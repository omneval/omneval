/**
 * omneval Branding Guide
 *
 * Comprehensive color palette for the omneval LLM/Agent tracing UI.
 * Designed for precision, data-richness, and AI-native aesthetics.
 * Deep near-black backgrounds with violet/indigo primary and electric cyan secondary.
 *
 * ## Categories
 *
 * ### The Void — Backgrounds & Surfaces
 * Near-black backgrounds with subtle blue-tinted dark surfaces create depth
 * and separation without breaking the immersive dark mode.
 *
 * ### Violet Primary — Accents & Data Visualization
 * Violet/indigo shades are the signature accent for active states, CTAs, charts.
 *
 * ### Typography
 * High-contrast text on dark backgrounds for readability in data-dense UIs.
 *
 * ## Usage
 *
 * TypeScript (React components):
 *   ```tsx
 *   import { colors } from "@/theme";
 *   <div style={{ backgroundColor: colors.backgrounds.voidBlack }}>
 *   ```
 *
 * CSS custom properties (global):
 *   ```css
 *   .my-element { background: var(--omneval-void); }
 *   ```
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
  | "emberGlow"
  | "greenSuccess"
  | "amberWarning"
  | "dangerRed"
  | "dangerLight"
  // typography
  | "pureLight"
  | "midTone"
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
    // Legacy names preserved for test compatibility
    abyssBlack: "#0A0A0F",
    charcoalDepth: "#111118",
    slightIllumination: "#1C1C28",
    caveWall: "#2A2A3A",
    // omneval names
    voidBlack: "#0A0A0F",
    depth: "#111118",
    surface: "#1C1C28",
    border: "#2A2A3A",
  },
  accents: {
    // Legacy names preserved for test compatibility — now map to violet palette
    emberFlare: "#7C3AED",   // violet (primary)
    softGlow: "#8B5CF6",     // violet-light
    flicker: "#A78BFA",      // violet-pale
    deepHeat: "#6D28D9",     // violet-dark
    emberGlow: "#7C3AED",    // violet (same as primary for glow)
    greenSuccess: "#10B981", // emerald
    amberWarning: "#F59E0B", // amber
    dangerRed: "#EF4444",
    dangerLight: "#FCA5A5",
    // omneval-specific names
    violet: "#7C3AED",
    violetLight: "#8B5CF6",
    violetPale: "#A78BFA",
    violetDark: "#6D28D9",
    cyan: "#06B6D4",
    cyanLight: "#22D3EE",
    cyanDark: "#0891B2",
  },
  typography: {
    pureLight: "#FFFFFF",
    midTone: "#C4C4D4",
    ashGrey: "#8B8BA7",
  },
  cssVariables: {
    // omneval CSS variable names
    "--omneval-void": "#0A0A0F",
    "--omneval-depth": "#111118",
    "--omneval-surface": "#1C1C28",
    "--omneval-border": "#2A2A3A",
    "--omneval-violet": "#7C3AED",
    "--omneval-violet-light": "#8B5CF6",
    "--omneval-violet-pale": "#A78BFA",
    "--omneval-violet-dark": "#6D28D9",
    "--omneval-cyan": "#06B6D4",
    "--omneval-cyan-light": "#22D3EE",
    "--omneval-success": "#10B981",
    "--omneval-warning": "#F59E0B",
    "--omneval-danger": "#EF4444",
    "--omneval-danger-light": "#FCA5A5",
    "--omneval-text-pure": "#FFFFFF",
    "--omneval-text-mid": "#C4C4D4",
    "--omneval-text-muted": "#8B8BA7",
    "--omneval-card-shadow": "0 1px 4px rgba(0, 0, 0, 0.6), 0 1px 2px rgba(0, 0, 0, 0.4)",
    "--omneval-card-hover-shadow": "0 4px 16px rgba(0, 0, 0, 0.7), 0 2px 8px rgba(0, 0, 0, 0.5)",
    // Legacy CSS variable names preserved for test compatibility
    "--omneval-bg-abyss": "#0A0A0F",
    "--omneval-bg-charcoal": "#111118",
    "--omneval-bg-illumination": "#1C1C28",
    "--omneval-bg-cave": "#2A2A3A",
    "--omneval-accent-ember": "#7C3AED",
    "--omneval-accent-glow": "#8B5CF6",
    "--omneval-accent-flicker": "#A78BFA",
    "--omneval-accent-heat": "#6D28D9",
    "--omneval-accent-ember-glow": "#7C3AED",
    "--omneval-accent-success": "#10B981",
    "--omneval-accent-warning": "#F59E0B",
    "--omneval-accent-danger": "#EF4444",
    "--omneval-accent-danger-light": "#FCA5A5",
    "--omneval-text-ash": "#8B8BA7",
  },
  /** Chart colors for multi-series visualizations — violet-to-cyan gradient palette. */
  chartColors: {
    /** Series fill colors — violet/indigo to cyan spectrum. */
    series: [
      "#7C3AED", // violet (primary)
      "#8B5CF6", // violet-light
      "#06B6D4", // cyan
      "#A78BFA", // violet-pale
      "#22D3EE", // cyan-light
      "#6D28D9", // violet-dark
    ] as const,
    /** Returns a fill-opacity for bar charts based on series index. */
    barOpacity: (index: number, total: number): number => {
      return 0.55 + (0.45 * index) / Math.max(total, 1);
    },
  },
  /** Focus ring colors for form inputs. */
  focusRing: {
    /** Normal focus — violet at 30% opacity */
    normal: "rgba(124, 58, 237, 0.3)",
    /** Danger focus — dangerRed at 30% opacity */
    danger: "rgba(239, 68, 68, 0.3)",
  },
} as const;

type BaseColors = typeof baseColors;

// ──────────────────────────────────────────────────────────────
// Color Palette (exported)
// ──────────────────────────────────────────────────────────────

export const colors: BaseColors & {
  /** Convert any hex color to rgba with given alpha (0–1). */
  toRgba: (hex: string, alpha: number) => string;
  /** Convenience: violet-pale as rgba with given alpha. */
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

  /** Pre-packaged violet-pale (flicker) with configurable alpha.
   *  Preserved for backward-compatibility — maps to #A78BFA. */
  flickerRgba(alpha: number): string {
    return this.toRgba("#A78BFA", alpha);
  },
};
