import { useState, useEffect } from "react";
import { colors } from "@/theme";
import { formatTime } from "@/utils/formatters";

// ── Types ──────────────────────────────────────────────────────────

interface DatasetListItem {
  dataset_id: string;
  name: string;
  created_at: string;
  item_count: number;
}

// ── Props ──────────────────────────────────────────────────────────

interface DatasetsPageProps {
  activeProject: string;
  onNavigateToDetail: (datasetId: string) => void;
}

// ── Component ──────────────────────────────────────────────────────

export default function DatasetsPage({ activeProject, onNavigateToDetail }: DatasetsPageProps) {
  const [datasets, setDatasets] = useState<DatasetListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showNewForm, setShowNewForm] = useState(false);
  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  const fetchDatasets = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(`/api/v1/datasets?project_id=${activeProject}`);
      if (!res.ok) throw new Error("Failed to fetch datasets");
      const data = await res.json();
      setDatasets(Array.isArray(data.datasets) ? data.datasets : []);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchDatasets();
  }, [activeProject]);

  const handleCreate = async () => {
    if (!newName.trim()) return;
    setCreating(true);
    setCreateError(null);
    try {
      const res = await fetch(`/api/v1/datasets?project_id=${activeProject}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: newName.trim() }),
      });
      if (!res.ok) {
        const text = await res.text();
        setCreateError(text || "Failed to create dataset");
        return;
      }
      setNewName("");
      setShowNewForm(false);
      await fetchDatasets();
    } catch {
      setCreateError("Network error while creating dataset");
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (datasetId: string) => {
    setDeleting(datasetId);
    setDeleteConfirmId(null);
    try {
      const res = await fetch(`/api/v1/datasets/${datasetId}?project_id=${activeProject}`, {
        method: "DELETE",
      });
      if (res.ok) {
        await fetchDatasets();
      }
    } catch {
      // silently fail
    } finally {
      setDeleting(null);
    }
  };

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b"
        style={{ borderColor: colors.backgrounds.caveWall }}
      >
        <h1 className="text-base font-semibold text-lantern-pure">Datasets</h1>
        <button
          onClick={() => setShowNewForm(true)}
          className="text-xs px-3 py-1.5 rounded transition-colors"
          style={{
            background: colors.accents.emberFlare,
            color: colors.typography.pureLight,
          }}
        >
          + New Dataset
        </button>
      </div>

      {/* Create Form */}
      {showNewForm && (
        <div className="px-6 py-3 border-b"
          style={{ borderColor: colors.backgrounds.caveWall }}
        >
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="Dataset name"
              className="text-xs px-3 py-1.5 rounded border outline-none flex-1"
              style={{
                backgroundColor: colors.backgrounds.abyssBlack,
                borderColor: colors.backgrounds.caveWall,
                color: colors.typography.pureLight,
              }}
              autoFocus
              onKeyDown={(e) => {
                if (e.key === "Enter") handleCreate();
                if (e.key === "Escape") {
                  setShowNewForm(false);
                  setNewName("");
                }
              }}
            />
            <button
              onClick={handleCreate}
              disabled={!newName.trim() || creating}
              className="text-xs px-3 py-1.5 rounded transition-colors disabled:opacity-50"
              style={{
                background: colors.accents.emberFlare,
                color: colors.typography.pureLight,
              }}
            >
              {creating ? "Creating…" : "Create"}
            </button>
            <button
              onClick={() => {
                setShowNewForm(false);
                setNewName("");
                setCreateError(null);
              }}
              className="text-xs px-3 py-1.5 rounded border transition-colors"
              style={{
                borderColor: colors.backgrounds.caveWall,
                color: colors.typography.ashGrey,
              }}
            >
              Cancel
            </button>
          </div>
          {createError && (
            <div className="text-xs text-red-400 mt-1">{createError}</div>
          )}
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-6">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" className="animate-spin">
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" strokeOpacity="0.25" />
              <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
            </svg>
          </div>
        ) : error ? (
          <div className="text-xs text-red-400 py-4 text-center">{error}</div>
        ) : datasets.length === 0 ? (
          <div className="text-xs text-lantern-ash py-8 text-center">
            No datasets found. Create one to evaluate model outputs.
          </div>
        ) : (
          <div className="space-y-2">
            {datasets.map((ds) => (
              <div
                key={ds.dataset_id}
                className="flex items-center justify-between rounded-lg border px-4 py-3 transition-colors group"
                style={{
                  borderColor: colors.backgrounds.caveWall,
                  backgroundColor: colors.backgrounds.abyssBlack,
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.backgroundColor = colors.backgrounds.slightIllumination;
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.backgroundColor = colors.backgrounds.abyssBlack;
                }}
              >
                <div className="flex-1 min-w-0 cursor-pointer" onClick={() => onNavigateToDetail(ds.dataset_id)}>
                  <div className="flex items-center gap-2">
                    <h3 className="text-sm font-medium text-lantern-pure truncate">
                      {ds.name}
                    </h3>
                  </div>
                  <p className="text-xs text-lantern-ash mt-0.5">
                    {ds.item_count} item{ds.item_count !== 1 ? "s" : ""} · {formatTime(ds.created_at)}
                  </p>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <button
                    onClick={() => onNavigateToDetail(ds.dataset_id)}
                    className="text-xs px-2 py-1 rounded transition-colors"
                    style={{
                      color: colors.typography.ashGrey,
                    }}
                    onMouseEnter={(e) => {
                      e.currentTarget.style.backgroundColor = colors.backgrounds.caveWall;
                      e.currentTarget.style.color = colors.typography.pureLight;
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.style.backgroundColor = "transparent";
                      e.currentTarget.style.color = colors.typography.ashGrey;
                    }}
                  >
                    View
                  </button>
                  {deleteConfirmId === ds.dataset_id ? (
                    <div className="flex items-center gap-1">
                      <button
                        onClick={() => handleDelete(ds.dataset_id)}
                        disabled={deleting === ds.dataset_id}
                        className="text-xs px-2 py-1 rounded transition-colors"
                        style={{
                          backgroundColor: "#F44336",
                          color: "#fff",
                        }}
                      >
                        {deleting === ds.dataset_id ? "…" : "Delete"}
                      </button>
                      <button
                        onClick={() => setDeleteConfirmId(null)}
                        className="text-xs px-2 py-1 rounded border"
                        style={{
                          borderColor: colors.backgrounds.caveWall,
                          color: colors.typography.ashGrey,
                        }}
                      >
                        Cancel
                      </button>
                    </div>
                  ) : (
                    <button
                      onClick={() => setDeleteConfirmId(ds.dataset_id)}
                      className="text-xs px-2 py-1 rounded transition-colors opacity-0 group-hover:opacity-100"
                      style={{
                        color: "#F44336",
                      }}
                    >
                      Delete
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
