# Appendix C Conformance Gate — Design Spec

**Date:** 2026-06-21
**Status:** Approved (design); pending spec review
**Repos touched:** `archistrator-platform` (framework-go — the reusable checks) and `archistrator` (the dogfood project — wiring, generated files, hooks)

## Problem

We want the architecture of any archistrator-built project to be **provably valid** against
the design rules in Juval Löwy's *Righting Software* Appendix C (the Design Standard,
`software/methodpoc/rightingsoftware/OEBPS/xhtml/appc.xhtml`) AND to keep the **Go code
aligned with the system design** held in project state. Today this is only partially true:

- `framework-go/arch` enforces code-side layer rules (single-layer classification,
  downward-only imports, Temporal isolation, interface-suffix per layer, error-last,
  dependency allowlist) via `go test`.
- `framework-go/methodcheck` reads `.aiarch/state/project.json` and runs design rules
  (`ValidateVolatilities`, `ValidateCoreUseCases`, `ValidateArchitecture`,
  `ValidateOperationalConcepts`, `ValidateStandardCheck`) plus a design↔code **alignment**
  pass. `methodcheck.Check(t, ProjectSpec)` already unifies design rules + `arch.Check` +
  alignment behind one call.

Three gaps remain:

1. **Contract↔interface conformance is unchecked.** 36 hand-authored contracts in
   `server/cmd/seed-contracts/data/*.json` declare each component's ops (with structured
   `inputs`/`outputs` carrying real Go types), but nothing forces the implemented Go
   interface to match the contract.
2. **Appendix C coverage is partial and unaudited.** Some rules are ported into
   `methodcheck`; there is no traceability matrix saying which Appendix C items are
   automated, which are checkable from design/contracts, and which are human-judgment only.
3. **Many interaction rules are only checkable on the design graph, not code.** Call mode
   (sync vs queued) and pub/sub are encoded in `project.json` but not in Go imports, so the
   §3.6 interaction don'ts can only be enforced against `.aiarch/**/*.json`.

## Decisions (settled during brainstorming)

| Decision | Choice |
|---|---|
| Contract→code direction | **Generate** the Go interface + DTOs from the contract JSON; the agent/dev writes method bodies; a drift check fails if the generated file is stale vs the contract. |
| Strictness | **Directives fail, guidelines warn + waiver.** Maps onto existing `Severity`: directives → `SeverityError` (fails), guidelines → `SeverityWarning` (logs; a recorded waiver suppresses it). Directives cannot be waived. |
| Home | **`archistrator-platform/framework-go`** — reusable across any archistrator project. |
| Triggers | Gate runs on **`server/**/*.go`** changes AND **`.aiarch/**/*.json`** changes, via both a local hook and CI. |
| Method signature shape (Fork B1) | `Op(ctx context.Context, req <ReqStruct>) (<RespStruct>, error)`, generated from structured `inputs`/`outputs`; `ctx` first, `error` last (matches `arch` Rule 4). The free-text `signature` becomes a *checked* field — a rule fails if it disagrees with the structured inputs/outputs. |
| Codegen scope (Fork B2) | Generate **DTOs for all layers**; generate **interfaces only for Manager / Engine / ResourceAccess**. Clients get DTOs + an optional handler-port interface (off by default). |

## Goals

- Every Appendix C item is either machine-enforced or explicitly classified as
  human-judgment, in a living traceability matrix.
- The Go interface for every Manager/Engine/ResourceAccess component is **generated from
  its contract** and cannot silently drift from it.
- A single gate (`methodcheck.Check`) enforces, on every server/go change and every
  `.aiarch` state change: design rules, design-graph interaction rules, contract metrics,
  layer naming, cardinality, code-side layer rules, contract drift, and design↔code
  alignment.
