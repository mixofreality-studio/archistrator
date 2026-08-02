# Call-Chain Realization — Release Notes (founder tags these)

Branch `callchain-realization` in BOTH repos. Suggested versions (next minor of each): `framework-go v0.10.0`, `framework-go-app-generator v0.9.0`, `method-assets v0.2.0`.

## framework-go v0.10.0 (methodcheck)

- **Model:** `DynamicView` is a step-keyed use-case realization — `Steps []CallStep{ActivityNodeID, Calls []TraceCall}`; `TraceCall` adds optional `Alt` (surface-alternative groups); `ActivityNode` adds `RoleName`, `LinkedActorID`, `DecidedBy`; kinds `timeEvent`/`acceptEvent` (edge-less entries legal); `Participants`/`Edges`/`LinkedCompID` removed.
- **New rules (ALL Error):** `CC-VIEW-USECASE`, `CC-STEP-NODE`, `CC-STEP-UNIQUE`, `CC-COVERAGE`, `CC-STEP-NONEMPTY`, `CC-ENDPOINT-RESOLVES`, `CC-ACTOR-EDGE`, `CC-ACTOR-LANE`, `CC-TRIGGER-EVENT`, `CC-PATH-CONNECTED`, `CC-DECIDED-BY`, `CUC-ACTOR-REQUIRED` (clientAction UCs must declare ≥1 actor).
- **Retargeted:** `DV-EDGE-ENDS/IN-MODEL/MODE/SINGLE-MGR`, `APPC-INT-*`, `DV-STATIC-COVERAGE`, `DV-REL-COVERAGE` over step fragments. **Retired:** `DV-PART-EXIST`, `DV-PART-USED`, `DV-CHAIN-CONNECTED`.
- **`DV-REL-COVERAGE` utility-target exemption:** utility-targeted static edges are exempt; the `DV-REL-UTILITY-EXEMPT` **Info line is load-bearing** — it names every exempted edge; a deleted utility realization silently migrates into this list, so watch its count in review.
- Path walker: complete enumeration bounded by a work budget + carry (memory-bounded for decision- and fork-shaped blowups); output capped at 512, deterministic prefix; exhaustion under-approximates only (no false findings), currently silent (earmark: CC-PATH-BUDGET Info).
- `UC-ACT-PRESENT` accepts event entries; CC section grammar is key-first (`dynamicView <key> step <node>`); platform gate *messages* remain title-first by design (sections are the join key).

## framework-go-app-generator v0.9.0 (four increments)

1. modelgen: `layerContext` supports `utility` (RA-style infra emission for utility contracts).
2. temporalgen: utility deps are activity-bearing.
3. temporalgen: built-guard (contracts with empty `goPackage` never emit — prevents uncompilable output).
4. composegen: startup schedule-registration seam — `Config.ScheduleRegistrarComponent` (empty = off) + `VariantHookArgs`/`VariantConstructorNoError`; generator ERROR (not silent nil) on an arm-less registrar component.

## method-assets v0.2.0

- `the-method-architecture` step 9 rewritten: step-keyed realization authoring (fragments, actors-as-participants, entry conventions, both-surface alt rule, decidedBy ladder, verb-calls-draw/deliveries-don't, note-honesty, downstream proportionality, cross-slot renames, live Error rule table). `linkedComp` back-population DELETED. PlantUML-on-DynamicView dropped.
- `the-method-core-use-cases`: entry = start OR edge-less event node ("exactly one start" retired); trigger↔entry machine-checked; clientAction requires actors; decidedBy guidance.
- system-draft/system-critique author + review realizations. Layers/Structurizr/platform-runtime doctrine reconciled to message-bus-utility + design-health-engine.

## DOWNSTREAM MIGRATION (gtdapp, gtd-qa2, every built app) — REQUIRED READING

- **Old-shape dynamic views decode as zero-step and now ERROR (`CC-COVERAGE`) on the next System draft/commit.** Realize every view (per the new step-9 doctrine) before touching the System artifact. Design Health shows the same Errors immediately on upgrade — truthful, not broken.
- **Temporal drain surface:** activities `durableExecutionAccess.*` → `messageBus.*`; **`QueryExecutionState` and `StartOrSignalExecution` are DELETED, not renamed**; new workflow `constructionPumpSweep`. **Drain in-flight workflows before deploying**; restart workers after regen.
- Schedules now register at startup (billing hourly, operations 30s, construction pump 30s + replan 5m); construction registration is skipped under `CONSTRUCTION_DRYRUN`. Note: a schedule paused in one Temporal cluster does not carry to a fresh db.

## THE THREE-PIN DISCIPLINE (release-blocking)

`server/go.mod` carries THREE PoC-temporary replaces: `framework-go`, `framework-go-app-generator`, `method-assets`. **All three must be swapped to the released tags together.** Bumping only some silently reverts behavior with all gates green — the `.method-assets-manifest.json` has NO content hashes and still records v0.1.8, so it CANNOT detect doctrine skew (earmark: hash-based seed verification).

## Known accepted state (tracked earmarks)

Contract op renames (post-charge-only vocabulary) + edge-label pass; Resume verb; BAC restatement 1035→1060 into the next tracking cut; C-WIA fixture defect + C-BG dispatch fate (settlement re-plan); ReplanSweep v1 no-op stub now firing 5m ticks; trigger-enum validation hardening; book-audit R6/R7/R8 (volatility split-and-name, Engine census, actors[] cleanup).
