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
    it("abyssBlack is #000000", () => {
      expect(colors.backgrounds.abyssBlack).toBe("#000000");
    });

    it("charcoalDepth is #0D0D0D", () => {
      expect(colors.backgrounds.charcoalDepth).toBe("#0D0D0D");
    });

    it("slightIllumination is #1A1A1A", () => {
      expect(colors.backgrounds.slightIllumination).toBe("#1A1A1A");
    });

    it("caveWall is #2D2D2D", () => {
      expect(colors.backgrounds.caveWall).toBe("#2D2D2D");
    });

    it("emberFlare is #FF5722", () => {
      expect(colors.accents.emberFlare).toBe("#FF5722");
    });

    it("softGlow is #FF8A65", () => {
      expect(colors.accents.softGlow).toBe("#FF8A65");
    });

    it("flicker is #FFCCBC", () => {
      expect(colors.accents.flicker).toBe("#FFCCBC");
    });

    it("deepHeat is #E64A19", () => {
      expect(colors.accents.deepHeat).toBe("#E64A19");
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

    it("ashGrey is #A1A1AA", () => {
      expect(colors.typography.ashGrey).toBe("#A1A1AA");
    });
  });

  describe("derived utilities", () => {
    it("flickerRgba produces correct rgba for 10% opacity", () => {
      const result = colors.flickerRgba(0.1);
      expect(result).toBe("rgba(255, 204, 188, 0.1)");
    });

    it("flickerRgba produces correct rgba for 15% opacity", () => {
      const result = colors.flickerRgba(0.15);
      expect(result).toBe("rgba(255, 204, 188, 0.15)");
    });

    it("flickerRgba produces correct rgba for 100% opacity", () => {
      const result = colors.flickerRgba(1);
      expect(result).toBe("rgba(255, 204, 188, 1)");
    });

    it("flickerRgba produces correct rgba for 0% opacity", () => {
      const result = colors.flickerRgba(0);
      expect(result).toBe("rgba(255, 204, 188, 0)");
    });

    it("toRgba converts hex to rgba for emberFlare", () => {
      const result = colors.toRgba("#FF5722", 0.5);
      expect(result).toBe("rgba(255, 87, 34, 0.5)");
    });

    it("toRgba converts hex to rgba for abyssBlack", () => {
      const result = colors.toRgba("#000000", 0.8);
      expect(result).toBe("rgba(0, 0, 0, 0.8)");
    });
  });

  describe("CSS custom properties", () => {
    it("cssVariables contains all expected keys", () => {
      const expectedKeys = [
        "--lantern-bg-abyss",
        "--lantern-bg-charcoal",
        "--lantern-bg-illumination",
        "--lantern-bg-cave",
        "--lantern-accent-ember",
        "--lantern-accent-glow",
        "--lantern-accent-flicker",
        "--lantern-accent-heat",
        "--lantern-text-pure",
        "--lantern-text-ash",
      ];

      expectedKeys.forEach((key) => {
        expect(colors.cssVariables).toHaveProperty(key);
      });
    });

    it("CSS variable values match hex colors", () => {
      expect(colors.cssVariables["--lantern-bg-abyss"]).toBe("#000000");
      expect(colors.cssVariables["--lantern-bg-charcoal"]).toBe("#0D0D0D");
      expect(colors.cssVariables["--lantern-accent-ember"]).toBe("#FF5722");
      expect(colors.cssVariables["--lantern-text-pure"]).toBe("#FFFFFF");
      expect(colors.cssVariables["--lantern-accent-danger"]).toBe("#EF4444");
      expect(colors.cssVariables["--lantern-accent-danger-light"]).toBe("#FCA5A5");
    });
  });
});
