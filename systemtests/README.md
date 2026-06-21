# archistrator system-test harness

The black-box, wire-level **System Test + Regression Harness** for archistrator —
the load-bearing (and only routine) test tier per the test-authoring
constitution (`the-method-testing` §7) and `operational-concepts.md` §17.

## What makes it un-cheatable

This is a **separate Go module**, a *sibling* to `../server` (never under it).
Because it sits outside the server's package tree, Go's `internal/` rule
**compiler-seals** `github.com/mixofreality-studio/archistrator/server/internal/...`
against import here — any such import is a `go build` error, not a lint. The
harness drives the running server **only over its published Client surfaces**
and links **zero** server code:

- it boots the real `cmd/server` binary as a **subprocess** (a `go build`
  subprocess, not a Go import);
- it speaks **HTTP** (webClient) and — once `mcpClient` is built — **MCP**;
- its import graph is **stdlib-only** (no `google/uuid`, no `testinfra`).

Two enforcement layers prove it, on every `go test` (no infra needed):

- `constitution/` — walks the module's source and fails if anything imports the
  server module or a mocking library, and asserts the module is not under a
  `server/` tree (R6).
- `.golangci.yml` — depguard bans the same imports at lint time.

## Layout

```
internal/harness/   wire driver: server subprocess boot, Transport seam, HTTP transport, step helpers
usecases/           one black-box flow per core use case (UC1 today), each driven through the surface
constitution/       R6 structural enforcement (no infra; always runs)
```

The `Transport` interface is the **transport-agnostic seam**: `runUC1` is written
once and runs against any surface. When `mcpClient` lands (activity C-MC), add an
MCP transport and the **R4 cross-surface equivalence test** runs UC1 through HTTP
*and* MCP, asserting identical committed state.

## Running

The wire tests need a provisioned stack. Without it — under `-short`, or with no
`ARCHISTRATOR_*` infra env — they **skip** (the `constitution/` tests still run).

Provisioning is split: **Postgres + Ollama** run in docker-compose; **Temporal**
runs as a local `temporal server start-dev` dev server (persistent SQLite + web
UI) via `scripts/temporal-dev.sh`. So you need Docker, the `temporal` CLI, and Go.

```bash
# Always-green structural gate (no infra):
make test-short                  # go test -short ./...

# Full wire run — brings the whole stack up, then runs the flows:
make test-integration

# ...or by hand:
make up                          # postgres+ollama (compose) + temporal dev server
export ARCHISTRATOR_POSTGRES_URL=postgres://archistrator:archistrator@localhost:5432/archistrator?sslmode=disable
export ARCHISTRATOR_TEMPORAL_HOSTPORT=localhost:7233
export ARCHISTRATOR_TEMPORAL_NAMESPACE=aiarch-test
export ARCHISTRATOR_OLLAMA_BASEURL=http://localhost:11434
go test ./...

make down                        # stop temporal dev server + compose (keeps volumes/DB)
```

The default model is `qwen2.5:3b` (`make OLLAMA_MODEL=… test-integration` to
override). In CI, run `temporal server start-dev --headless` and a Postgres
service container, and point the same env at them — the harness only cares about
the endpoints.

## Browsing test workflows (`make temporal-ui`)

The local Temporal dev server persists to `.temporal/aiarch-test.db` and serves
the web UI on `:8233`. Every wire-test workflow execution + full event history is
browsable, and survives `make down` (the DB file is kept):

```bash
make test-integration            # populates .temporal/aiarch-test.db
make temporal-ui                 # ensures the dev server is up, opens http://localhost:8233
```

This is the faithful port of the server module's `make temporal-ui` — same
`temporal server start-dev` against a persistent `.temporal/aiarch-test.db`,
same namespace (`aiarch-test`).
