# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root — single-context repo, this is the only domain glossary.
- **`docs/adr/`** — read ADRs that touch the area you're about to work in.

There is no `CONTEXT-MAP.md` — this is not a multi-context monorepo.

If `CONTEXT.md` or `docs/adr/` entries relevant to your topic don't exist, proceed silently. Don't flag their absence; don't suggest creating them upfront.

## File structure

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-writer-statefulset-pvc.md
│   ├── ...
│   └── 0007-catalog-backup-is-operator-responsibility.md
├── internal/
└── services/
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md` (e.g. Span, Trace, Conversation, Score, Lake, Catalog, Ingest Buffer, Batch Ledger, End User vs User). Don't drift to synonyms the glossary explicitly avoids.

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0004 (DuckLake storage core) — but worth reopening because…_
