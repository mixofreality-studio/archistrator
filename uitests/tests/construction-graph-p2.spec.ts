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
 * DISPATCH SAFETY: every non-GET request is aborted before any navigation, by the
 * context-level guard (support/dispatchGuard), never a per-page route.
 */
import type { APIRequestContext, Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';
const CARD_ID = /^construction-graph-card-/;
const LANE_ID = /^construction-graph-lane-/;

test.beforeEach(async ({ request }) => {
  // DISPATCH SAFETY: support/dispatchGuard's context route aborts every
  // non-GET before any navigation (and any request a test still holds, at teardown).
  await requireServer(request, BASE);
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
      Array.from(document.querySelectorAll('.MuiBackdrop-root')).filter(
        (b) => getComputedStyle(b).visibility !== 'hidden' && getComputedStyle(b).opacity !== '0'
      ).length
  );
  expect(backdrops, 'no backdrop over the canvas').toBe(0);
  // Non-modal for assistive technology too: a Modal marks the rest of the app
  // aria-hidden; the canvas must stay in the accessibility tree.
  const hidden = await page
    .getByTestId(TESTID.constructionGraphCanvas)
    .evaluate((el) => el.closest('[aria-hidden="true"]') !== null);
  expect(hidden, 'the canvas is not aria-hidden behind the drawer').toBe(false);

  // Non-modal: another lane on the canvas is still clickable with the drawer open.
  const second = (left[1] ?? '').replace('construction-graph-lane-', '');
  await page.getByTestId(left[1] ?? '').click();
  await expect.poll(() => new URL(page.url()).searchParams.get('a')).toBe(second);
});

test('the zoom controls stay reachable: clear of the gutter, never under the narrow drawer', async ({
  page,
}) => {
  await openGraph(page, 1100, 800);
  await page.getByTestId(LANE_ID).first().click();
  await expect(page.getByRole('dialog')).toBeVisible();
  const zoomIn = page.getByRole('button', { name: /zoom in/i });
  // Actionable: visible, stable, and nothing covers it (the drawer used to).
  await zoomIn.click({ trial: true, timeout: 5_000 });
  const control = await zoomIn.boundingBox();
  const drawer = await page.getByRole('dialog').boundingBox();
  const gutter = await page.getByTestId(TESTID.constructionGraphRowGutter).boundingBox();
  expect(control).not.toBeNull();
  expect((control?.x ?? 0) + (control?.width ?? 0)).toBeLessThanOrEqual(drawer?.x ?? 0);
  expect(control?.x ?? 0).toBeGreaterThanOrEqual((gutter?.x ?? 0) + (gutter?.width ?? 0));
});

test('at 1100 an open hover card never paints over the drawer (graph re-review)', async ({
  page,
}) => {
  await openGraph(page, 1100, 800);
  await page.getByTestId(LANE_ID).first().click();
  const drawer = page.getByRole('dialog');
  await expect(drawer).toBeVisible();
  const d = await drawer.boundingBox();
  expect(d).not.toBeNull();
  const drawerLeft = d?.x ?? 0;
  // A card clear of the drawer whose right-placed hover card (320px) must reach
  // under it — the case the z-order decides.
  const id = await page.getByTestId(CARD_ID).evaluateAll((els, left) => {
    const near = els
      .map((e) => ({ id: e.getAttribute('data-testid') ?? '', r: e.getBoundingClientRect() }))
      .filter((c) => c.r.width > 0 && c.r.right < left - 10 && c.r.right > left - 280)
      .sort((a, b) => b.r.right - a.r.right);
    return near[0]?.id ?? '';
  }, drawerLeft);
  expect(id, 'a card just left of the drawer').not.toBe('');
  await page.getByTestId(id).hover();
  const hover = page.getByTestId(TESTID.constructionGraphHoverCard);
  await expect(hover).toBeVisible();
  await page.waitForTimeout(150);
  const h = await hover.boundingBox();
  expect(h).not.toBeNull();
  const hr = h ?? { x: 0, y: 0, width: 0, height: 0 };
  expect(hr.x + hr.width, 'the hover card reaches under the drawer').toBeGreaterThan(drawerLeft + 4);
  // Where they overlap, the DRAWER is on top. The hover card is
  // pointer-events:none, and elementFromPoint skips such elements whatever
  // their z-order — so every element takes hits for the probe's instant, or
  // the probe could never see the hover card at all (the first cut could not).
  const x = (Math.max(hr.x, drawerLeft) + hr.x + hr.width) / 2;
  const y = hr.y + hr.height / 2;
  const onTop = await page.evaluate(
    ([px, py]) => {
      const probe = document.createElement('style');
      probe.textContent = '* { pointer-events: auto !important; }';
      document.head.append(probe);
      try {
        const hit = document.elementFromPoint(px ?? 0, py ?? 0);
        if (hit?.closest('[data-testid="construction-graph-hover-card"]') != null) return false;
        return hit?.closest('[role="dialog"]') != null;
      } finally {
        probe.remove();
      }
    },
    [x, y]
  );
  expect(onTop, 'the drawer paints over the hover card').toBe(true);
});

