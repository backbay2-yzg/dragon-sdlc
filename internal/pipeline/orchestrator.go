package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"dragon/internal/agent"
	"dragon/internal/config"
	"dragon/internal/gitops"
	"dragon/internal/hosting"
	"dragon/internal/release"
	"dragon/internal/router"
	"dragon/internal/store"
)

// StartRunRequest specifies parameters to launch a pipeline run for an issue.
type StartRunRequest struct {
	RepoID             string            `json:"repoId"`
	IssueID            string            `json:"issueId"`
	OverrideTemplateID string            `json:"overrideTemplateId"`
	OverrideAgentID    string            `json:"overrideAgentId"`
	OverrideStages     map[string]string `json:"overrideStages"`
}

type approvalDecision struct {
	approved bool
	feedback string
}

type activeRunHandle struct {
	cancel     context.CancelFunc
	approvalCh chan approvalDecision
}

// Orchestrator runs the 7-stage SDLC state machine with bounded concurrency and Human-in-the-Loop gates.
type Orchestrator struct {
	db           *store.DB
	paths        config.Paths
	provider     hosting.Provider
	git          *gitops.Manager
	registry     *agent.Registry
	router       *router.Router
	releaseMgr   *release.Manager
	events       EventSink
	notifier     Notifier
	tokenResolve TokenResolver

	// Configurable polling interval for CI checks (overridden to milliseconds in tests).
	ciPollInterval time.Duration

	poolMu     sync.Mutex
	poolCond   *sync.Cond
	maxWorkers int
	busyCount  int

	activeMu   sync.Mutex
	activeRuns map[string]*activeRunHandle
	baseCtx    context.Context
}

// NewOrchestrator constructs the SDLC Pipeline Orchestrator.
func NewOrchestrator(
	baseCtx context.Context,
	db *store.DB,
	paths config.Paths,
	provider hosting.Provider,
	git *gitops.Manager,
	registry *agent.Registry,
	rtr *router.Router,
	releaseMgr *release.Manager,
	events EventSink,
	notifier Notifier,
	tokenResolve TokenResolver,
	maxConcurrency int,
) *Orchestrator {
	if maxConcurrency < 1 {
		maxConcurrency = 2
	}
	o := &Orchestrator{
		db:             db,
		paths:          paths,
		provider:       provider,
		git:            git,
		registry:       registry,
		router:         rtr,
		releaseMgr:     releaseMgr,
		events:         events,
		notifier:       notifier,
		tokenResolve:   tokenResolve,
		ciPollInterval: 15 * time.Second,
		maxWorkers:     maxConcurrency,
		activeRuns:     make(map[string]*activeRunHandle),
		baseCtx:        baseCtx,
	}
	o.poolCond = sync.NewCond(&o.poolMu)
	return o
}

// SetMaxConcurrency dynamically updates the concurrent pipeline worker limit.
func (o *Orchestrator) SetMaxConcurrency(n int) {
	if n < 1 {
		n = 1
	}
	if n > 6 {
		n = 6
	}
	o.poolMu.Lock()
	o.maxWorkers = n
	o.poolCond.Broadcast()
	o.poolMu.Unlock()
}

// SetCIPollInterval overrides the CI check polling interval (useful in tests).
func (o *Orchestrator) SetCIPollInterval(d time.Duration) {
	if d > 0 {
		o.ciPollInterval = d
	}
}

func (o *Orchestrator) acquireSlot(ctx context.Context) error {
	o.poolMu.Lock()
	defer o.poolMu.Unlock()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			o.poolMu.Lock()
			o.poolCond.Broadcast()
			o.poolMu.Unlock()
		case <-done:
		}
	}()

	for o.busyCount >= o.maxWorkers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		o.poolCond.Wait()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	o.busyCount++
	return nil
}

func (o *Orchestrator) releaseSlot() {
	o.poolMu.Lock()
	if o.busyCount > 0 {
		o.busyCount--
	}
	o.poolCond.Signal()
	o.poolMu.Unlock()
}

// EnqueueAutoIssue implements poller.IssueStarter for trigger-labeled issues.
func (o *Orchestrator) EnqueueAutoIssue(ctx context.Context, repo store.Repository, issue store.Issue) error {
	_, err := o.StartIssueRun(ctx, StartRunRequest{
		RepoID:  repo.ID,
		IssueID: issue.ID,
	})
	return err
}

