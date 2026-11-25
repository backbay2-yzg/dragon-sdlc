package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"dragon/internal/hosting"
	"dragon/internal/store"
)

// IssueStarter is implemented by the Pipeline Orchestrator to auto-enqueue trigger-labeled issues.
type IssueStarter interface {
	EnqueueAutoIssue(ctx context.Context, repo store.Repository, issue store.Issue) error
}

// EventSink emits sync status updates to the frontend.
type EventSink interface {
	Emit(eventName string, payload any)
}

// SyncSummary captures the result of syncing a single repository.
type SyncSummary struct {
	RepoID             string `json:"repoId"`
	FullName           string `json:"fullName"`
	NotModified        bool   `json:"notModified"`
	SyncedCount        int    `json:"syncedCount"`
	AutoEnqueuedCount  int    `json:"autoEnqueuedCount"`
	RateLimitRemaining int    `json:"rateLimitRemaining"`
	SyncedAt           string `json:"syncedAt"`
}

// Poller periodically polls enabled GitHub repositories using ETag caching.
type Poller struct {
	db       *store.DB
	provider hosting.Provider
	starter  IssueStarter
	events   EventSink

	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	syncing  map[string]bool
}

// NewPoller initializes an Issue Poller.
func NewPoller(db *store.DB, provider hosting.Provider, starter IssueStarter, events EventSink) *Poller {
	return &Poller{
		db:       db,
		provider: provider,
		starter:  starter,
		events:   events,
		syncing:  make(map[string]bool),
	}
}

// Start runs the background polling loop until Stop is called or ctx is cancelled.
func (p *Poller) Start(ctx context.Context) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.stopCh = make(chan struct{})
	stopCh := p.stopCh
	p.mu.Unlock()

	go func() {
		// Initial light sync shortly after boot.
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-time.After(3 * time.Second):
			_, _ = p.SyncDueRepos(ctx)
		}

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-ticker.C:
				_, _ = p.SyncDueRepos(ctx)
			}
		}
	}()
}

// Stop stops the background polling loop.
func (p *Poller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running && p.stopCh != nil {
		close(p.stopCh)
		p.running = false
	}
}

// SyncDueRepos syncs every enabled repository whose poll interval has elapsed.
func (p *Poller) SyncDueRepos(ctx context.Context) ([]SyncSummary, error) {
	repos, err := p.db.ListRepositories()
	if err != nil {
		return nil, err
	}
	var results []SyncSummary
	now := time.Now().UTC()
	for _, repo := range repos {
		if !repo.Enabled {
			continue
		}
		if repo.LastSyncedAt != "" {
			if last, err := time.Parse(time.RFC3339, repo.LastSyncedAt); err == nil {
				interval := time.Duration(repo.PollIntervalSec) * time.Second
				if interval <= 0 {
					interval = 120 * time.Second
				}
				if now.Sub(last) < interval {
					continue
				}
			}
		}
		sum, err := p.SyncRepoNow(ctx, repo.ID)
		if err == nil && sum != nil {
			results = append(results, *sum)
		}
	}
	return results, nil
}

// SyncAllNow forces immediate synchronization of all enabled repositories.
func (p *Poller) SyncAllNow(ctx context.Context) ([]SyncSummary, error) {
	repos, err := p.db.ListRepositories()
	if err != nil {
		return nil, err
	}
	var results []SyncSummary
	var lastErr error
	for _, repo := range repos {
		if !repo.Enabled {
			continue
		}
		sum, err := p.SyncRepoNow(ctx, repo.ID)
		if err != nil {
			lastErr = err
			continue
		}
		if sum != nil {
			results = append(results, *sum)
		}
	}
	if len(results) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return results, nil
}

// SyncRepoNow fetches open issues for a single repository with ETag caching.
func (p *Poller) SyncRepoNow(ctx context.Context, repoID string) (*SyncSummary, error) {
	p.mu.Lock()
	if p.syncing[repoID] {
		p.mu.Unlock()
		return nil, nil
	}
	p.syncing[repoID] = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.syncing, repoID)
		p.mu.Unlock()
	}()

	repo, err := p.db.GetRepository(repoID)
	if err != nil {
		return nil, err
	}

	page, err := p.provider.ListIssues(ctx, repo.Owner, repo.Name, repo.ETag)
	if err != nil {
		return nil, err
	}

	now := store.NowRFC3339()
	if err := p.db.UpdateRepositorySyncState(repo.ID, page.ETag, now); err != nil {
		return nil, err
	}

	summary := &SyncSummary{
		RepoID:             repo.ID,
		FullName:           repo.FullName,
		NotModified:        page.NotModified,
		RateLimitRemaining: page.RateLimitRemaining,
		SyncedAt:           now,
	}

	if !page.NotModified {
		trigger := strings.ToLower(strings.TrimSpace(repo.TriggerLabel))
		existingIssues, _ := p.db.ListIssues(repo.ID, "all", "")
		byNumber := make(map[int]store.Issue, len(existingIssues))
		for _, it := range existingIssues {
			byNumber[it.Number] = it
		}

		for _, remote := range page.Issues {
			matched := false
			if trigger != "" {
				for _, lb := range remote.Labels {
					if strings.EqualFold(strings.TrimSpace(lb), trigger) {
						matched = true
						break
					}
				}
			}
			labelsBytes, _ := json.Marshal(remote.Labels)
			issueID := fmt.Sprintf("%s#%d", repo.ID, remote.Number)
			if prev, ok := byNumber[remote.Number]; ok {
				issueID = prev.ID
			} else if issueID == "" {
				issueID = uuid.NewString()
			}

			iss := store.Issue{
				ID:             issueID,
				RepoID:         repo.ID,
				Number:         remote.Number,
				Title:          remote.Title,
				Body:           remote.Body,
				State:          remote.State,
				Author:         remote.Author,
				LabelsJSON:     string(labelsBytes),
				HTMLURL:        remote.HTMLURL,
				TriggerMatched: matched,
				Status:         "backlog",
				UpdatedAt:      remote.UpdatedAt,
				SyncedAt:       now,
			}
			if err := p.db.UpsertIssue(iss); err != nil {
				continue
			}
			summary.SyncedCount++

			if matched && p.starter != nil && strings.TrimSpace(repo.LocalPath) != "" {
				stored, err := p.db.GetIssue(issueID)
				if err == nil && stored.Status == "backlog" {
					hasActive, err := p.db.HasActiveRunForIssue(stored.ID)
					if err == nil && !hasActive {
						if err := p.starter.EnqueueAutoIssue(ctx, repo, stored); err == nil {
							summary.AutoEnqueuedCount++
						}
					}
				}
			}
		}
	}

	if p.events != nil {
		p.events.Emit("dragon:issues:synced", summary)
	}
	return summary, nil
}
