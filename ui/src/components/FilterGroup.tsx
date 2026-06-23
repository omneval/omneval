import React, { useId } from "react";
import { colors } from "@/theme";

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export type EvalFilter = {
  kind?: string;
  model?: string;
  service_name?: string;
  prompt_name?: string;
  status_code?: string;
  min_cost_usd?: number | string;
  max_cost_usd?: number | string;
  min_duration_ms?: number | string;
  max_duration_ms?: number | string;
  attributes_match?: Array<{ key: string; pattern: string }>;
  and?: EvalFilter[];
  or?: EvalFilter[];
  not?: EvalFilter;
};

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const SPAN_KINDS = [
  { value: "llm", label: "LLM" },
  { value: "tool", label: "Tool" },
  { value: "agent", label: "Agent" },
  { value: "chain", label: "Chain" },
  { value: "internal", label: "Internal" },
] as const;

const STATUS_CODES = ["OK", "ERROR", "CANCELLED", "UNKNOWN"] as const;

const LEAF_CONDITIONS: { key: keyof EvalFilter; label: string }[] = [
  { key: "kind", label: "Span Kind" },
  { key: "model", label: "Model" },
  { key: "service_name", label: "Service Name" },
  { key: "status_code", label: "Status Code" },
  { key: "min_cost_usd", label: "Min Cost ($)" },
  { key: "max_cost_usd", label: "Max Cost ($)" },
  { key: "min_duration_ms", label: "Min Duration (ms)" },
  { key: "max_duration_ms", label: "Max Duration (ms)" },
];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function createEmptyFilter(): EvalFilter {
  return {};
}

// Infer which field is currently active by checking for presence of leaf keys.
function activeLeaf(filter: EvalFilter): keyof EvalFilter | undefined {
  for (const cond of LEAF_CONDITIONS) {
    if (cond.key in filter) return cond.key;
  }
  return undefined;
}

function withField<K extends keyof EvalFilter>(
  filter: EvalFilter,
  key: K,
  value: EvalFilter[K],
): EvalFilter {
  const next: EvalFilter = { ...filter };
  // Clear all leaf fields so only one condition type is active at a time.
  for (const cond of LEAF_CONDITIONS) {
    if (cond.key !== key) delete next[cond.key];
  }
  next[key] = value;
  return next;
}

// ---------------------------------------------------------------------------
// Reusable UI atoms (no dependencies on EvalRules)
// ---------------------------------------------------------------------------

