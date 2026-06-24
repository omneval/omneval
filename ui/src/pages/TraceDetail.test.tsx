import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import TraceDetailPage, { computeWaterfallAxisDomain, type WaterfallEntry } from "./TraceDetail";
import { ToastProvider } from "@/components/Toast";
import { totalTokens, formatMs } from "@/utils/formatters";
import { SpanKind } from "@/modules/spanKindVisuals";

function renderWithProvider(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

// Helper to detect React hook order violations in console.error output.
// React prints hook order warnings with "hook" or a line number (e.g. "310").
function captureConsoleErrors() {
  const saved = console.error;
  let found: string | null = null;
  console.error = (...args: unknown[]) => {
    const msg = args.join(" ");
    if (msg.includes("hook") || msg.includes("310")) {
      found = msg;
    }
    saved(...args);
  };
  return { found, restore: () => { console.error = saved; } };
}

describe("hook ordering", () => {
  it("does not throw React hook order violation when traceId is empty (loading state)", () => {
    const { found, restore } = captureConsoleErrors();

    expect(() => {
      renderWithProvider(
        <TraceDetailPage
          traceId=""
          activeProject="test-project"
          onBack={() => {}}
        />
      );
    }).not.toThrow();

    expect(found).toBeNull();
    restore();
  });

  it("does not throw React hook order violation when trace is not found (error state)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: false,
      status: 404,
    } as Response);

    const { found, restore } = captureConsoleErrors();

    // This test still shows loading first, then error. The hook order
    // must be stable across both states.
    renderWithProvider(
      <TraceDetailPage
        traceId="nonexistent-trace"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await screen.findByText("Trace not found");

    expect(found).toBeNull();
    restore();
  });
});

describe("totalTokens", () => {
  const testCases: Array<{ inputTokens: number; outputTokens: number; expected: number }> = [
    { inputTokens: 100, outputTokens: 200, expected: 300 },
    { inputTokens: 0, outputTokens: 0, expected: 0 },
    { inputTokens: 1000, outputTokens: 500, expected: 1500 },
    { inputTokens: 1_000_000, outputTokens: 500_000, expected: 1_500_000 },
  ];

  it.each(testCases)(
    "returns $expected for inputTokens=$inputTokens, outputTokens=$outputTokens",
    ({ inputTokens, outputTokens, expected }) => {
      expect(totalTokens({
        input_tokens: inputTokens,
        output_tokens: outputTokens,
      })).toBe(expected);
    },
  );
});

describe("formatMs (milliseconds display)", () => {
  const testCases: Array<[number, string]> = [
    [0, "0ms"],
    [100, "100ms"],
    [999, "999ms"],
    [1000, "1.0s"],
    [1500, "1.5s"],
    [5000, "5.0s"],
    [12345, "12.3s"],
    [60000, "60.0s"],
  ];

  it.each(testCases)("formats %d ms as '%s'", (ms, expected) => {
    expect(formatMs(ms)).toBe(expected);
  });
});

// ── Helper to assert trace loaded ────────────────────────────────

function waitForTraceLoaded() {
  return waitFor(() => {
    expect(screen.getAllByText("main-trace").length).toBeGreaterThan(0);
  });
}

// ── Trace Detail Rendering ───────────────────────────────────────

const mockTraceData = {
  trace_id: "test-trace-123",
  project_id: "test-project",
  root_span: {
    span_id: "root-span",
    trace_id: "test-trace-123",
    parent_id: "",
    project_id: "test-project",
    name: "main-trace",
    kind: "chain",
    model: "gpt-4",
    start_time: "2025-01-15T10:00:00Z",
    end_time: "2025-01-15T10:00:30Z",
    cost_usd: 0.05,
    input_tokens: 100,
    output_tokens: 200,
    children: [
      {
        span_id: "child-span-1",
        trace_id: "test-trace-123",
        parent_id: "root-span",
        project_id: "test-project",
        name: "llm-call",
        kind: "llm",
        model: "gpt-4",
        start_time: "2025-01-15T10:00:01Z",
        end_time: "2025-01-15T10:00:10Z",
        cost_usd: 0.03,
        input_tokens: 50,
        output_tokens: 100,
      },
      {
        span_id: "child-span-2",
        trace_id: "test-trace-123",
        parent_id: "root-span",
        project_id: "test-project",
        name: "tool-use",
        kind: "tool",
        start_time: "2025-01-15T10:00:11Z",
        end_time: "2025-01-15T10:00:20Z",
        cost_usd: 0.01,
        input_tokens: 25,
        output_tokens: 50,
      },
    ],
  },
};

