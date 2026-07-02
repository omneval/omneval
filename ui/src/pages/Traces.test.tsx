import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act, within } from "@testing-library/react";
import { useState } from "react";
import type { ComponentProps } from "react";
import TracesPage from "./Traces";
import { ToastProvider } from "@/components/Toast";
import { SlideInTraceDetail } from "./TraceDetail";

// ── Helper data ──────────────────────────────────────────────────

// Post-#136: the Traces list returns one row per trace — the root span
// annotated with trace-level rollups (span_count, kind_counts, summed
// tokens/cost, max end_time) — not a flat list of every span.
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
    input: '{"message":"hello"}',
    output: '{"reply":"hi"}',
    status_code: "OK",
    span_count: 4,
    kind_counts: { chain: 1, llm: 2, tool: 1 },
  },
  // Second trace with only one LLM child
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
    status_code: "OK",
    span_count: 2,
    kind_counts: { chain: 1, llm: 1 },
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
    status_code: "OK",
    span_count: 1,
    kind_counts: { llm: 1 },
  },
];

// ── Render helper ────────────────────────────────────────────────

function renderTracesPage(
  props: Partial<Omit<ComponentProps<typeof TracesPage>, "onOpenTraceOverlay">> &
    Partial<Pick<ComponentProps<typeof TracesPage>, "onOpenTraceOverlay">> = {}
) {
  return render(
    <TracesPage
      activeProject="test-project"
      onOpenTraceOverlay={vi.fn()}
      {...props}
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

  // Helper to enable the Levels column via the picker so pills render in the table.
  async function enableLevelsColumn() {
    const columnsButton = screen.getByRole("button", {
      name: "Column visibility",
    });
    await act(async () => {
      fireEvent.click(columnsButton);
    });
    const levelsToggle = screen.getByRole("button", {
      name: "Toggle Levels column",
    });
    await act(async () => {
      fireEvent.click(levelsToggle);
    });
    await act(async () => {
      fireEvent.click(columnsButton);
    });
  }

  it("#237: each span-kind badge has a title tooltip with the full kind name", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });
    await enableLevelsColumn();

    // trace-a's kind_counts is {chain: 1, llm: 2, tool: 1}
    // each badge pill should carry a title attribute equal to its full label
    const pills = screen.queryAllByRole("status");
    const titles = pills.map((p) => (p as HTMLElement).getAttribute("title"));
    expect(titles).toContain("LLM 2");
    expect(titles).toContain("Tool 1");
    expect(titles).toContain("Chain 1");
  });

  it("#237: agent and internal kinds map to their full labels", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: [
            {
              span_id: "root-3",
              trace_id: "trace-d",
              parent_id: "",
              project_id: "test-project",
              name: "agent-trace",
              kind: "chain",
              model: "gpt-4",
              start_time: "2025-01-15T13:00:00Z",
              end_time: "2025-01-15T13:00:20Z",
              cost_usd: 0.08,
              input_tokens: 100,
              output_tokens: 200,
              status_code: "OK",
              span_count: 3,
              kind_counts: { chain: 1, agent: 1, internal: 1 },
            },
          ],
          next: "",
          limit: 25,
        }),
    } as Response);

    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("agent-trace")).toBeInTheDocument();
    });
    await enableLevelsColumn();

    const pills = screen.queryAllByRole("status");
    const titles = pills.map((p) => (p as HTMLElement).getAttribute("title"));
    expect(titles).toContain("Agent 1");
    expect(titles).toContain("Internal 1");
    expect(titles).toContain("Chain 1");
  });

  it("#237: each span-kind badge has aria-label matching its title for screen readers", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });
    await enableLevelsColumn();

    const pills = screen.queryAllByRole("status");
    pills.forEach((pill) => {
      expect(pill).toHaveAttribute("aria-label");
      expect(pill).toHaveAttribute("title");
      expect(pill).toHaveAttribute("tabindex");
      expect(pill.getAttribute("aria-label")).toBe(pill.getAttribute("title"));
    });
  });

  it("renders a badge for each kind in kind_counts", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Levels column is off by default — enable it so the pills render.
    await enableLevelsColumn();

    // trace-a's kind_counts is {chain: 1, llm: 2, tool: 1} — the LLM and
    // TOOL badges should be present (CHA/LLM/TOO are the 3-char prefixes).
    const llmBadges = screen.getAllByText(/LLM/);
    expect(llmBadges.length).toBeGreaterThan(0);
  });

  it("shows multiple badges for mixed kind_counts", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Enable Levels so pills appear in the table.
    await enableLevelsColumn();

    // trace-a has kind_counts {chain: 1, llm: 2, tool: 1} — both LLM and
    // TOOL pills should render.
    const allText = screen.queryAllByText(/LLM|TOO/);
    expect(allText.length).toBeGreaterThan(1);
  });

  it("shows single badge when kind_counts has one llm entry", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("simple-trace")).toBeInTheDocument();
    });

    // Enable Levels so the pill renders.
    await enableLevelsColumn();

    // trace-b's kind_counts is {chain: 1, llm: 1} — should show "LLM 1".
    const llmBadges = screen.queryAllByText(/LLM 1/);
    expect(llmBadges.length).toBeGreaterThanOrEqual(1);
  });

  it("shows a badge for a single-span trace with kind_counts", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("leaf-span")).toBeInTheDocument();
    });

    // Enable Levels so the pill renders.
    await enableLevelsColumn();

    const leafRow = screen.getByText("leaf-span").closest("tr");
    expect(leafRow).toBeInTheDocument();

    // observationLevels is now the last visible column.
    const leafRowCells = leafRow?.querySelectorAll("td");
    const levelsCell = leafRowCells?.[leafRowCells!.length - 1];
    expect(levelsCell?.textContent).toContain("LLM 1");
  });
});

// ── Column toggle tests ──────────────────────────────────────────

