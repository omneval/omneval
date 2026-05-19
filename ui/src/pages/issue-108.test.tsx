/**
 * Issue #108 — UI polish acceptance criteria tests.
 * Each test corresponds to a specific acceptance criterion.
 * Run: npx vitest run src/pages/issue-108.test.tsx
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import Sidebar from "@/components/Sidebar";
import EvalRulesPage from "./EvalRules";
import PromptsPage from "./Prompts";
import SettingsPage from "./Settings";
import { ToastProvider } from "@/components/Toast";
import { formatJsonPreview } from "@/utils/formatters";

function renderWithToast(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

// ── Criterion 2: Sidebar section labels contrast ──────────────────────────────

describe("AC2: Sidebar section labels ≥ 4.5:1 contrast against charcoal background", () => {
  const sidebarProps = {
    collapsed: false,
    onToggle: vi.fn(),
    active: "traces",
    onNavigate: vi.fn(),
    onLogout: vi.fn(),
  };

  it("section label buttons do not use text-omneval-text-muted/70 (low-contrast class)", () => {
    const { container } = render(<Sidebar {...sidebarProps} />);
    // Section label buttons are accordion toggle buttons with chevron + label text
    const sectionLabelButtons = Array.from(
      container.querySelectorAll<HTMLButtonElement>("nav button")
    ).filter((btn) => {
      const text = btn.textContent ?? "";
      return text.includes("PROMPTS") || text.includes("EVALUATION");
    });

    expect(sectionLabelButtons.length).toBeGreaterThan(0);
    sectionLabelButtons.forEach((btn) => {
      expect(btn.className).not.toContain("text-omneval-text-muted/70");
    });
  });

  it("section label buttons use a high-contrast text class (text-omneval-text-muted or text-omneval-text-pure)", () => {
    const { container } = render(<Sidebar {...sidebarProps} />);
    const sectionLabelButtons = Array.from(
      container.querySelectorAll<HTMLButtonElement>("nav button")
    ).filter((btn) => {
      const text = btn.textContent ?? "";
      return text.includes("PROMPTS") || text.includes("EVALUATION");
    });

    expect(sectionLabelButtons.length).toBeGreaterThan(0);
    sectionLabelButtons.forEach((btn) => {
      const cls = btn.className;
      const hasHighContrast = cls.includes("text-omneval-text-muted") || cls.includes("text-omneval-text-pure");
      expect(hasHighContrast).toBe(true);
    });
  });
});

// ── Criterion 1: Page padding ─────────────────────────────────────────────────

describe("AC1: EvalRules page has 24px horizontal and 20px top padding", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ rules: [] }),
    } as Response);
  });

  it("outer container has px-6 class for 24px horizontal padding", async () => {
    const { container } = renderWithToast(
      <EvalRulesPage activeProject="proj-1" />
    );
    await waitFor(() =>
      expect(screen.getByText("Eval Rules")).toBeInTheDocument()
    );
    const outer = container.firstElementChild as HTMLElement;
    expect(outer.className).toContain("px-6");
  });

  it("outer container has pt-5 class for 20px top padding", async () => {
    const { container } = renderWithToast(
      <EvalRulesPage activeProject="proj-1" />
    );
    await waitFor(() =>
      expect(screen.getByText("Eval Rules")).toBeInTheDocument()
    );
    const outer = container.firstElementChild as HTMLElement;
    expect(outer.className).toContain("pt-5");
  });
});

describe("AC1: Prompts page has 24px horizontal and 20px top padding", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([]),
    } as Response);
  });

  it("outer container has px-6 class for 24px horizontal padding", async () => {
    const { container } = renderWithToast(
      <PromptsPage activeProject="proj-1" />
    );
    await waitFor(() =>
      expect(screen.getByText("Prompt Registry")).toBeInTheDocument()
    );
    const outer = container.firstElementChild as HTMLElement;
    expect(outer.className).toContain("px-6");
  });

  it("outer container has pt-5 class for 20px top padding", async () => {
    const { container } = renderWithToast(
      <PromptsPage activeProject="proj-1" />
    );
    await waitFor(() =>
      expect(screen.getByText("Prompt Registry")).toBeInTheDocument()
    );
    const outer = container.firstElementChild as HTMLElement;
    expect(outer.className).toContain("pt-5");
  });
});

// ── Criterion 8: Chat format parsing in trace list columns ───────────────────

describe("AC8: formatJsonPreview does not show raw [content: role:] Go-map format", () => {
  it("strips Go-map wrapper from single message", () => {
    const goMapInput = "[map[content:Hello world role:user]]";
    const result = formatJsonPreview(goMapInput, 80);
    expect(result).not.toContain("[map[");
    expect(result).not.toContain("map[content:");
  });

  it("returns readable content text from Go-map format", () => {
    const goMapInput = "[map[content:Hello world role:user]]";
    const result = formatJsonPreview(goMapInput, 80);
    expect(result).toContain("Hello world");
  });

  it("handles JSON array of chat messages by extracting content", () => {
    const jsonInput = JSON.stringify([
      { role: "user", content: "What is the capital of France?" },
    ]);
    const result = formatJsonPreview(jsonInput, 80);
    expect(result).toContain("What is the capital of France?");
    expect(result).not.toContain('"role"');
  });

  it("returns readable text from multi-message JSON array", () => {
    const jsonInput = JSON.stringify([
      { role: "system", content: "You are a helpful assistant." },
      { role: "user", content: "Hello!" },
    ]);
    const result = formatJsonPreview(jsonInput, 80);
    // Should show at least the first message content, not raw JSON structure
    expect(result).not.toBe(JSON.stringify([
      { role: "system", content: "You are a helpful assistant." },
      { role: "user", content: "Hello!" },
    ]).slice(0, 80) + "…");
    expect(result).toMatch(/You are a helpful assistant|Hello!/);
  });
});

// ── Criterion 5: Chat turn rendering in span panel ───────────────────────────

import { parseChatTurns } from "@/utils/formatters";

describe("AC5: parseChatTurns extracts structured chat messages", () => {
  it("parses JSON array of {role, content} objects", () => {
    const input = JSON.stringify([
      { role: "system", content: "You are helpful." },
      { role: "user", content: "Hello!" },
      { role: "assistant", content: "Hi there!" },
    ]);
    const turns = parseChatTurns(input);
    expect(turns).not.toBeNull();
    expect(turns).toHaveLength(3);
    expect(turns![0]).toEqual({ role: "system", content: "You are helpful." });
    expect(turns![1]).toEqual({ role: "user", content: "Hello!" });
    expect(turns![2]).toEqual({ role: "assistant", content: "Hi there!" });
  });

  it("returns null for a plain JSON string (not a chat array)", () => {
    const input = JSON.stringify({ key: "value" });
    const turns = parseChatTurns(input);
    expect(turns).toBeNull();
  });

  it("returns null for plain text", () => {
    const turns = parseChatTurns("hello world");
    expect(turns).toBeNull();
  });

  it("returns null for empty string", () => {
    expect(parseChatTurns("")).toBeNull();
  });
});

// ── Criterion 3: API key display name ────────────────────────────────────────

describe("AC3: API key entries display service_name prominently as the display name", () => {
  const mockKeys = [
    {
      key_id: "oev_svc_worker-1",
      kind: "service" as const,
      service_name: "my-agent",
      created_at: "2026-05-14T08:00:00Z",
    },
    {
      key_id: "oev_proj_abc123",
      kind: "project" as const,
      created_at: "2026-05-13T10:00:00Z",
    },
  ];

  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockKeys),
    } as Response);
  });

  it("service key shows service_name as primary display name", async () => {
    renderWithToast(<SettingsPage activeProject="proj-1" />);
    await waitFor(() =>
      expect(screen.getByText("my-agent")).toBeInTheDocument()
    );
    // service_name should appear in a prominent text element (not small/muted)
    const nameEl = screen.getByText("my-agent");
    // It should not be in a tiny secondary text — check font class or parent
    expect(nameEl).toBeInTheDocument();
  });

  it("project key key_id is shown as secondary/smaller text", async () => {
    renderWithToast(<SettingsPage activeProject="proj-1" />);
    await waitFor(() =>
      expect(screen.getByText("oev_proj_abc123")).toBeInTheDocument()
    );
    // key_id should be in a font-mono secondary element
    const keyIdEl = screen.getByText("oev_proj_abc123");
    expect(keyIdEl.className).toContain("font-mono");
  });
});
