# Construction Dispatch — Stage 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the construction pump dispatch work again — component identity comes from an authored `componentId` on the activity list instead of a service contract that does not exist yet — and drive the todomvc benchmark through a complete construction run.

**Architecture:** `projectstate.ActivityItem` gains an authored `ComponentID` naming a component in the committed `.systemDesign`. The pump stops consulting `ServiceContracts` entirely and resolves against that field, returning a three-state verdict so an undispatchable activity records a visible terminal failure instead of silently stalling the network. The benchmark harness gains a resume mode so the design-complete state repo can be replayed through construction alone.

**Tech Stack:** Go 1.26 (server, Temporal workflows), TypeScript (webApp SPA, archistrator-bench harness), git-as-DB project state.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-08-08-construction-dispatch-componentid-design.md`. Read §2.5, §4 and §5 before starting — the rule is *not* "coding activities require a component".
- **No fuzzy matching may ship.** Any string-normalizing match of an activity title against component names or contract keys is a plan violation, not an implementation detail.
- **This is Stage 1 only.** The `ACT-*` methodcheck rules, archistrator's own activity-list backfill, the skill change and the method-assets release are Stage 2 and are explicitly out of scope. Stage 1 must leave CI green without them.
- **Go builds run workspace-active.** Do not set `GOWORK=off` for `go build`/`go test` in the server module — `cmd/server` and `cmd/archistrator` depend on an unreleased platform commit that only resolves through the repo-root `go.work`. The `make gen-*` targets set `GOWORK=off` themselves; leave them as they are.
- **Any change to `.aiarch/state/project.json` must be authored by the `system-architect` subagent.** That includes the `FailureReason` enum in Task 3. Never hand-edit that file.
- **Never mutate the original benchmark state repo.** `$TMPDIR/archistrator-bench-scratch/run-20260808T000116Z-1744cf81/state-repo` is the only design-complete corpus in existence and re-creating it costs ~2.5h of agent time. Every task works on a copy.
- **Drain in-flight construction workflows before deploying** the changed pump selection logic (standing doctrine — `constructionActivity` rides child-workflow payloads).

---

### Task 1: Authored `ComponentID` on the activity list

Adds the field and propagates it through the generated OpenAPI + webApp codec chain. `ActivityItem` is hand-written (the contract `$defs` reaches it via an `x-go-type: ActivityItem` escape hatch), so this is a plain Go edit — but `clientgen` *reflects* the Go structs into the merged OAS, so the generated chain moves with it and the CI drift gates will fail if it is not regenerated.

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go:3734` (`ActivityItem`)
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go:6087` (add `Layer.String()` beside `layerNames`)
- Regenerate: `server/api/openapi.yaml`, `webApp/src/contracts/schema.ts`
- Test: `server/internal/resourceaccess/projectstate/access_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `projectstate.ActivityItem.ComponentID string` (JSON `componentId`, omitempty) and `func (l projectstate.Layer) String() string` returning the lowercase layer name (`"client"`, `"manager"`, `"engine"`, `"resourceAccess"`, `"resource"`, `"utility"` — exactly the values in `layerNames`). Task 2 consumes both.

- [ ] **Step 1: Write the failing test**

Add to `server/internal/resourceaccess/projectstate/access_test.go`:

```go
func TestActivityItem_ComponentIDRoundTrips(t *testing.T) {
	in := ActivityList{Activities: []ActivityItem{
		{Name: "C-TLM", Title: "TodoListManager", Coding: true, ComponentID: "todo-list-manager"},
		{Name: "N-STP", Title: "System Test Plan", Coding: false},
	}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// omitempty: the noncoding entry must not carry a misleading empty string.
	if strings.Contains(string(b), `"componentId":""`) {
		t.Fatalf("empty componentId must be omitted, got %s", b)
	}
	if !strings.Contains(string(b), `"componentId":"todo-list-manager"`) {
		t.Fatalf("authored componentId missing from wire form, got %s", b)
	}
	var out ActivityList
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Activities[0].ComponentID != "todo-list-manager" {
		t.Fatalf("want todo-list-manager, got %q", out.Activities[0].ComponentID)
	}
	if out.Activities[1].ComponentID != "" {
		t.Fatalf("want empty, got %q", out.Activities[1].ComponentID)
	}
}

func TestLayerString(t *testing.T) {
	for layer, want := range map[Layer]string{
		LayerClient:         "client",
		LayerManager:        "manager",
		LayerEngine:         "engine",
		LayerResourceAccess: "resourceAccess",
		LayerResource:       "resource",
		LayerUtility:        "utility",
	} {
		if got := layer.String(); got != want {
			t.Fatalf("Layer(%d).String() = %q, want %q", layer, got, want)
		}
	}
}
```

Check the existing imports in `access_test.go` and add `encoding/json` / `strings` only if they are not already there.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd server && go test ./internal/resourceaccess/projectstate/ -run 'TestActivityItem_ComponentIDRoundTrips|TestLayerString' -v
```

Expected: FAIL — `unknown field ComponentID in struct literal` and `layer.String undefined`.

- [ ] **Step 3: Add the field**

In `projectstateaccess.go`, inside `type ActivityItem struct` (after `Title`):

```go
	// ComponentID names the committed System component (systemDesign Components[].id)
	// this activity builds. AUTHORED at Phase-2 draft time — never derived by matching.
	// Its PRESENCE declares the activity STRUCTURAL (Löwy ch.13 Table 13-1, one per
	// architecture component); its absence declares it nonstructural (Table 13-2:
	// harnesses, base services) or noncoding. When present it must resolve to a
	// committed component. A noncoding provisioning activity (R-*) may name the
	// Resource component it provisions.
	ComponentID string `json:"componentId,omitempty"`
```

- [ ] **Step 4: Add `Layer.String()`**

Immediately after the `layerNames` map declaration (~line 6095):

```go
// String returns the canonical lowercase layer name — the same spelling layerNames
// carries on the wire. Defined so callers OUTSIDE this package (the construction
// Manager hydrating an activity's layer from its component) can name a Layer
// without reaching for the unexported map.
func (l Layer) String() string { return enumName(layerNames, l) }
```

Confirm `enumName`'s signature matches this call (`grep -n "func enumName" projectstateaccess.go`) and adjust the call — not the map — if it differs.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd server && go test ./internal/resourceaccess/projectstate/ -run 'TestActivityItem_ComponentIDRoundTrips|TestLayerString' -v
```