describe("column toggles", () => {
  it("toggles Levels column on then off", async () => {
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

    // Levels is off by default — enable it first.
    const columnsButton = screen.getByRole("button", {
      name: "Column visibility",
    });

    await act(async () => {
      fireEvent.click(columnsButton);
    });
    const levelsToggle = screen.getByRole("button", {
      name: "Toggle Levels column",
    });
    await act(async () => {
      fireEvent.click(levelsToggle);
    });

    // Close the picker and verify the header is now visible.
    await act(async () => {
      fireEvent.click(columnsButton);
    });
    await waitFor(() => {
      expect(screen.getByText("Levels")).toBeInTheDocument();
    });

    // Now toggle it off again.
    await act(async () => {
      fireEvent.click(columnsButton);
    });
    await act(async () => {
      fireEvent.click(levelsToggle);
    });
    await act(async () => {
      fireEvent.click(columnsButton);
    });

    expect(screen.queryByText("Levels")).not.toBeInTheDocument();
  });

  it("hides Levels column by default", async () => {
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

    // Levels is not visible by default; it can be toggled on via the picker.
    const tableHeaders = screen.queryAllByRole("columnheader");
    const levelsVisible = tableHeaders.some(h => h.textContent === "Levels");
    expect(levelsVisible).toBe(false);
  });

  it("hides Input column by default", async () => {
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

    // Input column header should not be visible by default — query the table
    // headers specifically to avoid picking up text elsewhere in the page.
    const tableHeaders = screen.queryAllByRole("columnheader");
    const inputVisible = tableHeaders.some(h => h.textContent === "Input");
    expect(inputVisible).toBe(false);
  });

  it("hides Output column by default", async () => {
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

    const tableHeaders = screen.queryAllByRole("columnheader");
    const outputVisible = tableHeaders.some(h => h.textContent === "Output");
    expect(outputVisible).toBe(false);
  });

  it("shows Latency column header by default", async () => {
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

    expect(screen.getByRole("columnheader", { name: "Latency" })).toBeInTheDocument();
  });

  it("shows Tokens column header by default", async () => {
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

    expect(screen.getByRole("columnheader", { name: "Tokens" })).toBeInTheDocument();
  });

  it("shows Cost column header by default", async () => {
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

    expect(screen.getByRole("columnheader", { name: "Cost" })).toBeInTheDocument();
  });

  it("Latency/Tokens/Cost columns appear before Input/Output in column order", async () => {
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

    // Enable Input and Output via the column picker so we can verify ordering.
    const columnsButton = screen.getByRole("button", {
      name: "Column visibility",
    });
    await act(async () => {
      fireEvent.click(columnsButton);
    });

    // Toggle Input on via the picker button.
    const inputToggle = screen.getByRole("button", {
      name: "Toggle Input column",
    });
    await act(async () => {
      fireEvent.click(inputToggle);
    });

    // Close the menu first so the column header re-renders.
    await act(async () => {
      fireEvent.click(columnsButton);
    });

    // Toggle Output on.
    const outputToggle = screen.getByRole("button", {
      name: "Toggle Output column",
    });
    await act(async () => {
      fireEvent.click(outputToggle);
    });

    // Close the menu so column headers settle.
    await act(async () => {
      fireEvent.click(columnsButton);
    });

    // Collect visible column headers in rendered order.
    const headers = Array.from(screen.queryAllByRole("columnheader"));
    const labels = headers.map(h => h.textContent ?? "").filter(Boolean);

    const latencyIdx = labels.indexOf("Latency");
    const tokensIdx = labels.indexOf("Tokens");
    const costIdx = labels.indexOf("Cost");
    const inputIdx = labels.indexOf("Input");
    const outputIdx = labels.indexOf("Output");

    // Verify Input and Output come after Latency, Tokens, and Cost.
    expect(latencyIdx).toBeLessThan(inputIdx);
    expect(latencyIdx).toBeLessThan(outputIdx);
    expect(tokensIdx).toBeLessThan(inputIdx);
    expect(tokensIdx).toBeLessThan(outputIdx);
    expect(costIdx).toBeLessThan(inputIdx);
    expect(costIdx).toBeLessThan(outputIdx);
  });

  it("table rows have reduced padding for denser layout", async () => {
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

    // Every <td> in the table body should have py-1 (4px vertical padding)
    // for a denser single-line row, not py-2.5 (10px) or larger.
    const rows = document.querySelectorAll("tbody tr");
    expect(rows.length).toBeGreaterThan(0);

    rows.forEach(row => {
      const cells = row.querySelectorAll("td");
      cells.forEach(cell => {
        expect(cell.classList.contains("py-1")).toBe(true);
      });
    });
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

    // The initial body must NOT contain the name search filter
    // (searchQuery is "" so no filter should be added)
    expect(fetchBodies[0]).not.toContain("contains");
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
      if (body.includes("contains") && body.includes("llm")) {
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
    expect(fetchBodies[0].body).not.toContain("contains");

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

    // The FIRST fetch after typing (call #2) MUST include the search term in
    // the body. The onChange handler calls fetchSpans() synchronously after
    // setSearchQuery(). If fetchSpans uses a stale closure, searchQuery will be
    // "" and the body will NOT have the name filter — this is the bug (#91).
    const firstPostTypingCall = fetchBodies[1];
    expect(firstPostTypingCall.body).toContain("contains");
    expect(firstPostTypingCall.body).toContain("llm");
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
        expect(fetchBodies.some((b) => b.includes("contains"))).toBe(true);
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
        expect(fetchBodies.some((b) => !b.includes("contains"))).toBe(true);
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
      if (body.includes("contains") && body.includes("trace-a")) {
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
        expect(fetchBodies.some((b) => b.body.includes("contains"))).toBe(true);
      },
      { timeout: 3000 }
    );

    // The search body must include the trace ID partial match
    const searchCall = fetchBodies.find((b) => b.body.includes("contains"));
    expect(searchCall).toBeDefined();
    expect(searchCall?.body).toContain("trace-a");
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
            spans: mockSpans.filter((s) => s.kind === "llm"),
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

    // Should show the leaf-span trace (kind: "llm")
    await waitFor(() => {
      expect(screen.getByText("leaf-span")).toBeInTheDocument();
    });

    vi.restoreAllMocks();
  });
});

// ── Data correctness tests (filters, project switch, time range) ──

