# aiarch server — developer make targets.

.PHONY: test test-short fmt vet lint sumtype-check method-check encapsulation-check gen-schemas gen-models gen-models-check gen gen-client gen-client-check gen-internal-tools gen-temporal gen-temporal-check gen-sdk gen-sdk-check gen-config gen-config-check bootstrap

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

# gen-models-check — drift gate for the generated model/interface layer
# (cmd/modelgen, driving the published framework-go-app-generator/modelgen), same
# shape as gen-temporal-check: regenerate, then fail on any diff. GOWORK=off-safe
# (see gen-models above), so it runs in CI (server-checks.yml).
gen-models-check: gen-models
	git diff --exit-code -- 'internal/**/contract.gen.go'

# gen-client — generate the entire CLIENT layer (internal/client/{web,mcp}) +
# the merged api/openapi.yaml from project.json .serviceContracts (cmd/clientgen,
# driving the published framework-go-http-generator + framework-go-mcp-generator).
# Reproducible + idempotent; internal/client carries ZERO hand-written Go.
gen-client:
	GOWORK=off go run ./cmd/clientgen

# gen-client-check — drift gate for the generated CLIENT layer + merged OAS
# (cmd/clientgen, driving framework-go-http-generator + framework-go-mcp-generator),
# same shape as gen-models-check/gen-temporal-check: regenerate, then fail on any
# diff. GOWORK=off-safe (see gen-client above), so it runs in CI (server-checks.yml).
gen-client-check: gen-client
	git diff --exit-code -- api/openapi.yaml internal/client

# gen-internal-tools — generate the INTERNAL MCP tool surface (one tool per
# ResourceAccess/Engine contract operation) from project.json .serviceContracts
# into internal/resourceaccess/projectstate/toolcatalog.gen.go (cmd/internaltoolsgen).
# NEVER merged into api/openapi.yaml — this is the internal surface aiarch-state-mcp
# registers, scoped per task by the agentic sub-workflow's tool palette.
gen-internal-tools:
	GOWORK=off go run ./cmd/internaltoolsgen

gen: gen-models gen-client gen-internal-tools gen-temporal gen-sdk

# gen-temporal — generate the per-manager Temporal layer (activities/invokers/
# worker .gen.go) for the 5 managers from project.json .serviceContracts
# (cmd/appgen, driving framework-go-app-generator/temporalgen).
# Release-backed and GOWORK=off like the other gen targets:
# framework-go-app-generator + framework-go-projectmodel are published platform
# tags pinned in go.mod, so cmd/appgen builds from the module cache with no
# build tag and no ../archistrator-platform workspace checkout. Folded into the
# `gen` aggregate so `make gen` regenerates the whole codegen surface.
gen-temporal:
	GOWORK=off go run ./cmd/appgen

# gen-temporal-check — drift gate for the generated Temporal layer, same shape
# as systemtests' `gen-check` (regenerate, then fail on any diff). Now
# GOWORK=off-safe (see gen-temporal above), so it runs in CI (server-checks.yml).
gen-temporal-check: gen-temporal
	git diff --exit-code -- 'internal/manager/**/*.gen.go'

# gen-sdk — the generated client SDK (cmd/appgen, driving
# framework-go-app-generator/transportgen): a self-contained, stdlib-only Go
# client (HTTP + MCP) for the 5 managers, emitted into the SIBLING systemtests
# module at ../systemtests/internal/sdk with prune-stale + content-hash
# discipline. cmd/appgen emits it in the SAME run as the Temporal layer (one
# project.json read), so gen-sdk shares the gen-temporal prerequisite rather
# than invoking appgen a second time.
gen-sdk: gen-temporal

# gen-sdk-check — drift gate for the generated SDK in the sibling systemtests
# module, same regenerate-then-diff shape as the other gen-*-check gates. The
# path is repo-relative so `git diff` works from the server working directory
# (server-checks.yml).
gen-sdk-check: gen-temporal
	git diff --exit-code -- ../systemtests/internal/sdk

# gen-config — generate the archistrator-server container config (cmd/server/
# config.gen.go) + the systemtests harness env-name consts (systemtests/internal/
# harness/envnames.gen.go) from project.json's deployment model (cmd/configgen,
# driving framework-go-app-generator/configgen).
#
# WORKSPACE-ONLY BRIDGE (release follow-up): configgen is UNRELEASED in
# framework-go-app-generator, so — unlike the other gen-* targets — this one runs
# in Go WORKSPACE mode (NO GOWORK=off; needs the ../archistrator-platform checkout
# + go.work). cmd/configgen is behind the `configgen` build tag with a no-op stub
# so ordinary GOWORK=off build/vet/test ./... stay clean. Mirrors the step-1 appgen
# precedent. Deliberately NOT folded into `gen` (which is all GOWORK=off) and NOT
# added to CI (server-checks.yml) yet — it cannot run without the workspace. Once
# framework-go-app-generator ships configgen in a tag, fold cmd/configgen into
# cmd/appgen tag-free and add gen-config-check to CI like gen-temporal-check.
gen-config:
	go run -tags configgen ./cmd/configgen

# gen-config-check — drift gate for the generated config + harness env-names,
# same regenerate-then-diff shape as the other gen-*-check gates. WORKSPACE-only
# (see gen-config); run locally, NOT in CI until configgen ships in a platform tag.
gen-config-check: gen-config
	git diff --exit-code -- cmd/server/config.gen.go ../systemtests/internal/harness/envnames.gen.go

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
