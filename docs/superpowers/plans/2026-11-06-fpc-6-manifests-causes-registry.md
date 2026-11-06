# FPC Plan 6 — Generator Manifests, Tiered Admission, Typed Failure Causes, Doctrine Registry

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Step detail is filled from Plan 5's final signatures before execution.

**Goal:** Edit scope and drift read one computed manifest; artifacts are admitted by their consumers before a paid run can see them; host faults never consume attempts; every rule the platform requires of a built app is enforced in code and the registry proves it.

**Architecture:** Each generator exposes `Plan(model, selection) []Output` ahead of emission; the review Engine (ruling 5) consumes `Plan` to derive task specs, edit scope and checks, out of the projectstate RA. Admission gains Tier 1 (emit + `go/types` over generated packages) on record and Tier 2 (full build chain) at commit. The agentic-job RA classifies failures into a sum type the delivery Manager's policy switches on exhaustively. method-assets holds a doctrine registry whose CI ratchets prompt-only rules to zero. **Founder rulings 5, 6, 7 required.**

**Spec:** §W7 (manifests, admission, typed causes), §W8 (registry); SA-3 rulings R1–R5 and defects 1–5; §4b (Tier‑1 placement, `reachedModel` on GH). **Depends on:** Plans 1–5 (consumers to register).

## Global Constraints (plan-local)
- `FailureCause`, `FaultClass`, `Output.Role`, `AdmissionTier` are sum types/enums with exhaustive switches.
- The server rail never runs `go build`; Tier 1 typechecks only packages whose imports are framework-go + generated siblings.

## Review Focus
1. **Generated file outside every task's edit scope** (FIX-053): Task 6.2 `TestEditScope_EveryPlannedPathIsInSomeTaskScope`.
2. **Agent hand-edits a generated file inside scope** (drift masked): Task 6.2 `TestGate_GeneratedPathMustEqualRegeneration`.
3. **Network outage counted as NoProgress** (FIX-065): Task 6.4 `TestAgenticJob_NoFingerprintWhenModelUnreached`.
4. **Agent claims green while gate red** (FIX-054/018): Task 6.4 `TestGateRed_TruthFromRunnerResults`.
5. **A normative prompt sentence with no enforcing rule**: Task 6.5 `TestRegistry_BuiltAppRulesEnforcedOrWaived`.

---

### Task 6.1: Generator `Plan()` manifests
**Files:** `framework-go-app-generator/plan/plan.go` (`type Output struct{ Path string; Role OutputRole }`, `//sumtype:decl OutputRole: Generated | Seam`), every generator package gains `func Plan(m *projectmodel.Model, sel Selection) []plan.Output` (modelgen, clientgen, composegen, configgen, rungen, temporalgen, transportgen, testgen, webgen, nullgen); `appgen.Consumers(kind ArtifactKind) []Consumer{Name, Plan, Emit}` registry. Tests: per generator `TestPlan_MatchesEmit` (emit to temp dir; set of written paths == `Plan` Generated paths), `TestConsumers_EveryArtifactKindHasAtLeastOne`.

### Task 6.2: Task-spec derivation moves to the review Engine and reads `Plan`
**Files:** app `internal/engine/review/taskspec.go` (new; moves `scopeAndChecks`, `serviceScopeAndChecks`, `frontendScopeAndChecks`, `generatedTestPaths`, `compositionPaths`, `activityEditScope` out of `projectstateaccess.go:12416-13150`), review contract gains op `DeriveTaskSpec(project, activity) TaskSpec` (own `project.json` systemDesign + serviceContracts + dynamic views amended via MCP in the same change; `ALIGN-*` gates must pass), `cmd/aiarch-state-mcp/editscope.go` (gate also regenerates every `Generated` path in the cumulative diff to scratch and requires byte-equality), check identity `(checkID, contractKey)` everywhere (`checks.go`, `recordcheckrun.go`, `deliveryactivity.go` `joinTestRun`). Tests: `TestEditScope_EveryPlannedPathIsInSomeTaskScope` (todomvc fixture, every lifecycle type), `TestGate_GeneratedPathMustEqualRegeneration`, `TestCheckIdentity_SingleJoin`.

