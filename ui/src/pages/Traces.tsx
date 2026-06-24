import { useState, useEffect, useCallback, useRef } from "react";
import { colors } from "@/theme";
import { OnboardingEmptyState } from "@/components/OnboardingEmptyState";
import { Skeleton } from "@/components/Skeleton";
import { EmptyState } from "@/components/EmptyState";
import {
  formatTimeWithYear,
  formatDuration,
  formatJsonPreview,
  totalTokens,
} from "@/utils/formatters";
import { presetToFromTo } from "@/utils/timeRange";
import BulkAddToDatasetModal from "@/components/BulkAddToDatasetModal";

// ── Types ──────────────────────────────────────────────────────────

interface TracesPageProps {
  activeProject: string;
  onNavigateToTrace: (traceId: string) => void;
  onNavigateToTraceDetail: () => void;
  /** Time-range preset from the Header (e.g. "1h", "1d", "7d"). */
  timeRange?: string;
  /** Whether the TraceDetail overlay is open. */
  traceDetailOpen?: boolean;
  /** Currently active trace ID (for keyboard navigation). */
  activeTraceId?: string;
  /** Setter for the active trace ID in the overlay. */
  setActiveTraceId?: (id: string) => void;
  /** Navigate to the next trace in the list. */
  onNavigateNextTrace?: () => void;
  /** Navigate to the previous trace in the list. */
  onNavigatePrevTrace?: () => void;
  /** Close the TraceDetail overlay. */
  onTraceDetailClose?: () => void;
}

interface Span {
  span_id: string;
  trace_id: string;
  parent_id: string;
  project_id: string;
  name: string;
  kind: string;
  model?: string;
  start_time: string;
  end_time: string;
  cost_usd: number;
  input_tokens: number;
  output_tokens: number;
  input?: string;
  output?: string;
  status_code?: string;
  attributes?: Record<string, unknown>;
  /** Number of spans in this trace (root span + rollups, issue #136). */
  span_count?: number;
  /** Per-kind span counts across the trace (e.g. {"llm": 2, "tool": 1}),
   *  used to render the Levels column without a flat span list. */
  kind_counts?: Record<string, number>;
}

interface SpanQueryResponse {
  spans: Span[];
  next?: string;
  limit: number;
}

interface SpanQueryRequest {
  project_id: string;
  from: string;
  to: string;
  limit: number;
  cursor?: string;
  filters?: QueryFilter[];
}

interface QueryFilter {
  field: string;
  op: string;
  value: string | string[] | number;
}

// Observation-level span kinds (used by the Levels column pills).
export const OBSERVATION_KINDS: string[] = ["llm", "tool", "agent", "chain"];

// ── Observation Level Pills ────────────────────────────────────────

// Full label for each span kind — used for tooltips and aria-labels.
const SPAN_KIND_LABELS: Record<string, string> = {
  llm: "LLM",
  tool: "Tool",
  agent: "Agent",
  chain: "Chain",
  internal: "Internal",
};

interface ObservationPillsProps {
  kindCounts?: Record<string, number>;
}

function ObservationPills({ kindCounts }: ObservationPillsProps) {
  if (!kindCounts || Object.keys(kindCounts).length === 0) return null;

  return (
    <div className="flex items-center gap-1 flex-wrap">
      {Object.entries(kindCounts).map(([kind, count]) => {
        const label = SPAN_KIND_LABELS[kind] ?? kind.slice(0, 3).toUpperCase();
        const displayName = `${label} ${count}`;
        return (
          <span
            key={kind}
            role="status"
            tabIndex={0}
            title={displayName}
            aria-label={displayName}
            className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium"
            style={{
              background: colors.toRgba(colors.accents.emberFlare, 0.13),
              color: colors.accents.softGlow,
              border: `1px solid ${colors.toRgba(colors.accents.emberFlare, 0.27)}`,
            }}
          >
            {kind.slice(0, 3).toUpperCase()} {count}
          </span>
        );
      })}
    </div>
  );
}

// ── Bookmark Star ──────────────────────────────────────────────────

function BookmarkStar({ starred, onClick }: { starred: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="transition-colors duration-150"
      style={{ color: starred ? colors.accents.emberFlare : colors.typography.ashGrey }}
      onMouseEnter={(e) => {
        if (!starred) (e.currentTarget as HTMLElement).style.color = colors.accents.softGlow;
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLElement).style.color = starred
          ? colors.accents.emberFlare
          : colors.typography.ashGrey;
      }}
      title={starred ? "Remove bookmark" : "Bookmark this trace"}
      aria-label={starred ? "Remove bookmark" : "Bookmark this trace"}
    >
      <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
        <path
          d="M8 1.5l1.8 3.7 4 .6-2.9 2.8.7 4L8 10.9 4.4 12.6l.7-4L2.2 5.8l4-.6L8 1.5z"
          stroke="currentColor"
          strokeWidth="1.2"
          fill={starred ? "currentColor" : "none"}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </button>
  );
}

// ── Filter types ──────────────────────────────────────────────────

interface RangeFilter {
  min: number | "";
  max: number | "";
}

interface FilterState {
  [fieldName: string]: string[] | RangeFilter;
}

// ── Filter Sidebar ─────────────────────────────────────────────────

const FILTER_SECTIONS = [
  "trace_name",
  "model",
  "kind",
  "status",
  "duration",
  "tokens",
  "cost",
];

