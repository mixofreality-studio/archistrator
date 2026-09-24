/**
 * Reusable black-box UI flows, expressed purely through the browser + testids.
 * No webApp imports, no API command dispatch — every action is a real click/fill,
 * exactly as systemtests drives the server only over its published surface.
 */
import { expect, type Page } from '@playwright/test';
import { TESTID } from './testids.js';
import { gotoApp } from './gating.js';
import { stubCreatedProject } from './designStubs.js';

/**
 * createProjectFromLanding opens `/`, creates a uniquely-named project via the
 * dialog, and waits for the home base to mount. Returns the project name (the
 * server mints the id; the UI navigates to /project/$id/home).
 *
 * The "new project" affordance differs by landing state: the first-login EMPTY
 * state surfaces the `new-project-button` CTA; a populated grid surfaces the
 * dashed `new-project-card`. We open whichever is present so the flow works from
 * any starting catalog.
 */
/**
 * openSharedProject opens THE real project for this test run. It NEVER creates one
 * (fix-G review ruling): the seed step, tests/seed/shared-project.setup.ts, is the
 * one place in this suite that may create a project, and it runs before every
 * spec (playwright.config's `seed-shared-project` project). Only the
 * live-drafting specs need a real project, so the seed runs only with
 * UITESTS_LIVE_DRAFTING on; every other spec opens a project faked in the browser
 * (openStubbedProject) and creates nothing.
 *
 * ONE PROJECT.JSON PER RUN (founder ruling 2026-08-14). A project's identity is
 * its repository: the local git substrate holds exactly one project.json per
 * repo, and `guardProjectIdentity` refuses a second project's write into a repo
 * another project already claimed. uitests.yml points the server at ONE bare
 * repo, so a suite where every spec created its own project could only ever have
 * its FIRST creation succeed. A new project means a new folder — and for this
 * suite, the new folder arrives with the next run.
 *
 * It used to create the project itself when the catalog was empty, so whichever
 * spec asked first created it as a side effect, and any spec could create one
 * against a writable server. The catalog is re-read rather than trusting cached
 * module state, so a worker restart or a retry finds the seeded project again.
 */
export async function openSharedProject(page: Page): Promise<void> {
  if (sharedProjectURL !== undefined) {
    await page.goto(sharedProjectURL);
    await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
    return;
  }

  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
  const existing = page.getByTestId(/^project-card-/);
  await expect(
    existing.first(),
    'no project to open: the seed step (tests/seed/shared-project.setup.ts) makes the run’s one real project, with UITESTS_LIVE_DRAFTING on; this flow never creates one'
  ).toBeVisible();
  await existing.first().click();
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
  await expect(page).toHaveURL(/\/project\/[^/]+\/home$/);
  sharedProjectURL = page.url();
}

/**
 * openStubbedProject opens a project faked in the browser (stubCreatedProject) on
 * its home base: a fresh Phase-0 project with no design session, whose catalog row
 * rides on the real catalog read. Nothing is created. Pair it with the shared
 * dispatch guard. Returns the number of creates the stub answered.
 */
export async function openStubbedProject(
  page: Page,
  projectId: string,
  name: string
): Promise<{ creates: number }> {
  const created = await stubCreatedProject(page, projectId, name);
  await gotoApp(page, `/project/${projectId}/home`);
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
  return created;
}

/** The run's single project home-base URL, memoized after the first open. */
let sharedProjectURL: string | undefined;

export async function createProjectFromLanding(page: Page): Promise<string> {
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

  const name = `uitest ${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
  await page.getByTestId(TESTID.newProjectNameInput).fill(name);
  await page.getByTestId(TESTID.createProjectSubmit).click();

  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
  await expect(page).toHaveURL(/\/project\/[^/]+\/home$/);
  return name;
}
