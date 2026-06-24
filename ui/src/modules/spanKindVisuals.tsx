import React from "react";

// ── Types ──────────────────────────────────────────────────────────

export type SpanKind = "llm" | "tool" | "agent" | "chain" | "internal";

export interface SpanKindVisual {
  color: string;
  label: string;
  Icon: React.ComponentType;
}

// ── Icon Glyphs (minimal inline SVGs, zero icon-library dependency) ─

function SparkleIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      role="img"
    >
      <path
        d="M8 1l1.2 3.6H13l-3 2.4 1.2 3.6L8 8.3 4.8 10.6 6 7 3 4.6h3.8L8 1z"
        fill="currentColor"
      />
    </svg>
  );
}

function WrenchIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      role="img"
    >
      <path
        d="M12.7 3.3A4.7 4.7 0 005.6 2l.3.3a4.7 4.7 0 00-2.3 8.3l2.8 2.8a1.4 1.4 0 002 0l1.4-1.4a1.4 1.4 0 000-2l-2.8-2.8A4.7 4.7 0 008.3 3.6l.3-.3z"
        stroke="currentColor"
        strokeWidth="1.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function PersonIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      role="img"
    >
      <circle cx="8" cy="5.5" r="2.5" stroke="currentColor" strokeWidth="1.2" />
      <path
        d="M3 14c0-2.8 2.2-5 5-5s5 2.2 5 5"
        stroke="currentColor"
        strokeWidth="1.2"
        strokeLinecap="round"
      />
    </svg>
  );
}

function ChainIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      role="img"
    >
      <path
        d="M6.3 7.3l-3-3A2.1 2.1 0 015 2.3h0a2.1 2.1 0 012.3.6l3 3A2.1 2.1 0 018.3 9h0a2.1 2.1 0 01-2.3-.6zM9.7 8.7l3 3a2.1 2.1 0 003.3-1.4v0a2.1 2.1 0 00-.6-2.3l-3-3A2.1 2.1 0 008.7 7h0a2.1 2.1 0 00.6 2.3z"
        stroke="currentColor"
        strokeWidth="1.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function DotIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      role="img"
    >
      <circle cx="8" cy="8" r="3" fill="currentColor" />
    </svg>
  );
}

// ── Visual Map ─────────────────────────────────────────────────────

const ICONS: Record<SpanKind, React.ComponentType> = {
  llm: SparkleIcon,
  tool: WrenchIcon,
  agent: PersonIcon,
  chain: ChainIcon,
  internal: DotIcon,
};

export const spanKindVisuals: Record<SpanKind, SpanKindVisual> = {
  llm: {
    color: "#7C3AED",
    label: "LLM",
    Icon: SparkleIcon,
  },
  tool: {
    color: "#06B6D4",
    label: "Tool",
    Icon: WrenchIcon,
  },
  agent: {
    color: "#F59E0B",
    label: "Agent",
    Icon: PersonIcon,
  },
  chain: {
    color: "#22D3EE",
    label: "Chain",
    Icon: ChainIcon,
  },
  internal: {
    color: "#8B8BA7",
    label: "Internal",
    Icon: DotIcon,
  },
};

// ── Convenience Accessors ──────────────────────────────────────────

export function getSpanKindColor(kind: SpanKind): string {
  return spanKindVisuals[kind].color;
}

export function getSpanKindLabel(kind: SpanKind): string {
  return spanKindVisuals[kind].label;
}

export function getSpanKindIcon(kind: SpanKind): React.ComponentType {
  return ICONS[kind];
}

// ── Combined Icon Component ────────────────────────────────────────

export function SpanKindIcon({ kind }: { kind: SpanKind }): React.ReactElement {
  const Icon = ICONS[kind];
  return <Icon />;
}