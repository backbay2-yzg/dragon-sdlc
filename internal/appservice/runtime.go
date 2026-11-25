package appservice

import (
	"context"
	"log"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"dragon/internal/agent"
	"dragon/internal/config"
	"dragon/internal/gitops"
	"dragon/internal/hosting"
	"dragon/internal/hosting/github"
	"dragon/internal/pipeline"
	"dragon/internal/poller"
	"dragon/internal/release"
	"dragon/internal/router"
	"dragon/internal/secrets"
	"dragon/internal/store"
)

// NavigateEvent instructs the frontend to focus a specific tab, run, or repo.
type NavigateEvent struct {
	Tab    string `json:"tab"`
	RunID  string `json:"runId"`
	RepoID string `json:"repoId"`
}

// AppCore aggregates Dragon's backend engines and Wails runtime references.
type AppCore struct {
	DB           *store.DB
	Paths        config.Paths
	Secrets      secrets.Store
	AuthMgr      *github.AuthManager
	GitHubClient *github.Client
	Provider     hosting.Provider
	GitMgr       *gitops.Manager
	Registry     *agent.Registry
	LLM          *agent.LLMClient
	Router       *router.Router
	ReleaseMgr   *release.Manager
	Orchestrator *pipeline.Orchestrator
	Poller       *poller.Poller

	App        *application.App
	MainWindow application.Window
	NotifSvc   *notifications.NotificationService
	notifReady atomic.Bool

	BaseCtx context.Context
	Cancel  context.CancelFunc

	settingsMu sync.RWMutex
	settings   config.AppSettings
}

// BootstrapOptions customizes AppCore construction (primarily for tests).
type BootstrapOptions struct {
	CustomRoot string
	DSN        string
	Secrets    secrets.Store
	Provider   hosting.Provider
}

// Bootstrap initializes storage, credential store, Git/Agent/Router/Pipeline/Release/Poller engines.
func Bootstrap(opts BootstrapOptions) (*AppCore, error) {
	paths, err := config.ResolvePaths(opts.CustomRoot)
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return nil, err
	}

	dsn := opts.DSN
	if dsn == "" {
		dsn = paths.DBPath
	}
	db, err := store.Open(dsn)
	if err != nil {
		return nil, err
	}

	_, _ = db.RecoverOrphanedRuns()

	settings, err := db.GetAppSettings()
	if err != nil {
		settings = config.DefaultSettings()
	}

	sec := opts.Secrets
	if sec == nil {
		sec = secrets.NewDefaultStore()
	}

	ctx, cancel := context.WithCancel(context.Background())
	core := &AppCore{
		DB:       db,
		Paths:    paths,
		Secrets:  sec,
		BaseCtx:  ctx,
		Cancel:   cancel,
		settings: settings,
	}

	core.AuthMgr = github.NewAuthManager(sec)

	var provider hosting.Provider
	if opts.Provider != nil {
		provider = opts.Provider
	} else {
		ghClient := github.NewClient(github.DefaultAPIBaseURL, func(c context.Context) (string, string, error) {
			s := core.GetSettingsSnapshot()
			return core.AuthMgr.ResolveToken(c, s.AuthMethod)
		})
	core.GitHubClient = ghClient
		provider = ghClient
	}
	core.Provider = provider

	core.GitMgr = gitops.NewManager(paths.ReposDir, paths.WorktreesDir)
	core.Registry = agent.NewRegistry(db, sec)
	migrateNativeFirstAgentDefaults(db, sec)
	core.LLM = agent.NewLLMClient(sec)
	core.Router = router.NewRouter(core.LLM, db)
	core.ReleaseMgr = release.NewManager(db, provider, core, core)

	tokenResolver := func() string {
		s := core.GetSettingsSnapshot()
		tok, _, err := core.AuthMgr.ResolveToken(core.BaseCtx, s.AuthMethod)
		if err != nil {
			return ""
		}
		return tok
	}

	core.Orchestrator = pipeline.NewOrchestrator(
		ctx,
		db,
		paths,
		provider,
		core.GitMgr,
		core.Registry,
		core.Router,
		core.ReleaseMgr,
		core,
		core,
		tokenResolver,
		settings.MaxConcurrency,
	)

	core.Poller = poller.NewPoller(db, provider, core.Orchestrator, core)
	return core, nil
}

// Shutdown cleanly stops background polling, cancels in-flight pipelines, and closes SQLite.
func (c *AppCore) Shutdown() {
	if c.Poller != nil {
		c.Poller.Stop()
	}
	if c.Cancel != nil {
		c.Cancel()
	}
	if c.DB != nil {
		_ = c.DB.Close()
	}
}

// GetSettingsSnapshot returns the current AppSettings thread-safely.
func (c *AppCore) GetSettingsSnapshot() config.AppSettings {
	c.settingsMu.RLock()
	defer c.settingsMu.RUnlock()
	return c.settings
}

// SetSettingsSnapshot updates cached settings and adjusts runtime concurrency.
func (c *AppCore) SetSettingsSnapshot(s config.AppSettings) {
	c.settingsMu.Lock()
	c.settings = s
	c.settingsMu.Unlock()
	if c.Orchestrator != nil {
		c.Orchestrator.SetMaxConcurrency(s.MaxConcurrency)
	}
}

// Emit forwards backend events onto the Wails v3 event bus when attached.
func (c *AppCore) Emit(eventName string, payload any) {
	if c.App != nil {
		c.App.Event.Emit(eventName, payload)
	}
}

