import { useState, useEffect, useCallback } from "react";
import { colors } from "@/theme";
import Breadcrumb from "@/components/Breadcrumb";
import { formatTime } from "@/utils/formatters";
import { truncate } from "@/utils/formatters";

// ── Types ──────────────────────────────────────────────────────────

interface DatasetInfo {
  dataset_id: string;
  project_id: string;
  name: string;
  created_at: string;
  item_count: number;
}

interface DatasetItemEntry {
  item_id: string;
  dataset_id: string;
  source_span_id: string;
  input: string;
  expected_output: string;
  created_at: string;
}

interface RunListItem {
  run_id: string;
  eval_rule_id: string;
  status: string;
  item_count: number;
  mean_score: number;
  created_at: string;
}

interface RunDetail {
  run_id: string;
  dataset_id: string;
  eval_rule_id: string;
  status: string;
  created_at: string;
  items: RunItemEntry[];
}

interface RunItemEntry {
  item_id: string;
  input: string;
  expected_output: string;
  score: number;
  reasoning: string;
}

interface EvalRule {
  rule_id: string;
  name: string;
  judge_model: string;
  enabled: boolean;
}

interface ListEvalRulesResponse {
  rules: EvalRule[];
}

// ── Props ──────────────────────────────────────────────────────────

interface DatasetDetailProps {
  datasetId: string;
  activeProject: string;
  onBack: () => void;
}

// ── Status Badge ───────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const colorMap: Record<string, { bg: string; text: string }> = {
    pending: { bg: "rgba(161, 161, 170, 0.15)", text: "#A1A1AA" },
    running: { bg: "rgba(255, 87, 34, 0.15)", text: "#FF5722" },
    complete: { bg: "rgba(76, 175, 80, 0.15)", text: "#4CAF50" },
    error: { bg: "rgba(244, 67, 54, 0.15)", text: "#F44336" },
  };
  const c = colorMap[status] ?? colorMap.pending;
  return (
    <span
      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium"
      style={{ backgroundColor: c.bg, color: c.text }}
    >
      {status === "running" && <RunningDot />}
      {status.charAt(0).toUpperCase() + status.slice(1)}
    </span>
  );
}

function RunningDot() {
  return (
    <span className="inline-block w-1.5 h-1.5 rounded-full bg-lantern-ember animate-pulse" />
  );
}

// ── Spinner ────────────────────────────────────────────────────────

function Spinner({ size = 16 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      className="animate-spin"
    >
      <circle
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        strokeWidth="3"
        strokeOpacity="0.25"
      />
      <path
        d="M12 2a10 10 0 0 1 10 10"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
      />
    </svg>
  );
}

// ── New Run Modal ──────────────────────────────────────────────────

interface NewRunModalProps {
  datasetId: string;
  activeProject: string;
  onClose: () => void;
  onSuccess: () => void;
}

