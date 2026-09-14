/**
 * System test coverage for one component (designer §2.6 section 2): DIRECT for
 * the managers the plan's steps call, REACHED THROUGH (use-case join) for
 * everything else — and neither ever reads as "untested".
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { TestScenarioView } from '../../../../contracts/types.ts';
import {
  reachedThroughLabel,
  systemTestCoverageFor,
  useCaseFlowsSentence,
} from './componentCoverage.ts';

function scenario(id: string, useCase: string, component: string): TestScenarioView {
  return {
    id,
    useCase,
    title: `Title ${id}`,
    cases: [
      {
        id: `${id}-H1`,
        kind: 'happy',
        title: 'h',
        steps: [
          {
            seq: 1,
            component,
            operation: 'op',
            inputs: null,
            expect: { result: 'ok', errorExpected: false },
          },
        ],
      },
    ],
  };
}

const plan = [
  scenario('STP-UC1', 'drive-system-design', 'systemDesignManager'),
  scenario('STP-UC3', 'execute-a-construction-activity', 'constructionManager'),
  scenario('STP-UC5', 'bill-the-user-for-usage', 'billingManager'),
];

void test('a manager the plan calls is covered DIRECTLY, and not also reached through', () => {
  const c = systemTestCoverageFor(
    plan,
    ['constructionManager', 'construction-manager'],
    ['execute-a-construction-activity', 'drive-system-design']
  );
  assert.deepEqual(
    c.direct.map((s) => s.id),
    ['STP-UC3']
  );
  assert.deepEqual(
    c.reachedThrough.map((r) => r.scenario.id),
    ['STP-UC1']
  );
});

void test('an engine is reached through the managers of the use cases it appears in', () => {
  const c = systemTestCoverageFor(
    plan,
    ['reviewEngine', 'review-engine'],
    ['execute-a-construction-activity']
  );
  assert.deepEqual(c.direct, []);
  assert.equal(c.reachedThrough.length, 1);
  const first = c.reachedThrough[0];
  assert.ok(first !== undefined);
  assert.deepEqual(first.via, ['constructionManager']);
  assert.equal(
    reachedThroughLabel(first),
    'STP-UC3 · Title STP-UC3 — reached through constructionManager'
  );
});

void test('a component in no scenario and no use case has no rows', () => {
  const c = systemTestCoverageFor(plan, ['nobody'], []);
  assert.deepEqual(c, { direct: [], reachedThrough: [] });
  // Empty names never match an empty step component.
  assert.deepEqual(systemTestCoverageFor(plan, [''], []).direct, []);
});

void test('the use-case flows sentence says flows, not tests', () => {
  assert.equal(useCaseFlowsSentence(0), 'Appears in no use-case flow.');
  assert.equal(
    useCaseFlowsSentence(1),
    'Appears in 1 use-case flow. These are designed call chains, not tests.'
  );
  assert.match(useCaseFlowsSentence(14), /^Appears in 14 use-case flows\./);
});
