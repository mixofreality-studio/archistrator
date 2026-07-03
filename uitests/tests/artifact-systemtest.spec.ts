import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  // This spec asserts against the REAL committed N-STP/N-IT system-test-plan
  // content (scenario descriptions, case steps, operation names) — content that
  // only exists when the server's project-state git substrate is pointed at a
  // repo seeded from this checkout's .aiarch/state/project.json (see
  // gating.ts). CI's project-creation specs deliberately run against a fresh,
  // empty project-state repo, so this spec self-skips there rather than failing
  // on infra it was never given.
  await skipUnlessConstructionArtifacts(request, BASE);
});

// The classification→renderer seam for the testing family:
//  - N-STP (testing:plan) renders TestPlanView — the black-box operation-sequence
//    scenarios (transport-agnostic manager operations), one react-flow per use case.
//  - N-IT (testing:systemTest) renders SystemTestView — run summary / defects, or the
//    honest empty state until real runs exist.
//
// Both scenarios and cases are pickers now (ScenarioBrowser: a scenario dropdown,
// then a happy/negative/boundary case-chip row within it) — drive both explicitly
// rather than relying on the first-of-each default, so the test exercises the
// real selection UI black-box (roles/labels + published testids only).
test('N-STP renders the black-box system-test-plan sequences', async ({ page }) => {
  await gotoApp(page, '/project/archistrator/construction');
  await page.getByTestId(TESTID.constructionTabArtifacts).click();
  await page.getByTestId(TESTID.constructionArtifactRow('N-STP')).click();

  const view = page.getByTestId(TESTID.constructionTestPlanView);
  await expect(view).toBeVisible();
  await expect(view).toContainText('black-box'); // plan header copy

  // Select the "Drive System Design" (STP-UC1) scenario via the picker.
  await page.getByTestId(TESTID.constructionScenarioPicker).click();
  await page.getByRole('option', { name: /drive-system-design/ }).click();
  // Select its happy-path case (STP-UC1-H1) via the case-chip row.
  await page.getByTestId(TESTID.constructionCaseChip('STP-UC1-H1')).click();

  await expect(view).toContainText('WHAT THIS PROVES'); // book-grounded what/why summary
  await expect(view).toContainText('call chain'); // summary names the use-case call chain
  await expect(view).toContainText('CreateProject'); // a real manager operation (step 1 of the case)
  await expect(view).toContainText('STP-UC1'); // use-case selector / trace (scenario id)
});

test('N-IT runs the plan against the real build (scenarios driving green)', async ({ page }) => {
  await gotoApp(page, '/project/archistrator/construction');
  await page.getByTestId(TESTID.constructionTabArtifacts).click();
  await page.getByTestId(TESTID.constructionArtifactRow('N-IT')).click();

  const view = page.getByTestId(TESTID.constructionSystemTestView);
  await expect(view).toBeVisible();
  // Run summary tile: scenarios green / total. Not hardcoded to a specific
  // numerator — the honest count reflects whichever steps N-IT has actually
  // driven green in the committed plan, and grows over time as real runs land.
  await expect(view).toContainText(/\d+\/5/);

  await page.getByTestId(TESTID.constructionScenarioPicker).click();
  await page.getByRole('option', { name: /drive-system-design/ }).click();
  await page.getByTestId(TESTID.constructionCaseChip('STP-UC1-H1')).click();

  await expect(view).toContainText('CreateProject'); // the same operation sequences, executed
});
