import { useState, useEffect, useCallback } from "react";
import LoginPage from "./pages/Login";
import TracesPage from "./pages/Traces";
import TraceDetailPage from "./pages/TraceDetail";
import DashboardPage from "./pages/Dashboard";
import PromptsPage from "./pages/Prompts";
import DatasetDetailPage from "./pages/DatasetDetail";
import DatasetsPage from "./pages/Datasets";
import SettingsPage, { NewProjectModal } from "./pages/Settings";
import EvalRulesPage from "./pages/EvalRules";
import Header from "./components/Header";
import Layout from "./components/Layout";
import { ToastProvider } from "./components/Toast";
import { colors } from "./theme";
import { ErrorBoundary } from "./components/ErrorBanner";

type Page =
  | "login"
  | "traces"
  | "trace-detail"
  | "dashboard"
  | "prompts"
  | "datasets"
  | "dataset-detail"
  | "settings"
  | "eval-rules";

const NAV_MAP: Record<string, Page> = {
  dashboard: "dashboard",
  traces: "traces",
  prompts: "prompts",
  datasets: "datasets",
  "dataset-detail": "dataset-detail",
  settings: "settings",
  "eval-rules": "eval-rules",
};

interface Project {
  project_id: string;
  name: string;
  org_id: string;
}

export default function App() {
  const [page, setPage] = useState<Page>("login");
  const [projects, setProjects] = useState<Project[]>([]);
  const [activeProject, setActiveProject] = useState<string>("");
  const [activeTraceId, setActiveTraceId] = useState<string>("");
  const [activeDatasetId, setActiveDatasetId] = useState<string>("");

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

  const fetchProjects = useCallback(async (_session: string) => {
    const res = await fetch("/api/v1/projects");
    if (res.ok) {
      const data = await res.json();
      if (Array.isArray(data)) {
        setProjects(data);
        setActiveProject(data[0]?.project_id ?? "");
      }
    }
  }, []);

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

  const handleNewProject = useCallback((project: Project) => {
    setProjects((prev) => [...prev, project]);
  }, []);

  const [showNewProject, setShowNewProject] = useState(false);
  const handleNewProjectTrigger = () => setShowNewProject(true);

  const handleNavigate = (id: string) => {
    const route = NAV_MAP[id];
    if (route) setPage(route);
  };

  const handleNavigateToDataset = (datasetId: string) => {
    setActiveDatasetId(datasetId);
    setPage("dataset-detail");
  };

  // ── Login ──
  if (page === "login") {
    return <LoginPage onLogin={handleLogin} />;
  }

  // ── Authenticated Layout ──
  return (
    <ErrorBoundary>
      <div className="flex flex-col h-screen" style={{ background: colors.backgrounds.abyssBlack }}>
      {/* Header */}
      <Header
        activeProject={activeProject}
        projects={projects}
        onProjectChange={setActiveProject}
        onNewProject={handleNewProjectTrigger}
        timeRange={timeRange}
        onTimeRangeChange={setTimeRange}
        environment={environment}
        onEnvironmentChange={setEnvironment}
        onLogout={handleLogout}
      />

      {/* Main Content */}
      <Layout activeNav={page === "trace-detail" ? "traces" : page} onNavigate={handleNavigate} onLogout={handleLogout}>
        <ToastProvider>
          <div style={{ background: colors.backgrounds.abyssBlack }}>
            {showNewProject && (
              <NewProjectModal
                onClose={() => setShowNewProject(false)}
                onCreated={handleNewProject}
              />
            )}
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
            {page === "datasets" && (
              <DatasetsPage
                activeProject={activeProject}
                onNavigateToDetail={handleNavigateToDataset}
              />
            )}
            {page === "dataset-detail" && activeDatasetId && (
              <DatasetDetailPage
                datasetId={activeDatasetId}
                activeProject={activeProject}
                onBack={() => setPage("datasets")}
              />
            )}
            {page === "settings" && (
              <SettingsPage
                activeProject={activeProject}
              />
            )}
            {page === "eval-rules" && (
              <EvalRulesPage
                activeProject={activeProject}
              />
            )}
          </div>
        </ToastProvider>
      </Layout>
    </div>
    </ErrorBoundary>
  );
}
