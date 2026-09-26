/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  activityViewToGraph,
  artifactKindOf,
  lifecycleKeyFor,
  OUTCOME_TEXT,
  shortDate,
  taskFactsFor,
  type ActivityViewWire,
} from './activityViewToGraph.ts';
import { lifecycleFor } from './lifecycles.gen.ts';

// THE FIXTURES LIVE IN TWO TREES, and this test reads from both. Nearly every
// activity-experience state is recorded under uitests/preview-fixtures, because that is
// the root the preview build is pointed at (playwright.config.ts's
// ARCHISTRATOR_PREVIEW_FIXTURES); one smoke state stays under webApp/preview/fixtures, the
// recorded-design location the U-SPA-web-client Design phase will fill. A name is looked
// up in both, in that order, and a miss names both roots rather than the one it happened
// to try last — a fixture moved between the trees must not read as a fixture deleted.
const FIXTURE_ROOTS = [
  '../../../../uitests/preview-fixtures/web-client/activity-experience/',
  '../../../preview/fixtures/web-client/activity-experience/',
];

function fixture(name: string): ActivityViewWire {
  const tried: string[] = [];
  for (const root of FIXTURE_ROOTS) {
    const url = new URL(`${root}${name}.json`, import.meta.url);
    tried.push(url.pathname);
    let text: string;
    try {
      text = readFileSync(url, 'utf8');
    } catch {
      continue;
    }
    const doc = JSON.parse(text) as {
      ops: { deliveryQueryActivityView: { result: ActivityViewWire } };
    };
    return doc.ops.deliveryQueryActivityView.result;
  }
  throw new Error(`fixture "${name}" is in neither tree: ${tried.join(', ')}`);
}

void test('a passed task becomes a done node, and a completed phase becomes a passed phase', () => {
  const graph = activityViewToGraph(fixture('done'));
  assert.ok(graph.nodes.length > 0);
  assert.ok(
    graph.nodes.every((n) => n.state === 'done'),
    'every task of the done fixture passed'
  );
  assert.ok(
    graph.phases.every((p) => p.passed),
    'every phase of the done fixture completed'
  );
  assert.deepEqual(
    graph.phases.map((p) => p.weight),
    [20, 45, 35],
    'the weights ride through untouched'
  );
});

void test('the outcome enum becomes the sentence a reader reads, never the wire word', () => {
  assert.equal(OUTCOME_TEXT.awaitingHuman, 'awaiting you');
  assert.equal(OUTCOME_TEXT.sentBack, 'sent back');
  assert.equal(OUTCOME_TEXT.passed, 'approved');
});

void test('the dispatch facts are joined from the lifecycle table — the wire carries none of them', () => {
  const view = fixture('not-started'); // U-SPA-web-client, type: frontend
  const facts = taskFactsFor(view, 'srs');
  // The FRONTEND lifecycle's `srs` is "UX Requirements", run by the ui-designer
  // charter — not the service lifecycle's senior-developer SRS. The two share a
  // task id and nothing else, which is exactly why this join is a table lookup
  // keyed on (type, variant) and never on the task id alone.
  assert.equal(facts.workerClass, 'ui-designer');
  assert.equal(facts.command, 'frontend-requirements');
  assert.equal(facts.artifactKind, 'SRS');
  assert.ok((facts.exitCriterion ?? '').length > 0, 'its phase names a binary exit criterion');
  assert.deepEqual(
    taskFactsFor(view, 'noSuchTask'),
    {},
    'an unknown task joins nothing, it does not throw'
  );
});

void test('a REVIEW task takes its artifact kind from the dispatch it judges', () => {
  // `architectureReview` carries `reviews: 'architectureDraft'` and no
  // artifactKind of its own; the draft carries `artifactKind: 'System'`.
  const view = {
    type: 'architecture',
    variant: undefined,
    tasks: [],
    phases: [],
  } as unknown as ActivityViewWire;
  assert.equal(taskFactsFor(view, 'architectureReview').artifactKind, 'System');
  assert.equal(taskFactsFor(view, 'architectureDraft').artifactKind, 'System');
});

void test('every review task in every lifecycle resolves an artifact kind', () => {
  // The regression this chain exists to prevent: reading `task.artifactKind`
  // alone returns undefined for all ten review tasks below, and the review
  // body's verb table keys on it.
  for (const [key, taskId, expected] of [
    ['requirements', 'missionReview', 'Mission'],
    ['requirements', 'glossaryReview', 'Glossary'],
    ['requirements', 'volatilitiesReview', 'Volatilities'],
    ['requirements', 'coreUseCasesReview', 'CoreUseCases'],
    ['service', 'srsReview', 'SRS'],
    ['service', 'designReview', 'DetailedDesign'],
    ['service', 'codeReview', 'Construction'],
    ['service', 'stpReview', 'STP'],
    ['service', 'testing', 'Integration'],
  ] as const) {
    const def = lifecycleFor(key);
    assert.ok(def !== undefined, key);
    const task = def.tasks.find((t) => t.id === taskId);
    assert.ok(task !== undefined, `${key}/${taskId}`);
    assert.equal(artifactKindOf(def, task), expected, `${key}/${taskId}`);
  }
});

void test('projectDesign’s sdpReview carries its own kind and judges no dispatch', () => {
  const def = lifecycleFor('projectDesign');
  assert.ok(def !== undefined);
  const task = def.tasks.find((t) => t.id === 'sdpReview');
  assert.ok(task !== undefined);
  assert.equal(task.reviews, undefined, 'the SDP is computed — there is nothing to judge');
  assert.equal(artifactKindOf(def, task), 'SdpReview');
});

void test('a testing activity keys on its variant; everything else keys on its type', () => {
  assert.equal(lifecycleKeyFor('testing', 'systemTest'), 'testing:systemTest');
  assert.equal(lifecycleKeyFor('testing', undefined), 'testing:plan');
  assert.equal(lifecycleKeyFor('architecture', undefined), 'architecture');
});

void test('an unparseable date is shown verbatim rather than as "Invalid Date"', () => {
  assert.equal(shortDate('2026-09-12T10:00:00Z'), 'Sep 12');
  assert.equal(shortDate('whenever'), 'whenever');
});
