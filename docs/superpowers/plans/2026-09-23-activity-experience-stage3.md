# Activity Experience — Stage 3 (One Staging/Review Rail) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make construction's review history REAL. Today a construction send-back leaves a one-line `OperatorNote` and nothing else — no roster, no verdicts, no thread, no round number, no subject — and the attempt ledger has exactly one production writer (`cmd/backfill-attempts`). Stage 3 gives every activity ONE staging → review → commit verb family over ONE owner: **`activityExecutionAccess`** (a new 12-verb ResourceAccess contract — Task 3 carries a founder ruling on whether it is a fifth FACET of the existing `project-state-access` component or a component of its own) holding two append-only ledgers per activity (`Attempts []TaskAttempt`, `Reviews []ReviewRound`) under `.activityExecution[activityId]`, written by the construction child workflow and by the design rails alike, with per-activity optimistic `Version` and a project-scoped serialized commit lane. `QueryActivityView` (stage 0) stops reconstructing revisions from send-back notes and reads the persisted rounds; the reconstruction path survives only for rows that predate the ledger. `.activityConstruction` → `.activityExecution` is this wave's ONE wire break, and the derived-only fields (`Phases`, `CurrentPhase`, `BuildStatus`, coarse `Phase`, `Kind`) stop being stored.

**Architecture:** Two blocking defects come FIRST, because both corrupt revision history the instant a rejection is persisted and both are invisible until then (spec §8 stage-3 entry criteria). Then the model and the code of `activityExecutionAccess` land in ONE commit — the stage-1 planning proved a `planned` RA has no green posture (`SYS-RA-ORPHAN` without relationships, `DV-REL-COVERAGE` with them), so the contract, the slot-5 prose and relationship labels, the dynamic-view steps and the Go implementation are indivisible. Planning ALSO found that the three "RAs" the spec folds are not components at all but **contract facets of the one `project-state-access` component** (ratified facet doctrine, stated in the component's own `encapsulates`), which changes what the commit deletes — see Task 3's recon correction. Then the construction child workflow starts WRITING (every write is a Temporal Activity, so the command sequence changes and every new call site needs a `workflow.GetVersion` fence and new replay fixtures). Then the design rails dual-write rounds beside `ArtifactSlot.ReviewThread`. Then `QueryActivityView` reads rounds. Then the one-shot migration runs on this repo's own state. Per-type behaviour stays in data and strategy; nothing branches on "design vs construction".

**Tech Stack:** Go 1.26 (`GOWORK=off` always), Temporal Go SDK (child workflows, `GetVersion`, replay fixtures), `.aiarch/state/project.json` as the model database (git-as-DB) with modelgen/clientgen/appgen/temporalgen codegen, `framework-go` arch + methodcheck gates, method-assets v0.9.0 lifecycles, React 19 + TypeScript for the SPA read model.

**Spec:** `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — §2 (vocabulary: Activity / LifecyclePhase / Task / Attempt / Revision / Episode), **§5.3 (the execution data model, the twelve verbs, dispositions, concurrency, migration)**, §5.4 (review engine), §8 (stage-3 row, the two entry criteria, the self-amendment procedure), §9 (testing), §10 (risks). Executors read the spec and this plan. Predecessor plans, whose verified facts this plan builds on: `docs/superpowers/plans/2026-09-21-activity-experience-stage0.md` (F1–F7; Task 7 `normalizeAttempts`/`deriveTaskViews`; Task 8 `QueryActivityView`), `docs/superpowers/plans/2026-09-22-activity-experience-stage2.md` (Task 7/8 contract + slot-5 edit recipe), `docs/superpowers/plans/2026-09-23-activity-experience-stage1.md` + `docs/bugs/2026-09-23-stage1-model-wave-earmarks.md` (why `activityExecutionAccess` must be modelled WITH its code, here).

## Global Constraints

- Work in the git worktree `.claude/worktrees/activity-stage3` (branch `activity-experience-stage3`, from `main` @`69e7a8fe`). The main checkout is shared with other sessions. Create it with `git worktree add .claude/worktrees/activity-stage3 -b activity-experience-stage3 main`, then `cd webApp && npm install` inside it (a fresh worktree has no `node_modules`) and export `GOLANGCI_LINT_CACHE=$(mktemp -d)` for the first `make lint` (the shared cache keys on absolute paths).
- `GOWORK=off` on every `go`/`make` command under `server/`. Gates run against PINNED platform tags, never a `replace`.
- **Slot 5 (System) edits ARE sanctioned in this wave**, and ONLY as Task 3 names them: the `project-state-access` component's `encapsulates` prose and `atomicBusinessVerbs`, the three `*-manager → project-state-access` relationship labels, the `calls[].label` of every dynamic-view step naming a retired verb, and `.serviceContracts`. (Under Task 3's ruling (B) only: one new component + two new relationships + the view steps that exercise them.) Any slot-5 finding WIDER than that list — anything touching a Manager component, the Manager cardinality, or the core use cases — is a **STOP**: that is stage 4's indivisible commit (spec §8, `docs/bugs/2026-09-23-stage1-model-wave-earmarks.md`).
- **Slots 9 and 10 are TOOL-ONLY** — never hand-edited; they move by `make derived-plan-write` alone. `.activityExecution` is likewise tool-written: Task 9's `cmd/migrate-activity-execution` is the only thing that may rewrite it, exactly as `cmd/backfill-attempts` is the only thing that ever wrote `Attempts`.
- **Never two implementers on `project.json` concurrently**, even in disjoint regions (earned 2026-09-23: two stage-1 tasks collided and one had to surgically revert its own hunks). Tasks 3 and 9 both touch it; they are strictly sequential.
- Self-amendment loop after any `project.json` hand-edit (spec §8): `make gen-models` (+ `gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-main`) → `make method-check` → `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System` **AND `--slot <every slot edited>`** (System DOWNGRADES other slots' Errors — VOL-GLOSS was caught only by `--slot Volatilities`) → `GOWORK=off make test-short` → `cd webApp && npm run gen:api && npm run gen:ops && npm run check`. `make method-check` does **not** carry the DH-* family; `validate` does.
- **Every new project-state write is a Temporal Activity**, so the construction child workflow's command sequence changes. Each new call site is fenced with `workflow.GetVersion` using the repo's own idiom (Task 5 quotes the two existing fences verbatim), the 13 replay fixtures under `server/internal/manager/construction/testdata/replay` stay green, and NEW fixtures are recorded for the new paths (Task 5 Step 8). `registered_names_test.go`'s golden WILL change — updating it is a deliberate, reviewed step, never a `-update` run slipped into an unrelated commit.
- **`required` in the contract schema dialect is PRESENCE-only.** Non-emptiness lives in the Go implementation, never in `minLength` (2026-08-13 contract-strictness ruling; `ValidateModelIdentities` + the paramguard arch gate hold the line). Every new op validates its own non-empty ids and returns `ContractMisuse`.
- Never weaken, skip or allowlist around a gate; no `//nolint` (decompose instead). No `default:` arm added to a switch over a sum type to dodge `make sumtype-check` — every new outcome/verdict vocabulary is total.
- `TestFileLayout` is absolute and has NO waiver mechanism (`framework-go/arch/filelayout.go`; the 2026-07-11 design doc: "No waiver mechanism: the gate is absolute from the moment it flips on"). A ResourceAccess package admits exactly `<leaf>access.go` + `access_test.go` + `*.gen.go`. Under Task 3's ruling (A) every new verb goes into the existing `projectstateaccess.go` / `access_test.go`; under (B) the new package `internal/resourceaccess/activityexecution` gets `activityexecutionaccess.go` + `access_test.go` and nothing else.
- `arch_bannedphase_test.go` bans the bare top-level identifier `phase`/`Phase` in `resourceaccess/projectstate` and `manager/construction` (`bannedPhaseScopeDirs`); the R3 naming ruling stands. New names say `lifecyclePhase`, `methodPhase` or `reviewRound`. A new RA package under ruling (B) must be ADDED to `bannedPhaseScopeDirs` — the gate does not walk it otherwise.
- `paramguard` (`server/internal/paramguard_arch_test.go`) walks the five **Manager** packages only (`managerDirs`); it does not scan ResourceAccess. A new required-string param on an exposed Manager op must be branched on in that Manager's method body — a call through a dependency field does not count.
- Wire-visible identifiers are NEVER renumbered: `ArtifactKind` ordinals, `ActivityType` ordinals, the root `phase`, the `AttemptID` format `<activityId>:<taskId>:<n>` (the episode ledger's `TargetRef` join depends on it) and the ActivityID map keys.
- Never run `git restore`, `git clean`, `git stash`, `git checkout -- <path>` or any tree-wide reset. Back up the gitignored SDD ledger (`.superpowers/sdd/`) to the session scratchpad after every append.
- A changed exposed-Manager op also needs: clientgen `mcpdocs` op-doc table, webApp `gen-enums OUTPUT_NAMES` for new string enums, `cmd/server/managerlog.go` if the Manager interface changes, and a systemtests SDK regen. `constructionManager.QueryActivityView` IS exposed, and Task 8 changes its response — all four obligations fire there.
- Drift gates before every commit touching generated inputs: every `gen-*-check`, `sumtype-check`, `derived-plan-check`, `encapsulation-check`, `method-check`, `fix-check`, `make lint`.
- webApp layer DAG (routes → containers → components → hooks → api) is lint-enforced; `npm run check` must be green, and the four `useActivityView` preview fixtures plus `fixture-schema.mjs` stay green at every commit.
- Match the surrounding code's and JSON's comment density, naming, key order and em-dash rationale idiom. Commit messages end with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- **Do NOT deploy in this plan.** A deploy of this stage needs in-flight construction children AND design sessions drained first (`*:nextActivity:*`, `*:construct:*`, the design session workflows) because the child workflow's command sequence and the state shape both move. Task 10 writes the release/drain note; the founder runs it.
- Out of scope, EARMARK only: deleting `ArtifactSlot.ReviewThread` (stage 6 — this wave DUAL-WRITES), deleting the stored derived fields (stage 6 — this wave stops WRITING them and emits them as computed view fields), the generic DAG child workflow and the Manager collapse (stage 4), the webApp Activity Experience itself (stage 5), and the `.claude/` materialized skills that name the folded RAs (a platform release is its own founder STOP).

## Pre-requisite (spec §8, a founder STOP before Task 1)

- [ ] **Land or abandon `lifecycle-2-conformance-gate`.** Verified 2026-09-23: the branch is UNMERGED and carries 4 commits touching exactly this stage's blast radius —
  ```
  21071224 fix(pipeline): prefer live/succeeded runs as canonical (stale failed run not selected)
  17dc17ce fix(construction): inject EscalateEverything intervention mode; probe ignores failed runs
  7f214765 fix(construction): resolve activity→component for dispatch; carry ServiceContracts across pump boundary
  3313e7df feat(construction): remove server-side LLM worker — construction is fully agentic
  ```
  `git diff --stat main...lifecycle-2-conformance-gate` = `server/cmd/server/{config,main,construction_adapters}.go`, `server/internal/manager/construction/codec.go`, `server/internal/resourceaccess/constructionpipeline/actions.go`. Stage 3 rewrites `constructactivity.go` and `constructionmanager.go` heavily; rebasing those four commits afterwards is a merge no one wants. **Ask the founder to land or abandon it, and do not start Task 1 until they answer.** (`pump-singular-per-project` is already merged — `git branch --merged main --list pump-singular-per-project` returns it — so the pump prerequisite is satisfied.)

## Task order

**Blocking entry criteria, FIRST and in one commit each:** Task 1 (the N4 duplication) → Task 2 (two rules for one phase-completion fact). Both are inert today and both corrupt revision history the instant Task 5 persists a gate rejection; each ships with a test that reproduces the corruption against a persisted rejection.

**Then the model+code wave:** Task 3 (`activityExecutionAccess` — the 12-verb contract, the slot-5 component prose and relationship labels, the re-keyed dynamic-view steps, the Go implementation, and the deletion/reduction of the three folded contract FACETS — **ONE commit**, after a founder ruling on facet-vs-component) → Task 4 (the `.activityConstruction` → `.activityExecution` shape change + `Version` + the dispositions).

**Then the writers:** Task 5 (the construction child workflow writes attempts + rounds, `GetVersion`-fenced, new replay fixtures, `registered_names_test.go` golden) → Task 6 (the design rails dual-write rounds beside `ArtifactSlot.ReviewThread`).

**Then the read and the data:** Task 7 (`QueryActivityView` reads persisted rounds; reconstruction survives only for `backfilled` rows) → Task 8 (`ActivityView` gains `verdicts[]`/`thread[]`/`subjectRef`/`round`; regen; the four webApp fixtures) → Task 9 (`cmd/migrate-activity-execution` + run it on this repo's own state) → Task 10 (docs, earmarks, release/drain note).

**Must ship together:**
- Task 3 is ONE commit: a `planned` `activityExecutionAccess` has no green posture (`SYS-RA-ORPHAN` without relationships, `DV-REL-COVERAGE` with them — both Errors, both reproduced during stage-1 planning), and `ALIGN-EXTRA-PKG` (Error) fires the moment a folded component is deleted while its Go package still exists. Component + relationships + views + contract + package + the three deletions land together or not at all.
- Task 5 is ONE commit for each fenced call site's code, its replay fixtures and the golden: a fixture recorded against half-applied code is a lie.
- Task 9's cmd and its run on `.aiarch/state/project.json` are ONE commit (the tool and the state it produced are each other's evidence).

Tasks 1 and 2 may run in parallel (disjoint functions, same file — sequence them if the same implementer holds both). Tasks 3 and 9 both touch `project.json` and are strictly sequential; nothing else may touch `project.json` while either runs.

---

### Task 1: N4 stops inventing a rejection that the ledger already holds

**Entry criterion (a), spec §8.** `appendGateAttempts` (N4) appends one `OutcomeRejected` gate attempt per `OperatorNote{Kind: sendBack, Gate: P}` with **no dedup against the ledger's own rejected attempts** for that gate. Today that is inert: `constructactivity.go` writes no attempts at all (stage-0 F1 — `cmd/backfill-attempts/main.go` is the ledger's only writer), so the notes are the only evidence and reconstructing from them is right. Task 5 makes the workflow persist a rejected gate attempt AND keep recording the note (the note is how a pending send-back is delivered to the next dispatch — `PendingOperatorNotes`, `changeOperatorNoteDelivery`). From that commit on, one real send-back yields TWO rejected gate attempts.

Two corruptions follow, and the second is the one that shows on screen:
1. **Revisions double-count.** R3 (stage-0 Task 7) makes `G`'s *n*-th attempt revision *n* of the review task, so one send-back reads as two.
2. **`passed` flips to `sentBack`.** The note-derived attempts are numbered *after* `highestAttempt(out, gate)` — i.e. after the ledger's passed attempt — so the LAST gate attempt is `rejected`, and the task state table's rule 4 reads the review task as `sentBack` on an activity whose gate has passed and whose work is merged.

**The rule:** N4 reconstructs only what the ledger does NOT hold. A note whose rejection is already an attempt of `G` is evidence of that attempt, not of another one. Match note→attempt by the same tails-aligned order R4 already uses (stage-0 Task 7 R4: notes exist only since B1.1, so an older rejection has none — the OLDEST unmatched element is the one without a partner, on whichever side is longer).

**Files:**
- Modify: `server/internal/manager/construction/constructionmanager.go` — `appendGateAttempts` (L2798–2830), the `for _, note := range row.OperatorNotes` loop at L2817–2822.
- Modify: `server/internal/manager/construction/manager_test.go` — append after the existing `TestNormalizeAttempts_*` block.

**Interfaces:**
- Consumes: `projectstate.TaskAttempt{AttemptID, Task, Phase, Attempt, Actor, StartedAt, EndedAt, Outcome, Evidence, Provenance}`, `projectstate.OutcomeRejected`, `projectstate.NoteSendBack`, `projectstate.GateTaskFor`, `highestAttempt`, `reconstructed`.
- Produces (unexported, package `construction`):
  ```go
  // ledgerRejections counts the gate task's already-recorded rejections in out.
  func ledgerRejections(out []projectstate.TaskAttempt, gate projectstate.MethodTask) int
  ```
  and a changed `appendGateAttempts` whose note loop skips the oldest `len(notes) - (len(notes) - held)` … see Step 3 for the exact code.

- [ ] **Step 1: Write the failing test.** Append to `server/internal/manager/construction/manager_test.go` (it uses the existing `avObserved` / `avSendBack` / `avTask` / `avOutcomes` helpers from stage-0 Task 7):

  ```go
  // ENTRY CRITERION (a), spec §8. Once the workflow persists a gate rejection (stage 3
  // Task 5) the send-back note that accompanies it is EVIDENCE OF THAT ATTEMPT, not of
  // another one. N4 used to append one rejected attempt per note unconditionally and
  // number it after the ledger's highest, so a single send-back read as two revisions AND
  // the last gate attempt was a rejection on an activity whose gate had already passed.
  func TestNormalizeAttempts_APersistedRejectionIsNotReconstructedTwice(t *testing.T) {
  	row := projectstate.ActivityConstructionStatus{
  		ActivityID: "C-X",
  		Attempts: []projectstate.TaskAttempt{
  			avObserved(projectstate.TaskDetailedDesign, 1, projectstate.OutcomePassed),
  			avObserved(projectstate.TaskDesignReview, 1, projectstate.OutcomeRejected),
  			avObserved(projectstate.TaskDetailedDesign, 2, projectstate.OutcomePassed),
  			avObserved(projectstate.TaskDesignReview, 2, projectstate.OutcomePassed),
  		},
  		OperatorNotes: []projectstate.OperatorNote{
  			avSendBack("detailed_design", "tighten the contract"),
  		},
  		Phases: []projectstate.PhaseCompletion{
  			{Phase: projectstate.MethodPhaseDetailedDesign, Completed: true},
  		},
  	}
  	got := normalizeAttempts("C-X", row, nil, nil)
  	gates := 0
  	var last projectstate.TaskOutcome
  	for _, a := range got {
  		if a.Task == projectstate.TaskDesignReview {
  			gates++
  			last = a.Outcome
  		}
  	}
  	if gates != 2 {
  		t.Fatalf("the ledger holds 2 designReview attempts and the note is evidence of the first; got %d:\n%s", gates, avDump(got))
  	}
  	if last != projectstate.OutcomePassed {
  		t.Fatalf("the gate passed on revision 2; the last designReview attempt reads %q", last)
  	}
  }

  // The reconstruction path is unchanged for a row that predates the ledger: a note with
  // NO recorded rejection is still the only evidence there is.
  func TestNormalizeAttempts_ANoteWithoutALedgerRejectionIsStillReconstructed(t *testing.T) {
  	row := projectstate.ActivityConstructionStatus{
  		ActivityID:    "C-X",
  		OperatorNotes: []projectstate.OperatorNote{avSendBack("detailed_design", "tighten the contract")},
  	}
  	got := normalizeAttempts("C-X", row, nil, nil)
  	gates := 0
  	for _, a := range got {
  		if a.Task == projectstate.TaskDesignReview && a.Outcome == projectstate.OutcomeRejected {
  			gates++
  		}
  	}
  	if gates != 1 {
  		t.Fatalf("a note with no ledger rejection must still reconstruct one; got %d:\n%s", gates, avDump(got))
  	}
  }

  // Tails aligned: two notes, one recorded rejection — the NEWER note is the recorded
  // one, so only the OLDER is reconstructed, and it sorts before the recorded attempt's
  // number is reached.
  func TestNormalizeAttempts_MoreNotesThanLedgerRejectionsReconstructsTheOldest(t *testing.T) {
  	row := projectstate.ActivityConstructionStatus{
  		ActivityID: "C-X",
  		Attempts: []projectstate.TaskAttempt{
  			avObserved(projectstate.TaskDesignReview, 1, projectstate.OutcomeRejected),
  		},
  		OperatorNotes: []projectstate.OperatorNote{
  			avSendBack("detailed_design", "older"),
  			avSendBack("detailed_design", "newer"),
  		},
  	}
  	got := normalizeAttempts("C-X", row, nil, nil)
  	gates := 0
  	for _, a := range got {
  		if a.Task == projectstate.TaskDesignReview {
  			gates++
  		}
  	}
  	if gates != 2 {
  		t.Fatalf("2 notes, 1 recorded rejection ⇒ exactly 1 reconstructed; got %d designReview attempts:\n%s", gates, avDump(got))
  	}
  }
  ```
  - [ ] **Verify first:** confirm the helper names `avObserved` / `avSendBack` and the constants `projectstate.TaskDetailedDesign` / `TaskDesignReview` / `MethodPhaseDetailedDesign` exist as written (`grep -n 'func avObserved\|func avSendBack\|TaskDesignReview ' server/internal/manager/construction/manager_test.go server/internal/resourceaccess/projectstate/contract.gen.go`). Add a small `avDump(attempts) string` helper beside them if one does not already exist — it prints `task/attempt/outcome` per line and is what makes these failures readable.

- [ ] **Step 2: Run them; confirm the first two fail for the right reason.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage3/server
  GOWORK=off go test ./internal/manager/construction/ -run 'TestNormalizeAttempts_APersistedRejection|TestNormalizeAttempts_ANoteWithout|TestNormalizeAttempts_MoreNotes' -count=1
  ```
  Expected: `APersistedRejection` FAILS with `got 3`, `MoreNotes` FAILS with `got 3`, `ANoteWithout` PASSES (the reconstruction path is correct today and must stay correct).

- [ ] **Step 3: Dedup N4 against the ledger.** In `appendGateAttempts` (`constructionmanager.go:2798`), replace the note loop (L2817–2822) with:

  ```go
  		// A send-back note and a RECORDED rejection of the same gate are one event, not
  		// two. The workflow records both (the note is how the feedback reaches the next
  		// dispatch — PendingOperatorNotes), so reconstructing one attempt per note on top
  		// of the ledger would double every revision AND leave a rejection as the gate's
  		// LAST attempt on an activity whose gate has passed. Tails aligned, like R4: notes
  		// exist only since B1.1, so it is the OLDEST rejections that have no note and the
  		// OLDEST notes that have no recorded rejection.
  		notes := sendBackNotesFor(row.OperatorNotes, p)
  		if unrecorded := len(notes) - ledgerRejections(out, gate); unrecorded > 0 {
  			for _, note := range notes[:unrecorded] {
  				at := note.RecordedAt
  				add(projectstate.OutcomeRejected, nil, &at, "operatorNotes["+note.NoteID+"]")
  			}
  		}
  ```

  and add, beside `highestAttempt` (after L2766):

  ```go
  // ledgerRejections counts the gate task's rejections ALREADY in out — the ones a real
  // run recorded. N4 reconstructs only the send-backs beyond them.
  func ledgerRejections(out []projectstate.TaskAttempt, gate projectstate.MethodTask) int {
  	n := 0
  	for _, a := range out {
  		if a.Task == gate && a.Outcome == projectstate.OutcomeRejected {
  			n++
  		}
  	}
  	return n
  }

  // sendBackNotesFor is the phase's send-back notes in recorded order (append-only slice
  // order IS RecordedAt order).
  func sendBackNotesFor(notes []projectstate.OperatorNote, p projectstate.ActivityMethodPhase) []projectstate.OperatorNote {
  	out := make([]projectstate.OperatorNote, 0, len(notes))
  	for _, note := range notes {
  		if note.Kind == projectstate.NoteSendBack && note.Gate == string(p) {
  			out = append(out, note)
  		}
  	}
  	return out
  }
  ```

  Note that `ledgerRejections` is called on `out` — which at this point already holds N1/N2/N3 output for earlier phases but only ever LEDGER rejections for `gate`, because N2 mints work attempts only (an episode's `TargetRef` names a dispatch task) and N3 mints one pending work attempt. N4 is the only producer of rejected gate attempts, and it runs one phase at a time.

- [ ] **Step 4: Re-run the three tests and the whole derivation suite.**
  ```bash
  GOWORK=off go test ./internal/manager/construction/ -run 'TestNormalizeAttempts|TestDeriveTaskViews|TestQueryActivityView' -count=1
  ```
  Expected: all green. If a stage-0 test that asserts a reconstructed count now fails, read it before touching it: a test built on a row with BOTH a ledger rejection and a note was asserting today's double-count and its expectation is the defect.

- [ ] **Step 5: Gates and commit.**
  ```bash
  GOWORK=off make test-short && GOWORK=off make lint
  ```
  Commit `fix(construction): a send-back note and its recorded rejection are one gate attempt` with the trailer `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.

---

### Task 2: ONE rule for phase completion — the resolved set, not the stored slice

**Entry criterion (b), spec §8.** `QueryActivityView` calls `projectstate.ResolveConstructionRow(row, item)` and throws its third return away (`constructionmanager.go:797` — `typ, variant, _, classified := …`). Its resolved completions are then re-derived TWICE, differently:
1. `appendGateAttempts` builds its completion map straight from the RAW `row.Phases` (`constructionmanager.go:2800–2803`);
2. `activityViewFrom` emits `ActivityLifecyclePhase.Completed` from the derived TASK state (`constructionmanager.go:3190–3193` — `Completed: states[ph.Gate] == taskPassed`), i.e. from `deriveTaskViews` over `normalizeAttempts`' reconstructed list.

So `ActivityView.Phases[i].Completed` and `resolved[i].Completed` are two independently computed answers to "is this lifecycle phase done", and neither is `ResolvePhaseCompletions`. `ResolvePhaseCompletions`' own doc says which one is right:

> THE PHASE ROW SET COMES FROM THE PROFILE; THE STORED SLICE ONLY SUPPLIES STATE. When the two disagree, the profile wins. […] Stored phases the profile does not carry are dropped; profile phases the store never had are materialized with unknown state.

Today the divergence is mostly invisible because a live run's `Phases` is seeded from the same profile. It stops being invisible in stage 3 for two reasons: (i) §5.3 stops STORING `Phases` at all, so the raw slice becomes stale-or-absent while the resolved set stays correct, and (ii) a row whose dispatch-time type differs from its read-time classification (`ResolvePhaseCompletions`' own named case) reconstructs a passed gate attempt for a phase its lifecycle does not have — a gate attempt on a task the lifecycle graph cannot place.

