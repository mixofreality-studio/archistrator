/**
 * construction-graph-hover.spec — the GRAPH lens's hover card is never clipped
 * (designer P1-1), black-box, at 1280, 1366 and 1600.
 *
 * The hover card used to be xyflow's NodeToolbar inside the canvas's clipping
 * box: Manager cards lost their top 44–64px and the utility bar its right edge.
 * It is now a portal kept inside the canvas. Hovered at rest (fit zoom), every
 * Manager card's and every utility's hover card must lie wholly inside the
 * canvas — and so inside the window.
 *
 * DISPATCH SAFETY: every non-GET request is aborted before any navigation.
 */
import { test, expect, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';
const CARD_ID = /^construction-graph-card-/;
const SIZES: [number, number][] = [
  [1280, 800],
  [1366, 768],
  [1600, 900],
];

test.beforeEach(async ({ page, request }) => {
  await page.route('**/*', (route) =>
    route.request().method() === 'GET' ? route.fallback() : route.abort()
  );
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface Rect {
  top: number;
  left: number;
  bottom: number;
  right: number;
}

async function rectOf(page: Page, testId: string): Promise<Rect> {
  const box = await page.getByTestId(testId).boundingBox();
  expect(box, testId).not.toBeNull();
  const b = box ?? { x: 0, y: 0, width: 0, height: 0 };
  return { top: b.y, left: b.x, bottom: b.y + b.height, right: b.x + b.width };
}

for (const [w, h] of SIZES) {
  test(`${String(w)}: no Manager or utility hover card is clipped — it lies inside the canvas`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: w, height: h });
    await gotoApp(page, GRAPH);
    await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
    await expect(page.getByTestId(CARD_ID).first()).toBeVisible();
    await page.waitForTimeout(400);
    const canvas = await rectOf(page, TESTID.constructionGraphCanvas);

    const ids = await page.getByTestId(CARD_ID).evaluateAll((els) =>
      els
        .filter((e) => ['manager', 'utility'].includes(e.getAttribute('data-row') ?? ''))
        .map((e) => e.getAttribute('data-testid') ?? '')
    );
    expect(ids.length).toBeGreaterThan(0);
    for (const id of ids) {
      await page.getByTestId(id).hover();
      const hover = page.getByTestId(TESTID.constructionGraphHoverCard);
      await expect(hover, id).toBeVisible();
      await page.waitForTimeout(120);
      const r = await rectOf(page, TESTID.constructionGraphHoverCard);
      expect(r.top, `${id} top`).toBeGreaterThanOrEqual(canvas.top - 0.5);
      expect(r.left, `${id} left`).toBeGreaterThanOrEqual(canvas.left - 0.5);
      expect(r.bottom, `${id} bottom`).toBeLessThanOrEqual(canvas.bottom + 0.5);
      expect(r.right, `${id} right`).toBeLessThanOrEqual(canvas.right + 0.5);
      await page.mouse.move(1, 1);
      await expect(hover).toHaveCount(0);
    }
  });
}
