# Construction Per-Phase Review/Approval Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the construction per-phase loop a **top-level "approval to continue" gate** whose sub-workflow is selected by a **proactive checkpoint policy** — default-inert (today's behavior), architect-only (the existing +1), or full reviewer-set (via `reviewEngine.ProposeReviews`) — mirroring the system-design gate, and surface it in the webApp.

**Architecture:** One top-level gate per phase: a phase reaches `approved` (durable `PhaseCompletion.Completed`) before the loop advances. HOW it reaches approval is chosen by a `checkpointPolicy` snapshot: `gate none` → auto (inert); a gated phase → `RecordPhaseStarted` → `ProposeReviews` computes the reviewer set → `StageAwaitingApproval` suspend on a **phase-multiplexed** `SubmitPhaseDecision` signal → Approve stamps `RecordPhaseCompleted`; SendBack runs a **redraft-this-phase** loop (NOT the failure/variance path). The contract surface (stage enum, op, DTOs) is generated from `project.json .serviceContracts`. WebApp adds a construction GatePanel + a real checkpoint-policy editor.

**Tech Stack:** Go 1.25.4 + Temporal (`testsuite` for the gate), schema-first codegen (`make gen` from `project.json .serviceContracts`), React 19 + MUI 7 + openapi-fetch (webApp).

## Global Constraints

- **Codegen-first:** the construction `ConstructionStage` enum, manager ops, and DTOs are GENERATED. To add `StageAwaitingApproval` / `SubmitPhaseDecision` / new DTOs: edit `/Users/davidmarne/mixofrealitystudio/archistrator/.aiarch/state/project.json` under `.serviceContracts` (the construction contract), then `cd server && make gen` (= `gen-models` + `gen-client`, regenerates every `contract.gen.go`, the web/mcp handlers, and `server/api/openapi.yaml`). NEVER hand-edit a `*.gen.go` or `openapi.yaml`. Use the system-design `SubmitReviewDecision` contract entry as the copy template.
- **Default inert:** `checkpointPolicy`'s zero value gates NOTHING → the phase loop behaves exactly as today. "Approve every step" = gate all phases; "pure vibes" = gate none. Prove inertness with a test.
- **SendBack ≠ variance.** SendBack is a human decision that redrafts THIS phase (mirror system-design reject→redraft), with a human-paced counter. Do NOT route it through `handleVariance`/`overrideCh` (that re-decides via the intervention engine, retries the whole activity from phase 0, and burns the failure budget).
- **Phase-multiplexed signal:** the gate is one step inside the per-*activity* workflow (`{projectId}:{activityId}`) that loops many phases, so `SubmitPhaseDecision` MUST carry which phase it answers; the workflow rejects a signal whose phase ≠ the suspended phase.
- **Wire `RecordPhaseStarted` + `RecordPhaseCompleted` as a pair** (both dormant today). Even the inert/vibes path records start+completion so `CoarsePhase`/`CoarseBuildStatus` derive correctly.
- **Don't stack two human gates:** the new gate IS the architect +1 for the review-bearing phase; `recordChangeReviewed` becomes the gate's durable record (the existing `relayArchApprovalAndRecord` is the reviewer-set=`{architect}` special case, not a second gate).
- **Glossary, not verbatim import:** the client mock's `gatePhaseIds` (`svc-contract`, `fe-approve`, `test-plan`) are NOT real phase ids. The server policy stores/reads canonical `ActivityMethodPhase` ids (`requirements`/`detailed_design`/`test_plan`/`construction`/`integration`); provide a mapping.
- **Tests:** Go stdlib `testing`, table-driven, Temporal `testsuite` for the workflow gate (drive with `env.RegisterDelayedCallback` + `env.SignalWorkflow`). Run `GOWORK=off go test ./...` from `server/`. Lint delta only: `PATH="/opt/homebrew/bin:$PATH" GOWORK=off golangci-lint run --new-from-rev=main ./...`. Do NOT touch `.golangci.yml`.
- **WebApp has NO unit-test runner.** Verify webApp with `npm run gen:api` (after server OAS regen) + `npm run check` (typecheck+lint+format) + `npm run build`.
- **Gates that will fire on new public symbols:** `make method-check`, `make encapsulation-check`, `make sumtype-check` — keep any `switch` over a sealed enum exhaustive; register intended-public symbols the encapsulation test names.

---

### Task 1: `checkpointPolicy` model + phase-gate glossary (pure, inert default)

The proactive policy snapshot + the predicate that decides whether a phase gates. Pure Go, no workflow yet.

**Files:**
- Create: `server/internal/manager/construction/checkpointpolicy.go`
- Test: `server/internal/manager/construction/checkpointpolicy_test.go`

