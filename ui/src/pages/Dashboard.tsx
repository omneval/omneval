import { useState, useEffect, useCallback, useMemo } from "react";
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
  formatTime,
  timeRangeLabel,
} from "@/utils/formatters";

// ── Types ──────────────────────────────────────────────────────────

interface DashboardPageProps {
  activeProject: string;
}



interface AnalyticsRequest {
  from: string;
  to: string;
  filters: { field: string; op: string; value: unknown }[];
  group_by: { field: string; truncate?: string }[];
  order_by: { field: string; desc: boolean }[];
  aggregations: { function: string; field: string; alias: string }[];
}

// ── Helpers ────────────────────────────────────────────────────────

function getDefaultFromTo(): { from: string; to: string } {
  const now = new Date();
  const from = new Date(now.getTime() - 24 * 60 * 60 * 1000);
  return {
    from: from.toISOString(),
    to: now.toISOString(),
  };
}

// ── Card Wrapper ───────────────────────────────────────────────────

function Card({ title, children, subtitle }: { title: string; children: React.ReactNode; subtitle?: string }) {
  return (
    <div className="card">
      <div className="card-header">
        <div className="flex items-center justify-between">
          <span>{title}</span>
          {subtitle && (
            <span className="text-xs font-normal text-lantern-ash opacity-60">{subtitle}</span>
          )}
        </div>
      </div>
      <div className="p-4">{children}</div>
    </div>
  );
}

// ── Shared Chart Config ────────────────────────────────────────────

const chartTooltipStyle: React.CSSProperties = {
  backgroundColor: colors.backgrounds.charcoalDepth,
  border: `1px solid ${colors.backgrounds.caveWall}`,
  borderRadius: "0.375rem",
  color: colors.typography.pureLight,
  fontSize: "0.875rem",
};

const gridColor = colors.backgrounds.caveWall;

/** Series color index for multi-chart consistency. */
const SERIES_EMBER = 0;

// ── Chart: Traces by Name (Horizontal Bar) ─────────────────────────

interface TracesByNameData {
  name: string;
  count: number;
}

function TracesByNameChart({ data, loading }: { data: TracesByNameData[]; loading: boolean }) {
  if (loading && data.length === 0) {
    return <LoadingState rows={5} rowHeight="1.25rem" />;
  }

  if (data.length === 0) {
    return (
      <EmptyState
        variant="onboarding"
        title="No traces yet"
        description="Send your first trace to see model breakdown here"
        actionLabel="View Traces"
      />
    );
  }

  return (
    <ResponsiveContainer width="100%" height={280}>
      <BarChart data={data} layout="vertical" margin={{ top: 5, right: 20, left: 60, bottom: 5 }}>
        <XAxis
          type="number"
          tick={{ fill: colors.typography.ashGrey, fontSize: 12 }}
          axisLine={{ stroke: gridColor }}
        />
        <YAxis
          type="category"
          dataKey="name"
          tick={{ fill: colors.typography.pureLight, fontSize: 12 }}
          width={80}
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
  time: string;
  count: number;
}

/** Extract the timestamp from a SQL date_trunc column name or raw column. */
function extractTime(row: Record<string, unknown>): string {
  // The outer query projects the group-by expression as the column name.
  // For date_trunc('hour', start_time) the column name is the full expression.
  // Also check common aliases.
  for (const key of [
    "date_trunc('hour', start_time)",
    "date_trunc('hour',start_time)",
    "start_time",
  ]) {
    if (row[key] !== undefined) {
      return formatTime(row[key] as string);
    }
  }
  // Fallback: look for any key containing "time" in the value.
  for (const [_key, value] of Object.entries(row)) {
    if (typeof value === "string" && value.includes("-")) {
      return formatTime(value);
    }
  }
  return "";
}

function TracesByTimeChart({ data, loading }: { data: TimeSeriesData[]; loading: boolean }) {
  if (loading && data.length === 0) {
    return <LoadingState rows={1} rowHeight="8rem" />;
  }

  if (data.length === 0) {
    return (
      <div className="text-center py-8 text-lantern-ash text-sm">
        No time-series data available
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={280}>
      <LineChart data={data} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
        <CartesianGrid strokeDasharray="3 3" stroke={gridColor} />
        <XAxis
          dataKey="time"
          tick={{ fill: colors.typography.ashGrey, fontSize: 11 }}
          axisLine={{ stroke: gridColor }}
          tickLine={false}
          angle={-45}
          textAnchor="end"
          height={60}
          interval="preserveStartEnd"
        />
        <YAxis
          tick={{ fill: colors.typography.ashGrey, fontSize: 12 }}
          axisLine={{ stroke: gridColor }}
          tickLine={false}
        />
        <Tooltip
          contentStyle={chartTooltipStyle}
          formatter={(value: number) => [formatNumber(value), "Traces"]}
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

function ModelCostsTable({ data, loading }: { data: CostData[]; loading: boolean }) {
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
              borderBottom: `1px solid ${colors.backgrounds.caveWall}`,
              color: colors.typography.ashGrey,
            }}
          >
            <th className="text-left py-2 px-3 font-medium text-xs uppercase tracking-wider">
              Model
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
                borderBottom: `1px solid ${colors.backgrounds.caveWall}`,
                background: i % 2 === 0 ? "transparent" : `${colors.backgrounds.slightIllumination}33`,
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLElement).style.background =
                  `rgba(255, 204, 188, 0.1)`;
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLElement).style.background =
                  i % 2 === 0 ? "transparent" : `${colors.backgrounds.slightIllumination}33`;
              }}
            >
              <td className="py-2 px-3 font-medium text-lantern-pure">{row.model}</td>
              <td className="py-2 px-3 text-right text-lantern-ash">
                {formatNumber(row.inputTokens)}
              </td>
              <td className="py-2 px-3 text-right text-lantern-ash">
                {formatNumber(row.outputTokens)}
              </td>
              <td
                className="py-2 px-3 text-right font-medium"
                style={{ color: colors.accents.emberFlare }}
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
      title="No scores yet"
      description="Configure evaluation rules to start scoring traces"
      actionLabel="Go to Settings"
    />
  );
}

