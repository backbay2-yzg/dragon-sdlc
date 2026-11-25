package store

import (
	"encoding/json"
	"time"
)

// Canonical 7 SDLC stage keys.
const (
	StageRequirement    = "requirement"
	StageDesign         = "design"
	StageImplementation = "implementation"
	StageVerification   = "verification"
	StageAIReview       = "ai_review"
	StagePRCI           = "pr_ci"
	StageMergeRelease   = "merge_release"
)

// Canonical stage descriptors.
type StageDescriptor struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Order       int    `json:"order"`
	DefaultOn   bool   `json:"defaultOn"`
}

// AllStageDescriptors returns the canonical 7 SDLC stages in execution order.
func AllStageDescriptors() []StageDescriptor {
	return []StageDescriptor{
		{Key: StageRequirement, Name: "需求分析", Description: "解析 Issue 背景、验收标准与边界，产出 spec.md", Order: 1, DefaultOn: true},
		{Key: StageDesign, Name: "方案设计", Description: "分析受影响模块、接口契约与测试策略，产出 plan.md", Order: 2, DefaultOn: true},
		{Key: StageImplementation, Name: "编码实现", Description: "在隔离 Git Worktree 中编写功能代码与单元测试", Order: 3, DefaultOn: true},
		{Key: StageVerification, Name: "测试验证", Description: "执行构建、Lint 与测试命令，失败时自动带日志修复", Order: 4, DefaultOn: true},
		{Key: StageAIReview, Name: "AI 代码审查", Description: "提取 Diff 进行跨 Agent 审查，产出 review.md 并修正阻塞项", Order: 5, DefaultOn: true},
		{Key: StagePRCI, Name: "提交 PR 与 CI 跟踪", Description: "推送分支、创建 Pull Request 并盯守 GitHub Check Runs", Order: 6, DefaultOn: true},
		{Key: StageMergeRelease, Name: "合并与发布", Description: "合并 PR 并按即时或批量发版列车生成 SemVer Release", Order: 7, DefaultOn: true},
	}
}

// Repository represents a connected GitHub (or future provider) repository.
type Repository struct {
	ID                  string `json:"id"`
	Provider            string `json:"provider"` // github
	Owner               string `json:"owner"`
	Name                string `json:"name"`
	FullName            string `json:"fullName"`
	Description         string `json:"description"`
	Private             bool   `json:"private"`
	DefaultBranch       string `json:"defaultBranch"`
	LocalPath           string `json:"localPath"`
	PollIntervalSec     int    `json:"pollIntervalSec"`
	TriggerLabel        string `json:"triggerLabel"`
	AutonomyMode        string `json:"autonomyMode"` // template | semi_auto | full_auto | step_by_step
	DefaultTemplateID   string `json:"defaultTemplateId"`
	ReleaseMode         string `json:"releaseMode"`   // immediate | batch
	MergeStrategy       string `json:"mergeStrategy"` // squash | merge | rebase
	BuildCmd            string `json:"buildCmd"`
	TestCmd             string `json:"testCmd"`
	LintCmd             string `json:"lintCmd"`
	MaxTestRetries      int    `json:"maxTestRetries"`
	MaxCIRetries        int    `json:"maxCiRetries"`
	SyncLabels          bool   `json:"syncLabels"`
	SyncComments        bool   `json:"syncComments"`
	DraftClarifications bool   `json:"draftClarifications"`
	StageGatesJSON      string `json:"stageGatesJson"`
	StageAgentsJSON     string `json:"stageAgentsJson"`
	CustomPromptsJSON   string `json:"customPromptsJson"`
	ETag                string `json:"etag"`
	LastSyncedAt        string `json:"lastSyncedAt"`
	Enabled             bool   `json:"enabled"`
	CreatedAt           string `json:"createdAt"`
}

