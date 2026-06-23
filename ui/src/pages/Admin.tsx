import { useState, useEffect, useCallback, useMemo } from "react";
import { useToast } from "@/components/Toast";
import { Skeleton } from "@/components/Skeleton";
// ── Types ──────────────────────────────────────────────────────────

interface APIKey {
  key_id: string;
  kind: "project" | "service";
  service_name?: string;
  name?: string;
  created_at: string;
  revoked_at?: string;
}

// keyDisplayName returns the best human-readable label for an API key:
// the user-supplied name (or service_name for service keys) if set,
// otherwise a fallback derived from the truncated key ID (#143).
function keyDisplayName(keyData: APIKey): string {
  if (keyData.name) return keyData.name;
  if (keyData.kind === "service" && keyData.service_name) return keyData.service_name;
  const suffix = keyData.key_id.slice(-4);
  return keyData.kind === "service" ? `Service Key (...${suffix})` : `Project Key (...${suffix})`;
}

interface Project {
  project_id: string;
  name: string;
  org_id: string;
}

interface TraceCount {
  count: number;
}

interface OpsMetrics {
  ingest_queue_depth?: number;
}

// ── Ops metric card ─────────────────────────────────────────────────

function MetricCard({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-md border border-omneval-border bg-omneval-surface px-4 py-3">
      <dt className="text-xs font-medium text-omneval-text-muted uppercase tracking-wide">
        {label}
      </dt>
      <dd className="mt-1 text-2xl font-semibold text-omneval-text-pure tabular-nums">
        {value}
      </dd>
    </div>
  );
}

// ── Components ─────────────────────────────────────────────────────

function TabButton({
  label,
  active,
  onClick,
  badge,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
  badge?: string;
}) {
  return (
    <button
      onClick={onClick}
      className={`px-4 py-2 text-sm font-medium rounded-t-md transition-colors inline-flex items-center gap-2 ${
        active
          ? "text-omneval-violet-pale bg-omneval-violet-active border-b-2 border-omneval-violet"
          : "text-omneval-text-muted hover:text-omneval-text-pure hover:bg-omneval-violet-hover"
      }`}
    >
      <span>{label}</span>
      {badge && (
        <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
          {badge}
        </span>
      )}
    </button>
  );
}

