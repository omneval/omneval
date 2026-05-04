// Parallel Planner with Review — four-phase orchestration loop
//
// Phase 1 (Plan):             A Pi agent reads all open Sandcastle-labeled issues,
//                             builds a dependency graph, and outputs a <plan> JSON
//                             listing unblocked issues with branch names.
// Phase 2 (Execute + Review): For each issue, a sandbox is created via
//                             createSandbox(). The implementer runs first (100
//                             iterations, Ralph Loop / TDD). If it produces commits,
//                             a reviewer runs in the same sandbox on the same branch
//                             (1 iteration). All issue pipelines run concurrently
//                             via Promise.allSettled().
// Phase 3 (Merge):            A single agent merges all completed branches into the
//                             current branch, resolves conflicts, runs Go tests, and
//                             closes the corresponding GitHub issues.
//
// The outer loop repeats up to MAX_ITERATIONS times so that newly unblocked
// issues are picked up after each round of merges.
//
// Usage:
//   npx tsx .sandcastle/main.mts
// Or add to package.json:
//   "scripts": { "sandcastle": "npx tsx .sandcastle/main.mts" }

// Load host-side env vars from .sandcastle/.env (Discord webhook URL, etc.)
process.loadEnvFile(".sandcastle/.env");

import * as sandcastle from "@ai-hero/sandcastle";
import { docker } from "@ai-hero/sandcastle/sandboxes/docker";

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const MODEL = "unsloth/Qwen3.6-35B-A3B-GGUF:Q4_K_M";

// Maximum number of plan→execute→merge cycles before stopping.
const MAX_ITERATIONS = 10;

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
//   1. Copy Pi model config so the agent knows which local model to use.
//   2. Warm the Go module cache (no-op on first run if go.work doesn't exist yet).
//   3. Post a Discord notification naming the issue being started.
function issueHooks(issue: { id: string; title: string; branch: string }) {
  return {
    sandbox: {
      onSandboxReady: [
        {
          command:
            "mkdir -p ~/.pi/agent && cp .sandcastle/pi-models.json ~/.pi/agent/models.json",
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

// Hooks for the planner and merger — no Discord notification, no Go warm-up needed.
const plannerHooks = {
  sandbox: {
    onSandboxReady: [
      {
        command:
          "mkdir -p ~/.pi/agent && cp .sandcastle/pi-models.json ~/.pi/agent/models.json",
      },
    ],
  },
};

const mergerHooks = {
  sandbox: {
    onSandboxReady: [
      {
        command:
          "mkdir -p ~/.pi/agent && cp .sandcastle/pi-models.json ~/.pi/agent/models.json",
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

for (let iteration = 1; iteration <= MAX_ITERATIONS; iteration++) {
  console.log(`\n=== Iteration ${iteration}/${MAX_ITERATIONS} ===\n`);

  // -------------------------------------------------------------------------
  // Phase 1: Plan
  //
  // The planning agent reads open Sandcastle-labeled issues, builds a dependency
  // graph, and selects issues that can be worked in parallel right now.
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

  console.log(
    `Planning complete. ${issues.length} issue(s) to work in parallel:`,
  );
  for (const issue of issues) {
    console.log(`  #${issue.id}: ${issue.title} → ${issue.branch}`);
  }

  await notify(
    `📋 Iteration ${iteration}: starting ${issues.length} issue(s):\n` +
    issues.map((issue) => `• #${issue.id}: ${issue.title}`).join("\n"),
  );

  // -------------------------------------------------------------------------
  // Phase 2: Execute + Review
  //
  // For each issue, create a sandbox so the implementer and reviewer share the
  // same container per branch. The implementer runs first; if it produces
  // commits, the reviewer runs in the same sandbox.
  //
  // Promise.allSettled means one failing pipeline doesn't cancel the others.
  // Note: all agents call the same local llama-server, so they serialize at
  // the model level — "parallel" here means parallel Docker containers, not
  // parallel inference.
  // -------------------------------------------------------------------------
  const settled = await Promise.allSettled(
    issues.map(async (issue) => {
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
          const review = await sandbox.run({
            name: "reviewer",
            maxIterations: 1,
            agent,
            promptFile: "./.sandcastle/review-prompt.md",
            promptArgs: { BRANCH: issue.branch },
          });

          await notify(`✅ Completed issue #${issue.id}: ${issue.title}`);

          return {
            ...review,
            commits: [...implement.commits, ...review.commits],
          };
        }

        await notify(
          `⚠️ Issue #${issue.id}: ${issue.title} — agent produced no commits`,
        );
        return implement;
      } catch (err) {
        await notify(
          `❌ Failed issue #${issue.id}: ${issue.title}\n${err}`,
        );
        throw err;
      } finally {
        await sandbox.close();
      }
    }),
  );

  for (const [i, outcome] of settled.entries()) {
    if (outcome.status === "rejected") {
      console.error(
        `  ✗ #${issues[i]!.id} (${issues[i]!.branch}) failed: ${outcome.reason}`,
      );
    }
  }

  const completedIssues = settled
    .map((outcome, i) => ({ outcome, issue: issues[i]! }))
    .filter(
      (entry) =>
        entry.outcome.status === "fulfilled" &&
        entry.outcome.value.commits.length > 0,
    )
    .map((entry) => entry.issue);

  const completedBranches = completedIssues.map((issue) => issue.branch);

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
  // One agent merges all completed branches into the current branch, resolves
  // conflicts, runs Go tests, and closes the corresponding GitHub issues.
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
        .map((issue) => `- ${issue.id}: ${issue.title}`)
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