Expected: PASS.

- [ ] **Step 6: Regenerate the client layer and the SPA codec**

```bash
cd server && make gen-client
cd ../webApp && npm run gen:api
```

Then confirm the field actually landed in both generated artifacts:

```bash
grep -n "componentId" server/api/openapi.yaml
grep -n -A 9 "ModelActivityItem: {" webApp/src/contracts/schema.ts
```

Expected: `componentId` appears in the OAS `ModelActivityItem` schema and as `componentId?: string;` in `schema.ts`. If it does not appear, **stop** — `clientgen`'s reflection did not pick the field up and the CI drift gate will fail; diagnose before continuing.

- [ ] **Step 7: Verify the drift gates and the full package suite are green**

```bash
cd server && make gen-client-check && make gen-models-check && make gen-internal-tools-check && make gen-uiprofiles-check
cd server && go test ./internal/resourceaccess/projectstate/
cd webApp && npm run typecheck
```

Expected: all pass, no diff from the check targets. These four are the drift gates CI runs (`server-checks.yml`); a reflected field that reaches one generated artifact but not another reds the build.

- [ ] **Step 8: Commit**

```bash
git add server/internal/resourceaccess/projectstate/ server/api/openapi.yaml webApp/src/contracts/schema.ts
git commit -m "feat: authored componentId on ActivityItem + Layer.String()"
```

---

### Task 2: Pump resolves the authored field, and reports a three-state verdict

The heart of the fix. `nextEligibleActivity` stops reading `ServiceContracts`, looks the authored `ComponentID` up in the committed `.systemDesign`, hydrates `Layer` from the component it found, and distinguishes *nothing eligible* from *eligible but undispatchable*. Task 3 wires the blocked verdict to a durable failure record; this task leaves it behaving as a quiet tick so the two changes stay independently reviewable.

**Files:**
- Modify: `server/internal/manager/construction/constructionmanager.go:736-795` (`nextEligibleActivity`)
- Delete: `server/internal/manager/construction/constructionmanager.go:821-884` (`resolveComponentID`, `matchContractKey`, `normalizeIdent`)
- Modify: `server/internal/manager/construction/constructionmanager.go:914` (`hydrateConstructionActivity`)
- Modify: `server/internal/manager/construction/constructionmanager.go:1038,1067,1081,1410` (the `NextEligibleActivity` func-field type + wiring)
- Modify: `server/internal/manager/construction/pumpnextactivity.go:38-135` (`PumpNextActivityWorkflow`, `nextEligible`)
- Test: `server/internal/manager/construction/manager_test.go`

**Interfaces:**
- Consumes: `projectstate.ActivityItem.ComponentID`, `projectstate.Layer.String()` (Task 1).
- Produces:
  - `type pumpVerdict int` with `verdictQuiescent`, `verdictDispatch`, `verdictBlocked`
  - `type pumpSelection struct { Activity constructionActivity; Verdict pumpVerdict; BlockedActivityID string; BlockedReason string }`
  - `func nextEligibleActivity(proj projectstate.Project) pumpSelection`
  - `func hydrateConstructionActivity(activityID string, item projectstate.ActivityItem, comp *projectstate.Component) constructionActivity`
  - `workflows.NextEligibleActivity func(proj projectstate.Project) pumpSelection`

  Task 3 consumes `pumpSelection.BlockedActivityID` and `.BlockedReason`.

- [ ] **Step 1: Write the failing tests**

Add to `server/internal/manager/construction/manager_test.go`. These use the existing `makeCommittedNetwork` / `makeCommittedActivityList` helpers; add a sibling helper for the systemDesign slot next to them (copy the shape of `makeCommittedActivityList` — read it first, it wraps the model in a committed `ArtifactSlot`).

```go
func makeCommittedSystemDesign(comps []projectstate.Component) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model:  &projectstate.System{Components: comps},
	}
}

var todoComponents = []projectstate.Component{
	{ID: "todo-list-manager", Name: "TodoListManager", Layer: projectstate.LayerManager},
	{ID: "todo-owner-client", Name: "TodoOwnerClient", Layer: projectstate.LayerClient},
}

func projWithActivities(acts []projectstate.ActivityItem, deps []projectstate.NetworkDependency) projectstate.Project {
	return projectstate.Project{
		Phase:        projectstate.PhaseConstruction,
		Network:      makeCommittedNetwork(deps),
		ActivityList: makeCommittedActivityList(acts),
		SystemDesign: makeCommittedSystemDesign(todoComponents),
	}
}

// The regression the whole change exists for: a fresh project with NO service
// contracts must still dispatch its first coding activity.
func TestNextEligibleActivity_DispatchesWithNoServiceContracts(t *testing.T) {
	proj := projWithActivities(
		[]projectstate.ActivityItem{{
			Name: "C-TLM", Title: "TodoListManager — settlement manager",
			Coding: true, EffortDays: 13, ComponentID: "todo-list-manager",
		}},
		[]projectstate.NetworkDependency{{Activity: "C-TLM", DependsOn: []string{}}},
	)
	// Deliberately nil: ServiceContracts must play no part in selection.
	proj.ServiceContracts = nil

	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("want verdictDispatch, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ComponentID != "todo-list-manager" {
		t.Fatalf("want componentID todo-list-manager, got %q", sel.Activity.ComponentID)
	}
	if sel.Activity.Layer != "manager" {
		t.Fatalf("want layer hydrated to manager, got %q", sel.Activity.Layer)
	}
	if sel.Activity.Kind != activityKindConstruction {
		t.Fatalf("want activityKindConstruction, got %v", sel.Activity.Kind)
	}
}

// Nonstructural coding (ch.13 Table 13-2) and noncoding activities are LEGAL
// with no componentId and dispatch with an empty component_id.
func TestNextEligibleActivity_ComponentlessActivitiesDispatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		item projectstate.ActivityItem
	}{
		{"nonstructural coding", projectstate.ActivityItem{Name: "I-UC1", Title: "Integrate use case 1", Coding: true}},
		{"noncoding", projectstate.ActivityItem{Name: "N-STP", Title: "System Test Plan", Coding: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := projWithActivities(
				[]projectstate.ActivityItem{tc.item},
				[]projectstate.NetworkDependency{{Activity: tc.item.Name, DependsOn: []string{}}},
			)
			sel := nextEligibleActivity(proj)
			if sel.Verdict != verdictDispatch {
				t.Fatalf("want verdictDispatch, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
			}
			if sel.Activity.ComponentID != "" {
				t.Fatalf("want empty componentID, got %q", sel.Activity.ComponentID)
			}
		})
	}
}

// The ONE blocked condition: a non-empty componentId naming no committed component.
func TestNextEligibleActivity_UnknownComponentIsBlocked(t *testing.T) {
	proj := projWithActivities(
		[]projectstate.ActivityItem{{
			Name: "C-TLM", Title: "TodoListManager", Coding: true, ComponentID: "todo-list-managr",
		}},
		[]projectstate.NetworkDependency{{Activity: "C-TLM", DependsOn: []string{}}},
	)
	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictBlocked {
		t.Fatalf("want verdictBlocked, got %v", sel.Verdict)
	}
	if sel.BlockedActivityID != "C-TLM" {
		t.Fatalf("want blocked activity C-TLM, got %q", sel.BlockedActivityID)
	}
	// The detail must name both the activity and the unresolvable id — it is the
	// only thing an operator sees in the console.
	if !strings.Contains(sel.BlockedReason, "C-TLM") || !strings.Contains(sel.BlockedReason, "todo-list-managr") {
		t.Fatalf("blocked reason must name the activity and the bad id, got %q", sel.BlockedReason)
	}
}

func TestNextEligibleActivity_NothingEligibleIsQuiescent(t *testing.T) {
	proj := projWithActivities(
		[]projectstate.ActivityItem{{Name: "B", Title: "B", Coding: false}},
		[]projectstate.NetworkDependency{{Activity: "B", DependsOn: []string{"A"}}},
	)
	sel := nextEligibleActivity(proj)
	if sel.Verdict != verdictQuiescent {
		t.Fatalf("want verdictQuiescent, got %v", sel.Verdict)
	}
}
```

