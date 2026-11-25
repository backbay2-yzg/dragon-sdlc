package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dragon/internal/agent"
	"dragon/internal/config"
	"dragon/internal/gitops"
	"dragon/internal/hosting"
	"dragon/internal/release"
	"dragon/internal/router"
	"dragon/internal/store"
)

type testMockAgent struct {
	id         string
	mu         sync.Mutex
	calls      []string
	failOnce   bool
	rejectOnce bool
}

func (m *testMockAgent) ID() string   { return m.id }
func (m *testMockAgent) Kind() string { return "mock" }
func (m *testMockAgent) Detect(ctx context.Context) (bool, string, string) {
	return true, "/mock/" + m.id, "1.0.0"
}

func (m *testMockAgent) Run(ctx context.Context, req agent.RunRequest, onEvent func(agent.StreamEvent)) (*agent.RunResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req.Prompt)
	m.mu.Unlock()

	if onEvent != nil {
		onEvent(agent.StreamEvent{Timestamp: store.NowRFC3339(), Stream: "stdout", EventType: "text", Text: "Mock agent running"})
	}

	// Simulate creating/updating code depending on the stage prompt.
	switch {
	case strings.Contains(req.Prompt, "Stage 1: Requirement Analysis"):
		return &agent.RunResult{AgentID: m.id, Output: "# Spec\nAdd feature X cleanly."}, nil
	case strings.Contains(req.Prompt, "Stage 2: Technical Design"):
		return &agent.RunResult{AgentID: m.id, Output: "# Plan\nWrite feature.txt"}, nil
	case strings.Contains(req.Prompt, "Stage 3: Code Implementation"):
		content := "v1\n"
		if strings.Contains(req.Prompt, "v2") {
			content = "v2\n"
		}
		if err := os.WriteFile(filepath.Join(req.WorkDir, "feature.txt"), []byte(content), 0o644); err != nil {
			return nil, err
		}
		if m.failOnce {
			m.failOnce = false
			_ = os.WriteFile(filepath.Join(req.WorkDir, "status.txt"), []byte("BROKEN\n"), 0o644)
		} else {
			_ = os.WriteFile(filepath.Join(req.WorkDir, "status.txt"), []byte("OK\n"), 0o644)
		}
		return &agent.RunResult{AgentID: m.id, Output: "Implemented feature.txt"}, nil
	case strings.Contains(req.Prompt, "Stage 4 Auto-Fix"):
		_ = os.WriteFile(filepath.Join(req.WorkDir, "status.txt"), []byte("OK\n"), 0o644)
		return &agent.RunResult{AgentID: m.id, Output: "Fixed status.txt to OK"}, nil
	case strings.Contains(req.Prompt, "Stage 5: AI Code Review"):
		return &agent.RunResult{AgentID: m.id, Output: "# Review\nLooks clean and well-tested."}, nil
	case strings.Contains(req.Prompt, "Stage 6 CI Auto-Fix"):
		_ = os.WriteFile(filepath.Join(req.WorkDir, "ci.txt"), []byte("FIXED\n"), 0o644)
		return &agent.RunResult{AgentID: m.id, Output: "Fixed CI failure"}, nil
	default:
		return &agent.RunResult{AgentID: m.id, Output: "Done"}, nil
	}
}

type testMockHosting struct {
	mu           sync.Mutex
	upstreamDir  string
	prCreated    bool
	ciChecksCnt  int
	merged       bool
	releasedTag  string
}

