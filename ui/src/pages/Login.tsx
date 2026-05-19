import { useState } from "react";
import { colors } from "@/theme";

interface LoginPageProps {
  onLogin: (email: string, password: string) => boolean | Promise<boolean>;
}

function OmnevalIcon() {
  return (
    <svg width="52" height="52" viewBox="0 0 52 52" fill="none" className="mx-auto mb-6">
      {/* Outer orbital ring */}
      <circle
        cx="26"
        cy="26"
        r="22"
        stroke={colors.accents.violet}
        strokeWidth="1"
        strokeDasharray="4 3"
        opacity="0.5"
      />
      {/* Middle precision ring */}
      <circle
        cx="26"
        cy="26"
        r="14"
        stroke={colors.accents.violetLight}
        strokeWidth="1.5"
        opacity="0.7"
      />
      {/* Core node */}
      <circle cx="26" cy="26" r="6" fill={colors.accents.violet} />
      <circle cx="26" cy="26" r="3" fill={colors.accents.violetPale} />
      {/* Cross-hair data points */}
      <path
        d="M26 8v6M26 38v6M8 26h6M38 26h6"
        stroke={colors.accents.violetLight}
        strokeWidth="1.5"
        strokeLinecap="round"
      />
      {/* Orbital dots */}
      <circle cx="26" cy="4" r="2" fill={colors.accents.cyan} />
      <circle cx="48" cy="26" r="2" fill={colors.accents.cyan} />
      <circle cx="26" cy="48" r="2" fill={colors.accents.violetPale} />
      <circle cx="4" cy="26" r="2" fill={colors.accents.violetPale} />
    </svg>
  );
}

export default function LoginPage({ onLogin }: LoginPageProps) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);

    try {
      const result = await onLogin(email, password);
      if (!result) {
        setError("Invalid email or password");
      }
    } catch {
      setError("Login failed. Please try again.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div
      className="flex items-center justify-center min-h-screen"
      style={{
        background: `radial-gradient(ellipse at 50% 0%, rgba(124, 58, 237, 0.15) 0%, transparent 60%), ${colors.backgrounds.voidBlack}`,
      }}
    >
      {/* Subtle grid background */}
      <div
        className="absolute inset-0 pointer-events-none"
        style={{
          backgroundImage: `linear-gradient(rgba(124, 58, 237, 0.04) 1px, transparent 1px), linear-gradient(90deg, rgba(124, 58, 237, 0.04) 1px, transparent 1px)`,
          backgroundSize: "48px 48px",
        }}
      />

      <div
        className="relative w-full max-w-sm mx-4 p-8 rounded-2xl"
        style={{
          background: "rgba(17, 17, 24, 0.95)",
          border: `1px solid ${colors.backgrounds.border}`,
          boxShadow: "0 24px 64px rgba(0, 0, 0, 0.7), 0 0 0 1px rgba(124, 58, 237, 0.1)",
          backdropFilter: "blur(12px)",
        }}
      >
        <OmnevalIcon />

        <div className="text-center mb-6">
          <h1 className="text-2xl font-bold tracking-tight text-omneval-text-pure mb-2">
            Sign in to{" "}
            <span style={{ color: colors.accents.violetLight }}>omneval</span>
          </h1>
          <p className="text-sm text-omneval-text-muted leading-relaxed">
            Omni-coverage LLM evaluation &amp; tracing
            <br />
            <span className="text-xs opacity-70">
              Precision-focused · Self-hostable · Privacy-first
            </span>
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          {error && (
            <div
              className="flex items-center gap-2 px-3 py-2.5 rounded-lg text-sm font-medium"
              style={{
                background: "rgba(239, 68, 68, 0.1)",
                border: "1px solid rgba(239, 68, 68, 0.3)",
                color: colors.accents.dangerLight,
              }}
              role="alert"
            >
              <svg width="14" height="14" viewBox="0 0 16 16" fill="none" className="flex-shrink-0">
                <circle cx="8" cy="8" r="6" stroke="currentColor" strokeWidth="1.5" />
                <path d="M8 6v4M8 10.5v.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
              {error}
            </div>
          )}

          <div>
            <label
              htmlFor="email"
              className="block text-xs font-medium text-omneval-text-muted mb-1.5"
            >
              Email
            </label>
            <input
              id="email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="w-full px-3 py-2.5 text-sm rounded-lg border bg-black/40 text-omneval-text-pure placeholder-omneval-text-muted/50 focus:outline-none transition-all"
              style={{
                borderColor: colors.backgrounds.border,
                boxShadow: "none",
              }}
              onFocus={(e) => {
                e.currentTarget.style.borderColor = colors.accents.violet;
                e.currentTarget.style.boxShadow = `0 0 0 2px ${colors.focusRing.normal}`;
              }}
              onBlur={(e) => {
                e.currentTarget.style.borderColor = colors.backgrounds.border;
                e.currentTarget.style.boxShadow = "none";
              }}
              required
              autoComplete="email"
              placeholder="you@company.com"
            />
          </div>

          <div>
            <label
              htmlFor="password"
              className="block text-xs font-medium text-omneval-text-muted mb-1.5"
            >
              Password
            </label>
            <input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full px-3 py-2.5 text-sm rounded-lg border bg-black/40 text-omneval-text-pure placeholder-omneval-text-muted/50 focus:outline-none transition-all"
              style={{
                borderColor: colors.backgrounds.border,
                boxShadow: "none",
              }}
              onFocus={(e) => {
                e.currentTarget.style.borderColor = colors.accents.violet;
                e.currentTarget.style.boxShadow = `0 0 0 2px ${colors.focusRing.normal}`;
              }}
              onBlur={(e) => {
                e.currentTarget.style.borderColor = colors.backgrounds.border;
                e.currentTarget.style.boxShadow = "none";
              }}
              required
              autoComplete="current-password"
              placeholder="••••••••"
            />
          </div>

          <button
            type="submit"
            disabled={loading}
            className="w-full py-2.5 px-4 rounded-lg text-sm font-semibold text-white transition-all duration-150 disabled:opacity-50"
            style={{
              background: `linear-gradient(135deg, ${colors.accents.violet} 0%, ${colors.accents.violetLight} 100%)`,
              boxShadow: `0 4px 16px rgba(124, 58, 237, 0.35)`,
            }}
            onMouseEnter={(e) => {
              if (!loading) {
                (e.currentTarget as HTMLElement).style.boxShadow = `0 6px 24px rgba(124, 58, 237, 0.5)`;
                (e.currentTarget as HTMLElement).style.transform = "translateY(-1px)";
              }
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLElement).style.boxShadow = `0 4px 16px rgba(124, 58, 237, 0.35)`;
              (e.currentTarget as HTMLElement).style.transform = "none";
            }}
          >
            {loading ? (
              <span className="flex items-center justify-center gap-2">
                <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24" fill="none">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                </svg>
                Authenticating...
              </span>
            ) : (
              "Sign in"
            )}
          </button>
        </form>

        {/* Brand footer */}
        <div className="mt-6 text-center">
          <span
            className="text-xs font-semibold tracking-widest uppercase"
            style={{ color: colors.accents.violet, letterSpacing: "0.15em" }}
          >
            omneval
          </span>
        </div>
      </div>
    </div>
  );
}
