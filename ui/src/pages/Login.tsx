import { useState } from "react";
import { colors } from "@/theme";

interface LoginPageProps {
  onLogin: (email: string, password: string) => boolean | Promise<boolean>;
}

function LanternIcon() {
  return (
    <svg width="40" height="40" viewBox="0 0 40 40" fill="none" className="mx-auto mb-4">
      <path
        d="M14 4h12v4l5 5v11a8 8 0 01-16 0V13l5-5V4z"
        stroke={colors.accents.emberFlare}
        strokeWidth="2"
        strokeLinejoin="round"
      />
      <path d="M10 32h20" stroke={colors.accents.emberFlare} strokeWidth="2" strokeLinecap="round" />
      <path d="M20 8v10" stroke={colors.accents.emberFlare} strokeWidth="2.5" strokeLinecap="round" />
      <circle cx="20" cy="22" r="2.5" fill={colors.accents.emberFlare} />
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
        background: `radial-gradient(ellipse at center top, #1a0a00 0%, transparent 60%), ${colors.backgrounds.abyssBlack}`,
      }}
    >
      <div
        className="w-full max-w-sm mx-4 p-8 rounded-xl"
        style={{
          background: "rgba(13, 13, 13, 0.95)",
          border: `1px solid ${colors.backgrounds.caveWall}`,
          boxShadow: "0 16px 48px rgba(0, 0, 0, 0.6)",
        }}
      >
        <LanternIcon />
        <h1 className="text-xl font-semibold text-center text-lantern-pure mb-1">
          Sign in to Lantern
        </h1>
        <p className="text-sm text-center text-lantern-ash mb-6 leading-relaxed">
          LLM/Agent tracing, evaluation &amp; cost observability
          <br />
          <span className="text-xs text-lantern-ash opacity-60">
            Self-hostable · Open source · Privacy-first
          </span>
        </p>

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
              className="block text-xs font-medium text-lantern-ash mb-1.5"
            >
              Email
            </label>
            <input
              id="email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="w-full px-3 py-2.5 text-sm rounded-lg border bg-black/40 text-lantern-pure placeholder-lantern-ash/50 focus:outline-none focus:ring-2 focus:ring-[#FF5722] focus:border-transparent transition-all"
              style={{ borderColor: colors.backgrounds.caveWall }}
              required
              autoComplete="email"
            />
          </div>
          <div>
            <label
              htmlFor="password"
              className="block text-xs font-medium text-lantern-ash mb-1.5"
            >
              Password
            </label>
            <input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full px-3 py-2.5 text-sm rounded-lg border bg-black/40 text-lantern-pure placeholder-lantern-ash/50 focus:outline-none focus:ring-2 focus:ring-[#FF5722] focus:border-transparent transition-all"
              style={{ borderColor: colors.backgrounds.caveWall }}
              required
              autoComplete="current-password"
            />
          </div>
          <button
            type="submit"
            disabled={loading}
            className="w-full py-2.5 px-4 rounded-lg text-sm font-medium text-white transition-all duration-150 disabled:opacity-50 hover:brightness-110 active:brightness-90"
            style={{
              background: colors.accents.emberFlare,
              boxShadow: `0 2px 8px rgba(255, 87, 34, 0.3)`,
            }}
          >
            {loading ? "Signing in..." : "Sign in"}
          </button>
        </form>
      </div>
    </div>
  );
}
