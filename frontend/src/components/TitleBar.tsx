import {
  Button,
  Dropdown,
  Option,
  Spinner,
  Tooltip,
} from "@fluentui/react-components";
import {
  ArrowSync20Regular,
  Dismiss16Regular,
  Maximize16Regular,
  Subtract16Regular,
  WeatherMoon20Regular,
  WeatherSunny20Regular,
} from "@fluentui/react-icons";
import { Window } from "@wailsio/runtime";
import type { Repository } from "../types/dragon.js";

interface TitleBarProps {
  repos: Repository[];
  selectedRepoId: string;
  onSelectRepo: (repoId: string) => void;
  syncing: boolean;
  onSyncAll: () => void;
  isDark: boolean;
  onToggleTheme: () => void;
}

export function TitleBar({
  repos,
  selectedRepoId,
  onSelectRepo,
  syncing,
  onSyncAll,
  isDark,
  onToggleTheme,
}: TitleBarProps) {
  const selectedRepoLabel =
    selectedRepoId === "all"
      ? "全部已连接仓库"
      : repos.find((r) => r.id === selectedRepoId)?.fullName || "选择仓库";

  return (
    <header className="dragon-titlebar">
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <div
            style={{
              width: 22,
              height: 22,
              borderRadius: 6,
              background: "linear-gradient(135deg, #0F6CBD 0%, #4F46E5 100%)",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              color: "#fff",
              fontWeight: 700,
              fontSize: 12,
              boxShadow: "0 1px 4px rgba(15, 108, 189, 0.35)",
            }}
          >
            D
          </div>
          <span style={{ fontWeight: 600, fontSize: 13.5, letterSpacing: "-0.01em" }}>
            Dragon
          </span>
          <span
            style={{
              fontSize: 11,
              color: "var(--dragon-text-secondary)",
              padding: "1px 6px",
              borderRadius: 4,
              border: "1px solid var(--dragon-stroke)",
            }}
          >
            SDLC Studio
          </span>
        </div>

        <div className="dragon-nodrag" style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <Dropdown
            size="small"
            value={selectedRepoLabel}
            selectedOptions={[selectedRepoId]}
            onOptionSelect={(_, data) => {
              if (data.optionValue) onSelectRepo(data.optionValue);
            }}
            style={{ minWidth: 190 }}
          >
            <Option value="all" text={`全部已连接仓库 (${repos.length})`}>
              全部已连接仓库 ({repos.length})
            </Option>
            {repos.map((r) => (
              <Option key={r.id} value={r.id}>
                {r.fullName}
              </Option>
            ))}
          </Dropdown>

          <Tooltip content="立即同步所有仓库 Issue" relationship="label">
            <Button
              size="small"
              appearance="subtle"
              disabled={syncing || repos.length === 0}
              onClick={onSyncAll}
              icon={syncing ? <Spinner size="extra-tiny" /> : <ArrowSync20Regular />}
            >
              {syncing ? "同步中" : "同步"}
            </Button>
          </Tooltip>
        </div>
      </div>

      <div className="dragon-nodrag" style={{ display: "flex", alignItems: "center" }}>
        <Tooltip content={isDark ? "切换浅色材质" : "切换深色材质"} relationship="label">
          <Button
            size="small"
            appearance="subtle"
            onClick={onToggleTheme}
            icon={isDark ? <WeatherSunny20Regular /> : <WeatherMoon20Regular />}
            style={{ marginRight: 6 }}
          />
        </Tooltip>

        <button
          type="button"
          className="dragon-caption-btn"
          aria-label="最小化"
          onClick={() => {
            void Window.Minimise();
          }}
        >
          <Subtract16Regular />
        </button>
        <button
          type="button"
          className="dragon-caption-btn"
          aria-label="最大化/还原"
          onClick={() => {
            void Window.ToggleMaximise();
          }}
        >
          <Maximize16Regular />
        </button>
        <button
          type="button"
          className="dragon-caption-btn close"
          aria-label="关闭窗口"
          onClick={() => {
            void Window.Close();
          }}
        >
          <Dismiss16Regular />
        </button>
      </div>
    </header>
  );
}
