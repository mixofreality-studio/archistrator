# aiarch-state-mcp Deliberate Agent-Surface Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make aiarch-state-mcp's registered tool surface one deliberate, pinned service contract — the per-mode composed verbs plus an explicit Engine compute grant — and stop registering the raw generated catalog (no more out-of-substrate tools that always error, no raw projectStateAccess reads).

**Architecture:** The composed verbs in `tools.go` are already the right agent-facing contract; the defect is `registerRawReadTools`, which registers every non-hidden read-only RA op + Engine op from the generated catalog in every mode — including eight RA components that can only return "unavailable in this substrate" errors, and raw whole-project reads that duplicate the kind-scoped composed reads. This plan (1) declares the surface as literal per-mode tables in a new `surfacecontract.go` with tests enforcing Appendix B budgets, (2) narrows registration to composed verbs + granted Engines only, (3) shrinks the `rawexec.go` execution rail to Engine dispatch, and (4) demotes the generated catalog to internal metadata (Engine descriptors + docs). Founder ruling context: per-task palettes are already dead (commit 91dd5de); scoping is per-mode, permanently.

**Tech Stack:** Go 1.x (server module), `modelcontextprotocol/go-sdk/mcp`, generated catalog in `server/internal/resourceaccess/projectstate/toolcatalog.gen.go`.

## Global Constraints

- All Go commands run from `/Users/davidmarne/mixofrealitystudio/archistrator/server` with `GOWORK=off` (memory: platform pins require it).
- Never weaken gates: the arch/encapsulation test (`server/internal/arch_test.go`) allowlist may only *shrink* here (removing a deleted export), never grow.
- `gofmt` clean; the 7-linter gate applies (errcheck/govet/ineffassign/staticcheck/unused/gochecksumtype/gosec).
- Do not touch `toolcatalog.gen.go` (generated) or `cmd/internaltoolsgen`'s emit logic — only its doc comment.
- The composed verbs themselves (names, modes, handlers, descriptions) are ratified and unchanged by this plan.
- Commit messages end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: Declare the surface contract (`surfacecontract.go` + pin tests)

**Files:**
- Create: `server/cmd/aiarch-state-mcp/surfacecontract.go`
- Test: `server/cmd/aiarch-state-mcp/surfacecontract_test.go`

**Interfaces:**
- Consumes: `composedVerbs(s *Session) []composedVerb` and mode constants `jobModeDraft/jobModeCritique/jobModeAnswer/jobModeConstruct` (`session.go`), `engineImpls() map[string]any` (`rawexec.go`).
- Produces: `agentSurfaceComposed map[string][]string`, `engineComponents []string`, `const appendixBMaxOps = 12` — Task 2's registration filter and tests use these exact names.

- [ ] **Step 1: Write the failing tests**

```go
package main

import (
	"reflect"
	"sort"
	"testing"
)

// TestSurfaceContract_ComposedMatchesRegistry pins the deliberate contract to the
// live composed-verb registry: every mode's registered composed set must equal the
// declared table exactly — a new verb or mode change is a contract change and must
// be made in BOTH places, deliberately.
func TestSurfaceContract_ComposedMatchesRegistry(t *testing.T) {
	s := &Session{}
	for _, mode := range []string{jobModeDraft, jobModeCritique, jobModeAnswer, jobModeConstruct} {
		s.Mode = mode
		var got []string
		for _, v := range composedVerbs(s) {
			if containsStr(v.modes, mode) {
				got = append(got, v.name)
			}
		}
		sort.Strings(got)
		want := append([]string{}, agentSurfaceComposed[mode]...)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mode %q: composed surface drifted from the declared contract\n got: %v\nwant: %v", mode, got, want)
		}
	}
}

// TestSurfaceContract_AppendixBBudget enforces Righting Software Appendix B on the
// agent-facing contract: at most 12 operations per mode facet.
func TestSurfaceContract_AppendixBBudget(t *testing.T) {
	for mode, ops := range agentSurfaceComposed {
		if len(ops) > appendixBMaxOps {
			t.Fatalf("mode %q exposes %d composed ops; Appendix B caps a contract at %d", mode, len(ops), appendixBMaxOps)
		}
	}
}

// TestSurfaceContract_EngineGrantMatchesConstructedEngines keeps the deliberate
// Engine grant honest: exactly the engines rawexec constructs, no more, no fewer.
func TestSurfaceContract_EngineGrantMatchesConstructedEngines(t *testing.T) {
	var constructed []string
	for c := range engineImpls() {
		constructed = append(constructed, c)
	}
	sort.Strings(constructed)
	granted := append([]string{}, engineComponents...)
	sort.Strings(granted)
	if !reflect.DeepEqual(granted, constructed) {
		t.Fatalf("engine grant drifted from constructed engines\ngranted: %v\nconstructed: %v", granted, constructed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && GOWORK=off go test ./cmd/aiarch-state-mcp/ -run TestSurfaceContract -v`
