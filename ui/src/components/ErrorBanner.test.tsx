import { describe, it, expect } from "vitest";
import { colors } from "@/theme";

describe("ErrorBanner color references", () => {
  it("error color matches theme dangerRed", () => {
    expect(colors.accents.dangerRed).toBe("#EF4444");
  });

  it("error light text matches theme dangerLight", () => {
    expect(colors.accents.dangerLight).toBe("#FCA5A5");
  });

  it("error background uses dangerRed at 10% opacity", () => {
    const expected = "rgba(239, 68, 68, 0.1)";
    expect(colors.toRgba("#EF4444", 0.1)).toBe(expected);
  });

  it("error border uses dangerRed at 25% opacity", () => {
    const expected = "rgba(239, 68, 68, 0.25)";
    expect(colors.toRgba("#EF4444", 0.25)).toBe(expected);
  });
});