**The rule:** `ResolveConstructionRow`'s `resolved` set is the ONE answer. `normalizeAttempts` takes it instead of reading `row.Phases`; `activityViewFrom` takes it instead of reading `states[ph.Gate]`. `canonicalMethodPhases` stops being a second phase inventory — N4 walks the resolved set, which is already profile-ordered.

**Files:**
- Modify: `server/internal/manager/construction/constructionmanager.go` — `QueryActivityView` L797, L820 and L821; `normalizeAttempts` L2690; `appendGateAttempts` L2798–2803; `canonicalMethodPhases` L2679–2688 (deleted); `activityViewFrom` L3175 (signature) and L3190–3193 (the `Completed` expression).
- Modify: `server/internal/manager/construction/manager_test.go` — the `normalizeAttempts` callers in the stage-0 suite gain the resolved argument.

**Interfaces:**
- Consumes: `projectstate.ResolveConstructionRow(r ActivityConstructionStatus, meta ActivityItem) (typ ActivityType, variant TestingVariant, resolved []PhaseCompletion, classified bool)` (`projectstateaccess.go:8304`); `projectstate.PhaseCompletion{Phase, Completed, CompletedAt}`.
- Produces (changed signatures, package `construction`):
  ```go
  func normalizeAttempts(activityID string, row projectstate.ActivityConstructionStatus, resolved []projectstate.PhaseCompletion, episodes []episode.EpisodeRecord, live *ConstructionSessionView) []projectstate.TaskAttempt
  func appendGateAttempts(out []projectstate.TaskAttempt, activityID string, row projectstate.ActivityConstructionStatus, resolved []projectstate.PhaseCompletion, live *ConstructionSessionView) []projectstate.TaskAttempt
  func activityViewFrom(activityID ActivityID, item projectstate.ActivityItem, typ projectstate.ActivityType, variant projectstate.TestingVariant, lc methodassets.Lifecycle, resolved []projectstate.PhaseCompletion, tasks []taskView) ActivityView
  ```
  `canonicalMethodPhases` is **deleted** — its only caller was `appendGateAttempts`.
  - [ ] **Verify first:** `grep -n 'canonicalMethodPhases' server/internal/manager/construction/*.go` must show only the declaration and the `appendGateAttempts` loop. If a test uses it, keep it as a test-local literal rather than re-exporting the production inventory.

