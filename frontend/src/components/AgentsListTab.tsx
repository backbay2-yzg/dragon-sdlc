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
  Input,
  Spinner,
  Switch,
  Tooltip,
} from "@fluentui/react-components";
import {
  Add16Regular,
  ChevronDown16Regular,
  ChevronRight12Regular,
  ChevronUp16Regular,
  Copy16Regular,
  Delete16Regular,
  Play16Regular,
} from "@fluentui/react-icons";
import { AgentService } from "../../bindings/dragon/internal/appservice/index.js";
import type {
  AgentProfile,
  ModelCatalogOption,
  TestAgentResult,
  WorkspaceProfile,
} from "../types/dragon.js";
import { BrandIcon } from "../components/BrandIcon.js";
import { ModelPickerPill } from "../components/ModelPickerPill.js";

interface AgentsListTabProps {
  agents: AgentProfile[];
  models: ModelCatalogOption[];
  profiles: WorkspaceProfile[];
  onReload: () => Promise<void>;
  onMessage: (msg: string) => void;
  onError: (err: string) => void;
  openNewAgentModal: boolean;
  onCloseNewAgentModal: () => void;
}

interface AgentGuide {
  installCmd: string;
  loginCmd: string;
  note: string;
  stagedMode: string;
  bypassMode: string;
}

const ACCENT_PALETTE = ["#10B981", "#0F6CBD", "#7C3AED", "#F59E0B", "#EC4899"];

const AGENT_GUIDES: Record<string, AgentGuide> = {
  codex: {
    installCmd: "npm install -g @openai/codex",
    loginCmd: "codex login",
    note: "支持直接复用 Microsoft Store 版 Codex 桌面端内嵌 CLI 与 ~/.codex/auth.json 原生登录。",
    stagedMode: "workspace-write",
    bypassMode: "bypass",
  },
  qoder: {
    installCmd: "npm install -g @qoder-ai/qodercli",
    loginCmd: "qodercli login",
    note: "原生支持 -p -o stream-json 无头调用，需先在终端完成 qodercli login 账号认证。",
    stagedMode: "accept_edits",
    bypassMode: "bypass",
  },
  workbuddy: {
    installCmd: "安装 WorkBuddy 桌面客户端（并确保系统已安装 Node.js >= 18）",
    loginCmd: "node \"D:\\Programs\\WorkBuddy\\resources\\app.asar.unpacked\\cli\\dist\\codebuddy-lite-wb.mjs\" login",
    note: "自动发现 WorkBuddy 桌面端内嵌 CodeBuddy CLI；若未登录可选择已配 Key 的 Provider 模型或完成客户端登录。",
    stagedMode: "acceptEdits",
    bypassMode: "bypass",
  },
  codebuddy: {
    installCmd: "npm install -g @tencent-ai/codebuddy-code",
    loginCmd: "codebuddy login",
    note: "腾讯 CodeBuddy Code 独立无头 CLI（支持 -p --output-format stream-json）。CodeBuddy CN 桌面 IDE 仅包含图形界面。",
    stagedMode: "acceptEdits",
    bypassMode: "bypass",
  },
  agy: {
    installCmd: "irm https://antigravity.google/cli/install.ps1 | iex",
    loginCmd: "agy",
    note: "安装独立 Antigravity CLI (agy) 并运行一次 agy 完成 Google 账号登录后，即可在无头流水线调用。",
    stagedMode: "auto",
    bypassMode: "bypass",
  },
  claude: {
    installCmd: "npm install -g @anthropic-ai/claude-code",
    loginCmd: "claude",
    note: "支持 Opus / Sonnet / Haiku 三档模型映射与原生 --append-system-prompt。",
    stagedMode: "acceptEdits",
    bypassMode: "bypass",
  },
  gemini: {
    installCmd: "npm install -g @google/gemini-cli",
    loginCmd: "gemini",
    note: "可通过 Google 账号或 Providers 中配置的 Gemini API Key 运行。",
    stagedMode: "auto",
    bypassMode: "bypass",
  },
  opencode: {
    installCmd: "npm install -g opencode-ai",
    loginCmd: "opencode auth login",
    note: "支持多模型与本地/云端 Provider 切换。",
    stagedMode: "auto",
    bypassMode: "bypass",
  },
  copilot: {
    installCmd: "winget install GitHub.Copilot",
    loginCmd: "gh auth login",
    note: "复用 GitHub CLI / Copilot 原生登录态。",
    stagedMode: "auto",
    bypassMode: "bypass",
  },
  kimi: {
    installCmd: "uv tool install kimi-cli",
    loginCmd: "kimi",
    note: "支持 Moonshot / Kimi 模型配置。",
    stagedMode: "auto",
    bypassMode: "bypass",
  },
  aider: {
    installCmd: "pip install aider-chat",
    loginCmd: "在 Providers 中配置 DeepSeek / OpenAI / Anthropic API Key",
    note: "Git 感知结对编程 CLI，适合成本优先与轻量重构。",
    stagedMode: "auto",
    bypassMode: "bypass",
  },
};

