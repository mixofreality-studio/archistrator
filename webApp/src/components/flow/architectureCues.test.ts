/// <reference types="node" />
/**
 * Unit tests for the pure architecture-diagram cues
 * (src/components/flow/architectureCues.ts). Covers componentLacksVolatility —
 * the anti-functional-decomposition smell: a Manager/Engine/ResourceAccess
 * component encapsulating NO volatility (typed field empty AND, for older
 * states, prose empty) gets the quiet warning badge; Client/Resource/Utility
 * are exempt by design (they encapsulate deployment/storage/mechanism, not
 * business volatility).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { componentLacksVolatility, NO_VOLATILITY_WARNING } from './architectureCues.ts';
import type { Layer } from '../../contracts/types';

/** Shorthand fixture for the predicate's input. */
function c(
  layer: Layer,
  encapsulates: string,
  encapsulatesVolatilities: string[] = []
): { layer: Layer; encapsulates: string; encapsulatesVolatilities: string[] } {
  return { layer, encapsulates, encapsulatesVolatilities };
}

void test('a volatility-less Manager/Engine/ResourceAccess is flagged', () => {
  assert.equal(componentLacksVolatility(c('manager', '')), true);
  assert.equal(componentLacksVolatility(c('engine', '')), true);
  assert.equal(componentLacksVolatility(c('resourceAccess', '')), true);
});

void test('the typed field alone clears the flag', () => {
  assert.equal(componentLacksVolatility(c('manager', '', ['Payment Methods'])), false);
});

void test('prose alone clears the flag (older states without the typed field)', () => {
  assert.equal(componentLacksVolatility(c('engine', 'Payment Methods: how we settle.')), false);
});

void test('whitespace-only prose does not clear the flag', () => {
  assert.equal(componentLacksVolatility(c('resourceAccess', '   ')), true);
});

void test('Client/Resource/Utility layers are exempt', () => {
  assert.equal(componentLacksVolatility(c('client', '')), false);
  assert.equal(componentLacksVolatility(c('resource', '')), false);
  assert.equal(componentLacksVolatility(c('utility', '')), false);
});

void test('the warning copy names the smell and its source', () => {
  assert.match(NO_VOLATILITY_WARNING, /functional decomposition/i);
  assert.match(NO_VOLATILITY_WARNING, /Righting Software/);
});
