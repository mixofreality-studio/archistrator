# Layer-Package File Layout Standard — Design

**Date:** 2026-07-11
**Status:** Approved by founder (this session)
**Scope:** archistrator `server/internal` layer packages + `archistrator-platform` (`framework-go/arch`, `temporalgen`)

## 1. Motivation

Layer packages have drifted into freeform multi-file layouts: systemdesign's manager
package alone has ~50 files, workflows for one manager share a single `workflow.go`,
tests are scattered across ~20 `*_test.go` files per package, and four managers carry
handwritten Temporal activities (`activities_custom.go`, `gitactivities.go`,
`gitrail.go`, `reviewledger.go`) that exist only because temporalgen cannot yet
generate them. This standard closes the file set per layer package so structure is
predictable, navigable, and machine-enforceable — for archistrator itself and for
every consumer repo the app generator emits.

## 2. The Standard (normative)

Binds every **leaf layer package** under `server/internal/{manager,engine,resourceaccess,client}/`
(and, via the platform arch check, the same layer trees in generated consumer repos).

The standard governs **handwritten `.go` files only**. Exempt: generated files
(`*.gen.go`), `contract.schema.json`, and non-Go assets (`schema.sql`, `assets/`
directories, etc.).

Allowed handwritten files per package — nothing else:

### Rule 1 — one contract-implementation file

`<pkg>manager.go` / `<pkg>engine.go` / `<pkg>access.go` / `<pkg>client.go`
(e.g. `billingmanager.go`, `estimationengine.go`, `projectstateaccess.go`).

Holds **all** methods of the contract implementation, plus everything shared across
workflows or not specific to one workflow: the package doc (today's `contract.go`
narrative), deps wiring, error mapping, adapters, signal handlers, the worker
manifest, prompt corpora, codecs, git rail/session mechanics, strategy tables and
strategy implementations, behavior free-functions. Large files are an accepted
consequence — `projectstateaccess.go` folds ~40 source files;
`systemdesignmanager.go` is similar scale.

Naming migration note: engine impl files today are `<pkg>.go` (`estimation.go`);
they rename to `<pkg>engine.go` etc. so the stereotype is explicit and checkable.
RA impl files (`projectstate.go`, `sourcecontrol.go`) rename to `<pkg>access.go`.

### Rule 2 — one file per Temporal workflow (managers only)

Filename = lowercased workflow **function** name with the `Workflow` suffix
stripped: `DeployWorkflow` → `deploy.go`, `PumpNextActivityWorkflow` →
`pumpnextactivity.go`, `CoAuthorArtifactWorkflow` → `coauthorartifact.go`.

Each workflow file declares **exactly one** function taking `workflow.Context`
plus helpers used only by that workflow. Code shared by two or more workflows
moves up into the contract-implementation file (Rule 1).

The registered Temporal workflow name must stay aligned with the function name;
the existing manifest tests gain an assertion that the registered leaf name equals
the lowercased function stem.

### Rule 3 — one test file per package

`manager_test.go` / `engine_test.go` / `access_test.go` / `client_test.go`.

All existing scattered test files merge into it. Build-tagged integration tests
(e.g. `durableexecution/temporal_integration_test.go`) lose their build tag and
convert to env-guarded `t.Skip` inside the single test file, so the rule has no
carve-outs. Module-root and `internal/`-root test files (`arch_test.go`,
`method_design_test.go`, …) are not in layer packages and are unaffected.

### Rule 4 — no other files

No `deps.go`, `errors.go`, `prompts.go`, `codec.go`, `dispatch.go`, `catalog.go`,
`gitrail.go`, `variant.go`, `behavior.go`, `strategy.go`, … as separate files.
Everything folds into Rules 1–2 files.

### Rule 5 — no handwritten Temporal activities

No `activities_custom.go`, no activity methods in any handwritten file, no
`CustomActivities` manifest entries. Every Temporal activity is generated
(`activities.gen.go`). Engine methods are pure compute — workflow bodies call
them **directly**, never through an activity (already today's doctrine:
"deliberately NOT Activities"). If a manager needs an I/O operation that has no
generated activity, the fix is to promote the operation into the owning RA's
contract so temporalgen generates it — never to handwrite a wrapper.

## 3. temporalgen Extension (enables Rule 5)

Current custom-activity inventory (all wrap genuine I/O that temporalgen has no
contract to generate from):

| Manager | Files | Count | What they wrap |
|---|---|---|---|
| billing | `activities_custom.go` | 3 | revenue-ledger seam ops |
| construction | `activities_custom.go`, `gitactivities.go` | 8 + 6 | projectEnvelope reads, constructionTransition head-state writes, git head-state writes |
| projectdesign | `activities_custom.go`, `gitrail.go`, `reviewledger.go` | 6 + 1 + 2 | envelope/branch reads, stage/commit/reject/withdraw artifact writes, rail + ledger ops |
| systemdesign | `activities_custom.go`, `gitrail.go`, `reviewledger.go` | 7 + 1 + 2 | same shape as projectdesign + reconcile |

Approach — root-cause, schema-first:

