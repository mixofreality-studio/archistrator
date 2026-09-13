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
 * DISPATCH SAFETY: every non-GET request is aborted before any navigation, by the
 * context-level guard (support/dispatchGuard), never a per-page route.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';
const CARD_ID = /^construction-graph-card-/;
const SIZES: [number, number][] = [
  [1280, 800],
  [1366, 768],
  [1600, 900],
];

test.beforeEach(async ({ request }) => {
  // DISPATCH SAFETY: support/dispatchGuard's context route aborts every
  // non-GET before any navigation (and any request a test still holds, at teardown).
  await requireServer(request, BASE);
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

    // The Client row too: it sits at the canvas's top edge, where a hover card
    // centred on its card would ride up over the ribbon unless it is bounded to
    // the CANVAS (popper.js's own defaults bound only to the viewport — the
    // mutation round showed Managers and utilities alone cannot tell them apart).
    const ids = await page.getByTestId(CARD_ID).evaluateAll((els) =>
      els
        .filter((e) => ['client', 'manager', 'utility'].includes(e.getAttribute('data-row') ?? ''))
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

/** Counts popper.js RE-CREATIONS of the open hover card: destroying a popper
 *  strips `data-popper-placement` from its element, so a record whose old value
 *  is null means the attribute came back after a destroy. */
type ProbeWindow = Window & { popperRecreated?: number };

test('a zoom under an open hover card never re-creates its popper (graph re-review)', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1366, height: 768 });
  await gotoApp(page, GRAPH);
  await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
  await page.waitForTimeout(400);
  const id = await page.getByTestId(CARD_ID).evaluateAll(
    (els) =>
      els
        .find(
          (e) => e.getAttribute('data-row') === 'manager' && e.getAttribute('data-lanes') !== '0'
        )
        ?.getAttribute('data-testid') ?? ''
  );
  expect(id, 'a Manager card with a lane').not.toBe('');
  await page.getByTestId(id).hover();
  const hover = page.getByTestId(TESTID.constructionGraphHoverCard);
  await expect(hover).toBeVisible();
  const scaleOf = async (): Promise<string> =>
    page
      .getByTestId(TESTID.constructionGraphCanvas)
      .evaluate((root) => root.querySelector('.react-flow__viewport')?.getAttribute('style') ?? '');
  const before = await scaleOf();
  await hover.evaluate((el) => {
    const root = el.closest('[data-popper-placement]');
    if (root === null) throw new Error('the hover card is not inside a popper');
    const w = window as ProbeWindow;
    w.popperRecreated = 0;
    new MutationObserver((records) => {
      for (const r of records) if (r.oldValue === null) w.popperRecreated = (w.popperRecreated ?? 0) + 1;
    }).observe(root, {
      attributes: true,
      attributeFilter: ['data-popper-placement'],
      attributeOldValue: true,
    });
  });
  // A pinch zoom about the pointer: the card stays under it (the hover holds)
  // while every node re-renders with the new zoom.
  await page.keyboard.down('Control');
  for (let i = 0; i < 3; i += 1) {
    await page.mouse.wheel(0, -120);
    await page.waitForTimeout(150);
  }
  await page.keyboard.up('Control');
  await page.waitForTimeout(400);
  expect(await scaleOf(), 'the zoom changed').not.toBe(before);
  await expect(hover).toBeVisible();
  expect(await page.evaluate(() => (window as ProbeWindow).popperRecreated)).toBe(0);
});

test('keyboard: a focus-opened hover card describes its lane and Escape dismisses it (WCAG 1.4.13)', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1366, height: 768 });
  await gotoApp(page, GRAPH);
  await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
  await page.waitForTimeout(400);
  const lane = page.getByTestId(/^construction-graph-lane-/).first();
  // A key press first, so the focus counts as keyboard focus (:focus-visible).
  await page.keyboard.press('Shift');
  await lane.focus();
  const hover = page.getByTestId(TESTID.constructionGraphHoverCard);
  await expect(hover).toBeVisible();

  // Announced: the lane is described by the open card's tooltip.
  const describedBy = (await lane.getAttribute('aria-describedby')) ?? '';
  expect(describedBy, 'the lane names its hover card').not.toBe('');
  const tooltip = page
    .getByRole('tooltip')
    .filter({ has: page.getByTestId(TESTID.constructionGraphHoverCard) });
  await expect(tooltip).toBeVisible();
  await expect(tooltip).toHaveAttribute('id', describedBy);

  // Dismissible without moving focus, and nothing else happens.
  const url = page.url();
  await page.keyboard.press('Escape');
  await expect(hover).toHaveCount(0);
  await expect(lane).toBeFocused();
  expect(await lane.getAttribute('aria-describedby')).toBeNull();
  expect(page.url()).toBe(url);
});
