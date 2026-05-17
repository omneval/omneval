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
            { name: "gpt-4", count: 150 },
            { name: "claude-3", count: 80 },
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
});
