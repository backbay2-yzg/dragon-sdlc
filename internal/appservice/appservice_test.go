package appservice

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"dragon/internal/gitops"
	"dragon/internal/hosting"
	"dragon/internal/secrets"
	"dragon/internal/store"
)

type fakeProvider struct {
	cloneURLs map[string]string
}

func (f *fakeProvider) Name() string { return "github" }
func (f *fakeProvider) VerifyAuth(ctx context.Context) (*hosting.AuthIdentity, error) {
	return &hosting.AuthIdentity{Authenticated: true, Method: "pat", Username: "maintainer"}, nil
}
func (f *fakeProvider) ListUserRepos(ctx context.Context) ([]hosting.RemoteRepo, error) {
	return []hosting.RemoteRepo{
		{
			Owner:           "acme",
			Name:            "dragon",
			FullName:        "acme/dragon",
			Description:     "Dragon orchestrator",
			DefaultBranch:   "main",
			Language:        "Go",
			OpenIssuesCount: 5,
			OwnerType:       "Organization",
			CanAdmin:        true,
			CanPush:         true,
			CanPull:         true,
		},
		{
			Owner:           "maintainer",
			Name:            "sidekick",
			FullName:        "maintainer/sidekick",
			Description:     "Helper CLI",
			DefaultBranch:   "main",
			Private:         true,
			Language:        "TypeScript",
			OpenIssuesCount: 2,
			OwnerType:       "User",
			CanPush:         true,
			CanPull:         true,
		},
	}, nil
}
func (f *fakeProvider) GetRepo(ctx context.Context, owner, name string) (*hosting.RemoteRepo, error) {
	cloneURL := ""
	if f.cloneURLs != nil {
		cloneURL = f.cloneURLs[owner+"/"+name]
	}
	return &hosting.RemoteRepo{
		Owner:         owner,
		Name:          name,
		FullName:      owner + "/" + name,
		DefaultBranch: "main",
		CloneURL:      cloneURL,
		CanPush:       true,
		CanPull:       true,
	}, nil
}
func (f *fakeProvider) ListIssues(ctx context.Context, owner, name, etag string) (*hosting.IssuePage, error) {
	return &hosting.IssuePage{NotModified: true, ETag: etag}, nil
}
func (f *fakeProvider) AddIssueLabels(ctx context.Context, owner, name string, number int, labels []string) error {
	return nil
}
func (f *fakeProvider) CreateIssueComment(ctx context.Context, owner, name string, number int, body string) error {
	return nil
}
func (f *fakeProvider) CreatePullRequest(ctx context.Context, owner, name string, req hosting.CreatePRRequest) (*hosting.RemotePR, error) {
	return &hosting.RemotePR{Number: 1}, nil
}
func (f *fakeProvider) GetPRChecks(ctx context.Context, owner, name, ref string) (*hosting.CheckSummary, error) {
	return &hosting.CheckSummary{OverallState: "none"}, nil
}
func (f *fakeProvider) GetFailedActionLogs(ctx context.Context, owner, name string, runID int64) (string, error) {
	return "", nil
}
func (f *fakeProvider) MergePullRequest(ctx context.Context, owner, name string, number int, strategy, title, message string) (*hosting.MergeResult, error) {
	return &hosting.MergeResult{Merged: true}, nil
}
func (f *fakeProvider) ListTags(ctx context.Context, owner, name string) ([]string, error) {
	return []string{"v0.1.0"}, nil
}
func (f *fakeProvider) CreateRelease(ctx context.Context, owner, name string, req hosting.CreateReleaseRequest) (*hosting.RemoteRelease, error) {
	return &hosting.RemoteRelease{TagName: req.TagName}, nil
}
func (f *fakeProvider) GetReleaseWorkflowStatus(ctx context.Context, owner, name, tag string) (*hosting.WorkflowRunStatus, error) {
	return &hosting.WorkflowRunStatus{Status: "none"}, nil
}

func TestAppCoreAndServicesRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	core, err := Bootstrap(BootstrapOptions{
		CustomRoot: tmp,
		DSN:        ":memory:",
		Secrets:    secrets.NewMemoryStore(),
		Provider:   &fakeProvider{},
	})
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	defer core.Shutdown()

	settingsSvc := NewSettingsService(core)
	s, err := settingsSvc.GetSettings()
	if err != nil || s.MaxConcurrency != 2 {
		t.Fatalf("GetSettings unexpected: %+v err=%v", s, err)
	}
	s.MaxConcurrency = 4
	s.ThemeMode = "dark"
	saved, err := settingsSvc.SaveSettings(s)
	if err != nil || saved.MaxConcurrency != 4 || saved.ThemeMode != "dark" {
		t.Fatalf("SaveSettings unexpected: %+v err=%v", saved, err)
	}

	if err := settingsSvc.SaveLLMAPIKey("sk-test"); err != nil {
		t.Fatalf("SaveLLMAPIKey failed: %v", err)
	}
	if !core.Secrets.Has(secrets.KeyLLMAPIKey) {
		t.Fatalf("expected LLM API key in secrets store")
	}

	repoSvc := NewRepoService(core)
	added, err := repoSvc.AddRepo(AddRepoRequest{FullName: "https://github.com/acme/dragon"})
	if err != nil || added.FullName != "acme/dragon" {
		t.Fatalf("AddRepo unexpected: %+v err=%v", added, err)
	}

	remoteOpts, err := repoSvc.ListRemoteRepos()
	if err != nil || len(remoteOpts) != 2 || !remoteOpts[0].AlreadyConnected || remoteOpts[1].AlreadyConnected {
		t.Fatalf("ListRemoteRepos unexpected: %+v err=%v", remoteOpts, err)
	}

	batchRes, err := repoSvc.BatchAddRepos(BatchAddReposRequest{
		FullNames: []string{"acme/dragon", "maintainer/sidekick", "maintainer/sidekick"},
	})
	if err != nil || len(batchRes.Added) != 1 || len(batchRes.Skipped) != 1 || batchRes.Added[0].FullName != "maintainer/sidekick" {
		t.Fatalf("BatchAddRepos unexpected: %+v err=%v", batchRes, err)
	}

	toggled, err := repoSvc.SetRepoEnabled(added.ID, false)
	if err != nil || toggled.Enabled {
		t.Fatalf("SetRepoEnabled(false) unexpected: %+v err=%v", toggled, err)
	}
	toggledOn, err := repoSvc.SetRepoEnabled(added.ID, true)
	if err != nil || !toggledOn.Enabled {
		t.Fatalf("SetRepoEnabled(true) unexpected: %+v err=%v", toggledOn, err)
	}

	health, err := repoSvc.CheckRepoHealth(added.ID)
	if err != nil || !health.OK || !health.CanPush {
		t.Fatalf("CheckRepoHealth unexpected: %+v err=%v", health, err)
	}

	// Verify DeleteRepoWithOptions cleans local mirror cache and sibling worktrees while preserving localPath.
	sidekickID := batchRes.Added[0].ID
	mirrorPath := core.GitMgr.MirrorPath("maintainer/sidekick")
	if err := os.MkdirAll(mirrorPath, 0o755); err != nil {
		t.Fatalf("MkdirAll mirrorPath failed: %v", err)
	}
	if err := repoSvc.DeleteRepoWithOptions(sidekickID, true); err != nil {
		t.Fatalf("DeleteRepoWithOptions failed: %v", err)
	}
	if _, err := os.Stat(mirrorPath); !os.IsNotExist(err) {
		t.Fatalf("expected mirrorPath %q deleted, stat err=%v", mirrorPath, err)
	}

	// Workspace configuration tests: DefaultWorkspaceRoot, collision fallback, UpdateRepo validation, CloneRepoWorkspace, and safe cleanup.
	wsRoot := filepath.Join(tmp, "workspaces")
	s.DefaultWorkspaceRoot = wsRoot
	if _, err := settingsSvc.SaveSettings(s); err != nil {
		t.Fatalf("SaveSettings(DefaultWorkspaceRoot) failed: %v", err)
	}

	batchWS, err := repoSvc.BatchAddRepos(BatchAddReposRequest{
		FullNames: []string{"org1/shared", "org2/shared"},
	})
	if err != nil || len(batchWS.Added) != 2 {
		t.Fatalf("BatchAddRepos collision test unexpected: %+v err=%v", batchWS, err)
	}
	if batchWS.Added[0].LocalPath != filepath.Join(wsRoot, "shared") {
		t.Fatalf("expected first repo localPath %q, got %q", filepath.Join(wsRoot, "shared"), batchWS.Added[0].LocalPath)
	}
	if batchWS.Added[1].LocalPath != filepath.Join(wsRoot, "org2__shared") {
		t.Fatalf("expected colliding repo localPath %q, got %q", filepath.Join(wsRoot, "org2__shared"), batchWS.Added[1].LocalPath)
	}

	// Duplicate LocalPath in UpdateRepo is rejected.
	dupTry := batchWS.Added[1]
	dupTry.LocalPath = batchWS.Added[0].LocalPath
	if _, err := repoSvc.UpdateRepo(dupTry); err == nil {
		t.Fatalf("expected duplicate LocalPath rejection in UpdateRepo")
	}

	// Non-empty non-git dir is rejected in UpdateRepo.
	nonGitDir := filepath.Join(tmp, "non-git-dir")
	_ = os.MkdirAll(nonGitDir, 0o755)
	_ = os.WriteFile(filepath.Join(nonGitDir, "file.txt"), []byte("x"), 0o644)
	badTry := batchWS.Added[0]
	badTry.LocalPath = nonGitDir
	if _, err := repoSvc.UpdateRepo(badTry); err == nil {
		t.Fatalf("expected non-empty non-git dir rejection in UpdateRepo")
	}

	// CheckRepoHealth reports workspace status.
	healthWS, err := repoSvc.CheckRepoHealth(batchWS.Added[0].ID)
	if err != nil || healthWS.WorkspaceStatus != gitops.WorkspaceStatusPendingClone || healthWS.LocalPath != batchWS.Added[0].LocalPath {
		t.Fatalf("expected pending_clone workspace status in CheckRepoHealth, got %+v err=%v", healthWS, err)
	}

	// Prepare a local bare git upstream so CloneRepoWorkspace executes end-to-end offline.
	bareUpstream := filepath.Join(tmp, "shared-upstream.git")
	seedDir := filepath.Join(tmp, "shared-seed")
	runGitTest := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		gitops.ConfigureHiddenCommand(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}
	_ = os.MkdirAll(seedDir, 0o755)
	runGitTest(tmp, "init", "--bare", bareUpstream)
	runGitTest(seedDir, "init", "-b", "main")
	_ = os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("hello"), 0o644)
	runGitTest(seedDir, "add", ".")
	runGitTest(seedDir, "-c", "user.name=T", "-c", "user.email=t@e.com", "commit", "-m", "init")
	runGitTest(seedDir, "remote", "add", "origin", bareUpstream)
	runGitTest(seedDir, "push", "-u", "origin", "main")
	runGitTest(bareUpstream, "symbolic-ref", "HEAD", "refs/heads/main")
	core.Provider.(*fakeProvider).cloneURLs = map[string]string{"org1/shared": bareUpstream}

	clonedWS, err := repoSvc.CloneRepoWorkspace(batchWS.Added[0].ID, "")
	if err != nil || clonedWS.Status != gitops.WorkspaceStatusReady || !clonedWS.IsGitRepo {
		t.Fatalf("CloneRepoWorkspace failed: %+v err=%v", clonedWS, err)
	}

	// Create a temporary sibling worktree and verify DeleteRepoWithOptions(..., true) deletes only sibling worktrees, NOT LocalPath.
	wtRoot := gitops.RepoWorktreesRoot(batchWS.Added[0].LocalPath)
	wtSample := filepath.Join(wtRoot, "issue-1-abc123")
	if err := core.GitMgr.CreateWorktreeFromLocal(core.BaseCtx, batchWS.Added[0].LocalPath, "main", "auto/issue-1-abc123", wtSample); err != nil {
		t.Fatalf("CreateWorktreeFromLocal failed: %v", err)
	}
	if err := repoSvc.DeleteRepoWithOptions(batchWS.Added[0].ID, true); err != nil {
		t.Fatalf("DeleteRepoWithOptions(org1/shared) failed: %v", err)
	}
	if _, err := os.Stat(wtSample); !os.IsNotExist(err) {
		t.Fatalf("expected sibling worktree removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(batchWS.Added[0].LocalPath, ".git")); err != nil {
		t.Fatalf("expected user local repo %q preserved on delete, err=%v", batchWS.Added[0].LocalPath, err)
	}
	// Clean up org2/shared so ConnectedRepos count below remains 1.
	if err := repoSvc.DeleteRepoWithOptions(batchWS.Added[1].ID, false); err != nil {
		t.Fatalf("DeleteRepoWithOptions(org2/shared) failed: %v", err)
	}
	// Restore DefaultWorkspaceRoot empty check consistency (no-op for remaining test).
	_ = strings.TrimSpace(wsRoot)

	// Verify DisconnectGitHub stores authMethod="none" and clears PAT.
	_ = core.Secrets.Set(secrets.KeyGitHubToken, "tok-abc")
	if _, err := settingsSvc.DisconnectGitHub(); err != nil {
		t.Fatalf("DisconnectGitHub failed: %v", err)
	}
	afterDisc, err := settingsSvc.GetSettings()
	if err != nil || afterDisc.AuthMethod != "none" || core.Secrets.Has(secrets.KeyGitHubToken) {
		t.Fatalf("expected disconnected state, got settings=%+v hasToken=%v err=%v",
			afterDisc, core.Secrets.Has(secrets.KeyGitHubToken), err)
	}

	dashSvc := NewDashboardService(core)
	ov, err := dashSvc.GetOverview()
	if err != nil || ov.Stats.ConnectedRepos != 1 {
		t.Fatalf("GetOverview unexpected: %+v err=%v", ov, err)
	}

	// Ensure SafeNotificationService wrapper returns valid service name and doesn't crash on nil app.
	safeNotif := NewSafeNotificationService(core)
	if safeNotif.ServiceName() == "" {
		t.Fatalf("empty service name")
	}

	// Test AgentService Magpie extensions: inline model update, provider key, profile apply/save, route simulator.
	agSvc := NewAgentService(core)
	if err := agSvc.SaveProviderAPIKey("moonshot", "sk-kimi-123"); err != nil {
		t.Fatalf("SaveProviderAPIKey failed: %v", err)
	}
	provs, err := agSvc.ListProviders()
	if err != nil || len(provs) == 0 {
		t.Fatalf("ListProviders failed: %v", err)
	}
	updatedAg, err := agSvc.SetAgentModel("claude", "moonshot", "kimi-k2.5")
	if err != nil || updatedAg.ProviderID != "moonshot" || updatedAg.Model != "kimi-k2.5" {
		t.Fatalf("SetAgentModel unexpected: %+v err=%v", updatedAg, err)
	}
	savedProf, err := agSvc.SaveCurrentAsProfile("My Custom", "Kimi powered", "#F59E0B")
	if err != nil || !savedProf.IsActive {
		t.Fatalf("SaveCurrentAsProfile unexpected: %+v err=%v", savedProf, err)
	}
	applied, err := agSvc.ApplyWorkspaceProfile("focus")
	if err != nil || applied.RoutingConfig.Strategy != "quality_first" {
		t.Fatalf("ApplyWorkspaceProfile unexpected: %+v err=%v", applied, err)
	}
	sim, err := agSvc.SimulateRoute(SimulateRouteRequest{
		RepoID: added.ID,
		Title:  "feat: add distributed cache",
		Labels: []string{"enhancement"},
	})
	if err != nil || sim.PrimaryAgent == "" || len(sim.StageModels) != 7 {
		t.Fatalf("SimulateRoute unexpected: %+v err=%v", sim, err)
	}

	snap, err := agSvc.GetWorkspaceSnapshot()
	if err != nil {
		t.Fatalf("GetWorkspaceSnapshot failed: %v", err)
	}
	if len(snap.Agents) == 0 || len(snap.Providers) == 0 || len(snap.Models) == 0 || len(snap.Profiles) == 0 || len(snap.Templates) == 0 || len(snap.Stages) != 7 || snap.RoutingConfig.Strategy == "" {
		t.Fatalf("GetWorkspaceSnapshot returned incomplete data: agents=%d providers=%d models=%d profiles=%d templates=%d stages=%d strategy=%q",
			len(snap.Agents), len(snap.Providers), len(snap.Models), len(snap.Profiles), len(snap.Templates), len(snap.Stages), snap.RoutingConfig.Strategy)
	}

	// Verify built-in Budget & Focus profiles preserve native CLI defaults when no OpenAI/DeepSeek keys exist.
	if _, err := agSvc.ApplyWorkspaceProfile("budget"); err != nil {
		t.Fatalf("ApplyWorkspaceProfile(budget) failed: %v", err)
	}
	codexAfterBudget, err := core.DB.GetAgentProfile("codex")
	if err != nil || codexAfterBudget.ProviderID != "" || codexAfterBudget.Model != "" {
		t.Fatalf("expected codex to preserve native default under budget without keys, got %+v err=%v", codexAfterBudget, err)
	}
	if _, err := agSvc.ApplyWorkspaceProfile("focus"); err != nil {
		t.Fatalf("ApplyWorkspaceProfile(focus) failed: %v", err)
	}
	codexAfterFocus, err := core.DB.GetAgentProfile("codex")
	if err != nil || codexAfterFocus.ProviderID != "" || codexAfterFocus.Model != "" {
		t.Fatalf("expected codex to preserve native default under focus without keys, got %+v err=%v", codexAfterFocus, err)
	}

	// Verify TestAgent returns helpful install hint for an unconfigured CLI.
	if _, err := agSvc.SaveAgent(store.AgentProfile{
		ID:      "missing-cli",
		Name:    "Missing CodeBuddy",
		Kind:    "codebuddy",
		Command: "missing-nonexistent-binary-xyz",
		Enabled: true,
	}); err != nil {
		t.Fatalf("SaveAgent custom failed: %v", err)
	}
	testRes, err := agSvc.TestAgent("missing-cli")
	if err != nil {
		t.Fatalf("TestAgent failed: %v", err)
	}
	if testRes.OK || testRes.Detected || testRes.Hint == "" {
		t.Fatalf("expected missing-cli TestAgent to report undetected with install hint, got %+v", testRes)
	}
}
