/**
 * landing.spec — the projects catalog (route `/`).
 *
 * AC flow: first-login EMPTY state → create a project → land on the home base →
 * the project surfaces in the catalog. Pure-UI: needs only the SPA + a dev-mode,
 * Postgres-backed server (no Temporal/worker), so it self-skips when that server
 * is unreachable (gating, README). Black-box: every assertion is by data-testid.
 *
 * SAFETY (fix-F review): the shared dispatch guard (support/dispatchGuard) aborts
 * every non-GET before any navigation, and create-project is answered in the
 * browser with a canned id (stubCreatedProject). No case here can create a real
 * project; each asserts the guard blocked nothing it did not expect.
 */
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, gotoApp } from './support/gating.js';
import { createProjectFromLanding } from './support/flows.js';
import { stubCreatedProject } from './support/designStubs.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** The id and name the faked create-project "mints". */
const CANNED_ID = 'uitest-landing-canned';
const CANNED_NAME = 'Landing Canned Project';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

test('landing renders the projects catalog past the session gate', async ({ page }) => {
  await gotoApp(page, '/');
  // The session gate (loading-indicator) has resolved; the landing screen mounts.
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
  // Either the first-login empty hero OR a populated grid renders once the catalog
  // read answers — both are valid catalog states. Counting the two at once raced
  // that read, and failed on a populated catalog before it arrived.
  await expect(
    page.getByTestId(TESTID.emptyState).or(page.getByTestId(TESTID.projectsGrid)).first(),
  ).toBeVisible();
});

test('first-login empty state exposes the create-project CTA', async ({ page }) => {
  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();

  const empty = page.getByTestId(TESTID.emptyState);
  const grid = page.getByTestId(TESTID.projectsGrid);
  // Let the catalog read answer before deciding which state this account is in.
  await expect(empty.or(grid).first()).toBeVisible();
  // Only assert the empty-state hero when this account actually has no projects;
  // a shared dev DB may already hold projects from a prior run.
  test.skip(
    (await empty.count()) === 0,
    'uitests: account already has projects — empty-state hero not shown (run against a fresh DB to exercise it)',
  );
  await expect(empty).toBeVisible();
  await expect(empty.getByTestId(TESTID.newProjectButton)).toBeVisible();
});

test('creating a project navigates to the home base and lists it', async ({
  page,
  dispatchGuard,
}) => {
  // create-project answers CANNED_ID in the browser; the project and its catalog
  // row are stubbed. The real create flow runs in the SPA; nothing is written.
  const created = await stubCreatedProject(page, CANNED_ID, CANNED_NAME);
  await createProjectFromLanding(page);
  // On the home base of the id create-project returned.
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/project/${CANNED_ID}/home$`));

  // Back on the catalog, the new project appears in the populated grid (the
  // empty hero is gone now that at least one project exists).
  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
  await expect(page.getByTestId(TESTID.projectsGrid)).toBeVisible();
  await expect(page.getByTestId(TESTID.emptyState)).toHaveCount(0);
  await expect(page.getByTestId(/^project-card-/).first()).toBeVisible();
  await expect(page.getByTestId(TESTID.projectsGrid)).toContainText(CANNED_NAME);

  // The one create was answered in the browser, and no other write left it.
  expect(created.creates).toBe(1);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('the create-project dialog surfaces the name-as-identity onboarding prerequisites', async ({
  page,
  dispatchGuard,
}) => {
  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();

  const card = page.getByTestId(TESTID.newProjectCard);
  const button = page.getByTestId(TESTID.newProjectButton);
  // Let the catalog read answer: the affordance differs by catalog state.
  await expect(card.or(button).first()).toBeVisible();
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
  expect(dispatchGuard.blocked).toEqual([]);
});

test('the create-project dialog cancels without creating', async ({ page, dispatchGuard }) => {
  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();

  const card = page.getByTestId(TESTID.newProjectCard);
  const button = page.getByTestId(TESTID.newProjectButton);
  await expect(card.or(button).first()).toBeVisible();
  if ((await card.count()) > 0) {
    await card.first().click();
  } else {
    await button.first().click();
  }

  const dialog = page.getByTestId(TESTID.createProjectDialog);
  await expect(dialog).toBeVisible();
  await page.getByTestId(TESTID.createProjectCancel).click();
  await expect(dialog).toBeHidden();
  // Still on the landing screen, no navigation happened, and nothing was written.
  await expect(page).toHaveURL(/\/$|\/#?$/);
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});
