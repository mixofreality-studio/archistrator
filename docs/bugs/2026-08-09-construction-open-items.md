# Open items after the construction-dispatch fix

**Recorded:** 2026-08-09 · **Merge:** `5698033` (branch `construction-dispatch-componentid`, 13 commits)

This is a record of findings, not a plan. It exists because the working ledger lived in
gitignored scratch. Everything below was found while fixing and verifying
`docs/bugs/construction-dispatch-no-contract-key.md`; the design that shipped is
`docs/superpowers/specs/2026-08-08-construction-dispatch-componentid-design.md`.

## What shipped

Component identity is an authored `componentId` on the Phase-2 activity list, resolved by exact
id against the committed systemDesign. The fuzzy resolvers are deleted. An undispatchable
activity records a durable, operator-visible terminal failure instead of spinning silently.
Four further defects found while verifying it are fixed (Temporal isolation, cross-project write
guard, harness completion measurement, milestone dependency resolution).

**Verified:** the 26-activity todomvc network drains to Done under a dry-run drain, confirmed in
the construction console. **Not verified:** a real construction pass. No activity has ever
reached Done with a real agent.

## Blocking a real construction pass

**1. Dispatch command misrouting.** Dispatch computes the command from prefix-only `DeriveType`,
so 13 of 26 benchmark activities and **25 of 68 of archistrator's own** get the wrong command
family — `N-ENV` (infra) received `/testing-plan-requirements`. The agent correctly refused to
write the wrong artifact; the executor read that as "no commits", retried, and the activity died
of `VarianceExhausted`. A full architect ruling exists (in session transcript): a new plan-time
`ClassifyActivity(id, workerClass, coding) (ActivityType, TestingVariant, error)` keyed on
authored signals, no default arm; new `ActivityTypeUIDesign`/`ActivityTypeIntegration`; new
`FailureReason` `ActivityUnclassifiable`; classify once and stamp Type+Variant onto the payload
so command and phase profile cannot disagree. The enum values are authored but unwired on branch
commit `b51f6db` (not merged — it reds the drift gate until the Go is regenerated).

Founder ratified during that ruling: the Integration phase **will** be gated under
`ReviewPresetCheckpoints`.

**2. `RecordActivityStarted` never sets `cs.Type`.** So `phaseSetFor(cs.Type=0)` seeds the 5-phase
*service* set for every live activity, including correctly-dispatched ones — earned value and
phase records silently diverge from the profile the workflow walks. Independent of (1) and
arguably worse, since it corrupts data that looks right.

**3. `activityTypeName()` still uses `DeriveType`** for the review policy's `GatedPhasesByType`
keys. Fixing dispatch without it gives deployment phases gated by testing rows.

## Stage 2 of the componentId work (specced in §7 of the design doc)

- The `ACT-*` methodcheck rules. `ACT-COMPONENT-COVERAGE` still satisfies itself through the
  fuzzy `deriveActivityComponent` and never reads `item.ComponentID` — **the gate and the pump
  have diverged, and only the pump has teeth.**
- **Archistrator's own activity-list backfill.** 24 of 43 coding activities previously resolved a
  component through the deleted fuzzy matcher; they now dispatch with an empty `component_id`.
  Land this before the dogfood rail runs construction again.
- The `the-method-activity-list` skill change + method-assets release. Benchmark repos never read
  the local `.claude`; without the release, future runs draft field-less activity lists.
- The `componentId` column in the rendered activity table — the design's sole accepted mitigation
  for the referentially-valid-swap residual (§5.1). Ships nowhere today.

## Rule-version drift

Whole-document validation on every write means **any rule-version bump retroactively invalidates
every project sealed before it**, and the failure surfaces at an unrelated later write as retry
exhaustion rather than as a staleness diagnosis.

Observed: the todomvc corpus was authored 2026-08-07 18:24; the `DEP-KEY-UNIQUE` family and its
paired "give every element a key" prompt fix landed together at 21:26 (`framework-go@a57d15e8`,
`method-assets v0.3.0`). The corpus was three hours too old. Repairing it took 196 lines — keys
alone just unmasked `DEP-EDGE-ISOLATED` and `DEP-FRONTEND-PRESENT` underneath.

Suggested seam: rule-version staleness detection at the Phase-2 → Phase-3 transition, reusing the
existing `crossartifact`/`staleness` machinery, which already handles artifact-version drift.

Not reproducible on current binaries (the prompt now mandates keys), and archistrator's own slot 6
is unaffected (12 nodes, 0 empty keys).

## Smaller earmarks

- **No un-fail seam.** `RecordActivityFailed` is sticky and nothing in the store reopens an
  activity. The failure text no longer promises otherwise, but a repaired plan still cannot
  resume without a hand-commit.
- **`~/.archistrator/temporal.db` is machine-wide.** Two local archistrator instances share
  workflow state by default, even on different ports. This is the *cause* of the contamination
  incident; the identity guard hardened only the consequence.
- **Bench harness leaks on signal.** Teardown covers throws and normal exits, not SIGINT/SIGTERM —
  Ctrl-C orphans a Temporal dev-server holding a port and a db file.
- **The SPA computes milestone state from activity records milestones never have**, so a reached
  milestone renders as not-reached and its dependents read "blocked" instead of "eligible". The
  view-layer twin of the pump bug fixed in this merge.
- **No codegen-time consistency gate** for same-named enum defs sharing a `goPackage`. The drift
  materialized on the first extension and only a manual audit caught it. `systemdesignmanager.go`
  also casts one generated `FailureReason` to another by raw ordinal with nothing checking they
  still agree.
- **`I-UC*` base-list shape.** Neither of the book's normal-solution worked examples contains a
  standalone integration activity; ours has five, each depending on 9-11 components and converging
  on `N-IT`. The architect owns this as its own defect and suspects they are mislabelled
  system-test slices. Per the design's §10 sequencing: fix, then reshape, then baseline.

## Pre-existing on main, not from this merge

`make gen-temporal-check` fails: `appgen: envnames: infra "postgres": not declared in deployment`.
All `slots` are byte-identical pre- and post-merge and this branch passed the check at `4d64502`,
so the cause is main's own content — likely `1017b0b` or the dogfood state churn.
