/**
 * construction-fix-d.spec — fix round D (fix-C review minors + designer final items).
 *
 *  - The no-attempt rule: a task with no attempt reads UNKNOWN only when its row is
 *    unclassified, or has build evidence but ZERO attempts (history predates
 *    per-task capture). Under a row with at least one attempt it reads NOT STARTED:
 *    hollow ○ in the tree, NOT STARTED in the pane, and no UNRECORDED chip.
 *  - A MIXED row under "Observed only" keeps its grade chip, with the hidden-count
 *    chip BESIDE it, and every `data-provenance` stays within the origin enum.
 *
 * The committed corpus has neither shape (every row with attempts has one for every
 * task, and nothing mixes observed with reconstructed), so these cases serve the
 * REAL project read with one row edited — in the browser. The read itself goes to
 * the server as normal; only its answer is edited. Nothing is written.
 *
 * SAFETY: every execute-next-activity request is TRAPPED and ABORTED by page.route
 * before the page opens. Nothing here presses Run, Begin or Retry.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

const ORIGINS = ['observed', 'backfilled', 'synthesized', 'unknown'];

interface WireAttempt {
  task: string;
  phase: string;
  provenance: { origin: string };
}
interface WireRow {
  ActivityID: string;
  attempts?: WireAttempt[];
  hasBuildEvidence: boolean;
}
interface Wire {
  ActivityConstruction?: Record<string, WireRow>;
}

// Every non-GET is aborted by the shared dispatch guard (support/dispatchGuard).
test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

/** Serve the real project read with `edit` applied to one row, in the browser. */
async function serveEdited(
  page: Page,
  activityId: string,
  edit: (row: WireRow) => void
): Promise<void> {
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const response = await route.fetch();
    const wire = (await response.json()) as Wire;
    const row = wire.ActivityConstruction?.[activityId];
    if (row === undefined) throw new Error(`no row ${activityId} in the read`);
    edit(row);
    await route.fulfill({ response, json: wire });
  });
}

/** The tree task row's state, read from the row's own `data-task-state` hook. */
async function taskStateOf(page: Page, nodeId: string): Promise<string | null> {
  return page.evaluate(
    (rowId) =>
      document
        .querySelector(`[data-testid="${rowId}"] [data-task-state]`)
        ?.getAttribute('data-task-state') ?? null,
    TESTID.constructionListRow(nodeId)
  );
}

async function openList(page: Page, suffix: string): Promise<void> {
  await page.setViewportSize({ width: 1280, height: 900 });
  await gotoApp(page, `/project/archistrator/construction?lens=list${suffix}`);
  await expect(page.getByTestId(TESTID.constructionListRow('C-billing-engine'))).toBeVisible({
    timeout: 15_000,
  });
}

test('a task with no attempt under a row WITH attempts reads NOT STARTED: hollow ○, no UNRECORDED chip', async ({
  page,
}) => {
  // C-billing-engine keeps 9 of its 10 attempts: its per-task history is complete,
  // so the one task missing from it has not run.
  await serveEdited(page, 'C-billing-engine', (row) => {
    row.attempts = (row.attempts ?? []).filter((a) => a.task !== 'srs');
    expect(row.attempts.length).toBeGreaterThan(0);
  });
  await openList(page, '&a=C-billing-engine&p=requirements&k=srs');

  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText('NOT STARTED');
  await expect(page.getByTestId(TESTID.constructionDetailProvenanceChip)).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionDetailPane)).not.toContainText('UNRECORDED');
  await expect
    .poll(() => taskStateOf(page, 'C-billing-engine::requirements::srs'))
    .toBe('notStarted');
});

test('a task under a row with evidence but ZERO attempts stays UNKNOWN, and says UNRECORDED', async ({
  page,
}) => {
  await serveEdited(page, 'C-billing-engine', (row) => {
    expect(row.hasBuildEvidence).toBe(true);
    row.attempts = [];
  });
  await openList(page, '&a=C-billing-engine&p=requirements&k=srs');

  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText('UNKNOWN');
  await expect(page.getByTestId(TESTID.constructionDetailProvenanceChip)).toHaveText('UNRECORDED');
  await expect
    .poll(() => taskStateOf(page, 'C-billing-engine::requirements::srs'))
    .toBe('unknown');
});

