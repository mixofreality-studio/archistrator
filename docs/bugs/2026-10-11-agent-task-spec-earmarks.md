# Agent task spec wave: earmarks

These items were deferred during the agent-task-spec wave (plan `docs/superpowers/plans/2026-10-11-agent-task-spec.md`).
Each one names what triggers it and what closes it.

## E1: the local rail still runs a dispatch with no envelope ungated (ruling R41). RESOLVED 2026-10-12

**Resolution.** The founder permitted the data edit ("yes to E1", 2026-10-12).
- The 23 non-answer `SubmitAgenticJob` bindings now carry `activity_id`, `task_id`, `spec_hash` and `revision`. Each task is the lifecycle task the command drafts (`missionDraft`, `coreUseCasesDraft`, `architectureDraft`, `construction`; `codeReview` for the review-phase dispatches; `activityListDraft`/`networkDraft` for the scope-change redrafts). Each revision is the Manager's attempt id `<activity>:<task>:<n>`, with n = 2 for a redraft, a requeue or a re-dispatch after a failure or a sendBack. Each hash is a per-(project, activity, task) sha256. The 4 answer bindings are unchanged (R30).
- The agenticJobAccess scenarios are regenerated (testgen run in the workspace, R29). Their rig double now answers the gated sequence green over the stamped envelope and records nothing, so every lifecycle scenario runs gated. A stale-version double fails 14 of the 20 scenarios, which proves it.
- `taskSpecEnvelopeOf` no longer has the ungated branch or the WARN. A dispatch with no envelope is refused at Submit ("carries no task spec envelope"), as a partial one is. `TestLocalRailRefusesADispatchWithNoEnvelope` pins this on the construct, draft and critique arms. No test pinned the WARN itself; the two Manager-level R41 tests stay.

The original text follows.


**What.** `agenticjob.taskSpecEnvelopeOf` lets one kind of dispatch through ungated: a lifecycle dispatch that carries neither `task_id` nor `spec_hash`. Such a run gets no spec, no `--model`/`--tools`, no edit scope and no checks.

**How it shows.** Each such dispatch is logged at WARN: `localexec: a lifecycle dispatch carries no task spec envelope; it runs UNGATED`.

**Why it is still there.** The 23 non-answer `SubmitAgenticJob` bindings in the agenticJobAccess component test plan (`.aiarch/state/project.json`) predate the envelope, and the generated agenticJobAccess scenarios dispatch them as written. Both Managers always send the envelope, and two tests pin this:
- `TestEveryDesignDispatchCarriesItsTaskSpecEnvelope`;
- `TestTestingGatePassCitesTheJudgedRun`, through `assertTaskSpecEnvelopes`.

So production dispatches are always gated.

**To close.** The data edit needs the founder's permission; it was refused in B6.
1. Add the envelope to those 23 bindings:
   - `task_id`, `spec_hash` and `revision` on every binding;
   - `activity_id` as well on the design ones.
2. Run `make gen-tests`.
3. Delete the ungated branch in `taskSpecEnvelopeOf`, so that a dispatch with no envelope is refused like a malformed one.

## E2: gen-tests ran at the repo root (ruling R42). RESOLVED by R43

R42 had both venues run `make gen-tests` at the checkout root. That broke the integration task for any project whose Go module is not at the root. archistrator is one: its `moduleDir` is `server`, and it has no root Makefile.

Ruling R43 supersedes R42's M2 part: both venues run `make gen-tests` in the project's module dir (project.json `moduleDir`; "" means the repo root).
- **Local rail:** fixed in B6 fix round 2 (`projectModuleDir`, which refuses a dir that leaves the repo).
- **Construct template:** platform task A10, to `make -C <moduleDir> gen-tests`.

## E3: the check gate is not sandboxed (rulings R27, R39)

The gate's subprocesses run code the agent wrote, outside the agent's sandbox. They run with an allowlisted env, and their git calls are isolated. Repo-local `.git/config` filter drivers remain the one open residual.

**To close:** R27's separate gate job.

## E4: gate time stacks onto the Manager's observe ceiling (M6, parked). RESOLVED by R54

The time before the agent (task-spec and gen-tests, 15 min each) and after it (edit-scope 15, checks 45, record 15) adds to the agent's 25 minutes. A long check gate can pass the Manager's 1h observe ceiling. The cloud job has the same shape.

**Resolution (R54).** The Manager now observes a dispatched job for the templates' job bound (`timeout-minutes: 90`) plus 15 minutes: `gatedJobObserveCeiling` = 105m in `deliveryactivity.go`. `TestObserveCeilingCoversTheJobBound` pins it to the seated templates. The local rail's stacked bounds (25 + 45 + the 15-minute gate steps) fit inside it.

