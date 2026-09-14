/**
 * construction-resume.spec — a paused project offers Resume in Begin's place (plan B1.7;
 * founder ruling 2026-09-13: "Begin on a paused project REFUSES — Paused — Resume to
 * continue").
 *
 * The seeded project is not paused, so each case that needs a pause EDITS the
 * get-project read on its way to the page (operatorPaused, pauseReason), exactly as the
 * server now sends it.
 *
 * SAFETY: the shared dispatch guard (support/dispatchGuard) aborts every non-GET before
 * any navigation. On top of it, resume-project and execute-next-activity are TRAPPED by
 * page.route handlers installed before the page opens, so no case can resume or begin
 * anything on a live server. The trapped calls are what the cases assert on.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const PAUSED = 'Paused — Resume to continue';

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface Trapped {
  resume: string[];
  begin: string[];
}

/** Trap both writes before the page opens. `resumeStatus` answers the resume with that
 *  status (never reaching a server); without it the resume is aborted. */
async function trapWrites(
  page: Page,
  resumeStatus?: { status: number; error: string }
): Promise<Trapped> {
  const trapped: Trapped = { resume: [], begin: [] };
  await page.route('**/execute-next-activity/**', async (route) => {
    trapped.begin.push(route.request().url());
    await route.abort();
  });
  await page.route('**/resume-project/**', async (route) => {
    trapped.resume.push(`${route.request().method()} ${route.request().url()}`);
    if (resumeStatus === undefined) {
      await route.abort();
      return;
    }
    await route.fulfill({
      status: resumeStatus.status,
      contentType: 'application/json',
      body: JSON.stringify({ error: resumeStatus.error }),
    });
  });
  return trapped;
}

/** Serve every get-project read with the operator's pause recorded. */
async function pauseTheRead(page: Page, reason: string): Promise<void> {
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const fetchWire = async () => {
      const response = await route.fetch();
      return {
        response,
        wire: (await response.json()) as Record<string, unknown>,
      };
    };
    try {
      let got: Awaited<ReturnType<typeof fetchWire>>;
      try {
        got = await fetchWire();
      } catch (err) {
        // A fetched body can be disposed before json() reads it; the read is an
        // idempotent GET on a still-unhandled route, so it is fetched once more.
        if (page.isClosed() || !/has been disposed/.test(String(err))) throw err;
        got = await fetchWire();
      }
      got.wire.operatorPaused = true;
      got.wire.pauseReason = reason;
      await route.fulfill({ response: got.response, json: got.wire });
    } catch (err) {
      if (page.isClosed() || /has been closed/.test(String(err))) return;
      throw err;
    }
  });
}

test('a paused project shows Resume in Begin’s place, with the founder’s label and the reason', async ({
  page,
}) => {
  const trapped = await trapWrites(page);
  await pauseTheRead(page, 'operator halt');
  await gotoApp(page, '/project/archistrator/construction?lens=list');

  const resume = page.getByTestId(TESTID.constructionResume);
  await expect(resume).toBeVisible();
  await expect(resume).toHaveText('Resume paused construction');
  await expect(resume).toBeEnabled();
  await expect(page.getByTestId(TESTID.constructionBegin)).toHaveCount(0);
  const label = page.getByTestId(TESTID.constructionPausedLabel);
  await expect(label).toHaveText(PAUSED);
  await expect(label).toHaveAttribute('title', 'operator halt');
  expect(trapped.resume).toEqual([]);
  expect(trapped.begin).toEqual([]);
});

test('the Tasks lens says the project is paused', async ({ page }) => {
  await trapWrites(page);
  await pauseTheRead(page, 'operator halt');
  await gotoApp(page, '/project/archistrator/construction?lens=tasks');
  const label = page.getByTestId(TESTID.constructionTasksPausedLabel);
  await expect(label).toBeVisible();
  await expect(label).toContainText(PAUSED);
  await expect(label).toContainText('(operator halt)');
});

test('Resume sends ONE resume-project POST through the ops client, and never a Begin', async ({
  page,
}) => {
  const trapped = await trapWrites(page);
  await pauseTheRead(page, 'operator halt');
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await page.getByTestId(TESTID.constructionResume).click();
  await expect.poll(() => trapped.resume.length).toBe(1);
  expect(trapped.resume[0]).toMatch(
    /^POST .*\/api\/v1\/construction\/resume-project\/archistrator$/
  );
  expect(trapped.begin).toEqual([]);
  // The trapped request never got an answer: the outcome is unknown, never "resumed".
  const outcome = page.getByTestId(TESTID.constructionResumeOutcome);
  await expect(outcome).toHaveAttribute('data-outcome', 'unknown');
  await expect(outcome).toContainText('Outcome unknown');
});

test('a refused resume says so loudly and does not claim the project resumed', async ({ page }) => {
  const trapped = await trapWrites(page, {
    status: 409,
    error: 'a pause is still being applied — retry in a moment',
  });
  await pauseTheRead(page, 'operator halt');
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await page.getByTestId(TESTID.constructionResume).click();
  const outcome = page.getByTestId(TESTID.constructionResumeOutcome);
  await expect(outcome).toHaveAttribute('data-outcome', 'rejected');
  await expect(outcome).toContainText('Resume refused');
  await expect(outcome).toContainText('still paused');
  expect(trapped.resume.length).toBe(1);
  // Still paused: Resume stays, Begin never appears.
  await expect(page.getByTestId(TESTID.constructionResume)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionBegin)).toHaveCount(0);
});

test('a project that is not paused still offers Begin, and no Resume', async ({ page }) => {
  await trapWrites(page);
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionBegin)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionResume)).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionPausedLabel)).toHaveCount(0);
});