test('Observed only on a MIXED row: the grade chip stays, the hidden count sits beside it', async ({
  page,
}) => {
  let hidden = 0;
  await serveEdited(page, 'C-billing-engine', (row) => {
    const attempts = row.attempts ?? [];
    const first = attempts[0];
    if (first === undefined) throw new Error('C-billing-engine has no attempts');
    first.provenance.origin = 'observed';
    hidden = attempts.filter((a) => a.provenance.origin !== 'observed').length;
  });
  await openList(page, '&a=C-billing-engine');
  await page.getByRole('switch', { name: 'Observed only' }).check();

  const grade = page.getByTestId(TESTID.constructionDetailProvenanceChip);
  const hiddenChip = page.getByTestId(TESTID.constructionDetailObservedOnlyChip);
  expect(hidden).toBeGreaterThan(0);
  await expect(grade).toHaveText('RECORDED');
  await expect(grade).toHaveAttribute('data-provenance', 'observed');
  await expect(hiddenChip).toHaveText(`OBSERVED ONLY · ${String(hidden)} reconstructed hidden`);
  // BESIDE: the same header line, grade first.
  const sameLine = await page.evaluate(
    ({ g, h }) => {
      const ge = document.querySelector(`[data-testid="${g}"]`);
      const he = document.querySelector(`[data-testid="${h}"]`);
      if (ge === null || he === null) return null;
      const gr = ge.getBoundingClientRect();
      const hr = he.getBoundingClientRect();
      return {
        siblings: ge.parentElement === he.parentElement,
        gradeFirst: gr.right <= hr.left,
        sameRow: Math.abs(gr.top - hr.top) < 2,
      };
    },
    { g: TESTID.constructionDetailProvenanceChip, h: TESTID.constructionDetailObservedOnlyChip }
  );
  expect(sameLine).toEqual({ siblings: true, gradeFirst: true, sameRow: true });
  // Every provenance attribute in the pane is an origin, never a toggle name.
  const values = await page.evaluate(
    (paneId) =>
      Array.from(document.querySelectorAll(`[data-testid="${paneId}"] [data-provenance]`)).map(
        (el) => el.getAttribute('data-provenance')
      ),
    TESTID.constructionDetailPane
  );
  expect(values.length).toBeGreaterThan(0);
  for (const v of values) expect(ORIGINS).toContain(v);
});

// ---------------------------------------------------------------------------
// N1 (fix-C review; designer final items)
// ---------------------------------------------------------------------------

test('N1: coming back from another lens does not re-open the deep link’s rows', async ({
  page,
}) => {
  await openList(page, '&a=N-STP&p=construction&k=codeReview');
  const taskRow = page.getByTestId(TESTID.constructionListRow('N-STP::construction::codeReview'));
  await expect(taskRow).toBeVisible();
  // A lens switch unmounts the tree; the way back remounts it. The selection is
  // the same, so the link has already been shown — nothing may re-open.
  await page.getByTestId(TESTID.constructionLensButton('graph')).click();
  await expect(taskRow).toHaveCount(0);
  await page.getByTestId(TESTID.constructionLensButton('list')).click();
  await expect(page.getByTestId(TESTID.constructionListRow('N-STP'))).toBeVisible();
  await page.waitForTimeout(600);
  await expect(taskRow).toBeHidden();
  // …while the selection itself is kept.
  expect(page.url()).toContain('k=codeReview');
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toBeVisible();
});

/** Where the row's midpoint sits in the band below the toolbar: 0 top, 1 bottom. */
async function bandFraction(page: Page, nodeId: string): Promise<number> {
  return page.evaluate((rowId) => {
    const row = document.querySelector(`[data-testid="${rowId}"]`);
    const bar = document.querySelector('[data-testid="construction-lens-toolbar"]');
    if (row === null || bar === null) return -1;
    let sc: HTMLElement | null = bar.parentElement;
    while (sc !== null && !/(auto|scroll)/.test(getComputedStyle(sc).overflowY)) {
      sc = sc.parentElement;
    }
    const r = row.getBoundingClientRect();
    const top = bar.getBoundingClientRect().bottom;
    const bottom = Math.min(sc?.getBoundingClientRect().bottom ?? innerHeight, innerHeight);
    return ((r.top + r.bottom) / 2 - top) / (bottom - top);
  }, TESTID.constructionListRow(nodeId));
}