- The build breaks only on directive-class violations (Löwy's "certain to cause failure");
  guideline-class violations warn and are triaged via recorded waivers.

## Non-goals

- No re-architecting of existing components to satisfy guideline warnings (those are triaged
  deliberately, not auto-fixed).
- No generation of method *bodies* — only interfaces + DTOs. Implementation stays
  agent/hand-written.
- No new design-state schema; we reuse `project.json`'s System component model and the
  standard-check slot's waiver concept.

## Design

### Part A — Appendix C coverage matrix (the "strict analysis")

A living artifact `framework-go/methodcheck/APPENDIX-C-COVERAGE.md` plus a machine-readable
table the rule engine iterates. Each of the ~80 Appendix C items gets:

- `appcRef` — stable id, e.g. `C.3.2.a` (System Design Guidelines → Cardinality → a).
- `kind` — `directive` | `guideline` (from the Appendix C prose: §C.2 + Prime Directive are
  directives; the numbered guideline sections are guidelines).
- `classification` — `automated-code` | `automated-design` | `automated-contract` |
  `human-judgment`.
- `ruleId` — the `methodcheck`/`arch` rule that enforces it, or `—` for human-judgment.

This doc is both the deliverable analysis and the spec-of-record for what the engine must
check. Items marked `human-judgment` (e.g. "Capture required behavior, not functionality")
are listed so coverage is honest, not silently omitted.

### Part B — Contract → Go codegen + drift gate

New package **`framework-go/contractgen`** (library) with a thin binary
`framework-go/contractgen/cmd/contractgen`.

**Input:** a contracts directory (`server/cmd/seed-contracts/data/*.json`) + a type-import
map resolving qualified type prefixes to import paths (`uuid` → `github.com/google/uuid`,
`fwmgr` → `…/framework-go/manager`, etc.). Built-ins (`[]byte`, `string`, `uuid.UUID`) need
no body; qualified types are emitted with their import.

**Output (in the server repo, beside each component):**
`server/internal/<layer>/<component>/<component>_contract_gen.go`, header
`// Code generated by contractgen; DO NOT EDIT.`, containing:

- one request struct per op `inputs[0]` and one response struct per non-error `outputs[*]`
  (fields + Go types taken verbatim from the contract);
- for Manager/Engine/ResourceAccess: the component interface, one method per op:
  `Op(ctx context.Context, req <ReqStruct>) (<RespStruct>, error)`.

**Conformance guarantee = the Go compiler.** The impl package embeds / asserts the generated
interface (`var _ <Iface> = (*impl)(nil)`). If the hand/agent-written impl doesn't satisfy
the contract, the package does not compile. No fuzzy AST matching.

**Drift check.** `contractgen.CheckDrift(t, opts)` (runs as `go test` in the server)
regenerates every `*_contract_gen.go` into memory and diffs against the committed bytes;
any difference fails. So editing a contract without regenerating — or hand-editing a
generated file — breaks CI. A `//go:generate contractgen …` directive keeps regeneration
one command away.

**Bonus rule (Fork B1 enforcement):** a contract whose free-text `signature` disagrees with
its structured `inputs`/`outputs` (op name, arity, types) fails — catching authoring drift
*inside* the contract.

### Part C — Rule engine extensions (`framework-go/methodcheck`)

Add new rule predicates over the already-decoded `project.json` System component model
(which carries layer, inbound/outbound, **call mode**, pub/sub, and ops). Each rule emits a
`Finding{RuleID, Severity, Message, Location}` tagged with its `appcRef`. Severity follows
the directive/guideline mapping.

New design-graph rules (all `automated-design`, runnable on `.aiarch` change alone):

- **Interaction don'ts (§3.6, directive-class → `SeverityError`):** Client → ≤1 Manager per
  use case; Manager queues to ≤1 Manager per use case; Engines/RA do not *receive* queued
  calls; Clients/Engines/RA/Resources do not *publish*; Engines/RA/Resources do not
  *subscribe*.
- **Closed-architecture call directions (§3.4):** no call up; no sideways-sync except queued
  Manager→Manager; no layer skips — checked on the design graph (complements `arch`'s
  import-graph check of the same on code).
- **Cardinality (§3.2, guideline-class → `SeverityWarning`):** ≤5 Managers *unless
  subsystems are declared*; golden Engine:Manager ratio; ≤ a handful of subsystems; ≤3
  Managers/subsystem.
- **Layer naming:** Engine names are gerunds, ResourceAccess names end `Access`, Manager
  names end `Manager` — vs declared layer (catches e.g. `artifactRenderingAccess` declared
  `layer: Engine`).
- **§6 Service Contract metrics (`automated-contract`):** avoid single-op contracts; strive
  3–5 ops; avoid >12 ops; **reject ≥20 ops** (this one is directive-class); ≤2 contracts per
  service; flag 0-op/degenerate contracts. Runs off the System ops in `project.json`.

These reuse the existing verb-orchestration pattern in `validate.go` (run a verb only when
its prerequisite slots are present) and the existing `Finding`/`Severity`/`Verdict` types in
`finding.go`. No new gate entrypoint — they fold into `ValidateProject` so
`methodcheck.Check` picks them up automatically.

### Waivers

A guideline (`SeverityWarning`) violation is advisory and already never fails the build.
Waivers exist to **suppress known/accepted warnings** so the warning list stays signal. We
reuse the standard-check slot's existing waiver concept (already validated by
`ValidateStandardCheck`): a waiver entry keyed by `ruleId + component` with a required
`reason`. A matching waiver downgrades a `SeverityWarning` finding to `SeverityInfo` (logged,
not surfaced as a warning). **Directive (`SeverityError`) findings ignore waivers** — they
cannot be silenced.

