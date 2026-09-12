import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  // This spec asserts against the REAL committed head-state — content that
  // only exists when the server's project-state git substrate is pointed at a
  // repo seeded from this checkout's .aiarch/state/project.json (see
  // gating.ts). CI's project-creation specs deliberately run against a fresh,
  // empty project-state repo, so this spec self-skips there rather than failing
  // on infra it was never given.
  await skipUnlessConstructionArtifacts(request, BASE);
});

// The classification→renderer seam for the testing family, reached through the
// LIST lens + detail pane now that Task 13 retired the Artifacts tab (which
// dispatched TestPlanView/SystemTestRunView unconditionally from
// classify(row), ignoring the row's own evidence).
//
// TASK 13 FINDING (live-verified, not a wiring gap this task can route around):
// N-STP (testing:plan) and N-IT (testing:systemTest) — like all 5 testing-kind
// activities in the committed corpus — carry `hasBuildEvidence: false`. The
// detail pane's bodyDispatch.detailBodyFor (Task 10) gates on the SELECTED
// node's TaskDetailState BEFORE it ever asks which classification/renderer
// applies: `state === 'unknown' || state === 'notStarted'` short-circuits to
// the unknown body for EVERY selection depth on a no-evidence row (activity,
// phase, or task — verified for all three). TestPlanView itself does not read
// evidence at all (`project.testingState.systemTestPlan.scenarios`), so the
// content these tests used to assert is not gone from the data — it is simply
// unreachable through the honest surface until one of these two activities
// carries real build evidence (Task 1's "no evidence, no claim" rule, applied
// uniformly, not special-cased for the testing family). Rewiring that gate is
// out of scope here — it is Task 10's already-reviewed logic — so these tests
// now pin the CURRENT honest behaviour instead of content that is currently
// unreachable. Flagged for whoever next touches testing-kind construction data
// or the bodyDispatch gate.
test('N-STP has no build evidence, so its detail pane renders the honest unknown body, not the plan content', async ({
  page,
}) => {
  await gotoApp(page, '/project/archistrator/construction');
  await page.getByTestId(TESTID.constructionLensSearch).locator('input').fill('N-STP');

  const row = page.getByTestId(TESTID.constructionListRow('N-STP'));
  await expect(row).toBeVisible({ timeout: 15_000 });
  await row.click();

  const pane = page.getByTestId(TESTID.constructionDetailPane);
  await expect(pane).toBeVisible();
  // The breadcrumb names the activity-list TITLE, not the raw id (DetailPane
  // falls back to the id only when no title is committed) — N-STP's is
  // "System test plan (all core use cases)".
  await expect(page.getByTestId(TESTID.constructionDetailBreadcrumb)).toContainText(
    'System test plan'
  );
  // The honest consequence of no build evidence: the unknown body, never the
  // artifact body — TestPlanView/ScenarioBrowser do not render for this row.
  await expect(page.getByTestId(TESTID.constructionDetailBodyUnknown)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionTestPlanView)).toHaveCount(0);
});

test('N-IT has no build evidence, so its detail pane renders the honest unknown body, not the run summary', async ({
  page,
}) => {
  await gotoApp(page, '/project/archistrator/construction');
  await page.getByTestId(TESTID.constructionLensSearch).locator('input').fill('N-IT');

  const row = page.getByTestId(TESTID.constructionListRow('N-IT'));
  await expect(row).toBeVisible({ timeout: 15_000 });
  await row.click();

  const pane = page.getByTestId(TESTID.constructionDetailPane);
  await expect(pane).toBeVisible();
  // N-IT's committed title is "System testing (terminal gate)".
  await expect(page.getByTestId(TESTID.constructionDetailBreadcrumb)).toContainText(
    'System testing'
  );
  await expect(page.getByTestId(TESTID.constructionDetailBodyUnknown)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionSystemTestView)).toHaveCount(0);
});
