/**
 * Pure module that extracts structured chat messages and tool calls from a
 * span's Attributes map.  Recognised attribute patterns:
 *
 * 1. OTLP Translation shape (gen_ai.prompt.N.*, gen_ai.completion.N.*)
 * 2. LLM prefix shape  (llm.prompt.N.*, llm.completion.N.*)
 * 3. Laminar (lmnr) instrumented shape — nested JSON-encoded strings
 *    under keys like `lmnr.prompt`, `lmnr.completion`, `lmnr.tool_calls`.
 */

// ── Public types ───────────────────────────────────────────────────

export interface ChatMessage {
  role: string;
  content: string;
}

export interface ToolCall {
  id: string;
  type: string;
  function: string;
  input: string;
  output?: string;
}

export interface SpanMessages {
  /** Parsed chat messages, or null when no recognisable prompt/completion keys exist. */
  messages: ChatMessage[] | null;
  /** Parsed tool calls, or null when no tool-call attributes are present. */
  toolCalls: ToolCall[] | null;
  /** Reference to the original (unmodified) attributes map. */
  raw: Record<string, unknown>;
}

// ── Helpers ────────────────────────────────────────────────────────

/**
 * Try to parse a JSON string; return null on failure.
 */
function tryParseJson<T>(raw: string): T | null {
  try {
    return JSON.parse(raw) as T;
  } catch {
    return null;
  }
}

/**
 * Extract numbered attributes like `prefix.N.key` and build a message array.
 * Skips indices where either role or content is missing.
 */
function extractNumberedMessages(
  attrs: Record<string, unknown>,
  prefix: string,
): ChatMessage[] {
  // Collect all indices that appear under this prefix.
  const indices = new Set<number>();
  for (const key of Object.keys(attrs)) {
    const idx = matchNumberedKey(key, prefix);
    if (idx != null) indices.add(idx);
  }
  if (indices.size === 0) return [];

  const messages: ChatMessage[] = [];
  for (const i of [...indices].sort((a, b) => a - b)) {
    const role = attrs[`${prefix}.${i}.role`];
    const content = attrs[`${prefix}.${i}.content`];
    if (typeof role === "string" && typeof content === "string") {
      messages.push({ role, content });
    }
  }
  return messages;
}

/**
 * Match key `prefix.N.suffix` and return N.  Returns null on no match.
 */
function matchNumberedKey(key: string, prefix: string): number | null {
  const needle = `${prefix}.`;
  if (!key.startsWith(needle)) return null;
  const rest = key.slice(needle.length);
  const dot = rest.indexOf(".");
  if (dot === -1) return null;
  const numStr = rest.slice(0, dot);
  if (!/^\d+$/.test(numStr)) return null;
  return parseInt(numStr, 10);
}

/**
 * Build an ordered chat message array by merging prompt and completion
 * numbered attributes.  Prompt entries (user / system) come first, followed
 * by completion entries (assistant).  Tool-result messages are appended
 * after.
 */
function mergeMessageArrays(
  prompt: ChatMessage[],
  completion: ChatMessage[],
): ChatMessage[] {
  return [...prompt, ...completion];
}

// ── Laminar (lmnr) helpers ────────────────────────────────────────

interface LmnrMessage {
  role: string;
  content?: string;
  tool_calls?: Array<{
    id?: string;
    type?: string;
    function?: string;
    input?: string;
  }>;
}

interface LmnrToolCall {
  id?: string;
  type?: string;
  function?: string;
  input?: string | object;
  output?: string | object;
}

/**
 * Parse lmnr instrumented attributes.
 *
 * The lmnr (Laminar) SDK stores data as JSON-encoded strings under keys like:
 *   - `lmnr.prompt`       → JSON array of {role, content, tool_calls?}
 *   - `lmnr.completion`   → JSON array of {role, content, tool_calls?}
 *   - `lmnr.tool_calls`   → JSON array of tool call objects (standalone)
 */
function extractLmnrShape(
  attrs: Record<string, unknown>,
): { messages: ChatMessage[]; toolCalls: ToolCall[] } | null {
  const messages: ChatMessage[] = [];
  const toolCalls: ToolCall[] = [];

  // lmnr.prompt — JSON-encoded array of messages
  const promptRaw = attrs["lmnr.prompt"];
  if (promptRaw != null) {
    const parsed = tryParseLmnrMessageArray(promptRaw);
    if (parsed) {
      for (const msg of parsed) {
        if (msg.content != null && msg.content !== "") {
          messages.push({ role: msg.role, content: msg.content });
        }
        if (msg.tool_calls) {
          for (const tc of msg.tool_calls) {
            if (tc.function && (tc.input != null || tc.input !== "")) {
              toolCalls.push({
                id: tc.id ?? "",
                type: tc.type ?? "function",
                function: tc.function,
                input: typeof tc.input === "string" ? tc.input : JSON.stringify(tc.input),
              });
            }
          }
        }
      }
    }
  }

  // lmnr.completion — JSON-encoded array
  const completionRaw = attrs["lmnr.completion"];
  if (completionRaw != null) {
    const parsed = tryParseLmnrMessageArray(completionRaw);
    if (parsed) {
      for (const msg of parsed) {
        // Extract assistant text content
        if (msg.role === "assistant" && msg.content != null && msg.content !== "") {
          messages.push({ role: msg.role, content: msg.content });
        }
        // Extract tool_calls from assistant messages (may have empty content)
        if (msg.role === "assistant" && msg.tool_calls) {
          for (const tc of msg.tool_calls) {
            if (tc.function && (tc.input != null || tc.input !== "")) {
              toolCalls.push({
                id: tc.id ?? `lc_${Math.random().toString(36).slice(2, 9)}`,
                type: tc.type ?? "function",
                function: tc.function,
                input: typeof tc.input === "string" ? tc.input : JSON.stringify(tc.input),
              });
            }
          }
        }
        // Record tool-call results that may carry output
        if (msg.role === "tool" && msg.content != null && msg.content !== "") {
          messages.push({ role: "tool", content: msg.content });
          // Try to associate with a pending tool call by matching id
          const parsedContent = tryParseJson<{tool_call_id?: string}>(msg.content);
          if (parsedContent?.tool_call_id) {
            const tc = toolCalls.find((t) => t.id === parsedContent.tool_call_id);
            if (tc) tc.output = msg.content;
          }
        }
      }
    }
  }

  // Standalone lmnr.tool_calls array
  const toolCallsRaw = attrs["lmnr.tool_calls"];
  if (toolCallsRaw != null) {
    const parsed = tryParseLmnrToolCallArray(toolCallsRaw);
    if (parsed) {
      for (const tc of parsed) {
        if (tc.function && (tc.input != null || tc.input !== "")) {
          const inputVal = typeof tc.input === "string" ? tc.input : JSON.stringify(tc.input);
          const outputVal =
            tc.output != null
              ? typeof tc.output === "string"
                ? tc.output
                : JSON.stringify(tc.output)
              : undefined;
          toolCalls.push({
            id: tc.id ?? `lc_${Math.random().toString(36).slice(2, 9)}`,
            type: tc.type ?? "function",
            function: tc.function,
            input: inputVal,
            output: outputVal,
          });
        }
      }
    }
  }

  if (messages.length === 0 && toolCalls.length === 0) return null;
  return { messages, toolCalls };
}

