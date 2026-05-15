import { useState, useEffect } from "react";
import { colors } from "@/theme";
import { ErrorBanner } from "@/components/ErrorBanner";
import { formatTime } from "@/utils/formatters";
import { useToast } from "@/components/Toast";

// ── Types ──────────────────────────────────────────────────────────

interface DatasetListItem {
  dataset_id: string;
  name: string;
  created_at: string;
  item_count: number;
}

interface ParsedItem {
  input: string;
  expected_output: string;
  source_span_id?: string;
}

// ── Props ──────────────────────────────────────────────────────────

interface DatasetsPageProps {
  activeProject: string;
  onNavigateToDetail: (datasetId: string) => void;
}

// ── CSV Parser ─────────────────────────────────────────────────────

function parseCSV(text: string): ParsedItem[] {
  const lines = text.split(/\r?\n/).filter((l) => l.trim() !== "");
  if (lines.length < 2) return [];

  const parseLine = (line: string): string[] => {
    const result: string[] = [];
    let current = "";
    let inQuotes = false;
    for (let i = 0; i < line.length; i++) {
      const ch = line[i];
      if (inQuotes) {
        if (ch === '"') {
          inQuotes = false;
        } else {
          current += ch;
        }
      } else {
        if (ch === '"') {
          inQuotes = true;
        } else if (ch === ",") {
          result.push(current.trim());
          current = "";
        } else {
          current += ch;
        }
      }
    }
    result.push(current.trim());
    return result;
  };

  const headers = parseLine(lines[0]).map((h) => h.toLowerCase());
  const inputIdx = headers.indexOf("input");
  const outputIdx = headers.indexOf("expected_output");
  const spanIdx = headers.indexOf("source_span_id");

  if (inputIdx === -1) return [];

  const items: ParsedItem[] = [];
  for (let i = 1; i < lines.length; i++) {
    const cols = parseLine(lines[i]);
    if (cols.length <= inputIdx) continue;
    const input = cols[inputIdx];
    if (!input) continue;

    const item: ParsedItem = {
      input,
      expected_output: outputIdx !== -1 ? (cols[outputIdx] || "") : "",
    };
    if (spanIdx !== -1 && cols[spanIdx]) {
      item.source_span_id = cols[spanIdx];
    }
    items.push(item);
  }
  return items;
}

function parseJSON(text: string): ParsedItem[] {
  const parsed = JSON.parse(text);
  if (!Array.isArray(parsed)) return [];

  return parsed
    .filter((item: Record<string, unknown>) => item.input && typeof item.input === "string")
    .map((item: Record<string, unknown>) => ({
      input: item.input as string,
      expected_output: (item.expected_output as string) || "",
      source_span_id: (item.source_span_id as string) || undefined,
    }));
}

// ── Constants ──────────────────────────────────────────────────────

const BATCH_SIZE = 50;

// ── Component ──────────────────────────────────────────────────────

