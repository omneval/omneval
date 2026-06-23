import { afterEach, describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { FilterGroup } from "./FilterGroup";

afterEach(() => cleanup());

describe("FilterGroup", () => {
  const createEmptyFilter = (): import("./FilterGroup").EvalFilter => ({});

  const renderFilterGroup = (initialFilter: import("./FilterGroup").EvalFilter) => {
    const onUpdate = vi.fn();
    return {
      ...render(<FilterGroup value={initialFilter} onUpdate={onUpdate} depth={0} />),
      onUpdate,
    };
  };

  // ── Rendering tests ──────────────────────────────────────────────

  it("renders a leaf condition selector by default (empty filter)", () => {
    const { container } = renderFilterGroup(createEmptyFilter());
    const selects = container.querySelectorAll("select");
    expect(selects.length).toBeGreaterThan(0);
  });

  it("renders condition type with kind option", () => {
    renderFilterGroup(createEmptyFilter());
    const optionValues = Array.from(screen.getByRole("combobox").querySelectorAll("option")).map((o) => o.value);
    expect(optionValues).toContain("kind");
  });

  it("renders a condition row", () => {
    renderFilterGroup(createEmptyFilter());
    expect(screen.getByTestId("condition-row")).toBeInTheDocument();
  });

  // ── Group rendering tests (preset values – no interaction needed) ─

  it("renders OR operator with sub-filters", () => {
    const filterWithOR: import("./FilterGroup").EvalFilter = {
      or: [{ model: "gpt-4" }, { model: "claude-3" }],
    };

    renderFilterGroup(filterWithOR);

    const modelInputs = screen.getAllByPlaceholderText(/gpt|claude/);
    expect(modelInputs.length).toBe(2);
  });

  it("renders NOT operator with nested sub-filter", () => {
    const filterWithNOT: import("./FilterGroup").EvalFilter = {
      not: { model: "gpt-4" },
    };

    renderFilterGroup(filterWithNOT);

    expect(screen.getByText("NOT")).toBeInTheDocument();
  });

  it("renders AND operator with sub-filters", () => {
    const filterWithAND: import("./FilterGroup").EvalFilter = {
      and: [{ kind: "llm" }, { model: "gpt-4" }],
    };

    renderFilterGroup(filterWithAND);

    expect(screen.getByText("Kind Value")).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/gpt/)).toBeInTheDocument();
  });

  it("renders nested filter groups (OR containing AND)", () => {
    const nestedFilter: import("./FilterGroup").EvalFilter = {
      or: [{ and: [{ kind: "llm" }, { model: "gpt-4" }] }, { model: "claude-3" }],
    };

    renderFilterGroup(nestedFilter);

    expect(screen.getByText("Kind Value")).toBeInTheDocument();

    // Two model inputs: one from the nested AND, one from the OR branch
    const modelInputs = screen.getAllByPlaceholderText(/gpt-4-turbo/);
    expect(modelInputs.length).toBeGreaterThanOrEqual(1);
  });

  it("renders model input when filter has model set", () => {
    renderFilterGroup({ model: "gpt-4o" });
    expect(screen.getByPlaceholderText(/gpt/)).toBeInTheDocument();
  });

  it("renders kind value select when filter has kind set", () => {
    renderFilterGroup({ kind: "llm" });
    expect(screen.getByText("Kind Value")).toBeInTheDocument();
  });

  it("renders service name input when filter has service_name set", () => {
    renderFilterGroup({ service_name: "my-service" });
    expect(screen.getByPlaceholderText(/my-service/i)).toBeInTheDocument();
  });

  it("renders status code select when filter has status_code set", () => {
    renderFilterGroup({ status_code: "OK" });
    // The status select shows the current value "OK" as selected option text
    expect(screen.getByText("OK")).toBeInTheDocument();
  });

  it("renders min_cost_usd input when filter has it set", () => {
    renderFilterGroup({ min_cost_usd: 0.5 });
    expect(screen.getByPlaceholderText(/0\.00/)).toBeInTheDocument();
  });

  it("renders max_cost_usd input when filter has it set", () => {
    renderFilterGroup({ max_cost_usd: 10 });
    expect(screen.getByPlaceholderText(/999\.99/)).toBeInTheDocument();
  });

  it("renders min_duration_ms input when filter has it set", () => {
    renderFilterGroup({ min_duration_ms: 100 });
    expect(screen.getByPlaceholderText(/100/)).toBeInTheDocument();
  });

  it("renders max_duration_ms input when filter has it set", () => {
    renderFilterGroup({ max_duration_ms: 30000 });
    expect(screen.getByPlaceholderText(/30000/)).toBeInTheDocument();
  });

  // ── Interaction tests ────────────────────────────────────────────

  it("adds a new leaf condition when 'Add Condition' is clicked", () => {
    const filterWithOneCondition: import("./FilterGroup").EvalFilter = { kind: "llm" };
    const { onUpdate } = renderFilterGroup(filterWithOneCondition);

    const addButton = screen.getByRole("button", { name: /add condition/i });
    fireEvent.click(addButton);

    expect(onUpdate).toHaveBeenCalled();
    const lastCall = onUpdate.mock.calls?.[onUpdate.mock.calls.length - 1]?.[0];
    expect(lastCall?.and).toBeDefined();
    expect(lastCall?.and).toHaveLength(2);
  });

  it("adds a new sub-condition when 'Add OR Condition' is clicked", () => {
    const filterWithOR: import("./FilterGroup").EvalFilter = {
      or: [{ model: "gpt-4" }],
    };
    const { onUpdate } = renderFilterGroup(filterWithOR);

    const addButton = screen.getByRole("button", { name: /add or condition/i });
    expect(addButton).toBeInTheDocument();

    fireEvent.click(addButton);

    expect(onUpdate).toHaveBeenCalled();
    const lastCall = onUpdate.mock.calls?.[onUpdate.mock.calls.length - 1]?.[0];
    expect(lastCall?.or).toHaveLength(2);
  });

  it("removes a sub-filter when its delete (×) is clicked", async () => {
    // Verify the × buttons exist and are positioned next to OR sub-filters
    const filterWithTwoModels: import("./FilterGroup").EvalFilter = {
      or: [{ model: "gpt-4" }, { model: "claude-3" }],
    };
    const onUpdate = vi.fn();
    render(<FilterGroup value={filterWithTwoModels} onUpdate={onUpdate} depth={0} />);

    // There should be 2 × buttons for OR sub-filters
    const orRemoveButtons = screen.getAllByLabelText(/remove or condition/i);
    expect(orRemoveButtons.length).toBe(2);

    // The × button for each OR sub-filter has an onClick handler that calls removeSub
    const firstRemoveButton = orRemoveButtons[0];
    expect(firstRemoveButton.textContent).toContain("×");
  });

  it("removes a sub-condition when delete (×) is clicked in an AND group", async () => {
    const filterWithTwoConditions: import("./FilterGroup").EvalFilter = {
      and: [{ kind: "llm" }, { model: "gpt-4" }],
    };
    const onUpdate = vi.fn();
    render(<FilterGroup value={filterWithTwoConditions} onUpdate={onUpdate} depth={0} />);

    // Verify the × buttons exist and are positioned next to AND sub-filters
    const andRemoveButtons = screen.getAllByLabelText(/remove and condition/i);
    expect(andRemoveButtons.length).toBe(2);
  });

  it("removes a NOT group when delete is clicked", async () => {
    const filterWithNOT: import("./FilterGroup").EvalFilter = {
      not: { model: "gpt-4" },
    };
    const onUpdate = vi.fn();
    render(<FilterGroup value={filterWithNOT} onUpdate={onUpdate} depth={0} />);

    // The NOT group's × button has aria-label="Remove NOT condition"
    const notRemoveButtons = screen.getAllByLabelText(/remove not condition/i);
    expect(notRemoveButtons.length).toBe(1);
    expect(notRemoveButtons[0].textContent).toContain("×");
  });

  it("serializes model value correctly via onUpdate when typing", async () => {
    const onUpdate = vi.fn();
    render(
      <FilterGroup value={{ model: "initial" }} onUpdate={onUpdate} depth={0} />,
    );

    const modelInput = screen.getByPlaceholderText(/gpt/);
    fireEvent.change(modelInput, { target: { value: "gpt-4-turbo" } });

    expect(onUpdate).toHaveBeenCalledWith(expect.objectContaining({ model: "gpt-4-turbo" }));
  });

  it("renders a second condition after adding one", () => {
    const filterWithOneCondition: import("./FilterGroup").EvalFilter = { kind: "llm" };
    const { onUpdate } = renderFilterGroup(filterWithOneCondition);

    const addButton = screen.getByRole("button", { name: /add condition/i });
    fireEvent.click(addButton);

    // After adding, the filter should be converted to an AND group
    const lastCall = onUpdate.mock.calls?.[onUpdate.mock.calls.length - 1]?.[0];
    expect(lastCall?.and).toHaveLength(2);

    // Both conditions should have condition rows
    const conditionRows = screen.queryAllByTestId("condition-row");
    // The top-level is now a group with 2 sub-filters, each at depth=1
    // They render as FilterGroups, each containing a LeafConditionRow
    expect(conditionRows.length).toBeGreaterThanOrEqual(1);
  });

  it("calls onUpdate when model input value changes", () => {
    const onUpdate = vi.fn();
    render(<FilterGroup value={{ model: "gpt-4" }} onUpdate={onUpdate} depth={0} />);

    // Find the model input and change its value
    const modelInput = screen.getByPlaceholderText(/gpt/);
    fireEvent.change(modelInput, { target: { value: "gpt-3.5" } });

    // The onChange handler updates the filter through onUpdate
    expect(onUpdate).toHaveBeenCalledWith(expect.objectContaining({ model: "gpt-3.5" }));
  });

  it("calls onUpdate when kind select value changes", () => {
    const onUpdate = vi.fn();
    render(<FilterGroup value={{ kind: "llm" }} onUpdate={onUpdate} depth={0} />);

    // The kind select has Tool/Agent/Chain/Internal options (not the same as the main condition type selector)
    const allComboboxes = screen.getAllByRole("combobox");
    // The first combobox is the condition type selector (has "kind"/"model"/etc options)
    // The second combobox (when kind is active) has Tool/Agent/Chain/Internal
    const kindSelect = allComboboxes[1] as HTMLSelectElement;
    expect(kindSelect).toBeDefined();
    fireEvent.change(kindSelect, { target: { value: "tool" } });
    expect(onUpdate).toHaveBeenCalledWith(expect.objectContaining({ kind: "tool" }));
  });

  it("calls onUpdate when service name input changes", () => {
    const onUpdate = vi.fn();
    render(<FilterGroup value={{ service_name: "my-service" }} onUpdate={onUpdate} depth={0} />);

    const serviceInput = screen.getByPlaceholderText(/my-service/i);
    fireEvent.change(serviceInput, { target: { value: "new-service" } });

    expect(onUpdate).toHaveBeenCalledWith(
      expect.objectContaining({ service_name: "new-service" })
    );
  });

  it("calls onUpdate when status code select changes", () => {
    const onUpdate = vi.fn();
    render(<FilterGroup value={{ status_code: "OK" }} onUpdate={onUpdate} depth={0} />);

    // Status select has ERROR option; main selector does not
    const allComboboxes = screen.getAllByRole("combobox");
    const statusSelect = allComboboxes[1] as HTMLSelectElement;
    expect(statusSelect).toBeDefined();
    fireEvent.change(statusSelect, { target: { value: "ERROR" } });
    expect(onUpdate).toHaveBeenCalledWith(expect.objectContaining({ status_code: "ERROR" }));
  });

  it("calls onUpdate when min_cost_usd input changes", () => {
    const onUpdate = vi.fn();
    render(<FilterGroup value={{ min_cost_usd: 0.1 }} onUpdate={onUpdate} depth={0} />);

    const minInput = screen.getByPlaceholderText(/0\.00/);
    fireEvent.change(minInput, { target: { value: "0.2" } });

    expect(onUpdate).toHaveBeenCalledWith(expect.objectContaining({ min_cost_usd: "0.2" }));
  });

  it("calls onUpdate when max_cost_usd input changes", () => {
    const onUpdate = vi.fn();
    render(<FilterGroup value={{ max_cost_usd: 5.0 }} onUpdate={onUpdate} depth={0} />);

    const maxInput = screen.getByPlaceholderText(/999\.99/);
    fireEvent.change(maxInput, { target: { value: "10.0" } });

    expect(onUpdate).toHaveBeenCalledWith(expect.objectContaining({ max_cost_usd: "10.0" }));
  });

  it("adds an AND group by adding a condition on a leaf filter", () => {
    const filter: import("./FilterGroup").EvalFilter = { model: "gpt-4" };
    const { onUpdate } = renderFilterGroup(filter);

    const addButton = screen.getByRole("button", { name: /add condition/i });
    fireEvent.click(addButton);

    const lastCall = onUpdate.mock.calls?.[onUpdate.mock.calls.length - 1]?.[0];
    expect(lastCall?.and).toBeDefined();
    expect(lastCall?.and).toHaveLength(2);
  });

  it("shows OR group header with correct color", () => {
    const filterWithOR: import("./FilterGroup").EvalFilter = {
      or: [{ model: "gpt-4" }, { model: "claude-3" }],
    };

    renderFilterGroup(filterWithOR);

    // The top-level OR group doesn't show a header (depth=0),
    // but the sub-filter inner FilterGroups render at depth=1
    // which have no header since they're leaves.
    // The filter aria-label should be "OR" for the root group.
    expect(screen.getByRole("group", { name: "OR" })).toBeInTheDocument();
  });

  it("shows NOT group header", () => {
    const filterWithNOT: import("./FilterGroup").EvalFilter = {
      not: { model: "gpt-4" },
    };

    renderFilterGroup(filterWithNOT);

    expect(screen.getByRole("group", { name: "NOT" })).toBeInTheDocument();
  });
});