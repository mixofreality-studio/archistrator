# Commentability + Accessibility pass — webApp (System / Project Design / Construction)

**Date:** 2026-07-04
**Status:** Approved (design + scope), implementation in progress
**Driver:** founder request — comment on any artifact-slot item at item granularity (never sub-item), keyboard-navigate list items, prefer the single "Comment on this item" button (the core-use-cases UX) over right-click/drag-select.

Grounded in three live UX/a11y reviews (System, Project Design, Construction) run against the running app at `http://localhost:5173`.

## Decisions (locked)

- **Granularity:** one anchor per artifact *item* (glossary term, scrubbed requirement, business objective, operational decision, standard-check row, activity, solution option, contract op, test step). Never sub-item / word-level.
- **Primary affordance:** a discoverable per-item "Comment on this item" button (mirrors `UseCaseCarousel`). Diagram surfaces keep click/right-click as a *secondary* path but must also expose a visible, arm-confirming button.
- **Keyboard:** list renderers become roving-tabindex `listbox`/`option`: `↑↓` + Home/End move focus, focused row `aria-selected`, Enter or `c` fires its comment.
- **Drag-select:** button-only for all itemized lists. Keep a **clamped** drag-select **only** for genuine narrative prose (Mission Vision/Statement, SDP recommendation), always paired with a per-paragraph button.
- **Phase 3 depth:** build the full comment channel (Construction mounts `CommentProvider` but wires no ChatRail/submission) — collect armed comments and submit them into the phase-gate / intervention decision path.

## Root-cause bugs to fix (found live, independent of the feature)

1. **`proseAnchor(kind, source)` collapses to a document-wide anchor** — `source` is the whole-artifact title (`ArtifactRenderer.tsx:86`), so every Mission/Scrubbed/StandardCheck/OpConcepts comment resolves to the *same* JSONPath. Fix: real per-section/per-item locators.
2. **`SelectionPopover` has no block clamp** — a drag spanning items yields one Frankenstein anchor. Fix: clamp the range to its nearest commentable block; only genuine-prose hosts carry `data-commentable`.
3. **Dead `NodeToolbar` comment buttons** — `C4Node`, `ActivityNode` render `NodeToolbar isVisible={selected}` but the flows never wire `selected`. Fix: wire controlled selection (Architecture Static/Perspective, Full-diagram use cases).
4. **Silent arming** — Architecture edge-click and Deployment node-click call `setAnchor` with no visual confirmation. Fix: visible armed chip / two-step confirm.
5. **Armed anchor persists across step navigation** — stale anchor bleeds onto the next artifact. Fix: clear on step change.
6. **Keyboard traps / mouse-only disclosures:** SDP `OptionCard` (the plan-of-record gate — **priority**), Activity-List accordion headers, PolicyPanel disclosure, Construction 66-activity rail rows, ContractCodeFlow op rows. Fix: real roles + keyboard handlers.
7. **Fake tables** (`display:contents` grids in RiskModel, SdpReview, NearCriticalFloat) → semantic `<table>`/roles.
8. **Unlabeled chart SVGs** (`BandedScatter`, `EvTrackingChart`) → `role="img"` + computed `aria-label`.
9. **Drawer/panel naming** (`InterventionDrawer`, `ActivityLifecyclePanel`) → `aria-labelledby`.

## Foundation (shared) — build first

### `components/comments/CommentableList.tsx` (new)
Roving-tabindex list primitive. API:
```tsx
<CommentableList
  ariaLabel="Business objectives"
  items={objectives}
  getKey={(item, i) => `obj-${i}`}
  getAnchor={(item, i) => Anchor}     // arms via useComments().setAnchor
  renderItem={(item, i) => ReactNode}
/>
```
Internals: `role="listbox"` host; each row `role="option"`, `aria-selected` on the focused row, roving `tabIndex` (0 on focused, −1 else); `↑↓`/Home/End move focus; Enter/`c` fire the row's comment; per-row "Comment on this" `IconButton` always in DOM, visible on hover/focus. Uses theme tokens.