func (h *testMockHosting) Name() string { return "github" }
func (h *testMockHosting) VerifyAuth(ctx context.Context) (*hosting.AuthIdentity, error) {
	return &hosting.AuthIdentity{Authenticated: true, Username: "tester"}, nil
}
func (h *testMockHosting) ListUserRepos(ctx context.Context) ([]hosting.RemoteRepo, error) {
	return nil, nil
}
func (h *testMockHosting) GetRepo(ctx context.Context, owner, name string) (*hosting.RemoteRepo, error) {
	return &hosting.RemoteRepo{
		Owner: owner, Name: name, FullName: owner + "/" + name,
		DefaultBranch: "main", CloneURL: h.upstreamDir,
	}, nil
}
func (h *testMockHosting) ListIssues(ctx context.Context, owner, name, etag string) (*hosting.IssuePage, error) {
	return &hosting.IssuePage{}, nil
}
func (h *testMockHosting) AddIssueLabels(ctx context.Context, owner, name string, number int, labels []string) error {
	return nil
}
func (h *testMockHosting) CreateIssueComment(ctx context.Context, owner, name string, number int, body string) error {
	return nil
}
func (h *testMockHosting) CreatePullRequest(ctx context.Context, owner, name string, req hosting.CreatePRRequest) (*hosting.RemotePR, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prCreated = true
	return &hosting.RemotePR{
		Number:  77,
		Title:   req.Title,
		HTMLURL: "https://github.com/acme/dragon/pull/77",
		State:   "open",
		HeadSHA: "headsha123",
	}, nil
}
func (h *testMockHosting) GetPRChecks(ctx context.Context, owner, name, ref string) (*hosting.CheckSummary, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ciChecksCnt++
	if h.ciChecksCnt == 1 {
		return &hosting.CheckSummary{
			OverallState: "failure",
			FailedRunIDs: []int64{404},
			Checks:       []hosting.CheckRunDetail{{Name: "ci-test", Status: "completed", Conclusion: "failure", RunID: 404}},
		}, nil
	}
	return &hosting.CheckSummary{
		OverallState: "success",
		Checks:       []hosting.CheckRunDetail{{Name: "ci-test", Status: "completed", Conclusion: "success"}},
	}, nil
}
func (h *testMockHosting) GetFailedActionLogs(ctx context.Context, owner, name string, runID int64) (string, error) {
	return "FAIL: TestCI missing ci.txt", nil
}
func (h *testMockHosting) MergePullRequest(ctx context.Context, owner, name string, number int, strategy, title, message string) (*hosting.MergeResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.merged = true
	return &hosting.MergeResult{Merged: true, SHA: "mergesha", Message: "Pull Request successfully merged"}, nil
}
func (h *testMockHosting) ListTags(ctx context.Context, owner, name string) ([]string, error) {
	return []string{"v0.1.0"}, nil
}
func (h *testMockHosting) CreateRelease(ctx context.Context, owner, name string, req hosting.CreateReleaseRequest) (*hosting.RemoteRelease, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.releasedTag = req.TagName
	return &hosting.RemoteRelease{
		ID:          1,
		TagName:     req.TagName,
		HTMLURL:     "https://github.com/acme/dragon/releases/tag/" + req.TagName,
		PublishedAt: store.NowRFC3339(),
	}, nil
}
func (h *testMockHosting) GetReleaseWorkflowStatus(ctx context.Context, owner, name, tag string) (*hosting.WorkflowRunStatus, error) {
	return &hosting.WorkflowRunStatus{Status: "completed_success"}, nil
}

type testEventSink struct {
	mu     sync.Mutex
	events []string
}

func (e *testEventSink) Emit(name string, payload any) {
	e.mu.Lock()
	e.events = append(e.events, name)
	e.mu.Unlock()
}

