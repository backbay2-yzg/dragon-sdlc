package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection and provides persistence for Dragon.
type DB struct {
	sql *sql.DB
}

// Open initializes a SQLite database at dsn (file path or ":memory:"),
// configures WAL mode and foreign keys, applies schema migrations, and seeds defaults.
func Open(dsn string) (*DB, error) {
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite handles concurrent writers best with serialized writes on a single pool connection in WAL mode.
	conn.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA synchronous=NORMAL;",
	}
	for _, p := range pragmas {
		if _, err := conn.Exec(p); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}

	db := &DB{sql: conn}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := db.seedDefaults(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("seed defaults: %w", err)
	}
	return db, nil
}

// Close closes the underlying SQLite database connection.
func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

func (db *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS repositories (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL DEFAULT 'github',
			owner TEXT NOT NULL,
			name TEXT NOT NULL,
			full_name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			private INTEGER NOT NULL DEFAULT 0,
			default_branch TEXT NOT NULL DEFAULT 'main',
			local_path TEXT NOT NULL DEFAULT '',
			poll_interval_sec INTEGER NOT NULL DEFAULT 120,
			trigger_label TEXT NOT NULL DEFAULT 'dragon-auto',
			autonomy_mode TEXT NOT NULL DEFAULT 'template',
			default_template_id TEXT NOT NULL DEFAULT 'auto',
			release_mode TEXT NOT NULL DEFAULT 'batch',
			merge_strategy TEXT NOT NULL DEFAULT 'squash',
			build_cmd TEXT NOT NULL DEFAULT '',
			test_cmd TEXT NOT NULL DEFAULT '',
			lint_cmd TEXT NOT NULL DEFAULT '',
			max_test_retries INTEGER NOT NULL DEFAULT 2,
			max_ci_retries INTEGER NOT NULL DEFAULT 2,
			sync_labels INTEGER NOT NULL DEFAULT 0,
			sync_comments INTEGER NOT NULL DEFAULT 0,
			draft_clarifications INTEGER NOT NULL DEFAULT 0,
			stage_gates_json TEXT NOT NULL DEFAULT '{}',
			stage_agents_json TEXT NOT NULL DEFAULT '{}',
			custom_prompts_json TEXT NOT NULL DEFAULT '{}',
			etag TEXT NOT NULL DEFAULT '',
			last_synced_at TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS issues (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			number INTEGER NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT 'open',
			author TEXT NOT NULL DEFAULT '',
			labels_json TEXT NOT NULL DEFAULT '[]',
			html_url TEXT NOT NULL DEFAULT '',
			trigger_matched INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'backlog',
			updated_at TEXT NOT NULL DEFAULT '',
			synced_at TEXT NOT NULL DEFAULT '',
			UNIQUE(repo_id, number),
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS pipeline_templates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			stages_json TEXT NOT NULL,
			default_gates_json TEXT NOT NULL DEFAULT '{}',
			is_builtin INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS model_providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			protocol TEXT NOT NULL DEFAULT 'openai_compatible',
			base_url TEXT NOT NULL DEFAULT '',
			models_json TEXT NOT NULL DEFAULT '[]',
			enabled INTEGER NOT NULL DEFAULT 1,
			is_builtin INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 100
		);`,
		`CREATE TABLE IF NOT EXISTS workspace_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			accent_color TEXT NOT NULL DEFAULT '#0F6CBD',
			routing_strategy TEXT NOT NULL DEFAULT 'rule_ai',
			agent_bindings_json TEXT NOT NULL DEFAULT '{}',
			stage_routing_json TEXT NOT NULL DEFAULT '{}',
			is_active INTEGER NOT NULL DEFAULT 0,
			is_builtin INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS agent_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			command TEXT NOT NULL,
			args_template TEXT NOT NULL DEFAULT '',
			provider_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			subtitle TEXT NOT NULL DEFAULT '',
			tiers_json TEXT NOT NULL DEFAULT '{}',
			supports_tiers INTEGER NOT NULL DEFAULT 0,
			primary_group INTEGER NOT NULL DEFAULT 1,
			sort_order INTEGER NOT NULL DEFAULT 100,
			permission_mode TEXT NOT NULL DEFAULT 'acceptEdits',
			extra_env_json TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			detected INTEGER NOT NULL DEFAULT 0,
			detected_path TEXT NOT NULL DEFAULT '',
			detected_version TEXT NOT NULL DEFAULT '',
			capabilities_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE TABLE IF NOT EXISTS routing_rules (
			id TEXT PRIMARY KEY,
			priority INTEGER NOT NULL DEFAULT 100,
			name TEXT NOT NULL,
			repo_id TEXT NOT NULL DEFAULT '',
			label_match TEXT NOT NULL DEFAULT '',
			title_regex TEXT NOT NULL DEFAULT '',
			complexity_hint TEXT NOT NULL DEFAULT 'auto',
			template_id TEXT NOT NULL DEFAULT 'full_feature',
			agent_id TEXT NOT NULL DEFAULT '',
			model_ref TEXT NOT NULL DEFAULT '',
			stage_overrides_json TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1
		);`,
		`CREATE TABLE IF NOT EXISTS pipeline_runs (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			issue_id TEXT NOT NULL,
			issue_number INTEGER NOT NULL,
			issue_title TEXT NOT NULL,
			template_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'queued',
			current_stage TEXT NOT NULL DEFAULT '',
			routed_agent_id TEXT NOT NULL DEFAULT '',
			routing_reason TEXT NOT NULL DEFAULT '',
			branch_name TEXT NOT NULL DEFAULT '',
			worktree_path TEXT NOT NULL DEFAULT '',
			pr_number INTEGER NOT NULL DEFAULT 0,
			pr_url TEXT NOT NULL DEFAULT '',
			ci_status TEXT NOT NULL DEFAULT 'none',
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS stage_executions (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			stage_key TEXT NOT NULL,
			stage_name TEXT NOT NULL,
			order_index INTEGER NOT NULL,
			agent_id TEXT NOT NULL DEFAULT '',
			model_ref TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			attempt INTEGER NOT NULL DEFAULT 0,
			summary TEXT NOT NULL DEFAULT '',
			artifact_path TEXT NOT NULL DEFAULT '',
			feedback TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '',
			UNIQUE(run_id, stage_key),
			FOREIGN KEY(run_id) REFERENCES pipeline_runs(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS run_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			stage_key TEXT NOT NULL,
			stream TEXT NOT NULL DEFAULT 'stdout',
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES pipeline_runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_run_logs_run_id ON run_logs(run_id, id);`,
		`CREATE TABLE IF NOT EXISTS release_records (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			tag_name TEXT NOT NULL,
			prev_tag TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL,
			bump_type TEXT NOT NULL DEFAULT 'patch',
			changelog TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL DEFAULT 'batch',
			status TEXT NOT NULL DEFAULT 'draft',
			html_url TEXT NOT NULL DEFAULT '',
			actions_status TEXT NOT NULL DEFAULT 'none',
			actions_url TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			published_at TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS release_items (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			run_id TEXT NOT NULL DEFAULT '',
			pr_number INTEGER NOT NULL,
			pr_title TEXT NOT NULL,
			commit_type TEXT NOT NULL DEFAULT 'chore',
			commit_scope TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			breaking INTEGER NOT NULL DEFAULT 0,
			merged_at TEXT NOT NULL,
			release_id TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(repo_id) REFERENCES repositories(id) ON DELETE CASCADE
		);`,
	}

	for _, s := range stmts {
		if _, err := db.sql.Exec(s); err != nil {
			return err
		}
	}

	// Idempotent column migrations for pre-existing databases.
	migrations := []struct {
		table string
		col   string
		def   string
	}{
		{"agent_profiles", "provider_id", "TEXT NOT NULL DEFAULT ''"},
		{"agent_profiles", "subtitle", "TEXT NOT NULL DEFAULT ''"},
		{"agent_profiles", "tiers_json", "TEXT NOT NULL DEFAULT '{}'"},
		{"agent_profiles", "supports_tiers", "INTEGER NOT NULL DEFAULT 0"},
		{"agent_profiles", "primary_group", "INTEGER NOT NULL DEFAULT 1"},
		{"agent_profiles", "sort_order", "INTEGER NOT NULL DEFAULT 100"},
		{"routing_rules", "model_ref", "TEXT NOT NULL DEFAULT ''"},
		{"stage_executions", "model_ref", "TEXT NOT NULL DEFAULT ''"},
		{"repositories", "description", "TEXT NOT NULL DEFAULT ''"},
		{"repositories", "private", "INTEGER NOT NULL DEFAULT 0"},
		{"repositories", "local_path", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, m := range migrations {
		if err := db.ensureColumn(m.table, m.col, m.def); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) ensureColumn(table, col, def string) error {
	rows, err := db.sql.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == col {
			return nil
		}
	}
	_, err = db.sql.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col, def))
	return err
}

func (db *DB) seedDefaults() error {
	templates := []PipelineTemplate{
		{
			ID:               "quick_bugfix",
			Name:             "简单修复 (Quick Fix)",
			Description:      "4 阶段快车道，适用于文案、拼写、单点小 Bug 快速修复：编码实现 → 测试验证 → 提交 PR 与 CI → 合并发布",
			StagesJSON:       mustJSON([]string{StageImplementation, StageVerification, StagePRCI, StageMergeRelease}),
			DefaultGatesJSON: mustJSON(map[string]bool{StageMergeRelease: true}),
			IsBuiltin:        true,
		},
		{
			ID:               "complex_fix",
			Name:             "复杂修复 (Complex Fix)",
			Description:      "完整 7 阶段深度根因排查，适用于跨模块故障、并发竞态、性能或安全修复",
			StagesJSON:       mustJSON([]string{StageRequirement, StageDesign, StageImplementation, StageVerification, StageAIReview, StagePRCI, StageMergeRelease}),
			DefaultGatesJSON: mustJSON(map[string]bool{StageDesign: true, StageMergeRelease: true}),
			IsBuiltin:        true,
		},
		{
			ID:               "full_feature",
			Name:             "完整功能开发 (Full Feature)",
			Description:      "完整 7 阶段标准研发流：需求分析 → 方案设计 → 编码实现 → 测试验证 → AI 代码审查 → 提交 PR 与 CI → 合并发布",
			StagesJSON:       mustJSON([]string{StageRequirement, StageDesign, StageImplementation, StageVerification, StageAIReview, StagePRCI, StageMergeRelease}),
			DefaultGatesJSON: mustJSON(map[string]bool{StageMergeRelease: true}),
			IsBuiltin:        true,
		},
		{
			ID:               "docs_chore",
			Name:             "文档与杂项维护 (Docs / Chore)",
			Description:      "轻量文档与维护流：编码实现 → AI 代码审查 → 提交 PR 与 CI → 合并发布",
			StagesJSON:       mustJSON([]string{StageImplementation, StageAIReview, StagePRCI, StageMergeRelease}),
			DefaultGatesJSON: mustJSON(map[string]bool{StageMergeRelease: true}),
			IsBuiltin:        true,
		},
	}

	for _, tpl := range templates {
		if _, err := db.sql.Exec(`
			INSERT OR IGNORE INTO pipeline_templates (id, name, description, stages_json, default_gates_json, is_builtin)
			VALUES (?, ?, ?, ?, ?, ?)`,
			tpl.ID, tpl.Name, tpl.Description, tpl.StagesJSON, tpl.DefaultGatesJSON, boolToInt(tpl.IsBuiltin),
		); err != nil {
			return err
		}
	}

	// Smoothly upgrade unmodified legacy default template seeds without overwriting user customizations.
	legacyQuickStages := mustJSON([]string{StageRequirement, StageImplementation, StageVerification, StageAIReview, StagePRCI, StageMergeRelease})
	if _, err := db.sql.Exec(`
		UPDATE pipeline_templates
		SET name = ?, description = ?, stages_json = ?
		WHERE id = 'quick_bugfix' AND is_builtin = 1 AND stages_json = ?`,
		templates[0].Name, templates[0].Description, templates[0].StagesJSON, legacyQuickStages,
	); err != nil {
		return err
	}
	legacyDocsStages := mustJSON([]string{StageRequirement, StageImplementation, StageAIReview, StagePRCI, StageMergeRelease})
	if _, err := db.sql.Exec(`
		UPDATE pipeline_templates
		SET name = ?, description = ?, stages_json = ?
		WHERE id = 'docs_chore' AND is_builtin = 1 AND stages_json = ?`,
		templates[3].Name, templates[3].Description, templates[3].StagesJSON, legacyDocsStages,
	); err != nil {
		return err
	}

	providers := []ModelProvider{
		{
			ID:        "anthropic",
			Name:      "Anthropic",
			Protocol:  "anthropic",
			BaseURL:   "https://api.anthropic.com",
			Enabled:   true,
			IsBuiltin: true,
			SortOrder: 10,
			ModelsJSON: mustJSON([]ModelCatalogEntry{
				{ID: "claude-opus-4-6", Name: "Claude Opus 4.6", ProviderID: "anthropic", CostTier: "flagship", ContextK: 200, Tags: []string{"reasoning", "architecture"}},
				{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", ProviderID: "anthropic", CostTier: "balanced", ContextK: 200, Tags: []string{"coding", "fast"}},
				{ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5", ProviderID: "anthropic", CostTier: "budget", ContextK: 200, Tags: []string{"fast", "triage"}},
			}),
		},
		{
			ID:        "openai",
			Name:      "OpenAI",
			Protocol:  "openai_compatible",
			BaseURL:   "https://api.openai.com/v1",
			Enabled:   true,
			IsBuiltin: true,
			SortOrder: 20,
			ModelsJSON: mustJSON([]ModelCatalogEntry{
				{ID: "gpt-5.4", Name: "GPT-5.4", ProviderID: "openai", CostTier: "flagship", ContextK: 400, Tags: []string{"reasoning", "coding"}},
				{ID: "gpt-5.3-codex", Name: "GPT-5.3 Codex", ProviderID: "openai", CostTier: "balanced", ContextK: 256, Tags: []string{"coding", "cli"}},
				{ID: "o4-mini", Name: "o4-mini", ProviderID: "openai", CostTier: "budget", ContextK: 200, Tags: []string{"fast", "tests"}},
			}),
		},
		{
			ID:        "google",
			Name:      "Google Gemini",
			Protocol:  "google",
			BaseURL:   "https://generativelanguage.googleapis.com/v1beta",
			Enabled:   true,
			IsBuiltin: true,
			SortOrder: 30,
			ModelsJSON: mustJSON([]ModelCatalogEntry{
				{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", ProviderID: "google", CostTier: "flagship", ContextK: 1000, Tags: []string{"long-context", "review"}},
				{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", ProviderID: "google", CostTier: "budget", ContextK: 1000, Tags: []string{"fast", "routing"}},
			}),
		},
		{
			ID:        "deepseek",
			Name:      "DeepSeek",
			Protocol:  "openai_compatible",
			BaseURL:   "https://api.deepseek.com/v1",
			Enabled:   true,
			IsBuiltin: true,
			SortOrder: 40,
			ModelsJSON: mustJSON([]ModelCatalogEntry{
				{ID: "deepseek-chat", Name: "DeepSeek V3.2", ProviderID: "deepseek", CostTier: "budget", ContextK: 128, Tags: []string{"coding", "cost-effective"}},
				{ID: "deepseek-reasoner", Name: "DeepSeek R1", ProviderID: "deepseek", CostTier: "balanced", ContextK: 128, Tags: []string{"reasoning"}},
			}),
		},
		{
			ID:        "moonshot",
			Name:      "Moonshot / Kimi",
			Protocol:  "anthropic",
			BaseURL:   "https://api.moonshot.cn/anthropic",
			Enabled:   true,
			IsBuiltin: true,
			SortOrder: 50,
			ModelsJSON: mustJSON([]ModelCatalogEntry{
				{ID: "kimi-k2.5", Name: "Kimi K2.5", ProviderID: "moonshot", CostTier: "balanced", ContextK: 256, Tags: []string{"coding", "agentic"}},
				{ID: "kimi-k2-thinking", Name: "Kimi K2 Thinking", ProviderID: "moonshot", CostTier: "flagship", ContextK: 256, Tags: []string{"reasoning"}},
			}),
		},
		{
			ID:        "zhipu",
			Name:      "Zhipu GLM",
			Protocol:  "openai_compatible",
			BaseURL:   "https://open.bigmodel.cn/api/paas/v4",
			Enabled:   true,
			IsBuiltin: true,
			SortOrder: 60,
			ModelsJSON: mustJSON([]ModelCatalogEntry{
				{ID: "glm-4.7", Name: "GLM-4.7", ProviderID: "zhipu", CostTier: "balanced", ContextK: 128, Tags: []string{"coding"}},
				{ID: "glm-4.5-air", Name: "GLM-4.5 Air", ProviderID: "zhipu", CostTier: "budget", ContextK: 128, Tags: []string{"fast"}},
			}),
		},
		{
			ID:        "qwen",
			Name:      "Qwen (DashScope)",
			Protocol:  "openai_compatible",
			BaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Enabled:   true,
			IsBuiltin: true,
			SortOrder: 70,
			ModelsJSON: mustJSON([]ModelCatalogEntry{
				{ID: "qwen3-coder-plus", Name: "Qwen3 Coder Plus", ProviderID: "qwen", CostTier: "balanced", ContextK: 256, Tags: []string{"coding"}},
				{ID: "qwen3-max", Name: "Qwen3 Max", ProviderID: "qwen", CostTier: "flagship", ContextK: 256, Tags: []string{"reasoning"}},
			}),
		},
		{
			ID:        "minimax",
			Name:      "MiniMax",
			Protocol:  "anthropic",
			BaseURL:   "https://api.minimaxi.com/anthropic",
			Enabled:   true,
			IsBuiltin: true,
			SortOrder: 80,
			ModelsJSON: mustJSON([]ModelCatalogEntry{
				{ID: "MiniMax-M2.5", Name: "MiniMax-M2.5", ProviderID: "minimax", CostTier: "budget", ContextK: 200, Tags: []string{"coding", "fast"}},
			}),
		},
		{
			ID:        "openrouter",
			Name:      "OpenRouter",
			Protocol:  "openai_compatible",
			BaseURL:   "https://openrouter.ai/api/v1",
			Enabled:   true,
			IsBuiltin: true,
			SortOrder: 90,
			ModelsJSON: mustJSON([]ModelCatalogEntry{
				{ID: "openrouter/auto", Name: "OpenRouter Auto", ProviderID: "openrouter", CostTier: "balanced", ContextK: 200, Tags: []string{"auto"}},
				{ID: "anthropic/claude-sonnet-4.6", Name: "Claude Sonnet 4.6 (OR)", ProviderID: "openrouter", CostTier: "balanced", ContextK: 200, Tags: []string{"coding"}},
			}),
		},
	}

	for _, p := range providers {
		if _, err := db.sql.Exec(`
			INSERT OR IGNORE INTO model_providers (id, name, protocol, base_url, models_json, enabled, is_builtin, sort_order)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.Name, p.Protocol, p.BaseURL, p.ModelsJSON, boolToInt(p.Enabled), boolToInt(p.IsBuiltin), p.SortOrder,
		); err != nil {
			return err
		}
	}

	agents := []AgentProfile{
		{
			ID:               "claude",
			Name:             "Claude Code",
			Kind:             "claude",
			Command:          "claude",
			ArgsTemplate:     "-p --output-format stream-json --verbose",
			ProviderID:       "anthropic",
			Model:            "claude-sonnet-4-6",
			Subtitle:         "switch the opus · sonnet · haiku tiers in /model",
			TiersJSON:        mustJSON(map[string]string{"opus": "anthropic/claude-opus-4-6", "sonnet": "anthropic/claude-sonnet-4-6", "haiku": "anthropic/claude-haiku-4-5"}),
			SupportsTiers:    true,
			PrimaryGroup:     true,
			SortOrder:        10,
			PermissionMode:   "acceptEdits",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"complex-feature", "architecture", "refactoring"}, "supportsStreamJSON": true}),
		},
		{
			ID:               "codex",
			Name:             "OpenAI Codex CLI",
			Kind:             "codex",
			Command:          "codex",
			ArgsTemplate:     "exec --json -",
			ProviderID:       "",
			Model:            "",
			Subtitle:         "复用 Codex 桌面端 / CLI 原生登录，支持按需指定模型",
			TiersJSON:        "{}",
			PrimaryGroup:     true,
			SortOrder:        20,
			PermissionMode:   "workspace-write",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"fast-implementation", "bugfix", "testing"}, "supportsStreamJSON": true}),
		},
		{
			ID:               "qoder",
			Name:             "Qoder CLI",
			Kind:             "qoder",
			Command:          "qodercli",
			ArgsTemplate:     "-p -o stream-json",
			ProviderID:       "",
			Model:            "",
			Subtitle:         "复用 Qoder CLI 原生账号登录",
			TiersJSON:        "{}",
			PrimaryGroup:     true,
			SortOrder:        25,
			PermissionMode:   "accept_edits",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"complex-feature", "coding", "review"}, "supportsStreamJSON": true}),
		},
		{
			ID:               "workbuddy",
			Name:             "WorkBuddy CLI",
			Kind:             "workbuddy",
			Command:          "workbuddy",
			ArgsTemplate:     "-p --output-format stream-json",
			ProviderID:       "",
			Model:            "",
			Subtitle:         "自动发现 WorkBuddy 桌面端内嵌 CLI",
			TiersJSON:        "{}",
			PrimaryGroup:     true,
			SortOrder:        26,
			PermissionMode:   "acceptEdits",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"coding", "agentic", "bugfix"}, "supportsStreamJSON": true}),
		},
		{
			ID:               "codebuddy",
			Name:             "CodeBuddy CLI",
			Kind:             "codebuddy",
			Command:          "codebuddy",
			ArgsTemplate:     "-p --output-format stream-json",
			ProviderID:       "",
			Model:            "",
			Subtitle:         "腾讯 CodeBuddy Code 无头 CLI",
			TiersJSON:        "{}",
			PrimaryGroup:     true,
			SortOrder:        27,
			PermissionMode:   "acceptEdits",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"coding", "refactoring", "review"}, "supportsStreamJSON": true}),
		},
		{
			ID:               "gemini",
			Name:             "Gemini CLI",
			Kind:             "gemini",
			Command:          "gemini",
			ArgsTemplate:     "-p",
			ProviderID:       "google",
			Model:            "gemini-2.5-pro",
			Subtitle:         "starts on the model picked here",
			TiersJSON:        "{}",
			PrimaryGroup:     true,
			SortOrder:        30,
			PermissionMode:   "auto",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"spec-analysis", "code-review", "docs"}, "supportsStreamJSON": false}),
		},
		{
			ID:               "agy",
			Name:             "Antigravity CLI",
			Kind:             "agy",
			Command:          "agy",
			ArgsTemplate:     "-p --output-format stream-json",
			ProviderID:       "",
			Model:            "",
			Subtitle:         "复用 Antigravity CLI 原生登录，支持按需指定模型",
			TiersJSON:        "{}",
			PrimaryGroup:     true,
			SortOrder:        40,
			PermissionMode:   "auto",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"architecture", "full-stack", "review"}, "supportsStreamJSON": true}),
		},
		{
			ID:               "opencode",
			Name:             "OpenCode",
			Kind:             "opencode",
			Command:          "opencode",
			ArgsTemplate:     "run",
			ProviderID:       "qwen",
			Model:            "qwen3-coder-plus",
			Subtitle:         "models synced to OpenCode's /models",
			TiersJSON:        "{}",
			PrimaryGroup:     true,
			SortOrder:        50,
			PermissionMode:   "auto",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"multi-model", "coding", "review"}, "supportsStreamJSON": false}),
		},
		{
			ID:               "copilot",
			Name:             "GitHub Copilot",
			Kind:             "copilot",
			Command:          "copilot",
			ArgsTemplate:     "-p",
			ProviderID:       "",
			Model:            "",
			Subtitle:         "GitHub-native CLI workflow",
			TiersJSON:        "{}",
			PrimaryGroup:     true,
			SortOrder:        60,
			PermissionMode:   "auto",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"github-workflow", "quick-fix"}, "supportsStreamJSON": false}),
		},
		{
			ID:               "kimi",
			Name:             "Kimi Code",
			Kind:             "kimi",
			Command:          "kimi",
			ArgsTemplate:     "-p",
			ProviderID:       "moonshot",
			Model:            "kimi-k2.5",
			Subtitle:         "models synced to Kimi Code's /model",
			TiersJSON:        "{}",
			PrimaryGroup:     true,
			SortOrder:        70,
			PermissionMode:   "auto",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"coding", "agentic"}, "supportsStreamJSON": false}),
		},
		{
			ID:               "aider",
			Name:             "Aider",
			Kind:             "aider",
			Command:          "aider",
			ArgsTemplate:     "--yes-always --no-auto-commits",
			ProviderID:       "deepseek",
			Model:            "deepseek-chat",
			Subtitle:         "git-aware pair programmer",
			TiersJSON:        "{}",
			PrimaryGroup:     true,
			SortOrder:        80,
			PermissionMode:   "auto",
			ExtraEnvJSON:     "{}",
			Enabled:          true,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"git-diff", "refactoring"}, "supportsStreamJSON": false}),
		},
		{
			ID:               "cline",
			Name:             "Cline CLI",
			Kind:             "cline",
			Command:          "cline",
			ArgsTemplate:     "-y",
			ProviderID:       "anthropic",
			Model:            "claude-sonnet-4-6",
			Subtitle:         "starts on the model picked here",
			TiersJSON:        "{}",
			PrimaryGroup:     false,
			SortOrder:        110,
			PermissionMode:   "auto",
			ExtraEnvJSON:     "{}",
			Enabled:          false,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"autonomous-coding"}, "supportsStreamJSON": false}),
		},
		{
			ID:               "qwen_code",
			Name:             "Qwen Code",
			Kind:             "qwen_code",
			Command:          "qwen",
			ArgsTemplate:     "-p",
			ProviderID:       "qwen",
			Model:            "qwen3-coder-plus",
			Subtitle:         "starts on the model picked here",
			TiersJSON:        "{}",
			PrimaryGroup:     false,
			SortOrder:        120,
			PermissionMode:   "auto",
			ExtraEnvJSON:     "{}",
			Enabled:          false,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"coding", "docs"}, "supportsStreamJSON": false}),
		},
		{
			ID:               "goose",
			Name:             "Goose CLI",
			Kind:             "goose",
			Command:          "goose",
			ArgsTemplate:     "run",
			ProviderID:       "openai",
			Model:            "gpt-5.3-codex",
			Subtitle:         "extensible developer agent",
			TiersJSON:        "{}",
			PrimaryGroup:     false,
			SortOrder:        130,
			PermissionMode:   "auto",
			ExtraEnvJSON:     "{}",
			Enabled:          false,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"automation"}, "supportsStreamJSON": false}),
		},
		{
			ID:               "amp",
			Name:             "Amp CLI",
			Kind:             "amp",
			Command:          "amp",
			ArgsTemplate:     "-x",
			ProviderID:       "anthropic",
			Model:            "claude-sonnet-4-6",
			Subtitle:         "starts on the model picked here",
			TiersJSON:        "{}",
			PrimaryGroup:     false,
			SortOrder:        140,
			PermissionMode:   "auto",
			ExtraEnvJSON:     "{}",
			Enabled:          false,
			CapabilitiesJSON: mustJSON(map[string]any{"strengths": []string{"coding"}, "supportsStreamJSON": false}),
		},
	}

	for _, a := range agents {
		if _, err := db.sql.Exec(`
			INSERT OR IGNORE INTO agent_profiles (
				id, name, kind, command, args_template, provider_id, model, subtitle, tiers_json,
				supports_tiers, primary_group, sort_order, permission_mode, extra_env_json,
				enabled, detected, detected_path, detected_version, capabilities_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '', '', ?)`,
			a.ID, a.Name, a.Kind, a.Command, a.ArgsTemplate, a.ProviderID, a.Model, a.Subtitle, a.TiersJSON,
			boolToInt(a.SupportsTiers), boolToInt(a.PrimaryGroup), a.SortOrder, a.PermissionMode, a.ExtraEnvJSON,
			boolToInt(a.Enabled), a.CapabilitiesJSON,
		); err != nil {
			return err
		}
		// Backfill Magpie metadata on rows migrated from earlier schema where subtitle/sort_order were default.
		_, _ = db.sql.Exec(`
			UPDATE agent_profiles
			SET subtitle = CASE WHEN subtitle = '' THEN ? ELSE subtitle END,
				supports_tiers = CASE WHEN id = 'claude' THEN 1 ELSE supports_tiers END,
				tiers_json = CASE WHEN tiers_json = '{}' OR tiers_json = '' THEN ? ELSE tiers_json END,
				primary_group = ?,
				sort_order = CASE WHEN sort_order = 100 THEN ? ELSE sort_order END
			WHERE id = ?`,
			a.Subtitle, a.TiersJSON, boolToInt(a.PrimaryGroup), a.SortOrder, a.ID,
		)
	}

	// Seed built-in Workspace Profiles (Budget, Balanced, Focus).
	profiles := []WorkspaceProfile{
		{
			ID:              "budget",
			Name:            "Budget",
			Description:     "极致性价比：优先选用低延迟、低成本模型处理日常 Issue 与测试修复",
			AccentColor:     "#10B981",
			RoutingStrategy: "budget_first",
			AgentBindingsJSON: mustJSON(map[string]AgentBindingSnapshot{
				"claude":    {Enabled: true, ProviderID: "anthropic", Model: "claude-haiku-4-5", TiersJSON: `{"opus":"anthropic/claude-sonnet-4-6","sonnet":"anthropic/claude-haiku-4-5","haiku":"anthropic/claude-haiku-4-5"}`},
				"codex":     {Enabled: true, ProviderID: "", Model: ""},
				"qoder":     {Enabled: true, ProviderID: "", Model: ""},
				"workbuddy": {Enabled: true, ProviderID: "", Model: ""},
				"codebuddy": {Enabled: true, ProviderID: "", Model: ""},
				"agy":       {Enabled: true, ProviderID: "", Model: ""},
				"gemini":    {Enabled: true, ProviderID: "google", Model: "gemini-2.5-flash"},
				"opencode":  {Enabled: true, ProviderID: "deepseek", Model: "deepseek-chat"},
				"aider":     {Enabled: true, ProviderID: "deepseek", Model: "deepseek-chat"},
			}),
			StageRoutingJSON: mustJSON(map[string]StageRouteBinding{
				StageRequirement:    {Tier: "haiku"},
				StageDesign:         {Tier: "sonnet"},
				StageImplementation: {Tier: "haiku"},
				StageVerification:   {Tier: "haiku"},
				StageAIReview:       {AgentID: "gemini", ModelRef: "google/gemini-2.5-flash"},
			}),
			IsActive:  false,
			IsBuiltin: true,
			CreatedAt: NowRFC3339(),
		},
		{
			ID:              "balanced",
			Name:            "Balanced",
			Description:     "日常开发均衡配置：规则优先 + AI 路由，主力使用 Sonnet / Codex / Kimi",
			AccentColor:     "#0F6CBD",
			RoutingStrategy: "rule_ai",
			AgentBindingsJSON: mustJSON(map[string]AgentBindingSnapshot{
				"claude":    {Enabled: true, ProviderID: "anthropic", Model: "claude-sonnet-4-6", TiersJSON: `{"opus":"anthropic/claude-opus-4-6","sonnet":"anthropic/claude-sonnet-4-6","haiku":"anthropic/claude-haiku-4-5"}`},
				"codex":     {Enabled: true, ProviderID: "", Model: ""},
				"qoder":     {Enabled: true, ProviderID: "", Model: ""},
				"workbuddy": {Enabled: true, ProviderID: "", Model: ""},
				"codebuddy": {Enabled: true, ProviderID: "", Model: ""},
				"agy":       {Enabled: true, ProviderID: "", Model: ""},
				"gemini":    {Enabled: true, ProviderID: "google", Model: "gemini-2.5-pro"},
				"opencode":  {Enabled: true, ProviderID: "qwen", Model: "qwen3-coder-plus"},
				"kimi":      {Enabled: true, ProviderID: "moonshot", Model: "kimi-k2.5"},
			}),
			StageRoutingJSON: mustJSON(map[string]StageRouteBinding{
				StageRequirement:    {Tier: "sonnet"},
				StageDesign:         {Tier: "opus"},
				StageImplementation: {Tier: "sonnet"},
				StageAIReview:       {Tier: "opus"},
			}),
			IsActive:  true,
			IsBuiltin: true,
			CreatedAt: NowRFC3339(),
		},
		{
			ID:              "focus",
			Name:            "Focus",
			Description:     "旗舰深度推理：全阶段启用顶级推理模型（Opus / GPT-5.4 / Gemini 2.5 Pro）",
			AccentColor:     "#7C3AED",
			RoutingStrategy: "quality_first",
			AgentBindingsJSON: mustJSON(map[string]AgentBindingSnapshot{
				"claude":    {Enabled: true, ProviderID: "anthropic", Model: "claude-opus-4-6", TiersJSON: `{"opus":"anthropic/claude-opus-4-6","sonnet":"anthropic/claude-opus-4-6","haiku":"anthropic/claude-sonnet-4-6"}`},
				"codex":     {Enabled: true, ProviderID: "", Model: ""},
				"qoder":     {Enabled: true, ProviderID: "", Model: ""},
				"workbuddy": {Enabled: true, ProviderID: "", Model: ""},
				"codebuddy": {Enabled: true, ProviderID: "", Model: ""},
				"agy":       {Enabled: true, ProviderID: "", Model: ""},
				"gemini":    {Enabled: true, ProviderID: "google", Model: "gemini-2.5-pro"},
				"opencode":  {Enabled: true, ProviderID: "qwen", Model: "qwen3-max"},
			}),
			StageRoutingJSON: mustJSON(map[string]StageRouteBinding{
				StageRequirement:    {Tier: "opus"},
				StageDesign:         {Tier: "opus"},
				StageImplementation: {Tier: "opus"},
				StageVerification:   {Tier: "sonnet"},
				StageAIReview:       {AgentID: "gemini", ModelRef: "google/gemini-2.5-pro"},
			}),
			IsActive:  false,
			IsBuiltin: true,
			CreatedAt: NowRFC3339(),
		},
	}

	for _, wp := range profiles {
		if _, err := db.sql.Exec(`
			INSERT OR IGNORE INTO workspace_profiles (
				id, name, description, accent_color, routing_strategy,
				agent_bindings_json, stage_routing_json, is_active, is_builtin, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			wp.ID, wp.Name, wp.Description, wp.AccentColor, wp.RoutingStrategy,
			wp.AgentBindingsJSON, wp.StageRoutingJSON, boolToInt(wp.IsActive), boolToInt(wp.IsBuiltin), wp.CreatedAt,
		); err != nil {
			return err
		}
	}

	return nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
