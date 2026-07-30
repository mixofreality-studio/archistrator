/// <reference types="node" />
/**
 * Unit tests for the glossary widget's pure chip-aggregation + filtering logic
 * (src/components/glossaryLogic.ts). Covers the two identity rules extracted
 * from GlossaryView:
 *
 *   • chip bar aggregates by Four-Questions BASE (refined "How · Activity"
 *     sub-labels roll up under one "How" chip, and the "How" chip filter
 *     matches across all How-* items) while sections keep refined labels;
 *   • comment-anchor identity is the item's ORIGINAL array index, so duplicate
 *     term names never collide onto one anchor.
 *
 * Plus the normalizeCategory refinement rules and the live-region announcement.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ArtifactModelEnvelope, GlossaryItem } from '../contracts/types';
import {
  buildUsageCorpus,
  categoryBase,
  categoryRank,
  chipCategories,
  filterGlossary,
  indexGlossaryItems,
  matchAnnouncement,
  normalizeCategory,
  termUsage,
  USAGE_STEP_LABELS,
} from './glossaryLogic.ts';

function item(term: string, category: string, definition = `${term} definition`): GlossaryItem {
  return { term, category, definition };
}

const CORPUS = indexGlossaryItems([
  item('Manager', 'How-activity'),
  item('Access', 'How resource access'),
  item('Founder', 'Who'),
  item('Project', 'What'),
  item('Cloud', 'Where'),
  item('Mystery', ''),
  item('Plain', 'How'),
]);

void test('normalizeCategory refines How-* sub-kinds and tolerates plain How', () => {
  assert.equal(normalizeCategory('How-activity'), 'How · Activity');
  assert.equal(normalizeCategory('How resource access'), 'How · Resource Access');
  assert.equal(normalizeCategory('How-resource-access'), 'How · Resource Access');
  assert.equal(normalizeCategory('How'), 'How');
  assert.equal(normalizeCategory('Who'), 'Who');
});

void test('normalizeCategory sinks blank/absent to Uncategorized', () => {
  assert.equal(normalizeCategory(undefined), 'Uncategorized');
  assert.equal(normalizeCategory(''), 'Uncategorized');
  assert.equal(normalizeCategory('   '), 'Uncategorized');
});

void test('categoryBase folds every refined How label onto How', () => {
  assert.equal(categoryBase('How · Activity'), 'How');
  assert.equal(categoryBase('How'), 'How');
  assert.equal(categoryBase('Who'), 'Who');
});

void test('categoryRank follows the Four Questions; unknowns sink to the end', () => {
  assert.ok(categoryRank('Who') < categoryRank('What'));
  assert.ok(categoryRank('What') < categoryRank('How · Activity'));
  assert.equal(categoryRank('How · Activity'), categoryRank('How'));
  assert.ok(categoryRank('How') < categoryRank('Where'));
  assert.ok(categoryRank('Uncategorized') < categoryRank('Custom'));
});

void test('chip bar aggregates refined How sub-labels under ONE How chip', () => {
  const chips = chipCategories(CORPUS, '');
  assert.deepEqual(chips, [
    ['Who', 1],
    ['What', 1],
    ['How', 3], // How-activity + How resource access + plain How rolled up
    ['Where', 1],
    ['Uncategorized', 1],
  ]);
});

void test('chip counts track the live query (text-only, ignoring active category)', () => {
  // A query that matches every How-* term by definition text, plus nothing else:
  // counts drop the non-matching chips and thin the How chip to its matches.
  const entries = indexGlossaryItems([
    item('Manager', 'How-activity', 'orchestrates activities'),
    item('Access', 'How resource access', 'orchestrates io'),
    item('Founder', 'Who', 'the person'),
    item('Plain', 'How', 'plain how note'),
  ]);
  // "orchestrates" hits the two How definitions only.
  assert.deepEqual(chipCategories(entries, 'orchestrates'), [['How', 2]]);
  // Term-text query, case-insensitive, narrows to a single chip.
  assert.deepEqual(chipCategories(entries, 'founder'), [['Who', 1]]);
  // A non-matching query yields no chips at all.
  assert.deepEqual(chipCategories(entries, 'zzz'), []);
});

void test('chip counts sum to the text-only total that feeds the All chip', () => {
  // The "How" chip is active in the UI, but chip counts ignore that and reflect
  // only the query — so the per-chip counts still sum to the All-chip total.
  const query = 'definition'; // matches every default definition in CORPUS
  const total = chipCategories(CORPUS, query).reduce((n, [, c]) => n + c, 0);
  assert.equal(total, filterGlossary(CORPUS, query, null).total);
  assert.equal(total, CORPUS.length);
});

void test('the How chip filters across ALL How-* items, sections keep refined labels', () => {
  const g = filterGlossary(CORPUS, '', 'How');
  assert.equal(g.total, 3);
  assert.deepEqual(
    g.sections.map(([label, entries]) => [label, entries.map((e) => e.item.term)]),
    [
      ['How', ['Plain']],
      ['How · Activity', ['Manager']],
      ['How · Resource Access', ['Access']],
    ]
  );
});

void test('null chip (All) passes everything; query matches term or definition', () => {
  assert.equal(filterGlossary(CORPUS, '', null).total, CORPUS.length);
  // Term match, case-insensitive.
  const byTerm = filterGlossary(CORPUS, 'founder', null);
  assert.deepEqual(
    byTerm.sections.flatMap(([, entries]) => entries.map((e) => e.item.term)),
    ['Founder']
  );
  // Definition match.
  const byDef = filterGlossary(CORPUS, 'cloud definition', null);
  assert.deepEqual(
    byDef.sections.flatMap(([, entries]) => entries.map((e) => e.item.term)),
    ['Cloud']
  );
});

void test('chip filter and query compose (base chip + non-matching query = empty)', () => {
  const g = filterGlossary(CORPUS, 'Founder', 'How');
  assert.equal(g.total, 0);
  assert.deepEqual(g.sections, []);
});

void test('entries within a section are alphabetized by term', () => {
  const entries = indexGlossaryItems([item('Zeta', 'What'), item('Alpha', 'What')]);
  const g = filterGlossary(entries, '', null);
  assert.deepEqual(
    g.sections[0]?.[1].map((e) => e.item.term),
    ['Alpha', 'Zeta']
  );
});

void test('duplicate term names keep DISTINCT original-index anchor identities', () => {
  const entries = indexGlossaryItems([
    item('Session', 'What', 'first meaning'),
    item('Founder', 'Who'),
    item('Session', 'How-activity', 'second meaning'),
  ]);
  assert.deepEqual(
    entries.map((e) => e.index),
    [0, 1, 2]
  );
  const g = filterGlossary(entries, 'Session', null);
  assert.equal(g.total, 2);
  const indices = g.sections.flatMap(([, es]) => es.map((e) => e.index)).sort((a, b) => a - b);
  assert.deepEqual(indices, [0, 2]); // not [0, 0] — no term-text collision
});

void test('live-region announcement wording', () => {
  assert.equal(matchAnnouncement(0), 'No terms match');
  assert.equal(matchAnnouncement(1), '1 term matches');
  assert.equal(matchAnnouncement(7), '7 terms match');
});

// ── Cross-artifact term-usage joins (glossary usability pass) ─────────────────

/** Hand-built committed envelopes covering the four searched steps. */
function corpusFixture(): ReturnType<typeof buildUsageCorpus> {
  const scrubbedRequirements: ArtifactModelEnvelope = {
    kind: 'scrubbedRequirements',
    model: {
      items: [
        { id: 'B-01', statement: 'The architect commits each design session to the ledger.' },
        { id: 'B-02', statement: 'A design session may be resumed after a pause.' },
        { id: 'B-03', statement: 'Sessions are never billed twice.' },
        { id: 'B-04', statement: 'The catalog lists projects.' },
      ],
    },
  };
  const volatilities: ArtifactModelEnvelope = {
    kind: 'volatilities',
    model: {
      items: [
        {
          axis: 'sameCustomerOverTime',
          name: 'Design Session lifecycle',
          rationale: 'How a session starts and ends changes over time.',
          traces: null,
        },
        {
          axis: 'allCustomersAtOneTime',
          name: 'Billing policy',
          rationale: 'Pricing differs across customers.',
          traces: null,
        },
      ],
      rejected: null,
    },
  };
  const coreUseCases: ArtifactModelEnvelope = {
    kind: 'coreUseCases',
    model: {
      decisions: [
        {
          rejectionReason: '',
          useCase: {
            id: 'UC-1',
            name: 'Run a design session',
            classification: 'core',
            trigger: 'clientAction',
            actors: null,
            activity: null,
            variationOf: null,
          },
        },
        {
          rejectionReason: '',
          useCase: {
            id: 'UC-2',
            name: 'Close the books',
            classification: 'core',
            trigger: 'timer',
            actors: null,
            activity: null,
            variationOf: null,
          },
        },
      ],
    },
  };
  const system: ArtifactModelEnvelope = {
    kind: 'system',
    model: {
      components: [
        {
          id: 'sessionmanager',
          name: 'SessionManager',
          kind: 'manager',
          layer: 'manager',
          encapsulates: 'Sequencing of one design session.',
          atomicBusinessVerbs: null,
        },
        {
          id: 'billingengine',
          name: 'BillingEngine',
          kind: 'engine',
          layer: 'engine',
          encapsulates: 'Billing computation volatility.',
          atomicBusinessVerbs: null,
        },
      ],
      relationships: null,
      dynamicViews: null,
    } as unknown as NonNullable<ArtifactModelEnvelope['model']>,
  };
  return buildUsageCorpus({ scrubbedRequirements, volatilities, coreUseCases, system });
}

