# aiarch server — developer make targets.

.PHONY: test test-short fmt vet lint sumtype-check method-check encapsulation-check gen-models gen-models-check gen-fakes gen-fakes-check gen gen-client gen-client-check gen-internal-tools gen-internal-tools-check gen-temporal gen-temporal-check gen-sdk gen-sdk-check gen-config gen-config-check gen-uiprofiles gen-uiprofiles-check claude-assets

# claude-assets — materialize the .claude method prompt surface (agents/commands/
# skills) into the repo root from the method-assets lib pinned in go.mod. The
# method tree is NOT vendored (it is gitignored); pull it in here after checkout.
# Uses the same aiarch-state-mcp seat-assets path the construction runner uses,
# so the materialized version always tracks go.mod. App-local .claude files
# (settings.json, skills/grillme) are left untouched.
claude-assets:
	GOWORK=off go run ./cmd/aiarch-state-mcp seat-assets --dest ..

# Contract codegen pipeline, all-Go (jsonschema-go). project.json
# (.serviceContracts) is the OWNER of every built component's contract; modelgen
# generates contract.gen.go straight from it.
# All targets run GOWORK=off — the server builds against the PUBLISHED platform
# tags (framework-go v0.3.0 + the framework-go-{http,mcp}-generator modules), so
# the Go workspace is no longer required for server build/codegen/test.
#   gen-models  — generate contract.gen.go from project.json .serviceContracts
#                 (cmd/modelgen). project.json is the source of truth.
#   gen-fakes   — generate each contract's Fake<Iface> test double
#                 (<goPackage>/fake/fake.gen.go). Alias of gen-models — same
#                 cmd/modelgen run emits both outputs.
#   gen         — steady-state regen (idempotent); gen-models + gen-fakes + …
gen-models:
	GOWORK=off go run ./cmd/modelgen

# gen-models-check — drift gate for the generated model/interface layer
# (cmd/modelgen, driving the published framework-go-app-generator/modelgen), same
# shape as gen-temporal-check: regenerate, then fail on any diff. GOWORK=off-safe
# (see gen-models above), so it runs in CI (server-checks.yml).
gen-models-check: gen-models
	git diff --exit-code -- 'internal/**/contract.gen.go'

# gen-fakes — generate each contract's Fake<Iface> test double
# (<goPackage>/fake/fake.gen.go, cmd/modelgen driving the published
# framework-go-app-generator/modelgen's GenerateFakes). cmd/modelgen emits BOTH
# contract.gen.go and fake.gen.go from a single project.json read, so gen-fakes
# is an ALIAS of gen-models rather than a second invocation. Kept as its own
# target (not silently folded into gen-models) so gen-fakes-check can scope its
# diff to just the fake tree, and so the generated files' own header comment
# ("re-run `make gen-fakes`") names a real target.
gen-fakes: gen-models

# gen-fakes-check — drift gate for the generated Fake<Iface> test-double layer,
# same shape as gen-models-check: regenerate, then fail on any diff.
# GOWORK=off-safe (see gen-fakes above), so it runs in CI (server-checks.yml).
gen-fakes-check: gen-fakes
	git diff --exit-code -- 'internal/**/fake/fake.gen.go'

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

# gen-internal-tools-check — drift gate for the generated internal MCP tool
# catalog (cmd/internaltoolsgen), same regenerate-then-diff shape as the other
# gen-*-check gates. GOWORK=off-safe (see gen-internal-tools above), so it runs
# in CI (server-checks.yml).
gen-internal-tools-check: gen-internal-tools
	git diff --exit-code -- internal/resourceaccess/projectstate/toolcatalog.gen.go

gen: gen-models gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-uiprofiles

# gen-temporal — ONE appgen run (cmd/appgen, driving framework-go-app-generator's
# temporalgen + transportgen + configgen) that emits, from a single project.json
# read:
#   1. the per-manager Temporal layer (activities/invokers/worker .gen.go) for
#      the 5 managers,
#   2. the stdlib-only client SDK into ../systemtests/internal/sdk (gen-sdk),
#   3. the archistrator-server container config (cmd/server/config.gen.go) + the
#      systemtests harness env-name consts (gen-config).
# gen-sdk and gen-config are therefore ALIASES sharing this prerequisite rather
# than invoking appgen again; the three -check targets keep their distinct diff
# scopes. Release-backed and GOWORK=off like the other gen targets:
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

