import { describe, it, expect } from "vitest";
import {
  colors,
  type ColorName,
  type ColorCategory,
} from "./theme";

describe("Branding Theme", () => {
  describe("color palette completeness", () => {
    const expectedColors: { name: ColorName; category: ColorCategory }[] = [
      { name: "abyssBlack", category: "backgrounds" },
      { name: "charcoalDepth", category: "backgrounds" },
      { name: "slightIllumination", category: "backgrounds" },
      { name: "caveWall", category: "backgrounds" },
      { name: "emberFlare", category: "accents" },
      { name: "softGlow", category: "accents" },
      { name: "flicker", category: "accents" },
      { name: "deepHeat", category: "accents" },
      { name: "dangerRed", category: "accents" },
      { name: "dangerLight", category: "accents" },
      { name: "pureLight", category: "typography" },
      { name: "ashGrey", category: "typography" },
    ];

    it.each(expectedColors)(
      "includes $name in $category",
      ({ name, category }) => {
        expect(colors[category as ColorCategory]).toHaveProperty(name);
      }
    );

    it("has all expected background colors", () => {
      const bg = colors.backgrounds;
      expect(bg).toHaveProperty("abyssBlack");
      expect(bg).toHaveProperty("charcoalDepth");
      expect(bg).toHaveProperty("slightIllumination");
      expect(bg).toHaveProperty("caveWall");
    });

    it("has all expected accent colors", () => {
      const accent = colors.accents;
      expect(accent).toHaveProperty("emberFlare");
      expect(accent).toHaveProperty("softGlow");
      expect(accent).toHaveProperty("flicker");
      expect(accent).toHaveProperty("deepHeat");
      expect(accent).toHaveProperty("dangerRed");
      expect(accent).toHaveProperty("dangerLight");
    });

    it("has all expected typography colors", () => {
      const type = colors.typography;
      expect(type).toHaveProperty("pureLight");
      expect(type).toHaveProperty("ashGrey");
    });
  });

  describe("hex color format", () => {
    const hexPattern = /^#[0-9A-Fa-f]{6}$/;

    it.each([colors.backgrounds, colors.accents, colors.typography])(
      "all colors in a category are valid hex",
      (palette) => {
        Object.values(palette).forEach((hex) => {
          expect(hex).toMatch(hexPattern);
        });
      }
    );
  });

  describe("specific color values", () => {
    it("abyssBlack maps to void black", () => {
      expect(colors.backgrounds.abyssBlack).toBe("#0A0A0F");
    });

    it("charcoalDepth maps to depth surface", () => {
      expect(colors.backgrounds.charcoalDepth).toBe("#111118");
    });

    it("slightIllumination maps to elevated surface", () => {
      expect(colors.backgrounds.slightIllumination).toBe("#1C1C28");
    });

    it("caveWall maps to border color", () => {
      expect(colors.backgrounds.caveWall).toBe("#2A2A3A");
    });

    it("emberFlare maps to violet primary", () => {
      expect(colors.accents.emberFlare).toBe("#7C3AED");
    });

    it("softGlow maps to violet light", () => {
      expect(colors.accents.softGlow).toBe("#8B5CF6");
    });

    it("flicker maps to violet pale", () => {
      expect(colors.accents.flicker).toBe("#A78BFA");
    });

    it("deepHeat maps to violet dark", () => {
      expect(colors.accents.deepHeat).toBe("#6D28D9");
    });

    it("dangerRed is #EF4444", () => {
      expect(colors.accents.dangerRed).toBe("#EF4444");
    });

    it("dangerLight is #FCA5A5", () => {
      expect(colors.accents.dangerLight).toBe("#FCA5A5");
    });

    it("pureLight is #FFFFFF", () => {
      expect(colors.typography.pureLight).toBe("#FFFFFF");
    });

    it("ashGrey maps to muted text", () => {
      expect(colors.typography.ashGrey).toBe("#8B8BA7");
    });
  });

  describe("derived utilities", () => {
    it("flickerRgba produces correct rgba for 10% opacity (violet-pale)", () => {
      const result = colors.flickerRgba(0.1);
      expect(result).toBe("rgba(167, 139, 250, 0.1)");
    });

    it("flickerRgba produces correct rgba for 15% opacity", () => {
      const result = colors.flickerRgba(0.15);
      expect(result).toBe("rgba(167, 139, 250, 0.15)");
    });

    it("flickerRgba produces correct rgba for 100% opacity", () => {
      const result = colors.flickerRgba(1);
      expect(result).toBe("rgba(167, 139, 250, 1)");
    });

    it("flickerRgba produces correct rgba for 0% opacity", () => {
      const result = colors.flickerRgba(0);
      expect(result).toBe("rgba(167, 139, 250, 0)");
    });

    it("toRgba converts hex to rgba for violet primary", () => {
      const result = colors.toRgba("#7C3AED", 0.5);
      expect(result).toBe("rgba(124, 58, 237, 0.5)");
    });

    it("toRgba converts hex to rgba for abyssBlack", () => {
      const result = colors.toRgba("#000000", 0.8);
      expect(result).toBe("rgba(0, 0, 0, 0.8)");
    });
  });

  describe("CSS custom properties", () => {
    it("cssVariables contains all expected omneval keys", () => {
      const expectedKeys = [
        "--omneval-void",
        "--omneval-depth",
        "--omneval-surface",
        "--omneval-border",
        "--omneval-violet",
        "--omneval-violet-light",
        "--omneval-text-pure",
        "--omneval-text-mid",
        // legacy keys still present for backward compatibility
        "--omneval-bg-abyss",
        "--omneval-accent-ember",
        "--omneval-text-pure",
        "--omneval-text-ash",
      ];

      expectedKeys.forEach((key) => {
        expect(colors.cssVariables).toHaveProperty(key);
      });
    });

    it("CSS variable values match new omneval palette", () => {
      expect(colors.cssVariables["--omneval-void"]).toBe("#0A0A0F");
      expect(colors.cssVariables["--omneval-violet"]).toBe("#7C3AED");
      expect(colors.cssVariables["--omneval-text-pure"]).toBe("#FFFFFF");
      expect(colors.cssVariables["--omneval-danger"]).toBe("#EF4444");
      expect(colors.cssVariables["--omneval-danger-light"]).toBe("#FCA5A5");
    });
  });
});
