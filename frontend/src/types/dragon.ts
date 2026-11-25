export type {
  AddRepoRequest,
  AgentWorkspaceSnapshot,
  ApplyProfileResult,
  ArtifactEntry,
  AuthStatusDetail,
  BatchAddReposRequest,
  BatchAddReposResult,
  DashboardOverview,
  ModelCatalogOption,
  NavigateEvent,
  ReleaseOverview,
  RemoteRepoOption,
  RepoHealthResult,
  RunDetail,
  SimulateRouteRequest,
  TestAgentResult,
} from "../../bindings/dragon/internal/appservice/models.js";

export type { AppSettings, Paths } from "../../bindings/dragon/internal/config/models.js";
export type { WorkspaceCheckResult } from "../../bindings/dragon/internal/gitops/models.js";
export type { AuthIdentity, RemoteRepo } from "../../bindings/dragon/internal/hosting/models.js";
export type { DeviceFlowPollResult, DeviceFlowStart } from "../../bindings/dragon/internal/hosting/github/models.js";
export type { StartRunRequest } from "../../bindings/dragon/internal/pipeline/models.js";
export type { SyncSummary } from "../../bindings/dragon/internal/poller/models.js";
export type { PublishRequest, ReleasePreview } from "../../bindings/dragon/internal/release/models.js";
export type { RoutingDecision } from "../../bindings/dragon/internal/router/models.js";
export type {
  AgentProfile,
  DashboardStats,
  Issue,
  ModelProvider,
  PipelineRun,
  PipelineTemplate,
  ReleaseItem,
  ReleaseRecord,
  Repository,
  RoutingConfig,
  RoutingRule,
  RunLog,
  StageDescriptor,
  StageExecution,
  StageRouteBinding,
  WorkspaceProfile,
} from "../../bindings/dragon/internal/store/models.js";

export interface ModelCatalogEntry {
  id: string;
  name: string;
  providerId: string;
  costTier: string;
  contextK: number;
  tags: string[];
}

export type NavTab = "dashboard" | "issues" | "pipelines" | "releases" | "agents" | "repos";

export const STATUS_LABELS: Record<string, string> = {
  backlog: "待办 Backlog",
  queued: "排队中",
  running: "执行中",
  waiting_approval: "等待审批",
  approved: "已批准",
  pr_open: "PR 已提交",
  completed: "已完成",
  succeeded: "已成功",
  failed: "失败阻塞",
  cancelled: "已中止",
  ignored: "已忽略",
  pending: "待执行",
  skipped: "已跳过",
  published: "已发布",
  draft: "草稿",
};

export const AUTONOMY_LABELS: Record<string, string> = {
  template: "跟随模板门禁（推荐）",
  semi_auto: "半自动护航",
  full_auto: "全自动一条龙",
  step_by_step: "多节点强审批",
};

export const TEMPLATE_MODE_LABELS: Record<string, string> = {
  auto: "智能自动分流",
};

export function parseLabels(labelsJson: string): string[] {
  try {
    const parsed = JSON.parse(labelsJson || "[]");
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

export function formatShortTime(rfc3339: string): string {
  if (!rfc3339) return "—";
  const d = new Date(rfc3339);
  if (Number.isNaN(d.getTime())) return rfc3339;
  return d.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function formatDurationMs(ms: number): string {
  if (!ms || ms <= 0) return "";
  if (ms < 1000) return `${ms}ms`;
  const sec = Math.round(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  return `${min}m ${sec % 60}s`;
}
