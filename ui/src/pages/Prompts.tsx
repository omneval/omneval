import { useState, useEffect } from "react";
import { colors } from "@/theme";
import { formatTime } from "@/utils/formatters";

// ── Types ──────────────────────────────────────────────────────────

interface PromptVersion {
  version_id: string;
  project_id: string;
  name: string;
  version: number;
  template: string;
  model: string;
  temperature: number;
  max_tokens: number;
  created_at: string;
}

interface PromptListItem {
  name: string;
  latest_version: number;
  labels: Record<string, number>;
}

// ── Props ──────────────────────────────────────────────────────────

interface PromptsPageProps {
  activeProject: string;
}

// ── Labels ─────────────────────────────────────────────────────────

const LABELS = ["production", "staging", "dev"] as const;

function labelColor(label: string): string {
  switch (label) {
    case "production": return "#FF5722";
    case "staging": return "#FF8A65";
    case "dev": return "#A1A1AA";
    default: return "#666";
  }
}

function labelBg(label: string): string {
  switch (label) {
    case "production": return "rgba(255, 87, 34, 0.15)";
    case "staging": return "rgba(255, 138, 101, 0.15)";
    case "dev": return "rgba(161, 161, 170, 0.15)";
    default: return "rgba(102, 102, 102, 0.15)";
  }
}

// ── Component ──────────────────────────────────────────────────────

