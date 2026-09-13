/**
 * construction-graph-lens.spec — the GRAPH lens (Stage D), black-box.
 *
 * The lens draws the committed architecture layer by layer, each component
 * card carrying its activity's lifecycle. Every expectation below is computed
 * from the project read over the wire (get-project) — the architecture, the
 * activity rows, the milestones — never hardcoded, so the spec follows the
 * plan as it changes and pins the RULES, not today's numbers:
 *
 *   - every committed activity has exactly one lane, placed by the SERVER's
 *     layer for the activity (spec R5), project-wide ones in System-wide;
 *   - a component no activity builds is hollow and says so;
 *   - no edge touches a utility, and the utilities sit in the side bar;
 *   - the layering check states the counts the architecture implies;
 *   - the same state lays out in the same place, reload after reload, and
 *     "Observed only" (provenance) moves nothing;
 *   - selection and viewport survive a remount (a lens round trip);
 *   - a lane opens the shared pane, whose run action is present and enabled;
 *   - the milestone ribbon never shows a count over unobserved evidence (§9.2);
 *   - Sort and "Expand to current phase" are list-only.
 *
 * Selectors are published testids (a RegExp over a testid family where a
 * whole set is asserted) or roles. React Flow's own structure — node
 * transforms, edges, the viewport — is read inside one evaluate on the canvas
 * testid, because that structure has no testid or role of its own.
 *
 * DISPATCH SAFETY: nothing here may dispatch. Every test aborts
 * execute-next-activity before it navigates, and no test clicks Begin or Run.
 *
 * Gated like the other construction specs: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy.
 */
