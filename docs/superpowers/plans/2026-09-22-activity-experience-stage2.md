# Activity Experience — Stage 2 (One Lifecycle Source, One Review Engine, the Design Prefix) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the method-assets lifecycle data the ONLY source of per-activity-type lifecycles (retiring the Go `profileRows` tables and `gen-uiprofiles`), make `reviewEngine` the one place that decides who reviews AND whether a human must (absorbing `EffectiveGate`, the Manager's kind table and the design rails' `Preset == vibes` reads, with the M0 spend floor), and put `requirements → architecture → projectDesign → M0` into the derived plan as real activities that the pump refuses to dispatch until stage 4.

**Architecture:** Part A (Tasks 1–4) re-points every reader of the retiring tables at `methodassets.LifecycleFor` through an adapter that preserves the generated `Profile` shape, so the linear workflow, earned value and the construction console are byte-identical (proven by keeping `gen-uiprofiles-check` alive one task longer), then makes the clean cut. Part B (Tasks 6–10) appends three `ActivityType` values, generalizes `ProposeReviews` to take activity type + lifecycle phase + policy and return `RequiresHuman`, re-points the three callers, extends `DerivePlan` with the design prefix, and classifies/backfills/serves the prefix while the pump refuses it. No behaviour change on the rails' command sequences; no new Temporal workflow or activity.

**Tech Stack:** Go 1.26 (`GOWORK=off` always), method-assets v0.9.0, project.json contracts → modelgen/clientgen/appgen, Temporal replay fixtures, React 19 + TypeScript, `node:test`.

**Spec:** `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` (§3, §4, §5.1, §5.4, §6, §8 stage-2 row as amended 2026-09-21). Executors read both.

## Global Constraints

- `GOWORK=off` on every `go`/`make` command in `server/` (prefix `make lint` / `make test-short` in a worktree). Gates run against PINNED platform tags, never a `replace`.
- Work in the git worktree `.claude/worktrees/activity-stage2` (branch `activity-experience-stage2`, from `main` @c10ba4d5). The main checkout is shared with other sessions.
- `.serviceContracts` in `.aiarch/state/project.json` IS hand-edited, followed by the self-amendment loop: `make gen-models` (+ `gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-main`) → `make method-check` → `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System` → `GOWORK=off make test-short` → `cd webApp && npm run gen:api && npm run gen:ops && npm run check`. State slots 9/10 are NEVER hand-edited — `make derived-plan-write`. Slot 5 (System) relationship additions are permitted ONLY where a task names them (Task 8: the two `*-design-manager → review-engine` edges); any wider slot-5 finding is a STOP (that is stage 1).
- Never weaken, skip or allowlist around a gate; no `//nolint` for complexity findings (decompose). If a gate is red, fix the cause or stop and report.
- Never run `git restore`, `git clean`, `git stash`, `git checkout -- <path>` or any tree-wide reset. Back up the gitignored SDD ledger to the session scratchpad after every append.
- `TestFileLayout`: one impl file + one file per workflow + ONE test file per package; no new `.go` files inside existing packages. `arch_bannedphase_test.go` bans the bare identifier `phase`. `TestGeneratedOnlyPublic` / encapsulation allowlists change only where a task names the entry.
- `ActivityType` ordinals are APPENDED (7 Requirements, 8 Architecture, 9 ProjectDesign), never renumbered; every copy of the `$def` moves together; `make sumtype-check` must stay green (no `default:` arms added to dodge it).
- Lifecycle type key rule (one production home after Task 1): `t.String()` for non-testing types, `"testing:" + v.String()` for testing (QA is `testing:qaProcess`).
- Behaviour parity on the rails: vibes = auto, checkpoints/full = human on the design rail; construction gating identical to today's `EffectiveGate`; the projectDesign `sdp` gate always human. Pinned by table tests. The 13 replay fixtures in `server/internal/manager/construction/testdata/replay` stay green; `registered_names_test.go` golden unchanged.
- A changed Manager/Engine op also needs: clientgen `mcpdocs` op-doc table, webApp `gen-enums OUTPUT_NAMES` for new string enums, `cmd/server/managerlog.go` if a Manager interface changes.
- webApp layer DAG (routes → containers → components → hooks → api) is lint-enforced; construction graph/list/detail goldens must pass unedited unless a task proves the rendered data identical.
- Drift gates before every commit touching generated inputs: every `gen-*-check` (incl. `gen-lifecycles-check`; `gen-uiprofiles-check` until Task 4 deletes it), `sumtype-check`, `derived-plan-check`, `encapsulation-check`, `method-check`, `fix-check`.
- Match surrounding comment density, naming and idiom. Commit messages end with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- Out of scope: dispatching design activities (stage 4), the LIST-lens Table 11-1 ordering (stage 5), the method-assets `the-method-review-routing` SKILL.md drift (earmark; a platform release is its own founder STOP).

## Task order

Tasks 1 → 2 → 3 → 4 (Part A), then 6 → 7+8 (one commit) → 9 → 10 (Part B). Task 6 is independent of Part A and may run in parallel with Task 2 only if no shared files (Task 6 touches `ActivityType` in projectstate + contracts; Task 2 is webApp-only).

---

### Task 1: One production home for the lifecycle-key rule

Stage 0 wrote the key rule twice on purpose — `lifecycleTypeKey` in `manager/construction/constructionmanager.go` (production, feeding `methodassets.LifecycleFor`) and an identical test-only copy in `projectstate/access_test.go` (the parity test could not import a `_test.go` symbol from another package). Stage 2 makes the data the only source, so the rule that names the data must have exactly one home, and that home is beside `CommandFor` in projectStateAccess: the package that already owns `ActivityType`, `TestingVariant` and their `String()` wire names, and the package every later task in this plan reads the lifecycle from.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — add `LifecycleKeyFor` immediately after `CommandFor` (L8768–8775); add the `methodassets` import to the import block (the file has no method-assets import today — this is the first, and the precedent for a ResourceAccess package importing it in PRODUCTION is `internal/resourceaccess/agenticjob/agenticjobaccess.go:82` and `internal/resourceaccess/sourcecontrol/sourcecontrolaccess.go:83`).
- Modify: `server/internal/manager/construction/constructionmanager.go` — delete `lifecycleTypeKey` (L3094–3100); re-point its one caller in `QueryActivityView` (L803).
- Modify: `server/internal/manager/construction/manager_test.go` — `TestLifecycleTypeKey_CoversEveryTypeAndVariant` (L9788–9820) re-points at `projectstate.LifecycleKeyFor`.
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` — delete the test-only `lifecycleTypeKey` (L10112–10119); its two uses (L10120 `allLifecycleCombos`' caller at L10147, and L10181 in `TestLifecyclesParity_EveryProfileEqualsItsLifecycle`) call `LifecycleKeyFor`.
- Modify: `server/internal/arch_test.go` — add `"LifecycleKeyFor"` to the `internal/resourceaccess/projectstate` entry of `encapsulationAllowlistData`, in the Figure A-1 block's alphabetical run after `"LabelForTask"` (L579), with its justification line in the block comment above (L541–572).

**Interfaces:**
- Consumes: `projectstate.ActivityType.String()`, `projectstate.TestingVariant.String()`, `projectstate.ActivityTypeTesting`; `methodassets.LifecycleFor(typeKey string) (Lifecycle, bool)`.
- Produces: `func LifecycleKeyFor(t ActivityType, v TestingVariant) string` in package `projectstate` — total, side-effect-free.
- Removes: `construction.lifecycleTypeKey` (unexported), `projectstate` test-only `lifecycleTypeKey`.

- [ ] **Step 1: Write the failing test.** Append to `server/internal/resourceaccess/projectstate/access_test.go`, directly after `TestLifecyclesParity_ProjectDesignIsOneUndispatchedGate` (the last function in the parity block added by stage 0). It is the manager-side table moved down to the rule's new home, plus the resolution check that makes it worth having.

  ```go
  // The lifecycle-key rule, over EVERY activity type and testing variant, at its ONE
  // production home. A stray variant on a non-testing type is ignored (the zero variant
  // is what every non-testing activity carries), and every key it produces must resolve
  // in the pinned method-assets — a key rule nothing can look up is a silent 404.
  func TestLifecycleKeyFor_CoversEveryTypeAndVariant(t *testing.T) {
  	cases := []struct {
  		typ     ActivityType
  		variant TestingVariant
  		want    string
  	}{
  		{ActivityTypeService, TestVariantPlan, "service"},
  		{ActivityTypeFrontend, TestVariantPlan, "frontend"},
  		{ActivityTypeDeployment, TestVariantPlan, "deployment"},
  		{ActivityTypeDocumentation, TestVariantPlan, "documentation"},
  		{ActivityTypeUIDesign, TestVariantPlan, "uiDesign"},
  		{ActivityTypeIntegration, TestVariantPlan, "integration"},
  		{ActivityTypeTesting, TestVariantPlan, "testing:plan"},
  		{ActivityTypeTesting, TestVariantHarness, "testing:harness"},
  		{ActivityTypeTesting, TestVariantPerf, "testing:perf"},
  		{ActivityTypeTesting, TestVariantSystemTest, "testing:systemTest"},
  		{ActivityTypeTesting, TestVariantQAProcess, "testing:qaProcess"},
  		{ActivityTypeService, TestVariantHarness, "service"},
  	}
  	for _, c := range cases {
  		got := LifecycleKeyFor(c.typ, c.variant)
  		if got != c.want {
  			t.Errorf("LifecycleKeyFor(%s, %s) = %q, want %q", c.typ, c.variant, got, c.want)
  		}
  		if _, ok := methodassets.LifecycleFor(got); !ok {
  			t.Errorf("method-assets has no lifecycle for key %q", got)
  		}
  	}
  }
  ```

- [ ] **Step 2: Run it; confirm it fails for the right reason.**
  ```bash
  cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestLifecycleKeyFor' -count=1 2>&1 | head
  ```
  Expected: `./access_test.go:NNNN:10: undefined: LifecycleKeyFor` and `FAIL … [build failed]`.

- [ ] **Step 3: Add the function.** In `server/internal/resourceaccess/projectstate/projectstateaccess.go`, immediately after `CommandFor` (which ends at L8775):

  ```go
  // LifecycleKeyFor is the method-assets lifecycle key of an activity: the activity
  // type's wire name, and "testing:<variant wire name>" for a testing activity (so QA is
  // "testing:qaProcess").
  //
  // THE ONE PRODUCTION STATEMENT OF THE RULE. Stage 0 carried it twice — once in the
  // construction Manager, once as a test-only copy here — because the parity test it
  // protected could not reach across packages into a _test.go. Stage 2 deletes the Go
  // lifecycle tables that duplication existed to guard, so the rule that names the data
  // collapses to one home, beside CommandFor: this package owns ActivityType,
  // TestingVariant and the String() wire names the key is spelled from, and it is where
  // every lifecycle lookup in the server now starts.
  func LifecycleKeyFor(t ActivityType, v TestingVariant) string {
  	if t == ActivityTypeTesting {
  		return t.String() + ":" + v.String()
  	}
  	return t.String()
  }
  ```

  Add the import to the file's import block (grouped with the other third-party modules, matching `agenticjobaccess.go:82`):
  ```go
  	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
  ```
  - [ ] **Verify first:** `GOWORK=off go build ./internal/resourceaccess/projectstate/` must be green with the import added but only `LifecycleKeyFor` using it — if goimports/`make fix` strips it because nothing in the non-test file references `methodassets` yet, add the import in Task 4 instead and keep `LifecycleKeyFor` import-free here (it is: it only reads `String()`). Confirm which by running the build; do not add a blank identifier to force it.

- [ ] **Step 4: Delete the test-only copy** in `server/internal/resourceaccess/projectstate/access_test.go`. Remove L10112–10119 (the comment block `// lifecycleTypeKey is the method-assets lifecycle key of a profile: …` through the closing brace) and replace the two call sites with `LifecycleKeyFor`:
  ```go
  	key := LifecycleKeyFor(combo.t, combo.v)
  ```
  (in `TestLifecyclesParity_EveryProfileEqualsItsLifecycle`, today `key := lifecycleTypeKey(combo.t, combo.v)`).

- [ ] **Step 5: Delete the Manager's copy and re-point its caller** — `server/internal/manager/construction/constructionmanager.go`.

  Delete L3094–3100 (the `lifecycleTypeKey` comment and function) and change L803 in `QueryActivityView` from
  ```go
  	key := lifecycleTypeKey(typ, variant)
  ```
  to
  ```go
  	key := projectstate.LifecycleKeyFor(typ, variant)
  ```
  The Manager already imports both `projectstate` and `methodassets`, so no import moves.

- [ ] **Step 6: Re-point the Manager's test** — `server/internal/manager/construction/manager_test.go`, `TestLifecycleTypeKey_CoversEveryTypeAndVariant` (L9788–9820). The rule is now tested at its home (Step 1), so this test narrows to the one thing that is still the Manager's business: that `QueryActivityView`'s key resolves. Replace the whole function with:

  ```go
  // QueryActivityView looks the activity's lifecycle up by projectstate.LifecycleKeyFor;
  // the key RULE is pinned in projectstate (TestLifecycleKeyFor_CoversEveryTypeAndVariant).
  // What is the Manager's business is that the pinned method-assets answers for every key
  // this Manager can build — an unresolvable key surfaces as an Infrastructure error at a
  // read the Activity Experience makes on every poll.
  func TestQueryActivityView_EveryLifecycleKeyResolvesInThePinnedAssets(t *testing.T) {
  	for _, c := range []struct {
  		typ     projectstate.ActivityType
  		variant projectstate.TestingVariant
  	}{
  		{projectstate.ActivityTypeService, projectstate.TestVariantPlan},
  		{projectstate.ActivityTypeFrontend, projectstate.TestVariantPlan},
  		{projectstate.ActivityTypeDeployment, projectstate.TestVariantPlan},
  		{projectstate.ActivityTypeDocumentation, projectstate.TestVariantPlan},
  		{projectstate.ActivityTypeUIDesign, projectstate.TestVariantPlan},
  		{projectstate.ActivityTypeIntegration, projectstate.TestVariantPlan},
  		{projectstate.ActivityTypeTesting, projectstate.TestVariantPlan},
  		{projectstate.ActivityTypeTesting, projectstate.TestVariantHarness},
  		{projectstate.ActivityTypeTesting, projectstate.TestVariantPerf},
  		{projectstate.ActivityTypeTesting, projectstate.TestVariantSystemTest},
  		{projectstate.ActivityTypeTesting, projectstate.TestVariantQAProcess},
  	} {
  		key := projectstate.LifecycleKeyFor(c.typ, c.variant)
  		if _, ok := methodassets.LifecycleFor(key); !ok {
  			t.Errorf("method-assets has no lifecycle for key %q", key)
  		}
  	}
  }
  ```

- [ ] **Step 7: Add the encapsulation-allowlist entry** — `server/internal/arch_test.go`. In the block comment that documents the Figure A-1 exports (L541–572), add one line to the caller inventory, keeping the existing `name → caller` alignment:
  ```
  //	LifecycleKeyFor → the construction Manager (constructionmanager.go,
  //	                  QueryActivityView): the method-assets lifecycle key of an
  //	                  activity. ONE production home for the rule, which stage 0
  //	                  deliberately carried twice.
  ```
  and add the name to the sorted run, between `"LabelForTask"` and `"PhaseForTask"`:
  ```go
  		"LifecycleKeyFor",
  ```

- [ ] **Step 8: Run both tests; confirm they pass.**
  ```bash
  cd server
  GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestLifecycleKeyFor|TestLifecyclesParity' -count=1
  GOWORK=off go test ./internal/manager/construction/ -run 'TestQueryActivityView' -count=1
  ```
  Expected: `ok` for both. The parity tests must still pass — nothing about the data changed, only who spells the key.

- [ ] **Step 9: Gates.**
  ```bash
  cd server
  GOWORK=off go build ./...
  GOWORK=off make fix-check
  GOWORK=off make lint
  GOWORK=off make test-short
  GOWORK=off go test ./internal/ -run 'TestMethodLayering|TestFileLayout|TestGeneratedOnlyPublic|TestRepoStructureCmdIsClosed|TestNoBannedPhaseIdentifier' -count=1
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-uiprofiles-check gen-lifecycles-check
  GOWORK=off make sumtype-check method-check encapsulation-check derived-plan-check
  cd ../webApp && npm run check
  ```
  All green. `encapsulation-check` is the one that would fail on a missing allowlist entry; `TestNoBannedPhaseIdentifier` sees no new top-level `phase`/`Phase` declaration.

