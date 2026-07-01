import { describe, it, expect } from "vitest";
import {
  formatTime,
  formatTimeWithYear,
  formatDuration,
  formatDurationMs,
  formatCost,
  safeExtractInputOutput,
  formatJsonPreview,
  truncate,
  getToolSummary,
} from "@/utils/formatters";

// ── formatTime ─────────────────────────────────────────────────────

describe("formatTime", () => {
  it("returns N/A for empty string", () => {
    expect(formatTime("")).toBe("N/A");
  });

  it("returns N/A for null-like input", () => {
    expect(formatTime("")).toBe("N/A");
  });

  it("formats a recent ISO timestamp", () => {
    const d = new Date();
    const iso = d.toISOString();
    const result = formatTime(iso);
    // Should contain month abbreviation
    expect(result).not.toBe("N/A");
    expect(result.length).toBeGreaterThan(3);
  });

  it("formats timestamps from previous year WITHOUT year", () => {
    // Jan 15, 2025 — if we run this test in 2026, this is a previous-year date
    const iso = "2025-01-15T14:30:00Z";
    const result = formatTime(iso);
    expect(result).not.toBe("N/A");
    // Old formatTime does NOT include year
    expect(result).not.toMatch(/\d{4}/);
  });
});

// ── formatTimeWithYear ─────────────────────────────────────────────

describe("formatTimeWithYear", () => {
  it("returns N/A for empty string", () => {
    expect(formatTimeWithYear("")).toBe("N/A");
  });

  it("includes year for dates from previous calendar year", () => {
    const iso = "2025-01-15T14:30:00Z";
    const result = formatTimeWithYear(iso);
    expect(result).toContain("2025");
  });

  it("includes year for dates from far in the past", () => {
    const iso = "2020-06-15T10:00:00Z";
    const result = formatTimeWithYear(iso);
    expect(result).toContain("2020");
  });

  it("formats current-year date without showing year", () => {
    const now = new Date();
    const iso = now.toISOString();
    const result = formatTimeWithYear(iso);
    expect(result).not.toBe("N/A");
    // Current year date should not show year (more compact)
    const currentYear = now.getFullYear().toString();
    expect(result).not.toContain(currentYear);
  });

  it("formats same-year date compactly", () => {
    const now = new Date();
    const sameYear = new Date(now.getFullYear(), 0, 1).toISOString();
    const result = formatTimeWithYear(sameYear);
    expect(result).not.toBe("N/A");
    const currentYear = now.getFullYear().toString();
    expect(result).not.toContain(currentYear);
  });
});

// ── safeExtractInputOutput ─────────────────────────────────────────

describe("safeExtractInputOutput", () => {
  it("extracts content from valid JSON array", () => {
    const json = JSON.stringify([
      { role: "user", content: "Hello world" },
    ]);
    expect(safeExtractInputOutput(json)).toBe("Hello world");
  });

  it("returns raw string when not valid JSON", () => {
    const raw = "[map[content:User asked role:user]]";
    expect(safeExtractInputOutput(raw)).toBe(raw);
  });

  it("returns raw string when array element has no content field", () => {
    const json = JSON.stringify([{ type: "text" }]);
    expect(safeExtractInputOutput(json)).toBe(json);
  });
});

// ── formatJsonPreview ──────────────────────────────────────────────

describe("formatJsonPreview", () => {
  it("returns truncated readable JSON for valid JSON input", () => {
    const json = JSON.stringify({
      role: "user",
      content: "This is a longer message that should be truncated at the max length",
    });
    const result = formatJsonPreview(json, 50);
    expect(result).not.toBe(json);
    expect(result.length).toBeLessThanOrEqual(50 + 3); // +3 for "..."
  });

  it("returns a clean preview, not raw Go map format", () => {
    const goMap = "[map[content:User asked role:user]]";
    const result = formatJsonPreview(goMap, 50);
    // Should not contain Go map syntax
    expect(result).not.toContain("map[");
    // Should be a cleaned-up version
    expect(result.length).toBeGreaterThan(0);
  });

  it("returns full string when shorter than max length", () => {
    const json = JSON.stringify({ x: 1 });
    const result = formatJsonPreview(json, 100);
    expect(result).not.toContain("...");
  });

  it("returns truncated string for very long JSON", () => {
    const long = JSON.stringify({ data: "x".repeat(500) });
    const result = formatJsonPreview(long, 40);
    expect(result.length).toBeLessThanOrEqual(43); // 40 + "..."
  });
});

// ── truncate ───────────────────────────────────────────────────────

