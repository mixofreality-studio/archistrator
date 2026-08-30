/**
 * deployment-lens.spec — the Architecture step's 4th lens: the committed deployment
 * topology, rendered by the SAME DeploymentFlow the retired Deployment & Operations
 * step used to render, WITH the live health overlay that step used to fetch.
 *
 * ── Why this spec exists ────────────────────────────────────────────────────
 * Phase 1 collapsed to Requirements + Architecture (2026-08-30), retiring the
 * Deployment & Operations step. The topology it used to host is a retired-in-place
 * wire kind (`operationalConcepts`) that ArchitectureView still joins off the project
 * head-state through CommittedSlotsContext — so the DIAGRAM outlived the PAGE. The
 * deployment assertions that used to live in architecture-views.spec drove the
 * removed page's profile switcher and could not be kept: that spec is live-gated, and
 * no live run reaches a committed `operationalConcepts` slot any more now that the
 * step is out of the drafting sequence.
 *
 * So the coverage moved here and became HERMETIC (the volatility-map/glossary
 * tactic): the wire is route-intercepted with a project carrying a committed `system`
 * AND a committed topology, and the REAL SPA renders it. No infra gating — only the
 * SPA process is required.
 *
 * ── The two health arms ─────────────────────────────────────────────────────
 * The health overlay was ALSO the page's, fetched by a view on the components/hooks
 * legacy allowlist. Re-homing the diagram dropped it until DeploymentHealthContext
 * restored it, so both arms are covered here:
 *
 *   1. ABSENT (the local profile — operations capability false, D9): nothing fetches,
 *      the topology still renders, and — the case most likely to regress silently —
 *      no node reads as UNHEALTHY just because nothing was observed.
 *   2. PRESENT: a stubbed unhealthy reading reaches the diagram and visibly changes
 *      the node it names, proving the container → provider → lens → DeploymentFlow
 *      path is actually connected.
 *
 * Arm 2 asserts the tint as a DIFFERENCE against arm 1's own rendering of the same
 * node rather than against a hard-coded colour: health is expressed only as a border
 * colour (DeploymentNodes.tsx — no accessible name, no testid), so pinning a literal
 * would couple the spec to theme tokens, while a same-node before/after comparison
 * proves the overlay landed without asserting which red it chose.
 *
 * The lens is instance-less by design — the per-profile choice belonged to the retired
 * step, so the first committed environment (`cloud`) is what renders and there is no
 * switcher left to assert on (its `deploy-profile-switch` testid was deleted with the
 * page, and this package forbids hand-typed testid strings).
 */
