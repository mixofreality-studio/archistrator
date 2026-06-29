# aiarch server — developer make targets.

.PHONY: test test-short fmt vet lint sumtype-check method-check encapsulation-check gen-schemas gen-models gen gen-client bootstrap

# Contract codegen pipeline, all-Go (jsonschema-go). project.json
# (.serviceContracts) is the OWNER of every built component's contract; modelgen
# generates contract.gen.go straight from it.
# All targets run GOWORK=off — the server builds against the PUBLISHED platform
# tags (framework-go v0.3.0 + the framework-go-{http,mcp}-generator modules), so
# the Go workspace is no longer required for server build/codegen/test.
#   gen-models  — generate contract.gen.go from project.json .serviceContracts
#                 (cmd/modelgen). project.json is the source of truth.
#   gen         — steady-state regen (idempotent); just gen-models.
#   gen-schemas — DEPRECATED / bootstrap-only. Re-seeds the legacy per-component
#                 contract.schema.json by reflecting current Go (cmd/schemagen).
#                 NOT part of the source of truth — do not use in steady state.
gen-models:
	GOWORK=off go run ./cmd/modelgen

# gen-client — generate the entire CLIENT layer (internal/client/{web,mcp}) +
# the merged api/openapi.yaml from project.json .serviceContracts (cmd/clientgen,
# driving the published framework-go-http-generator + framework-go-mcp-generator).
# Reproducible + idempotent; internal/client carries ZERO hand-written Go.
gen-client:
	GOWORK=off go run ./cmd/clientgen

gen: gen-models gen-client

gen-schemas:
	GOWORK=off go run ./cmd/schemagen

# bootstrap — re-bootstrap a component: generate its contract.gen.go from
# project.json, then strip the now-duplicate hand-written surface (refuses to drop
# behavior). Idempotent: in steady state the components are already stripped, so
# contractstrip is a no-op. Safe to re-run.
#   1. gen-models    project.json -> contract.gen.go
#   2. contractstrip remove any hand-written surface owned by the contract
#   3. build         verify the packages compile from the generated surface
bootstrap: gen-models
	GOWORK=off go run ./cmd/contractstrip
	GOWORK=off go build ./...

# Everything, including the model-gated integration tests (need a live, capable
# Ollama-backed worker — see internal/manager/systemdesign/integration_test.go).
# These hang without a real model, so do NOT run this in CI.
test:
	GOWORK=off go test ./...

# CI quality gate. `-short` makes the model-gated integration tests' TestMain
# skip (they require a live Ollama worker); everything else — including the
# fake-backed workflow gating tests — still runs.
test-short:
	GOWORK=off go test -short ./...

fmt:
	gofmt -w .

vet:
	GOWORK=off go vet ./...

lint:
	golangci-lint run ./...

# Exhaustiveness check over generated sealed sum types (the //sumtype:decl
# interfaces emitted by cmd/modelgen). Runs the pinned go-check-sumtype tool
# dependency (go.mod `tool` directive) in workspace mode. golangci-lint also runs
# this as its bundled `gochecksumtype` linter (.golangci.yml).
sumtype-check:
	GOWORK=off go tool go-check-sumtype ./...

# Run the Method conformance gate (design rules + layer rules + alignment).
# Reads .aiarch/state/project.json from the repo root (one level up).
method-check:
	GOWORK=off go test ./internal/... -run TestMethod -v

# ENCAPSULATION GATE. Enforces that the ONLY exported symbols in the
# engine/resourceaccess/manager packages are the generated contract surface
# (contract.gen.go + the in-package types it structurally references) plus the
# small DOCUMENTED allowlist in internal/arch_test.go. Fails the moment any new
# non-generated public symbol appears in those layers. Also runs as part of
# `test`/`test-short` (it is an ordinary `go test`), but exposed here as a
# dedicated gate. To regenerate the allowlist after an intentional contract
# change: ENCAP_DUMP=1 GOWORK=off go test ./internal/ -run TestGeneratedOnlyPublic -v
encapsulation-check:
	GOWORK=off go test ./internal/ -run TestGeneratedOnlyPublic
