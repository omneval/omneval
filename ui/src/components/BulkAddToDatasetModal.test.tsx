import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import type { ComponentProps } from "react";
import BulkAddToDatasetModal from "./BulkAddToDatasetModal";

// ── Helper data ──────────────────────────────────────────────────

const mockSpanIds = ["span-1", "span-2", "span-3"];

function renderModal(
  props: Partial<ComponentProps<typeof BulkAddToDatasetModal>> = {}
) {
  return render(
    <BulkAddToDatasetModal
      spanIds={mockSpanIds}
      onClose={() => {}}
      onSuccess={() => {}}
      {...props}
    />
  );
}

// ── Tests ─────────────────────────────────────────────────────────

describe("BulkAddToDatasetModal", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders with the correct span count", () => {
    renderModal();
    expect(screen.getByText("3 spans selected")).toBeInTheDocument();
  });

  it("shows loading state when datasets are being fetched", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => {
      return new Promise(() => {
        // Never resolves — simulate loading
      });
    });

    renderModal();

    await waitFor(() => {
      expect(screen.getByText("Loading datasets…")).toBeInTheDocument();
    });
  });

  it("renders existing datasets in the dropdown", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          datasets: [
            { dataset_id: "ds-1", name: "Dataset A", item_count: 5 },
            { dataset_id: "ds-2", name: "Dataset B", item_count: 10 },
          ],
        }),
    } as Response);

    renderModal();

    await waitFor(() => {
      expect(screen.getByRole("combobox")).toBeInTheDocument();
    });

    // Dataset names appear in option elements
    expect(screen.getByRole("option", { name: "Dataset A (5 items)" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Dataset B (10 items)" })).toBeInTheDocument();
  });

  it("selects the first dataset by default when only one exists", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          datasets: [
            { dataset_id: "ds-1", name: "Only Dataset", item_count: 0 },
          ],
        }),
    } as Response);

    renderModal();

    await waitFor(() => {
      const select = screen.getByRole("combobox");
      expect(select).toHaveValue("ds-1");
    });
  });

  it("submits the batch API call with correct payload", async () => {
    const spansData = [
      { span_id: "span-1", input: "input 1", output: "output 1" },
      { span_id: "span-2", input: "input 2", output: "output 2" },
      { span_id: "span-3", input: "input 3", output: "output 3" },
    ];
    let capturedBody: string | null = null;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, _init) => {
      if (String(url).includes("/api/v1/datasets") && !String(url).includes("/items")) {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              datasets: [
                { dataset_id: "ds-1", name: "Target DS", item_count: 0 },
              ],
            }),
        } as Response;
      }
      if (String(url).includes("/api/v1/spans")) {
        return {
          ok: true,
          json: () => Promise.resolve({ spans: spansData, next: "", limit: 100 }),
        } as Response;
      }
      if (String(url).includes("/items/batch")) {
        capturedBody = _init?.body as string;
        return {
          ok: true,
          json: () => Promise.resolve({ created: 3, span_ids: ["span-1", "span-2", "span-3"] }),
        } as Response;
      }
      return { ok: false, text: () => Promise.resolve("not found") } as Response;
    });

    renderModal();

    // Wait for combobox and dataset option to appear
    await waitFor(() => {
      expect(screen.getByRole("option", { name: "Target DS (0 items)" })).toBeInTheDocument();
    });

    // Select the dataset
    const select = screen.getByRole("combobox");
    await act(async () => {
      fireEvent.change(select, { target: { value: "ds-1" } });
    });

    // Click save button
    const saveBtn = screen.getByRole("button", { name: /Save/i });
    await act(async () => {
      fireEvent.click(saveBtn);
    });

    // Wait for the batch save request
    await waitFor(() => {
      expect(capturedBody).not.toBeNull();
    });

    const body = JSON.parse(capturedBody!);
    expect(body.items).toHaveLength(3);
    expect(body.items[0].source_span_id).toBe("span-1");
    expect(body.items[0].input).toBe("input 1");
    expect(body.items[0].expected_output).toBe("output 1");
  });

  it("closes on success", async () => {
    let closed = false;
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          datasets: [
            { dataset_id: "ds-1", name: "Target DS", item_count: 0 },
          ],
          created: 3,
        }),
    } as Response);

    renderModal({
      onSuccess: () => {
        closed = true;
      },
    });

    await waitFor(() => {
      expect(screen.getByRole("option", { name: "Target DS (0 items)" })).toBeInTheDocument();
    });

    const select = screen.getByRole("combobox");
    await act(async () => {
      fireEvent.change(select, { target: { value: "ds-1" } });
    });

    const saveBtn = screen.getByRole("button", { name: /Save/i });
    await act(async () => {
      fireEvent.click(saveBtn);
    });

    await waitFor(() => {
      expect(closed).toBe(true);
    });
  });

  it("shows error when save fails", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, _init) => {
      if (String(url).includes("/api/v1/datasets") && !String(url).includes("/items")) {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              datasets: [
                { dataset_id: "ds-1", name: "Target DS", item_count: 0 },
              ],
            }),
        } as Response;
      }
      if (String(url).includes("/items/batch")) {
        return {
          ok: false,
          statusText: "Bad Request",
          text: () => Promise.resolve("Dataset not found"),
        } as Response;
      }
      return { ok: true, json: () => Promise.resolve({ spans: [], next: "", limit: 100 }) } as Response;
    });

    renderModal();

    await waitFor(() => {
      expect(screen.getByRole("option", { name: "Target DS (0 items)" })).toBeInTheDocument();
    });

    const select = screen.getByRole("combobox");
    await act(async () => {
      fireEvent.change(select, { target: { value: "ds-1" } });
    });

    // Trigger a save
    const saveBtn = screen.getByRole("button", { name: /Save/i });
    await act(async () => {
      fireEvent.click(saveBtn);
    });

    await waitFor(() => {
      expect(screen.getByText("Dataset not found")).toBeInTheDocument();
    });
  });

  it("shows no datasets message when there are none", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ datasets: [] }),
    } as Response);

    renderModal();

    await waitFor(() => {
      expect(screen.getByText("No datasets found. Create one to get started.")).toBeInTheDocument();
    });
  });

  it("allows creating a new dataset and then saving", async () => {
    let capturedBody: string | null = null;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url, _init) => {
      if (String(url) === "/api/v1/datasets" && _init?.method === "POST") {
        capturedBody = String(_init.body);
        return {
          ok: true,
          json: () => Promise.resolve({ dataset_id: "new-ds-1" }),
        } as Response;
      }
      if (String(url).includes("/api/v1/datasets") && !String(url).includes("/items")) {
        return {
          ok: true,
          json: () =>
            Promise.resolve({
              datasets: [
                { dataset_id: "ds-1", name: "Existing DS", item_count: 0 },
              ],
            }),
        } as Response;
      }
      if (String(url).includes("/items/batch")) {
        capturedBody = String(url);
        return {
          ok: true,
          json: () => Promise.resolve({ created: 2, span_ids: ["span-1", "span-2"] }),
        } as Response;
      }
      return { ok: false, text: () => Promise.resolve("not found") } as Response;
    });

    renderModal({ spanIds: ["span-1", "span-2"] });

    // Wait for initial datasets to load
    await waitFor(() => {
      expect(screen.getByRole("option", { name: "Existing DS (0 items)" })).toBeInTheDocument();
    });

    // Click "Create new dataset"
    await act(async () => {
      fireEvent.click(screen.getByText("+ Create new dataset"));
    });

    // Type a new dataset name
    const nameInput = screen.getByPlaceholderText("e.g., eval-prompts-v1");
    await act(async () => {
      fireEvent.change(nameInput, { target: { value: "New Dataset" } });
    });

    // Click Create
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Create" }));
    });

    // Wait for save button to appear again and click it
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /Save/i })).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Save/i }));
    });

    await waitFor(() => {
      expect(capturedBody).not.toBeNull();
    });
  });

  it("hides the save button when no datasets exist", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ datasets: [] }),
    } as Response);

    renderModal();

    // When no datasets, there's no combobox and no save button — user must create first
    // The save button is only rendered in the footer when datasets.length > 0
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: /Save/i })).not.toBeInTheDocument();
    });
  });
});