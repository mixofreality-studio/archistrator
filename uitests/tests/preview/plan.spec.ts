/**
 * THE PLAN (`/project/$projectId/plan?lens=list|graph|tasks`) — the one surface
 * that replaces the construction console's three lenses (spec §7.3/§7.4), driven
 * in the PREVIEW build over `uitests/preview-fixtures/web-client/plan/`.
 *
 * What each case pins, in the order the spec promises it:
 *   - LIST: Table 11-1 order, with the M0 divider between the last front-end
 *     activity and the first build row, and a row that opens the activity;
 *   - GRAPH: one tile per placed activity, the pinned row gutter reading
 *     FRONT END → RESOURCES → RESOURCE ACCESS → ENGINES → MANAGERS → CLIENTS →
 *     SYSTEM TESTING (there is no Deployment row — R5), transitive hover focus
 *     over the full upstream AND downstream chain, M0's own hover, and a tile
 *     that opens the activity;
 *   - TASKS: the surviving lens, the DESIGN review row Task 11's `designOwedFor`
 *     is the only source of, and its [Review] landing on that gate;
 *   - the ✕ ROUND TRIP, which lives here and not in activity-experience.spec
 *     because only a PLAN fixture carries BOTH screens' ops: a preview fixture is
 *     one screen's data, and this trip spans two.
 *
 * `incidents(page)` is asserted EMPTY in every case. Without it a screen that
 * rendered half a state over an op no fixture answered would read as a pass.
 */
import { test, expect } from '../support/dispatchGuard.js';
import { TESTID } from '../support/testids.js';
import { fixture, incidents, openState, viewResult } from '../support/previewShell.js';

/** The plan's rows, by their own testid prefix — never a hand-typed string. */
const PLAN_ROW_RE = new RegExp(`^${TESTID.planRow('')}`);
const PLAN_TILE_RE = new RegExp(`^${TESTID.planTile('')}`);

/**
 * `MUTED_OPACITY` as the dimmed tiles actually render it (components/flow/
 * flowLayout). A hover focus is read off the tile's own opacity, because that IS
 * the affordance: the lit chain stays at 1 and everything else recedes.
 */
const DIMMED = '0.12';

/**
 * Table 11-1's order over this fixture's committed activity list: the three
 * front-end design activities, then the build stack top→down in BUILD order
 * (resources first, clients last), then the side lane, then system testing.
 * Within a band the committed list's own authored order is kept — the plan
 * groups Table 11-1's rows, it does not re-sort what the architect wrote.
 */
const TABLE_11_1_ORDER: readonly string[] = [
  // FRONT END
  'requirements',
  'architecture',
  'projectDesign',
  // ◆ M0
  // RESOURCES
  'R-construction-pipeline-runtime',
  'R-github',
  'R-merchant-gateway',
  'R-operated-runtime',
  // RESOURCE ACCESS
  'C-agentic-job-access',
  'C-artifact-access',
  'C-billing-state-access',
  'C-episode-access',
  'C-merchant-gateway-access',
  'C-operated-runtime-access',
  'C-operated-system-state-access',
  'C-project-state-access',
  'C-source-control-access',
  'C-usage-access',
  // ENGINES
  'C-autoscaler-engine',
  'C-billing-engine',
  'C-design-health-engine',
  'C-estimation-engine',
  'C-intervention-engine',
  'C-operation-estimation-engine',
  'C-review-engine',
  // MANAGERS
  'C-billing-manager',
  'C-construction-manager',
  'C-operations-manager',
  'C-project-design-manager',
  'C-system-design-manager',
  // CLIENTS
  'U-SPA-web-client',
  // the SIDE LANE closes the build stack, and SYSTEM TESTING closes the plan
  'N-STP',
  'N-IT',
];

/** The gutter's rows, top→down. There is no `deployment` row (R5). */
const GUTTER_ROWS: readonly { row: string; label: string }[] = [
  { row: 'frontEnd', label: 'Front end' },
  { row: 'resource', label: 'Resources' },
  { row: 'resourceAccess', label: 'Resource' },
  { row: 'engine', label: 'Engines' },
  { row: 'manager', label: 'Managers' },
  { row: 'client', label: 'Clients' },
  { row: 'systemTesting', label: 'System' },
];