describe("fetch correctness", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("every fetch after applying a filter includes the new filter state (no stale-closure fetch)", async () => {
    const fetchBodies: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, init) => {
      // The Model filter's distinct-models lookup (/api/v1/analytics/spans)
      // is a separate, independent fetch — exclude it from the spans/query
      // stale-closure check below.
      if (init?.body && !String(url).includes("/api/v1/analytics/spans")) {
        fetchBodies.push(String(init.body));
      }
      return {
        ok: true,
        json: () => Promise.resolve({ spans: mockSpans, next: "", limit: 25, rows: [] }),
      } as Response;
    });

    renderTracesPage();
    await waitFor(() => {
      expect(fetchBodies.length).toBeGreaterThanOrEqual(1);
    });

    // Expand the Model filter and apply "gpt-4"
    const modelHeader = screen.getByText("Model");
    await act(async () => {
      fireEvent.click(modelHeader);
    });
    const modelBorderDiv = modelHeader.closest("div.border-b") as HTMLElement;
    const expanded = modelBorderDiv.querySelector("div.px-3") as HTMLElement;
    const input = expanded.querySelector<HTMLInputElement>('input[type="text"]');
    await act(async () => {
      fireEvent.change(input!, { target: { value: "gpt-4" } });
    });
    await act(async () => {
      fireEvent.click(expanded.querySelector("button") as HTMLButtonElement);
    });

    await waitFor(() => {
      expect(fetchBodies.length).toBeGreaterThanOrEqual(2);
    });
    // Let any further effect-driven fetches settle
    await act(async () => {
      await new Promise((r) => setTimeout(r, 400));
    });

    // Every fetch issued AFTER the apply must carry the model filter. A
    // stale-closure fetch (issued with the pre-apply filter state) is the
    // bug that makes filters appear to "not work" and can race the correct
    // response.
    const postApply = fetchBodies.slice(1);
    expect(postApply.length).toBeGreaterThanOrEqual(1);
    for (const body of postApply) {
      expect(body).toContain("gpt-4");
    }
  });

  it("does not keep showing the previous project's spans after switching projects", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
      const body = JSON.parse(String(init?.body ?? "{}"));
      if (body.project_id === "test-project") {
        return {
          ok: true,
          json: () => Promise.resolve({ spans: mockSpans, next: "", limit: 25 }),
        } as Response;
      }
      // The other project errors out — old rows must still be cleared.
      return { ok: false, json: () => Promise.resolve({}) } as Response;
    });

    const view = renderTracesPage();
    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    view.rerender(
      <TracesPage activeProject="other-project" onOpenTraceOverlay={vi.fn()} />
    );

    await waitFor(() => {
      expect(screen.queryByText("main-trace")).not.toBeInTheDocument();
    });
  });

  it("uses the header time range preset for the query window", async () => {
    const fetchBodies: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
      if (init?.body) fetchBodies.push(String(init.body));
      return {
        ok: true,
        json: () => Promise.resolve({ spans: mockSpans, next: "", limit: 25 }),
      } as Response;
    });

    renderTracesPage({ timeRange: "1h" });
    await waitFor(() => {
      expect(fetchBodies.length).toBeGreaterThanOrEqual(1);
    });

    const body = JSON.parse(fetchBodies[0]);
    const diffMs =
      new Date(body.to).getTime() - new Date(body.from).getTime();
    expect(diffMs).toBeGreaterThanOrEqual(59 * 60 * 1000);
    expect(diffMs).toBeLessThanOrEqual(61 * 60 * 1000);
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

  it("load next page button calls fetch with cursor when more results exist", async () => {
    const fetchBodies: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
      if (init?.body) {
        fetchBodies.push(String(init.body));
      }
      const bodyObj = JSON.parse(String(init?.body || "{}"));
      const limit = bodyObj.limit || 25;
      // Initial fetch (no cursor): return up to `limit` spans + next cursor
      // Pagination fetch (has cursor): return empty + no cursor
      if (bodyObj.cursor) {
        return {
          ok: true,
          json: () =>
            Promise.resolve({ spans: [], next: "", limit }),
        } as Response;
      }
      return {
        ok: true,
        json: () =>
          Promise.resolve({
            spans: mockSpans.slice(0, Math.min(limit, mockSpans.length)),
            next: "cursor-page-2",
            limit,
          }),
      } as Response;
    });

    renderTracesPage();

    // Wait for initial fetch with default page size 25
    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    }, { timeout: 3000 });

    // The button should show "Load Next Page" since the API returned a cursor
    expect(screen.getByText("Load Next Page")).toBeInTheDocument();

    // The first API request should NOT have a cursor
    expect(fetchBodies[0]).not.toContain("cursor");

    // Click "Load Next Page"
    const loadNextButton = screen.getByText("Load Next Page");
    await act(async () => {
      fireEvent.click(loadNextButton);
    });

    // Wait for the next fetch to include the cursor
    await waitFor(
      () => {
        expect(fetchBodies.length).toBeGreaterThanOrEqual(2);
      },
      { timeout: 3000 }
    );

    // The second request MUST include the cursor from the first page
    expect(fetchBodies[1]).toContain("cursor-page-2");

    // After second fetch (empty results), button should say "No more data"
    await waitFor(() => {
      expect(screen.getByText("No more data")).toBeInTheDocument();
    }, { timeout: 3000 });
  });

  it("disables load next page button when no more results", async () => {
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
    }, { timeout: 3000 });

    // With next="", the button should say "No more data" and be disabled
    const loadNextButton = screen.getByText("No more data");
    expect(loadNextButton.closest("button")).toBeDisabled();
  });
});

// ── Span rendering tests ─────────────────────────────────────────

describe("span rendering", () => {
  it("renders small costs with humanized precision", async () => {
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

    // formatCost(0.01234) produces "$0.012"
    expect(screen.getByText("$0.012")).toBeInTheDocument();
  });
});

// ── Sentinel token/cost display tests ────────────────────────────