// StartIssueRun routes an Issue and queues a new 7-stage SDLC PipelineRun.
func (o *Orchestrator) StartIssueRun(ctx context.Context, req StartRunRequest) (*store.PipelineRun, error) {
	repo, err := o.db.GetRepository(req.RepoID)
	if err != nil {
		return nil, err
	}
	ws := o.git.ValidateLocalWorkspace(ctx, repo.LocalPath, repo.FullName)
	if !ws.OK {
		if ws.Status == gitops.WorkspaceStatusUnconfigured {
			return nil, errors.New("该仓库尚未配置本地工作区目录，请先在「仓库管理 -> 配置」中指定工作区文件夹")
		}
		return nil, fmt.Errorf("本地工作区目录不可用: %s", ws.Message)
	}
	issue, err := o.db.GetIssue(req.IssueID)
	if err != nil {
		return nil, err
	}
	rules, _ := o.db.ListRoutingRules()
	profiles, _ := o.db.ListAgentProfiles()
	templates, _ := o.db.ListTemplates()
	providers, _ := o.db.ListModelProviders()
	routingCfg, _ := o.db.GetRoutingConfig()
	settings, _ := o.db.GetAppSettings()

	decision, err := o.router.Route(ctx, router.RouteRequest{
		Repo:               repo,
		Issue:              issue,
		Rules:              rules,
		Profiles:           profiles,
		Templates:          templates,
		Providers:          providers,
		RoutingConfig:      routingCfg,
		Settings:           settings,
		OverrideTemplateID: req.OverrideTemplateID,
		OverrideAgentID:    req.OverrideAgentID,
		OverrideStages:     req.OverrideStages,
	})
	if err != nil {
		return nil, err
	}

	tpl, err := o.db.GetTemplate(decision.TemplateID)
	if err != nil {
		tpl, err = o.db.GetTemplate("full_feature")
		if err != nil {
			return nil, fmt.Errorf("未找到流水线模板: %w", err)
		}
	}

	runID := uuid.NewString()
	branchName := fmt.Sprintf("auto/issue-%d-%s", issue.Number, runID[:6])
	worktreePath := o.git.WorktreePathForRun(repo.LocalPath, issue.Number, runID)

	run := store.PipelineRun{
		ID:            runID,
		RepoID:        repo.ID,
		IssueID:       issue.ID,
		IssueNumber:   issue.Number,
		IssueTitle:    issue.Title,
		TemplateID:    tpl.ID,
		Status:        "queued",
		CurrentStage:  "",
		RoutedAgentID: decision.PrimaryAgent,
		RoutingReason: decision.Reason,
		BranchName:    branchName,
		WorktreePath:  worktreePath,
		CIStatus:      "none",
		StartedAt:     store.NowRFC3339(),
	}

	enabledStages := map[string]bool{}
	for _, key := range tpl.Stages() {
		enabledStages[key] = true
	}

	var stages []store.StageExecution
	for _, desc := range store.AllStageDescriptors() {
		stStatus := "pending"
		if !enabledStages[desc.Key] {
			stStatus = "skipped"
		}
		agID := decision.StageAgents[desc.Key]
		if agID == "" {
			agID = decision.PrimaryAgent
		}
		stages = append(stages, store.StageExecution{
			ID:         uuid.NewString(),
			RunID:      runID,
			StageKey:   desc.Key,
			StageName:  desc.Name,
			OrderIndex: desc.Order,
			AgentID:    agID,
			ModelRef:   decision.StageModels[desc.Key],
			Status:     stStatus,
		})
	}

	if err := o.db.CreatePipelineRun(run, stages); err != nil {
		return nil, err
	}
	_ = o.db.UpdateIssueStatus(issue.ID, "queued")
	o.emit("dragon:run:updated", run)

	runCtx, cancel := context.WithCancel(o.baseCtx)
	handle := &activeRunHandle{
		cancel:     cancel,
		approvalCh: make(chan approvalDecision, 1),
	}
	o.activeMu.Lock()
	o.activeRuns[runID] = handle
	o.activeMu.Unlock()

	go o.executeRun(runCtx, handle, run, repo, issue)
	return &run, nil
}

// RetryRun starts a fresh pipeline run for the same issue of a failed or cancelled run.
func (o *Orchestrator) RetryRun(ctx context.Context, runID string) (*store.PipelineRun, error) {
	oldRun, err := o.db.GetPipelineRun(runID)
	if err != nil {
		return nil, err
	}
	return o.StartIssueRun(ctx, StartRunRequest{
		RepoID:             oldRun.RepoID,
		IssueID:            oldRun.IssueID,
		OverrideTemplateID: oldRun.TemplateID,
		OverrideAgentID:    oldRun.RoutedAgentID,
	})
}

// ApproveRun approves a waiting gate checkpoint so the pipeline resumes.
func (o *Orchestrator) ApproveRun(runID string) error {
	o.activeMu.Lock()
	handle, ok := o.activeRuns[runID]
	o.activeMu.Unlock()
	if !ok {
		return errors.New("当前流水线未处于运行或等待审批状态")
	}
	select {
	case handle.approvalCh <- approvalDecision{approved: true}:
		return nil
	default:
		return errors.New("已有待处理的审批响应")
	}
}

// RequestChanges rejects the current approval checkpoint with maintainer feedback and triggers rework.
func (o *Orchestrator) RequestChanges(runID, feedback string) error {
	o.activeMu.Lock()
	handle, ok := o.activeRuns[runID]
	o.activeMu.Unlock()
	if !ok {
		return errors.New("当前流水线未处于运行或等待审批状态")
	}
	if strings.TrimSpace(feedback) == "" {
		feedback = "请根据评审要求修改实现并重新验证。"
	}
	select {
	case handle.approvalCh <- approvalDecision{approved: false, feedback: feedback}:
		return nil
	default:
		return errors.New("已有待处理的审批响应")
	}
}

