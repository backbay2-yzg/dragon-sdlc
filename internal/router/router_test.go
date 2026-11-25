package router

import (
	"context"
	"testing"

	"dragon/internal/config"
	"dragon/internal/store"
)

func TestRulePriorityAndStageOverrides(t *testing.T) {
	r := NewRouter(nil)
	repo := store.Repository{
		ID:                "repo-1",
		FullName:          "acme/dragon",
		DefaultTemplateID: "full_feature",
		StageAgentsJSON:   `{"ai_review":"gemini"}`,
	}
	issue := store.Issue{
		ID:         "iss-1",
		RepoID:     "repo-1",
		Number:     101,
		Title:      "fix: memory leak in worker pool",
		LabelsJSON: `["bug","urgent"]`,
	}
	profiles := []store.AgentProfile{
		{ID: "claude", Name: "Claude", ProviderID: "anthropic", Model: "claude-sonnet-4-6", Enabled: true, Detected: true, SupportsTiers: true},
		{ID: "codex", Name: "Codex", ProviderID: "openai", Model: "gpt-5.3-codex", Enabled: true, Detected: true},
		{ID: "gemini", Name: "Gemini", ProviderID: "google", Model: "gemini-2.5-pro", Enabled: true, Detected: false},
	}
	rules := []store.RoutingRule{
		{
			ID:         "r-low",
			Priority:   200,
			Name:       "Fallback rule",
			LabelMatch: "bug",
			TemplateID: "full_feature",
			AgentID:    "claude",
			Enabled:    true,
		},
		{
			ID:                 "r-high",
			Priority:           10,
			Name:               "Bugfix fast lane",
			LabelMatch:         "bug",
			TitleRegex:         "leak",
			TemplateID:         "quick_bugfix",
			AgentID:            "codex",
			ModelRef:           "deepseek/deepseek-chat",
			StageOverridesJSON: `{"requirement":"claude"}`,
			Enabled:            true,
		},
	}

	rcfg := store.DefaultRoutingConfig()
	rcfg.StageBindings[store.StageDesign] = store.StageRouteBinding{AgentID: "claude", Tier: "opus"}

	dec, err := r.Route(context.Background(), RouteRequest{
		Repo:          repo,
		Issue:         issue,
		Rules:         rules,
		Profiles:      profiles,
		RoutingConfig: rcfg,
		Settings:      config.DefaultSettings(),
	})
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if dec.MatchedRuleID != "r-high" || dec.TemplateID != "quick_bugfix" || dec.PrimaryAgent != "codex" {
		t.Fatalf("unexpected routing decision: %+v", dec)
	}
	if dec.PrimaryModel != "deepseek/deepseek-chat" {
		t.Fatalf("expected PrimaryModel deepseek/deepseek-chat, got %q", dec.PrimaryModel)
	}
	if dec.StageAgents[store.StageDesign] != "claude" || dec.StageModels[store.StageDesign] != "opus" {
		t.Fatalf("expected stage design claude/opus, got %s/%s", dec.StageAgents[store.StageDesign], dec.StageModels[store.StageDesign])
	}
	if dec.StageAgents[store.StageRequirement] != "claude" {
		t.Fatalf("expected rule stage override claude for requirement, got %q", dec.StageAgents[store.StageRequirement])
	}
	if dec.StageAgents[store.StageAIReview] != "gemini" || dec.StageModels[store.StageAIReview] != "google/gemini-2.5-pro" {
		t.Fatalf("expected repo stage override gemini + google/gemini-2.5-pro, got %q / %q", dec.StageAgents[store.StageAIReview], dec.StageModels[store.StageAIReview])
	}
	if dec.StageAgents[store.StageImplementation] != "codex" {
		t.Fatalf("expected primary codex for implementation, got %q", dec.StageAgents[store.StageImplementation])
	}
}

func TestBudgetAndFocusStrategies(t *testing.T) {
	r := NewRouter(nil)
	profiles := []store.AgentProfile{
		{ID: "claude", Name: "Claude", ProviderID: "anthropic", Model: "claude-sonnet-4-6", Enabled: true, Detected: true, SupportsTiers: true},
		{ID: "aider", Name: "Aider", ProviderID: "deepseek", Model: "deepseek-chat", Enabled: true, Detected: true},
	}
	providers := []store.ModelProvider{
		{
			ID: "deepseek", Enabled: true, HasKey: true,
			ModelsJSON: `[{"id":"deepseek-chat","name":"DeepSeek V3.2","providerId":"deepseek","costTier":"budget"}]`,
		},
	}
	budgetCfg := store.DefaultRoutingConfig()
	budgetCfg.Strategy = "budget_first"
	dec, err := r.Route(context.Background(), RouteRequest{
		Repo:          store.Repository{ID: "r1"},
		Issue:         store.Issue{Number: 1, Title: "fix: typo"},
		Profiles:      profiles,
		Providers:     providers,
		RoutingConfig: budgetCfg,
	})
	if err != nil || dec.PrimaryAgent != "aider" || dec.PrimaryModel != "deepseek/deepseek-chat" {
		t.Fatalf("unexpected budget route: %+v err=%v", dec, err)
	}

	focusCfg := store.DefaultRoutingConfig()
	focusCfg.Strategy = "quality_first"
	dec2, err := r.Route(context.Background(), RouteRequest{
		Repo:          store.Repository{ID: "r1"},
		Issue:         store.Issue{Number: 2, Title: "feat: architecture"},
		Profiles:      profiles,
		Providers:     providers,
		RoutingConfig: focusCfg,
	})
	if err != nil || dec2.PrimaryAgent != "claude" || dec2.PrimaryModel != "opus" {
		t.Fatalf("unexpected focus route: %+v err=%v", dec2, err)
	}
}

