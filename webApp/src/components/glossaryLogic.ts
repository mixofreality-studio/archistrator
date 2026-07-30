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
import type {
  ArtifactModelEnvelope,
  CoreUseCases,
  GlossaryItem,
  ScrubbedRequirements,
  System,
  Volatilities,
} from '../contracts/types';

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
 * Whether an item's term or definition contains the (already trimmed +
 * lowercased) query; the empty query matches everything. Shared by the chip
 * bar and the section list so they stay in lock-step on what "matches".
 */
function matchesQuery(item: GlossaryItem, loweredQuery: string): boolean {
  if (loweredQuery === '') return true;
  return (
    item.term.toLowerCase().includes(loweredQuery) ||
    item.definition.toLowerCase().includes(loweredQuery)
  );
}

/**
 * The chip bar's categories: [base label, count] aggregated by categoryBase so
 * refined How sub-labels roll up under one "How" chip, in Four-Questions order.
 *
 * Counts come from a TEXT-only pass that ignores whichever category chip is
 * active, so every chip shows how many of the query's matches fall in it — the
 * counts track the live query and never go stale under an active category.
 */
export function chipCategories(
  entries: readonly IndexedGlossaryItem[],
  query: string
): [string, number][] {
  const q = query.trim().toLowerCase();
  const counts = new Map<string, number>();
  for (const e of entries) {
    if (!matchesQuery(e.item, q)) continue;
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
    return matchesQuery(e.item, q);
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

// ── Cross-artifact term-usage joins (glossary usability pass) ─────────────────
//
// Per term, WHERE is it actually used across the committed downstream artifacts?
// Pure text logic over per-step item texts: the view builds the corpus once from
// the committed slot envelopes (buildUsageCorpus), then termUsage answers with
// per-STEP item counts ("Behaviors ×3", "Architecture ×2") — never a
// per-occurrence listing.

/** The searched steps, in canonical (spine) order. */
export const USAGE_STEP_KINDS = [
  'scrubbedRequirements',
  'volatilities',
  'coreUseCases',
  'system',
] as const;
export type UsageStepKind = (typeof USAGE_STEP_KINDS)[number];

/** Short chip labels per searched step (the chips stay quiet — one word each). */
export const USAGE_STEP_LABELS: Record<UsageStepKind, string> = {
  scrubbedRequirements: 'Behaviors',
  volatilities: 'Volatilities',
  coreUseCases: 'Use Cases',
  system: 'Architecture',
};

/** Per step, one searchable text per ITEM (a term hitting an item's text twice
 *  still counts that item once). */
export type UsageCorpus = Record<UsageStepKind, readonly string[]>;

/** One usage chip: the step plus how many of its items mention the term. */
export interface UsageChip {
  kind: UsageStepKind;
  count: number;
}

/** Narrow an envelope to its model when the kind matches; else undefined. The
 *  caller casts to the kind's typed model (the asDeploymentModel idiom). */
function modelOf(
  envelope: ArtifactModelEnvelope | undefined,
  kind: UsageStepKind
): ArtifactModelEnvelope['model'] {
  if (envelope?.kind !== kind || envelope.model === undefined) return undefined;
  return envelope.model;
}

/**
 * Build the per-step item-text corpus from the committed slot envelopes:
 * Required Behaviors statements, volatility names+rationales, use case names,
 * component names+encapsulates. Absent/mismatched envelopes yield empty steps —
 * the joins degrade to nothing, never crash.
 */
export function buildUsageCorpus(slots: {
  scrubbedRequirements: ArtifactModelEnvelope | undefined;
  volatilities: ArtifactModelEnvelope | undefined;
  coreUseCases: ArtifactModelEnvelope | undefined;
  system: ArtifactModelEnvelope | undefined;
}): UsageCorpus {
  const behaviors = modelOf(slots.scrubbedRequirements, 'scrubbedRequirements') as
    | ScrubbedRequirements
    | undefined;
  const volatilities = modelOf(slots.volatilities, 'volatilities') as Volatilities | undefined;
  const useCases = modelOf(slots.coreUseCases, 'coreUseCases') as CoreUseCases | undefined;
  const system = modelOf(slots.system, 'system') as System | undefined;
  return {
    scrubbedRequirements: (behaviors?.items ?? []).map((i) => i.statement),
    volatilities: (volatilities?.items ?? []).map((v) => `${v.name} ${v.rationale}`),
    coreUseCases: (useCases?.decisions ?? []).map((d) => d.useCase.name),
    system: (system?.components ?? []).map((c) => `${c.name} ${c.encapsulates}`),
  };
}

/**
 * Compile a term into its case-insensitive, whole-word-ish matcher:
 *  • boundaries are non-letter/digit on both sides (so "cat" never hits
 *    "catalog" and "session" never hits "SessionManager"),
 *  • internal whitespace matches any space/hyphen run (so "design session"
 *    hits "design-session"),
 *  • the whole term tolerates a simple trailing plural ("book" hits "books"),
 *  • regex metacharacters in the term are literal.
 * Null for a blank term.
 */
function termMatcher(term: string): RegExp | null {
  const words = term
    .trim()
    .split(/\s+/)
    .filter((w) => w.length > 0);
  if (words.length === 0) return null;
  const escaped = words.map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
  const body = escaped.join('[\\s-]+');
  return new RegExp(`(?<![\\p{L}\\p{N}])(?:${body})(?:e?s)?(?![\\p{L}\\p{N}])`, 'iu');
}

/**
 * Where a glossary term is USED: per searched step (canonical order), the count
 * of that step's items mentioning the term. Zero-count steps are omitted; a
 * blank or unmatched term yields [].
 */
export function termUsage(term: string, corpus: UsageCorpus): UsageChip[] {
  const matcher = termMatcher(term);
  if (matcher === null) return [];
  const chips: UsageChip[] = [];
  for (const kind of USAGE_STEP_KINDS) {
    const count = corpus[kind].filter((text) => matcher.test(text)).length;
    if (count > 0) chips.push({ kind, count });
  }
  return chips;
}
