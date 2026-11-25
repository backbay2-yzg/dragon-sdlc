import type { ReactNode } from "react";

interface MarkdownViewerProps {
  content: string;
}

export function MarkdownViewer({ content }: MarkdownViewerProps) {
  if (!content.trim()) {
    return (
      <div style={{ color: "var(--dragon-text-secondary)", fontSize: 13 }}>
        暂无内容。
      </div>
    );
  }

  const lines = content.split("\n");
  const blocks: ReactNode[] = [];
  let inCode = false;
  let codeLines: string[] = [];

  const flushCode = (key: string) => {
    if (codeLines.length === 0) return;
    blocks.push(
      <pre
        key={key}
        className="dragon-console"
        style={{ margin: "8px 0", padding: "10px 12px", maxHeight: 320 }}
      >
        <code>{codeLines.join("\n")}</code>
      </pre>
    );
    codeLines = [];
  };

  lines.forEach((raw, idx) => {
    const line = raw.replace(/\r$/, "");
    if (line.trim().startsWith("```")) {
      if (inCode) {
        inCode = false;
        flushCode(`code-${idx}`);
      } else {
        inCode = true;
      }
      return;
    }
    if (inCode) {
      codeLines.push(line);
      return;
    }

    if (line.startsWith("### ")) {
      blocks.push(
        <h4 key={idx} style={{ margin: "12px 0 4px", fontSize: 14, fontWeight: 600 }}>
          {line.slice(4)}
        </h4>
      );
    } else if (line.startsWith("## ")) {
      blocks.push(
        <h3 key={idx} style={{ margin: "14px 0 6px", fontSize: 15.5, fontWeight: 600 }}>
          {line.slice(3)}
        </h3>
      );
    } else if (line.startsWith("# ")) {
      blocks.push(
        <h2 key={idx} style={{ margin: "12px 0 8px", fontSize: 17, fontWeight: 700 }}>
          {line.slice(2)}
        </h2>
      );
    } else if (line.startsWith("> ")) {
      blocks.push(
        <blockquote
          key={idx}
          style={{
            margin: "6px 0",
            padding: "4px 10px",
            borderLeft: "3px solid var(--dragon-accent)",
            color: "var(--dragon-text-secondary)",
            fontSize: 13,
          }}
        >
          {line.slice(2)}
        </blockquote>
      );
    } else if (line.trim().startsWith("- ") || line.trim().startsWith("* ")) {
      blocks.push(
        <div key={idx} style={{ display: "flex", gap: 8, margin: "3px 0 3px 6px", fontSize: 13.5, lineHeight: 1.55 }}>
          <span style={{ color: "var(--dragon-accent)" }}>•</span>
          <span>{line.trim().slice(2)}</span>
        </div>
      );
    } else if (line.trim() === "") {
      blocks.push(<div key={idx} style={{ height: 6 }} />);
    } else {
      blocks.push(
        <p key={idx} style={{ margin: "3px 0", fontSize: 13.5, lineHeight: 1.6 }}>
          {line}
        </p>
      );
    }
  });
  if (inCode) {
    flushCode("code-end");
  }

  return <div style={{ userSelect: "text" }}>{blocks}</div>;
}
