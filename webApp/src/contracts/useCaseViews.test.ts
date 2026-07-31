/// <reference types="node" />
/**
 * Unit tests for the pure use-case view module (src/contracts/useCaseViews.ts)
 * that adapters.toCoreUseCasesView / dynamicViewKeyForUseCase delegate to:
 *
 *  - toUseCaseView maps the optional UseCaseDecision.essenceRationale (the
 *    WHY-core argument, symmetric to the nonCore rejectionReason) — '' when
 *    absent/null so older committed states render no empty chrome.
 *  - viewKeyForUseCase resolves the System dynamic view that renders a use
 *    case's call chain (the use-case → architecture navigable join): first
 *    keyed view whose useCaseId links back, undefined otherwise.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { System, UseCaseDecision } from './types';
import { ownerUseCaseId, toUseCaseView, viewKeyForUseCase } from './useCaseViews.ts';

function decision(
  id: string,
  name: string,
  opts: {
    classification?: 'core' | 'nonCore';
    essenceRationale?: string | null;
    rejectionReason?: string;
  } = {}
): UseCaseDecision {
  return {
    rejectionReason: opts.rejectionReason ?? '',
    // Omit essenceRationale entirely when the test doesn't set it (the wire field
    // is optional — older committed states never carry it).
    ...(opts.essenceRationale !== undefined ? { essenceRationale: opts.essenceRationale } : {}),
    useCase: {
      id,
      name,
      classification: opts.classification ?? 'core',
      trigger: 'clientAction',
      actors: [],
      activity: null,
      variationOf: null,
    },
  };
}

// ── essenceRationale mapping ────────────────────────────────────────────────

void test('a present essenceRationale maps onto the UseCaseView verbatim', () => {
  const uc = toUseCaseView(
    decision('uc-1', 'Place Order', { essenceRationale: 'It IS the business.' })
  );
  assert.equal(uc.essenceRationale, 'It IS the business.');
});

void test('an absent essenceRationale (older states) maps to the empty string', () => {
  assert.equal(toUseCaseView(decision('uc-1', 'Place Order')).essenceRationale, '');
});

void test('an explicit null essenceRationale maps to the empty string', () => {
  assert.equal(
    toUseCaseView(decision('uc-1', 'Place Order', { essenceRationale: null })).essenceRationale,
    ''
  );
});

void test('rejectionReason keeps mapping alongside the new field', () => {
  const uc = toUseCaseView(
    decision('uc-2', 'Fax Order', { classification: 'nonCore', rejectionReason: 'A variation.' })
  );
  assert.equal(uc.rejectionReason, 'A variation.');
  assert.equal(uc.essenceRationale, '');
});

// ── viewKeyForUseCase resolution ────────────────────────────────────────────

const SYSTEM: System = {
  components: null,
  relationships: null,
  dynamicViews: [
    { key: 'dv-order', title: 'Place Order', useCaseId: 'uc-1', steps: null },
    { key: '', title: 'Broken', useCaseId: 'uc-2', steps: null },
    { key: 'dv-track', title: 'Track', useCaseId: 'uc-2', steps: null },
  ],
};

void test('resolves the dynamic view keyed to the use case', () => {
  assert.equal(viewKeyForUseCase(SYSTEM, 'uc-1'), 'dv-order');
});

void test('skips a blank-keyed view in favor of a later keyed match', () => {
  assert.equal(viewKeyForUseCase(SYSTEM, 'uc-2'), 'dv-track');
});

void test('returns undefined when no view links back to the use case', () => {
  assert.equal(viewKeyForUseCase(SYSTEM, 'uc-none'), undefined);
});

void test('returns undefined for an absent model or a blank id', () => {
  assert.equal(viewKeyForUseCase(undefined, 'uc-1'), undefined);
  assert.equal(viewKeyForUseCase(SYSTEM, '  '), undefined);
});

void test('tolerates null dynamicViews', () => {
  const bare: System = { components: null, relationships: null, dynamicViews: null };
  assert.equal(viewKeyForUseCase(bare, 'uc-1'), undefined);
});

// ── ownerUseCaseId resolution (the inverse join) ───────────────────────────

void test('resolves the use case a keyed dynamic view realizes', () => {
  assert.equal(ownerUseCaseId(SYSTEM, 'dv-order'), 'uc-1');
  assert.equal(ownerUseCaseId(SYSTEM, 'dv-track'), 'uc-2');
});

void test('returns undefined for an unknown, blank or absent key/model', () => {
  assert.equal(ownerUseCaseId(SYSTEM, 'dv-none'), undefined);
  assert.equal(ownerUseCaseId(SYSTEM, '  '), undefined);
  assert.equal(ownerUseCaseId(undefined, 'dv-order'), undefined);
});

void test('a view with no use-case back-link resolves to undefined', () => {
  const synthetic: System = {
    components: null,
    relationships: null,
    dynamicViews: [{ key: 'dv-synth', title: 'Synthetic', useCaseId: '', steps: null }],
  };
  assert.equal(ownerUseCaseId(synthetic, 'dv-synth'), undefined);
});