function ConfirmDialog({
  title,
  message,
  confirmLabel,
  onConfirm,
  onCancel,
  danger = false,
}: {
  title: string;
  message: string;
  confirmLabel: string;
  onConfirm: () => void;
  onCancel: () => void;
  danger?: boolean;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div
        className="bg-omneval-depth border border-omneval-border rounded-lg p-6 w-full max-w-sm mx-4"
        style={{ boxShadow: "0 16px 48px rgba(0,0,0,0.5)" }}
      >
        <h3 className="text-lg font-medium text-omneval-text-pure mb-2">
          {title}
        </h3>
        <p className="text-sm text-omneval-text-muted mb-4">{message}</p>
        <div className="flex gap-3">
          <button
            onClick={onConfirm}
            className={`flex-1 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
              danger
                ? "text-white bg-omneval-violet hover:bg-omneval-violet"
                : "bg-omneval-violet-active text-omneval-violet-pale hover:bg-omneval-violet"
            }`}
          >
            {confirmLabel}
          </button>
          <button
            onClick={onCancel}
            className="flex-1 px-3 py-2 rounded-md text-sm bg-omneval-surface border border-omneval-border text-omneval-text-muted hover:text-omneval-text-pure transition-colors"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}

function SectionHeader({ title, description }: { title: string; description: string }) {
  return (
    <div className="mb-4">
      <h2 className="text-lg font-medium text-omneval-text-pure">{title}</h2>
      <p className="text-sm text-omneval-text-muted mt-1">{description}</p>
    </div>
  );
}

// ── Admin Keys Section ─────────────────────────────────────────────

function AdminKeysSection({
  allKeys,
  loading,
  onDeleteKey,
}: {
  allKeys: APIKey[];
  loading: boolean;
  onDeleteKey: (keyId: string) => void;
}) {
  const [deleteKeyId, setDeleteKeyId] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [sortOrder, setSortOrder] = useState<"newest" | "oldest">("newest");

  const handleDelete = async () => {
    if (!deleteKeyId) return;
    await onDeleteKey(deleteKeyId);
    setDeleteKeyId(null);
  };

  const totalKeys = allKeys.length;

  const filteredKeys = useMemo(() => {
    const query = search.trim().toLowerCase();
    let keys = allKeys;
    if (query) {
      keys = keys.filter((k) => {
        const name = (
          k.service_name ?? (k.kind === "service" ? "Service Key" : "Project Key")
        ).toLowerCase();
        return (
          k.key_id.toLowerCase().includes(query) || name.includes(query)
        );
      });
    }
    const sorted = [...keys].sort((a, b) => {
      const aTime = new Date(a.created_at).getTime();
      const bTime = new Date(b.created_at).getTime();
      return sortOrder === "newest" ? bTime - aTime : aTime - bTime;
    });
    return sorted;
  }, [allKeys, search, sortOrder]);

  const projectKeys = filteredKeys.filter((k) => k.kind === "project");
  const serviceKeys = filteredKeys.filter((k) => k.kind === "service");

  if (loading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-16 rounded-md" />
        ))}
      </div>
    );
  }

  const toggleSortOrder = () => {
    setSortOrder((prev) => (prev === "newest" ? "oldest" : "newest"));
  };

  return (
    <div>
      <SectionHeader
        title="API Keys"
        description={`Delete project or service keys (${totalKeys} total). Revoked keys cannot be recovered.`}
      />

      {totalKeys > 0 && (
        <div className="flex items-center gap-2 mb-4">
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search by name or key ID..."
            className="flex-1 px-3 py-2 text-sm rounded-md border border-omneval-border bg-omneval-surface text-omneval-text-pure placeholder:text-omneval-text-muted focus:outline-none focus:ring-1 focus:ring-omneval-violet"
          />
          <button
            type="button"
            onClick={toggleSortOrder}
            className="px-3 py-2 text-sm rounded-md border border-omneval-border bg-omneval-surface text-omneval-text-mid hover:text-omneval-text-pure transition-colors whitespace-nowrap"
            title="Toggle sort order by Created date"
          >
            Created {sortOrder === "newest" ? "↓" : "↑"}
          </button>
        </div>
      )}

      {totalKeys === 0 ? (
        <div className="text-sm text-omneval-text-muted py-8 text-center bg-omneval-depth/30 rounded-md border border-omneval-border">
          No API keys found.
        </div>
      ) : filteredKeys.length === 0 ? (
        <div className="text-sm text-omneval-text-muted py-8 text-center bg-omneval-depth/30 rounded-md border border-omneval-border">
          No matching API keys found.
        </div>
      ) : (
        <div className="space-y-3">
          {projectKeys.length > 0 && (
            <div>
              <h3 className="text-sm font-medium text-omneval-text-mid mb-2">
                Project Keys ({projectKeys.length})
              </h3>
              <div className="space-y-2">
                {projectKeys.map((k) => (
                  <KeyRow
                    key={k.key_id}
                    keyData={k}
                    onDelete={() => setDeleteKeyId(k.key_id)}
                  />
                ))}
              </div>
            </div>
          )}

          {serviceKeys.length > 0 && (
            <div className="mt-4">
              <h3 className="text-sm font-medium text-omneval-text-mid mb-2">
                Service Keys ({serviceKeys.length})
              </h3>
              <div className="space-y-2">
                {serviceKeys.map((k) => (
                  <KeyRow
                    key={k.key_id}
                    keyData={k}
                    onDelete={() => setDeleteKeyId(k.key_id)}
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {deleteKeyId && (
        <ConfirmDialog
          title="Delete API Key"
          message="Are you sure you want to delete this API key? This action cannot be undone."
          confirmLabel="Delete Key"
          onConfirm={handleDelete}
          onCancel={() => setDeleteKeyId(null)}
          danger
        />
      )}
    </div>
  );
}

function KeyRow({
  keyData,
  onDelete,
}: {
  keyData: APIKey;
  onDelete: () => void;
}) {
  return (
    <div className="flex items-center justify-between px-4 py-3 rounded-md border border-omneval-border bg-omneval-surface">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-0.5">
          <span className="text-sm font-medium text-omneval-text-pure truncate">
            {keyDisplayName(keyData)}
          </span>
          <span
            className={`text-xs px-1.5 py-0.5 rounded font-medium flex-shrink-0 ${
              keyData.kind === "service"
                ? "text-omneval-violet-pale bg-omneval-violet-active"
                : "text-omneval-text-muted bg-omneval-border"
            }`}
          >
            {keyData.kind === "service" ? "Service" : "Project"}
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-mono text-omneval-text-muted truncate">
            {keyData.key_id}
          </span>
        </div>
        <p className="text-xs text-omneval-text-muted mt-0.5">
          Created: {new Date(keyData.created_at).toLocaleDateString()}
        </p>
      </div>
      <button
        onClick={onDelete}
        className="ml-3 btn-destructive text-xs py-1"
      >
        Delete
      </button>
    </div>
  );
}

// ── Admin Projects Section ─────────────────────────────────────────

function AdminProjectsSection({
  projects,
  loading,
  onDeleteProject,
}: {
  projects: Project[];
  loading: boolean;
  onDeleteProject: (projectId: string, projectName: string) => void;
}) {
  const [deleteProjectId, setDeleteProjectId] = useState<string | null>(null);

  const handleDelete = async () => {
    if (!deleteProjectId) return;
    const project = projects.find((p) => p.project_id === deleteProjectId);
    if (!project) {
      setDeleteProjectId(null);
      return;
    }
    await onDeleteProject(deleteProjectId, project.name);
    setDeleteProjectId(null);
  };

  if (loading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-16 rounded-md" />
        ))}
      </div>
    );
  }

  return (
    <div>
      <SectionHeader
        title="Projects"
        description={`Manage and delete projects (${projects.length} total). Deleting a project removes all its traces and keys.`}
      />

      {projects.length === 0 ? (
        <div className="text-sm text-omneval-text-muted py-8 text-center bg-omneval-depth/30 rounded-md border border-omneval-border">
          No projects found.
        </div>
      ) : (
        <div className="space-y-2">
          {projects.map((p) => (
            <ProjectRow
              key={p.project_id}
              project={p}
              onDelete={() => setDeleteProjectId(p.project_id)}
            />
          ))}
        </div>
      )}

      {deleteProjectId && (
        <ConfirmDialog
          title="Delete Project"
          message={`Are you sure you want to delete "${
            projects.find((p) => p.project_id === deleteProjectId)?.name || ""
          }"? This will remove all its traces and API keys.`}
          confirmLabel="Delete Project"
          onConfirm={handleDelete}
          onCancel={() => setDeleteProjectId(null)}
          danger
        />
      )}
    </div>
  );
}

function ProjectRow({
  project,
  onDelete,
}: {
  project: Project;
  onDelete: () => void;
}) {
  return (
    <div className="flex items-center justify-between px-4 py-3 rounded-md border border-omneval-border bg-omneval-surface">
      <div>
        <span className="text-sm font-medium text-omneval-text-pure">
          {project.name}
        </span>
        <div className="text-xs text-omneval-text-muted font-mono mt-0.5">
          {project.project_id}
        </div>
      </div>
      <button
        onClick={onDelete}
        className="ml-3 btn-destructive text-xs py-1"
      >
        Delete
      </button>
    </div>
  );
}

// ── Admin Traces Section ───────────────────────────────────────────

function AdminTracesSection({
  activeProject,
  traceCount,
  loading,
  onDeleteAllTraces,
}: {
  activeProject: string;
  traceCount: TraceCount | null;
  loading: boolean;
  onDeleteAllTraces: () => void;
}) {
  const [confirmDelete, setConfirmDelete] = useState(false);

  const handleDelete = async () => {
    await onDeleteAllTraces();
    setConfirmDelete(false);
  };

  if (loading) {
    return <Skeleton className="h-24 rounded-md" />;
  }

  const count = traceCount?.count ?? 0;

  return (
    <div>
      <SectionHeader
        title="Traces"
        description={`Delete all traces for the active project (${count} traces). This is a destructive action.`}
      />

      <div className="flex items-center justify-between px-4 py-4 rounded-md border border-omneval-border bg-omneval-surface">
        <div>
          <p className="text-sm text-omneval-text-pure font-medium">
            Active Project
          </p>
          <p className="text-xs text-omneval-text-muted font-mono mt-0.5">
            {activeProject || "No project selected"}
          </p>
          <p className="text-xs text-omneval-text-muted mt-1">
            Total traces: {count}
          </p>
        </div>
        <button
          onClick={() => setConfirmDelete(true)}
          className="btn-destructive px-4 py-2 text-sm"
        >
          Delete All Traces
        </button>
      </div>

      {confirmDelete && (
        <ConfirmDialog
          title="Delete All Traces"
          message={`This will permanently delete all ${count} traces for this project. This action cannot be undone.`}
          confirmLabel="Yes, Delete All"
          onConfirm={handleDelete}
          onCancel={() => setConfirmDelete(false)}
          danger
        />
      )}
    </div>
  );
}

// ── Admin Ops Section (read-only health metrics) ───────────────────

function AdminOpsSection({
  metrics,
  loading,
}: {
  metrics: OpsMetrics | null;
  loading: boolean;
}) {
  if (loading) {
    return <Skeleton className="h-24 rounded-md" />;
  }

  if (!metrics) {
    return (
      <div className="text-sm text-omneval-text-muted py-8 text-center bg-omneval-depth/30 rounded-md border border-omneval-border">
        No metrics available.
      </div>
    );
  }

  const { ingest_queue_depth } = metrics;

  return (
    <div>
      <SectionHeader
        title="Operations"
        description="Read-only system health metrics. No destructive actions here."
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <MetricCard
          label="Ingest Queue Depth"
          value={ingest_queue_depth ?? "Unavailable"}
        />
      </div>
    </div>
  );
}

// ── Admin Page ─────────────────────────────────────────────────────

export default function AdminPage({
  activeProject,
}: {
  activeProject: string;
}) {
  const { addToast } = useToast();
  const [activeTab, setActiveTab] = useState<"keys" | "projects" | "traces" | "ops">("keys");

  // Keys
  const [allKeys, setAllKeys] = useState<APIKey[]>([]);
  const [keysLoading, setKeysLoading] = useState(true);

  // Projects
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectsLoading, setProjectsLoading] = useState(true);

  // Traces
  const [traceCount, setTraceCount] = useState<TraceCount | null>(null);
  const [tracesLoading, setTracesLoading] = useState(true);

  // Ops
  const [opsMetrics, setOpsMetrics] = useState<OpsMetrics | null>(null);
  const [opsLoading, setOpsLoading] = useState(true);

  const fetchAll = useCallback(async () => {
    // Fetch API keys across all projects
    setKeysLoading(true);
    try {
      const res = await fetch("/api/v1/admin/api-keys");
      if (res.ok) {
        const data = await res.json();
        setAllKeys(Array.isArray(data) ? data : []);
      }
    } catch {
      // Silently ignore
    } finally {
      setKeysLoading(false);
    }

    // Fetch projects
    setProjectsLoading(true);
    try {
      const res = await fetch("/api/v1/projects");
      if (res.ok) {
        const data = await res.json();
        setProjects(Array.isArray(data) ? data : []);
      }
    } catch {
      // Silently ignore
    } finally {
      setProjectsLoading(false);
    }

    // Fetch trace count for active project
    setTracesLoading(true);
    try {
      const res = await fetch(`/api/v1/admin/traces/${activeProject}/count`);
      if (res.ok) {
        const data = await res.json();
        setTraceCount(data);
      } else {
        setTraceCount({ count: 0 });
      }
    } catch {
      setTraceCount({ count: 0 });
    } finally {
      setTracesLoading(false);
    }

    // Fetch ops metrics
    setOpsLoading(true);
    try {
      const res = await fetch("/api/v1/admin/ops");
      if (res.ok) {
        const data = await res.json();
        setOpsMetrics(data);
      } else {
        setOpsMetrics(null);
      }
    } catch {
      setOpsMetrics(null);
    } finally {
      setOpsLoading(false);
    }
  }, [activeProject]);

  useEffect(() => {
    fetchAll();
  }, [fetchAll]);

  const handleDeleteKey = async (keyId: string) => {
    try {
      const res = await fetch(`/api/v1/admin/api-keys/${keyId}`, {
        method: "DELETE",
      });
      if (res.ok) {
        addToast("success", "API key deleted");
        await fetchAll();
      } else {
        const err = await res.json().catch(() => ({}));
        addToast("error", err.error || "Failed to delete API key");
      }
    } catch {
      addToast("error", "Failed to delete API key");
    }
  };

  const handleDeleteProject = async (projectId: string, projectName: string) => {
    try {
      const res = await fetch(`/api/v1/admin/projects/${projectId}`, {
        method: "DELETE",
      });
      if (res.ok) {
        addToast("success", `Project "${projectName}" deleted`);
        await fetchAll();
      } else {
        const err = await res.json().catch(() => ({}));
        addToast("error", err.error || "Failed to delete project");
      }
    } catch {
      addToast("error", "Failed to delete project");
    }
  };

  const handleDeleteAllTraces = async () => {
    try {
      const res = await fetch(`/api/v1/admin/traces/${activeProject}`, {
        method: "DELETE",
      });
      if (res.ok) {
        addToast("success", "All traces deleted");
        await fetchAll();
      } else {
        const err = await res.json().catch(() => ({}));
        addToast("error", err.error || "Failed to delete traces");
      }
    } catch {
      addToast("error", "Failed to delete traces");
    }
  };

  const tabs = useMemo(
    () => [
      { id: "keys" as const, label: "API Keys", badge: undefined as string | undefined },
      { id: "projects" as const, label: "Projects", badge: undefined as string | undefined },
      { id: "traces" as const, label: "Traces", badge: undefined as string | undefined },
      { id: "ops" as const, label: "Ops", badge: "Read-only" },
    ],
    []
  );

  return (
    <div className="flex flex-col gap-8 py-6 px-6">
      {/* Tab Navigation */}
      <div className="flex gap-0 border-b border-omneval-border">
        {tabs.map((tab) => (
          <TabButton
            key={tab.id}
            label={tab.label}
            active={activeTab === tab.id}
            onClick={() => setActiveTab(tab.id)}
            badge={tab.badge}
          />
        ))}
      </div>

      {/* Tab Content */}
      {activeTab === "keys" && (
        <AdminKeysSection
          allKeys={allKeys}
          loading={keysLoading}
          onDeleteKey={handleDeleteKey}
        />
      )}
      {activeTab === "projects" && (
        <AdminProjectsSection
          projects={projects}
          loading={projectsLoading}
          onDeleteProject={handleDeleteProject}
        />
      )}
      {activeTab === "traces" && (
        <AdminTracesSection
          activeProject={activeProject}
          traceCount={traceCount}
          loading={tracesLoading}
          onDeleteAllTraces={handleDeleteAllTraces}
        />
      )}
      {activeTab === "ops" && (
        <AdminOpsSection
          metrics={opsMetrics}
          loading={opsLoading}
        />
      )}
    </div>
  );
}