// Issue represents a synced issue from a tracked repository.
type Issue struct {
	ID             string `json:"id"`
	RepoID         string `json:"repoId"`
	Number         int    `json:"number"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	State          string `json:"state"`
	Author         string `json:"author"`
	LabelsJSON     string `json:"labelsJson"`
	HTMLURL        string `json:"htmlUrl"`
	TriggerMatched bool   `json:"triggerMatched"`
	Status         string `json:"status"` // backlog | queued | running | waiting_approval | pr_open | completed | failed | ignored
	UpdatedAt      string `json:"updatedAt"`
	SyncedAt       string `json:"syncedAt"`
}

// Labels parses LabelsJSON cleanly.
func (i Issue) Labels() []string {
	var out []string
	_ = json.Unmarshal([]byte(i.LabelsJSON), &out)
	return out
}

// PipelineTemplate defines which SDLC stages run for a category of work.
type PipelineTemplate struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	StagesJSON       string `json:"stagesJson"`
	DefaultGatesJSON string `json:"defaultGatesJson"`
	IsBuiltin        bool   `json:"isBuiltin"`
}

// Stages returns the ordered stage keys enabled in this template.
func (t PipelineTemplate) Stages() []string {
	var out []string
	_ = json.Unmarshal([]byte(t.StagesJSON), &out)
	return out
}

// ModelCatalogEntry describes a model offered by a ModelProvider.
type ModelCatalogEntry struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ProviderID string   `json:"providerId"`
	CostTier   string   `json:"costTier"` // budget | balanced | flagship
	ContextK   int      `json:"contextK"`
	Tags       []string `json:"tags"`
}

// ModelProvider represents an LLM/Coding model provider (Anthropic, OpenAI, DeepSeek, Kimi, etc.).
type ModelProvider struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`   // anthropic | openai_compatible | google
	BaseURL    string `json:"baseUrl"`
	ModelsJSON string `json:"modelsJson"` // []ModelCatalogEntry
	Enabled    bool   `json:"enabled"`
	HasKey     bool   `json:"hasKey"`
	IsBuiltin  bool   `json:"isBuiltin"`
	SortOrder  int    `json:"sortOrder"`
}

// Models parses ModelsJSON cleanly.
func (p ModelProvider) Models() []ModelCatalogEntry {
	var out []ModelCatalogEntry
	_ = json.Unmarshal([]byte(p.ModelsJSON), &out)
	return out
}

// AgentProfile describes a local Coding CLI or custom agent executor.
type AgentProfile struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Kind             string `json:"kind"` // claude | codex | gemini | agy | opencode | copilot | kimi | aider | custom
	Command          string `json:"command"`
	ArgsTemplate     string `json:"argsTemplate"`
	ProviderID       string `json:"providerId"`
	Model            string `json:"model"`
	Subtitle         string `json:"subtitle"`
	TiersJSON        string `json:"tiersJson"` // {"opus":"...","sonnet":"...","haiku":"..."}
	SupportsTiers    bool   `json:"supportsTiers"`
	PrimaryGroup     bool   `json:"primaryGroup"`
	SortOrder        int    `json:"sortOrder"`
	PermissionMode   string `json:"permissionMode"`
	ExtraEnvJSON     string `json:"extraEnvJson"`
	Enabled          bool   `json:"enabled"`
	Detected         bool   `json:"detected"`
	DetectedPath     string `json:"detectedPath"`
	DetectedVersion  string `json:"detectedVersion"`
	CapabilitiesJSON string `json:"capabilitiesJson"`
}

// Tiers parses TiersJSON into a map of tier name -> model ref.
func (a AgentProfile) Tiers() map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal([]byte(a.TiersJSON), &out)
	return out
}

// RoutingRule maps issue metadata to a pipeline template and agent(s).
type RoutingRule struct {
	ID                 string `json:"id"`
	Priority           int    `json:"priority"`
	Name               string `json:"name"`
	RepoID             string `json:"repoId"` // empty or "*" matches all repos
	LabelMatch         string `json:"labelMatch"`
	TitleRegex         string `json:"titleRegex"`
	ComplexityHint     string `json:"complexityHint"`
	TemplateID         string `json:"templateId"`
	AgentID            string `json:"agentId"`
	ModelRef           string `json:"modelRef"` // optional providerId/modelId override
	StageOverridesJSON string `json:"stageOverridesJson"`
	Enabled            bool   `json:"enabled"`
}

