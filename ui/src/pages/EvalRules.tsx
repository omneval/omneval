import { useState, useEffect, useCallback } from "react";
import { colors } from "@/theme";
import { EmptyState } from "@/components/EmptyState";
import { formatTime } from "@/utils/formatters";
import { truncate } from "@/utils/formatters";
import { useToast } from "@/components/Toast";

interface PreviewSpan {
  span_id: string;
  trace_id: string;
  name: string;
  kind: string;
  model: string;
  start_time: string;
  cost_usd: number;
}

interface PreviewResult {
  spans: PreviewSpan[];
  match_count_24h: number;
}

export interface EvalRule {
  rule_id: string;
  project_id: string;
  name: string;
  judge_model: string | null;
  prompt_name: string;
  prompt_version: number;
  sample_rate: number | null;
  enabled: boolean;
  created_at: string;
  filter: EvalFilter | null | undefined;
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

interface EvalRulesPageProps {
  activeProject: string;
}

interface NewRuleFormState {
  ruleName: string;
  judgeModel: string;
  promptName: string;
  promptVersion: number;
  sampleRate: number;
  filterKind: string;
  filterModel: string;
  filterService: string;
  filterStatus: string;
  filterMinCost: string;
  filterMaxCost: string;
  filterMinDuration: string;
  filterMaxDuration: string;
}

const SPAN_KINDS = [
  { value: "llm", label: "LLM" },
  { value: "tool", label: "Tool" },
  { value: "agent", label: "Agent" },
  { value: "chain", label: "Chain" },
  { value: "internal", label: "Internal" },
] as const;

const STATUS_CODES = ["OK", "ERROR", "CANCELLED", "UNKNOWN"] as const;

const defaultFormState: NewRuleFormState = {
  ruleName: "",
  judgeModel: "gpt-4",
  promptName: "",
  promptVersion: 1,
  sampleRate: 100,
  filterKind: "",
  filterModel: "",
  filterService: "",
  filterStatus: "",
  filterMinCost: "",
  filterMaxCost: "",
  filterMinDuration: "",
  filterMaxDuration: "",
};

function StatusBadge({ enabled }: { enabled: boolean }) {
  if (enabled) {
    return (
      <span
        className="text-xs px-2 py-0.5 rounded font-medium"
        style={{ backgroundColor: colors.toRgba(colors.accents.emberFlare, 0.1), color: colors.accents.emberFlare }}
      >
        Enabled
      </span>
    );
  }
  return (
    <span
      className="text-xs px-2 py-0.5 rounded font-medium"
      style={{
        backgroundColor: "rgba(161,161,170,0.12)",
        color: colors.typography.ashGrey,
        border: "1px solid rgba(161,161,170,0.25)",
      }}
    >
      Disabled
    </span>
  );
}

function buildFilter(form: NewRuleFormState): EvalFilter {
  const filter: EvalFilter = {};
  if (form.filterKind) filter.kind = form.filterKind;
  if (form.filterModel) filter.model = form.filterModel;
  if (form.filterService) filter.service_name = form.filterService;
  if (form.filterStatus) filter.status_code = form.filterStatus;
  if (form.filterMinCost) filter.min_cost_usd = parseFloat(form.filterMinCost);
  if (form.filterMaxCost) filter.max_cost_usd = parseFloat(form.filterMaxCost);
  if (form.filterMinDuration) filter.min_duration_ms = parseInt(form.filterMinDuration);
  if (form.filterMaxDuration) filter.max_duration_ms = parseInt(form.filterMaxDuration);
  return filter;
}

function sampleRatePercent(rate: unknown): string {
  if (typeof rate !== "number" || !Number.isFinite(rate)) return "N/A";
  return `${Math.round(rate * 100)}%`;
}

function filterDisplayText(filter: EvalFilter | null | undefined): string {
  if (!filter) return "no filter";
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
}

function FormField({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <label className="block text-xs text-omneval-text-muted mb-1">{label}</label>
      {children}
    </div>
  );
}

function StyledInput({
  value,
  onChange,
  ...rest
}: Omit<React.InputHTMLAttributes<HTMLInputElement>, "onChange"> & {
  onChange: React.ChangeEventHandler<HTMLInputElement>;
}) {
  return (
    <input
      value={value}
      onChange={onChange}
      className="input-focus w-full px-3 py-2 text-sm rounded-md border border-omneval-border transition-colors"
      style={{
        backgroundColor: colors.backgrounds.abyssBlack,
        color: colors.typography.pureLight,
      }}
      {...rest}
    />
  );
}

function StyledSelect({
  value,
  onChange,
  children,
  ...rest
}: Omit<React.SelectHTMLAttributes<HTMLSelectElement>, "onChange"> & {
  onChange: React.ChangeEventHandler<HTMLSelectElement>;
}) {
  return (
    <select
      value={value}
      onChange={onChange}
      className="input-focus w-full px-3 py-2 text-sm rounded-md border border-omneval-border transition-colors"
      style={{
        backgroundColor: colors.backgrounds.abyssBlack,
        color: colors.typography.pureLight,
      }}
      {...rest}
    >
      {children}
    </select>
  );
}

function ActionButton({
  children,
  onClick,
  variant = "primary",
  onMouseEnterStyle,
  onMouseLeaveStyle,
}: {
  children: React.ReactNode;
  onClick: () => void;
  variant?: "primary" | "secondary";
  onMouseEnterStyle?: React.CSSProperties;
  onMouseLeaveStyle?: React.CSSProperties;
}) {
  const baseStyle: React.CSSProperties =
    variant === "primary"
      ? { backgroundColor: colors.accents.emberFlare, color: "#fff" }
      : { backgroundColor: colors.backgrounds.slightIllumination, color: colors.typography.ashGrey };

  return (
    <button
      onClick={onClick}
      className="px-4 py-2 text-sm font-medium rounded-md transition-all duration-150"
      style={baseStyle}
      onMouseEnter={(e) => {
        Object.assign(e.currentTarget.style, onMouseEnterStyle);
      }}
      onMouseLeave={(e) => {
        Object.assign(e.currentTarget.style, onMouseLeaveStyle);
      }}
    >
      {children}
    </button>
  );
}

function RuleCard({ rule, onDelete }: { rule: EvalRule; onDelete: (ruleId: string) => void }) {
  return (
    <div
      className="rounded-lg border transition-all duration-150"
      style={{
        backgroundColor: colors.backgrounds.slightIllumination,
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
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-omneval-text-pure truncate">
              {rule.name}
            </span>
            <StatusBadge enabled={rule.enabled} />
          </div>
          <div className="flex items-center gap-4 mt-1 text-xs text-omneval-text-muted">
            <span>Model: <span className="text-omneval-text-pure">{rule.judge_model || "—"}</span></span>
            {rule.prompt_name && (
              <span>Prompt: <span className="text-omneval-text-pure">{rule.prompt_name}{rule.prompt_version > 1 ? ` v${rule.prompt_version}` : ""}</span></span>
            )}
            <span>Sample: <span className="text-omneval-text-pure">{sampleRatePercent(rule.sample_rate)}</span></span>
            <span className="truncate">Filter: <span className="text-omneval-text-pure">{truncate(filterDisplayText(rule.filter), 40)}</span></span>
          </div>
        </div>

        <span className="text-xs text-omneval-text-muted flex-shrink-0 hidden sm:block">
          {formatTime(rule.created_at)}
        </span>

        <button
          onClick={() => onDelete(rule.rule_id)}
          className="p-1.5 rounded-md transition-colors flex-shrink-0"
          style={{ color: colors.accents.emberFlare }}
          onMouseEnter={(e) => {
            e.currentTarget.style.backgroundColor = "rgba(255,87,34,0.1)";
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.backgroundColor = "transparent";
          }}
          title="Delete rule"
          aria-label={`Delete rule: ${rule.name}`}
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
            <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        </button>
      </div>
    </div>
  );
}

export default function EvalRulesPage({ activeProject }: EvalRulesPageProps) {
  const [rules, setRules] = useState<EvalRule[]>([]);
  const { addToast } = useToast();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showNewRuleForm, setShowNewRuleForm] = useState(false);
  const [createForm, setCreateForm] = useState<NewRuleFormState>(defaultFormState);
  const [createError, setCreateError] = useState<string | null>(null);
  const [formErrors, setFormErrors] = useState<Record<string, string>>({});
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [previewResult, setPreviewResult] = useState<PreviewResult | null>(null);
  const [showPreview, setShowPreview] = useState(false);

  const fetchRules = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(`/api/v1/eval-rules?project_id=${activeProject}`);
      if (!res.ok) throw new Error("Failed to fetch eval rules");
      const data = await res.json();
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

  const resetCreateForm = () => {
    setCreateForm(defaultFormState);
    setCreateError(null);
    setShowNewRuleForm(false);
    setPreviewResult(null);
    setPreviewLoading(false);
    setPreviewError(null);
    setShowPreview(false);
  };

  const handlePreview = useCallback(async () => {
    const filter = buildFilter(createForm);
    const hasConditions = Object.keys(filter).length > 0;

    if (!hasConditions) {
      setPreviewError("Add at least one filter condition to preview");
      return;
    }

    setPreviewLoading(true);
    setPreviewError(null);
    setShowPreview(true);

    try {
      const res = await fetch("/api/v1/eval-rules/preview", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ filter }),
      });
      if (!res.ok) {
        const bodyText = await res.text();
        setPreviewError(bodyText || "Failed to preview");
        return;
      }
      const data = await res.json();
      setPreviewResult(data);
    } catch (e: unknown) {
      setPreviewError(e instanceof Error ? e.message : "Unknown error");
    } finally {
      setPreviewLoading(false);
    }
  }, [createForm]);

  const validateForm = (): boolean => {
    const errors: Record<string, string> = {};

    if (!createForm.ruleName.trim()) {
      errors.ruleName = "Rule name is required";
    } else if (createForm.ruleName.length > 128) {
      errors.ruleName = "Name must be 128 characters or fewer";
    }

    if (!createForm.judgeModel.trim()) {
      errors.judgeModel = "Judge model is required";
    } else if (createForm.judgeModel.length > 64) {
      errors.judgeModel = "Model name must be 64 characters or fewer";
    }

    if (createForm.sampleRate < 0 || createForm.sampleRate > 100) {
      errors.sampleRate = "Sample rate must be between 0 and 100";
    }

    if (createForm.promptName.trim()) {
      if (createForm.promptName.length > 128) {
        errors.promptName = "Prompt name must be 128 characters or fewer";
      }
    }

    setFormErrors(errors);
    return Object.keys(errors).length === 0;
  };

  const handleCreate = async () => {
    setCreateError(null);
    if (!validateForm()) {
      return;
    }

    const body: CreateEvalRuleRequest = {
      name: createForm.ruleName.trim(),
      judge_model: createForm.judgeModel.trim(),
      prompt_name: createForm.promptName.trim(),
      prompt_version: createForm.promptVersion,
      sample_rate: createForm.sampleRate / 100,
      enabled: true,
      filter: buildFilter(createForm),
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
      addToast("success", `Rule "${createForm.ruleName.trim()}" created`);
      resetCreateForm();
      await fetchRules();
    } catch (e: unknown) {
      setCreateError(e instanceof Error ? e.message : "Unknown error");
    }
  };

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
      addToast("success", "Rule deleted");
      setRules((prev) => prev.filter((r) => r.rule_id !== ruleId));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Unknown error");
    }
  };

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center h-[60vh]">
        <div className="animate-spin rounded-full h-10 w-10 border-2 border-omneval-violet border-t-transparent" />
        <p className="text-sm text-omneval-text-muted mt-3">Loading eval rules...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center h-[60vh]">
        <div className="text-omneval-violet-pale text-xl mb-2">⚠</div>
        <p className="text-sm text-omneval-text-muted">{error}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6 px-6 pt-5">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-omneval-text-pure">Eval Rules</h1>
          <p className="text-sm text-omneval-text-muted mt-0.5">
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
          <h3 className="text-sm font-medium text-omneval-text-pure mb-4">New Eval Rule</h3>

          {createError && (
            <div className="mb-3 px-3 py-2 rounded text-sm" style={{ backgroundColor: "rgba(255,87,34,0.1)", color: colors.accents.emberFlare }}>
              {createError}
            </div>
          )}

          <div className="grid grid-cols-2 gap-4 mb-4">
            <FormField label="Rule Name">
              <StyledInput
                type="text"
                value={createForm.ruleName}
                onChange={(e) => { setCreateForm({ ...createForm, ruleName: e.target.value }); setFormErrors((prev) => ({ ...prev, ruleName: "" })); }}
                placeholder="e.g. Production LLM Eval"
                style={{
                  ...{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  },
                  ...(formErrors.ruleName ? { borderColor: colors.accents.dangerRed } : {}),
                }}
              />
              {formErrors.ruleName && (
                <p className="text-xs mt-1" style={{ color: colors.accents.dangerRed }}>{formErrors.ruleName}</p>
              )}
            </FormField>
            <FormField label="Judge Model">
              <StyledInput
                type="text"
                value={createForm.judgeModel}
                onChange={(e) => { setCreateForm({ ...createForm, judgeModel: e.target.value }); setFormErrors((prev) => ({ ...prev, judgeModel: "" })); }}
                placeholder="e.g. gpt-4"
                style={{
                  ...{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  },
                  ...(formErrors.judgeModel ? { borderColor: colors.accents.dangerRed } : {}),
                }}
              />
              {formErrors.judgeModel && (
                <p className="text-xs mt-1" style={{ color: colors.accents.dangerRed }}>{formErrors.judgeModel}</p>
              )}
            </FormField>
          </div>

          <div className="grid grid-cols-3 gap-4 mb-4">
            <FormField label="Judge Prompt Name">
              <StyledInput
                type="text"
                value={createForm.promptName}
                onChange={(e) => { setCreateForm({ ...createForm, promptName: e.target.value }); setFormErrors((prev) => ({ ...prev, promptName: "" })); }}
                placeholder="e.g. judge-v1"
                style={{
                  ...{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  },
                  ...(formErrors.promptName ? { borderColor: colors.accents.dangerRed } : {}),
                }}
              />
              {formErrors.promptName && (
                <p className="text-xs mt-1" style={{ color: colors.accents.dangerRed }}>{formErrors.promptName}</p>
              )}
            </FormField>
            <FormField label="Prompt Version">
              <StyledInput
                type="number"
                value={createForm.promptVersion}
                min={1}
                onChange={(e) => setCreateForm({ ...createForm, promptVersion: parseInt(e.target.value) || 1 })}
              />
            </FormField>
            <FormField label="Sample Rate (%)">
              <StyledInput
                type="number"
                value={createForm.sampleRate}
                min={0}
                max={100}
                onChange={(e) => { setCreateForm({ ...createForm, sampleRate: Math.min(100, Math.max(0, parseInt(e.target.value) || 0)) }); setFormErrors((prev) => ({ ...prev, sampleRate: "" })); }}
                style={{
                  ...{
                    backgroundColor: colors.backgrounds.abyssBlack,
                    borderColor: colors.backgrounds.caveWall,
                    color: colors.typography.pureLight,
                  },
                  ...(formErrors.sampleRate ? { borderColor: colors.accents.dangerRed } : {}),
                }}
              />
              {formErrors.sampleRate && (
                <p className="text-xs mt-1" style={{ color: colors.accents.dangerRed }}>{formErrors.sampleRate}</p>
              )}
            </FormField>
          </div>

          <div
            className="p-3 rounded-md border mb-4"
            style={{ backgroundColor: colors.backgrounds.abyssBlack, borderColor: colors.backgrounds.caveWall }}
          >
            <h4 className="text-xs font-medium text-omneval-text-muted mb-3 uppercase tracking-wider">Filter Conditions (AND logic)</h4>

            <div className="grid grid-cols-3 gap-3 mb-3">
              <FormField label="Span Kind">
                <StyledSelect
                  value={createForm.filterKind}
                  onChange={(e) => setCreateForm({ ...createForm, filterKind: e.target.value })}
                >
                  <option value="">Any</option>
                  {SPAN_KINDS.map((k) => (
                    <option key={k.value} value={k.value}>{k.label}</option>
                  ))}
                </StyledSelect>
              </FormField>
              <FormField label="Model">
                <StyledInput
                  type="text"
                  value={createForm.filterModel}
                  onChange={(e) => setCreateForm({ ...createForm, filterModel: e.target.value })}
                  placeholder="e.g. gpt-4-turbo"
                />
              </FormField>
              <FormField label="Service Name">
                <StyledInput
                  type="text"
                  value={createForm.filterService}
                  onChange={(e) => setCreateForm({ ...createForm, filterService: e.target.value })}
                  placeholder="e.g. my-service"
                />
              </FormField>
            </div>

            <div className="grid grid-cols-3 gap-3 mb-3">
              <FormField label="Status Code">
                <StyledSelect
                  value={createForm.filterStatus}
                  onChange={(e) => setCreateForm({ ...createForm, filterStatus: e.target.value })}
                >
                  <option value="">Any</option>
                  {STATUS_CODES.map((s) => (
                    <option key={s} value={s}>{s}</option>
                  ))}
                </StyledSelect>
              </FormField>
              <FormField label="Min Cost ($)">
                <StyledInput
                  type="number"
                  value={createForm.filterMinCost}
                  min={0}
                  step={0.01}
                  onChange={(e) => setCreateForm({ ...createForm, filterMinCost: e.target.value })}
                  placeholder="0.00"
                />
              </FormField>
              <FormField label="Max Cost ($)">
                <StyledInput
                  type="number"
                  value={createForm.filterMaxCost}
                  min={0}
                  step={0.01}
                  onChange={(e) => setCreateForm({ ...createForm, filterMaxCost: e.target.value })}
                  placeholder="999.99"
                />
              </FormField>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <FormField label="Min Duration (ms)">
                <StyledInput
                  type="number"
                  value={createForm.filterMinDuration}
                  min={0}
                  onChange={(e) => setCreateForm({ ...createForm, filterMinDuration: e.target.value })}
                  placeholder="100"
                />
              </FormField>
              <FormField label="Max Duration (ms)">
                <StyledInput
                  type="number"
                  value={createForm.filterMaxDuration}
                  min={0}
                  onChange={(e) => setCreateForm({ ...createForm, filterMaxDuration: e.target.value })}
                  placeholder="30000"
                />
              </FormField>
            </div>
          </div>

          {/* Preview button */}
          <div className="flex items-center gap-3 mb-4">
            <button
              onClick={handlePreview}
              className="px-4 py-2 text-sm font-medium rounded-md transition-all duration-150 text-white"
              style={{ backgroundColor: colors.accents.emberFlare }}
              onMouseEnter={(e) => (e.currentTarget.style.opacity = "0.85")}
              onMouseLeave={(e) => (e.currentTarget.style.opacity = "1")}
              disabled={previewLoading}
            >
              {previewLoading ? "Previewing..." : "Preview Matching Spans"}
            </button>
            {previewResult && (
              <span className="text-xs text-omneval-text-muted">
                {previewResult.spans.length} matches in last hour · {previewResult.match_count_24h} in last 24h
              </span>
            )}
          </div>

          {/* Collapsible preview results */}
          {showPreview && (
            <div
              className="rounded-md border mb-4 overflow-hidden"
              style={{ backgroundColor: colors.backgrounds.abyssBlack, borderColor: colors.backgrounds.caveWall }}
            >
              <button
                onClick={() => setShowPreview(!showPreview)}
                className="w-full flex items-center justify-between px-4 py-3 text-sm font-medium text-omneval-text-pure transition-colors"
                onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = "rgba(255,87,34,0.05)")}
                onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = "transparent")}
              >
                <span>{previewResult ? "Matching spans" : "Preview results"}</span>
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 14 14"
                  fill="none"
                  className={`transition-transform duration-150 ${showPreview ? "rotate-180" : ""}`}
                >
                  <path d="M3 5l4 4 4-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              </button>

              {showPreview && (
                <div className="px-4 pb-4">
                  {previewError ? (
                    <div className="text-sm text-omneval-violet-pale">{previewError}</div>
                  ) : previewResult && previewResult.spans.length === 0 ? (
                    <div className="text-sm text-omneval-text-muted py-2">No matching spans found.</div>
                  ) : (
                    <div className="space-y-2">
                      {previewResult?.spans.map((span) => (
                        <div
                          key={span.span_id}
                          className="flex items-center gap-4 px-3 py-2 rounded text-xs"
                          style={{ backgroundColor: "rgba(255,255,255,0.03)" }}
                        >
                          <span className="font-mono text-omneval-text-pure truncate max-w-[120px]" title={span.name}>
                            {span.name}
                          </span>
                          <span className="text-omneval-text-muted">model: <span className="text-omneval-text-pure">{span.model}</span></span>
                          <span className="text-omneval-text-muted">kind: <span className="text-omneval-text-pure">{span.kind}</span></span>
                          <span className="text-omneval-text-muted">cost: <span className="text-omneval-text-pure">${span.cost_usd.toFixed(4)}</span></span>
                          <span className="text-omneval-text-muted flex-1 text-right truncate" title={span.start_time}>
                            {new Date(span.start_time).toLocaleString()}
                          </span>
                          <span className="font-mono text-omneval-text-muted flex-shrink-0">
                            <span className="text-omneval-text-pure">trace: </span>{span.trace_id.substring(0, 8)}...
                          </span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          <div className="flex gap-3">
            <ActionButton onClick={handleCreate}>Create Rule</ActionButton>
            <ActionButton
              onClick={resetCreateForm}
              variant="secondary"
              onMouseEnterStyle={{ opacity: "0.85" }}
              onMouseLeaveStyle={{ opacity: "1" }}
            >
              Cancel
            </ActionButton>
          </div>
        </div>
      )}

      {/* Rule List */}
      {rules.length === 0 ? (
        <EmptyState
          variant="default"
          title="No eval rules yet"
          description="Create your first eval rule to start evaluating traces"
          actionLabel="+ New Rule"
          onAction={() => setShowNewRuleForm(true)}
        />
      ) : (
        <div className="space-y-2">
          {rules.map((rule) => (
            <RuleCard
              key={rule.rule_id}
              rule={rule}
              onDelete={handleDelete}
            />
          ))}
        </div>
      )}
    </div>
  );
}
