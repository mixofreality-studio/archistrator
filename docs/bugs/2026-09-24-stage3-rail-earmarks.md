# Stage 3 (one staging/review rail) — earmarks, carry-forwards, and the drain note (2026-09-24)

Stage 3 shipped: the `activityExecutionAccess` facet (12 verbs, additive), the `.activityExecution` row with per-activity `Version`, the construction workflow writing attempts + review rounds behind the `execution-ledger-writes` fence (2 new replay fixtures, 13 old ones byte-identical), the design rails dual-writing rounds behind `design-round-ledger`, `QueryActivityView` reading persisted rounds, `ActivityView` carrying verdicts/thread/subject/round, and the migration of this repo's state (26 rows, zero derived deltas). Spec §5.3/§8 amended where planning proved them wrong.

## DRAIN NOTE — before this stage is deployed (do not deploy from a wave)

**One sequence, run once, covering stages 3 + 4a + 4b.** Amended 2026-09-25 (stage 4a); the Schedule step is step 5.

1. Pause every project (`SetProjectRunState`/pause).
2. Drain `{projectId}:nextActivity` pumps and every `constructActivity` child; drain every design session (`{projectId}:systemDesign`, `{projectId}:<ordinal>` co-authors, `sdpReview`). **Sweep the `delivery:` prefix too** — the memorised `drain *:nextActivity:*` guidance predates the `delivery` namespace, and after 4b re-keys the pump, sweep whatever it re-keys it to. Grepping one prefix is how a live sweep survives a drain. Drain the in-flight `{p}:phaseAdvance` workflows as well (they last seconds; 4a splits the one id into `:systemDesign` / `:projectDesign`).
3. The `execution-ledger-writes` and `design-round-ledger` fences make in-flight replays safe, but the registered activity set grew by 36 names in stage 3 (12 construction + 24 design) and then SHRANK by 92 in 4a (231 → 139) — a worker built from an older commit cannot serve a workflow started on this one, and a worker built from THIS commit cannot serve a workflow an older one started against a name that no longer exists. Drain in both directions.
4. Run `cmd/migrate-activity-execution` on any OTHER project state that predates this commit (this repo's state is migrated). `construction-state-reset` is safe only AFTER migration.
5. **DELETE the two old sweep Schedules, by id, before the release** — `temporal schedule delete --schedule-id construction:pumpSweep`, then `temporal schedule delete --schedule-id construction:replanSweep`. 4a renamed the consts to `delivery:pumpSweep` / `delivery:replanSweep` (`574012f7`; `deliverymanager.go:10107`, `:10112`), so the two `construction:*` Schedules are abandoned with their action still on a task queue no worker polls, and **nothing will ever converge them**. A Schedule left pointing at `construction` is a **silent dead sweep, not an error**: every project simply stops advancing, and `temporal schedule describe` is the only thing that says so. Do not expect the re-register to fix it — `messagebus.RegisterSchedule` (`internal/utility/messagebus/messagebus.go:170-216`) does Create-then-Update-in-place on `ErrScheduleAlreadyRunning`, which ADOPTS a same-id Schedule rather than replacing it; that adoption is exactly why the id had to change.
6. Release; unpause. **Confirm with `temporal schedule list`** that exactly two sweep Schedules exist, and with `temporal schedule describe` that each one's action is on task queue **`delivery`**. (On a `CONSTRUCTION_DRYRUN=true` server no Schedule is registered at all — live-verified: `"messageBus.RegisterSchedule skipped — CONSTRUCTION_DRYRUN=true"` for both ids, followed by `"deliveryManager Temporal Schedules registered"` — so a local dry-run server proves nothing about steps 5 and 6 in either direction.)

Schedules are the one part of this cutover that is **not** one-way: a rollback re-registers under whatever ids the old image holds, which is why step 5 is cheap in both directions. The STATE rollback is one-way and unchanged — see below.

**Between step 4 and the release in step 6 an old reader pod renders an EMPTY WORLD.** The state now carries `.activityExecution` and the pre-stage-3 reader knows only `.activityConstruction`, so every activity reads as NotStarted until the new image is live. Harmless while every project is paused (nothing dispatches), and expected — do not treat it as a migration failure.

**Rollback: restore `project.json` FIRST, then roll the image.** The state move is ONE-WAY. The base (pre-stage-3) `projectstateaccess.go` has no read tolerance for `.activityExecution` — the tolerance added in stage 3 reads the LEGACY member, not the new one — and the stage-3 encoder never emits `activityConstruction` again. So the moment the migration runs (step 4), or the moment the FIRST write lands on the new image, an older image reads every activity as NotStarted. Roll the image back on that state and the pump re-dispatches the WHOLE project the instant it is unpaused: fresh attempts, fresh branches, fresh spend, against activities that are already Done. The order is therefore: pause → `git revert`/restore `project.json` to its pre-migration commit → roll the image → unpause. Rolling the image alone is not a rollback.

---

**AMENDED 2026-09-25 (stage 4a). The drain now covers stages 3 + 4a + 4b, and 4a MUST NOT DEPLOY ALONE.** 4b changes workflow TYPE names and workflow ids again, so a deploy between the two would need its own drain for nothing. Merge both, drain once, release once.

**What 4a adds to the drain, now folded into the six steps above.** Every row measured in the worktree at `b4815ec4`:

| Thing | Today | After 4a | Mechanism |
|---|---|---|---|
| TaskQueue `system-design` | systemdesign worker (`internal/manager/systemdesign/worker.gen.go:13` at `4baed01a`) | **gone** | drain-and-cutover |
| TaskQueue `project-design` | projectdesign worker | **gone** | drain-and-cutover |
| TaskQueue `construction` | construction worker | **gone** | drain-and-cutover |
| TaskQueue `delivery` | — | new: ONE worker, eleven workflow types, every name unchanged (`internal/manager/delivery/worker.gen.go:13`) | — |
| Schedule `construction:pumpSweep` (30 s) | fires into TaskQueue `construction` | **renamed `delivery:pumpSweep`** on TaskQueue `delivery` (`574012f7`); the old id must be DELETED — **drain step 5** | `RegisterSchedules` creates if absent and ADOPTS a same-id Schedule; `temporal schedule delete` is the only thing that retires an id |
| Schedule `construction:replanSweep` (300 s) | fires into TaskQueue `construction` | **renamed `delivery:replanSweep`** on TaskQueue `delivery` (`574012f7`); same | same |
| `{p}:phaseAdvance` | BOTH design Managers, one string | `{p}:phaseAdvance:systemDesign` / `:projectDesign` (`deliverymanager.go:5057`, `:7289`) | new histories only; drain the in-flight ones (they last seconds) |

**Every workflow TYPE name and every other workflow id is UNCHANGED in 4a** — that is deliberate, and it is why the fifteen construction replay fixtures still replay. The drain is required anyway, for one reason: **the registered activity-name set SHRANK by 92 names** (231 → 139, stated and golden-pinned at `internal/registered_names_test.go:68`). A worker built from an older commit can serve a workflow this commit started; a worker built from THIS commit cannot serve a workflow an older one started against a name that no longer exists. Drain before release, in both directions.

**THE SCHEDULE STEP IS DRAIN STEP 5** — one sequence, not a second list. It was written here as a separate four-step procedure while the rename was still in flight; the rename landed (`574012f7`), so the step is now unconditional and lives with the rest of the drain at the top of this file. Do not run two lists.

**The model↔code drift the rename was owed a ruling on is CLOSED**: the label at `project.json:6758` and the two consts both say `delivery:*`. What is NOT closed is that nothing in the gate set compares a Schedule id string to the model — the only guard is that `Test_RegisterSchedules_RegistersPumpSweepAndReplanSweep` now asserts both ids as LITERALS as well as through the consts. Carried in `docs/bugs/2026-09-25-stage4a-earmarks.md`.

**Rollback for the STATE is unchanged and still one-way:** restore `project.json` FIRST, then roll the image.

## Stage-4 ENTRY CRITERIA (blocking)
- ~~Design-rail round numbering is per-SESSION, so a second session of one kind re-mints the first's `RoundID`~~ — **LANDED IN STAGE 3** (`seedRoundBaseFromLedger` on both design rails, behind the existing `design-round-ledger` fence, reading the registered `ReadActivityExecution` verb). A session now numbers its rounds above everything the durable ledger holds for that kind's gate. This is the precondition stage 5 needs to make the round ledger the design READ path: until it landed, two sessions' reviews could not be told apart in the data.
- ~~Arm the per-activity CAS: thread the activity `Version` through the 12 verbs' contract (return the activity version; take an expected activity version); needed the moment parallel children exist.~~ — **LANDED IN STAGE 4a** (Task 3, `d06b6745` + `225966a5`): armed across ten row-writing paths including the four retired-facet verbs, the three workflow fakes honour the expectation, and `NoActivityVersionExpectation` is the named sentinel for a caller that has no number. The recovery path for a row-level `Conflict` is NOT armed — see `applyRecovering` below, still 4b's.
- ~~`buildStatus` enum/vocabulary rule (from stage 1) before `delivery-manager` is authored as `planned`.~~ — **LANDED IN STAGE 4a** (Task 2, `0ab023ec` + `f8f6fd1f`): `DH-BUILDSTATUS-VOCAB` is an Error in the live tier, and `TestBuildStatusVocabulariesAgree` (`internal/arch_test.go:215`) pins estimation's `knownBuildStatus` equal to designhealth's inline vocabulary through the AST, because the import boundary forbids a shared leaf. **And the criterion's own text was wrong:** `delivery-manager` is not authored as `planned` — it carries no `buildStatus` at all (`.aiarch/state/project.json` slot 5), because `ALIGN-STALE-PLANNED` is an Error for a `planned` component whose package exists, and 4a lands `internal/manager/delivery` in the same commit as the component. `planned` was never an option here.
- Delete the three deprecated facets (`gitActivityStatusAccess`, `constructionTransitionAccess` Record*, `designSessionAccess`) AFTER the drain — **still open, still 4b, still post-drain.** **CORRECTION:** `DH-CONTRACT-DEADOP` does NOT flip back to `assertAbsent` then. Measured: deleting `constructionTransitionAccess` clears the `RecordOperatorNote` duplicate but NOT the `AcknowledgeStaleBasis` one, because `projectStateAccess` publishes that verb too and spec §5.3 keeps `projectStateAccess` at 9 ops. The pin stays until one of those two verbs goes. (Both findings also now read "with DIFFERING param signatures — a name collision, not a proven dead op", which is the honest reading.)
- `applyRecovering` treats terminality `Conflict`s (OpenActivity on an exited row; Decide/Append on a decided round) like version conflicts — burns retries then fails non-retryably; a duplicate pump dispatch of a finished activity now fails the child where the old rail wrote nothing. Needs a non-conflict error class.
- Stage-4 sweep closes stranded `pending` rounds (a crash between OpenReviewRound and Decide leaves round n pending; resume mints n+1; nothing duplicates, but the stranded round renders `running` forever).
- Design-rail join collision: once stage 4 records DESIGN attempts, two kinds' round 1 would bind one gate attempt — add the artifact kind as a FIELD on the round (today it lives only inside the 4-part id, which nothing may parse).
- `engineReviewPolicy` triplication + inline `NewReviewEngine()` collapse (from stage 2) — **PARTLY DISCHARGED IN STAGE 4a**: the three byte-identical copies collapsed to one because the package merge put them in one namespace and the compiler forced it (Task 6 Step 2b, class C). The `EffectiveGate` / `RequiresHuman` / floor-keyword move INTO the review Engine is still 4b's, and so is the design slot→activity/phase table's third copy.
- `RoundWithdrawn` maps to wire `failed` — give it its own wire member.

**Everything else on this list stays open and is carried, with its 4a measurements and the order 4b must take it in, in `docs/bugs/2026-09-25-stage4a-earmarks.md`.**

## Deferred to stage 6
- Delete `ArtifactSlot.ReviewThread` (dual-written now), `CritiqueVerdict`/`CritiqueNotes` (→ ordinary `productManager` verdicts), `ArtifactSlot.Revisions` (add the drift test = derived round count first), the `LegacyActivityConstructionRow` read tolerance, the stored derived fields; narrow `PendingOperatorNotes` (the writer stopped in stage 3; the reader still honours stored `NoteSendBack` because narrowing it breaks a pre-fence history).
- `ReconcileBranchFromMain` overlays only the slot table, so a design branch's `.activityExecution` is stale (harmless: the 3-way merge resolves main-side); fix the doc invariant or the overlay.
- Each design gate entry now writes up to five commits to main's `project.json` (openActivity, openReviewRound, critic verdict, human verdict, decide) — repo-growth earmark.

## Carried from the whole-branch review (not fixed in stage 3)
- `OpenActivity` re-opening an existing row with the SAME pin but a different `typ`/`variant` overwrites both silently (`activityExecutionAccess.OpenActivity`, `projectstateaccess.go`) — write-once is enforced on the pin alone. Stage 4.
- The PR rail's rounds 1 and 2 cite the SAME `SubjectRef` (the pull request is per-activity, not per-round), so the ledger cannot say which revision each round judged — `gateSubjectRef` in `construction/constructactivity.go`. Naming the commit is the fix.
- Gate-attempt `Actor` semantics CHANGED in stage 3: a gate attempt is now `human`/`system` (who decided) where it used to be `agent` (who was dispatched). The stage-5 UI must label gate rows from that vocabulary, not the work rows'.
- No test covers "a NEW send-back after a completed redraft carries only the new round's comments" (`construction/constructactivity.go`, the send-back carry) — the shape exists, the regression net does not.
- The design-rail fake `AppendReviewVerdict` (and `ReadActivityExecution`) are more permissive than the store: the fake appends to any round it holds and answers an unopened activity with a zero row where the store raises `NotFound`. A workflow bug the store would refuse can pass the unit tests (`systemdesign`/`projectdesign`/`construction` `manager_test.go` fakes).
- `applyRoundReviewBatch`'s content-dedup keys on (author, anchor, text), so two genuinely distinct byte-identical comments in one round are mis-paired as a re-issue and the second is dropped. Narrow, but it is a silent drop.
- The migration's acceptance test (`TestMigrate_DerivedPhasesEqualThePreMigrationDerivation`) is SELF-REFERENTIAL and confirmed vacuous on this repo: the base rows stored no `phases`, so the before/after comparison compares two empty derivations. Derive a real pre-migration subset before the tool is trusted on another project's state.
- `designRounds` mints NO round for a critique-only slot (`CritiqueVerdict` set, `ReviewThread` empty): the skip guard admits such a slot but `slotRounds` groups by thread rounds, so the §5.3 projectManager-verdict conversion is not performed for it. Documented on the function; none exist on this repo's state.
- A migrated round's thread renders `answered` where the slot stored `addressed` (codec alias, carried by both copies of the comment). A dual-read window, closed when stage 6 deletes `ArtifactSlot.ReviewThread`.
- Wire-field casing: `GetProject.activityExecution` is camelCase among PascalCase siblings (`ActivityGit`, `ServiceContracts`, `GitRows`). Normalise in stage 6 with the other wire cleanups — it is a wire break either way.

## Owed waiver (slot 3, carried from stage 1, still owed)
- The §2h sentence naming the three Managers' TRANSITIONAL `Project Delivery Workflow` facet group and its stage-4 expiry; a Glossary entry for `Design Conformance Rules`.

## Platform (method-assets) — founder STOP
- `.claude/` skills naming `constructionTransitionAccess` / `gitActivityStatusAccess` / `designSessionAccess` / `systemDesignManager` (`grep -rln` across `.claude/skills .claude/commands .claude/agents`); `the-method-review-routing` drift vs the stage-2 engine; the nine Phase-2 draft kinds have no review task in the `projectDesign` lifecycle (correct per R7 — the rail dies in stage 4).

## Post-merge on main only
- `cd uitests && npm run regen:core-use-cases-fixture` (reads branch `main`).
- Browser run of `uitests/tests/preview/preview-shell.spec.ts` (`construction · owed-gate`, `service-pane`, `built-surface-link`, `unclassified-row`) — the stage-3 fixtures were re-derived through the real read path (zero deltas) but not driven in a browser; the previews render a 29-row world vs the server's 32 (three planned-no-record design rows hand-trimmed).

## Model / code hygiene
- Backfilled rounds carry no `openedAt`/`decidedAt`/`Reviewers` (nothing in the record knows them — not laundered); `fixture-schema.mjs`'s "a round-backed revision always has a startedAt" rule must not be pointed at migrated rows.
- `webApp/src/components/comments/CommentContext.tsx` still mints `$.activityConstruction[id=…]` anchors deliberately (stored comment anchors keep the old string); prose in `wire.ts`/`construction-tracker.spec.ts` still says `activityConstruction`.
- The `fixture-schema` `GATE_TASK` table is a hand mirror of the lifecycle gates — generate it from `lifecycles.gen.ts` in stage 5.
- Three orphan `contract.*.schema.json` files in `projectstate` are produced by no generator; `cmd/backfill-attempts`' two gosec suppressions could take `filepath.Clean` like the migration cmd; `atomicBusinessVerbs` is at 18/20.
- Six `uitests/preview-fixtures/**` snapshots still embed the 19-volatility world; `designhealthengine.go:22` and `systemdesignmanager.go:132` name a retired volatility.
- The MCP output schema of `activityExecutionReadActivityExecution` is shapeless (`x-go-type` only) after the `$defs.ActivityExecution` deletion; the codec's strict-decode gap (an idempotent value mangle passes the writer guard) belongs in the codec.
