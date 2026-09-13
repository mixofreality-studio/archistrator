/**
 * construction-integration-pending.spec — INTEGRATION-PENDING rows (architect (D),
 * D.3; the designer's final-pass blocker and items 1-3, 6).
 *
 * The backfill built C-billing-manager and C-system-design-manager against their
 * dependencies' contracts and left Integration waiting until every dependency is
 * Done (62efcafe). Their head-state reads in-review, which every lens rendered as
 * RUNNING, and Begin sat on "Construction running…" with no pump anywhere. The
 * server now marks such a row `pendingResume {fromPhase, waitsOn[]}`; these cases
 * pin the surface reading it, end to end, on the seeded state:
 *
 *   - the WAITING state: both rows read pending, never running, in the list, the
 *     graph and the pane; the pane says what each waits on; Begin reads an enabled
 *     "Begin construction"; Expand to current phase has nothing to open;
 *   - WHERE it waits: the Integration phase row, the lane's segment and the hover
 *     card say "waits on …";
 *   - the TASKS counts sum to the activity count and name the waiting rows;
 *   - at 500px the kind and provenance columns step aside so the ids read in full.
 *
 * The expectations are read from the server's own pendingResume, then checked
 * against the two committed rows by name, so a state that loses them fails here.
 *
 * DISPATCH SAFETY: the guarded `test` (support/dispatchGuard) aborts every non-GET
 * before navigation. Begin is read, never clicked.
 */
import type { APIRequestContext, Locator, Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const READ = `${BASE}/api/v1/system-design/get-project/archistrator`;

/** The two rows 62efcafe committed as integration pending. */
const BILLING = 'C-billing-manager';
const SYSTEM_DESIGN = 'C-system-design-manager';

const REASON: Record<string, string> = {
  notBuilt: 'not built',
  builtNotIntegrated: 'built but not integrated',
  milestoneNotReached: 'not reached',
  unresolved: 'unresolved',
};

interface WireDep {
  id: string;
  reason: string;
}
interface WireRow {
  ActivityID: string;
  pendingResume?: { fromPhase: string; waitsOn: WireDep[] };
}
interface Wire {
  ActivityConstruction?: Record<string, WireRow>;
}

interface Pending {
  id: string;
  fromPhase: string;
  waitsOn: WireDep[];
}

async function readWire(request: APIRequestContext): Promise<WireRow[]> {
  const res = await request.get(READ);
  expect(res.ok()).toBe(true);
  const wire = (await res.json()) as Wire;
  return Object.values(wire.ActivityConstruction ?? {});
}

async function readPending(request: APIRequestContext): Promise<Pending[]> {
  return (await readWire(request))
    .filter((r) => r.pendingResume !== undefined)
    .map((r) => ({
      id: r.ActivityID,
      fromPhase: r.pendingResume?.fromPhase ?? '',
      waitsOn: r.pendingResume?.waitsOn ?? [],
    }));
}

/** "Integration pending — waits on C-a (not built), …", from the wire. */
function sentenceFor(p: Pending): string {
  const tail =
    p.waitsOn.length === 0
      ? 'next in line'
      : `waits on ${p.waitsOn.map((d) => `${d.id} (${REASON[d.reason] ?? d.reason})`).join(', ')}`;
  return `Integration pending — ${tail}`;
}

let PENDING: Pending[] = [];

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
  PENDING = await readPending(request);
  // The committed state's two rows, both resuming at Integration. A state that
  // loses them fails here, loudly, rather than passing on nothing.
  expect(PENDING.map((p) => p.id).sort()).toEqual([BILLING, SYSTEM_DESIGN]);
  for (const p of PENDING) expect(p.fromPhase).toBe('integration');
});

function pendingFor(id: string): Pending {
  const p = PENDING.find((x) => x.id === id);
  if (p === undefined) throw new Error(`${id} is not pending in this read`);
  return p;
}

async function openList(page: Page, query = ''): Promise<void> {
  await page.setViewportSize({ width: 1366, height: 900 });
  await gotoApp(page, `/project/archistrator/construction?lens=list${query}`);
  await expect(page.getByTestId(TESTID.constructionListRow(BILLING))).toBeVisible();
}

async function openGraph(page: Page): Promise<void> {
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=graph');
  await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
}

/** A read whose C-billing-manager waits on nothing: next in line. */
async function serveNextInLine(page: Page): Promise<void> {
  await page.route(`**/system-design/get-project/archistrator**`, async (route) => {
    const response = await route.fetch();
    const wire = (await response.json()) as Wire;
    const row = wire.ActivityConstruction?.[BILLING];
    if (row?.pendingResume === undefined) throw new Error(`no pending ${BILLING} in the read`);
    row.pendingResume.waitsOn = [];
    await route.fulfill({ response, json: wire });
  });
}

// ---------------------------------------------------------------------------
// The WAITING state — not in flight, in every lens and the pane
// ---------------------------------------------------------------------------

/** The tier-1 row's own state slot: its kind (chip / notStarted / empty) and text.
 *  Read the way construction-fix-b.spec reads the grid's `data-slot` columns. */