import { test, expect, type APIRequestContext, type Locator, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';
/** The card and lane testid families (TESTID.constructionGraphCard / Lane). */
const CARD_ID = /^construction-graph-card-/;
const LANE_ID = /^construction-graph-lane-/;

test.beforeEach(async ({ page, request }) => {
  await page.route('**/execute-next-activity/**', (route) => route.abort());
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

// ---------------------------------------------------------------------------
// The truth, read over the wire
// ---------------------------------------------------------------------------

interface WireActivity {
  id: string;
  layer: string | undefined;
  layerBand: string | undefined;
  componentId: string | undefined;
}
interface Truth {
  activities: WireActivity[];
  components: { id: string; layer: string }[];
  relationships: { from: string; to: string; mode: string }[];
  milestones: { id: string; dependsOn: string[] }[];
}
interface WireProject {
  ActivityConstruction: Record<string, { layer?: string; layerBand?: string }>;
  Slots: { kind: string; model: { model: Record<string, unknown> } }[];
}

async function truth(request: APIRequestContext): Promise<Truth> {
  const res = await request.get(`${BASE}/api/v1/system-design/get-project/archistrator`);
  expect(res.ok()).toBe(true);
  const body = (await res.json()) as WireProject;
  const model = (kind: string): Record<string, unknown> =>
    body.Slots.find((s) => s.kind === kind)?.model.model ?? {};
  const system = model('system') as {
    components?: { id: string; layer: string }[];
    relationships?: { from: string; to: string; mode: string }[];
  };
  const list = model('activityList') as {
    activities?: { name: string; componentId?: string }[];
  };
  const network = model('network') as {
    milestones?: { id: string; dependsOn?: string[] | null }[];
  };
  const componentOf = new Map((list.activities ?? []).map((a) => [a.name, a.componentId]));
  return {
    activities: Object.entries(body.ActivityConstruction).map(([id, row]) => ({
      id,
      layer: row.layer,
      layerBand: row.layerBand,
      componentId: componentOf.get(id),
    })),
    components: system.components ?? [],
    relationships: system.relationships ?? [],
    milestones: (network.milestones ?? []).map((m) => ({
      id: m.id,
      dependsOn: m.dependsOn ?? [],
    })),
  };
}

const RANK: Record<string, number> = {
  client: 0,
  manager: 1,
  engine: 2,
  resourceAccess: 3,
  resource: 4,
};

// ---------------------------------------------------------------------------
// Page helpers
// ---------------------------------------------------------------------------

function canvas(page: Page): Locator {
  return page.getByTestId(TESTID.constructionGraphCanvas);
}

async function openGraph(page: Page): Promise<void> {
  await gotoApp(page, GRAPH);
  await expect(canvas(page)).toBeVisible();
  await expect(page.getByTestId(CARD_ID).first()).toBeVisible();
}

interface CardFacts {
  id: string;
  row: string | null;
  hollow: string | null;
  top: number;
  left: number;
  right: number;
}

async function cardFacts(page: Page): Promise<CardFacts[]> {
  return page.getByTestId(CARD_ID).evaluateAll((els) =>
    els.map((e) => {
      const r = e.getBoundingClientRect();
      return {
        id: e.getAttribute('data-testid') ?? '',
        row: e.getAttribute('data-row'),
        hollow: e.getAttribute('data-hollow'),
        top: r.top,
        left: r.left,
        right: r.right,
      };
    })
  );
}

/** Every card's flow-space position (xyflow's node transform), by card id. */
async function cardPositions(page: Page): Promise<Record<string, string>> {
  return canvas(page).evaluate((root) =>
    Object.fromEntries(
      [...root.querySelectorAll('.react-flow__node-graphCard')].map((n) => [
        n.getAttribute('data-id') ?? '',
        (n as HTMLElement).style.transform,
      ])
    )
  );
}

async function edgeIds(page: Page): Promise<string[]> {
  return canvas(page).evaluate((root) =>
    [...root.querySelectorAll('.react-flow__edge')].map(
      (e) => e.getAttribute('data-id') ?? e.getAttribute('data-testid') ?? ''
    )
  );
}

async function alarmEdgeCount(page: Page): Promise<number> {
  return canvas(page).evaluate(
    (root) => root.querySelectorAll('.graph-edge-up, .graph-edge-sideways').length
  );
}

async function viewportTransform(page: Page): Promise<string | null> {
  return canvas(page).evaluate(
    (root) => root.querySelector('.react-flow__viewport')?.getAttribute('style') ?? null
  );
}

// ---------------------------------------------------------------------------
// Placement and coverage
// ---------------------------------------------------------------------------

test('every committed activity has exactly one lane, placed by its own layer', async ({
  page,
  request,
}) => {
  const t = await truth(request);
  await openGraph(page);

  await expect(page.getByTestId(LANE_ID)).toHaveCount(t.activities.length);
  for (const a of t.activities) {
    const lane = page.getByTestId(TESTID.constructionGraphLane(a.id));
    await expect(lane, a.id).toHaveCount(1);
    const row = a.layerBand === 'layered' && a.layer !== undefined ? a.layer : 'systemWide';
    await expect(page.getByTestId(CARD_ID).filter({ has: lane }), a.id).toHaveAttribute(
      'data-row',
      row
    );
  }

  // A Client-layer activity is DRAWN above every Manager (R5's trap, geometrically).
  const clientActivity = t.activities.find(
    (a) => a.layerBand === 'layered' && a.layer === 'client'
  );
  if (clientActivity !== undefined) {
    const clientBox = await page
      .getByTestId(CARD_ID)
      .filter({
        has: page.getByTestId(TESTID.constructionGraphLane(clientActivity.id)),
      })
      .boundingBox();
    expect(clientBox).not.toBeNull();
    const managers = (await cardFacts(page)).filter((c) => c.row === 'manager');
    expect(managers.length).toBeGreaterThan(0);
    for (const m of managers) expect(clientBox?.y ?? Number.POSITIVE_INFINITY).toBeLessThan(m.top);
  }
});

test('every component no activity builds is hollow and says so', async ({ page, request }) => {
  const t = await truth(request);
  await openGraph(page);

  const layerOf = new Map(t.components.map((c) => [c.id, c.layer]));
  const built = new Set(
    t.activities
      .filter(
        (a) =>
          a.layerBand === 'layered' &&
          a.componentId !== undefined &&
          layerOf.get(a.componentId) === a.layer
      )
      .map((a) => a.componentId)
  );
  const hollow = t.components.filter((c) => !built.has(c.id));

  expect((await cardFacts(page)).filter((c) => c.hollow === 'true')).toHaveLength(hollow.length);
  for (const c of hollow) {
    const card = page.getByTestId(TESTID.constructionGraphCard(c.id));
    await expect(card, c.id).toHaveAttribute('data-hollow', 'true');
    await expect(card, c.id).toContainText('no activity');
  }
});

test('no edge touches a utility, and the utilities sit in the side bar', async ({
  page,
  request,
}) => {
  const t = await truth(request);
  await openGraph(page);

  const utilities = new Set(t.components.filter((c) => c.layer === 'utility').map((c) => c.id));
  const known = new Set(t.components.map((c) => c.id));
  const expected = t.relationships.filter(
    (r) => known.has(r.from) && known.has(r.to) && !utilities.has(r.from) && !utilities.has(r.to)
  ).length;

  const ids = await edgeIds(page);
  expect(ids).toHaveLength(expected);
  for (const raw of ids) {
    const [from = '', to = ''] = raw
      .replace(/^rf__edge-/, '')
      .replace(/#\d+$/, '')
      .split('->');
    expect(utilities.has(from), raw).toBe(false);
    expect(utilities.has(to), raw).toBe(false);
  }

  const facts = await cardFacts(page);
  const bar = facts.filter((c) => c.row === 'utility');
  const rest = facts.filter((c) => c.row !== 'utility');
  expect(bar).toHaveLength(utilities.size);
  const widest = Math.max(...rest.map((c) => c.right));
  for (const u of bar) expect(u.left, u.id).toBeGreaterThan(widest);
});

test('the layering check states the counts the architecture implies', async ({ page, request }) => {
  const t = await truth(request);
  await openGraph(page);

  const layerOf = new Map(t.components.map((c) => [c.id, c.layer]));
  let up = 0;
  let sideways = 0;
  let sanctioned = 0;
  for (const r of t.relationships) {
    const from = RANK[layerOf.get(r.from) ?? ''];
    const to = RANK[layerOf.get(r.to) ?? ''];
    if (from === undefined || to === undefined) continue;
    if (to < from) up += 1;
    else if (to === from) {
      if (layerOf.get(r.from) === 'manager' && r.mode === 'queued') sanctioned += 1;
      else sideways += 1;
    }
  }
  const check = page.getByTestId(TESTID.constructionGraphLayerCheck);
  await expect(check).toContainText(`${String(up)} upward · ${String(sideways)} sideways`);
  if (sanctioned > 0) {
    await expect(check).toContainText(`${String(sanctioned)} queued Manager→Manager`);
  }
  expect(await alarmEdgeCount(page)).toBe(up + sideways);
});

// ---------------------------------------------------------------------------
// Determinism and memory
// ---------------------------------------------------------------------------

test('the same state lays out in the same place, reload after reload', async ({ page }) => {
  await openGraph(page);
  const first = await cardPositions(page);
  expect(Object.keys(first).length).toBeGreaterThan(0);
  await page.reload();
  await expect(page.getByTestId(CARD_ID).first()).toBeVisible();
  expect(await cardPositions(page)).toEqual(first);
});

test('"Observed only" keeps every lane, strips the hatch, and moves nothing', async ({ page }) => {
  await openGraph(page);
  const lanes = await page.getByTestId(LANE_ID).count();
  const positions = await cardPositions(page);

  await page.getByRole('switch', { name: 'Observed only' }).click();
  await expect(page.getByTestId(LANE_ID)).toHaveCount(lanes);
  const origins = await page
    .getByTestId(LANE_ID)
    .evaluateAll((els) => els.map((e) => e.getAttribute('data-provenance')));
  expect(origins.filter((o) => o === 'backfilled' || o === 'synthesized')).toEqual([]);
  await expect(canvas(page).getByTestId(TESTID.constructionProvenanceBadge)).toHaveCount(0);
  expect(await cardPositions(page)).toEqual(positions);
});

test('selection and viewport survive a lens round trip', async ({ page, request }) => {
  const t = await truth(request);
  const target = t.activities.find((a) => a.layerBand === 'layered');
  expect(target).toBeDefined();
  const id = target?.id ?? '';
  await openGraph(page);

  const lane = page.getByTestId(TESTID.constructionGraphLane(id));
  await lane.click();
  await expect.poll(() => new URL(page.url()).searchParams.get('a')).toBe(id);
  await page.getByRole('button', { name: /zoom in/i }).click();
  await page.waitForTimeout(600);
  const before = await viewportTransform(page);

  await page.getByTestId(TESTID.constructionLensButton('list')).click();
  await expect(canvas(page)).toHaveCount(0);
  await page.getByTestId(TESTID.constructionLensButton('graph')).click();
  await expect(canvas(page)).toBeVisible();
  await page.waitForTimeout(600);

  expect(await viewportTransform(page)).toBe(before);
  await expect(lane).toHaveAttribute('data-selected', 'true');
  expect(new URL(page.url()).searchParams.get('a')).toBe(id);
});

// ---------------------------------------------------------------------------
// The pane, the ribbon, the list-only controls
// ---------------------------------------------------------------------------

test('a lane opens the shared pane, whose run action is present and enabled', async ({
  page,
  request,
}) => {
  const t = await truth(request);
  const id = t.activities[0]?.id ?? '';
  await openGraph(page);
  await page.getByTestId(TESTID.constructionGraphLane(id)).click();
  await expect(page.getByTestId(TESTID.constructionDetailPane)).toBeVisible();
  // Present and enabled in every state (§9 AC3) — and never clicked here.
  await expect(page.getByTestId(TESTID.constructionDetailActionRun)).toBeEnabled();
});

test('the ribbon shows every milestone and no count over unobserved evidence', async ({
  page,
  request,
}) => {
  const t = await truth(request);
  await openGraph(page);
  for (const m of t.milestones) {
    const chip = page.getByTestId(TESTID.constructionGraphMilestone(m.id));
    await expect(chip, m.id).toBeVisible();
    if (m.dependsOn.length === 0) {
      await expect(chip, m.id).toContainText('gates');
    } else if ((await chip.getAttribute('data-provenance')) !== 'observed') {
      // §9.2: a count over reconstructed or unrecorded evidence is never shown.
      await expect(chip, m.id).toContainText('—');
      await expect(chip, m.id).not.toContainText(/\d+\/\d+/);
    }
  }
});

test('Sort and "Expand to current phase" are list-only', async ({ page }) => {
  await openGraph(page);
  await expect(page.getByTestId(TESTID.constructionLensExpandToPhase)).toBeDisabled();
  const sort = page.getByTestId(TESTID.constructionLensSort).getByRole('combobox');
  await expect(sort).toHaveAttribute('aria-disabled', 'true');

  await page.getByTestId(TESTID.constructionLensButton('list')).click();
  await expect(sort).not.toHaveAttribute('aria-disabled', 'true');
});
