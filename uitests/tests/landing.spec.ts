/**
 * landing.spec — the projects catalog (route `/`).
 *
 * AC flow: first-login EMPTY state → create a project → land on the home base →
 * the project surfaces in the catalog. Pure-UI: needs only the SPA + a dev-mode,
 * Postgres-backed server (no Temporal/worker), so it self-skips when that server
 * is unreachable (gating, README). Black-box: every assertion is by data-testid.
 */
import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, gotoApp } from './support/gating.js';
import { createProjectFromLanding } from './support/flows.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

test('landing renders the projects catalog past the session gate', async ({ page }) => {
  await gotoApp(page, '/');
  // The session gate (loading-indicator) has resolved; the landing screen mounts.
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
  // Either the first-login empty hero OR a populated grid is present — both are
  // valid catalog states. At least one must render.
  const empty = page.getByTestId(TESTID.emptyState);
  const grid = page.getByTestId(TESTID.projectsGrid);
  const hasEmpty = (await empty.count()) > 0;
  const hasGrid = (await grid.count()) > 0;
  expect(hasEmpty || hasGrid).toBe(true);
});

test('first-login empty state exposes the create-project CTA', async ({ page }) => {
  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();

  const empty = page.getByTestId(TESTID.emptyState);
  // Only assert the empty-state hero when this account actually has no projects;
  // a shared dev DB may already hold projects from a prior run.
  test.skip(
    (await empty.count()) === 0,
    'uitests: account already has projects — empty-state hero not shown (run against a fresh DB to exercise it)',
  );
  await expect(empty).toBeVisible();
  await expect(empty.getByTestId(TESTID.newProjectButton)).toBeVisible();
});

test('creating a project navigates to the home base and lists it', async ({ page }) => {
  await createProjectFromLanding(page);
  // On the home base now.
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();

  // Back on the catalog, the new project appears in the populated grid (the
  // empty hero is gone now that at least one project exists).
  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
  await expect(page.getByTestId(TESTID.projectsGrid)).toBeVisible();
  await expect(page.getByTestId(TESTID.emptyState)).toHaveCount(0);
  // At least one project card is rendered in the grid.
  await expect(page.getByTestId(/^project-card-/).first()).toBeVisible();
});

test('the create-project dialog surfaces the name-as-identity onboarding prerequisites', async ({
  page,
}) => {
  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();

  const card = page.getByTestId(TESTID.newProjectCard);
  const button = page.getByTestId(TESTID.newProjectButton);
  if ((await card.count()) > 0) {
    await card.first().click();
  } else {
    await button.first().click();
  }

  const dialog = page.getByTestId(TESTID.createProjectDialog);
  await expect(dialog).toBeVisible();

  // Name-as-identity onboarding: the prerequisites panel spells out the repo +
  // both GitHub-App installs the user must complete before adopting. There is NO
  // token field on the form (aiarch does no secret management).
  const prereqs = page.getByTestId(TESTID.createProjectPrereqs);
  await expect(prereqs).toBeVisible();
  await expect(prereqs).toContainText('GitHub');
  await expect(prereqs).toContainText('install-github-app');
  await expect(page.getByTestId(TESTID.newProjectNameInput)).toBeVisible();

  await page.getByTestId(TESTID.createProjectCancel).click();
  await expect(dialog).toBeHidden();
});

test('the create-project dialog cancels without creating', async ({ page }) => {
  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();

  const card = page.getByTestId(TESTID.newProjectCard);
  const button = page.getByTestId(TESTID.newProjectButton);
  if ((await card.count()) > 0) {
    await card.first().click();
  } else {
    await button.first().click();
  }

  const dialog = page.getByTestId(TESTID.createProjectDialog);
  await expect(dialog).toBeVisible();
  await page.getByTestId(TESTID.createProjectCancel).click();
  await expect(dialog).toBeHidden();
  // Still on the landing screen, no navigation happened.
  await expect(page).toHaveURL(/\/$|\/#?$/);
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
});