// AbortRun cancels an active or waiting pipeline run and cleans up its isolated worktree.
func (o *Orchestrator) AbortRun(runID string) error {
	o.activeMu.Lock()
	handle, ok := o.activeRuns[runID]
	o.activeMu.Unlock()
	if ok {
		handle.cancel()
	}
	run, err := o.db.GetPipelineRun(runID)
	if err != nil {
		return err
	}
	if run.Status == "succeeded" || run.Status == "cancelled" {
		return nil
	}
	run.Status = "cancelled"
	run.FinishedAt = store.NowRFC3339()
	run.ErrorMessage = "已被用户手动中止"
	_ = o.db.UpdatePipelineRun(run)
	_ = o.db.UpdateIssueStatus(run.IssueID, "backlog")
	if repo, err := o.db.GetRepository(run.RepoID); err == nil {
		if repo.LocalPath != "" {
			_ = o.git.RemoveWorktreeFromLocal(context.Background(), repo.LocalPath, run.WorktreePath)
		} else {
			_ = o.git.RemoveWorktree(context.Background(), repo.FullName, run.WorktreePath)
		}
	}
	o.emit("dragon:run:updated", run)
	return nil
}

// CleanupRunWorktree removes a retained worktree directory after a finished or failed run.
func (o *Orchestrator) CleanupRunWorktree(runID string) error {
	run, err := o.db.GetPipelineRun(runID)
	if err != nil {
		return err
	}
	if run.Status == "queued" || run.Status == "running" || run.Status == "waiting_approval" {
		return errors.New("流水线仍在运行或等待审批中，无法清理工作区")
	}
	if strings.TrimSpace(run.WorktreePath) == "" {
		return nil
	}
	if repo, err := o.db.GetRepository(run.RepoID); err == nil {
		if repo.LocalPath != "" {
			return o.git.RemoveWorktreeFromLocal(context.Background(), repo.LocalPath, run.WorktreePath)
		}
		return o.git.RemoveWorktree(context.Background(), repo.FullName, run.WorktreePath)
	}
	return os.RemoveAll(run.WorktreePath)
}

