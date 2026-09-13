/**
 * construction-narrow-drawer.spec — below 1200px the list lens's detail pane is a
 * NON-modal drawer, the same persistent drawer the graph lens uses (designer
 * re-check #11), and it keeps its a11y: focus moves in when it opens, Escape
 * inside it closes it, and focus returns to the row that opened it.
 *
 * It was MUI's temporary (modal) Drawer: a backdrop over the whole console, a
 * focus trap, and everything else marked aria-hidden, so the lens toggle could
 * not be reached while the pane was open.
 *
 * Gated like the other construction specs: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy. SAFETY: the shared dispatch
 * guard; nothing here writes.
 */
import type { Locator, Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** An activity row at the top level of the list (its node id is the activity id). */
const ACTIVITY = 'U-SPA-web-client';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

async function openNarrowList(page: Page): Promise<void> {
  await page.setViewportSize({ width: 1100, height: 800 });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });
}

/** The drawer's paper: the dialog the pane renders into below 1200px. */
function drawerPaper(page: Page): Locator {
  return page.getByTestId(TESTID.constructionDetailDrawer).getByRole('dialog');
}

test('at 1100px the pane opens as a NON-modal drawer: no backdrop, nothing aria-hidden, and the lens toggle still works', async ({
  page,
  dispatchGuard,
}) => {
  await openNarrowList(page);
  await page.getByTestId(TESTID.constructionListRow(ACTIVITY)).click();
  const paper = drawerPaper(page);
  await expect(paper).toBeVisible();
  await expect(paper).toHaveAttribute('aria-modal', 'false');
  // Where a persistent drawer renders is up to the page (it is not portalled to the
  // body): it must still sit on the viewport's right edge, full height.
  const box = await paper.boundingBox();
  expect(box, 'the drawer paper has a box').not.toBeNull();
  expect(Math.abs((box?.x ?? 0) + (box?.width ?? 0) - 1100), 'flush with the right edge').toBeLessThan(1);
  expect(box?.y ?? -1, 'from the top').toBeLessThan(1);
  expect(box?.height ?? 0, 'full height').toBeGreaterThan(799);
  // eslint-disable-next-line no-restricted-syntax -- a Modal's backdrop has no role or testid; its absence is the structural assertion
  await expect(page.locator('.MuiBackdrop-root')).toHaveCount(0);

  // Nothing outside the drawer is hidden from assistive technology: a Modal marks
  // every sibling of its container aria-hidden, the list with it.
  const hiddenTree = await page.evaluate(
    (treeId) => document.querySelectorAll(`[aria-hidden="true"] [data-testid="${treeId}"]`).length,
    TESTID.constructionListTree
  );
  expect(hiddenTree, 'the list is aria-hidden behind the drawer').toBe(0);

  // The lens toggle is on top, not under the drawer or a backdrop: a real click
  // lands on it, and switches the lens with the pane still open on its activity.
  const graph = page.getByTestId(TESTID.constructionLensButton('graph'));
  const onTop = await graph.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const top = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return top !== null && el.contains(top);
  });
  expect(onTop, 'the lens toggle is covered').toBe(true);
  await graph.click();
  await expect(page).toHaveURL(/[?&]lens=graph/);
  await expect(page).toHaveURL(new RegExp(`[?&]a=${ACTIVITY}`));
  await expect(paper).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});

test('at 1100px focus moves into the drawer when it opens, Escape inside it closes it, and focus returns to the row', async ({
  page,
  dispatchGuard,
}) => {
  await openNarrowList(page);
  const row = page.getByTestId(TESTID.constructionListRow(ACTIVITY));
  await row.click();
  const paper = drawerPaper(page);
  await expect(paper).toBeVisible();

  // Focus moved in, so the next keys act on the drawer.
  await expect
    .poll(() => paper.evaluate((el) => el.contains(document.activeElement)), {
      message: 'focus is inside the drawer',
    })
    .toBe(true);

  // Escape inside it closes it, and the selection with it.
  await page.keyboard.press('Escape');
  await expect(paper).toHaveCount(0);
  await expect(page).not.toHaveURL(/[?&]a=/);

  // Focus is back on the row that opened it: the tree item that holds the row,
  // exactly. (Focus dropped to <body> when the drawer unmounted would "contain" the
  // row too, which is why this names the item.)
  await expect
    .poll(
      () =>
        row.evaluate((el) => {
          const item = el.closest('[role="treeitem"]');
          return item !== null && document.activeElement === item;
        }),
      { message: 'focus returned to the row' }
    )
    .toBe(true);
  expect(dispatchGuard.blocked).toEqual([]);
});