- **Promote** the underlying operations into the owning RA contract schemas
  (projectstate transition verbs, envelope/version/branch reads, revenue-ledger
  ops) so `activities.gen.go` emits their activities, including idempotency-key
  derivation (the generator already emits `genActivityIdempotencyKey`).
- **Dissolve** manager-local pure translation (the `projectEnvelope` codec,
  seam/envelope arg bundling) into workflow-side pure code or contract types —
  whichever the per-activity detailed design finds; this is plan-level work with
  a recon step per activity.
- **Hard constraint (as ratified — supersedes the freeze below):** the original
  "activity names must never change" freeze was unimplementable given the shape
  the promotions actually took (§8a) and was ratified away during execution.
  Registered Temporal **workflow** names are frozen — the 20 externally-referenced
  names in Global Constraints, asserted verbatim by
  `TestRegisteredTemporalNamesGolden_FrozenWorkflowNames`. Registered **activity**
  names MAY change (clean cut): the promoted/deleted activities land under new
  contract-qualified names (`designSessionAccess.stageArtifactForReviewOnBranch`,
  etc.), and the full registered-name set — workflows + activities — is asserted
  against a committed golden list in `internal/registered_names_test.go`
  (`TestRegisteredTemporalNamesGolden`). A deliberate rename is a reviewable
  one-line diff against that golden, not silent drift. This is the new stability
  mechanism; `internal/arch_activitynames_test.go` remains in force for its
  original purpose (method-value invocation + no hand registration) but is not
  itself the activity-name freeze. **Consequence:** any in-flight Temporal
  workflow execution histories from before this release reference the
  now-renamed activity names by string; they must be drained (allowed to
  complete under the OLD worker) before a worker built from this release is
  deployed. Temporal Schedules resume automatically post-drain — no operator
  action needed there.

## 4. Enforcement — `framework-go/arch` FileLayout check

New check in the platform `arch` package, consumed by archistrator's existing
`arch_test.go` (`appArchSpec()`) and inherited by generated consumer repos via
their arch harness:

- Walks each leaf layer package and asserts the allowed file set of §2
  (handwritten `.go` files only; `*.gen.go` and non-Go files ignored).
- **Workflow rule:** any handwritten file declaring a func with a
  `workflow.Context` parameter must declare exactly one such func and be named
  `tolower(funcName - "Workflow") + ".go"`; such files may exist only in manager
  packages.
- **Activity rule:** `RegisterActivity*` calls and manifest `CustomActivities`
  entries are forbidden outside `*.gen.go`.
- **Test rule:** the only `_test.go` file permitted is `<stereotype>_test.go`.

Platform release + archistrator pin bump required (same two-repo coordination as
the lint-gate effort).

## 5. Migration (big-bang, gate lands last)

1. **Platform first:** temporalgen extension + FileLayout check (dormant — not yet
   wired into `MethodSpec` defaults), release, pin bump in archistrator.
2. **Per-package migration** in order engines → resourceaccess → clients →
   managers (managers last, since the activity regen lands there). Folding,
   workflow-splitting, and test-merging are **move-only commits** (no behavior
   change); the custom-activity → generated-activity swap is the only behavioral
   change and is isolated in its own commits.
3. **Flip the gate on** in `arch_test.go`; the whole tree must be green — no
   waiver list.
4. **Verify:** full `GOWORK=off go test ./...`, the methoddesign gate, the
   activity-name stability test, and a local app boot exercising a real workflow.

## 6. Verification strategy

- Move-only folds are proven by the unchanged test suite passing at every step.
- temporalgen changes get golden tests in the platform repo (existing pattern).
- The FileLayout check gets red/green unit tests in the platform, plus a
  deliberate red run against pre-migration archistrator to prove it detects
  violations.
- Registered activity/workflow name stability asserted by
  `arch_activitynames_test.go` + the new manifest name-alignment assertions.
- Final: local boot per the run-app-locally procedure; execute a workflow
  end-to-end against real state.

## 7. Accepted costs / non-goals

- Very large single files (`projectstateaccess.go` in the tens of thousands of lines).
- Two-repo release coordination.
- No waiver mechanism: the gate is absolute from the moment it flips on.
- webApp/TypeScript layout is out of scope (covered by the TS layer-enforcement
  package separately).

## 8. Outcome amendments (2026-07-11 execution)

Recorded post-hoc, after the migration executed and the gate flipped on
(framework-go v0.5.2, `TestFileLayout` green, methoddesign green) and the D2
verification sweep closed it out. Each item is a deviation from — or a
clarification of — the design above, as actually built.

**(a) Clean-cut + drain, ratified.** §3's original activity-name freeze
assumed the promoted activities could keep their old names. They couldn't:
promoting an operation into an RA contract gives it a new contract-qualified
registered name (`<contractKey>.<opName>`), and activity *payloads* changed
shape too (envelope args replaced ad hoc arg bundles). The founder ratified a
clean cut instead of a name-preserving shim: activity names and payloads
change; **workflow names are frozen** (the 20 external names, Global
Constraints); the **registered-names golden test**
(`internal/registered_names_test.go`) is the stability mechanism going
forward — `TestRegisteredTemporalNamesGolden` pins the full set,
`TestRegisteredTemporalNamesGolden_FrozenWorkflowNames` pins the 20 workflow
names verbatim. Operational consequence: **in-flight workflow executions
must be drained before deploying this release** — their histories reference
the old activity names/payload shapes by string, which the new worker no
longer registers. Temporal Schedules resume automatically once drained; no
manual re-registration needed.

