# Stage 5 (webApp Activity Experience) — earmarks and carry-forwards (2026-09-24)

Stage 5 shipped §7 of `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md`: the Activity Experience, the Plan screen, §7.5's four component gaps, and the teardown of the construction console, both design rails and the home phase cards. Branch `activity-experience-stage5`, 25 commits from `10d32f23` (base `main` @`5e09b0b6`). The spec's §7.1–§7.4, §8 and §9 carry the amendments; what could NOT ship here is below.

Every claim in this file was grepped or measured in the worktree at the commit that records it. Where a count differs from a task report's estimate, the measured number is the one written down and the method is named.

---

## Deploy note — DO NOT DEPLOY THIS STAGE ALONE

This stage is SPA-only: it adds no server contract and changes no handler, so it needs **no drain of its own**. But it must **not ship ahead of stages 3/4's server**. The screens read `ActivityView.thread`, `.verdicts` and `.subjectRef`, and only a stage-3 server fills them. Against a pre-stage-3 server every review body renders an honest-empty history — which is true, and useless.

## Post-merge step (main only)

- `cd uitests && npm run regen:core-use-cases-fixture`, then commit. The regen reads the committed **main** branch (`server/cmd/gen-uitests-fixtures/main.go:96` → `gitRepoLocator{branch: "main"}`), so it cannot run in a worktree. Carried from the stage-1 earmarks and still owed; see also "orphans" below — `uitests/testdata/coreUseCasesProject.json` now has no reader in `tests/` at all.

---

## Deferred to stage 4 (server) — the four the webApp had to work around

| # | What | Why it could not ship | Where the workaround is |
|---|---|---|---|
| **R1 / GAP-5** | **The artifact-as-of-revision read.** `QueryActivityView` should take a revision and return the artifact as of that revision's `stagedRef`. | `ConstructionReviewSubjectRef.ref` names a git object that **no HTTP or MCP op exposes**. There is no way to ask for it. | A non-latest revision shows the CURRENT artifact under a caption saying so (`UI_IDENTIFIERS.Activity.HISTORY_CAPTION`). Everything else about history is real: the banner, Back to latest, that revision's thread with resolutions expanded, its verdicts and roster, no submit bar. |
| **R2 / GAP-6** | **The construction comment lifecycle** — Resolve / Reopen / Ask on a construction review. | `constructionManager` has no `SetReviewCommentStatus` and no `AskQuestions`. Only `systemDesignManager` and `projectDesignManager` do. | The rail is **read-unified and write-asymmetric**: one review body for every activity type, but a construction review can submit only a verdict. Closes on stage 4's `deliveryManager.SubmitReviewDecision`. |
| **R3 / GAP-7** | **The batched plan read** (`QueryProjectView`). | Drawing 32 mini lifecycles would mean 32 `QueryActivityView` calls. | `components/activity/miniLifecycleFromRow.ts` DERIVES each thumbnail from the project read the plan already has. Correct today; it re-derives what the server knows. |
| **R4 / GAP-4** | **`LayerForActivity` widened**, so the build-order row is a server fact. | It answers `("", "projectWide")` for a componentless activity, which is not a build-order row. | `components/activity/planRowFor.ts` derives the row client-side from the activity id + the committed architecture. Optional — the derivation is small and tested — but it is a second place that knows the build order. |

## The stage-4 hook re-key list

Stage 4 collapses the three Managers into `deliveryManager` (12 ops). Every mutation the two new containers make goes through a hook that names its manager in the op id, and **`src/containers/activityVerbs.ts` is the ONE table that changes** — it is the seam. The call sites, by hook and current op:

