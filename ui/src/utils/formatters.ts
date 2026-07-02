/**
 * Shared formatting utilities for the Omneval UI.
 */

/**
 * Format an ISO timestamp to a human-readable locale string.
 * Dates from the current calendar year omit the year for compactness;
 * dates from other years include it.
 */
export function formatTime(iso: string): string {
  return formatTimeWithYear(iso);
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
 * Format a duration in milliseconds to a human-readable string:
 * `< 1ms`, `847ms`, `4.6s`, `1m 21s`, `1h 2m`. Raw seconds are never
 * shown at or above one minute, raw milliseconds never at or above
 * one second.
 */
export function formatDurationMs(ms: number): string {
  if (ms <= 0) return "0ms";
  if (ms < 1) return "< 1ms";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) {
    const rendered = (ms / 1000).toFixed(1);
    if (rendered !== "60.0") return `${rendered}s`;
    // 59.95s+ rounds up to 60.0 — fall through to the minute form.
  }
  if (ms < 3_600_000) {
    let minutes = Math.floor(ms / 60_000);
    let seconds = Math.round((ms % 60_000) / 1000);
    if (seconds === 60) {
      minutes += 1;
      seconds = 0;
    }
    if (minutes === 60) return "1h";
    return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;
  }
  const hours = Math.floor(ms / 3_600_000);
  const minutes = Math.floor((ms % 3_600_000) / 60_000);
  return minutes === 0 ? `${hours}h` : `${hours}h ${minutes}m`;
}

/**
 * Format a duration from two ISO timestamps (see formatDurationMs).
 */
export function formatDuration(start: string, end: string): string {
  if (!start || !end) return "N/A";
  return formatDurationMs(new Date(end).getTime() - new Date(start).getTime());
}

/**
 * Format a USD cost for display. Zero is a legitimate value (self-hosted
 * models priced at $0) and renders plainly as `$0.00`. Small costs keep
 * three decimals (`$0.031`), dollar-plus costs two (`$12.35`), and dust
 * below a tenth of a cent renders as `< $0.001`.
 */
export function formatCost(cost: number | null | undefined): string {
  if (cost == null || isNaN(cost) || cost <= 0) return "$0.00";
  if (cost >= 1) return `$${cost.toFixed(2)}`;
  if (cost < 0.001) return "< $0.001";
  const rendered = cost.toFixed(3);
  // Trim a trailing zero but keep at least two decimals ($0.50, $0.031).
  return `$${rendered.endsWith("0") ? rendered.slice(0, -1) : rendered}`;
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

// ── Tool / action span summaries ───────────────────────────────────

export interface ToolSummary {
  /** Short one-line summary (e.g. the command, file path, or skill name). */
  title: string;
  /** Truncated detail body (e.g. command output, diff/content preview, reasoning). */
  detail: string;
}

/**
 * Try to parse a string as JSON, returning the parsed object (if it is a
 * plain object) or null otherwise.
 */
function tryParseObject(raw: string | undefined): Record<string, unknown> | null {
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
  } catch {
    // not JSON
  }
  return null;
}

/**
 * Return the first string value found among the given keys of an object.
 */
function firstStringField(obj: Record<string, unknown> | null, keys: string[]): string | undefined {
  if (!obj) return undefined;
  for (const key of keys) {
    const val = obj[key];
    if (typeof val === "string" && val.length > 0) return val;
  }
  return undefined;
}

const TOOL_DETAIL_MAX_LEN = 500;

/**
 * Build a generic "tool call summary" for tool/action-kind spans, used in
 * the trace Tree view.
 *
 * Recognizes a handful of common agent-framework action shapes
 * (TerminalAction, FileEditorAction, InvokeSkillAction, ThinkAction) and
 * extracts a short title plus a detail body. Falls back to a compact JSON
 * (or plain-text) preview of input/output for anything else. Returns null
 * when there is no input or output to summarize.
 */
export function getToolSummary(
  actionName: string | undefined,
  input: string | undefined,
  output: string | undefined,
): ToolSummary | null {
  const hasInput = !!input && input.length > 0;
  const hasOutput = !!output && output.length > 0;
  if (!hasInput && !hasOutput) return null;

  const inputObj = tryParseObject(input);
  const outputObj = tryParseObject(output);
  const name = actionName || "";

  // TerminalAction: command in input, output/stdout(+stderr) in output.
  if (/terminal|shell|bash|exec/i.test(name) || firstStringField(inputObj, ["command", "cmd"])) {
    const command = firstStringField(inputObj, ["command", "cmd"]);
    if (command) {
      const stdout = firstStringField(outputObj, ["output", "stdout", "result"]);
      const stderr = firstStringField(outputObj, ["stderr"]);
      const combined = [stdout, stderr].filter(Boolean).join("\n");
      return {
        title: `$ ${command}`,
        detail: combined ? truncate(combined, TOOL_DETAIL_MAX_LEN) : "(no output)",
      };
    }
  }

  // FileEditorAction: file path + diff/content in input.
  if (/file|edit/i.test(name) || firstStringField(inputObj, ["path", "file_path", "filepath", "file"])) {
    const path = firstStringField(inputObj, ["path", "file_path", "filepath", "file"]);
    if (path) {
      const detail = firstStringField(inputObj, ["diff", "content", "new_content", "patch"]);
      return {
        title: path,
        detail: detail ? truncate(detail, TOOL_DETAIL_MAX_LEN) : "(no diff/content)",
      };
    }
  }

  // InvokeSkillAction: skill name (+ args) in input.
  if (/invokeskill|skill/i.test(name) || firstStringField(inputObj, ["skill", "skill_name"])) {
    const skill = firstStringField(inputObj, ["skill", "skill_name"]);
    if (skill) {
      const args = inputObj?.args ?? inputObj?.arguments;
      return {
        title: `skill: ${skill}`,
        detail: args !== undefined ? truncate(JSON.stringify(args), TOOL_DETAIL_MAX_LEN) : "(no args)",
      };
    }
  }

  // ThinkAction: reasoning text is typically the output.
  if (/think|reason/i.test(name) && hasOutput && !outputObj) {
    return {
      title: "thought",
      detail: truncate(output, TOOL_DETAIL_MAX_LEN),
    };
  }

  // Generic fallback: compact JSON (or plain text) preview of input/output.
  const title = hasInput ? formatJsonPreview(input!, 120) : "(no input)";
  const detail = hasOutput ? truncate(formatJsonPreview(output!, TOOL_DETAIL_MAX_LEN), TOOL_DETAIL_MAX_LEN) : "(no output)";
  return { title, detail };
}

/**
 * Format milliseconds to a human-readable string (see formatDurationMs).
 */
export function formatMs(ms: number): string {
  return formatDurationMs(ms);
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
 * Sentinel values of -1 (used when tokens are not reported) are clamped to 0
 * so the total is never negative.
 */
export function totalTokens(span: HasTokenCounts): number {
  const input = Math.max(0, span.input_tokens);
  const output = Math.max(0, span.output_tokens);
  return input + output;
}
