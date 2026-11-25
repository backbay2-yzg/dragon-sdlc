package store

import (
	"testing"

	"dragon/internal/config"
)

func TestStoreMigrationsAndCRUD(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	defer db.Close()

	// Verify built-in templates, providers, profiles, and agents seeded.
	tpls, err := db.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates failed: %v", err)
	}
	if len(tpls) < 4 || tpls[0].ID != "quick_bugfix" || tpls[1].ID != "complex_fix" {
		t.Fatalf("expected >= 4 seeded templates starting with quick_bugfix and complex_fix, got %+v", tpls)
	}
	if len(tpls[0].Stages()) != 4 {
		t.Fatalf("expected quick_bugfix to have 4 fast-lane stages, got %v", tpls[0].Stages())
	}

	providers, err := db.ListModelProviders()
	if err != nil || len(providers) < 8 {
		t.Fatalf("expected >= 8 seeded providers, got %d err=%v", len(providers), err)
	}

	profiles, err := db.ListWorkspaceProfiles()
	if err != nil || len(profiles) < 3 {
		t.Fatalf("expected >= 3 seeded workspace profiles, got %d err=%v", len(profiles), err)
	}

	agents, err := db.ListAgentProfiles()
	if err != nil {
		t.Fatalf("ListAgentProfiles failed: %v", err)
	}
	if len(agents) < 12 {
		t.Fatalf("expected >= 12 seeded agents, got %d", len(agents))
	}
	if agents[0].ID != "claude" || !agents[0].SupportsTiers {
		t.Fatalf("expected first featured agent claude with SupportsTiers=true, got %+v", agents[0])
	}

	// RoutingConfig roundtrip.
	rcfg, err := db.GetRoutingConfig()
	if err != nil || rcfg.Strategy != "rule_ai" {
		t.Fatalf("GetRoutingConfig failed: %+v err=%v", rcfg, err)
	}
	rcfg.Strategy = "focus"
	rcfg.RouterModelRef = "anthropic/claude-sonnet-4-6"
	rcfg.StageBindings[StageAIReview] = StageRouteBinding{AgentID: "gemini", ModelRef: "google/gemini-2.5-pro"}
	if _, err := db.SaveRoutingConfig(rcfg); err != nil {
		t.Fatalf("SaveRoutingConfig failed: %v", err)
	}
	gotRCfg, err := db.GetRoutingConfig()
	if err != nil || gotRCfg.StageBindings[StageAIReview].AgentID != "gemini" {
		t.Fatalf("GetRoutingConfig roundtrip failed: %+v err=%v", gotRCfg, err)
	}

	// Settings roundtrip.
	s, err := db.GetAppSettings()
	if err != nil {
		t.Fatalf("GetAppSettings failed: %v", err)
	}
	s.MaxConcurrency = 4
	s.BackdropMaterial = "acrylic"
	s.DefaultWorkspaceRoot = `D:\DragonWorkspaces\`
	saved, err := db.SaveAppSettings(s)
	if err != nil || saved.MaxConcurrency != 4 || saved.BackdropMaterial != "acrylic" || saved.DefaultWorkspaceRoot != `D:\DragonWorkspaces` {
		t.Fatalf("SaveAppSettings failed: %+v, err=%v", saved, err)
	}

	// Repo CRUD.
	repo := Repository{
		ID:          "repo-1",
		Owner:       "acme",
		Name:        "rocket",
		FullName:    "acme/rocket",
		Description: "Rocket engine",
		LocalPath:   `D:\DragonWorkspaces\rocket\`,
		Private:     true,
		Enabled:     true,
		ReleaseMode: "batch",
	}
	if err := db.UpsertRepository(repo); err != nil {
		t.Fatalf("UpsertRepository failed: %v", err)
	}
	gotRepo, err := db.GetRepository("repo-1")
	if err != nil || gotRepo.FullName != "acme/rocket" || gotRepo.DefaultBranch != "main" || !gotRepo.Private || gotRepo.Description != "Rocket engine" || gotRepo.LocalPath != `D:\DragonWorkspaces\rocket` {
		t.Fatalf("GetRepository unexpected: %+v, err=%v", gotRepo, err)
	}
	byName, err := db.GetRepositoryByFullName("ACME/Rocket")
	if err != nil || byName.ID != "repo-1" {
		t.Fatalf("GetRepositoryByFullName unexpected: %+v, err=%v", byName, err)
	}
	if err := db.SetRepositoryEnabled("repo-1", false); err != nil {
		t.Fatalf("SetRepositoryEnabled failed: %v", err)
	}
	pausedRepo, err := db.GetRepository("repo-1")
	if err != nil || pausedRepo.Enabled {
		t.Fatalf("expected repo disabled, got %+v, err=%v", pausedRepo, err)
	}
	if err := db.SetRepositoryEnabled("repo-1", true); err != nil {
		t.Fatalf("SetRepositoryEnabled(true) failed: %v", err)
	}

	// Issue upsert & status preservation.
	iss := Issue{
		ID:             "iss-1",
		RepoID:         "repo-1",
		Number:         42,
		Title:          "feat: support dark mode",
		Body:           "Please add dark mode",
		State:          "open",
		Author:         "alice",
		LabelsJSON:     `["enhancement","dragon-auto"]`,
		TriggerMatched: true,
		Status:         "backlog",
		UpdatedAt:      NowRFC3339(),
		SyncedAt:       NowRFC3339(),
	}
	if err := db.UpsertIssue(iss); err != nil {
		t.Fatalf("UpsertIssue failed: %v", err)
	}
	if err := db.UpdateIssueStatus("iss-1", "running"); err != nil {
		t.Fatalf("UpdateIssueStatus failed: %v", err)
	}
	// Re-syncing issue with status=backlog should keep active status 'running'.
	if err := db.UpsertIssue(iss); err != nil {
		t.Fatalf("UpsertIssue re-sync failed: %v", err)
	}
	gotIss, err := db.GetIssue("iss-1")
	if err != nil || gotIss.Status != "running" {
		t.Fatalf("expected issue status 'running', got %q (err=%v)", gotIss.Status, err)
	}

	// Pipeline run & stages & logs.
	run := PipelineRun{
		ID:            "run-1",
		RepoID:        "repo-1",
		IssueID:       "iss-1",
		IssueNumber:   42,
		IssueTitle:    iss.Title,
		TemplateID:    "full_feature",
		Status:        "running",
		CurrentStage:  StageRequirement,
		RoutedAgentID: "claude",
		BranchName:    "auto/issue-42",
		StartedAt:     NowRFC3339(),
	}
	stages := []StageExecution{
		{ID: "st-1", RunID: "run-1", StageKey: StageRequirement, StageName: "需求分析", OrderIndex: 1, AgentID: "claude", Status: "running"},
		{ID: "st-2", RunID: "run-1", StageKey: StageMergeRelease, StageName: "合并与发布", OrderIndex: 7, AgentID: "claude", Status: "pending"},
	}
	if err := db.CreatePipelineRun(run, stages); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}
	if _, err := db.AppendRunLog("run-1", StageRequirement, "stdout", "Analyzing issue #42"); err != nil {
		t.Fatalf("AppendRunLog failed: %v", err)
	}
	logs, err := db.ListRunLogs("run-1", 50)
	if err != nil || len(logs) != 1 {
		t.Fatalf("ListRunLogs unexpected: %d, err=%v", len(logs), err)
	}

	// Release item & release record.
	if err := db.InsertReleaseItem(ReleaseItem{
		ID:         "ri-1",
		RepoID:     "repo-1",
		RunID:      "run-1",
		PRNumber:   99,
		PRTitle:    "feat(ui): dark mode",
		CommitType: "feat",
		Summary:    "dark mode",
		MergedAt:   NowRFC3339(),
	}); err != nil {
		t.Fatalf("InsertReleaseItem failed: %v", err)
	}
	pending, err := db.ListPendingReleaseItems("repo-1")
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListPendingReleaseItems failed: len=%d err=%v", len(pending), err)
	}

	stats, err := db.GetDashboardStats()
	if err != nil || stats.ActiveRuns != 1 || stats.PendingReleaseItems != 1 {
		t.Fatalf("GetDashboardStats unexpected: %+v, err=%v", stats, err)
	}

	// ResolveStageGates sanity check & template-level gate resolution.
	gates := ResolveStageGates("semi_auto", `{"design":true}`)
	if !gates[StageMergeRelease] || !gates[StageDesign] || gates[StageRequirement] {
		t.Fatalf("unexpected resolved gates: %+v", gates)
	}
	tplGates := ResolveStageGatesWithTemplate("template", `{"design":true,"merge_release":true}`, `{"pr_ci":true}`)
	if !tplGates[StageDesign] || !tplGates[StageMergeRelease] || tplGates[StagePRCI] {
		t.Fatalf("expected template mode to obey template gates without stale repo custom override, got %+v", tplGates)
	}
	autoGates := ResolveStageGatesWithTemplate("full_auto", `{"design":true,"merge_release":true}`, `{"pr_ci":false}`)
	if autoGates[StageDesign] || autoGates[StageMergeRelease] {
		t.Fatalf("expected full_auto mode to disable template gates, got %+v", autoGates)
	}

	// Custom template CRUD & built-in delete guard.
	if err := db.DeleteTemplate("quick_bugfix"); err == nil {
		t.Fatalf("expected error deleting built-in template")
	}
	if err := db.UpsertTemplate(PipelineTemplate{
		ID:               "custom-flow",
		Name:             "Custom Flow",
		StagesJSON:       `["implementation","pr_ci"]`,
		DefaultGatesJSON: `{"pr_ci":true}`,
		IsBuiltin:        false,
	}); err != nil {
		t.Fatalf("UpsertTemplate custom failed: %v", err)
	}
	if err := db.DeleteTemplate("custom-flow"); err != nil {
		t.Fatalf("DeleteTemplate custom failed: %v", err)
	}

	_ = config.DefaultSettings()
}
