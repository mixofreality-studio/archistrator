# Agent task spec: test baseline (2026-10-11)

Taken before any agent-task-spec change. App repo: `main` @ e1c1e90461ca461d41a005f92c28ecb4a3f01f89 (worktree `archistrator-ats`, branch `agent-task-spec`, identical tree). Platform repo: @ f5e9c03a3657bb5f821305a060bc867c2e26ced6 (branch `agent-task-spec`, clean). Server built with the workspace (`go.work`), as development will use. Regression rule: every test green here must stay green; `comm -13 baseline new` on the same jq filter must print nothing.

Raw baseline files live in the session scratchpad (`baseline/*.txt`, `*.json`); everything red is reproduced verbatim below.

## Stack

The server suite needs no Docker: every scenario package boots its own in-memory Temporal SQLite dev server (`temporal` on PATH). Pre-existing containers `dct-r4int-pg` and `archistrator-dev-pg` were left running and untouched. No suite failed at host boot, so no "boot-failed (stack down)" entries exist.

## Platform modules (GOWORK=off, go test ./... -count=1)

| module | failing tests | failing packages |
|---|---|---|
| method-assets | none | none |
| framework-go | none | none |
| framework-go-scenariohost | none | none |
| framework-go-app-generator | none | none |

All four are fully green.

## Server: go test -p 1 -timeout 90m ./... -count=1

