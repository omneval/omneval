// Sequential Planner with Review — four-phase orchestration loop
//
// Phase 1 (Plan):             A Pi agent reads all open Sandcastle-labeled issues,
//                             builds a dependency graph, and outputs a <plan> JSON
//                             listing unblocked issues in priority order.
// Phase 2 (Execute + Review): The first unblocked issue is worked sequentially.
//                             The implementer runs first (100 iterations, Ralph Loop
//                             / TDD). If it produces commits, a reviewer runs in the
//                             same sandbox on the same branch (1 iteration).
//                             Only one issue runs at a time because the local
//                             llama-server handles one request at a time — parallel
//                             agents would just block each other and time out.
// Phase 3 (Merge):            A single agent merges the completed branch into the
//                             current branch, resolves conflicts, runs Go tests, and
//                             closes the corresponding GitHub issue.
//
// The outer loop repeats up to MAX_ITERATIONS times so that newly unblocked
// issues are picked up after each round of merges.
//
// Langfuse tracing:           When LANGFUSE_PUBLIC_KEY + LANGFUSE_SECRET_KEY are set,
//                             a local HTTP proxy intercepts every LLM call made by Pi
//                             agents and records them as Langfuse generations. Each
//                             issue maps to one Langfuse session; each phase
//                             (planner / implementer / reviewer / merger) is a trace.
//
// Usage:
//   npx tsx .sandcastle/main.mts
// Or add to package.json:
//   "scripts": { "sandcastle": "npx tsx .sandcastle/main.mts" }

// Load host-side env vars from .sandcastle/.env (Discord webhook URL, Langfuse keys, etc.)
process.loadEnvFile(".sandcastle/.env");

import * as sandcastle from "@ai-hero/sandcastle";
import { docker } from "@ai-hero/sandcastle/sandboxes/docker";
import * as http from "http";
import { Langfuse } from "langfuse";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface Issue {
  id: string;
  title: string;
  branch: string;
}

