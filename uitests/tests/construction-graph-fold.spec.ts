/**
 * construction-graph-fold.spec — the GRAPH lens fits above the fold (designer
 * P1-2), black-box.
 *
 *   - at 1366×768 at rest, and with the detail pane open at 1280×800 and
 *     1366×768, the whole canvas sits inside the window and the page does not
 *     scroll (neither the console's scroller nor the document);
 *   - the milestone ribbon is ONE line that scrolls sideways, never wraps;
 *   - the key is one "Key" popover button beside the up/sideways check.
 *
 * DISPATCH SAFETY: every non-GET request is aborted before any navigation.
 */
import { test, expect, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';
const LANE_ID = /^construction-graph-lane-/;

test.beforeEach(async ({ page, request }) => {
  await page.route('**/*', (route) =>
    route.request().method() === 'GET' ? route.fallback() : route.abort()
  );
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface Fold {
  canvasTop: number;
  canvasBottom: number;
  canvasHeight: number;
  innerHeight: number;
  scrollerOverflow: number;
  documentOverflow: number;
}

/** The canvas box, and how far its scroller and the document can scroll. */
async function fold(page: Page): Promise<Fold> {
  return page.getByTestId(TESTID.constructionGraphCanvas).evaluate((canvas) => {
    let scroller: HTMLElement | null = canvas.parentElement;
    while (scroller !== null && !/(auto|scroll)/.test(getComputedStyle(scroller).overflowY)) {
      scroller = scroller.parentElement;
    }
    const r = canvas.getBoundingClientRect();
    const doc = document.scrollingElement ?? document.documentElement;
    return {
      canvasTop: r.top,
      canvasBottom: r.bottom,
      canvasHeight: r.height,
      innerHeight: window.innerHeight,
      scrollerOverflow: scroller === null ? 0 : scroller.scrollHeight - scroller.clientHeight,
      documentOverflow: doc.scrollHeight - doc.clientHeight,
    };
  });
}

function expectFits(f: Fold, label: string): void {
  expect(f.canvasBottom, `${label}: canvas bottom within the window`).toBeLessThanOrEqual(
    f.innerHeight + 0.5
  );
  expect(f.canvasHeight, `${label}: the canvas keeps a usable height`).toBeGreaterThanOrEqual(
    270
  );
  expect(f.scrollerOverflow, `${label}: the console does not scroll`).toBeLessThanOrEqual(1);
  expect(f.documentOverflow, `${label}: the document does not scroll`).toBeLessThanOrEqual(1);
}

async function openGraph(page: Page, w: number, h: number): Promise<void> {
  await page.setViewportSize({ width: w, height: h });
  await gotoApp(page, GRAPH);
  await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
  await expect(page.getByTestId(LANE_ID).first()).toBeVisible();
  await page.waitForTimeout(400);
}

test('1366×768 at rest: the canvas fits above the fold and the page does not scroll', async ({
  page,
}) => {
  await openGraph(page, 1366, 768);
  expectFits(await fold(page), '1366 rest');
});

for (const [w, h] of [
  [1280, 800],
  [1366, 768],
] as const) {
  test(`${String(w)}×${String(h)} with the pane open: the canvas fits and the page does not scroll`, async ({
    page,
  }) => {
    await openGraph(page, w, h);
    await page.getByTestId(LANE_ID).first().click();
    await expect(page.getByTestId(TESTID.constructionDetailPane)).toBeVisible();
    await page.waitForTimeout(500);
    expectFits(await fold(page), `${String(w)} pane open`);
  });
}

test('the ribbon is one line that scrolls sideways, and the key is a popover button', async ({
  page,
}) => {
  await openGraph(page, 1280, 800);
  // Open the pane: the narrowest content column the lens gets at 1280.
  await page.getByTestId(LANE_ID).first().click();
  await expect(page.getByTestId(TESTID.constructionDetailPane)).toBeVisible();
  const ribbon = page.getByTestId(TESTID.constructionGraphRibbon);
  const geometry = await ribbon.evaluate((el) => ({
    wrap: getComputedStyle(el).flexWrap,
    overflowX: getComputedStyle(el).overflowX,
    tops: Array.from(el.children).map((c) => Math.round(c.getBoundingClientRect().top)),
  }));
  expect(geometry.wrap).toBe('nowrap');
  expect(['auto', 'scroll']).toContain(geometry.overflowX);
  expect(new Set(geometry.tops).size, 'every milestone chip on one line').toBe(1);

  // The key: closed at rest, one button beside the check, opens as a popover.
  await expect(page.getByTestId(TESTID.constructionGraphKey)).toHaveCount(0);
  const check = await page.getByTestId(TESTID.constructionGraphLayerCheck).boundingBox();
  const button = await page.getByTestId(TESTID.constructionGraphKeyButton).boundingBox();
  expect(check).not.toBeNull();
  expect(button).not.toBeNull();
  expect(Math.abs((check?.y ?? 0) - (button?.y ?? 99)), 'the button sits on the check line').toBeLessThan(
    14
  );
  await page.getByTestId(TESTID.constructionGraphKeyButton).click();
  await expect(page.getByTestId(TESTID.constructionGraphKey)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionGraphKey)).toContainText('RECONSTRUCTED');
});

for (const [w, h] of [
  [1280, 800],
  [1366, 768],
] as const) {
  test(`${String(w)}×${String(h)} with a pane at its FULL height: no scroller above it and not the document scrolls (designer re-check 2)`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: w, height: h });
    // N-IT's pane is long enough to reach its height cap — the case where the
    // cap, not the content, decides the pane's bottom edge.
    await gotoApp(page, `${GRAPH}&a=N-IT`);
    const pane = page.getByTestId(TESTID.constructionDetailPane);
    await expect(pane).toBeVisible();
    await expect(page.getByTestId(LANE_ID).first()).toBeVisible();
    await page.waitForTimeout(600);
    const probe = await pane.evaluate((el) => {
      const canvas = document.querySelector('[data-testid="construction-graph-canvas"]');
      const overflows: string[] = [];
      const seen = new Set<Element>();
      for (const start of [el.parentElement, canvas?.parentElement ?? null]) {
        for (let s: HTMLElement | null = start; s !== null; s = s.parentElement) {
          if (seen.has(s) || !/(auto|scroll)/.test(getComputedStyle(s).overflowY)) continue;
          seen.add(s);
          const over = s.scrollHeight - s.clientHeight;
          if (over > 1) overflows.push(`${s.tagName}.${s.className.slice(0, 40)} +${String(over)}px`);
        }
      }
      const doc = document.scrollingElement ?? document.documentElement;
      return {
        atCap: el.getBoundingClientRect().height >= Number.parseFloat(getComputedStyle(el).maxHeight) - 1,
        overflows,
        documentOverflow: doc.scrollHeight - doc.clientHeight,
      };
    });
    expect(probe.atCap, 'the pane reached its height cap (else this test proves nothing)').toBe(true);
    expect(probe.overflows, 'no scroller above the pane or the canvas scrolls').toEqual([]);
    expect(probe.documentOverflow, 'the document does not scroll').toBeLessThanOrEqual(1);
  });
}
