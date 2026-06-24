import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Cell,
} from "recharts";
import { colors } from "@/theme";
import { KIND_COLOR_MAP } from "@/components/spanKind";
import Breadcrumb from "@/components/Breadcrumb";
import JsonCodeBlock from "@/components/JsonCodeBlock";
import { Skeleton } from "@/components/Skeleton";
import { EmptyState, LoadingState } from "@/components/EmptyState";
import { formatTime, formatDuration, formatMs, totalTokens, parseChatTurns, getToolSummary } from "@/utils/formatters";
import { useToast } from "@/components/Toast";
import SaveToDatasetModal from "@/components/SaveToDatasetModal";
import { extractSpanMessages } from "@/utils/spanMessages";

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
  attributes?: Record<string, unknown>;
}

// hasRealContent returns true when a span's input/output field contains at
// least one message with non-empty text content. Filters out the normalizer's
// empty-wrapper pattern ([{"role":"user","content":""}]) that appears for
// non-LLM spans whose actual data lives in the attributes overflow map.
function hasRealContent(val: string | undefined): boolean {
  if (!val || val.trim().length === 0) return false;
  try {
    const parsed = JSON.parse(val);
    if (Array.isArray(parsed)) {
      return parsed.some(
        (m) => m && typeof m.content === "string" && m.content.trim().length > 0,
      );
    }
  } catch {
    // not JSON — treat non-empty string as real content
  }
  return val.trim().length > 0;
}

interface Score {
  eval_name: string;
  value: number;
}

interface TraceResponse {
  trace_id: string;
  project_id: string;
  root_span: Span;
  total_input_tokens?: number;
  total_output_tokens?: number;
  total_cost_usd?: number;
}

export interface WaterfallEntry {
  name: string;
  spanId: string;
  start: number;
  duration: number;
  color: string;
  kind: string;
  model?: string;
}

// ── Trace Detail Page ──────────────────────────────────────────────

const LIVE_POLL_INTERVAL_MS = 5_000;
const DEFAULT_WORD_WRAP = import.meta.env.VITE_DEFAULT_WORD_WRAP !== "false";
const LIVE_TIMEOUT_MS = (() => {
  const raw = import.meta.env.VITE_LIVE_TRACE_TIMEOUT_MINUTES;
  const parsed = Number(raw);
  return (Number.isFinite(parsed) && parsed > 0 ? parsed : 20) * 60 * 1000;
})();

