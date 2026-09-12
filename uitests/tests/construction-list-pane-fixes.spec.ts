/**
 * construction-list-pane-fixes.spec — fix round A's list and pane fixes, driven
 * live against the seeded project:
 *
 *   P0-4  no activity id is truncated, and each id cell names its id;
 *   P1-5  the pane carries ONE "Run this task" (the body used to repeat it);
 *   P1-6  an activity's pane is headed "This activity", counts attempts and
 *         phases instead of "NO ATTEMPTS", and draws no empty "Exit: —" line;
 *   P1-9  a search that matches nothing says so, names the query, and "Clear
 *         filters" brings every row back;
 *   P1-10 rows a search opened close when the query clears; a row the operator
 *         opened stays open.
 *
 * Gated like construction-tracker.spec.ts: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy.
 */
import { test, expect, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

/** Every rendered tree row's node id, top to bottom (collapsed subtrees are
 *  unmounted, so this is exactly what the operator sees open). */
async function renderedRowIds(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll('[data-testid^="construction-list-row-"]')).map((el) =>
      (el.getAttribute('data-testid') ?? '').slice('construction-list-row-'.length)
    )
  );
}

async function openList(page: Page, query = ''): Promise<void> {
  await gotoApp(page, `/project/archistrator/construction?lens=list${query}`);
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });
}

async function search(page: Page, text: string): Promise<void> {
  await page.getByTestId(TESTID.constructionLensSearch).getByRole('textbox').fill(text);
  // The reveal is a render-time state adjustment plus one rAF-deferred focus.
  await page.waitForTimeout(600);
}

test('P0-4: at 1280px with the pane open, no activity id is truncated and each id cell names its id', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await openList(page, '&a=U-SPA-web-client');
  await expect(page.getByTestId(TESTID.constructionDetailPane)).toBeVisible();

  const cells = await page.evaluate(() =>
    Array.from(document.querySelectorAll('[data-testid^="construction-list-id-"]')).map((el) => ({
      id: el.textContent,
      title: el.getAttribute('title'),
      truncated: el.scrollWidth > el.clientWidth + 0.5,
    }))
  );
  expect(cells.length).toBeGreaterThan(20);
  for (const c of cells) {
    expect(c.truncated, `${c.id ?? '?'} is truncated`).toBe(false);
    expect(c.title).toBe(c.id);
  }
});

test('P1-5 + P1-6: an activity’s pane has one "Run this task", is headed "This activity", counts what it holds, and draws no empty exit line', async ({
  page,
}) => {
  await openList(page, '&a=U-SPA-web-client');
  const pane = page.getByTestId(TESTID.constructionDetailPane);
  await expect(pane).toBeVisible();

  await expect(pane.getByRole('button', { name: /Run this task/ })).toHaveCount(1);
  const body = page.getByTestId(TESTID.constructionDetailBodyUnknown);
  await expect(body).toContainText(/this activity/i);
  await expect(body).not.toContainText(/this task/i);
  await expect(page.getByTestId(TESTID.constructionDetailSelectionSummary)).toHaveText(
    /^0 attempts · \d+ phases$/
  );
  await expect(pane).not.toContainText('NO ATTEMPTS');
  // No phase applies to an activity-level selection with no current phase.
  await expect(page.getByTestId(TESTID.constructionDetailExitCriterion)).toHaveCount(0);

  // A backfilled activity counts its attempts rather than saying "NO ATTEMPTS"
  // beside its PASSED chip...
  await openList(page, '&a=C-billing-engine');
  await expect(page.getByTestId(TESTID.constructionDetailSelectionSummary)).toHaveText(
    /^[1-9]\d* attempts · 5 phases$/
  );
  // ...and a phase selection keeps its exit line: a phase applies there.
  await openList(page, '&a=C-billing-engine&p=requirements');
  await expect(page.getByTestId(TESTID.constructionDetailSelectionSummary)).toHaveText(
    /^\d+ attempts? · 2 tasks$/
  );
  await expect(page.getByTestId(TESTID.constructionDetailExitCriterion)).toBeVisible();
});

test('P1-9: a search that matches nothing names the query, and Clear filters brings every row back', async ({
  page,
}) => {
  await openList(page);
  const atRest = (await renderedRowIds(page)).length;
  expect(atRest).toBeGreaterThan(0);

  await search(page, 'zzz-no-such-activity');
  const empty = page.getByTestId(TESTID.constructionListEmpty);
  await expect(empty).toContainText('No activity matches “zzz-no-such-activity”.');
  await expect(empty).not.toContainText('No construction activities are recorded');

  await page.getByTestId(TESTID.constructionListClearFilters).click();
  await expect(page.getByTestId(TESTID.constructionLensSearch).getByRole('textbox')).toHaveValue('');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible();
  expect((await renderedRowIds(page)).length).toBe(atRest);
});

test('P1-10: clearing a search closes the rows it opened, and keeps the one the operator opened', async ({
  page,
}) => {
  await openList(page);
  const activityRow = page.getByTestId(TESTID.constructionListRow('C-billing-engine'));
  await activityRow.click();
  // The OPERATOR opens it (keyboard expansion on the focused row).
  await page.keyboard.press('ArrowRight');
  await expect(
    page.getByTestId(TESTID.constructionListRow('C-billing-engine::requirements'))
  ).toBeVisible();
  const operatorOpen = await renderedRowIds(page);

  await search(page, 'srs review');
  expect((await renderedRowIds(page)).length).toBeGreaterThan(operatorOpen.length);

  await search(page, '');
  // Exactly what the operator had open: the search's rows closed, theirs did not.
  expect(await renderedRowIds(page)).toEqual(operatorOpen);
});
