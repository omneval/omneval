import { useState, useEffect, useCallback, useMemo, useRef } from "react";
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

// ── Types ──────────────────────────────────────────────────────────

interface TracesPageProps {
  activeProject: string;
  onNavigateToTrace: (traceId: string) => void;
  onNavigateToTraceDetail: () => void;
  onNavigateToConversation: (conversationId: string) => void;
  /** Tab shown on mount — lets the back button from ConversationDetail
   *  return to the Conversations tab rather than the default Traces tab. */
  initialTab?: "traces" | "conversations";
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

// ── Conversation types (issue #67) ─────────────────────────────────

interface ConversationListItem {
  conversation_id: string;
  project_id: string;
  service_name: string;
  trace_count: number;
  span_count: number;
  start_time: string;
  end_time: string;
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
}

interface ConversationListResponse {
  conversations: ConversationListItem[];
  next?: string;
  limit: number;
}

// ── Observation Level Pills ────────────────────────────────────────

interface ObservationPillsProps {
  childSpans: Span[];
}

function ObservationPills({ childSpans }: ObservationPillsProps) {
  if (childSpans.length === 0) return null;

  // Aggregate child counts by kind.
  const kindCounts: Record<string, number> = {};
  childSpans.forEach((span) => {
    const kind = span.kind ?? "span";
    kindCounts[kind] = (kindCounts[kind] ?? 0) + 1;
  });

  return (
    <div className="flex items-center gap-1 flex-wrap">
      {Object.entries(kindCounts).map(([kind, count]) => (
        <span
          key={kind}
          className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium"
          style={{
            background: colors.toRgba(colors.accents.emberFlare, 0.13),
            color: colors.accents.softGlow,
            border: `1px solid ${colors.toRgba(colors.accents.emberFlare, 0.27)}`,
          }}
        >
          {kind.slice(0, 3).toUpperCase()} {count}
        </span>
      ))}
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
        className="input-focus w-full text-sm px-2 py-1.5 rounded border border-omneval-border bg-omneval-surface text-omneval-text-pure placeholder-omneval-text-muted"
      />
      <button
        type="button"
        onClick={handleApply}
        className="text-xs px-2 py-1 rounded-md font-medium text-white transition-all duration-150 hover:brightness-110 active:brightness-90"
        style={{
          background: colors.accents.emberFlare,
          boxShadow: "0 1px 4px rgba(255, 87, 34, 0.2)",
        }}
      >
        Apply
      </button>
    </div>
  );
}

// ── CheckboxFilter: multi-select checkboxes ──────────────────────

function CheckboxFilter({
  options,
  value,
  onApply,
}: {
  options: string[];
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
    <div className="space-y-1">
      {options.map((option) => (
        <label
          key={option}
          className="flex items-center gap-2 text-sm text-omneval-text-muted hover:text-omneval-text-pure cursor-pointer"
        >
          <input
            type="checkbox"
            checked={value.includes(option)}
            onChange={() => toggle(option)}
            className="rounded"
            style={{ accentColor: colors.accents.emberFlare }}
          />
          <span className="capitalize">{option}</span>
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
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <div className="flex-1">
          <span className="text-xs text-omneval-text-muted mb-1 block">{minLabel}</span>
          <input
            type="number"
            value={value.min}
            onChange={handleMin}
            placeholder={placeholder}
            min={0}
            className="input-focus w-full text-sm px-2 py-1.5 rounded border border-omneval-border bg-omneval-surface text-omneval-text-pure placeholder-omneval-text-muted"
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
            className="input-focus w-full text-sm px-2 py-1.5 rounded border border-omneval-border bg-omneval-surface text-omneval-text-pure placeholder-omneval-text-muted"
          />
        </div>
      </div>
      <button
        type="button"
        onClick={() => onApply(value)}
        className="text-xs px-2 py-1 rounded-md font-medium text-white transition-all duration-150 hover:brightness-110 active:brightness-90"
        style={{
          background: colors.accents.emberFlare,
          boxShadow: "0 1px 4px rgba(255, 87, 34, 0.2)",
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
}: {
  name: string;
  label: string;
  expanded: boolean;
  onToggle: () => void;
  onApply: (field: string, val: string[] | RangeFilter) => void;
  value: string[] | RangeFilter;
}) {
  const isRange = isRangeFilter(value);

  return (
    <div
      className="border-b"
      style={{ borderColor: colors.backgrounds.caveWall }}
    >
      <button
        onClick={onToggle}
        className="flex items-center justify-between w-full px-3 py-2 text-sm font-medium text-omneval-text-pure hover:bg-omneval-violet-hover transition-colors"
      >
        <span>{label}</span>
        {isRange && (
          <span className="text-xs text-omneval-text-muted font-normal ml-2">
            {formatRange(value.min, value.max)}
          </span>
        )}
        {!isRange && Array.isArray(value) && value.length > 0 && (
          <span className="text-xs text-omneval-text-muted font-normal ml-2">
            {value.length} selected
          </span>
        )}
        <svg
          width="12"
          height="12"
          viewBox="0 0 12 12"
          fill="none"
          className={`transition-transform duration-200 ${expanded ? "rotate-90" : ""}`}
        >
          <path d="M4.5 3l3 3-3 3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>

      {expanded && (
        <div className="px-3 pb-3">
          {name === "trace_name" || name === "model" ? (
            <TextFilter
              value={Array.isArray(value) ? value : []}
              onApply={(vals) => onApply(name, vals)}
              placeholder={name === "model" ? "e.g. gpt-4, claude-3…" : "Search trace names…"}
            />
          ) : name === "kind" ? (
            <CheckboxFilter
              options={KNOWN_KINDS}
              value={Array.isArray(value) ? value : []}
              onApply={(vals) => onApply(name, vals)}
            />
          ) : name === "status" ? (
            <CheckboxFilter
              options={["OK", "ERROR"]}
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
  childSpans: Span[];
  bookmarks: Set<string>;
  onToggleBookmark: (traceId: string) => void;
  onNavigateToTrace: (traceId: string) => void;
  onNavigateToTraceDetail: () => void;
}

function TableCellRenderer({
  col,
  span,
  childSpans,
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
          className="text-left block w-full"
          title="View trace waterfall"
        >
          <div className="text-omneval-text-pure font-medium">{span.name}</div>
          <div className="text-omneval-text-muted text-xs font-mono truncate max-w-[120px]">
            {span.trace_id.slice(0, 12)}…
          </div>
        </button>
      );
    case "input":
      return (
        <div key={col.key} className="max-w-[200px] text-omneval-text-muted text-xs font-mono">
          {span.input ? formatJsonPreview(span.input, 40) : "\u2014"}
        </div>
      );
    case "output":
      return (
        <div key={col.key} className="max-w-[200px] text-omneval-text-muted text-xs font-mono">
          {span.output ? formatJsonPreview(span.output, 40) : "\u2014"}
        </div>
      );
    case "observationLevels":
      return <ObservationPills key={col.key} childSpans={childSpans} />;
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
    case "environment":
      return <span key={col.key} className="text-omneval-text-muted text-xs">default</span>;
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
        boxShadow: "0 2px 8px rgba(255, 87, 34, 0.25)",
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
  onNavigateToConversation,
  initialTab,
}: TracesPageProps) {
  const [spans, setSpans] = useState<Span[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loading, setLoading] = useState(false);
  const [pageSize, setPageSize] = useState(25);
  const [searchQuery, setSearchQuery] = useState("");
  const searchQueryRef = useRef(searchQuery);
  // Keep ref in sync so fetchSpans always reads the latest value.
  useEffect(() => {
    searchQueryRef.current = searchQuery;
  }, [searchQuery]);
  const [activeTab, setActiveTab] = useState<"traces" | "conversations">(
    initialTab ?? "traces",
  );
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [columnVisibility, setColumnVisibility] = useState({
    bookmark: true,
    timestamp: true,
    name: true,
    input: true,
    output: true,
    observationLevels: true,
    latency: true,
    tokens: true,
    cost: true,
    environment: true,
  });

  // Filters
  const [expandedFilters, setExpandedFilters] = useState<Record<string, boolean>>({
    trace_name: true,
  });
  const [filterState, setFilterState] = useState<FilterState>(DEFAULT_FILTERS);
  const [bookmarks, setBookmarks] = useState<Set<string>>(new Set());
  const [showColumnsMenu, setShowColumnsMenu] = useState(false);

  // ── Columns Definition ──
  const columns = [
    { key: "bookmark", label: "", visible: columnVisibility.bookmark },
    { key: "timestamp", label: "Timestamp", visible: columnVisibility.timestamp },
    { key: "name", label: "Name", visible: columnVisibility.name },
    { key: "input", label: "Input", visible: columnVisibility.input },
    { key: "output", label: "Output", visible: columnVisibility.output },
    { key: "observationLevels", label: "Levels", visible: columnVisibility.observationLevels },
    { key: "latency", label: "Latency", visible: columnVisibility.latency },
    { key: "tokens", label: "Tokens", visible: columnVisibility.tokens },
    { key: "cost", label: "Cost", visible: columnVisibility.cost },
    { key: "environment", label: "Env", visible: columnVisibility.environment },
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
    environment: "Deployment environment",
  };

  const fetchSpans = useCallback(
    async (cursor: string, append = false) => {
      setLoading(true);
      try {
        const body: SpanQueryRequest = {
          project_id: activeProject,
          from: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString(),
          to: new Date().toISOString(),
          limit: pageSize,
        };

        if (cursor) body.cursor = cursor;

        // Apply text search — always read from the ref to avoid stale
        // closures (e.g. when onChange calls fetchSpans synchronously
        // before React has re-rendered with the new state).
        const currentQuery = searchQueryRef.current;
        if (currentQuery) {
          // "contains" compiles server-side to LIKE '%value%'. The substring
          // wrapping is applied by the query compiler, so the raw term is sent
          // as-is. ("ilike" is not in the operator allowlist and 400s.)
          body.filters = [
            { field: "name", op: "contains", value: currentQuery },
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

        if (res.ok) {
          const data: SpanQueryResponse = await res.json();
          if (append) {
            setSpans((prev) => [...prev, ...(data.spans ?? [])]);
          } else {
            setSpans(data.spans ?? []);
          }
          setNextCursor(data.next ?? "");
        }
      } finally {
        setLoading(false);
      }
    },
    [activeProject, pageSize, filterState, searchQueryRef],
  );

  useEffect(() => {
    fetchSpans("");
  }, [activeProject, pageSize, fetchSpans]);

  // Auto-refresh effect
  useEffect(() => {
    if (!autoRefresh) return;
    const interval = setInterval(() => {
      fetchSpans("", false);
    }, 30000);
    return () => clearInterval(interval);
  }, [autoRefresh, fetchSpans]);

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
    setFilterState((prev) => ({
      ...prev,
      [field]: val,
    }));
    fetchSpans("", false);
  };

  // Reconstruct parent-child relationships from the flat span list.
  const traceGroups = useMemo<Record<string, Span[]>>(() => {
    const groups: Record<string, Span[]> = {};
    spans.forEach((span) => (groups[span.trace_id] ??= []).push(span));
    return groups;
  }, [spans]);

  const visibleColumns = columns.filter((c) => c.visible);

  return (
    <div className="flex h-full" style={{ background: colors.backgrounds.abyssBlack }}>
      {/* ── Filter Sidebar ── */}
      <div
        className="flex flex-col w-64 border-r overflow-y-auto"
        style={{
          background: colors.backgrounds.charcoalDepth,
          borderColor: colors.backgrounds.caveWall,
        }}
      >
        <div className="px-3 py-3 border-b" style={{ borderColor: colors.backgrounds.caveWall }}>
          <h3 className="text-sm font-medium text-omneval-text-pure">Filters</h3>
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
            />
          ))}
        </div>
        <div className="px-3 py-2 border-t" style={{ borderColor: colors.backgrounds.caveWall }}>
          <button
            onClick={() => {
              setFilterState(DEFAULT_FILTERS);
              setExpandedFilters({ trace_name: true });
              fetchSpans("", false);
            }}
            className="w-full text-center text-sm py-1.5 rounded transition-colors"
            style={{
              border: `1px solid ${colors.backgrounds.caveWall}`,
              color: colors.typography.ashGrey,
            }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLElement).style.borderColor = colors.accents.emberFlare;
              (e.currentTarget as HTMLElement).style.color = colors.accents.emberFlare;
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLElement).style.borderColor = colors.backgrounds.caveWall;
              (e.currentTarget as HTMLElement).style.color = colors.typography.ashGrey;
            }}
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
          {/* Tabs */}
          <div className="flex gap-1">
            {(["traces", "conversations"] as const).map((tab) => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                className={`px-3 py-1.5 text-sm rounded-md transition-colors capitalize ${
                  tab === activeTab
                    ? "text-omneval-violet-pale"
                    : "text-omneval-text-muted hover:text-omneval-text-pure"
                }`}
                style={
                  tab === activeTab
                    ? {
                        background: colors.backgrounds.charcoalDepth,
                        borderBottom: `2px solid ${colors.accents.emberFlare}`,
                      }
                    : {}
                }
              >
                {tab}
              </button>
            ))}
          </div>

          {/* Search */}
          <div className="flex-1" />
          {activeTab === "traces" && (
          <input
            type="text"
            placeholder="Search by ID/Name..."
            value={searchQuery}
            onChange={(e) => {
              setSearchQuery(e.target.value);
              searchQueryRef.current = e.target.value;
              fetchSpans("", false);
            }}
            className="input-focus w-48 text-sm px-2.5 py-1.5 rounded border border-omneval-border bg-omneval-depth text-omneval-text-pure placeholder-omneval-text-muted"
          />
          )}

          {/* Auto-refresh toggle */}
          {activeTab === "traces" && (
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
          )}

          {/* Column visibility menu */}
          {activeTab === "traces" && (
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
          )}
        </div>

        {activeTab === "conversations" ? (
          <ConversationsView
            activeProject={activeProject}
            onOpenConversation={onNavigateToConversation}
          />
        ) : (
        <>
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
                  {visibleColumns.map((col) => (
                    <th
                      key={col.key}
                      className="px-3 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap"
                      title={columnTooltips[col.key] ?? col.label}
                    >
                      {col.label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {spans.map((span) => {
                  const childSpans = (
                    traceGroups[span.trace_id] ?? []
                  ).filter((s) => s.parent_id === span.span_id);
                  return (
                    <tr
                      key={span.span_id}
                      className="cursor-pointer transition-colors duration-150"
                      style={{
                        borderBottom: `1px solid ${colors.backgrounds.caveWall}`,
                      }}
                      onMouseEnter={(e) => {
                        (e.currentTarget as HTMLElement).style.background =
                          `rgba(255, 204, 188, 0.1)`;
                      }}
                      onMouseLeave={(e) => {
                        (e.currentTarget as HTMLElement).style.background = "transparent";
                      }}
                    >
                      {visibleColumns.map((col) => (
                        <td key={col.key} className="px-3 py-2.5 whitespace-nowrap">
                          <TableCellRenderer
                            col={col}
                            span={span}
                            childSpans={childSpans}
                            bookmarks={bookmarks}
                            onToggleBookmark={toggleBookmark}
                            onNavigateToTrace={onNavigateToTrace}
                            onNavigateToTraceDetail={onNavigateToTraceDetail}
                          />
                        </td>
                      ))}
                    </tr>
                  );
                })}
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
        </>
        )}
      </div>
    </div>
  );
}

// ── Conversations Tab (issue #67) ───────────────────────────────────

function ConversationsView({
  activeProject,
  onOpenConversation,
}: {
  activeProject: string;
  onOpenConversation: (conversationId: string) => void;
}) {
  const [conversations, setConversations] = useState<ConversationListItem[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loading, setLoading] = useState(false);

  const fetchConversations = useCallback(
    async (cursor: string, append = false) => {
      setLoading(true);
      try {
        const params = new URLSearchParams({
          project_id: activeProject,
          from: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString(),
          to: new Date().toISOString(),
          limit: "50",
        });
        if (cursor) params.set("cursor", cursor);

        let res: Response;
        try {
          res = await fetch(`/api/v1/conversations?${params.toString()}`);
        } catch {
          return;
        }
        if (res.ok) {
          const data: ConversationListResponse = await res.json();
          if (append) {
            setConversations((prev) => [...prev, ...(data.conversations ?? [])]);
          } else {
            setConversations(data.conversations ?? []);
          }
          setNextCursor(data.next ?? "");
        }
      } finally {
        setLoading(false);
      }
    },
    [activeProject],
  );

  useEffect(() => {
    fetchConversations("");
  }, [fetchConversations]);

  const headers = [
    "Conversation ID",
    "Service",
    "Traces",
    "Spans",
    "Started",
    "Duration",
    "Total Cost",
    "Tokens",
  ];

  return (
    <>
      <div className="flex-1 overflow-auto">
        {conversations.length === 0 && !loading ? (
          <EmptyState
            variant="default"
            title="No conversations yet"
            description="Group an agent session's traces by sending a conversation_id from the SDK — e.g. set_active_conversation_id() in Python, WithConversationID() in Go, or Omneval.setActiveConversationId() in TypeScript"
          />
        ) : loading && conversations.length === 0 ? (
          <div className="flex flex-col gap-2 p-4">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-6 rounded" style={{ width: "85%" }} />
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
                {headers.map((h) => (
                  <th
                    key={h}
                    className="px-3 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap"
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {conversations.map((c) => (
                <tr
                  key={c.conversation_id}
                  className="cursor-pointer transition-colors duration-150 hover:bg-omneval-violet-hover"
                  style={{ borderBottom: `1px solid ${colors.backgrounds.caveWall}` }}
                  onClick={() => onOpenConversation(c.conversation_id)}
                  role="button"
                  aria-label={`Open conversation ${c.conversation_id}`}
                >
                  <td className="px-3 py-2.5 whitespace-nowrap">
                    <span className="font-mono text-xs text-omneval-violet-pale">
                      {c.conversation_id.slice(0, 12)}…
                    </span>
                  </td>
                  <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-pure text-xs">
                    {c.service_name || "—"}
                  </td>
                  <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-muted font-mono text-xs">
                    {c.trace_count}
                  </td>
                  <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-muted font-mono text-xs">
                    {c.span_count}
                  </td>
                  <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-muted text-xs">
                    {formatTimeWithYear(c.start_time)}
                  </td>
                  <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-muted text-xs">
                    {formatDuration(c.start_time, c.end_time || c.start_time)}
                  </td>
                  <td
                    className="px-3 py-2.5 whitespace-nowrap font-medium text-xs"
                    style={{ color: colors.accents.emberFlare }}
                  >
                    ${c.total_cost_usd.toFixed(4)}
                  </td>
                  <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-muted font-mono text-xs">
                    {(c.total_input_tokens + c.total_output_tokens).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Pagination */}
      <div
        className="flex items-center justify-end px-4 py-3 border-t"
        style={{ borderColor: colors.backgrounds.caveWall }}
      >
        <button
          disabled={loading || !nextCursor}
          onClick={() => fetchConversations(nextCursor, true)}
          className="text-sm px-4 py-1.5 rounded-md font-medium text-white transition-all duration-150 disabled:opacity-40 disabled:cursor-not-allowed hover:brightness-110 active:brightness-90"
          style={
            nextCursor
              ? {
                  background: colors.accents.emberFlare,
                  boxShadow: "0 2px 8px rgba(255, 87, 34, 0.25)",
                }
              : { background: colors.backgrounds.caveWall, boxShadow: "none" }
          }
        >
          {loading ? "Loading..." : nextCursor ? "Load Next Page" : "No more data"}
        </button>
      </div>
    </>
  );
}
