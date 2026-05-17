import { describe, it, expect } from "vitest";
import {
  formatTime,
  formatTimeWithYear,
  formatDuration,
  safeExtractInputOutput,
  formatJsonPreview,
  truncate,
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
