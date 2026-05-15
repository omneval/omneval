/**
 * Shared formatting utilities for the Lantern UI.
 */

/**
 * Format an ISO timestamp to a human-readable locale string (compact, no year).
 * For dates from the current calendar year, omits the year.
 */
export function formatTime(iso: string): string {
  if (!iso) return "N/A";
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * Format an ISO timestamp, including the year for dates from previous
 * calendar years so that cross-year dates are disambiguated.
 */
export function formatTimeWithYear(iso: string): string {
  if (!iso) return "N/A";
  const d = new Date(iso);
  const now = new Date();
  const sameYear = d.getFullYear() === now.getFullYear();
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: sameYear ? undefined : "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * Format a duration from two ISO timestamps.
 * Returns milliseconds as "Xms" or seconds as "X.XXs".
 */
export function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime();
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

/**
 * Truncate a string to a maximum length, appending an ellipsis.
 */
export function truncate(str: string | undefined, len: number): string {
  if (!str) return "\u2014";
  return str.length > len ? str.slice(0, len) + "\u2026" : str;
}

/**
 * Format a large number with SI suffixes (K, M).
 */
export function formatNumber(v: unknown): string {
  if (v == null) return "0";
  const num = typeof v === "string" ? parseFloat(v) : Number(v);
  if (isNaN(num)) return "0";
  if (num >= 1000000) return `${(num / 1000000).toFixed(1)}M`;
  if (num >= 1000) return `${(num / 1000).toFixed(1)}K`;
  return num.toFixed(2);
}

/**
 * Safely parse JSON and extract the first element's content field.
 * Returns the raw string if parsing fails.
 */
export function safeExtractInputOutput(json: string): string {
  try {
    const parsed = JSON.parse(json) as unknown[];
    const first = parsed[0] as Record<string, unknown> | undefined;
    return (typeof first?.content === "string"
      ? first.content
      : json);
  } catch {
    return json;
  }
}

/**
 * Produce a clean, truncated JSON preview suitable for display in table cells.
 * If the input is valid JSON it will be compacted; otherwise the raw string
 * is cleaned up (Go-map-like syntax becomes plain text) and truncated.
 */
export function formatJsonPreview(json: string, maxLen = 60): string {
  if (!json) return "\u2014";

  try {
    const obj = JSON.parse(json);
    const compact = JSON.stringify(obj);
    if (compact.length <= maxLen) return compact;
    return compact.slice(0, maxLen) + "\u2026";
  } catch {
    // Not valid JSON — sanitise and truncate
    // Remove Go-map artefacts like "map[content:x role:y]"
    let cleaned = json
      .replace(/\[map\[/g, "[")
      .replace(/map\[/g, "[")
      .replace(/\]\]/g, "]")
      .trim();
    if (cleaned.length <= maxLen) return cleaned;
    return cleaned.slice(0, maxLen) + "\u2026";
  }
}

/**
 * Format milliseconds to a human-readable string ("Xms" or "X.Xs").
 */
export function formatMs(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  return `${ms}ms`;
}

/**
 * Derive a human-readable time-range label from a start timestamp.
 */
export function timeRangeLabel(from: string): string {
  const now = new Date();
  const fromD = new Date(from);
  const diffHours = (now.getTime() - fromD.getTime()) / (1000 * 60 * 60);
  if (diffHours <= 1) return "Past hour";
  if (diffHours <= 24) return "Past 24 hours";
  if (diffHours <= 168) return "Past 7 days";
  return "Custom range";
}

// ── Span helpers ───────────────────────────────────────────────────

interface HasTokenCounts {
  input_tokens: number;
  output_tokens: number;
}

/**
 * Compute the total token count (input + output) from a span-like object.
 */
export function totalTokens(span: HasTokenCounts): number {
  return span.input_tokens + span.output_tokens;
}
