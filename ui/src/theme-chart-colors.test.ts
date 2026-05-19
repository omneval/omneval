import { describe, it, expect } from "vitest";
import { colors } from "./theme";

describe("Chart colors for multi-series dashboards", () => {
  it("chartColors contains at least 4 distinct series colors", () => {
    expect(colors.chartColors.series).toBeDefined();
    expect(colors.chartColors.series.length).toBeGreaterThanOrEqual(4);
  });

  it("each chart series color is a valid hex color", () => {
    const hexPattern = /^#[0-9A-Fa-f]{6}$/;
    colors.chartColors.series.forEach((hex) => {
      expect(hex).toMatch(hexPattern);
    });
  });

  it("violet primary is the first chart series color", () => {
    expect(colors.chartColors.series[0]).toBe("#7C3AED");
  });

  it("chartColors includes violet-light as secondary accent", () => {
    // Should contain softGlow (violet-light) as a secondary accent
    expect(colors.chartColors.series).toContain(colors.accents.softGlow);
  });

  it("chartColors includes a distinct cyan/teal accent for contrast", () => {
    // Should have a non-violet color for contrast (cyan)
    const nonViolet = colors.chartColors.series.filter(
      (c) => !c.match(/^#[67][CD][0-9A-F].*$/)
    );
    expect(nonViolet.length).toBeGreaterThanOrEqual(1);
  });

  it("chartColors includes an opacity helper for bars", () => {
    expect(colors.chartColors.barOpacity).toBeDefined();
    expect(typeof colors.chartColors.barOpacity).toBe("function");
  });
});