describe("sentinel token and cost display", () => {
  it("shows 0 total tokens (not -2) when span has -1 sentinel token values", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: [
            {
              ...mockSpans[0],
              input_tokens: -1,
              output_tokens: -1,
              cost_usd: 0,
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

    // Total token display must be "0", never "-2"
    expect(screen.getByText("0")).toBeInTheDocument();
    expect(screen.queryByText("-2")).not.toBeInTheDocument();
  });

  it("does not show -1+-1 breakdown when span has -1 sentinel token values", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          spans: [
            {
              ...mockSpans[0],
              input_tokens: -1,
              output_tokens: -1,
              cost_usd: 0,
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

    // The "(X+Y)" breakdown must not show negative numbers
    const tokenBreakdown = document.body.textContent ?? "";
    expect(tokenBreakdown).not.toContain("-1+-1");
    expect(tokenBreakdown).not.toContain("(-1+");
    // It should show (0+0)
    expect(tokenBreakdown).toContain("(0+0)");
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
    expect(thirtyLabel).not.toHaveClass("text-omneval-violet-pale");

    // Enable auto-refresh (the "30s" label is next to the checkbox)
    const checkbox = thirtyLabel.previousElementSibling as HTMLElement;
    await act(async () => {
      fireEvent.click(checkbox);
    });

    expect(thirtyLabel).toHaveClass("text-omneval-violet-pale");

    vi.restoreAllMocks();
  });
});

// ── Filter tests ───────────────────────────────────────────────────

describe("filters", () => {
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

  it("renders filter sidebar with all filter sections", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // All filter section headers should be visible in the sidebar
    const filterSidebar = document.querySelector(".w-64.border-r");
    expect(filterSidebar).toBeInTheDocument();

    // Check filter section headers exist in sidebar
    const sidebarText = filterSidebar?.textContent ?? "";
    expect(sidebarText).toContain("Trace Name");
    expect(sidebarText).toContain("Model");
    expect(sidebarText).toContain("Kind");
    expect(sidebarText).toContain("Status Code");
    expect(sidebarText).toContain("Duration");
    expect(sidebarText).toContain("Tokens");
    expect(sidebarText).toContain("Cost");
  });

  it("sends model filter as 'in' operator when model values are entered", async () => {
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

    // Wait for initial fetch
    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(1); },
      { timeout: 2000 }
    );

    // Expand Model filter - click on "Model" text in the sidebar header
    const modelHeader = screen.getByText("Model");
    await act(async () => {
      fireEvent.click(modelHeader);
    });

    // After expanding, find the expanded content div for Model section
    // The structure is: border-b div > button (header) > expanded div (content)
    const modelBorderDiv = modelHeader.closest("div.border-b") as HTMLElement;
    const expandedContent = modelBorderDiv.querySelector("div.px-3") as HTMLElement;
    expect(expandedContent).toBeInTheDocument();

    // Get the text input and Apply button from the expanded content
    const modelInput = expandedContent.querySelector<HTMLInputElement>('input[type="text"]');
    expect(modelInput).toBeInTheDocument();
    await act(async () => {
      fireEvent.change(modelInput!, { target: { value: "gpt-4" } });
    });

    const applyBtn = expandedContent.querySelector('button');
    expect(applyBtn).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(applyBtn as HTMLButtonElement);
    });

    // Wait for the fetch with the filter
    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(2); },
      { timeout: 2000 }
    );

    // The body should contain model in filter
    const modelBody = fetchBodies.find((b) => b.includes('"model"') && b.includes('"in"'));
    expect(modelBody).toBeDefined();
    expect(modelBody).toContain("gpt-4");
  });

  it("shows checkboxes for distinct models in the project alongside free-text search", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (String(url).includes("/api/v1/analytics/spans")) {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              rows: [{ model: "gpt-4" }, { model: "claude-3" }, { model: "unknown" }],
            }),
        } as Response;
      }
      return {
        ok: true,
        json: () => Promise.resolve({ spans: mockSpans, next: "", limit: 25 }),
      } as Response;
    });

    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Expand the Model filter section
    const modelHeader = screen.getByText("Model");
    await act(async () => {
      fireEvent.click(modelHeader);
    });

    const modelBorderDiv = modelHeader.closest("div.border-b") as HTMLElement;
    const expandedContent = modelBorderDiv.querySelector("div.px-3") as HTMLElement;
    expect(expandedContent).toBeInTheDocument();

    // The distinct models returned by the analytics endpoint should render
    // as checkboxes within the Model filter section.
    await waitFor(() => {
      expect(expandedContent.querySelector('input[type="checkbox"]')).toBeInTheDocument();
    });

    expect(expandedContent.textContent).toContain("gpt-4");
    expect(expandedContent.textContent).toContain("claude-3");
    expect(expandedContent.textContent).toContain("unknown");

    // Free-text search box must still be present alongside the checkboxes.
    expect(expandedContent.querySelector('input[type="text"]')).toBeInTheDocument();
  });

  it("sends model filter as 'in' operator when a known-model checkbox is toggled, and merges with free-text entries", async () => {
    const fetchBodies: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, init) => {
      if (String(url).includes("/api/v1/analytics/spans")) {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              rows: [{ model: "gpt-4" }, { model: "claude-3" }],
            }),
        } as Response;
      }
      if (init?.body) {
        fetchBodies.push(String(init.body));
      }
      return {
        ok: true,
        json: () => Promise.resolve({ spans: mockSpans, next: "", limit: 25 }),
      } as Response;
    });

    renderTracesPage();

    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(1); },
      { timeout: 2000 }
    );

    // Expand the Model filter section
    const modelHeader = screen.getByText("Model");
    await act(async () => {
      fireEvent.click(modelHeader);
    });

    const modelBorderDiv = modelHeader.closest("div.border-b") as HTMLElement;
    const expandedContent = modelBorderDiv.querySelector("div.px-3") as HTMLElement;

    await waitFor(() => {
      expect(expandedContent.querySelector('input[type="checkbox"]')).toBeInTheDocument();
    });

    // Toggle the "claude-3" checkbox.
    const checkboxes = Array.from(
      expandedContent.querySelectorAll<HTMLInputElement>('input[type="checkbox"]'),
    );
    const claudeCheckbox = checkboxes.find(
      (cb) => cb.closest("label")?.textContent?.includes("claude-3"),
    );
    expect(claudeCheckbox).toBeDefined();
    await act(async () => {
      fireEvent.click(claudeCheckbox!);
    });

    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(2); },
      { timeout: 2000 }
    );

    let modelBody = fetchBodies.find((b) => b.includes('"model"') && b.includes('"in"'));
    expect(modelBody).toBeDefined();
    expect(modelBody).toContain("claude-3");

    // Now also enter a free-text model not in the known list — it should be
    // unioned with the already-checked "claude-3" rather than replacing it.
    const modelInput = expandedContent.querySelector<HTMLInputElement>('input[type="text"]');
    expect(modelInput).toBeInTheDocument();
    await act(async () => {
      fireEvent.change(modelInput!, { target: { value: "custom-model" } });
    });

    const applyBtn = expandedContent.querySelector('button');
    expect(applyBtn).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(applyBtn as HTMLButtonElement);
    });

    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(3); },
      { timeout: 2000 }
    );

    modelBody = fetchBodies[fetchBodies.length - 1];
    expect(modelBody).toContain("claude-3");
    expect(modelBody).toContain("custom-model");
  });

  it("sends kind filter when kind checkboxes are selected", async () => {
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

    // Wait for initial fetch
    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(1); },
      { timeout: 2000 }
    );

    // Expand Kind filter
    const kindHeader = screen.getByText("Kind");
    await act(async () => {
      fireEvent.click(kindHeader);
    });

    // Select the "llm" checkbox within the Kind section
    const kindBorderDiv = kindHeader.closest("div.border-b") as HTMLElement;
    const expandedContent = kindBorderDiv.querySelector("div.px-3") as HTMLElement;
    const llmCheckbox = expandedContent.querySelector<HTMLInputElement>('input[type="checkbox"]');
    expect(llmCheckbox).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(llmCheckbox!);
    });

    // Wait for the fetch with the filter
    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(2); },
      { timeout: 2000 }
    );

    // The body should contain kind in filter with "llm"
    const kindBody = fetchBodies.find((b) => b.includes('"kind"') && b.includes('"in"'));
    expect(kindBody).toBeDefined();
    expect(kindBody).toContain("llm");
  });

  it("sends duration filter as range with gte/lte operators", async () => {
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

    // Wait for initial fetch
    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(1); },
      { timeout: 2000 }
    );

    // Expand Duration filter
    const durationHeader = screen.getByText("Duration");
    await act(async () => {
      fireEvent.click(durationHeader);
    });

    // Set min and max values within the Duration section
    const durationBorderDiv = durationHeader.closest("div.border-b") as HTMLElement;
    const expandedContent = durationBorderDiv.querySelector("div.px-3") as HTMLElement;
    const durationInputs = expandedContent.querySelectorAll<HTMLInputElement>('input[type="number"]');
    expect(durationInputs.length).toBe(2);

    await act(async () => {
      fireEvent.change(durationInputs[0], { target: { value: "100" } });
    });
    await act(async () => {
      fireEvent.change(durationInputs[1], { target: { value: "5000" } });
    });

    // Find Apply button within the Duration section
    const applyBtn = expandedContent.querySelector('button');
    expect(applyBtn).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(applyBtn as HTMLButtonElement);
    });

    // Wait for the fetch with the filter
    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(2); },
      { timeout: 2000 }
    );

    // The body should contain duration_ms gte and lte filters.
    // Use the last fetch body since onChange triggers re-fetches for each input change.
    const lastBody = fetchBodies[fetchBodies.length - 1];
    expect(lastBody).toContain('"duration_ms"');
    expect(lastBody).toContain('"gte"');
    expect(lastBody).toContain('"lte"');
    expect(lastBody).toContain("100");
    expect(lastBody).toContain("5000");
  });

  it("clears all filters and resets to defaults", async () => {
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

    // Wait for initial fetch
    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(1); },
      { timeout: 2000 }
    );

    // Apply a filter first - expand Model section
    const modelHeader = screen.getByText("Model");
    await act(async () => {
      fireEvent.click(modelHeader);
    });

    const modelBorderDiv = modelHeader.closest("div.border-b") as HTMLElement;
    const modelExpanded = modelBorderDiv.querySelector("div.px-3") as HTMLElement;
    const modelInput = modelExpanded.querySelector<HTMLInputElement>('input[type="text"]');
    await act(async () => {
      fireEvent.change(modelInput!, { target: { value: "gpt-4" } });
    });

    const modelApplyBtn = modelExpanded.querySelector('button');
    await act(async () => {
      fireEvent.click(modelApplyBtn as HTMLButtonElement);
    });

    // Wait for the filtered fetch
    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(2); },
      { timeout: 2000 }
    );

    // Clear all filters
    const clearBtn = screen.getByRole("button", { name: "Clear All Filters" });
    await act(async () => {
      fireEvent.click(clearBtn);
    });

    // Should trigger a fresh fetch without filters
    await waitFor(
      () => { expect(fetchBodies.length).toBeGreaterThanOrEqual(3); },
      { timeout: 2000 }
    );
  });
});