func TestHeuristicFallbackRouting(t *testing.T) {
	r := NewRouter(nil)
	profiles := []store.AgentProfile{
		{ID: "claude", Enabled: true, Detected: false},
		{ID: "codex", Enabled: true, Detected: true},
	}

	dec, err := r.Route(context.Background(), RouteRequest{
		Repo: store.Repository{ID: "repo-1", DefaultTemplateID: "auto"},
		Issue: store.Issue{
			Number:     5,
			Title:      "docs: clarify installation steps",
			LabelsJSON: `[]`,
		},
		Profiles: profiles,
	})
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if dec.TemplateID != "docs_chore" || dec.PrimaryAgent != "codex" {
		t.Fatalf("unexpected heuristic decision: %+v", dec)
	}

	simpleFix, err := r.Route(context.Background(), RouteRequest{
		Repo: store.Repository{ID: "repo-1", DefaultTemplateID: "auto"},
		Issue: store.Issue{
			Number:     6,
			Title:      "fix: wrong button padding in header",
			LabelsJSON: `["bug"]`,
		},
		Profiles: profiles,
	})
	if err != nil || simpleFix.TemplateID != "quick_bugfix" {
		t.Fatalf("expected quick_bugfix for simple bug, got %+v err=%v", simpleFix, err)
	}

	complexFix, err := r.Route(context.Background(), RouteRequest{
		Repo: store.Repository{ID: "repo-1", DefaultTemplateID: "auto"},
		Issue: store.Issue{
			Number:     7,
			Title:      "fix: race condition and deadlock in worker pool",
			Body:       "Occurs under concurrent shutdown.",
			LabelsJSON: `["bug","critical"]`,
		},
		Profiles: profiles,
	})
	if err != nil || complexFix.TemplateID != "complex_fix" {
		t.Fatalf("expected complex_fix for complex bug, got %+v err=%v", complexFix, err)
	}

	feat, err := r.Route(context.Background(), RouteRequest{
		Repo: store.Repository{ID: "repo-1", DefaultTemplateID: "auto"},
		Issue: store.Issue{
			Number:     8,
			Title:      "feat: add multi-repo support",
			LabelsJSON: `["enhancement"]`,
		},
		Profiles: profiles,
	})
	if err != nil || feat.TemplateID != "full_feature" {
		t.Fatalf("expected full_feature for feature issue, got %+v err=%v", feat, err)
	}

	locked, err := r.Route(context.Background(), RouteRequest{
		Repo: store.Repository{ID: "repo-1", DefaultTemplateID: "complex_fix"},
		Issue: store.Issue{
			Number:     9,
			Title:      "fix: typo in tooltip",
			LabelsJSON: `["bug"]`,
		},
		Profiles: profiles,
	})
	if err != nil || locked.TemplateID != "complex_fix" {
		t.Fatalf("expected locked template complex_fix to remain locked, got %+v err=%v", locked, err)
	}
}

func TestNativeFirstAgentsInRoutingPools(t *testing.T) {
	r := NewRouter(nil)
	profiles := []store.AgentProfile{
		{ID: "qoder", Name: "Qoder CLI", Kind: "qoder", Enabled: true, Detected: true},
		{ID: "workbuddy", Name: "WorkBuddy CLI", Kind: "workbuddy", Enabled: true, Detected: true},
		{ID: "codex", Name: "OpenAI Codex CLI", Kind: "codex", Enabled: true, Detected: true},
	}
	providers := []store.ModelProvider{
		{
			ID: "openai", Enabled: true, HasKey: false,
			ModelsJSON: `[{"id":"gpt-5.4","name":"GPT-5.4","providerId":"openai","costTier":"flagship"}]`,
		},
		{
			ID: "deepseek", Enabled: true, HasKey: false,
			ModelsJSON: `[{"id":"deepseek-chat","name":"DeepSeek V3.2","providerId":"deepseek","costTier":"budget"}]`,
		},
	}

	budgetCfg := store.DefaultRoutingConfig()
	budgetCfg.Strategy = "budget_first"
	decBudget, err := r.Route(context.Background(), RouteRequest{
		Repo:          store.Repository{ID: "r1"},
		Issue:         store.Issue{Number: 10, Title: "fix: minor lint"},
		Profiles:      profiles,
		Providers:     providers,
		RoutingConfig: budgetCfg,
	})
	if err != nil || decBudget.PrimaryAgent != "qoder" || decBudget.PrimaryModel != "" {
		t.Fatalf("expected budget_first to pick qoder with native default model, got %+v err=%v", decBudget, err)
	}

	focusCfg := store.DefaultRoutingConfig()
	focusCfg.Strategy = "quality_first"
	decFocus, err := r.Route(context.Background(), RouteRequest{
		Repo:          store.Repository{ID: "r1"},
		Issue:         store.Issue{Number: 11, Title: "feat: new engine"},
		Profiles:      profiles,
		Providers:     providers,
		RoutingConfig: focusCfg,
	})
	if err != nil || decFocus.PrimaryAgent != "codex" || decFocus.PrimaryModel != "" {
		t.Fatalf("expected quality_first to pick codex with native default model, got %+v err=%v", decFocus, err)
	}
}