- [ ] **Step 1: Write the failing test.** Append to `manager_test.go`:

  ```go
  // ENTRY CRITERION (b), spec §8. ResolveConstructionRow reconciles the stored phase
  // slice against the profile — dropping stored phases the profile does not carry and
  // materializing profile phases the store never had — and QueryActivityView threw that
  // reconciliation away while N4 re-derived completion from the raw slice. Two rules for
  // one fact. A row stamped at dispatch as one type and classified at read as another is
  // the case the resolver exists for, and the raw slice reconstructs a passed gate for a
  // phase this activity's lifecycle does not have.
  func TestNormalizeAttempts_GateCompletionComesFromTheResolvedSetOnly(t *testing.T) {
  	row := projectstate.ActivityConstructionStatus{
  		ActivityID: "N-STP",
  		// Seeded from the zero-value (service) phase set at dispatch; the read-time
  		// classification is testing, whose lifecycle has no detailed_design phase.
  		Phases: []projectstate.PhaseCompletion{
  			{Phase: projectstate.MethodPhaseDetailedDesign, Completed: true},
  		},
  	}
  	item := projectstate.ActivityItem{Name: "N-STP", WorkerClass: "test-engineer", Coding: false}
  	_, _, resolved, classified := projectstate.ResolveConstructionRow(row, item)
  	if !classified {
  		t.Fatalf("N-STP must classify; the fixture is wrong")
  	}
  	got := normalizeAttempts("N-STP", row, resolved, nil, nil)
  	for _, a := range got {
  		if a.Phase == projectstate.MethodPhaseDetailedDesign {
  			t.Fatalf("a detailed_design gate attempt on an activity whose resolved profile has no such phase: %+v\nresolved=%+v", a, resolved)
  		}
  	}
  }

  // The view's phase completion and the resolver's must be the SAME fact. A row whose
  // gate task has a passed ATTEMPT but whose stored slice says otherwise (or vice versa)
  // used to render one answer on the Activity Experience and another to the pump.
  func TestActivityViewFrom_PhaseCompletionIsTheResolvedSet(t *testing.T) {
  	lc := avServiceLifecycle()
  	resolved := []projectstate.PhaseCompletion{
  		{Phase: projectstate.MethodPhaseRequirements, Completed: true},
  		{Phase: projectstate.MethodPhaseDetailedDesign, Completed: false},
  	}
  	// No task views at all: the derived task states are all pending, so the OLD rule
  	// (states[ph.Gate] == taskPassed) says nothing is complete.
  	got := activityViewFrom("C-X", projectstate.ActivityItem{Name: "C-X"}, projectstate.ActivityTypeService, "", lc, resolved, nil)
  	for _, ph := range got.Phases {
  		want := ph.ID == string(projectstate.MethodPhaseRequirements)
  		if ph.Completed != want {
  			t.Fatalf("phase %s completed=%v, want %v — the view must report the resolved set", ph.ID, ph.Completed, want)
  		}
  	}
  }
  ```
  - [ ] **Verify first:** confirm `N-STP` + `workerClass: test-engineer, coding: false` really classifies as `ActivityTypeTesting` and that the testing profile has no `detailed_design` phase — `GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestResolveConstructionRow -count=1 -v` and `jq -r '.slots["9"].model.activities[]|select(.name=="N-STP")' .aiarch/state/project.json`. If the fixture does not reproduce, substitute any (row type, read type) pair where the two profiles differ and say which in the test's comment — the POINT (one rule, the resolved one) does not move.

- [ ] **Step 2: Run it; it must fail** with `a detailed_design gate attempt on an activity whose resolved profile has no such phase` — and it must fail to COMPILE first, because `normalizeAttempts` does not take `resolved` yet. Add the parameter in Step 3, not before: the compiler error IS the proof that every caller is accounted for.

