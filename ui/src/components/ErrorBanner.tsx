interface ErrorBannerProps {
  message: string;
  onDismiss?: () => void;
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
export function ErrorBanner({ message, onDismiss }: ErrorBannerProps) {
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
