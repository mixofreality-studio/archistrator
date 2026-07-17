/**
 * Pure chip-aggregation + filtering logic for the glossary widget, extracted
 * from GlossaryView so it is unit-testable without a render harness
 * (glossaryLogic.test.ts — the sendBackLogic/headerChipStage pattern).
 *
 * Two identities matter here and must not be conflated:
 *
 *   • The CHIP BAR aggregates by the Four-Questions BASE (categoryBase), so
 *     refined "How · Activity" / "How · Resource Access" sub-labels roll up
 *     under one "How" chip whose click filters across every How-* item.
 *   • The SECTION HEADERS keep the refined normalizeCategory() labels, so the
 *     Method's How sub-kind distinction stays visible in the grouped list.
 *
 * Items are carried as {item, index} pairs where `index` is the position in
 * the ORIGINAL model items array — the comment-anchor identity ($.items[n]).
 * Keying anchors by index (not by term text) keeps duplicate term names from
 * silently colliding onto one anchor.
 */
import type { GlossaryItem } from '../contracts/types';

// The Four Questions, in canonical order; anything else sinks to the end.
export const CATEGORY_ORDER = ['Who', 'What', 'How', 'Where', 'Uncategorized'];

/**
 * Normalize a raw category tag to its display label. The "How" question is refined
 * by The Method into distinct sub-kinds (How-activity vs How-resource-access); we
 * KEEP that distinction as "How · Activity" / "How · Resource Access" rather than
 * collapsing every How-* onto one bucket, while still tolerating a plain "How".
 * Blank/absent sinks to "Uncategorized".
 */
export function normalizeCategory(c: string | undefined): string {
  if (c === undefined || c.trim() === '') return 'Uncategorized';
  const trimmed = c.trim();
  if (trimmed === 'How') return 'How';
  // How-activity / How resource access / How-resource-access → "How · <Sub>".
  const howMatch = /^How[\s-]+(.+)$/i.exec(trimmed);
  if (howMatch?.[1] !== undefined) {
    const sub = howMatch[1]
      .split(/[\s-]+/)
      .map((w) => (w.length > 0 ? w.charAt(0).toUpperCase() + w.slice(1) : w))
      .join(' ');
    return `How · ${sub}`;
  }
  return trimmed;
}

/** The Four-Questions bucket a (possibly refined) label ranks under. */
export function categoryBase(c: string): string {
  return c.startsWith('How') ? 'How' : c;
}

export function categoryRank(c: string): number {
  const i = CATEGORY_ORDER.indexOf(categoryBase(c));
  // Refined How sub-labels share the "How" rank but sort after the plain bucket.
  return i === -1 ? CATEGORY_ORDER.length : i;
}

/** A glossary item paired with its index in the ORIGINAL model items array. */
export interface IndexedGlossaryItem {
  item: GlossaryItem;
  /** Position in the original array — the `$.items[n]` comment-anchor identity. */
  index: number;
}

/** Pair every item with its original-array index (the anchor identity). */
export function indexGlossaryItems(items: readonly GlossaryItem[]): IndexedGlossaryItem[] {
  return items.map((item, index) => ({ item, index }));
}

/**
 * The chip bar's categories: [base label, count] aggregated by categoryBase so
 * refined How sub-labels roll up under one "How" chip, in Four-Questions order.
 */
export function chipCategories(entries: readonly IndexedGlossaryItem[]): [string, number][] {
  const counts = new Map<string, number>();
  for (const e of entries) {
    const base = categoryBase(normalizeCategory(e.item.category));
    counts.set(base, (counts.get(base) ?? 0) + 1);
  }
  return [...counts.entries()].sort(
    (a, b) => categoryRank(a[0]) - categoryRank(b[0]) || a[0].localeCompare(b[0])
  );
}

export interface FilteredGlossary {
  total: number;
  /** [refined display label, entries alphabetized by term] in category order. */
  sections: [string, IndexedGlossaryItem[]][];
}

/**
 * Filter by active BASE chip (null = All) + search query (term or definition,
 * case-insensitive), then group under the refined section labels.
 */
export function filterGlossary(
  entries: readonly IndexedGlossaryItem[],
  query: string,
  activeBase: string | null
): FilteredGlossary {
  const q = query.trim().toLowerCase();
  const matches = entries.filter((e) => {
    const label = normalizeCategory(e.item.category);
    if (activeBase !== null && categoryBase(label) !== activeBase) return false;
    if (q === '') return true;
    return e.item.term.toLowerCase().includes(q) || e.item.definition.toLowerCase().includes(q);
  });
  const g = new Map<string, IndexedGlossaryItem[]>();
  for (const e of matches) {
    const label = normalizeCategory(e.item.category);
    const arr = g.get(label) ?? [];
    arr.push(e);
    g.set(label, arr);
  }
  for (const arr of g.values()) arr.sort((a, b) => a.item.term.localeCompare(b.item.term));
  return {
    total: matches.length,
    sections: [...g.entries()].sort(
      (a, b) => categoryRank(a[0]) - categoryRank(b[0]) || a[0].localeCompare(b[0])
    ),
  };
}

/** The polite live-region message announcing the current match count. */
export function matchAnnouncement(total: number): string {
  if (total === 0) return 'No terms match';
  return total === 1 ? '1 term matches' : `${String(total)} terms match`;
}
