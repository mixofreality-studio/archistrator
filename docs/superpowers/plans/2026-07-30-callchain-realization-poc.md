# Call-Chain Realization — PoC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the proof-of-concept vertical slice of the call-chain realization spec (`docs/superpowers/specs/2026-07-30-call-chain-realization-design.md`): step-keyed `DynamicView` model, `CC-*` validation (advisory), the wired-up walkthrough/dynamic-lens UI, and archistrator's own `drive-system-design` use case realized — then STOP for founder Chrome QA.

**Architecture:** The model change starts at its source of truth — `.serviceContracts["projectStateAccess"]` inside `.aiarch/state/project.json` — and regenerates outward (server `contract.gen.go` → OAS → webApp `schema.ts`). New `CC-*` rules land in platform `framework-go/methodcheck` (consumed locally via a PoC-temporary `replace` directive) and are mirrored in the app's `designhealth` live tier. The existing `UseCaseWalkthrough` and `DynamicViewFlow` UIs consume the realization; no new shell.

**Tech Stack:** Go 1.25/1.26 (server + platform), TypeScript/React/MUI/React-Flow (webApp), node `--test` unit tests, Playwright (uitests), git-as-DB project.json.

## Global Constraints

- All work on branch `callchain-realization` (already created). Do NOT push — the PoC carries a local-only `replace` directive (Task 6) that would red CI; the final phase (post-QA plan) swaps it for a released pin.
- **PoC severity:** every new `CC-*` rule AND the retargeted `DV-STATIC-COVERAGE`/`DV-REL-COVERAGE` emit `SeverityWarning` (advisory). The flip to Error is the post-QA plan's job. Express this as one constant per tier (`ccGateSeverity`) so the flip is a two-line change.
- Server/platform Go commands run with the Makefile defaults (they force `GOWORK=off`); ad-hoc `go test` for platform modules runs from the module dir with `GOWORK=off`.
- webApp: `exactOptionalPropertyTypes` + `noUncheckedIndexedAccess` are on — write `{...(x !== undefined ? { x } : {})}` and `?? fallback` idioms. Unit tests are node built-in runner, pure leaf modules, explicit `.ts` import extensions, `/// <reference types="node" />`, `void test(...)` idiom. `npm run check` (typecheck+lint+format+test) must pass before any task is "done".
- Do not add files to `LEGACY_COMPONENTS_HOOKS_FILES` in `eslint.platform.config.js`. Components stay pure (components→components/contracts/utilities only).
- New JSON wire names are camelCase: `steps`, `activityNodeId`, `calls`, `timeEvent`, `acceptEvent`.
- Commit after every task with the message forms given in the task; project.json state edits use the `systemDesign: … (design amendment)` convention.

---

### Task 1: Model source of truth — `$defs` + hand types + regen