func (o *Orchestrator) executeRun(ctx context.Context, handle *activeRunHandle, run store.PipelineRun, repo store.Repository, issue store.Issue) {
	defer func() {
		o.activeMu.Lock()
		delete(o.activeRuns, run.ID)
		o.activeMu.Unlock()
		handle.cancel()
	}()

	if err := o.acquireSlot(ctx); err != nil {
		o.failRun(&run, &issue, "", err)
		return
	}
	slotHeld := true
	defer func() {
		if slotHeld {
			o.releaseSlot()
		}
	}()

	run.Status = "running"
	_ = o.db.UpdatePipelineRun(run)
	_ = o.db.UpdateIssueStatus(issue.ID, "running")
	o.emit("dragon:run:updated", run)

	o.log(run.ID, "system", "system", fmt.Sprintf("开始处理 Issue #%d: %s (路由 Agent: %s — %s)", issue.Number, issue.Title, run.RoutedAgentID, run.RoutingReason))

	if repo.SyncLabels && o.provider != nil {
		_ = o.provider.AddIssueLabels(ctx, repo.Owner, repo.Name, issue.Number, []string{"dragon:in-progress"})
	}

	// Prepare mirror and isolated worktree.
	token := ""
	if o.tokenResolve != nil {
		token = o.tokenResolve()
	}
	cloneURL := fmt.Sprintf("https://github.com/%s.git", repo.FullName)
	if o.provider != nil {
		if remoteRepo, err := o.provider.GetRepo(ctx, repo.Owner, repo.Name); err == nil {
			if remoteRepo.CloneURL != "" {
				cloneURL = remoteRepo.CloneURL
			}
			if remoteRepo.DefaultBranch != "" {
				repo.DefaultBranch = remoteRepo.DefaultBranch
			}
		}
	}

	o.log(run.ID, "system", "system", fmt.Sprintf("准备本地仓库 (%s) 与隔离 Worktree (%s -> %s)", repo.LocalPath, repo.DefaultBranch, run.WorktreePath))
	if err := o.git.EnsureLocalRepo(ctx, repo.LocalPath, repo.FullName, cloneURL, token); err != nil {
		o.failRun(&run, &issue, "", fmt.Errorf("准备本地仓库失败: %w", err))
		return
	}
	if err := o.git.CreateWorktreeFromLocal(ctx, repo.LocalPath, repo.DefaultBranch, run.BranchName, run.WorktreePath); err != nil {
		o.failRun(&run, &issue, "", fmt.Errorf("创建隔离 Worktree 失败: %w", err))
		return
	}

	stages, err := o.db.ListStagesByRun(run.ID)
	if err != nil {
		o.failRun(&run, &issue, "", err)
		return
	}

	tplDefaultGates := ""
	if tpl, tplErr := o.db.GetTemplate(run.TemplateID); tplErr == nil {
		tplDefaultGates = tpl.DefaultGatesJSON
	}
	gates := store.ResolveStageGatesWithTemplate(repo.AutonomyMode, tplDefaultGates, repo.StageGatesJSON)
	artifacts := map[string]string{}

	for idx := 0; idx < len(stages); idx++ {
		if ctx.Err() != nil {
			return
		}
		st := &stages[idx]
		if st.Status == "skipped" {
			continue
		}

		// Pre-stage gate for irreversible GitHub actions (PR creation / Merge & Release).
		isPreGate := (st.StageKey == store.StagePRCI || st.StageKey == store.StageMergeRelease) && gates[st.StageKey]
		if isPreGate && st.Status != "approved" {
			o.releaseSlot()
			slotHeld = false
			dec, waitErr := o.waitForApproval(ctx, handle, &run, &issue, st, "等待人工批准以继续执行「"+st.StageName+"」")
			if waitErr != nil {
				return
			}
			if err := o.acquireSlot(ctx); err != nil {
				o.failRun(&run, &issue, st.StageKey, err)
				return
			}
			slotHeld = true

			if !dec.approved {
				st.Feedback = dec.feedback
				st.Status = "pending"
				_ = o.db.UpdateStageExecution(*st)
				o.emit("dragon:stage:updated", *st)
				o.log(run.ID, st.StageKey, "system", "审批退回修改，回退至编码实现阶段: "+dec.feedback)

				// Roll back to implementation stage with maintainer feedback injected.
				implIdx := findStageIndex(stages, store.StageImplementation)
				if implIdx >= 0 {
					for j := implIdx; j <= idx; j++ {
						if stages[j].Status != "skipped" {
							stages[j].Status = "pending"
							if stages[j].StageKey == store.StageImplementation {
								stages[j].Feedback = dec.feedback
							}
							_ = o.db.UpdateStageExecution(stages[j])
							o.emit("dragon:stage:updated", stages[j])
						}
					}
					idx = implIdx - 1
					continue
				}
			} else {
				st.Status = "approved"
				_ = o.db.UpdateStageExecution(*st)
				o.emit("dragon:stage:updated", *st)
			}
		}

		run.CurrentStage = st.StageKey
		run.Status = "running"
		_ = o.db.UpdatePipelineRun(run)
		o.emit("dragon:run:updated", run)

		st.Attempt++
		st.Status = "running"
		st.StartedAt = store.NowRFC3339()
		st.ErrorMessage = ""
		_ = o.db.UpdateStageExecution(*st)
		o.emit("dragon:stage:updated", *st)

		stageStart := time.Now()
		stageErr := o.runSingleStage(ctx, &run, repo, issue, st, stages, artifacts)
		st.FinishedAt = store.NowRFC3339()
		st.DurationMs = time.Since(stageStart).Milliseconds()

		if stageErr != nil {
			if ctx.Err() != nil {
				return
			}
			st.Status = "failed"
			st.ErrorMessage = stageErr.Error()
			_ = o.db.UpdateStageExecution(*st)
			o.emit("dragon:stage:updated", *st)
			o.failRun(&run, &issue, st.StageKey, stageErr)
			return
		}

		st.Status = "succeeded"
		_ = o.db.UpdateStageExecution(*st)
		o.emit("dragon:stage:updated", *st)

		// Post-stage gate for specification/design/code/review stages.
		isPostGate := (st.StageKey != store.StagePRCI && st.StageKey != store.StageMergeRelease) && gates[st.StageKey]
		if isPostGate {
			o.releaseSlot()
			slotHeld = false
			dec, waitErr := o.waitForApproval(ctx, handle, &run, &issue, st, "阶段「"+st.StageName+"」已完成，等待人工确认产出")
			if waitErr != nil {
				return
			}
			if err := o.acquireSlot(ctx); err != nil {
				o.failRun(&run, &issue, st.StageKey, err)
				return
			}
			slotHeld = true

			if !dec.approved {
				st.Feedback = dec.feedback
				st.Status = "pending"
				_ = o.db.UpdateStageExecution(*st)
				o.emit("dragon:stage:updated", *st)
				o.log(run.ID, st.StageKey, "system", "产出被退回修改，将重新执行本阶段: "+dec.feedback)
				idx-- // Re-run same stage with feedback.
				continue
			}
		}
	}

	// Clean up isolated worktree before marking the run succeeded so UI and callers see the cleaned state immediately.
	_ = o.git.RemoveWorktreeFromLocal(context.Background(), repo.LocalPath, run.WorktreePath)

	run.Status = "succeeded"
	run.FinishedAt = store.NowRFC3339()
	_ = o.db.UpdatePipelineRun(run)
	_ = o.db.UpdateIssueStatus(issue.ID, "completed")
	o.emit("dragon:run:updated", run)
	o.log(run.ID, "system", "system", "全部流水线阶段已完成！")

	if o.notifier != nil {
		o.notifier.NotifyRunSucceeded(run)
	}
}

