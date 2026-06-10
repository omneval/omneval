import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import ConversationDetailPage, { extractTraceIO } from "./ConversationDetail";

const CONV_ID = "abc123def456abc123def456abc12345";

const mockDetail = {
  conversation_id: CONV_ID,
  traces: [
    {
      trace_id: "trace-1",
      root_span_name: "agent.step",
      root_span_kind: "agent",
      start_time: "2025-01-15T10:00:00Z",
      end_time: "2025-01-15T10:00:45Z",
      span_count: 4,
      cost_usd: 0.008,
      input_tokens: 2400,
      output_tokens: 640,
      model: "gpt-4o",
    },
    {
      trace_id: "trace-2",
      root_span_name: "agent.step",
      root_span_kind: "agent",
      start_time: "2025-01-15T10:01:00Z",
      end_time: "2025-01-15T10:01:30Z",
      span_count: 2,
      cost_usd: 0.004,
      input_tokens: 1200,
      output_tokens: 300,
      model: "gpt-4o",
    },
  ],
};

const llmInput = JSON.stringify([
  { role: "system", content: "Be helpful" },
  { role: "user", content: "first question" },
  { role: "user", content: "latest question" },
]);
const llmOutput = JSON.stringify([{ role: "assistant", content: "the answer" }]);

function mockTraceResponse(traceId: string) {
  return {
    trace_id: traceId,
    project_id: "test-project",
    root_span: {
      span_id: `${traceId}-root`,
      kind: "agent",
      children: [
        {
          span_id: `${traceId}-llm`,
          kind: "llm",
          input: llmInput,
          output: llmOutput,
        },
      ],
    },
  };
}

function mockFetch() {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
    const u = String(url);
    if (u.includes(`/api/v1/conversations/${CONV_ID}`)) {
      return {
        ok: true,
        json: () => Promise.resolve(mockDetail),
      } as Response;
    }
    const m = u.match(/\/api\/v1\/traces\/([^?]+)/);
    if (m) {
      return {
        ok: true,
        json: () => Promise.resolve(mockTraceResponse(m[1])),
      } as Response;
    }
    return { ok: false, status: 404, json: () => Promise.resolve({}) } as Response;
  });
}

function renderPage(overrides: Record<string, unknown> = {}) {
  return render(
    <ConversationDetailPage
      conversationId={CONV_ID}
      activeProject="test-project"
      onBack={() => {}}
      onNavigateToTrace={() => {}}
      onNavigateToTraceDetail={() => {}}
      {...overrides}
    />
  );
}

// Some Node versions ship an experimental (and non-functional without a
// flag) global localStorage that shadows jsdom's. Stub a real in-memory
// Storage so the persistence behavior is testable everywhere.
function stubLocalStorage() {
  const store = new Map<string, string>();
  const fake = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, String(v)),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
    key: (i: number) => [...store.keys()][i] ?? null,
    get length() {
      return store.size;
    },
  };
  Object.defineProperty(window, "localStorage", {
    value: fake,
    configurable: true,
  });
  return fake;
}

beforeEach(() => {
  stubLocalStorage();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ConversationDetailPage", () => {
  it("renders the full conversation id with the chronological trace list", async () => {
    mockFetch();
    renderPage();

    await waitFor(() => {
      expect(screen.getByText(CONV_ID)).toBeInTheDocument();
    });
    expect(screen.getAllByText("agent.step")).toHaveLength(2);
    expect(screen.getByText("2 traces")).toBeInTheDocument();
  });

  it("shows only the last input message when the toggle is off (default)", async () => {
    mockFetch();
    renderPage();

    await waitFor(() => {
      expect(screen.getAllByText(/latest question/).length).toBeGreaterThan(0);
    });
    // Earlier turns of the input array are hidden with the toggle off.
    expect(screen.queryByText(/first question/)).not.toBeInTheDocument();
    // Assistant output renders fully in both modes.
    expect(screen.getAllByText(/the answer/).length).toBeGreaterThan(0);
  });

  it("shows the full message history when the toggle is on, and persists it", async () => {
    mockFetch();
    renderPage();

    await waitFor(() => {
      expect(screen.getAllByText(/latest question/).length).toBeGreaterThan(0);
    });

    const toggle = screen.getByLabelText("Show full message history");
    await act(async () => {
      fireEvent.click(toggle);
    });

    expect(screen.getAllByText(/first question/).length).toBeGreaterThan(0);
    expect(window.localStorage.getItem("omneval_conv_show_full_history")).toBe("true");
  });

  it("navigates to the trace detail when a trace row is clicked", async () => {
    mockFetch();
    const onNavigateToTrace = vi.fn();
    const onNavigateToTraceDetail = vi.fn();
    renderPage({ onNavigateToTrace, onNavigateToTraceDetail });

    await waitFor(() => {
      expect(screen.getAllByText("agent.step")).toHaveLength(2);
    });

    await act(async () => {
      fireEvent.click(screen.getAllByText("agent.step")[0]);
    });

    expect(onNavigateToTrace).toHaveBeenCalledWith("trace-1");
    expect(onNavigateToTraceDetail).toHaveBeenCalled();
  });

  it("returns to the conversations tab via the back button", async () => {
    mockFetch();
    const onBack = vi.fn();
    renderPage({ onBack });

    await waitFor(() => {
      expect(screen.getByText("Conversations")).toBeInTheDocument();
    });
    await act(async () => {
      fireEvent.click(screen.getByText("Conversations"));
    });
    expect(onBack).toHaveBeenCalled();
  });

  it("shows an error state for a missing conversation", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: false,
      status: 404,
      json: () => Promise.resolve({ error: "conversation not found" }),
    } as Response);
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Conversation not found")).toBeInTheDocument();
    });
  });
});

describe("extractTraceIO", () => {
  it("prefers the deepest LLM span's input/output", () => {
    const io = extractTraceIO({
      span_id: "root",
      kind: "agent",
      input: "root-in",
      children: [
        { span_id: "a", kind: "llm", input: "llm-in", output: "llm-out" },
      ],
    });
    expect(io).toEqual({ input: "llm-in", output: "llm-out" });
  });

  it("falls back to the root span's own I/O when no LLM span exists", () => {
    const io = extractTraceIO({
      span_id: "root",
      kind: "chain",
      input: "root-in",
      output: "root-out",
    });
    expect(io).toEqual({ input: "root-in", output: "root-out" });
  });

  it("handles a null root", () => {
    expect(extractTraceIO(null)).toEqual({});
  });
});
