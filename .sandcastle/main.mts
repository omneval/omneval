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
// Tracing: the pi-otel-telemetry extension inside each sandbox exports spans
// directly to Langfuse via OTLP (OTEL_EXPORTER_OTLP_ENDPOINT /
// OTEL_EXPORTER_OTLP_HEADERS set in .sandcastle/.env). No host-side proxy needed.
//
// Usage:
//   npx tsx .sandcastle/main.mts
// Or add to package.json:
//   "scripts": { "sandcastle": "npx tsx .sandcastle/main.mts" }

// Load host-side env vars from .sandcastle/.env (Discord webhook URL, OTEL config, etc.)
process.loadEnvFile(".sandcastle/.env");

import * as sandcastle from "@ai-hero/sandcastle";
import { docker } from "@ai-hero/sandcastle/sandboxes/docker";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface Issue {
  id: string;
  title: string;
  branch: string;
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const MODEL = "unsloth/Qwen3.6-35B-A3B-GGUF:Q4_K_M";

// Maximum number of plan→execute→merge cycles before stopping.
const MAX_ITERATIONS = 30;

const DISCORD_WEBHOOK_URL = process.env.DISCORD_WEBHOOK_URL ?? "";

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


// Install pi model config pointing directly at llama-server.
const INSTALL_MODELS_CMD = "mkdir -p ~/.pi/agent && cp .sandcastle/pi-models.json ~/.pi/agent/models.json";

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
//   1. Install pi model config (direct llama-server, no proxy).
//   2. Warm the Go module cache.
//   3. Post a Discord notification naming the issue being started.
function issueHooks(issue: { id: string; title: string; branch: string }) {
  return {
    sandbox: {
      onSandboxReady: [
        { command: INSTALL_MODELS_CMD },
        { command: "go work sync 2>/dev/null || true" },
        {
          // jq handles escaping so issue titles with special characters are safe.
          command: `[ -n "$DISCORD_WEBHOOK_URL" ] && jq -cn --arg c "🚀 Starting issue #${issue.id}: ${issue.title} → \`${issue.branch}\`" '{"content":$c}' | curl -s -o /dev/null -X POST "$DISCORD_WEBHOOK_URL" -H "Content-Type: application/json" -d @- || true`,
        },
      ],
    },
  };
}

const plannerHooks = {
  sandbox: {
    onSandboxReady: [
      { command: INSTALL_MODELS_CMD },
    ],
  },
};

const mergerHooks = {
  sandbox: {
    onSandboxReady: [
      { command: INSTALL_MODELS_CMD },
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

for (let iteration = 1; iteration <= MAX_ITERATIONS; iteration++) {
  console.log(`\n=== Iteration ${iteration}/${MAX_ITERATIONS} ===\n`);

  // -------------------------------------------------------------------------
  // Phase 1: Plan
  //
  // The planning agent reads open Sandcastle-labeled issues, builds a dependency
  // graph, and selects issues that can be worked on right now.
  // It outputs a <plan> JSON block — we parse that to drive Phase 2.
  // -------------------------------------------------------------------------
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

console.log("\nAll done.");