Expected: FAIL to compile — `undefined: agentSurfaceComposed`, `undefined: engineComponents`, `undefined: appendixBMaxOps`

- [ ] **Step 3: Write `surfacecontract.go`**

```go
package main

// surfacecontract.go — THE deliberate agent-facing service contract of
// aiarch-state-mcp (founder ruling 2026-07-06). The registered tool surface is
// exactly: the composed verbs of the job mode (declared below, verbatim), plus the
// operations of the granted pure Engine components (each Engine's own Method
// contract at run-time binding — side-effect-free compute, safe in every venue).
//
// The generated internal catalog (projectstate.InternalToolCatalog) is METADATA:
// it supplies the Engine tools' schemas/descriptors and documents the raw op set,
// but it is NOT an eligible registration surface. Out-of-substrate ResourceAccess
// ops are never registered (absence beats an "unavailable in this substrate"
// error), and raw projectStateAccess reads are not agent tools — the kind-scoped
// composed reads are the deliberate read surface.
//
// Changing this file is a CONTRACT change: it is pinned 1:1 against the live
// composed-verb registry and the constructed engine set by surfacecontract_test.go,
// and budgeted per Righting Software Appendix B.

// appendixBMaxOps is the Appendix B contract budget: a service contract should hold
// 3–5 operations and never exceed 12.
const appendixBMaxOps = 12

// agentSurfaceComposed is the composed-verb facet of the contract, per job mode.
// Kept sorted; the pin test compares sets, but keep these readable.
var agentSurfaceComposed = map[string][]string{
	jobModeDraft: {
		"getCommittedSlot", "getDraftSlot", "getResearchSource", "getReviewThread",
		"listResearchSources", "publishDraft", "putDraftModel", "respondToReviewComment",
	},
	jobModeCritique: {
		"getCommittedSlot", "getDraftSlot", "getResearchSource", "getReviewThread",
		"listResearchSources", "publishDraft", "setCritiqueVerdict",
	},
	jobModeAnswer: {
		"getCommittedSlot", "getDraftSlot", "getResearchSource", "getReviewThread",
		"listResearchSources", "publishDraft", "respondToReviewComment",
	},
	jobModeConstruct: {
		"getCommittedSlot", "getResearchSource", "listResearchSources", "publishDraft",
		"recordPhaseArtifact", "recordServiceContract", "recordTestingState",
	},
}

// engineComponents is the deliberate compute grant: the Engine components whose
// contract operations register as tools in EVERY mode. A new Engine op on a granted
// component flows in automatically via the generated catalog (no hand-wiring); a
// new Engine COMPONENT requires a deliberate edit here.
var engineComponents = []string{
	"autoscalerEngine",
	"billingEngine",
	"estimationEngine",
	"handOffEngine",
	"interventionEngine",
	"operationEstimationEngine",
	"reviewEngine",
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && GOWORK=off go test ./cmd/aiarch-state-mcp/ -run TestSurfaceContract -v`
Expected: PASS (all three)

