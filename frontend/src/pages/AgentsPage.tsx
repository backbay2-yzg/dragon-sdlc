import { useCallback, useEffect, useState } from "react";
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
import { Add16Regular, ArrowSync20Regular, Delete16Regular } from "@fluentui/react-icons";
import { AgentService } from "../../bindings/dragon/internal/appservice/index.js";
import type {
  AgentProfile,
  ModelCatalogOption,
  ModelProvider,
  PipelineTemplate,
  Repository,
  RoutingConfig,
  RoutingRule,
  StageDescriptor,
  WorkspaceProfile,
} from "../types/dragon.js";
import { AgentsListTab } from "../components/AgentsListTab.js";
import { ProvidersTab } from "../components/ProvidersTab.js";
import { RoutingTab } from "../components/RoutingTab.js";

type SubView = "agents" | "providers" | "routing" | "templates";

interface AgentsPageProps {
  repos: Repository[];
  requestedSubView?: { view: SubView; nonce: number };
}

export function AgentsPage({ repos, requestedSubView }: AgentsPageProps) {
  const [subView, setSubView] = useState<SubView>(requestedSubView?.view || "agents");

  useEffect(() => {
    if (requestedSubView?.view) {
      setSubView(requestedSubView.view);
    }
  }, [requestedSubView]);
  const [agents, setAgents] = useState<AgentProfile[]>([]);
  const [providers, setProviders] = useState<ModelProvider[]>([]);
  const [models, setModels] = useState<ModelCatalogOption[]>([]);
  const [profiles, setProfiles] = useState<WorkspaceProfile[]>([]);
  const [rules, setRules] = useState<RoutingRule[]>([]);
  const [templates, setTemplates] = useState<PipelineTemplate[]>([]);
  const [stages, setStages] = useState<StageDescriptor[]>([]);
  const [routingConfig, setRoutingConfig] = useState<RoutingConfig>({
    strategy: "rule_ai",
    routerModelRef: "",
    fallbackAgentId: "",
    stageBindings: {},
  });

  const [scanning, setScanning] = useState<boolean>(false);
  const [msg, setMsg] = useState<string>("");
  const [errMsg, setErrMsg] = useState<string>("");

  const [newAgentOpen, setNewAgentOpen] = useState<boolean>(false);
  const [newProviderOpen, setNewProviderOpen] = useState<boolean>(false);
  const [newRuleOpen, setNewRuleOpen] = useState<boolean>(false);
  const [editTpl, setEditTpl] = useState<PipelineTemplate | null>(null);

  const loadAll = useCallback(async () => {
    try {
      const snap = await AgentService.GetWorkspaceSnapshot();
      if (!snap) return;
      setAgents(snap.agents || []);
      setProviders(snap.providers || []);
      setModels(snap.models || []);
      setProfiles(snap.profiles || []);
      setRules(snap.rules || []);
      setTemplates(snap.templates || []);
      setStages(snap.stages || []);
      if (snap.routingConfig) setRoutingConfig(snap.routingConfig);
    } catch (e) {
      setErrMsg(String(e));
    }
  }, []);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  const handleDetectAll = async () => {
    setScanning(true);
    setMsg("");
    setErrMsg("");
    try {
      const updated = await AgentService.DetectAgents();
      setAgents(updated || []);
      const found = (updated || []).filter((a) => a.detected).length;
      setMsg(`深度扫描完成，本机共检测到 ${found} 个可用 Coding Agent CLI（含桌面端内嵌 CLI）。`);
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setScanning(false);
    }
  };

  const handleContextualAdd = () => {
    switch (subView) {
      case "agents":
        setNewAgentOpen(true);
        break;
      case "providers":
        setNewProviderOpen(true);
        break;
      case "routing":
        setNewRuleOpen(true);
        break;
      case "templates":
        setEditTpl({
          id: "",
          name: "",
          description: "",
          stagesJson: JSON.stringify(stages.map((s) => s.key)),
          defaultGatesJson: '{"merge_release":true}',
          isBuiltin: false,
        });
        break;
    }
  };

  const parseTemplateStages = (raw: string): string[] => {
    try {
      const arr = JSON.parse(raw || "[]");
      return Array.isArray(arr) ? arr : [];
    } catch {
      return [];
    }
  };

  const parseTemplateGates = (raw: string): Record<string, boolean> => {
    try {
      const obj = JSON.parse(raw || "{}");
      return obj && typeof obj === "object" ? obj : {};
    } catch {
      return {};
    }
  };

  const handleDeleteTemplate = async (tpl: PipelineTemplate) => {
    setErrMsg("");
    setMsg("");
    try {
      await AgentService.DeleteTemplate(tpl.id);
      await loadAll();
      setMsg(`模板「${tpl.name}」已删除。`);
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  const saveTemplate = async () => {
    if (!editTpl || !editTpl.name.trim()) return;
    setErrMsg("");
    try {
      await AgentService.SaveTemplate(editTpl);
      setEditTpl(null);
      await loadAll();
      setMsg(`模板「${editTpl.name}」已保存。`);
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  const addButtonLabel =
    subView === "agents"
      ? "自定义 CLI"
      : subView === "providers"
      ? "自定义 Provider"
      : subView === "routing"
      ? "路由规则"
      : "SDLC 模板";

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14, maxWidth: 1080, margin: "0 auto" }}>
      {/* Magpie-inspired Top Segmented Header */}
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
        <div className="dragon-segmented-tabs" role="tablist">
          {(
            [
              { id: "agents", label: "Agents" },
              { id: "providers", label: "Providers" },
              { id: "routing", label: "Routing" },
              { id: "templates", label: "Templates" },
            ] as const
          ).map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={subView === t.id}
              className={`dragon-seg-tab ${subView === t.id ? "active" : ""}`}
              onClick={() => setSubView(t.id)}
            >
              {t.label}
            </button>
          ))}
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <Tooltip content="重新扫描本机 Coding CLI 与模型状态" relationship="label">
            <Button
              size="small"
              appearance="subtle"
              disabled={scanning}
              icon={scanning ? <Spinner size="extra-tiny" /> : <ArrowSync20Regular />}
              onClick={() => void handleDetectAll()}
            >
              {scanning ? "扫描中" : "扫描本机"}
            </Button>
          </Tooltip>
          <Button size="small" appearance="outline" icon={<Add16Regular />} onClick={handleContextualAdd}>
            {addButtonLabel}
          </Button>
        </div>
      </div>

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

      {subView === "agents" && (
        <AgentsListTab
          agents={agents}
          models={models}
          profiles={profiles}
          onReload={loadAll}
          onMessage={setMsg}
          onError={setErrMsg}
          openNewAgentModal={newAgentOpen}
          onCloseNewAgentModal={() => setNewAgentOpen(false)}
        />
      )}

      {subView === "providers" && (
        <ProvidersTab
          providers={providers}
          onReload={loadAll}
          onMessage={setMsg}
          onError={setErrMsg}
          openNewProviderModal={newProviderOpen}
          onCloseNewProviderModal={() => setNewProviderOpen(false)}
        />
      )}

      {subView === "routing" && (
        <RoutingTab
          repos={repos}
          agents={agents}
          models={models}
          rules={rules}
          templates={templates}
          stages={stages}
          routingConfig={routingConfig}
          onReload={loadAll}
          onMessage={setMsg}
          onError={setErrMsg}
          openNewRuleModal={newRuleOpen}
          onCloseNewRuleModal={() => setNewRuleOpen(false)}
        />
      )}

      {subView === "templates" && (
        <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)" }}>
            定义不同类型 Issue 的 SDLC 阶段与审批门禁（简单修复快车道 / 复杂修复深度排查 / 自定义流水线）；当仓库审批档位选择「跟随模板门禁」时直接生效。
          </div>
          <div className="dragon-list-card">
            {templates.map((tpl) => {
              const enabledKeys = parseTemplateStages(tpl.stagesJson);
              const gatesMap = parseTemplateGates(tpl.defaultGatesJson);
              return (
                <div key={tpl.id} className="dragon-list-row">
                  <div style={{ minWidth: 0, flex: 1 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                      <span style={{ fontWeight: 600, fontSize: 14 }}>{tpl.name}</span>
                      <Badge size="small" appearance="outline">
                        {tpl.id}
                      </Badge>
                      {tpl.isBuiltin && (
                        <Badge size="small" appearance="tint" color="informative">
                          内置
                        </Badge>
                      )}
                    </div>
                    <div style={{ fontSize: 12, color: "var(--dragon-text-secondary)", marginTop: 3 }}>
                      {tpl.description}
                    </div>
                    <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginTop: 6 }}>
                      {stages.map((st) => {
                        const active = enabledKeys.includes(st.key);
                        const gated = active && Boolean(gatesMap[st.key]);
                        return (
                          <Badge
                            key={st.key}
                            size="small"
                            appearance={active ? "tint" : "outline"}
                            color={gated ? "warning" : active ? "brand" : "subtle"}
                          >
                            {st.order}. {st.name}
                            {gated ? " · ⏸ 审批" : ""}
                          </Badge>
                        );
                      })}
                    </div>
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 6, flexShrink: 0 }}>
                    <Button size="small" appearance="outline" onClick={() => setEditTpl({ ...tpl })}>
                      编辑阶段与门禁
                    </Button>
                    {!tpl.isBuiltin && (
                      <Tooltip content="删除自定义模板" relationship="label">
                        <Button
                          size="small"
                          appearance="subtle"
                          className="dragon-danger-btn"
                          icon={<Delete16Regular />}
                          aria-label={`删除 ${tpl.name}`}
                          onClick={() => void handleDeleteTemplate(tpl)}
                        />
                      </Tooltip>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Template Editor Modal */}
      <Dialog open={editTpl !== null} onOpenChange={(_, d) => !d.open && setEditTpl(null)}>
        <DialogSurface style={{ maxWidth: 540, width: "calc(100vw - 48px)" }}>
          <DialogBody>
            <DialogTitle>{editTpl?.id ? `编辑模板: ${editTpl.name}` : "新增 SDLC 流水线模板"}</DialogTitle>
            {editTpl && (
              <DialogContent style={{ display: "flex", flexDirection: "column", gap: 12, marginTop: 10 }}>
                <div>
                  <div className="dragon-field-label">模板名称</div>
                  <Input
                    placeholder="例如：架构重构深度流"
                    value={editTpl.name}
                    onChange={(_, d) => setEditTpl({ ...editTpl, name: d.value })}
                    style={{ width: "100%" }}
                  />
                </div>
                <div>
                  <div className="dragon-field-label">模板说明</div>
                  <Input
                    placeholder="简要描述适用哪类 Issue"
                    value={editTpl.description}
                    onChange={(_, d) => setEditTpl({ ...editTpl, description: d.value })}
                    style={{ width: "100%" }}
                  />
                </div>
                <div>
                  <div className="dragon-field-label">启用的 SDLC 阶段与审批门禁</div>
                  <div className="dragon-list-card">
                    {stages.map((st) => {
                      const current = parseTemplateStages(editTpl.stagesJson);
                      const gatesMap = parseTemplateGates(editTpl.defaultGatesJson);
                      const checked = current.includes(st.key);
                      const gated = checked && Boolean(gatesMap[st.key]);
                      return (
                        <div
                          key={st.key}
                          className="dragon-list-row"
                          style={{ padding: "7px 12px" }}
                        >
                          <Checkbox
                            label={`${st.order}. ${st.name}`}
                            checked={checked}
                            onChange={(_, d) => {
                              const enabled = Boolean(d.checked);
                              const next = enabled
                                ? [...new Set([...current, st.key])]
                                : current.filter((k) => k !== st.key);
                              const ordered = stages.map((s) => s.key).filter((k) => next.includes(k));
                              const nextGates = { ...gatesMap };
                              if (!enabled) {
                                delete nextGates[st.key];
                              }
                              setEditTpl({
                                ...editTpl,
                                stagesJson: JSON.stringify(ordered),
                                defaultGatesJson: JSON.stringify(nextGates),
                              });
                            }}
                          />
                          <Checkbox
                            label="审批暂停"
                            disabled={!checked}
                            checked={gated}
                            onChange={(_, d) => {
                              const nextGates = { ...gatesMap, [st.key]: Boolean(d.checked) };
                              if (!d.checked) {
                                delete nextGates[st.key];
                              }
                              setEditTpl({
                                ...editTpl,
                                defaultGatesJson: JSON.stringify(nextGates),
                              });
                            }}
                          />
                        </div>
                      );
                    })}
                  </div>
                </div>
              </DialogContent>
            )}
            <DialogActions>
              <Button appearance="secondary" onClick={() => setEditTpl(null)}>
                取消
              </Button>
              <Button appearance="primary" disabled={!editTpl?.name.trim()} onClick={() => void saveTemplate()}>
                保存模板
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </div>
  );
}