export default function DatasetsPage({ activeProject, onNavigateToDetail }: DatasetsPageProps) {
  const { addToast } = useToast();
  const [datasets, setDatasets] = useState<DatasetListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showNewForm, setShowNewForm] = useState(false);
  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  // Upload state
  const [uploadedFile, setUploadedFile] = useState<File | null>(null);
  const [parsedItems, setParsedItems] = useState<ParsedItem[]>([]);
  const [parseError, setParseError] = useState<string | null>(null);

  // Import progress state
  const [importing, setImporting] = useState(false);
  const [importProgress, setImportProgress] = useState(0);
  const [importError, setImportError] = useState<string | null>(null);

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

  const handleFileChange = (file: File) => {
    setUploadedFile(file);
    setParseError(null);

    const reader = new FileReader();
    reader.onload = () => {
      const text = reader.result as string;
      try {
        let items: ParsedItem[];
        if (file.name.endsWith(".csv")) {
          items = parseCSV(text);
        } else {
          items = parseJSON(text);
        }
        setParsedItems(items);
        if (items.length === 0) {
          setParseError("No valid items found. CSV needs an 'input' column; JSON needs an array of objects with 'input' fields.");
        }
      } catch {
        setParseError("Failed to parse file. Ensure it is a valid CSV or JSON file.");
      }
    };
    reader.onerror = () => {
      setParseError("Failed to read file.");
    };
    reader.readAsText(file);
  };

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
        setCreating(false);
        return;
      }
      const data: { dataset_id: string } = await res.json();
      const datasetId = data.dataset_id;
      setNewName("");

      // If there are parsed items, import them
      if (parsedItems.length > 0) {
        await importItems(datasetId, parsedItems);
      } else {
        addToast("success", `Dataset "${newName.trim()}" created`);
        setShowNewForm(false);
        setParsedItems([]);
        setUploadedFile(null);
        await fetchDatasets();
      }
    } catch {
      setCreateError("Network error while creating dataset");
    } finally {
      setCreating(false);
    }
  };

  const importItems = async (datasetId: string, items: ParsedItem[]) => {
    setImporting(true);
    setImportError(null);
    setImportProgress(0);

    let created = 0;
    const total = items.length;

    for (let i = 0; i < total; i += BATCH_SIZE) {
      const batch = items.slice(i, i + BATCH_SIZE);
      try {
        const res = await fetch(`/api/v1/datasets/${datasetId}/items/batch`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ items: batch }),
        });
        if (!res.ok) {
          const text = await res.text();
          setImportError(text || `Failed to upload batch starting at item ${i + 1}`);
          break;
        }
        const data = await res.json();
        created += data.created;
        setImportProgress(Math.min(100, Math.round(((i + batch.length) / total) * 100)));
      } catch {
        setImportError(`Network error during import at item ${i + 1}`);
        break;
      }
    }

    setImporting(false);
    setParsedItems([]);
    setUploadedFile(null);
    setShowNewForm(false);

    await fetchDatasets();
    onNavigateToDetail(datasetId);
  };

  const handleDelete = async (datasetId: string) => {
    setDeleting(datasetId);
    setDeleteConfirmId(null);
    try {
      const res = await fetch(`/api/v1/datasets/${datasetId}?project_id=${activeProject}`, {
        method: "DELETE",
      });
      if (res.ok) {
        addToast("success", "Dataset deleted");
        await fetchDatasets();
      } else {
        addToast("error", "Failed to delete dataset");
      }
    } catch {
      addToast("error", "Failed to delete dataset");
    } finally {
      setDeleting(null);
    }
  };

  const handleCancelImport = () => {
    setParsedItems([]);
    setUploadedFile(null);
    setParseError(null);
    setImportError(null);
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
          className="px-3 py-1.5 text-xs font-medium rounded-md transition-all duration-150 text-white"
          style={{ backgroundColor: colors.accents.emberFlare }}
          onMouseEnter={(e) => (e.currentTarget.style.opacity = "0.85")}
          onMouseLeave={(e) => (e.currentTarget.style.opacity = "1")}
        >
          + New Dataset
        </button>
      </div>

      {/* Create Form */}
      {showNewForm && (
        <div className="px-6 py-4 border-b"
          style={{ borderColor: colors.backgrounds.caveWall }}
        >
          <h3 className="text-sm font-medium text-lantern-pure mb-3">New Dataset</h3>

          {/* Name input */}
          <div className="flex items-center gap-2 mb-3">
            <input
              type="text"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="Dataset name"
              className="text-xs px-3 py-1.5 rounded border outline-none flex-1 bg-black/40"
              style={{
                borderColor: colors.backgrounds.caveWall,
                color: colors.typography.pureLight,
              }}
              autoFocus
              onKeyDown={(e) => {
                if (e.key === "Enter") handleCreate();
              }}
            />
            <button
              onClick={handleCreate}
              disabled={!newName.trim() || creating || importing}
              className="px-3 py-1.5 text-xs font-medium rounded-md transition-all duration-150 text-white disabled:opacity-50"
              style={{ backgroundColor: colors.accents.emberFlare }}
            >
              {creating ? "Creating…" : "Create"}
            </button>
            <button
              onClick={() => {
                setShowNewForm(false);
                setNewName("");
                setCreateError(null);
                handleCancelImport();
              }}
              className="px-3 py-1.5 text-xs rounded border transition-colors"
              style={{
                borderColor: colors.backgrounds.caveWall,
                color: colors.typography.ashGrey,
              }}
            >
              Cancel
            </button>
          </div>

          {/* File upload section */}
          <div
            className="rounded-md border border-dashed p-4 mb-3"
            style={{ borderColor: colors.backgrounds.caveWall }}
          >
            <label className="block text-xs font-medium text-lantern-ash mb-1">
              Upload CSV or JSON
            </label>
            <p className="text-[10px] text-lantern-ash mb-2">
              Each item needs <span className="text-lantern-pure">input</span> and <span className="text-lantern-pure">expected_output</span> fields. CSV uses headers; JSON uses an array of objects. Unknown columns are ignored.
            </p>

            {uploadedFile && (
              <div className="flex items-center justify-between rounded px-3 py-2 mb-2"
                style={{ backgroundColor: colors.backgrounds.charcoalDepth }}
              >
                <div className="flex items-center gap-2 min-w-0">
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" className="shrink-0 text-lantern-ember">
                    <path d="M11 1H3a1 1 0 00-1 1v12a1 1 0 001 1h10a1 1 0 001-1V5L9.5 1H11z" stroke="currentColor" strokeWidth="1.2" />
                    <path d="M9.5 1v4h4" stroke="currentColor" strokeWidth="1.2" />
                  </svg>
                  <span className="text-xs text-lantern-pure truncate">{uploadedFile.name}</span>
                  <span className="text-xs text-lantern-ash">({parsedItems.length} items)</span>
                </div>
                <button
                  onClick={handleCancelImport}
                  className="text-xs text-lantern-ash hover:text-lantern-pure transition-colors shrink-0"
                >
                  ✕
                </button>
              </div>
            )}

            <input
              type="file"
              accept=".csv,.json"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file) handleFileChange(file);
                e.target.value = "";
              }}
              className="block w-full text-xs text-lantern-ash file:mr-3 file:py-1 file:px-2 file:rounded file:text-xs file:border-0 file:cursor-pointer file:font-medium file:transition-colors"
              style={{
                color: colors.typography.ashGrey,
              }}
            />
          </div>

          {/* Parse error */}
          {parseError && (
            <ErrorBanner message={parseError} />
          )}

          {/* Import progress */}
          {importing && (
            <div className="mb-3">
              <div className="flex items-center justify-between text-xs text-lantern-ash mb-1">
                <span>Importing {parsedItems.length} items…</span>
                <span>{importProgress}%</span>
              </div>
              <div className="w-full h-1.5 rounded-full overflow-hidden" style={{ backgroundColor: colors.backgrounds.caveWall }}>
                <div
                  className="h-full rounded-full transition-all duration-300"
                  style={{
                    width: `${importProgress}%`,
                    backgroundColor: colors.accents.emberFlare,
                  }}
                />
              </div>
              {importError && <ErrorBanner message={importError} />}
            </div>
          )}

          {createError && <ErrorBanner message={createError} />}
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-6">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <svg data-testid="spinner" width="24" height="24" viewBox="0 0 24 24" fill="none" className="animate-spin">
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" strokeOpacity="0.25" />
              <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
            </svg>
          </div>
        ) : error ? (
          <ErrorBanner message={error} />
        ) : datasets.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 text-center">
            <svg width="48" height="48" viewBox="0 0 18 18" fill="none" className="mb-4 text-lantern-ash/40">
              <rect x="2" y="2" width="14" height="14" rx="2" stroke="currentColor" strokeWidth="1.5" />
              <path d="M6 6h6M6 9h4M6 12h6" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
            </svg>
            <p className="text-xs text-lantern-ash">
              No datasets found. Create one to evaluate model outputs.
            </p>
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
                          backgroundColor: "rgba(255,87,34,0.15)",
                          color: "#FF5722",
                        }}
                      >
                        {deleting === ds.dataset_id ? "…" : "Delete"}
                      </button>
                      <button
                        onClick={() => setDeleteConfirmId(null)}
                        className="text-xs px-2 py-1 rounded border transition-colors"
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
                        background: "transparent",
                        border: "none",
                        color: "#FF5722",
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
