/// <reference types="node" />
/**
 * Unit tests for GatePanel's PM REVIEW presentation logic (src/components/design/
 * pmReviewLogic.ts) — the F-QA2-7 surface: the founder at the human gate sees what
 * the PM concluded (verdict + rationale + judged round), not just the machine
 * validation counters.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pmReviewPresentation } from './pmReviewLogic.ts';

void test('approve with notes: APPROVED badge, rationale verbatim', () => {
  const p = pmReviewPresentation({
    role: 'productManager',
    verdict: 'approve',
    summary: 'solid mission; one wording reservation noted',
    round: 0,
  });
  assert.equal(p.badge, 'APPROVED');
  assert.equal(p.approved, true);
  assert.equal(p.caption, 'Product manager');
  assert.equal(p.summary, 'solid mission; one wording reservation noted');
});

void test('clean approve (no notes): honest fallback sentence, never a blank body', () => {
  const p = pmReviewPresentation({
    role: 'productManager',
    verdict: 'approve',
    summary: '',
    round: 0,
  });
  assert.equal(p.badge, 'APPROVED');
  assert.ok(p.summary.length > 0);
});

void test('revise: PUSHED BACK badge with the PM rationale', () => {
  const p = pmReviewPresentation({
    role: 'productManager',
    verdict: 'revise',
    summary: 'tighten the vision sentence',
    round: 4,
  });
  assert.equal(p.badge, 'PUSHED BACK');
  assert.equal(p.approved, false);
  assert.equal(p.summary, 'tighten the vision sentence');
});

void test('round > 0 is named in the caption; round 0 is not', () => {
  const later = pmReviewPresentation({
    role: 'productManager',
    verdict: 'approve',
    summary: 'converged',
    round: 2,
  });
  assert.equal(later.caption, 'Product manager · judged draft round 2');
  const first = pmReviewPresentation({
    role: 'productManager',
    verdict: 'approve',
    summary: 'ok',
    round: 0,
  });
  assert.equal(first.caption, 'Product manager');
});

void test('an unknown critic role passes through verbatim (no fabricated label)', () => {
  const p = pmReviewPresentation({
    role: 'qaEngineer',
    verdict: 'revise',
    summary: 'x',
    round: 0,
  });
  assert.equal(p.caption, 'qaEngineer');
});
