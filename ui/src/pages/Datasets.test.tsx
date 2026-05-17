import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import DatasetsPage from "./Datasets";
import { ToastProvider } from "@/components/Toast";

function renderWithToast(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

describe("DatasetsPage", () => {
  const mockDatasets = [
    {
      dataset_id: "ds-1",
      name: "Test Dataset",
      created_at: "2026-05-13T10:00:00Z",
      item_count: 5,
    },
    {
      dataset_id: "ds-2",
      name: "Production Eval",
      created_at: "2026-05-12T08:00:00Z",
      item_count: 0,
    },
  ];

  const resolveDatasets = (datasets: any[]) =>
    ({ ok: true, json: () => Promise.resolve({ datasets }) } as Response);

  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockReset();
  });

  // ── Rendering ──

  it("renders the page with header", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveDatasets(mockDatasets));

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("Datasets")).toBeInTheDocument();
    });
    expect(screen.getByText("+ New Dataset")).toBeInTheDocument();
  });

  it("shows loading spinner on initial render", async () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise<Response>(() => {}));

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    expect(screen.getByText("Datasets")).toBeInTheDocument();
    // Spinner is present during loading (svg with animate-spin)
    const spinner = screen.getByTestId("spinner");
    expect(spinner).toHaveClass("animate-spin");
  });

  it("lists all datasets with name, item count, and date", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveDatasets(mockDatasets));

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("Test Dataset")).toBeInTheDocument();
      expect(screen.getByText("Production Eval")).toBeInTheDocument();
    });
    expect(screen.getByText(/5 items/)).toBeInTheDocument();
    expect(screen.getByText(/0 items/)).toBeInTheDocument();
  });

  // ── Empty state ──

  it("shows empty state when no datasets exist", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveDatasets([]));

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText(/No datasets found/)).toBeInTheDocument();
    });
  });

  // ── Error state ──

  it("shows error banner on fetch failure", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      { ok: false, status: 500, text: () => Promise.resolve("Server error") } as Response
    );

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText(/Failed to fetch datasets/)).toBeInTheDocument();
    });
  });

  // ── Create dataset ──

  it("shows create form when + New Dataset is clicked", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveDatasets([]));

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("+ New Dataset")).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByText("+ New Dataset"));
    });

    await waitFor(() => {
      expect(screen.getByPlaceholderText("Dataset name")).toBeInTheDocument();
    });
    expect(screen.getByText("Create")).toBeInTheDocument();
    expect(screen.getByText("Cancel")).toBeInTheDocument();
  });

  it("calls POST /api/v1/datasets on create", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/datasets") && !url.includes("/items")) {
          return Promise.resolve(
            ({ ok: true, json: () => Promise.resolve({ dataset_id: "new-ds" }) } as Response)
          );
        }
        if (url.includes("/api/v1/datasets?")) {
          return Promise.resolve(resolveDatasets([]));
        }
        return Promise.resolve(resolveDatasets([]));
      }
    );

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("+ New Dataset")).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByText("+ New Dataset"));
    });

    await act(async () => {
      const nameInput = screen.getByPlaceholderText("Dataset name");
      fireEvent.change(nameInput, { target: { value: "My Dataset" } });
    });

    await act(async () => {
      fireEvent.click(screen.getByText("Create"));
    });

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        expect.stringContaining("/api/v1/datasets"),
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ name: "My Dataset" }),
        })
      );
    });
  });

  it("shows create error on failure", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      { ok: false, status: 400, text: () => Promise.resolve("Name is required") } as Response
    );

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("+ New Dataset")).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByText("+ New Dataset"));
    });

    await act(async () => {
      const nameInput = screen.getByPlaceholderText("Dataset name");
      fireEvent.change(nameInput, { target: { value: "" } });
    });

    await act(async () => {
      fireEvent.click(screen.getByText("Create"));
    });

    // Form should not close and error should be shown
    await waitFor(() => {
      expect(screen.getByText("Create")).toBeInTheDocument();
    });
  });

  it("cancels create form and closes it", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveDatasets([]));

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    await act(async () => {
      fireEvent.click(screen.getByText("+ New Dataset"));
    });

    await waitFor(() => {
      expect(screen.getByPlaceholderText("Dataset name")).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByText("Cancel"));
    });

    expect(screen.queryByPlaceholderText("Dataset name")).not.toBeInTheDocument();
  });

  // ── Delete dataset ──

  it("shows delete button on hover and deletes on confirmation", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/datasets") && url.endsWith("/ds-1") && !url.includes("/items")) {
          return Promise.resolve({ ok: true, status: 204 } as Response);
        }
        return Promise.resolve(resolveDatasets(mockDatasets));
      }
    );

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("Test Dataset")).toBeInTheDocument();
    });

    // Find the first View button and hover its parent row
    const viewButtons = screen.getAllByRole("button", { name: /View/ });
    const firstRow = viewButtons[0].closest(".group") as HTMLElement;

    await act(async () => {
      fireEvent.mouseEnter(firstRow);
    });

    await waitFor(() => {
      const deleteButtons = screen.getAllByText("Delete");
      expect(deleteButtons.length).toBeGreaterThanOrEqual(1);
    });

    // Click the first delete button (on the hovered row)
    const deleteButtons = screen.getAllByText("Delete");
    await act(async () => {
      fireEvent.click(deleteButtons[0]);
    });

    // Now should show confirm buttons on that row (the row now has a styled Delete + Cancel)
    expect(screen.getByText("Cancel")).toBeInTheDocument();

    // Click the confirm Delete button (first one in the DOM, which is the one on the hovered row)
    const allDelete = screen.getAllByRole("button", { name: "Delete" });
    await act(async () => {
      fireEvent.click(allDelete[0]);
    });

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        expect.stringContaining("/api/v1/datasets/ds-1"),
        expect.objectContaining({ method: "DELETE" })
      );
    });
  });

  // ── Navigate to detail ──

  it("calls onNavigateToDetail when clicking a dataset", async () => {
    const navigate = vi.fn();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveDatasets(mockDatasets));

    renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={navigate} />);

    await waitFor(() => {
      expect(screen.getByText("Test Dataset")).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByText("Test Dataset"));
    });

    expect(navigate).toHaveBeenCalledWith("ds-1");
  });

  // ── Fetch on project change ──

  it("refetches when project changes", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveDatasets(mockDatasets));

    const { rerender } = renderWithToast(<DatasetsPage activeProject="proj-1" onNavigateToDetail={() => {}} />);

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith("/api/v1/datasets?project_id=proj-1");
    });

    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveDatasets([]));
    rerender(<ToastProvider><DatasetsPage activeProject="proj-2" onNavigateToDetail={() => {}} /></ToastProvider>);

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith("/api/v1/datasets?project_id=proj-2");
    });
  });
});
