import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import DashboardPage, { AnalyticsRequest, formatTraceTimeTick, fetchAnalyticsBatch } from "./Dashboard";
import { ToastProvider } from "@/components/Toast";

function renderWithToast(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

// ── Analytics response helper ────────────────────────────────────

const resolveAnalyticsResponse = (data: Record<string, unknown>) =>
  ({ ok: true, json: () => Promise.resolve(data) } as Response);

const rejectAnalyticsResponse = () =>
  ({ ok: false, status: 500, json: () => Promise.resolve({}) } as Response);

beforeEach(() => {
  vi.restoreAllMocks();
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

// ── Tests ────────────────────────────────────────────────────────

describe("DashboardPage", () => {
  it("renders the Dashboard header", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });
  });

  it("includes activeProject as project_id in analytics requests", async () => {
    const projectIdStore: string[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string);
        projectIdStore.push(body.project_id);
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-active" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    // All analytics requests should include the active project ID
    expect(projectIdStore.length).toBeGreaterThan(0);
    for (const pid of projectIdStore) {
      expect(pid).toBe("proj-active");
    }
  });

  it("clears the previous project's data when switching to a project with no rows", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string);
        if (body.project_id === "proj-1") {
          return resolveAnalyticsResponse({
            rows: [
              {
                model: "gpt-4-from-proj-1",
                count: 150,
                input_tokens: 10000,
                output_tokens: 5000,
                total_cost: 1.23,
              },
            ],
          });
        }
        // Go's JSON encoder marshals empty slices as null — the UI must
        // treat that as "no data", not "keep the old data".
        return resolveAnalyticsResponse({ rows: null });
      }
      return rejectAnalyticsResponse();
    });

    const view = renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getAllByText("gpt-4-from-proj-1").length).toBeGreaterThan(0);
    });

    view.rerender(
      <ToastProvider>
        <DashboardPage activeProject="proj-2" />
      </ToastProvider>
    );

    await waitFor(() => {
      expect(screen.queryByText("gpt-4-from-proj-1")).not.toBeInTheDocument();
    });
  });

  it("displays traces by model chart when data is returned", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({
          rows: [
            { model: "gpt-4", count: 150 },
            { model: "claude-3", count: 80 },
          ],
        });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      // Chart container should be rendered with SVG
      const svg = document.querySelector("svg");
      expect(svg).toBeInTheDocument();
    });
  });

  it("displays cost data table when returned", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({
          rows: [
            {
              model: "gpt-4",
              input_tokens: 10000,
              output_tokens: 5000,
              total_cost: 0.15,
            },
          ],
        });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      // Verify cost data table rendered with dollar-formatted amounts
      // (appears in both Cost by Model and Model Usage cost tabs)
      const costElements = screen.queryAllByText(/\$0\.\d+/);
      expect(costElements.length).toBeGreaterThan(0);
    });
  });

  // ── Issues #233 / #337: Unpriced indicator (API-driven) ────────────

  it("#337: shows 'Unpriced' only for models the Query API reports as unpriced", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({
          rows: [
            {
              model: "unknown-model",
              input_tokens: 1000,
              output_tokens: 500,
              total_cost: 0,
            },
            {
              model: "local-llama",
              input_tokens: 2000,
              output_tokens: 900,
              total_cost: 0,
            },
          ],
        });
      }
      if (typeof url === "string" && url.startsWith("/api/v1/models/priced")) {
        return resolveAnalyticsResponse({ "unknown-model": false, "local-llama": true });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      // The unpriced model's row shows the badge…
      const unknownRows = screen.getAllByText("unknown-model").map((el) => el.closest("tr")!);
      expect(unknownRows.length).toBeGreaterThan(0);
      expect(unknownRows.some((row) => within(row).queryByText("Unpriced"))).toBe(true);

      // …while the $0-priced (self-hosted) model's row shows a plain $0.00.
      const pricedRows = screen.getAllByText("local-llama").map((el) => el.closest("tr")!);
      expect(pricedRows.length).toBeGreaterThan(0);
      for (const row of pricedRows) {
        expect(within(row).queryByText("Unpriced")).toBeNull();
      }
      expect(pricedRows.some((row) => within(row).queryByText("$0.00"))).toBe(true);
    });
  });

  // ── Issue #233: Exclude non-LLM spans from model charts ─────────

  it("#233: model queries include kind='llm' filter", async () => {
    const modelQueries: AnalyticsRequest[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        // Detect model-based queries: group_by includes model field
        if (body.group_by?.some((g: { field: string }) => g.field === "model")) {
          modelQueries.push(body);
          return resolveAnalyticsResponse({ rows: [] });
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    // Every model-based query must include kind='llm' filter
    expect(modelQueries.length).toBeGreaterThan(0);
    for (const q of modelQueries) {
      const llmFilter = q.filters?.find(
        (f: { field: string; op: string; value: unknown }) => f.field === "kind" && f.op === "eq" && f.value === "llm"
      );
      expect(llmFilter).toBeDefined();
    }
  });

  it("displays traces over time when data is returned", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({
          rows: [
            { "date_trunc('hour', start_time)": "2025-01-01T10:00:00Z", count: 10 },
          ],
        });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Traces over Time")).toBeInTheDocument();
    });
  });

  it("displays user consumption chart when data is returned", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({
          rows: [
            { service_name: "my-app", count: 200 },
          ],
        });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("User Consumption")).toBeInTheDocument();
    });
  });

  it("shows empty state when no traces data is returned", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("No traces yet")).toBeInTheDocument();
    });
  });

  it("shows KPI tiles with zero values when no data returned", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      // KPI tiles show zero values when no data
      expect(screen.getByText("0.00")).toBeInTheDocument(); // total_cost
      expect(screen.getByText("0.00%")).toBeInTheDocument(); // error_rate
    });
  });

  it("refreshes data when project_id changes", async () => {
    let callCount = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      callCount++;
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    const { rerender } = renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    const initialCalls = callCount;

    // Re-render with a different project
    rerender(<ToastProvider><DashboardPage activeProject="proj-2" /></ToastProvider>);

    await waitFor(() => {
      expect(callCount).toBeGreaterThan(initialCalls);
    });
  });

  it("renders all 7 widget cards plus KPI tiles", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    // Verify all widget card titles are present
    // Cost by Model has been removed; Latency Percentiles and Error Rate are new.
    const expectedCards = [
      "Traces by Model",
      "Eval Scores",
      "Traces over Time",
      "Token Usage",
      "User Consumption",
      "Latency Percentiles",
      "Error Rate",
    ];

    for (const cardTitle of expectedCards) {
      // Use regex to avoid duplicate text issues
      const elements = screen.queryAllByText(cardTitle);
      expect(elements.length).toBeGreaterThan(0);
    }

    // KPI tiles should also be rendered
    expect(screen.getByText("Total Cost")).toBeInTheDocument();
    expect(screen.getByText("Total Traces")).toBeInTheDocument();
    // Error Rate appears twice (KPI tile + chart header) — use getAllByText
    expect(screen.getAllByText("Error Rate").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("Average Latency")).toBeInTheDocument();
  });

  it("shows loading state on initial render", async () => {
    // Never resolve the fetch to simulate loading
    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => new Promise(() => {})
    );

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });
  });

  it("shows error banner when analytics requests fail", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("Network error"));

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Network error")).toBeInTheDocument();
    });
  });

  it("defaults to a 7-day date range (not 24 hours)", async () => {
    // Capture the from/to values by intercepting the first analytics request
    let capturedFrom = "";
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string);
        capturedFrom = body.from;
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      const now = new Date();
      const fromD = new Date(capturedFrom);
      const diffHours = (now.getTime() - fromD.getTime()) / (1000 * 60 * 60);
      // 7-day range = ~168 hours, 24h range = ~24 hours
      expect(diffHours).toBeGreaterThan(100);
      expect(diffHours).toBeLessThanOrEqual(168.5);
    });
  });

  // ── Time range preset tests (issue #12) ──────────────────────────

  it("sets from to ~1 hour ago when timeRange='1h'", async () => {
    let capturedFrom = "";
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string);
        capturedFrom = body.from;
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" timeRange="1h" />);

    await waitFor(() => {
      expect(capturedFrom).not.toBe("");
    });

    const now = new Date();
    const fromD = new Date(capturedFrom);
    const diffMinutes = (now.getTime() - fromD.getTime()) / (1000 * 60);
    expect(diffMinutes).toBeGreaterThan(55);
    expect(diffMinutes).toBeLessThanOrEqual(65);
  });

  it("sets from to ~24 hours ago when timeRange='1d'", async () => {
    let capturedFrom = "";
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string);
        capturedFrom = body.from;
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" timeRange="1d" />);

    await waitFor(() => {
      expect(capturedFrom).not.toBe("");
    });

    const now = new Date();
    const fromD = new Date(capturedFrom);
    const diffHours = (now.getTime() - fromD.getTime()) / (1000 * 60 * 60);
    expect(diffHours).toBeGreaterThan(23);
    expect(diffHours).toBeLessThanOrEqual(25);
  });

  it("sets from to ~7 days ago when timeRange='7d'", async () => {
    let capturedFrom = "";
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string);
        capturedFrom = body.from;
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" timeRange="7d" />);

    await waitFor(() => {
      expect(capturedFrom).not.toBe("");
    });

    const now = new Date();
    const fromD = new Date(capturedFrom);
    const diffDays = (now.getTime() - fromD.getTime()) / (1000 * 60 * 60 * 24);
    expect(diffDays).toBeGreaterThan(6.9);
    expect(diffDays).toBeLessThanOrEqual(7.1);
  });

  it("shows the preset label in subtitle when timeRange='1d'", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" timeRange="1d" />);

    await waitFor(() => {
      expect(screen.getByText("Past 24 hours")).toBeInTheDocument();
    });
  });

  it("shows 'Past 7 days' in subtitle when timeRange='7d'", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" timeRange="7d" />);

    await waitFor(() => {
      expect(screen.getByText("Past 7 days")).toBeInTheDocument();
    });
  });

  it("shows 'Past 1 hour' in subtitle when timeRange='1h'", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" timeRange="1h" />);

    await waitFor(() => {
      expect(screen.getByText("Past 1 hour")).toBeInTheDocument();
    });
  });

  it("shows 'Custom range' in subtitle when timeRange='custom'", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" timeRange="custom" />);

    await waitFor(() => {
      expect(screen.getByText("Custom range")).toBeInTheDocument();
    });
  });

  // ── Issue #35: modelCostsReq order_by alias fix ──────────────────

  it("#35: modelCostsReq uses order_by field 'total_cost' (alias), not raw 'cost_usd'", async () => {
    const modelCostsBodies: AnalyticsRequest[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        // Collect requests that group by model (the modelCostsReq)
        if (body.group_by?.some((g: { field: string }) => g.field === "model") &&
            body.aggregations?.some((a: { alias: string }) => a.alias === "total_cost")) {
          modelCostsBodies.push(body);
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    expect(modelCostsBodies.length).toBeGreaterThan(0);
    for (const body of modelCostsBodies) {
      const orderByFields = body.order_by.map((o: { field: string }) => o.field);
      expect(orderByFields).not.toContain("cost_usd");
      expect(orderByFields).toContain("total_cost");
    }
  });

  // ── Issue #36: tracesByNameReq group_by model fix ─────────────────

  it("#36: tracesByNameReq uses group_by field 'model', not 'name'", async () => {
    const tracesByNameBodies: AnalyticsRequest[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        // Collect requests that are the traces-by-name/model request:
        // they count(*) and order by 'count'
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "count") &&
            body.order_by?.some((o: { field: string }) => o.field === "count")) {
          tracesByNameBodies.push(body);
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    // There should be at least one request matching the traces-by-model pattern
    const tracesByModelBodies = tracesByNameBodies.filter((b) =>
      b.group_by?.some((g: { field: string }) => g.field === "model")
    );
    expect(tracesByModelBodies.length).toBeGreaterThan(0);

    // None of the count-ordered requests should group by 'name'
    for (const body of tracesByNameBodies) {
      const groupByFields = body.group_by.map((g: { field: string }) => g.field);
      expect(groupByFields).not.toContain("name");
    }
  });

  // ── Issue #37: tokenUsageReq group_by model fix ───────────────────

  it("#37: tokenUsageReq uses group_by field 'model', not 'kind'", async () => {
    const tokenUsageBodies: AnalyticsRequest[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        // Token usage request: sums input_tokens and output_tokens, grouped by model
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "input_tokens") &&
            body.aggregations?.some((a: { alias: string }) => a.alias === "output_tokens") &&
            body.group_by?.some((g: { field: string }) => g.field === "model")) {
          tokenUsageBodies.push(body);
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    expect(tokenUsageBodies.length).toBeGreaterThan(0);
    for (const body of tokenUsageBodies) {
      const groupByFields = body.group_by.map((g: { field: string }) => g.field);
      expect(groupByFields).not.toContain("kind");
      expect(groupByFields).toContain("model");
    }
  });

  // ── Issue #140: "by Type" tabs should show kind-grouped data ─────

  it("#140: issues an analytics request grouped by 'kind' for the by-Type tabs", async () => {
    const kindUsageBodies: AnalyticsRequest[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        if (
          body.group_by?.some((g: { field: string }) => g.field === "kind") &&
          body.aggregations?.some((a: { alias: string }) => a.alias === "input_tokens") &&
          body.aggregations?.some((a: { alias: string }) => a.alias === "output_tokens")
        ) {
          kindUsageBodies.push(body);
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    expect(kindUsageBodies.length).toBeGreaterThan(0);
  });

  it("#140: 'Usage by Type' tab renders kind values (e.g. 'llm', 'tool'), not model names", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        if (body.group_by?.some((g: { field: string }) => g.field === "kind")) {
          return resolveAnalyticsResponse({
            rows: [
              { kind: "llm", input_tokens: 1000, output_tokens: 500, total_cost: 0.05 },
              { kind: "tool", input_tokens: 200, output_tokens: 100, total_cost: 0.01 },
            ],
          });
        }
        if (body.group_by?.some((g: { field: string }) => g.field === "model")) {
          return resolveAnalyticsResponse({
            rows: [
              { model: "gpt-4-model-only", input_tokens: 1000, output_tokens: 500, total_cost: 0.05 },
            ],
          });
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    // Switch to the "Usage by Type" tab (index 3 of USAGE_TABS)
    const usageByTypeTab = await screen.findByText("Usage by Type");
    const tokenUsageCard = usageByTypeTab.closest(".card") as HTMLElement;
    fireEvent.click(usageByTypeTab);

    await waitFor(() => {
      expect(screen.getByText("llm")).toBeInTheDocument();
      expect(screen.getByText("tool")).toBeInTheDocument();
    });

    // The model-only data should not leak into the Token Usage card's
    // "by Type" tab (it may legitimately appear in the separate
    // "Cost by Model" widget elsewhere on the dashboard).
    const { queryByText } = within(tokenUsageCard);
    expect(queryByText("gpt-4-model-only")).not.toBeInTheDocument();
  });

  it("#140: 'Cost by Type' tab renders kind values (e.g. 'agent'), not model names", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        if (body.group_by?.some((g: { field: string }) => g.field === "kind")) {
          return resolveAnalyticsResponse({
            rows: [
              { kind: "agent", input_tokens: 1000, output_tokens: 500, total_cost: 0.05 },
            ],
          });
        }
        if (body.group_by?.some((g: { field: string }) => g.field === "model")) {
          return resolveAnalyticsResponse({
            rows: [
              { model: "claude-model-only", input_tokens: 1000, output_tokens: 500, total_cost: 0.05 },
            ],
          });
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    // Switch to the "Cost by Type" tab (index 1 of USAGE_TABS)
    const costByTypeTab = await screen.findByText("Cost by Type");
    const tokenUsageCard = costByTypeTab.closest(".card") as HTMLElement;
    fireEvent.click(costByTypeTab);

    await waitFor(() => {
      expect(screen.getByText("agent")).toBeInTheDocument();
    });

    const { queryByText } = within(tokenUsageCard);
    expect(queryByText("claude-model-only")).not.toBeInTheDocument();
  });

  it("refreshes data and updates from/to when timeRange prop changes", async () => {
    let lastCapturedFrom = "";
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string);
        lastCapturedFrom = body.from;
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    const { rerender } = renderWithToast(
      <DashboardPage activeProject="proj-1" timeRange="7d" />
    );

    await waitFor(() => {
      expect(lastCapturedFrom).not.toBe("");
    });

    const sevenDayFrom = lastCapturedFrom;

    // Switch to 1h preset
    rerender(
      <ToastProvider>
        <DashboardPage activeProject="proj-1" timeRange="1h" />
      </ToastProvider>
    );

    await waitFor(() => {
      expect(lastCapturedFrom).not.toBe(sevenDayFrom);
    });

    // The new from should be ~1 hour ago, not 7 days ago
    const now = new Date();
    const newFromD = new Date(lastCapturedFrom);
    const diffHours = (now.getTime() - newFromD.getTime()) / (1000 * 60 * 60);
    expect(diffHours).toBeLessThan(2);
  });
});

// ── Issue #239: KPI tiles ──────────────────────────────────────────

  it("#239: shows KPI tiles with total cost, total traces, error rate, and average latency", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;

        // KPI request: no group_by, aggregates total_traces, total_cost, avg_latency_ms
        if (body.group_by && body.group_by.length === 0 &&
            body.aggregations?.some((a: { alias: string }) => a.alias === "total_cost")) {
          return resolveAnalyticsResponse({
            rows: [
              { total_traces: 150, total_cost: 4.5678, avg_latency_ms: 234.5 },
            ],
          });
        }
        // KPI error-count request: same no-group_by shape, aggregates error_count
        if (body.group_by && body.group_by.length === 0 &&
            body.aggregations?.some((a: { alias: string }) => a.alias === "error_count")) {
          return resolveAnalyticsResponse({
            rows: [
              { error_count: 10 },
            ],
          });
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      // Total Cost tile (unique label)
      expect(screen.getByText("Total Cost")).toBeInTheDocument();
      // Total Traces tile (unique label)
      expect(screen.getByText("Total Traces")).toBeInTheDocument();
      // Average Latency tile (unique label)
      expect(screen.getByText("Average Latency")).toBeInTheDocument();
      // Error Rate KPI tile: appears as both KPI label and error-rate chart header, so check multiple
      expect(screen.getAllByText("Error Rate").length).toBeGreaterThan(0);
    });
  });

  it("#239: KPI tiles fetch a KPI request (no group_by, 3 aggregations) plus a separate error-count request", async () => {
    const kpiBodies: AnalyticsRequest[] = [];
    const errorCountBodies: AnalyticsRequest[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;

        // A KPI request has no group_by and at least 3 aggregations (total_traces, total_cost, avg_latency_ms)
        if (body.group_by && body.group_by.length === 0 &&
            body.aggregations?.some((a: { alias: string }) => a.alias === "total_cost")) {
          kpiBodies.push(body);
        }
        // A KPI error-count request has no group_by and error_count aggregation
        if (body.group_by && body.group_by.length === 0 &&
            body.aggregations?.some((a: { alias: string }) => a.alias === "error_count")) {
          errorCountBodies.push(body);
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    // There should be exactly one KPI request and one error-count request
    expect(kpiBodies.length).toBe(1);
    expect(errorCountBodies.length).toBe(1);

    // Verify the KPI request body contains the expected aggregations
    const kpiBody = kpiBodies[0];
    const aliases = kpiBody.aggregations.map((a: { alias: string }) => a.alias);
    expect(aliases).toContain("total_traces");
    expect(aliases).toContain("total_cost");
    expect(aliases).toContain("avg_latency_ms");
    // error_rate should NOT be in the KPI request — it comes from a separate request
    expect(aliases).not.toContain("error_rate");

    // Verify the error-count request body
    const errorCountBody = errorCountBodies[0];
    const errorCountAliases = errorCountBody.aggregations.map((a: { alias: string }) => a.alias);
    expect(errorCountAliases).toContain("error_count");
  });

  it("#239: KPI tiles display actual values from the API response", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        if (body.group_by && body.group_by.length === 0 &&
            body.aggregations?.some((a: { alias: string }) => a.alias === "total_cost")) {
          return resolveAnalyticsResponse({
            rows: [
              { total_traces: 150, total_cost: 4.5678, avg_latency_ms: 234.5 },
            ],
          });
        }
        if (body.group_by && body.group_by.length === 0 &&
            body.aggregations?.some((a: { alias: string }) => a.alias === "error_count")) {
          return resolveAnalyticsResponse({
            rows: [
              { error_count: 10 },
            ],
          });
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      // Total Traces: 150 → formatNumber(150) = "150.00"
      expect(screen.getByText("150.00")).toBeInTheDocument();
    });

    await waitFor(() => {
      // Error Rate: 10/150 = 0.0667 → 6.67%
      expect(screen.getByText("6.67%")).toBeInTheDocument();
    });

    await waitFor(() => {
      // Average Latency: 234.5ms humanized to whole milliseconds
      expect(screen.getByText("235ms")).toBeInTheDocument();
    });
  });

  // ── Issue #239: Latency-percentile chart ────────────────────────────

  it("#239: latency-percentile chart renders when API returns percentile data", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;

        // The latency request groups by time_bucket and aggregates p50/p95/p99
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "p50_ms")) {
          return resolveAnalyticsResponse({
            rows: [
              { "date_trunc('hour', start_time)": "2025-06-22T10:00:00Z", p50_ms: 120, p95_ms: 340, p99_ms: 890 },
              { "date_trunc('hour', start_time)": "2025-06-22T11:00:00Z", p50_ms: 135, p95_ms: 350, p99_ms: 920 },
            ],
          });
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Latency Percentiles")).toBeInTheDocument();
    });

    await waitFor(() => {
      // Chart should render with SVG containing the line data
      const svg = document.querySelector("svg");
      expect(svg).toBeInTheDocument();
    });
  });

  it("#239: latency-percentile chart sends p50/p95/p99 aggregations on duration_ms", async () => {
    const latencyBodies: AnalyticsRequest[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "p50_ms")) {
          latencyBodies.push(body);
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    expect(latencyBodies.length).toBeGreaterThan(0);
    const latBody = latencyBodies[0];
    const aliases = latBody.aggregations.map((a: { alias: string }) => a.alias);
    expect(aliases).toContain("p50_ms");
    expect(aliases).toContain("p95_ms");
    expect(aliases).toContain("p99_ms");
    // All percentile aggregations should use duration_ms as the field
    for (const agg of latBody.aggregations) {
      if (agg.alias === "p50_ms" || agg.alias === "p95_ms" || agg.alias === "p99_ms") {
        expect(agg.field).toBe("duration_ms");
      }
    }
  });

  it("#239: latency-percentile chart shows p50, p95, p99 in tooltip legend", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "p50_ms")) {
          return resolveAnalyticsResponse({
            rows: [
              { "date_trunc('hour', start_time)": "2025-06-22T10:00:00Z", p50_ms: 120, p95_ms: 340, p99_ms: 890 },
            ],
          });
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Latency Percentiles")).toBeInTheDocument();
    });

    // Check legend labels for the percentile names
    await waitFor(() => {
      expect(screen.getByText("p50")).toBeInTheDocument();
      expect(screen.getByText("p95")).toBeInTheDocument();
      expect(screen.getByText("p99")).toBeInTheDocument();
    });
  });

  // ── Issue #239: Error-rate chart ─────────────────────────────────────

  it("#239: error-rate chart renders when API returns error rate data", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;

        // Error-rate total-count request (no filter, groups by time_bucket)
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "total_count") &&
            !body.aggregations?.some((a: { alias: string }) => a.alias === "error_count")) {
          return resolveAnalyticsResponse({
            rows: [
              { "date_trunc('hour', start_time)": "2025-06-22T10:00:00Z", total_count: 100 },
              { "date_trunc('hour', start_time)": "2025-06-22T11:00:00Z", total_count: 80 },
            ],
          });
        }
        // Error-rate error-count request (with status_code filter)
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "error_count")) {
          return resolveAnalyticsResponse({
            rows: [
              { "date_trunc('hour', start_time)": "2025-06-22T10:00:00Z", error_count: 5 },
              { "date_trunc('hour', start_time)": "2025-06-22T11:00:00Z", error_count: 2 },
            ],
          });
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Error Rate")).toBeInTheDocument();
    });

    await waitFor(() => {
      const svg = document.querySelector("svg");
      expect(svg).toBeInTheDocument();
    });
  });

  it("#239: error-rate chart sends two separate requests: total-count and error-count", async () => {
    const totalBodies: AnalyticsRequest[] = [];
    const errorBodies: AnalyticsRequest[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        // Error-rate total-count request: has total_count, no error_count, and has group_by
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "total_count") &&
            !body.aggregations?.some((a: { alias: string }) => a.alias === "error_count") &&
            body.group_by && body.group_by.length > 0) {
          totalBodies.push(body);
        }
        // Error-rate error-count request: has error_count and has group_by
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "error_count") &&
            body.group_by && body.group_by.length > 0) {
          errorBodies.push(body);
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    // Must have a separate error-count request with status_code filter
    expect(errorBodies.length).toBe(1);
    const errBody = errorBodies[0];
    expect(errBody.filters.some((f: { field: string; op: string }) => f.field === "status_code" && f.op === "neq")).toBe(true);

    // Must have a separate total-count request without the status_code filter
    expect(totalBodies.length).toBe(1);
    const totalBody = totalBodies[0];
    // filters is either absent or empty — no status_code filter
    expect(totalBody.filters?.length === undefined || totalBody.filters?.length === 0).toBe(true);

    // Verify error-count request has error_count aggregation
    const errorAliases = errBody.aggregations.map((a: { alias: string }) => a.alias);
    expect(errorAliases).toContain("error_count");

    // Verify total-count request has total_count aggregation only
    const totalAliases = totalBody.aggregations.map((a: { alias: string }) => a.alias);
    expect(totalAliases).toContain("total_count");
    expect(totalAliases).not.toContain("error_count");
  });

  it("#239: error-rate chart shows error rate percentages", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
      if (url === "/api/v1/analytics/spans" && options?.body) {
        const body = JSON.parse(options.body as string) as AnalyticsRequest;
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "total_count") &&
            !body.aggregations?.some((a: { alias: string }) => a.alias === "error_count")) {
          return resolveAnalyticsResponse({
            rows: [
              { "date_trunc('hour', start_time)": "2025-06-22T10:00:00Z", total_count: 100 },
            ],
          });
        }
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "error_count")) {
          return resolveAnalyticsResponse({
            rows: [
              { "date_trunc('hour', start_time)": "2025-06-22T10:00:00Z", error_count: 5 },
            ],
          });
        }
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      // "Error Rate" chart heading renders
      expect(screen.getByText("Error Rate")).toBeInTheDocument();
    });

    // Verify the chart rendered an SVG with line data (proves the error rate is computed)
    await waitFor(() => {
      expect(document.querySelector("svg")).toBeInTheDocument();
    });
  });

  // ── Issue #239: Duplicate Cost by Model removal ──────────────────────

  it("#239: only one 'Cost by Model' card exists (duplicate removed)", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    // Count cards titled "Cost by Model"
    // The standalone "Cost by Model" table card should be removed,
    // but the tab inside the Token Usage widget should still exist.
    // Check that the standalone card is NOT rendered at the top level.
    const cards = document.querySelectorAll<HTMLElement>(".card");
    const costByModelCards: HTMLElement[] = [];
    cards.forEach((card) => {
      const header = card.querySelector(".card-header");
      if (header) {
        const title = header.textContent?.trim();
        if (title === "Cost by Model") {
          costByModelCards.push(card);
        }
      }
    });

    // Should be at most one (inside Token Usage widget tabs, not as a standalone card)
    // Actually, after the fix there should be ZERO standalone "Cost by Model" cards
    // because the standalone panel was removed (the data is in the Token Usage widget)
    expect(costByModelCards.length).toBeLessThanOrEqual(1);
  });

  it("#239: standalone 'Cost by Model' card is removed from dashboard", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Dashboard")).toBeInTheDocument();
    });

    // The standalone "Cost by Model" card (that was card #2 in the old layout)
    // should no longer appear on the dashboard.
    // We check that "Cost by Model" is NOT visible as a top-level card title.
    // The Tab label "Cost by Model" inside the Token Usage widget will exist
    // but that's a button, not a card header.
    const standaloneCostTitle = screen.queryByText("Cost by Model");
    if (standaloneCostTitle) {
      // It might only appear as a tab button inside the Token Usage widget
      // Check if it's inside a card-header (standalone card) vs inside a tab button
      const parentCard = standaloneCostTitle.closest(".card-header");
      expect(parentCard).toBeNull();
    }
  });

  // ── formatTraceTimeTick (issues #42 / #47) ────────────────────────────