function mockFetchSuccess() {
  vi.spyOn(globalThis, "fetch").mockResolvedValue({
    ok: true,
    json: () => Promise.resolve(mockTraceData),
  } as Response);
}

describe("waterfall view renders with loaded trace data", () => {
  beforeEach(() => {
    mockFetchSuccess();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders the waterfall chart with span bars", async () => {
    renderWithProvider(
      <TraceDetailPage
        traceId="test-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitForTraceLoaded();

    expect(screen.getByText("llm-call")).toBeInTheDocument();
    expect(screen.getByText("tool-use")).toBeInTheDocument();
    expect(screen.getByText("Waterfall Timeline")).toBeInTheDocument();
  });

  it("renders the waterfall span list with count", async () => {
    renderWithProvider(
      <TraceDetailPage
        traceId="test-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitForTraceLoaded();
    expect(screen.getByText("Spans (3)")).toBeInTheDocument();
  });

  it("renders span kind badges", async () => {
    renderWithProvider(
      <TraceDetailPage
        traceId="test-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitForTraceLoaded();
    expect(screen.getByText("chain")).toBeInTheDocument();
  });
});

// ── Header pill token rollup (#137) ──────────────────────────────

describe("header pill token rollup", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows the trace-level token rollup, not just the root span's own tokens", async () => {
    // The root span (an orchestration span) has 0 tokens of its own; the
    // actual usage lives on descendant llm spans. The API response carries
    // a precomputed trace-level rollup.
    const tracePayload = {
      trace_id: "rollup-trace",
      project_id: "test-project",
      total_input_tokens: 35000,
      total_output_tokens: 11795,
      total_cost_usd: 0.6,
      root_span: {
        span_id: "root-span",
        trace_id: "rollup-trace",
        parent_id: "",
        project_id: "test-project",
        name: "conversation",
        kind: "chain",
        start_time: "2025-01-15T10:00:00Z",
        end_time: "2025-01-15T10:00:30Z",
        cost_usd: 0,
        input_tokens: 0,
        output_tokens: 0,
        children: [
          {
            span_id: "llm-span-1",
            trace_id: "rollup-trace",
            parent_id: "root-span",
            project_id: "test-project",
            name: "litellm.completion",
            kind: "llm",
            model: "gpt-4",
            start_time: "2025-01-15T10:00:01Z",
            end_time: "2025-01-15T10:00:10Z",
            cost_usd: 0.6,
            input_tokens: 35000,
            output_tokens: 11795,
          },
        ],
      },
    };

    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(tracePayload),
    } as Response);

    renderWithProvider(
      <TraceDetailPage
        traceId="rollup-trace"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitFor(() => {
      expect(screen.getAllByText("conversation").length).toBeGreaterThan(0);
    });

    expect(screen.getByText("46,795 tokens")).toBeInTheDocument();
  });
});

