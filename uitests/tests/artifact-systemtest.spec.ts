import { test, expect } from './support/dispatchGuard.js';
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
// TASK 13 FOUND A REAL BUG, NOT JUST A GAP TO ROUTE AROUND:
// N-STP (testing:plan) and N-IT (testing:systemTest) then both carried
// `hasBuildEvidence: false` (N-STP has since been backfilled Done from its
// committed plan; N-IT still has no record, so it now exercises the bypass
// alone). The
// detail pane's bodyDispatch.detailBodyFor gated on the SELECTED node's
// TaskDetailState BEFORE it ever asked which classification/renderer applies,
// so `state === 'unknown' || state === 'notStarted'` short-circuited to the
// unknown body for every selection depth on a no-evidence row — even though
// N-STP's 5 committed scenarios were sitting right there on the wire
// (`project.testingState.systemTestPlan.scenarios`). `hasBuildEvidence`
// answers "did the BUILD progress"; a committed test plan is a real artifact
// that exists independently of that question, so gating it on build evidence
// hid true information.
//
// THE FIX: `bodyDispatch.testingArtifactRendererKeyFor` renders the artifact
// body for `testing:plan`/`testing:systemTest` whenever the corresponding
// testing-state artifact (systemTestPlan for N-STP, a recorded test run for
// N-IT) is actually present, regardless of hasBuildEvidence — reachable from
// the bare activity row and from the plan's own phases/tasks (Plan Authoring/
// Plan Review). The task's own STATE stays honest (still `Not started`): only
// the rendered BODY changes, and `service`/`uiDesign`/`frontend` are untouched.
test('N-STP renders its committed plan, and its state chip reads the backfilled truth', async ({
  page,
}) => {
  await gotoApp(page, '/project/archistrator/construction');
  await page.getByTestId(TESTID.constructionLensSearch).getByRole('textbox').fill('N-STP');

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
  // The plan is a real, committed artifact.
  await expect(page.getByTestId(TESTID.constructionDetailBodyArtifact)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionTestPlanView)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionDetailBodyUnknown)).toHaveCount(0);
  // The header state chip reads the truth: N-STP's attempts were backfilled
  // from the committed plan and every one passed. (That they are reconstructed
  // rather than observed is the provenance axis's job — the hatched rail and
  // the group badge — pinned by construction-search-provenance-guarantee.)
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toContainText(/passed/i);
});

test('N-IT has no build evidence, but its recorded test run renders anyway — the task state stays honest', async ({
  page,
  request,
}) => {
  await gotoApp(page, '/project/archistrator/construction');
  await page.getByTestId(TESTID.constructionLensSearch).getByRole('textbox').fill('N-IT');

  const row = page.getByTestId(TESTID.constructionListRow('N-IT'));
  await expect(row).toBeVisible({ timeout: 15_000 });
  await row.click();

  const pane = page.getByTestId(TESTID.constructionDetailPane);
  await expect(pane).toBeVisible();
  // N-IT's committed title is "System testing (terminal gate)".
  await expect(page.getByTestId(TESTID.constructionDetailBreadcrumb)).toContainText(
    'System testing'
  );
  // The committed corpus carries one `testingState.testRuns` entry, so the same
  // bypass that widens N-STP widens this row too: the run summary renders even
  // though N-IT itself has no attempts.
  await expect(page.getByTestId(TESTID.constructionDetailBodyArtifact)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionSystemTestView)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionDetailBodyUnknown)).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toContainText(/not started/i);

  // Fix round B (designer P1-12): N-IT has never been attempted, so nothing may
  // read as a failure. The view says "not run · N scenarios planned", its chip is
  // muted, and there is no amber green/total tile.
  const planned = await request.get(`${BASE}/api/v1/system-design/get-project/archistrator`, {
    headers: { Accept: 'application/json' },
  });
  const wire = (await planned.json()) as {
    testingState?: { systemTestPlan?: { scenarios?: unknown[] } };
  };
  const n = wire.testingState?.systemTestPlan?.scenarios?.length ?? 0;
  expect(n).toBeGreaterThan(0);
  const view = page.getByTestId(TESTID.constructionSystemTestView);
  await expect(view).toContainText(`not run · ${String(n)} scenarios planned`);
  await expect(view).not.toContainText('failing');
  await expect(view).not.toContainText(/scenarios green/i);
  await expect(view).toContainText('not run');
});
