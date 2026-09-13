/**
 * construction-fix-e.spec — fix round E minors (fix-D review M1, M3).
 *
 *  - M1  The deep-link memory is per PROJECT. It was keyed by a/p/k alone, so an
 *        in-app switch to a second project with the same N-STP link found it
 *        "already shown", and its row stayed hidden under a closed chevron.
 *  - M3  A never-run N-IT's active case has its EXPECT label and left border in
 *        neutral ink, while N-STP's plan keeps its red. It draws no call-chain
 *        CHECKS chip either: there is no verdict to report.
 *
 * The second project is the real archistrator read, served in the browser under
 * another id. The read itself goes to the server as a GET; nothing is written.
 *
 * SAFETY: the shared dispatch guard (support/dispatchGuard) aborts every non-GET
 * before any navigation. Nothing here presses Run, Begin or Retry.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

async function openList(page: Page, suffix: string): Promise<void> {
  await page.setViewportSize({ width: 1280, height: 900 });
  await gotoApp(page, `/project/archistrator/construction?lens=list${suffix}`);
  await expect(page.getByTestId(TESTID.constructionListRow('C-billing-engine'))).toBeVisible({
    timeout: 15_000,
  });
}

// ---------------------------------------------------------------------------
// M1
// ---------------------------------------------------------------------------

const TWIN = 'archistrator-twin';

test('M1: an in-app switch to a second project with the same deep link reveals its row', async ({
  page,
}) => {
  await page.route(`**/system-design/get-project/${TWIN}**`, async (route) => {
    const response = await route.fetch({
      url: route.request().url().replace(TWIN, 'archistrator'),
    });
    await route.fulfill({ response });
  });
  const link = '?lens=list&a=N-STP&p=construction&k=codeReview';
  const taskRow = page.getByTestId(TESTID.constructionListRow('N-STP::construction::codeReview'));
  await openList(page, '&a=N-STP&p=construction&k=codeReview');
  await expect(taskRow).toBeVisible();

  // An IN-APP switch (the router follows a history change; the document stays),
  // so the module memory of the last link shown survives it.
  await page.evaluate(() => {
    (window as unknown as { __sameDocument?: boolean }).__sameDocument = true;
  });
  await page.evaluate((to) => {
    window.history.pushState(null, '', to);
    window.dispatchEvent(new PopStateEvent('popstate', { state: null }));
  }, `/project/${TWIN}/construction${link}`);
  await expect(page).toHaveURL(new RegExp(`/project/${TWIN}/construction`));
  await expect(taskRow).toBeVisible({ timeout: 15_000 });
  expect(
    await page.evaluate(
      () => (window as unknown as { __sameDocument?: boolean }).__sameDocument === true
    ),
    'no reload: the switch happened in-app'
  ).toBe(true);
});

// ---------------------------------------------------------------------------
// M3
// ---------------------------------------------------------------------------

interface ViewInk {
  /** How many call-chain CHECKS chips the view draws. */
  checksChips: number;
  /** How many step-through calls carry a status (so the flow did render). */
  steps: number;
  activeCaseInk: string | null;
  activeBorder: string;
  expectColor: string;
  /** The view's own muted headline ink, the neutral to compare against. */
  neutral: string;
}

async function viewInk(page: Page, viewId: string): Promise<ViewInk> {
  return page.evaluate(
    ({ view, chip, active, expectId }) => {
      const root = document.querySelector(`[data-testid="${view}"]`);
      if (root === null) throw new Error(`no ${view}`);
      const box = root.querySelector<HTMLElement>(`[data-testid="${active}"]`);
      const exp = root.querySelector<HTMLElement>(`[data-testid="${expectId}"]`);
      const headline = root.firstElementChild as HTMLElement;
      return {
        checksChips: root.querySelectorAll(`[data-testid="${chip}"]`).length,
        steps: root.querySelectorAll('[data-step-status]').length,
        activeCaseInk: box?.getAttribute('data-case-ink') ?? null,
        activeBorder: box === null ? '' : getComputedStyle(box).borderLeftColor,
        expectColor: exp === null ? '' : getComputedStyle(exp).color,
        neutral: getComputedStyle(headline).color,
      };
    },
    {
      view: viewId,
      chip: TESTID.archCcChecksChip,
      active: TESTID.constructionActiveCase,
      expectId: TESTID.constructionCaseExpect,
    }
  );
}

// The chip lives on the fragment card, which only the Architecture walkthrough
// mounts. The test views drive the flow by its step bar, so neither N-IT nor
// N-STP ever draws it, and the review's "chip renders for planned" mutant is
// equivalent on this surface. The rule itself (no chip for 'planned') is pinned in
// node: fragmentCaption.test.ts, ccChecksChipShown. This case guards the surface.
test('M3: a never-run N-IT draws no call-chain CHECKS chip over its planned calls', async ({
  page,
}) => {
  await openList(page, '&a=N-IT');
  const nit = page.getByTestId(TESTID.constructionSystemTestView);
  await expect(nit).toContainText('not run');
  await expect
    .poll(async () => (await viewInk(page, TESTID.constructionSystemTestView)).steps)
    .toBeGreaterThan(0);
  expect((await viewInk(page, TESTID.constructionSystemTestView)).checksChips).toBe(0);
});

test('M3: on a never-run N-IT the active NEGATIVE case’s EXPECT and border are neutral; on N-STP, red', async ({
  page,
}) => {
  await openList(page, '&a=N-IT');
  const nit = page.getByTestId(TESTID.constructionSystemTestView);
  await nit.getByTestId(TESTID.constructionCaseChip('STP-UC1-N1')).click();
  await expect(nit.getByTestId(TESTID.constructionActiveCase)).toHaveAttribute('data-case-ink', 'neutral');
  await expect(nit.getByTestId(TESTID.constructionCaseExpect)).toBeVisible();
  const never = await viewInk(page, TESTID.constructionSystemTestView);
  expect(never.activeBorder).toBe(never.neutral);
  expect(never.expectColor).toBe(never.neutral);

  // N-STP's plan: the same negative case in danger ink, so neither is the neutral.
  await openList(page, '&a=N-STP');
  const plan = page.getByTestId(TESTID.constructionTestPlanView);
  await plan.getByTestId(TESTID.constructionCaseChip('STP-UC1-N1')).click();
  await expect(plan.getByTestId(TESTID.constructionActiveCase)).toHaveAttribute('data-case-ink', 'danger');
  const planned = await viewInk(page, TESTID.constructionTestPlanView);
  expect(planned.activeBorder).not.toBe(planned.neutral);
  expect(planned.expectColor).not.toBe(planned.neutral);
  expect(planned.expectColor).toBe(planned.activeBorder);
});