for (const width of [1280, 1366, 1600]) {
  test(`N1: at ${String(width)} a deep-linked row lands CENTRED, even near the list’s end`, async ({
    page,
  }) => {
    for (const [suffix, nodeId] of [
      ['&a=N-STP&p=construction&k=codeReview', 'N-STP::construction::codeReview'],
      ['&a=U-SPA-web-client&p=requirements&k=srs', 'U-SPA-web-client::requirements::srs'],
    ] as const) {
      await page.setViewportSize({ width, height: 900 });
      await gotoApp(page, `/project/archistrator/construction?lens=list${suffix}`);
      await expect(page.getByTestId(TESTID.constructionListRow(nodeId))).toBeVisible({
        timeout: 15_000,
      });
      // Measured before this fix: 0.62 and 0.81 at 1280/1366 — the list's end. The
      // centre is the band BELOW the stuck toolbar: a band from the scroller's top
      // would put the row at ~0.44, outside this window.
      await expect
        .poll(() => bandFraction(page, nodeId), { timeout: 5_000, message: nodeId })
        .toBeGreaterThan(0.45);
      await expect
        .poll(() => bandFraction(page, nodeId), { timeout: 5_000, message: nodeId })
        .toBeLessThan(0.55);
      // The runway is exactly what centring needed — sized once the rows had
      // finished expanding, not against a half-grown list: when there is one, the
      // scroller sits at its maximum, with no surplus blank space below.
      const fit = await page.evaluate((runwayId) => {
        const runway = document.querySelector<HTMLElement>(`[data-testid="${runwayId}"]`);
        let sc: HTMLElement | null = runway?.parentElement ?? null;
        while (sc !== null && !/(auto|scroll)/.test(getComputedStyle(sc).overflowY)) {
          sc = sc.parentElement;
        }
        if (runway === null || sc === null) return null;
        return { runway: runway.offsetHeight, slack: sc.scrollHeight - sc.clientHeight - sc.scrollTop };
      }, TESTID.constructionListRunway);
      expect(fit, nodeId).not.toBeNull();
      if (fit !== null && fit.runway > 0) {
        expect(Math.abs(fit.slack), `${nodeId}: surplus runway`).toBeLessThanOrEqual(2);
      }
    }
  });
}

// ---------------------------------------------------------------------------
// Designer final items: the toolbar's toggles, the header floor, never-run ink
// ---------------------------------------------------------------------------

for (const width of [1280, 1366, 1600]) {
  test(`at ${String(width)}, Expand and Observed only wrap TOGETHER as the toolbar's second row`, async ({
    page,
  }) => {
    await openList(page, '&a=C-billing-engine');
    await page.setViewportSize({ width, height: 900 });
    await page.waitForTimeout(400);
    const read = await page.evaluate(
      ({ expandId, observedId, groupId, listId }) => {
        const top = (id: string): number =>
          document.querySelector(`[data-testid="${id}"]`)?.getBoundingClientRect().top ?? -1;
        const group = document.querySelector(`[data-testid="${groupId}"]`);
        return {
          expand: top(expandId),
          observed: top(observedId),
          grouped:
            group !== null &&
            group.contains(document.querySelector(`[data-testid="${expandId}"]`)) &&
            group.contains(document.querySelector(`[data-testid="${observedId}"]`)),
          groupTop: group?.getBoundingClientRect().top ?? -1,
          lensTop: top(listId),
        };
      },
      {
        expandId: TESTID.constructionLensExpandToPhase,
        observedId: TESTID.constructionLensObservedOnly,
        groupId: TESTID.constructionLensToolbarToggles,
        listId: TESTID.constructionLensButton('list'),
      }
    );
    expect(read.grouped, 'one group holds both').toBe(true);
    // The switch sits a few px lower than the button's box: compare centres loosely.
    expect(Math.abs(read.expand - read.observed), 'same row').toBeLessThan(12);
    expect(read.groupTop, 'on the second row, below the lens control').toBeGreaterThan(
      read.lensTop + 20
    );
  });

  test(`at ${String(width)} with the pane open, no header label is under 9px`, async ({ page }) => {
    await openList(page, '&a=C-billing-engine');
    await page.setViewportSize({ width, height: 900 });
    await page.waitForTimeout(400);
    const sizes = await page.evaluate((headerId) => {
      const header = document.querySelector(`[data-testid="${headerId}"]`);
      if (header === null) throw new Error('no list header');
      return Array.from(header.querySelectorAll('.MuiTypography-root')).map((el) => ({
        text: (el as HTMLElement).innerText.trim(),
        px: parseFloat(getComputedStyle(el).fontSize),
      }));
    }, TESTID.constructionListHeader);
    expect(sizes.length).toBeGreaterThan(5);
    for (const s of sizes) expect(s.px, `"${s.text}"`).toBeGreaterThanOrEqual(9);
  });
}

