/**
 * status-decides-outcome.spec — an empty-body error is an error, outside
 * construction too (fix-E review, the same false-success bug).
 *
 * openapi-fetch returns `error: undefined` when an error response has no body
 * (Content-Length 0), which is what a proxy's 502/503/504 looks like. The hooks
 * tested `error !== undefined`, so those read as SUCCESS: create-project then
 * went on to POST set-operating-model/undefined and navigated to a project that
 * did not exist, and a review decision sent over ops.call reported success.
 *
 * SAFETY: the shared dispatch guard aborts every non-GET before navigation. The
 * writes under test are answered in the browser by page routes; nothing reaches
 * the server.
 *
 * ── Stage 5 Task 13 ─────────────────────────────────────────────────────────
 * The third case ("an ops.call mutation: an EMPTY-body 500 on a review decision
 * surfaces as an error") drove the DESIGN RAIL — `/project/$id/design/system`,
 * its GatePanel and its submit bar — which spec §7.4 deletes; that route now
 * redirects to the plan, so the case had no surface left to drive. The two
 * create-project cases below are untouched: they drive the catalog, which
 * survives. What the deleted case held is EARMARKED: `ops.call`'s empty-body-500
 * mapping is still covered by unit tests, but no black-box spec exercises it
 * through a real submit any more. Restoring it needs an activity-route wire stub
 * (see the task-13 report's uitests earmarks).
 *
 * ── Stage 4a ────────────────────────────────────────────────────────────────
 * The THIRD remaining case ("set-operating-model: create succeeds, then an
 * EMPTY-body 500 on the model …") is DELETED, because its subject no longer
 * exists: create-project and set-operating-model are one write, `POST
 * /api/v1/delivery/start-project`, whose body carries the model. There is no
 * second request to fake, and the bug it guarded — a false success interpolating
 * `set-operating-model/undefined` — needed a URL with the id in it. Its FIRST
 * half, "a bare 500 is not a success", is what the two cases below assert, and
 * they now also pin that the create sends `projectID` ABSENT rather than empty,
 * which is the one signal that asks the server to mint an id.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** A bare status, the way a proxy's 5xx arrives: Content-Length 0. */
const EMPTY_500 = { status: 500, headers: { 'content-length': '0' }, body: '' };

/** The ONE write the create dialog sends (stage 4a): create AND operating model. */
const START_PROJECT = '**/api/v1/delivery/start-project';

/** One start-project body, as the wire carried it. */
type StartBody = Record<string, unknown>;

interface CreateTrap {
  /** Every start-project body the dialog sent. */
  starts: StartBody[];
}

async function trapCreate(page: Page): Promise<CreateTrap> {
  const trap: CreateTrap = { starts: [] };
  await page.route(START_PROJECT, async (route) => {
    trap.starts.push(route.request().postDataJSON() as StartBody);
    await route.fulfill(EMPTY_500);
  });
  return trap;
}

async function openCreateDialog(page: Page): Promise<void> {
  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
  const card = page.getByTestId(TESTID.newProjectCard);
  if ((await card.count()) > 0) await card.first().click();
  else await page.getByTestId(TESTID.newProjectButton).first().click();
  await expect(page.getByTestId(TESTID.createProjectDialog)).toBeVisible();
  await page.getByTestId(TESTID.newProjectNameInput).fill('fix-f-empty-500');
}

for (const model of ['selfOperated', 'archistratorOperated'] as const) {
  test(`start-project: an EMPTY-body 500 is an error, with nothing sent after it (${model})`, async ({
    page,
    request,
  }) => {
    await requireServer(request, BASE);
    const trap = await trapCreate(page);
    await openCreateDialog(page);
    await page.getByTestId(`operating-model-${model}`).check();
    await page.getByTestId(TESTID.createProjectSubmit).click();

    const dialog = page.getByTestId(TESTID.createProjectDialog);
    const alert = dialog.getByTestId(TESTID.errorAlert);
    await expect(alert).toBeVisible({ timeout: 10_000 });
    await expect(alert).toContainText('request failed with status 500');
    await expect(alert).toContainText('HTTP 500');
    // It did not "succeed": the dialog stays and nothing navigated.
    await expect(dialog).toBeVisible();
    await expect(page).toHaveURL(/\/$|\/#?$/);
    // ONE write, ONCE. Stage 4a folded create + set-operating-model into
    // start-project, so the second-write bug this file was written for — a
    // false success sending `set-operating-model/undefined` after a bare 500 —
    // cannot be expressed as a route any more: there is no second route, and
    // the id the dialog would have interpolated is no longer in a URL at all.
    // What remains testable, and is tested here, is the FIRST half of that bug:
    // a bare 500 must not read as success. `projectID` is ABSENT, never empty —
    // absence is what asks the server to create one.
    expect(trap.starts).toHaveLength(1);
    const sent = trap.starts[0] ?? {};
    expect(sent).toMatchObject({ name: 'fix-f-empty-500', model, start: false });
    expect('projectID' in sent).toBe(false);
  });
}
