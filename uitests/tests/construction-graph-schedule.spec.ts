/**
 * construction-graph-schedule.spec — the GRAPH lens's schedule channels
 * (architect Q2 ruling), black-box, at 1280, 1366 and 1600.
 *
 * Every expectation is computed from the project read over the wire — slot 9's
 * effort, slot 10's `computed[id]` — never hardcoded:
 *
 *   - a lane carries a float rail + numeral EXACTLY when the network has a
 *     computed entry for its activity, and the numeral is that totalFloat;
 *   - the critical path is the LANE's 3px edge — and nothing that is not a
 *     lane (no card, no edge) carries a critical treatment;
 *   - the key captions the figures as the unstaffed derived network;
 *   - the "Critical path" and "Near-critical" scope chips DIM (never move) the
 *     lanes they do not match, with the list's own predicates.
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

interface Computed {
  totalFloat: number;
  onCriticalPath: boolean;
  band: string;
}
interface Truth {
  activityIds: string[];
  computed: Record<string, Computed>;
  effort: Record<string, number>;
}

async function truth(request: APIRequestContext): Promise<Truth> {
  const res = await request.get(`${BASE}/api/v1/system-design/get-project/archistrator`);
  expect(res.ok()).toBe(true);
  const body = (await res.json()) as {
    ActivityConstruction: Record<string, unknown>;
    Slots: { kind: string; model: { model: Record<string, unknown> } }[];
  };
  const model = (kind: string): Record<string, unknown> =>
    body.Slots.find((s) => s.kind === kind)?.model.model ?? {};
  const network = model('network') as { computed?: Record<string, Computed> };
  const list = model('activityList') as { activities?: { name: string; effortDays: number }[] };
  return {
    activityIds: Object.keys(body.ActivityConstruction),
    computed: network.computed ?? {},
    effort: Object.fromEntries((list.activities ?? []).map((a) => [a.name, a.effortDays])),
  };
}

async function openGraph(page: Page, w: number, h: number): Promise<void> {
  await page.setViewportSize({ width: w, height: h });
  await gotoApp(page, GRAPH);
  await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
  await expect(page.getByTestId(/^construction-graph-lane-/).first()).toBeVisible();
}

async function pickScope(page: Page, label: string): Promise<void> {
  await page.getByTestId(TESTID.constructionLensScope).getByRole('combobox').click();
  await page.getByRole('option', { name: label }).click();
}

async function laneOpacity(page: Page, id: string): Promise<number> {
  return Number(
    await page
      .getByTestId(TESTID.constructionGraphLane(id))
      .evaluate((el) => getComputedStyle(el).opacity)
  );
}

async function cardTransforms(page: Page): Promise<Record<string, string>> {
  return page.getByTestId(TESTID.constructionGraphCanvas).evaluate((root) =>
    Object.fromEntries(
      Array.from(root.querySelectorAll('.react-flow__node-graphCard')).map((n) => [
        n.getAttribute('data-id') ?? '',
        (n as HTMLElement).style.transform,
      ])
    )
  );
}

for (const [w, h] of SIZES) {
  test(`${String(w)}: a float rail exactly where the network computed one, its numeral the totalFloat`, async ({
    page,
    request,
  }) => {
    const t = await truth(request);
    await openGraph(page, w, h);
    for (const id of t.activityIds) {
      const mark = page.getByTestId(TESTID.constructionGraphLaneFloat(id));
      const cpm = t.computed[id];
      if (cpm === undefined) {
        // No computed entry: NO rail — never a 0.
        await expect(mark, id).toHaveCount(0);
      } else {
        await expect(mark, id).toHaveAttribute('data-float', String(cpm.totalFloat));
        await expect(mark, id).toContainText(String(cpm.totalFloat));
      }
    }
  });

  test(`${String(w)}: the critical path is the lane's 3px edge — never a card, never an edge`, async ({
    page,
    request,
  }) => {
    const t = await truth(request);
    await openGraph(page, w, h);
    const critical = t.activityIds.filter((id) => t.computed[id]?.onCriticalPath === true);
    expect(critical.length).toBeGreaterThan(0);
    for (const id of t.activityIds) {
      const lane = page.getByTestId(TESTID.constructionGraphLane(id));
      const on = critical.includes(id);
      await expect(lane, id).toHaveAttribute('data-critical', String(on));
      const px = await lane.evaluate((el) => getComputedStyle(el).borderLeftWidth);
      expect(px, id).toBe(on ? '3px' : '2px');
    }
    // Nothing but a lane carries the critical treatment.
    // A page-wide attribute sweep — structural, with no single testid to select by.
    const holders = await page.evaluate(() =>
      Array.from(document.querySelectorAll('[data-critical="true"]')).map(
        (e) => e.getAttribute('data-testid') ?? ''
      )
    );
    expect(holders).toHaveLength(critical.length);
    for (const tid of holders) expect(tid).toMatch(/^construction-graph-lane-/);
    // And in PAINT, not just in attributes (graph re-review): no card's computed
    // left, right or bottom border, nor its outline, carries the critical lane's
    // treatment (3px in the critical ink). The top edge is the layer colour, and
    // the System-wide row's layer colour is that same ink, so it is not a
    // critical mark and is not inspected.
    const ink = await page
      .getByTestId(TESTID.constructionGraphLane(critical[0] ?? ''))
      .evaluate((el) => getComputedStyle(el).borderLeftColor);
    const cards = await page.getByTestId(/^construction-graph-card-/).evaluateAll((els) =>
      els.map((e) => {
        const cs = getComputedStyle(e);
        return {
          id: e.getAttribute('data-testid') ?? '',
          sides: [
            [cs.borderLeftWidth, cs.borderLeftColor],
            [cs.borderRightWidth, cs.borderRightColor],
            [cs.borderBottomWidth, cs.borderBottomColor],
            [cs.outlineStyle === 'none' ? '0px' : cs.outlineWidth, cs.outlineColor],
          ],
        };
      })
    );
    expect(cards.length).toBeGreaterThan(0);
    for (const c of cards) {
      for (const [width, colour] of c.sides) {
        const heavy = Number.parseFloat(width ?? '0') >= 3;
        expect(heavy && colour === ink, `${c.id} carries the critical border`).toBe(false);
      }
    }
    // No edge is drawn in a critical way: every edge's class names only its direction.
    const classes = await page
      .getByTestId(TESTID.constructionGraphCanvas)
      .evaluate((root) =>
        Array.from(root.querySelectorAll('.react-flow__edge')).map((e) => e.getAttribute('class') ?? '')
      );
    for (const c of classes) expect(c).not.toMatch(/critical/i);
  });

  test(`${String(w)}: the caption names the unstaffed derived network, and effort sets spine length`, async ({
    page,
    request,
  }) => {
    const t = await truth(request);
    await openGraph(page, w, h);
    // The key is one popover button beside the check (designer P1-2).
    await page.getByTestId(TESTID.constructionGraphKeyButton).click();
    await expect(page.getByTestId(TESTID.constructionGraphScheduleCaption)).toHaveText(
      'Float and critical path of the derived network, unstaffed.'
    );
    await page.keyboard.press('Escape');
    await expect(page.getByTestId(TESTID.constructionGraphKey)).toHaveCount(0);
    const max = Math.max(...Object.values(t.effort));
    for (const id of t.activityIds.slice(0, 6)) {
      const e = t.effort[id];
      if (e === undefined) continue;
      await expect(page.getByTestId(TESTID.constructionGraphLane(id)), id).toHaveAttribute(
        'data-effort',
        String(e)
      );
      const fraction = await page
        .getByTestId(TESTID.constructionGraphLane(id))
        .evaluate(
          (lane) =>
            lane.querySelector('[data-spine-fraction]')?.getAttribute('data-spine-fraction') ?? ''
        );
      expect(Number(fraction), id).toBeCloseTo(e / max, 6);
    }
  });

  test(`${String(w)}: "Critical path" and "Near-critical" dim what they do not match and move nothing`, async ({
    page,
    request,
  }) => {
    const t = await truth(request);
    await openGraph(page, w, h);
    const before = await cardTransforms(page);

    await pickScope(page, 'Critical path');
    for (const id of t.activityIds) {
      const on = t.computed[id]?.onCriticalPath === true;
      const o = await laneOpacity(page, id);
      if (on) expect(o, id).toBe(1);
      else expect(o, id).toBeLessThan(1);
    }
    expect(await cardTransforms(page)).toEqual(before);

    await pickScope(page, 'Near-critical');
    for (const id of t.activityIds) {
      const c = t.computed[id];
      const near = c !== undefined && !c.onCriticalPath && c.totalFloat <= 5;
      const o = await laneOpacity(page, id);
      if (near) expect(o, id).toBe(1);
      else expect(o, id).toBeLessThan(1);
    }
    expect(await cardTransforms(page)).toEqual(before);
  });
}
