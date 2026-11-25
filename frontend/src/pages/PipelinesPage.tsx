import { useCallback, useEffect, useState } from "react";
import {
  Badge,
  Button,
  Dropdown,
  MessageBar,
  MessageBarBody,
  Option,
  Spinner,
  Tab,
  TabList,
  Textarea,
} from "@fluentui/react-components";
import {
  ArrowClockwise16Regular,
  Checkmark16Regular,
  Delete16Regular,
  Dismiss16Regular,
  FolderOpen16Regular,
  Open16Regular,
  Send16Regular,
} from "@fluentui/react-icons";
import { Browser } from "@wailsio/runtime";
import { PipelineService, SettingsService } from "../../bindings/dragon/internal/appservice/index.js";
import type { PipelineRun, RunDetail, RunLog } from "../types/dragon.js";
import { formatShortTime } from "../types/dragon.js";
import { StatusBadge } from "../components/StatusBadge.js";
import { StageStepper } from "../components/StageStepper.js";
import { LiveConsole } from "../components/LiveConsole.js";
import { DiffViewer } from "../components/DiffViewer.js";
import { MarkdownViewer } from "../components/MarkdownViewer.js";

interface PipelinesPageProps {
  selectedRepoId: string;
  selectedRunId: string;
  onSelectRun: (runId: string) => void;
  liveLog: RunLog | null;
  refreshSignal: number;
}

