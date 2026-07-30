/// <reference types="node" />
/**
 * Unit tests for the Architecture step's ?view= deep-link resolution — the
 * use-case → call-chain jump. The rules under test:
 *
 *  - an explicit ?view= naming an existing dynamic view WINS over module-memory
 *    lens persistence on mount (fresh navigation);
 *  - a param already consumed at the SAME location (a background-refetch
 *    remount, not a navigation) yields to module memory, so the deep link never
 *    fights a lens the reader chose afterwards (the remount gotcha);
 *  - a NEW navigation (new location key) re-applies, even with the same param;
 *  - a blank or dangling param never applies (memory / defaults rule).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveDeepLinkView } from './architectureDeepLink.ts';

const KEYS = ['dv-order', 'dv-track'];

void test('an explicit param naming an existing view applies on a fresh navigation', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-track',
    locationKey: 'loc-1',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.deepEqual(d, { apply: true, key: 'dv-track' });
});

void test('a blank param never applies (module memory rules)', () => {
  const d = resolveDeepLinkView({
    viewParam: '',
    locationKey: 'loc-1',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.equal(d.apply, false);
});

void test('a dangling param (no such view) never applies', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-gone',
    locationKey: 'loc-1',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.equal(d.apply, false);
});

void test('a param consumed at the same location yields to module memory (remount)', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-order',
    locationKey: 'loc-1',
    consumedLocationKey: 'loc-1',
    availableKeys: KEYS,
  });
  assert.equal(d.apply, false);
});

void test('a NEW navigation (new location key) re-applies the same param', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-order',
    locationKey: 'loc-2',
    consumedLocationKey: 'loc-1',
    availableKeys: KEYS,
  });
  assert.deepEqual(d, { apply: true, key: 'dv-order' });
});

void test('a missing location key applies best-effort (never blocks the deep link)', () => {
  const d = resolveDeepLinkView({
    viewParam: 'dv-order',
    locationKey: '',
    consumedLocationKey: '',
    availableKeys: KEYS,
  });
  assert.equal(d.apply, true);
});
