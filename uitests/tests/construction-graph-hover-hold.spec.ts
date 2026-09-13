/**
 * construction-graph-hover-hold.spec — a RESTING pointer holds a hover card
 * open, black-box, at 1600.
 *
 * Found by graph round 2's flake hunt: every hover change rebuilds the canvas's
 * node objects, and a controlled xyflow node that arrives without dimensions is
 * drawn `visibility: hidden` until it is measured again. For that frame the
 * pointer sat over the bare pane, so the browser reported a mouseleave, the
 * hover card closed, the card re-appeared, the hover re-opened — a ~350ms
 * flicker under a pointer that never moved. The nodes now carry the layout's
 * own width and height, so no node is ever unmeasured.
 *
 * Sampled every 25ms for a second, on a utility (the case that flickered) and on
 * a Manager card: the hover card never closes, and the node under the pointer is
 * never hidden. The flicker needs a mouseleave to land in a one-frame gap, so the
 * sampling alone is probabilistic (the mutation round showed it); the CAUSE is
 * not — so a MutationObserver on the node layer, installed before the hover,
 * also requires that no node is ever written `visibility: hidden`.
 *
 * DISPATCH SAFETY: every non-GET request is aborted before any navigation, by the
 * context-level guard (support/dispatchGuard), never a per-page route.
 */
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';
const CARD_ID = /^construction-graph-card-/;

test.beforeEach(async ({ request }) => {
  // DISPATCH SAFETY: support/dispatchGuard's context route aborts every
  // non-GET before any navigation (and any request a test still holds, at teardown).
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface Sample {
  open: boolean;
  hidden: boolean;
}

/** Counts every node style write that hides a node (old or new value). */
type HoldWindow = Window & { hiddenNodeWrites?: number };

for (const row of ['utility', 'manager']) {
  test(`1600: a resting pointer holds a ${row} card's hover card open — no flicker, no hidden node`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1600, height: 900 });
    await gotoApp(page, GRAPH);
    await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
    await expect(page.getByTestId(CARD_ID).first()).toBeVisible();
    await page.waitForTimeout(400);
    const id = await page
      .getByTestId(CARD_ID)
      .evaluateAll(
        (els, r) => els.find((e) => e.getAttribute('data-row') === r)?.getAttribute('data-testid') ?? '',
        row
      );
    expect(id, `a ${row} card`).not.toBe('');
    const card = page.getByTestId(id);
    await card.evaluate((el) => {
      const layer = el.closest('.react-flow__nodes');
      if (layer === null) throw new Error('the card is not inside the node layer');
      const w = window as HoldWindow;
      w.hiddenNodeWrites = 0;
      new MutationObserver((records) => {
        for (const r of records) {
          const node = r.target as HTMLElement;
          if (!node.classList.contains('react-flow__node')) continue;
          if (node.style.visibility === 'hidden' || (r.oldValue ?? '').includes('visibility: hidden')) {
            w.hiddenNodeWrites = (w.hiddenNodeWrites ?? 0) + 1;
          }
        }
      }).observe(layer, {
        attributes: true,
        attributeFilter: ['style'],
        attributeOldValue: true,
        subtree: true,
      });
    });
    await card.hover();
    const hover = page.getByTestId(TESTID.constructionGraphHoverCard);
    await expect(hover).toBeVisible();

    const samples = await card.evaluate(async (el) => {
      const node = el.closest('.react-flow__node');
      const out: Sample[] = [];
      for (let i = 0; i < 40; i += 1) {
        out.push({
          open: document.querySelector('[data-testid="construction-graph-hover-card"]') !== null,
          hidden: node !== null && getComputedStyle(node).visibility === 'hidden',
        });
        await new Promise((resolve) => setTimeout(resolve, 25));
      }
      return out;
    });
    expect(samples.filter((s) => s.hidden), 'the hovered node is never hidden').toHaveLength(0);
    expect(samples.filter((s) => !s.open), 'the hover card never closes').toHaveLength(0);
    // Leaving rebuilds every node again: still none is ever hidden.
    await page.mouse.move(1, 1);
    await expect(hover).toHaveCount(0);
    await page.waitForTimeout(200);
    expect(
      await page.evaluate(() => (window as HoldWindow).hiddenNodeWrites),
      'no node is ever written visibility:hidden across hover changes'
    ).toBe(0);
  });
}
