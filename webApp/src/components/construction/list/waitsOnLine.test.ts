/**
 * Where an integration-pending row says what it waits on (designer final pass,
 * item 2): the fromPhase's own row in the list reads "waits on C-a, C-b" (or
 * "next in line") — and no other phase does.
 *
 * Task 13: a second case cross-asserted the GRAPH's segment line
 * (`segmentLineFor`) against the same rule, so the list's wording and the
 * graph's could not drift. The graph lens and its hover card are deleted, so
 * that cross-check has no second party and went with them; `pendingPhaseLine`
 * below is the one remaining source of the sentence.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pendingPhaseLine } from './pendingResume.ts';
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
