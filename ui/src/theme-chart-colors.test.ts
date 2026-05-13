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

  it("emberFlare is the first chart series color", () => {
    expect(colors.chartColors.series[0]).toBe("#FF5722");
  });

  it("chartColors includes a secondary warm accent", () => {
    // Should contain softGlow as a warm accent
    expect(colors.chartColors.series).toContain(colors.accents.softGlow);
  });

  it("chartColors includes a distinct blue/teal accent", () => {
    // Should have a non-orange color for contrast
    const nonOrange = colors.chartColors.series.filter(
      (c) => !c.match(/^#FF.*$/)
    );
    expect(nonOrange.length).toBeGreaterThanOrEqual(1);
  });

  it("chartColors includes an opacity helper for bars", () => {
    expect(colors.chartColors.barOpacity).toBeDefined();
    expect(typeof colors.chartColors.barOpacity).toBe("function");
  });
});
