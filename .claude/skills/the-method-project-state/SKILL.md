---
name: the-method-project-state
description: The project.json git-as-DB driver. Use whenever a construction command must read from, traverse, or update the typed project state at .aiarch/state/project.json. Teaches the slot map, common read paths, the record-then-commit write discipline, and the git-as-DB invariants.
---

# Project State (git-as-DB)

`project.json` at `.aiarch/state/project.json` is the single source of truth for the whole project. It is a typed JSON object; the Go structs in `server/internal/resourceaccess/projectstate/` are its schema of record. This skill is how a construction agent reads and updates it. Never write a parallel markdown copy of state — markdown is render-on-read only.

The on-disk JSON is produced by the Go codec (`EncodeProjectJSON`/`DecodeProjectJSON` in `artifactmodel.go`). When writing a slot, match the exact struct JSON shape for that artifact — read the backing Go struct in `projectstate/` if unsure.

## Storage is dual: flat keys + a `.slots` map

`project.json` does **not** put every artifact at its own top-level key. There are two storage shapes:

1. **Flat top-level keys** — read/write directly, no ordinal involved.
2. **`.slots["<ordinal>"].model`** — Phase-1/Phase-2 artifacts only, keyed by the integer `ArtifactKind` ordinal (as a string, `"0"`..`"16"`) from `identity.go`. Each entry is `{status, kind, model}`; the artifact itself is under `.model`.

### Flat top-level keys

| key | Go type | read/write | holds |
|---|---|---|---|
| `.serviceContracts["<component-id>"]` | `map[string]projectstate.ServiceContract` (`servicecontract.go`) | READ + WRITE (detailed-design writes here) | component, layer, goPackage, infra, deps, stub, title, `$defs`, `interface` (ops) |
| `.activityConstruction["<activity-id>"]` | `map[string]projectstate.ActivityConstructionStatus` (`activityconstructionstatus.go`) | READ-ONLY — Manager-owned | activityID, phase, buildStatus, `produced[]`, failure info |
| `.constructionProgress` | `*projectstate.ConstructionProgress` (`constructionprogress.go`) | READ-ONLY — Manager-owned | Week, TotalWeeks, HandOffModel, SupervisionCap (earned-value rollup) |
| `.testingState` | `*projectstate.TestingState` (`phaseartifacts.go`) | READ + WRITE (testing phases) | `systemTestPlan`, `harnessModule`, `perfHarness`, `qualityGates[]`, `qualityAuditReport`, `testRuns[]`, `defects[]` |
| `.phaseArtifacts` | `*projectstate.PhaseArtifacts` (`phaseartifacts.go`) | WRITE (non-contract phase artifacts) | maps of `srs`/`testPlan`/`integrationNote`/`uxRequirements`/`uiDesign`/`provisioningSpec`/`deployNote`/`docOutline`/`docNote` records, keyed by component/surface/resource/doc. `omitempty`/nil until the first artifact is produced — it may be **absent** in a fresh file. The write verb is `RecordPhaseArtifactProduced`. |
| `.reviewPolicy` | `projectstate.ReviewPolicy` (`reviewpolicy.go`) | READ-ONLY — Manager-owned via `UpdateReviewPolicy` | `gatedPhasesByType`: activity-type wire name → phases requiring human approval |
| `.research` | raw research corpus (`research.go`) | READ-ONLY | source material feeding Phase-1 |
| `.phase`, `.version`, `.id`, `.name`, `.owner`, `.updatedAt` | scalars | metadata | project-level bookkeeping |

There is **no** `.handoff` slot. Worker-class / hand-off is decided by the Manager and is not stored in `project.json` as its own artifact (see `.constructionProgress.HandOffModel` for the currently-active model, which is Manager-owned).

### `.slots["<ordinal>"].model` — Phase-1/Phase-2 artifacts

