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
  Dropdown,
  Input,
  MessageBar,
  MessageBarBody,
  Option,
  Spinner,
} from "@fluentui/react-components";
import {
  Add20Regular,
  ArrowSync16Regular,
  ChevronDown16Regular,
  ChevronUp16Regular,
  Folder16Regular,
  Search16Regular,
} from "@fluentui/react-icons";
import { RepoService, SettingsService } from "../../bindings/dragon/internal/appservice/index.js";
import type {
  AuthStatusDetail,
  RemoteRepoOption,
} from "../types/dragon.js";
import { AUTONOMY_LABELS, formatShortTime } from "../types/dragon.js";

interface RepoPickerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  auth: AuthStatusDetail | null;
  defaultAutonomyMode: string;
  defaultWorkspaceRoot: string;
  onOpenAuth: () => void;
  onReposAdded: () => void;
  onNotifySuccess: (msg: string) => void;
}

type PickerMode = "github" | "manual";
type RepoFilter = "all" | "owner" | "org" | "private";

function joinWorkspacePath(parentDir: string, name: string): string {
  const cleanParent = parentDir.trim().replace(/[\\/]+$/, "");
  const cleanName = name.trim().replace(/^[\\/]+/, "");
  if (!cleanParent || !cleanName) return "";
  const sep = cleanParent.includes("/") && !cleanParent.includes("\\") ? "/" : "\\";
  return `${cleanParent}${sep}${cleanName}`;
}

