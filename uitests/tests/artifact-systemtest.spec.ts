import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

// The classification→renderer seam: an N-IT (testing:systemTest) activity renders
// the bespoke SystemTestView (run summary + defect table) from project.testingState,
// not the generic honest-pointer card.
test('N-IT system-test activity renders the SystemTestView (runs + defects)', async ({ page }) => {
  await gotoApp(page, '/project/archistrator/construction');
  await page.getByTestId(TESTID.constructionTabArtifacts).click();
  await page.getByTestId(TESTID.constructionArtifactRow('N-IT')).click();

  const view = page.getByTestId(TESTID.constructionSystemTestView);
  await expect(view).toBeVisible();
  // seeded run summary + a run id + a defect id
  await expect(view).toContainText('92'); // total passed across runs
  await expect(view).toContainText('run-2026-07-01');
  await expect(view).toContainText('D-101');
});
