# Construction UI Rewrite — Stage C: the TASKS lens

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fill the shell's third lens. TASKS lists every decision the construction pipeline is stopped on and waiting for a human to make — a gate awaiting approval, a variance awaiting a steer, an activity that failed — one row per decision, sorted by what it costs to leave it. The shared detail pane then lets the human make the decision in place, and confirms it only on evidence that the agent resumed.

**Architecture:** The owed set is derived from the **live workflow stage** (the per-activity construction session's `awaitingApproval` / `awaitingTakeover`) plus head-state `failed` — never from head-state `in-review`, which only means "some phases complete" (spec §1: the old queue's defect). The route fans the existing per-activity session probe out over the activities the pump has started and not finished (bounded by the supervision cap; zero today), feeds the answers into one pure derivation (`tasks/owedWork.ts`), and hands the result to a presentation-only `TasksLens` and to the shared `DetailPane` as an optional `decision` prop. No server change.

**Tech Stack:** React 19, TypeScript 5.9 (`exactOptionalPropertyTypes`), TanStack Router/Query, MUI 7, `node:test`, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-09-construction-ui-rewrite-design.md` — §6 product rulings, §7.4 shell, §7.7 Lens 3, §7.8 detail surface, §9, §10, §11.
**Predecessors:** Stage A (ledger, provenance), Stage B (shell, list lens, pane) + fix rounds A–D — `construction-ui-rewrite` @ `42e9e94`.
**Branch:** `construction-ui-tasks-lens` off `42e9e94`, worktree `scratchpad/wt-tasks-lens`, in parallel with the Graph lens (`construction-ui-graph-lens`). Not pushed.

## Global Constraints

Everything in Stage B's list still holds (tsc -b is the TS gate, node:test only, no hand-edited generated files, never weaken a gate, no bare `phase` identifier, provenance is a channel, failure is never terminal). Stage C adds:

- **NEVER dispatch or decide against a real server.** Every Playwright spec traps `**/execute-next-activity/**` (abort) and every decision route (`submit-phase-decision`, `override-activity`, `pause-project`, `update-review-policy`, `set-review-policy`) BEFORE navigating — abort, or `fulfill` a canned answer. The dev Vite on :5711 reaches :8888 only through a GET-only proxy.
- **Owed is a live fact.** Head-state `in-review` never makes a row owed; only a session stage or a recorded `failed` does.
- **Unknown renders as unknown.** Where the server does not say (gate-open time, round, which gate key), the cell reads `—` with a tooltip saying why — never a guessed number.
- **Shared files: additive and small.** `ConstructionConsole.tsx` (one lens-ternary arm + one wiring block), `DetailPane.tsx` (one optional prop), `UIIdentifiers.ts` / `testids.ts` (one new block each, placed in the intervention section rather than at the end where the Graph lens appends). `ConstructionShell.tsx` needs no edit: its `tasksOwed` prop already exists.
- Commit messages end with the two attribution lines (Co-Authored-By + Claude-Session).

## Facts (verified 2026-09-12 against the worktree and :8888)

| Fact | Value |
|---|---|
| Live rows | 29 — 23 recorded (all `integrated`, 214 backfilled attempts, no `StartedAt`), 6 planned-no-record |
| Live owed decisions | **0** — no row the pump started; the populated table is verified with route-edited reads |
| `reviewPolicy` | `{}` — no preset, no map. Server `EffectiveGate` then gates **only** the risk floor (construction dispatch + local merge of a deploy/spend/schema contract) |
| `SupervisionCap` | 3 |
| `gitRows` | empty (no PR/CI data) |
| Gate location | `runPhaseGate` → `awaitPhaseDecision` sets `StageAwaitingApproval`, keyed by `phase.String()`; `runLocalMergeStep` sets the same stage keyed `"merge"` |
| Session view | `{stage, pipelinePhase, reviewSet, variance}` — **no** gate key, **no** gate-open time, **no** redraft-exhausted flag |
| `recordActivityStarted` | written only when `gitOn` (the git profile); stored `StartedAt`/`CompletedAt` already ride the get-project wire but `mapConstructionRow` drops them |
| Wrong gate key | `SubmitPhaseDecision` with a key the workflow is not waiting on is **silently discarded** |
| `OverrideActivity` | signals the per-activity workflow; notes required; a failed (exited) activity has no workflow to signal |
| Detail pane actions | `Approve` / `Send back` / `Run` render with **no onClick** today (Stage B) |

## Decisions (where the spec does not decide)

| # | Decision | Why it is the spec-consistent option |
|---|---|---|
| DC1 | The owed set = session stage `awaitingApproval` (a gate) ∪ `awaitingTakeover` (a variance steer) ∪ head-state `failed`. | §1 names head-state `in-review` as the old defect; §6 puts failed rows in the same table. |
| DC2 | Probe the existing per-activity session route only for rows the pump **started and has not finished** (`startedAt` set, no `completedAt`, not `failed`). Zero probes today; ≤ the supervision cap in practice. | Fix A deleted 29 per-load probes; this probes only where a workflow can exist. Limitation → Q1. |
| DC3 | The gate key is the row's `currentLifecyclePhase` (what `PhaseGatePanel` already uses). A merge gate is indistinguishable; the decision flow's resume-evidence check (DC7) makes a swallowed decision loud rather than silent. | §6: "a swallowed approval is worse than no button". The real fix → Q2. |
| DC4 | ROUND = the gate task's attempts in the ledger (pending one included); `—` when the ledger holds none. WAITING = `—` (no gate-open time exists). No staleness flag and no "days of critical path stalled" clause until one does. | "Unknown is shippable, false is not" (R6). → Q2. |
| DC5 | WHY attributes the gate to the **current** policy when it gates `(type, lifecycle phase)` (`preset: checkpoints`, `gated: service › detailed_design`); otherwise to the **risk floor** — inferred by elimination, never by mirroring the server's keyword list — with a tooltip noting a policy in force at the activity's start could also explain it. The risk-floor flag is the same inference. | "No hand-mirroring the server"; policy is snapshotted at workflow start. |
| DC6 | The degraded banner stays permanent while `reviewPolicy` is empty, but its words are made true: the server gates only the risk floor under an empty policy, and rows come from the live workflow stage. The spec's text ("showing the 3 default gates; tasks are derived from activity status only") describes the old queue and would be false here. | §7.7's own rule: never present a hardcoded constant as project policy. |
| DC7 | On decide: `sending` → `awaitingResume` (poll that activity's session) → `resumed` ("resumed — now in <phase>", lingers 30s, then leaves) **or** `notLanded` (still awaiting after 12s — loud) **or** `failed` (request error — loud, with the 4xx/5xx honesty rule from fix C). | §6 "confirmation is evidence of the resume, not acknowledgement of the click". |
| DC8 | Wave-1 actions: Approve · Send back (note **required** — free text, or anchored comments) · Open in GitHub (only when a PR url exists; no dead link). Takeover and failed rows get Review + GitHub only. | §6 wave-1 list; steering verbs → Q3. |
| DC9 | Policy is read-only in Tasks: a one-line summary of what the project gates, and a link to the home page's review-policy control, labelled "Stop asking me about this class of thing". The "what got auto-approved" report is not built: nothing records an auto-approval. | §10 cuts policy editing from Tasks (read-only + link). |
| DC10 | The empty state's CTA uses the header's Begin/Resume label (from `constructionStarted`) and opens the **existing** confirm dialog. Its counts come from `computeActivityStatuses` over the committed network. | §7.7 empty state; the fix-A ruling that the label is the server's truth. |
| DC11 | The shared toolbar's search/kind/layer/scope apply to Tasks through the same `applyToolbarToActivities` pipeline (an owed row shows iff its activity passes). "Observed only" never hides an owed row: the owed facts are live, i.e. observed. | §7.4: toolbar state persists across lenses. |
| DC12 | The single `.find()` phase gate and its `PhaseGatePanel` under the list retire: the decision moves into the shared pane, reachable from every lens, and the pane's reviewer set comes from the probed session of the **selected** activity. | §1 (the `.find` can return one), §7.7 ("[Review] opens the detail pane in place"). |

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `webApp/src/contracts/types.ts`, `wire.ts` | `startedAt` / `completedAt` onto `ConstructionRow` | 1 |
| `webApp/src/components/construction/tasks/owedWork.ts` *(new)* | pure: probe candidates; session × rows → owed items | 1 |
| `webApp/src/components/construction/tasks/owedRanking.ts` *(new)* | pure: blast radius, WHY attribution, default sort | 2 |
| `webApp/src/hooks/useConstructionSessions.ts` *(new)* | the session probe fanned out over N activities | 3 |
| `webApp/src/routes/ConstructionConsole.tsx` | owed set wiring, badge, pane reviewSet, retire `.find` | 3, 5 |
| `webApp/src/components/construction/tasks/tasksLensCopy.ts` *(new)* | pure: headline, empty-state counts, banner, policy summary | 4 |
| `webApp/src/components/construction/tasks/TasksLens.tsx` *(new)* | the table, header, banner, empty state | 4 |
| `webApp/src/components/construction/tasks/decisionFlow.ts` *(new)* | pure: decide → resume-evidence state machine; send-back readiness | 5 |
| `webApp/src/components/construction/detail/DetailPane.tsx` | optional `decision` prop: wired Approve/Send back + note composer | 5 |
| `uitests/tests/construction-tasks-lens.spec.ts` *(new)* | live empty state + route-edited owed rows, decide flows (trapped) | 6 |

---

### Task 1: The owed set, from the live stage

**Files:** `contracts/types.ts`, `contracts/wire.ts`, `tasks/owedWork.ts` (+ `.test.ts`), `contracts/constructionAdapters.test.ts` (mapper case).

**Interfaces — produces:**
- `ConstructionRow.startedAt?` / `.completedAt?` (ISO strings, absent when null).
- `probeCandidatesFor(rows): string[]` — sorted ids of rows started, not completed, not failed.
- `owedItemsFor({ rows, sessions, titleFor }): OwedItem[]` where `OwedItem = { key, reason: 'gate' | 'takeover' | 'failed', activityId, title?, kind?, lifecyclePhase?, gate?: { task, label, bookLabel, exitCriterion, phaseName }, round?: number, reviewers: ConstructionReviewer[], variance?: string, failure?: { reason, detail? } }`; `key` = `<activityId>:<gateTask>:<round>` when all three are known, else `<activityId>:<reason>`.

The derivation rules, in order:
1. A `failed` row is owed (reason `failed`), whatever its session says.
2. Otherwise a row is owed only if its session is present and its stage is `awaitingApproval` (reason `gate`) or `awaitingTakeover` (reason `takeover`, carrying `variance.summary`). **An `in-review` row with no session, or with a running session, is not owed.**
3. A gate's lifecycle phase is `row.currentLifecyclePhase`; its gate task is the profile phase's `gate: true` task (from the generated profile — `taskBriefing.profileFor`); an unclassified row or a phase the profile lacks yields a gate with no task (rendered `—`), never a guessed one.
4. ROUND counts the gate task's attempts in the ledger; none → undefined.

- [ ] **Step 1: failing tests** — `in-review` without a session is not owed (the §1 defect pinned); `awaitingApproval` is owed with the profile's gate task (service `detailed_design` → `designReview`); `awaitingTakeover` carries the variance summary; `failed` is owed with no session; a `pipelineRunning` session is not owed; candidates exclude completed, failed, never-started and planned-no-record rows; ROUND is the ledger count, undefined on an empty ledger; the mapper carries `StartedAt`/`CompletedAt` and drops nulls.
- [ ] **Step 2:** run, confirm red.
- [ ] **Step 3:** implement (pure; relative value imports carry `.ts`).
- [ ] **Step 4:** `npm run typecheck && npm run lint && npm test && npm run build`. **Mutation-verify** rule 2 (owe `in-review` rows) and the candidate filter (drop the `completedAt` check).
- [ ] **Step 5:** commit `feat(construction): the owed set comes from the live workflow stage, never from in-review`.

### Task 2: Blast radius, the policy rule that opened the gate, and the default order

**Files:** `tasks/owedRanking.ts` (+ `.test.ts`).

**Interfaces — produces:** `downstreamOf(network, activityId, done): string[]` (transitive successors through dependency rows, milestones traversed but not counted, activities already `integrated` excluded); `whyFor(policy, kind, lifecyclePhase): { rule: string; riskFloor: boolean; tooltip }`; `sortOwed(items, facts): OwedItem[]`.

- WHY (DC5): explicit map hit → `gated · <kind> › <phase name>`; preset hit (`checkpoints`: detailed design / construction / integration; `full`: every phase) → `preset · <name>`; otherwise → `risk floor`, `riskFloor: true`. A `failed`/`takeover` row → `variance · interventionEngine` / `failed · <reason>`, `riskFloor: false`. The preset table is the server's `EffectiveGate`, restated for **attribution only** (it decides nothing on the client); the test file cites `projectstateaccess.go EffectiveGate` so a drift is findable. (Recorded here because it is the one place this stage restates server logic; the alternative — a server-computed attribution — needs Q2's session fields.)
- Sort (§6): risk-floor first · blast radius desc · age desc (age unknown today, so ties fall through) · activity id.

- [ ] **Step 1: failing tests** — a chain A→M1→B→C with B integrated counts C once and M1 not at all; diamonds count once; WHY for `{}` policy is `risk floor`; `checkpoints` at `test_plan` is the floor, at `detailed_design` is the preset; explicit map wins over an unset preset; sort puts a floor gate with blast 0 above a non-floor gate with blast 5, then blast desc.
- [ ] **Step 2–5:** red, implement, gates, **mutation-verify** the milestone rule and the floor-first key; commit `feat(construction): blast radius, the rule that opened the gate, and the risk-floor-first order`.

### Task 3: Probe the in-flight sessions; the badge and the pane read them

**Files:** `hooks/useConstructionSession.ts` (export its query options), `hooks/useConstructionSessions.ts` *(new)*, `routes/ConstructionConsole.tsx`.

- `useConstructionSessions(projectId, activityIds)` = `useQueries` over the SAME per-activity options (same keys, same dormant-404-as-null probe, same polling) → `Record<activityId, ConstructionSessionState | null>`.
- Route: `candidates = probeCandidatesFor(rawRows)` → sessions → `owed = owedItemsFor(...)`. The TASKS badge = `owed.length` (was: the `in-review` count). The pane's `reviewSet` = the SELECTED activity's session (was: only the one `.find` hit). The `.find` / `PhaseGatePanel` mount under the list and its approve/send-back closures move out (DC12) — Task 5 rewires them into the pane.

- [ ] **Step 1:** test the hook's pure helper (`sessionsByActivity(ids, results)` — pairs results to ids, drops errors to `undefined`, keeps `null`).
- [ ] **Step 2–4:** red, implement, gates; drive the live app: badge absent (0 owed), no session GETs issued (network log), list unchanged.
- [ ] **Step 5:** commit `feat(construction): probe only the sessions a workflow can exist for; the badge counts real decisions`.

### Task 4: Render the lens

**Files:** `tasks/tasksLensCopy.ts` (+ `.test.ts`), `tasks/TasksLens.tsx`, `routes/ConstructionConsole.tsx` (the `tasks` arm), `UIIdentifiers.ts`, `testids.ts`.

- **Header** (§7.7): `N decisions are blocking M downstream activities` + ` · K on the critical path` when K>0; a second line `G of CAP worker slots stopped at a gate` when G>0. No days-stalled clause (DC4).
- **Banner** (DC6), permanent while `reviewPolicy` is absent or empty.
- **Policy line** (DC9): what the project gates + the "Stop asking me about this class of thing" link to `/project/$projectId/home`.
- **Columns** (§7.7): WHAT (float rail via `bandTokens` + numeral, CP border weight, `activity › phase › gate task`, book name secondary where it differs, KindBadge) · WHY (rule, risk-floor mark) · WHO (reviewer roles from the session; `—` if none) · WAITING (`—` + tooltip, DC4; ROUND beside it) · BLAST (`↓N` + float + CP) · ACTION (`[Review]` primary; `[GH ↗]` secondary, only with a PR url). The seven PM triage fields: ask (a sentence from the gate task label + exit criterion), blast, waiting, round, shape (`Contract · N ops` from `serviceContracts`, `Test plan · N scenarios` from `testingState`, else `—`), machine verdict (CI status from `gitRows`, else "no CI record"), risk floor.
- **Selected** row highlights; `[Review]` selects `{a, p, k}` (gate) or `{a}` (takeover/failed) — the pane opens in place.
- **Empty state** (§7.7, DC10): "Nothing needs you." + `E eligible · F in flight · B blocked` + the Begin/Resume button (opens the existing confirm).
- Tokens only; five themes; no chip for unknown.

- [ ] **Step 1: failing tests** for the copy helpers: headline pluralisation and CP clause; the banner present for `undefined` and `{}` policy, absent for a populated one; empty-state counts computed from statuses (a fixture with a different split moves the numbers); shape strings; the ask sentence for a gate with and without a resolved task.
- [ ] **Step 2–4:** red, implement, gates; live screenshots at 1280/1366/1600 of the empty state.
- [ ] **Step 5:** commit `feat(construction): the TASKS lens — one row per decision, and "Nothing needs you" when that is true`.

### Task 5: Decide in the pane, and prove the resume

**Files:** `tasks/decisionFlow.ts` (+ `.test.ts`), `detail/DetailPane.tsx` (optional `decision` prop), `routes/ConstructionConsole.tsx`, `UIIdentifiers.ts`, `testids.ts`.

**Interfaces:** `DetailPaneProps.decision?: { gateTask?: string; lifecyclePhase: string; pending: boolean; flow: DecisionView; onApprove(): void; onSendBack(note: string): void }`. When present and the selection is the gated activity (activity-level or its gate task), the pane state is `awaitingHuman`, Approve/Send back carry handlers, Send back opens a required-note composer (anchored comments count as the note). Without the prop the pane is byte-for-byte Stage B.

`decisionFlow.ts`: `step(state, event, now)` over `idle → sending → awaitingResume → resumed | notLanded | failed`; events `sent`, `sendFailed(status)`, `session(stage, phase)`, `tick`. `resumed` holds 30s then `idle`; `awaitingResume` past 12s → `notLanded`. `sendBackReady(note, anchoredCount)`.

The Tasks row reflects the flow in place ("resumed — now in Construction" / "decision did not land — the gate is still waiting" / the error), and the item leaves after the linger.

- [ ] **Step 1: failing tests** — a session leaving `awaitingApproval` after `sent` is `resumed` naming the new phase; still awaiting after 12s is `notLanded`; a 4xx is `failed` "rejected", a 5xx/network is `failed` "outcome unknown" (fix-C rule); `resumed` clears after 30s; send-back not ready with an empty/whitespace note and no anchors, ready with either.
- [ ] **Step 2–4:** red, implement, gates. **Mutation-verify** the `notLanded` timeout and the note requirement.
- [ ] **Step 5:** commit `feat(construction): decide in the pane; confirm only on evidence of the resume`.

### Task 6: Playwright — the lens, driven

**Files:** `uitests/tests/construction-tasks-lens.spec.ts`.

All trapped (Global Constraints). Owed rows are produced by editing the real project read in the browser (the fix-D pattern) and fulfilling the session route for the edited ids.

- live: the lens renders "Nothing needs you." with counts and a disabled-until-known Begin/Resume; no badge; zero session probes.
- a gate + a takeover + a failed row: three rows, floor-first order, WHY/WHO/BLAST cells, badge `3`; an `in-review` row with a running session is absent.
- `[Review]` opens the pane in place on the gate task with Approve/Send back; Send back is disabled until a note is typed.
- Approve → fulfilled 200 → the session stub flips to `pipelineRunning` → the row reads "resumed"; a stub that keeps awaiting → "did not land" alert; a 500 → "outcome unknown".
- the banner reads the corrected copy; GH link absent without a PR url.
- Screenshots at 1280/1366/1600 into `scratchpad/stage-c/`.

- [ ] Steps: write, run against Vite :5711 with the construction specs, `npm run lint` in uitests, commit `test(construction): drive the TASKS lens`.

---

## Stage C exit criteria

```bash
cd webApp && npm run typecheck && npm run lint && npm test && npm run build
cd ../uitests && npm run lint && npx playwright test construction-   # against :5711
```
No Go changes are planned; if one lands, the full Go gate list from the brief applies.

## Open questions

**Q1 — Architecture: where should the Tasks lens learn which activities are stopped at a human stage?** Today it probes the per-activity session of activities whose stored row says the pump started them (DC2). But `recordActivityStarted` is written only in the git profile, so in a non-git composition no row is ever "started" and the lens is **blind** — it would say "Nothing needs you." while a gate waits. *Recommendation:* the project-level pump/supervision query enumerates its children at `awaitingApproval`/`awaitingTakeover`, with the gate key and the time the stage was entered, and get-project (or one session call) carries that list. Not blocking for this stage: DC2 is correct for the git profile the app runs today.

**Q2 — Architecture: may the session view carry `awaitingGate`, `awaitingSince` and `redraftExhausted`?** They are the missing inputs for WAITING, the staleness flag, "days of critical path stalled", the §9.8 throughput metric, the redraft-exhausted signal (§6 must-hold 2) and a correct merge-gate decision (DC3). Setting them requires touching `awaitPhaseDecision` and `runLocalMergeStep`, and R6 says "do not touch `awaitPhaseDecision`" (in the verdict-persistence deferral). *Recommendation:* allow it as metadata only — three fields on `constructState` set where the stage is set, surfaced by the existing query; no new workflow commands, no signal semantics change, `workflow.Now` is replay-safe, so no `GetVersion` gate. Not implemented on this branch.

**Q3 — Product + architecture: what do "Retry"/"Run" do for a failed activity and for a variance awaiting a steer?** A failed activity's workflow has exited, so `OverrideActivity` has nothing to signal and no verb re-queues it. A takeover has four override kinds and no wave-1 action. *Recommendation:* takeover → `Run` = `OverrideRetry`, `⋯` = Takeover / Skip / Reassign, note required (the server already requires notes); failed → a new, reviewed verb that clears the failure and returns the activity to the pump's eligible set. Until ruled, those rows carry Review + GitHub only (DC8) and the pane's Run stays as inert as Stage B left it.

**Q4 — Design (non-blocking, list lens): the head-state `in-review` conflation outside Tasks.** The list's "Awaiting me" scope and the pane's state chip still read `in-review` as "awaiting you", and the pane shows (inert) Approve/Send back for any `in-review` row. This stage wires the buttons only through the live `decision` prop, so the inert ones can never send anything. *Recommendation:* the list and pane read the same live owed set; do it on `construction-ui-rewrite` after this branch merges, to keep this diff off the list files.

## Earmarks

- Probe fan-out is per page load while in flight; a project-level read (Q1) replaces it.
- The "auto-approved" calibration report (§6) needs a record of auto-approvals — none exists.
- The Exited-skipped row (7a concern 4) reads Running/InReview on head-state; Tasks is immune (session truth), the list is not.