const FILTER_LABELS: Record<string, string> = {
  trace_name: "Trace Name",
  model: "Model",
  kind: "Kind",
  status: "Status Code",
  duration: "Duration",
  tokens: "Tokens",
  cost: "Cost",
};

const KNOWN_KINDS = ["llm", "tool", "agent", "chain"];

// Default values for each filter field.
const DEFAULT_FILTERS: FilterState = {
  trace_name: [],
  model: [],
  kind: [],
  status: [],
  duration: { min: "", max: "" },
  tokens: { min: "", max: "" },
  cost: { min: "", max: "" },
};

// Mapping from filter section names to their API field names and operators.
const filterSectionConfig: Record<
  string,
  | { field: string; op: string }
  | { field: string; op: string; isRange: true }
> = {
  trace_name: { field: "name", op: "in" },
  model: { field: "model", op: "in" },
  kind: { field: "kind", op: "in" },
  status: { field: "status_code", op: "in" },
  duration: { field: "duration_ms", op: "gte", isRange: true },
  tokens: { field: "input_tokens", op: "gte", isRange: true },
  cost: { field: "cost_usd", op: "gte", isRange: true },
};

function isRangeFilter(val: unknown): val is RangeFilter {
  return (
    typeof val === "object" &&
    val !== null &&
    !Array.isArray(val) &&
    "min" in val &&
    "max" in val
  );
}

function buildFiltersFromState(
  filterState: FilterState,
): QueryFilter[] {
  const filters: QueryFilter[] = [];

  for (const [section, val] of Object.entries(filterState)) {
    if (Array.isArray(val)) {
      if (val.length > 0) {
        const config = filterSectionConfig[section];
        if (config && !("isRange" in config)) {
          filters.push({ field: config.field, op: config.op, value: val });
        }
      }
    } else if (isRangeFilter(val)) {
      const config = filterSectionConfig[section];
      if (!config || !("isRange" in config)) continue;

      // Range filters emit two entries: one for the min (gte) and one for max (lte).
      if (val.min !== "") {
        filters.push({ field: config.field, op: config.op, value: Number(val.min) });
      }
      if (val.max !== "") {
        filters.push({ field: config.field, op: config.op === "gte" ? "lte" : "gte", value: Number(val.max) });
      }
    }
  }

  return filters;
}

// ── TextFilter: single free-text input ────────────────────────────

function TextFilter({
  value,
  onApply,
  placeholder,
}: {
  value: string[];
  onApply: (vals: string[]) => void;
  placeholder?: string;
}) {
  const [input, setInput] = useState(value.join(", "));

  useEffect(() => {
    setInput(value.join(", "));
  }, [value]);

  const handleApply = () => {
    const vals = input
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    onApply(vals);
  };

  return (
    <div className="space-y-2">
      <input
        type="text"
        value={input}
        onChange={(e) => setInput(e.target.value)}
        placeholder={placeholder ?? "Enter values…"}
        className="input-focus w-full text-sm px-2.5 py-1.5 rounded-md border border-omneval-border bg-omneval-surface text-omneval-text-pure placeholder-omneval-text-muted"
      />
      <button
        type="button"
        onClick={handleApply}
        className="text-xs px-2.5 py-1 rounded-md font-medium text-white transition-all duration-150 hover:brightness-110 active:brightness-90"
        style={{
          background: "var(--color-omneval-violet)",
          boxShadow: "0 1px 4px var(--omneval-focus-ring)",
        }}
      >
        Apply
      </button>
    </div>
  );
}

// ── ModelFilter: known-model checkboxes + free-text search ───────
//
// Combines a checkbox list of the distinct models seen in the current
// project/time range (fetched via the Analytics DSL) with the existing
// free-text search box, so users can quickly toggle known models on/off
// while still being able to type a model that hasn't been seen yet.

function ModelFilter({
  value,
  onApply,
  activeProject,
  timeRange,
}: {
  value: string[];
  onApply: (vals: string[]) => void;
  activeProject: string;
  timeRange?: string;
}) {
  const [knownModels, setKnownModels] = useState<string[]>([]);

  useEffect(() => {
    let cancelled = false;

    async function fetchDistinctModels() {
      try {
        const { from, to } = presetToFromTo(timeRange);
        const res = await fetch("/api/v1/analytics/spans", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            project_id: activeProject,
            from,
            to,
            group_by: [{ field: "model" }],
          }),
        });
        if (!res.ok) return;
        const data: { rows?: Array<{ model?: string }> } = await res.json();
        if (cancelled) return;
        const models = (data.rows ?? [])
          .map((row) => row.model)
          .filter((m): m is string => !!m);
        setKnownModels(models);
      } catch {
        // Network error — leave knownModels empty; free-text search still works.
      }
    }

    fetchDistinctModels();
    return () => {
      cancelled = true;
    };
  }, [activeProject, timeRange]);

  return (
    <div className="space-y-3">
      {knownModels.length > 0 && (
        <CheckboxFilter options={knownModels} value={value} onApply={onApply} />
      )}
      <TextFilter
        value={value}
        onApply={(vals) => {
          // Merge free-text entries with any currently-checked known models,
          // deduping so the same model isn't listed twice.
          const merged = Array.from(new Set([...value.filter((v) => knownModels.includes(v)), ...vals]));
          onApply(merged);
        }}
        placeholder="e.g. gpt-4, claude-3…"
      />
    </div>
  );
}

