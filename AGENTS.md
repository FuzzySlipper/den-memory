# Den Memories - Agent Coding Standards

Coding standards, module architecture, and design patterns for the Den
Memories service. Agents working on this repo MUST follow this guide.

> Principle: agents follow established patterns well. If the codebase has
> clear, formal structure, agents will reinforce it. If it has loose
> conventions, agents will amplify the looseness. This document exists to make
> the right thing the obvious thing.

## 1. Project Direction

Den Memories is a standalone, runtime-neutral long-term memory substrate for
Den. It owns the memory ontology, capture/curation lifecycle, guided recall,
source refs, audit exports, and doctor surfaces. Hermes, pi-crew, Den Core, and
future clients should integrate through thin adapters over the service API.

### 1.1 Canonical implementation language

The canonical service implementation is Go.

- Do not add new Python runtime service code.
- The existing Python implementation is an executable prototype/spec while the
  Go implementation is brought up.
- Python may be touched only for temporary parity tests, one-off migration
  helpers, or removing the old implementation during the rewrite.
- New production service features belong in Go.

### 1.2 Contract-first behavior

The v0 contract artifacts are load-bearing:

- `contracts/v0/registry.json`
- `contracts/v0/scoring-defaults.json`
- `contracts/v0/schemas/*.schema.json`
- `examples/v0/*.example.json`

Agents must preserve the public API semantics and JSON response shapes unless a
task explicitly changes the contract. Contract changes require updated schemas,
examples, tests, and documentation.

### 1.3 Service boundary

Den Memories must not import Den Core, Hermes, or pi-crew internals. Store
source references and concise summaries, not copied authoritative records from
other systems.

Adapters may differ by language and runtime wiring. They must not fork the
memory ontology, scoring vocabulary, packet shape, or capture semantics.

## 2. Target Go Architecture

Use small packages with explicit dependencies. Keep HTTP, storage, recall, and
contract logic independently testable.

Recommended layout:

```text
cmd/den-memories/        -> CLI flags, config loading, server startup/shutdown
internal/app/            -> service assembly and dependency wiring
internal/httpapi/        -> HTTP routes, request/response mapping
internal/store/          -> SQLite repositories and transactions
internal/migrations/     -> embedded SQL migrations
internal/recall/         -> scoring, traversal, packet rendering
internal/capture/        -> capture policy and candidate creation logic
internal/curation/       -> claim/promote/reject/split/merge flows
internal/audit/          -> doctor and audit export logic
internal/contracts/      -> registry/scoring-default loading and validation
internal/testutil/       -> shared test database/server helpers
pkg/client/              -> optional Go client for external callers
contracts/               -> runtime-neutral JSON Schema artifacts
examples/                -> valid example payloads
docs/                    -> design docs, runbooks, reports
```

Do not create broad catch-all packages such as `internal/common`, `internal/util`,
or `internal/helpers`. If a helper does not have an obvious owning package, the
design probably needs another pass.

## 3. File and Package Rules

- One primary type or cohesive responsibility per file.
- Target file size: under 300 lines.
- Hard file size limit: 500 lines. Split before crossing it.
- `main.go` is for startup only: flags/config, dependency assembly call,
  signal handling, and process exit.
- Co-locate tests: `foo_test.go` lives next to `foo.go`.
- Directory nesting should stay shallow: normally no more than 3 levels below
  repo root.
- Prefer several focused files over one overstuffed file.
- Avoid circular package dependencies by keeping domain logic below HTTP
  adapters.

## 4. Go Style

### 4.1 Formatting and imports

- Use `gofumpt` formatting when available; otherwise use `gofmt`.
- Use `goimports` grouping: standard library, third-party, internal.
- Keep package comments for non-trivial packages.
- Exported symbols require doc comments that start with the symbol name.

### 4.2 Naming

| Category | Convention | Example |
| --- | --- | --- |
| Files | snake_case | `recall_packet.go` |
| Packages | short lowercase | `recall`, `store`, `httpapi` |
| Types | PascalCase | `RecallPacket` |
| Interfaces | PascalCase, small | `Store`, `Clock`, `Scorer` |
| Exported funcs | PascalCase | `NewServer` |
| Unexported funcs | camelCase | `scoreItem` |
| Tests | `*_test.go` | `recall_packet_test.go` |
| Test funcs | PascalCase | `TestRecallSkipsSupersededEntry` |

Receiver names should be short and idiomatic: `s *Store`, `h *Handler`,
`r *Repository`, `srv *Server`.

### 4.3 Type discipline

- Avoid `any` except at JSON/API boundaries.
- Convert untyped JSON request maps into typed request structs as early as
  possible.
- Keep internal functions concrete and typed.
- If a type assertion is unavoidable and failure would be surprising, explain
  the invariant with a short comment.
- Prefer `time.Time`, typed enums, and domain structs internally; marshal to the
  contract shape at the boundary.

