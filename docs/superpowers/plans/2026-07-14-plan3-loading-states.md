# Plan 3: Honest Role-Driven Loading States (spec §9)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Loading screens say who is doing what — "Architect is crafting the vision and mission statement", "Product manager is reviewing …" — driven by real workflow state set at dispatch boundaries; never simulated (QA-F15 honesty bar).

**Architecture:** `SessionStateView` (both design managers, schema-first in project.json) gains `activeRole`/`activeStep`/`round`, set on the workflow state at the dispatch/observe boundaries and served by the existing sessionState query. The SPA maps the new fields through the generated enum chain and renders a role line (RoleAvatar + verb + per-kind phrase) in GeneratingScene with an honest fallback. Construction gets the same labeling client-side from the already-flowing `workerClass`.

**Spec:** `docs/superpowers/specs/2026-07-13-method-prompt-pass-design.md` §9. Recon anchors verified @ local main daa6a5b.

## Global Constraints

- Branch `loading-states` off local `main`. NEVER push archistrator. Server work under `server/` with GOWORK=off.
- Schema-first: contract shapes change ONLY in `.aiarch/state/project.json` `$defs`, then `make gen-models gen-client gen-sdk` (server/Makefile:29/43/94); never hand-edit `*.gen.go` / `enums.gen.ts` (webApp regen: `npm run gen:api`).
- Honesty bar: fields are set at dispatch boundaries and cleared on observed completion/terminal — no timers, no inference. Setting workflow-local state adds NO Temporal history commands → no GetVersion gate needed (assert this stays true; if an implementer finds an activity call is needed, STOP).
- New enums (both manager contracts, identical): `ActiveRole` int enum varnames `ActiveRoleNone, ActiveRoleArchitect, ActiveRoleProductManager`; `ActiveStep` varnames `ActiveStepNone, ActiveStepDrafting, ActiveStepCritiquing, ActiveStepRevising`. Clean lowerFirst-derivable → fully-generated TS pattern (enums.gen.ts, like SessionStage at :601-635); no hand enumMappings block.
- `SessionStateView` additions (both `$defs.SessionStateView`): `activeRole`→`#/$defs/ActiveRole`, `activeStep`→`#/$defs/ActiveStep`, `round`→integer; all three added to `required` (zero values = none/none/0).
- Copy (exact): draft → "Architect is crafting the {phrase}"; critique → "Product manager is reviewing the {phrase}"; revise → "Architect is revising the {phrase} (round {N})". Fallback when role=none → today's "DRAFTING…" pill unchanged. {phrase} per kind from METHOD_METADATA (new `phrase` field), mission = "vision and mission statement".

---

### Task C1: Contract + systemdesign workflow sub-steps

