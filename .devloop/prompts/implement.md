# TASK

Fix issue {{TASK_ID}}: {{ISSUE_TITLE}}

Work on branch {{BRANCH}}. Make commits and run tests.

Only work on the issue specified.

{{FEEDBACK}}

# THE ISSUE (FULL TEXT)

{{ISSUE_BODY}}

If the text above is empty, pull the issue with `gh issue view {{TASK_ID}}`. If it references a parent PRD, pull that in too.

# SCOPE — READ THIS BEFORE WRITING ANY CODE

Every requirement and acceptance criterion in the issue is in scope, **regardless of language or layer**. omneval is NOT a Go-only repository — it is a multi-ecosystem monorepo:

| Layer | Location | Language | Test command |
|-------|----------|----------|--------------|
| Backend services | `services/{ingest,query,writer,eval}/`, `internal/` | Go (`go.work` workspace) | `go build ./... && go vet ./... && go test ./...` |
| Web UI | `ui/` | TypeScript + React (Vite) | `cd ui && npm install && npm test && npm run build` |
| Go SDK | `sdk/go/` | Go (part of `go.work`) | covered by `go test ./...` |
| Python SDK | `sdk/python/` | Python (uv) | `cd sdk/python && uv sync && uv run pytest` |
| TypeScript SDK | `sdk/ts/` | TypeScript | `cd sdk/ts && npm install && npm test` |

If the issue lists backend, SDK, and UI work, you implement the backend, the SDKs, AND the UI. You are NOT allowed to declare any part of the issue "out of scope", "follow-up work", or "outside Go scope". The only acceptable reason to leave a criterion unimplemented is a hard blocker you cannot resolve, and then you must say so explicitly via a `QUESTION:` line or in your final summary.

First action: write the issue's acceptance criteria into a checklist using the task tracker, one entry per criterion. Work through them all.

# CONTEXT

Review recent history to understand current conventions:

```
git log -n 10 --format="%H%n%ad%n%B---" --date=short
```

omneval is a Go workspace (`go.work` at the repo root, one `go.mod` per service, shared types in `internal/`) plus the UI and SDK ecosystems in the table above. Follow the standards in `.devloop/CODING_STANDARDS.md`. Match API response shapes, field names, and attribute names to the issue text **exactly** — do not rename or simplify them.

# EXPLORATION

Explore the repo and fill your context window with relevant information that will allow you to complete the task.

Pay extra attention to test files that touch the relevant parts of the code — in every ecosystem the issue touches (`*_test.go`, `ui/src/**/*.test.tsx`, `sdk/python/tests/`, `sdk/ts/tests/`).

# EXECUTION

This issue involves writing code, so drive it test-first. Invoke the `tdd` skill (`invoke_skill('tdd')`) and follow its red-green-refactor loop — one failing test, then the minimum code to pass it, repeat; never write all the tests up front.

Use the right test command for the layer you are changing (see the SCOPE table).

1. RED: write one failing test and confirm it fails
2. GREEN: write the minimum implementation to pass that test
3. REPEAT until all acceptance criteria are covered
4. REFACTOR: clean up without breaking tests

If you genuinely cannot proceed without a human decision, emit a single line starting with `QUESTION:` followed by the question, then stop and wait.

# FEEDBACK LOOPS

Before committing, run the checks for every layer you touched and confirm they all pass with no errors:

```
# Go (services, internal, sdk/go):
go build ./... && go vet ./... && go test ./...

# UI:
cd ui && npm test && npm run build

# Python SDK:
cd sdk/python && uv run pytest

# TypeScript SDK:
cd sdk/ts && npm test
```

# COMMIT

Make a git commit. The commit message must:

1. Start with `RALPH:` prefix
2. Include task completed + issue reference (e.g. `fixes #{{TASK_ID}}`)
3. Key decisions made
4. Files changed
5. Blockers or notes for next iteration

Keep it concise.

# BEFORE YOU DECLARE COMPLETE

Re-read the acceptance criteria in the issue text above, one by one. For each, verify the implementation exists in your diff (`git diff main...HEAD`) — not in a comment, not in a plan, in working code with tests. If any criterion is unmet, go back to EXECUTION.

Then end your final message with a checklist of every acceptance criterion marked ✅ implemented or ❌ not implemented (with the reason).

If the task is not complete, leave a comment on the issue with what was done and what remains. Do not close the issue — this will be done by the merge agent.

Once complete, YOU MUST output exactly <promise>COMPLETE</promise>.

# FINAL RULES

ONLY WORK ON A SINGLE TASK.
