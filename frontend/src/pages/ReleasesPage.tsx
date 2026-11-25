import { useCallback, useEffect, useState } from "react";
import {
  Badge,
  Button,
  Dropdown,
  Input,
  MessageBar,
  MessageBarBody,
  Option,
  Spinner,
  Switch,
  Textarea,
} from "@fluentui/react-components";
import { ArrowSync16Regular, Open16Regular, Rocket20Regular } from "@fluentui/react-icons";
import { Browser } from "@wailsio/runtime";
import { ReleaseService } from "../../bindings/dragon/internal/appservice/index.js";
import type { ReleaseOverview, Repository } from "../types/dragon.js";
import { formatShortTime } from "../types/dragon.js";
import { StatusBadge } from "../components/StatusBadge.js";

interface ReleasesPageProps {
  repos: Repository[];
  selectedRepoId: string;
  refreshSignal: number;
}

export function ReleasesPage({ repos, selectedRepoId, refreshSignal }: ReleasesPageProps) {
  const [repoId, setRepoId] = useState<string>(
    selectedRepoId !== "all" ? selectedRepoId : repos[0]?.id || ""
  );
  const [overview, setOverview] = useState<ReleaseOverview | null>(null);
  const [tagName, setTagName] = useState<string>("");
  const [changelog, setChangelog] = useState<string>("");
  const [draft, setDraft] = useState<boolean>(false);
  const [prerelease, setPrerelease] = useState<boolean>(false);
  const [loading, setLoading] = useState<boolean>(false);
  const [publishing, setPublishing] = useState<boolean>(false);
  const [msg, setMsg] = useState<string>("");
  const [errMsg, setErrMsg] = useState<string>("");

  useEffect(() => {
    if (selectedRepoId !== "all") {
      setRepoId(selectedRepoId);
    } else if (!repoId && repos.length > 0) {
      setRepoId(repos[0].id);
    }
  }, [selectedRepoId, repos, repoId]);

  const loadOverview = useCallback(async () => {
    if (!repoId) return;
    setLoading(true);
    setErrMsg("");
    try {
      const ov = await ReleaseService.GetReleaseOverview(repoId);
      setOverview(ov);
      if (ov?.preview) {
        setTagName(ov.preview.nextTag || "");
        setChangelog(ov.preview.changelog || "");
      }
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setLoading(false);
    }
  }, [repoId]);

  useEffect(() => {
    void loadOverview();
  }, [loadOverview, refreshSignal]);

  const handlePublish = async () => {
    if (!repoId) return;
    setPublishing(true);
    setMsg("");
    setErrMsg("");
    try {
      const rec = await ReleaseService.PublishRelease({
        repoId,
        tagName,
        changelog,
        mode: "batch",
        draft,
        prerelease,
        itemIds: overview?.preview?.itemIds || [],
      });
      if (rec) {
        setMsg(`已成功创建 GitHub Release ${rec.tagName}！`);
      }
      await loadOverview();
    } catch (e) {
      setErrMsg(String(e));
    } finally {
      setPublishing(false);
    }
  };

  const handleRefreshActions = async (releaseId: string) => {
    try {
      await ReleaseService.RefreshReleaseActions(releaseId);
      await loadOverview();
    } catch (e) {
      setErrMsg(String(e));
    }
  };

  const activeRepo = repos.find((r) => r.id === repoId);
  const pendingItems = overview?.pendingItems || [];
  const records = overview?.records || [];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 600 }}>发版列车与 Release 中心</h1>
          <div style={{ fontSize: 13, color: "var(--dragon-text-secondary)", marginTop: 2 }}>
            支持单 PR 即时发版与多 PR 批量列车发版，基于 Conventional Commits 自动计算 SemVer 并追踪 GitHub Actions 发布流水线
          </div>
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          {activeRepo && (
            <Badge appearance="tint" color="brand">
              当前模式: {activeRepo.releaseMode === "immediate" ? "单 PR 即时发布" : "批量发版列车"}
            </Badge>
          )}
          <Dropdown
            value={activeRepo?.fullName || "选择仓库"}
            selectedOptions={repoId ? [repoId] : []}
            onOptionSelect={(_, d) => d.optionValue && setRepoId(d.optionValue)}
            style={{ minWidth: 200 }}
          >
            {repos.map((r) => (
              <Option key={r.id} value={r.id}>
                {r.fullName}
              </Option>
            ))}
          </Dropdown>
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

      {repos.length === 0 ? (
        <div className="dragon-card" style={{ padding: 36, textAlign: "center", color: "var(--dragon-text-secondary)" }}>
          请先连接至少一个 GitHub 仓库以管理发版列车。
        </div>
      ) : (
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1.15fr", gap: 14 }}>
          {/* Pending Train + Release Preview Form */}
          <div className="dragon-card" style={{ padding: 16, display: "flex", flexDirection: "column", gap: 12 }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <span style={{ fontWeight: 600, fontSize: 15 }}>待发布 PR 队列 ({pendingItems.length})</span>
              {loading && <Spinner size="extra-tiny" />}
            </div>

            {pendingItems.length === 0 ? (
              <div className="dragon-subtle-box" style={{ padding: 16, fontSize: 13, color: "var(--dragon-text-secondary)" }}>
                当前没有待聚合成新版本的已合并 PR；你仍可手动输入 Tag 与 Changelog 直接发版。
              </div>
            ) : (
              <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: 190, overflowY: "auto" }}>
                {pendingItems.map((it) => (
                  <div
                    key={it.id}
                    className="dragon-subtle-box"
                    style={{ padding: "8px 10px", display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}
                  >
                    <div style={{ minWidth: 0, flex: 1 }}>
                      <div style={{ fontSize: 13, fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                        #{it.prNumber} {it.prTitle}
                      </div>
                      <div style={{ fontSize: 11.5, color: "var(--dragon-text-secondary)" }}>
                        合并于 {formatShortTime(it.mergedAt)}
                      </div>
                    </div>
                    <Badge appearance="tint" color={it.breaking ? "danger" : it.commitType === "feat" ? "brand" : "informative"}>
                      {it.breaking ? "BREAKING" : it.commitType}
                    </Badge>
                  </div>
                ))}
              </div>
            )}

            <div style={{ borderTop: "1px solid var(--dragon-stroke)", paddingTop: 12, display: "flex", flexDirection: "column", gap: 10 }}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <span style={{ fontWeight: 600, fontSize: 14 }}>下一版本发布预览</span>
                {overview?.preview && (
                  <span style={{ fontSize: 12, color: "var(--dragon-text-secondary)" }}>
                    基线: {overview.preview.prevTag || "无历史 Tag"} → 建议 <b>{overview.preview.bumpType}</b> 升级
                  </span>
                )}
              </div>

              <div>
                <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4 }}>Git Tag / 版本号</div>
                <Input value={tagName} onChange={(_, d) => setTagName(d.value)} placeholder="v0.1.0" style={{ width: "100%" }} />
              </div>

              <div>
                <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 4 }}>Release Changelog (Markdown)</div>
                <Textarea
                  value={changelog}
                  onChange={(_, d) => setChangelog(d.value)}
                  rows={7}
                  style={{ width: "100%", fontFamily: "var(--fontFamilyMonospace)" }}
                />
              </div>

              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <div style={{ display: "flex", gap: 12 }}>
                  <Switch label="保存为 Draft" checked={draft} onChange={(_, d) => setDraft(d.checked)} />
                  <Switch label="标记 Pre-release" checked={prerelease} onChange={(_, d) => setPrerelease(d.checked)} />
                </div>

                <Button
                  appearance="primary"
                  disabled={publishing || !tagName.trim()}
                  icon={publishing ? <Spinner size="extra-tiny" /> : <Rocket20Regular />}
                  onClick={() => void handlePublish()}
                >
                  {publishing ? "发布中..." : "发布 GitHub Release"}
                </Button>
              </div>
            </div>
          </div>

          {/* Release History & Actions Status */}
          <div className="dragon-card" style={{ padding: 16, display: "flex", flexDirection: "column", gap: 10 }}>
            <div style={{ fontWeight: 600, fontSize: 15 }}>历史发版与 GitHub Actions 追踪</div>
            {records.length === 0 ? (
              <div className="dragon-subtle-box" style={{ padding: 24, textAlign: "center", color: "var(--dragon-text-secondary)" }}>
                该仓库尚无通过 Dragon 发布的版本记录。
              </div>
            ) : (
              <div style={{ display: "flex", flexDirection: "column", gap: 8, overflowY: "auto", maxHeight: 540 }}>
                {records.map((rec) => (
                  <div key={rec.id} className="dragon-subtle-box" style={{ padding: 12, display: "flex", flexDirection: "column", gap: 6 }}>
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                        <span style={{ fontWeight: 700, fontSize: 15 }}>{rec.tagName}</span>
                        <StatusBadge status={rec.status} />
                        <Badge appearance="outline" size="small">
                          {rec.mode === "immediate" ? "单 PR 即时" : "批量列车"}
                        </Badge>
                      </div>
                      <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
                        <StatusBadge status={rec.actionsStatus || "none"} />
                        <Button
                          size="small"
                          appearance="subtle"
                          icon={<ArrowSync16Regular />}
                          onClick={() => void handleRefreshActions(rec.id)}
                          title="刷新 Actions 发布状态"
                        />
                        {rec.htmlUrl && (
                          <Button
                            size="small"
                            appearance="subtle"
                            icon={<Open16Regular />}
                            onClick={() => void Browser.OpenURL(rec.htmlUrl)}
                          >
                            Release
                          </Button>
                        )}
                      </div>
                    </div>
                    <div style={{ fontSize: 12, color: "var(--dragon-text-secondary)" }}>
                      前序版本: {rec.prevTag || "—"} · 升级类型: {rec.bumpType} · 发布于 {formatShortTime(rec.publishedAt || rec.createdAt)}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