### Wiring — "must pass after every change"

One entrypoint, two triggers, two surfaces.

**Entrypoint:** `methodcheck.Check(t, ProjectSpec{RepoRoot, Arch})` already runs design
rules (incl. all Part C additions) + `arch.Check` + alignment. Add `contractgen.CheckDrift`
to the same server test file (or call it from within the server's `methodcheck` test). Expose
as `make method-check` in `server/Makefile`.

**CI:**
- `server/.github/workflows/server-checks.yml` — add the gate step alongside
  `gofmt/vet/build/test-short`. Triggers already cover `server/**`.
- `aiarch-construct.yml` — its server-checks step runs the gate, so **agent-built code is
  gated before the PR opens**.
- `framework-go/.github/workflows/platform-checks.yml` — **turn on `go test`** (today it
  only builds; the platform's own rule tests never run in CI).
- Add an `.aiarch/**/*.json` path trigger so editing project state alone re-runs the
  design-graph + contract rules (the gate runs even with no Go change).

**Local hook (PostToolUse):** mirror the existing `.claude/hooks/validate-structurizr.sh`.
- On Edit/Write of `server/**/*.go` → run the code-side gate (drift + arch + alignment).
- On Edit/Write of `.aiarch/**/*.json` (and `seed-contracts/data/*.json`) → run the
  design-graph + contract-metric gate.
Fast feedback in-session; CI remains the authority.

## Testing strategy

- `contractgen`: golden-file tests (contract JSON → expected generated Go) + a drift test
  that mutates a contract and asserts `CheckDrift` fails.
- New `methodcheck` rules: table-driven tests mirroring `rules_test.go`, each with a
  passing and a violating fixture under `methodcheck/testdata`, asserting `RuleID` +
  `Severity`.
- Coverage matrix: a test asserts every `appcRef` in the matrix is either bound to a
  registered rule or explicitly `human-judgment` (no silent gaps).
- Waivers: a test that a guideline warning is suppressed by a matching waiver and that a
  directive error is not.

## Rollout

1. Land Part A matrix + Part C rules in `framework-go` (warnings only at first — nothing
   new fails yet because guideline findings are `SeverityWarning`).
2. Land `contractgen` + generate `*_contract_gen.go` for existing components; add drift test
   and conformance assertions. (Directive-class; must pass.)
3. Wire CI triggers + the PostToolUse hook.
4. Triage the known guideline warnings (6 Managers, golden ratio, single-op contracts,
   `artifactRenderingAccess` naming, off-range op counts) — fix or waive each with a reason.

## Known guideline violations to triage (already present)

- §3.2.a — 6 Managers (violation unless subsystems declared).
- §3.2.d — ~10 Engines vs 6 Managers (golden ratio inverted).
- §6.2.a — 4 single-op contracts (`C-PE`, `autoscalerEngine`, `constructionEstimationEngine`,
  `handOffEngine`), all FROZEN.
- §6.2.b — ~11 contracts outside the 3–5 op band.
- Layer naming — `artifactRenderingAccess` declared `layer: Engine`.
- `operationsRead-ruling.json` — 0 ops (degenerate contract).