func (o *Orchestrator) runSingleStage(
	ctx context.Context,
	run *store.PipelineRun,
	repo store.Repository,
	issue store.Issue,
	st *store.StageExecution,
	allStages []store.StageExecution,
	artifacts map[string]string,
) error {
	sysPrompt := buildStageSystemPrompt(repo)

	switch st.StageKey {
	case store.StageRequirement:
		out, err := o.invokeAgent(ctx, run.ID, st.StageKey, st.AgentID, st.ModelRef, run.WorktreePath, sysPrompt, buildRequirementPrompt(repo, issue, st.Feedback), true)
		if err != nil {
			return err
		}
		artifacts[store.StageRequirement] = out
		st.ArtifactPath = o.saveArtifact(run.ID, "spec.md", out)
		st.Summary = summarizeText(out)
		if repo.DraftClarifications && strings.Contains(out, "[NEEDS_CLARIFICATION]") {
			o.saveArtifact(run.ID, "clarification_draft.md", out)
			o.log(run.ID, st.StageKey, "system", "检测到需求存在不明确之处，已保存澄清草稿 clarification_draft.md")
		}
		return nil

	case store.StageDesign:
		out, err := o.invokeAgent(ctx, run.ID, st.StageKey, st.AgentID, st.ModelRef, run.WorktreePath, sysPrompt, buildDesignPrompt(repo, issue, artifacts[store.StageRequirement], st.Feedback), true)
		if err != nil {
			return err
		}
		artifacts[store.StageDesign] = out
		st.ArtifactPath = o.saveArtifact(run.ID, "plan.md", out)
		st.Summary = summarizeText(out)
		return nil

	case store.StageImplementation:
		prompt := buildImplementationPrompt(repo, issue, artifacts[store.StageRequirement], artifacts[store.StageDesign], st.Feedback)
		out, err := o.invokeAgent(ctx, run.ID, st.StageKey, st.AgentID, st.ModelRef, run.WorktreePath, sysPrompt, prompt, false)
		if err != nil {
			return err
		}
		artifacts[store.StageImplementation] = out
		st.ArtifactPath = o.saveArtifact(run.ID, "implementation.md", out)
		st.Summary = summarizeText(out)
		return nil

	case store.StageVerification:
		return o.runVerificationStage(ctx, run, repo, issue, st, allStages, sysPrompt, artifacts)

	case store.StageAIReview:
		diff, err := o.git.GetDiff(ctx, run.WorktreePath, repo.DefaultBranch)
		if err != nil {
			return fmt.Errorf("提取 Diff 失败: %w", err)
		}
		if strings.TrimSpace(diff) == "" {
			st.Summary = "无代码变更，跳过深度审查"
			return nil
		}
		o.saveArtifact(run.ID, "changes.diff", diff)
		out, err := o.invokeAgent(ctx, run.ID, st.StageKey, st.AgentID, st.ModelRef, run.WorktreePath, sysPrompt, buildAIReviewPrompt(repo, issue, artifacts[store.StageRequirement], diff, st.Feedback), true)
		if err != nil {
			return err
		}
		artifacts[store.StageAIReview] = out
		st.ArtifactPath = o.saveArtifact(run.ID, "review.md", out)
		st.Summary = summarizeText(out)

		if strings.Contains(out, "CRITICAL:") {
			o.log(run.ID, st.StageKey, "system", "AI 代码审查发现 CRITICAL 问题，自动触发一轮修复与验证")
			implAgent, implModel := stageAgentAndModelFor(allStages, store.StageImplementation, run.RoutedAgentID, "")
			fixPrompt := buildImplementationPrompt(repo, issue, artifacts[store.StageRequirement], artifacts[store.StageDesign], "AI Review CRITICAL findings:\n"+out)
			if _, err := o.invokeAgent(ctx, run.ID, st.StageKey, implAgent, implModel, run.WorktreePath, sysPrompt, fixPrompt, false); err != nil {
				return err
			}
			if err := o.runVerifyCommands(ctx, run.ID, st.StageKey, repo, run.WorktreePath); err != nil {
				return fmt.Errorf("审查修复后验证失败: %w", err)
			}
		}
		return nil

	case store.StagePRCI:
		return o.runPRAndCIStage(ctx, run, repo, issue, st, allStages, sysPrompt, artifacts)

	case store.StageMergeRelease:
		return o.runMergeReleaseStage(ctx, run, repo, st)
	}

	return nil
}