- [ ] **Step 3: Thread the resolved set through.**
  - `QueryActivityView` (L797): `typ, variant, resolved, classified := projectstate.ResolveConstructionRow(row, item)`.
  - `QueryActivityView` (L820): `tasks := deriveTaskViews(lc, normalizeAttempts(id, row, resolved, records, live), row.OperatorNotes, liveGate)`.
  - `normalizeAttempts` (L2690) takes `resolved []projectstate.PhaseCompletion` after `row` and forwards it to `appendGateAttempts`.
  - `appendGateAttempts` replaces its map build (L2800–2803) and its loop head (L2804) with:
    ```go
  	// ResolveConstructionRow's reconciled set IS the phase inventory and the completion
  	// state, in profile order. There is no second inventory: canonicalMethodPhases was one,
  	// and two inventories over one row is exactly what ResolvePhaseCompletions exists to
  	// remove ("when the two disagree, the profile wins").
  	for _, pc := range resolved {
  		p := pc.Phase
    ```
    and the completion arm becomes `case pc.Completed && !passed: add(projectstate.OutcomePassed, nil, pc.CompletedAt, "phases["+string(p)+"].completed")`.
  - Delete `canonicalMethodPhases` (L2679–2688).
  - `activityViewFrom` (L3175) takes `resolved []projectstate.PhaseCompletion` before `tasks`, and its phase loop (L3190–3193) becomes:
    ```go
  	done := make(map[projectstate.ActivityMethodPhase]bool, len(resolved))
  	for _, pc := range resolved {
  		done[pc.Phase] = pc.Completed
  	}
  	for _, ph := range lc.Phases {
  		// ONE rule: ResolvePhaseCompletions. The gate task's derived STATE is a view of
  		// the same evidence, but it is derived through normalizeAttempts' reconstruction
  		// and can disagree with the resolver over a partial row — and a screen that
  		// disagrees with the pump about whether a phase is done is the defect this
  		// collapses.
  		view.Phases = append(view.Phases, ActivityLifecyclePhase{
  			ID: ph.ID, Label: ph.Label, Weight: int64(ph.Weight), GateTaskID: ph.Gate,
  			Completed: done[projectstate.ActivityMethodPhase(ph.ID)],
  		})
  	}
    ```
  - [ ] **Verify first:** `PhaseCompletion` must carry `CompletedAt *time.Time` (today's `stored[p].CompletedAt` is already passed as a pointer, so it does). Confirm with `grep -n 'type PhaseCompletion' -A6 server/internal/resourceaccess/projectstate/contract.gen.go`.

- [ ] **Step 4: Fix the test callers.** Every stage-0 `normalizeAttempts(...)` call in `manager_test.go` gains the resolved argument. For fixtures that were passing a row with stored `Phases` and asserting today's behaviour, pass `projectstate.ResolvePhaseCompletions(projectstate.ProfileFor(typ, variant), row.Phases, row.Attempts)` rather than the raw slice — the test then asserts the ONE rule too.

- [ ] **Step 5: Re-run and gate.**
  ```bash
  GOWORK=off go test ./internal/manager/construction/ -run 'TestNormalizeAttempts|TestDeriveTaskViews|TestQueryActivityView' -count=1
  GOWORK=off make test-short && GOWORK=off make lint
  ```
  Commit `fix(construction): phase completion is the resolved set, not the stored slice` with the trailer.


---

### Task 3: `activityExecutionAccess` — the model and the code, ONE commit

> ### ⚠️ RECON CORRECTION — read before executing
>
> The spec (§5.3, §10) and the stage-1 earmark both speak of `activityExecutionAccess` as a new ResourceAccess **component**, folding "three RAs". **That is not the shape of the model.** Verified by `jq` against the live state at `69e7a8fe`:
>
> ```bash
> jq -c '.slots["5"].model.components[] | select(.id|test("access")) | {id, contractKey}' .aiarch/state/project.json
> jq -c '.serviceContracts | to_entries | map({k:.key, comp:.value.component, gp:.value.goPackage})' .aiarch/state/project.json
> ```
> There is **no** `construction-transition-access`, `git-activity-status-access` or `design-session-access` component. There is exactly ONE component, `project-state-access` (`contractKey: "projectStateAccess"`), whose own `encapsulates` prose says so:
> > "one component, **four contract facets** — project-state …, construction-transition …, git-activity-status …, and design-session … . **Facets are contracts, not components** (ratified facet doctrine — see operational concepts)."
>
> All four `.serviceContracts` entries carry `"component": "projectStateAccess"` and the same `"goPackage": "internal/resourceaccess/projectstate"`. So there are **no component deletions to make**, `ALIGN-EXTRA-PKG` cannot fire (no package is orphaned), and the stage-1 `SYS-RA-ORPHAN` / `DV-REL-COVERAGE` reproductions describe the OTHER reading — a brand-new component with its own package.
>
> Two readings, and the founder must pick one:
>
> | | **(A) Fifth facet** — recommended | **(B) New component** — spec-literal |
> |---|---|---|
> | Model | a 5th `.serviceContracts` entry `activityExecutionAccess` with `component: "projectStateAccess"`, `goPackage: internal/resourceaccess/projectstate`; `constructionTransitionAccess` + `gitActivityStatusAccess` deleted; `designSessionAccess` reduced | a new `activity-execution-access` component + relationships + dynamic-view steps + a new `goPackage` |
> | Gates today | green: no new component ⇒ no `SYS-RA-ORPHAN`, no `DV-REL-COVERAGE`, no `ALIGN-*` | `SYS-RA-ORPHAN` until it has a relationship, `DV-REL-COVERAGE` until a view exercises it, and it must claim a volatility `project-state-access` does not already own |
> | Code | all new verbs land in `projectstateaccess.go` (already ~9,400 lines; `TestFileLayout` forbids a second impl file in that package) | a new package `internal/resourceaccess/activityexecution` with its own `activityexecutionaccess.go` + `access_test.go` — but it is a second `*GitStore` over the SAME repo and the same `applyMutationOnBranchFiles` funnel |
> | Doctrine | matches the ratified facet doctrine the model already states | contradicts it unless the doctrine is amended |
>
> **Recommendation: (A).** The fold the spec asks for — "one verb family for every activity" — is a CONTRACT fold, and the model already ruled that facets are contracts. (B) buys a smaller impl file at the cost of two Error-class gates, a fabricated volatility claim, and a second `GitStore` over one repo. If the founder rules (B), Step 0b below lists exactly what changes.
>
> - [ ] **Step 0: STOP for the founder's ruling** — (A) or (B). Do not start the task without it. Record the ruling in `docs/bugs/2026-09-24-stage3-rail-earmarks.md` (Task 10).

Everything below is written for **(A)**.

**What folds.** The twelve §5.3 verbs replace 18 of today's 26 facet ops:

| New verb | Replaces |
|---|---|
| `OpenActivity` | `gitActivityStatusAccess.RecordActivityStarted` + `constructionTransitionAccess.RecordPhaseStarted` |
| `StageTaskOutput` | `designSessionAccess.StageArtifactForReviewOnBranch` |
| `RecordAttemptOutcome` | (new — nothing writes attempts today) |
| `OpenReviewRound` | (new) |
| `AppendReviewVerdict` | `designSessionAccess.RejectArtifactOnBranchWithComments` + `SeedReviewCommentsOnBranch` + `constructionTransitionAccess.RecordOperatorNote` (send-back arm) |
| `SetReviewCommentStatus` | `designSessionAccess.SetReviewCommentStatusOnBranch` |
| `DecideReviewRound` | `constructionTransitionAccess.RecordPhaseCompleted` + `RecordChangeReviewed` |
| `CommitActivityArtifacts` | `designSessionAccess.CommitArtifactWithProvenance` + `constructionTransitionAccess.RecordServiceContractProduced` + `RecordPhaseArtifactProduced` + `gitActivityStatusAccess.RecordActivityMerged` |
| `RecordActivityOutcome` | `constructionTransitionAccess.RecordActivityExited` + `RecordActivityFailed` + `gitActivityStatusAccess.RecordActivityCompleted` |
| `RecordOperatorNote` | `constructionTransitionAccess.RecordOperatorNote` (override/retry/takeover/requeue/skip arms) + `RecordOperatorNoteDelivered` |
| `AcknowledgeStaleBasis` | `projectStateAccess.AcknowledgeStaleBasis` |
| `ReadActivityExecution` | (new — a narrow read; `ReadProject` stays for whole-aggregate readers) |

**Not folded, and why** — each stays where it is, and the reason is written into the contract's description so the next reader does not re-litigate it:
- `designSessionAccess.ReadProjectOnBranch` / `ReconcileBranchFromMain` / `WithdrawArtifactOnBranch` — branch mechanics, not review. `ReadProjectOnBranch` is the pump's whole-aggregate read (`pumpnextactivity.go`), which is not an activity-execution read at all. `designSessionAccess` survives this wave with these three ops; its disposition is stage 4's.
- `constructionTransitionAccess.RecordOperatorPaused` / `RecordOperatorResumed` / `RecordReviewPolicy` — project-scoped, not activity-scoped. They move to `projectStateAccess` in this task (it already owns policy and project-level state), taking it from 9 ops to 12 — the App. B ceiling, and the reason no more may follow.
- `gitActivityStatusAccess.RecordActivityBranchOpened` / `RecordActivityCIObserved` / `RecordActivityArchApproved` — the git-forward mirror (PR/CI facts), not execution. They move onto `activityExecutionAccess`? **No** — they would take it to 15 ops. They stay as a reduced `gitActivityStatusAccess` facet (3 ops).
  - [ ] **Verify first:** count the resulting op totals before writing any JSON — `activityExecutionAccess` must be exactly 12, `projectStateAccess` ≤ 12, `designSessionAccess` 3, `gitActivityStatusAccess` 3. If any exceeds 12, `DH-CONTRACT-OPCOUNT-MAX` fires (Warning at 13, per stage-0 F5; an Error above) and the split must be re-cut before committing. Check with `jq '.serviceContracts | to_entries | map({k:.key, n:(.value.interface.operations|length)})'`.

**On `projectStateAccess`'s "12 dead ops (D1–D4)" (spec §5.3).** **Verified: there are none to shed, and it is safely separable because it is already done.** `projectStateAccess` has **9** ops today (`contract.gen.go:989–999`), not 20, and a caller grep over all 35 facet ops found **zero** with no non-test, non-RA caller. The "12 dead ops" is a 2026-07-20 QA finding about the committed *dynamic views* — `docs/superpowers/qa/2026-07-20-qa-findings-backlog.md:392-394`: "recordChangeReviewed appears twice (project-state-access edge 17 + construction-transition-access edge 25). Facet collapse should prune the 12 dead ops" — i.e. duplicate call-chain EDGES, not Go code. Step 5 below prunes them as part of the view re-keying, which is where they always belonged.
  - [ ] **Verify first:** re-run the caller grep before believing this. `for op in $(jq -r '.serviceContracts.projectStateAccess.interface.operations[].name' .aiarch/state/project.json); do printf '%s %s\n' "$op" "$(grep -rl "\.$op(" server/internal server/cmd --include='*.go' | grep -v '/projectstate/' | grep -cv '_test\.go')"; done`. A zero means a genuinely dead op — delete it in this task and say so.

**Files:**
- Modify: `/Users/davidmarne/mixofrealitystudio/archistrator/.aiarch/state/project.json` — `.serviceContracts.activityExecutionAccess` (new), `.serviceContracts.constructionTransitionAccess` (deleted), `.serviceContracts.gitActivityStatusAccess` (reduced to 3), `.serviceContracts.designSessionAccess` (reduced to 3), `.serviceContracts.projectStateAccess` (+3 project-scoped ops); `.slots["5"].model.components[]` → `project-state-access` (`encapsulates` prose + `atomicBusinessVerbs`); `.slots["5"].model.relationships[]` (the three Manager→`project-state-access` labels); `.slots["5"].model.dynamicViews[]` (every `calls[].label` naming a folded verb).
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — the new verbs, the new `ActivityExecution`/`ReviewRound` types, the narrowed `designSessionAccess`/`gitActivityStatusAccess` forwarders, the substrate constructors.
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` — the RA's tests.
- Regenerate (never hand-edit): `server/internal/resourceaccess/projectstate/{contract.gen.go,toolcatalog.gen.go}`, every Manager's `{contract,activities,invokers,worker}.gen.go`, `server/api/openapi.yaml`, `server/internal/client/{web,mcp}/**`, `../systemtests/internal/sdk/**`, `webApp/src/contracts/{schema.ts,enums.gen.ts}`, `webApp/src/api/ops.gen.ts`.
- Modify: `server/cmd/server/{hooks.go,construction_adapters.go}` — the composition root's variant wiring for the new facet (`activityExecutionAccess/GitLocal`, `activityExecutionAccess/GitHub`; precedent `cmd/appgen/main.go:168–194`).
- Modify: `server/internal/arch_bannedphase_test.go` — nothing (the package is already in `bannedPhaseScopeDirs`).

**Interfaces:**
- Produces, in `.serviceContracts.activityExecutionAccess` (`component: "projectStateAccess"`, `goPackage: "internal/resourceaccess/projectstate"`), generating into `contract.gen.go`:
  ```go
  type ActivityExecutionAccess interface {
  	OpenActivity(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, typ ActivityType, variant TestingVariant, pin LifecyclePin, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
  	StageTaskOutput(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, taskID string, branch string, model ModelEnvelope, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (StagedRef, Version, error)
  	RecordAttemptOutcome(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, attempt TaskAttemptInput, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
  	OpenReviewRound(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, round ReviewRoundInput, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
  	AppendReviewVerdict(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, roundID string, verdict ReviewVerdict, comments []ReviewComment, replies []ReviewReply, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
  	SetReviewCommentStatus(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, roundID string, commentID string, status string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
  	DecideReviewRound(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, roundID string, outcome ReviewRoundOutcome, decidedBy string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
  	CommitActivityArtifacts(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, artifacts CommitArtifactsInput, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
  	RecordActivityOutcome(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, outcome ActivityOutcome, reason FailureReason, detail string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
  	RecordOperatorNote(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, note OperatorNoteInput, deliveredToAttemptID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
  	AcknowledgeStaleBasis(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, kind ArtifactKind, note string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
  	ReadActivityExecution(rc fwra.Context, projectID ProjectID, activityID string) (ActivityExecution, error)
  }
  ```
  Every op returns `(…, Version, error)` and takes `expectedVersion` + `idempotencyKey`, exactly like the facets it replaces — the dedup-first probe in `applyMutationOnBranchFiles` (STEP 1, `projectstateaccess.go:1074`) is what makes a Temporal retry a no-op, and a verb that skipped the key would lose that.
- Consumes: `applyMutationOnBranchFiles` (the ONE mutation funnel: identity guard → dedup probe → version guard → pure transition → git ref-CAS commit), `ApplyReviewBatch(thread []ReviewComment, round int64, comments []ReviewComment, replies []ReviewReply) ([]ReviewComment, error)` (`projectstateaccess.go:9332` — the existing append+reply normalizer, reused verbatim for `ReviewRound.Thread`), `normalizeReviewThread`, `upsertActivityConstruction` (`:3864`).

**On the concurrency lane (spec §5.3, "a project-scoped serialized commit lane `{projectId}:statewrite`").** **Decision: do NOT build one. The existing single-writer path already is one, and a second mechanism would be a new durable singleton per project — exactly the shape the `pump-singular-per-project` fix had to clean up.** The evidence:
- `applyMutationOnBranchFiles` (`projectstateaccess.go:1021–1109`) fetches the branch tip fresh on every call, probes the idempotency ledger BEFORE the version guard, and commits `project.json` + the dedup record in ONE git commit CAS'd against `snap.Base`. "A non-fast-forward CAS loss is already `fwra.Conflict` from the satellite" — git's ref update is the cross-process gate, identical on the git-local and GitHub substrates (they differ only in `gitAuth`, `:213–221`).
- `fwra.Conflict` is a Temporal-RETRYABLE error (only `ContractMisuse` is in `NonRetryableErrorTypes`, `invokers.gen.go:39`), and the workflow already carries the re-read→re-apply loop: `(wf *workflows) applyRecovering(ctx, projectID, seed, apply)` (`constructactivity.go:2571`), bounded by `maxMutateConflictAttempts`, with `readVersionE` between attempts. `systemdesign` carries the identical discipline.
- `pumpWorkflowID(projectID) = "{projectId}:nextActivity"` (`constructionmanager.go:1079`) is already the ONE per-project workflow, and it is already merged to main.

So: **per-activity `Version` (Task 4) is an additional in-document guard inside the transition**, not a new lane — it makes two children writing DIFFERENT activities unable to corrupt each other even when the project-level CAS lets both through in sequence. `{projectId}:statewrite` is EARMARKED for stage 4, where the parallel pump actually raises contention, and Task 10 records it. Write this reasoning into the `activityExecutionAccess` contract description so the decision travels with the code.

- [ ] **Step 1: Write the RA's failing tests first** — `server/internal/resourceaccess/projectstate/access_test.go`, one per verb family, against the git-local substrate the existing tests use. The shapes that matter:

  ```go
  // Two appends of the SAME deterministic RoundID under retry produce ONE round. This is
  // the property the whole ledger rests on: Temporal retries an activity, and an append
  // that is not idempotent doubles the history it is supposed to record.
  func TestOpenReviewRound_IsIdempotentUnderRetry(t *testing.T) {
  	s, projectID := newLocalStoreWithProject(t)
  	in := ReviewRoundInput{RoundID: "C-X:designReview:1", TaskID: "designReview", Reviews: "detailedDesign", Round: 1,
  		SubjectRef: SubjectRef{Kind: SubjectCommit, Ref: "deadbeef"},
  		Reviewers:  []RoundReviewer{{Role: "architect", Actor: "system-architect", Required: true}}}
  	v1, err := s.OpenReviewRound(raCtx(), projectID, 1, "C-X", in, localCred, key("k1"))
  	if err != nil {
  		t.Fatalf("OpenReviewRound: %v", err)
  	}
  	v2, err := s.OpenReviewRound(raCtx(), projectID, 1, "C-X", in, localCred, key("k1"))
  	if err != nil || v2 != v1 {
  		t.Fatalf("a retry of the same idempotencyKey must replay, not re-apply: v1=%d v2=%d err=%v", v1, v2, err)
  	}
  	exec, err := s.ReadActivityExecution(raCtx(), projectID, "C-X")
  	if err != nil {
  		t.Fatalf("ReadActivityExecution: %v", err)
  	}
  	if len(exec.Reviews) != 1 {
  		t.Fatalf("one round, appended once; got %d: %+v", len(exec.Reviews), exec.Reviews)
  	}
  }

  // A verdict and its comments land in ONE commit — the spec's "AppendReviewVerdict
  // (verdict + its comments in one commit)". A crash between them would leave a round
  // whose verdict cites comments nobody can read.
  func TestAppendReviewVerdict_CarriesItsCommentsInTheSameCommit(t *testing.T) {
  	s, projectID := newLocalStoreWithProject(t)
  	v := openRoundFixture(t, s, projectID)
  	v, err := s.AppendReviewVerdict(raCtx(), projectID, v, "C-X", "C-X:designReview:1",
  		ReviewVerdict{ReviewerRole: "architect", Actor: "system-architect", Verdict: VerdictSendBack, Summary: "contract too wide", AttemptID: "C-X:designReview:1"},
  		[]ReviewComment{{ID: "c1", Anchor: "ops[3]", Text: "split this op", AuthorRole: "architect", Round: 1, Status: "open", Type: "changeRequest"}},
  		nil, localCred, key("k2"))
  	if err != nil {
  		t.Fatalf("AppendReviewVerdict: %v", err)
  	}
  	exec, _ := s.ReadActivityExecution(raCtx(), projectID, "C-X")
  	r := exec.Reviews[0]
  	if len(r.Verdicts) != 1 || len(r.Thread) != 1 {
  		t.Fatalf("verdict and comment must land together; verdicts=%d thread=%d", len(r.Verdicts), len(r.Thread))
  	}
  	if r.Outcome != RoundPending {
  		t.Fatalf("a verdict does not decide the round; outcome=%q", r.Outcome)
  	}
  }

  // Appends are append-only: DecideReviewRound stamps the outcome, it never rewrites a
  // verdict or a comment, and a second decision on a decided round is a Conflict.
  func TestDecideReviewRound_IsTerminalAndAppendOnly(t *testing.T) { /* … */ }

  // A required id is PRESENCE-only in the schema dialect, so the RA enforces non-emptiness
  // itself (2026-08-13 contract-strictness ruling).
  func TestActivityExecutionVerbs_RefuseEmptyIdentifiers(t *testing.T) { /* table over all 12 verbs */ }
  ```
  - [ ] **Verify first:** read the existing `access_test.go` helpers (`newLocalStoreWithProject`, `raCtx`, `localCred`, `key`) and use their real names. If they differ, use what is there — do not add parallel harness helpers to a file `TestFileLayout` allows only one of.

- [ ] **Step 2: Author the contract in `project.json`.** Add `.serviceContracts.activityExecutionAccess` matching the sibling facets' key order (`component`, `goPackage`, `interface`, `$defs`), with the 12 ops above. Delete `.serviceContracts.constructionTransitionAccess`. Reduce `gitActivityStatusAccess` to `RecordActivityBranchOpened`/`RecordActivityCIObserved`/`RecordActivityArchApproved` and `designSessionAccess` to `ReadProjectOnBranch`/`ReconcileBranchFromMain`/`WithdrawArtifactOnBranch`. Move the three project-scoped verbs onto `projectStateAccess`. Every new `$def` carries a `description` that says what it is FOR, in the surrounding register.

- [ ] **Step 3: Regenerate and let the compiler enumerate the work.**
  ```bash
  cd server && GOWORK=off make gen-models gen-fakes gen-temporal gen-client gen-internal-tools gen-sdk gen-config gen-main
  GOWORK=off go build ./... 2>&1 | head -60
  ```
  Expected: a long list of unresolved `Acts.ConstructionTransition*` / `Acts.GitStatus*` calls in `constructactivity.go` and the two co-author workflows. That list IS Task 5's and Task 6's work; in THIS task each call site is re-pointed at the nearest new verb with no behaviour change (`RecordPhaseCompleted` → `DecideReviewRound(passed)`, `RecordActivityExited/Failed` → `RecordActivityOutcome`, …). Nothing new is WRITTEN yet — attempts and rounds start being written in Task 5.

- [ ] **Step 4: Update the component in slot 5.** `project-state-access`'s `encapsulates` prose goes from "four contract facets" to **five**, naming `activity-execution` ("the append-only attempt and review-round ledgers per activity, and the stage → review → commit verb family that writes them"), and its `atomicBusinessVerbs` array gains `openActivity`, `stageTaskOutput`, `recordAttemptOutcome`, `openReviewRound`, `appendReviewVerdict`, `decideReviewRound`, `commitActivityArtifacts` and loses the verbs the fold retired. Key order and em-dash register copied from the existing entry.

- [ ] **Step 5: Re-point relationship labels and dynamic-view steps — prune the duplicate edges.** The three `*-manager → project-state-access` relationship labels name folded verbs; rewrite each to the new verb family. Then, in `.slots["5"].model.dynamicViews`, every `calls[]` entry whose `to` is `project-state-access` and whose `label` names a retired verb is re-labelled, and the duplicate `recordChangeReviewed` edge the 2026-07-20 QA finding named is removed (the fold makes it one edge, which is the whole point).
  ```bash
  jq -r '.slots["5"].model.dynamicViews[] | .key as $k | .steps[] | .calls[]? | select(.to=="project-state-access") | "\($k)\t\(.label)"' .aiarch/state/project.json
  ```
  Run that BEFORE and AFTER; the after-list must name only live verbs. `DV-REL-COVERAGE` is satisfied throughout because the `construction-manager → project-state-access` relationship never stops being exercised — it is the same edge with a new label.

- [ ] **Step 6: Run the FULL self-amendment loop, including every slot edited.**
  ```bash
  cd server && GOWORK=off make gen-models && GOWORK=off make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot ServiceContracts   # and every other slot touched
  GOWORK=off make test-short && GOWORK=off make lint && GOWORK=off make encapsulation-check && GOWORK=off make sumtype-check
  cd ../webApp && npm run gen:api && npm run gen:ops && npm run check
  ```
  - [ ] **Verify first:** the validator's slot-name vocabulary. Run `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot NoSuchSlot` and read the list it prints; use those exact names. `make method-check` does NOT carry the DH-* family — `validate` does, so BOTH must be green.

- [ ] **Step 7: Prove `make derived-plan-write` is a no-op.** Slots 9/10 are tool-only and this task must not move them.
  ```bash
  cd server && GOWORK=off make derived-plan-write && git status --short -- ../.aiarch/state/project.json
  ```
  If slot 9 or 10 moved, STOP: something in the contract fold reached the derivation, and that is a finding, not a diff to accept.

- [ ] **Step 8: Commit ONE commit** — `refactor(projectstate): activityExecutionAccess is the one staging/review/commit facet`, listing in the body the retired ops, the moved ops, and the `{projectId}:statewrite` decision. Trailer as always.

- [ ] **Step 0b (only if the founder rules (B)):** additionally create `server/internal/resourceaccess/activityexecution/{activityexecutionaccess.go,access_test.go}` (`TestFileLayout`: the impl file must be `<leaf>access.go`, the test file exactly `access_test.go`, nothing else non-generated); add the `activity-execution-access` component to `.slots["5"].model.components` with `layer: "resourceAccess"` and a name that normalizes to the package leaf (`StereotypeSuffixNormalizer` strips one trailing `access`); add `{"from":"construction-manager","to":"activity-execution-access",…}` and `{"from":"activity-execution-access","to":"project-git-repo",…}` relationships (the second is what clears `SYS-RA-ORPHAN`); add at least one `calls[]` step to `uc3-execute-construction-activity` exercising each new relationship (`DV-REL-COVERAGE`); add `"internal/resourceaccess/activityexecution"` to `bannedPhaseScopeDirs` in `arch_bannedphase_test.go`; and answer, in the component's `encapsulates`, which volatility it owns that `project-state-access` does not — an unanswerable question is the strongest argument for (A).


---

### Task 4: `.activityConstruction` → `.activityExecution` — the shape change and the `Version`

Spec §5.3's "ONE wire break, stage 3". The map is renamed and re-shaped in the same commit that teaches every reader the new shape; Task 9 migrates the DATA. The key rename alone would be a silent data loss, so the codec keeps reading the old member for one wave (Task 9 removes it).

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` — `ActivityConstructionStatus` (L7549–7591) becomes `ActivityExecution`; the map field on `projectDoc` (L1223), `Project` (L5489) and `ProjectEnvelope` (L3633); `decodeProjectDoc` (L1306), `checkActivityConstructionKeys` (L1315), `buildStateFiles` (L1488), `EncodeProject` (L3656), `ProjectEnvelope.Decode` (L3693), `upsertActivityConstruction` (L3864), `isConstructionComplete` (L853).
- Modify: `server/internal/manager/construction/constructionmanager.go` (L795, L1452, L1459), `constructactivity.go` (L1209, L1226), `server/internal/manager/systemdesign/systemdesignmanager.go` (L3170, L3171, L4035–4036 and its `$defs`-generated `ActivityConstruction` field at `contract.gen.go:444`).
- Modify: `.aiarch/state/project.json` — `.serviceContracts.systemDesignManager["$defs"]` (the `ProjectState.ActivityConstruction` property → `activityExecution`), `.serviceContracts.activityExecutionAccess["$defs"]` (the new `ActivityExecution`/`ReviewRound`/`TaskAttempt` shapes).
- Modify: `webApp/src/contracts/{types.ts,wire.ts}` and every construction view that reads `ProjectState.ActivityConstruction`.

