# Omneval Branding Guide

Comprehensive color palette for the **Omneval** LLM/Agent tracing UI.

## Design Philosophy

Deep near-black backgrounds with subtle blue-tinted surfaces create depth and
separation without breaking the immersive dark mode. Violet/indigo is the
signature accent — used for active states, CTAs, and data visualization. Electric
cyan provides secondary contrast for charts, highlights, and success indicators.
High-contrast typography ensures readability in data-dense interfaces.

---

## Colors

### The Void — Backgrounds & Surfaces

Near-black backgrounds with subtle blue-tinted dark surfaces create depth and
separation between the sidebar, main canvas, and data cards without breaking
the immersive dark mode.

| Name | Hex | Use |
|---|---|---|
| Void Black | `#0A0A0F` | Absolute root background. Main canvas behind dashboard widgets and data tables. |
| Depth | `#111118` | Secondary background. Left sidebar, top header bar, dashboard widget/card backgrounds. |
| Surface | `#1C1C28` | Tertiary dark shade. Table headers, search bar backgrounds, subtle hover states on dark rows. |
| Border | `#2A2A3A` | UI borders, dividers between table rows, inactive tabs, card edges. |

### Violet Primary — Accents & Data Visualization

Violet/indigo shades are the signature accent for active states, CTAs, charts,
and primary data visualization elements.

| Name | Hex | Use |
|---|---|---|
| Violet | `#7C3AED` | Primary brand accent. Active tab borders, primary CTA buttons, "Omneval" logo, main vector line on charts. |
| Violet Light | `#8B5CF6` | Secondary buttons, checkbox ticks, pagination controls, secondary data bars in horizontal charts. |
| Violet Pale | `#A78BFA` | Faint background wash on row hover in Tracing List View, highlighting text strings in JSON snippets. |
| Violet Dark | `#6D28D9` | Pressed button states, critical alerts, emphasis states on primary elements. |

### Cyan Secondary — Data Highlights & Success

Electric cyan provides secondary contrast for success indicators, data highlights,
and accent colors that need to stand apart from the violet primary.

| Name | Hex | Use |
|---|---|---|
| Cyan | `#06B6D4` | Secondary accent. Success indicators, data series highlights in charts, secondary interactive elements. |
| Cyan Light | `#22D3EE` | Lighter cyan for high-contrast secondary data bars, status badges. |
| Cyan Dark | `#0891B2` | Darker cyan for pressed states on cyan-accented elements, deeper data series. |

### Semantic States

Standard semantic colors for success, warning, and error states.

| Name | Hex | Use |
|---|---|---|
| Success | `#10B981` | Success indicators, positive values, green status dots, confirmation states. |
| Warning | `#F59E0B` | Warning states, caution indicators, amber status dots. |
| Danger | `#EF4444` | Error states, critical alerts, danger indicators, error/high-latency spikes in data tables. |
| Danger Light | `#FCA5A5` | Error text on dark backgrounds, secondary error indicators, readable red text. |

### Typography

High-contrast text colors for readability on dark backgrounds.

| Name | Hex | Use |
|---|---|---|
| Pure Light | `#FFFFFF` | Primary text. Trace names, numbers, data table values, active menu items. |
| Mid | `#C4C4D4` | Secondary text. Column headers, data descriptions, active-but-not-primary states. |
| Muted | `#8B8BA7` | Tertiary text. Column headers, empty states ("No data" placeholder), timestamps, disabled UI elements. |

---

## CSS Custom Properties

All colors are available as CSS variables under the `:root` scope.

### Core Colors

| Variable | Value |
|---|---|
| `--omneval-void` | `#0A0A0F` |
| `--omneval-depth` | `#111118` |
| `--omneval-surface` | `#1C1C28` |
| `--omneval-border` | `#2A2A3A` |

### Violet Primary

| Variable | Value |
|---|---|
| `--omneval-violet` | `#7C3AED` |
| `--omneval-violet-light` | `#8B5CF6` |
| `--omneval-violet-pale` | `#A78BFA` |
| `--omneval-violet-dark` | `#6D28D9` |

### Cyan Secondary

| Variable | Value |
|---|---|
| `--omneval-cyan` | `#06B6D4` |
| `--omneval-cyan-light` | `#22D3EE` |

### Semantic States

