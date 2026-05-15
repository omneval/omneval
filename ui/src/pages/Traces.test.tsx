import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
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
    cost_usd: 0.05,
    input_tokens: 100,
    output_tokens: 200,
    input: '{"message":"hello"}',
    output: '{"reply":"hi"}',
    status_code: "OK",
  },
  {
    span_id: "child-llm-1",
    trace_id: "trace-a",
    parent_id: "root-1",
    project_id: "test-project",
    name: "llm-call-1",
    kind: "llm",
    model: "gpt-4",
    start_time: "2025-01-15T10:00:01Z",
    end_time: "2025-01-15T10:00:10Z",
    cost_usd: 0.03,
    input_tokens: 50,
    output_tokens: 100,
    input: '{"prompt":"a"}',
    output: '{"completion":"b"}',
    status_code: "OK",
  },
  {
    span_id: "child-llm-2",
    trace_id: "trace-a",
    parent_id: "root-1",
    project_id: "test-project",
    name: "llm-call-2",
    kind: "llm",
    model: "gpt-4",
    start_time: "2025-01-15T10:00:11Z",
    end_time: "2025-01-15T10:00:20Z",
    cost_usd: 0.02,
    input_tokens: 50,
    output_tokens: 100,
    input: '{"prompt":"c"}',
    output: '{"completion":"d"}',
    status_code: "OK",
  },
  {
    span_id: "child-tool-1",
    trace_id: "trace-a",
    parent_id: "root-1",
    project_id: "test-project",
    name: "tool-use",
    kind: "tool",
    start_time: "2025-01-15T10:00:21Z",
    end_time: "2025-01-15T10:00:25Z",
    cost_usd: 0.01,
    input_tokens: 10,
    output_tokens: 5,
  },
  // Second trace with only one child
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
    cost_usd: 0.01,
    input_tokens: 10,
    output_tokens: 20,
  },
  {
    span_id: "child-llm-3",
    trace_id: "trace-b",
    parent_id: "root-2",
    project_id: "test-project",
    name: "llm-call",
    kind: "llm",
    model: "gpt-4",
    start_time: "2025-01-15T11:00:01Z",
    end_time: "2025-01-15T11:00:09Z",
    cost_usd: 0.01,
    input_tokens: 10,
    output_tokens: 20,
  },
  // Third trace with no children (leaf span)
  {
    span_id: "leaf-1",
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
  },
];

// ── Render helper ────────────────────────────────────────────────

function renderTracesPage() {
  return render(
    <TracesPage
      activeProject="test-project"
      onNavigateToTrace={() => {}}
      onNavigateToTraceDetail={() => {}}
    />
  );
}

// ── ObservationPills component tests ─────────────────────────────

describe("ObservationPills component", () => {
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

  it("renders a single badge when one kind of child exists", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // trace-a root span has 2 llm children + 1 tool child
    // trace-b root span has 1 llm child
    // trace-c root span has 0 children (leaf)
    // The first root row should show LLM and TOOL badges
    const llmBadges = screen.getAllByText(/LLM/);
    expect(llmBadges.length).toBeGreaterThan(0);
  });

  it("shows multiple badges for mixed child kinds", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // trace-a has 2 LLM + 1 tool children, so should show both
    const allText = screen.queryAllByText(/LLM|TOOL/);
    expect(allText.length).toBeGreaterThan(1);
  });

  it("shows single badge when all children are same kind", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("simple-trace")).toBeInTheDocument();
    });

    // trace-b has only 1 LLM child, so should show just one badge
    const llmBadges = screen.queryAllByText(/LLM 1/);
    expect(llmBadges.length).toBeGreaterThanOrEqual(1);
  });

  it("shows nothing when a trace has no children", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("leaf-span")).toBeInTheDocument();
    });

    // leaf-span has no children, so ObservationPills renders nothing
    const leafRow = screen.getByText("leaf-span").closest("tr");
    expect(leafRow).toBeInTheDocument();

    // The observation levels cell should be empty (no badge text)
    const leafRowCells = leafRow?.querySelectorAll("td");
    // observationLevels is the 6th column (0-indexed = 5)
    const levelsCell = leafRowCells?.[5];
    expect(levelsCell?.textContent).toBe("");
  });
});

// ── Column toggle tests ──────────────────────────────────────────