## 5. Dependencies

- Prefer the Go standard library.
- New dependencies must earn their keep and be small, maintained, and easy to
  remove.
- Use `modernc.org/sqlite` for SQLite unless a task explicitly changes this
  decision.
- Do not introduce CGO-only dependencies for the core service.
- Do not add framework-heavy HTTP stacks. Standard `net/http` plus a small
  router is enough if route matching becomes tedious.
- No local `replace` directives in committed `go.mod` unless explicitly
  approved for a short-lived branch.

## 6. SQLite and Storage Rules

SQLite is the source of truth for v0. Keep database behavior explicit and
portable.

- Use `modernc.org/sqlite`.
- Enable `PRAGMA foreign_keys = ON` for every connection.
- Configure `busy_timeout`.
- Prefer WAL mode for file-backed databases.
- Keep migrations as ordered SQL files embedded into the binary.
- Migrations must be idempotent where practical and tested from an empty DB.
- Use transactions for multi-step writes such as capture, promotion, split,
  merge, recall logging, and FTS refresh.
- Keep FTS maintenance in the store layer, not scattered through HTTP handlers.
- Use parameterized SQL. Dynamic SQL is allowed only for whitelisted table,
  column, or sort names.
- JSON stored in SQLite should be canonical JSON produced by `encoding/json`.
- Treat SQLite JSON and FTS5 support as required startup/test capabilities.

Any startup path should fail clearly if required tables, FTS5, JSON functions,
or migrations are unavailable.

## 7. API and Contract Rules

- Public JSON uses the v0 contract field names, including snake_case.
- HTTP handlers should be thin: decode, validate, call service/store, encode.
- Do not leak SQL rows directly from handlers. Map rows into response structs.
- Unknown registry values must be rejected, not silently accepted.
- Preserve auditability: every capture attempt gets a capture event, including
  ignored, filtered, and errored decisions.
- Candidates are not truth. Promotion requires curation logic and a curation
  event.
- Independent audit surfaces must remain read-only and must not call recall.

## 8. Error Handling

Use typed sentinel errors for known failure modes:

```go
var (
    ErrNotFound   = errors.New("not found")
    ErrDuplicate  = errors.New("already exists")
    ErrInvalid    = errors.New("invalid input")
    ErrConflict   = errors.New("conflict")
    ErrForbidden  = errors.New("forbidden")
)
```

- Wrap errors with context: `fmt.Errorf("promote candidate %d: %w", id, err)`.
- Use `errors.Is` and `errors.As`; do not compare errors with `==` except when
  defining or checking exact sentinels in the same package.
- Never swallow errors. If ignoring an error is correct, add a short comment.
- HTTP error mapping belongs in `internal/httpapi`, not in store packages.
- Error responses must stay contract-compatible and deterministic.

## 9. Testing Standards

Tests are the guard rail for the rewrite.

- Use real SQLite databases in tests, usually under `t.TempDir()`.
- Fakes are preferred over mocks. Do not add a mocking framework.
- Each test owns its database and server. No test interdependence.
- Add black-box HTTP tests for public API behavior.
- Add store tests for migrations, constraints, FTS, JSON queries, and
  transaction behavior.
- Add recall tests for authority scope, discovery scope, claim strength,
  skipped reasons, traversal depth, and ordering.
- Validate JSON Schema examples as part of the test suite.
- Keep parity tests from the Python prototype until the Go implementation owns
  the behavior.

Useful commands:

```bash
go test ./... -count=1
go test ./... -race
go vet ./...
go build -o /tmp/den-memories ./cmd/den-memories
```

If `staticcheck` is available, run it before closeout.

## 10. Agent Workflow

Before implementing a task:

1. Read this file.
2. Read the relevant Den task and project documents when available.
3. Check `docs/`, `contracts/`, and existing tests before inventing behavior.
4. Keep changes scoped to the task.
5. Update tests and docs with code changes that affect behavior.

For Den Memories work, the most relevant design documents are usually:

- `den-memories-v0-reconciliation`
- `den-memories-runtime-adapter-contract`
- `den-memories-v0-contract-artifacts`
- `den-memories-implementation-sketch`

When using Den MCP, attach meaningful closeout evidence to tasks: tests run,
smoke results, packet IDs, migration notes, and any contract changes.

## 11. Documentation

- Keep README commands current.
- Design decisions that affect future agents should be documented in `docs/` or
  as Den project documents.
- Use short `// DESIGN:` comments only where code alone will not explain an
  important tradeoff.
- Avoid comments that merely restate the code.

## 12. Version Control

Do not commit unless the user explicitly asks.

When committing is requested, use concise messages and reference Den task IDs
when applicable:

```text
feat(store): add Go SQLite migration runner for task 1234
fix(recall): preserve skipped superseded entries in packets
test(api): add capture policy parity coverage
docs(agents): add Go service coding standards
```

`main` should remain buildable and testable.
