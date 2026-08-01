# Call-Chain Realization — Post-QA Rollout Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take the founder-accepted PoC (branch `callchain-realization`, app @ 25064a9, platform @ 7a1d64d7) to merged, hard-gated production state: all 16 views realized, model refinements (`decidedBy`, alternative groups, required actors), doctrine shipped to method-assets, severity flipped to Error, platform released and pinned, branches merged.

**Architecture:** Model increments land first (schema → regen → both validation tiers → UI) so the amendment pass over all 16 views authors against the final model once. Amendments follow (activity diagrams, then realizations in three review-sized batches, then collateral slots). The severity flip and release choreography close, with founder gates at ratification and push points.

**Tech Stack:** unchanged from the PoC plan (Go server + platform methodcheck twin-tier, TS webApp, git-as-DB project.json).

## Global Constraints

- Branches: `callchain-realization` in BOTH repos; still unpushed until Task 15. Founder executes all pushes/tags/merges.
- Founder rulings now binding: (R-A) every `clientAction` use case must declare ≥1 actor — new rule `CUC-ACTOR-REQUIRED`; (R-B) `decidedBy` + alternative groups are IN this rollout; (R-C) chat rail untouched.
- Severity staging: ALL new rules ship at the existing `ccGateSeverity`/`ccLiveSeverity` constants (still Warning) until Task 12 flips both to Error.
- Section grammar END-STATE (Task 3 aligns the platform): both tiers key-first — step-scoped `"dynamicView "+<view KEY>+" step "+<nodeId>`, view-scoped `"dynamicView "+<KEY>`, use-case `"useCase "+<ucID>`. The webApp already joins on this.
- Wire names camelCase: `decidedBy`, `alt`. Tolerant decode: all new fields optional; old data must keep decoding.
- Amendment mechanics (established in the PoC): hand-edit `.aiarch/state/project.json` + gate loop from `server/`: `make gen-models` (no-op), `make method-check`, `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System`, `GOWORK=off make test-short`, then `cd webApp && npm run check`. Keys/titles/useCaseIds verbatim (deep-link anchors). Commit convention `systemDesign: … (design amendment)` / `coreUseCases: …` as applicable.
- Repo idioms as in the PoC plan (exactOptionalPropertyTypes spreads, noUncheckedIndexedAccess, pure-leaf node --test, components purity, one-behavior-per-Test-func in methodcheck, table-driven in designhealth).
- The QA stack (server :8891, vite :5173, temporal :7235, clone archistrator-qa-clone) stays up through Task 14 for founder spot-checks; Task 15 tears it down.
- Every implementer reads the ledger note file for its task if the task names one; authoritative recon lives in: spec §6a (architect amendment scope), `.superpowers/sdd/2026-07-30-callchain-realization-poc/task-12-report.md` (uc1 authoring precedent), `architect-qa/` + `ux-review/` (UI assessments).

---

### Task 1: Spec amendment — model refinements + rulings

**Files:** Modify: `docs/superpowers/specs/2026-07-30-call-chain-realization-design.md`

