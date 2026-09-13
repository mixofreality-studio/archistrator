/**
 * construction-tracker.spec — the Construction console's LIST lens
 * (route `/project/$projectId/construction`, the default lens; the console
 * is the lens shell directly since Task 13 retired the Tracker/Interventions/
 * Artifacts tab bar that used to wrap it).
 *
 * Exercises the OBSERVABLE half of the Method core use case "Execute a
 * Construction Activity" (a busMessage-triggered flow: an eligible activity
 * is dispatched, a branch/PR opens, the run is observed, exit criteria are
 * checked — see .coreUseCases in project.json). The DISPATCH half needs a
 * live build cluster (R-CPR, claude-code-action against real GitHub Actions)
 * that is not provisioned in this harness — see ConstructionConsole.tsx's own
 * doc comment ("gated on a build cluster... not provisioned here, so the
 * session is usually quiet"). Clicking "Begin construction" against a shared
 * dev server would have real, uncontrolled side effects (real job dispatch),
 * so this spec does not click it.
 *
 * What IS exercisable today, against the real committed head-state: opening
 * the console, selecting a real CPM activity node, and reading its shared
 * detail pane (breadcrumb, state chip, action bar — Stage B Task 4) — the
 * "Observe run; validate against exit criteria" step of the same use case,
 * driven by real committed ActivityConstruction data (this repo dogfoods its
 * own construction phase — see the "archistrator" project's committed
 * .activityConstruction). The default viewport here is well above the
 * pane's 1200px breakpoint, so this exercises the BESIDE-CONTENT layout, not
 * the narrow-viewport overlay Drawer (DETAIL_DRAWER, still kept for <1200px
 * — see DetailPane.tsx).
 *
 * Gated like artifact-systemtest.spec: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy. No live drafting needed.
 *
 * THE SELECTED ROW: C-construction-manager — a live derived activity (the
 * committed activity list is deterministic, so the id is stable) whose
 * attempts were backfilled from code evidence. Every derived activity is an
 * ordinary, selectable row now that the legacy seam is gone. A real gap noted
 * in passing, still open for whoever next touches activityScope.ts: its
 * search-reveal auto-expand (`matchingTaskIds`) only fires on a TASK-field
 * match, not an activity-id-only one.
 */
import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';
import { tagUseCase } from './support/useCases.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

test('the Tracker renders the activity tree and an activity row opens its shared detail pane', async ({
  page,
}) => {
  tagUseCase('execute-a-construction-activity');

  await gotoApp(page, '/project/archistrator/construction');
  // The lens shell IS the console (Task 13 retired the tab bar) — LIST is the
  // default lens, no click needed. The toolbar itself proves the console
  // mounted the shell directly, with no tab bar around it.
  await expect(page.getByTestId(TESTID.constructionLensToolbar)).toBeVisible();
  // Stage B Task 6: the LIST lens's body is the three-tier activity tree, not
  // the CPM graph (which returns under the GRAPH lens in Stage D). Every row
  // carries a published per-node testid, so no class-name escape hatch is
  // needed any more.
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible();

  const firstRow = page.getByTestId(TESTID.constructionListRow('C-construction-manager'));
  await expect(firstRow).toBeVisible({ timeout: 15_000 });
  await firstRow.click();

  const panel = page.getByTestId(TESTID.constructionDetailPane);
  await expect(panel).toBeVisible();
  // The narrow-viewport overlay Drawer must NOT be the one that rendered —
  // at this viewport width the pane sits BESIDE the content instead (the
  // whole point of Task 4: the old Drawer's modal backdrop used to cover the
  // toolbar's right end while a selection was open).
  await expect(page.getByTestId(TESTID.constructionDetailDrawer)).toHaveCount(0);
  // The toolbar's own controls stay reachable — the exact regression Task 4
  // fixes (the old overlay Drawer's backdrop covered the toolbar's right end
  // while a selection was open).
  await expect(page.getByTestId(TESTID.constructionLensKind)).toBeVisible();

  // The breadcrumb + action bar (with the always-present retry action — disabled
  // with its reason until the console can start work, designer P1-2) always
  // render once a node is selected — real content, not a stub, proving the click
  // drove a genuine activity selection against committed data.
  await expect(page.getByTestId(TESTID.constructionDetailBreadcrumb)).toBeVisible();
  const runAction = page.getByTestId(TESTID.constructionDetailActionRun);
  await expect(runAction).toBeVisible();
  await expect(runAction).toBeDisabled();
  await expect(runAction).toHaveAttribute('data-reason', /not wired/i);
});

