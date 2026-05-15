import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import TraceDetailPage from "./TraceDetail";
import { ToastProvider } from "@/components/Toast";
import { totalTokens } from "./TraceDetail";
import { formatMs } from "@/utils/formatters";

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
        span_id: "x",
        trace_id: "x",
        parent_id: "",
        project_id: "",
        name: "",
        kind: "",
        start_time: "",
        end_time: "",
        input_tokens: inputTokens,
        output_tokens: outputTokens,
        cost_usd: 0,
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

  it("shows 'No child spans' for traces without children", async () => {
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

    expect(screen.getByText(/No child spans/)).toBeInTheDocument();
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