# gen-config — the archistrator-server container config (cmd/server/config.gen.go)
# + the systemtests harness env-name consts (../systemtests/internal/harness/
# envnames.gen.go) from project.json's deployment model (cmd/appgen, driving the
# published framework-go-app-generator/configgen — v0.4.0 shipped it, so the
# former workspace-only cmd/configgen bridge is folded into cmd/appgen tag-free
# and this is GOWORK=off like every other gen target). cmd/appgen emits it in the
# SAME run as the Temporal layer (one project.json read), so gen-config shares
# the gen-temporal prerequisite rather than invoking appgen a second time.
gen-config: gen-temporal

# gen-config-check — drift gate for the generated config + harness env-names,
# same regenerate-then-diff shape as the other gen-*-check gates. GOWORK=off-safe
# (see gen-config), so it runs in CI (server-checks.yml).
gen-config-check: gen-temporal
	git diff --exit-code -- cmd/server/config.gen.go ../systemtests/internal/harness/envnames.gen.go

# gen-main — the archistrator-server composition root (cmd/server/main.gen.go)
# from project.json's deployment model + service contracts (cmd/appgen, driving
# the published framework-go-app-generator/composegen — v0.5.0 shipped it, so this
# is GOWORK=off like every other gen target). cmd/appgen emits it in the SAME run
# as the Temporal layer + SDK + config (one project.json read), so gen-main shares
# the gen-temporal prerequisite rather than invoking appgen a second time.
gen-main: gen-temporal

# gen-main-check — drift gate for the generated composition root, same
# regenerate-then-diff shape as the other gen-*-check gates. GOWORK=off-safe
# (composegen ships in the pinned v0.5.0 app-generator release), so it runs in CI
# (server-checks.yml).
gen-main-check: gen-temporal
	git diff --exit-code -- cmd/server/main.gen.go

# gen-uiprofiles — the per-ActivityKind canonical lifecycle phase table
# (id/phase/name/weight, cmd/gen-uiprofiles) emitted into the sibling webApp
# module at ../webApp/src/components/construction/lifecycleTemplates.gen.ts,
# straight from internal/resourceaccess/projectstate/activityprofile.go's
# ProfileFor. No project.json read (Profile is a closed static enumeration),
# so this needs no `-project`/`-id` flags, unlike the other gen-* targets.
gen-uiprofiles:
	GOWORK=off go run ./cmd/gen-uiprofiles

# gen-uiprofiles-check — drift gate for the generated lifecycle-phase table in
# the sibling webApp module, same regenerate-then-diff shape as the other
# gen-*-check gates. The path is repo-relative so `git diff` works from the
# server working directory (server-checks.yml).
gen-uiprofiles-check: gen-uiprofiles
	git diff --exit-code -- ../webApp/src/components/construction/lifecycleTemplates.gen.ts

# Everything, including the model-gated integration tests (need a live, capable
# Ollama-backed worker — see internal/manager/systemdesign/integration_test.go).
# These hang without a real model, so do NOT run this in CI.
# Depends on claude-assets: several tests read the .claude prompt surface, which
# is materialized from the method-assets lib (not vendored), so it must be
# present before the suite runs.
test: claude-assets
	GOWORK=off go test ./...

# CI quality gate. `-short` makes the model-gated integration tests' TestMain
# skip (they require a live Ollama worker); everything else — including the
# fake-backed workflow gating tests — still runs. Depends on claude-assets (see
# `test`): the prompt-surface tests need the materialized .claude tree.
test-short: claude-assets
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

# Run the FULL Method conformance gate. With -tags methoddesign this runs the
# layer rules (TestMethodLayering) AND the DESIGN-rule + design↔code
# ALIGNMENT/conformance gate over the committed .aiarch/state/project.json
# (TestMethodDesignArtifacts), against the PUBLISHED framework-go (alignment
# wired since v0.4.1), which decodes the C4 container deployment model and
# cross-checks it against the built code. -count=1 defeats the test cache:
# TestMethodDesignArtifacts reads an out-of-tree file
# (../.aiarch/state/project.json), so a green run would otherwise cache and a
# later project.json edit would never re-run. This is the FULL gate — layer
# rules + design rules + alignment/conformance — and it is CI-enforced (see
# the "Method conformance gate" step in server-checks.yml).
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
