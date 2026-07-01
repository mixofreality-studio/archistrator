# Construction Per-Phase Review/Approval Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the construction per-phase loop a **conditional human-approval gate** driven by a **per-project, committed, editable review policy** — reusing the system-design gate pattern and the existing `reviewEngine.ProposeReviews` (who reviews), rather than a new parallel mechanism. Default (empty policy) = today's behavior.

**Architecture:** A per-project `ReviewPolicy` is committed in `project.json` and read as a by-value snapshot at each `ConstructActivityWorkflow` start. In the phase loop, after a phase's pipeline succeeds: skip if already completed (resumable); else `ProposeReviews` computes the reviewer set (display), and **iff `policy.RequiresHuman(activityType, phase)`** the workflow suspends on a phase-multiplexed `SubmitPhaseDecision` signal (the same stage→suspend→decide shape system-design uses). Approve → `RecordPhaseCompleted`; SendBack → redraft *this* phase (own human-paced budget, never the variance path). Phase records are gated on `gitOn` so the no-gate path is byte-for-byte today's behavior. The contract surface (stage enum, ops, DTOs) is generated from `.aiarch/state/project.json .serviceContracts` via `make gen`. WebApp adds a construction GatePanel + a live PolicyPanel editing the policy.

**Tech Stack:** Go 1.25.4 + Temporal (`testsuite`), schema-first codegen (`make gen`), React 19 + MUI 7 + openapi-fetch (webApp).

## Global Constraints

