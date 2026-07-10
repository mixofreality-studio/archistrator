/**
 * artifact-affordances.spec — coverage added for the 2026-07-05/06 Design
 * Experience rework (diagram keyboard a11y + explicit commenting, the
 * intro-banner → info-chip declutter, and the Core-Use-Cases picker's
 * Core/Variations grouping — see UX-P0-2 / UX-P1-4 / A6 in the project's UX +
 * architect review passes).
 *
 * Three behaviors, asserted against ONE driven-once coreUseCases draft/commit
 * (the SAME cost-conscious shape architecture-views.spec uses, for the same
 * reason: this harness has no seed/import API, so reaching a real drafted +
 * committed coreUseCases artifact costs a full live co-author run through
 * mission → glossary → scrubbedRequirements → volatilities → coreUseCases):
 *
 *   1. While the coreUseCases draft is awaiting review, its activity diagram's
 *      steps are keyboard-focusable and Enter/'c' arms a comment on the
 *      focused step (no mouse needed) — the CommentContext ARMED_ANCHOR probe
 *      is the black-box way to observe arming.
 *   2. Once coreUseCases is COMMITTED, its first paint carries NO full-width
 *      intro banner (`artifact-intro`) — only the compact (?) info button
 *      (`artifact-info`) — the banner-stacking declutter.
 *   3. The committed artifact's use-case picker groups entries under "Core"
 *      and (when present) "Variations" ListSubheaders, rather than a flat
 *      list — the A6 fix.
 *
 * LIVE-only (UITESTS_LIVE_DRAFTING=1) for the same reason design-experience.spec's
 * drafting block and architecture-views.spec are: a real drafted/committed
 * artifact is reachable ONLY via the real co-author workflow over the wire.
 */
import { test, expect } from '@playwright/test';
import { TESTID, PHASE1_ARTIFACTS } from './support/testids.js';
import { skipUnlessServer, skipUnlessLiveDrafting } from './support/gating.js';
import { createProjectFromLanding, enterDesignExperience, commitArtifactsThrough } from './support/flows.js';
import { tagUseCase } from './support/useCases.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

// Generous ceiling matching architecture-views.spec: this test drives FIVE
// sequential Phase-1 steps (mission..coreUseCases) on a local model before its
// own assertions even start. A hosted/capable worker never approaches it.
const GATE_TIMEOUT = 1_800_000;

test.describe('coreUseCases diagram + picker affordances (live backend — UITESTS_LIVE_DRAFTING=1)', () => {
  test.describe.configure({ timeout: 10_000_000 });

  test.beforeEach(async ({ request }) => {
    skipUnlessLiveDrafting();
    await skipUnlessServer(request, BASE);
    tagUseCase('drive-system-design');
  });

  test('keyboard comment-arming on the review diagram; committed paint drops the banner for a chip; the picker groups Core/Variations', async ({
    page,
  }) => {
    await createProjectFromLanding(page);
    await enterDesignExperience(page);

    // Drive mission → volatilities to real committed state (commitArtifactsThrough
    // leaves the spine re-selected on `volatilities`, its committed artifact
    // rendered); approving volatilities auto-advances AND auto-starts the
    // coreUseCases draft.
    await commitArtifactsThrough(page, 'volatilities', PHASE1_ARTIFACTS);

    // Re-select coreUseCases (defensively — the auto-advance should already have
    // landed here) and let it settle into its actionable state, mirroring
    // commitArtifactsThrough's own step logic.
    await page.getByTestId(TESTID.spineStep('coreUseCases')).click();
    const cta = page.getByTestId(TESTID.requestDraft);
    const generating = page.getByTestId(TESTID.generatingScene);
    const gate = page.getByTestId(TESTID.gatePanel);
    await Promise.race([
      cta.waitFor({ state: 'visible' }).catch(() => undefined),
      generating.waitFor({ state: 'visible' }).catch(() => undefined),
      gate.waitFor({ state: 'visible' }).catch(() => undefined),
    ]);
    if (await cta.isVisible().catch(() => false)) {
      await cta.click();
    }
    await expect(gate).toBeVisible({ timeout: GATE_TIMEOUT });
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible();

    // ── 1. Keyboard comment-arming on the in-review activity diagram ─────────
    // ActivityNode renders a focusable, labeled `role=button` wrapper per step
    // (aria-label ends "Press C to comment." when commenting is enabled); no
    // mouse selection or NodeToolbar click needed.
    const step = page.getByRole('button', { name: /press c to comment/i }).first();
    await expect(step).toBeVisible({ timeout: 15_000 });
    await step.focus();
    await page.keyboard.press('c');
    const armedAnchor = page.getByTestId(TESTID.commentArmedAnchor);
    await expect(armedAnchor).toHaveAttribute('data-anchor-label', /.+/);

    // Commit it.
    await page.getByTestId(TESTID.gateApprove).click();
    await expect(gate).toHaveCount(0, { timeout: 30_000 });

    // Approve auto-advanced to `system` — re-select coreUseCases to render its
    // now-COMMITTED artifact.
    await page.getByTestId(TESTID.spineStep('coreUseCases')).click();
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 30_000 });

    // ── 2. Committed first paint: chip, not banner ────────────────────────────
    await expect(page.getByTestId(TESTID.artifactIntro)).toHaveCount(0);
    await expect(page.getByTestId(TESTID.artifactInfo)).toBeVisible();

    // ── 3. The use-case picker groups Core / Variations ───────────────────────
    const picker = page.getByTestId(TESTID.useCasePicker);
    await expect(picker).toBeVisible();
    await picker.click();
    const listbox = page.getByRole('listbox');
    await expect(listbox).toContainText(/Core\s*·/);
    // A drafted corpus may legitimately carry zero variations (Method minimum is
    // "2-6 core use cases", no variation floor) — only assert the Variations
    // section when the model actually produced one, tolerating a thinner draft
    // exactly as architecture-views.spec does for its own assertions.
    const hasVariations = (await listbox.getByText(/Variations\s*·/).count()) > 0;
    if (hasVariations) {
      await expect(listbox).toContainText(/variation of /);
    }
    await page.keyboard.press('Escape');
  });
});
