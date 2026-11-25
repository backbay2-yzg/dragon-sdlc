import type { ReactElement } from "react";
import { Avatar, Badge, Tooltip } from "@fluentui/react-components";
import {
  Bot20Regular,
  BranchFork20Regular,
  Code20Regular,
  Home20Regular,
  Rocket20Regular,
  Settings20Regular,
  TaskListSquareLtr20Regular,
} from "@fluentui/react-icons";
import type { AuthStatusDetail, DashboardStats, NavTab } from "../types/dragon.js";

interface NavigationRailProps {
  activeTab: NavTab;
  onSelectTab: (tab: NavTab) => void;
  stats: DashboardStats;
  auth: AuthStatusDetail | null;
  onOpenAuth: () => void;
  onOpenSettings: () => void;
}

const METHOD_SHORT_LABELS: Record<string, string> = {
  gh_cli: "gh CLI",
  pat: "PAT",
  oauth_device: "OAuth",
};

export function NavigationRail({
  activeTab,
  onSelectTab,
  stats,
  auth,
  onOpenAuth,
  onOpenSettings,
}: NavigationRailProps) {
  const navItems: Array<{
    key: NavTab;
    label: string;
    icon: ReactElement;
    badge?: number;
    badgeColor?: "brand" | "warning" | "informative";
  }> = [
    { key: "dashboard", label: "工作台概览", icon: <Home20Regular /> },
    {
      key: "issues",
      label: "Issue 队列",
      icon: <TaskListSquareLtr20Regular />,
      badge: stats.backlogIssues > 0 ? stats.backlogIssues : undefined,
      badgeColor: "informative",
    },
    {
      key: "pipelines",
      label: "SDLC 流水线",
      icon: <BranchFork20Regular />,
      badge:
        stats.waitingApprovals > 0
          ? stats.waitingApprovals
          : stats.activeRuns > 0
          ? stats.activeRuns
          : undefined,
      badgeColor: stats.waitingApprovals > 0 ? "warning" : "brand",
    },
    {
      key: "releases",
      label: "发版列车",
      icon: <Rocket20Regular />,
      badge: stats.pendingReleaseItems > 0 ? stats.pendingReleaseItems : undefined,
      badgeColor: "brand",
    },
    { key: "agents", label: "Agent 与路由", icon: <Bot20Regular /> },
    { key: "repos", label: "仓库管理", icon: <Code20Regular /> },
  ];

  const isAuth = Boolean(auth?.identity?.authenticated);
  const methodLabel = auth?.identity?.method
    ? METHOD_SHORT_LABELS[auth.identity.method] || auth.identity.method
    : "";

  return (
    <aside className="dragon-acrylic-rail">
      <nav aria-label="主导航">
        <div
          style={{
            fontSize: 11,
            fontWeight: 600,
            color: "var(--dragon-text-secondary)",
            padding: "4px 12px 8px",
            letterSpacing: "0.04em",
          }}
        >
          工作区
        </div>
        {navItems.map((item) => {
          const isActive = activeTab === item.key;
          return (
            <button
              key={item.key}
              type="button"
              className={`dragon-nav-item ${isActive ? "active" : ""}`}
              onClick={() => onSelectTab(item.key)}
            >
              <span style={{ display: "inline-flex", color: isActive ? "var(--dragon-accent)" : undefined }}>
                {item.icon}
              </span>
              <span style={{ flex: 1 }}>{item.label}</span>
              {item.badge !== undefined && (
                <Badge appearance="filled" color={item.badgeColor || "brand"} size="small">
                  {item.badge}
                </Badge>
              )}
            </button>
          );
        })}
      </nav>

      <div className="dragon-rail-footer">
        <Tooltip content="管理 GitHub 账号与授权方式" relationship="label">
          <button type="button" className="dragon-account-btn" onClick={onOpenAuth}>
            <Avatar
              size={28}
              name={auth?.identity?.username || "GitHub"}
              image={auth?.identity?.avatarUrl ? { src: auth.identity.avatarUrl } : undefined}
              color={isAuth ? "brand" : "neutral"}
            />
            <div style={{ minWidth: 0, flex: 1 }}>
              <div
                style={{
                  fontSize: 12.5,
                  fontWeight: 600,
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                }}
              >
                {isAuth ? `@${auth?.identity?.username}` : "未连接 GitHub"}
              </div>
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 5,
                  fontSize: 11,
                  color: "var(--dragon-text-secondary)",
                  marginTop: 1,
                }}
              >
                <span className={`dragon-status-dot ${isAuth ? "online" : "offline"}`} />
                <span style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                  {isAuth ? `${methodLabel} · 管理授权` : "点击授权登录"}
                </span>
              </div>
            </div>
          </button>
        </Tooltip>

        <Tooltip content="系统设置" relationship="label">
          <button
            type="button"
            className="dragon-gear-btn"
            aria-label="系统设置"
            onClick={onOpenSettings}
          >
            <Settings20Regular />
          </button>
        </Tooltip>
      </div>
    </aside>
  );
}
