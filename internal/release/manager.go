package release

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"dragon/internal/gitops"
	"dragon/internal/hosting"
	"dragon/internal/store"
)

// EventSink broadcasts release updates to the UI.
type EventSink interface {
	Emit(eventName string, payload any)
}

// ReleaseToastNotifier sends OS notifications when a release finishes.
type ReleaseToastNotifier interface {
	NotifyReleasePublished(repo store.Repository, rec store.ReleaseRecord)
}

// ReleasePreview summarizes pending merged PRs and computed SemVer release notes.
type ReleasePreview struct {
	RepoID    string              `json:"repoId"`
	PrevTag   string              `json:"prevTag"`
	NextTag   string              `json:"nextTag"`
	Version   string              `json:"version"`
	BumpType  string              `json:"bumpType"`
	Changelog string              `json:"changelog"`
	ItemIDs   []string            `json:"itemIds"`
	Items     []store.ReleaseItem `json:"items"`
}

// PublishRequest describes parameters to publish a GitHub Release.
type PublishRequest struct {
	RepoID     string   `json:"repoId"`
	TagName    string   `json:"tagName"`
	Changelog  string   `json:"changelog"`
	Mode       string   `json:"mode"` // immediate | batch
	Draft      bool     `json:"draft"`
	Prerelease bool     `json:"prerelease"`
	ItemIDs    []string `json:"itemIds"`
}

// Manager handles Conventional Commit aggregation, SemVer releases, and Actions tracking.
type Manager struct {
	db       *store.DB
	provider hosting.Provider
	events   EventSink
	notifier ReleaseToastNotifier
}

// NewManager creates a Release Manager.
func NewManager(db *store.DB, provider hosting.Provider, events EventSink, notifier ReleaseToastNotifier) *Manager {
	return &Manager{
		db:       db,
		provider: provider,
		events:   events,
		notifier: notifier,
	}
}

// RecordMergedPR parses a merged PR into a pending ReleaseItem.
func (m *Manager) RecordMergedPR(repo store.Repository, run store.PipelineRun, prTitle string) (store.ReleaseItem, error) {
	parsed := gitops.ParseConventionalTitle(run.PRNumber, prTitle)
	item := store.ReleaseItem{
		ID:          uuid.NewString(),
		RepoID:      repo.ID,
		RunID:       run.ID,
		PRNumber:    run.PRNumber,
		PRTitle:     prTitle,
		CommitType:  parsed.Type,
		CommitScope: parsed.Scope,
		Summary:     parsed.Summary,
		Breaking:    parsed.Breaking,
		MergedAt:    store.NowRFC3339(),
		ReleaseID:   "",
	}
	if err := m.db.InsertReleaseItem(item); err != nil {
		return store.ReleaseItem{}, err
	}
	m.emit("dragon:release:updated", store.ReleaseRecord{RepoID: repo.ID})
	return item, nil
}

// PreviewRelease computes the next SemVer version and categorized Changelog for pending ReleaseItems.
func (m *Manager) PreviewRelease(ctx context.Context, repoID string) (*ReleasePreview, error) {
	repo, err := m.db.GetRepository(repoID)
	if err != nil {
		return nil, err
	}
	items, err := m.db.ListPendingReleaseItems(repoID)
	if err != nil {
		return nil, err
	}

	var prevTag string
	if m.provider != nil {
		if tags, err := m.provider.ListTags(ctx, repo.Owner, repo.Name); err == nil {
			prevTag = gitops.LatestSemVerTag(tags)
		}
	}
	if prevTag == "" {
		// Fall back to latest local published release record if remote tags couldn't be fetched.
		if recs, err := m.db.ListReleaseRecords(repoID, 10); err == nil {
			var tags []string
			for _, r := range recs {
				if r.TagName != "" {
					tags = append(tags, r.TagName)
				}
			}
			prevTag = gitops.LatestSemVerTag(tags)
		}
	}

	changes := make([]gitops.ConventionalChange, 0, len(items))
	itemIDs := make([]string, 0, len(items))
	for _, it := range items {
		changes = append(changes, gitops.ConventionalChange{
			PRNumber: it.PRNumber,
			Title:    it.PRTitle,
			Type:     it.CommitType,
			Scope:    it.CommitScope,
			Summary:  it.Summary,
			Breaking: it.Breaking,
		})
		itemIDs = append(itemIDs, it.ID)
	}

	nextTag, ver, bump := gitops.NextVersion(prevTag, changes)
	changelog := gitops.GenerateChangelog(nextTag, repo.FullName, changes)

	return &ReleasePreview{
		RepoID:    repoID,
		PrevTag:   prevTag,
		NextTag:   nextTag,
		Version:   ver,
		BumpType:  bump,
		Changelog: changelog,
		ItemIDs:   itemIDs,
		Items:     items,
	}, nil
}