## E5: use-case and check-source links in the task spec panel (R57 M2, R60; D1 F3)

Spec §6 asks for component chips that link to the component, and for check sources that link to the use case or its diagram. Both still render as text or toggles:
- `TaskSpecPanel.tsx` MAY EDIT: component chips toggle their path list;
- `taskSpecPanelLogic.ts` `sourceLabel`: scenario rows read `<contract> · use case <id>` as plain text (0 `<a>` elements).

**To close:** link each to the existing design-experience routes (component, use case dynamic view).

## E6: a state-only design task lists the Edit and Write built-ins (R60; D1 F8)

A state-only design draft (glossaryDraft) shows Edit and Write in its tools beside "Writes through state tools only". The tools come from the step manifest; the edit-scope gate is what binds, so this is display only.

**To close:** either drop file-writing built-ins from state-only steps' manifests in method-assets, or have the panel say the gate admits only `.aiarch/state/**` for them.

## E7: the state-tool pin drift gate (R50)

Every cloud job resolves and hashes its task spec with the `aiarch-state-mcp` at `StateMcpModulePin`. A release that changes a command, charter, lifecycle, pricing or `TaskSpecFor` without moving the pin makes every cloud job refuse with "task spec drifted". The pin names a commit of THIS repo, so no test can check it before that commit exists.