describe("column toggles", () => {
  it("hides Levels column when toggle is clicked", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: mockSpans,
          next: "",
          limit: 25,
        }),
    } as Response);

    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    const olButton = screen.getByRole("button", {
      name: "Toggle Levels column",
    });
    await act(async () => {
      fireEvent.click(olButton);
    });

    expect(screen.queryByText("Levels")).not.toBeInTheDocument();
  });

  it("shows Levels column header by default", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: mockSpans,
          next: "",
          limit: 25,
        }),
    } as Response);

    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    expect(screen.getByText("Levels")).toBeInTheDocument();
  });
});

// ── Traces tab tests ─────────────────────────────────────────────

describe("traces tab", () => {
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

  it("renders traces tab with span data", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    expect(screen.getByText("simple-trace")).toBeInTheDocument();
    expect(screen.getByText("leaf-span")).toBeInTheDocument();
  });
});

// ── Observations tab tests ───────────────────────────────────────

describe("observations tab", () => {
  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: mockSpans.filter((s) =>
            ["llm", "tool", "agent", "chain"].includes(s.kind)
          ),
          next: "",
          limit: 25,
        }),
    } as Response);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows only observation spans when observations tab is active", async () => {
    renderTracesPage();

    // Click the observations tab
    const obsTab = screen.getByText("observations");
    await act(async () => {
      fireEvent.click(obsTab);
    });

    // All shown spans should have observation kinds
    // The API returns only LLM/tool/agent/chain spans
    await waitFor(() => {
      expect(screen.getByText("llm-call-1")).toBeInTheDocument();
      expect(screen.getByText("tool-use")).toBeInTheDocument();
    });
  });
});

// ── Bookmark tests ───────────────────────────────────────────────

describe("bookmark toggling", () => {
  it("toggles bookmark star on click", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: mockSpans,
          next: "",
          limit: 25,
        }),
    } as Response);

    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Get the first bookmark button for the first visible trace
    const starButtons = screen.getAllByRole("button", {
      name: "Bookmark this trace",
    });
    expect(starButtons.length).toBeGreaterThan(0);
    const starButton = starButtons[0];

    await act(async () => {
      fireEvent.click(starButton);
    });

    // Should now show "Remove bookmark" for the clicked row
    const removeButtons = screen.getAllByRole("button", {
      name: "Remove bookmark",
    });
    expect(removeButtons.length).toBeGreaterThan(0);
  });
});

// ── Search tests ─────────────────────────────────────────────────

describe("search", () => {
  it("filters spans by search query", async () => {
    // First call (initial mount) returns empty
    // Second call (after search) returns filtered results
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({ spans: [], next: "", limit: 25 }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            spans: mockSpans.filter((s) => s.name.toLowerCase().includes("llm")),
            next: "",
            limit: 25,
          }),
      } as Response);

    renderTracesPage();

    // Initial empty state
    await waitFor(() => {
      expect(screen.queryByText("main-trace")).not.toBeInTheDocument();
    });

    // Enter search query
    const searchInput = screen.getByPlaceholderText("Search by ID/Name...");
    await act(async () => {
      fireEvent.change(searchInput, { target: { value: "llm" } });
    });

    // Should show LLM results
    await waitFor(() => {
      expect(screen.getByText("llm-call-1")).toBeInTheDocument();
    });

    vi.restoreAllMocks();
  });
});

// ── Pagination tests ─────────────────────────────────────────────

describe("pagination", () => {
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

  it("shows pagination controls", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    expect(screen.getByText("Rows per page:")).toBeInTheDocument();
  });
});

// ── Span rendering tests ─────────────────────────────────────────

describe("span rendering", () => {
  it("renders cost with four decimal places", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: [
            {
              ...mockSpans[0],
              cost_usd: 0.01234,
              input: "",
              output: "",
            },
          ],
          next: "",
          limit: 25,
        }),
    } as Response);

    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // cost_usd.toFixed(4) produces "$0.0123"
    expect(screen.getByText("$0.0123")).toBeInTheDocument();
  });
});

// ── Auto-refresh tests ───────────────────────────────────────────

describe("auto-refresh", () => {
  it("indicates auto-refresh is enabled when checkbox is checked", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: mockSpans,
          next: "",
          limit: 25,
        }),
    } as Response);

    renderTracesPage();

    // Initial fetch completes
    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    }, { timeout: 3000 });

    // Auto-refresh should be off initially
    const thirtyLabel = screen.getByText("30s");
    expect(thirtyLabel).not.toHaveClass("text-lantern-ember");

    // Enable auto-refresh (the "30s" label is next to the checkbox)
    const checkbox = thirtyLabel.previousElementSibling as HTMLElement;
    await act(async () => {
      fireEvent.click(checkbox);
    });

    expect(thirtyLabel).toHaveClass("text-lantern-ember");

    vi.restoreAllMocks();
  });
});
