/// <reference types="node" />
/**
 * Unit tests for the Architecture step's ?view=&step= deep-link resolution —
 * the use-case → call-chain jump, now to a specific step (Task 11). The rules
 * under test:
 *
 *  - an explicit ?view= naming an existing dynamic view WINS over module-memory
 *    lens persistence on mount (fresh navigation);
 *  - a param already consumed at the SAME location (a background-refetch
 *    remount, not a navigation) yields to module memory, so the deep link never
 *    fights a lens the reader chose afterwards (the remount gotcha);
 *  - a NEW navigation (new location key) re-applies, even with the same param;
 *  - a blank or dangling param never applies (memory / defaults rule);
 *  - ?step= (1-based) rides alongside ?view= under the SAME consume-once-per-
 *    location gating — it never applies on its own, and a non-positive-integer
 *    step is ignored (undefined) without blocking the view from applying.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveDeepLinkView } from './architectureDeepLink.ts';

const KEYS = ['dv-order', 'dv-track'];

void test('an explicit param naming an existing view applies on a fresh navigation', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-track',
    stepParam: '',
    locationKey: 'loc-1',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.deepEqual(d, { apply: true, key: 'dv-track', step: undefined });
});

void test('a blank param never applies (module memory rules)', () => {
  const d = resolveDeepLinkView({
    viewParam: '',
    stepParam: '',
    locationKey: 'loc-1',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.equal(d.apply, false);
});

void test('a dangling param (no such view) never applies', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-gone',
    stepParam: '',
    locationKey: 'loc-1',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.equal(d.apply, false);
});

void test('a param consumed at the same location yields to module memory (remount)', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-order',
    stepParam: '',
    locationKey: 'loc-1',
    consumedLocationKey: 'loc-1',
    availableKeys: KEYS,
  });
  assert.equal(d.apply, false);
});

void test('a NEW navigation (new location key) re-applies the same param', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-order',
    stepParam: '',
    locationKey: 'loc-2',
    consumedLocationKey: 'loc-1',
    availableKeys: KEYS,
  });
  assert.deepEqual(d, { apply: true, key: 'dv-order', step: undefined });
});

void test('a missing location key applies best-effort (never blocks the deep link)', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-order',
    stepParam: '',
    locationKey: '',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.equal(d.apply, true);
});

// ── step= (Task 11) ─────────────────────────────────────────────────────────

void test('a fresh navigation applies BOTH the view and a positive-integer step', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-order',
    stepParam: '3',
    locationKey: 'loc-1',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.deepEqual(d, { apply: true, key: 'dv-order', step: 3 });
});

void test('a same-location remount yields no step either (module memory)', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-order',
    stepParam: '3',
    locationKey: 'loc-1',
    consumedLocationKey: 'loc-1',
    availableKeys: KEYS,
  });
  assert.deepEqual(d, { apply: false, key: '', step: undefined });
});

void test('a non-positive-integer step is ignored (undefined) but the view still applies', () => {
  for (const raw of ['0', '-3', '2.5', 'abc', '']) {
    const d = resolveDeepLinkView({
      viewParam: 'dv-order',
      stepParam: raw,
      locationKey: 'loc-1',
      consumedLocationKey: '',
      availableKeys: KEYS,
    });
    assert.equal(d.apply, true, `apply should still be true for stepParam ${raw}`);
    assert.equal(d.step, undefined, `step should be undefined for stepParam ${raw}`);
  }
});

void test('a step param never applies on its own (no view naming a real view)', () => {
  const d = resolveDeepLinkView({
    viewParam: '',
    stepParam: '2',
    locationKey: 'loc-1',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.deepEqual(d, { apply: false, key: '', step: undefined });
});

void test('a dangling view param ignores the step too', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-gone',
    stepParam: '2',
    locationKey: 'loc-1',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.deepEqual(d, { apply: false, key: '', step: undefined });
});
