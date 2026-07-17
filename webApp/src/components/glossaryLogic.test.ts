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
import type { GlossaryItem } from '../contracts/types';
import {
  categoryBase,
  categoryRank,
  chipCategories,
  filterGlossary,
  indexGlossaryItems,
  matchAnnouncement,
  normalizeCategory,
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
  const chips = chipCategories(CORPUS);
  assert.deepEqual(chips, [
    ['Who', 1],
    ['What', 1],
    ['How', 3], // How-activity + How resource access + plain How rolled up
    ['Where', 1],
    ['Uncategorized', 1],
  ]);
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