**Files:**
- Modify: `.aiarch/state/project.json` → `.serviceContracts["projectStateAccess"].contract.$defs` only (NOT slot 5 yet)
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` (hand-defined `ActivityNode` ~:4032, `NewSystem` ~:3972, `DynamicView` doc comments ~:3941)
- Generated: `server/internal/resourceaccess/projectstate/contract.gen.go`, `.../fake/fake.gen.go`

**Interfaces (produced — every later task consumes these exact shapes):**

```go
// contract.gen.go (generated from $defs):
type DynamicView struct {
    UseCaseID string     `json:"useCaseId"`
    Key       string     `json:"key"`
    Title     string     `json:"title"`
    Steps     []CallStep `json:"steps"`
}
type CallStep struct {
    ActivityNodeID string         `json:"activityNodeId"`
    Calls          []Relationship `json:"calls"`
}
// ActivityNodeKind gains (appended AFTER NodeInterruptEdge — iota order is load-bearing):
//   NodeTimeEvent   (wire "timeEvent")
//   NodeAcceptEvent (wire "acceptEvent")
```

```go
// projectstateaccess.go (hand-defined) — ActivityNode loses LinkedCompID:
type ActivityNode struct {
    ID            string           `json:"id"`
    Kind          ActivityNodeKind `json:"kind"`
    Label         string           `json:"label"`
    RoleName      string           `json:"roleName"`
    LinkedActorID *string          `json:"linkedActorId"`
}
```

- [ ] **Step 1: Edit `$defs` in project.json.** In `.serviceContracts["projectStateAccess"].contract.$defs`: replace `DynamicView`'s `participants` + `edges` properties with `steps` (array of `$ref: CallStep`, required); add `$defs.CallStep` = object `{activityNodeId: string, calls: array of $ref Relationship}` (both required); in `$defs.ActivityNode` remove the `linkedCompId` property and add `"timeEvent"`, `"acceptEvent"` to the `kind` enum list (after `"interruptEdge"`). Match the JSON-schema idiom of the neighboring `$defs` entries exactly (look at `$defs.Relationship` for the reference style).
- [ ] **Step 2: Edit the hand types.** In `projectstateaccess.go`: delete the `LinkedCompID *ComponentID` field from `ActivityNode` (and its doc-comment line); extend `ActivityNodeKind`'s `BookEnumerated()` comment noting the two UML event kinds are NOT book-enumerated (they sort after `NodeInterruptEdge`, so `k <= NodeNote` stays correct); update the `DynamicView` doc comment block (~:3941-3952) to describe steps-keyed realizations ("one CallStep per realized activity node; participants derived; endpoints resolve to a Component.ID or an owning-use-case Actor.ID"). In `NewSystem` (~:3972), update any validation that iterates `dv.Participants`/`dv.Edges` to iterate `dv.Steps[].Calls` instead (grep `Participants` and `\.Edges` inside the file first — only touch dynamic-view code paths, not activity edges).
- [ ] **Step 3: Regenerate.** Run: `cd server && make gen-models`. Expected: `contract.gen.go` shows the Task-1 Interfaces shapes; `git diff` contains no unrelated churn.
- [ ] **Step 4: Compile the package.** Run: `cd server && GOWORK=off go build ./internal/resourceaccess/projectstate/...`. Fix any residual references to `LinkedCompID`/`Participants` **inside this package only** (cross-package fallout is Task 2).
- [ ] **Step 5: Commit.**
```bash
git add .aiarch/state/project.json server/internal/resourceaccess/projectstate/
git commit -m "model(projectstate): step-keyed DynamicView realization + UML event node kinds"
```

---

### Task 2: Server-wide compile fallout + read-back findings

**Files:**
- Modify: `server/internal/manager/systemdesign/coauthorartifact.go` (`useCaseDynamicFindings` ~:1650)
- Modify: every other server file `grep -rln 'LinkedCompID\|\.Participants' server/internal server/cmd` reports (expect: systemdesign manager encode/validation paths)
- Test: existing suites

**Interfaces:**
- Consumes: Task-1 `DynamicView.Steps` / `CallStep`.
- Produces: a compiling server; `useCaseDynamicFindings` unchanged in RULE ID (`USECASE-DYNAMIC-MISSING`) and semantics (view presence per use case) but reading the new shape.

- [ ] **Step 1: Find all fallout.** Run: `cd server && GOWORK=off go build ./... 2>&1 | head -50`. List every failing file.
- [ ] **Step 2: Fix each reference.** `useCaseDynamicFindings` joins `DynamicViews[].UseCaseID` against committed use-case IDs — the join field survives, so most fixes are mechanical (`dv.Edges` → flatten `dv.Steps[].Calls`, `dv.Participants` → derive `participantIDs(dv)`). Where a derived participant set is needed, add ONE helper in `projectstate`:
```go
// ParticipantIDs derives the distinct endpoint ids of a realization's calls, in
// first-appearance order. Actor ids are included; callers filter as needed.
func ParticipantIDs(dv DynamicView) []string {
    seen := map[string]bool{}
    var out []string
    for _, s := range dv.Steps {
        for _, c := range s.Calls {
            for _, id := range []string{c.From, c.To} {
                if !seen[id] { seen[id] = true; out = append(out, id) }
            }
        }
    }
    return out
}
```
- [ ] **Step 3: Full build + short tests.** Run: `cd server && GOWORK=off go build ./... && GOWORK=off make test-short`. Expected: PASS. (The committed project.json still has old-shape views — they decode as zero-step views because Go's decoder ignores unknown JSON keys; that is the spec's tolerant decode, and nothing may crash on it.)
- [ ] **Step 4: Commit.**
```bash
git add -A server/
git commit -m "server: migrate dynamic-view consumers to step-keyed realizations"
```

---

### Task 3: methodcheck model types + event kinds + UC-ACTDIAG

**Files:**
- Modify: `archistrator-platform/framework-go/methodcheck/project.go:156-253` (parallel model)
- Modify: `archistrator-platform/framework-go/methodcheck/rules.go` (UC-ACTDIAG impl ~:403)
- Test: `archistrator-platform/framework-go/methodcheck/rules_test.go` + siblings

**Interfaces (produced):**
```go
// project.go — string-typed wire model:
type DynamicView struct {
    UseCaseID string     `json:"useCaseId"`
    Key       string     `json:"key"`
    Title     string     `json:"title"`
    Steps     []CallStep `json:"steps"`
}
type CallStep struct {
    ActivityNodeID string         `json:"activityNodeId"`
    Calls          []Relationship `json:"calls"`
}
type ActivityNode struct {
    ID            string `json:"id"`
    Kind          string `json:"kind"`
    Label         string `json:"label"`
    RoleName      string `json:"roleName"`
    LinkedActorID string `json:"linkedActorId"` // "" when absent
}
const (
    kindTimeEvent   = "timeEvent"
    kindAcceptEvent = "acceptEvent"
)
```
- Consumes: nothing from earlier tasks (platform module is independent).
- Produces for Tasks 4–5: the shapes above + `UseCase.Actors []Actor` and `UseCase.Trigger string` already present in `project.go`.

- [ ] **Step 1: Write failing tests for the decode + UC-ACTDIAG event entries.** In `rules_test.go` (or a new `rules_events_test.go`):
```go
func TestDecode_StepKeyedDynamicView(t *testing.T) {
    raw := []byte(`{"slots":{"5":{"kind":5,"status":2,"model":{"components":[],"relationships":[],
      "dynamicViews":[{"useCaseId":"uc","key":"k","title":"T",
        "steps":[{"activityNodeId":"n1","calls":[{"from":"a","to":"b","mode":"sync","label":"x"}]}]}]}}}}`)
    p, err := DecodeProject(raw)
    if err != nil { t.Fatalf("decode: %v", err) }
    dv := p.System.DynamicViews[0]
    if len(dv.Steps) != 1 || dv.Steps[0].ActivityNodeID != "n1" || len(dv.Steps[0].Calls) != 1 {
        t.Fatalf("step-keyed shape not decoded: %+v", dv)
    }
}

