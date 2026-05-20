import { describe, it, expect } from "vitest";
import { colors } from "./theme";

/**
 * Issue #102: Dark theme visual polish — contrast, spacing, typography
 */

describe("Issue #102 — Dark Theme Polish", () => {
  describe("WCAG AA contrast ratios", () => {
    /**
     * WCAG AA requires ≥ 4.5:1 for normal text (< 18px) and ≥ 3:1 for large text.
     * We target ≥ 4.5:1 for all body text and labels.
     */
    function luminance(hex: string): number {
      const cleaned = hex.replace("#", "");
      const r = parseInt(cleaned.slice(0, 2), 16) / 255;
      const g = parseInt(cleaned.slice(2, 4), 16) / 255;
      const b = parseInt(cleaned.slice(4, 6), 16) / 255;
      const toLinear = (c: number) => c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
      return 0.2126 * toLinear(r) + 0.7152 * toLinear(g) + 0.0722 * toLinear(b);
    }

    function contrastRatio(c1: string, c2: string): number {
      const l1 = luminance(c1);
      const l2 = luminance(c2);
      const lighter = Math.max(l1, l2);
      const darker = Math.min(l1, l2);
      return (lighter + 0.05) / (darker + 0.05);
    }

    const backgrounds = [
      colors.backgrounds.abyssBlack,
      colors.backgrounds.charcoalDepth,
      colors.backgrounds.slightIllumination,
      colors.backgrounds.caveWall,
    ];

    it("typography.pureLight passes WCAG AA on all dark backgrounds", () => {
      backgrounds.forEach((bg) => {
        const ratio = contrastRatio(colors.typography.pureLight, bg);
        expect(ratio).toBeGreaterThanOrEqual(4.5);
      });
    });

    it("mid-tone text color passes WCAG AA on all dark backgrounds", () => {
      const midText = colors.typography.midTone;
      backgrounds.forEach((bg) => {
        const ratio = contrastRatio(midText, bg);
        expect(ratio).toBeGreaterThanOrEqual(4.5);
      });
    });

    it("danger text color passes WCAG AA on dark backgrounds", () => {
      const dangerText = colors.accents.dangerLight;
      backgrounds.forEach((bg) => {
        const ratio = contrastRatio(dangerText, bg);
        expect(ratio).toBeGreaterThanOrEqual(4.5);
      });
    });
  });

  describe("color palette completeness", () => {
    it("includes midTone text color for contrast-safe secondary text", () => {
      expect(colors.typography).toHaveProperty("midTone");
      expect(typeof colors.typography.midTone).toBe("string");
      expect(colors.typography.midTone).toMatch(/^#[0-9A-Fa-f]{6}$/);
    });

    it("includes emberGlow accent for active/selected states", () => {
      expect(colors.accents).toHaveProperty("emberGlow");
      expect(typeof colors.accents.emberGlow).toBe("string");
      expect(colors.accents.emberGlow).toMatch(/^#[0-9A-Fa-f]{6}$/);
    });

    it("includes greenSuccess accent for success states", () => {
      expect(colors.accents).toHaveProperty("greenSuccess");
      expect(typeof colors.accents.greenSuccess).toBe("string");
      expect(colors.accents.greenSuccess).toMatch(/^#[0-9A-Fa-f]{6}$/);
    });

    it("includes amberWarning accent for warning states", () => {
      expect(colors.accents).toHaveProperty("amberWarning");
      expect(typeof colors.accents.amberWarning).toBe("string");
      expect(colors.accents.amberWarning).toMatch(/^#[0-9A-Fa-f]{6}$/);
    });
  });

  describe("CSS custom properties", () => {
    it("includes new CSS variables for semantic colors", () => {
      const vars = colors.cssVariables;
      expect(vars).toHaveProperty("--omneval-text-mid");
      expect(vars).toHaveProperty("--omneval-success");
      expect(vars).toHaveProperty("--omneval-warning");
    });

    it("includes new CSS variables for card elevation", () => {
      const vars = colors.cssVariables;
      expect(vars).toHaveProperty("--omneval-card-shadow");
      expect(vars).toHaveProperty("--omneval-card-hover-shadow");
    });
  });
});
