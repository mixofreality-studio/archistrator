# Stage 4b1 (the generic DAG child) — earmarks, carry-forwards, and the open founder questions (2026-09-27)

Stage 4b1 shipped §5.2/§5.3/§5.4/§6/§7.2 of `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md`: **ONE generic DAG child** (`deliveryActivity`, `{p}:activity:{a}`) where `walkPhases` + `coauthorartifact.go` + `coauthorphase2artifact.go` stood; **deterministic Project Design** (the compute writes slots 9–16, M0 is an ordinary gate with no send-back); the **five refused construction write paths made real** (GAP-6 closed); the **stranded-round sweep**; `applyRecovering`'s row-level terminality class; the round's artifact kind as a FIELD; `RoundWithdrawn`'s own wire member; the commit a round judged; **the seven retired workflow types and their files deleted** (golden 141 → 134, frozen 22 → 15), with the nineteen pre-4b1 replay fixtures archived as evidence and **eight fresh ones captured against a real Temporal server** in their place.

Branch `activity-experience-stage4b1` from `origin/main` @`86d3223a`. Plan `fe0385fc` + follow-up `c3165d95` (four pre-flight rounds, five blockers, three re-checks). Code commits in landing order:

| Task | Commits |
|---|---|
| 1 — the behaviour oracle (`Test_LifecycleShapes`, 7 cases, replay-archive README) | `3c1db06b` |
| 2 — thirteen design-health rules leave the Manager for `designhealth` | `65a65f35` |
| 3 — `ReviewRound.artifactKind` as a FIELD | `a6fb9e0f` |
| 4 — `RoundWithdrawn`'s wire member | `aae23748` |
| 5 — `gateSubjectRef` names the commit; `OpenActivity` refuses a re-type | `38fd7f9c` |
| 7 — the row re-read + `MutateTerminalConflict` (+ fix round 1) | `290ce5ec`, `5d6c4f31` |
| 6 — `deliveryRoundSweep`, the stranded-round sweep | `31edd214` |
| 8 — the strategy table + the DAG walker + the signal router | `e5b25b06` (+ minors `77caaa09`) |
| 9 — Project Design is computed; M0 is an ordinary gate | `b30cfc82` (+ findings `91ddd6fe`) |
| 10 — Requirements and Architecture walk the child; the pump re-points | `839d98a6` (+ fixes `ff64776a`) |
| 11 — construction walks the child; the variance loop per TASK | `7ddaa445`, `618467bc` |
| 12 — the five write paths; the requeue re-open; the open-comment guard | `4eadbd48`, `179a43b2`, `2650dffa` |
| 13 — the twins die; the façade collapse; the fresh fixtures | `4039c0e8`, `98e4a906`, `bdf90047` |

Gates at ship (measured at `bdf90047`, the code HEAD this file was written against): registered-names golden **134**, frozen workflow names **15**, `Test_Replay_DeliveryHistories` **8/8** (201–537 events each), `Test_LifecycleShapes` **10/10**, `GOWORK=off go test -short ./...` whole module green, non-short `./internal/` (the full-stack boot test) green, `make lint` **0 issues**, `fix-check` clean, 9× `gen-*-check` no drift, `encapsulation-check` + `derived-plan-check` + `method-check` green, `validate --root .. --slot System` **43 advisory / 0 errors** (unmoved all wave), `webApp npm run check` **1217/1217** + `build:mcp`, `systemtests` builds.

Claims below carry the task and the commit that produced them. Where a number was re-measured for this file at `bdf90047`, it says so.

---

## Deploy note — 4b1 does not deploy, and not alone

The drain note lives in `docs/bugs/2026-09-24-stage3-rail-earmarks.md` and was amended in the same commit as this file. In one line: **one drain covers stages 3 + 4a + 4b1 + 4b2, once, before one release** — 4b2 re-keys the pump and folds the two design session view types, so a release between 4b1 and 4b2 buys a second drain for nothing. What 4b1 adds is the `{p}:activity:*` id family, a THIRD Schedule (`delivery:roundSweep`), and **seven retired workflow TYPE names whose in-flight executions are unresumable and must be terminated by hand** — the bounded cost architect Ruling 3(d) accepted. Read the amended note before releasing anything; do not rely on this summary.

## The measurement §9 asked for, and it HOLDS

Spec §9's acceptance — *"line count of delivery manager < sum of predecessors"* — is measured at the end of 4b per §5.2's amendment. Re-measured for this file at `bdf90047` with the recipe the plan states (`wc -l internal/manager/delivery/*.go`; hand-written = total − `manager_test.go` − the four `*.gen.go`; the `fake/` subdirectory is outside the glob at every baseline, so the three figures are comparable):

| | total | non-test | hand-written non-generated non-test |
|---|---:|---:|---:|
| `4baed01a` — the three predecessor packages (`systemdesign` + `projectdesign` + `construction`) | 61,576 | 29,778 | **25,643** |
| `86d3223a` — the 4b1 base (`delivery`, post-4a) | 59,763 | 27,379 | **25,162** |
| `bdf90047` — **4b1 HEAD** | **44,665** | **21,201** | **18,983** |

Both baselines were re-derived from git rather than copied: the three-package sum at `4baed01a` reproduces §5.2's `25,643` / `29,778` exactly, and `86d3223a` reproduces `25,162` / `27,379` exactly.

**§9's acceptance holds, on every reading.** Hand-written: 18,983 < 25,643 — **74.0% of the sum of predecessors, −6,660 lines (−26.0%)**; against the 4b1 base, −6,179 (−24.6%). Non-test: 21,201 < 29,778 (−28.8%); against the base, −6,178 (−22.6%). Including tests: 44,665 < 61,576. The shrink is real deletion inside one package, not a move — 3,364 of the retired 10,700 production lines MOVED and are counted in the 18,983.

Two honest caveats a reader of that number is owed. **First, the test half shrank harder than the production half**: 31,798 → 23,464 lines (−26.2%), and §9's acceptance does not measure tests, so the ratio flatters the wave slightly. The coverage question is its own item below, and it is the one thing in this wave that wants a second pair of eyes. **Second, the number is now a floor to defend, not a win to bank**: 4b2 deletes `child.Get`, the eight Phase-2 draft slugs, `DesignBranch` and one of the two design session view types, and adds a react-by-signal pump. If hand-written lines go UP through 4b2, the acceptance is where that will show.

