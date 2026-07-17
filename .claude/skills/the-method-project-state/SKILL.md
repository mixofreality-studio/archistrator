---
name: the-method-project-state
description: The project.json git-as-DB driver. Use whenever a construction command must read from, traverse, or update the typed project state at .aiarch/state/project.json. Teaches the slot map, common read paths, the aiarch-state MCP write tools (state changes go through the tools, not hand-edits), and the git-as-DB invariants.
---

# Project State (git-as-DB)

`project.json` at `.aiarch/state/project.json` is the single source of truth for the whole project. It is a typed JSON object; the Go structs in `internal/resourceaccess/projectstate/` are its schema of record. Never write a parallel markdown copy of state — markdown is render-on-read only.

> **STATE CHANGES GO THROUGH THE `aiarch-state` MCP TOOLS — NOT hand-edits.**
> A Method CI job — a design-rail job (job mode `draft` / `critique` / `answer`) or a construction job (job mode `construct`) — runs the `aiarch-state` MCP server. Record your phase's artifact through its tools and let `publishDraft` commit it. **Do NOT hand-edit `project.json` and do NOT run `git` yourself for state.** The tools validate every write through the full server codec **and** the Method CI rules *before* it lands, so a malformed contract/artifact is rejected in-loop with an actionable error instead of committed to stall the rail. The ambient kind/component/activity already fix your target — you never choose a slot.
>
> The direct `jq`/`git` editing described below is retained only for **humans and debugging** (inspecting or hand-repairing state), not for agents on the construction rail.

The on-disk JSON is produced by the Go codec (`EncodeProjectJSON`/`DecodeProjectJSON` in `artifactmodel.go`). The `aiarch-state` write tools reuse that exact codec, so a write that survives them is byte-for-byte what the server accepts on read-back.

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
| 0 | Mission | `*projectstate.MissionStatement` (`models_phase1.go`) | `vision`, numbered `objectives`, `mission` statement (the business-language "how"; see [[the-method-business-alignment]]) |
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

Prefer reading the smallest slice that answers your question; you may run several `jq` reads. (The `aiarch-state` `getCommittedSlot` tool returns a committed artifact's typed model directly if you'd rather read through the tool than `jq`.)

## Updating (record the artifact through the `aiarch-state` tools)

When your phase produces an artifact that lives in state, record it through the matching `aiarch-state` MCP tool, then call `publishDraft` (once, last) to commit and push it onto your activity branch. **You do not hand-edit `project.json` and you do not run `git`.** Each artifact maps to a tool + target:

| artifact | tool | target |
|---|---|---|
| service contract (detailed-design) | `recordServiceContract` | `.serviceContracts["<ambient component>"]` |
| UI-design concept / SRS / integration note / provisioning spec / deploy note / doc outline·note | `recordPhaseArtifact` (set exactly one payload field, pass the `mapKey`) | `.phaseArtifacts.<field>["<mapKey>"]` |
| testing plan / results (system test plan, harness, quality gate, test run, defect, audit report) | `recordTestingState` (set exactly one payload field) | `.testingState.<field>` |
| code | *(files, not state)* | files in the package the contract's `goPackage` names |

The tool payload is the typed Go struct for that target (field names + shapes exactly — read the backing struct in `projectstate/` if unsure); the tool rejects invented/malformed fields before writing. `recordServiceContract` uses your ambient component; `recordPhaseArtifact`/`recordTestingState` use your ambient activity. A rejected write tells you exactly what to fix — correct it and call the tool again. When every artifact is recorded, call `publishDraft`.

If a specific raw ResourceAccess/Engine operation is in your task's tool allowlist, call it by its generated tool name; anything not covered by a tool is a code file, not a state edit.

## Design-rail jobs (job modes `draft` / `critique` / `answer`)

The `record*` verbs above drive Phase-3 construction. Phase-1/2 design artifacts run on the **design rail**, where the ambient `AIARCH_ARTIFACT_KIND` + `AIARCH_JOB_MODE` env fix the target slot and which verbs are in play — you never choose a slot or a kind. The reads (`getCommittedSlot`, `getDraftSlot`, `getReviewThread`, `getCritique`) are common to every mode; the write verb is the mode's:

- **`draft`** — read the basis with `getCommittedSlot`/`getDraftSlot` and the ledger with `getReviewThread` and `getCritique` (if it carries a `revise` verdict, its notes are the PM's revision guidance and your draft must address them), then author the typed model with `putDraftModel` (it validates through the full server codec **and** the Method CI rules, returning actionable errors — fix and resubmit until accepted). Answer every open ledger comment with `respondToReviewComment`, then finish with `publishDraft` exactly once.
- **`critique`** — record the verdict (approve/revise + comments) with `setCritiqueVerdict`, which writes the carrier `getCritique` reads back. A critique **never rewrites the model** — there is no `putDraftModel` in this mode. Then `publishDraft` exactly once.
- **`answer`** — answer the founder's open questions with `respondToReviewComment` only; no model write and no verdict. Then `publishDraft` exactly once.

Same invariants as the construct rail: one `publishDraft`, never hand-edit `project.json`, never run `git` for state, never touch `.activityConstruction` / `.constructionProgress` / `.reviewPolicy`.

## Status is NOT yours

Do not write phase start/exit status or earned-value fields. The Manager (orchestrator) owns `.activityConstruction[...]` status transitions, `.constructionProgress`, and `.reviewPolicy` — you only write the phase's *artifact*.

## Invariants

- `project.json` is the source of truth; **state changes go through the `aiarch-state` tools** (`recordServiceContract`/`recordPhaseArtifact`/`recordTestingState`), then `publishDraft` commits — you never hand-edit the file or run `git` for state.
- One artifact per phase, into its one target (or as code).
- Never write `.activityConstruction`, `.constructionProgress`, or `.reviewPolicy` — those are Manager-owned (there is no tool for them).
- Never edit `*/generated/`.
- If a payload's shape is unclear, read the backing Go struct in `projectstate/` rather than guessing; the tool will also reject a malformed payload with the exact fix.
