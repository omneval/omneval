import { useState, useEffect, useCallback, useRef } from "react";
import LoginPage from "./pages/Login";
import TracesPage from "./pages/Traces";
import TraceDetailPage from "./pages/TraceDetail";
import ConversationDetailPage from "./pages/ConversationDetail";
import ConversationsPage from "./pages/Conversations";
import DashboardPage from "./pages/Dashboard";
import PromptsPage from "./pages/Prompts";
import DatasetDetailPage from "./pages/DatasetDetail";
import DatasetsPage from "./pages/Datasets";
import SettingsPage, { NewProjectModal } from "./pages/Settings";
import EvalRulesPage from "./pages/EvalRules";
import AdminPage from "./pages/Admin";
import Header from "./components/Header";
import Layout from "./components/Layout";
import { ToastProvider } from "./components/Toast";
import { colors } from "./theme";
import { ErrorBoundary } from "./components/ErrorBanner";

/** localStorage key used to persist the user's active project selection. */
const ACTIVE_PROJECT_KEY = "omneval_active_project";

type Page =
  | "login"
  | "traces"
  | "trace-detail"
  | "conversation-detail"
  | "conversations"
  | "dashboard"
  | "prompts"
  | "datasets"
  | "dataset-detail"
  | "settings"
  | "eval-rules"
  | "admin";

const NAV_MAP: Record<string, Page> = {
  dashboard: "dashboard",
  traces: "traces",
  conversations: "conversations",
  prompts: "prompts",
  datasets: "datasets",
  "dataset-detail": "dataset-detail",
  settings: "settings",
  "eval-rules": "eval-rules",
  admin: "admin",
} as const;

// Reverse map: Page → URL path segment
const PAGE_TO_PATH: Record<Page, string> = {
  login: "",
  dashboard: "",
  traces: "traces",
  "trace-detail": "traces",
  "conversation-detail": "traces",
  conversations: "conversations",
  prompts: "prompts",
  datasets: "datasets",
  "dataset-detail": "datasets",
  settings: "settings",
  "eval-rules": "eval-rules",
  admin: "admin",
};

