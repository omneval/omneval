# ISSUE

You are planning a single issue — the one whose `{{AGENT_LABEL}}` label triggered this run. Fetch its current details:

```
gh issue view {{TRIGGERING_ISSUE}} --json number,state,title,body,labels,comments
```

# TASK

Confirm the issue is still open and still carries the `{{AGENT_LABEL}}` label. If it's closed or no longer labeled, someone else has already handled it — output an empty plan.

Otherwise, assign it a branch name using the format `agent/issue-{id}-{slug}`.

omneval is a multi-ecosystem monorepo: Go services (each with its own `go.mod` under `go.work`, shared types in `internal/`), a React/TypeScript UI in `ui/`, and Go/Python/TypeScript SDKs in `sdk/`. An issue may span several of these layers — that does not change the plan, but note it in the title slug if helpful.

{{FEEDBACK}}

# OUTPUT

Output your plan as a JSON object wrapped in `<plan>` tags:

<plan>
{"issues": [{"id": "42", "title": "Fix auth bug", "branch": "agent/issue-42-fix-auth-bug"}]}
</plan>

If the issue should not be planned (closed, unlabeled, or otherwise already resolved), output:

<plan>
{"issues": []}
</plan>
