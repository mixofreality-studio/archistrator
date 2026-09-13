import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ConstructionRow } from '../../../contracts/types';
import {
  beginControlFor,
  beginHoldFor,
  dispatchOutcomeCopy,
  dispatchOutcomeFor,
  holdExpiredCopy,
  notStartedActivities,
  pumpEvidencedSince,
  UNKNOWN_OUTCOME_HOLD_MS,
} from './beginControl.ts';

const COMMITTED_WORDS = /Begin construction|Resume construction/;

void test('while the project is loading, the button is disabled and names neither Begin nor Resume', () => {
  for (const constructionStarted of [true, false, undefined]) {
    const c = beginControlFor({ constructionStarted, projectLoading: true, running: false });
    assert.equal(c.disabled, true, `loading + ${String(constructionStarted)}`);
    assert.doesNotMatch(c.label, COMMITTED_WORDS, `loading + ${String(constructionStarted)}`);
  }
});

void test('the label is the server’s constructionStarted: true is Resume, false is Begin', () => {
  const resume = beginControlFor({
    constructionStarted: true,
    projectLoading: false,
    running: false,
  });
  assert.equal(resume.label, 'Resume construction');
  assert.equal(resume.verb, 'Resume');
  assert.equal(resume.disabled, false);
  const begin = beginControlFor({
    constructionStarted: false,
    projectLoading: false,
    running: false,
  });
  assert.equal(begin.label, 'Begin construction');
  assert.equal(begin.verb, 'Begin');
  assert.equal(begin.disabled, false);
});

void test('no project read claims neither word, and dispatches nothing', () => {
  const c = beginControlFor({
    constructionStarted: undefined,
    projectLoading: false,
    running: false,
  });
  assert.doesNotMatch(c.label, COMMITTED_WORDS);
  assert.equal(c.disabled, true);
});

void test('a run in flight disables the button whatever the project says', () => {
  const c = beginControlFor({ constructionStarted: false, projectLoading: false, running: true });
  assert.equal(c.disabled, true);
});

function row(overrides: Partial<ConstructionRow>): ConstructionRow {
  return {
    activityId: 'C-x',
    classified: true,
    hasBuildEvidence: false,
    recorded: false,
    phases: [],
    attempts: [],
    ...overrides,
  };
}

void test('a Begin names exactly the unrecorded rows, sorted, with titles where the list has one', () => {
  const rows = {
    'N-IT': row({ activityId: 'N-IT' }),
    'C-done': row({ activityId: 'C-done', recorded: true, hasBuildEvidence: true }),
    // Recorded but with no evidence yet (started, no phase): under way, not a candidate.
    'C-started': row({ activityId: 'C-started', recorded: true, hasBuildEvidence: false }),
    'C-billing-state-access': row({ activityId: 'C-billing-state-access' }),
  };
  const titles: Record<string, string> = { 'N-IT': 'System testing' };
  const got = notStartedActivities(rows, (id) => titles[id]);
  assert.deepEqual(got, [
    { activityId: 'C-billing-state-access' },
    { activityId: 'N-IT', title: 'System testing' },
  ]);
  assert.deepEqual(
    notStartedActivities(undefined, () => undefined),
    [],
    'no rows yet is nothing to name'
  );
});

// ---------------------------------------------------------------------------
// A failed dispatch (fix-C review, Important): a 5xx or a dropped response can
// arrive AFTER the server started the pump, so only a 4xx is a refusal.
// ---------------------------------------------------------------------------

void test('only a 4xx is a rejection; a 5xx, no status at all, or anything else is an unknown outcome', () => {
  assert.deepEqual(dispatchOutcomeFor(400, 'empty tickId'), {
    kind: 'rejected',
    message: 'empty tickId',
  });
  assert.equal(dispatchOutcomeFor(404, 'no such project').kind, 'rejected');
  assert.equal(dispatchOutcomeFor(499, 'x').kind, 'rejected');
  for (const status of [500, 502, 503, 504, undefined, 302, 200]) {
    assert.equal(dispatchOutcomeFor(status, 'boom').kind, 'unknown', String(status));
  }
  assert.equal(dispatchOutcomeFor(400, '  ').message, 'no reason given');
});

