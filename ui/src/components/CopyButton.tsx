import { useState, useCallback } from "react";

interface CopyButtonProps {
  text: string;
  /** Label shown on the button when not copying. Defaults to a copy icon. */
  copyLabel?: string;
  /** Aria label for the button. */
  ariaLabel?: string;
}

/**
 * CopyButton — Renders a small clipboard icon that copies text to the
 * user's clipboard and briefly shows "Copied!" feedback.
 */
export function CopyButton({
  text,
  copyLabel = "Copy",
  ariaLabel = "Copy to clipboard",
}: CopyButtonProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard write failed — silently ignore
    }
  }, [text]);

  return (
    <button
      onClick={handleCopy}
      aria-label={ariaLabel}
      className="inline-flex items-center gap-1 text-xs text-omneval-text-muted hover:text-omneval-text-pure transition-colors px-2 py-1 rounded hover:bg-omneval-surface"
    >
      {copied ? (
        <span className="text-omneval-violet-pale font-medium">Copied!</span>
      ) : (
        <>
          <svg
            width="14"
            height="14"
            viewBox="0 0 16 16"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
          >
            <rect x="5" y="5" width="9" height="9" rx="1.5" />
            <path d="M3 11V3a1.5 1.5 0 011.5-1.5H11" strokeLinecap="round" />
          </svg>
          <span>{copyLabel}</span>
        </>
      )}
    </button>
  );
}
