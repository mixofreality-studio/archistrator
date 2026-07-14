/// <reference types="node" />
/**
 * Unit tests for roleLineFor — the pure copy mapping behind the generating
 * scene's role line. Run with `npm run test` (Node's built-in test runner over
 * TypeScript via native type stripping; there is no other test framework in the
 * webApp toolchain — see webapp-checks.yml).
 *
 * The app build pins `types: ["vite/client"]`, so this file pulls in the Node
 * runtime type declarations (node:test / node:assert) via the reference above
 * rather than widening the whole project's global types.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { roleLineFor } from './roleLine.ts';

const PHRASE = 'vision and mission statement';

void test('none role → undefined (fallback to the plain DRAFTING… pill)', () => {
  assert.equal(roleLineFor('none', 'none', 0, PHRASE), undefined);
  // role none wins even if a step somehow leaked through
  assert.equal(roleLineFor('none', 'drafting', 0, PHRASE), undefined);
});

void test('none step → undefined even when a role is set', () => {
  assert.equal(roleLineFor('architect', 'none', 0, PHRASE), undefined);
});

void test('drafting → architect crafting, architect avatar', () => {
  assert.deepEqual(roleLineFor('architect', 'drafting', 0, PHRASE), {
    seed: 'system-architect',
    text: 'Architect is crafting the vision and mission statement',
  });
});

void test('critiquing → product manager reviewing, PM avatar', () => {
  assert.deepEqual(roleLineFor('productManager', 'critiquing', 0, 'glossary'), {
    seed: 'product-manager',
    text: 'Product manager is reviewing the glossary',
  });
});

void test('revising with round > 0 shows the (round N) suffix', () => {
  assert.deepEqual(roleLineFor('architect', 'revising', 2, 'core use cases'), {
    seed: 'system-architect',
    text: 'Architect is revising the core use cases (round 2)',
  });
});

void test('revising with round 0 omits the suffix (N shown only when > 0)', () => {
  assert.deepEqual(roleLineFor('architect', 'revising', 0, 'risk model'), {
    seed: 'system-architect',
    text: 'Architect is revising the risk model',
  });
});

// Only three (role, step) pairs are legal: architect+drafting, productManager+critiquing,
// architect+revising. Every other combo must fall back to undefined (the honest plain pill)
// rather than render a mismatched avatar/caption.
void test('illegal combo productManager+drafting → undefined', () => {
  assert.equal(roleLineFor('productManager', 'drafting', 0, PHRASE), undefined);
});

void test('illegal combo architect+critiquing → undefined', () => {
  assert.equal(roleLineFor('architect', 'critiquing', 0, PHRASE), undefined);
});

void test('illegal combo productManager+revising → undefined', () => {
  assert.equal(roleLineFor('productManager', 'revising', 1, PHRASE), undefined);
});
