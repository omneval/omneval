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

// ── Issue #56: project setting persistence ──────────────────────

describe("issue #56: project setting persists across page refresh", () => {
  beforeEach(() => {
    // Clear localStorage before each test
    localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    window.history.replaceState({}, "", "/");
  });

  it("remembers the selected project after a page refresh", async () => {
    const mockMultiProjects = [
      { project_id: "proj-default", name: "Default Project" },
      { project_id: "proj-omneval", name: "Omneval Project" },
    ];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      const urlStr = String(url);
      if (urlStr === "/api/v1/me") {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              user_id: "user-1",
              email: "alice@example.com",
              projects: mockMultiProjects,
            }),
        } as Response;
      }
      return {
        ok: false,
        json: () => Promise.resolve({}),
      } as Response;
    });

    window.history.replaceState({}, "", "/");

    // First render: initial load — defaults to first project
    const { unmount } = render(<App />);

    await waitFor(
      () => {
        expect(screen.getByText("Default Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );

    // Simulate user selecting the second project via the dropdown
    // The Header renders a dropdown button containing "Project:" label with the selected project name
    const projectDropdownBtn = screen.getByRole("button", { name: /Project.*Default Project/i });
    await userEvent.click(projectDropdownBtn);

    // Click the "Omneval Project" option in the dropdown
    const omnevalOption = screen.getByRole("button", { name: "Omneval Project" });
    await userEvent.click(omnevalOption);

    await waitFor(() => {
      expect(screen.getByText("Omneval Project")).toBeInTheDocument();
    });

    // Verify localStorage was updated with the selected project
    const storedProject = localStorage.getItem("omneval_active_project");
    expect(storedProject).toBe("proj-omneval");

    // Simulate page refresh: unmount and re-render (fresh state but same localStorage)
    unmount();
    render(<App />);

    // After "refresh", the app should restore the saved project
    await waitFor(
      () => {
        expect(screen.getByText("Omneval Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
  });

  it("falls back to first project when stored project is not in the user's projects", async () => {
    // Store a project ID that won't be in the response
    localStorage.setItem("omneval_active_project", "proj-gone");

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      const urlStr = String(url);
      if (urlStr === "/api/v1/me") {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              user_id: "user-1",
              email: "alice@example.com",
              projects: [
                { project_id: "proj-current", name: "Current Project" },
              ],
            }),
        } as Response;
      }
      return {
        ok: false,
        json: () => Promise.resolve({}),
      } as Response;
    });

    window.history.replaceState({}, "", "/");

    render(<App />);

    await waitFor(
      () => {
        // Should fall back to the first available project
        expect(screen.getByText("Current Project")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
  });

  it("clears stored project on logout", async () => {
    const mockMultiProjects = [
      { project_id: "proj-1", name: "Project One" },
    ];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      const urlStr = String(url);
      if (urlStr === "/api/v1/me") {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              user_id: "user-1",
              email: "alice@example.com",
              projects: mockMultiProjects,
            }),
        } as Response;
      }
      return {
        ok: true,
        json: () => Promise.resolve({}),
      } as Response;
    });

    window.history.replaceState({}, "", "/");

    render(<App />);

    await waitFor(
      () => {
        expect(screen.getByText("Project One")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );

    // Pre-populate localStorage to simulate a saved selection
    localStorage.setItem("omneval_active_project", "proj-1");

    // Click logout
    const logoutButton = screen.getByRole("button", { name: /logout/i });
    await userEvent.click(logoutButton);

    // Verify localStorage was cleared on logout
    const storedProject = localStorage.getItem("omneval_active_project");
    expect(storedProject).toBeNull();

    // Verify login screen is shown
    await waitFor(() => {
      expect(screen.getByText("Sign in to")).toBeInTheDocument();
    });
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
