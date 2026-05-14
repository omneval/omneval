import { colors } from "@/theme";
import { Skeleton } from "./Skeleton";

// ── Empty State Variants ───────────────────────────────────────────

export type EmptyStateVariant =
  | "default"
  | "onboarding"
  | "search"
  | "error"
  | "loading";

// ── Icon Sets per Variant ──────────────────────────────────────────

const variantIcons: Record<EmptyStateVariant, React.ReactNode> = {
  default: (
    <svg width="56" height="56" viewBox="0 0 56 56" fill="none">
      <rect
        x="8"
        y="8"
        width="40"
        height="40"
        rx="6"
        stroke="currentColor"
        strokeWidth="2"
      />
      <path
        d="M18 28h20M28 18v20"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  ),
  onboarding: (
    <svg width="56" height="56" viewBox="0 0 56 56" fill="none">
      <path
        d="M20 6h16v4l6 6v14a10 10 0 01-20 0V16l6-6V6z"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinejoin="round"
      />
      <path d="M16 44h24" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      <path d="M28 12v12" stroke={colors.accents.emberFlare} strokeWidth="2.5" strokeLinecap="round" />
      <circle cx="28" cy="30" r="3" fill={colors.accents.emberFlare} />
    </svg>
  ),
  search: (
    <svg width="56" height="56" viewBox="0 0 56 56" fill="none">
      <circle cx="24" cy="24" r="14" stroke="currentColor" strokeWidth="2" />
      <path d="M34 34l14 14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  ),
  error: (
    <svg width="56" height="56" viewBox="0 0 56 56" fill="none">
      <circle cx="28" cy="28" r="18" stroke="currentColor" strokeWidth="2" />
      <path d="M28 18v12M28 34v2" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  ),
  loading: null, // handled separately
};

// ── EmptyState Component ───────────────────────────────────────────

interface EmptyStateProps {
  /** Variant controls the icon and default text */
  variant?: EmptyStateVariant;
  /** Title shown in the center */
  title?: string;
  /** Optional description below title */
  description?: string;
  /** Optional action button */
  actionLabel?: string;
  /** Called when action is clicked */
  onAction?: () => void;
  /** Override the icon */
  icon?: React.ReactNode;
  /** Custom className for the container */
  className?: string;
  /** Custom style for the container */
  style?: React.CSSProperties;
}

export function EmptyState({
  variant = "default",
  title,
  description,
  actionLabel,
  onAction,
  icon,
  className = "",
  style,
}: EmptyStateProps) {
  const defaultConfig: Record<Exclude<EmptyStateVariant, "loading">, { title: string; description: string }> = {
    onboarding: {
      title: "No traces yet",
      description: "Get started by sending your first trace to Lantern",
    },
    search: {
      title: "No results found",
      description: "Try adjusting your filters or search query",
    },
    error: {
      title: "Something went wrong",
      description: "Please try refreshing the page",
    },
    default: {
      title: "Nothing here",
      description: "Create something to get started",
    },
  };

  const config = variant === "loading" ? { title: "", description: "" } : defaultConfig[variant];
  const resolvedTitle = title ?? config.title;
  const resolvedDescription = description ?? config.description;

  return (
    <div
      className={`flex flex-col items-center justify-center py-12 px-4 text-center ${className}`}
      style={{ ...style }}
      role="status"
    >
      {/* Icon */}
      <div
        className={`mb-4 ${variant === "error" ? "text-lantern-danger" : "text-lantern-bg-cave"}`}
      >
        {icon ?? variantIcons[variant]}
      </div>

      {/* Title */}
      <p className="text-base font-semibold text-lantern-pure mb-1">
        {resolvedTitle}
      </p>

      {/* Description */}
      {resolvedDescription && (
        <p className="text-sm text-lantern-ash mb-4 opacity-80">
          {resolvedDescription}
        </p>
      )}

      {/* Action */}
      {onAction && actionLabel && (
        <button
          onClick={onAction}
          className="px-4 py-2 text-sm font-medium rounded-md text-white transition-all duration-150 hover:brightness-110 active:brightness-90"
          style={{
            background: colors.accents.emberFlare,
            boxShadow: "0 2px 8px rgba(255, 87, 34, 0.25)",
          }}
        >
          {actionLabel}
        </button>
      )}
    </div>
  );
}

// ── Loading State ──────────────────────────────────────────────────

interface LoadingStateProps {
  /** Number of skeleton rows to show */
  rows?: number;
  /** Override height of skeleton bars */
  rowHeight?: string;
  /** Show a title skeleton */
  showTitle?: boolean;
  /** Custom className */
  className?: string;
}

export function LoadingState({
  rows = 6,
  rowHeight = "1rem",
  showTitle = false,
  className = "",
}: LoadingStateProps) {
  return (
    <div className={`flex flex-col gap-3 p-4 ${className}`}>
      {showTitle && <Skeleton className="h-5 w-32 rounded" />}
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} height={rowHeight} className="rounded" />
      ))}
    </div>
  );
}