### `components/comments/CommentContext.tsx`
- Add item anchor builders (see scheme). Keep existing ones.
- Fix `proseAnchor` to accept a real section id (vision/mission/recommendation), not the artifact title.
- Add `resetAnchor()` and call it on step navigation from the experience hosts.

### `components/comments/SelectionPopover.tsx`
- Clamp selection to nearest `[data-commentable]` block; ignore ranges crossing block boundaries.
- Only genuine-prose hosts set `data-commentable`; list renderers drop it.

### Anchor scheme (roots at `$` = typed model)
| kind | builder | JSONPath |
|---|---|---|
| mission objective | `missionObjectiveAnchor(n)` | `$.objectives[n]` |
| mission prose | `proseAnchor('mission', 'vision'|'mission')` | `$.vision` / `$.mission` |
| glossary item | `glossaryItemAnchor(n)` | `$.items[n]` |
| scrubbed req | `scrubbedRequirementAnchor(n)` | `$.items[n]` |
| standard-check row | `standardCheckItemAnchor(n)` | `$.items[n]` |
| operational decision | `operationalDecisionAnchor(n)` | `$.decisions[n]` |
| planning flag | `planningFlagAnchor(n)` | `$.notes.flags[n]` |
| activity (P2) | `activityAnchor(name)` | `$.activities[name=…]` |
| solution knob/rate | `solutionAnchor(kind, knob)` | `$.options[kind=…]…` |
| risk option row | `riskModelRowAnchor(kind)` | `$.options[kind=…]` |
| sdp option | `sdpOptionAnchor(kind)` | `$.options[kind=…]` |
| construction activity | `activityConstructionAnchor(id)` | `$.activityConstruction[id=…]` |
| intervention | `interventionAnchor(id)` | `$.interventions[id=…]` |
| contract op | `contractOpAnchor(component, sig)` | `$.serviceContracts[component=…].ops[signature=…]` |
| test step | `testScenarioStepAnchor(scenario, case, seq)` | `$.scenarios[…].cases[…].steps[seq]` |

## Per-phase rollout

**Phase 1 (System):** Mission → dedicated typed view (objectives via `CommentableList`, Vision/Statement prose + per-paragraph button + clamped select); Glossary/Scrubbed/StandardCheck/OpConcepts-decisions → `CommentableList`; wire Architecture Static/Perspective + Full-diagram use-case node selection so existing toolbars fire; edge/deployment arm-confirmation; Volatilities list-grouping polish.

**Phase 2 (Project Design):** thread `useComments` into every renderer; PlanningAssumptions flags/stats buttons; ActivityList accordion header a11y + rows via `CommentableList`; Solutions knob/rate buttons; RiskModel + SdpReview per-option comments + **semantic tables** + **SDP radiogroup keyboard fix** + chart aria; Network node focus-ring (+ optional list fallback); clear stale anchor on nav.

**Phase 3 (Construction) — includes new comment channel:** add a comment composer/rail surface bound to the active gate/intervention so armed comments have a home and submit into the decision reason path; ArtifactActivityList rail → keyboard + per-row comment; wire `ScenarioBrowser` `onCommentStep`; ContractCodeFlow op-row keyboard + comment; NearCriticalFloat/RecordTable semantic tables; EvChart aria; Drawer/panel `aria-labelledby`.

## Verification (no test runner exists)
- `npm run check` (tsc + eslint + prettier) and `npm run build` clean.
- Live browser (Claude-in-Chrome) against `localhost:5173` real state: for each phase, confirm keyboard nav (`↑↓`/Home/End/Enter) reaches and comments each item, comment button arms the right anchor (assert via the `data-anchor-path` test probe in `CommentProvider`), and no regressions. Capture screenshots/GIF for the final review.
- Present one consolidated diff for founder review (per the agreed "implement all, review at end").