test('at 1100 the graph drawer takes focus, Escape closes it, and focus returns to the lane (graph re-review)', async ({
  page,
}) => {
  await openGraph(page, 1100, 800);
  const lanes = page.getByTestId(LANE_ID);
  const first = (await lanes.first().getAttribute('data-testid')) ?? '';
  await page.getByTestId(first).click();
  const drawer = page.getByRole('dialog');
  await expect(drawer).toBeVisible();
  // Focus moves INTO the drawer when it opens.
  await expect.poll(() => drawer.evaluate((d) => d.contains(document.activeElement))).toBe(true);

  // Non-modal: another lane is chosen with the drawer open — focus then returns to it.
  const second = await lanes.evaluateAll(
    (els, skip) =>
      els
        .filter((e) => e.getBoundingClientRect().right < window.innerWidth - 520)
        .map((e) => e.getAttribute('data-testid') ?? '')
        .find((id) => id !== skip) ?? '',
    first
  );
  expect(second, 'a second lane clear of the drawer').not.toBe('');
  await page.getByTestId(second).click();
  await expect
    .poll(() => new URL(page.url()).searchParams.get('a'))
    .toBe(second.replace('construction-graph-lane-', ''));
  await drawer.evaluate((d) => {
    (d.querySelector('[tabindex="-1"]') as HTMLElement | null)?.focus();
  });

  // Escape inside the drawer closes it, and focus lands back on the lane.
  await page.keyboard.press('Escape');
  await expect(drawer).toHaveCount(0);
  await expect.poll(() => new URL(page.url()).searchParams.get('a')).toBeNull();
  await expect(page.getByTestId(second)).toBeFocused();
});

test('edges are not Tab stops: the first lane is a few Tabs past the Key button (designer re-check)', async ({
  page,
}) => {
  await openGraph(page);
  const focusableEdges = await page
    .getByTestId(TESTID.constructionGraphCanvas)
    .evaluate((root) => root.querySelectorAll('.react-flow__edge[tabindex]').length);
  expect(focusableEdges, 'no edge takes focus').toBe(0);
  await page.getByTestId(TESTID.constructionGraphKeyButton).focus();
  let presses = 0;
  while (presses < 12) {
    await page.keyboard.press('Tab');
    presses += 1;
    const onLane = await page.evaluate(
      () =>
        document.activeElement?.getAttribute('data-testid')?.startsWith('construction-graph-lane-') ===
        true
    );
    if (onLane) break;
  }
  expect(presses, 'Tabs from the Key button to the first lane').toBeLessThanOrEqual(3);
});

test('the key draws its swatches and never reads "= =" (designer re-check 8)', async ({ page }) => {
  await openGraph(page);
  await page.getByTestId(TESTID.constructionGraphKeyButton).click();
  const key = page.getByTestId(TESTID.constructionGraphKey);
  await expect(key).toBeVisible();
  expect((await key.textContent()) ?? '', 'no "=" in the key').not.toContain('=');
  const paint = async (kind: string, prop: string): Promise<string> =>
    page
      .getByTestId(TESTID.constructionGraphKeySwatch(kind))
      .evaluate((el, p) => getComputedStyle(el).getPropertyValue(p), prop);
  expect(await paint('hatch', 'background-image'), 'the hatch is drawn').not.toBe('none');
  expect(await paint('critical', 'border-left-width'), 'the critical edge is drawn').toBe('3px');
  await expect(page.getByTestId(TESTID.constructionGraphKeySwatch('spine'))).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionGraphKeySwatch('float'))).toContainText(/\d/);
  await expect(key).toContainText('RECONSTRUCTED');
});

for (const [w, h] of [
  [1280, 800],
  [1366, 768],
  [1600, 900],
] as const) {
  test(`${String(w)}: at fit, every lane's provenance rail sits 2 screen px clear of its edge (designer re-check 7)`, async ({
    page,
  }) => {
    await openGraph(page, w, h);
    const gaps = await page.getByTestId(LANE_ID).evaluateAll((lanes) =>
      lanes.flatMap((lane) => {
        const rail = lane.querySelector('[data-testid="construction-provenance-rail"]');
        if (rail === null) return [];
        const r = lane.getBoundingClientRect();
        const zoom = r.width / (lane as HTMLElement).offsetWidth;
        const edgeRight = r.left + (lane as HTMLElement).clientLeft * zoom;
        return [
          {
            id: lane.getAttribute('data-testid') ?? '',
            gap: rail.getBoundingClientRect().left - edgeRight,
          },
        ];
      })
    );
    expect(gaps.length, 'some lane carries a provenance rail').toBeGreaterThan(0);
    for (const g of gaps) expect(g.gap, g.id).toBeGreaterThanOrEqual(1.95);
  });
}

test('at 1100 a deep link frames its card CLEAR of the drawer, not under it (designer re-check 4)', async ({
  page,
}) => {
  await openGraph(page, 1100, 800, `${GRAPH}&a=N-IT`);
  const drawer = page.getByRole('dialog');
  await expect(drawer).toBeVisible();
  // The framing animates (400ms) after two frames; let it settle.
  await page.waitForTimeout(1200);
  const lane = await page.getByTestId(TESTID.constructionGraphLane('N-IT')).boundingBox();
  const d = await drawer.boundingBox();
  const c = await page.getByTestId(TESTID.constructionGraphCanvas).boundingBox();
  expect(lane).not.toBeNull();
  expect((lane?.x ?? 0) + (lane?.width ?? 0), 'the lane ends left of the drawer').toBeLessThanOrEqual(
    d?.x ?? 0
  );
  expect(lane?.x ?? 0, 'the lane starts inside the canvas').toBeGreaterThanOrEqual(c?.x ?? 0);
});