**For now:** a D3 checklist line (move the pin to the pushed merge, re-seat, push). The local rail has its own guard since R56 (`aiarch-state-mcp version` against the server's own `projectstate.StateToolVersion`).

**To close:** have the cloud spec step run `aiarch-state-mcp version` and fail with the same "stale binary" message, so a skewed pin is named rather than reported as drift. The server would need to put its own version into the dispatch inputs.

## E8: design-task CheckRuns live only on the session branch (final review M4)

Approve-merge overlays only the slot onto main (`OverlaySlotFromBranchOntoMain`), and the F82 self-heal hard-resets the session branch to main. After a self-heal a design task's panel loses its recorded results: the view reads the branch and falls back to main only on NotFound.

**To close:** carry the task's CheckRuns across the overlay, or make the self-heal keep `testingState.checkRuns`.

## E9: the rule registry is a hand table, not generated and drift-gated (final review M6)

Spec §4.3 calls for a generated, drift-gated rule registry. It is `ruleSlotAttributionPrefixes`, read by `RulePrefixesFor`. Plan deviation 3 accepted per-family granularity but not the missing drift gate; the `TestAttributeRule_*` tests pin a few rules, not completeness.

**To close:** a test that every rule id methodcheck can emit attributes to exactly one slot family (or the whole-project check), so a new rule family cannot fall through silently.

## E10: the webClient scripted agent and the uitests test repo (R58 follow-ups)

R58 made the scripted agent honour its edit scope, and `execute-a-project-activity-P1` passes. Two things a longer webClient walk will hit:
- **`command:arch` is red on every construction in the uitests stack.** The test repo has no Go module, so `go test ./internal/` reports "no Go files in ./internal". The run retries until the scenario ends. P1 only needs the dispatch, so it passes; a scenario that needs construction to go green will not.
- **The test-plan task's recorded plan is refused.** For a `test_plan` task the agent records the design state's `phaseArtifacts.testPlan[<contract>]`, and the state tool's validation rejects it with TP-STATE findings, so that job exits 1 and retries.

**To close:** give the uitests stack a check runner double (as the Go venue hooks do with `testinfra.RunChecksDouble`), or seed the test repo with a module the commands can build; and record a test plan that passes the TP rules (or skip the test-plan task in the script).

## E11: the seated-workflows drift test renders with an empty AppSlug (R55)

`TestSeatedWorkflowsEqualTheTemplates` renders the templates with `AppSlug ""`. Cloud sync-on-dispatch seats with `archistrator-bot`. If sync ever commits its slugged render into this repo, the drift test goes red. Decide at D3 whether this repo's seated copy should carry the production slug. The construct `allowed_bots:` renders empty today, the same as on main and in every prior seat, so that is not a regression.

## E12: a stale workspace `aiarch-state-mcp` passes the version probe (final re-review m2)

Under the workspace `go.work`, `method-assets` reports `(devel)` on both the server and the state tool, and `(devel)` == `(devel)`. A stale workspace binary therefore passes the R56 probe. Only a `TaskSpecSchemaVersion` bump (hand-maintained) or a tagged build tells them apart, so a schema-only change surfaces as "task spec drifted". Workspace-only: a `GOWORK=off` release build carries the real tag.

**To close:** stamp the workspace build with the VCS revision of method-assets (`-buildvcs`), or have `make` rebuild the tool whenever method-assets or projectstate changes.

## E13: the 81 converted CheckRuns carry absolute host artifact paths (final review M3). RESOLVED 2026-10-12

**Resolution.** The founder permitted the data edit ("yes to M3", 2026-10-12). R57 says to rewrite an artifact to repo-relative when it lies under the repo, and to empty it otherwise.
- None of the 81 lies under this repo. Each lies in another checkout: 62 at `archistrator-dct/server/test-results`, 11 at `archistrator-dct/server`, and 8 at `archistrator-dct-wt5r2-arch/server/test-results`, a worktree that no longer exists.
- Their repo-relative forms would name the shared, gitignored `server/test-results` (whatever the last `go test` in the reading checkout left there, not this run's evidence) or the whole `server` module. So all 81 are emptied.
- The testRun view now treats an empty artifact as "no artifact". It skips the index read and renders the verdicts from the CheckRun with no logs. `ReadTestRunFile` answers NotFound ("kept no artifact"). The panel labels the run "no artifact".
- Before this change, the store's refusal of the empty ref, and of the absolute paths before that, failed the whole view.
- The testing gate already indexes a refused ref as nothing, and is unchanged (a change there would be replay-relevant).

The original text follows.


`.aiarch/state/project.json` holds 81 `"artifact": "/Users/..."` entries on the CheckRuns converted from TestRuns. They resolve only on the machine that wrote them. **The data fix awaits founder permission** (rewrite to repo-relative `.aiarch/test-results/<run>` or drop the entries).

## E14: the testing gate does not compare `CheckRun.SpecHash` with the dispatched hash (final re-review M1(b))

The CLI gates (spec, edit-scope, record) hold the job to the dispatched spec hash, but the Manager's testing gate accepts any recorded CheckRun for the task without comparing its `SpecHash` to the hash dispatched. A run recorded against a drifted spec can satisfy the gate. Never ruled in.

**To close:** carry the dispatched hash into the gate's ledger and require equality (a mismatch holds, as no-run does). Replay-breaking, so it rides a drain.

## D3 release checklist (R62; the order is binding)

Execute in this order, from this section. The plan file lives on main in another session's tree and is not edited here.

1. **Tag the platform**: `method-assets` v0.11.0, `framework-go-scenariohost` v0.4.0, `framework-go-app-generator` v0.13.0.
2. **Bump the server `go.mod` pins** to those tags.
3. **`make gen-tests`** (regenerates `uitests/generated`).
4. **`GOWORK=off` build, the make targets, and regress against the baseline.** Without the workspace the pins must resolve; expect no new reds.
5. **Message the session `archistrator-6b` before the merge** (coordinate every merge to main).
6. **Merge to main.**
7. **Push.**
8. **Move `StateMcpModulePin`** (`server/internal/resourceaccess/sourcecontrol/sourcecontrolaccess.go`) to the pushed main commit (R50, E7). Skipping this makes every cloud job refuse with "task spec drifted".
9. **Re-seat the workflows and `.claude`**: `cd server && ATS_RESEAT_WORKFLOWS=1 go test ./internal -run TestSeatedWorkflowsEqualTheTemplates`, then the `.claude` materialization (see E11 on the slug).
10. **Push.**
11. **Drain** every in-flight `*:activity:*` workflow and the construct and design runs they observe, and any GitHub Actions run still on the old template, **before deploying**. The replay-breaking changes (final-review §5, plus the R54 ceiling):
    - `dispatchSpecHash` schedules `DesignSessionReadProjectOnBranch` (plus a `readProject` fallback) before every design and construction submit: a new command ahead of `PipelineSubmitAgenticJob`.
    - The testing gate calls `ArtifactGetTestRunIndex` once before the contract loop instead of once per contract with a run: the command count changes.
    - The persisted evidence kind changes from `testRun` to `checkRun`, and `gateLedger.testRunID` becomes `checkRunID`.
    - Histories recorded `ProjectEnvelope.TestingState.testRuns`; they decode to empty `checkRuns`, so replay takes the hold branch.
    - The observe ladder ceiling moved (R54, R64: 105m to 150m, 32 to 41 polls), which changes the timer schedule.
    - Not a replay break, but order-sensitive: the new dispatch inputs (`task_id`, `spec_hash`, `revision`) are rejected with a 422 by an old seated workflow. The managed-scaffold sync re-seats before dispatch.
12. **Deploy.**