Add `"strings"` to the test file's imports if it is not already imported.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd server && go test ./internal/manager/construction/ -run 'TestNextEligibleActivity_' -v
```

Expected: FAIL — `undefined: verdictDispatch`, `undefined: pumpSelection`, and the existing `nextEligibleActivity` returning two values.

- [ ] **Step 3: Replace the selection tail and the resolvers**

In `constructionmanager.go`, above `nextEligibleActivity`, add the verdict vocabulary:

```go
// pumpVerdict is the pump's three-state selection outcome. It replaces the former
// (activity, bool) pair, whose false arm conflated "the network is drained" with
// "this activity cannot be dispatched" — the conflation that let a stalled network
// masquerade as a quiescent one for a whole benchmark run.
type pumpVerdict int

const (
	verdictQuiescent pumpVerdict = iota
	verdictDispatch
	verdictBlocked
)

// pumpSelection carries the verdict plus whichever payload it implies: the hydrated
// activity on verdictDispatch, the offending id + operator-facing reason on
// verdictBlocked, nothing on verdictQuiescent.
type pumpSelection struct {
	Activity          constructionActivity
	Verdict           pumpVerdict
	BlockedActivityID string
	BlockedReason     string
}
```

Change the signature to `func nextEligibleActivity(proj projectstate.Project) pumpSelection`, replace every `return constructionActivity{}, false` in its body with `return pumpSelection{Verdict: verdictQuiescent}`, and replace the tail (from `chosen := candidates[0].activity` onward) with:

```go
	chosen := candidates[0].activity
	item := itemByName[chosen]

	// Component identity is AUTHORED (spec §2.5): its presence declares the activity
	// structural, its absence declares it nonstructural or noncoding. ServiceContracts
	// play NO part in selection — requiring one was the chicken-and-egg that stalled
	// every fresh project, since the contract is produced by the detailed-design PHASE
	// of the very activity being selected.
	var comp *projectstate.Component
	if item.ComponentID != "" {
		comp = lookupComponent(proj, item.ComponentID)
		if comp == nil {
			return pumpSelection{
				Verdict:           verdictBlocked,
				BlockedActivityID: chosen,
				BlockedReason: fmt.Sprintf(
					"activity %s names component %q, which is not in the committed systemDesign — amend the committed activityList",
					chosen, item.ComponentID),
			}
		}
	}
	return pumpSelection{Verdict: verdictDispatch, Activity: hydrateConstructionActivity(chosen, item, comp)}