Run ONCE (one -json stream; test-level and package-level lists derived from it, 32 packages ok). The first run happened before `make claude-assets` (the suite's `make test` depends on it; `.claude/commands` is gitignored and absent in a fresh worktree). That caused one worktree-artifact failure, `TestPromptSurfaceToolReferencesExistInRegistry` ("could not locate the repo root ... .claude/commands"), which is NOT baseline-red: after `make claude-assets` the package `cmd/aiarch-state-mcp` passes. The two packages that had failures were then re-run (second run) to separate stable reds from flakes.

### Failing tests, verbatim (stable red, red in both runs)

```
github.com/mixofreality-studio/archistrator/server/internal/manager/delivery TestScenario_ask_a_clarifying_question_during_review_P4
github.com/mixofreality-studio/archistrator/server/internal/manager/delivery TestScenario_ask_a_clarifying_question_during_review_P4/ask-a-clarifying-question-during-review-P4
```

Failure: `manager_scenarios.gen_test.go:520: ask-a-clarifying-question-during-review-P4 probe 1.2 step 1: result {"kind":"session","session":{"reviewThread":[{"type":"question","status":"open","addressee":"architect"}]}} is not a subset of ...`

### Failing packages, verbatim

```
github.com/mixofreality-studio/archistrator/server/internal/manager/delivery
```

(Package fails only because of the test above plus the flake below in the rerun.)

### Flaky (red in exactly one run; treat as non-baseline, not a regression if it recurs)

```
github.com/mixofreality-studio/archistrator/server/internal/manager/delivery TestScenario_commit_to_a_project_option_P2
github.com/mixofreality-studio/archistrator/server/internal/manager/delivery TestScenario_commit_to_a_project_option_P2/commit-to-a-project-option-P2
```

Rerun-only failure: `manager_scenarios.gen_test.go:652: commit-to-a-project-option-P2 step 3: unexpected error: manager: the project changed concurrently while the revoke was applied ... github.CommitSubtree: concurrent ref advance (non-fast-forward): git ref compare-and-swap lost`. Passed in the full run. In the first full run the P4 scenario also logged a separate hook failure (`deployment-integration run on R-github ... record-test-run: exit status 1`) in one of its repeated runs.

## Server: make lint

`make lint`: 0 issues, exit 0.

## Server: gen drift gates

No aggregate `gen-check` target exists. Every `gen-*-check` target run individually:

| target | exit |
|---|---|
| gen-client-check | 0 |
| gen-config-check | 0 |
| gen-fakes-check | 0 |
| gen-internal-tools-check | 0 |
| gen-lifecycles-check | 0 |
| gen-main-check | 0 |
| gen-models-check | 0 |
| gen-temporal-check | 0 |
| gen-tests-check | 2 (RED) |

`gen-tests-check` is red on main too (reproduced in the main tree: same 43 `stale:` lines). Verbatim stale list (all under `../uitests/generated/`):

```
stale: ../uitests/generated/webClientBilling/onboard-a-new-customer-P1.spec.gen.ts
stale: ../uitests/generated/webClientBilling/onboard-a-new-customer-P2.spec.gen.ts
stale: ../uitests/generated/webClientBilling/onboard-a-new-customer-P3.spec.gen.ts
stale: ../uitests/generated/webClientBilling/playwright.config.gen.ts
stale: ../uitests/generated/webClientBilling/reporter.gen.ts
stale: ../uitests/generated/webClientBilling/view-a-billing-cycle-P1.spec.gen.ts
stale: ../uitests/generated/webClientBilling/view-a-billing-cycle-P2.spec.gen.ts
stale: ../uitests/generated/webClientOperations/operate-a-delivered-system-P1.spec.gen.ts
stale: ../uitests/generated/webClientOperations/playwright.config.gen.ts
stale: ../uitests/generated/webClientOperations/register-a-delivered-system-for-operation-P1.spec.gen.ts
stale: ../uitests/generated/webClientOperations/reporter.gen.ts
stale: ../uitests/generated/webClientOperations/view-live-deployment-health-P1.spec.gen.ts
stale: ../uitests/generated/webClientOperations/view-live-deployment-health-P2.spec.gen.ts
stale: ../uitests/generated/webClientOperations/view-operating-cost-projection-P1.spec.gen.ts
stale: ../uitests/generated/webClientOperations/view-operating-cost-projection-P2.spec.gen.ts
stale: ../uitests/generated/webClientOperations/withdraw-a-delivered-system-P1.spec.gen.ts
stale: ../uitests/generated/webClientOperations/withdraw-a-delivered-system-P2.spec.gen.ts
stale: ../uitests/generated/webClientPlan/commit-to-a-project-option-P1.spec.gen.ts
stale: ../uitests/generated/webClientPlan/commit-to-a-project-option-P2.spec.gen.ts
stale: ../uitests/generated/webClientPlan/commit-to-a-project-option-P3.spec.gen.ts
stale: ../uitests/generated/webClientPlan/download-generated-source-code-P1.spec.gen.ts
stale: ../uitests/generated/webClientPlan/download-generated-source-code-P2.spec.gen.ts
stale: ../uitests/generated/webClientPlan/execute-a-project-activity-P1.spec.gen.ts
stale: ../uitests/generated/webClientPlan/execute-a-project-activity-P2.spec.gen.ts
stale: ../uitests/generated/webClientPlan/execute-a-project-activity-P3.spec.gen.ts
stale: ../uitests/generated/webClientPlan/execute-a-project-activity-P4.spec.gen.ts
stale: ../uitests/generated/webClientPlan/execute-a-project-activity-P5.spec.gen.ts
stale: ../uitests/generated/webClientPlan/execute-a-project-activity-P6.spec.gen.ts
stale: ../uitests/generated/webClientPlan/execute-a-project-activity-P7.spec.gen.ts
stale: ../uitests/generated/webClientPlan/pause-construction-P1.spec.gen.ts
stale: ../uitests/generated/webClientPlan/playwright.config.gen.ts
stale: ../uitests/generated/webClientPlan/replan-under-scope-change-P1.spec.gen.ts
stale: ../uitests/generated/webClientPlan/replan-under-scope-change-P2.spec.gen.ts
stale: ../uitests/generated/webClientPlan/replan-under-scope-change-P3.spec.gen.ts
stale: ../uitests/generated/webClientPlan/reporter.gen.ts
stale: ../uitests/generated/webClientPlan/resume-paused-construction-P1.spec.gen.ts
stale: ../uitests/generated/webClientPlan/resume-paused-construction-P2.spec.gen.ts
stale: ../uitests/generated/webClientPlan/track-weekly-project-progress-P1.spec.gen.ts
stale: ../uitests/generated/webClientPlan/track-weekly-project-progress-P2.spec.gen.ts
stale: ../uitests/generated/webClientPlan/track-weekly-project-progress-P3.spec.gen.ts
stale: ../uitests/generated/webClientPlan/track-weekly-project-progress-P4.spec.gen.ts
stale: ../uitests/generated/webClientPlan/view-the-project-state-log-P1.spec.gen.ts
stale: ../uitests/generated/webClientPlan/view-the-project-state-log-P2.spec.gen.ts
```

Also printed: `testgen: security has no goPackage; no tests generated`.

## webApp

| command | exit |
|---|---|
| npm run typecheck | 0 |
| npm run lint | 0 |
| npm test | 0 (31 pass, 0 fail) |