export function PipelinesPage({
  selectedRepoId,
  selectedRunId,
  onSelectRun,
  liveLog,
  refreshSignal,
}: PipelinesPageProps) {
  const [runs, setRuns] = useState<PipelineRun[]>([]);
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [detail, setDetail] = useState<RunDetail | null>(null);
  const [detailTab, setDetailTab] = useState<string>("console");
  const [diffText, setDiffText] = useState<string>("");
  const [selectedArtifact, setSelectedArtifact] = useState<string>("");
  const [artifactContent, setArtifactContent] = useState<string>("");
  const [feedback, setFeedback] = useState<string>("");
  const [actionBusy, setActionBusy] = useState<boolean>(false);
  const [errMsg, setErrMsg] = useState<string>("");

  const loadRuns = useCallback(async () => {
    try {
      const list = await PipelineService.ListRuns(selectedRepoId || "all", statusFilter);
      const items = list || [];
      setRuns(items);
      if (!selectedRunId && items.length > 0) {
        onSelectRun(items[0].id);
      }
    } catch (e) {
      setErrMsg(String(e));
    }
  }, [selectedRepoId, statusFilter, selectedRunId, onSelectRun]);

  const loadDetail = useCallback(async (runId: string) => {
    if (!runId) {
      setDetail(null);
      return;
    }
    try {
      const d = await PipelineService.GetRunDetail(runId);
      setDetail(d);
      if (d?.artifacts && d.artifacts.length > 0) {
        const defaultArt = d.artifacts[0].name;
        setSelectedArtifact((prev) =>
          d.artifacts?.some((a) => a.name === prev) ? prev : defaultArt
        );
      } else {
        setSelectedArtifact("");
        setArtifactContent("");
      }
    } catch (e) {
      setErrMsg(String(e));
    }
  }, []);

  useEffect(() => {
    void loadRuns();
  }, [loadRuns, refreshSignal]);

  useEffect(() => {
    void loadDetail(selectedRunId);
  }, [selectedRunId, loadDetail, refreshSignal]);

  // Append incoming streamed log chunks without full reload flicker.
  useEffect(() => {
    if (!liveLog || !selectedRunId || liveLog.runId !== selectedRunId) return;
    setDetail((prev) => {
      if (!prev || prev.run.id !== liveLog.runId) return prev;
      const existing = prev.logs || [];
      if (existing.some((l) => l.id === liveLog.id && liveLog.id > 0)) return prev;
      return { ...prev, logs: [...existing, liveLog] };
    });
  }, [liveLog, selectedRunId]);

  // Load artifact content when selectedArtifact changes.
  useEffect(() => {
    if (!selectedRunId || !selectedArtifact) return;
    void PipelineService.GetStageArtifact(selectedRunId, selectedArtifact)
      .then((txt) => setArtifactContent(txt || ""))
      .catch(() => setArtifactContent(""));
  }, [selectedRunId, selectedArtifact, refreshSignal]);

  // Load diff lazily when switching to Diff tab.
  useEffect(() => {
    if (detailTab !== "diff" || !selectedRunId) return;
    void PipelineService.GetRunDiff(selectedRunId)
      .then((txt) => setDiffText(txt || ""))
      .catch(() => setDiffText(""));
  }, [detailTab, selectedRunId, refreshSignal]);

  const handleApprove = async () => {
    if (!detail) return;
    setActionBusy(true);
    setErrMsg("");
    try {
      await PipelineService.ApproveRun(detail.run.id);
      await loadDetail(detail.run.id);
      await loadRuns();
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setActionBusy(false);
    }
  };

  const handleReject = async () => {
    if (!detail) return;
    setActionBusy(true);
    setErrMsg("");
    try {
      await PipelineService.RequestChanges(detail.run.id, feedback);
      setFeedback("");
      await loadDetail(detail.run.id);
      await loadRuns();
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setActionBusy(false);
    }
  };

  const handleAbort = async () => {
    if (!detail) return;
    setActionBusy(true);
    try {
      await PipelineService.AbortRun(detail.run.id);
      await loadDetail(detail.run.id);
      await loadRuns();
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setActionBusy(false);
    }
  };

  const handleRetry = async () => {
    if (!detail) return;
    setActionBusy(true);
    try {
      const next = await PipelineService.RetryRun(detail.run.id);
      await loadRuns();
      if (next) onSelectRun(next.id);
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setActionBusy(false);
    }
  };

  const handleOpenWorktree = async () => {
    if (!detail?.run?.worktreePath) return;
    setErrMsg("");
    try {
      await SettingsService.OpenDirectory(detail.run.worktreePath);
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  const handleCleanupWorktree = async () => {
    if (!detail) return;
    setActionBusy(true);
    setErrMsg("");
    try {
      await PipelineService.CleanupRunWorktree(detail.run.id);
      await loadDetail(detail.run.id);
      await loadRuns();
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setActionBusy(false);
    }
  };

  const curRun = detail?.run;
  const isWaiting = curRun?.status === "waiting_approval";
  const isActive = curRun?.status === "running" || curRun?.status === "queued" || isWaiting;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14, height: "100%" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 600 }}>SDLC 自动化流水线</h1>
          <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
            实时监控 7 阶段执行状态、Agent 流式日志、规范与审查产物、代码 Diff 与人工审批门禁
          </div>
        </div>
        <Dropdown
          size="small"
          value={statusFilter === "all" ? "全部任务状态" : statusFilter}
          selectedOptions={[statusFilter]}
          onOptionSelect={(_, d) => d.optionValue && setStatusFilter(d.optionValue)}
          style={{ minWidth: 150 }}
        >
          <Option value="all">全部任务状态</Option>
          <Option value="waiting_approval">等待审批</Option>
          <Option value="running">执行中</Option>
          <Option value="succeeded">已成功</Option>
          <Option value="failed">失败</Option>
          <Option value="cancelled">已中止</Option>
        </Dropdown>
      </div>

      {errMsg && (
        <MessageBar intent="error">
          <MessageBarBody>{errMsg}</MessageBarBody>
        </MessageBar>
      )}

      <div style={{ display: "grid", gridTemplateColumns: "300px 1fr", gap: 14, flex: 1, minHeight: 0 }}>
        {/* Left Master List */}
        <div className="dragon-card" style={{ padding: 10, overflowY: "auto", display: "flex", flexDirection: "column", gap: 6 }}>
          {runs.length === 0 ? (
            <div style={{ padding: 24, textAlign: "center", fontSize: 13, color: "var(--dragon-text-secondary)" }}>
              暂无流水线运行记录
            </div>
          ) : (
            runs.map((r) => {
              const selected = r.id === selectedRunId;
              return (
                <div
                  key={r.id}
                  className="dragon-subtle-box"
                  style={{
                    padding: "10px 10px",
                    cursor: "pointer",
                    borderColor: selected ? "var(--dragon-accent)" : undefined,
                    background: selected ? "var(--dragon-nav-active-bg)" : undefined,
                  }}
                  onClick={() => onSelectRun(r.id)}
                >
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 6 }}>
                    <span style={{ fontWeight: 700, fontSize: 12.5, color: "var(--dragon-accent)" }}>
                      #{r.issueNumber}
                    </span>
                    <StatusBadge status={r.status} />
                  </div>
                  <div
                    style={{
                      fontWeight: 600,
                      fontSize: 13,
                      marginTop: 4,
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {r.issueTitle}
                  </div>
                  <div style={{ fontSize: 11.5, color: "var(--dragon-text-secondary)", marginTop: 4 }}>
                    {r.routedAgentId} · {formatShortTime(r.startedAt)}
                  </div>
                </div>
              );
            })
          )}
        </div>

        {/* Right Inspector */}
        <div className="dragon-card" style={{ padding: 16, overflowY: "auto", display: "flex", flexDirection: "column", gap: 14 }}>
          {!curRun ? (
            <div style={{ margin: "auto", color: "var(--dragon-text-secondary)", fontSize: 13.5 }}>
              请在左侧选择一条流水线记录查看详情
            </div>
          ) : (
            <>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12 }}>
                <div>
                  <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                    <h2 style={{ margin: 0, fontSize: 17, fontWeight: 600 }}>
                      #{curRun.issueNumber} {curRun.issueTitle}
                    </h2>
                    <StatusBadge status={curRun.status} size="medium" />
                  </div>
                  <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)", marginTop: 4 }}>
                    路由 Agent: <b>{curRun.routedAgentId}</b>（{curRun.routingReason}）· 分支: <code>{curRun.branchName}</code>
                  </div>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 6,
                      fontSize: 11.5,
                      color: "var(--dragon-text-secondary)",
                      marginTop: 3,
                      flexWrap: "wrap",
                    }}
                  >
                    <span title={curRun.worktreePath || ""}>
                      执行工作区: <code>{curRun.worktreePath || "—"}</code>
                    </span>
                    {curRun.worktreePath && !detail?.worktreeExists && !isActive && (
                      <Badge size="small" appearance="tint" color="subtle">
                        已清理
                      </Badge>
                    )}
                  </div>
                </div>

                <div style={{ display: "flex", gap: 6, flexWrap: "wrap", justifyContent: "flex-end" }}>
                  {detail?.worktreeExists && curRun.worktreePath && (
                    <Button
                      size="small"
                      appearance="outline"
                      icon={<FolderOpen16Regular />}
                      onClick={() => void handleOpenWorktree()}
                    >
                      打开工作区
                    </Button>
                  )}
                  {detail?.worktreeExists && curRun.worktreePath && !isActive && (
                    <Button
                      size="small"
                      appearance="subtle"
                      icon={<Delete16Regular />}
                      disabled={actionBusy}
                      onClick={() => void handleCleanupWorktree()}
                    >
                      清理工作区
                    </Button>
                  )}
                  {curRun.prUrl && (
                    <Button
                      size="small"
                      appearance="outline"
                      icon={<Open16Regular />}
                      onClick={() => void Browser.OpenURL(curRun.prUrl)}
                    >
                      PR #{curRun.prNumber}
                    </Button>
                  )}
                  {isActive && (
                    <Button size="small" appearance="subtle" icon={<Dismiss16Regular />} disabled={actionBusy} onClick={() => void handleAbort()}>
                      中止
                    </Button>
                  )}
                  {(curRun.status === "failed" || curRun.status === "cancelled") && (
                    <Button size="small" appearance="primary" icon={<ArrowClockwise16Regular />} disabled={actionBusy} onClick={() => void handleRetry()}>
                      重新运行
                    </Button>
                  )}
                </div>
              </div>

              {curRun.errorMessage && (
                <MessageBar intent="error">
                  <MessageBarBody>{curRun.errorMessage}</MessageBarBody>
                </MessageBar>
              )}

              {/* Human-in-the-Loop Approval Banner */}
              {isWaiting && (
                <div
                  className="dragon-subtle-box"
                  style={{
                    padding: 14,
                    borderColor: "#D97706",
                    background: "rgba(217, 119, 6, 0.1)",
                    display: "flex",
                    flexDirection: "column",
                    gap: 10,
                  }}
                >
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <span style={{ fontWeight: 600, fontSize: 14 }}>
                      人工审批门禁：当前暂停在「{curRun.currentStage}」阶段前，请审阅产物或 Diff 后决定下一步
                    </span>
                    {actionBusy && <Spinner size="tiny" />}
                  </div>
                  <Textarea
                    placeholder="如需打回重做，请输入修改意见（批准继续时可留空）..."
                    value={feedback}
                    onChange={(_, d) => setFeedback(d.value)}
                    rows={2}
                  />
                  <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
                    <Button appearance="outline" icon={<Send16Regular />} disabled={actionBusy} onClick={() => void handleReject()}>
                      附带意见打回重做
                    </Button>
                    <Button appearance="primary" icon={<Checkmark16Regular />} disabled={actionBusy} onClick={() => void handleApprove()}>
                      批准并继续执行
                    </Button>
                  </div>
                </div>
              )}

              {/* 7-Stage Stepper */}
              <StageStepper stages={detail?.stages || []} />

              {/* Sub-tabs */}
              <TabList selectedValue={detailTab} onTabSelect={(_, d) => setDetailTab(String(d.value))}>
                <Tab value="console">实时终端日志 ({(detail?.logs || []).length})</Tab>
                <Tab value="artifacts">阶段产物 ({(detail?.artifacts || []).length})</Tab>
                <Tab value="diff">代码变更 Diff</Tab>
                <Tab value="pr">PR 与 CI 状态</Tab>
              </TabList>

              {detailTab === "console" && <LiveConsole logs={detail?.logs || []} />}

              {detailTab === "artifacts" && (
                <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
                  {(detail?.artifacts || []).length === 0 ? (
                    <div className="dragon-subtle-box" style={{ padding: 24, textAlign: "center", color: "var(--dragon-text-secondary)" }}>
                      暂无产出文档（将在需求分析、方案设计、AI 审查阶段自动生成 Markdown 报告）。
                    </div>
                  ) : (
                    <>
                      <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                        {(detail?.artifacts || []).map((a) => (
                          <Button
                            key={a.name}
                            size="small"
                            appearance={selectedArtifact === a.name ? "primary" : "outline"}
                            onClick={() => setSelectedArtifact(a.name)}
                          >
                            {a.name}
                          </Button>
                        ))}
                      </div>
                      <div className="dragon-subtle-box" style={{ padding: 14, maxHeight: 420, overflowY: "auto" }}>
                        <MarkdownViewer content={artifactContent} />
                      </div>
                    </>
                  )}
                </div>
              )}

              {detailTab === "diff" && <DiffViewer diffText={diffText} />}

              {detailTab === "pr" && (
                <div className="dragon-subtle-box" style={{ padding: 16, display: "flex", flexDirection: "column", gap: 10 }}>
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <span style={{ fontWeight: 600 }}>Pull Request & GitHub Checks</span>
                    <Badge appearance="tint" color={curRun.ciStatus === "success" ? "success" : curRun.ciStatus === "failure" ? "danger" : "informative"}>
                      CI: {curRun.ciStatus || "none"}
                    </Badge>
                  </div>
                  {curRun.prNumber > 0 ? (
                    <div style={{ fontSize: 13.5, display: "flex", flexDirection: "column", gap: 6 }}>
                      <div>
                        PR 编号: <b>#{curRun.prNumber}</b>
                      </div>
                      <div>
                        来源分支: <code>{curRun.branchName}</code>
                      </div>
                      <div>
                        链接:{" "}
                        <Button size="small" appearance="subtle" icon={<Open16Regular />} onClick={() => void Browser.OpenURL(curRun.prUrl)}>
                          {curRun.prUrl}
                        </Button>
                      </div>
                    </div>
                  ) : (
                    <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)" }}>
                      尚未提交 Pull Request，流水线推进至「提交 PR 与 CI 跟踪」阶段后将自动创建并追踪检查状态。
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}
