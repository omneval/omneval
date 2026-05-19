/**
 * Shared formatting utilities for the Omneval UI.
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
 * Returns `< 1ms` for sub-millisecond durations, `Xms` for
 * millisecond ranges, and `X.Xs` for second ranges.
 */
export function formatDuration(start: string, end: string): string {
  if (!start || !end) return "N/A";
  const ms = new Date(end).getTime() - new Date(start).getTime();
  if (ms < 0) return "0ms";
  if (ms < 1) return "< 1ms";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

/**
 * Truncate a string to a maximum length, appending an ellipsis.
 */
export function truncate(str: string | undefined, len: number): string {
  if (!str) return "—";
  return str.length > len ? str.slice(0, len) + "…" : str;
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

// ── Chat message types ─────────────────────────────────────────────

export interface ChatTurn {
  role: string;
  content: string;
}

/**
 * Parse a string into an array of chat turns (role + content).
 * Handles JSON arrays of {role, content} objects.
 * Returns null if the input is not a chat structure.
 */
export function parseChatTurns(raw: string): ChatTurn[] | null {
  if (!raw) return null;
  // JSON array of {role, content} objects
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed) && parsed.length > 0) {
      const first = parsed[0] as Record<string, unknown>;
      if (typeof first?.content === "string" && typeof first?.role === "string") {
        return (parsed as Record<string, unknown>[])
          .filter((m) => typeof m?.content === "string" && typeof m?.role === "string")
          .map((m) => ({ role: m.role as string, content: m.content as string }));
      }
    }
  } catch {
    // not JSON
  }
  // Go-map format: [map[content:... role:...] ...] — Go's fmt.Sprint of []map[string]string
  // Keys are alphabetically sorted by Go, so content always precedes role.
  if (raw.includes("map[") && raw.includes("content:") && raw.includes("role:")) {
    const pairs = [...raw.matchAll(/map\[content:(.*?)\s+role:(\w+)\]/g)];
    if (pairs.length > 0) {
      return pairs.map((m) => ({ content: m[1].trim(), role: m[2].trim() }));
    }
  }
  return null;
}

/**
 * Extract readable text from a chat-message array (JSON or Go-map format).
 * Returns null when the input is not a recognised chat structure.
 */
function extractChatPreview(raw: string): string | null {
  // JSON array of {role, content} objects.
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed) && parsed.length > 0) {
      const first = parsed[0] as Record<string, unknown>;
      if (typeof first?.content === "string" && typeof first?.role === "string") {
        return (parsed as Record<string, unknown>[])
          .filter((m) => typeof m?.content === "string")
          .map((m) => `${m.role as string}: ${m.content as string}`)
          .join(" | ");
      }
    }
  } catch {
    // not JSON — fall through
  }

  // Go-map format: [map[content:... role:...] ...]
  // Extract each `content:VALUE` segment (value ends at next key: or closing bracket).
  if (raw.includes("map[") && raw.includes("content:")) {
    const matches = [...raw.matchAll(/content:([^\]]*?)(?:\s+\w+:|])/g)];
    if (matches.length > 0) {
      return matches.map((m) => m[1].trim()).filter(Boolean).join(" | ");
    }
    const cleaned = raw
      .replace(/\[map\[/g, "")
      .replace(/map\[/g, "")
      .replace(/\]\]/g, "")
      .replace(/\]/g, "")
      .trim();
    return cleaned || null;
  }

  return null;
}

/**
 * Produce a clean, truncated JSON preview suitable for display in table cells.
 * Chat message arrays (JSON or Go-map) render as "role: content" text.
 * Other JSON is compacted; non-JSON strings are truncated as-is.
 */
export function formatJsonPreview(json: string, maxLen = 60): string {
  if (!json) return "—";

  const chat = extractChatPreview(json);
  if (chat !== null) {
    if (chat.length <= maxLen) return chat;
    return chat.slice(0, maxLen) + "…";
  }

  try {
    const obj = JSON.parse(json);
    const compact = JSON.stringify(obj);
    if (compact.length <= maxLen) return compact;
    return compact.slice(0, maxLen) + "…";
  } catch {
    const cleaned = json.trim();
    if (cleaned.length <= maxLen) return cleaned;
    return cleaned.slice(0, maxLen) + "…";
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
