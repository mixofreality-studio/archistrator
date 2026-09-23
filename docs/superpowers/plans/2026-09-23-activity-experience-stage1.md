# Activity Experience — Stage 1 (The Model Wave) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Amend archistrator's own architecture model (`.aiarch/state/project.json`) so that the system it describes is the one the spec ruled: ONE **Project Delivery Workflow** volatility where three phase-workflow volatilities stood, ONE `delivery-manager` component (contract `deliveryManager`, 12 ops) encapsulating it, and THREE core use cases where five stood — `execute-a-project-activity` absorbing `drive-system-design`, `commit-to-a-project-option` and `execute-a-construction-activity`, with every non-core use case re-parented and every one of them still carrying its own activity diagram and its own step-keyed dynamic view. Planning established that only the volatility merge can ship ahead of the Go implementation; read the blocker box below before anything else.

**Architecture:** This is a MODEL wave: the unit of work is a hand edit to `.aiarch/state/project.json` followed by the self-amendment gate loop, and the only Go that moves is one rule in the estimation engine. Task 1 retires three volatilities into one and re-points every join that named them — the typed `encapsulatesVolatilities` edge is what makes a half-done merge impossible to commit. Task 4 encodes the founder's ruling that a `buildStatus: "planned"` component must not become a dispatchable construction activity: the estimation engine cannot see `buildStatus` at all today (`estimation.SystemComponent` has no such field), and the pump has no secondary "should we build this" gate, so a planned Manager would be handed to a build agent by the 30-second sweep. Tasks 2 and 3 carry the `delivery-manager` design itself — component, 12-op contract, the collapsed core use case and every realization — into the wave where it can be green.

> ### ⚠️ BLOCKER FOUND DURING PLANNING — read before executing anything
>
> The spec's §8 gate recon is **incomplete**, and the stage-1 row as written **cannot be made green**. Four gates, three of them reproduced empirically against the live committed state, close every door at once:
>
> | # | Move | Gate | Severity | Evidence |
> |---|---|---|---|---|
> | 1 | Keep the three Managers, add `delivery-manager` (`planned`) | **`SYS-CARD-MGR`** | **Error** | REPRODUCED: `system has 6 Managers; The Method limits a system to 5 Managers without introducing subsystems`. `framework-go/methodcheck/rules_system.go:310–331` counts `c.Kind == kindManager` with **no `buildStatus` exemption**, and `applyWaivers` (`rules_appc.go:356–368`) downgrades **Warning** findings only, in the APPC family only — there is no waiver path for an Error, and the `standardCheck` slot the waiver set is read from was torn down in 2026-07. |
> | 2 | Delete the three Managers instead | **`ALIGN-EXTRA-PKG`** ×3 | **Error** | `align.go:529–552` emits one per unmatched `(normalizedLeaf, layer)` code package. This is the spec's OWN §8 reason for keeping them. |
> | 3 | Collapse the core use cases without `delivery-manager` | **`DV-SINGLE-MGR`** | **Error** | `rules_dynamic.go:316–337`: a view whose Client edges enter more than one **distinct** Manager. The unified `execute-a-project-activity` view enters `construction-manager` (pump), `system-design-manager` (draft/decision) and `project-design-manager` (SDP) — three. |
> | 4 | Author the `deliveryManager` contract alone, no component | **`DH-CONTRACT-FACET`** | **Error** | REPRODUCED: `service contract "deliveryManager" resolves to no component: its key is not a component contract key and its component field "deliveryManager" names no component`. |
>
> Separately, REPRODUCED: `activityExecutionAccess` as a `planned` RA with **no** relationships fires `SYS-RA-ORPHAN` (**Error**: "every ResourceAccess must encapsulate at least one resource"); with a relationship it fires `DV-REL-COVERAGE` (**Error**) until a dynamic view exercises that relationship — which cannot be authored honestly while no code makes the call.
>
> And: the unified view must exercise the **twelve** relationships that today have no exerciser but the three views it replaces (`jq` inventory in Task 3), so routing it through `delivery-manager` orphans nine Manager-rooted ones under `DV-REL-COVERAGE` (Error each) in the same breath.
>
> **Conclusion.** The `delivery-manager` component, the `deliveryManager` contract, its relationships, the core-use-case collapse and every re-keyed dynamic view are **one indivisible commit**, and that commit is only green **together with the deletion of the three old Manager components** — i.e. it is the head of **stage 4**, not stage 1. The one part of spec §4 that ships independently is the volatility merge (Task 1 — verified green by probe).
>
> **What this plan therefore does.** Task 1 and Task 4 are executable in stage 1 today and land now. Task 2 and Task 3 are written in full, executable form — the authored JSON, the contract, the diagrams, the views — and are **gated on a founder ruling** (Task 2 Step 0) that moves them into stage 4's commit. Task 5 amends the spec and records the earmark. Nothing designed here is wasted; only its landing point moves.

**Tech Stack:** Go 1.26 (`GOWORK=off` always), `.aiarch/state/project.json` as the model database (git-as-DB), modelgen/clientgen/appgen codegen, `framework-go` methodcheck + the in-repo `designhealth` engine, React 19 + TypeScript for the SPA schema, Playwright fixtures in `uitests/`.

**Spec:** `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` (§1 R3/R4, §4 Architecture model, §5.3 execution data model, §8 stage-1 row + "must-ship-together" + the self-amendment procedure). Executors read both. Precedent for a self-amendment wave: `docs/superpowers/plans/2026-07-31-callchain-rollout.md`.

## Global Constraints

- Work in the git worktree `.claude/worktrees/activity-stage1` (branch `activity-experience-stage1`, from `main` @`12f21f2c`). The main checkout is shared with other sessions. Create it with `git worktree add .claude/worktrees/activity-stage1 -b activity-experience-stage1 main`, then `cd webApp && npm install` inside it (a fresh worktree has no `node_modules`) and `GOLANGCI_LINT_CACHE=$(mktemp -d)` for the first `make lint` (the shared cache keys on absolute paths).
- `GOWORK=off` on every `go`/`make` command under `server/`. Gates run against PINNED platform tags, never a `replace`.
- **Slot 5 (System) edits ARE sanctioned in this wave** — unlike stage 2, this plan hand-edits `.slots["5"].model.components`, `.relationships`, `.dynamicViews` and `.waivers`, plus `.slots["2"].model.items[].volatilityHint`, `.slots["3"].model`, `.slots["4"].model.decisions` and `.serviceContracts`. **Slots 9 and 10 are still TOOL-ONLY** — never hand-edited; they move by `make derived-plan-write` alone, and Tasks 1 and 4 both expect that command to be a NO-OP (Task 4 proves it).
- **The three old Manager components stay in slot 5 until the commit that both adds `delivery-manager` and deletes them.** `ALIGN-EXTRA-PKG` (Error) fires the moment `internal/manager/{systemdesign,projectdesign,construction}` has no component; `SYS-CARD-MGR` (Error) fires the moment a sixth exists. There is no ordering in which the two can be separate commits.
- A contract with no `goPackage` generates nothing (`cmd/modelgen/main.go:21–24` — "Entries without a `goPackage` are skipped"), and `cmd/clientgen/main.go:62` `exposedManagers` / `cmd/appgen/main.go:155` `WebExposedManagers` are explicit key lists, not derived — so Tasks 1 and 4 touch no generated file at all. Confirm with `git status --short -- server/internal` after `make gen-models`.
- **A `planned` component must not become a dispatchable construction activity.** The pump sweep (`server/internal/manager/construction/constructionmanager.go:1449–1476`) dispatches any slot-9 activity whose deps are Done, with no secondary "should we build this" gate; a derived `C-delivery-manager` would be handed to an agent every 30 s. Task 4 makes `buildStatus: "planned"` a first-class skip in the derivation and pins it with tests.
- **`required` in the contract schema dialect is PRESENCE-only.** Non-emptiness lives in the Go implementation, never in `minLength` (2026-08-13 contract-strictness ruling; `ValidateModelIdentities` + the paramguard arch gate hold the line).
- **Must ship together (spec §8):** the volatility merge, the component + contract + relationships, and EVERY use-case realization land in ONE commit per task, and Tasks 1–3 may not be left half-applied across a green gate run — a use case with an activity diagram and no dynamic view is an Error. Task 3 in particular is one atomic commit: decisions + dynamic views together.
- Never weaken, skip, or allowlist around a gate; no `//nolint`. If a gate is red, fix the cause or stop and report.
- Never run `git restore`, `git clean`, `git stash`, `git checkout -- <path>` or any tree-wide reset. Back up the gitignored SDD ledger (`.superpowers/sdd/`) to the session scratchpad after every append.
- `TestFileLayout`: one impl file + ONE test file per package; no new `.go` files inside existing packages. Task 4's Go edits go in `estimationengine.go` / `engine_test.go` / `projectdesignmanager.go` only.
- Wire-visible identifiers are NEVER renumbered: `ArtifactKind` ordinals, `ActivityType` ordinals, the root `phase`, and the `B-*` behavior ids in `.slots["2"]`. The `B-*` ids are behavior ids, not volatility ids — the volatilities are keyed by NAME (`.slots["3"].model.items[].name`, joined from `Component.encapsulatesVolatilities` and `.slots["2"].model.items[].volatilityHint`), so "retire or alias the old B-ids" is a non-question: no B-id changes at all.
- Match the surrounding JSON idiom exactly: key order, prose register, the `— …` em-dash rationale style. Commit messages end with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- Out of scope, EARMARK only: `.claude/` materialized skills that name the three managers (they are platform method-assets content — a platform release is its own founder STOP); the `uitests` `check:core-use-cases-fixture` script (Task 5 — it reads the **committed `main` branch**, not the worktree, so it cannot be made green before merge); deleting the three Manager components, their contracts, or any Go implementation (stage 4).

## Task order

**Lands in stage 1 now:** Task 1 (volatility merge) → Task 4 (the `planned`-skips-derivation engine rule) → Task 5 (spec amendment + earmarks).

**Written here, lands at the head of stage 4** (gated on the Task 2 Step 0 ruling): Task 2 (`delivery-manager` component + `deliveryManager` contract + relationships + deletion of the three old Managers) → Task 3 (the core-use-case collapse and every dynamic view, ONE commit with Task 2).

Task 1 must precede Task 2 whichever wave Task 2 lands in: Task 2's component joins `DH-VOL-ENCAP-MISSING` and `DH-COMP-VOL-DANGLING` against the merged volatility name Task 1 creates. Task 4 must precede Task 2 too — it is the rule that stops Task 2's component from being handed to a build agent by the pump.

---

