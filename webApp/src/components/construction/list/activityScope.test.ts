/**
 * The Task 11 navigability rules (activityScope.ts) — a plain `.ts` module, so
 * it is tested with no renderer in the way. The brief's own steps come first
 * verbatim in intent (every scope chip against a fixture carrying an
 * unclassified row, a reconstructed row and a retried row; "expand to current
 * phase" opening only in-flight activities); the rest pin kind/layer/search/
 * sort, which this task also implements end to end.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, TaskAttemptRow } from '../../../contracts/types.ts';
import { buildActivityTree, type ActivityMeta, type ActivityNode } from './activityTree.ts';
import {
  activityPassesSearch,
  applyToolbarToActivities,
  currentPhaseExpansionIds,
  expandToCurrentPhaseControl,
  isInFlight,
  rowIsInFlight,
  matchesKind,
  matchesLayer,
  matchingTaskIds,
  needsInlineProvenanceMark,
  scopePredicate,
  sortActivities,
} from './activityScope.ts';
import { observedOnlyRow } from './observedOnly.ts';

// ---------------------------------------------------------------------------
// Fixtures — the same shape activityTree.test.ts and activityRowPresentation
// .test.ts already establish.
// ---------------------------------------------------------------------------

function row(overrides: Partial<ConstructionRow> = {}): ConstructionRow {
  return {
    activityId: 'C-x',
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    phases: [],
    attempts: [],
    ...overrides,
  };
}

function attempt(overrides: Partial<TaskAttemptRow> = {}): TaskAttemptRow {
  return {
    attemptId: 'a1',
    task: 'codeReview',
    phase: 'construction',
    attempt: 1,
    outcome: 'passed',
    evidence: { kind: '', ref: '' },
    provenance: { origin: 'observed' },
    ...overrides,
  };
}

function nodeFor(r: ConstructionRow, meta?: ActivityMeta): ActivityNode {
  const nodes = buildActivityTree(
    [r],
    meta !== undefined ? { meta: { [r.activityId]: meta } } : {}
  );
  const node = nodes[0];
  assert.ok(node !== undefined);
  return node;
}

// The brief's fixture set: unclassified, reconstructed, retried.
const unclassified = nodeFor(row({ activityId: 'C-unclassified', classified: false }));

const reconstructed = nodeFor(
  row({
    activityId: 'C-reconstructed',
    kind: 'service',
    attempts: [
      attempt({
        task: 'srsReview',
        phase: 'requirements',
        outcome: 'passed',
        provenance: { origin: 'backfilled', basis: 'founderRuling[2026-09-09]' },
      }),
    ],
    status: 'integrated',
  })
);

const retried = nodeFor(
  row({
    activityId: 'C-retried',
    kind: 'service',
    status: 'in-construction',
    attempts: [
      attempt({ task: 'codeReview', phase: 'construction', attempt: 1, outcome: 'failed' }),
      attempt({ task: 'codeReview', phase: 'construction', attempt: 2, outcome: 'passed' }),
    ],
  })
);

const critical = nodeFor(row({ activityId: 'C-critical' }), { onCriticalPath: true, float: 0 });
const near = nodeFor(row({ activityId: 'C-near' }), { onCriticalPath: false, float: 4 });
const farFloat = nodeFor(row({ activityId: 'C-far' }), { onCriticalPath: false, float: 40 });
const awaitingMe = nodeFor(row({ activityId: 'C-awaiting', kind: 'service', status: 'in-review' }));
const recorded = nodeFor(
  row({
    activityId: 'C-recorded',
    kind: 'service',
    status: 'integrated',
    attempts: [attempt({ provenance: { origin: 'observed' } })],
  })
);

// ---------------------------------------------------------------------------
// Scope chips
// ---------------------------------------------------------------------------

void test('scope "all" passes every fixture', () => {
  for (const n of [unclassified, reconstructed, retried, critical, near, farFloat, awaitingMe]) {
    assert.equal(scopePredicate('all', n), true);
  }
});

void test('scope "critical" is on-critical-path only', () => {
  assert.equal(scopePredicate('critical', critical), true);
  assert.equal(scopePredicate('critical', near), false);
  assert.equal(scopePredicate('critical', unclassified), false);
});

void test('scope "near" is float<=5 and NOT on the critical path', () => {
  assert.equal(scopePredicate('near', near), true);
  assert.equal(scopePredicate('near', critical), false, 'critical path excludes near-critical');
  assert.equal(scopePredicate('near', farFloat), false, 'float 40 is not near-critical');
  assert.equal(scopePredicate('near', unclassified), false, 'no float on record is not near');
});

void test('scope "awaitingMe" is the live owed set, never head-state in-review (Q4)', () => {
  const owed = new Map([['C-awaiting', { reason: 'gate' as const }]]);
  assert.equal(scopePredicate('awaitingMe', awaitingMe), false, 'in-review alone is not a gate');
  assert.equal(scopePredicate('awaitingMe', awaitingMe, owed), true);
  assert.equal(scopePredicate('awaitingMe', retried, owed), false, 'not owed is not awaiting me');
  assert.equal(scopePredicate('awaitingMe', unclassified, owed), false);
  // A steer and a failure are owed too — the TASKS lens's rows, exactly.
  const steer = new Map([['C-retried', { reason: 'takeover' as const }]]);
  assert.equal(scopePredicate('awaitingMe', retried, steer), true);
});

void test('scope "inFlight" is in construction OR in review (the one isInFlight)', () => {
  assert.equal(scopePredicate('inFlight', retried), true);
  // Mid-lifecycle (in-review) is in flight, and so is a live gate or a steer on it:
  // the owed-aware row state is running or awaiting (orchestrator ruling).
  assert.equal(scopePredicate('inFlight', awaitingMe), true, 'in-review is mid-lifecycle');
  assert.equal(
    scopePredicate('inFlight', awaitingMe, new Map([['C-awaiting', { reason: 'gate' as const }]])),
    true,
    'a live gate is still in flight (it overlaps awaitingMe on purpose)'
  );
  assert.equal(
    scopePredicate('inFlight', retried, new Map([['C-retried', { reason: 'takeover' as const }]])),
    true,
    'a steer is still in flight'
  );
  assert.equal(
    scopePredicate('inFlight', retried, new Map([['C-retried', { reason: 'failed' as const }]])),
    false,
    'a recorded failure reads failed: the pump stopped on it'
  );
  assert.equal(scopePredicate('inFlight', recorded), false, 'integrated is not in flight');
});

void test('scope "hasRetries" is retryCount > 0', () => {
  assert.equal(scopePredicate('hasRetries', retried), true);
  assert.equal(scopePredicate('hasRetries', reconstructed), false);
  assert.equal(scopePredicate('hasRetries', unclassified), false);
});

void test('scope "reconstructed" is the reconstructed provenance grade', () => {
  assert.equal(scopePredicate('reconstructed', reconstructed), true);
  assert.equal(scopePredicate('reconstructed', recorded), false);
  assert.equal(
    scopePredicate('reconstructed', unclassified),
    false,
    'unknown is not reconstructed'
  );
});

void test('scope "unknown" is the unknown provenance grade (empty ledger)', () => {
  assert.equal(scopePredicate('unknown', unclassified), true);
  assert.equal(scopePredicate('unknown', reconstructed), false);
  assert.equal(scopePredicate('unknown', recorded), false);
});

// ---------------------------------------------------------------------------
// Kind / layer
// ---------------------------------------------------------------------------

void test('matchesKind: "all" passes everything, else exact match', () => {
  assert.equal(matchesKind(reconstructed, 'all'), true);
  assert.equal(matchesKind(reconstructed, 'service'), true);
  assert.equal(matchesKind(reconstructed, 'frontend'), false);
  assert.equal(matchesKind(unclassified, 'service'), false, 'no kind on an unclassified row');
});

void test('matchesLayer: "all" passes everything, else exact match', () => {
  const layered = nodeFor(row({ activityId: 'C-layer', layer: 'manager', layerBand: 'layered' }));
  assert.equal(matchesLayer(layered, 'all'), true);
  assert.equal(matchesLayer(layered, 'manager'), true);
  assert.equal(matchesLayer(layered, 'engine'), false);
  assert.equal(matchesLayer(unclassified, 'manager'), false, 'no layer on an unclassified row');
});

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

const searchable = nodeFor(row({ activityId: 'C-billing-engine', kind: 'service' }), {
  label: 'Billing Manager',
  componentId: 'billing-manager',
});
const searchableWithTask = nodeFor(
  row({
    activityId: 'C-SVC',
    kind: 'service',
    attempts: [attempt({ task: 'codeReview', phase: 'construction' })],
  }),
  { label: 'Some Service' }
);

void test('search matches activity id, title (label) and componentId', () => {
  assert.equal(
    activityPassesSearch(searchable, 'C-BILLING-ENG'),
    true,
    'activity id, case-insensitive'
  );
  assert.equal(activityPassesSearch(searchable, 'billing manager'), true, 'title');
  assert.equal(activityPassesSearch(searchable, 'billing-manager'), true, 'componentId');
  assert.equal(activityPassesSearch(searchable, 'nope'), false);
});

void test('a tier-3 (task) match keeps the activity and names the matched task id', () => {
  assert.equal(
    activityPassesSearch(searchableWithTask, 'code review'),
    true,
    'the task label matched even though no activity-level field did'
  );
  const ids = matchingTaskIds(searchableWithTask, 'code review');
  assert.deepEqual(ids, ['C-SVC::construction::codeReview']);
});

void test('matchingTaskIds is empty for an empty query and for a non-matching one', () => {
  assert.deepEqual(matchingTaskIds(searchableWithTask, ''), []);
  assert.deepEqual(matchingTaskIds(searchableWithTask, 'zzz'), []);
});

void test('an empty query passes everything and reveals nothing', () => {
  assert.equal(activityPassesSearch(unclassified, ''), true);
  assert.deepEqual(matchingTaskIds(searchableWithTask, ''), []);
});

// ---------------------------------------------------------------------------
// Sort — tier 1 only
// ---------------------------------------------------------------------------

void test('sortActivities("network") preserves input order verbatim', () => {
  const input = [farFloat, critical, near];
  assert.deepEqual(
    sortActivities(input, 'network').map((n) => n.activityId),
    ['C-far', 'C-critical', 'C-near']
  );
});

void test('sortActivities("floatAsc") sorts ascending with unknown float LAST', () => {
  const input = [farFloat, critical, near, unclassified];
  assert.deepEqual(
    sortActivities(input, 'floatAsc').map((n) => n.activityId),
    ['C-critical', 'C-near', 'C-far', 'C-unclassified']
  );
});

// ---------------------------------------------------------------------------
// The combined pipeline
// ---------------------------------------------------------------------------

void test('applyToolbarToActivities combines every filter with AND, then sorts', () => {
  const nodes = [critical, near, farFloat, reconstructed, recorded];
  const out = applyToolbarToActivities(nodes, {
    scope: 'all',
    kind: 'all',
    layer: 'all',
    search: '',
    sort: 'floatAsc',
  });
  // No toolbar filter removes a row for its provenance any more ("Observed only"
  // rewrites evidence, it does not filter — observedOnly.ts): the rows sort by
  // float asc, the two that carry no float trailing in their relative order.
  assert.deepEqual(
    out.map((n) => n.activityId),
    ['C-critical', 'C-near', 'C-far', 'C-reconstructed', 'C-recorded']
  );
});

// ---------------------------------------------------------------------------
// "Expand to current phase"
// ---------------------------------------------------------------------------

void test('isInFlight is true for in-construction and in-review only', () => {
  const at = (status: ConstructionRow['status']): ActivityNode =>
    nodeFor(row({ activityId: 'C-s', kind: 'service', ...(status ? { status } : {}) }));
  assert.equal(isInFlight(at('in-construction')), true);
  assert.equal(isInFlight(at('in-review')), true);
  assert.equal(isInFlight(at('integrated')), false);
  assert.equal(isInFlight(at('failed')), false);
  assert.equal(isInFlight(at(undefined)), false);
  // It reads the ROW STATE, so an unclassified in-construction row is not in flight.
  assert.equal(isInFlight(nodeFor(row({ classified: false, status: 'in-construction' }))), false);
});

void test('isInFlight reads the OWED-AWARE row state: a gate or steer is in flight, a failure is not', () => {
  const n = nodeFor(row({ activityId: 'C-s', kind: 'service', status: 'in-construction' }));
  const owedAs = (reason: 'gate' | 'takeover' | 'failed'): Map<string, { reason: typeof reason }> =>
    new Map([['C-s', { reason }]]);
  assert.equal(isInFlight(n, owedAs('gate')), true, 'a live gate: awaiting, still in flight');
  assert.equal(isInFlight(n, owedAs('takeover')), true, 'a steer: awaiting, still in flight');
  assert.equal(isInFlight(n, owedAs('failed')), false, 'a recorded failure reads failed');
  // Another activity's mark is not this one's.
  assert.equal(isInFlight(n, new Map([['C-other', { reason: 'failed' as const }]])), true);
  // The bare-row form is the same rule (the Begin control reads it).
  assert.equal(rowIsInFlight(n.row, { reason: 'failed' }), false);
  assert.equal(rowIsInFlight(n.row, { reason: 'gate' }), true);
  assert.equal(rowIsInFlight(n.row), true);
});

void test('the chip and the button still agree once the owed set is applied', () => {
  const failedMark = new Map([['C-retried', { reason: 'failed' as const }]]);
  const nodes = [retried, awaitingMe, recorded];
  const expanded = new Set(currentPhaseExpansionIds(nodes, failedMark));
  for (const n of nodes) {
    assert.equal(scopePredicate('inFlight', n, failedMark), expanded.has(n.nodeId), n.activityId);
  }
  assert.deepEqual([...expanded], ['C-awaiting']);
  assert.equal(expandToCurrentPhaseControl([retried], failedMark).enabled, false);
  assert.equal(expandToCurrentPhaseControl([retried]).enabled, true);
});

// Final web review: the scope chip used to read `running` only while the button
// read in-construction OR in-review — "In flight" meant two things on one toolbar.
void test('the "In flight" scope and "Expand to current phase" share ONE definition', () => {
  const stripped = nodeFor(
    observedOnlyRow(
      row({
        activityId: 'C-stripped',
        kind: 'service',
        status: 'in-construction',
        attempts: [attempt({ provenance: { origin: 'backfilled' } })],
      })
    )
  );
  // An unclassified row CAN carry an in-construction status; with no profile it
  // has no current phase to open, and the chip must not list it either.
  const unclassifiedRunning = nodeFor(
    row({ activityId: 'C-unclassified-running', classified: false, status: 'in-construction' })
  );
  const all = [
    unclassified,
    unclassifiedRunning,
    reconstructed,
    retried,
    awaitingMe,
    recorded,
    critical,
    near,
    stripped,
  ];
  const expanded = new Set(currentPhaseExpansionIds(all));
  for (const n of all) {
    assert.equal(
      scopePredicate('inFlight', n),
      expanded.has(n.nodeId),
      `${n.activityId}: the chip and the button disagree`
    );
  }
  assert.deepEqual([...expanded], ['C-retried', 'C-awaiting']);
  // What the chip shows is exactly what enables the button.
  const shown = all.filter((n) => scopePredicate('inFlight', n));
  assert.equal(expandToCurrentPhaseControl(shown).enabled, true);
  assert.equal(
    expandToCurrentPhaseControl(all.filter((n) => !scopePredicate('inFlight', n))).enabled,
    false
  );
});

void test('currentPhaseExpansionIds opens ONLY the in-flight activities', () => {
  const notStarted = nodeFor(row({ activityId: 'C-notstarted', kind: 'service' }));
  const integrated = nodeFor(row({ activityId: 'C-done', kind: 'service', status: 'integrated' }));
  const ids = currentPhaseExpansionIds([
    unclassified,
    retried, // in-construction
    awaitingMe, // in-review
    notStarted,
    integrated,
  ]);
  assert.deepEqual(ids, ['C-retried', 'C-awaiting']);
});

// ---------------------------------------------------------------------------
// The search-reveal provenance guarantee (carried forward from Task 7)
// ---------------------------------------------------------------------------
//
// A tier-3 task row read in isolation asserts a state with no provenance mark
// of its own — the design's defence is that its group header (which carries
// the badge) is always visible above it. Search can reveal and focus a task
// row on its own initiative, which is exactly what can break that argument.
// `needsInlineProvenanceMark` is what ActivityTreeView.tsx calls to decide
// whether a MATCHED row must carry its own mark regardless of ancestor
// visibility — pinned here so a future edit cannot silently narrow it back to
// "the ancestors are probably visible".

void test('needsInlineProvenanceMark: reconstructed AND matched needs its own mark', () => {
  assert.equal(needsInlineProvenanceMark(true, 'backfilled'), true);
  assert.equal(needsInlineProvenanceMark(true, 'synthesized'), true);
});

void test('needsInlineProvenanceMark: not a search match never needs a mark, any origin', () => {
  assert.equal(needsInlineProvenanceMark(false, 'backfilled'), false);
  assert.equal(needsInlineProvenanceMark(false, 'synthesized'), false);
  assert.equal(needsInlineProvenanceMark(false, 'observed'), false);
  assert.equal(needsInlineProvenanceMark(false, 'unknown'), false);
});

void test('needsInlineProvenanceMark: a matched but RECORDED row needs no mark — it is true', () => {
  assert.equal(needsInlineProvenanceMark(true, 'observed'), false);
});

void test('needsInlineProvenanceMark: a matched but UNKNOWN row needs no mark — it already asserts nothing', () => {
  assert.equal(needsInlineProvenanceMark(true, 'unknown'), false);
});

void test('search matches the book name of a task the profile renamed', () => {
  const stp = nodeFor(row({ activityId: 'N-STP', kind: 'testing', variant: 'plan' }));
  // N-STP's construction gate is labelled for the profile, not "Code Review" …
  const ids = matchingTaskIds(stp, 'code review');
  // … yet the operator who knows the book's word still finds it.
  assert.deepEqual(ids, ['N-STP::construction::codeReview']);
  assert.equal(activityPassesSearch(stp, 'code review'), true);
});

void test('"Expand to current phase" is disabled, and says why, when nothing is in flight', () => {
  const idle = expandToCurrentPhaseControl([critical, near, recorded, unclassified]);
  assert.equal(idle.enabled, false);
  assert.match(idle.tooltip, /Nothing is in construction or awaiting your review/);
  const one = expandToCurrentPhaseControl([critical, retried]);
  assert.equal(one.enabled, true);
  assert.match(one.tooltip, /the activity in construction/);
  const two = expandToCurrentPhaseControl([retried, awaitingMe, near]);
  assert.match(two.tooltip, /2 activities/);
});
