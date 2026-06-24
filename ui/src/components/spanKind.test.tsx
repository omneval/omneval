import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { SpanKindIcon, KIND_COLOR_MAP } from "@/components/spanKind";
import { colors } from "@/theme";

/** Normalise a CSS colour value to hex (#RRGGBB) for comparison. */
function toHex(cssColor: string): string {
  if (cssColor.startsWith("#")) return cssColor.toLowerCase();
  // rgb(r, g, b) → #rrggbb
  const m = cssColor.match(
    /rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/,
  );
  if (!m) return cssColor;
  const [_, r, g, b] = m.map(Number);
  return (
    "#" +
    [r, g, b]
      .map((c) => c.toString(16).padStart(2, "0"))
      .join("")
  );
}

// ── KIND_COLOR_MAP constants ────────────────────────────────────

describe("KIND_COLOR_MAP", () => {
  it("maps 'llm' to the emberFlare accent", () => {
    expect(KIND_COLOR_MAP.llm).toBe(colors.accents.emberFlare);
  });

  it("maps 'tool' to the softGlow accent", () => {
    expect(KIND_COLOR_MAP.tool).toBe(colors.accents.softGlow);
  });

  it("maps 'agent' to the flicker accent", () => {
    expect(KIND_COLOR_MAP.agent).toBe(colors.accents.flicker);
  });

  it("maps 'chain' to a blue tone", () => {
    expect(KIND_COLOR_MAP.chain).toBe("#60a5fa");
  });

  it("maps 'internal' to ashGrey", () => {
    expect(KIND_COLOR_MAP.internal).toBe(colors.typography.ashGrey);
  });

  it("returns undefined for unknown kinds", () => {
    expect(KIND_COLOR_MAP["unknown"]).toBeUndefined();
  });
});

// ── SpanKindIcon component ──────────────────────────────────────

describe("SpanKindIcon", () => {
  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: [
            {
              span_id: "root-1",
              trace_id: "trace-a",
              parent_id: "",
              project_id: "test-project",
              name: "main-trace",
              kind: "chain",
              start_time: "2025-01-15T10:00:00Z",
              end_time: "2025-01-15T10:00:30Z",
              cost_usd: 0.11,
              input_tokens: 210,
              output_tokens: 405,
            },
          ],
          next: "",
          limit: 25,
        }),
    } as Response);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function findDot(): HTMLElement {
    const dots = document.querySelectorAll('[aria-hidden="true"].w-2.h-2');
    expect(dots.length).toBeGreaterThan(0);
    return dots[0] as HTMLElement;
  }

  it("renders a dot with the correct color for the given kind", async () => {
    render(<SpanKindIcon kind="llm" />);
    const dot = await waitFor(findDot);
    expect(toHex(dot.style.backgroundColor)).toBe(
      colors.accents.emberFlare.toLowerCase(),
    );
  });

  it("renders a dot with the correct color for 'agent' kind", async () => {
    render(<SpanKindIcon kind="agent" />);
    const dot = await waitFor(findDot);
    expect(toHex(dot.style.backgroundColor)).toBe(
      colors.accents.flicker.toLowerCase(),
    );
  });

  it("renders a dot with the correct color for 'tool' kind", async () => {
    render(<SpanKindIcon kind="tool" />);
    const dot = await waitFor(findDot);
    expect(toHex(dot.style.backgroundColor)).toBe(
      colors.accents.softGlow.toLowerCase(),
    );
  });

  it("renders a dot with the correct color for 'chain' kind", async () => {
    render(<SpanKindIcon kind="chain" />);
    const dot = await waitFor(findDot);
    expect(toHex(dot.style.backgroundColor)).toBe("#60a5fa");
  });

  it("falls back to caveWall for unknown kinds", async () => {
    render(<SpanKindIcon kind="mystery" />);
    const dot = await waitFor(findDot);
    expect(toHex(dot.style.backgroundColor)).toBe(
      colors.backgrounds.caveWall.toLowerCase(),
    );
  });

  it("renders an optional label when label is true", async () => {
    render(<SpanKindIcon kind="llm" label={true} />);
    await waitFor(() => {
      expect(screen.getByText("LLM")).toBeInTheDocument();
    });
  });

  it("uses labelOverride when provided", async () => {
    render(<SpanKindIcon kind="llm" label={true} labelOverride="GEN" />);
    await waitFor(() => {
      expect(screen.getByText("GEN")).toBeInTheDocument();
    });
  });

  it("does not render a label when label is false", async () => {
    render(<SpanKindIcon kind="llm" label={false} />);
    await waitFor(() => {
      expect(screen.queryByText("LLM")).not.toBeInTheDocument();
    });
  });

  it("applies the title tooltip attribute", async () => {
    render(<SpanKindIcon kind="llm" />);
    await waitFor(() => {
      const spans = document.querySelectorAll('span[title="llm"]');
      expect(spans.length).toBeGreaterThan(0);
    });
  });
});