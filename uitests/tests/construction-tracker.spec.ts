/**
 * construction-tracker.spec — the Construction console's Tracker tab
 * (route `/project/$projectId/construction`, the default tab).
 *
 * Exercises the OBSERVABLE half of the Method core use case "Execute a
 * Construction Activity" (a busMessage-triggered flow: an eligible activity
 * is dispatched, a branch/PR opens, the run is observed, exit criteria are
 * checked — see .coreUseCases in project.json). The DISPATCH half needs a
 * live build cluster (R-CPR, claude-code-action against real GitHub Actions)
 * that is not provisioned in this harness — see ConstructionConsole.tsx's own
 * doc comment ("gated on a build cluster... not provisioned here, so the
 * session is usually quiet"). Clicking "Begin construction" against a shared
 * dev server would have real, uncontrolled side effects (real job dispatch),
 * so this spec does not click it.
 *
 * What IS exercisable today, against the real committed head-state: opening
 * the Tracker, selecting a real CPM activity node, and reading its App-A
 * activity-tracking detail (kind, status, binary-exit life-cycle phases) —
 * the "Observe run; validate against exit criteria" step of the same use
 * case, driven by real committed ActivityConstruction data (this repo
 * dogfoods its own construction phase — see the "archistrator" project's
 * committed .activityConstruction).
 *
 * Gated like artifact-systemtest.spec: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy. No live drafting needed.
 */
import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';
import { tagUseCase } from './support/useCases.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

test('the Tracker renders the committed CPM network and an activity node opens its App-A tracking detail', async ({
  page,
}) => {
  tagUseCase('execute-a-construction-activity');

  await gotoApp(page, '/project/archistrator/construction');
  // Tracker is the default tab — no click needed — but the tab bar itself
  // proves the console mounted on the right section.
  await expect(page.getByTestId(TESTID.constructionTabTracker)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionTracker)).toBeVisible();

  // Structural assertion: the CPM graph's activity nodes are react-flow-
  // generated DOM with no per-node data-testid today (UIIdentifiers.
  // Construction.trackerNode is declared but not yet wired into the shared
  // NetworkView node renderer) — select the first rendered node by the
  // generated `.react-flow__node` class, the SAME escape hatch
  // architecture-views.spec uses for the same library/limitation.
  // eslint-disable-next-line no-restricted-syntax -- see comment above
  const firstNode = page.locator('.react-flow__node').first();
  await expect(firstNode).toBeVisible({ timeout: 15_000 });
  await firstNode.click();

  const panel = page.getByTestId(TESTID.activityLifecyclePanel);
  await expect(panel).toBeVisible();
  // The header ("ACTIVITY TRACKING · APP A") always renders once a node is
  // selected — real content, not a stub, proving the click drove a genuine
  // activity selection against committed data.
  await expect(panel).toContainText('ACTIVITY TRACKING');
});
