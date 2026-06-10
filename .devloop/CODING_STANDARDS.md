# Coding Standards

## Style

- Use `gofmt`-compliant formatting. Never submit unformatted code.
- Package names are lowercase, single words, no underscores: `auth`, `store`, `ingest`.
- Exported names are PascalCase; unexported names are camelCase.
- Acronyms follow Go convention: `ID`, `URL`, `HTTP`, `API` (not `Id`, `Url`, `Http`).
- Prefer named return values only when they materially aid clarity (e.g., multiple returns of the same type). Never use them as a substitute for clear variable names.
- Keep functions short. If a function exceeds ~50 lines, look for a natural split.
- Opt to use more verbose variable names instead of shorter ones when it enhances clarity.

## Error Handling

- Wrap errors with `fmt.Errorf("context: %w", err)` to preserve the chain.
- Never swallow errors silently. Either return them or log and handle explicitly.
- Error strings are lowercase and do not end with punctuation (Go convention).
- Sentinel errors use `errors.New`; never compare error strings.

## Interfaces

- Define interfaces in the **consumer** package, not the implementor package.
- Keep interfaces small — prefer one-method interfaces where possible.
- The `MetadataStore` interface (and similar cross-cutting interfaces) lives in `internal/` and is imported by each service that needs it.
- Do not define an interface until you have at least two concrete implementations or a test fake that needs it.

## Testing

- Use hand-written fakes that implement the relevant interface. Do not use `mockery` or any mock-generation tool.
- Test behavior through public interfaces, not implementation details.
- Name fakes `Fake<InterfaceName>` (e.g., `FakeMetadataStore`) and keep them in `_test.go` files unless shared across packages, in which case they live in an `internal/testutil/` package.
- Use table-driven tests for exhaustive input coverage.
- Tests for the OTLP translation layer, analytics DSL compiler, and API key validation are pure unit tests with no external dependencies.
- Integration tests that require a real database spin up instances in Docker via `testcontainers-go`.
- Every public function that handles external input must have at least one test.

## Architecture

- Each service has its own `go.mod` inside a Go workspace (`go.work` at repo root).
- Shared types, interfaces, and utilities live in `internal/` at the repo root and are imported via the workspace.
- Services are independently deployable — no service imports another service's package directly.
- Dependency injection over global state. Pass dependencies explicitly; avoid `init()` side effects.
- The `MetadataStore` interface abstracts Postgres and SQLite. Never write SQL that is dialect-specific outside of the concrete implementation files.
- DuckDB is written exclusively by the Writer Service. Query API opens snapshots read-only. This constraint is architectural — do not add DuckDB write paths to any other service.

## Logging

- Use `slog` (stdlib) for all structured logging.
- Log at `Info` for normal operations, `Warn` for recoverable anomalies, `Error` for failures that need attention.
- Always include relevant context as key-value pairs: `slog.Error("failed to flush", "project_id", pid, "err", err)`.
- Do not use `log.Printf` or `fmt.Println` in production code paths.

## Comments

- Only comment when the **why** is non-obvious: a hidden constraint, a subtle invariant, or a workaround for a specific bug.
- Do not describe what the code does — well-named identifiers do that.
- Do not add TODO comments without a linked GitHub issue number.
