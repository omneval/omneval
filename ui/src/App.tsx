import { useState, useEffect } from "react";
import LoginPage from "./pages/Login";
import TracesPage from "./pages/Traces";
import TraceDetailPage from "./pages/TraceDetail";
import DashboardPage from "./pages/Dashboard";
import PromptsPage from "./pages/Prompts";
import Header from "./components/Header";
import Layout from "./components/Layout";
import { ToastProvider } from "./components/Toast";
import { colors } from "./theme";

type Page =
  | "login"
  | "traces"
  | "trace-detail"
  | "dashboard"
  | "scores"
  | "sessions"
  | "users"
  | "prompts"
  | "playground"
  | "judge-llm"
  | "human-annotation"
  | "datasets"
  | "settings";

const NAV_MAP: Record<string, Page> = {
  dashboard: "dashboard",
  traces: "traces",
  sessions: "sessions",
  users: "users",
  prompts: "prompts",
  playground: "playground",
  scores: "scores",
  "judge-llm": "judge-llm",
  "human-annotation": "human-annotation",
  datasets: "datasets",
  settings: "settings",
};

export default function App() {
  const [page, setPage] = useState<Page>("login");
  const [projects, setProjects] = useState<
    { project_id: string; name: string; org_id: string }[]
  >([]);
  const [activeProject, setActiveProject] = useState<string>("");
  const [activeTraceId, setActiveTraceId] = useState<string>("");

  const [timeRange, setTimeRange] = useState("1d");
  const [environment, setEnvironment] = useState("default");

  // Check for existing session on mount
  useEffect(() => {
    const cookie = document.cookie
      .split("; ")
      .find((c) => c.startsWith("lantern_session="));
    if (cookie) {
      const id = cookie.split("=")[1];
      fetchProjects(id);
      setPage("traces");
    }
  }, []);

  const fetchProjects = async (_session: string) => {
    const res = await fetch("/api/v1/projects");
    if (res.ok) {
      const data = await res.json();
      if (Array.isArray(data)) {
        setProjects(data);
        setActiveProject(data[0]?.project_id ?? "");
      }
    }
  };

  const handleLogin = async (email: string, password: string): Promise<boolean> => {
    const res = await fetch("/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, password }),
    });
    if (res.ok) {
      const data = await res.json();
      fetchProjects(data.session_id);
      setPage("traces");
    }
    return res.ok;
  };

  const handleLogout = async () => {
    await fetch("/logout", { method: "POST" });
    setProjects([]);
    setActiveProject("");
    setTimeRange("1d");
    setEnvironment("default");
    setPage("login");
  };

  const handleNavigate = (id: string) => {
    const route = NAV_MAP[id];
    if (route) setPage(route);
  };

  // ── Login ──
  if (page === "login") {
    return <LoginPage onLogin={handleLogin} />;
  }

  // ── Authenticated Layout ──
  return (
    <div className="flex flex-col h-screen" style={{ background: colors.backgrounds.abyssBlack }}>
      {/* Header */}
      <Header
        activeProject={activeProject}
        projects={projects}
        onProjectChange={setActiveProject}
        timeRange={timeRange}
        onTimeRangeChange={setTimeRange}
        environment={environment}
        onEnvironmentChange={setEnvironment}
        onLogout={handleLogout}
      />

      {/* Main Content */}
      <Layout activeNav={page === "trace-detail" ? "traces" : page} onNavigate={handleNavigate}>
        <ToastProvider>
          <div style={{ background: colors.backgrounds.abyssBlack }}>
            {page === "dashboard" && (
              <DashboardPage activeProject={activeProject} />
            )}
            {page === "traces" && (
              <TracesPage
                activeProject={activeProject}
                onNavigateToTrace={setActiveTraceId}
                onNavigateToTraceDetail={() => setPage("trace-detail")}
              />
            )}
            {page === "trace-detail" && (
              <TraceDetailPage
                traceId={activeTraceId}
                activeProject={activeProject}
                onBack={() => setPage("traces")}
              />
            )}
            {page === "prompts" && (
              <PromptsPage activeProject={activeProject} />
            )}
            {page !== "dashboard" &&
              page !== "traces" &&
              page !== "trace-detail" &&
              page !== "prompts" && (
                <div className="flex flex-col items-center justify-center h-[60vh]">
                  <svg
                    width="48"
                    height="48"
                    viewBox="0 0 48 48"
                    fill="none"
                    className="mb-4 text-lantern-bg-cave"
                  >
                    <path
                      d="M24 4l20 12v20c0 2-4 8-20 16C8 44 4 38 4 36V16L24 4z"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinejoin="round"
                    />
                    <path d="M24 16v16M16 24h16" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
                  </svg>
                  <h2 className="text-lg font-medium text-lantern-pure">Coming Soon</h2>
                  <p className="text-sm text-lantern-ash mt-1">
                    This page is under development
                  </p>
                </div>
              )}
          </div>
        </ToastProvider>
      </Layout>
    </div>
  );
}
