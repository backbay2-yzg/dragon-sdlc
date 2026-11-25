import { useState } from "react";
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
  Option,
  Spinner,
  Switch,
} from "@fluentui/react-components";
import { Add16Regular, Delete16Regular, Play16Regular, Sparkle16Regular } from "@fluentui/react-icons";
import { AgentService } from "../../bindings/dragon/internal/appservice/index.js";
import type {
  AgentProfile,
  ModelCatalogOption,
  PipelineTemplate,
  Repository,
  RoutingConfig,
  RoutingDecision,
  RoutingRule,
  StageDescriptor,
  StageRouteBinding,
} from "../types/dragon.js";
import { BrandIcon } from "./BrandIcon.js";
import { ModelPickerPill } from "./ModelPickerPill.js";

interface RoutingTabProps {
  repos: Repository[];
  agents: AgentProfile[];
  models: ModelCatalogOption[];
  rules: RoutingRule[];
  templates: PipelineTemplate[];
  stages: StageDescriptor[];
  routingConfig: RoutingConfig;
  onReload: () => Promise<void>;
  onMessage: (msg: string) => void;
  onError: (err: string) => void;
  openNewRuleModal: boolean;
  onCloseNewRuleModal: () => void;
}

const STRATEGIES = [
  { id: "rule_ai", label: "规则优先 + AI 兜底", desc: "先匹配自定义规则，未命中时由路由模型智能评估" },
  { id: "budget_first", label: "成本优先 (Budget)", desc: "优先挑选轻量高速 Agent 与 Budget 档位模型" },
  { id: "quality_first", label: "旗舰质量 (Focus)", desc: "优先指派最强推理 Agent 与 Opus/Flagship 模型" },
  { id: "manual", label: "手动锁死", desc: "固定由下方指定默认 Agent 处理（仍受条件规则覆盖）" },
];

