import { describe, it, expect } from "vitest";
import { colors } from "@/theme";

describe("focus ring colors", () => {
  it("exports focus ring colors on the colors object", () => {
    expect(colors.focusRing).toBeDefined();
    expect(colors.focusRing.normal).toBe("rgba(255, 87, 34, 0.25)");
    expect(colors.focusRing.danger).toBe("rgba(239, 68, 68, 0.3)");
  });

  it("focus ring normal matches emberFlare at 25% opacity", () => {
    const r = parseInt("FF", 16);
    const g = parseInt("57", 16);
    const b = parseInt("22", 16);
    expect(colors.focusRing.normal).toBe(`rgba(${r}, ${g}, ${b}, 0.25)`);
  });

  it("focus ring danger matches dangerRed at 30% opacity", () => {
    const r = parseInt("EF", 16);
    const g = parseInt("44", 16);
    const b = parseInt("44", 16);
    expect(colors.focusRing.danger).toBe(`rgba(${r}, ${g}, ${b}, 0.3)`);
  });
});