---

## FOUNDER / ARCHITECT QUESTIONS (nothing is blocked on them; each is owed a ruling)

### Q1 — GAP-4B-4: THREE artifact kinds live in no lifecycle, and it is three, not two

`operationalConcepts` (slot 6, status 2, three revisions), `standardCheck` (slot 7, status 4 — the only non-committed slot) **and `scrubbedRequirements`** appear in **no lifecycle task and no required-kinds list**. Measured against `method-assets@v0.9.0`: the eleven artifact kinds any lifecycle task names are Construction, CoreUseCases, DetailedDesign, Glossary, Integration, Mission, SdpReview, SRS, STP, System, Volatilities — and none of the three is among them. Yet all three keep **live `DesignCommandFor` slugs** (`operational-concepts-draft`, `standard-check-draft`, `scrubbed-requirements-draft`) with matching method-assets command files, and all three are named in `ReviewRoundInput.roundId`'s own contract text as kinds that share a lifecycle phase. Nothing in 4b1 drafts them and nothing deletes them.

The ruling is a choice between two: **add them to the `requirements` / `architecture` lifecycles** (a method-assets release, and then the pump drafts them), or **declare them frozen-as-committed** (and retire the three slugs and their command files). Note what rides on it: resolving this is what makes Task 3's `ReviewRound.artifactKind` field **load-bearing rather than defensive** — today the one kinded row this repo holds is `architecture:architectureReview:operationalConcepts:2`, i.e. a round judging a kind no lifecycle names.

### Q2 — when and where the founder edits the DEFAULTED planning assumptions

R-E, as the controller overrode it. Slot 8 absent no longer refuses: the compute fills The Method's defaults, records WHICH FAMILIES it assumed, and says so at M0. What is missing is the **UX** — an operating-model screen where the founder replaces an assumed calendar or rate card with a decided one, and a way to tell "assumed" from "accepted" once they have read it. Until then **a founder can approve a cost built on defaults they only ever saw in a copy line.**

