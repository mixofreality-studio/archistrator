/// <reference types="node" />
/**
 * Unit tests for operationsEnabled (src/utilities/capabilities.ts). The
 * loading case pins operations-argocd-deployment spec D9's SAFE default:
 * capabilities not yet loaded must read as hidden, never shown — a flash of an
 * Operations tab that then vanishes is worse than a tab that appears a moment
 * late.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { operationsEnabled } from './capabilities.ts';

void test('operations hidden when the server reports no operations capability', () => {
  assert.equal(operationsEnabled({ operations: false }), false);
});

void test('operations hidden while capabilities are still loading', () => {
  assert.equal(operationsEnabled(undefined), false);
});

void test('operations shown only when the server reports the capability', () => {
  assert.equal(operationsEnabled({ operations: true }), true);
});