// StageRouteBinding binds an SDLC stage to a specific Agent and/or Model/Tier.
type StageRouteBinding struct {
	AgentID  string `json:"agentId"`  // empty = follow primary routed agent
	ModelRef string `json:"modelRef"` // empty = agent default, or "providerId/modelId"
	Tier     string `json:"tier"`     // "" | "opus" | "sonnet" | "haiku"
}

// RoutingConfig stores global routing strategy, router model, and 7-stage bindings.
type RoutingConfig struct {
	Strategy        string                       `json:"strategy"` // rule_ai | budget_first | quality_first | manual
	RouterModelRef  string                       `json:"routerModelRef"`
	FallbackAgentID string                       `json:"fallbackAgentId"`
	StageBindings   map[string]StageRouteBinding `json:"stageBindings"`
}

// DefaultRoutingConfig returns Dragon's default global routing settings.
func DefaultRoutingConfig() RoutingConfig {
	bindings := make(map[string]StageRouteBinding)
	for _, st := range AllStageDescriptors() {
		bindings[st.Key] = StageRouteBinding{}
	}
	return RoutingConfig{
		Strategy:        "rule_ai",
		RouterModelRef:  "",
		FallbackAgentID: "",
		StageBindings:   bindings,
	}
}

// AgentBindingSnapshot captures one agent's model & toggle state inside a WorkspaceProfile.
type AgentBindingSnapshot struct {
	Enabled    bool   `json:"enabled"`
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
	TiersJSON  string `json:"tiersJson"`
}

// WorkspaceProfile is a one-click preset snapshot (Budget, Balanced, Focus, or custom).
type WorkspaceProfile struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	AccentColor       string `json:"accentColor"`
	RoutingStrategy   string `json:"routingStrategy"`
	AgentBindingsJSON string `json:"agentBindingsJson"` // map[agentId]AgentBindingSnapshot
	StageRoutingJSON  string `json:"stageRoutingJson"`  // map[stageKey]StageRouteBinding
	IsActive          bool   `json:"isActive"`
	IsBuiltin         bool   `json:"isBuiltin"`
	CreatedAt         string `json:"createdAt"`
}

// PipelineRun represents one automated SDLC execution for an issue.
type PipelineRun struct {
	ID            string `json:"id"`
	RepoID        string `json:"repoId"`
	IssueID       string `json:"issueId"`
	IssueNumber   int    `json:"issueNumber"`
	IssueTitle    string `json:"issueTitle"`
	TemplateID    string `json:"templateId"`
	Status        string `json:"status"` // queued | running | waiting_approval | succeeded | failed | cancelled
	CurrentStage  string `json:"currentStage"`
	RoutedAgentID string `json:"routedAgentId"`
	RoutingReason string `json:"routingReason"`
	BranchName    string `json:"branchName"`
	WorktreePath  string `json:"worktreePath"`
	PRNumber      int    `json:"prNumber"`
	PRURL         string `json:"prUrl"`
	CIStatus      string `json:"ciStatus"` // none | pending | success | failure
	StartedAt     string `json:"startedAt"`
	FinishedAt    string `json:"finishedAt"`
	ErrorMessage  string `json:"errorMessage"`
}

// StageExecution tracks an individual stage inside a PipelineRun.
type StageExecution struct {
	ID           string `json:"id"`
	RunID        string `json:"runId"`
	StageKey     string `json:"stageKey"`
	StageName    string `json:"stageName"`
	OrderIndex   int    `json:"orderIndex"`
	AgentID      string `json:"agentId"`
	ModelRef     string `json:"modelRef"`
	Status       string `json:"status"` // pending | running | waiting_approval | approved | succeeded | failed | skipped
	Attempt      int    `json:"attempt"`
	Summary      string `json:"summary"`
	ArtifactPath string `json:"artifactPath"`
	Feedback     string `json:"feedback"`
	StartedAt    string `json:"startedAt"`
	FinishedAt   string `json:"finishedAt"`
	DurationMs   int64  `json:"durationMs"`
	ErrorMessage string `json:"errorMessage"`
}

