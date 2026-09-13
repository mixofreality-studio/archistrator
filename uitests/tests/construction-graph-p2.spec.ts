/**
 * construction-graph-p2.spec — the designer's adopted P2s on the GRAPH lens,
 * black-box:
 *
 *   - the selection outline is 2/zoom, clamped to 2–5px;
 *   - a lane's keyboard focus opens its hover card, blur closes it;
 *   - the pane's no-phase note is lens-aware ("click a segment of its
 *     lifecycle bar" beside the graph, the list's words beside the list);
 *   - the up/sideways check is muted at zero alarms;
 *   - one vocabulary: "Passed", never "Gate passed";
 *   - below 1200px the graph's detail drawer is non-modal: no backdrop, and
 *     the canvas beside it keeps taking clicks.
 *
 * DISPATCH SAFETY: every non-GET request is aborted before any navigation.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';
const CARD_ID = /^construction-graph-card-/;
const LANE_ID = /^construction-graph-lane-/;

test.beforeEach(async ({ page, request }) => {
  await page.route('**/*', (route) =>
    route.request().method() === 'GET' ? route.fallback() : route.abort()
  );
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

async function openGraph(page: Page, w = 1366, h = 768, path = GRAPH): Promise<void> {
  await page.setViewportSize({ width: w, height: h });
  await gotoApp(page, path);
  await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
  await expect(page.getByTestId(LANE_ID).first()).toBeVisible();
  await page.waitForTimeout(400);
}

async function zoomOf(page: Page): Promise<number> {
  const style = await page
    .getByTestId(TESTID.constructionGraphCanvas)
    .evaluate(
      (root) => root.querySelector('.react-flow__viewport')?.getAttribute('style') ?? ''
    );
  const m = /scale\(([\d.]+)\)/.exec(style);
  return m === null ? Number.NaN : Number(m[1]);
}

/** An activity with no record at all (planned, nothing attempted). */
async function plannedActivity(request: APIRequestContext): Promise<string | undefined> {
  const res = await request.get(`${BASE}/api/v1/system-design/get-project/archistrator`);
  const body = (await res.json()) as {
    ActivityConstruction: Record<string, { worstOrigin?: string; classified?: boolean }>;
  };
  return Object.entries(body.ActivityConstruction).find(
    ([, r]) => r.worstOrigin === undefined && r.classified === true
  )?.[0];
}

test('the selection outline is 2/zoom, clamped to 2–5px — at fit and zoomed in', async ({
  page,
}) => {
  await openGraph(page);
  const lane = page.getByTestId(LANE_ID).first();
  await lane.click();
  await expect(lane).toHaveAttribute('data-selected', 'true');
  const expected = (zoom: number): number => Math.min(5, Math.max(2, 2 / zoom));
  // The lane publishes the width it asked for; Chrome then snaps an outline to
  // whole device pixels, so the drawn width is within 1px of it.
  const check = async (zoom: number): Promise<void> => {
    const asked = Number(await lane.getAttribute('data-outline-px'));
    expect(asked).toBeCloseTo(expected(zoom), 2);
    const drawn = Number.parseFloat(await lane.evaluate((el) => getComputedStyle(el).outlineWidth));
    expect(Math.abs(drawn - asked)).toBeLessThan(1);
  };
  const atFit = await zoomOf(page);
  await check(atFit);
  for (let i = 0; i < 4; i += 1) await page.getByRole('button', { name: /zoom in/i }).click();
  await page.waitForTimeout(600);
  const zoomed = await zoomOf(page);
  expect(zoomed).toBeGreaterThan(atFit);
  await check(zoomed);
});

test("a lane's keyboard focus opens its hover card; blur closes it", async ({ page }) => {
  await openGraph(page);
  const lane = page.getByTestId(LANE_ID).first();
  // A key press first, so the browser treats the focus as keyboard focus
  // (:focus-visible) — only that opens the hover card; a mouse press never does.
  await page.keyboard.press('Shift');
  await lane.focus();
  await expect(page.getByTestId(TESTID.constructionGraphHoverCard)).toBeVisible();
  await lane.evaluate((el) => {
    (el as HTMLElement).blur();
  });
  await expect(page.getByTestId(TESTID.constructionGraphHoverCard)).toHaveCount(0);
});

test('the pane says "click a segment of its lifecycle bar" beside the graph, the list words beside the list', async ({
  page,
  request,
}) => {
  const id = await plannedActivity(request);
  test.skip(id === undefined, 'no planned, unrecorded activity in this project');
  await openGraph(page, 1366, 768, `${GRAPH}&a=${id ?? ''}`);
  const pane = page.getByTestId(TESTID.constructionDetailPane);
  await expect(pane).toContainText('click a segment of its lifecycle bar');
  await expect(pane).not.toContainText('drawn in the list');

  await page.getByTestId(TESTID.constructionLensButton('list')).click();
  await expect(pane).toContainText('drawn in the list — select a phase or task there');
});

test('the up/sideways check is muted at zero alarms', async ({ page }) => {
  await openGraph(page);
  const check = page.getByTestId(TESTID.constructionGraphLayerCheck);
  const alarms = Number(await check.getAttribute('data-alarms'));
  await expect(check).toHaveAttribute('data-tone', alarms > 0 ? 'alarm' : 'quiet');
});

test('one vocabulary: the key and the hover card say "Passed", never "Gate passed"', async ({
  page,
}) => {
  await openGraph(page);
  await page.getByTestId(TESTID.constructionGraphKeyButton).click();
  const key = page.getByTestId(TESTID.constructionGraphKey);
  await expect(key).toContainText('Passed');
  await expect(key).not.toContainText('Gate passed');
  await page.keyboard.press('Escape');

  const withLanes = await page.getByTestId(CARD_ID).evaluateAll(
    (els) =>
      els.find((e) => Number(e.getAttribute('data-lanes') ?? '0') > 0)?.getAttribute('data-testid') ??
      ''
  );
  await page.getByTestId(withLanes).hover();
  const hover = page.getByTestId(TESTID.constructionGraphHoverCard);
  await expect(hover).toBeVisible();
  await expect(hover).not.toContainText('Gate passed');
});

test('below 1200px the graph drawer is non-modal: no backdrop, and the canvas still takes clicks', async ({
  page,
}) => {
  await openGraph(page, 1100, 800);
  // The two leftmost lanes — clear of the 480px drawer on the right.
  const left = await page.getByTestId(LANE_ID).evaluateAll((els) =>
    els
      .map((e) => ({
        id: e.getAttribute('data-testid') ?? '',
        x: e.getBoundingClientRect().left,
      }))
      .sort((a, b) => a.x - b.x)
      .slice(0, 2)
      .map((l) => l.id)
  );
  expect(left).toHaveLength(2);
  await page.getByTestId(left[0] ?? '').click();
  await expect(page.getByRole('dialog')).toBeVisible();
  const backdrops = await page.evaluate(
    () =>
      [...document.querySelectorAll('.MuiBackdrop-root')].filter(
        (b) => getComputedStyle(b).visibility !== 'hidden' && getComputedStyle(b).opacity !== '0'
      ).length
  );
  expect(backdrops, 'no backdrop over the canvas').toBe(0);

  // Non-modal: another lane on the canvas is still clickable with the drawer open.
  const second = (left[1] ?? '').replace('construction-graph-lane-', '');
  await page.getByTestId(left[1] ?? '').click();
  await expect.poll(() => new URL(page.url()).searchParams.get('a')).toBe(second);
});