### Task 1: Three phase-workflow volatilities become one Project Delivery Workflow

Spec §4's first bullet. Today `.slots["3"].model.items` holds **19** volatilities, three of which say the same thing in three tenses — "System Design Phase Workflow" (`items[0]`, traces B-02/B-03), "Project Design Phase Workflow" (`items[1]`, B-04) and "Construction Phase Workflow" (`items[2]`, B-13/B-14). R1 says Requirements, Architecture and Project Design are activities at the head of the SAME plan the construction activities live in, so the three sequences are one sequence and the axis they vary on is one axis. They merge into **Project Delivery Workflow**.

`designHealthEngine` is the one component that owned a phase-workflow volatility without owning its workflow: `components[14].encapsulatesVolatilities` is `["System Design Phase Workflow"]`, and slot 5's §2d waiver calls it "a shared System Design Phase Workflow facet with the Manager (workflow choreography vs rule evaluation)". Pointing it at the merged delivery volatility would make it a facet of a workflow it has nothing to do with; deleting the component is stage 4's engines 7→4 work and would fire `ALIGN-EXTRA-PKG` today (`server/internal/engine/designhealth` exists). The smallest consistent model change is the one the 08-30 ruling already implies: its volatility is **which Method rules judge a draft**, which evolves with the Method doctrine release over release and has nothing to do with delivery choreography. So it gets a stand-alone **Method Conformance Rules** volatility, and its shared-facet waiver becomes a true 1:1.

Net: 19 volatilities → **18** (−3 merged, +1 stand-alone).

**On the old `B-*` ids: nothing is retired and nothing is aliased.** `B-02 … B-14` are *behavior* ids in `.slots["2"].model.items[].id`, not volatility ids. The volatility↔behavior join is `.slots["3"].model.items[].traces` → behavior id (`volatilityTraceFindings`, `designhealthengine.go` — `DH-VOL-TRACE`, SeverityError, a trace that resolves to no requirement id is a finding), and the volatility↔component join is by **exact name** (`typedEncapsulationFindings`, `designhealthengine.go:2301`). Both joins survive a rename as long as the union of traces is carried and every referring name is re-pointed in the SAME edit. No behavior id moves.

**Files** (all in `/Users/davidmarne/mixofrealitystudio/archistrator/.aiarch/state/project.json`; hand-edited, per the self-amendment procedure):
- `slots["3"].model.items[0]` (`"name": "System Design Phase Workflow"`) — rewritten in place as the merged volatility.
- `slots["3"].model.items[1]` (`"Project Design Phase Workflow"`) and `items[2]` (`"Construction Phase Workflow"`) — deleted; a new `"Method Conformance Rules"` item takes position 1 so the two design-time volatilities stay adjacent. `items` goes 19 → 18.
- `slots["3"].model.rejected` — three `"class": "foldedInto"` entries appended (precedent: the three 2026-07-22 folds already in that array).
- `slots["3"].model.waivers[0].justification` — the §2h count sentence, "19 volatilities — 13 same-customer-over-time, 6 all-customers-at-one-moment".
- `slots["2"].model.items[1]` (B-02), `[2]` (B-03), `[3]` (B-04), `[12]` (B-13), `[13]` (B-14) — `volatilityHint` entries re-pointed.
- `slots["5"].model.components[3]` (`system-design-manager`), `[4]` (`project-design-manager`), `[5]` (`construction-manager`) — `encapsulatesVolatilities` and the `encapsulates` blurb.
- `slots["5"].model.components[14]` (`design-health-engine`) — same two fields.
- `slots["5"].model.waivers[0].justification` — the design-health-engine sentence in the §2d Engines-to-Managers waiver.

**Interfaces:**
- Consumes: `designhealth.Input.Slots.Volatilities` (names + traces), `.encapsulatesVolatilities` (the typed join), `.encapsulatesBlurbs` (the fallback substring join), `requirementIDs()`.
- Produces: volatility names `"Project Delivery Workflow"` and `"Method Conformance Rules"`, owned by `system-design-manager`/`project-design-manager`/`construction-manager` and `design-health-engine` respectively.
- Removes: volatility names `"System Design Phase Workflow"`, `"Project Design Phase Workflow"`, `"Construction Phase Workflow"` (and every reference to them anywhere in `project.json` — there are exactly 8 + 4 + 5 = 17 occurrences today; `grep -o` them to zero at Step 6).

- [ ] **Step 1: Make the gate fail for the right reason first.** Delete `slots["3"].model.items[1]` and `items[2]` and NOTHING else, then run the design-rule gate:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage1/server
  GOWORK=off make method-check 2>&1 | grep -E 'DH-COMP-VOL-DANGLING|DH-VOL-ENCAP-MISSING|FAIL'
  ```
  Expected: two `DH-COMP-VOL-DANGLING` Errors (`component "project-design-manager" lists "Project Design Phase Workflow" in encapsulatesVolatilities, which is not a committed volatility name`; the same for `construction-manager`) and a FAIL. This is the shape of the whole task: the typed join is what makes a half-done merge impossible to commit.
  - [ ] **Verify first:** confirm `DH-COMP-VOL-DANGLING` is reported by `make method-check` and not only by the live Design Health screen — `designhealthengine.go` is the LIVE tier. If `make method-check` does not carry the DH-* family, use the engine's own test instead and say so: `GOWORK=off go test ./internal/engine/designhealth/ -run TestGreenFixture -count=1 -v`.

- [ ] **Step 2: Write the merged volatility.** Replace `slots["3"].model.items[0]` wholesale (key order copied from its siblings: `name`, `rationale`, `axis`, `traces`):
  ```json
  {
    "name": "Project Delivery Workflow",
    "rationale": "How a project's activities are scheduled and how one activity's task DAG advances: dispatch → collect → review → gate → advance. System design, project design and construction were authored as three separate phase-workflow volatilities, which was a description of the 2026-07 implementation (three Managers, three rails, three UIs) rather than of the business: an agent generates an artifact, agents and humans review it, and the activity advances through its own life cycle (Righting Software App. A) — identically for a Requirements activity, an Architecture activity and a Service activity. Requirements, Architecture and Project Design are activities 1–3 of the project network (Table 11-1), fenced from construction by milestone M0, so there is ONE sequence and ONE axis of change along it: the lifecycle shape per activity type, who must review what, when a human is required, how a send-back re-opens work, and how the pump chooses what runs next. All of that evolves for every customer at once as the Method practice matures, and none of it evolves independently of the rest — the reason three entries were always one.",
    "axis": "sameCustomerOverTime",
    "traces": [
      "B-02",
      "B-03",
      "B-04",
      "B-13",
      "B-14"
    ]
  }
  ```
  The `traces` array is the exact UNION of the three retiring entries' traces, in behavior order, so `DH-VOL-TRACE` keeps resolving and no behavior loses its volatility.

- [ ] **Step 3: Write the stand-alone Method Conformance Rules volatility** as the new `slots["3"].model.items[1]` (taking the slot the deleted Project Design entry vacated):
  ```json
  {
    "name": "Method Conformance Rules",
    "rationale": "WHICH Method rules judge a draft, and HOW their findings are computed: the rule inventory (counts, layer rules, chain coverage, traceability joins, design↔code alignment, drift) evolves with the Method doctrine release over release, independently of the delivery choreography that asks for the verdict. It was authored as an evaluation facet of the System Design Phase Workflow volatility, which was only ever true of the rail that happened to call it; once the three phase workflows merge, a rule inventory is plainly not a facet of how activities are scheduled. Stands alone so DesignHealthEngine's 1:1 volatility-to-encapsulator relationship is real rather than a shared-facet waiver. Platform-wide, so axis 1.",
    "axis": "sameCustomerOverTime",
    "traces": [
      "B-02",
      "B-03"
    ]
  }
  ```

- [ ] **Step 4: Record the fold in the rejected ledger.** Append to `slots["3"].model.rejected`, matching the existing entries' key order (`name`, `reason`, `class`):
  ```json
  {
    "name": "System Design Phase Workflow",
    "reason": "Merged into Project Delivery Workflow (2026-09-23, spec §4): system design, project design and construction are one activity sequence fenced by M0 (R1), so their choreography varies on one axis, not three. Its rule-evaluation facet — which Method rules judge a draft — split out as the stand-alone Method Conformance Rules volatility instead of riding along.",
    "class": "foldedInto"
  },
  {
    "name": "Project Design Phase Workflow",
    "reason": "Merged into Project Delivery Workflow (2026-09-23, spec §4): under R7 project design is deterministic — one review task (SDP Review · M0) over an engine-computed artifact — so there is no separate drafting choreography left to vary. The estimation model it used to carry remains its own volatility (Construction Estimation Model).",
    "class": "foldedInto"
  },
  {
    "name": "Construction Phase Workflow",
    "reason": "Merged into Project Delivery Workflow (2026-09-23, spec §4): per-activity execution choreography is the SAME choreography the design activities run, differing only in data (the lifecycle) and strategy (command, worker class, artifact codec) — never in a branch on design-vs-construction (spec §5.2).",
    "class": "foldedInto"
  }
  ```

- [ ] **Step 5: Re-point every reference, in the same edit.** Five behaviors and four components:

  `slots["2"].model.items[].volatilityHint` — replace the old name in place, keeping each array's remaining entries and their order:
  | index | id | before | after |
  |---|---|---|---|
  | 1 | B-02 | `["System Design Phase Workflow"]` | `["Project Delivery Workflow"]` |
  | 2 | B-03 | `["System Design Phase Workflow","Review Policy"]` | `["Project Delivery Workflow","Method Conformance Rules","Review Policy"]` |
  | 3 | B-04 | `["Project Design Phase Workflow","Construction Estimation Model"]` | `["Project Delivery Workflow","Construction Estimation Model"]` |
  | 12 | B-13 | `["Construction Phase Workflow","Agentic Job Venue","Source Control Target"]` | `["Project Delivery Workflow","Agentic Job Venue","Source Control Target"]` |
  | 13 | B-14 | `["Construction Phase Workflow","Intervention Policy","Review Policy","Workflow Execution Substrate"]` | `["Project Delivery Workflow","Intervention Policy","Review Policy","Workflow Execution Substrate"]` |

  B-03 gains `Method Conformance Rules` because B-03 is the behavior about gating progression on artifact validity — the only behavior the rule inventory serves directly; it is also why the new volatility traces B-02/B-03 rather than nothing.

  `slots["5"].model.components[3]` (`system-design-manager`):
  ```json
  "encapsulates": "Encapsulates the Project Delivery Workflow volatility (B-02, B-03) for the Phase-1 activities: the artifact co-authoring sequence, the per-artifact human review gate, back-edge amendments with staleness propagation/acknowledgement, and the Phase-1 seal. A shared owner with ProjectDesignManager and ConstructionManager until stage 4 of the unified-activity-experience wave replaces all three with DeliveryManager — the doctrine allows a ratified facet group, and this one is explicitly transitional.",
  "encapsulatesVolatilities": [
    "Project Delivery Workflow"
  ],
  ```
  `components[4]` (`project-design-manager`):
  ```json
  "encapsulates": "Encapsulates the Project Delivery Workflow volatility (B-04) for the Phase-2 activity: how options are drafted, estimated, presented at SDP review, and committed — all estimation math stays in Engines. A shared owner with SystemDesignManager and ConstructionManager until stage 4 replaces all three with DeliveryManager.",
  "encapsulatesVolatilities": [
    "Project Delivery Workflow"
  ],
  ```
  `components[5]` (`construction-manager`):
  ```json
  "encapsulates": "Encapsulates the Project Delivery Workflow volatility (B-13, B-14) for the construction activities: per-activity execution choreography — hand-off, branch/PR rail, agentic-job dispatch, review routing, variance intervention, pause/override/takeover. A shared owner with SystemDesignManager and ProjectDesignManager until stage 4 replaces all three with DeliveryManager.",
  "encapsulatesVolatilities": [
    "Project Delivery Workflow"
  ],
  ```
  `components[14]` (`design-health-engine`):
  ```json
  "encapsulates": "Encapsulates the Method Conformance Rules volatility (B-02, B-03): WHICH Method rules judge a draft (counts, layer rules, chain coverage, traceability joins, drift) and HOW findings are computed — the rule inventory evolves with the Method doctrine release over release while the delivery choreography that asks for the verdict stays put. Pure computation over the typed project state the calling Manager passes in (no I/O). Sole owner of its volatility since 2026-09-23: it was authored as an evaluation facet of the System Design Phase Workflow, which stopped being a coherent claim when the three phase workflows merged into one delivery workflow. Interim home of the platform methodcheck extension; contents upstream to the platform framework at the earmarked release.",
  "encapsulatesVolatilities": [
    "Method Conformance Rules"
  ],
  ```

- [ ] **Step 6: Update the two waiver justifications.** `slots["3"].model.waivers[0].justification` — replace the opening count clause:
  > `WAIVED (Refreshed 2026-09-23 (stage-1 model wave)): 18 volatilities — 12 same-customer-over-time, 6 all-customers-at-one-moment — still over the 6-15 target. Down from 19: the three phase-workflow volatilities (System Design / Project Design / Construction) merged into one Project Delivery Workflow, and DesignHealthEngine's rule inventory split out as the stand-alone Method Conformance Rules; both moves are recorded in the volatilities rejected ledger. Every remaining entry has exactly one named encapsulator (or a ratified facet group) and requirement traces. Re-evaluate if the count grows past 25.`

  `slots["5"].model.waivers[0].justification` — replace the final design-health-engine sentence:
  > `design-health-engine joined the Engine set 2026-08-01 (founder ruling R-F: its call carries the ci-check business verdict in uc1's chain — an evaluation Strategy, not ambient infrastructure); since 2026-09-23 it is the SOLE owner of the Method Conformance Rules volatility, so its 1:1 relationship no longer rests on a shared System Design Phase Workflow facet. Re-evaluate at the next scope-change replan.`
  - [ ] **Verify first:** count the axes after the merge before writing "12 same-customer-over-time, 6 all-customers-at-one-moment":
    ```bash
    cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage1
    jq -r '.slots["3"].model.items | group_by(.axis) | map("\(.[0].axis): \(length)") | .[]' .aiarch/state/project.json
    ```
    Write what it prints, not what this plan predicts.