/** Derive the intended page from the current URL path. Falls back to "dashboard". */
function pageFromPathname(pathname: string): Page {
  const segment = pathname.replace(/^\//, "").split("/")[0];
  return NAV_MAP[segment] ?? "dashboard";
}

interface Project {
  project_id: string;
  name: string;
  org_id: string;
}

interface MeResponse {
  user_id: string;
  email: string;
  projects: Array<{ project_id: string; name: string }>;
}

export default function App() {
  const [page, setPage] = useState<Page>("login");
  const [projects, setProjects] = useState<Project[]>([]);
  const [activeProject, setActiveProject] = useState<string>("");
  const [activeTraceId, setActiveTraceId] = useState<string>("");
  const [traceDetailOpen, setTraceDetailOpen] = useState(false);
  const [activeDatasetId, setActiveDatasetId] = useState<string>("");
  const [activeConversationId, setActiveConversationId] = useState<string>("");
  // Where TraceDetail's back button leads (issue #67).
  const [_traceDetailReturnTo, setTraceDetailReturnTo] = useState<Page>("traces");

  // Which page the user came from before reaching ConversationDetail, so the back button returns to the correct source page.
  const [conversationDetailReturnTo, setConversationDetailReturnTo] = useState<Page>("conversations");

  const [timeRange, setTimeRange] = useState("1d");
  const [showNewProject, setShowNewProject] = useState(false);

  // Persist active project to localStorage whenever it changes
  const persistActiveProject = useCallback((projectId: string) => {
    try {
      localStorage.setItem(ACTIVE_PROJECT_KEY, projectId);
    } catch {
      // localStorage may be unavailable in some environments
    }
  }, []);

  // Restore active project from localStorage; falls back to first available
  const resolveActiveProject = useCallback(
    (defaultProjectId: string, availableProjects: Project[]) => {
      if (availableProjects.length === 0) return "";

      let stored: string | null = null;
      try {
        stored = localStorage.getItem(ACTIVE_PROJECT_KEY);
      } catch {
        // localStorage may be unavailable in some environments
      }

      // Use stored project if it still exists in available projects
      if (stored && availableProjects.some((p) => p.project_id === stored)) {
        return stored;
      }
      // Fall back to the default project or first available
      if (availableProjects.some((p) => p.project_id === defaultProjectId)) {
        return defaultProjectId;
      }
      return availableProjects[0].project_id;
    },
    []
  );

  // Detect an existing session on mount. We call GET /api/v1/me instead of
  // reading document.cookie because the omneval_session cookie is HttpOnly.
  const hasPerformedInitialSessionCheck = useRef(false);

  useEffect(() => {
    if (hasPerformedInitialSessionCheck.current) return;
    hasPerformedInitialSessionCheck.current = true;

    fetch("/api/v1/me")
      .then((res) => {
        if (res.ok) return res.json() as Promise<MeResponse>;
        throw new Error("unauthorized");
      })
      .then((data) => {
        if (Array.isArray(data.projects) && data.projects.length > 0) {
          // The /me endpoint does not include org_id; set it to empty.
          const projects: Project[] = data.projects.map((p) => ({
            project_id: p.project_id,
            name: p.name,
            org_id: "",
          }));
          setProjects(projects);
          const resolved = resolveActiveProject(projects[0].project_id, projects);
          setActiveProject(resolved);
          persistActiveProject(resolved);
        } else {
          // No projects on /me — fetch them via the normal projects endpoint.
          fetchProjects("fallback-session-id");
        }
        // Navigate to the page that matches the current URL path on hard load,
        // so that refreshing or deep-linking to /traces, /prompts, etc. works.
        setPage(pageFromPathname(window.location.pathname));
      })
      .catch(() => {
        // No valid session — the page stays on login.
      });
  }, []);

  // React to browser back/forward: re-derive the page from the URL whenever
  // a popstate event fires. Without this, pushState-based navigation makes
  // the back button change the URL but not the rendered page.
  useEffect(() => {
    const onPopState = () => {
      setPage((current) =>
        current === "login" ? current : pageFromPathname(window.location.pathname)
      );
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  const fetchProjects = useCallback(async (_session: string) => {
    const res = await fetch("/api/v1/projects");
    if (res.ok) {
      const data = await res.json();
      if (Array.isArray(data)) {
        setProjects(data);
        const resolved = resolveActiveProject(data[0]?.project_id ?? "", data);
        setActiveProject(resolved);
        persistActiveProject(resolved);
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
      setPage("dashboard");
    }
    return res.ok;
  };

  const handleLogout = async () => {
    await fetch("/logout", { method: "POST" });
    try {
      localStorage.removeItem(ACTIVE_PROJECT_KEY);
    } catch {
      // localStorage may be unavailable
    }
    setProjects([]);
    setActiveProject("");
    setTimeRange("1d");
    setPage("login");
  };

  const handleNewProject = useCallback((project: Project) => {
    setProjects((prev) => [...prev, project]);
  }, []);

  const handleNewProjectTrigger = () => setShowNewProject(true);

  const handleNavigate = (id: string) => {
    const route = NAV_MAP[id];
    if (route) {
      setPage(route);
      // Keep the URL in sync so the back button works and hard-reloading
      // the page returns to the same section.
      const pathSegment = PAGE_TO_PATH[route];
      const newPath = pathSegment ? "/" + pathSegment : "/";
      window.history.pushState({}, "", newPath);
    }
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
      <div className="flex flex-col h-screen" style={{ background: colors.backgrounds.voidBlack }}>
      {/* Header */}
      <Header
        activeProject={activeProject}
        projects={projects}
        onProjectChange={(p) => {
          setActiveProject(p);
          persistActiveProject(p);
        }}
        onNewProject={handleNewProjectTrigger}
        timeRange={timeRange}
        onTimeRangeChange={setTimeRange}
      />

      {/* Main Content */}
      <Layout
        activeNav={
          page === "trace-detail" || page === "conversation-detail"
            ? "traces"
            : page
        }
        onNavigate={handleNavigate}
        onLogout={handleLogout}
      >
        <ToastProvider>
          <div style={{ background: colors.backgrounds.voidBlack }}>
            {showNewProject && (
              <NewProjectModal
                onClose={() => setShowNewProject(false)}
                onCreated={handleNewProject}
              />
            )}
            {page === "dashboard" && (
              <DashboardPage activeProject={activeProject} timeRange={timeRange} />
            )}
            {page === "traces" && (
              <>
                <TracesPage
                  activeProject={activeProject}
                  timeRange={timeRange}
                  onNavigateToTrace={setActiveTraceId}
                  onNavigateToTraceDetail={() => {
                    setTraceDetailReturnTo("traces");
                    setTraceDetailOpen(true);
                  }}
                  traceDetailOpen={traceDetailOpen}
                  activeTraceId={activeTraceId}
                  setActiveTraceId={setActiveTraceId}
                  onTraceDetailClose={() => setTraceDetailOpen(false)}
                />
                {traceDetailOpen && (
                  <div className="fixed inset-0 z-50 flex justify-end" role="dialog" aria-modal="true">
                    <div
                      className="fixed inset-0 bg-black/50"
                      onClick={() => setTraceDetailOpen(false)}
                    />
                    <div className="relative h-full w-full max-w-[90vw] bg-omneval-background-caveWall shadow-2xl">
                      <TraceDetailPage
                        traceId={activeTraceId}
                        activeProject={activeProject}
                        onClose={() => setTraceDetailOpen(false)}
                      />
                    </div>
                  </div>
                )}
              </>
            )}
            {page === "conversations" && (
              <ConversationsPage
                activeProject={activeProject}
                onNavigateToConversation={(conversationId) => {
                  setActiveConversationId(conversationId);
                  setConversationDetailReturnTo("conversations");
                  setPage("conversation-detail");
                }}
              />
            )}
            {page === "conversation-detail" && activeConversationId && (
              <ConversationDetailPage
                conversationId={activeConversationId}
                activeProject={activeProject}
                onBack={() => setPage(conversationDetailReturnTo)}
                onNavigateToTrace={setActiveTraceId}
                onNavigateToTraceDetail={() => {
                  setTraceDetailReturnTo("conversation-detail");
                  setPage("trace-detail");
                }}
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
            {page === "admin" && (
              <AdminPage
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
