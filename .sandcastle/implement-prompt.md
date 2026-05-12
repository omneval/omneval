# TASK

Fix issue {{TASK_ID}}: {{ISSUE_TITLE}}

Pull in the issue using `gh issue view {{TASK_ID}}`. If it has a parent PRD, pull that in too.

Only work on the issue specified.

Work on branch {{BRANCH}}. Make commits and run tests.

# CONTEXT

Here are the last 10 commits:

<recent-commits>

!`git log -n 10 --format="%H%n%ad%n%B---" --date=short`

</recent-commits>

# EXPLORATION

Explore the repo and fill your context window with relevant information that will allow you to complete the task.

Pay extra attention to test files that touch the relevant parts of the code.

# EXECUTION

Use the Ralph Loop (RGR) to complete the task:

1. RED: write one failing test (`go test ./...` confirms it fails)
2. GREEN: write the minimum implementation to pass that test
3. REPEAT until all acceptance criteria are covered
4. REFACTOR: clean up without breaking tests

# FEEDBACK LOOPS

Before committing, run the following to confirm everything is clean:

```
go build ./...
go vet ./...
go test ./...
```

All three must pass with no errors before you commit.

# COMMIT

Make a git commit. The commit message must:

1. Start with `RALPH:` prefix
2. Include task completed + issue reference (e.g. `fixes #{{TASK_ID}}`)
3. Key decisions made
4. Files changed
5. Blockers or notes for next iteration

Keep it concise.

# THE ISSUE

If the task is not complete, leave a comment on the issue with what was done and what remains.

Do not close the issue — this will be done by the merge agent.

Once complete, YOU MUST output exactly <promise>COMPLETE</promise>.

# FINAL RULES

ONLY WORK ON A SINGLE TASK.
