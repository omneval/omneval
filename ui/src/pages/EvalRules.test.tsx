import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import EvalRulesPage, { type EvalRule } from "./EvalRules";
import { ToastProvider } from "@/components/Toast";


function renderWithToast(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

describe("EvalRulesPage", () => {
  beforeEach(() => {
    vi.stubGlobal("confirm", () => true);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  const mockRules = [
    {
      rule_id: "rule-1",
      project_id: "proj-1",
      name: "Production LLM Eval",
      judge_model: "gpt-4",
      prompt_name: "judge-v1",
      prompt_version: 1,
      sample_rate: 1.0,
      enabled: true,
      created_at: "2026-05-13T10:00:00Z",
      filter: {
        kind: "llm",
        model: "gpt-4-turbo",
        service_name: "my-service",
      },
    },
    {
      rule_id: "rule-2",
      project_id: "proj-1",
      name: "Cost Monitoring",
      judge_model: "claude-3",
      prompt_name: "cost-check",
      prompt_version: 2,
      sample_rate: 0.5,
      enabled: false,
      created_at: "2026-05-12T08:00:00Z",
      filter: {
        min_cost_usd: 0.1,
      },
    },
  ];

  const resolveRules = (rules: any[]) =>
    ({ ok: true, json: () => Promise.resolve({ rules }) } as Response);

  it("renders the page with header and project id", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(mockRules));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Eval Rules")).toBeInTheDocument();
    });

    expect(screen.getByText(/2 rules/)).toBeInTheDocument();
    expect(screen.getByText(/proj-1/)).toBeInTheDocument();
  });

  it("shows loading state on initial render", async () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise<Response>(() => {}));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    expect(screen.getByText(/Loading eval rules.../)).toBeInTheDocument();
  });

  it("renders a list of eval rules with key fields", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(mockRules));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Production LLM Eval")).toBeInTheDocument();
    });

    expect(screen.getByText("Cost Monitoring")).toBeInTheDocument();
    expect(screen.getByText("gpt-4")).toBeInTheDocument();
    expect(screen.getByText("claude-3")).toBeInTheDocument();
    expect(screen.getByText("50%")).toBeInTheDocument();
    expect(screen.getByText(/May 12/)).toBeInTheDocument();
  });

  it("shows delete button on each rule row", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(mockRules));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getAllByRole("button", { name: /delete/i })).toHaveLength(2);
    });
  });

  it("shows empty state when no rules exist", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules([]));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("No eval rules yet")).toBeInTheDocument();
      expect(screen.getByText(/Create your first eval rule/)).toBeInTheDocument();
    });
  });

  it("calls DELETE /api/v1/eval-rules/:id on delete confirmation", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/eval-rules") && !url.includes("/api/v1/eval-rules/")) {
          return Promise.resolve(resolveRules(mockRules));
        }
        if (url.includes("/api/v1/eval-rules/")) {
          return Promise.resolve({ ok: true, status: 204, text: async () => "" } as Response);
        }
        return Promise.resolve(resolveRules(mockRules));
      }
    );
    vi.spyOn(window, "confirm").mockReturnValue(true);

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getAllByRole("button", { name: /delete/i })).toHaveLength(2);
    });

    const deleteButtons = screen.getAllByRole("button", { name: /delete/i });
    fireEvent.click(deleteButtons[0]);

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        expect.stringContaining("/api/v1/eval-rules/rule-1"),
        expect.objectContaining({ method: "DELETE" })
      );
    });
  });

  it("cancels delete when confirm returns false", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(mockRules));
    vi.spyOn(window, "confirm").mockReturnValue(false);

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getAllByRole("button", { name: /delete/i })).toHaveLength(2);
    });

    const deleteButtons = screen.getAllByRole("button", { name: /delete/i });
    fireEvent.click(deleteButtons[0]);

    expect(fetch).not.toHaveBeenCalledWith(
      expect.stringContaining("/api/v1/eval-rules/rule-1"),
      expect.anything()
    );
  });

  it("fetches rules on mount and on project change", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules([]));

    const { rerender } = renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/eval-rules?project_id=proj-1"
      );
    });

    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules([mockRules[0]]));
    rerender(<ToastProvider><EvalRulesPage activeProject="proj-2" /></ToastProvider>);

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/eval-rules?project_id=proj-2"
      );
    });
  });

  it("shows disabled badge for disabled rules", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(mockRules));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Disabled")).toBeInTheDocument();
    });
  });

  it("shows enabled badge for enabled rules", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(mockRules));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Enabled")).toBeInTheDocument();
    });
  });

  it("handles rules with null/undefined filter without crashing", async () => {
    const rulesWithNullFilter = [
      {
        rule_id: "rule-1",
        project_id: "proj-1",
        name: "Rule With Null Filter",
        judge_model: "gpt-4",
        prompt_name: "judge-v1",
        prompt_version: 1,
        sample_rate: 1.0,
        enabled: true,
        created_at: "2026-05-13T10:00:00Z",
        filter: null,
      },
    ];

    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(rulesWithNullFilter));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Rule With Null Filter")).toBeInTheDocument();
      expect(screen.getByText("gpt-4")).toBeInTheDocument();
    });
  });

  it("handles rules with missing filter field without crashing", async () => {
    const rulesWithMissingFilter = [
      {
        rule_id: "rule-1",
        project_id: "proj-1",
        name: "Rule Without Filter",
        judge_model: "gpt-4",
        prompt_name: "judge-v1",
        prompt_version: 1,
        sample_rate: 1.0,
        enabled: true,
        created_at: "2026-05-13T10:00:00Z",
      },
    ];

    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(rulesWithMissingFilter));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Rule Without Filter")).toBeInTheDocument();
      expect(screen.getByText("gpt-4")).toBeInTheDocument();
    });
  });

  it.each([
    {
      sampleRate: null as unknown as number | null,
      judgeModel: "gpt-4",
      ruleName: "null sampleRate",
      expectNaN: false,
      expectEmDash: false,
    },
    {
      sampleRate: undefined as unknown as number | null,
      judgeModel: "gpt-4",
      ruleName: "undefined sampleRate",
      expectNaN: false,
      expectEmDash: false,
    },
    {
      sampleRate: 1.0,
      judgeModel: "",
      ruleName: "empty model",
      expectNaN: true,
      expectEmDash: true,
    },
    {
      sampleRate: null as unknown as number | null,
      judgeModel: "",
      ruleName: "both null values",
      expectNaN: false,
      expectEmDash: true,
    },
  ])(
    "handles $ruleName without showing NaN or blank model",
    async ({ sampleRate, judgeModel, ruleName, expectNaN, expectEmDash }) => {
      const rule: EvalRule = {
        rule_id: "rule-1",
        project_id: "proj-1",
        name: `Rule with ${ruleName}`,
        judge_model: judgeModel,
        prompt_name: "judge-v1",
        prompt_version: 1,
        sample_rate: sampleRate,
        enabled: true,
        created_at: "2026-05-13T10:00:00Z",
        filter: null,
      };

      vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules([rule]));

      renderWithToast(<EvalRulesPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText(`Rule with ${ruleName}`)).toBeInTheDocument();
      });

      if (expectNaN) {
        expect(screen.queryByText(/NaN/)).not.toBeInTheDocument();
      } else {
        expect(screen.queryByText(/NaN/)).not.toBeInTheDocument();
      }
      if (expectEmDash) {
        expect(screen.getByText(/—/)).toBeInTheDocument();
      }
    }
  );

  describe("filter summary display", () => {
    it("omits null cost and duration fields from filter summary", async () => {
      const ruleWithNullCost = [
        {
          rule_id: "rule-null-cost",
          project_id: "proj-1",
          name: "Rule With Null Cost Fields",
          judge_model: "gpt-4",
          prompt_name: "judge-v1",
          prompt_version: 1,
          sample_rate: 1.0,
          enabled: true,
          created_at: "2026-05-13T10:00:00Z",
          filter: {
            model: "gpt-4o",
            min_cost_usd: null,
            max_cost_usd: null,
            min_duration_ms: null,
            max_duration_ms: null,
          },
        },
      ];

      vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(ruleWithNullCost));

      renderWithToast(<EvalRulesPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("Rule With Null Cost Fields")).toBeInTheDocument();
      });

      expect(screen.queryByText(/\$null/)).not.toBeInTheDocument();
      expect(screen.getByText(/model=gpt-4o/)).toBeInTheDocument();
      expect(screen.queryByText(/min_cost/)).not.toBeInTheDocument();
      expect(screen.queryByText(/max_cost/)).not.toBeInTheDocument();
      expect(screen.queryByText(/min_dur/)).not.toBeInTheDocument();
      expect(screen.queryByText(/max_dur/)).not.toBeInTheDocument();
    });

    it("shows cost fields only when they have actual numeric values", async () => {
      const ruleWithCost = [
        {
          rule_id: "rule-with-cost",
          project_id: "proj-1",
          name: "Rule With Cost",
          judge_model: "gpt-4",
          prompt_name: "",
          prompt_version: 1,
          sample_rate: 1.0,
          enabled: true,
          created_at: "2026-05-13T10:00:00Z",
          filter: {
            min_cost_usd: 0.1,
            max_cost_usd: 5.0,
          },
        },
      ];

      vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(ruleWithCost));

      renderWithToast(<EvalRulesPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("Rule With Cost")).toBeInTheDocument();
      });

      expect(screen.getByText(/min_cost=\$0\.1/)).toBeInTheDocument();
      expect(screen.getByText(/max_cost=\$5/)).toBeInTheDocument();
    });
  });

  describe("wire format: API response uses snake_case field names", () => {
    const snakeCaseRules = [
      {
        rule_id: "rule-snake-1",
        project_id: "proj-1",
        name: "My Production Eval",
        judge_model: "gpt-4",
        prompt_name: "judge-v1",
        prompt_version: 1,
        sample_rate: 0.75,
        enabled: true,
        created_at: "2026-05-13T10:00:00Z",
        filter: { kind: "llm" },
      },
      {
        rule_id: "rule-snake-2",
        project_id: "proj-1",
        name: "Cost Monitor",
        judge_model: "claude-3",
        prompt_name: "",
        prompt_version: 1,
        sample_rate: 0.5,
        enabled: false,
        created_at: "2026-05-12T08:00:00Z",
        filter: null,
      },
    ];

    it("displays rule name from API response (snake_case wire format)", async () => {
      vi.spyOn(globalThis, "fetch").mockResolvedValue(
        { ok: true, json: () => Promise.resolve({ rules: snakeCaseRules }) } as Response
      );

      renderWithToast(<EvalRulesPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("My Production Eval")).toBeInTheDocument();
      });
      expect(screen.getByText("Cost Monitor")).toBeInTheDocument();
    });

    it("displays sample rate as percentage from API response (snake_case wire format)", async () => {
      vi.spyOn(globalThis, "fetch").mockResolvedValue(
        { ok: true, json: () => Promise.resolve({ rules: snakeCaseRules }) } as Response
      );

      renderWithToast(<EvalRulesPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("My Production Eval")).toBeInTheDocument();
      });
      // 0.75 → "75%", 0.5 → "50%"
      expect(screen.getByText("75%")).toBeInTheDocument();
      expect(screen.getByText("50%")).toBeInTheDocument();
    });

    it("displays judge model from API response (snake_case wire format)", async () => {
      vi.spyOn(globalThis, "fetch").mockResolvedValue(
        { ok: true, json: () => Promise.resolve({ rules: snakeCaseRules }) } as Response
      );

      renderWithToast(<EvalRulesPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getByText("My Production Eval")).toBeInTheDocument();
      });
      expect(screen.getByText("gpt-4")).toBeInTheDocument();
      expect(screen.getByText("claude-3")).toBeInTheDocument();
    });

    it("delete button targets the correct rule_id from API response", async () => {
      vi.spyOn(globalThis, "fetch").mockImplementation(
        (input: RequestInfo | URL) => {
          const url = typeof input === "string" ? input : input.toString();
          if (url.includes("/api/v1/eval-rules/rule-snake-1")) {
            return Promise.resolve({ ok: true, status: 204, text: async () => "" } as Response);
          }
          return Promise.resolve({ ok: true, json: () => Promise.resolve({ rules: snakeCaseRules }) } as Response);
        }
      );
      vi.spyOn(window, "confirm").mockReturnValue(true);

      renderWithToast(<EvalRulesPage activeProject="proj-1" />);

      await waitFor(() => {
        expect(screen.getAllByRole("button", { name: /delete/i })).toHaveLength(2);
      });

      const deleteButtons = screen.getAllByRole("button", { name: /delete/i });
      fireEvent.click(deleteButtons[0]);

      await waitFor(() => {
        expect(fetch).toHaveBeenCalledWith(
          expect.stringContaining("/api/v1/eval-rules/rule-snake-1"),
          expect.objectContaining({ method: "DELETE" })
        );
      });
    });
  });

  // ── FilterGroup integration: wired form state ──────────────────────

  function openNewRuleForm() {
    // There are two "New Rule" buttons: one in the header, one in the empty state.
    // We need the one in the header (which is inside a div with "Eval Rules").
    const allButtons = screen.getAllByRole("button", { name: /new rule/i });
    // The header button is the first one rendered; the empty-state button is shown
    // conditionally. We pick the one inside the top-level section header.
    if (allButtons.length >= 1) {
      return allButtons[0];
    }
    throw new Error("No New Rule button found");
  }

  it("renders the FilterGroup component in the new rule form", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules([]));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    // Wait for initial load, then open the form
    await waitFor(() => {
      expect(screen.getByText("Eval Rules")).toBeInTheDocument();
    });

    // Open the form (empty state provides the only button)
    fireEvent.click(openNewRuleForm());

    // FilterGroup renders a condition row with a condition type selector
    expect(screen.getByTestId("condition-row")).toBeInTheDocument();
    expect(screen.getByRole("combobox")).toBeInTheDocument();
  });

  it("renders the filter conditions section header", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules([]));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Eval Rules")).toBeInTheDocument();
    });

    fireEvent.click(openNewRuleForm());

    expect(screen.getByText("Filter Conditions")).toBeInTheDocument();
  });

  it("renders nested AND group when a second condition is added", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules([]));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Eval Rules")).toBeInTheDocument();
    });

    fireEvent.click(openNewRuleForm());

    // Set first condition: kind = llm
    const kindSelect = screen.getByRole("combobox");
    fireEvent.change(kindSelect, { target: { value: "kind" } });
    await waitFor(() => {
      expect(screen.getByText("Kind Value")).toBeInTheDocument();
    });
    const kindValueSelect = screen.getAllByRole("combobox")[1];
    fireEvent.change(kindValueSelect, { target: { value: "llm" } });

    // Add second condition – converts to AND group
    const addButton = screen.getByRole("button", { name: /add condition/i });
    fireEvent.click(addButton);

    // The form now has two condition rows (AND group at depth 0)
    expect(screen.getAllByTestId("condition-row")).toHaveLength(2);
    // The second row should be empty (fresh condition)
    const secondRowCombobox = screen.getAllByRole("combobox")[2];
    expect(secondRowCombobox).toHaveValue("");
  });

  it("sends grouped filter to preview endpoint when preview is clicked", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/eval-rules/preview")) {
          return Promise.resolve(
            ({
              ok: true,
              json: () => Promise.resolve({ spans: [], match_count_24h: 0 }),
            } as Response)
          );
        }
        if (url.includes("/api/v1/eval-rules")) {
          return Promise.resolve(resolveRules([]));
        }
        return Promise.resolve(
          ({
            ok: true,
            json: () => Promise.resolve({ spans: [], match_count_24h: 0 }),
          } as Response)
        );
      }
    );

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Eval Rules")).toBeInTheDocument();
    });

    fireEvent.click(openNewRuleForm());

    // Set model = gpt-4
    const typeSelect = screen.getByRole("combobox");
    fireEvent.change(typeSelect, { target: { value: "model" } });
    await waitFor(() => {
      expect(screen.getByPlaceholderText(/gpt-4-turbo/)).toBeInTheDocument();
    });
    const modelInput = screen.getByPlaceholderText(/gpt-4-turbo/);
    fireEvent.change(modelInput, { target: { value: "gpt-4" } });

    // Click preview
    const previewButton = screen.getByRole("button", { name: /preview matching spans/i });
    fireEvent.click(previewButton);

    await waitFor(() => {
      const calls = vi
        .mocked(fetch)
        .mock.calls.filter(
          (c) => typeof c[0] === "string" && c[0].includes("/api/v1/eval-rules/preview")
        );
      expect(calls.length).toBeGreaterThan(0);

      // Verify the filter body contains the model condition
      const lastCall = calls[calls.length - 1];
      const options = lastCall[1] as { body: string };
      const body = JSON.parse(options.body) as { filter: Record<string, unknown> };
      expect(body.filter.model).toBe("gpt-4");
    });
  });

  it("sends grouped AND filter to preview when multiple conditions exist", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/eval-rules/preview")) {
          return Promise.resolve(
            ({
              ok: true,
              json: () => Promise.resolve({ spans: [], match_count_24h: 0 }),
            } as Response)
          );
        }
        if (url.includes("/api/v1/eval-rules")) {
          return Promise.resolve(resolveRules([]));
        }
        return Promise.resolve(
          ({
            ok: true,
            json: () => Promise.resolve({ spans: [], match_count_24h: 0 }),
          } as Response)
        );
      }
    );

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Eval Rules")).toBeInTheDocument();
    });

    fireEvent.click(openNewRuleForm());

    // Wait for the form to open, then set first condition: kind = llm using Condition Type label selector
    await waitFor(() => {
      expect(screen.getByLabelText("Condition Type")).toBeInTheDocument();
    });
    const typeSelect = screen.getByLabelText("Condition Type");
    fireEvent.change(typeSelect, { target: { value: "kind" } });
    await waitFor(() => {
      expect(screen.getByLabelText("Kind Value")).toBeInTheDocument();
    });
    const kindValueSelect = screen.getByLabelText("Kind Value") as HTMLSelectElement;
    fireEvent.change(kindValueSelect, { target: { value: "llm" } });

    // Add second condition → converts top-level to AND group
    const addButton = screen.getByRole("button", { name: /add condition/i });
    fireEvent.click(addButton);

    // Verify the condition type selector exists for the new AND group
    // (there should now be 2 Condition Type selectors)
    await waitFor(() => {
      const typeSelectors = screen.getAllByLabelText("Condition Type");
      expect(typeSelectors.length).toBe(2);
    });

    // Set second condition: model = gpt-4 on the second condition row
    const modelTypeSelect = screen.getAllByLabelText("Condition Type")[1];
    fireEvent.change(modelTypeSelect as HTMLSelectElement, { target: { value: "model" } });
    await waitFor(() => {
      expect(screen.getByLabelText("Model")).toBeInTheDocument();
    });
    const modelInput = screen.getByLabelText("Model") as HTMLInputElement;
    fireEvent.change(modelInput, { target: { value: "gpt-4" } });

    // Click preview
    const previewButton = screen.getByRole("button", { name: /preview matching spans/i });
    fireEvent.click(previewButton);

    await waitFor(() => {
      const calls = vi
        .mocked(fetch)
        .mock.calls.filter(
          (c) => typeof c[0] === "string" && c[0].includes("/api/v1/eval-rules/preview")
        );
      expect(calls.length).toBeGreaterThan(0);
      const lastCall = calls[calls.length - 1];
      const options = lastCall[1] as { body: string };
      const body = JSON.parse(options.body) as { filter: { and?: unknown[] } };
      // Should contain an AND group with two conditions
      expect(body.filter.and).toBeDefined();
      expect(body.filter.and!.length).toBe(2);
    });
  });

  it("creates rule with grouped filter via POST /api/v1/eval-rules", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/eval-rules") && !url.includes("/api/v1/eval-rules/")) {
          // List endpoint
          return Promise.resolve(resolveRules([]));
        }
        if (url.includes("/api/v1/eval-rules/preview")) {
          return Promise.resolve(
            ({
              ok: true,
              json: () => Promise.resolve({ spans: [], match_count_24h: 0 }),
            } as Response)
          );
        }
        if (url.includes("/api/v1/eval-rules/") && url.includes("DELETE")) {
          return Promise.resolve({ ok: true, status: 204, text: async () => "" } as Response);
        }
        // Create endpoint
        return Promise.resolve(
          ({
            ok: true,
            status: 201,
            json: () => Promise.resolve({ rule_id: "new-rule-1" }),
          } as Response)
        );
      }
    );

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Eval Rules")).toBeInTheDocument();
    });

    fireEvent.click(openNewRuleForm());

    // Fill in required fields
    const nameInput = screen.getByPlaceholderText(/Production LLM Eval/i);
    fireEvent.change(nameInput, { target: { value: "Test Grouped Rule" } });

    // Set model via the Condition Type selector
    const typeSelect = screen.getByLabelText("Condition Type");
    fireEvent.change(typeSelect, { target: { value: "model" } });
    await waitFor(() => {
      expect(screen.getByLabelText("Model")).toBeInTheDocument();
    });
    const modelInput = screen.getByLabelText("Model") as HTMLInputElement;
    fireEvent.change(modelInput, { target: { value: "gpt-4" } });

    // Judge Prompt is now a registry picker (select); leaving it unselected
    // is valid since prompt_name is optional in validation.

    // Click Create Rule
    const createBtn = screen.getByRole("button", { name: /create rule/i });
    fireEvent.click(createBtn);

    await waitFor(() => {
      expect(screen.getByText(/Rule .* created/i)).toBeInTheDocument();
    });

    // Verify the POST body contained a filter with a model field
    const createCalls = vi
      .mocked(fetch)
      .mock.calls.filter(
        (c) =>
          typeof c[0] === "string" &&
          c[0] === "/api/v1/eval-rules" &&
          (c[1] as { method?: string })?.method === "POST"
      );
    expect(createCalls.length).toBeGreaterThan(0);
    const lastCall = createCalls[createCalls.length - 1];
    const options = lastCall[1] as { body: string };
    const body = JSON.parse(options.body) as { filter: { model?: string } };
    expect(body.filter.model).toBe("gpt-4");
  });

  it("sends nested OR filter to preview when OR group is used", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/eval-rules/preview")) {
          return Promise.resolve(
            ({
              ok: true,
              json: () => Promise.resolve({ spans: [], match_count_24h: 0 }),
            } as Response)
          );
        }
        if (url.includes("/api/v1/eval-rules")) {
          return Promise.resolve(resolveRules([]));
        }
        return Promise.resolve(
          ({
            ok: true,
            json: () => Promise.resolve({ spans: [], match_count_24h: 0 }),
          } as Response)
        );
      }
    );

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Eval Rules")).toBeInTheDocument();
    });

    fireEvent.click(openNewRuleForm());

    // Set first condition: model = gpt-4
    const typeSelect = screen.getByLabelText("Condition Type");
    fireEvent.change(typeSelect, { target: { value: "model" } });
    await waitFor(() => {
      expect(screen.getByLabelText("Model")).toBeInTheDocument();
    });
    const modelInput = screen.getByLabelText("Model") as HTMLInputElement;
    fireEvent.change(modelInput, { target: { value: "gpt-4" } });

    // Add a second condition → converts top-level to AND group with both conditions
    const addConditionBtn = screen.getByRole("button", { name: /add condition/i });
    fireEvent.click(addConditionBtn);

    // Set model = claude-3 on the second condition row
    const secondTypeSelect = screen.getAllByLabelText("Condition Type")[1];
    fireEvent.change(secondTypeSelect as HTMLSelectElement, { target: { value: "model" } });
    await waitFor(() => {
      expect(screen.getAllByLabelText("Model")).toHaveLength(2);
    });
    const secondModelInput = screen.getAllByLabelText("Model")[1] as HTMLInputElement;
    fireEvent.change(secondModelInput, { target: { value: "claude-3" } });

    // Now convert the second condition to an OR group by clicking "Add OR/AND Group"
    // This button appears on non-top-level leaves - pick the second one (second condition row)
    const addOrGroupBtns = screen.getAllByRole("button", { name: /add or\/and group/i });
    fireEvent.click(addOrGroupBtns[1]);

    // Wait for the OR header to appear (rendered at depth > 0)
    await waitFor(() => {
      expect(screen.getByText("OR")).toBeInTheDocument();
    });

    // Verify the second sub-filter now has an OR group
    const conditionRows = screen.getAllByTestId("condition-row");
    expect(conditionRows.length).toBeGreaterThan(0);

    // Click preview
    const previewButton = screen.getByRole("button", { name: /preview matching spans/i });
    fireEvent.click(previewButton);

    await waitFor(() => {
      const calls = vi
        .mocked(fetch)
        .mock.calls.filter(
          (c) => typeof c[0] === "string" && c[0].includes("/api/v1/eval-rules/preview")
        );
      expect(calls.length).toBeGreaterThan(0);
      const lastCall = calls[calls.length - 1];
      const options = lastCall[1] as { body: string };
      const body = JSON.parse(options.body) as { filter: Record<string, unknown> };
      expect(body.filter).toBeDefined();
      // The filter should contain an OR group with two model conditions
      const hasCondition =
        body.filter.model != null ||
        body.filter.kind != null ||
        body.filter.and != null ||
        body.filter.or != null ||
        body.filter.not != null;
      expect(hasCondition).toBe(true);
    });
  });

  describe("judge prompt picker", () => {
    const mockPrompts = [
      { name: "judge-v1", latest_version: 3, labels: { production: 3, staging: 2, dev: 1 } },
      { name: "cost-check", latest_version: 2, labels: { production: 2 } },
      { name: "quality-assessor", latest_version: 5, labels: { production: 5, staging: 3, dev: 1 } },
    ];

    const resolveRules = (rules: any[]) =>
      ({ ok: true, json: () => Promise.resolve({ rules }) } as Response);

    const resolvePrompts = (prompts: typeof mockPrompts) =>
      ({ ok: true, json: () => Promise.resolve(prompts) } as Response);

    const renderWithMockedPrompts = () => {
      vi.spyOn(globalThis, "fetch").mockImplementation(
        (input: RequestInfo | URL) => {
          const url = typeof input === "string" ? input : input.toString();
          if (url.includes("/api/v1/eval-rules") && !url.includes("/api/v1/eval-rules/")) {
            return Promise.resolve(resolveRules([]));
          }
          if (url.includes("/api/v1/prompts")) {
            return Promise.resolve(resolvePrompts(mockPrompts));
          }
          return Promise.resolve(resolveRules([]));
        }
      );
      renderWithToast(<EvalRulesPage activeProject="proj-1" />);
    };

    const openNewRuleForm = async () => {
      await waitFor(() => {
        expect(screen.getByText(/no eval rules yet/i)).toBeInTheDocument();
      });
      const buttons = screen.getAllByRole("button", { name: /new rule/i });
      fireEvent.click(buttons[0]);
    };

    beforeEach(() => {
      vi.clearAllMocks();
    });

    it("fetches prompts from the registry when creating a new rule", async () => {
      renderWithMockedPrompts();
      await openNewRuleForm();

      await waitFor(() => {
        expect(fetch).toHaveBeenCalledWith(
          expect.stringContaining("/api/v1/prompts?project_id=proj-1")
        );
      });
    });

    it("renders the prompt picker as a select element with registry options", async () => {
      renderWithMockedPrompts();
      await openNewRuleForm();

      // Wait for prompts to load and verify picker options are visible
      await waitFor(() => {
        expect(screen.getByText(/judge-v1 \(v3\)/)).toBeInTheDocument();
      });

      // All prompt options should be visible in the select
      expect(screen.getByText(/judge-v1 \(v3\)/)).toBeInTheDocument();
      expect(screen.getByText(/cost-check \(v2\)/)).toBeInTheDocument();
      expect(screen.getByText(/quality-assessor \(v5\)/)).toBeInTheDocument();
    });

    it("shows a prompt label selector when a prompt is selected", async () => {
      renderWithMockedPrompts();
      await openNewRuleForm();

      await waitFor(() => {
        expect(screen.getByText(/judge-v1 \(v3\)/)).toBeInTheDocument();
      });

      // Find the judge prompt select by its label text
      const judgePromptLabel = screen.getByText("Judge Prompt");
      const promptSelect = judgePromptLabel.closest("div")?.querySelector("select") as HTMLSelectElement;
      fireEvent.change(promptSelect, { target: { value: "judge-v1" } });

      await waitFor(() => {
        expect(screen.getByText("Prompt Label")).toBeInTheDocument();
      });
    });

    it("updates version dropdown based on selected prompt's latest version", async () => {
      renderWithMockedPrompts();
      await openNewRuleForm();

      await waitFor(() => {
        expect(screen.getByText(/quality-assessor \(v5\)/)).toBeInTheDocument();
      });

      // Find the judge prompt select by its label text
      const judgePromptLabel = screen.getByText("Judge Prompt");
      const promptSelect = judgePromptLabel.closest("div")?.querySelector("select") as HTMLSelectElement;
      fireEvent.change(promptSelect, { target: { value: "quality-assessor" } });

      // Find the version dropdown by its label
      const versionLabel = screen.getByText("Prompt Version");
      const versionDiv = versionLabel.closest("div");
      const versionSelect = versionDiv?.querySelector("select") as HTMLSelectElement;
      // The latest version should be among the options
      const options = Array.from(versionSelect?.options || []);
      const hasV5 = options.some((opt) => opt.value === "5");
      expect(hasV5).toBe(true);
    });

    it("prevents creating a rule when no prompt is selected (validation)", async () => {
      renderWithMockedPrompts();
      await openNewRuleForm();

      // Wait for prompts to load (so validation can check against registry)
      await waitFor(() => {
        expect(screen.getByText(/judge-v1 \(v3\)/)).toBeInTheDocument();
      });

      // Try to create without selecting a prompt (promptName is still empty string)
      const createButton = screen.getByRole("button", { name: /create rule/i });
      fireEvent.click(createButton);

      expect(fetch).not.toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({
          method: "POST",
        })
      );
    });

    it("allows creating a rule with a selected prompt from the picker", async () => {
      let postCalled = false;

      vi.spyOn(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/api/v1/eval-rules") && !url.includes("/api/v1/eval-rules/")) {
          if (init?.method === "POST") {
            postCalled = true;
            return Promise.resolve({ ok: true, json: () => Promise.resolve({ rule_id: "new-rule" }) } as Response);
          }
          return Promise.resolve(resolveRules([]));
        }
        if (url.includes("/api/v1/prompts")) {
          return Promise.resolve(resolvePrompts(mockPrompts));
        }
        return Promise.resolve(resolveRules([]));
      });

      renderWithToast(<EvalRulesPage activeProject="proj-1" />);
      await openNewRuleForm();

      // Wait for prompts to be loaded and visible in the picker
      await waitFor(() => {
        expect(screen.getByText(/judge-v1 \(v3\)/)).toBeInTheDocument();
      });

      // Fill in rule name (required by validation) via placeholder text
      const ruleNameInput = screen.getByPlaceholderText(/Production LLM Eval/);
      fireEvent.change(ruleNameInput, { target: { value: "Test Rule" } });

      // Find the judge prompt select by its label text
      const judgePromptLabel = screen.getByText("Judge Prompt");
      const promptSelect = judgePromptLabel.closest("div")?.querySelector("select") as HTMLSelectElement;

      // Select a prompt and wait for state to flush
      fireEvent.change(promptSelect, { target: { value: "judge-v1" } });
      await new Promise((r) => setTimeout(r, 0));
      await waitFor(() => {
        expect(promptSelect.value).toBe("judge-v1");
      });

      const createButton = screen.getByRole("button", { name: /create rule/i });
      fireEvent.click(createButton);

      await waitFor(() => {
        expect(postCalled).toBe(true);
      });
    });
  });
});