function tryParseLmnrMessageArray(raw: unknown): LmnrMessage[] | null {
  if (typeof raw !== "string") return null;
  return tryParseJson<LmnrMessage[]>(raw);
}

function tryParseLmnrToolCallArray(raw: unknown): LmnrToolCall[] | null {
  if (typeof raw !== "string") return null;
  return tryParseJson<LmnrToolCall[]>(raw);
}

// ── Main extractor ─────────────────────────────────────────────────

/**
 * Extract structured chat messages and tool calls from a span's attributes.
 *
 * Returns `{ messages, toolCalls, raw }` where:
 *  - `messages` contains formatted chat messages (role + content), or null when
 *    no recognizable prompt / completion attribute keys are present.
 *  - `toolCalls` contains parsed tool call objects, or null when no tool call
 *    attributes are present.
 *  - `raw` is a reference to the original attributes map for the fallback view.
 */
export function extractSpanMessages(
  attrs: Record<string, unknown>,
): SpanMessages {
  let messages: ChatMessage[] | null = null;
  let toolCalls: ToolCall[] | null = null;

  // ── 1. Check lmnr shape first (most structured) ──────────────────
  const lmnr = extractLmnrShape(attrs);
  if (lmnr) {
    messages = lmnr.messages;
    toolCalls = lmnr.toolCalls;
  }

  // ── 2. Try OTLP Translation / LLM prefix numbered attributes ────
  if (messages === null) {
    const promptPrefixes = ["gen_ai.prompt", "llm.prompt"];
    const completionPrefixes = ["gen_ai.completion", "llm.completion"];

    let allPrompt: ChatMessage[] = [];
    let allCompletion: ChatMessage[] = [];

    for (const prefix of promptPrefixes) {
      const found = extractNumberedMessages(attrs, prefix);
      if (found.length > 0) allPrompt = allPrompt.concat(found);
    }
    for (const prefix of completionPrefixes) {
      const found = extractNumberedMessages(attrs, prefix);
      if (found.length > 0) allCompletion = allCompletion.concat(found);
    }

    if (allPrompt.length > 0 || allCompletion.length > 0) {
      messages = mergeMessageArrays(allPrompt, allCompletion);
    }
  }

  // ── 3. Look for tool call attributes ─────────────────────────────
  // gen_ai.usage.tool_calls is a JSON string of an array of tool call objects.
  // Also check lmnr.tool_calls via the lmnr path above.
  if (toolCalls === null) {
    const toolCallsAttr = attrs["gen_ai.usage.tool_calls"];
    if (typeof toolCallsAttr === "string") {
      const parsed = tryParseJson<LmnrToolCall[]>(toolCallsAttr);
      if (parsed && Array.isArray(parsed) && parsed.length > 0) {
        toolCalls = parsed.map((tc) => ({
          id: tc.id ?? `lc_${Math.random().toString(36).slice(2, 9)}`,
          type: tc.type ?? "function",
          function: tc.function ?? "",
          input:
            tc.input != null
              ? typeof tc.input === "string"
                ? tc.input
                : JSON.stringify(tc.input)
              : "",
        }));
      }
    }
  }

  // ── 4. Also check tool_call result attributes ────────────────────
  if (toolCalls === null) {
    // Some instrumentations put tool_call results directly.
    const toolCallAttr = attrs["tool_call"];
    if (typeof toolCallAttr === "string") {
      const parsed = tryParseJson<LmnrToolCall[]>(toolCallAttr);
      if (parsed && Array.isArray(parsed) && parsed.length > 0) {
        toolCalls = parsed.map((tc) => ({
          id: tc.id ?? `tc_${Math.random().toString(36).slice(2, 9)}`,
          type: tc.type ?? "function",
          function: tc.function ?? "",
          input:
            tc.input != null
              ? typeof tc.input === "string"
                ? tc.input
                : JSON.stringify(tc.input)
              : "",
        }));
      }
    }
  }

  return {
    messages,
    toolCalls,
    raw: attrs,
  };
}