func TestUCActDiag_EventEntryWithoutIncomingEdgeIsLegal(t *testing.T) {
    // a timeEvent node with NO incoming edge is a standard UML alternative entry
    c := CoreUseCases{Decisions: []UseCaseDecision{{UseCase: UseCase{
        Name: "Sweep", Classification: classCore, Trigger: "timer",
        Activity: &ActivityDiagram{
            Nodes: []ActivityNode{
                {ID: "tick", Kind: kindTimeEvent, Label: "period elapses"},
                {ID: "act", Kind: "action", Label: "run sweep"},
                {ID: "end", Kind: "end"},
            },
            Edges: []ActivityEdge{{From: "tick", To: "act"}, {From: "act", To: "end"}},
        },
    }}}}
    if hasRuleFindings(validateCoreUseCases(c), ruleUCActDiag) {
        t.Fatalf("edge-less event entry must be well-formed")
    }
}
```
(Adapt the second test's fixture/rule-entry names to the actual UC-ACTDIAG impl at `rules.go:403` — read it first; the assertion is: no start-node-reachability/orphan finding for an event node acting as entry.)
- [ ] **Step 2: Run to verify failure.** `cd ../archistrator-platform/framework-go && GOWORK=off go test ./methodcheck/ -run 'TestDecode_StepKeyed|TestUCActDiag_EventEntry' -v`. Expected: compile failure (`Steps` undefined) — that IS the failing state.
- [ ] **Step 3: Implement.** Rewrite `DynamicView`/add `CallStep` in `project.go` per Interfaces; add `RoleName`/`LinkedActorID` to its `ActivityNode`; add the two kind consts next to the existing kind consts; relax UC-ACTDIAG so nodes of kind `timeEvent`/`acceptEvent` count as legal entry roots (reachability seeds = start nodes ∪ event nodes; an event node with no incoming edge is NOT an orphan).
- [ ] **Step 4: Fix in-module fallout.** `GOWORK=off go build ./...` in framework-go; every rule referencing `dv.Participants`/`dv.Edges` fails to compile — for THIS task, patch them minimally to compile against a derived form (add the helper below to `rules_dynamic.go`; Task 5 does the real retargeting):
```go
// stepCalls flattens a realization's fragments in authored step order.
func stepCalls(dv DynamicView) []Relationship {
    var out []Relationship
    for _, s := range dv.Steps { out = append(out, s.Calls...) }
    return out
}
// participantIDs derives the distinct endpoint ids in first-appearance order.
func participantIDs(dv DynamicView) []string {
    seen := map[string]bool{}
    var out []string
    for _, s := range dv.Steps {
        for _, c := range s.Calls {
            for _, id := range []string{c.From, c.To} {
                if !seen[id] { seen[id] = true; out = append(out, id) }
            }
        }
    }
    return out
}
```
- [ ] **Step 5: Run full module tests; triage.** `GOWORK=off go test ./methodcheck/...`. Existing DV-rule tests that construct old-shape fixtures now fail to compile — update those FIXTURES to `Steps: []CallStep{{ActivityNodeID: "n", Calls: []Relationship{...}}}` keeping each test's intent identical. Expected: all green plus the two new tests.
- [ ] **Step 6: Commit (platform repo).**
```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
git checkout -b callchain-realization
git add framework-go/methodcheck/
git commit -m "methodcheck: step-keyed DynamicView model + UML event node kinds"
```

---

### Task 4: methodcheck path walker

**Files:**
- Create: `archistrator-platform/framework-go/methodcheck/activitypaths.go`
- Test: `archistrator-platform/framework-go/methodcheck/activitypaths_test.go`

**Interfaces (produced — Task 5 consumes verbatim):**
```go
// pathEntry describes one enumeration root of an activity diagram.
type pathEntry struct {
    NodeID string
    Kind   string // "start", "timeEvent", "acceptEvent"
}

// activityPaths enumerates every entry→end node-id path of a: entries are start
// nodes plus event nodes (any event node is a root, wherever it sits); decision/
// switch branches enumerate; loops (back-edges) are traversed at most once; fork
// branches are concatenated in declared edge order (fork-without-join supported);
// paths terminate at end nodes or when no outgoing edge remains.
func activityPaths(a ActivityDiagram) []struct {
    Entry pathEntry
    Nodes []string // node ids in walk order, Entry.NodeID first
}
```

- [ ] **Step 1: Write failing tests.** `activitypaths_test.go`, covering exactly: (a) linear start→action→end yields 1 path; (b) one decision with 2 guarded branches yields 2 paths; (c) a back-edge (loop) is taken at most once — a diagram with `decision → action → decision` yields finite paths and the loop body appears at most twice in any path; (d) fork with 2 branches and no join yields ONE path with both branches concatenated in declared edge order; (e) two end nodes both terminate paths; (f) an edge-less `timeEvent` node is its own entry — a diagram with a start-rooted path AND a timeEvent-rooted path yields both, with `Entry.Kind` distinguishing them. Build fixtures inline as `ActivityDiagram{Nodes: […], Edges: […]}` literals (see Task 3 test for the idiom).
```go
func TestPaths_DecisionBranches(t *testing.T) {
    a := ActivityDiagram{
        Nodes: []ActivityNode{{ID: "s", Kind: "start"}, {ID: "d", Kind: "decision"},
            {ID: "x", Kind: "action"}, {ID: "y", Kind: "action"}, {ID: "e", Kind: "end"}},
        Edges: []ActivityEdge{{From: "s", To: "d"}, {From: "d", To: "x", Guard: "[yes]"},
            {From: "d", To: "y", Guard: "[no]"}, {From: "x", To: "e"}, {From: "y", To: "e"}},
    }
    got := activityPaths(a)
    if len(got) != 2 { t.Fatalf("want 2 paths, got %d: %v", len(got), got) }
}
```
- [ ] **Step 2: Run to verify failure.** `GOWORK=off go test ./methodcheck/ -run TestPaths -v` → compile error (`activityPaths` undefined).
- [ ] **Step 3: Implement.** DFS from each entry; per-path visited-edge set forbids re-traversing an edge (loop-once); at a fork node, do NOT branch — append the walks of each outgoing edge sequentially in declared order into the same path; at decision/switch, branch into one path per outgoing edge; cap total paths at 512 per diagram (return what was enumerated — Task 5's rule notes the cap in its message when hit).
- [ ] **Step 4: Run to verify pass.** Same command. Expected: PASS.
- [ ] **Step 5: Commit.** `git add framework-go/methodcheck/activitypaths.go framework-go/methodcheck/activitypaths_test.go && git commit -m "methodcheck: activity-graph path enumeration for call-chain validation"`

---

### Task 5: methodcheck CC-* rules + DV retargeting + retirements

**Files:**
- Create: `archistrator-platform/framework-go/methodcheck/rules_callchain.go`
- Test: `archistrator-platform/framework-go/methodcheck/rules_callchain_test.go`
- Modify: `rules_dynamic.go`, `rules_statevalidation.go` (delete DV-CHAIN-CONNECTED + DV-PART-*), `rules_system.go` (no change to ARCH-CHAINCOV/USECASE-DYNAMIC-MISSING), `rules.go:86` (`validateArchitecture` append chain), `rules_appc.go` (retarget over `stepCalls`)

**Interfaces:**
- Consumes: Task-3 shapes, Task-4 `activityPaths`.
- Produces: rule IDs `CC-STEP-NODE`, `CC-STEP-UNIQUE`, `CC-COVERAGE`, `CC-STEP-NONEMPTY`, `CC-ENDPOINT-RESOLVES`, `CC-ACTOR-EDGE`, `CC-ACTOR-LANE`, `CC-TRIGGER-EVENT`, `CC-PATH-CONNECTED`; `const ccGateSeverity = SeverityWarning // PoC-advisory; post-QA plan flips to SeverityError`.