function extractManualRepoName(input: string): string {
  let clean = input.trim();
  clean = clean.replace(/^https?:\/\/github\.com\//i, "");
  clean = clean.replace(/\.git$/i, "");
  clean = clean.replace(/^\/+|\/+$/g, "");
  const parts = clean.split("/").filter(Boolean);
  return parts.length >= 2 ? parts[1] : "";
}

export function RepoPickerDialog({
  open,
  onOpenChange,
  auth,
  defaultAutonomyMode,
  defaultWorkspaceRoot,
  onOpenAuth,
  onReposAdded,
  onNotifySuccess,
}: RepoPickerDialogProps) {
  const [mode, setMode] = useState<PickerMode>("github");
  const [remoteRepos, setRemoteRepos] = useState<RemoteRepoOption[]>([]);
  const [loadingList, setLoadingList] = useState<boolean>(false);
  const [search, setSearch] = useState<string>("");
  const [filter, setFilter] = useState<RepoFilter>("all");
  const [showArchived, setShowArchived] = useState<boolean>(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const [showAdvanced, setShowAdvanced] = useState<boolean>(false);
  const [triggerLabel, setTriggerLabel] = useState<string>("dragon-auto");
  const [autonomyMode, setAutonomyMode] = useState<string>(defaultAutonomyMode || "template");
  const [workspaceParentDir, setWorkspaceParentDir] = useState<string>(defaultWorkspaceRoot || "");

  const [manualInput, setManualInput] = useState<string>("");
  const [manualLocalPath, setManualLocalPath] = useState<string>("");
  const [manualPathEdited, setManualPathEdited] = useState<boolean>(false);
  const [submitting, setSubmitting] = useState<boolean>(false);
  const [msg, setMsg] = useState<string>("");
  const [errMsg, setErrMsg] = useState<string>("");

  const isAuth = Boolean(auth?.identity?.authenticated);
  const usernameLower = (auth?.identity?.username || "").toLowerCase();

  const loadRemoteRepos = useCallback(async () => {
    setLoadingList(true);
    setErrMsg("");
    try {
      const list = await RepoService.ListRemoteRepos();
      setRemoteRepos(list || []);
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setLoadingList(false);
    }
  }, []);

  useEffect(() => {
    if (!open) {
      setSelected(new Set());
      setMsg("");
      setErrMsg("");
      return;
    }
    setAutonomyMode(defaultAutonomyMode || "template");
    setWorkspaceParentDir(defaultWorkspaceRoot || "");
    setManualLocalPath("");
    setManualPathEdited(false);
    if (isAuth && mode === "github") {
      void loadRemoteRepos();
    }
  }, [open, isAuth, mode, defaultAutonomyMode, defaultWorkspaceRoot, loadRemoteRepos]);

  const filteredRepos = useMemo(() => {
    const q = search.trim().toLowerCase();
    return remoteRepos.filter((r) => {
      if (!showArchived && r.archived) return false;
      if (filter === "private" && !r.private) return false;
      if (filter === "owner") {
        const isPersonal = r.ownerType === "User" && (!usernameLower || r.owner.toLowerCase() === usernameLower);
        if (!isPersonal) return false;
      }
      if (filter === "org") {
        const isOrgOrCollab =
          r.ownerType === "Organization" || (Boolean(usernameLower) && r.owner.toLowerCase() !== usernameLower);
        if (!isOrgOrCollab) return false;
      }
      if (q) {
        const hay = `${r.fullName} ${r.description} ${r.language}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
  }, [remoteRepos, search, filter, showArchived, usernameLower]);

  const selectableVisible = useMemo(
    () => filteredRepos.filter((r) => !r.alreadyConnected),
    [filteredRepos]
  );

  const toggleSelect = (fullName: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(fullName)) {
        next.delete(fullName);
      } else {
        next.add(fullName);
      }
      return next;
    });
  };

  const handleBatchAdd = async () => {
    if (selected.size === 0) return;
    setSubmitting(true);
    setErrMsg("");
    setMsg("");
    try {
      const res = await RepoService.BatchAddRepos({
        fullNames: Array.from(selected),
        workspaceParentDir: workspaceParentDir.trim(),
        triggerLabel: triggerLabel.trim() || "dragon-auto",
        autonomyMode,
        defaultTemplateId: "auto",
        releaseMode: "batch",
      });
      const addedCount = res?.added?.length || 0;
      const skippedCount = res?.skipped?.length || 0;
      const errs = res?.errors || [];
      if (errs.length > 0 && addedCount === 0) {
        setErrMsg(errs.join("；"));
        return;
      }
      onReposAdded();
      const summaryParts = [`已接入 ${addedCount} 个仓库并开始同步 Issue`];
      if (skippedCount > 0) summaryParts.push(`跳过 ${skippedCount} 个已接入仓库`);
      if (errs.length > 0) summaryParts.push(`${errs.length} 个失败: ${errs.join("；")}`);
      onNotifySuccess(summaryParts.join("，") + "。");
      onOpenChange(false);
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setSubmitting(false);
    }
  };

  const handleTestManual = async () => {
    if (!manualInput.trim()) return;
    setSubmitting(true);
    setErrMsg("");
    setMsg("");
    try {
      const r = await RepoService.TestRepoConnection(manualInput.trim());
      if (r) {
        const permText = r.archived ? "已归档（只读）" : r.canPush ? "具备可写/PR权限" : "仅只读权限";
        setMsg(`连接成功：${r.fullName}（分支: ${r.defaultBranch} · ${r.private ? "私有" : "公开"} · ${permText}）`);
      }
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setSubmitting(false);
    }
  };

  const handleAddManual = async () => {
    if (!manualInput.trim()) return;
    setSubmitting(true);
    setErrMsg("");
    setMsg("");
    try {
      const added = await RepoService.AddRepo({
        fullName: manualInput.trim(),
        localPath: manualLocalPath.trim(),
        triggerLabel: triggerLabel.trim() || "dragon-auto",
        autonomyMode,
        defaultTemplateId: "auto",
        releaseMode: "batch",
        buildCmd: "",
        testCmd: "",
        lintCmd: "",
      });
      setManualInput("");
      setManualLocalPath("");
      setManualPathEdited(false);
      onReposAdded();
      if (added) {
        onNotifySuccess(`已接入仓库 ${added.fullName} 并开始同步 Issue。`);
      }
      onOpenChange(false);
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setSubmitting(false);
    }
  };

  const handlePickBatchParent = async () => {
    try {
      const picked = await SettingsService.PickDirectory(
        "选择批量接入仓库的工作区父目录",
        workspaceParentDir || defaultWorkspaceRoot || ""
      );
      if (picked) {
        setWorkspaceParentDir(picked);
      }
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  const handlePickManualLocalPath = async () => {
    try {
      const picked = await SettingsService.PickDirectory(
        "选择仓库本地工作区目录",
        manualLocalPath || defaultWorkspaceRoot || ""
      );
      if (picked) {
        setManualLocalPath(picked);
        setManualPathEdited(true);
      }
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(_, d) => onOpenChange(d.open)}>
      <DialogSurface style={{ maxWidth: 680 }}>
        <DialogBody>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12 }}>
            <DialogTitle>接入 GitHub 仓库</DialogTitle>
            <div className="dragon-segmented-tabs" role="tablist">
              <button
                type="button"
                role="tab"
                aria-selected={mode === "github"}
                className={`dragon-seg-tab ${mode === "github" ? "active" : ""}`}
                onClick={() => setMode("github")}
              >
                从 GitHub 选择
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={mode === "manual"}
                className={`dragon-seg-tab ${mode === "manual" ? "active" : ""}`}
                onClick={() => setMode("manual")}
              >
                手动输入
              </button>
            </div>
          </div>

          <DialogContent style={{ display: "flex", flexDirection: "column", gap: 12, marginTop: 12 }}>
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

            {mode === "github" ? (
              !isAuth ? (
                <div
                  className="dragon-subtle-box"
                  style={{
                    padding: "24px 20px",
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "center",
                    textAlign: "center",
                    gap: 10,
                  }}
                >
                  <div style={{ fontWeight: 600, fontSize: 14.5 }}>尚未连接 GitHub 账号</div>
                  <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)", maxWidth: 440, lineHeight: 1.55 }}>
                    授权后 Dragon 将自动拉取你有权限访问的个人、协作及组织仓库列表，支持多选一键批量接入。
                  </div>
                  <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
                    <Button appearance="primary" onClick={onOpenAuth}>
                      前往授权 GitHub
                    </Button>
                    <Button appearance="subtle" onClick={() => setMode("manual")}>
                      改为手动输入
                    </Button>
                  </div>
                </div>
              ) : (
                <>
                  {/* Search & Refresh */}
                  <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                    <Input
                      contentBefore={<Search16Regular />}
                      placeholder="搜索仓库名、描述或开发语言..."
                      value={search}
                      onChange={(_, d) => setSearch(d.value)}
                      style={{ flex: 1 }}
                    />
                    <Button
                      appearance="outline"
                      disabled={loadingList}
                      icon={loadingList ? <Spinner size="extra-tiny" /> : <ArrowSync16Regular />}
                      onClick={() => void loadRemoteRepos()}
                    >
                      刷新列表
                    </Button>
                  </div>

                  {/* Filter Pills & Show Archived */}
                  <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, flexWrap: "wrap" }}>
                    <div className="dragon-segmented-tabs">
                      {(
                        [
                          { id: "all", label: "全部" },
                          { id: "owner", label: "个人" },
                          { id: "org", label: "组织与协作" },
                          { id: "private", label: "私有" },
                        ] as const
                      ).map((f) => (
                        <button
                          key={f.id}
                          type="button"
                          className={`dragon-seg-tab ${filter === f.id ? "active" : ""}`}
                          onClick={() => setFilter(f.id)}
                        >
                          {f.label}
                        </button>
                      ))}
                    </div>

                    <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                      <Checkbox
                        label="显示归档"
                        checked={showArchived}
                        onChange={(_, d) => setShowArchived(Boolean(d.checked))}
                      />
                      {selectableVisible.length > 0 && (
                        <Button
                          size="small"
                          appearance="subtle"
                          onClick={() => {
                            const allVisibleSelected = selectableVisible.every((r) => selected.has(r.fullName));
                            setSelected((prev) => {
                              const next = new Set(prev);
                              for (const r of selectableVisible) {
                                if (allVisibleSelected) next.delete(r.fullName);
                                else next.add(r.fullName);
                              }
                              return next;
                            });
                          }}
                        >
                          {selectableVisible.every((r) => selected.has(r.fullName)) ? "取消全选" : "全选当前"}
                        </Button>
                      )}
                    </div>
                  </div>

                  {/* Scrollable Remote Repo List */}
                  <div
                    className="dragon-list-card"
                    style={{ maxHeight: 330, overflowY: "auto" }}
                  >
                    {loadingList && remoteRepos.length === 0 ? (
                      <div style={{ padding: 28, display: "flex", alignItems: "center", justifyContent: "center", gap: 10 }}>
                        <Spinner size="small" />
                        <span style={{ fontSize: 13, color: "var(--dragon-text-secondary)" }}>
                          正在拉取 GitHub 仓库列表...
                        </span>
                      </div>
                    ) : filteredRepos.length === 0 ? (
                      <div style={{ padding: 24, textAlign: "center", fontSize: 13, color: "var(--dragon-text-secondary)" }}>
                        {remoteRepos.length === 0
                          ? "未拉取到可访问的仓库（如果使用 Fine-grained PAT，请确认已授予仓库访问范围，或切换「手动输入」）。"
                          : "没有匹配当前筛选条件的仓库。"}
                      </div>
                    ) : (
                      filteredRepos.map((r) => {
                        const checked = r.alreadyConnected || selected.has(r.fullName);
                        return (
                          <div
                            key={r.fullName}
                            className="dragon-list-row"
                            style={{
                              padding: "9px 14px",
                              cursor: r.alreadyConnected ? "default" : "pointer",
                              opacity: r.alreadyConnected ? 0.65 : 1,
                            }}
                            onClick={() => {
                              if (!r.alreadyConnected) toggleSelect(r.fullName);
                            }}
                          >
                            <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0, flex: 1 }}>
                              <Checkbox
                                checked={checked}
                                disabled={r.alreadyConnected}
                                onClick={(e) => e.stopPropagation()}
                                onChange={() => {
                                  if (!r.alreadyConnected) toggleSelect(r.fullName);
                                }}
                                aria-label={`选择 ${r.fullName}`}
                              />
                              <div style={{ minWidth: 0, flex: 1 }}>
                                <div style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                                  <span style={{ fontWeight: 600, fontSize: 13.5 }}>{r.fullName}</span>
                                  {r.private && (
                                    <Badge size="small" appearance="tint" color="informative">
                                      私有
                                    </Badge>
                                  )}
                                  {r.archived && (
                                    <Badge size="small" appearance="tint" color="subtle">
                                      已归档
                                    </Badge>
                                  )}
                                  {!r.canPush && (
                                    <Badge size="small" appearance="tint" color="warning">
                                      只读
                                    </Badge>
                                  )}
                                  {r.alreadyConnected && (
                                    <Badge size="small" appearance="tint" color="success">
                                      已接入
                                    </Badge>
                                  )}
                                </div>
                                <div
                                  style={{
                                    fontSize: 12,
                                    color: "var(--dragon-text-secondary)",
                                    whiteSpace: "nowrap",
                                    overflow: "hidden",
                                    textOverflow: "ellipsis",
                                    marginTop: 2,
                                  }}
                                >
                                  {r.description || "无仓库简介"}
                                </div>
                              </div>
                            </div>

                            <div
                              style={{
                                fontSize: 11.5,
                                color: "var(--dragon-text-secondary)",
                                flexShrink: 0,
                                textAlign: "right",
                                whiteSpace: "nowrap",
                              }}
                            >
                              <div>
                                {r.language ? `${r.language} · ` : ""}
                                {r.defaultBranch || "main"}
                              </div>
                              <div style={{ marginTop: 2 }}>
                                {r.openIssuesCount} issues · {formatShortTime(r.pushedAt)}
                              </div>
                            </div>
                          </div>
                        );
                      })
                    )}
                  </div>
                  {/* Workspace Parent Directory Selector */}
                  <div className="dragon-subtle-box" style={{ padding: "10px 12px", display: "flex", flexDirection: "column", gap: 6 }}>
                    <div style={{ fontSize: 12, fontWeight: 600 }}>工作区父目录（可选，默认跟随系统设置）</div>
                    <div style={{ display: "flex", gap: 8 }}>
                      <Input
                        placeholder="例如 D:\DragonWorkspaces（留空则接入后手动逐个配置）"
                        value={workspaceParentDir}
                        onChange={(_, d) => setWorkspaceParentDir(d.value)}
                        style={{ flex: 1, minWidth: 0 }}
                      />
                      <Button
                        appearance="outline"
                        icon={<Folder16Regular />}
                        onClick={() => void handlePickBatchParent()}
                      >
                        选择文件夹
                      </Button>
                    </div>
                    <div style={{ fontSize: 11.5, color: "var(--dragon-text-secondary)" }}>
                      {workspaceParentDir.trim()
                        ? `所选仓库将默认绑定至 ${joinWorkspacePath(workspaceParentDir, "<仓库名>")}（同名冲突时自动区分 owner__repo）。`
                        : "当前未设置工作区父目录；接入后需到「仓库管理 -> 配置」中指定本地目录后才能执行流水线。"}
                    </div>
                  </div>
                </>
              )
            ) : (
              /* Manual Fallback Input Mode */
              <div className="dragon-subtle-box" style={{ padding: 14, display: "flex", flexDirection: "column", gap: 10 }}>
                <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)" }}>
                  适用于接入外部开源仓库，或 Fine-grained PAT 未在 <code>/user/repos</code> 中列出的指定仓库。
                </div>
                <div style={{ display: "flex", gap: 8 }}>
                  <Input
                    placeholder="输入 owner/repo 或完整 GitHub URL"
                    value={manualInput}
                    onChange={(_, d) => {
                      const val = d.value;
                      setManualInput(val);
                      if (!manualPathEdited && defaultWorkspaceRoot.trim()) {
                        const repoName = extractManualRepoName(val);
                        setManualLocalPath(
                          repoName ? joinWorkspacePath(defaultWorkspaceRoot, repoName) : ""
                        );
                      }
                    }}
                    style={{ flex: 1 }}
                  />
                  <Button
                    appearance="outline"
                    disabled={submitting || !manualInput.trim()}
                    onClick={() => void handleTestManual()}
                  >
                    测试连接
                  </Button>
                </div>
                <div>
                  <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4 }}>
                    本地工作区目录（执行流水线必填）
                  </div>
                  <div style={{ display: "flex", gap: 8 }}>
                    <Input
                      placeholder="例如 D:\Projects\my-repo（空目录将在首次执行时自动克隆）"
                      value={manualLocalPath}
                      onChange={(_, d) => {
                        setManualLocalPath(d.value);
                        setManualPathEdited(true);
                      }}
                      style={{ flex: 1, minWidth: 0 }}
                    />
                    <Button
                      appearance="outline"
                      icon={<Folder16Regular />}
                      onClick={() => void handlePickManualLocalPath()}
                    >
                      选择文件夹
                    </Button>
                  </div>
                </div>
              </div>
            )}

            {/* Collapsible Advanced Default Options */}
            {(mode === "manual" || isAuth) && (
              <div className="dragon-subtle-box" style={{ padding: "8px 12px" }}>
                <button
                  type="button"
                  onClick={() => setShowAdvanced((v) => !v)}
                  style={{
                    width: "100%",
                    border: "none",
                    background: "transparent",
                    color: "var(--dragon-text-secondary)",
                    fontSize: 12.5,
                    fontWeight: 600,
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    cursor: "pointer",
                    padding: "2px 0",
                  }}
                >
                  <span>高级：自定义本次默认接入参数（触发标签: {triggerLabel || "dragon-auto"}）</span>
                  {showAdvanced ? <ChevronUp16Regular /> : <ChevronDown16Regular />}
                </button>
                {showAdvanced && (
                  <div style={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)", gap: 10, marginTop: 10 }}>
                    <div style={{ minWidth: 0 }}>
                      <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4 }}>自动触发 Label</div>
                      <Input
                        value={triggerLabel}
                        onChange={(_, d) => setTriggerLabel(d.value)}
                        placeholder="dragon-auto"
                        style={{ width: "100%" }}
                      />
                    </div>
                    <div style={{ minWidth: 0 }}>
                      <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4 }}>自动化门禁档位</div>
                      <Dropdown
                        value={AUTONOMY_LABELS[autonomyMode] || autonomyMode}
                        selectedOptions={[autonomyMode]}
                        onOptionSelect={(_, d) => d.optionValue && setAutonomyMode(d.optionValue)}
                        style={{ width: "100%", minWidth: 0 }}
                      >
                        <Option value="template">跟随模板门禁（推荐）</Option>
                        <Option value="semi_auto">半自动护航</Option>
                        <Option value="full_auto">全自动一条龙</Option>
                        <Option value="step_by_step">多节点强审批</Option>
                      </Dropdown>
                    </div>
                  </div>
                )}
              </div>
            )}
          </DialogContent>

          <DialogActions>
            {mode === "github" && isAuth && (
              <span style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)", marginRight: "auto" }}>
                已选择 <b>{selected.size}</b> 个仓库
              </span>
            )}
            <Button appearance="secondary" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            {mode === "github" ? (
              isAuth && (
                <Button
                  appearance="primary"
                  icon={<Add20Regular />}
                  disabled={submitting || selected.size === 0}
                  onClick={() => void handleBatchAdd()}
                >
                  {submitting ? "接入中..." : `接入所选仓库 (${selected.size})`}
                </Button>
              )
            ) : (
              <Button
                appearance="primary"
                icon={<Add20Regular />}
                disabled={submitting || !manualInput.trim()}
                onClick={() => void handleAddManual()}
              >
                {submitting ? "接入中..." : "接入仓库"}
              </Button>
            )}
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}