// PublishRelease creates a GitHub Release, attaches pending items, and tracks Actions release status.
func (m *Manager) PublishRelease(ctx context.Context, req PublishRequest) (*store.ReleaseRecord, error) {
	if m.provider == nil {
		return nil, errors.New("未初始化 Hosting Provider")
	}
	repo, err := m.db.GetRepository(req.RepoID)
	if err != nil {
		return nil, err
	}

	preview, err := m.PreviewRelease(ctx, req.RepoID)
	if err != nil {
		return nil, err
	}

	tagName := strings.TrimSpace(req.TagName)
	if tagName == "" {
		tagName = preview.NextTag
	}
	changelog := strings.TrimSpace(req.Changelog)
	if changelog == "" {
		changelog = preview.Changelog
	}
	mode := req.Mode
	if mode == "" {
		mode = repo.ReleaseMode
	}
	itemIDs := req.ItemIDs
	if len(itemIDs) == 0 {
		itemIDs = preview.ItemIDs
	}

	now := store.NowRFC3339()
	rec := store.ReleaseRecord{
		ID:            uuid.NewString(),
		RepoID:        repo.ID,
		TagName:       tagName,
		PrevTag:       preview.PrevTag,
		Version:       strings.TrimPrefix(tagName, "v"),
		BumpType:      preview.BumpType,
		Changelog:     changelog,
		Mode:          mode,
		Status:        "draft",
		ActionsStatus: "none",
		CreatedAt:     now,
	}

	remoteRel, err := m.provider.CreateRelease(ctx, repo.Owner, repo.Name, hosting.CreateReleaseRequest{
		TagName:         tagName,
		TargetCommitish: repo.DefaultBranch,
		Name:            tagName,
		Body:            changelog,
		Draft:           req.Draft,
		Prerelease:      req.Prerelease,
	})
	if err != nil {
		rec.Status = "failed"
		_ = m.db.UpsertReleaseRecord(rec)
		m.emit("dragon:release:updated", rec)
		return nil, fmt.Errorf("发布 Release %s 失败: %w", tagName, err)
	}

	if req.Draft {
		rec.Status = "draft"
	} else {
		rec.Status = "published"
		rec.PublishedAt = remoteRel.PublishedAt
		if rec.PublishedAt == "" {
			rec.PublishedAt = store.NowRFC3339()
		}
		rec.ActionsStatus = "queued"
	}
	rec.HTMLURL = remoteRel.HTMLURL

	if err := m.db.UpsertReleaseRecord(rec); err != nil {
		return nil, err
	}
	if err := m.db.AttachItemsToRelease(itemIDs, rec.ID); err != nil {
		return nil, err
	}

	m.emit("dragon:release:updated", rec)
	if !req.Draft && m.notifier != nil {
		m.notifier.NotifyReleasePublished(repo, rec)
	}

	if !req.Draft {
		go m.watchReleaseWorkflow(repo, rec)
	}

	return &rec, nil
}

func (m *Manager) watchReleaseWorkflow(repo store.Repository, rec store.ReleaseRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	for i := 0; i < 12; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
		st, err := m.provider.GetReleaseWorkflowStatus(ctx, repo.Owner, repo.Name, rec.TagName)
		if err != nil {
			continue
		}
		if st.Status != "" && st.Status != rec.ActionsStatus {
			rec.ActionsStatus = st.Status
			rec.ActionsURL = st.HTMLURL
			_ = m.db.UpsertReleaseRecord(rec)
			m.emit("dragon:release:updated", rec)
			if st.Status == "completed_success" || st.Status == "completed_failure" {
				return
			}
		}
	}
}

// RefreshReleaseActions polls the latest GitHub Actions status for a ReleaseRecord.
func (m *Manager) RefreshReleaseActions(ctx context.Context, releaseID string) (*store.ReleaseRecord, error) {
	rec, err := m.db.GetReleaseRecord(releaseID)
	if err != nil {
		return nil, err
	}
	repo, err := m.db.GetRepository(rec.RepoID)
	if err != nil {
		return nil, err
	}
	st, err := m.provider.GetReleaseWorkflowStatus(ctx, repo.Owner, repo.Name, rec.TagName)
	if err != nil {
		return nil, err
	}
	rec.ActionsStatus = st.Status
	if st.HTMLURL != "" {
		rec.ActionsURL = st.HTMLURL
	}
	if err := m.db.UpsertReleaseRecord(rec); err != nil {
		return nil, err
	}
	m.emit("dragon:release:updated", rec)
	return &rec, nil
}

func (m *Manager) emit(name string, payload any) {
	if m.events != nil {
		m.events.Emit(name, payload)
	}
}
