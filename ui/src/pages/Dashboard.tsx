import { useState, useEffect, useCallback } from "react";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  BarChart,
  Bar,
} from "recharts";

interface DashboardPageProps {
  activeProject: string;
  projects: { project_id: string; name: string; org_id: string }[];
}

interface RowData {
  [key: string]: any;
}

// Default time range: last 24 hours.
function getDefaultFromTo(): { from: string; to: string } {
  const now = new Date();
  const from = new Date(now.getTime() - 24 * 60 * 60 * 1000);
  return {
    from: from.toISOString(),
    to: now.toISOString(),
  };
}

export default function DashboardPage({
  activeProject,
}: DashboardPageProps) {
  const [from, setFrom] = useState(getDefaultFromTo().from);
  const [to, setTo] = useState(getDefaultFromTo().to);
  const [loading, setLoading] = useState(false);
  const [latencyData, setLatencyData] = useState<RowData[]>([]);
  const [costData, setCostData] = useState<RowData[]>([]);
  const [tokenData, setTokenData] = useState<RowData[]>([]);
  const [errorRateData, setErrorRateData] = useState<RowData[]>([]);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const body = {
        from,
        to,
        filters: [],
        group_by: [
          {
            field: "start_time",
            truncate: "hour",
          },
        ],
        order_by: [{ field: "start_time", desc: false }],
      };

      // Latency: p50, p95, p99 of duration_ms.
      const latencyReq = {
        ...body,
        aggregations: [
          {
            function: "p50",
            field: "duration_ms",
            alias: "p50",
          },
          {
            function: "p95",
            field: "duration_ms",
            alias: "p95",
          },
          {
            function: "p99",
            field: "duration_ms",
            alias: "p99",
          },
          {
            function: "count",
            field: "*",
            alias: "count",
          },
        ],
      };

      const latencyRes = await fetch("/api/v1/analytics/spans", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(latencyReq),
      });

      // Cost: sum of cost_usd.
      const costReq = {
        ...body,
        aggregations: [
          {
            function: "sum",
            field: "cost_usd",
            alias: "total_cost",
          },
          {
            function: "count",
            field: "*",
            alias: "count",
          },
        ],
      };

      const costRes = await fetch("/api/v1/analytics/spans", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(costReq),
      });

      // Tokens: sum of input_tokens and output_tokens.
      const tokenReq = {
        ...body,
        aggregations: [
          {
            function: "sum",
            field: "input_tokens",
            alias: "total_input",
          },
          {
            function: "sum",
            field: "output_tokens",
            alias: "total_output",
          },
          {
            function: "count",
            field: "*",
            alias: "count",
          },
        ],
      };

      const tokenRes = await fetch("/api/v1/analytics/spans", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(tokenReq),
      });

      // Error rate: % of spans with status_code != "OK".
      const errorRateRes = await fetch("/api/v1/analytics/spans", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ...body,
          filters: [
            { field: "status_code", op: "neq", value: "OK" },
          ],
          aggregations: [
            {
              function: "count",
              field: "*",
              alias: "errors",
            },
          ],
        }),
      });

      // Total count for error rate calculation.
      const totalRes = await fetch("/api/v1/analytics/spans", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ...body,
          aggregations: [
            {
              function: "count",
              field: "*",
              alias: "total",
            },
          ],
        }),
      });

      const [
        latencyResp,
        costResp,
        tokenResp,
        errorRateJSON,
        totalJSON,
      ] = await Promise.all([
        latencyRes.ok ? latencyRes.json() : Promise.resolve({ rows: [] }),
        costRes.ok ? costRes.json() : Promise.resolve({ rows: [] }),
        tokenRes.ok ? tokenRes.json() : Promise.resolve({ rows: [] }),
        errorRateRes.ok ? errorRateRes.json() : Promise.resolve({ rows: [] }),
        totalRes.ok ? totalRes.json() : Promise.resolve({ rows: [] }),
      ]);

      // Process latency data.
      if (latencyResp.rows && Array.isArray(latencyResp.rows)) {
        const processed = latencyResp.rows.map((row: RowData) => {
          const time = row.start_time
            ? new Date(row.start_time).toLocaleString()
            : "N/A";
          return {
            time,
            p50: formatNumber(row.p50),
            p95: formatNumber(row.p95),
            p99: formatNumber(row.p99),
            count: row.count ? parseInt(String(row.count)) : 0,
          };
        });
        setLatencyData(processed);
      }

      // Process cost data.
      if (costResp.rows && Array.isArray(costResp.rows)) {
        const processed = costResp.rows.map((row: RowData) => {
          const time = row.start_time
            ? new Date(row.start_time).toLocaleString()
            : "N/A";
          return {
            time,
            total_cost: row.total_cost ? parseFloat(String(row.total_cost)) : 0,
            count: row.count ? parseInt(String(row.count)) : 0,
          };
        });
        setCostData(processed);
      }

      // Process token data.
      if (tokenResp.rows && Array.isArray(tokenResp.rows)) {
        const processed = tokenResp.rows.map((row: RowData) => {
          const time = row.start_time
            ? new Date(row.start_time).toLocaleString()
            : "N/A";
          return {
            time,
            total_input: row.total_input
              ? parseInt(String(row.total_input))
              : 0,
            total_output: row.total_output
              ? parseInt(String(row.total_output))
              : 0,
            count: row.count ? parseInt(String(row.count)) : 0,
          };
        });
        setTokenData(processed);
      }

      // Process error rate data (combine with total count).
      if (
        errorRateJSON.rows &&
        Array.isArray(errorRateJSON.rows) &&
        totalJSON.rows &&
        Array.isArray(totalJSON.rows)
      ) {
        const errorRows = errorRateJSON.rows;
        const totalRows = totalJSON.rows;
        const processed = totalRows.map((row: RowData, i: number) => {
          const time = row.start_time
            ? new Date(row.start_time).toLocaleString()
            : "N/A";
          const total = row.total ? parseInt(String(row.total)) : 1;
          const errors =
            i < errorRows.length && errorRows[i].errors
              ? parseInt(String(errorRows[i].errors))
              : 0;
          return {
            time,
            error_rate: total > 0 ? (errors / total) * 100 : 0,
            errors,
            total,
          };
        });
        setErrorRateData(processed);
      }
    } finally {
      setLoading(false);
    }
  }, [from, to]);

  // Refetch when project or time range changes.
  useEffect(() => {
    fetchData();
  }, [activeProject, from, to, fetchData]);

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-xl font-semibold text-gray-900">Dashboard</h1>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <label className="text-sm text-gray-600">From:</label>
            <input
              type="datetime-local"
              value={new Date(from).toISOString().slice(0, 16)}
              onChange={(e) => setFrom(e.target.value)}
              className="text-sm border border-gray-300 rounded-md px-2 py-1"
            />
          </div>
          <div className="flex items-center gap-2">
            <label className="text-sm text-gray-600">To:</label>
            <input
              type="datetime-local"
              value={new Date(to).toISOString().slice(0, 16)}
              onChange={(e) => setTo(e.target.value)}
              className="text-sm border border-gray-300 rounded-md px-2 py-1"
            />
          </div>
          <button
            onClick={fetchData}
            disabled={loading}
            className="text-sm bg-blue-600 text-white px-3 py-1 rounded-md hover:bg-blue-700 disabled:opacity-50"
          >
            {loading ? "Loading..." : "Refresh"}
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Latency Chart */}
        <div className="bg-white rounded-lg border border-gray-200 p-4">
          <h2 className="text-lg font-medium text-gray-900 mb-3">
            Latency (ms) — p50/p95/p99
          </h2>
          {loading && latencyData.length === 0 ? (
            <div className="text-center py-8 text-gray-500">Loading...</div>
          ) : latencyData.length === 0 ? (
            <div className="text-center py-8 text-gray-500">No data</div>
          ) : (
            <ResponsiveContainer width="100%" height={300}>
              <LineChart data={latencyData}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis
                  dataKey="time"
                  angle={-45}
                  textAnchor="end"
                  height={80}
                  interval="preserveStartEnd"
                />
                <YAxis />
                <Tooltip />
                <Legend />
                <Line
                  type="monotone"
                  dataKey="p50"
                  stroke="#3b82f6"
                  name="p50"
                  strokeWidth={2}
                />
                <Line
                  type="monotone"
                  dataKey="p95"
                  stroke="#f59e0b"
                  name="p95"
                  strokeWidth={2}
                />
                <Line
                  type="monotone"
                  dataKey="p99"
                  stroke="#ef4444"
                  name="p99"
                  strokeWidth={2}
                />
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>

        {/* Cost Chart */}
        <div className="bg-white rounded-lg border border-gray-200 p-4">
          <h2 className="text-lg font-medium text-gray-900 mb-3">
            Cost (USD) — Total
          </h2>
          {loading && costData.length === 0 ? (
            <div className="text-center py-8 text-gray-500">Loading...</div>
          ) : costData.length === 0 ? (
            <div className="text-center py-8 text-gray-500">No data</div>
          ) : (
            <ResponsiveContainer width="100%" height={300}>
              <BarChart data={costData}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis
                  dataKey="time"
                  angle={-45}
                  textAnchor="end"
                  height={80}
                  interval="preserveStartEnd"
                />
                <YAxis />
                <Tooltip
                  formatter={(value: number) => `$${value.toFixed(4)}`}
                />
                <Legend />
                <Bar dataKey="total_cost" fill="#8b5cf6" name="Cost ($)" />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>

        {/* Token Chart */}
        <div className="bg-white rounded-lg border border-gray-200 p-4">
          <h2 className="text-lg font-medium text-gray-900 mb-3">
            Tokens — Input/Output
          </h2>
          {loading && tokenData.length === 0 ? (
            <div className="text-center py-8 text-gray-500">Loading...</div>
          ) : tokenData.length === 0 ? (
            <div className="text-center py-8 text-gray-500">No data</div>
          ) : (
            <ResponsiveContainer width="100%" height={300}>
              <BarChart data={tokenData}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis
                  dataKey="time"
                  angle={-45}
                  textAnchor="end"
                  height={80}
                  interval="preserveStartEnd"
                />
                <YAxis />
                <Tooltip />
                <Legend />
                <Bar
                  dataKey="total_input"
                  fill="#3b82f6"
                  name="Input Tokens"
                />
                <Bar
                  dataKey="total_output"
                  fill="#10b981"
                  name="Output Tokens"
                />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>

        {/* Error Rate Chart */}
        <div className="bg-white rounded-lg border border-gray-200 p-4">
          <h2 className="text-lg font-medium text-gray-900 mb-3">
            Error Rate (%)
          </h2>
          {loading && errorRateData.length === 0 ? (
            <div className="text-center py-8 text-gray-500">Loading...</div>
          ) : errorRateData.length === 0 ? (
            <div className="text-center py-8 text-gray-500">No data</div>
          ) : (
            <ResponsiveContainer width="100%" height={300}>
              <LineChart data={errorRateData}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis
                  dataKey="time"
                  angle={-45}
                  textAnchor="end"
                  height={80}
                  interval="preserveStartEnd"
                />
                <YAxis />
                <Tooltip formatter={(value: number) => `${value.toFixed(2)}%`} />
                <Legend />
                <Line
                  type="monotone"
                  dataKey="error_rate"
                  stroke="#ef4444"
                  name="Error Rate"
                  strokeWidth={2}
                />
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>
    </div>
  );
}

// formatNumber formats a number for display, handling large values.
function formatNumber(v: any): string {
  if (v == null) return "0";
  const num = typeof v === "string" ? parseFloat(v) : Number(v);
  if (isNaN(num)) return "0";
  if (num >= 1000000) return `${(num / 1000000).toFixed(1)}M`;
  if (num >= 1000) return `${(num / 1000).toFixed(1)}K`;
  return num.toFixed(2);
}