interface Plan {
  issues: Issue[];
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const MODEL = "unsloth/Qwen3.6-35B-A3B-GGUF:Q4_K_M";

// Maximum number of plan→execute→merge cycles before stopping.
const MAX_ITERATIONS = 10;

const DISCORD_WEBHOOK_URL = process.env.DISCORD_WEBHOOK_URL ?? "";

// Langfuse tracing — enabled when both keys are present.
const LANGFUSE_ENABLED = !!(process.env.LANGFUSE_SECRET_KEY && process.env.LANGFUSE_PUBLIC_KEY);
// Port the proxy listens on on the host; must not conflict with llama-server (8080).
const LANGFUSE_PROXY_PORT = parseInt(process.env.LANGFUSE_PROXY_PORT ?? "8081", 10);
// Where the real llama-server lives (host-side address, not Docker internal).
const LLAMA_SERVER_URL = process.env.LLAMA_SERVER_URL ?? "http://localhost:8080";

// Header names to strip when proxying requests and responses.
const PROXY_STRIP_HEADERS = new Set(["host", "content-length", "transfer-encoding"]);

// ---------------------------------------------------------------------------
// Pre-flight: verify llama-server is reachable before burning time on Docker
// ---------------------------------------------------------------------------

try {
  const response = await fetch("http://localhost:8080/v1/models");
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`);
  }
} catch (err) {
  console.error(
    "llama-server is not reachable at http://localhost:8080.\n" +
    "Start it with: llama-server --model unsloth/Qwen3.6-35B-A3B-GGUF:Q4_K_M\n" +
    `Error: ${err}`,
  );
  process.exit(1);
}

// ---------------------------------------------------------------------------
// Langfuse tracing setup
// ---------------------------------------------------------------------------

// Mutable trace pointer — updated before each sandbox.run() call.
// This is safe ONLY because the main loop is sequential (one issue at a time).
// If you ever re-introduce Promise.allSettled parallelism, refactor to per-sandbox
// context passing instead.
let activeTrace: ReturnType<Langfuse["trace"]> | null = null;

let lf: Langfuse | null = null;
let proxyServer: http.Server | null = null;

// ---------------------------------------------------------------------------
// Langfuse helpers
// ---------------------------------------------------------------------------

function createLangfuseTrace(
  langfuse: Langfuse,
  name: string,
  sessionId: string,
  metadata: Record<string, unknown>,
): void {
  activeTrace = langfuse.trace({ name, sessionId, metadata });
}

// ---------------------------------------------------------------------------
// HTTP proxy helpers
// ---------------------------------------------------------------------------

function readBody(req: http.IncomingMessage): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks)));
    req.on("error", reject);
  });
}

function stripHeaders(headers: http.IncomingHttpHeaders): Record<string, string> {
  const result: Record<string, string> = {};
  for (const [k, v] of Object.entries(headers)) {
    if (PROXY_STRIP_HEADERS.has(k.toLowerCase())) continue;
    result[k] = Array.isArray(v) ? v.join(", ") : (v ?? "");
  }
  return result;
}

function parseCompletionRequest(body: Buffer): {
  model?: string;
  messages?: unknown[];
  temperature?: number;
  max_tokens?: number;
  stream?: boolean;
  stream_options?: { include_usage?: boolean };
} | null {
  try {
    return JSON.parse(body.toString());
  } catch {
    return null;
  }
}

function startLangfuseProxy(langfuse: Langfuse, port: number, upstreamBase: string): http.Server {
  const server = http.createServer(async (req, res) => {
    const bodyBuf = await readBody(req);
    const url = req.url ?? "/";
    const isCompletions = url.includes("/completions");

    // Parse request body for completions requests (non-completions pass through as-is).
    const requestData = isCompletions && bodyBuf.length > 0
      ? parseCompletionRequest(bodyBuf)
      : null;

    // Inject stream_options.include_usage so token counts appear in streaming responses.
    let forwardBody: Buffer = bodyBuf;
    if (requestData?.stream) {
      const patched = {
        ...requestData,
        stream_options: { ...(requestData.stream_options ?? {}), include_usage: true },
      };
      forwardBody = Buffer.from(JSON.stringify(patched));
    }

    // Create a Langfuse generation span before forwarding the request.
    let generation: ReturnType<ReturnType<Langfuse["trace"]>["generation"]> | null = null;
    if (activeTrace && requestData) {
      generation = activeTrace.generation({
        name: "completion",
        model: requestData.model,
        input: requestData.messages,
        modelParameters: {
          temperature: requestData.temperature,
          maxTokens: requestData.max_tokens,
        },
      });
    }

    // Forward headers, stripping hop-by-hop and host.
    const forwardHeaders = stripHeaders(req.headers);

    // Forward request to the upstream llama-server.
    let upstream: Response;
    try {
      upstream = await fetch(`${upstreamBase}${url}`, {
        method: req.method ?? "GET",
        headers: forwardHeaders,
        body: forwardBody.length > 0 ? forwardBody : undefined,
      });
    } catch (err) {
      generation?.end({ level: "ERROR", statusMessage: String(err) });
      res.writeHead(502, { "Content-Type": "text/plain" });
      res.end(`Upstream error: ${err}`);
      return;
    }

    // Relay response headers, stripping hop-by-hop.
    const outHeaders = stripHeaders(upstream.headers as http.IncomingHttpHeaders);
    res.writeHead(upstream.status, outHeaders);

    const isStream = requestData?.stream === true;

    if (isStream && upstream.body) {
      const sseChunks = await relaySseStream(upstream.body, res, generation);
      res.end();

      // Parse accumulated SSE output for Langfuse.
      if (generation) {
        const raw = sseChunks.join("");
        const { outputText, inputTokens, outputTokens } = parseSseOutput(raw);
        generation.end({
          output: outputText,
          usage: { input: inputTokens, output: outputTokens },
        });
      }
    } else {
      const buf = Buffer.from(await upstream.arrayBuffer());
      res.end(buf);
      await recordNonStreamingGeneration(generation, buf);
    }
  });

  server.listen(port, "0.0.0.0", () => {
    console.log(`[Langfuse] Proxy listening on 0.0.0.0:${port} → ${upstreamBase}`);
  });

  return server;
}

// Relay an SSE (Server-Sent Events) stream to the HTTP response.
// Returns the accumulated text for Langfuse parsing.
async function relaySseStream(
  body: ReadableStream<Uint8Array>,
  res: http.ServerResponse,
  generation: ReturnType<ReturnType<Langfuse["trace"]>["generation"]> | null,
): Promise<string[]> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  const sseChunks: string[] = [];

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    // Write raw bytes to the client immediately for low-latency streaming.
    res.write(value);
    // Accumulate text for Langfuse generation parsing.
    sseChunks.push(decoder.decode(value, { stream: true }));
  }

  return sseChunks;
}

interface SseOutput {
  outputText: string;
  inputTokens: number;
  outputTokens: number;
}

function parseSseOutput(rawSse: string): SseOutput {
  let outputText = "";
  let inputTokens = 0;
  let outputTokens = 0;

  for (const line of rawSse.split("\n")) {
    if (!line.startsWith("data: ") || line === "data: [DONE]") continue;
    try {
      const d = JSON.parse(line.slice(6)) as {
        choices?: Array<{ delta?: { content?: string } }>;
        usage?: { prompt_tokens?: number; completion_tokens?: number };
      };
      if (d.choices?.[0]?.delta?.content) outputText += d.choices[0].delta.content;
      if (d.usage) {
        inputTokens = d.usage.prompt_tokens ?? 0;
        outputTokens = d.usage.completion_tokens ?? 0;
      }
    } catch {
      // Malformed SSE chunk — skip.
    }
  }

  return { outputText, inputTokens, outputTokens };
}

// Parse a non-streaming response and record it as a Langfuse generation.
async function recordNonStreamingGeneration(
  generation: ReturnType<ReturnType<Langfuse["trace"]>["generation"]> | null,
  responseBuf: Buffer,
): Promise<void> {
  if (!generation) return;

  try {
    const d = JSON.parse(responseBuf.toString()) as {
      choices?: Array<{ message?: { content?: string } }>;
      usage?: { prompt_tokens?: number; completion_tokens?: number };
    };
    generation.end({
      output: d.choices?.[0]?.message?.content ?? "",
      usage: {
        input: d.usage?.prompt_tokens ?? 0,
        output: d.usage?.completion_tokens ?? 0,
      },
    });
  } catch {
    generation.end({ output: "" });
  }
}

if (LANGFUSE_ENABLED) {
  lf = new Langfuse({
    secretKey: process.env.LANGFUSE_SECRET_KEY!,
    publicKey: process.env.LANGFUSE_PUBLIC_KEY!,
    baseUrl: process.env.LANGFUSE_HOST ?? "https://cloud.langfuse.com",
  });
  proxyServer = startLangfuseProxy(lf, LANGFUSE_PROXY_PORT, LLAMA_SERVER_URL);
} else {
  console.log("[Langfuse] Tracing disabled (LANGFUSE_PUBLIC_KEY / LANGFUSE_SECRET_KEY not set).");
}

// Command to install the correct pi models config into ~/.pi/agent/models.json.
// When Langfuse is enabled, the JSON is inlined via printf so it works in any
// git worktree (gitignored files aren't present in worktree checkouts).
// When disabled, we cp the committed pi-models.json which exists in all worktrees.
const INSTALL_MODELS_CMD: string = (() => {
  if (!LANGFUSE_ENABLED) {
    return "mkdir -p ~/.pi/agent && cp .sandcastle/pi-models.json ~/.pi/agent/models.json";
  }
  const proxyModels = JSON.stringify({
    providers: {
      local: {
        baseUrl: `http://host.docker.internal:${LANGFUSE_PROXY_PORT}/v1`,
        api: "openai-completions",
        apiKey: "not-needed",
        models: [{ id: MODEL }],
      },
    },
  });
  // Single-quote the JSON so the shell doesn't interpret its double quotes.
  // Safe because the JSON content contains no single quotes.
  return `mkdir -p ~/.pi/agent && printf '%s' '${proxyModels}' > ~/.pi/agent/models.json`;
})();

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