**Interfaces:**
- Produces (hand-written, package `projectstate`; the `$defs` counterpart is generated into `contract.gen.go`):
  ```go
  // ActivityExecution is the per-activity execution record: two APPEND-ONLY ledgers and
  // the sticky head facts. Everything else about an activity — its revisions, its task
  // states, its lifecycle-phase completions, its coarse build status, its earned value —
  // is DERIVED from these on read (spec §5.3). A derived field that is also stored is two
  // answers to one question, and this type deliberately holds only one of each.
  type ActivityExecution struct {
  	ActivityID    string          `json:"activityId"`
  	Type          ActivityType    `json:"type,omitempty"`
  	Variant       TestingVariant  `json:"variant,omitempty"`
  	LifecyclePin  LifecyclePin    `json:"lifecyclePin"`
  	StartedAt     *time.Time      `json:"startedAt,omitempty"`
  	CompletedAt   *time.Time      `json:"completedAt,omitempty"`
  	FailureReason FailureReason   `json:"failureReason,omitempty"`
  	FailureDetail string          `json:"failureDetail,omitempty"`
  	Attempts      []TaskAttempt   `json:"attempts,omitempty"`
  	Reviews       []ReviewRound   `json:"reviews,omitempty"`
  	Produced      []ProducedArtifact `json:"produced,omitempty"`
  	OperatorNotes []OperatorNote  `json:"operatorNotes,omitempty"`
  	Version       int64           `json:"version"`
  }

  // LifecyclePin names the method-assets lifecycle this activity was opened against, so a
  // platform release that changes a lifecycle cannot retro-actively re-shape a finished
  // activity's history.
  type LifecyclePin struct {
  	ID      string `json:"id"`
  	Version string `json:"version"`
  }

  // ReviewRound is one review occurrence: who was asked, what they said, the thread that
  // carried it, and the decision. APPEND-ONLY as a container; the only in-place mutations
  // are a comment's Status and Replies (ApplyReviewBatch), because comments are replied to
  // and resolved after the attempt that raised them has ended — which is exactly why the
  // attempt ledger alone cannot hold a review.
  type ReviewRound struct {
  	RoundID    string          `json:"roundId"`   // "<activityId>:<reviewTaskId>:<n>"
  	TaskID     string          `json:"taskId"`
  	Reviews    string          `json:"reviews"`   // the judged dispatch task
  	Round      int             `json:"round"`
  	SubjectRef SubjectRef      `json:"subjectRef"`
  	Reviewers  []RoundReviewer `json:"reviewers,omitempty"`
  	Verdicts   []ReviewVerdict `json:"verdicts,omitempty"`
  	Thread     []ReviewComment `json:"thread,omitempty"` // the EXISTING type, unchanged
  	Outcome    ReviewRoundOutcome `json:"outcome"`
  	DecidedAt  *time.Time      `json:"decidedAt,omitempty"`
  	DecidedBy  string          `json:"decidedBy,omitempty"`
  	Provenance AttemptProvenance `json:"provenance"`
  }

  type SubjectRef struct {
  	Kind SubjectKind `json:"kind"` // commit | slot | none
  	Ref  string      `json:"ref"`  // the StagedRef sha for a commit
  }

  type RoundReviewer struct {
  	Role     string `json:"role"`
  	Actor    string `json:"actor,omitempty"`
  	Required bool   `json:"required"`
  }

  type ReviewVerdict struct {
  	ReviewerRole string     `json:"reviewerRole"`
  	Actor        string     `json:"actor,omitempty"`
  	Verdict      VerdictKind `json:"verdict"` // approve | sendBack | abstain
  	Summary      string     `json:"summary,omitempty"`
  	At           time.Time  `json:"at"`
  	AttemptID    string     `json:"attemptId,omitempty"`
  }
  ```
  `VerdictKind`, `ReviewRoundOutcome` (`passed|sentBack|pending`) and `SubjectKind` are closed string enums with `x-enum-varnames`, so `make sumtype-check` covers every switch over them — and no switch may grow a `default:` arm.
- Removed from the row, and NOT replaced: `Phase`, `Phases`, `CurrentPhase`, `Kind`, `BuildStatus`. They become computed view fields through stage 5 (spec §5.3 dispositions) — `EffectiveConstructionPhase` / `CoarseBuildStatusFor` / `ResolvePhaseCompletions` already derive them, and they now derive from `Attempts` + `Reviews` alone.
  - [ ] **Verify first:** `ResolvePhaseCompletions` currently takes `(profile, stored []PhaseCompletion, attempts []TaskAttempt)` and uses `stored` as the base for state. With `Phases` gone, the base must be the profile alone and the state must come entirely from `phaseCompleteFromAttempts` (`projectstateaccess.go:7518` — "complete iff the phase's GATE task's LATEST attempt passed"). Read that function and confirm it decides for every phase that has any gate attempt; a phase with none is `unknown`, which is the honest answer and what the profile materialization already produces.

- [ ] **Step 1: Write the codec test first** — a row written in the OLD shape must still decode, and a row written in the NEW shape must round-trip with nothing lost. `CodecCarriesEveryMember` (`projectstateaccess.go:9186`) is the guard the derived-plan writer already uses and it is exactly the right tool:
  ```go
  // The one wire break of the wave. A legacy document's activityConstruction member must
  // decode into ActivityExecution (Task 9 rewrites it; until then, production reads it),
  // and the codec must not DROP anything on the way back out — the half no equality check
  // can supply (CodecCarriesEveryMember's own doc).
  func TestActivityExecution_LegacyRowDecodesAndTheCodecLosesNothing(t *testing.T) {
  	lost, err := CodecCarriesEveryMember(json.RawMessage(legacyActivityConstructionJSON), json.RawMessage(mustEncode(t, mustDecode(t, legacyActivityConstructionJSON))))
  	if err != nil {
  		t.Fatalf("CodecCarriesEveryMember: %v", err)
  	}
  	// The DERIVED members are the only permitted losses, and they are named, not a count.
  	want := []string{"activityConstruction.C-X.buildStatus", "activityConstruction.C-X.currentPhase", "activityConstruction.C-X.kind", "activityConstruction.C-X.phase", "activityConstruction.C-X.phases"}
  	if !slices.Equal(lost, want) {
  		t.Fatalf("codec losses:\n got %v\nwant %v", lost, want)
  	}
  }
  ```
  - [ ] **Verify first:** run it against TODAY's codec before changing anything — it must report `[]`. That baseline is what proves the later list is caused by this task and nothing else.

- [ ] **Step 2: Rename and re-shape.** `ActivityConstructionStatus` → `ActivityExecution` with the fields above; the map member `activityConstruction` → `activityExecution` on `projectDoc`, with a `legacyActivityConstruction` shadow member the decoder reads when `activityExecution` is absent and the encoder never writes. Add the deprecation sentence naming Task 9 and stage 6.

- [ ] **Step 3: Per-activity `Version` in the transition.** Every `activityExecutionAccess` verb's transition function reads `exec.Version`, refuses a mismatch with `fwra.Conflict` naming BOTH versions, and stamps `exec.Version++` on success. The project-level `expectedVersion` guard is untouched and both run: the project version is the git-CAS token, the activity version is what stops two children interleaving on one activity.
  ```go
  // withActivityVersion wraps a narrow activity transition in the per-activity optimistic
  // check. The project-level guard (loadAggregateForMutation, STEP 3) is the CAS token for
  // the whole document; this one is scoped to the row, so two children writing DIFFERENT
  // activities never contend, and two writers on the SAME activity cannot interleave.
  func withActivityVersion(activityID string, expected int64, apply func(*ActivityExecution) error) func(*Project) error
  ```
  - [ ] **Verify first:** decide whether the workflow can supply `expected` honestly. `applyRecovering` re-reads the PROJECT version on Conflict (`readVersionE`), not the activity version. If the workflow cannot cheaply read the activity version, pass `expected = 0` meaning "no expectation" and have the RA accept it — an honest no-op guard beats a fabricated one. Record which you chose and why in the function's doc.

