import { useCallback, useEffect, useState } from "react";
import {
  Badge,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Dropdown,
  Input,
  MessageBar,
  MessageBarBody,
  Option,
  Spinner,
} from "@fluentui/react-components";
import {
  ArrowSync20Regular,
  Open16Regular,
  Play20Regular,
  Search20Regular,
  Sparkle20Regular,
} from "@fluentui/react-icons";
import { Browser } from "@wailsio/runtime";
import {
  AgentService,
  IssueService,
  RepoService,
} from "../../bindings/dragon/internal/appservice/index.js";
import type {
  AgentProfile,
  Issue,
  PipelineTemplate,
  Repository,
  RoutingDecision,
} from "../types/dragon.js";
import { formatShortTime, parseLabels } from "../types/dragon.js";
import { StatusBadge } from "../components/StatusBadge.js";

interface IssuesPageProps {
  repos: Repository[];
  selectedRepoId: string;
  onRunStarted: (runId: string) => void;
  onConfigureRepo: (repoId: string) => void;
  refreshSignal: number;
}

export function IssuesPage({
  repos,
  selectedRepoId,
  onRunStarted,
  onConfigureRepo,
  refreshSignal,
}: IssuesPageProps) {
  const [issues, setIssues] = useState<Issue[]>([]);
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [search, setSearch] = useState<string>("");
  const [loading, setLoading] = useState<boolean>(false);
  const [syncMsg, setSyncMsg] = useState<string>("");
  const [errMsg, setErrMsg] = useState<string>("");

  // Launch dialog state
  const [launchIssue, setLaunchIssue] = useState<Issue | null>(null);
  const [routePreview, setRoutePreview] = useState<RoutingDecision | null>(null);
  const [agents, setAgents] = useState<AgentProfile[]>([]);
  const [templates, setTemplates] = useState<PipelineTemplate[]>([]);
  const [chosenTemplate, setChosenTemplate] = useState<string>("");
  const [chosenAgent, setChosenAgent] = useState<string>("");
  const [launching, setLaunching] = useState<boolean>(false);

  const loadIssues = useCallback(async () => {
    setLoading(true);
    setErrMsg("");
    try {
      const list = await IssueService.ListIssues(selectedRepoId || "all", statusFilter, search);
      setIssues(list || []);
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setLoading(false);
    }
  }, [selectedRepoId, statusFilter, search]);

  useEffect(() => {
    void loadIssues();
  }, [loadIssues, refreshSignal]);

  const handleSync = async () => {
    if (repos.length === 0) return;
    setLoading(true);
    setErrMsg("");
    setSyncMsg("");
    try {
      const targetId = selectedRepoId !== "all" ? selectedRepoId : repos[0].id;
      const res = await RepoService.SyncRepo(targetId);
      if (res) {
        setSyncMsg(
          res.notModified
            ? `仓库 ${res.fullName} 无变更 (304 Not Modified，剩余配额 ${res.rateLimitRemaining})`
            : `已同步 ${res.fullName}：${res.syncedCount} 条 Issue，自动入队 ${res.autoEnqueuedCount} 条`
        );
      }
      await loadIssues();
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setLoading(false);
    }
  };

  const openLaunchDialog = async (issue: Issue) => {
    setLaunchIssue(issue);
    setRoutePreview(null);
    setLaunching(false);
    try {
      const [preview, ags, tpls] = await Promise.all([
        IssueService.PreviewRoute(issue.repoId, issue.id),
        AgentService.ListAgents(),
        AgentService.ListTemplates(),
      ]);
      setAgents(ags || []);
      setTemplates(tpls || []);
      if (preview) {
        setRoutePreview(preview);
        setChosenTemplate(preview.templateId);
        setChosenAgent(preview.primaryAgent);
      }
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  const confirmLaunch = async () => {
    if (!launchIssue) return;
    setLaunching(true);
    setErrMsg("");
    try {
      const run = await IssueService.StartIssueRun({
        repoId: launchIssue.repoId,
        issueId: launchIssue.id,
        overrideTemplateId: chosenTemplate,
        overrideAgentId: chosenAgent,
        overrideStages: {},
      });
      setLaunchIssue(null);
      if (run) {
        onRunStarted(run.id);
      }
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setLaunching(false);
    }
  };

  const handleIgnoreToggle = async (issue: Issue) => {
    if (issue.status === "ignored") {
      await IssueService.ReopenIssue(issue.id);
    } else {
      await IssueService.IgnoreIssue(issue.id);
    }
    await loadIssues();
  };

  const repoNameMap = new Map(repos.map((r) => [r.id, r.fullName]));
  const launchRepo = launchIssue ? repos.find((r) => r.id === launchIssue.repoId) : undefined;
  const launchMissingWorkspace = Boolean(launchIssue && !launchRepo?.localPath);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 600 }}>Issue 待办与准入队列</h1>
          <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
            匹配仓库触发标签（如 <code>dragon-auto</code>）的 Issue 将自动启动，也可手动预览路由并一键执行
          </div>
        </div>
        <Button
          appearance="primary"
          disabled={loading || repos.length === 0}
          icon={loading ? <Spinner size="extra-tiny" /> : <ArrowSync20Regular />}
          onClick={handleSync}
        >
          立即同步 Issue
        </Button>
      </div>

      {syncMsg && (
        <MessageBar intent="success">
          <MessageBarBody>{syncMsg}</MessageBarBody>
        </MessageBar>
      )}
      {errMsg && (
        <MessageBar intent="error">
          <MessageBarBody>{errMsg}</MessageBarBody>
        </MessageBar>
      )}

      {/* Filter Bar */}
      <div className="dragon-card" style={{ padding: "10px 14px", display: "flex", gap: 10, alignItems: "center" }}>
        <Input
          contentBefore={<Search20Regular />}
          placeholder="搜索 Issue 标题、编号或正文..."
          value={search}
          onChange={(_, d) => setSearch(d.value)}
          style={{ flex: 1 }}
        />
        <Dropdown
          value={statusFilter === "all" ? "全部状态" : statusFilter}
          selectedOptions={[statusFilter]}
          onOptionSelect={(_, d) => d.optionValue && setStatusFilter(d.optionValue)}
          style={{ minWidth: 150 }}
        >
          <Option value="all">全部状态</Option>
          <Option value="backlog">待办 Backlog</Option>
          <Option value="queued">排队中</Option>
          <Option value="running">执行中</Option>
          <Option value="waiting_approval">等待审批</Option>
          <Option value="pr_open">PR 已提交</Option>
          <Option value="completed">已完成</Option>
          <Option value="failed">失败</Option>
          <Option value="ignored">已忽略</Option>
        </Dropdown>
      </div>

      {/* Issue List */}
      {issues.length === 0 ? (
        <div className="dragon-card" style={{ padding: 36, textAlign: "center", color: "var(--dragon-text-secondary)" }}>
          {repos.length === 0
            ? "尚未连接任何 GitHub 仓库，请先到「仓库管理」接入你的开源项目。"
            : "当前筛选条件下没有匹配的 Open Issue。"}
        </div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          {issues.map((iss) => {
            const labels = parseLabels(iss.labelsJson);
            const canLaunch = iss.status === "backlog" || iss.status === "failed" || iss.status === "ignored";
            return (
              <div
                key={iss.id}
                className="dragon-card dragon-card-interactive"
                style={{ padding: "13px 16px", display: "flex", justifyContent: "space-between", alignItems: "center", gap: 16 }}
              >
                <div style={{ minWidth: 0, flex: 1 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                    <span style={{ fontWeight: 700, color: "var(--dragon-accent)" }} className="tabular-nums">
                      #{iss.number}
                    </span>
                    <span style={{ fontWeight: 600, fontSize: 14.5 }}>{iss.title}</span>
                    <StatusBadge status={iss.status} />
                    {iss.triggerMatched && (
                      <Badge appearance="filled" color="brand" size="small">
                        命中自动触发标签
                      </Badge>
                    )}
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 6, flexWrap: "wrap" }}>
                    <span style={{ fontSize: 12, color: "var(--dragon-text-secondary)" }}>
                      {repoNameMap.get(iss.repoId) || iss.repoId} · @{iss.author} · 更新于 {formatShortTime(iss.updatedAt)}
                    </span>
                    {labels.map((lb) => (
                      <Badge key={lb} appearance="outline" size="small">
                        {lb}
                      </Badge>
                    ))}
                  </div>
                </div>

                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  {iss.htmlUrl && (
                    <Button
                      size="small"
                      appearance="subtle"
                      icon={<Open16Regular />}
                      onClick={() => void Browser.OpenURL(iss.htmlUrl)}
                    >
                      GitHub
                    </Button>
                  )}
                  <Button size="small" appearance="subtle" onClick={() => void handleIgnoreToggle(iss)}>
                    {iss.status === "ignored" ? "恢复待办" : "忽略"}
                  </Button>
                  <Button
                    size="small"
                    appearance="primary"
                    disabled={!canLaunch}
                    icon={<Play20Regular />}
                    onClick={() => void openLaunchDialog(iss)}
                  >
                    启动自动化流
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Launch Dialog with Intelligent Route Preview */}
      <Dialog open={launchIssue !== null} onOpenChange={(_, d) => !d.open && setLaunchIssue(null)}>
        <DialogSurface style={{ maxWidth: 560 }}>
          <DialogBody>
            <DialogTitle>启动 SDLC 自动化流水线</DialogTitle>
            <DialogContent style={{ display: "flex", flexDirection: "column", gap: 14, marginTop: 8 }}>
              {launchIssue && (
                <div className="dragon-subtle-box" style={{ padding: 12 }}>
                  <div style={{ fontWeight: 600, fontSize: 14 }}>
                    #{launchIssue.number} {launchIssue.title}
                  </div>
                  <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)", marginTop: 4, maxHeight: 72, overflow: "hidden" }}>
                    {launchIssue.body || "（无详细正文）"}
                  </div>
                </div>
              )}

              {launchIssue && (
                launchRepo?.localPath ? (
                  <div className="dragon-subtle-box" style={{ padding: "10px 12px", fontSize: 12 }}>
                    <div>
                      执行工作区: <code>{launchRepo.localPath}</code>
                    </div>
                    <div style={{ color: "var(--dragon-text-secondary)", marginTop: 2 }}>
                      流水线将在同级 <code>.dragon-worktrees</code> 目录中创建隔离分支 Worktree 执行。
                    </div>
                  </div>
                ) : (
                  <MessageBar intent="warning">
                    <MessageBarBody>
                      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, flexWrap: "wrap" }}>
                        <span>该仓库尚未配置本地工作区目录，配置后才能启动自动化流水线。</span>
                        <Button
                          size="small"
                          appearance="outline"
                          onClick={() => {
                            const rid = launchIssue.repoId;
                            setLaunchIssue(null);
                            onConfigureRepo(rid);
                          }}
                        >
                          前往配置工作区
                        </Button>
                      </div>
                    </MessageBarBody>
                  </MessageBar>
                )
              )}

              {routePreview ? (
                <div className="dragon-subtle-box" style={{ padding: 12, borderColor: "rgba(15, 108, 189, 0.35)" }}>
                  <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 6, fontWeight: 600, fontSize: 13 }}>
                      <Sparkle20Regular style={{ color: "var(--dragon-accent)" }} />
                      智能路由推荐结果
                    </div>
                    <div style={{ display: "flex", gap: 6 }}>
                      {routePreview.strategy && (
                        <Badge size="small" appearance="outline">
                          {routePreview.strategy}
                        </Badge>
                      )}
                      {routePreview.primaryModel && (
                        <Badge size="small" appearance="tint" color="brand">
                          {routePreview.primaryModel}
                        </Badge>
                      )}
                    </div>
                  </div>
                  <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)", marginTop: 4 }}>
                    {routePreview.reason}
                  </div>
                </div>
              ) : (
                <Spinner size="small" label="正在计算最优 Agent 与 SDLC 模板..." />
              )}

              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
                <div>
                  <div style={{ fontSize: 12.5, fontWeight: 600, marginBottom: 4 }}>SDLC 流程模板</div>
                  <Dropdown
                    value={templates.find((t) => t.id === chosenTemplate)?.name || chosenTemplate}
                    selectedOptions={[chosenTemplate]}
                    onOptionSelect={(_, d) => d.optionValue && setChosenTemplate(d.optionValue)}
                    style={{ width: "100%" }}
                  >
                    {templates.map((t) => (
                      <Option key={t.id} value={t.id}>
                        {t.name}
                      </Option>
                    ))}
                  </Dropdown>
                </div>

                <div>
                  <div style={{ fontSize: 12.5, fontWeight: 600, marginBottom: 4 }}>主执行 Agent</div>
                  <Dropdown
                    value={agents.find((a) => a.id === chosenAgent)?.name || chosenAgent}
                    selectedOptions={[chosenAgent]}
                    onOptionSelect={(_, d) => d.optionValue && setChosenAgent(d.optionValue)}
                    style={{ width: "100%" }}
                  >
                    {agents.map((a) => (
                      <Option key={a.id} value={a.id} text={`${a.name} ${a.detected ? "(本机可用)" : "(未检测到)"}`}>
                        {a.name} {a.detected ? "✓ 本机可用" : "(未检测到)"}
                      </Option>
                    ))}
                  </Dropdown>
                </div>
              </div>
            </DialogContent>
            <DialogActions>
              <Button appearance="secondary" onClick={() => setLaunchIssue(null)}>
                取消
              </Button>
              <Button
                appearance="primary"
                disabled={launching || !routePreview || launchMissingWorkspace}
                onClick={() => void confirmLaunch()}
              >
                {launching ? "正在入队..." : "确认启动流水线"}
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </div>
  );
}
