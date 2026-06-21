# aiarch system-test harness — developer make targets.
#
# Provisioning: Postgres + Ollama run in docker-compose; Temporal runs as a local
# `temporal server start-dev` dev server (persistent SQLite + web UI), so its
# history is browsable via `make temporal-ui`. The harness connects to all three
# only through the ARCHISTRATOR_* env below — it imports none of them (R3).

OLLAMA_MODEL ?= qwen2.5:3b

# Drafting mode for the wire tests: replay (default, offline cassettes — no model),
# WHEN_REQUIRED (replay + record misses via Ollama), or live (direct Ollama).
DRAFTING ?= replay

.PHONY: test-short test test-integration record up down temporal temporal-stop temporal-ui fmt vet

# Always-green structural gate: R6 constitution tests run, wire tests skip. No infra.
test-short:
	go test -short ./...

# Full wire run against the provisioned stack. Brings the stack up, then builds +
# drives the real server over the wire. Workflow executions persist in the
# Temporal dev server's SQLite DB — browse them afterwards with `make temporal-ui`.
test-integration: up
	ARCHISTRATOR_POSTGRES_URL=postgres://archistrator:archistrator@localhost:5432/archistrator?sslmode=disable \
	ARCHISTRATOR_TEMPORAL_HOSTPORT=localhost:7233 \
	ARCHISTRATOR_TEMPORAL_NAMESPACE=aiarch-test \
	ARCHISTRATOR_OLLAMA_BASEURL=http://localhost:11434 \
	ARCHISTRATOR_OLLAMA_MODEL=$(OLLAMA_MODEL) \
	ARCHISTRATOR_SYSTEMTESTS_DRAFTING=$(DRAFTING) \
	go test ./... -count=1

# Record/refresh cassettes by running the wire flow with WHEN_REQUIRED (cache hit
# when present, generate+record on miss). Commit the resulting testdata/cassettes.
record:
	$(MAKE) test-integration DRAFTING=WHEN_REQUIRED

test:
	go test ./... -count=1

# Bring the whole stack up: Postgres + Ollama containers (and pull the model),
# plus the local Temporal dev server (idempotent — reused if already running).
up:
	docker compose up -d --wait
	docker compose exec -T ollama ollama pull $(OLLAMA_MODEL)
	./scripts/temporal-dev.sh

# Tear it down: stop the Temporal dev server, then the containers. KEEPS the
# Temporal SQLite DB and docker volumes so history + the pulled model survive.
down:
	./scripts/temporal-dev-stop.sh
	docker compose down

# Just the local Temporal dev server (persistent SQLite + UI), idempotent.
temporal:
	./scripts/temporal-dev.sh

temporal-stop:
	./scripts/temporal-dev-stop.sh

# Browse every test workflow execution + full event history. Ensures the Temporal
# dev server is up and opens its web UI. Run `make test-integration` first, then
# `make temporal-ui`, then browse http://localhost:8233 (namespace "aiarch-test").
temporal-ui:
	./scripts/temporal-ui.sh

fmt:
	gofmt -w .

vet:
	go vet ./...
