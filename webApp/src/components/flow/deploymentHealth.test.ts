import { test } from 'node:test';
import assert from 'node:assert/strict';
import { healthColorName, environmentIsObservable } from './deploymentHealth.ts';

void test('unknown model keys render neutral, never red', () => {
  assert.equal(healthColorName(undefined), 'neutral');
});

void test('healthy renders green', () => {
  assert.equal(healthColorName('Healthy'), 'green');
});

void test('unhealthy renders red', () => {
  assert.equal(healthColorName('Unhealthy'), 'red');
});

void test('nodes in the local and test environments are never coloured', () => {
  assert.equal(environmentIsObservable('cloud'), true);
  assert.equal(environmentIsObservable('local'), false);
  assert.equal(environmentIsObservable('test'), false);
});
