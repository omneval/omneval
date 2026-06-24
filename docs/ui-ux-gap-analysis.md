# UI/UX Gap Analysis — Omneval vs. Langfuse / LangSmith / Helicone

Live audit of https://omneval.blosshomelab.com (project `omneval`, 2026-06-22) against documented
behavior of the three leading LLM observability products. Goal: a concrete punch list, not a
redesign brief.

## Status as of 2026-06-23 (re-verified live)

10 of the 12 punch-list items below shipped within a day (tracked under PRD #232, issues
#235-#246). Re-verified directly on the live instance — see the original sections below for what
each one looked like before; striking through what's now fixed in place rather than rewriting it.

**Shipped and confirmed live:**
- Span Input/Output formatted chat rendering (#235) — Trace Detail panel now shows SYSTEM/USER
  messages distinctly, raw Attributes kept as a secondary section.
- Traces list default Latency/Tokens/Cost columns + denser rows (#236).
- Levels badge tooltips (#237).
- Bulk add-to-dataset from Traces list (#238) — checkbox-select → "Add to dataset (N)" button.
- Dashboard KPI tiles + latency-percentile + error-rate charts, duplicate Cost-by-Model panel
  removed (#239).
- Conversations promoted to top-level nav (#240).
- Chat-type (multi-message) Prompt editor (#241, #270).
- Eval Rule recursive AND/OR/NOT filter-group builder (#243) — "+ Add OR/AND Group" confirmed.
- Eval Rule Judge Prompt picker replacing freeform text (#244).
- Admin Ops view scaffold + Ingest Queue depth metric (#245).
- `ui/BRANDING.md` rewritten for the purple palette (#246).
- New Prompt form: model dropdown instead of freeform text (#242) — present, but see new bug below.

**Still open — confirmed still broken/missing on the live instance:**
- **#233 (P0)** — Cost still reads `$0.0000` everywhere (Dashboard KPI tile and Traces list Cost
  column both confirmed today) and the same model is still split into `unknown` /
  `openai/nvidia/Qwen3.6-...` / `nvidia/Qwen3.6-...` rows on the Dashboard's "Traces by Model"
  chart. Highest-priority remaining item.
- **#234 (P0)** — Admin → API Keys still reports "No API keys found" for the `omneval` project.
- **#247 / #248** — Admin Ops view still shows only Ingest Queue depth; Ingest Buffer
  reconciliation sweep status and Quack Server Table Maintenance status are not yet surfaced.

**New finding (not in the original audit):** the model dropdown shipped for #242 calls
`GET /api/v1/models`, which returns **502** on the live instance — the New Prompt form shows a red
"Failed to fetch models" message and falls back to an "Other..." option. The feature is wired up
correctly client-side; the backend endpoint itself is failing. Not yet filed as an issue.

## Method

- Browsed every top-level page on the live instance (Dashboard, Traces, Trace Detail, Conversations,
  Prompts, Eval Rules, Datasets, Settings, Admin) and exercised the "create" flows for Prompts,
  Eval Rules, and Datasets.
- Researched Langfuse, LangSmith, and Helicone's public docs/changelogs for their navigation, trace
  view, datasets/experiments, evals, prompt management, and dashboard patterns.

## Competitor pattern summary

| Area | Langfuse | LangSmith | Helicone |
|---|---|---|---|
| Sessions/Threads | First-class top-level nav item; session replay stitches traces together | First-class nav item; Threads group by `thread_id` | First-class nav item; Chat/Tree/Span view toggle |
| Trace view | Nested observations, cost+tokens rolled up at trace/span level, color-coded by type | Tree view with rollups at 3 levels (trace/parent/child run), reasoning-token breakdown | Three explicit view modes (chat/tree/span) |
| Span detail | Clean Input/Output panel (formatted chat messages) separate from raw metadata | Same — input/output rendered, not raw OTEL dump | Same |
| Dataset creation | From CSV/API **and** select-rows-from-trace-table → "Add to dataset" | Same — examples can come from production runs | Same — checkbox-select requests → "Add to Dataset" |
| Dataset comparison | Multi-run "Compare" view, one row per item, diffs side-by-side | Comparison view with red/green diff highlighting per feedback key | N/A (experiments feature deprecated 2025) |
| Prompts | Text **and** chat-message prompt types, versioned, environment labels, linked to traces, Playground integration | Prompt Hub, versioned/tagged, "Test over dataset" from Playground | Versioned, typed `{{var}}` templates, prompt partials for reuse |
| Evals | Score object (numeric/categorical/boolean/text) + LLM-as-judge + annotation queues (human review) | Code evals, LLM-judge, pairwise evals, annotation queues with A/B/Equal hotkeys | Mostly external score POST-back; in-app evaluator builder is thinner |
| Dashboard | Curated starter dashboards + drag-and-drop custom widget builder | Cost/latency-percentile/error-rate widgets + Alerts (threshold rules → Slack/PagerDuty/webhook) | KPI tiles, latency-quantile chart, cache-savings tile, weekly email reports |
| Filters | Filter sidebar shows value counts, saved views, column picker | Structured trace query DSL (`eq`, `gt`, `in`, `and`/`or`) | Cross-page persistent + URL-shareable filters |

## Live omneval findings

### Dashboard
- No top-of-page KPI tiles (total cost, total traces, error rate, avg latency) — the page goes
  straight into charts. Every competitor leads with scannable summary numbers.
- **Cost is broken end-to-end**: every "Total Cost" cell reads `$0.0000`, and the same physical model
  is split across three rows (`unknown`, `openai/nvidia/Qwen3.6-35B-A3B-NVFP4`,
  `nvidia/Qwen3.6-35B-A3B-NVFP4`) — the provider-prefix double-counting bug. This is the single most
  visible defect on the page; cost is the first number every competitor highlights.
- "Cost by Model" (top) and "Token Usage → Cost by Model" tab (bottom) show the same data twice —
  redundant panel.
- No latency-percentile (p50/p95/p99) or error-rate chart anywhere on the dashboard, despite both
  being headline metrics on every competitor's dashboard.
- "Eval Scores" empty state uses a star/favorite icon, which reads as "rating" rather than "no data."

### Traces list
- Default columns are Timestamp/Name/Input/Output only. Latency, Tokens, Cost, and Levels are
  enabled in the column picker but pushed off-screen — you must scroll horizontally to see any of
  them. Competitors show cost/latency/tokens inline by default.
- Row height is tall relative to the single line of content shown, hurting density versus
  Helicone's compact-row redesign.
- "Levels" column shows unlabeled badges (`AGE 23`, `INT 3`, `LLM 23`, `TOO 29`) with no tooltip —
  unclear what these abbreviate without reading source.
- Filters panel is a permanently-visible full-height sidebar with no collapse toggle, eating ~270px
  of width on top of the horizontal-scroll problem above.
- "Conversations" exists only as a tab next to Traces, not a top-level nav item — all three
  competitors treat session/thread grouping as first-class IA, not a sub-tab.
- Conversations tab empty state is genuinely good: explains exactly which SDK call sets
  `conversation_id` per language. Worth keeping as the model for other empty states.

### Trace detail
- Waterfall view: bars render correctly once loaded, color varies by span kind, hierarchy depth is
  visible. Reasonable parity with competitors here.
- Tree view (better than waterfall for this data): clean indentation, expand/collapse, duration per
  node — but the per-node count column always shows token count only (`0t` for wrapper spans) and
  never a cost rollup. LangSmith rolls up both tokens and cost at every level of the tree.
- **Span detail panel is the biggest gap.** Clicking a span shows only a raw "Attributes" JSON blob —
  no rendered Input/Output section with the actual prompt/completion messages. Buried in that blob,
  keys are double-quote-escaped (`""gen_ai.response.id""` instead of `"gen_ai.response.id"`) and
  nested JSON strings (tool definitions) are shown unparsed — effectively unreadable for anyone
  trying to inspect what was actually sent to the model. Every competitor renders input/output as a
  distinct, formatted section (chat bubbles or pretty-printed JSON) separate from raw metadata.
- No cost shown in the span panel at all (only tokens), consistent with the dashboard cost bug.

### Prompts
- "New Prompt" only supports a single flat text template (`{{variable}}` syntax) — no chat/message-
  array prompt type (system/user/assistant roles), which is how most production LLM calls are
  actually structured. Langfuse/LangSmith both support chat-type prompts.
- No environment/label field (e.g., "production") at creation, no Playground link, no tags.
- Model field is freeform text with a `gpt-4` placeholder — no dropdown of supported/known models, so
  typos aren't caught and there's no model-specific defaults.
- Empty state ("Create your first prompt to get started") gives no explanation of what prompts are
  for or how they connect to eval rules/playground, unlike the Conversations empty state.

### Eval Rules
- The rule builder is solid: named rule → judge model/prompt/version → sample rate → AND-only filter
  conditions → "Preview Matching Spans" before saving. The preview-before-create step is a good
  pattern competitors don't always have.
- "Judge Prompt Name" is freeform text, not a picker into the Prompt Registry — there's no
  validation that the referenced prompt/version actually exists.
- Filter conditions are AND-only (explicitly labeled in the UI) — no OR/grouping, unlike Langfuse's
  filter sidebar or LangSmith's `and`/`or` query DSL.
- No human-in-the-loop annotation queue concept anywhere in the product — Langfuse and LangSmith
  both treat human review queues as a first-class eval method alongside LLM-as-judge.

### Datasets
- Creation is CSV/JSON upload only. There is no "select traces → Add to dataset" action anywhere on
  the Traces page — every competitor lets you curate a dataset directly from production traffic,
  which is the main way real eval sets get built. Requiring an external export/upload step first is
  significant friction.
- Empty state design is inconsistent with Prompts/Eval Rules (no centered CTA button, just inline
  text) — minor, but noticeable once you've seen the other two.

### Settings / Admin
- Settings → API Keys: auto-generated key names (`Project Key (2026-05-30, ...r79g)`) aren't
  editable/renameable, and there's no "last used" timestamp — Langfuse/Helicone show both for key
  hygiene.
- **Bug, not just UX**: Admin → API Keys reports "No API keys found (0 total)" while Settings → API
  Keys for the same project shows 4 active keys. The two views appear to read from different
  sources. Worth a ticket — an admin trying to audit/revoke keys org-wide cannot actually see them.
- Admin is exclusively destructive controls (delete keys, delete projects, delete all traces) with no
  operational visibility — no ingest queue depth, no S3/Ingest-Buffer sync status, no Quack Server
  Table Maintenance (compaction/retention GC) status. Given omneval's whole pitch is self-hosting
  without a ClickHouse cluster, homelab operators are exactly the audience that needs this kind of
  infra health view, and right now there's nowhere in the UI to get it.

### Branding docs are stale
`ui/BRANDING.md` documents an orange "Ember/Cave" palette, but the current direction is the purple
theme already live on every page — the doc itself is outdated, not the UI. `ui/BRANDING.md` should
be rewritten to describe the purple palette actually in use so it stays a reliable reference.

## Prioritized recommendations

**P0 — correctness bugs that undermine trust in the data**
1. Fix the cost-integrity defect (provider-prefix double counting → `$0.0000` everywhere). Already
   tracked as the Phase 3 "Cost integrity" roadmap item — this audit confirms it's still live and is
   the most visible thing a new user would notice.
2. Fix Admin → API Keys returning 0 results when keys clearly exist (Settings page shows 4 for the
   same project).

**P1 — biggest UX gaps vs. competitors, highest leverage**
3. Span detail panel: render Input/Output as formatted chat messages, fix the double-escaped JSON
   keys, stop dumping raw OTEL attributes as the only view.
4. Traces list: give Latency/Tokens/Cost default visibility without horizontal scroll; reduce row
   height.
5. Datasets: add "select traces → Add to dataset" from the Traces page.
6. Dashboard: add top-of-page KPI tiles (cost, traces, error rate, avg latency) and a latency-
   percentile chart; remove the duplicate cost-by-model panel.

**P2 — polish and parity**
7. Promote Conversations to a top-level nav item.
8. Add chat-type (multi-message) prompts, model dropdown, and environment labels to Prompt Registry.
9. Add OR/grouped filter logic to Eval Rules; make "Judge Prompt Name" a picker into existing
   prompts.
10. Tooltip or relabel the cryptic `AGE/INT/LLM/TOO` level badges in the Traces table.
11. Rewrite `ui/BRANDING.md` to document the purple palette actually in use (it currently describes
    an abandoned orange "Ember/Cave" direction).
12. Add an Admin operational-health view (ingest queue depth, Ingest Buffer sync, Quack Server
    table-maintenance status) — distinct from the existing destructive-only Admin controls.
