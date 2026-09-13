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
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, gotoApp } from './support/gating.js';
import { stubAwaitingReviewGlossary } from './support/designStubs.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** A bare status, the way a proxy's 5xx arrives: Content-Length 0. */
const EMPTY_500 = { status: 500, headers: { 'content-length': '0' }, body: '' };

interface CreateTrap {
  creates: number;
  operatingModelCalls: string[];
}

async function trapCreate(page: Page): Promise<CreateTrap> {
  const trap: CreateTrap = { creates: 0, operatingModelCalls: [] };
  await page.route('**/api/v1/system-design/create-project', async (route) => {
    trap.creates += 1;
    await route.fulfill(EMPTY_500);
  });
  await page.route('**/api/v1/system-design/set-operating-model/**', async (route) => {
    trap.operatingModelCalls.push(route.request().url());
    await route.abort();
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
  test(`create-project: an EMPTY-body 500 is an error, with nothing sent after it (${model})`, async ({
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
    // It did not "succeed": the dialog stays, nothing navigated, and no
    // set-operating-model/undefined followed.
    await expect(dialog).toBeVisible();
    await expect(page).toHaveURL(/\/$|\/#?$/);
    expect(trap.creates).toBe(1);
    expect(trap.operatingModelCalls).toEqual([]);
  });
}

test('set-operating-model: create succeeds, then an EMPTY-body 500 on the model is an error, and nothing navigates', async ({
  page,
  request,
}) => {
  // fix-F review: the second write of the create flow. Create answers with a
  // canned id (in the browser), then set-operating-model answers a bare 500. That
  // used to pass as success and navigate to the new project's home.
  await requireServer(request, BASE);
  const creates: string[] = [];
  const models: string[] = [];
  await page.route('**/api/v1/system-design/create-project', async (route) => {
    creates.push(route.request().url());
    await route.fulfill({ status: 200, json: 'fix-g-canned-project' });
  });
  await page.route('**/api/v1/system-design/set-operating-model/**', async (route) => {
    models.push(route.request().url());
    await route.fulfill(EMPTY_500);
  });
  await openCreateDialog(page);
  await page.getByTestId('operating-model-archistratorOperated').check();
  await page.getByTestId(TESTID.createProjectSubmit).click();

  const dialog = page.getByTestId(TESTID.createProjectDialog);
  const alert = dialog.getByTestId(TESTID.errorAlert);
  await expect(alert).toBeVisible({ timeout: 10_000 });
  await expect(alert).toContainText('request failed with status 500');
  await expect(dialog).toBeVisible();
  await expect(page).toHaveURL(/\/$|\/#?$/);
  expect(creates).toHaveLength(1);
  // The model was set against the id create returned, not "undefined".
  expect(models).toHaveLength(1);
  expect(models[0]).toContain('/set-operating-model/fix-g-canned-project');
});

test('an ops.call mutation: an EMPTY-body 500 on a review decision surfaces as an error', async ({
  page,
}) => {
  const projectId = await stubAwaitingReviewGlossary(page, [
    { term: 'Architect', definition: 'The single design authority.', category: 'Who' },
  ]);
  const decisions: string[] = [];
  await page.route('**/api/v1/system-design/submit-review-decision/**', async (route) => {
    decisions.push(route.request().url());
    await route.fulfill(EMPTY_500);
  });
  await page.goto(`/project/${projectId}/design/system`);
  await expect(page.getByTestId(TESTID.gatePanel)).toBeVisible();

  const note = 'Name the dispatch venue.';
  await page.getByTestId(TESTID.chatInput).getByRole('textbox').fill(note);
  await page.getByTestId(TESTID.chatSend).click();
  await expect(page.getByText('PENDING · NOT SENT · 1')).toBeVisible();
  await page.getByTestId(TESTID.gateSendback).click();

  // A 5xx is indeterminate: the cause-neutral banner, and the staged note kept.
  // Read as a success, the banner never showed and the note was cleared.
  const banner = page.getByTestId(TESTID.gateError);
  await expect(banner).toBeVisible({ timeout: 10_000 });
  await expect(banner).toContainText('could not be confirmed');
  await expect(page.getByText('PENDING · NOT SENT · 1')).toBeVisible();
  await expect(page.getByText(note)).toBeVisible();
  expect(decisions).toHaveLength(1);
});