describe("filter panel styling (#142)", () => {
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

  it("uses the shared omneval @theme tokens for the sidebar surface and dividers", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("Filters")).toBeInTheDocument();
    });

    // Sidebar surface + border use the shared @theme tokens, not raw colors.ts values.
    const heading = screen.getByText("Filters");
    const sidebar = heading.closest("div.flex.flex-col") as HTMLElement;
    expect(sidebar.className).toContain("bg-omneval-depth");
    expect(sidebar.className).toContain("border-omneval-border");

    // Each FilterSection is a bordered row using the shared border token.
    const sections = sidebar.querySelectorAll("div.border-b");
    expect(sections.length).toBeGreaterThan(0);
    sections.forEach((section) => {
      expect(section.className).toContain("border-omneval-border");
    });
  });

  it("renders custom-styled checkboxes with the violet accent in the Kind filter", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("Filters")).toBeInTheDocument();
    });

    const kindHeader = screen.getByText("Kind");
    await act(async () => {
      fireEvent.click(kindHeader);
    });

    const kindBorderDiv = kindHeader.closest("div.border-b") as HTMLElement;
    const expandedContent = kindBorderDiv.querySelector("div.px-3") as HTMLElement;
    const checkbox = expandedContent.querySelector<HTMLInputElement>('input[type="checkbox"]');

    expect(checkbox).toBeInTheDocument();
    expect(checkbox!.className).toContain("accent-omneval-violet");
    expect(checkbox!.className).toContain("border-omneval-border");

    // The label row gets a subtle violet hover background.
    const label = checkbox!.closest("label") as HTMLElement;
    expect(label.className).toContain("bg-violet-hover");
  });

  it("renders Apply buttons (TextFilter, RangeFilter) with the violet accent", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("Filters")).toBeInTheDocument();
    });

    // Model filter (TextFilter) — Apply button
    const modelHeader = screen.getByText("Model");
    await act(async () => {
      fireEvent.click(modelHeader);
    });
    const modelBorderDiv = modelHeader.closest("div.border-b") as HTMLElement;
    const modelExpanded = modelBorderDiv.querySelector("div.px-3") as HTMLElement;
    const modelApplyBtn = modelExpanded.querySelector("button") as HTMLButtonElement;
    expect(modelApplyBtn).toHaveTextContent("Apply");
    expect(modelApplyBtn.style.background).toBe("var(--color-omneval-violet)");

    // Duration filter (RangeFilter) — Apply button
    const durationHeader = screen.getByText("Duration");
    await act(async () => {
      fireEvent.click(durationHeader);
    });
    const durationBorderDiv = durationHeader.closest("div.border-b") as HTMLElement;
    const durationExpanded = durationBorderDiv.querySelector("div.px-3") as HTMLElement;
    const durationApplyBtn = durationExpanded.querySelector("button") as HTMLButtonElement;
    expect(durationApplyBtn).toHaveTextContent("Apply");
    expect(durationApplyBtn.style.background).toBe("var(--color-omneval-violet)");
  });

  it("renders text and number filter inputs with consistent focus styling", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("Filters")).toBeInTheDocument();
    });

    const modelHeader = screen.getByText("Model");
    await act(async () => {
      fireEvent.click(modelHeader);
    });
    const modelBorderDiv = modelHeader.closest("div.border-b") as HTMLElement;
    const modelExpanded = modelBorderDiv.querySelector("div.px-3") as HTMLElement;
    const modelInput = modelExpanded.querySelector<HTMLInputElement>('input[type="text"]');

    expect(modelInput!.className).toContain("input-focus");
    expect(modelInput!.className).toContain("border-omneval-border");
    expect(modelInput!.className).toContain("bg-omneval-surface");
  });

  it("renders the Clear All Filters button using the shared secondary button style", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("Filters")).toBeInTheDocument();
    });

    const clearBtn = screen.getByRole("button", { name: "Clear All Filters" });
    expect(clearBtn.className).toContain("btn-secondary");
  });

  it("renders checkbox column headers for row selection", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    // The selection column header checkbox should be present
    const selectAllCheckbox = screen.getByRole("checkbox", { name: /select all/i });
    expect(selectAllCheckbox).toBeInTheDocument();
  });

  it("renders a checkbox for each trace row", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    // Each row should have a checkbox
    const rowCheckboxes = screen.getAllByRole("checkbox", { name: /select trace/i });
    // We have 3 mock spans → 3 checkboxes (one per row)
    expect(rowCheckboxes).toHaveLength(3);
  });

  it("toggles selection when a row checkbox is clicked", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    const rowCheckboxes = screen.getAllByRole("checkbox", { name: /select trace/i });
    expect(rowCheckboxes[0]).not.toBeChecked();

    await act(async () => {
      fireEvent.click(rowCheckboxes[0]);
    });

    expect(rowCheckboxes[0]).toBeChecked();
  });

  it("selects all rows when the header checkbox is clicked", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    const selectAllCheckbox = screen.getByRole("checkbox", { name: /select all/i });
    expect(selectAllCheckbox).not.toBeChecked();

    await act(async () => {
      fireEvent.click(selectAllCheckbox);
    });

    // All row checkboxes should now be checked
    const rowCheckboxes = screen.getAllByRole("checkbox", { name: /select trace/i });
    rowCheckboxes.forEach((cb) => expect(cb).toBeChecked());
  });

  it("toggles select-all checkbox when individual row checkboxes are clicked", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    const selectAllCheckbox = screen.getByRole("checkbox", { name: /select all/i });
    const rowCheckboxes = screen.getAllByRole("checkbox", { name: /select trace/i });

    // Click first row
    await act(async () => {
      fireEvent.click(rowCheckboxes[0]);
    });
    // select-all should become indeterminate or checked depending on count
    // With 3 rows, 1 checked → not all → select-all should not be checked
    expect(selectAllCheckbox).not.toBeChecked();

    // Click remaining rows
    for (let i = 1; i < rowCheckboxes.length; i++) {
      await act(async () => {
        fireEvent.click(rowCheckboxes[i]);
      });
    }
    expect(selectAllCheckbox).toBeChecked();
  });
});

