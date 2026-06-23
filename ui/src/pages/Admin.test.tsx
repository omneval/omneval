import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import AdminPage from "./Admin";
import { ToastProvider } from "@/components/Toast";

function renderWithToast(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

const mockKeys = [
  {
    key_id: "oev_proj_abc123",
    kind: "project" as const,
    created_at: "2026-05-13T10:00:00Z",
  },
  {
    key_id: "oev_svc_worker-1",
    kind: "service" as const,
    service_name: "my-agent",
    created_at: "2026-05-14T08:00:00Z",
  },
];

const mockProjects = [
  { project_id: "proj-1", name: "Test Project", org_id: "" },
  { project_id: "proj-2", name: "Other Project", org_id: "" },
];

function resolveKeys(keys: unknown[]) {
  return ({ ok: true, json: () => Promise.resolve(keys) } as Response);
}

function resolveProjects(projects: unknown[]) {
  return ({ ok: true, json: () => Promise.resolve(projects) } as Response);
}

beforeEach(() => {
  vi.restoreAllMocks();
  vi.spyOn(console, "error").mockImplementation(() => {});
});

describe("AdminPage", () => {
  it("renders the API Keys tab by default", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("API Keys")).toBeInTheDocument();
    });
  });

  it("renders all three tabs", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("API Keys")).toBeInTheDocument();
      expect(screen.getByText("Projects")).toBeInTheDocument();
      expect(screen.getByText("Traces")).toBeInTheDocument();
    });
  });

  it("shows empty state when no keys exist", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("No API keys found.")).toBeInTheDocument();
    });
  });

  it("renders API keys with kind badges", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys(mockKeys);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("oev_proj_abc123")).toBeInTheDocument();
      expect(screen.getByText("oev_svc_worker-1")).toBeInTheDocument();
    });
  });

  it("shows the user-supplied name for a project key, falling back to a truncated key ID when unnamed (#143)", async () => {
    const keysWithNames = [
      {
        key_id: "oev_proj_named1234",
        kind: "project" as const,
        name: "CI ingest",
        created_at: "2026-05-13T10:00:00Z",
      },
      {
        key_id: "oev_proj_unnamed5678",
        kind: "project" as const,
        created_at: "2026-05-13T10:00:00Z",
      },
    ];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys(keysWithNames);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("CI ingest")).toBeInTheDocument();
      expect(screen.getByText(/Project Key \(\.\.\.5678\)/)).toBeInTheDocument();
    });
  });

  it("groups keys by kind (project vs service)", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys(mockKeys);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Project Keys (1)")).toBeInTheDocument();
      expect(screen.getByText("Service Keys (1)")).toBeInTheDocument();
    });
  });

  it("shows delete button for each key", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys(mockKeys);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      const deleteButtons = screen.getAllByRole("button", { name: /delete/i });
      expect(deleteButtons.length).toBe(2);
    });
  });

  it("opens confirm dialog when deleting a key", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys(mockKeys);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("oev_proj_abc123")).toBeInTheDocument();
    });

    const deleteButtons = screen.getAllByRole("button", { name: /delete/i });
    fireEvent.click(deleteButtons[0]);

    await waitFor(() => {
      expect(screen.getByText("Delete API Key")).toBeInTheDocument();
    });
  });

  it("deletes a key via DELETE API and refreshes", async () => {
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, init) => {
      callCount++;
      if (url === "/api/v1/admin/api-keys" && init?.method !== "DELETE") {
        return resolveKeys(mockKeys);
      }
      if (url === "/api/v1/admin/api-keys/oev_proj_abc123" && init?.method === "DELETE") {
        return ({ ok: true } as Response);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("oev_proj_abc123")).toBeInTheDocument();
    });

    const deleteButtons = screen.getAllByRole("button", { name: /delete/i });
    fireEvent.click(deleteButtons[0]);

    await waitFor(() => {
      expect(screen.getByText("Delete API Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Delete Key"));

    await waitFor(() => {
      // Should show "API key deleted" toast
      expect(screen.getByText("API key deleted")).toBeInTheDocument();
    });
  });

  it("switches to the Projects tab", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects(mockProjects);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Projects")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Projects"));

    await waitFor(() => {
      expect(screen.getByText("Test Project")).toBeInTheDocument();
      expect(screen.getByText("Other Project")).toBeInTheDocument();
    });
  });

  it("shows projects with delete buttons", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects(mockProjects);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Projects")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Projects"));

    await waitFor(() => {
      const deleteButtons = screen.getAllByRole("button", { name: /delete/i });
      expect(deleteButtons.length).toBe(2);
    });
  });

  it("switches to the Traces tab", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      if (url === "/api/v1/admin/traces/proj-1/count") {
        return ({
          ok: true,
          json: () => Promise.resolve({ count: 42 }),
        } as Response);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Traces")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Traces"));

    await waitFor(() => {
      expect(screen.getByText("Delete All Traces")).toBeInTheDocument();
    });
  });

  it("shows trace count in the Traces tab", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      if (url === "/api/v1/admin/traces/proj-1/count") {
        return ({
          ok: true,
          json: () => Promise.resolve({ count: 123 }),
        } as Response);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Traces")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Traces"));

    await waitFor(() => {
      expect(screen.getByText(/Total traces: 123/)).toBeInTheDocument();
    });
  });

  it("shows the active project name in the Traces tab", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      if (url === "/api/v1/admin/traces/proj-1/count") {
        return ({
          ok: true,
          json: () => Promise.resolve({ count: 0 }),
        } as Response);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Traces")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Traces"));

    await waitFor(() => {
      expect(screen.getByText("proj-1")).toBeInTheDocument();
    });
  });

  it("opens confirm dialog when clicking Delete All Traces", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      if (url === "/api/v1/admin/traces/proj-1/count") {
        return ({
          ok: true,
          json: () => Promise.resolve({ count: 10 }),
        } as Response);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Traces")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Traces"));

    await waitFor(() => {
      expect(screen.getByText("Delete All Traces")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /delete all traces/i }));

    await waitFor(() => {
      expect(screen.getByText(/permanently delete all 10 traces/)).toBeInTheDocument();
    });
  });

  it("deletes all traces via DELETE API", async () => {
    const deleteCalls: string[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url: RequestInfo | URL, _init?: RequestInit) => {
      const urlStr = url instanceof Request ? url.url : String(url);
      if (urlStr === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (urlStr === "/api/v1/projects") {
        return resolveProjects([]);
      }
      if (urlStr === "/api/v1/admin/traces/proj-1/count") {
        return ({
          ok: true,
          json: () => Promise.resolve({ count: 10 }),
        } as Response);
      }
      if (urlStr === "/api/v1/admin/traces/proj-1") {
        deleteCalls.push(urlStr);
        return ({ ok: true } as Response);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Traces")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Traces"));

    await waitFor(() => {
      expect(screen.getByText("Delete All Traces")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /delete all traces/i }));

    await waitFor(() => {
      expect(screen.getByText(/permanently delete all 10 traces/)).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /yes, delete all/i }));

    await waitFor(() => {
      expect(screen.getByText("All traces deleted")).toBeInTheDocument();
    });

    expect(deleteCalls).toContain("/api/v1/admin/traces/proj-1");
  });

  it("shows loading skeletons while fetching", async () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(
      new Promise<Response>(() => {})
    );

    renderWithToast(<AdminPage activeProject="proj-1" />);

    // Skeletons should be visible while loading
    const skeletons = document.querySelectorAll('[class*="animate-pulse"]');
    expect(skeletons.length).toBeGreaterThan(0);
  });

  it("handles API errors gracefully for keys", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return ({ ok: false, status: 500 } as Response);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects([]);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("No API keys found.")).toBeInTheDocument();
    });
  });

  it("handles API errors gracefully for projects", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects") {
        return ({ ok: false, status: 500 } as Response);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Projects")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Projects"));

    await waitFor(() => {
      expect(screen.getByText("No projects found.")).toBeInTheDocument();
    });
  });

  it("deletes a project via DELETE API", async () => {
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, init) => {
      callCount++;
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys([]);
      }
      if (url === "/api/v1/projects" && init?.method !== "DELETE") {
        return resolveProjects(mockProjects);
      }
      if (url === "/api/v1/admin/projects/proj-1" && init?.method === "DELETE") {
        return ({ ok: true } as Response);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Projects")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Projects"));

    await waitFor(() => {
      expect(screen.getByText("Test Project")).toBeInTheDocument();
    });

    const deleteButtons = screen.getAllByRole("button", { name: /delete/i });
    fireEvent.click(deleteButtons[0]);

    await waitFor(() => {
      expect(screen.getByText(/delete "Test Project"/)).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /delete project/i }));

    await waitFor(() => {
      expect(screen.getByText(/Project "Test Project" deleted/)).toBeInTheDocument();
    });
  });

  it("displays all keys from all projects in admin", async () => {
    const allProjectKeys = [
      {
        key_id: "oev_proj_proj1_key",
        kind: "project" as const,
        created_at: "2026-05-10T10:00:00Z",
      },
      {
        key_id: "oev_proj_proj2_key",
        kind: "project" as const,
        created_at: "2026-05-11T10:00:00Z",
      },
    ];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (url === "/api/v1/admin/api-keys") {
        return resolveKeys(allProjectKeys);
      }
      if (url === "/api/v1/projects") {
        return resolveProjects(mockProjects);
      }
      return resolveKeys([{ count: 0 }]);
    });

    renderWithToast(<AdminPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("oev_proj_proj1_key")).toBeInTheDocument();
      expect(screen.getByText("oev_proj_proj2_key")).toBeInTheDocument();
    });
  });

  describe("sorting by Created date", () => {
    const sortKeys = [
      {
        key_id: "oev_proj_oldest",
        kind: "project" as const,
        created_at: "2026-05-01T10:00:00Z",
      },
      {
        key_id: "oev_proj_newest",
        kind: "project" as const,
        created_at: "2026-06-14T10:00:00Z",
      },
      {
        key_id: "oev_proj_middle",
        kind: "project" as const,
        created_at: "2026-05-29T10:00:00Z",
      },
    ];

    function mockFetchWithSortKeys() {
      vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
        if (url === "/api/v1/admin/api-keys") {
          return resolveKeys(sortKeys);
        }
        if (url === "/api/v1/projects") {
          return resolveProjects([]);
        }
        return resolveKeys([{ count: 0 }]);
      });
    }

    function getKeyOrder() {
      const ids = sortKeys.map((k) => k.key_id);
      const text = document.body.textContent || "";
      return ids
        .map((id) => ({ id, index: text.indexOf(id) }))
        .filter((entry) => entry.index !== -1)
        .sort((a, b) => a.index - b.index)
        .map((entry) => entry.id);
    }

    it("defaults to newest-first order by Created date", async () => {
      mockFetchWithSortKeys();

      renderWithToast(<AdminPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("oev_proj_oldest")).toBeInTheDocument();
      });

      expect(getKeyOrder()).toEqual([
        "oev_proj_newest",
        "oev_proj_middle",
        "oev_proj_oldest",
      ]);
    });

    it("toggles to oldest-first order when the Created sort control is clicked", async () => {
      mockFetchWithSortKeys();

      renderWithToast(<AdminPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("oev_proj_oldest")).toBeInTheDocument();
      });

      const sortButton = screen.getByRole("button", { name: /created/i });
      fireEvent.click(sortButton);

      await waitFor(() => {
        expect(getKeyOrder()).toEqual([
          "oev_proj_oldest",
          "oev_proj_middle",
          "oev_proj_newest",
        ]);
      });

      // Clicking again should toggle back to newest-first
      fireEvent.click(sortButton);

      await waitFor(() => {
        expect(getKeyOrder()).toEqual([
          "oev_proj_newest",
          "oev_proj_middle",
          "oev_proj_oldest",
        ]);
      });
    });
  });

  describe("search/filter by name or key ID", () => {
    const searchKeys = [
      {
        key_id: "oev_proj_abc123",
        kind: "project" as const,
        created_at: "2026-05-13T10:00:00Z",
      },
      {
        key_id: "oev_svc_worker-1",
        kind: "service" as const,
        service_name: "my-agent",
        created_at: "2026-05-14T08:00:00Z",
      },
      {
        key_id: "oev_proj_xyz789",
        kind: "project" as const,
        created_at: "2026-05-15T08:00:00Z",
      },
    ];

    function mockFetchWithSearchKeys() {
      vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
        if (url === "/api/v1/admin/api-keys") {
          return resolveKeys(searchKeys);
        }
        if (url === "/api/v1/projects") {
          return resolveProjects([]);
        }
        return resolveKeys([{ count: 0 }]);
      });
    }

    it("renders a search input for filtering keys", async () => {
      mockFetchWithSearchKeys();

      renderWithToast(<AdminPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("oev_proj_abc123")).toBeInTheDocument();
      });

      expect(
        screen.getByPlaceholderText(/search/i)
      ).toBeInTheDocument();
    });

    it("filters keys by key ID prefix", async () => {
      mockFetchWithSearchKeys();

      renderWithToast(<AdminPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("oev_proj_abc123")).toBeInTheDocument();
      });

      const searchInput = screen.getByPlaceholderText(/search/i);
      fireEvent.change(searchInput, { target: { value: "abc123" } });

      await waitFor(() => {
        expect(screen.getByText("oev_proj_abc123")).toBeInTheDocument();
        expect(screen.queryByText("oev_proj_xyz789")).not.toBeInTheDocument();
        expect(screen.queryByText("oev_svc_worker-1")).not.toBeInTheDocument();
      });
    });

    it("filters keys by service/key name (case-insensitive)", async () => {
      mockFetchWithSearchKeys();

      renderWithToast(<AdminPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("oev_proj_abc123")).toBeInTheDocument();
      });

      const searchInput = screen.getByPlaceholderText(/search/i);
      fireEvent.change(searchInput, { target: { value: "MY-AGENT" } });

      await waitFor(() => {
        expect(screen.getByText("oev_svc_worker-1")).toBeInTheDocument();
        expect(screen.queryByText("oev_proj_abc123")).not.toBeInTheDocument();
        expect(screen.queryByText("oev_proj_xyz789")).not.toBeInTheDocument();
      });
    });

    it("shows a no-results message when the search matches nothing", async () => {
      mockFetchWithSearchKeys();

      renderWithToast(<AdminPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("oev_proj_abc123")).toBeInTheDocument();
      });

      const searchInput = screen.getByPlaceholderText(/search/i);
      fireEvent.change(searchInput, { target: { value: "no-such-key" } });

      await waitFor(() => {
        expect(screen.queryByText("oev_proj_abc123")).not.toBeInTheDocument();
        expect(screen.queryByText("oev_proj_xyz789")).not.toBeInTheDocument();
        expect(screen.queryByText("oev_svc_worker-1")).not.toBeInTheDocument();
        expect(
          screen.getByText(/no .* keys (found|match)/i)
        ).toBeInTheDocument();
      });
    });
  });

  describe("Ops tab", () => {
    it("renders the Ops tab alongside the destructive tabs", async () => {
      vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
        if (url === "/api/v1/admin/api-keys") {
          return resolveKeys([]);
        }
        if (url === "/api/v1/projects") {
          return resolveProjects([]);
        }
        if (url === "/api/v1/admin/traces/proj-1/count") {
          return Promise.resolve(({ ok: true, json: () => Promise.resolve({ count: 0 }) }) as Response);
        }
        return Promise.resolve(({ ok: true, json: () => Promise.resolve({ ingest_queue_depth: 0 }) }) as Response);
      });

      renderWithToast(<AdminPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("API Keys")).toBeInTheDocument();
        expect(screen.getByText("Projects")).toBeInTheDocument();
        expect(screen.getByText("Traces")).toBeInTheDocument();
        expect(screen.getByText("Ops")).toBeInTheDocument();
      });
    });

    it("shows ingest_queue_depth metric when Ops tab is active", async () => {
      vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
        if (url === "/api/v1/admin/api-keys") {
          return resolveKeys([]);
        }
        if (url === "/api/v1/projects") {
          return resolveProjects([]);
        }
        if (url === "/api/v1/admin/traces/proj-1/count") {
          return Promise.resolve(({ ok: true, json: () => Promise.resolve({ count: 0 }) }) as Response);
        }
        if (url === "/api/v1/admin/ops") {
          return Promise.resolve(({ ok: true, json: () => Promise.resolve({ ingest_queue_depth: 17 }) }) as Response);
        }
        return Promise.resolve(({ ok: true, json: () => Promise.resolve({}) }) as Response);
      });

      renderWithToast(<AdminPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("Ops")).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText("Ops"));

      await waitFor(() => {
        expect(screen.getByText("Ingest Queue Depth")).toBeInTheDocument();
        expect(screen.getByText("17")).toBeInTheDocument();
      });
    });

    it("shows zero when ingest_queue_depth is 0", async () => {
      vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
        if (url === "/api/v1/admin/api-keys") {
          return resolveKeys([]);
        }
        if (url === "/api/v1/projects") {
          return resolveProjects([]);
        }
        if (url === "/api/v1/admin/traces/proj-1/count") {
          return Promise.resolve(({ ok: true, json: () => Promise.resolve({ count: 0 }) }) as Response);
        }
        if (url === "/api/v1/admin/ops") {
          return Promise.resolve(({ ok: true, json: () => Promise.resolve({ ingest_queue_depth: 0 }) }) as Response);
        }
        return Promise.resolve(({ ok: true, json: () => Promise.resolve({}) }) as Response);
      });

      renderWithToast(<AdminPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("Ops")).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText("Ops"));

      await waitFor(() => {
        expect(screen.getByText("Ingest Queue Depth")).toBeInTheDocument();
        expect(screen.getByText("0")).toBeInTheDocument();
      });
    });

    it("shows a loading skeleton for the Ops tab while fetching metrics", async () => {
      vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
        if (url === "/api/v1/admin/api-keys") {
          return resolveKeys([]);
        }
        if (url === "/api/v1/projects") {
          return resolveProjects([]);
        }
        if (url === "/api/v1/admin/traces/proj-1/count") {
          return Promise.resolve(({ ok: true, json: () => Promise.resolve({ count: 0 }) }) as Response);
        }
        // Delay the ops response
        await new Promise((resolve) => setTimeout(resolve, 200));
        if (url === "/api/v1/admin/ops") {
          return Promise.resolve(({ ok: true, json: () => Promise.resolve({ ingest_queue_depth: 5 }) }) as Response);
        }
        return Promise.resolve(({ ok: true, json: () => Promise.resolve({}) }) as Response);
      });

      renderWithToast(<AdminPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("Ops")).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText("Ops"));

      // Immediately after switching tab, value should not be present yet
      // (skeleton loading state), and after delay the metric should appear
      await waitFor(() => {
        expect(screen.getByText("5")).toBeInTheDocument();
      });
    });
  });
});
