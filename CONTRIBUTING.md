# Contributing

Read `PLAN.md` for what Ekin is and `CONVENTIONS.md` before writing code. The layer
rules there are enforced by `make lint`.

## Before you open a pull request

```sh
make test       # go test -race ./...
make lint       # golangci-lint, including the layer-boundary rules
make generate   # after changing api/openapi.yaml; commit the generated files
cd web && bun run lint && bun run build
```

- A backend fix comes with a Go test that fails without it, table-driven and next to
  the code under test where it fits.
- Commits follow Conventional Commits with no scope and a one-line, lowercase,
  imperative subject: `fix: keep the preview when only the title changes`. Allowed
  types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`, `ci`.
- One unit of work per commit; don't mix unrelated changes.

By contributing you agree that your work is licensed under the project's
[FSL-1.1-ALv2](LICENSE.md) license, including its grant of the Apache License 2.0
two years after each release.
