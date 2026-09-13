/**
 * construction-rail-tooltip.spec — the provenance rail's tooltip is ONE fixed hint
 * (fix I), never the basis text behind it.
 *
 * The rail is a 4px hatched column nested inside a row (and, on the graph, inside
 * a lane on a card), and its tooltip used to carry the full basis prose — the
 * founder's ruling that stands in for the evidence, hundreds of characters of it.
 * It now says what the ribbon's milestone chip says, and where the basis is:
 * "Reconstructed (backfilled): written from a basis, not observed. Select it for its
 * basis." The basis itself is read in the detail pane's provenance note.
 *
 * Gated like the other construction specs: needs the seeded "archistrator"
 * construction-phase project (its reconstructed rows) behind the SPA proxy.
 * SAFETY: the shared dispatch guard; nothing here writes.
 */
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

const HINT = /^Reconstructed \((backfilled|inferred)\): written from a basis, not observed\. Select it for its basis\.$/;

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

for (const lens of ['list', 'graph'] as const) {
  test(`${lens}: a reconstructed rail's tooltip is the fixed hint, never a wall of basis text`, async ({
    page,
    dispatchGuard,
  }) => {
    await page.setViewportSize({ width: 1366, height: 768 });
    await gotoApp(page, `/project/archistrator/construction?lens=${lens}`);
    const rail = page.getByTestId(TESTID.constructionProvenanceRail).first();
    await expect(rail).toBeVisible({ timeout: 15_000 });
    await expect(rail).toHaveAttribute('data-provenance', /^(backfilled|synthesized)$/);
    await rail.hover();
    // The rail's own tooltip. (On the graph the card's hover card is a second
    // role="tooltip", so the rail's is picked out by its words.)
    const tip = page.getByRole('tooltip').filter({ hasText: 'Select it for its basis.' });
    await expect(tip).toHaveCount(1);
    const text = ((await tip.textContent()) ?? '').trim();
    expect(text).toMatch(HINT);
    expect(text).not.toMatch(/Basis:/);
    expect(dispatchGuard.blocked).toEqual([]);
  });
}
