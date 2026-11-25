package router

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"dragon/internal/agent"
	"dragon/internal/config"
	"dragon/internal/store"
)

// RouteRequest encapsulates inputs for Agent, Model & Template routing.
type RouteRequest struct {
	Repo               store.Repository
	Issue              store.Issue
	Rules              []store.RoutingRule
	Profiles           []store.AgentProfile
	Templates          []store.PipelineTemplate
	Providers          []store.ModelProvider
	RoutingConfig      store.RoutingConfig
	Settings           config.AppSettings
	OverrideTemplateID string
	OverrideAgentID    string
	OverrideStages     map[string]string
}

// RoutingDecision describes the resolved Template, primary Agent + Model, per-stage Agents + Models, and explanation.
type RoutingDecision struct {
	TemplateID    string            `json:"templateId"`
	Strategy      string            `json:"strategy"`
	PrimaryAgent  string            `json:"primaryAgent"`
	PrimaryModel  string            `json:"primaryModel"`
	StageAgents   map[string]string `json:"stageAgents"`
	StageModels   map[string]string `json:"stageModels"`
	Reason        string            `json:"reason"`
	MatchedRuleID string            `json:"matchedRuleId,omitempty"`
	UsedAI        bool              `json:"usedAi"`
}

// Router picks the optimal pipeline template, Coding Agent(s), and Model(s) for an Issue.
type Router struct {
	llm *agent.LLMClient
	db  *store.DB
}

// NewRouter creates a Router with an optional lightweight LLMClient and optional store.DB.
func NewRouter(llm *agent.LLMClient, db ...*store.DB) *Router {
	var d *store.DB
	if len(db) > 0 {
		d = db[0]
	}
	return &Router{llm: llm, db: d}
}