**"Who authors slot 8" is NOT the open question** — the compute does, provisionally, by design (§6's "no agent-drafted steps" is what makes a default the only alternative to a dead end). Seven families can be defaulted and recorded, each with its source in its own doc comment: settlement terms, infrastructure kind, calendar days per week, declared usage, rate card, indirect daily rate, resources. The defaulting is **per family, not all-or-nothing** (Task 9 D1) and a NAMED value is never replaced.

### Q3 — `terms.revenueShare == RevenueShareUnknown`: the SDP has been uncomputable since 2026-06-09

The sharpest finding of the wave, and it is about committed state, not code. Slot 8 IS committed (status 2, two revisions) and its `terms.revenueShare` is `0`. `projectstate.RevenueShareUnknown == 0`. `billingEngine.ProjectCommitTimeRevenueShareAndComputeCost` refuses an unknown regime outright — *"settling real money under an unregistered revenue-share regime is a financial-correctness hazard… the Engine NEVER silently falls back to a default regime."* **So the SDP assembly could not run at all against the state it describes, and has not been able to since the 2026-06-09 merchant-of-record reversal wrote `revenueShare: 0` to mean "no revenue share".** That one unusable field is a sufficient explanation for slots 11–16 carrying `revisions: 1` + `staleBasis: true` ever since: nothing could re-derive them.

4b1 works around it (Task 9 `b30cfc82`): a field whose value is its vocabulary's UNKNOWN member is absent in the only sense that matters, so it defaults and the fill is recorded. The default is `RevenueShareNegotiatedRate` **at 0%**, because "no revenue share" has no member of its own.

**The ruling wanted: add a `RevenueShareNone` member to `RevenueShareKind`** (a `project.json` enum edit plus codegen, barred from Task 9), then point `defaultSettlementTerms` at it and rewrite slot 8. **Cost if the ruling goes the other way:** every M0 screen on this project says the billing terms were assumed — honest, but noisy — and an SDP priced at a 0% negotiated rate is what the founder approves. Visible in the M0 review either way.

### Q4 — `CalendarDaysPerWeek` is committed as **2**, where The Method's default is **5**

Not a defect and deliberately NOT defaulted (a named value is never replaced — `Test_DefaultPlanningAssumptions_DoNotOverridePresentData` asserts the 2 survives, and asserts the durations differ between committed and defaulted assumptions). But it is the single biggest lever on every duration and cost in the SDP, it was authored once, and nothing has re-confirmed it. Worth an explicit founder re-ratification at the next M0 rather than an inherited 2.

### Q5 — a construction question has no answerer

`AskTaskQuestions` records the questions on the round as `ReviewComment.type = question` and the SPA renders them. **That is all.** No agent job can answer a construction round's thread: `respondToReviewComment` is **slot-scoped** and is **not registered in the construction job mode** (`cmd/aiarch-state-mcp/tools.go` — `modes: {draft, answer}`), and a construction round has no kind and no slot, so a dispatched answer job would start a session that finds nothing to do. The addressee (`pm` / `architect`) is a **human** until method-assets grows a construction answer command. The ruling: **write that command, or decide explicitly that construction questions are human-answered** and label the addressee accordingly on screen.

### Q6 — do design questions belong on the activity branch, or on main beside the slot?

`resolveQuestionBranch` asked a live per-kind co-author session for its session branch. There is no such session after 4b1, so it reads the **slot-derived** view, and the derived view never reports a live stage — therefore it **always falls back to main**. Consequence: **design questions land on MAIN while the draft under review is staged on the ACTIVITY branch** (`activity/{activityId}`). Nothing is lost — the slot's thread on main is what the SPA reads and what the answer job answers — but a question filed mid-draft no longer lands beside the draft it is about. The honest fix resolves the branch from the activity's own branch when its child is live (`designActivityFor(kind)` plus a liveness read); it is small, and Task 13 did not take it because it is a behaviour change the brief did not name. **This is a product question as much as a code one.**

Note the same read makes `projectstate.DesignBranch` **dead-but-compiled**: re-measured at `bdf90047`, its one surviving non-test caller is inside `resolveQuestionBranch`, past a guard that can no longer be true.

---

## The contract additions 4b1 made — and the ONE it could not make

**4b1 added exactly two wire members and one description, all APPENDED or in place. It added no contract OP.**

| Change | Where | Task |
|---|---|---|
| `ReviewRound.artifactKind` / `ReviewRoundInput.artifactKind` (optional) | `activityExecutionAccess.$defs` | 3 · `a6fb9e0f` |
| `TaskRevisionOutcome` += `withdrawn` / `TaskRevisionWithdrawn` | `deliveryManager.$defs` | 4 · `aae23748` |
| `OperatorNoteKind`'s **description**, facet-scoped | `activityExecutionAccess` + `constructionTransitionAccess` | 12 · `179a43b2`, `bdf90047` |

### The re-open is a FOLD, not a thirteenth verb — and the brief's prediction that `ReopenActivity` is "the one 4b1 contract addition" is **STALE**

Task 12 reported the reopen path BLOCKED and specified a 13th op, `activityExecutionAccess.ReopenActivity`. The controller ruled it unblocked and sanctioned the contract edit. **The op was then built exactly as specified, measured, and withdrawn**, because two requirements could not both hold:

```
--- FAIL: TestGreenFixtureAdvisoriesFire
    rule DH-CONTRACT-OPCOUNT-MAX should NOT fire on the committed (green) state, but it did
--- FAIL: TestRegisteredTemporalNamesGolden   got 142, want 141   + activityExecutionAccess.reopenActivity
```

`activityExecutionAccess` sits at **exactly 12 ops**, App-C's ceiling, and designhealth's own advisory pin asserts `DH-CONTRACT-OPCOUNT-MAX` **ABSENT** on the committed state — *"the largest surviving contract is 12 … the FIRST time the repo has had no Manager contract past App-C's ceiling"*, which is what the 4a collapse achieved. A thirteenth verb is not one more advisory; it breaks that gate. No other op on the facet is removable (all twelve have live callers).

**So the re-open is `RecordOperatorNote{Kind: NoteRequeue}` re-arming a terminal row** (`179a43b2`). Every substantive line of the ruling's spec survives the fold — it clears exactly `StartedAt` / `CompletedAt` / `FailureReason` / `FailureDetail`, preserves the attempt ledger, the review ledger, the pin, the type, `Produced` and the notes, CASes both versions, and refuses a non-terminal row — and three things got better:

1. **`requeue` finally has a writer.** `NoteRequeue` has been in `OperatorNoteKind` since stage 3 as a vocabulary member nothing wrote.
2. **ONE commit instead of two.** The two-op shape recorded the reason and then re-armed; a crash between them leaves either a re-armed activity with nobody's name on it, or a reason filed against an activity that was never re-armed. The fold makes it atomic — the store's own "an unexplained decision is not an audit entry" rule, by construction.
3. **No new registered Temporal name**, so the golden stayed 141 through Task 12 and the frozen list was untouched.

**How the pump re-selects a re-opened activity is the half that was nearly missed** (Task 12 review round 3, `bdf90047`). With `StartedAt` cleared, `PumpWroteRow` is false — but `CoarsePhaseFor` then falls through to the LEDGER, and a ledger whose every gate passed resolves **Done**, so `isActivityDispatchable` answered false for the very activity the operator had just re-opened. The re-open cleared the facts the pump reads and handed it a derivation that put the terminal straight back. The fix is DATA, not a fifth head field: `projectstate.RequeuedAfterExit(row)` asks two facts the store already holds — the head must read not-started, and **the newest requeue NOTE must be newer than the newest RESOLVED attempt** — and `isActivityDispatchable` asks it BEFORE the ledger derivation. The second half is what stops a stale note re-arming an activity that was re-opened, re-run and finished again; the note IS the evidence, which is why no new field was needed.

Three things follow, and all three are earmarks:

- **The re-open is MANUAL.** Nothing re-opens an activity on its own. The design rail's post-exit slot-commit window in particular (a Completed activity whose slots are still `AwaitingReview`) is now **detectable from head state alone** — a sweep could find it and re-open it. Not 4b1's; a good 4b2 candidate, and it would reuse `deliveryRoundSweep`'s exact shape.
- **`RequeuedAfterExit` compares a server clock to an attempt's `EndedAt`.** Both are written by the same store today, so they share a clock. If a future writer ever stamps an attempt from a workflow's deterministic time while the note keeps the server's, a re-run finishing inside the same tick as its requeue reads as "not re-armed". The comparison is strict (`After`), so a tie falls on the SAFE side (not dispatchable) — the direction that fails loudly rather than looping.
- **`reopenActivity` is check-then-act**, like every other precheck in this façade: it pre-checks terminality on the row it just read and refuses `FailedPrecondition`, and the store's `fwra.Conflict` is the backstop for the genuine race. That pre-check is not cosmetic — without it `onActivityRow` read the store's refusal as a RACE and retried three times, so an operator re-opening a still-running activity got *"changed concurrently … re-read it and try again"*: a sentence about a race that never happened.

### `OverrideActivity` now means two things, keyed by liveness

Steer an escalation (a live child, a failed dispatch awaiting a takeover) **or** re-open a finished activity (no live child). Every override KIND reaches the re-open arm when the session query answers `NotFound`, because an operator looking at a finished activity is asking for it to run again whatever word the button carried. **The server cannot tell which the operator MEANT, only which is possible** — so the SPA must label them separately. Task 14's.

---

## What goes live ON THIS REPO at the next deploy

**The pump starts the three design activities on the first tick.** `phase == 2`; slot 9's derived prefix holds `requirements` / `architecture` / `projectDesign`; none of the three has an execution row; the new `eligibleWithDesign` rung admits them. So **the first thing that happens after this release is three design activities being opened and closed.**

That is correct, and it is load-bearing on ONE guard: `seedWalkFromLedger`'s `committedArtifactOfTask` arm. Re-measured — all seventeen slots are status 2 except `standardCheck` (status 4) — so every task of all three walks seeds **passed**, the walks dispatch nothing, commit nothing, merge nothing and exit. **If that guard were wrong, the pump would re-draft a mission committed months ago.** It has a test (`Test_DesignWalk_AlreadyCommittedSlotsAreNotRedrafted`), and the guard protects the general case too (a project whose LEDGER is empty and whose SLOTS are committed — every project onboarded before the execution ledger, and any whose backfill has not run). On THIS repo the two answers agree: `requirements` holds 8 passed attempts, `architecture` 2 plus a passed round 2, `projectDesign` 1.

**Watch the first tick anyway.** A drain-then-watch is the cheap version of trusting a guard.

Also live at the same tick: `delivery:roundSweep` at 300 s, which reads every project every five minutes and writes head state only where a stranded round exists.

---

## Carried to 4b2 (the out-of-scope list)

1. **`child.Get` is still the self-cascade.** The pump blocks on the child and `ContinueAsNew`s per tick; react-by-signal, pump-as-merge-queue and `PumpResult.activityIds` are architect Ruling 3(b) and are 4b2's. The comment naming 4b2 is at the site.
2. **The eight Phase-2 draft slugs are dead-but-green, and retiring them with their eight `.claude/commands/*.md` is ONE coordinated platform step** (R-F). Verified at `bdf90047`: all eight command files are present — `planning-assumptions-draft`, `activity-list-draft`, `network-draft`, `normal-solution-draft`, `subcritical-solution-draft`, `compressed-solution-draft`, `decompressed-solution-draft`, `risk-model-draft`. Nothing dispatches them (the compute does their work), and `DesignCommandFor` still answers for every one.
3. **`DesignBranch` and the one live branch survive unused for one release.** `origin/aiarch-design/archistrator/0-amend-1` is the only `aiarch-design/**` ref on the remote (verified). `projectstate.DesignBranch` is dead-but-compiled (see Q6).
4. **The three `kind:'session'` members are now TWO DERIVED views plus ONE live one** (R-J). `session` and `projectSession` lost their producers and are rebuilt from the durable SLOT (`designCompletedSessionView` / `planCompletedSessionView`); `constructionSession` is the child's live `sessionState` query. Collapsing three into one is 4b2's, and it is a WIRE change — which is also why four pairs of near-twin bodies survive (`askDesignQuestions` / `askPlanQuestions`, `ackDesignStaleBasis` / `ackPlanStaleBasis`, the two refusals, the two completed-session views): they differ only by the kind SET they admit and the wire TYPE they return, and `ProjectView` carries both members. Where the two were genuine byte-twins they WERE folded (`readProjectMaybeBranch`, `ListEpisodesForArtifact`, `resolveQuestionBranch`, `dispatchAnswerJob`, `watchAnswerEpisode`).
5. **A PUSHED job-completion signal would remove the polling the backoff ladder mitigates**, and it needs an RA producer nothing has. 4b2's, and **Task 8 Step 6's observe selector is the seam it plugs into** — the loop already receives on the task's own inbox between polls, so a completion signal is one more arm, not a rewrite.
6. **The three deprecated RA facets are still there** and still post-drain (see the drain note's entry criteria). 4b1 did not touch them; `designSessionAccess.commitArtifactWithProvenance` is still how the child commits design slots.
7. **The artifact-as-of-revision read** (R1/GAP-5) and **the batched `QueryProjectView(plan)`** (R3/GAP-7) are untouched. 4b1 ratified the revision handle the first one needs: `SubjectRef.ref` in the `<branch>@v<version>` shape, which is what `ReadProjectAtRef` should take.
8. **`constructState`'s single-valued VIEW fields are clobbered on a fork.** `stage`, `reviewSet`, `awaitingGate`, `awaitingSince`, `pipelinePhase` describe ONE gate and the walk can hold two — proved, not theorised (`Test_DeliveryGate_TwoSimultaneousHumanGatesAreDecidedIndependently`). **The LEDGER is correct** (each review coroutine owns its own `*gateLedger`; the single-valued one would have cross-decided rounds on a fork); what a QUERY sees is the most recently entered gate. A per-task session view is UI work.
9. **`TaskAttempt` still carries no artifact kind.** A kinded design round therefore cites NO gate attempt. That is the honest answer today, but `attemptGateKey` is the ONE change site and it needs a `project.json` edit plus codegen.
10. **Nine of Task 2's thirteen moved rule ids collide with platform `methodcheck` ids on purpose**, so `aiarch-state-mcp validate` (which runs both tiers) reports a violating draft **twice** for those nine. Accepted precedent (the CC-* family already does it, and the webApp's Design Health surface joins on rule id across both call sites), but the count a reviewer sees on a RED draft is 2× for nine rules. Three rule PAIRS now overlap deliberately (`SYS-ENCAPSULATES`/`DH-COMP-NO-VOLATILITY`, `USECASE-DYNAMIC-MISSING`/`DH-COV-UC-DYNAMIC`, `SYS-VOLATILITY-COVERAGE`/`DH-VOL-ENCAP-MISSING`) — both members of each pair are silent on the committed state and each catches something its partner does not. Whether the Design Health view should show two findings for one defect is a founder ruling for a later wave.

---

## Measurements that are FLOORS, not facts

### The child's history budget is a floor, not a measurement (R-L)

`deliveryActivityHistoryBudget = 4000` events and the **23-poll** observe ladder (4 × 15 s, 9 × 60 s, 10 × 300 s) are derived from FIXTURE counts in a test environment plus the production poll arithmetic. **Nobody has yet watched a real `requirements` activity's history length.** The fresh fixtures put a whole walk at 201–537 events, which is where the 4,000 came from with room to spare — but a fixture is a fast-forwarded walk with no human sitting at a gate for a day, and the long tail is exactly what a budget is for. **First production run: record the observed history length and re-tune the budget against the observed number, not the predicted one.** `shouldContinueAsNew` is checked only at `inflight == 0`, so the budget is never enforced mid-fan-out; the `continue-as-new-mid-walk` shape case drives the real const (there is no injectable budget — the SDK's `SetCurrentHistoryLength` made one unnecessary).

