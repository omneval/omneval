import { useState, useEffect, useCallback } from "react";
import { colors } from "@/theme";
import { Skeleton } from "@/components/Skeleton";
import { EmptyState } from "@/components/EmptyState";
import {
  formatTimeWithYear,
  formatDuration,
  formatCost,
} from "@/utils/formatters";

// ── Types ──────────────────────────────────────────────────────────

interface ConversationListItem {
  conversation_id: string;
  service_name: string;
  trace_count: number;
  span_count: number;
  start_time: string;
  end_time: string;
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
}

interface ConversationListResponse {
  conversations: ConversationListItem[];
  next?: string;
}

interface ConversationsPageProps {
  activeProject: string;
  onNavigateToConversation: (conversationId: string) => void;
}

// ── Component ──────────────────────────────────────────────────────

export default function ConversationsPage({
  activeProject,
  onNavigateToConversation,
}: ConversationsPageProps) {
  const [conversations, setConversations] = useState<ConversationListItem[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loading, setLoading] = useState(false);

  const fetchConversations = useCallback(
    async (cursor: string, append = false) => {
      setLoading(true);
      try {
        const params = new URLSearchParams({
          project_id: activeProject,
          from: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString(),
          to: new Date().toISOString(),
          limit: "50",
        });
        if (cursor) params.set("cursor", cursor);

        let res: Response;
        try {
          res = await fetch(`/api/v1/conversations?${params.toString()}`);
        } catch {
          return;
        }
        if (res.ok) {
          const data: ConversationListResponse = await res.json();
          if (append) {
            setConversations((prev) => [...prev, ...(data.conversations ?? [])]);
          } else {
            setConversations(data.conversations ?? []);
          }
          setNextCursor(data.next ?? "");
        }
      } finally {
        setLoading(false);
      }
    },
    [activeProject],
  );

  useEffect(() => {
    fetchConversations("");
  }, [fetchConversations]);

  const headers = [
    "Conversation ID",
    "Service",
    "Traces",
    "Spans",
    "Started",
    "Duration",
    "Total Cost",
    "Tokens",
  ];

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="px-6 py-4 border-b" style={{ borderColor: colors.backgrounds.caveWall }}>
        <h1 className="text-xl font-semibold" style={{ color: colors.typography.pureLight }}>
          Conversations
        </h1>
        <p className="text-sm mt-1" style={{ color: colors.typography.ashGrey }}>
          Group agent sessions by conversation for structured analysis
        </p>
      </div>

      {/* Content */}
      <div className="flex-1 flex flex-col">
        {conversations.length === 0 && !loading ? (
          <EmptyState
            variant="default"
            title="No conversations yet"
            description="Group an agent session's traces by sending a conversation_id from the SDK — e.g. set_active_conversation_id() in Python, WithConversationID() in Go, or Omneval.setActiveConversationId() in TypeScript"
          />
        ) : loading && conversations.length === 0 ? (
          <div className="flex flex-col gap-2 p-4">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-6 rounded" style={{ width: "85%" }} />
            ))}
          </div>
        ) : (
          <>
            <div className="flex-1 overflow-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr
                    style={{
                      background: colors.backgrounds.slightIllumination,
                      borderBottom: `1px solid ${colors.backgrounds.caveWall}`,
                      color: colors.typography.ashGrey,
                    }}
                  >
                    {headers.map((h) => (
                      <th
                        key={h}
                        className="px-3 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap"
                      >
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {conversations.map((c) => (
                    <tr
                      key={c.conversation_id}
                      className="cursor-pointer transition-colors duration-150 hover:bg-omneval-violet-hover"
                      style={{ borderBottom: `1px solid ${colors.backgrounds.caveWall}` }}
                      onClick={() => onNavigateToConversation(c.conversation_id)}
                      role="button"
                      aria-label={`Open conversation ${c.conversation_id}`}
                    >
                      <td className="px-3 py-2.5 whitespace-nowrap">
                        <span className="font-mono text-xs text-omneval-violet-pale">
                          {c.conversation_id.slice(0, 12)}…
                        </span>
                      </td>
                      <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-pure text-xs">
                        {c.service_name || "—"}
                      </td>
                      <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-muted font-mono text-xs">
                        {c.trace_count}
                      </td>
                      <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-muted font-mono text-xs">
                        {c.span_count}
                      </td>
                      <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-muted text-xs">
                        {formatTimeWithYear(c.start_time)}
                      </td>
                      <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-muted text-xs">
                        {formatDuration(c.start_time, c.end_time || c.start_time)}
                      </td>
                      <td
                        className="px-3 py-2.5 whitespace-nowrap font-medium text-xs"
                        style={{ color: colors.accents.emberFlare }}
                      >
                        {formatCost(c.total_cost_usd)}
                      </td>
                      <td className="px-3 py-2.5 whitespace-nowrap text-omneval-text-muted font-mono text-xs">
                        {(c.total_input_tokens + c.total_output_tokens).toLocaleString()}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {/* Pagination */}
            <div
              className="flex items-center justify-end px-4 py-3 border-t"
              style={{ borderColor: colors.backgrounds.caveWall }}
            >
              <button
                disabled={loading || !nextCursor}
                onClick={() => fetchConversations(nextCursor, true)}
                className="text-sm px-4 py-1.5 rounded-md font-medium text-white transition-all duration-150 disabled:opacity-40 disabled:cursor-not-allowed hover:brightness-110 active:brightness-90"
                style={
                  nextCursor
                    ? {
                        background: colors.accents.emberFlare,
                        boxShadow: "0 2px 8px rgba(124, 58, 237, 0.3)",
                      }
                    : { background: colors.backgrounds.caveWall, boxShadow: "none" }
                }
              >
                {loading ? "Loading..." : nextCursor ? "Load Next Page" : "No more data"}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}