// Route evaluates deterministic routing rules first, then applies global RoutingConfig strategy,
// optional AI classification, or heuristic defaults, resolving per-stage Agent & Model bindings.
func (r *Router) Route(ctx context.Context, req RouteRequest) (*RoutingDecision, error) {
	strategy := req.RoutingConfig.Strategy
	if strategy == "" {
		strategy = "rule_ai"
	}

	autoTemplate := req.Repo.DefaultTemplateID == "" || req.Repo.DefaultTemplateID == "auto"
	decision := &RoutingDecision{
		TemplateID:  req.Repo.DefaultTemplateID,
		Strategy:    strategy,
		StageAgents: make(map[string]string),
		StageModels: make(map[string]string),
	}
	if autoTemplate {
		decision.TemplateID = "full_feature"
	}

	agentByID := make(map[string]store.AgentProfile, len(req.Profiles))
	for _, p := range req.Profiles {
		agentByID[p.ID] = p
	}

	// Sort rules by ascending priority.
	rules := make([]store.RoutingRule, 0, len(req.Rules))
	for _, rl := range req.Rules {
		if rl.Enabled {
			rules = append(rules, rl)
		}
	}
	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})

	var ruleStageOverrides map[string]string
	var matchedRuleModel string
	for _, rl := range rules {
		if ruleMatches(rl, req.Repo, req.Issue) {
			decision.MatchedRuleID = rl.ID
			if rl.TemplateID != "" {
				decision.TemplateID = rl.TemplateID
			}
			if rl.AgentID != "" {
				decision.PrimaryAgent = rl.AgentID
			}
			if rl.ModelRef != "" {
				matchedRuleModel = rl.ModelRef
			}
			if rl.StageOverridesJSON != "" && rl.StageOverridesJSON != "{}" {
				_ = json.Unmarshal([]byte(rl.StageOverridesJSON), &ruleStageOverrides)
			}
			decision.Reason = fmt.Sprintf("命中路由规则「%s」(优先级 %d)", rl.Name, rl.Priority)
			break
		}
	}

	// Strategy-driven routing when no rule locked the agent.
	if decision.PrimaryAgent == "" && req.OverrideAgentID == "" {
		switch strategy {
		case "manual":
			if req.RoutingConfig.FallbackAgentID != "" {
				decision.PrimaryAgent = req.RoutingConfig.FallbackAgentID
				tpl, hReason := heuristicClassify(req.Issue, req.Repo.DefaultTemplateID)
				if autoTemplate && decision.MatchedRuleID == "" {
					decision.TemplateID = tpl
					decision.Reason = fmt.Sprintf("手动锁死策略，固定指派 `%s`；%s", decision.PrimaryAgent, hReason)
				} else {
					decision.Reason = fmt.Sprintf("手动锁死策略，固定指派 `%s`", decision.PrimaryAgent)
				}
			}
		case "budget_first":
			best := pickBestAvailableAgent(req.Profiles, []string{"aider", "qoder", "workbuddy", "codebuddy", "codex", "gemini", "opencode", "kimi", "claude"})
			tpl, hReason := heuristicClassify(req.Issue, req.Repo.DefaultTemplateID)
			if autoTemplate && decision.MatchedRuleID == "" {
				decision.TemplateID = tpl
			}
			decision.PrimaryAgent = best
			if ap, ok := agentByID[best]; ok && agent.IsNativeFirstKind(ap.Kind) && ap.ProviderID == "" && ap.Model == "" {
				decision.PrimaryModel = ""
			} else if bm := pickModelByCostTier(req.Providers, "budget"); bm != "" {
				decision.PrimaryModel = bm
			}
			decision.Reason = fmt.Sprintf("成本优先 (Budget) 策略：%s，选用 `%s`", hReason, best)
		case "quality_first":
			best := pickBestAvailableAgent(req.Profiles, []string{"claude", "codex", "agy", "qoder", "codebuddy", "workbuddy", "gemini", "opencode"})
			tpl, hReason := heuristicClassify(req.Issue, req.Repo.DefaultTemplateID)
			if autoTemplate && decision.MatchedRuleID == "" {
				decision.TemplateID = tpl
			}
			decision.PrimaryAgent = best
			if ap, ok := agentByID[best]; ok && agent.IsNativeFirstKind(ap.Kind) && ap.ProviderID == "" && ap.Model == "" {
				decision.PrimaryModel = ""
			} else if ap.SupportsTiers {
				decision.PrimaryModel = "opus"
			} else if fm := pickModelByCostTier(req.Providers, "flagship"); fm != "" {
				decision.PrimaryModel = fm
			}
			decision.Reason = fmt.Sprintf("旗舰质量 (Focus) 策略：%s，选用 `%s`", hReason, best)
		default: // rule_ai
			if r.llm != nil {
				if aiTpl, aiAgent, aiReason, ok := r.tryAIRoute(ctx, req); ok {
					decision.UsedAI = true
					if autoTemplate && decision.MatchedRuleID == "" {
						if aiTpl != "" {
							decision.TemplateID = aiTpl
						} else {
							heuristicTpl, _ := heuristicClassify(req.Issue, req.Repo.DefaultTemplateID)
							decision.TemplateID = heuristicTpl
						}
					}
					decision.PrimaryAgent = aiAgent
					decision.Reason = aiReason
				}
			}
		}
	}

	// Heuristic fallback if still unselected.
	if decision.PrimaryAgent == "" {
		bestAgent := req.RoutingConfig.FallbackAgentID
		if bestAgent == "" {
			bestAgent = pickBestAvailableAgent(req.Profiles, []string{"claude", "codex", "qoder", "workbuddy", "codebuddy", "agy", "gemini", "opencode", "copilot", "kimi", "aider"})
		}
		heuristicTpl, heuristicReason := heuristicClassify(req.Issue, req.Repo.DefaultTemplateID)
		if autoTemplate && decision.MatchedRuleID == "" {
			decision.TemplateID = heuristicTpl
		}
		decision.PrimaryAgent = bestAgent
		if decision.Reason == "" {
			decision.Reason = heuristicReason + "，自动指派可用 Agent `" + bestAgent + "`"
		}
	}

	// Apply manual launch overrides.
	if req.OverrideTemplateID != "" {
		decision.TemplateID = req.OverrideTemplateID
	}
	if req.OverrideAgentID != "" {
		decision.PrimaryAgent = req.OverrideAgentID
		decision.PrimaryModel = ""
		decision.Reason = fmt.Sprintf("手动指定主 Agent `%s`（原建议：%s）", req.OverrideAgentID, decision.Reason)
	}

	if matchedRuleModel != "" && req.OverrideAgentID == "" {
		decision.PrimaryModel = matchedRuleModel
	}
	if decision.PrimaryModel == "" {
		decision.PrimaryModel = formatAgentModelRef(agentByID[decision.PrimaryAgent])
	}

	// Layer per-stage Agent & Model bindings cleanly:
	// 1) Primary agent + model
	// 2) Global RoutingConfig.StageBindings (Agent / Tier / ModelRef)
	// 3) Repo StageAgentsJSON
	// 4) Matched rule StageOverridesJSON
	// 5) Launch OverrideStages
	for _, st := range store.AllStageDescriptors() {
		stageAgent := decision.PrimaryAgent
		stageModel := decision.PrimaryModel

		if b, ok := req.RoutingConfig.StageBindings[st.Key]; ok {
			if b.AgentID != "" {
				stageAgent = b.AgentID
				stageModel = formatAgentModelRef(agentByID[stageAgent])
			}
			if b.ModelRef != "" {
				stageModel = b.ModelRef
			} else if b.Tier != "" {
				stageModel = b.Tier
			}
		}

		decision.StageAgents[st.Key] = stageAgent
		decision.StageModels[st.Key] = stageModel
	}

	applyStageAgentMap := func(overrides map[string]string) {
		for k, v := range overrides {
			if v == "" {
				continue
			}
			decision.StageAgents[k] = v
			decision.StageModels[k] = formatAgentModelRef(agentByID[v])
		}
	}

	if req.Repo.StageAgentsJSON != "" && req.Repo.StageAgentsJSON != "{}" {
		var repoStages map[string]string
		if err := json.Unmarshal([]byte(req.Repo.StageAgentsJSON), &repoStages); err == nil {
			applyStageAgentMap(repoStages)
		}
	}
	applyStageAgentMap(ruleStageOverrides)
	applyStageAgentMap(req.OverrideStages)

	return decision, nil
}