// ── Tabbed Widget: Model Usage ─────────────────────────────────────

const USAGE_TABS = ["Cost by Model", "Cost by Type", "Usage by Model", "Usage by Type"];

interface UsageByModelData {
  model: string;
  inputTokens: number;
  outputTokens: number;
}

function ModelUsageWidget({
  loading,
  data,
}: {
  loading: boolean;
  data: CostData[] | UsageByModelData[];
}) {
  const [activeTab, setActiveTab] = useState(0);

  if (loading) {
    return <LoadingState rows={3} rowHeight="2.5rem" />;
  }

  const isEmpty = Array.isArray(data) && data.length === 0;

  // "Usage by Model" and "Usage by Type" tabs show tokens only (no cost).
  const showTokensOnly = activeTab === 2 || activeTab === 3;

  return (
    <div>
      <div className="flex gap-1 mb-4 border-b" style={{ borderColor: colors.backgrounds.caveWall }}>
        {USAGE_TABS.map((tab, i) => (
          <button
            key={tab}
            onClick={() => setActiveTab(i)}
            className={`px-3 py-1.5 text-sm rounded-t-md transition-colors ${
              i === activeTab
                ? "text-lantern-ember"
                : "text-lantern-ash hover:text-lantern-pure"
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
                  borderBottom: `1px solid ${colors.backgrounds.caveWall}`,
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
              {data
                .sort((a, b) => b.inputTokens + b.outputTokens - (a.inputTokens + a.outputTokens))
                .map((row: CostData | UsageByModelData, i) => (
                  <tr
                    key={row.model}
                    className="transition-colors duration-150"
                    style={{
                      borderBottom: `1px solid ${colors.backgrounds.caveWall}`,
                      background: i % 2 === 0 ? "transparent" : `${colors.backgrounds.slightIllumination}33`,
                    }}
                    onMouseEnter={(e) => {
                      (e.currentTarget as HTMLElement).style.background =
                        `rgba(255, 204, 188, 0.1)`;
                    }}
                    onMouseLeave={(e) => {
                      (e.currentTarget as HTMLElement).style.background =
                        i % 2 === 0 ? "transparent" : `${colors.backgrounds.slightIllumination}33`;
                    }}
                  >
                    <td className="py-2 px-3 font-medium text-lantern-pure">{row.model}</td>
                    <td className="py-2 px-3 text-right text-lantern-ash">
                      {formatNumber((row as CostData).inputTokens)}
                    </td>
                    <td className="py-2 px-3 text-right text-lantern-ash">
                      {formatNumber((row as CostData).outputTokens)}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      ) : (
        <ModelCostsTable data={data as CostData[]} loading={false} />
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
      <div className="text-center py-8 text-lantern-ash text-sm">
        No user consumption data available
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={280}>
      <BarChart data={data} layout="vertical" margin={{ top: 5, right: 20, left: 60, bottom: 5 }}>
        <XAxis
          type="number"
          tick={{ fill: colors.typography.ashGrey, fontSize: 12 }}
          axisLine={{ stroke: gridColor }}
        />
        <YAxis
          type="category"
          dataKey="user"
          tick={{ fill: colors.typography.pureLight, fontSize: 12 }}
          width={80}
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

// ── Main Dashboard ─────────────────────────────────────────────────

export default function DashboardPage({ activeProject }: DashboardPageProps) {
  const defaultFromTo = getDefaultFromTo();
  const [from, setFrom] = useState(defaultFromTo.from);
  const [to, setTo] = useState(defaultFromTo.to);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Dashboard state
  const [tracesByName, setTracesByName] = useState<TracesByNameData[]>([]);
  const [tracesByTime, setTracesByTime] = useState<TimeSeriesData[]>([]);
  const [modelCosts, setModelCosts] = useState<CostData[]>([]);
  const [userConsumption, setUserConsumption] = useState<UserConsumptionData[]>([]);

  // Token Usage tab data
  const [tokenUsageData, setTokenUsageData] = useState<CostData[]>([]);

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const body = {
        from,
        to,
        filters: [],
        group_by: [],
        order_by: [],
      };

      // ── Traces by Name ──
      const tracesByNameReq: AnalyticsRequest = {
        ...body,
        group_by: [{ field: "name" }],
        order_by: [{ field: "count", desc: true }],
        aggregations: [
          { function: "count", field: "*", alias: "count" },
        ],
      };

      // ── Traces by Time ──
      const tracesByTimeReq: AnalyticsRequest = {
        ...body,
        group_by: [{ field: "start_time", truncate: "hour" }],
        order_by: [{ field: "start_time", desc: false }],
        aggregations: [
          { function: "count", field: "*", alias: "count" },
        ],
      };

      // ── Token Usage: Cost by Type (grouped by kind) ──
      const tokenUsageReq: AnalyticsRequest = {
        ...body,
        group_by: [{ field: "kind" }],
        order_by: [{ field: "total_cost", desc: true }],
        aggregations: [
          { function: "sum", field: "input_tokens", alias: "input_tokens" },
          { function: "sum", field: "output_tokens", alias: "output_tokens" },
          { function: "sum", field: "cost_usd", alias: "total_cost" },
        ],
      };

      // ── Model Costs ──
      const modelCostsReq: AnalyticsRequest = {
        ...body,
        group_by: [{ field: "model" }],
        order_by: [{ field: "cost_usd", desc: true }],
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

      const [
        tracesByNameResp,
        tracesByTimeResp,
        tokenUsageResp,
        modelCostsResp,
        userConsumptionResp,
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
          body: JSON.stringify(modelCostsReq),
        }),
        fetch("/api/v1/analytics/spans", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(userConsumptionReq),
        }),
      ]);

      // Parse traces by name
      if (tracesByNameResp.ok) {
        const data = await tracesByNameResp.json();
        if (data.rows) {
          setTracesByName(
            data.rows.map((row: Record<string, unknown>) => ({
              name: (row.name as string) || "unknown",
              count: Number(row.count) || 0,
            })),
          );
        }
      }

      // Parse traces by time (column name may be the date_trunc expression)
      if (tracesByTimeResp.ok) {
        const data = await tracesByTimeResp.json();
        if (data.rows) {
          setTracesByTime(
            data.rows.map((row: Record<string, unknown>) => ({
              time: extractTime(row),
              count: Number(row.count) || 0,
            })),
          );
        }
      }

      // Parse token usage data (for the tabbed widget)
      if (tokenUsageResp.ok) {
        const data = await tokenUsageResp.json();
        if (data.rows) {
          setTokenUsageData(
            data.rows.map((row: Record<string, unknown>) => ({
              model: (row.kind as string) || "unknown",
              inputTokens: Number(row.input_tokens) || 0,
              outputTokens: Number(row.output_tokens) || 0,
              totalCost: Number(row.total_cost) || 0,
            })),
          );
        }
      }

      // Parse model costs
      if (modelCostsResp.ok) {
        const data = await modelCostsResp.json();
        if (data.rows) {
          setModelCosts(
            data.rows.map((row: Record<string, unknown>) => ({
              model: (row.model as string) || "unknown",
              inputTokens: Number(row.input_tokens) || 0,
              outputTokens: Number(row.output_tokens) || 0,
              totalCost: Number(row.total_cost) || 0,
            })),
          );
        }
      }

      // Parse user consumption (uses service_name, not user_id)
      if (userConsumptionResp.ok) {
        const data = await userConsumptionResp.json();
        if (data.rows) {
          setUserConsumption(
            data.rows.map((row: Record<string, unknown>) => ({
              user: (row.service_name as string) || "anonymous",
              count: Number(row.count) || 0,
            })),
          );
        }
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Failed to load dashboard data";
      setError(msg);
      console.error("Dashboard fetch error:", err);
    } finally {
      setLoading(false);
    }
  }, [from, to]);

  useEffect(() => {
    fetchData();
  }, [activeProject, from, to, fetchData]);

  return (
    <div className="p-6" style={{ background: colors.backgrounds.abyssBlack }}>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold text-lantern-pure">Dashboard</h1>
          <p className="text-sm text-lantern-ash mt-1">{timeRangeLabel(from)}</p>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <label className="text-sm text-lantern-ash">From:</label>
            <input
              type="datetime-local"
              value={new Date(from).toISOString().slice(0, 16)}
              onChange={(e) => setFrom(e.target.value)}
              className="input-focus text-sm px-2 py-1 rounded-md border border-lantern-bg-cave bg-lantern-bg-illumination text-lantern-pure"
              style={{
                colorScheme: "dark",
              }}
            />
          </div>
          <div className="flex items-center gap-2">
            <label className="text-sm text-lantern-ash">To:</label>
            <input
              type="datetime-local"
              value={new Date(to).toISOString().slice(0, 16)}
              onChange={(e) => setTo(e.target.value)}
              className="input-focus text-sm px-2 py-1 rounded-md border border-lantern-bg-cave bg-lantern-bg-illumination text-lantern-pure"
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
              background: colors.accents.emberFlare,
              boxShadow: "0 2px 8px rgba(255, 87, 34, 0.25)",
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

      {/* ── 6-Widget Grid ── */}
      <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-4">
        {/* 1. Traces by Name (Horizontal Bar) */}
        <Card title="Traces by Model" subtitle="count">
          <TracesByNameChart data={tracesByName} loading={loading} />
        </Card>

        {/* 2. Model Costs (Data Table) */}
        <Card title="Cost by Model" subtitle="USD">
          <ModelCostsTable data={modelCosts} loading={loading} />
        </Card>

        {/* 3. Scores (Empty State) */}
        <Card title="Eval Scores" subtitle="score (0–1)">
          <ScoresWidget loading={loading} />
        </Card>

        {/* 4. Traces by Time (Line Graph) */}
        <Card title="Traces over Time" subtitle="count/hour">
          <TracesByTimeChart data={tracesByTime} loading={loading} />
        </Card>

        {/* 5. Model Usage (Tabbed) */}
        <Card title="Token Usage" subtitle="input + output tokens">
          <ModelUsageWidget loading={loading} data={tokenUsageData} />
        </Card>

        {/* 6. User Consumption (Horizontal Bar) */}
        <Card title="User Consumption" subtitle="traces per service">
          <UserConsumptionChart data={userConsumption} loading={loading} />
        </Card>
      </div>
    </div>
  );
}