**Interfaces:**
- Produces:
  - `type checkpointPolicy struct { GatedPhases map[projectstate.ActivityMethodPhase]bool }` (zero value = gate nothing)
  - `func (p checkpointPolicy) gates(phase projectstate.ActivityMethodPhase) bool`
  - `func checkpointPolicyFromGateIDs(activityGateIDs []string) checkpointPolicy` — maps the mock's ad-hoc gate ids → canonical phases via `gateIDToPhase`
  - `var gateIDToPhase = map[string]projectstate.ActivityMethodPhase{...}`

- [ ] **Step 1: Write the failing test**

Create `checkpointpolicy_test.go`:

```go
package construction

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestCheckpointPolicy_ZeroValueGatesNothing(t *testing.T) {
	var p checkpointPolicy
	for _, ph := range []projectstate.ActivityMethodPhase{
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseDetailedDesign,
		projectstate.MethodPhaseTestPlan, projectstate.MethodPhaseConstruction,
		projectstate.MethodPhaseIntegration,
	} {
		if p.gates(ph) {
			t.Errorf("zero-value policy gates %q, want inert", ph)
		}
	}
}

func TestCheckpointPolicy_GatesConfiguredPhase(t *testing.T) {
	p := checkpointPolicy{GatedPhases: map[projectstate.ActivityMethodPhase]bool{
		projectstate.MethodPhaseDetailedDesign: true,
	}}
	if !p.gates(projectstate.MethodPhaseDetailedDesign) {
		t.Error("expected detailed_design gated")
	}
	if p.gates(projectstate.MethodPhaseConstruction) {
		t.Error("construction should not be gated")
	}
}

func TestCheckpointPolicyFromGateIDs_MapsMockIDsToCanonical(t *testing.T) {
	// The client mock uses ad-hoc ids; the server stores canonical phases.
	p := checkpointPolicyFromGateIDs([]string{"svc-contract", "svc-review"})
	if !p.gates(projectstate.MethodPhaseDetailedDesign) {
		t.Error("svc-contract must map to detailed_design")
	}
	if !p.gates(projectstate.MethodPhaseIntegration) {
		t.Error("svc-review must map to integration")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && GOWORK=off go test ./internal/manager/construction/ -run TestCheckpointPolicy -v`
Expected: FAIL — `undefined: checkpointPolicy`.

- [ ] **Step 3: Implement**

Create `checkpointpolicy.go`:

```go
package construction

import "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"

// checkpointPolicy is the PROACTIVE per-phase approval policy: which phases pause a
// HEALTHY activity for a human "approve to continue" gate. It is distinct from the
// REACTIVE interventionPolicy (which fires only on variance/failure). The zero value
// gates nothing — the phase loop then behaves exactly as before this feature ("pure
// vibes"). Captured by value at workflow start; never read mid-loop.
type checkpointPolicy struct {
	// GatedPhases: canonical ActivityMethodPhase ids that require human approval.
	GatedPhases map[projectstate.ActivityMethodPhase]bool
}

// gates reports whether the given phase requires a human approval gate.
func (p checkpointPolicy) gates(phase projectstate.ActivityMethodPhase) bool {
	return p.GatedPhases[phase]
}

// gateIDToPhase maps the webApp PolicyPanel's ad-hoc gate ids (svc-contract, fe-approve,
// test-plan, ...) to canonical phases. The mock vocabulary is NOT stored server-side; it
// is translated here so head-state and the workflow only ever see canonical phase ids.
var gateIDToPhase = map[string]projectstate.ActivityMethodPhase{
	"svc-contract": projectstate.MethodPhaseDetailedDesign,
	"svc-review":   projectstate.MethodPhaseIntegration,
	"fe-approve":   projectstate.MethodPhaseDetailedDesign,
	"test-plan":    projectstate.MethodPhaseTestPlan,
}

// checkpointPolicyFromGateIDs builds a policy from a list of gate ids (canonical or the
// mock's ad-hoc names). Unknown ids that are already canonical pass through; others are
// dropped.
func checkpointPolicyFromGateIDs(gateIDs []string) checkpointPolicy {
	gated := make(map[projectstate.ActivityMethodPhase]bool)
	for _, id := range gateIDs {
		if ph, ok := gateIDToPhase[id]; ok {
			gated[ph] = true
			continue
		}
		// Already-canonical id passes through.
		ph := projectstate.ActivityMethodPhase(id)
		switch ph {
		case projectstate.MethodPhaseRequirements, projectstate.MethodPhaseDetailedDesign,
			projectstate.MethodPhaseTestPlan, projectstate.MethodPhaseConstruction,
			projectstate.MethodPhaseIntegration:
			gated[ph] = true
		}
	}
	return checkpointPolicy{GatedPhases: gated}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && GOWORK=off go test ./internal/manager/construction/ -run TestCheckpointPolicy -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/manager/construction/checkpointpolicy.go server/internal/manager/construction/checkpointpolicy_test.go
git commit -m "feat(construction): proactive checkpointPolicy + gate-id glossary (inert default)"
```