- [ ] **Step 5: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/cmd/aiarch-state-mcp/surfacecontract.go server/cmd/aiarch-state-mcp/surfacecontract_test.go
git commit -m "aiarch-state-mcp: declare the deliberate agent-surface contract (pinned + App-B budgeted)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Registration switch — composed verbs + granted Engines only

**Files:**
- Modify: `server/cmd/aiarch-state-mcp/tools.go` (buildServer doc comment + `registerRawReadTools` → `registerEngineTools`, around lines 55–100)
- Modify: `server/cmd/aiarch-state-mcp/rig_test.go` (`TestRig_RawReadToolsInEveryModeOverStdio` at :205, `TestRig_RawReadToolExecutesOverStdio` at :275, `TestRig_RawUnavailableToolOverStdio` at :316)

**Interfaces:**
- Consumes: `engineComponents` (Task 1), `projectstate.InternalToolCatalog()`, existing `registerRawTool(srv, s, tool)`.
- Produces: `registerEngineTools(srv *mcp.Server, s *Session)` — called from `buildServer`; the registered set per mode is now `agentSurfaceComposed[mode] + engine tools`.

- [ ] **Step 1: Rewrite the rig registration test to the desired surface (failing first)**

Replace `TestRig_RawReadToolsInEveryModeOverStdio` (rig_test.go:200–273) with — keep the existing per-mode session/stdio harness lines of the old test body intact and change only the name, doc comment, and assertions:

```go
// TestRig_EngineToolsOnlyInEveryModeOverStdio proves the deliberate-surface
// registration model: granted Engine compute registers in every mode, and nothing
// else from the raw generated catalog does — no out-of-substrate RA reads (absence,
// not an "unavailable" error) and no raw projectState reads (the composed
// kind-scoped reads are the read surface).
func TestRig_EngineToolsOnlyInEveryModeOverStdio(t *testing.T) {
	// ... (unchanged harness from the old test: for each mode, boot the stdio
	//      server, list tools into `got` keyed by name) ...

	if got["estimationComputeNetwork"] == nil {
		t.Fatalf("granted engine tool estimationComputeNetwork must register in every mode: %v", keysOfTools(got))
	}
	if a := got["estimationComputeNetwork"].Annotations; a == nil || !a.ReadOnlyHint {
		t.Fatalf("engine tool must carry readOnlyHint: %+v", got["estimationComputeNetwork"].Annotations)
	}
	if got["projectStateReadProject"] != nil {
		t.Fatalf("raw projectState reads must NOT register — composed reads are the read surface")
	}
	if got["billingStateReadBilling"] != nil {
		t.Fatalf("out-of-substrate RA tools must NOT register (absence beats an unavailable error)")
	}
	if got["sourceControlGetPullRequestStatus"] != nil {
		t.Fatalf("out-of-substrate RA tools must NOT register (absence beats an unavailable error)")
	}
}
```

Delete `TestRig_RawReadToolExecutesOverStdio` (:275, it drives `projectStateReadProject` over stdio) and `TestRig_RawUnavailableToolOverStdio` (:316, it asserts the unavailable error we are removing).

