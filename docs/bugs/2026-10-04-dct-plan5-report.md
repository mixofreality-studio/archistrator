# DCT Plan 5 — report (run stopped by the founder)

On 2026-10-11 the founder stopped the run during round 4's integrate step: "lets skip the remaining tests, and get in a good spot to commit / merge to main". Workflow `wf_1a8d9ddd-01e` was stopped at branch head `f2cfa4b8`. The rulings log is `2026-10-04-dct-plan5-rulings.md`.

## Gates at the merge

`make gen-*-check`, `method-check` (TP-BOUND 0, TP-OP-REACHED 0 as an Error), `encapsulation-check`, `sumtype-check`, `fix-check`, `vet`, `lint`, `test-short`, and the webApp `check` and `build` were all run before the merge. The results are recorded in the merge commit.

## Components green and merged (25)

Each component below had a generated `TestScenario_*` suite (or Playwright, or MCP suite) green when its unit was merged. The count is the number of scenarios.

| Layer | Component (scenarios) |
|---|---|
| Manager | deliveryManager 37, operationsManager 16, billingManager 11 |
| Engine | estimation 18, billing 16, operationEstimation 12, intervention 12, review 6, autoscaler 4, designHealth 4 |
| ResourceAccess | projectState 88, agenticJob 20, sourceControl 15, operatedSystemState 14, billingState 12, artifact 11, episode 8, operatedRuntime 8, merchantGateway 7, usage 7 |
| Utility | messageBus 3 |
| Client | mcpClient 37, mcpClientOperations 15, mcpClientBilling 4, webClientProjects 2 |

## Not green

| Component | State | Open items |
|---|---|---|
| webClient (Activity Experience) | Merged at 25216ca9: 20/21 green in its unit run. The integrated full run (stopped) had 15 pass and 5 fail. | **execute P7** is unproducible: the method-assets codeReview task carries no command, so agent reviewers never run (needs a method-assets lifecycle and delivery fix). **requeue P1** hung for 6.3 h with no per-test timeout. **replan P1/P2 and send-back P1** passed in the unit run but failed in the integrated run; the suspected cause is the local state-write contention below. |
| webClientPlan | **Not merged.** Branch `dct5r4/client-webClientPlan` @ b6b7f3a8 is 21/24; the verifier rejected it. | **commit P2:** the revoke re-opens the walk about 2.1 s after the click, past the spec's 5 s window. **execute P7:** as above. **resume-paused P2:** `refuseWhilePauseInFlight` only refuses while the supervision run is RUNNING, and runPauseBranch records and relays in one pass. |
| webClientBilling | **Not merged.** Branch `dct5r4/client-webClientBilling` @ 3cfc86d2 is 1/5. | registercustomer.go:108-110 refuses when slot 8 planning assumptions are absent. No actor path produces a CHARGED close of the previous month. |
| webClientOperations | **Not merged.** Branch `dct5r4/client-webClientOperations` @ 319dbdc5 is 4/8. | The hooks never start or design dct-ops-operate-p1, dct-ops-withdraw-p2 or dct-ops-health-p2. |

There are no generated Playwright specs on main for webClientPlan, webClientBilling or webClientOperations until their branches land.

## Earmarks

1. **Per-test Playwright timeout.** The generated `playwright.config.gen.ts` (app-generator testgen) lets one stuck scenario block the serial suite for hours. Fix it in the platform and release. `uitests.yml` has no `timeout-minutes`, so it falls back to the 6 h GitHub default.
2. **uitests CI is red on main** until the webClient reds above are fixed.
3. **Local state-write contention** (deferred by the founder 2026-10-10; see the rulings log). Fix direction: batch-birth the activityExecution rows in one commit, add a per-project write queue in projectStateAccess, and let the RA re-apply unconditional writes.
4. **TestRuns missing** for billingManager, mcpClient, mcpClientOperations, mcpClientBilling, webClient and webClientProjects. Their unit runs were green, but the integrate step was stopped before it recorded them.
5. **`make derived-plan-check` is stale** (it runs no tests). Fix it or delete it.
6. **Before deploy:** drain or terminate the Temporal workflows of the activity types removed in this wave (N-STP, N-IT). The registered-names golden now includes `deliveryAnswerJob`.
7. **The webClient test split** by screen facet (D3, 165501aa, made to meet the 12-op contract cap) is not by use case. The founder questioned it 2026-10-10; it is unchanged pending a ruling.
