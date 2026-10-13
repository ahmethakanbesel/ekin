# Kurgu

Unattended video and article production with human review. See `PLAN.md` for
the product and `CONVENTIONS.md` before writing code.

## Run on a laptop

```sh
kurgu user create --email you@example.com --name You   # first account; prompts for a password
make run          # API, web UI, scheduler and worker in one process on :8080
make test         # go test -race ./...
make lint         # golangci-lint, including the layer-boundary rules
make generate     # regenerate the Go server and TS client from api/openapi.yaml
```

Data lives in `./data` (`KURGU_DATA_DIR`). Configuration is `KURGU_*`
environment variables; see `internal/config/config.go` for every setting and
its default.

## Processes

| Command | Runs |
|---|---|
| `kurgu serve` | API under `/api/v1`, embedded web UI, cron scheduler |
| `kurgu worker` | Job runner, throttled by `KURGU_QUEUE_CONCURRENCY`, `KURGU_HEAVY_EXCLUSIVE`, `KURGU_HEAVY_COOLDOWN` |
| `kurgu all` | Both, for a single machine |
| `kurgu migrate up\|down\|status` | Schema migrations (also applied at start unless `KURGU_MIGRATE_ON_START=false`) |
| `kurgu user create\|reset-password` | Accounts; the password comes from `KURGU_PASSWORD`, a prompt or stdin |

## Accounts

There is no sign-up page. The first account comes from `kurgu user create`; it
also becomes the owner of any workspace created before accounts existed.
Everyone else is invited from a workspace's Members page: the owner copies the
link (valid 7 days) and sends it. Roles are per workspace: **owner** (settings
and members), **editor** (pipelines, runs, review), **viewer** (read only).

Sessions are HttpOnly cookies (30 days idle, 90 days at most).
`KURGU_PUBLIC_URL` must be the address people open in the browser
(`https://...` in production): it prefixes invitation links, is the trusted
origin for writes, and an https URL marks the cookie Secure.
`kurgu user reset-password` recovers a locked-out account and signs it out
everywhere.
