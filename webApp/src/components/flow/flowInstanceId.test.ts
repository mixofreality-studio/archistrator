/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { flowInstanceId } from './flowInstanceId.ts';

void test('every React id format reduces to a safe, distinct flow id', () => {
  assert.equal(flowInstanceId(':r1:'), 'rf-r1');
  assert.equal(flowInstanceId('«r1»'), 'rf-r1');
  assert.equal(flowInstanceId('_r_1_'), 'rf-_r_1_');
  assert.notEqual(flowInstanceId(':r1:'), flowInstanceId(':r2:'));
  assert.match(flowInstanceId(':r1f:'), /^[A-Za-z0-9_-]+$/);
  assert.equal(flowInstanceId('::'), 'rf-0');
});
