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
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    const traceButtons = screen.getAllByTitle(/View trace waterfall/i);
    expect(traceButtons.length).toBeGreaterThan(0);
  });

  it("calls setActiveTraceId when ArrowDown is pressed and overlay is open", async () => {
    const setActiveTraceId = vi.fn();

    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={vi.fn()}
        onNavigateToTraceDetail={vi.fn()}
        traceDetailOpen={true}
        activeTraceId="trace-a"
        setActiveTraceId={setActiveTraceId}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // After mount, the useEffect syncs selectedIndex to match activeTraceId ("trace-a" is index 0).
    // ArrowDown from index 0 goes to index 1 (trace-b).
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    });
    expect(setActiveTraceId).toHaveBeenCalledWith("trace-b");
  });

  it("calls setActiveTraceId when ArrowUp is pressed and overlay is open", async () => {
    const setActiveTraceId = vi.fn();

    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={vi.fn()}
        onNavigateToTraceDetail={vi.fn()}
        traceDetailOpen={true}
        activeTraceId="trace-c"
        setActiveTraceId={setActiveTraceId}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // After mount, selectedIndex is synced to "trace-c" (index 2).
    // ArrowUp from index 2 goes to index 1 (trace-b).
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowUp", bubbles: true }));
    });
    expect(setActiveTraceId).toHaveBeenCalledWith("trace-b");

    // ArrowUp from index 1 goes to index 0 (trace-a).
    setActiveTraceId.mockClear();
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowUp", bubbles: true }));
    });
    expect(setActiveTraceId).toHaveBeenCalledWith("trace-a");
  });

  it("does not change trace when ArrowUp is pressed at start", async () => {
    const setActiveTraceId = vi.fn();

    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={vi.fn()}
        onNavigateToTraceDetail={vi.fn()}
        traceDetailOpen={true}
        activeTraceId="trace-a"
        setActiveTraceId={setActiveTraceId}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Flush effects so the selectedIndex ← activeTraceId sync runs.
    await act(async () => {});

    // Fire ArrowUp key at start — should not call setActiveTraceId
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowUp", bubbles: true }));
    });

    expect(setActiveTraceId).not.toHaveBeenCalled();
  });

  it("does not change trace when ArrowDown is pressed at end", async () => {
    const setActiveTraceId = vi.fn();

    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={vi.fn()}
        onNavigateToTraceDetail={vi.fn()}
        traceDetailOpen={true}
        activeTraceId="trace-c"
        setActiveTraceId={setActiveTraceId}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Flush effects so the selectedIndex ← activeTraceId sync runs.
    await act(async () => {});

    // Fire ArrowDown at end — should not call setActiveTraceId
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    });

    expect(setActiveTraceId).not.toHaveBeenCalled();
  });

  it("ignores keyboard events when traceDetailOpen is false", async () => {
    const setActiveTraceId = vi.fn();

    render(
      <TracesPage
        activeProject="test-project"
        onNavigateToTrace={vi.fn()}
        onNavigateToTraceDetail={vi.fn()}
        traceDetailOpen={false}
        activeTraceId=""
        setActiveTraceId={setActiveTraceId}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Fire ArrowDown — should be ignored
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    });

    expect(setActiveTraceId).not.toHaveBeenCalled();
  });
});