- [ ] **Step 7: Prove no stale name survives.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage1
  grep -o "System Design Phase Workflow\|Project Design Phase Workflow\|Construction Phase Workflow" .aiarch/state/project.json | sort | uniq -c
  ```
  Expected: exactly **3** lines, one occurrence each, all of them inside the `slots["3"].model.rejected` entries written at Step 4 (a fold ledger must name what was folded). Confirm with `jq -r '.slots["3"].model.rejected[].name'`. Any other hit is a missed reference.

- [ ] **Step 8: Gates.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage1/server
  GOWORK=off make gen-models
  git -C .. status --short -- server/internal   # expected: EMPTY — no contract changed in this task
  GOWORK=off make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off make test-short
  GOWORK=off make derived-plan-check
  cd ../webApp && npm run check
  ```
  All green. `DH-VOL-ENCAP-MISSING` and `DH-COMP-VOL-DANGLING` must report zero findings (both SeverityError). `DH-VOL-ENCAP-AMBIGUOUS` cannot fire — the typed join is active (`typedEncapsulationActive()`), and under it multiple owners are legitimate, which is exactly what the three Managers sharing one volatility relies on. `DH-CARD-VOLATILITY` is SeverityInfo and merely re-states the new count. `derived-plan-check` is green because the estimation engine never reads volatilities.

- [ ] **Step 9: Commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage1
  git add .aiarch/state/project.json
  git commit -F - <<'MSG'
  model(volatility): three phase workflows are one Project Delivery Workflow

  System design, project design and construction were three volatilities
  describing three Managers rather than one business: an agent generates an
  artifact, agents and humans review it, and the activity advances through its
  own life cycle — identically for Requirements, Architecture and a Service
  component. R1 makes the first three activities of the same network, so the
  choreography varies on one axis. The union of B-02/B-03/B-04/B-13/B-14 rides
  onto the merged entry and all three Managers share it as an explicitly
  transitional facet group until stage 4 replaces them with DeliveryManager.

  DesignHealthEngine's rule inventory was never a facet of a phase workflow; it
  splits out as the stand-alone Method Conformance Rules volatility, which turns
  its shared-facet waiver into a real 1:1. 19 volatilities become 18.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 2: `delivery-manager`, the `deliveryManager` contract, and the retirement of the three — ONE commit

> **Step 0 — STOP. Founder ruling required before any edit in this task.**
>
> This task cannot land in stage 1. Reproduce the blocker yourself before asking — it takes two minutes and it is the whole argument:
> ```bash
> cd /Users/davidmarne/mixofrealitystudio/archistrator
> T=$(mktemp -d)/probe; mkdir -p "$T/.aiarch/state"
> jq '.slots["5"].model.components += [{"id":"delivery-manager","name":"DeliveryManager","kind":"manager","layer":"manager","encapsulates":"probe","encapsulatesVolatilities":["Project Delivery Workflow"],"atomicBusinessVerbs":[],"buildStatus":"planned","constructionProfile":"handwritten","provisioning":"owned","uiSurface":false}]' \
>   .aiarch/state/project.json > "$T/.aiarch/state/project.json"
> cd server && GOWORK=off go run ./cmd/aiarch-state-mcp validate --root "$T" --slot System 2>&1 | grep '^ERROR'
> ```
> Expected, verbatim: `ERROR [SYS-CARD-MGR] system has 6 Managers; The Method limits a system to 5 Managers without introducing subsystems (at system cardinality)`. Repeat with `.serviceContracts.deliveryManager` set and no component for the `DH-CONTRACT-FACET` Error. Both are `SeverityError`, both fail `make method-check` (`framework-go/methodcheck/check.go:70–83` — "It reports every Error-severity finding via `t.Errorf`").
>
> **The three options, and the recommendation.**
> 1. **RECOMMENDED — move this task and Task 3 to the head of stage 4**, where the three old Manager components are deleted in the same commit. Manager count goes 5 → 3 without ever passing through 6; `ALIGN-EXTRA-PKG` stays silent because the new `internal/manager/delivery` package lands in that same commit; `DV-SINGLE-MGR` is satisfiable because the unified view enters exactly one Manager; `DV-REL-COVERAGE` is satisfiable because the nine Manager-rooted relationships that lose their exerciser are deleted with their Managers. Cost: stage 1 ships the volatility merge and the derivation rule only; stage 5 (webApp) is unaffected — it reads `constructionManager.QueryActivityView` from stage 0, not the model.
> 2. Raise `maxManagers` in `framework-go/methodcheck/rules_system.go`. **Rejected:** it weakens a gate to fit a transitional model, it is a platform release (founder STOP in its own right), and the finding is TRUE while both sets of Managers exist.
> 3. Keep the three, add the sixth, and accept one standing Error. **Rejected:** `make method-check` is CI-enforced (`server-checks.yml`, "Method conformance gate"), so every subsequent commit in the repo would be red.
>
> Until the founder rules, **do not edit `project.json` for this task**. If the ruling is option 1, execute Steps 1–8 below as stage 4's first commit, together with the Go package that implements them.

