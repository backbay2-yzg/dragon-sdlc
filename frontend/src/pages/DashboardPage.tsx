import { Badge, Button, Spinner } from "@fluentui/react-components";
import {
  Add20Regular,
  ArrowRight16Regular,
  BranchFork20Regular,
  CheckmarkCircle20Regular,
  PauseCircle20Regular,
  Rocket20Regular,
} from "@fluentui/react-icons";
import { PipelineService } from "../../bindings/dragon/internal/appservice/index.js";
import type { DashboardOverview, NavTab } from "../types/dragon.js";
import { AUTONOMY_LABELS, formatShortTime } from "../types/dragon.js";
import { StatusBadge } from "../components/StatusBadge.js";

interface DashboardPageProps {
  overview: DashboardOverview | null;
  loading: boolean;
  onNavigate: (tab: NavTab, runId?: string) => void;
  onRefresh: () => void;
}

export function DashboardPage({ overview, loading, onNavigate, onRefresh }: DashboardPageProps) {
  if (loading && !overview) {
    return (
      <div style={{ display: "flex", alignItems: "center", justifyContent: "center", height: 260 }}>
        <Spinner label="正在加载 Dragon 工作台..." />
      </div>
    );
  }

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
  const repos = overview?.repos || [];
  const activeRuns = overview?.activeRuns || [];
  const pendingApprovals = overview?.pendingApprovals || [];
  const recentReleases = overview?.recentReleases || [];

  const handleQuickApprove = async (runId: string) => {
    await PipelineService.ApproveRun(runId);
    onRefresh();
  };

  const kpis = [
    {
      label: "运行中流水线",
      value: stats.activeRuns,
      sub: `共成功 ${stats.succeededRuns} 次 / 失败 ${stats.failedRuns} 次`,
      icon: <BranchFork20Regular style={{ color: "#0F6CBD" }} />,
      tab: "pipelines" as NavTab,
    },
    {
      label: "待人工审批门禁",
      value: stats.waitingApprovals,
      sub: "半自动护航暂停中",
      icon: <PauseCircle20Regular style={{ color: "#D97706" }} />,
      tab: "pipelines" as NavTab,
    },
    {
      label: "待办 Open Issues",
      value: stats.backlogIssues,
      sub: `已连接 ${stats.connectedRepos} 个仓库`,
      icon: <CheckmarkCircle20Regular style={{ color: "#10B981" }} />,
      tab: "issues" as NavTab,
    },
    {
      label: "待发版 PR 队列",
      value: stats.pendingReleaseItems,
      sub: `本机可用 Agent: ${stats.detectedAgents}`,
      icon: <Rocket20Regular style={{ color: "#6366F1" }} />,
      tab: "releases" as NavTab,
    },
  ];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 600 }}>工作台概览</h1>
          <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
            从开源 Issue 拉取、智能 Agent 路由、7 阶段 SDLC 到 PR / CI / SemVer 发版的一条龙自动化监控
          </div>
        </div>
        <div style={{ display: "flex", gap: 8 }}>
          <Button appearance="outline" onClick={() => onNavigate("issues")}>
            查看 Issue 队列
          </Button>
          <Button appearance="primary" icon={<Add20Regular />} onClick={() => onNavigate("repos")}>
            连接开源仓库
          </Button>
        </div>
      </div>

      {repos.length === 0 && (
        <div
          className="dragon-card"
          style={{
            padding: 18,
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            borderColor: "rgba(15, 108, 189, 0.35)",
          }}
        >
          <div>
            <div style={{ fontWeight: 600, fontSize: 15 }}>欢迎使用 Dragon！先绑定你的 GitHub 开源仓库</div>
            <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)", marginTop: 4 }}>
              连接仓库后，Dragon 将通过 ETag 低配额轮询自动同步最新 Issue，并按你的规则调度本机 Claude Code / Codex / Gemini 等 Agent 执行完整开发流程。
            </div>
          </div>
          <Button appearance="primary" onClick={() => onNavigate("repos")}>
            前往接入仓库
          </Button>
        </div>
      )}

      {/* KPI Material Cards */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(4, minmax(0, 1fr))", gap: 12 }}>
        {kpis.map((k) => (
          <div
            key={k.label}
            className="dragon-card dragon-card-interactive"
            style={{ padding: "14px 16px", cursor: "pointer" }}
            onClick={() => onNavigate(k.tab)}
          >
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <span style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)", fontWeight: 500 }}>
                {k.label}
              </span>
              {k.icon}
            </div>
            <div className="tabular-nums" style={{ fontSize: 28, fontWeight: 700, margin: "6px 0 4px" }}>
              {k.value}
            </div>
            <div style={{ fontSize: 11.5, color: "var(--dragon-text-secondary)" }}>{k.sub}</div>
          </div>
        ))}
      </div>

      {/* Main Split: Approval Queue + Active Runs */}
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 14 }}>
        <div className="dragon-card" style={{ padding: 16 }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
            <span style={{ fontWeight: 600, fontSize: 15 }}>待人工审批门禁</span>
            <Badge appearance="tint" color={pendingApprovals.length > 0 ? "warning" : "subtle"}>
              {pendingApprovals.length} 项待确认
            </Badge>
          </div>
          {pendingApprovals.length === 0 ? (
            <div className="dragon-subtle-box" style={{ padding: 20, textAlign: "center", color: "var(--dragon-text-secondary)", fontSize: 13 }}>
              当前没有卡在人工审批门禁的流水线任务。
            </div>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {pendingApprovals.map((run) => (
                <div
                  key={run.id}
                  className="dragon-subtle-box"
                  style={{ padding: "10px 12px", display: "flex", alignItems: "center", justifyContent: "space-between", gap: 10 }}
                >
                  <div style={{ minWidth: 0, flex: 1 }}>
                    <div style={{ fontWeight: 600, fontSize: 13.5, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      #{run.issueNumber} {run.issueTitle}
                    </div>
                    <div style={{ fontSize: 12, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
                      暂停阶段: {run.currentStage} · Agent: {run.routedAgentId}
                    </div>
                  </div>
                  <div style={{ display: "flex", gap: 6 }}>
                    <Button size="small" appearance="subtle" onClick={() => onNavigate("pipelines", run.id)}>
                      详情
                    </Button>
                    <Button size="small" appearance="primary" onClick={() => handleQuickApprove(run.id)}>
                      批准继续
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="dragon-card" style={{ padding: 16 }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
            <span style={{ fontWeight: 600, fontSize: 15 }}>正在运行的流水线</span>
            <Button size="small" appearance="subtle" icon={<ArrowRight16Regular />} iconPosition="after" onClick={() => onNavigate("pipelines")}>
              全部任务
            </Button>
          </div>
          {activeRuns.length === 0 ? (
            <div className="dragon-subtle-box" style={{ padding: 20, textAlign: "center", color: "var(--dragon-text-secondary)", fontSize: 13 }}>
              暂无正在执行的任务，可前往 Issue 队列启动自动开发。
            </div>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {activeRuns.map((run) => (
                <div
                  key={run.id}
                  className="dragon-subtle-box"
                  style={{ padding: "10px 12px", cursor: "pointer" }}
                  onClick={() => onNavigate("pipelines", run.id)}
                >
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8 }}>
                    <span style={{ fontWeight: 600, fontSize: 13.5 }}>
                      #{run.issueNumber} {run.issueTitle}
                    </span>
                    <StatusBadge status={run.status} />
                  </div>
                  <div style={{ fontSize: 12, color: "var(--dragon-text-secondary)", marginTop: 4 }}>
                    阶段: {run.currentStage || "初始化"} · Agent: {run.routedAgentId} · 分支: {run.branchName}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Bottom Split: Connected Repos + Recent Releases */}
      <div style={{ display: "grid", gridTemplateColumns: "1.2fr 1fr", gap: 14 }}>
        <div className="dragon-card" style={{ padding: 16 }}>
          <div style={{ fontWeight: 600, fontSize: 15, marginBottom: 10 }}>已接入开源项目</div>
          {repos.length === 0 ? (
            <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)" }}>尚未添加仓库。</div>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {repos.map((r) => (
                <div
                  key={r.id}
                  className="dragon-subtle-box"
                  style={{ padding: "10px 12px", display: "flex", justifyContent: "space-between", alignItems: "center" }}
                >
                  <div>
                    <div style={{ fontWeight: 600, fontSize: 13.5 }}>{r.fullName}</div>
                    <div style={{ fontSize: 12, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
                      触发标签: <code>{r.triggerLabel}</code> · {AUTONOMY_LABELS[r.autonomyMode] || r.autonomyMode} · 发版: {r.releaseMode === "immediate" ? "单PR即时" : "批量列车"}
                    </div>
                  </div>
                  <div style={{ fontSize: 11.5, color: "var(--dragon-text-secondary)", textAlign: "right" }}>
                    <div>分支: {r.defaultBranch}</div>
                    <div>上次同步: {formatShortTime(r.lastSyncedAt)}</div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="dragon-card" style={{ padding: 16 }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
            <span style={{ fontWeight: 600, fontSize: 15 }}>近期发布记录</span>
            <Button size="small" appearance="subtle" onClick={() => onNavigate("releases")}>
              发版中心
            </Button>
          </div>
          {recentReleases.length === 0 ? (
            <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)" }}>暂无发布记录。</div>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {recentReleases.slice(0, 5).map((rel) => (
                <div
                  key={rel.id}
                  className="dragon-subtle-box"
                  style={{ padding: "8px 12px", display: "flex", justifyContent: "space-between", alignItems: "center" }}
                >
                  <div>
                    <div style={{ fontWeight: 600, fontSize: 13.5 }}>{rel.tagName}</div>
                    <div style={{ fontSize: 11.5, color: "var(--dragon-text-secondary)" }}>
                      {rel.mode === "immediate" ? "单 PR 即时发布" : "批量列车发布"} · {formatShortTime(rel.publishedAt || rel.createdAt)}
                    </div>
                  </div>
                  <StatusBadge status={rel.status} />
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
