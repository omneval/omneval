import { describe, it, expect } from "vitest";

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
