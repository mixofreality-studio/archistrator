/**
 * glossary.spec — the glossary reference widget (GlossaryView) rendered for a
 * COMMITTED glossary artifact: search-as-you-type + Four-Questions category
 * chips + the grouped term list.
 *
 * Behaviors under test (the 2026-07 glossary-widget pass):
 *
 *   1. The chip bar renders "All · N" plus one chip per Four-Questions BASE with
 *      its count, and `aria-pressed` tracks the active chip (toggle semantics).
 *   2. Refined "How · Activity"-style sub-labels ROLL UP under the single "How"
 *      chip (chipCategories aggregates by categoryBase), while the section
 *      headers keep the refined labels.
 *   3. Typing in the search filters terms, and the visually-hidden polite live
 *      region announces the match count ("N terms match").
 *   4. A no-match query renders the filtered-empty state.
 *   5. Every term row is item-granular commentable — the CommentableList per-row
 *      "Comment on this item" anchor button is present, keyed by
 *      `${term}-${originalIndex}` so duplicate terms never collide.
 *
 * Route-intercepted (see support/designStubs.stubCommittedGlossary): the spec
 * stubs the wire and drives the REAL SPA, so it runs hermetically WITHOUT a live
 * drafting stack — the design-experience-regressions tactic. No infra gating
 * needed: only the SPA process is required.
 */
import { test, expect, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { stubCommittedGlossary, type StubGlossaryItem } from './support/designStubs.js';

/**
 * Six terms across every chip bucket. Original-array INDEX is the comment-anchor
 * identity ($.items[n]) and the CommentableList row key suffix. The two How-*
 * items carry DIFFERENT refined sub-labels so the roll-up (one "How" chip, two
 * refined sections) is actually exercised. 'Draft Task' (term) and 'Design
 * Venue' (definition contains "drafting") are the deliberate 2-term match set
 * for the query "draft".
 */
const ITEMS: StubGlossaryItem[] = [
  { term: 'Architect', definition: 'The single design authority for the system.', category: 'Who' },
  {
    term: 'Volatility',
    definition: 'An area of likely change a component encapsulates.',
    category: 'What',
  },
  {
    term: 'Draft Task',
    definition: 'One artifact-producing unit of work dispatched to a worker.',
    category: 'How-activity',
  },
  {
    term: 'Project Repository',
    definition: 'The git substrate holding the typed project state.',
    category: 'How-resource-access',
  },
  { term: 'Design Venue', definition: 'Where drafting jobs execute.', category: 'Where' },
  { term: 'Fizzbin', definition: 'A stray note nobody ever bucketed.', category: '' },
];

/** Stub the committed glossary, open the design experience, select its step. */
async function openCommittedGlossary(page: Page): Promise<void> {
  const projectId = await stubCommittedGlossary(page, ITEMS);
  await page.goto(`/project/${projectId}/design/system`);
  await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();

  // The committed glossary slot is not the first-open step — select it.
  await page.getByTestId(TESTID.spineStep('glossary')).click();
  await expect(page.getByTestId(TESTID.glossaryRoot)).toBeVisible();
}

test.describe('glossary reference widget (stubbed committed artifact — hermetic)', () => {
  test('category chips render base counts and aria-pressed tracks the active chip', async ({
    page,
  }) => {
    await openCommittedGlossary(page);

    // "All · N" plus one chip per BASE bucket, each with its count.
    const all = page.getByTestId(TESTID.glossaryChipAll);
    await expect(all).toContainText('All · 6');
    await expect(page.getByTestId(TESTID.glossaryChip('Who'))).toContainText('Who · 1');
    await expect(page.getByTestId(TESTID.glossaryChip('What'))).toContainText('What · 1');
    await expect(page.getByTestId(TESTID.glossaryChip('Where'))).toContainText('Where · 1');
    await expect(page.getByTestId(TESTID.glossaryChip('Uncategorized'))).toContainText(
      'Uncategorized · 1',
    );

    // Toggle semantics: All is pressed initially; activating Who flips both.
    await expect(all).toHaveAttribute('aria-pressed', 'true');
    const who = page.getByTestId(TESTID.glossaryChip('Who'));
    await expect(who).toHaveAttribute('aria-pressed', 'false');
    await who.click();
    await expect(who).toHaveAttribute('aria-pressed', 'true');
    await expect(all).toHaveAttribute('aria-pressed', 'false');
  });

  test('refined How sub-labels roll up under the single How chip; sections keep them', async ({
    page,
  }) => {
    await openCommittedGlossary(page);

    // ONE "How" chip counting BOTH refined items — no per-sub-label chips.
    const how = page.getByTestId(TESTID.glossaryChip('How'));
    await expect(how).toContainText('How · 2');
    await expect(page.getByTestId(TESTID.glossaryChip('How · Activity'))).toHaveCount(0);

    // Filtering by the How chip shows both refined SECTION headers.
    await how.click();
    await expect(page.getByTestId(TESTID.glossarySection('How · Activity'))).toBeVisible();
    await expect(page.getByTestId(TESTID.glossarySection('How · Resource Access'))).toBeVisible();
    await expect(page.getByTestId(TESTID.commentListItem('Draft Task-2'))).toBeVisible();
    await expect(page.getByTestId(TESTID.commentListItem('Project Repository-3'))).toBeVisible();
    // The non-How terms are filtered out.
    await expect(page.getByTestId(TESTID.commentListItem('Architect-0'))).toHaveCount(0);
  });

  test('search filters terms with a live-region match announcement; no match shows the empty state', async ({
    page,
  }) => {
    await openCommittedGlossary(page);

    // The polite announcer (role=status) reflects the unfiltered count…
    const status = page.getByTestId(TESTID.glossaryRoot).getByRole('status');
    await expect(status).toHaveText('6 terms match');

    // …then the filtered count: "draft" matches Draft Task (term) and Design
    // Venue (definition "drafting"), and only those rows remain.
    await page.getByTestId(TESTID.glossarySearch).fill('draft');
    await expect(status).toHaveText('2 terms match');
    await expect(page.getByTestId(TESTID.commentListItem('Draft Task-2'))).toBeVisible();
    await expect(page.getByTestId(TESTID.commentListItem('Design Venue-4'))).toBeVisible();
    await expect(page.getByTestId(TESTID.commentListItem('Architect-0'))).toHaveCount(0);

    // A no-match query renders the filtered-empty state and announces it.
    await page.getByTestId(TESTID.glossarySearch).fill('zz-no-such-term');
    await expect(page.getByTestId(TESTID.glossaryEmpty)).toBeVisible();
    await expect(status).toHaveText('No terms match');
  });

  test('every term row carries its per-term comment anchor button', async ({ page }) => {
    await openCommittedGlossary(page);

    // CommentableList keys rows (and their comment buttons) `${term}-${index}`
    // by ORIGINAL model index — the $.items[n] anchor identity.
    for (const [index, item] of ITEMS.entries()) {
      await expect(
        page.getByTestId(TESTID.commentListItemButton(`${item.term}-${String(index)}`)),
      ).toBeVisible();
    }
  });
});
