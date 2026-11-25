import { useMemo, useState } from "react";
import { Badge, Button } from "@fluentui/react-components";

interface DiffViewerProps {
  diffText: string;
}

interface DiffFileSection {
  filename: string;
  lines: string[];
}

export function DiffViewer({ diffText }: DiffViewerProps) {
  const [selectedFile, setSelectedFile] = useState<string>("all");

  const sections = useMemo<DiffFileSection[]>(() => {
    if (!diffText.trim()) return [];
    const lines = diffText.split("\n");
    const result: DiffFileSection[] = [];
    let current: DiffFileSection | null = null;

    for (const line of lines) {
      if (line.startsWith("diff --git ")) {
        const parts = line.split(" b/");
        const fname = parts[1] || line;
        current = { filename: fname, lines: [line] };
        result.push(current);
      } else {
        if (!current) {
          current = { filename: "changes", lines: [] };
          result.push(current);
        }
        current.lines.push(line);
      }
    }
    return result;
  }, [diffText]);

  if (!diffText.trim()) {
    return (
      <div className="dragon-subtle-box" style={{ padding: 24, textAlign: "center", color: "var(--dragon-text-secondary)" }}>
        当前工作区暂无代码变更 Diff（等待编码阶段执行或任务尚未开始）。
      </div>
    );
  }

  const visibleSections =
    selectedFile === "all" ? sections : sections.filter((s) => s.filename === selectedFile);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 6, alignItems: "center" }}>
        <Button
          size="small"
          appearance={selectedFile === "all" ? "primary" : "subtle"}
          onClick={() => setSelectedFile("all")}
        >
          全部文件 ({sections.length})
        </Button>
        {sections.map((s) => (
          <Button
            key={s.filename}
            size="small"
            appearance={selectedFile === s.filename ? "primary" : "outline"}
            onClick={() => setSelectedFile(s.filename)}
          >
            {s.filename}
          </Button>
        ))}
      </div>

      <div className="dragon-diff" style={{ maxHeight: 440 }}>
        {visibleSections.map((sec) => (
          <div key={sec.filename} style={{ marginBottom: 8 }}>
            <div
              style={{
                padding: "6px 12px",
                background: "rgba(255,255,255,0.06)",
                borderBottom: "1px solid rgba(255,255,255,0.08)",
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
              }}
            >
              <span>{sec.filename}</span>
              <Badge size="extra-small" appearance="tint" color="brand">
                {sec.lines.length} 行
              </Badge>
            </div>
            {sec.lines.map((line, i) => {
              let cls = "dragon-diff-line";
              if (line.startsWith("+") && !line.startsWith("+++")) cls += " add";
              else if (line.startsWith("-") && !line.startsWith("---")) cls += " del";
              else if (line.startsWith("@@")) cls += " hunk";
              else if (line.startsWith("diff --git") || line.startsWith("+++") || line.startsWith("---"))
                cls += " header";

              return (
                <div key={i} className={cls}>
                  {line || " "}
                </div>
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
}
