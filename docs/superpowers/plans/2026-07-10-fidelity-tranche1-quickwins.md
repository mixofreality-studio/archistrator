# Fidelity Tranche 1 — Quick-Win Defects Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land every no-ratification-needed defect fix from the fidelity plan §9 "Now" list and the workflow plan's "Now" list — doctrine bugs, ghost references, stale orchestration docs, the missing CI gate, four Go control-flow defects (O3/O4/O5/O8), the six prompt-context gaps, and the F5 contract cleanup.

**Architecture:** Three independent batches: (A) doctrine/docs — `.claude/` skill+command fixes, no code; (B) CI — the existing `make method-check` target becomes a required gate; (C) Go — small, test-first fixes in systemdesign/construction managers + prompts; (D) one contract change (PipelineSpec) with regen. All anchors below verified at main @4390645 (recon 2026-07-10); re-verify line numbers at execution time, cite by symbol not line where possible.

**Tech Stack:** Go (`GOWORK=off` from `server/`), Temporal SDK, modelgen regen pipeline (`make gen`), GitHub Actions, markdown skills.

## Global Constraints

- All Go commands run from `server/` with `GOWORK=off`. Baseline verify: `GOWORK=off go build ./... && GOWORK=off go test ./internal/... -count=1`.
- Never weaken existing gates; new gates are additive. Never hand-edit `*.gen.go`; contract changes go project.json → `make gen` → commit both together.
- Temporal determinism: workflow-side edits use existing replay-safe idioms; any new workflow logic behind `workflow.GetVersion` if it changes command sequence for in-flight workflows.
- Zero behavior change EXCEPT where a task explicitly states its intended behavior change (O3/O4/O5/O8 are deliberate control-flow fixes; each states its new behavior and pins it with a test).
- Doc tasks: the-method skills cite the book via `../../../research/rightingsoftware/OEBPS/...` (3 levels up + `research/`); slot references use the real state layout (no flat-key fiction, no `.handoff`).
- Branch: `fidelity-t1` off main. Commit per task, style: `<area>: <what> — <why>` + `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` + Claude-Session trailer.
- Out of tranche (do NOT touch): orphan contracts artifactValidationEngine/systemDesignEngine (promoted as components later by ratified H1 — deletion would be wrong), workItemAccess contract (H3), the 2 uncovered dogfood edges (H4 view redraw), A1 schema extensions, ReviewPolicy default (D1), deployment/documentation dispatch (D5).

---

### Task 1: Fix the inverted ratio rule + Temporal-grep contradiction in the standard-check skill

**Files:**
- Modify: `.claude/skills/the-method-system-design-standard-check/SKILL.md` (item 2d at ~:76; §I items 7a/7b/7d at ~:128-140; item 6c at ~:118)

**Interfaces:** none (doc). Consumes the golden-ratio table from `the-method-layers/SKILL.md` (Cardinality section) — cite, don't restate.

- [ ] **Step 1: Replace item 2d.** Current (INVERTED): `| 2d | Golden Engines-to-Managers ratio | Confirm more Engines than Managers (or at least 2:1 favoring Engines) |`. Replace with:

```markdown
| 2d | Golden Engines-to-Managers ratio | Confirm FEWER Engines than Managers per the golden-ratio bands in [the-method-layers](../the-method-layers/SKILL.md#cardinality) (1→0–1, 2→1, 3→2, 5→3; ≥8 Managers = failed design). A deliberate excess is a WAIVE with explicit justification naming each Engine's strategy-volatility, never a silent pass. |
```

