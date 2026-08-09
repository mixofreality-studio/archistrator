# Finish Construction — design

**Date:** 2026-08-09 · **Approver:** system-architect (all rulings recorded below) · **Founder ask:** every activity in `project.json` implemented and marked complete; the project reads as *operating* when loaded in the deployed app at https://archistrator.capture-gtd.com/; the record looks genuinely built with archistrator; all tests and lints pass. Billing/Stripe may be non-functional at runtime, but a setup guide must document what is needed to turn it on.

## Current state (verified 2026-08-09)

- 62/68 `activityConstruction` rows are Done+Integrated (the precedented hand-seeded reconciliation shape). Stragglers: **C-BM** (NotStarted), **C-BS** (phantom Running — no Temporal workflow exists behind it), **C-BG** and **C-WIA** (Failed, sticky — no reopen seam exists), **N-DEP** and **R-BG** (Done but buildStatus InReview).
- **Hard blocker:** committed System component `episode-access` has no coding activity in slot 9, so `ACT-COMPONENT-COVERAGE` (SeverityError, `server/cmd/aiarch-state-mcp/crossartifact.go`) rejects **every** construction rail write (`recordPhaseArtifact` / `recordServiceContract` / `recordTestingState`) project-wide. The rule matches normalized name/title and ignores `componentId`. The episode-access *code and contract already exist* (`server/internal/resourceaccess/episode`, `serviceContracts["episodeAccess"]`).
- **Data corruption:** server-side projectstate writes round-trip `project.json` through the generated contract type, which does not model `deployment.{infrastructure,bindings,settings}`; commit `87e4fe9` silently deleted all three (5/15/18 → 0/0/0). This is why `make gen-temporal-check` fails (`appgen: envnames: infra "postgres": not declared in deployment` — four CI steps share that one appgen run). Last-good content: `f6d2a75`. Working-tree `server/cmd/server/config.gen.go` is fallout from a half-run appgen — discard, do not commit.
- **"Operating" does not exist in the product.** The phase enum ends at construction; the construction console shows "Awaiting the construction pump" / "Resume construction" forever, even at 100 % integrated; the HomeBase construction card can structurally never show DONE.
- **Deployment:** the deployed server reads project state from GitHub `mixofreality-studio/archistrator` branch `main` (push = state deploy). Local main is 31 commits ahead of origin, unpushed. Code changes ride `release.yml` → ghcr images → hand-bump of `image.tag` in `../software/k8s/argocd/applications/archistrator-server.yaml` (Argo auto-syncs).
- Slot-9 carries **zero** `componentId` fields (the authored-componentId dispatch fix landed without the backfill).
- The C-BG Requirements SRS for billingStateAccess was fully authored, rejected 5× on the coverage blocker, and is preserved verbatim for re-recording.

## Approach — five phases, two hard sequencing gates

### Phase A — repair the substrate *(hard gate: no server-side rail write before this is green)*

1. Restore `slots["6"].model.deployment.{infrastructure,bindings,settings}` from `f6d2a75` into `project.json` (hand-commit, plain reconciliation commit message).
2. Close the round-trip drop at its root: extend the projectStateAccess contract `$defs` so `DeploymentOperationsModel` models the three sections; regen (`modelgen`); add a **round-trip test** — encode/decode through the generated contract type and assert infrastructure/bindings/settings survive.
3. Discard the stale working-tree `config.gen.go`; re-run the appgen family; bring all server + webApp gates green locally.

Rationale (architect): one `publishDraft` through the un-fixed codec re-deletes the sections; the fix must precede the first draft session.

### Phase B — slot-9 + slot-10 amendment through the legal rail

One amendment wave — two sequential draft artifacts (activityList, then network) if the rail requires one kind per draft session; slots 11–16 acked once, at the end of the wave. Per artifact: `projectDesignRequestArtifactDraft` → `putDraftModel` (full model) → `publishDraft` → commit. Content:

- **Add C-EA** — name `C-EA`, title "Build Episode Access", `componentId: episode-access`, coding, worker class + effort/risk quanta mirroring sibling RA coding activities. (The title must normalize to contain `episodeaccess` — the coverage rule ignores `componentId`.)
- **Backfill `componentId`** onto every coding activity in the same amendment (the stale-basis cascade fires once per wave; converts coverage from fragile title-matching to authored truth).
- **Keep C-WIA / C-HE / R-WIT** (architect ruling: descoped work is recorded as complete-with-zero-scope plus a descope note, not erased; deletion is history rewriting, forces network surgery, and orphans `activityConstruction` keys; the ACT-UNKNOWN-COMPONENT Warnings are acceptable and honest).
- **Amend slot-10 in the same wave**: add a minimal dependency row for C-EA (dependsOn mirroring sibling RA activities, off the critical path) — slot 10 goes stale in this wave regardless, so re-committing it costs one draft instead of one ack.
- **AcknowledgeStaleBasis on slots 11–16** (12 precedented acks exist). No Phase-2 re-run — disproportionate for a bookkeeping reconciliation of completed work.

### Phase C — finish the activity record *(rail writes before lifecycle hand-commits)*

Rail-first, then hand-commit only what the rail structurally cannot reach:

1. **Rail writes (post-amendment):** re-record the preserved C-BG SRS verbatim (`recordPhaseArtifact` mapKey `billingStateAccess`, then `publishDraft`); enumerate code-layer components (37-component roster) against `serviceContracts` (29 keys, facet doctrine counts) and record genuinely missing contracts through the rail. Its two known drift items (`bindPaymentInstrument` vs `BindGatewayLive`; revenueLedgerAccess facet homing) stay **open questions on the SRS**, deferred to the settlement re-plan — slot-5 is not touched (its stale cascade would cover slots 6–16).
2. **Billing stubs:** implement buildable code honoring the frozen contracts for the billing scope (billingManager, billing engine seam, billingstate/merchantgateway remain contract-first stubs) — compiles, tests green, runtime returns explicit not-configured errors. This makes "built, integration deferred pending Stripe keys" a *true* statement.
3. **Lifecycle hand-commits** (precedented reconciliation shape; plain commit messages; no synthetic `applied_mutations` entries):
   - C-BG, C-WIA: Failed → Done+Integrated; C-WIA's note records the 2026-07-20 work-item descope; C-BG's note references the re-recorded SRS and the settlement re-plan.
   - C-BS: phantom Running → Done+Integrated (note: contract-only stub per the billing→settlement deferral).
   - C-BM: NotStarted → Done+Integrated (references the stub build).
   - C-EA: Done+Integrated (code + contract demonstrably exist).
   - R-BG: Done+Integrated with the Completed-exit shape; note states vendor provisioning is deferred to the settlement re-plan, cross-referencing the Stripe setup guide. (Architect: InReview-forever is itself a lie; the honesty lives in the note.)
   - N-DEP: **perform the review first** (verify the deployment against the running system — software-tester/architect), record the verdict, then Integrated is earned.
4. **Recompute `constructionProgress` from the rows** (single script), so 100 % earned value is internally consistent with the activity record — no hand-typed figures.
5. **Earmark** the reopen/un-fail seam as deferred product work, homed with the settlement re-plan.

Prohibition (architect): **no mass-backfill of SRS/design phaseArtifacts** for the other components — invented after-the-fact artifacts are forgeries; the 62 reconciliation rows are a documented mid-stream reconciliation and should look like what they are.

### Phase D — derived "Operating" presentation state (no enum change)

Architect ruling: the phase enum models the design-process axis, which genuinely ends at construction; the operated life is a different volatility axis (operations domain). A Phase ordinal 3 would couple the axes — rejected on decomposition grounds. Instead:

- **Predicate:** `phase == construction && every activityConstruction row is Done+Integrated`. One shared derivation — a single function in the webApp contracts adapter; if the catalog projection lacks the activity rows needed to compute it on the tiles, extend the server catalog projection with one derived boolean computed by one Go function (single authority per side, no stored field).
- **Renders:** catalog chip + HomeBase label **"Operating"**; construction console replaces the misleading awaiting-pump panel with a completion panel — copy: **"Construction complete — all 69 activities integrated."** plus the final earned-value figure; HomeBase construction card gets a DONE treatment; "Resume construction" disappears in that state.
- **Milestone SPA bug fixed now** (architect Q7): `computeActivityStatuses` in `webApp/src/contracts/constructionAdapters.ts` mirrors server `resolveDependencySatisfied` (recursive milestone satisfaction with cycle guard) + a milestone-in-dependsOn test fixture.
- **Operations console stays out of scope** (architect Q5): registering the dogfood app / hand-inserting an operated_system row to show RUNNING would fabricate a runtime record — refused. No operations link from the completion panel (an empty console is worse than no link). Earmark: F4 bridge (`DeployAfterConstruction` → `RegisterOperatedApp`) as the follow-up. EconomicsStrip's "operated net: —" stands (currently honest).

### Phase E — verify, document, ship

1. All gates green: server (`gofmt`, `go vet`, `fix-check`, build, golangci-lint v2.12, `test-short`, `method-check`, every `gen-*-check`), webApp (`npm run check`: gen:api drift, `tsc -b`, eslint, prettier, node tests), systemtests (constitution + `gen-check`), method-assets drift.
2. **Record `testingState` through the rail after the gates actually run green** (real, not asserted).
3. **Stripe setup guide** at `docs/billing-setup.md`: every key/secret/envvar the billing scope needs (Stripe Connect keys, webhook secrets, the not-configured envvars the stubs read), where they land (helm values / k8s secrets in the `software` repo), and the settlement re-plan pointer.
4. Push `main` (this alone updates the deployed app's state); release images; hand-bump the Argo `image.tag` in `../software`; push that repo.
5. Browser-verify https://archistrator.capture-gtd.com/ shows the project Operating with all 69 activities integrated.

## Error handling / risks

- Every hand-edit of `project.json` is followed by `./aiarch-state-mcp validate --root .` (expect zero Errors after Phase B) and a full local gate run before commit.
- The two poisoned idempotency keys (C-WIA/C-BG failure records) only matter for replays of those exact runs — new runs mint fresh keys; no action.
- If the deployed server writes to project.json between our push and verification (30s reconcile/pump ticks), the version counter moves — re-read before any further mutation; our writes all go through fresh-read → edit → commit.
- Rollback: state commits are plain git commits on main; revert restores prior state; the deployed app reads whatever main says.

## Test plan

- New: codec round-trip test (Phase A), milestone-in-dependsOn adapter test (Phase D), operating-derivation tests with **shared fixtures exercised by both the Go catalog derivation and the TS adapter derivation** (all-integrated, one-failed, one-in-review, empty, and a Skipped-shaped row) — architect condition to prevent cross-side drift, billing stub unit tests.
- Existing: full server/webApp/systemtests suites per Phase E.

## Explicitly out of scope (earmarked)

- Reopen/un-fail seam (settlement re-plan).
- Operations console wiring / F4 deploy-after-construction → RegisterOperatedApp bridge.
- slot-5 verb-name drift (`bindPaymentInstrument` → `BindGatewayLive`) and revenueLedgerAccess facet homing (settlement re-plan).
- Dispatch-command misrouting / `RecordActivityStarted cs.Type` / ACT-rule componentId unification (open-items doc) — not needed for this wave since no real pump run occurs; they remain tracked in `docs/bugs/2026-08-09-construction-open-items.md`.
