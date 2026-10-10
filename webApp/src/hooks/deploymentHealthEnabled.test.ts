import { test } from 'node:test';
import assert from 'node:assert/strict';
import { deploymentHealthQueryEnabled } from './deploymentHealthEnabled.ts';

void test('dormant when operatedAppId is empty (malformed route / no project loaded)', () => {
  assert.equal(deploymentHealthQueryEnabled(''), false);
});

void test('enabled when a real operated-app id is supplied', () => {
  assert.equal(deploymentHealthQueryEnabled('project-123'), true);
});
