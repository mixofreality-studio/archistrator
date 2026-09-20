/// <reference types="node" />
/**
 * Unit tests for `resolveSubmitVerb` (submitVerb.ts) — the one function that picks
 * the SubmitBar's single primary verb from what is staged and the slot's
 * committed/lifecycle state. See submitVerb.ts's own doc comment for why this is
 * pure and lives outside the .tsx bar component.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveSubmitVerb } from './submitVerb.ts';

const base = {
  committed: false,
  stage: 'awaitingReview' as const,
  stagedChangeRequests: 0,
  stagedQuestions: 0,
  openThreads: 0,
};

void test('staged change requests send the draft back', () => {
  const v = resolveSubmitVerb({ ...base, stagedChangeRequests: 2, stagedQuestions: 1 });
  assert.equal(v.action, 'sendBack');
  assert.equal(v.label, 'Send back (3)');
  assert.equal(v.consequence, '2 change requests → redraft · 1 question → PM');
});

void test('questions alone ask without a redraft', () => {
  const v = resolveSubmitVerb({ ...base, stagedQuestions: 1 });
  assert.equal(v.action, 'ask');
  assert.equal(v.label, 'Ask (1) — no redraft');
});

void test('nothing staged and nothing open approves', () => {
  const v = resolveSubmitVerb(base);
  assert.equal(v.action, 'approve');
  assert.equal(v.disabled, false);
});

void test('open threads block approve and say so', () => {
  const v = resolveSubmitVerb({ ...base, openThreads: 3 });
  assert.equal(v.action, 'approve');
  assert.equal(v.disabled, true);
  assert.equal(v.label, 'Resolve 3 threads to approve');
});

void test('a committed slot amends', () => {
  const v = resolveSubmitVerb({ ...base, committed: true, stage: 'other', stagedChangeRequests: 1 });
  assert.equal(v.action, 'amend');
  assert.equal(v.label, 'Amend (1)');
});

void test('a committed slot with nothing staged offers no primary verb', () => {
  assert.equal(resolveSubmitVerb({ ...base, committed: true, stage: 'other' }).action, 'none');
});

// RULING P18: allowEmptySendBack (MCP has no client-side comment accumulator)
// must NEVER demote Approve as the primary verb — it only ever adds Send back
// as a SECONDARY (overflow) action. A branch that swapped the primary verb
// instead made Approve permanently unreachable for MCP; these tests pin the
// decision inside this pure function so no component-side inference can regress it.

void test('allowEmptySendBack offers Send back as a SECONDARY without demoting Approve', () => {
  const v = resolveSubmitVerb({ ...base, allowEmptySendBack: true });
  assert.equal(v.action, 'approve');
  assert.equal(v.label, 'Approve');
  assert.equal(v.disabled, false);
  assert.deepEqual(v.secondaryActions, ['sendBack']);
});

void test('allowEmptySendBack + open threads: primary stays the disabled Resolve-N-threads, Send back still secondary', () => {
  const v = resolveSubmitVerb({ ...base, openThreads: 3, allowEmptySendBack: true });
  assert.equal(v.action, 'approve');
  assert.equal(v.disabled, true);
  assert.equal(v.label, 'Resolve 3 threads to approve');
  assert.deepEqual(v.secondaryActions, ['sendBack']);
});

void test('allowEmptySendBack does not change the staged-change-request path', () => {
  const v = resolveSubmitVerb({ ...base, stagedChangeRequests: 2, allowEmptySendBack: true });
  assert.equal(v.action, 'sendBack');
  assert.equal(v.label, 'Send back (2)');
  assert.deepEqual(v.secondaryActions, []);
});

void test('allowEmptySendBack offers no secondary on a SEALED committed slot (stage: other) — superseded in spirit by RULING P20 below, which is about a LIVE amendment, not this inert case', () => {
  const v = resolveSubmitVerb({ ...base, committed: true, stage: 'other', allowEmptySendBack: true });
  assert.equal(v.action, 'none');
  assert.deepEqual(v.secondaryActions, []);
});

void test('allowEmptySendBack: false is identical to omitting it, throughout', () => {
  const withFalse = resolveSubmitVerb({ ...base, allowEmptySendBack: false });
  const omitted = resolveSubmitVerb(base);
  assert.deepEqual(withFalse, omitted);
  assert.deepEqual(withFalse.secondaryActions, []);
});

// RULING P19: STAGE decides whether a live draft is under review, NOT
// `committed` — `committed` only colours the wording. A committed slot's
// AMENDMENT under review (stage: 'awaitingReview') is exactly `committed: true`
// — the original code checked `committed` before `stage` and returned 'none'
// for this state, so a reviewer opening an amendment's review screen (before
// typing any new feedback — the NORMAL starting state) could never approve it
// at all. These tests pin the fix: `stage: 'awaitingReview'` (or 'drafted')
// always gets the live-draft rows, regardless of `committed`; 'none' is
// reserved for `stage: 'other'` (no live session) on a committed slot.

void test('a committed slot under active review (awaitingReview) with nothing staged still approves — THE REGRESSION CASE', () => {
  const v = resolveSubmitVerb({ ...base, committed: true, stage: 'awaitingReview' });
  assert.equal(v.action, 'approve');
  assert.equal(v.label, 'Approve');
  assert.equal(v.disabled, false);
});

void test('a committed slot under active review, open threads block approve exactly like an uncommitted draft', () => {
  const v = resolveSubmitVerb({ ...base, committed: true, stage: 'awaitingReview', openThreads: 2 });
  assert.equal(v.action, 'approve');
  assert.equal(v.disabled, true);
  assert.equal(v.label, 'Resolve 2 threads to approve');
});

void test('a committed slot under active review with staged feedback still amends (not send-back)', () => {
  const v = resolveSubmitVerb({
    ...base,
    committed: true,
    stage: 'awaitingReview',
    stagedChangeRequests: 1,
  });
  assert.equal(v.action, 'amend');
  assert.equal(v.label, 'Amend (1)');
  assert.equal(v.consequence, '1 change request → amend');
});

void test('a committed slot with NO live session (stage: other) offers no primary verb even with open threads', () => {
  const v = resolveSubmitVerb({ ...base, committed: true, stage: 'other', openThreads: 4 });
  assert.equal(v.action, 'none');
});

void test('an uncommitted slot with no live session (stage: other) is unaffected — still approves', () => {
  const v = resolveSubmitVerb({ ...base, committed: false, stage: 'other' });
  assert.equal(v.action, 'approve');
  assert.equal(v.disabled, false);
});

// RULING P20: the SECONDARY Send back (allowEmptySendBack) must be gated on
// `liveDraft`, not `!committed` — the same family of bug as P19, one level
// up. A committed slot's AMENDMENT under active review is exactly as much a
// "live draft an MCP host needs to send back" as an original, uncommitted
// draft is: gating the secondary on `!committed` left MCP with no reject path
// at all once reviewing an amendment with nothing newly staged (only Approve
// reachable). Only a genuinely SEALED artifact (`stage: 'other'`) offers none.

void test('a committed slot under active review offers Send back as a SECONDARY too — THE REGRESSION CASE', () => {
  const v = resolveSubmitVerb({
    ...base,
    committed: true,
    stage: 'awaitingReview',
    allowEmptySendBack: true,
  });
  assert.equal(v.action, 'approve');
  assert.equal(v.disabled, false);
  assert.deepEqual(v.secondaryActions, ['sendBack']);
});

void test('a SEALED committed slot (stage: other) still offers no secondary, even with allowEmptySendBack', () => {
  const v = resolveSubmitVerb({
    ...base,
    committed: true,
    stage: 'other',
    allowEmptySendBack: true,
  });
  assert.equal(v.action, 'none');
  assert.deepEqual(v.secondaryActions, []);
});

void test('allowEmptySendBack: false offers no secondary on an active amendment either', () => {
  const v = resolveSubmitVerb({ ...base, committed: true, stage: 'awaitingReview' });
  assert.deepEqual(v.secondaryActions, []);
});
