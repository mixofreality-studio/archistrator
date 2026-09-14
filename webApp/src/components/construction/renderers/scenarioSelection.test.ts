/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { activeScenarioId, casesAsDropdown } from './scenarioSelection.ts';

const PLAN = [{ id: 'STP-UC1' }, { id: 'STP-UC2' }, { id: 'STP-UC3' }];

void test('the deep link wins: a reached-through row lands on ITS scenario, not the first', () => {
  assert.equal(activeScenarioId(PLAN, ['STP-UC3', 'STP-UC2']), 'STP-UC3');
});

void test('with no link, the local pick; with neither, the first scenario', () => {
  assert.equal(activeScenarioId(PLAN, [undefined, 'STP-UC2']), 'STP-UC2');
  assert.equal(activeScenarioId(PLAN, [undefined, '']), 'STP-UC1');
});

void test('an id the plan does not hold is ignored, never rendered as nothing', () => {
  assert.equal(activeScenarioId(PLAN, ['STP-UC9', undefined]), 'STP-UC1');
  assert.equal(activeScenarioId([], ['STP-UC3']), '');
});

void test('more than 3 cases go into a dropdown only in the narrow pane', () => {
  assert.equal(casesAsDropdown(4, 480), true);
  assert.equal(casesAsDropdown(5, 520), true);
  assert.equal(casesAsDropdown(3, 480), false);
  assert.equal(casesAsDropdown(4, 900), false);
  assert.equal(casesAsDropdown(4, 0), true, 'unmeasured reads narrow');
});