- [ ] **Step 4: Re-point every reader.** The compiler enumerates them; the list in **Files** above is the verified inventory (10 production readers, 12 RA writer sites, `cmd/backfill-attempts`, and the systemdesign Manager's own `$defs`-generated copy). `cmd/backfill-attempts` is re-pointed but NOT re-run — its output is already committed state and Task 9 migrates it.

- [ ] **Step 5: webApp.** `ProjectState.ActivityConstruction` is what the SPA reads (`contract.gen.go:444`, wire tag `"ActivityConstruction"`). Regenerate, then fix `webApp/src/contracts/{types.ts,wire.ts}` and the construction views. `npm run check` and the construction graph/list/detail goldens must pass — if a golden moves, prove the rendered data is identical before accepting it.

- [ ] **Step 6: Gates and commit.** Full self-amendment loop (Task 3 Step 6) plus `GOWORK=off make test-short`, `make lint`, `cd webApp && npm run check`. Commit `feat(projectstate): activityExecution replaces activityConstruction (the wave's one wire break)`.

---

### Task 5: The construction child workflow WRITES attempts and review rounds

Stage-0 F1, verified again here: `constructactivity.go` (2,604 lines) writes no attempts and no review data. `nextTaskAttempt` (~L357) is workflow-local and its own comment says "no live writer appends to that ledger yet". `recordChangeReviewed` (L2504) carries no verdict payload. `completePhase` (L1707) calls `RecordPhaseCompleted` with an empty `artifactRef`. Every gate roster the review engine computes is thrown away after it is displayed.

This task makes each of those a real write — and every write is a Temporal **Activity**, so the workflow's command sequence changes at five points and each needs a `GetVersion` fence.

**Files:**
- Modify: `server/internal/manager/construction/constructactivity.go` — `runPipeline` (L1398), `runPhaseGate` (L1480), `awaitPhaseDecision` (L1540), `completePhase` (L1707), `finalizeActivity` (L1347), `proposeReviewSet` (L2205), and a new `const changeExecutionLedger = "execution-ledger-writes"` beside `changeOperatorNoteDelivery` (L1785).
- Modify: `server/internal/manager/construction/manager_test.go` — new workflow tests + the new replay scenarios.
- Add: `server/internal/manager/construction/testdata/replay/post-stage3/*.json` (recorded, not hand-written).
- Modify: `server/internal/registered_names_test.go` — the `registeredTemporalNamesGolden` literal (L69–265).

**Interfaces:**
- Consumes (generated by temporalgen from Task 3's contract into `activities.gen.go` / `invokers.gen.go`, registered as `"activityExecutionAccess.<op>"`): `wf.Acts.ActivityExecutionOpenActivity`, `…RecordAttemptOutcome`, `…OpenReviewRound`, `…AppendReviewVerdict`, `…DecideReviewRound`, `…CommitActivityArtifacts`, `…RecordActivityOutcome`, `…RecordOperatorNote`, `…StageTaskOutput`.
- Consumes: `review.ReviewSet{Reviewers []Reviewer, RequiresHuman bool, Reason string, ArtifactKind ReviewArtifactKind}` and `review.Reviewer{Role, Perspective, ReferenceArtifact string, MayAmend bool}` (`engine/review/contract.gen.go:47–64`) — this is the roster persisted per round.
- Produces: no new exported Go surface; five new activity invocations in the workflow's command sequence.

**The `GetVersion` fence.** The repo's idiom, copied from `changeOperatorNoteDelivery` (`constructactivity.go:1220–1228`) — a named const, called unconditionally at a fixed point so a new execution records the marker before its first write and an execution that recorded none stays wholly old:
```go
// EXECUTION LEDGER (stage 3). GetVersion is always called here, so a new execution
// records the marker before its first dispatch and an execution that recorded none stays
// wholly old: no attempt recorded, no round opened, no verdict appended. Its history has
// no events for those Activities and never will.
state.executionLedger = workflow.GetVersion(ctx, changeExecutionLedger, workflow.DefaultVersion, 1) >= 1
```
ONE change-id covers all five write points because they are one feature and an execution is either on it or off it — the same argument `changeLedgerPartialResume` already makes by reusing its const at two call sites (`constructactivity.go:1208`, `pumpnextactivity.go:345`). **Do not** mint five ids: a half-fenced execution would open a round and never decide it.

- [ ] **Step 1: Write the workflow tests first** — Temporal test-suite cases, per spec §9:
  ```go
  // A send-back must now leave a ROUND, not just a note: the roster the engine computed,
  // the verdict that was given, the comments that rode with it, the round number, and the
  // subject it judged. Before stage 3 the only trace was an OperatorNote string.
  func Test_Construct_SendBack_PersistsTheReviewRound(t *testing.T) {
  	var ts testsuite.WorkflowTestSuite
  	env := ts.NewTestWorkflowEnvironment()
  	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseDetailedDesign))
  	deps := gateDeps(ps)
  	deps.Review = review.NewReviewEngine()
  	registerConstruct(env, newWorkflows(deps), ps, newFakePipeline())
  	env.RegisterDelayedCallback(b12SendBack(env, "detailed_design", "split the contract"), 30*time.Second)
  	env.RegisterDelayedCallback(b12Decide(env, "detailed_design", PhaseApprove), 90*time.Second)
  	b12Run(env)
  	if err := env.GetWorkflowError(); err != nil {
  		t.Fatalf("workflow error: %v", err)
  	}
  	exec := ps.execution("C-B12")
  	if len(exec.Reviews) != 2 {
  		t.Fatalf("a send-back then an approval is TWO rounds; got %d: %+v", len(exec.Reviews), exec.Reviews)
  	}
  	r1 := exec.Reviews[0]
  	if r1.Outcome != projectstate.RoundSentBack || r1.Round != 1 {
  		t.Fatalf("round 1 must be a recorded send-back; got outcome=%q round=%d", r1.Outcome, r1.Round)
  	}
  	if len(r1.Reviewers) == 0 {
  		t.Fatalf("the round must carry the roster the engine computed; got none")
  	}
  	if len(r1.Verdicts) != 1 || r1.Verdicts[0].Verdict != projectstate.VerdictSendBack {
  		t.Fatalf("the human's verdict must be a row in Verdicts; got %+v", r1.Verdicts)
  	}
  	if r1.SubjectRef.Ref == "" {
  		t.Fatalf("a round must name what it judged (the staged ref); got %+v", r1.SubjectRef)
  	}
  	if exec.Reviews[1].Round != 2 {
  		t.Fatalf("the redraft opens round 2; got %d", exec.Reviews[1].Round)
  	}
  }

  // Attempts are written by the RUN now, not reconstructed. The work task and the gate
  // task each get one per revision, and the AttemptID format is unchanged so the episode
  // ledger's TargetRef join survives.
  func Test_Construct_WritesTheAttemptLedger(t *testing.T) { /* … assert AttemptIDs "C-B12:detailedDesign:1", ":2", "C-B12:designReview:1", ":2" … */ }

  // An old execution keeps its old history: with the marker absent, nothing is written.
  func Test_Construct_WithoutTheLedgerFence_WritesNothing(t *testing.T) { /* … */ }
  ```
  - [ ] **Verify first:** `b12SendBack` may not exist — the existing harness has `b12Decide(env, gate, PhaseApprove)`. Read `manager_test.go` around `b12Decide` and add the send-back variant beside it (same file; `TestFileLayout` allows exactly one test file here, so it must go there).

- [ ] **Step 2: Open the activity and pin the lifecycle.** `ConstructActivityWorkflow` (L1000) calls `OpenActivity` where it calls `recordActivityStarted` today, passing the `LifecyclePin` from `methodassets.LifecycleFor(key)`. Under the fence.

- [ ] **Step 3: Record every attempt.** `runPipeline` (L1398) records a `pending` attempt before dispatch and its outcome after — `RecordAttemptOutcome`, with `AttemptID` from the existing `projectstate.AttemptID(activityID, task, n)` and `Evidence` = the episode it just captured. This is where `nextTaskAttempt`'s workflow-local counter stops being the only record of the attempt it mints.

- [ ] **Step 4: Open a round at every gate, with the roster.** `runPhaseGate` (L1480) calls `proposeReviewSet` (L2205) and TODAY discards the result after display. It now calls `OpenReviewRound` with `Reviewers` built from `ReviewSet.Reviewers` (`Required = ReviewSet.RequiresHuman` for the human row), `SubjectRef` = the staged ref of the work attempt that reached the gate, and `Round` = the gate task's attempt number. The round is opened even when the engine refused (`reviewSetError` non-nil) — with an empty roster and the refusal in `Provenance.Basis`, because a gate that happened with no recorded roster is still a gate that happened.

- [ ] **Step 5: Append the verdict, then decide.** `awaitPhaseDecision` (L1540) appends the human's verdict (`AppendReviewVerdict`, with the send-back's comments in the same commit) and then calls `DecideReviewRound(passed|sentBack)`. Two verbs, not one, because agent reviewers append their verdicts to the same round before the human's and the decision is a separate, terminal fact. `completePhase` (L1707) stops calling `RecordPhaseCompleted` — the round's `passed` outcome IS the phase's exit criterion (App. A), and Task 4 removed the stored `Phases`.

- [ ] **Step 6: `NoteSendBack` stops being written.** Spec §5.3: "pending feedback = the latest round's open comments". `awaitPhaseDecision`'s `recordOperatorNote(NoteSendBack, …)` call is deleted; `PendingOperatorNotes` (`projectstateaccess.go:7621`) drops `NoteSendBack` from its delivered-kinds switch, and the redraft prompt takes its feedback from `latestRound.Thread` filtered to `Status == "open"`. The other note kinds (`NoteRetry`, `NoteTakeover`, `NoteReassign`, `NoteRequeue`, `NoteSkip`) are untouched — `OperatorNotes` is NARROWED, not deleted.
  - [ ] **Verify first:** trace `renderOperatorNotes` / `submitCarryingNotes` (L1757–2128) and confirm the send-back text reaches the agent prompt through that path and nowhere else. If a second path exists, re-point it in this step — a redraft that loses its feedback is worse than the defect this wave fixes.

- [ ] **Step 7: Run the tests, then the EXISTING replay fixtures.**
  ```bash
  GOWORK=off go test ./internal/manager/construction/ -run 'Test_Construct_|Test_Replay_' -count=1
  ```
  All 13 existing fixtures under `testdata/replay/{pre-b1,pre-d,post-b1,post-b17}` must be green with the fence in place. A non-determinism error here means the fence is in the wrong place — fix the fence, never the fixture. **Never re-capture a `pre-*` directory** (`manager_test.go:6763–6786`: "they are the record of what already ran").

- [ ] **Step 8: Record the NEW fixtures.** Add the new scenarios to `replayScenariosPostB1`'s successor list (a new `replayScenariosPostStage3()` returning scenarios with `dir: "post-stage3"`), then capture:
  ```bash
  cd server && CONSTRUCT_HISTORY_CAPTURE=1 CONSTRUCT_HISTORY_CAPTURE_DIR=post-stage3 GOWORK=off \
    go test ./internal/manager/construction/ -run '^TestCaptureConstructHistories$' -count=1
  ```
  It needs the `temporal` CLI on `PATH` (it starts an offline dev server via `testsuite.StartDevServer`'s `ExistingPath`). `Test_Replay_PreB1Histories_StayDeterministic` globs `testdata/replay/*/*.json` and FAILS on any file with no matching scenario, so a captured file with no scenario is caught immediately.
  - [ ] **Verify first:** `temporal` on PATH (`command -v temporal`). If absent, STOP and report — do not hand-write a history JSON.

- [ ] **Step 9: Update the `registered_names_test.go` golden, deliberately.** Task 3's contract adds ~12 activities named `"activityExecutionAccess.<op>"` and retires the `"constructionTransitionAccess.*"` / `"gitActivityStatusAccess.*"` names it replaced. There is no `-update` flag and no separate golden file: run the test, read the printed got/want diff (`diffStringSlices`, L374), and hand-edit the `registeredTemporalNamesGolden` literal (L69–265) to match. `TestRegisteredTemporalNamesGolden_FrozenWorkflowNames` (L319) asserts the 20 externally-referenced WORKFLOW names are still present — none of them moves here, and if one does, STOP.

- [ ] **Step 10: Gates and commit.** `make test-short`, `make lint`, every `gen-*-check`. ONE commit: the code, the fixtures and the golden together.

---

### Task 6: The design rails dual-write rounds beside `ReviewThread`

Spec §5.3 disposition: "`ArtifactSlot.ReviewThread`: read-through to `ReviewRound` in stage 3 (**dual-write during the wave**), deleted in stage 6." The design rails already have everything a round needs — a round number, a roster (the PM critic and the architect self-review), a verdict, a thread — it is simply scattered across `ArtifactSlot.ReviewThread`, `CritiqueVerdict`/`CritiqueNotes` and the decision signal.

**Files:**
- Modify: `server/internal/manager/systemdesign/coauthorartifact.go` — `handleReviewDecision` L1095–1255 (`case ReviewReject:` L1137–1216; `case ReviewApprove:` L1126–1135), `commitOnApprove` L1261–1369, `runCritiqueRound` L876, `loadReviewThread` L3519.
- Modify: `server/internal/manager/projectdesign/coauthorphase2artifact.go` — `coAuthorApplyDecision` L893–1039 (`case ReviewReject:` L931–1005; `case ReviewApprove:` L922–929), `coAuthorApprove` L1040–1155, `loadReviewThread` L2340.
- Modify: `server/internal/manager/{systemdesign,projectdesign}/manager_test.go`.
- Modify: `.aiarch/state/project.json` — delete `CritiqueVerdict`/`CritiqueNotes` from the `ArtifactSlot` `$def`.

**Interfaces:**
- Consumes: `DesignSessionAccess.RejectArtifactOnBranchWithComments(rc, projectID, expectedVersion, branch, kind, notes, round int64, comments []ReviewComment, replies []ReviewReply, idempotencyKey)` (the existing writer, KEPT for the dual-write), and Task 3's `ActivityExecutionAccess.OpenReviewRound` / `AppendReviewVerdict` / `DecideReviewRound`.
- Produces: no new Go surface. Each design artifact kind maps to an activity id and a review task id — the requirements/architecture activities stage 2 appended (`ActivityTypeRequirements` = 7, `ActivityTypeArchitecture` = 8, `ActivityTypeProjectDesign` = 9) — so a design review round is keyed exactly like a construction one.
  - [ ] **Verify first:** the `ArtifactKind` → `(activityID, reviewTaskID)` mapping. Stage 2 Task 9/10 put the design prefix into the derived plan; read slot 9 for the activity ids it emitted (`jq -r '.slots["9"].model.activities[] | select(.name|test("^A-|^1 |requirements|architecture";"i")) | .name'`) and the method-assets `lifecycles.json` for each type's review task ids. If the prefix activities are not yet in slot 9 for THIS project, the mapping is still correct for a fresh project and this task's tests use a fixture — say so in the test's comment and earmark the backfill for Task 9.

**Dual-write, not cut-over, and the reason must be in the code:** the two co-author workflows are documented near-twins ("Edit all three together" — `coauthorartifact.go:948–952`), the SPA reads `ReviewThread` through `GetSessionState` → `committedSessionView` → `reviewThreadToView` in BOTH design Managers, and stage 5 has not yet re-pointed the SPA. Deleting `ReviewThread` here would blank the design review UI for the length of the wave.

- [ ] **Step 1: Write the failing test** (systemdesign; then the byte-identical twin in projectdesign):
  ```go
  // A design reject must now leave a ReviewRound as well as a ReviewThread — the same
  // round number, the same comments, plus the roster and the verdict the thread never
  // carried. The thread stays until stage 6 because the SPA still reads it.
  func Test_CoAuthor_Reject_DualWritesTheRoundAndTheThread(t *testing.T) {
  	// … drive the rail to a reject with two anchored comments …
  	slot := ps.slot(projectstate.KindGlossary)
  	exec := ps.execution("A-requirements")
  	if len(slot.ReviewThread) != 2 {
  		t.Fatalf("the legacy thread must still be written; got %d", len(slot.ReviewThread))
  	}
  	r := exec.Reviews[len(exec.Reviews)-1]
  	if len(r.Thread) != len(slot.ReviewThread) {
  		t.Fatalf("the round's thread and the slot's thread are the same comments; %d vs %d", len(r.Thread), len(slot.ReviewThread))
  	}
  	if int64(r.Round) != slot.ReviewThread[0].Round {
  		t.Fatalf("one round number, two places: round=%d comment.round=%d", r.Round, slot.ReviewThread[0].Round)
  	}
  	if len(r.Verdicts) != 1 || r.Verdicts[0].Verdict != projectstate.VerdictSendBack {
  		t.Fatalf("the verdict is a row now, not an absence; got %+v", r.Verdicts)
  	}
  }
  ```

- [ ] **Step 2: Fence both rails.** A new named const per rail, following `"design-vibes-autogate"` (both files already carry that exact id at `coauthorartifact.go:408` and `coauthorphase2artifact.go:466`, which is the precedent for one id used by both twins): `const changeDesignRoundLedger = "design-round-ledger"`. Same rule as Task 5 — ONE id per rail covering all of its write points.

- [ ] **Step 3: Open the round where the roster is computed.** `runCritiqueRound` (L876) and the gate both know the reviewer set; `OpenReviewRound` is called once per review occurrence with `SubjectRef{Kind: SubjectCommit, Ref: <session branch head sha>}`.

- [ ] **Step 4: `CritiqueVerdict`/`CritiqueNotes` become an ordinary verdict.** Spec §5.3: "deleted in stage 3 — an ordinary verdict with `role: projectManager`". `critiqueViewFor` / `critique.Validate` (`coauthorartifact.go:3695`/`3720`) now append `ReviewVerdict{ReviewerRole: "projectManager", Verdict: …, Summary: <the notes>}` to the open round, and the two `ArtifactSlot` fields are deleted from the `$def` and the Go struct. Every reader of them (`grep -rn 'CritiqueVerdict\|CritiqueNotes' server/ webApp/src`) is re-pointed at the round's verdicts.

- [ ] **Step 5: `ArtifactSlot.Revisions` gets its drift test.** Spec §5.3: "kept, deprecated in place, stamped at commit, with a drift test equal to the derived round count." `commitTransition` (`projectstateaccess.go:6715`) already does `slot.Revisions++`; add the test that `slot.Revisions == len(exec.Reviews filtered to this artifact's review task)` for every sealed slot in the committed state, and mark the field deprecated in place naming stage 6.

- [ ] **Step 6: Replay.** Both design rails have their own fixtures or none — check `server/internal/manager/{systemdesign,projectdesign}/testdata/`. If they have none, the fence is still required (in-flight design sessions exist in production; the drain note in Task 10 covers them) and the Temporal test-suite cases are the only regression net. Say which is true in the commit body.

- [ ] **Step 7: Gates and commit.** Self-amendment loop (the `ArtifactSlot` `$def` changed), `make test-short`, `make lint`, `cd webApp && npm run check`. Commit `feat(design): design reviews write ReviewRounds beside the legacy thread`.


---

### Task 7: `QueryActivityView` reads the PERSISTED rounds

Stage 0's `QueryActivityView` doc already says what happens here: "Everything is DERIVED on read (`normalizeAttempts`, `deriveTaskViews`); **stage 3 stores revisions and this becomes a projection**." After Tasks 5 and 6 the rounds are real, so the reconstruction must stop competing with them.

**The rule: reconstruction is for rows that predate the ledger, and provenance says which those are.** A row whose `Reviews` is non-empty is a row a real run wrote; N4 does not touch its gate tasks at all. A row with no `Reviews` is pre-ledger and reconstructs exactly as today (Task 1's dedup keeps the two consistent at the boundary — a row with SOME rounds and SOME older notes). Every reconstructed round is stamped `Provenance.Origin = backfilled`, which is already how `TaskRevisionProvenance` reaches the wire (`synthesized|backfilled|observed`, worst-origin over the revision's members) — so the SPA can say "reconstructed" without a new field.

**Files:**
- Modify: `server/internal/manager/construction/constructionmanager.go` — `QueryActivityView` L778–830, `normalizeAttempts` L2690, `appendGateAttempts` L2798, `deriveTaskViews` L2846, `phaseRevisions` L2906, `reviewRevision` L3027, `activityViewFrom` L3175.
- Modify: `server/internal/manager/construction/manager_test.go`.

**Interfaces:**
- Consumes: `projectstate.ActivityExecution{Attempts, Reviews}` (Task 4), `projectstate.ReviewRound{RoundID, TaskID, Reviews, Round, SubjectRef, Reviewers, Verdicts, Thread, Outcome, DecidedAt, DecidedBy, Provenance}`.
- Produces (unexported, package `construction`):
  ```go
  // roundRevisions turns the PERSISTED rounds for one review task into revisions — the
  // join is RoundID ↔ AttemptID, both "<activityId>:<taskId>:<n>", so revision n of the
  // review task is round n and attempt n of the same task, with no ordering heuristic.
  func roundRevisions(rounds []projectstate.ReviewRound, taskID string) []taskRevision

  // reconstructedReviewRevisions is stage 0's R3/R4 path, kept for rows that predate the
  // ledger. A row with ANY persisted round for a task never reaches it.
  func reconstructedReviewRevisions(attempts []projectstate.TaskAttempt, notes []projectstate.OperatorNote, taskID string) []taskRevision
  ```
  and a changed `deriveTaskViews(lc, attempts, notes, rounds []projectstate.ReviewRound, liveGate string) []taskView`.

- [ ] **Step 1: Write the failing tests.**
  ```go
  // The persisted round IS the revision. R4's tails-aligned note matching was a
  // reconstruction for a world with no rounds; where a round exists, an ordering
  // heuristic must not get a vote.
  func TestDeriveTaskViews_PersistedRoundsWin(t *testing.T) {
  	lc := avServiceLifecycle()
  	rounds := []projectstate.ReviewRound{
  		{RoundID: "C-X:designReview:1", TaskID: "designReview", Reviews: "detailedDesign", Round: 1,
  			Outcome: projectstate.RoundSentBack,
  			Verdicts: []projectstate.ReviewVerdict{{ReviewerRole: "architect", Verdict: projectstate.VerdictSendBack, Summary: "too wide"}},
  			Thread:   []projectstate.ReviewComment{{ID: "c1", Text: "split this op", Round: 1, Status: "open"}},
  			Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginObserved}},
  		{RoundID: "C-X:designReview:2", TaskID: "designReview", Reviews: "detailedDesign", Round: 2,
  			Outcome: projectstate.RoundPassed, Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginObserved}},
  	}
  	// A misleading note: recorded, but the rounds are the record now.
  	notes := []projectstate.OperatorNote{avSendBack("detailed_design", "stale note")}
  	views := deriveTaskViews(lc, avSRSPassed, notes, rounds, "")
  	got := avOutcomes(avTask(t, views, "designReview"))
  	if !slices.Equal(got, []string{"sentBack", "passed"}) {
  		t.Fatalf("the persisted rounds are the revisions; got %v", got)
  	}
  	if p := avTask(t, views, "designReview").Revisions[0].Provenance; p != projectstate.OriginObserved {
  		t.Fatalf("a round a run wrote is observed, not backfilled; got %q", p)
  	}
  }

  // A row that predates the ledger still reconstructs, and says so.
  func TestDeriveTaskViews_PreLedgerRowStillReconstructs(t *testing.T) {
  	lc := avServiceLifecycle()
  	notes := []projectstate.OperatorNote{avSendBack("detailed_design", "tighten it")}
  	views := deriveTaskViews(lc, append(slices.Clone(avSRSPassed), avObserved(projectstate.TaskDesignReview, 1, projectstate.OutcomeRejected)), notes, nil, "")
  	rev := avTask(t, views, "designReview").Revisions[0]
  	if rev.Outcome != "sentBack" || rev.Note != "tighten it" {
  		t.Fatalf("reconstruction must survive for pre-ledger rows; got %+v", rev)
  	}
  	if rev.Provenance != projectstate.OriginBackfilled {
  		t.Fatalf("a reconstructed revision is backfilled; got %q", rev.Provenance)
  	}
  }
  ```

- [ ] **Step 2: Implement the split.** `deriveTaskViews` takes `rounds`; per review task, if any round has that `TaskID`, use `roundRevisions`, else `reconstructedReviewRevisions`. `normalizeAttempts` stops appending gate attempts for a task that has rounds (Task 1's `ledgerRejections` generalizes to "rounds beat notes beat nothing").

- [ ] **Step 3: Carry the round's facts onto the revision.** `taskRevision` gains `Verdicts`, `Thread`, `SubjectRef`, `Round`, `DecidedBy` — Task 8 puts them on the wire.

- [ ] **Step 4: Gates and commit.** `GOWORK=off go test ./internal/manager/construction/ -count=1`, `make test-short`, `make lint`. Commit `feat(construction): the Activity Experience reads persisted review rounds`.

---

### Task 8: `ActivityView` carries verdicts, thread, subject and round

`constructionManager.QueryActivityView` IS wire-exposed (stage-0 F6: `cmd/clientgen/main.go:62` `exposedManagers`), so this change fires every exposed-Manager obligation.

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.constructionManager["$defs"].TaskRevisionView.properties` (four additive properties) and four new `$defs` (`ReviewVerdictView`, `ReviewSubjectRef`, `ReviewThreadComment`, `ReviewVerdictKind`). Today's `$defs` set is 35 keys; it becomes 39.
- Regenerate: `server/internal/manager/construction/{contract.gen.go,fake/fake.gen.go}`, `server/api/openapi.yaml`, `server/internal/client/{web,mcp}/**`, `../systemtests/internal/sdk/**`, `webApp/src/contracts/{schema.ts,enums.gen.ts}`, `webApp/src/api/ops.gen.ts`.
- Modify: `server/cmd/clientgen/mcpdocs.go` — `mcpOpDocs["ConstructionManager"]["QueryActivityView"]` (the doc must describe what the op now returns, or `mcpemit.Generate` errors).
- Modify: `webApp/scripts/gen-enums.mjs` — `OUTPUT_NAMES` gains an entry for the new `ReviewVerdictKind` enum, or `npm run gen:api` throws.
- Modify: `server/cmd/server/managerlog.go` — only if the `ConstructionManager` interface signature changes (it does not here; the RESULT type changes, the method does not). Confirm by building.
- Modify: `webApp/preview/fixtures/web-client/activity-experience/{not-started,deployment-linear,done,service-fork-sent-back}.json`.
- Modify: `webApp/src/hooks/useActivityView.ts` only if a derived type needs widening (`ActivityView` is `OpResult<'constructionQueryActivityView'>`, structural — usually no edit).

**Interfaces — the additive `$defs` (author them in the surrounding register; `required` is PRESENCE-only):**
```json
"ReviewVerdictKind": {
  "type": "string",
  "enum": ["approve", "sendBack", "abstain"],
  "x-enum-varnames": ["VerdictApprove", "VerdictSendBack", "VerdictAbstain"],
  "x-go-base": "string",
  "description": "One reviewer's answer in a round. Agent and human verdicts are the same kind of row; abstain is a reviewer who was asked and declined, which is not silence."
},
"ReviewVerdictView": {
  "type": "object",
  "properties": {
    "reviewerRole": { "type": "string" },
    "actor": { "type": "string", "description": "The agent or person who gave it. Omitted when the role alone identifies the reviewer." },
    "verdict": { "$ref": "#/$defs/ReviewVerdictKind" },
    "summary": { "type": "string", "description": "The reviewer's one-line reason, verbatim." },
    "at": { "type": "string", "format": "date-time", "x-go-import": "time", "x-go-type": "time.Time" },
    "attemptId": { "type": "string", "x-go-name": "AttemptID", "description": "The gate attempt this verdict was given at." }
  },
  "required": ["reviewerRole", "verdict", "at"],
  "additionalProperties": false
},
"ReviewSubjectRef": {
  "type": "object",
  "properties": {
    "kind": { "type": "string", "enum": ["commit", "slot", "none"], "x-enum-varnames": ["SubjectCommit", "SubjectSlot", "SubjectNone"], "x-go-base": "string" },
    "ref": { "type": "string", "description": "What the round judged: the staged commit sha, or the artifact slot's wire name. The artifact AS OF a revision is a git read of this ref — no second copy is stored." }
  },
  "required": ["kind", "ref"],
  "additionalProperties": false
},
"ReviewThreadComment": {
  "type": "object",
  "properties": {
    "id": { "type": "string", "x-go-name": "ID" },
    "anchor": { "type": "string" },
    "anchorText": { "type": "string" },
    "text": { "type": "string" },
    "authorRole": { "type": "string" },
    "round": { "type": "integer" },
    "status": { "type": "string", "enum": ["open", "answered", "resolved"], "x-enum-varnames": ["ThreadCommentOpen", "ThreadCommentAnswered", "ThreadCommentResolved"], "x-go-base": "string" },
    "replies": { "type": "array", "items": { "$ref": "#/$defs/ReviewThreadReply" } },
    "reopened": { "type": "boolean" },
    "type": { "type": "string", "enum": ["changeRequest", "question", "staleAck"], "x-enum-varnames": ["ThreadChangeRequest", "ThreadQuestion", "ThreadStaleAck"], "x-go-base": "string" },
    "addressee": { "type": "string" }
  },
  "required": ["id", "anchor", "text", "authorRole", "round", "status", "replies", "reopened", "type"],
  "additionalProperties": false
}
```
and on `TaskRevisionView`, directly after `"comments"`:
```json
"verdicts": { "type": "array", "items": { "$ref": "#/$defs/ReviewVerdictView" }, "description": "Every reviewer's answer in this round, agent and human alike. Empty on a dispatch revision and on a revision reconstructed from a pre-ledger row." },
"thread": { "type": "array", "items": { "$ref": "#/$defs/ReviewThreadComment" }, "description": "The round's comment thread with its replies and resolutions — the same comments the design rails' ArtifactSlot.reviewThread carries, which is a read-through to this until stage 6." },
"subjectRef": { "$ref": "#/$defs/ReviewSubjectRef", "description": "What this revision judged. The artifact as of a non-latest revision is read from it." },
"round": { "type": "integer", "description": "The stored round number. Equal to n for a persisted round; derived for a reconstructed one, which is why both are carried." }
```
The existing `comments` (`TaskRevisionComment{jsonPath,text}`) and `note` stay: they are what a RECONSTRUCTED revision can offer, and deleting them would blank every pre-ledger row.

- [ ] **Step 1: Amend the contract, regenerate, and read what came out.**
  ```bash
  cd server && GOWORK=off make gen-models gen-client gen-sdk
  cd ../webApp && npm run gen:api && npm run gen:ops
  ```
  - [ ] **Verify first:** open `server/internal/manager/construction/contract.gen.go` and confirm the generated names (`TaskRevisionView.Verdicts []ReviewVerdictView`, `.Thread []ReviewThreadComment`, `.SubjectRef ReviewSubjectRef`, `.Round int64`). Use what modelgen emitted, not what this plan guessed.
  - [ ] **Verify first:** `ReviewThreadReply` is referenced above but not authored — either add it as a `$def` (mirroring `ReviewCommentReply{id, authorRole, text, at}`) or reuse an existing one if the constructionManager `$defs` already carries a reply shape. Check with `jq '.serviceContracts.constructionManager["$defs"] | keys'` before writing.

- [ ] **Step 2: Fill them in `activityViewFrom`.** The revision-level fields come straight off `taskRevision` (Task 7 Step 3). A dispatch revision carries none of the four.

- [ ] **Step 3: `mcpdocs` + `OUTPUT_NAMES`.** Add/expand `mcpOpDocs["ConstructionManager"]["QueryActivityView"]`; `mcpemit.Generate` errors with "no documentation for operation …" if it is empty. Add `ReviewVerdictKind` (and any other new enum the OAS exposes) to `webApp/scripts/gen-enums.mjs`'s `OUTPUT_NAMES`, classified in `NON_MECHANICAL`/`UNVERIFIED_MECHANICAL` as the script's own error message directs.

- [ ] **Step 4: Update the four fixtures.** `service-fork-sent-back.json` is the one that must actually exercise the new fields — its sent-back revision gains a `verdicts[]`, a `thread[]` with one reply and one resolved comment, a `subjectRef` and a `round`. `not-started.json` and `deployment-linear.json` change only where the schema requires it. Then:
  ```bash
  cd webApp && npm test && npm run check
  ```
  `scripts/fixture-schema.test.mjs` validates both `webApp/preview/fixtures` and `uitests/preview-fixtures` against `server/api/openapi.yaml`, and `vite.preview.config.ts` fails the preview build on any invalid fixture — so a fixture written before Step 1's regen cannot pass, and that ordering is the point.

- [ ] **Step 5: Gates and commit.** Every `gen-*-check`, `make test-short`, `npm run check`. Commit `feat(construction): ActivityView carries verdicts, thread, subject and round`.

---

### Task 9: `cmd/migrate-activity-execution` — and run it on this repo's state

Spec §5.3 F. One-shot, modelled exactly on `cmd/backfill-attempts`: read `.aiarch/state/project.json` raw, decode through the production codec, edit in memory, re-encode, **splice only the members it owns back into the original bytes**, write the file, and let a human review the diff. It does NOT go through the RA's commit path.

**Files:**
- Add: `server/cmd/migrate-activity-execution/{main.go,main_test.go}`.
- Modify: `server/internal/repostructure_test.go` — `allowedCmd` (L42–55) gains `"migrate-activity-execution": true,`. That is the entire mechanical requirement; `TestRepoStructureCmdIsClosed` (L110) enforces it.
- Modify: `/Users/davidmarne/mixofrealitystudio/archistrator/.aiarch/state/project.json` — `.activityConstruction` → `.activityExecution`, rewritten (tool-written, never hand-edited).

**Interfaces:**
- Consumes: `projectstate.DecodeProjectJSON` / `EncodeProjectJSON`, `projectstate.CodecCarriesEveryMember(stored, encoded json.RawMessage) ([]string, error)` (`projectstateaccess.go:9186`), `projectstate.ResolvePhaseCompletions`, `projectstate.ProfileFor`, `projectstate.ActivityExecution`, `ArtifactSlot.{ReviewThread,CritiqueVerdict,CritiqueNotes,Revisions}`.
- Produces: `.activityExecution` and a report on stdout naming every row, every backfilled round and every dropped derived field.

**The transformation, verbatim from §5.3 F:**
1. Rename the map. Carry `ActivityID`, `Type`, `Variant`, `StartedAt`, `CompletedAt`, `FailureReason`, `FailureDetail`, `Produced`, `Attempts`, `OperatorNotes` **verbatim**, each attempt's `Provenance.Origin` preserved (backfilled stays backfilled — the migration is not evidence of anything and must not launder provenance).
2. Drop `Phase`, `Phases`, `CurrentPhase`, `Kind`, `BuildStatus`.
3. Stamp `Version = 1` and the current `LifecyclePin` for the row's classified type.
4. For each SEALED artifact slot: one **backfilled** `ReviewRound` per distinct `ReviewComment.round`, `Thread` = that round's comments verbatim, `SubjectRef` = the commit sha if resolvable from `slot.Provenance`/git else `{kind: "none", ref: ""}`, `Outcome` = `sentBack` for every round but the last, `passed` for the last iff the slot is committed.
5. `CritiqueVerdict`/`CritiqueNotes` → one backfilled `ReviewVerdict{ReviewerRole: "projectManager", Summary: CritiqueNotes}` on the round matching the critique's round (the last, if none matches).
6. `OperatorNote{Kind: sendBack}` → a round whose single verdict carries the note's `Comments`, unless step 4 already produced a round with that number for the same task.
7. Every synthesized round is stamped `Provenance{Origin: backfilled, Generator: "cmd/migrate-activity-execution", Basis: "<what it was made from>"}`.

**Acceptance (spec §5.3 F):** the DERIVED `Phases` after migration equal the pre-migration STORED `Phases`, for every row. That is the test, not a spot check.

- [ ] **Step 1: Write the acceptance test first** — `server/cmd/migrate-activity-execution/main_test.go`:
  ```go
  // The migration's one acceptance criterion (spec §5.3 F): what the document USED to say
  // about phase completion and what the new shape DERIVES must be the same answer for
  // every row. A migration that changes an activity's history is not a migration.
  func TestMigrate_DerivedPhasesEqualThePreMigrationStoredPhases(t *testing.T) {
  	before := mustLoadFixture(t, "testdata/pre-migration-project.json")
  	after := mustMigrate(t, before)
  	for id, old := range before.ActivityConstruction {
  		row, ok := after.ActivityExecution[id]
  		if !ok {
  			t.Fatalf("row %s vanished", id)
  		}
  		item := committedItem(before, id)
  		typ, variant, _, _ := projectstate.ResolveConstructionRow(legacyRow(old), item)
  		got := projectstate.ResolvePhaseCompletions(projectstate.ProfileFor(typ, variant), nil, row.Attempts)
  		if !samePhaseCompletion(got, old.Phases) {
  			t.Fatalf("row %s: derived phases differ from the stored ones\n derived %+v\n stored  %+v", id, got, old.Phases)
  		}
  	}
  }

  // Provenance is not laundered: a backfilled attempt stays backfilled.
  func TestMigrate_PreservesAttemptProvenanceOrigin(t *testing.T) { /* … */ }

  // The splice touches only the members it owns.
  func TestMigrate_OnlyTheExecutionAndSlotMembersMoved(t *testing.T) { /* mirror backfill-attempts' confirmOnlyConstructionMoved */ }
  ```
  - [ ] **Verify first:** `ResolvePhaseCompletions`' signature after Task 4 (it loses the `stored` argument, or takes `nil`). Use the real one. Build the `testdata/pre-migration-project.json` fixture by copying today's real `.activityConstruction` (`jq '{activityConstruction, slots}' .aiarch/state/project.json`) — a fixture built from the real data is what makes the acceptance test mean something.

- [ ] **Step 2: Write the cmd**, copying `cmd/backfill-attempts/main.go`'s splice discipline exactly: `roundTripsExactly` (the codec must round-trip the ORIGINAL member byte-for-byte before it is replaced), `onlyConstructionEdited`, `confirmOnlyConstructionMoved`, and `CodecCarriesEveryMember` over every member it rewrites. A migration that cannot prove it lost nothing does not run.

- [ ] **Step 3: Allowlist the cmd.** One line in `repostructure_test.go`'s `allowedCmd`. Run `GOWORK=off go test ./internal/ -run TestRepoStructureCmdIsClosed -count=1`.

- [ ] **Step 4: Dry-run against the real state, and READ the report.**
  ```bash
  cd server && GOWORK=off go run ./cmd/migrate-activity-execution --root .. --dry-run | tee /tmp/migrate-report.txt
  jq '.activityConstruction | keys | length' ../.aiarch/state/project.json
  jq '[.activityConstruction[] | select((.attempts|length) > 0)] | length' ../.aiarch/state/project.json
  ```
  The report's row count must equal the first number, and the rows it says carry attempts must equal the second. If they do not, the migration is not seeing what is there — STOP.

- [ ] **Step 5: Run it for real, then re-run the whole gate suite against the migrated state.**
  ```bash
  cd server && GOWORK=off go run ./cmd/migrate-activity-execution --root ..
  git --no-pager diff --stat -- ../.aiarch/state/project.json
  GOWORK=off make method-check && GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off make test-short && GOWORK=off make derived-plan-write && git status --short -- ../.aiarch/state/project.json
  ```
  `derived-plan-write` must be a NO-OP: slots 9/10 do not move because an execution row is not a plan row.

- [ ] **Step 6: Read the diff by hand before committing.** It is the project's own history; a reviewer who has not read it has not reviewed it. Then ONE commit — the cmd, its test, the allowlist line and the migrated state together: `chore(state): migrate activityConstruction to activityExecution`.

---

### Task 10: Docs, earmarks, and the drain note

**Files:**
- Modify: `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — §5.3 and §8.
- Add: `docs/bugs/2026-09-24-stage3-rail-earmarks.md`.

- [ ] **Step 1: Amend the spec where planning proved it wrong.** Three corrections, each with its evidence:
  - §5.3 "ResourceAccess" bullet: the three folded things are **contract facets of the one `project-state-access` component**, not components. Record the founder's Task 3 Step 0 ruling ((A) fifth facet / (B) new component) and what it implied.
  - §5.3 "`projectStateAccess` … sheds its 12 dead ops (D1–D4)": it has **9** ops and none is uncalled. The "12 dead ops" is the 2026-07-20 QA finding about duplicate call-chain EDGES in the committed dynamic views (`docs/superpowers/qa/2026-07-20-qa-findings-backlog.md:392-394`), pruned by Task 3 Step 5.
  - §5.3 "Concurrency": the `{projectId}:statewrite` lane is **not built in stage 3**. `applyMutationOnBranchFiles`' git ref-CAS + the idempotency dedup ledger + the workflow's `applyRecovering` loop already serialize correctly; a second durable per-project singleton is the shape `pump-singular-per-project` had to clean up. Move the lane to stage 4, where the parallel pump raises real contention.
  - §8 stage-6 row: the migration cmd ran in stage 3 (§5.3 F already said stage 3 in one place and stage 6 in another — reconcile to stage 3, which is where the shape changed).

- [ ] **Step 2: Write the earmark file**, in the register of `docs/bugs/2026-09-23-stage1-model-wave-earmarks.md`:
  - **Deferred to stage 4:** `{projectId}:statewrite` serialized merge/commit lane; the disposition of the reduced `designSessionAccess` (3 ops) and `gitActivityStatusAccess` (3 ops) — both are candidates to fold into `deliveryManager`'s wave; the `ReadProjectOnBranch` whole-aggregate read the pump still makes.
  - **Deferred to stage 6:** delete `ArtifactSlot.ReviewThread` (dual-written here), delete `ArtifactSlot.Revisions` (deprecated in place with a drift test here), delete the legacy `activityConstruction` decode shadow, delete the computed view fields' stored counterparts.
  - **Owed waiver (slot 3), carried forward from stage 1 and still owed:** the §2h sentence naming the three Managers' TRANSITIONAL `Project Delivery Workflow` facet group and its stage-4 expiry; a Glossary entry for `Design Conformance Rules`.
  - **Platform (method-assets) — founder STOP:** `.claude/` skills naming `constructionTransitionAccess` / `gitActivityStatusAccess` / `designSessionAccess` (`grep -rln` them across `.claude/skills .claude/commands .claude/agents`).
  - **Post-merge on main only:** `cd uitests && npm run regen:core-use-cases-fixture` (the regen reads the committed `main` branch, `cmd/gen-uitests-fixtures/main.go:96`, so it cannot run in a worktree).
  - **Model hygiene carried from stage 1:** six `uitests/preview-fixtures/**` snapshots still embed the 19-volatility world; `designhealthengine.go:22` and `systemdesignmanager.go:132` name a retired volatility.

- [ ] **Step 3: Write the release + DRAIN note into the earmark file. Do not deploy.** Before this stage may be deployed, in this order: (1) pause every project (`SetProjectRunState`); (2) drain `{projectId}:nextActivity` pumps and every `constructionConstructActivity` child — the child's command sequence changed and its state shape changed, and the `GetVersion` fences keep an in-flight execution on the OLD path, which reads a map that Task 9 renamed; (3) drain the two design-rail co-author workflows for the same reason; (4) run `cmd/migrate-activity-execution` against the PRODUCTION project repos (this plan ran it only against this repo's own state); (5) release and deploy; (6) resume. The founder runs this.

- [ ] **Step 4: Commit** `docs: stage-3 spec corrections and earmarks`.

---

## Self-review

**Spec §8 stage-3 entry criteria — both are blocking work items, both are first, and each has a test that reproduces the corruption against a persisted rejection.**

| Criterion | Task | Reproduction |
|---|---|---|
| (a) N4 appends a rejected gate attempt per send-back note with no dedup against ledger rejections → revisions double-count, `passed` flips to `sentBack` | **Task 1** | `TestNormalizeAttempts_APersistedRejectionIsNotReconstructedTwice` (3 gate attempts where 2 are real; last reads `rejected` on a passed gate) + the tails-aligned and pre-ledger companions |
| (b) phase completion re-derived from gate state while `ResolveConstructionRow`'s resolved completions are discarded | **Task 2** | `TestNormalizeAttempts_GateCompletionComesFromTheResolvedSetOnly` + `TestActivityViewFrom_PhaseCompletionIsTheResolvedSet`. Planning found the discard is at `constructionmanager.go:797` AND the re-derivation happens TWICE (`appendGateAttempts` from raw `row.Phases`, `activityViewFrom` from `states[ph.Gate]`) — three answers collapsed to one |

**Spec §5.3 bullet → task:**

| §5.3 | Task |
|---|---|
| `.activityExecution[activityId] = ActivityExecution{… Attempts, Reviews, Version}` | 4 |
| `TaskAttempt{AttemptID, TaskID, Revision, Attempt, Actor, StartedAt, EndedAt, Outcome, StagedRef, Evidence, Provenance}` | 4 (kept; `StagedRef` added to the existing type) |
| `ReviewRound{RoundID, TaskID, Reviews, Round, SubjectRef, Reviewers, Verdicts, Thread, Outcome, DecidedAt, DecidedBy, Provenance}` | 4 (type), 5 (construction writes), 6 (design writes) |
| Agent and human verdicts are rows in `Verdicts`; Ask = `ReviewComment.type = question` — no third mechanism | 5 Step 5, 6 Step 4 |
| Artifact-as-of-revision = `SubjectRef.ref` (a git read, no new storage) | 4 (type), 8 (`subjectRef` on the wire) |
| Only in-place mutations are comment `status`/`replies` (`ApplyReviewBatch`) | 3 (verb `SetReviewCommentStatus` reuses the existing normalizer) |
| Derived, never stored: revisions, task state, `Phases`/`PhaseCompletion`, `CurrentPhase`, `BuildStatus`, coarse `Phase`, earned value | 4 Step 2 (stop storing), 2 (ONE derivation rule), 7 (revisions derive from rounds) |
| Key rename `.activityConstruction` → `.activityExecution`, ONE wire break, ActivityID + AttemptID format kept | 4 |
| `ArtifactSlot.ReviewThread` → read-through, dual-write this wave, delete in 6 | 6 (dual-write), 10 (stage-6 earmark) |
| `CritiqueVerdict`/`CritiqueNotes` deleted → an ordinary `projectManager` verdict | 6 Step 4 |
| `ArtifactSlot.Revisions` kept, deprecated in place, drift test = derived round count | 6 Step 5 |
| `NoteSendBack` stops being written; pending feedback = latest round's open comments; `OperatorNotes` narrowed | 5 Step 6 |
| `Phases`/`CurrentPhase`/`BuildStatus`/`Phase`/`Kind` emitted as computed view fields through stage 5 | 4 (stop storing), 8 (the view) |
| Twelve verbs on `activityExecutionAccess`; `sourceControlAccess`/`episodeAccess` stay separate | 3 |
| `projectStateAccess` keeps slots/plan/policy; sheds its "12 dead ops" | 3 (verified: 9 ops, none dead; the 12 are duplicate VIEW EDGES — pruned in Step 5, spec amended in Task 10) |
| View derivation lives in an Engine, not the RA | **NOT DONE in stage 3, deliberately.** Stage-0 F3 proved no production Engine may import `projectstate` and `TestGeneratedOnlyPublic` forbids the extra exported surface, so the derivation stays in the Manager beside its one caller (stage-0 F3's own resolution). Earmarked in Task 10 for stage 4, where `deliveryManager` moves. |
| Per-activity `Version`, CAS, retry; narrow `func(*Project) error` transitions; one project-scoped serialized commit lane | 4 Step 3 (`Version` + `withActivityVersion`); Task 3's concurrency ruling defers `{projectId}:statewrite` to stage 4 with evidence |
| Deterministic ids (AttemptID/RoundID/NoteID) make every append idempotent | 3 Step 1 (`TestOpenReviewRound_IsIdempotentUnderRetry`) |
| Migration F (rename, verbatim carry, drop derived, `Version=1` + `LifecyclePin`, thread→rounds, critique→verdict, notes→round; acceptance: derived `Phases` == stored `Phases`) | 9 |

**Spec §8 stage-3 row → task:** "task-level revisions/threads/verdicts in project state" → 4, 5, 6; "activity branch" → 3 (`StageTaskOutput` writes on `activity/{activityId}`); "stage/review/commit verb family" → 3; "construction send-back persistence" → 5; "`QueryActivityView` read" → 7, 8.

**Spec §9 testing → task:** rail (revision read returns artifact-as-of-`stagedRef`; construction send-back round-trips comments) → 5 Step 1, 7 Step 1, 8 Step 4. Replay per shape → 5 Steps 7–8. Review-engine table tests → already shipped in stage 2; the roster is now PERSISTED and asserted in 5 Step 1.

**Not in this plan, on purpose:** the generic DAG child workflow and the Manager collapse (stage 4); the Activity Experience UI (stage 5); deleting `ReviewThread` and the stored derived fields (stage 6); any deploy (Task 10 Step 3 writes the drain recipe and stops).

**Open founder decisions this plan STOPs on:** (i) land or abandon `lifecycle-2-conformance-gate` (Pre-requisite); (ii) Task 3 Step 0 — `activityExecutionAccess` as a fifth contract facet (recommended) or a new component.