**Files** (`.aiarch/state/project.json` unless noted):
- `slots["5"].model.components` — append `delivery-manager`; **delete** `components[3]` (`system-design-manager`), `[4]` (`project-design-manager`), `[5]` (`construction-manager`).
- `slots["5"].model.relationships` — add the 14 edges below; delete every edge whose `from` or `to` is one of the three deleted ids (today: indices 0,1,2,5,6,7,10,13,14,15,16,17,18,19,25,26,27,28,29,30,31,39,40,41,42,43,44,58,59,60,61,62,63,70,72,73,74 — recompute, do not trust this list).
- `serviceContracts.deliveryManager` — new; `serviceContracts.{systemDesignManager,projectDesignManager,constructionManager}` — deleted.
- Stage-4 companions (NOT this plan's scope, but they are in the same commit): `server/internal/manager/delivery/**`, `cmd/clientgen/main.go:62` `exposedManagers`, `cmd/appgen/main.go:155` `WebExposedManagers`, `server/internal/arch_test.go` allowlists, `registered_names_test.go` golden, the drain.

**Interfaces:**
- Consumes: the merged volatility name `"Project Delivery Workflow"` (Task 1); `projectstate` `$defs` for the reused shapes.
- Produces: component `delivery-manager` / `DeliveryManager`, contract key `deliveryManager`, 12 operations.

- [ ] **Step 1: The component.** Append to `slots["5"].model.components`, key order copied from `construction-manager`:
  ```json
  {
    "id": "delivery-manager",
    "name": "DeliveryManager",
    "kind": "manager",
    "layer": "manager",
    "encapsulates": "Encapsulates the Project Delivery Workflow volatility (B-02, B-03, B-04, B-13, B-14): how a project's activities are scheduled and how one activity's task DAG advances — dispatch → collect → review → gate → advance. A parent pump workflow per project starts a child activity-lifecycle workflow for every eligible activity (R3); the child is one generic DAG walker whose per-type differences are DATA (the method-assets lifecycle) and STRATEGY (command, worker class, artifact codec), never a branch on design-versus-construction. Replaces SystemDesignManager, ProjectDesignManager and ConstructionManager, which were three choreographies of one workflow.",
    "encapsulatesVolatilities": [
      "Project Delivery Workflow"
    ],
    "atomicBusinessVerbs": [],
    "constructionProfile": "handwritten",
    "provisioning": "owned",
    "uiSurface": false
  }
  ```
  - [ ] **Verify first:** `DeliveryManager` normalizes to `delivery` under `StereotypeSuffixNormalizer` (`align.go:72–87` strips one trailing `access|engine|manager|client`). `ownerKeyForPackage` (`align.go:415–426`) attributes a package to the **deepest path segment that normalizes to a component key in that layer**, so a component named `delivery` would hijack any `server/internal/manager/**` path containing a `delivery` segment. Confirm there is none other than the new package: `ls server/internal/manager/`.
  - No `buildStatus` — under option 1 the Go package lands in the same commit, and `buildStatus: "planned"` on a component that HAS a package is itself an Error (`ALIGN-STALE-PLANNED`, `align.go:348–359`).

- [ ] **Step 2: The relationships.** Fourteen edges, replacing the three deleted Managers' fan-out. Shape copied from the existing entries (`from`, `to`, `mode`, `label` — the keys are exactly those four):
  | from | to | mode | label |
  |---|---|---|---|
  | `web-client` | `delivery-manager` | sync | `startProject \| dispatchActivityTask \| submitReviewDecision \| setProjectRunState \| overrideActivity \| queryProjectView \| queryActivityView` |
  | `mcp-client` | `delivery-manager` | sync | same as web-client (R4 cross-surface equivalence) |
  | `scheduler-client` | `delivery-manager` | sync | `executeNextActivity \| replanProject` |
  | `delivery-manager` | `review-engine` | sync | `ProposeReviews(change, activityType, lifecyclePhase, componentId, policy, floorTouched, contracts) → ReviewSet` |
  | `delivery-manager` | `estimation-engine` | sync | `DerivePlan / computeNetwork / EstimateForOption / computeEarnedValue (the projectDesign activity's computed artifact, §6)` |
  | `delivery-manager` | `intervention-engine` | sync | `DecideOnVariance(variance) → {Retry \| Escalate \| Takeover}` |
  | `delivery-manager` | `design-health-engine` | sync | `EvaluateDesignHealth(project, systemModel) → findings` |
  | `delivery-manager` | `project-state-access` | sync | `readProject / stage·commit·reject·withdraw typed artifact model / plan + policy writes` |
  | `delivery-manager` | `agentic-job-access` | sync | `submitAgenticJob / observeAgenticJob / cancelAgenticJob` |
  | `delivery-manager` | `source-control-access` | sync | `getInstallationToken \| openBranch \| openPullRequest \| getPullRequestStatus \| postReview \| mergePullRequest` |
  | `delivery-manager` | `artifact-access` | sync | `storeConstructionOutput / retrieveConstructionOutput / retrieveOutputTree` |
  | `delivery-manager` | `episode-access` | sync | `appendEpisode / listEpisodes / readTraceEvents` |
  | `delivery-manager` | `message-bus` | sync | `registerSchedule(deliveryPump, replanSweep) / deliverSignal(replan trigger)` |
  | `delivery-manager` | `logging` / `diagnostics` | sync | `Logs` / `Reports health` (two rows) |
  - **`billing-manager → operations-manager` (queued) survives; `construction-manager → project-design-manager` (queued, index 72) is DELETED** — the replan trigger becomes an internal signal of one Manager, which is not an architecture edge (the `message-bus` doctrine note on `components[36]`). Its dynamic-view step in `replan-under-scope-change` must be re-authored in the same commit or `CC-STEP-NONEMPTY`/`DV-EDGE-IN-MODEL` fire.
  - **Every non-Utility row above must be exercised by some dynamic view** or `DV-REL-COVERAGE` fires at Error, one per row (`rules_dynamic.go:204–261`; Utility targets are exempt, which is why the `logging`/`diagnostics` rows need no step). Task 3 is where that is discharged — this is the mechanical reason the two tasks are one commit.
  - `delivery-manager` must have ≥1 outbound Engine/RA edge or `DH-GRAPH-MANAGER-EMPTY` (Warning) fires; it has eleven.

- [ ] **Step 3: The contract — `serviceContracts.deliveryManager`.** Top-level shape copied from `serviceContracts.constructionManager` (`component`, `layer`, `goPackage`, `deps`, `title`, `$defs`, `interface`):
  ```json
  {
    "component": "deliveryManager",
    "layer": "Manager",
    "goPackage": "internal/manager/delivery",
    "title": "DeliveryManager",
    "deps": [
      { "name": "client", "goType": "client.Client", "goImport": "go.temporal.io/sdk/client" },
      { "name": "projectState", "component": "projectStateAccess" },
      { "name": "artifact", "component": "artifactAccess" },
      { "name": "intervention", "component": "interventionEngine" },
      { "name": "review", "component": "reviewEngine" },
      { "name": "estimation", "component": "estimationEngine" },
      { "name": "designHealth", "component": "designHealthEngine" },
      { "name": "pipeline", "component": "agenticJobAccess" },
      { "name": "rail", "component": "sourceControlAccess" },
      { "name": "messageBus", "component": "messageBus" },
      { "name": "episodes", "component": "episodeAccess" }
    ],
    "$defs": { "…": "see Step 4" },
    "interface": { "name": "DeliveryManager", "layer": "Manager", "operations": [ "…see below" ] }
  }
  ```
  If the founder rules option 1, `goPackage` IS present (the package ships in the same commit) and `make gen-models` emits `internal/manager/delivery/contract.gen.go` + its fake. If instead the ruling is ever to land the model ahead of the code, **drop `goPackage`** — `cmd/modelgen/main.go:21–24` skips such an entry and nothing is generated — and set `buildStatus: "planned"` on the component; that pair is completely inert for alignment (`align.go:232–239` never reaches the `goPackage` join for a planned component).

- [ ] **Step 4: The twelve operations.** Exactly twelve — App-C's max is 12 (`DH-CONTRACT-OPCOUNT-MAX` Warning at >12, `DH-CONTRACT-OPCOUNT-REJECT` **Error** at ≥20). At 12 neither fires, so the merged contract is the first Manager contract in the repo that is CLEAN on op count where `systemDesignManager` (16) and `constructionManager` (13) both warn today.
  ```json
  [
    { "name": "StartProject",
      "params": [ { "name": "owner", "schema": { "$ref": "#/$defs/OwnerScope" } },
                  { "name": "name", "schema": { "type": "string" } },
                  { "name": "research", "schema": { "$ref": "#/$defs/ResearchInput" } },
                  { "name": "model", "schema": { "$ref": "#/$defs/OperatingModel" } } ],
      "result": { "$ref": "#/$defs/ProjectID" }, "error": true },
    { "name": "ExecuteNextActivity",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "tickID", "schema": { "type": "string" } } ],
      "result": { "$ref": "#/$defs/PumpResult" }, "error": true },
    { "name": "DispatchActivityTask",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } },
                  { "name": "taskID", "schema": { "type": "string" } },
                  { "name": "feedback", "schema": { "$ref": "#/$defs/ReviewFeedback" } } ],
      "result": { "$ref": "#/$defs/SessionRef" }, "error": true },
    { "name": "SubmitReviewDecision",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } },
                  { "name": "taskID", "schema": { "type": "string" } },
                  { "name": "decision", "schema": { "$ref": "#/$defs/ReviewDecision" } },
                  { "name": "feedback", "schema": { "$ref": "#/$defs/ReviewFeedback" } } ],
      "error": true },
    { "name": "AskQuestions",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } },
                  { "name": "taskID", "schema": { "type": "string" } },
                  { "name": "addressee", "schema": { "type": "string" } },
                  { "name": "questions", "schema": { "type": "array", "items": { "type": "string" } } } ],
      "error": true },
    { "name": "AcknowledgeStaleBasis",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } },
                  { "name": "note", "schema": { "type": "string" } } ],
      "error": true },
    { "name": "SetProjectRunState",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "runState", "schema": { "$ref": "#/$defs/ProjectRunState" } },
                  { "name": "reason", "schema": { "type": "string" } } ],
      "error": true },
    { "name": "OverrideActivity",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } },
                  { "name": "override", "schema": { "$ref": "#/$defs/ActivityOverride" } } ],
      "error": true },
    { "name": "ReplanProject",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "tickID", "schema": { "type": "string" } } ],
      "result": { "$ref": "#/$defs/ReplanSweepResult" }, "error": true },
    { "name": "SetProjectExecutionPolicy",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "policy", "schema": { "$ref": "#/$defs/ExecutionPolicyInput" } } ],
      "error": true },
    { "name": "QueryProjectView",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "view", "schema": { "$ref": "#/$defs/ProjectViewKind" } } ],
      "result": { "$ref": "#/$defs/ProjectView" }, "error": true },
    { "name": "QueryActivityView",
      "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                  { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } } ],
      "result": { "$ref": "#/$defs/ActivityView" }, "error": true }
  ]
  ```
  Naming: every one is an atomic verb over one noun. `SetProjectRunState`/`SetProjectExecutionPolicy` replace four setter pairs with one state-setter each rather than a `Pause`/`Resume`/`Set`/`Update` quartet, which is the same move `SubmitReviewDecision` makes over `SubmitPhaseDecision`/`SubmitSDPDecision`/`AdvancePhase`/`AdvanceToConstruction`.

- [ ] **Step 5: The `$defs`.** COPY VERBATIM from the source contracts — do not re-derive, or the generated Go stops being structurally identical and every wire consumer moves. Provenance table (source contract → def; take the def and everything it transitively `$ref`s):
  | From | Defs to copy |
  |---|---|
  | `constructionManager` | `ProjectID`, `ActivityID`, **`ActivityView`** and its whole closure (`ActivityLifecyclePhase`, `ActivityTaskKind`, `ActivityTaskState`, `ActivityTaskView`, `ActivityViewState`, `TaskRevisionView`, `TaskRevisionComment`, `TaskRevisionOutcome`, `TaskRevisionProvenance`, `AnchoredComment`), `ActivityOverride`, `OverrideKind`, `PumpResult`, `PumpStatus`, `ReplanSweepResult`, `ReviewFeedback`, `ReviewSet`, `Reviewer`, `EpisodeKind`, `EpisodeLineage`, `EpisodeOutcome`, `EpisodeRecordView`, `EpisodeTimeline`, `EpisodeUsage`, `SubagentSpan`, `TimelineEvent` |
  | `systemDesignManager` | `OwnerScope`, `ResearchInput`, `ResearchSource`, `OperatingModel`, `ReviewDecision`, `ProjectState` (→ `ProjectView`'s body), `ProjectSummary`, `DesignHealth`, `Finding`, `RuleID`, `Severity`, `Location`, `ArtifactSlotView`, `ArtifactStage`, `ReviewCommentView`, `ReviewCommentReply`, `Version` |
  | `projectDesignManager` | `SessionRef`, `SessionStage`, `SessionStateView`, `ActiveRole`, `ActiveStep`, `OptionID` |
  | NEW (spelled out below) | `ProjectRunState`, `ProjectViewKind`, `ProjectView`, `ExecutionPolicyInput` |
  `ActivityView` is taken **as-is** from `constructionManager` — stage 0 designed it as the Activity Experience's single read and the webApp's fixtures are already validated against it; changing it here would break stage 5 for no reason.

  The four new defs, in the repo's string-enum idiom (`x-enum-varnames` + `x-go-base`, copied from `ArtifactStage`):
  ```json
  "ProjectRunState": {
    "type": "string",
    "enum": ["running", "paused"],
    "x-enum-varnames": ["ProjectRunning", "ProjectPaused"],
    "x-go-base": "string"
  },
  "ProjectViewKind": {
    "type": "string",
    "enum": ["summary", "plan", "pump", "designHealth", "episodes"],
    "x-enum-varnames": ["ProjectViewSummary", "ProjectViewPlan", "ProjectViewPump", "ProjectViewDesignHealth", "ProjectViewEpisodes"],
    "x-go-base": "string"
  },
  "ProjectView": {
    "type": "object",
    "properties": {
      "kind": { "$ref": "#/$defs/ProjectViewKind" },
      "summary": { "$ref": "#/$defs/ProjectState" },
      "projects": { "type": "array", "items": { "$ref": "#/$defs/ProjectSummary" } },
      "pump": { "$ref": "#/$defs/PumpStatus" },
      "designHealth": { "$ref": "#/$defs/DesignHealth" },
      "episodes": { "type": "array", "items": { "$ref": "#/$defs/EpisodeRecordView" } },
      "timeline": { "$ref": "#/$defs/EpisodeTimeline" }
    },
    "required": ["kind"]
  },
  "ExecutionPolicyInput": {
    "type": "object",
    "properties": {
      "preset": { "type": "string" },
      "policy": { "$ref": "#/$defs/ReviewPolicyInput" }
    }
  }
  ```
  - [ ] **Verify first:** `required` in this schema dialect is **PRESENCE-only** — non-emptiness lives in the GO implementation, never in `minLength` (2026-08-13 contract-strictness ruling, `ValidateModelIdentities` + the paramguard arch gate). Do not add `minLength` anywhere.

- [ ] **Step 6: Delete the three contracts and the three components,** and every relationship naming them. Recompute the relationship index list mechanically, never by eye:
  ```bash
  jq -r '.slots["5"].model.relationships | to_entries[] | select(.value.from|IN("system-design-manager","project-design-manager","construction-manager")) // select(.value.to|IN("system-design-manager","project-design-manager","construction-manager")) | .key' .aiarch/state/project.json
  ```

- [ ] **Step 7: Expected design-health movement.** After this commit:
  - **Silenced:** `DH-CONTRACT-OPCOUNT-MAX` no longer has `systemDesignManager` (16) or `constructionManager` (13) to fire on — **but `projectStateAccess` still has 14 ops**, so the rule id keeps firing at Warning and `assertPresent(t, got, RuleContractOpMax, methodcheck.SeverityWarning)` (`engine_test.go`) stays green with only its **comment** needing a rewrite. Verify with `jq '.serviceContracts | map_values(.interface.operations|length) | to_entries | map(select(.value > 12))'`.
  - **Newly firing:** `DH-CARD-MANAGERS-MIN` is Info at exactly 1 Manager — not reached (3). `SYS-CARD-RATIO` (Warning) goes from "7 Engines vs 5 Managers" to "7 vs 3" — still fires, same id, same severity, no test edit.
  - **Silenced Error:** none was firing; keep it that way.
  - `TestGreenFixtureNoErrors` (`engine_test.go:26–43`) is the backstop: it fails on ANY Error-severity finding.

- [ ] **Step 8: Gates and commit** — run the full sweep from Task 4 Step 6 (this commit changes generated code, unlike Tasks 1 and 4), then commit together with Task 3 and the stage-4 Go package. There is no separate commit for this task.

---

### Task 3: Five core use cases become three — every realization in the SAME commit as Task 2

R4. `.slots["4"].model.decisions` holds **18** use cases today (5 core, 13 `nonCore` variations) and `.slots["5"].model.dynamicViews` holds **18** views, one per use case — core and variation alike. That one-to-one is not a convention: `USECASE-DYNAMIC-MISSING` (`framework-go/methodcheck/rules_system.go:379–410`, **Error**) is the founder extension that "every use case in the committed CoreUseCases set — core AND nonCore variations alike — must carry its own dynamic view", and coverage is keyed on `dv.useCaseId == uc.id` exactly, with `variationOf` never consulted. `UC-ACT-PRESENT` (`rules_statevalidation.go:216–244`, **Error**) is equally unconditional: every decision needs a non-null activity diagram with an entry and ≥1 `action` node.

**The move:** `drive-system-design` and `execute-a-construction-activity` are **deleted** (their diagrams' content becomes steps of the new core — spec §4 "absorbs"); `commit-to-a-project-option` is **demoted** to a variation (spec §4, verbatim); `execute-a-project-activity` is **added** as core. Decisions go 18 → **17**; views go 18 → **17**. Core goes 5 → 3, inside `DH-CARD-COREUC`'s 2–6 band.

**Re-parenting (`variationOf`), the ten orphans + the demotion:**
| use case | `variationOf` before | after |
|---|---|---|
| `manage-projects`, `add-a-use-case-to-an-in-flight-project`, `ask-a-clarifying-question-during-review`, `send-back-change-requests-for-a-redraft` | `drive-system-design` | `execute-a-project-activity` |
| `replan-under-scope-change`, `track-weekly-project-progress`, `view-the-project-state-log`, `download-generated-source-code`, `resume-paused-construction`, `requeue-a-failed-construction-activity` | `execute-a-construction-activity` | `execute-a-project-activity` |
| `commit-to-a-project-option` | *(core)* | `execute-a-project-activity` — and `classification` flips `core` → `nonCore`, and it needs a non-empty `rejectionReason` |
| `retry-a-declined-service-invoice`, `onboard-a-new-customer` | `bill-the-user-for-usage` | unchanged |
| `view-operating-cost-projection` | `operate-a-delivered-system` | unchanged |
`UC-VARIATION-REF` (`rules_statevalidation.go:300–360`, **Error**) enforces all three halves: a `core` UC must NOT carry `variationOf`; a `nonCore` UC MUST carry one that resolves to an existing **core** id, AND a non-empty `rejectionReason`. `commit-to-a-project-option`'s `rejectionReason` is the R7 argument:
> `Absorbed into execute-a-project-activity 2026-09-23 (R4/R7). Committing to a project option is not a use case of its own: it is the projectDesign activity's single gate task — the user approves an engine-computed plan and cost at M0. Nothing about it differs from any other review gate except that no send-back is offered, which is a policy of the gate, not a different chain.`

**Files:** `.aiarch/state/project.json` — `slots["4"].model.decisions` (delete indices 0 and 2, rewrite index 1, append the new core, re-point 10 `variationOf` values); `slots["5"].model.dynamicViews` (delete the `drive-system-design` and `execute-a-construction-activity` views, re-key `commit-to-a-project-option`'s, add `execute-a-project-activity`'s). Plus `server/internal/engine/designhealth/engine_test.go` (the `realizedViews` table) and `uitests/testdata/coreUseCasesProject.json` (Task 5).

- [ ] **Step 1: Inventory what the three deleted views were the sole exerciser of.** This is the list the new view MUST cover or `DV-REL-COVERAGE` (Error, one per row) fires:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator
  jq -r '
    def utils: ["security","logging","diagnostics","message-bus"];
    . as $r |
    ($r.slots["5"].model.dynamicViews | map({uc:.useCaseId, calls:[.steps[].calls[] | "\(.from)|\(.to)|\(.mode)"]})) as $views |
    $r.slots["5"].model.relationships
    | map(select(.to as $t | (utils | index($t)) | not))
    | map(. as $rel | ("\($rel.from)|\($rel.to)|\($rel.mode)") as $k |
        {k:$k, by: [$views[] | select(.calls | index($k)) | .uc]})
    | map(select((.by|length) > 0 and ((.by - ["drive-system-design","commit-to-a-project-option","execute-a-construction-activity"])|length) == 0))
    | .[] | .k' .aiarch/state/project.json
  ```
  Expected (12 rows, verified 2026-09-23): `scheduler-client→construction-manager`, `construction-manager→{intervention-engine, review-engine, artifact-access, source-control-access, episode-access}`, `system-design-manager→{review-engine, episode-access}`, `project-design-manager→review-engine`, `episode-access→project-git-repo`, `artifact-access→project-git-repo`, `agentic-job-access→construction-pipeline-runtime`. Under Task 2 the nine Manager-rooted rows are deleted with their Managers and replaced by the `delivery-manager` rows; the three RA→Resource rows must still be exercised by the new view. **Re-run this query after the edit with the new view's id excluded — it must print nothing.**

- [ ] **Step 2: Author the new core decision.** Append to `slots["4"].model.decisions`, key order `useCase` / `rejectionReason` / `essenceRationale`, and inside `useCase` the order `id` / `name` / `actors` / `trigger` / `classification` / `variationOf` / `activity`.
  ```json
  {
    "useCase": {
      "id": "execute-a-project-activity",
      "name": "Execute a Project Activity",
      "actors": [ { "id": "architect-user", "role": "Architect User" }, { "id": "operator", "role": "Operator" } ],
      "trigger": "timer",
      "classification": "core",
      "variationOf": null,
      "activity": { "nodes": [ … ], "edges": [ … ] }
    },
    "rejectionReason": "",
    "essenceRationale": "This is the whole delivery promise in one chain: an activity of the project network becomes eligible, an agent produces its artifact, agents and humans review it, and the activity advances through its own life cycle (Righting Software App. A). It reads identically for Requirements, for Architecture and for a Service component — which is exactly why the platform used to carry three of it. Requirements, Architecture and Project Design are activities 1–3 of the same network (Table 11-1), fenced from construction by M0; nothing about the chain changes across that fence except the data (the lifecycle) and the strategy (command, worker class, artifact codec). Venues, agents and code generation will all churn; orchestrated activity execution against a committed plan is what the business does."
  }
  ```
  **The activity diagram** (spec §4: "two entries — timer/pump and clientAction — as step-local `alt` groups in one dynamic view"). `trigger` is `"timer"`, which `CC-TRIGGER-EVENT` (`designhealthengine.go:1274–1301`, **Error**) reads as "the diagram must carry a `timeEvent` entry node with zero incoming edges". A `clientAction` trigger would be the wrong choice here: that arm of the rule demands the diagram carry **neither** a `timeEvent` nor an `acceptEvent` entry, which the pump entry violates. A `start` node for the human-initiated arm is legal alongside the `timeEvent` — it is neither of the two forbidden kinds, and `drive-system-design` already proves a `start`-rooted chain walks (`CC-PATH-CONNECTED`'s `isActorToClient` arm, `designhealthengine.go:1347–1352`).

  Nodes (18) — merge the three absorbed diagrams, keeping every absorbed node id that is still true so the prose is traceable:
  | id | kind | label |
  |---|---|---|
  | `pump-fires` | `timeEvent` | Delivery pump comes due |
  | `start` | `start` | *(empty — the human-initiated arm)* |
  | `activity-eligible` | `action` | Activity eligible: predecessors done and the M0 fence honoured; open its lifecycle |
  | `next-task` | `action` | Take the next ready task of the activity's lifecycle DAG |
  | `task-kind` | `decision` | Dispatch task or review task? |
  | `dispatch-task` | `action` | Dispatch the task's agentic job; stage its artifact on the activity branch |
  | `observe-validate` | `action` | Read the result back and validate it against the task's exit criterion |
  | `exit-met` | `decision` | Exit criterion met? |
  | `retry-remain` | `decision` | Retries remain? *(`decidedBy: intervention-engine`)* |
  | `escalate-operator` | `action` | Escalate to the operator; the steer is kept on the activity and delivered to its next attempt *(`roleName: Operator`, `linkedActorId: operator`)* |
  | `operator-intervened` | `decision` | Operator intervened? *(`decidedBy: operator`)* |
  | `propose-reviews` | `action` | Determine the reviewer set and whether a human must decide |
  | `requires-human` | `decision` | Does the policy require a human decision? *(`decidedBy: review-engine`)* |
  | `dispatch-reviewers` | `action` | Reviewers review the staged artifact |
  | `human-decision` | `action` | The architect user approves or sends back, with comments *(`roleName: Architect User`, `linkedActorId: architect-user`)* |
  | `verdict` | `decision` | Approve or send back? |
  | `commit-artifact` | `action` | Commit the staged artifact; the gate passes; the LifecyclePhase is earned |
  | `merge-advance` | `merge` | *(empty)* |
  | `record-exit` | `action` | Record binary exit; recompute earned value and projection |
  | `end` | `end` | *(empty)* |
  Edges: `pump-fires→activity-eligible`; `start→activity-eligible`; `activity-eligible→next-task`; `next-task→task-kind`; `task-kind→dispatch-task [dispatch]`; `task-kind→propose-reviews [review]`; `dispatch-task→observe-validate`; `observe-validate→exit-met`; `exit-met→merge-advance [yes]`; `exit-met→retry-remain [no]`; `retry-remain→dispatch-task [yes: retry]`; `retry-remain→escalate-operator [no: exhausted]`; `escalate-operator→operator-intervened`; `operator-intervened→escalate-operator [no]`; `operator-intervened→merge-advance [yes]`; `propose-reviews→requires-human`; `requires-human→dispatch-reviewers [agents only]`; `requires-human→human-decision [human required]`; `dispatch-reviewers→verdict`; `human-decision→verdict`; `verdict→commit-artifact [approve]`; `verdict→next-task [sendBack: reopens the judged task as revision n+1]`; `commit-artifact→merge-advance`; `merge-advance→record-exit`; `record-exit→end`.
  - Every `guardedFlow` edge carries non-empty guard text or `UC-GUARD-LABEL` (**Error**) fires. The bracketed text above IS the `guard` value; edges with no bracket are `controlFlow` with `"guard": ""`.
  - [ ] **Verify first:** `activityHasEntryAndAction` (`rules_statevalidation.go:246–270`) needs ≥1 `start` OR a zero-incoming `timeEvent`/`acceptEvent`, AND ≥1 `action`. This diagram has both entry forms and eleven actions.

- [ ] **Step 3: Author the dynamic view.** Key `uc-execute-project-activity`, `useCaseId` `execute-a-project-activity`, title `Execute a Project Activity`. `CC-COVERAGE` (**Error**, bidirectional) requires a step for EVERY `action`/`timeEvent`/`acceptEvent` node and FORBIDS a step on any node that is not one of those or a `decision`/`switch` (`ccMustHaveStep` / `ccMayHaveStep`, `designhealthengine.go:842–860`). So: a step for each of `pump-fires` and the eleven actions (12 required), optional steps on `retry-remain`, `operator-intervened`, `requires-human` (which the absorbed views already carry and which are worth keeping — they are where the Engines are called), and **no step on `start`, `task-kind`, `exit-met`, `verdict`, `merge-advance`, `end`**.
  The two entries as step-local `alt` groups — this is the `alt` idiom already used 100 times in the committed views (e.g. the `escalate-operator` step of `uc3-execute-construction-activity`, calls tagged `"alt": "s1"` / `"s2"`):
  ```json
  { "activityNodeId": "pump-fires",
    "calls": [ { "from": "scheduler-client", "to": "delivery-manager", "mode": "sync", "label": "executeNextActivity(projectId, tickId) — the pump starts a child for every eligible activity" } ] },
  { "activityNodeId": "activity-eligible",
    "calls": [
      { "from": "architect-user", "to": "web-client", "mode": "sync", "label": "opens the activity from the plan", "alt": "s1" },
      { "from": "architect-user", "to": "mcp-client", "mode": "sync", "label": "opens the activity over the MCP surface (R4 cross-surface equivalence)", "alt": "s1" },
      { "from": "web-client", "to": "delivery-manager", "mode": "sync", "label": "queryActivityView(projectId, activityId)", "alt": "s2" },
      { "from": "mcp-client", "to": "delivery-manager", "mode": "sync", "label": "queryActivityView(projectId, activityId) — same Manager entry", "alt": "s2" },
      { "from": "delivery-manager", "to": "project-state-access", "mode": "sync", "label": "readProject → the next ready tasks of the activity's lifecycle DAG" } ] }
  ```
  Then one step per remaining action, routing exactly the Task 2 relationships: `dispatch-task` → `source-control-access` (token, branch) + `agentic-job-access` (submit) + `agentic-job-access→construction-pipeline-runtime`; `observe-validate` → `agentic-job-access` (observe) + `artifact-access` (store) + `artifact-access→project-git-repo` + `episode-access` (append) + `episode-access→project-git-repo`; `retry-remain` → `intervention-engine`; `escalate-operator` → `project-state-access` + the operator `alt` pair into both clients + back into `delivery-manager` (`overrideActivity`); `propose-reviews` → `source-control-access` (openPullRequest) + `review-engine`; `requires-human` → `review-engine`; `dispatch-reviewers` → `agentic-job-access`; `human-decision` → the `architect-user` `alt` pair + `submitReviewDecision` into `delivery-manager`; `commit-artifact` → `project-state-access` (commit) + `source-control-access` (postReview, mergePullRequest) + `design-health-engine` (EvaluateDesignHealth on a design artifact's gate); `next-task` → `project-state-access`; `record-exit` → `project-state-access` + `estimation-engine` (computeEarnedValue).
  Rules this must satisfy, each an Error: `DV-EDGE-IN-MODEL` — every component↔component call matches a declared `(from,to,mode)`; `DV-EDGE-ENDS` — every end is a component id or an actor **of this use case**; `DV-SINGLE-MGR` — Client edges enter exactly ONE Manager (`delivery-manager`); `CC-ACTOR-EDGE` — an actor's counterpart must be a `client`-kind component and the mode `sync`; `CC-ACTOR-LANE` — `escalate-operator` and `human-decision` carry `linkedActorId`, so their steps must name that actor; `CC-PATH-CONNECTED` — the `timeEvent` path's first call must be client→manager (`scheduler-client→delivery-manager` ✓) and the `start` path's first call must be actor→client (`architect-user→web-client` ✓), every later call rooted at an already-reached `from`.
  - [ ] **Verify first:** `CC-PATH-CONNECTED` enumerates every entry→end path (`activityPaths`, cap `maxActivityPaths = 512`). This diagram's `retry-remain→dispatch-task` and `verdict→next-task` back-edges make the graph cyclic; confirm the walker's cycle handling before authoring (`designhealthengine.go:1502–1810`) and, if the path count explodes past the cap, simplify by moving the send-back back-edge to `merge-advance`. Run the gate and read the finding rather than guessing.

- [ ] **Step 4: Delete the two absorbed decisions and their views; re-key the demoted one.** `slots["4"].model.decisions` — remove the `drive-system-design` and `execute-a-construction-activity` entries; `slots["5"].model.dynamicViews` — remove `uc1-drive-system-design` and `uc3-execute-construction-activity` (confirm the exact `key` values with `jq -r '.slots["5"].model.dynamicViews[].key'`). `commit-to-a-project-option`'s view is KEPT as-is except that its Manager endpoint becomes `delivery-manager` and its op labels become the new contract's (`dispatchActivityTask`, `submitReviewDecision`).

- [ ] **Step 5: Update the design-health pin** — `server/internal/engine/designhealth/engine_test.go`, `TestGreenFixtureAdvisoriesFire`. `indexBySeverity` is a SET, so a newly-firing Warning needs no edit; the only counted things in the whole test are `CC-COVERAGE` (must stay 0) and `CC-TRIGGER-EVENT` (must stay 0). What DOES need editing is the `realizedViews` table (the file's own comment says so: "A table, not sixteen copies of one `if`: adding a use case is a row"): delete the `{"drive-system-design", "PoC"}` and `{"execute-a-construction-activity", "batch-1"}` rows, add `{"execute-a-project-activity", "stage-1"}`, and correct the two `t.Errorf` strings and the comment block that hard-code "18 dynamic views" / "18 committed use cases" / the 118-eligible-node arithmetic. Also rewrite the `assertPresent(t, got, RuleContractOpMax, …)` comment: after Task 2 the rule fires for `projectStateAccess` (14 ops), not for the two deleted Manager contracts.

- [ ] **Step 6: Gates + commit.** Task 2 Step 8's sweep, then ONE commit carrying Task 2 + Task 3 + the stage-4 Go package. Message body: the three-into-one use case, the eleven re-parentings, the one demotion, and the count 18 → 17.

---

### Task 4: A `planned` component derives no construction activity (the founder's ruling, pinned)

**The defect this closes.** `estimation.SystemComponent` (`server/internal/engine/estimation/contract.gen.go:167–174`) carries only `ID`, `Name`, `Kind`, `ConstructionProfile`, `Provisioning`, `UiSurface` — **`buildStatus` cannot reach the derivation at all** (`grep -rn "buildStatus\|BuildStatus" server/internal/engine/estimation/` returns nothing). So a `handwritten` Manager marked `planned` yields `C-<id>` (25 days), lands in slot 9 via `make derived-plan-write`, and the pump sweep dispatches it the moment its dependencies are Done — `nextEligibleActivity` (`constructionmanager.go:1449–1476`) has a project-phase gate, an eligibility rule, a dependency check, a component-resolution check and a classification check, and **no "should we build this" check**. The 30-second production sweep would hand `deliveryManager` to a build agent.

Today's only `planned` component, `scheduler-client`, escapes by luck: it is skipped because it is `generated` and has no UI surface (`codingActivityFor` `:1577`, `clientAppActivityFor` `:1628`), not because it is planned. The rule has never been stated.

**Why plumb `buildStatus` rather than dodge it.** The alternatives are lies: authoring `delivery-manager` as `constructionProfile: "generated"` says a generator builds it, `"provided"` says the platform ships it. The honest statement is the one the founder ruled — *a planned component has no code to build yet* — and it needs the field. Blast radius is four files and no contract consumer outside the estimation engine.

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.estimationEngine["$defs"].SystemComponent.properties`, one additive property.
- Regenerate (never hand-edit): `server/internal/engine/estimation/contract.gen.go`, `server/internal/engine/estimation/fake/fake.gen.go`.
- Modify: `server/internal/manager/projectdesign/projectdesignmanager.go` — `toEstimationSystemView` (`:3123–3142`), one field + its `derefString` default.
- Modify: `server/internal/engine/estimation/estimationengine.go` — `codingActivityFor` (`:1576`), `provisioningActivityFor` (`:1598`), `clientAppActivityFor` (`:1627`).
- Modify: `server/internal/engine/estimation/engine_test.go` — two new tests + the `sampleSystem()` fixture.
- Modify: `server/internal/engine/estimation/testdata/system_view.json` — add `buildStatus` to the four components that carry one.
- Modify: `server/internal/manager/projectdesign/manager_test.go` — `TestFixtureSystemViewMatchesLiveCommittedSystem` (`:5895`) gains the `buildStatus` comparison.

**Interfaces:**
- Consumes: `projectstate.Component.BuildStatus *string` (`projectstate/contract.gen.go:224` — declared since 2026-07 and **read by no Go code in the repo**; this task gives it its first reader).
- Produces: `estimation.SystemComponent.BuildStatus string`; the skip in all three emitters.

- [ ] **Step 1: Write the failing test.** Append to `server/internal/engine/estimation/engine_test.go`, after `TestDeriveActivitiesClientAppFollowsUISurfaceNotConstructionProfile` (`:899`), in that function's idiom (append to `sampleSystem()`, never edit it):
  ```go
  // A planned component is DESIGNED but not yet implemented: the model declares it so the
  // architecture can be reasoned about and the alignment gate can exempt it, and there is
  // by definition no code to build. Deriving a C-* for it would put a dispatchable activity
  // in slot 9, and the pump has no second opinion — nextEligibleActivity dispatches anything
  // whose dependencies are Done. Every emitter must skip it, whatever its layer.
  func TestDeriveActivitiesEmitsNothingForAPlannedComponent(t *testing.T) {
  	sys := sampleSystem()
  	sys.Components = append(sys.Components,
  		SystemComponent{ID: "delivery-manager", Name: "DeliveryManager", Kind: "manager", ConstructionProfile: "handwritten", BuildStatus: "planned"},
  		SystemComponent{ID: "ledger-db", Name: "LedgerDB", Kind: "resource", Provisioning: "vendor", BuildStatus: "planned"},
  		SystemComponent{ID: "desk-client", Name: "DeskClient", Kind: "client", ConstructionProfile: "handwritten", UiSurface: true, BuildStatus: "planned"},
  	)
  	for _, a := range deriveActivities(sys) {
  		switch a.ComponentID {
  		case "delivery-manager", "ledger-db", "desk-client":
  			t.Errorf("emitted %s for %s, which is buildStatus=planned — it has no code to build, and the pump would dispatch it", a.Name, a.ComponentID)
  		}
  	}
  }

  // The skip is keyed on "planned" ALONE. "external" marks a component the platform does
  // not build either, but it is already covered by constructionProfile "provided" and by
  // kind, and widening the skip would silently drop work the moment a marker is authored
  // loosely (message-bus carries no buildStatus though three sibling utilities do).
  func TestDeriveActivitiesStillBuildsAComponentWithNoBuildStatus(t *testing.T) {
  	sys := sampleSystem()
  	got := names(deriveActivities(sys))
  	for _, want := range []string{"C-order-manager", "C-pricing-engine", "C-order-access"} {
  		if _, ok := got[want]; !ok {
  			t.Errorf("%s disappeared — the planned skip must not catch an unmarked component", want)
  		}
  	}
  }
  ```

- [ ] **Step 2: Run it; confirm it fails for the right reason.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage1/server
  GOWORK=off go test ./internal/engine/estimation/ -run 'TestDeriveActivitiesEmitsNothingForAPlannedComponent' -count=1 2>&1 | head
  ```
  Expected: `unknown field BuildStatus in struct literal of type SystemComponent` and `FAIL … [build failed]`. The compiler refusing the field IS the defect: the derivation cannot see buildStatus.

- [ ] **Step 3: Add the field to the contract.** `.aiarch/state/project.json`, `.serviceContracts.estimationEngine["$defs"].SystemComponent.properties`, appended after `uiSurface` and NOT added to `required` (an unauthored buildStatus is the common case):
  ```json
  "buildStatus": {
    "type": "string",
    "description": "The component's build state: empty (the default — built or to be built), \"planned\" (designed, no code yet — derives no activity), or \"external\" (supplied outside the project). Read by the plan derivation: a planned component yields no construction activity, because the pump dispatches whatever slot 9 holds."
  }
  ```
  Then:
  ```bash
  GOWORK=off make gen-models
  git status --short -- internal/engine/estimation
  ```
  Expected modified: `internal/engine/estimation/contract.gen.go` (`BuildStatus string \`json:"buildStatus"\``) and `internal/engine/estimation/fake/fake.gen.go`. Nothing else — `estimationEngine` is not an exposed Manager, so no OAS, no `schema.ts`, no client layer.
  - [ ] **Verify first:** open `contract.gen.go` and use the name modelgen actually emitted (`BuildStatus string` vs `*string`) in Steps 4–5.

- [ ] **Step 4: Plumb it.** `server/internal/manager/projectdesign/projectdesignmanager.go`, `toEstimationSystemView` (`:3123`) — add to the struct literal, matching the neighbours' `derefString` idiom:
  ```go
  			BuildStatus:         derefString(c.BuildStatus, ""),
  ```
  and extend the doc comment above it (`:3114–3122`) — today it says "identity, kind, and the three typed doctrine attributes"; it is now four, and the fourth is the one that decides whether there is anything to build at all.

- [ ] **Step 5: The skip.** `server/internal/engine/estimation/estimationengine.go` — one guard, stated once, beside the layer rule it joins. Add above `codingActivityFor` (`:1559`'s doc block):
  ```go
  // buildStatusPlanned marks a component the design declares but no code implements yet.
  // The alignment gate exempts it from ALIGN-MISSING-PKG on exactly that ground
  // (framework-go/methodcheck/align.go), and the plan must agree with the gate: a
  // component with no code has no construction work, so it derives no activity. This is
  // not an optimization — slot 9 is the pump's dispatch queue, and nextEligibleActivity
  // has no second opinion about whether an activity SHOULD be built, only about whether
  // its dependencies are done. Emitting C-<planned component> hands it to a build agent.
  const buildStatusPlanned = "planned"
  ```
  and the first line of each of the three emitters:
  ```go
  	if c.BuildStatus == buildStatusPlanned {
  		return DerivedActivity{}, false
  	}
  ```
  Three copies rather than one wrapper: `deriveActivities` (`:1700–1706`) calls the three as peers, each already stating its own whole precondition, and a fourth call site would be the place a future emitter forgets to ask.

- [ ] **Step 6: Run the tests, then the fixtures.**
  ```bash
  GOWORK=off go test ./internal/engine/estimation/ -count=1
  ```
  `TestParityDerivesExactlyTheTable11_1ActivitySet` (`engine_test.go:1810`, exact set of 32 names) and `TestParityDropsGeneratedClientCodingActivities` (`:1748`) must stay green **unedited** — `scheduler-client` was already skipped, now for two reasons instead of one. If the set moved, the skip is too wide: fix the guard, not the expectation.
  Then the live-fixture join, `server/internal/engine/estimation/testdata/system_view.json`: add `"buildStatus": "planned"` to `scheduler-client` and `"buildStatus": "external"` to `security`, `logging`, `diagnostics` (and nothing to `message-bus`, which carries none in slot 5). Extend `TestFixtureSystemViewMatchesLiveCommittedSystem` (`server/internal/manager/projectdesign/manager_test.go:5895`) to compare `BuildStatus` alongside `Kind`/`ConstructionProfile`/`Provisioning`/`UiSurface` — otherwise the fixture can silently drift from slot 5 on the one field that now decides whether work exists.

- [ ] **Step 7: The derived plan must not move.**
  ```bash
  GOWORK=off make derived-plan-write
  git status --short -- ../.aiarch/state/project.json
  GOWORK=off make derived-plan-check
  ```
  `derived-plan-write` must be a **NO-OP** — slots 9 and 10 unchanged, no `staleBasis` flag raised on slots 11–16 — because the only `planned` component in the model (`scheduler-client`) already derived nothing. A diff here means the skip caught something it should not have: read it, do not commit it. (`derived-plan-write` reads and rewrites the working-tree file at `../../../../.aiarch/state/project.json` through the projectstate codec with member splicing, so it works inside the worktree.)

- [ ] **Step 8: Gates.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage1/server
  GOWORK=off go build ./...
  GOWORK=off make fix-check
  GOWORK=off make lint
  GOWORK=off make test-short
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-lifecycles-check
  GOWORK=off make sumtype-check method-check encapsulation-check derived-plan-check
  GOWORK=off go test ./internal/ -run 'TestMethodLayering|TestFileLayout|TestGeneratedOnlyPublic|TestNoBannedPhaseIdentifier' -count=1
  cd ../webApp && npm run check
  ```
  All green. `encapsulation-check` needs no new allowlist entry — `buildStatusPlanned` is unexported and `BuildStatus` is generated contract surface.

- [ ] **Step 9: Commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage1
  git add .aiarch/state/project.json \
          server/internal/engine/estimation/estimationengine.go \
          server/internal/engine/estimation/contract.gen.go \
          server/internal/engine/estimation/fake/fake.gen.go \
          server/internal/engine/estimation/engine_test.go \
          server/internal/engine/estimation/testdata/system_view.json \
          server/internal/manager/projectdesign/projectdesignmanager.go \
          server/internal/manager/projectdesign/manager_test.go
  git commit -F - <<'MSG'
  fix(estimation): a planned component derives no construction activity

  buildStatus could not reach the derivation at all — estimation.SystemComponent
  carried identity, kind and three doctrine attributes, and Component.BuildStatus
  had no reader anywhere in the repo. So a handwritten component marked planned
  yielded C-<id>, landed in slot 9, and the 30-second pump sweep would hand it to
  a build agent: nextEligibleActivity decides whether an activity's dependencies
  are done, never whether it should be built at all.

  The alignment gate already exempts a planned component from ALIGN-MISSING-PKG
  on the ground that no code implements it; the plan now agrees with the gate.
  scheduler-client, the model's only planned component, was skipped by luck (it
  is generated and has no UI surface) — the derived plan is unchanged, which is
  what makes this a rule rather than a behaviour change.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 5: Amend the spec, and record what stage 1 could not carry

The spec's §8 stage-1 row and its "Why not model-first" note are now known to be wrong on the facts (they cite `ALIGN-*` and `CC-*` but not `SYS-CARD-MGR`, `DV-SINGLE-MGR` or `DH-CONTRACT-FACET`). The spec is the executable input to stages 2–6, so leaving it stale would make the next planner re-derive the same blocker.

**Files:**
- Modify: `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` — §8's "Why not model-first" paragraph and the stage-1 / stage-4 rows of the stage table.
- Create: `docs/bugs/2026-09-23-stage1-model-wave-earmarks.md` (the directory already holds `docs/bugs/2026-08-09-construction-open-items.md`).

- [ ] **Step 1: Rewrite §8's "Why not model-first" paragraph** to the four-gate finding, quoting the reproduced messages, and naming the probe recipe (`jq` into a temp root + `go run ./cmd/aiarch-state-mcp validate --root <tmp> --slot System`) so it can be re-checked rather than re-argued.

- [ ] **Step 2: Move the stage rows.** Stage 1 becomes: *volatility merge (three phase workflows → Project Delivery Workflow; DesignHealthEngine's rules split out as Method Conformance Rules) + the planned-component derivation rule.* Stage 4 gains: *`delivery-manager` + the `deliveryManager` contract + relationships + the three-into-one core use case + every re-keyed dynamic view + the deletion of the three Manager components and contracts — one commit with the new Go package.* Update the must-ship-together list in §8 accordingly, and §10's "Self-amendment blast radius" risk, which now understates it.

- [ ] **Step 3: Write the earmark file**, carrying:
  - **`uitests/testdata/coreUseCasesProject.json` is already drifted and cannot be fixed in a worktree.** It holds **16** decisions against the model's 18. `npm run check:core-use-cases-fixture` is **red today** (verified 2026-09-23) — the regen writes 18 and `git diff --exit-code` fails. It is not wired into any CI workflow (`grep -rn "check:core-use-cases-fixture" .github/workflows/` = nothing) and not into `webApp`'s `npm run check`, which is why it has gone unnoticed. **The regen reads the committed `main` branch, not the working tree** (`cmd/gen-uitests-fixtures/main.go:96` → `projectstate.NewGitLocalProjectStateAccess` → `gitRepoLocator{branch: "main"}`, `projectstateaccess.go:1548–1562`), so running it on a feature branch reproduces main, not the branch. The regen is therefore a **post-merge step**: after the stage-1 merge to `main`, run `cd uitests && npm run regen:core-use-cases-fixture` and commit the result, which closes both the stage-1 delta and the pre-existing 16-vs-18 drift in one go. Do not attempt it inside the worktree.
  - **`.claude/` materialized method assets name the retired Managers.** `.claude/skills/the-method-project-tracking/SKILL.md:56` says an artifact is read "through the `systemDesignManager` view". `.claude/` is materialized from the method-assets platform module (`server/Makefile:9`, `:175`), so the fix is a platform release and a founder STOP in its own right — **earmark, not a stage-1 edit**. The wider sweep (`grep -rln "systemDesignManager\|constructionManager\|projectDesignManager" .claude/skills .claude/commands .claude/agents`) finds exactly that one file today; re-run it at stage 4, when the names actually stop existing.
  - **Carry-forward to stage 4's first commit:** Tasks 2 and 3 of this plan, verbatim, plus `cmd/clientgen/main.go:62` `exposedManagers`, `cmd/appgen/main.go:155` `WebExposedManagers`, `server/internal/arch_test.go` allowlists, the `registered_names_test.go` golden, the `engine_test.go` `realizedViews` table, and the drain of `*:nextActivity:*` and the design sessions.
  - **`activityExecutionAccess` (spec §5.3) is deferred to stage 3**, where its code lands. REPRODUCED: as a `planned` RA with no relationships it fires `SYS-RA-ORPHAN` (**Error** — "every ResourceAccess must encapsulate at least one resource"); with relationships it fires `DV-REL-COVERAGE` (**Error**) until a dynamic view exercises them, which cannot be authored honestly while `gitActivityStatusAccess`, `constructionTransitionAccess` and `designSessionAccess` are the components the code actually calls. There is no `planned` posture for an RA that is both green and true.
  - **The `message-bus` buildStatus inconsistency:** three of the four `provided` utilities carry `buildStatus: "external"` and `message-bus` carries none. Nothing reads it today and Task 4 does not change that (the skip is on `"planned"` only), but it is a latent inconsistency in the model — worth one line in the next System pass.

- [ ] **Step 4: Commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/activity-stage1
  git add docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md \
          docs/bugs/2026-09-23-stage1-model-wave-earmarks.md
  git commit -F - <<'MSG'
  docs(spec): the model wave cannot precede the code — four Error gates say so

  §8's gate recon named ALIGN-* and CC-* but missed SYS-CARD-MGR (Error at a 6th
  Manager, no buildStatus exemption and no waiver path for an Error),
  DV-SINGLE-MGR (Error when a view's Client edges enter more than one Manager,
  which the unified use case does against the three old rails) and
  DH-CONTRACT-FACET (Error for a contract no component owns). With ALIGN-EXTRA-PKG
  closing the remaining door, delivery-manager, its contract, its relationships and
  the collapsed core use case are one indivisible commit that is only green
  alongside the new Go package. The stage table moves them to stage 4; stage 1
  keeps the volatility merge and the planned-component derivation rule.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

## Self-review

**Spec §4 bullets → where they land**

| §4 bullet | Task | Ships in |
|---|---|---|
| B-02/B-03/B-04/B-13/B-14 merge into one **Project Delivery Workflow** | Task 1 Steps 2, 5 | **stage 1** (probe-verified green) |
| `designHealthEngine`'s rules stop being a facet of the system-design volatility | Task 1 Steps 3, 5, 6 — a stand-alone **Method Conformance Rules** volatility, because the component must stay (its Go package exists → `ALIGN-EXTRA-PKG`) and must own a volatility (`DH-VOL-ENCAP-MISSING`, Error) | **stage 1** |
| three managers → **`delivery-manager`** (`deliveryManager`) | Task 2 Steps 1, 3, 6 | stage 4 — `SYS-CARD-MGR` |
| `reviewEngine` generalized | **already done** — stage 2 landed the 7-param `ProposeReviews` (`.serviceContracts.reviewEngine`, slot-5 relationships 17–19). Nothing to do. | — |
| `designSessionAccess` + `constructionTransitionAccess` review/staging verbs unify (§5.3) | Task 5 Step 3 earmark — `SYS-RA-ORPHAN` / `DV-REL-COVERAGE` | stage 3 |
| 3 core use cases, two entries as step-local `alt` groups | Task 3 Steps 2, 3 | stage 4 — `DV-SINGLE-MGR` |
| all former variations re-parent; `commit-to-a-project-option` demoted | Task 3 Step 2 table + the `UC-VARIATION-REF` triple | stage 4 |
| `deliveryManager` contract, 12 ops | Task 2 Steps 4, 5 — every op and every `$def`'s provenance | stage 4 |
| M0 approval is `SubmitReviewDecision` on the projectDesign gate | Task 2 Step 4 (no separate op) + Task 3 Step 2 (`verdict` has no distinct M0 arm) | stage 4 |

**Spec §8 stage-1 row → coverage**

| Row clause | Answer |
|---|---|
| "volatility merge" | Task 1. |
| "`delivery-manager`" | Task 2, moved to stage 4 with reproduced evidence and a founder ruling gate. |
| "3 core use cases + re-parented variations, dynamic views" | Task 3, same move, same commit. |
| "`deliveryManager`/`reviewEngine` contracts" | `deliveryManager` = Task 2 Step 4; `reviewEngine` already generalized by stage 2 — verified in the committed contract. |
| "regen (modelgen, OAS, ops.gen)" | Nothing regenerates in stage 1: no exposed-Manager contract changes and `estimationEngine` is not exposed (Task 4 Step 3 proves it with `git status`). The OAS/`ops.gen` regen belongs to stage 4. |
| "Old managers remain as the implementation until stage 4" | Held — and this plan shows the constraint is stronger than the spec states: they must remain *in the model* too, until the commit that replaces them. |
| "must-ship-together: model + every use-case realization (a half-authored dynamic view is red)" | Enforced in Task 3 (one commit with Task 2) and demonstrated in Task 1 Step 1, where deleting two volatilities without re-pointing their components is made to fail on purpose. |
| "self-amendment procedure" | Every task ends with the §8 loop: `gen-models` → `method-check` → `aiarch-state-mcp validate --slot System` → `test-short` → `npm run check`. Slots 9/10 move only via `derived-plan-write` (Task 4 Step 7), never by hand. |

**Open question carried to the founder:** Task 2 Step 0's ruling. Everything else in this plan is executable as written.