- [ ] **Step 2: Run rig tests to verify the new one fails**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && GOWORK=off go test ./cmd/aiarch-state-mcp/ -run TestRig_EngineToolsOnly -v`
Expected: FAIL — `projectStateReadProject` (and the out-of-substrate reads) still register today

- [ ] **Step 3: Switch registration in `tools.go`**

Replace `registerRawReadTools` (tools.go:88–98) with:

```go
// registerEngineTools registers the granted pure-Engine compute surface — every
// operation of a component named in engineComponents (surfacecontract.go), with
// schemas drawn from the generated catalog. This is the ONLY consumer of the raw
// catalog at registration time: composed verbs are the sole state surface, and no
// other raw op (RA reads included) is ever registered.
func registerEngineTools(srv *mcp.Server, s *Session) {
	granted := make(map[string]bool, len(engineComponents))
	for _, c := range engineComponents {
		granted[c] = true
	}
	for _, tool := range projectstate.InternalToolCatalog() {
		if tool.AgentHidden || tool.Layer != "Engine" || !granted[tool.Component] {
			continue
		}
		registerRawTool(srv, s, tool)
	}
}
```

In `buildServer` (tools.go:71–86): change the call `registerRawReadTools(srv, s)` → `registerEngineTools(srv, s)`, and replace the two-layer SCOPING doc comment (tools.go:60–70) with:

```go
// SCOPING (founder ruling 2026-07-06 — the deliberate agent-surface contract,
// surfacecontract.go). Two layers register together, in EVERY job mode:
//  1. the per-mode composed verbs (agentSurfaceComposed) — the only state surface; and
//  2. the granted pure-Engine compute tools (engineComponents) — each Engine's own
//     contract at run-time binding, side-effect-free in any venue.
// Nothing else from the generated catalog registers: raw writes and AgentHidden ops
// never did; out-of-substrate RA ops and raw projectState reads no longer do —
// absence beats an "unavailable in this substrate" error, and the kind-scoped
// composed reads are the deliberate read surface.
```

- [ ] **Step 4: Run the rig + surface tests**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && GOWORK=off go test ./cmd/aiarch-state-mcp/ -run 'TestRig_|TestSurfaceContract' -v`
Expected: PASS (including `TestRig_FullDraftCycleOverStdio`, `TestRig_CritiqueModeToolSet`, `TestRig_AnswerModeToolSet`, `TestRig_ConstructModeFullCycleOverStdio` — composed verbs untouched)

- [ ] **Step 5: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/cmd/aiarch-state-mcp/tools.go server/cmd/aiarch-state-mcp/rig_test.go
git commit -m "aiarch-state-mcp: register only the deliberate surface — composed verbs + granted Engine compute

Out-of-substrate RA reads and raw projectState reads no longer register;
absence beats an unavailable-in-substrate error.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Shrink the execution rail to Engine dispatch

**Files:**
- Modify: `server/cmd/aiarch-state-mcp/rawexec.go` (delete `unavailableDeps` :71–84, `executeProjectStateRead` :185–205; simplify `executeRawTool` :88–104 and `inSubstrateComponents` :207–224; rewrite the file-header SUBSTRATE MODEL comment :1–30)
- Modify: `server/cmd/aiarch-state-mcp/rawexec_test.go` (delete `TestExecuteRawTool_UnavailableInSubstrate` :71, `TestExecuteProjectStateRead_ReadProject` :102; update `TestInSubstrateLedger` :124; keep `TestExecuteEngineTool_LiveDispatch`, `TestExecuteEngineTool_SurfacesDomainError`, `TestExecuteRawTool_AgentHiddenRefused`)

**Interfaces:**
- Consumes: `engineImpls()`, `executeEngineTool(ctx, tool, args)` (both unchanged).
- Produces: `executeRawTool` that dispatches Engines and refuses everything else with a not-on-surface error; `inSubstrateComponents()` returning only the engine components.

- [ ] **Step 1: Update the tests first**

Delete `TestExecuteRawTool_UnavailableInSubstrate` and `TestExecuteProjectStateRead_ReadProject`. Update `TestInSubstrateLedger` to expect engines only:

```go
// TestInSubstrateLedger pins the positive side of the executes-in-venue set: the
// granted engines, and nothing else (projectStateAccess left this rail when the
// composed reads became the sole read surface).
func TestInSubstrateLedger(t *testing.T) {
	got := inSubstrateComponents()
	want := []string{
		"autoscalerEngine", "billingEngine", "estimationEngine", "handOffEngine",
		"interventionEngine", "operationEstimationEngine", "reviewEngine",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("in-substrate ledger drifted\n got: %v\nwant: %v", got, want)
	}
}
```