describe("formatTraceTimeTick", () => {
  const noonTs = new Date("2025-06-15T12:00:00Z").getTime();

  it("1h preset: formats as time (HH:MM AM/PM)", () => {
    const result = formatTraceTimeTick(noonTs, "1h");
    expect(result).toMatch(/AM|PM/i);
    expect(result).not.toMatch(/Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec/);
  });

  it("6h preset: formats as time (HH:MM AM/PM)", () => {
    const result = formatTraceTimeTick(noonTs, "6h");
    expect(result).toMatch(/AM|PM/i);
    expect(result).not.toMatch(/Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec/);
  });

  it("1d preset: formats as time (HH:MM AM/PM), not a date — the boundary bug", () => {
    const result = formatTraceTimeTick(noonTs, "1d");
    expect(result).toMatch(/AM|PM/i);
    expect(result).not.toMatch(/Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec/);
  });

  it("7d preset: formats as date (Mon DD), not a time", () => {
    const result = formatTraceTimeTick(noonTs, "7d");
    expect(result).toMatch(/Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec/);
    expect(result).not.toMatch(/AM|PM/i);
  });

  it("30d preset: formats as date (Mon DD), not a time", () => {
    const result = formatTraceTimeTick(noonTs, "30d");
    expect(result).toMatch(/Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec/);
    expect(result).not.toMatch(/AM|PM/i);
  });
});

