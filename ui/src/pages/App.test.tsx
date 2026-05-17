import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import App from "@/App";

// ── Mock the entire App component's fetch calls ───────────────────

const mockProjects = [
  { project_id: "proj-1", name: "Test Project", org_id: "org-1" },
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

    // The login page should be rendered (Sign in to Lantern)
    await waitFor(() => {
      expect(screen.getByText("Sign in to Lantern")).toBeInTheDocument();
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