async function notify(message: string): Promise<void> {
  if (!DISCORD_WEBHOOK_URL) return;
  await fetch(DISCORD_WEBHOOK_URL, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ content: message }),
  }).catch(() => {});
}

// Builds per-issue sandbox hooks:
//   1. Install pi model config (proxy-routed when Langfuse is enabled).
//   2. Warm the Go module cache.
//   3. Post a Discord notification naming the issue being started.
function issueHooks(issue: { id: string; title: string; branch: string }) {
  return {
    sandbox: {
      onSandboxReady: [
        {
          command:
            INSTALL_MODELS_CMD,
        },
        { command: "go work sync 2>/dev/null || true" },
        {
          // jq handles escaping so issue titles with special characters are safe.
          command: `[ -n "$DISCORD_WEBHOOK_URL" ] && jq -cn --arg c "🚀 Starting issue #${issue.id}: ${issue.title} → \`${issue.branch}\`" '{"content":$c}' | curl -s -o /dev/null -X POST "$DISCORD_WEBHOOK_URL" -H "Content-Type: application/json" -d @- || true`,
        },
      ],
    },
  };
}

// Hooks for the planner and merger — also routed through the proxy when Langfuse is enabled.
const plannerHooks = {
  sandbox: {
    onSandboxReady: [
      {
        command:
          INSTALL_MODELS_CMD,
      },
    ],
  },
};