| Variable | Value |
|---|---|
| `--omneval-success` | `#10B981` |
| `--omneval-warning` | `#F59E0B` |
| `--omneval-danger` | `#EF4444` |
| `--omneval-danger-light` | `#FCA5A5` |

### Typography

| Variable | Value |
|---|---|
| `--omneval-text-pure` | `#FFFFFF` |
| `--omneval-text-mid` | `#C4C4D4` |
| `--omneval-text-muted` | `#8B8BA7` |

### Derived Variables

| Variable | Value |
|---|---|
| `--omneval-violet-hover` | `rgba(124, 58, 237, 0.12)` |
| `--omneval-violet-hover-strong` | `rgba(124, 58, 237, 0.18)` |
| `--omneval-violet-active-bg` | `rgba(139, 92, 246, 0.15)` |
| `--omneval-cyan-hover` | `rgba(6, 182, 212, 0.12)` |

### Focus Ring

| Variable | Value |
|---|---|
| `--omneval-focus-ring` | `rgba(124, 58, 237, 0.3)` |
| `--omneval-focus-ring-danger` | `rgba(239, 68, 68, 0.3)` |

### Card Elevation

| Variable | Value |
|---|---|
| `--omneval-card-shadow` | `0 1px 4px rgba(0, 0, 0, 0.6), 0 1px 2px rgba(0, 0, 0, 0.4)` |
| `--omneval-card-hover-shadow` | `0 4px 16px rgba(0, 0, 0, 0.7), 0 2px 8px rgba(0, 0, 0, 0.5)` |

### Semantic Aliases

| Variable | Value |
|---|---|
| `--omneval-bg-root` | `var(--omneval-void)` |
| `--omneval-bg-surface` | `var(--omneval-depth)` |
| `--omneval-bg-card` | `var(--omneval-depth)` |
| `--omneval-bg-elevated` | `var(--omneval-surface)` |
| `--omneval-primary` | `var(--omneval-violet)` |
| `--omneval-secondary` | `var(--omneval-violet-light)` |

---

## Usage

### In React Components (TypeScript)

```tsx
import { colors } from "@/theme";

<div style={{
  backgroundColor: colors.backgrounds.voidBlack,
  color: colors.typography.pureLight,
  border: `1px solid ${colors.backgrounds.border}`,
}}>
  Content
</div>
```

### In CSS

```css
.table-row:hover {
  background: var(--omneval-violet-hover);
}

.nav-link.active {
  border-bottom: 2px solid var(--omneval-violet);
  color: var(--omneval-text-pure);
}
```

### Hover States

Apply the "Violet Pale" (`#A78BFA`) hover state to table rows with **12% to 18% opacity**
(e.g., `rgba(124, 58, 237, 0.12)` via `--omneval-violet-hover`) over the Void Black
background. This creates a subtle, glowing highlight effect that draws the eye across
data rows without overwhelming the content.

### Focus States

Use `--omneval-focus-ring` for standard input focus (violet at 30% opacity) and
`--omneval-focus-ring-danger` for validation errors (danger at 30% opacity).

### Card Shadows

Use `--omneval-card-shadow` for default card elevation and
`--omneval-card-hover-shadow` for interactive hover states on cards and panels.

---

## Chart Colors

For multi-series data visualizations, the palette cycles through violet-to-cyan
shades:

| Series | Color |
|---|---|
| 1 | `#7C3AED` (violet primary) |
| 2 | `#8B5CF6` (violet-light) |
| 3 | `#06B6D4` (cyan) |
| 4 | `#A78BFA` (violet-pale) |
| 5 | `#22D3EE` (cyan-light) |
| 6 | `#6D28D9` (violet-dark) |

Access via `colors.chartColors.series` in TypeScript or derive in CSS using the
violet and cyan custom properties.

---

## Files

| File | Purpose |
|---|---|
| `src/theme.ts` | TypeScript constants — source of truth for AI agents and frontend components |
| `src/theme.css` | CSS custom properties — usable in any stylesheet |
| `BRANDING.md` | This document — human & AI reference |

---

## Next Steps

- Integrate with a CSS-in-JS or Tailwind theme configuration
- Apply the full dark mode palette across all pages (App, Login, Traces, Dashboard)
- Expand chart color schemes using the violet-to-cyan gradient palette