test.describe('plan · LIST', () => {
  test('opens in Table 11-1 order, with the M0 divider between Project Design and the first build row', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'plan', 'list');
    const list = page.getByTestId(TESTID.planList);
    await expect(list).toBeVisible();

    await expect(page.getByTestId(PLAN_ROW_RE)).toHaveCount(TABLE_11_1_ORDER.length);
    const drawn = await Promise.all(
      (await page.getByTestId(PLAN_ROW_RE).all()).map(async (row) =>
        ((await row.getAttribute('data-testid')) ?? '').slice(TESTID.planRow('').length)
      )
    );
    expect(drawn).toEqual(TABLE_11_1_ORDER);

    // The divider is where the committed order stops being design and starts
    // being construction — asserted by geometry, which is what a reader sees.
    const divider = page.getByTestId(TESTID.planM0Divider);
    await expect(divider).toBeVisible();
    const dividerBox = await divider.boundingBox();
    const lastFrontEnd = await page.getByTestId(TESTID.planRow('projectDesign')).boundingBox();
    const firstBuild = await page
      .getByTestId(TESTID.planRow('R-construction-pipeline-runtime'))
      .boundingBox();
    expect(dividerBox).not.toBeNull();
    expect(dividerBox?.y ?? 0).toBeGreaterThan(lastFrontEnd?.y ?? 0);
    expect(dividerBox?.y ?? 0).toBeLessThan(firstBuild?.y ?? 0);

    expect(await incidents(page)).toEqual([]);
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(offBundle).toEqual([]);
  });

  test('a row opens the activity full screen', async ({ page }) => {
    const offBundle = await openState(page, 'plan', 'list');
    await expect(page.getByTestId(TESTID.planList)).toBeVisible();

    await page.getByTestId(TESTID.planRow('C-review-engine')).click();

    // FULL SCREEN: the plan is gone, and the activity's own lifecycle is the spine.
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleGraph)).toBeVisible();
    await expect(page.getByTestId(TESTID.planList)).toHaveCount(0);

    expect(await incidents(page)).toEqual([]);
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(offBundle).toEqual([]);
  });
});

