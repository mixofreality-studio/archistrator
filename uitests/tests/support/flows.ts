/**
 * Reusable black-box UI flows, expressed purely through the browser + testids.
 * No webApp imports, no API command dispatch — every action is a real click/fill,
 * exactly as systemtests drives the server only over its published surface.
 */
import { expect, type Page } from '@playwright/test';
import { TESTID } from './testids.js';
import { gotoApp } from './gating.js';

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

/**
 * enterDesignExperience clicks "Resume design" / "Enter System Design" on the
 * home base and waits for the full-screen design experience to mount.
 */
export async function enterDesignExperience(page: Page): Promise<void> {
  await page.getByTestId(TESTID.resumeDesign).click();
  await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();
  await expect(page).toHaveURL(/\/project\/[^/]+\/design\/system$/);
}

/** Default research corpus the first step needs before drafting can start. */
const RESEARCH_NOTE =
  'A small community-events coordination app. Organizers post events; members RSVP, ' +
  'comment, and get reminders. Volatilities: notification channels, identity provider, ' +
  'venue data source. The architect should draft from this through to deployment.';

/**
 * Per-step budget for a live model to reach the human gate and commit. The largest
 * Phase-1 artifacts (coreUseCases with activity diagrams, system, operationalConcepts)
 * draft thousands of JSON tokens and, on a LOCAL model, a single draft can take ~3
 * minutes — times up to maxRedraftAttempts cycles (draft → validate → PM-critique).
 * This is a generous CEILING, not the expected duration: a hosted/capable worker
 * (Claude) drafts each step in seconds and never approaches it.
 */
const STEP_GATE_TIMEOUT = 1_800_000;

/**
 * commitArtifactsThrough drives the live co-author loop along the spine, committing
 * each Phase-1 step in order UP TO AND INCLUDING `targetKind`, leaving the spine on
 * the target step with its committed artifact (the auto-advance after the final
 * approve lands on the next step, so we re-select the target to render it).
 *
 * This is the ONLY way committed `system` (with dynamicViews) / `operationalConcepts`
 * (with a deployment topology) artifacts enter a project's head-state in this
 * black-box harness — the harness links no webApp source and there is NO seed/import
 * API, so artifacts are produced solely by the real drafting workflow over the wire.
 * It therefore requires the full Postgres+Temporal+worker stack (UITESTS_LIVE_DRAFTING=1)
 * AND a model capable of converging each step to a committable artifact.
 *
 * Each step: request a draft (the first step starts the phase after research input),
 * wait for the gate, approve. The SPA auto-advances on approve.
 */
export async function commitArtifactsThrough(
  page: Page,
  targetKind: string,
  orderedKinds: readonly string[]
): Promise<void> {
  const targetIndex = orderedKinds.indexOf(targetKind);
  if (targetIndex < 0) throw new Error(`unknown artifact kind: ${targetKind}`);

  for (let i = 0; i <= targetIndex; i++) {
    const kind = orderedKinds[i];
    // The active step should already be `kind` (fresh project starts at index 0;
    // approve auto-advances). Select it defensively so the loop is order-robust.
    const step = page.getByTestId(TESTID.spineStep(kind));
    await expect(step).toBeVisible();
    await step.click();

    const cta = page.getByTestId(TESTID.requestDraft);
    const research = page.getByTestId(TESTID.researchInput);
    const generating = page.getByTestId(TESTID.generatingScene);
    const gate = page.getByTestId(TESTID.gatePanel);

    // Let the step settle into ONE of its actionable states. The FIRST step
    // (fresh project) shows the Request-draft CTA; on later steps, approve
    // auto-advances AND auto-starts the next draft, so the step is already
    // generating (or even at the gate) with no CTA to click.
    await Promise.race([
      cta.waitFor({ state: 'visible' }).catch(() => undefined),
      research.waitFor({ state: 'visible' }).catch(() => undefined),
      generating.waitFor({ state: 'visible' }).catch(() => undefined),
      gate.waitFor({ state: 'visible' }).catch(() => undefined),
    ]);

    // Click Request-draft only when it is actually offered.
    if (await cta.isVisible().catch(() => false)) {
      await cta.click();
      // The first step then surfaces the research-input panel (409 precondition).
      await Promise.race([
        research.waitFor({ state: 'visible' }).catch(() => undefined),
        generating.waitFor({ state: 'visible' }).catch(() => undefined),
        gate.waitFor({ state: 'visible' }).catch(() => undefined),
      ]);
    }

    if (await research.isVisible().catch(() => false)) {
      await page.getByTestId(TESTID.researchInputTitle).fill('Founder brief');
      await page.getByTestId(TESTID.researchInputText).fill(RESEARCH_NOTE);
      await page.getByTestId(TESTID.researchInputSubmit).click();
    }

    // Wait for the human gate, then approve to commit and auto-advance.
    await expect(gate).toBeVisible({ timeout: STEP_GATE_TIMEOUT });
    await page.getByTestId(TESTID.gateApprove).click();
    await expect(gate).toHaveCount(0, { timeout: 30_000 });
  }

  // Re-select the target so its committed artifact renders (approve advanced past it).
  await page.getByTestId(TESTID.spineStep(targetKind)).click();
  await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 30_000 });
}
