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
  it("initial fetch does NOT include search filter when query is empty", async () => {
    const fetchBodies: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
      if (init?.body) {
        fetchBodies.push(String(init.body));
      }
      return {
        ok: true,
        json: () =>
          Promise.resolve({ spans: mockSpans, next: "", limit: 25 }),
      } as Response;
    });

    renderTracesPage();

    // Wait for the initial fetch
    await waitFor(() => {
      expect(fetchBodies.length).toBeGreaterThanOrEqual(1);
    });

    // The initial body must NOT contain the name/ilike filter
    // (searchQuery is "" so no filter should be added)
    expect(fetchBodies[0]).not.toContain("ilike");
  });

  it("sends search query in API request body when typing", async () => {
    const fetchBodies: { callNum: number; body: string }[] = [];
    const fetchResolve: (() => void)[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
      const body = init?.body ? String(init.body) : "";
      fetchBodies.push({ callNum: fetchBodies.length + 1, body });
      // Block all fetches until test signals them to resolve
      await new Promise<void>((resolve) => fetchResolve.push(resolve));
      let resultSpans = mockSpans;
      if (body.includes("ilike") && body.includes("%llm%")) {
        resultSpans = mockSpans.filter((s) =>
          s.name.toLowerCase().includes("llm")
        );
      }
      return {
        ok: true,
        json: () =>
          Promise.resolve({ spans: resultSpans, next: "", limit: 25 }),
      } as Response;
    });

    renderTracesPage();

    // Wait for initial fetch to be registered (it's blocked)
    await waitFor(
      () => {
        expect(fetchBodies.length).toBeGreaterThanOrEqual(1);
      },
      { timeout: 2000 }
    );

    // Resolve the initial fetch so the UI gets data
    while (fetchResolve.length > 0) {
      const resolver = fetchResolve.shift();
      resolver?.();
    }

    // Wait for the UI to render with data
    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Verify initial fetch has NO search filter
    expect(fetchBodies[0].body).not.toContain("ilike");

    // Type search query
    const searchInput = screen.getByPlaceholderText("Search by ID/Name...");
    await act(async () => {
      fireEvent.change(searchInput, { target: { value: "llm" } });
    });

    // Wait for at least one more fetch to be initiated
    // (from onChange handler or effect)
    await waitFor(
      () => {
        expect(fetchBodies.length).toBeGreaterThanOrEqual(2);
      },
      { timeout: 3000 }
    );

    // The FIRST fetch after typing (call #2) MUST include "%llm%" in the body.
    // The onChange handler calls fetchSpans() synchronously after setSearchQuery().
    // If fetchSpans uses a stale closure, searchQuery will be "" and the body
    // will NOT have the "%llm%" filter — this is the bug (issue #91).
    const firstPostTypingCall = fetchBodies[1];
    expect(firstPostTypingCall.body).toContain("%llm%");
  });

  it("shows 'No results found' when search has no matches", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, _init) => {
      return {
        ok: true,
        json: () =>
          Promise.resolve({ spans: [], next: "", limit: 25 }),
      } as Response;
    });

    renderTracesPage();

    // Initial empty state shows OnboardingEmptyState
    // After search, should show No results found
    const searchInput = screen.getByPlaceholderText("Search by ID/Name...");
    await act(async () => {
      fireEvent.change(searchInput, { target: { value: "nonexistent" } });
    });

    await waitFor(() => {
      expect(screen.getByText(/No results found/)).toBeInTheDocument();
    });

    expect(screen.getByText(/No traces match "nonexistent"/)).toBeInTheDocument();
  });

  it("clears search and restores results", async () => {
    const fetchBodies: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
      if (init?.body) {
        fetchBodies.push(String(init.body));
      }
      return {
        ok: true,
        json: () =>
          Promise.resolve({ spans: mockSpans, next: "", limit: 25 }),
      } as Response;
    });

    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Type a search that filters results
    const searchInput = screen.getByPlaceholderText("Search by ID/Name...");
    await act(async () => {
      fireEvent.change(searchInput, { target: { value: "llm" } });
    });

    // Wait for the filtered fetch
    await waitFor(
      () => {
        expect(fetchBodies.some((b) => b.includes("ilike"))).toBe(true);
      },
      { timeout: 2000 }
    );

    // Clear the search
    await act(async () => {
      fireEvent.change(searchInput, { target: { value: "" } });
    });

    // Wait for a fetch that no longer includes the search filter
    await waitFor(
      () => {
        expect(fetchBodies.some((b) => !b.includes("ilike"))).toBe(true);
      },
      { timeout: 2000 }
    );
  });

  it("filters spans by partial trace ID", async () => {
    const fetchBodies: { callNum: number; body: string }[] = [];
    const fetchResolve: (() => void)[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
      const body = init?.body ? String(init.body) : "";
      fetchBodies.push({ callNum: fetchBodies.length + 1, body });
      await new Promise<void>((resolve) => fetchResolve.push(resolve));
      let resultSpans = mockSpans;
      if (body.includes("ilike") && body.includes("trace-a")) {
        resultSpans = mockSpans.filter((s) => s.trace_id.includes("trace-a"));
      }
      return {
        ok: true,
        json: () =>
          Promise.resolve({ spans: resultSpans, next: "", limit: 25 }),
      } as Response;
    });

    renderTracesPage();

    // Wait for initial fetch to be registered
    await waitFor(
      () => {
        expect(fetchBodies.length).toBeGreaterThanOrEqual(1);
      },
      { timeout: 2000 }
    );

    // Resolve the initial fetch so the UI gets data
    while (fetchResolve.length > 0) {
      const resolver = fetchResolve.shift();
      resolver?.();
    }
    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Search by partial trace ID
    const searchInput = screen.getByPlaceholderText("Search by ID/Name...");
    await act(async () => {
      fireEvent.change(searchInput, { target: { value: "trace-a" } });
    });

    // Wait for the search fetch
    await waitFor(
      () => {
        expect(fetchBodies.some((b) => b.body.includes("ilike"))).toBe(true);
      },
      { timeout: 3000 }
    );

    // The search body must include the trace ID partial match
    const searchCall = fetchBodies.find((b) => b.body.includes("ilike"));
    expect(searchCall).toBeDefined();
    expect(searchCall?.body).toContain("%trace-a%");
  });

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