**Files:**
- Modify: `.aiarch/state/project.json` (`.serviceContracts.systemDesignManager.$defs`: + ActiveRole, ActiveStep, SessionStateView fields; same under `.serviceContracts.projectDesignManager.$defs` — do BOTH here so one regen covers all)
- Regen: `cd server && make gen-models gen-client gen-sdk` (touches both managers' contract.gen.go, api/openapi.yaml, systemtests/internal/sdk)
- Modify: `server/internal/manager/systemdesign/coauthorartifact.go` — `coAuthorState` (:1115-1161) gains `activeRole ActiveRole`, `activeStep ActiveStep`, `activeRound int`; `state.view()` (:1163-1223) returns them.
- Test: `server/internal/manager/systemdesign/manager_test.go`

**Set/clear sites (recon-verified):**
- `dispatchDraftAndReadBack` — immediately before the dispatch at :676: role=Architect, step = Drafting if `*redraftCount==0` else Revising, round=`*redraftCount`; clear (None/None/0) at the observed-success return :714 AND on the failure returns :691/:697 (the DraftFailed stage carries failure display).
- `runPMCritique` — before the dispatch at :742: role=ProductManager, step=Critiquing; clear on BOTH outcomes (:800 revise path — before returning stepRedraft — and the :803 proceed).
- Belt-and-braces clears where stage goes AwaitingReview (:357, :1092) and at terminals (:993 withdrawn, :1082 committed) and :1503 draftFailed.

**Steps:** failing test first — drive a session through draft → critique → revise → approve using the existing fakes and assert the queried view's (activeRole, activeStep, round) sequence at each phase boundary: (architect,drafting,0) while draft in flight → (pm,critiquing,0) while critique in flight → (architect,revising,1) on redraft → (none,none) at awaitingReview/committed; plus draftFailed clears to none. Then implement; `GOWORK=off go build ./... && go test ./internal/manager/systemdesign/... && golangci-lint run ./internal/manager/systemdesign/...`; run `make gen-models-check gen-client-check gen-sdk-check` (post-regen drift clean); commit.

### Task C2: projectdesign mirror

**Files:** `server/internal/manager/projectdesign/coauthorphase2artifact.go` (state struct :244-area, view, draft dispatch :487-495 set / :522 clear, stage sites :676/:778/:795/:888/:898/:951 clears), `assemblesdpreview.go` (NO role — assembly is server-side; ensure view returns none/none/0), `manager_test.go`.

Phase-2 has no critique: roles are Architect-only. Test mirrors C1 (draft → revise → approve sequence + assembling view stays none). Same gates. Commit.

### Task C3: webApp role line

**Files:**
- Regen: `cd webApp && npm run gen:api` (enums.gen.ts gains ActiveRole/ActiveStep from x-enum-varnames)
- Modify: `webApp/src/contracts/wire.ts` — `mapSessionState` (:457-482) + `mapProjectSessionState` (:486-509) carry `activeRole`/`activeStep`/`round` into the view (ordinal→app-string via the generated maps, sessionStageFromOrdinal pattern wire.ts:107-109)
- Modify: `webApp/src/contracts/methodMetadata.ts` — add `phrase: string` to every METHOD_METADATA entry (17 kinds; mission = "vision and mission statement", others = natural lowercase noun phrases of their titles)
- Modify: `webApp/src/components/design/GeneratingScene.tsx` — new optional props `activeRole?`, `activeStep?`, `round?`, `phrase?`; when role ≠ none render the role line: `RoleAvatar` (seed `system-architect` | `product-manager`, small size) + the exact copy from Global Constraints; when none/absent, today's pill and copy EXACTLY as-is (fallback = old server). Keep the CI-job affordance and amendment notice untouched. Update the honesty header comment: the per-role line is now real state, set at dispatch boundaries server-side.
- Modify: `webApp/src/routes/DesignExperience.tsx` (:651-654) + `webApp/src/routes/ProjectDesignExperience.tsx` (:589-592) — pass the new view fields + `phrase` from METHOD_METADATA[kind]
- Test: follow the repo's existing webApp test conventions (check how components are currently tested; if only typecheck/lint gates exist, add the copy mapping as a small pure function `roleLineFor(role, step, round, phrase)` in a testable module with a unit test, and have GeneratingScene consume it)

Gates: `npm run typecheck` (or the tsc command webapp-checks.yml runs — read it), eslint, and the gen:api drift is clean (re-run produces no diff). Commit.

### Task C4: construction labeling (client-only)

**Files:** `webApp/src/components/construction/ConstructionTracker.tsx` (+ small helpers) — for the in-flight activity, render a role line: RoleAvatar (seed = the activity's `workerClass`, already on schema.ts DTOs :649) + "{Role display name} is {phase verb} {activity title}" (phase verb from the activity's method phase: requirements→"scoping", detailed-design→"designing", construction→"constructing", test-plan→"planning tests for", integration→"integrating"). No server change; label derives ONLY from already-flowing dispatched state (honesty bar). Reuse `roleLineFor`-style pure helper + test per C3's convention. Gates + commit.

### Task C5: full gate + final review + merge

- `cd server && gofmt -l . && GOWORK=off go build ./... && go vet ./... && golangci-lint run ./... (delta vs main = 0) && make test-short` + all 7 gen-*-checks; webApp typecheck/lint/gen drift.
- Whole-branch review (SDD final template, most capable model): seams = contract↔regen↔wire↔scene chain end-to-end; honesty bar (no simulated state anywhere); enum ordinal alignment across Go/OAS/TS; construction label truthfulness.
- Fix wave if needed → merge `loading-states` → local main (NO push).
