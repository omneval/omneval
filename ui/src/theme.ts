/**
 * Lantern Branding Guide
 *
 * Comprehensive color palette for the Lantern LLM/Agent tracing UI.
 * Designed to balance the deep, oppressive darkness of a cave with
 * the warm, guiding glow of a lantern — ensuring high contrast and
 * legibility for complex data visualization.
 *
 * ## Categories
 *
 * ### The Cave — Backgrounds & Surfaces
 * These colors form the foundation of the app. By using very slight
 * variations of black and dark grey, you create depth and separation
 * between the sidebar, main canvas, and data cards without breaking
 * the immersive dark mode.
 *
 * ### The Lantern — Accents & Data Visualization
 * These varying shades of orange are your primary tools for drawing
 * the user's eye, indicating active states, and plotting data on charts.
 *
 * ### Illumination — Typography
 * To maintain readability against the dark backgrounds, typography
 * must be high-contrast but not blinding.
 *
 * ## Usage
 *
 * TypeScript (React components):
 *   ```tsx
 *   import { colors } from "@/theme";
 *   <div style={{ backgroundColor: colors.backgrounds.abyssBlack }}>
 *   ```
 *
 * CSS custom properties (global):
 *   ```css
 *   .my-element { background: var(--lantern-bg-abyss); }
 *   ```
 *
 * ## Implementation Notes
 *
 * - Hover state on table rows: use flickerRgba(0.1) or flickerRgba(0.15)
 *   over abyssBlack for a subtle glowing highlight effect.
 */

// ──────────────────────────────────────────────────────────────
// Types
// ──────────────────────────────────────────────────────────────

export type ColorCategory = "backgrounds" | "accents" | "typography";

export type ColorName =
  // backgrounds
  | "abyssBlack"
  | "charcoalDepth"
  | "slightIllumination"
  | "caveWall"
  // accents
  | "emberFlare"
  | "softGlow"
  | "flicker"
  | "deepHeat"
  | "dangerRed"
  | "dangerLight"
  // typography
  | "pureLight"
  | "ashGrey";

export type HexColor = `#${string}`;

export interface BrandingTheme {
  backgrounds: Record<string, HexColor>;
  accents: Record<string, HexColor>;
  typography: Record<string, HexColor>;
}

// ── Internal base (as const for precise types) ──────────────

const baseColors = {
  backgrounds: {
    abyssBlack: "#000000",
    charcoalDepth: "#0D0D0D",
    slightIllumination: "#1A1A1A",
    caveWall: "#2D2D2D",
  },
  accents: {
    emberFlare: "#FF5722",
    softGlow: "#FF8A65",
    flicker: "#FFCCBC",
    deepHeat: "#E64A19",
    dangerRed: "#EF4444",
    dangerLight: "#FCA5A5",
  },
  typography: {
    pureLight: "#FFFFFF",
    ashGrey: "#A1A1AA",
  },
  cssVariables: {
    "--lantern-bg-abyss": "#000000",
    "--lantern-bg-charcoal": "#0D0D0D",
    "--lantern-bg-illumination": "#1A1A1A",
    "--lantern-bg-cave": "#2D2D2D",
    "--lantern-accent-ember": "#FF5722",
    "--lantern-accent-glow": "#FF8A65",
    "--lantern-accent-flicker": "#FFCCBC",
    "--lantern-accent-heat": "#E64A19",
    "--lantern-accent-danger": "#EF4444",
    "--lantern-accent-danger-light": "#FCA5A5",
    "--lantern-text-pure": "#FFFFFF",
    "--lantern-text-ash": "#A1A1AA",
  },
} as const;

type BaseColors = typeof baseColors;

// ──────────────────────────────────────────────────────────────
// Color Palette (exported)
// ──────────────────────────────────────────────────────────────

export const colors: BaseColors & {
  /** Convert any hex color to rgba with given alpha (0–1). */
  toRgba: (hex: string, alpha: number) => string;
  /** Convenience: flicker (pale orange) as rgba with given alpha. */
  flickerRgba: (alpha: number) => string;
} = {
  ...baseColors,

  /** Convert a hex color to rgba string. */
  toRgba(hex: string, alpha: number): string {
    const cleaned = hex.replace("#", "");
    const r = parseInt(cleaned.slice(0, 2), 16);
    const g = parseInt(cleaned.slice(2, 4), 16);
    const b = parseInt(cleaned.slice(4, 6), 16);
    return `rgba(${r}, ${g}, ${b}, ${alpha})`;
  },

  /** Pre-packaged flicker with configurable alpha. */
  flickerRgba(alpha: number): string {
    return this.toRgba("#FFCCBC", alpha);
  },
};