describe("truncate", () => {
  it("returns em dash for undefined", () => {
    expect(truncate(undefined, 10)).toBe("\u2014");
  });

  it("returns full string when within limit", () => {
    expect(truncate("hello", 10)).toBe("hello");
  });

  it("truncates and adds ellipsis when over limit", () => {
    const result = truncate("hello world", 8);
    // slice(0, 8) + "\u2026" = 9 chars total
    expect(result.length).toBe(9);
    expect(result.endsWith("…")).toBe(true);
  });

  it("handles zero length limit", () => {
    expect(truncate("hello", 0)).toBe("…");
  });
});

// ── totalTokens ────────────────────────────────────────────────────

import { totalTokens } from "@/utils/formatters";

describe("totalTokens", () => {
  it("sums positive input and output tokens", () => {
    expect(totalTokens({ input_tokens: 100, output_tokens: 200 })).toBe(300);
  });

  it("returns 0 when both token counts are 0", () => {
    expect(totalTokens({ input_tokens: 0, output_tokens: 0 })).toBe(0);
  });

  it("clamps -1 sentinel values to 0 so the total is never negative", () => {
    expect(totalTokens({ input_tokens: -1, output_tokens: -1 })).toBe(0);
  });

  it("clamps only the negative field, keeps positive field intact", () => {
    expect(totalTokens({ input_tokens: -1, output_tokens: 50 })).toBe(50);
    expect(totalTokens({ input_tokens: 100, output_tokens: -1 })).toBe(100);
  });
});

// ── formatDuration ─────────────────────────────────────────────────

describe("formatDuration", () => {
  it("returns < 1ms for identical start and end times (0ms)", () => {
    const iso = "2025-01-15T10:00:00.000Z";
    expect(formatDuration(iso, iso)).toBe("< 1ms");
  });

  it("returns < 1ms for sub-millisecond durations (999µs)", () => {
    const start = "2025-01-15T10:00:00.000Z";
    const end = "2025-01-15T10:00:00.000999Z";
    expect(formatDuration(start, end)).toBe("< 1ms");
  });

  it("returns Xms for durations >= 1ms and < 1000ms", () => {
    const start = "2025-01-15T10:00:00.001Z";
    const end = "2025-01-15T10:00:00.002Z";
    expect(formatDuration(start, end)).toBe("1ms");
  });

  it("returns Xms for 500ms duration", () => {
    const start = "2025-01-15T10:00:00.000Z";
    const end = "2025-01-15T10:00:00.500Z";
    expect(formatDuration(start, end)).toBe("500ms");
  });

  it("returns seconds with one decimal for durations >= 1000ms", () => {
    const start = "2025-01-15T10:00:00.000Z";
    const end = "2025-01-15T10:00:01.000Z";
    expect(formatDuration(start, end)).toBe("1.0s");
  });

  it("returns 5.2s for 5200ms duration", () => {
    const start = "2025-01-15T10:00:00.000Z";
    const end = "2025-01-15T10:00:05.200Z";
    expect(formatDuration(start, end)).toBe("5.2s");
  });

  it("returns seconds with one decimal for 1.5s", () => {
    const start = "2025-01-15T10:00:00.000Z";
    const end = "2025-01-15T10:00:01.500Z";
    expect(formatDuration(start, end)).toBe("1.5s");
  });

  it("handles negative duration gracefully", () => {
    const start = "2025-01-15T10:00:01.000Z";
    const end = "2025-01-15T10:00:00.000Z";
    expect(formatDuration(start, end)).toBe("0ms");
  });

  it("handles empty string inputs gracefully", () => {
    expect(formatDuration("", "")).toBe("N/A");
  });

  it("handles real-world LLM span durations", () => {
    // LLM call taking 1234ms
    const start = "2025-01-15T10:00:00.000Z";
    const end = "2025-01-15T10:00:01.234Z";
    expect(formatDuration(start, end)).toBe("1.2s");
  });
});

// ── formatCost ────────────────────────────────────────────────────

