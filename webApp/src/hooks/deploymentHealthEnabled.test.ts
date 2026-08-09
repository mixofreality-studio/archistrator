import { test } from 'node:test';
import assert from 'node:assert/strict';
import { deploymentHealthQueryEnabled } from './deploymentHealthEnabled.ts';

void test('dormant when the operations capability is off, even with a real project id', () => {
  assert.equal(deploymentHealthQueryEnabled(false, 'project-123'), false);
});

void test('dormant when operatedAppId is empty (malformed route / no project loaded), even with the capability on', () => {
  assert.equal(deploymentHealthQueryEnabled(true, ''), false);
});

void test('enabled when a real project id is supplied and the capability is on', () => {
  assert.equal(deploymentHealthQueryEnabled(true, 'project-123'), true);
});