// HandleNotificationClick brings the main window to foreground and directs the UI to the target item.
func (c *AppCore) HandleNotificationClick(tab, runID, repoID string) {
	if c.MainWindow != nil {
		c.MainWindow.Show().Focus()
	}
	c.Emit("dragon:navigate", NavigateEvent{
		Tab:    tab,
		RunID:  runID,
		RepoID: repoID,
	})
}

func (c *AppCore) sendToast(id, title, body string, data map[string]any) {
	if !c.GetSettingsSnapshot().EnableToastNotifications || !c.notifReady.Load() || c.NotifSvc == nil {
		return
	}
	_ = c.NotifSvc.SendNotification(notifications.NotificationOptions{
		ID:    id,
		Title: title,
		Body:  body,
		Data:  data,
	})
}

// NotifyApprovalNeeded sends a toast when a Human-in-the-Loop gate pauses a pipeline.
func (c *AppCore) NotifyApprovalNeeded(run store.PipelineRun, stage store.StageExecution) {
	c.sendToast(
		"approval-"+run.ID,
		"Dragon 等待人工审批",
		"Issue #"+itoa(run.IssueNumber)+" "+run.IssueTitle+" 暂停在「"+stage.StageName+"」",
		map[string]any{"tab": "pipelines", "runId": run.ID, "repoId": run.RepoID},
	)
}

// NotifyRunFailed sends a toast when a pipeline terminates with an error.
func (c *AppCore) NotifyRunFailed(run store.PipelineRun, errMsg string) {
	c.sendToast(
		"failed-"+run.ID,
		"Dragon 流水线执行阻塞",
		"Issue #"+itoa(run.IssueNumber)+" 执行失败："+truncateToast(errMsg, 120),
		map[string]any{"tab": "pipelines", "runId": run.ID, "repoId": run.RepoID},
	)
}

// NotifyRunSucceeded sends a toast when an issue completes the SDLC pipeline.
func (c *AppCore) NotifyRunSucceeded(run store.PipelineRun) {
	c.sendToast(
		"ok-"+run.ID,
		"Dragon 流水线完成",
		"Issue #"+itoa(run.IssueNumber)+" "+run.IssueTitle+" 已完成处理",
		map[string]any{"tab": "pipelines", "runId": run.ID, "repoId": run.RepoID},
	)
}

// NotifyReleasePublished sends a toast when a GitHub Release is published.
func (c *AppCore) NotifyReleasePublished(repo store.Repository, rec store.ReleaseRecord) {
	c.sendToast(
		"rel-"+rec.ID,
		"Dragon 已发布新版本 "+rec.TagName,
		repo.FullName+" 已成功创建 GitHub Release "+rec.TagName,
		map[string]any{"tab": "releases", "repoId": repo.ID},
	)
}

// SafeNotificationService wraps Wails v3's NotificationService so startup registry issues never crash the app.
type SafeNotificationService struct {
	core *AppCore
	inner *notifications.NotificationService
}

// NewSafeNotificationService constructs a resilient notification service wrapper.
func NewSafeNotificationService(core *AppCore) *SafeNotificationService {
	inner := notifications.New()
	core.NotifSvc = inner
	inner.OnNotificationResponse(func(res notifications.NotificationResult) {
		if res.Error != nil {
			return
		}
		tab, _ := res.Response.UserInfo["tab"].(string)
		runID, _ := res.Response.UserInfo["runId"].(string)
		repoID, _ := res.Response.UserInfo["repoId"].(string)
		if tab == "" {
			tab = "pipelines"
		}
		core.HandleNotificationClick(tab, runID, repoID)
	})
	return &SafeNotificationService{core: core, inner: inner}
}

func (s *SafeNotificationService) ServiceName() string {
	return "dragon/safe-notifications"
}

func (s *SafeNotificationService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if err := s.inner.ServiceStartup(ctx, options); err != nil {
		log.Printf("warning: native notifications disabled (%v)", err)
		s.core.notifReady.Store(false)
		return nil
	}
	s.core.notifReady.Store(true)
	return nil
}

func (s *SafeNotificationService) ServiceShutdown() error {
	if s.core.notifReady.Load() {
		return s.inner.ServiceShutdown()
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func truncateToast(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func migrateNativeFirstAgentDefaults(db *store.DB, sec secrets.Store) {
	const migrationKey = "migration_native_cli_defaults_v1"
	if db.GetAppSettingRaw(migrationKey) == "done" {
		return
	}
	legacySeedModels := map[string]bool{
		"gpt-5.3-codex":  true,
		"gpt-5.4":        true,
		"deepseek-chat":  true,
		"gemini-2.5-pro": true,
	}
	for _, agID := range []string{"codex", "agy"} {
		ag, err := db.GetAgentProfile(agID)
		if err != nil {
			continue
		}
		changed := false
		if ag.ProviderID != "" && !sec.Has(secrets.ProviderSecretKey(ag.ProviderID)) && legacySeedModels[ag.Model] {
			ag.ProviderID = ""
			ag.Model = ""
			changed = true
		}
		if agID == "codex" && ag.PermissionMode == "full-auto" {
			ag.PermissionMode = "workspace-write"
			changed = true
		}
		if changed {
			_ = db.UpsertAgentProfile(ag)
		}
	}
	_ = db.SetAppSettingRaw(migrationKey, "done")
}
