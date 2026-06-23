import { describe, it, expect } from "vitest";
import { extractSpanMessages } from "@/utils/spanMessages";

// ── OTLP gen_ai.prompt.N.* / gen_ai.completion.N.* shape ─────────

describe("OTLP gen_ai shape — basic conversation", () => {
  it("extracts prompt messages from gen_ai.prompt.N.role / gen_ai.prompt.N.content keys", () => {
    const attrs: Record<string, unknown> = {
      "gen_ai.prompt.0.role": "system",
      "gen_ai.prompt.0.content": "You are a helpful assistant.",
      "gen_ai.prompt.1.role": "user",
      "gen_ai.prompt.1.content": "Hello",
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages).not.toBeNull();
    expect(result.messages!.length).toBe(2);
    expect(result.messages![0]).toEqual({ role: "system", content: "You are a helpful assistant." });
    expect(result.messages![1]).toEqual({ role: "user", content: "Hello" });
    expect(result.toolCalls).toBeNull();
    expect(result.raw).toBe(attrs);
  });

  it("extracts completion messages from gen_ai.completion.N.* keys", () => {
    const attrs: Record<string, unknown> = {
      "gen_ai.completion.0.role": "assistant",
      "gen_ai.completion.0.content": "Hi there! How can I help you?",
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages).not.toBeNull();
    expect(result.messages!.length).toBe(1);
    expect(result.messages![0]).toEqual({ role: "assistant", content: "Hi there! How can I help you?" });
    expect(result.toolCalls).toBeNull();
  });

  it("combines prompt and completion into a single ordered message array", () => {
    const attrs: Record<string, unknown> = {
      "gen_ai.prompt.0.role": "user",
      "gen_ai.prompt.0.content": "What is 2+2?",
      "gen_ai.completion.0.role": "assistant",
      "gen_ai.completion.0.content": "2+2 equals 4.",
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages!.length).toBe(2);
    expect(result.messages![0].role).toBe("user");
    expect(result.messages![1].role).toBe("assistant");
  });

  it("handles a full conversation with multiple turns", () => {
    const attrs: Record<string, unknown> = {
      "gen_ai.prompt.0.role": "system",
      "gen_ai.prompt.0.content": "You are helpful.",
      "gen_ai.prompt.1.role": "user",
      "gen_ai.prompt.1.content": "Tell me a joke.",
      "gen_ai.completion.0.role": "assistant",
      "gen_ai.completion.0.content": "Why did the chicken cross the road? To get to the other side.",
      "gen_ai.prompt.2.role": "user",
      "gen_ai.prompt.2.content": "That was bad.",
      "gen_ai.completion.1.role": "assistant",
      "gen_ai.completion.1.content": "Sorry! Try again: What do you call a fake noodle? An impasta.",
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages!.length).toBe(5);
    expect(result.messages![0].role).toBe("system");
    expect(result.messages![1].role).toBe("user");
    expect(result.messages![2].role).toBe("user"); // gen_ai.prompt.2
    expect(result.messages![3].role).toBe("assistant"); // gen_ai.completion.0
    expect(result.messages![4].role).toBe("assistant"); // gen_ai.completion.1
  });

  it("ignores gaps in numbering — missing index is silently skipped", () => {
    const attrs: Record<string, unknown> = {
      "gen_ai.prompt.0.role": "user",
      "gen_ai.prompt.0.content": "Hello",
      "gen_ai.prompt.2.role": "user",
      "gen_ai.prompt.2.content": "Still here?",
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages!.length).toBe(2);
  });

  it("skips entries where either role or content is missing", () => {
    const attrs: Record<string, unknown> = {
      "gen_ai.prompt.0.role": "user",
      "gen_ai.prompt.0.content": "Hello",
      "gen_ai.prompt.1.role": "user",
      // gen_ai.prompt.1.content is missing
      "gen_ai.prompt.2.role": "assistant",
      "gen_ai.prompt.2.content": "Hi!",
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages!.length).toBe(2);
    expect(result.messages![0].role).toBe("user");
    expect(result.messages![1].role).toBe("assistant");
  });

  it("falls back to llm.prompt / llm.completion prefixes when gen_ai keys are absent", () => {
    const attrs: Record<string, unknown> = {
      "llm.prompt.0.role": "user",
      "llm.prompt.0.content": "Hello",
      "llm.completion.0.role": "assistant",
      "llm.completion.0.content": "Hi there!",
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages!.length).toBe(2);
    expect(result.messages![0].role).toBe("user");
    expect(result.messages![1].role).toBe("assistant");
  });
});

// ── Laminar (lmnr) instrumented shape ──────────────────────────────

