/**
 * team.spec — the Team / Agents roster (route `/project/$projectId/team`).
 *
 * AC (Goal 4): the canonical Method roles from the approved UX mock are visible in
 * the deployed webApp. The roster is STATIC content (the fixed 10 Method roles, no
 * backend wiring): role cards with generated avatars, each opening a charter drawer
 * (Owns / Does-NOT / Reviewed-by / chapter) with a secondary "View full prompt"
 * disclosure. Reached from the project-scoped AppShell's Team nav link.
 *
 * Pure-UI: a freshly created project mounts the shell + this static roster from
 * head-state with no draft, so this needs only the SPA + a Postgres-backed dev
 * server — NO Temporal/worker/model. It self-skips when that server is unreachable,
 * exactly like homebase.spec.
 */
import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer } from './support/gating.js';
import { createProjectFromLanding } from './support/flows.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

// One representative role per Method phase group, so the assertion fails if a whole
// section of the roster goes missing (System Design / Construction / Quality).
const SAMPLE_ROLE_IDS = ['system-architect', 'senior-developer', 'qa-engineer'] as const;

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

test('the Team nav opens the Method-roles roster with role cards', async ({ page }) => {
  await createProjectFromLanding(page);
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();

  await page.getByTestId(TESTID.teamNav).click();

  await expect(page.getByTestId(TESTID.teamScreen)).toBeVisible();
  await expect(page).toHaveURL(/\/project\/[^/]+\/team$/);

  for (const id of SAMPLE_ROLE_IDS) {
    await expect(page.getByTestId(TESTID.teamRoleCard(id))).toBeVisible();
  }
});

test('a role card opens its charter with the full-prompt disclosure', async ({ page }) => {
  await createProjectFromLanding(page);
  await page.getByTestId(TESTID.teamNav).click();
  await expect(page.getByTestId(TESTID.teamScreen)).toBeVisible();

  await page.getByTestId(TESTID.teamRoleCard('system-architect')).click();

  const drawer = page.getByTestId(TESTID.teamCharterDrawer);
  await expect(drawer).toBeVisible();

  // The charter's secondary disclosure reveals the raw agent prompt — proving the
  // static role data (charter + prompt), not just the avatar, is wired through.
  await drawer.getByTestId(TESTID.teamCharterTogglePrompt).click();

  await page.getByTestId(TESTID.teamCharterClose).click();
  await expect(drawer).toBeHidden();
});
