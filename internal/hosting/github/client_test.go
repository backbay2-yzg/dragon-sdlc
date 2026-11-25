package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dragon/internal/hosting"
)

func TestGitHubClientETagAndPRLifecycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "octocat"})
		case "/repos/acme/dragon/issues":
			if r.Header.Get("If-None-Match") == `"etag-v1"` {
				w.Header().Set("X-RateLimit-Remaining", "4999")
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"etag-v1"`)
			w.Header().Set("X-RateLimit-Remaining", "4998")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"number":     10,
					"title":      "feat: add automation",
					"body":       "body",
					"state":      "open",
					"html_url":   "https://github.com/acme/dragon/issues/10",
					"updated_at": "2026-11-24T00:00:00Z",
					"user":       map[string]any{"login": "bob"},
					"labels":     []map[string]any{{"name": "dragon-auto"}},
				},
				{
					"number":       11,
					"title":        "PR disguised as issue",
					"state":        "open",
					"pull_request": map[string]any{"url": "..."},
				},
			})
		case "/repos/acme/dragon/pulls":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number":   55,
				"title":    "feat: add automation",
				"html_url": "https://github.com/acme/dragon/pull/55",
				"state":    "open",
				"head":     map[string]any{"sha": "abc123"},
			})
		case "/repos/acme/dragon/commits/abc123/check-runs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"check_runs": []map[string]any{
					{
						"id":          1,
						"name":        "go-test",
						"status":      "completed",
						"conclusion":  "failure",
						"html_url":    "https://github.com/acme/dragon/actions/runs/777/jobs/1",
						"details_url": "https://github.com/acme/dragon/actions/runs/777/jobs/1",
					},
				},
			})
		case "/repos/acme/dragon/releases":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           901,
				"tag_name":     "v1.2.0",
				"html_url":     "https://github.com/acme/dragon/releases/tag/v1.2.0",
				"published_at": "2026-11-24T01:00:00Z",
			})
		case "/repos/acme/dragon":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":              "dragon",
				"full_name":         "acme/dragon",
				"description":       "Automated SDLC",
				"default_branch":    "main",
				"clone_url":         "https://github.com/acme/dragon.git",
				"private":           true,
				"archived":          false,
				"language":          "Go",
				"open_issues_count": 3,
				"pushed_at":         "2026-11-24T02:00:00Z",
				"html_url":          "https://github.com/acme/dragon",
				"owner":             map[string]any{"login": "acme", "type": "Organization"},
				"permissions":       map[string]any{"admin": true, "push": true, "pull": true},
			})
		case "/user/repos":
			page := r.URL.Query().Get("page")
			if page == "1" {
				batch := make([]map[string]any, 100)
				for i := range batch {
					batch[i] = map[string]any{
						"name":           "repo",
						"full_name":      "acme/repo",
						"default_branch": "main",
						"owner":          map[string]any{"login": "acme", "type": "Organization"},
						"permissions":    map[string]any{"push": true, "pull": true},
					}
				}
				_ = json.NewEncoder(w).Encode(batch)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"name":           "final",
					"full_name":      "octocat/final",
					"default_branch": "develop",
					"private":        true,
					"owner":          map[string]any{"login": "octocat", "type": "User"},
					"permissions":    map[string]any{"push": false, "pull": true},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, func(ctx context.Context) (string, string, error) {
		return "tok-test", "pat", nil
	})
	ctx := context.Background()

	id, err := client.VerifyAuth(ctx)
	if err != nil || !id.Authenticated || id.Username != "octocat" {
		t.Fatalf("VerifyAuth unexpected: %+v err=%v", id, err)
	}

	repoInfo, err := client.GetRepo(ctx, "acme", "dragon")
	if err != nil || !repoInfo.Private || !repoInfo.CanPush || !repoInfo.CanAdmin || repoInfo.Description != "Automated SDLC" || repoInfo.Language != "Go" {
		t.Fatalf("GetRepo unexpected: %+v err=%v", repoInfo, err)
	}

	userRepos, err := client.ListUserRepos(ctx)
	if err != nil || len(userRepos) != 101 || userRepos[100].FullName != "octocat/final" || userRepos[100].CanPush {
		t.Fatalf("ListUserRepos unexpected: len=%d err=%v", len(userRepos), err)
	}

	unauthClient := NewClient(srv.URL, func(ctx context.Context) (string, string, error) {
		return "", "none", nil
	})
	if _, err := unauthClient.ListUserRepos(ctx); err == nil {
		t.Fatalf("expected error from unauthenticated ListUserRepos")
	}

	page1, err := client.ListIssues(ctx, "acme", "dragon", "")
	if err != nil || page1.NotModified || len(page1.Issues) != 1 || page1.Issues[0].Number != 10 {
		t.Fatalf("ListIssues page1 unexpected: %+v err=%v", page1, err)
	}

	page2, err := client.ListIssues(ctx, "acme", "dragon", page1.ETag)
	if err != nil || !page2.NotModified {
		t.Fatalf("expected 304 NotModified, got %+v err=%v", page2, err)
	}

	pr, err := client.CreatePullRequest(ctx, "acme", "dragon", hosting.CreatePRRequest{
		Title: "feat: add automation", Head: "auto/issue-10", Base: "main",
	})
	if err != nil || pr.Number != 55 || pr.HeadSHA != "abc123" {
		t.Fatalf("CreatePullRequest unexpected: %+v err=%v", pr, err)
	}

	checks, err := client.GetPRChecks(ctx, "acme", "dragon", pr.HeadSHA)
	if err != nil || checks.OverallState != "failure" || len(checks.FailedRunIDs) != 1 || checks.FailedRunIDs[0] != 777 {
		t.Fatalf("GetPRChecks unexpected: %+v err=%v", checks, err)
	}

	rel, err := client.CreateRelease(ctx, "acme", "dragon", hosting.CreateReleaseRequest{
		TagName: "v1.2.0", TargetCommitish: "main", Name: "v1.2.0", Body: "notes",
	})
	if err != nil || rel.TagName != "v1.2.0" {
		t.Fatalf("CreateRelease unexpected: %+v err=%v", rel, err)
	}
}
