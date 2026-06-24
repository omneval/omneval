import { colors } from "@/theme";

// ── Span Kind color mapping ─────────────────────────────────────

/**
 * Maps a span kind string to its canonical display colour.
 *
 * This module is the single source of truth for span kind colours and icons.
 * Consumers (Traces list, TraceDetail, ConversationDetail) must import from
 * here so every place shows the same colour/shape for each kind.
 */
export const KIND_COLOR_MAP: Record<string, string> = {
  llm: colors.accents.emberFlare,
  tool: colors.accents.softGlow,
  agent: colors.accents.flicker,
  chain: "#60a5fa",
  internal: colors.typography.ashGrey,
};

// ── Span Kind icon component ────────────────────────────────────

interface SpanKindIconProps {
  kind: string;
  /** When true, render a small label next to the dot (e.g. "LLM"). */
  label?: boolean;
  /** Label override — defaults to upper-cased first three chars of kind. */
  labelOverride?: string;
  /** Extra CSS class (for tests / overrides). */
  className?: string;
}

/**
 * Small dot + optional label indicating a span's kind.
 *
 * The dot is 8×8 px with a subtle ring so it reads on both dark and light
 * backgrounds.  It matches the size/shape used in TraceDetail's tree view.
 */
export function SpanKindIcon({
  kind,
  label,
  labelOverride,
  className,
}: SpanKindIconProps) {
  const color = KIND_COLOR_MAP[kind] ?? colors.backgrounds.caveWall;
  const displayLabel = labelOverride ?? kind.slice(0, 3).toUpperCase();

  return (
    <span
      className={`inline-flex items-center gap-1.5 ${className ?? ""}`}
      title={kind}
    >
      <span
        className="inline-block w-2 h-2 rounded-full flex-shrink-0"
        style={{ backgroundColor: color }}
        aria-hidden="true"
      />
      {label && (
        <span className="text-xs font-medium tabular-nums" style={{ color }}>
          {displayLabel}
        </span>
      )}
    </span>
  );
}