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

PROJECT_JSON ?= ../.aiarch/state/project.json
GEN_DIR       := internal/generated
# PID-suffixed and evaluated ONCE (`:=`, not `?=`/`=` — those re-run $(shell)
# on every reference, which would hand each recipe line in `gen-check` a
# DIFFERENT dir) so concurrent `make gen-check` runs (e.g. two agents/CI jobs
# on the same box) never race on the same scratch dir.
GEN_CHECK_DIR := /tmp/aiarch-gen-systemtests-check.$(shell echo $$$$)

.PHONY: test-short test test-integration record up down temporal temporal-stop temporal-ui fmt vet gen gen-check

# Always-green structural gate: R6 constitution tests run, wire tests skip. No infra.
test-short:
	go test -short ./...

# Regenerate the wire-test step tables from the committed System Test Plan
# (.aiarch/state/project.json .testingState.systemTestPlan) — so the plan and
# the table it drives can never drift. The generator lives in the SERVER module
# (it decodes project.json via projectstate) but is never imported by
# systemtests: it only WRITES into internal/generated, whose files (besides the
# hand-written types.go) import nothing (constitution R1/R3). `go run` resolves
# packages against the CURRENT module even under GOWORK=off, so the generator
# must be invoked FROM the server module — cd there, not a relative package path.
gen:
	cd ../server && GOWORK=off go run ./cmd/gen-systemtests \
		-project $(PROJECT_JSON) \
		-out ../systemtests/$(GEN_DIR)

# Drift check: regenerate into a scratch dir (seeded with the hand-written
# types.go, which the generator never writes) and diff against the committed
# internal/generated tree. Nonzero exit on ANY difference — a plan/table drift
# fails this, not a downstream flaky symptom. Safe to run twice in a row: the
# generator is byte-stable idempotent (see gen-systemtests's content-hash
# header comment), so an unchanged plan always produces a clean diff.
gen-check:
	rm -rf $(GEN_CHECK_DIR)
	mkdir -p $(GEN_CHECK_DIR)
	cp $(GEN_DIR)/types.go $(GEN_CHECK_DIR)/types.go
	cd ../server && GOWORK=off go run ./cmd/gen-systemtests \
		-project $(PROJECT_JSON) \
		-out $(GEN_CHECK_DIR)
	diff -rq $(GEN_DIR) $(GEN_CHECK_DIR)
	rm -rf $(GEN_CHECK_DIR)

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
