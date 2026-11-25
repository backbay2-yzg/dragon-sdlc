import { useEffect, useMemo, useRef, useState } from "react";
import { Button, Dropdown, Option, Switch } from "@fluentui/react-components";
import { Copy16Regular } from "@fluentui/react-icons";
import type { RunLog } from "../types/dragon.js";

interface LiveConsoleProps {
  logs: RunLog[];
}

export function LiveConsole({ logs }: LiveConsoleProps) {
  const [stageFilter, setStageFilter] = useState<string>("all");
  const [autoScroll, setAutoScroll] = useState<boolean>(true);
  const [copied, setCopied] = useState<boolean>(false);
  const bottomRef = useRef<HTMLDivElement | null>(null);

  const stageKeys = useMemo(() => {
    const s = new Set<string>();
    for (const l of logs) {
      if (l.stageKey) s.add(l.stageKey);
    }
    return Array.from(s);
  }, [logs]);

  const filtered = useMemo(() => {
    if (stageFilter === "all") return logs;
    return logs.filter((l) => l.stageKey === stageFilter);
  }, [logs, stageFilter]);

  useEffect(() => {
    if (autoScroll) {
      bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [filtered.length, autoScroll]);

  const copyAll = async () => {
    const text = filtered.map((l) => `[${l.stageKey}] ${l.content}`).join("\n");
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1800);
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8, height: "100%" }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 10 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <Dropdown
            size="small"
            value={stageFilter === "all" ? "全部阶段日志" : stageFilter}
            selectedOptions={[stageFilter]}
            onOptionSelect={(_, d) => d.optionValue && setStageFilter(d.optionValue)}
            style={{ minWidth: 160 }}
          >
            <Option value="all" text={`全部阶段日志 (${logs.length})`}>
              全部阶段日志 ({logs.length})
            </Option>
            {stageKeys.map((k) => (
              <Option key={k} value={k}>
                {k}
              </Option>
            ))}
          </Dropdown>
          <Switch
            label="自动滚动"
            checked={autoScroll}
            onChange={(_, d) => setAutoScroll(d.checked)}
          />
        </div>

        <Button size="small" appearance="subtle" icon={<Copy16Regular />} onClick={copyAll}>
          {copied ? "已复制" : "复制日志"}
        </Button>
      </div>

      <div className="dragon-console" style={{ flex: 1, minHeight: 280, maxHeight: 440 }}>
        {filtered.length === 0 ? (
          <div style={{ color: "#9ca3af" }}>暂无日志输出，等待 Agent 执行中...</div>
        ) : (
          filtered.map((entry, idx) => {
            let color = "#e5e7eb";
            if (entry.stream === "stderr") color = "#fca5a5";
            else if (entry.stream === "system") color = "#7dd3fc";

            return (
              <div key={entry.id || idx} style={{ color, whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
                <span style={{ color: "#6b7280", marginRight: 8 }}>[{entry.stageKey || "sys"}]</span>
                {entry.content}
              </div>
            );
          })
        )}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
