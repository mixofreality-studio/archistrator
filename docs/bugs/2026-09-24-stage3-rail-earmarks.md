# Stage 3 (one staging/review rail) — earmarks, carry-forwards, and the drain note (2026-09-24)

Stage 3 shipped: the `activityExecutionAccess` facet (12 verbs, additive), the `.activityExecution` row with per-activity `Version`, the construction workflow writing attempts + review rounds behind the `execution-ledger-writes` fence (2 new replay fixtures, 13 old ones byte-identical), the design rails dual-writing rounds behind `design-round-ledger`, `QueryActivityView` reading persisted rounds, `ActivityView` carrying verdicts/thread/subject/round, and the migration of this repo's state (26 rows, zero derived deltas). Spec §5.3/§8 amended where planning proved them wrong.

## DRAIN NOTE — before this stage is deployed (do not deploy from a wave)
1. Pause every project (`SetProjectRunState`/pause).
2. Drain `{projectId}:nextActivity` pumps and every `constructActivity` child; drain every design session (`{projectId}:systemDesign`, `{projectId}:<ordinal>` co-authors, `sdpReview`).
3. The `execution-ledger-writes` and `design-round-ledger` fences make in-flight replays safe, but the registered activity set grew by 36 names (12 construction + 24 design) — a worker built from an older commit cannot serve a workflow started on this one.
4. Run `cmd/migrate-activity-execution` on any OTHER project state that predates this commit (this repo's state is migrated). `construction-state-reset` is safe only AFTER migration.
5. Release; unpause.

## Stage-4 ENTRY CRITERIA (blocking)
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