test.describe('plan · GRAPH', () => {
  test('draws a tile per placed activity, under a gutter that reads in BUILD order', async ({
    page,
  }) => {
    const project = viewResult<{ activityExecution: Record<string, unknown> }>(
      fixture('plan', 'graph'),
      'summary',
    );
    const placed = Object.keys(project.activityExecution).length;
    expect(placed).toBeGreaterThan(0);

    const offBundle = await openState(page, 'plan', 'graph');
    await expect(page.getByTestId(TESTID.planGraph)).toBeVisible();
    // Every activity of this capture places, so there is nothing to report as
    // unplaced — and the note that WOULD say so is absent rather than empty.
    await expect(page.getByTestId(PLAN_TILE_RE)).toHaveCount(placed);
    await expect(page.getByTestId(TESTID.planUnplacedNote)).toHaveCount(0);
    await expect(page.getByTestId(TESTID.planMilestone('M0'))).toBeVisible();

    await expect(page.getByTestId(TESTID.planGutter)).toBeVisible();
    let previousY = -1;
    for (const { row, label } of GUTTER_ROWS) {
      const gutterRow = page.getByTestId(TESTID.planGutterRow(row));
      await expect(gutterRow).toContainText(label);
      const box = await gutterRow.boundingBox();
      expect(box?.y ?? 0).toBeGreaterThan(previousY);
      previousY = box?.y ?? 0;
    }

    expect(await incidents(page)).toEqual([]);
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(offBundle).toEqual([]);
  });

  test('a hover lights the full upstream AND downstream chain — two hops out, and never across M0', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'plan', 'graph');
    await expect(page.getByTestId(TESTID.planGraph)).toBeVisible();

    // A mid-stack tile: C-ConstructionManager sits in MANAGERS, with resource
    // access above it and the client below.
    await page.getByTestId(TESTID.planTile('C-construction-manager')).hover();

    // Lit: itself, one hop up (a ResourceAccess it calls), TWO hops up (the
    // Resource behind that access), one hop down (the client that calls it) and
    // TWO hops down (system testing behind the client). Two hops is the point:
    // the prototype lit only the direct neighbours.
    for (const id of [
      'C-construction-manager',
      'C-project-state-access',
      'R-github',
      'U-SPA-web-client',
      'N-IT',
    ]) {
      await expect(page.getByTestId(TESTID.planTile(id))).toHaveCSS('opacity', '1');
    }

    // Dim: a sibling manager on neither chain, the side lane, and — the R1 fix —
    // the three front-end activities and M0. The closure walks call and sequence
    // edges only, so a build tile's ancestor walk can never climb through its own
    // milestone edge into the front-end chain.
    for (const id of ['C-billing-manager', 'N-STP', 'requirements', 'architecture', 'projectDesign']) {
      await expect(page.getByTestId(TESTID.planTile(id))).toHaveCSS('opacity', DIMMED);
    }
    await expect(page.getByTestId(TESTID.planMilestone('M0'))).toHaveCSS('opacity', DIMMED);

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('hovering M0 lights every non-front-end tile — that is what a forced dependency means', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'plan', 'graph');
    await expect(page.getByTestId(TESTID.planGraph)).toBeVisible();

    await page.getByTestId(TESTID.planMilestone('M0')).hover();

    await expect(page.getByTestId(TESTID.planMilestone('M0'))).toHaveCSS('opacity', '1');
    // One from every band it gates, the side lane included.
    for (const id of [
      'R-github',
      'C-project-state-access',
      'C-review-engine',
      'C-construction-manager',
      'U-SPA-web-client',
      'N-STP',
      'N-IT',
    ]) {
      await expect(page.getByTestId(TESTID.planTile(id))).toHaveCSS('opacity', '1');
    }
    // The three activities that produced the plan are not gated BY it.
    for (const id of ['requirements', 'architecture', 'projectDesign']) {
      await expect(page.getByTestId(TESTID.planTile(id))).toHaveCSS('opacity', DIMMED);
    }

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('a tile opens the activity full screen, and ✕ returns to the GRAPH the reader left', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'plan', 'graph');
    await expect(page.getByTestId(TESTID.planGraph)).toBeVisible();
    await expect(page.getByTestId(TESTID.planLensGraph)).toHaveAttribute('aria-pressed', 'true');

    await page.getByTestId(TESTID.planTile('C-review-engine')).click();
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleGraph)).toBeVisible();
    await expect(page.getByTestId(TESTID.planGraph)).toHaveCount(0);
    // Mid-trip: the activity screen answered entirely from this ONE fixture, so
    // a missing op cannot pass as a pass.
    expect(await incidents(page)).toEqual([]);

    await page.getByTestId(TESTID.designClose).click();

    // Back on the plan, on the lens the reader left — not the default LIST.
    await expect(page.getByTestId(TESTID.planScreen)).toBeVisible();
    await expect(page.getByTestId(TESTID.planGraph)).toBeVisible();
    await expect(page.getByTestId(TESTID.planList)).toHaveCount(0);
    await expect(page.getByTestId(TESTID.planLensGraph)).toHaveAttribute('aria-pressed', 'true');

    expect(await incidents(page)).toEqual([]);
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(offBundle).toEqual([]);
  });
});

test.describe('plan · TASKS', () => {
  test('still renders, owes a DESIGN review, and its decision row opens the activity ON that gate', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'plan', 'tasks');
    await expect(page.getByTestId(TESTID.constructionTasksLens)).toBeVisible();
    await expect(page.getByTestId(TESTID.constructionTasksHeadline)).toContainText('1 decision');

    // THE DESIGN BRANCH (Task 11 Step 5b). The three design activities never open
    // a construction session, so this row can only come from `designOwedFor`
    // reading the artifact SLOT — here the M0 cost approval, owed again after the
    // plan's basis went stale. Its key is (activity, gate, round).
    const row = page.getByTestId(TESTID.constructionTasksRow('projectDesign:sdpReview:1'));
    await expect(row).toBeVisible();
    await expect(row).toContainText('SDP Review · M0');

    await page.getByTestId(TESTID.constructionTasksReview('projectDesign:sdpReview:1')).click();

    // The row's [Review] navigates with `?task=<gate>`. The preview runs on a
    // MEMORY history, so the search string itself is not readable — its one
    // observable effect is, and it is the effect that matters: the activity opens
    // ON the gate the row named, with that gate's own verb on the bar.
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleNode('sdpReview'))).toHaveAttribute(
      'aria-current',
      'step'
    );
    await expect(page.getByTestId(TESTID.submitBarPrimary)).toHaveText(
      'Approve plan & cost — start construction'
    );

    expect(await incidents(page)).toEqual([]);
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(offBundle).toEqual([]);
  });
});
