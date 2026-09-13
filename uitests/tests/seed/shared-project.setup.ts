/**
 * THE SEED STEP: the one place in this suite that may create a real project
 * (fix-G review ruling).
 *
 * playwright.config runs it as its own project, `seed-shared-project`, which every
 * spec depends on, so it runs once, before any spec, and is named rather than
 * left as a side effect. Until fix H, the run's project was created by whichever
 * spec first called openSharedProject on an empty catalog (billing.spec, in CI),
 * and any of six specs could create one against a writable server.
 *
 * What needs it: only the live-drafting specs (design-experience's drafting block,
 * architecture-views, artifact-affordances) drive real writes against a real
 * project. Every other spec fakes its project in the browser, under the shared
 * dispatch guard, and cannot create one. So the seed runs only with
 * UITESTS_LIVE_DRAFTING on, and SKIPS otherwise, CI's default run included. A
 * skipped seed does not hold the specs back.
 *
 * It adopts a project the catalog already lists (a retry, or a reused server),
 * and creates one through the real create flow only when there is none: ONE
 * project.json per run (see openSharedProject).
 */
import { test as setup, expect } from '@playwright/test';
import { TESTID } from '../support/testids.js';
import { gotoApp, skipUnlessLiveDrafting, requireServer } from '../support/gating.js';
import { createProjectFromLanding } from '../support/flows.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

setup('seed the run’s one real project (live drafting only)', async ({ page, request }) => {
  skipUnlessLiveDrafting();
  await requireServer(request, BASE);

  await gotoApp(page, '/');
  await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
  const cards = page.getByTestId(/^project-card-/);
  const empty = page.getByTestId(TESTID.emptyState);
  // Let the catalog read answer before deciding whether a project exists.
  await expect(cards.first().or(empty)).toBeVisible();
  if ((await cards.count()) > 0) return;

  await createProjectFromLanding(page);
});
