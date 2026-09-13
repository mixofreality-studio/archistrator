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
import { test, expect, type Page } from '@playwright/test';
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

test.beforeEach(async ({ page, request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
  await page.route('**/execute-next-activity/**', (r) => r.abort());
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