function NewRunModal({ datasetId, activeProject, onClose, onSuccess }: NewRunModalProps) {
  const [rules, setRules] = useState<EvalRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedRuleId, setSelectedRuleId] = useState("");
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchRules = async () => {
      try {
        const res = await fetch(`/api/v1/eval-rules?project_id=${activeProject}`);
        if (res.ok) {
          const data: ListEvalRulesResponse = await res.json();
          const activeRules = (data.rules ?? []).filter((r) => r.enabled);
          setRules(activeRules);
          if (activeRules.length === 1) {
            setSelectedRuleId(activeRules[0].rule_id);
          }
        }
      } catch {
        setError("Failed to load eval rules");
      } finally {
        setLoading(false);
      }
    };
    fetchRules();
  }, [activeProject]);

  const handleRun = async () => {
    if (!selectedRuleId) return;
    setRunning(true);
    setError(null);
    try {
      const res = await fetch(`/api/v1/datasets/${datasetId}/runs`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ eval_rule_id: selectedRuleId }),
      });
      if (res.ok) {
        onSuccess();
      } else {
        const text = await res.text();
        setError(text || "Failed to start run");
      }
    } catch {
      setError("Network error while starting run");
    } finally {
      setRunning(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ backgroundColor: "rgba(0, 0, 0, 0.7)" }}
      onClick={onClose}
    >
      <div
        className="flex flex-col w-full max-w-md mx-4 rounded-xl border overflow-hidden"
        style={{
          backgroundColor: colors.backgrounds.charcoalDepth,
          borderColor: colors.backgrounds.caveWall,
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b"
          style={{ borderColor: colors.backgrounds.caveWall }}
        >
          <h2 className="text-sm font-semibold text-lantern-pure">
            New Dataset Run
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 rounded transition-colors"
            style={{ color: colors.typography.ashGrey }}
            onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = colors.backgrounds.slightIllumination)}
            onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = "transparent")}
            aria-label="Close"
          >
            <svg width="18" height="18" viewBox="0 0 16 16" fill="none">
              <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
          </button>
        </div>

        {/* Body */}
        <div className="px-5 py-4 space-y-4">
          {loading ? (
            <div className="text-xs text-lantern-ash py-4 flex items-center justify-center gap-2">
              <Spinner size={14} /> Loading eval rules…
            </div>
          ) : rules.length === 0 ? (
            <div className="text-xs text-lantern-ash py-4 text-center">
              No active eval rules found. Create an eval rule to start a run.
            </div>
          ) : (
            <>
              <div>
                <label className="text-xs font-medium text-lantern-ash mb-1 block">
                  Eval Rule
                </label>
                <select
                  value={selectedRuleId}
                  onChange={(e) => setSelectedRuleId(e.target.value)}
                  className="w-full text-xs px-3 py-2 rounded border outline-none"
                  style={{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  }}
                >
                  {rules.map((rule) => (
                    <option key={rule.rule_id} value={rule.rule_id}>
                      {rule.name} ({rule.judge_model})
                    </option>
                  ))}
                </select>
              </div>
              {error && (
                <div className="text-xs text-red-400">{error}</div>
              )}
            </>
          )}
        </div>

        {/* Footer */}
        {rules.length > 0 && (
          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t"
            style={{ borderColor: colors.backgrounds.caveWall }}
          >
            <button
              onClick={onClose}
              className="text-xs px-3 py-1.5 rounded border transition-colors"
              style={{
                borderColor: colors.backgrounds.caveWall,
                color: colors.typography.ashGrey,
              }}
            >
              Cancel
            </button>
            <button
              onClick={handleRun}
              disabled={!selectedRuleId || running}
              className="text-xs px-3 py-1.5 rounded transition-colors disabled:opacity-50"
              style={{
                background: colors.accents.emberFlare,
                color: colors.typography.pureLight,
              }}
            >
              {running ? "Running…" : "Start Run"}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

// ── Per-Item Result Row ────────────────────────────────────────────

function ItemResultRow({ item }: { item: RunItemEntry }) {
  const [expanded, setExpanded] = useState(false);
  const scoreColor =
    item.score >= 0.8
      ? "#4CAF50"
      : item.score >= 0.5
        ? "#FFC107"
        : item.score >= 0.2
          ? "#FF9800"
          : "#F44336";

  return (
    <div
      className="border rounded-lg overflow-hidden transition-colors"
      style={{
        borderColor: colors.backgrounds.caveWall,
        backgroundColor: colors.backgrounds.abyssBlack,
      }}
    >
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center justify-between px-4 py-3 text-left"
        style={{ color: colors.typography.pureLight }}
      >
        <div className="flex items-center gap-3 min-w-0">
          <span
            className="text-xs font-mono font-bold px-2 py-0.5 rounded"
            style={{
              backgroundColor: scoreColor + "22",
              color: scoreColor,
            }}
          >
            {item.score.toFixed(1)}
          </span>
          <span className="text-xs text-lantern-ash truncate max-w-xs">
            {truncate(item.input, 80)}
          </span>
        </div>
        <svg
          width="14"
          height="14"
          viewBox="0 0 16 16"
          fill="none"
          className={`shrink-0 transition-transform ${expanded ? "rotate-180" : ""}`}
          style={{ color: colors.typography.ashGrey }}
        >
          <path d="M4 6l4 4 4-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      {expanded && (
        <div className="px-4 pb-4 space-y-3 border-t"
          style={{ borderColor: colors.backgrounds.caveWall }}
        >
          <div>
            <label className="text-xs font-medium text-lantern-ash mb-1 block">Input</label>
            <div
              className="text-xs font-mono px-3 py-2 rounded border overflow-auto max-h-32"
              style={{
                backgroundColor: colors.backgrounds.charcoalDepth,
                borderColor: colors.backgrounds.caveWall,
                color: colors.typography.ashGrey,
              }}
            >
              {item.input}
            </div>
          </div>
          <div>
            <label className="text-xs font-medium text-lantern-ash mb-1 block">Expected Output</label>
            <div
              className="text-xs font-mono px-3 py-2 rounded border overflow-auto max-h-32"
              style={{
                backgroundColor: colors.backgrounds.charcoalDepth,
                borderColor: colors.backgrounds.caveWall,
                color: colors.typography.ashGrey,
              }}
            >
              {item.expected_output || "—"}
            </div>
          </div>
          <div>
            <label className="text-xs font-medium text-lantern-ash mb-1 block">Reasoning</label>
            <div
              className="text-xs px-3 py-2 rounded border"
              style={{
                backgroundColor: colors.backgrounds.charcoalDepth,
                borderColor: colors.backgrounds.caveWall,
                color: colors.typography.ashGrey,
              }}
            >
              {item.reasoning || "—"}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Runs Tab ───────────────────────────────────────────────────────

interface RunsTabProps {
  datasetId: string;
  activeProject: string;
}

function RunsTab({ datasetId, activeProject }: RunsTabProps) {
  const [runs, setRuns] = useState<RunListItem[]>([]);
  const [runDetails, setRunDetails] = useState<Map<string, RunDetail>>(new Map());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showNewRunModal, setShowNewRunModal] = useState(false);
  const [expandedRuns, setExpandedRuns] = useState<Set<string>>(new Set());
  const fetchRuns = useCallback(async () => {
    try {
      const res = await fetch(`/api/v1/datasets/${datasetId}/runs?project_id=${activeProject}`);
      if (!res.ok) throw new Error("Failed to fetch runs");
      const data = await res.json();
      setRuns(data.runs ?? []);
      setError(null);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  }, [datasetId, activeProject]);

  const fetchRunDetail = useCallback(async (runId: string) => {
    try {
      const res = await fetch(`/api/v1/datasets/${datasetId}/runs/${runId}?project_id=${activeProject}`);
      if (!res.ok) throw new Error("Failed to fetch run detail");
      const data = await res.json();
      setRunDetails((prev) => new Map(prev).set(runId, data));
    } catch {
      // silently fail
    }
  }, [datasetId, activeProject]);

  useEffect(() => {
    fetchRuns();
  }, [fetchRuns]);

  // Poll actively running runs every 3 seconds
  useEffect(() => {
    const hasActiveRuns = runs.some((r) => r.status === "running");
    if (!hasActiveRuns) return;

    const activeRunIds = runs.filter((r) => r.status === "running").map((r) => r.run_id);
    if (activeRunIds.length === 0) return;

    const interval = setInterval(async () => {
      for (const runId of activeRunIds) {
        try {
          const res = await fetch(`/api/v1/datasets/${datasetId}/runs/${runId}/status?project_id=${activeProject}`);
          if (!res.ok) continue;
          const data = await res.json();
          if (data.status !== "running") {
            // Run completed or errored — refetch full list
            await fetchRuns();
          }
        } catch {
          // silently fail
        }
      }
    }, 3000);

    return () => clearInterval(interval);
  }, [runs, datasetId, activeProject, fetchRuns]);

  const toggleRunExpand = async (runId: string) => {
    const isExpanded = expandedRuns.has(runId);
    const next = new Set(expandedRuns);
    if (isExpanded) {
      next.delete(runId);
    } else {
      next.add(runId);
      // Fetch detail if not already cached
      if (!runDetails.has(runId)) {
        await fetchRunDetail(runId);
      }
    }
    setExpandedRuns(next);
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Spinner size={24} />
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-xs text-red-400 py-4 px-4">{error}</div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Header with New Run button */}
      <div className="flex items-center justify-between">
        <p className="text-xs text-lantern-ash">
          {runs.length} run{runs.length !== 1 ? "s" : ""}
        </p>
        <button
          onClick={() => setShowNewRunModal(true)}
          className="text-xs px-3 py-1.5 rounded transition-colors disabled:opacity-50"
          style={{
            background: colors.accents.emberFlare,
            color: colors.typography.pureLight,
          }}
        >
          + New Run
        </button>
      </div>

      {runs.length === 0 ? (
        <div className="text-xs text-lantern-ash py-8 text-center">
          No runs yet. Create a run to evaluate dataset items.
        </div>
      ) : (
        <div className="space-y-2">
          {runs.map((run) => {
            const detail = runDetails.get(run.run_id);
            const isExpanded = expandedRuns.has(run.run_id);

            return (
              <div
                key={run.run_id}
                className="rounded-lg border overflow-hidden"
                style={{
                  borderColor: colors.backgrounds.caveWall,
                  backgroundColor: colors.backgrounds.abyssBlack,
                }}
              >
                {/* Run header row */}
                <button
                  onClick={() => toggleRunExpand(run.run_id)}
                  className="w-full flex items-center justify-between px-4 py-3 text-left"
                  style={{ color: colors.typography.pureLight }}
                >
                  <div className="flex items-center gap-4 flex-1 min-w-0">
                    {/* Status */}
                    <StatusBadge status={run.status} />
                    {/* Run ID */}
                    <span className="text-xs font-mono text-lantern-ash truncate" title={run.run_id}>
                      {truncate(run.run_id, 12)}
                    </span>
                    {/* Eval rule */}
                    <span className="text-xs text-lantern-ash truncate max-w-[200px]">
                      {run.eval_rule_id}
                    </span>
                  </div>
                  <div className="flex items-center gap-4 shrink-0">
                    {/* Mean score */}
                    {run.status === "complete" && run.mean_score > 0 && (
                      <span
                        className="text-xs font-mono font-bold"
                        style={{
                          color:
                            run.mean_score >= 0.8
                              ? "#4CAF50"
                              : run.mean_score >= 0.5
                                ? "#FFC107"
                                : "#FF9800",
                        }}
                      >
                        avg {run.mean_score.toFixed(2)}
                      </span>
                    )}
                    {/* Item count */}
                    <span className="text-xs text-lantern-ash">
                      {run.item_count} item{run.item_count !== 1 ? "s" : ""}
                    </span>
                    {/* Created date */}
                    <span className="text-xs text-lantern-ash hidden md:block">
                      {formatTime(run.created_at)}
                    </span>
                    {/* Chevron */}
                    <svg
                      width="14"
                      height="14"
                      viewBox="0 0 16 16"
                      fill="none"
                      className={`shrink-0 transition-transform ${isExpanded ? "rotate-180" : ""}`}
                      style={{ color: colors.typography.ashGrey }}
                    >
                      <path d="M4 6l4 4 4-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                  </div>
                </button>

                {/* Expanded per-item results */}
                {isExpanded && (
                  <div className="px-4 pb-4 space-y-3 border-t"
                    style={{ borderColor: colors.backgrounds.caveWall }}
                  >
                    {run.status === "error" && (
                      <div className="text-xs text-red-400 py-2">
                        Run failed. Check the server logs for details.
                      </div>
                    )}
                    {detail && detail.items.length > 0 ? (
                      detail.items.map((item, idx) => (
                        <ItemResultRow key={idx} item={item} />
                      ))
                    ) : run.status === "running" ? (
                      <div className="text-xs text-lantern-ash py-4 flex items-center gap-2">
                        <Spinner size={14} /> Scoring items…
                      </div>
                    ) : (
                      <div className="text-xs text-lantern-ash py-4">No items scored yet.</div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {/* New Run Modal */}
      {showNewRunModal && (
        <NewRunModal
          datasetId={datasetId}
          activeProject={activeProject}
          onClose={() => setShowNewRunModal(false)}
          onSuccess={() => {
            setShowNewRunModal(false);
            fetchRuns();
          }}
        />
      )}
    </div>
  );
}

// ── Items Tab ──────────────────────────────────────────────────────

interface ItemsTabProps {
  datasetId: string;
  activeProject: string;
}

function ItemsTab({ datasetId, activeProject }: ItemsTabProps) {
  const [items, setItems] = useState<DatasetItemEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchItems = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(
        `/api/v1/datasets/${datasetId}/items?project_id=${activeProject}&limit=100`
      );
      if (!res.ok) throw new Error("Failed to fetch items");
      const data = await res.json();
      setItems(data.items ?? []);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  }, [datasetId, activeProject]);

  useEffect(() => {
    fetchItems();
  }, [fetchItems]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Spinner size={24} />
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-xs text-red-400 py-4 px-4">{error}</div>
    );
  }

  return (
    <div className="space-y-2">
      {items.length === 0 ? (
        <div className="text-xs text-lantern-ash py-8 text-center">
          No items in this dataset. Save spans from traces to build your dataset.
        </div>
      ) : (
        items.map((item, idx) => (
          <div
            key={item.item_id}
            className="rounded-lg border overflow-hidden"
            style={{
              borderColor: colors.backgrounds.caveWall,
              backgroundColor: colors.backgrounds.abyssBlack,
            }}
          >
            <div className="px-4 py-3">
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs font-mono text-lantern-ash">
                  #{idx + 1}
                </span>
                <span className="text-xs text-lantern-ash">
                  {formatTime(item.created_at)}
                </span>
              </div>
              <div className="space-y-2">
                <div>
                  <label className="text-xs font-medium text-lantern-ash mb-1 block">
                    Input
                  </label>
                  <div
                    className="text-xs font-mono px-3 py-2 rounded border overflow-auto max-h-24"
                    style={{
                      backgroundColor: colors.backgrounds.charcoalDepth,
                      borderColor: colors.backgrounds.caveWall,
                      color: colors.typography.ashGrey,
                    }}
                  >
                    {item.input}
                  </div>
                </div>
                {item.expected_output && (
                  <div>
                    <label className="text-xs font-medium text-lantern-ash mb-1 block">
                      Expected Output
                    </label>
                    <div
                      className="text-xs font-mono px-3 py-2 rounded border overflow-auto max-h-24"
                      style={{
                        backgroundColor: colors.backgrounds.charcoalDepth,
                        borderColor: colors.backgrounds.caveWall,
                        color: colors.typography.ashGrey,
                      }}
                    >
                      {item.expected_output}
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        ))
      )}
    </div>
  );
}

// ── Main Component ─────────────────────────────────────────────────

export default function DatasetDetail({ datasetId, activeProject, onBack }: DatasetDetailProps) {
  const [dataset, setDataset] = useState<DatasetInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<"items" | "runs">("items");

  useEffect(() => {
    const fetchDataset = async () => {
      setLoading(true);
      setError(null);
      try {
        const res = await fetch(`/api/v1/datasets/${datasetId}?project_id=${activeProject}`);
        if (!res.ok) throw new Error("Failed to fetch dataset");
        const data = await res.json();
        setDataset(data);
      } catch (e: unknown) {
        setError(e instanceof Error ? e.message : "Unknown error");
      } finally {
        setLoading(false);
      }
    };
    fetchDataset();
  }, [datasetId, activeProject]);

  const tabBtnClass = (tab: "items" | "runs") =>
    `px-4 py-2 text-sm font-medium rounded-t-lg transition-colors ${
      activeTab === tab
        ? "text-lantern-ember border-b-2 border-lantern-ember"
        : "text-lantern-ash hover:text-lantern-pure"
    }`;

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <Spinner size={32} />
        <p className="text-sm text-lantern-ash mt-3">Loading dataset…</p>
      </div>
    );
  }

  if (error || !dataset) {
    return (
      <div className="py-12 text-center">
        <p className="text-sm text-red-400">{error || "Dataset not found"}</p>
        <button
          onClick={onBack}
          className="mt-4 text-xs px-3 py-1.5 rounded border transition-colors"
          style={{
            borderColor: colors.backgrounds.caveWall,
            color: colors.typography.ashGrey,
          }}
        >
          ← Back
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header with breadcrumb */}
      <div className="flex flex-col gap-1.5 px-6 py-4 border-b"
        style={{ borderColor: colors.backgrounds.caveWall }}
      >
        <Breadcrumb items={[
          { label: "Datasets", onClick: onBack },
          { label: dataset.name },
        ]} />
        <div className="flex items-center gap-4">
          <button
            onClick={onBack}
            className="p-1 rounded transition-colors"
            style={{ color: colors.typography.ashGrey }}
            onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = colors.backgrounds.slightIllumination)}
            onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = "transparent")}
            aria-label="Back"
          >
            <svg width="18" height="18" viewBox="0 0 16 16" fill="none">
              <path d="M10 2L5 8l5 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
          <div>
            <h1 className="text-base font-semibold text-lantern-pure">
              {dataset.name}
            </h1>
            <p className="text-xs text-lantern-ash mt-0.5">
              {dataset.item_count} item{dataset.item_count !== 1 ? "s" : ""} · Created {formatTime(dataset.created_at)}
            </p>
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex items-center gap-0 px-6"
        style={{ borderBottom: `1px solid ${colors.backgrounds.caveWall}` }}
      >
        <button className={tabBtnClass("items")} onClick={() => setActiveTab("items")}>
          Items
        </button>
        <button className={tabBtnClass("runs")} onClick={() => setActiveTab("runs")}>
          Runs
        </button>
      </div>

      {/* Tab content */}
      <div className="flex-1 overflow-y-auto p-6">
        {activeTab === "items" ? (
          <ItemsTab datasetId={datasetId} activeProject={activeProject} />
        ) : (
          <RunsTab datasetId={datasetId} activeProject={activeProject} />
        )}
      </div>
    </div>
  );
}
