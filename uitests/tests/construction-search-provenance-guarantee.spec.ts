/**
 * construction-search-provenance-guarantee.spec — the PERMANENT regression pin
 * for Stage B Task 11's most load-bearing property.
 *
 * THE GUARANTEE THIS SPEC EXISTS TO DEFEND
 * -----------------------------------------
 * The construction backfill (server/cmd/backfill-attempts) re-derived 214 task
 * attempts onto 23 of the 29 derived activities from code evidence plus a
 * recorded founder sign-off. Every one of those 23 now renders `100% ✓ PASSED`
 * with every lifecycle phase complete — and none of it was observed as it
 * happened: each attempt is stamped `backfilled`, its evidence reconstructed
 * after the fact and recorded as `provenance.basis`. The list's answer to
 * "would a reader mistake one of those rows for observed fact?" is the
 * `≈ RECONSTRUCTED` badge — but that badge rides GROUP headers (tier 1/2) only, never a task
 * row, which is an ARGUMENT FROM CONTEXT: a task row in isolation still reads
 * `SRS Review ✓ PASSED` with no mark of its own, and the design's defence is
 * that its group header is always visible above it.
 *
 * Search is the one feature that can break that argument: it can reveal AND
 * focus a tier-3 row on its OWN initiative, independent of whatever the
 * operator had scrolled to. `needsInlineProvenanceMark` (activityScope.ts) is
 * the structural fix — it is pure and already carries four unit tests — but
 * NOTHING pinned that `TaskRow` actually RENDERS what it returns. A regression
 * that deleted the JSX block calling it would pass tsc, eslint, and the whole
 * node:test suite. This spec is that missing pin, and it can only live here:
 * node's test runner cannot load a `.tsx` module at all.
 *
 * THE INDEPENDENT ORACLE
 * -----------------------
 * Classifying a matched row as "actually reconstructed" is done WITHOUT
 * touching the mechanism under test: `ProvenanceRailMark` (provenance.tsx,
 * Task 7 — a different component entirely) only ever emits the
 * `construction-provenance-rail` testid when the node's grade is
 * `reconstructed` (see provenanceRailFor — `recorded` and `unknown` both
 * return `widthPx: 0` and render no rail element at all). So "does this row's
 * rail testid exist" is ground truth from a component this spec's search
 * mechanism never touches — exactly the independence a meaningful regression
 * test needs; classifying by the mark's OWN presence would make assertion 1
 * circular.
 *
 * Gated like construction-tracker.spec.ts: needs the seeded "archistrator"
 * construction-phase project (real committed backfilled state) behind the SPA
 * proxy.
 */
import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

// The Figure A-1 task this spec searches for: every 'service'-kind classified
// activity carries a "SRS Review" task in its Requirements phase (profile-
// derived, so it renders even for a row with no record at all) — giving both
// a reconstructed population (the backfilled rows) and a non-reconstructed one
// (the planned-no-record rows: classified, no ledger) to search for and
// CONTRAST in one query.
const TASK_SUFFIX = '::requirements::srsReview';

// Pinned, not only discovered. Discovery alone still passes if the list quietly
// stops rendering most of the reconstructed rows, so the guarantee is also held
// against named rows that are reconstructed NOW: three live backfilled service
// activities, one per layer the backfill reached (Manager, Engine,
// ResourceAccess). The contrast pins are the three planned-no-record service
// activities — their code does not qualify as evidence, so they carry no
// ledger and must never be marked. Re-pin deliberately when any of these
// changes state (e.g. once C-billing-state-access is actually built).
//
// RE-PIN NOTICE — to whoever builds one of the PINNED_NOT_RECONSTRUCTED
// activities: once it has a ledger it is no longer a contrast pin, and this
// spec will fail on it (its header will then carry the badge, which is correct).
// Move it out of PINNED_NOT_RECONSTRUCTED and replace it with another
// planned-no-record activity of the same kind — do not delete the assertion or
// weaken it to "at least one".
const PINNED_RECONSTRUCTED = [
  'C-construction-manager',
  'C-review-engine',
  'C-project-state-access',
] as const;
const PINNED_NOT_RECONSTRUCTED = [
  'C-billing-state-access',
  'C-design-health-engine',
  'C-merchant-gateway-access',
] as const;

interface Discovery {
  /** Task nodeIds, in DOM (top-to-bottom) order, whose OWN provenance rail
   *  proves grade==='reconstructed' — independent of the mark under test. */
  reconstructed: string[];
  /** Task nodeIds matched by the search whose rail proves they are NOT
   *  reconstructed (no rail element at all — recorded or unknown grade). */
  nonReconstructed: string[];
}

