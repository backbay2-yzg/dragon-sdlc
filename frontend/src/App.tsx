import { useCallback, useEffect, useState } from "react";
import { FluentProvider } from "@fluentui/react-components";
import {
  DashboardService,
  SettingsService,
} from "../bindings/dragon/internal/appservice/index.js";
import { TitleBar } from "./components/TitleBar.js";
import { NavigationRail } from "./components/NavigationRail.js";
import { DashboardPage } from "./pages/DashboardPage.js";
import { IssuesPage } from "./pages/IssuesPage.js";
import { PipelinesPage } from "./pages/PipelinesPage.js";
import { ReleasesPage } from "./pages/ReleasesPage.js";
import { AgentsPage } from "./pages/AgentsPage.js";
import { ReposPage } from "./pages/ReposPage.js";
import { GitHubAuthDialog } from "./components/GitHubAuthDialog.js";
import { AppSettingsDialog } from "./components/AppSettingsDialog.js";
import { useWailsEvents } from "./hooks/useWailsEvents.js";
import { dragonDarkTheme, dragonLightTheme, resolveEffectiveDark } from "./theme/dragonTheme.js";
import type {
  AppSettings,
  AuthStatusDetail,
  DashboardOverview,
  NavTab,
  RunLog,
} from "./types/dragon.js";

const defaultSettings: AppSettings = {
  themeMode: "system",
  backdropMaterial: "mica",
  maxConcurrency: 2,
  minimizeToTray: true,
  enableToastNotifications: true,
  defaultAutonomyMode: "template",
  defaultWorkspaceRoot: "",
  authMethod: "gh_cli",
  oauthClientId: "",
  llmProvider: "none",
  llmBaseUrl: "https://api.openai.com/v1",
  llmModel: "gpt-4o-mini",
};