func formatAgentModelRef(p store.AgentProfile) string {
	if p.Model == "" {
		return ""
	}
	if p.ProviderID != "" && !strings.Contains(p.Model, "/") {
		return p.ProviderID + "/" + p.Model
	}
	return p.Model
}

func pickModelByCostTier(providers []store.ModelProvider, wantTier string) string {
	// Only inject models from enabled providers that actually have an API key configured.
	for _, p := range providers {
		if !p.Enabled || !p.HasKey {
			continue
		}
		for _, m := range p.Models() {
			if m.CostTier == wantTier {
				return p.ID + "/" + m.ID
			}
		}
	}
	return ""
}

func ruleMatches(rl store.RoutingRule, repo store.Repository, issue store.Issue) bool {
	if rl.RepoID != "" && rl.RepoID != "*" && rl.RepoID != repo.ID && !strings.EqualFold(rl.RepoID, repo.FullName) {
		return false
	}
	if strings.TrimSpace(rl.LabelMatch) != "" {
		want := strings.ToLower(strings.TrimSpace(rl.LabelMatch))
		matched := false
		for _, lb := range issue.Labels() {
			if strings.ToLower(strings.TrimSpace(lb)) == want {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if strings.TrimSpace(rl.TitleRegex) != "" {
		re, err := regexp.Compile("(?i)" + rl.TitleRegex)
		if err != nil || !re.MatchString(issue.Title) {
			return false
		}
	}
	return true
}

func pickBestAvailableAgent(profiles []store.AgentProfile, preferredOrder []string) string {
	byID := make(map[string]store.AgentProfile, len(profiles))
	for _, p := range profiles {
		byID[p.ID] = p
	}
	for _, id := range preferredOrder {
		if p, ok := byID[id]; ok && p.Enabled && p.Detected {
			return p.ID
		}
	}
	for _, p := range profiles {
		if p.Enabled && p.Detected {
			return p.ID
		}
	}
	for _, id := range preferredOrder {
		if p, ok := byID[id]; ok && p.Enabled {
			return p.ID
		}
	}
	if len(profiles) > 0 {
		return profiles[0].ID
	}
	return "claude"
}

func hasComplexSignals(issue store.Issue) bool {
	complexLabels := []string{
		"complex", "critical", "high-priority", "p0", "p1",
		"security", "breaking", "architecture", "refactor",
		"performance", "concurrency", "race", "memory",
	}
	for _, lb := range issue.Labels() {
		l := strings.ToLower(strings.TrimSpace(lb))
		for _, kw := range complexLabels {
			if strings.Contains(l, kw) {
				return true
			}
		}
	}

	combined := strings.ToLower(issue.Title + "\n" + issue.Body)
	complexKeywords := []string{
		"race", "deadlock", "memory leak", "leak", "concurrency",
		"refactor", "migration", "architecture", "security",
		"vulnerability", "performance", "oom",
		"竞态", "死锁", "内存泄漏", "重构", "架构", "安全漏洞", "性能",
	}
	for _, kw := range complexKeywords {
		if strings.Contains(combined, kw) {
			return true
		}
	}

	bodyTrimmed := strings.TrimSpace(issue.Body)
	if len(bodyTrimmed) >= 600 {
		return true
	}
	checklistCount := strings.Count(bodyTrimmed, "- [ ]") + strings.Count(bodyTrimmed, "* [ ]")
	return checklistCount >= 3
}

func heuristicClassify(issue store.Issue, defaultTpl string) (templateID string, reason string) {
	lowerTitle := strings.ToLower(strings.TrimSpace(issue.Title))
	labels := issue.Labels()

	isDocs := false
	isBug := false
	isFeature := false
	matchedLabel := ""

	for _, lb := range labels {
		l := strings.ToLower(strings.TrimSpace(lb))
		switch {
		case strings.Contains(l, "doc") || strings.Contains(l, "readme") || strings.Contains(l, "chore") || strings.Contains(l, "typo"):
			isDocs = true
			matchedLabel = lb
		case strings.Contains(l, "bug") || strings.Contains(l, "fix") || strings.Contains(l, "hotfix") || strings.Contains(l, "defect"):
			isBug = true
			matchedLabel = lb
		case strings.Contains(l, "feat") || strings.Contains(l, "enhancement"):
			isFeature = true
			matchedLabel = lb
		}
		if matchedLabel != "" {
			break
		}
	}

	if isDocs || strings.HasPrefix(lowerTitle, "docs") || strings.HasPrefix(lowerTitle, "chore") ||
		strings.Contains(lowerTitle, "readme") || strings.Contains(lowerTitle, "typo") ||
		strings.Contains(lowerTitle, "文档") || strings.Contains(lowerTitle, "拼写") {
		if matchedLabel != "" {
			return "docs_chore", fmt.Sprintf("检测到标签 `%s`，归类为文档/维护轻量流", matchedLabel)
		}
		return "docs_chore", "根据 Issue 标题特征匹配轻量文档/维护流"
	}

	if !isBug && (strings.HasPrefix(lowerTitle, "fix") || strings.Contains(lowerTitle, "bug") ||
		strings.Contains(lowerTitle, "crash") || strings.Contains(lowerTitle, "error") ||
		strings.Contains(lowerTitle, "修复") || strings.Contains(lowerTitle, "报错")) {
		isBug = true
	}
	if isBug {
		if hasComplexSignals(issue) {
			return "complex_fix", "检测到复杂故障特征，自动走「复杂修复」7 阶段深度排查流"
		}
		if matchedLabel != "" {
			return "quick_bugfix", fmt.Sprintf("检测到标签 `%s` 且特征轻量，自动走「简单修复」4 阶段快车道", matchedLabel)
		}
		return "quick_bugfix", "识别为轻量缺陷，自动走「简单修复」4 阶段快车道"
	}

	if isFeature || strings.HasPrefix(lowerTitle, "feat") || strings.Contains(lowerTitle, "add ") ||
		strings.Contains(lowerTitle, "support ") || strings.Contains(lowerTitle, "新增") || strings.Contains(lowerTitle, "支持") {
		if matchedLabel != "" {
			return "full_feature", fmt.Sprintf("检测到标签 `%s`，采用「完整功能开发」7 阶段流", matchedLabel)
		}
		return "full_feature", "识别为功能开发需求，采用「完整功能开发」7 阶段流"
	}

	if hasComplexSignals(issue) {
		return "complex_fix", "检测到较高复杂度特征，自动走「复杂修复」7 阶段深度排查流"
	}
	if defaultTpl != "" && defaultTpl != "auto" {
		return defaultTpl, "采用仓库默认 SDLC 流水线模板"
	}
	return "full_feature", "未匹配特定类型特征，默认采用「完整功能开发」7 阶段流水线"
}

func (r *Router) tryAIRoute(ctx context.Context, req RouteRequest) (templateID, agentID, reason string, ok bool) {
	var agentLines []string
	validAgents := map[string]bool{}
	for _, p := range req.Profiles {
		if p.Enabled && p.Detected {
			validAgents[p.ID] = true
			agentLines = append(agentLines, fmt.Sprintf("- %s (%s, model=%s): %s", p.ID, p.Name, p.Model, p.CapabilitiesJSON))
		}
	}
	if len(agentLines) == 0 {
		return "", "", "", false
	}

	var tplLines []string
	validTpls := map[string]bool{}
	for _, t := range req.Templates {
		validTpls[t.ID] = true
		tplLines = append(tplLines, fmt.Sprintf("- %s: %s", t.ID, t.Description))
	}

	sysPrompt := `You are Dragon's SDLC Issue Router. Choose the best pipeline template and coding agent for the GitHub Issue.
Respond ONLY with raw JSON: {"templateId":"...","agentId":"...","reason":"concise Chinese reason"}.`

	userPrompt := fmt.Sprintf("Repository: %s\nIssue #%d: %s\nLabels: %s\nBody:\n%s\n\nAvailable Templates:\n%s\n\nAvailable Detected Agents:\n%s",
		req.Repo.FullName, req.Issue.Number, req.Issue.Title, req.Issue.LabelsJSON,
		truncateStr(req.Issue.Body, 1500), strings.Join(tplLines, "\n"), strings.Join(agentLines, "\n"))

	resp, err := r.llm.CompleteWithModelRef(ctx, r.db, req.Settings, req.RoutingConfig.RouterModelRef, sysPrompt, userPrompt)
	if err != nil {
		return "", "", "", false
	}

	cleaned := strings.TrimSpace(resp)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var parsed struct {
		TemplateID string `json:"templateId"`
		AgentID    string `json:"agentId"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return "", "", "", false
	}
	if !validAgents[parsed.AgentID] {
		return "", "", "", false
	}
	if !validTpls[parsed.TemplateID] {
		parsed.TemplateID = ""
	}
	if parsed.Reason == "" {
		parsed.Reason = "AI 智能评估 Issue 复杂度并指派最佳 Agent"
	}
	return parsed.TemplateID, parsed.AgentID, parsed.Reason, true
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