- [ ] **Step 2: Fix §I items 7a/7b/7d (and re-check 6c).** These grep `architecture.dsl` for Temporal-primitive edge-label prefixes (`StartWorkflow(`, `Activity:`, `Schedule[`) that `the-method-architecture/SKILL.md:14` explicitly bans from `Relationship.Label`. Rewrite 7a/7b/7d to check what the architecture skill actually emits: 7a → "every Client → Manager edge label is a use-case-level operation or responsibility phrase (no transport/engine primitives)"; 7b → "every Manager → ResourceAccess edge label is an atomic business verb of the target RA"; 7d → delete (workflowExecutionAccess Temporal-primitive labeling contradicts the ban; the Temporal mapping lives in the-method-layers' Temporal table as an *operational* mapping, not a DSL-label rule). Keep 6c only if it asserts ABSENCE of `Activity:` prefixes (consistent with the ban) — verify its current text and align its rationale wording.
- [ ] **Step 3: Grep for regressions:** `grep -rn "StartWorkflow(\|Activity:\|Schedule\[" .claude/skills/the-method-system-design-standard-check/` → remaining hits must all be in prose explaining the *ban*, not in check instructions demanding the prefixes.
- [ ] **Step 4: Commit** — `skills: fix inverted 2d engine-ratio rule + delete Temporal-label greps contradicting the architecture label ban`

### Task 2: Kill the `.handoff` ghost slot

**Files:**
- Modify: `.claude/skills/the-method-handoff/SKILL.md` (:10,35,90,98,100,165,176), `.claude/skills/the-method-scope-change/SKILL.md` (:139,211), `.claude/skills/the-method/SKILL.md` (:72 index row), `.claude/commands/implement-project.md` (:38,52,68,73,212 — coordinate with Task 4 which rewrites this file; do Task 4 first and this file's `.handoff` refs die there)

**Interfaces:** the real home is `.constructionProgress.HandOffModel` (string, `projectstate/contract.gen.go:237`), written via the construction Manager (AgentHidden — agents/skills READ it, the decision is recorded through the Manager's rail). `the-method-project-state/SKILL.md:37` already states this correctly — match its wording.

- [ ] **Step 1:** In each file, replace every `.handoff` slot reference with `.constructionProgress.HandOffModel`, and rewrite any "produces the committed handoff artifact in … `.handoff`" claims to: the skill produces the hand-off DECISION; it is recorded on `.constructionProgress.HandOffModel` (see [[the-method-project-state]]); there is no `.handoff` slot.
- [ ] **Step 2:** Fix the index table row in `the-method/SKILL.md:72` → `| [[the-method-handoff]] | once at Phase 3 start | .constructionProgress.HandOffModel |`.
- [ ] **Step 3:** `grep -rn "\.handoff" .claude/` → zero hits (except historic mentions inside docs/superpowers plan docs, which are records — leave those).
- [ ] **Step 4: Commit** — `skills: .handoff ghost → .constructionProgress.HandOffModel everywhere`

### Task 3: Fix stale pre-clock-correction activity taxonomy

**Files:**
- Modify: `.claude/commands/project-design.md` (:53-57 Step 2 bullet list; :64 Type line)
- Modify: `.claude/skills/the-method-network-draft/NETWORK-SCHEMA.md` (:36 type enum; :49-:62 worked example; :114-115 activity-types table; :163 validation rule)

**Interfaces:** normative source is `the-method-activity-list/SKILL.md`: ONE coding activity per component; detailed-design/construction are internal lifecycle PHASES of that activity, not separate activities. Type enum: `coding | integration | quality | noncoding` (verify the activity-list skill's exact enum wording first and mirror it).

- [ ] **Step 1:** `project-design.md` Step 2: replace the two per-component bullets ("One `detailed-design` activity (5–10 days, senior-developer)" / "One `construction` activity (5–35 days, junior-developer)") with: `One coding activity per component (5–35 days; detailed-design → test-plan → construction → integration run as internal phases per the activity profile — see [[the-method-activity-list]])`. Fix the `:64` Type line to the corrected enum.
- [ ] **Step 2:** NETWORK-SCHEMA.md: fix the enum (:36), collapse the worked example's A002/A003 pair into one coding activity (keep ids stable where the example's later references allow; renumber consistently otherwise), fix the activity-types table rows and the `:163` rule (`type == coding ⇒ component non-null`).
- [ ] **Step 3:** `grep -rn "detailed-design.*activity\|type: detailed-design\|type: construction" .claude/commands/project-design.md .claude/skills/the-method-network-draft/` → zero taxonomy hits (phase-name mentions are fine).
- [ ] **Step 4: Commit** — `skills: clock-correction taxonomy — one coding activity per component in project-design + NETWORK-SCHEMA`

### Task 4: Rewrite `/implement-project` as a thin console-driving procedure

**Files:**
- Modify: `.claude/commands/implement-project.md` (full rewrite, ~200 lines → ~60)
- Modify: `.claude/agents/junior-developer.md` (:103 — `aiarch/construct/<activity-id>` → `activity/<activity_id>`, matching the phase stubs)

**Interfaces:** the LIVE Phase-3 model (verified in recon + orchestration map): server pump owns dispatch (`ExecuteNextActivity` → `PumpNextActivityWorkflow` → per-activity `ConstructActivityWorkflow`), Manager owns ALL phase status (agents never self-mark), agent jobs run `.claude/commands/<profileSlug>-<phase>.md` stubs on branch `activity/<id>`, human gates via `SubmitPhaseDecision`. The command's remaining legitimate jobs: Step 0 hand-off decision (via [[the-method-handoff]] → `.constructionProgress.HandOffModel`), start/drive construction from the console or API (`POST /api/v1/construction/execute-next-activity/{projectID}`), monitor (`GetSessionState`/tracking views), and route the human's gate decisions.

- [ ] **Step 1:** Rewrite the command: (1) preconditions (network+activityList committed, SDP decision recorded, hand-off model recorded); (2) Begin/pump-tick procedure (the API call; what the pump does — link, don't restate); (3) monitoring (what to watch, where); (4) gate decisions (SubmitPhaseDecision semantics, review routing via [[the-method-review-routing]]); (5) variance/escalation handling (operator override verbs); (6) weekly tracking pointer ([[the-method-project-tracking]]). DELETE: the Step-3 type-routing table (:101-140), self-marked status writes (:97-99, :150-153), the devops-agent line (:214), all `.handoff` refs (Task 2 coordination). Preserve any content that documents real console verbs — verify each claimed verb exists on the construction façade before keeping it.
- [ ] **Step 2:** `grep -rn "aiarch/construct" .claude/` → zero hits after the junior-developer.md fix.
- [ ] **Step 3: Commit** — `commands: implement-project rewritten to drive the live pump rail — agents never self-mark status`

### Task 5: Dead artifacts + dead book paths

**Files:**
- Delete: `.claude/hooks/validate-structurizr.sh`, `.claude/structurizr-validate`, `.claude/structurizr-serve`
- Modify: `.claude/settings.local.json` (:7-14 — remove the PostToolUse registration; if this file is user-local/untracked, report instead of editing and remove only tracked references)
- Modify: `.claude/skills/the-method-architecture/SKILL.md` (11 dead `../../../../rightingsoftware/...` paths at :19-27,:71,:102,:197,:249 → `../../../research/rightingsoftware/...`), `.claude/skills/the-method-testing/SKILL.md` (:14 `../../../rightingsoftware/` → `../../../research/rightingsoftware/`)

- [ ] **Step 1:** Check tracking status first: `git ls-files .claude/settings.local.json .claude/hooks/validate-structurizr.sh .claude/structurizr-validate .claude/structurizr-serve`. Delete only tracked files via git; untracked user-local files: remove the hook registration only, note in report.
- [ ] **Step 2:** Fix the 12 book paths; spot-check 3 resolve: `ls research/rightingsoftware/OEBPS/xhtml/ch03.xhtml` etc.
- [ ] **Step 3:** Leave `server/cmd/validate` ALONE (recon: not dead — used manually in a754b6c; wiring it is not this task).
- [ ] **Step 4: Commit** — `claude: delete dead structurizr hook+helpers; fix 12 dead book citation paths`

### Task 6: MCP preamble on the 14 phase stubs missing it

**Files:**
- Modify: the 9 `*-integration.md` + 5 `*-requirements.md`/`*-test-plan.md` command stubs in `.claude/commands/` (enumerate by grep at execution)

**Interfaces:** copy the preamble VERBATIM from an existing carrier (e.g. `.claude/commands/service-construction.md` — the "State changes go through the aiarch-state MCP tools" block naming `recordServiceContract`/`recordPhaseArtifact`/`recordTestingState`/`publishDraft`), adjusted only where a stub's phase writes a different artifact set (integration stubs record phase artifacts + testing state; requirements/test-plan stubs record phase artifacts). The 5 top-level commands (system-design, project-design, implement-project, sdp-review, add-use-case) are console procedures, not job stubs — they get NO preamble (they don't run as MCP-equipped jobs).

- [ ] **Step 1:** `grep -L "aiarch-state MCP" .claude/commands/*-integration.md .claude/commands/*-requirements.md .claude/commands/*-test-plan.md` → list; add the preamble to each, phase-appropriate.
- [ ] **Step 2:** Verify no stub instructs hand-editing project.json: `grep -rln "edit.*project.json\|Edit project.json" .claude/commands/` → zero.
- [ ] **Step 3: Commit** — `commands: aiarch-state MCP preamble on all phase stubs (14 were missing it)`

### Task 7: methodcheck into CI

**Files:**
- Modify: `.github/workflows/server-checks.yml` (add step after `make test-short`, ~:55)

- [ ] **Step 1: Prove it's green on main first:** `cd server && GOWORK=off make method-check` (target at Makefile:204-205). If it FAILS on current main, STOP this task and report the failures — fixing dogfood-state violations is adjudication, not CI wiring (likely interactions: Task 1's rule fixes live in skills, not the Go checker — but the Go checker may carry the same inverted rule; if so, fix the Go rule in this task with the same golden-ratio bands, then re-run).
- [ ] **Step 2:** Add the workflow step:

```yaml
      - name: method-check (Method design rules, -tags methoddesign)
        working-directory: server
        run: GOWORK=off make method-check
```

- [ ] **Step 3:** Commit + push branch; confirm the workflow runs it (`gh run watch` on the branch's checks or verify in the PR's checks list).
- [ ] **Step 4: Commit** — `ci: methodcheck suite is now a required server check — hand-committed state can no longer bypass the Method rules`

### Task 8: O3 — one seal path (auto-seal enforces the façade's gates)

**Files:**
- Modify: `server/internal/manager/systemdesign/workflow.go` (`runPhaseAdvance` ~:1422-1459; parent call at :434), `server/internal/manager/systemdesign/systemdesignmanager.go` (extract the STD-FAIL-OPEN check :523-532 + STALE-UNACKED check :533-543 into a shared, workflow-callable gate)
- Test: `server/internal/manager/systemdesign/workflow_test.go`

**Interfaces:** Produces `func phaseSealGate(proj projectstate.Project /* or the view both paths already hold */) error` (name/signature per what both call sites can share — the façade checks run on ctx reads, the workflow on activity-read state; the shared function takes the DATA, each caller fetches it its own way). Behavior change (intended): a project whose standard check carries a FAIL item, or with unacknowledged stale slots, no longer auto-seals after the 8th approve — the parent workflow surfaces the refusal (existing DraftFailed/awaiting semantics — pick the idiom the parent already uses for gate refusals and reuse it; do not invent a new stage).

- [ ] **Step 1 (failing test):** parent workflow with all 8 kinds committed but standard check carrying a FAIL item does NOT advance phase; same for unacked stale basis. Follow the existing parent-workflow test harness in `workflow_test.go` (find the test that drives the full Phase-1 cascade).
- [ ] **Step 2:** Extract the two façade checks into the shared gate; façade calls it (behavior unchanged — keep its exact error codes/messages); `runPhaseAdvance` calls it before `ProjectStateAdvancePhase`, behind `workflow.GetVersion("phase-seal-gate", ...)` since it adds a command-sequence-visible refusal path for in-flight workflows.
- [ ] **Step 3:** Tests pass; full suite; commit — `systemdesign: auto-seal runs the same STD-FAIL-OPEN + stale-basis gates as the manual verb (O3)`

### Task 9: O4 — pause reliably reaches the pump cascade

**Files:**
- Modify: `server/internal/manager/construction/workflow.go` (pump pause gate ~:352-368), `server/internal/manager/construction/signals.go` (pause branch — what `RecordOperatorPaused` persists), possibly `eligibility.go`
- Test: `server/internal/manager/construction/workflow_test.go`

**Interfaces:** Design (decided): the durable pause signal is HEAD-STATE, not signal racing. The supervision workflow's pause branch already executes `recordOperatorPaused` → verify what it writes (find the RecordOperatorPaused activity → which projectstate field). The pump already reads the project every tick (`readProject`, workflow.go:369+): add a check — if head-state says operator-paused, go quiet exactly like the existing signal gate (same `pumpDispatch{Decided: true, Dispatched: false}` + log, no ContinueAsNew). Keep the existing same-execution signal gate as the fast path. Resume path: find what clears the paused record (resume verb/branch) and assert the pump dispatches again after it. If `RecordOperatorPaused` turns out to persist nothing pump-readable, STOP and report — extending head-state is an A1-adjacent schema decision, not this task's call.

- [ ] **Step 1 (failing test):** pause recorded in head-state (construct the state the pause branch actually writes) → pump tick returns quiet, no child dispatched. Companion: un-paused state dispatches (existing tests cover; don't duplicate).
- [ ] **Step 2:** Implement the head-state check (pure addition before `nextEligible`; GetVersion guard per the pause-gate precedent comment at :330-333).
- [ ] **Step 3:** Tests + full suite; commit — `construction: pump honors operator pause via head-state each tick — PauseProject now actually halts the cascade (O4)`

### Task 10: O5 — unresolvable activity skips itself, not the whole tick

**Files:**
- Modify: `server/internal/manager/construction/eligibility.go` (:73-83 — `chosen = candidates[0]` + whole-tick false return)
- Modify: `server/internal/manager/construction/workflow.go` (pumpDispatch query struct — add skipped-activity visibility)
- Test: `server/internal/manager/construction/next_eligible_activity_test.go`

**Interfaces:** Behavior change (intended): iterate candidates in order; an activity whose component can't resolve is SKIPPED (structured warn) and the next candidate dispatches; the pump's `pumpDispatch` query gains `SkippedActivities []string` (activity name + reason) so the console/operator can SEE starvation instead of a false "quiescent". Only when ALL candidates fail does the tick return quiet — and then `pumpDispatch` carries the full skip list. No schema change (query-only surface).

- [ ] **Step 1 (failing test):** two eligible candidates, first unresolvable, second resolvable → second dispatches, skip list names the first. Second test: all unresolvable → quiet + both listed.
- [ ] **Step 2:** Implement (loop in `nextEligible` or its caller — follow the existing structure; keep selection order semantics for resolvable candidates identical).
- [ ] **Step 3:** Tests + full suite; commit — `construction: unresolvable activities skip themselves and surface in pumpDispatch — one bad title no longer starves the pump (O5)`

### Task 11: O8 — SubmitPhaseDecision precheck

**Files:**
- Modify: `server/internal/manager/construction/constructionmanager.go` (:329-347)
- Test: `server/internal/manager/construction/constructionmanager_test.go`

**Interfaces:** Mirror systemdesign's F19 pattern (`SubmitReviewDecision` at `systemdesignmanager.go:292-313`: query live state → `checkReviewPrecondition` → FailedPrecondition naming the actual stage). Construction's per-activity workflow already exposes a session/state query (find the query the console uses for `ActivityTrackingDetail`); precheck: the target activity workflow exists AND is currently gated on the named phase; otherwise refuse `FailedPrecondition` naming the actual phase/stage. Behavior change (intended): a decision for a non-gated phase returns an error instead of silently vanishing.

- [ ] **Step 1 (failing test):** decision for phase X while workflow gated on phase Y (or not gated) → FailedPrecondition; decision for the live gated phase → signal delivered (existing happy-path test stays green).
- [ ] **Step 2:** Implement precheck (Describe/Query before signal, per the F19 idiom including the dead-workflow defense from `systemdesignmanager.go:331-337`).
- [ ] **Step 3:** Tests + full suite; commit — `construction: SubmitPhaseDecision prechecks the live gate — wrong-phase decisions refuse instead of vanishing (O8)`

### Task 12: Prompt context gaps — all six priors lists

**Files:**
- Modify: `server/internal/manager/systemdesign/prompts.go` (case arms :60-75; `writeResearch` usage), `server/internal/manager/projectdesign/prompts.go` (:60-63)
- Test: `server/internal/manager/systemdesign/prompts_test.go` + projectdesign equivalent (find existing prompt tests — construction_prompts_test.go pattern)

**Interfaces:** New priors per artifact kind (Method basis in workflow plan §6.3):
- Glossary: priors Mission + **research corpus** (`writeResearch`) — Four Questions run over the domain.
- ScrubbedRequirements: Mission, Glossary + **research corpus** — scrubbing interrogates the customer's actual statements.
- Volatilities: Mission, Glossary, ScrubbedRequirements + **research corpus** — competitor lens + longevity heuristic need business context.
- CoreUseCases: Mission, Glossary, Volatilities, **ScrubbedRequirements** (its own skill's Reads line).
- StandardCheck: **Mission, Glossary, ScrubbedRequirements, Volatilities, CoreUseCases**, System, OperationalConcepts — the App C walk needs all its inputs.
- (projectdesign) Network: ActivityList, PlanningAssumptions, **System** — behavioral dependencies come from the call chains. ActivityList: System, PlanningAssumptions, **OperationalConcepts**.
`writeResearch` currently fires only for KindMission (:60-61) — refactor so research-pointer emission is per-kind (same pointer mechanism; do NOT inline corpus bytes — pointers keep the 64KB budget safe).

- [ ] **Step 1 (failing tests):** per kind, assert the emitted prompt names the required priors (string-contains on `writePriorsPointer` output) and the research pointer where required.
- [ ] **Step 2:** Implement the case-arm changes.
- [ ] **Step 3:** Tests + full suite; commit — `prompts: every design step now names the priors the book says it consumes (scrub/stdcheck/network gaps + 3 material)`

### Task 13: F5 — PipelineSpec sheds its GH-Actions shape

**Files:**
- Modify: `.aiarch/state/project.json` → `serviceContracts.constructionPipelineAccess.$defs.PipelineSpec` (remove `WorkflowFile`; replace `DispatchInputs map[string]string` with the typed fields the dispatch actually carries — read `server/internal/manager/construction/workflow.go` `dispatchInputsFor` for the real set: idempotency token, activity id, component id, command, phase, role; model as a typed `DispatchPayload` $def)
- Regenerate: `cd server && make gen` (constructionpipeline contract.gen.go + any dependents)
- Modify: `server/internal/resourceaccess/constructionpipeline/actions.go` + callers (managers build the typed payload; RA maps it to `workflow_dispatch` inputs internally — the GH shape becomes RA-private), `server/internal/manager/{construction,systemdesign,projectdesign}` call sites as compile errors direct
- Test: existing constructionpipeline + manager suites

- [ ] **Step 1:** Read `dispatchInputsFor` + both design managers' dispatch builders — enumerate every key actually sent; design the typed `DispatchPayload` $def covering all three call families (design jobs carry artifact_kind/design_prompt/target_branch/prior_state_ref/job_mode — verify against `SD/dispatch.go`). If design-job and construct-job payloads genuinely differ, model two typed defs, not one stringly map.
- [ ] **Step 2:** Edit project.json $defs; `make gen`; confirm diff confined to constructionpipeline gen files + OAS.
- [ ] **Step 3:** Fix compile errors: managers construct typed payloads; RA serializes to GH inputs internally (keep the 64KB-cap comment with the RA, where it now belongs). `WorkflowFile` was already construction-time config (`actions_http_client.go:70`) — just delete the contract field and any dead plumbing.
- [ ] **Step 4:** Full suite + `make method-check`; commit — `constructionPipelineAccess: typed dispatch payload; WorkflowFile/DispatchInputs GH shape becomes RA-private (F5)`

---

## Sequencing & verification

Order: Tasks 1–6 (docs, parallelizable, zero risk) → 7 (CI gate — AFTER doc fixes so the checker/skills agree) → 8–12 (Go, independent of each other) → 13 (contract+regen last, biggest blast radius). Every Go task ends at the full baseline verify; Task 13 additionally runs `make method-check`. Final: whole-branch review, then founder merge decision.

## Self-Review Notes

- Spec coverage: fidelity §9 items → Tasks 1-7 (Mercedes-test already fixed — recon; orphans/uncovered-edges deliberately excluded per ratified H1/H4); workflow-plan Now items → Tasks 8-13 + prompt gaps Task 12. All 16 live recon items mapped; 2 recon-FIXED items dropped.
- Known judgment points left to implementers WITH guardrails: Task 7 Step 1 (methodcheck may fail on main → STOP semantics), Task 9 (head-state design decided, but STOP if RecordOperatorPaused persists nothing readable), Task 13 Step 1 (one vs two payload defs — decided by reading the real call families).
- Type consistency: pumpDispatch.SkippedActivities (Task 10) is query-surface only — no projectstate schema change anywhere in this tranche.
