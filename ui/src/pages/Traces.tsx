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
  from: string;
  to: string;
  limit: number;
  cursor?: string;
  filters?: QueryFilter[];
}

interface QueryFilter {
  field: string;
  op: string;
  value: string | string[];
}

interface FilterState {
  [fieldName: string]: string[];
}

// Observation-level span kinds shown on the Observations tab.
export const OBSERVATION_KINDS: string[] = ["llm", "tool", "agent", "chain"];

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

// ── Filter Sidebar ─────────────────────────────────────────────────

const FILTER_SECTIONS = [
  "environment",
  "trace_name",
  "trace_id",
  "user_id",
  "session_id",
  "tags",
  "latency",
  "tokens",
  "cost",
];

const FILTER_LABELS: Record<string, string> = {
  environment: "Environment",
  trace_name: "Trace Name",
  trace_id: "Trace ID",
  user_id: "User ID",
  session_id: "Session ID",
  tags: "Tags",
  latency: "Latency",
  tokens: "Tokens",
  cost: "Cost",
};

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
  onApply: (val: string[]) => void;
  value: string[];
}) {
  const [input, setInput] = useState(value.join(", "));

  // Sync local input state when the value prop changes from outside.
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

  const applyButton = (
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
  );

  const filterInput = (
    <input
      type="text"
      value={input}
      onChange={(e) => setInput(e.target.value)}
      className="input-focus w-full text-sm px-2 py-1.5 rounded border border-lantern-bg-cave bg-lantern-bg-illumination text-lantern-pure placeholder-lantern-ash"
    />
  );

  return (
    <div
      className="border-b"
      style={{ borderColor: colors.backgrounds.caveWall }}
    >
      <button
        onClick={onToggle}
        className="flex items-center justify-between w-full px-3 py-2 text-sm font-medium text-lantern-pure hover:bg-lantern-accent-flicker-hover transition-colors"
      >
        <span>{label}</span>
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
          {name === "trace_name" ? (
            <div className="space-y-1">
              {["planner", "implementer", "reviewer", "merger", "coder", "architect"].map((traceName) => (
                <label
                  key={traceName}
                  className="flex items-center gap-2 text-sm text-lantern-ash hover:text-lantern-pure cursor-pointer"
                >
                  <input
                    type="checkbox"
                    checked={value.includes(traceName)}
                    onChange={(e) => {
                      const newVals = e.target.checked
                        ? [...value, traceName]
                        : value.filter((v) => v !== traceName);
                      onApply(newVals);
                    }}
                    className="rounded"
                    style={{
                      accentColor: colors.accents.emberFlare,
                    }}
                  />
                  {traceName}
                </label>
              ))}
            </div>
          ) : (
            <div className="space-y-2">
              {filterInput}
              {applyButton}
            </div>
          )}
        </div>
      )}
    </div>
  );
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
        <span key={col.key} className="text-lantern-ash text-xs">
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
          <div className="text-lantern-pure font-medium">{span.name}</div>
          <div className="text-lantern-ash text-xs font-mono truncate max-w-[120px]">
            {span.trace_id.slice(0, 12)}…
          </div>
        </button>
      );
    case "input":
      return (
        <div key={col.key} className="max-w-[200px] text-lantern-ash text-xs font-mono">
          {span.input ? formatJsonPreview(span.input, 40) : "\u2014"}
        </div>
      );
    case "output":
      return (
        <div key={col.key} className="max-w-[200px] text-lantern-ash text-xs font-mono">
          {span.output ? formatJsonPreview(span.output, 40) : "\u2014"}
        </div>
      );
    case "observationLevels":
      return <ObservationPills key={col.key} childSpans={childSpans} />;
    case "latency":
      return (
        <span key={col.key} className="text-lantern-ash">
          {formatDuration(span.start_time, span.end_time)}
        </span>
      );
    case "tokens":
      return (
        <span key={col.key} className="text-lantern-ash">
          {totalTokens(span).toLocaleString()}
          <span className="text-lantern-ash opacity-60 text-xs ml-1">
            ({span.input_tokens}+{span.output_tokens})
          </span>
        </span>
      );
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
      return <span key={col.key} className="text-lantern-ash text-xs">default</span>;
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

  return (
    <div className="flex items-center justify-between px-4 py-3 border-t" style={{ borderColor: colors.backgrounds.caveWall }}>
      <div className="flex items-center gap-2">
        <span className="text-sm text-lantern-ash">Rows per page:</span>
        <select
          value={pageSize}
          onChange={(e) => onPageSizeChange(Number(e.target.value))}
          className="input-focus text-sm px-2 py-1 rounded border border-lantern-bg-cave bg-lantern-bg-illumination text-lantern-pure"
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
          disabled={loading || !hasMore}
          onClick={onLoadNext}
          className="text-sm px-4 py-1.5 rounded-md font-medium text-white transition-all duration-150 disabled:opacity-40 disabled:cursor-not-allowed hover:brightness-110 active:brightness-90"
          style={{
            background: hasMore ? colors.accents.emberFlare : colors.backgrounds.caveWall,
            boxShadow: hasMore ? "0 2px 8px rgba(255, 87, 34, 0.25)" : "none",
          }}
        >
          {loading ? (
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
          )}
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
  const [activeTab, setActiveTab] = useState<"traces" | "observations">("traces");
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
  const [filterState, setFilterState] = useState<FilterState>({});
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
          body.filters = [
            { field: "name", op: "ilike", value: `%${currentQuery}%` },
          ];
        }

        // Observations tab: show only observation-level spans (LLM, tool, agent, chain).
        if (activeTab === "observations") {
          body.filters = body.filters ?? [];
          body.filters.push({ field: "kind", op: "in", value: OBSERVATION_KINDS });
        }

        // Apply filter state
        if (Object.keys(filterState).length > 0) {
          body.filters = body.filters ?? [];
          for (const [field, values] of Object.entries(filterState)) {
            if (values.length > 0) {
              if (field === "trace_name") {
                body.filters.push({ field: "name", op: "in", value: values });
              } else if (field === "latency") {
                const match = values[0]?.match(/[><=]?\s*(\d+)/);
                if (match) {
                  const threshold = parseInt(match[1] ?? "0");
                  const op = values[0]?.includes(">") && !values[0]?.includes(">=")
                    ? "gt"
                    : values[0]?.includes(">=")
                      ? "gte"
                      : values[0]?.includes("<") && !values[0]?.includes("<=")
                        ? "lt"
                        : values[0]?.includes("<=")
                          ? "lte"
                          : "eq";
                  body.filters.push({ field: "duration_ms", op, value: String(threshold) });
                }
              }
            }
          }
        }

        const res = await fetch("/api/v1/spans/query", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });

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
    [pageSize, filterState, searchQueryRef, activeTab],
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

  const applyFilter = (field: string, values: string[]) => {
    setFilterState((prev) => ({
      ...prev,
      [field]: values,
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
          <h3 className="text-sm font-medium text-lantern-pure">Filters</h3>
        </div>
        <div className="flex-1 py-1">
          {FILTER_SECTIONS.map((section) => (
            <FilterSection
              key={section}
              name={section}
              label={FILTER_LABELS[section] ?? section}
              expanded={expandedFilters[section]}
              onToggle={() => toggleFilter(section)}
              onApply={(vals) => applyFilter(section, vals)}
              value={filterState[section] ?? []}
            />
          ))}
        </div>
        <div className="px-3 py-2 border-t" style={{ borderColor: colors.backgrounds.caveWall }}>
          <button
            onClick={() => {
              setFilterState({});
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
            {(["traces", "observations"] as const).map((tab) => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                className={`px-3 py-1.5 text-sm rounded-md transition-colors capitalize ${
                  tab === activeTab
                    ? "text-lantern-ember"
                    : "text-lantern-ash hover:text-lantern-pure"
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
          <input
            type="text"
            placeholder="Search by ID/Name..."
            value={searchQuery}
            onChange={(e) => {
              setSearchQuery(e.target.value);
              searchQueryRef.current = e.target.value;
              fetchSpans("", false);
            }}
            className="input-focus w-48 text-sm px-2.5 py-1.5 rounded border border-lantern-bg-cave bg-lantern-bg-charcoal text-lantern-pure placeholder-lantern-ash"
          />

          {/* Auto-refresh toggle */}
          <label className="flex items-center gap-2 text-sm text-lantern-ash cursor-pointer">
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={(e) => setAutoRefresh(e.target.checked)}
              className="rounded"
              style={{ accentColor: colors.accents.emberFlare }}
            />
            <span className={autoRefresh ? "text-lantern-ember" : ""}>
              30s
            </span>
          </label>

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
              className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium border border-lantern-bg-cave text-lantern-ash hover:text-lantern-pure hover:border-lantern-bg-illumination transition-all duration-150"
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
                      className="flex items-center gap-2.5 w-full px-3 py-1.5 text-sm text-left transition-colors hover:bg-lantern-accent-flicker-hover"
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
                    className="w-full px-3 py-1.5 text-xs text-left transition-colors hover:bg-lantern-accent-flicker-hover"
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
      </div>
    </div>
  );
}