| Hook (module) | Current op | Called from |
|---|---|---|
| `useSubmitPhaseDecision` (`useConstructionMutations`) | `constructionSubmitPhaseDecision` | `ActivityExperienceContainer` |
| `useOverrideActivity` (`useConstructionMutations`) | `constructionOverrideActivity` | `ActivityExperienceContainer` |
| `useBeginConstruction` / `useBeginConstructionPending` (`useConstructionMutations`) | `constructionExecuteNextActivity` | `PlanContainer` |
| `useResumeConstruction` (`useConstructionMutations`) | `constructionResumeProject` | `PlanContainer:471` |
| `useSubmitReviewDecision` (`useDesignMutations`) | `systemDesignSubmitReviewDecision` | `ActivityExperienceContainer` |
| `useSetReviewCommentStatus` (`useDesignMutations`) | `systemDesignSetReviewCommentStatus` | `ActivityExperienceContainer` |
| `useAskQuestions` (`useDesignMutations`) | `systemDesignAskQuestions` | `ActivityExperienceContainer` |
| `useAcknowledgeStaleBasis` (`useDesignMutations`) | `systemDesignAcknowledgeStaleBasis` | `ActivityExperienceContainer` |
| `useRequestArtifactDraft` (`useDesignMutations`) | `systemDesignRequestArtifactDraft` | `ActivityExperienceContainer` |
| `useSubmitSDPDecision` (`useProjectDesignMutations`) | `projectDesignSubmitSdpDecision` | `ActivityExperienceContainer` (M0) |
| `useAdvanceToConstruction` (`useProjectDesignMutations`) | `projectDesignAdvanceToConstruction` | `ActivityExperienceContainer` (M0) |
| `useSetProjectReviewCommentStatus` (`useProjectDesignMutations`) | `projectDesignSetReviewCommentStatus` | `ActivityExperienceContainer` (M0) |
| `useAcknowledgeProjectStaleBasis` (`useProjectDesignMutations`) | `projectDesignAcknowledgeStaleBasis` | `ActivityExperienceContainer` (M0) |

The one hook in that module with **no site at all** is `usePauseConstruction` — see "No pause control exists" below.

---

## R17 — the seven classifications with no artifact renderer

`components/activity/taskArtifactFor.ts` returns `{ kind: 'unavailable', reason }` for each, with a copy line of its own (`components/activity/activityCopy.ts`), and each is pinned by a unit test. They are not bugs: they are seven renderers nobody has written.

| Classification | What the panel says |
|---|---|
| `deployment` | `artifactUnavailable('deployment')` |
| `documentation` | `artifactUnavailable('documentation')` |
| `integration` | `artifactUnavailable('integration')` |
| `testing:harness` | `artifactUnavailable('testing:harness')` |
| `testing:perf` | `artifactUnavailable('testing:perf')` |
| `testing:qaProcess` | `artifactUnavailable('testing:qaProcess')` |
| `service` on any phase but `detailed_design` | `artifactNotOfThisPhase('service')` — the contract is the only view a service activity has, and it belongs to one phase |

## GAP-3 — CLOSED, measured

**No leftovers.** Walking `ConstructionActivityView` and its eleven `Construction*` children through the OAS-generated `fixtures.schema.json` gives **75 members across 12 types**, and **every one of them is carried by at least one of the ten `activity-experience` fixtures**. Re-measure with the same walk after any contract change; the fixtures are the only thing keeping it true.

---

## Contract / wire defects found, not fixed here

