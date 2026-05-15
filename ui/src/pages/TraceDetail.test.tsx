import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import TraceDetailPage from "./TraceDetail";
import { ToastProvider } from "@/components/Toast";
import { totalTokens } from "./TraceDetail";
import { formatMs } from "@/utils/formatters";

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
  const testCases: Array<{ inputTokens: number; outputTokens: number; expected: number }> = [
    { inputTokens: 100, outputTokens: 200, expected: 300 },
    { inputTokens: 0, outputTokens: 0, expected: 0 },
    { inputTokens: 1000, outputTokens: 500, expected: 1500 },
    { inputTokens: 1_000_000, outputTokens: 500_000, expected: 1_500_000 },
  ];

  it.each(testCases)(
    "returns $expected for inputTokens=$inputTokens, outputTokens=$outputTokens",
    ({ inputTokens, outputTokens, expected }) => {
      expect(totalTokens({
        span_id: "x",
        trace_id: "x",
        parent_id: "",
        project_id: "",
        name: "",
        kind: "",
        start_time: "",
        end_time: "",
        input_tokens: inputTokens,
        output_tokens: outputTokens,
        cost_usd: 0,
      })).toBe(expected);
    },
  );
});

describe("formatMs (milliseconds display)", () => {
  const testCases: Array<[number, string]> = [
    [0, "0ms"],
    [100, "100ms"],
    [999, "999ms"],
    [1000, "1.0s"],
    [1500, "1.5s"],
    [5000, "5.0s"],
    [12345, "12.3s"],
    [60000, "60.0s"],
  ];

  it.each(testCases)("formats %d ms as '%s'", (ms, expected) => {
    expect(formatMs(ms)).toBe(expected);
  });
});
