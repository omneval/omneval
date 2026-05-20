import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import SettingsPage from "./Settings";
import { ToastProvider } from "@/components/Toast";

function renderWithToast(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

const mockApiKeys = [
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

const resolveKeys = (keys: unknown[]) =>
  ({ ok: true, json: () => Promise.resolve(keys) } as Response);

beforeEach(() => {
  vi.restoreAllMocks();
  vi.spyOn(console, "error").mockImplementation(() => {});
});

describe("SettingsPage", () => {
  it("renders the API Keys section header", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys([]));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("API Keys")).toBeInTheDocument();
    });
  });

  it("shows generate key buttons", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys([]));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
      expect(screen.getByText("+ New Service Key")).toBeInTheDocument();
    });
  });

  it("shows empty state when no keys exist", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys([]));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(
        screen.getByText(/No API keys yet/)
      ).toBeInTheDocument();
    });
  });

  it("renders a list of API keys", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      resolveKeys(mockApiKeys)
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("oev_proj_abc123")).toBeInTheDocument();
      expect(screen.getByText("oev_svc_worker-1")).toBeInTheDocument();
    });
  });

  it("shows kind badge for project keys", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      resolveKeys(mockApiKeys)
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Project")).toBeInTheDocument();
    });
  });

  it("shows kind badge for service keys", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      resolveKeys(mockApiKeys)
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Service")).toBeInTheDocument();
    });
  });

  it("shows service name for service keys", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      resolveKeys(mockApiKeys)
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      // Service name is rendered as "Service:" + <span>service_name</span>
      // across sibling elements within the same <p> tag.
      // Query by finding the paragraph containing the service name.
      expect(
        screen.getByText("my-agent")
      ).toBeInTheDocument();
    });
  });

  it("shows revoked badge for revoked keys", async () => {
    const revokedKey = [
      {
        key_id: "oev_proj_revoked",
        kind: "project" as const,
        created_at: "2026-05-10T10:00:00Z",
        revoked_at: "2026-05-15T12:00:00Z",
      },
    ];

    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys(revokedKey));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Revoked")).toBeInTheDocument();
    });
  });

  it("hides Revoke button for revoked keys", async () => {
    const revokedKey = [
      {
        key_id: "oev_proj_revoked",
        kind: "project" as const,
        created_at: "2026-05-10T10:00:00Z",
        revoked_at: "2026-05-15T12:00:00Z",
      },
    ];

    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys(revokedKey));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Revoked")).toBeInTheDocument();
    });

    const revokeButtons = screen.queryAllByRole("button", { name: /revoke/i });
    expect(revokeButtons).toHaveLength(0);
  });

  it("shows Revoke button for non-revoked keys", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys(mockApiKeys));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      const revokeButtons = screen.getAllByRole("button", { name: /revoke/i });
      expect(revokeButtons).toHaveLength(2);
    });
  });

  it("opens GenerateKeyDialog when clicking + New Project Key", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys([]));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    await waitFor(() => {
      expect(
        screen.getByText("Generate Project API Key")
      ).toBeInTheDocument();
    });
  });

  it("opens GenerateKeyDialog when clicking + New Service Key", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys([]));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Service Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Service Key"));

    await waitFor(() => {
      expect(
        screen.getByText("Generate Service API Key")
      ).toBeInTheDocument();
    });
  });

  it("shows service name input for service key generation", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys([]));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Service Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Service Key"));

    await waitFor(() => {
      expect(screen.getByText("Service Name")).toBeInTheDocument();
    });

    expect(
      screen.getByPlaceholderText("e.g. my-agent")
    ).toBeInTheDocument();
  });

  it("does not show service name input for project key generation", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys([]));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    // Should not have a service name input
    expect(
      screen.queryByPlaceholderText("e.g. my-agent")
    ).not.toBeInTheDocument();
  });

  it("closes dialog when clicking Cancel", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveKeys([]));

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    await waitFor(() => {
      expect(
        screen.getByText("Generate Project API Key")
      ).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Cancel"));

    // Dialog should be closed
    expect(
      screen.queryByText("Generate Project API Key")
    ).not.toBeInTheDocument();
  });

  it("generates API key and shows success dialog", async () => {
    const generatedKey = "oev_proj_XYZ123abc456def789ghi012jkl345mno678";
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => {
        callCount++;
        // First call is GET (list keys), second call is POST (generate key)
        if (callCount === 1) {
          return Promise.resolve(resolveKeys([]));
        }
        // POST request for key generation
        return Promise.resolve({
          ok: true,
          status: 201,
          json: () =>
            Promise.resolve({
              key_id: "key-123",
              project_id: "proj-1",
              kind: "project",
              raw_key: generatedKey,
              created_at: new Date().toISOString(),
            }),
        } as Response);
      }
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    await waitFor(() => {
      expect(
        screen.getByText("Generate Project API Key")
      ).toBeInTheDocument();
    });

    // Click Generate Key button
    fireEvent.click(screen.getByText("Generate Key"));

    // Wait for success dialog
    await waitFor(() => {
      expect(screen.getByText("Your API Key")).toBeInTheDocument();
    });

    // Check that the raw key is displayed
    expect(
      screen.getByText(generatedKey)
    ).toBeInTheDocument();
  });

  it("shows error when backend returns no raw_key", async () => {
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(resolveKeys([]));
        }
        // POST request returns success but no raw_key
        return Promise.resolve({
          ok: true,
          status: 201,
          json: () =>
            Promise.resolve({
              key_id: "key-123",
              project_id: "proj-1",
              kind: "project",
              created_at: new Date().toISOString(),
            }),
        } as Response);
      }
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    await waitFor(() => {
      expect(
        screen.getByText("Generate Project API Key")
      ).toBeInTheDocument();
    });

    // Click Generate Key button
    fireEvent.click(screen.getByText("Generate Key"));

    // Wait for error toast
    await waitFor(() => {
      expect(screen.getByText(/Server returned an invalid key/)).toBeInTheDocument();
    });
  });

  it("shows error when backend returns empty raw_key", async () => {
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(resolveKeys([]));
        }
        return Promise.resolve({
          ok: true,
          status: 201,
          json: () =>
            Promise.resolve({
              key_id: "key-123",
              project_id: "proj-1",
              kind: "project",
              raw_key: "",
              created_at: new Date().toISOString(),
            }),
        } as Response);
      }
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    await waitFor(() => {
      expect(
        screen.getByText("Generate Project API Key")
      ).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Generate Key"));

    // Wait for error toast
    await waitFor(() => {
      expect(screen.getByText(/Server returned an invalid key/)).toBeInTheDocument();
    });
  });

  it("shows error when backend returns non-string raw_key", async () => {
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(resolveKeys([]));
        }
        return Promise.resolve({
          ok: true,
          status: 201,
          json: () =>
            Promise.resolve({
              key_id: "key-123",
              project_id: "proj-1",
              kind: "project",
              raw_key: 12345,
              created_at: new Date().toISOString(),
            }),
        } as Response);
      }
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    fireEvent.click(screen.getByText("Generate Key"));

    // Wait for error toast
    await waitFor(() => {
      expect(screen.getByText(/Server returned an invalid key/)).toBeInTheDocument();
    });
  });

  it("shows error when backend returns HTTP error", async () => {
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(resolveKeys([]));
        }
        return Promise.resolve({
          ok: false,
          status: 400,
          json: () =>
            Promise.resolve({ error: "invalid request body" }),
        } as Response);
      }
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    fireEvent.click(screen.getByText("Generate Key"));

    // Wait for error toast
    await waitFor(() => {
      expect(screen.getByText("invalid request body")).toBeInTheDocument();
    });
  });

  it("shows error when backend fails without error field", async () => {
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(resolveKeys([]));
        }
        return Promise.resolve({
          ok: false,
          status: 500,
          json: () =>
            Promise.resolve({}),
        } as Response);
      }
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    fireEvent.click(screen.getByText("Generate Key"));

    // Wait for generic error toast
    await waitFor(() => {
      expect(
        screen.getByText("Failed to generate API key")
      ).toBeInTheDocument();
    });
  });

  it("copies key to clipboard when Copy Key is clicked", async () => {
    const generatedKey = "oev_proj_testKey123";
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(resolveKeys([]));
        }
        return Promise.resolve({
          ok: true,
          status: 201,
          json: () =>
            Promise.resolve({
              key_id: "key-123",
              project_id: "proj-1",
              kind: "project",
              raw_key: generatedKey,
              created_at: new Date().toISOString(),
            }),
        } as Response);
      }
    );

    const mockWriteText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText: mockWriteText } });

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    fireEvent.click(screen.getByText("Generate Key"));

    await waitFor(() => {
      expect(screen.getByText("Your API Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Copy Key"));

    await waitFor(() => {
      expect(mockWriteText).toHaveBeenCalledWith(generatedKey);
    });
  });

  it("closes the dialog on Close button", async () => {
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(resolveKeys([]));
        }
        return Promise.resolve({
          ok: true,
          status: 201,
          json: () =>
            Promise.resolve({
              key_id: "key-123",
              project_id: "proj-1",
              kind: "project",
              raw_key: "oev_proj_test123",
              created_at: new Date().toISOString(),
            }),
        } as Response);
      }
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    await waitFor(() => {
      expect(
        screen.getByText("Generate Project API Key")
      ).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Generate Key"));

    await waitFor(() => {
      expect(screen.getByText("Your API Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("Close"));

    // Dialog should be closed
    await waitFor(() => {
      expect(
        screen.queryByText("Your API Key")
      ).not.toBeInTheDocument();
    });
  });

  it("shows loading state while fetching keys", async () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(
      new Promise<Response>(() => {})
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    // Skeletons should be visible while loading
    const skeletons = document.querySelectorAll('[class*="animate-pulse"]');
    expect(skeletons.length).toBeGreaterThan(0);
  });

  it("handles fetch failure gracefully", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(
      new Error("Network error")
    );

    // Should not crash - should show empty state
    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(
        screen.getByText(/No API keys yet/)
      ).toBeInTheDocument();
    });
  });

  it("shows the generated key with CopyButton", async () => {
    const generatedKey = "oev_proj_testKey123";
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(resolveKeys([]));
        }
        return Promise.resolve({
          ok: true,
          status: 201,
          json: () =>
            Promise.resolve({
              key_id: "key-123",
              project_id: "proj-1",
              kind: "project",
              raw_key: generatedKey,
              created_at: new Date().toISOString(),
            }),
        } as Response);
      }
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Project Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Project Key"));

    fireEvent.click(screen.getByText("Generate Key"));

    await waitFor(() => {
      expect(screen.getByText("Your API Key")).toBeInTheDocument();
    });

    // Should have a copy button (the CopyButton component)
    const copyButtons = screen.getAllByRole("button");
    expect(copyButtons).toContainEqual(
      screen.getByText("Copy Key")
    );
  });

  it("shows service key warning when generating service key", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      resolveKeys([])
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Service Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Service Key"));

    await waitFor(() => {
      expect(
        screen.getByText("Generate Service API Key")
      ).toBeInTheDocument();
    });

    // Service name input should be visible
    expect(screen.getByText("Service Name")).toBeInTheDocument();
  });

  it("generates service key with service name", async () => {
    const generatedKey = "oev_svc_myagent123abc456def789ghi";
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(resolveKeys([]));
        }
        return Promise.resolve({
          ok: true,
          status: 201,
          json: () =>
            Promise.resolve({
              key_id: "key-456",
              project_id: "proj-1",
              kind: "service",
              service_name: "my-agent",
              raw_key: generatedKey,
              created_at: new Date().toISOString(),
            }),
        } as Response);
      }
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Service Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Service Key"));

    // Fill in service name
    fireEvent.change(screen.getByPlaceholderText("e.g. my-agent"), {
      target: { value: "my-agent" },
    });

    fireEvent.click(screen.getByText("Generate Key"));

    await waitFor(() => {
      expect(screen.getByText("Your API Key")).toBeInTheDocument();
    });

    // Verify service key prefix
    expect(screen.getByText(generatedKey)).toBeInTheDocument();
  });

  it("disables Generate Key button when service name is empty", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      resolveKeys([])
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Service Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Service Key"));

    const generateButton = screen.getByText("Generate Key");
    expect(generateButton).toBeDisabled();
  });

  it("enables Generate Key button when service name is filled", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      resolveKeys([])
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("+ New Service Key")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText("+ New Service Key"));

    // Fill in service name
    fireEvent.change(screen.getByPlaceholderText("e.g. my-agent"), {
      target: { value: "my-agent" },
    });

    const generateButton = screen.getByText("Generate Key");
    expect(generateButton).not.toBeDisabled();
  });

  it("fetches API keys on mount", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      resolveKeys(mockApiKeys)
    );

    renderWithToast(<SettingsPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/projects/proj-1/api-keys"
      );
    });
  });
});
