/**
 * construction-search-provenance-guarantee.spec — the PERMANENT regression pin
 * for Stage B Task 11's most load-bearing property.
 *
 * THE GUARANTEE THIS SPEC EXISTS TO DEFEND
 * -----------------------------------------
 * On 2026-09-09 the founder ruled "assume any component that is fully
 * implemented is done and reviewed and integrated". A backfill wrote 218 task
 * attempts onto 25 activities from that ruling — 21 of which now render
 * `100% ✓ PASSED` with every lifecycle phase complete, and six of their ten
 * tasks per activity have NO artifact at all: their only evidence is the
 * ruling itself, recorded as `provenance.basis`. The list's answer to "would a
 * reader mistake one of those rows for observed fact?" is the `≈ RECONSTRUCTED`
 * badge — but that badge rides GROUP headers (tier 1/2) only, never a task
 * row, which is an ARGUMENT FROM CONTEXT: a task row in isolation still reads
 * `SRS Review ✓ PASSED` with no mark of its own, and the design's defence is
 * that its group header is always visible above it.
 *
 * Search is the one feature that can break that argument: it can reveal AND
 * focus a tier-3 row on its OWN initiative, independent of whatever the
 * operator had scrolled to. `needsInlineProvenanceMark` (activityScope.ts) is
 * the structural fix — it is pure and already carries four unit tests — but
 * NOTHING pinned that `TaskRow` actually RENDERS what it returns. A regression
 * that deleted the JSX block calling it would pass tsc, eslint, and all 588
 * node:test cases. This spec is that missing pin, and it can only live here:
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
// derived, so it renders even for a classified-no-evidence row) — giving both
// a reconstructed population (the 21 founder-ruling rows) and a non-
// reconstructed one (classified rows with no backfilled evidence) to search
// for and CONTRAST in one query.
const TASK_SUFFIX = '::requirements::srsReview';

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
  // vacuous — this is the corpus check the earlier live drive already made:
  // "srs review" matches BOTH the 21 founder-ruling-backfilled activities and
  // a real population of classified-but-unattempted ones.
  expect(reconstructed.length).toBeGreaterThan(0);
  expect(nonReconstructed.length).toBeGreaterThan(0);

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
  expect(reconstructed.length).toBeGreaterThan(0);
  // The LAST DOM match — furthest down the revealed list — maximizes the
  // amount of content (including its own ancestors) available to scroll past,
  // so the "past its own group header" claim is exercised for real rather
  // than by an accidental few pixels.
  const targetNodeId = reconstructed[reconstructed.length - 1];
  if (targetNodeId === undefined) throw new Error('unreachable — length checked above');
  const activityId = targetNodeId.split('::')[0];
  if (activityId === undefined) throw new Error('unreachable — nodeId always has an activity prefix');

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
