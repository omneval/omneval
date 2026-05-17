import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import PromptsPage from "./Prompts";
import { ToastProvider } from "@/components/Toast";

function renderWithToast(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

// Mock response helpers
function mockListPrompts(prompts: { name: string; latest_version: number; labels: Record<string, number> }[]): Response {
  return {
    ok: true,
    json: () => Promise.resolve(prompts),
  } as Response;
}

function mockVersions(name: string, versions: {
  version_id: string;
  project_id: string;
  name: string;
  version: number;
  template: string;
  model: string;
  temperature: number;
  max_tokens: number;
  created_at: string;
}[]): Response {
  return {
    ok: true,
    json: () => Promise.resolve({ name, versions, count: versions.length }),
  } as Response;
}

describe("PromptsPage", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });
  const mockPromptList = [
    {
      name: "greeting",
      latest_version: 2,
      labels: { production: 2, staging: 1, dev: 1 },
    },
    {
      name: "summarize",
      latest_version: 1,
      labels: { dev: 1 },
    },
  ];

  const mockVersionsData = [
    {
      version_id: "v1-id",
      project_id: "proj-1",
      name: "greeting",
      version: 1,
      template: "Hello {{name}}!",
      model: "gpt-3.5",
      temperature: 0.5,
      max_tokens: 128,
      created_at: "2026-05-10T10:00:00Z",
    },
    {
      version_id: "v2-id",
      project_id: "proj-1",
      name: "greeting",
      version: 2,
      template: "Hi {{name}}, welcome!",
      model: "gpt-4",
      temperature: 0.7,
      max_tokens: 256,
      created_at: "2026-05-15T14:30:00Z",
    },
  ];

  it("renders loading state on mount", () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise<Response>(() => {}));
    renderWithToast(<PromptsPage activeProject="proj-1" />);
    expect(screen.getByText(/Loading prompts.../)).toBeInTheDocument();
  });

  it("renders the header with project id", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts(mockPromptList));
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Prompt Registry")).toBeInTheDocument();
    });
    expect(screen.getByText(/proj-1/)).toBeInTheDocument();
  });

  it("renders a list of prompts", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts(mockPromptList));
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("greeting")).toBeInTheDocument();
    });
    expect(screen.getByText("summarize")).toBeInTheDocument();
  });

  it("shows empty state when no prompts exist", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(mockListPrompts([]));
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("No prompts yet")).toBeInTheDocument();
    });
  });

  it("clicking a prompt row expands to show version history", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts(mockPromptList));
        }
        if (url.includes("/api/v1/prompts/greeting/versions")) {
          return Promise.resolve(mockVersions("greeting", mockVersionsData));
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("greeting")).toBeInTheDocument();
    });

    // Click the expand arrow on the greeting prompt
    const expandButtons = screen.getAllByRole("button");
    // Find the button with the chevron SVG (expand arrow)
    const chevronButtons = expandButtons.filter(btn => btn.querySelector("svg") !== null);

    // Click the first chevron button (for "greeting")
    fireEvent.click(chevronButtons[0]);

    // Version history should now be visible
    await waitFor(() => {
      expect(screen.getByText("Version 1")).toBeInTheDocument();
      expect(screen.getByText("Version 2")).toBeInTheDocument();
    });

    // Template content should be visible
    expect(screen.getByText(/Hello \{\{name\}\}!/)).toBeInTheDocument();
    expect(screen.getByText(/Hi \{\{name\}\}, welcome!/)).toBeInTheDocument();

    // Model config should be visible
    expect(screen.getByText(/gpt-3\.5/)).toBeInTheDocument();
    expect(screen.getByText(/gpt-4/)).toBeInTheDocument();
  });

  it("collapsing a previously-expanded prompt hides the detail content", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts(mockPromptList));
        }
        if (url.includes("/api/v1/prompts/greeting/versions")) {
          return Promise.resolve(mockVersions("greeting", mockVersionsData));
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("greeting")).toBeInTheDocument();
    });

    // Expand
    const expandButtons = screen.getAllByRole("button");
    const chevronButtons = expandButtons.filter(btn => btn.querySelector("svg") !== null);
    fireEvent.click(chevronButtons[0]);

    await waitFor(() => {
      expect(screen.getByText("Version 1")).toBeInTheDocument();
    });

    // Collapse
    fireEvent.click(chevronButtons[0]);

    // Content should be hidden after collapse
    expect(screen.queryByText(/Version 1/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Hello \{\{name\}\}!/)).not.toBeInTheDocument();
  });

  it("clicking a prompt row fetches version data on first expand", async () => {
    const fetchCalls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        fetchCalls.push(url);
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts(mockPromptList));
        }
        if (url.includes("/api/v1/prompts/greeting/versions")) {
          return Promise.resolve(mockVersions("greeting", mockVersionsData));
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("greeting")).toBeInTheDocument();
    });

    // At this point, only the list endpoint should have been called
    expect(fetchCalls.filter((c) => c.includes("/api/v1/prompts/greeting/versions"))).toHaveLength(0);

    // Expand
    const expandButtons = screen.getAllByRole("button");
    const chevronButtons = expandButtons.filter(btn => btn.querySelector("svg") !== null);
    fireEvent.click(chevronButtons[0]);

    // Now the versions endpoint should have been called
    await waitFor(() => {
      expect(fetchCalls.filter((c) => c.includes("/api/v1/prompts/greeting/versions"))).toHaveLength(1);
    });
  });

  it("version data is cached and not re-fetched on subsequent expands", async () => {
    const fetchCalls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        fetchCalls.push(url);
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts(mockPromptList));
        }
        if (url.includes("/api/v1/prompts/greeting/versions")) {
          return Promise.resolve(mockVersions("greeting", mockVersionsData));
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("greeting")).toBeInTheDocument();
    });

    const expandButtons = screen.getAllByRole("button");
    const chevronButtons = expandButtons.filter(btn => btn.querySelector("svg") !== null);

    // Expand → collapse → expand again
    fireEvent.click(chevronButtons[0]); // expand
    await waitFor(() => {
      expect(screen.getByText("Version 1")).toBeInTheDocument();
    });

    fireEvent.click(chevronButtons[0]); // collapse

    fireEvent.click(chevronButtons[0]); // expand again

    // The versions endpoint should only have been called once (cached)
    expect(fetchCalls.filter((c) => c.includes("/api/v1/prompts/greeting/versions"))).toHaveLength(1);
  });

  it("shows label badges with versions", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts(mockPromptList));
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("greeting")).toBeInTheDocument();
    });

    // Check label badges are visible
    // All labels appear for each prompt; missing labels show faint (opacity 0.4)
    expect(screen.getAllByText(/production/)).toHaveLength(2);
    expect(screen.getAllByText(/staging/)).toHaveLength(2);
    expect(screen.getAllByText(/dev/)).toHaveLength(2);
  });

  it("shows + New Prompt button", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(mockListPrompts([]));
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      // + New Prompt appears in the header and in empty state action
      expect(screen.getAllByText("+ New Prompt")).toHaveLength(2);
    });
  });

  it("shows new prompt form when + New Prompt is clicked", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(mockListPrompts([]));
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getAllByText("+ New Prompt")).toHaveLength(2);
    });

    // Click the first one (header button)
    const buttons = screen.getAllByText("+ New Prompt");
    fireEvent.click(buttons[0]);
    expect(screen.getByText("New Prompt")).toBeInTheDocument();
    expect(screen.getByPlaceholderText("e.g. greeting")).toBeInTheDocument();
    // Template textarea placeholder text
    expect(document.body.innerHTML).toContain("variable");
  });

  it("renders version history with timestamps for each version", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts(mockPromptList));
        }
        if (url.includes("/api/v1/prompts/greeting/versions")) {
          return Promise.resolve(mockVersions("greeting", mockVersionsData));
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("greeting")).toBeInTheDocument();
    });

    const expandButtons = screen.getAllByRole("button");
    const chevronButtons = expandButtons.filter(btn => btn.querySelector("svg") !== null);
    fireEvent.click(chevronButtons[0]);

    await waitFor(() => {
      expect(screen.getByText("Version 1")).toBeInTheDocument();
      expect(screen.getByText("Version 2")).toBeInTheDocument();
    });

    // Timestamps should be formatted
    expect(screen.getByText(/May 10/)).toBeInTheDocument();
    expect(screen.getByText(/May 15/)).toBeInTheDocument();
  });

  it("shows Compare versions button when multiple versions exist", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts(mockPromptList));
        }
        if (url.includes("/api/v1/prompts/greeting/versions")) {
          return Promise.resolve(mockVersions("greeting", mockVersionsData));
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("greeting")).toBeInTheDocument();
    });

    const expandButtons = screen.getAllByRole("button");
    const chevronButtons = expandButtons.filter(btn => btn.querySelector("svg") !== null);
    fireEvent.click(chevronButtons[0]);

    await waitFor(() => {
      // Compare versions button appears for prompts with >= 2 versions
      expect(screen.getAllByText("Compare versions")).toHaveLength(1);
    });
  });

  it("shows Compare versions button is hidden when only one version exists", async () => {
    const singleVersion: typeof mockVersionsData = [
      {
        version_id: "v1-id",
        project_id: "proj-1",
        name: "summarize",
        version: 1,
        template: "Summarize: {{text}}",
        model: "gpt-4",
        temperature: 0.0,
        max_tokens: 512,
        created_at: "2026-05-10T10:00:00Z",
      },
    ];

    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts([{
            name: "summarize",
            latest_version: 1,
            labels: { dev: 1 },
          }]));
        }
        if (url.includes("/api/v1/prompts/summarize/versions")) {
          return Promise.resolve(mockVersions("summarize", singleVersion));
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("summarize")).toBeInTheDocument();
    });

    const expandButtons = screen.getAllByRole("button");
    const chevronButtons = expandButtons.filter(btn => btn.querySelector("svg") !== null);
    fireEvent.click(chevronButtons[0]);

    await waitFor(() => {
      expect(screen.getByText("Version 1")).toBeInTheDocument();
    });

    // Compare button should not appear with only one version
    expect(screen.queryByText("Compare versions")).not.toBeInTheDocument();
  });
});