describe("lmnr instrumented shape", () => {
  it("extracts messages from a JSON-encoded lmnr.prompt array", () => {
    const attrs: Record<string, unknown> = {
      "lmnr.prompt": JSON.stringify([
        { role: "system", content: "You are helpful." },
        { role: "user", content: "Hello" },
      ]),
      "lmnr.completion": JSON.stringify([
        { role: "assistant", content: "Hi! How can I help?" },
      ]),
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages).not.toBeNull();
    expect(result.messages!.length).toBe(3);
    expect(result.messages![0]).toEqual({ role: "system", content: "You are helpful." });
    expect(result.messages![1]).toEqual({ role: "user", content: "Hello" });
    expect(result.messages![2]).toEqual({ role: "assistant", content: "Hi! How can I help?" });
  });

  it("parses tool_calls inside an lmnr message", () => {
    const attrs: Record<string, unknown> = {
      "lmnr.prompt": JSON.stringify([
        { role: "user", content: "Check disk space." },
      ]),
      "lmnr.completion": JSON.stringify([
        {
          role: "assistant",
          content: "",
          tool_calls: [
            {
              id: "call_abc",
              type: "function",
              function: "check_disk_space",
              input: "{}",
            },
          ],
        },
      ]),
    };

    const result = extractSpanMessages(attrs);

    // Only the user message from prompt (assistant has no text content)
    expect(result.messages!).toHaveLength(1);
    expect(result.toolCalls).not.toBeNull();
    expect(result.toolCalls!.length).toBe(1);
    expect(result.toolCalls![0]).toEqual({
      id: "call_abc",
      type: "function",
      function: "check_disk_space",
      input: "{}",
    });
  });

  it("parses nested JSON tool-definition objects (not double-stringified)", () => {
    // The lmnr tool input may be a JSON-encoded string containing an object.
    // The extractor should preserve the string as-is (a single parse), not
    // wrap it again in JSON.stringify.
    const toolInput = JSON.stringify({ query: "SELECT 1" });

    const attrs: Record<string, unknown> = {
      "lmnr.prompt": JSON.stringify([
        { role: "user", content: "Run a query." },
      ]),
      "lmnr.completion": JSON.stringify([
        {
          role: "assistant",
          content: "",
          tool_calls: [
            {
              id: "call_xyz",
              type: "function",
              function: "sql_query",
              input: toolInput,
            },
          ],
        },
      ]),
    };

    const result = extractSpanMessages(attrs);

    expect(result.toolCalls!.length).toBe(1);
    expect(result.toolCalls![0].input).toBe(toolInput);
  });

  it("records tool call results from lmnr tool-role messages", () => {
    const attrs: Record<string, unknown> = {
      "lmnr.prompt": JSON.stringify([
        { role: "user", content: "Call the API." },
      ]),
      "lmnr.completion": JSON.stringify([
        {
          role: "assistant",
          content: "",
          tool_calls: [
            {
              id: "call_123",
              type: "function",
              function: "api_call",
              input: JSON.stringify({ endpoint: "/health" }),
            },
          ],
        },
        { role: "tool", content: JSON.stringify({ tool_call_id: "call_123", result: "ok" }) },
      ]),
    };

    const result = extractSpanMessages(attrs);

    expect(result.toolCalls!.length).toBe(1);
    expect(result.toolCalls![0].id).toBe("call_123");
    // The tool call should have its output set from the tool-role message
    expect(result.toolCalls![0].output).toBe(JSON.stringify({ tool_call_id: "call_123", result: "ok" }));
    expect(result.messages!.pop()).toEqual({ role: "tool", content: JSON.stringify({ tool_call_id: "call_123", result: "ok" }) });
  });

  it("extracts standalone tool_calls from lmnr.tool_calls", () => {
    const attrs: Record<string, unknown> = {
      "lmnr.prompt": JSON.stringify([{ role: "user", content: "Calculate." }]),
      "lmnr.tool_calls": JSON.stringify([
        {
          id: "standalone_1",
          type: "function",
          function: "calculator",
          input: JSON.stringify({ expr: "2 + 2" }),
          output: JSON.stringify({ result: 4 }),
        },
      ]),
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages).not.toBeNull();
    expect(result.messages!.length).toBe(1);
    expect(result.toolCalls).not.toBeNull();
    expect(result.toolCalls!.length).toBe(1);
    expect(result.toolCalls![0].id).toBe("standalone_1");
    expect(result.toolCalls![0].function).toBe("calculator");
    expect(result.toolCalls![0].output).toBe(JSON.stringify({ result: 4 }));
  });

  it("prefers lmnr shape over gen_ai numbered attributes when both present", () => {
    // lmnr takes priority; gen_ai numbered attributes should be ignored.
    const attrs: Record<string, unknown> = {
      "lmnr.prompt": JSON.stringify([{ role: "user", content: "lmnr message" }]),
      "gen_ai.prompt.0.role": "system",
      "gen_ai.prompt.0.content": "gen_ai message",
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages!.length).toBe(1);
    expect(result.messages![0].content).toBe("lmnr message");
  });

  it("returns null when only non-prompt/completion attributes are present", () => {
    const attrs: Record<string, unknown> = {
      "gen_ai.system.fingerprint": "fp_123",
      "gen_ai.request.model": "gpt-4",
      "gen_ai.request.max_tokens": 256,
      "gen_ai.usage.total_tokens": 50,
      "span.kind": "internal",
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages).toBeNull();
    expect(result.toolCalls).toBeNull();
    expect(result.raw).toBe(attrs);
  });

  it("returns null for a span with no prompt/completion attributes at all", () => {
    const attrs: Record<string, unknown> = {
      "span.kind": "tool",
      "tool.name": "calculator",
      "duration_ms": 42,
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages).toBeNull();
    expect(result.toolCalls).toBeNull();
    expect(result.raw).toBe(attrs);
  });

  it("returns messages but no toolCalls when no tool attributes are present", () => {
    const attrs: Record<string, unknown> = {
      "gen_ai.prompt.0.role": "user",
      "gen_ai.prompt.0.content": "Hello",
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages).not.toBeNull();
    expect(result.toolCalls).toBeNull();
  });

  it("returns null messages but toolCalls when only tool attributes are present", () => {
    const attrs: Record<string, unknown> = {
      "gen_ai.usage.tool_calls": JSON.stringify([
        { id: "tc_1", type: "function", function: "calc", input: "1+1" },
      ]),
    };

    const result = extractSpanMessages(attrs);

    expect(result.messages).toBeNull();
    expect(result.toolCalls).not.toBeNull();
    expect(result.toolCalls!.length).toBe(1);
  });
});