async function stateSlotOf(row: Locator): Promise<{ kind: string | null; text: string }> {
  return row.evaluate((el) => {
    const slot = el.querySelector('[data-slot="state"]');
    return {
      kind: slot?.getAttribute('data-state-slot') ?? null,
      text: slot?.textContent ?? '',
    };
  });
}

test('list: both rows read Pending, never running, and carry the sentence', async ({ page }) => {
  await openList(page);
  for (const id of [BILLING, SYSTEM_DESIGN]) {
    const row = page.getByTestId(TESTID.constructionListRow(id));
    const slot = await stateSlotOf(row);
    expect(slot.kind).toBe('chip');
    // The list's fixed state slot keeps the state's own word; the pane and the
    // graph lane name the phase.
    expect(slot.text).toBe('PENDING');
    // The chip's accessible name (and tooltip) is the whole sentence.
    await expect(row.getByLabel(sentenceFor(pendingFor(id)))).toHaveCount(1);
  }
});

test('pane: the activity and its Integration phase read Integration pending, and say what it waits on', async ({
  page,
}) => {
  for (const [id, query] of [
    [BILLING, `&a=${BILLING}`],
    [SYSTEM_DESIGN, `&a=${SYSTEM_DESIGN}`],
    [BILLING, `&a=${BILLING}&p=integration`],
  ] as const) {
    await openList(page, query);
    const pane = page.getByTestId(TESTID.constructionDetailPane);
    await expect(pane.getByTestId(TESTID.constructionDetailPendingResume)).toHaveText(
      sentenceFor(pendingFor(id))
    );
    await expect(pane.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(
      'INTEGRATION PENDING'
    );
  }
  expect(sentenceFor(pendingFor(BILLING))).toBe(
    'Integration pending — waits on C-billing-state-access (not built), C-merchant-gateway-access (not built)'
  );
});

test('pane: a pending row that waits on nothing reads next in line', async ({ page }) => {
  await serveNextInLine(page);
  await openList(page, `&a=${BILLING}`);
  await expect(page.getByTestId(TESTID.constructionDetailPendingResume)).toHaveText(
    'Integration pending — next in line'
  );
});

test('Begin reads an enabled "Begin construction", and Expand has nothing in flight to open', async ({
  page,
}) => {
  await openList(page);
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText('Begin construction');
  await expect(begin).toBeEnabled();
  await expect(page.getByTestId(TESTID.constructionLensExpandToPhase)).toBeDisabled();
});

test('graph: the lanes read waiting, with the Integration pending chip', async ({ page }) => {
  await openGraph(page);
  for (const id of [BILLING, SYSTEM_DESIGN]) {
    const lane = page.getByTestId(TESTID.constructionGraphLane(id));
    await expect(lane).toHaveAttribute('data-state', 'waiting');
    await expect(lane.getByText(/^integration pending$/i)).toBeVisible();
  }
});

// ---------------------------------------------------------------------------
// WHERE it waits — the fromPhase row, the lane segment, the hover card
// ---------------------------------------------------------------------------

/** "waits on C-a, C-b" — the fromPhase row's own line. */
function phaseLineFor(p: Pending): string {
  return p.waitsOn.length === 0
    ? 'next in line'
    : `waits on ${p.waitsOn.map((d) => d.id).join(', ')}`;
}

test('list: the Integration phase row says what it waits on', async ({ page }) => {
  const p = pendingFor(BILLING);
  await openList(page, `&a=${BILLING}&p=integration`);
  await expect(page.getByTestId(TESTID.constructionListPendingLine(BILLING))).toHaveText(
    `· ${phaseLineFor(p)}`
  );
  expect(phaseLineFor(p)).toBe('waits on C-billing-state-access, C-merchant-gateway-access');
});

test('list: a pending row that waits on nothing reads next in line on its phase row', async ({
  page,
}) => {
  await serveNextInLine(page);
  await openList(page, `&a=${BILLING}&p=integration`);
  await expect(page.getByTestId(TESTID.constructionListPendingLine(BILLING))).toHaveText(
    '· next in line'
  );
});

test('graph: the Integration segment and the hover card say what it waits on', async ({ page }) => {
  await openGraph(page);
  for (const id of [BILLING, SYSTEM_DESIGN]) {
    await expect(
      page.getByTestId(TESTID.constructionGraphSegment(id, 'integration'))
    ).toHaveAttribute('aria-label', new RegExp(`· ${phaseLineFor(pendingFor(id))}$`));
  }
  const lane = page.getByTestId(TESTID.constructionGraphLane(BILLING));
  const box = await lane.boundingBox();
  if (box === null) throw new Error('no lane box');
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  const card = page.getByTestId(TESTID.constructionGraphHoverCard);
  await expect(card).toBeVisible();
  await expect(card.getByTestId(TESTID.constructionGraphHoverPending(BILLING))).toContainText(
    'waits on C-billing-state-access, C-merchant-gateway-access'
  );
});