import { test, expect, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { stubCommittedArchitecture, type StubNodeHealth } from './support/designStubs.js';

// xyflow renders deployment boxes as DOM nodes carrying its own generated class;
// they publish no testid/role of their own.
const NODE = '.react-flow__node';

/** OperationsHealthState ordinal for Unhealthy (0 Neutral, 1 Healthy, 2 Unhealthy). */
const UNHEALTHY = 2;
/** A topology element key from the stub's `cloud` environment (an infrastructure
 *  node, whose renderer takes the health border). */
const OBSERVED_KEY = 'infra-db';
const OBSERVED_LABEL = 'ProjectStateDB';

/** Stub the committed architecture + topology and open the Architecture step. */
async function openCommittedArchitecture(page: Page, health?: StubNodeHealth[]): Promise<void> {
  const projectId = await stubCommittedArchitecture(page, health);
  await page.goto(`/project/${projectId}/design/system`);
  await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();

  await page.getByTestId(TESTID.spineStep('system')).click();
  await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible();
  await expect(page.getByTestId(TESTID.archViewSwitch)).toBeVisible();
}

/** Switch to the Deployment lens and wait for the topology to paint. */
async function openDeploymentLens(page: Page): Promise<void> {
  const deploymentToggle = page
    .getByTestId(TESTID.archViewSwitch)
    .getByRole('button', { name: /deployment/i });
  await expect(deploymentToggle).toBeVisible();
  await expect(deploymentToggle).toBeEnabled();
  await deploymentToggle.click();
  // Structural assertion: xyflow deployment nodes carry no data-testid/role of their
  // own; selecting by the generated `.react-flow__node` DOM class is the only way to
  // prove the lens actually drew the topology.
  // eslint-disable-next-line no-restricted-syntax -- see comment above
  await expect(page.locator(NODE).first()).toBeVisible();
}

/**
 * The rendered border colour of the observed infrastructure node — the ONLY way the
 * health overlay manifests (DeploymentNodes.tsx sets a border colour; there is no
 * accessible name or testid carrying health). Reached from the node's visible label
 * rather than by a CSS/XPath selector: xyflow wraps each custom node in its own
 * unstyled `[data-id]` element, so the styled root is one level inside it.
 */
async function observedNodeBorderColor(page: Page): Promise<string> {
  const label = page.getByText(OBSERVED_LABEL).first();
  await expect(label).toBeVisible();
  return label.evaluate((el) => {
    const wrapper = el.closest('[data-id]');
    const styled = wrapper?.firstElementChild ?? wrapper;
    return styled === null || styled === undefined ? '' : getComputedStyle(styled).borderColor;
  });
}

test.describe('architecture deployment lens (stubbed committed topology — hermetic)', () => {
  test('the Deployment lens renders the committed topology', async ({ page }) => {
    await openCommittedArchitecture(page);
    await openDeploymentLens(page);

    // eslint-disable-next-line no-restricted-syntax -- xyflow nodes carry no testid
    const nodes = page.locator(NODE);
    await expect.poll(() => nodes.count()).toBeGreaterThanOrEqual(2);

    // The empty arm must NOT be what rendered.
    await expect(page.getByText('No deployment topology committed yet.')).toHaveCount(0);

    // The stubbed topology's own content reaches the canvas.
    await expect(page.getByText('Design cluster')).toBeVisible();
    await expect(page.getByText(OBSERVED_LABEL)).toBeVisible();
  });

  test('with NO health data (the local profile) the topology renders untinted, not unhealthy', async ({
    page,
  }) => {
    // capabilities answers operations:false, so useDeploymentHealth never fires and
    // the provider hands the lens `undefined`.
    const healthRequests: string[] = [];
    page.on('request', (r) => {
      if (r.url().includes('query-deployment-health')) healthRequests.push(r.url());
    });

    await openCommittedArchitecture(page);
    await openDeploymentLens(page);

    // The diagram is fully rendered…
    await expect(page.getByText('Design cluster')).toBeVisible();
    await expect(page.getByText(OBSERVED_LABEL)).toBeVisible();
    // …and the overlay stayed dormant: the gated read was never issued at all.
    expect(healthRequests).toHaveLength(0);

    // Absence must not paint the node as failing — it keeps its ordinary border.
    // The next test pins the same node's observed-unhealthy tint and proves the two
    // differ, which is what makes this a real assertion rather than a tautology.
    const neutral = await observedNodeBorderColor(page);
    expect(neutral).not.toBe('');
  });

  test('a live UNHEALTHY reading tints the node it names, and absence does not', async ({
    page,
  }) => {
    // Baseline: the same node, same environment, with the overlay dormant.
    await openCommittedArchitecture(page);
    await openDeploymentLens(page);
    const neutral = await observedNodeBorderColor(page);

    // Now with operations:true and an unhealthy reading for that element key. This is
    // the whole restored path in one assertion: container hook → DeploymentHealthProvider
    // → ArchitectureView → DeploymentFlow → DeploymentNodes.
    await openCommittedArchitecture(page, [{ ModelKey: OBSERVED_KEY, Health: UNHEALTHY }]);
    await openDeploymentLens(page);
    await expect.poll(() => observedNodeBorderColor(page)).not.toBe(neutral);

    // …and the unobserved rendering was NOT already showing the unhealthy tint (the
    // silent-regression case: absence rendering as red would make these equal).
    const unhealthy = await observedNodeBorderColor(page);
    expect(neutral).not.toBe(unhealthy);
  });
});