void test('termUsage counts ITEMS per step, in canonical step order', () => {
  const corpus = corpusFixture();
  const chips = termUsage('session', corpus);
  assert.deepEqual(chips, [
    // B-01, B-02 mention "session"; B-03's "Sessions" matches via plural tolerance.
    { kind: 'scrubbedRequirements', count: 3 },
    // name + rationale both searched, but the volatility counts ONCE per item.
    { kind: 'volatilities', count: 1 },
    { kind: 'coreUseCases', count: 1 },
    // SessionManager is one word — "session" must NOT match inside it; the
    // encapsulates prose "design session" does match that component's item text.
    { kind: 'system', count: 1 },
  ]);
});

void test('termUsage is case-insensitive', () => {
  const corpus = corpusFixture();
  assert.deepEqual(termUsage('BILLING', corpus), [
    { kind: 'volatilities', count: 1 },
    { kind: 'system', count: 1 },
  ]);
});

void test('termUsage tolerates a simple plural on the term ("whole-word-ish")', () => {
  const corpus = corpusFixture();
  // "book" matches "books" (Close the books) but nothing else.
  assert.deepEqual(termUsage('book', corpus), [{ kind: 'coreUseCases', count: 1 }]);
});

void test('termUsage matches multi-word terms across space/hyphen variants', () => {
  const corpus = corpusFixture();
  const chips = termUsage('design session', corpus);
  assert.deepEqual(chips, [
    { kind: 'scrubbedRequirements', count: 2 },
    { kind: 'volatilities', count: 1 },
    { kind: 'coreUseCases', count: 1 },
    { kind: 'system', count: 1 },
  ]);
  // Hyphenated occurrence still matches a spaced term.
  const hyphen = buildUsageCorpus({
    scrubbedRequirements: {
      kind: 'scrubbedRequirements',
      model: { items: [{ id: 'B-01', statement: 'Each design-session is durable.' }] },
    },
    volatilities: undefined,
    coreUseCases: undefined,
    system: undefined,
  });
  assert.deepEqual(termUsage('design session', hyphen), [
    { kind: 'scrubbedRequirements', count: 1 },
  ]);
});