### The observe ladder's 23 polls replaced two flat loops of 240

Both flat observe loops were re-pointed at the one ladder and their four consts deleted. The 19 replays stayed green because the first four polls are 15 s on both schedules and every fixture terminates inside them. The ladder's total wall-clock bound is ~1h 5m per dispatch; nothing has measured a real agentic job against it.

### `fwra.Transient` row reads retry unbounded under the generated default

Construction and both design rails alike, and `manager/operations/deploy.go` + `manager/billing/onboard.go` carry their own `applyRecovering` loops with the same project-version-only re-read. One envelope question for all of them, deliberately not fixed on one rail inside one task. A preset was added during Task 7 and then **reverted** when the belief behind it was checked and found false (`fwra` carries `Retryable` per error and framework-go honours it, so `NotFound` returns on the first attempt with no preset).

---

## Defects, near-misses, and the class the final review must sweep for

### THE CLASS: a guard that moved rails and did not move with them

**The open-review-comment guard was a REGRESSION, landed across Tasks 8–10 and fixed in Task 12 round 2 (`2650dffa`).** On the retired rails an OPEN review comment gated two things: the `vibes` autogate (an `OpenReviewCommentIDs == 0` precondition) and the human approve (a Manager refusal plus a child re-check). On the generic child, `OpenReviewCommentIDs` had **ZERO callers**; `runGate`'s no-human arm read no thread, and neither did `precheckTaskDecision` nor `decideTaskGate`'s approve arm. So under `vibes` the child auto-approved over open comments, and it became REACHABLE the moment Task 12 shipped the construction Ask. Nothing in the gate set said so; it was found by a reviewer reading the retired rail's guards against the new one.

