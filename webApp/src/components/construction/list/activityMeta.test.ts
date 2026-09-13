/**
 * The one per-activity schedule join (activityMeta.ts) both lenses read. The
 * rule with teeth: an activity with no computed entry gets NO float — never a
 * fabricated zero, which would read as "on the critical path".
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { activityMetaFor } from './activityMeta.ts';

const LIST = [
  { name: 'C-a', title: 'Billing Manager', effortDays: 15, componentId: 'billing-manager' },
  { name: 'C-b', title: '', effortDays: 10, componentId: '' },
  { name: 'N-STP', title: 'System Test Plan', effortDays: 10 },
];

const COMPUTED = {
  'C-a': { totalFloat: 6, onCriticalPath: false, band: 'yellow' },
  'N-STP': { totalFloat: 0, onCriticalPath: true, band: 'critical' },
};

void test('a computed entry joins float, criticality and band untouched', () => {
  const meta = activityMetaFor(LIST, COMPUTED);
  assert.deepEqual(meta['C-a'], {
    label: 'Billing Manager',
    effortDays: 15,
    float: 6,
    onCriticalPath: false,
    band: 'yellow',
    componentId: 'billing-manager',
  });
});

void test('a true zero float passes through as zero', () => {
  const stp = activityMetaFor(LIST, COMPUTED)['N-STP'];
  assert.ok(stp !== undefined);
  assert.equal(stp.float, 0);
  assert.equal(stp.onCriticalPath, true);
});

void test('NO computed entry: no float, no band, no criticality — never a fabricated 0', () => {
  const meta = activityMetaFor(LIST, COMPUTED)['C-b'];
  assert.ok(meta !== undefined);
  assert.equal('float' in meta, false);
  assert.equal('band' in meta, false);
  assert.equal('onCriticalPath' in meta, false);
  assert.equal(meta.effortDays, 10, 'effort is slot 9 and joins on its own');
});

void test('no network at all joins no schedule figure for anyone', () => {
  for (const m of Object.values(activityMetaFor(LIST, undefined))) {
    assert.equal('float' in m, false);
  }
});

void test('an empty title or componentId is absent, not an empty string', () => {
  const meta = activityMetaFor(LIST, COMPUTED)['C-b'];
  assert.ok(meta !== undefined);
  assert.equal('label' in meta, false);
  assert.equal('componentId' in meta, false);
});

void test('an activity the list does not carry gets no metadata at all', () => {
  assert.equal(activityMetaFor(LIST, COMPUTED)['C-gone'], undefined);
  assert.deepEqual(activityMetaFor(undefined, COMPUTED), {});
});