func (o *Orchestrator) runVerificationStage(
	ctx context.Context,
	run *store.PipelineRun,
	repo store.Repository,
	issue store.Issue,
	st *store.StageExecution,
	allStages []store.StageExecution,
	sysPrompt string,
	artifacts map[string]string,
) error {
	if strings.TrimSpace(repo.BuildCmd) == "" && strings.TrimSpace(repo.LintCmd) == "" && strings.TrimSpace(repo.TestCmd) == "" {
		o.log(run.ID, st.StageKey, "system", "仓库未配置 Build/Lint/Test 命令，跳过本地命令验证。")
		st.Summary = "未配置验证命令（已跳过）"
		return nil
	}

	maxRetries := repo.MaxTestRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	implAgent, implModel := stageAgentAndModelFor(allStages, store.StageImplementation, run.RoutedAgentID, st.ModelRef)

	var lastReport strings.Builder
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			o.log(run.ID, st.StageKey, "system", fmt.Sprintf("测试验证失败，正在调度 Agent `%s` 进行第 %d/%d 轮自动修复...", implAgent, attempt, maxRetries))
			if _, err := o.invokeAgent(ctx, run.ID, st.StageKey, implAgent, implModel, run.WorktreePath, sysPrompt, buildTestFixPrompt(repo, issue, lastReport.String(), attempt), false); err != nil {
				return err
			}
		}
		err := o.runVerifyCommands(ctx, run.ID, st.StageKey, repo, run.WorktreePath)
		if err == nil {
			report := fmt.Sprintf("# Verification Passed\n\nAttempts used: %d\n- Build: `%s`\n- Lint: `%s`\n- Test: `%s`\n",
				attempt+1, repo.BuildCmd, repo.LintCmd, repo.TestCmd)
			artifacts[store.StageVerification] = report
			st.ArtifactPath = o.saveArtifact(run.ID, "verification.md", report)
			st.Summary = fmt.Sprintf("构建与测试通过（共执行 %d 轮）", attempt+1)
			return nil
		}
		lastReport.Reset()
		lastReport.WriteString(err.Error())
		if attempt == maxRetries {
			st.ArtifactPath = o.saveArtifact(run.ID, "verification_failed.md", lastReport.String())
			return fmt.Errorf("验证命令重试 %d 次后仍失败:\n%s", maxRetries, truncateForPrompt(lastReport.String(), 2000))
		}
	}
	return nil
}

func (o *Orchestrator) runVerifyCommands(ctx context.Context, runID, stageKey string, repo store.Repository, workDir string) error {
	cmds := []struct {
		label string
		cmd   string
	}{
		{"Build", repo.BuildCmd},
		{"Lint", repo.LintCmd},
		{"Test", repo.TestCmd},
	}
	for _, c := range cmds {
		if strings.TrimSpace(c.cmd) == "" {
			continue
		}
		o.log(runID, stageKey, "system", fmt.Sprintf("执行 %s 命令: %s", c.label, c.cmd))
		out, err := gitops.RunShellCommand(ctx, workDir, c.cmd)
		if strings.TrimSpace(out) != "" {
			o.log(runID, stageKey, "stdout", truncateForPrompt(out, 4000))
		}
		if err != nil {
			return fmt.Errorf("%s 命令失败 (`%s`): %v\n%s", c.label, c.cmd, err, out)
		}
	}
	return nil
}

