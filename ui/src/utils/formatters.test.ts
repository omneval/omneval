import { describe, it, expect } from "vitest";
import {
  formatTime,
  formatTimeWithYear,
  formatDuration,
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

  it("includes the year for timestamps from a previous year", () => {
    const iso = "2025-01-15T14:30:00Z";
    const result = formatTime(iso);
    expect(result).not.toBe("N/A");
    expect(result).toContain("2025");
  });

  it("omits the year for current-year timestamps", () => {
    const now = new Date();
    const result = formatTime(now.toISOString());
    expect(result).not.toContain(now.getFullYear().toString());
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
  it("returns 0ms for identical start and end times", () => {
    const iso = "2025-01-15T10:00:00.000Z";
    expect(formatDuration(iso, iso)).toBe("0ms");
  });

  it("returns 0ms for sub-millisecond durations (JS Date truncates to whole ms)", () => {
    const start = "2025-01-15T10:00:00.000Z";
    const end = "2025-01-15T10:00:00.000999Z";
    expect(formatDuration(start, end)).toBe("0ms");
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

// ── formatDurationMs ───────────────────────────────────────────────

import { formatDurationMs } from "@/utils/formatters";

describe("formatDurationMs", () => {
  it("renders exact zero as 0ms (chart axis origin)", () => {
    expect(formatDurationMs(0)).toBe("0ms");
  });

  it("renders sub-millisecond durations as < 1ms", () => {
    expect(formatDurationMs(0.4)).toBe("< 1ms");
  });

  it("renders whole milliseconds below one second", () => {
    expect(formatDurationMs(847)).toBe("847ms");
    expect(formatDurationMs(999)).toBe("999ms");
  });

  it("crosses the 1s boundary at 1000ms", () => {
    expect(formatDurationMs(1000)).toBe("1.0s");
  });

  it("renders seconds with one decimal below one minute", () => {
    expect(formatDurationMs(4600)).toBe("4.6s");
    expect(formatDurationMs(59_900)).toBe("59.9s");
  });

  it("crosses the 1m boundary just below 60s (59.96s → 1m)", () => {
    expect(formatDurationMs(59_960)).toBe("1m");
  });

  it("renders minutes and seconds below one hour", () => {
    expect(formatDurationMs(81_000)).toBe("1m 21s");
    expect(formatDurationMs(3_599_000)).toBe("59m 59s");
  });

  it("drops zero seconds in the minute range", () => {
    expect(formatDurationMs(120_000)).toBe("2m");
  });

  it("renders hours and minutes at one hour and above", () => {
    expect(formatDurationMs(3_600_000)).toBe("1h");
    expect(formatDurationMs(3_720_000)).toBe("1h 2m");
  });

  it("renders the issue's 3699.5s example as hours + minutes, never raw seconds", () => {
    expect(formatDurationMs(3_699_500)).toBe("1h 1m");
  });

  it("clamps negative durations to 0ms", () => {
    expect(formatDurationMs(-5)).toBe("0ms");
  });
});

// ── formatDuration (minute+ ranges) ────────────────────────────────

describe("formatDuration above one minute", () => {
  it("never renders raw seconds above 60s", () => {
    const start = "2025-01-15T10:00:00.000Z";
    const end = "2025-01-15T11:01:39.500Z"; // 3699.5s
    expect(formatDuration(start, end)).toBe("1h 1m");
  });
});

// ── formatCost ─────────────────────────────────────────────────────

import { formatCost } from "@/utils/formatters";

describe("formatCost", () => {
  it("renders zero plainly as $0.00 (priced-at-zero is a legitimate value)", () => {
    expect(formatCost(0)).toBe("$0.00");
  });

  it("renders small costs with enough precision", () => {
    expect(formatCost(0.031)).toBe("$0.031");
    expect(formatCost(0.0123)).toBe("$0.012");
  });

  it("trims trailing zeros but keeps at least two decimals", () => {
    expect(formatCost(0.5)).toBe("$0.50");
  });

  it("renders costs of a dollar or more with two decimals", () => {
    expect(formatCost(12.3456)).toBe("$12.35");
    expect(formatCost(1)).toBe("$1.00");
  });

  it("renders dust amounts as a floor", () => {
    expect(formatCost(0.0004)).toBe("< $0.001");
  });

  it("handles null/undefined/NaN gracefully", () => {
    expect(formatCost(undefined)).toBe("$0.00");
    expect(formatCost(null)).toBe("$0.00");
    expect(formatCost(NaN)).toBe("$0.00");
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

// ── formatStatus ───────────────────────────────────────────────────

import { formatStatus } from "@/utils/formatters";

describe("formatStatus", () => {
  it("renders UNSET as 'No status', not the raw enum", () => {
    expect(formatStatus("UNSET")).toBe("No status");
    expect(formatStatus("unset")).toBe("No status");
  });

  it("passes through other statuses unchanged", () => {
    expect(formatStatus("OK")).toBe("OK");
    expect(formatStatus("ERROR")).toBe("ERROR");
    expect(formatStatus("error")).toBe("error");
  });

  it("renders empty/undefined as an em dash", () => {
    expect(formatStatus("")).toBe("—");
    expect(formatStatus(undefined)).toBe("—");
  });
});
