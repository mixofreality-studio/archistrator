/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { activeScenarioId } from './scenarioSelection.ts';

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
