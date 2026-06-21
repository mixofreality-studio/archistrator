/**
 * architecture-views.spec — the three new view families on committed Phase-1
 * artifacts:
 *
 *   • System artifact   → ArchitectureView: a segmented control (`arch-view-switch`)
 *     toggling Static / Dynamic / Component-focus, each with its own picker
 *     (`arch-dynamic-picker`, `arch-perspective-picker`).
 *   • operationalConcepts → OperationalConceptsView: a Deployment section with a
 *     profile switcher (`deploy-profile-switch`) over the present profiles
 *     (cloud / local / test), each rendering its deployment topology.
 *
 * ── Why these are LIVE-only (UITESTS_LIVE_DRAFTING=1) ────────────────────────
 * These views render COMMITTED head-state artifacts. This harness is strictly
 * black-box (the UI sibling of ../systemtests): it links zero webApp source and
 * drives the real SPA → real Go server over the wire. There is NO seed / import /
 * fixture API — the only way a `system` (with dynamicViews) or `operationalConcepts`
 * (with a deployment topology) slot reaches a project's head-state is to run the
 * real co-author drafting workflow and commit every predecessor in order (the
 * Manager's precondition gate enforces required predecessors). That needs the full
 * Postgres + Temporal + worker stack — hence the same UITESTS_LIVE_DRAFTING gate as
 * design-experience.spec's drafting block.
 *
 * Assertions are deliberately black-box and resilient to the non-deterministic
 * model output: we assert the SWITCHER CONTROLS render and respond (the deployment
 * switcher actually changes the rendered topology), and — where the committed shape
 * supports it — that numbered dynamic edges and perspective related nodes appear,
 * tolerating a model that produced a thinner artifact (mirroring systemtests, which
 * also refuse to hard-gate on local-model output shape).
 */
import { test, expect, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessLiveDrafting } from './support/gating.js';
import {
  createProjectFromLanding,
  enterDesignExperience,
  commitArtifactsThrough,
} from './support/flows.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

// The ordered Phase-1 kinds the spine commits through (mirrors testids.PHASE1_ARTIFACTS).
const ORDERED_KINDS = [
  'mission',
  'glossary',
  'scrubbedRequirements',
  'volatilities',
  'coreUseCases',
  'system',
  'operationalConcepts',
] as const;

// xyflow renders edge labels and node titles as DOM text inside the canvas.
const EDGE_TEXT = '.react-flow__edge-text';
const NODE = '.react-flow__node';

test.describe('architecture & deployment views (live backend — UITESTS_LIVE_DRAFTING=1)', () => {
  // ONE test drives the full Phase-1 spine ONCE (committing every artifact up to
  // operationalConcepts) and then asserts all three new view families against that
  // single committed project. Driving the chain once — rather than re-drafting the
  // whole spine per assertion — is the only affordable shape for a live model: it
  // pays the (slow, several-minute-per-heavy-artifact) convergence cost a single
  // time. The budget must cover seven sequential step gates plus the assertions.
  test.describe.configure({ timeout: 12_000_000 });

  test.beforeEach(async ({ request }) => {
    skipUnlessLiveDrafting();
    await skipUnlessServer(request, BASE);
  });

  test('dynamic, component-focus, and deployment views render on a committed project', async ({
    page,
  }) => {
    await createProjectFromLanding(page);
    await enterDesignExperience(page);
    // Drive the whole spine through operationalConcepts (the furthest artifact),
    // committing system (with dynamicViews) and operationalConcepts (with a
    // deployment topology) along the way. Leaves the spine on operationalConcepts.
    await commitArtifactsThrough(page, 'operationalConcepts', ORDERED_KINDS);

    // ── Deployment views (operationalConcepts step) ──────────────────────────
    // The Deployment section's profile switcher renders one toggle per present
    // profile (cloud / local / test). It exists only when a topology was committed.
    const profileSwitch = page.getByTestId(TESTID.deployProfileSwitch);
    await expect(profileSwitch).toBeVisible();

    const profiles = profileSwitch.getByRole('button');
    const profileCount = await profiles.count();
    expect(profileCount).toBeGreaterThanOrEqual(1);

    // Each present profile renders deployment nodes; switching re-renders the
    // topology. Capture each profile's node text to prove the switch is honoured.
    const renderings: string[] = [];
    for (let i = 0; i < profileCount; i++) {
      await profiles.nth(i).click();
      const nodes = page.locator(NODE);
      await expect(nodes.first()).toBeVisible({ timeout: 15_000 });
      renderings.push((await nodes.allInnerTexts()).join('|'));
    }
    // A multi-profile (deliveryStyle `both`) topology: ≥2 profiles must differ —
    // proving the switch actually swapped the rendered topology.
    if (profileCount >= 2) {
      expect(new Set(renderings).size).toBeGreaterThanOrEqual(2);
    }

    // ── Architecture views (system step) ─────────────────────────────────────
    // Re-select the committed System step to render its ArchitectureView.
    await page.getByTestId(TESTID.spineStep('system')).click();
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 30_000 });

    // Dynamic lens: pick the first use case → participants + numbered call edges.
    await page.getByTestId(TESTID.archViewSwitch).getByRole('button', { name: /dynamic/i }).click();
    const dynamicPicker = page.getByTestId(TESTID.archDynamicPicker);
    await expect(dynamicPicker).toBeVisible();
    await dynamicPicker.click();
    await page.getByRole('option').first().click();
    await expect(page.locator(NODE).first()).toBeVisible();
    // At least one ordered call edge carries a numeric "1." sequence prefix.
    await expect(
      page.locator(EDGE_TEXT).filter({ hasText: /(^|\b)1\.\s/ }).first()
    ).toBeVisible();

    // Component-focus (perspective) lens: pick the first component → focus node
    // plus ≥1 related node (a Manager fans out, so a populated perspective ≥2 nodes).
    await page
      .getByTestId(TESTID.archViewSwitch)
      .getByRole('button', { name: /component focus|perspective/i })
      .click();
    const perspectivePicker = page.getByTestId(TESTID.archPerspectivePicker);
    await expect(perspectivePicker).toBeVisible();
    await perspectivePicker.click();
    await page.getByRole('option').first().click();
    const nodes = page.locator(NODE);
    await expect(nodes.first()).toBeVisible();
    await expect.poll(() => nodes.count(), { timeout: 15_000 }).toBeGreaterThanOrEqual(2);
  });
});
