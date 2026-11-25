import { useEffect, useRef, useState } from "react";
import {
  Avatar,
  Badge,
  Button,
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
} from "@fluentui/react-components";
import {
  ArrowSync16Regular,
  CheckmarkCircle16Regular,
  Copy16Regular,
  Key16Regular,
  Open16Regular,
  SignOut20Regular,
} from "@fluentui/react-icons";
import { Browser } from "@wailsio/runtime";
import { SettingsService } from "../../bindings/dragon/internal/appservice/index.js";
import type {
  AppSettings,
  AuthStatusDetail,
  DeviceFlowStart,
} from "../types/dragon.js";

interface GitHubAuthDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  auth: AuthStatusDetail | null;
  settings: AppSettings;
  onAuthChanged: () => Promise<void> | void;
  onSettingsUpdated: (s: AppSettings) => void;
}

type AuthTab = "gh_cli" | "pat" | "oauth_device";

const AUTH_METHOD_LABELS: Record<string, string> = {
  gh_cli: "本机 gh CLI",
  pat: "Fine-grained PAT",
  oauth_device: "OAuth Device Flow",
  none: "未连接",
};

export function GitHubAuthDialog({
  open,
  onOpenChange,
  auth,
  settings,
  onAuthChanged,
  onSettingsUpdated,
}: GitHubAuthDialogProps) {
  const [tab, setTab] = useState<AuthTab>("gh_cli");
  const [patInput, setPatInput] = useState<string>("");
  const [oauthClientID, setOauthClientID] = useState<string>("");
  const [deviceFlow, setDeviceFlow] = useState<DeviceFlowStart | null>(null);
  const [copiedCode, setCopiedCode] = useState<boolean>(false);
  const [busy, setBusy] = useState<boolean>(false);
  const [pollingStatus, setPollingStatus] = useState<string>("");
  const [msg, setMsg] = useState<string>("");
  const [errMsg, setErrMsg] = useState<string>("");

  const pollTimerRef = useRef<number | null>(null);
  const pollActiveRef = useRef<boolean>(false);

  const stopPolling = () => {
    pollActiveRef.current = false;
    if (pollTimerRef.current !== null) {
      window.clearTimeout(pollTimerRef.current);
      pollTimerRef.current = null;
    }
  };

  useEffect(() => {
    if (open) {
      const m = settings.authMethod;
      if (m === "pat" || m === "oauth_device" || m === "gh_cli") {
        setTab(m);
      } else {
        setTab("gh_cli");
      }
      setOauthClientID(settings.oauthClientId || "");
      setMsg("");
      setErrMsg("");
    } else {
      stopPolling();
      setDeviceFlow(null);
      setPollingStatus("");
      setCopiedCode(false);
    }
    return () => {
      stopPolling();
    };
  }, [open, settings.authMethod, settings.oauthClientId]);

  const refreshAll = async () => {
    const [updatedSettings] = await Promise.all([
      SettingsService.GetSettings(),
      Promise.resolve(onAuthChanged()),
    ]);
    if (updatedSettings) onSettingsUpdated(updatedSettings);
  };

  const handleRefreshCheck = async () => {
    setBusy(true);
    setErrMsg("");
    setMsg("");
    try {
      await onAuthChanged();
      setMsg("已重新校验当前 GitHub 登录态。");
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setBusy(false);
    }
  };

  const handleDisconnect = async () => {
    stopPolling();
    setDeviceFlow(null);
    setPollingStatus("");
    setBusy(true);
    setErrMsg("");
    setMsg("");
    try {
      await SettingsService.DisconnectGitHub();
      await refreshAll();
      setMsg("已断开 GitHub 连接并清除本地已保存的 Token。");
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setBusy(false);
    }
  };

  const handleUseGHCLI = async () => {
    setBusy(true);
    setErrMsg("");
    setMsg("");
    try {
      const id = await SettingsService.UseGHCLIAuth();
      await refreshAll();
      if (id?.authenticated) {
        setMsg(`已成功复用本机 gh CLI 登录态（@${id.username}）。`);
      } else {
        setErrMsg(`gh CLI 校验未通过：${id?.error || "请先在终端运行 gh auth login"}`);
      }
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setBusy(false);
    }
  };

  const handleSavePAT = async () => {
    if (!patInput.trim()) return;
    setBusy(true);
    setErrMsg("");
    setMsg("");
    try {
      const id = await SettingsService.SaveGitHubPAT(patInput.trim());
      setPatInput("");
      await refreshAll();
      if (id?.authenticated) {
        setMsg(`PAT 已加密存入 Windows Credential Manager 并验证通过（@${id.username}）。`);
      } else {
        setErrMsg(`PAT 已保存，但校验失败：${id?.error || "请检查 Token 是否过期或权限不足"}`);
      }
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setBusy(false);
    }
  };

  const pollOnce = async (clientID: string, deviceCode: string) => {
    const res = await SettingsService.PollGitHubDeviceFlow(clientID, deviceCode);
    if (!pollActiveRef.current && res?.status !== "completed") {
      // Manual check when background polling is off still needs to handle completion.
    }
    if (res?.status === "completed") {
      stopPolling();
      setDeviceFlow(null);
      setPollingStatus("");
      await refreshAll();
      setMsg("OAuth Device Flow 授权成功，Token 已加密存入 Windows Credential Manager！");
      return res;
    }
    if (res?.status === "expired") {
      stopPolling();
      setDeviceFlow(null);
      setPollingStatus("");
      setErrMsg(res.error || "授权码已过期，请重新发起授权。");
      return res;
    }
    if (res?.status === "error") {
      stopPolling();
      setPollingStatus("");
      setErrMsg(res.error || "OAuth 授权校验失败。");
      return res;
    }
    if (res?.status === "slow_down") {
      setPollingStatus("等待浏览器确认中（按 GitHub 要求降频轮询）...");
    } else {
      setPollingStatus("等待你在浏览器中完成授权确认，正在自动检测...");
    }
    return res;
  };

  const schedulePoll = (clientID: string, deviceCode: string, intervalSec: number) => {
    if (!pollActiveRef.current) return;
    pollTimerRef.current = window.setTimeout(async () => {
      if (!pollActiveRef.current) return;
      try {
        const res = await SettingsService.PollGitHubDeviceFlow(clientID, deviceCode);
        if (!pollActiveRef.current) return;
        if (res?.status === "completed") {
          stopPolling();
          setDeviceFlow(null);
          setPollingStatus("");
          await refreshAll();
          setMsg("OAuth Device Flow 授权成功，Token 已加密存入 Windows Credential Manager！");
          return;
        }
        if (res?.status === "expired") {
          stopPolling();
          setDeviceFlow(null);
          setPollingStatus("");
          setErrMsg(res.error || "授权码已过期，请重新发起授权。");
          return;
        }
        if (res?.status === "error") {
          stopPolling();
          setPollingStatus("");
          setErrMsg(res.error || "OAuth 授权校验失败。");
          return;
        }
        const nextInterval = res?.status === "slow_down" ? intervalSec + 5 : intervalSec;
        setPollingStatus(
          res?.status === "slow_down"
            ? `等待浏览器确认中（每 ${nextInterval}s 自动检测）...`
            : `等待你在浏览器中输入授权码确认（每 ${nextInterval}s 自动检测）...`
        );
        schedulePoll(clientID, deviceCode, nextInterval);
      } catch {
        if (!pollActiveRef.current) return;
        setPollingStatus(`轮询遇到网络波动，${intervalSec}s 后重试...`);
        schedulePoll(clientID, deviceCode, intervalSec);
      }
    }, Math.max(3, intervalSec) * 1000);
  };

  const handleStartDeviceFlow = async () => {
    const cid = oauthClientID.trim();
    if (!cid) return;
    stopPolling();
    setBusy(true);
    setErrMsg("");
    setMsg("");
    try {
      const flow = await SettingsService.StartGitHubDeviceFlow(cid);
      if (!flow) return;
      setDeviceFlow(flow);
      const updatedCfg = await SettingsService.GetSettings();
      if (updatedCfg) onSettingsUpdated(updatedCfg);
      if (flow.verificationUri) {
        void Browser.OpenURL(flow.verificationUri);
      }
      const interval = flow.interval > 0 ? flow.interval : 5;
      pollActiveRef.current = true;
      setPollingStatus(`等待你在浏览器中输入授权码确认（每 ${interval}s 自动检测）...`);
      schedulePoll(cid, flow.deviceCode, interval);
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setBusy(false);
    }
  };

  const handleManualPoll = async () => {
    if (!deviceFlow) return;
    setBusy(true);
    setErrMsg("");
    try {
      await pollOnce(oauthClientID.trim(), deviceFlow.deviceCode);
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setBusy(false);
    }
  };

  const handleCopyCode = async () => {
    if (!deviceFlow?.userCode) return;
    try {
      await navigator.clipboard.writeText(deviceFlow.userCode);
      setCopiedCode(true);
      window.setTimeout(() => setCopiedCode(false), 2000);
    } catch {
      // Ignore clipboard permission errors in restricted contexts.
    }
  };

  const isAuth = Boolean(auth?.identity?.authenticated);
  const currentMethod = auth?.identity?.method || settings.authMethod || "none";

  return (
    <Dialog open={open} onOpenChange={(_, d) => onOpenChange(d.open)}>
      <DialogSurface style={{ maxWidth: 540 }}>
        <DialogBody>
          <DialogTitle>GitHub 账号与授权管理</DialogTitle>
          <DialogContent style={{ display: "flex", flexDirection: "column", gap: 14, marginTop: 10 }}>
            {/* Top Current Account Summary */}
            <div
              className="dragon-subtle-box"
              style={{
                padding: "12px 14px",
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: 12,
              }}
            >
              <div style={{ display: "flex", alignItems: "center", gap: 11, minWidth: 0, flex: 1 }}>
                <Avatar
                  size={36}
                  name={auth?.identity?.username || "GitHub"}
                  image={auth?.identity?.avatarUrl ? { src: auth.identity.avatarUrl } : undefined}
                  color={isAuth ? "brand" : "neutral"}
                />
                <div style={{ minWidth: 0, flex: 1 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                    <span style={{ fontWeight: 600, fontSize: 14 }}>
                      {isAuth ? `@${auth?.identity?.username}` : "未连接 GitHub"}
                    </span>
                    <Badge size="small" appearance="tint" color={isAuth ? "success" : "warning"}>
                      {isAuth ? "已认证" : "未认证"}
                    </Badge>
                  </div>
                  <div
                    style={{
                      fontSize: 12,
                      color: "var(--dragon-text-secondary)",
                      marginTop: 2,
                      whiteSpace: "nowrap",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                    }}
                    title={auth?.identity?.scopes || auth?.identity?.error || ""}
                  >
                    {isAuth
                      ? `方式: ${AUTH_METHOD_LABELS[currentMethod] || currentMethod}${
                          auth?.identity?.scopes ? ` · Scopes: ${auth.identity.scopes}` : ""
                        }`
                      : auth?.identity?.error || "请选择下方任意一种方式完成 GitHub 授权"}
                  </div>
                </div>
              </div>

              <div style={{ display: "flex", alignItems: "center", gap: 6, flexShrink: 0 }}>
                <Button
                  size="small"
                  appearance="subtle"
                  disabled={busy}
                  icon={<ArrowSync16Regular />}
                  onClick={() => void handleRefreshCheck()}
                >
                  刷新
                </Button>
                {(isAuth || currentMethod !== "none" || auth?.hasPat) && (
                  <Button
                    size="small"
                    appearance="subtle"
                    disabled={busy}
                    icon={<SignOut20Regular />}
                    onClick={() => void handleDisconnect()}
                  >
                    断开
                  </Button>
                )}
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

            {/* Auth Method Switcher */}
            <div>
              <div className="dragon-segmented-tabs" role="tablist" style={{ width: "100%", display: "flex" }}>
                {(
                  [
                    { id: "gh_cli", label: "本机 gh CLI" },
                    { id: "pat", label: "Fine-grained PAT" },
                    { id: "oauth_device", label: "OAuth Device Flow" },
                  ] as const
                ).map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    role="tab"
                    aria-selected={tab === item.id}
                    className={`dragon-seg-tab ${tab === item.id ? "active" : ""}`}
                    style={{ flex: 1 }}
                    onClick={() => setTab(item.id)}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>

            {tab === "gh_cli" && (
              <div className="dragon-subtle-box" style={{ padding: 14, display: "flex", flexDirection: "column", gap: 10 }}>
                <div style={{ fontSize: 13, fontWeight: 600 }}>自动复用本机 GitHub CLI 登录态</div>
                <div style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)", lineHeight: 1.55 }}>
                  直接读取本机已登录的 <code>gh auth token</code>，无需额外创建或粘贴 Token，适合已安装并登录{" "}
                  <code>gh</code> 的开发环境。
                </div>
                <div style={{ display: "flex", justifyContent: "flex-end" }}>
                  <Button appearance="primary" disabled={busy} onClick={() => void handleUseGHCLI()}>
                    {busy ? "检测中..." : "检测并复用 gh CLI 登录态"}
                  </Button>
                </div>
              </div>
            )}

            {tab === "pat" && (
              <div className="dragon-subtle-box" style={{ padding: 14, display: "flex", flexDirection: "column", gap: 10 }}>
                <div style={{ fontSize: 13, fontWeight: 600 }}>Fine-grained Personal Access Token (PAT)</div>
                <div style={{ fontSize: 12, color: "var(--dragon-text-secondary)", lineHeight: 1.55 }}>
                  建议授予目标仓库权限：<code>Contents (R/W)</code>、<code>Issues (R/W)</code>、
                  <code>Pull requests (R/W)</code> 与 <code>Actions (Read)</code>。
                </div>
                <div style={{ display: "flex", gap: 8 }}>
                  <Input
                    type="password"
                    contentBefore={<Key16Regular />}
                    placeholder={auth?.hasPat ? "已存储 PAT（输入新 Token 可覆盖）" : "github_pat_... 或 ghp_..."}
                    value={patInput}
                    onChange={(_, d) => setPatInput(d.value)}
                    style={{ flex: 1 }}
                  />
                  <Button
                    appearance="primary"
                    disabled={busy || !patInput.trim()}
                    onClick={() => void handleSavePAT()}
                  >
                    保存并验证
                  </Button>
                </div>
              </div>
            )}

            {tab === "oauth_device" && (
              <div className="dragon-subtle-box" style={{ padding: 14, display: "flex", flexDirection: "column", gap: 10 }}>
                <div style={{ fontSize: 13, fontWeight: 600 }}>GitHub OAuth Device Flow 网页授权</div>
                <div style={{ fontSize: 12, color: "var(--dragon-text-secondary)", lineHeight: 1.55 }}>
                  填写你在 GitHub Developer Settings 中创建的 OAuth App Client ID（需开启 Device Flow），会自动记住供后续使用。
                </div>
                <div style={{ display: "flex", gap: 8 }}>
                  <Input
                    placeholder="Ov23li..."
                    value={oauthClientID}
                    onChange={(_, d) => setOauthClientID(d.value)}
                    style={{ flex: 1 }}
                  />
                  <Button
                    appearance="primary"
                    disabled={busy || !oauthClientID.trim()}
                    onClick={() => void handleStartDeviceFlow()}
                  >
                    发起授权
                  </Button>
                </div>

                {deviceFlow && (
                  <div
                    className="dragon-card"
                    style={{ padding: 12, display: "flex", flexDirection: "column", gap: 8 }}
                  >
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
                      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                        <span style={{ fontSize: 12.5, color: "var(--dragon-text-secondary)" }}>一次性授权码:</span>
                        <code
                          style={{
                            fontSize: 15,
                            fontWeight: 700,
                            letterSpacing: "0.08em",
                            padding: "2px 8px",
                            borderRadius: 6,
                            background: "var(--dragon-surface-subtle)",
                            border: "1px solid var(--dragon-stroke)",
                          }}
                        >
                          {deviceFlow.userCode}
                        </code>
                      </div>
                      <div style={{ display: "flex", gap: 6 }}>
                        <Button
                          size="small"
                          appearance="subtle"
                          icon={copiedCode ? <CheckmarkCircle16Regular /> : <Copy16Regular />}
                          onClick={() => void handleCopyCode()}
                        >
                          {copiedCode ? "已复制" : "复制"}
                        </Button>
                        <Button
                          size="small"
                          appearance="outline"
                          icon={<Open16Regular />}
                          onClick={() => void Browser.OpenURL(deviceFlow.verificationUri)}
                        >
                          打开验证页
                        </Button>
                      </div>
                    </div>

                    {pollingStatus && (
                      <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12, color: "var(--dragon-text-secondary)" }}>
                        <Spinner size="extra-tiny" />
                        <span>{pollingStatus}</span>
                      </div>
                    )}

                    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                      <Button
                        size="small"
                        appearance="subtle"
                        onClick={() => {
                          stopPolling();
                          setDeviceFlow(null);
                          setPollingStatus("");
                        }}
                      >
                        取消等待
                      </Button>
                      <Button size="small" appearance="primary" disabled={busy} onClick={() => void handleManualPoll()}>
                        立即验证状态
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            )}

            <div style={{ fontSize: 11.5, color: "var(--dragon-text-secondary)" }}>
              安全说明：所有 Token 仅加密保存在 Windows Credential Manager 中，不会写入任何明文配置文件。
            </div>
          </DialogContent>
          <DialogActions>
            <Button appearance="secondary" onClick={() => onOpenChange(false)}>
              关闭
            </Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}
