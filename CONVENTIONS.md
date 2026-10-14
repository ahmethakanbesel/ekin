# Engineering conventions

Source of truth for cross-cutting decisions. Update it when a decision turns out to be
wrong — don't let the code and this file drift apart.

## 1. Layers

Business, transport and persistence are separate packages. Each bounded context has one
version-numbered package per layer, named after the context:

```
internal/domain/<ctx>/v1/          package <ctx>    aggregates, value objects, rules
internal/repository/<ctx>/v1/      package <ctx>    Repository interface + sqlite + inmemory
internal/service/<ctx>/v1/         package <ctx>    use cases
internal/transport/http/<ctx>/v1/  package handler  OpenAPI strict-server slice
internal/transport/job/<ctx>/v1/   package worker   job handlers
internal/adapter/<vendor>/                          third-party integrations (ports live in service)
```

Import rules (enforced by `depguard` in `.golangci.yml`):

| Layer | May import | Must not import |
|---|---|---|
| domain | stdlib, `internal/apperror`, `pkg/*`, pure libraries (uuid, cron parser) | repository, service, transport, adapter, db, gen |
| repository | domain, `internal/db`, `internal/apperror` | service, transport |
| service | domain, repository, `internal/db`, other services | transport, gen |
| transport | service, domain, `internal/gen`, `internal/apperror` | repository, db |

Callers alias imports by layer: `pipelinedomain`, `pipelinerepo`, `pipelinesvc`.

## 2. Domain model

- **No anemic types.** Aggregates keep their fields unexported and expose behaviour:
  `p.Pause(now)`, `p.Reschedule(s, now)`, `p.Trigger(now)`. A rule that decides something
  (state transitions, schedule maths, validation of combinations) lives on the domain type,
  not in a service `if`/`switch`.
- **Constructors guard invariants.** `New(...)` builds a valid aggregate or returns an
  `apperror`. There is no way to obtain an invalid one.
- **Persistence boundary.** `Snapshot()` returns a plain exported struct for the repository
  to store; `Rehydrate(Snapshot)` rebuilds the aggregate and re-checks invariants. Only
  repositories call `Rehydrate`.
- **Parse, don't validate.** Transport turns wire values into domain values once
  (`ParseLanguage`, `ParseSchedule`, ...). Services only ever receive parsed types.
- Times are `time.Time` in UTC; a timezone is a value (`*time.Location`) where it matters
  (schedules, workspace display).
- IDs are UUIDv7 (`pkg/id`).

## 3. Services

Services orchestrate: load → call domain → save → enqueue / notify, inside
`db.TxManager.WithinTx` when more than one write must be atomic. They hold no business
decisions of their own. Each takes `now func() time.Time` so tests control the clock.

## 4. Errors

`internal/apperror` codes map to HTTP status in one table. Handlers return errors; the
strict-server error handler writes `{"code", "message"}`. Never hardcode a status in a
handler.

## 5. Persistence

- SQLite via `modernc.org/sqlite` (pure Go — static binary, cross-compiles).
- Pragmas: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`, `synchronous=NORMAL`.
- One writer connection (`SetMaxOpenConns(1)`) for writes and transactions; a separate
  read pool. `db.TxManager` stores the transaction in the context; repositories ask
  `db.Q(ctx)` for the right querier.
- Migrations: goose, SQL files embedded in `internal/db/migrations`, run with
  `kurgu migrate up` (and at `serve` start when `KURGU_MIGRATE_ON_START=true`, the default
  for single-instance deployments).
- Every table carries `workspace_id`; every repository method takes it explicitly.
- Each repository has an in-memory implementation and a shared conformance test suite
  that both implementations pass.
- Timestamps are stored as Unix milliseconds (`INTEGER`).

## 6. Jobs

The queue is a `jobs` table. A worker claims with `UPDATE … RETURNING` under the writer
connection, holds a lease it extends by heartbeat, and completes, retries (exponential
backoff) or fails the job. A `unique_key` prevents double-enqueueing a scheduled slot.
Job handlers are transport: they parse the payload and call a service.

## 7. Configuration

Environment variables prefixed `KURGU_`, with safe local defaults. Secrets fail fast when
unset in production. Runtime-tunable settings (throttling) live in the database with env
defaults.

## 8. Dependencies

Go modules are **not vendored** (the pure-Go SQLite driver alone is ~100 MB across
platforms). `go.sum` pins versions; Docker builds use a module cache mount. Tools run as
pinned `go run module@version`.

## 9. Testing

- Domain: table-driven unit tests next to the code.
- Repository: conformance suite run against inmemory and sqlite (temp file).
- Service: inmemory repositories + fixed clock.
- HTTP: `httptest` through the real router.
- A backend bug fix ships with a test that fails without the fix.

## 10. Access control

- The spec is the source of truth for who may call what: an operation is public only when
  it says `security: []`; everything else needs the `kurgu_session` cookie.
- The router's strict middleware (`internal/transport/http/access.go`) authenticates, and
  for `/workspaces/{workspaceId}/...` checks the caller's membership against the
  operation's `identity.Action` (view / edit / manage). A workspace operation missing
  from that table is refused, and `TestAccessRulesCoverSpec` fails.
- Non-members get `WorkspaceNotFound`, not `Forbidden`, so workspace ids do not leak.
- Rules that depend on more than the role (last owner, leaving vs removing) live on the
  domain (`Membership.ChangeRole`, `Membership.CheckRemoval`).
- Handlers read the caller from `principal.From(ctx)` / `principal.Member(ctx)`; services
  take the actor's user id explicitly and never read the context.
- Secrets (session and invitation tokens) are stored as SHA-256 hashes; request logs
  record the route pattern, never the path.

## 11. Routes outside the spec

Two route groups stream binary data in ways the strict server cannot, and are registered
directly on the mux:

- `GET /api/v1/workspaces/{ws}/stories/{id}/deliverables/{did}/revisions/{n}/{artifact}` —
  revision media with Range support (`internal/transport/http/media.go`); same session and
  `view` check as the spec's operations.
- `/api/v1/worker/...` — the hybrid-worker API (`internal/transport/http/worker`),
  bearer-token auth (`KURGU_WORKER_TOKEN`), off when the token is empty.

Everything else goes through the spec.

## 12. Agent runs

- Work dirs live at `<KURGU_WORK_DIR>/<deliverable id>`; each revision's dir is archived
  to the blob store, so another host can restore it.
- Instructions are templates in `internal/service/production/v1/prompts`; they name the
  tools `kurgu tool …`, and the agent's PATH starts with a `kurgu` link to the running
  binary.
- A step is begin (server: state + inputs) → run (no database; may run remotely) →
  finish (server: archive + state). Keep `Run*` free of repositories.
