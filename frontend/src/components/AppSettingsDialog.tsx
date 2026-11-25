import { useEffect, useState } from "react";
import {
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
  Slider,
  Switch,
} from "@fluentui/react-components";
import { Folder16Regular, FolderOpen16Regular, Save16Regular } from "@fluentui/react-icons";
import { SettingsService } from "../../bindings/dragon/internal/appservice/index.js";
import type { AppSettings, AuthStatusDetail } from "../types/dragon.js";
import { AUTONOMY_LABELS } from "../types/dragon.js";

interface AppSettingsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  settings: AppSettings;
  auth: AuthStatusDetail | null;
  onSettingsUpdated: (s: AppSettings) => void;
}

const THEME_LABELS: Record<string, string> = {
  system: "跟随系统 (System)",
  light: "浅色 (Light)",
  dark: "深色 (Dark)",
};

const MATERIAL_LABELS: Record<string, string> = {
  mica: "Mica (云母原生默认)",
  mica_alt: "Mica Alt (深邃云母)",
  acrylic: "Acrylic (亚克力磨砂)",
};

export function AppSettingsDialog({
  open,
  onOpenChange,
  settings,
  auth,
  onSettingsUpdated,
}: AppSettingsDialogProps) {
  const [localSettings, setLocalSettings] = useState<AppSettings>(settings);
  const [saving, setSaving] = useState<boolean>(false);
  const [errMsg, setErrMsg] = useState<string>("");

  useEffect(() => {
    if (open) {
      setLocalSettings(settings);
      setErrMsg("");
    }
  }, [open, settings]);

  const handleSave = async () => {
    setSaving(true);
    setErrMsg("");
    try {
      const saved = await SettingsService.SaveSettings(localSettings);
      onSettingsUpdated(saved);
      onOpenChange(false);
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setSaving(false);
    }
  };

  const handlePickDefaultRoot = async () => {
    setErrMsg("");
    try {
      const picked = await SettingsService.PickDirectory(
        "选择默认工作区根目录",
        localSettings.defaultWorkspaceRoot || ""
      );
      if (picked) {
        setLocalSettings({ ...localSettings, defaultWorkspaceRoot: picked });
      }
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  const handleOpenDefaultRoot = async () => {
    if (!localSettings.defaultWorkspaceRoot.trim()) return;
    setErrMsg("");
    try {
      await SettingsService.OpenDirectory(localSettings.defaultWorkspaceRoot);
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(_, d) => onOpenChange(d.open)}>
      <DialogSurface style={{ maxWidth: 560 }}>
        <DialogBody>
          <DialogTitle>系统设置</DialogTitle>
          <DialogContent style={{ display: "flex", flexDirection: "column", gap: 14, marginTop: 10 }}>
            {errMsg && (
              <MessageBar intent="error">
                <MessageBarBody>{errMsg}</MessageBarBody>
              </MessageBar>
            )}

            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 10 }}>
              <div>
                <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4 }}>主题模式</div>
                <Dropdown
                  value={THEME_LABELS[localSettings.themeMode] || localSettings.themeMode}
                  selectedOptions={[localSettings.themeMode]}
                  onOptionSelect={(_, d) =>
                    d.optionValue && setLocalSettings({ ...localSettings, themeMode: d.optionValue })
                  }
                  style={{ width: "100%" }}
                >
                  <Option value="system">跟随系统 (System)</Option>
                  <Option value="light">浅色 (Light)</Option>
                  <Option value="dark">深色 (Dark)</Option>
                </Dropdown>
              </div>

              <div>
                <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4 }}>Windows 11 背景材质</div>
                <Dropdown
                  value={MATERIAL_LABELS[localSettings.backdropMaterial] || localSettings.backdropMaterial}
                  selectedOptions={[localSettings.backdropMaterial]}
                  onOptionSelect={(_, d) =>
                    d.optionValue &&
                    setLocalSettings({ ...localSettings, backdropMaterial: d.optionValue })
                  }
                  style={{ width: "100%" }}
                >
                  <Option value="mica">Mica (云母原生默认)</Option>
                  <Option value="mica_alt">Mica Alt (深邃云母)</Option>
                  <Option value="acrylic">Acrylic (亚克力磨砂)</Option>
                </Dropdown>
              </div>
            </div>

            <div>
              <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4 }}>
                默认工作区根目录（新接入仓库默认克隆位置）
              </div>
              <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
                <Input
                  placeholder="例如 D:\DragonWorkspaces"
                  value={localSettings.defaultWorkspaceRoot}
                  onChange={(_, d) =>
                    setLocalSettings({ ...localSettings, defaultWorkspaceRoot: d.value })
                  }
                  style={{ flex: 1, minWidth: 0 }}
                />
                <Button
                  appearance="outline"
                  icon={<Folder16Regular />}
                  onClick={() => void handlePickDefaultRoot()}
                >
                  选择目录
                </Button>
                {localSettings.defaultWorkspaceRoot.trim() && (
                  <>
                    <Button
                      appearance="subtle"
                      icon={<FolderOpen16Regular />}
                      onClick={() => void handleOpenDefaultRoot()}
                    >
                      打开
                    </Button>
                    <Button
                      appearance="subtle"
                      onClick={() => setLocalSettings({ ...localSettings, defaultWorkspaceRoot: "" })}
                    >
                      清空
                    </Button>
                  </>
                )}
              </div>
              <div style={{ fontSize: 11.5, color: "var(--dragon-text-secondary)", marginTop: 4, lineHeight: 1.45 }}>
                接入新仓库时自动默认绑定为 <code>&lt;根目录&gt;\&lt;仓库名&gt;</code>，可在每个仓库的配置中单独修改。
              </div>
            </div>

            <div>
              <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4 }}>新接入仓库默认自动化门禁档位</div>
              <Dropdown
                value={AUTONOMY_LABELS[localSettings.defaultAutonomyMode] || localSettings.defaultAutonomyMode}
                selectedOptions={[localSettings.defaultAutonomyMode]}
                onOptionSelect={(_, d) =>
                  d.optionValue &&
                  setLocalSettings({ ...localSettings, defaultAutonomyMode: d.optionValue })
                }
                style={{ width: "100%", minWidth: 0 }}
              >
                <Option value="template">跟随模板门禁（推荐）</Option>
                <Option value="semi_auto">半自动护航（仅合并前审批）</Option>
                <Option value="full_auto">全自动一条龙</Option>
                <Option value="step_by_step">多节点强审批</Option>
              </Dropdown>
            </div>

            <div>
              <div style={{ fontSize: 12, fontWeight: 600 }}>
                最大并发流水线数: <b>{localSettings.maxConcurrency}</b> 个 Worktree 并行
              </div>
              <Slider
                min={1}
                max={6}
                step={1}
                value={localSettings.maxConcurrency}
                onChange={(_, d) => setLocalSettings({ ...localSettings, maxConcurrency: d.value })}
              />
            </div>

            <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
              <Switch
                label="点击关闭按钮时最小化到系统托盘（保持后台轮询与 CI 监控）"
                checked={localSettings.minimizeToTray}
                onChange={(_, d) => setLocalSettings({ ...localSettings, minimizeToTray: d.checked })}
              />
              <Switch
                label="启用 Windows 原生 Toast 通知（等待审批 / 阻塞 / 发版完成）"
                checked={localSettings.enableToastNotifications}
                onChange={(_, d) =>
                  setLocalSettings({ ...localSettings, enableToastNotifications: d.checked })
                }
              />
            </div>

            <div className="dragon-subtle-box" style={{ padding: "10px 12px", fontSize: 11.5, color: "var(--dragon-text-secondary)", lineHeight: 1.55 }}>
              <div>
                Dragon 数据库与日志产物目录: <code>{auth?.paths?.rootDir || "%LOCALAPPDATA%\\Dragon"}</code>
              </div>
              <div>代码与验证命令将在各仓库绑定的本地工作区同级 <code>.dragon-worktrees</code> 隔离目录中执行。</div>
              <div>注：更改窗口原生 DWM 材质（Mica / Mica Alt / Acrylic）将在下次重启应用时完全生效。</div>
            </div>
          </DialogContent>
          <DialogActions>
            <Button appearance="secondary" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button
              appearance="primary"
              disabled={saving}
              icon={<Save16Regular />}
              onClick={() => void handleSave()}
            >
              {saving ? "保存中..." : "保存设置"}
            </Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}
