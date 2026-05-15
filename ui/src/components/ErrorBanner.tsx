import { Component, ErrorInfo, ReactNode } from "react";
import { colors } from "@/theme";

// ── Error Boundary ─────────────────────────────────────────────────

interface ErrorBoundaryProps {
  children: ReactNode;
  fallback?: ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

/**
 * ErrorBoundary catches React errors in its child tree and renders
 * a user-friendly fallback UI instead of a blank white screen.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("[ErrorBoundary] Uncaught error:", error, info);
  }

  handleReload = (): void => {
    this.setState({ hasError: false, error: null });
    window.location.reload();
  };

  render(): ReactNode {
    if (this.state.hasError) {
      if (this.props.fallback) return this.props.fallback;
      return (
        <div
          className="flex flex-col items-center justify-center py-16 px-4 text-center"
          style={{ background: colors.backgrounds.abyssBlack }}
        >
          <svg
            width="48" height="48" viewBox="0 0 56 56" fill="none"
            className="mb-4 text-lantern-danger"
          >
            <circle cx="28" cy="28" r="18" stroke="currentColor" strokeWidth="2" />
            <path d="M28 18v12M28 34v2" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p className="text-lg font-semibold text-lantern-pure mb-2">
            Something went wrong
          </p>
          <p className="text-sm text-lantern-ash mb-4 max-w-md">
            {this.state.error?.message || "An unexpected error occurred"}
          </p>
          <button
            onClick={this.handleReload}
            className="px-4 py-2 text-sm font-medium rounded-md text-white transition-all duration-150 hover:brightness-110"
            style={{
              background: colors.accents.emberFlare,
              boxShadow: "0 2px 8px rgba(255, 87, 34, 0.25)",
            }}
          >
            Reload Page
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}

interface ErrorBannerProps {
  message: string;
  onDismiss?: () => void;
  onRetry?: () => void;
  retryLabel?: string;
}

interface InfoBannerProps {
  message: string;
}

function ErrorIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" className="flex-shrink-0">
      <circle cx="8" cy="8" r="6" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 6v4M8 10.5v.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function WarningIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" className="flex-shrink-0">
      <path d="M8 2l6 12H2L8 2z" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
      <path d="M8 6v3M8 10.5v.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function InfoIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" className="flex-shrink-0">
      <circle cx="8" cy="8" r="6" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 7v4M8 5v.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

/**
 * ErrorBanner — Shows an error with red icon + text on a dark background.
 * Uses the lantern accent color system for consistent error styling.
 */
function RetryIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" className="flex-shrink-0">
      <path
        d="M2 8a6 6 0 0111.13-2.83M14 8a6 6 0 01-11.13 2.83"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
      <path d="M13 2v4h-4" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M3 14v-4h4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function ErrorBanner({ message, onDismiss, onRetry, retryLabel = "Retry" }: ErrorBannerProps) {
  return (
    <div
      className="flex items-start gap-2.5 px-3 py-2.5 rounded-lg text-sm font-medium"
      style={{
        background: "rgba(239, 68, 68, 0.1)",
        border: "1px solid rgba(239, 68, 68, 0.25)",
        color: "#FCA5A5",
      }}
      role="alert"
    >
      <ErrorIcon />
      <span className="flex-1">{message}</span>
      {onDismiss && (
        <button
          onClick={onDismiss}
          className="flex-shrink-0 p-0.5 rounded hover:bg-white/10 transition-colors"
          aria-label="Dismiss"
          style={{ color: "inherit" }}
        >
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
            <path
              d="M4 4l8 8M12 4l-8 8"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
            />
          </svg>
        </button>
      )}
      {onRetry && (
        <button
          onClick={onRetry}
          className="flex-shrink-0 inline-flex items-center gap-1.5 px-2.5 py-1 rounded text-xs font-medium transition-all duration-150 hover:brightness-110"
          style={{
            background: "rgba(255, 87, 34, 0.15)",
            border: "1px solid rgba(255, 87, 34, 0.3)",
            color: "#FF8A65",
          }}
        >
          <RetryIcon />
          {retryLabel}
        </button>
      )}
    </div>
  );
}

/**
 * WarningBanner — Shows a warning with amber icon.
 */
export function WarningBanner({ message }: { message: string }) {
  return (
    <div
      className="flex items-start gap-2.5 px-3 py-2.5 rounded-lg text-sm font-medium"
      style={{
        background: "rgba(245, 158, 11, 0.1)",
        border: "1px solid rgba(245, 158, 11, 0.25)",
        color: "#FCD34D",
      }}
      role="alert"
    >
      <WarningIcon />
      <span className="flex-1">{message}</span>
    </div>
  );
}

/**
 * InfoBanner — Shows an info message with blue icon.
 */
export function InfoBanner({ message }: InfoBannerProps) {
  return (
    <div
      className="flex items-start gap-2.5 px-3 py-2.5 rounded-lg text-sm font-medium"
      style={{
        background: "rgba(59, 130, 246, 0.1)",
        border: "1px solid rgba(59, 130, 246, 0.25)",
        color: "#93C5FD",
      }}
    >
      <InfoIcon />
      <span className="flex-1">{message}</span>
    </div>
  );
}
