/**
 * Lightweight LCS-based diff utility for the Prompt Registry version comparison view.
 *
 * Pure client-side — no external dependencies required.
 */

// ── Types ──────────────────────────────────────────────────────────

export type DiffLine =
  | { type: "added"; text: string }
  | { type: "removed"; text: string }
  | { type: "unchanged"; text: string }
  | { type: "header"; text: string };

export interface ModelConfigDiff {
  field: string;
  oldValue: string | number;
  newValue: string | number;
}

export interface PromptVersion {
  model: string;
  temperature: number;
  max_tokens: number;
  template?: string;
  version?: number;
}

// ── LCS-based line diff ───────────────────────────────────────────

/**
 * Compute a line-level diff between two texts using the classic
 * Longest Common Subsequence algorithm (O(n*m) space).
 *
 * Returns an array of `DiffLine` objects suitable for rendering
 * with CSS classes for added/removed/unchanged styling.
 */
export function diffText(oldText: string, newText: string): DiffLine[] {
  const oldLines = oldText.split("\n");
  const newLines = newText.split("\n");

  const m = oldLines.length;
  const n = newLines.length;

  // Build the LCS table
  const dp: number[][] = Array.from({ length: m + 1 }, () =>
    Array(n + 1).fill(0)
  );
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      if (oldLines[i - 1] === newLines[j - 1]) {
        dp[i][j] = dp[i - 1][j - 1] + 1;
      } else {
        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
      }
    }
  }

  // Backtrack to produce the diff
  let i = m;
  let j = n;
  const stack: DiffLine[] = [];

  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
      stack.push({ type: "unchanged", text: oldLines[i - 1] });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      stack.push({ type: "added", text: newLines[j - 1] });
      j--;
    } else {
      stack.push({ type: "removed", text: oldLines[i - 1] });
      i--;
    }
  }

  stack.reverse();

  // Wrap in header
  const headerText = `v${oldText ? "old" : "(empty)"} vs v${newText ? "new" : "(empty)"}`;

  return [{ type: "header", text: headerText }, ...stack];
}

// ── Model config diff ─────────────────────────────────────────────

/**
 * Compare two prompt version model configs and return a structured
 * list of differences for the comparison table below the text diff.
 *
 * Supports baseline comparison: if `oldConfig` is undefined, fields
 * show `"—" → actualValue` to indicate "no previous version".
 */
export function diffModelConfig(
  oldConfig: PromptVersion | undefined,
  newConfig: PromptVersion | undefined
): ModelConfigDiff[] {
  const diffs: ModelConfigDiff[] = [];

  const fields: { key: string; oldVal: string | number; newVal: string | number }[] = [
    { key: "model", oldVal: oldConfig?.model ?? "—", newVal: newConfig?.model ?? "—" },
    { key: "temperature", oldVal: oldConfig?.temperature ?? "—", newVal: newConfig?.temperature ?? "—" },
    { key: "max_tokens", oldVal: oldConfig?.max_tokens ?? "—", newVal: newConfig?.max_tokens ?? "—" },
  ];

  for (const f of fields) {
    if (f.oldVal !== f.newVal) {
      diffs.push({ field: f.key, oldValue: f.oldVal, newValue: f.newVal });
    }
  }

  return diffs;
}
