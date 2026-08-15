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
/**
 * openSharedProject gives a spec THE project for this test run.
 *
 * ONE PROJECT.JSON PER RUN (founder ruling 2026-08-14). A project's identity is
 * its repository: the local git substrate holds exactly one project.json per
 * repo, and `guardProjectIdentity` refuses a second project's write into a repo
 * another project already claimed. uitests.yml points the server at ONE bare
 * repo, so a suite where every spec created its own project could only ever have
 * its FIRST creation succeed — which is exactly what happened: ten specs failed
 * with "identity mismatch" on every main commit for a week while the two specs
 * that create nothing kept passing. The guard is right; creating N projects
 * against one repo was wrong. A new project means a new folder — and for this
 * suite, the new folder arrives with the next run.
 *
 * The create FLOW is still exercised once per run, by whichever spec asks first
 * (this function's create path asserts the dialog, the home base, and the URL
 * shape). Later callers navigate to the project already made. The catalog is
 * re-read rather than trusting cached module state, so a worker restart or a
 * retry adopts the existing project instead of trying to create a second one.
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
  if ((await existing.count()) > 0) {
    await existing.first().click();
    await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
    await expect(page).toHaveURL(/\/project\/[^/]+\/home$/);
    sharedProjectURL = page.url();
    return;
  }

  await createProjectFromLanding(page);
  sharedProjectURL = page.url();
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

/**
 * enterDesignExperience clicks "Resume design" / "Enter System Design" on the
 * home base and waits for the full-screen design experience to mount.
 */
export async function enterDesignExperience(page: Page): Promise<void> {
  await page.getByTestId(TESTID.resumeDesign).click();
  await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();
  // The experience deep-links to the ARTIFACT it opens on (…/design/system/mission),
  // so the artifact segment is optional here: this flow asserts "we are in the
  // system-design experience", not which step it happened to land on. Pinning the
  // bare route was a stale assertion — the SPA gained the artifact segment and the
  // flow kept demanding the URL stop short of it.
  await expect(page).toHaveURL(/\/project\/[^/]+\/design\/system(\/[^/]+)?$/);
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
