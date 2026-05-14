import { useState, useEffect, useCallback } from "react";
import { colors } from "@/theme";
import { OnboardingEmptyState } from "@/components/OnboardingEmptyState";
import { Skeleton } from "@/components/Skeleton";
import { EmptyState } from "@/components/EmptyState";
import {
  formatTime,
  formatDuration,
  truncate,
  safeExtractInputOutput,
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
  name: string;
  kind: string;
  model?: string;
  start_time: string;
  end_time: string;
  cost_usd: number;
  input_tokens: number;
  output_tokens: number;
  Children?: Span[];
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

// ── Observation Level Pills ────────────────────────────────────────

interface ObservationPillsProps {
  count: number;
  kind?: string;
}

function ObservationPills({ count, kind }: ObservationPillsProps) {
  if (count <= 0) return null;

  const kindLabel = kind ?? "span";
  const shortLabel = kindLabel.slice(0, 3).toUpperCase();

  return (
    <span
      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium"
      style={{
        background: `${colors.accents.emberFlare}22`,
        color: colors.accents.softGlow,
        border: `1px solid ${colors.accents.emberFlare}44`,
      }}
    >
      {shortLabel} {count}
    </span>
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
      className="w-full text-sm px-2 py-1.5 rounded border bg-lantern-bg-illumination text-lantern-pure placeholder-lantern-ash"
      style={{ borderColor: colors.backgrounds.caveWall }}
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
          {name === "trace_name" && (
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
          )}
          {name === "latency" && (
            <div className="space-y-2">
              {filterInput}
              {applyButton}
            </div>
          )}
          {name !== "trace_name" && name !== "latency" && (
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

// ── Pagination ─────────────────────────────────────────────────────

function PaginationControls({
  pageSize,
  onPageSizeChange,
  hasMore,
  loading,
}: {
  pageSize: number;
  onPageSizeChange: (size: number) => void;
  hasMore: boolean;
  loading: boolean;
}) {
  const sizes = [10, 25, 50, 100];

  return (
    <div className="flex items-center justify-between px-4 py-3 border-t" style={{ borderColor: colors.backgrounds.caveWall }}>
      <div className="flex items-center gap-2">
        <span className="text-sm text-lantern-ash">Rows per page:</span>
        <select
          value={pageSize}
          onChange={(e) => onPageSizeChange(Number(e.target.value))}
          className="text-sm px-2 py-1 rounded border bg-lantern-bg-illumination text-lantern-pure"
          style={{ borderColor: colors.backgrounds.caveWall }}
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
  const [_, setPage] = useState(1);
  const [searchQuery, setSearchQuery] = useState("");
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

        // Apply text search
        if (searchQuery) {
          body.filters = [
            { field: "name", op: "ilike", value: `%${searchQuery}%` },
          ];
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
    [pageSize, searchQuery, filterState],
  );

  const resetAndFetch = useCallback(
    (cursor = "", append = false) => {
      fetchSpans(cursor, append);
    },
    [fetchSpans],
  );

  useEffect(() => {
    resetAndFetch("");
  }, [activeProject, pageSize, resetAndFetch]);

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
    resetAndFetch("", false);
  };

  const totalTokens = (span: Span) => span.input_tokens + span.output_tokens;

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
              expanded={!!expandedFilters[section]}
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
              resetAndFetch("", false);
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
              resetAndFetch("", false);
            }}
            className="w-48 text-sm px-2.5 py-1.5 rounded border bg-lantern-bg-charcoal text-lantern-pure placeholder-lantern-ash"
            style={{ borderColor: colors.backgrounds.caveWall }}
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

          {/* Column toggles */}
          <div className="flex items-center gap-1">
            {columns.map((col) => {
              const isVisible = columnVisibility[col.key as keyof typeof columnVisibility];
              return (
                <button
                  key={col.key}
                  onClick={() => toggleColumn(col.key as keyof typeof columnVisibility)}
                  className={`w-7 h-7 flex items-center justify-center rounded-md text-xs font-semibold transition-all duration-150 border ${
                    isVisible
                      ? "border-lantern-ember bg-lantern-accent-ember-glow text-lantern-ember"
                      : "border-lantern-bg-cave text-lantern-ash/60 hover:text-lantern-pure hover:border-lantern-bg-illumination"
                  }`}
                  title={`Toggle ${col.label}`}
                >
                  {col.label.slice(0, 2).toUpperCase()}
                </button>
              );
            })}
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
                    >
                      {col.label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {spans.map((span) => {
                  const observationCount = span.Children?.length ?? 0;
                  const observationKind = span.kind;
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
                          {col.key === "bookmark" && (
                            <BookmarkStar
                              starred={bookmarks.has(span.trace_id)}
                              onClick={() => toggleBookmark(span.trace_id)}
                            />
                          )}
                          {col.key === "timestamp" && (
                            <span className="text-lantern-ash text-xs">
                              {formatTime(span.start_time)}
                            </span>
                          )}
                          {col.key === "name" && (
                            <button
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
                          )}
                          {col.key === "input" && (
                            <div className="max-w-[200px] truncate text-lantern-ash text-xs font-mono">
                              {span.input ? truncate(safeExtractInputOutput(span.input), 40) : "—"}
                            </div>
                          )}
                          {col.key === "output" && (
                            <div className="max-w-[200px] truncate text-lantern-ash text-xs font-mono">
                              {span.output ? truncate(safeExtractInputOutput(span.output), 40) : "—"}
                            </div>
                          )}
                          {col.key === "observationLevels" && (
                            <ObservationPills count={observationCount} kind={observationKind} />
                          )}
                          {col.key === "latency" && (
                            <span className="text-lantern-ash">
                              {formatDuration(span.start_time, span.end_time)}
                            </span>
                          )}
                          {col.key === "tokens" && (
                            <span className="text-lantern-ash">
                              {totalTokens(span).toLocaleString()}
                              <span className="text-lantern-ash opacity-60 text-xs ml-1">
                                ({span.input_tokens}+{span.output_tokens})
                              </span>
                            </span>
                          )}
                          {col.key === "cost" && (
                            <span
                              className="font-medium"
                              style={{ color: colors.accents.emberFlare }}
                            >
                              ${span.cost_usd.toFixed(4)}
                            </span>
                          )}
                          {col.key === "environment" && (
                            <span className="text-lantern-ash text-xs">default</span>
                          )}
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
            setPage(1);
          }}
          hasMore={!!nextCursor}
          loading={loading}
        />
      </div>
    </div>
  );
}