- **Codegen-first:** the construction `ConstructionStage` enum, manager ops, DTOs are GENERATED. To add `StageAwaitingApproval` / `SubmitPhaseDecision` / `UpdateReviewPolicy` / DTOs: edit `.aiarch/state/project.json .serviceContracts` (construction contract), then `cd server && make gen` (regenerates every `contract.gen.go`, web/mcp handlers, `server/api/openapi.yaml`). NEVER hand-edit a `*.gen.go` or `openapi.yaml`. Copy the system-design `SubmitReviewDecision` contract entry as the template.
- **No parallel policy struct, no "sub-workflow."** Reuse: the Phase-1/2 gate shape (stage→suspend→decide), `reviewEngine.ProposeReviews` (who reviews — `deps.go:288`, currently uncalled in construction), and `handOffPolicy` (human-vs-AI actor). The only net-new policy is the per-project committed `ReviewPolicy` (whether a human must approve, which phases).
- **Default inert:** an empty `ReviewPolicy` gates nothing → the phase loop behaves exactly as today. "Approve every step" = all phases listed; "pure vibes" = empty. Prove with a test.
- **SendBack ≠ variance.** SendBack redrafts THIS phase (mirror system-design reject→redraft, `systemdesign/workflow.go:712-735`), with a human-paced counter SEPARATE from `maxVarianceAttempts`. On budget exhaustion, keep awaiting the human (or `recordActivityFailed` THIS activity) — do NOT `phaseFailed=true; continue` (that re-enters the `for attempt` variance loop at `workflow.go:375`, restarts from phase 0, re-gates approved phases, and burns the failure budget). Never route SendBack through `handleVariance`/`overrideCh`.
- **Resumable phase loop:** at the top of each phase, if head-state already has `RecordPhaseCompleted` for it, SKIP dispatch and the gate. Without this, any variance retry re-suspends for approval on already-approved phases (`workflow.go:450-471` has no resume cursor today).
- **`RecordPhaseStarted`/`RecordPhaseCompleted` need Activity wrappers.** They live on the `constructionTransitionAccess` seam (`deps.go:61-62`) — a workflow cannot call RA I/O directly. Add `RecordPhaseStartedActivity`/`RecordPhaseCompletedActivity` (+ args structs + name consts + `worker.go` `RegisterActivityWithOptions`). Wire the pair together.
- **`gitOn` no-op:** gate the phase-records on the existing `gitOn` condition (as `recordActivityStarted`/`recordActivityCompleted` are, `workflow.go:364-368,507-511`) so the no-gate path is a true no-op = today.
- **Phase-multiplexed signal:** the gate is one step inside the per-*activity* workflow (`{projectId}:{activityId}`) looping many phases, so `SubmitPhaseDecision` MUST carry which phase it answers; drain-until-matching-phase, reject stale.
- **Don't stack two gates:** for the review-bearing phase the new gate IS the architect +1; `recordChangeReviewed`/`relayArchApprovalAndRecord` become the gate's record (reviewer set = `{architect}` case), not a second gate.
- **Engine call in-workflow is fine:** `ProposeReviews` is pure/deterministic and called directly in-workflow by design (`deps.go:22-27`) — no Activity wrapper. Source its `architectureGraph`/`contracts` from a `readProject` (don't pass empty).
- **Policy edits only affect newly-started activity workflows** (already-running ones captured their snapshot). State this in the UI.
- **Tests:** Go stdlib `testing`, table-driven, Temporal `testsuite` (drive with `env.RegisterDelayedCallback`+`env.SignalWorkflow`). Run `GOWORK=off go test ./...` from `server/`. Lint delta: `PATH="/opt/homebrew/bin:$PATH" GOWORK=off golangci-lint run --new-from-rev=main ./...`. Never edit `.golangci.yml`. Gates: `make method-check`/`encapsulation-check`/`sumtype-check`. WebApp: no unit runner — verify with `npm run gen:api` + `npm run check` + `npm run build`.

---

### Task 1: Per-project `ReviewPolicy` model + persistence (projectstate)

The committed policy: which (activity-type, phase) pairs require human approval. Stored on `Project`, encoded/decoded symmetrically, with a pure `RequiresHuman`. Empty = inert.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/artifactmodel.go` (add `ReviewPolicy ReviewPolicy` field to `Project`; the type)
- Modify: `server/internal/resourceaccess/projectstate/gitstore.go` (encode/decode the field in `projectDoc`, mirror `OperatorPaused`/`PauseReason` at the lines the reconnaissance cited `:719-724`)
- Create: `server/internal/resourceaccess/projectstate/reviewpolicy.go` (the type + `RequiresHuman` + gate-id glossary)
- Test: `server/internal/resourceaccess/projectstate/reviewpolicy_test.go`

**Interfaces:**
- Produces:
  - `type ReviewPolicy struct { GatedPhasesByType map[string][]ActivityMethodPhase }` (key = `ActivityType.String()`)
  - `func (p ReviewPolicy) RequiresHuman(activityType string, phase ActivityMethodPhase) bool`
  - `func ReviewPolicyFromGateIDs(byType map[string][]string) ReviewPolicy` (maps ad-hoc/canonical ids → canonical phases)
  - `Project.ReviewPolicy ReviewPolicy` (persisted, `json:"reviewPolicy,omitempty"`)

- [ ] **Step 1: Write the failing test**

Create `reviewpolicy_test.go`:

```go
package projectstate

import "testing"

func TestReviewPolicy_EmptyRequiresNoHuman(t *testing.T) {
	var p ReviewPolicy
	if p.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("empty policy must require no human approval (inert)")
	}
}

func TestReviewPolicy_RequiresHumanForGatedPhase(t *testing.T) {
	p := ReviewPolicy{GatedPhasesByType: map[string][]ActivityMethodPhase{
		"frontend": {MethodPhaseDetailedDesign},
	}}
	if !p.RequiresHuman("frontend", MethodPhaseDetailedDesign) {
		t.Error("frontend/detailed_design should require human")
	}
	if p.RequiresHuman("frontend", MethodPhaseConstruction) {
		t.Error("frontend/construction not gated")
	}
	if p.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("service not gated")
	}
}