---

### Task 2: Contract — add `StageAwaitingApproval`, `SubmitPhaseDecision`, `PhaseDecision` (codegen)

Extend the generated construction contract surface. This is a codegen task: edit the committed contract JSON, regenerate, verify the generated Go compiles.

**Files:**
- Modify: `/Users/davidmarne/mixofrealitystudio/archistrator/.aiarch/state/project.json` (`.serviceContracts` — the construction manager contract)
- Regenerated (do not hand-edit): `server/internal/manager/construction/contract.gen.go`, `server/internal/client/web/construction/*_handlers.gen.go`, `server/api/openapi.yaml`

**Interfaces:**
- Produces (generated):
  - `ConstructionStage` gains `StageAwaitingApproval` (next ordinal after `StageExited`=6 → 7)
  - `type PhaseDecision int` with `PhaseDecisionUnknown=0 / PhaseApprove=1 / PhaseSendBack=2`
  - `SubmitPhaseDecision(rc, projectID, activityID, phase string, decision PhaseDecision, feedback *ReviewFeedback) error` on the generated `ConstructionManager` interface
  - REST: `POST /api/v1/construction/submit-phase-decision/{projectID}/{activityID}`

- [ ] **Step 1: Read the templates**

Read the system-design `SubmitReviewDecision` contract entry and the construction contract entry in `.aiarch/state/project.json .serviceContracts`. Read `server/internal/manager/systemdesign/contract.gen.go:277-289` (the `ReviewDecision`/`ReviewFeedback` shapes) and `server/internal/manager/construction/contract.gen.go:37-47` (the `ConstructionStage` enum) to see exactly what the JSON must produce.

- [ ] **Step 2: Edit the construction contract JSON**