test('a never-run N-IT names its targets in neutral ink; N-STP’s plan keeps its red', async ({
  page,
}) => {
  await openList(page, '&a=N-IT');
  const view = page.getByTestId(TESTID.constructionSystemTestView);
  await expect(view).toBeVisible();
  await expect(view).toContainText('not run');
  const inks = async (): Promise<{
    chip: Record<string, string | null>;
    chipBorder: string;
    neutral: string;
    target: string | null;
    targetColor: string;
  }> =>
    page.evaluate(
      ({ viewId, negId, boundId }) => {
        const root = document.querySelector(`[data-testid="${viewId}"]`);
        if (root === null) throw new Error('no system test view');
        const chip = (id: string): HTMLElement | null =>
          root.querySelector<HTMLElement>(`[data-testid="${id}"]`);
        const neg = chip(negId);
        const target = root.querySelector<HTMLElement>('[data-step-status]');
        // The view's own muted headline is the neutral ink to compare against.
        const headline = root.firstElementChild as HTMLElement;
        return {
          chip: {
            negative: neg?.getAttribute('data-case-ink') ?? null,
            boundary: chip(boundId)?.getAttribute('data-case-ink') ?? null,
          },
          chipBorder: neg === null ? '' : getComputedStyle(neg).borderTopColor,
          neutral: getComputedStyle(headline).color,
          target: target?.getAttribute('data-step-status') ?? null,
          targetColor: target === null ? '' : getComputedStyle(target).color,
        };
      },
      { viewId: TESTID.constructionSystemTestView, negId: TESTID.constructionCaseChip('STP-UC1-N1'), boundId: TESTID.constructionCaseChip('STP-UC1-B1') }
    );
  const nit = await inks();
  expect(nit.chip).toEqual({ negative: 'neutral', boundary: 'neutral' });
  expect(nit.target).toBe('planned');
  // Real colours, not only the attribute: the ink IS the muted headline's.
  expect(nit.chipBorder).toBe(nit.neutral);
  expect(nit.targetColor).toBe(nit.neutral);

  // N-STP's plan is the PLAN: its targets stay red, its negative chips danger.
  await openList(page, '&a=N-STP');
  const plan = page.getByTestId(TESTID.constructionTestPlanView);
  await expect(plan).toBeVisible();
  const planInk = await page.evaluate(
    ({ planId, negId }) => {
      const root = document.querySelector(`[data-testid="${planId}"]`);
      return {
        negative:
          root?.querySelector(`[data-testid="${negId}"]`)?.getAttribute('data-case-ink') ?? null,
        target: root?.querySelector('[data-step-status]')?.getAttribute('data-step-status') ?? null,
      };
    },
    { planId: TESTID.constructionTestPlanView, negId: TESTID.constructionCaseChip('STP-UC1-N1') }
  );
  expect(planInk).toEqual({ negative: 'danger', target: 'red' });
});
