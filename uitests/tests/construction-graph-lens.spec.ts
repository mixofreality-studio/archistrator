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
 * DISPATCH SAFETY: nothing here may dispatch. The context-level guard
 * (support/dispatchGuard) aborts every non-GET before any navigation, and no
 * test clicks Begin or Run.
 *
 * Gated like the other construction specs: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy.
 */
import type { APIRequestContext, Locator, Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';
/** The card and lane testid families (TESTID.constructionGraphCard / Lane). */
const CARD_ID = /^construction-graph-card-/;
const LANE_ID = /^construction-graph-lane-/;

test.beforeEach(async ({ request }) => {
  // DISPATCH SAFETY: support/dispatchGuard's context route aborts every
  // non-GET before any navigation (and any request a test still holds, at teardown).
  await requireServer(request, BASE);
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
      Array.from(root.querySelectorAll('.react-flow__node-graphCard')).map((n) => [
        n.getAttribute('data-id') ?? '',
        (n as HTMLElement).style.transform,
      ])
    )
  );
}

async function edgeIds(page: Page): Promise<string[]> {
  return canvas(page).evaluate((root) =>
    Array.from(root.querySelectorAll('.react-flow__edge')).map(
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
  // Layered components only: a utility is never hollow (designer P1-5 / Q3).
  const hollow = t.components.filter((c) => c.layer !== 'utility' && !built.has(c.id));

  expect((await cardFacts(page)).filter((c) => c.hollow === 'true')).toHaveLength(hollow.length);
  for (const c of hollow) {
    const card = page.getByTestId(TESTID.constructionGraphCard(c.id));
    await expect(card, c.id).toHaveAttribute('data-hollow', 'true');
    await expect(card, c.id).toContainText('no activity');
  }

  // The key counts exactly those — "6 components with no activity (dashed)" today.
  await page.getByTestId(TESTID.constructionGraphKeyButton).click();
  await expect(page.getByTestId(TESTID.constructionGraphKey)).toContainText(
    `${String(hollow.length)} ${hollow.length === 1 ? 'component' : 'components'} with no activity (dashed)`
  );
  await page.keyboard.press('Escape');
});

test('P1-5: a utility is a solid, muted card — no "no activity", and its hover says why', async ({
  page,
  request,
}) => {
  const t = await truth(request);
  await openGraph(page);
  const utilities = t.components.filter((c) => c.layer === 'utility');
  expect(utilities.length).toBeGreaterThan(0);
  for (const u of utilities) {
    const card = page.getByTestId(TESTID.constructionGraphCard(u.id));
    await expect(card, u.id).toHaveAttribute('data-hollow', 'false');
    await expect(card, u.id).toHaveAttribute('data-utility', 'true');
    await expect(card, u.id).not.toContainText('no activity');
    const borderStyle = await card.evaluate((el) => getComputedStyle(el).borderLeftStyle);
    expect(borderStyle, u.id).toBe('solid');
  }
  const first = utilities[0]?.id ?? '';
  await page.getByTestId(TESTID.constructionGraphCard(first)).hover();
  await expect(page.getByTestId(TESTID.constructionGraphHoverCard)).toContainText(
    'Utility — shared infrastructure. The Method plans no activity for a utility.'
  );
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

test('a lane opens the shared pane, whose run action is present and disabled with its reason', async ({
  page,
  request,
}) => {
  const t = await truth(request);
  const id = t.activities[0]?.id ?? '';
  await openGraph(page);
  await page.getByTestId(TESTID.constructionGraphLane(id)).click();
  await expect(page.getByTestId(TESTID.constructionDetailPane)).toBeVisible();
  // Present in every state, in every lens's pane (§9.3 AC3), and disabled with its
  // reason until the console can start work (detailPaneState.RUN_NOT_WIRED_REASON).
  // The graph shares the list's pane, so it says what the list says.
  const run = page.getByTestId(TESTID.constructionDetailActionRun);
  await expect(run).toBeVisible();
  await expect(run).toBeDisabled();
  await expect(run).toHaveAttribute('data-reason', /not wired/i);
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

test('P0-1: the hover card never launders a reconstructed lane — stamp + chip per line, unknown unmarked', async ({
  page,
}) => {
  await openGraph(page);
  // Every card that carries lanes, read from the canvas itself.
  const cards = await page.getByTestId(CARD_ID).evaluateAll((els) =>
    els
      .filter((e) => Number(e.getAttribute('data-lanes') ?? '0') > 0)
      .map((e) => ({
        id: (e.getAttribute('data-testid') ?? '').replace('construction-graph-card-', ''),
        lanes: Array.from(e.querySelectorAll('[data-testid^="construction-graph-lane-"]')).map((l) => ({
          id: (l.getAttribute('data-testid') ?? '').replace('construction-graph-lane-', ''),
          origin: l.getAttribute('data-provenance'),
          state: l.getAttribute('data-state'),
        })),
      }))
  );
  const reconstructed = (o: string | null): boolean => o === 'backfilled' || o === 'synthesized';
  const withRecon = cards.find((c) => c.lanes.some((l) => reconstructed(l.origin)));
  const unknownOnly = cards.find((c) => c.lanes.every((l) => l.origin === 'unknown'));

  for (const c of [withRecon, unknownOnly]) {
    if (c === undefined) continue;
    await page.getByTestId(TESTID.constructionGraphCard(c.id)).hover();
    const hover = page.getByTestId(TESTID.constructionGraphHoverCard);
    await expect(hover, c.id).toBeVisible();
    const anyRecon = c.lanes.some((l) => reconstructed(l.origin));
    // One stamp in the header when any lane is reconstructed, plus one per reconstructed line.
    const expected = (anyRecon ? 1 : 0) + c.lanes.filter((l) => reconstructed(l.origin)).length;
    await expect(hover.getByTestId(TESTID.constructionProvenanceBadge), c.id).toHaveCount(expected);
    for (const l of c.lanes) {
      const line = hover.getByTestId(TESTID.constructionGraphHoverLane(l.id));
      await expect(line, l.id).toHaveAttribute('data-provenance', l.origin ?? '');
      await expect(line.getByTestId(TESTID.constructionProvenanceBadge), l.id).toHaveCount(
        reconstructed(l.origin) ? 1 : 0
      );
      // A reconstructed line also carries the lane's state chip, so the stamp
      // qualifies the "passed" it sits beside; no other line carries one.
      const chip = await line.evaluate(
        (el) => el.querySelector('[data-chip-state]')?.getAttribute('data-chip-state') ?? null
      );
      const chipBearing = ['passed', 'failed', 'running', 'awaitingHuman'].includes(l.state ?? '');
      expect(chip, `${l.id} chip`).toBe(reconstructed(l.origin) && chipBearing ? l.state : null);
    }
    await page.mouse.move(2, 2);
  }
  expect(withRecon, 'a card with a reconstructed lane exists').toBeDefined();
});

test('P1-3: at LOD-1 a segment shows a short code; its full name is its title and aria-label', async ({
  page,
}) => {
  await openGraph(page);
  // Hovering a card puts it at LOD-1; take the first card whose lane has a
  // canonical Service requirements segment.
  const target = await page.getByTestId(LANE_ID).evaluateAll((els) =>
    els
      .map((e) => (e.getAttribute('data-testid') ?? '').replace('construction-graph-lane-', ''))
      .find((id) => id.startsWith('C-'))
  );
  expect(target).toBeDefined();
  const id = target ?? '';
  await page
    .getByTestId(CARD_ID)
    .filter({ has: page.getByTestId(TESTID.constructionGraphLane(id)) })
    .hover();
  const seg = page.getByTestId(TESTID.constructionGraphSegment(id, 'requirements'));
  const facts = await seg.evaluate((el) => ({
    code: el.querySelector('[data-segment-code]')?.textContent ?? null,
    title: el.getAttribute('title') ?? '',
    aria: el.getAttribute('aria-label') ?? '',
  }));
  expect(facts.code).toBe('REQ');
  expect(facts.title).toMatch(/^Requirements · /);
  expect(facts.aria).toMatch(/^Requirements · /);
});

test('P1-4: a filter is never silent — "N of 29 match · Clear filters", whole cards dim', async ({
  page,
  request,
}) => {
  const t = await truth(request);
  await openGraph(page);
  // At rest: no status line, and no card is filter-dimmed.
  await expect(page.getByTestId(TESTID.constructionGraphFilterStatus)).toHaveCount(0);
  const dimmedAtRest = await page
    .getByTestId(CARD_ID)
    .evaluateAll((els) => els.filter((e) => e.getAttribute('data-filter-dimmed') === 'true').length);
  expect(dimmedAtRest).toBe(0);

  // A search that matches some lanes: the count is the lanes left lit.
  const search = page.getByPlaceholder(/Search id, title or component/);
  await search.fill('manager');
  const status = page.getByTestId(TESTID.constructionGraphFilterStatus);
  await expect(status).toBeVisible();
  const matched = Number(await status.getAttribute('data-matched'));
  expect(matched).toBeGreaterThan(0);
  await expect(status).toContainText(
    `${String(matched)} of ${String(t.activities.length)} match`
  );
  await expect(status).toContainText('Clear filters');
  // Hollow and utility cards dim whenever a filter is active; so does any card
  // none of whose lanes match — and every card with a matching lane stays lit.
  const cards = await page.getByTestId(CARD_ID).evaluateAll((els) =>
    els.map((e) => ({
      id: e.getAttribute('data-testid') ?? '',
      row: e.getAttribute('data-row'),
      hollow: e.getAttribute('data-hollow'),
      lanes: Number(e.getAttribute('data-lanes') ?? '0'),
      dimmed: e.getAttribute('data-filter-dimmed'),
    }))
  );
  for (const c of cards) {
    if (c.lanes === 0) expect(c.dimmed, c.id).toBe('true');
  }
  expect(cards.filter((c) => c.dimmed === 'false').length).toBeGreaterThan(0);

  // Nothing matching reuses the list's own copy.
  await search.fill('zzz-no-such-activity');
  await expect(status).toContainText('No activity matches “zzz-no-such-activity”.');
  // One mark of punctuation, not two: a sentence that ends in its own full stop
  // takes no " · " before "Clear filters" (designer re-check).
  expect((await status.textContent()) ?? '').not.toMatch(/[.!?…]\s*·/);

  // Clear filters restores the canvas.
  await page.getByTestId(TESTID.constructionGraphClearFilters).click();
  await expect(status).toHaveCount(0);
  await expect(search).toHaveValue('');
});

test('Sort and "Expand to current phase" are list-only', async ({ page }) => {
  await openGraph(page);
  await expect(page.getByTestId(TESTID.constructionLensExpandToPhase)).toBeDisabled();
  const sort = page.getByTestId(TESTID.constructionLensSort).getByRole('combobox');
  await expect(sort).toHaveAttribute('aria-disabled', 'true');

  await page.getByTestId(TESTID.constructionLensButton('list')).click();
  await expect(sort).not.toHaveAttribute('aria-disabled', 'true');
});

test('hovering a milestone chip\'s stamp opens ONE tooltip, carrying the count and the provenance (designer re-check 10)', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1366, height: 768 });
  await gotoApp(page, GRAPH);
  await expect(page.getByTestId(TESTID.constructionGraphRibbon)).toBeVisible();
  const ribbon = page.getByTestId(TESTID.constructionGraphRibbon);
  const stamped = ribbon.getByTestId(TESTID.constructionProvenanceBadge).first();
  test.skip((await stamped.count()) === 0, 'no milestone carries reconstructed evidence');
  // The stamp takes no pointer events (the fix), so Playwright's actionability
  // check would refuse it; a real pointer simply rests over it.
  const box = await stamped.boundingBox();
  expect(box).not.toBeNull();
  await page.mouse.move((box?.x ?? 0) + (box?.width ?? 0) / 2, (box?.y ?? 0) + (box?.height ?? 0) / 2);
  const tips = page.getByRole('tooltip');
  await expect(tips).toHaveCount(1);
  await expect(tips.first()).toContainText(/feeder|Gates/);
  await expect(tips.first()).toContainText(/reconstruct/i);
});

test('a reconstructed milestone chip\'s tooltip stays short — never a feeder\'s basis text (graph round 3)', async ({
  page,
}) => {
  // The regression this pins: 3938e6c (polish 10.10) folded EVERY feeder's
  // full basis prose into the chip's one tooltip — 948-1752 characters,
  // truncated mid-word, running off-screen at 1366x768. The fix replaces that
  // with the count sentence plus one fixed line naming where a basis actually
  // lives (a feeder), never quoting one here.
  await page.setViewportSize({ width: 1366, height: 768 });
  await gotoApp(page, GRAPH);
  const ribbon = page.getByTestId(TESTID.constructionGraphRibbon);
  await expect(ribbon).toBeVisible();
  const stamped = ribbon.getByTestId(TESTID.constructionProvenanceBadge).first();
  test.skip((await stamped.count()) === 0, 'no milestone carries reconstructed evidence');
  const box = await stamped.boundingBox();
  expect(box).not.toBeNull();
  await page.mouse.move((box?.x ?? 0) + (box?.width ?? 0) / 2, (box?.y ?? 0) + (box?.height ?? 0) / 2);
  const tip = page.getByRole('tooltip');
  await expect(tip).toHaveCount(1);
  const text = (await tip.first().innerText()).trim();
  expect(text.length, `tooltip text: ${JSON.stringify(text)}`).toBeLessThan(200);
  expect(text).not.toContain('…');
  expect(text).toContain('Select a feeder for its basis.');
});

test('hovering a card\'s ≈ RECONSTRUCTED stamp opens exactly ONE popup — the hover card, never its own tooltip too (graph round 3)', async ({
  page,
}) => {
  await openGraph(page);
  const cards = await page.getByTestId(CARD_ID).evaluateAll((els) =>
    els
      .filter((e) => Number(e.getAttribute('data-lanes') ?? '0') > 0)
      .map((e) => ({
        id: (e.getAttribute('data-testid') ?? '').replace('construction-graph-card-', ''),
        reconstructed: Array.from(
          e.querySelectorAll('[data-testid^="construction-graph-lane-"]')
        ).some((l) => {
          const o = l.getAttribute('data-provenance');
          return o === 'backfilled' || o === 'synthesized';
        }),
      }))
  );
  const target = cards.find((c) => c.reconstructed);
  test.skip(target === undefined, 'no card carries reconstructed evidence');
  const card = page.getByTestId(TESTID.constructionGraphCard(target?.id ?? ''));
  // The card's OWN head stamp — never the (portalled) hover card's copy, which
  // lives outside this locator's DOM subtree.
  const headStamp = card.getByTestId(TESTID.constructionProvenanceBadge);
  await expect(headStamp).toHaveCount(1);
  const box = await headStamp.boundingBox();
  expect(box).not.toBeNull();
  await page.mouse.move((box?.x ?? 0) + (box?.width ?? 0) / 2, (box?.y ?? 0) + (box?.height ?? 0) / 2);
  // The hover card still opens (the stamp takes no pointer events, so the
  // pointer reads as resting on the card, not on nothing). MUI's bare Popper
  // (BasePopper) defaults its OWN root to role="tooltip" — the hover card is
  // legitimately one "tooltip" by that role alone, so the fix is pinned by
  // COUNT (exactly one, never a second from the badge's own MUI Tooltip
  // opening on top of it), not by absence.
  await expect(page.getByTestId(TESTID.constructionGraphHoverCard)).toBeVisible();
  // Give the badge's own MUI Tooltip every chance to open (its default hover
  // delay) before asserting the count never grows past the hover card itself.
  await page.waitForTimeout(400);
  const tips = page.getByRole('tooltip');
  await expect(tips).toHaveCount(1);
  // The one tooltip IS the hover card (id `construction-graph-hover-<cardId>`),
  // never a second, separate popper the badge's own Tooltip would have opened.
  await expect(tips.first()).toHaveId(`construction-graph-hover-${target?.id ?? ''}`);
  await expect(tips.first().getByTestId(TESTID.constructionGraphHoverCard)).toBeVisible();
});

test('segment codes: shown only where they FIT — none wider than its segment, no "…", rows never jump (designer re-check blocker)', async ({
  page,
}) => {
  await openGraph(page);
  // LOD-1 everywhere: zoom in with the controls until the viewport's scale is
  // clearly past 0.8 (four clicks land at ~0.81, on the threshold, mid-animation).
  const scale = async (): Promise<number> =>
    canvas(page).evaluate((root) => {
      const m = /scale\(([\d.]+)\)/.exec(
        root.querySelector('.react-flow__viewport')?.getAttribute('style') ?? ''
      );
      return m === null ? 0 : Number(m[1]);
    });
  for (let i = 0; i < 8 && (await scale()) < 0.85; i += 1) {
    await page.getByRole('button', { name: /zoom in/i }).click();
    await page.waitForTimeout(350);
  }
  await page.waitForTimeout(500);
  expect(await scale(), 'zoomed past the LOD-1 threshold').toBeGreaterThanOrEqual(0.8);
  const facts = await canvas(page).evaluate((root) => {
    const rows = Array.from(root.querySelectorAll('[data-segment-code-fits]'));
    return {
      rows: rows.length,
      shown: rows.filter((r) => r.getAttribute('data-segment-code-fits') === 'true').length,
      heights: [...new Set(rows.map((r) => Math.round((r as HTMLElement).offsetHeight)))],
      tooWide: rows.flatMap((r) => {
        const code = r.querySelector('[data-segment-code]');
        if (code === null) return [];
        const codeW = (code as HTMLElement).offsetWidth;
        const segW = (r as HTMLElement).clientWidth;
        return codeW > segW + 0.5 ? [`${code.textContent ?? ''} ${String(codeW)}>${String(segW)}`] : [];
      }),
      letterSpacings: [...new Set(rows.map((r) => getComputedStyle(r).letterSpacing))],
      ellipsized: rows.filter((r) => getComputedStyle(r).textOverflow === 'ellipsis').length,
      text: root.textContent ?? '',
    };
  });
  expect(facts.rows, 'code rows exist at LOD-1').toBeGreaterThan(0);
  expect(facts.shown, 'some codes fit and show').toBeGreaterThan(0);
  expect(facts.tooWide, 'no rendered code is wider than its segment').toEqual([]);
  expect(facts.text, 'no ellipsis anywhere on the canvas').not.toContain('…');
  expect(facts.ellipsized, 'no code row truncates with an ellipsis').toBe(0);
  expect(facts.letterSpacings.every((l) => l === '0px' || l === 'normal')).toBe(true);
  expect(facts.heights, 'every code row keeps one height, shown or not').toEqual([7]);
});

test('segment codes re-measure once a font finishes loading — a ResizeObserver alone misses it (graph round 3)', async ({
  page,
}) => {
  // The regression this pins: GraphNodes.tsx's SegmentCode only re-measured
  // inside a ResizeObserver callback, which never fires for a web font
  // finishing load — the segment's own box does not resize, only the glyphs a
  // re-flowed font draws inside it do. A code that fit the fallback font could
  // go on to clip, unnoticed, once the real font swapped in.
  //
  // Simulated here with a `measureText` override standing in for that font
  // swap's effect on measured width, flipped via `document.fonts`' own
  // `loadingdone` event — the exact signal the fix subscribes to (the other,
  // `fonts.ready`, is covered by a node:test against a fake FontFaceSet in
  // segmentCode.test.ts, where its timing is fully controllable).
  await page.addInitScript(() => {
    const w = window as typeof window & { __wideMetrics?: boolean };
    w.__wideMetrics = false;
    const orig = CanvasRenderingContext2D.prototype.measureText;
    CanvasRenderingContext2D.prototype.measureText = function (
      this: CanvasRenderingContext2D,
      text: string
    ): TextMetrics {
      if (w.__wideMetrics) return { width: 100_000 } as unknown as TextMetrics;
      return orig.call(this, text);
    };
  });
  await openGraph(page);
  const scale = async (): Promise<number> =>
    canvas(page).evaluate((root) => {
      const m = /scale\(([\d.]+)\)/.exec(
        root.querySelector('.react-flow__viewport')?.getAttribute('style') ?? ''
      );
      return m === null ? 0 : Number(m[1]);
    });
  for (let i = 0; i < 8 && (await scale()) < 0.85; i += 1) {
    await page.getByRole('button', { name: /zoom in/i }).click();
    await page.waitForTimeout(350);
  }
  await page.waitForTimeout(500);
  expect(await scale(), 'zoomed past the LOD-1 threshold').toBeGreaterThanOrEqual(0.8);

  const shownCount = async (): Promise<number> =>
    canvas(page).evaluate(
      (root) => root.querySelectorAll('[data-segment-code-fits="true"]').length
    );
  const before = await shownCount();
  expect(before, 'some codes fit under the (real) fallback-font metrics').toBeGreaterThan(0);

  // The swap: metrics change, and the ONLY signal fired is `loadingdone` — no
  // segment resizes, so a ResizeObserver alone would never see this.
  await page.evaluate(() => {
    (window as typeof window & { __wideMetrics?: boolean }).__wideMetrics = true;
    document.fonts.dispatchEvent(new Event('loadingdone'));
  });
  await expect
    .poll(shownCount, {
      message: 'every code re-measures against the new (huge) width and stops fitting',
    })
    .toBe(0);
});

test('a segment code unsubscribes its font-metric listener on unmount — no leak (graph round 3)', async ({
  page,
}) => {
  // Counts document.fonts' own 'loadingdone' add/removeEventListener calls, so
  // an un-mounted SegmentCode that forgot its cleanup (the subscription stays
  // registered forever) is visible even though nothing it renders looks wrong.
  await page.addInitScript(() => {
    const counts = { add: 0, remove: 0 };
    (window as typeof window & { __fontListenerCounts?: typeof counts }).__fontListenerCounts =
      counts;
    const fonts = document.fonts;
    const origAdd = fonts.addEventListener.bind(fonts);
    const origRemove = fonts.removeEventListener.bind(fonts);
    fonts.addEventListener = ((
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | AddEventListenerOptions
    ): void => {
      if (type === 'loadingdone') counts.add += 1;
      origAdd(type, listener, options);
    }) as typeof fonts.addEventListener;
    fonts.removeEventListener = ((
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | EventListenerOptions
    ): void => {
      if (type === 'loadingdone') counts.remove += 1;
      origRemove(type, listener, options);
    }) as typeof fonts.removeEventListener;
  });
  await openGraph(page);
  const listenerCounts = async (): Promise<{ add: number; remove: number }> =>
    page.evaluate(
      () =>
        (window as typeof window & { __fontListenerCounts?: { add: number; remove: number } })
          .__fontListenerCounts ?? { add: 0, remove: 0 }
    );

  const scale = async (): Promise<number> =>
    canvas(page).evaluate((root) => {
      const m = /scale\(([\d.]+)\)/.exec(
        root.querySelector('.react-flow__viewport')?.getAttribute('style') ?? ''
      );
      return m === null ? 0 : Number(m[1]);
    });
  for (let i = 0; i < 8 && (await scale()) < 0.85; i += 1) {
    await page.getByRole('button', { name: /zoom in/i }).click();
    await page.waitForTimeout(350);
  }
  await page.waitForTimeout(500);
  expect(await scale(), 'zoomed past the LOD-1 threshold').toBeGreaterThanOrEqual(0.8);

  // The zoom-in animation itself can pass through the LOD-1 threshold more
  // than once (mounting and unmounting SegmentCode along the way), so `add`
  // and `remove` both grow during it — that churn is expected, not a leak.
  // The invariant is net subscriptions: at rest with codes on screen, more
  // were added than have been removed.
  const mounted = await listenerCounts();
  expect(
    mounted.add - mounted.remove,
    'at least one SegmentCode is currently mounted and subscribed'
  ).toBeGreaterThan(0);

  // Fit View drops well under the LOD-1 threshold, unmounting every SegmentCode.
  await page.getByRole('button', { name: /fit view/i }).click();
  await page.waitForTimeout(500);
  expect(await scale(), 'back under the LOD-1 threshold').toBeLessThan(0.8);

  await expect
    .poll(async () => (await listenerCounts()).add - (await listenerCounts()).remove, {
      message:
        'every loadingdone listener ever added is removed once its SegmentCode unmounts — net zero, no leak',
    })
    .toBe(0);
});