const BYPASS_MODES = new Set([
  "bypass",
  "bypasspermissions",
  "bypass_permissions",
  "danger-full-access",
  "dangerously-skip-permissions",
  "dangerously-bypass-approvals-and-sandbox",
  "full-bypass",
]);

function isBypassMode(mode: string): boolean {
  return BYPASS_MODES.has((mode || "").trim().toLowerCase());
}

function describeDetectedSource(ag: AgentProfile): string {
  if (!ag.detected) return "未在 PATH 或本地应用目录检测到";
  const p = (ag.detectedPath || "").toLowerCase();
  if (p.includes("windowsapps")) return "Connected · WindowsApps 内嵌 CLI";
  if (p.includes("workbuddy")) return "Connected · WorkBuddy 内嵌 CLI";
  if (p.includes(".qoder")) return "Connected · Qoder 本地 CLI";
  return "Connected";
}

function isAuthErrorResult(res?: TestAgentResult): boolean {
  if (!res || res.ok || !res.detected) return false;
  const hay = `${res.error || ""} ${res.hint || ""} ${res.output || ""}`.toLowerCase();
  return (
    hay.includes("login") ||
    hay.includes("auth") ||
    hay.includes("401") ||
    hay.includes("api key") ||
    hay.includes("登录")
  );
}