- [ ] **Step 10: Commit.**
  ```bash
  git add server/internal/resourceaccess/projectstate/projectstateaccess.go \
          server/internal/resourceaccess/projectstate/access_test.go \
          server/internal/manager/construction/constructionmanager.go \
          server/internal/manager/construction/manager_test.go \
          server/internal/arch_test.go
  git commit -F - <<'MSG'
  refactor(projectstate): one production home for the lifecycle-key rule

  Stage 0 wrote "the activity type's wire name, testing:<variant> for testing"
  twice — in the construction Manager and as a test-only copy in projectstate —
  because the parity test could not reach a _test.go across packages. Stage 2
  removes the tables that duplication guarded, so the rule collapses onto
  projectstate.LifecycleKeyFor, beside CommandFor and the enums it spells the
  key from. The Manager's table test narrows to what is still its business:
  every key QueryActivityView can build resolves in the pinned method-assets.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 2: The construction console reads the method-assets lifecycles

The console's five consumers of `lifecycleTemplates.gen.ts` are re-pointed at `activity/lifecycles.gen.ts` through ONE adapter that presents a `LifecycleDef` in the `GeneratedPhase` shape those consumers already speak. Nothing the console renders changes: `GeneratedPhase.id` is the phase's dispatch-task `command`, which stage 0's parity test pinned equal to `CommandFor(t, v, p)` for all eleven profiles; `phase`/`name`/`weight`/`exitCriterion` are the lifecycle phase's `id`/`label`/`weight`/`exitCriterion`, pinned equal to `ProfileFor`'s `Phase`/`Label`/`Weight` and `ExitCriterionFor`; each task's `label` is the lifecycle task's `title`, pinned equal to `TaskLabelFor`. So the existing golden and layout tests under `graph/`, `list/` and `detail/` must stay green **with no edits** — an edit there means the adapter is wrong, not the golden.

Two things `lifecycles.json` deliberately does not carry and the adapter supplies: the book's own task names (`bookLabel`, today the server's `LabelForTask`) and the two conditional sub-attempt rows (`someConstruction`, `testClient`), which spec §3 keeps as sub-attempts of their phase's work task rather than lifecycle nodes. Both are small closed tables, stated once, beside the shape that needs them.

The adapter lives under `construction/` rather than `activity/` on purpose: it is the OLD console's view over the platform data and dies with the console in stage 5, while the Activity Experience reads `LifecycleDef` directly. It also collapses the `profileFor(row)` rule that is currently copy-pasted into three modules (`detail/detailPaneState.ts:398`, `detail/bodies/taskBriefing.ts:102`, `list/activityTree.ts:230`) into one exported function.

**Files:**
- Create: `webApp/src/components/construction/lifecycleProfiles.ts`
- Create: `webApp/src/components/construction/lifecycleProfiles.test.ts`
- Modify: `webApp/src/components/construction/graph/laneSpine.ts` (import L35; `CANONICAL_LIFECYCLE` L41–43)
- Modify: `webApp/src/components/construction/detail/detailPaneState.ts` (import L26–29; delete local `profileFor` L398–406)
- Modify: `webApp/src/components/construction/detail/bodies/taskBriefing.ts` (module doc L15–16, L25; imports L45–50; delete local `profileFor` L100–110 and re-export the adapter's under the same name — `bodyDispatch.ts` imports `profileFor` from here at L50)
- Modify: `webApp/src/components/construction/detail/bodies/bodyDispatch.ts` (type-only import L48)
- Modify: `webApp/src/components/construction/list/activityTree.ts` (imports L55–60; delete local `profileFor` L230–238)
- Unchanged and must stay green with no edits: `graph/laneSpine.test.ts`, `list/activityTree.test.ts`, `detail/bodies/taskBriefing.test.ts`, `detail/bodies/taskBriefingLens.test.ts`, `detail/DetailPane.test.ts`, `detail/selectionSummary.test.ts`.
- NOT touched here: `lifecycleTemplates.gen.ts`, its test, `cmd/gen-uiprofiles` and the Makefile/CI targets — Task 4 cuts those once nothing imports them and the server no longer needs the generator as a proof.

**Interfaces:**
- Consumes: `LIFECYCLES`, `lifecycleFor`, `type LifecycleDef`, `type LifecyclePhaseDef`, `type LifecycleTaskDef`, `type LifecycleTypeKey` from `../activity/lifecycles.gen.ts`; `type ActivityKind` from `./KindBadge`; `type TestingVariantName` from `../../contracts/types`.
- Produces, from `webApp/src/components/construction/lifecycleProfiles.ts`:
  - `export type LifecyclePhase = 'requirements' | 'detailed_design' | 'test_plan' | 'construction' | 'integration'`
  - `export interface GeneratedTask { task: string; label: string; bookLabel: string; gate: boolean; conditional: boolean }`
  - `export interface GeneratedPhase { id: string; phase: LifecyclePhase; name: string; weight: number; exitCriterion: string; tasks: readonly GeneratedTask[] }`
  - `export function profileFor(kind: ActivityKind | undefined, variant: TestingVariantName | undefined): readonly GeneratedPhase[] | undefined`
  - `export const SERVICE_PROFILE: readonly GeneratedPhase[]`
- Removes: three copies of `profileFor(row)`; every `lifecycleTemplates.gen.ts` import in `src/`.

- [ ] **Step 1: Write the failing test.** Create `webApp/src/components/construction/lifecycleProfiles.test.ts`. It asserts the adapter equals what the console renders today, by comparing against the still-present generated table — the strongest possible statement of "renders identically", and it is trimmed to the invariants in Task 4, when that table is deleted.

  ```ts
  /**
   * The construction console's profile view over the method-assets lifecycles.
   *
   * While BOTH generated tables exist, this pins the adapter against the one the
   * console renders from today: every phase and every task row, field for field.
   * Task 4 deletes lifecycleTemplates.gen.ts and with it this comparison; the
   * invariants below it (sub-attempt rows, book labels, canonical keys) stay.
   */
  /// <reference types="node" />
  import { test } from 'node:test';
  import assert from 'node:assert/strict';

  import {
    GENERATED_TEMPLATES,
    GENERATED_TESTING_VARIANTS,
  } from './lifecycleTemplates.gen.ts';
  import { profileFor, SERVICE_PROFILE } from './lifecycleProfiles.ts';
  import type { ActivityKind } from './KindBadge';
  import type { TestingVariantName } from '../../contracts/types';

  const KINDS: readonly ActivityKind[] = [
    'service',
    'frontend',
    'testing',
    'deployment',
    'documentation',
    'uiDesign',
    'integration',
  ];
  const VARIANTS: readonly TestingVariantName[] = [
    'plan',
    'harness',
    'perf',
    'systemTest',
    'qaProcess',
  ];

  void test('every kind profile equals the table the console renders today', () => {
    for (const kind of KINDS) {
      assert.deepEqual(profileFor(kind, undefined), GENERATED_TEMPLATES[kind], kind);
    }
  });

  void test('every testing variant profile equals the table it renders today', () => {
    for (const variant of VARIANTS) {
      assert.deepEqual(
        profileFor('testing', variant),
        GENERATED_TESTING_VARIANTS[variant],
        variant
      );
    }
  });

  void test('an unclassified row has no profile', () => {
    assert.equal(profileFor(undefined, undefined), undefined);
    assert.equal(profileFor(undefined, 'perf'), undefined);
  });

  void test('SERVICE_PROFILE is the canonical five in Method order', () => {
    assert.deepEqual(
      SERVICE_PROFILE.map((p) => p.phase),
      ['requirements', 'detailed_design', 'test_plan', 'construction', 'integration']
    );
  });

  void test('the two sub-attempt rows are carried, in place, and flagged', () => {
    const design = SERVICE_PROFILE.find((p) => p.phase === 'detailed_design');
    const construction = SERVICE_PROFILE.find((p) => p.phase === 'construction');
    assert.ok(design !== undefined && construction !== undefined);
    assert.deepEqual(
      design.tasks.map((t) => t.task),
      ['someConstruction', 'detailedDesign', 'designReview']
    );
    assert.deepEqual(
      construction.tasks.map((t) => t.task),
      ['construction', 'testClient', 'codeReview']
    );
    assert.deepEqual(
      design.tasks.map((t) => t.conditional),
      [true, false, false]
    );
    // A sub-attempt is no lifecycle node, so it keeps the book's own name.
    assert.equal(design.tasks[0].label, 'Some Construction');
    assert.equal(design.tasks[0].bookLabel, 'Some Construction');
  });

  void test('exactly one gate per phase, and the labels are the profile’s own', () => {
    const flows = profileFor('frontend', undefined)?.find((p) => p.phase === 'test_plan');
    assert.ok(flows !== undefined);
    // The frontend's Flows phase is not an STP, though its keys are stp/stpReview.
    assert.deepEqual(
      flows.tasks.map((t) => t.task),
      ['stp', 'stpReview']
    );
    assert.ok(flows.tasks.every((t) => t.label !== t.bookLabel));
    for (const kind of KINDS) {
      for (const p of profileFor(kind, undefined) ?? []) {
        assert.equal(p.tasks.filter((t) => t.gate).length, 1, `${kind}/${p.phase}`);
        assert.ok(p.exitCriterion.length > 0, `${kind}/${p.phase}`);
        assert.ok(p.id.length > 0, `${kind}/${p.phase}`);
      }
    }
  });
  ```

- [ ] **Step 2: Run it; confirm it fails for the right reason.**
  ```bash
  cd webApp && npx tsc -b 2>&1 | head
  ```
  Expected: `error TS2307: Cannot find module './lifecycleProfiles.ts'` (the module does not exist yet).

- [ ] **Step 3: Write the adapter** — `webApp/src/components/construction/lifecycleProfiles.ts`.

  ```ts
  /**
   * The construction console's profile view over the platform's lifecycles.
   *
   * The per-activity-type lifecycle is platform-fixed method DATA (method-assets
   * lifecycles.json), generated into `activity/lifecycles.gen.ts`. It used to be a Go
   * table in projectStateAccess rendered into this directory as
   * `lifecycleTemplates.gen.ts`; the table is gone, and this module presents the data in
   * the shape the console's five consumers already speak, so nothing they render moves.
   *
   * What the data deliberately does NOT carry, and this module supplies:
   *  - `bookLabel`, Figure A-1's own name for a task, beside the profile's word for it;
   *  - the two conditional sub-attempt rows. `someConstruction` (Löwy's pre-design spike)
   *    and `testClient` (Construction's tandem partner) are sub-attempts of their phase's
   *    work task, not nodes of the DAG (spec §3), so the lifecycle has no node for them.
   *    The console renders a row for each ONLY once a real attempt exists — the filter
   *    lives in `list/activityTree.ts` and `detail/detailPaneState.ts`, which is why the
   *    row must exist here, flagged, for them to filter.
   *
   * This module is the OLD console's adapter and dies with it in stage 5; the Activity
   * Experience reads `LifecycleDef` directly.
   *
   * Pure — no React — pinned by lifecycleProfiles.test.ts.
   */
  import {
    lifecycleFor,
    type LifecycleDef,
    type LifecyclePhaseDef,
    type LifecycleTypeKey,
  } from '../activity/lifecycles.gen.ts';
  import type { ActivityKind } from './KindBadge';
  import type { TestingVariantName } from '../../contracts/types';

  /** Canonical Method lifecycle phase (Righting Software Appendix A / Table A-1). */
  export type LifecyclePhase =
    | 'requirements'
    | 'detailed_design'
    | 'test_plan'
    | 'construction'
    | 'integration';

  /** One Figure A-1 task row within a lifecycle phase. */
  export interface GeneratedTask {
    /** The Figure A-1 task KEY — invariant across profiles; the ledger's join key. */
    task: string;
    /** This profile's display label for the task (the lifecycle task's title). */
    label: string;
    /** The book's own name for the task. */
    bookLabel: string;
    /** True when this task's success IS the phase's binary exit criterion (App A). */
    gate: boolean;
    /** True when the row is a sub-attempt, rendered only if a real attempt exists. */
    conditional: boolean;
  }

  export interface GeneratedPhase {
    /** The phase's slash-command cell, e.g. `service-detailed-design`. */
    id: string;
    phase: LifecyclePhase;
    name: string;
    /** % contribution (App A Table A-1); weights sum to 100 per kind. */
    weight: number;
    /** This profile's binary exit criterion for the phase. */
    exitCriterion: string;
    tasks: readonly GeneratedTask[];
  }

  /** The book's own name per Figure A-1 task (server: projectstate's LabelForTask). */
  const BOOK_LABEL: Readonly<Record<string, string>> = {
    srs: 'SRS',
    srsReview: 'SRS Review',
    stp: 'STP',
    stpReview: 'STP Review',
    someConstruction: 'Some Construction',
    detailedDesign: 'Detailed Design',
    designReview: 'Design Review',
    construction: 'Construction',
    testClient: 'Test Client',
    codeReview: 'Code Review',
    integration: 'Integration',
    testing: 'Testing',
  };

  /**
   * The full task-row order of the two phases that carry a sub-attempt, as the server's
   * phaseTasks stated it. Every other phase is simply [work, gate]: task KEYS never vary
   * by profile, so keying this by task id holds for all of them.
   */
  const PHASE_TASK_ORDER: Partial<Record<LifecyclePhase, readonly string[]>> = {
    detailed_design: ['someConstruction', 'detailedDesign', 'designReview'],
    construction: ['construction', 'testClient', 'codeReview'],
  };

  const KIND_LIFECYCLE: Readonly<Record<ActivityKind, LifecycleTypeKey>> = {
    service: 'service',
    frontend: 'frontend',
    // A testing row that arrived without a variant reads as the plan variant — the same
    // fallback the server's zero TestingVariant makes.
    testing: 'testing:plan',
    deployment: 'deployment',
    documentation: 'documentation',
    uiDesign: 'uiDesign',
    integration: 'integration',
  };

  const VARIANT_LIFECYCLE: Readonly<Record<TestingVariantName, LifecycleTypeKey>> = {
    plan: 'testing:plan',
    harness: 'testing:harness',
    perf: 'testing:perf',
    systemTest: 'testing:systemTest',
    qaProcess: 'testing:qaProcess',
  };

  function taskRow(def: LifecycleDef, phase: LifecyclePhaseDef, id: string): GeneratedTask {
    const task = def.tasks.find((t) => t.id === id);
    const bookLabel = BOOK_LABEL[id] ?? id;
    return {
      task: id,
      // A sub-attempt is no lifecycle node, so the data has no title for it and the
      // book's own name is the honest label.
      label: task?.title ?? bookLabel,
      bookLabel,
      gate: id === phase.gate,
      conditional: task === undefined,
    };
  }

  function phaseRow(def: LifecycleDef, phase: LifecyclePhaseDef): GeneratedPhase {
    const work = def.tasks.find((t) => t.phase === phase.id && t.kind === 'dispatch');
    const order = PHASE_TASK_ORDER[phase.id as LifecyclePhase] ?? [
      ...(work === undefined ? [] : [work.id]),
      phase.gate,
    ];
    return {
      id: work?.command ?? '',
      phase: phase.id as LifecyclePhase,
      name: phase.label,
      weight: phase.weight,
      exitCriterion: phase.exitCriterion,
      tasks: order.map((id) => taskRow(def, phase, id)),
    };
  }

  function profileOf(key: LifecycleTypeKey): readonly GeneratedPhase[] {
    const def = lifecycleFor(key);
    return def === undefined ? [] : def.phases.map((p) => phaseRow(def, p));
  }

  /**
   * The activity's Figure A-1 profile, or `undefined` when the server did not classify
   * it. A testing activity uses its VARIANT profile (the five variants have genuinely
   * different phase sets); a testing row that arrived without a variant falls back to the
   * plan profile. ONE copy of this rule — it used to be pasted into detailPaneState,
   * taskBriefing and activityTree.
   */
  export function profileFor(
    kind: ActivityKind | undefined,
    variant: TestingVariantName | undefined
  ): readonly GeneratedPhase[] | undefined {
    if (kind === undefined) return undefined;
    if (kind === 'testing' && variant !== undefined) return profileOf(VARIANT_LIFECYCLE[variant]);
    return profileOf(KIND_LIFECYCLE[kind]);
  }

  /** The canonical five, in Method order — the Service profile's order. */
  export const SERVICE_PROFILE: readonly GeneratedPhase[] = profileOf('service');
  ```

  - [ ] **Verify first:** `activity/lifecycles.gen.ts` exports `lifecycleFor(typeKey: string): LifecycleDef | undefined` (L1230) and `LifecycleTypeKey` (L18). If `lifecycleFor`'s parameter is `string` rather than `LifecycleTypeKey`, the calls above still typecheck; if the generated module exports `LIFECYCLES` only, add a local `const byKey = new Map(LIFECYCLES.map((l) => [l.type, l]))` instead of importing `lifecycleFor`. Read the file's last 10 lines before writing.

- [ ] **Step 4: Run the test; confirm it passes.**
  ```bash
  cd webApp && npx tsc -b && node --test --experimental-strip-types src/components/construction/lifecycleProfiles.test.ts
  ```
  Expected: all six subtests pass. A `deepEqual` failure names the exact field that moved — fix the ADAPTER, never the expectation: the generated table is what ships today.
  - [ ] **Verify first:** check how the repo runs a single node test (`package.json` `test` script / `npm run check`'s test step). Use that invocation rather than the line above if it differs.

- [ ] **Step 5: Re-point `list/activityTree.ts`.** Replace the import block (L55–60):
  ```ts
  import {
    profileFor,
    type GeneratedPhase,
    type GeneratedTask,
    type LifecyclePhase,
  } from '../lifecycleProfiles.ts';
  ```
  and delete the local `profileFor` (L230–238 with its doc comment L222–229), replacing its two call sites' argument list — `profileFor(row)` becomes `profileFor(row.kind, row.variant)`.

- [ ] **Step 6: Re-point `detail/detailPaneState.ts`.** Replace the import (L26–29) with
  ```ts
  import { profileFor, type GeneratedPhase } from '../lifecycleProfiles.ts';
  ```
  delete the local `profileFor` (L398–406), and change its call sites to `profileFor(row.kind, row.variant)`.

- [ ] **Step 7: Re-point `detail/bodies/taskBriefing.ts`.** Replace the import (L45–50) with
  ```ts
  import {
    profileFor as lifecycleProfileFor,
    type GeneratedPhase,
    type GeneratedTask,
    type LifecyclePhase,
  } from '../../lifecycleProfiles.ts';
  ```
  and replace the local `profileFor` body (L100–110) with a re-export wrapper, because `bodyDispatch.ts` imports `profileFor` FROM this module (L50) and takes a `ConstructionRow`:
  ```ts
  export function profileFor(
    row: ConstructionRow | undefined
  ): readonly GeneratedPhase[] | undefined {
    return lifecycleProfileFor(row?.kind, row?.variant);
  }
  ```
  Update the module doc's two references to `lifecycleTemplates.gen.ts` (L15–16, L25) to name `lifecycleProfiles.ts` and the method-assets lifecycles.

- [ ] **Step 8: Re-point the two type-only importers.**
  - `detail/bodies/bodyDispatch.ts` L48: `import type { LifecyclePhase } from '../../lifecycleProfiles.ts';`
  - `graph/laneSpine.ts` L35: `import { SERVICE_PROFILE, type LifecyclePhase } from '../lifecycleProfiles.ts';` and L41–43 becomes
    ```ts
    export const CANONICAL_LIFECYCLE: readonly LifecyclePhase[] = SERVICE_PROFILE.map(
      (p) => p.phase
    );
    ```

- [ ] **Step 9: Confirm nothing in `src/` imports the old table any more.**
  ```bash
  cd webApp && grep -rn "lifecycleTemplates" src/ | grep -v "lifecycleTemplates.gen.ts:" | grep -v "lifecycleTemplates.gen.test.ts:" | grep -v "lifecycleProfiles"
  ```
  Expected: no output. (`lifecycleProfiles.test.ts` still imports it on purpose until Task 4.)

- [ ] **Step 10: Gates.**
  ```bash
  cd webApp && npm run check
  ```
  Green, including `graph/laneSpine.test.ts`, `list/activityTree.test.ts` and the `detail/` suites **unedited**. If any of them fails, the adapter is wrong — do not touch the golden. The server is untouched by this task, so no `make` gate runs.

- [ ] **Step 11: Commit.**
  ```bash
  git add webApp/src/components/construction/lifecycleProfiles.ts \
          webApp/src/components/construction/lifecycleProfiles.test.ts \
          webApp/src/components/construction/graph/laneSpine.ts \
          webApp/src/components/construction/detail/detailPaneState.ts \
          webApp/src/components/construction/detail/bodies/taskBriefing.ts \
          webApp/src/components/construction/detail/bodies/bodyDispatch.ts \
          webApp/src/components/construction/list/activityTree.ts
  git commit -F - <<'MSG'
  refactor(webapp): the construction console reads the method-assets lifecycles

  lifecycleProfiles.ts presents activity/lifecycles.gen.ts in the GeneratedPhase
  shape the console's five consumers already speak, and supplies the two things
  the platform data deliberately omits: Figure A-1's own task names, and the
  someConstruction / testClient sub-attempt rows, which are sub-attempts of their
  phase's work task rather than nodes of the DAG. A field-for-field test against
  the still-present generated table pins that nothing rendered moves; the graph,
  list and detail goldens pass unedited. The profileFor(row) rule, until now
  pasted into three modules, collapses to one.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 3: The lifecycle's WORDS come from method-assets; `profileRows` is deleted

`profileRows` / `serviceRows` / `profileRowsForTestingVariant` / `testPlanRows` is the per-profile table of phases, weights, labels, work/gate words and exit criteria — platform-fixed method content sitting in a ResourceAccess package, which spec §3 calls the wrong layer. It goes; `ProfileFor`, `CommandFor`, `ExitCriterionFor` and `TaskLabelFor` keep their exact signatures and read `methodassets.LifecycleFor` instead. `profileSlug` and `kebabPhase` go with it: a phase's command is now the phase's dispatch-task `command`, stated in the data.

**Nothing observable changes, and the repo proves it.** `cmd/gen-uiprofiles` still exists in this task, still renders `lifecycleTemplates.gen.ts` from `ProfileFor` / `CommandFor` / `ExitCriterionFor` / `TaskLabelFor`, and `make gen-uiprofiles-check` regenerates-then-diffs it. A green `gen-uiprofiles-check` after the swap is a byte-for-byte proof that every word the eleven profiles state is unchanged — which is exactly why the generator is cut in Task 4 and not here.

The task DAG's STRUCTURE (`phaseTasks`, `gateTasks`, `AgentTaskFor`, `GateTaskFor`, `PhaseForTask`, `TasksForPhase`, `conditionalTasks`, `taskLabels`) is untouched in this task and moves in Task 4.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go`
  - delete `profileRow` (L7933–7959, comment included), `profileRows` (L7973–8019), `serviceRows` (L8021–8031), `profileRowsForTestingVariant` (L8033–…) and `testPlanRows`, `profileRowFor` (L8946–8955), `profileSlug` (L8723–8760), `kebabPhase` (L8762–8766)
  - rewrite `ProfileFor` (L7960–7972), `CommandFor` (L8768–8775), `TaskLabelFor` (L8931–8945), `ExitCriterionFor` (L8961–8966)
  - add `lifecycleFor` and `dispatchTaskIn` beside `ProfileFor`
  - add the `methodassets` import if Task 1 Step 3's verify deferred it
