import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import TracesPage from "./Traces";

// ── Helper data ──────────────────────────────────────────────────

const mockSpans = [
  {
    span_id: "root-1",
    trace_id: "trace-a",
    parent_id: "",
    project_id: "test-project",
    name: "main-trace",
    kind: "chain",
    model: "gpt-4",
    start_time: "2025-01-15T10:00:00Z",
    end_time: "2025-01-15T10:00:30Z",
    cost_usd: 0.11,
    input_tokens: 210,
    output_tokens: 405,
    span_count: 4,
    kind_counts: { chain: 1, llm: 2, tool: 1 },
  },
  {
    span_id: "root-2",
    trace_id: "trace-b",
    parent_id: "",
    project_id: "test-project",
    name: "simple-trace",
    kind: "chain",
    model: "",
    start_time: "2025-01-15T11:00:00Z",
    end_time: "2025-01-15T11:00:10Z",
    cost_usd: 0.02,
    input_tokens: 20,
    output_tokens: 40,
    span_count: 2,
    kind_counts: { chain: 1, llm: 1 },
  },
  {
    span_id: "root-3",
    trace_id: "trace-c",
    parent_id: "",
    project_id: "test-project",
    name: "leaf-span",
    kind: "llm",
    model: "gpt-3.5",
    start_time: "2025-01-15T12:00:00Z",
    end_time: "2025-01-15T12:00:05Z",
    cost_usd: 0.005,
    input_tokens: 5,
    output_tokens: 10,
    span_count: 1,
    kind_counts: { llm: 1 },
  },
];

// ── Test fixtures ────────────────────────────────────────────────

// ── Trace overlay keyboard navigation tests ──────────────────────

describe("trace overlay keyboard navigation", () => {
  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: mockSpans,
          next: "",
          limit: 25,
        }),
    } as Response);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders the TracesPage without throwing", () => {
    expect(() => {
      render(
        <TracesPage
          activeProject="test-project"
          onNavigateToTrace={() => {}}
          onNavigateToTraceDetail={() => {}}
          traceDetailOpen={false}
          activeTraceId=""
          onNavigateNextTrace={() => {}}
          onNavigatePrevTrace={() => {}}
        />
      );
    }).not.toThrow();
  });

  it("opens the overlay when a trace name button is clicked", async () => {
    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={() => {}}
        onNavigateToTraceDetail={() => {}}
        traceDetailOpen={false}
        activeTraceId=""
        setActiveTraceId={vi.fn()}
        onNavigateNextTrace={() => {}}
        onNavigatePrevTrace={() => {}}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    const traceButtons = screen.getAllByTitle(/View trace waterfall/i);
    expect(traceButtons.length).toBeGreaterThan(0);
  });

  it("navigates to next trace when ArrowDown is pressed and overlay is open", async () => {
    const navigateNextTrace = vi.fn();
    const navigatePrevTrace = vi.fn();

    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={vi.fn()}
        onNavigateToTraceDetail={vi.fn()}
        traceDetailOpen={true}
        activeTraceId="trace-a"
        setActiveTraceId={vi.fn()}
        onNavigateNextTrace={navigateNextTrace}
        onNavigatePrevTrace={navigatePrevTrace}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Fire ArrowDown key
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    });

    // Verify navigation callback was invoked
    expect(navigateNextTrace).toHaveBeenCalled();
  });

  it("navigates to previous trace when ArrowUp is pressed and overlay is open", async () => {
    const navigateNextTrace = vi.fn();
    const navigatePrevTrace = vi.fn();

    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={vi.fn()}
        onNavigateToTraceDetail={vi.fn()}
        traceDetailOpen={true}
        activeTraceId="trace-c"
        setActiveTraceId={vi.fn()}
        onNavigateNextTrace={navigateNextTrace}
        onNavigatePrevTrace={navigatePrevTrace}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Fire ArrowUp key
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowUp", bubbles: true }));
    });

    // Verify previous navigation callback was invoked
    expect(navigatePrevTrace).toHaveBeenCalled();
  });

  it("does not call onNavigatePrevTrace when ArrowUp is pressed at start", async () => {
    const navigateNextTrace = vi.fn();
    const navigatePrevTrace = vi.fn();

    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={vi.fn()}
        onNavigateToTraceDetail={vi.fn()}
        traceDetailOpen={true}
        activeTraceId="trace-a"
        setActiveTraceId={vi.fn()}
        onNavigateNextTrace={navigateNextTrace}
        onNavigatePrevTrace={navigatePrevTrace}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Fire ArrowUp key at start — should not call the callback
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowUp", bubbles: true }));
    });

    // ArrowUp at start is a no-op — no callback should fire
    expect(navigatePrevTrace).not.toHaveBeenCalled();
  });

  it("does not call onNavigateNextTrace when ArrowDown is pressed at end", async () => {
    const navigateNextTrace = vi.fn();
    const navigatePrevTrace = vi.fn();

    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={vi.fn()}
        onNavigateToTraceDetail={vi.fn()}
        traceDetailOpen={true}
        activeTraceId="trace-c"
        setActiveTraceId={vi.fn()}
        onNavigateNextTrace={navigateNextTrace}
        onNavigatePrevTrace={navigatePrevTrace}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Fire ArrowDown at end — should not call the callback
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    });

    expect(navigateNextTrace).not.toHaveBeenCalled();
  });

  it("ignores keyboard events when traceDetailOpen is false", async () => {
    const navigateNextTrace = vi.fn();
    const navigatePrevTrace = vi.fn();

    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={vi.fn()}
        onNavigateToTraceDetail={vi.fn()}
        traceDetailOpen={false}
        activeTraceId=""
        setActiveTraceId={vi.fn()}
        onNavigateNextTrace={navigateNextTrace}
        onNavigatePrevTrace={navigatePrevTrace}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Fire ArrowDown — should be ignored
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    });

    expect(navigateNextTrace).not.toHaveBeenCalled();
    expect(navigatePrevTrace).not.toHaveBeenCalled();
  });
});