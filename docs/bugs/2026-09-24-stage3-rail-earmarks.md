# Stage 3 (one staging/review rail) — earmarks, carry-forwards, and the drain note (2026-09-24)

Stage 3 shipped: the `activityExecutionAccess` facet (12 verbs, additive), the `.activityExecution` row with per-activity `Version`, the construction workflow writing attempts + review rounds behind the `execution-ledger-writes` fence (2 new replay fixtures, 13 old ones byte-identical), the design rails dual-writing rounds behind `design-round-ledger`, `QueryActivityView` reading persisted rounds, `ActivityView` carrying verdicts/thread/subject/round, and the migration of this repo's state (26 rows, zero derived deltas). Spec §5.3/§8 amended where planning proved them wrong.

## DRAIN NOTE — before this stage is deployed (do not deploy from a wave)
1. Pause every project (`SetProjectRunState`/pause).
2. Drain `{projectId}:nextActivity` pumps and every `constructActivity` child; drain every design session (`{projectId}:systemDesign`, `{projectId}:<ordinal>` co-authors, `sdpReview`).
3. The `execution-ledger-writes` and `design-round-ledger` fences make in-flight replays safe, but the registered activity set grew by 36 names (12 construction + 24 design) — a worker built from an older commit cannot serve a workflow started on this one.
4. Run `cmd/migrate-activity-execution` on any OTHER project state that predates this commit (this repo's state is migrated). `construction-state-reset` is safe only AFTER migration.
5. Release; unpause.

**Between step 4 and step 5 an old reader pod renders an EMPTY WORLD.** The state now carries `.activityExecution` and the pre-stage-3 reader knows only `.activityConstruction`, so every activity reads as NotStarted until the new image is live. Harmless while every project is paused (nothing dispatches), and expected — do not treat it as a migration failure.

**Rollback: restore `project.json` FIRST, then roll the image.** The state move is ONE-WAY. The base (pre-stage-3) `projectstateaccess.go` has no read tolerance for `.activityExecution` — the tolerance added in stage 3 reads the LEGACY member, not the new one — and the stage-3 encoder never emits `activityConstruction` again. So the moment the migration runs (step 4), or the moment the FIRST write lands on the new image, an older image reads every activity as NotStarted. Roll the image back on that state and the pump re-dispatches the WHOLE project the instant it is unpaused: fresh attempts, fresh branches, fresh spend, against activities that are already Done. The order is therefore: pause → `git revert`/restore `project.json` to its pre-migration commit → roll the image → unpause. Rolling the image alone is not a rollback.

## Stage-4 ENTRY CRITERIA (blocking)
- ~~Design-rail round numbering is per-SESSION, so a second session of one kind re-mints the first's `RoundID`~~ — **LANDED IN STAGE 3** (`seedRoundBaseFromLedger` on both design rails, behind the existing `design-round-ledger` fence, reading the registered `ReadActivityExecution` verb). A session now numbers its rounds above everything the durable ledger holds for that kind's gate. This is the precondition stage 5 needs to make the round ledger the design READ path: until it landed, two sessions' reviews could not be told apart in the data.
- Arm the per-activity CAS: thread the activity `Version` through the 12 verbs' contract (return the activity version; take an expected activity version); needed the moment parallel children exist.
- `buildStatus` enum/vocabulary rule (from stage 1) before `delivery-manager` is authored as `planned`.
- Delete the three deprecated facets (`gitActivityStatusAccess`, `constructionTransitionAccess` Record*, `designSessionAccess`) AFTER the drain; the `DH-CONTRACT-DEADOP` Warning pinned in `designhealth/engine_test.go` flips back to `assertAbsent` then.
- `applyRecovering` treats terminality `Conflict`s (OpenActivity on an exited row; Decide/Append on a decided round) like version conflicts — burns retries then fails non-retryably; a duplicate pump dispatch of a finished activity now fails the child where the old rail wrote nothing. Needs a non-conflict error class.
- Stage-4 sweep closes stranded `pending` rounds (a crash between OpenReviewRound and Decide leaves round n pending; resume mints n+1; nothing duplicates, but the stranded round renders `running` forever).
- Design-rail join collision: once stage 4 records DESIGN attempts, two kinds' round 1 would bind one gate attempt — add the artifact kind as a FIELD on the round (today it lives only inside the 4-part id, which nothing may parse).
- `engineReviewPolicy` triplication + inline `NewReviewEngine()` collapse (from stage 2); the design slot→activity/phase table is now a THIRD copy of the mapping the two design Managers hold.
- `RoundWithdrawn` maps to wire `failed` — give it its own wire member.

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