- Modify: `server/internal/resourceaccess/projectstate/access_test.go`
  - delete `TestLifecyclesParity_EveryProfileEqualsItsLifecycle`, `assertLifecyclePhaseParity`, `allLifecycleCombos`, `lifecycleTaskWords`, `lifecycleTaskWordsOf` (stage 0's L10120–10200ish)
  - KEEP `TestLifecyclesParity_DesignActivitiesFollowTheDesignRail` and `TestLifecyclesParity_ProjectDesignIsOneUndispatchedGate` — they hold the data against `Phase1RequiredKinds` / `DesignCommandFor`, which are still hand tables, and Part B builds on them
  - add the two totality tests below
- Unchanged: `cmd/gen-uiprofiles`, `webApp/src/components/construction/lifecycleTemplates.gen.ts`, the Makefile, CI.

**Interfaces:**
- Consumes: `methodassets.LifecycleFor(string) (Lifecycle, bool)`, `methodassets.Lifecycles() []Lifecycle`, `methodassets.Lifecycle{Type, Phases, Tasks}`, `methodassets.LifecyclePhase{ID, Label, Weight, Gate, ExitCriterion}`, `methodassets.LifecycleTask{ID, Kind, Title, Phase, Command}`, `methodassets.LifecycleTaskDispatch`; `LifecycleKeyFor` (Task 1).
- Produces (all signatures UNCHANGED): `ProfileFor(ActivityType, TestingVariant) Profile`, `CommandFor(ActivityType, TestingVariant, ActivityMethodPhase) string`, `ExitCriterionFor(ActivityType, TestingVariant, ActivityMethodPhase) string`, `TaskLabelFor(ActivityType, TestingVariant, MethodTask) string`; unexported `lifecycleFor(ActivityType, TestingVariant) methodassets.Lifecycle`, `dispatchTaskIn(methodassets.Lifecycle, ActivityMethodPhase) methodassets.LifecycleTask`.
- Removes: `profileRow`, `profileRows`, `serviceRows`, `profileRowsForTestingVariant`, `testPlanRows`, `profileRowFor`, `profileSlug`, `kebabPhase` — all unexported, so no `arch_test.go` allowlist change.

- [ ] **Step 1: Write the failing tests.** Append to `server/internal/resourceaccess/projectstate/access_test.go`, after `TestLifecycleKeyFor_CoversEveryTypeAndVariant` (Task 1). These are the smaller invariant that replaces the parity test: the table is gone, so what is left to hold is that the mapping is TOTAL in both directions — no activity type without a lifecycle, no lifecycle nobody can reach.

  ```go
  // ---- lifecycle totality (stage 2: the data is the only source) ----
  //
  // profileRows is gone, so profile↔lifecycle parity is tautological and its test with
  // it. What survives is the pair of claims a table cannot make for itself: every
  // (ActivityType, TestingVariant) an activity can carry resolves to a lifecycle, and
  // every lifecycle the platform ships is reachable from one.

  func TestEveryActivityTypeResolvesToALifecycle(t *testing.T) {
  	for _, combo := range append(allProfileCombos(),
  		profileCombo{ActivityTypeUIDesign, 0},
  		profileCombo{ActivityTypeIntegration, 0}) {
  		key := LifecycleKeyFor(combo.t, combo.v)
  		lc, ok := methodassets.LifecycleFor(key)
  		if !ok {
  			t.Errorf("no lifecycle %q — ProfileFor would silently fall back to service", key)
  			continue
  		}
  		if got := ProfileFor(combo.t, combo.v); len(got.Phases) != len(lc.Phases) {
  			t.Errorf("%s: ProfileFor has %d phases, the lifecycle has %d", key, len(got.Phases), len(lc.Phases))
  		}
  		for _, ph := range lc.Phases {
  			p := ActivityMethodPhase(ph.ID)
  			if CommandFor(combo.t, combo.v, p) == "" {
  				t.Errorf("%s/%s: no dispatch command — the phase walk would dispatch nothing", key, p)
  			}
  			if ExitCriterionFor(combo.t, combo.v, p) == "" {
  				t.Errorf("%s/%s: no exit criterion", key, p)
  			}
  		}
  	}
  }

  // designLifecycleKeys are the three lifecycles that have no ActivityType yet: Part B of
  // this stage appends requirements/architecture/projectDesign to the enum and emits them
  // as the plan's fixed prefix. When it does, they move into allProfileCombos and out of
  // here, and this test's failure is the reminder.
  var designLifecycleKeys = []string{"requirements", "architecture", "projectDesign"}

  func TestEveryLifecycleIsReachableFromAnActivityType(t *testing.T) {
  	reachable := map[string]bool{}
  	for _, key := range designLifecycleKeys {
  		reachable[key] = true
  	}
  	for _, combo := range append(allProfileCombos(),
  		profileCombo{ActivityTypeUIDesign, 0},
  		profileCombo{ActivityTypeIntegration, 0}) {
  		reachable[LifecycleKeyFor(combo.t, combo.v)] = true
  	}
  	for _, lc := range methodassets.Lifecycles() {
  		if !reachable[lc.Type] {
  			t.Errorf("lifecycle %q is shipped but no activity type reaches it", lc.Type)
  		}
  		delete(reachable, lc.Type)
  	}
  	for key := range reachable {
  		t.Errorf("activity types reach key %q but the platform ships no such lifecycle", key)
  	}
  }

  // A phase a profile does not carry has no command. profileSlug used to fabricate one
  // ("deployment-requirements") for a .claude/commands file that does not exist; the data
  // simply has no dispatch task there, and "" is the honest answer.
  func TestCommandFor_IsEmptyForAPhaseTheProfileDoesNotCarry(t *testing.T) {
  	if got := CommandFor(ActivityTypeDeployment, 0, MethodPhaseRequirements); got != "" {
  		t.Errorf("CommandFor(deployment, requirements) = %q, want \"\"", got)
  	}
  	if got := CommandFor(ActivityTypeIntegration, 0, MethodPhaseConstruction); got != "" {
  		t.Errorf("CommandFor(integration, construction) = %q, want \"\"", got)
  	}
  }
  ```

- [ ] **Step 2: Run them; confirm the third fails and the first two pass.**
  ```bash
  cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ \
    -run 'TestEveryActivityTypeResolvesToALifecycle|TestEveryLifecycleIsReachableFromAnActivityType|TestCommandFor_IsEmptyForAPhaseTheProfileDoesNotCarry' -count=1 -v 2>&1 | tail -20
  ```
  Expected: the two totality tests PASS already (the data is correct; they are the guard, not the driver) and `TestCommandFor_IsEmptyForAPhaseTheProfileDoesNotCarry` FAILS with `CommandFor(deployment, requirements) = "deployment-requirements", want ""` — today's `profileSlug`+`kebabPhase` fabricating a command for a phase the profile has not got. If a totality test fails instead, the pinned `lifecycles.json` is wrong: stop and report, do not adjust the test.

- [ ] **Step 3: Swap `ProfileFor` onto the data** — `server/internal/resourceaccess/projectstate/projectstateaccess.go`. Replace `ProfileFor`, `profileRow`, `profileRows`, `serviceRows`, `profileRowsForTestingVariant` and `testPlanRows` (the whole run from the `profileRow` comment at L7933 through the end of `testPlanRows`) with:

  ```go
  // THE LIFECYCLE IS DATA, NOT A GO TABLE. profileRows used to state each activity type's
  // phases, weights, labels, work/gate words and exit criteria right here — platform-fixed
  // method content in a ResourceAccess package, which is the wrong layer for it (spec §3).
  // The one source is now lifecycles.json in the method-assets release pinned in go.mod:
  // the same bytes cmd/gen-lifecycles renders into the webApp, so the server and the SPA
  // cannot disagree about what a Service activity's Detailed Design phase is called or
  // weighs. What remains here is the ADAPTER — the questions this package's callers
  // already ask, answered out of that data.

  // lifecycleFor is the total lookup behind every function below. An activity type outside
  // the data falls back to the service lifecycle, exactly as profileRows' default arm did:
  // every caller (hydrateConstructionActivity, ResolveConstructionRow, phaseSetFor) passes
  // a value ClassifyActivity produced, and TestEveryActivityTypeResolvesToALifecycle pins
  // that each of those resolves, so the fallback is unreachable rather than lenient.
  func lifecycleFor(t ActivityType, v TestingVariant) methodassets.Lifecycle {
  	if lc, ok := methodassets.LifecycleFor(LifecycleKeyFor(t, v)); ok {
  		return lc
  	}
  	lc, _ := methodassets.LifecycleFor(ActivityTypeService.String())
  	return lc
  }

  // dispatchTaskIn is a lifecycle phase's single work task — what an agent is dispatched
  // to do, as opposed to the review that gates it. The zero task for a phase the lifecycle
  // does not carry, and for a phase whose work is not dispatched at all (projectDesign's
  // gate stands over a computed artifact).
  func dispatchTaskIn(lc methodassets.Lifecycle, p ActivityMethodPhase) methodassets.LifecycleTask {
  	for _, task := range lc.Tasks {
  		if task.Phase == string(p) && task.Kind == methodassets.LifecycleTaskDispatch {
  			return task
  		}
  	}
  	return methodassets.LifecycleTask{}
  }

  // lifecyclePhaseIn is the named phase of a lifecycle; false when it carries no such phase.
  func lifecyclePhaseIn(lc methodassets.Lifecycle, p ActivityMethodPhase) (methodassets.LifecyclePhase, bool) {
  	for _, ph := range lc.Phases {
  		if ph.ID == string(p) {
  			return ph, true
  		}
  	}
  	return methodassets.LifecyclePhase{}, false
  }

  // ProfileFor returns the canonical-phase profile for an activity type (and testing
  // variant, meaningful only when t == ActivityTypeTesting): the phases that type's
  // lifecycle carries, in lifecycle order, with their earned-value weights and labels.
  // Weights sum to 100 within each profile (the platform's own lifecycles_test.go).
  func ProfileFor(t ActivityType, v TestingVariant) Profile {
  	lc := lifecycleFor(t, v)
  	phases := make([]ProfilePhase, len(lc.Phases))
  	for i, ph := range lc.Phases {
  		phases[i] = ProfilePhase{Phase: ActivityMethodPhase(ph.ID), Weight: ph.Weight, Label: ph.Label}
  	}
  	return Profile{Phases: phases}
  }
  ```

- [ ] **Step 4: Swap the three word lookups.**

  Replace `profileSlug` (L8723–8760), `kebabPhase` (L8762–8766) and `CommandFor` (L8768–8775) with just:

  ```go
  // CommandFor returns the .claude slash-command name for a (type, variant, phase) cell:
  // the command of that phase's dispatch task, as the lifecycle states it. It matches a
  // .claude/commands/<name>.md file.
  //
  // "" for a phase the profile does not carry. profileSlug used to compose a name for any
  // phase at all — "deployment-requirements" for a profile with no requirements phase, a
  // command file that has never existed. A caller walking ProfileFor's phases never asked
  // that question; a caller that does now gets an honest empty answer instead of a
  // dispatch that would 404.
  func CommandFor(t ActivityType, v TestingVariant, p ActivityMethodPhase) string {
  	return dispatchTaskIn(lifecycleFor(t, v), p).Command
  }
  ```

  Replace `TaskLabelFor` (L8931–8945) and `profileRowFor` (L8946–8955) with:

  ```go
  // TaskLabelFor returns the display label of a Figure A-1 task as ONE profile names it:
  // the lifecycle task's own title (a test plan's construction gate is "Scenario Review",
  // not the book's "Code Review"), and the book's name (LabelForTask) for a task the
  // lifecycle carries no node for — the two conditional sub-attempts, and any task in a
  // phase this profile does not carry. The task KEY never varies; only this label does.
  func TaskLabelFor(t ActivityType, v TestingVariant, task MethodTask) string {
  	for _, tk := range lifecycleFor(t, v).Tasks {
  		if tk.ID == string(task) {
  			return tk.Title
  		}
  	}
  	return LabelForTask(task)
  }
  ```

  Replace `ExitCriterionFor` (L8961–8966) with:

  ```go
  // ExitCriterionFor returns a lifecycle phase's binary exit criterion as ONE profile
  // states it, or "" for a phase that profile does not carry.
  func ExitCriterionFor(t ActivityType, v TestingVariant, p ActivityMethodPhase) string {
  	ph, _ := lifecyclePhaseIn(lifecycleFor(t, v), p)
  	return ph.ExitCriterion
  }
  ```

- [ ] **Step 5: Rewrite the two tests that read the deleted tables** — `server/internal/resourceaccess/projectstate/access_test.go`. Both reach into now-gone unexported symbols; both say something worth keeping, through the exported surface.

  (a) `TestCommandForTotalOverProfiles` (L7283–7299) asserts the command equals `profileSlug(combo.t, combo.v) + "-" + kebabPhase(p)`, which is the deleted composition rule restated. What is left to assert is the SHAPE — that the data's commands are well-formed slugs — with `TestConstructionCommandsExist` (L7340) already proving each names a real `.claude/commands/<name>.md`. Replace the body:

  ```go
  // CommandFor is total over exactly the phases ProfileFor emits: every one names a
  // well-formed command slug. That it names a command file that EXISTS is
  // TestConstructionCommandsExist, below; the composition rule itself is no longer this
  // package's to state — the lifecycle data carries the name.
  func TestCommandForTotalOverProfiles(t *testing.T) {
  	slug := regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
  	for _, combo := range allProfileCombos() {
  		for _, p := range ProfileFor(combo.t, combo.v).PhaseIDs() {
  			got := CommandFor(combo.t, combo.v, p)
  			if got == "" {
  				t.Errorf("CommandFor(%v,%v,%q) empty", combo.t, combo.v, p)
  			}
  			if !slug.MatchString(got) {
  				t.Errorf("CommandFor(%v,%v,%q) = %q, not a command slug", combo.t, combo.v, p, got)
  			}
  		}
  	}
  }
  ```
  - [ ] **Verify first:** `regexp` may not be imported in `access_test.go`. Check the import block; add it if missing, or assert the shape with `strings.ContainsAny(got, " _/A-Z")` instead if the file's idiom avoids `regexp`.

  (b) `TestProfileRows_TotalOverExactlyTheProfilesPhases` (L9182–9214) walks `profileRows` directly. Rename it and read the same claim off the exported surface — which is strictly stronger, because that is what the SPA's generator and the Manager actually see:

  ```go
  // Every phase a profile carries is WHOLE — a label, a work word, a gate word and an
  // exit — and a canonical phase the profile does not carry has no exit criterion (the
  // SPA's `absent` body names it instead). Read through the exported surface, not the
  // data file: these four functions are what the generator and the Manager see.
  func TestProfileWords_TotalOverExactlyTheProfilesPhases(t *testing.T) {
  	canonical := []ActivityMethodPhase{
  		MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
  		MethodPhaseConstruction, MethodPhaseIntegration,
  	}
  	for _, pr := range allProfiles {
  		carried := map[ActivityMethodPhase]bool{}
  		for _, ph := range ProfileFor(pr.typ, pr.variant).Phases {
  			carried[ph.Phase] = true
  			work := TaskLabelFor(pr.typ, pr.variant, AgentTaskFor(ph.Phase))
  			gate := TaskLabelFor(pr.typ, pr.variant, GateTaskFor(ph.Phase))
  			exit := ExitCriterionFor(pr.typ, pr.variant, ph.Phase)
  			if ph.Label == "" || work == "" || gate == "" || exit == "" {
  				t.Errorf("%s: phase %q is incomplete: label=%q work=%q gate=%q exit=%q",
  					pr.name, ph.Phase, ph.Label, work, gate, exit)
  			}
  			if work == gate {
  				t.Errorf("%s: phase %q names its work and its gate the same (%q)", pr.name, ph.Phase, work)
  			}
  		}
  		for _, p := range canonical {
  			if carried[p] {
  				continue
  			}
  			if exit := ExitCriterionFor(pr.typ, pr.variant, p); exit != "" {
  				t.Errorf("%s: phase %q is not in the profile but has exit %q", pr.name, p, exit)
  			}
  		}
  	}
  }
  ```

- [ ] **Step 6: Retire the parity test.** In `server/internal/resourceaccess/projectstate/access_test.go`, delete `TestLifecyclesParity_EveryProfileEqualsItsLifecycle`, `assertLifecyclePhaseParity`, `allLifecycleCombos`, `type lifecycleTaskWords` and `lifecycleTaskWordsOf`. Amend the block comment that opens the parity section to say what is left:

  ```go
  // ---- lifecycles.json, held against this package's remaining hand tables ----
  //
  // The per-type lifecycle no longer exists twice: profileRows is gone and ProfileFor is
  // an adapter over the data, so a profile-vs-lifecycle comparison compares the data with
  // itself. What these two still hold is the DESIGN rail, whose commands and required
  // kinds are hand tables here (DesignCommandFor, Phase1RequiredKinds) and whose
  // lifecycles ship in the platform: a command renamed on one side alone fails here.
  ```

  Keep `TestLifecyclesParity_DesignActivitiesFollowTheDesignRail` and `TestLifecyclesParity_ProjectDesignIsOneUndispatchedGate` exactly as they are.

- [ ] **Step 7: Run the package; confirm green.**
  ```bash
  cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -count=1
  ```
  Expected: `ok`. `TestCommandFor_IsEmptyForAPhaseTheProfileDoesNotCarry` now passes; every pre-existing profile test (`TestProfileVocabulary_*`, `TestProfileFor_*`, the weight-sum tests) must pass **unedited** — they describe the words, and the words did not move. A failure there means the data and the deleted table disagreed; report it, do not edit the test.

- [ ] **Step 8: The byte-identity proof.**
  ```bash
  cd server && GOWORK=off make gen-uiprofiles-check && echo "IDENTICAL"
  ```
  Expected: `IDENTICAL`. `gen-uiprofiles` re-renders `webApp/src/components/construction/lifecycleTemplates.gen.ts` from `ProfileFor` / `CommandFor` / `ExitCriterionFor` / `TaskLabelFor`, and `git diff --exit-code` finds nothing — the eleven profiles state exactly the same words after the swap as before it. **This is the acceptance criterion of the task.** If it diffs, read the diff: the data is wrong, or the adapter is. Never commit the regenerated file to make this green.

- [ ] **Step 9: Gates.**
  ```bash
  cd server
  GOWORK=off go build ./...
  GOWORK=off make fix-check
  GOWORK=off make lint
  GOWORK=off make test-short
  GOWORK=off go test ./internal/ -run 'TestMethodLayering|TestFileLayout|TestGeneratedOnlyPublic|TestRepoStructureCmdIsClosed|TestNoBannedPhaseIdentifier' -count=1
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-uiprofiles-check gen-lifecycles-check
  GOWORK=off make sumtype-check method-check encapsulation-check derived-plan-check
  cd ../webApp && npm run check
  ```
  All green. `make lint`'s `exhaustive` check no longer sees the `profileRows` switch; `conditionalTasks` and `taskLabels` still carry their exhaustive maps, so that discipline is unchanged. `TestMethodLayering` admits the whole `github.com/mixofreality-studio/` family for every layer, and two ResourceAccess packages already import method-assets in production, so the new import needs no rule change.

- [ ] **Step 10: Commit.**
  ```bash
  git add server/internal/resourceaccess/projectstate/projectstateaccess.go \
          server/internal/resourceaccess/projectstate/access_test.go
  git commit -F - <<'MSG'
  refactor(projectstate): the lifecycle's words come from method-assets

  profileRows / serviceRows / profileRowsForTestingVariant stated every activity
  type's phases, weights, labels, work and gate words and exit criteria as a Go
  table inside a ResourceAccess package — the wrong layer for platform-fixed
  method content. ProfileFor, CommandFor, ExitCriterionFor and TaskLabelFor keep
  their signatures and read lifecycles.json instead; profileSlug and kebabPhase
  go with the table, so a phase's command is the phase's dispatch task rather
  than a name composed for a file that may not exist.

  gen-uiprofiles-check is the proof: it re-renders the SPA's lifecycle table from
  these four functions and diffs clean, so not one rendered word moved. The
  profile-vs-lifecycle parity test retires with the duplication it guarded;
  totality in both directions replaces it, and the design-rail parity tests stay.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 4: Cut `gen-uiprofiles`, `lifecycleTemplates.gen.ts`, and the last hand-authored task tables

The generator has done its last job: Task 3 used its drift gate to prove the swap moved no word, and Task 2 left it with no consumer. It goes, and with it the SPA table it wrote, its Makefile targets, its CI step, its `repostructure_test.go` allowlist entry, and its test.

That cut takes four exports with it — `TasksForPhase`, `TaskLabelFor`, `ExitCriterionFor`, `LabelForTask` — whose only caller outside `projectstate` was `cmd/gen-uiprofiles`, which is the standard the allowlist block states for itself ("Every name below has at least one caller OUTSIDE this package"). Their words now travel to the SPA in `lifecycles.gen.ts`.

Then the last hand-authored structure goes the way `profileRows` went. `phaseTasks` and `gateTasks` said which tasks make up each lifecycle phase and which of them is its gate — facts `lifecycles.json` now states per lifecycle. They are folded once, at package init, into a phase-keyed pair table. A phase id keys it rather than a (type, phase) pair because the callers that ask hold a phase and no type (the workflow's phase walk through `AgentTaskFor`, App A's completion rule through `phaseCompleteFromAttempts` → `GateTaskFor`) and because the data says the answer does not vary: the eleven construction lifecycles share the canonical five phase ids and name the same work and gate task under each, and the three design lifecycles use phase ids that collide with none of them. A new test makes that a checked invariant rather than an assumption.

`conditionalTasks` survives, because `someConstruction` and `testClient` survive: spec §3 keeps them as conditional sub-attempts of their phase's work task, NOT nodes of the DAG, so `lifecycles.json` carries no node for them and a derived table cannot answer for them. The map is re-typed from `map[MethodTask]bool` to `map[MethodTask]ActivityMethodPhase` so it states the one fact still needed — which phase such an attempt belongs to, for `PhaseForTask`'s denormalized stamp — in one place instead of two, keeping its exhaustive-over-all-twelve discipline. The Manager's `foldConditional` (`constructionmanager.go:2929`) never consulted `IsConditionalTask`: it folds any attempt in a phase that is neither the work nor the gate task, so its semantics are untouched.

**Files:**
- Delete: `server/cmd/gen-uiprofiles/` (`main.go`, `main_test.go`, the directory)
- Delete: `webApp/src/components/construction/lifecycleTemplates.gen.ts`, `webApp/src/components/construction/lifecycleTemplates.gen.test.ts`
- Modify: `server/Makefile` — drop `gen-uiprofiles` and `gen-uiprofiles-check` from `.PHONY` (L3), from the `gen` aggregate (L81), and delete both target blocks with their comments (L154–168)
- Modify: `.github/workflows/server-checks.yml` — delete the `Generated UI lifecycle-profile drift check` step and its comment (L145–150)
- Modify: `server/internal/repostructure_test.go` — delete `"gen-uiprofiles": true,` from `allowedCmd` (L50)
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — delete `phaseTasks`, `gateTasks`, `TasksForPhase`, `TaskLabelFor`, `ExitCriterionFor`, `taskLabels`, `LabelForTask`, `IsConditionalTask`, `lifecyclePhaseIn`; re-type `conditionalTasks`; rewrite `AgentTaskFor`, `GateTaskFor`, `isGateTask`, `PhaseForTask`, `TasksForProfile`; add `lifecyclePhaseTasks`
- Modify: `server/internal/arch_test.go` — remove `"ExitCriterionFor"`, `"IsConditionalTask"`, `"LabelForTask"`, `"TaskLabelFor"`, `"TasksForPhase"` from the projectstate allowlist and their lines from the block comment's caller inventory (L541–572)
- Modify: `server/cmd/backfill-attempts/main.go` — `attemptsFor` (L853–858) drops the now-vacuous conditional filter
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` — `constructionLedger` (L8967–8979); delete `TestTasksForPhase_MatchesFigureA2Grouping`, `TestTasksForPhase_TwelveTasksTotal`, `TestIsConditionalTask_OnlySomeConstructionAndTestClient`, `TestGeneratedTasksAllCarryALabel`; rewrite `TestPhaseForTask_RoundTrips`, `TestAgentTaskFor_IsTheSingleAIWorkTaskPerPhase` (L9040–9062), the `TasksForPhase` cross-check at L8728, `TestProfileWords_TotalOverExactlyTheProfilesPhases` and the four `TestTaskLabelFor_*` tests; add the unambiguity tests
- Modify: `server/internal/manager/construction/manager_test.go` — `passedLedger` (L1786–1798)
- Modify: `server/internal/manager/systemdesign/manager_test.go` — `pendingLedger` (L11602–11622)
- Modify: `webApp/src/components/construction/lifecycleProfiles.test.ts` — drop the two `deepEqual`-against-the-old-table tests and their import

**Interfaces:**
- Consumes: `methodassets.Lifecycles()`; `LifecycleKeyFor`, `dispatchTaskIn` (Task 3).
- Produces (signatures UNCHANGED): `AgentTaskFor(ActivityMethodPhase) MethodTask`, `GateTaskFor(ActivityMethodPhase) MethodTask`, `PhaseForTask(MethodTask) ActivityMethodPhase`, `TasksForProfile(Profile) []MethodTask`. New unexported: `var lifecyclePhaseTasks map[ActivityMethodPhase]lifecyclePhaseTaskPair`, `type lifecyclePhaseTaskPair struct{ work, gate MethodTask }`, `func buildLifecyclePhaseTasks() map[...]...`.
- Removes: `TasksForPhase`, `TaskLabelFor`, `ExitCriterionFor`, `LabelForTask`, `IsConditionalTask` (exported); `phaseTasks`, `gateTasks`, `taskLabels`, `lifecyclePhaseIn` (unexported); `cmd/gen-uiprofiles`; `make gen-uiprofiles`, `make gen-uiprofiles-check`.
- Behaviour note: `TasksForProfile` now returns only a profile's work and gate tasks — exactly what its one caller (`cmd/backfill-attempts`) got after filtering, in the same order, so the backfill's output is unchanged.

- [ ] **Step 1: Write the failing test.** Append to `server/internal/resourceaccess/projectstate/access_test.go`, after `TestCommandFor_IsEmptyForAPhaseTheProfileDoesNotCarry` (Task 3). This is what licenses the phase-keyed fold; without it the fold silently picks whichever lifecycle the data happens to list last.

  ```go
  // A phase id names ONE work task and ONE gate task across every lifecycle the platform
  // ships. That is what lets AgentTaskFor and GateTaskFor take a phase and no activity
  // type — the signature the workflow's phase walk and App A's completion rule both need.
  // A release that made two lifecycles disagree about a phase id would otherwise be
  // resolved silently, by map-insertion order.
  func TestLifecyclePhaseTasksAreUnambiguous(t *testing.T) {
  	type pair struct{ work, gate, source string }
  	seen := map[string]pair{}
  	for _, lc := range methodassets.Lifecycles() {
  		for _, ph := range lc.Phases {
  			got := pair{dispatchTaskIn(lc, ActivityMethodPhase(ph.ID)).ID, ph.Gate, lc.Type}
  			prev, held := seen[ph.ID]
  			if held && (prev.work != got.work || prev.gate != got.gate) {
  				t.Errorf("phase %q: %s says work=%q gate=%q, %s says work=%q gate=%q",
  					ph.ID, prev.source, prev.work, prev.gate, got.source, got.work, got.gate)
  			}
  			if !held {
  				seen[ph.ID] = got
  			}
  		}
  	}
  }

  // The same, one level down: a task id belongs to ONE phase across every lifecycle, which
  // is what makes PhaseForTask's denormalized stamp on a TaskAttempt well defined.
  func TestLifecycleTasksBelongToOnePhase(t *testing.T) {
  	seen := map[string]string{}
  	for _, lc := range methodassets.Lifecycles() {
  		for _, task := range lc.Tasks {
  			if prev, held := seen[task.ID]; held && prev != task.Phase {
  				t.Errorf("task %q is in phase %q and in phase %q", task.ID, prev, task.Phase)
  			}
  			seen[task.ID] = task.Phase
  		}
  	}
  	for task, p := range conditionalTasks {
  		if p == "" {
  			continue
  		}
  		if _, isNode := seen[string(task)]; isNode {
  			t.Errorf("%q is recorded as a sub-attempt of %q but the data carries a node for it", task, p)
  		}
  	}
  }
  ```

- [ ] **Step 2: Run them; confirm the second fails to compile.**
  ```bash
  cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ \
    -run 'TestLifecyclePhaseTasksAreUnambiguous|TestLifecycleTasksBelongToOnePhase' -count=1 2>&1 | head
  ```
  Expected: a build failure — `invalid operation: p == "" (mismatched types bool and untyped string)` — because `conditionalTasks` is still `map[MethodTask]bool`. `TestLifecyclePhaseTasksAreUnambiguous` compiles and would pass; that is fine, it is the guard the next step installs, not its driver.

- [ ] **Step 3: Delete the generator and everything that points at it.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator
  git rm -r server/cmd/gen-uiprofiles
  git rm webApp/src/components/construction/lifecycleTemplates.gen.ts \
         webApp/src/components/construction/lifecycleTemplates.gen.test.ts
  ```
  Then by hand:
  - `server/Makefile` L3: remove `gen-uiprofiles gen-uiprofiles-check` from the `.PHONY` list.
  - `server/Makefile` L81: `gen: gen-models gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-lifecycles`
  - `server/Makefile` L154–168: delete the `# gen-uiprofiles …` comment block, the `gen-uiprofiles:` target, the `# gen-uiprofiles-check …` comment block and the `gen-uiprofiles-check:` target. The `gen-lifecycles-check` comment references it — change "the same regenerate-then-diff shape as gen-uiprofiles-check" to "the same regenerate-then-diff shape as the other gen-*-check gates".
  - `.github/workflows/server-checks.yml` L145–150: delete the four comment lines and the `- name: Generated UI lifecycle-profile drift check` step with its `run:`.
  - `server/internal/repostructure_test.go` L50: delete `"gen-uiprofiles":       true,`.

- [ ] **Step 4: Fold the task tables onto the data** — `server/internal/resourceaccess/projectstate/projectstateaccess.go`.

  Delete `phaseTasks` (L8805–8813), `gateTasks` (L8815–8823), `TasksForPhase` (L8850–8860), `taskLabels` (L8903–8929 with its comment), `LabelForTask` (L8930ish), `TaskLabelFor` and `ExitCriterionFor` (the Task 3 rewrites), `lifecyclePhaseIn` (added in Task 3, now unused), and `IsConditionalTask`. Re-type `conditionalTasks` and add the derived table:

  ```go
  // lifecyclePhaseTasks is the phase-keyed work/gate pair, folded once over EVERY
  // lifecycle the pinned method-assets ships. phaseTasks and gateTasks used to state the
  // same thing as hand-authored maps here; the data states it per lifecycle now.
  //
  // A PHASE ID keys it, not a (type, phase) pair, for two reasons. The callers that ask
  // hold a phase and no activity type — the workflow's phase walk (AgentTaskFor) and App
  // A's completion rule (phaseCompleteFromAttempts → GateTaskFor) — and the data says the
  // answer does not vary: the eleven construction lifecycles share the canonical five
  // phase ids and name the same work and gate task under each, while the three design
  // lifecycles use phase ids that collide with none of them.
  // TestLifecyclePhaseTasksAreUnambiguous is what keeps that true; without it a release
  // that made two lifecycles disagree would be resolved silently by insertion order.
  var lifecyclePhaseTasks = buildLifecyclePhaseTasks()

  type lifecyclePhaseTaskPair struct{ work, gate MethodTask }

  func buildLifecyclePhaseTasks() map[ActivityMethodPhase]lifecyclePhaseTaskPair {
  	out := map[ActivityMethodPhase]lifecyclePhaseTaskPair{}
  	for _, lc := range methodassets.Lifecycles() {
  		for _, ph := range lc.Phases {
  			p := ActivityMethodPhase(ph.ID)
  			out[p] = lifecyclePhaseTaskPair{
  				work: MethodTask(dispatchTaskIn(lc, p).ID),
  				gate: MethodTask(ph.Gate),
  			}
  		}
  	}
  	return out
  }

  // conditionalTasks is the sub-attempt rule of the Figure A-1 vocabulary: the two tasks
  // that are NOT nodes of any lifecycle. someConstruction is Löwy's pre-design spike and
  // testClient is Construction's tandem partner; spec §3 keeps both as conditional
  // sub-attempts of their phase's work task, so lifecycles.json carries no node for either
  // and lifecyclePhaseTasks cannot answer for them — but the attempt ledger does record
  // them, and every TaskAttempt carries a denormalized Phase. The value is the lifecycle
  // phase such an attempt belongs to; "" means the task IS a lifecycle node.
  //
  // Listed exhaustively (all twelve MethodTask values, not just the two) so `exhaustive`
  // (check: [switch, map] in .golangci.yml) fails the build the moment a thirteenth task
  // is added without a conscious call, rather than defaulting it to "a real node".
  var conditionalTasks = map[MethodTask]ActivityMethodPhase{
  	TaskSRS:              "",
  	TaskSRSReview:        "",
  	TaskSTP:              "",
  	TaskSTPReview:        "",
  	TaskSomeConstruction: MethodPhaseDetailedDesign,
  	TaskDetailedDesign:   "",
  	TaskDesignReview:     "",
  	TaskConstruction:     "",
  	TaskTestClient:       MethodPhaseConstruction,
  	TaskCodeReview:       "",
  	TaskIntegration:      "",
  	TaskTesting:          "",
  }
  ```

  Rewrite the four derivations (each keeps its existing doc comment; only the body and the sentence naming the old table change):

  ```go
  // GateTaskFor returns the task whose success IS the phase's binary exit criterion.
  func GateTaskFor(p ActivityMethodPhase) MethodTask { return lifecyclePhaseTasks[p].gate }

  // AgentTaskFor returns the phase's single AI-WORK task: the task an agent is dispatched
  // to do, which the phase's gate then reviews. "" for a phase outside the vocabulary, for
  // which no attribution is possible at all.
  //
  // (keep the existing paragraph explaining why a dispatch must not stamp the gate task)
  func AgentTaskFor(p ActivityMethodPhase) MethodTask { return lifecyclePhaseTasks[p].work }

  // isGateTask reports whether a task is some lifecycle phase's binary exit criterion.
  func isGateTask(t MethodTask) bool {
  	for _, pair := range lifecyclePhaseTasks {
  		if pair.gate == t {
  			return true
  		}
  	}
  	return false
  }

  // PhaseForTask returns the lifecycle phase a task belongs to: the phase whose work or
  // gate it is, or — for the two sub-attempt tasks, which no lifecycle carries a node for
  // — the phase their work task belongs to. The empty phase when the task is unknown.
  func PhaseForTask(t MethodTask) ActivityMethodPhase {
  	for p, pair := range lifecyclePhaseTasks {
  		if pair.work == t || pair.gate == t {
  			return p
  		}
  	}
  	return conditionalTasks[t]
  }

  // TasksForProfile returns every task an activity with this profile can have as a
  // lifecycle NODE: each phase's work task then its gate task, in phase order. This is the
  // ROW SET of the list view: it is derived from the profile, never from storage, so the
  // tasks that have not happened still render. The two sub-attempt tasks are excluded by
  // construction rather than by a filter — they are not nodes (see conditionalTasks).
  func TasksForProfile(pr Profile) []MethodTask {
  	out := make([]MethodTask, 0, 2*len(pr.Phases))
  	for _, ph := range pr.Phases {
  		pair := lifecyclePhaseTasks[ph.Phase]
  		if pair.work != "" {
  			out = append(out, pair.work)
  		}
  		if pair.gate != "" {
  			out = append(out, pair.gate)
  		}
  	}
  	return out
  }
  ```

- [ ] **Step 5: Drop the vacuous filter in the backfill** — `server/cmd/backfill-attempts/main.go`, `attemptsFor` (L853–858). `TasksForProfile` no longer emits a sub-attempt task, so the guard is dead code that reads as though it might fire:
  ```go
  	// TasksForProfile emits a profile's lifecycle NODES only: the sub-attempt tasks
  	// (someConstruction, testClient) are not among them, which is the right answer here
  	// for the reason the paragraph above gives — inventing one would assert a pre-design
  	// spike or a test client that may never have existed.
  	for _, task := range projectstate.TasksForProfile(projectstate.ProfileFor(typ, variant)) {
  		if v.IntegrationPending != "" && projectstate.PhaseForTask(task) == projectstate.MethodPhaseIntegration {
  			continue
  		}
  ```
  Keep the rest of the loop body byte-identical. `cmd/backfill-attempts/main_test.go` pins the tool's output; it must pass unedited.

- [ ] **Step 6: Rewrite the three ledger helpers in the test suites.** All three build a backfill-shaped ledger by walking `TasksForPhase` and filtering conditionals — exactly what `TasksForProfile` now does, but they are keyed by phase, not by profile. Replace the inner loop in each with the phase's two node tasks:

  `server/internal/resourceaccess/projectstate/access_test.go`, `constructionLedger` (L8967–8979):
  ```go
  		for _, task := range []MethodTask{AgentTaskFor(ph), GateTaskFor(ph)} {
  			out = append(out, constructionAttempt(activityID, task, 1, OutcomePassed))
  		}
  ```
  `server/internal/manager/construction/manager_test.go`, `passedLedger` (L1788–1796):
  ```go
  		for _, task := range []projectstate.MethodTask{
  			projectstate.AgentTaskFor(ph), projectstate.GateTaskFor(ph),
  		} {
  			out = append(out, ledgerAttempt(activityID, task, 1, projectstate.OutcomePassed))
  		}
  ```
  `server/internal/manager/systemdesign/manager_test.go`, `pendingLedger` (L11604–11620): same substitution, keeping the inline `TaskAttempt` literal. Update each helper's doc comment: "one passed attempt per non-conditional task" becomes "one passed attempt per lifecycle node task (work, then gate)".

- [ ] **Step 7: Rewrite the projectstate tests that walked the deleted tables** — `server/internal/resourceaccess/projectstate/access_test.go`.
  - Delete `TestTasksForPhase_MatchesFigureA2Grouping` (L8972–8994) and `TestTasksForPhase_TwelveTasksTotal` (L8996–9009): the grouping is data now and `TestLifecyclePhaseTasksAreUnambiguous` + `TestLifecycleTasksBelongToOnePhase` hold it. `TestTasksForProfile_PerTypeTaskSets` (L9110) still pins the per-type SET and must pass unedited — its expectations already list node tasks only.
  - Delete `TestIsConditionalTask_OnlySomeConstructionAndTestClient` (L9065–9075) and `TestGeneratedTasksAllCarryALabel` (L9077–9090) — the first tests a deleted function, the second a deleted table that only `gen-uiprofiles` read.
  - `TestAgentTaskFor_IsTheSingleAIWorkTaskPerPhase` (L9040–9062): the "exactly one candidate" loop over `TasksForPhase` goes; what survives is the per-phase table, `isGateTask(want) == false`, and the unknown-phase case. Replace the candidate loop with
    ```go
    		if got := GateTaskFor(phase); got == want {
    			t.Errorf("phase %v names %q as both its work and its gate", phase, want)
    		}
    ```
  - `TestPhaseForTask_RoundTrips` (L9096–9108): walk `[]MethodTask{AgentTaskFor(p), GateTaskFor(p)}` instead of `TasksForPhase(p)`, and add the two sub-attempt tasks:
    ```go
    	if got := PhaseForTask(TaskSomeConstruction); got != MethodPhaseDetailedDesign {
    		t.Errorf("PhaseForTask(someConstruction) = %v, want detailed_design", got)
    	}
    	if got := PhaseForTask(TaskTestClient); got != MethodPhaseConstruction {
    		t.Errorf("PhaseForTask(testClient) = %v, want construction", got)
    	}
    ```
  - The `TasksForPhase` cross-check at L8728 (inside the attempt-stamp test): replace `slices.Contains(TasksForPhase(a.Phase), a.Task)` with `PhaseForTask(a.Task) == a.Phase`, and its message with `attempt %q stamped Phase %v, but the task belongs to %v`.
  - `TestProfileWords_TotalOverExactlyTheProfilesPhases` (Task 3 Step 5b, i.e. Step 5's second rewrite) and `TestProfileVocabulary_PhaseLabelNamesOneOfItsTasks` (L9219–9237) read `TaskLabelFor`, and `TestTaskLabelFor_TestingReadsItsPhaseLabel` / `_ServiceReadsTheBook` / `_NonServiceProfilesUseTheirOwnWords` / the exit-criterion tests at L9248–9302 read `TaskLabelFor` / `ExitCriterionFor` / `LabelForTask`. Those four functions are gone from the server; the claims they make — per-profile words, distinct from the book's and from each other's — belong to the DATA and are already held by the platform's own `lifecycles_test.go` and by `webApp/src/components/construction/lifecycleProfiles.test.ts`. **Delete all of them**, and fold what is still this package's business into `TestProfileWords_TotalOverExactlyTheProfilesPhases`, narrowed to what ProfileFor states:
    ```go
    // Every phase a profile carries is whole: a label, a weight and a work/gate pair that
    // are two different tasks. The per-profile WORDS (a test plan's construction gate is
    // "Scenario Review", not "Code Review") left this package with TaskLabelFor; they are
    // the platform data's business now, held by method-assets' own lifecycles_test.go and
    // by the SPA's lifecycleProfiles.test.ts.
    func TestProfileWords_TotalOverExactlyTheProfilesPhases(t *testing.T) {
    	for _, pr := range allProfiles {
    		sum := 0
    		for _, ph := range ProfileFor(pr.typ, pr.variant).Phases {
    			sum += ph.Weight
    			work, gate := AgentTaskFor(ph.Phase), GateTaskFor(ph.Phase)
    			if ph.Label == "" || work == "" || gate == "" {
    				t.Errorf("%s: phase %q is incomplete: label=%q work=%q gate=%q", pr.name, ph.Phase, ph.Label, work, gate)
    			}
    			if work == gate {
    				t.Errorf("%s: phase %q names its work and its gate the same (%q)", pr.name, ph.Phase, work)
    			}
    		}
    		if sum != 100 {
    			t.Errorf("%s: weights sum to %d, want 100", pr.name, sum)
    		}
    	}
    }
    ```
  - [ ] **Verify first:** grep the package for every remaining reference before deleting — `grep -n 'TasksForPhase\|TaskLabelFor\|ExitCriterionFor\|LabelForTask\|IsConditionalTask' server/internal/resourceaccess/projectstate/access_test.go` — and handle each hit. The list above was taken from the tree at `c10ba4d5`; a test added since is not in it.

- [ ] **Step 8: Trim the allowlist** — `server/internal/arch_test.go`. Delete `"ExitCriterionFor"`, `"IsConditionalTask"`, `"LabelForTask"`, `"TaskLabelFor"` and `"TasksForPhase"` from the `internal/resourceaccess/projectstate` entry, and delete their five lines from the caller inventory in the block comment above (L541–572, the `TasksForPhase → cmd/gen-uiprofiles …` run). Add one line recording why the block shrank:
  ```
  //	gen-uiprofiles is gone (stage 2): the SPA's lifecycle table is rendered from
  //	method-assets by cmd/gen-lifecycles, not from this package, so the five names
  //	whose only outside caller it was left with it.
  ```

- [ ] **Step 9: Trim the webApp adapter test** — `webApp/src/components/construction/lifecycleProfiles.test.ts`. Delete the `lifecycleTemplates.gen.ts` import and the two tests that `deepEqual` against it (`every kind profile equals the table the console renders today`, `every testing variant profile equals the table it renders today`). The four invariant tests below them stay and are now the whole file. Update the module doc's first paragraph to drop the "while BOTH generated tables exist" framing.

- [ ] **Step 10: Run the suites.**
  ```bash
  cd server
  GOWORK=off go build ./...
  GOWORK=off go test ./internal/resourceaccess/projectstate/ ./internal/manager/construction/ ./internal/manager/systemdesign/ ./cmd/backfill-attempts/ -count=1
  ```
  Expected: `ok` for all four. `cmd/backfill-attempts/main_test.go` and `TestTasksForProfile_PerTypeTaskSets` pass **unedited** — they are the proof that the derived task set equals the hand-authored one.

- [ ] **Step 11: Gates.**
  ```bash
  cd server
  GOWORK=off make fix-check
  GOWORK=off make lint
  GOWORK=off make test-short
  GOWORK=off go test ./internal/ -run 'TestMethodLayering|TestFileLayout|TestGeneratedOnlyPublic|TestRepoStructureCmdIsClosed|TestNoBannedPhaseIdentifier' -count=1
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-lifecycles-check
  GOWORK=off make sumtype-check method-check encapsulation-check derived-plan-check
  cd ../webApp && npm run check
  ```
  All green. Note `gen-uiprofiles-check` is GONE from the list — if `make` still accepts it, a Makefile edit was missed. `TestRepoStructureCmdIsClosed` fails if the `allowedCmd` entry outlived the directory or vice versa; `encapsulation-check` fails if a removed export is still allowlisted or a kept one is not.
  - [ ] **Verify first:** `grep -rn "gen-uiprofiles\|lifecycleTemplates" --include="*.go" --include="Makefile" --include="*.yml" --include="*.ts" --include="*.tsx" .` from the repo root (excluding `node_modules` and `.aiarch/state/project.json`, whose Task-17 gate-run note is a historical record and is not edited). Expected: no hits.

- [ ] **Step 12: Commit.**
  ```bash
  git add -A server/Makefile server/cmd server/internal webApp/src/components/construction .github/workflows/server-checks.yml
  git commit -F - <<'MSG'
  refactor: cut gen-uiprofiles and the last hand-authored Figure A-1 tables

  The generator's last job was proving the previous commit moved no word; the SPA
  reads the lifecycles from method-assets now, so cmd/gen-uiprofiles, the table it
  wrote, its Makefile targets, its CI step and its repo-structure entry all go.
  Five exports whose only outside caller it was — TasksForPhase, TaskLabelFor,
  ExitCriterionFor, LabelForTask, IsConditionalTask — go with it.

  phaseTasks and gateTasks follow profileRows onto the data: one phase-keyed fold
  over every shipped lifecycle, licensed by a new test that a phase id names the
  same work and gate task in all of them. conditionalTasks stays, re-typed to name
  the phase a sub-attempt belongs to — someConstruction and testClient are
  sub-attempts of their phase's work task, not nodes, so no lifecycle carries them
  and PhaseForTask still has to answer for them. The backfill's output and the
  per-type task sets are unchanged, pinned by their existing tests, unedited.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

## Part A self-review

| # | Requirement | Where it lands |
|---|---|---|
| 1 | Inventory every reader and decide per reader | Every reader found by grep over the tree at `c10ba4d5` is named with a file and line, and each is either re-pointed or deleted. **Re-pointed:** `constructionmanager.go:1630` (`ProfileFor(...).PhaseIDs()` seeding `Phases`), `:2733` (`AgentTaskFor` in `appendRunningAttempt`), `:2766` (`GateTaskFor` in `appendGateAttempts`), `:2672`/`:2695` (`PhaseForTask` in `normalizeAttempts`/`parseAttemptRef`), `constructactivity.go:112` (`CommandFor`), `:1096` (`walkPhases`' `PhaseIDs()` seed), `:1215` (`seedResumeFromLedger`), `:1384` (`AgentTaskFor`) — all keep their call unchanged because Tasks 3 and 4 keep every signature; `projectstateaccess.go:7512` (`phaseCompleteFromAttempts` → `GateTaskFor`), `:7638` (`phaseSetFor`), `:8347` (`ResolveConstructionRow` → `ResolvePhaseCompletions`), and `CoarsePhaseFor`/earned value, all unchanged for the same reason; `cmd/backfill-attempts/main.go:855` re-pointed and its filter dropped (Task 4 Step 5); the three test ledger helpers (Task 4 Step 6). **Deleted:** `cmd/gen-uiprofiles` and the five exports it alone read (Task 4 Steps 3, 4, 8); the five webApp consumers re-pointed at `lifecycles.gen.ts` (Task 2). `systemdesign` / `projectdesign` read only `DesignCommandFor`, which is not a retiring symbol — verified by grep, nothing to do. Layer check settled from evidence, not assumption: `TestMethodLayering` admits `github.com/mixofreality-studio/…` for every layer, and `resourceaccess/agenticjob` + `resourceaccess/sourcecontrol` already import method-assets in production, so the adapter belongs in the RA beside the enums it keys on (Task 1 Files note). |
| 2 | `lifecycleTypeKey` moves to ONE production home; both stage-0 copies die | Task 1 in full: `projectstate.LifecycleKeyFor`, exported and allowlisted; `construction.lifecycleTypeKey` and the `access_test.go` copy both deleted. |
| 3 | `ProfileFor`/`Profile` replaced by an adapter over `methodassets.LifecycleFor`, no behaviour change on the rails | Task 3 Steps 3–4: `lifecycleFor` + `dispatchTaskIn` + `lifecyclePhaseIn` under an unchanged `ProfileFor`/`CommandFor`/`ExitCriterionFor`/`TaskLabelFor`; Task 4 Step 4 adds the phase-keyed `lifecyclePhaseTasks` behind unchanged `AgentTaskFor`/`GateTaskFor`/`PhaseForTask`/`TasksForProfile`. `Profile`/`ProfilePhase` keep their generated shape, so `walkPhases`, `ResolvePhaseCompletions`, `CoarsePhaseFor` and earned value are not touched and no contract is amended. The workflow stays linear over `PhaseIDs()`; the DAG walker is stage 4. Task 3 Step 8 makes `gen-uiprofiles-check` the byte-identity proof. |
| 4 | `gen-uiprofiles` deleted with its Makefile targets, CI step and allowlist entry; `lifecycleTemplates.gen.ts` + test deleted; five consumers re-pointed | Task 2 (the five: `graph/laneSpine.ts`, `detail/detailPaneState.ts`, `detail/bodies/taskBriefing.ts`, `detail/bodies/bodyDispatch.ts` and the fifth, **`list/activityTree.ts`**, found by grep) through `lifecycleProfiles.ts`; Task 4 Step 3 does the cut. The goldens under `graph/`, `list/` and `detail/` are required to pass **unedited** (Task 2 Step 10), and Task 2 Step 1's field-for-field `deepEqual` against the old table is what makes that safe. |
| 5 | Parity test retired; a smaller invariant kept | Task 3 Step 6 retires `TestLifecyclesParity_EveryProfileEqualsItsLifecycle` and its helpers (keeping the two design-rail parity tests, which hold the data against the still-hand-authored `DesignCommandFor`/`Phase1RequiredKinds` and which Part B builds on). Task 3 Step 1 adds `TestEveryActivityTypeResolvesToALifecycle` and `TestEveryLifecycleIsReachableFromAnActivityType` — both directions, with `designLifecycleKeys` marked as Part B's to remove. Task 4 Step 1 adds the two fold-licensing invariants. |
| 6 | MethodTask constants stay; `conditionalTasks` semantics preserved | The twelve `MethodTask` constants and `AttemptID` are untouched throughout. Task 4 Step 4 keeps `conditionalTasks`, re-typed to `map[MethodTask]ActivityMethodPhase` so the sub-attempt fact and its phase are stated once, with the exhaustive-over-twelve discipline intact; `TestLifecycleTasksBelongToOnePhase` asserts no lifecycle carries a node for either. `foldConditional` (`constructionmanager.go:2929`) is untouched and unaffected — verified by reading it: it folds any attempt in a phase that is neither the work nor the gate task, and never consults `IsConditionalTask`. The webApp keeps rendering both rows (Task 2's `PHASE_TASK_ORDER`), and `TasksForProfile`'s new node-only result is byte-identical to what the backfill got after filtering. |

**Out of Part A's scope, by instruction:** the review engine, `EffectiveGate`, the M0 human floor, plan derivation's activities 1–3 + M0 prefix, `ClassifyActivity` and the `ActivityType` enum — all Part B. Part A deliberately leaves `designLifecycleKeys` (Task 3 Step 1) as the hook Part B removes when it appends the three types.

---

### Task 6: Append the three design `ActivityType` values (vocabulary only)

This task adds the words and nothing else: no activity is derived, classified or dispatched
yet. It lands green on its own because every consumer either already reads method-assets
(Part A) or gets an explicit arm here.

**Consumed from Part A (do not re-do):** `projectstate.LifecycleKeyFor(t ActivityType, v TestingVariant) string`
is the one production home of the type key, and `projectstate.ProfileFor` / `CommandFor` /
`cmd/gen-uiprofiles` read the pinned method-assets `lifecycles.json` instead of the deleted
`profileRows`/`phaseTasks`/`gateTasks`. method-assets v0.9.0 already ships the three design
lifecycles — `requirements` (4 phases `mission`/`glossary`/`volatilities`/`coreUseCases`, 8
tasks = 4 dispatch→review pairs), `architecture` (1 phase `architecture`, 2 tasks), and
`projectDesign` (1 phase `sdp`, 1 review task `sdpReview`, no dispatch) — verified in
`webApp/src/components/activity/lifecycles.gen.ts:74-253`. Nothing in this task changes that data.

**Files:**
- Modify (hand-edit, self-amendment procedure): `.aiarch/state/project.json` — the `ActivityType`
  `$def` in **three** contracts, which are byte-identical copies of one vocabulary and must move
  together: `.serviceContracts.projectStateAccess["$defs"].ActivityType`,
  `.serviceContracts.gitActivityStatusAccess["$defs"].ActivityType`,
  `.serviceContracts.systemDesignManager["$defs"].ActivityType`. (Verified: those are the only
  three — `constructionManager` carries none.)
- Regenerate (never hand-edit): `server/internal/resourceaccess/projectstate/contract.gen.go`,
  `.../toolcatalog.gen.go`, `server/internal/resourceaccess/gitactivitystatus/contract.gen.go`,
  `server/internal/manager/systemdesign/contract.gen.go`, each package's `fake/fake.gen.go`,
  `server/api/openapi.yaml`, `server/internal/client/{web,mcp}/**`, `systemtests/internal/sdk/**`,
  `webApp/src/contracts/schema.ts`, `webApp/src/contracts/enums.gen.ts`,
  `webApp/src/components/construction/lifecycleTemplates.gen.ts`.
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `activityTypeNames`
  (L7122–7130), the per-value doc block (~L7740–7764), `String()` (L7766–7788), `DeriveProduced`'s
  type switch (L8652+), `profileSlug` (L8722+).
- Modify: `server/cmd/gen-uiprofiles/main.go` — the profile table (L60–68).
- Modify: `server/internal/manager/construction/constructactivity.go` — `reviewArtifactKindFor`
  (L2276–2293). Task 8 deletes this function; here it only needs an arm so `exhaustive` passes.
- Modify: `webApp/src/components/construction/KindBadge.tsx` — `ActivityKind` union (L19–26),
  `KIND_META` (L28–36), `kindColor` (L39–55), `kindIcon` (L58+).
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` (the enum round-trip at
  L6217 counts `len(activityTypeNames)`), `server/internal/manager/construction/manager_test.go`
  (`Test_ReviewArtifactKindFor_IsTotalOverEveryDispatchablePhase`, L7590-7620, iterates a hardcoded
  seven-type slice).

**Interfaces:**
- Consumes: `projectstate.LifecycleKeyFor`, `projectstate.ProfileFor`, `methodassets.LifecycleFor` (Part A).
- Produces: `projectstate.ActivityTypeRequirements = 7`, `ActivityTypeArchitecture = 8`,
  `ActivityTypeProjectDesign = 9` with wire names `"requirements"`, `"architecture"`,
  `"projectDesign"` — the same strings that key `lifecycles.json`, so `LifecycleKeyFor` needs no
  special case. **Ordinals 0–6 are never renumbered** (the stored `.activityConstruction` rows and
  the committed plan decode by wire name, with a legacy-ordinal fallback in `UnmarshalJSON`).
- The exact `$def`, identical in all three contracts:
  ```json
  "ActivityType": {
    "type": "integer",
    "enum": [0, 1, 2, 3, 4, 5, 6, 7, 8, 9],
    "x-enum-varnames": [
      "ActivityTypeService", "ActivityTypeFrontend", "ActivityTypeTesting",
      "ActivityTypeDeployment", "ActivityTypeDocumentation", "ActivityTypeUIDesign",
      "ActivityTypeIntegration", "ActivityTypeRequirements", "ActivityTypeArchitecture",
      "ActivityTypeProjectDesign"
    ],
    "x-go-base": "int"
  }
  ```

- [ ] **Step 1: Write the failing tests.**

  (a) In `server/internal/resourceaccess/projectstate/access_test.go`, beside the existing enum
  round-trip block (~L6210):

  ```go
  // The three design activity types (spec 2026-09-20 §5.1). Their wire names ARE the
  // method-assets lifecycle keys, so a plan activity of this type resolves its lifecycle
  // with no special case. Ordinals 7/8/9 are appended, never inserted.
  func TestActivityType_DesignTypesRoundTripAndKeyTheirLifecycles(t *testing.T) {
  	cases := []struct {
  		typ  ActivityType
  		name string
  	}{
  		{ActivityTypeRequirements, "requirements"},
  		{ActivityTypeArchitecture, "architecture"},
  		{ActivityTypeProjectDesign, "projectDesign"},
  	}
  	for _, c := range cases {
  		if got := c.typ.String(); got != c.name {
  			t.Errorf("%d.String() = %q, want %q", int(c.typ), got, c.name)
  		}
  		raw, err := json.Marshal(c.typ)
  		if err != nil {
  			t.Fatalf("marshal %s: %v", c.name, err)
  		}
  		if string(raw) != `"`+c.name+`"` {
  			t.Errorf("marshal %s = %s, want %q", c.name, raw, c.name)
  		}
  		var back ActivityType
  		if err := json.Unmarshal(raw, &back); err != nil || back != c.typ {
  			t.Errorf("round trip %s: got %v, %v", c.name, back, err)
  		}
  		if got := LifecycleKeyFor(c.typ, TestVariantPlan); got != c.name {
  			t.Errorf("LifecycleKeyFor(%s) = %q, want %q", c.name, got, c.name)
  		}
  		if len(ProfileFor(c.typ, TestVariantPlan).Phases) == 0 {
  			t.Errorf("%s has no lifecycle phases — the method-assets pin does not carry it", c.name)
  		}
  	}
  	if len(activityTypeNames) != 10 {
  		t.Fatalf("activityTypeNames holds %d types, want 10", len(activityTypeNames))
  	}
  }

  // The phase shapes the three design lifecycles publish, pinned so a method-assets
  // release that reshapes them fails here rather than silently reshaping the console.
  func TestProfileFor_DesignLifecycleShapes(t *testing.T) {
  	want := map[ActivityType][]string{
  		ActivityTypeRequirements: {"mission", "glossary", "volatilities", "coreUseCases"},
  		ActivityTypeArchitecture: {"architecture"},
  		ActivityTypeProjectDesign: {"sdp"},
  	}
  	for typ, ids := range want {
  		var got []string
  		for _, p := range ProfileFor(typ, TestVariantPlan).PhaseIDs() {
  			got = append(got, string(p))
  		}
  		if !slices.Equal(got, ids) {
  			t.Errorf("%s phases = %v, want %v", typ, got, ids)
  		}
  	}
  }
  ```

  (b) In `webApp/src/components/construction/` add to the nearest existing `*.test.ts` covering the
  badge (if none exists, put it in `list/activityRowPresentation.test.ts`, which already imports
  from this folder — **do not create a new test file** if the package's convention is one per
  module; `npm run check` will tell you):

  ```ts
  test('every activity kind, design types included, has a label and a colour', () => {
    const kinds: ActivityKind[] = [
      'service', 'frontend', 'testing', 'deployment', 'documentation', 'uiDesign',
      'integration', 'requirements', 'architecture', 'projectDesign',
    ];
    for (const k of kinds) {
      assert.ok(KIND_META[k]?.label, `${k} has no label`);
      assert.ok(kindColor(lightTokens, k).fg, `${k} has no colour`);
    }
  });
  ```

- [ ] **Step 2: Run them; confirm they fail.**
  ```bash
  cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestActivityType_DesignTypes|TestProfileFor_DesignLifecycleShapes' -count=1
  cd ../webApp && npm run test
  ```
  Expected: Go fails to COMPILE (`undefined: ActivityTypeRequirements`) — that is the right
  failure for a vocabulary that does not exist yet. TS fails on the union.

- [ ] **Step 3: Amend the contract in all three places and regenerate.**
  ```bash
  cd server && make gen-models
  git status --short -- internal/resourceaccess/projectstate internal/resourceaccess/gitactivitystatus internal/manager/systemdesign
  ```
  - [ ] **Verify first:** open `internal/resourceaccess/projectstate/contract.gen.go` (~L101–112) and
    confirm the three new constants are appended with values 7/8/9 and that 0–6 are unchanged. If
    modelgen re-sorted the block, STOP — a renumber is a wire break.

- [ ] **Step 4: Teach the hand-written projectstate surface the three values.**
  - `activityTypeNames`: three entries, wire names as above.
  - `String()`: three cases returning the same strings. Keep the existing "defensive fallback"
    comment accurate (it now covers ten values).
  - The per-value doc block: three short paragraphs in the house voice — *requirements* is Table
    11-1 #1 (the mission→glossary→volatilities→core-use-cases chain that used to be the Phase-1
    design rail's first four steps); *architecture* is #2 (the System model with its four views);
    *projectDesign* is #3 (the deterministic SDP review, whose single task is the M0 gate).
  - `DeriveProduced`: add `case ActivityTypeRequirements, ActivityTypeArchitecture, ActivityTypeProjectDesign:`
    with an empty body and a comment — a design activity's product is a committed artifact SLOT, not a
    corpus-observable file, so it contributes no `ProducedArtifact` from corpus evidence. (Empty on
    purpose: `exhaustive` wants the arm, and inventing a produced artifact here would fabricate one.)
  - `profileSlug`: three cases returning `"requirements"`, `"architecture"`, `"projectDesign"`.
    - [ ] **Verify first:** Part A may already have deleted `profileSlug` when it re-pointed
      `CommandFor` at method-assets. If it is gone, skip this bullet and check Task 10's Step 1
      instead — that is where `CommandFor`'s design answers are pinned.

- [ ] **Step 5: Close the remaining exhaustive switches.**
  - `server/cmd/gen-uiprofiles/main.go`: three rows — `{"requirements", "REQUIREMENTS_PHASES", projectstate.ActivityTypeRequirements, projectstate.TestVariantPlan}`,
    `{"architecture", "ARCHITECTURE_PHASES", …}`, `{"projectDesign", "PROJECT_DESIGN_PHASES", …}`.
  - `constructactivity.go` `reviewArtifactKindFor`: add
    `case projectstate.ActivityTypeRequirements, projectstate.ActivityTypeArchitecture, projectstate.ActivityTypeProjectDesign:`
    with the one-line comment that the design types' reviewer rows move into the engine in Task 8 and
    that until then they fall through to `ReviewKindNoncoding` (the architect sign-off), which is
    unreachable because the pump refuses to dispatch them (Task 10).
  - `manager_test.go` `Test_ReviewArtifactKindFor_IsTotalOverEveryDispatchablePhase`: add the three
    types to its `types` slice, so the totality proof covers the new vocabulary.
  ```bash
  cd server && GOWORK=off make lint && make sumtype-check
  ```
  `exhaustive` (`.golangci.yml` `check: [switch, map]`) is what finds anything missed here — fix
  every finding in code, never with an exclusion.

- [ ] **Step 6: The webApp side.**
  ```bash
  cd server && make gen-client gen-internal-tools gen-temporal gen-sdk gen-uiprofiles gen-lifecycles
  cd ../webApp && npm run gen:api && npm run gen:ops
  ```
  `enums.gen.ts` picks the three varnames up from the OAS automatically (`ActivityType`'s
  `OUTPUT_NAMES` entry `SystemDesignActivityType` already exists; no generator edit is needed —
  and `gen-enums.mjs` throws by design if a genuinely NEW enum schema lacks an entry, so a throw
  here means something else changed).
  Then hand-extend `KindBadge.tsx`: the `ActivityKind` union, `KIND_META`
  (`requirements: { label: 'Requirements' }`, `architecture: { label: 'Architecture' }`,
  `projectDesign: { label: 'Project design' }`), `kindColor` and `kindIcon`. Reuse existing tokens —
  the three design kinds are architect-owned, so `chatArchitectFg/Bg` is the honest pick; give
  `projectDesign` the `FactCheckOutlinedIcon`/gate-flavoured icon already imported rather than
  adding a dependency.

- [ ] **Step 7: Run everything.**
  ```bash
  cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ ./internal/manager/... -count=1
  git add ../.aiarch/state/project.json . ../systemtests/internal/sdk ../webApp/src/contracts ../webApp/src/components/construction/lifecycleTemplates.gen.ts
  make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-uiprofiles-check gen-lifecycles-check derived-plan-check
  make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off make test-short && make lint && make sumtype-check && make fix-check
  cd ../webApp && npm run gen:api && npm run gen:ops && npm run check
  ```
  `derived-plan-check` must stay GREEN: nothing derives a design activity yet, so slots 9/10 do not
  move. If it goes red, the derivation changed — that is Task 9, not this task.

- [ ] **Step 8: Commit.**
  ```bash
  git commit -m "$(cat <<'EOF'
  feat(projectstate): name the three design activity types

  Requirements, Architecture and Project Design are activities at the head of the
  plan (spec 2026-09-20 §5.1). Their wire names are the method-assets lifecycle
  keys, so LifecycleKeyFor and ProfileFor resolve them with no special case.
  Ordinals 7/8/9 are appended; 0-6 are untouched. Vocabulary only: nothing
  derives, classifies or dispatches a design activity yet.

  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  EOF
  )"
  ```

---

### Task 7: One review engine — it owns WHO reviews and WHETHER a human must (spec §5.4)

Today the decision is split three ways: the Manager holds the 7×5 `(ActivityType, ActivityMethodPhase) → kind`
table (`constructactivity.go:2255-2311`), `projectStateAccess` holds the gate policy
(`ReviewPolicy.EffectiveGate`/`RequiresHuman`, `projectstateaccess.go:9460-9545`), and the two design
rails read `Preset == vibes` straight off head-state (`systemdesign/coauthorartifact.go:428`,
`projectdesign/coauthorphase2artifact.go:428`). This task moves all three into the engine behind
ONE call. The policy **data** stays in project state.

**What stays in `projectstate` (decided, so nobody has to guess):** the generated `ReviewPolicy`
type; the preset wire values `ReviewPresetVibes`/`Checkpoints`/`Full` (the write-path validator
`constructionmanager.go:1018` reads them and the webApp's PolicyPanel writes them);
`ReviewPolicyFromGateIDs` + `gateIDToPhase` (L9547-9573 — pure shaping of the client's gate-id
vocabulary INTO the stored document, not a decision); and `reviewFloorKeywords` +
`ContractTouchesReviewFloor` (L9480-9502 — it reads a `projectstate.ServiceContract`, a type no
Engine may import, and it produces the `floorTouched` boolean the engine is handed). All four keep
their `internal/arch_test.go` allowlist entries (L896-917), whose comments need one edit each: they
now feed the engine rather than decide anything.
**What moves:** the BODIES of `EffectiveGate` and `RequiresHuman` (the preset switch, the explicit-map
fallback, and which lifecycle phase the floor guards), plus the design rails' `Preset == vibes` rule.
Both methods are deleted from `projectstateaccess.go`. They are methods on a generated contract type,
so no allowlist entry is removed with them (verified: neither `ReviewPolicy.EffectiveGate` nor
`ReviewPolicy.RequiresHuman` appears in `internal/arch_test.go`).

**Files:**
- Modify (hand-edit, self-amendment procedure): `.aiarch/state/project.json` —
  `.serviceContracts.reviewEngine["$defs"]` (two new defs, `ReviewSet` widened) and
  `.serviceContracts.reviewEngine.interface.operations[0].params` (replaced wholesale).
- Regenerate: `server/internal/engine/review/contract.gen.go`,
  `server/internal/engine/review/fake/fake.gen.go`,
  `server/internal/resourceaccess/projectstate/toolcatalog.gen.go` (the internal MCP tool
  `reviewProposeReviews`).
- Modify: `server/internal/engine/review/reviewengine.go` — package doc (L1-54), the role/perspective
  const blocks (L60-83), `ProposeReviews` (L110-146), `componentScoped` (L148-160), `reviewersFor`
  (L162-219).
- Modify: `server/internal/engine/review/engine_test.go` (all five tests plus the new tables).
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — delete `RequiresHuman`
  (L9460-9463) and `EffectiveGate` (L9504-9545); keep and re-comment L9451-9459, L9465-9502, L9547-9573.
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` — the nine
  `TestReviewPolicy_*` tests (L7826-7982) move to `engine_test.go` (see Step 4);
  `TestContractTouchesReviewFloor_KeywordMatch` (L7984-8010) stays.
- Modify: `server/internal/arch_test.go` — comment-only edits on the four allowlist entries.

**Interfaces:**
- Consumes: `fweng.Context`, `fweng.New(fweng.ContractMisuse|fweng.InternalInvariant, …)`.
- Produces:
  ```go
  ProposeReviews(
  	rc fweng.Context,
  	change ReviewChange,
  	activityType ActivityType,
  	lifecyclePhase string,
  	componentID string,
  	policy ReviewPolicy,
  	floorTouched bool,
  	contracts []string,
  ) (ReviewSet, error)
  ```
  with `ReviewSet{Reviewers []Reviewer; RequiresHuman bool; Reason string; ArtifactKind ReviewArtifactKind}`.
- **`architectureGraph` is DELETED from the signature.** Every production call passed `""`
  (`constructactivity.go:2247`) and the v1 policy ignored it; a parameter that is always empty is a
  lie the compiler cannot catch. `contracts` is kept — it is really populated
  (`state.reviewContracts`) and is the input a future policy refines on.
- **`lifecyclePhase` is a bare `string`, not an enum, and that is deliberate.** A design activity's
  lifecycle phases are `mission`/`glossary`/`volatilities`/`coreUseCases`/`architecture`/`sdp`
  (method-assets), which are not members of `ActivityMethodPhase`'s closed five. A closed enum here
  would either have to absorb them — freezing platform-fixed data into a wire contract that
  `never renumber` then binds forever — or force the Manager to lie. The engine's table is total
  over the string instead, and Step 3's Manager-side parity test is what keeps the strings honest.

**The exact contract edits.**

(a) `.serviceContracts.reviewEngine["$defs"]` — add beside `ReviewChange`:
```json
"ActivityType": {
  "type": "string",
  "enum": ["service", "frontend", "testing", "deployment", "documentation", "uiDesign", "integration", "requirements", "architecture", "projectDesign"],
  "x-enum-varnames": ["ActivityTypeService", "ActivityTypeFrontend", "ActivityTypeTesting", "ActivityTypeDeployment", "ActivityTypeDocumentation", "ActivityTypeUIDesign", "ActivityTypeIntegration", "ActivityTypeRequirements", "ActivityTypeArchitecture", "ActivityTypeProjectDesign"],
  "x-go-base": "string"
},
"ReviewPolicy": {
  "type": "object",
  "properties": {
    "GatedPhasesByType": { "type": "object", "additionalProperties": { "type": ["null", "array"], "items": { "type": "string" } } },
    "Preset": { "type": "string" }
  },
  "required": ["GatedPhasesByType", "Preset"],
  "additionalProperties": false
}
```
The engine keys on the WIRE NAMES, not projectstate's ordinals: an Engine may not import
`projectstate` (F3, stage-0), the ordinals are that RA's storage concern, and the ten strings are
already the shared vocabulary (they key `lifecycles.json` too). `Preset` is a plain string here —
`""` is the legacy/explicit mode — where projectstate stores `*string`; the Manager dereferences.

(b) `.serviceContracts.reviewEngine["$defs"].ReviewSet` — replace with:
```json
"ReviewSet": {
  "type": "object",
  "properties": {
    "Reviewers": { "type": ["null", "array"], "items": { "$ref": "#/$defs/Reviewer" } },
    "RequiresHuman": { "type": "boolean" },
    "Reason": { "type": "string" },
    "ArtifactKind": { "$ref": "#/$defs/ReviewArtifactKind" }
  },
  "required": ["Reviewers", "RequiresHuman", "Reason", "ArtifactKind"],
  "additionalProperties": false
}
```
`ArtifactKind` is reported, not accepted: it keeps the generated `ReviewArtifactKind` enum (and its
`exhaustive` guard) referenced now that no parameter carries it, and it makes the engine's routing
legible on the reviewer strip (spec §7.2's "one-line reason").

(c) `.serviceContracts.reviewEngine.interface.operations[0].params` — replace the whole array:
```json
[
  { "name": "change", "schema": { "$ref": "#/$defs/ReviewChange" } },
  { "name": "activityType", "schema": { "$ref": "#/$defs/ActivityType" } },
  { "name": "lifecyclePhase", "schema": { "type": "string" } },
  { "name": "componentID", "schema": { "type": "string" } },
  { "name": "policy", "schema": { "$ref": "#/$defs/ReviewPolicy" } },
  { "name": "floorTouched", "schema": { "type": "boolean" } },
  { "name": "contracts", "schema": { "type": "array", "items": { "type": "string" } } }
]
```

**The gate table the engine now owns (every cell decided; behaviour must be IDENTICAL to today).**

| activityType | lifecyclePhase | RequiresHuman |
|---|---|---|
| any | `construction` **and** `floorTouched` | **true** — the non-overridable floor, exactly `EffectiveGate`'s first arm |
| `projectDesign` | `sdp` | **true** always — the new floor: M0 commits spend (spec §6/§5.4) |
| `requirements`, `architecture`, `projectDesign` (any other phase) | — | `preset != "vibes"` — today's design-rail rule verbatim (`vibes` ⇒ auto; `checkpoints`/`full`/`""` ⇒ human) |
| the seven construction types | — | `preset == "vibes"` ⇒ false; `"full"` ⇒ true; `"checkpoints"` ⇒ phase ∈ {`detailed_design`, `construction`, `integration`}; `""` ⇒ `policy.GatedPhasesByType[activityType]` contains the phase |

The two legacy (`""`) arms genuinely differ — construction falls back to the explicit map, design
falls back to "human". That is what the two code paths do today, and preserving the difference is
the point of the stage; a table test pins both.

**The reviewer rows for the three design types** (read off `critiqueCriticFor`,
`systemdesign/coauthorartifact.go:3547-3567`, and the lifecycle data):

| activityType | lifecyclePhase | Reviewers | ArtifactKind |
|---|---|---|---|
| `requirements` | `mission`, `glossary`, `coreUseCases` | `productManager` · perspective `businessAlignment` · ref `mission` · no amend — today's PM-critique round | `Noncoding` |
| `requirements` | `volatilities` | none — `critiqueCriticFor(KindVolatilities)` returns no critic; volatility identification is the architect's own signature skill and has never had a critique round | `Noncoding` |
| `architecture` | `architecture` | `architect` · perspective `architecture` · ref `architecture` · **may amend** — today's architect self-critique (`critiqueCriticFor(KindSystem) = ActiveRoleArchitect`) | `Noncoding` |
| `projectDesign` | any | none — the plan is computed, not drafted; the only judge is the human at M0 | `Noncoding` |

Role strings are the design rail's own wire labels — `"productManager"` / `"architect"`
(`coauthorartifact.go:3591-3594`) — so both surfaces still name the role identically. The
InternalInvariant "a recognised kind must yield ≥1 reviewer" is therefore **narrowed**, not dropped:
an empty reviewer set is legal only when nobody reviews by design (the rows above); for the seven
construction types an empty set is still an engine bug. State that in the guard's error text.

- [ ] **Step 1: Write the failing engine tests** — `server/internal/engine/review/engine_test.go`.
  They are the whole point of the task: the behaviour-preservation proof.

  ```go
  // vibes/checkpoints/full/legacy, construction side — transcribed from the nine
  // TestReviewPolicy_* cases that used to live in projectstate's access_test.go. Every
  // row here passed there before the policy moved; a diff in this table is a behaviour
  // change, not a refactor.
  func Test_ProposeReviews_ConstructionGate_MatchesTheRetiredEffectiveGate(t *testing.T) {
  	phases := []string{"requirements", "detailed_design", "test_plan", "construction", "integration"}
  	cases := []struct {
  		name    string
  		policy  ReviewPolicy
  		floor   bool
  		gated   map[string]bool // phase -> requiresHuman
  	}{
  		{"vibes gates nothing", ReviewPolicy{Preset: "vibes"}, false,
  			map[string]bool{}},
  		{"vibes still cannot bypass the floor", ReviewPolicy{Preset: "vibes"}, true,
  			map[string]bool{"construction": true}},
  		{"the floor guards only the construction dispatch", ReviewPolicy{Preset: "full"}, true,
  			map[string]bool{"requirements": true, "detailed_design": true, "test_plan": true, "construction": true, "integration": true}},
  		{"checkpoints", ReviewPolicy{Preset: "checkpoints"}, false,
  			map[string]bool{"detailed_design": true, "construction": true, "integration": true}},
  		{"full", ReviewPolicy{Preset: "full"}, false,
  			map[string]bool{"requirements": true, "detailed_design": true, "test_plan": true, "construction": true, "integration": true}},
  		{"legacy falls back to the explicit map", ReviewPolicy{GatedPhasesByType: map[string][]string{"frontend": {"detailed_design"}}}, false,
  			map[string]bool{}},
  	}
  	e := NewReviewEngine()
  	for _, c := range cases {
  		for _, p := range phases {
  			set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "C-x", ComponentID: "x"},
  				ActivityTypeService, p, "x", c.policy, c.floor, nil)
  			if err != nil {
  				t.Fatalf("%s/%s: %v", c.name, p, err)
  			}
  			if set.RequiresHuman != c.gated[p] {
  				t.Errorf("%s/%s: requiresHuman=%v, want %v (reason %q)", c.name, p, set.RequiresHuman, c.gated[p], set.Reason)
  			}
  			if set.Reason == "" {
  				t.Errorf("%s/%s: every verdict must carry its one-line reason", c.name, p)
  			}
  		}
  	}
  	// The legacy map is consulted per TYPE, so the frontend row it holds does gate.
  	set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "U-SPA-w", ComponentID: "w"},
  		ActivityTypeFrontend, "detailed_design", "w",
  		ReviewPolicy{GatedPhasesByType: map[string][]string{"frontend": {"detailed_design"}}}, false, nil)
  	if err != nil || !set.RequiresHuman {
  		t.Fatalf("the legacy explicit map must still gate frontend/detailed_design: %+v %v", set, err)
  	}
  }

  // The design rail's rule, verbatim: vibes auto-approves, everything else (including the
  // unset/legacy preset) holds for a human. This is what coauthorartifact.go:428 and
  // coauthorphase2artifact.go:428 computed inline before the engine owned it.
  func Test_ProposeReviews_DesignGate_KeepsTheVibesAutogateRule(t *testing.T) {
  	e := NewReviewEngine()
  	for _, tc := range []struct {
  		preset string
  		human  bool
  	}{{"vibes", false}, {"checkpoints", true}, {"full", true}, {"", true}} {
  		for _, cell := range []struct {
  			typ   ActivityType
  			phase string
  		}{
  			{ActivityTypeRequirements, "mission"},
  			{ActivityTypeRequirements, "glossary"},
  			{ActivityTypeRequirements, "volatilities"},
  			{ActivityTypeRequirements, "coreUseCases"},
  			{ActivityTypeArchitecture, "architecture"},
  			{ActivityTypeProjectDesign, "planningAssumptions"},
  		} {
  			set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: string(cell.typ)},
  				cell.typ, cell.phase, "", ReviewPolicy{Preset: tc.preset}, false, nil)
  			if err != nil {
  				t.Fatalf("%s/%s preset=%q: %v", cell.typ, cell.phase, tc.preset, err)
  			}
  			if set.RequiresHuman != tc.human {
  				t.Errorf("%s/%s preset=%q: requiresHuman=%v, want %v", cell.typ, cell.phase, tc.preset, set.RequiresHuman, tc.human)
  			}
  		}
  	}
  }

  // The new non-overridable floor: M0 commits spend, so the projectDesign gate always
  // holds for a human — under vibes, and with no contract and no component in sight.
  func Test_ProposeReviews_ProjectDesignGateAlwaysRequiresAHuman(t *testing.T) {
  	for _, preset := range []string{"vibes", "checkpoints", "full", ""} {
  		set, err := NewReviewEngine().ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "projectDesign"},
  			ActivityTypeProjectDesign, "sdp", "", ReviewPolicy{Preset: preset}, false, nil)
  		if err != nil {
  			t.Fatalf("preset %q: %v", preset, err)
  		}
  		if !set.RequiresHuman {
  			t.Fatalf("preset %q: the SDP review approves the plan AND its cost; no preset may bypass it", preset)
  		}
  		if len(set.Reviewers) != 0 {
  			t.Errorf("preset %q: the plan is computed, so no agent reviews it; got %+v", preset, set.Reviewers)
  		}
  	}
  }

  // The design reviewer rows, against today's critiqueCriticFor.
  func Test_ProposeReviews_DesignReviewerRows(t *testing.T) {
  	e := NewReviewEngine()
  	cases := []struct {
  		typ   ActivityType
  		phase string
  		roles []string
  		amend bool
  	}{
  		{ActivityTypeRequirements, "mission", []string{"productManager"}, false},
  		{ActivityTypeRequirements, "glossary", []string{"productManager"}, false},
  		{ActivityTypeRequirements, "volatilities", nil, false},
  		{ActivityTypeRequirements, "coreUseCases", []string{"productManager"}, false},
  		{ActivityTypeArchitecture, "architecture", []string{"architect"}, true},
  	}
  	for _, c := range cases {
  		set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "a"}, c.typ, c.phase, "",
  			ReviewPolicy{Preset: "vibes"}, false, nil)
  		if err != nil {
  			t.Fatalf("%s/%s: %v", c.typ, c.phase, err)
  		}
  		var got []string
  		for _, r := range set.Reviewers {
  			got = append(got, r.Role)
  		}
  		if !slices.Equal(got, c.roles) {
  			t.Errorf("%s/%s reviewers = %v, want %v", c.typ, c.phase, got, c.roles)
  		}
  		if len(set.Reviewers) == 1 && set.Reviewers[0].MayAmend != c.amend {
  			t.Errorf("%s/%s mayAmend = %v, want %v", c.typ, c.phase, set.Reviewers[0].MayAmend, c.amend)
  		}
  	}
  }
  ```
  Keep and re-type the three surviving stage-0 tests: `Test_ProposeReviews_PerKind` becomes a table
  over `(ActivityType, lifecyclePhase) → ArtifactKind` (it is the stage-0 Manager table, moved);
  `Test_ProposeReviews_ComponentIsRequiredOnlyWhereThereIsOne` keeps its assertions but drives them
  through `(ActivityTypeService, "detailed_design")` etc.; `Test_ProposeReviews_APhaseWireNameIsNotAKind`
  is **deleted** — with the kind no longer a parameter, the defect it pinned is unrepresentable, and
  the totality test below is what replaces it.

- [ ] **Step 2: Run them; confirm they fail.**
  ```bash
  cd server && GOWORK=off go test ./internal/engine/review/ -count=1
  ```
  Expected: a COMPILE failure (`too many arguments`, `undefined: ReviewPolicy`). If it compiles, the
  contract was edited before the tests were written — re-order.

- [ ] **Step 3: Amend the contract and regenerate.**
  ```bash
  cd server && make gen-models && make gen-internal-tools
  ```
  - [ ] **Verify first:** open `internal/engine/review/contract.gen.go` and confirm (i) the new
    `ProposeReviews` signature, (ii) `type ActivityType string` with the ten constants, (iii)
    `ReviewPolicy` with `GatedPhasesByType map[string][]string` and `Preset string`, (iv)
    `ReviewArtifactKind` and its six constants SURVIVED (they are now referenced only from
    `ReviewSet.ArtifactKind`; if modelgen prunes them, that is a generator finding — stop and report
    rather than hand-writing the type back).
  - Note the engine's generated `ActivityType` deliberately **shadows nothing**: `internal/engine/review`
    imports no `projectstate`, and `TestGeneratedOnlyPublic` accepts it because it is generated surface.

- [ ] **Step 4: Rewrite the engine.** In `reviewengine.go`:
  - Add `roleProductManager = "productManager"` to the role block and
    `perspectiveBusinessAlignment = "businessAlignment"` to the perspective block, each with the
    one-line doc the block's neighbours carry (the PM ratifies business alignment — Löwy ch. 7; the
    string matches the design rail's `critiqueRoleProductManager`).
  - `ProposeReviews` keeps its two pre-conditions (empty `ActivityID`; a component-scoped kind with
    no component) and then computes, in order: `kind := artifactKindFor(activityType, lifecyclePhase)`,
    `reviewers := reviewersFor(activityType, lifecyclePhase, kind)`,
    `human, reason := requiresHuman(activityType, lifecyclePhase, policy, floorTouched)`.
  - `artifactKindFor` is the stage-0 Manager table, moved verbatim and re-keyed on the engine's own
    `ActivityType` plus the phase string: `service`/`deployment` → design `DetailedDesign`, build
    `Construction`; `frontend`/`uiDesign` → `UIDesign`/`UICode`; `integration` → `Noncoding`/`Noncoding`;
    `testing`/`documentation` → `Noncoding` throughout; the three design types → `Noncoding`. The
    phase arm maps `requirements`/`test_plan` → `Noncoding`, `detailed_design` → design,
    `construction` → build, `integration` → `Integration`, anything else → `Noncoding`. Carry over the
    "no component degrades a component-scoped kind to Noncoding" rule (stage 0's justification
    paragraph moves with it) — the engine now owns both halves, so the mirrored-rule warning in the
    Manager's comment is deleted rather than relocated.
  - `requiresHuman` implements the four-row table above and returns the one-line `Reason` with it.
  - Delete the old package-doc paragraph that says "the Manager owns the (activity type, lifecycle
    phase) → kind table" (L51-53) and replace it with the fact: this engine answers both questions —
    who reviews, and whether a human must sign off — from the activity's type, its lifecycle phase,
    the project's committed ReviewPolicy and the floor flag; the policy DOCUMENT stays in project
    state, the policy DECISION lives here. Keep the purity contract paragraph untouched.

- [ ] **Step 5: Delete the moved bodies from `projectstateaccess.go`** (`RequiresHuman` L9460-9463,
  `EffectiveGate` L9504-9545, and `EffectiveGate`'s 22-line doc block) and re-point the surviving
  comments: L9451-9455's "It composes with the reviewEngine (which computes WHO reviews)" becomes
  "the reviewEngine computes both WHO reviews and WHETHER a human must; this type is the committed
  DOCUMENT it reads"; L9465-9468 and L9480-9491 keep the preset/floor descriptions but stop naming
  `EffectiveGate`. Move the nine `TestReviewPolicy_*` cases out of `access_test.go` — they are
  already transcribed into Step 1's first table, so delete them here rather than leaving dead copies.
  `TestContractTouchesReviewFloor_KeywordMatch` stays where it is.

- [ ] **Step 6: Run the engine suite and the projectstate suite.**
  ```bash
  cd server && GOWORK=off go test ./internal/engine/review/ ./internal/resourceaccess/projectstate/ -count=1
  ```
  Expected: `ok` for review; `projectstate` fails only where a test still calls the deleted methods —
  delete those call sites (`access_test.go:2216`, `:7831-7944`, `:8041-8050` assert
  `ReviewPolicy.RequiresHuman` as a decoding smoke test; replace each with an assertion on the
  DECODED document, e.g. `slices.Contains(back.ReviewPolicy.GatedPhasesByType["service"], MethodPhaseDetailedDesign)`).
  Task 8 fixes the Manager and the two design rails; they do not compile until then, which is
  expected and is why Tasks 7 and 8 land in ONE commit (Step 8).

- [ ] **Step 7: Gates.** Run the full block from Task 6 Step 7. `method-check` must be green:
  `reviewEngine` still has exactly ONE operation, so `DH-CONTRACT-OPCOUNT-*` does not move, and no
  package-import edge changes in this task.

- [ ] **Step 8:** Do NOT commit yet — the tree does not build until Task 8 re-points the three
  callers. Tasks 7 and 8 are ONE commit, made at the end of Task 8.

**Earmark (not this plan):** `method-assets`'s `the-method-review-routing` SKILL.md documents the
hand-run routing with its own `artifactKind` vocabulary (`code`/`ui-design`/`test-plan`/`ui-code`,
SKILL.md:28,37), which already diverges from the Go engine's six. This task changes the generated
`reviewProposeReviews` MCP tool's inputs from `artifactKind` to `(activityType, lifecyclePhase,
policy, floorTouched)`, widening that drift. Closing it needs a method-assets release, which is its
own founder STOP, and spec §5.2 deletes the hand-run seam in stage 4 — so it is recorded here, not
fixed here.

---

### Task 8: Re-point the three callers at the one engine call (B1, second half)

The Manager's kind table and its separate gate lookup collapse into one `ProposeReviews`; the two
design rails stop reading `Preset` and ask the engine. Same commit as Task 7 — the tree does not
build in between.

**Files:**
- Modify (hand-edit, self-amendment procedure): `.aiarch/state/project.json` —
  (i) `.slots["5"].model.relationships`: two new edges, `system-design-manager → review-engine` and
  `project-design-manager → review-engine`; (ii) `.serviceContracts.constructionManager["$defs"].ReviewSet`:
  two additive optional properties.
- Regenerate: `server/internal/manager/construction/contract.gen.go`, `.../fake/fake.gen.go`,
  `server/api/openapi.yaml`, `server/internal/client/{web,mcp}/**`, `systemtests/internal/sdk/**`,
  `webApp/src/contracts/schema.ts`.
- Modify: `server/internal/manager/construction/constructactivity.go` — `reviewSetFromEngine`
  (L919-934), `runPhaseGate`'s gate decision (L1461-1478), `surfaceReviewSet` (L1480-1499),
  `runLocalMergeStep`'s gate (L2126-2131), `proposeReviewSet` (L2233-2253), and **delete**
  `reviewArtifactKindFor` (L2255-2293) and `componentReviewKind` (L2295-2311).
- Modify: `server/internal/manager/construction/deps.go` — the Manager's `ReviewEngine` consumer
  mirror (the independent interface it adapts to).
- Modify: `server/internal/manager/systemdesign/coauthorartifact.go` — the vibes snapshot (L422-428)
  and the `ArtifactKind → (activityType, lifecyclePhase)` mapping beside `critiqueCriticFor` (L3547).
- Modify: `server/internal/manager/projectdesign/coauthorphase2artifact.go` — the vibes snapshot (L416-428).
- Modify: `server/internal/manager/construction/manager_test.go` (`fakeReview`, L2981-2993 + its
  `kinds` recorder), `server/internal/manager/systemdesign/manager_test.go`,
  `server/internal/manager/projectdesign/manager_test.go`.

**Interfaces:**
- Consumes: `review.ReviewEngine.ProposeReviews` (Task 7); `projectstate.ContractTouchesReviewFloor`
  (still the source of `state.floorTouched`, `constructactivity.go:1197`);
  `constructionActivity.activityTypeName()` (`constructionmanager.go:1292-1300`).
- Produces (Manager-local, unexported):
  ```go
  func engineReviewPolicy(p projectstate.ReviewPolicy) review.ReviewPolicy
  ```
  the one adapter from the stored document to the engine's copy — `Preset` dereferenced (`nil` → `""`),
  `GatedPhasesByType` re-keyed to plain strings. It belongs in `constructactivity.go`'s adapters
  section beside `reviewSetFromEngine` (L895-934), which already documents why an adapter exists
  rather than a cast. **Each design rail needs its own copy** (three packages, no shared home that
  does not cost an `internal/arch_test.go` allowlist entry for a five-line conversion); keep them
  byte-identical and cross-referenced by comment, and pin them equal with the parity test in Step 5.
- `constructionManager.ReviewSet` gains, additively and NOT in `required`:
  ```json
  "requiresHuman": { "type": "boolean", "description": "Whether the review engine requires a human decision at this gate. Display-only on the session view: the enforced gate is the suspend itself." },
  "reason": { "type": "string", "description": "The engine's one-line explanation of the gate verdict (preset, policy row, non-overridable floor, or the project-design spend floor). Omitted when the engine refused to propose." }
  ```

**The model edge is REQUIRED, not optional.** `methodcheck`'s `CODE-EDGE-NOT-IN-MODEL` is an **Error**
(`framework-go/methodcheck/rules_conformance.go:14,26`), and slot 5 today carries
`construction-manager → review-engine` but NOT the two design managers
(verified against `.slots["5"].model.relationships`). Importing `internal/engine/review` from
`internal/manager/systemdesign` and `internal/manager/projectdesign` without declaring the edge turns
`make method-check` red. Two relationships is the smallest honest amendment, and it is an edge stage 1
inherits wholesale when the three managers become `delivery-manager`.

- [ ] **Step 1: Write the failing Manager tests** — `manager_test.go`, beside the stage-0 review tests
  (~L7560).

  ```go
  // The Manager asks the engine ONCE per gate and obeys its requiresHuman. Under vibes the
  // gate does not suspend; with the floor touched at construction it does, whatever the
  // preset says. This is the same behaviour EffectiveGate gave, now decided in one place.
  func Test_PhaseGate_ObeysTheEnginesRequiresHuman(t *testing.T) {
  	for _, c := range []struct {
  		name    string
  		policy  projectstate.ReviewPolicy
  		floor   bool
  		suspend bool
  	}{
  		{"vibes walks through", vibesPreset(), false, false},
  		{"vibes stops at a floor-touching construction dispatch", vibesPreset(), true, true},
  		{"full stops at detailed design", fullPreset(), false, true},
  	} {
  		t.Run(c.name, func(t *testing.T) { /* drive b12Run with deps.Review = review.NewReviewEngine() */ })
  	}
  }

  // The Manager passes the activity's TYPE and the lifecycle phase's wire name — never a
  // kind it computed itself. The table that used to live here is the engine's now.
  func Test_PhaseGate_PassesTypeAndPhaseToTheEngine(t *testing.T) {
  	fake := &fakeReview{set: review.ReviewSet{Reviewers: []review.Reviewer{{Role: "architect"}}, RequiresHuman: true, Reason: "full"}}
  	// … run a service activity gated at detailed_design …
  	if len(fake.calls) == 0 || fake.calls[0].activityType != review.ActivityTypeService || fake.calls[0].lifecyclePhase != "detailed_design" {
  		t.Fatalf("the engine was asked with %+v", fake.calls)
  	}
  }

  // The engine's copy of the policy document must not drift from the stored one.
  func Test_EngineReviewPolicy_CarriesTheStoredDocument(t *testing.T) {
  	preset := projectstate.ReviewPresetCheckpoints
  	in := projectstate.ReviewPolicy{Preset: &preset, GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
  		"service": {projectstate.MethodPhaseDetailedDesign, projectstate.MethodPhaseIntegration}}}
  	got := engineReviewPolicy(in)
  	if got.Preset != projectstate.ReviewPresetCheckpoints {
  		t.Errorf("preset = %q", got.Preset)
  	}
  	if !slices.Equal(got.GatedPhasesByType["service"], []string{"detailed_design", "integration"}) {
  		t.Errorf("gated phases = %v", got.GatedPhasesByType["service"])
  	}
  	if engineReviewPolicy(projectstate.ReviewPolicy{}).Preset != "" {
  		t.Error("a nil preset must read as the legacy/explicit mode, not panic")
  	}
  }
  ```
  And in `systemdesign/manager_test.go` (mirror it in `projectdesign/manager_test.go`):
  ```go
  // The design rail's autogate is the engine's answer now. Behaviour is unchanged:
  // vibes auto-approves the draft at the review gate; every other preset holds for the human.
  func Test_DesignSession_AutogateMatchesTheEngine(t *testing.T) {
  	for _, tc := range []struct {
  		preset string
  		auto   bool
  	}{{projectstate.ReviewPresetVibes, true}, {projectstate.ReviewPresetCheckpoints, false},
  		{projectstate.ReviewPresetFull, false}, {"", false}} {
  		// … start a KindGlossary session under tc.preset, assert the gate auto-approves iff tc.auto …
  	}
  }
  ```

- [ ] **Step 2: Run them; confirm they fail** (compile failure on `fakeReview`'s old signature and on
  `engineReviewPolicy`, which does not exist).
  ```bash
  cd server && GOWORK=off go test ./internal/manager/construction/ ./internal/manager/systemdesign/ ./internal/manager/projectdesign/ -count=1
  ```

- [ ] **Step 3: Amend slot 5 and the constructionManager contract; regenerate.**
  ```bash
  cd server && make gen-models
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  make method-check
  ```
  - [ ] **Verify first:** copy the shape of the existing `construction-manager -> review-engine`
    relationship row exactly (same member set, same `kind`/`technology`/`description` conventions)
    before adding the two new ones; a hand-written row with a missing member fails `validate`
    louder than it fails review.
  - [ ] **Verify first:** run `make method-check` immediately after the edit and read every finding.
    Expect `CODE-EDGE-NOT-IN-MODEL` to stop firing for the two new imports, and `MODEL-EDGE-NOT-IN-CODE`
    (Warning) to fire until Step 5 adds the imports. If a `CC-*` rule demands the new call appear in a
    dynamic view (use-case realization), add the step to `drive-system-design` /
    `commit-to-a-project-option` in slot 5 — those two views already carry the design rails' engine
    calls. **If the finding set is wider than the two relationships plus those realizations, STOP and
    report**: the model wave is stage 1, and stage 2 must not absorb it.

- [ ] **Step 4: Collapse the construction Manager onto one call.**
  - `proposeReviewSet` becomes the single decision point and returns the engine's whole answer:
    ```go
    func (wf *workflows) proposeReviewSet(in constructActivityInput, phase projectstate.ActivityMethodPhase, policy projectstate.ReviewPolicy, state *constructState) (ReviewSet, error) {
    	change := review.ReviewChange{ActivityID: string(in.ActivityID), ComponentID: in.Activity.ComponentID}
    	set, err := wf.Review.ProposeReviews(fweng.Context{Context: context.Background()},
    		change, review.ActivityType(in.Activity.activityTypeName()), phase.String(),
    		in.Activity.ComponentID, engineReviewPolicy(policy), state.floorTouched, state.reviewContracts)
    	if err != nil {
    		return ReviewSet{}, err
    	}
    	return reviewSetFromEngine(set), nil
    }
    ```
  - `runPhaseGate` (L1461-1478): ask once, then branch on the answer.
    ```go
    	// ONE call decides both halves of the review question: who reviews (the engine's
    	// reviewer rows) and whether a human must sign off (the project's committed
    	// ReviewPolicy plus the non-overridable deploy/spend/schema floor). It used to be
    	// two lookups in two components, which is how the kind table and the gate policy
    	// drifted apart. An engine refusal is LOGGED and SHOWN (reviewSetError) and opens
    	// the gate rather than failing the activity — the set is display-only in v1 and a
    	// roster defect must not cost the work. A call that only assigns and logs emits no
    	// commands, so this still needs no version gate and every replay fixture replays
    	// unchanged.
    	set, err := wf.proposeReviewSet(in, phase, policy, state)
    	if err != nil {
    		workflow.GetLogger(ctx).Error("review engine refused to propose reviewers; the gate opens without a reviewer set",
    			"activityId", in.ActivityID, "lifecyclePhase", phase.String(), "err", err.Error())
    		state.reviewSet, state.reviewSetError = nil, err.Error()
    		return false, wf.completePhase(ctx, in, phase, state, headVersion, gitOn, cred)
    	}
    	if !set.RequiresHuman {
    		state.reviewSet, state.reviewSetError = nil, ""
    		return false, wf.completePhase(ctx, in, phase, state, headVersion, gitOn, cred)
    	}
    	state.reviewSet, state.reviewSetError = &set, ""
    	return wf.awaitPhaseDecision(ctx, in, phase, state, gf, headVersion, gitOn, cred)
    ```
    **A refusal now completes the phase instead of gating it.** That is deliberate and it is the
    conservative arm: before this task an engine refusal could not reach the gate decision at all
    (`EffectiveGate` decided that separately and never failed), so failing CLOSED would gate phases
    that run ungated today — a behaviour change stage 2 is not allowed to make. The refusal is still
    loud: Error-level log plus `reviewSetError` on the session view.
    `surfaceReviewSet` (L1480-1499) is folded into the above and **deleted**; its "the roster and the
    gate occurrence are the same fact (I1)" paragraph moves into the comment above, because the
    invariant still holds — the roster is published exactly when the gate opens.
  - `runLocalMergeStep` (L2126-2131): replace `policy.EffectiveGate(...)` with the same engine call at
    `projectstate.MethodPhaseConstruction`, reading only `set.RequiresHuman`; on a refusal, log and
    treat it as "no hold" (matching the arm above). Keep the `local-merge-step` GetVersion fence exactly
    where it is.
  - `reviewSetFromEngine` (L919-934): carry `RequiresHuman` and `Reason` onto the façade type; keep the
    existing `ReferenceArtifact` `*string` bridge and its comment.
  - Delete `reviewArtifactKindFor` and `componentReviewKind`. Leave ONE sentence where they stood
    pointing at `internal/engine/review`: the table moved into the engine when `ProposeReviews` took
    the activity type (spec 2026-09-20 §5.4, stage 2).
  - `deps.go`: update the Manager's `ReviewEngine` consumer mirror to the new signature.

- [ ] **Step 5: Move the two design rails onto the engine.**
  - `systemdesign/coauthorartifact.go` L422-428: replace the `Preset == vibes` read with
    ```go
    	// The review engine owns the autogate rule now (spec 2026-09-20 §5.4): it is asked
    	// once, at session start, with this artifact's design activity type and lifecycle
    	// phase, and its answer is the same one the inline Preset check gave — vibes
    	// auto-approves, every other preset (including the unset legacy value) holds for the
    	// human. A pure engine call emits no commands, so the existing
    	// "design-vibes-autogate" GetVersion fence above still governs replay and no new
    	// fence is needed.
    	typ, lifecyclePhase := designActivityFor(toPSKind(in.ArtifactKind))
    	set, perr := review.NewReviewEngine().ProposeReviews(fweng.Context{Context: context.Background()},
    		review.ReviewChange{ActivityID: string(in.ProjectID)}, typ, lifecyclePhase, "",
    		engineReviewPolicy(proj.ReviewPolicy), false, nil)
    	state.policyAutoApprove = perr == nil && !set.RequiresHuman
    ```
    A refusal reads as "a human must decide", the safe arm for a design gate.
  - Add `designActivityFor(kind projectstate.ArtifactKind) (review.ActivityType, string)` beside
    `critiqueCriticFor` (L3547): `KindMission`→`(requirements, "mission")`,
    `KindGlossary`→`(requirements, "glossary")`, `KindScrubbedRequirements`→`(requirements, "glossary")`
    (the scrubbed-requirements pass runs with the glossary in the `the-method-requirements-analysis`
    step and shares its gate), `KindVolatilities`→`(requirements, "volatilities")`,
    `KindCoreUseCases`→`(requirements, "coreUseCases")`, `KindSystem`/`KindOperationalConcepts`/
    `KindStandardCheck`→`(architecture, "architecture")`, and every Phase-2 kind
    (`KindPlanningAssumptions`…`KindSdpReview`)→`(projectDesign, <the kind's own wire name>)`.
    **The Phase-2 kinds deliberately do NOT map to the phase id `sdp`.** `sdp` is the M0 gate of the
    projectDesign lifecycle, and Task 7 makes it always-human; the nine Phase-2 artifact drafts are
    not that gate (spec §6 deletes them entirely in stage 4), and mapping them to `sdp` would gate
    nine drafts that auto-approve under vibes today. Pin that with a test.
  - `projectdesign/coauthorphase2artifact.go` L416-428: the same change, with its own copy of
    `engineReviewPolicy` and a call to the systemdesign-side mapping re-expressed locally (the two
    packages do not import each other).
  - `manager_test.go` `fakeReview` (L2981-2993): new signature; replace the `kinds` recorder with a
    `calls []struct{ activityType review.ActivityType; lifecyclePhase string; floorTouched bool }`
    recorder, and keep the "validate through the REAL engine first" behaviour that stage 0 added —
    that guard is exactly what would catch a Manager passing a phase id the engine does not know.

- [ ] **Step 6: Run the suites, replay fixtures included.**
  ```bash
  cd server && GOWORK=off go test ./internal/manager/... ./internal/engine/review/ ./internal/resourceaccess/projectstate/ -count=1
  ```
  All **13** fixtures under `internal/manager/construction/testdata/replay/{pre-b1,post-b1,post-b17,pre-d}`
  must replay green. If any reports non-determinism, STOP: Step 4 added a command where it may only
  assign and log — the engine call is pure and in-workflow, `completePhase` is only reached on paths
  that already reached it, and `awaitPhaseDecision` is unchanged.

- [ ] **Step 7: Regenerate downstream and run every gate.**
  ```bash
  cd server && make gen-client gen-internal-tools gen-temporal gen-sdk
  cd ../webApp && npm run gen:api && npm run gen:ops
  cd ../server && git add ../.aiarch/state/project.json . ../systemtests/internal/sdk ../webApp/src/contracts
  make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-uiprofiles-check gen-lifecycles-check derived-plan-check
  make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off make test-short && make lint && make sumtype-check && make fix-check
  GOWORK=off go test ./internal/ -run 'TestFileLayout|TestGeneratedOnlyPublic|TestNoBannedPhaseIdentifier|TestManagerRequiredStringsAreInspected' -count=1
  cd ../webApp && npm run check
  ```
  No new `.go` file is created anywhere (`TestFileLayout`); no Manager op is added or changed in
  arity, so `cmd/clientgen`'s `mcpdocs` op-doc table, `cmd/server/managerlog.go` and
  `registered_names_test.go`'s golden are all untouched — confirm by `git status` showing none of
  them modified.

- [ ] **Step 8: Commit Tasks 7 + 8 together.**
  ```bash
  git commit -m "$(cat <<'EOF'
  refactor(review): one engine decides who reviews and whether a human must

  ProposeReviews now takes the activity type, its lifecycle phase, the committed
  ReviewPolicy and the floor flag, and answers with the reviewer set, the gate
  verdict and its one-line reason. The Manager's (type, phase) -> kind table and
  ReviewPolicy.EffectiveGate/RequiresHuman move into the engine; the policy
  DOCUMENT, the preset vocabulary, ReviewPolicyFromGateIDs and
  ContractTouchesReviewFloor stay in project state. The two design rails stop
  reading Preset == vibes and ask the engine, with identical behaviour under
  vibes/checkpoints/full/legacy. New non-overridable floor: the projectDesign
  gate always requires a human, because M0 approves spend.

  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  EOF
  )"
  ```

---

### Task 9: `DerivePlan` emits the design prefix and M0 hangs off it (spec §5.1/§8)

Table 11-1 #1–3 stop being "the design rail" and become the first three activities of the plan.
M0 gains the fan-IN it never had — today it is `{Id: "M0"}` with no predecessors, satisfied
derivedly (`projectstateaccess.go:8534-8550`), and the comment at `estimationengine.go:1761-1765`
says so explicitly: "Its predecessors are the design phases (Phases 1-2), which are not activities".
This task makes them activities.

**Files:**
- Modify: `server/internal/engine/estimation/estimationengine.go` — `DerivePlan` (L1317-1325),
  the always-emit block (L1414-1438), `deriveActivities` (L1585-1601), `addSinkEdges` (L1700-1725),
  `addSourceEdges` (L1727-1739), `deriveDependencies` (L1741-1758), `deriveMilestones` (L1761-1800),
  `applyDeltas`'s final sort (L2017).
- Modify: `server/internal/engine/estimation/engine_test.go` — the A1 set assertion, the
  empty-system case, the M0 source-rule case, the sink-rule case.
- Modify: `server/internal/manager/projectdesign/manager_test.go` —
  `TestDerivedPlanMatchesCommittedState` fixtures and any test pinning the 29-activity shape.
- Rewritten BY TOOL, never by hand: `.aiarch/state/project.json` slots 9 and 10
  (`make derived-plan-write`), in the SAME commit as the engine change so `derived-plan-check`
  never goes red (the precedent is the 2026-09-12 plan's Task 3).

**Interfaces:**
- Consumes: nothing new. The Engine gains no import — the three durations are constants here, not
  method-assets data, because effort is a per-project PLANNING number while `lifecycles.json` is the
  platform's task DAG. (`EstimationEngine` is pure; it may not read the embedded assets and does not
  need to.)
- Produces, in `estimationengine.go`:
  ```go
  const (
  	requirementsActivity  = "requirements"
  	architectureActivity  = "architecture"
  	projectDesignActivity = "projectDesign"
  )

  // The design prefix's planning durations, in whole 5-day quanta. Deliberately NOT
  // Table 11-1's 15/20/20: those are human-team numbers for a team that writes the
  // mission, glossary, volatilities, use cases and architecture by hand. Here every
  // design task is an agent dispatch with a human review gate, and the observed scale
  // of a full design run on this platform is hours, not weeks. One quantum each is the
  // smallest honest unit the Method's 5-day atom allows. These three constants are the
  // ONE place to tune the front end; a per-project exception is an authored override
  // delta with a justification, exactly as for every other derived effort.
  const (
  	requirementsEffortDays  = 5.0
  	architectureEffortDays  = 5.0
  	projectDesignEffortDays = 5.0
  )

  func designPrefixActivities() []DerivedActivity
  func designPrefixDependencies() []NetworkDependency
  func isDesignPrefix(activityName string) bool
  ```
  Each prefix activity: `WorkerClass: "system-architect"` (on the fixed roster), `Coding: false`,
  `ComponentID: ""`, `Derived: true`, `RiskBucket: defaultRiskFor(effort)`. Titles: "Requirements",
  "Architecture & Call Chains", "Project Design (SDP Review · M0)".
- The prefix ids are reserved by construction: `validateAdditive` (L1876-1880) already refuses an
  additive that shadows a derived activity ("that is an exclusion in disguise"), so no new guard is
  needed — add a test that proves it for these three ids rather than new code.

- [ ] **Step 1: Write the failing engine tests** — `server/internal/engine/estimation/engine_test.go`.

  ```go
  // Spec 2026-09-20 §5.1: the plan opens with a fixed three-activity design prefix and
  // M0 depends on the last of them. Before this, M0 had no fan-in at all and the design
  // rail was invisible to the network.
  func TestDerivedPlanEmitsDesignPrefixAndM0(t *testing.T) {
  	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, committedSystemView(t), ActivityListDeltas{})
  	if err != nil {
  		t.Fatalf("DerivePlan: %v", err)
  	}

  	// 1. The three activities exist, in Table 11-1 order, at the head of the plan.
  	var names []string
  	for _, a := range plan.Activities {
  		names = append(names, a.Name)
  	}
  	if len(names) < 3 || names[0] != "requirements" || names[1] != "architecture" || names[2] != "projectDesign" {
  		t.Fatalf("the plan must open with the design prefix, got %v", names[:min(3, len(names))])
  	}

  	// 2. Each one is a componentless, noncoding, architect-owned activity with a real effort.
  	byName := map[string]DerivedActivity{}
  	for _, a := range plan.Activities {
  		byName[a.Name] = a
  	}
  	for _, id := range []string{"requirements", "architecture", "projectDesign"} {
  		a := byName[id]
  		if a.ComponentID != "" || a.Coding || a.WorkerClass != "system-architect" || a.EffortDays <= 0 || !a.Derived {
  			t.Errorf("%s = %+v, want a derived, noncoding, componentless architect activity", id, a)
  		}
  	}

  	// 3. The prefix is a chain, and it does NOT hang off M0 — M0 hangs off IT.
  	deps := map[string][]string{}
  	for _, d := range plan.Dependencies {
  		deps[d.Activity] = d.DependsOn
  	}
  	if len(deps["requirements"]) != 0 {
  		t.Errorf("requirements is the plan's root; got dependsOn %v", deps["requirements"])
  	}
  	if !slices.Equal(deps["architecture"], []string{"requirements"}) {
  		t.Errorf("architecture dependsOn %v, want [requirements]", deps["architecture"])
  	}
  	if !slices.Equal(deps["projectDesign"], []string{"architecture"}) {
  		t.Errorf("projectDesign dependsOn %v, want [architecture]", deps["projectDesign"])
  	}
  	var m0 NetworkMilestone
  	for _, m := range plan.Milestones {
  		if m.Id == "M0" {
  			m0 = m
  		}
  	}
  	if !slices.Equal(m0.DependsOn, []string{"projectDesign"}) {
  		t.Fatalf("M0 dependsOn %v, want [projectDesign] — the SDP review IS the milestone's predecessor", m0.DependsOn)
  	}

  	// 4. M0 still fans OUT to every construction root, and no construction root
  	//    accidentally acquired a design activity as a predecessor.
  	for _, d := range plan.Dependencies {
  		if isDesignPrefix(d.Activity) {
  			continue
  		}
  		for _, p := range d.DependsOn {
  			if isDesignPrefix(p) {
  				t.Errorf("%s depends directly on the design activity %s; construction hangs off M0, not off the prefix", d.Activity, p)
  			}
  		}
  	}

  	// 5. N-IT is still the only sink: the prefix must not be swept into the sink rule.
  	for _, p := range deps["N-IT"] {
  		if isDesignPrefix(p) {
  			t.Errorf("system testing depends on %s; the design prefix ends at M0, not at N-IT", p)
  		}
  	}
  }

  // A project whose architecture is not committed yet still owes requirements and
  // architecture — that is the whole point of one plan. It derives the prefix and M0
  // and nothing else (M1-M3 are layer completions with no layer to complete).
  func TestDerivePlan_EmptySystemStillOwesItsDesign(t *testing.T) {
  	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, SystemView{}, ActivityListDeltas{})
  	if err != nil {
  		t.Fatalf("DerivePlan: %v", err)
  	}
  	if len(plan.Activities) != 3 || len(plan.Milestones) != 1 || plan.Milestones[0].Id != "M0" {
  		t.Fatalf("empty system: %d activities, milestones %+v", len(plan.Activities), plan.Milestones)
  	}
  }

  // The three ids are reserved: an additive may not shadow one (the existing
  // exclusion-in-disguise guard, proved for the new names).
  func TestDerivePlan_DesignPrefixIdsAreReserved(t *testing.T) {
  	for _, id := range []string{"requirements", "architecture", "projectDesign"} {
  		_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, committedSystemView(t), ActivityListDeltas{
  			Additive: []AdditiveActivity{{Name: id, Title: "x", EffortDays: 5, RiskBucket: 2,
  				WorkerClass: "system-architect", Justification: "none"}}})
  		if err == nil {
  			t.Errorf("an additive named %q must be refused", id)
  		}
  	}
  }
  ```

- [ ] **Step 2: Run them; confirm they fail.**
  ```bash
  cd server && GOWORK=off go test ./internal/engine/estimation/ -run 'TestDerivedPlanEmitsDesignPrefixAndM0|TestDerivePlan_EmptySystem|TestDerivePlan_DesignPrefixIdsAreReserved' -count=1
  ```
  Expected: compile failure on `isDesignPrefix`, then "the plan must open with the design prefix".

- [ ] **Step 3: Implement the prefix.**
  - Add the two const blocks and `designPrefixActivities()` / `designPrefixDependencies()` / `isDesignPrefix()`.
  - `deriveActivities` (L1585): prepend `designPrefixActivities()` before the component loop, and
    replace the trailing `sort.Slice` with the plan-order comparator below.
  - `deriveDependencies` (L1741): append `designPrefixDependencies()` to the reduced set AFTER the
    two general rules run, and **exempt the prefix from both rules**:
    - `addSinkEdges` (L1700): skip a design-prefix activity when collecting sinks. Its successor is
      M0, a milestone, which `hasSuccessor` cannot see — without the skip, `projectDesign` (and, on an
      empty system, all three) would be fed to `N-IT` and the plan would say system testing waits on
      the SDP review.
    - `addSourceEdges` (L1727): skip a design-prefix activity. `requirements` is the plan's true root;
      hanging it off M0 while M0 depends on `projectDesign` is a cycle, and
      `ResolveDependencySatisfied`'s cycle guard would report `DependencyCycle` at the pump.
    Both skips get a one-line comment saying exactly that.
  - `deriveMilestones` (L1786): `{Id: "M0", DependsOn: []string{projectDesignActivity}}`, and rewrite
    the M0 paragraph (L1761-1765) — its predecessor is now an activity, not "the design phases, which
    are not activities". Keep the "fan-OUT to every root is the source rule" sentence: it is still true
    and now literally reads `addSourceEdges` skipping the prefix.
  - `DerivePlan` (L1318-1320): the empty-system arm returns
    `DerivedPlan{Activities: designPrefixActivities(), Dependencies: designPrefixDependencies(), Milestones: []NetworkMilestone{{Id: "M0", DependsOn: []string{projectDesignActivity}}}}`
    with a comment: a project before its architecture is committed still owes its design; the
    construction half of the plan is what an empty System has nothing to say about.
  - Plan order — one comparator, used by `deriveActivities` AND `applyDeltas`'s final sort (L2017), so
    the two never disagree:
    ```go
    // planOrderLess orders the plan the way Table 11-1 reads it: the design prefix first,
    // in its own fixed order, then everything else by name. Slot 9's declaration order is
    // the pump's selection order and the console's row order, so "requirements" sorting
    // after "U-SPA-web-client" (lowercase sorts after uppercase in ASCII) would put the
    // front end of the project at the bottom of the screen.
    func planOrderLess(a, b string) bool {
    	ra, rb := planOrderRank(a), planOrderRank(b)
    	if ra != rb {
    		return ra < rb
    	}
    	return a < b
    }
    ```
    with `planOrderRank` giving `requirements` 0, `architecture` 1, `projectDesign` 2 and everything
    else 3.

- [ ] **Step 4: Fix the tests the change breaks.** The A1-set assertion in `engine_test.go` grows from
  29 to 32 ids and its first three entries are the prefix; the "18 roots each depend on M0" assertion
  becomes "every non-prefix root depends on M0"; the empty-system assertion is replaced by Step 1's.
  Read each failure before editing it — a test that fails for a reason not listed here is a finding,
  not a chore.
  ```bash
  cd server && GOWORK=off go test ./internal/engine/estimation/ ./internal/manager/projectdesign/ -count=1
  ```

- [ ] **Step 5: Re-materialize slots 9 and 10 BY TOOL, in this same change.**
  ```bash
  cd server && GOWORK=off make derived-plan-write
  git diff --stat ../.aiarch/state/project.json
  GOWORK=off make derived-plan-check
  ```
  - [ ] **Verify first:** read the slot-9/10 diff before staging it. Expect exactly: three new
    `activities[]` rows at the head, three new `dependencies[]` rows, M0's `dependsOn` gaining
    `projectDesign`, the activity/dependency arrays re-ordered by the new comparator, and **no
    change to any existing activity's effort, risk, workerClass, coding or componentId**. Anything
    else means the comparator or the exemptions are wrong. `derived-plan-write` also flags slots
    11-16 `staleBasis` if the plan moved — that is correct and expected (the network changed), and it
    is the same behaviour the 2026-09-12 wave relied on.
  - `.aiarch/state/project.json` is never hand-edited here; if the writer refuses, fix the engine.

- [ ] **Step 6: Gates.** Run the full block from Task 6 Step 7, with `derived-plan-check` now
  REQUIRED to be green on the re-materialized slots. `make method-check` must stay green: no
  component, relationship or contract moved in this task.

- [ ] **Step 7: Commit (engine + slots together).**
  ```bash
  git commit -m "$(cat <<'EOF'
  feat(estimation): derive the design prefix and give M0 its fan-in

  Requirements, Architecture and Project Design are Table 11-1 #1-3 and they are
  activities, not a separate rail (spec 2026-09-20 §5.1). The derivation emits
  them as a fixed chain at the head of the plan, M0 now depends on projectDesign
  instead of having no predecessor at all, and the prefix is exempt from the sink
  and source rules -- N-IT does not wait on the SDP review, and the prefix does
  not hang off the milestone it precedes. Durations are named constants, one
  quantum each: Table 11-1's 15/20/20 are human-team numbers.

  Slots 9 and 10 are re-materialized by `make derived-plan-write` in this same
  commit so derived-plan-check never goes red.

  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  EOF
  )"
  ```

---

### Task 10: Classify the prefix, refuse to dispatch it, backfill it, and serve it

The three activities now exist in the committed plan. This task makes every reader of that plan
handle them honestly: the classifier types them, the pump refuses to dispatch them, the backfill
records this project's already-done design work against them, and `QueryActivityView` + the
construction console render them instead of 404ing.

**The binding constraint (08-30 S2 ruling):** *"`ClassifyActivity` must REFUSE Design type pre-S3
(else the pump dispatches design-mission as construction)."* An unrefused design activity would be
handed to `constructActivityWorkflow`, which would resolve `CommandFor` to a design slash-command and
run it as a construction pipeline — the exact shape of the N-ENV defect that
`ClassifyActivity`'s doc block (`projectstateaccess.go:8210-8239`) was written to prevent.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `ClassifyActivity`
  (L8210-8268), `ClassifyType` (L8270-8295), a new package-level sentinel beside them.
- Modify: `server/internal/arch_test.go` — one new encapsulation-allowlist entry, with its reason.
- Modify: `server/internal/manager/construction/constructionmanager.go` — `pumpSelection` (its
  declaration, ~L1340), the scan loop in `nextEligibleActivity` (L1437-1470),
  `dispatchSelectionFor` (L1481-1521).
- Modify: `server/internal/manager/construction/pumpnextactivity.go` — after `sel := wf.nextEligible(...)` (L126).
- Modify: `server/cmd/backfill-attempts/main.go` — the evidence vocabulary (L98-150), `evaluate`'s
  switch (L228-240), plus `main_test.go`.
- Modify: `server/internal/manager/construction/manager_test.go` —
  `TestQueryActivityView_UnknownActivityIsNotFound` (stage 0, ~L3605) loses the three ids.
- Modify: `webApp/uitests/tests/construction-tracker.spec.ts` and
  `construction-search-provenance-guarantee.spec.ts` (row counts / ids).
- Rewritten BY TOOL: `.aiarch/state/project.json` `.activityConstruction`
  (`cmd/backfill-attempts`, committed separately from the tool, per the 2026-09-12 Task 5 pattern).

**Interfaces:**
- Produces:
  ```go
  // ErrDesignActivityNotDispatchable is returned by ClassifyActivity, WITH the resolved
  // design ActivityType, for the three reserved design-prefix ids. The pair is the point:
  // a design activity IS classifiable — the console, QueryActivityView and the backfill
  // all need its type and its lifecycle — but it is not DISPATCHABLE by the construction
  // pump, which would run its design command as a construction pipeline (08-30 S2 ruling).
  // Stage 4's DeliveryManager dispatches it; until then callers select on errors.Is.
  var ErrDesignActivityNotDispatchable = errors.New(
  	"projectstate: design activities are not dispatched by the construction pump")
  ```
  and `pumpSelection.SkippedDesign []string`.
- Consumes: `projectstate.ClassifyType`, `projectstate.ResolveConstructionRow`,
  `projectstate.LifecycleKeyFor`, `methodassets.LifecycleFor`, `projectstate.TasksForProfile` /
  `ProfileFor` / `PhaseForTask` / `AttemptID` / `IsConditionalTask` (all already used by
  `QueryActivityView` at `constructionmanager.go:778-830` and by `attemptsFor`,
  `backfill-attempts/main.go:853-884`) — none of them needs a change, because Part A made them
  method-assets-backed and Task 6 made the three type keys resolve.

**The classification rule: exact id, checked FIRST.** The three ids are reserved platform ids that
only the derivation emits (`validateAdditive` refuses an additive that shadows one, Task 9), so an
exact-id match is stable, total and needs no new `ActivityItem` field. It must run before every other
rule: `system-architect` + `coding=false` is rule 6 today and would type all three as `Documentation`.

- [ ] **Step 1: Write the failing tests.**

  (a) `access_test.go`:
  ```go
  // The three reserved design ids classify to their own types and carry the
  // not-dispatchable sentinel with them. The workerClass/coding pair they are authored
  // with (system-architect, coding=false) would otherwise type them as Documentation.
  func TestClassifyActivity_DesignPrefixIsTypedButNotDispatchable(t *testing.T) {
  	cases := []struct {
  		id   string
  		want ActivityType
  	}{
  		{"requirements", ActivityTypeRequirements},
  		{"architecture", ActivityTypeArchitecture},
  		{"projectDesign", ActivityTypeProjectDesign},
  	}
  	for _, c := range cases {
  		typ, variant, err := ClassifyActivity(c.id, "system-architect", false)
  		if typ != c.want || variant != TestVariantPlan {
  			t.Errorf("%s -> (%s, %s), want (%s, plan)", c.id, typ, variant, c.want)
  		}
  		if !errors.Is(err, ErrDesignActivityNotDispatchable) {
  			t.Errorf("%s: err = %v, want ErrDesignActivityNotDispatchable", c.id, err)
  		}
  		// The VIEW lens must still type it: a design row renders with its lifecycle.
  		if got, ok := ClassifyType(c.id, "system-architect", false, false); !ok || got != c.want {
  			t.Errorf("ClassifyType(%s) = (%s, %v), want (%s, true)", c.id, got, ok, c.want)
  		}
  	}
  	// An ordinary activity is untouched and an unclassifiable one still fails the old way.
  	if _, _, err := ClassifyActivity("C-billing-engine", "junior-developer", true); err != nil {
  		t.Errorf("a coding activity must still classify cleanly: %v", err)
  	}
  	if _, ok := ClassifyType("N-WAT", "", false, false); ok {
  		t.Error("an unclassifiable activity must still be refused, not swept in with design")
  	}
  }

  // B3: the command a design lifecycle's work task runs is today's design command, and
  // the projectDesign gate has none because it has no dispatch task at all.
  func TestCommandFor_DesignLifecycles(t *testing.T) {
  	cases := []struct {
  		typ   ActivityType
  		phase ActivityMethodPhase
  		want  string
  	}{
  		{ActivityTypeRequirements, "mission", "mission-draft"},
  		{ActivityTypeRequirements, "glossary", "glossary-draft"},
  		{ActivityTypeRequirements, "volatilities", "volatilities-draft"},
  		{ActivityTypeRequirements, "coreUseCases", "core-use-cases-draft"},
  		{ActivityTypeArchitecture, "architecture", "system-draft"},
  		{ActivityTypeProjectDesign, "sdp", ""},
  	}
  	for _, c := range cases {
  		if got := CommandFor(c.typ, TestVariantPlan, c.phase); got != c.want {
  			t.Errorf("CommandFor(%s, %s) = %q, want %q", c.typ, c.phase, got, c.want)
  		}
  	}
  }
  ```
  - [ ] **Verify first:** `TestCommandFor_DesignLifecycles` is a CHECK on Part A's adapter, not new
    work — Part A re-pointed `CommandFor` at `lifecycles.json`, whose design phases carry
    `command: "mission-draft"` etc. on their dispatch task and nothing on `sdpReview`
    (`lifecycles.gen.ts:106-190,205-232,233-253`). If it fails because Part A's adapter returns the
    derived `<slug>-<phase>` string instead of the task's `command`, that is Part A's adapter to fix,
    not this task's — report it and coordinate rather than adding a second rule here.

  (b) `manager_test.go` — the pump:
  ```go
  // The pump walks past a design activity and says so. It must NOT block it: blocking
  // records a sticky RecordActivityFailed with no reopen path, which would poison the
  // activity stage 4 is built to run.
  func Test_NextEligible_SkipsDesignActivitiesWithoutBlockingThem(t *testing.T) {
  	proj := projectWithDesignPrefix(t) // requirements/architecture/projectDesign not started
  	sel := nextEligibleActivity(proj, eligibleNotStarted)
  	if sel.Verdict == verdictBlocked {
  		t.Fatalf("a design activity must never be blocked: %+v", sel)
  	}
  	if !slices.Equal(sel.SkippedDesign, []string{"requirements", "architecture", "projectDesign"}) {
  		t.Errorf("skipped = %v, want every design activity named", sel.SkippedDesign)
  	}
  	if sel.Verdict != verdictQuiescent {
  		t.Errorf("with only design work eligible the pump is quiescent, got %+v", sel)
  	}
  }

  // Defense in depth: if a design activity somehow reaches the dispatch resolver, it goes
  // quiet rather than blocking or dispatching.
  func Test_DispatchSelectionFor_DesignActivityGoesQuiet(t *testing.T) {
  	sel := dispatchSelectionFor(projectWithDesignPrefix(t), "architecture",
  		projectstate.ActivityItem{Name: "architecture", WorkerClass: "system-architect"})
  	if sel.Verdict != verdictQuiescent {
  		t.Fatalf("got %+v, want quiescent", sel)
  	}
  }
  ```

  (c) `manager_test.go` — the read, replacing the three ids stage 0 pinned as NotFound:
  ```go
  // Stage 0 returned NotFound for these three because they were not activities yet. They
  // are now, and each one serves its method-assets lifecycle.
  func TestQueryActivityView_ServesTheDesignPrefix(t *testing.T) {
  	want := map[string]struct{ phases, tasks int }{
  		"requirements":  {4, 8},
  		"architecture":  {1, 2},
  		"projectDesign": {1, 1},
  	}
  	for id, w := range want {
  		v, err := avManager(nil, planWithDesignPrefix(), &fakeEpisodes{}).QueryActivityView(testCtx(), "p", ActivityID(id))
  		if err != nil {
  			t.Fatalf("%s: %v", id, err)
  		}
  		if v.Type != id || len(v.Phases) != w.phases || len(v.Tasks) != w.tasks {
  			t.Errorf("%s: type=%s phases=%d tasks=%d, want %s/%d/%d", id, v.Type, len(v.Phases), len(v.Tasks), id, w.phases, w.tasks)
  		}
  	}
  }
  ```

- [ ] **Step 2: Run them; confirm they fail.**
  ```bash
  cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ ./internal/manager/construction/ -count=1
  ```

- [ ] **Step 3: Implement the classifier.** In `projectstateaccess.go`, add the sentinel and a
  `designActivityType(id string) (ActivityType, bool)` exact-match helper, and make it
  `ClassifyActivity`'s FIRST rule — inserted above the `switch workerClass` at L8241, with the
  precedence list in the doc block (L8226-8238) renumbered to start at "0. one of the three reserved
  design ids". `ClassifyType` (L8285-8294) treats the sentinel as classified:
  ```go
  	typ, _, err := ClassifyActivity(id, workerClass, coding)
  	if err != nil && !errors.Is(err, ErrDesignActivityNotDispatchable) {
  		return ActivityTypeService, false
  	}
  	return typ, true
  ```
  with a comment: the view lens asks "can this row be rendered honestly", and a design activity can —
  it has a type, a lifecycle and committed artifacts; only the PUMP cares that it is not dispatchable.
  Add the allowlist entry to `internal/arch_test.go` beside the other review/policy entries, with the
  two-sentence reason above (it is read from `internal/manager/construction`, a different package).

- [ ] **Step 4: Teach the pump to walk past it.**
  - `pumpSelection` gains `SkippedDesign []string` with a doc line.
  - `nextEligibleActivity`'s scan loop: after the `eligibleUnder` filter, skip a design activity,
    collecting its name. Keep the added code to one `if` over a tiny helper so the loop body stays
    readable.
    - [ ] **Verify first:** `nextEligibleActivity` is close to the `gocyclo` ceiling (15, no
      exclusions, `.golangci.yml:40-41`). Run `make lint` right after this edit. If it trips, fold the
      design filter into `eligibleUnder` (which is small) and collect `SkippedDesign` in a separate
      three-line pass over `activityList.Activities` — do not raise the ceiling.
  - `dispatchSelectionFor`: before the `ClassifyActivity` error arm (L1510-1520), branch on
    `errors.Is(cerr, projectstate.ErrDesignActivityNotDispatchable)` and return
    `pumpSelection{Verdict: verdictQuiescent, SkippedDesign: []string{chosen}}`. **Never `verdictBlocked`**:
    `verdictBlocked` writes `RecordActivityFailed`, which is sticky and has no reopen path — it would
    terminally fail the very activity stage 4 exists to run. Say that in the comment.
  - `pumpnextactivity.go` after L126: log at Info when `sel.SkippedDesign` is non-empty, naming the
    activities and the reason ("the construction pump does not dispatch design activities; the
    DeliveryManager does, from stage 4").
  - **Replay:** no `GetVersion` fence is needed and none may be added. No committed plan has ever
    held a design activity, so no pump history can contain a selection this skip would change; the
    new arm is unreachable on every existing execution. Adding a fence would be noise that outlives
    its reason.

- [ ] **Step 5: Extend the backfill with a committed-design-artifact evidence path.**
  The three activities are componentless, so `evaluate` (L228-240) sends them to `signedOff`, which
  reports "no evidence path". A founder quote would be a fabrication here — the real evidence is that
  the slots are committed and reviewed. Add a THIRD evidence path beside `fullyImplemented` and
  `signedOff`:
  ```go
  // designSlotEvidence is the evidence path for a design-prefix activity: every artifact
  // slot the activity produces is committed in project.json with a committed review
  // status. It is the artifact analogue of fullyImplemented's "read the code" — the
  // artifact IS the work product, and a committed slot is the same kind of fact a
  // contract-plus-implementation file is. An activity whose slots are not all committed
  // does not qualify: that is real design work still owed.
  func designSlotEvidence(activityID string, p projectstate.Project) verdict
  ```
  Slot sets: `requirements` → `.mission`, `.glossary`, `.scrubbedRequirements`, `.volatilities`,
  `.coreUseCases`; `architecture` → `.systemDesign`, `.operationalConcepts`; `projectDesign` →
  `.planningAssumptions`, `.activityList`, `.network`, the four solution slots, `.riskModel`,
  `.sdpReview`. Each must be `ReviewCommitted`. The Basis names every slot ref and its committed
  status, plus a count ("systemDesign: 26 components, 4 views"), exactly as `signOffBasis` names its
  artifact; `ArtifactRef` is set so `evidenceFor` (L804-806) stamps every task with
  `EvidenceArtifact` — its `v.ArtifactRef != ""` short-circuit already does the right thing for task
  ids outside the Figure A-1 twelve, so that switch needs NO edit.
  Route it in `evaluate`'s switch as a new first case, keyed on `designActivityType`, before
  `hasComponent`. `attemptsFor` (L853) then emits one passed, `OriginBackfilled` attempt per design
  task automatically, because `ProfileFor` resolves the design lifecycle (Part A) and none of its
  tasks is conditional.
  - [ ] **Verify first:** run `gateIntegration` mentally against the prefix — `requirements` has no
    dependency, `architecture` depends on `requirements`, `projectDesign` on `architecture`, so none
    is integration-pending once all three qualify. If the dry run reports one as pending, the chain
    is wrong in slot 10, not here.

- [ ] **Step 6: Dry run, then run the backfill.**
  ```bash
  cd server && GOWORK=off go test ./cmd/backfill-attempts/ -count=1
  GOWORK=off go run ./cmd/backfill-attempts -repo .. -dry-run
  ```
  Report which of the three qualified and why. Expect all three: this project's design slots are
  committed. Then the real run, and commit the tool and the state output SEPARATELY (the state commit
  carries the basis text, per the 2026-09-12 pattern).
  ```bash
  GOWORK=off go run ./cmd/backfill-attempts -repo ..
  git diff --stat ../.aiarch/state/project.json
  ```
  - [ ] **Verify first:** the diff must touch `.activityConstruction` and `.constructionProgress`
    ONLY — the byte-fidelity gate (`onlyConstructionEdited`, `confirmOnlyConstructionMoved`) enforces
    it, so a refusal means the codec moved, not that the guard is wrong.
  - Because the three rows are now Done, the pump's design skip is unreachable for THIS project and
    M0 resolves satisfied exactly as it did before slot 10 gave it a predecessor — which is the whole
    reason the backfill is in this task and not deferred: without it, `AllDepsSatisfied` would find
    M0 unsatisfied and every construction activity would stop being eligible.

- [ ] **Step 7: The read path and the console.**
  - Delete `"requirements", "architecture", "projectDesign"` from
    `TestQueryActivityView_UnknownActivityIsNotFound`'s id list (stage-0 Task 8, ~L3607) — keep
    `"C-nope"`. The stage-0 plan's Global Constraint "requirements / architecture / projectDesign do
    not exist as plan activities until stage 2 — `QueryActivityView` returns the not-found error for
    them" is **superseded here**; no code in the façade changes, because `committedActivityItem` +
    `ResolveConstructionRow` + `LifecycleKeyFor` + `methodassets.LifecycleFor`
    (`constructionmanager.go:791-807`) already compose to serve them once the plan holds them and the
    classifier types them.
  - webApp: run `npm run check`, then drive the real console on this project's state
    (`archistrator-run-app-locally`: `GOWORK=off` server + SPA, `/project/archistrator/construction?lens=list`)
    and confirm by eye that the three rows render at the TOP of the list, each with its type chip
    (Task 6's `KIND_META`), each `done`, each expanding to its lifecycle sub-rows — 4 phase pairs for
    Requirements, 1 pair for Architecture, 1 gate row for Project Design — and that no row renders as
    Unclassified. Screenshot each and read it back.
  - Update `uitests/tests/construction-tracker.spec.ts` and
    `construction-search-provenance-guarantee.spec.ts` for the new row count and ids; the provenance
    guarantee must still hold for the three backfilled rows.

- [ ] **Step 8: Gates and commits.**
  Run the full gate block from Task 6 Step 7 plus:
  ```bash
  cd server && GOWORK=off go test ./internal/ -run 'TestFileLayout|TestGeneratedOnlyPublic|TestNoBannedPhaseIdentifier|TestMethodLayering' -count=1
  GOWORK=off go test ./cmd/... -count=1
  cd ../webApp && npm run check
  ```
  Two commits:
  ```bash
  git commit -m "$(cat <<'EOF'
  feat(construction): type the design prefix, and refuse to dispatch it

  ClassifyActivity types the three reserved design ids and returns
  ErrDesignActivityNotDispatchable with them: a design activity IS classifiable --
  the console, QueryActivityView and the backfill all need its lifecycle -- but the
  construction pump must not run its design command as a construction pipeline
  (08-30 S2 ruling). The pump skips it and logs why; it never BLOCKS it, because a
  block is sticky and has no reopen path. The backfill gains a committed-design-
  artifact evidence path, and QueryActivityView now serves all three lifecycles.

  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  EOF
  )"
  # then, separately:
  git commit -m "$(cat <<'EOF'
  chore(state): backfill the design prefix from the committed design slots

  Requirements, Architecture and Project Design are recorded Done with
  origin=backfilled, one passed attempt per lifecycle task, basis citing each
  committed slot. Without it M0's new dependency on projectDesign would read as
  unsatisfied and no construction activity would be eligible.

  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  EOF
  )"
  ```

---

## Part B self-review

| Brief | Where it lands | Notes |
|---|---|---|
| **B1** engine owns WHO (kind table moves in, typed on the engine's own generated enums) | Task 7 Steps 3-4 (`$defs.ActivityType` string-enum copy, `artifactKindFor` moved), Task 8 Step 4 (`reviewArtifactKindFor`/`componentReviewKind` deleted) | The engine copy is keyed on WIRE NAMES, not projectstate's ordinals — F3 forbids the import and the strings are already the shared vocabulary |
| **B1** engine owns WHETHER (EffectiveGate/RequiresHuman/presets/floor move in; policy DATA stays) | Task 7 Steps 1, 4, 5 | Exactly what stays vs moves is decided in prose: `ReviewPolicy`, the preset consts, `ReviewPolicyFromGateIDs`+`gateIDToPhase`, `reviewFloorKeywords`+`ContractTouchesReviewFloor` stay |
| **B1** new non-overridable `projectDesign` human floor | Task 7 Step 1 (`Test_ProposeReviews_ProjectDesignGateAlwaysRequiresAHuman`), Step 4 | Scoped to lifecyclePhase `sdp`, so the nine Phase-2 drafts keep today's vibes behaviour (Task 8 Step 5) |
| **B1** design reviewer rows named faithfully from the PM-critic dispatch | Task 7's reviewer-row table, read off `critiqueCriticFor` + the wire labels at `coauthorartifact.go:3591` | `volatilities` gets NO agent reviewer, because it has no critique round today |
| **B1** Manager's table + `runPhaseGate`'s gate call collapse to one engine call | Task 8 Step 4 | Plus `runLocalMergeStep`'s second `EffectiveGate` call site, which the brief did not name but which is the same policy read |
| **B1** design rails' `Preset == vibes` replaced by asking the engine, behaviour identical | Task 8 Step 5, pinned by Task 7 `Test_ProposeReviews_DesignGate_KeepsTheVibesAutogateRule` + the per-rail `Test_DesignSession_AutogateMatchesTheEngine` | The legacy `""` arm differs between design (human) and construction (explicit map); both are pinned |
| **B1** contract hand-edit + full regen + `method-check` + `validate --slot System` | Tasks 7 Step 3, 8 Steps 3 and 7 | Task 8 Step 3 also adds the TWO slot-5 relationships without which `CODE-EDGE-NOT-IN-MODEL` (Error) fails the build |
| **B1** Manager fake keeps validating through the real engine | Task 8 Step 5 | `kinds` recorder becomes a `calls` recorder |
| **B1** Temporal replay: pure/in-workflow, no command change, 13 fixtures green | Task 8 Steps 4 and 6 | The refusal arm deliberately fails OPEN, because failing closed would gate phases that run ungated today |
| **B2** append `ActivityType` 7/8/9, never renumber, every exhaustive switch | Task 6 (whole task) | Three contracts carry the `$def` and move together |
| **B2** `DerivePlan` emits the fixed prefix with named duration constants; M0 `dependsOn projectDesign`; M0 still fans out | Task 9 Steps 1-3 | Verified today's M0 fan-out is `addSourceEdges`; the prefix is exempted from BOTH the source and sink rules, or the network cycles / N-IT waits on the SDP review |
| **B2** `ClassifyActivity` classifies by a stable rule + typed sentinel; pump refuses with a logged reason | Task 10 Steps 1, 3, 4 | Exact-id rule, checked first; refusal is `verdictQuiescent`, never `verdictBlocked` |
| **B2** re-materialize slots 9/10 with `make derived-plan-write`, gated by `derived-plan-check` | Task 9 Step 5 | Same commit as the engine change |
| **B2** backfill the prefix as Done/backfilled so progress and the console do not regress | Task 10 Steps 5-6 | New `designSlotEvidence` path rather than a fabricated founder quote; it is also what keeps M0 satisfied |
| **B2** `TestDerivedPlanEmitsDesignPrefixAndM0` acceptance | Task 9 Step 1, by that exact name | |
| **B2** console renders the three honestly; `QueryActivityView` serves them (stage 0's out-of-scope rule dies) | Task 10 Step 7 | 4 pairs / 1 pair / 1 gate asserted in `TestQueryActivityView_ServesTheDesignPrefix` |
| **B3** `CommandFor` returns the lifecycle task's command, `""` for the projectDesign gate | Task 10 Step 1 `TestCommandFor_DesignLifecycles` | Written as a CHECK on Part A's adapter, with an explicit hand-back if Part A returns the derived slug instead |

**Assumptions consumed from Part A, stated once:** `projectstate.LifecycleKeyFor(t, v) string` is the
one production home of the lifecycle key (the stage-0 copy in `constructionmanager.go` is gone), and
`ProfileFor` / `CommandFor` / `TasksForProfile` / `PhaseForTask` / `GateTaskFor` / `cmd/gen-uiprofiles`
all read the pinned method-assets `lifecycles.json`. Part B adds no reader of its own and re-points
none of them; it only widens the type vocabulary those readers key on.

**Two things Part B deliberately does NOT do**, both flagged inline: it does not reorder the console's
LIST lens into full Table 11-1 build order (spec §7.3 — stage 5; Task 9 only makes slot 9's own
declaration order honest), and it does not reconcile method-assets' `the-method-review-routing`
SKILL.md with the engine's new inputs (a platform release is its own founder STOP, and stage 4 deletes
the hand-run seam — earmarked at the end of Task 7).
