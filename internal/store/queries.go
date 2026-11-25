package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"dragon/internal/config"
)

var ErrNotFound = errors.New("record not found")

// --- App Settings ---

const settingsKey = "app_settings"

func (db *DB) GetAppSettings() (config.AppSettings, error) {
	var raw string
	err := db.sql.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, settingsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return config.DefaultSettings(), nil
	}
	if err != nil {
		return config.DefaultSettings(), err
	}
	s := config.DefaultSettings()
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return config.DefaultSettings(), err
	}
	return s.Normalize(), nil
}

func (db *DB) SaveAppSettings(s config.AppSettings) (config.AppSettings, error) {
	s = s.Normalize()
	b, err := json.Marshal(s)
	if err != nil {
		return s, err
	}
	_, err = db.sql.Exec(`
		INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		settingsKey, string(b),
	)
	return s, err
}

func (db *DB) GetAppSettingRaw(key string) string {
	var val string
	if err := db.sql.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&val); err != nil {
		return ""
	}
	return val
}

func (db *DB) SetAppSettingRaw(key, value string) error {
	_, err := db.sql.Exec(`
		INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// --- Repositories ---

const repoSelectColumns = `id, provider, owner, name, full_name, description, private, default_branch, local_path, poll_interval_sec, trigger_label,
	autonomy_mode, default_template_id, release_mode, merge_strategy, build_cmd, test_cmd, lint_cmd,
	max_test_retries, max_ci_retries, sync_labels, sync_comments, draft_clarifications,
	stage_gates_json, stage_agents_json, custom_prompts_json, etag, last_synced_at, enabled, created_at`

func (db *DB) ListRepositories() ([]Repository, error) {
	rows, err := db.sql.Query(`SELECT ` + repoSelectColumns + ` FROM repositories ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Repository
	for rows.Next() {
		r, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) GetRepository(id string) (Repository, error) {
	row := db.sql.QueryRow(`SELECT `+repoSelectColumns+` FROM repositories WHERE id = ?`, id)
	r, err := scanRepository(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	return r, err
}

func (db *DB) GetRepositoryByFullName(fullName string) (Repository, error) {
	row := db.sql.QueryRow(`SELECT `+repoSelectColumns+` FROM repositories WHERE LOWER(full_name) = LOWER(?)`, strings.TrimSpace(fullName))
	r, err := scanRepository(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	return r, err
}

func (db *DB) UpsertRepository(r Repository) error {
	if r.Provider == "" {
		r.Provider = "github"
	}
	if r.DefaultBranch == "" {
		r.DefaultBranch = "main"
	}
	r.LocalPath = strings.TrimSpace(r.LocalPath)
	if r.LocalPath != "" {
		r.LocalPath = filepath.Clean(r.LocalPath)
	}
	if r.PollIntervalSec <= 0 {
		r.PollIntervalSec = 120
	}
	if r.AutonomyMode == "" {
		r.AutonomyMode = "template"
	}
	if r.DefaultTemplateID == "" {
		r.DefaultTemplateID = "auto"
	}
	if r.ReleaseMode == "" {
		r.ReleaseMode = "batch"
	}
	if r.MergeStrategy == "" {
		r.MergeStrategy = "squash"
	}
	if r.StageGatesJSON == "" {
		r.StageGatesJSON = "{}"
	}
	if r.StageAgentsJSON == "" {
		r.StageAgentsJSON = "{}"
	}
	if r.CustomPromptsJSON == "" {
		r.CustomPromptsJSON = "{}"
	}
	if r.CreatedAt == "" {
		r.CreatedAt = NowRFC3339()
	}

	_, err := db.sql.Exec(`
		INSERT INTO repositories (
			id, provider, owner, name, full_name, description, private, default_branch, local_path, poll_interval_sec, trigger_label,
			autonomy_mode, default_template_id, release_mode, merge_strategy, build_cmd, test_cmd, lint_cmd,
			max_test_retries, max_ci_retries, sync_labels, sync_comments, draft_clarifications,
			stage_gates_json, stage_agents_json, custom_prompts_json, etag, last_synced_at, enabled, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			provider = excluded.provider,
			owner = excluded.owner,
			name = excluded.name,
			full_name = excluded.full_name,
			description = excluded.description,
			private = excluded.private,
			default_branch = excluded.default_branch,
			local_path = excluded.local_path,
			poll_interval_sec = excluded.poll_interval_sec,
			trigger_label = excluded.trigger_label,
			autonomy_mode = excluded.autonomy_mode,
			default_template_id = excluded.default_template_id,
			release_mode = excluded.release_mode,
			merge_strategy = excluded.merge_strategy,
			build_cmd = excluded.build_cmd,
			test_cmd = excluded.test_cmd,
			lint_cmd = excluded.lint_cmd,
			max_test_retries = excluded.max_test_retries,
			max_ci_retries = excluded.max_ci_retries,
			sync_labels = excluded.sync_labels,
			sync_comments = excluded.sync_comments,
			draft_clarifications = excluded.draft_clarifications,
			stage_gates_json = excluded.stage_gates_json,
			stage_agents_json = excluded.stage_agents_json,
			custom_prompts_json = excluded.custom_prompts_json,
			etag = excluded.etag,
			last_synced_at = excluded.last_synced_at,
			enabled = excluded.enabled`,
		r.ID, r.Provider, r.Owner, r.Name, r.FullName, r.Description, boolToInt(r.Private), r.DefaultBranch, r.LocalPath, r.PollIntervalSec, r.TriggerLabel,
		r.AutonomyMode, r.DefaultTemplateID, r.ReleaseMode, r.MergeStrategy, r.BuildCmd, r.TestCmd, r.LintCmd,
		r.MaxTestRetries, r.MaxCIRetries, boolToInt(r.SyncLabels), boolToInt(r.SyncComments), boolToInt(r.DraftClarifications),
		r.StageGatesJSON, r.StageAgentsJSON, r.CustomPromptsJSON, r.ETag, r.LastSyncedAt, boolToInt(r.Enabled), r.CreatedAt,
	)
	return err
}

func (db *DB) SetRepositoryEnabled(id string, enabled bool) error {
	res, err := db.sql.Exec(`UPDATE repositories SET enabled = ? WHERE id = ?`, boolToInt(enabled), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) UpdateRepositorySyncState(id, etag, syncedAt string) error {
	_, err := db.sql.Exec(`UPDATE repositories SET etag = ?, last_synced_at = ? WHERE id = ?`, etag, syncedAt, id)
	return err
}

func (db *DB) DeleteRepository(id string) error {
	_, err := db.sql.Exec(`DELETE FROM repositories WHERE id = ?`, id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRepository(s scanner) (Repository, error) {
	var r Repository
	var private, syncLabels, syncComments, draftClarifications, enabled int
	err := s.Scan(
		&r.ID, &r.Provider, &r.Owner, &r.Name, &r.FullName, &r.Description, &private, &r.DefaultBranch, &r.LocalPath, &r.PollIntervalSec, &r.TriggerLabel,
		&r.AutonomyMode, &r.DefaultTemplateID, &r.ReleaseMode, &r.MergeStrategy, &r.BuildCmd, &r.TestCmd, &r.LintCmd,
		&r.MaxTestRetries, &r.MaxCIRetries, &syncLabels, &syncComments, &draftClarifications,
		&r.StageGatesJSON, &r.StageAgentsJSON, &r.CustomPromptsJSON, &r.ETag, &r.LastSyncedAt, &enabled, &r.CreatedAt,
	)
	r.Private = private == 1
	r.SyncLabels = syncLabels == 1
	r.SyncComments = syncComments == 1
	r.DraftClarifications = draftClarifications == 1
	r.Enabled = enabled == 1
	return r, err
}

// --- Issues ---

func (db *DB) UpsertIssue(issue Issue) error {
	if issue.LabelsJSON == "" {
		issue.LabelsJSON = "[]"
	}
	if issue.Status == "" {
		issue.Status = "backlog"
	}
	_, err := db.sql.Exec(`
		INSERT INTO issues (
			id, repo_id, number, title, body, state, author, labels_json, html_url,
			trigger_matched, status, updated_at, synced_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_id, number) DO UPDATE SET
			title = excluded.title,
			body = excluded.body,
			state = excluded.state,
			author = excluded.author,
			labels_json = excluded.labels_json,
			html_url = excluded.html_url,
			trigger_matched = excluded.trigger_matched,
			updated_at = excluded.updated_at,
			synced_at = excluded.synced_at,
			status = CASE
				WHEN issues.status IN ('queued', 'running', 'waiting_approval', 'pr_open', 'ignored', 'completed')
				THEN issues.status
				ELSE excluded.status
			END`,
		issue.ID, issue.RepoID, issue.Number, issue.Title, issue.Body, issue.State, issue.Author, issue.LabelsJSON,
		issue.HTMLURL, boolToInt(issue.TriggerMatched), issue.Status, issue.UpdatedAt, issue.SyncedAt,
	)
	return err
}

func (db *DB) ListIssues(repoID, statusFilter, search string) ([]Issue, error) {
	query := `SELECT id, repo_id, number, title, body, state, author, labels_json, html_url, trigger_matched, status, updated_at, synced_at FROM issues`
	var clauses []string
	var args []any

	if repoID != "" && repoID != "all" {
		clauses = append(clauses, "repo_id = ?")
		args = append(args, repoID)
	}
	if statusFilter != "" && statusFilter != "all" {
		clauses = append(clauses, "status = ?")
		args = append(args, statusFilter)
	}
	if strings.TrimSpace(search) != "" {
		clauses = append(clauses, "(title LIKE ? OR body LIKE ? OR CAST(number AS TEXT) LIKE ?)")
		pat := "%" + strings.TrimSpace(search) + "%"
		args = append(args, pat, pat, pat)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY number DESC"

	rows, err := db.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Issue
	for rows.Next() {
		var it Issue
		var matched int
		if err := rows.Scan(
			&it.ID, &it.RepoID, &it.Number, &it.Title, &it.Body, &it.State, &it.Author,
			&it.LabelsJSON, &it.HTMLURL, &matched, &it.Status, &it.UpdatedAt, &it.SyncedAt,
		); err != nil {
			return nil, err
		}
		it.TriggerMatched = matched == 1
		out = append(out, it)
	}
	return out, rows.Err()
}

func (db *DB) GetIssue(id string) (Issue, error) {
	var it Issue
	var matched int
	err := db.sql.QueryRow(`
		SELECT id, repo_id, number, title, body, state, author, labels_json, html_url, trigger_matched, status, updated_at, synced_at
		FROM issues WHERE id = ?`, id).Scan(
		&it.ID, &it.RepoID, &it.Number, &it.Title, &it.Body, &it.State, &it.Author,
		&it.LabelsJSON, &it.HTMLURL, &matched, &it.Status, &it.UpdatedAt, &it.SyncedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	it.TriggerMatched = matched == 1
	return it, err
}

func (db *DB) UpdateIssueStatus(id, status string) error {
	_, err := db.sql.Exec(`UPDATE issues SET status = ? WHERE id = ?`, status, id)
	return err
}

// --- Pipeline Templates ---

func (db *DB) ListTemplates() ([]PipelineTemplate, error) {
	rows, err := db.sql.Query(`
		SELECT id, name, description, stages_json, default_gates_json, is_builtin
		FROM pipeline_templates
		ORDER BY
			CASE id
				WHEN 'quick_bugfix' THEN 1
				WHEN 'complex_fix' THEN 2
				WHEN 'full_feature' THEN 3
				WHEN 'docs_chore' THEN 4
				ELSE 100
			END ASC,
			is_builtin DESC,
			name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PipelineTemplate
	for rows.Next() {
		var t PipelineTemplate
		var builtin int
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.StagesJSON, &t.DefaultGatesJSON, &builtin); err != nil {
			return nil, err
		}
		t.IsBuiltin = builtin == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) GetTemplate(id string) (PipelineTemplate, error) {
	var t PipelineTemplate
	var builtin int
	err := db.sql.QueryRow(`SELECT id, name, description, stages_json, default_gates_json, is_builtin FROM pipeline_templates WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.Description, &t.StagesJSON, &t.DefaultGatesJSON, &builtin)
	if errors.Is(err, sql.ErrNoRows) {
		return PipelineTemplate{}, ErrNotFound
	}
	t.IsBuiltin = builtin == 1
	return t, err
}

func (db *DB) UpsertTemplate(t PipelineTemplate) error {
	if t.StagesJSON == "" {
		t.StagesJSON = "[]"
	}
	if t.DefaultGatesJSON == "" {
		t.DefaultGatesJSON = "{}"
	}
	_, err := db.sql.Exec(`
		INSERT INTO pipeline_templates (id, name, description, stages_json, default_gates_json, is_builtin)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			stages_json = excluded.stages_json,
			default_gates_json = excluded.default_gates_json`,
		t.ID, t.Name, t.Description, t.StagesJSON, t.DefaultGatesJSON, boolToInt(t.IsBuiltin),
	)
	return err
}

func (db *DB) DeleteTemplate(id string) error {
	res, err := db.sql.Exec(`DELETE FROM pipeline_templates WHERE id = ? AND is_builtin = 0`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("内置模板不可删除或目标模板不存在")
	}
	return nil
}

// --- Agent Profiles ---

func scanAgentProfile(s scanner) (AgentProfile, error) {
	var a AgentProfile
	var supportsTiers, primaryGroup, enabled, detected int
	err := s.Scan(
		&a.ID, &a.Name, &a.Kind, &a.Command, &a.ArgsTemplate,
		&a.ProviderID, &a.Model, &a.Subtitle, &a.TiersJSON,
		&supportsTiers, &primaryGroup, &a.SortOrder,
		&a.PermissionMode, &a.ExtraEnvJSON, &enabled, &detected,
		&a.DetectedPath, &a.DetectedVersion, &a.CapabilitiesJSON,
	)
	a.SupportsTiers = supportsTiers == 1
	a.PrimaryGroup = primaryGroup == 1
	a.Enabled = enabled == 1
	a.Detected = detected == 1
	return a, err
}

func (db *DB) ListAgentProfiles() ([]AgentProfile, error) {
	rows, err := db.sql.Query(`
		SELECT id, name, kind, command, args_template,
			provider_id, model, subtitle, tiers_json, supports_tiers, primary_group, sort_order,
			permission_mode, extra_env_json, enabled, detected, detected_path, detected_version, capabilities_json
		FROM agent_profiles ORDER BY primary_group DESC, sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentProfile
	for rows.Next() {
		a, err := scanAgentProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) GetAgentProfile(id string) (AgentProfile, error) {
	row := db.sql.QueryRow(`
		SELECT id, name, kind, command, args_template,
			provider_id, model, subtitle, tiers_json, supports_tiers, primary_group, sort_order,
			permission_mode, extra_env_json, enabled, detected, detected_path, detected_version, capabilities_json
		FROM agent_profiles WHERE id = ?`, id)
	a, err := scanAgentProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentProfile{}, ErrNotFound
	}
	return a, err
}

func (db *DB) UpsertAgentProfile(a AgentProfile) error {
	if a.ExtraEnvJSON == "" {
		a.ExtraEnvJSON = "{}"
	}
	if a.CapabilitiesJSON == "" {
		a.CapabilitiesJSON = "{}"
	}
	if a.TiersJSON == "" {
		a.TiersJSON = "{}"
	}
	if a.SortOrder <= 0 {
		a.SortOrder = 200
	}
	_, err := db.sql.Exec(`
		INSERT INTO agent_profiles (
			id, name, kind, command, args_template,
			provider_id, model, subtitle, tiers_json, supports_tiers, primary_group, sort_order,
			permission_mode, extra_env_json, enabled, detected, detected_path, detected_version, capabilities_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			kind = excluded.kind,
			command = excluded.command,
			args_template = excluded.args_template,
			provider_id = excluded.provider_id,
			model = excluded.model,
			subtitle = excluded.subtitle,
			tiers_json = excluded.tiers_json,
			supports_tiers = excluded.supports_tiers,
			primary_group = excluded.primary_group,
			sort_order = excluded.sort_order,
			permission_mode = excluded.permission_mode,
			extra_env_json = excluded.extra_env_json,
			enabled = excluded.enabled,
			detected = excluded.detected,
			detected_path = excluded.detected_path,
			detected_version = excluded.detected_version,
			capabilities_json = excluded.capabilities_json`,
		a.ID, a.Name, a.Kind, a.Command, a.ArgsTemplate,
		a.ProviderID, a.Model, a.Subtitle, a.TiersJSON, boolToInt(a.SupportsTiers), boolToInt(a.PrimaryGroup), a.SortOrder,
		a.PermissionMode, a.ExtraEnvJSON, boolToInt(a.Enabled), boolToInt(a.Detected), a.DetectedPath, a.DetectedVersion, a.CapabilitiesJSON,
	)
	return err
}

func (db *DB) UpdateAgentDetection(id string, detected bool, path, version string) error {
	_, err := db.sql.Exec(`UPDATE agent_profiles SET detected = ?, detected_path = ?, detected_version = ? WHERE id = ?`,
		boolToInt(detected), path, version, id)
	return err
}

func (db *DB) DeleteAgentProfile(id string) error {
	_, err := db.sql.Exec(`DELETE FROM agent_profiles WHERE id = ?`, id)
	return err
}

// --- Routing Rules ---

func (db *DB) ListRoutingRules() ([]RoutingRule, error) {
	rows, err := db.sql.Query(`
		SELECT id, priority, name, repo_id, label_match, title_regex, complexity_hint,
			template_id, agent_id, model_ref, stage_overrides_json, enabled
		FROM routing_rules ORDER BY priority ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoutingRule
	for rows.Next() {
		var r RoutingRule
		var enabled int
		if err := rows.Scan(
			&r.ID, &r.Priority, &r.Name, &r.RepoID, &r.LabelMatch, &r.TitleRegex,
			&r.ComplexityHint, &r.TemplateID, &r.AgentID, &r.ModelRef, &r.StageOverridesJSON, &enabled,
		); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) UpsertRoutingRule(r RoutingRule) error {
	if r.StageOverridesJSON == "" {
		r.StageOverridesJSON = "{}"
	}
	_, err := db.sql.Exec(`
		INSERT INTO routing_rules (
			id, priority, name, repo_id, label_match, title_regex, complexity_hint,
			template_id, agent_id, model_ref, stage_overrides_json, enabled
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			priority = excluded.priority,
			name = excluded.name,
			repo_id = excluded.repo_id,
			label_match = excluded.label_match,
			title_regex = excluded.title_regex,
			complexity_hint = excluded.complexity_hint,
			template_id = excluded.template_id,
			agent_id = excluded.agent_id,
			model_ref = excluded.model_ref,
			stage_overrides_json = excluded.stage_overrides_json,
			enabled = excluded.enabled`,
		r.ID, r.Priority, r.Name, r.RepoID, r.LabelMatch, r.TitleRegex, r.ComplexityHint,
		r.TemplateID, r.AgentID, r.ModelRef, r.StageOverridesJSON, boolToInt(r.Enabled),
	)
	return err
}

func (db *DB) DeleteRoutingRule(id string) error {
	_, err := db.sql.Exec(`DELETE FROM routing_rules WHERE id = ?`, id)
	return err
}

// --- Pipeline Runs & Stages ---

func (db *DB) CreatePipelineRun(run PipelineRun, stages []StageExecution) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		INSERT INTO pipeline_runs (
			id, repo_id, issue_id, issue_number, issue_title, template_id, status, current_stage,
			routed_agent_id, routing_reason, branch_name, worktree_path, pr_number, pr_url, ci_status,
			started_at, finished_at, error_message
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.RepoID, run.IssueID, run.IssueNumber, run.IssueTitle, run.TemplateID, run.Status, run.CurrentStage,
		run.RoutedAgentID, run.RoutingReason, run.BranchName, run.WorktreePath, run.PRNumber, run.PRURL, run.CIStatus,
		run.StartedAt, run.FinishedAt, run.ErrorMessage,
	)
	if err != nil {
		return err
	}

	for _, st := range stages {
		_, err = tx.Exec(`
			INSERT INTO stage_executions (
				id, run_id, stage_key, stage_name, order_index, agent_id, model_ref, status, attempt,
				summary, artifact_path, feedback, started_at, finished_at, duration_ms, error_message
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			st.ID, st.RunID, st.StageKey, st.StageName, st.OrderIndex, st.AgentID, st.ModelRef, st.Status, st.Attempt,
			st.Summary, st.ArtifactPath, st.Feedback, st.StartedAt, st.FinishedAt, st.DurationMs, st.ErrorMessage,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) UpdatePipelineRun(run PipelineRun) error {
	_, err := db.sql.Exec(`
		UPDATE pipeline_runs SET
			status = ?, current_stage = ?, routed_agent_id = ?, routing_reason = ?,
			branch_name = ?, worktree_path = ?, pr_number = ?, pr_url = ?, ci_status = ?,
			started_at = ?, finished_at = ?, error_message = ?
		WHERE id = ?`,
		run.Status, run.CurrentStage, run.RoutedAgentID, run.RoutingReason,
		run.BranchName, run.WorktreePath, run.PRNumber, run.PRURL, run.CIStatus,
		run.StartedAt, run.FinishedAt, run.ErrorMessage, run.ID,
	)
	return err
}

func (db *DB) GetPipelineRun(id string) (PipelineRun, error) {
	var r PipelineRun
	err := db.sql.QueryRow(`
		SELECT id, repo_id, issue_id, issue_number, issue_title, template_id, status, current_stage,
			routed_agent_id, routing_reason, branch_name, worktree_path, pr_number, pr_url, ci_status,
			started_at, finished_at, error_message
		FROM pipeline_runs WHERE id = ?`, id).Scan(
		&r.ID, &r.RepoID, &r.IssueID, &r.IssueNumber, &r.IssueTitle, &r.TemplateID, &r.Status, &r.CurrentStage,
		&r.RoutedAgentID, &r.RoutingReason, &r.BranchName, &r.WorktreePath, &r.PRNumber, &r.PRURL, &r.CIStatus,
		&r.StartedAt, &r.FinishedAt, &r.ErrorMessage,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PipelineRun{}, ErrNotFound
	}
	return r, err
}

func (db *DB) ListPipelineRuns(repoID, statusFilter string, limit int) ([]PipelineRun, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, repo_id, issue_id, issue_number, issue_title, template_id, status, current_stage,
			routed_agent_id, routing_reason, branch_name, worktree_path, pr_number, pr_url, ci_status,
			started_at, finished_at, error_message
		FROM pipeline_runs`
	var clauses []string
	var args []any
	if repoID != "" && repoID != "all" {
		clauses = append(clauses, "repo_id = ?")
		args = append(args, repoID)
	}
	if statusFilter != "" && statusFilter != "all" {
		clauses = append(clauses, "status = ?")
		args = append(args, statusFilter)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY started_at DESC, rowid DESC LIMIT %d", limit)

	rows, err := db.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PipelineRun
	for rows.Next() {
		var r PipelineRun
		if err := rows.Scan(
			&r.ID, &r.RepoID, &r.IssueID, &r.IssueNumber, &r.IssueTitle, &r.TemplateID, &r.Status, &r.CurrentStage,
			&r.RoutedAgentID, &r.RoutingReason, &r.BranchName, &r.WorktreePath, &r.PRNumber, &r.PRURL, &r.CIStatus,
			&r.StartedAt, &r.FinishedAt, &r.ErrorMessage,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) HasActiveRunForIssue(issueID string) (bool, error) {
	var cnt int
	err := db.sql.QueryRow(`
		SELECT COUNT(1) FROM pipeline_runs
		WHERE issue_id = ? AND status IN ('queued', 'running', 'waiting_approval', 'succeeded')`,
		issueID).Scan(&cnt)
	return cnt > 0, err
}

func (db *DB) RecoverOrphanedRuns() (int64, error) {
	now := NowRFC3339()
	res, err := db.sql.Exec(`
		UPDATE pipeline_runs
		SET status = 'failed', finished_at = ?, error_message = '应用意外退出，流水线任务已中断，请点击重试恢复执行。'
		WHERE status IN ('queued', 'running')`, now)
	if err != nil {
		return 0, err
	}
	_, _ = db.sql.Exec(`
		UPDATE stage_executions
		SET status = 'failed', finished_at = ?, error_message = '运行中断'
		WHERE status = 'running'`, now)
	return res.RowsAffected()
}

func (db *DB) ListStagesByRun(runID string) ([]StageExecution, error) {
	rows, err := db.sql.Query(`
		SELECT id, run_id, stage_key, stage_name, order_index, agent_id, model_ref, status, attempt,
			summary, artifact_path, feedback, started_at, finished_at, duration_ms, error_message
		FROM stage_executions WHERE run_id = ? ORDER BY order_index ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StageExecution
	for rows.Next() {
		var st StageExecution
		if err := rows.Scan(
			&st.ID, &st.RunID, &st.StageKey, &st.StageName, &st.OrderIndex, &st.AgentID, &st.ModelRef, &st.Status, &st.Attempt,
			&st.Summary, &st.ArtifactPath, &st.Feedback, &st.StartedAt, &st.FinishedAt, &st.DurationMs, &st.ErrorMessage,
		); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (db *DB) UpdateStageExecution(st StageExecution) error {
	_, err := db.sql.Exec(`
		UPDATE stage_executions SET
			agent_id = ?, model_ref = ?, status = ?, attempt = ?, summary = ?, artifact_path = ?,
			feedback = ?, started_at = ?, finished_at = ?, duration_ms = ?, error_message = ?
		WHERE id = ?`,
		st.AgentID, st.ModelRef, st.Status, st.Attempt, st.Summary, st.ArtifactPath,
		st.Feedback, st.StartedAt, st.FinishedAt, st.DurationMs, st.ErrorMessage, st.ID,
	)
	return err
}

// --- Run Logs ---

func (db *DB) AppendRunLog(runID, stageKey, stream, content string) (RunLog, error) {
	now := NowRFC3339()
	res, err := db.sql.Exec(`
		INSERT INTO run_logs (run_id, stage_key, stream, content, created_at)
		VALUES (?, ?, ?, ?, ?)`, runID, stageKey, stream, content, now)
	if err != nil {
		return RunLog{}, err
	}
	id, _ := res.LastInsertId()
	return RunLog{
		ID:        id,
		RunID:     runID,
		StageKey:  stageKey,
		Stream:    stream,
		Content:   content,
		CreatedAt: now,
	}, nil
}

func (db *DB) ListRunLogs(runID string, limit int) ([]RunLog, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := db.sql.Query(`
		SELECT id, run_id, stage_key, stream, content, created_at
		FROM (
			SELECT id, run_id, stage_key, stream, content, created_at
			FROM run_logs WHERE run_id = ? ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunLog
	for rows.Next() {
		var l RunLog
		if err := rows.Scan(&l.ID, &l.RunID, &l.StageKey, &l.Stream, &l.Content, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// --- Releases ---

func (db *DB) InsertReleaseItem(item ReleaseItem) error {
	_, err := db.sql.Exec(`
		INSERT INTO release_items (
			id, repo_id, run_id, pr_number, pr_title, commit_type, commit_scope, summary, breaking, merged_at, release_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.RepoID, item.RunID, item.PRNumber, item.PRTitle, item.CommitType,
		item.CommitScope, item.Summary, boolToInt(item.Breaking), item.MergedAt, item.ReleaseID,
	)
	return err
}

func (db *DB) ListPendingReleaseItems(repoID string) ([]ReleaseItem, error) {
	query := `
		SELECT id, repo_id, run_id, pr_number, pr_title, commit_type, commit_scope, summary, breaking, merged_at, release_id
		FROM release_items WHERE release_id = ''`
	var args []any
	if repoID != "" && repoID != "all" {
		query += " AND repo_id = ?"
		args = append(args, repoID)
	}
	query += " ORDER BY merged_at ASC"
	rows, err := db.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReleaseItem
	for rows.Next() {
		var it ReleaseItem
		var breaking int
		if err := rows.Scan(
			&it.ID, &it.RepoID, &it.RunID, &it.PRNumber, &it.PRTitle, &it.CommitType,
			&it.CommitScope, &it.Summary, &breaking, &it.MergedAt, &it.ReleaseID,
		); err != nil {
			return nil, err
		}
		it.Breaking = breaking == 1
		out = append(out, it)
	}
	return out, rows.Err()
}

func (db *DB) AttachItemsToRelease(itemIDs []string, releaseID string) error {
	if len(itemIDs) == 0 {
		return nil
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range itemIDs {
		if _, err := tx.Exec(`UPDATE release_items SET release_id = ? WHERE id = ?`, releaseID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) UpsertReleaseRecord(rec ReleaseRecord) error {
	_, err := db.sql.Exec(`
		INSERT INTO release_records (
			id, repo_id, tag_name, prev_tag, version, bump_type, changelog, mode, status,
			html_url, actions_status, actions_url, created_at, published_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			tag_name = excluded.tag_name,
			prev_tag = excluded.prev_tag,
			version = excluded.version,
			bump_type = excluded.bump_type,
			changelog = excluded.changelog,
			mode = excluded.mode,
			status = excluded.status,
			html_url = excluded.html_url,
			actions_status = excluded.actions_status,
			actions_url = excluded.actions_url,
			published_at = excluded.published_at`,
		rec.ID, rec.RepoID, rec.TagName, rec.PrevTag, rec.Version, rec.BumpType, rec.Changelog, rec.Mode, rec.Status,
		rec.HTMLURL, rec.ActionsStatus, rec.ActionsURL, rec.CreatedAt, rec.PublishedAt,
	)
	return err
}

func (db *DB) ListReleaseRecords(repoID string, limit int) ([]ReleaseRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, repo_id, tag_name, prev_tag, version, bump_type, changelog, mode, status,
			html_url, actions_status, actions_url, created_at, published_at
		FROM release_records`
	var args []any
	if repoID != "" && repoID != "all" {
		query += " WHERE repo_id = ?"
		args = append(args, repoID)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)
	rows, err := db.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReleaseRecord
	for rows.Next() {
		var rec ReleaseRecord
		if err := rows.Scan(
			&rec.ID, &rec.RepoID, &rec.TagName, &rec.PrevTag, &rec.Version, &rec.BumpType,
			&rec.Changelog, &rec.Mode, &rec.Status, &rec.HTMLURL, &rec.ActionsStatus,
			&rec.ActionsURL, &rec.CreatedAt, &rec.PublishedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (db *DB) GetReleaseRecord(id string) (ReleaseRecord, error) {
	var rec ReleaseRecord
	err := db.sql.QueryRow(`
		SELECT id, repo_id, tag_name, prev_tag, version, bump_type, changelog, mode, status,
			html_url, actions_status, actions_url, created_at, published_at
		FROM release_records WHERE id = ?`, id).Scan(
		&rec.ID, &rec.RepoID, &rec.TagName, &rec.PrevTag, &rec.Version, &rec.BumpType,
		&rec.Changelog, &rec.Mode, &rec.Status, &rec.HTMLURL, &rec.ActionsStatus,
		&rec.ActionsURL, &rec.CreatedAt, &rec.PublishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ReleaseRecord{}, ErrNotFound
	}
	return rec, err
}

// --- Dashboard Stats ---

func (db *DB) GetDashboardStats() (DashboardStats, error) {
	var st DashboardStats
	queries := []struct {
		sql  string
		dest *int
	}{
		{`SELECT COUNT(1) FROM pipeline_runs WHERE status IN ('queued', 'running')`, &st.ActiveRuns},
		{`SELECT COUNT(1) FROM pipeline_runs WHERE status = 'waiting_approval'`, &st.WaitingApprovals},
		{`SELECT COUNT(1) FROM issues WHERE state = 'open' AND status = 'backlog'`, &st.BacklogIssues},
		{`SELECT COUNT(1) FROM release_items WHERE release_id = ''`, &st.PendingReleaseItems},
		{`SELECT COUNT(1) FROM pipeline_runs WHERE status = 'succeeded'`, &st.SucceededRuns},
		{`SELECT COUNT(1) FROM pipeline_runs WHERE status = 'failed'`, &st.FailedRuns},
		{`SELECT COUNT(1) FROM repositories WHERE enabled = 1`, &st.ConnectedRepos},
		{`SELECT COUNT(1) FROM agent_profiles WHERE detected = 1 AND enabled = 1`, &st.DetectedAgents},
	}
	for _, q := range queries {
		if err := db.sql.QueryRow(q.sql).Scan(q.dest); err != nil {
			return st, err
		}
	}
	return st, nil
}

// --- Routing Config ---

const routingConfigKey = "routing_config"

func (db *DB) GetRoutingConfig() (RoutingConfig, error) {
	def := DefaultRoutingConfig()
	var raw string
	err := db.sql.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, routingConfigKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	if err != nil {
		return def, err
	}
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		return DefaultRoutingConfig(), err
	}
	if def.Strategy == "" {
		def.Strategy = "rule_ai"
	}
	if def.StageBindings == nil {
		def.StageBindings = DefaultRoutingConfig().StageBindings
	}
	return def, nil
}

func (db *DB) SaveRoutingConfig(cfg RoutingConfig) (RoutingConfig, error) {
	if cfg.Strategy == "" {
		cfg.Strategy = "rule_ai"
	}
	if cfg.StageBindings == nil {
		cfg.StageBindings = DefaultRoutingConfig().StageBindings
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	_, err = db.sql.Exec(`
		INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		routingConfigKey, string(b),
	)
	return cfg, err
}

// --- Model Providers ---

func scanModelProvider(s scanner) (ModelProvider, error) {
	var p ModelProvider
	var enabled, builtin int
	err := s.Scan(&p.ID, &p.Name, &p.Protocol, &p.BaseURL, &p.ModelsJSON, &enabled, &builtin, &p.SortOrder)
	p.Enabled = enabled == 1
	p.IsBuiltin = builtin == 1
	return p, err
}

func (db *DB) ListModelProviders() ([]ModelProvider, error) {
	rows, err := db.sql.Query(`
		SELECT id, name, protocol, base_url, models_json, enabled, is_builtin, sort_order
		FROM model_providers ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelProvider
	for rows.Next() {
		p, err := scanModelProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) GetModelProvider(id string) (ModelProvider, error) {
	row := db.sql.QueryRow(`
		SELECT id, name, protocol, base_url, models_json, enabled, is_builtin, sort_order
		FROM model_providers WHERE id = ?`, id)
	p, err := scanModelProvider(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ModelProvider{}, ErrNotFound
	}
	return p, err
}

func (db *DB) UpsertModelProvider(p ModelProvider) error {
	if p.Protocol == "" {
		p.Protocol = "openai_compatible"
	}
	if p.ModelsJSON == "" {
		p.ModelsJSON = "[]"
	}
	if p.SortOrder <= 0 {
		p.SortOrder = 200
	}
	_, err := db.sql.Exec(`
		INSERT INTO model_providers (id, name, protocol, base_url, models_json, enabled, is_builtin, sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			protocol = excluded.protocol,
			base_url = excluded.base_url,
			models_json = excluded.models_json,
			enabled = excluded.enabled,
			sort_order = excluded.sort_order`,
		p.ID, p.Name, p.Protocol, p.BaseURL, p.ModelsJSON, boolToInt(p.Enabled), boolToInt(p.IsBuiltin), p.SortOrder,
	)
	return err
}

func (db *DB) DeleteModelProvider(id string) error {
	_, err := db.sql.Exec(`DELETE FROM model_providers WHERE id = ? AND is_builtin = 0`, id)
	return err
}

// --- Workspace Profiles ---

func scanWorkspaceProfile(s scanner) (WorkspaceProfile, error) {
	var wp WorkspaceProfile
	var active, builtin int
	err := s.Scan(
		&wp.ID, &wp.Name, &wp.Description, &wp.AccentColor, &wp.RoutingStrategy,
		&wp.AgentBindingsJSON, &wp.StageRoutingJSON, &active, &builtin, &wp.CreatedAt,
	)
	wp.IsActive = active == 1
	wp.IsBuiltin = builtin == 1
	return wp, err
}

func (db *DB) ListWorkspaceProfiles() ([]WorkspaceProfile, error) {
	rows, err := db.sql.Query(`
		SELECT id, name, description, accent_color, routing_strategy,
			agent_bindings_json, stage_routing_json, is_active, is_builtin, created_at
		FROM workspace_profiles ORDER BY is_builtin DESC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceProfile
	for rows.Next() {
		wp, err := scanWorkspaceProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, wp)
	}
	return out, rows.Err()
}

func (db *DB) GetWorkspaceProfile(id string) (WorkspaceProfile, error) {
	row := db.sql.QueryRow(`
		SELECT id, name, description, accent_color, routing_strategy,
			agent_bindings_json, stage_routing_json, is_active, is_builtin, created_at
		FROM workspace_profiles WHERE id = ?`, id)
	wp, err := scanWorkspaceProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceProfile{}, ErrNotFound
	}
	return wp, err
}

func (db *DB) UpsertWorkspaceProfile(wp WorkspaceProfile) error {
	if wp.AccentColor == "" {
		wp.AccentColor = "#0F6CBD"
	}
	if wp.RoutingStrategy == "" {
		wp.RoutingStrategy = "rule_ai"
	}
	if wp.AgentBindingsJSON == "" {
		wp.AgentBindingsJSON = "{}"
	}
	if wp.StageRoutingJSON == "" {
		wp.StageRoutingJSON = "{}"
	}
	if wp.CreatedAt == "" {
		wp.CreatedAt = NowRFC3339()
	}
	_, err := db.sql.Exec(`
		INSERT INTO workspace_profiles (
			id, name, description, accent_color, routing_strategy,
			agent_bindings_json, stage_routing_json, is_active, is_builtin, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			accent_color = excluded.accent_color,
			routing_strategy = excluded.routing_strategy,
			agent_bindings_json = excluded.agent_bindings_json,
			stage_routing_json = excluded.stage_routing_json,
			is_active = excluded.is_active`,
		wp.ID, wp.Name, wp.Description, wp.AccentColor, wp.RoutingStrategy,
		wp.AgentBindingsJSON, wp.StageRoutingJSON, boolToInt(wp.IsActive), boolToInt(wp.IsBuiltin), wp.CreatedAt,
	)
	return err
}

func (db *DB) SetActiveWorkspaceProfile(id string) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE workspace_profiles SET is_active = 0`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE workspace_profiles SET is_active = 1 WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) DeleteWorkspaceProfile(id string) error {
	_, err := db.sql.Exec(`DELETE FROM workspace_profiles WHERE id = ? AND is_builtin = 0`, id)
	return err
}