// RunLog stores a single streamed log line from a pipeline stage.
type RunLog struct {
	ID        int64  `json:"id"`
	RunID     string `json:"runId"`
	StageKey  string `json:"stageKey"`
	Stream    string `json:"stream"` // stdout | stderr | system
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

// ReleaseItem tracks a merged PR awaiting or included in a Release.
type ReleaseItem struct {
	ID          string `json:"id"`
	RepoID      string `json:"repoId"`
	RunID       string `json:"runId"`
	PRNumber    int    `json:"prNumber"`
	PRTitle     string `json:"prTitle"`
	CommitType  string `json:"commitType"`
	CommitScope string `json:"commitScope"`
	Summary     string `json:"summary"`
	Breaking    bool   `json:"breaking"`
	MergedAt    string `json:"mergedAt"`
	ReleaseID   string `json:"releaseId"`
}

// ReleaseRecord represents a GitHub Release created immediately or from the Release Train.
type ReleaseRecord struct {
	ID            string `json:"id"`
	RepoID        string `json:"repoId"`
	TagName       string `json:"tagName"`
	PrevTag       string `json:"prevTag"`
	Version       string `json:"version"`
	BumpType      string `json:"bumpType"` // major | minor | patch
	Changelog     string `json:"changelog"`
	Mode          string `json:"mode"`   // immediate | batch
	Status        string `json:"status"` // draft | published | failed
	HTMLURL       string `json:"htmlUrl"`
	ActionsStatus string `json:"actionsStatus"` // none | queued | in_progress | completed_success | completed_failure
	ActionsURL    string `json:"actionsUrl"`
	CreatedAt     string `json:"createdAt"`
	PublishedAt   string `json:"publishedAt"`
}

// DashboardStats aggregates high-level operational counters for the Overview page.
type DashboardStats struct {
	ActiveRuns          int `json:"activeRuns"`
	WaitingApprovals    int `json:"waitingApprovals"`
	BacklogIssues       int `json:"backlogIssues"`
	PendingReleaseItems int `json:"pendingReleaseItems"`
	SucceededRuns       int `json:"succeededRuns"`
	FailedRuns          int `json:"failedRuns"`
	ConnectedRepos      int `json:"connectedRepos"`
	DetectedAgents      int `json:"detectedAgents"`
}

// NowRFC3339 returns the current UTC timestamp formatted consistently.
func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// ResolveStageGates merges autonomy preset defaults with custom repository gate overrides.
func ResolveStageGates(autonomyMode string, customJSON string) map[string]bool {
	return ResolveStageGatesWithTemplate(autonomyMode, "", customJSON)
}

// ResolveStageGatesWithTemplate resolves effective per-stage approval gates.
// When autonomyMode == "template", it uses templateGatesJSON (falling back to merge_release if empty).
// For preset modes (full_auto | semi_auto | step_by_step), it applies the preset and repoCustomJSON overrides.
func ResolveStageGatesWithTemplate(autonomyMode, templateGatesJSON, repoCustomJSON string) map[string]bool {
	gates := map[string]bool{
		StageRequirement:    false,
		StageDesign:         false,
		StageImplementation: false,
		StageVerification:   false,
		StageAIReview:       false,
		StagePRCI:           false,
		StageMergeRelease:   false,
	}

	if autonomyMode == "template" {
		hasAny := false
		if templateGatesJSON != "" && templateGatesJSON != "{}" {
			var tplMap map[string]bool
			if err := json.Unmarshal([]byte(templateGatesJSON), &tplMap); err == nil {
				for k, v := range tplMap {
					gates[k] = v
					hasAny = true
				}
			}
		}
		if !hasAny {
			gates[StageMergeRelease] = true
		}
		return gates
	}

	switch autonomyMode {
	case "full_auto":
		// All gates disabled.
	case "step_by_step":
		gates[StageDesign] = true
		gates[StagePRCI] = true
		gates[StageMergeRelease] = true
	default: // semi_auto
		gates[StageMergeRelease] = true
	}

	if repoCustomJSON != "" && repoCustomJSON != "{}" {
		var custom map[string]bool
		if err := json.Unmarshal([]byte(repoCustomJSON), &custom); err == nil {
			for k, v := range custom {
				gates[k] = v
			}
		}
	}
	return gates
}