/**
 * Reads the CURRENT DOM (post-search) and classifies every rendered SRS-Review
 * task row. A row for this task exists in the DOM at all ONLY when its
 * activity passed the search filter (Collapse's `unmountOnExit` keeps a
 * collapsed activity's descendants OUT of the DOM entirely — see
 * ActivityTreeView.tsx's own doc comment) — so every row this finds is, by
 * construction, a genuine search match; no separate "is this a match" signal
 * is needed.
 *
 * `document.querySelectorAll` inside `page.evaluate` (not Playwright's banned
 * `.locator()` escape hatch — see eslint.config.js) mirrors the exact pattern
 * construction-tracker.spec.ts's own `scrollTo` helper already uses for a
 * structural read with no clean testid/role path of its own: the SET of real
 * activity ids behind this assertion is exactly what varies with live data,
 * so it is discovered rather than hardcoded.
 */
async function discoverMatches(page: import('@playwright/test').Page): Promise<Discovery> {
  return page.evaluate((suffix) => {
    const reconstructed: string[] = [];
    const nonReconstructed: string[] = [];
    const rows = Array.from(document.querySelectorAll('[data-testid^="construction-list-row-"]'));
    for (const row of rows) {
      const testid = row.getAttribute('data-testid') ?? '';
      if (!testid.endsWith(suffix)) continue;
      const nodeId = testid.slice('construction-list-row-'.length);
      const hasRail = row.querySelector('[data-testid="construction-provenance-rail"]') !== null;
      (hasRail ? reconstructed : nonReconstructed).push(nodeId);
    }
    return { reconstructed, nonReconstructed };
  }, TASK_SUFFIX);
}

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

test('a search-matched reconstructed task row carries its own provenance mark; a matched non-reconstructed row does not', async ({
  page,
}) => {
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });

  await page
    .getByTestId(TESTID.constructionLensSearch)
    .getByRole('textbox')
    .fill('srs review');
  // The reveal is a render-time state adjustment (not a debounce) plus one
  // rAF-deferred focus; a short settle covers both comfortably.
  await page.waitForTimeout(600);

  const { reconstructed, nonReconstructed } = await discoverMatches(page);

  // Neither population may be empty, or the two assertions below would be
  // vacuous: "srs review" matches BOTH the backfilled service activities and
  // the planned-no-record ones.
  expect(reconstructed.length).toBeGreaterThan(0);
  expect(nonReconstructed.length).toBeGreaterThan(0);
  // …and the pinned rows land in the population the oracle (their own rail)
  // says they belong to, so the loops below demonstrably cover them.
  for (const id of PINNED_RECONSTRUCTED) {
    expect(reconstructed).toContain(`${id}${TASK_SUFFIX}`);
  }
  for (const id of PINNED_NOT_RECONSTRUCTED) {
    expect(nonReconstructed).toContain(`${id}${TASK_SUFFIX}`);
  }

  // The GROUP badge (review I1). The spelled-out `≈ RECONSTRUCTED` stamp on
  // tier-1 and tier-2 headers is the design's PRIMARY defence — the inline mark
  // below only covers the one case search can break — and nothing pinned it:
  // ProvenanceGroupStamp could render null and this whole spec stayed green. So
  // each pinned reconstructed activity's own header, and its revealed
  // Requirements phase header, must carry it; each pinned planned-no-record
  // activity's must not. Each header is proven RENDERED first, so an absence can
  // never pass because the row itself was missing.
  for (const id of PINNED_RECONSTRUCTED) {
    for (const header of [id, `${id}::requirements`]) {
      const row = page.getByTestId(TESTID.constructionListRow(header));
      await expect(row).toBeVisible();
      const badge = row.getByTestId(TESTID.constructionProvenanceBadge);
      await expect(badge).toBeVisible();
      // Spelled out, on tier 1 AND tier 2 (fix-B review M2): a badge abbreviated to
      // a bare "≈" is still visible, and would pass a visibility check alone. The
      // word is authored lower-case and set in capitals by CSS, so the DOM text is
      // pinned case-insensitively and what the reader SEES (innerText, which
      // applies text-transform) is pinned in capitals.
      await expect(badge).toHaveText(/^≈\s*reconstructed$/i);
      expect((await badge.innerText()).replace(/\s+/g, ' ').trim()).toMatch(/^≈ ?RECONSTRUCTED$/);
    }
  }
  for (const id of PINNED_NOT_RECONSTRUCTED) {
    for (const header of [id, `${id}::requirements`]) {
      const row = page.getByTestId(TESTID.constructionListRow(header));
      await expect(row).toBeVisible();
      await expect(row.getByTestId(TESTID.constructionProvenanceBadge)).toHaveCount(0);
    }
  }

  // Assertion 1: every row whose OWN rail proves reconstructed carries the
  // inline mark.
  for (const nodeId of reconstructed) {
    await expect(page.getByTestId(TESTID.constructionSearchMatchProvenance(nodeId))).toBeVisible();
  }

  // Assertion 2: every OTHER matched row (rail proves it is NOT reconstructed)
  // carries NO mark at all — this is what fails an implementation that stamps
  // every matched row regardless of grade, rather than only the reconstructed
  // ones.
  for (const nodeId of nonReconstructed) {
    await expect(page.getByTestId(TESTID.constructionSearchMatchProvenance(nodeId))).toHaveCount(0);
  }
});

