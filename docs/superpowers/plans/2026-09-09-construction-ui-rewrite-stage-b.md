# Construction UI Rewrite — Stage B: the shell and the activity-list lens

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the construction console's three tabs with one route, a lens control, and a persistent shared detail pane — then build the first lens: a three-tier activity tree (activity › lifecycle phase › Figure A-1 task) that renders an honest Method skeleton for every activity and fills it from the attempt ledger Stage A delivered.

**Architecture:** One TanStack route (`/project/$projectId/construction`) owns a shared toolbar, a lens segmented-control, and a resizable right detail pane laid out beside the content — not an overlay. Selection lives in URL search params so the pane never owns it and survives the 1.5s cascade poll. The tree's ROW SET is derived from `lifecycleTemplates.gen.ts` (server-generated profiles + Figure A-1 task vocabulary), never from stored data; only row STATE comes from `ConstructionRow.phases` / `.attempts`. Stages C and D add lenses to this shell without touching it.

**Tech Stack:** React 19, TypeScript 5.9 (`exactOptionalPropertyTypes`), TanStack Router 1.135, MUI 7 + `@mui/x-tree-view` (new, MIT), `@xyflow/react` 12 (already present, Stage D), `node:test`, Vite.

**Spec:** `docs/superpowers/specs/2026-09-09-construction-ui-rewrite-design.md`
**Predecessor:** `docs/superpowers/plans/2026-09-09-construction-ui-rewrite-stage-a.md` (complete; 34 commits on this branch)

## Global Constraints

