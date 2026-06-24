import { useState, useEffect } from "react";
import { colors } from "@/theme";
import Breadcrumb from "@/components/Breadcrumb";
import { CopyButton } from "@/components/CopyButton";
import { Skeleton } from "@/components/Skeleton";
import { EmptyState, LoadingState } from "@/components/EmptyState";
import {
  formatTime,
  formatDuration,
  parseChatTurns,
} from "@/utils/formatters";

// ── Constants ──────────────────────────────────────────────────────

// Mirrors KIND_COLOR_MAP in TraceDetail.tsx so kind pills look identical
// across the two pages.
const KIND_COLOR_MAP: Record<string, string> = {
  llm: colors.accents.emberFlare,
  tool: colors.accents.softGlow,
  agent: colors.accents.flicker,
  chain: "#60a5fa",
  internal: colors.typography.ashGrey,
};

/** localStorage key persisting the "Show full message history" toggle. */
const SHOW_FULL_HISTORY_KEY = "omneval_conv_show_full_history";

// ── Types ──────────────────────────────────────────────────────────

interface ConversationDetailPageProps {
  conversationId: string;
  activeProject: string;
  onBack: () => void;
  onNavigateToTrace: (traceId: string) => void;
  onNavigateToTraceDetail: (traceId: string) => void;
}

interface ConversationTraceItem {
  trace_id: string;
  root_span_name: string;
  root_span_kind: string;
  start_time: string;
  end_time: string;
  span_count: number;
  cost_usd: number;
  input_tokens: number;
  output_tokens: number;
  model: string;
}

interface ConversationDetailResponse {
  conversation_id: string;
  traces: ConversationTraceItem[];
}

interface DetailSpan {
  span_id: string;
  kind: string;
  input?: string;
  output?: string;
  children?: DetailSpan[];
}

/** Per-trace LLM input/output extracted from the trace's span tree. */
interface TraceIO {
  input?: string;
  output?: string;
}

// ── Helpers ────────────────────────────────────────────────────────

function readShowFullHistory(): boolean {
  try {
    return window.localStorage.getItem(SHOW_FULL_HISTORY_KEY) === "true";
  } catch {
    return false;
  }
}

/** Walk the span tree and return the I/O of the last LLM span (falling back
 *  to the root span's own I/O) — the conversation-level "what was said". */
export function extractTraceIO(root: DetailSpan | null): TraceIO {
  if (!root) return {};
  let llm: DetailSpan | null = null;
  const walk = (s: DetailSpan) => {
    if (s.kind === "llm" && (s.input || s.output)) llm = s;
    for (const child of s.children ?? []) walk(child);
  };
  walk(root);
  const pick: DetailSpan = llm ?? root;
  return { input: pick.input, output: pick.output };
}

// ── Page ───────────────────────────────────────────────────────────

