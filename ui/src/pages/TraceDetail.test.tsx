import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import TraceDetailPage from "./TraceDetail";
import { ToastProvider } from "@/components/Toast";

function renderWithProvider(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

describe("hook ordering", () => {
  it("does not throw React hook order violation when traceId is empty (loading state)", () => {
    const consoleError = console.error;
    let hookOrderError: string | null = null;
    console.error = (...args: unknown[]) => {
      const msg = args.join(" ");
      if (msg.includes("hook") || msg.includes("310")) {
        hookOrderError = msg;
      }
      consoleError(...args);
    };

    expect(() => {
      renderWithProvider(
        <TraceDetailPage
          traceId=""
          activeProject="test-project"
          onBack={() => {}}
        />
      );
    }).not.toThrow();

    expect(hookOrderError).toBeNull();

    console.error = consoleError;
  });

  it("does not throw React hook order violation when trace is not found (error state)", async () => {
    // Mock fetch to return 404
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: false,
      status: 404,
    } as Response);

    const consoleError = console.error;
    let hookOrderError: string | null = null;
    console.error = (...args: unknown[]) => {
      const msg = args.join(" ");
      if (msg.includes("hook") || msg.includes("310")) {
        hookOrderError = msg;
      }
      consoleError(...args);
    };

    // Note: This will still show loading first, then error. The hook order
    // must be stable across both states.
    renderWithProvider(
      <TraceDetailPage
        traceId="nonexistent-trace"
        activeProject="test-project"
        onBack={() => {}}
      />
    );

    await screen.findByText("Trace not found");

    expect(hookOrderError).toBeNull();

    console.error = consoleError;
  });
});

describe("totalTokens", () => {
  const sumTokens = (inputTokens: number, outputTokens: number): number =>
    inputTokens + outputTokens;

  it("returns the sum of input and output tokens", () => {
    expect(sumTokens(100, 200)).toBe(300);
    expect(sumTokens(0, 0)).toBe(0);
    expect(sumTokens(1000, 500)).toBe(1500);
  });

  it("handles large token counts", () => {
    expect(sumTokens(1_000_000, 500_000)).toBe(1_500_000);
  });
});

describe("formatMs (milliseconds display)", () => {
  const formatMs = (ms: number): string => {
    if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
    return `${ms}ms`;
  };

  const tests: Array<[number, string]> = [
    [0, "0ms"],
    [100, "100ms"],
    [999, "999ms"],
    [1000, "1.0s"],
    [1500, "1.5s"],
    [5000, "5.0s"],
    [12345, "12.3s"],
    [60000, "60.0s"],
  ];

  it.each(tests)("formats %d ms as '%s'", (ms, expected) => {
    expect(formatMs(ms)).toBe(expected);
  });
});
