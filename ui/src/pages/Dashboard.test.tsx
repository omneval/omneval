import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import DashboardPage from "./Dashboard";
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

  it("shows empty state for cost data when no data returned", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/analytics/spans") {
        return resolveAnalyticsResponse({ rows: [] });
      }
      return rejectAnalyticsResponse();
    });

    renderWithToast(<DashboardPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("No cost data yet")).toBeInTheDocument();
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

  it("renders all 6 widget cards", async () => {
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
    const expectedCards = [
      "Traces by Model",
      "Cost by Model",
      "Eval Scores",
      "Traces over Time",
      "Token Usage",
      "User Consumption",
    ];

    for (const cardTitle of expectedCards) {
      // Use regex to avoid duplicate text issues
      const elements = screen.queryAllByText(cardTitle);
      expect(elements.length).toBeGreaterThan(0);
    }
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
        // Token usage request: sums input_tokens and output_tokens
        if (body.aggregations?.some((a: { alias: string }) => a.alias === "input_tokens") &&
            body.aggregations?.some((a: { alias: string }) => a.alias === "output_tokens")) {
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
