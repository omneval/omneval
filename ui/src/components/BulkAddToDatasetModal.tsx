import { useState, useEffect } from "react";
import { colors } from "@/theme";

// ── Types ──────────────────────────────────────────────────────────

interface DatasetItem {
  dataset_id: string;
  name: string;
  created_at: string;
  item_count: number;
}

interface SpanData {
  span_id: string;
  input: string;
  output: string;
}

interface BulkAddToDatasetModalProps {
  spanIds: string[];
  onClose: () => void;
  onSuccess: () => void;
}

// ── Modal Component ────────────────────────────────────────────────

export default function BulkAddToDatasetModal({
  spanIds,
  onClose,
  onSuccess,
}: BulkAddToDatasetModalProps) {
  const [datasets, setDatasets] = useState<DatasetItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedDatasetId, setSelectedDatasetId] = useState("");
  const [showCreateNew, setShowCreateNew] = useState(false);
  const [newDatasetName, setNewDatasetName] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [fetchedSpans, setFetchedSpans] = useState<SpanData[]>([]);

  // Load existing datasets and span data on mount
  useEffect(() => {
    fetchDatasets();
    fetchSpanData();
  }, []);

  const fetchDatasets = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch("/api/v1/datasets");
      if (res.ok) {
        const data = await res.json();
        setDatasets(data.datasets ?? []);
        if (data.datasets?.length === 1) {
          setSelectedDatasetId(data.datasets[0].dataset_id);
        }
      }
    } catch {
      setError("Failed to load datasets");
    } finally {
      setLoading(false);
    }
  };

  const fetchSpanData = async () => {
    try {
      const res = await fetch("/api/v1/spans/batch", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ span_ids: spanIds }),
      });
      if (res.ok) {
        const data = await res.json();
        setFetchedSpans(data.spans ?? []);
      }
    } catch {
      // If we can't fetch span data, we'll still allow saving with just the span IDs
    }
  };

  const handleSave = async () => {
    if (!selectedDatasetId) return;
    setSaving(true);
    setError(null);
    try {
      const spanIdToData = new Map(fetchedSpans.map((s) => [s.span_id, s]));
      const items = spanIds.map((spanId) => {
        const spanData = spanIdToData.get(spanId);
        return {
          input: spanData?.input ?? "",
          expected_output: spanData?.output || undefined,
          source_span_id: spanId,
        };
      });

      const res = await fetch(
        `/api/v1/datasets/${selectedDatasetId}/items/batch`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ items }),
        },
      );
      if (res.ok) {
        const data: { created?: number; skipped?: { source_span_id?: string; reason: string }[] } =
          await res.json();
        const createdCount = data.created ?? 0;
        if (createdCount < items.length) {
          const skippedCount = data.skipped?.length ?? items.length - createdCount;
          setError(
            `Added ${createdCount} of ${items.length} spans — ${skippedCount} skipped (no input)`,
          );
        } else {
          onSuccess();
        }
      } else {
        const text = await res.text();
        setError(text || "Failed to save to dataset");
      }
    } catch {
      setError("Network error while saving");
    } finally {
      setSaving(false);
    }
  };

  const handleCreateDataset = async () => {
    if (!newDatasetName.trim()) return;
    setCreating(true);
    setCreateError(null);
    try {
      const res = await fetch("/api/v1/datasets", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: newDatasetName.trim() }),
      });
      if (res.ok) {
        const data = await res.json();
        setSelectedDatasetId(data.dataset_id);
        setShowCreateNew(false);
        setNewDatasetName("");
        await fetchDatasets();
      } else {
        const text = await res.text();
        setCreateError(text || "Failed to create dataset");
      }
    } catch {
      setCreateError("Network error while creating dataset");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ backgroundColor: "rgba(0, 0, 0, 0.7)" }}
      onClick={onClose}
    >
      <div
        className="flex flex-col w-full max-w-lg mx-4 rounded-xl border overflow-hidden"
        style={{
          backgroundColor: colors.backgrounds.depth,
          borderColor: colors.backgrounds.border,
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div
          className="flex items-center justify-between px-5 py-4 border-b"
          style={{ borderColor: colors.backgrounds.border }}
        >
          <div>
            <h2 className="text-sm font-semibold text-omneval-text-pure">
              Add to Dataset
            </h2>
            <p className="text-xs text-omneval-text-muted mt-0.5">
              {spanIds.length} span{spanIds.length !== 1 ? "s" : ""} selected
            </p>
          </div>
          <button
            onClick={onClose}
            className="p-1.5 rounded transition-colors"
            style={{ color: colors.typography.ashGrey }}
            onMouseEnter={(e) =>
              (e.currentTarget.style.backgroundColor = colors.backgrounds.surface)
            }
            onMouseLeave={(e) =>
              (e.currentTarget.style.backgroundColor = "transparent")
            }
            aria-label="Close"
          >
            <svg width="18" height="18" viewBox="0 0 16 16" fill="none">
              <path
                d="M4 4l8 8M12 4l-8 8"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
              />
            </svg>
          </button>
        </div>

        {/* Body */}
        <div className="px-5 py-4 space-y-4">
          {/* Dataset selection or create new */}
          {!showCreateNew ? (
            <>
              <div>
                <label className="text-xs font-medium text-omneval-text-muted mb-1 block">
                  Dataset
                </label>
                {loading ? (
                  <div className="text-xs text-omneval-text-muted py-2">
                    Loading datasets…
                  </div>
                ) : datasets.length === 0 ? (
                  <div className="text-xs text-omneval-text-muted py-2">
                    No datasets found. Create one to get started.
                  </div>
                ) : (
                  <select
                    value={selectedDatasetId}
                    onChange={(e) => setSelectedDatasetId(e.target.value)}
                    className="w-full text-xs px-3 py-2 rounded border outline-none"
                    style={{
                      backgroundColor: colors.backgrounds.voidBlack,
                      borderColor: colors.backgrounds.border,
                      color: colors.typography.pureLight,
                    }}
                  >
                    {datasets.map((ds) => (
                      <option key={ds.dataset_id} value={ds.dataset_id}>
                        {ds.name} ({ds.item_count} items)
                      </option>
                    ))}
                  </select>
                )}
              </div>

              {/* Create new dataset link */}
              <button
                onClick={() => setShowCreateNew(true)}
                className="text-xs text-omneval-violet-pale hover:underline"
              >
                + Create new dataset
              </button>

              {error && <div className="text-xs text-red-400">{error}</div>}
            </>
          ) : (
            /* Create new dataset form */
            <div className="space-y-3">
              <div>
                <label className="text-xs font-medium text-omneval-text-muted mb-1 block">
                  New Dataset Name
                </label>
                <input
                  type="text"
                  value={newDatasetName}
                  onChange={(e) => setNewDatasetName(e.target.value)}
                  placeholder="e.g., eval-prompts-v1"
                  className="w-full text-xs px-3 py-2 rounded border outline-none"
                  style={{
                    backgroundColor: colors.backgrounds.voidBlack,
                    borderColor: colors.backgrounds.border,
                    color: colors.typography.pureLight,
                  }}
                  autoFocus
                  onKeyDown={(e) => {
                    if (e.key === "Enter") handleCreateDataset();
                  }}
                />
              </div>

              <div className="flex gap-2">
                <button
                  onClick={handleCreateDataset}
                  disabled={!newDatasetName.trim() || creating}
                  className="text-xs px-3 py-1.5 rounded transition-colors disabled:opacity-50"
                  style={{
                    background: colors.accents.violet,
                    color: colors.typography.pureLight,
                  }}
                >
                  {creating ? "Creating…" : "Create"}
                </button>
                <button
                  onClick={() => {
                    setShowCreateNew(false);
                    setCreateError(null);
                  }}
                  className="text-xs px-3 py-1.5 rounded border transition-colors"
                  style={{
                    borderColor: colors.backgrounds.border,
                    color: colors.typography.ashGrey,
                  }}
                >
                  Cancel
                </button>
              </div>

              {createError && (
                <div className="text-xs text-red-400">{createError}</div>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        {!showCreateNew && datasets.length > 0 && (
          <div
            className="flex items-center justify-end gap-2 px-5 py-3 border-t"
            style={{ borderColor: colors.backgrounds.border }}
          >
            <button
              onClick={onClose}
              className="text-xs px-3 py-1.5 rounded border transition-colors"
              style={{
                borderColor: colors.backgrounds.border,
                color: colors.typography.ashGrey,
              }}
            >
              Cancel
            </button>
            <button
              onClick={handleSave}
              disabled={!selectedDatasetId || saving}
              className="text-xs px-3 py-1.5 rounded transition-colors disabled:opacity-50"
              style={{
                background: colors.accents.violet,
                color: colors.typography.pureLight,
              }}
            >
              {saving ? "Saving…" : "Save to Dataset"}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}