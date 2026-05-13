import { useState, useEffect } from "react";
import { colors } from "@/theme";
import { formatTime } from "@/utils/formatters";
import { truncate } from "@/utils/formatters";

// ── Types ──────────────────────────────────────────────────────────

interface EvalRule {
  RuleID: string;
  ProjectID: string;
  Name: string;
  JudgeModel: string;
  PromptName: string;
  PromptVersion: number;
  SampleRate: number;
  Enabled: boolean;
  CreatedAt: string;
  Filter: EvalFilter;
}

interface EvalFilter {
  kind?: string;
  model?: string;
  service_name?: string;
  prompt_name?: string;
  status_code?: string;
  min_cost_usd?: number;
  max_cost_usd?: number;
  min_duration_ms?: number;
  max_duration_ms?: number;
  attributes_match?: Array<{ key: string; pattern: string }>;
  and?: EvalFilter[];
  or?: EvalFilter[];
  not?: EvalFilter;
}

interface CreateEvalRuleRequest {
  name: string;
  judge_model: string;
  prompt_name: string;
  prompt_version?: number;
  sample_rate: number;
  enabled?: boolean;
  filter: EvalFilter;
}

interface ApiResponse {
  rules: EvalRule[];
}

// ── Props ──────────────────────────────────────────────────────────

interface EvalRulesPageProps {
  activeProject: string;
}

// ── SpanKind options ───────────────────────────────────────────────

const SPAN_KINDS = [
  { value: "llm", label: "LLM" },
  { value: "tool", label: "Tool" },
  { value: "agent", label: "Agent" },
  { value: "chain", label: "Chain" },
  { value: "internal", label: "Internal" },
] as const;

const STATUS_CODES = ["OK", "ERROR", "CANCELLED", "UNKNOWN"] as const;

// ── Component ──────────────────────────────────────────────────────