// ── CheckboxFilter: multi-select checkboxes ──────────────────────

function CheckboxFilter({
  options,
  labels,
  value,
  onApply,
}: {
  options: string[];
  /** Optional display-label overrides, keyed by option value. */
  labels?: Record<string, string>;
  value: string[];
  onApply: (vals: string[]) => void;
}) {
  const toggle = (option: string) => {
    const newVals = value.includes(option)
      ? value.filter((v) => v !== option)
      : [...value, option];
    onApply(newVals);
  };

  return (
    <div className="space-y-1.5">
      {options.map((option) => (
        <label
          key={option}
          className="flex items-center gap-2.5 text-sm text-omneval-text-muted hover:text-omneval-text-pure cursor-pointer rounded-md px-1.5 py-1 -mx-1.5 transition-colors bg-violet-hover"
        >
          <input
            type="checkbox"
            checked={value.includes(option)}
            onChange={() => toggle(option)}
            className="h-3.5 w-3.5 rounded border border-omneval-border bg-omneval-surface accent-omneval-violet cursor-pointer"
            style={{ accentColor: "var(--color-omneval-violet)" }}
          />
          <span className="capitalize">{labels?.[option] ?? option}</span>
        </label>
      ))}
    </div>
  );
}

// ── RangeFilter: min/max number inputs ────────────────────────────

function makeRangeHandler(
  key: "min" | "max",
  onApply: (r: RangeFilter) => void,
  current: RangeFilter,
): (e: React.ChangeEvent<HTMLInputElement>) => void {
  return (e: React.ChangeEvent<HTMLInputElement>) => {
    const raw = e.target.value;
    const num = raw === "" ? "" : Math.max(0, Number(raw));
    onApply({ ...current, [key]: num === "" ? "" : num });
  };
}

function RangeFilterField({
  value,
  onApply,
  minLabel,
  maxLabel,
  placeholder,
}: {
  value: RangeFilter;
  onApply: (r: RangeFilter) => void;
  minLabel: string;
  maxLabel: string;
  placeholder?: string;
}) {
  const handleMin = makeRangeHandler("min", onApply, value);
  const handleMax = makeRangeHandler("max", onApply, value);

  return (
    <div className="space-y-2.5">
      <div className="flex items-center gap-2">
        <div className="flex-1">
          <span className="text-xs text-omneval-text-muted mb-1 block">{minLabel}</span>
          <input
            type="number"
            value={value.min}
            onChange={handleMin}
            placeholder={placeholder}
            min={0}
            className="input-focus w-full text-sm px-2.5 py-1.5 rounded-md border border-omneval-border bg-omneval-surface text-omneval-text-pure placeholder-omneval-text-muted"
          />
        </div>
        <span className="text-omneval-text-muted pt-5">—</span>
        <div className="flex-1">
          <span className="text-xs text-omneval-text-muted mb-1 block">{maxLabel}</span>
          <input
            type="number"
            value={value.max}
            onChange={handleMax}
            placeholder={placeholder}
            min={0}
            className="input-focus w-full text-sm px-2.5 py-1.5 rounded-md border border-omneval-border bg-omneval-surface text-omneval-text-pure placeholder-omneval-text-muted"
          />
        </div>
      </div>
      <button
        type="button"
        onClick={() => onApply(value)}
        className="text-xs px-2.5 py-1 rounded-md font-medium text-white transition-all duration-150 hover:brightness-110 active:brightness-90"
        style={{
          background: "var(--color-omneval-violet)",
          boxShadow: "0 1px 4px var(--omneval-focus-ring)",
        }}
      >
        Apply
      </button>
    </div>
  );
}

// ── FilterSection: accordion section ──────────────────────────────

function FilterSection({
  name,
  label,
  expanded,
  onToggle,
  onApply,
  value,
  activeProject,
  timeRange,
}: {
  name: string;
  label: string;
  expanded: boolean;
  onToggle: () => void;
  onApply: (field: string, val: string[] | RangeFilter) => void;
  value: string[] | RangeFilter;
  activeProject: string;
  timeRange?: string;
}) {
  const isRange = isRangeFilter(value);

  return (
    <div className="border-b border-omneval-border">
      <button
        onClick={onToggle}
        className="flex items-center justify-between w-full px-3 py-2.5 text-sm font-medium text-omneval-text-pure hover:bg-omneval-violet-hover transition-colors"
      >
        <span>{label}</span>
        <span className="flex items-center gap-2 ml-2">
          {isRange && (
            <span className="text-xs text-omneval-text-muted font-normal">
              {formatRange(value.min, value.max)}
            </span>
          )}
          {!isRange && Array.isArray(value) && value.length > 0 && (
            <span
              className="text-xs font-medium px-1.5 py-0.5 rounded-full"
              style={{
                color: "var(--color-omneval-violet-pale)",
                background: "var(--omneval-violet-hover-strong)",
              }}
            >
              {value.length} selected
            </span>
          )}
          <svg
            width="12"
            height="12"
            viewBox="0 0 12 12"
            fill="none"
            className={`text-omneval-text-muted transition-transform duration-200 ${expanded ? "rotate-90" : ""}`}
          >
            <path d="M4.5 3l3 3-3 3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </span>
      </button>

      {expanded && (
        <div className="px-3 pb-3 pt-1">
          {name === "model" ? (
            <ModelFilter
              value={Array.isArray(value) ? value : []}
              onApply={(vals) => onApply(name, vals)}
              activeProject={activeProject}
              timeRange={timeRange}
            />
          ) : name === "trace_name" ? (
            <TextFilter
              value={Array.isArray(value) ? value : []}
              onApply={(vals) => onApply(name, vals)}
              placeholder="Search trace names…"
            />
          ) : name === "kind" ? (
            <CheckboxFilter
              options={KNOWN_KINDS}
              value={Array.isArray(value) ? value : []}
              onApply={(vals) => onApply(name, vals)}
            />
          ) : name === "status" ? (
            <CheckboxFilter
              options={["OK", "ERROR", "UNSET"]}
              labels={{ UNSET: "Unset" }}
              value={Array.isArray(value) ? value : []}
              onApply={(vals) => onApply(name, vals)}
            />
          ) : (
            <RangeFilterField
              value={isRange ? value : { min: "", max: "" }}
              onApply={(r) => onApply(name, r)}
              minLabel={name === "duration" ? "Min (ms)" : name === "cost" ? "Min ($)" : "Min"}
              maxLabel={name === "duration" ? "Max (ms)" : name === "cost" ? "Max ($)" : "Max"}
              placeholder={name === "duration" ? "ms" : name === "cost" ? "USD" : "tokens"}
            />
          )}
        </div>
      )}
    </div>
  );
}

