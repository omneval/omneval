import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import ConversationsPage from "./Conversations";

// ── Helper data ──────────────────────────────────────────────────

const mockConversations = [
  {
    conversation_id: "abc123def456abc123def456abc12345",
    service_name: "my-agent",
    trace_count: 5,
    span_count: 23,
    start_time: "2025-01-15T10:00:00Z",
    end_time: "2025-01-15T10:04:32Z",
    total_cost_usd: 0.042,
    total_input_tokens: 12400,
    total_output_tokens: 3200,
  },
  {
    conversation_id: "fed987654321fed987654321fed54321",
    service_name: "another-service",
    trace_count: 2,
    span_count: 8,
    start_time: "2025-01-15T09:00:00Z",
    end_time: "2025-01-15T09:02:00Z",
    total_cost_usd: 0.015,
    total_input_tokens: 500,
    total_output_tokens: 200,
  },
];

// ── Render helper ────────────────────────────────────────────────

function renderPage(overrides: Record<string, unknown> = {}) {
  return render(
    <ConversationsPage
      activeProject="test-project"
      onNavigateToConversation={() => {}}
      {...overrides}
    />
  );
}

// ── Mock helper ─────────────────────────────────────────────────

function mockFetchWithConversations(conversations = mockConversations, nextCursor = "") {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
    if (String(url).includes("/api/v1/conversations")) {
      return {
        ok: true,
        json: () => Promise.resolve({ conversations, next: nextCursor }),
      } as Response;
    }
    return { ok: false, status: 404, json: () => Promise.resolve({}) } as Response;
  });
}

// ── Tests ────────────────────────────────────────────────────────

describe("ConversationsPage", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  // ── Header ───────────────────────────────────────────────────

  it("renders the Conversations header and subtitle", async () => {
    mockFetchWithConversations([]);
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Conversations")).toBeInTheDocument();
    });
    expect(
      screen.getByText("Group agent sessions by conversation for structured analysis")
    ).toBeInTheDocument();
  });

  // ── Empty state ─────────────────────────────────────────────

  it("shows the SDK hint empty state when there are no conversations", async () => {
    mockFetchWithConversations([]);
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("No conversations yet")).toBeInTheDocument();
    });
    expect(screen.getByText(/conversation_id from the SDK/)).toBeInTheDocument();
    expect(screen.getByText(/set_active_conversation_id/)).toBeInTheDocument();
    expect(screen.getByText(/WithConversationID/)).toBeInTheDocument();
    expect(screen.getByText(/Omneval.setActiveConversationId/)).toBeInTheDocument();
  });

  // ── Loading state ───────────────────────────────────────────

  it("shows skeleton placeholders while loading with no data", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => new Promise(() => {}) // never resolves
    );
    renderPage();

    // Skeletons should appear before data arrives
    await waitFor(() => {
      const rows = document.querySelectorAll("div[style*='width']");
      expect(rows.length).toBeGreaterThan(0);
    });
  });

  // ── Data rendering ──────────────────────────────────────────

  it("renders conversation rows with truncated IDs and metadata", async () => {
    mockFetchWithConversations();
    renderPage();

    await waitFor(() => {
      expect(screen.getByText(/abc123def456…/)).toBeInTheDocument();
    });
    expect(screen.getByText("my-agent")).toBeInTheDocument();
    expect(screen.getByText("5")).toBeInTheDocument(); // trace_count
    expect(screen.getByText("23")).toBeInTheDocument(); // span_count
    expect(screen.getByText("$0.0420")).toBeInTheDocument();
    expect(screen.getByText("15,600")).toBeInTheDocument(); // total tokens
  });

  it("renders multiple conversation rows", async () => {
    mockFetchWithConversations();
    renderPage();

    await waitFor(() => {
      const rows = document.querySelectorAll("tr");
      expect(rows.length).toBeGreaterThan(2);
    });
  });

  it("shows '—' for a service with no name", async () => {
    mockFetchWithConversations([{ ...mockConversations[0], service_name: "" }]);
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("—")).toBeInTheDocument();
    });
  });

  // ── Navigation ──────────────────────────────────────────────

  it("calls onNavigateToConversation when a row is clicked", async () => {
    mockFetchWithConversations();
    const onNavigateToConversation = vi.fn();
    renderPage({ onNavigateToConversation });

    await waitFor(() => {
      expect(screen.getByText(/abc123def456…/)).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/abc123def456…/));
    });

    expect(onNavigateToConversation).toHaveBeenCalledWith(
      "abc123def456abc123def456abc12345"
    );
  });

  it("sets correct aria-label for accessibility", async () => {
    mockFetchWithConversations();
    renderPage();

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /Open conversation abc123/ })).toBeInTheDocument();
    });
  });

  // ── API integration ─────────────────────────────────────────

  it("fetches from the conversations endpoint with project_id", async () => {
    const urls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      urls.push(String(url));
      return {
        ok: true,
        json: () => Promise.resolve({ conversations: [], next: "" }),
      } as Response;
    });
    renderPage();

    await waitFor(() => {
      expect(urls.length).toBeGreaterThanOrEqual(1);
    });
    expect(
      urls.some((u) => u.includes("/api/v1/conversations") && u.includes("project_id=test-project"))
    ).toBe(true);
  });

  it("includes a next-cursor pagination button when data has a cursor", async () => {
    mockFetchWithConversations(mockConversations, "next-page-token");
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Load Next Page")).toBeInTheDocument();
    });
  });

  it("shows 'No more data' when there is no next cursor", async () => {
    mockFetchWithConversations(mockConversations, "");
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("No more data")).toBeInTheDocument();
    });
  });

  it("fetches next page of conversations when the pagination button is clicked", async () => {
    const urls: string[] = [];
    let callCount = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      const u = String(url);
      urls.push(u);
      callCount++;
      if (callCount === 1) {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              conversations: mockConversations,
              next: "next-token",
            }),
        } as Response;
      }
      // Second call (pagination)
      return {
        ok: true,
        json: () => Promise.resolve({ conversations: [], next: "" }),
      } as Response;
    });
    renderPage();

    await waitFor(() => {
      expect(screen.getByText(/abc123def456…/)).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByText("Load Next Page"));
    });

    // Should have made two calls: initial + pagination
    expect(urls.length).toBeGreaterThanOrEqual(2);
    expect(urls[1]).toContain("cursor=next-token");
  });
});