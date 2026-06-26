# aiarch server — developer make targets.

.PHONY: test test-short fmt vet lint method-check gen-schemas gen-models gen bootstrap

# Schema-first contract pipeline (strangler bootstrap), all-Go (jsonschema-go).
# DEV NOTE: these run in Go WORKSPACE mode (NOT GOWORK=off) so they see the local
# framework-go (layer Context types not yet in the published v0.2.0). Once
# framework-go/v0.3.0 is tagged + go.mod bumped, restore GOWORK=off.
#   gen-schemas — reflect each component's contract surface into its
#                 contract.schema.json SEED (cmd/schemagen).
#   gen-models  — generate contract.gen.go from each contract.schema.json
#                 (cmd/modelgen). The schema is the source of truth.
#   gen         — both, in order (steady-state regen; idempotent).
gen-schemas:
	go run ./cmd/schemagen

gen-models:
	go run ./cmd/modelgen

gen: gen-schemas gen-models

# bootstrap — the FULL automated dance that breaks the schema-first chicken-and-egg
# (schemagen must reflect a COMPILING package, so generate first, then strip the
# now-duplicate hand-written surface). Idempotent: safe to re-run.
#   1. gen-schemas  reflect current Go -> contract.schema.json (needs compiling pkgs)
#   2. gen-models   contract.schema.json -> contract.gen.go (now duplicates hand-written)
#   3. contractstrip remove the hand-written surface (refuses to drop behavior)
#   4. build        verify the package compiles from the generated surface
bootstrap: gen-schemas gen-models
	go run ./cmd/contractstrip
	go build ./...

# Everything, including the model-gated integration tests (need a live, capable
# Ollama-backed worker — see internal/manager/systemdesign/integration_test.go).
# These hang without a real model, so do NOT run this in CI.
test:
	go test ./...

# CI quality gate. `-short` makes the model-gated integration tests' TestMain
# skip (they require a live Ollama worker); everything else — including the
# fake-backed workflow gating tests — still runs.
test-short:
	go test -short ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	golangci-lint run ./...

# Run the Method conformance gate (design rules + layer rules + alignment).
# Reads .aiarch/state/project.json from the repo root (one level up).
method-check:
	go test ./internal/... -run TestMethod -v