**(b) temporalgen needed no generation extension.** §3 designed a
temporalgen extension to let the generator emit activities for promoted
operations. It wasn't needed: every promotion rode the *existing*
contract-backed RA component machinery — `constructionTransitionAccess`,
`gitActivityStatusAccess`, `designSessionAccess`, `revenueLedgerAccess`, and
`sourceControlAccess.syncManagedScaffold` — which `activities.gen.go`
already generates activities for once an op is declared on a contract
schema. The only generator-adjacent change across the whole migration was
**deleting `CustomActivities`** (the manifest hook for handwritten
activities) now that nothing declares any. Rule 5 (§2) is enforced with zero
platform changes.

**(c) Multi-component-per-package contract tooling.** Promoting operations
onto RA contracts that share a package (e.g. `projectstate`) needed contract
schemas to live one-per-component rather than one-per-package:
`contract.<key>.schema.json` (e.g. `contract.designSessionAccess.schema.json`,
`contract.constructionTransitionAccess.schema.json`,
`contract.gitActivityStatusAccess.schema.json`,
`contract.revenueLedgerAccess.schema.json`). `modelgen` merges every
`contract.*.schema.json` in a package into that package's generated models,
and a package with multiple contract files nominates a **sticky primary**
(the pre-existing single-contract file, when one exists) so codegen output
for the original component doesn't reshuffle just because siblings were
added alongside it.

**(d) `ProjectEnvelope` is the shared wire projection.** `projectstate.go`
gained two envelope types: `ModelEnvelope` (opaque `oneOf`-style wire
wrapper for the artifact model union — replaces the narrower `ArtifactModel`
on ops that need it, e.g. `StageArtifactForReviewOnBranch`) and
`ProjectEnvelope` (the shared read-side projection multiple managers
consume; its construction-related sections are `omitempty` for
managers/call sites that don't need them). The `Stage` op signature takes
`ModelEnvelope` over the wire; the decode into the concrete typed model now
happens **inside the RA** (`designSessionAccess.StageArtifactForReviewOnBranch`),
not in the manager — keeping the manager layer free of wire-format
knowledge.

**(e) Rule 2 semantics, clarified in practice.** One `*Workflow`-suffixed
**entry** function per workflow file, filename derived from that entry
function (§2 Rule 2 as written). What wasn't explicit in the original
design and had to be settled during execution: context-taking helper
functions (`func(ctx workflow.Context, ...)` that are NOT themselves the
registered entry point) are legal *inside* a workflow file when they're
private to that one workflow — the Rule-2 "exactly one such func" language
means exactly one func matching the *entry* shape (the one temporalgen
registers), not a ban on any other function that happens to take a
`workflow.Context`. Such a context-taking helper is **forbidden in the
Rule-1 impl file** (`<pkg>manager.go`) — that file is workflow-agnostic by
design, and the gate rejects any `workflow.Context`-taking func found
there. Whether a shared helper can move to the impl file therefore turns
on its signature: a helper shared by two or more workflows that is
**context-free** moves up into the impl file by convention (as §2 Rule 2
already said), authored as an ordinary Go function. A helper shared by two
or more workflows that **takes** `workflow.Context` cannot move to the
impl file at all — it stays in Go source, living in its *first caller's*
workflow file, marked with a one-line convention comment noting which
other workflow file(s) also call it, rather than being duplicated per
workflow file.

**(f) Single test file rule — dual test packages folded.** Some packages
had both an in-package (`package foo`) and an external (`package foo_test`)
test file. Rule 3 ("one test file per package") is satisfied by folding
both **into the in-package test file** — `package foo_test` files are
absorbed in-package rather than kept as a second file. This loses nothing:
the exported-surface discipline (external test files exist to prove the
package's exported API is sufficient) is kept as a *convention* enforced by
review, not by a second physical file. No gate encodes it; it's a norm.

**(g) Platform + pin state at close.** Platform releases produced by this
effort: `framework-go` v0.5.0 → v0.5.1 → v0.5.2 (FileLayout check landed
dormant in v0.5.0, activated/hardened through v0.5.2), and
`framework-go-app-generator` v0.6.0 → v0.6.1. archistrator's `server/go.mod`
pins `framework-go v0.5.2` and `framework-go-app-generator v0.6.1`; the
`FrameworkGoVersion` const consumed by generated consumer repos'
`sourceControlAccess` (`internal/resourceaccess/sourcecontrol/sourcecontrolaccess.go`)
is `"v0.5.2"` — new consumer repos scaffolded after this release inherit the
FileLayout gate automatically via that pin, with no waiver list available to
them either.
