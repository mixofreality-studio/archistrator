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