```

Add the lookup helper below it:

```go
// lookupComponent resolves a component id against the committed systemDesign by EXACT
// id match. Returns nil when the slot is uncommitted/unpopulated or no component has
// that id — both are the caller's blocked case. No normalization, no name matching:
// the authored id is the identity (spec §2.1).
func lookupComponent(proj projectstate.Project, id string) *projectstate.Component {
	if proj.SystemDesign.Status != projectstate.ReviewCommitted {
		return nil
	}
	sys, ok := proj.SystemDesign.Model.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	for i := range sys.Components {
		if sys.Components[i].ID == id {
			return &sys.Components[i]
		}
	}
	return nil
}
```

Delete `resolveComponentID`, `matchContractKey` and `normalizeIdent` outright. Confirm nothing else in the package calls them:

```bash
cd server && grep -rn "resolveComponentID\|matchContractKey\|normalizeIdent" internal/manager/construction/
```

Only `manager_test.go` should still reference them at this point.

- [ ] **Step 4: Hydrate `Layer` from the component**

Replace `hydrateConstructionActivity`'s signature and body:

```go
// hydrateConstructionActivity populates a constructionActivity from the activity id +
// its ActivityList item. Coding=true → Construction; Coding=false → Noncoding. comp is
// the resolved systemDesign component, or nil for a componentless (nonstructural or
// noncoding) activity — it supplies BOTH the ComponentID passed to the dispatch as
// component_id AND the Layer, which had no populator before this change and printed
// as an empty string into every PR body.
func hydrateConstructionActivity(activityID string, item projectstate.ActivityItem, comp *projectstate.Component) constructionActivity {
	kind := activityKindNoncoding
	if item.Coding {
		kind = activityKindConstruction
	}
	typ := projectstate.DeriveType(activityID)
	variant := projectstate.DeriveVariant(activityID)
	act := constructionActivity{
		ActivityID:   activityID,
		Kind:         kind,
		EstimateDays: item.EffortDays,
		Phases:       projectstate.ProfileFor(typ, variant).PhaseIDs(),
	}
	if comp != nil {
		act.ComponentID = comp.ID
		act.Layer = comp.Layer.String()
	}
	return act
}
```

- [ ] **Step 5: Update the func-field type and the pump**

In `constructionmanager.go` change both declarations (:1038, :1067) and the wiring (:1081, :1410) from
`func(proj projectstate.Project) (constructionActivity, bool)` to
`func(proj projectstate.Project) pumpSelection`.

In `pumpnextactivity.go`, change `nextEligible` and the call site:

```go
// nextEligible resolves the next selection via the injected helper. With no helper
// wired it is a quiet tick.
func (wf *workflows) nextEligible(proj projectstate.Project) pumpSelection {
	if wf.NextEligibleActivity == nil {
		return pumpSelection{Verdict: verdictQuiescent}
	}
	return wf.NextEligibleActivity(proj)
}
```

and in `PumpNextActivityWorkflow`, replace `activity, eligible := wf.nextEligible(proj)` and its `if !eligible` block with:

```go
	sel := wf.nextEligible(proj)
	switch sel.Verdict {
	case verdictBlocked:
		// Task 3 replaces this arm with a durable RecordActivityFailed. Until then it
		// is loud in the log and ends the cascade — never a silent skip.
		logger.Error("construction pump: activity cannot be dispatched",
			"projectId", string(in.ProjectID),
			"activityId", sel.BlockedActivityID,
			"reason", sel.BlockedReason)
		dispatch = pumpDispatch{Decided: true, Dispatched: false}
		return PumpResult{Dispatched: false}, nil
	case verdictQuiescent:
		logger.Info("no eligible activity — cascade quiescent", "projectId", string(in.ProjectID))
		dispatch = pumpDispatch{Decided: true, Dispatched: false}
		return PumpResult{Dispatched: false}, nil
	case verdictDispatch:
		// fall through to the dispatch below
	}
	activity := sel.Activity
```

Leave the rest of the workflow body unchanged.

- [ ] **Step 6: Rework the existing tests that assert the old contract**

- Delete `TestResolveComponentID` (`manager_test.go:1426`) entirely — the function it covers is gone.
- `TestNextEligibleActivity_Chain` (:1071), `_UncommittedSlots` (:1155), `_RequiresConstructionPhase` (:1197), `_ProjectExportDogfood` (:1243), `_HydratedFields` (:1290): change `got, ok := nextEligibleActivity(proj)` to `sel := nextEligibleActivity(proj)`, assert on `sel.Verdict` (`verdictDispatch` where the old test wanted `ok == true`, `verdictQuiescent` where it wanted `false`), and read fields off `sel.Activity`. Where these fixtures rely on `ServiceContracts` to make an activity resolvable, give the activity an authored `ComponentID` and add the matching component to a committed `SystemDesign` slot instead.
- `TestHydrateConstructionActivity_ServicePhases` (:265) and `_TestingPlanIsThreePhases` (:282): add the third argument. Pass `nil` where the test does not care about component/layer.
- Any test that constructs a `workflows` value with a `NextEligibleActivity` func literal must return `pumpSelection`.

- [ ] **Step 7: Run the full construction suite**

```bash
cd server && go test ./internal/manager/construction/ -v 2>&1 | tail -40
```

Expected: PASS, including the four new tests. If `Test_Pump_NoEligibleActivity_QuietTick` (:2472) or `Test_Pump_EligibleActivity_RunsChild_ThenContinueAsNew` (:2524) fail, their injected helper still returns the old pair — fix the literal, not the workflow.

- [ ] **Step 8: Verify the dead-code and lint gates**

```bash
cd server && make lint && make vet
```

Expected: clean. `unused` will fire if `normalizeIdent` or a helper survived the deletion — remove it rather than silencing it.

- [ ] **Step 9: Commit**

```bash
git add server/internal/manager/construction/
git commit -m "fix: pump resolves authored componentId, reports three-state verdict

Selection no longer consults ServiceContracts — the contract is produced by the
detailed-design phase of the very activity the pump was refusing to dispatch.
An unresolvable component is now a distinct blocked verdict rather than an
indistinguishable quiescent tick."
```

---

### Task 3: A blocked activity records a visible terminal failure

Turns the log line from Task 2 into durable, operator-visible head-state. Requires a new `FailureReason` variant, which lives in the generated enum and therefore in `.aiarch/state/project.json` — **that edit must be authored by the `system-architect` subagent.**

**Files:**
- Modify (via system-architect): `.aiarch/state/project.json` → `.serviceContracts.projectStateAccess.$defs.FailureReason`
- Regenerate: `server/internal/resourceaccess/projectstate/contract.gen.go`
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go:6412` (`FailureReason.String()`) and the doc-comment block at :6391-6409
- Modify: `server/internal/manager/construction/pumpnextactivity.go` (the `verdictBlocked` arm)
- Test: `server/internal/manager/construction/manager_test.go`, `server/internal/resourceaccess/projectstate/access_test.go`

**Interfaces:**
- Consumes: `pumpSelection.BlockedActivityID`, `pumpSelection.BlockedReason` (Task 2).
- Produces: `projectstate.PlanUnresolvable FailureReason = 6`, wire name `"planUnresolvable"`.

- [ ] **Step 1: Have the system-architect author the enum change**

Dispatch the `system-architect` subagent with this task — do not edit `project.json` yourself:

> Add a seventh variant to the `FailureReason` enum in `.aiarch/state/project.json` →
> `.serviceContracts.projectStateAccess.$defs.FailureReason`: ordinal `6`, `x-enum-varnames`
> entry `PlanUnresolvable`. It records that an eligible construction activity could not be
> dispatched because its authored `componentId` names no component in the committed
> systemDesign. Use `recordServiceContract` per the-method-project-state — never a hand-edit.
> Ordinals 0-5 must not move: they are the persisted wire form of every existing failure record.

