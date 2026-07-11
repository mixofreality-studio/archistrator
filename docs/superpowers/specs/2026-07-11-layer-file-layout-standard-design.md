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
- **Hard constraint:** registered Temporal activity names must not change —
  in-flight workflow histories reference them. `internal/arch_activitynames_test.go`
  is the stability harness and must stay green throughout. The generator must be
  able to emit the exact stable names currently registered via `CustomActivities`.

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
