import { describe, it, expect } from "vitest";
import { computeWaterfallAxisDomain, type WaterfallEntry } from "./TraceDetail";
import { totalTokens, formatMs } from "@/utils/formatters";

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
        input_tokens: inputTokens,
        output_tokens: outputTokens,
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
    [60000, "1m"],
    [81000, "1m 21s"],
    [3720000, "1h 2m"],
  ];

  it.each(testCases)("formats %d ms as '%s'", (ms, expected) => {
    expect(formatMs(ms)).toBe(expected);
  });
});

describe("waterfall axis domain (#280)", () => {
  it("returns [0, maxStartPlusDuration] for a single 26.3s span (the live-bug scenario)", () => {
    // A real Trace where the single Span ran 26.3 seconds should render
    // an axis that goes 0ms–26.3s, NOT 0ms–4ms (the default Recharts
    // behaviour when domain is not set and start values cluster near 0).
    const entries: WaterfallEntry[] = [
      {
        name: "agent.run",
        spanId: "single-span",
        start: 0,
        duration: 26_300, // 26.3 seconds
        color: "#ff6b35",
        kind: "agent",
      },
    ];

    const domain = computeWaterfallAxisDomain(entries);
    expect(domain).toEqual([0, 26_300]);
  });

  it("handles an empty span list by returning [0, 0]", () => {
    const domain = computeWaterfallAxisDomain([]);
    expect(domain).toEqual([0, 0]);
  });

  it("computes the domain from the latest-span end when children extend beyond the root", () => {
    const entries: WaterfallEntry[] = [
      { name: "root", spanId: "r1", start: 0, duration: 5_000, color: "#aaa", kind: "chain" },
      { name: "fast child", spanId: "c1", start: 1_000, duration: 2_000, color: "#bbb", kind: "tool" },
      { name: "slow child", spanId: "c2", start: 4_500, duration: 3_000, color: "#ccc", kind: "llm" },
      // fast child ends at 3000, slow child ends at 7500 → domain = [0, 7500]
    ];

    const domain = computeWaterfallAxisDomain(entries);
    expect(domain).toEqual([0, 7_500]);
  });

  it("computes the correct domain for a long-running child after the root ended", () => {
    // Root span: 0-5s.  Child starts at 4s and runs for 20s (ends at 24s).
    // The axis must go to 24000, not just 5000.
    const entries: WaterfallEntry[] = [
      { name: "root", spanId: "r1", start: 0, duration: 5_000, color: "#aaa", kind: "chain" },
      { name: "network-io", spanId: "c1", start: 4_000, duration: 20_000, color: "#bbb", kind: "tool" },
    ];

    const domain = computeWaterfallAxisDomain(entries);
    expect(domain).toEqual([0, 24_000]);
  });
});