describe("formatCost", () => {
  it("formats $0.00 for zero cost", () => {
    expect(formatCost(0)).toBe("$0.00");
  });

  it("formats cents with two decimals", () => {
    expect(formatCost(0.01)).toBe("$0.01");
    expect(formatCost(0.99)).toBe("$0.99");
  });

  it("formats sub-penny values with three significant decimals", () => {
    expect(formatCost(0.003)).toBe("$0.003");
    expect(formatCost(0.009)).toBe("$0.009");
  });

  it("formats pennies-to-dollars with three significant decimals below $1", () => {
    expect(formatCost(0.031)).toBe("$0.031");
    expect(formatCost(0.123)).toBe("$0.123");
  });

  it("drops trailing zeros above $1", () => {
    expect(formatCost(1.50)).toBe("$1.5");
    expect(formatCost(10.00)).toBe("$10");
    expect(formatCost(100.50)).toBe("$100.5");
  });

  it("formats larger values sensibly", () => {
    expect(formatCost(2.505)).toBe("$2.51");
    expect(formatCost(100)).toBe("$100");
    expect(formatCost(12.3456)).toBe("$12.35");
  });

  it("returns 'unpriced' when isUnpriced is true", () => {
    expect(formatCost(0, true)).toBe("unpriced");
    expect(formatCost(5, true)).toBe("unpriced");
  });

  it("handles very small costs with up to 4 decimals", () => {
    expect(formatCost(0.0001)).toBe("$0.0001");
    expect(formatCost(0.0005)).toBe("$0.0005");
    // Values below 0.00005 round to $0.00 (below our precision)
    expect(formatCost(0.00001)).toBe("$0.00");
    expect(formatCost(0.000015)).toBe("$0.00");
  });

  it("handles large costs", () => {
    expect(formatCost(999.999)).toBe("$1,000");
    expect(formatCost(999.50)).toBe("$999.5");
    expect(formatCost(5000)).toBe("$5,000");
  });

  it("formats with commas for thousands", () => {
    expect(formatCost(1500.50)).toBe("$1,500.5");
  });
});

// ── formatDurationMs ──────────────────────────────────────────────

describe("formatDurationMs (human-friendly duration with minutes/hours)", () => {
  it("returns '< 1ms' for zero", () => {
    expect(formatDurationMs(0)).toBe("< 1ms");
  });

  it("returns 'Xms' for 1–999ms", () => {
    expect(formatDurationMs(1)).toBe("1ms");
    expect(formatDurationMs(847)).toBe("847ms");
    expect(formatDurationMs(999)).toBe("999ms");
  });

  it("rounds 999.5ms to 1000ms → '1s'", () => {
    expect(formatDurationMs(999)).toBe("999ms");
  });

  it("shows 'Xs' (no decimal) for clean seconds", () => {
    expect(formatDurationMs(1000)).toBe("1s");
    expect(formatDurationMs(2000)).toBe("2s");
  });

  it("shows 'X.Xs' (with decimal) for non-integer seconds in 1000–59999ms", () => {
    expect(formatDurationMs(4600)).toBe("4.6s");
    expect(formatDurationMs(12345)).toBe("12.3s");
  });

  it("shows 'Xs' (no decimal) for clean seconds in 1000–59999ms", () => {
    expect(formatDurationMs(2000)).toBe("2s");
    expect(formatDurationMs(30000)).toBe("30s");
  });

  it("returns '1m' for 60000ms", () => {
    expect(formatDurationMs(60000)).toBe("1m 0s");
  });

  it("returns 'Xm Ys' for 1–59 minutes with seconds", () => {
    expect(formatDurationMs(61000)).toBe("1m 1s");
    expect(formatDurationMs(120000)).toBe("2m 0s");
    expect(formatDurationMs(847000)).toBe("14m 7s");
  });

  it("returns 'Xh Ym' for exactly-on-the-minute hours", () => {
    expect(formatDurationMs(3600000)).toBe("1h 0m");
    expect(formatDurationMs(3660000)).toBe("1h 1m");
    expect(formatDurationMs(7200000)).toBe("2h 0m");
  });

  it("returns 'Xh Ym Zs' when seconds are non-zero with hours", () => {
    // 3661000ms = 3661s = 1h 1m 1s
    expect(formatDurationMs(3661000)).toBe("1h 1m 1s");
    // 3725000ms = 3725s = 1h 2m 5s
    expect(formatDurationMs(3725000)).toBe("1h 2m 5s");
  });

  it("handles the Langfuse example: 81s → '1m 21s'", () => {
    expect(formatDurationMs(81000)).toBe("1m 21s");
  });

  it("handles the issue example: 3699.5s → '1h 1m 40s'", () => {
    // 3699500ms = 3699.5s = 1h 1m 39.5s → rounds to 1h 1m 40s
    expect(formatDurationMs(3699500)).toBe("1h 1m 40s");
  });

  it("handles negative durations gracefully", () => {
    expect(formatDurationMs(-100)).toBe("0ms");
  });

  // ── Boundary tests for the issue ─────────────────────────────────
  it("converts 59.9s → '59.9s' not '1m' (boundary at 60s)", () => {
    // 59 900ms is 59.9s — still in the seconds bucket
    expect(formatDurationMs(59900)).toBe("59.9s");
  });

  it("converts 60s → '1m 0s' (enters minutes bucket)", () => {
    expect(formatDurationMs(60000)).toBe("1m 0s");
  });

  it("ensures no raw seconds > 120 appear (121s → '2m 1s')", () => {
    expect(formatDurationMs(121000)).toBe("2m 1s");
  });

  it("ensures no raw seconds > 120 appear (180s → '3m 0s')", () => {
    expect(formatDurationMs(180000)).toBe("3m 0s");
  });

  it("ensures no raw milliseconds > 10 000 appear (10000ms → '10s')", () => {
    expect(formatDurationMs(10000)).toBe("10s");
  });

  it("ensures no raw milliseconds > 10 000 appear (60000ms → '1m 0s')", () => {
    expect(formatDurationMs(60000)).toBe("1m 0s");
  });

  it("formats 0.5s → '500ms' (sub-second)", () => {
    expect(formatDurationMs(500)).toBe("500ms");
  });

  it("formats 100000ms → '1m 40s'", () => {
    expect(formatDurationMs(100000)).toBe("1m 40s");
  });

  it("formats 36000000ms → '10h 0m' (large duration)", () => {
    expect(formatDurationMs(36000000)).toBe("10h 0m");
  });
});