func TestReviewPolicyFromGateIDs_MapsMockIDs(t *testing.T) {
	p := ReviewPolicyFromGateIDs(map[string][]string{"service": {"svc-contract"}})
	if !p.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("svc-contract must map to detailed_design")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestReviewPolicy -v`
Expected: FAIL — `undefined: ReviewPolicy`.

- [ ] **Step 3: Implement the type**

Create `reviewpolicy.go`:

```go
package projectstate

// ReviewPolicy is the per-project, committed configuration of WHICH phases require a
// human approval gate during construction. It composes with the reviewEngine (which
// computes WHO reviews): the engine gives the reviewer set; this policy says whether a
// human must sign off before the phase advances. The zero value gates nothing — the
// construction loop then behaves exactly as before this feature ("pure vibes").
type ReviewPolicy struct {
	// GatedPhasesByType maps an ActivityType wire name ("service"/"frontend"/"testing"/...)
	// to the canonical phases that require human approval for that type.
	GatedPhasesByType map[string][]ActivityMethodPhase `json:"gatedPhasesByType,omitempty"`
}

// RequiresHuman reports whether a phase of the given activity type requires human approval.
func (p ReviewPolicy) RequiresHuman(activityType string, phase ActivityMethodPhase) bool {
	for _, gated := range p.GatedPhasesByType[activityType] {
		if gated == phase {
			return true
		}
	}
	return false
}

// gateIDToPhase maps the webApp PolicyPanel's ad-hoc gate ids to canonical phases, so the
// mock vocabulary never reaches head-state. Canonical ids pass through in ReviewPolicyFromGateIDs.
var gateIDToPhase = map[string]ActivityMethodPhase{
	"svc-contract": MethodPhaseDetailedDesign,
	"svc-review":   MethodPhaseIntegration,
	"fe-approve":   MethodPhaseDetailedDesign,
	"test-plan":    MethodPhaseTestPlan,
}

// ReviewPolicyFromGateIDs builds a policy from per-type gate-id lists (canonical or ad-hoc).
func ReviewPolicyFromGateIDs(byType map[string][]string) ReviewPolicy {
	out := ReviewPolicy{GatedPhasesByType: map[string][]ActivityMethodPhase{}}
	for typ, ids := range byType {
		for _, id := range ids {
			ph, ok := gateIDToPhase[id]
			if !ok {
				ph = ActivityMethodPhase(id)
			}
			switch ph {
			case MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
				MethodPhaseConstruction, MethodPhaseIntegration:
				out.GatedPhasesByType[typ] = append(out.GatedPhasesByType[typ], ph)
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Persist on `Project`**

In `artifactmodel.go` add to `Project` (near `OperatorPaused`): `ReviewPolicy ReviewPolicy `json:"reviewPolicy,omitempty"``. In `gitstore.go`, add the field to `projectDoc` and its encode+decode (mirror `OperatorPaused`/`PauseReason`, cited `:719-724`). Confirm round-trip symmetry.

- [ ] **Step 5: Run to verify it passes + round-trip**

Run: `cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run "TestReviewPolicy\|TestProjectDoc" -v`
Expected: PASS (and any existing projectDoc round-trip test still green with the new field).

- [ ] **Step 6: Commit**

```bash
git add server/internal/resourceaccess/projectstate/reviewpolicy.go server/internal/resourceaccess/projectstate/reviewpolicy_test.go server/internal/resourceaccess/projectstate/artifactmodel.go server/internal/resourceaccess/projectstate/gitstore.go
git commit -m "feat(projectstate): per-project committed ReviewPolicy (inert default) + persistence"
```

---

### Task 2: Contract — `StageAwaitingApproval`, `SubmitPhaseDecision`, `UpdateReviewPolicy`, `PhaseDecision` (codegen)

**Files:**
- Modify: `.aiarch/state/project.json` (`.serviceContracts` — construction contract)
- Regenerated (do not hand-edit): construction `contract.gen.go`, web handler, `server/api/openapi.yaml`

**Interfaces (generated):**
- `ConstructionStage` gains `StageAwaitingApproval` (ordinal 7, after `StageExited`=6)
- `type PhaseDecision int` — `Unknown=0/Approve=1/SendBack=2`
- `SubmitPhaseDecision(rc, projectID, activityID, phase string, decision PhaseDecision, feedback *ReviewFeedback) error` → `POST /api/v1/construction/submit-phase-decision/{projectID}/{activityID}`
- `UpdateReviewPolicy(rc, projectID, policy ReviewPolicyInput) error` → `POST /api/v1/construction/review-policy/{projectID}` (a `ReviewPolicyInput` DTO = per-type gate-id lists)

- [ ] **Step 1: Read templates** — the system-design `SubmitReviewDecision` contract entry + `ReviewDecision`/`ReviewFeedback` shapes (`systemdesign/contract.gen.go:277-289`), the construction `OverrideActivity` op (for the `{projectID}/{activityID}` path shape), and the `ConstructionStage` enum (`construction/contract.gen.go:37-47`).

- [ ] **Step 2: Edit the construction contract JSON** — add: `StageAwaitingApproval` (enum value 7); `PhaseDecision` enum (Unknown/Approve/SendBack); a `ReviewFeedback` def (mirror system-design's `{notes, comments}`); a `ReviewPolicyInput` def (`{ gatedPhasesByType: map<string, string[]> }`); the `SubmitPhaseDecision` op (params projectID, activityID, phase:string, decision:PhaseDecision, feedback:*ReviewFeedback; error:true); the `UpdateReviewPolicy` op (params projectID, policy:ReviewPolicyInput; error:true).

- [ ] **Step 3: Regenerate + compile-guard** — `cd server && make gen && GOWORK=off go build ./...`. Expect compile errors only at the two unimplemented interface methods (Tasks 3/4) and any non-exhaustive `ConstructionStage` switch. Stub the two manager methods to **return a mapped error** (`return fwm.NewContractMisuse("SubmitPhaseDecision: not yet implemented")`) — NOT `panic` (Task 2's commit ships a live route; it must not panic in an HTTP handler). Add the `StageAwaitingApproval` case where `sumtype-check`/compiler points.

- [ ] **Step 4: Verify generated surface** — `GOWORK=off go vet ./internal/manager/construction/`; `grep -n "StageAwaitingApproval\|SubmitPhaseDecision\|UpdateReviewPolicy\|PhaseDecision" server/internal/manager/construction/contract.gen.go`; `grep -n "submit-phase-decision\|review-policy" server/api/openapi.yaml`.

- [ ] **Step 5: Commit**

```bash
git add .aiarch/state/project.json server/internal/manager/construction/ server/internal/client/web/construction/ server/api/openapi.yaml
git commit -m "feat(construction): contract — approval-gate + review-policy ops (codegen)"
```

---

### Task 3: `SubmitPhaseDecision` signal + manager send (phase-multiplexed)

Mirror `OverrideActivity` (`constructionmanager.go:207-228`).

**Files:** `signals.go` (payload), `worker.go` (signal const), `constructionmanager.go` (implement, replacing the Task-2 error stub), `constructionmanager_test.go` (append).

**Interfaces:** `const signalPhaseDecision = "phaseDecision"`; `type phaseDecisionSignal struct { Phase string; Decision PhaseDecision; Feedback *ReviewFeedback }`; `SubmitPhaseDecision(rc, projectID, activityID, phase string, decision PhaseDecision, feedback *ReviewFeedback) error`.

- [ ] **Step 1: Failing test** (mirror the existing `OverrideActivity` manager test's `fakeTemporalClient` capture — copy that exact fake/helper):

```go
func TestSubmitPhaseDecision_SignalsActivityWorkflowWithPhase(t *testing.T) {
	fc := &fakeTemporalClient{}
	m := newTestConstructionManager(fc)
	if err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseApprove, nil); err != nil {
		t.Fatalf("SubmitPhaseDecision: %v", err)
	}
	if fc.lastWorkflowID != "proj-1:C-Orders" || fc.lastSignalName != signalPhaseDecision {
		t.Fatalf("wfID=%q signal=%q", fc.lastWorkflowID, fc.lastSignalName)
	}
	sig, ok := fc.lastSignalArg.(phaseDecisionSignal)
	if !ok || sig.Phase != "detailed_design" || sig.Decision != PhaseApprove {
		t.Fatalf("payload=%+v", fc.lastSignalArg)
	}
}
```

- [ ] **Step 2: Run → FAIL** (`GOWORK=off go test ./internal/manager/construction/ -run TestSubmitPhaseDecision -v`).

- [ ] **Step 3: Implement** — add the const to `worker.go`'s signal block; add `phaseDecisionSignal` to `signals.go`; implement `SubmitPhaseDecision` (mirror `OverrideActivity`: validate SendBack needs feedback notes, `wfID := constructActivityWorkflowID(projectID, activityID)`, `m.client.SignalWorkflow(rc.Context, wfID, "", signalPhaseDecision, phaseDecisionSignal{...})`, `mapSignalError`).

- [ ] **Step 4: Run → PASS.**

- [ ] **Step 5: Commit** — `feat(construction): phase-multiplexed SubmitPhaseDecision signal + manager send`.

---

### Task 4: `UpdateReviewPolicy` manager op + persistence

Persist the per-project policy so it's editable. Replaces the Task-2 error stub.

**Files:** `constructionmanager.go` (implement `UpdateReviewPolicy`), a projectstate RA write verb if none fits (`gitconstruction.go` — a `RecordReviewPolicy(projectID, expectedVersion, policy, cred, idem) (Version, error)` mirroring `RecordOperatorPaused` at `gitconstruction.go:141-145`), the port decl in `deps.go`, and the fake. Test: `constructionmanager_test.go`.

**Interfaces:** `UpdateReviewPolicy(rc, projectID, policy ReviewPolicyInput) error`; RA `RecordReviewPolicy(...)`.

- [ ] **Step 1: Failing test** — assert `UpdateReviewPolicy` maps the `ReviewPolicyInput` (per-type gate-id lists) through `projectstate.ReviewPolicyFromGateIDs` and calls the RA write with the resulting typed policy (use the manager's fake projectstate; assert the persisted `ReviewPolicy`).

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement** — `UpdateReviewPolicy` converts `ReviewPolicyInput.GatedPhasesByType` (map<string,[]string>) via `projectstate.ReviewPolicyFromGateIDs`, reads current version, calls `RecordReviewPolicy`. Add `RecordReviewPolicy` to the port + `gitconstruction.go` impl (set `p.ReviewPolicy = policy` inside `applyMutation`, mirror `RecordOperatorPaused`) + the fake.

- [ ] **Step 4: Run → PASS** + `GOWORK=off go build ./...`.

- [ ] **Step 5: Commit** — `feat(construction): UpdateReviewPolicy op + RA persistence`.

---

### Task 5: Phase-record Activity wrappers + registration (B3)

Make the dormant `RecordPhaseStarted`/`RecordPhaseCompleted` callable from the workflow.

**Files:** `activities.go` (two `*Activity` methods + args structs), `worker.go` (two name consts + `RegisterActivityWithOptions`), `workflow_test.go` (extend the fakes to capture). 

**Interfaces:** `RecordPhaseStartedActivity(ctx, recordPhaseStartedArgs) (Version, error)` and `RecordPhaseCompletedActivity(ctx, recordPhaseCompletedArgs) (Version, error)`; consts `actRecordPhaseStarted`/`actRecordPhaseCompleted`.

- [ ] **Step 1: Failing test** — a small activity test (mirror an existing `Record*Activity` test in `activities_test.go` if present) asserting `RecordPhaseStartedActivity` forwards to the transition-access seam with the right args; or assert via the workflow test in Task 6. If activities have no direct unit test, cover them through Task 6's gate test and note that here.

- [ ] **Step 2: Run → FAIL** (undefined).

- [ ] **Step 3: Implement** — add the two Activity methods (mirror `RecordChangeReviewedActivity` in `activities.go`: build args, call `wf.Transition.RecordPhaseStarted(...)`/`RecordPhaseCompleted(...)`, return version). Add the two name consts and two `RegisterActivityWithOptions(wf.RecordPhaseStartedActivity, activity.RegisterOptions{Name: actRecordPhaseStarted})` lines in `worker.go:132-159`.

- [ ] **Step 4: Run → PASS** + `GOWORK=off go build ./...`.

- [ ] **Step 5: Commit** — `feat(construction): RecordPhaseStarted/Completed activity wrappers + registration`.

---

### Task 6: The gate in the phase loop (snapshot, resumable, conditional suspend, redraft)

The core. Read the policy snapshot at workflow start; make the loop resumable; on each phase success, gate iff the policy requires human approval.

**Files:** `workflow.go` (read snapshot at start; `constructState`; the phase loop `:450-471`; `runPhaseGate` helper; `maxPhaseRedrafts` const), `workflow_test.go` (append gate tests).

**Interfaces:** Consumes `projectstate.ReviewPolicy.RequiresHuman`, `signalPhaseDecision`/`phaseDecisionSignal` (T3), `RecordPhaseStartedActivity`/`RecordPhaseCompletedActivity` (T5), `Review.ProposeReviews` (`deps.go:288`), `StageAwaitingApproval` (T2). Produces the gated, resumable loop.

- [ ] **Step 1: Write the failing tests**

Append to `workflow_test.go` (reuse `runPumpWith`/`fakePipeline`/`fakeProjectState`; drive signals with `env.RegisterDelayedCallback`+`env.SignalWorkflow` like `systemdesign` `Test_CoAuthor_Approve_Commits`). Extend `fakeProjectState` to (a) return a `ReviewPolicy` from its project and (b) capture `phaseCompleted(activityID, phase)`.

```go
func Test_Construct_EmptyPolicy_NoGate_WalksAllPhases(t *testing.T) {
	// Empty ReviewPolicy → no suspend, all phases dispatch. Byte-for-byte today's behavior.
	pipe := runPumpWith(t, sampleActivity()) // fakeProjectState default policy = empty
	if len(pipe.submitted) != 5 {
		t.Fatalf("empty policy submitted %d, want 5", len(pipe.submitted))
	}
}

func Test_Construct_GatedPhase_ApproveRecordsCompleted(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign},
	}})
	wf := newWorkflows(gateDeps(ps, newFakePipeline()))
	registerConstruct(env, wf)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 30*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("expected RecordPhaseCompleted(detailed_design) after approval")
	}
}

func Test_Construct_GatedPhase_StaleSignalIgnored(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign},
	}})
	wf := newWorkflows(gateDeps(ps, newFakePipeline()))
	registerConstruct(env, wf)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "requirements", Decision: PhaseApprove}) // wrong phase
	}, 10*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 40*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("gate must release only on the matching-phase decision")
	}
}
```

Add helpers `newFakeProjectStateWithPolicy`, `gateDeps` (a `wfDeps` builder), and the fake's `phaseCompleted`.

- [ ] **Step 2: Run → FAIL** (no gate; helpers undefined).

- [ ] **Step 3: Implement**

**Snapshot at start:** near workflow entry (before the `for attempt` loop), read the project once and capture `reviewPolicy := proj.ReviewPolicy` by value (deterministic; do NOT re-read mid-loop). Thread it into the loop scope.

**Resumable + gate in the loop** (`workflow.go:450`):
```go
for _, phase := range in.Activity.Phases {
	if state.phaseAlreadyDone(phase) { // reads head-state PhaseCompletion (resumable skip-guard)
		continue
	}
	state.stage = StagePipelineRunning
	obs, perr := wf.runPipeline(ctx, in, phase, state, &gf, &headVersion)
	if perr != nil {
		return perr
	}
	if obs.Phase == PipelineFailed || obs.Phase == PipelineCancelled {
		// ... existing variance handling UNCHANGED ...
		phaseFailed = true
		break
	}
	if done, gErr := wf.runPhaseGate(ctx, in, phase, reviewPolicy, state, &gf, &headVersion, gitOn, startedCred); gErr != nil {
		return gErr
	} else if done {
		return nil // gate terminally failed THIS activity (redraft exhausted) — recorded inside
	}
}
```

**`runPhaseGate`** (mirror `systemdesign/workflow.go:635-755`, phase-scoped):
```go
// runPhaseGate records phase start, and — iff the review policy requires human approval
// for this (activityType, phase) — suspends on the phase-multiplexed decision signal.
// Approve records completion; SendBack redrafts THIS phase up to maxPhaseRedrafts, then
// (mirroring systemdesign) keeps awaiting the human — it NEVER re-enters the variance
// loop. Returns done=true only if it terminally fails this activity. Phase records are
// gated on gitOn so the no-gate path is a true no-op.
func (wf *workflows) runPhaseGate(ctx workflow.Context, in constructActivityInput, phase projectstate.ActivityMethodPhase, policy projectstate.ReviewPolicy, state *constructState, gf *gitForward, headVersion *projectstate.Version, gitOn bool, cred startedCredential) (done bool, err error) {
	if gitOn {
		if v, e := wf.recordPhaseStarted(ctx, in, phase, *headVersion, cred); e != nil {
			return false, e
		} else {
			*headVersion = v
		}
	}
	if !policy.RequiresHuman(in.Activity.activityTypeName(), phase) {
		return false, wf.recordPhaseCompletedIfGit(ctx, in, phase, headVersion, gitOn, cred)
	}
	if rs, e := wf.proposeReviewSet(ctx, in, phase); e == nil {
		state.reviewSet = &rs // NOTE: pointer
	}
	ch := workflow.GetSignalChannel(ctx, signalPhaseDecision)
	redraft := 0
	for {
		state.stage = StageAwaitingApproval
		var sig phaseDecisionSignal
		for { // drain until a decision for THIS phase; ignore stale
			ch.Receive(ctx, &sig)
			if sig.Phase == phase.String() {
				break
			}
		}
		switch sig.Decision {
		case PhaseApprove:
			return false, wf.recordPhaseCompletedIfGit(ctx, in, phase, headVersion, gitOn, cred)
		case PhaseSendBack:
			redraft++
			if redraft >= maxPhaseRedrafts {
				// Mirror systemdesign exhaustion: do NOT restart the activity; keep awaiting
				// the human. (Alternatively record a terminal failure for THIS activity.)
				continue
			}
			state.stage = StagePipelineRunning
			if _, e := wf.runPipeline(ctx, in, phase, state, gf, headVersion); e != nil {
				return false, e
			}
		default:
			// unknown decision: ignore and keep awaiting
		}
	}
}
```

Add `const maxPhaseRedrafts = 5` (separate from `maxVarianceAttempts`). Implement `state.phaseAlreadyDone(phase)` (reads the activity's `PhaseCompletion` from head-state / the project the workflow already holds), `recordPhaseStarted`/`recordPhaseCompletedIfGit` (call the Task-5 Activities; the completed variant is a no-op when `!gitOn`), `proposeReviewSet` (build `reviewChange`+artifactKind from `in.Activity`+phase, pass the project's `architectureGraph`+`contracts` — source them from the project the workflow read at start; on engine error return the zero set so the gate still functions), and `constructionActivity.activityTypeName()` (returns `projectstate.DeriveType(activityID).String()`). For the review-bearing phase the existing `relayArchApprovalAndRecord`/`recordChangeReviewed` stay the durable record — do not add a second gate.

- [ ] **Step 4: Run → PASS**

Run: `cd server && GOWORK=off go test ./internal/manager/construction/ -v`
Expected: PASS (empty-policy walks all 5; gated approve records completed; stale ignored; all pre-existing pump tests). Watch `gocognit` — the gate is its own function; if it trips, extract the redraft loop. Don't edit `.golangci.yml`.

- [ ] **Step 5: Commit** — `feat(construction): conditional per-phase approval gate (resumable, redraft, gitOn no-op)`.

---

### Task 7: Server suite green + gates + inertness proof

- [ ] **Step 1:** `cd server &&` `GOWORK=off go test ./...` · `GOWORK=off go vet ./...` · `make method-check` · `make encapsulation-check` · `make sumtype-check` · `PATH="/opt/homebrew/bin:$PATH" GOWORK=off golangci-lint run --new-from-rev=main ./...`. All green; delta-lint 0-new. Register new public symbols (`SubmitPhaseDecision`, `UpdateReviewPolicy`, `PhaseDecision`, `StageAwaitingApproval`, `ReviewPolicy`, `RequiresHuman`, `ReviewPolicyFromGateIDs`) if `encapsulation-check` names them.
- [ ] **Step 2:** Confirm `Test_Construct_EmptyPolicy_NoGate_WalksAllPhases` passes AND add a non-git test proving the empty-policy path writes no phase records when `gitOn=false` (the "pure vibes = today" guarantee, B6).
- [ ] **Step 3:** Commit any fixups — `test(construction): full-suite green + inertness (incl. non-git) proof`.

---

### Task 8: WebApp — regen API + `useSubmitPhaseDecision` + `useUpdateReviewPolicy` hooks

- [ ] **Step 1:** `cd webApp && npm run gen:api`; verify `grep -n "submit-phase-decision\|review-policy" src/api/schema.ts`.
- [ ] **Step 2:** In `hooks/useConstructionMutations.ts` add `useSubmitPhaseDecision(projectId)` (mirror `useOverrideActivity` `:59-80`; POST `/api/v1/construction/submit-phase-decision/{projectID}/{activityID}`, body `{ phase, decision: phaseDecisionToOrdinal(...), feedback? }`, invalidate `constructionSessionKey`) and `useUpdateReviewPolicy(projectId)` (POST `/api/v1/construction/review-policy/{projectID}`, body `{ gatedPhasesByType }`, invalidate `projectKey`). Add `phaseDecisionToOrdinal` in `api/enums.ts`.
- [ ] **Step 3:** `npm run typecheck && npm run lint` → clean.
- [ ] **Step 4:** Commit — `feat(webapp): submitPhaseDecision + updateReviewPolicy hooks`.

---

### Task 9: WebApp — construction `PhaseGatePanel` + live `PolicyPanel`

- [ ] **Step 1:** Create `components/construction/PhaseGatePanel.tsx` (mirror `design/GatePanel.tsx:133-164` — Approve & continue / Send back, `pending` prop, shows the phase label + `reviewSet`). 
- [ ] **Step 2:** In the construction console screen, when `ConstructionSessionView.stage === StageAwaitingApproval`, render `PhaseGatePanel` wired to `useSubmitPhaseDecision` (mirror `DesignExperience.tsx:188-217`).
- [ ] **Step 3:** Rewrite `PolicyPanel.tsx` to drive from `useUpdateReviewPolicy` (toggling a rule POSTs the per-type gate list) instead of client-only `useState`; remove the "client-only, no backend" comment; add the "edits apply to newly-started activities" note.
- [ ] **Step 4:** `npm run check && npm run build` → clean.
- [ ] **Step 5:** Commit — `feat(webapp): construction PhaseGatePanel + live ReviewPolicy editor`.

---

## Self-Review (completed during authoring)

**Design (founder + architect) coverage:** per-project committed editable ReviewPolicy → Task 1 (model+persist), Task 4 (UpdateReviewPolicy op). Reuse existing gate/engine, no parallel struct/sub-workflow → Task 6 (`ProposeReviews` + conditional suspend; no `checkpointPolicy`). Inert default → Task 1 (zero value), Task 7 Step 2 (empty + non-git proof).

**Architect blocking fixes:** B1 redraft exhaustion keeps awaiting human, never `phaseFailed=true;continue` → Task 6 (`maxPhaseRedrafts` → `continue` the await loop, not the outer variance loop). B2 resumable skip-guard → Task 6 (`state.phaseAlreadyDone`). B3 phase-record Activity wrappers + registration → Task 5 (own task). B4 policy route exists → Task 2 + Task 4 (`UpdateReviewPolicy`). B5 per-exec snapshot from project state → Task 6 Step 3 (read at start, by value) + the "newly-started only" note (Task 9). B6 gitOn no-op → Task 6 (`recordPhaseStarted`/`recordPhaseCompletedIfGit` gated on `gitOn`) + Task 7 non-git test. Non-blocking: `&rs` pointer (Task 6), error-stub not panic (Task 2 Step 3), `ProposeReviews` arch-graph/contracts from the read project (Task 6 Step 3).

**Placeholder scan:** no TBD/TODO; Task 5 Step 1 explicitly flags the fallback (cover via Task 6) if no direct activity unit test exists. Every code step shows real code or cites the exact pattern file:line to mirror.

**Type consistency:** `ReviewPolicy{GatedPhasesByType}` / `RequiresHuman(type,phase)` / `ReviewPolicyFromGateIDs` (Task 1) ↔ `RecordReviewPolicy`/`UpdateReviewPolicy` (Task 4) ↔ read-at-start + `RequiresHuman` (Task 6) ↔ `useUpdateReviewPolicy` (Task 8). `phaseDecisionSignal{Phase,Decision,Feedback}` + `signalPhaseDecision` + `PhaseDecision{Approve,SendBack}` consistent Tasks 2–6 + `phaseDecisionToOrdinal` (Task 8). `SubmitPhaseDecision(projectID,activityID,phase,decision,feedback)` identical across Tasks 2/3/8.
