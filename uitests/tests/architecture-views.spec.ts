/**
 * architecture-views.spec — the lens families on the committed System artifact:
 * ArchitectureView's segmented control (`arch-view-switch`) toggling Static /
 * Dynamic / Component-focus, each with its own picker (`arch-dynamic-picker`,
 * `arch-perspective-picker`).
 *
 * ── Why this is LIVE-only (UITESTS_LIVE_DRAFTING=1) ──────────────────────────
 * These views render COMMITTED head-state artifacts. This harness is strictly
 * black-box (the UI sibling of ../systemtests): it links zero webApp source and
 * drives the real SPA → real Go server over the wire. There is NO seed / import /
 * fixture API — the only way a `system` slot (with dynamicViews) reaches a
 * project's head-state is to run the real co-author drafting workflow and commit
 * every predecessor in order (the Manager's precondition gate enforces required
 * predecessors). That needs the full Postgres + Temporal + worker stack — hence the
 * same UITESTS_LIVE_DRAFTING gate as design-experience.spec's drafting block.
 *
 * ── The Deployment lens is NOT covered here (2026-08-30) ─────────────────────
 * The deployment-topology assertions this spec used to carry drove the Deployment &
 * Operations STEP and its profile switcher (`deploy-profile-switch`). That step is
 * retired: Phase 1 now ends at Architecture, so a project driven through this
 * harness never commits an `operationalConcepts` slot, and the Deployment lens
 * (which reads that slot) is correctly unavailable on every project this spec can
 * build. The lens itself survives and still renders for projects that HAVE a
 * committed topology — it is simply unreachable from a black-box live run until
 * something re-establishes how that slot gets committed. Flagged, not silently
 * dropped.
 *
 * Assertions are deliberately black-box and resilient to the non-deterministic
 * model output: we assert the SWITCHER CONTROLS render and respond, and — where the
 * committed shape supports it — that numbered dynamic edges and perspective related
 * nodes appear, tolerating a model that produced a thinner artifact (mirroring
 * systemtests, which also refuse to hard-gate on local-model output shape).
 */
// SAFETY (fix-G review ruling): the shared dispatch guard, letting through ONLY the
// co-author loop's writes (LIVE_DRAFTING_WRITES). It cannot create a project: the
// real project it drives is made by the seed step (tests/seed/shared-project.setup.ts).
import { test, expect, LIVE_DRAFTING_WRITES } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessLiveDrafting } from './support/gating.js';
import {
  openSharedProject,
  enterDesignExperience,
  commitArtifactsThrough,
} from './support/flows.js';
import { tagUseCase } from './support/useCases.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

// The ordered Phase-1 kinds the spine commits through (mirrors testids.PHASE1_ARTIFACTS).
// `system` (Architecture) is now the last Phase-1 step — scrubbedRequirements and
// operationalConcepts left the drafting sequence with their pages.
const ORDERED_KINDS = ['mission', 'glossary', 'volatilities', 'coreUseCases', 'system'] as const;

// xyflow renders edge labels and node titles as DOM text inside the canvas.
const EDGE_TEXT = '.react-flow__edge-text';
const NODE = '.react-flow__node';

test.describe('architecture & deployment views (live backend — UITESTS_LIVE_DRAFTING=1)', () => {
  // ONE test drives the full Phase-1 spine ONCE (committing every artifact up to
  // `system`) and then asserts the lens families against that single committed
  // project. Driving the chain once — rather than re-drafting the whole spine per
  // assertion — is the only affordable shape for a live model: it pays the (slow,
  // several-minute-per-heavy-artifact) convergence cost a single time. The budget
  // must cover five sequential step gates plus the assertions.
  test.describe.configure({ timeout: 12_000_000 });
  test.use({ dispatchGuardAllows: LIVE_DRAFTING_WRITES });

  test.beforeEach(async ({ request }) => {
    skipUnlessLiveDrafting();
    await requireServer(request, BASE);
    // commitArtifactsThrough below drives the same dispatch → observe → gate →
    // approve loop as design-experience.spec's drafting block, across the whole
    // Phase-1 spine — the Method core use case "Drive System Design".
    tagUseCase('drive-system-design');
  });

  test('dynamic and component-focus views render on a committed project', async ({ page }) => {
    await openSharedProject(page);
    await enterDesignExperience(page);
    // Drive the whole spine through `system` (the furthest — and now last — Phase-1
    // artifact), committing it with its dynamicViews. Leaves the spine on `system`.
    await commitArtifactsThrough(page, 'system', ORDERED_KINDS);

    // ── Architecture views (system step) ─────────────────────────────────────
    // Re-select the committed System step to render its ArchitectureView.
    await page.getByTestId(TESTID.spineStep('system')).click();
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 30_000 });

    // Dynamic lens: pick the first use case → participants + numbered call edges.
    await page.getByTestId(TESTID.archViewSwitch).getByRole('button', { name: /dynamic/i }).click();
    const dynamicPicker = page.getByTestId(TESTID.archDynamicPicker);
    await expect(dynamicPicker).toBeVisible();
    await dynamicPicker.click();
    // F-QA2-51: every picker option must carry non-empty visible text — a dynamic
    // view with a blank title falls back to its use case's name, then to a
    // positional "Untitled view N" (adapters.dynamicViewLabel), never blank.
    const dynamicOptions = page.getByRole('option');
    await expect(dynamicOptions.first()).toBeVisible();
    const optionTexts = await dynamicOptions.allInnerTexts();
    expect(optionTexts.length).toBeGreaterThanOrEqual(1);
    for (const text of optionTexts) {
      expect(text.trim()).not.toBe('');
    }
    await dynamicOptions.first().click();
    // Structural assertion: xyflow participant nodes carry no data-testid/role of
    // their own; selecting by the generated `.react-flow__node` DOM class is the
    // only way to confirm the dynamic lens actually rendered participants.
    // eslint-disable-next-line no-restricted-syntax -- see comment above
    await expect(page.locator(NODE).first()).toBeVisible();
    // At least one ordered call edge carries a numeric "1." sequence prefix.
    // Structural assertion: xyflow renders edge labels as plain DOM text with no
    // testid/role; selecting by the generated `.react-flow__edge-text` class is
    // the only way to read the rendered call-sequence prefix.
    await expect(
      // eslint-disable-next-line no-restricted-syntax -- see comment above
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
    // Structural assertion: xyflow perspective nodes carry no data-testid/role of
    // their own; see the note above.
    // eslint-disable-next-line no-restricted-syntax -- see comment above
    const nodes = page.locator(NODE);
    await expect(nodes.first()).toBeVisible();
    await expect.poll(() => nodes.count(), { timeout: 15_000 }).toBeGreaterThanOrEqual(2);
  });
});
