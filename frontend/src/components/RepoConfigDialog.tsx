import { useEffect, useState } from "react";
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
  Switch,
} from "@fluentui/react-components";
import {
  ArrowDownload16Regular,
  Folder16Regular,
  FolderOpen16Regular,
  Open16Regular,
  Save16Regular,
} from "@fluentui/react-icons";
import { RepoService, SettingsService } from "../../bindings/dragon/internal/appservice/index.js";
import type { PipelineTemplate, Repository, WorkspaceCheckResult } from "../types/dragon.js";

interface RepoConfigDialogProps {
  repo: Repository | null;
  templates: PipelineTemplate[];
  defaultWorkspaceRoot: string;
  onClose: () => void;
  onSave: (repo: Repository) => Promise<void>;
  onRepoUpdated?: () => void;
  onJumpToCustomize: () => void;
}

type ConfigTab = "flow" | "build";

const AUTONOMY_CHOICES = [
  {
    id: "template",
    title: "跟随模板门禁",
    badge: "推荐",
    desc: "简单修复仅合并前确认，复杂修复自动增加方案设计审批",
  },
  {
    id: "full_auto",
    title: "全自动一条龙",
    desc: "全程免人工暂停，测试与 CI 通过后自动合并发布",
  },
  {
    id: "semi_auto",
    title: "仅合并前审批",
    desc: "无论简单或复杂流程，统一仅在合并 PR 与发版前暂停",
  },
  {
    id: "step_by_step",
    title: "多节点强审批",
    desc: "方案设计、提交 PR 前、合并发布前均暂停人工确认",
  },
] as const;

const MERGE_STRATEGY_LABELS: Record<string, string> = {
  squash: "Squash（压缩为单条提交）",
  merge: "Merge（保留完整分支记录）",
  rebase: "Rebase（线性变基合并）",
};

const RELEASE_MODE_LABELS: Record<string, string> = {
  batch: "批量发版列车",
  immediate: "单 PR 即时发布",
};

function joinDefaultWorkspace(rootDir: string, name: string): string {
  const cleanRoot = rootDir.trim().replace(/[\\/]+$/, "");
  const cleanName = name.trim().replace(/^[\\/]+/, "");
  if (!cleanRoot || !cleanName) return "";
  const sep = cleanRoot.includes("/") && !cleanRoot.includes("\\") ? "/" : "\\";
  return `${cleanRoot}${sep}${cleanName}`;
}

