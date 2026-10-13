# Kurgu

Unattended video and article production with human review. See `PLAN.md` for
the product and `CONVENTIONS.md` before writing code.

## Run on a laptop

```sh
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

The API has no authentication yet (milestone 2). It listens on 127.0.0.1 by
default; do not expose it.
