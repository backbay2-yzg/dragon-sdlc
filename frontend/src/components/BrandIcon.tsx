interface BrandIconProps {
  kind: string;
  size?: number;
}

interface BrandMeta {
  bg: string;
  fg: string;
  glyph: string;
}

const BRAND_META: Record<string, BrandMeta> = {
  // Agents
  claude: { bg: "rgba(217, 119, 87, 0.16)", fg: "#D97757", glyph: "C" },
  codex: { bg: "rgba(99, 102, 241, 0.16)", fg: "#6366F1", glyph: "CX" },
  qoder: { bg: "rgba(16, 185, 129, 0.16)", fg: "#059669", glyph: "QD" },
  workbuddy: { bg: "rgba(14, 165, 233, 0.16)", fg: "#0284C7", glyph: "WB" },
  codebuddy: { bg: "rgba(59, 130, 246, 0.16)", fg: "#2563EB", glyph: "CB" },
  gemini: { bg: "rgba(37, 99, 235, 0.16)", fg: "#2563EB", glyph: "G" },
  agy: { bg: "rgba(14, 165, 233, 0.16)", fg: "#0EA5E9", glyph: "A" },
  opencode: { bg: "rgba(100, 116, 139, 0.18)", fg: "#475569", glyph: "[]" },
  copilot: { bg: "rgba(16, 185, 129, 0.16)", fg: "#059669", glyph: "CP" },
  kimi: { bg: "rgba(15, 23, 42, 0.12)", fg: "#0F172A", glyph: "K·" },
  aider: { bg: "rgba(245, 158, 11, 0.16)", fg: "#D97706", glyph: "Ai" },
  cline: { bg: "rgba(139, 92, 246, 0.16)", fg: "#7C3AED", glyph: "CL" },
  qwen_code: { bg: "rgba(124, 58, 237, 0.16)", fg: "#7C3AED", glyph: "Q" },
  goose: { bg: "rgba(236, 72, 153, 0.15)", fg: "#DB2777", glyph: "GS" },
  amp: { bg: "rgba(239, 68, 68, 0.15)", fg: "#DC2626", glyph: "⚡" },

  // Providers
  anthropic: { bg: "rgba(217, 119, 87, 0.16)", fg: "#D97757", glyph: "✳" },
  openai: { bg: "rgba(16, 163, 127, 0.16)", fg: "#10A37F", glyph: "◎" },
  google: { bg: "rgba(59, 130, 246, 0.16)", fg: "#2563EB", glyph: "✦" },
  deepseek: { bg: "rgba(56, 189, 248, 0.18)", fg: "#0284C7", glyph: "DS" },
  moonshot: { bg: "rgba(71, 85, 105, 0.18)", fg: "#334155", glyph: "K·" },
  zhipu: { bg: "rgba(79, 70, 229, 0.16)", fg: "#4F46E5", glyph: "Z" },
  qwen: { bg: "rgba(139, 92, 246, 0.18)", fg: "#7C3AED", glyph: "QW" },
  minimax: { bg: "rgba(244, 63, 94, 0.16)", fg: "#E11D48", glyph: "MM" },
  openrouter: { bg: "rgba(20, 184, 166, 0.16)", fg: "#0D9488", glyph: "OR" },
  native: { bg: "rgba(107, 114, 128, 0.15)", fg: "#6B7280", glyph: "•" },
};

export function BrandIcon({ kind, size = 30 }: BrandIconProps) {
  const key = (kind || "").toLowerCase();
  const meta = BRAND_META[key] || { bg: "rgba(15, 108, 189, 0.15)", fg: "#0F6CBD", glyph: (key[0] || "A").toUpperCase() };
  const fontSize = Math.max(10, Math.round(size * 0.4));

  return (
    <span
      aria-hidden="true"
      style={{
        width: size,
        height: size,
        minWidth: size,
        borderRadius: Math.round(size * 0.27),
        background: meta.bg,
        color: meta.fg,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        fontWeight: 700,
        fontSize,
        letterSpacing: "-0.03em",
        border: "1px solid var(--dragon-stroke)",
      }}
    >
      {meta.glyph}
    </span>
  );
}