// ── Trace detail overlay tests (#281) ──────────────────────────────────


/**
 * Component that mirrors App.tsx's overlay pattern: TracesPage +
 * SlideInTraceDetail mounted conditionally on local state.  This lets the
 * tests verify that the Traces list stays mounted behind the overlay.
 */
function TracesOverlayWrapper({
  children,
}: {
  children?: Partial<
    Omit<ComponentProps<typeof TracesPage>, "onOpenTraceOverlay">
  > &
    Partial<Pick<ComponentProps<typeof TracesPage>, "onOpenTraceOverlay">>;
}) {
  const [traceOverlay, setTraceOverlay] = useState<string | null>(null);
  return (
    <ToastProvider>
      <TracesPage
        activeProject="test-project"
        onOpenTraceOverlay={(traceId: string) => setTraceOverlay(traceId)}
        activeOverlayTraceId={traceOverlay}
        {...children}
      />
      {traceOverlay && (
        <SlideInTraceDetail
          traceId={traceOverlay}
          activeProject="test-project"
          onClose={() => setTraceOverlay(null)}
        />
      )}
    </ToastProvider>
  );
}

function renderTracesPageWithOverlay(
  props: Partial<Omit<ComponentProps<typeof TracesPage>, "onOpenTraceOverlay">> &
    Partial<Pick<ComponentProps<typeof TracesPage>, "onOpenTraceOverlay">> = {}
) {
  return {
    ...render(<TracesOverlayWrapper children={props} />),
    setTraceOverlay: null as unknown as (id: string | null) => void,
  };
}

// Shared mock for the trace-detail API.  The overlay fetches a single
// trace by trace_id via GET /api/v1/traces/{id} — return the response
// shape the SlideInTraceDetail component expects.
const mockTraceDetail: unknown = {
  trace_id: "trace-a",
  project_id: "test-project",
  root_span: mockSpans[0],
  total_input_tokens: 210,
  total_output_tokens: 405,
  total_cost_usd: 0.11,
};

