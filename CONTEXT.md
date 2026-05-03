# Lantern — Bounded Context

## Terms

### Ralph Loop
A method for orchestrating "simpleton" agents in a tight feedback cycle: the agent attempts a task, checks its own output (tests, compilation), and repeats until done or the iteration limit is reached. Commits produced by a Ralph Loop are prefixed `RALPH:` in the commit message so they are identifiable in git history.

### Sandcastle Label
The GitHub issue label (`Sandcastle`) that marks an issue as eligible for autonomous agent pickup. Applied manually by the developer when the issue is considered ready for agent work. Removed implicitly when the issue is closed after a successful merge.

### Sandcastle Orchestration
The multi-phase automation loop driven by `npx tsx .sandcastle/main.mts`:
1. **Plan** — a Pi agent reads all open `Sandcastle`-labeled issues, builds a dependency graph, and selects unblocked issues for parallel execution.
2. **Execute** — one Pi agent per issue implements the work in an isolated Docker sandbox using the Ralph Loop (TDD: Red → Green → Refactor).
3. **Review** — a second Pi agent with a fresh context window reviews the same branch for correctness, conciseness, and quality.
4. **Merge** — a single Pi agent merges all completed branches into `main`, resolves conflicts, runs tests, and closes the corresponding GitHub issues.

### Pi Agent
The coding agent CLI (`@mariozechner/pi-coding-agent`) used as the agent runtime inside Docker sandboxes. Invoked by sandcastle via `pi -p --mode json --no-session --model <id>`. Does not support session capture or resume — each run is stateless.

### Local Model
The qwen3.6-35b-a3b model (`unsloth/Qwen3.6-35B-A3B-GGUF:Q4_K_M`) served locally via `llama-server` on `http://localhost:8080`. Pi agents inside Docker reach it at `http://host.docker.internal:8080/v1`. Configured via `~/.pi/agent/models.json` inside the container (copied in by the `onSandboxReady` hook).

**Critical constraint:** The model ID string must be identical in `.sandcastle/pi-models.json` (the `"id"` field) and in every `sandcastle.pi(MODEL)` call in `main.mts`. A mismatch causes Pi to fail silently with an unknown model error. The canonical value is `unsloth/Qwen3.6-35B-A3B-GGUF:Q4_K_M`.
