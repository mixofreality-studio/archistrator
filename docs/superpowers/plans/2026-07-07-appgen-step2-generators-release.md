# appgen Step 2: http/mcp Generators onto projectmodel + Release Mechanics

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Migrate `framework-go-http-generator` and `framework-go-mcp-generator` onto `framework-go-projectmodel` (deleting both embedded `contract/` copies), and complete the step-1 release follow-up: tags, server pins, appgen build-tag drop, GOWORK=off flip, CI drift gate, drift-guard test.

**Founder authorization:** push platform `main` + module tags to origin (2026-07-07). The archistrator `qa-gtd-pass_rearch` branch stays local.

**Spec:** docs/superpowers/specs/2026-07-06-app-generator-codegen-design.md (delivery step 2 + "release follow-up" earmarks).

## Global Constraints

- Multi-module tag format: `<module>/vX.Y.Z` (existing convention, e.g. `framework-go/v0.4.4`).
- Replace directives in dependency go.mods are IGNORED by consumers: every published module's `require` must name a real pushed tag. Standalone-dev `replace ... => ../framework-go-projectmodel` entries stay for in-repo builds.
- Migrations must be BYTE-IDENTICAL in output: http/mcp golden tests and archistrator's `make gen-client` regen must show zero diff. The projectmodel copies of contract.go/gotype.go/naming.go were verified logic-identical in step 1 (only package clause/doc changed), so this is a pure import swap.
- Platform work on branch `appgen-step2` (off main), merged to main after review, tags cut FROM MAIN post-merge. Archistrator work on branch `appgen-step2` off `qa-gtd-pass_rearch`.
- Per-task gates: module `GOWORK=off go test/vet + golangci-lint + gofmt`; archistrator tasks also boot the app + real Chrome check.
- One naming decision, made here: the generators' internal references switch from `contract.X` to `projectmodel.X` (import `projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"`). No type aliases, no shim package — callers (httpgen/mcpgen/clientgen/mcpemit) update their imports.

### Task 1 (controller): push platform main + tag projectmodel
- [ ] `git -C archistrator-platform push origin main`
- [ ] `git -C archistrator-platform tag framework-go-projectmodel/v0.1.0 main && git push origin framework-go-projectmodel/v0.1.0`
- [ ] Verify remote resolution: from a temp dir, `GOPROXY=direct GOWORK=off go mod download github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel@v0.1.0` (or equivalently proceed and let Task 2's tidy prove it).

### Task 2: app-generator require bump + tag
**Files:** archistrator-platform/framework-go-app-generator/{go.mod,go.sum}
- [ ] On branch appgen-step2: change `require framework-go-projectmodel v0.0.0` → `v0.1.0` (keep the replace). `GOWORK=off go mod tidy && GOWORK=off go test ./...` (still resolves locally via replace; tidy records the tag in go.sum — verify go.sum gains real hashes by temporarily moving the replace? NO: with replace present, tidy may not add sums. Correct verification: `GOFLAGS=-mod=mod GOWORK=off go list -m all` in a consumer context happens in Task 5; here just ensure the require names v0.1.0).
- [ ] Commit `app-generator: require projectmodel v0.1.0 (published tag)`. Review-lite (controller eyeballs the 2-line diff — no subagent needed). Merge to main, push, tag `framework-go-app-generator/v0.1.0`, push tag.

### Task 3: http-generator onto projectmodel
**Files:** delete framework-go-http-generator/contract/ (3 files); modify go.mod (+require projectmodel v0.1.0, +replace for dev), httpgen/*.go imports (contract.→projectmodel.), any tests importing contract.
- [ ] Swap imports; delete embedded package; `GOWORK=off go mod tidy`.
- [ ] Gates: `GOWORK=off go test ./...` — ALL GOLDENS BYTE-IDENTICAL (no -update permitted); vet, golangci-lint, gofmt clean.
- [ ] Grep: zero remaining references to `framework-go-http-generator/contract`.
- [ ] Commit `http-generator: consume framework-go-projectmodel (embedded contract/ copy deleted)`.

### Task 4: mcp-generator onto projectmodel
Same recipe as Task 3 for framework-go-mcp-generator (contract/ deleted; mcpgen + tests re-imported; goldens byte-identical; commit).

### Task 4b (controller): platform review + merge + tags
- [ ] Task-review Tasks 3+4 diffs (one reviewer), merge appgen-step2 → main, push, tag + push `framework-go-http-generator/v0.2.0` and `framework-go-mcp-generator/v0.2.0`.

### Task 5: archistrator pins + import swaps + release flips
**Files:** server/go.mod+go.sum; server/cmd/clientgen/main.go + internal/mcpemit/mcpemit.go (imports → projectmodel); server/cmd/appgen/main.go (drop `//go:build appgen`); server/Makefile (gen-temporal → `GOWORK=off go run ./cmd/appgen`, fold gen-temporal into `gen:`, keep gen-temporal-check); .github/workflows/server-checks.yml (enable the commented-out drift step, now GOWORK=off-safe).
- [ ] go.mod: http/mcp generators → v0.2.0; add projectmodel v0.1.0 + app-generator v0.1.0. `GOWORK=off go mod tidy` (fetches pushed tags from origin — proves the release chain).
- [ ] Import swaps in clientgen + mcpemit (httpcontract/mcp contract → projectmodel). NOTE clientgen hands raw serviceContracts entries to Parse — the projectmodel `Parse` is the same function; `httpgen.Generate` now takes projectmodel types.
- [ ] `make gen-client` (GOWORK=off) → `git diff --exit-code` on internal/client + api/openapi.yaml: BYTE-IDENTICAL.
- [ ] `make gen-temporal && make gen-temporal-check` under GOWORK=off: zero diff.
- [ ] Gates: full `GOWORK=off go build ./... && go test ./... && go vet ./...`; lint on touched dirs. Boot app + Chrome check (controller).
- [ ] Commit `appgen: publish-pinned generators (projectmodel-backed), GOWORK=off gen-temporal, CI drift gate live`.

### Task 6: drift-guard contract test
**Files:** create server/internal/projectmodelguard_test.go
- [ ] Test (spec "Schema ownership" promise): locate repo root (ascend from cwd, the methoddesign-gate pattern), read `.aiarch/state/project.json`, run `projectmodel.Load` — assert no error; `t.Logf` each Warning (do NOT assert empty — warnings are advisory). This makes any writer-side shape change that breaks the codegen subset fail archistrator's own CI.
- [ ] Gates + commit `appgen: projectmodel drift-guard — writer output must Load under the published parser`.

### Wrap: final review-lite (single reviewer over both repos' step-2 diffs), merge archistrator appgen-step2 → qa-gtd-pass_rearch (founder's established pattern), branches deleted. qa-gtd branch stays unpushed.
