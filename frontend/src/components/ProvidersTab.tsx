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
  Switch,
} from "@fluentui/react-components";
import { Add16Regular, ChevronRight12Regular, Delete16Regular, Key16Regular } from "@fluentui/react-icons";
import { AgentService } from "../../bindings/dragon/internal/appservice/index.js";
import type { ModelCatalogEntry, ModelProvider } from "../types/dragon.js";
import { BrandIcon } from "./BrandIcon.js";

interface ProvidersTabProps {
  providers: ModelProvider[];
  onReload: () => Promise<void>;
  onMessage: (msg: string) => void;
  onError: (err: string) => void;
  openNewProviderModal: boolean;
  onCloseNewProviderModal: () => void;
}

export function ProvidersTab({
  providers,
  onReload,
  onMessage,
  onError,
  openNewProviderModal,
  onCloseNewProviderModal,
}: ProvidersTabProps) {
  const [keyProvider, setKeyProvider] = useState<ModelProvider | null>(null);
  const [apiKeyInput, setApiKeyInput] = useState<string>("");
  const [editProvider, setEditProvider] = useState<ModelProvider | null>(null);
  const [catalogModels, setCatalogModels] = useState<ModelCatalogEntry[]>([]);
  const [newModelId, setNewModelId] = useState<string>("");
  const [newModelName, setNewModelName] = useState<string>("");
  const [newModelTier, setNewModelTier] = useState<string>("balanced");

  const [newProv, setNewProv] = useState<ModelProvider>({
    id: "",
    name: "",
    protocol: "openai_compatible",
    baseUrl: "",
    modelsJson: "[]",
    enabled: true,
    hasKey: false,
    isBuiltin: false,
    sortOrder: 200,
  });

  const parseModels = (raw: string): ModelCatalogEntry[] => {
    try {
      const parsed = JSON.parse(raw || "[]");
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  };

  const openModelsModal = (p: ModelProvider) => {
    setEditProvider({ ...p });
    setCatalogModels(parseModels(p.modelsJson));
    setNewModelId("");
    setNewModelName("");
  };

  const saveModelsAndEndpoint = async () => {
    if (!editProvider) return;
    onError("");
    try {
      await AgentService.SaveProvider({
        ...editProvider,
        modelsJson: JSON.stringify(catalogModels),
      });
      setEditProvider(null);
      await onReload();
      onMessage(`已更新供应商「${editProvider.name}」配置与模型列表。`);
    } catch (e) {
      onError(String(e));
    }
  };

  const handleSaveKey = async () => {
    if (!keyProvider || !apiKeyInput.trim()) return;
    onError("");
    try {
      await AgentService.SaveProviderAPIKey(keyProvider.id, apiKeyInput.trim());
      setApiKeyInput("");
      setKeyProvider(null);
      await onReload();
      onMessage(`已将 ${keyProvider.name} 的 API Key 加密存入 Windows 凭据管理器。`);
    } catch (e) {
      onError(String(e));
    }
  };

  const handleClearKey = async () => {
    if (!keyProvider) return;
    onError("");
    try {
      await AgentService.ClearProviderAPIKey(keyProvider.id);
      setApiKeyInput("");
      setKeyProvider(null);
      await onReload();
      onMessage(`已清除 ${keyProvider.name} 的本地 API Key。`);
    } catch (e) {
      onError(String(e));
    }
  };

  const handleToggle = async (p: ModelProvider, enabled: boolean) => {
    await AgentService.SaveProvider({ ...p, enabled });
    await onReload();
  };

  const addCatalogModel = () => {
    if (!editProvider || !newModelId.trim()) return;
    setCatalogModels((prev) => [
      ...prev,
      {
        id: newModelId.trim(),
        name: newModelName.trim() || newModelId.trim(),
        providerId: editProvider.id,
        costTier: newModelTier,
        contextK: 128,
        tags: [],
      },
    ]);
    setNewModelId("");
    setNewModelName("");
  };

  const createCustomProvider = async () => {
    if (!newProv.name.trim() || !newProv.baseUrl.trim()) return;
    onError("");
    try {
      await AgentService.SaveProvider(newProv);
      onCloseNewProviderModal();
      setNewProv({
        id: "",
        name: "",
        protocol: "openai_compatible",
        baseUrl: "",
        modelsJson: "[]",
        enabled: true,
        hasKey: false,
        isBuiltin: false,
        sortOrder: 200,
      });
      await onReload();
      onMessage("已新增自定义模型供应商。");
    } catch (e) {
      onError(String(e));
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)" }}>
        集中管理各模型供应商的 API Base URL、模型目录与凭据；所有 API Key 均加密托管于 Windows Credential Manager。
      </div>

      <div className="dragon-list-card">
        {providers.map((p) => {
          const list = parseModels(p.modelsJson);
          return (
            <div key={p.id} className="dragon-list-row">
              <div style={{ display: "flex", alignItems: "center", gap: 12, minWidth: 0, flex: 1 }}>
                <BrandIcon kind={p.id} size={32} />
                <div style={{ minWidth: 0 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                    <span style={{ fontWeight: 600, fontSize: 13.5 }}>{p.name}</span>
                    <Badge size="extra-small" appearance="outline">
                      {p.protocol === "anthropic" ? "Anthropic" : p.protocol === "google" ? "Gemini" : "OpenAI 兼容"}
                    </Badge>
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
                    <span className={`dragon-status-dot ${p.hasKey ? "online" : "offline"}`} />
                    <span>{p.hasKey ? "API Key 已配置" : "未配置 Key (将使用 CLI 原生登录态)"}</span>
                    <span>· {p.baseUrl}</span>
                  </div>
                </div>
              </div>

              <div style={{ display: "flex", alignItems: "center", gap: 10, flexShrink: 0 }}>
                <button type="button" className="dragon-row-link" onClick={() => openModelsModal(p)}>
                  <span>Models {list.length}</span>
                  <ChevronRight12Regular />
                </button>

                <Button
                  size="small"
                  appearance={p.hasKey ? "outline" : "primary"}
                  icon={<Key16Regular />}
                  onClick={() => {
                    setApiKeyInput("");
                    setKeyProvider(p);
                  }}
                >
                  {p.hasKey ? "更新 Key" : "配置 Key"}
                </Button>

                {!p.isBuiltin && (
                  <Button
                    size="small"
                    appearance="subtle"
                    icon={<Delete16Regular />}
                    onClick={() => void AgentService.DeleteProvider(p.id).then(onReload)}
                  />
                )}

                <Switch checked={p.enabled} onChange={(_, d) => void handleToggle(p, d.checked)} />
              </div>
            </div>
          );
        })}
      </div>

      {/* API Key Modal */}
      <Dialog open={keyProvider !== null} onOpenChange={(_, d) => !d.open && setKeyProvider(null)}>
        <DialogSurface style={{ maxWidth: 440 }}>
          <DialogBody>
            <DialogTitle>配置 {keyProvider?.name} API Key</DialogTitle>
            <DialogContent style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 8 }}>
              <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)" }}>
                凭据将保存至 Windows Credential Manager (<code>Dragon/provider_key/{keyProvider?.id}</code>)。
              </div>
              <Input
                type="password"
                placeholder="输入 API Key (sk-...)"
                value={apiKeyInput}
                onChange={(_, d) => setApiKeyInput(d.value)}
              />
            </DialogContent>
            <DialogActions>
              {keyProvider?.hasKey && (
                <Button appearance="subtle" onClick={() => void handleClearKey()}>
                  清除 Key
                </Button>
              )}
              <Button appearance="secondary" onClick={() => setKeyProvider(null)}>
                取消
              </Button>
              <Button appearance="primary" disabled={!apiKeyInput.trim()} onClick={() => void handleSaveKey()}>
                加密保存
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>

      {/* Provider Models & BaseURL Modal */}
      <Dialog open={editProvider !== null} onOpenChange={(_, d) => !d.open && setEditProvider(null)}>
        <DialogSurface style={{ maxWidth: 560 }}>
          <DialogBody>
            <DialogTitle>管理供应商与模型目录：{editProvider?.name}</DialogTitle>
            {editProvider && (
              <DialogContent style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 8 }}>
                <Input
                  placeholder="API Base URL"
                  value={editProvider.baseUrl}
                  onChange={(_, d) => setEditProvider({ ...editProvider, baseUrl: d.value })}
                />
                <div className="dragon-subtle-box" style={{ padding: 10, maxHeight: 200, overflowY: "auto" }}>
                  {catalogModels.length === 0 ? (
                    <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)" }}>暂无模型，请在下方添加。</div>
                  ) : (
                    catalogModels.map((m, idx) => (
                      <div key={m.id + idx} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "4px 0" }}>
                        <div style={{ fontSize: 13 }}>
                          <b>{m.name}</b> <code style={{ fontSize: 11.5 }}>{m.id}</code>
                        </div>
                        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                          <Badge size="extra-small" appearance="outline">
                            {m.costTier}
                          </Badge>
                          <Button
                            size="small"
                            appearance="subtle"
                            icon={<Delete16Regular />}
                            onClick={() => setCatalogModels((prev) => prev.filter((_, i) => i !== idx))}
                          />
                        </div>
                      </div>
                    ))
                  )}
                </div>
                <div style={{ display: "flex", gap: 6 }}>
                  <Input placeholder="模型 ID (如 deepseek-chat)" value={newModelId} onChange={(_, d) => setNewModelId(d.value)} style={{ flex: 1 }} />
                  <Input placeholder="显示名" value={newModelName} onChange={(_, d) => setNewModelName(d.value)} style={{ flex: 1 }} />
                  <Dropdown
                    value={newModelTier}
                    selectedOptions={[newModelTier]}
                    onOptionSelect={(_, d) => d.optionValue && setNewModelTier(d.optionValue)}
                    style={{ minWidth: 105 }}
                  >
                    <Option value="budget">budget</Option>
                    <Option value="balanced">balanced</Option>
                    <Option value="flagship">flagship</Option>
                  </Dropdown>
                  <Button appearance="outline" icon={<Add16Regular />} onClick={addCatalogModel}>
                    添加
                  </Button>
                </div>
              </DialogContent>
            )}
            <DialogActions>
              <Button appearance="secondary" onClick={() => setEditProvider(null)}>
                取消
              </Button>
              <Button appearance="primary" onClick={() => void saveModelsAndEndpoint()}>
                保存更新
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>

      {/* Add Custom Provider Modal */}
      <Dialog open={openNewProviderModal} onOpenChange={(_, d) => !d.open && onCloseNewProviderModal()}>
        <DialogSurface style={{ maxWidth: 480 }}>
          <DialogBody>
            <DialogTitle>新增自定义模型供应商</DialogTitle>
            <DialogContent style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 8 }}>
              <Input placeholder="供应商名称 (如 SiliconFlow / Local Ollama)" value={newProv.name} onChange={(_, d) => setNewProv({ ...newProv, name: d.value })} />
              <Dropdown
                value={newProv.protocol}
                selectedOptions={[newProv.protocol]}
                onOptionSelect={(_, d) => d.optionValue && setNewProv({ ...newProv, protocol: d.optionValue })}
              >
                <Option value="openai_compatible">OpenAI 兼容协议 (/v1/chat/completions)</Option>
                <Option value="anthropic">Anthropic 协议 (/v1/messages)</Option>
              </Dropdown>
              <Input placeholder="Base URL (如 http://127.0.0.1:11434/v1)" value={newProv.baseUrl} onChange={(_, d) => setNewProv({ ...newProv, baseUrl: d.value })} />
            </DialogContent>
            <DialogActions>
              <Button appearance="secondary" onClick={onCloseNewProviderModal}>
                取消
              </Button>
              <Button appearance="primary" disabled={!newProv.name.trim() || !newProv.baseUrl.trim()} onClick={() => void createCustomProvider()}>
                保存供应商
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </div>
  );
}
