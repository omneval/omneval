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

// ── Types ──────────────────────────────────────────────────────────

interface DashboardPageProps {
  activeProject: string;
  projects: { project_id: string; name: string; org_id: string }[];
}

interface RowData {
  [key: string]: any;
}

interface AnalyticsRequest {
  from: string;
  to: string;
  filters: { field: string; op: string; value: unknown }[];
  group_by: { field: string; truncate?: string }[];
  order_by: { field: string; desc: boolean }[];
  aggregations: { function: string; field: string; alias: string }[];
  limit?: number;
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

function formatNumber(v: unknown): string {
  if (v == null) return "0";
  const num = typeof v === "string" ? parseFloat(v) : Number(v);
  if (isNaN(num)) return "0";
  if (num >= 1000000) return `${(num / 1000000).toFixed(1)}M`;
  if (num >= 1000) return `${(num / 1000).toFixed(1)}K`;
  return num.toFixed(2);
}

function formatTime(iso: string): string {
  if (!iso) return "N/A";
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// ── Empty State ────────────────────────────────────────────────────

function EmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-12 px-4">
      <svg
        width="48"
        height="48"
        viewBox="0 0 48 48"
        fill="none"
        className="mb-3 text-lantern-bg-cave"
      >
        <rect x="8" y="8" width="32" height="32" rx="4" stroke="currentColor" strokeWidth="2" />
        <path d="M18 24h12M24 18v12" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      </svg>
      <p className="text-sm font-medium text-lantern-text-ash">{title}</p>
      <p className="text-xs text-lantern-text-ash mt-1 opacity-70">{description}</p>
    </div>
  );
}

// ── Card Wrapper ───────────────────────────────────────────────────

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div
      className="card"
      style={{
        background: colors.backgrounds.charcoalDepth,
        border: `1px solid ${colors.backgrounds.caveWall}`,
        borderRadius: "0.5rem",
      }}
    >
      <div
        className="card-header"
        style={{ borderBottom: `1px solid ${colors.backgrounds.caveWall}` }}
      >
        {title}
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

// ── Chart: Traces by Name (Horizontal Bar) ─────────────────────────

interface TracesByNameData {
  name: string;
  count: number;
}

function TracesByNameChart({ data, loading }: { data: TracesByNameData[]; loading: boolean }) {
  if (loading && data.length === 0) {
    return <div className="text-center py-8 text-lantern-ash text-sm">Loading...</div>;
  }

  if (data.length === 0) {
    return (
      <div className="text-center py-8 text-lantern-ash text-sm">
        No trace data available
      </div>
    );
  }

  // maxCount computed for scaling

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
              fill={colors.accents.emberFlare}
              fillOpacity={0.5 + (0.5 * index) / Math.max(data.length, 1)}
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

function TracesByTimeChart({ data, loading }: { data: TimeSeriesData[]; loading: boolean }) {
  if (loading && data.length === 0) {
    return <div className="text-center py-8 text-lantern-ash text-sm">Loading...</div>;
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
          stroke={colors.accents.emberFlare}
          strokeWidth={2.5}
          dot={{ fill: colors.accents.emberFlare, r: 3 }}
          activeDot={{ r: 5, fill: colors.accents.emberFlare }}
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
    return <div className="text-center py-8 text-lantern-ash text-sm">Loading...</div>;
  }

  if (data.length === 0) {
    return <EmptyState title="No Cost Data" description="No model usage data available for this time range" />;
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
    return <div className="text-center py-8 text-lantern-ash text-sm">Loading...</div>;
  }

  return (
    <EmptyState title="No Scores Yet" description="Evaluation scores will appear here once eval rules fire" />
  );
}

// ── Tabbed Widget: Model Usage ─────────────────────────────────────

const USAGE_TABS = ["Cost by Model", "Cost by Type", "Usage by Model", "Usage by Type"];

function ModelUsageWidget({ loading }: { loading: boolean }) {
  const [activeTab, setActiveTab] = useState(0);

  if (loading) {
    return <div className="text-center py-8 text-lantern-ash text-sm">Loading...</div>;
  }

  return (
    <div>
      {/* Tabs */}
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
                    borderBottom: `2px solid ${colors.accents.emberFlare}`,
                    paddingBottom: "0.375rem",
                  }
                : { borderBottom: "2px solid transparent" }
            }
          >
            {tab}
          </button>
        ))}
      </div>
      <EmptyState title="No Model Usage Data" description="Model usage metrics will appear here" />
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
    return <div className="text-center py-8 text-lantern-ash text-sm">Loading...</div>;
  }

  if (data.length === 0) {
    return (
      <div className="text-center py-8 text-lantern-ash text-sm">
        No user consumption data available
      </div>
    );
  }

  // maxCount computed for scaling

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
              fill={colors.accents.softGlow}
              fillOpacity={0.7}
            />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  );
}

// ── Main Dashboard ─────────────────────────────────────────────────

