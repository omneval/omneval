import { describe, it, expect, vi, beforeEach } from "vitest";
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
  const mockPromptList: { name: string; latest_version: number; labels: Record<string, number> }[] = [
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

  it("renders model dropdown in the new prompt form", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts([]));
        }
        if (url.includes("/api/v1/models")) {
          return Promise.resolve({ ok: true, json: () => Promise.resolve(["gpt-4", "gpt-3.5-turbo", "claude-sonnet-4"]) } as Response);
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getAllByText("+ New Prompt")).toHaveLength(2);
    });

    // Click the first "+ New Prompt" button (header) to open the form
    const buttons = screen.getAllByText("+ New Prompt");
    fireEvent.click(buttons[0]);

    // Wait for the form to show (form has "Prompt Name" label)
    await screen.findByLabelText("Prompt Name");

    // Model field should be a dropdown select element (wait for fetch to complete)
    await waitFor(() => {
      const select = screen.getByLabelText("Model");
      expect(select.tagName).toBe("SELECT");
    });
  });

  it("shows known models in the dropdown", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts([]));
        }
        if (url.includes("/api/v1/models")) {
          return Promise.resolve({ ok: true, json: () => Promise.resolve(["gpt-4", "gpt-3.5-turbo", "claude-sonnet-4"]) } as Response);
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getAllByText("+ New Prompt")).toHaveLength(2);
    });

    const buttons = screen.getAllByText("+ New Prompt");
    fireEvent.click(buttons[0]);

    await screen.findByLabelText("Prompt Name");

    // Wait for dropdown to populate and check options
    await waitFor(() => {
      const select = screen.getByLabelText("Model") as HTMLSelectElement;
      const options = Array.from(select.options).map((o) => o.value);
      expect(options).toContain("gpt-4");
      expect(options).toContain("gpt-3.5-turbo");
      expect(options).toContain("claude-sonnet-4");
      expect(options).toContain("__other__");
    });
  });

  it("shows freeform input when 'Other...' is selected", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          return Promise.resolve(mockListPrompts([]));
        }
        if (url.includes("/api/v1/models")) {
          return Promise.resolve({ ok: true, json: () => Promise.resolve(["gpt-4"]) } as Response);
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getAllByText("+ New Prompt")).toHaveLength(2);
    });

    const buttons = screen.getAllByText("+ New Prompt");
    fireEvent.click(buttons[0]);

    await screen.findByLabelText("Prompt Name");

    const select = await screen.findByLabelText("Model");
    // Select "Other..." option
    fireEvent.change(select, { target: { value: "__other__" } });

    // Custom input should appear
    const customInput = await screen.findByPlaceholderText("Enter custom model name");
    expect(customInput).toBeInTheDocument();
    expect((customInput as HTMLInputElement).value).toBe("");
  });

  it("submits custom model from 'Other...' option", async () => {
    let capturedBody: Record<string, unknown> | null = null;
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
          if (init?.method === "POST") {
            const body = await (init.body as string);
            capturedBody = JSON.parse(body);
          }
          return Promise.resolve(mockListPrompts([]));
        }
        if (url.includes("/api/v1/models")) {
          return Promise.resolve({ ok: true, json: () => Promise.resolve(["gpt-4"]) } as Response);
        }
        return Promise.resolve({ ok: false } as Response);
      }
    );
    renderWithToast(<PromptsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getAllByText("+ New Prompt")).toHaveLength(2);
    });

    const buttons = screen.getAllByText("+ New Prompt");
    fireEvent.click(buttons[0]);

    await screen.findByLabelText("Prompt Name");

    // Fill in required fields
    const nameInput = screen.getByLabelText("Prompt Name");
    fireEvent.change(nameInput, { target: { value: "my-custom-prompt" } });

    // Template textarea (identified by placeholder)
    const templateInput = screen.getByPlaceholderText(/Hello.*{{name}}/) as HTMLTextAreaElement;
    fireEvent.change(templateInput, { target: { value: "Hello, {{name}}" } });

    // Select "Other..." and type a custom model
    const select = await screen.findByLabelText("Model");
    fireEvent.change(select, { target: { value: "__other__" } });

    await screen.findByPlaceholderText("Enter custom model name");
    const customInput = screen.getByPlaceholderText("Enter custom model name") as HTMLInputElement;
    fireEvent.change(customInput, { target: { value: "llama-3.1-70b" } });

    // Click Create button
    const createBtn = screen.getByRole("button", { name: /Create/ });
    fireEvent.click(createBtn);

    // Wait for success
    await waitFor(() => {
      expect(screen.getByText(/Created prompt/)).toBeInTheDocument();
    });

    // Verify the custom model was submitted in the request body
    expect(capturedBody).not.toBeNull();
    expect(capturedBody!.model).toBe("llama-3.1-70b");
    expect(capturedBody!.name).toBe("my-custom-prompt");
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

  // Regression tests for #270: #241's backend chat-type support
  // (PromptKind/PromptMessage, InterpolateChat, prompt.go handler) was
  // never wired up in the UI — there was no type selector, no
  // multi-message editor, and the version history had no branch to
  // render a chat-type version's messages.
  describe("chat-type prompt support", () => {
    it("shows a Text/Chat type selector in the New Prompt form, defaulting to Text", async () => {
      vi.spyOn(globalThis, "fetch").mockImplementation(
        (input: RequestInfo | URL) => {
          const url = typeof input === "string" ? input : input.toString();
          if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
            return Promise.resolve(mockListPrompts([]));
          }
          if (url.includes("/api/v1/models")) {
            return Promise.resolve({ ok: true, json: () => Promise.resolve(["gpt-4"]) } as Response);
          }
          return Promise.resolve({ ok: false } as Response);
        }
      );
      renderWithToast(<PromptsPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getAllByText("+ New Prompt")).toHaveLength(2);
      });
      fireEvent.click(screen.getAllByText("+ New Prompt")[0]);

      await screen.findByLabelText("Prompt Name");

      const textRadio = screen.getByRole("radio", { name: "Text" });
      const chatRadio = screen.getByRole("radio", { name: "Chat" });
      expect(textRadio).toBeChecked();
      expect(chatRadio).not.toBeChecked();

      // Flat template textarea is the default view.
      expect(screen.getByPlaceholderText(/Hello.*{{name}}/)).toBeInTheDocument();
    });

    it("switches to a multi-message editor when Chat is selected", async () => {
      vi.spyOn(globalThis, "fetch").mockImplementation(
        (input: RequestInfo | URL) => {
          const url = typeof input === "string" ? input : input.toString();
          if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
            return Promise.resolve(mockListPrompts([]));
          }
          if (url.includes("/api/v1/models")) {
            return Promise.resolve({ ok: true, json: () => Promise.resolve(["gpt-4"]) } as Response);
          }
          return Promise.resolve({ ok: false } as Response);
        }
      );
      renderWithToast(<PromptsPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getAllByText("+ New Prompt")).toHaveLength(2);
      });
      fireEvent.click(screen.getAllByText("+ New Prompt")[0]);
      await screen.findByLabelText("Prompt Name");

      fireEvent.click(screen.getByRole("radio", { name: "Chat" }));

      // Flat template textarea is gone, replaced by a message editor.
      expect(screen.queryByPlaceholderText(/Hello.*{{name}}/)).not.toBeInTheDocument();
      expect(screen.getByText("+ Add message")).toBeInTheDocument();
      // One message row exists by default.
      expect(screen.getAllByPlaceholderText("Message content...")).toHaveLength(1);
    });

    it("submits a chat-type prompt with kind and messages instead of template", async () => {
      let capturedBody: Record<string, unknown> | null = null;
      vi.spyOn(globalThis, "fetch").mockImplementation(
        async (input: RequestInfo | URL, init?: RequestInit) => {
          const url = typeof input === "string" ? input : input.toString();
          if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
            if (init?.method === "POST") {
              capturedBody = JSON.parse(init.body as string);
            }
            return Promise.resolve(mockListPrompts([]));
          }
          if (url.includes("/api/v1/models")) {
            return Promise.resolve({ ok: true, json: () => Promise.resolve(["gpt-4"]) } as Response);
          }
          return Promise.resolve({ ok: false } as Response);
        }
      );
      renderWithToast(<PromptsPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getAllByText("+ New Prompt")).toHaveLength(2);
      });
      fireEvent.click(screen.getAllByText("+ New Prompt")[0]);
      await screen.findByLabelText("Prompt Name");

      fireEvent.change(screen.getByLabelText("Prompt Name"), { target: { value: "chat-prompt" } });
      fireEvent.click(screen.getByRole("radio", { name: "Chat" }));

      // Fill the default message row.
      const roleSelects = screen.getAllByLabelText(/Message \d+ role/i);
      fireEvent.change(roleSelects[0], { target: { value: "system" } });
      const contentBoxes = screen.getAllByPlaceholderText("Message content...");
      fireEvent.change(contentBoxes[0], { target: { value: "You are helpful." } });

      // Add a second message.
      fireEvent.click(screen.getByText("+ Add message"));
      const contentBoxes2 = screen.getAllByPlaceholderText("Message content...");
      fireEvent.change(contentBoxes2[1], { target: { value: "Hello {{name}}" } });
      const roleSelects2 = screen.getAllByLabelText(/Message \d+ role/i);
      fireEvent.change(roleSelects2[1], { target: { value: "user" } });

      fireEvent.click(screen.getByRole("button", { name: /Create/ }));

      await waitFor(() => {
        expect(capturedBody).not.toBeNull();
      });

      expect(capturedBody!.kind).toBe("chat");
      expect(capturedBody!.messages).toEqual([
        { role: "system", content: "You are helpful." },
        { role: "user", content: "Hello {{name}}" },
      ]);
      expect(capturedBody!.template).toBeUndefined();
    });

    it("renders a chat-type version's message list in version history instead of the template", async () => {
      const chatVersions = [
        {
          version_id: "v1-id",
          project_id: "proj-1",
          name: "chatty",
          version: 1,
          kind: "chat",
          messages: [
            { role: "system", content: "You are concise." },
            { role: "user", content: "Summarize {{text}}" },
          ],
          template: "",
          model: "gpt-4",
          temperature: 0.2,
          max_tokens: 256,
          created_at: "2026-05-10T10:00:00Z",
        },
      ];
      vi.spyOn(globalThis, "fetch").mockImplementation(
        (input: RequestInfo | URL) => {
          const url = typeof input === "string" ? input : input.toString();
          if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
            return Promise.resolve(mockListPrompts([
              { name: "chatty", latest_version: 1, labels: {} },
            ]));
          }
          if (url.includes("/api/v1/prompts/chatty/versions")) {
            return Promise.resolve({
              ok: true,
              json: () => Promise.resolve({ name: "chatty", versions: chatVersions, count: 1 }),
            } as Response);
          }
          return Promise.resolve({ ok: false } as Response);
        }
      );
      renderWithToast(<PromptsPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("chatty")).toBeInTheDocument();
      });

      const expandButtons = screen.getAllByRole("button");
      const chevronButtons = expandButtons.filter((btn) => btn.querySelector("svg") !== null);
      fireEvent.click(chevronButtons[0]);

      await waitFor(() => {
        expect(screen.getByText("Version 1")).toBeInTheDocument();
      });

      // Message list rendered instead of a flat template block.
      expect(screen.getByText("You are concise.")).toBeInTheDocument();
      expect(screen.getByText("Summarize {{text}}")).toBeInTheDocument();
      expect(screen.getByText("system", { exact: false })).toBeInTheDocument();
    });

    it("compares two chat-type versions in the diff panel without crashing", async () => {
      const chatVersions = [
        {
          version_id: "v1-id",
          project_id: "proj-1",
          name: "chatty",
          version: 1,
          kind: "chat",
          messages: [{ role: "system", content: "You are concise." }],
          template: "",
          model: "gpt-4",
          temperature: 0.2,
          max_tokens: 256,
          created_at: "2026-05-10T10:00:00Z",
        },
        {
          version_id: "v2-id",
          project_id: "proj-1",
          name: "chatty",
          version: 2,
          kind: "chat",
          messages: [
            { role: "system", content: "You are concise." },
            { role: "user", content: "Summarize {{text}}" },
          ],
          template: "",
          model: "gpt-4",
          temperature: 0.2,
          max_tokens: 256,
          created_at: "2026-05-12T10:00:00Z",
        },
      ];
      vi.spyOn(globalThis, "fetch").mockImplementation(
        (input: RequestInfo | URL) => {
          const url = typeof input === "string" ? input : input.toString();
          if (url.includes("/api/v1/prompts") && !url.includes("/api/v1/prompts/")) {
            return Promise.resolve(mockListPrompts([
              { name: "chatty", latest_version: 2, labels: {} },
            ]));
          }
          if (url.includes("/api/v1/prompts/chatty/versions")) {
            return Promise.resolve({
              ok: true,
              json: () => Promise.resolve({ name: "chatty", versions: chatVersions, count: 2 }),
            } as Response);
          }
          return Promise.resolve({ ok: false } as Response);
        }
      );
      renderWithToast(<PromptsPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("chatty")).toBeInTheDocument();
      });

      const expandButtons = screen.getAllByRole("button");
      const chevronButtons = expandButtons.filter((btn) => btn.querySelector("svg") !== null);
      fireEvent.click(chevronButtons[0]);

      await waitFor(() => {
        expect(screen.getByText("Compare versions")).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText("Compare versions"));

      await waitFor(() => {
        expect(screen.getByText(/Compare: chatty/)).toBeInTheDocument();
      });

      // The added user message line should appear in the rendered diff.
      expect(screen.getByText(/user: Summarize \{\{text\}\}/)).toBeInTheDocument();
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