/**
 * Review round 1 (Task 4): the pane's action bar must stay reachable at every
 * scroll position, and the MECHANISM that keeps it there must be the one the
 * code claims.
 *
 * This is the one property in this surface no other gate can see — typecheck,
 * eslint and the whole node:test suite stayed green while the action bar sat
 * thousands of pixels below the fold, and stayed green again when a dead
 * `alignSelf` was credited with fixing it. So it is asserted here, live, in the
 * only place that can actually measure it.
 *
 * What is measured, and why each part matters:
 *
 *   - `construction-lens-detail` (the shell's wrapper) IS stretched tall by the
 *     content row. That is LOAD-BEARING, not a bug: `position: sticky` can only
 *     travel within its containing block, so the tall wrapper is exactly what
 *     gives the pane room to stay pinned for the whole scroll. Asserted so that
 *     "fixing" the stretch — e.g. making the wrapper `display:flex` +
 *     `alignSelf:'flex-start'` — fails here instead of silently un-pinning the
 *     pane.
 *   - `construction-detail-pane` is MUCH shorter than that wrapper: it takes no
 *     explicit height and is capped by `maxHeight`, so it is sized by its own
 *     content rather than by the column beside it.
 *   - the run action stays fully inside the viewport at BOTH ends of the scroll
 *     range, and holds a fixed y once sticky has engaged.
 *
 * The content column needs to be genuinely tall for this to be a real scroll
 * rather than a vacuous one — the resting tree (one collapsed row per derived
 * activity) is not reliably taller than the viewport, so this first searches
 * "srs review", whose reveal expands every matched service activity's
 * Requirements phase, purely to lengthen the page under test. The selected row
 * (C-construction-manager) is itself one of the matches, so it stays on screen.
 */
test('the detail pane stays pinned beside content, so its action bar survives a long scroll', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1600, height: 900 });
  await gotoApp(page, '/project/archistrator/construction');

  // See the doc comment above: the search only lengthens the page.
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });
  await page
    .getByTestId(TESTID.constructionLensSearch)
    .getByRole('textbox')
    .fill('srs review');
  await page.waitForTimeout(600);

  const firstRow = page.getByTestId(TESTID.constructionListRow('C-construction-manager'));
  await expect(firstRow).toBeVisible({ timeout: 15_000 });
  await firstRow.click();

  const runAction = page.getByTestId(TESTID.constructionDetailActionRun);
  await expect(runAction).toBeVisible();

  const wrapper = await page.getByTestId(TESTID.constructionLensDetail).boundingBox();
  const pane = await page.getByTestId(TESTID.constructionDetailPane).boundingBox();
  expect(wrapper).not.toBeNull();
  expect(pane).not.toBeNull();
  // The pane is sized by its own content, not by the wrapper it sits in. A
  // generous margin: the point is orders of magnitude, not a pixel count.
  expect(pane!.height).toBeLessThan(wrapper!.height / 2);
  // ...and it never exceeds what fits below the sticky lens toolbar.
  expect(pane!.height).toBeLessThanOrEqual(900);

  // The console does NOT scroll the window — it scrolls an inner container, so
  // `page.mouse.wheel` at the viewport centre moves nothing an assertion can
  // see (over the old CPM canvas it was swallowed for pan/zoom outright), and
  // an assertion built on it would pass vacuously. Drive the real scroller, and
  // prove it actually moved before believing anything measured after it.
  const scrollTo = async (top: number): Promise<number> =>
    page.evaluate((t) => {
      for (const el of Array.from(document.querySelectorAll('*'))) {
        const style = window.getComputedStyle(el);
        if (el.scrollHeight > el.clientHeight + 4 && /(auto|scroll)/.test(style.overflowY)) {
          el.scrollTop = t === -1 ? el.scrollHeight : t;
          return el.scrollTop;
        }
      }
      return -1;
    }, top);

  const atTop = await scrollTo(0);
  expect(atTop).toBe(0);
  await page.waitForTimeout(250);
  const boxAtTop = await runAction.boundingBox();

  const atBottom = await scrollTo(-1);
  // The scroll is REAL — well past the 900px the pane would otherwise be pushed
  // down by. Without this the two measurements below could be the same point.
  expect(atBottom).toBeGreaterThan(900);
  await page.waitForTimeout(250);
  const boxAtBottom = await runAction.boundingBox();

  expect(boxAtTop).not.toBeNull();
  expect(boxAtBottom).not.toBeNull();
  // The invariant: the action bar is fully inside the viewport at BOTH ends of
  // the scroll range. This is what "reachable at every scroll position" means,
  // and it is what the old stretched layout broke.
  for (const box of [boxAtTop!, boxAtBottom!]) {
    expect(box.y).toBeGreaterThanOrEqual(0);
    expect(box.y + box.height).toBeLessThanOrEqual(900);
  }
  // Once sticky has engaged it PINS: scrolling the remaining ~1000px does not
  // move the action bar at all.
  const mid = await scrollTo(300);
  expect(mid).toBe(300);
  await page.waitForTimeout(250);
  const boxAtMid = await runAction.boundingBox();
  expect(boxAtMid).not.toBeNull();
  expect(Math.abs(boxAtBottom!.y - boxAtMid!.y)).toBeLessThan(4);
});