- Node commands from `webApp/`. Go commands from `server/`, ALWAYS `GOWORK=off`.
- **The real TypeScript gate is `npm run typecheck` (`tsc -b`)**, NOT `npx tsc --noEmit` — the latter uses a looser config and let a real break through twice in Stage A.
- **This repo has NO vitest.** Every test uses `node:test`. Run `npm test` or `node --test <files>`.
- **Never hand-edit a generated file**: `schema.ts`, `enums.gen.ts`, `ops.gen.ts`, `lifecycleTemplates.gen.ts`, any `*.gen.go`. Change the generator and re-run it.
- **Never weaken a gate.** These must stay green: `make lint` (0 issues), `go test ./...`, `TestFileLayout`, `TestGeneratedOnlyPublic`, `TestRepoStructureCmdIsClosed`, `TestNoBannedPhaseIdentifier`, and the four `-check` drift targets.
- **The bare word `phase` is banned as a NEW identifier** (gated). Use `ProjectPhase`, `LifecyclePhase`, `MethodTask`, `TaskAttempt`.
- **No hand-mirroring the server.** The phase set, the task vocabulary, the gate flag and the conditional flag all come from `lifecycleTemplates.gen.ts`. If you need something it does not carry, extend `server/cmd/gen-uiprofiles` and re-run it.
- **Skeleton always, fill sometimes.** Tier-2 and tier-3 rows exist for every activity because the profile is deterministic. Only their STATE is unknown. Never omit a row because there is no data, and never invent state because a row exists.
- **Conditional tasks** (`someConditional === true`: `someConstruction`, `testClient`) render ONLY when a real attempt exists for them. Emitting a row for work that never happened is the failure this whole rewrite exists to remove.
- **Provenance is a channel, not a state.** Anything reconstructed gets the hatched rail; the badge sits on the GROUP header, never per row. Never use colour for provenance — colour is committed to status and float.
- **Failure is never terminal.** Every failed row carries an inline retry, and the detail surface's `↻ Run this task` is present and enabled in EVERY state, including `passed` and `unknown`.
- Commit after every task. End messages with `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.

## Facts carried from Stage A (verified, do not re-derive)

| Fact | Value |
|---|---|
| `ConstructionRow` optional fields | `kind`, `variant`, `status`, `currentLifecyclePhase`, `worstOrigin` are ABSENT when unknowable — all deliberately gated |
| `ConstructionRow` always-present | `phases: PhaseRow[]`, `attempts: TaskAttemptRow[]`, `classified: boolean` |
| Live rows | 69 total · 46 classified · 23 unclassified · 25 with a ledger (92 attempts) · 127 phase entries emitted |
| Attempt key | `<activityId>:<task>:<n>` — the tree's node id and the URL's deep-link key |
| `@mui/x-tree-view` | NOT installed. Stage B adds it. |
| `@xyflow/react` | `^12.10.2`, already present |
| Selection-wipe hazard | `NetworkView.tsx:106-128` `signatureOf()` + module-level `selectionStore` — the console polls at 1.5s; copy this pattern or selection is wiped mid-glance |
| Design tokens | `utilities/theme/themes.ts` via `useTokens()`; `scan()` is the hatch texture; five themes must all keep working |

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `server/internal/manager/systemdesign/systemdesignmanager.go` | evidence-less rows stop asserting a build status | 1 |
| `webApp/src/contracts/types.ts`, `wire.ts` | `layer`/`layerBand` onto `ConstructionRow`; `status` gated on evidence | 1, 2 |
| `server/cmd/gen-uiprofiles/main.go` | emit per-task display labels | 2 |
| `webApp/src/components/construction/lens/ConstructionShell.tsx` *(new)* | route shell: toolbar, lens control, detail-pane layout | 3 |
| `webApp/src/components/construction/lens/useLensSelection.ts` *(new)* | URL search-param selection + poll-proof store | 3 |
| `webApp/src/components/construction/detail/DetailPane.tsx` *(new)* | resizable laid-out pane, invariant header + action bar | 4 |
| `webApp/src/components/construction/list/activityTree.ts` *(new)* | pure: row → three-tier tree model (the derivation) | 5, 6, 7 |
| `webApp/src/components/construction/list/ActivityTreeView.tsx` *(new)* | `RichTreeView` + custom `TreeItem` grid | 5, 6, 7 |
| `webApp/src/components/construction/provenance.tsx` *(new)* | hatch rail + `ReconstructedBadge` | 8 |
| `webApp/src/components/construction/detail/bodies/*.tsx` *(new)* | unknown / episode / review / artifact bodies | 9, 10, 11 |
| `webApp/src/components/construction/list/CoverageStrip.tsx` *(new)* | 40-vs-69 seam, legacy tray | 12 |
| `webApp/src/routes/ConstructionConsole.tsx` | rewired to the shell | 3, 13 |

---

### Task 1: An activity with no evidence stops claiming it is being built

**Files:**
- Modify: `server/internal/manager/systemdesign/systemdesignmanager.go`
- Modify: `webApp/src/contracts/wire.ts`, `webApp/src/contracts/types.ts`
- Test: `server/internal/manager/systemdesign/manager_test.go`, `webApp/src/contracts/constructionAdapters.test.ts`

**Interfaces:**
- Produces: a `ConstructionRow.status` that is absent when the row has neither stored phases nor an attempt ledger.

This is the residual the Stage A final review ruled *"deferrable for this merge, must-fix before these views are presented as authoritative"* — and Stage B is where they become authoritative, so it is the first commit.

Twenty **classified** activities have no stored phases and no attempts. They resolve to `nil` completions, so `CoarseBuildStatus` returns its zero value `BuildInConstruction`, and `BUILD_STATUS_META['in-construction']` renders **"In construction"** for work that has not begun. Worse, it is load-bearing: `computeActivityStatuses` (`constructionAdapters.ts:264-267`) short-circuits network-derived readiness for any row present in `constructionRows`, so these twenty never fall through to `eligible`/`blocked` either.

Per the spec's test — *does it state something false, or merely leave it unknown?* — "In construction" is a positive assertion contradicted by the record. `Phase = notStarted` is NOT part of this: it is true of the record and has no consumer.

**Do NOT reuse the `unclassified` status.** That means "we don't know *what this is*"; this row is classified and simply has no evidence. Conflating them is the second conflation this rewrite exists to prevent. The spec (§7.2) rules that this state gets **no chip at all**, so express it the way Stage A expressed every sibling case: gate it, and let the consumer render an honest fallback.

- [ ] **Step 1: Write the failing tests**

Go — assert a classified row with neither phases nor attempts does not assert a build status:

```go
func TestConstructionRowsToContract_NoEvidenceAssertsNoBuildStatus(t *testing.T) {
	// A classified row with no stored phases AND no attempt ledger has no basis
	// for any build status. "In construction" would be a positive claim about
	// work that has not begun.
	rows := map[string]projectstate.ActivityConstructionStatus{
		"C-x": {ActivityID: "C-x"},
	}
	meta := map[string]projectstate.ActivityItem{
		"C-x": {Name: "C-x", WorkerClass: "junior-developer", Coding: true},
	}
	got := constructionRowsToContract(rows, meta, nil)
	row := got["C-x"]
	if !row.Classified {
		t.Fatalf("fixture should classify; got Classified=false")
	}
	if row.HasBuildEvidence {
		t.Errorf("HasBuildEvidence = true for a row with no phases and no attempts")
	}
}
```

TypeScript — assert the mapper drops `status`:

```ts
it('does not surface a status for a classified row with no evidence', () => {
  const row = mapConstructionRow({
    ActivityID: 'C-x', Classified: true, hasBuildEvidence: false,
    Phases: [], attempts: [], BuildStatus: 0, Phase: 0,
  } as never);
  expect(Object.prototype.hasOwnProperty.call(row, 'status')).toBe(false);
});

it('still surfaces a status for a classified row WITH evidence', () => {
  const row = mapConstructionRow({
    ActivityID: 'C-y', Classified: true, hasBuildEvidence: true,
    Phases: [{ Phase: 'construction', Weight: 40, Label: 'Construction', Completed: true, ArtifactRef: '' }],
    attempts: [], BuildStatus: 1, Phase: 1,
  } as never);
  expect(row.status).toBeDefined();
});
```

- [ ] **Step 2: Run them and confirm they fail**

```bash
cd server && GOWORK=off go test ./internal/manager/systemdesign/ -run TestConstructionRowsToContract_NoEvidence -v
cd ../webApp && node --test src/contracts/constructionAdapters.test.ts
```

Expected: Go fails on the unknown field `HasBuildEvidence`; TS fails because `status` is present.

- [ ] **Step 3: Implement**

Add `hasBuildEvidence` to the wire contract. `.aiarch/state/project.json` → `.serviceContracts.systemDesignManager.$defs.ActivityConstructionStatus.properties`, and to its `required` list (a dropped flag must read as *no evidence*, which is the safe direction):

```json
"hasBuildEvidence": { "type": "boolean" }
```

Then regenerate and wire it:

```bash
cd server && GOWORK=off make gen-models && GOWORK=off make gen-client
cd ../webApp && npm run gen:api && npm run gen:ops
```

In `constructionRowsToContract`, set it from the same `resolved` slice the coarse status already uses — evidence means the row resolved to at least one phase completion, i.e. it had stored phases or a ledger. In `mapConstructionRow`, gate `status` on `classified && hasBuildEvidence`, using the same conditional-spread idiom already used for `kind`, and document why in the field's doc comment.

Then find every consumer of `row.status` (`npm run typecheck` lists them) and give each an honest fallback — the same treatment `kind` already received. Check `computeActivityStatuses` specifically: a row with no evidence must now fall through to network-derived `eligible`/`blocked` rather than short-circuiting.

- [ ] **Step 4: Verify**

```bash
cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/manager/systemdesign/ && GOWORK=off make gen-models-check gen-client-check && GOWORK=off make lint
cd ../webApp && npm run typecheck && npx eslint src/ && npm test
```

Then confirm on live data that the twenty rows changed and nothing else did:

```bash
curl -s 'http://localhost:8888/api/v1/system-design/get-project/archistrator' | python3 -c "
import json,sys
from collections import Counter
rows=json.load(sys.stdin)['ActivityConstruction']
noev=[k for k,v in rows.items() if v.get('classified') and not v.get('hasBuildEvidence')]
print('classified, no evidence:', len(noev))
print('with evidence:', sum(1 for v in rows.values() if v.get('hasBuildEvidence')))
"
```

Expected: ~20 with no evidence, ~26 with. If the second number is not 26, the resolution changed — investigate before committing.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "fix(construction): an activity with no evidence asserts no build status

Twenty classified activities have neither stored phases nor an attempt
ledger, so they resolved to nil completions and CoarseBuildStatus returned
its zero value — rendering 'In construction' for work that has not begun,
and short-circuiting computeActivityStatuses so they never fell through to
eligible/blocked either.

Gated on a new hasBuildEvidence flag rather than reusing 'unclassified',
which means something different (we do not know WHAT this is, versus we
know what it is and have no evidence about it).

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: Dependencies and the two generator gaps

**Files:**
- Modify: `webApp/package.json`
- Modify: `server/cmd/gen-uiprofiles/main.go` → regenerates `lifecycleTemplates.gen.ts`
- Modify: `webApp/src/contracts/types.ts`, `wire.ts`

**Interfaces:**
- Produces: `@mui/x-tree-view` available; `GeneratedTask.label`; `ConstructionRow.layer` / `.layerBand`.

Three small prerequisites, batched because each is a few lines and all three block later tasks.

**(a) `@mui/x-tree-view`.** Install it (MIT — do NOT install `@mui/x-tree-view-pro` or `@mui/x-data-grid`, both commercial). Pin a version compatible with MUI 7.

**(b) Per-task display labels.** The generated vocabulary carries `task`, `gate`, `conditional` but no human label, so Stage B would hand-author twelve strings in TS — a fresh hand-mirror in the very file whose header documents the last one. Extend `gen-uiprofiles` to emit `label` per task (`srs` → `"SRS"`, `srsReview` → `"SRS Review"`, `someConstruction` → `"Some Construction"`, `detailedDesign` → `"Detailed Design"`, `designReview` → `"Design Review"`, `construction` → `"Construction"`, `testClient` → `"Test Client"`, `codeReview` → `"Code Review"`, `integration` → `"Integration"`, `testing` → `"Testing"`, `stp` → `"STP"`, `stpReview` → `"STP Review"`). Put the label table in Go beside the task constants, not in the generator, so the vocabulary stays in one place.

**(c) `layer` / `layerBand` on `ConstructionRow`.** Stage A put them on the wire; the SPA mapper never read them (Task 9 predated Task 11). Stage D needs them and it is two lines now. Map them with the usual `?? undefined` guard.

- [ ] **Step 1: Write the failing tests**

```ts
it('carries the layer projection through to the row', () => {
  const row = mapConstructionRow({
    ActivityID: 'U-SPA-billing-manager', Classified: true,
    Phases: [], attempts: [], layer: 'client', layerBand: 'layered',
  } as never);
  expect(row.layer).toBe('client');
  expect(row.layerBand).toBe('layered');
});
```

And a Go test asserting every emitted task carries a non-empty label:

```go
func TestGeneratedTasksAllCarryALabel(t *testing.T) {
	for _, task := range []MethodTask{
		TaskSRS, TaskSRSReview, TaskSTP, TaskSTPReview, TaskSomeConstruction,
		TaskDetailedDesign, TaskDesignReview, TaskConstruction, TaskTestClient,
		TaskCodeReview, TaskIntegration, TaskTesting,
	} {
		if LabelForTask(task) == "" {
			t.Errorf("task %q has no display label", task)
		}
	}
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestGeneratedTasksAllCarryALabel -v
cd ../webApp && node --test src/contracts/constructionAdapters.test.ts
```

- [ ] **Step 3: Implement**

```bash
cd webApp && npm install --save @mui/x-tree-view
```

Add `LabelForTask` beside the task constants in `projectstateaccess.go` (remember: no new files in that package). It is a new exported symbol, so add it to the encapsulation allowlist in `server/internal/arch_test.go` under the existing pure-derivation-helper category — **and write only claims you verify by grep**; name `cmd/gen-uiprofiles` as the consumer, because it is.

Emit `label` from `writePhasesConst` alongside `task`/`gate`/`conditional`, extend the `GeneratedTask` interface in `writeHeader`, then `GOWORK=off make gen-uiprofiles`.

- [ ] **Step 4: Verify**

```bash
cd server && GOWORK=off make gen-uiprofiles-check && GOWORK=off go test ./internal/ -run 'TestFileLayout|TestGeneratedOnlyPublic|TestNoBannedPhaseIdentifier' && GOWORK=off make lint
cd ../webApp && npx prettier --check src/components/construction/lifecycleTemplates.gen.ts && npm run typecheck && npm test
grep -c "label:" src/components/construction/lifecycleTemplates.gen.ts   # expect > 40
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(construction): tree-view dep, per-task labels, layer on the row

@mui/x-tree-view (MIT) for the three-tier list. Per-task display labels are
generated from the Go vocabulary rather than hand-authored in TS — the file
that would have carried them documents the last hand-mirror this codebase
paid for. layer/layerBand finally reach the SPA row; Stage A put them on the
wire and the mapper predated them.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: The shell — one route, a lens control, poll-proof selection

**Files:**
- Create: `webApp/src/components/construction/lens/ConstructionShell.tsx`, `lens/useLensSelection.ts`
- Modify: `webApp/src/routes/ConstructionConsole.tsx`
- Test: `webApp/src/components/construction/lens/useLensSelection.test.ts` *(new)*

**Interfaces:**
- Produces: `type LensId = 'list' | 'graph' | 'tasks'`; `useLensSelection()` returning `{ lens, selection, setLens, select, clear }` where `selection` is `{ activityId?, lifecyclePhase?, task?, attempt? }`; `<ConstructionShell>` rendering a toolbar, a lens control, a content slot and a detail slot.

The three tabs become three **lenses over one dataset**. The difference matters: tabs unmount the detail pane and drop selection on every switch, and this console polls at 1.5s while cascading, which already forced `NetworkView` to keep a module-level selection store. Tabs would multiply that.

Selection lives in URL search params — `?lens=list&a=<activityId>&p=<lifecyclePhase>&k=<task>&n=<attempt>` — so the detail pane never owns it, a remount cannot wipe it, and a link can address exactly one task attempt.

**Copy the `signatureOf()` + module-level store pattern from `NetworkView.tsx:106-128`.** This is a recorded, previously-fixed bug; re-introducing it wastes an operator's glance every 1.5 seconds.

- [ ] **Step 1: Write the failing test**

Test the hook's pure parts (param encode/decode and the signature guard) via `node:test`:

```ts
it('round-trips a full selection through search params', () => {
  const parsed = parseLensSearch({ lens: 'list', a: 'C-BG', p: 'construction', k: 'codeReview', n: '2' });
  expect(parsed.lens).toBe('list');
  expect(parsed.selection).toEqual({ activityId: 'C-BG', lifecyclePhase: 'construction', task: 'codeReview', attempt: 2 });
});

it('defaults to the list lens and an empty selection', () => {
  const parsed = parseLensSearch({});
  expect(parsed.lens).toBe('list');
  expect(parsed.selection).toEqual({});
});

it('rejects an unknown lens rather than rendering a blank surface', () => {
  expect(parseLensSearch({ lens: 'bogus' }).lens).toBe('list');
});

it('drops a non-numeric attempt rather than passing NaN downstream', () => {
  expect(parseLensSearch({ a: 'C-BG', n: 'x' }).selection.attempt).toBeUndefined();
});
```

- [ ] **Step 2: Run and confirm failure**

```bash
cd webApp && node --test src/components/construction/lens/useLensSelection.test.ts
```

- [ ] **Step 3: Implement**

`useLensSelection.ts` exports the pure `parseLensSearch` / `serializeLensSearch` plus the hook wrapping TanStack Router's search params. `ConstructionShell.tsx` renders: `ExperienceChrome` (unchanged) → a shared toolbar containing the `▤ LIST │ ⬡ GRAPH │ ⚑ TASKS` segmented control (Tasks carries a count badge — the only lens that asserts something is owed), search box, and scope/kind/layer/sort controls whose state persists across lenses → a content slot → the detail slot.

Register the route's search-param schema so a deep link validates. For this task, GRAPH and TASKS render an honest "Coming in a later stage" placeholder — do not stub them with fake content.

- [ ] **Step 4: Verify**

```bash
cd webApp && npm run typecheck && npx eslint src/ && npm test && npm run build
```

Then drive it — the app is running (server `:8888`, SPA `:5199`):

```bash
cd ../uitests && cat > /tmp/lens.mjs <<'EOF'
import { chromium } from '@playwright/test';
const b = await chromium.launch();
const p = await b.newContext({ viewport: { width: 1600, height: 1000 } }).then(c => c.newPage());
await p.goto('http://localhost:5199/project/archistrator/construction?lens=list&a=C-BG', { waitUntil: 'networkidle' });
await p.waitForTimeout(2000);
await p.screenshot({ path: '/tmp/lens.png', fullPage: true });
console.log('url after load:', p.url());
await b.close();
EOF
node /tmp/lens.mjs
```

The URL must still carry `lens=list&a=C-BG` after load — if the app rewrites or drops it, selection is not surviving and the detail pane will lose it too.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(construction): one route, three lenses, selection in the URL

Replaces the Tracker/Interventions/Artifacts tabs with a lens control over
one dataset. Selection lives in search params so the shared detail pane never
owns it and the 1.5s cascade poll's remount cannot wipe it — the bug
NetworkView already had to work around with a module-level store.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: The shared detail pane

**Files:**
- Create: `webApp/src/components/construction/detail/DetailPane.tsx`
- Test: `webApp/src/components/construction/detail/DetailPane.test.ts` *(new)*

**Interfaces:**
- Consumes: `useLensSelection` (Task 3).
- Produces: `<DetailPane selection={...} row={...} onClose>` with an invariant header, a body slot, and an invariant action bar.

Today clicking a node opens a 480px overlay `Drawer` that covers the graph you clicked from. The rewrite lays the pane out BESIDE the content: default 520px, resizable, collapsible, width remembered in `localStorage` (wrap the access in try/catch — it throws in some contexts). Below 1200px it degrades to the existing overlay `Drawer`; keep that code path.

**The header is invariant across every body**: breadcrumb (`activity › lifecycle phase › task · attempt N`) · state chip · provenance chip · attempt selector · exit criterion + Table A-1 weight.

**The action bar is invariant too**, and `↻ Run this task` is **present and enabled in EVERY state** — including `passed` (re-run) and `unknown` (run it for the first time). This is the founder's standing ruling that failure is never terminal, made structural rather than conditional. There is no state in which the retry affordance is absent or disabled.

For this task the body slot renders a placeholder; Tasks 9–11 fill it.

- [ ] **Step 1: Write the failing test**

```ts
it('enables the retry action in every task state', () => {
  for (const state of ['unknown', 'notStarted', 'running', 'awaitingHuman', 'passed', 'failed'] as const) {
    const actions = detailActionsFor(state);
    const retry = actions.find(a => a.id === 'run');
    expect(retry, `no retry action for ${state}`).toBeDefined();
    expect(retry!.disabled, `retry disabled for ${state}`).toBe(false);
  }
});

it('offers approve and send-back only where a human decision is owed', () => {
  expect(detailActionsFor('awaitingHuman').map(a => a.id)).toContain('approve');
  expect(detailActionsFor('passed').map(a => a.id)).not.toContain('approve');
});
```

- [ ] **Step 2: Run and confirm failure**

```bash
cd webApp && node --test src/components/construction/detail/DetailPane.test.ts
```

- [ ] **Step 3: Implement**

Export the pure `detailActionsFor(state)` alongside the component so the invariant is testable without rendering. Reuse `useTokens()`; no hardcoded colours; all five themes must keep working.

- [ ] **Step 4: Verify**

```bash
cd webApp && npm run typecheck && npx eslint src/ && npm test && npm run build
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(construction): shared detail pane laid out beside the content

Replaces the overlay drawer that covered the thing you clicked. Header and
action bar are invariant across every body, and 'Run this task' is present
and enabled in EVERY state — the always-retryable ruling made structural
rather than conditional.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: The tree derivation — activity › phase › task

**Files:**
- Create: `webApp/src/components/construction/list/activityTree.ts`
- Test: `webApp/src/components/construction/list/activityTree.test.ts` *(new)*

**Interfaces:**
- Consumes: `ConstructionRow`, `GENERATED_TEMPLATES`, `GENERATED_TESTING_VARIANTS`, `GeneratedPhase`, `GeneratedTask`.
- Produces: `buildActivityTree(rows, opts) → ActivityNode[]`, where `ActivityNode` has `phases: PhaseNode[]`, each with `tasks: TaskNode[]`, each with `attempts: TaskAttemptRow[]` and a derived `state`.

This is the heart of the lens and it is a **pure function** — no React, fully testable. Get it right and the components are presentation.

**The derivation, in order:**
1. Row set comes from the PROFILE: `GENERATED_TEMPLATES[row.kind]` (or `GENERATED_TESTING_VARIANTS[row.variant]` when `kind === 'testing'`). An **unclassified** row (`kind` absent) yields **zero** phases and zero tasks — never a default profile.
2. Each phase contributes its `weight`, `name` and `id`, plus its ordered `tasks`.
3. A **conditional** task (`conditional === true`) is emitted ONLY if `row.attempts` contains an attempt for it.
4. Each task's attempts are those in `row.attempts` with a matching `task`, sorted by `attempt` ascending. The task's own state comes from the LATEST attempt; earlier ones are `superseded`.
5. Task state: no attempts → `unknown`; latest `outcome === 'passed'` → `passed`; `'rejected'`/`'failed'` → `failed`; `''` (pending) → `running`.
6. Phase completion comes from `row.phases` (the server already derived it — do NOT re-derive from attempts here, or the two will drift; Stage A made the server the single authority).
7. A phase present in the profile but absent from `row.phases` is `unknown`, not incomplete. A phase in `row.phases` that the profile does not carry is a **data defect** — drop it and count it, do not render an extra row.
8. Activity `%` complete = Σ weights of completed phases (App A). If any phase is `unknown`, the percentage is `undefined`, not zero — an unknown denominator is not a zero numerator.

- [ ] **Step 1: Write the failing tests**

```ts
it('derives the row set from the profile, not from stored data', () => {
  const [node] = buildActivityTree([row({ activityId: 'C-x', kind: 'service', phases: [], attempts: [] })]);
  expect(node.phases.map(p => p.phase)).toEqual([
    'requirements', 'detailed_design', 'test_plan', 'construction', 'integration',
  ]);
  // Every task exists, every one unknown; conditional tasks suppressed.
  const tasks = node.phases.flatMap(p => p.tasks);
  expect(tasks.every(t => t.state === 'unknown')).toBe(true);
  expect(tasks.map(t => t.task)).not.toContain('someConstruction');
  expect(tasks.map(t => t.task)).not.toContain('testClient');
});

it('emits a conditional task once a real attempt exists for it', () => {
  const [node] = buildActivityTree([row({
    activityId: 'C-x', kind: 'service', phases: [],
    attempts: [attempt({ task: 'someConstruction', attempt: 1, outcome: 'passed' })],
  })]);
  const dd = node.phases.find(p => p.phase === 'detailed_design')!;
  expect(dd.tasks.map(t => t.task)).toContain('someConstruction');
});

it('gives an unclassified activity no phases and no tasks', () => {
  const [node] = buildActivityTree([row({ activityId: 'C-AA', classified: false, phases: [], attempts: [] })]);
  expect(node.phases).toEqual([]);
  expect(node.unclassified).toBe(true);
});

it('shows a retried task as one row whose state is the LATEST attempt', () => {
  const [node] = buildActivityTree([row({
    activityId: 'C-x', kind: 'service', phases: [],
    attempts: [
      attempt({ task: 'designReview', attempt: 1, outcome: 'rejected' }),
      attempt({ task: 'designReview', attempt: 2, outcome: 'passed' }),
    ],
  })]);
  const dd = node.phases.find(p => p.phase === 'detailed_design')!;
  const dr = dd.tasks.find(t => t.task === 'designReview')!;
  expect(dr.attempts).toHaveLength(2);
  expect(dr.state).toBe('passed');
  expect(dr.attempts[0].superseded).toBe(true);
  expect(dr.attempts[1].superseded).toBe(false);
});

it('selects the latest attempt by number, not by array position', () => {
  const [node] = buildActivityTree([row({
    activityId: 'C-x', kind: 'service', phases: [],
    attempts: [
      attempt({ task: 'codeReview', attempt: 2, outcome: 'passed' }),
      attempt({ task: 'codeReview', attempt: 1, outcome: 'rejected' }),
    ],
  })]);
  const cr = node.phases.find(p => p.phase === 'construction')!.tasks.find(t => t.task === 'codeReview')!;
  expect(cr.state).toBe('passed');
});

it('uses the variant profile for a testing activity', () => {
  const [node] = buildActivityTree([row({ activityId: 'N-IT', kind: 'testing', variant: 'systemTest', phases: [], attempts: [] })]);
  expect(node.phases.map(p => p.name)).toEqual(['Smoke Pass', 'Use-Case Execution', 'Regression & Sign-off']);
});

it('reports percent complete as undefined while any phase is unknown', () => {
  const [node] = buildActivityTree([row({ activityId: 'C-x', kind: 'service', phases: [], attempts: [] })]);
  expect(node.percentComplete).toBeUndefined();
});

it('drops a stored phase the profile does not carry and counts it as a defect', () => {
  const [node] = buildActivityTree([row({
    activityId: 'G-SPA', kind: 'uiDesign',
    phases: [
      phaseRow({ phase: 'requirements', weight: 40, completed: false }),
      phaseRow({ phase: 'detailed_design', weight: 60, completed: true }),
      phaseRow({ phase: 'construction', weight: 40, completed: true }), // not in the uiDesign profile
    ],
    attempts: [],
  })]);
  expect(node.phases).toHaveLength(2);
  expect(node.offProfilePhaseCount).toBe(1);
});
```

- [ ] **Step 2: Run and confirm failure**

```bash
cd webApp && node --test src/components/construction/list/activityTree.test.ts
```

- [ ] **Step 3: Implement**

Pure module, no React import. Keep the phase/task ordering exactly as the generated data gives it — that ordering is Method order and execution order respectively, and re-sorting it would be a hand-mirror.

- [ ] **Step 4: Verify**

```bash
cd webApp && npm run typecheck && npx eslint src/ && node --test src/components/construction/list/activityTree.test.ts
```

Then run it against the real 69 rows and eyeball the shape:

```bash
cat > /tmp/tree-probe.mjs <<'EOF'
const res = await fetch('http://localhost:8888/api/v1/system-design/get-project/archistrator');
const rows = (await res.json()).ActivityConstruction;
console.log('rows', Object.keys(rows).length);
console.log('unclassified', Object.values(rows).filter(r => !r.classified).length);
console.log('with attempts', Object.values(rows).filter(r => (r.attempts||[]).length).length);
EOF
node /tmp/tree-probe.mjs
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(construction): derive the three-tier activity tree

Pure derivation: the row set comes from the server-generated profile, never
from stored data, so an activity with no history still expands into a
complete, correct, quiet Method skeleton. Only state is unknown.

Conditional tasks appear only once a real attempt exists. A retried task is
ONE row whose state is the latest attempt by number, not by array position.
An unclassified activity gets no phases at all.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: Render the tree

**Files:**
- Create: `webApp/src/components/construction/list/ActivityTreeView.tsx`
- Modify: `webApp/src/components/construction/lens/ConstructionShell.tsx`

**Interfaces:**
- Consumes: `buildActivityTree` (Task 5), `useLensSelection` (Task 3), `useTokens`.
- Produces: `<ActivityTreeView nodes onSelect selection>`.

`RichTreeView` + `slots.item` with a custom `TreeItem` wrapping our own CSS grid — the tree is *derived*, so a data-driven `items` prop is the right shape. `apiRef` drives search-reveal and expand-to-current-phase. Selection is controlled and mirrored into the URL.

**Channels — magnitudes get geometry, states get at most one chip** (the recorded diagnosis from two rejected prototype rounds: *"every magnitude gets a geometric channel, nothing with a geometry gets a chip"*):

| Magnitude | Channel |
|---|---|
| effort days | left bar length |
| float | rail band (`bandTokens`) **plus an always-visible numeral** — WCAG 1.4.1, colour is never the sole carrier |
| on critical path | border weight 2→3px, never a "CRITICAL" chip |
| phase weight | segment width |
| % complete | fill extent |
| retry count | `↻N` numeral + doubled stroke |

| State | Channel | Chip? |
|---|---|---|
| `unknown` | hairline dashed outline, muted, no fill | **no** — chip-less IS the signal |
| `absent` (not in this profile) | gap, 40% opacity, struck label | no |
| `notStarted` | hollow circle | no |
| `running` | teal dot — the ONLY animated element on screen | yes (xs) |
| `awaitingHuman` | `awaitingBg`/`awaitingFg` + 3px accent left edge — loudest thing on screen | yes |
| `passed` | olive check | yes (xs) |
| `failed` | `dangerFg` on `awaitingBg` **+ inline `↻ Retry`** | yes |
| `superseded` | 55% opacity + `↻n` index | no |

`failed` is amber-backed with a red foreground — "needs you", never "dead".

**Tier 2 renders as a group RULE, not a card** (`┌ PHASE NAME ···· wt N · exit: <criterion>`), so perceived depth stays 2 despite three tiers. Exit-criterion prose comes from `EXIT_CRITERIA` in `lifecycleTemplates.ts`.

**Attempts collapse into the `↻N` counter** by default; the row shows the latest attempt's state. Clicking the counter expands attempts newest-first.

At rest: tier-1 rows only, all collapsed.

- [ ] **Step 1: Write the failing test**

Test the presentation-logic helpers, not the DOM:

```ts
it('never renders a chip for the unknown state', () => {
  expect(chipFor('unknown')).toBeUndefined();
});
it('marks awaitingHuman as the loudest state', () => {
  expect(emphasisRank('awaitingHuman')).toBeGreaterThan(emphasisRank('running'));
  expect(emphasisRank('awaitingHuman')).toBeGreaterThan(emphasisRank('failed'));
});
it('always offers an inline retry on a failed task', () => {
  expect(inlineActionsFor('failed')).toContain('retry');
});
it('renders float as a numeral, not only a colour band', () => {
  expect(floatPresentation(6).numeral).toBe('6');
});
```

- [ ] **Step 2: Run and confirm failure**

- [ ] **Step 3: Implement**

Reuse `construction/status.tsx` (extend its union; invent no colours), `KindBadge`, `bandTokens`, `primitives/RecordTable` where a table fits. No hardcoded colours anywhere.

- [ ] **Step 4: Verify — and this is a founder review checkpoint**

```bash
cd webApp && npm run typecheck && npx eslint src/ && npm test && npm run build
```

Then drive the live app and screenshot at rest AND with one activity expanded to tasks. Read the screenshots back with the Read tool and check them against the channel table above before reporting.

**STOP after this task and report to the controller with the screenshots.** Per the founder's standing UI review loop, each rendered lens gets a review checkpoint before the next is built.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(construction): render the three-tier activity tree

Magnitudes get geometry, states get at most one chip, and 'unknown' gets no
chip at all — chip-less IS the signal, and unknown is the majority state.
Float carries a numeral beside its band so colour is never the sole carrier.
Retries collapse into a counter rather than exploding the row count.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: Provenance as an orthogonal axis

**Files:**
- Create: `webApp/src/components/construction/provenance.tsx`
- Modify: `webApp/src/components/project/computed.tsx`, `list/ActivityTreeView.tsx`

**Interfaces:**
- Produces: `<ReconstructedBadge>`, `provenanceRailFor(origin)`, `worstOriginOf(node)`.

Provenance is **not a state** — every row carries both. `recorded` gets **nothing** (the default is the absence of a mark). `reconstructed` gets a 3px hatched left rail using the existing `scan()` texture helper. `unknown` gets the dashed outline.

**Two rules make this survivable at ~528 potential rows:**
1. **Stamp the GROUP, not the row.** The `≈ RECONSTRUCTED` badge lives on the tier-1 and tier-2 headers; rows inherit only the hatched rail. A screen of 300 chips reads as damage; a consistent hatch on a rail reads as a *material*.
2. **Sub-grade in the tooltip, not on screen.** `backfilled` and `inferred` share the hatch; the tooltip names which, and an inferred one cites the input it guessed from.

**Never use colour for provenance** — colour is committed to the status and float channels, and a colour swap would read as a status change.

`ReconstructedBadge` joins `ComputedBadge`/`AuthoredBadge` in `project/computed.tsx` — a third member of an existing family, not a new language.

**Contagion:** a node's provenance is the WORST among its descendants. A parent must carry the badge if any descendant attempt is synthesized — a tree must not hide a fake behind a collapsed node.

- [ ] **Step 1: Write the failing test**

```ts
it('takes the worst origin among descendants', () => {
  expect(worstOriginOf(nodeWith(['observed', 'backfilled']))).toBe('backfilled');
  expect(worstOriginOf(nodeWith(['observed', 'backfilled', 'synthesized']))).toBe('synthesized');
});
it('reports unknown, not observed, for a node with no attempts at all', () => {
  expect(worstOriginOf(nodeWith([]))).toBe('unknown');
});
it('never expresses provenance through colour', () => {
  expect(provenanceRailFor('backfilled').texture).toBeDefined();
  expect(provenanceRailFor('backfilled').color).toBeUndefined();
});
```

Note the second test: this is where Stage A's `worstOrigin`-absent-means-no-ledger decision pays off. An empty ledger is `unknown` here, never `observed`.

- [ ] **Step 2–5:** Run, implement, verify (`typecheck`, `eslint`, `npm test`, `build`), commit as `feat(construction): provenance as a hatched rail, stamped on the group`.

---

### Task 8: The unknown body — the majority surface

**Files:**
- Create: `webApp/src/components/construction/detail/bodies/UnknownBody.tsx`, `AbsentBody.tsx`

This body will be shown **more than all the others combined**, so it gets designed as a feature rather than an empty state. Calm, informative, one action. No error tone, no red, no spinner, no "something went wrong".

It teaches the Method while it waits:

```
▫  DESIGN REVIEW
   No record. This task has not run, or it ran before per-task
   history was captured.

   WHAT IT IS   The architect reviews the detailed design before
                construction begins (Figure A-1).
   EXIT         Detailed design is approved.
   WEIGHT       part of Detailed Design · 20% of this activity.
   RETRY RULE   A failing review repeats Detailed Design.

                    [ ↻ Run this task ]
```

A **distinct sibling** for `absent` (a phase the profile does not carry): *"Deployment activities carry no Test Plan phase. This is by design, not missing data."* Do not conflate the two — one is missing data, the other is a correct absence.

Task metadata (what it is, its exit criterion, its weight) comes from the generated vocabulary + `EXIT_CRITERIA`, never hand-authored per task.

- [ ] Steps 1–5 as established: test that the body renders an enabled retry in the unknown state and that `absent` gets the sibling copy (not the unknown copy); implement; verify; commit as `feat(construction): design the unknown body as a feature, not an empty state`.

---

### Task 9: The episode body

**Files:**
- Create: `webApp/src/components/construction/detail/bodies/EpisodeBody.tsx`

Reuse `components/episodes/EpisodesPanel.tsx` + `EpisodeTimeline.tsx` **verbatim** — they already render outcome, duration, model, workerClass, the 4-way token split with its "tokens (main loop)" caption, turns, cost, tool counts, subagent spans and the lineage tree. Add a subagent-span gantt strip (spans carry `startedAt`/`endedAt`).

**Required honesty caption.** Stage A made `TargetRef` carry the attempt key for NEW episodes, but every episode written before it carries the bare activity id, and capture itself is still deferred. So the panel header must read:

> `EPISODES · N — activity-level; episodes written before this release are not attributable to a specific task`

Do NOT imply these N episodes belong to the selected task unless the episode's `TargetRef` actually matches the selected `attemptId`. When it does match, say so and show only those.

- [ ] Steps 1–5: test that an episode whose `TargetRef` is a bare activity id is labelled unattributed while one matching the attempt key is labelled attributed; implement; verify; commit.

---

### Task 10: The review and artifact bodies

**Files:**
- Create: `webApp/src/components/construction/detail/bodies/ReviewBody.tsx`, `ArtifactBody.tsx`

**Review body** — artifact above, verdict below. The artifact pane is the same renderer the artifact body would use, read-only, with `CommentableList`-style anchors armed into `CommentProvider` (`setAnchor`) so send-back carries item-granular comments; that mechanism already works end to end. Verdict = per-reviewer rows (role via `RoleAvatar`, `PASS`/`PASS-WITH-NOTES`/`FAIL`, notes), from `phaseGateSession.view.reviewSet` when live.

**Construction review verdicts are a known dropped signal** — `awaitPhaseDecision` never dereferences `sig.Feedback`, so no structured verdict exists. Where the only surviving text is prose in `activityConstruction[].produced[].Note`, render it stamped `≈ reconstructed from the produced-record note` and **never present it as a structured verdict**.

**Artifact body** — dispatch on the EXISTING `classify(row)` from `artifactClassification.ts`, which already returns the right key:

| Kind | Renderer |
|---|---|
| `service` | `ServiceContractView` → `ContractCodeFlow` (the code-level diagram) + `ContractComponentFlow` + `ContractRevisionHistory` |
| `uiDesign` | `FrontendArtifactView` (the UI spec) |
| `testing:*` | `TestPlanView` / `ScenarioBrowser` / `DynamicViewFlow` (the test dynamic diagrams) |
| `frontend` | `FrontendArtifactView` |
| everything else | the unknown body from Task 8 — **cut for this stage** |

All three artifact kinds the founder named already have renderers. Ship those three; `deployment`/`documentation`/`integration` fall back to the unknown body per the cut list.

- [ ] Steps 1–5: test the dispatch picks the right renderer per kind and falls back honestly for the cut kinds; test the produced-note verdict is stamped reconstructed; implement; verify; commit.

---

### Task 11: Navigability — scope, search, sort, expand

**Files:**
- Modify: `lens/ConstructionShell.tsx`, `list/ActivityTreeView.tsx`

No DataGrid means no free sort/filter/virtualization. Design them explicitly:

- **Never offer "Expand all"** — that is the 528-row trap. Offer **"Expand to current phase"**, which opens only the in-flight phases (today: 1–3 rows).
- **Scope chips** (shared toolbar, apply to every lens): `All · Critical path · Near-critical · Awaiting me · In flight · Has retries · Reconstructed only · Unknown only`. `Critical path`/`Near-critical` carry over from the existing tracker filter bar — same vocabulary, promoted.
- **Kind** filter reuses `KindBadge`; **Layer** filter uses `row.layerBand`/`row.layer` (Task 2) and also drives Stage D.
- **Search** matches activity id, title and componentId; a tier-3 match auto-expands its ancestors via `apiRef` and highlights.
- **Sort applies to tier 1 ONLY** — `Network order (default) · Float ascending`. Tiers 2 and 3 are a *sequence*; sorting them is nonsense and is not offered. Say so in the sort menu's helper text. (Effort/last-event/most-retries are cut.)
- **The "hide synthesized" toggle**, default off. This is the wave's acceptance test: with it ON, the lens must render without crashing and show approximately **one** activity with lifecycle data. If more appear, something is fabricating.
- **No virtualization this stage.** Resting DOM is 69 tier-1 rows; worst realistic expansion ~120. Earmark windowing above 150.

- [ ] Steps 1–5: test each scope chip's predicate against a fixture including an unclassified row, a reconstructed row and a retried row; test "expand to current phase" opens only in-flight activities; implement; verify; commit.

---

### Task 12: The 40-vs-69 seam, made visible

**Files:**
- Create: `webApp/src/components/construction/list/CoverageStrip.tsx`
- Modify: `list/ActivityTreeView.tsx`

A persistent strip above the tree:

```
COVERAGE  40 derived · 31 mapped · 9 cross-cutting ‖ 69 legacy · 9 reconcile · 60 orphaned ⚠
```

The orphaned legacy records get a bottom tree group `▸ LEGACY RECORDS · UNRECONCILED`, muted, hatched, **read-only** (not selectable for actions, but drillable for reading). They are never blended into the derived set and never deleted from view.

Per founder decision D8 these are kept **only as context while the derived activities are brought into good shape, and deleted once that is done** — so the group's copy must say that, and the deletion is a separate future commit, not this one.

Derive the numbers; do not hardcode them. They will change.

- [ ] Steps 1–5: test the counts are computed from the row set (feed a fixture with a different split and assert the strip follows); test legacy rows are non-selectable; implement; verify; commit.

---

### Task 13: Retire the old tabs

**Files:**
- Modify: `webApp/src/routes/ConstructionConsole.tsx`
- Delete: the tab-shell code paths superseded by the lens shell

Remove the `TabId = 'tracker' | 'interventions' | 'artifacts'` shell and its tab bar. **Do NOT delete** the components the later lenses still need: `InterventionQueue`, `PolicyPanel`, `PhaseGatePanel`, `InterventionDrawer` (Stage C), `NetworkView` (Stage D reference), the artifact renderers (Task 10 uses them), or `EvTrackingChart`/`HeadStateRollup`/`NearCriticalFloat` — decide per component and say which you kept and why in your report.

Anything genuinely dead after the lens shell lands should go; anything a later stage needs stays with a comment naming the stage.

- [ ] Steps 1–5: verify no orphaned imports (`npm run typecheck` + `npx eslint`), the full suite passes, the build is clean, and drive the live app one final time; commit as `refactor(construction): retire the tab shell`.

---

## Stage B exit criteria

```bash
cd server
GOWORK=off go build ./... && GOWORK=off go test ./... 2>&1 | grep -v "^ok\|no test files"
GOWORK=off make lint
GOWORK=off make gen-models-check gen-client-check gen-uiprofiles-check derived-plan-check
GOWORK=off go test ./internal/ -run 'TestFileLayout|TestGeneratedOnlyPublic|TestRepoStructureCmdIsClosed|TestNoBannedPhaseIdentifier'
cd ../webApp
npm run typecheck && npx eslint src/ && npm test && npm run build
```

Then, against the live app:

1. **The synthesized toggle.** With "hide synthesized" ON, the list renders without crashing and shows approximately **one** activity with lifecycle data.
2. **Unclassified is visible and empty.** The 23 unclassified activities render as Unclassified with **zero** phase and task sub-rows.
3. **No evidence, no claim.** The ~20 classified-but-evidence-less activities show no build-status chip (Task 1).
4. **The skeleton is always there.** Every classified activity expands into its full profile's phases and tasks, dashed and stateless where unknown.
5. **Retries are one row.** An activity with a retried task shows one task row with `↻2`, expanding to two attempts newest-first.
6. **Selection survives the poll.** Select a task, wait 5 seconds through at least three cascade polls, confirm selection and detail pane are unchanged.
7. **Retry is never absent.** `↻ Run this task` is present and enabled in every state, in every body.
8. **Playwright-driven and ux-reviewed** per the founder's standing loop, with a STOP checkpoint after Task 6.

## Earmarks carried from Stage A (do not lose)

- Per-project `worstOrigin` does not exist; spec AC 2 (EV header shows "—") is unimplementable until it does — needs a contract change.
- `RecordTaskAttempt` was never implemented; there is no write path to the ledger.
- Episode attempt numbers are workflow-local, so a re-dispatched activity re-emits `…:task:1`. Must seed from the durable ledger once `RecordTaskAttempt` exists.
- `evidenceFromRow` in `backfill-attempts` can learn a new produced kind and keep every test green — close if the tool stops being one-shot.
- Six rows reference contract files whose components no longer have a `.serviceContracts` entry (`settlementEngine` renamed, `handOffEngine` cut).
- The legacy-60 group is deleted once the derived 40 are in good shape (founder D8).
