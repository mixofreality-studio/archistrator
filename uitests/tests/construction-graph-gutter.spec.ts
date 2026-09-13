/**
 * construction-graph-gutter.spec — the GRAPH lens's row labels are readable at
 * fit (designer P1-6), black-box, at 1280, 1366 and 1600.
 *
 * The labels are a pinned HTML gutter synced to the viewport's y: at rest (fit
 * zoom) every row's label is drawn at a readable size, centred on that row's
 * band of cards, with the cards clear of the gutter; zoomed in, the labels keep
 * their size and still track their rows.
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

interface Band {
  row: string;
  top: number;
  bottom: number;
}

/** Each layered row's band: the vertical span of its cards on screen. */
async function bands(page: Page): Promise<Band[]> {
  const cards = await page.getByTestId(CARD_ID).evaluateAll((els) =>
    els.map((e) => {
      const r = e.getBoundingClientRect();
      return { row: e.getAttribute('data-row') ?? '', top: r.top, bottom: r.bottom, left: r.left };
    })
  );
  const byRow = new Map<string, Band>();
  for (const c of cards) {
    if (c.row === 'utility') continue;
    const b = byRow.get(c.row);
    byRow.set(c.row, {
      row: c.row,
      top: Math.min(b?.top ?? Number.POSITIVE_INFINITY, c.top),
      bottom: Math.max(b?.bottom ?? Number.NEGATIVE_INFINITY, c.bottom),
    });
  }
  return [...byRow.values()];
}

async function labelFacts(
  page: Page,
  row: string
): Promise<{ centre: number; right: number; fontPx: number }> {
  return page.getByTestId(TESTID.constructionGraphRowLabel(row)).evaluate((el) => {
    const span = el.firstElementChild ?? el;
    const r = span.getBoundingClientRect();
    return {
      centre: r.top + r.height / 2,
      right: el.getBoundingClientRect().right,
      fontPx: Number.parseFloat(getComputedStyle(span).fontSize),
    };
  });
}

async function expectLabelsTrackRows(page: Page, label: string): Promise<void> {
  const canvas = await page.getByTestId(TESTID.constructionGraphCanvas).boundingBox();
  expect(canvas).not.toBeNull();
  for (const b of await bands(page)) {
    // Only rows actually on screen carry a label.
    if (b.bottom < (canvas?.y ?? 0) || b.top > (canvas?.y ?? 0) + (canvas?.height ?? 0)) continue;
    const f = await labelFacts(page, b.row);
    expect(f.fontPx, `${label} ${b.row}: readable`).toBeGreaterThanOrEqual(9);
    expect(f.centre, `${label} ${b.row}: on its row`).toBeGreaterThanOrEqual(b.top - 1);
    expect(f.centre, `${label} ${b.row}: on its row`).toBeLessThanOrEqual(b.bottom + 1);
  }
}

for (const [w, h] of SIZES) {
  test(`${String(w)}: every row label is readable at fit, on its row, with the cards clear of it`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: w, height: h });
    await gotoApp(page, GRAPH);
    await expect(page.getByTestId(TESTID.constructionGraphRowGutter)).toBeVisible();
    await expect(page.getByTestId(CARD_ID).first()).toBeVisible();
    await page.waitForTimeout(400);

    await expectLabelsTrackRows(page, 'fit');

    // The fitted cards clear the gutter.
    const gutter = await page.getByTestId(TESTID.constructionGraphRowGutter).boundingBox();
    const leftmost = await page.getByTestId(CARD_ID).evaluateAll((els) =>
      Math.min(
        ...els
          .filter((e) => e.getAttribute('data-row') !== 'utility')
          .map((e) => e.getBoundingClientRect().left)
      )
    );
    expect(leftmost).toBeGreaterThanOrEqual((gutter?.x ?? 0) + (gutter?.width ?? 0) - 12);

    // Zoomed in, the labels keep their size and follow their rows.
    await page.getByRole('button', { name: /zoom in/i }).click();
    await page.getByRole('button', { name: /zoom in/i }).click();
    await page.waitForTimeout(600);
    await expectLabelsTrackRows(page, 'zoomed');
  });
}
