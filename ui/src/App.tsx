import { useState, useEffect } from "react";
import LoginPage from "./pages/Login";
import TracesPage from "./pages/Traces";
import DashboardPage from "./pages/Dashboard";

type Page = "login" | "traces" | "dashboard";

export default function App() {
  const [page, setPage] = useState<Page>("login");
  const [projects, setProjects] = useState<
    { project_id: string; name: string; org_id: string }[]
  >([]);
  const [activeProject, setActiveProject] = useState<string>("");
  const [loadingProjects, setLoadingProjects] = useState(false);

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
    setLoadingProjects(true);
    try {
      const res = await fetch("/api/v1/projects");
      if (res.ok) {
        const data = await res.json();
        if (Array.isArray(data)) {
          setProjects(data);
          setActiveProject(
            data[0]?.project_id ?? "",
          );
        }
      }
    } finally {
      setLoadingProjects(false);
    }
  };

  const handleLogin = async (email: string, password: string) => {
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
    setPage("login");
  };

  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white border-b border-gray-200 px-4 py-2 flex items-center justify-between">
        <div className="font-semibold text-gray-900">Lantern</div>
        {page === "traces" && (
          <div className="flex items-center gap-3">
            <button
              onClick={() => setPage("dashboard")}
              className="text-sm text-gray-600 hover:text-gray-900"
            >
              Dashboard
            </button>
            <div className="flex items-center gap-2">
              {loadingProjects ? (
                <div className="text-sm text-gray-500">Loading...</div>
              ) : (
                <select
                  value={activeProject}
                  onChange={(e) => setActiveProject(e.target.value)}
                  className="text-sm border border-gray-300 rounded-md px-2 py-1 bg-white"
                >
                  {projects.map((p) => (
                    <option key={p.project_id} value={p.project_id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              )}
              <button
                onClick={() => setPage("traces")}
                className="text-sm text-gray-600 hover:text-gray-900"
              >
                Traces
              </button>
              <button
                onClick={handleLogout}
                className="text-sm text-gray-600 hover:text-gray-900"
              >
                Logout
              </button>
            </div>
          </div>
        )}
      </nav>
      <main className="max-w-6xl mx-auto p-4">
        {page === "login" && <LoginPage onLogin={handleLogin} />}
        {page === "traces" && (
          <TracesPage
            activeProject={activeProject}
            projects={projects}
          />
        )}
        {page === "dashboard" && <DashboardPage activeProject={activeProject} projects={projects} />}
      </main>
    </div>
  );
}