function formatRange(min: number | "", max: number | ""): string {
  if (min === "" && max === "") return "";
  const minStr = min === "" ? "0" : String(min);
  const maxStr = max === "" ? "∞" : String(max);
  return `${minStr}–${maxStr}`;
}

// ── Table Cell Rendering ─────────────────────────────────────────

interface TableCellRendererProps {
  col: { key: string; label: string };
  span: Span;
  bookmarks: Set<string>;
  onToggleBookmark: (traceId: string) => void;
  onNavigateToTrace: (traceId: string) => void;
  onNavigateToTraceDetail: () => void;
}

function TableCellRenderer({
  col,
  span,
  bookmarks,
  onToggleBookmark,
  onNavigateToTrace,
  onNavigateToTraceDetail,
}: TableCellRendererProps) {
  switch (col.key) {
    case "bookmark":
      return (
        <BookmarkStar
          key={col.key}
          starred={bookmarks.has(span.trace_id)}
          onClick={() => onToggleBookmark(span.trace_id)}
        />
      );
    case "timestamp":
      return (
        <span key={col.key} className="text-omneval-text-muted text-xs">
          {formatTimeWithYear(span.start_time)}
        </span>
      );
    case "name":
      return (
        <button
          key={col.key}
          onClick={() => {
            onNavigateToTrace(span.trace_id);
            onNavigateToTraceDetail();
          }}
          className="text-left block w-full leading-tight"
          title="View trace waterfall"
        >
          <div className="text-omneval-text-pure font-medium">{span.name}</div>
          <div className="text-omneval-text-muted text-[11px] font-mono truncate max-w-[120px]">
            {span.trace_id.slice(0, 12)}…
          </div>
        </button>
      );
    case "input":
      return (
        <div key={col.key} className="max-w-[200px] text-omneval-text-muted text-xs font-mono">
          {span.input ? formatJsonPreview(span.input, 40) : "—"}
        </div>
      );
    case "output":
      return (
        <div key={col.key} className="max-w-[200px] text-omneval-text-muted text-xs font-mono">
          {span.output ? formatJsonPreview(span.output, 40) : "—"}
        </div>
      );
    case "observationLevels":
      return <ObservationPills key={col.key} kindCounts={span.kind_counts} />;
    case "latency":
      return (
        <span key={col.key} className="text-omneval-text-muted">
          {formatDuration(span.start_time, span.end_time)}
        </span>
      );
    case "tokens": {
      const inputDisplay = Math.max(0, span.input_tokens);
      const outputDisplay = Math.max(0, span.output_tokens);
      return (
        <span key={col.key} className="text-omneval-text-muted">
          {totalTokens(span).toLocaleString()}
          <span className="text-omneval-text-muted opacity-60 text-xs ml-1">
            ({inputDisplay}+{outputDisplay})
          </span>
        </span>
      );
    }
    case "cost":
      return (
        <span
          key={col.key}
          className="font-medium"
          style={{ color: colors.accents.emberFlare }}
        >
          ${span.cost_usd.toFixed(4)}
        </span>
      );
    default:
      return null;
  }
}

// ── Pagination ─────────────────────────────────────────────────────