| ordinal | slot name | Go type | holds |
|---|---|---|---|
| 0 | Mission | `*projectstate.MissionStatement` (`models_phase1.go`) | `vision`, numbered `objectives`, `mission` statement expressed in components |
| 1 | Glossary | `*projectstate.Glossary` (`models_phase1.go`) | `items[]`: term/definition/category (the Four Questions) |
| 2 | ScrubbedRequirements | `*projectstate.ScrubbedRequirements` (`models_phase1.go`) | `items[]`: `id`/`statement` |
| 3 | Volatilities | `*projectstate.Volatilities` (`models_phase1.go`) | `items[]`: name/rationale/axis |
| 4 | CoreUseCases | `*projectstate.CoreUseCases` (`models_phase1.go`) | `decisions[]`: useCase + rejectionReason ("" when core) |
| 5 | System | `*projectstate.System` (`system.go`) | `components[]`, `relationships[]`, `dynamicViews[]` — the layered architecture (Grammar A). Component ids here are kebab-case (e.g. `billing-engine`) — note this differs from the camelCase keys used in `.serviceContracts` (e.g. `billingEngine`). |
| 6 | OperationalConcepts | `*projectstate.OperationalConcepts` (`models_phase1.go`) | `decisions[]` (topic/decision/justifyingObjective) + `deployment` topology |
| 7 | StandardCheck | `*projectstate.StandardCheck` (`models_phase1.go`) | `items[]`: App C design-standard rows (section/guideline/status/justification) |
| 8 | PlanningAssumptions | `*projectstate.PlanningAssumptions` (`models_phase2.go`) | `resources[]`, `calendarDaysPerWeek`, `infrastructureKind`, `declaredUsage`, `terms`, `notes` |
| 9 | ActivityList | `*projectstate.ActivityList` (`models_phase2.go`) | `activities[]`: name, effortDays, workerClass, coding, riskBucket, title |
| 10 | Network | `*projectstate.Network` (`models_phase2.go`) | authored `dependencies[]`/`criticalPath[]`/`milestones[]`; compute-at-read `computed{}`/`summary` |
| 11 | NormalSolution | `*projectstate.Solution` (`models_phase2.go`) | `staffingCap`, `calendarDaysPerWeek`, `classRates`, `bufferDays` |
| 12 | SubcriticalSolution | `*projectstate.Solution` (`models_phase2.go`; shared struct, `slotKind` discriminates) | same shape as NormalSolution |
| 13 | CompressedSolution | `*projectstate.Solution` (`models_phase2.go`) | same shape as NormalSolution |
| 14 | DecompressedSolution | `*projectstate.Solution` (`models_phase2.go`) | same shape as NormalSolution |
| 15 | RiskModel | `*projectstate.RiskModel` (`models_phase2.go`) | `rows[]`: solutionKind, criticalityRisk, activityRisk, composite |
| 16 | SdpReview | `*projectstate.SdpReview` (`models_phase2.go`) | `options[]` (per-option joined duration/cost/risk/settlement row), `recommendation`, `rationale` |

## Reading (you do this yourself — no pre-extraction)

There is no jq pre-extraction step in CI. You read what you need directly. Common paths — Phase-1/2 artifacts go through `.slots["<ordinal>"].model`, everything else is flat:

- The dispatched activity: `jq '.slots["9"].model.activities[] | select(.name=="<ACTIVITY_ID>")' .aiarch/state/project.json`
- Its service contract: `jq '.serviceContracts["<COMPONENT_ID>"]' .aiarch/state/project.json`
- The system design (components, relationships, views): `jq '.slots["5"].model' .aiarch/state/project.json`
- Neighbour discovery for integration/detailed-design: read `.slots["5"].model.relationships`, find the inbound/outbound component ids for your component (kebab-case), then read each `.serviceContracts[<neighbour>]` (camelCase key).
- Core use cases: `jq '.slots["4"].model' .aiarch/state/project.json`; Mission: `jq '.slots["0"].model' .aiarch/state/project.json`.

Prefer reading the smallest slice that answers your question; you may run several `jq` reads.

## Updating (record the artifact, then commit)

When your phase produces an artifact that lives in state (e.g. a service contract, a UI-design concept, a phase-artifact note), write it into its typed target and `git commit` it onto the activity branch. Each artifact maps to a target:

- service contract -> `.serviceContracts["<component>"]` (flat)
- UI-design concept -> `.phaseArtifacts.uiDesign["<surface>"]` (flat; verb `RecordPhaseArtifactProduced`)
- integration note -> `.phaseArtifacts.integrationNote` (flat)
- testing plan/results -> `.testingState` (flat)
- code artifacts are files under `server/internal/...`, not a state slot

Write valid typed JSON matching the Go struct for that target (field names + shapes exactly). Do not invent fields. After writing, commit with a message naming the activity + phase.

## Status is NOT yours

Do not write phase start/exit status or earned-value fields. The Manager (orchestrator) owns `.activityConstruction[...]` status transitions, `.constructionProgress`, and `.reviewPolicy` — you only write the phase's *artifact*.

## Invariants

- `project.json` is the source of truth; commit after every state write.
- One artifact per phase, into its one target (or as code).
- Never write `.activityConstruction`, `.constructionProgress`, or `.reviewPolicy` — those are Manager-owned.
- Never edit `*/generated/`.
- If a slot's shape is unclear, read the backing Go struct in `projectstate/` rather than guessing.