- [ ] **Step 2: Regenerate and confirm the constant exists**

```bash
cd server && make gen-models
grep -n "PlanUnresolvable" internal/resourceaccess/projectstate/contract.gen.go
```

Expected: `PlanUnresolvable FailureReason = 6` in the const block, with ordinals 0-5 unchanged.

- [ ] **Step 3: Write the failing tests**

In `server/internal/resourceaccess/projectstate/access_test.go`:

```go
func TestFailureReason_PlanUnresolvableWireName(t *testing.T) {
	if got := PlanUnresolvable.String(); got != "planUnresolvable" {
		t.Fatalf("want planUnresolvable, got %q", got)
	}
	// The persisted ordinals of the pre-existing variants are load-bearing.
	if PipelineFailed != 1 || EscalationTimedOut != 5 {
		t.Fatal("existing FailureReason ordinals must not move")
	}
}
```

In `server/internal/manager/construction/manager_test.go`, alongside the existing pump tests. The `fakeProjectState` already captures failure records in its `failed []failCall` slice (`manager_test.go:1627`), and `registerPump` already registers `recordActivityFailed`:

```go
// A blocked activity is recorded as a TERMINAL, app-visible failure — not a silent
// skip, and not a workflow error (a failed Temporal execution is invisible in the
// console; the head-state record IS the escalation).
func Test_Pump_BlockedActivity_RecordsTerminalFailure(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	const reason = `activity C-TLM names component "todo-list-managr", which is not in the committed systemDesign — amend the committed activityList`
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project) pumpSelection {
			return pumpSelection{
				Verdict:           verdictBlocked,
				BlockedActivityID: "C-TLM",
				BlockedReason:     reason,
			}
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if !env.IsWorkflowCompleted() {
		t.Fatal("pump did not complete")
	}
	// The cascade ENDS — no ContinueAsNew, and no error surfaced to Temporal.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a blocked activity must not fail the workflow, got %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("want Dispatched:false, got %+v", res)
	}
	// The durable, operator-visible record.
	if len(ps.failed) != 1 {
		t.Fatalf("want exactly one failure record, got %v", ps.failed)
	}
	got := ps.failed[0]
	if got.activityID != "C-TLM" {
		t.Fatalf("want C-TLM, got %q", got.activityID)
	}
	if got.reason != projectstate.PlanUnresolvable {
		t.Fatalf("want PlanUnresolvable, got %v", got.reason)
	}
	if !strings.Contains(got.detail, "C-TLM") || !strings.Contains(got.detail, "todo-list-managr") {
		t.Fatalf("detail must name the activity and the bad id, got %q", got.detail)
	}
	// Nothing was dispatched: no child recorded an exit.
	if len(ps.exited) != 0 {
		t.Fatalf("no child should have run, got %v", ps.exited)
	}
}
```

Confirm the `failCall` field names (`activityID`, `reason`, `detail`) against the struct near `manager_test.go:1627` before running — match the real names if they differ.

- [ ] **Step 4: Run the tests to verify they fail**

```bash
cd server && go test ./internal/resourceaccess/projectstate/ -run TestFailureReason_PlanUnresolvableWireName -v
cd server && go test ./internal/manager/construction/ -run Test_Pump_BlockedActivity_RecordsTerminalFailure -v
```

Expected: the first FAILs on the wire name (`String()` has no case yet); the second FAILs because the pump records nothing.

- [ ] **Step 5: Add the wire name and its doc comment**

In `projectstateaccess.go`, add to the doc-comment block after the `EscalationTimedOut` comment (~:6409):

```go
// PlanUnresolvable — an eligible activity could not be dispatched because its
// authored componentId names no component in the committed systemDesign. A plan
// defect, terminal until a human amends the activity list — deliberately NOT routed
// through the interventionEngine, which adjudicates variance for work legitimately
// in flight.
```

and a case to `String()` (:6412), before the `FailureReasonUnknown` case:

```go
	case PlanUnresolvable:
		return "planUnresolvable"
```

Update the "six defined FailureReason values" wording in the trailing comment to seven.

- [ ] **Step 6: Wire the pump's blocked arm**

In `pumpnextactivity.go`, replace the Task-2 placeholder `verdictBlocked` arm with:

```go
	case verdictBlocked:
		// LOUD, DURABLE, APP-VISIBLE (spec §4.3). ActivityConstructionFailed is sticky
		// via CoarsePhaseFor, so the operator sees a red node carrying this reason —
		// not a warning buried in a serve log, which is how this defect consumed an
		// entire benchmark run undetected. An empty credential is correct for the local
		// store; the git adapter mints just-in-time (same as the pause path).
		logger.Error("construction pump: activity cannot be dispatched",
			"projectId", string(in.ProjectID),
			"activityId", sel.BlockedActivityID,
			"reason", sel.BlockedReason)
		if _, ferr := wf.Acts.ConstructionTransitionRecordActivityFailed(
			ctx, projectstate.ProjectID(in.ProjectID), proj.Version,
			sel.BlockedActivityID, projectstate.PlanUnresolvable, sel.BlockedReason,
			projectstate.RepoCredential{},
		); ferr != nil {
			return PumpResult{}, ferr
		}
		dispatch = pumpDispatch{Decided: true, Dispatched: false}
		return PumpResult{Dispatched: false}, nil
```

Check the exact invoker signature at `invokers.gen.go:241` and the expected-version argument the neighbouring calls pass (`projectsupervision.go` is the reference for the empty-credential pause path) — match them rather than the sketch above if they differ.

- [ ] **Step 7: Run the tests to verify they pass**

```bash
cd server && go test ./internal/resourceaccess/projectstate/ ./internal/manager/construction/
```

Expected: PASS.

- [ ] **Step 8: Verify the exhaustive-enum and drift gates**

```bash
cd server && make sumtype-check && make lint && make gen-models-check
```

Expected: clean. If the exhaustive linter names a switch over `FailureReason` outside `String()`, add the `PlanUnresolvable` case there too.

