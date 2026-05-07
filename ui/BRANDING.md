# Lantern Branding Guide

Comprehensive color palette for the **Lantern** LLM/Agent tracing UI.

## Design Philosophy

Balance the deep, oppressive darkness of a cave with the warm, guiding glow
of a lantern — ensuring high contrast and legibility for complex data
visualization.

---

## Colors

### The Cave — Backgrounds & Surfaces

These colors form the foundation of the app. Very slight variations of black
and dark grey create depth and separation between the sidebar, main canvas,
and data cards without breaking the immersive dark mode.

| Name | Hex | Use |
|---|---|---|
| Abyss Black | `#000000` | Absolute background. Main canvas behind dashboard widgets and data tables. |
| Charcoal Depth | `#0D0D0D` | Secondary background. Left sidebar, top header bar, dashboard widget/card backgrounds. |
| Slight Illumination | `#1A1A1A` | Tertiary dark shade. Table headers, search bar backgrounds, subtle hover states on dark rows. |
| Cave Wall | `#2D2D2D` | UI borders, dividers between table rows, inactive tabs. |

### The Lantern — Accents & Data Visualization

These varying shades of orange draw the user's eye, indicate active states,
and plot data on charts.

| Name | Hex | Use |
|---|---|---|
| Ember Flare | `#FF5722` | Primary brand accent. Active tab borders, primary CTA buttons, "Lantern" logo, main bright vector line on charts. |
| Soft Glow | `#FF8A65` | Secondary buttons, checkbox ticks, pagination controls, secondary data bars in horizontal charts. |
| Flicker | `#FFCCBC` | Pale orange. Faint background wash on row hover in Tracing List View, highlighting text strings in JSON snippets. |
| Deep Heat | `#E64A19` | Darker orange-red. Pressed button states, critical alerts, error/high-latency spikes in the data table. |

### Illumination — Typography

High-contrast but not blinding.

| Name | Hex | Use |
|---|---|---|
| Pure Light | `#FFFFFF` | Primary text. Trace names, numbers, data table values, active menu items. |
| Ash Grey | `#A1A1AA` | Secondary text. Column headers, empty states ("No data" placeholder), timestamps, disabled UI elements. |

---

## CSS Custom Properties

All colors are available as CSS variables under the `:root` scope.

| Variable | Value |
|---|---|
| `--lantern-bg-abyss` | `#000000` |
| `--lantern-bg-charcoal` | `#0D0D0D` |
| `--lantern-bg-illumination` | `#1A1A1A` |
| `--lantern-bg-cave` | `#2D2D2D` |
| `--lantern-accent-ember` | `#FF5722` |
| `--lantern-accent-glow` | `#FF8A65` |
| `--lantern-accent-flicker` | `#FFCCBC` |
| `--lantern-accent-heat` | `#E64A19` |
| `--lantern-text-pure` | `#FFFFFF` |
| `--lantern-text-ash` | `#A1A1AA` |

### Derived Variables

| Variable | Value |
|---|---|
| `--lantern-accent-flicker-hover` | `rgba(255, 204, 188, 0.1)` |
| `--lantern-accent-flicker-hover-strong` | `rgba(255, 204, 188, 0.15)` |
| `--lantern-accent-ember-glow` | `rgba(255, 87, 34, 0.15)` |
| `--lantern-accent-heat-glow` | `rgba(230, 74, 25, 0.15)` |

---

## Usage

### In React Components (TypeScript)

```tsx
import { colors } from "@/theme";

<div style={{
  backgroundColor: colors.backgrounds.abyssBlack,
  color: colors.typography.pureLight,
  border: `1px solid ${colors.backgrounds.caveWall}`,
}}>
  Content
</div>
```

### In CSS

```css
.table-row:hover {
  background: var(--lantern-accent-flicker-hover);
}

.nav-link.active {
  border-bottom: 2px solid var(--lantern-accent-ember);
  color: var(--lantern-text-pure);
}
```

### Hover States

Apply the "Flicker" (`#FFCCBC`) hover state to table rows with **10% to 15% opacity**
(e.g., `rgba(255, 204, 188, 0.1)`) over the Abyss Black background. This creates a
subtle, glowing highlight effect that mimics lantern light sweeping across the data.

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
- Add chart color schemes using the accent palette