describe("tree view renders with loaded trace data", () => {
  beforeEach(() => {
    mockFetchSuccess();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("switches from waterfall to tree view", async () => {
    renderWithProvider(
      <TraceDetailPage
        traceId="test-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitForTraceLoaded();

    // Waterfall is the default view
    expect(screen.getByText("Waterfall Timeline")).toBeInTheDocument();

    const treeTab = screen.getByText("Tree");
    await act(async () => {
      fireEvent.click(treeTab);
    });

    // Tree view renders the same child spans
    expect(screen.getByText("llm-call")).toBeInTheDocument();
    expect(screen.getByText("tool-use")).toBeInTheDocument();
  });

  it("renders root span as the first item in the tree view", async () => {
    renderWithProvider(
      <TraceDetailPage
        traceId="test-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitForTraceLoaded();

    const treeTab = screen.getByText("Tree");
    await act(async () => {
      fireEvent.click(treeTab);
    });

    // Root span appears in info bar + tree = 2 occurrences
    expect(screen.getAllByText("main-trace").length).toBeGreaterThanOrEqual(2);
    // Child spans should also be visible
    expect(screen.getByText("llm-call")).toBeInTheDocument();
    expect(screen.getByText("tool-use")).toBeInTheDocument();
  });

  it("renders single-span traces in the tree view (not 'No child spans')", async () => {
    const singleSpanTrace = {
      trace_id: "single-trace",
      project_id: "test-project",
      root_span: {
        span_id: "root-span",
        trace_id: "single-trace",
        parent_id: "",
        project_id: "test-project",
        name: "single-span",
        kind: "llm",
        start_time: "2025-01-15T10:00:00Z",
        end_time: "2025-01-15T10:00:10Z",
        cost_usd: 0.01,
        input_tokens: 10,
        output_tokens: 20,
        children: [],
      },
    };

    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(singleSpanTrace),
    } as Response);

    renderWithProvider(
      <TraceDetailPage
        traceId="single-trace"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitFor(() => {
      const spans = screen.queryAllByText("single-span");
      expect(spans.length).toBeGreaterThan(0);
    });

    const treeTab = screen.getByText("Tree");
    await act(async () => {
      fireEvent.click(treeTab);
    });

    // Single-span trace shows root span in info bar + tree = 2 occurrences
    expect(screen.getAllByText("single-span").length).toBeGreaterThanOrEqual(2);
    // Should NOT show "No child spans"
    expect(screen.queryByText(/No child spans/)).not.toBeInTheDocument();
  });

  it("selects a span in the tree view and opens the detail panel", async () => {
    renderWithProvider(
      <TraceDetailPage
        traceId="test-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitForTraceLoaded();

    const treeTab = screen.getByText("Tree");
    await act(async () => {
      fireEvent.click(treeTab);
    });

    // Click on the detail toggle button for the "llm-call" span
    // The span row has a button with aria-label="Show details"
    const showDetailButtons = screen.getAllByLabelText("Show details");
    // First button is root span, second is first child (llm-call)
    expect(showDetailButtons.length).toBeGreaterThanOrEqual(2);
    const llmCallDetailBtn = showDetailButtons[1];

    await act(async () => {
      fireEvent.click(llmCallDetailBtn);
    });

    // After expanding, the detail panel should show the span's info
    // The expanded detail contains the span's model and token count
    const gpt4Elements = screen.getAllByText("gpt-4");
    expect(gpt4Elements.length).toBeGreaterThan(0);
  });
});

describe("back navigation from trace detail", () => {
  beforeEach(() => {
    mockFetchSuccess();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("calls onBack when Back button is clicked", async () => {
    const onBack = vi.fn();

    renderWithProvider(
      <TraceDetailPage
        traceId="test-trace-123"
        activeProject="test-project"
        onBack={onBack}
      />
    );

    await waitForTraceLoaded();

    const backButton = screen.getByText("Back").closest("button");
    backButton?.click();

    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it("shows breadcrumb with Traces link and trace ID", async () => {
    renderWithProvider(
      <TraceDetailPage
        traceId="test-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitForTraceLoaded();

    expect(screen.getByText("Traces")).toBeInTheDocument();
    expect(screen.getByText("Trace: test-tra…")).toBeInTheDocument();
  });
});

describe("span selection in waterfall", () => {
  beforeEach(() => {
    mockFetchSuccess();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("selects a span when clicked and shows detail panel", async () => {
    renderWithProvider(
      <TraceDetailPage
        traceId="test-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitForTraceLoaded();

    const initialLlmCalls = screen.getAllByText("llm-call").length;
    expect(initialLlmCalls).toBeGreaterThanOrEqual(1);

    const llmCallRow = screen.getByText("llm-call").closest('[role="button"]');
    expect(llmCallRow).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(llmCallRow!);
    });

    // After selection, "llm-call" appears in both the list and the detail panel header
    const llmCallElements = screen.getAllByText("llm-call");
    expect(llmCallElements.length).toBe(initialLlmCalls + 1);

    // The detail panel shows the span kind badge
    const llmBadges = screen.getAllByText("llm");
    expect(llmBadges.length).toBeGreaterThan(0);
  });
});

describe("trace root span info", () => {
  beforeEach(() => {
    mockFetchSuccess();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("displays root span metadata", async () => {
    renderWithProvider(
      <TraceDetailPage
        traceId="test-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitForTraceLoaded();

    // gpt-4 appears in root span info bar and in waterfall span list
    const gpt4Elements = screen.getAllByText("gpt-4");
    expect(gpt4Elements.length).toBeGreaterThan(0);
  });
});

describe("hook ordering with loaded data", () => {
  beforeEach(() => {
    mockFetchSuccess();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("does not throw React hook order violation when trace loads successfully", async () => {
    const { found, restore } = captureConsoleErrors();

    expect(() => {
      renderWithProvider(
        <TraceDetailPage
          traceId="test-trace-123"
          activeProject="test-project"
          onBack={() => {}}
        />
      );
    }).not.toThrow();

    await waitForTraceLoaded();

    expect(found).toBeNull();
    restore();
  });
});

describe("tool/action span details in tree view", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  const toolTraceData = {
    trace_id: "tool-trace-123",
    project_id: "test-project",
    root_span: {
      span_id: "root-span",
      trace_id: "tool-trace-123",
      parent_id: "",
      project_id: "test-project",
      name: "agent.step",
      kind: "agent",
      start_time: "2025-01-15T10:00:00Z",
      end_time: "2025-01-15T10:00:30Z",
      cost_usd: 0,
      input_tokens: 0,
      output_tokens: 0,
      children: [
        {
          span_id: "terminal-span",
          trace_id: "tool-trace-123",
          parent_id: "root-span",
          project_id: "test-project",
          name: "TerminalAction",
          kind: "tool",
          start_time: "2025-01-15T10:00:01Z",
          end_time: "2025-01-15T10:00:05Z",
          cost_usd: 0,
          input_tokens: 0,
          output_tokens: 0,
          input: JSON.stringify({ command: "npm test" }),
          output: JSON.stringify({ output: "All 44 tests passed", exit_code: 0 }),
        },
        {
          span_id: "fileeditor-span",
          trace_id: "tool-trace-123",
          parent_id: "root-span",
          project_id: "test-project",
          name: "FileEditorAction",
          kind: "tool",
          start_time: "2025-01-15T10:00:06Z",
          end_time: "2025-01-15T10:00:08Z",
          cost_usd: 0,
          input_tokens: 0,
          output_tokens: 0,
          input: JSON.stringify({ path: "/src/utils/formatters.ts", diff: "+export function getToolSummary() {}" }),
          output: JSON.stringify({ status: "ok" }),
        },
        {
          span_id: "generic-span",
          trace_id: "tool-trace-123",
          parent_id: "root-span",
          project_id: "test-project",
          name: "InvokeSkillAction",
          kind: "tool",
          start_time: "2025-01-15T10:00:09Z",
          end_time: "2025-01-15T10:00:10Z",
          cost_usd: 0,
          input_tokens: 0,
          output_tokens: 0,
          input: JSON.stringify({ skill: "diagnose", args: { issue: "flaky test" } }),
        },
      ],
    },
  };

  function mockToolFetchSuccess() {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(toolTraceData),
    } as Response);
  }

  it("shows a TerminalAction command summary inline in the tree row", async () => {
    mockToolFetchSuccess();

    renderWithProvider(
      <TraceDetailPage
        traceId="tool-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitFor(() => {
      expect(screen.getAllByText("agent.step").length).toBeGreaterThan(0);
    });

    const treeTab = screen.getByText("Tree");
    await act(async () => {
      fireEvent.click(treeTab);
    });

    expect(screen.getByText("TerminalAction")).toBeInTheDocument();
    expect(screen.getByText(/npm test/)).toBeInTheDocument();
  });

  it("shows the FileEditorAction file path inline and diff in expanded detail", async () => {
    mockToolFetchSuccess();

    renderWithProvider(
      <TraceDetailPage
        traceId="tool-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitFor(() => {
      expect(screen.getAllByText("agent.step").length).toBeGreaterThan(0);
    });

    const treeTab = screen.getByText("Tree");
    await act(async () => {
      fireEvent.click(treeTab);
    });

    expect(screen.getByText("FileEditorAction")).toBeInTheDocument();
    expect(screen.getByText(/\/src\/utils\/formatters\.ts/)).toBeInTheDocument();

    // Expand the FileEditorAction row to reveal the diff detail
    const showDetailButtons = screen.getAllByLabelText("Show details");
    // root, terminal, fileeditor, generic — fileeditor is index 2
    await act(async () => {
      fireEvent.click(showDetailButtons[2]);
    });

    expect(screen.getAllByText(/getToolSummary/).length).toBeGreaterThan(0);
  });

  it("falls back to a generic JSON preview for unrecognized action input", async () => {
    mockToolFetchSuccess();

    renderWithProvider(
      <TraceDetailPage
        traceId="tool-trace-123"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitFor(() => {
      expect(screen.getAllByText("agent.step").length).toBeGreaterThan(0);
    });

    const treeTab = screen.getByText("Tree");
    await act(async () => {
      fireEvent.click(treeTab);
    });

    expect(screen.getByText("InvokeSkillAction")).toBeInTheDocument();
    expect(screen.getByText(/diagnose/)).toBeInTheDocument();
  });
});

// ── Regression test for #268 ─────────────────────────────────────
// hasChatContent previously only checked extracted.messages, so a span
// whose attributes yield only toolCalls (no gen_ai prompt/completion text)
// never showed the Chat tab even though FormattedChatMessages can render
// tool calls on their own.

describe("Chat tab for spans with tool calls but no text messages", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  const toolCallsOnlyTraceData = {
    trace_id: "tool-calls-only-trace",
    project_id: "test-project",
    root_span: {
      span_id: "root-span",
      trace_id: "tool-calls-only-trace",
      parent_id: "",
      project_id: "test-project",
      name: "main-trace",
      kind: "chain",
      start_time: "2025-01-15T10:00:00Z",
      end_time: "2025-01-15T10:00:30Z",
      cost_usd: 0,
      input_tokens: 0,
      output_tokens: 0,
      children: [
        {
          span_id: "tool-call-span",
          trace_id: "tool-calls-only-trace",
          parent_id: "root-span",
          project_id: "test-project",
          name: "llm-tool-call",
          kind: "llm",
          start_time: "2025-01-15T10:00:01Z",
          end_time: "2025-01-15T10:00:05Z",
          cost_usd: 0,
          input_tokens: 0,
          output_tokens: 0,
          attributes: {
            "gen_ai.usage.tool_calls": JSON.stringify([
              { id: "tc_1", type: "function", function: "calculator", input: "1+1" },
            ]),
          },
        },
      ],
    },
  };

  function mockToolCallsOnlyFetchSuccess() {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(toolCallsOnlyTraceData),
    } as Response);
  }

  it("shows the Chat tab and renders tool calls for a span with toolCalls but no messages", async () => {
    mockToolCallsOnlyFetchSuccess();

    renderWithProvider(
      <TraceDetailPage
        traceId="tool-calls-only-trace"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await waitFor(() => {
      expect(screen.getAllByText("main-trace").length).toBeGreaterThan(0);
    });

    const spanRow = screen.getByText("llm-tool-call").closest('[role="button"]');
    expect(spanRow).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(spanRow!);
    });

    expect(screen.getByText("Chat")).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByText("Chat"));
    });

    expect(screen.getByText(/Tool Calls/)).toBeInTheDocument();
    expect(screen.getByText(/calculator/)).toBeInTheDocument();
  });
});

// ── Waterfall axis domain (#280) ─────────────────────────────────

describe("waterfall axis domain (#280)", () => {
  it("returns [0, maxStartPlusDuration] for a single 26.3s span (the live-bug scenario)", () => {
    // A real Trace where the single Span ran 26.3 seconds should render
    // an axis that goes 0ms–26.3s, NOT 0ms–4ms (the default Recharts
    // behaviour when domain is not set and start values cluster near 0).
    const entries: WaterfallEntry[] = [
      {
        name: "agent.run",
        spanId: "single-span",
        start: 0,
        duration: 26_300, // 26.3 seconds
        color: "#ff6b35",
        kind: "agent",
      },
    ];

    const domain = computeWaterfallAxisDomain(entries);
    expect(domain).toEqual([0, 26_300]);
  });

  it("handles an empty span list by returning [0, 0]", () => {
    const domain = computeWaterfallAxisDomain([]);
    expect(domain).toEqual([0, 0]);
  });

  it("computes the domain from the latest-span end when children extend beyond the root", () => {
    const entries: WaterfallEntry[] = [
      { name: "root", spanId: "r1", start: 0, duration: 5_000, color: "#aaa", kind: "chain" },
      { name: "fast child", spanId: "c1", start: 1_000, duration: 2_000, color: "#bbb", kind: "tool" },
      { name: "slow child", spanId: "c2", start: 4_500, duration: 3_000, color: "#ccc", kind: "llm" },
      // fast child ends at 3000, slow child ends at 7500 → domain = [0, 7500]
    ];

    const domain = computeWaterfallAxisDomain(entries);
    expect(domain).toEqual([0, 7_500]);
  });

  it("computes the correct domain for a long-running child after the root ended", () => {
    // Root span: 0-5s.  Child starts at 4s and runs for 20s (ends at 24s).
    // The axis must go to 24000, not just 5000.
    const entries: WaterfallEntry[] = [
      { name: "root", spanId: "r1", start: 0, duration: 5_000, color: "#aaa", kind: "chain" },
      { name: "network-io", spanId: "c1", start: 4_000, duration: 20_000, color: "#bbb", kind: "tool" },
    ];

    const domain = computeWaterfallAxisDomain(entries);
    expect(domain).toEqual([0, 24_000]);
  });
});

// ── SpanKind Visual Module Integration ─────────────────────────────

describe("SpanKind visual module in TraceDetail integration", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Helper: render a trace with spans of different Kinds
  function renderTraceWithSpans(
    spanNames: Array<{ name: string; kind: SpanKind }>,
  ) {
    const spanKinds = spanNames.map((s) => s.kind);
    const traceId = `integration-test-trace-${spanKinds.join("-")}`;
    const traceData: any = {
      trace_id: traceId,
      project_id: "test-project",
      total_input_tokens: 20,
      total_output_tokens: 20,
      total_cost_usd: 0.02,
      root_span: {
        span_id: "root",
        trace_id: traceId,
        parent_id: "",
        project_id: "test-project",
        name: spanNames[0]?.name ?? "integration-root",
        kind: spanNames[0]?.kind ?? "chain",
        start_time: "2025-01-15T10:00:00Z",
        end_time: "2025-01-15T10:01:00Z",
        cost_usd: 0.01,
        input_tokens: 10,
        output_tokens: 10,
        children: spanNames.slice(1).map((s) => ({
          span_id: `span-${s.name}`,
          trace_id: traceId,
          parent_id: "root",
          project_id: "test-project",
          name: s.name,
          kind: s.kind,
          start_time: "2025-01-15T10:00:10Z",
          end_time: "2025-01-15T10:00:50Z",
          cost_usd: 0.005,
          input_tokens: 5,
          output_tokens: 5,
        })),
      },
    };

    const fetchMock = vi.spyOn(globalThis, "fetch");
    fetchMock.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(traceData),
    } as Response);

    return renderWithProvider(
      <TraceDetailPage
        traceId={traceId}
        activeProject="test-project"
        onBack={() => {}}
      />,
    );
  }

  function waitForIntegrationTraceLoaded() {
    // Wait for the spans list to render (it shows the span count)
    return waitFor(() => {
      expect(screen.getByText(/Spans \(\d+\)/)).toBeInTheDocument();
    });
  }

  describe("Waterfall bars use spanKindVisuals colors", () => {
    it("renders each SpanKind bar with its distinct color from spanKindVisuals", async () => {
      renderTraceWithSpans([
        { name: "llm-span", kind: "llm" },
        { name: "tool-span", kind: "tool" },
        { name: "agent-span", kind: "agent" },
        { name: "chain-span", kind: "chain" },
        { name: "internal-span", kind: "internal" },
      ]);

      await waitForIntegrationTraceLoaded();

      // The spans list should have 5 rows, each with a colored dot (w-2 h-2 rounded-full).
      const dots = document.querySelectorAll(".w-2.h-2.rounded-full.flex-shrink-0");
      expect(dots.length).toBe(5);

      // Verify each dot has a distinct color from the spanKindVisuals mapping.
      const colors = new Set<string>();
      dots.forEach((dot) => {
        const style = window.getComputedStyle(dot);
        const bg = style.backgroundColor;
        expect(bg).toMatch(/rgba?\(/);
        colors.add(bg);
      });
      // All 5 Kinds must have distinct colors (asserted by color uniqueness)
      expect(colors.size).toBe(5);

      // The root span badge should show the kind label ("llm")
      const llmBadge = screen.getAllByText(/^llm$/);
      expect(llmBadge.length).toBeGreaterThan(0);

      // The waterfall chart rendered
      expect(screen.getByText("Waterfall Timeline")).toBeInTheDocument();
    });

    it("uses different colors for different SpanKinds in the waterfall chart", async () => {
      renderTraceWithSpans([
        { name: "llm-span", kind: "llm" },
        { name: "tool-span", kind: "tool" },
        { name: "agent-span", kind: "agent" },
      ]);

      await waitForIntegrationTraceLoaded();

      // Collect colors from span row dots — they should use the spanKindVisuals colors.
      const dots = document.querySelectorAll(".w-2.h-2.rounded-full.flex-shrink-0");
      const colors = new Set<string>();
      dots.forEach((dot) => {
        const style = window.getComputedStyle(dot);
        colors.add(style.backgroundColor);
      });
      // 3 Kinds → 3 distinct colors
      expect(colors.size).toBe(3);
    });
  });

  describe("Tree row dots use spanKindVisuals colors", () => {
    it("renders tree row dots with spanKindVisuals colors", async () => {
      renderTraceWithSpans([
        { name: "llm-span", kind: "llm" },
        { name: "tool-span", kind: "tool" },
        { name: "chain-span", kind: "chain" },
      ]);

      // Switch to tree view
      await waitForIntegrationTraceLoaded();
      await act(async () => {
        const treeButton = screen.getByText("Tree");
        fireEvent.click(treeButton);
      });

      // The spans list in tree view should have colored dots for each span row.
      await waitFor(() => {
        const dots = document.querySelectorAll(".w-2.h-2.rounded-full.flex-shrink-0");
        // 3 spans → 3 dots
        expect(dots.length).toBe(3);
      });
    });
  });

  describe("Span detail panel uses spanKindVisuals colors", () => {
    it("renders the Kind badge in the detail panel with spanKindVisuals color", async () => {
      renderTraceWithSpans([
        { name: "test-span", kind: "llm" },
      ]);

      await waitForIntegrationTraceLoaded();

      // Click on the span row (the button with the aria-label) to open the detail panel.
      // There are two "test-span" labels (root card + span row), so use the button role.
      const spanRow = screen.getByRole("button", { name: /test-span/i });
      await act(async () => {
        fireEvent.click(spanRow);
      });

      // The detail panel should show the Kind badge with the correct color.
      // The root span summary card also shows a kind badge, so we look for
      // the badge in the detail panel which opens on the right side.
      await waitFor(() => {
        const llmBadges = screen.getAllByText(/^llm$/);
        expect(llmBadges.length).toBeGreaterThan(0);
        // At least one badge should have a background color from getSpanKindColor
        const badgeStyle = window.getComputedStyle(llmBadges[0]);
        expect(badgeStyle.backgroundColor).toMatch(/rgba?\(/);
      });
    });

    it("renders the SpanKind icon component inside the detail panel Kind badge", async () => {
      renderTraceWithSpans([
        { name: "icon-test", kind: "llm" },
      ]);

      await waitForIntegrationTraceLoaded();

      // Open the span detail panel by clicking on the span row button.
      const spanRow = screen.getByRole("button", { name: /icon-test/i });
      await act(async () => {
        fireEvent.click(spanRow);
      });

      // The detail panel badge should contain an SVG element (the inline icon).
      await waitFor(() => {
        const svgElements = document.querySelectorAll("svg[role='img']");
        const iconCount = svgElements.length;
        // At least one SVG (the kind icon) should be visible in the detail panel.
        expect(iconCount).toBeGreaterThan(0);
      });
    });
  });

  describe("Root span info bar badge uses spanKindVisuals", () => {
    it("renders the kind icon in the root span info bar badge", async () => {
      renderTraceWithSpans([{ name: "root-icon-test", kind: "tool" }]);

      await waitForIntegrationTraceLoaded();

      // The root span card should have a kind badge with an inline icon.
      await waitFor(() => {
        const svgElements = document.querySelectorAll("svg[role='img']");
        expect(svgElements.length).toBeGreaterThan(0);
      });
    });
  });
});