void test('the unknown copy is the ruling verbatim and never invites a retry; the rejection quotes the server', () => {
  const unknown = dispatchOutcomeCopy(dispatchOutcomeFor(500, 'request failed with status 500'));
  assert.equal(
    unknown.headline,
    'Outcome unknown — the pump may have started; the list will show it if it did.'
  );
  assert.doesNotMatch(`${unknown.headline} ${unknown.detail}`, /again|retry|failed:/i);
  const rejected = dispatchOutcomeCopy(dispatchOutcomeFor(400, 'empty tickId'));
  assert.match(rejected.headline, /^Construction dispatch rejected: empty tickId\.$/);
  assert.match(rejected.detail, /nothing was started/);
});

// ---------------------------------------------------------------------------
// After an unknown outcome, Begin is held until the pump is EVIDENCED, or a
// bounded hold expires (orchestrator ruling on the fix-D review, I3). A newer
// read alone no longer lifts it.
// ---------------------------------------------------------------------------

const NO_READS = {
  projectReadAt: 0,
  constructionStarted: undefined,
  sessionReadAt: 0,
  sessionStage: undefined,
} as const;

void test('a newer read that says "not started", with no live session, is NOT pump evidence', () => {
  assert.equal(pumpEvidencedSince(1000, NO_READS), false, 'no read yet');
  assert.equal(
    pumpEvidencedSince(1000, { ...NO_READS, projectReadAt: 5000, constructionStarted: false }),
    false
  );
  assert.equal(
    pumpEvidencedSince(1000, { ...NO_READS, sessionReadAt: 5000, sessionStage: undefined }),
    false,
    'a probe that established no session exists'
  );
});

void test('pump evidence: a newer read says constructionStarted, or a newer probe shows a live session', () => {
  assert.equal(
    pumpEvidencedSince(1000, { ...NO_READS, projectReadAt: 1001, constructionStarted: true }),
    true
  );
  for (const stage of [
    'dispatching',
    'pipelineRunning',
    'reviewing',
    'awaitingTakeover',
    'awaitingApproval',
  ] as const) {
    assert.equal(
      pumpEvidencedSince(1000, { ...NO_READS, sessionReadAt: 1001, sessionStage: stage }),
      true,
      stage
    );
  }
  for (const stage of ['exited', 'paused', 'unknown'] as const) {
    assert.equal(
      pumpEvidencedSince(1000, { ...NO_READS, sessionReadAt: 1001, sessionStage: stage }),
      false,
      `${stage} is not a running pump`
    );
  }
});

void test('only reads NEWER than the failure count as evidence', () => {
  assert.equal(
    pumpEvidencedSince(1000, { ...NO_READS, projectReadAt: 1000, constructionStarted: true }),
    false,
    'a read from the same instant is not newer'
  );
  assert.equal(
    pumpEvidencedSince(1000, { ...NO_READS, sessionReadAt: 999, sessionStage: 'pipelineRunning' }),
    false,
    'a session seen before the failure'
  );
});

void test('the hold: an unknown outcome is held until evidence or expiry; a rejection is never held', () => {
  const unknown = { outcome: dispatchOutcomeFor(503, 'x'), holdExpired: false };
  assert.equal(beginHoldFor(null, false), 'none');
  assert.equal(
    beginHoldFor({ outcome: dispatchOutcomeFor(400, 'x'), holdExpired: false }, false),
    'none'
  );
  assert.equal(beginHoldFor(unknown, false), 'held');
  assert.equal(beginHoldFor(unknown, true), 'evidenced');
  assert.equal(beginHoldFor({ ...unknown, holdExpired: true }, false), 'expired');
  assert.equal(
    beginHoldFor({ ...unknown, holdExpired: true }, true),
    'evidenced',
    'evidence after expiry still decides'
  );
  assert.equal(UNKNOWN_OUTCOME_HOLD_MS, 60_000);
});

void test('once the hold expires the alert asks the ruling’s question verbatim', () => {
  const copy = holdExpiredCopy(dispatchOutcomeFor(500, 'request failed with status 500'));
  assert.equal(copy.headline, 'No sign the pump started. Begin again?');
  assert.match(copy.detail, /request failed with status 500/);
  assert.match(copy.detail, /60s/);
});

void test('while held for the pump the button is disabled and names neither Begin nor Resume', () => {
  for (const constructionStarted of [true, false]) {
    const c = beginControlFor({
      constructionStarted,
      projectLoading: false,
      running: false,
      awaitingPump: true,
    });
    assert.equal(c.disabled, true);
    assert.doesNotMatch(c.label, COMMITTED_WORDS);
  }
});