**This is the shape the final whole-branch review must sweep for: a precondition that lived in a deleted body and was not re-asserted in the body that replaced it.** Three more instances of exactly this class were caught during the wave, which is why it is worth a systematic pass rather than a spot check:

- `runTask` returned a production error **before** `recordTaskAttempt`, so an operator saw a failed activity with an EMPTY attempt ledger — while `sdpComputeStrategy`'s own doc comment claimed the attempt was recorded (Task 10 D7).
- `dispatchConstructionOnce` returned on a submit error before `resolveWorkAttempt`, leaving a PENDING attempt forever (Task 12 D1). The test double could not reach it: `SubmitAgenticJob` could not fail.
- `eligibleUnder` compared `== eligibleDispatchable` where the rules are CUMULATIVE, silently demoting the newest rung (Task 10 D10).

The asymmetric pair of rules the fix landed on is worth keeping straight, because they are deliberately NOT the same rule: **the autogate holds for a human on ANY open comment, questions included** (doctrine makes an open question a soft warning, never a hard block — but a warning needs somebody to warn, and the autogate's premise is that nobody has to look; a read that FAILS also holds, because an unknown thread is not an empty one), while **the façade's approve refuses over open CHANGE REQUESTS only**, in the design rail's exact words, because there the human IS the one being warned. A gate held only by an open comment auto-passes the moment the last one is resolved, through the comment-status **mirror signal** — which is what put the `statuses` channel back to work after Task 12 had measured it dead.

### Under `vibes`, an agent critic's SEND-BACK holds the gate for a human

Ruled and landed as Task 13's FIRST commit, separately (`4039c0e8`). `criticVerdictFor` records a revise honestly as a `sendBack` verdict, and `runGate`'s no-human arm did not read verdicts — so a critic that asked for a revision with no anchored comment was auto-approved over. A redraft loop under `vibes` is unbounded and nobody warns, so the ruling was: hold for a human. One more condition on the same arm.

### M1 — a latent kind, waiting on a vocabulary

Construction tasks DO name `artifactKind`s (`srs`→SRS, `detailedDesign`→DetailedDesign, `construction`→Construction, `integration`→Integration, `stp`→STP). None of them is one of the seventeen design SLOTS, so `ArtifactKindFromWireName` does not resolve them and a construction round is **kindless** — which is exactly what `ReviewRound.ArtifactKind`'s optionality means. **The latent: if SRS / DetailedDesign / Construction / Integration / STP ever join `projectstate.ArtifactKind`, every construction round silently gains a non-nil `artifactKind`** — and `roundGateKey` keys on it, so rounds that shared a gate stop sharing it and `latestRoundFor` answers differently. `roundKindOfTask` is the one resolver both writers and readers go through (pinned across all fourteen lifecycles, with the lifecycle count itself pinned at 14 so a dropped release cannot leave the case green), which is what makes this a one-site change rather than a hunt. Task 12's D3 headline claim ("no construction task names an artifact kind") was FALSE and is retracted at the section itself.

### The override storm was a real drain defect, and it is fixed

65 overrides delivered to a task that drains them became 65 sequential `recordOperatorNote` CAS writes from one coroutine while a sibling fork branch wrote its own attempts, and the per-activity CAS genuinely exhausted — `MutateConflictExhausted`, **an activity killed by an operator pressing a button repeatedly** (measured, by the reviewer and the implementer independently). Fixed at the drain (`618467bc`): `drainInboxWhileDispatching` collects a pass's overrides and writes **ONE note per note KIND**, texts joined by the separator `renderOperatorNotes` already uses. Grouping by kind rather than folding everything into one note is deliberate — `PendingOperatorNotes` reads the Kind to decide what rides the next dispatch, so a retry and a skip must not become one note claiming to be either. First-seen kind order, so the writes are replay-deterministic. **Residual:** a LATER pass's overrides are too late for a dispatch that already carries one, and say so in the log.

### `walkState.feedback` drops a send-back's anchored COMMENTS

`signalNotes(fb)` is a string, so `carryWalkFeedback` hands the redraft the notes TEXT and an empty comment list, where the retired rail's `roundFeedback` rebuilt the open comments from the round. **The round still holds them, so nothing is lost from the record — the AGENT just does not see them.** This is also what makes the stage-3 earmark *"no test covers a NEW send-back after a completed redraft carrying only the new round's comments"* **still open, and now weaker than when it was written**: 4b1's cases cover one send-back → redraft → approve and nothing beyond it, and the carry they exercise is text-only.

### Two more prechecks are check-then-act, with their consequences written down

- **Withdraw at a live gate** is refused, and that refusal IS the design (a live gate is answered with Approve, a send-back or a re-dispatch; withdraw is for the round nobody is judging). A gate that opens in the millisecond after the session read would be withdrawn out from under — and the real consequence is now recorded: the CHILD's own `DecideReviewRound` hits the round's terminality Conflict, `terminalAfterRowReread` recognises it in one or two attempts (it does NOT burn the bound), the walk fails the activity legibly, and the requeue recovers it. Routing the withdraw through the child's inbox does not apply: that needs a child, and the window's premise is that there was none.
- **`SubmitTaskDecision` / `OverrideActivity`** carry the same residual window they have had since B1.3, and the round ledger's terminality Conflict is the backstop.

### Two doubles that were more permissive than the store, and what they hid

Recorded because the pattern recurred five times in this wave and each instance hid a real arm: `AppendReviewVerdict` appended to a round the row did not hold and reported **success** (the store answers `NotFound`); `SetReviewCommentStatus` logged the id and applied **nothing**, so a resolve of a nonexistent comment, a resolve onto the wrong round and an illegal transition all reported success; `RecordAttemptOutcome` overwrote an already-resolved attempt (the store refuses); `SubmitAgenticJob` could not fail at all; `ReadActivityExecution` answered a zero row where the store answers `NotFound`. **All five were tightened rather than worked around**, and one of them (`RecordAttemptOutcome`) is why `producedSubject.AttemptRecorded` looked harmless when removed.

**ONE store rule is deliberately NOT mirrored, and it dies with the rail:** the store refuses a RE-OPEN of a resolved attempt, and the retired rail's `DefaultVersion` path provoked exactly that (`Test_Construct_LedgerPartialResume_DefaultVersion_KeepsTheStoredSeed` re-mints attempt 1 over a resolved one). Mirroring it would have turned a pre-existing production hazard on a pre-marker history into a red test for a rail Task 13 deletes — which it then did, so **the rule is now unreachable and the earmark is discharged by deletion**. It is recorded in the replay-archive README rather than dropped, because the hazard predates this wave and the archived histories are the only place it still exists.

### Nine — now eight — comments still name the retired sentinel

`ErrDesignActivityNotDispatchable` / `SkippedDesign` have **zero CODE hits** (the gate) and, re-measured at `bdf90047`, **eight comment hits**, each the historical record of the deletion at the site of the deletion. Task 10 reported nine before Task 13's deletions moved one. This repo's convention favours the record; if the controller wants a literal-zero grep, the phrasings soften without touching behaviour.

### Smaller ones, each with its one-line consequence

- **`SYS-SERVICES-EXPLOSION` is one rename away from firing on our own state.** 4a left exactly 3 Managers and 3 core use cases, so the COUNT arm already matches; only the 60% name-mirroring threshold keeps it quiet (currently 0/3 mirror). Renaming `OperationsManager` toward its use case's wording trips it.
- **`constructionPumpSweep` is missing from the frozen-names list**, by the same argument that put `deliveryRoundSweep` in it (a Schedule holds the workflow TYPE name as live namespace state). Task 13 left the omission untouched; it is a one-line edit and a real hole.
- **`slot 16`'s committed `recommendation` is `"opt-decompressed"`**, an `OptionID` scheme the current code no longer produces (it renders `decompressedSolution`). The next rewrite of slots 11–16 changes that string.
- **`Solution.ClassRates` is now DERIVED, not dropped.** The first cut omitted it on the reasoning that nothing reads it; `webApp/src/components/project/SolutionView.tsx` reads it (AuthoredBadge + a per-rate anchor). It is computed with the same `deriveClassRates(pa, classes)` the option assembly uses, and asserted non-empty with every rate positive and currencied at the compute level.
- **The two phase seals lost their Temporal retry envelope.** `sealPhase` retries a Conflict `acknowledgeStaleMaxAttempts` times and returns Infrastructure; the retired workflows were retried by the SDK's own workflow-task retry for as long as the caller waited. The caller awaited the workflow either way, so this is a bounded loop replacing an unbounded one — the safer direction, but a change.
- **`.aiarch/state/project.json`'s `ReviewSubjectRef.kind` description is stale.** It says today's writers mint only `pullRequest` and that `commit` is for "a future subject-by-sha writer". As of `38fd7f9c` both rails mint `commit`. No behaviour depends on it; fixing it is a `project.json` edit plus codegen (the text is copied verbatim into `server/api/openapi.yaml`, `webApp/src/contracts/schema.ts` and `delivery_tools.gen.go`).
- **`startSystemDesign` names `"requirements"` as a LITERAL.** The three reserved design activity ids are the derived plan's, and `projectstate.ClassifyActivity` resolves the same three prefixes — but **no constant is shared between the plan derivation and this bootstrap**, so a rename in method-assets breaks onboarding silently. A test pinning the three reserved ids against the derivation closes it; Task 13 added the constant and its reason but not that test. **Wants the pin.**
- **The brand-new-project bootstrap still enters through `startSystemDesign`.** The op survives, re-pointed: it starts `DeliveryActivityWorkflow` for the reserved `requirements` activity, because the pump cannot do it (`nextEligibleActivity` selects from the COMMITTED activity list, and M0 is what writes slot 9). The ResearchInput precondition and the USE_EXISTING / ALLOW_DUPLICATE id policy are unchanged, and the skip-if-committed seed is what makes a restart-a-closed-run safe. **It is ANSWERED but UNTESTED against a real empty project** — nothing in the suite onboards one end to end.
- **Over-deleted symbols were restored at the END of `deliverymanager.go`.** The reachability sweep that enumerated the dead code is scoped to one directory, so it wrongly dropped the two EXPORTED symbols `cmd/server` calls (`RegisterManagerWorker`, `RegisterSchedules`), some grouped `const` blocks and three struct types whose FIELDS `unused` had flagged. All were restored from `HEAD` and the whole-module build and test run are what proved it — but a reader diffing `98e4a906` will see `constructState`, the signal-name consts and the gate vocabulary appear at the **bottom** of the file rather than in place. Cosmetic, and worth a tidy pass so the next reader does not infer meaning from the position.

---

## Coverage that was lost, and the list that wants a second pair of eyes

`manager_test.go` went **38,377 → 23,331** lines in `98e4a906` (23,464 at `bdf90047`). The great majority is the retired rails' own suite — roughly 190 `Test_CoAuthor*` / `Test_PD_*` / `Test_CoAuthorPhase2_*` / `Test_Phase*` / `Test_AssembleSDPReview*` / `Test_Construct_*` cases plus three replay harnesses — which had to go with the workflows. But the compiler-driven prune also dropped, for CASCADE reasons, tests of ops that **survive**, and **119 blocks were deliberately restored and retargeted** (the whole catalog family, the episode facet, the ask/acknowledge doors, the nine `Test_RowConflict_*`, the 32 `Test_Pump*`/`Test_PumpSweep_*`, the five `Test_GitForward_*`, `Test_AdvanceToConstruction_*`, `Test_SubmitProjectDesignDecision_M0Reject_*`).

**What was NOT restored, listed so it can be reviewed rather than trusted:**

| Not restored | The implementer's reason |
|---|---|
| `Test_assembleSdpReview_FourRows_Deterministic`, `…MissingPrerequisite_Errors` | the join is covered by `Test_ComputeProjectPlanSlots_ReproducesTheCommittedPlan`, which runs the same `assembleSdpReviewOver` |
| `Test_ReplyTo_RefusedOnDoorsThatCannotRouteIt`, `TestSubmitRoutesReplyToAnExistingThread` | they drove the retired `SubmitReviewDecision`'s per-kind door |
| `Test_DesignRoundID_*`, `Test_SubjectRef_*`, `Test_LedgerRoundBase_*` | their subjects (`designRoundID`, `designSubjectRef`, `ledgerRoundBase`) are deleted |
| `Test_ResolveQuestionBranch_ClosedWorkflowLeftoverBranch_SeedsOnMain` | its premise was a dead co-author run's leftover branch |

The implementer judged each one and says so plainly: **a judgement made 437 blocks into a mechanical prune is the kind that wants a second pair of eyes.** Check this list against the diff in the final whole-branch review.

Two test-shapes worth knowing about while reading the survivors: **the M0 end-to-end test seeds a committed activity list** (`activityLifecycle` reads `committedPlanInputs`, so a façade-level test of the `projectDesign` activity needs slot 9 committed — which a real project at M0 has by definition, since M0 is what commits the NEXT plan), and the store double gained `seedCommentOnRound` to file a comment the instant a round opens, standing in for the minutes a critic occupies in production — with no job reporting RUNNING the whole walk executes at workflow time zero, so no delayed callback can land inside `openRound`→autogate. Both are documented as the shapes they are; the first is the one place the suite asserts the resolver's precondition rather than driving it.

---

## Test-suite couplings introduced by this wave

- **`Test_LifecycleShapes` is coupled to the observe ladder AND to a drain window.** `full-inbox-does-not-wedge-the-router` depends on `stp`'s drains landing at t=15 / t=30 and its terminal at t=45, with the 65-override flood at t=25 **inside** that drain window; `fork-signal-reaches-the-named-task` depends on the override window; both fork cases depend on `runningPolls = 1` making the losing branch burn a real durable timer. A change to `observeInterval`'s table moves all of them. **They fail legibly** — each names what it expected — but the coupling is new.
- **The FIXTURE CAPTURE is coupled to the ladder too.** `deliveryReplayAwaitDone` needs **six minutes**, not the one `replayAwaitDone` allowed: a forked walk's losing branch waits out RUNNING polls on real 15-second durable timers, so `join-waits-for-all` alone spends ~90 s inside the ladder.
- **The capture rig's `temporal`-CLI dependency is stated only in a `t.Fatalf`.** That is deliberate — the message names the requirement where the person running it will read it — but it is not in any README or Makefile target, so a fresh checkout discovers it by failing.
- **The 20-event floor is a GATE, not a sanity check.** `Test_Replay_DeliveryHistories` FAILS a fixture under 20 events, because a thin history records a park rather than a shape and replays green against any walker. The eight fresh fixtures are 201–537 events.
- **`Test_Facade_RedraftReaches…` and `…OverrideAtATakeover…`** ride delayed callbacks at t=1 min and t=5 min for the same reason, and move with the same table.

---

## Platform (method-assets) — founder STOP

- **The eight Phase-2 draft command files** must retire in the same coordinated step as the eight slugs (see the 4b2 list).
- **A construction ANSWER command** does not exist (Q5). Either it is authored, or construction questions are declared human-answered.
- **GAP-4B-4's resolution is a method-assets release** if the ruling adds the three orphan kinds to a lifecycle (Q1).
- **`Test_RoundKindOfTask_AgreesWithWhatTheChildStamps` pins `len(methodassets.Lifecycles()) == 14`.** A release that DROPS a lifecycle now fails there instead of leaving an entire activity type's rounds unchecked. A release that ADDS one fails there too, deliberately — the pin is a conversation, not a bug.
- **`Test_Phase1RequiredKinds_AreExactlyTheTwoDesignLifecyclesOutput`** walks the lifecycle data and compares the produced kinds against `Phase1RequiredKinds()`. A release that moves either side fails there rather than leaving the Phase-1 seal waiting on a kind nothing drafts.
- **`Test_DispatchInputs_CommandFallbackAgreesWithTheLifecycleData`** and `Test_DesignCommands_MatchTheLifecycleData` pin `CommandFor` / `DesignCommandFor` against the lifecycle data's own `command` strings.
- **`Test_LastPhaseGateIsTheWalksFinalTask`** asserts, for all fourteen, that the gate task of the last phase IS the last task in declaration order and that nothing depends on it. A release that grows a task AFTER the final gate fails there instead of merging a branch mid-walk.

---

## Task 14 (the SPA) — carries

Server-side facts Task 14 consumes, recorded here because they are the server's half of the contract:

- **The M0 approve WORKS for the first time**, and it carries the `OptionID` — the screen's option choice now reaches the round. `SubmitReviewDecision` was widened for it.
- **`OverrideActivity` has two meanings keyed by liveness** — Steer (a live escalation) and Reopen (a terminal activity) — and the SPA must label them separately, because the server cannot tell which the operator meant.
- **Which notices flip, and which stay.** `NO_CONSTRUCTION_THREAD_OP` / `allowAsk` / `allowQuestions` can go for **resolve, reopen, ask, withdraw and re-dispatch**; the **stale-basis guard STAYS** for construction and its refusal is now SEMANTIC (a construction round names no design slot, so there is no basis flag to clear — clearing the Architecture's flag from an activity would un-stale that slot for every other activity too).
- **`webApp/src/hooks/phaseDecisionKey.ts` is a RENAME, not a deletion.** `phaseDecisionMutationKey` is `useSubmitReviewDecision`'s live `mutationKey`, `phaseDecisionFilters` is re-exported and `PhaseDecisionFailure` is the mutation's error type — the file is a live per-project query-key helper whose NAME is stale. `reviewDecisionKey.ts` is the honest name.
- **A `withdrawn` round wants a preview fixture.** The wire shape is a review task's revision in `QueryActivityView` (`outcome: "withdrawn"`), `roundOutcome` maps it and `OUTCOME_TEXT.withdrawn` already renders it — but no fixture carries one, so both halves are unit-tested only. Put it beside a LATER round, which is what a re-dispatch leaves behind.
- **`GatePanel.tsx` now reads `QueryProjectView{designHealth}`** (Task 14, `89a522ee`) rather than its own copy of the rules.
- **The two design `session` views are slot-DERIVED.** A project whose design activity is MID-WALK reports the SLOT's stage, not a live drafting stage: `StageDrafting` is no longer reachable through that door. Check the Design Experience's loading states against that.
- **Attempt `Detail` strings are render-ready:** a design attempt reads `drafted <kind> on <branch>`, a construction one `dispatched <command> for <phase>`, a failed one carries the venue's whole sentence including the run URL, and an ask's verdict row reads `asked N question(s) of <role>`.
- **The M0 defaulting sentence** was PLANNED to come from the attempt `Detail` (`defaultedDetail`); Task 14 could not, because `Detail` is on no view — see "What Task 14 actually shipped" below.
- **Both signals refuse AWAY from their gate**, so approve / send-back / re-dispatch belong to the live gate only; a dormant activity's refusal names itself.

### What Task 14 actually shipped, and the three gaps it could not close from the SPA

- **The M0 defaulting sentence does NOT read the attempt `Detail`, because `Detail` is on no view.** `DeliveryTaskRevisionView` carries `attemptIds` only — the whole OAS-generated schema was grepped — so putting `defaultedDetail` in front of the founder is a server + OAS + regen change, not an SPA one. What ships instead reads the committed planning-assumptions slot: absent ⇒ one sentence, committed carrying `defaultPlanningAssumptionsNote`'s `"Derived defaults: "` prefix ⇒ a different one. **The consequence is the honest part: a PER-FAMILY default (`resolvePlanningAssumptions` filling an `Unknown` revenue share or an empty rate card on an otherwise committed slot) leaves no trace on any view, so the notice is SILENT for exactly the case Task 9's review called the same lie as refusing.** The founder can still approve a cost computed on numbers nobody showed them. Fix: carry the attempt's `Detail`, or a `defaultedFamilies []string`, on `DeliveryTaskRevisionView`, then re-point the notice at it and delete the slot-reading proxy. Ruled acceptable for 4b1 only because it is a wire change arriving after the code freeze, and it is recorded here rather than left to be discovered at an M0.
- **"Escalated" is DERIVED in the client, not read.** `DeliveryActivityView` has no escalated member — a takeover and an approval gate both fold into `awaitingHuman` — so `overrideActionFor` reproduces the server's `escalatedTaskOf` rule client-side: a second copy of a server rule, the drift hazard this whole wave exists to remove. Terminal (`done|failed`) is exact and is checked first, so a Reopen is never mislabelled; only a Steer can be offered on an activity that is awaiting something else. Exit: read `ConstructionSessionView.stage === 'awaitingTakeover'`, or put the stage on the activity view.
- **`openThreadCount` was loosened.** An ANSWERED change request no longer blocks approve — that is `ReviewCommentBlocksApprove` applied to the field, and it is the server's rule — but a reviewer who read the old count as "threads I have not finished with" will see a different number for the same board.
- `reconcileStale: {kind:'dispatch'}` is wired on the construction arm and is **unreachable**: no construction gate judges a slot. It is honest rather than a refusal notice, and it costs nothing, but nobody should read its presence as coverage.
- **The override bar's SUBMIT is not preview-covered** — the preview transport answers no `deliveryOverrideActivity` — so the labels, the mutual absence of Steer/Reopen and the required-note disable are pinned, and the round trip is not.
- Surviving `"stage 4b"` strings in `webApp/` are prose naming the stage that landed a change (file headers, doc comments, fixture notes). No user-facing string and no `VerbTarget.reason` explains itself by a stage number; node tests assert that for both survivors.

---

## CLOSED in 4b1 — recorded so nobody re-opens them

- **The variance loop is NOT a parity gap.** It was one for two commits and it is closed (`618467bc`): `runTaskVariance` is `handleVariance` + `executeOverride` **per TASK** — 10 attempts, the intervention Engine, Retry/Takeover re-dispatching THIS TASK instead of a flat phase list from index 0, Escalate waiting on the task's own inbox, `Skip` terminal, the budget checked at the TOP of the loop, and the escalation timeout ending the walk **nil** so the pump's cascade survives. The retired rail's decision table is matched arm for arm.
- **The design rail's post-exit slot-commit window heals** (`179a43b2` + `bdf90047`), and the prediction that it "self-heals on a re-run" was FALSE until it was fixed: `commitDesignArtifacts` began with `if !walkRanAnyTask(ws) { return nil }`, so a re-opened activity whose tasks all passed ran no task and the repair pass skipped the very commits it existed to make. It now asks once which slots are still uncommitted and commits those.
- **The `statuses` channel has a production sender again** — the comment-status mirror. It was measured dead in Task 12 and put back to work in fix round 2 as the seam a held autogate is released through.
- **The M0 approve is answerable.** Before Task 13 the façade signalled retired per-kind workflows at eleven sites and `SubmitSDPDecision` signalled the retired SDP assembly, so M0's approve reached nothing.
- **The 4a construction approve could never have worked**, and now it can: `submitConstructionDecision` passed a TASK id into a validator admitting only `requirements|detailed_design|test_plan|construction|integration|merge`, so `approve` at `designReview` was a `ContractMisuse` before it left the Manager.
- **`Test_DeliveryGate_ReceiverDrainsPastTheInboxCapacity` proves what it claims.** It used to count the double's call log for 76 comment ids no round holds; it now counts the router's own delivery hook, which is strictly closer to the claim.
- **`SubmitPhaseDecision`, `validatePhaseDecision`, `precheckPhaseDecision`, `phaseDecisionSignal`, `constructActivityWorkflowID`** are gone, and the grep for them is literally zero in code AND comments.
