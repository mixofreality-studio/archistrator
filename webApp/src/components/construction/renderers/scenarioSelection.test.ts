/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { activeScenarioId, scenarioPlanState } from './scenarioSelection.ts';

const PLAN = [{ id: 'UC1-P1' }, { id: 'UC2-P1' }, { id: 'UC3-P2' }];

void test('the deep link wins: a reached-through row lands on ITS scenario, not the first', () => {
  assert.equal(activeScenarioId(PLAN, ['UC3-P2', 'UC2-P1']), 'UC3-P2');
});

void test('with no link, the local pick; with neither, the first scenario', () => {
  assert.equal(activeScenarioId(PLAN, [undefined, 'UC2-P1']), 'UC2-P1');
  assert.equal(activeScenarioId(PLAN, [undefined, '']), 'UC1-P1');
});

void test('an id the plan does not hold is ignored, never rendered as nothing', () => {
  assert.equal(activeScenarioId(PLAN, ['UC9-P9', undefined]), 'UC1-P1');
  assert.equal(activeScenarioId([], ['UC3-P2']), '');
});

// -- scenarioPlanState: what the plan view says about a read in each state -------

void test('a DISABLED read (no component) is "unread", never an empty projection', () => {
  // react-query's disabled shape: not loading, no error, no data. The old view read
  // this as "no use-case path reaches this component" — a claim about a read that
  // never ran.
  assert.equal(scenarioPlanState({ data: undefined, error: null, isLoading: false }), 'unread');
});

void test('loading, error, empty and ready are told apart, in that precedence', () => {
  assert.equal(scenarioPlanState({ data: undefined, error: null, isLoading: true }), 'loading');
  assert.equal(
    scenarioPlanState({ data: undefined, error: new Error('x'), isLoading: false }),
    'error'
  );
  // An empty ARRAY is the server's real answer: nothing reaches the component.
  assert.equal(scenarioPlanState({ data: [], error: null, isLoading: false }), 'empty');
  assert.equal(
    scenarioPlanState({ data: [{ id: 'UC1-P1' }], error: null, isLoading: false }),
    'ready'
  );
  // A stale answer with an error on top: the error is what the reviewer must see.
  assert.equal(scenarioPlanState({ data: [], error: new Error('x'), isLoading: false }), 'error');
});
