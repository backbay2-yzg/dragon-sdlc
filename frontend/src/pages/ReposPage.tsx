import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Badge,
  Button,
  Checkbox,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Input,
  MessageBar,
  MessageBarBody,
  Spinner,
  Tooltip,
} from "@fluentui/react-components";
import {
  Add20Regular,
  Delete16Regular,
  FolderOpen16Regular,
  Open16Regular,
  Pause16Regular,
  Play16Regular,
  PlugConnected16Regular,
  Search16Regular,
  Settings16Regular,
} from "@fluentui/react-icons";
import { Browser } from "@wailsio/runtime";
import {
  AgentService,
  RepoService,
  SettingsService,
} from "../../bindings/dragon/internal/appservice/index.js";
import { RepoConfigDialog } from "../components/RepoConfigDialog.js";
import { RepoPickerDialog } from "../components/RepoPickerDialog.js";
import type {
  AuthStatusDetail,
  PipelineTemplate,
  RepoHealthResult,
  Repository,
} from "../types/dragon.js";
import { AUTONOMY_LABELS, formatShortTime } from "../types/dragon.js";

interface ReposPageProps {
  repos: Repository[];
  auth: AuthStatusDetail | null;
  defaultAutonomyMode: string;
  defaultWorkspaceRoot: string;
  pendingConfigRepoId?: string | null;
  onClearPendingConfigRepo?: () => void;
  onOpenAuth: () => void;
  onJumpToTemplates: () => void;
  onRefreshAll: () => void;
}

type StatusFilter = "all" | "enabled" | "paused";

