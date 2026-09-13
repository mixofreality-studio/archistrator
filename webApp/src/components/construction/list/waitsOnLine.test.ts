/**
 * Where an integration-pending row says what it waits on (designer final pass,
 * item 2): the fromPhase's own row in the list, its segment on the graph lane, and
 * its line in the hover card read "waits on C-a, C-b" (or "next in line") — and no
 * other phase does.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pendingPhaseLine } from './pendingResume.ts';
import { segmentLineFor } from '../graph/hoverCard.ts';
import { BILLING, doneRow, pendingRow } from './pendingResumeFixtures.ts';

void test('the fromPhase line, and only on the fromPhase', () => {
  assert.equal(
    pendingPhaseLine(BILLING, 'integration'),
    'waits on C-billing-state-access, C-merchant-gateway-access'
  );
  assert.equal(pendingPhaseLine(BILLING, 'construction'), undefined);
  assert.equal(pendingPhaseLine(pendingRow('C-next', []), 'integration'), 'next in line');
  assert.equal(pendingPhaseLine(doneRow('x'), 'integration'), undefined);
});

void test('the graph segment line names what the fromPhase waits on, and only there', () => {
  assert.equal(
    segmentLineFor(BILLING, { phase: 'integration', name: 'Integration' }, 'Not passed'),
    'Integration · Not passed · waits on C-billing-state-access, C-merchant-gateway-access'
  );
  assert.equal(
    segmentLineFor(BILLING, { phase: 'construction', name: 'Construction' }, 'Passed'),
    'Construction · Passed'
  );
  // A segment with no profile name reads its wire phase.
  assert.equal(
    segmentLineFor(doneRow('x'), { phase: 'integration' }, 'Passed'),
    'integration · Passed'
  );
});