**No webApp change is needed for this variant** — already verified: the SPA carries `failureReason` as an opaque string (`webApp/src/contracts/schema.ts:1509`, `webApp/src/contracts/types.ts:416`) with no closed union and no label map, so the new wire value renders without a TS edit.

- [ ] **Step 9: Commit**

```bash
git add .aiarch/state/project.json server/internal/resourceaccess/projectstate/ server/internal/manager/construction/
git commit -m "feat: record PlanUnresolvable when the pump cannot dispatch an activity"
```

---

### Task 4: Backfill the benchmark state repo

The design-complete corpus predates the field, so its coding activities carry no `componentId` and would dispatch componentless. This is a project-state change and **must be authored by the `system-architect` subagent**.

**Files:**
- Create: a working copy of the state repo (never mutate the original)
- Modify (via system-architect): `<copy>/.aiarch/state/project.json` → activity list slot

**Interfaces:**
- Consumes: `ActivityItem.ComponentID` (Task 1).
- Produces: a construction-ready state repo at `$TMPDIR/archistrator-bench-scratch/resume-base/state-repo`, referenced by Tasks 5-7.

- [ ] **Step 1: Take a pristine copy**

```bash
SRC="$TMPDIR/archistrator-bench-scratch/run-20260808T000116Z-1744cf81/state-repo"
DST="$TMPDIR/archistrator-bench-scratch/resume-base/state-repo"
mkdir -p "$(dirname "$DST")" && cp -R "$SRC" "$DST"
cd "$DST" && git status --short && git log --oneline -1
```

Expected: the copy is clean apart from an untracked `.mcp.json`, HEAD is the `AdvancePhase` commit.

- [ ] **Step 2: Confirm the starting state**

```bash
cd "$TMPDIR/archistrator-bench-scratch/resume-base/state-repo" && python3 -c "
import json
p = json.load(open('.aiarch/state/project.json'))
print('phase', p['phase'], 'version', p['version'], 'id', p['id'])
for a in p['slots']['9']['model']['activities']:
    print(f\"{a['name']:14} coding={str(a['coding']):5} componentId={a.get('componentId','')!r}\")
"
```

Expected: `phase 2`, 26 activities, every `componentId` empty.

- [ ] **Step 3: Dispatch the system-architect to author the backfill**

> Backfill the authored `componentId` on the activity list in
> `$TMPDIR/archistrator-bench-scratch/resume-base/state-repo/.aiarch/state/project.json`
> (slot `"9"`), per `docs/superpowers/specs/2026-08-08-construction-dispatch-componentid-design.md`
> §2.5 and §8. Component ids come from the committed systemDesign in slot `"5"`.
> `componentId` marks a STRUCTURAL coding activity; nonstructural coding activities and
> noncoding activities legitimately carry none. The expected reading is
> `C-TLM`→`todo-list-manager`, `C-ACE`→`accounting-engine`, `C-AE`→`admitting-engine`,
> `C-TRA`→`todo-record-access`, `C-TAC`→`todo-agent-client`, and all three of
> `U-SPA-CONSULT`/`U-SPA-AMEND`/`U-SPA-EMBED`→`todo-owner-client`; `R-TRS`→`todo-record-store`
> is optional. That reading is the expected outcome, not a pre-approval — depart from it if
> the committed systemDesign says otherwise, and explain why. Every id must match a
> component `id` EXACTLY. Commit the change in that repo.

- [ ] **Step 4: Verify the backfill**

```bash
cd "$TMPDIR/archistrator-bench-scratch/resume-base/state-repo" && python3 -c "
import json
p = json.load(open('.aiarch/state/project.json'))
ids = {c['id'] for c in p['slots']['5']['model']['components']}
bad = 0
for a in p['slots']['9']['model']['activities']:
    cid = a.get('componentId','')
    if cid and cid not in ids:
        print('UNRESOLVABLE', a['name'], cid); bad += 1
covered = {a.get('componentId') for a in p['slots']['9']['model']['activities'] if a.get('coding') and a.get('componentId')}
code_layers = {'client','manager','engine','resourceAccess'}
for c in p['slots']['5']['model']['components']:
    if c['layer'] in code_layers and c['id'] not in covered:
        print('UNCLAIMED', c['id']); bad += 1
print('OK' if bad == 0 else f'{bad} PROBLEMS')
"
```

Expected: `OK`. The layer values printed by this script are the wire spellings — if the comparison misfires, print one component and match its actual `layer` encoding rather than assuming.

- [ ] **Step 5: Snapshot the backfilled base**

```bash
cd "$TMPDIR/archistrator-bench-scratch/resume-base" && tar czf ../resume-base.tgz state-repo
```

Every later task restores from this tarball, so a botched run never costs the backfill.

---

### Task 5: Benchmark resume mode

`provision.ts` has no reuse hook, but `driveConstruction` (`src/runner/drive.ts:531`) is already separable from steps 1-4. This adds `--resume-from`, which makes `--dry-run` meaningful for the first time: design is already committed, so a dry run exercises construction alone.

**Files:**
- Modify: `../archistrator-bench/src/runner/cli.ts:83-123` (`parseArgs`)
- Modify: `../archistrator-bench/src/stack/provision.ts` (skip `prepareStateRepo` + `runArchistratorInit` when resuming)
- Modify: `../archistrator-bench/src/runner/index.ts` (skip drive steps 1-4 when resuming)
- Test: `../archistrator-bench/src/runner/cli.test.ts` (create if absent — check for the existing test file naming convention first)

**Interfaces:**
- Consumes: the backfilled state repo from Task 4.
- Produces: `bench run <benchmark> --archistrator <path> --resume-from <stateRepoPath> [--dry-run]`, and `ParsedRunArgs.resumeFrom?: string`.

- [ ] **Step 1: Write the failing parser test**

```ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { parseArgs } from "./cli.js";

test("parseArgs accepts --resume-from", () => {
  const parsed = parseArgs([
    "todomvc", "--archistrator", "../archistrator",
    "--resume-from", "/tmp/state-repo", "--dry-run",
  ]);
  assert.equal(parsed.benchmark, "todomvc");
  assert.equal(parsed.resumeFrom, "/tmp/state-repo");
  assert.equal(parsed.dryRun, true);
});

test("parseArgs leaves resumeFrom undefined for a fresh run", () => {
  const parsed = parseArgs(["todomvc", "--archistrator", "../archistrator"]);
  assert.equal(parsed.resumeFrom, undefined);
});
```

