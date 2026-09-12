/**
 * The review body's pure half (reviewVerdict.ts) and the artifact body's
 * dispatch (bodyDispatch.artifactRendererKeyFor), which the review body shares.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type {
  ConstructionReviewSet,
  ConstructionRow,
  ProducedArtifactRow,
} from '../../../../contracts/types.ts';
import {
  producedNoteAnchorPath,
  reviewerAnchorPath,
  reviewVerdictFor,
  RECONSTRUCTED_VERDICT_STAMP,
} from './reviewVerdict.ts';
import { artifactRendererKeyFor } from './bodyDispatch.ts';

function row(overrides: Partial<ConstructionRow> = {}): ConstructionRow {
  return {
    activityId: 'C-artifact-access',
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    phases: [],
    attempts: [],
    ...overrides,
  };
}

function produced(overrides: Partial<ProducedArtifactRow> = {}): ProducedArtifactRow {
  return {
    kind: 'code',
    title: 'artifactAccess — built component',
    source: 'implementation/log',
    produced: true,
    note: '',
    ...overrides,
  };
}

const REVIEW_SET: ConstructionReviewSet = {
  reviewers: [
    { role: 'system-architect', perspective: 'Contract conformance', mayAmend: true },
    { role: 'qa-engineer', perspective: 'Process health', mayAmend: false },
  ],
};

// ---------------------------------------------------------------------------
// The dropped signal — the single most important thing this body must not fake
// ---------------------------------------------------------------------------

void test('a produced-record note is STAMPED reconstructed, never presented as a verdict', () => {
  const verdict = reviewVerdictFor(
    row({
      produced: [produced({ note: 'Reviewed by the architect and merged; contract frozen.' })],
    }),
    undefined
  );
  assert.equal(verdict.source, 'reconstructedNote');
  assert.equal(verdict.stamp, RECONSTRUCTED_VERDICT_STAMP);
  assert.match(verdict.stamp, /reconstructed from the produced-record note/);
  assert.equal(verdict.notes.length, 1);
  const note = verdict.notes[0];
  assert.ok(note !== undefined);
  // The prose is reproduced VERBATIM and nothing derives a grade from it: a note
  // reading "reviewed and merged" is not a recorded PASS, and the view model
  // carries no field that could hold one.
  assert.equal(note.text, 'Reviewed by the architect and merged; contract frozen.');
  assert.equal(Object.keys(note).includes('verdict'), false);
  assert.equal(
    Object.keys(verdict).some((k) => /pass|fail|outcome|grade/i.test(k)),
    false,
    'the verdict view model must carry no synthesized outcome field'
  );
});

void test('the statement names the dropped signal rather than blaming this surface', () => {
  for (const verdict of [
    reviewVerdictFor(row(), undefined),
    reviewVerdictFor(row({ produced: [produced({ note: 'merged' })] }), undefined),
    reviewVerdictFor(row(), REVIEW_SET),
  ]) {
    assert.match(verdict.statement, /awaitPhaseDecision never dereferences sig\.Feedback/);
  }
});

void test('a live reviewer set is WHO WAS ASKED, and says so', () => {
  const verdict = reviewVerdictFor(row(), REVIEW_SET);
  assert.equal(verdict.source, 'reviewerSet');
  assert.deepEqual(
    verdict.reviewers.map((r) => r.role),
    ['system-architect', 'qa-engineer']
  );
  assert.match(verdict.statement, /who was asked to look/);
  assert.match(verdict.statement, /No per-reviewer verdict is recorded/);
  // No reviewer carries an outcome, because none is stored.
  for (const reviewer of verdict.reviewers) {
    assert.equal('verdict' in reviewer, false);
  }
});

void test('an empty note is not a note — blank produced records never become evidence', () => {
  const verdict = reviewVerdictFor(
    row({ produced: [produced({ note: '' }), produced({ note: '   ' })] }),
    undefined
  );
  assert.equal(verdict.notes.length, 0);
  assert.equal(verdict.stamp, undefined);
  assert.equal(verdict.source, 'none');
});

void test('reviewer set and surviving prose coexist without merging', () => {
  const verdict = reviewVerdictFor(row({ produced: [produced({ note: 'merged' })] }), REVIEW_SET);
  assert.equal(verdict.source, 'reviewerSet');
  assert.equal(verdict.reviewers.length, 2);
  // The prose is still shown — and still stamped. It is never attached to a
  // reviewer, because nothing records which reviewer (if any) wrote it.
  assert.equal(verdict.notes.length, 1);
  assert.equal(verdict.stamp, RECONSTRUCTED_VERDICT_STAMP);
});

void test('a note keeps its ORIGINAL produced index so its comment anchors to the right record', () => {
  const verdict = reviewVerdictFor(
    row({
      produced: [
        produced({ note: '' }),
        produced({ note: '' }),
        produced({ title: 'contract', note: 'frozen at the R-014 re-cut' }),
      ],
    }),
    undefined
  );
  const note = verdict.notes[0];
  assert.ok(note !== undefined);
  assert.equal(note.index, 2);
  assert.equal(
    producedNoteAnchorPath('C-artifact-access', note.index),
    '$.activityConstruction[C-artifact-access].produced[2].Note'
  );
});

void test('anchor paths are stable and human-meaningful — the server treats them as opaque', () => {
  assert.equal(
    reviewerAnchorPath('C-artifact-access', 1),
    '$.activityConstruction[C-artifact-access].reviewSet.reviewers[1]'
  );
  assert.equal(
    producedNoteAnchorPath('U-SPA-1', 0),
    '$.activityConstruction[U-SPA-1].produced[0].Note'
  );
});

// ---------------------------------------------------------------------------
// The artifact body's dispatch — the same key the review body renders above
// its verdict, so the two can never disagree about what is under review
// ---------------------------------------------------------------------------

void test('dispatch picks the right renderer key per kind', () => {
  assert.equal(
    artifactRendererKeyFor(row({ kind: 'service' }), { task: 'detailedDesign' }),
    'service'
  );
  assert.equal(
    artifactRendererKeyFor(row({ kind: 'uiDesign' }), { task: 'designReview' }),
    'uiDesign'
  );
  assert.equal(
    artifactRendererKeyFor(row({ kind: 'frontend' }), { task: 'construction' }),
    'frontend'
  );
  assert.equal(
    artifactRendererKeyFor(row({ kind: 'testing', variant: 'plan' }), { task: 'construction' }),
    'testing:plan'
  );
  assert.equal(
    artifactRendererKeyFor(row({ kind: 'testing', variant: 'systemTest' }), {
      task: 'construction',
    }),
    'testing:systemTest'
  );
});

void test('the cut kinds fall back honestly rather than to an empty renderer frame', () => {
  for (const kind of ['deployment', 'documentation', 'integration'] as const) {
    assert.equal(artifactRendererKeyFor(row({ kind }), { task: 'construction' }), undefined, kind);
  }
});

void test("a gate task sees its own phase's artifact, which is what it is reviewing", () => {
  // designReview gates Detailed Design, so the contract IS the thing under
  // review; codeReview gates Construction, where a service activity has no
  // artifact view in this stage.
  const service = row({ kind: 'service' });
  assert.equal(artifactRendererKeyFor(service, { task: 'designReview' }), 'service');
  assert.equal(artifactRendererKeyFor(service, { task: 'codeReview' }), undefined);
});
