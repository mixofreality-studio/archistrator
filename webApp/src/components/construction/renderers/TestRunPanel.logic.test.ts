/// <reference types="node" />
/**
 * The test-run panel's pure helpers (testRunPanelLogic.ts), pinned without a
 * renderer. Plan 2 Task 9 — Review Focus 4 lives here: a run whose `videoRef` is
 * absent (a server component) offers no Video tab, so nothing ever mounts a
 * `<video>` over an empty src.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  goTestNameFor,
  goTestOutputFor,
  summarize,
  tabsFor,
  venueRunUrl,
} from './testRunPanelLogic.ts';

void test('summarize counts pass / fail / skip and nothing else', () => {
  const s = summarize([
    { status: 'pass' },
    { status: 'pass' },
    { status: 'fail' },
    { status: 'skip' },
    { status: 'weird' },
  ]);
  assert.deepEqual(s, { pass: 2, fail: 1, skip: 1 });
  assert.deepEqual(summarize([]), { pass: 0, fail: 0, skip: 0 });
});

void test('a server scenario (no videoRef) offers only the Output tab — Review Focus 4', () => {
  assert.deepEqual(tabsFor({ videoRef: '', traceRef: '' }), ['Output']);
  assert.deepEqual(tabsFor({ videoRef: 'ui/UC3-P2.webm', traceRef: '' }), ['Output', 'Video']);
  // A trace alone is a button, never a tab: the viewer opens in a new window.
  assert.deepEqual(tabsFor({ videoRef: '', traceRef: 'ui/UC3-P2.zip' }), ['Output']);
});

void test('the generated Go test name sanitizes the scenario id as testgen does', () => {
  // app-generator/testgen `ident`: every non-alphanumeric rune becomes `_`.
  assert.equal(goTestNameFor('UC3-P2'), 'TestScenario_UC3_P2');
  assert.equal(
    goTestNameFor('execute-a-project-activity-P1'),
    'TestScenario_execute_a_project_activity_P1'
  );
});

void test('goTestOutputFor keeps only the output lines of one scenario and its subtests', () => {
  const jsonl = [
    JSON.stringify({ Action: 'run', Test: 'TestScenario_UC3_P2' }),
    JSON.stringify({
      Action: 'output',
      Test: 'TestScenario_UC3_P2',
      Output: '=== RUN   TestScenario_UC3_P2\n',
    }),
    JSON.stringify({
      Action: 'output',
      Test: 'TestScenario_UC3_P2/UC3-P2',
      Output: '    host.go:12: step 1 ok\n',
    }),
    JSON.stringify({ Action: 'output', Test: 'TestScenario_UC3_P1', Output: 'NOT MINE\n' }),
    JSON.stringify({ Action: 'output', Test: 'TestScenario_UC3_P20', Output: 'NOT MINE EITHER\n' }),
    JSON.stringify({ Action: 'output', Package: 'x', Output: 'ok  \tx\t0.1s\n' }),
    'not json at all',
    JSON.stringify({ Action: 'pass', Test: 'TestScenario_UC3_P2', Elapsed: 0.01 }),
  ].join('\n');
  assert.equal(
    goTestOutputFor(jsonl, 'UC3-P2'),
    '=== RUN   TestScenario_UC3_P2\n    host.go:12: step 1 ok\n'
  );
  assert.equal(goTestOutputFor(jsonl, 'UC9-P9'), '');
  assert.equal(goTestOutputFor('', 'UC3-P2'), '');
});

void test('the venue link is the GitHub run page beside the PR, or nothing for a local run', () => {
  assert.equal(
    venueRunUrl('https://github.com/acme/widgets/pull/17', '123456'),
    'https://github.com/acme/widgets/actions/runs/123456'
  );
  assert.equal(venueRunUrl(undefined, '123456'), undefined);
  assert.equal(venueRunUrl('https://github.com/acme/widgets/pull/17', ''), undefined);
  assert.equal(venueRunUrl('not a url', '1'), undefined);
});