func TestPipelineVerificationRetryAndApprovalFlow(t *testing.T) {
	tmp := t.TempDir()
	// Create bare remote repo so git push from worktree succeeds locally!
	remoteBare := filepath.Join(tmp, "remote.git")
	seedDir := filepath.Join(tmp, "seed")
	runCmd := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		gitops.ConfigureHiddenCommand(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runCmd(tmp, "init", "--bare", remoteBare)
	runCmd(seedDir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(seedDir, "add", ".")
	runCmd(seedDir, "-c", "user.name=T", "-c", "user.email=t@e.com", "commit", "-m", "init")
	runCmd(seedDir, "remote", "add", "origin", remoteBare)
	runCmd(seedDir, "push", "-u", "origin", "main")
	// Set bare HEAD to main so clone --bare checks out main.
	runCmd(remoteBare, "symbolic-ref", "HEAD", "refs/heads/main")

	paths, err := config.ResolvePaths(filepath.Join(tmp, "appdata"))
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed repo with verification command requiring status.txt == OK (using Go-portable check via findstr/grep).
	localRepoPath := filepath.Join(tmp, "workspaces", "dragon")
	repo := store.Repository{
		ID:                "repo-1",
		Owner:             "acme",
		Name:              "dragon",
		FullName:          "acme/dragon",
		DefaultBranch:     "main",
		LocalPath:         localRepoPath,
		AutonomyMode:      "semi_auto",
		DefaultTemplateID: "full_feature",
		ReleaseMode:       "immediate",
		MergeStrategy:     "squash",
		TestCmd:           "findstr /C:\"OK\" status.txt",
		MaxTestRetries:    2,
		MaxCIRetries:      1,
		Enabled:           true,
	}
	if err := db.UpsertRepository(repo); err != nil {
		t.Fatal(err)
	}
	issue := store.Issue{
		ID:         "iss-1",
		RepoID:     "repo-1",
		Number:     1,
		Title:      "add feature X",
		Body:       "Implement feature X",
		State:      "open",
		Author:     "alice",
		LabelsJSON: `["enhancement"]`,
		Status:     "backlog",
	}
	if err := db.UpsertIssue(issue); err != nil {
		t.Fatal(err)
	}

	// Mark claude detected in DB and register mock agent overriding claude.
	_ = db.UpdateAgentDetection("claude", true, "/mock/claude", "1.0")
	mockAg := &testMockAgent{id: "claude", failOnce: true}
	reg := agent.NewRegistry(db)
	reg.RegisterOverride(mockAg)

	mockHost := &testMockHosting{upstreamDir: remoteBare}
	sink := &testEventSink{}
	relMgr := release.NewManager(db, mockHost, sink, nil)
	gitMgr := gitops.NewManager(paths.ReposDir, paths.WorktreesDir)
	rtr := router.NewRouter(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orch := NewOrchestrator(ctx, db, paths, mockHost, gitMgr, reg, rtr, relMgr, sink, nil, nil, 2)
	orch.SetCIPollInterval(5 * time.Millisecond)

	run, err := orch.StartIssueRun(ctx, StartRunRequest{
		RepoID:  "repo-1",
		IssueID: "iss-1",
	})
	if err != nil {
		t.Fatalf("StartIssueRun failed: %v", err)
	}

	// Wait for semi_auto gate pause before merge_release.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		cur, _ := db.GetPipelineRun(run.ID)
		if cur.Status == "waiting_approval" {
			break
		}
		if cur.Status == "failed" {
			t.Fatalf("run failed unexpectedly: %s", cur.ErrorMessage)
		}
		time.Sleep(25 * time.Millisecond)
	}

	cur, _ := db.GetPipelineRun(run.ID)
	if cur.Status != "waiting_approval" || cur.CurrentStage != store.StageMergeRelease {
		t.Fatalf("expected waiting_approval at merge_release, got status=%s stage=%s err=%s", cur.Status, cur.CurrentStage, cur.ErrorMessage)
	}

	// Reject with feedback to test rollback to implementation!
	if err := orch.RequestChanges(run.ID, "please update feature.txt to v2"); err != nil {
		t.Fatalf("RequestChanges failed: %v", err)
	}

	// Wait until it returns to waiting_approval at merge_release a second time.
	time.Sleep(50 * time.Millisecond)
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		cur, _ = db.GetPipelineRun(run.ID)
		if cur.Status == "waiting_approval" {
			break
		}
		if cur.Status == "failed" {
			t.Fatalf("run failed on rework: %s", cur.ErrorMessage)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if cur.Status != "waiting_approval" {
		t.Fatalf("expected second waiting_approval after rework, got %s", cur.Status)
	}

	// Now approve!
	if err := orch.ApproveRun(run.ID); err != nil {
		t.Fatalf("ApproveRun failed: %v", err)
	}

	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		cur, _ = db.GetPipelineRun(run.ID)
		if cur.Status == "succeeded" || cur.Status == "failed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if cur.Status != "succeeded" {
		t.Fatalf("expected run succeeded, got %s (%s)", cur.Status, cur.ErrorMessage)
	}
	if !mockHost.prCreated || !mockHost.merged || mockHost.releasedTag != "v0.2.0" {
		t.Fatalf("unexpected hosting state: pr=%v merged=%v tag=%q", mockHost.prCreated, mockHost.merged, mockHost.releasedTag)
	}
	if !strings.Contains(cur.WorktreePath, ".dragon-worktrees") {
		t.Fatalf("expected sibling worktree under .dragon-worktrees, got %q", cur.WorktreePath)
	}
	if _, err := os.Stat(cur.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree auto-cleaned on success, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(localRepoPath, ".git")); err != nil {
		t.Fatalf("expected cloned local repo preserved at %q, err=%v", localRepoPath, err)
	}
}

func TestPipelineWorkspaceGuardsAndFailedRetention(t *testing.T) {
	tmp := t.TempDir()
	remoteBare := filepath.Join(tmp, "remote.git")
	seedDir := filepath.Join(tmp, "seed")
	runCmd := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		gitops.ConfigureHiddenCommand(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runCmd(tmp, "init", "--bare", remoteBare)
	runCmd(seedDir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(seedDir, "add", ".")
	runCmd(seedDir, "-c", "user.name=T", "-c", "user.email=t@e.com", "commit", "-m", "init")
	runCmd(seedDir, "remote", "add", "origin", remoteBare)
	runCmd(seedDir, "push", "-u", "origin", "main")
	runCmd(remoteBare, "symbolic-ref", "HEAD", "refs/heads/main")

	paths, err := config.ResolvePaths(filepath.Join(tmp, "appdata"))
	if err != nil {
		t.Fatal(err)
	}
	_ = paths.EnsureDirs()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 1. Empty LocalPath blocks StartIssueRun.
	_ = db.UpsertRepository(store.Repository{
		ID:            "repo-unconf",
		Owner:         "acme",
		Name:          "unconf",
		FullName:      "acme/unconf",
		DefaultBranch: "main",
		Enabled:       true,
	})
	_ = db.UpsertIssue(store.Issue{
		ID: "iss-unconf", RepoID: "repo-unconf", Number: 10, Title: "fix typo", State: "open", Status: "backlog",
	})

	_ = db.UpdateAgentDetection("claude", true, "/mock/claude", "1.0")
	mockAg := &testMockAgent{id: "claude"}
	reg := agent.NewRegistry(db)
	reg.RegisterOverride(mockAg)
	mockHost := &testMockHosting{upstreamDir: remoteBare}
	sink := &testEventSink{}
	relMgr := release.NewManager(db, mockHost, sink, nil)
	gitMgr := gitops.NewManager(paths.ReposDir, paths.WorktreesDir)
	rtr := router.NewRouter(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orch := NewOrchestrator(ctx, db, paths, mockHost, gitMgr, reg, rtr, relMgr, sink, nil, nil, 2)
	if _, err := orch.StartIssueRun(ctx, StartRunRequest{RepoID: "repo-unconf", IssueID: "iss-unconf"}); err == nil {
		t.Fatalf("expected StartIssueRun to block repo with empty LocalPath")
	}

	// 2. Failed run retains worktree for inspection and supports CleanupRunWorktree.
	localFailRepo := filepath.Join(tmp, "workspaces", "failrepo")
	_ = db.UpsertRepository(store.Repository{
		ID:                "repo-fail",
		Owner:             "acme",
		Name:              "failrepo",
		FullName:          "acme/failrepo",
		DefaultBranch:     "main",
		LocalPath:         localFailRepo,
		AutonomyMode:      "full_auto",
		DefaultTemplateID: "quick_bugfix",
		TestCmd:           "findstr /C:\"NEVER_MATCH_XYZ\" status.txt",
		MaxTestRetries:    0,
		Enabled:           true,
	})
	_ = db.UpsertIssue(store.Issue{
		ID: "iss-fail", RepoID: "repo-fail", Number: 11, Title: "broken test", State: "open", Status: "backlog",
	})

	run, err := orch.StartIssueRun(ctx, StartRunRequest{RepoID: "repo-fail", IssueID: "iss-fail"})
	if err != nil {
		t.Fatalf("StartIssueRun failed: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	var cur store.PipelineRun
	for time.Now().Before(deadline) {
		cur, _ = db.GetPipelineRun(run.ID)
		if cur.Status == "failed" || cur.Status == "succeeded" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if cur.Status != "failed" {
		t.Fatalf("expected failed run, got %s", cur.Status)
	}
	if info, statErr := os.Stat(cur.WorktreePath); statErr != nil || !info.IsDir() {
		t.Fatalf("expected failed run worktree retained at %q, statErr=%v", cur.WorktreePath, statErr)
	}

	if err := orch.CleanupRunWorktree(run.ID); err != nil {
		t.Fatalf("CleanupRunWorktree failed: %v", err)
	}
	if _, statErr := os.Stat(cur.WorktreePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected worktree cleaned after CleanupRunWorktree, statErr=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(localFailRepo, ".git")); statErr != nil {
		t.Fatalf("expected localFailRepo preserved, statErr=%v", statErr)
	}
}