describe("trace detail opens as overlay (#281)", () => {
  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockImplementation((_input: RequestInfo | URL, _init?: RequestInit) => {
      // Route trace-detail fetch to our mock; everything else returns the list payload.
      const url = _input.toString();
      return Promise.resolve({
        ok: true,
        json: () =>
          url.includes("/api/v1/traces/")
            ? Promise.resolve(mockTraceDetail)
            : Promise.resolve({ spans: mockSpans, next: "", limit: 25 }),
      } as Response);
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Helper: get the first trace row's name cell (the .font-medium div in the table body).
  function getTraceNameCell() {
    const firstRow = document.querySelector("tbody tr");
    return firstRow?.querySelector(".font-medium") as HTMLElement | null;
  }

  it("opens trace detail as an overlay when the trace name is clicked", async () => {
    renderTracesPageWithOverlay();

    // Wait for the table to render, then click the first trace row's name cell.
    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    const traceNameCell = getTraceNameCell();
    expect(traceNameCell).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(traceNameCell!);
    });

    // The overlay should now show trace detail content — verify the Waterfall tab is present.
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Switch to waterfall view" })).toBeInTheDocument();
    });

    // The Traces list should still be visible in the DOM (mounted behind overlay).
    expect(document.querySelector(".font-medium")).toBeInTheDocument();
  });

  it("closes the overlay and returns to the Traces list", async () => {
    renderTracesPageWithOverlay();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    // Open the overlay by clicking the first trace row.
    const tableBody = document.querySelector("tbody");
    const firstRow = tableBody?.querySelector("tr");
    const traceNameCell = firstRow?.querySelector(".font-medium") as HTMLElement;
    await act(async () => {
      fireEvent.click(traceNameCell);
    });

    // Wait for the SlideInTraceDetail's waterfall tab to confirm the overlay has rendered.
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Switch to waterfall view" })).toBeInTheDocument();
    });

    // Close via the overlay close button (the X button, the second "Close trace detail" button).
    const closeButtons = await screen.findAllByRole("button", { name: /close trace detail/i });
    const xButton = closeButtons[1]; // second one is the X icon button
    await act(async () => {
      fireEvent.click(xButton);
    });

    // The overlay should be gone, and the Traces list should still be there.
    expect(
      screen.queryByRole("button", { name: /close trace detail/i })
    ).not.toBeInTheDocument();
    expect(screen.getByRole("table")).toBeInTheDocument();
  });

  it("keeps the Traces list scrollable behind the overlay", async () => {
    renderTracesPageWithOverlay();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    // Open the overlay by clicking the first trace row.
    const tableBody = document.querySelector("tbody");
    const firstRow = tableBody?.querySelector("tr");
    const traceNameCell = firstRow?.querySelector(".font-medium") as HTMLElement;
    await act(async () => {
      fireEvent.click(traceNameCell);
    });

    // All three trace names should be in the document (list + overlay title).
    const traceNames = screen.queryAllByText(/main-trace|simple-trace|leaf-span/);
    expect(traceNames.length).toBeGreaterThan(3); // at least list rows + overlay
  });
});

// ── Trace overlay keyboard navigation tests (#282) ──────────────────────

describe("trace overlay keyboard navigation (#282)", () => {
  // One trace-detail response per mock span, keyed by trace_id, so the
  // overlay's displayed title proves which trace is actually showing.
  const traceDetailById: Record<string, unknown> = Object.fromEntries(
    mockSpans.map((span) => [
      span.trace_id,
      {
        trace_id: span.trace_id,
        project_id: "test-project",
        root_span: span,
        total_input_tokens: span.input_tokens,
        total_output_tokens: span.output_tokens,
        total_cost_usd: span.cost_usd,
      },
    ]),
  );

  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockImplementation((_input: RequestInfo | URL) => {
      const url = _input.toString();
      const match = url.match(/\/api\/v1\/traces\/([^?]+)/);
      if (match) {
        const detail = traceDetailById[match[1]];
        return Promise.resolve({
          ok: !!detail,
          json: () => Promise.resolve(detail ?? {}),
        } as Response);
      }
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ spans: mockSpans, next: "", limit: 25 }),
      } as Response);
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function getOverlay() {
    return screen.getByLabelText("Trace detail");
  }

  function getTraceNameCell() {
    const firstRow = document.querySelector("tbody tr");
    return firstRow?.querySelector(".font-medium") as HTMLElement | null;
  }

  it("ArrowDown moves the overlay to the next trace without closing it", async () => {
    renderTracesPageWithOverlay();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(getTraceNameCell()!);
    });

    await waitFor(() => {
      expect(within(getOverlay()).getAllByText("main-trace")[0]).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.keyDown(window, { key: "ArrowDown" });
    });

    await waitFor(() => {
      expect(within(getOverlay()).getAllByText("simple-trace")[0]).toBeInTheDocument();
    });
    // The Traces list stays mounted behind the overlay throughout.
    expect(screen.getByRole("table")).toBeInTheDocument();
  });

  it("ArrowUp moves the overlay to the previous trace without closing it", async () => {
    renderTracesPageWithOverlay();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    const rows = document.querySelectorAll("tbody tr");
    const lastRowNameCell = rows[rows.length - 1].querySelector(".font-medium") as HTMLElement;
    await act(async () => {
      fireEvent.click(lastRowNameCell);
    });

    await waitFor(() => {
      expect(within(getOverlay()).getAllByText("leaf-span")[0]).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.keyDown(window, { key: "ArrowUp" });
    });

    await waitFor(() => {
      expect(within(getOverlay()).getAllByText("simple-trace")[0]).toBeInTheDocument();
    });
  });

  it("is a no-op at the end of the list (ArrowDown on the last trace)", async () => {
    renderTracesPageWithOverlay();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    const rows = document.querySelectorAll("tbody tr");
    const lastRowNameCell = rows[rows.length - 1].querySelector(".font-medium") as HTMLElement;
    await act(async () => {
      fireEvent.click(lastRowNameCell);
    });

    await waitFor(() => {
      expect(within(getOverlay()).getAllByText("leaf-span")[0]).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.keyDown(window, { key: "ArrowDown" });
    });

    // Still on the last trace — no error, no close.
    expect(within(getOverlay()).getAllByText("leaf-span")[0]).toBeInTheDocument();
  });

  it("is a no-op at the start of the list (ArrowUp on the first trace)", async () => {
    renderTracesPageWithOverlay();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(getTraceNameCell()!);
    });

    await waitFor(() => {
      expect(within(getOverlay()).getAllByText("main-trace")[0]).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.keyDown(window, { key: "ArrowUp" });
    });

    expect(within(getOverlay()).getAllByText("main-trace")[0]).toBeInTheDocument();
  });

  it("ignores ArrowDown/ArrowUp when no overlay is open", async () => {
    renderTracesPageWithOverlay();

    await waitFor(() => {
      expect(screen.getByRole("table")).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.keyDown(window, { key: "ArrowDown" });
    });

    expect(screen.queryByLabelText("Trace detail")).not.toBeInTheDocument();
  });
});

