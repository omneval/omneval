import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "@/App";

// ── Mock the entire App component's fetch calls ───────────────────

const mockProjects = [
  { project_id: "proj-1", name: "Test Project" },
];

// ── Session persistence tests ────────────────────────────────────

describe("session persistence on page load", () => {
  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/me") {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              user_id: "user-1",
              email: "alice@example.com",
              projects: mockProjects,
            }),
        } as Response;
      }
      return {
        ok: false,
        json: () => Promise.resolve({}),
      } as Response;
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("navigates to traces page when /api/v1/me returns a valid session", async () => {
    render(<App />);

    // After the /me call succeeds, the app should navigate to traces
    // The traces page renders with the active project
    await waitFor(
      () => {
        // The header should be visible (authenticated layout)
        expect(screen.getByText("Test Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
  });

  it("shows login screen when /api/v1/me returns 401", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/me") {
        return {
          ok: false,
          status: 401,
          json: () => Promise.resolve({ error: "unauthorized" }),
        } as Response;
      }
      return {
        ok: false,
        json: () => Promise.resolve({}),
      } as Response;
    });

    render(<App />);

    // The login page should be rendered (omneval login)
    await waitFor(() => {
      expect(screen.getByText("Sign in to")).toBeInTheDocument();
    });
  });

  it("calls /api/v1/me on mount instead of reading document.cookie", async () => {
    const fetchCalls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      fetchCalls.push(String(url));
      if (url === "/api/v1/me") {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              user_id: "user-1",
              email: "alice@example.com",
              projects: mockProjects,
            }),
        } as Response;
      }
      return {
        ok: false,
        json: () => Promise.resolve({}),
      } as Response;
    });

    render(<App />);

    await waitFor(() => {
      expect(fetchCalls).toContain("/api/v1/me");
    });

    // The app should NOT try to read document.cookie — there should be no
    // calls to non-existent endpoints like /api/v1/session or similar
    expect(fetchCalls[0]).toBe("/api/v1/me");
  });

  it("sets the active project from the /me response", async () => {
    render(<App />);

    await waitFor(
      () => {
        expect(screen.getByText("Test Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
  });
});

// ── URL-based routing tests ──────────────────────────────────────

describe("URL-based page routing on hard load", () => {
  const makeMeMock = () =>
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      const urlStr = String(url);
      if (urlStr === "/api/v1/me") {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              user_id: "user-1",
              email: "alice@example.com",
              projects: mockProjects,
            }),
        } as Response;
      }
      // Return empty arrays for list endpoints so pages render without errors
      if (urlStr.startsWith("/api/v1/prompts")) {
        return {
          ok: true,
          json: () => Promise.resolve([]),
        } as Response;
      }
      return {
        ok: false,
        json: () => Promise.resolve({}),
      } as Response;
    });

  afterEach(() => {
    vi.restoreAllMocks();
    // Reset URL back to root after each test
    window.history.replaceState({}, "", "/");
  });

  it("hard-loading /traces shows Traces page, not Dashboard", async () => {
    makeMeMock();
    window.history.replaceState({}, "", "/traces");

    render(<App />);

    await waitFor(
      () => {
        // Traces page renders a "Filters" panel
        expect(screen.getByText("Filters")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );

    // Dashboard heading must NOT be visible
    expect(screen.queryByRole("heading", { name: "Dashboard" })).toBeNull();
  });

  it("hard-loading /prompts shows Prompts page, not Dashboard", async () => {
    makeMeMock();
    window.history.replaceState({}, "", "/prompts");

    render(<App />);

    await waitFor(
      () => {
        // Prompts page renders a "Prompt Registry" heading
        expect(
          screen.getByRole("heading", { name: /prompt registry/i })
        ).toBeInTheDocument();
      },
      { timeout: 3000 }
    );

    expect(screen.queryByRole("heading", { name: "Dashboard" })).toBeNull();
  });

  it("hard-loading / (root) shows Dashboard", async () => {
    makeMeMock();
    window.history.replaceState({}, "", "/");

    render(<App />);

    await waitFor(
      () => {
        expect(
          screen.getByRole("heading", { name: "Dashboard" })
        ).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
  });
});

// ── URL sync on sidebar navigation ──────────────────────────────

describe("URL sync on sidebar navigation", () => {
  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      const urlStr = String(url);
      if (urlStr === "/api/v1/me") {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              user_id: "user-1",
              email: "alice@example.com",
              projects: mockProjects,
            }),
        } as Response;
      }
      return {
        ok: false,
        json: () => Promise.resolve({}),
      } as Response;
    });
    window.history.replaceState({}, "", "/");
  });

  afterEach(() => {
    vi.restoreAllMocks();
    window.history.replaceState({}, "", "/");
  });

  it("clicking Traces nav item updates window.location.pathname to /traces", async () => {
    const user = userEvent.setup();
    render(<App />);

    // Wait for authenticated layout
    await waitFor(
      () => {
        expect(screen.getByText("Test Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );

    // Click the Traces sidebar button
    const tracesLink = screen.getByRole("button", { name: /^traces$/i });
    await user.click(tracesLink);

    expect(window.location.pathname).toBe("/traces");
  });
});

// ── Browser history navigation (popstate) ───────────────────────

describe("browser back/forward navigation", () => {
  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      const urlStr = String(url);
      if (urlStr === "/api/v1/me") {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              user_id: "user-1",
              email: "alice@example.com",
              projects: mockProjects,
            }),
        } as Response;
      }
      if (urlStr.startsWith("/api/v1/prompts")) {
        return {
          ok: true,
          json: () => Promise.resolve([]),
        } as Response;
      }
      return {
        ok: false,
        json: () => Promise.resolve({}),
      } as Response;
    });
    window.history.replaceState({}, "", "/");
  });

  afterEach(() => {
    vi.restoreAllMocks();
    window.history.replaceState({}, "", "/");
  });

  it("pressing browser back after navigating to Traces returns to Dashboard", async () => {
    const user = userEvent.setup();
    render(<App />);

    await waitFor(
      () => {
        expect(screen.getByText("Test Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );

    // Navigate Dashboard → Traces via the sidebar
    await user.click(screen.getByRole("button", { name: /^traces$/i }));
    expect(window.location.pathname).toBe("/traces");
    await waitFor(() => {
      expect(screen.getByText("Filters")).toBeInTheDocument();
    });

    // Simulate the browser back button: URL returns to "/" and a popstate
    // event fires. The app must react and render the Dashboard again.
    window.history.replaceState({}, "", "/");
    window.dispatchEvent(new PopStateEvent("popstate"));

    await waitFor(() => {
      expect(
        screen.getByRole("heading", { name: "Dashboard" })
      ).toBeInTheDocument();
    });
    expect(screen.queryByText("Filters")).toBeNull();
  });

  it("popstate to /prompts renders the Prompts page", async () => {
    render(<App />);

    await waitFor(
      () => {
        expect(screen.getByText("Test Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );

    window.history.replaceState({}, "", "/prompts");
    window.dispatchEvent(new PopStateEvent("popstate"));

    await waitFor(() => {
      expect(
        screen.getByRole("heading", { name: /prompt registry/i })
      ).toBeInTheDocument();
    });
  });
});

// ── Project setting persistence (issue #56) ─────────────────────

describe("project setting persistence across page refresh", () => {
  const multiProjects = [
    { project_id: "proj-default", name: "Default Project" },
    { project_id: "proj-omneval", name: "omneval" },
  ];

  beforeEach(() => {
    // Clear localStorage before each test
    localStorage.clear();
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/me") {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              user_id: "user-1",
              email: "alice@example.com",
              projects: multiProjects,
            }),
        } as Response;
      }
      return {
        ok: false,
        json: () => Promise.resolve({}),
      } as Response;
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it("persists selected project to localStorage on change", async () => {
    render(<App />);

    // Wait for the app to load with default (first) project
    await waitFor(
      () => {
        expect(screen.getByText("Default Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );

    // The active project should be persisted in localStorage
    const stored = localStorage.getItem("omneval_active_project");
    expect(stored).toBe("proj-default");
  });

  it("restores persisted project on page reload instead of defaulting to first", async () => {
    // Simulate a previous session where user selected "omneval"
    localStorage.setItem("omneval_active_project", "proj-omneval");

    render(<App />);

    // After mount, the header dropdown should show "omneval" (the project name)
    // not "Default Project". We check the Project dropdown button text.
    await waitFor(
      () => {
        const projectDropdownBtn = screen.getByRole("button", { name: /project.*omneval/i });
        expect(projectDropdownBtn).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
  });

  it("falls back to first project when stored project no longer exists", async () => {
    // Simulate a stale project ID that is no longer available
    localStorage.setItem("omneval_active_project", "proj-deleted");

    render(<App />);

    // Should fall back to the first available project
    await waitFor(
      () => {
        expect(screen.getByText("Default Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
  });

  it("clears persisted project on logout", async () => {
    render(<App />);

    await waitFor(
      () => {
        expect(screen.getByText("Default Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );

    const user = userEvent.setup();

    // Open the sidebar if needed and click logout
    const logoutBtn = screen.getByRole("button", { name: /logout/i });
    await user.click(logoutBtn);

    await waitFor(() => {
      expect(screen.getByText("Sign in to")).toBeInTheDocument();
    });

    // localStorage should be cleared
    const stored = localStorage.getItem("omneval_active_project");
    expect(stored).toBeNull();
  });
});

// ── Direct-link trace detail URL routing ─────────────────────────

describe("direct-link /traces/{traceId} routing", () => {
  const mockProject = [
    { project_id: "proj-1", name: "Test Project" },
  ];

  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      const urlStr = String(url);
      if (urlStr === "/api/v1/me") {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              user_id: "user-1",
              email: "alice@example.com",
              projects: mockProject,
            }),
        } as Response;
      }
      // Mock /api/v1/traces/{traceId} for direct-link trace detail rendering
      if (urlStr.startsWith("/api/v1/traces/") && !urlStr.includes("/spans")) {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              trace_id: "test-trace",
              project_id: "proj-1",
              root_span: {
                span_id: "root-1",
                trace_id: "test-trace",
                parent_id: null,
                name: "root-span",
                kind: "llm",
                start_time: new Date().toISOString(),
                end_time: new Date().toISOString(),
                attributes: {},
                events: [],
                status: { code: 0 },
                resource: {},
                severity_text: "",
                severity_number: 0,
                duration_ms: 100,
              },
              total_input_tokens: 0,
              total_output_tokens: 0,
              total_cost_usd: 0,
            }),
        } as Response;
      }
      // Mock spans list for waterfall/tree rendering
      if (urlStr.startsWith("/api/v1/spans")) {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              spans: [
                {
                  span_id: "root-1",
                  trace_id: "test-trace",
                  parent_id: null,
                  name: "root-span",
                  kind: "llm",
                  start_time: new Date().toISOString(),
                  end_time: new Date().toISOString(),
                  cost_usd: 0.11,
                  attributes: {},
                  events: [],
                  status: { code: 0 },
                  resource: {},
                  severity_text: "",
                  severity_number: 0,
                  duration_ms: 100,
                },
              ],
              trace: null,
            }),
        } as Response;
      }
      return {
        ok: false,
        json: () => Promise.resolve({}),
      } as Response;
    });
    window.history.replaceState({}, "", "/");
  });

  afterEach(() => {
    vi.restoreAllMocks();
    window.history.replaceState({}, "", "/");
  });

  it("hard-loading /traces/{traceId} renders Traces list with trace detail overlay", async () => {
    window.history.replaceState({}, "", "/traces/abc123xyz");

    render(<App />);

    // Wait for the /api/v1/me call
    await waitFor(
      () => {
        expect(screen.getByText("Test Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );

    // Direct navigation to /traces/{traceId} renders the Traces list
    // AND opens the SlideInTraceDetail overlay.
    expect(screen.queryAllByRole("button", { name: "Close trace detail" }).length).toBeGreaterThan(0);
});
});