Match the import style and test runner the bench repo already uses — check an existing `*.test.ts` before writing this one.

- [ ] **Step 2: Run it to verify it fails**

```bash
cd ../archistrator-bench && npx vitest run src/runner/cli.test.ts
```

Expected: FAIL — `resumeFrom` is not a property of `ParsedRunArgs`. (The repo has `vitest.config.ts`; if the suite is wired to `npm test`, use that instead.)

- [ ] **Step 3: Add the flag**

In `cli.ts`: add `resumeFrom?: string` to `ParsedRunArgs`, a `let resumeFrom: string | undefined;`, a `case "--resume-from": resumeFrom = requireValue(rest, ++i, "--resume-from"); break;` alongside the existing cases, and `resumeFrom` to the returned object. Update the usage comment at :27.

- [ ] **Step 4: Run it to verify it passes**

```bash
cd ../archistrator-bench && npx vitest run src/runner/cli.test.ts
```

Expected: PASS.

- [ ] **Step 5: Seed the state repo from the resume source instead of initializing it**

Add `resumeFrom?: string` to `ProvisionStackOptions` and thread it into the state-repo setup. Where `prepareStateRepo(stateRepoPath)` + `runArchistratorInit(...)` are called today, branch:

```ts
if (opts.resumeFrom) {
  // A resumed run works on a COPY. The design-complete corpus costs ~2.5h of agent
  // time to reproduce and must survive a failed construction run untouched.
  await fs.promises.mkdir(path.dirname(stateRepoPath), { recursive: true });
  await fs.promises.cp(opts.resumeFrom, stateRepoPath, { recursive: true });
} else {
  await prepareStateRepo(stateRepoPath);
  await runArchistratorInit(archistratorBin, stateRepoPath);
}
```

Everything downstream — `buildServeSpec`, the health poll, teardown — is unchanged: `cwd` is already the control surface, and the copied repo carries its own `.aiarch` and `.gitignore`.

- [ ] **Step 6: Skip design when resuming**

`index.ts` calls the whole drive through `deps.driveToBuiltApp`, so the skip belongs in `drive.ts`. Add `resume?: boolean` to `DriveOptions` (`drive.ts:113`), and in `driveToBuiltApp` (`drive.ts:617`) branch straight to step 5 before any project creation:

```ts
  if (opts.resume) {
    // Steps 1-4 (create, configure, Phase 1, Phase 2, SDP) are already committed in
    // the resumed state repo. Re-running them would re-draft artifacts that exist.
    log("resume-construction", { projectId: opts.pid });
    const construction = await driveConstruction(mcp, opts.pid, rs, budget, clock, log, newTickId);
    return {
      outcome: construction.ok ? "succeeded" : "failed",
      operatorActions: rs.operatorActions,
      gaps: rs.gaps,
      activitySummary: construction.summary,
    };
  }
```

Place it immediately after `rs`/`budget`/`newTickId` are constructed and before `createProject`. Match the exact `driveConstruction` argument order and the result-shape fields against the existing call at `drive.ts:658` — copy them, do not retype from this plan.

In `index.ts`, when `resumeFrom` is set, read the project id out of the provisioned copy rather than minting one, and fail fast if the corpus is not construction-ready:

```ts
  let pid = opts.pid;
  if (opts.resumeFrom) {
    const statePath = path.join(handle.stateRepoPath, ".aiarch", "state", "project.json");
    const state = JSON.parse(await fs.promises.readFile(statePath, "utf8"));
    if (state.phase !== 2) {
      throw new Error(
        `--resume-from repo is at phase ${state.phase}, expected 2 (construction) — ` +
          `resume only replays construction against a design-complete corpus`,
      );
    }
    pid = state.id;
  }
```

Pass `pid` and `resume: Boolean(opts.resumeFrom)` through to `driveToBuiltApp`. Leave the `harvest` call untouched — a resumed run seals exactly like a fresh one.

- [ ] **Step 7: Raise the construction budget**

In `drive.ts:162`, raise `constructionTicks` from `4_320` to `17_280` (~24h at the 5s poll interval) — the pump is sequential and the real run is ~100+ agent episodes (spec §8). Leave every other budget untouched.

- [ ] **Step 8: Verify the harness still type-checks and its suite is green**

```bash
cd ../archistrator-bench && npm run typecheck 2>/dev/null || npx tsc --noEmit
cd ../archistrator-bench && npx vitest run
```

Expected: PASS.

- [ ] **Step 9: Commit (in the bench repo)**

```bash
cd ../archistrator-bench
git add src/runner/cli.ts src/runner/index.ts src/runner/drive.ts src/stack/provision.ts src/runner/cli.test.ts
git commit -m "feat: --resume-from for construction-only runs against a design-complete state repo"
```

---

### Task 6: Prove the whole network drains under dry-run

Nothing downstream of dispatch has ever executed in a benchmark. Dry-run makes every submit succeed instantly while keeping the Temporal orchestration, the per-activity lifecycle and the head-state writes real — so the 26-activity network drains in minutes with zero agents. **This task is a debugging loop, not a single edit: expect to find further defects here and fix them.**

**Files:**
- Modify: whatever the drain exposes (most likely `server/internal/manager/construction/constructactivity.go`)
- Test: a regression test in `server/internal/manager/construction/manager_test.go` for each defect found

**Interfaces:**
- Consumes: Tasks 1-5.
- Produces: a state repo whose every activity has reached a terminal phase, and a green dry-run resume run.

- [ ] **Step 1: Build the three local binaries**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator && bash scripts/build-local.sh
```

Expected: `archistrator`, `archistrator-server` and `aiarch-state-mcp` at the repo root. All three must be siblings — discovery resolves them by adjacency.

- [ ] **Step 2: Restore a fresh copy of the backfilled base**

```bash
cd "$TMPDIR/archistrator-bench-scratch" && rm -rf resume-run && mkdir resume-run \
  && tar xzf resume-base.tgz -C resume-run