// ── Span Kind icon tests (issue #277) ───────────────────────────

describe("Span Kind icons in Traces list (issue #277)", () => {
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

  // Helper: find the kind-icon wrapper style by navigating from the name text
  // to the SpanKindIcon wrapper, which carries a `title` matching the kind
  // and a `color` style tinting the inline SVG icon (shared spanKindVisuals
  // module — see ui/src/modules/spanKindVisuals.tsx, issue #276).
  function findKindIconStyle(name: string, kind: string): string | null {
    const nameSpan = screen.getByText(name);
    expect(nameSpan).toBeInTheDocument();
    const button = nameSpan.closest("button")!;
    expect(button).toBeInTheDocument();
    const iconSpansList = button.querySelectorAll("[title]");
    expect(iconSpansList.length).toBeGreaterThan(0);
    let iconSpan: HTMLElement | null = null;
    for (let i = 0; i < iconSpansList.length; i++) {
      const el = iconSpansList[i] as HTMLElement;
      if (el.getAttribute("title") === kind) {
        iconSpan = el;
        break;
      }
    }
    expect(iconSpan).not.toBeNull();
    expect(iconSpan).toBeInTheDocument();
    expect(iconSpan!.querySelector("svg")).toBeInTheDocument();
    return iconSpan?.getAttribute("style") ?? null;
  }

  it("renders a SpanKindIcon next to each trace name", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // Verify each trace name has a kind icon.
    findKindIconStyle("main-trace", "chain");
    findKindIconStyle("simple-trace", "chain");
    findKindIconStyle("leaf-span", "llm");
  });

  it("renders the correct color for an LLM root span", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("leaf-span")).toBeInTheDocument();
    });

    // trace-c is kind: "llm" — spanKindVisuals llm color is #7C3AED.
    const style = findKindIconStyle("leaf-span", "llm");
    expect(style).toContain("color");
    expect(style).toContain("rgb(124, 58, 237)");
  });

  it("renders the correct color for a chain root span", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });

    // trace-a is kind: "chain" — spanKindVisuals chain color is #22D3EE.
    const style = findKindIconStyle("main-trace", "chain");
    expect(style).toContain("color");
    expect(style).toContain("rgb(34, 211, 238)");
  });

  it("renders a different color for different span kinds in the same view", async () => {
    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
      expect(screen.getByText("leaf-span")).toBeInTheDocument();
    });

    // main-trace is chain, leaf-span is llm — they must differ.
    const mainStyle = findKindIconStyle("main-trace", "chain");
    const leafStyle = findKindIconStyle("leaf-span", "llm");

    expect(mainStyle).not.toBe(leafStyle);
  });
});

// ── Issue #340: load states (pending / empty / filtered-empty / error) ──

describe("issue #340: traces list load states", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("never shows the onboarding empty state while the query is pending", async () => {
    // A fetch that never settles — the query is permanently in flight.
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));

    renderTracesPage();

    // Loading skeleton is visible; onboarding is not.
    await waitFor(() => {
      expect(document.querySelector(".animate-pulse")).toBeInTheDocument();
    });
    expect(screen.queryByText("No traces yet")).not.toBeInTheDocument();
    expect(screen.queryByText(/Get started/)).not.toBeInTheDocument();
  });

  it("shows onboarding only on a confirmed empty result for an unfiltered default query", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ spans: [], next: "", limit: 25 }),
    } as Response);

    renderTracesPage();

    await waitFor(() => {
      expect(screen.getByText("No traces yet")).toBeInTheDocument();
    });
  });

  it("shows a filtered-empty message instead of onboarding when a narrow time range is active", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ spans: [], next: "", limit: 25 }),
    } as Response);

    renderTracesPage({ timeRange: "1h" });

    await waitFor(() => {
      expect(screen.getByText(/No traces match/)).toBeInTheDocument();
    });
    expect(screen.queryByText("No traces yet")).not.toBeInTheDocument();
  });

  it("surfaces an error state with a working Retry when the query fails", async () => {
    let call = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
      call++;
      if (call === 1) throw new TypeError("network down");
      return {
        ok: true,
        json: () => Promise.resolve({ spans: mockSpans, next: "", limit: 25 }),
      } as Response;
    });

    renderTracesPage();

    // Error state, not onboarding.
    await waitFor(() => {
      expect(screen.getByRole("alert")).toBeInTheDocument();
    });
    expect(screen.queryByText("No traces yet")).not.toBeInTheDocument();

    // Retry refetches and renders the data.
    fireEvent.click(screen.getByRole("button", { name: /Retry/ }));
    await waitFor(() => {
      expect(screen.getByText("main-trace")).toBeInTheDocument();
    });
  });

  it("aborts a hung query after the 30s client timeout and shows the error state", async () => {
    vi.useFakeTimers();
    try {
      vi.spyOn(globalThis, "fetch").mockImplementation((_url, options) => {
        return new Promise((_resolve, reject) => {
          options?.signal?.addEventListener("abort", () => {
            reject(new DOMException("aborted", "AbortError"));
          });
        });
      });

      renderTracesPage();

      // Just before the timeout: still loading, no error.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(29_000);
      });
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();

      // Past the timeout: the request is aborted and the error state shows.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(2_000);
      });
      expect(screen.getByRole("alert")).toBeInTheDocument();
      expect(screen.queryByText("No traces yet")).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
});