export function App() {
  const [activeTab, setActiveTab] = useState<NavTab>("dashboard");
  const [selectedRepoId, setSelectedRepoId] = useState<string>("all");
  const [selectedRunId, setSelectedRunId] = useState<string>("");
  const [overview, setOverview] = useState<DashboardOverview | null>(null);
  const [settings, setSettings] = useState<AppSettings>(defaultSettings);
  const [auth, setAuth] = useState<AuthStatusDetail | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [syncing, setSyncing] = useState<boolean>(false);
  const [refreshSignal, setRefreshSignal] = useState<number>(0);
  const [liveLog, setLiveLog] = useState<RunLog | null>(null);
  const [authDialogOpen, setAuthDialogOpen] = useState<boolean>(false);
  const [settingsDialogOpen, setSettingsDialogOpen] = useState<boolean>(false);
  const [pendingConfigRepoId, setPendingConfigRepoId] = useState<string | null>(null);
  const [agentsSubViewReq, setAgentsSubViewReq] = useState<
    { view: "agents" | "providers" | "routing" | "templates"; nonce: number } | undefined
  >(undefined);
  const [systemDark, setSystemDark] = useState<boolean>(() =>
    window.matchMedia("(prefers-color-scheme: dark)").matches
  );

  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const listener = (e: MediaQueryListEvent) => setSystemDark(e.matches);
    mq.addEventListener("change", listener);
    return () => mq.removeEventListener("change", listener);
  }, []);

  const isDark = resolveEffectiveDark(settings.themeMode, systemDark);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", isDark ? "dark" : "light");
    document.documentElement.setAttribute("data-material", settings.backdropMaterial || "mica");
  }, [isDark, settings.backdropMaterial]);

  const loadGlobalData = useCallback(async () => {
    try {
      const [ov, cfg, authRes] = await Promise.all([
        DashboardService.GetOverview(),
        SettingsService.GetSettings(),
        SettingsService.GetAuthStatus(),
      ]);
      if (ov) setOverview(ov);
      if (cfg) setSettings(cfg);
      if (authRes) setAuth(authRes);
    } catch (e) {
      console.error("Failed to load Dragon overview:", e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadGlobalData();
  }, [loadGlobalData]);

  const bumpRefresh = useCallback(() => {
    setRefreshSignal((v) => v + 1);
    void loadGlobalData();
  }, [loadGlobalData]);

  useWailsEvents({
    onRunUpdated: useCallback(() => bumpRefresh(), [bumpRefresh]),
    onStageUpdated: useCallback(() => setRefreshSignal((v) => v + 1), []),
    onLogChunk: useCallback((chunk: RunLog) => setLiveLog(chunk), []),
    onIssuesSynced: useCallback(() => bumpRefresh(), [bumpRefresh]),
    onReleaseUpdated: useCallback(() => bumpRefresh(), [bumpRefresh]),
    onNavigate: useCallback((nav) => {
      if (nav.repoId) setSelectedRepoId(nav.repoId);
      if (nav.runId) setSelectedRunId(nav.runId);
      if (nav.tab) {
        setActiveTab((nav.tab === "settings" ? "repos" : nav.tab) as NavTab);
      }
    }, []),
  });

  const handleSyncAll = async () => {
    setSyncing(true);
    try {
      await DashboardService.SyncAll();
      bumpRefresh();
    } finally {
      setSyncing(false);
    }
  };

  const handleToggleTheme = async () => {
    const nextMode = isDark ? "light" : "dark";
    const updated = await SettingsService.SaveSettings({ ...settings, themeMode: nextMode });
    if (updated) setSettings(updated);
  };

  const handleNavigate = (tab: NavTab, runId?: string) => {
    if (runId) setSelectedRunId(runId);
    setActiveTab(tab);
  };

  const repos = overview?.repos || [];

  useEffect(() => {
    if (selectedRepoId !== "all" && !repos.some((r) => r.id === selectedRepoId)) {
      setSelectedRepoId("all");
    }
  }, [repos, selectedRepoId]);

  const stats = overview?.stats || {
    activeRuns: 0,
    waitingApprovals: 0,
    backlogIssues: 0,
    pendingReleaseItems: 0,
    succeededRuns: 0,
    failedRuns: 0,
    connectedRepos: 0,
    detectedAgents: 0,
  };

  return (
    <FluentProvider
      theme={isDark ? dragonDarkTheme : dragonLightTheme}
      style={{ height: "100%", width: "100%", background: "transparent" }}
    >
      <div className="dragon-shell">
        <TitleBar
          repos={repos}
          selectedRepoId={selectedRepoId}
          onSelectRepo={setSelectedRepoId}
          syncing={syncing}
          onSyncAll={() => void handleSyncAll()}
          isDark={isDark}
          onToggleTheme={() => void handleToggleTheme()}
        />

        <div className="dragon-body">
          <NavigationRail
            activeTab={activeTab}
            onSelectTab={setActiveTab}
            stats={stats}
            auth={auth}
            onOpenAuth={() => setAuthDialogOpen(true)}
            onOpenSettings={() => setSettingsDialogOpen(true)}
          />

          <main className="dragon-main">
            {activeTab === "dashboard" && (
              <DashboardPage
                overview={overview}
                loading={loading}
                onNavigate={handleNavigate}
                onRefresh={bumpRefresh}
              />
            )}
            {activeTab === "issues" && (
              <IssuesPage
                repos={repos}
                selectedRepoId={selectedRepoId}
                onRunStarted={(runId) => {
                  setSelectedRunId(runId);
                  setActiveTab("pipelines");
                  bumpRefresh();
                }}
                onConfigureRepo={(repoId) => {
                  setPendingConfigRepoId(repoId);
                  setActiveTab("repos");
                }}
                refreshSignal={refreshSignal}
              />
            )}
            {activeTab === "pipelines" && (
              <PipelinesPage
                selectedRepoId={selectedRepoId}
                selectedRunId={selectedRunId}
                onSelectRun={setSelectedRunId}
                liveLog={liveLog}
                refreshSignal={refreshSignal}
              />
            )}
            {activeTab === "releases" && (
              <ReleasesPage
                repos={repos}
                selectedRepoId={selectedRepoId}
                refreshSignal={refreshSignal}
              />
            )}
            {activeTab === "agents" && (
              <AgentsPage repos={repos} requestedSubView={agentsSubViewReq} />
            )}
            {activeTab === "repos" && (
              <ReposPage
                repos={repos}
                auth={auth}
                defaultAutonomyMode={settings.defaultAutonomyMode}
                defaultWorkspaceRoot={settings.defaultWorkspaceRoot}
                pendingConfigRepoId={pendingConfigRepoId}
                onClearPendingConfigRepo={() => setPendingConfigRepoId(null)}
                onOpenAuth={() => setAuthDialogOpen(true)}
                onJumpToTemplates={() => {
                  setAgentsSubViewReq({ view: "templates", nonce: Date.now() });
                  setActiveTab("agents");
                }}
                onRefreshAll={bumpRefresh}
              />
            )}
          </main>
        </div>

        <GitHubAuthDialog
          open={authDialogOpen}
          onOpenChange={setAuthDialogOpen}
          auth={auth}
          settings={settings}
          onAuthChanged={loadGlobalData}
          onSettingsUpdated={setSettings}
        />

        <AppSettingsDialog
          open={settingsDialogOpen}
          onOpenChange={setSettingsDialogOpen}
          settings={settings}
          auth={auth}
          onSettingsUpdated={setSettings}
        />
      </div>
    </FluentProvider>
  );
}

export default App;
