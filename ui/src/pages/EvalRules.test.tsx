import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import EvalRulesPage from "./EvalRules";
import { ToastProvider } from "@/components/Toast";

function renderWithToast(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

describe("EvalRulesPage", () => {
  const mockRules = [
    {
      RuleID: "rule-1",
      ProjectID: "proj-1",
      Name: "Production LLM Eval",
      JudgeModel: "gpt-4",
      PromptName: "judge-v1",
      PromptVersion: 1,
      SampleRate: 1.0,
      Enabled: true,
      CreatedAt: "2026-05-13T10:00:00Z",
      Filter: {
        kind: "llm",
        model: "gpt-4-turbo",
        service_name: "my-service",
      },
    },
    {
      RuleID: "rule-2",
      ProjectID: "proj-1",
      Name: "Cost Monitoring",
      JudgeModel: "claude-3",
      PromptName: "cost-check",
      PromptVersion: 2,
      SampleRate: 0.5,
      Enabled: false,
      CreatedAt: "2026-05-12T08:00:00Z",
      Filter: {
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
        RuleID: "rule-1",
        ProjectID: "proj-1",
        Name: "Rule With Null Filter",
        JudgeModel: "gpt-4",
        PromptName: "judge-v1",
        PromptVersion: 1,
        SampleRate: 1.0,
        Enabled: true,
        CreatedAt: "2026-05-13T10:00:00Z",
        Filter: null,
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
        RuleID: "rule-1",
        ProjectID: "proj-1",
        Name: "Rule Without Filter",
        JudgeModel: "gpt-4",
        PromptName: "judge-v1",
        PromptVersion: 1,
        SampleRate: 1.0,
        Enabled: true,
        CreatedAt: "2026-05-13T10:00:00Z",
      },
    ];

    vi.spyOn(globalThis, "fetch").mockResolvedValue(resolveRules(rulesWithMissingFilter));

    renderWithToast(<EvalRulesPage activeProject="proj-1" />);

    await waitFor(() => {
      expect(screen.getByText("Rule Without Filter")).toBeInTheDocument();
      expect(screen.getByText("gpt-4")).toBeInTheDocument();
    });
  });
});