- [ ] **Step 1: Write failing tests — one Test func per behavior (this package's idiom), covering every rule.** Fixtures: a helper `sysWith(dvs ...DynamicView) System` (components: person-free roster `web-client` Client, `mgr` Manager, `ra` ResourceAccess, `res` Resource + the static relationships web-client→mgr, mgr→ra, ra→res sync) and `ucWith(trigger string, nodes []ActivityNode, edges []ActivityEdge, actors ...Actor) CoreUseCases`. Minimum test list (names are contract):
  - `TestCC_StepNodeDanglingFires` / `TestCC_StepUniqueDuplicateFires`
  - `TestCC_CoverageActionWithoutStepFires` / `TestCC_CoverageStepOnMergeFires` / `TestCC_CoverageDecisionStepOptional` (no finding either way)
  - `TestCC_StepNonemptyFires` (step with `Calls: []`)
  - `TestCC_EndpointResolvesUnknownFires` / `TestCC_EndpointActorResolves` (actor id from `UseCase.Actors` passes)
  - `TestCC_ActorEdgeNonClientFires` (actor→mgr) / `TestCC_ActorEdgeQueuedFires` / `TestCC_ActorToActorFires`
  - `TestCC_ActorLaneMismatchFires` (node `LinkedActorID:"u"` but step's calls never touch actor `u`)
  - `TestCC_TriggerEventTimerWithoutTimeEventFires` / `TestCC_TriggerEventClientActionWithEventEntryFires`
  - `TestCC_PathConnected_HappyChainPasses` (actor→client, client→mgr on first action; mgr→ra on second)
  - `TestCC_PathConnected_DisconnectedFromFires` (second step's call From a component never seen)
  - `TestCC_PathConnected_MidChainActorReentryPasses` (later step rooted actor→client)
  - `TestCC_PathConnected_TimeEventRootClientToManagerPasses` (timeEvent step's first call `sched-client→mgr`)
  - `TestCC_AllRulesAreWarningSeverityInPoC` (assert `findingSeverity(...) == SeverityWarning` for a firing CC rule)
- [ ] **Step 2: Run to verify failures.** `GOWORK=off go test ./methodcheck/ -run TestCC -v` → compile errors.
- [ ] **Step 3: Implement `rules_callchain.go`.** Skeleton:
```go
const (
    ruleCCStepNode      RuleID = "CC-STEP-NODE"
    ruleCCStepUnique    RuleID = "CC-STEP-UNIQUE"
    ruleCCCoverage      RuleID = "CC-COVERAGE"
    ruleCCStepNonempty  RuleID = "CC-STEP-NONEMPTY"
    ruleCCEndpoint      RuleID = "CC-ENDPOINT-RESOLVES"
    ruleCCActorEdge     RuleID = "CC-ACTOR-EDGE"
    ruleCCActorLane     RuleID = "CC-ACTOR-LANE"
    ruleCCTriggerEvent  RuleID = "CC-TRIGGER-EVENT"
    ruleCCPathConnected RuleID = "CC-PATH-CONNECTED"
)

// ccGateSeverity is the PoC-advisory severity for the correspondence family and
// the two coverage retargets. The post-QA rollout flips it to SeverityError.
const ccGateSeverity = SeverityWarning

// callChainRules validates every use case's realization against its activity
// diagram: coverage, endpoint resolution, actor legality, trigger alignment,
// and per-path chain connectivity (spec §4).
func callChainRules(s System, c CoreUseCases) []Finding
```
Per-rule notes: step-eligibility sets `mustHaveStep = {action, timeEvent, acceptEvent}`, `mayHaveStep = {decision, switch}`, all others illegal. Endpoint resolution: component index ∪ per-use-case actor index; an id in BOTH → ambiguous finding. `CC-ACTOR-EDGE`: for a call with ≥1 actor endpoint — other endpoint must be `kindClient` layer component, `Mode == modeSync`, both-actors → finding. `CC-ACTOR-LANE`: node with `LinkedActorID != ""` and a step → that actor id must appear among the step's call endpoints. `CC-TRIGGER-EVENT`: timer → ≥1 `timeEvent` node with no incoming edge; busMessage → ≥1 `acceptEvent` entry; clientAction → no event-node entry. `CC-PATH-CONNECTED`: for each `activityPaths` path, walk realized steps in path order; maintain `reached` set; legal fragment roots re-seed: actor→Client call always legal; for a path whose `Entry.Kind == "timeEvent"` the first call must be Client→Manager; `acceptEvent` → first call `Mode == "queued"` into a Manager; `clientAction` initial-entry paths → first call actor→Client; otherwise a call's `From` must ∈ reached. Location grammar: `loc(i+1, "dynamicView "+viewLabel(dv)+" step "+step.ActivityNodeID)` for step-scoped findings; `"useCase "+uc.ID` for coverage/trigger findings.
Retargets in the same step: rewrite `DV-EDGE-ENDS`, `DV-EDGE-IN-MODEL` (match `(from,to,mode)`, skip calls with an actor endpoint), `DV-MODE`, `DV-SINGLE-MGR` (over union `stepCalls`; multiple Clients entering ONE manager stays legal), `APPC-INT-*` over `stepCalls(dv)`; `DV-STATIC-COVERAGE`/`DV-REL-COVERAGE` compute over unions and emit `ccGateSeverity`; DELETE `dvChainConnected`, `ruleDVPartExist`, `ruleDVPartUsed` (+ their tests); append `callChainRules(s, c)` in `validateArchitecture`.
- [ ] **Step 4: Run the full module.** `GOWORK=off go test ./methodcheck/...` → all green.
- [ ] **Step 5: Commit.** `git add framework-go/methodcheck/ && git commit -m "methodcheck: CC-* call-chain correspondence family (PoC-advisory) + DV retargeting"`

---

### Task 6: Server gate wiring — replace directive + CC attribution

**Files:**
- Modify: `server/go.mod` (PoC-temporary replace)
- Modify: `server/cmd/aiarch-state-mcp/staleness.go` (`ruleSlotAttributionPrefixes` ~:211)
- Test: run gates

**Interfaces:**
- Consumes: Task-5 rule ids via the replaced local framework-go.
- Produces: local gates (`make method-check`, `validate` one-shot) evaluating CC-* against the working tree.

- [ ] **Step 1: Add the replace.** In `server/go.mod`, after the require block:
```
// PoC-TEMPORARY (callchain-realization): consume local methodcheck CC-* rules.
// The post-QA rollout releases framework-go and swaps this for a version pin.
replace github.com/mixofreality-studio/archistrator-platform/framework-go => ../../archistrator-platform/framework-go
```
Run `cd server && GOWORK=off go mod tidy && GOWORK=off go build ./...`.
- [ ] **Step 2: Add CC attribution.** In `staleness.go`'s `ruleSlotAttributionPrefixes`, next to the `{"DV-", …}` entry:
```go
{"CC-", projectstate.KindSystem, attribSlot},
```
And extend the comment at `staleness.go:26` ("USECASE-DYNAMIC-MISSING and the DV-* family are System-attributed…") to name CC-*.
- [ ] **Step 3: Run the gates on the current (old-shape) state.** `cd server && make method-check` and `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System`. Expected: PASS with advisory CC findings (all 16 views decode zero-step → `CC-COVERAGE` warnings per use case; zero Errors). If any Error fires, the severity constant or a retarget is wrong — fix before proceeding.
- [ ] **Step 4: Commit.** `git add server/go.mod server/go.sum server/cmd/aiarch-state-mcp/staleness.go && git commit -m "server: PoC replace for local methodcheck + CC-* slot attribution"`

---

### Task 7: designhealth — new shape + CC mirror

**Files:**
- Modify: `server/internal/utility/designhealth/parse.go:107-119`, `designhealth.go` (rule ids + Evaluate), new `rules_callchain.go` in the package
- Modify: `archistrator-platform/framework-go-projectmodel/system.go` — NO change (deliberately does not parse dynamicViews); designhealth's own tolerant slice does the work
- Test: `server/internal/utility/designhealth/designhealth_test.go`

**Interfaces:**
- Consumes: wire JSON (slot 5 new shape + slot 4 activity).
- Produces (webApp consumes via GetDesignHealth): exported ids `RuleCCCoverage methodcheck.RuleID = "CC-COVERAGE"` etc. (all nine, exported, same strings as Task 5); **section grammar** `"useCase "+ucID` (coverage/trigger) and `"dynamicView "+dvLabel+" step "+activityNodeID` (step-scoped) — the webApp joins on these exact strings.

- [ ] **Step 1: Extend the tolerant decode.** In `parse.go`:
```go
type dynamicView struct {
    UseCaseID string     `json:"useCaseId"`
    Key       string     `json:"key"`
    Steps     []viewStep `json:"steps"`
}
type viewStep struct {
    ActivityNodeID string     `json:"activityNodeId"`
    Calls          []viewEdge `json:"calls"`
}
```
(keep `viewEdge` as-is) and add a slot-4 activity slice to `parseSlots` (nodes: `id`, `kind`, `label`, `roleName`, `linkedActorId`; edges: `from`, `to`, `guard`; use-case: `id`, `trigger`, `actors[]{id}`) — mirror the existing slot-5 parsing style.
- [ ] **Step 2: Write failing table cases.** Extend `TestNegativeFixturesEachRuleFires` with one case per CC rule (severity `methodcheck.SeverityWarning`), using the existing `sysDoc`/`slot`/`withSlots` builders plus a new `dvSteps(useCaseID string, steps ...any)`/`step(nodeID string, es ...any)` builder pair and a slot-4 fixture builder `useCasesDoc(...)`. Run `GOWORK=off go test ./internal/utility/designhealth/ -run TestNegativeFixturesEachRuleFires -v` → fails (rules absent).
- [ ] **Step 3: Implement the mirror rules.** New `rules_callchain.go` in the package re-implementing the nine CC checks over the tolerant slices (port the Task-5 logic; the path walker gets a package-local copy `activityPaths` over the parsed slices — ~80 lines, acceptable duplication mirroring the existing designhealth-vs-methodcheck twin pattern). Append `callChainFindings(in)` in `Evaluate` after `coverageFindings`. All CC severities `methodcheck.SeverityWarning` via a package `const ccLiveSeverity = methodcheck.SeverityWarning`.
- [ ] **Step 4: Fix the green-fixture pins.** `GOWORK=off go test ./internal/utility/designhealth/...` — `TestGreenFixtureAdvisoriesFire` pins the exact advisory list against the real committed project.json; add the expected CC warnings (16 zero-step views → per-use-case `CC-COVERAGE`, plus `CC-TRIGGER-EVENT` on the 5 timer/busMessage use cases). `TestGreenFixtureNoErrors` must stay green untouched.
- [ ] **Step 5: Commit.** `git add server/internal/utility/designhealth/ && git commit -m "designhealth: mirror CC-* call-chain family over step-keyed views (advisory)"`

---

### Task 8: webApp codec regen + pure realization/linearization modules

**Files:**
- Generated: `server/api/openapi.yaml`, `webApp/src/contracts/schema.ts`, `enums.gen.ts`, `src/api/ops.gen.ts`
- Create: `webApp/src/contracts/realization.ts`, `webApp/src/contracts/realization.test.ts`
- Create: `webApp/src/components/flow/useCaseFindings.ts`, `.../useCaseFindings.test.ts`
- Modify: `webApp/src/contracts/adapters.ts` (`toDynamicView`, `DynamicViewModel`), `webApp/src/contracts/useCaseViews.ts`

**Interfaces (produced — Tasks 9–11 consume verbatim):**
```ts
// realization.ts (pure leaf, type-only imports from './types')
export interface RealizedCall { from: string; to: string; mode: CallMode; label: string; }
export interface RealizedStep { nodeId: string; calls: RealizedCall[]; }
/** Map of activityNodeId → its realization step for one use case ('' when none). */
export function realizationByNode(system: System | undefined, useCaseId: string): Map<string, RealizedStep>
/** Person participants: the owning use case's actors that appear as call endpoints. */
export function personParticipants(system: System | undefined, uc: UseCase | undefined): { id: string; role: string }[]

// adapters.ts
export interface PersonView { id: string; role: string; }
export type SequencedCall = C4Relationship & {
  seq: number;            // global 1-based across the linearization
  stepNodeId: string;     // owning activity node
  stepLabel: string;      // that node's label
  callInStep: number;     // 1-based within the step
  callsInStep: number;
};
export interface DynamicViewModel {
  title: string;
  participants: C4Component[];
  persons: PersonView[];
  edges: SequencedCall[];
}
// toDynamicView(envelope, key, useCasesEnvelope?) — linearizes Steps in a
// deterministic DFS over the use case's activity graph (authored branch order,
// loop edges once); steps for nodes missing from the graph append last in
// authored order (visible, never silently dropped).

// useCaseFindings.ts (components/flow leaf). dvLabel is the view's key — the
// same label designhealth's section grammar uses (Task 7).
export function findingsForUseCase(findings: readonly Finding[], useCaseId: string, dvLabel?: string): Finding[]
  // matches section === 'useCase '+useCaseId, plus (when dvLabel given) any
  // section starting with `dynamicView ${dvLabel}`
export function findingsForStep(findings: readonly Finding[], dvLabel: string, nodeId: string): Finding[]
  // matches section === `dynamicView ${dvLabel} step ${nodeId}`
```

- [ ] **Step 1: Regenerate the wire.** `cd server && make gen-client && cd ../webApp && npm run gen:api && npm run gen:ops`. Expected: `schema.ts` `ModelDynamicView` shows `steps`, `ModelActivityNode.kind` includes `'timeEvent' | 'acceptEvent'`, `linkedCompId` gone. `npm run typecheck` now FAILS (NODE_DIMS exhaustiveness + removed fields) — expected; fixed across Tasks 8–10. Commit the regen alone: `git add server/api webApp/src/contracts/schema.ts webApp/src/contracts/enums.gen.ts webApp/src/api/ops.gen.ts && git commit -m "codegen: step-keyed DynamicView + event node kinds on the wire"`.
- [ ] **Step 2: Write failing unit tests** for `realization.ts` (fixture `System` literal with one realized view; assert map keys, calls, persons derivation) and `useCaseFindings.ts` (section-grammar joins, including the `dynamicView <label> step <node>` parse). Run `npm run test` → fail.
- [ ] **Step 3: Implement `realization.ts` + `useCaseFindings.ts`** per Interfaces. `findingsForUseCase` impl note: match `f.location?.section === 'useCase '+id` plus step-scoped sections whose dvLabel resolves to this use case — pass the resolved dvLabel in as a second arg instead of guessing: signature `findingsForUseCase(findings, useCaseId, dvLabel?)`.
- [ ] **Step 4: Rewrite `toDynamicView` + `useCaseViews`.** Linearization: DFS from entry nodes (start ∪ event nodes) following authored edge order, each edge once; emit each realized step's calls on first node visit; then append never-visited realized steps. Persons resolve from `useCasesEnvelope` actors; participants = components referenced by calls (order of first appearance); an endpoint resolving to NEITHER component nor actor still renders — synthesize a `C4Component`-shaped entry `{id, name: id, kind: 'utility', layer: 'utility', …}`? NO — per spec error-visibility: give `DynamicViewModel` a fourth field `unresolved: string[]` and list them; the UI shows them (Task 10). `useCaseViews.toUseCaseView` keeps dropping nothing new (linkedCompId no longer exists); no change beyond types compiling.
- [ ] **Step 5: Green the leaf tests.** `npm run test` → PASS (whole-app typecheck still red until Task 10; that's fine — `node --test` only compiles the leaves).
- [ ] **Step 6: Commit.** `git add webApp/src/contracts webApp/src/components/flow/useCaseFindings.* && git commit -m "webApp: realization codec — step-keyed linearization, persons, finding joins"`

---

### Task 9: webApp activity node kinds + walkthrough multi-root

**Files:**
- Modify: `webApp/src/components/usecase/nodeDims.ts`, `ActivityNode.tsx` (~:241 switch), `UseCaseWalkthrough.tsx` (`KIND_HEADER` ~:35, `startId` ~:80)
- Test: `webApp/src/components/usecase/walkthroughRoots.test.ts` (new pure leaf)

**Interfaces:**
- Consumes: regenerated `ActivityNodeKind`.
- Produces: `walkthroughRoots.ts` pure helper `export function walkthroughRoots(nodes: {id:string;kind:ActivityNodeKind}[], edges: {from:string;to:string}[]): string[]` (in-degree-0 ∪ kind==='start', deduped, diagram order) — extracted so it is unit-testable and shared by the walkthrough.

- [ ] **Step 1: Write the failing root test** (`walkthroughRoots.test.ts`): single start → `[start]`; start + edge-less timeEvent → both, start first; no start → first in-degree-0.
- [ ] **Step 2: Implement `walkthroughRoots.ts`**; rewire `UseCaseWalkthrough` — when roots.length > 1, the initial focus card becomes an entry chooser: eyebrow "Entry", title "How does this use case begin?", one branch-style button per root (label `nodeText(root)`), `advance(rootId)` semantics via `setPath([rootId])`. `KIND_HEADER` gains `timeEvent: 'Time event'`, `acceptEvent: 'Event received'`.
- [ ] **Step 3: NODE_DIMS + shapes.** `nodeDims.ts`: `timeEvent: { w: 56, h: 64 }`, `acceptEvent: { w: 200, h: 60 }`. `ActivityNode.tsx`: new `Hourglass(t, d, isSelected)` builder (two stacked CSS triangles via `clipPath: 'polygon(0 0, 100% 0, 50% 50%, 100% 100%, 0 100%, 50% 50%)'`, lane-colored border) for `timeEvent`; `Pentagon(t, d, isSelected)` (`clipPath: 'polygon(0 0, 100% 0, 100% 100%, 0 100%, 18px 50%)'` — concave left notch, Note-builder style) for `acceptEvent`; add both cases to the switch.
- [ ] **Step 4: Verify.** `npm run test` (new leaf green) and `npm run typecheck` — the NODE_DIMS exhaustiveness error is gone; remaining typecheck errors must now only be in files Tasks 10–11 own (list them; if any other file is red, fix here).
- [ ] **Step 5: Commit.** `git add webApp/src/components/usecase/ && git commit -m "webApp: UML event node kinds + multi-root walkthrough entry"`

---

### Task 10: webApp person lane + DynamicViewFlow step captions/status

**Files:**
- Modify: `webApp/src/components/flow/flowLayout.ts` (`LAYER_ROWS`, `rowLabelText`, `layerColors`, `computeLayout` inputs), `flowShared.tsx` (person node type), `DynamicViewFlow.tsx`, `ArchitectureView.tsx` (statusBySeq wiring + step deep-link consume), `adapters.ts` only if a type nit surfaces
- Test: extend `webApp/src/components/flow/architectureDeepLink.test.ts`; new `webApp/src/components/flow/callStatus.test.ts`

**Interfaces:**
- Consumes: Task-8 `DynamicViewModel` (persons/edges/unresolved), `findingsForStep`.
- Produces: `DynamicViewFlow` new optional props `initialStep?: number` (0-based, clamped; applied when `resetKey` changes) — Task 11's deep link consumes it; `callStatus.ts` pure leaf `export function statusBySeqFromFindings(dv: DynamicViewModel, findings: readonly Finding[], dvLabel: string): Map<number, StepStatus>` (red where the owning step has findings, green where the step is realized and clean).

- [ ] **Step 1: Person lane.** `flowLayout.ts`: `export type FlowLayer = Layer | 'person'`; `LayoutComponent.layer: FlowLayer`; `LAYER_ROWS` gains `'person'` FIRST; `rowLabelText` case `'person'` → `'People'`; `layerColors` person color = `t.accent`-tinted neutral. `flowShared`/`flowNodeTypes`: a `PersonNode` (circle head + shoulders glyph via two stacked rounded boxes, name + role caption) registered as node type `person`; `DynamicViewFlow.build()` renders `dv.persons` as person-layer layout components and person-typed nodes; calls touching persons render like any edge. `dv.unresolved` renders as a warning chip row above the canvas listing the ids.
- [ ] **Step 2: Two-level caption + status.** `StepBar` caption becomes: line 1 `Step {seq} of {total} — {stepLabel} (call {callInStep}/{callsInStep})`, line 2 the call `label` + `from → to` (all data already on `SequencedCall`). Write `callStatus.test.ts` first (fixture findings with the Task-7 section grammar), then `callStatus.ts`, then wire in `ArchitectureView`: `statusBySeq={useMemo(() => statusBySeqFromFindings(dynamicModel, structureFindings, activeDynamicKey), […])}` — `activeDynamicKey` is the view key, which is exactly the `dvLabel` designhealth's section grammar uses.
- [ ] **Step 3: `initialStep` prop.** In `DynamicViewFlow`, extend the reset-on-`resetKey` block to seed `stepIndex` from `initialStep ?? 0` (clamped to edges length).
- [ ] **Step 4: Verify.** `npm run check` — typecheck must be fully green from here on. Manually eyeball via Storybook-less smoke: `npm run dev` against a running server is Task 13's job; here rely on tests + types.
- [ ] **Step 5: Commit.** `git add webApp/src && git commit -m "webApp: person participants, step-aware captions, CC status tinting"`

---

### Task 11: webApp badges, chip, focus-card calls, &step deep link

**Files:**
- Modify: `webApp/src/components/usecase/UseCaseWalkthrough.tsx`, `UseCaseCarousel.tsx`, `webApp/src/components/shared/StepLink.tsx:38`, `webApp/src/routes/router.tsx:53-56`, `webApp/src/components/flow/architectureDeepLink.ts` + test, `ArchitectureView.tsx` (`viewMemory` + consume), `webApp/src/utilities/constants/UIIdentifiers.ts`
- Test: extend `architectureDeepLink.test.ts`; new `webApp/src/components/usecase/useCaseChip.test.ts`

**Interfaces:**
- Consumes: `realizationByNode`, `findingsForUseCase`, `findingsForStep`, `DynamicViewFlow.initialStep`, Task-8 types.
- Produces: search param shape `{ view?: string; step?: number }` (step = 1-based seq); testids `UseCaseCarousel.REALIZATION_CHIP`, `UseCaseCarousel.STEP_BADGE`, `UseCaseCarousel.STEP_CALLS` added to `UI_IDENTIFIERS` + uitests `TESTID` map.

- [ ] **Step 1: Deep link.** Extend `resolveDeepLinkView` input/decision with `stepParam: string` → `DeepLinkDecision.step: number | undefined` (positive int only); write the failing test cases first (fresh nav applies view+step; same-location remount yields; dangling step ignored). `router.tsx` `validateSearch` parses `step` (`Number.isInteger(Number(v)) && Number(v) > 0`); `StepLink.search?: { view?: string; step?: number }`; `viewMemory` gains `dynamicStep: number` mirrored like `dynamicKey`; `ArchitectureView` passes `initialStep={consumedStep - 1}`.
- [ ] **Step 2: Walkthrough badges + calls.** `UseCaseWalkthrough` gains props `realization: Map<string, RealizedStep>` and `stepFindings: (nodeId: string) => Finding[]` (passed from `UseCaseCarousel`, which reads `useStructureFindings()` + `useCommittedSlotEnvelope('system')` — hook-free contexts, boundary-legal). Focus card: after the lane chip, a badge chip — `✓ realized` (step exists, findings empty), `✗ <ruleId>` (findings non-empty), `— no realization` (eligible kind, no step); below the title, when a step exists, a compact mono list of its calls (`from → to · label`) with the "View call chain" `StepLink` carrying `{ view: callChainKey, step: firstSeqOfNode }` — compute `firstSeqOfNode` from `toDynamicView(...).edges.find(e => e.stepNodeId === currentId)?.seq`.
- [ ] **Step 3: Carousel chip.** New pure leaf `useCaseChip.ts`: `export function realizationChip(realization: Map<string, RealizedStep>, eligibleNodeIds: string[], findings: readonly Finding[]): { label: string; tone: 'ok' | 'warn' | 'error' }` → `"7/9 steps realized"` etc.; test first. Render next to the CORE/NON-CORE chip (`UseCaseCarousel.tsx:270`) with `data-testid` `REALIZATION_CHIP`.
- [ ] **Step 4: Verify + commit.** `npm run check` green. `git add webApp/src uitests/tests/support/testids.ts && git commit -m "webApp: realization badges, use-case chip, step deep link"`

---

### Task 12: PoC amendment — realize `drive-system-design` in archistrator's project.json

**Files:**
- Modify: `.aiarch/state/project.json` — slot 5 `.model.dynamicViews` (all 16 → new shape; `drive-system-design` fully realized), slot 4 mechanical cleanup
- Test: the full gate loop

**Interfaces:**
- Consumes: everything above.
- Produces: the QA-able state.

- [ ] **Step 1: Mechanical reshape.** With jq or editor: every one of the 16 `dynamicViews` entries becomes `{useCaseId, key, title, steps: []}` (drop `participants`/`edges`; keep `key`/`title` verbatim — they are deep-link anchors). In slot 4, strip every `"linkedCompId"` key (all null) — `jq 'walk(if type == "object" then del(.linkedCompId) else . end)'` scoped to the slot-4 model.
- [ ] **Step 2: Author the `uc1-drive-system-design` realization.** Steps for all 14 action nodes per the architect's sampled walk (spec §6a). Entry convention: the entry calls ride the first action node. Both-surface entry (founder R4 ruling). Exact content to author (labels may be tuned during authoring, structure may not):
```
read-prior-models:    architect-user→web-client "opens the design experience" (sync)
                      architect-user→mcp-client "drives design via MCP surface" (sync)
                      web-client→system-design-manager "startSystemDesign(projectId)" (sync)
                      mcp-client→system-design-manager "startSystemDesign(projectId)" (sync)
                      system-design-manager→project-state-access "readProject(projectId) → head state" (sync)
dispatch-draft-job:   system-design-manager→agentic-job-access "dispatch architect-draft job" (sync)
observe-read-back:    system-design-manager→agentic-job-access "observe job; read back drafted slot" (sync)
regenerate-escalate:  system-design-manager→agentic-job-access "re-dispatch / escalate draft job" (sync)
dispatch-critique-job: system-design-manager→agentic-job-access "dispatch critique job (PM or self)" (sync)
weave-pm-notes:       system-design-manager→agentic-job-access "re-dispatch draft with critique notes" (sync)
stage-for-review:     system-design-manager→project-state-access "stageArtifactForReviewOnBranch" (sync)
human-gate:           architect-user→web-client "reviews the staged artifact; submits review action" (sync)
                      web-client→system-design-manager "submitReviewDecision(...)" (sync)
ask-questions:        system-design-manager→project-state-access "seedReviewCommentsOnBranch (questions)" (sync)
                      system-design-manager→agentic-job-access "dispatch answer job" (sync)
weave-feedback:       system-design-manager→agentic-job-access "re-dispatch draft with change requests" (sync)
commit-advance:       system-design-manager→project-state-access "commitArtifactWithProvenance; advance step" (sync)
                      project-state-access→project-git-repo "commit guarded by ref CAS" (sync)
withdraw:             system-design-manager→project-state-access "withdrawArtifactOnBranch" (sync)
reconcile-stale:      system-design-manager→project-state-access "reconcileBranchFromMain / amend stale slot" (sync)
seal-phase:           system-design-manager→project-state-access "advancePhase (Phase-1 seal)" (sync)
```
Decision nodes carry no steps except `ci-check`: `system-design-manager→design-health "EvaluateDesignHealth(draft) → findings"` (sync) — the architect flagged this natural attachment; include it (decision steps are legal).
- [ ] **Step 3: Gate loop.** Run in order, all from `server/`: `make gen-models` (must be a no-op diff), `make method-check`, `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System`, `GOWORK=off make test-short`. Expected: zero Errors; advisory CC warnings only for the 15 unrealized views + 5 trigger-event warnings; **none for `drive-system-design`**. Then `cd ../webApp && npm run check`. Fix designhealth green-fixture advisory pins if the realized view changed the expected list (it should — `drive-system-design`'s CC-COVERAGE warning disappears).
- [ ] **Step 4: Commit.**
```bash
git add .aiarch/state/project.json server/internal/utility/designhealth/
git commit -m "systemDesign: realize drive-system-design as step-keyed call chain (design amendment, PoC)"
```

---

### Task 13: QA environment + smoke + STOP for founder

**Files:**
- Create: nothing in-repo (scratch clone lives outside)
- Test: Playwright smoke + manual founder QA

- [ ] **Step 1: Scratch clone with our branch as its main** (the local-git substrate hard-codes branch `main`):
```bash
rm -rf /Users/davidmarne/mixofrealitystudio/archistrator-qa-clone
git clone /Users/davidmarne/mixofrealitystudio/archistrator /Users/davidmarne/mixofrealitystudio/archistrator-qa-clone
cd /Users/davidmarne/mixofrealitystudio/archistrator-qa-clone
git checkout -B main callchain-realization
git config receive.denyCurrentBranch updateInstead
```
- [ ] **Step 2: Boot server against the clone + dev SPA from the working tree:**
```bash
# terminal 1
cd /Users/davidmarne/mixofrealitystudio/archistrator/server
ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true \
ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL=file:///Users/davidmarne/mixofrealitystudio/archistrator-qa-clone \
ARCHISTRATOR_ARTIFACT_REPO_URL=file:///Users/davidmarne/mixofrealitystudio/archistrator-qa-clone \
ARCHISTRATOR_LISTEN_ADDR=:8888 ARCHISTRATOR_AUTH_DEV_MODE=true \
ARCHISTRATOR_CONSTRUCTION_DRYRUN=true \
go run ./cmd/server        # workspace-active is fine here; replace directive covers GOWORK=off too
# terminal 2
cd /Users/davidmarne/mixofrealitystudio/archistrator/webApp && npm run dev
```
- [ ] **Step 3: Playwright smoke.** `cd uitests && npm run list` (structural gate) then `npm test -- architecture-views` with the dev stack up. If the spec's dynamic-lens assertions need updating for the two-level caption, update `architecture-views.spec.ts` minimally (same behaviors, new caption text).
- [ ] **Step 4: STOP.** Report to the founder with the QA checklist: use-cases step → Drive System Design → walkthrough badges (✓ on realized steps, — on the 15 sibling use cases' views), focus-card call lists, "View call chain →" landing on the right step, dynamic lens two-level captions + person row, Design Health showing CC advisories, carousel realization chip. **No further work (full amendment, doctrine, severity flip, releases) until founder sign-off.**

---

## Self-review notes

- Spec §3 model → Tasks 1–3; §4 validation → Tasks 5–7 (severity staged per Global Constraints); §5 UI → Tasks 8–11; §6 PoC checkpoint → Tasks 12–13. Deliberately OUT (post-QA plan): remaining 15 realizations, activity-diagram event-node amendments, linkedActorId cleanup, attestation rewording, method-assets doctrine, framework-go release + pin swap, severity flip to Error, `serviceContracts` collateral beyond `$defs` (done in Task 1), downstream staleness pass.
- Type names cross-checked: `CallStep.ActivityNodeID`/`activityNodeId`, `ccGateSeverity` (methodcheck) vs `ccLiveSeverity` (designhealth), `SequencedCall`, `RealizedStep`, `walkthroughRoots`, `statusBySeqFromFindings`, `realizationChip`, `initialStep` — each defined in exactly one task's Produces block before use.