export function RoutingTab({
  repos,
  agents,
  models,
  rules,
  templates,
  stages,
  routingConfig,
  onReload,
  onMessage,
  onError,
  openNewRuleModal,
  onCloseNewRuleModal,
}: RoutingTabProps) {
  const [editRule, setEditRule] = useState<RoutingRule | null>(null);
  const [simRepoId, setSimRepoId] = useState<string>(repos[0]?.id || "");
  const [simTitle, setSimTitle] = useState<string>("fix: race condition in worker pool shutdown");
  const [simLabels, setSimLabels] = useState<string>("bug, high-priority");
  const [simResult, setSimResult] = useState<RoutingDecision | null>(null);
  const [simRunning, setSimRunning] = useState<boolean>(false);

  const saveConfig = async (next: RoutingConfig) => {
    onError("");
    try {
      await AgentService.SaveRoutingConfig(next);
      await onReload();
    } catch (e) {
      onError(String(e));
    }
  };

  const updateStageBinding = async (stageKey: string, patch: Partial<StageRouteBinding>) => {
    const current = routingConfig.stageBindings?.[stageKey] || { agentId: "", modelRef: "", tier: "" };
    const nextStage: StageRouteBinding = { ...current, ...patch };
    const nextCfg: RoutingConfig = {
      ...routingConfig,
      stageBindings: {
        ...(routingConfig.stageBindings || {}),
        [stageKey]: nextStage,
      },
    };
    await saveConfig(nextCfg);
  };

  const activeRuleModal = editRule ?? (openNewRuleModal
    ? {
        id: "",
        priority: 50,
        name: "",
        repoId: "*",
        labelMatch: "",
        titleRegex: "",
        complexityHint: "auto",
        templateId: "full_feature",
        agentId: agents[0]?.id || "claude",
        modelRef: "",
        stageOverridesJson: "{}",
        enabled: true,
      }
    : null);

  const [draftRule, setDraftRule] = useState<RoutingRule | null>(null);
  const effectiveRule = draftRule && (draftRule.id === activeRuleModal?.id) ? draftRule : activeRuleModal;

  const closeRuleModal = () => {
    setEditRule(null);
    setDraftRule(null);
    onCloseNewRuleModal();
  };

  const saveRule = async () => {
    if (!effectiveRule) return;
    onError("");
    try {
      await AgentService.SaveRoutingRule(effectiveRule);
      closeRuleModal();
      await onReload();
      onMessage(`路由规则「${effectiveRule.name}」已保存。`);
    } catch (e) {
      onError(String(e));
    }
  };

  const runSimulator = async () => {
    setSimRunning(true);
    onError("");
    try {
      const labels = simLabels
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      const res = await AgentService.SimulateRoute({
        repoId: simRepoId,
        title: simTitle,
        body: "",
        labels,
      });
      setSimResult(res);
    } catch (e) {
      onError(String(e));
    } finally {
      setSimRunning(false);
    }
  };

  const agentName = (id: string) => agents.find((a) => a.id === id)?.name || id || "跟随主路由";

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {/* 1. Strategy & AI Router Card */}
      <div className="dragon-card" style={{ padding: 16, display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
          <div>
            <div style={{ fontWeight: 600, fontSize: 15 }}>全局路由策略与裁判模型</div>
            <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
              {STRATEGIES.find((s) => s.id === routingConfig.strategy)?.desc}
            </div>
          </div>

          <div className="dragon-segmented-tabs">
            {STRATEGIES.map((s) => (
              <button
                key={s.id}
                type="button"
                className={`dragon-seg-tab ${routingConfig.strategy === s.id ? "active" : ""}`}
                onClick={() => void saveConfig({ ...routingConfig, strategy: s.id })}
              >
                {s.label}
              </button>
            ))}
          </div>
        </div>

        <div
          className="dragon-subtle-box"
          style={{ padding: "10px 14px", display: "flex", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: 12 }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Sparkle16Regular style={{ color: "var(--dragon-accent)" }} />
            <span style={{ fontSize: 13, fontWeight: 500 }}>AI 辅助路由裁判模型</span>
            <ModelPickerPill
              valueRef={routingConfig.routerModelRef || ""}
              options={models}
              defaultLabel="启发式 / 全局轻量 LLM"
              onChange={(_, __, fullRef) => void saveConfig({ ...routingConfig, routerModelRef: fullRef })}
            />
          </div>

          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <span style={{ fontSize: 13, fontWeight: 500 }}>兜底 / 锁定默认 Agent</span>
            <Dropdown
              size="small"
              value={routingConfig.fallbackAgentId ? agentName(routingConfig.fallbackAgentId) : "自动择优可用 CLI"}
              selectedOptions={[routingConfig.fallbackAgentId || ""]}
              onOptionSelect={(_, d) =>
                void saveConfig({ ...routingConfig, fallbackAgentId: d.optionValue || "" })
              }
              style={{ minWidth: 170 }}
            >
              <Option value="">自动择优可用 CLI</Option>
              {agents.map((a) => (
                <Option key={a.id} value={a.id}>
                  {a.name}
                </Option>
              ))}
            </Dropdown>
          </div>
        </div>
      </div>

      {/* 2. 7-Stage SDLC Matrix (Magpie list style) */}
      <div>
        <div style={{ fontWeight: 600, fontSize: 14.5, marginBottom: 8 }}>
          7 阶段 SDLC 精细路由矩阵（按阶段独立混用 Agent 与模型 / Tier）
        </div>
        <div className="dragon-list-card">
          {stages.map((st) => {
            const binding = routingConfig.stageBindings?.[st.key] || { agentId: "", modelRef: "", tier: "" };
            const effectiveModelValue = binding.modelRef || binding.tier || "";
            return (
              <div key={st.key} className="dragon-list-row">
                <div style={{ display: "flex", alignItems: "center", gap: 12, minWidth: 0, flex: 1 }}>
                  <Badge appearance="tint" color="brand" size="medium">
                    {st.order}
                  </Badge>
                  <div style={{ minWidth: 0 }}>
                    <div style={{ fontWeight: 600, fontSize: 13.5 }}>{st.name}</div>
                    <div style={{ fontSize: 12, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
                      {st.description}
                    </div>
                  </div>
                </div>

                <div style={{ display: "flex", alignItems: "center", gap: 10, flexShrink: 0 }}>
                  <Dropdown
                    size="small"
                    value={binding.agentId ? agentName(binding.agentId) : "跟随主路由 Agent"}
                    selectedOptions={[binding.agentId || ""]}
                    onOptionSelect={(_, d) =>
                      void updateStageBinding(st.key, { agentId: d.optionValue || "" })
                    }
                    style={{ minWidth: 165 }}
                  >
                    <Option value="">跟随主路由 Agent</Option>
                    {agents.map((a) => (
                      <Option key={a.id} value={a.id}>
                        {a.name}
                      </Option>
                    ))}
                  </Dropdown>

                  <ModelPickerPill
                    valueRef={effectiveModelValue}
                    options={models}
                    includeTiers={true}
                    defaultLabel="跟随 Agent 默认"
                    onChange={(_, modelId, fullRef) => {
                      if (fullRef === "opus" || fullRef === "sonnet" || fullRef === "haiku") {
                        void updateStageBinding(st.key, { tier: fullRef, modelRef: "" });
                      } else {
                        void updateStageBinding(st.key, { modelRef: fullRef || modelId, tier: "" });
                      }
                    }}
                  />
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* 3. Conditional Rules */}
      <div>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
          <span style={{ fontWeight: 600, fontSize: 14.5 }}>条件匹配路由规则 ({rules.length})</span>
          <Button
            size="small"
            appearance="outline"
            icon={<Add16Regular />}
            onClick={() =>
              setEditRule({
                id: "",
                priority: 50,
                name: "",
                repoId: "*",
                labelMatch: "",
                titleRegex: "",
                complexityHint: "auto",
                templateId: "full_feature",
                agentId: agents[0]?.id || "claude",
                modelRef: "",
                stageOverridesJson: "{}",
                enabled: true,
              })
            }
          >
            新增条件规则
          </Button>
        </div>

        <div className="dragon-list-card">
          {rules.length === 0 ? (
            <div style={{ padding: 20, textAlign: "center", fontSize: 13, color: "var(--dragon-text-secondary)" }}>
              暂无条件规则；点击右上角「新增条件规则」可按仓库、Label 或标题正则指定专属模板、Agent 与模型。
            </div>
          ) : (
            rules.map((rl) => (
              <div key={rl.id} className="dragon-list-row">
                <div style={{ display: "flex", alignItems: "center", gap: 12, minWidth: 0, flex: 1 }}>
                  <BrandIcon kind={rl.agentId || "claude"} size={28} />
                  <div style={{ minWidth: 0 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                      <Badge size="small" appearance="filled" color="brand">
                        P{rl.priority}
                      </Badge>
                      <span style={{ fontWeight: 600, fontSize: 13.5 }}>{rl.name}</span>
                      <Badge size="small" appearance="outline">
                        {rl.templateId}
                      </Badge>
                      {rl.modelRef && (
                        <Badge size="small" appearance="tint" color="informative">
                          {rl.modelRef}
                        </Badge>
                      )}
                    </div>
                    <div style={{ fontSize: 12, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
                      Repo: <code>{rl.repoId || "*"}</code> · Label: <code>{rl.labelMatch || "*"}</code> · Regex:{" "}
                      <code>{rl.titleRegex || "*"}</code> → <b>{agentName(rl.agentId)}</b>
                    </div>
                  </div>
                </div>

                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <Button size="small" appearance="subtle" onClick={() => setEditRule({ ...rl })}>
                    编辑
                  </Button>
                  <Button
                    size="small"
                    appearance="subtle"
                    icon={<Delete16Regular />}
                    onClick={() => void AgentService.DeleteRoutingRule(rl.id).then(onReload)}
                  />
                  <Switch
                    checked={rl.enabled}
                    onChange={(_, d) => void AgentService.SaveRoutingRule({ ...rl, enabled: d.checked }).then(onReload)}
                  />
                </div>
              </div>
            ))
          )}
        </div>
      </div>

      {/* 4. Live Route Simulator */}
      <div className="dragon-card" style={{ padding: 16, display: "flex", flexDirection: "column", gap: 10 }}>
        <div style={{ fontWeight: 600, fontSize: 14.5 }}>实时路由模拟器（Dry-Run 预览 7 阶段指派结果）</div>
        <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
          <Dropdown
            size="small"
            value={repos.find((r) => r.id === simRepoId)?.fullName || "默认演示仓库"}
            selectedOptions={simRepoId ? [simRepoId] : []}
            onOptionSelect={(_, d) => d.optionValue && setSimRepoId(d.optionValue)}
            style={{ minWidth: 170 }}
          >
            {repos.length === 0 && <Option value="">默认演示仓库</Option>}
            {repos.map((r) => (
              <Option key={r.id} value={r.id}>
                {r.fullName}
              </Option>
            ))}
          </Dropdown>
          <Input
            placeholder="输入模拟 Issue 标题..."
            value={simTitle}
            onChange={(_, d) => setSimTitle(d.value)}
            style={{ flex: 2, minWidth: 220 }}
          />
          <Input
            placeholder="Labels (逗号分隔，如 bug, urgent)"
            value={simLabels}
            onChange={(_, d) => setSimLabels(d.value)}
            style={{ flex: 1, minWidth: 160 }}
          />
          <Button
            appearance="primary"
            disabled={simRunning}
            icon={simRunning ? <Spinner size="extra-tiny" /> : <Play16Regular />}
            onClick={() => void runSimulator()}
          >
            模拟路由
          </Button>
        </div>

        {simResult && (
          <div className="dragon-subtle-box" style={{ padding: 12, display: "flex", flexDirection: "column", gap: 8 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", fontSize: 13 }}>
              <Badge appearance="filled" color="brand">
                模板: {simResult.templateId}
              </Badge>
              <Badge appearance="tint" color="success">
                主 Agent: {simResult.primaryAgent}
              </Badge>
              {simResult.primaryModel && (
                <Badge appearance="tint" color="informative">
                  主模型: {simResult.primaryModel}
                </Badge>
              )}
              <span style={{ color: "var(--dragon-text-secondary)" }}>{simResult.reason}</span>
            </div>

            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))", gap: 6 }}>
              {stages.map((st) => (
                <div key={st.key} className="dragon-subtle-box" style={{ padding: "6px 8px", fontSize: 11.5 }}>
                  <div style={{ fontWeight: 600 }}>
                    {st.order}. {st.name}
                  </div>
                  <div style={{ color: "var(--dragon-text-secondary)", marginTop: 2 }}>
                    {simResult.stageAgents?.[st.key] || simResult.primaryAgent} ·{" "}
                    <code>{simResult.stageModels?.[st.key] || "default"}</code>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Add/Edit Rule Modal */}
      <Dialog open={effectiveRule !== null} onOpenChange={(_, d) => !d.open && closeRuleModal()}>
        <DialogSurface style={{ maxWidth: 540 }}>
          <DialogBody>
            <DialogTitle>{effectiveRule?.id ? "编辑路由规则" : "新增条件路由规则"}</DialogTitle>
            {effectiveRule && (
              <DialogContent style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 8 }}>
                <Input
                  placeholder="规则名称 (如 Critical Bug 走 Codex + DeepSeek)"
                  value={effectiveRule.name}
                  onChange={(_, d) => setDraftRule({ ...effectiveRule, name: d.value })}
                />
                <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
                  <Input
                    type="number"
                    placeholder="优先级 (越小越先)"
                    value={String(effectiveRule.priority)}
                    onChange={(_, d) => setDraftRule({ ...effectiveRule, priority: Number(d.value) || 100 })}
                  />
                  <Dropdown
                    value={effectiveRule.repoId || "*"}
                    selectedOptions={[effectiveRule.repoId || "*"]}
                    onOptionSelect={(_, d) => d.optionValue && setDraftRule({ ...effectiveRule, repoId: d.optionValue })}
                  >
                    <Option value="*">全部仓库 (*)</Option>
                    {repos.map((r) => (
                      <Option key={r.id} value={r.id}>
                        {r.fullName}
                      </Option>
                    ))}
                  </Dropdown>
                </div>
                <Input
                  placeholder="匹配 Label (精确匹配，可留空)"
                  value={effectiveRule.labelMatch}
                  onChange={(_, d) => setDraftRule({ ...effectiveRule, labelMatch: d.value })}
                />
                <Input
                  placeholder="匹配标题正则 (可留空)"
                  value={effectiveRule.titleRegex}
                  onChange={(_, d) => setDraftRule({ ...effectiveRule, titleRegex: d.value })}
                />
                <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
                  <Dropdown
                    value={effectiveRule.templateId}
                    selectedOptions={[effectiveRule.templateId]}
                    onOptionSelect={(_, d) => d.optionValue && setDraftRule({ ...effectiveRule, templateId: d.optionValue })}
                  >
                    {templates.map((t) => (
                      <Option key={t.id} value={t.id}>
                        {t.name}
                      </Option>
                    ))}
                  </Dropdown>
                  <Dropdown
                    value={effectiveRule.agentId}
                    selectedOptions={[effectiveRule.agentId]}
                    onOptionSelect={(_, d) => d.optionValue && setDraftRule({ ...effectiveRule, agentId: d.optionValue })}
                  >
                    {agents.map((a) => (
                      <Option key={a.id} value={a.id}>
                        {a.name}
                      </Option>
                    ))}
                  </Dropdown>
                </div>

                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
                  <span style={{ fontSize: 12.5, fontWeight: 500 }}>覆盖模型 / Tier (可选)</span>
                  <ModelPickerPill
                    valueRef={effectiveRule.modelRef || ""}
                    options={models}
                    includeTiers={true}
                    defaultLabel="使用 Agent 默认模型"
                    onChange={(_, __, fullRef) => setDraftRule({ ...effectiveRule, modelRef: fullRef })}
                  />
                </div>

                <Input
                  placeholder='阶段 Agent 覆盖 JSON (如 {"ai_review":"gemini"})'
                  value={effectiveRule.stageOverridesJson}
                  onChange={(_, d) => setDraftRule({ ...effectiveRule, stageOverridesJson: d.value })}
                />
                <Switch
                  label="启用此规则"
                  checked={effectiveRule.enabled}
                  onChange={(_, d) => setDraftRule({ ...effectiveRule, enabled: d.checked })}
                />
              </DialogContent>
            )}
            <DialogActions>
              <Button appearance="secondary" onClick={closeRuleModal}>
                取消
              </Button>
              <Button appearance="primary" disabled={!effectiveRule?.name.trim()} onClick={() => void saveRule()}>
                保存规则
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </div>
  );
}