### Task 6.3: Tier 1 and Tier 2 admission
**Files:** platform `appgen.Consumers` (Task 6.1) + `conformance.RunChain` (Plan 1); app `internal/engine/review/admission.go` (`Admit(p, kind, tier) []Problem`): Tier 1 = for each consumer of `kind`: `Emit` to `fs.FS` in memory, parse, `go/types` with an importer limited to framework-go modules in the server's own module cache and the emitted siblings; findings split `Local` (refuse) vs `Incomplete` (warn, enforce at Tier 2) by whether the referenced id exists in the project. Called by `recordServiceContract`, `recordPhaseArtifact`, `putDraftModel` (both rails: the MCP verb layer and the delivery Manager's design rail). Tier 2 = `conformance.RunChain` over the project at `CommitArtifact`/phase-advance (venue job). Tests: `TestAdmitTier1_RefusesContractGeneratorsCannotType`, `TestAdmitTier1_IncompleteIsWarning`, `TestAdmit_AllFindingsAtOnce`, `TestAdmitTier2_RunsAtCommitOnly`.

### Task 6.4: Typed failure causes
**Files:** app `$defs` (`AgenticJobResult{reachedModel bool, cause FailureCause, gateFailures []checkID}`; `//sumtype:decl FailureCause` variants grouped by `FaultClass`: host{ModelUnreachable, RateLimited, RunnerLost, InfraTimeout} · agent{AgentGaveUp, NoProgress, ToolRefusedByPolicy} · gate{GateRed} · Unknown), `resourceaccess/agenticjob` (classification from the transport: local = Claude CLI exit/stream; GitHub = the uploaded execution log artifact — add the upload step to `aiarch-construct.yml.tmpl`; missing artifact → `Unknown`; fingerprint published only when `reachedModel`), `manager/delivery` variance policy (`switch cause.(type)` exhaustive: host → bounded wall-clock backoff then human escalation, never consumes an attempt; agent → consumes; gate → truth from recorded `CheckRun`; Unknown → consumes + surfaced), bench `classify.ts` reads the enum (bench repo change noted, not done here). Tests: `TestAgenticJob_NoFingerprintWhenModelUnreached`, `TestVariance_HostFaultNeverConsumesAttempt`, `TestGateRed_TruthFromRunnerResults`, `TestCause_SwitchExhaustive`.

### Task 6.5: Doctrine registry
**Files:** `method-assets/doctrine/registry.json` (`[{id, statement, scope: builtApp|agentConduct|judgement, enforcedBy: [ruleRef], promptRefs: [file#anchor], waiver?: {reason, owner, expires}}]`), `method-assets/doctrine/registry_test.go`: every `ruleRef` resolves to an id exported by `methodcheck.EmittedRuleIDs()` (export of `emittedRuleIDs`), `arch.RuleIDs()`, or a generator invariant test name; each referenced rule has a violating fixture (`testdata/violations/<rule>.json` or Go test named `Test<Rule>_Fires`); `builtApp` entries without `enforcedBy` fail unless waived; waiver count is asserted ≤ a checked-in `waivers.max` that only decreases; every enforced rule appears in ≥1 prompt as `[RULE-ID]`; a prompt lint flags normative sentences (`must|never|always|every|required`) lacking a `[…]` id. Seed entries: Layer A interaction policy, a11y always-on, no IdP resource (FIX-055), every Manager Temporal-hosted (FIX-035), clients have contracts (TP-CLIENT-CONTRACTED), no hooks (TP-HOOK/ARCH-TEST-COMPOSE), targets are design elements (TP-UI-TARGET), both surfaces drawn (SYS-SURFACE-ENTRY), required surfaces (SYS-SURFACE-REQUIRED), composition boots (smoke), discriminating tests (TP-DISCRIMINATE).

### Task 6.6: Release, pin, proof, paid run
- Release framework-go-app-generator + method-assets; pin; own project.json amended for the review-Engine op; `make test` green.
- Conformance: `TestConformance_Manifests` (every path the chain writes is in some `Plan`).
- Then spec §7.7: one paid todomvc run. Acceptance criterion: COMPLETE by the bench's definition, or every bench failure maps to a red archistrator gate that fired first. Record in `ledger.jsonl` and the memory file.

## Done when
All six plans released and pinned; the paid run recorded with its mapping of bench failures to archistrator gates.