function PaginationControls({
  pageSize,
  onPageSizeChange,
  hasMore,
  loading,
  onLoadNext,
}: {
  pageSize: number;
  onPageSizeChange: (size: number) => void;
  hasMore: boolean;
  loading: boolean;
  onLoadNext: () => void;
}) {
  const sizes = [10, 25, 50, 100];

  const isButtonDisabled = loading || !hasMore;

  const buttonText = loading ? (
    <span className="flex items-center gap-2">
      <svg className="animate-spin h-3.5 w-3.5" viewBox="0 0 24 24" fill="none">
        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
      </svg>
      Loading...
    </span>
  ) : hasMore ? (
    "Load Next Page"
  ) : (
    "No more data"
  );

  const buttonStyle: React.CSSProperties = hasMore
    ? {
        background: colors.accents.emberFlare,
        boxShadow: "0 2px 8px rgba(124, 58, 237, 0.3)",
      }
    : {
        background: colors.backgrounds.caveWall,
        boxShadow: "none",
      };

  return (
    <div className="flex items-center justify-between px-4 py-3 border-t" style={{ borderColor: colors.backgrounds.caveWall }}>
      <div className="flex items-center gap-2">
        <span className="text-sm text-omneval-text-muted">Rows per page:</span>
        <select
          value={pageSize}
          onChange={(e) => onPageSizeChange(Number(e.target.value))}
          className="input-focus text-sm px-2 py-1 rounded border border-omneval-border bg-omneval-surface text-omneval-text-pure"
        >
          {sizes.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>
      <div className="flex items-center gap-2">
        <button
          disabled={isButtonDisabled}
          onClick={onLoadNext}
          className="text-sm px-4 py-1.5 rounded-md font-medium text-white transition-all duration-150 disabled:opacity-40 disabled:cursor-not-allowed hover:brightness-110 active:brightness-90"
          style={buttonStyle}
        >
          {buttonText}
        </button>
      </div>
    </div>
  );
}

// ── Main Traces Page ───────────────────────────────────────────────

export default function TracesPage({
  activeProject,
  onNavigateToTrace,
  onNavigateToTraceDetail,
  timeRange,
  traceDetailOpen,
  activeTraceId,
  setActiveTraceId,
}: TracesPageProps) {
  const [spans, setSpans] = useState<Span[]>([]);
  // Index of the trace currently shown in the overlay (when open).
  const [selectedIndex, setSelectedIndex] = useState<number>(-1);
  const [nextCursor, setNextCursor] = useState("");
  const [loading, setLoading] = useState(false);
  const [pageSize, setPageSize] = useState(25);
  const [searchQuery, setSearchQuery] = useState("");
  // Debounced copy of the search query — fetches are driven by this value
  // so a fetch fires shortly after the user stops typing instead of one
  // racing request per keystroke.
  const [debouncedQuery, setDebouncedQuery] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebouncedQuery(searchQuery), 250);
    return () => clearTimeout(t);
  }, [searchQuery]);
  // Monotonic sequence number for span fetches. Responses that arrive for
  // an outdated sequence are dropped so a slow stale request can never
  // overwrite the latest results (filters/search/project switches race).
  const requestSeq = useRef(0);
  const [autoRefresh, setAutoRefresh] = useState(false);
  // Default-visible columns: Timestamp, Name, Latency, Tokens, Cost.
  // Input/Output/Levels are available via the column-visibility picker but
  // off by default so they don't push the key metrics off-screen.
  const [columnVisibility, setColumnVisibility] = useState({
    bookmark: true,
    timestamp: true,
    name: true,
    latency: true,
    tokens: true,
    cost: true,
    input: false,
    output: false,
    observationLevels: false,
  });

  // Filters
  const [expandedFilters, setExpandedFilters] = useState<Record<string, boolean>>({
    trace_name: true,
  });
  const [filterState, setFilterState] = useState<FilterState>(DEFAULT_FILTERS);
  const [bookmarks, setBookmarks] = useState<Set<string>>(new Set());
  const [showColumnsMenu, setShowColumnsMenu] = useState(false);

  // ── Row Selection ────────────────────────────────────────────────
  const [selectedSpanIds, setSelectedSpanIds] = useState<Set<string>>(new Set());
  const [isBulkModalOpen, setIsBulkModalOpen] = useState(false);

  const handleSelectAll = useCallback(() => {
    setSelectedSpanIds((prev) => {
      if (prev.size === spans.length) {
        return new Set();
      }
      return new Set(spans.map((s) => s.span_id));
    });
  }, [spans]);

  const handleSelectRow = useCallback((spanId: string) => {
    setSelectedSpanIds((prev) => {
      const next = new Set(prev);
      if (next.has(spanId)) {
        next.delete(spanId);
      } else {
        next.add(spanId);
      }
      return next;
    });
  }, []);

  // ── Columns Definition ──
  // Priority order: Timestamp → Name → Latency → Tokens → Cost → Input → Output → Levels
  const allColumns = [
    { key: "selection", label: "", visible: true },
    { key: "bookmark", label: "", visible: columnVisibility.bookmark },
    { key: "timestamp", label: "Timestamp", visible: columnVisibility.timestamp },
    { key: "name", label: "Name", visible: columnVisibility.name },
    { key: "latency", label: "Latency", visible: columnVisibility.latency },
    { key: "tokens", label: "Tokens", visible: columnVisibility.tokens },
    { key: "cost", label: "Cost", visible: columnVisibility.cost },
    { key: "input", label: "Input", visible: columnVisibility.input },
    { key: "output", label: "Output", visible: columnVisibility.output },
    { key: "observationLevels", label: "Levels", visible: columnVisibility.observationLevels },
  ];

  // "selection" and "bookmark" are not toggleable column visibility
  const toggleableColumns = allColumns.filter((c) => c.key !== "selection" && c.key !== "bookmark");

  const columns = [
    { key: "selection", label: "", visible: true },
    { key: "bookmark", label: "", visible: columnVisibility.bookmark },
    ...toggleableColumns.map((c) => ({ ...c, visible: columnVisibility[c.key as keyof typeof columnVisibility] })),
  ];

  const columnTooltips: Record<string, string> = {
    timestamp: "Timestamp of the span",
    name: "Span or trace name",
    input: "Input content (JSON preview)",
    output: "Output content (JSON preview)",
    observationLevels: "Observation count and type (e.g. LLM 3, chain 2)",
    latency: "Duration of the span",
    tokens: "Total token count (input + output)",
    cost: "Estimated cost in USD",
  };

  const fetchSpans = useCallback(
    async (cursor: string, append = false) => {
      const seq = ++requestSeq.current;
      setLoading(true);
      try {
        const { from, to } = presetToFromTo(timeRange);
        const body: SpanQueryRequest = {
          project_id: activeProject,
          from,
          to,
          limit: pageSize,
        };

        if (cursor) body.cursor = cursor;

        if (debouncedQuery) {
          // "contains" compiles server-side to LIKE '%value%'. The substring
          // wrapping is applied by the query compiler, so the raw term is sent
          // as-is. ("ilike" is not in the operator allowlist and 400s.)
          body.filters = [
            { field: "name", op: "contains", value: debouncedQuery },
          ];
        }

        // Apply filter sidebar state.
        const builtFilters = buildFiltersFromState(filterState);
        if (builtFilters.length > 0) {
          body.filters = body.filters ?? [];
          body.filters.push(...builtFilters);
        }

        let res: Response;
        try {
          res = await fetch("/api/v1/spans/query", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body),
          });
        } catch {
          return;
        }

        // A newer request was started while this one was in flight —
        // discard this response so it can't overwrite fresher data.
        if (seq !== requestSeq.current) return;

        if (res.ok) {
          const data: SpanQueryResponse = await res.json();
          if (seq !== requestSeq.current) return;
          if (append) {
            setSpans((prev) => [...prev, ...(data.spans ?? [])]);
          } else {
            setSpans(data.spans ?? []);
          }
          setNextCursor(data.next ?? "");
        } else if (!append) {
          // The query failed for the current parameters (e.g. after a
          // project switch). Showing the previous results would attribute
          // them to the wrong project — clear instead.
          setSpans([]);
          setNextCursor("");
        }
      } finally {
        if (seq === requestSeq.current) setLoading(false);
      }
    },
    [activeProject, pageSize, filterState, debouncedQuery, timeRange],
  );

  // Single fetch driver: any change to project, page size, filters, search
  // or time range changes fetchSpans' identity and triggers exactly one
  // fresh (non-append) fetch. Event handlers only update state — they never
  // call fetchSpans directly, which previously caused stale-closure fetches.
  useEffect(() => {
    fetchSpans("");
  }, [fetchSpans]);

  // Auto-refresh effect
  useEffect(() => {
    if (!autoRefresh) return;
    const interval = setInterval(() => {
      fetchSpans("", false);
    }, 30000);
    return () => clearInterval(interval);
  }, [autoRefresh, fetchSpans]);

  // Reset selection and re-fetch after bulk add succeeds
  const handleBulkAddSuccess = useCallback(() => {
    setSelectedSpanIds(new Set());
    setIsBulkModalOpen(false);
    fetchSpans("", false);
  }, [fetchSpans]);

  const toggleBookmark = async (traceId: string) => {
    const newBookmarks = new Set(bookmarks);
    if (newBookmarks.has(traceId)) {
      newBookmarks.delete(traceId);
    } else {
      newBookmarks.add(traceId);
    }
    setBookmarks(newBookmarks);
    // API call placeholder — toggling bookmark on the server
    try {
      await fetch(`/api/v1/traces/${traceId}/bookmark`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bookmarked: newBookmarks.has(traceId) }),
      });
    } catch {
      // Silently fail — bookmark is local-first
    }
  };

  const toggleColumn = (col: keyof typeof columnVisibility) => {
    setColumnVisibility((prev) => ({ ...prev, [col]: !prev[col] }));
  };

  const toggleFilter = (name: string) => {
    setExpandedFilters((prev) => ({ ...prev, [name]: !prev[name] }));
  };

  const applyFilter = (field: string, val: string[] | RangeFilter) => {
    // State change alone re-triggers the fetch effect with the new filters.
    setFilterState((prev) => ({
      ...prev,
      [field]: val,
    }));
  };

  // ── Trace overlay keyboard navigation ──────────────────────────
  // When the overlay is open, ArrowDown / ArrowUp move between traces.
  // We keep the listener active ({ once: false }) and use a ref to track
  // the latest handler so rapid key-presses always route through the
  // current closure without stale-closure bugs.
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (!traceDetailOpen) return;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIndex((prev) => {
          // Boundary: don't move past the last trace.
          if (prev >= spans.length - 1) return prev;
          const next = prev + 1;
          const traceId = spans[next]?.trace_id;
          if (traceId && setActiveTraceId) {
            setActiveTraceId(traceId);
          }
          return next;
        });
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIndex((prev) => {
          // Boundary: don't move before the first trace.
          if (prev <= 0) return prev;
          const prevIdx = prev - 1;
          const traceId = spans[prevIdx]?.trace_id;
          if (traceId && setActiveTraceId) {
            setActiveTraceId(traceId);
          }
          return prevIdx;
        });
      }
    },
    [traceDetailOpen, spans, setActiveTraceId],
  );

  // Ref to always point at the latest handler so the listener never
  // captures a stale closure.
  const handlerRef = useRef(handleKeyDown);
  useEffect(() => {
    handlerRef.current = handleKeyDown;
  }, [handleKeyDown]);

  useEffect(() => {
    if (!traceDetailOpen || spans.length === 0) return;
    // Ensure we start with the overlay showing the trace that was clicked.
    if (selectedIndex === -1) {
      const activeIdx = spans.findIndex((s) => s.trace_id === activeTraceId);
      if (activeIdx >= 0) {
        setSelectedIndex(activeIdx);
      }
    }
  }, [traceDetailOpen, spans, activeTraceId, selectedIndex]);

  useEffect(() => {
    if (!traceDetailOpen) return;
    const listener = (e: KeyboardEvent) => handlerRef.current(e);
    window.addEventListener("keydown", listener);
    return () => window.removeEventListener("keydown", listener);
  }, [traceDetailOpen]);

  const visibleColumns = columns.filter((c) => c.visible);

  return (
    <div className="flex h-full" style={{ background: colors.backgrounds.abyssBlack }}>
      {/* ── Filter Sidebar ── */}
      <div className="flex flex-col w-64 border-r border-omneval-border bg-omneval-depth overflow-y-auto">
        <div className="px-3 py-3 border-b border-omneval-border">
          <h3 className="text-sm font-semibold tracking-wide text-omneval-text-pure uppercase">Filters</h3>
        </div>
        <div className="flex-1 py-1">
          {FILTER_SECTIONS.map((section) => (
            <FilterSection
              key={section}
              name={section}
              label={FILTER_LABELS[section] ?? section}
              expanded={expandedFilters[section]}
              onToggle={() => toggleFilter(section)}
              onApply={(field, vals) => applyFilter(field, vals)}
              value={filterState[section] ?? []}
              activeProject={activeProject}
              timeRange={timeRange}
            />
          ))}
        </div>
        <div className="px-3 py-3 border-t border-omneval-border">
          <button
            onClick={() => {
              setFilterState(DEFAULT_FILTERS);
              setExpandedFilters({ trace_name: true });
            }}
            className="btn-secondary w-full text-center text-sm"
          >
            Clear All Filters
          </button>
        </div>
      </div>

      {/* ── Main Content ── */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Toolbar */}
        <div
          className="flex items-center gap-4 px-4 py-2 border-b"
          style={{
            borderColor: colors.backgrounds.caveWall,
            background: colors.backgrounds.slightIllumination,
          }}
        >
          {/* Search */}
          <div className="flex-1" />
          <input
            type="text"
            placeholder="Search by ID/Name..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="input-focus w-48 text-sm px-2.5 py-1.5 rounded border border-omneval-border bg-omneval-depth text-omneval-text-pure placeholder-omneval-text-muted"
          />

          {/* Auto-refresh toggle */}
          <label className="flex items-center gap-2 text-sm text-omneval-text-muted cursor-pointer">
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={(e) => setAutoRefresh(e.target.checked)}
              className="rounded"
              style={{ accentColor: colors.accents.emberFlare }}
            />
            <span className={autoRefresh ? "text-omneval-violet-pale" : ""}>
              30s
            </span>
          </label>

          {/* Bulk "Add to dataset" button — shown when rows are selected */}
          {selectedSpanIds.size > 0 && (
          <button
            onClick={() => setIsBulkModalOpen(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border transition-all duration-150"
            style={{
              borderColor: colors.accents.violet,
              background: colors.toRgba(colors.accents.violet, 0.15),
              color: colors.accents.violet,
            }}
            aria-label="Add to dataset"
          >
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
              <path d="M8 2v12M2 8h12" stroke="currentColor" strokeWidth="2" strokeLinecap="round"/>
            </svg>
            Add to dataset ({selectedSpanIds.size})
          </button>
          )}

          {/* Column visibility menu */}
          <div className="relative">
            {/* Screen-reader / test accessible column toggles — always in DOM, no visible text */}
            <div className="sr-only">
              {columns.filter((c) => c.key !== "bookmark").map((col) => (
                <button
                  key={col.key}
                  onClick={() => toggleColumn(col.key as keyof typeof columnVisibility)}
                  aria-label={`Toggle ${col.label} column`}
                  tabIndex={-1}
                />
              ))}
            </div>
            <button
              onClick={() => setShowColumnsMenu((v) => !v)}
              className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium border border-omneval-border text-omneval-text-muted hover:text-omneval-text-pure hover:border-omneval-surface transition-all duration-150"
              title="Show/hide columns"
              aria-label="Column visibility"
            >
              <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
                <path d="M2 4h12M4 8h8M6 12h4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"/>
              </svg>
              Columns
            </button>
            {showColumnsMenu && (
              <div
                className="absolute right-0 top-full mt-1 z-50 rounded-lg border py-1 min-w-[160px]"
                style={{
                  background: colors.backgrounds.charcoalDepth,
                  borderColor: colors.backgrounds.caveWall,
                  boxShadow: "0 8px 24px rgba(0,0,0,0.5)",
                }}
              >
                {columns.filter((c) => c.key !== "bookmark").map((col) => {
                  const isVisible = columnVisibility[col.key as keyof typeof columnVisibility];
                  return (
                    <button
                      key={col.key}
                      onClick={() => toggleColumn(col.key as keyof typeof columnVisibility)}
                      aria-label={`Toggle ${col.label} column`}
                      aria-hidden="true"
                      tabIndex={-1}
                      className="flex items-center gap-2.5 w-full px-3 py-1.5 text-sm text-left transition-colors hover:bg-omneval-violet-hover"
                      style={{ color: isVisible ? colors.typography.pureLight : colors.typography.ashGrey }}
                    >
                      <span
                        className="w-3.5 h-3.5 rounded-sm border flex-shrink-0 flex items-center justify-center"
                        style={{
                          borderColor: isVisible ? colors.accents.emberFlare : colors.backgrounds.caveWall,
                          background: isVisible ? colors.toRgba(colors.accents.emberFlare, 0.15) : "transparent",
                        }}
                      >
                        {isVisible && (
                          <svg width="9" height="9" viewBox="0 0 10 10" fill="none">
                            <path d="M2 5l2.5 2.5L8 3" stroke={colors.accents.emberFlare} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                          </svg>
                        )}
                      </span>
                      <span aria-hidden="true">{col.label}</span>
                    </button>
                  );
                })}
                <div className="border-t mt-1 pt-1" style={{ borderColor: colors.backgrounds.caveWall }}>
                  <button
                    onClick={() => setShowColumnsMenu(false)}
                    className="w-full px-3 py-1.5 text-xs text-left transition-colors hover:bg-omneval-violet-hover"
                    style={{ color: colors.typography.ashGrey }}
                  >
                    Close
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>

        {/* Table */}
        <div className="flex-1 overflow-auto">
          {spans.length === 0 && !loading ? (
            searchQuery ? (
              <EmptyState
                variant="search"
                title="No results found"
                description={`No traces match "${searchQuery}"`}
                actionLabel="Clear Search"
                onAction={() => setSearchQuery("")}
              />
            ) : (
              <OnboardingEmptyState />
            )
          ) : loading && spans.length === 0 ? (
            <div className="flex flex-col gap-2 p-4">
              {Array.from({ length: 8 }).map((_, i) => (
                <Skeleton
                  key={i}
                  className="h-6 rounded"
                  style={{ width: `${70 + Math.random() * 30}%` }}
                />
              ))}
            </div>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr
                  style={{
                    background: colors.backgrounds.slightIllumination,
                    borderBottom: `1px solid ${colors.backgrounds.caveWall}`,
                    color: colors.typography.ashGrey,
                  }}
                >
                  {visibleColumns.map((col) => {
                    if (col.key === "selection") {
                      return (
                        <th
                          key={col.key}
                          className="px-3 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap"
                        >
                          <input
                            type="checkbox"
                            checked={
                              spans.length > 0 && selectedSpanIds.size === spans.length
                            }
                            onChange={handleSelectAll}
                            aria-label="Select all traces"
                            className="rounded accent-[#7C3AED]"
                          />
                        </th>
                      );
                    }
                    return (
                      <th
                        key={col.key}
                        className="px-3 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap"
                        title={columnTooltips[col.key] ?? col.label}
                      >
                        {col.label}
                      </th>
                    );
                  })}
                </tr>
              </thead>
              <tbody>
                {spans.map((span, idx) => (
                  <tr
                    key={span.span_id}
                    className={`cursor-pointer transition-colors duration-150 ${idx === selectedIndex && traceDetailOpen ? "bg-violet-600/10" : ""}`}
                    style={{
                      borderBottom: `1px solid ${colors.backgrounds.caveWall}`,
                    }}
                    onClick={() => {
                      setSelectedIndex(idx);
                      if (traceDetailOpen && setActiveTraceId) {
                        setActiveTraceId(span.trace_id);
                      }
                    }}
                    onMouseEnter={(e) => {
                      if (idx !== selectedIndex || !traceDetailOpen) {
                        (e.currentTarget as HTMLElement).style.background =
                          "rgba(124, 58, 237, 0.12)";
                      }
                    }}
                    onMouseLeave={(e) => {
                      if (idx !== selectedIndex || !traceDetailOpen) {
                        (e.currentTarget as HTMLElement).style.background = "transparent";
                      }
                    }}
                  >
                    {visibleColumns.map((col) => {
                      if (col.key === "selection") {
                        return (
                          <td
                            key={col.key}
                            className="px-2 py-1 whitespace-nowrap"
                            onClick={(e) => e.stopPropagation()}
                          >
                            <input
                              type="checkbox"
                              checked={selectedSpanIds.has(span.span_id)}
                              onChange={() => handleSelectRow(span.span_id)}
                              aria-label={`Select trace ${span.name}`}
                              className="rounded accent-[#7C3AED]"
                            />
                          </td>
                        );
                      }
                      return (
                        <td key={col.key} className="px-2 py-1 whitespace-nowrap">
                          <TableCellRenderer
                            col={col}
                            span={span}
                            bookmarks={bookmarks}
                            onToggleBookmark={toggleBookmark}
                            onNavigateToTrace={onNavigateToTrace}
                            onNavigateToTraceDetail={onNavigateToTraceDetail}
                          />
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {/* Pagination */}
        <PaginationControls
          pageSize={pageSize}
          onPageSizeChange={(size) => {
            setPageSize(size);
          }}
          hasMore={!!nextCursor}
          loading={loading}
          onLoadNext={() => fetchSpans(nextCursor, true)}
        />

        {/* Bulk Add to Dataset Modal */}
        {isBulkModalOpen && (
          <BulkAddToDatasetModal
            spanIds={Array.from(selectedSpanIds)}
            onClose={() => setIsBulkModalOpen(false)}
            onSuccess={handleBulkAddSuccess}
          />
        )}
        </div>
    </div>
  );
}