export default function ConversationDetailPage({
  conversationId,
  activeProject,
  onBack,
  onNavigateToTrace,
  onNavigateToTraceDetail,
}: ConversationDetailPageProps) {
  const [traces, setTraces] = useState<ConversationTraceItem[]>([]);
  const [traceIO, setTraceIO] = useState<Record<string, TraceIO>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showFullHistory, setShowFullHistory] = useState(readShowFullHistory);

  // Persist the toggle.
  useEffect(() => {
    try {
      window.localStorage.setItem(SHOW_FULL_HISTORY_KEY, String(showFullHistory));
    } catch {
      // localStorage may be unavailable
    }
  }, [showFullHistory]);

  // Fetch the conversation's trace list, then each trace's spans (in
  // parallel) for the inline LLM input/output previews.
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    fetch(
      `/api/v1/conversations/${conversationId}?project_id=${encodeURIComponent(activeProject)}`,
      { signal: controller.signal },
    )
      .then((res) => {
        if (!res.ok) {
          if (res.status === 404) throw new Error("Conversation not found");
          throw new Error(`Failed to load conversation (${res.status})`);
        }
        return res.json();
      })
      .then(async (data: ConversationDetailResponse) => {
        const items = data.traces ?? [];
        setTraces(items);
        setLoading(false);
        // Fetch span-level I/O for each trace in parallel (the detail
        // endpoint returns root-span metadata only).
        const results = await Promise.all(
          items.map(async (t) => {
            try {
              const res = await fetch(
                `/api/v1/traces/${t.trace_id}?project_id=${encodeURIComponent(activeProject)}`,
                { signal: controller.signal },
              );
              if (!res.ok) return [t.trace_id, {}] as const;
              const trace = await res.json();
              return [t.trace_id, extractTraceIO(trace.root_span ?? null)] as const;
            } catch {
              return [t.trace_id, {}] as const;
            }
          }),
        );
        setTraceIO(Object.fromEntries(results));
      })
      .catch((err) => {
        if (err.name !== "AbortError") {
          setError(err.message);
          setLoading(false);
        }
      });
    return () => {
      controller.abort();
    };
  }, [conversationId, activeProject]);

  if (loading) {
    return (
      <div className="flex flex-col h-full" style={{ background: colors.backgrounds.abyssBlack }}>
        <div className="px-4 py-3 border-b" style={{ borderColor: colors.backgrounds.caveWall }}>
          <Skeleton className="h-5 w-48 rounded" />
        </div>
        <div className="flex-1 overflow-auto p-4">
          <Skeleton className="h-16 w-full rounded-lg mb-4" />
          <LoadingState rows={4} rowHeight="3rem" />
        </div>
      </div>
    );
  }

  if (error || traces.length === 0) {
    return (
      <div className="flex flex-col h-full">
        <EmptyState
          variant="error"
          title={error ?? "Conversation not found"}
          description="The requested conversation could not be loaded"
          actionLabel="Back to Conversations"
          onAction={onBack}
        />
      </div>
    );
  }

  const totalCost = traces.reduce((sum, t) => sum + t.cost_usd, 0);
  const serviceModel = traces.find((t) => t.model)?.model ?? "";
  const firstStart = traces[0]?.start_time ?? "";
  const lastEnd = traces[traces.length - 1]?.end_time || traces[traces.length - 1]?.start_time || "";

  return (
    <div className="flex flex-col h-full" style={{ background: colors.backgrounds.abyssBlack }}>
      {/* Header with breadcrumb */}
      <div
        className="flex flex-col gap-2 px-4 py-3 border-b"
        style={{ borderColor: colors.backgrounds.caveWall }}
      >
        <Breadcrumb
          items={[
            { label: "Conversations", onClick: onBack },
            { label: `${conversationId.slice(0, 8)}…` },
          ]}
        />
        <div className="flex items-center gap-3 flex-wrap">
          <span className="font-mono text-sm text-omneval-text-pure">{conversationId}</span>
          <CopyButton text={conversationId} ariaLabel="Copy conversation ID" />
          <div className="flex-1" />
          <span className="text-xs text-omneval-text-muted">
            {traces.length} trace{traces.length === 1 ? "" : "s"}
          </span>
          {serviceModel && (
            <span className="text-xs font-mono text-omneval-text-muted">{serviceModel}</span>
          )}
          {totalCost > 0 && (
            <span className="text-xs" style={{ color: colors.accents.emberFlare }}>
              ${totalCost.toFixed(4)}
            </span>
          )}
          {firstStart && (
            <span className="text-xs text-omneval-text-muted">
              {formatTime(firstStart)}
              {lastEnd && lastEnd !== firstStart ? ` → ${formatTime(lastEnd)}` : ""}
            </span>
          )}
        </div>
      </div>

      {/* Toggle bar */}
      <div
        className="flex items-center gap-2 px-4 py-2 border-b"
        style={{ borderColor: colors.backgrounds.caveWall }}
      >
        <label className="flex items-center gap-2 text-xs text-omneval-text-muted cursor-pointer select-none">
          <input
            type="checkbox"
            checked={showFullHistory}
            onChange={(e) => setShowFullHistory(e.target.checked)}
            aria-label="Show full message history"
          />
          Show full message history
        </label>
      </div>

      {/* Chronological trace list */}
      <div className="flex-1 overflow-auto px-4 py-4">
        <div className="max-w-4xl mx-auto space-y-3">
          {traces.map((t) => (
            <TraceRow
              key={t.trace_id}
              trace={t}
              io={traceIO[t.trace_id] ?? {}}
              showFullHistory={showFullHistory}
              onClick={() => {
                onNavigateToTrace(t.trace_id);
                onNavigateToTraceDetail(t.trace_id);
              }}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

// ── Trace Row ──────────────────────────────────────────────────────

function TraceRow({
  trace,
  io,
  showFullHistory,
  onClick,
}: {
  trace: ConversationTraceItem;
  io: TraceIO;
  showFullHistory: boolean;
  onClick: () => void;
}) {
  const kindColor = KIND_COLOR_MAP[trace.root_span_kind] || colors.backgrounds.caveWall;
  const tokens = trace.input_tokens + trace.output_tokens;

  return (
    <div
      className="rounded-lg border overflow-hidden cursor-pointer transition-colors hover:bg-omneval-violet-hover"
      style={{
        backgroundColor: colors.backgrounds.charcoalDepth,
        borderColor: colors.backgrounds.caveWall,
        borderLeft: `3px solid ${kindColor}`,
      }}
      onClick={onClick}
      role="button"
      tabIndex={0}
      aria-label={`Open trace ${trace.root_span_name}`}
      onKeyDown={(e) => {
        if (e.key === "Enter") onClick();
      }}
    >
      {/* Metadata line */}
      <div className="flex items-center gap-3 px-3 py-2">
        <span
          className="text-xs px-2 py-0.5 rounded-full font-medium flex-shrink-0"
          style={{ background: `${kindColor}1A`, color: kindColor }}
        >
          {trace.root_span_kind}
        </span>
        <span className="text-xs font-mono text-omneval-text-pure truncate">
          {trace.root_span_name}
        </span>
        {trace.model && (
          <span className="text-xs text-omneval-text-muted font-mono truncate">{trace.model}</span>
        )}
        <div className="flex-1" />
        <span className="text-xs text-omneval-text-muted flex-shrink-0">
          {tokens.toLocaleString()}t
        </span>
        {trace.cost_usd > 0 && (
          <span className="text-xs flex-shrink-0" style={{ color: colors.accents.emberFlare }}>
            ${trace.cost_usd.toFixed(4)}
          </span>
        )}
        <span className="text-xs text-omneval-text-muted flex-shrink-0">
          {formatDuration(trace.start_time, trace.end_time || trace.start_time)}
        </span>
      </div>

      {/* Inline LLM input/output preview */}
      {(io.input || io.output) && (
        <div
          className="px-3 pb-3 space-y-1.5"
          style={{ borderTop: `1px solid ${colors.backgrounds.caveWall}` }}
        >
          <ChatPreview value={io.input} role="input" showFullHistory={showFullHistory} />
          <ChatPreview value={io.output} role="output" showFullHistory={showFullHistory} />
        </div>
      )}
    </div>
  );
}

// ── Compact chat bubble preview ────────────────────────────────────

function ChatPreview({
  value,
  role,
  showFullHistory,
}: {
  value?: string;
  role: "input" | "output";
  showFullHistory: boolean;
}) {
  if (!value) return null;
  let turns = parseChatTurns(value);
  if (!turns || turns.length === 0) {
    // Not a chat structure — render the raw value as a single turn.
    turns = [{ role: role === "input" ? "user" : "assistant", content: value }];
  }

  // Toggle off: only the LAST message of the input array is shown; the
  // assistant output renders fully in both modes.
  const visible = role === "input" && !showFullHistory ? turns.slice(-1) : turns;

  return (
    <div className="space-y-1 pt-1.5">
      {visible.map((turn, i) => {
        const isAssistant = turn.role === "assistant";
        return (
          <div key={i} className={`flex ${isAssistant ? "justify-end" : "justify-start"}`}>
            <div
              className="font-mono text-xs px-2.5 py-1.5 rounded-md max-w-[85%] whitespace-pre-wrap break-words"
              style={{
                backgroundColor: isAssistant
                  ? colors.flickerRgba(0.08)
                  : colors.backgrounds.slightIllumination,
                color: colors.typography.pureLight,
              }}
            >
              <span
                className="font-semibold uppercase tracking-wider mr-2"
                style={{ color: colors.typography.ashGrey, fontSize: "0.6rem" }}
              >
                {turn.role}
              </span>
              {turn.content.length > 600 ? `${turn.content.slice(0, 600)}…` : turn.content}
            </div>
          </div>
        );
      })}
    </div>
  );
}