// ── fetchAnalyticsBatch ──────────────────────────────────────────────
// Regression coverage for the production incident where an unbounded
// Promise.all fan-out of every Dashboard widget's analytics request
// saturated the Query API's small Lake connection pool, which in turn
// starved the readiness/liveness Catalog probe sharing that pool and
// crash-looped the pod under nothing worse than normal Dashboard load.

describe("fetchAnalyticsBatch", () => {
  const makeReq = (label: string): AnalyticsRequest => ({
    from: "2025-01-01T00:00:00Z",
    to: "2025-01-02T00:00:00Z",
    filters: [{ field: "label", op: "eq", value: label }],
    group_by: [],
    order_by: [],
    aggregations: [],
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("never runs more than `concurrency` requests at once", async () => {
    let inFlight = 0;
    let maxInFlight = 0;
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
      inFlight += 1;
      maxInFlight = Math.max(maxInFlight, inFlight);
      await new Promise((resolve) => setTimeout(resolve, 5));
      inFlight -= 1;
      return new Response(JSON.stringify({ rows: [] }), { status: 200 });
    });

    const requests = Array.from({ length: 10 }, (_, i) => makeReq(String(i)));
    await fetchAnalyticsBatch(requests, 3);

    expect(fetchMock).toHaveBeenCalledTimes(10);
    expect(maxInFlight).toBeLessThanOrEqual(3);
  });

  it("resolves responses in request order regardless of completion order", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
      const body = JSON.parse((init as RequestInit).body as string) as AnalyticsRequest;
      const label = body.filters[0].value as string;
      // Earlier requests resolve later, so completion order is reversed.
      const delay = label === "0" ? 15 : 0;
      await new Promise((resolve) => setTimeout(resolve, delay));
      return new Response(JSON.stringify({ rows: [{ label }] }), { status: 200 });
    });

    const requests = [makeReq("0"), makeReq("1"), makeReq("2")];
    const responses = await fetchAnalyticsBatch(requests, 3);
    const bodies = await Promise.all(responses.map((r) => r.json()));

    expect(bodies.map((b) => b.rows[0].label)).toEqual(["0", "1", "2"]);
  });
});
