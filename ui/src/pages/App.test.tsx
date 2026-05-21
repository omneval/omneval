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