export function RepoConfigDialog({
  repo,
  templates,
  defaultWorkspaceRoot,
  onClose,
  onSave,
  onRepoUpdated,
  onJumpToCustomize,
}: RepoConfigDialogProps) {
  const [draft, setDraft] = useState<Repository | null>(null);
  const [tab, setTab] = useState<ConfigTab>("flow");
  const [saving, setSaving] = useState<boolean>(false);
  const [errMsg, setErrMsg] = useState<string>("");
  const [wsCheck, setWsCheck] = useState<WorkspaceCheckResult | null>(null);
  const [checkingWs, setCheckingWs] = useState<boolean>(false);
  const [cloningWs, setCloningWs] = useState<boolean>(false);
  const [cloneMsg, setCloneMsg] = useState<string>("");

  useEffect(() => {
    if (repo) {
      setDraft({
        ...repo,
        localPath: repo.localPath || "",
        defaultTemplateId: repo.defaultTemplateId || "auto",
        autonomyMode: repo.autonomyMode || "template",
      });
      setTab("flow");
      setErrMsg("");
      setCloneMsg("");
      setWsCheck(null);
    } else {
      setDraft(null);
      setWsCheck(null);
      setCloneMsg("");
    }
  }, [repo]);

  useEffect(() => {
    if (!draft) return;
    let cancelled = false;
    setCheckingWs(true);
    const timer = window.setTimeout(() => {
      void RepoService.ValidateRepoWorkspace(draft.id, draft.fullName, draft.localPath || "")
        .then((res) => {
          if (!cancelled) {
            setWsCheck(res);
          }
        })
        .catch(() => {
          if (!cancelled) {
            setWsCheck(null);
          }
        })
        .finally(() => {
          if (!cancelled) {
            setCheckingWs(false);
          }
        });
    }, 200);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [draft?.id, draft?.fullName, draft?.localPath]);

  const parseStageCount = (stagesJson: string): number => {
    try {
      const arr = JSON.parse(stagesJson || "[]");
      return Array.isArray(arr) ? arr.length : 0;
    } catch {
      return 0;
    }
  };

  const selectedTemplate = templates.find((t) => t.id === draft?.defaultTemplateId);
  const flowDisplayValue =
    !draft?.defaultTemplateId || draft.defaultTemplateId === "auto"
      ? "智能自动分流（推荐）"
      : selectedTemplate?.name || draft.defaultTemplateId;

  const handleSaveClick = async () => {
    if (!draft) return;
    setSaving(true);
    setErrMsg("");
    try {
      await onSave(draft);
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setSaving(false);
    }
  };

  const handlePickFolder = async () => {
    if (!draft) return;
    setErrMsg("");
    setCloneMsg("");
    try {
      const picked = await SettingsService.PickDirectory(
        `选择 ${draft.fullName} 的本地工作区目录`,
        draft.localPath || defaultWorkspaceRoot || ""
      );
      if (picked) {
        setDraft({ ...draft, localPath: picked });
      }
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  const handleOpenFolder = async () => {
    if (!draft?.localPath.trim()) return;
    setErrMsg("");
    try {
      await SettingsService.OpenDirectory(draft.localPath);
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  const handleCloneNow = async () => {
    if (!draft) return;
    setCloningWs(true);
    setErrMsg("");
    setCloneMsg("");
    try {
      const res = await RepoService.CloneRepoWorkspace(draft.id, draft.localPath);
      if (res) {
        setWsCheck(res);
        setDraft((prev) => (prev ? { ...prev, localPath: res.path || prev.localPath } : prev));
        setCloneMsg("仓库已克隆并绑定至本地工作区，可以立即运行流水线。");
        onRepoUpdated?.();
      }
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setCloningWs(false);
    }
  };

  const wsBadgeColor =
    wsCheck?.status === "ready"
      ? "success"
      : wsCheck?.status === "pending_clone"
      ? "informative"
      : wsCheck?.status === "invalid"
      ? "danger"
      : "warning";

  const wsBadgeLabel =
    wsCheck?.status === "ready"
      ? "已就绪"
      : wsCheck?.status === "pending_clone"
      ? "待克隆"
      : wsCheck?.status === "invalid"
      ? "路径无效"
      : "未配置";

  const invalidConfiguredPath = Boolean(draft && draft.localPath.trim() !== "" && wsCheck && !wsCheck.ok);

  return (
    <Dialog open={draft !== null} onOpenChange={(_, d) => !d.open && onClose()}>
      <DialogSurface style={{ maxWidth: 600, width: "calc(100vw - 48px)" }}>
        <DialogBody>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              gap: 12,
              flexWrap: "wrap",
            }}
          >
            <DialogTitle style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>
              配置仓库：{draft?.fullName}
            </DialogTitle>
            <div className="dragon-segmented-tabs" role="tablist">
              <button
                type="button"
                role="tab"
                aria-selected={tab === "flow"}
                className={`dragon-seg-tab ${tab === "flow" ? "active" : ""}`}
                onClick={() => setTab("flow")}
              >
                流程与策略
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={tab === "build"}
                className={`dragon-seg-tab ${tab === "build" ? "active" : ""}`}
                onClick={() => setTab("build")}
              >
                构建与回写
              </button>
            </div>
          </div>

          {draft && (
            <DialogContent
              style={{
                display: "flex",
                flexDirection: "column",
                gap: 14,
                marginTop: 14,
                overflowX: "hidden",
              }}
            >
              {errMsg && (
                <MessageBar intent="error">
                  <MessageBarBody>{errMsg}</MessageBarBody>
                </MessageBar>
              )}
              {cloneMsg && (
                <MessageBar intent="success">
                  <MessageBarBody>{cloneMsg}</MessageBarBody>
                </MessageBar>
              )}

              {tab === "flow" ? (
                <>
                  {/* 0. Local Workspace Directory (Required for Execution) */}
                  <div
                    className="dragon-subtle-box"
                    style={{ padding: "10px 12px", display: "flex", flexDirection: "column", gap: 8 }}
                  >
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
                      <div className="dragon-field-label" style={{ marginBottom: 0 }}>
                        本地工作区目录（执行必填）
                      </div>
                      {!draft.localPath.trim() && defaultWorkspaceRoot.trim() && (
                        <Button
                          size="small"
                          appearance="subtle"
                          onClick={() =>
                            setDraft({
                              ...draft,
                              localPath: joinDefaultWorkspace(defaultWorkspaceRoot, draft.name),
                            })
                          }
                        >
                          使用默认路径
                        </Button>
                      )}
                    </div>
                    <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
                      <Input
                        placeholder="例如 D:\DragonWorkspaces\my-repo（空目录将自动克隆）"
                        value={draft.localPath}
                        onChange={(_, d) => {
                          setCloneMsg("");
                          setDraft({ ...draft, localPath: d.value });
                        }}
                        style={{ flex: 1, minWidth: 0 }}
                      />
                      <Button
                        appearance="outline"
                        icon={<Folder16Regular />}
                        onClick={() => void handlePickFolder()}
                      >
                        选择文件夹
                      </Button>
                      {draft.localPath.trim() && (
                        <Button
                          appearance="subtle"
                          icon={<FolderOpen16Regular />}
                          onClick={() => void handleOpenFolder()}
                        >
                          打开
                        </Button>
                      )}
                    </div>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, flexWrap: "wrap" }}>
                      <div style={{ display: "flex", alignItems: "center", gap: 6, minWidth: 0, flex: 1 }}>
                        {checkingWs ? (
                          <Spinner size="extra-tiny" />
                        ) : (
                          wsCheck && (
                            <Badge size="small" appearance="tint" color={wsBadgeColor}>
                              {wsBadgeLabel}
                            </Badge>
                          )
                        )}
                        <span style={{ fontSize: 11.5, color: "var(--dragon-text-secondary)", lineHeight: 1.4 }}>
                          {wsCheck?.message || "每次任务将基于此仓库在同级 .dragon-worktrees 目录创建隔离 Worktree。"}
                        </span>
                      </div>
                      {wsCheck?.status === "pending_clone" && (
                        <Button
                          size="small"
                          appearance="outline"
                          disabled={cloningWs}
                          icon={cloningWs ? <Spinner size="extra-tiny" /> : <ArrowDownload16Regular />}
                          onClick={() => void handleCloneNow()}
                        >
                          {cloningWs ? "克隆中..." : "立即克隆"}
                        </Button>
                      )}
                    </div>
                  </div>

                  {/* 1. Issue Flow / Triage Mode */}
                  <div>
                    <div className="dragon-field-label">Issue 处理流程</div>
                    <Dropdown
                      value={flowDisplayValue}
                      selectedOptions={[draft.defaultTemplateId || "auto"]}
                      onOptionSelect={(_, d) =>
                        d.optionValue && setDraft({ ...draft, defaultTemplateId: d.optionValue })
                      }
                      style={{ width: "100%", minWidth: 0 }}
                    >
                      <Option value="auto" text="智能自动分流（推荐）">
                        智能自动分流（按 Issue 自动选 简单修复 / 复杂修复 / 功能 / 文档）
                      </Option>
                      {templates.map((tpl) => (
                        <Option
                          key={tpl.id}
                          value={tpl.id}
                          text={`${tpl.name} (${parseStageCount(tpl.stagesJson)} 阶段)`}
                        >
                          {tpl.name} · {parseStageCount(tpl.stagesJson)} 阶段
                        </Option>
                      ))}
                    </Dropdown>
                    <div className="dragon-field-hint">
                      {draft.defaultTemplateId === "auto" || !draft.defaultTemplateId
                        ? "收到新 Issue 时自动识别复杂度：轻量 Bug 走「简单修复」4 阶段快车道，高风险或架构类问题走「复杂修复」7 阶段。"
                        : selectedTemplate?.description || "已锁定为固定 SDLC 流水线模板（仍受自定义路由规则覆盖）。"}
                    </div>
                  </div>

                  {/* 2. Approval Gate Mode (2x2 Choice Cards) */}
                  <div>
                    <div className="dragon-field-label">审批门禁档位</div>
                    <div
                      style={{
                        display: "grid",
                        gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)",
                        gap: 8,
                      }}
                    >
                      {AUTONOMY_CHOICES.map((ch) => {
                        const active = draft.autonomyMode === ch.id;
                        return (
                          <button
                            key={ch.id}
                            type="button"
                            className={`dragon-choice-card ${active ? "active" : ""}`}
                            onClick={() => setDraft({ ...draft, autonomyMode: ch.id })}
                          >
                            <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                              <span style={{ fontWeight: 600, fontSize: 13 }}>{ch.title}</span>
                              {"badge" in ch && ch.badge && (
                                <Badge size="small" appearance="tint" color="brand">
                                  {ch.badge}
                                </Badge>
                              )}
                            </div>
                            <span
                              style={{
                                fontSize: 11.5,
                                color: "var(--dragon-text-secondary)",
                                lineHeight: 1.4,
                              }}
                            >
                              {ch.desc}
                            </span>
                          </button>
                        );
                      })}
                    </div>
                  </div>

                  {/* 3. Trigger, Polling, Merge & Release (clean 2x2 grid with minWidth: 0) */}
                  <div
                    style={{
                      display: "grid",
                      gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)",
                      gap: 10,
                    }}
                  >
                    <div style={{ minWidth: 0 }}>
                      <div className="dragon-field-label">自动触发 Label</div>
                      <Input
                        value={draft.triggerLabel}
                        onChange={(_, d) => setDraft({ ...draft, triggerLabel: d.value })}
                        placeholder="dragon-auto"
                        style={{ width: "100%" }}
                      />
                    </div>
                    <div style={{ minWidth: 0 }}>
                      <div className="dragon-field-label">轮询间隔 (秒)</div>
                      <Input
                        type="number"
                        value={String(draft.pollIntervalSec)}
                        onChange={(_, d) =>
                          setDraft({ ...draft, pollIntervalSec: Math.max(15, Number(d.value) || 120) })
                        }
                        style={{ width: "100%" }}
                      />
                    </div>
                    <div style={{ minWidth: 0 }}>
                      <div className="dragon-field-label">PR 合并策略</div>
                      <Dropdown
                        value={MERGE_STRATEGY_LABELS[draft.mergeStrategy] || draft.mergeStrategy}
                        selectedOptions={[draft.mergeStrategy]}
                        onOptionSelect={(_, d) =>
                          d.optionValue && setDraft({ ...draft, mergeStrategy: d.optionValue })
                        }
                        style={{ width: "100%", minWidth: 0 }}
                      >
                        <Option value="squash">Squash（压缩为单条提交）</Option>
                        <Option value="merge">Merge（保留完整分支记录）</Option>
                        <Option value="rebase">Rebase（线性变基合并）</Option>
                      </Dropdown>
                    </div>
                    <div style={{ minWidth: 0 }}>
                      <div className="dragon-field-label">发版模式</div>
                      <Dropdown
                        value={RELEASE_MODE_LABELS[draft.releaseMode] || draft.releaseMode}
                        selectedOptions={[draft.releaseMode]}
                        onOptionSelect={(_, d) =>
                          d.optionValue && setDraft({ ...draft, releaseMode: d.optionValue })
                        }
                        style={{ width: "100%", minWidth: 0 }}
                      >
                        <Option value="batch">批量发版列车</Option>
                        <Option value="immediate">单 PR 即时发布</Option>
                      </Dropdown>
                    </div>
                  </div>
                </>
              ) : (
                <>
                  {/* Build, Test & Lint Commands */}
                  <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
                    <div>
                      <div className="dragon-field-label">构建命令（选填，留空则跳过）</div>
                      <Input
                        placeholder="例如 pnpm build 或 go build ./..."
                        value={draft.buildCmd}
                        onChange={(_, d) => setDraft({ ...draft, buildCmd: d.value })}
                        style={{ width: "100%" }}
                      />
                    </div>
                    <div>
                      <div className="dragon-field-label">测试命令（选填，留空则跳过）</div>
                      <Input
                        placeholder="例如 pnpm test 或 go test ./..."
                        value={draft.testCmd}
                        onChange={(_, d) => setDraft({ ...draft, testCmd: d.value })}
                        style={{ width: "100%" }}
                      />
                    </div>
                    <div>
                      <div className="dragon-field-label">Lint 检查命令（选填）</div>
                      <Input
                        placeholder="例如 pnpm lint"
                        value={draft.lintCmd}
                        onChange={(_, d) => setDraft({ ...draft, lintCmd: d.value })}
                        style={{ width: "100%" }}
                      />
                    </div>
                  </div>

                  {/* Retry Limits */}
                  <div
                    style={{
                      display: "grid",
                      gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)",
                      gap: 10,
                    }}
                  >
                    <div style={{ minWidth: 0 }}>
                      <div className="dragon-field-label">本地测试失败自动修复上限</div>
                      <Input
                        type="number"
                        value={String(draft.maxTestRetries)}
                        onChange={(_, d) =>
                          setDraft({ ...draft, maxTestRetries: Math.max(0, Number(d.value) || 0) })
                        }
                        style={{ width: "100%" }}
                      />
                    </div>
                    <div style={{ minWidth: 0 }}>
                      <div className="dragon-field-label">GitHub CI 失败自动修复上限</div>
                      <Input
                        type="number"
                        value={String(draft.maxCiRetries)}
                        onChange={(_, d) =>
                          setDraft({ ...draft, maxCiRetries: Math.max(0, Number(d.value) || 0) })
                        }
                        style={{ width: "100%" }}
                      />
                    </div>
                  </div>

                  {/* GitHub Sync Switches */}
                  <div className="dragon-subtle-box" style={{ padding: "10px 12px", display: "flex", flexDirection: "column", gap: 4 }}>
                    <div className="dragon-field-label" style={{ marginBottom: 2 }}>
                      GitHub 同步与互动开关
                    </div>
                    <Switch
                      label="向公开 GitHub Issue 自动同步状态 Label"
                      checked={draft.syncLabels}
                      onChange={(_, d) => setDraft({ ...draft, syncLabels: d.checked })}
                    />
                    <Switch
                      label="提交 PR 时在原 Issue 下自动发表评论通知"
                      checked={draft.syncComments}
                      onChange={(_, d) => setDraft({ ...draft, syncComments: d.checked })}
                    />
                    <Switch
                      label="需求不明确时保存澄清草稿（不直接公开发布）"
                      checked={draft.draftClarifications}
                      onChange={(_, d) => setDraft({ ...draft, draftClarifications: d.checked })}
                    />
                    <Switch
                      label="启用此仓库后台定时轮询"
                      checked={draft.enabled}
                      onChange={(_, d) => setDraft({ ...draft, enabled: d.checked })}
                    />
                  </div>
                </>
              )}
            </DialogContent>
          )}

          <DialogActions>
            <Button
              size="small"
              appearance="subtle"
              icon={<Open16Regular />}
              style={{ marginRight: "auto" }}
              onClick={onJumpToCustomize}
            >
              自定义流程与阶段 Agent
            </Button>
            <Button appearance="secondary" onClick={onClose}>
              取消
            </Button>
            <Button
              appearance="primary"
              disabled={saving || cloningWs || invalidConfiguredPath}
              icon={<Save16Regular />}
              onClick={() => void handleSaveClick()}
            >
              {saving ? "保存中..." : "保存仓库配置"}
            </Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}
