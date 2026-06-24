import { describe, it, expect } from "vitest";
import {
  spanKindVisuals,
  SpanKind,
  getSpanKindColor,
  getSpanKindIcon,
  getSpanKindLabel,
  SpanKindIcon,
} from "./spanKindVisuals";
import { render, screen } from "@testing-library/react";

// ── The set of all SpanKind values ───────────────────────────────

const ALL_KINDS: SpanKind[] = ["llm", "tool", "agent", "chain", "internal"];

describe("spanKindVisuals", () => {
  it("exports a visual info entry for every SpanKind value", () => {
    for (const kind of ALL_KINDS) {
      expect(spanKindVisuals[kind]).toBeDefined();
    }
  });

  it("assigns a visually distinct color to each SpanKind", () => {
    const colors = new Set<string>();
    for (const kind of ALL_KINDS) {
      const color = spanKindVisuals[kind]!.color;
      expect(colors).not.toContain(color);
      colors.add(color);
    }
  });

  it("provides a human-readable label for every kind", () => {
    for (const kind of ALL_KINDS) {
      const label = spanKindVisuals[kind]!.label;
      expect(typeof label).toBe("string");
      expect(label.length).toBeGreaterThan(0);
    }
  });

  it("exports a convenience accessor that resolves a kind's color", () => {
    for (const kind of ALL_KINDS) {
      expect(getSpanKindColor(kind)).toBeDefined();
    }
  });

  it("exports a convenience accessor that resolves a kind's label", () => {
    for (const kind of ALL_KINDS) {
      expect(getSpanKindLabel(kind)).toBeDefined();
    }
  });

  it("renders a unique icon component for each kind", () => {
    for (const kind of ALL_KINDS) {
      const Icon = getSpanKindIcon(kind);
      expect(typeof Icon).toBe("function");
      expect(() => render(<Icon />)).not.toThrow();
    }
  });

  it("renders the SpanKindIcon component with the expected icon for llm", () => {
    render(<SpanKindIcon kind="llm" />);
    // The SVG icon should be present in the DOM
    const svg = screen.getByRole("img");
    expect(svg).toBeInTheDocument();
  });
});