export function AgentsListTab({
  agents,
  models,
  profiles,
  onReload,
  onMessage,
  onError,
  openNewAgentModal,
  onCloseNewAgentModal,
}: AgentsListTabProps) {
  const [showMore, setShowMore] = useState<boolean>(false);
  const [tiersAgent, setTiersAgent] = useState<AgentProfile | null>(null);
  const [tiersMap, setTiersMap] = useState<Record<string, string>>({});
  const [inspectAgent, setInspectAgent] = useState<AgentProfile | null>(null);
  const [testingId, setTestingId] = useState<string>("");
  const [testResults, setTestResults] = useState<Record<string, TestAgentResult>>({});
  const [copiedTag, setCopiedTag] = useState<string>("");
  const [saveProfileOpen, setSaveProfileOpen] = useState<boolean>(false);
  const [newProfName, setNewProfName] = useState<string>("");
  const [newProfDesc, setNewProfDesc] = useState<string>("");
  const [newProfColor, setNewProfColor] = useState<string>("#0F6CBD");
  const [draftCustomAgent, setDraftCustomAgent] = useState<AgentProfile>({
    id: "",
    name: "",
    kind: "custom",
    command: "",
    argsTemplate: "",
    providerId: "",
    model: "",
    subtitle: "starts on the model picked here",
    tiersJson: "{}",
    supportsTiers: false,
    primaryGroup: false,
    sortOrder: 200,
    permissionMode: "auto",
    extraEnvJson: "{}",
    enabled: true,
    detected: false,
    detectedPath: "",
    detectedVersion: "",
    capabilitiesJson: "{}",
  });

  const detectedAgents = agents.filter((a) => a.detected);
  const undetectedAgents = agents.filter((a) => !a.detected);
  const primaryAgents = detectedAgents.length > 0 ? detectedAgents : agents.filter((a) => a.primaryGroup);
  const secondaryAgents = detectedAgents.length > 0 ? undetectedAgents : agents.filter((a) => !a.primaryGroup);

  const agentModelRef = (ag: AgentProfile) => {
    if (!ag.model) return "";
    if (ag.providerId && !ag.model.includes("/")) {
      return `${ag.providerId}/${ag.model}`;
    }
    return ag.model;
  };

  const handleModelPick = async (ag: AgentProfile, providerId: string, modelId: string) => {
    onError("");
    try {
      await AgentService.SetAgentModel(ag.id, providerId, modelId);
      await onReload();
    } catch (e) {
      onError(String(e));
    }
  };

  const handleToggle = async (ag: AgentProfile, enabled: boolean) => {
    onError("");
    try {
      await AgentService.SetAgentEnabled(ag.id, enabled);
      await onReload();
    } catch (e) {
      onError(String(e));
    }
  };

  const handleTestAgent = async (ag: AgentProfile) => {
    setTestingId(ag.id);
    onError("");
    try {
      const res = await AgentService.TestAgent(ag.id);
      if (res) {
        setTestResults((prev) => ({ ...prev, [ag.id]: res }));
        if (res.ok) {
          onMessage(`${ag.name} 测试调用通过（${Math.max(0.1, res.durationMs / 1000).toFixed(1)}s）：${res.output || "OK"}`);
        } else {
          const hintPart = res.hint ? ` ｜ 建议：${res.hint}` : "";
          onError(`${ag.name} 测试失败：${res.error || "未知错误"}${hintPart}`);
        }
      }
      await onReload();
    } catch (e) {
      onError(String(e));
    } finally {
      setTestingId("");
    }
  };

  const copyText = async (tag: string, text: string) => {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      setCopiedTag(tag);
      setTimeout(() => setCopiedTag((cur) => (cur === tag ? "" : cur)), 1800);
    } catch {
      onError("复制到剪贴板失败，请手动选中命令复制。");
    }
  };

  const openTiers = (ag: AgentProfile) => {
    let parsed: Record<string, string> = {};
    try {
      parsed = JSON.parse(ag.tiersJson || "{}");
    } catch {
      parsed = {};
    }
    setTiersMap({
      opus: parsed.opus || "",
      sonnet: parsed.sonnet || "",
      haiku: parsed.haiku || "",
    });
    setTiersAgent(ag);
  };

  const saveTiers = async () => {
    if (!tiersAgent) return;
    onError("");
    try {
      await AgentService.SaveAgentTiers(tiersAgent.id, JSON.stringify(tiersMap));
      setTiersAgent(null);
      await onReload();
      onMessage(`已更新 ${tiersAgent.name} 的 Opus / Sonnet / Haiku 三档模型映射。`);
    } catch (e) {
      onError(String(e));
    }
  };

  const saveAgentDetails = async (target: AgentProfile, closeModal: () => void) => {
    onError("");
    try {
      await AgentService.SaveAgent(target);
      closeModal();
      await onReload();
      onMessage(`Agent「${target.name}」配置已保存并重新检测。`);
    } catch (e) {
      onError(String(e));
    }
  };

  const applyProfile = async (wp: WorkspaceProfile) => {
    onError("");
    try {
      await AgentService.ApplyWorkspaceProfile(wp.id);
      await onReload();
      onMessage(`已切换至 Profile「${wp.name}」（同步更新了 Agent 模型映射与路由策略）。`);
    } catch (e) {
      onError(String(e));
    }
  };

  const saveCurrentProfile = async () => {
    if (!newProfName.trim()) return;
    onError("");
    try {
      await AgentService.SaveCurrentAsProfile(newProfName.trim(), newProfDesc.trim(), newProfColor);
      setSaveProfileOpen(false);
      setNewProfName("");
      setNewProfDesc("");
      await onReload();
      onMessage("已将当前全部 Agent、模型与阶段路由状态保存为新 Profile。");
    } catch (e) {
      onError(String(e));
    }
  };

  const renderRow = (ag: AgentProfile) => {
    const statusLabel = describeDetectedSource(ag);
    const guide = AGENT_GUIDES[ag.kind];
    const tooltipText = ag.detected
      ? `路径: ${ag.detectedPath}\n版本: ${ag.detectedVersion}`
      : guide
      ? `未检测到可执行文件。\n安装指引: ${guide.installCmd}\n登录方式: ${guide.loginCmd}`
      : `请确认命令 "${ag.command}" 已安装并加入系统 PATH（未安装前仍可预先配置模型）`;

    const testRes = testResults[ag.id];
    const isTesting = testingId === ag.id;

    return (
      <div key={ag.id} className="dragon-list-row">
        <div style={{ display: "flex", alignItems: "center", gap: 12, minWidth: 0, flex: 1 }}>
          <BrandIcon kind={ag.kind} size={32} />
          <div style={{ minWidth: 0 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <span style={{ fontWeight: 600, fontSize: 13.5, lineHeight: 1.3 }}>{ag.name}</span>
              {testRes && (
                <Badge
                  size="small"
                  appearance="tint"
                  color={testRes.ok ? "success" : isAuthErrorResult(testRes) ? "warning" : "danger"}
                >
                  {testRes.ok
                    ? `通过 · ${(Math.max(100, testRes.durationMs) / 1000).toFixed(1)}s`
                    : isAuthErrorResult(testRes)
                    ? "需登录"
                    : "测试失败"}
                </Badge>
              )}
            </div>
            <div
              title={tooltipText}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 6,
                fontSize: 12,
                color: "var(--dragon-text-secondary)",
                marginTop: 2,
              }}
            >
              <span className={`dragon-status-dot ${ag.detected ? "online" : "offline"}`} />
              <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {statusLabel} · {ag.subtitle || ag.command}
              </span>
            </div>
          </div>
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 10, flexShrink: 0 }}>
          {ag.detected ? (
            <Tooltip content="发起轻量只读测试调用，校验 CLI 登录态与可执行性" relationship="label">
              <Button
                size="small"
                appearance="subtle"
                disabled={isTesting || testingId !== ""}
                icon={isTesting ? <Spinner size="extra-tiny" /> : <Play16Regular />}
                onClick={() => void handleTestAgent(ag)}
              >
                {isTesting ? "测试中" : "测试调用"}
              </Button>
            </Tooltip>
          ) : (
            <Button size="small" appearance="subtle" onClick={() => setInspectAgent({ ...ag })}>
              安装指引
            </Button>
          )}

          {ag.supportsTiers ? (
            <button type="button" className="dragon-row-link" onClick={() => openTiers(ag)}>
              <span>Tiers</span>
              <ChevronRight12Regular />
            </button>
          ) : (
            <button type="button" className="dragon-row-link" onClick={() => setInspectAgent({ ...ag })}>
              <span>详情 / 权限</span>
              <ChevronRight12Regular />
            </button>
          )}

          <ModelPickerPill
            valueRef={agentModelRef(ag)}
            options={models}
            onChange={(provId, modId) => void handleModelPick(ag, provId, modId)}
          />

          <Switch
            checked={ag.enabled}
            onChange={(_, d) => void handleToggle(ag, d.checked)}
            aria-label={`启用 ${ag.name}`}
          />
        </div>
      </div>
    );
  };

  const inspectGuide = inspectAgent ? AGENT_GUIDES[inspectAgent.kind] : undefined;
  const inspectTestRes = inspectAgent ? testResults[inspectAgent.id] : undefined;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)" }}>
        已检测到的本地 Coding Agent 自动置顶显示（含桌面端内嵌 CLI）。保持「CLI 默认模型」时优先复用各工具原生登录态；仅当你显式选定 Provider 模型且配置了 Key 时，Dragon 才会注入环境变量。
      </div>

      {/* Unified Agent List Card */}
      <div className="dragon-list-card">
        {primaryAgents.map(renderRow)}
        {showMore && secondaryAgents.map(renderRow)}

        {secondaryAgents.length > 0 && (
          <button
            type="button"
            className="dragon-show-more-row"
            onClick={() => setShowMore((v) => !v)}
          >
            <span>
              {showMore
                ? "收起未安装 / 更多 Agent"
                : `Show ${secondaryAgents.length} more (未安装 / 待配置 ${secondaryAgents.length} 个)`}
            </span>
            {showMore ? <ChevronUp16Regular /> : <ChevronDown16Regular />}
          </button>
        )}
      </div>

      {/* Bottom PROFILES Bar */}
      <div className="dragon-profiles-bar">
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <span
            style={{
              fontSize: 11,
              fontWeight: 700,
              letterSpacing: "0.06em",
              color: "var(--dragon-text-secondary)",
              marginRight: 4,
            }}
          >
            PROFILES
          </span>
          {profiles.map((wp) => (
            <button
              key={wp.id}
              type="button"
              title={wp.description || wp.name}
              className={`dragon-profile-pill ${wp.isActive ? "active" : ""}`}
              onClick={() => void applyProfile(wp)}
            >
              <span>{wp.name}</span>
              <span
                style={{
                  width: 7,
                  height: 7,
                  borderRadius: "50%",
                  background: wp.accentColor || "#0F6CBD",
                }}
              />
            </button>
          ))}
        </div>

        <Button
          size="small"
          appearance="subtle"
          icon={<Add16Regular />}
          onClick={() => setSaveProfileOpen(true)}
        >
          Save current
        </Button>
      </div>

      {/* Claude Tiers Mapping Modal */}
      <Dialog open={tiersAgent !== null} onOpenChange={(_, d) => !d.open && setTiersAgent(null)}>
        <DialogSurface style={{ maxWidth: 500 }}>
          <DialogBody>
            <DialogTitle>配置 {tiersAgent?.name} 模型档位 (Tiers)</DialogTitle>
            <DialogContent style={{ display: "flex", flexDirection: "column", gap: 12, marginTop: 10 }}>
              <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)" }}>
                子进程执行时将通过 <code>ANTHROPIC_DEFAULT_*_MODEL</code> 自动映射各档位，在 CLI 内使用 <code>/model</code> 或阶段路由选择 Tier 时生效。
              </div>

              {(["opus", "sonnet", "haiku"] as const).map((tierKey) => (
                <div
                  key={tierKey}
                  className="dragon-subtle-box"
                  style={{ padding: "10px 12px", display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12 }}
                >
                  <div>
                    <div style={{ fontWeight: 600, fontSize: 13.5, textTransform: "capitalize" }}>{tierKey}</div>
                    <div style={{ fontSize: 11.5, color: "var(--dragon-text-secondary)" }}>
                      {tierKey === "opus"
                        ? "深度架构设计 / 复杂审查"
                        : tierKey === "sonnet"
                        ? "主力日常开发与实现"
                        : "快速分类与轻量校验"}
                    </div>
                  </div>
                  <ModelPickerPill
                    valueRef={tiersMap[tierKey] || ""}
                    options={models}
                    defaultLabel="保持默认"
                    onChange={(_, __, fullRef) => setTiersMap((prev) => ({ ...prev, [tierKey]: fullRef }))}
                  />
                </div>
              ))}
            </DialogContent>
            <DialogActions>
              <Button appearance="secondary" onClick={() => setTiersAgent(null)}>
                取消
              </Button>
              <Button appearance="primary" onClick={() => void saveTiers()}>
                保存 Tiers
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>

      {/* Inspect / Edit Agent CLI Modal */}
      <Dialog open={inspectAgent !== null} onOpenChange={(_, d) => !d.open && setInspectAgent(null)}>
        <DialogSurface style={{ maxWidth: 540, width: "calc(100vw - 48px)" }}>
          <DialogBody>
            <DialogTitle>配置 Agent CLI：{inspectAgent?.name}</DialogTitle>
            {inspectAgent && (
              <DialogContent style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 8 }}>
                <div className="dragon-subtle-box" style={{ padding: 10, fontSize: 12.5 }}>
                  <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
                    <div>
                      状态:{" "}
                      <Badge size="small" appearance="tint" color={inspectAgent.detected ? "success" : "subtle"}>
                        {inspectAgent.detected ? describeDetectedSource(inspectAgent) : "未检测到"}
                      </Badge>{" "}
                      {inspectAgent.detectedVersion}
                    </div>
                    {inspectAgent.detected && (
                      <Button
                        size="small"
                        appearance="outline"
                        disabled={testingId !== ""}
                        icon={testingId === inspectAgent.id ? <Spinner size="extra-tiny" /> : <Play16Regular />}
                        onClick={() => void handleTestAgent(inspectAgent)}
                      >
                        {testingId === inspectAgent.id ? "测试中..." : "测试调用"}
                      </Button>
                    )}
                  </div>
                  <div style={{ marginTop: 4, color: "var(--dragon-text-secondary)", wordBreak: "break-all" }}>
                    路径: {inspectAgent.detectedPath || "—"}
                  </div>
                  {inspectGuide?.note && (
                    <div style={{ marginTop: 4, color: "var(--dragon-text-secondary)" }}>
                      说明: {inspectGuide.note}
                    </div>
                  )}
                </div>

                {inspectTestRes && (
                  <div
                    className="dragon-subtle-box"
                    style={{
                      padding: 10,
                      fontSize: 12,
                      borderColor: inspectTestRes.ok ? "rgba(16, 185, 129, 0.35)" : "rgba(239, 68, 68, 0.35)",
                    }}
                  >
                    <div style={{ display: "flex", alignItems: "center", gap: 6, fontWeight: 600 }}>
                      <Badge
                        size="small"
                        appearance="tint"
                        color={inspectTestRes.ok ? "success" : isAuthErrorResult(inspectTestRes) ? "warning" : "danger"}
                      >
                        {inspectTestRes.ok ? "测试通过" : isAuthErrorResult(inspectTestRes) ? "需要登录" : "执行失败"}
                      </Badge>
                      <span>耗时 {(Math.max(100, inspectTestRes.durationMs) / 1000).toFixed(1)}s</span>
                    </div>
                    {inspectTestRes.output && (
                      <div style={{ marginTop: 4, color: "var(--dragon-text-secondary)", whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
                        输出: {inspectTestRes.output}
                      </div>
                    )}
                    {inspectTestRes.error && (
                      <div style={{ marginTop: 4, color: "#DC2626", wordBreak: "break-word" }}>
                        错误: {inspectTestRes.error}
                      </div>
                    )}
                    {inspectTestRes.hint && (
                      <div style={{ marginTop: 4, color: "var(--dragon-text-primary)" }}>
                        建议: {inspectTestRes.hint}
                      </div>
                    )}
                  </div>
                )}

                {inspectGuide && (
                  <div className="dragon-subtle-box" style={{ padding: 10, display: "flex", flexDirection: "column", gap: 6, fontSize: 12 }}>
                    <div style={{ fontWeight: 600 }}>安装与登录指引（可一键复制）</div>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
                      <code style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                        安装: {inspectGuide.installCmd}
                      </code>
                      <Button
                        size="small"
                        appearance="subtle"
                        icon={<Copy16Regular />}
                        onClick={() => void copyText("install", inspectGuide.installCmd)}
                      >
                        {copiedTag === "install" ? "已复制" : "复制"}
                      </Button>
                    </div>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
                      <code style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                        登录: {inspectGuide.loginCmd}
                      </code>
                      <Button
                        size="small"
                        appearance="subtle"
                        icon={<Copy16Regular />}
                        onClick={() => void copyText("login", inspectGuide.loginCmd)}
                      >
                        {copiedTag === "login" ? "已复制" : "复制"}
                      </Button>
                    </div>
                  </div>
                )}

                <div>
                  <div className="dragon-field-label">权限策略预设</div>
                  <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
                    <Button
                      size="small"
                      appearance={!isBypassMode(inspectAgent.permissionMode) ? "primary" : "outline"}
                      onClick={() =>
                        setInspectAgent({
                          ...inspectAgent,
                          permissionMode: inspectGuide?.stagedMode || "acceptEdits",
                        })
                      }
                    >
                      按阶段分级（推荐）
                    </Button>
                    <Button
                      size="small"
                      appearance={isBypassMode(inspectAgent.permissionMode) ? "primary" : "outline"}
                      onClick={() =>
                        setInspectAgent({
                          ...inspectAgent,
                          permissionMode: inspectGuide?.bypassMode || "bypass",
                        })
                      }
                    >
                      完全放行 (bypass)
                    </Button>
                  </div>
                </div>

                <div>
                  <div className="dragon-field-label">显示名称</div>
                  <Input
                    style={{ width: "100%" }}
                    placeholder="显示名称"
                    value={inspectAgent.name}
                    onChange={(_, d) => setInspectAgent({ ...inspectAgent, name: d.value })}
                  />
                </div>
                <div>
                  <div className="dragon-field-label">命令名或自定义路径（保留符号名如 codex 可自动跨版本追踪）</div>
                  <Input
                    style={{ width: "100%" }}
                    placeholder="命令名 (如 codex / qodercli / workbuddy)"
                    value={inspectAgent.command}
                    onChange={(_, d) => setInspectAgent({ ...inspectAgent, command: d.value })}
                  />
                </div>
                <div>
                  <div className="dragon-field-label">参数模板（仅通用 / custom CLI 生效）</div>
                  <Input
                    style={{ width: "100%" }}
                    placeholder="参数模板 (支持 {{work_dir}}, {{model}}, {{prompt_file}})"
                    value={inspectAgent.argsTemplate}
                    onChange={(_, d) => setInspectAgent({ ...inspectAgent, argsTemplate: d.value })}
                  />
                </div>
                <div>
                  <div className="dragon-field-label">权限模式值</div>
                  <Input
                    style={{ width: "100%" }}
                    placeholder="权限模式 (如 workspace-write / acceptEdits / bypass)"
                    value={inspectAgent.permissionMode}
                    onChange={(_, d) => setInspectAgent({ ...inspectAgent, permissionMode: d.value })}
                  />
                </div>
                <div>
                  <div className="dragon-field-label">说明副标题</div>
                  <Input
                    style={{ width: "100%" }}
                    placeholder="说明副标题"
                    value={inspectAgent.subtitle}
                    onChange={(_, d) => setInspectAgent({ ...inspectAgent, subtitle: d.value })}
                  />
                </div>
              </DialogContent>
            )}
            <DialogActions>
              {inspectAgent?.kind === "custom" && (
                <Button
                  appearance="subtle"
                  icon={<Delete16Regular />}
                  onClick={() => {
                    const id = inspectAgent.id;
                    setInspectAgent(null);
                    void AgentService.DeleteAgent(id).then(onReload);
                  }}
                >
                  删除
                </Button>
              )}
              <Button appearance="secondary" onClick={() => setInspectAgent(null)}>
                取消
              </Button>
              <Button
                appearance="primary"
                onClick={() => inspectAgent && void saveAgentDetails(inspectAgent, () => setInspectAgent(null))}
              >
                保存配置
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>

      {/* Add Custom Agent Modal */}
      <Dialog open={openNewAgentModal} onOpenChange={(_, d) => !d.open && onCloseNewAgentModal()}>
        <DialogSurface style={{ maxWidth: 480 }}>
          <DialogBody>
            <DialogTitle>新增自定义 Coding CLI</DialogTitle>
            <DialogContent style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 8 }}>
              <Input
                placeholder="Agent 名称 (如 My Agent)"
                value={draftCustomAgent.name}
                onChange={(_, d) => setDraftCustomAgent({ ...draftCustomAgent, name: d.value })}
              />
              <Input
                placeholder="可执行命令 (如 my-agent)"
                value={draftCustomAgent.command}
                onChange={(_, d) => setDraftCustomAgent({ ...draftCustomAgent, command: d.value })}
              />
              <Input
                placeholder="参数模板 (如 exec --model {{model}} --file {{prompt_file}})"
                value={draftCustomAgent.argsTemplate}
                onChange={(_, d) => setDraftCustomAgent({ ...draftCustomAgent, argsTemplate: d.value })}
              />
              <Input
                placeholder="副标题描述"
                value={draftCustomAgent.subtitle}
                onChange={(_, d) => setDraftCustomAgent({ ...draftCustomAgent, subtitle: d.value })}
              />
            </DialogContent>
            <DialogActions>
              <Button appearance="secondary" onClick={onCloseNewAgentModal}>
                取消
              </Button>
              <Button
                appearance="primary"
                disabled={!draftCustomAgent.name.trim() || !draftCustomAgent.command.trim()}
                onClick={() => void saveAgentDetails(draftCustomAgent, onCloseNewAgentModal)}
              >
                添加并检测
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>

      {/* Save Current Profile Modal */}
      <Dialog open={saveProfileOpen} onOpenChange={(_, d) => !d.open && setSaveProfileOpen(false)}>
        <DialogSurface style={{ maxWidth: 420 }}>
          <DialogBody>
            <DialogTitle>保存当前配置为 Profile</DialogTitle>
            <DialogContent style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 8 }}>
              <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)" }}>
                将当前所有 Agent 开关、模型绑定、Claude Tiers 以及路由策略打包为一键切换快照。
              </div>
              <Input
                placeholder="Profile 名称 (例如 Review Heavy)"
                value={newProfName}
                onChange={(_, d) => setNewProfName(d.value)}
              />
              <Input
                placeholder="简介说明 (可选)"
                value={newProfDesc}
                onChange={(_, d) => setNewProfDesc(d.value)}
              />
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <span style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)" }}>标志色:</span>
                {ACCENT_PALETTE.map((col) => (
                  <button
                    key={col}
                    type="button"
                    onClick={() => setNewProfColor(col)}
                    style={{
                      width: 20,
                      height: 20,
                      borderRadius: "50%",
                      background: col,
                      border: newProfColor === col ? "2px solid var(--dragon-text-primary)" : "2px solid transparent",
                      cursor: "pointer",
                    }}
                  />
                ))}
              </div>
            </DialogContent>
            <DialogActions>
              <Button appearance="secondary" onClick={() => setSaveProfileOpen(false)}>
                取消
              </Button>
              <Button appearance="primary" disabled={!newProfName.trim()} onClick={() => void saveCurrentProfile()}>
                保存快照
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </div>
  );
}
