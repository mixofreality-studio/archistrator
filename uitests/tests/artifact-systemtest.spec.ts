import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

// The classification→renderer seam for the testing family:
//  - N-STP (testing:plan) renders TestPlanView — the black-box operation-sequence
//    scenarios (transport-agnostic manager operations), one react-flow per use case.
//  - N-IT (testing:systemTest) renders SystemTestView — run summary / defects, or the
//    honest empty state until real runs exist.
test('N-STP renders the black-box system-test-plan sequences', async ({ page }) => {
  await gotoApp(page, '/project/archistrator/construction');
  await page.getByTestId(TESTID.constructionTabArtifacts).click();
  await page.getByTestId(TESTID.constructionArtifactRow('N-STP')).click();

  const view = page.getByTestId(TESTID.constructionTestPlanView);
  await expect(view).toBeVisible();
  await expect(view).toContainText('black-box'); // plan header copy
  await expect(view).toContainText('createProject'); // a real manager operation
  await expect(view).toContainText('UC1'); // use-case trace
});

test('N-IT renders the SystemTestView (honest empty until real runs)', async ({ page }) => {
  await gotoApp(page, '/project/archistrator/construction');
  await page.getByTestId(TESTID.constructionTabArtifacts).click();
  await page.getByTestId(TESTID.constructionArtifactRow('N-IT')).click();

  const view = page.getByTestId(TESTID.constructionSystemTestView);
  await expect(view).toBeVisible();
});