void test('termUsage requires whole words (no substring hits)', () => {
  const corpus = corpusFixture();
  // "cat" must not match "catalog" (plural tolerance is a suffix on the WHOLE
  // word, not a licence for substring matching).
  assert.deepEqual(termUsage('cat', corpus), []);
  // "Manager" must not match inside the one-word component name "SessionManager".
  assert.deepEqual(termUsage('Manager', corpus), []);
});

void test('termUsage handles no-match and blank terms safely', () => {
  const corpus = corpusFixture();
  assert.deepEqual(termUsage('zeppelin', corpus), []);
  assert.deepEqual(termUsage('', corpus), []);
  assert.deepEqual(termUsage('   ', corpus), []);
  // Regex metacharacters in a term must be treated literally, not crash.
  assert.deepEqual(termUsage('a+b (c)', corpus), []);
});

void test('termUsage on an empty/absent corpus renders nothing', () => {
  const empty = buildUsageCorpus({
    scrubbedRequirements: undefined,
    volatilities: undefined,
    coreUseCases: undefined,
    system: undefined,
  });
  assert.deepEqual(termUsage('session', empty), []);
});

void test('every searched step carries a short chip label', () => {
  assert.equal(USAGE_STEP_LABELS.scrubbedRequirements, 'Behaviors');
  assert.equal(USAGE_STEP_LABELS.volatilities, 'Volatilities');
  assert.equal(USAGE_STEP_LABELS.coreUseCases, 'Use Cases');
  assert.equal(USAGE_STEP_LABELS.system, 'Architecture');
});
