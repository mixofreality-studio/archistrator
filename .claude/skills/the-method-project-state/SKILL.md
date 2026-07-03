---
name: the-method-project-state
description: The project.json git-as-DB driver. Use whenever a construction command must read from, traverse, or update the typed project state at .aiarch/state/project.json. Teaches the slot map, common read paths, the record-then-commit write discipline, and the git-as-DB invariants.
---

# Project State (git-as-DB)

`project.json` at `.aiarch/state/project.json` is the single source of truth for the whole project. It is a typed JSON object; the Go structs in `server/internal/resourceaccess/projectstate/` are its schema of record. This skill is how a construction agent reads and updates it. Never write a parallel markdown copy of state — markdown is render-on-read only.

## The slot map

| slot | Go type | holds |
|---|---|---|
| `.mission` | `*projectstate.MissionStatement` (`models_phase1.go`) | `vision` (one sentence), numbered `objectives`, `mission` statement expressed in components |
| `.glossary` | `*projectstate.Glossary` (`models_phase1.go`) | `items[]`: term/definition/category (the Four Questions) |
| `.scrubbedRequirements` | `*projectstate.ScrubbedRequirements` (`models_phase1.go`) | `items[]`: `id`/`statement` |
| `.volatilities` | `*projectstate.Volatilities` (`models_phase1.go`) | `items[]`: name/rationale/axis (same-customer-over-time vs all-customers-at-one-time) |
| `.coreUseCases` | `*projectstate.CoreUseCases` (`models_phase1.go`) | `decisions[]`: useCase + rejectionReason ("" when core) |
| `.systemDesign` (Go field `Project.SystemDesign`, kind `KindSystem`) | `*projectstate.System` (`system.go`) | `components[]`, `relationships[]`, `dynamicViews[]` — the layered architecture (Grammar A) |
| `.operationalConcepts` | `*projectstate.OperationalConcepts` (`models_phase1.go`) | `decisions[]` (topic/decision/justifyingObjective) + `deployment` topology |
| `.standardCheck` | `*projectstate.StandardCheck` (`models_phase1.go`) | `items[]`: App C design-standard rows (section/guideline/status/justification) |
| `.planningAssumptions` | `*projectstate.PlanningAssumptions` (`models_phase2.go`) | `resources[]`, `calendarDaysPerWeek`, `infrastructureKind`, `declaredUsage`, `terms`, `notes` |
| `.activityList` | `*projectstate.ActivityList` (`models_phase2.go`) | `activities[]`: name, effortDays, workerClass, coding, riskBucket, title |
| `.network` | `*projectstate.Network` (`models_phase2.go`) | authored `dependencies[]`/`criticalPath[]`/`milestones[]`; compute-at-read `computed{}`/`summary` |
| `.normalSolution` / `.subcriticalSolution` / `.compressedSolution` / `.decompressedSolution` | `*projectstate.Solution` (`models_phase2.go`; one shared struct, `slotKind` discriminates) | `staffingCap`, `calendarDaysPerWeek`, `classRates`, `bufferDays` |
| `.riskModel` | `*projectstate.RiskModel` (`models_phase2.go`) | `rows[]`: solutionKind, criticalityRisk, activityRisk, composite |
| `.sdpReview` | `*projectstate.SdpReview` (`models_phase2.go`) | `options[]` (per-option joined duration/cost/risk/settlement row), `recommendation`, `rationale` |
| `.serviceContracts["<Component>"]` | `map[string]projectstate.ServiceContract` (`servicecontract.go`) | component, layer, goPackage, infra, deps, stub, title, `$defs`, `interface` (ops) |
| `.activityConstruction["<ActivityID>"]` | `map[string]projectstate.ActivityConstructionStatus` (`activityconstructionstatus.go`) | type, variant, phase, `phases[]`, currentPhase, startedAt/completedAt, `produced[]`, failureReason/failureDetail |
| `.constructionProgress` | `*projectstate.ConstructionProgress` (`activityconstructionstatus.go`) | Week, TotalWeeks, HandOffModel, SupervisionCap (project-level Phase-3 tracking scalars) |
| `.phaseArtifacts` | `*projectstate.PhaseArtifacts` (`phaseartifacts.go`) | maps of `srs`/`testPlan`/`integrationNote`/`uxRequirements`/`uiDesign`/`provisioningSpec`/`deployNote`/`docOutline`/`docNote` records, keyed by component/surface/resource/doc |
| `.testingState` | `*projectstate.TestingState` (`phaseartifacts.go`) | `systemTestPlan`, `harnessModule`, `perfHarness`, `qualityGates[]`, `qualityAuditReport`, `testRuns[]`, `defects[]` |
| `.reviewPolicy` | `projectstate.ReviewPolicy` (`reviewpolicy.go`) | `gatedPhasesByType`: activity-type wire name → phases requiring human approval |
| `.handoff` | *(no backing Go type found)* | Referenced by `[[the-method-handoff]]` as the committed hand-off model, but no `Handoff` struct, `Project.Handoff` field, or `ArtifactKind` exists in `server/internal/resourceaccess/projectstate/` today — treat as not yet implemented; check with the architect before assuming a shape |
| `.operatorPaused` / `.pauseReason` | `bool` / `string` | Construction pause flag + operator-supplied reason |

## Reading (you do this yourself — no pre-extraction)

There is no jq pre-extraction step in CI. You read what you need directly. Common paths:

- The activity you were dispatched for: `jq '.activityList.activities[] | select(.name=="<ACTIVITY_ID>")' .aiarch/state/project.json`
- Its service contract (if a component build): `jq '.serviceContracts["<COMPONENT_ID>"]' .aiarch/state/project.json`
- A neighbour's contract (for integration/detailed-design): look up inbound/outbound parties in `.systemDesign` relationships, then read each `.serviceContracts[<neighbour>]`.
- The current review policy / handoff model: `.reviewPolicy`, `.handoff`.

Prefer reading the smallest slice that answers your question; you may run several `jq` reads.

## Updating (record the artifact, then commit)

When your phase produces an artifact that lives in state (e.g. a service contract, a UI-design concept, a phase-artifact note), write it into its typed slot and `git commit` it onto the activity branch. Each artifact maps to a slot:

- service contract -> `.serviceContracts["<component>"]`
- UI-design concept -> `.phaseArtifacts.uiDesign["<surface>"]`
- integration note -> `.phaseArtifacts.integrationNote`
- (code artifacts are files under `server/internal/...`, not state slots)

Write valid typed JSON matching the Go struct for that slot (field names + shapes exactly). Do not invent fields. After writing, commit with a message naming the activity + phase.

## Status is NOT yours

Do not write phase start/exit status or earned-value fields. The Manager (orchestrator) owns `.activityConstruction[...]` status transitions and the review gate; you only write the phase's *artifact*.

## Invariants

- `project.json` is the source of truth; commit after every state write.
- One artifact per phase, into its one slot (or as code).
- Never edit `*/generated/`.
- If a slot's shape is unclear, read the backing Go struct in `projectstate/` rather than guessing.
