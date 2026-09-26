/// <reference types="node" />
/**
 * ?screen=&state= resolution (src/previewShell/fixtureRegistry.ts): an exact
 * match boots, anything else is an honest, specific error.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolvePreviewState, statesFromModules } from './fixtureRegistry.ts';

const resting = { route: '/project/demo/construction', ops: {} };
const loading = {
  route: '/project/demo/construction',
  ops: { deliveryQueryProjectView: { summary: { pending: true } } },
};
const landing = { route: '/', ops: {} };

const states = statesFromModules({
  '/abs/uitests/preview-fixtures/web-client/construction/resting.json': resting,
  '/abs/uitests/preview-fixtures/web-client/construction/loading.json': loading,
  '/abs/uitests/preview-fixtures/web-client/landing/resting.json': landing,
});

void test('indexes fixtures by screen directory and state file name', () => {
  assert.deepEqual([...states.keys()], ['construction', 'landing']);
  assert.deepEqual([...(states.get('construction')?.keys() ?? [])], ['loading', 'resting']);
});

void test('an exact screen and state resolves to its fixture', () => {
  const r = resolvePreviewState('?screen=construction&state=loading', states);
  assert.ok(r.kind === 'ok');
  assert.equal(r.fixture, loading);
});

void test('an unknown screen, an unknown state, or a missing param never guesses', () => {
  assert.equal(resolvePreviewState('?screen=billing&state=resting', states).kind, 'unknown-screen');
  assert.equal(
    resolvePreviewState('?screen=construction&state=empty', states).kind,
    'unknown-state'
  );
  assert.equal(resolvePreviewState('?screen=construction', states).kind, 'missing-params');
  assert.equal(resolvePreviewState('', states).kind, 'missing-params');
  const r = resolvePreviewState('?screen=billing&state=x', states);
  assert.deepEqual(r.kind === 'ok' ? [] : r.available, [
    { screen: 'construction', states: ['loading', 'resting'] },
    { screen: 'landing', states: ['resting'] },
  ]);
});

void test('a build with no fixtures says so', () => {
  assert.equal(resolvePreviewState('?screen=a&state=b', statesFromModules({})).kind, 'no-fixtures');
});
