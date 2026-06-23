import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  BarChart,
  Bar,
  Cell,
} from "recharts";
import { colors } from "@/theme";
import { ErrorBanner } from "@/components/ErrorBanner";
import { Skeleton } from "@/components/Skeleton";
import { EmptyState, LoadingState } from "@/components/EmptyState";
import {
  formatNumber,
  formatMs,
  timeRangeLabel,
} from "@/utils/formatters";

// ── Types ──────────────────────────────────────────────────────────

interface DashboardPageProps {
  activeProject: string;
  /** Preset key from the Header time-range selector (e.g. "1h", "1d", "7d", "30d", "custom"). */
  timeRange?: string;
}

export interface AnalyticsRequest {
  from: string;
  to: string;
  filters: { field: string; op: string; value: unknown }[];
  group_by: { field: string; truncate?: string; interval?: string }[];
  order_by: { field: string; desc: boolean }[];
  aggregations: { function: string; field: string; alias: string }[];
}

// ── Chart tick formatter ───────────────────────────────────────────

/**
 * Format a chart X-axis tick value for the "Traces over Time" chart.
 *
 * Presets that show sub-day resolution (1h, 6h, 1d) render as a time string
 * ("09:00 AM"); multi-day presets (7d, 30d) render as a short date ("Jun 15").
 *
 * Exported for unit testing (issues #42 / #47).
 */
export function formatTraceTimeTick(val: number, presetKey: string): string {
  const date = new Date(val);
  const timePresets = new Set(["1h", "6h", "1d"]);
  if (timePresets.has(presetKey)) {
    return date.toLocaleTimeString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
      hour12: true,
    });
  }
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}

// ── Constants ──────────────────────────────────────────────────────

const DASHBOARD_DATE_RANGE_DAYS = 7;

/** Map from Header time-range preset values to human-readable labels. */
const TIME_RANGE_LABELS: Record<string, string> = {
  "1h": "Past 1 hour",
  "6h": "Past 6 hours",
  "1d": "Past 24 hours",
  "7d": "Past 7 days",
  "30d": "Past 30 days",
  "custom": "Custom range",
};

/** Map from preset value to millisecond duration. */
const TIME_RANGE_MS: Record<string, number> = {
  "1h": 60 * 60 * 1000,
  "6h": 6 * 60 * 60 * 1000,
  "1d": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
};

// ── Helpers ────────────────────────────────────────────────────────

/** Compute from/to ISO strings for a given preset value. */
function presetToFromTo(preset: string | undefined): { from: string; to: string } {
  const now = new Date();
  const durationMs = preset && TIME_RANGE_MS[preset]
    ? TIME_RANGE_MS[preset]
    : DASHBOARD_DATE_RANGE_DAYS * 24 * 60 * 60 * 1000;
  const from = new Date(now.getTime() - durationMs);
  return {
    from: from.toISOString(),
    to: now.toISOString(),
  };
}

/**
 * Format an ISO timestamp for a datetime-local input, which expects
 * *local* wall-clock time ("YYYY-MM-DDTHH:mm") with no timezone suffix.
 * Slicing toISOString() here would display UTC and shift the picker by
 * the user's UTC offset.
 */