// ── getToolSummary ─────────────────────────────────────────────────

describe("getToolSummary", () => {
  it("returns null when both input and output are empty", () => {
    expect(getToolSummary("TerminalAction", undefined, undefined)).toBeNull();
    expect(getToolSummary("TerminalAction", "", "")).toBeNull();
  });

  it("summarizes a TerminalAction with command and output", () => {
    const input = JSON.stringify({ command: "ls -la /tmp" });
    const output = JSON.stringify({ output: "total 0\ndrwxr-xr-x  2 root root", exit_code: 0 });
    const summary = getToolSummary("TerminalAction", input, output);

    expect(summary).not.toBeNull();
    expect(summary!.title).toContain("ls -la /tmp");
    expect(summary!.detail).toContain("total 0");
  });

  it("summarizes a TerminalAction with stdout/stderr fields", () => {
    const input = JSON.stringify({ command: "npm test" });
    const output = JSON.stringify({ stdout: "All tests passed", stderr: "", exit_code: 0 });
    const summary = getToolSummary("TerminalAction", input, output);

    expect(summary!.title).toContain("npm test");
    expect(summary!.detail).toContain("All tests passed");
  });

  it("truncates long terminal output", () => {
    const input = JSON.stringify({ command: "cat bigfile" });
    const output = JSON.stringify({ output: "x".repeat(1000) });
    const summary = getToolSummary("TerminalAction", input, output);

    expect(summary!.detail.length).toBeLessThan(1000);
    expect(summary!.detail.endsWith("…")).toBe(true);
  });

  it("summarizes a FileEditorAction with path and diff", () => {
    const input = JSON.stringify({
      path: "/src/main.go",
      diff: "-old line\n+new line",
    });
    const output = JSON.stringify({ status: "ok" });
    const summary = getToolSummary("FileEditorAction", input, output);

    expect(summary).not.toBeNull();
    expect(summary!.title).toContain("/src/main.go");
    expect(summary!.detail).toContain("new line");
  });

  it("summarizes a FileEditorAction with file_path and content", () => {
    const input = JSON.stringify({
      file_path: "/src/app.py",
      content: "print('hello')",
    });
    const summary = getToolSummary("FileEditorAction", input, undefined);

    expect(summary!.title).toContain("/src/app.py");
    expect(summary!.detail).toContain("print('hello')");
  });

  it("surfaces ThinkAction reasoning text as output", () => {
    const output = "I should check the config file before editing it.";
    const summary = getToolSummary("ThinkAction", undefined, output);

    expect(summary).not.toBeNull();
    expect(summary!.detail).toContain("check the config file");
  });

  it("summarizes an InvokeSkillAction with skill name and args", () => {
    const input = JSON.stringify({ skill: "diagnose", args: { issue: "flaky test" } });
    const summary = getToolSummary("InvokeSkillAction", input, undefined);

    expect(summary).not.toBeNull();
    expect(summary!.title).toContain("diagnose");
  });

  it("falls back to a compact JSON preview for unrecognized actions", () => {
    const input = JSON.stringify({ foo: "bar", nested: { a: 1 } });
    const output = JSON.stringify({ result: "done" });
    const summary = getToolSummary("CustomWidgetAction", input, output);

    expect(summary).not.toBeNull();
    expect(summary!.title).toContain("foo");
    expect(summary!.detail).toContain("result");
  });

  it("falls back to plain text for non-JSON input/output", () => {
    const summary = getToolSummary("tool", "plain text input", "plain text output");

    expect(summary).not.toBeNull();
    expect(summary!.title).toContain("plain text input");
    expect(summary!.detail).toContain("plain text output");
  });
});
