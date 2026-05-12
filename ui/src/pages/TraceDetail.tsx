import { useState, useEffect, useCallback } from "react";
import { colors } from "@/theme";
import { ErrorBanner } from "@/components/ErrorBanner";
import { formatTime, formatDuration } from "@/utils/formatters";
import { useToast } from "@/components/Toast";
import SaveToDatasetModal from "@/components/SaveToDatasetModal";

// ── Types ──────────────────────────────────────────────────────────

interface TraceDetailPageProps {
  traceId: string;
  activeProject: string;
  onBack: () => void;
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
  children?: Span[];
  input?: string;
  output?: string;
  status_code?: string;
  scores?: { eval_name: string; value: number }[];
}

interface TraceResponse {
  trace_id: string;
  project_id: string;
  root_span: Span;
  spans?: Span[];
}

// ── Trace Detail Page ──────────────────────────────────────────────

export default function TraceDetailPage({
  traceId,
  activeProject,
  onBack,
}: TraceDetailPageProps) {
  const { addToast } = useToast();
  const [trace, setTrace] = useState<Span | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saveSpan, setSaveSpan] = useState<{
    span: Span;
    input: string;
    output?: string;
  } | null>(null);

  // Fetch trace detail
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetch(`/api/v1/traces/${traceId}`)
      .then((res) => {
        if (!res.ok) {
          if (res.status === 404) {
            throw new Error("Trace not found");
          }
          throw new Error(`Failed to load trace (${res.status})`);
        }
        return res.json();
      })
      .then((data: TraceResponse) => {
        if (!cancelled) {
          setTrace(data.root_span);
          setLoading(false);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err.message);
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [traceId, activeProject]);

  const handleSaveSuccess = useCallback(() => {
    setSaveSpan(null);
    addToast("success", "Span saved to dataset");
  }, [addToast]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="flex flex-col items-center gap-3">
          <div
            className="w-6 h-6 rounded-full border-2 animate-spin"
            style={{
              borderColor: colors.backgrounds.caveWall,
              borderTopColor: colors.accents.emberFlare,
            }}
          />
          <p className="text-sm text-lantern-ash">Loading trace…</p>
        </div>
      </div>
    );
  }

  if (error || !trace) {
    return (
      <div className="flex flex-col items-center justify-center h-full gap-4 py-8">
        <ErrorBanner
          message={error ?? "Trace not found"}
          onDismiss={onBack}
        />
      </div>
    );
  }

  const totalTokens = (span: Span) => span.input_tokens + span.output_tokens;

  // Determine span color based on kind
  const kindColor: Record<string, string> = {
    llm: colors.accents.emberFlare,
    tool: colors.accents.softGlow,
    agent: colors.accents.flicker,
    chain: "#60a5fa",
    internal: colors.typography.ashGrey,
  };

  return (
    <div className="flex flex-col h-full" style={{ background: colors.backgrounds.abyssBlack }}>
      {/* Header */}
      <div className="flex items-center gap-3 px-4 py-3 border-b"
        style={{ borderColor: colors.backgrounds.caveWall }}
      >
        <button
          onClick={onBack}
          className="flex items-center gap-1.5 text-sm px-2 py-1 rounded transition-colors"
          style={{ color: colors.typography.ashGrey }}
          onMouseEnter={(e) => (e.currentTarget.style.color = colors.typography.pureLight)}
          onMouseLeave={(e) => (e.currentTarget.style.color = colors.typography.ashGrey)}
        >
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
            <path
              d="M10 3l-5 5 5 5"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
          Back
        </button>
        <div className="flex-1" />
        <div className="flex items-center gap-3 text-xs text-lantern-ash">
          <span>
            Trace: <span className="font-mono text-lantern-pure">{traceId}</span>
          </span>
        </div>
      </div>

      {/* Waterfall tree */}
      <div className="flex-1 overflow-auto px-4 py-4">
        <div className="max-w-4xl mx-auto">
          {/* Root span info bar */}
          <div
            className="mb-4 px-4 py-3 rounded-lg border"
            style={{
              background: colors.backgrounds.charcoalDepth,
              borderColor: kindColor[trace.kind] ? `${kindColor[trace.kind]}44` : colors.backgrounds.caveWall,
              borderLeft: `3px solid ${kindColor[trace.kind] || colors.backgrounds.caveWall}`,
            }}
          >
            <div className="flex items-center gap-3 mb-1">
              <span className="text-sm font-semibold text-lantern-pure">{trace.name}</span>
              {trace.kind && (
                <span
                  className="text-xs px-2 py-0.5 rounded-full font-medium"
                  style={{
                    background: `${kindColor[trace.kind] || colors.backgrounds.caveWall}22`,
                    color: kindColor[trace.kind] || colors.typography.ashGrey,
                  }}
                >
                  {trace.kind}
                </span>
              )}
              {trace.model && (
                <span className="text-xs font-mono text-lantern-ash">{trace.model}</span>
              )}
            </div>
            <div className="flex items-center gap-4 text-xs text-lantern-ash">
              <span>{formatTime(trace.start_time)}</span>
              <span>{formatDuration(trace.start_time, trace.end_time)}</span>
              <span>{totalTokens(trace).toLocaleString()} tokens</span>
              {trace.cost_usd > 0 && (
                <span style={{ color: colors.accents.emberFlare }}>
                  ${trace.cost_usd.toFixed(4)}
                </span>
              )}
            </div>
          </div>

          {/* Span tree */}
          {trace.children && trace.children.length > 0 ? (
            <div className="ml-4 pl-4 border-l-2" style={{ borderColor: colors.backgrounds.caveWall }}>
              {trace.children.map((child) => (
                <SpanRow
                  key={child.span_id}
                  span={child}
                  depth={1}
                  kindColor={kindColor}
                  onShowSaveModal={(span) => {
                    setSaveSpan({
                      span,
                      input: span.input ?? "",
                      output: span.output,
                    });
                  }}
                />
              ))}
            </div>
          ) : (
            <div className="text-xs text-lantern-ash py-4 text-center">
              No child spans
            </div>
          )}
        </div>
      </div>

      {/* Save to dataset modal */}
      {saveSpan && (
        <SaveToDatasetModal
          spanId={saveSpan.span.span_id}
          traceId={traceId}
          input={saveSpan.input}
          expectedOutput={saveSpan.output}
          onClose={() => setSaveSpan(null)}
          onSuccess={handleSaveSuccess}
        />
      )}
    </div>
  );
}