1. **`GitRows: null` on the wire vs a non-nullable OAS.** The live server marshals a nil Go map as JSON `null`; the OAS types `GitRows` as a plain object, so a verbatim live capture fails the generated schema. Go's zero value for a map IS the empty map — only the encoder loses that. This is the ONE normalization the plan/M0 captures make (`{}`), and it is commented in both capture scripts. **Fix: either mark `GitRows` nullable in the emitter, or have the handler serve `{}`.** A fixture should not have to correct the wire.
2. **`TimelineEvent.raw` is `type: [null]` in the OAS** (`webApp/src/contracts/schema.ts:1101, 1872, 2418 — `raw?: null`). A preview fixture therefore **cannot carry a turn's content**: the generating scene and the timeline can be shown, but not what an agent actually said. Give `raw` a JSON type and the dispatch body becomes fully fixturable.
3. **The skipped-GATE divergence.** A gate attempt with `OutcomeSkipped` makes the task read PASSED and its phase read NOT COMPLETE, from the same ledger:
   - `server/internal/manager/construction/constructionmanager.go:3490-3501` — `reviewEvidenceState`'s switch has no `revSkipped` arm, so a skipped gate falls through to `return taskPassed // 7`;
   - `server/internal/resourceaccess/projectstate/projectstateaccess.go:7584` — `phaseCompleteFromAttempts` returns `complete = (latest.Outcome == OutcomePassed)`, i.e. **false, decided**.

   App A's binary exit criterion and the contract's "`completed` iff the gate passed" are pulled apart by exactly one attempt outcome. No fixture pins it (`failed.json` deliberately puts its `skipped` revision on a DISPATCH task to avoid asserting a view whose two derivations disagree). **This needs a ruling, not a patch:** either a skipped gate completes its phase, or it does not pass its task.

---

## Stage 6 — deletions this stage could not make

1. **The MCP widget cluster.** `containers/McpSystemDesignContainer.tsx` (500 L) is out of scope for stage 5, and it is the ONLY reason three things are still alive:
   - `components/design/SystemDesignView.tsx` (827 L, including its step ladder and `ApproveFaultBanner`) — imported at `:55`, mounted at `:390`;
   - `components/design/SlimSpine.tsx` (164 L) — reached through `SystemDesignView.tsx:64`;
   - `contracts/methodMetadata.ts`'s `PHASE1_ORDER` — imported at `:38`.

   `PHASE1_ORDER` has two more readers that must move with it: `routes/HomeBase.tsx:44` and `uitests/tests/support/testids.ts:26` (which re-exports it as `PHASE1_ARTIFACTS`). Delete the container and all four go.
2. **The design rails' `<projectId>:<kind>` pending-comment keys.** `SystemDesignContainer` is gone; only the MCP path still writes one. The activity screen uses a per-TASK key instead. Retire the old shape with the container.
3. **`LensSelection` is NOT narrowed.** Narrowing it costs **30 `tsc` errors** across five KEPT modules (`components/construction/detail/detailPaneState.ts`'s `.attempt`/`.lifecyclePhase`, `tasks/taskBriefing.ts`, `tasks/decisionFlow.ts`). A red typecheck after a cut means the cut was wrong, so it was not made. Do it in the stage-6 pass that retires `detailPaneState.ts`.
4. **`useLensSelection.ts` residue.** Reduced 492 → 219 lines; what survives is the `LensSelection`/`LensId` types and `validateLensSearch`, read by `TasksLens.tsx`, `decisionFlow.ts`, `toolbarForLens.ts`, `detailPaneState.ts` and `contracts/routePaths.ts`'s documentation. It goes with item 3.
5. **Two dormant rules that now say so at their site.** `components/construction/tasks/decisionFlow.ts`'s gate-occurrence retirement rule (nothing computes an `epoch` since `useGateOccurrences` went) and `hooks/readRequestTimes.subscribeToShownReads` / `readListeners` (no subscriber). **Whoever re-mounts decision submission must re-supply an occurrence source** — the rule is kept, not deleted, precisely so that it is found.

## Orphans — kept, with no caller

Verified by grep at `dbc6c2ba`:

- **`containers/EpisodesPanelContainer.tsx` (117 L)** — no importer at all. Its only mention in the tree is a doc comment at `components/design/SystemDesignView.tsx:208`.
- **`components/episodes/EpisodesPanel.tsx` (518 L)** — imported only by that orphan container.
- **`components/episodes/EpisodeTimeline.tsx` is NOT orphaned.** `components/activity/DispatchBody.tsx:43` imports it and mounts it at `:171`. It stays.
- **`uitests/testdata/coreUseCasesProject.json`** — its only reader was `stubCommittedCoreUseCases`, which went with the retired specs. The file and its `regen:` / `check:core-use-cases-fixture` scripts are server-generated and were deliberately left untouched. A generated fixture nothing reads will rot: stage 6 should either wire it into an activity-route spec or retire the pair.

Mount the two episodes components on the activity screen, or retire them. They are the episodes EXPORT (JSON/CSV) surface, which nothing else offers.

## `UI_IDENTIFIERS` — 36 unused ids, measured

Task 13 estimated "~75" and did not prune (the brief did not authorise it). The measured number at `dbc6c2ba`, by scanning every `UI_IDENTIFIERS.<Group>.<Key>` reference across `webApp/src` and `uitests`, is **36 unused of 504 declared**:

`HomeBase.{RESUME_DESIGN, OPEN_SYSTEM_DESIGN, OPEN_PROJECT_DESIGN}` · `DesignWizard.{SCREEN, artifactStep}` · `DesignExperience.RECONCILE` · `ProjectDesign.{SDP_ASSEMBLE, ADVANCE_CONSTRUCTION, ADVANCE_RESULT, ADVANCE_STALE_ERROR, ADVANCE_ANYWAY}` · `Construction.{ROOT, LENS_LAYER, LENS_CONTENT, listAttempts, DETAIL_RESIZE_HANDLE, DETAIL_ATTEMPT_SELECT, DETAIL_BODY_ABSENT, DETAIL_BODY_EPISODES, DETAIL_PROVENANCE_BASIS, DETAIL_EPISODE_CAPTION, DETAIL_SUBAGENT_GANTT, DETAIL_VERDICT_STAMP, CONTRACT_UNRESOLVED}` · `Operations.{APP_SELECTOR, appOption}` · `ChangeRequests.subprojectCard` · `Subproject.CLOSE` · `Gate.{STAGE_CHIP, REQUEST_DRAFT_BUTTON, DRAFT_DISPLAY, FINDINGS_LIST, APPROVE_BUTTON, REJECT_BUTTON, WITHDRAW_BUTTON, FEEDBACK_INPUT}`

**Plus a rename, not a deletion:** `components/design/ExperienceChrome.tsx` still stamps `DesignExperience.ROOT` (`:154`), `DesignExperience.CLOSE` (`:188`) and `DesignExperience.DESIGN_SCROLL` (`:319`) on the **plan and activity** screens, which are not design screens. The group name is now a lie about which surface an id belongs to. Rename it when the design rails' remaining ids go (stage 6) — and note that `uitests` asserts `TESTID.designExperience` on the activity screen today, so the rename is a two-repo-directory change.

---

## Coverage this stage removed, and what it would take back

Task 13 retired **38 uitests specs** with the screens that drove them. One replacement shipped in Task 14 — `uitests/tests/preview/activity-renderers.spec.ts`, five cases proving the five kept Phase-1 renderers still MOUNT — but nothing black-box drives what is INSIDE them any more.

**The recipe that brings all of it back is one helper:** `stubActivityView(page, projectId, activityId, view)` beside `stubCreatedProject` in `uitests/tests/support/`, answering `constructionQueryActivityView` (copy the shapes from `uitests/preview-fixtures/web-client/activity-experience/{requirements-backfilled,architecture-round}.json`), plus a `goto` at `/project/$id/activity/{requirements|architecture}?task=…`.

| Lost coverage | Subject (KEPT code) |
|---|---|
| `glossary.spec.ts` | `GlossaryView` — chips, roll-up, search live region, per-row comment anchors |
| `volatility-map.spec.ts` | `VolatilityMap` — axes overview, listbox lanes, keyboard roving, rejected-candidates disclosure |
| `deployment-lens.spec.ts` | `DeploymentFlow` + the live health overlay's two arms |
| `artifact-systemtest.spec.ts` | `TestPlanView` / `SystemTestRunView`, the classification→renderer seam |
| `architecture-views.spec.ts` | `ArchitectureView`'s dynamic/perspective switches |
| `episodes-panel.spec.ts` | `EpisodesPanel` (also orphaned — above) |
| `gate-sendback-fault.spec.ts` | F-QA2-47: a 503 on Send back retains the staged note, re-enables, shows a cause-neutral banner |
| `status-decides-outcome.spec.ts` case 3 | `ops.call`'s empty-body-5xx mapping, through a real submit |
| `preview-shell` `service-pane` case 2 | a 4xx submit reads "Rejected" — **no preview fixture answers a submit with an error** |
| `preview-shell` `built-surface-link` | the `navigation-blocked` incident kind — **no surviving fixture renders an `<a target="_blank">`**. Capture a `U-SPA-web-client` (frontend) activity fixture and it comes back. |

Two guard rails lost their vehicle and are named separately because they are GUARDS, not features: **`navigation-blocked`** and **the 4xx-submit path**. Both need one new fixture each, listed above.

---

## Smaller things, each verified

1. **`SdpReviewView`'s option rows enrol no comment anchor.** `components/project/SdpReviewView.tsx:195-199` ARMS `sdpOptionAnchor(o.solutionKind)` via `setAnchor`, but the card never calls `useRegisterAnchor` — only `CommentableList.tsx` and `ContractSignatureList.tsx` do, in the whole app. So an SDP option comment can **never** be placed beside its option; it falls to UNPLACED in the margin, on the live M0 gate as well as in history. That is a real UX gap in the one surface whose whole job is choosing between those options. It is documented by the control half of `uitests/tests/preview/activity-experience.spec.ts`'s gap-1 case (`:371-372`: "an SDP option row enrols NO anchor, so a comment on one has nothing to sit beside and lands in the unanchored group").
2. **Every margin card in the shipped fixture set was UNPLACED** until Task 12 re-pointed the fixtures' anchors at real ones. Worth a look at whether any **committed** construction comment in the real project carries a pointer no anchor resolves — the fixtures were hand-written, but if the rails ever wrote an anchor in a different shape, the margin has been silently dumping those threads into UNPLACED in production too.
3. **The `comment-armed-anchor` probe span is always rendered on an ENABLED surface**, with empty attributes when nothing is armed (`components/comments/CommentContext.tsx:301-309`); it is suppressed entirely when `enabled === false`. A test must therefore assert it **EMPTY, not ABSENT**, on a live surface — asserting absence there passes for the wrong reason.
4. **The M0 stale-basis chip is LIVE on the real state.** The committed `sdpReview` slot carries `staleBasis: true` (from `activityList` rev 3), so the first thing a founder sees on the M0 gate is "basis changed — reconcile". That is TRUE, and it is the case the spec names ("re-opening Architecture invalidates projectDesign"). But the M0's reconcile exit navigates to the Architecture activity rather than **recomputing the plan** — and recompute is stage 4's deterministic Project Design (spec §6). Until stage 4 lands, the chip points at a door, not at the fix.
5. **No PAUSE control exists anywhere** (R11) — and it never did, on the console either. `useResumeConstruction` IS mounted (`src/containers/PlanContainer.tsx:471`), so a project paused by MCP, by another tab or by the server can be resumed from the Plan screen; but `usePauseConstruction` (op `constructionPauseProject`, which takes a `reason`) has **no caller in the app at all**. The asymmetry is easy to misread as a regression from the console: it is not. If the founder wants pause it is a small piece of new UX (reason capture + a confirm), not a port.
6. **`rememberPlanLens(lens)` is called in `PlanContainer`'s RENDER BODY** (`src/containers/PlanContainer.tsx:173`), not in an effect. Deliberate and commented at the site: it is an idempotent write to module memory with no state and no subscription, and the ✕ that reads it may be pressed before an effect for that render would have flushed. Noted so a future React-strictness sweep does not "fix" it into a bug.
7. **`laneLabel` is never set.** `UI_IDENTIFIERS.ActivityLifecycle.laneLabel` is declared and placed by `LifecycleGraph`, but nothing supplies a label: the 14 lifecycles contain only two forks, and at their width `lifecycleGraphGeometry` would drop the text. Spec §7.1's "lane labels at forks" is amended accordingly.
8. **`LifecycleRevision.detail` is unpopulated** — the wire carries no source for it. Kept in the type because the adapter is total; dropped from the UI.
9. **`plan/tasks.json` is no longer a pure capture.** One slot stage was moved by hand (`sdpReview` → `awaitingReview`) so the TASKS lens has a design review to owe. Its `note` says so. **If the plan fixtures are ever regenerated, that edit must be re-applied** or the TASKS case goes back to "Nothing needs you".
10. **`project-design-m0-history.json` is synthetic.** Its project read is real; its second round is hand-written, because no live project has an M0 with two rounds. The shapes mirror `architecture-round.json`'s persisted round exactly and both the schema and the derivation clauses in `fixture-schema.test.mjs` pass over it.
11. **A fixture cannot carry `staleCause`** — the OAS slot schema rejects it as an additional property. The app DERIVES the string from `staleBasisCause` (`contracts/wire.ts:333-347`), which the OAS does carry untyped. The named-cause copy works; it just cannot be set directly.
12. **`HomeBase` has no preview fixture.** Its one-card change (Open plan) is covered by typecheck and lint only; nothing in the preview build or the Playwright suite opens `?screen=home`.

## Preview bundle size

`webApp/dist-preview` is **13 MB**, of which the JS bundle is **10.6 MB** — 20 fixture states (16 MB on disk) are bundled at build time by `import.meta.glob`, and eight of them carry their own ~1 MB copy of the same project capture. Nothing gates bundle size and the build is still ~3 s, so this is not urgent. **A shared-fixture mechanism** — an `ops.$ref` into one common capture, or a per-screen default op table — would take roughly half of it back and is worth doing before the fixture set grows again.

---

## Process note earned here

**`golangci-lint`'s cache is shared across identical-content copies of the module and reports the OTHER copy's paths.** `GOWORK=off make lint` in this worktree reported 33 issues against files under a scratch directory that **no longer exists** (a sibling agent's merge-verify copy of the same tree). A scoped run of the same packages reported 0. `golangci-lint cache clean` followed by the full run: **0 issues**. Before believing a lint failure whose paths are not in your tree, clean the cache.
