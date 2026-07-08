# aiarch server — developer make targets.

.PHONY: test test-short fmt vet lint sumtype-check method-check encapsulation-check gen-schemas gen-models gen gen-client gen-internal-tools gen-temporal gen-temporal-check bootstrap

# Contract codegen pipeline, all-Go (jsonschema-go). project.json
# (.serviceContracts) is the OWNER of every built component's contract; modelgen
# generates contract.gen.go straight from it.
# All targets run GOWORK=off — the server builds against the PUBLISHED platform
# tags (framework-go v0.3.0 + the framework-go-{http,mcp}-generator modules), so
# the Go workspace is no longer required for server build/codegen/test.
#   gen-models  — generate contract.gen.go from project.json .serviceContracts
#                 (cmd/modelgen). project.json is the source of truth.
#   gen         — steady-state regen (idempotent); just gen-models.
#   gen-schemas — BOOTSTRAP-ONLY re-seed, fully mechanical (no hand-edit):
#                   1. cmd/schemagen  reflects current Go into each registered
#                      component's <dir>/contract.schema.json.
#                   2. cmd/contractfold folds that document straight into
#                      project.json's .serviceContracts[<key>] (a surgical byte
#                      splice — see cmd/internal/contractfold's package doc),
#                      for every component that already has BOTH a
#                      .serviceContracts entry and a freshly-written
#                      contract.schema.json.
#                 NOT part of the source of truth (project.json is) — do not use
#                 in steady state; gen-models already reads project.json
#                 directly. To bootstrap a BRAND NEW component's first contract
#                 (no .serviceContracts entry yet), run schemagen for it, then
#                 `go run ./cmd/contractfold <dir> <key>` directly (creates the
#                 entry) instead of the bulk `gen-schemas` target.
gen-models:
	GOWORK=off go run ./cmd/modelgen

# gen-client — generate the entire CLIENT layer (internal/client/{web,mcp}) +
# the merged api/openapi.yaml from project.json .serviceContracts (cmd/clientgen,
# driving the published framework-go-http-generator + framework-go-mcp-generator).
# Reproducible + idempotent; internal/client carries ZERO hand-written Go.
gen-client:
	GOWORK=off go run ./cmd/clientgen

# gen-internal-tools — generate the INTERNAL MCP tool surface (one tool per
# ResourceAccess/Engine contract operation) from project.json .serviceContracts
# into internal/resourceaccess/projectstate/toolcatalog.gen.go (cmd/internaltoolsgen).
# NEVER merged into api/openapi.yaml — this is the internal surface aiarch-state-mcp
# registers, scoped per task by the agentic sub-workflow's tool palette.
gen-internal-tools:
	GOWORK=off go run ./cmd/internaltoolsgen

gen: gen-models gen-client gen-internal-tools

# gen-temporal — generate the per-manager Temporal layer (activities/invokers/
# worker .gen.go) for the 5 managers from project.json .serviceContracts
# (cmd/appgen, driving framework-go-app-generator/temporalgen).
# WORKSPACE-ONLY, NOT GOWORK=off: framework-go-app-generator has no published
# version yet, so cmd/appgen is gated behind the `appgen` build tag and
# resolved via go.work rather than server/go.mod. NOT folded into the `gen`
# aggregate for the same reason — `make gen` must keep working under
# GOWORK=off CI, which has no ../archistrator-platform checkout.
# Task-14 followup: once framework-go-app-generator is released, pin real
# versions in go.mod, drop the appgen tag, flip this to GOWORK=off like the
# other gen targets, and fold it into `gen`.
gen-temporal:
	go run -tags appgen ./cmd/appgen

# gen-temporal-check — drift gate for the generated Temporal layer, same shape
# as systemtests' `gen-check` (regenerate, then fail on any diff). WORKSPACE-ONLY
# like gen-temporal above: requires go.work + a sibling ../archistrator-platform
# checkout, so it is NOT wired into server-checks.yml yet — CI checks out only
# this repo, and there is no in-repo precedent for a workspace-dependent gate
# (methoddesign is ALSO local-only for the same published-version reason, see
# method-check above). Run this locally (or from a local workspace checkout in
# a future CI job that also checks out archistrator-platform) before landing a
# .serviceContracts change that touches the 5 migrated managers.
# Task-14 followup: once framework-go-app-generator is released and gen-temporal
# flips to GOWORK=off (see the note above), fold this into a CI step the same
# way systemtests/Makefile's gen-check is wired into systemtests.yml.
gen-temporal-check: gen-temporal
	git diff --exit-code -- 'internal/manager/**/*.gen.go'

gen-schemas:
	GOWORK=off go run ./cmd/schemagen
	GOWORK=off go run ./cmd/contractfold

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

# Run the Method conformance gate. With -tags methoddesign this runs BOTH the
# layer rules (TestMethodLayering) and the DESIGN-rule gate over the committed
# .aiarch/state/project.json (TestMethodDesignArtifacts) against the PUBLISHED
# framework-go v0.4.0, which decodes the C4 container deployment model. -count=1
# defeats the test cache: TestMethodDesignArtifacts reads an out-of-tree file
# (../.aiarch/state/project.json), so a green run would otherwise cache and a
# later project.json edit would never re-run.
# NOTE: the design↔code ALIGNMENT pass is intentionally NOT enabled yet — see the
# SCOPE note in method_design_test.go (a component/package naming reconciliation +
# a not-yet-built mechanism must be decided first). This target guards the design
# rules + the layer rules.
method-check:
	GOWORK=off go test -tags methoddesign ./internal/... -run TestMethod -count=1 -v

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
