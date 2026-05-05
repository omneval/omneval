import { useState, useEffect, useCallback } from "react";

interface TracesPageProps {
  activeProject: string;
  projects: { project_id: string; name: string; org_id: string }[];
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
}

export default function TracesPage({ activeProject }: TracesPageProps) {
  const [spans, setSpans] = useState<Span[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);

  const fetchSpans = useCallback(async (cursor: string) => {
    setLoading(true);
    try {
      const body: Record<string, unknown> = {
        from: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString(),
        to: new Date().toISOString(),
        limit: 25,
      };
      if (cursor) body.cursor = cursor;

      const res = await fetch("/api/v1/spans/query", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (res.ok) {
        const data = await res.json();
        setSpans(data.spans ?? []);
        setNextCursor(data.next ?? "");
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    setPage(1);
    fetchSpans("");
  }, [activeProject, fetchSpans]);

  const loadMore = () => {
    if (nextCursor) {
      fetchSpans(nextCursor);
      setPage((p) => p + 1);
    }
  };

  const formatTime = (iso: string) => {
    return new Date(iso).toLocaleString();
  };

  const formatDuration = (start: string, end: string) => {
    const ms = new Date(end).getTime() - new Date(start).getTime();
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-xl font-semibold text-gray-900">Traces</h1>
      </div>

      {spans.length === 0 && !loading && (
        <div className="text-center py-12 text-gray-500">
          No traces found for this project.
        </div>
      )}

      <div className="bg-white rounded-lg border border-gray-200 overflow-hidden">
        <table className="w-full">
          <thead className="bg-gray-50 border-b border-gray-200">
            <tr>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">
                Trace
              </th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">
                Name
              </th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">
                Model
              </th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">
                Duration
              </th>
              <th className="px-4 py-2 text-right text-xs font-medium text-gray-500 uppercase">
                Cost
              </th>
              <th className="px-4 py-2 text-right text-xs font-medium text-gray-500 uppercase">
                Tokens
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {spans.map((span) => (
              <tr key={span.span_id} className="hover:bg-gray-50">
                <td className="px-4 py-3 text-sm">
                  <div className="font-mono text-gray-900">{span.trace_id}</div>
                  <div className="text-xs text-gray-400">
                    {formatTime(span.start_time)}
                  </div>
                </td>
                <td className="px-4 py-3 text-sm text-gray-700">{span.name}</td>
                <td className="px-4 py-3 text-sm text-gray-500">
                  {span.model || "—"}
                </td>
                <td className="px-4 py-3 text-sm text-gray-500">
                  {formatDuration(span.start_time, span.end_time)}
                </td>
                <td className="px-4 py-3 text-sm text-right text-gray-700">
                  ${span.cost_usd?.toFixed(4) ?? "0.0000"}
                </td>
                <td className="px-4 py-3 text-sm text-right text-gray-500">
                  {span.input_tokens + span.output_tokens}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {nextCursor && (
        <div className="mt-4 text-center">
          <button
            onClick={loadMore}
            disabled={loading}
            className="text-sm text-blue-600 hover:text-blue-800 font-medium"
          >
            {loading ? "Loading..." : `Load more (${page + 1})`}
          </button>
        </div>
      )}
    </div>
  );
}
