# aiarch server — developer make targets.

.PHONY: test test-short fmt vet lint sumtype-check method-check gen-schemas gen-models gen gen-client bootstrap

# Contract codegen pipeline, all-Go (jsonschema-go). project.json
# (.serviceContracts) is the OWNER of every built component's contract; modelgen
# generates contract.gen.go straight from it.
# DEV NOTE: these run in Go WORKSPACE mode (NOT GOWORK=off) so they see the local
# framework-go (layer Context types not yet in the published v0.2.0). Once
# framework-go/v0.3.0 is tagged + go.mod bumped, restore GOWORK=off.
#   gen-models  — generate contract.gen.go from project.json .serviceContracts
#                 (cmd/modelgen). project.json is the source of truth.
#   gen         — steady-state regen (idempotent); just gen-models.
#   gen-schemas — DEPRECATED / bootstrap-only. Re-seeds the legacy per-component
#                 contract.schema.json by reflecting current Go (cmd/schemagen).
#                 NOT part of the source of truth — do not use in steady state.
gen-models:
	go run ./cmd/modelgen

# gen-client — generate the entire CLIENT layer (internal/client/{web,mcp}) +
# the merged api/openapi.yaml from project.json .serviceContracts (cmd/clientgen,
# driving framework-go-http-generator + framework-go-mcp-generator). Reproducible
# + idempotent; internal/client carries ZERO hand-written Go. Runs in workspace
# mode (the generators are local sibling modules in go.work).
gen-client:
	go run ./cmd/clientgen

gen: gen-models gen-client

gen-schemas:
	go run ./cmd/schemagen

# bootstrap — re-bootstrap a component: generate its contract.gen.go from
# project.json, then strip the now-duplicate hand-written surface (refuses to drop
# behavior). Idempotent: in steady state the components are already stripped, so
# contractstrip is a no-op. Safe to re-run.
#   1. gen-models    project.json -> contract.gen.go
#   2. contractstrip remove any hand-written surface owned by the contract
#   3. build         verify the packages compile from the generated surface
bootstrap: gen-models
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

# Exhaustiveness check over generated sealed sum types (the //sumtype:decl
# interfaces emitted by cmd/modelgen). Runs the pinned go-check-sumtype tool
# dependency (go.mod `tool` directive) in workspace mode. golangci-lint also runs
# this as its bundled `gochecksumtype` linter (.golangci.yml).
sumtype-check:
	go tool go-check-sumtype ./...

# Run the Method conformance gate (design rules + layer rules + alignment).
# Reads .aiarch/state/project.json from the repo root (one level up).
method-check:
	go test ./internal/... -run TestMethod -v