```

- [ ] **Step 3: Run the dry-run resume**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-bench
npm run bench -- run todomvc --archistrator ../archistrator \
  --resume-from "$TMPDIR/archistrator-bench-scratch/resume-run/state-repo" --dry-run
```

Watch the serve log in a second terminal — its path is printed at startup:

```bash
tail -f "$TMPDIR/archistrator-bench-scratch/<runId>/serve.log"
```

- [ ] **Step 4: Assert the network actually drained**

```bash
python3 -c "
import json, sys
p = json.load(open('$TMPDIR/archistrator-bench-scratch/<runId>/state-repo/.aiarch/state/project.json'))
ac = p.get('activityConstruction') or {}
acts = [a['name'] for a in p['slots']['9']['model']['activities']]
missing = [n for n in acts if n not in ac]
byphase = {}
for n, s in ac.items():
    byphase.setdefault(s.get('phase'), []).append(n)
print('activities', len(acts), 'with status', len(ac), 'missing', missing)
for ph, names in sorted(byphase.items()):
    print(' phase', ph, len(names), names[:6])
"
```

Expected: every activity present and in the Done phase. Read the `ActivityConstructionPhase` enum in `contract.gen.go` for the ordinal that means Done rather than guessing it.

- [ ] **Step 5: Diagnose and fix, one defect at a time**

For each failure: reproduce it as a Go test in `manager_test.go` first, watch it fail, fix it, watch it pass, commit, then re-run from Step 2 with a fresh copy. Do not batch fixes — a dirty state repo makes the next symptom unreadable.

Two specific things to check even if the drain looks green:
- **The review floor.** `ContractTouchesReviewFloor` gates dispatch under *every* preset, vibes included, when a contract operation's name contains `deploy`, `spend` or `schema`. Under dry-run no contracts are produced, so the floor will not fire here — but it can in Task 7. Confirm the driver's gate-clearing path works before relying on it.
- **`FailureReasonPlanUnresolvable` must not appear.** If any activity records it, the Task-4 backfill is incomplete — go back, do not work around it.

- [ ] **Step 6: Confirm the app renders the finished state**

Start a server against the drained repo and check the construction console in the browser:

```bash
cd "$TMPDIR/archistrator-bench-scratch/<runId>/state-repo" \
  && ARCHISTRATOR_CONSTRUCTION_DRYRUN=true ARCHISTRATOR_AUTH_DEV_MODE=true \
     /Users/davidmarne/mixofrealitystudio/archistrator/archistrator serve --port 8099 --skip-auth-check
```

Open `http://127.0.0.1:8099/` and confirm every construction activity renders complete. This is half the acceptance criterion — verify it visually, do not infer it from the JSON.

- [ ] **Step 7: Commit any fixes**

```bash
git add server/ && git commit -m "fix: <the specific defect the drain exposed>"
```

---

### Task 7: The real metrics run

**Files:**
- Create: `../archistrator-bench/runs/todomvc/<runId>/` (harvested by the harness)
- Create: `../archistrator-bench/analysis/todomvc/<runId>/metrics.json`

**Interfaces:**
- Consumes: a green dry-run drain (Task 6).
- Produces: the sealed run archive and the metrics that close AC iteration 0.

- [ ] **Step 1: Restore another fresh copy of the backfilled base**

```bash
cd "$TMPDIR/archistrator-bench-scratch" && rm -rf real-run && mkdir real-run \
  && tar xzf resume-base.tgz -C real-run
```

- [ ] **Step 2: Confirm the binaries match the committed fix**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator && git status --short && git log --oneline -1
bash scripts/build-local.sh
```

The recorded `archistratorCommit` must match the running binary, and the binary must carry the SP1 capture seam or no traces are emitted. Commit any stragglers before building.

- [ ] **Step 3: Launch the run**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-bench
npm run bench -- run todomvc --archistrator ../archistrator \
  --resume-from "$TMPDIR/archistrator-bench-scratch/real-run/state-repo"
```

No `--dry-run`. Expect 10-20h of sequential wall clock (spec §8) — this needs babysitting, not a single sitting.

- [ ] **Step 4: Watch for the rate-limit wall**

```bash
tail -f "$TMPDIR/archistrator-bench-scratch/<runId>/serve.log" | grep -iE "quota|capacity|limit|failed"
```

The executor has no rate-limit-aware retry: `ErrCapacity` maps to `fwra.QuotaExhausted`, whose `DefaultRetryable()` is false, so a usage-limit exhaustion will mark activities Failed. The spec deliberately defers designing for this. When it happens, **capture exactly how it surfaces** — the log line, the recorded `FailureReason`, the activity phase — and bring that evidence back before fixing anything.

- [ ] **Step 5: Confirm the acceptance criterion**

Re-run Task 6 Steps 4 and 6 against the real run's state repo: every activity terminal and Done in the JSON, and every construction task rendering complete in the app.

- [ ] **Step 6: Extract and publish the metrics**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-bench
npm run bench -- extract <runId> --benchmark todomvc
npm run bench -- detect todomvc
npm run bench -- dashboard build
npm run dashboard:dev   # http://localhost:5173/
```

- [ ] **Step 7: Record what the run measured, and what it did not**

Append a short note to `docs/bugs/construction-dispatch-no-contract-key.md` marking it fixed, citing the run id. State plainly that this run resumed a design-complete corpus, so its metrics cover construction only — and that per spec §10 the `I-UC*` base-list reshape should land before these numbers are treated as a baseline.

---

## Notes for the executor

**What Stage 1 deliberately does NOT do**, so you do not "helpfully" add it:

- No `ACT-COMPONENT-REQUIRED`, `ACT-UNKNOWN-COMPONENT` promotion, or `ACT-NONSTRUCTURAL-CODING` rule. Stage 1 ships no new methodcheck rules — that is what keeps CI green while archistrator's own activity list is still unbackfilled.
- No change to archistrator's own `.aiarch/state/project.json` activity list.
- No `the-method-activity-list` skill edit and no method-assets release.
- No `componentId` column in the webApp activity table.
- No parallel dispatch, and no rate-limit-aware retry.

**If a task's premise turns out to be wrong** — a signature that does not match, a helper that does not exist, a test harness shaped differently than described — fix the plan's assumption against the real code and say so in the commit message. Do not force the code to match the plan.