function Input({
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

function Select({
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

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  const id = useId();
  const child = React.Children.only(children) as React.ReactElement;
  const newProps = { ...child.props, id: child.props.id ?? id } as React.ComponentPropsWithRef<"input" | "select">;
  return (
    <div>
      <label htmlFor={id} className="block text-xs text-omneval-text-muted mb-1">{label}</label>
      {React.cloneElement(child, newProps)}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Leaf condition row
// ---------------------------------------------------------------------------

function LeafConditionRow({
  filter,
  onUpdate,
  onDelete,
  showDelete,
}: {
  filter: EvalFilter;
  onUpdate: (f: EvalFilter) => void;
  onDelete?: () => void;
  showDelete?: boolean;
}) {
  const active = activeLeaf(filter);

  return (
    <div className="flex items-start gap-2 py-2" data-testid="condition-row">
      <div className="flex-1 grid grid-cols-2 gap-2">
        {/* Condition type selector */}
        <Field label="Condition Type">
          <Select
            value={active ?? ""}
            onChange={(e) => {
              const key = e.target.value as keyof EvalFilter;
              if (key) {
                onUpdate(withField(filter, key, ""));
              }
            }}
          >
            <option value="">Select...</option>
            {LEAF_CONDITIONS.map((c) => (
              <option key={c.key} value={c.key}>{c.label}</option>
            ))}
          </Select>
        </Field>

        {/* Value field – shown only when a type is selected */}
        {active === "kind" && (
          <Field label="Kind Value">
            <Select value={filter.kind ?? ""} onChange={(e) => onUpdate(withField(filter, "kind", e.target.value))}>
              <option value="">Select kind...</option>
              {SPAN_KINDS.map((k) => (
                <option key={k.value} value={k.value}>{k.label}</option>
              ))}
            </Select>
          </Field>
        )}

        {active === "model" && (
          <Field label="Model">
            <Input value={filter.model ?? ""} onChange={(e) => onUpdate(withField(filter, "model", e.target.value))} placeholder="e.g. gpt-4-turbo" />
          </Field>
        )}

        {active === "service_name" && (
          <Field label="Service Name">
            <Input value={filter.service_name ?? ""} onChange={(e) => onUpdate(withField(filter, "service_name", e.target.value))} placeholder="e.g. my-service" />
          </Field>
        )}

        {active === "status_code" && (
          <Field label="Status Code">
            <Select value={filter.status_code ?? ""} onChange={(e) => onUpdate(withField(filter, "status_code", e.target.value))}>
              <option value="">Select status...</option>
              {STATUS_CODES.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </Select>
          </Field>
        )}

        {active === "min_cost_usd" && (
          <Field label="Min Cost ($)">
            <Input type="number" value={String(filter.min_cost_usd ?? "")} onChange={(e) => onUpdate(withField(filter, "min_cost_usd", e.target.value))} placeholder="0.00" />
          </Field>
        )}

        {active === "max_cost_usd" && (
          <Field label="Max Cost ($)">
            <Input type="number" value={String(filter.max_cost_usd ?? "")} onChange={(e) => onUpdate(withField(filter, "max_cost_usd", e.target.value))} placeholder="999.99" />
          </Field>
        )}

        {active === "min_duration_ms" && (
          <Field label="Min Duration (ms)">
            <Input type="number" value={String(filter.min_duration_ms ?? "")} onChange={(e) => onUpdate(withField(filter, "min_duration_ms", e.target.value))} placeholder="100" />
          </Field>
        )}

        {active === "max_duration_ms" && (
          <Field label="Max Duration (ms)">
            <Input type="number" value={String(filter.max_duration_ms ?? "")} onChange={(e) => onUpdate(withField(filter, "max_duration_ms", e.target.value))} placeholder="30000" />
          </Field>
        )}
      </div>

      {showDelete && onDelete && (
        <button
          onClick={onDelete}
          className="p-1 rounded transition-colors mt-5 flex-shrink-0"
          style={{ color: colors.typography.ashGrey }}
          onMouseEnter={(e) => { e.currentTarget.style.color = colors.accents.emberFlare; e.currentTarget.style.backgroundColor = "rgba(255,87,34,0.1)"; }}
          onMouseLeave={(e) => { e.currentTarget.style.color = colors.typography.ashGrey; e.currentTarget.style.backgroundColor = "transparent"; }}
          title="Remove condition"
          aria-label="Remove condition"
        >
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
            <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        </button>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Recursive FilterGroup component
// ---------------------------------------------------------------------------

interface FilterGroupProps {
  value: EvalFilter;
  onUpdate: (filter: EvalFilter) => void;
  depth?: number;
}

function FilterGroup({ value, onUpdate, depth = 0 }: FilterGroupProps) {
  const hasOr = !!value.or?.length;
  const hasAnd = !!value.and?.length;
  const hasNot = !!value.not;
  const isLeaf = !hasOr && !hasAnd && !hasNot;
  const isTopLevel = depth === 0;

  /* ── sub-filter operations ─────────────────────────────────────── */

  const addSub = (key: "or" | "and") => () => {
    const existing = value[key] ?? [];
    onUpdate({ ...value, [key]: [...existing, createEmptyFilter()] });
  };

  const updateSub = (key: "or" | "and") => (index: number) => (sub: EvalFilter) => {
    const arr = [...(value[key] ?? [])];
    arr[index] = sub;
    onUpdate({ ...value, [key]: arr });
  };

  const removeSub = (key: "or" | "and") => (index: number) => () => {
    const arr = (value[key] ?? []).filter((_, i) => i !== index);
    onUpdate({ ...value, [key]: arr.length ? arr : undefined });
  };

  /* ── NOT operations ──────────────────────────────────────────── */

  const updateNot = (sub: EvalFilter) => {
    onUpdate({ ...value, not: sub });
  };

  const removeNot = () => {
    const { not, ...rest } = value;
    onUpdate(rest);
  };

  /* ── rendered content ────────────────────────────────────────── */

  // Determine label for the group header (only when >1 sub or >0 subs at depth>0)
  const groupLabel = hasNot ? "NOT" : hasOr ? "OR" : hasAnd ? "AND" : "";
  const shouldShowHeader = groupLabel && depth > 0;

  const operatorColor = groupLabel === "AND"
    ? { bg: "rgba(245,158,11,0.15)", fg: colors.accents.amberWarning }
    : groupLabel === "OR"
      ? { bg: "rgba(6,182,212,0.15)", fg: colors.accents.cyan }
      : { bg: "rgba(239,68,68,0.15)", fg: colors.accents.dangerRed };

  return (
    <div
      style={{
        borderLeft: depth > 0 ? `2px solid ${colors.backgrounds.caveWall}` : "none",
        paddingLeft: depth > 0 ? "0.75rem" : 0,
        paddingBottom: depth > 0 ? "0.5rem" : 0,
      }}
      role="group"
      aria-label={groupLabel || "Filter group"}
    >
      {/* Group header */}
      {shouldShowHeader && (
        <span
          className="text-xs font-semibold uppercase px-2 py-0.5 rounded mb-2 inline-block"
          style={{ backgroundColor: operatorColor.bg, color: operatorColor.fg }}
        >
          {groupLabel}
        </span>
      )}

      {/* Content: leaf row or nested groups */}
      {isLeaf ? (
        <LeafConditionRow
          filter={value}
          onUpdate={(f) => {
            // If there's already an active group, append as a sibling.
            if (hasOr) {
              onUpdate({ ...value, or: [...value.or!, f] });
            } else if (hasAnd) {
              onUpdate({ ...value, and: [...value.and!, f] });
            } else {
              onUpdate(f);
            }
          }}
          showDelete={!isTopLevel}
          onDelete={isTopLevel ? undefined : () => onUpdate({})}
        />
      ) : (
        <div className="space-y-2">
          {/* AND / OR sub-filters */}
          {(hasOr || hasAnd) && (
            <>
              <div className="space-y-1">
                {(hasOr ? value.or! : value.and!).map((sub, idx) => (
                  <div
                    key={idx}
                    className="relative"
                    style={{
                      backgroundColor: colors.backgrounds.abyssBlack,
                      borderRadius: 6,
                      padding: "0.5rem",
                      border: `1px solid ${colors.backgrounds.caveWall}`,
                    }}
                  >
                    <FilterGroup
                      value={sub}
                      onUpdate={updateSub(hasOr ? "or" : "and")(idx)}
                      depth={depth + 1}
                    />
                    <button
                      onClick={removeSub(hasOr ? "or" : "and")(idx)}
                      className="absolute -top-1.5 -right-1.5 w-5 h-5 rounded-full flex items-center justify-center text-xs font-bold transition-colors"
                      style={{ backgroundColor: colors.backgrounds.abyssBlack, color: colors.typography.ashGrey, border: `1px solid ${colors.backgrounds.caveWall}` }}
                      onMouseEnter={(e) => { e.currentTarget.style.color = colors.accents.emberFlare; e.currentTarget.style.borderColor = colors.accents.emberFlare; }}
                      onMouseLeave={(e) => { e.currentTarget.style.color = colors.typography.ashGrey; e.currentTarget.style.borderColor = colors.backgrounds.caveWall; }}
                      title={`Remove ${hasOr ? "OR" : "AND"} condition`}
                      aria-label={`Remove ${hasOr ? "OR" : "AND"} condition`}
                    >
                      ×
                    </button>
                  </div>
                ))}
              </div>
              <ActionButton onClick={addSub(hasOr ? "or" : "and")} variant="secondary">
                + Add {hasOr ? "OR" : "AND"} Condition
              </ActionButton>
            </>
          )}

          {/* NOT sub-filter */}
          {hasNot && value.not && (
            <div
              className="relative"
              style={{
                backgroundColor: colors.backgrounds.abyssBlack,
                borderRadius: 6,
                padding: "0.5rem",
                border: `1px solid ${colors.backgrounds.caveWall}`,
              }}
            >
              <div className="flex items-center gap-2 mb-1">
                <span className="text-xs font-semibold uppercase px-2 py-0.5 rounded" style={{ backgroundColor: "rgba(239,68,68,0.15)", color: colors.accents.dangerRed }}>
                  NOT
                </span>
              </div>
              <FilterGroup value={value.not} onUpdate={updateNot} depth={depth + 1} />
              <button
                onClick={removeNot}
                className="absolute -top-1.5 -right-1.5 w-5 h-5 rounded-full flex items-center justify-center text-xs font-bold transition-colors"
                style={{ backgroundColor: colors.backgrounds.abyssBlack, color: colors.typography.ashGrey, border: `1px solid ${colors.backgrounds.caveWall}` }}
                onMouseEnter={(e) => { e.currentTarget.style.color = colors.accents.emberFlare; e.currentTarget.style.borderColor = colors.accents.emberFlare; }}
                onMouseLeave={(e) => { e.currentTarget.style.color = colors.typography.ashGrey; e.currentTarget.style.borderColor = colors.backgrounds.caveWall; }}
                title="Remove NOT condition"
                aria-label="Remove NOT condition"
              >
                ×
              </button>
            </div>
          )}
        </div>
      )}

      {/* Bottom actions */}
      {/* Top-level leaf: allow adding a sibling condition (converts to AND). */}
      {isTopLevel && isLeaf && (
        <div className="mt-2">
          <ActionButton onClick={() => onUpdate({ and: [{ ...value }, createEmptyFilter()] })} variant="secondary">
            + Add Condition
          </ActionButton>
        </div>
      )}

      {/* Non-top-level leaf (but NOT present): allow wrapping in OR group */}
      {isLeaf && !isTopLevel && !hasNot && (
        <div className="mt-2">
          <button
            onClick={() => {
              // Convert to OR group: keep current value as first branch, add empty second
              const filterWithOr: EvalFilter = value.kind || value.model
                ? { or: [{ ...value }, createEmptyFilter()] }
                : { or: [createEmptyFilter(), createEmptyFilter()] };
              onUpdate(filterWithOr);
            }}
            className="text-xs font-medium transition-colors"
            style={{ color: colors.typography.ashGrey }}
            onMouseEnter={(e) => { e.currentTarget.style.color = colors.accents.emberFlare; }}
            onMouseLeave={(e) => { e.currentTarget.style.color = colors.typography.ashGrey; }}
          >
            + Add OR/AND Group
          </button>
        </div>
      )}
    </div>
  );
}

/* ── Small helper button component ──────────────────────────────────── */

function ActionButton({
  children,
  onClick,
  variant = "primary",
}: {
  children: React.ReactNode;
  onClick: () => void;
  variant?: "primary" | "secondary";
}) {
  const base: React.CSSProperties =
    variant === "primary"
      ? { backgroundColor: colors.accents.emberFlare, color: "#fff" }
      : { backgroundColor: colors.backgrounds.slightIllumination, color: colors.typography.ashGrey };

  return (
    <button
      onClick={onClick}
      className="px-4 py-2 text-sm font-medium rounded-md transition-all duration-150"
      style={base}
      onMouseEnter={(e) => { e.currentTarget.style.opacity = "0.85"; }}
      onMouseLeave={(e) => { e.currentTarget.style.opacity = "1"; }}
    >
      {children}
    </button>
  );
}

// ---------------------------------------------------------------------------
// Exports
// ---------------------------------------------------------------------------

export { FilterGroup, createEmptyFilter };