export default function PromptsPage({ activeProject }: PromptsPageProps) {
  const [prompts, setPrompts] = useState<PromptListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Expansion state: which prompt names have their versions expanded
  const [expandedNames, setExpandedNames] = useState<Set<string>>(new Set());
  // Version data for each prompt (cached)
  const [versionData, setVersionData] = useState<Map<string, PromptVersion[]>>(new Map());
  // New prompt form state
  const [showNewPromptForm, setShowNewPromptForm] = useState(false);
  const [newName, setNewName] = useState("");
  const [newTemplate, setNewTemplate] = useState("");
  const [newModel, setNewModel] = useState("gpt-4");
  const [newTemperature, setNewTemperature] = useState(0.7);
  const [newMaxTokens, setNewMaxTokens] = useState(256);
  const [createError, setCreateError] = useState<string | null>(null);
  const [createSuccess, setCreateSuccess] = useState<string | null>(null);

  // Label reassignment state: { promptName, label } -> new version
  const [labelAssignments, setLabelAssignments] = useState<Map<string, number>>(new Map());

  const fetchPrompts = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(`/api/v1/prompts?project_id=${activeProject}`);
      if (!res.ok) throw new Error("Failed to fetch prompts");
      const data = await res.json();
      setPrompts(Array.isArray(data) ? data : []);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchPrompts();
  }, [activeProject]);

  // Toggle expansion
  const toggleExpand = (name: string) => {
    setExpandedNames((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  // Fetch versions for a prompt (on demand)
  const fetchVersions = async (name: string) => {
    if (versionData.has(name)) return;
    try {
      const res = await fetch(`/api/v1/prompts/${name}/versions?project_id=${activeProject}`);
      if (!res.ok) return;
      const data = await res.json();
      setVersionData((prev) => new Map(prev).set(name, data.versions || []));
    } catch {
      // silently fail
    }
  };

  // Handle label reassignment
  const handleLabelChange = async (promptName: string, label: string, version: number) => {
    try {
      const res = await fetch(`/api/v1/prompts/${promptName}/labels/${label}?project_id=${activeProject}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ version }),
      });
      if (!res.ok) throw new Error("Failed to reassign label");
      // Update the assignment cache and refresh the prompt list
      setLabelAssignments((prev) => new Map(prev).set(`${promptName}:${label}`, version));
      await fetchPrompts();
    } catch (e: unknown) {
      // show error briefly
      console.error("Label reassignment failed:", e);
    }
  };

  // Create new prompt
  const handleCreate = async () => {
    setCreateError(null);
    setCreateSuccess(null);
    if (!newName.trim() || !newTemplate.trim()) {
      setCreateError("Name and template are required");
      return;
    }
    try {
      const res = await fetch(`/api/v1/prompts?project_id=${activeProject}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: newName.trim(),
          version: 1,
          template: newTemplate,
          model: newModel,
          temperature: newTemperature,
          max_tokens: newMaxTokens,
        }),
      });
      if (!res.ok) {
        const body = await res.text();
        if (res.status === 409) {
          setCreateError("A prompt with this name already exists");
        } else {
          setCreateError(body || "Failed to create prompt");
        }
        return;
      }
      setCreateSuccess(`Created prompt "${newName.trim()}" version 1`);
      setNewName("");
      setNewTemplate("");
      setShowNewPromptForm(false);
      await fetchPrompts();
    } catch (e: unknown) {
      setCreateError(e instanceof Error ? e.message : "Unknown error");
    }
  };

  // ── Render ───────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center h-[60vh]">
        <div className="animate-spin rounded-full h-10 w-10 border-2 border-lantern-ember border-t-transparent" />
        <p className="text-sm text-lantern-ash mt-3">Loading prompts...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center h-[60vh]">
        <div className="text-lantern-ember text-xl mb-2">⚠</div>
        <p className="text-sm text-lantern-ash">{error}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-lantern-pure">Prompt Registry</h1>
          <p className="text-sm text-lantern-ash mt-0.5">
            {prompts.length} prompt{prompts.length !== 1 ? "s" : ""} · {activeProject}
          </p>
        </div>
        <button
          onClick={() => setShowNewPromptForm(!showNewPromptForm)}
          className="px-4 py-2 text-sm font-medium rounded-md transition-all duration-150 text-white"
          style={{ backgroundColor: colors.accents.emberFlare }}
          onMouseEnter={(e) => (e.currentTarget.style.opacity = "0.85")}
          onMouseLeave={(e) => (e.currentTarget.style.opacity = "1")}
        >
          + New Prompt
        </button>
      </div>

      {/* New Prompt Form */}
      {showNewPromptForm && (
        <div
          className="rounded-lg p-5 border"
          style={{
            backgroundColor: colors.backgrounds.charcoalDepth,
            borderColor: colors.backgrounds.caveWall,
          }}
        >
          <h3 className="text-sm font-medium text-lantern-pure mb-4">New Prompt</h3>

          {createError && (
            <div className="mb-3 px-3 py-2 rounded text-sm" style={{ backgroundColor: "rgba(255,87,34,0.1)", color: colors.accents.emberFlare }}>
              {createError}
            </div>
          )}
          {createSuccess && (
            <div className="mb-3 px-3 py-2 rounded text-sm" style={{ backgroundColor: "rgba(100,255,100,0.1)", color: "#66ff66" }}>
              {createSuccess}
            </div>
          )}

          <div className="grid grid-cols-2 gap-4 mb-4">
            <div>
              <label className="block text-xs text-lantern-ash mb-1">Prompt Name</label>
              <input
                type="text"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="e.g. greeting"
                className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                style={{
                  backgroundColor: colors.backgrounds.abyssBlack,
                  borderColor: colors.backgrounds.caveWall,
                  color: colors.typography.pureLight,
                }}
              />
            </div>
            <div>
              <label className="block text-xs text-lantern-ash mb-1">Model</label>
              <input
                type="text"
                value={newModel}
                onChange={(e) => setNewModel(e.target.value)}
                placeholder="e.g. gpt-4"
                className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                style={{
                  backgroundColor: colors.backgrounds.abyssBlack,
                  borderColor: colors.backgrounds.caveWall,
                  color: colors.typography.pureLight,
                }}
              />
            </div>
          </div>

          <div className="grid grid-cols-3 gap-4 mb-4">
            <div>
              <label className="block text-xs text-lantern-ash mb-1">Temperature (0–1)</label>
              <input
                type="number"
                value={newTemperature}
                min={0}
                max={1}
                step={0.1}
                onChange={(e) => setNewTemperature(parseFloat(e.target.value) || 0)}
                className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                style={{
                  backgroundColor: colors.backgrounds.abyssBlack,
                  borderColor: colors.backgrounds.caveWall,
                  color: colors.typography.pureLight,
                }}
              />
            </div>
            <div>
              <label className="block text-xs text-lantern-ash mb-1">Max Tokens</label>
              <input
                type="number"
                value={newMaxTokens}
                min={1}
                onChange={(e) => setNewMaxTokens(parseInt(e.target.value) || 0)}
                className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                style={{
                  backgroundColor: colors.backgrounds.abyssBlack,
                  borderColor: colors.backgrounds.caveWall,
                  color: colors.typography.pureLight,
                }}
              />
            </div>
          </div>

          <div className="mb-4">
            <label className="block text-xs text-lantern-ash mb-1">
              Template
              <span className="text-lantern-ash ml-2 text-xs">
                (use <code className="text-lantern-soft">{'{{variable}}'}</code> syntax)
              </span>
            </label>
            <textarea
              value={newTemplate}
              onChange={(e) => setNewTemplate(e.target.value)}
              placeholder="Hello {{name}}, welcome to {{place}}!"
              rows={4}
              className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors font-mono resize-none"
              style={{
                backgroundColor: colors.backgrounds.abyssBlack,
                borderColor: colors.backgrounds.caveWall,
                color: colors.typography.pureLight,
              }}
            />
          </div>

          <div className="flex gap-3">
            <button
              onClick={handleCreate}
              className="px-4 py-2 text-sm font-medium rounded-md text-white transition-all duration-150"
              style={{ backgroundColor: colors.accents.emberFlare }}
              onMouseEnter={(e) => (e.currentTarget.style.opacity = "0.85")}
              onMouseLeave={(e) => (e.currentTarget.style.opacity = "1")}
            >
              Create
            </button>
            <button
              onClick={() => { setShowNewPromptForm(false); setCreateError(null); }}
              className="px-4 py-2 text-sm rounded-md transition-colors"
              style={{ color: colors.typography.ashGrey, backgroundColor: colors.backgrounds.slightIllumination }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Prompt List */}
      {prompts.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20">
          <svg
            width="48"
            height="48"
            viewBox="0 0 18 18"
            fill="none"
            className="mb-4 text-lantern-ash"
          >
            <rect x="3" y="3" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
            <path d="M7 7h4M7 10h2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          <h2 className="text-lg font-medium text-lantern-pure">No prompts yet</h2>
          <p className="text-sm text-lantern-ash mt-1">
            Create your first prompt to get started
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {prompts.map((prompt) => {
            const isExpanded = expandedNames.has(prompt.name);
            const versions = versionData.get(prompt.name) || [];

            return (
              <div
                key={prompt.name}
                className="rounded-lg border transition-all duration-150 overflow-hidden"
                style={{
                  backgroundColor: colors.backgrounds.charcoalDepth,
                  borderColor: isExpanded ? colors.accents.emberFlare : colors.backgrounds.caveWall,
                }}
                onMouseEnter={(e) => {
                  if (!isExpanded) {
                    e.currentTarget.style.borderColor = colors.accents.softGlow;
                  }
                }}
                onMouseLeave={(e) => {
                  if (!isExpanded) {
                    e.currentTarget.style.borderColor = colors.backgrounds.caveWall;
                  }
                }}
              >
                {/* Prompt summary row */}
                <button
                  onClick={() => {
                    toggleExpand(prompt.name);
                    if (!isExpanded) fetchVersions(prompt.name);
                  }}
                  className="flex items-center gap-4 w-full px-5 py-4 text-left"
                >
                  {/* Expand arrow */}
                  <svg
                    width="16"
                    height="16"
                    viewBox="0 0 16 16"
                    fill="none"
                    className={`text-lantern-ash transition-transform duration-200 flex-shrink-0 ${isExpanded ? "rotate-90" : ""}`}
                  >
                    <path d="M6 4l4 4-4 4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>

                  {/* Name */}
                  <span className="text-sm font-medium text-lantern-pure truncate flex-1">
                    {prompt.name}
                  </span>

                  {/* Latest version */}
                  <span
                    className="text-xs px-2 py-0.5 rounded"
                    style={{ backgroundColor: colors.backgrounds.slightIllumination, color: colors.typography.ashGrey }}
                  >
                    v{prompt.latest_version}
                  </span>

                  {/* Labels */}
                  <div className="flex gap-1.5">
                    {LABELS.map((label) => {
                      const version = prompt.labels[label];
                      const hasLabel = version != null && version > 0;
                      return (
                        <span
                          key={label}
                          className="text-xs px-2 py-0.5 rounded font-medium"
                          style={{
                            backgroundColor: hasLabel ? labelBg(label) : "transparent",
                            color: hasLabel ? labelColor(label) : colors.typography.ashGrey,
                            opacity: hasLabel ? 1 : 0.4,
                          }}
                        >
                          {label}
                          {hasLabel && <span className="ml-0.5">v{version}</span>}
                        </span>
                      );
                    })}
                  </div>
                </button>

                {/* Expanded version history */}
                {isExpanded && (
                  <div className="border-t px-5 py-4" style={{ borderColor: colors.backgrounds.caveWall }}>
                    {versions.length === 0 ? (
                      <p className="text-xs text-lantern-ash">No version history</p>
                    ) : (
                      <div className="space-y-3">
                        {versions.map((v) => (
                          <div
                            key={v.version_id}
                            className="p-3 rounded-md border"
                            style={{
                              backgroundColor: colors.backgrounds.abyssBlack,
                              borderColor: colors.backgrounds.caveWall,
                            }}
                          >
                            {/* Version header */}
                            <div className="flex items-center justify-between mb-2">
                              <span
                                className="text-xs font-medium px-2 py-0.5 rounded"
                                style={{ backgroundColor: colors.backgrounds.slightIllumination, color: colors.typography.ashGrey }}
                              >
                                Version {v.version}
                              </span>
                              <span className="text-xs text-lantern-ash">{formatTime(v.created_at)}</span>
                            </div>

                            {/* Template */}
                            <div className="mb-2">
                              <div className="text-xs text-lantern-ash mb-1">Template</div>
                              <pre
                                className="text-xs font-mono text-lantern-pure p-2 rounded"
                                style={{ backgroundColor: colors.backgrounds.charcoalDepth, overflow: "auto", maxHeight: "120px" }}
                              >
                                {v.template}
                              </pre>
                            </div>

                            {/* Model config */}
                            <div className="flex gap-4 text-xs text-lantern-ash">
                              <span>Model: <span className="text-lantern-pure">{v.model}</span></span>
                              <span>Temp: <span className="text-lantern-pure">{v.temperature}</span></span>
                              <span>Max: <span className="text-lantern-pure">{v.max_tokens}</span></span>
                            </div>

                            {/* Label assignment dropdowns */}
                            <div className="mt-3 flex gap-3">
                              {LABELS.map((label) => {
                                const currentVersion = labelAssignments.get(`${prompt.name}:${label}`)
                                  ?? prompt.labels[label]
                                  ?? null;
                                return (
                                  <div key={label} className="flex items-center gap-1.5">
                                    <span className="text-xs" style={{ color: labelColor(label) }}>
                                      {label}:
                                    </span>
                                    <select
                                      value={currentVersion || ""}
                                      onChange={(e) => {
                                        const newVersion = parseInt(e.target.value);
                                        if (newVersion > 0) {
                                          handleLabelChange(prompt.name, label, newVersion);
                                        }
                                      }}
                                      className="text-xs px-1.5 py-1 rounded border outline-none"
                                      style={{
                                        backgroundColor: colors.backgrounds.abyssBlack,
                                        borderColor: colors.backgrounds.caveWall,
                                        color: colors.typography.pureLight,
                                        minWidth: "60px",
                                      }}
                                    >
                                      <option value="">—</option>
                                      {versions.map((ver) => (
                                        <option key={ver.version} value={ver.version}>
                                          v{ver.version}
                                        </option>
                                      ))}
                                    </select>
                                  </div>
                                );
                              })}
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