function toLocalInputValue(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Parse a datetime-local input value (local time) back to a UTC ISO string. */
function fromLocalInputValue(value: string): string | null {
  const d = new Date(value);
  return isNaN(d.getTime()) ? null : d.toISOString();
}

// ── Card Wrapper ───────────────────────────────────────────────────

function Card({ title, children, subtitle }: { title: string; children: React.ReactNode; subtitle?: string }) {
  return (
    <div className="card">
      <div className="card-header">
        <div className="flex items-center justify-between">
          <span>{title}</span>
          {subtitle && (
            <span className="text-xs font-normal text-omneval-text-muted opacity-70">{subtitle}</span>
          )}
        </div>
      </div>
      <div className="p-4">{children}</div>
    </div>
  );
}

// ── KPI Tile ──────────────────────────────────────────────────────

interface KPIValue {
  totalCost: number;
  totalTraces: number;
  errorRate: number;
  avgLatencyMs: number;
}

interface KPITileProps {
  label: string;
  value: string;
  subtitle: string;
  color: string;
  icon: React.ReactNode;
}

function KPITile({ label, value, subtitle, color, icon }: KPITileProps) {
  return (
    <div className="rounded-lg p-4 text-center transition-all duration-200 hover:brightness-125"
      style={{
        background: `${color}15`,
        border: `1px solid ${color}30`,
      }}
    >
      <div className="flex items-center justify-center gap-1.5 mb-2">
        <span style={{ color }}>{icon}</span>
        <span className="text-xs font-medium text-omneval-text-muted uppercase tracking-wider">{label}</span>
      </div>
      <div className="text-2xl font-semibold mb-1" style={{ color }}>
        {value}
      </div>
      <div className="text-xs text-omneval-text-muted">{subtitle}</div>
    </div>
  );
}

// ── KPI Tiles ──────────────────────────────────────────────────────

const KPISVGIcons = {
  dollar: (
    <svg width="16" height="16" viewBox="0 0 48 48" fill="none">
      <circle cx="24" cy="24" r="18" stroke="currentColor" strokeWidth="2" />
      <path d="M24 12v24M19 17h7.5a4.5 4.5 0 010 9h-5a4.5 4.5 0 000 9H29" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  ),
  traces: (
    <svg width="16" height="16" viewBox="0 0 48 48" fill="none">
      <rect x="6" y="28" width="8" height="14" rx="1" stroke="currentColor" strokeWidth="2" />
      <rect x="20" y="18" width="8" height="24" rx="1" stroke="currentColor" strokeWidth="2" />
      <rect x="34" y="10" width="8" height="32" rx="1" stroke="currentColor" strokeWidth="2" />
      <path d="M4 44h40" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  ),
  error: (
    <svg width="16" height="16" viewBox="0 0 48 48" fill="none">
      <circle cx="24" cy="24" r="18" stroke="currentColor" strokeWidth="2" />
      <path d="M24 16v12M24 32v0" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  ),
  latency: (
    <svg width="16" height="16" viewBox="0 0 48 48" fill="none">
      <circle cx="24" cy="24" r="18" stroke="currentColor" strokeWidth="2" />
      <path d="M24 14v10l7 5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  ),
};

function KPITiles({ data, loading }: { data: KPIValue | null; loading: boolean }) {
  if (loading || !data) {
    return (
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        {[...Array(4)].map((_, i) => (
          <div key={i} className="rounded-lg p-4" style={{ background: colors.backgrounds.surface }}>
            <Skeleton className="h-4 rounded mb-2" />
            <Skeleton className="h-8 rounded mb-1" />
            <Skeleton className="h-3 rounded" />
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
      <KPITile
        label="Total Cost"
        value={`$${data.totalCost.toFixed(2)}`}
        subtitle="sum of cost_usd"
        color={colors.accents.violet}
        icon={KPISVGIcons.dollar}
      />
      <KPITile
        label="Total Traces"
        value={formatNumber(data.totalTraces)}
        subtitle="count of spans"
        color={colors.chartColors.series[0]}
        icon={KPISVGIcons.traces}
      />
      <KPITile
        label="Error Rate"
        value={`${(data.errorRate * 100).toFixed(2)}%`}
        subtitle="errors / total"
        color={data.errorRate > 0.1 ? colors.accents.dangerRed : colors.accents.greenSuccess}
        icon={KPISVGIcons.error}
      />
      <KPITile
        label="Average Latency"
        value={formatMs(data.avgLatencyMs)}
        subtitle="avg duration_ms"
        color={colors.chartColors.series[2]}
        icon={KPISVGIcons.latency}
      />
    </div>
  );
}

// ── Shared Chart Config ────────────────────────────────────────────

const chartTooltipStyle: React.CSSProperties = {
  backgroundColor: colors.backgrounds.depth,
  border: `1px solid ${colors.backgrounds.border}`,
  borderRadius: "0.375rem",
  color: colors.typography.pureLight,
  fontSize: "0.875rem",
};

const gridColor = colors.backgrounds.border;

/** Series color index for multi-chart consistency. */
const SERIES_EMBER = 0;

// ── Chart: Traces by Name (Horizontal Bar) ─────────────────────────

interface TracesByNameData {
  name: string;
  count: number;
}

const BarChartIcon = () => (
  <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
    <rect x="6" y="28" width="8" height="14" rx="1" stroke="currentColor" strokeWidth="2" />
    <rect x="20" y="18" width="8" height="24" rx="1" stroke="currentColor" strokeWidth="2" />
    <rect x="34" y="10" width="8" height="32" rx="1" stroke="currentColor" strokeWidth="2" />
    <path d="M4 44h40" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
  </svg>
);

const DollarIcon = () => (
  <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
    <circle cx="24" cy="24" r="18" stroke="currentColor" strokeWidth="2" />
    <path d="M24 12v24M19 17h7.5a4.5 4.5 0 010 9h-5a4.5 4.5 0 000 9H29" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
  </svg>
);

const StarIcon = () => (
  <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
    <path d="M24 6l4.5 9 10 1.5-7.25 7 1.75 10L24 29l-9 4.5 1.75-10L9.5 16.5l10-1.5L24 6z"
      stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
  </svg>
);

const ClockIcon = () => (
  <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
    <circle cx="24" cy="24" r="18" stroke="currentColor" strokeWidth="2" />
    <path d="M24 14v10l7 5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
);

const ChipIcon = () => (
  <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
    <rect x="14" y="14" width="20" height="20" rx="2" stroke="currentColor" strokeWidth="2" />
    <path d="M20 14v-4M28 14v-4M20 38v-4M28 38v-4M14 20H10M14 28H10M38 20h-4M38 28h-4" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    <rect x="18" y="18" width="12" height="12" rx="1" stroke="currentColor" strokeWidth="1.5" />
  </svg>
);

const UserGroupIcon = () => (
  <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
    <circle cx="18" cy="18" r="7" stroke="currentColor" strokeWidth="2" />
    <path d="M4 40c0-7.732 6.268-14 14-14s14 6.268 14 14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    <circle cx="34" cy="16" r="5" stroke="currentColor" strokeWidth="2" />
    <path d="M44 38c0-5.523-4.477-10-10-10" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
  </svg>
);

// ── Category Y-axis tick (ellipsizes long labels) ──────────────────

/** Width of the category-axis gutter for horizontal bar charts. */
const Y_AXIS_WIDTH = 150;
/** Max characters before a category label is ellipsized to fit the gutter. */
const Y_AXIS_LABEL_MAX = 22;

interface TickProps {
  x?: number;
  y?: number;
  payload?: { value?: string | number };
}

/**
 * Category-axis tick that ellipsizes long labels so model names with a provider
 * prefix (e.g. "openai/qwen3.6-27b-mtp") and service paths (e.g.
 * "/usr/local/bin/agent-entrypoint.py") stay inside the axis gutter instead of
 * overflowing off the card edge. The full value is exposed via an SVG <title>
 * tooltip on hover.
 */
function TruncatedYTick({ x = 0, y = 0, payload }: TickProps) {
  const full = String(payload?.value ?? "");
  const label =
    full.length > Y_AXIS_LABEL_MAX ? `${full.slice(0, Y_AXIS_LABEL_MAX - 1)}…` : full;
  return (
    <g transform={`translate(${x},${y})`}>
      <title>{full}</title>
      <text x={-6} y={0} dy={4} textAnchor="end" fill={colors.typography.pureLight} fontSize={12}>
        {label}
      </text>
    </g>
  );
}

function TracesByNameChart({ data, loading }: { data: TracesByNameData[]; loading: boolean }) {
  if (loading && data.length === 0) {
    return <LoadingState rows={5} rowHeight="1.25rem" />;
  }

  if (data.length === 0) {
    return (
      <EmptyState
        variant="default"
        icon={<BarChartIcon />}
        title="No traces yet"
        description="Send your first trace to see model breakdown here"
        actionLabel="View Traces"
      />
    );
  }

  return (
    <ResponsiveContainer width="100%" height={280}>
      <BarChart data={data} layout="vertical" margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
        <XAxis
          type="number"
          tick={{ fill: colors.typography.ashGrey, fontSize: 12 }}
          axisLine={{ stroke: gridColor }}
        />
        <YAxis
          type="category"
          dataKey="name"
          tick={<TruncatedYTick />}
          width={Y_AXIS_WIDTH}
        />
        <Tooltip
          contentStyle={chartTooltipStyle}
          formatter={(value: number) => [formatNumber(value), "Traces"]}
        />
        <Bar dataKey="count" radius={[0, 4, 4, 0]}>
          {data.map((_entry, index) => (
            <Cell
              key={`cell-${index}`}
              fill={colors.chartColors.series[index % colors.chartColors.series.length]}
              fillOpacity={colors.chartColors.barOpacity(index, data.length)}
            />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  );
}

// ── Chart: Traces by Time (Line Graph) ─────────────────────────────

interface TimeSeriesData {
  time: number;
  count: number;
}

/** Extract the raw epoch timestamp in milliseconds from a row returned by a grouped analytics query. */
function extractRawTime(row: Record<string, unknown>): number {
  for (const key of [
    "date_trunc('hour', start_time)",
    "date_trunc('hour',start_time)",
    "start_time",
  ]) {
    if (row[key] !== undefined && row[key] !== null) {
      const t = new Date(row[key] as string).getTime();
      if (!isNaN(t)) return t;
    }
  }
  for (const [_key, value] of Object.entries(row)) {
    if (typeof value === "string" && value.includes("-")) {
      const t = new Date(value).getTime();
      if (!isNaN(t)) return t;
    }
  }
  return 0;
}

interface TracesByTimeChartProps {
  data: TimeSeriesData[];
  loading: boolean;
  from: string;
  to: string;
  timeRange?: string;
}

function TracesByTimeChart({ data, loading, from, to, timeRange }: TracesByTimeChartProps) {
  if (loading && data.length === 0) {
    return <LoadingState rows={1} rowHeight="8rem" />;
  }

  if (data.length === 0) {
    return (
      <EmptyState
        variant="default"
        icon={<ClockIcon />}
        title="No time-series data"
        description="Trace activity will appear here once data is available"
      />
    );
  }

  const fromMs = new Date(from).getTime();
  const toMs = new Date(to).getTime();
  const diffMs = toMs - fromMs;

  const presets = [
    { key: "1h", ms: 60 * 60 * 1000 },
    { key: "6h", ms: 6 * 60 * 60 * 1000 },
    { key: "1d", ms: 24 * 60 * 60 * 1000 },
    { key: "7d", ms: 7 * 24 * 60 * 60 * 1000 },
    { key: "30d", ms: 30 * 24 * 60 * 60 * 1000 },
  ];

  let presetKey = timeRange;
  if (!presetKey || presetKey === "custom" || !presets.some(p => p.key === presetKey)) {
    let minDiff = Infinity;
    let closestKey = "7d";
    for (const p of presets) {
      const d = Math.abs(diffMs - p.ms);
      if (d < minDiff) {
        minDiff = d;
        closestKey = p.key;
      }
    }
    presetKey = closestKey;
  }

  let intervalMs = 24 * 60 * 60 * 1000; // default 1 day
  if (presetKey === "1h") {
    intervalMs = 15 * 60 * 1000;
  } else if (presetKey === "6h") {
    intervalMs = 60 * 60 * 1000;
  } else if (presetKey === "1d") {
    intervalMs = 4 * 60 * 60 * 1000;
  } else if (presetKey === "7d") {
    intervalMs = 24 * 60 * 60 * 1000;
  } else if (presetKey === "30d") {
    intervalMs = 5 * 24 * 60 * 60 * 1000;
  }

  const ticks: number[] = [];
  let current = fromMs;
  while (current < toMs - intervalMs / 2) {
    ticks.push(current);
    current += intervalMs;
  }
  ticks.push(toMs);

  const tickFormatter = (val: number) => formatTraceTimeTick(val, presetKey);

  return (
    <ResponsiveContainer width="100%" height={280}>
      <LineChart data={data} margin={{ top: 5, right: 20, left: 10, bottom: 15 }}>
        <CartesianGrid strokeDasharray="3 3" stroke={gridColor} />
        <XAxis
          type="number"
          dataKey="time"
          domain={[fromMs, toMs]}
          ticks={ticks}
          tickFormatter={tickFormatter}
          tick={{ fill: colors.typography.ashGrey, fontSize: 11 }}
          axisLine={{ stroke: gridColor }}
          tickLine={false}
          angle={0}
          textAnchor="middle"
          height={30}
        />
        <YAxis
          tick={{ fill: colors.typography.ashGrey, fontSize: 12 }}
          axisLine={{ stroke: gridColor }}
          tickLine={false}
        />
        <Tooltip
          contentStyle={chartTooltipStyle}
          formatter={(value: number) => [formatNumber(value), "Traces"]}
          labelFormatter={(val: number) =>
            new Date(val).toLocaleString(undefined, {
              month: "short",
              day: "numeric",
              hour: "2-digit",
              minute: "2-digit",
            })
          }
        />
        <Line
          type="monotone"
          dataKey="count"
          stroke={colors.chartColors.series[SERIES_EMBER]}
          strokeWidth={2.5}
          dot={{ fill: colors.chartColors.series[SERIES_EMBER], r: 3 }}
          activeDot={{ r: 5, fill: colors.chartColors.series[SERIES_EMBER] }}
        />
      </LineChart>
    </ResponsiveContainer>
  );
}

// ── Table: Model Costs ─────────────────────────────────────────────

interface CostData {
  model: string;
  inputTokens: number;
  outputTokens: number;
  totalCost: number;
}

function ModelCostsTable({
  data,
  loading,
  labelHeader = "Model",
}: {
  data: CostData[];
  loading: boolean;
  labelHeader?: string;
}) {
  const sorted = useMemo(
    () => [...data].sort((a, b) => b.totalCost - a.totalCost),
    [data],
  );

  if (loading && data.length === 0) {
    return <LoadingState rows={5} rowHeight="1.5rem" />;
  }

  if (data.length === 0) {
    return (
      <EmptyState
        variant="default"
        icon={<DollarIcon />}
        title="No cost data yet"
        description="Cost breakdown will appear once traces are ingested"
      />
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr
            className="border-b"
            style={{
              borderBottom: `1px solid ${colors.backgrounds.border}`,
              color: colors.typography.ashGrey,
            }}
          >
            <th className="text-left py-2 px-3 font-medium text-xs uppercase tracking-wider">
              {labelHeader}
            </th>
            <th className="text-right py-2 px-3 font-medium text-xs uppercase tracking-wider">
              Input Tokens
            </th>
            <th className="text-right py-2 px-3 font-medium text-xs uppercase tracking-wider">
              Output Tokens
            </th>
            <th className="text-right py-2 px-3 font-medium text-xs uppercase tracking-wider">
              Total Cost
            </th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((row, i) => (
            <tr
              key={row.model}
              className="transition-colors duration-150"
              style={{
                borderBottom: `1px solid ${colors.backgrounds.border}`,
                background: i % 2 === 0 ? "transparent" : `${colors.backgrounds.surface}33`,
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLElement).style.background =
                  `rgba(124, 58, 237, 0.1)`;
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLElement).style.background =
                  i % 2 === 0 ? "transparent" : `${colors.backgrounds.surface}33`;
              }}
            >
              <td className="py-2 px-3 font-medium text-omneval-text-pure">{row.model}</td>
              <td className="py-2 px-3 text-right text-omneval-text-muted">
                {formatNumber(row.inputTokens)}
              </td>
              <td className="py-2 px-3 text-right text-omneval-text-muted">
                {formatNumber(row.outputTokens)}
              </td>
              <td
                className="py-2 px-3 text-right font-medium"
                style={{ color: colors.accents.violet }}
              >
                ${row.totalCost.toFixed(4)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ── Chart: Scores (Empty State) ────────────────────────────────────

function ScoresWidget({ loading }: { loading: boolean }) {
  if (loading) {
    return (
      <div className="space-y-3 py-4">
        <Skeleton className="h-6 rounded" />
        <Skeleton className="h-4 rounded" />
      </div>
    );
  }

  return (
    <EmptyState
      variant="default"
      icon={<StarIcon />}
      title="No scores yet"
      description="Configure evaluation rules to start scoring traces"
      actionLabel="Go to Settings"
    />
  );
}

// ── Tabbed Widget: Model Usage ─────────────────────────────────────

const USAGE_TABS = ["Cost by Model", "Cost by Type", "Usage by Model", "Usage by Type"];

function ModelUsageWidget({
  loading,
  data,
  typeData,
}: {
  loading: boolean;
  /** Model-grouped data, used by the "by Model" tabs (0, 2). */
  data: CostData[];
  /** Kind-grouped data (span kind: llm/tool/agent/chain), used by the "by Type" tabs (1, 3). */
  typeData: CostData[];
}) {
  const [activeTab, setActiveTab] = useState(0);

  if (loading) {
    return <LoadingState rows={3} rowHeight="2.5rem" />;
  }

  // "by Type" tabs (1, 3) use the kind-grouped data; "by Model" tabs (0, 2)
  // use the model-grouped data.
  const isTypeTab = activeTab === 1 || activeTab === 3;
  const activeData = isTypeTab ? typeData : data;
  const isEmpty = activeData.length === 0;

  // Tokens-only tabs: "Usage by Model" and "Usage by Type".
  const showTokensOnly = activeTab === 2 || activeTab === 3;

  return (
    <div>
      <div className="flex gap-1 mb-4 border-b" style={{ borderColor: colors.backgrounds.border }}>
        {USAGE_TABS.map((tab, i) => (
          <button
            key={tab}
            onClick={() => setActiveTab(i)}
            className={`px-3 py-1.5 text-sm rounded-t-md transition-colors ${
              i === activeTab
                ? "text-omneval-violet-pale"
                : "text-omneval-text-muted hover:text-omneval-text-pure"
            }`}
            style={
              i === activeTab
                ? {
                    borderBottom: `2px solid ${colors.chartColors.series[SERIES_EMBER]}`,
                    paddingBottom: "0.375rem",
                  }
                : { borderBottom: "2px solid transparent" }
            }
          >
            {tab}
          </button>
        ))}
      </div>
      {isEmpty ? (
        <EmptyState
          variant="default"
          icon={<ChipIcon />}
          title="No usage data yet"
          description="Token counts will appear once traces are ingested"
        />
      ) : showTokensOnly ? (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr
                className="border-b"
                style={{
                  borderBottom: `1px solid ${colors.backgrounds.border}`,
                  color: colors.typography.ashGrey,
                }}
              >
                <th className="text-left py-2 px-3 font-medium text-xs uppercase tracking-wider">
                  {activeTab === 2 ? "Model" : "Type"}
                </th>
                <th className="text-right py-2 px-3 font-medium text-xs uppercase tracking-wider">
                  Input Tokens
                </th>
                <th className="text-right py-2 px-3 font-medium text-xs uppercase tracking-wider">
                  Output Tokens
                </th>
              </tr>
            </thead>
            <tbody>
              {activeData
                .sort((a, b) => b.inputTokens + b.outputTokens - (a.inputTokens + a.outputTokens))
                .map((row, i) => (
                  <tr
                    key={row.model}
                    className="transition-colors duration-150"
                    style={{
                      borderBottom: `1px solid ${colors.backgrounds.border}`,
                      background: i % 2 === 0 ? "transparent" : `${colors.backgrounds.surface}33`,
                    }}
                    onMouseEnter={(e) => {
                      (e.currentTarget as HTMLElement).style.background =
                        `rgba(124, 58, 237, 0.1)`;
                    }}
                    onMouseLeave={(e) => {
                      (e.currentTarget as HTMLElement).style.background =
                        i % 2 === 0 ? "transparent" : `${colors.backgrounds.surface}33`;
                    }}
                  >
                    <td className="py-2 px-3 font-medium text-omneval-text-pure">{row.model}</td>
                    <td className="py-2 px-3 text-right text-omneval-text-muted">
                      {formatNumber(row.inputTokens)}
                    </td>
                    <td className="py-2 px-3 text-right text-omneval-text-muted">
                      {formatNumber(row.outputTokens)}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      ) : (
        <ModelCostsTable
          data={activeData}
          loading={false}
          labelHeader={activeTab === 1 ? "Type" : "Model"}
        />
      )}
    </div>
  );
}

// ── Chart: User Consumption ────────────────────────────────────────

interface UserConsumptionData {
  user: string;
  count: number;
}

function UserConsumptionChart({ data, loading }: { data: UserConsumptionData[]; loading: boolean }) {
  if (loading && data.length === 0) {
    return <LoadingState rows={5} rowHeight="1.25rem" />;
  }

  if (data.length === 0) {
    return (
      <EmptyState
        variant="default"
        icon={<UserGroupIcon />}
        title="No consumption data"
        description="Trace counts per service will appear here"
      />
    );
  }

  return (
    <ResponsiveContainer width="100%" height={280}>
      <BarChart data={data} layout="vertical" margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
        <XAxis
          type="number"
          tick={{ fill: colors.typography.ashGrey, fontSize: 12 }}
          axisLine={{ stroke: gridColor }}
        />
        <YAxis
          type="category"
          dataKey="user"
          tick={<TruncatedYTick />}
          width={Y_AXIS_WIDTH}
        />
        <Tooltip
          contentStyle={chartTooltipStyle}
          formatter={(value: number) => [formatNumber(value), "Traces"]}
        />
        <Bar dataKey="count" radius={[0, 4, 4, 0]}>
          {data.map((_entry, index) => (
            <Cell
              key={`cell-${index}`}
              fill={colors.chartColors.series[index % colors.chartColors.series.length]}
              fillOpacity={colors.chartColors.barOpacity(index, data.length)}
            />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  );
}

// ── Chart: Latency Percentiles (p50/p95/p99) ──────────────────────

interface LatencyPercentileData {
  time: number;
  p50_ms: number;
  p95_ms: number;
  p99_ms: number;
}

const LatencyPercentileChart = ({ data, loading }: { data: LatencyPercentileData[]; loading: boolean }) => {
  if (loading && data.length === 0) {
    return <LoadingState rows={1} rowHeight="8rem" />;
  }

  if (data.length === 0) {
    return (
      <EmptyState
        variant="default"
        icon={<ClockIcon />}
        title="No latency data"
        description="Latency percentiles will appear once traces are ingested"
      />
    );
  }

  return (
    <div>
      {/* Legend */}
      <div className="flex gap-4 mb-3 text-xs text-omneval-text-muted">
        <span className="flex items-center gap-1.5">
          <span className="w-3 h-0.5 rounded" style={{ background: colors.chartColors.series[0] }}></span>
          p50
        </span>
        <span className="flex items-center gap-1.5">
          <span className="w-3 h-0.5 rounded" style={{ background: colors.chartColors.series[1] }}></span>
          p95
        </span>
        <span className="flex items-center gap-1.5">
          <span className="w-3 h-0.5 rounded" style={{ background: colors.chartColors.series[2] }}></span>
          p99
        </span>
      </div>
      <ResponsiveContainer width="100%" height={200}>
        <LineChart data={data} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" stroke={gridColor} />
          <XAxis
            type="number"
            dataKey="time"
            tick={{ fill: colors.typography.ashGrey, fontSize: 11 }}
            axisLine={{ stroke: gridColor }}
            tickLine={false}
            tickFormatter={(val: number) => formatTraceTimeTick(val, "1d")}
          />
          <YAxis
            tick={{ fill: colors.typography.ashGrey, fontSize: 11 }}
            axisLine={{ stroke: gridColor }}
            tickLine={false}
            tickFormatter={(val: number) => `${Math.round(val)}ms`}
          />
          <Tooltip
            contentStyle={chartTooltipStyle}
            formatter={(value: number, name: string) => [`${Math.round(value)}ms`, name]}
            labelFormatter={(val: number) => new Date(val).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}
          />
          <Line type="monotone" dataKey="p50_ms" name="p50" stroke={colors.chartColors.series[0]} strokeWidth={2} dot={false} activeDot={{ r: 4 }} />
          <Line type="monotone" dataKey="p95_ms" name="p95" stroke={colors.chartColors.series[1]} strokeWidth={2} dot={false} activeDot={{ r: 4 }} />
          <Line type="monotone" dataKey="p99_ms" name="p99" stroke={colors.chartColors.series[2]} strokeWidth={2} dot={false} activeDot={{ r: 4 }} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
};

// ── Chart: Error Rate ─────────────────────────────────────────────

interface ErrorRateData {
  time: number;
  total_count: number;
  error_count: number;
}

const ErrorRateChart = ({ data, loading }: { data: ErrorRateData[]; loading: boolean }) => {
  if (loading && data.length === 0) {
    return <LoadingState rows={1} rowHeight="8rem" />;
  }

  if (data.length === 0) {
    return (
      <EmptyState
        variant="default"
        icon={<StarIcon />}
        title="No error data"
        description="Error rate will appear once traces are ingested"
      />
    );
  }

  // Compute percentage from raw counts
  const chartData = data
    .map((d) => ({
      time: d.time,
      errorRate: d.total_count > 0 ? (d.error_count / d.total_count) * 100 : 0,
    }))
    .filter((d) => d.time > 0);

  return (
    <ResponsiveContainer width="100%" height={200}>
      <LineChart data={chartData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
        <CartesianGrid strokeDasharray="3 3" stroke={gridColor} />
        <XAxis
          type="number"
          dataKey="time"
          tick={{ fill: colors.typography.ashGrey, fontSize: 11 }}
          axisLine={{ stroke: gridColor }}
          tickLine={false}
          tickFormatter={(val: number) => formatTraceTimeTick(val, "1d")}
        />
        <YAxis
          tick={{ fill: colors.typography.ashGrey, fontSize: 11 }}
          axisLine={{ stroke: gridColor }}
          tickLine={false}
          tickFormatter={(val: number) => `${Math.round(val)}%`}
        />
        <Tooltip
          contentStyle={chartTooltipStyle}
          formatter={(value: number) => [`${Number(value).toFixed(2)}%`, "Error Rate"]}
          labelFormatter={(val: number) => new Date(val).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}
        />
        <Line
          type="monotone"
          dataKey="errorRate"
          name="Error Rate"
          stroke={colors.accents.dangerRed}
          strokeWidth={2}
          dot={false}
          activeDot={{ r: 4 }}
        />
      </LineChart>
    </ResponsiveContainer>
  );
};

// ── Main Dashboard ─────────────────────────────────────────────────

export default function DashboardPage({ activeProject, timeRange }: DashboardPageProps) {
  const defaultFromTo = presetToFromTo(timeRange);
  const [from, setFrom] = useState(defaultFromTo.from);
  const [to, setTo] = useState(defaultFromTo.to);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // When the timeRange preset changes (from the Header), recompute from/to.
  useEffect(() => {
    if (timeRange && timeRange !== "custom") {
      const { from: newFrom, to: newTo } = presetToFromTo(timeRange);
      setFrom(newFrom);
      setTo(newTo);
    }
  }, [timeRange]);

  // Dashboard state
  const [tracesByName, setTracesByName] = useState<TracesByNameData[]>([]);
  const [tracesByTime, setTracesByTime] = useState<TimeSeriesData[]>([]);
  const [userConsumption, setUserConsumption] = useState<UserConsumptionData[]>([]);

  // Token Usage tab data (model-grouped, for "by Model" tabs)
  const [tokenUsageData, setTokenUsageData] = useState<CostData[]>([]);
  // Token Usage tab data (kind-grouped, for "by Type" tabs)
  const [kindUsageData, setKindUsageData] = useState<CostData[]>([]);

  // KPI state (total cost, traces, error rate, avg latency)
  const [kpiData, setKpiData] = useState<KPIValue | null>(null);
  // Latency percentile state
  const [latencyData, setLatencyData] = useState<LatencyPercentileData[]>([]);
  // Error rate state
  const [errorRateData, setErrorRateData] = useState<ErrorRateData[]>([]);

  // Monotonic sequence so a slow in-flight response (e.g. for a previous
  // project) can never overwrite the latest results.
  const requestSeq = useRef(0);

  const fetchData = useCallback(async () => {
    const seq = ++requestSeq.current;
    setLoading(true);
    setError(null);
    try {
      const body = {
        project_id: activeProject,
        from,
        to,
        filters: [],
        group_by: [],
        order_by: [],
      };

      // ── Traces by Name ──
      const tracesByNameReq: AnalyticsRequest = {
        ...body,
        group_by: [{ field: "model" }],
        order_by: [{ field: "count", desc: true }],
        aggregations: [
          { function: "count", field: "*", alias: "count" },
        ],
      };

      // ── Traces by Time ──
      let tracesByTimeGroupBy: AnalyticsRequest["group_by"] = [{ field: "start_time", truncate: "hour" }];
      const diffMs = new Date(to).getTime() - new Date(from).getTime();
      if (diffMs <= 1.5 * 60 * 60 * 1000) {
        tracesByTimeGroupBy = [{ field: "time_bucket", interval: "5m" }];
      } else if (diffMs <= 25 * 60 * 60 * 1000) {
        tracesByTimeGroupBy = [{ field: "time_bucket", interval: "1h" }];
      } else {
        tracesByTimeGroupBy = [{ field: "time_bucket", interval: "1d" }];
      }

      const tracesByTimeReq: AnalyticsRequest = {
        ...body,
        group_by: tracesByTimeGroupBy,
        order_by: [{ field: "start_time", desc: false }],
        aggregations: [
          { function: "count", field: "*", alias: "count" },
        ],
      };

      // ── Token Usage: grouped by model ──
      const tokenUsageReq: AnalyticsRequest = {
        ...body,
        group_by: [{ field: "model" }],
        order_by: [{ field: "total_cost", desc: true }],
        aggregations: [
          { function: "sum", field: "input_tokens", alias: "input_tokens" },
          { function: "sum", field: "output_tokens", alias: "output_tokens" },
          { function: "sum", field: "cost_usd", alias: "total_cost" },
        ],
      };

      // ── Token Usage: grouped by kind (span type: llm/tool/agent/chain) ──
      const kindUsageReq: AnalyticsRequest = {
        ...body,
        group_by: [{ field: "kind" }],
        order_by: [{ field: "total_cost", desc: true }],
        aggregations: [
          { function: "sum", field: "input_tokens", alias: "input_tokens" },
          { function: "sum", field: "output_tokens", alias: "output_tokens" },
          { function: "sum", field: "cost_usd", alias: "total_cost" },
        ],
      };

      // ── User Consumption (by service_name) ──
      const userConsumptionReq: AnalyticsRequest = {
        ...body,
        group_by: [{ field: "service_name" }],
        order_by: [{ field: "count", desc: true }],
        aggregations: [
          { function: "count", field: "*", alias: "count" },
        ],
      };

      // ── KPI: total cost, traces, error rate, avg latency ──
      const kpiReq: AnalyticsRequest = {
        ...body,
        group_by: [],
        aggregations: [
          { function: "count", field: "*", alias: "total_traces" },
          { function: "sum", field: "cost_usd", alias: "total_cost" },
          { function: "count", field: "status_code", alias: "error_rate" },
          { function: "avg", field: "duration_ms", alias: "avg_latency_ms" },
        ],
      };

      // ── Latency Percentiles: p50/p95/p99 over time ──
      const latencyPercentilesReq: AnalyticsRequest = {
        ...body,
        group_by: [
          { field: "time_bucket", interval: "1h" },
        ],
        order_by: [{ field: "start_time", desc: false }],
        aggregations: [
          { function: "p50", field: "duration_ms", alias: "p50_ms" },
          { function: "p95", field: "duration_ms", alias: "p95_ms" },
          { function: "p99", field: "duration_ms", alias: "p99_ms" },
        ],
      };

      // ── Error Rate: total count + error count per time bucket ──
      const errorRateReq: AnalyticsRequest = {
        ...body,
        filters: [
          { field: "status_code", op: "neq", value: "OK" },
        ],
        group_by: [
          { field: "time_bucket", interval: "1h" },
        ],
        order_by: [{ field: "start_time", desc: false }],
        aggregations: [
          { function: "count", field: "*", alias: "total_count" },
          { function: "count", field: "status_code", alias: "error_count" },
        ],
      };

      const [
        tracesByNameResp,
        tracesByTimeResp,
        tokenUsageResp,
        kindUsageResp,
        userConsumptionResp,
        kpiResp,
        latencyResp,
        errorRateResp,
      ] = await Promise.all([
        fetch("/api/v1/analytics/spans", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(tracesByNameReq),
        }),
        fetch("/api/v1/analytics/spans", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(tracesByTimeReq),
        }),
        fetch("/api/v1/analytics/spans", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(tokenUsageReq),
        }),
        fetch("/api/v1/analytics/spans", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(kindUsageReq),
        }),
        fetch("/api/v1/analytics/spans", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(userConsumptionReq),
        }),
        fetch("/api/v1/analytics/spans", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(kpiReq),
        }),
        fetch("/api/v1/analytics/spans", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(latencyPercentilesReq),
        }),
        fetch("/api/v1/analytics/spans", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(errorRateReq),
        }),
      ]);

      // Parse all responses, then bail if a newer request superseded this
      // one. Every widget's state is always set — a response with no rows
      // (Go marshals empty slices as null) must clear the previous
      // project's data rather than leave it on screen.
      const [
        tracesByNameData,
        tracesByTimeData,
        tokenUsageJson,
        kindUsageJson,
        userConsumptionJson,
        kpiJson,
        latencyJson,
        errorRateJson,
      ] = await Promise.all([
        tracesByNameResp.ok ? tracesByNameResp.json() : { rows: [] },
        tracesByTimeResp.ok ? tracesByTimeResp.json() : { rows: [] },
        tokenUsageResp.ok ? tokenUsageResp.json() : { rows: [] },
        kindUsageResp.ok ? kindUsageResp.json() : { rows: [] },
        userConsumptionResp.ok ? userConsumptionResp.json() : { rows: [] },
        kpiResp.ok ? kpiResp.json() : { rows: [] },
        latencyResp.ok ? latencyResp.json() : { rows: [] },
        errorRateResp.ok ? errorRateResp.json() : { rows: [] },
      ]);

      if (seq !== requestSeq.current) return;

      const mapCostRows = (rows: Record<string, unknown>[]): CostData[] =>
        rows.map((row) => ({
          model: (row.model as string) || "unknown",
          inputTokens: Number(row.input_tokens) || 0,
          outputTokens: Number(row.output_tokens) || 0,
          totalCost: Number(row.total_cost) || 0,
        }));

      // Kind-grouped rows reuse the CostData shape, keyed off `model` so the
      // existing table components render without changes — the `model`
      // field here holds the span `kind` (llm/tool/agent/chain).
      const mapKindRows = (rows: Record<string, unknown>[]): CostData[] =>
        rows.map((row) => ({
          model: (row.kind as string) || "unknown",
          inputTokens: Number(row.input_tokens) || 0,
          outputTokens: Number(row.output_tokens) || 0,
          totalCost: Number(row.total_cost) || 0,
        }));

      setTracesByName(
        (tracesByNameData.rows ?? []).map((row: Record<string, unknown>) => ({
          name: (row.model as string) || "unknown",
          count: Number(row.count) || 0,
        })),
      );
      setTracesByTime(
        (tracesByTimeData.rows ?? [])
          .map((row: Record<string, unknown>) => ({
            time: extractRawTime(row),
            count: Number(row.count) || 0,
          }))
          .filter((d: TimeSeriesData) => d.time > 0),
      );
      setTokenUsageData(mapCostRows(tokenUsageJson.rows ?? []));
      setKindUsageData(mapKindRows(kindUsageJson.rows ?? []));
      setUserConsumption(
        (userConsumptionJson.rows ?? []).map((row: Record<string, unknown>) => ({
          user: (row.service_name as string) || "anonymous",
          count: Number(row.count) || 0,
        })),
      );

      // KPI data: read row 0 (default to zeros when no rows)
      const kpiRows = kpiJson.rows ?? [];
      if (kpiRows.length > 0) {
        const r = kpiRows[0] as Record<string, unknown>;
        setKpiData({
          totalCost: Number(r.total_cost) || 0,
          totalTraces: Number(r.total_traces) || 0,
          // error_rate from backend: either a pre-computed ratio or a count
          errorRate: Number(r.error_rate) || 0,
          avgLatencyMs: Number(r.avg_latency_ms) || 0,
        });
      } else {
        setKpiData({
          totalCost: 0,
          totalTraces: 0,
          errorRate: 0,
          avgLatencyMs: 0,
        });
      }

      // Latency percentile data
      setLatencyData(
        (latencyJson.rows ?? [])
          .map((row: Record<string, unknown>) => ({
            time: extractRawTime(row),
            p50_ms: Number(row.p50_ms) || 0,
            p95_ms: Number(row.p95_ms) || 0,
            p99_ms: Number(row.p99_ms) || 0,
          }))
          .filter((d: LatencyPercentileData) => d.time > 0),
      );

      // Error rate data
      setErrorRateData(
        (errorRateJson.rows ?? [])
          .map((row: Record<string, unknown>) => ({
            time: extractRawTime(row),
            total_count: Number(row.total_count) || 0,
            error_count: Number(row.error_count) || 0,
          }))
          .filter((d: ErrorRateData) => d.time > 0),
      );
    } catch (err) {
      if (seq !== requestSeq.current) return;
      const msg = err instanceof Error ? err.message : "Failed to load dashboard data";
      setError(msg);
      console.error("Dashboard fetch error:", err);
    } finally {
      if (seq === requestSeq.current) setLoading(false);
    }
  }, [activeProject, from, to]);

  useEffect(() => {
    fetchData();
  }, [activeProject, from, to, fetchData]);

  return (
    <div className="p-6" style={{ background: colors.backgrounds.voidBlack }}>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold text-omneval-text-pure">Dashboard</h1>
          <p className="text-sm text-omneval-text-muted mt-1">
            {timeRange && TIME_RANGE_LABELS[timeRange]
              ? TIME_RANGE_LABELS[timeRange]
              : timeRangeLabel(from)}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <label className="text-sm text-omneval-text-muted">From:</label>
            <input
              type="datetime-local"
              value={toLocalInputValue(from)}
              onChange={(e) => {
                const iso = fromLocalInputValue(e.target.value);
                if (iso) setFrom(iso);
              }}
              className="input-focus text-sm px-2 py-1 rounded-md border border-omneval-border bg-omneval-surface text-omneval-text-pure"
              style={{
                colorScheme: "dark",
              }}
            />
          </div>
          <div className="flex items-center gap-2">
            <label className="text-sm text-omneval-text-muted">To:</label>
            <input
              type="datetime-local"
              value={toLocalInputValue(to)}
              onChange={(e) => {
                const iso = fromLocalInputValue(e.target.value);
                if (iso) setTo(iso);
              }}
              className="input-focus text-sm px-2 py-1 rounded-md border border-omneval-border bg-omneval-surface text-omneval-text-pure"
              style={{
                colorScheme: "dark",
              }}
            />
          </div>
          <button
            onClick={fetchData}
            disabled={loading}
            className="text-sm px-3 py-1.5 rounded-md font-medium text-white transition-all duration-150 disabled:opacity-50 hover:brightness-110 active:brightness-90"
            style={{
              background: colors.accents.violet,
              boxShadow: "0 2px 8px rgba(124, 58, 237, 0.3)",
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
            ) : (
              "Refresh"
            )}
          </button>
        </div>
      </div>

      {/* Error Banner */}
      {error && (
        <ErrorBanner
          message={error}
          onDismiss={() => setError(null)}
          onRetry={fetchData}
          retryLabel="Retry"
        />
      )}

      {/* ── KPI Tiles ── */}
      <div className="mb-4">
        <KPITiles data={kpiData} loading={loading} />
      </div>

      {/* ── Latency & Error Rate Row ── */}
      <div className="grid grid-cols-1 xl:grid-cols-2 gap-4 mb-4">
        <Card title="Latency Percentiles" subtitle="ms">
          <LatencyPercentileChart data={latencyData} loading={loading} />
        </Card>
        <Card title="Error Rate" subtitle="% errors">
          <ErrorRateChart data={errorRateData} loading={loading} />
        </Card>
      </div>

      {/* ── Widget Grid ── */}
      <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-4">
        {/* 1. Traces by Name (Horizontal Bar) */}
        <Card title="Traces by Model" subtitle="count">
          <TracesByNameChart data={tracesByName} loading={loading} />
        </Card>

        {/* 2. Scores (Empty State) */}
        <Card title="Eval Scores" subtitle="score (0–1)">
          <ScoresWidget loading={loading} />
        </Card>

        {/* 3. Traces by Time (Line Graph) */}
        <Card title="Traces over Time" subtitle="count/hour">
          <TracesByTimeChart data={tracesByTime} loading={loading} from={from} to={to} timeRange={timeRange} />
        </Card>

        {/* 4. Model Usage (Tabbed) */}
        <Card title="Token Usage" subtitle="input + output tokens">
          <ModelUsageWidget loading={loading} data={tokenUsageData} typeData={kindUsageData} />
        </Card>

        {/* 5. User Consumption (Horizontal Bar) */}
        <Card title="User Consumption" subtitle="traces per service">
          <UserConsumptionChart data={userConsumption} loading={loading} />
        </Card>
      </div>
    </div>
  );
}
