import { CopyButton } from "./CopyButton";

/**
 * OnboardingEmptyState — Actionable empty state for the Traces page.
 * Guides new users through the first three steps: install SDK,
 * create API key, and send a trace.
 */
export function OnboardingEmptyState() {
  const installCommand = "pip install lantern-sdk";
  const apiKeyCommand =
    'curl -X POST http://localhost:8080/api/v1/projects/<project-id>/api-keys -H "Content-Type: application/json" -d \'{"kind":"project"}\'';

  return (
    <div className="flex flex-col items-center justify-center py-12 px-4 text-center">
      <svg
        width="56"
        height="56"
        viewBox="0 0 56 56"
        fill="none"
        className="mb-4 text-lantern-accent-ember"
      >
        <path
          d="M20 6h16v4l6 6v14a10 10 0 01-20 0V16l6-6V6z"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinejoin="round"
        />
        <path d="M16 44h24" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
        <path d="M28 12v12" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
        <circle cx="28" cy="30" r="3" fill="currentColor" />
      </svg>

      <p className="text-base font-semibold text-lantern-pure mb-1">
        No traces yet
      </p>
      <p className="text-sm text-lantern-ash mb-6">
        Get started in 3 simple steps:
      </p>

      <div className="w-full max-w-md space-y-3">
        {/* Step 1: Install SDK */}
        <div className="flex items-center gap-3 bg-lantern-bg-charcoal rounded-lg px-4 py-2.5 border border-lantern-bg-cave">
          <span className="flex-shrink-0 w-6 h-6 flex items-center justify-center rounded-full text-xs font-bold bg-lantern-accent-ember/20 text-lantern-ember">
            1
          </span>
          <span className="text-sm text-lantern-pure flex-1 text-left">
            Install the SDK
          </span>
          <CopyButton text={installCommand} copyLabel="Copy" ariaLabel="Copy install command" />
        </div>

        {/* Step 2: Create API Key */}
        <div className="flex items-center gap-3 bg-lantern-bg-charcoal rounded-lg px-4 py-2.5 border border-lantern-bg-cave">
          <span className="flex-shrink-0 w-6 h-6 flex items-center justify-center rounded-full text-xs font-bold bg-lantern-accent-ember/20 text-lantern-ember">
            2
          </span>
          <span className="text-sm text-lantern-pure flex-1 text-left">
            Create an API key
          </span>
          <CopyButton
            text={apiKeyCommand}
            copyLabel="Copy"
            ariaLabel="Copy API key curl command"
          />
        </div>

        {/* Step 3: Send first trace */}
        <div className="flex items-center gap-3 bg-lantern-bg-charcoal rounded-lg px-4 py-2.5 border border-lantern-bg-cave">
          <span className="flex-shrink-0 w-6 h-6 flex items-center justify-center rounded-full text-xs font-bold bg-lantern-accent-ember/20 text-lantern-ember">
            3
          </span>
          <span className="text-sm text-lantern-pure flex-1 text-left">
            Send your first trace
          </span>
          <a
            href="/docs/ingest"
            target="_blank"
            rel="noopener noreferrer"
            className="text-xs text-lantern-ember hover:underline flex-shrink-0"
          >
            View ingest docs →
          </a>
        </div>
      </div>
    </div>
  );
}