const mergerHooks = {
  sandbox: {
    onSandboxReady: [
      {
        command:
          INSTALL_MODELS_CMD,
      },
      { command: "go work sync 2>/dev/null || true" },
    ],
  },
};

const agent = sandcastle.pi(MODEL);

// ---------------------------------------------------------------------------
// Main loop
// ---------------------------------------------------------------------------

let consecutivePlannerFailures = 0;
const MAX_CONSECUTIVE_PLANNER_FAILURES = 3;

try {
  for (let iteration = 1; iteration <= MAX_ITERATIONS; iteration++) {
    console.log(`\n=== Iteration ${iteration}/${MAX_ITERATIONS} ===\n`);

    // -------------------------------------------------------------------------
    // Phase 1: Plan
    //
    // The planning agent reads open Sandcastle-labeled issues, builds a dependency
    // graph, and selects issues that can be worked on right now.
    // It outputs a <plan> JSON block — we parse that to drive Phase 2.
    // -------------------------------------------------------------------------
    if (lf) {
      createLangfuseTrace(lf, "planner", `iteration-${iteration}`, { iteration });
    }

    const plan = await sandcastle.run({
      hooks: plannerHooks,
      sandbox: docker(),
      name: "planner",
      maxIterations: 1,
      agent,
      promptFile: "./.sandcastle/plan-prompt.md",
    });

    const planMatch = plan.stdout.match(/<plan>([\s\S]*?)<\/plan>/);
    if (!planMatch) {
      consecutivePlannerFailures++;
      console.warn(
        `[iteration ${iteration}] Planner did not produce a <plan> tag ` +
        `(${consecutivePlannerFailures}/${MAX_CONSECUTIVE_PLANNER_FAILURES} consecutive failures).\n` +
        plan.stdout.slice(-1000),
      );
      if (consecutivePlannerFailures >= MAX_CONSECUTIVE_PLANNER_FAILURES) {
        throw new Error(
          `Planner failed ${MAX_CONSECUTIVE_PLANNER_FAILURES} consecutive times without a <plan> tag. Aborting.`,
        );
      }
      continue;
    }
    consecutivePlannerFailures = 0;

    const { issues } = JSON.parse(planMatch[1]!) as {
      issues: { id: string; title: string; branch: string }[];
    };

    if (issues.length === 0) {
      console.log("No unblocked issues to work on. Exiting.");
      break;
    }

    // Only work on the first unblocked issue. The local llama-server handles
    // one request at a time, so running multiple agents in parallel just causes
    // them to block on inference and time out without producing commits.
    const issue = issues[0]!;

    console.log(
      `Planning complete. Working on 1 issue (${issues.length} unblocked total):`,
    );
    console.log(`  #${issue.id}: ${issue.title} → ${issue.branch}`);

    await notify(
      `📋 Iteration ${iteration}: starting #${issue.id}: ${issue.title}`,
    );

    // -------------------------------------------------------------------------
    // Phase 2: Execute + Review
    //
    // Create a sandbox for the single issue. The implementer runs first; if it
    // produces commits, the reviewer runs in the same sandbox.
    // -------------------------------------------------------------------------
    let completedIssues: Issue[] = [];

    const sandbox = await sandcastle.createSandbox({
      branch: issue.branch,
      sandbox: docker(),
      hooks: issueHooks(issue),
    });

    try {
      if (lf) {
        createLangfuseTrace(lf, "implementer", `issue-${issue.id}`, {
          issueId: issue.id,
          issueTitle: issue.title,
          branch: issue.branch,
        });
      }

      const implement = await sandbox.run({
        name: "implementer",
        maxIterations: 100,
        agent,
        promptFile: "./.sandcastle/implement-prompt.md",
        promptArgs: {
          TASK_ID: issue.id,
          ISSUE_TITLE: issue.title,
          BRANCH: issue.branch,
        },
      });

      if (implement.commits.length > 0) {
        if (lf) {
          createLangfuseTrace(lf, "reviewer", `issue-${issue.id}`, {
            issueId: issue.id,
            issueTitle: issue.title,
            branch: issue.branch,
          });
        }

        await sandbox.run({
          name: "reviewer",
          maxIterations: 1,
          agent,
          promptFile: "./.sandcastle/review-prompt.md",
          promptArgs: { BRANCH: issue.branch },
        });

        await notify(`✅ Completed issue #${issue.id}: ${issue.title}`);
        completedIssues = [issue];
      } else {
        await notify(
          `⚠️ Issue #${issue.id}: ${issue.title} — agent produced no commits`,
        );
      }
    } catch (err) {
      await notify(
        `❌ Failed issue #${issue.id}: ${issue.title}\n${err}`,
      );
      console.error(`  ✗ #${issue.id} (${issue.branch}) failed: ${err}`);
    } finally {
      await sandbox.close();
    }

    const completedBranches = completedIssues.map((i) => i.branch);

    console.log(
      `\nExecution complete. ${completedBranches.length} branch(es) with commits:`,
    );
    for (const branch of completedBranches) {
      console.log(`  ${branch}`);
    }

    if (completedBranches.length === 0) {
      console.log("No commits produced. Nothing to merge.");
      continue;
    }

    // -------------------------------------------------------------------------
    // Phase 3: Merge
    //
    // One agent merges the completed branch into the current branch, resolves
    // conflicts, runs Go tests, and closes the corresponding GitHub issue.
    // -------------------------------------------------------------------------
    if (lf) {
      createLangfuseTrace(lf, "merger", `iteration-${iteration}`, {
        iteration,
        branches: completedBranches,
        issues: completedIssues.map((i) => i.id),
      });
    }

    await sandcastle.run({
      hooks: mergerHooks,
      sandbox: docker(),
      name: "merger",
      maxIterations: 1,
      agent,
      promptFile: "./.sandcastle/merge-prompt.md",
      promptArgs: {
        BRANCHES: completedBranches.map((branch) => `- ${branch}`).join("\n"),
        ISSUES: completedIssues
          .map((i) => `- ${i.id}: ${i.title}`)
          .join("\n"),
      },
    });

    await notify(
      `🔀 Merged ${completedBranches.length} branch(es) into main:\n` +
      completedBranches.map((branch) => `• ${branch}`).join("\n"),
    );

    console.log("\nBranches merged.");
  }
} finally {
  // Flush all pending Langfuse events before the process exits.
  activeTrace = null;
  await lf?.shutdownAsync();
  proxyServer?.close();
}

console.log("\nAll done.");