export default function TraceDetailPage({
  traceId,
  activeProject,
  onBack,
}: TraceDetailPageProps) {
  const { addToast } = useToast();
  const [trace, setTrace] = useState<Span | null>(null);
  const [traceRollup, setTraceRollup] = useState<{
    inputTokens: number;
    outputTokens: number;
    costUsd: number;
  } | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saveSpan, setSaveSpan] = useState<{
    span: Span;
    input: string;
    output?: string;
  } | null>(null);
  const [liveStatus, setLiveStatus] = useState<"live" | "stalled" | null>(null);
  const liveStartRef = useRef<number | null>(null);

  // Fetch trace detail and start live polling while the root span is absent.
  // The root span (parent_id = "") is only committed when the agent session
  // ends; until then the server returns a child span as a fallback root and
  // we poll at the Commit Cadence to pick up new spans as they arrive.
  useEffect(() => {
    const controller = new AbortController();
    let intervalId: ReturnType<typeof setInterval> | null = null;

    const loadTrace = () =>
      fetch(
        `/api/v1/traces/${traceId}?project_id=${encodeURIComponent(activeProject)}`,
        { signal: controller.signal },
      ).then((res) => {
        if (!res.ok) {
          if (res.status === 404) throw new Error("Trace not found");
          throw new Error(`Failed to load trace (${res.status})`);
        }
        return res.json() as Promise<TraceResponse>;
      });

    const applyData = (data: TraceResponse) => {
      setTrace(data.root_span);
      setTraceRollup({
        inputTokens: data.total_input_tokens ?? 0,
        outputTokens: data.total_output_tokens ?? 0,
        costUsd: data.total_cost_usd ?? 0,
      });
    };

    setLoading(true);
    setError(null);
    setLiveStatus(null);
    liveStartRef.current = null;

    loadTrace()
      .then((data) => {
        applyData(data);
        setLoading(false);
        // Session is in progress when the real root span (parent_id = "") has
        // not been committed yet — the server falls back to a child span as root.
        const rootArrived = !data.root_span?.parent_id;
        if (!rootArrived) {
          setLiveStatus("live");
          liveStartRef.current = Date.now();
          intervalId = setInterval(() => {
            if (
              liveStartRef.current !== null &&
              Date.now() - liveStartRef.current > LIVE_TIMEOUT_MS
            ) {
              setLiveStatus("stalled");
              if (intervalId !== null) clearInterval(intervalId);
              return;
            }
            loadTrace()
              .then((pollData) => {
                applyData(pollData);
                if (pollData.root_span?.parent_id) return; // still in progress
                setLiveStatus(null);
                if (intervalId !== null) clearInterval(intervalId);
              })
              .catch(() => {
                // Non-fatal: retry next tick
              });
          }, LIVE_POLL_INTERVAL_MS);
        }
      })
      .catch((err) => {
        if (err.name !== "AbortError") {
          setError(err.message);
          setLoading(false);
        }
      });

    return () => {
      controller.abort();
      if (intervalId !== null) clearInterval(intervalId);
    };
  }, [traceId, activeProject]);

  const handleSaveSuccess = useCallback(() => {
    setSaveSpan(null);
    addToast("success", "Span saved to dataset");
  }, [addToast]);

  // View mode and selection state — must be declared before any early returns
  const [viewMode, setViewMode] = useState<"waterfall" | "tree">("waterfall");
  const [selectedSpanId, setSelectedSpanId] = useState<string | null>(null);
  const [expandedSpanId, setExpandedSpanId] = useState<string | null>(null);

  // Flatten the span tree for the waterfall chart
  const allSpans = useMemo(() => {
    if (!trace) return [];
    const result: Span[] = [trace];
    const collect = (spans: Span[]) => {
      for (const span of spans) {
        result.push(span);
        if (span.children) collect(span.children);
      }
    };
    if (trace.children) collect(trace.children);
    return result;
  }, [trace]);

  // Waterfall chart data: bars representing each span
  const waterfallData = useMemo(() => {
    if (!trace) return [];
    const baseTime = new Date(trace.start_time).getTime();
    return allSpans.map((span) => {
      const start = new Date(span.start_time).getTime() - baseTime;
      const end = new Date(span.end_time).getTime() - baseTime;
      const duration = end - start;
      const color = KIND_COLOR_MAP[span.kind] || colors.backgrounds.caveWall;
      return {
        name: span.name,
        spanId: span.span_id,
        start,
        duration,
        color,
        kind: span.kind,
        model: span.model,
      } satisfies WaterfallEntry;
    });
  }, [allSpans, trace]);

  if (loading) {
    return (
      <div className="flex flex-col h-full" style={{ background: colors.backgrounds.abyssBlack }}>
        <div className="px-4 py-3 border-b" style={{ borderColor: colors.backgrounds.caveWall }}>
          <Skeleton className="h-5 w-48 rounded" />
        </div>
        <div className="flex-1 overflow-auto p-4">
          <Skeleton className="h-16 w-full rounded-lg mb-4" />
          <LoadingState rows={5} rowHeight="2rem" />
        </div>
      </div>
    );
  }

  if (error || !trace) {
    return (
      <div className="flex flex-col h-full">
        <EmptyState
          variant="error"
          title={error ?? "Trace not found"}
          description="The requested trace could not be loaded"
          actionLabel="Back to Traces"
          onAction={onBack}
        />
      </div>
    );
  }

  const selectedSpan = allSpans.find((s) => s.span_id === selectedSpanId);

  return (
    <div className="flex flex-col h-full" style={{ background: colors.backgrounds.abyssBlack }}>
      {/* Header with breadcrumb */}
      <div className="flex flex-col gap-2 px-4 py-3 border-b"
        style={{ borderColor: colors.backgrounds.caveWall }}
      >
        <Breadcrumb items={[
          { label: "Traces", onClick: onBack },
          { label: `Trace: ${traceId.slice(0, 8)}…` },
        ]} />
        <div className="flex items-center gap-3">
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
          {liveStatus !== null && (
            <div
              className="flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs"
              style={{
                background:
                  liveStatus === "live"
                    ? "rgba(139, 92, 246, 0.15)"
                    : "rgba(107, 114, 128, 0.15)",
                color:
                  liveStatus === "live"
                    ? colors.accents.flicker
                    : colors.typography.ashGrey,
              }}
            >
              <span
                className={`w-1.5 h-1.5 rounded-full ${liveStatus === "live" ? "animate-pulse" : ""}`}
                style={{
                  background:
                    liveStatus === "live"
                      ? colors.accents.flicker
                      : colors.typography.ashGrey,
                }}
              />
              {liveStatus === "live" ? "Live" : "Stalled"}
            </div>
          )}
          <span className="text-xs text-omneval-text-muted">
            <span className="font-mono text-omneval-text-pure">{traceId}</span>
          </span>
        </div>
      </div>

      {/* View mode tabs */}
      <div className="flex items-center gap-1 px-4 py-2 border-b" style={{ borderColor: colors.backgrounds.caveWall }}>
        {(["waterfall", "tree"] as const).map((mode) => (
          <button
            key={mode}
            onClick={() => setViewMode(mode)}
            className={`px-3 py-1.5 text-sm rounded-md transition-colors capitalize ${
              mode === viewMode
                ? "text-omneval-violet-pale"
                : "text-omneval-text-muted hover:text-omneval-text-pure"
            }`}
            style={
              mode === viewMode
                ? {
                    background: colors.backgrounds.charcoalDepth,
                    borderBottom: `2px solid ${colors.accents.emberFlare}`,
                  }
                : {}
            }
            aria-label={`Switch to ${mode} view`}
          >
            {mode === "waterfall" ? "Waterfall" : "Tree"}
          </button>
        ))}
      </div>

      <div className="flex-1 flex overflow-hidden">
        {/* Main content */}
        <div className="flex-1 flex flex-col overflow-hidden">
          {/* Root span info bar */}
          <div
            className="mx-4 mt-4 mb-4 px-4 py-3 rounded-lg border"
            style={{
              background: colors.backgrounds.charcoalDepth,
              borderColor: KIND_COLOR_MAP[trace.kind] ? `${KIND_COLOR_MAP[trace.kind]}44` : colors.backgrounds.caveWall,
              borderLeft: `3px solid ${KIND_COLOR_MAP[trace.kind] || colors.backgrounds.caveWall}`,
            }}
          >
            <div className="flex items-center gap-3 mb-1">
              <span className="text-sm font-semibold text-omneval-text-pure">{trace.name}</span>
              {trace.kind && (
                <span
                  className="text-xs px-2 py-0.5 rounded-full font-medium"
                  style={{
                    background: `${KIND_COLOR_MAP[trace.kind] || colors.backgrounds.caveWall}22`,
                    color: KIND_COLOR_MAP[trace.kind] || colors.typography.ashGrey,
                  }}
                >
                  {trace.kind}
                </span>
              )}
              {trace.model && (
                <span className="text-xs font-mono text-omneval-text-muted">{trace.model}</span>
              )}
            </div>
            <div className="flex items-center gap-4 text-xs text-omneval-text-muted">
              <span>{formatTime(trace.start_time)}</span>
              <span>{formatDuration(trace.start_time, trace.end_time)}</span>
              <span>
                {(
                  (traceRollup?.inputTokens ?? 0) + (traceRollup?.outputTokens ?? 0)
                ).toLocaleString()}{" "}
                tokens
              </span>
              {(traceRollup?.costUsd ?? 0) > 0 && (
                <span style={{ color: colors.accents.emberFlare }}>
                  ${(traceRollup?.costUsd ?? 0).toFixed(4)}
                </span>
              )}
            </div>
          </div>

          {/* View content */}
          <div className="flex-1 overflow-auto px-4 pb-4">
            <div className="max-w-4xl mx-auto">
              {viewMode === "waterfall" ? (
                <GanttWaterfall
                  data={waterfallData}
                  selectedSpanId={selectedSpanId}
                  onSelect={(spanId) => setSelectedSpanId(selectedSpanId === spanId ? null : spanId)}
                />
              ) : (
                <TraceTree
                  trace={trace}
                  expandedSpanId={expandedSpanId}
                  onToggleExpand={(id) => setExpandedSpanId(expandedSpanId === id ? null : id)}
                  onShowSaveModal={(span) => setSaveSpan({ span, input: span.input ?? "", output: span.output })}
                  selectedSpanId={selectedSpanId}
                  onSelect={(spanId) => setSelectedSpanId(selectedSpanId === spanId ? null : spanId)}
                />
              )}
            </div>
          </div>
        </div>

        {/* Slide-in detail panel for selected span */}
        {selectedSpan && (
          <SlideInDetailPanel
            span={selectedSpan}
            onClose={() => setSelectedSpanId(null)}
            onSave={() => setSaveSpan({ span: selectedSpan, input: selectedSpan.input ?? "", output: selectedSpan.output })}
          />
        )}
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

// ── Chat Turns Renderer ────────────────────────────────────────────

const ROLE_COLORS: Record<string, string> = {
  system: colors.typography.ashGrey,
  user: colors.accents.softGlow,
  assistant: colors.accents.flicker,
  tool: colors.accents.amberWarning,
};

function ChatTurnsView({ value, label, wordWrap }: { value: string; label: string; wordWrap: boolean }) {
  const turns = parseChatTurns(value);

  if (!turns) {
    return <JsonCodeBlock value={value} label={label} maxHeight={300} />;
  }

  return (
    <div>
      <div className="text-xs font-medium text-omneval-text-muted mb-2 uppercase tracking-wider">
        {label}
      </div>
      <div
        className="rounded-md border overflow-hidden"
        style={{
          backgroundColor: colors.backgrounds.abyssBlack,
          borderColor: colors.backgrounds.caveWall,
        }}
      >
        {turns.map((turn, i) => (
          <div
            key={i}
            className="px-3 py-2 text-xs"
            style={{
              borderBottom: i < turns.length - 1
                ? `1px solid ${colors.backgrounds.caveWall}`
                : undefined,
            }}
          >
            <span
              className="font-semibold uppercase tracking-wider mr-2"
              style={{ color: ROLE_COLORS[turn.role] ?? colors.typography.ashGrey, fontSize: "0.65rem" }}
            >
              {turn.role}
            </span>
            <span
              className={`text-omneval-text-pure ${wordWrap ? "whitespace-pre-wrap" : "whitespace-pre"}`}
              style={wordWrap ? undefined : { display: "block", overflowX: "auto" }}
            >
              {turn.content}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Formatted Chat Messages ────────────────────────────────────────

interface FormattedChatMessagesProps {
  messages: { role: string; content: string }[];
  toolCalls: { id: string; type: string; function: string; input: string; output?: string }[] | null;
}

function FormattedChatMessages({ messages, toolCalls }: FormattedChatMessagesProps) {
  return (
    <div>
      {messages.length > 0 && (
        <>
          <div className="text-xs font-medium text-omneval-text-muted mb-2 uppercase tracking-wider">
            Messages
          </div>
          <div
            className="rounded-md border overflow-hidden"
            style={{
              backgroundColor: colors.backgrounds.abyssBlack,
              borderColor: colors.backgrounds.caveWall,
            }}
          >
            {messages.map((msg, i) => (
              <div
                key={i}
                className="px-3 py-2 text-xs"
                style={{
                  borderBottom: i < messages.length - 1
                    ? `1px solid ${colors.backgrounds.caveWall}`
                    : undefined,
                }}
              >
                <span
                  className="font-semibold uppercase tracking-wider mr-2"
                  style={{ color: ROLE_COLORS[msg.role] ?? colors.typography.ashGrey, fontSize: "0.65rem" }}
                >
                  {msg.role}
                </span>
                <span className="text-omneval-text-pure">
                  {msg.content}
                </span>
              </div>
            ))}
          </div>
        </>
      )}

      {/* Tool calls */}
      {toolCalls != null && toolCalls.length > 0 && (
        <div className="mt-3">
          <div className="text-xs font-medium mb-1 uppercase tracking-wider" style={{ color: colors.typography.ashGrey }}>
            Tool Calls ({toolCalls.length})
          </div>
          {toolCalls.map((tc, i) => (
            <div
              key={i}
              className="rounded-md border overflow-hidden text-xs mb-2"
              style={{
                backgroundColor: colors.backgrounds.abyssBlack,
                borderColor: colors.backgrounds.caveWall,
              }}
            >
              <div
                className="px-3 py-2 font-mono"
                style={{ borderBottom: `1px solid ${colors.backgrounds.caveWall}`, color: colors.accents.softGlow }}
              >
                <span className="font-semibold">call</span>:{tc.id} · {tc.function}
              </div>
              <div className="px-3 py-2" style={{ color: colors.typography.ashGrey }}>
                <span className="text-omneval-text-muted mr-2">Input:</span>
                <span className="font-mono whitespace-pre-wrap break-all">{tc.input}</span>
              </div>
              {tc.output && (
                <div className="px-3 py-2 border-t" style={{ borderColor: colors.backgrounds.caveWall }}>
                  <span className="text-omneval-text-muted mr-2">Output:</span>
                  <span className="font-mono whitespace-pre-wrap break-all">{tc.output}</span>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── Score Badges ───────────────────────────────────────────────────

function ScoreBadges({ scores }: { scores: Score[] }) {
  return (
    <div className="flex flex-wrap gap-2">
      {scores.map((score) => (
        <span
          key={score.eval_name}
          className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium"
          style={{
            background: score.value >= 0.8
              ? "rgba(76, 175, 80, 0.15)"
              : score.value >= 0.5
                ? "rgba(255, 193, 7, 0.15)"
                : "rgba(244, 67, 54, 0.15)",
            color: score.value >= 0.8
              ? "#4CAF50"
              : score.value >= 0.5
                ? "#FFC107"
                : "#F44336",
          }}
        >
          {score.eval_name}: {score.value.toFixed(2)}
        </span>
      ))}
    </div>
  );
}

// ── Gantt-Style Waterfall Chart ────────────────────────────────────

function GanttWaterfall({
  data,
  selectedSpanId,
  onSelect,
}: {
  data: WaterfallEntry[];
  selectedSpanId: string | null;
  onSelect: (spanId: string) => void;
}) {
  if (data.length === 0) {
    return <EmptyState variant="default" title="No spans" description="This trace has no child spans" />;
  }

  return (
    <div className="space-y-4">
      {/* Gantt bars */}
      <div
        className="rounded-lg border overflow-hidden"
        style={{
          backgroundColor: colors.backgrounds.charcoalDepth,
          borderColor: colors.backgrounds.caveWall,
        }}
      >
        <div className="px-4 py-2 border-b" style={{ borderColor: colors.backgrounds.caveWall }}>
          <span className="text-xs font-medium text-omneval-text-muted uppercase tracking-wider">Waterfall Timeline</span>
        </div>
        <div className="overflow-x-auto">
          <ResponsiveContainer width="100%" height={Math.max(60, data.length * 40)}>
            <BarChart data={data} layout="vertical" margin={{ top: 5, right: 80, left: 120, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke={colors.backgrounds.caveWall} horizontal={false} />
              <XAxis
                type="number"
                dataKey="start"
                tick={{ fill: colors.typography.ashGrey, fontSize: 11 }}
                axisLine={{ stroke: colors.backgrounds.caveWall }}
                tickFormatter={formatMs}
              />
              <YAxis
                type="category"
                dataKey="name"
                tick={{ fill: colors.typography.pureLight, fontSize: 11, width: 110 }}
                width={120}
              />
              <Tooltip
                contentStyle={{
                  backgroundColor: colors.backgrounds.charcoalDepth,
                  border: `1px solid ${colors.backgrounds.caveWall}`,
                  borderRadius: "0.375rem",
                  color: colors.typography.pureLight,
                  fontSize: "0.8125rem",
                }}
                formatter={(value: number, name: string) => {
                  if (name === "start" || name === "duration") return [formatMs(value), name === "start" ? "Start offset" : "Duration"];
                  return [value, name];
                }}
                labelFormatter={(label) => label}
              />
              <Bar dataKey="start" stackId="a" fill="transparent" />
              <Bar
                dataKey="duration"
                stackId="a"
                fill={colors.accents.emberFlare}
                radius={[0, 3, 3, 0]}
                cursor="pointer"
              >
                {data.map((entry, index) => (
                  <Cell
                    key={`cell-${index}`}
                    fill={entry.color}
                    fillOpacity={selectedSpanId === entry.spanId ? 1 : 0.8}
                    stroke={selectedSpanId === entry.spanId ? "#fff" : "transparent"}
                    strokeWidth={selectedSpanId === entry.spanId ? 1 : 0}
                    onClick={() => onSelect(entry.spanId)}
                  />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      </div>

      {/* Span list */}
      <div
        className="rounded-lg border overflow-hidden"
        style={{
          backgroundColor: colors.backgrounds.charcoalDepth,
          borderColor: colors.backgrounds.caveWall,
        }}
      >
        <div className="px-4 py-2 border-b" style={{ borderColor: colors.backgrounds.caveWall }}>
          <span className="text-xs font-medium text-omneval-text-muted uppercase tracking-wider">Spans ({data.length})</span>
        </div>
        {data.map((entry, i) => (
          <div
            key={entry.spanId}
            className={`flex items-center gap-3 px-4 py-2 cursor-pointer transition-colors ${
              selectedSpanId === entry.spanId
                ? ""
                : "hover:bg-omneval-violet-hover"
            }`}
            style={{
              backgroundColor: selectedSpanId === entry.spanId
                ? colors.flickerRgba(0.08)
                : i % 2 === 0
                  ? "transparent"
                  : `${colors.backgrounds.slightIllumination}33`,
              borderLeft: `3px solid ${entry.color}`,
            }}
            onClick={() => onSelect(entry.spanId)}
            role="button"
            tabIndex={0}
            aria-label={`Select span: ${entry.name}`}
            onKeyDown={(e) => { if (e.key === "Enter") onSelect(entry.spanId); }}
          >
            <div className="w-2 h-2 rounded-full flex-shrink-0" style={{ backgroundColor: entry.color }} />
            <div className="flex-1 min-w-0">
              <div className="text-xs font-medium text-omneval-text-pure truncate">{entry.name}</div>
              {entry.model && (
                <div className="text-xs text-omneval-text-muted font-mono truncate max-w-[150px]">{entry.model}</div>
              )}
            </div>
            <span className="text-xs text-omneval-text-muted flex-shrink-0">{formatMs(entry.duration)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Slide-In Detail Panel ──────────────────────────────────────────

function SlideInDetailPanel({
  span,
  onClose,
  onSave,
}: {
  span: Span;
  onClose: () => void;
  onSave: () => void;
}) {
  const [wordWrap, setWordWrap] = useState(DEFAULT_WORD_WRAP);
  const [activeTab, setActiveTab] = useState<"chat" | "raw">("chat");

  const hasInput = hasRealContent(span.input);
  const hasOutput = hasRealContent(span.output);
  const hasAttributes =
    span.attributes != null && Object.keys(span.attributes).length > 0;

  // Extract structured messages from attributes
  const extracted = useMemo(() => {
    if (span.attributes != null) return extractSpanMessages(span.attributes);
    return { messages: null, toolCalls: null, raw: {} };
  }, [span.attributes]);

  const hasChatContent =
    (extracted.messages != null && extracted.messages.length > 0) ||
    (extracted.toolCalls != null && extracted.toolCalls.length > 0);

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-40 bg-black/40"
        onClick={onClose}
      />
      {/* Panel */}
      <aside
        className="fixed right-0 top-0 bottom-0 z-50 w-full max-w-xl border-l overflow-hidden flex flex-col"
        style={{
          backgroundColor: colors.backgrounds.charcoalDepth,
          borderColor: colors.backgrounds.caveWall,
          boxShadow: "-8px 0 24px rgba(0,0,0,0.5)",
        }}
        aria-label="Span detail panel"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b" style={{ borderColor: colors.backgrounds.caveWall }}>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-sm font-semibold text-omneval-text-pure truncate">{span.name}</span>
              {span.kind && (
                <span
                  className="text-xs px-2 py-0.5 rounded-full font-medium flex-shrink-0"
                  style={{
                    background: `${KIND_COLOR_MAP[span.kind] || colors.backgrounds.caveWall}22`,
                    color: KIND_COLOR_MAP[span.kind] || colors.typography.ashGrey,
                  }}
                >
                  {span.kind}
                </span>
              )}
            </div>
            <div className="flex items-center gap-3 text-xs text-omneval-text-muted mt-1">
              <span>{formatDuration(span.start_time, span.end_time)}</span>
              <span>{totalTokens(span).toLocaleString()} tokens</span>
              {span.cost_usd > 0 && (
                <span style={{ color: colors.accents.emberFlare }}>${span.cost_usd.toFixed(4)}</span>
              )}
            </div>
          </div>
          <div className="flex items-center gap-1">
            <button
              onClick={() => setWordWrap((w) => !w)}
              className="flex-shrink-0 p-1.5 rounded-md transition-colors"
              style={{ color: wordWrap ? colors.accents.flicker : colors.typography.ashGrey }}
              onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = colors.backgrounds.slightIllumination)}
              onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = "transparent")}
              aria-label={wordWrap ? "Disable word wrap" : "Enable word wrap"}
              title={wordWrap ? "Word wrap: on" : "Word wrap: off"}
            >
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                <path d="M2 4h12M2 8h8a2 2 0 010 4H8m0 0l2-2m-2 2l2 2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </button>
            <button
              onClick={onClose}
              className="flex-shrink-0 p-1.5 rounded-md transition-colors"
              style={{ color: colors.typography.ashGrey }}
              onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = colors.backgrounds.slightIllumination)}
              onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = "transparent")}
              aria-label="Close detail panel"
            >
              <svg width="18" height="18" viewBox="0 0 16 16" fill="none">
                <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
            </button>
          </div>
        </div>

        {/* Tab bar — show only when structured content exists */}
        {hasChatContent && (
          <div className="flex border-b" style={{ borderColor: colors.backgrounds.caveWall }}>
            <button
              onClick={() => setActiveTab("chat")}
              className={`flex-1 px-4 py-2.5 text-xs font-medium transition-colors ${
                activeTab === "chat"
                  ? "border-b-2"
                  : "opacity-50 hover:opacity-100"
              }`}
              style={
                activeTab === "chat"
                  ? {
                      borderColor: colors.accents.emberFlare,
                      color: colors.typography.pureLight,
                    }
                  : {
                      borderColor: "transparent",
                      color: colors.typography.ashGrey,
                    }
              }
            >
              Chat
            </button>
            <button
              onClick={() => setActiveTab("raw")}
              className={`flex-1 px-4 py-2.5 text-xs font-medium transition-colors ${
                activeTab === "raw"
                  ? "border-b-2"
                  : "opacity-50 hover:opacity-100"
              }`}
              style={
                activeTab === "raw"
                  ? {
                      borderColor: colors.accents.emberFlare,
                      color: colors.typography.pureLight,
                    }
                  : {
                      borderColor: "transparent",
                      color: colors.typography.ashGrey,
                    }
              }
            >
              Raw
            </button>
          </div>
        )}

        {/* Content */}
        <div className="flex-1 overflow-auto px-4 py-4 space-y-4">
          {/* Metadata */}
          <div className="grid grid-cols-2 gap-3 text-xs">
            <div>
              <span className="text-omneval-text-muted">Span ID</span>
              <div className="font-mono text-omneval-text-pure mt-0.5 truncate" title={span.span_id}>
                {span.span_id}
              </div>
            </div>
            <div>
              <span className="text-omneval-text-muted">Parent ID</span>
              <div className="font-mono text-omneval-text-pure mt-0.5 truncate" title={span.parent_id}>
                {span.parent_id || "—"}
              </div>
            </div>
            {span.model && (
              <div>
                <span className="text-omneval-text-muted">Model</span>
                <div className="text-omneval-text-pure mt-0.5">{span.model}</div>
              </div>
            )}
            {span.status_code && (
              <div>
                <span className="text-omneval-text-muted">Status</span>
                <div className="text-omneval-text-pure mt-0.5">{span.status_code}</div>
              </div>
            )}
          </div>

          {/* Scores */}
          {span.scores && span.scores.length > 0 && (
            <div>
              <div className="text-xs font-medium text-omneval-text-muted mb-2">Scores</div>
              <ScoreBadges scores={span.scores} />
            </div>
          )}

          {/* ── Chat view: formatted messages ── */}
          {hasChatContent && activeTab === "chat" && (
            <FormattedChatMessages
              messages={extracted.messages ?? []}
              toolCalls={extracted.toolCalls}
            />
          )}

          {/* ── Raw view (or fallback when no structured content exists) ── */}
          {(!hasChatContent || activeTab === "raw") && (
            <>
              {hasInput && (
                <ChatTurnsView value={span.input!} label="Input" wordWrap={wordWrap} />
              )}
              {hasOutput && (
                <ChatTurnsView value={span.output!} label="Output" wordWrap={wordWrap} />
              )}
              {hasAttributes && (
                <div>
                  <div className="text-xs font-medium text-omneval-text-muted mb-2 uppercase tracking-wider">
                    Attributes
                  </div>
                  <JsonCodeBlock
                    value={JSON.stringify(span.attributes, null, 2)}
                    label="Attributes"
                    maxHeight={400}
                  />
                </div>
              )}
            </>
          )}

          {hasInput && (
            <button
              onClick={onSave}
              className="w-full flex items-center justify-center gap-2 px-4 py-2 text-sm font-medium rounded-md text-white transition-all duration-150 hover:brightness-110"
              style={{
                background: colors.accents.emberFlare,
                boxShadow: "0 2px 8px rgba(255, 87, 34, 0.25)",
              }}
            >
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                <path d="M2 4h12M4 4v8a2 2 0 002 2h4a2 2 0 002-2V4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                <path d="M6 4V2h4v2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              Save to Dataset
            </button>
          )}
        </div>
      </aside>
    </>
  );
}

// ── Trace Tree View ────────────────────────────────────────────────

function TraceTree({
  trace,
  expandedSpanId,
  onToggleExpand,
  onShowSaveModal,
  selectedSpanId,
  onSelect,
}: {
  trace: Span;
  expandedSpanId: string | null;
  onToggleExpand: (id: string) => void;
  onShowSaveModal: (span: Span) => void;
  selectedSpanId: string | null;
  onSelect: (spanId: string) => void;
}) {
  return (
    <div>
      <SpanRow
        span={trace}
        depth={0}
        onShowSaveModal={onShowSaveModal}
        expandedSpanId={expandedSpanId}
        onToggleExpand={onToggleExpand}
        selectedSpanId={selectedSpanId}
        onSelect={onSelect}
      />
    </div>
  );
}

// ── Span Row (Tree View) ───────────────────────────────────────────

function SpanRow({
  span,
  depth,
  onShowSaveModal,
  expandedSpanId,
  onToggleExpand,
  selectedSpanId,
  onSelect,
}: {
  span: Span;
  depth: number;
  onShowSaveModal: (span: Span) => void;
  expandedSpanId: string | null;
  onToggleExpand: (id: string) => void;
  selectedSpanId: string | null;
  onSelect: (spanId: string) => void;
}) {
  const isExpanded = expandedSpanId === span.span_id;
  const [treeExpanded, setTreeExpanded] = useState(depth <= 1);
  const hasChildren = span.children && span.children.length > 0;
  const hasInput = span.input && span.input.length > 0;
  const hasOutput = span.output && span.output.length > 0;
  const isToolSpan = span.kind === "tool" || /Action$/.test(span.name);
  const toolSummary = isToolSpan ? getToolSummary(span.name, span.input, span.output) : null;

  const indentPx = depth * 20 + 12;
  const isSelected = selectedSpanId === span.span_id;

  return (
    <div>
      {/* Span row */}
      <div
        className={`group flex items-center gap-2 py-1.5 px-2 rounded-md transition-colors ${
          isSelected ? "cursor-pointer" : "cursor-default"
        }`}
        style={{
          marginLeft: `${indentPx}px`,
          backgroundColor: isSelected ? colors.flickerRgba(0.08) : undefined,
        }}
        onClick={() => onSelect(span.span_id)}
        onMouseEnter={(e) => {
          if (!isSelected) (e.currentTarget as HTMLElement).style.background = colors.flickerRgba(0.08);
        }}
        onMouseLeave={(e) => {
          if (!isSelected) (e.currentTarget as HTMLElement).style.background = "transparent";
        }}
      >
        <div
          className="w-2 h-2 rounded-full flex-shrink-0"
          style={{ background: KIND_COLOR_MAP[span.kind] || colors.backgrounds.caveWall }}
        />
        {/* Expand/collapse toggle */}
        {hasChildren ? (
          <button
            onClick={(e) => {
              e.stopPropagation();
              setTreeExpanded(!treeExpanded);
            }}
            className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded transition-colors"
            style={{ color: colors.typography.ashGrey }}
            onMouseEnter={(e) => (e.currentTarget.style.color = colors.typography.pureLight)}
            onMouseLeave={(e) => (e.currentTarget.style.color = colors.typography.ashGrey)}
            aria-label={treeExpanded ? "Collapse" : "Expand"}
          >
            <svg
              width="12"
              height="12"
              viewBox="0 0 12 12"
              fill="none"
              className={`transition-transform duration-200 ${treeExpanded ? "rotate-90" : ""}`}
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

        {/* Detail toggle */}
        <button
          onClick={(e) => {
            e.stopPropagation();
            onToggleExpand(span.span_id);
          }}
          className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded transition-colors"
          style={{
            color: isExpanded ? colors.accents.emberFlare : colors.typography.ashGrey,
          }}
          onMouseEnter={(e) => (e.currentTarget.style.color = colors.typography.pureLight)}
          onMouseLeave={(e) => {
            e.currentTarget.style.color = isExpanded
              ? colors.accents.emberFlare
              : colors.typography.ashGrey;
          }}
          aria-label={isExpanded ? "Hide details" : "Show details"}
        >
          <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
            <path d="M8 3v10M3 8h10" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        </button>

        {/* Span name */}
        <div className="flex-1 min-w-0">
          <div className="text-xs text-omneval-text-pure truncate">{span.name}</div>
          <div className="text-xs text-omneval-text-muted font-mono truncate max-w-[200px]">
            {span.span_id.slice(0, 8)}
            {span.model && (
              <span className="ml-2 opacity-60">{span.model}</span>
            )}
          </div>
          {toolSummary && (
            <div
              className="text-xs font-mono truncate max-w-[480px] mt-0.5"
              style={{ color: colors.typography.ashGrey }}
              title={toolSummary.title}
            >
              {toolSummary.title}
            </div>
          )}
        </div>

        {/* Duration */}
        <span className="text-xs text-omneval-text-muted flex-shrink-0">
          {formatDuration(span.start_time, span.end_time)}
        </span>

        {/* Token count */}
        <span className="text-xs text-omneval-text-muted flex-shrink-0 hidden sm:block">
          {totalTokens(span).toLocaleString()}t
        </span>

        {/* Save to dataset button */}
        {hasInput && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              onShowSaveModal(span);
            }}
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
                <path d="M2 4h12M4 4v8a2 2 0 002 2h4a2 2 0 002-2V4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                <path d="M6 4V2h4v2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              Save
            </span>
          </button>
        )}
      </div>

      {/* Detail panel */}
      {isExpanded && (
        <div
          className="px-3 pb-3 ml-3"
          style={{
            borderLeft: `1px solid ${colors.backgrounds.caveWall}`,
            marginLeft: `${indentPx + 12}px`,
          }}
        >
          {toolSummary && (
            <div className="mb-3">
              <div className="text-xs font-medium mb-1 uppercase tracking-wider" style={{ color: colors.typography.ashGrey }}>
                Tool Call
              </div>
              <div
                className="rounded-md border overflow-hidden text-xs"
                style={{
                  backgroundColor: colors.backgrounds.abyssBlack,
                  borderColor: colors.backgrounds.caveWall,
                }}
              >
                <div
                  className="px-3 py-2 font-mono break-all"
                  style={{ borderBottom: `1px solid ${colors.backgrounds.caveWall}`, color: colors.typography.pureLight }}
                >
                  {toolSummary.title}
                </div>
                <pre className="px-3 py-2 whitespace-pre-wrap break-all" style={{ color: colors.typography.ashGrey }}>
                  {toolSummary.detail}
                </pre>
              </div>
            </div>
          )}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {hasInput && (
              <JsonCodeBlock value={span.input!} label="Input" maxHeight={200} />
            )}
            {hasOutput && (
              <JsonCodeBlock value={span.output!} label="Output" maxHeight={200} />
            )}
          </div>
          {span.scores && span.scores.length > 0 && (
            <div className="mt-3">
              <div className="text-xs font-medium mb-1" style={{ color: colors.typography.ashGrey }}>Scores</div>
              <ScoreBadges scores={span.scores} />
            </div>
          )}
        </div>
      )}

      {/* Children */}
      {treeExpanded && hasChildren && (
        <div
          className="pl-3"
          style={{ borderLeft: `1px solid ${colors.backgrounds.caveWall}` }}
        >
          {span.children!.map((child) => (
            <SpanRow
              key={child.span_id}
              span={child}
              depth={depth + 1}
              onShowSaveModal={onShowSaveModal}
              expandedSpanId={expandedSpanId}
              onToggleExpand={onToggleExpand}
              selectedSpanId={selectedSpanId}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </div>
  );
}