In `.aiarch/state/project.json .serviceContracts`, on the construction manager contract:
- Add `StageAwaitingApproval` as the next value of the `ConstructionStage` enum def (ordinal 7).
- Add a `PhaseDecision` enum def: `Unknown=0, Approve=1, SendBack=2` (mirror `ReviewDecision`'s shape).
- Add a `SubmitPhaseDecision` operation to the interface: params `projectID` (ProjectID), `activityID` (ActivityID), `phase` (string), `decision` (PhaseDecision), `feedback` (*ReviewFeedback — reuse/define like system-design's); no result; `error: true`. Mirror the existing `OverrideActivity` op's param/path shape so the generated REST path becomes `/api/v1/construction/submit-phase-decision/{projectID}/{activityID}`.

- [ ] **Step 3: Regenerate**

Run: `cd server && make gen`
Expected: regenerates `contract.gen.go`, the construction web handler, and `openapi.yaml`. Then `GOWORK=off go build ./...` — expect a COMPILE ERROR only where the new `SubmitPhaseDecision` interface method is unimplemented by `constructionManager` (that's Task 4) and where a `switch` over `ConstructionStage` is now non-exhaustive (add the `StageAwaitingApproval` case where the compiler/`make sumtype-check` points). Fix only those (do not implement business logic yet — a `panic("TODO Task 4")` stub for `SubmitPhaseDecision` on the manager is acceptable to make it compile).

- [ ] **Step 4: Verify generated surface**

Run: `cd server && GOWORK=off go vet ./internal/manager/construction/ && GOWORK=off go build ./...`
Expected: builds (with the Task-4 stub). Confirm `grep -n "StageAwaitingApproval\|SubmitPhaseDecision\|PhaseDecision" server/internal/manager/construction/contract.gen.go` shows the generated symbols and `grep -n "submit-phase-decision" server/api/openapi.yaml` shows the route.

- [ ] **Step 5: Commit**

```bash
git add .aiarch/state/project.json server/internal/manager/construction/ server/internal/client/web/construction/ server/api/openapi.yaml
git commit -m "feat(construction): contract — StageAwaitingApproval + SubmitPhaseDecision op (codegen)"
```

---

### Task 3: Phase-multiplexed `SubmitPhaseDecision` signal + manager send

Wire the manager op to signal the per-activity workflow, carrying the phase discriminator. Mirror `OverrideActivity`.

**Files:**
- Modify: `server/internal/manager/construction/signals.go` (payload struct)
- Modify: `server/internal/manager/construction/worker.go` (signal-name const)
- Modify: `server/internal/manager/construction/constructionmanager.go` (implement `SubmitPhaseDecision`, replacing the Task-2 stub)
- Test: `server/internal/manager/construction/constructionmanager_test.go` (append)

**Interfaces:**
- Consumes: `constructActivityWorkflowID(projectID, activityID)` (`constructionmanager.go:278`), `mapSignalError`
- Produces:
  - const `signalPhaseDecision = "phaseDecision"`
  - `type phaseDecisionSignal struct { Phase string; Decision PhaseDecision; Feedback *ReviewFeedback }`
  - `func (m *constructionManager) SubmitPhaseDecision(rc fwm.Context, projectID ProjectID, activityID ActivityID, phase string, decision PhaseDecision, feedback *ReviewFeedback) error`

- [ ] **Step 1: Write the failing test**

Append to `constructionmanager_test.go` (mirror the existing `OverrideActivity` manager test — read it first for the fake `client.SignalWorkflow` capture pattern):

```go
func TestSubmitPhaseDecision_SignalsActivityWorkflowWithPhase(t *testing.T) {
	fc := &fakeTemporalClient{}
	m := newTestConstructionManager(fc) // reuse the helper the OverrideActivity test uses
	err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseApprove, nil)
	if err != nil {
		t.Fatalf("SubmitPhaseDecision: %v", err)
	}
	if fc.lastWorkflowID != "proj-1:C-Orders" {
		t.Errorf("wfID = %q, want proj-1:C-Orders", fc.lastWorkflowID)
	}
	if fc.lastSignalName != signalPhaseDecision {
		t.Errorf("signal = %q, want %q", fc.lastSignalName, signalPhaseDecision)
	}
	sig, ok := fc.lastSignalArg.(phaseDecisionSignal)
	if !ok || sig.Phase != "detailed_design" || sig.Decision != PhaseApprove {
		t.Errorf("signal payload = %+v", fc.lastSignalArg)
	}
}
```

(If `fakeTemporalClient`/`newTestConstructionManager` differ, copy the exact fake + helper the existing `OverrideActivity` test uses — do NOT invent a new fake.)

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && GOWORK=off go test ./internal/manager/construction/ -run TestSubmitPhaseDecision -v`
Expected: FAIL (stub panics / undefined signal const).

- [ ] **Step 3: Implement**

In `worker.go` add to the signal-name const block: `signalPhaseDecision = "phaseDecision"`.

In `signals.go` add:
```go
// phaseDecisionSignal delivers a human approve/send-back for ONE gated phase. Phase
// carries which phase it answers so the per-activity workflow (which loops many phases)
// rejects a decision meant for a different phase.
type phaseDecisionSignal struct {
	Phase    string
	Decision PhaseDecision
	Feedback *ReviewFeedback
}
```

In `constructionmanager.go` replace the Task-2 stub (mirror `OverrideActivity` at `:207-228`):
```go
func (m *constructionManager) SubmitPhaseDecision(rc fwm.Context, projectID ProjectID, activityID ActivityID, phase string, decision PhaseDecision, feedback *ReviewFeedback) error {
	if decision == PhaseSendBack && (feedback == nil || feedback.Notes == "") {
		return fwm.NewContractMisuse("SubmitPhaseDecision: SendBack requires feedback notes")
	}
	wfID := constructActivityWorkflowID(projectID, activityID)
	sig := phaseDecisionSignal{Phase: phase, Decision: decision, Feedback: feedback}
	if err := m.client.SignalWorkflow(rc.Context, wfID, "", signalPhaseDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}
```
(Match the exact `fwm.Context` field/verbs the neighboring `OverrideActivity` uses — copy its error-constructor and `rc` access verbatim.)

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && GOWORK=off go test ./internal/manager/construction/ -run TestSubmitPhaseDecision -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/manager/construction/signals.go server/internal/manager/construction/worker.go server/internal/manager/construction/constructionmanager.go server/internal/manager/construction/constructionmanager_test.go
git commit -m "feat(construction): phase-multiplexed SubmitPhaseDecision signal + manager send"
```

---

### Task 4: The gate in the phase loop — suspend, reviewer-set, approve/sendback

Add the top-level gate to the per-phase loop: when policy gates the phase, record start → compute reviewer set → suspend on the phase-multiplexed signal → Approve records completion; SendBack redrafts THIS phase (own counter). Mirror the system-design gate shape.

**Files:**
- Modify: `server/internal/manager/construction/workflow.go` (the phase loop `:450-468`; `constructState`; add `StageAwaitingApproval` handling; fold `CheckpointPolicy` into `workflows` + `wfDeps`)
- Modify: `server/internal/manager/construction/worker.go` (fold `checkpointPolicy` into `wfDeps` at `RegisterWorker`)
- Test: `server/internal/manager/construction/workflow_test.go` (append gate tests)

**Interfaces:**
- Consumes: `checkpointPolicy.gates` (Task 1), `signalPhaseDecision`/`phaseDecisionSignal` (Task 3), `RecordPhaseStarted`/`RecordPhaseCompleted` (dormant, `deps.go:61-62`), `Review.ProposeReviews` (`deps.go:287`), `StageAwaitingApproval` (Task 2)
- Produces: gated phase loop; `workflows.CheckpointPolicy checkpointPolicy`; a `runPhaseGate` helper

- [ ] **Step 1: Write the failing tests**

Append to `workflow_test.go` (reuse the `runPumpWith`/`fakePipeline`/`fakeProjectState` scaffold added in the consolidation plan and the delayed-signal pattern from system-design's `Test_CoAuthor_Approve_Commits`):

```go
func Test_Construct_InertPolicy_NoGate_WalksAllPhases(t *testing.T) {
	// Zero-value checkpointPolicy: behaves exactly as today — no suspend, all phases dispatch.
	pipe := runPumpWith(t, sampleActivity()) // sampleActivity: service, 5 phases; default policy
	if len(pipe.submitted) != 5 {
		t.Fatalf("inert policy submitted %d, want 5", len(pipe.submitted))
	}
}

func Test_Construct_GatedPhase_SuspendsThenApprove_RecordsCompleted(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectState()
	pipe := newFakePipeline() // phases succeed
	wf := newWorkflowsGated(t, ps, pipe, checkpointPolicy{GatedPhases: map[projectstate.ActivityMethodPhase]bool{
		projectstate.MethodPhaseDetailedDesign: true,
	}})
	registerConstruct(env, wf)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity(),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("expected RecordPhaseCompleted(detailed_design) after approval")
	}
}

func Test_Construct_GatedPhase_StaleSignalRejected(t *testing.T) {
	// A decision for a DIFFERENT phase than the one suspended must not release the gate.
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectState()
	wf := newWorkflowsGated(t, ps, newFakePipeline(), checkpointPolicy{GatedPhases: map[projectstate.ActivityMethodPhase]bool{
		projectstate.MethodPhaseDetailedDesign: true,
	}})
	registerConstruct(env, wf)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "requirements", Decision: PhaseApprove}) // wrong phase
	}, 10*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove}) // correct
	}, 40*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("gate should release only on the matching-phase decision")
	}
}
```

Add the `newWorkflowsGated` test helper (a `newWorkflows` wrapper that sets `CheckpointPolicy`) and the `fakeProjectState.phaseCompleted`/`RecordPhaseCompleted` capture (extend the existing fake at `workflow_test.go:147,156`).

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && GOWORK=off go test ./internal/manager/construction/ -run Test_Construct_ -v`
Expected: FAIL (no gate; `newWorkflowsGated` undefined).

- [ ] **Step 3: Implement the gate**

Add `CheckpointPolicy checkpointPolicy` to the `workflows` struct (`workflow.go:78-97`) and to `wfDeps`; fold it in at `worker.go` `RegisterWorker` (default zero value — inert). In the phase loop (`workflow.go:450-468`), after a phase's pipeline SUCCEEDS, insert the gate before advancing:

```go
for _, phase := range in.Activity.Phases {
	state.stage = StagePipelineRunning
	obs, perr := wf.runPipeline(ctx, in, phase, state, &gf, &headVersion)
	if perr != nil {
		return perr
	}
	if obs.Phase == PipelineFailed || obs.Phase == PipelineCancelled {
		// ... existing variance handling (unchanged) ...
		phaseFailed = true
		break
	}
	// --- Top-level approval gate: policy selects the sub-workflow to reach "approved" ---
	if sentBack, gErr := wf.runPhaseGate(ctx, in, phase, state, &gf, &headVersion, startedCred); gErr != nil {
		return gErr
	} else if sentBack {
		phaseFailed = true // redraft budget exhausted → treat as a retry of the activity
		break
	}
}
```

Add `runPhaseGate` (mirror system-design `workflow.go:635-755` shape, but phase-scoped and policy-guarded):

```go
// runPhaseGate applies the top-level per-phase approval gate. When the checkpoint policy
// does NOT gate this phase it records completion and returns immediately (inert path).
// When gated, it records the phase started, computes the reviewer set (who must approve),
// suspends on the phase-multiplexed phaseDecision signal, and on Approve records the phase
// completed. SendBack redrafts THIS phase up to a human-paced budget; on exhaustion it
// returns sentBack=true so the caller retries the activity. It never routes through the
// failure/variance path.
func (wf *workflows) runPhaseGate(ctx workflow.Context, in constructActivityInput, phase projectstate.ActivityMethodPhase, state *constructState, gf *gitForward, headVersion *projectstate.Version, cred startedCredential) (sentBack bool, err error) {
	// Record the phase started (paired with completed below) — dormant hook, now wired.
	if v, e := wf.recordPhaseStarted(ctx, in, phase, *headVersion, cred); e != nil {
		return false, e
	} else {
		*headVersion = v
	}

	if !wf.CheckpointPolicy.gates(phase) {
		// Inert / vibes path: auto-approve, just stamp completion.
		return false, wf.recordPhaseCompletedStep(ctx, in, phase, headVersion, cred)
	}

	// Gated: compute the reviewer set for the session view (who must approve).
	if rs, e := wf.proposeReviewSet(ctx, in, phase); e == nil {
		state.reviewSet = rs
	}

	redraft := 0
	ch := workflow.GetSignalChannel(ctx, signalPhaseDecision)
	for {
		state.stage = StageAwaitingApproval
		var sig phaseDecisionSignal
		// Drain until we get a decision for THIS phase (reject stale/mismatched).
		for {
			ch.Receive(ctx, &sig)
			if sig.Phase == phase.String() {
				break
			}
			// stale signal for another phase — ignore.
		}
		switch sig.Decision {
		case PhaseApprove:
			return false, wf.recordPhaseCompletedStep(ctx, in, phase, headVersion, cred)
		case PhaseSendBack:
			redraft++
			if redraft >= maxPhaseRedrafts {
				return true, nil
			}
			// Redraft THIS phase with feedback: re-dispatch the same phase's pipeline.
			state.stage = StagePipelineRunning
			if _, e := wf.runPipeline(ctx, in, phase, state, gf, headVersion); e != nil {
				return false, e
			}
			// loop back to await the next decision on the redrafted output
		default:
			return false, workflow.NewContinueAsNewError(ctx, executionKindConstructActivity) // unreachable; defensive
		}
	}
}
```

Add `const maxPhaseRedrafts = 5` (human-paced budget, SEPARATE from `maxVarianceAttempts`). Implement the thin helpers `recordPhaseStarted`, `recordPhaseCompletedStep` (wrapping the dormant `RecordPhaseStarted`/`RecordPhaseCompleted` deps), and `proposeReviewSet` (calls `wf.Review.ProposeReviews(...)` building `reviewChange`/artifactKind from `in.Activity` + phase; on error return nil so the gate still functions). For the review-bearing phase, the existing `relayArchApprovalAndRecord`/`recordChangeReviewed` remain the durable record — do not add a second gate.

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && GOWORK=off go test ./internal/manager/construction/ -v`
Expected: PASS (inert, approve, stale-rejection tests + all pre-existing pump tests). Watch `gocognit` on the loop — `runPhaseGate` is a separate function so the loop stays small; if the gate function trips complexity, extract the redraft loop into a helper. Do NOT edit `.golangci.yml`.

- [ ] **Step 5: Commit**

```bash
git add server/internal/manager/construction/workflow.go server/internal/manager/construction/worker.go server/internal/manager/construction/workflow_test.go
git commit -m "feat(construction): per-phase approval gate (policy-gated suspend + redraft-this-phase)"
```

---

### Task 5: Source the checkpoint policy (config → workflow, inert default)

Give the policy a real source so it can be turned on, keeping the default inert. Mirror how `interventionMode` flows from config.

**Files:**
- Modify: `server/internal/manager/construction/adapters.go` (a `constructionCheckpointPolicy(gateIDs []string) checkpointPolicy` builder)
- Modify: `server/internal/manager/construction/constructionmanager.go` + `contract.gen.go` constructor path (accept the policy source) — see note
- Modify: `server/cmd/server/config.go` + `server/cmd/server/main.go` (a `ARCHISTRATOR_CONSTRUCTION_GATED_PHASES` comma list, default empty = inert)
- Test: `server/internal/manager/construction/adapters_test.go` (append)

**Interfaces:**
- Produces: `func constructionCheckpointPolicy(gateIDs []string) checkpointPolicy`

- [ ] **Step 1: Write the failing test**

```go
func TestConstructionCheckpointPolicy_EmptyIsInert(t *testing.T) {
	p := constructionCheckpointPolicy(nil)
	if len(p.GatedPhases) != 0 {
		t.Fatalf("empty gate list must be inert, got %v", p.GatedPhases)
	}
}

func TestConstructionCheckpointPolicy_BuildsFromGateIDs(t *testing.T) {
	p := constructionCheckpointPolicy([]string{"detailed_design"})
	if !p.gates(projectstate.MethodPhaseDetailedDesign) {
		t.Error("expected detailed_design gated")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && GOWORK=off go test ./internal/manager/construction/ -run TestConstructionCheckpointPolicy -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

In `adapters.go`:
```go
// constructionCheckpointPolicy builds the proactive per-phase approval policy from a list
// of gate ids (canonical phase ids or the webApp's ad-hoc names). Empty → inert.
func constructionCheckpointPolicy(gateIDs []string) checkpointPolicy {
	return checkpointPolicyFromGateIDs(gateIDs)
}
```

Thread a `[]string` gate list from config through the manager constructor to `RegisterWorker`'s `wfDeps.CheckpointPolicy` (mirror `interventionMode`'s path: `config.go` field `ConstructionGatedPhases []string` defaulted from `env("ARCHISTRATOR_CONSTRUCTION_GATED_PHASES","")` split on comma; `main.go` passes it into `NewConstructionManager`; `constructionmanager.go` stores it; `worker.go` builds `constructionCheckpointPolicy(impl.gatedPhases)` into `wfDeps`). The manager-constructor signature lives in `contract.gen.go` — if it needs a new param, add it to the contract JSON (`.serviceContracts` constructor) and `make gen`, mirroring how `interventionMode` is a constructor param.

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && GOWORK=off go test ./internal/manager/construction/ -run TestConstructionCheckpointPolicy -v && GOWORK=off go build ./...`
Expected: PASS + builds. Default (no env) → inert.

- [ ] **Step 5: Commit**

```bash
git add server/internal/manager/construction/ server/cmd/server/config.go server/cmd/server/main.go .aiarch/state/project.json server/api/openapi.yaml
git commit -m "feat(construction): source checkpoint policy from config (default inert)"
```

---

### Task 6: Server suite green + gates + inertness proof

**Files:**
- Test only (verification).

- [ ] **Step 1: Full suite + vet + gates**

Run (from `server/`):
```bash
GOWORK=off go test ./...
GOWORK=off go vet ./...
make method-check
make encapsulation-check
make sumtype-check
PATH="/opt/homebrew/bin:$PATH" GOWORK=off golangci-lint run --new-from-rev=main ./...
```
Expected: all green; delta-lint 0-new. If `encapsulation-check` flags new public symbols (`SubmitPhaseDecision`, `PhaseDecision`, `StageAwaitingApproval`), register them as intended-public per the test's printed guidance (do NOT unexport). If `sumtype-check` flags a non-exhaustive `ConstructionStage`/`PhaseDecision` switch, add the missing case.

- [ ] **Step 2: Prove inertness explicitly**

Confirm `Test_Construct_InertPolicy_NoGate_WalksAllPhases` passes and that the default server config produces an empty gate list (grep `ARCHISTRATOR_CONSTRUCTION_GATED_PHASES` default is `""`). This is the "pure vibes = today's behavior" guarantee.

- [ ] **Step 3: Commit (if any fixups)**

```bash
git add -A
git commit -m "test(construction): full-suite green + inertness proof for the approval gate"
```

---

### Task 7: WebApp — regen API + `useSubmitPhaseDecision` + `useUpdateCheckpointPolicy` hooks

**Files:**
- Regenerate: `webApp/src/api/schema.ts` (via `npm run gen:api`)
- Modify: `webApp/src/hooks/useConstructionMutations.ts` (append the two hooks — mirror `useOverrideActivity`)
- Modify: `webApp/src/api/enums.ts` (a `phaseDecisionToOrdinal` helper, mirror `reviewDecisionToOrdinal`)

**Interfaces:**
- Produces: `useSubmitPhaseDecision(projectId)`, `useUpdateCheckpointPolicy(projectId)`

- [ ] **Step 1: Regenerate the typed API**

Run: `cd webApp && npm run gen:api`
Expected: `src/api/schema.ts` now contains `/api/v1/construction/submit-phase-decision/{projectID}/{activityID}` and the checkpoint-policy route. Verify with `grep -n "submit-phase-decision" src/api/schema.ts`.

- [ ] **Step 2: Add the hooks**

In `useConstructionMutations.ts` (mirror `useOverrideActivity` at `:59-80`):
```ts
export interface PhaseDecisionVars {
  activityId: string;
  phase: string;
  decision: 'approve' | 'sendBack';
  notes?: string;
}

export function useSubmitPhaseDecision(projectId: string): UseMutationResult<undefined, Error, PhaseDecisionVars> {
  const client = useQueryClient();
  return useMutation<undefined, Error, PhaseDecisionVars>({
    mutationFn: async (vars) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/construction/submit-phase-decision/{projectID}/{activityID}',
        {
          params: { path: { projectID: projectId, activityID: vars.activityId } },
          body: {
            phase: vars.phase,
            decision: phaseDecisionToOrdinal(vars.decision),
            ...(vars.notes !== undefined ? { feedback: { notes: vars.notes } } : {}),
          },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: constructionSessionKey(projectId) });
    },
  });
}
```
Add `phaseDecisionToOrdinal` in `enums.ts` (`approve`→1, `sendBack`→2). Add `useUpdateCheckpointPolicy` the same way against the policy route.

- [ ] **Step 3: Verify**

Run: `cd webApp && npm run typecheck && npm run lint`
Expected: clean (the generated `schema.ts` types the new routes).

- [ ] **Step 4: Commit**

```bash
git add webApp/src/api/schema.ts webApp/src/hooks/useConstructionMutations.ts webApp/src/api/enums.ts
git commit -m "feat(webapp): submitPhaseDecision + updateCheckpointPolicy hooks"
```

---

### Task 8: WebApp — construction GatePanel + real PolicyPanel wiring

**Files:**
- Create: `webApp/src/components/construction/PhaseGatePanel.tsx` (mirror `design/GatePanel.tsx` — Approve / Send back)
- Modify: `webApp/src/components/construction/PolicyPanel.tsx` (drive from `useUpdateCheckpointPolicy` instead of client-only `useState`)
- Modify: the construction console screen that renders the intervention queue (wire `PhaseGatePanel` where `ConstructionSessionView.stage === StageAwaitingApproval`)

**Interfaces:**
- Consumes: `useSubmitPhaseDecision`, `useUpdateCheckpointPolicy` (Task 7); `ConstructionSessionView.stage`/`reviewSet`/`activityId`

- [ ] **Step 1: Build the PhaseGatePanel**

Create `PhaseGatePanel.tsx` mirroring `design/GatePanel.tsx:133-164` (Approve & continue / Send back buttons, `pending` prop, testids from `UI_IDENTIFIERS`). It takes `onApprove`/`onSendBack`/`pending` and shows the phase label + the `reviewSet` (who must approve).

- [ ] **Step 2: Wire it in the console**

In the construction console screen, when the session view's `stage === StageAwaitingApproval`, render `PhaseGatePanel` wired to `useSubmitPhaseDecision(projectId)` with `{ activityId, phase, decision }` (mirror `DesignExperience.tsx:188-217`'s approve/sendBack handlers).

- [ ] **Step 3: Make PolicyPanel real**

Replace `PolicyPanel.tsx`'s client-only `useState` with `useUpdateCheckpointPolicy` — toggling a rule POSTs the gate list; the header comment about "client-only, no backend call" is removed. Keep the `PolicyRule` shape but send canonical gate ids (or let the server glossary map them).

- [ ] **Step 4: Verify**

Run: `cd webApp && npm run check && npm run build`
Expected: typecheck + lint + format clean; build succeeds.

- [ ] **Step 5: Commit**

```bash
git add webApp/src/components/construction/ webApp/src/screens/
git commit -m "feat(webapp): construction PhaseGatePanel + live checkpoint PolicyPanel"
```

---

## Self-Review (completed during authoring)

**Spec/design coverage:**
- Top-level gate per phase (approved-to-continue) → Task 4 (`runPhaseGate`, `PhaseCompletion` via `RecordPhaseCompleted`).
- Policy selects the sub-workflow (vibes/architect-only/full reviewer-set) → Task 1 (policy), Task 4 (`gates` guard + `ProposeReviews` on gated phases; inert path = auto-approve).
- checkpointPolicy proactive, default inert → Task 1 (zero value), Task 5 (empty config), Task 6 Step 2 (inertness proof).
- SendBack = redraft-this-phase, own budget, NOT variance → Task 4 (`maxPhaseRedrafts`, re-dispatch this phase, never `handleVariance`).
- Phase-multiplexed signal + reject stale → Task 3 (`phaseDecisionSignal.Phase`), Task 4 (drain-until-matching-phase + `Test_..._StaleSignalRejected`).
- Wire `RecordPhaseStarted`+`RecordPhaseCompleted` as a pair → Task 4 (both in `runPhaseGate`, including inert path).
- Don't stack two gates (arch +1) → Task 4 note (existing relay stays the durable record).
- Glossary (mock ids ≠ canonical) → Task 1 (`gateIDToPhase`).
- Codegen-first for the contract surface → Task 2, Task 5 (edit `.serviceContracts` → `make gen`).
- WebApp gate + real policy → Tasks 7–8.

**Placeholder scan:** the only intentional stub is Task 2 Step 3's `panic("TODO Task 4")` on the unimplemented interface method to make the generated code compile — replaced in Task 3/4. Test helpers (`newWorkflowsGated`, `fakeProjectState.phaseCompleted`) are described with the exact fakes to extend (Task 4 Step 1). Every code step shows real code or cites the verbatim pattern file:line to mirror.

**Type consistency:** `checkpointPolicy{GatedPhases}` / `gates(phase)` / `checkpointPolicyFromGateIDs` (Task 1) ↔ `constructionCheckpointPolicy` (Task 5) ↔ `workflows.CheckpointPolicy` (Task 4). `phaseDecisionSignal{Phase, Decision, Feedback}` + `signalPhaseDecision` + `PhaseDecision{Approve,SendBack}` consistent across Tasks 2–4 and the webApp `phaseDecisionToOrdinal` (Task 7). `SubmitPhaseDecision(projectID, activityID, phase, decision, feedback)` identical in contract (Task 2), manager (Task 3), and hook (Task 7).

**Note for the architect gate:** per the founder, route this written plan through the system-architect before execution — focus areas: the `runPhaseGate` control flow (redraft loop vs the outer variance retry), the codegen contract edits (Task 2/5), and confirmation the inert path is a true no-op.