test('the inline provenance mark survives its own group header scrolling out of view', async ({
  page,
}) => {
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });

  await page
    .getByTestId(TESTID.constructionLensSearch)
    .getByRole('textbox')
    .fill('srs review');
  await page.waitForTimeout(600);

  const { reconstructed } = await discoverMatches(page);
  // The first pinned reconstructed activity. Scrolling its row to the top can
  // only push its header out of view if enough revealed content sits BELOW it
  // to let the container scroll that far — a match near the end of the list
  // cannot (the container bottoms out with the header still on screen).
  // C-construction-manager sorts early in the tree, so the matches after it
  // guarantee that room, and pinning it keeps this test on a row that is
  // reconstructed NOW rather than on whichever row happens to come last.
  const activityId = PINNED_RECONSTRUCTED[0];
  const targetNodeId = `${activityId}${TASK_SUFFIX}`;
  expect(reconstructed).toContain(targetNodeId);

  const targetRow = page.getByTestId(TESTID.constructionListRow(targetNodeId));
  const targetMark = page.getByTestId(TESTID.constructionSearchMatchProvenance(targetNodeId));
  const activityHeader = page.getByTestId(TESTID.constructionListRow(activityId));

  await expect(targetMark).toBeVisible();

  // Read whichever real element scrolls (the console's inner overflow:auto
  // region, not the window — see construction-tracker.spec.ts's identical
  // "the console does NOT scroll the window" note), AND its own clip
  // boundary — a scrolled-out descendant's `getBoundingClientRect` is always
  // window-viewport-relative, not container-relative, and this container sits
  // well below the browser's y=0 (a fixed brand bar + tabs above it), so
  // "out of view" must be measured against the CONTAINER's own top edge, not
  // literal 0.
  const readScrollState = (): Promise<{ scrollTop: number; containerTop: number }> =>
    page.evaluate(() => {
      for (const el of Array.from(document.querySelectorAll('*'))) {
        const style = window.getComputedStyle(el);
        if (el.scrollHeight > el.clientHeight + 4 && /(auto|scroll)/.test(style.overflowY)) {
          return { scrollTop: el.scrollTop, containerTop: el.getBoundingClientRect().top };
        }
      }
      return { scrollTop: -1, containerTop: -1 };
    });

  const before = await readScrollState();

  // Align the MATCHED ROW to the viewport's own top edge. Because the SRS
  // Review task sits near the very top of its activity's expanded subtree
  // (Requirements is Figure A-1's first phase, and SRS Review is its 2nd of
  // 2 tasks), this reliably pushes the activity's own tier-1 header — and the
  // Requirements phase's own tier-2 group header, both of which sit just
  // above the row — fully above the scrollable region's own visible top edge,
  // while the row itself lands right at the boundary (still visible).
  await targetRow.evaluate((el) => {
    el.scrollIntoView({ block: 'start' });
  });
  await page.waitForTimeout(250);

  const after = await readScrollState();
  // The scroll is REAL, not vacuous — construction-tracker.spec.ts's own
  // caution ("prove it actually moved before believing anything measured
  // after it") applies here too.
  expect(after.scrollTop).toBeGreaterThan(before.scrollTop);

  const headerRect = await activityHeader.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { top: r.top, bottom: r.bottom };
  });
  // The group header (which carries the badge the design's CONTEXTUAL
  // argument relies on) is now fully above the scrollable region's own clip
  // boundary — genuinely scrolled past, not just partially clipped, and not
  // merely past the raw browser y=0 (which sits above a fixed chrome bar this
  // console does not scroll).
  expect(headerRect.bottom).toBeLessThanOrEqual(after.containerTop);

  // ...and yet the row's OWN inline mark is still rendered AND within the
  // visible viewport. This is the structural half of the guarantee: it does
  // not depend on the header being reachable.
  await expect(targetMark).toBeVisible();
  const markRect = await targetMark.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { top: r.top, bottom: r.bottom };
  });
  expect(markRect.bottom).toBeGreaterThan(after.containerTop);
});
