/// <reference types="node" />
/**
 * `liveDesignGate` — the MCP design widget's approve/reject gate, re-pointed at the
 * LIVE authority (stage 4b2 Task 9, controller ruling 1).
 *
 * The regression this pins: `SystemDesignView` opened its gate on
 * `stage === 'awaitingReview'`, a value the derived design-artifact session door has
 * been unable to emit since 4b1 (its whole vocabulary is unknown / committed /
 * withdrawn / draftFailed). The widget's gate could therefore never open for anyone —
 * a founder looking at a draft that was waiting on them saw no approve, no send-back
 * and no withdraw.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { designAmendmentInFlight, liveDesignGate } from './liveDesignGate.ts';
import type { ActivityViewWire } from '../activity/activityViewToGraph.ts';

type Revision = ActivityViewWire['tasks'][number]['revisions'][number];

function revision(n: number, outcome: string, detail?: string): Revision {
  return {
    n,
    outcome,
    attemptIds: [],
    commentCount: 0,
    comments: [],
    provenance: 'ledger',
    ...(detail !== undefined ? { detail } : {}),
  } as unknown as Revision;
}

function view(tasks: { id: string; revisions: Revision[] }[]): ActivityViewWire {
  return {
    activityId: 'requirements',
    name: 'Requirements',
    type: 'requirements',
    state: 'running',
    phases: [],
    tasks: tasks.map((t) => ({
      id: t.id,
      title: t.id,
      kind: 'dispatch',
      phase: 'mission',
      dependsOn: [],
      state: 'running',
      revisions: t.revisions,
    })),
  } as unknown as ActivityViewWire;
}

const REF = { reviewTaskId: 'missionReview', dispatchTaskId: 'missionDraft' };

void test('the gate OPENS when the review task’s latest revision awaits a human', () => {
  const gate = liveDesignGate(
    view([
      { id: 'missionReview', revisions: [revision(1, 'sentBack'), revision(2, 'awaitingHuman')] },
    ]),
    REF
  );
  assert.equal(gate.awaitingHuman, true);
});

void test('the gate stays SHUT on a decided round — the LATEST revision is the question', () => {
  // A revision list is oldest-first, so reading the first one would hold the gate
  // open on an activity whose round was approved an hour ago.
  const gate = liveDesignGate(
    view([
      { id: 'missionReview', revisions: [revision(1, 'awaitingHuman'), revision(2, 'passed')] },
    ]),
    REF
  );
  assert.equal(gate.awaitingHuman, false);
});

void test('no activity view, no such task, and no revisions all answer SHUT rather than throwing', () => {
  assert.equal(liveDesignGate(undefined, REF).awaitingHuman, false);
  assert.equal(liveDesignGate(view([]), REF).awaitingHuman, false);
  assert.equal(
    liveDesignGate(view([{ id: 'missionReview', revisions: [] }]), REF).awaitingHuman,
    false
  );
  // An unresolvable kind gives `designTaskRef` no activity, hence an empty id.
  assert.equal(
    liveDesignGate(view([{ id: 'missionReview', revisions: [revision(1, 'awaitingHuman')] }]), {
      reviewTaskId: '',
      dispatchTaskId: '',
    }).awaitingHuman,
    false
  );
});

void test('the failed run link is read out of the failed dispatch attempt’s own sentence', () => {
  const gate = liveDesignGate(
    view([
      {
        id: 'missionDraft',
        revisions: [
          revision(
            1,
            'failed',
            'the design job failed in your CI: https://github.com/acme/app/actions/runs/42.'
          ),
        ],
      },
    ]),
    REF
  );
  // Trailing sentence punctuation is not part of the URL.
  assert.equal(gate.failedRunUrl, 'https://github.com/acme/app/actions/runs/42');
});

void test('a sentence with no URL yields no link, and a PASSED attempt never yields one', () => {
  // Nothing is fabricated: the old `failureRunUrl` was absent in exactly this case.
  assert.equal(
    liveDesignGate(
      view([
        { id: 'missionDraft', revisions: [revision(1, 'failed', 'the worker ran out of credits')] },
      ]),
      REF
    ).failedRunUrl,
    undefined
  );
  // A passed attempt's detail says what it DRAFTED. Linking that as "the failed run"
  // would be a claim the reader has no way to check.
  assert.equal(
    liveDesignGate(
      view([
        {
          id: 'missionDraft',
          revisions: [revision(1, 'passed', 'drafted mission on https://example.test/run/1')],
        },
      ]),
      REF
    ).failedRunUrl,
    undefined
  );
});

// ── The OTHER half of the ack refusal: a draft that is RUNNING (Task 9 review) ──
// `sessionLive` had collapsed to `draftFailed` alone, so the stale-basis ack stayed
// ENABLED over an amendment in flight — precisely the merge conflict the popover's own
// sentence warns about. The predicate lives here, not in the .tsx, so it can be held.

void test('a RUNNING dispatch is reported, and a settled one is not', () => {
  const running = liveDesignGate(
    view([{ id: 'missionDraft', revisions: [revision(1, 'passed'), revision(2, 'running')] }]),
    REF
  );
  assert.equal(running.dispatchRunning, true);
  assert.equal(
    liveDesignGate(
      view([{ id: 'missionDraft', revisions: [revision(1, 'running'), revision(2, 'passed')] }]),
      REF
    ).dispatchRunning,
    false,
    'the LATEST revision is the question here too'
  );
  // No view, no such task, no revisions: shut, never a throw.
  assert.equal(liveDesignGate(undefined, REF).dispatchRunning, false);
  assert.equal(liveDesignGate(view([]), REF).dispatchRunning, false);
});

void test('an amendment is in flight while a draft runs, while one awaits a human, or after it failed', () => {
  const base = { awaitingHuman: false, dispatchRunning: false, stage: 'committed' };
  assert.equal(
    designAmendmentInFlight({ ...base, dispatchRunning: true }),
    true,
    'a running draft is the case the ack refusal names out loud, and it was the one uncovered'
  );
  assert.equal(designAmendmentInFlight({ ...base, awaitingHuman: true }), true);
  assert.equal(designAmendmentInFlight({ ...base, stage: 'draftFailed' }), true);
  // A clean committed slot is the ONE state the ack may proceed from.
  assert.equal(designAmendmentInFlight(base), false);
  assert.equal(designAmendmentInFlight({ ...base, stage: 'withdrawn' }), false);
});