export default function DashboardPage({ activeProject }: DashboardPageProps) {
  const [from, setFrom] = useState(getDefaultFromTo().from);
  const [to, setTo] = useState(getDefaultFromTo().to);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Dashboard state
  const [tracesByName, setTracesByName] = useState<TracesByNameData[]>([]);
  const [tracesByTime, setTracesByTime] = useState<TimeSeriesData[]>([]);
  const [modelCosts, setModelCosts] = useState<CostData[]>([]);
  const [userConsumption, setUserConsumption] = useState<UserConsumptionData[]>([]);

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

      // ── User Consumption ──
      const userConsumptionReq: AnalyticsRequest = {
        ...body,
        group_by: [{ field: "user_id" }],
        order_by: [{ field: "count", desc: true }],
        aggregations: [
          { function: "count", field: "*", alias: "count" },
        ],
      };

      const [
        tracesByNameResp,
        tracesByTimeResp,
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
            data.rows.map((row: RowData) => ({
              name: row.name || "unknown",
              count: Number(row.count) || 0,
            })),
          );
        }
      }

      // Parse traces by time
      if (tracesByTimeResp.ok) {
        const data = await tracesByTimeResp.json();
        if (data.rows) {
          setTracesByTime(
            data.rows.map((row: RowData) => ({
              time: formatTime(row.start_time),
              count: Number(row.count) || 0,
            })),
          );
        }
      }

      // Parse model costs
      if (modelCostsResp.ok) {
        const data = await modelCostsResp.json();
        if (data.rows) {
          setModelCosts(
            data.rows.map((row: RowData) => ({
              model: row.model || "unknown",
              inputTokens: Number(row.input_tokens) || 0,
              outputTokens: Number(row.output_tokens) || 0,
              totalCost: Number(row.total_cost) || 0,
            })),
          );
        }
      }

      // Parse user consumption
      if (userConsumptionResp.ok) {
        const data = await userConsumptionResp.json();
        if (data.rows) {
          setUserConsumption(
            data.rows.map((row: RowData) => ({
              user: row.user_id || "anonymous",
              count: Number(row.count) || 0,
            })),
          );
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load dashboard data");
      console.error("Dashboard fetch error:", err);
    } finally {
      setLoading(false);
    }
  }, [from, to]);

  useEffect(() => {
    fetchData();
  }, [activeProject, from, to, fetchData]);

  const timeRangeLabel = (() => {
    const now = new Date();
    const fromD = new Date(from);
    const diffHours = (now.getTime() - fromD.getTime()) / (1000 * 60 * 60);
    if (diffHours <= 1) return "Past hour";
    if (diffHours <= 24) return "Past 24 hours";
    if (diffHours <= 168) return "Past 7 days";
    return "Custom range";
  })();

  return (
    <div className="p-6" style={{ background: colors.backgrounds.abyssBlack }}>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold text-lantern-pure">Dashboard</h1>
          <p className="text-sm text-lantern-ash mt-1">{timeRangeLabel}</p>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <label className="text-sm text-lantern-ash">From:</label>
            <input
              type="datetime-local"
              value={new Date(from).toISOString().slice(0, 16)}
              onChange={(e) => setFrom(e.target.value)}
              className="text-sm border rounded-md px-2 py-1 bg-lantern-bg-illumination border-lantern-bg-cave text-lantern-pure"
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
              className="text-sm border rounded-md px-2 py-1 bg-lantern-bg-illumination border-lantern-bg-cave text-lantern-pure"
              style={{
                colorScheme: "dark",
              }}
            />
          </div>
          <button
            onClick={fetchData}
            disabled={loading}
            className="text-sm px-3 py-1.5 rounded-md text-lantern-pure transition-colors"
            style={{
              background: colors.accents.emberFlare,
              opacity: loading ? 0.6 : 1,
            }}
            onMouseEnter={(e) => {
              if (!loading) (e.target as HTMLElement).style.background = colors.accents.softGlow;
            }}
            onMouseLeave={(e) => {
              (e.target as HTMLElement).style.background = colors.accents.emberFlare;
            }}
          >
            {loading ? "Loading..." : "Refresh"}
          </button>
        </div>
      </div>

      {/* Error Banner */}
      {error && (
        <div
          className="mb-4 p-3 rounded-md text-sm"
          style={{
            background: "rgba(230, 74, 25, 0.15)",
            border: `1px solid ${colors.accents.deepHeat}`,
            color: colors.accents.deepHeat,
          }}
        >
          {error}
        </div>
      )}

      {/* ── 6-Widget Grid ── */}
      <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-4">
        {/* 1. Traces by Name (Horizontal Bar) */}
        <Card title="Traces by Name">
          <TracesByNameChart data={tracesByName} loading={loading} />
        </Card>

        {/* 2. Model Costs (Data Table) */}
        <Card title="Model Costs">
          <ModelCostsTable data={modelCosts} loading={loading} />
        </Card>

        {/* 3. Scores (Empty State) */}
        <Card title="Scores">
          <ScoresWidget loading={loading} />
        </Card>

        {/* 4. Traces by Time (Line Graph) */}
        <Card title="Traces by Time">
          <TracesByTimeChart data={tracesByTime} loading={loading} />
        </Card>

        {/* 5. Model Usage (Tabbed Empty State) */}
        <Card title="Model Usage">
          <ModelUsageWidget loading={loading} />
        </Card>

        {/* 6. User Consumption (Horizontal Bar) */}
        <Card title="User Consumption">
          <UserConsumptionChart data={userConsumption} loading={loading} />
        </Card>
      </div>
    </div>
  );
}
