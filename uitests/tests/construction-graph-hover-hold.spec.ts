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
 * never hidden.
 *
 * DISPATCH SAFETY: every non-GET request is aborted before any navigation.
 */
import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';
const CARD_ID = /^construction-graph-card-/;

test.beforeEach(async ({ page, request }) => {
  await page.route('**/*', (route) =>
    route.request().method() === 'GET' ? route.fallback() : route.abort()
  );
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface Sample {
  open: boolean;
  hidden: boolean;
}

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
  });
}