Add a refusal test for non-Engine tools:

```go
// TestExecuteRawTool_NonEngineRefused proves the rail executes ONLY Engine compute:
// any RA-layer tool (registration should never bind one, but defense in depth) is
// refused with a not-on-surface error, not routed to I/O.
func TestExecuteRawTool_NonEngineRefused(t *testing.T) {
	tool, ok := projectstate.InternalToolByName("usageReadRange")
	if !ok {
		t.Fatal("usageReadRange not in catalog")
	}
	_, err := executeRawTool(context.Background(), &Session{}, tool, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "not on the agent surface") {
		t.Fatalf("want not-on-the-agent-surface refusal, got: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify the new/changed tests fail**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && GOWORK=off go test ./cmd/aiarch-state-mcp/ -run 'TestInSubstrateLedger|TestExecuteRawTool_NonEngineRefused' -v`
Expected: FAIL — ledger still contains `projectStateAccess`; `usageReadRange` still returns the unavailable-deps error text

- [ ] **Step 3: Simplify `rawexec.go`**

Replace `executeRawTool` with:

```go
// executeRawTool is the entry point registerRawTool binds each registered raw
// tool's handler to. Registration only ever binds granted Engine tools
// (registerEngineTools), so this rail executes pure compute and refuses anything
// else — defense in depth, not a routing layer.
func executeRawTool(ctx context.Context, s *Session, tool projectstate.InternalTool, args map[string]any) (any, error) {
	if tool.AgentHidden {
		return nil, fmt.Errorf("raw tool %q (%s.%s) is agent-hidden and must not execute; its authority stays on the server rail", tool.Name, tool.Component, tool.Operation)
	}
	if tool.Layer != "Engine" {
		return nil, fmt.Errorf("tool %q (%s.%s) is not on the agent surface: only granted Engine compute and the composed verbs execute in a venue", tool.Name, tool.Component, tool.Operation)
	}
	return executeEngineTool(ctx, tool, args)
}
```

Delete `unavailableDeps` and `executeProjectStateRead` entirely. Simplify `inSubstrateComponents`:

```go
// inSubstrateComponents returns the sorted set of components whose raw tools execute
// in this substrate — exactly the constructed engines (the deliberate compute grant).
func inSubstrateComponents() []string {
	out := make([]string, 0, len(engineImpls()))
	for c := range engineImpls() {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
```

