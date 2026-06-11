/**
 * Shared helpers for the Header time-range presets.
 *
 * The Header exposes presets ("1h", "6h", "1d", "7d", "30d", "custom");
 * every page that queries spans converts the active preset into a concrete
 * from/to window with these helpers so the selector behaves identically
 * everywhere.
 */

/** Map from preset value to millisecond duration. */
export const TIME_RANGE_MS: Record<string, number> = {
  "1h": 60 * 60 * 1000,
  "6h": 6 * 60 * 60 * 1000,
  "1d": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
};

/** Human-readable labels for each preset. */
export const TIME_RANGE_LABELS: Record<string, string> = {
  "1h": "Past 1 hour",
  "6h": "Past 6 hours",
  "1d": "Past 24 hours",
  "7d": "Past 7 days",
  "30d": "Past 30 days",
  custom: "Custom range",
};

const DEFAULT_RANGE_DAYS = 7;

/**
 * Compute from/to ISO strings for a preset. Unknown presets (including
 * "custom" and undefined) fall back to the past 7 days.
 */
export function presetToFromTo(preset: string | undefined): {
  from: string;
  to: string;
} {
  const now = new Date();
  const durationMs =
    preset && TIME_RANGE_MS[preset]
      ? TIME_RANGE_MS[preset]
      : DEFAULT_RANGE_DAYS * 24 * 60 * 60 * 1000;
  return {
    from: new Date(now.getTime() - durationMs).toISOString(),
    to: now.toISOString(),
  };
}