**Produces (binding for every later task):**
- §3 model additions: `ActivityNode.DecidedBy *string` (wire `decidedBy`, optional; legal ONLY on `decision`/`switch`; resolves like a call endpoint — component id or owning use case's actor id); `CallStep.Calls` become `[]TraceCall` where `TraceCall = Relationship + Alt *string` (wire `alt`, optional; calls in one step sharing an `alt` value are surface-alternatives — equivalent entries, not a sequence).
- §4 additions: `CUC-ACTOR-REQUIRED` (clientAction use case with zero actors — coreUseCases-scoped, section `"useCase "+id`); `CC-DECIDED-BY` (a `decidedBy` that resolves to neither a component nor an owning-use-case actor, or appears on a non-decision kind — step-scoped/use-case-scoped per site); note that alternative groups do NOT change CC-PATH-CONNECTED semantics (all alternatives seed `reached`; numbering is presentation). Both new rules ride `ccGateSeverity`.
- §4 grammar note: platform tier aligns to key-first (supersedes the title-first `viewLabel` in gate messages' Section strings; message TEXT may still name titles).
- §5: decider preference order — explicit `decidedBy` > actor-lane inference > entry-Manager inference; alternative groups render as `1a/1b` chips + captions; validation-visibility additions (named CC chip with click-through, per-view roll-up beside the picker, empty-sibling "Not yet realized — part of the pending amendment" state distinct from failure).
- §6 rollout order updated to this plan.

- [ ] **Step 1:** Make the edits above (mirror the spec's existing table/bullet style; date-stamp "rollout rulings 2026-07-31").
- [ ] **Step 2:** Commit: `spec: rollout rulings — decidedBy, alternative groups, required actors, key-first alignment`.

---

### Task 2: Model increment — $defs + hand types + regen + server fallout

**Files:** Modify: `.aiarch/state/project.json` (`.serviceContracts.projectStateAccess["$defs"]` ONLY), `server/internal/resourceaccess/projectstate/projectstateaccess.go`; Generated: `contract.gen.go`, `fake/fake.gen.go`, `server/api/openapi.yaml`.

**Interfaces (produced):**
```go
type TraceCall struct {
    From  string   `json:"from"`
    To    string   `json:"to"`
    Mode  CallMode `json:"mode"`
    Label string   `json:"label"`
    Alt   *string  `json:"alt,omitempty"`
}
type CallStep struct {
    ActivityNodeID string      `json:"activityNodeId"`
    Calls          []TraceCall `json:"calls"`
}
// ActivityNode gains: DecidedBy *string `json:"decidedBy,omitempty"`
```

- [ ] **Step 1:** `$defs`: add `TraceCall` (clone `Relationship`'s schema shape + optional `alt` string); `CallStep.calls` items `$ref` → `TraceCall`; `ActivityNode` gains optional `decidedBy` string. `jq empty` validates.
- [ ] **Step 2:** Hand types: add `DecidedBy` to `ActivityNode` in `projectstateaccess.go` with a doc comment (decision/switch only; endpoint-style resolution). Run `cd server && make gen-models` — expect `TraceCall` + updated `CallStep` in `contract.gen.go`.
- [ ] **Step 3:** Server fallout to zero: `GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off make test-short`. Expected fallout: anywhere iterating `CallStep.Calls` as `[]Relationship` (systemdesign manager, `ParticipantIDs`-descendant helpers if any, `requireDynamicViewSteps` validation — add `decidedBy`/`alt` passthrough there). Old committed data (no `alt`/`decidedBy`) must decode unchanged — add one regression test decoding a step WITHOUT the new fields.
- [ ] **Step 4:** `make gen-client` (OAS). Commit: `model(projectstate): TraceCall.alt + ActivityNode.decidedBy`.

---

### Task 3: Platform methodcheck — new rules, key-first grammar, walker bounding

**Files (platform repo, branch callchain-realization):** Modify: `framework-go/methodcheck/project.go`, `rules_callchain.go`, `rules_statevalidation.go` (viewLabel usage in CC sections only), `activitypaths.go`; Tests: siblings.

**Interfaces (produced):** rule ids `CUC-ACTOR-REQUIRED`, `CC-DECIDED-BY` (both at `ccGateSeverity`); `TraceCall{From,To,Mode,Label string; Alt string}` (string-typed, `Alt` empty when absent); `ActivityNode` gains `DecidedBy string`; CC Section strings minted key-first via a new `ccKeyLabel(dv)` = `Key` fallback `UseCaseID` (matching designhealth's `ccViewLabel` semantics exactly).

- [ ] **Step 1 (TDD):** failing tests: `TestCUCActorRequired_ClientActionWithoutActorFires` / `_TimerWithoutActorPasses`; `TestCC_DecidedByUnresolvableFires` / `_OnActionKindFires` / `_ResolvesToActorPasses` / `_ResolvesToComponentPasses`; `TestCC_SectionsAreKeyFirst` (a step-scoped finding's Section uses the view KEY when Title differs); `TestPaths_BudgetBoundsNestedForkDecision` (adversarial nested fork×decision diagram returns within the cap WITHOUT full enumeration — assert bounded work via a diagram whose full cross-product would exceed 100k paths but which returns ≤512 promptly); alt-group no-op test (`TestCC_PathConnected_AltGroupBothSeedReached`).
- [ ] **Step 2:** Implement: model fields; `CUC-ACTOR-REQUIRED` in the coreUseCases family; `CC-DECIDED-BY` in callChainRules (decision/switch-only legality + endpoint-style resolution, ambiguity = finding like CC-ENDPOINT-RESOLVES); swap CC Section construction from `viewLabel` to `ccKeyLabel` and DELETE the now-misleading grammar comment block, replacing it with: both tiers share the key-first grammar (remove the PRE-MERGE tracked note for grammar). Walker: reintroduce a work budget that bounds RECURSION (charge per walk-step, not per final path; on exhaustion return the paths completed so far — semantics: deterministic prefix, mirroring the output cap) and remove/replace the "PRE-MERGE TRACKED" comment with the actual bound documented.
- [ ] **Step 3:** `GOWORK=off go test ./methodcheck/... && golangci-lint run ./methodcheck/...` green. Commit: `methodcheck: required actors, decidedBy resolution, key-first sections, bounded walker`.

---

### Task 4: designhealth mirror of Task 3

**Files:** `server/internal/utility/designhealth/parse.go` (decode `decidedBy`, `alt`), `rules_callchain.go` (both new rules + bounded walker port), `designhealth_test.go` (table cases + green-fixture pins — expect NO pin-count change: current data has no violations of the new rules; assert their absence explicitly).

- [ ] **Step 1:** TDD table cases per new rule (Warning severity), decode extension, bounded-walker port (line-faithful to Task 3's final code), key-first note updated (grammar now uniform — simplify the comment).
- [ ] **Step 2:** `GOWORK=off go test ./internal/utility/designhealth/... && GOWORK=off make test-short` green; validate one-shot still 0 errors slot-scoped. Commit: `designhealth: mirror required-actors + decidedBy rules, bounded walker`.

---

### Task 5: webApp — codec + alt groups + decidedBy preference

**Files:** regen (`npm run gen:api && npm run gen:ops`); Modify: `webApp/src/contracts/realization.ts` (+`linearizeSteps` alt-aware numbering), `adapters.ts`, `webApp/src/components/flow/deciderResolution.ts` (+test), `DynamicViewFlow.tsx` (chips/captions `1a`/`1b`), `fragmentCaption.ts` if caption strings change; leaf tests.

**Interfaces (produced):** `SequencedCall` gains `altLabel?: string` (e.g. `"1a"` — computed: calls sharing an `alt` value within a step share the numeric part, letters by declared order; calls without `alt` keep plain numbers); `resolveDecider` gains a first branch: explicit node `decidedBy` (resolve id against persons ∪ components with the SAME placement guard as the actor branch; unresolvable → existing inference chain).

- [ ] **Step 1 (TDD):** leaf tests: alt-aware numbering (5-call entry step with two alt pairs → `1a,1b,2a,2b,3`); decidedBy-first preference incl. placement-guard fallback; seq-chip rendering value = `altLabel ?? String(seq)`.
- [ ] **Step 2:** Implement; FragmentBar caption rows show the alt label; `npm run check` green. Commit: `webApp: alternative-group numbering + explicit decidedBy preference`.

---

### Task 6: webApp — validation visibility

**Files:** Modify: `webApp/src/components/flow/ArchitectureView.tsx`, `DynamicViewFlow.tsx` (FragmentBar chip), new leaf `webApp/src/components/flow/viewVerdict.ts` + test; `UIIdentifiers.ts` + uitests TESTID map.

**Interfaces:** `viewVerdict(findings, dvKey, realizedStepCount, eligibleNodeCount): { label: string; tone: 'ok'|'warn'|'error'|'pending' }` — e.g. `"15/15 realized · CC clean"`, `"0/7 realized · pending"`, `"2 CC findings"`.

- [ ] **Step 1 (TDD):** leaf tests for the four verdict shapes; then: (a) the FragmentBar "passing ✓" chip becomes `CC checks · passing` and clicking navigates to the Design Health step (StepLink kind `standardCheck`) — a REAL affordance this time; (b) a per-view roll-up chip beside the dynamic picker (`viewVerdict` output, testid `Architecture.VIEW_VERDICT`); (c) empty sibling views render "Not yet realized — part of the pending realization amendment" (distinct copy + `pending` tone) instead of the generic empty message.
- [ ] **Step 2:** `npm run check` + uitests lint green. Commit: `webApp: named CC verdicts, view roll-up, honest pending state`.

---

### Task 7: Activity-diagram amendments (slot 4) — event entries, reshapes, decidedBy

**Files:** `.aiarch/state/project.json` slot 4 (+ slot 5 keys untouched); gate loop.

Consult the system-architect agent BEFORE authoring (founder standing directive): dispatch it to validate the concrete node/edge edits below against spec §6a and current data, and to author the `decidedBy` assignments. Binding content (from spec §6a + architect assessment):
- 5 timer/busMessage diagrams gain event ENTRIES (no incoming edge): `bill-the-user-for-usage` (`period-elapses` action → `timeEvent`), `retry-a-declined-service-invoice`, `execute-a-construction-activity` (busMessage → `acceptEvent` entry), `replan-under-scope-change` (RECLASSIFY trigger busMessage → timer + `timeEvent` entry — no queued static realization exists), `operate-a-delivered-system` (drop the artificial fork: operator path keeps `start`, reconcile sweep becomes an edge-less `timeEvent` entry; keep both end nodes).
- linkedActorId cleanup: remove from nodes whose "actor" can never legally call a Client (`charge-user`/payment-provider, `customer-charged`/customer, settlement-manager, operated-system, infrastructure) — lanes stay via roleName.
- Node folds: `customer-charged` → note or fold into `charge-user`; `in-flight` → note; `argo-reconcile` → note (no honest realization).
- `decidedBy` authoring: every decision node whose decider is NOT the entry Manager gets an explicit id — at minimum uc1's `decision` ("Review action?") → `architect-user`, `critique-result` → the critic role's actor if declared (else leave for Manager inference); architect judges the rest.
- CUC-ACTOR-REQUIRED must stay green (all clientAction UCs keep ≥1 actor).

- [ ] **Step 1:** Architect consult (report to workspace). **Step 2:** Author the edits. **Step 3:** Full gate loop (Global Constraints) — expect CC-TRIGGER-EVENT warnings to DROP for the 5 amended UCs; UC-ACT-PRESENT green (event entries satisfy it); zero Errors. **Step 4:** Commit: `coreUseCases: UML event entries, trigger reclassification, decider attribution (design amendment)`.

---

### Task 7b: Reclassification amendment — message-bus utility + design-health engine + replan queued edge (FOUNDER-RATIFIED 2026-08-01)

**Files:** `.aiarch/state/project.json` (slot 5 + `.serviceContracts` re-home), server package moves (`internal/resourceaccess/durableexecution` → `internal/utility/messagebus` or per alignment-gate naming; `internal/utility/designhealth` → `internal/engine/designhealth`), `server/internal/arch_test.go` (Managers-only import rule for the messaging utility), generated artifacts re-run, webApp flow-layer/UI cleanup (utility carve-outs no longer apply to design-health).

Rulings encoded: durable-execution-access + durable-execution-runtime fold into a `message-bus` UTILITY (verbs deliverSignal/registerSchedule; execution-substrate role stays invisible; M→RA edges REMOVED; queued M→M edges remain the messaging representation; only-Managers enforced by arch-test import rule — the ch. 5 restriction). design-health becomes an ENGINE (`design-health-engine`; existing sdm edge becomes canonical M→E; webApp utility carve-outs stop applying naturally — verify the trace renders it as a normal engine). NEW queued edge `construction-manager →(queued)→ project-design-manager` ("variance detected → trigger replan sweep (queued)") added in the same slot-5 amendment. Architect consult REQUIRED before landing (formal amendment spec: exact component/edge/volatility-mapping/contract changes + package-move map + which methodcheck/designhealth expectations shift). All gates green both repos; realizations (8–10) author against the corrected roster.

### Task 7c: Schedule wiring (FOUNDER-RATIFIED 2026-08-01)

**Files:** `server/cmd/server` startup path (or hooks.go seam), `server/internal/manager/construction/` (new RegisterSchedules: pump 30s + replanSweep 5m via the messaging utility's registerSchedule), stale header comment fix (`constructionmanager.go:7-8`), tests.

Wire `RegisterSchedules` (billing, operations, construction) at startup so the Schedule-fired model describes running code. Idempotent registration (the RA/utility verb already converges via last-writer-wins). Verify: a locally booted server registers all schedules against dev Temporal (assert via ScheduleClient list in a test or the report); `SettlementDelay` retry path becomes live.

### Tasks 8–10: Realize the 15 views (three batches) + uc1 retrofit

**Batching (one task each, identical procedure):**
- **Task 8 — core four:** `commit-to-a-project-option`, `execute-a-construction-activity`, `operate-a-delivered-system`, `bill-the-user-for-usage`. FIRST check `STP-CHAIN-COVER` (Error): the committed system test plan's happy-case step order must agree with each realization's Client-entry order — read the committed `.testingState`/test-plan slot before authoring; where they conflict, the realization follows the ARCHITECTURE and the discrepancy is reported for a test-plan amendment (do not silently bend the chain).
- **Task 9 — uc1 retrofit + operations/PM cluster:** retrofit `drive-system-design` (alt groups on the 4 entry calls → `1a/1b/2a/2b`; add the missing design-session facet ops as a step on the "Review action?" decision — calls `system-design-manager→project-state-access` for `rejectArtifactOnBranchWithComments` / `readProjectOnBranch` per the Task-12 review earmark); realize `manage-projects`, `track-weekly-project-progress`, `replan-under-scope-change`, `retry-a-declined-service-invoice`.
- **Task 10 — remaining variations:** `onboard-a-new-customer`, `add-a-use-case-to-an-in-flight-project`, `view-the-project-state-log`, `download-generated-source-code`, `view-operating-cost-projection`, `ask-a-clarifying-question-during-review`, `send-back-change-requests-for-a-redraft`.

**Procedure per batch (binding):** (1) system-architect agent authors the step→calls tables against activity nodes + static relationships (spec §6a sampled walks are the precedent; every component→component call must match a static `(from,to,mode)`; both-surface entries get alt groups; timer/busMessage entries root per CC-PATH-CONNECTED; no static-model surgery — an undrawable chain STOPS the task for founder escalation per the composable-design caution). (2) Implementer lands the JSON. (3) Gate loop; success criteria: ZERO CC findings for every realized view in the batch; designhealth green-fixture pins updated by computed delta (CC-COVERAGE count drops by the batch's eligible-node count; document the arithmetic). (4) Founder spot-check on the live QA stack is INVITED (stack stays up) but not blocking. Commit per batch: `systemDesign: realize <n> views — batch k/3 (design amendment)`.

- [ ] Task 8 steps 1–4 as above. - [ ] Task 9 steps 1–4. - [ ] Task 10 steps 1–4 (after this: CC-COVERAGE pins reach ZERO; `TestGreenFixtureAdvisoriesFire` now asserts NO CC advisories at all).

---

### Task 11: Collateral slots — attestations, staleness pass, ACT-COMPONENT-COVERAGE

**Files:** `.aiarch/state/project.json` slots 5 (attestations), 6/8–16 (reviewThread notes), 9 (activityList) OR a waiver; gate loop.

- [ ] **Step 1:** Re-word slot-5 attestations D3 ("each carries its own dynamic view…" → step-keyed realization language) and D4 ("new call chains" → "new step-keyed realizations"); re-stage all 10 with the amendment provenance.
- [ ] **Step 2:** Staleness pass: append a "Reviewed — unaffected: call-chain realization amendment (steps model, all 16 views realized)" review note to each committed downstream slot 6, 8–16 (slot 7 withdrawn — skip), mirroring the existing rev-2 note pattern.
- [ ] **Step 3 (FOUNDER GATE):** the 3 pre-existing unscoped-validate errors (`ACT-COMPONENT-COVERAGE`: agentic-job-access, merchant-gateway-access, artifact-access lack coding activities). Dispatch the system-architect + project-manager agents to propose: add the missing coding activities to `.activityList` (with float/network impact noted) OR record a justified waiver. Present both to the founder; land the ratified option. Unscoped `validate --root ..` must exit 0 after this step.
- [ ] **Step 4:** Full gate loop; commit: `state: attestation re-wording, staleness pass, activity coverage resolution (design amendment)`.

---

### Task 12: Severity flip

**Files:** platform `framework-go/methodcheck/rules_callchain.go` (`ccGateSeverity` → `SeverityError`), app `server/internal/utility/designhealth/rules_callchain.go` (`ccLiveSeverity` → Error); tests both sides.

- [ ] **Step 1:** Flip both constants; update `TestCC_AllRulesAreWarningSeverityInPoC` → `...ErrorSeverity` (and rename honestly); designhealth green-fixture: zero CC findings of ANY severity on the committed state (proven by Tasks 8–11) — pins already assert absence; add one negative-fixture severity assertion per tier confirming Error.
- [ ] **Step 2:** Full gates both repos + unscoped validate 0 errors + `make method-check` + webApp check. THE HARD GATE IS NOW LIVE: any future System draft/commit with a CC violation blocks. Commit both repos: `gates: call-chain correspondence flips to Error (hard gate live)`.

---

### Task 13: method-assets doctrine + command skills

**Files (platform repo):** `method-assets/` skills: `the-method-architecture/SKILL.md` (+STRUCTURIZR-CONVENTIONS.md if it names dynamic-view shapes), system-draft/system-critique command skills; then the app's materialized `.claude/` copies via the established materialization flow (drift gate proves sync).

- [ ] **Step 1:** Rewrite step 9 (author `Steps` with `TraceCall`s; actors as participants; entry-call convention — entry calls ride the first action step; alt groups for cross-surface equivalence; within-step call order is load-bearing; decidedBy on decisions; event-node entries for timer/busMessage; step-eligibility table). DELETE the "back-populate linkedComp" section. Update the per-view validation table to the CC-* reality (all ten + CUC-ACTOR-REQUIRED, Error). Drop step-11's PlantUML-carried-on-DynamicView instruction.
- [ ] **Step 2:** Update system-draft/system-critique command skills to author/review realizations (reference the rules by id; critique checklist gains "does each step's fragment tell the truth about the decomposition").
- [ ] **Step 3:** Materialize into the app repo's `.claude/`; drift gate green (`make claude-assets` or the repo's established target — discover via the Makefile). Commits: platform `method-assets: step-keyed realization doctrine`; app `chore: materialize method-assets doctrine`.

---

### Task 14: Cleanup wave

**Files:** small, enumerated: `webApp/src/components/flow/flowLayout.ts` (muted-edge 0.12 literal → `MUTED_NODE_OPACITY` import or a shared `MUTED_OPACITY`), `.gitignore` (+`.superpowers/brainstorm/`), `.github/workflows/webapp-checks.yml` (+`npm run format:check`), `server/internal/resourceaccess/projectstate/projectstateaccess.go` (requireSystemFields doc-comment rewrap), `webApp/src/components/usecase/UseCaseWalkthrough.tsx` (path ResizeObserver deps; aria-controls dangling note → render the map region id on a stub div pre-mount OR accept + comment), docs note on the twin-tier mirror requirement (one paragraph in `server/internal/utility/designhealth/designhealth.go` header naming the platform twin).

- [ ] **Step 1:** Land all; full app gates + uitests lint. Commit: `chore: rollout cleanup wave (tokens, gitignore, CI format gate, doc nits)`.

---

### Task 15: Release choreography + final review + merge (FOUNDER EXECUTES PUSHES)

- [ ] **Step 1:** Final whole-branch review (both repos, most capable model, ledger-pointed) over the rollout range; ONE fix wave + scoped re-review if findings.
- [ ] **Step 2:** Platform release prep: confirm `framework-go` + `method-assets` diffs are release-clean (`GOWORK=off` build/test/lint per module); write the release notes (rule inventory delta, model changes, migration note for downstream apps: old-shape views decode as zero-step and now ERROR on commit — downstream must realize before their next System commit).
- [ ] **Step 3 (FOUNDER):** push platform branch, merge, tag `framework-go` + `method-assets` releases (versions per platform convention — next minor of each).
- [ ] **Step 4:** App: remove the PoC `replace` from `server/go.mod`, pin the new released versions (framework-go + method-assets), `GOWORK=off` full gates + `make gen-models-check` + method-check + validate green against PINNED deps.
- [ ] **Step 5 (FOUNDER):** push app branch; CI green; merge `callchain-realization` → `main`.
- [ ] **Step 6:** Teardown: stop QA server/vite/temporal (runbook §8), `rm -rf archistrator-qa-clone`, delete the SDD workspaces for both plans, update the auto-memory file for this workstream (status: SHIPPED).

---

## Self-review notes

- Coverage vs spec §6 final phase + all ledger parked items: realizations (8–10), activity amendments + trigger fixes (7), attestations/staleness (11), ACT-COMPONENT-COVERAGE (11), flip (12), doctrine (13), release/pin swap (15), grammar alignment (3), walker bounding (3–4), validation visibility (6), decidedBy/alt (1–5, 7, 9), required actors (1, 3–4), cleanup minors (14). Deliberately excluded: per-pane scrolling + chat-rail default (founder ruled leave), gtdapp/gtd-qa2 amendments (their own sessions), reverse sync click-node→jump (future increment, data in place).
- Founder gates: Task 11 step 3 (ratify activities-vs-waiver), Task 15 steps 3+5 (pushes/merges). Batches invite non-blocking spot-checks on the live stack.
- Type-name consistency: `TraceCall.Alt`/`alt`, `ActivityNode.DecidedBy`/`decidedBy`, `altLabel`, `viewVerdict`, `ccKeyLabel` — each defined once before use.