func (o *Orchestrator) runPRAndCIStage(
	ctx context.Context,
	run *store.PipelineRun,
	repo store.Repository,
	issue store.Issue,
	st *store.StageExecution,
	allStages []store.StageExecution,
	sysPrompt string,
	artifacts map[string]string,
) error {
	if o.provider == nil {
		return errors.New("未配置代码托管 Provider，无法提交 PR")
	}

	changed, err := o.git.HasChanges(ctx, run.WorktreePath, repo.DefaultBranch)
	if err != nil {
		return err
	}
	if !changed && run.PRNumber == 0 {
		return errors.New("工作区没有任何代码变更，无法创建 Pull Request")
	}

	token := ""
	if o.tokenResolve != nil {
		token = o.tokenResolve()
	}

	prTitle := buildPRTitle(issue)
	commitMsg := fmt.Sprintf("%s\n\nCloses #%d", prTitle, issue.Number)

	maxCIRetries := repo.MaxCIRetries
	if maxCIRetries < 0 {
		maxCIRetries = 0
	}
	implAgent, implModel := stageAgentAndModelFor(allStages, store.StageImplementation, run.RoutedAgentID, st.ModelRef)

	for ciAttempt := 0; ciAttempt <= maxCIRetries; ciAttempt++ {
		sha, err := o.git.CommitAndPush(ctx, run.WorktreePath, run.BranchName, commitMsg, token)
		if err != nil {
			return fmt.Errorf("提交并推送分支失败: %w", err)
		}
		o.log(run.ID, st.StageKey, "system", fmt.Sprintf("已推送分支 `%s` (commit %s)", run.BranchName, shortSHA(sha)))

		if run.PRNumber == 0 {
			pr, err := o.provider.CreatePullRequest(ctx, repo.Owner, repo.Name, hosting.CreatePRRequest{
				Title: prTitle,
				Body:  buildPRBody(issue, artifacts[store.StageRequirement], artifacts[store.StageAIReview], run.RoutedAgentID),
				Head:  run.BranchName,
				Base:  repo.DefaultBranch,
			})
			if err != nil {
				return err
			}
			run.PRNumber = pr.Number
			run.PRURL = pr.HTMLURL
			run.CIStatus = "pending"
			_ = o.db.UpdatePipelineRun(*run)
			_ = o.db.UpdateIssueStatus(issue.ID, "pr_open")
			o.emit("dragon:run:updated", *run)
			o.log(run.ID, st.StageKey, "system", fmt.Sprintf("已创建 PR #%d: %s", pr.Number, pr.HTMLURL))

			if repo.SyncComments {
				_ = o.provider.CreateIssueComment(ctx, repo.Owner, repo.Name, issue.Number,
					fmt.Sprintf("已由 Dragon 自动提交 Pull Request #%d (%s)。", pr.Number, pr.HTMLURL))
			}
		}

		// Watch CI checks for current HEAD SHA.
		ciSummary, err := o.watchCIChecks(ctx, repo, sha)
		if err != nil {
			return err
		}
		run.CIStatus = ciSummary.OverallState
		_ = o.db.UpdatePipelineRun(*run)
		o.emit("dragon:run:updated", *run)

		if ciSummary.OverallState == "success" || ciSummary.OverallState == "none" {
			st.Summary = fmt.Sprintf("PR #%d 已就绪 (CI: %s)", run.PRNumber, ciSummary.OverallState)
			return nil
		}

		// CI failed!
		if ciAttempt == maxCIRetries {
			return fmt.Errorf("PR #%d CI 检查失败，已达到最大自动修复次数 (%d)", run.PRNumber, maxCIRetries)
		}

		var logsBuf strings.Builder
		for _, rID := range ciSummary.FailedRunIDs {
			if l, err := o.provider.GetFailedActionLogs(ctx, repo.Owner, repo.Name, rID); err == nil && l != "" {
				logsBuf.WriteString(l + "\n")
			}
		}
		if logsBuf.Len() == 0 {
			for _, chk := range ciSummary.Checks {
				if chk.Conclusion != "" && chk.Conclusion != "success" {
					logsBuf.WriteString(fmt.Sprintf("Check %s failed (%s): %s\n", chk.Name, chk.Conclusion, chk.HTMLURL))
				}
			}
		}

		o.log(run.ID, st.StageKey, "system", fmt.Sprintf("PR #%d CI 检查失败，正在提取失败日志交由 Agent `%s` 进行第 %d/%d 次修复...", run.PRNumber, implAgent, ciAttempt+1, maxCIRetries))
		if _, err := o.invokeAgent(ctx, run.ID, st.StageKey, implAgent, implModel, run.WorktreePath, sysPrompt, buildCIFixPrompt(repo, issue, logsBuf.String(), ciAttempt+1), false); err != nil {
			return err
		}
		commitMsg = fmt.Sprintf("fix: address CI failure (attempt %d)", ciAttempt+1)
	}
	return nil
}

func (o *Orchestrator) watchCIChecks(ctx context.Context, repo store.Repository, sha string) (*hosting.CheckSummary, error) {
	// Wait briefly for GitHub Actions to register check runs, then poll until terminal or timeout.
	maxPolls := 60 // ~15 minutes at 15s default
	for i := 0; i < maxPolls; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(o.ciPollInterval):
			}
		}
		sum, err := o.provider.GetPRChecks(ctx, repo.Owner, repo.Name, sha)
		if err != nil {
			if i >= 2 {
				return &hosting.CheckSummary{OverallState: "none"}, nil
			}
			continue
		}
		switch sum.OverallState {
		case "success", "failure":
			return sum, nil
		case "none":
			// Give Actions 2 poll cycles to spin up; if still none, repo has no CI checks.
			if i >= 1 {
				return sum, nil
			}
		}
	}
	return &hosting.CheckSummary{OverallState: "pending"}, errors.New("等待 PR CI 检查超时")
}

func (o *Orchestrator) runMergeReleaseStage(ctx context.Context, run *store.PipelineRun, repo store.Repository, st *store.StageExecution) error {
	if o.provider == nil {
		return errors.New("未配置代码托管 Provider，无法合并 PR")
	}
	if run.PRNumber <= 0 {
		return errors.New("缺少有效 PR 编号，无法执行合并与发布")
	}

	prTitle := buildPRTitle(store.Issue{Title: run.IssueTitle, LabelsJSON: "[]"})
	if iss, err := o.db.GetIssue(run.IssueID); err == nil {
		prTitle = buildPRTitle(iss)
	}

	res, err := o.provider.MergePullRequest(ctx, repo.Owner, repo.Name, run.PRNumber, repo.MergeStrategy, prTitle, fmt.Sprintf("Closes #%d", run.IssueNumber))
	if err != nil {
		return err
	}
	if !res.Merged {
		return fmt.Errorf("PR #%d 未能成功合并: %s", run.PRNumber, res.Message)
	}
	o.log(run.ID, st.StageKey, "system", fmt.Sprintf("PR #%d 已通过 %s 策略合并 (%s)", run.PRNumber, repo.MergeStrategy, shortSHA(res.SHA)))

	if o.releaseMgr != nil {
		item, err := o.releaseMgr.RecordMergedPR(repo, *run, prTitle)
		if err != nil {
			return err
		}
		if repo.ReleaseMode == "immediate" {
			rec, err := o.releaseMgr.PublishRelease(ctx, release.PublishRequest{
				RepoID:  repo.ID,
				Mode:    "immediate",
				ItemIDs: []string{item.ID},
			})
			if err != nil {
				return err
			}
			o.log(run.ID, st.StageKey, "system", fmt.Sprintf("已自动发布 GitHub Release %s (%s)", rec.TagName, rec.HTMLURL))
			st.Summary = fmt.Sprintf("已合并 PR #%d 并即时发布 %s", run.PRNumber, rec.TagName)
			return nil
		}
		st.Summary = fmt.Sprintf("已合并 PR #%d 并加入批量发版队列", run.PRNumber)
		return nil
	}

	st.Summary = fmt.Sprintf("已合并 PR #%d", run.PRNumber)
	return nil
}