Rewrite the file-header SUBSTRATE MODEL comment (:1–30) to match: Engines execute in-process; projectStateAccess reads/writes live on the composed-verb surface only; no other RA is reachable, because it is never registered (surfacecontract.go is the authority). Remove the now-unused `Session` field access if the compiler flags `s` as unused in `executeRawTool` — keep the parameter (registerRawTool's signature binds it).

- [ ] **Step 4: Run the full package**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && GOWORK=off go test ./cmd/aiarch-state-mcp/ -v`
Expected: PASS, no skips related to these changes

- [ ] **Step 5: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/cmd/aiarch-state-mcp/rawexec.go server/cmd/aiarch-state-mcp/rawexec_test.go
git commit -m "aiarch-state-mcp: execution rail is Engine-only; drop unavailable-in-substrate ledger + raw PSA reads

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Demote the catalog to metadata (delete dead export, fix comments, spec)

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/toolpalette.go` (delete `AgentExposableTools` :81–93; rewrite the DOCTRINE header comment :5–29)
- Modify: `server/internal/resourceaccess/projectstate/toolcatalog_test.go` (:67–73, drop the `AgentExposableTools` assertions)
- Modify: `server/internal/arch_test.go` (:397, remove the `"AgentExposableTools"` allowlist entry — allowlist shrinks, never grows)
- Modify: `server/cmd/internaltoolsgen/main.go` (:285 area, the generated header sentence)
- Create: `docs/superpowers/specs/2026-07-06-aiarch-state-agent-surface.md`
- Modify: `docs/later.md` (append one earmark line)

**Interfaces:**
- Consumes: nothing new.
- Produces: no code interfaces — doc + cleanup. `InternalToolCatalog` and `InternalToolByName` remain exported (consumed by `registerEngineTools` and tests).

- [ ] **Step 1: Verify `AgentExposableTools` has no remaining consumers**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator && grep -rn "AgentExposableTools" server --include="*.go" | grep -v "toolpalette.go\|toolcatalog_test.go\|arch_test.go"`
Expected: no output. (If anything appears, STOP — do not delete; report the consumer.)

- [ ] **Step 2: Delete the export + test assertions + allowlist entry**

In `toolpalette.go`: delete the `AgentExposableTools` function and its doc comment. Rewrite the file-header DOCTRINE comment (:5–29) to:

```go
// toolpalette.go — the generated internal tool CATALOG for archistrator's own
// ResourceAccess and Engine contracts.
//
// STATUS (founder ruling 2026-07-06). The catalog is METADATA, not a registration
// surface: cmd/aiarch-state-mcp registers exactly its deliberate agent-surface
// contract (surfacecontract.go there) — the per-mode composed verbs plus the
// granted pure-Engine compute, whose schemas/descriptors this catalog supplies.
// Out-of-substrate ResourceAccess ops are generated for documentation and future
// substrate bindings but are never registered as agent tools. The composed verbs
// (putDraftModel, publishDraft, getCommittedSlot, …) remain the only write surface;
// raw projectStateAccess ops (writes AND reads) stay off the agent surface.
```

In `toolcatalog_test.go` (:67–73): delete the loop asserting over `AgentExposableTools()`. In `arch_test.go` (:397): delete the `"AgentExposableTools",` line.

In `cmd/internaltoolsgen/main.go` (:285 area): change the generated-header sentence from "the catalog aiarch-state-mcp will register" to "the descriptor metadata aiarch-state-mcp draws its granted Engine tools' schemas from; registration is governed by that binary's deliberate agent-surface contract". Then regenerate to confirm the emitted file only changes in that comment:

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && GOWORK=off make -n 2>/dev/null | head -1; grep -rn "internaltoolsgen" Makefile | head -3`
Use whichever Makefile target invokes internaltoolsgen (per the grep output) to regenerate `toolcatalog.gen.go`, then `git diff server/internal/resourceaccess/projectstate/toolcatalog.gen.go` — Expected: comment-only diff.

- [ ] **Step 3: Run the module gates**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && GOWORK=off go test ./internal/resourceaccess/projectstate/ ./internal/ ./cmd/aiarch-state-mcp/ && GOWORK=off gofmt -l cmd internal`
Expected: tests PASS; gofmt lists nothing

- [ ] **Step 4: Write the spec + earmark**

Create `docs/superpowers/specs/2026-07-06-aiarch-state-agent-surface.md`:

```markdown
# aiarch-state-mcp: the deliberate agent-surface contract

**Status:** ratified 2026-07-06 (founder). Supersedes the "generated tools ARE the
eligible surface" clause of item 3 in `2026-07-03-agentic-managers-design.md` and
the every-mode raw-read widening settled in commit 91dd5de.

## Ruling

The agent-facing tool surface of a design/construction episode is ONE deliberate
service contract, declared in `server/cmd/aiarch-state-mcp/surfacecontract.go` and
pinned by test:

1. **Composed verbs per job mode** (draft / critique / answer / construct) — the
   only state surface. Kind-scoped typed-JSON reads and writes over project.json
   (putDraftModel validates through the full server codec + Method CI rules).
   Budgeted per Righting Software Appendix B (≤12 ops per mode facet).
2. **Granted pure-Engine compute** — each granted Engine's own Method contract at
   run-time binding (side-effect-free, venue-safe). New op on a granted engine
   flows in via the generated catalog; new engine component is a deliberate edit.

Nothing else registers. Out-of-substrate ResourceAccess ops (GitHub App, Temporal,
Postgres, Stripe, operated runtime) are ABSENT, not "unavailable": absence beats a
tool that always errors — it saves agent context and invites no wasted calls. Raw
projectStateAccess reads are off the surface too; the composed kind-scoped reads
are the read surface (a whole-project dump verb would blow episode context — if a
real need appears, design a composed overview read deliberately).

## What the generated catalog is now

`projectstate.InternalToolCatalog` (emitted by cmd/internaltoolsgen from
.serviceContracts) is internal METADATA: Engine tool schemas/descriptors, raw-op
documentation, and the substrate-neutral basis for possible future per-binding
surfaces (e.g. the mode-C platform worker). It is not consumed at registration
time except to look up granted Engine descriptors.

## Why (Method grounding)

The episode is the dispatching Manager at run-time binding; its tool surface is a
CONTRACT and is judged like any contract (Appendix B), not as a projection of all
13 component contracts. Per-task palettes were ruled dead (91dd5de); scoping is
per-mode, permanently. The composed verbs compile invariants in (doctrine rule 3);
merge authority stays on the server rail (raw writes stay AgentHidden).
```

Append to `docs/later.md` under earmarks:

```markdown
- aiarch-state agent surface (2026-07-06): if construct episodes ever need a whole-project read, add a composed `getProjectOverview` deliberately (raw projectStateReadProject was removed from the surface); if mode-C platform workers need a wider in-cluster surface, that is a per-substrate-binding grant in surfacecontract.go, not a return to catalog-wide registration.
```

- [ ] **Step 5: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/internal/resourceaccess/projectstate/toolpalette.go \
        server/internal/resourceaccess/projectstate/toolcatalog_test.go \
        server/internal/resourceaccess/projectstate/toolcatalog.gen.go \
        server/internal/arch_test.go server/cmd/internaltoolsgen/main.go \
        docs/superpowers/specs/2026-07-06-aiarch-state-agent-surface.md docs/later.md
git commit -m "catalog demoted to metadata: drop AgentExposableTools, doctrine comments, agent-surface spec

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Full-module verification

**Files:** none new.

- [ ] **Step 1: Run the whole server module**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && GOWORK=off go build ./... && GOWORK=off go test ./...`
Expected: build clean; all tests PASS. Pay attention to `internal/manager/systemdesign` and `internal/manager/projectdesign` (their prompts/workflow tests exercise dispatch wiring — none reference raw tool names, verified 2026-07-06, but the suite is the proof).

- [ ] **Step 2: Run the lint gate**

Run the repo's standard lint target (check `server/Makefile` for the lint target name, e.g. `GOWORK=off make lint`); Expected: clean, same as before this plan.

- [ ] **Step 3: No commit** — this task only verifies; fix-forward inside the offending task's files if anything fails, amend that task's commit.

---

## Self-Review (done at authoring time)

- **Spec coverage:** ruling → Task 1 (declare+pin), registration narrowing → Task 2, rail shrink → Task 3, catalog demotion + docs → Task 4, gates → Task 5. The composed verbs themselves are explicitly out of scope (ratified, unchanged).
- **Placeholder scan:** none; every step carries code or an exact command. One deliberate lookup remains (the Makefile target that runs internaltoolsgen / lint) — expressed as a concrete grep, not a TBD.
- **Type consistency:** `agentSurfaceComposed`, `engineComponents`, `appendixBMaxOps` (Task 1) are consumed with identical names in Tasks 2–3; `registerEngineTools` name matches between Task 2's code and comments; deleted symbols (`unavailableDeps`, `executeProjectStateRead`, `AgentExposableTools`) are referenced only in deletion steps.