export default function EvalRulesPage({ activeProject }: EvalRulesPageProps) {
  const [rules, setRules] = useState<EvalRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // New rule form state
  const [showNewRuleForm, setShowNewRuleForm] = useState(false);
  const [newRuleName, setNewRuleName] = useState("");
  const [newJudgeModel, setNewJudgeModel] = useState("gpt-4");
  const [newPromptName, setNewPromptName] = useState("");
  const [newPromptVersion, setNewPromptVersion] = useState(1);
  const [newSampleRate, setNewSampleRate] = useState(100);
  const [newFilterKind, setNewFilterKind] = useState("");
  const [newFilterModel, setNewFilterModel] = useState("");
  const [newFilterService, setNewFilterService] = useState("");
  const [newFilterStatus, setNewFilterStatus] = useState("");
  const [newFilterMinCost, setNewFilterMinCost] = useState("");
  const [newFilterMaxCost, setNewFilterMaxCost] = useState("");
  const [newFilterMinDuration, setNewFilterMinDuration] = useState("");
  const [newFilterMaxDuration, setNewFilterMaxDuration] = useState("");

  const [createError, setCreateError] = useState<string | null>(null);

  const fetchRules = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(`/api/v1/eval-rules?project_id=${activeProject}`);
      if (!res.ok) throw new Error("Failed to fetch eval rules");
      const data: ApiResponse = await res.json();
      setRules(data.rules || []);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchRules();
  }, [activeProject]);

  // Handle create new rule
  const handleCreate = async () => {
    setCreateError(null);
    if (!newRuleName.trim()) {
      setCreateError("Rule name is required");
      return;
    }
    if (!newJudgeModel.trim()) {
      setCreateError("Judge model is required");
      return;
    }

    const filter: EvalFilter = {};
    if (newFilterKind) filter.kind = newFilterKind;
    if (newFilterModel) filter.model = newFilterModel;
    if (newFilterService) filter.service_name = newFilterService;
    if (newFilterStatus) filter.status_code = newFilterStatus;
    if (newFilterMinCost) filter.min_cost_usd = parseFloat(newFilterMinCost);
    if (newFilterMaxCost) filter.max_cost_usd = parseFloat(newFilterMaxCost);
    if (newFilterMinDuration) filter.min_duration_ms = parseInt(newFilterMinDuration);
    if (newFilterMaxDuration) filter.max_duration_ms = parseInt(newFilterMaxDuration);

    const body: CreateEvalRuleRequest = {
      name: newRuleName.trim(),
      judge_model: newJudgeModel.trim(),
      prompt_name: newPromptName.trim(),
      prompt_version: newPromptVersion,
      sample_rate: newSampleRate / 100,
      enabled: true,
      filter,
    };

    try {
      const res = await fetch("/api/v1/eval-rules", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        const bodyText = await res.text();
        setCreateError(bodyText || "Failed to create rule");
        return;
      }
      setNewRuleName("");
      setNewJudgeModel("gpt-4");
      setNewPromptName("");
      setNewPromptVersion(1);
      setNewSampleRate(100);
      setNewFilterKind("");
      setNewFilterModel("");
      setNewFilterService("");
      setNewFilterStatus("");
      setNewFilterMinCost("");
      setNewFilterMaxCost("");
      setNewFilterMinDuration("");
      setNewFilterMaxDuration("");
      setShowNewRuleForm(false);
      await fetchRules();
    } catch (e: unknown) {
      setCreateError(e instanceof Error ? e.message : "Unknown error");
    }
  };

  // Handle delete rule
  const handleDelete = async (ruleId: string) => {
    if (!window.confirm("Are you sure you want to delete this eval rule?")) {
      return;
    }
    try {
      const res = await fetch(`/api/v1/eval-rules/${ruleId}`, {
        method: "DELETE",
      });
      if (!res.ok) {
        const bodyText = await res.text();
        setError(bodyText || "Failed to delete rule");
        return;
      }
      setRules((prev) => prev.filter((r) => r.RuleID !== ruleId));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Unknown error");
    }
  };

  // Format sample rate for display
  const formatSampleRate = (rate: number): string => {
    return `${(rate * 100).toFixed(0)}%`;
  };

  // Build filter display text
  const filterDisplayText = (filter: EvalFilter): string => {
    const parts: string[] = [];
    if (filter.kind) parts.push(`kind=${filter.kind}`);
    if (filter.model) parts.push(`model=${filter.model}`);
    if (filter.service_name) parts.push(`service=${filter.service_name}`);
    if (filter.status_code) parts.push(`status=${filter.status_code}`);
    if (filter.min_cost_usd !== undefined) parts.push(`min_cost=$${filter.min_cost_usd}`);
    if (filter.max_cost_usd !== undefined) parts.push(`max_cost=$${filter.max_cost_usd}`);
    if (filter.min_duration_ms !== undefined) parts.push(`min_dur=${filter.min_duration_ms}ms`);
    if (filter.max_duration_ms !== undefined) parts.push(`max_dur=${filter.max_duration_ms}ms`);
    if (filter.attributes_match && filter.attributes_match.length > 0) {
      parts.push(`attrs=${filter.attributes_match.length}`);
    }
    return parts.join(", ") || "no filter";
  };

  // ── Render ───────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center h-[60vh]">
        <div className="animate-spin rounded-full h-10 w-10 border-2 border-lantern-ember border-t-transparent" />
        <p className="text-sm text-lantern-ash mt-3">Loading eval rules...</p>
      </div>
    );
  }

  if (error && !loading) {
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
          <h1 className="text-xl font-semibold text-lantern-pure">Eval Rules</h1>
          <p className="text-sm text-lantern-ash mt-0.5">
            {rules.length} rule{rules.length !== 1 ? "s" : ""} · {activeProject}
          </p>
        </div>
        <button
          onClick={() => setShowNewRuleForm(!showNewRuleForm)}
          className="px-4 py-2 text-sm font-medium rounded-md transition-all duration-150 text-white"
          style={{ backgroundColor: colors.accents.emberFlare }}
          onMouseEnter={(e) => (e.currentTarget.style.opacity = "0.85")}
          onMouseLeave={(e) => (e.currentTarget.style.opacity = "1")}
        >
          + New Rule
        </button>
      </div>

      {/* New Rule Form */}
      {showNewRuleForm && (
        <div
          className="rounded-lg p-5 border"
          style={{
            backgroundColor: colors.backgrounds.charcoalDepth,
            borderColor: colors.backgrounds.caveWall,
          }}
        >
          <h3 className="text-sm font-medium text-lantern-pure mb-4">New Eval Rule</h3>

          {createError && (
            <div className="mb-3 px-3 py-2 rounded text-sm" style={{ backgroundColor: "rgba(255,87,34,0.1)", color: colors.accents.emberFlare }}>
              {createError}
            </div>
          )}

          {/* Basic fields */}
          <div className="grid grid-cols-2 gap-4 mb-4">
            <div>
              <label className="block text-xs text-lantern-ash mb-1">Rule Name</label>
              <input
                type="text"
                value={newRuleName}
                onChange={(e) => setNewRuleName(e.target.value)}
                placeholder="e.g. Production LLM Eval"
                className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                style={{
                  backgroundColor: colors.backgrounds.abyssBlack,
                  borderColor: colors.backgrounds.caveWall,
                  color: colors.typography.pureLight,
                }}
              />
            </div>
            <div>
              <label className="block text-xs text-lantern-ash mb-1">Judge Model</label>
              <input
                type="text"
                value={newJudgeModel}
                onChange={(e) => setNewJudgeModel(e.target.value)}
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
              <label className="block text-xs text-lantern-ash mb-1">Judge Prompt Name</label>
              <input
                type="text"
                value={newPromptName}
                onChange={(e) => setNewPromptName(e.target.value)}
                placeholder="e.g. judge-v1"
                className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                style={{
                  backgroundColor: colors.backgrounds.abyssBlack,
                  borderColor: colors.backgrounds.caveWall,
                  color: colors.typography.pureLight,
                }}
              />
            </div>
            <div>
              <label className="block text-xs text-lantern-ash mb-1">Prompt Version</label>
              <input
                type="number"
                value={newPromptVersion}
                min={1}
                onChange={(e) => setNewPromptVersion(parseInt(e.target.value) || 1)}
                className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                style={{
                  backgroundColor: colors.backgrounds.abyssBlack,
                  borderColor: colors.backgrounds.caveWall,
                  color: colors.typography.pureLight,
                }}
              />
            </div>
            <div>
              <label className="block text-xs text-lantern-ash mb-1">Sample Rate (%)</label>
              <input
                type="number"
                value={newSampleRate}
                min={0}
                max={100}
                onChange={(e) => setNewSampleRate(Math.min(100, Math.max(0, parseInt(e.target.value) || 0)))}
                className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                style={{
                  backgroundColor: colors.backgrounds.abyssBlack,
                  borderColor: colors.backgrounds.caveWall,
                  color: colors.typography.pureLight,
                }}
              />
            </div>
          </div>

          {/* Filter conditions */}
          <div
            className="p-3 rounded-md border mb-4"
            style={{ backgroundColor: colors.backgrounds.abyssBlack, borderColor: colors.backgrounds.caveWall }}
          >
            <h4 className="text-xs font-medium text-lantern-ash mb-3 uppercase tracking-wider">Filter Conditions (AND logic)</h4>

            <div className="grid grid-cols-3 gap-3 mb-3">
              <div>
                <label className="block text-xs text-lantern-ash mb-1">Span Kind</label>
                <select
                  value={newFilterKind}
                  onChange={(e) => setNewFilterKind(e.target.value)}
                  className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                  style={{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  }}
                >
                  <option value="">Any</option>
                  {SPAN_KINDS.map((k) => (
                    <option key={k.value} value={k.value}>{k.label}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs text-lantern-ash mb-1">Model</label>
                <input
                  type="text"
                  value={newFilterModel}
                  onChange={(e) => setNewFilterModel(e.target.value)}
                  placeholder="e.g. gpt-4-turbo"
                  className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                  style={{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  }}
                />
              </div>
              <div>
                <label className="block text-xs text-lantern-ash mb-1">Service Name</label>
                <input
                  type="text"
                  value={newFilterService}
                  onChange={(e) => setNewFilterService(e.target.value)}
                  placeholder="e.g. my-service"
                  className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                  style={{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  }}
                />
              </div>
            </div>

            <div className="grid grid-cols-3 gap-3 mb-3">
              <div>
                <label className="block text-xs text-lantern-ash mb-1">Status Code</label>
                <select
                  value={newFilterStatus}
                  onChange={(e) => setNewFilterStatus(e.target.value)}
                  className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                  style={{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  }}
                >
                  <option value="">Any</option>
                  {STATUS_CODES.map((s) => (
                    <option key={s} value={s}>{s}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs text-lantern-ash mb-1">Min Cost ($)</label>
                <input
                  type="number"
                  value={newFilterMinCost}
                  min={0}
                  step={0.01}
                  onChange={(e) => setNewFilterMinCost(e.target.value)}
                  placeholder="0.00"
                  className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                  style={{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  }}
                />
              </div>
              <div>
                <label className="block text-xs text-lantern-ash mb-1">Max Cost ($)</label>
                <input
                  type="number"
                  value={newFilterMaxCost}
                  min={0}
                  step={0.01}
                  onChange={(e) => setNewFilterMaxCost(e.target.value)}
                  placeholder="999.99"
                  className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                  style={{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  }}
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs text-lantern-ash mb-1">Min Duration (ms)</label>
                <input
                  type="number"
                  value={newFilterMinDuration}
                  min={0}
                  onChange={(e) => setNewFilterMinDuration(e.target.value)}
                  placeholder="100"
                  className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                  style={{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  }}
                />
              </div>
              <div>
                <label className="block text-xs text-lantern-ash mb-1">Max Duration (ms)</label>
                <input
                  type="number"
                  value={newFilterMaxDuration}
                  min={0}
                  onChange={(e) => setNewFilterMaxDuration(e.target.value)}
                  placeholder="30000"
                  className="w-full px-3 py-2 text-sm rounded-md border outline-none transition-colors"
                  style={{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  }}
                />
              </div>
            </div>
          </div>

          <div className="flex gap-3">
            <button
              onClick={handleCreate}
              className="px-4 py-2 text-sm font-medium rounded-md text-white transition-all duration-150"
              style={{ backgroundColor: colors.accents.emberFlare }}
              onMouseEnter={(e) => (e.currentTarget.style.opacity = "0.85")}
              onMouseLeave={(e) => (e.currentTarget.style.opacity = "1")}
            >
              Create Rule
            </button>
            <button
              onClick={() => { setShowNewRuleForm(false); setCreateError(null); }}
              className="px-4 py-2 text-sm rounded-md transition-colors"
              style={{ color: colors.typography.ashGrey, backgroundColor: colors.backgrounds.slightIllumination }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Rule List */}
      {rules.length === 0 ? (
        <div
          className="flex flex-col items-center justify-center py-20 rounded-lg border"
          style={{ borderColor: colors.backgrounds.caveWall }}
        >
          <svg
            width="48"
            height="48"
            viewBox="0 0 18 18"
            fill="none"
            className="mb-4 text-lantern-ash"
          >
            <path
              d="M9 2l6 4v6l-6 4-6-4V6l6-4z"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinejoin="round"
            />
            <path d="M9 6v6M6 8l3 2 3-2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <h2 className="text-lg font-medium text-lantern-pure">No eval rules yet</h2>
          <p className="text-sm text-lantern-ash mt-1">
            Create your first eval rule to get started
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {rules.map((rule) => (
            <div
              key={rule.RuleID}
              className="rounded-lg border transition-all duration-150"
              style={{
                backgroundColor: colors.backgrounds.charcoalDepth,
                borderColor: colors.backgrounds.caveWall,
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.borderColor = colors.accents.softGlow;
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.borderColor = colors.backgrounds.caveWall;
              }}
            >
              <div className="flex items-center gap-4 px-5 py-4">
                {/* Rule name */}
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-lantern-pure truncate">
                      {rule.Name}
                    </span>
                    {rule.Enabled ? (
                      <span
                        className="text-xs px-2 py-0.5 rounded font-medium"
                        style={{ backgroundColor: "rgba(34,197,94,0.1)", color: "#22c55e" }}
                      >
                        Enabled
                      </span>
                    ) : (
                      <span
                        className="text-xs px-2 py-0.5 rounded font-medium"
                        style={{ backgroundColor: "rgba(161,161,170,0.1)", color: colors.typography.ashGrey }}
                      >
                        Disabled
                      </span>
                    )}
                  </div>
                  <div className="flex items-center gap-4 mt-1 text-xs text-lantern-ash">
                    <span>Model: <span className="text-lantern-pure">{rule.JudgeModel}</span></span>
                    {rule.PromptName && (
                      <span>Prompt: <span className="text-lantern-pure">{rule.PromptName}{rule.PromptVersion > 1 ? ` v${rule.PromptVersion}` : ""}</span></span>
                    )}
                    <span>Sample: <span className="text-lantern-pure">{formatSampleRate(rule.SampleRate)}</span></span>
                    <span className="truncate">Filter: <span className="text-lantern-pure">{truncate(filterDisplayText(rule.Filter), 40)}</span></span>
                  </div>
                </div>

                {/* Created date */}
                <span className="text-xs text-lantern-ash flex-shrink-0 hidden sm:block">
                  {formatTime(rule.CreatedAt)}
                </span>

                {/* Delete button */}
                <button
                  onClick={() => handleDelete(rule.RuleID)}
                  className="p-1.5 rounded-md transition-colors flex-shrink-0"
                  style={{ color: colors.accents.emberFlare }}
                  onMouseEnter={(e) => {
                    e.currentTarget.style.backgroundColor = "rgba(255,87,34,0.1)";
                  }}
                  onMouseLeave={(e) => {
                    e.currentTarget.style.backgroundColor = "transparent";
                  }}
                  title="Delete rule"
                  aria-label={`Delete rule: ${rule.Name}`}
                >
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                    <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                  </svg>
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