export function ReposPage({
  repos,
  auth,
  defaultAutonomyMode,
  defaultWorkspaceRoot,
  pendingConfigRepoId,
  onClearPendingConfigRepo,
  onOpenAuth,
  onJumpToTemplates,
  onRefreshAll,
}: ReposPageProps) {
  const [search, setSearch] = useState<string>("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [pickerOpen, setPickerOpen] = useState<boolean>(false);

  const [editingRepo, setEditingRepo] = useState<Repository | null>(null);
  const [deletingRepo, setDeletingRepo] = useState<Repository | null>(null);
  const [cleanLocalCache, setCleanLocalCache] = useState<boolean>(false);
  const [deletingBusy, setDeletingBusy] = useState<boolean>(false);

  const [togglingIds, setTogglingIds] = useState<Record<string, boolean>>({});
  const [testingIds, setTestingIds] = useState<Record<string, boolean>>({});
  const [testingAll, setTestingAll] = useState<boolean>(false);
  const [healthMap, setHealthMap] = useState<Record<string, RepoHealthResult>>({});

  const [templates, setTemplates] = useState<PipelineTemplate[]>([]);
  const [msg, setMsg] = useState<string>("");
  const [errMsg, setErrMsg] = useState<string>("");

  const loadMeta = useCallback(async () => {
    try {
      const tpls = await AgentService.ListTemplates();
      setTemplates(tpls || []);
    } catch (e) {
      setErrMsg(String(e));
    }
  }, []);

  useEffect(() => {
    void loadMeta();
  }, [loadMeta]);

  useEffect(() => {
    if (!pendingConfigRepoId) return;
    const target = repos.find((r) => r.id === pendingConfigRepoId);
    if (target) {
      setEditingRepo({ ...target });
      onClearPendingConfigRepo?.();
    }
  }, [pendingConfigRepoId, repos, onClearPendingConfigRepo]);

  const enabledCount = useMemo(() => repos.filter((r) => r.enabled).length, [repos]);
  const pausedCount = repos.length - enabledCount;

  const filteredRepos = useMemo(() => {
    const q = search.trim().toLowerCase();
    return repos.filter((r) => {
      if (statusFilter === "enabled" && !r.enabled) return false;
      if (statusFilter === "paused" && r.enabled) return false;
      if (q) {
        const hay = `${r.fullName} ${r.triggerLabel} ${r.description} ${r.defaultBranch}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
  }, [repos, search, statusFilter]);

  const handleToggleRepo = async (repo: Repository) => {
    setErrMsg("");
    setMsg("");
    setTogglingIds((prev) => ({ ...prev, [repo.id]: true }));
    try {
      const nextEnabled = !repo.enabled;
      await RepoService.SetRepoEnabled(repo.id, nextEnabled);
      setMsg(nextEnabled ? `已启动 ${repo.fullName} 轮询并触发后台同步。` : `已暂停 ${repo.fullName} 自动轮询。`);
      onRefreshAll();
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setTogglingIds((prev) => {
        const next = { ...prev };
        delete next[repo.id];
        return next;
      });
    }
  };

  const runHealthCheck = async (repo: Repository): Promise<RepoHealthResult | null> => {
    setTestingIds((prev) => ({ ...prev, [repo.id]: true }));
    try {
      const res = await RepoService.CheckRepoHealth(repo.id);
      if (res) {
        setHealthMap((prev) => ({ ...prev, [repo.id]: res }));
        if (res.branchUpdated) {
          onRefreshAll();
        }
      }
      return res;
    } catch (e) {
      setErrMsg(String(e));
      return null;
    } finally {
      setTestingIds((prev) => {
        const next = { ...prev };
        delete next[repo.id];
        return next;
      });
    }
  };

  const handleTestAll = async () => {
    if (repos.length === 0) return;
    setTestingAll(true);
    setErrMsg("");
    setMsg("");
    try {
      const results = await Promise.all(repos.map((r) => runHealthCheck(r)));
      const valid = results.filter((r): r is RepoHealthResult => r !== null);
      const okCount = valid.filter((r) => r.ok && !r.permissionWarning).length;
      const warnCount = valid.filter((r) => r.ok && Boolean(r.permissionWarning)).length;
      const failCount = valid.filter((r) => !r.ok).length;
      setMsg(`全部体检完成：${okCount} 个可写正常，${warnCount} 个权限受限/只读，${failCount} 个连接异常。`);
    } finally {
      setTestingAll(false);
    }
  };

  const handleSaveRepo = async (updated: Repository) => {
    setErrMsg("");
    setMsg("");
    await RepoService.UpdateRepo(updated);
    setEditingRepo(null);
    setMsg(`仓库 ${updated.fullName} 的流程策略与工作区配置已保存。`);
    onRefreshAll();
  };

  const handleOpenWorkspaceDir = async (localPath: string) => {
    setErrMsg("");
    try {
      await SettingsService.OpenDirectory(localPath);
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  const handleConfirmDelete = async () => {
    if (!deletingRepo) return;
    setDeletingBusy(true);
    setErrMsg("");
    setMsg("");
    try {
      const name = deletingRepo.fullName;
      const id = deletingRepo.id;
      await RepoService.DeleteRepoWithOptions(id, cleanLocalCache);
      setDeletingRepo(null);
      setCleanLocalCache(false);
      setHealthMap((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
      setMsg(`已移除仓库 ${name}${cleanLocalCache ? " 并清理本地 Git 镜像缓存" : ""}。`);
      onRefreshAll();
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setDeletingBusy(false);
    }
  };

  const getFlowLabel = (tplId: string) => {
    if (!tplId || tplId === "auto") return "智能分流";
    const found = templates.find((t) => t.id === tplId);
    return found ? found.name.replace(/\s*\([^)]*\)\s*$/, "") : tplId;
  };

  const isAuth = Boolean(auth?.identity?.authenticated);

  const renderHealthBadge = (repo: Repository) => {
    const h = healthMap[repo.id];
    if (!h) return null;
    const wsSuffix = h.workspaceMessage ? ` · 工作区: ${h.workspaceMessage}` : "";
    const wsBadge =
      h.workspaceStatus === "unconfigured" ? (
        <Tooltip content={h.workspaceMessage || "尚未配置本地工作区目录"} relationship="label">
          <Badge
            size="small"
            appearance="tint"
            color="warning"
            style={{ cursor: "pointer" }}
            onClick={() => setEditingRepo({ ...repo })}
          >
            工作区未配置
          </Badge>
        </Tooltip>
      ) : h.workspaceStatus === "invalid" ? (
        <Tooltip content={h.workspaceMessage || "本地工作区路径无效"} relationship="label">
          <Badge
            size="small"
            appearance="tint"
            color="danger"
            style={{ cursor: "pointer" }}
            onClick={() => setEditingRepo({ ...repo })}
          >
            工作区异常
          </Badge>
        </Tooltip>
      ) : null;

    if (!h.ok) {
      return (
        <>
          <Tooltip content={(h.error || "连接校验失败") + wsSuffix} relationship="label">
            <Badge size="small" appearance="tint" color="danger">
              连接异常
            </Badge>
          </Tooltip>
          {wsBadge}
        </>
      );
    }
    if (h.permissionWarning) {
      return (
        <>
          <Tooltip
            content={`${h.permissionWarning}（检查时间 ${formatShortTime(h.checkedAt)}）${wsSuffix}`}
            relationship="label"
          >
            <Badge size="small" appearance="tint" color="warning">
              {h.archived ? "已归档" : "只读"} · {h.latencyMs}ms
            </Badge>
          </Tooltip>
          {wsBadge}
        </>
      );
    }
    const permDesc = h.canAdmin ? "管理员/可写" : "可写/PR";
    const branchNote = h.branchUpdated ? ` · 默认分支已更新为 ${h.defaultBranch}` : "";
    return (
      <>
        <Tooltip
          content={`GitHub 连通正常（${permDesc} · 延迟 ${h.latencyMs}ms · ${formatShortTime(h.checkedAt)}${branchNote}${wsSuffix}）`}
          relationship="label"
        >
          <Badge size="small" appearance="tint" color="success">
            可写 · {h.latencyMs}ms
          </Badge>
        </Tooltip>
        {wsBadge}
      </>
    );
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14, maxWidth: 1080, margin: "0 auto" }}>
      {/* Page Header */}
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 600 }}>仓库管理</h1>
          <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
            自动拉取授权 GitHub 项目接入工作台，支持单行启停轮询、连通性与权限体检及阶段门禁配置
          </div>
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          {repos.length > 0 && (
            <Button
              appearance="outline"
              disabled={testingAll}
              icon={testingAll ? <Spinner size="extra-tiny" /> : <PlugConnected16Regular />}
              onClick={() => void handleTestAll()}
            >
              {testingAll ? "体检中..." : "全部测试"}
            </Button>
          )}
          <Button appearance="primary" icon={<Add20Regular />} onClick={() => setPickerOpen(true)}>
            接入仓库
          </Button>
        </div>
      </div>

      {/* Unauthenticated Callout */}
      {!isAuth && (
        <div
          className="dragon-card"
          style={{
            padding: "12px 16px",
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 12,
            borderColor: "rgba(245, 158, 11, 0.38)",
          }}
        >
          <div style={{ minWidth: 0 }}>
            <div style={{ fontWeight: 600, fontSize: 13.5 }}>尚未连接 GitHub 账号</div>
            <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
              点击左下角头像或右侧按钮完成授权后，即可自动获取你的 GitHub 项目列表并进行连接体检与流水线推 PR。
            </div>
          </div>
          <Button appearance="primary" size="small" onClick={onOpenAuth}>
            前往授权
          </Button>
        </div>
      )}

      {msg && (
        <MessageBar intent="success">
          <MessageBarBody>{msg}</MessageBarBody>
        </MessageBar>
      )}
      {errMsg && (
        <MessageBar intent="error">
          <MessageBarBody>{errMsg}</MessageBarBody>
        </MessageBar>
      )}

      {/* Search & Status Filter Bar */}
      {repos.length > 0 && (
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
          <Input
            contentBefore={<Search16Regular />}
            placeholder="搜索已接入仓库、分支或触发标签..."
            value={search}
            onChange={(_, d) => setSearch(d.value)}
            style={{ width: 320, maxWidth: "100%" }}
          />
          <div className="dragon-segmented-tabs" role="tablist">
            {(
              [
                { id: "all", label: `全部 (${repos.length})` },
                { id: "enabled", label: `运行中 (${enabledCount})` },
                { id: "paused", label: `已暂停 (${pausedCount})` },
              ] as const
            ).map((f) => (
              <button
                key={f.id}
                type="button"
                role="tab"
                aria-selected={statusFilter === f.id}
                className={`dragon-seg-tab ${statusFilter === f.id ? "active" : ""}`}
                onClick={() => setStatusFilter(f.id)}
              >
                {f.label}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Single-Row Unified Repository List */}
      {repos.length === 0 ? (
        <div
          className="dragon-card"
          style={{
            padding: "32px 24px",
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            textAlign: "center",
            gap: 10,
          }}
        >
          <div style={{ fontWeight: 600, fontSize: 15 }}>还没有接入任何 GitHub 仓库</div>
          <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)", maxWidth: 480, lineHeight: 1.55 }}>
            点击「接入仓库」即可从已授权的 GitHub 账号中直接勾选个人或组织项目批量接入，也可以手动输入外部开源仓库。
          </div>
          <Button
            appearance="primary"
            icon={<Add20Regular />}
            style={{ marginTop: 4 }}
            onClick={() => setPickerOpen(true)}
          >
            从 GitHub 选择仓库
          </Button>
        </div>
      ) : (
        <div className="dragon-list-card">
          {filteredRepos.length === 0 ? (
            <div style={{ padding: 24, textAlign: "center", fontSize: 13, color: "var(--dragon-text-secondary)" }}>
              没有符合当前筛选条件的仓库。
            </div>
          ) : (
            filteredRepos.map((r) => {
              const isToggling = Boolean(togglingIds[r.id]);
              const isTesting = Boolean(testingIds[r.id]);
              return (
                <div key={r.id} className="dragon-list-row">
                  <div style={{ minWidth: 0, flex: 1 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                      <span className={`dragon-status-dot ${r.enabled ? "online" : "offline"}`} />
                      <span
                        style={{
                          fontWeight: 600,
                          fontSize: 14,
                          overflow: "hidden",
                          textOverflow: "ellipsis",
                          whiteSpace: "nowrap",
                        }}
                      >
                        {r.fullName}
                      </span>
                      <Tooltip content="在浏览器中打开 GitHub 仓库" relationship="label">
                        <Button
                          size="small"
                          appearance="subtle"
                          icon={<Open16Regular />}
                          aria-label={`打开 ${r.fullName}`}
                          onClick={() => void Browser.OpenURL(`https://github.com/${r.fullName}`)}
                        />
                      </Tooltip>
                      {r.private && (
                        <Badge size="small" appearance="tint" color="informative">
                          私有
                        </Badge>
                      )}
                      <Badge size="small" appearance="outline">
                        {r.defaultBranch || "main"}
                      </Badge>
                      {!r.localPath && (
                        <Tooltip content="未配置本地工作区目录，点击前往配置" relationship="label">
                          <Badge
                            size="small"
                            appearance="tint"
                            color="warning"
                            style={{ cursor: "pointer" }}
                            onClick={() => setEditingRepo({ ...r })}
                          >
                            未配置工作区 · 点击设置
                          </Badge>
                        </Tooltip>
                      )}
                      {renderHealthBadge(r)}
                    </div>

                    <div
                      style={{
                        fontSize: 12,
                        color: "var(--dragon-text-secondary)",
                        marginTop: 3,
                        whiteSpace: "nowrap",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                      }}
                    >
                      {r.enabled ? "轮询运行中" : "已暂停"} · {getFlowLabel(r.defaultTemplateId)} ·{" "}
                      {AUTONOMY_LABELS[r.autonomyMode] || r.autonomyMode} · 触发: <code>{r.triggerLabel}</code> ·{" "}
                      上次同步: {formatShortTime(r.lastSyncedAt)}
                    </div>
                    {r.localPath && (
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 4,
                          fontSize: 11.5,
                          color: "var(--dragon-text-secondary)",
                          marginTop: 2,
                        }}
                      >
                        <span
                          title={r.localPath}
                          style={{
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            whiteSpace: "nowrap",
                          }}
                        >
                          工作区: <code>{r.localPath}</code>
                        </span>
                        <Tooltip content="在资源管理器中打开本地工作区目录" relationship="label">
                          <Button
                            size="small"
                            appearance="subtle"
                            icon={<FolderOpen16Regular />}
                            aria-label={`打开 ${r.fullName} 工作区目录`}
                            onClick={() => void handleOpenWorkspaceDir(r.localPath)}
                          />
                        </Tooltip>
                      </div>
                    )}
                  </div>

                  <div style={{ display: "flex", alignItems: "center", gap: 6, flexShrink: 0 }}>
                    <Button
                      size="small"
                      appearance="subtle"
                      disabled={isToggling}
                      icon={r.enabled ? <Pause16Regular /> : <Play16Regular />}
                      onClick={() => void handleToggleRepo(r)}
                    >
                      {r.enabled ? "暂停" : "开始"}
                    </Button>
                    <Button
                      size="small"
                      appearance="subtle"
                      disabled={isTesting}
                      icon={isTesting ? <Spinner size="extra-tiny" /> : <PlugConnected16Regular />}
                      onClick={() => void runHealthCheck(r)}
                    >
                      测试
                    </Button>
                    <Button
                      size="small"
                      appearance="outline"
                      icon={<Settings16Regular />}
                      onClick={() => setEditingRepo({ ...r })}
                    >
                      配置
                    </Button>
                    <Tooltip content="移除仓库" relationship="label">
                      <Button
                        size="small"
                        appearance="subtle"
                        className="dragon-danger-btn"
                        icon={<Delete16Regular />}
                        aria-label={`删除 ${r.fullName}`}
                        onClick={() => {
                          setCleanLocalCache(false);
                          setDeletingRepo(r);
                        }}
                      />
                    </Tooltip>
                  </div>
                </div>
              );
            })
          )}
        </div>
      )}

      {/* Repo Picker Dialog */}
      <RepoPickerDialog
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        auth={auth}
        defaultAutonomyMode={defaultAutonomyMode}
        defaultWorkspaceRoot={defaultWorkspaceRoot}
        onOpenAuth={onOpenAuth}
        onReposAdded={onRefreshAll}
        onNotifySuccess={(m) => {
          setErrMsg("");
          setMsg(m);
        }}
      />

      {/* Delete Confirmation Dialog */}
      <Dialog open={deletingRepo !== null} onOpenChange={(_, d) => !d.open && setDeletingRepo(null)}>
        <DialogSurface style={{ maxWidth: 460 }}>
          <DialogBody>
            <DialogTitle>确认移除仓库：{deletingRepo?.fullName}</DialogTitle>
            <DialogContent style={{ display: "flex", flexDirection: "column", gap: 12, marginTop: 8 }}>
              <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)", lineHeight: 1.55 }}>
                移除仓库后，Dragon 将级联删除该仓库下所有已同步的 Issue、流水线运行记录与发版记录（不会影响 GitHub 远端仓库）。
              </div>
              <Checkbox
                label="同时清理该仓库关联的临时 Worktree 与旧版缓存（不会删除你的本地代码目录）"
                checked={cleanLocalCache}
                onChange={(_, d) => setCleanLocalCache(Boolean(d.checked))}
              />
            </DialogContent>
            <DialogActions>
              <Button appearance="secondary" onClick={() => setDeletingRepo(null)}>
                取消
              </Button>
              <Button
                appearance="primary"
                disabled={deletingBusy}
                onClick={() => void handleConfirmDelete()}
                style={{ background: "#c42b1c", borderColor: "#c42b1c" }}
              >
                {deletingBusy ? "移除中..." : "确认移除"}
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>

      {/* Simplified Repo Strategy & Build Dialog */}
      <RepoConfigDialog
        repo={editingRepo}
        templates={templates}
        defaultWorkspaceRoot={defaultWorkspaceRoot}
        onClose={() => setEditingRepo(null)}
        onSave={handleSaveRepo}
        onRepoUpdated={onRefreshAll}
        onJumpToCustomize={() => {
          setEditingRepo(null);
          onJumpToTemplates();
        }}
      />
    </div>
  );
}
