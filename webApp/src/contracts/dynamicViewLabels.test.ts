/// <reference types="node" />
/**
 * Unit tests for the dynamic-view picker label fallback chain (F-QA2-51):
 * a drafted DynamicView can carry a blank title, which used to render blank
 * MUI Select options. Labels now resolve through the chain
 *
 *   title → linked use case's name (useCaseId against the committed
 *   coreUseCases model) → positional "Untitled view N"
 *
 * so a picker option is never blank. Covers the pure logic module
 * (src/contracts/dynamicViewLabels.ts) that adapters.listDynamicViews /
 * listDynamicViewsForComponent delegate to.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { CoreUseCases, UseCaseDecision } from './types';
import { dynamicViewLabel, indexUseCaseNames } from './dynamicViewLabels.ts';

function decision(id: string, name: string): UseCaseDecision {
  return {
    rejectionReason: '',
    useCase: {
      id,
      name,
      classification: 'core',
      trigger: 'clientAction',
      actors: [],
      activity: null,
      variationOf: null,
    },
  };
}

const USE_CASES: CoreUseCases = {
  decisions: [decision('uc-1', 'Place Order'), decision('uc-2', '  Track Shipment  ')],
};
const NAME_BY_ID = indexUseCaseNames(USE_CASES);

void test('a present title wins over every fallback', () => {
  const label = dynamicViewLabel({ title: 'Place Order Flow', useCaseId: 'uc-1' }, 0, NAME_BY_ID);
  assert.equal(label, 'Place Order Flow');
});

void test('a blank title falls back to the linked use case name (trimmed)', () => {
  assert.equal(dynamicViewLabel({ title: '', useCaseId: 'uc-2' }, 1, NAME_BY_ID), 'Track Shipment');
  // Whitespace-only titles count as blank too.
  assert.equal(dynamicViewLabel({ title: '   ', useCaseId: 'uc-1' }, 0, NAME_BY_ID), 'Place Order');
});

void test('an unresolvable useCaseId falls back to a 1-based positional placeholder', () => {
  assert.equal(
    dynamicViewLabel({ title: '', useCaseId: 'uc-missing' }, 2, NAME_BY_ID),
    'Untitled view 3'
  );
});

void test('without any use cases, blank titles go straight to placeholders', () => {
  const empty = indexUseCaseNames(undefined);
  assert.equal(empty.size, 0);
  assert.equal(dynamicViewLabel({ title: '', useCaseId: 'uc-1' }, 0, empty), 'Untitled view 1');
});

void test('a use case with a blank name is excluded from the index (never a blank label)', () => {
  const index = indexUseCaseNames({ decisions: [decision('uc-blank', '   ')] });
  assert.equal(index.has('uc-blank'), false);
  const label = dynamicViewLabel({ title: '', useCaseId: 'uc-blank' }, 4, index);
  assert.equal(label, 'Untitled view 5');
});

void test('null decisions are tolerated', () => {
  assert.equal(indexUseCaseNames({ decisions: null }).size, 0);
});