// ── Span Row ───────────────────────────────────────────────────────

function SpanRow({
  span,
  depth,
  kindColor,
  onShowSaveModal,
}: {
  span: Span;
  depth: number;
  kindColor: Record<string, string>;
  onShowSaveModal: (span: Span) => void;
}) {
  const [expanded, setExpanded] = useState(depth <= 1);
  const hasChildren = span.children && span.children.length > 0;
  const hasInput = span.input && span.input.length > 0;

  const indentPx = depth * 20 + 12;

  return (
    <div>
      {/* Span row */}
      <div
        className="group flex items-center gap-2 py-1.5 px-2 rounded-md transition-colors cursor-default"
        style={{ marginLeft: `${indentPx}px` }}
        onMouseEnter={(e) => {
          (e.currentTarget as HTMLElement).style.background = colors.flickerRgba(0.08);
        }}
        onMouseLeave={(e) => {
          (e.currentTarget as HTMLElement).style.background = "transparent";
        }}
      >
        {/* Expand/collapse toggle */}
        {hasChildren ? (
          <button
            onClick={() => setExpanded(!expanded)}
            className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded transition-colors"
            style={{ color: colors.typography.ashGrey }}
            onMouseEnter={(e) => (e.currentTarget.style.color = colors.typography.pureLight)}
            onMouseLeave={(e) => (e.currentTarget.style.color = colors.typography.ashGrey)}
            aria-label={expanded ? "Collapse" : "Expand"}
          >
            <svg
              width="12"
              height="12"
              viewBox="0 0 12 12"
              fill="none"
              className={`transition-transform duration-200 ${expanded ? "rotate-90" : ""}`}
            >
              <path
                d="M4.5 3l3 3-3 3"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </button>
        ) : (
          <div className="w-5 h-5 flex-shrink-0" />
        )}

        {/* Kind indicator */}
        <div
          className="w-2 h-2 rounded-full flex-shrink-0"
          style={{ background: kindColor[span.kind] || colors.backgrounds.caveWall }}
        />

        {/* Span name */}
        <div className="flex-1 min-w-0">
          <div className="text-xs text-lantern-pure truncate">{span.name}</div>
          <div className="text-xs text-lantern-ash font-mono truncate max-w-[200px]">
            {span.span_id.slice(0, 8)}
            {span.model && (
              <span className="ml-2 opacity-60">{span.model}</span>
            )}
          </div>
        </div>

        {/* Duration */}
        <span className="text-xs text-lantern-ash flex-shrink-0">
          {formatDuration(span.start_time, span.end_time)}
        </span>

        {/* Token count */}
        <span className="text-xs text-lantern-ash flex-shrink-0 hidden sm:block">
          {totalTokens(span).toLocaleString()}t
        </span>

        {/* Save to dataset button — only for spans with non-empty input */}
        {hasInput && (
          <button
            onClick={() => onShowSaveModal(span)}
            className="flex-shrink-0 text-xs px-2 py-1 rounded transition-colors opacity-0 group-hover:opacity-100"
            style={{
              background: colors.accents.emberFlare,
              color: colors.typography.pureLight,
            }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLElement).style.background = colors.accents.softGlow;
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLElement).style.background = colors.accents.emberFlare;
            }}
            title="Save to dataset"
            aria-label="Save span to dataset"
          >
            <span className="flex items-center gap-1">
              <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
                <path
                  d="M2 4h12M4 4v8a2 2 0 002 2h4a2 2 0 002-2V4"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
                <path
                  d="M6 4V2h4v2"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
              Save
            </span>
          </button>
        )}
      </div>

      {/* Children */}
      {expanded && hasChildren && (
        <div
          className="pl-3"
          style={{ borderLeft: `1px solid ${colors.backgrounds.caveWall}` }}
        >
          {span.children!.map((child) => (
            <SpanRow
              key={child.span_id}
              span={child}
              depth={depth + 1}
              kindColor={kindColor}
              onShowSaveModal={onShowSaveModal}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function totalTokens(span: Span): number {
  return span.input_tokens + span.output_tokens;
}