func (o *Orchestrator) waitForApproval(
	ctx context.Context,
	handle *activeRunHandle,
	run *store.PipelineRun,
	issue *store.Issue,
	st *store.StageExecution,
	msg string,
) (approvalDecision, error) {
	run.Status = "waiting_approval"
	run.CurrentStage = st.StageKey
	_ = o.db.UpdatePipelineRun(*run)
	_ = o.db.UpdateIssueStatus(issue.ID, "waiting_approval")
	o.emit("dragon:run:updated", *run)

	st.Status = "waiting_approval"
	_ = o.db.UpdateStageExecution(*st)
	o.emit("dragon:stage:updated", *st)

	o.log(run.ID, st.StageKey, "system", msg)
	if o.notifier != nil {
		o.notifier.NotifyApprovalNeeded(*run, *st)
	}

	select {
	case <-ctx.Done():
		return approvalDecision{}, ctx.Err()
	case dec := <-handle.approvalCh:
		return dec, nil
	}
}

func (o *Orchestrator) invokeAgent(ctx context.Context, runID, stageKey, agentID, modelRef, workDir, sysPrompt, prompt string, readOnly bool) (string, error) {
	adapter, profile, resolvedModel, providerEnv, err := o.registry.ResolveInvocation(agentID, modelRef)
	if err != nil {
		return "", err
	}
	modelLabel := resolvedModel
	if modelLabel == "" {
		modelLabel = "default"
	}
	o.log(runID, stageKey, "system", fmt.Sprintf("调用 Agent: %s (%s · model=%s)", profile.Name, profile.ID, modelLabel))
	res, err := adapter.Run(ctx, agent.RunRequest{
		WorkDir:        workDir,
		Prompt:         prompt,
		SystemPrompt:   sysPrompt,
		Model:          resolvedModel,
		PermissionMode: profile.PermissionMode,
		Env:            providerEnv,
		ReadOnly:       readOnly,
	}, func(ev agent.StreamEvent) {
		o.log(runID, stageKey, ev.Stream, ev.Text)
	})
	if err != nil {
		return "", err
	}
	return res.Output, nil
}

func (o *Orchestrator) saveArtifact(runID, filename, content string) string {
	dir := filepath.Join(o.paths.ArtifactsDir, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	p := filepath.Join(dir, filename)
	_ = os.WriteFile(p, []byte(content), 0o644)
	return p
}

func (o *Orchestrator) log(runID, stageKey, stream, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	entry, err := o.db.AppendRunLog(runID, stageKey, stream, content)
	if err == nil {
		o.emit("dragon:log:chunk", entry)
	}
}

func (o *Orchestrator) failRun(run *store.PipelineRun, issue *store.Issue, stageKey string, err error) {
	run.Status = "failed"
	run.FinishedAt = store.NowRFC3339()
	run.ErrorMessage = err.Error()
	_ = o.db.UpdatePipelineRun(*run)
	_ = o.db.UpdateIssueStatus(issue.ID, "failed")
	o.log(run.ID, stageKey, "stderr", "流水线执行失败: "+err.Error())
	if run.WorktreePath != "" {
		if info, statErr := os.Stat(run.WorktreePath); statErr == nil && info.IsDir() {
			o.log(run.ID, stageKey, "system", "已保留失败现场工作区以便排查: "+run.WorktreePath)
		}
	}
	o.emit("dragon:run:updated", *run)
	if o.notifier != nil {
		o.notifier.NotifyRunFailed(*run, err.Error())
	}
}

func (o *Orchestrator) emit(name string, payload any) {
	if o.events != nil {
		o.events.Emit(name, payload)
	}
}

func findStageIndex(stages []store.StageExecution, key string) int {
	for i, s := range stages {
		if s.StageKey == key {
			return i
		}
	}
	return -1
}

func stageAgentAndModelFor(stages []store.StageExecution, key, fallbackAgent, fallbackModel string) (string, string) {
	for _, s := range stages {
		if s.StageKey == key && s.AgentID != "" {
			return s.AgentID, s.ModelRef
		}
	}
	return fallbackAgent, fallbackModel
}

func summarizeText(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for _, l := range lines {
		t := strings.TrimSpace(strings.TrimLeft(l, "#>-* "))
		if len(t) > 6 {
			if len(t) > 140 {
				return t[:140] + "..."
			}
			return t
		}
	}
	return "已完成"
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
