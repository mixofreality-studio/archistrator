import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ConstructionRow } from '../../../contracts/types';
import {
  anyRowInFlight,
  awaitingPickup,
  beginControlFor,
  beginHoldFor,
  beginRunning,
  CASCADE_POLL_MS,
  consolePollMs,
  constructionInFlight,
  dispatchOutcomeCopy,
  dispatchOutcomeFor,
  failureLeavesMemory,
  holdExpiredCopy,
  IN_FLIGHT_POLL_MS,
  newestLiveSession,
  notStartedActivities,
  pickupEvidencedSince,
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

// A Begin on a project not yet started: the read at the failure said `false`.
const NO_READS = {
  projectRequestedAt: 0,
  constructionStarted: undefined,
  startedAtFailure: false,
  rowsInFlight: false,
  sessionRequestedAt: 0,
  sessionStage: undefined,
} as const;

void test('pump evidence: a newer read that shows an activity in flight, even with constructionStarted false', () => {
  assert.equal(
    pumpEvidencedSince(1000, {
      ...NO_READS,
      projectRequestedAt: 1001,
      constructionStarted: false,
      rowsInFlight: true,
    }),
    true
  );
  assert.equal(
    pumpEvidencedSince(1000, { ...NO_READS, projectRequestedAt: 1000, rowsInFlight: true }),
    false,
    'a read from the same instant is not newer'
  );
});

void test('a newer read that says "not started", with no live session, is NOT pump evidence', () => {
  assert.equal(pumpEvidencedSince(1000, NO_READS), false, 'no read yet');
  assert.equal(
    pumpEvidencedSince(1000, { ...NO_READS, projectRequestedAt: 5000, constructionStarted: false }),
    false
  );
  assert.equal(
    pumpEvidencedSince(1000, { ...NO_READS, sessionRequestedAt: 5000, sessionStage: undefined }),
    false,
    'a probe that established no session exists'
  );
});

void test('pump evidence: a newer read says constructionStarted, or a newer probe shows a live session', () => {
  assert.equal(
    pumpEvidencedSince(1000, { ...NO_READS, projectRequestedAt: 1001, constructionStarted: true }),
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
      pumpEvidencedSince(1000, { ...NO_READS, sessionRequestedAt: 1001, sessionStage: stage }),
      true,
      stage
    );
  }
  for (const stage of ['exited', 'paused', 'unknown'] as const) {
    assert.equal(
      pumpEvidencedSince(1000, { ...NO_READS, sessionRequestedAt: 1001, sessionStage: stage }),
      false,
      `${stage} is not a running pump`
    );
  }
});

void test('only reads NEWER than the failure count as evidence', () => {
  assert.equal(
    pumpEvidencedSince(1000, { ...NO_READS, projectRequestedAt: 1000, constructionStarted: true }),
    false,
    'a read from the same instant is not newer'
  );
  assert.equal(
    pumpEvidencedSince(1000, {
      ...NO_READS,
      sessionRequestedAt: 999,
      sessionStage: 'pipelineRunning',
    }),
    false,
    'a session seen before the failure'
  );
  assert.equal(
    pumpEvidencedSince(1000, {
      ...NO_READS,
      sessionRequestedAt: 1000,
      sessionStage: 'pipelineRunning',
    }),
    false,
    'a session probe from the same instant is not newer'
  );
});

void test('I1 (fix-G review): constructionStarted is evidence only if it was false when the dispatch failed', () => {
  const startedRead = { ...NO_READS, projectRequestedAt: 5000, constructionStarted: true };
  assert.equal(
    pumpEvidencedSince(1000, { ...startedRead, startedAtFailure: false }),
    true,
    'a Begin: not started at the failure, started since'
  );
  assert.equal(
    pumpEvidencedSince(1000, { ...startedRead, startedAtFailure: true }),
    false,
    'a Resume: already started before the dispatch, so it proves nothing'
  );
  assert.equal(
    pumpEvidencedSince(1000, { ...startedRead, startedAtFailure: undefined }),
    false,
    'no read at the failure: nothing to have changed from'
  );
  // On a project already started, what changed after the dispatch still counts.
  assert.equal(
    pumpEvidencedSince(1000, { ...startedRead, startedAtFailure: true, rowsInFlight: true }),
    true,
    'work in flight'
  );
  assert.equal(
    pumpEvidencedSince(1000, {
      ...NO_READS,
      startedAtFailure: true,
      sessionRequestedAt: 5000,
      sessionStage: 'pipelineRunning',
    }),
    true,
    'a live session'
  );
});

// ---------------------------------------------------------------------------
// After a SUCCESS: the pickup hold (fix H).
// ---------------------------------------------------------------------------

void test('pickup evidence: a newer read showing work in flight, or a newer live session; never constructionStarted alone', () => {
  assert.equal(
    pickupEvidencedSince(1000, { ...NO_READS, projectRequestedAt: 1001, rowsInFlight: true }),
    true
  );
  assert.equal(
    pickupEvidencedSince(1000, {
      ...NO_READS,
      sessionRequestedAt: 1001,
      sessionStage: 'pipelineRunning',
    }),
    true
  );
  assert.equal(
    pickupEvidencedSince(1000, { ...NO_READS, projectRequestedAt: 1000, rowsInFlight: true }),
    false,
    'a read from the same instant is not newer'
  );
  assert.equal(
    pickupEvidencedSince(1000, {
      ...NO_READS,
      sessionRequestedAt: 999,
      sessionStage: 'pipelineRunning',
    }),
    false,
    'a session seen before the success'
  );
  assert.equal(
    pickupEvidencedSince(1000, { ...NO_READS, sessionRequestedAt: 1001, sessionStage: 'exited' }),
    false,
    'an exited session is no pump'
  );
  // A Resume's read already said constructionStarted before the dispatch: it is
  // not the pickup. Pump evidence after a FAILURE still counts it.
  const startedOnly = { ...NO_READS, projectRequestedAt: 5000, constructionStarted: true };
  assert.equal(pickupEvidencedSince(1000, startedOnly), false);
  assert.equal(pumpEvidencedSince(1000, startedOnly), true);
});

void test('a success awaits its pickup until a newer read shows it; no record, no hold', () => {
  assert.equal(awaitingPickup(null, NO_READS), false, 'nothing dispatched');
  assert.equal(awaitingPickup({ at: 1000 }, NO_READS), true, 'no read since');
  assert.equal(
    awaitingPickup({ at: 1000 }, { ...NO_READS, projectRequestedAt: 2000, rowsInFlight: false }),
    true,
    'a newer read with nothing in flight: the gap before the pickup'
  );
  assert.equal(
    awaitingPickup({ at: 1000 }, { ...NO_READS, projectRequestedAt: 900, rowsInFlight: true }),
    true,
    'work seen in flight BEFORE the success is not its pickup'
  );
  assert.equal(
    awaitingPickup({ at: 1000 }, { ...NO_READS, projectRequestedAt: 2000, rowsInFlight: true }),
    false,
    'picked up'
  );
});

// ---------------------------------------------------------------------------
// The label follows STATE, not timers (fix-F review, root-cause ruling).
// ---------------------------------------------------------------------------

void test('in flight by state: a row running or awaiting a human, or a live session', () => {
  const at = (status: ConstructionRow['status']): ConstructionRow =>
    row({ hasBuildEvidence: true, recorded: true, ...(status !== undefined ? { status } : {}) });
  const none = { sessionStage: undefined };
  assert.equal(constructionInFlight({ rows: { a: at('in-construction') }, ...none }), true);
  assert.equal(constructionInFlight({ rows: { a: at('in-review') }, ...none }), true, 'in review');
  for (const status of ['integrated', 'failed', undefined] as const) {
    assert.equal(constructionInFlight({ rows: { a: at(status) }, ...none }), false, String(status));
  }
  assert.equal(
    constructionInFlight({ rows: { a: row({ status: 'in-construction' }) }, ...none }),
    false,
    'no build evidence: not started, whatever the coarse status says'
  );
  assert.equal(
    constructionInFlight({
      rows: { a: row({ classified: false, hasBuildEvidence: true, status: 'in-construction' }) },
      ...none,
    }),
    false,
    'unclassified: unknown, not in flight'
  );
  assert.equal(constructionInFlight({ rows: undefined, ...none }), false, 'no read');
  // One in-flight row among settled ones is enough.
  assert.equal(anyRowInFlight({ a: at('integrated'), b: at('in-construction'), c: row({}) }), true);
  // A live session counts on its own, with no row in flight: it is the pump.
  for (const stage of [
    'dispatching',
    'pipelineRunning',
    'reviewing',
    'awaitingTakeover',
    'awaitingApproval',
  ] as const) {
    assert.equal(constructionInFlight({ rows: {}, sessionStage: stage }), true, stage);
  }
  for (const stage of ['exited', 'paused', 'unknown'] as const) {
    assert.equal(constructionInFlight({ rows: {}, sessionStage: stage }), false, stage);
  }
});

// Tasks merge review I1: a probe candidate with no answer is not "nothing in flight".
void test('a PENDING probe candidate is in flight; an answered one is not', () => {
  const none = { rows: {}, sessionStage: undefined };
  assert.equal(constructionInFlight({ ...none, pendingProbes: 1 }), true, 'one unanswered');
  assert.equal(constructionInFlight({ ...none, pendingProbes: 3 }), true, 'several');
  assert.equal(constructionInFlight({ ...none, pendingProbes: 0 }), false, 'all answered');
  assert.equal(constructionInFlight(none), false, 'no candidates at all');
  // An answered probe that found no live session settles it: the stage decides.
  assert.equal(
    constructionInFlight({ ...none, pendingProbes: 0, sessionStage: 'exited' }),
    false,
    'answered, and the session has ended'
  );
});

// Tasks merge-2 ruling (a), fix I: while a probe keeps FAILING, the true state is
// unknown, so the button checks rather than claiming a running pump.
void test('a probe that keeps failing reads "Checking construction…", disabled, for Begin and Resume alike', () => {
  for (const constructionStarted of [true, false]) {
    const c = beginControlFor({
      constructionStarted,
      projectLoading: false,
      running: false,
      probesFailing: true,
    });
    assert.equal(c.label, 'Checking construction…', `started: ${String(constructionStarted)}`);
    assert.equal(c.disabled, true);
    assert.doesNotMatch(c.label, /Construction running…/);
    assert.doesNotMatch(c.label, COMMITTED_WORDS);
  }
  // What the state DOES show still wins: work in flight reads running.
  const running = beginControlFor({
    constructionStarted: true,
    projectLoading: false,
    running: true,
    probesFailing: true,
  });
  assert.equal(running.label, 'Construction running…');
  // The probes settle: the read decides again.
  const settled = beginControlFor({
    constructionStarted: true,
    projectLoading: false,
    running: false,
    probesFailing: false,
  });
  assert.equal(settled.label, 'Resume construction');
  assert.equal(settled.disabled, false);
});

// Tasks-lens merge round: "awaiting" comes from the live owed set (Q4), never
// head-state, and the Begin label reads it.
void test('in flight reads the OWED set: a live gate or a steer is in flight, a recorded failure is not', () => {
  const inConstruction = row({
    activityId: 'a',
    hasBuildEvidence: true,
    recorded: true,
    status: 'in-construction',
  });
  const none = { sessionStage: undefined };
  const owedAs = (reason: 'gate' | 'takeover' | 'failed'): Map<string, { reason: typeof reason }> =>
    new Map([['a', { reason }]]);
  assert.equal(constructionInFlight({ rows: { a: inConstruction }, ...none }), true);
  assert.equal(
    constructionInFlight({ rows: { a: inConstruction }, owed: owedAs('gate'), ...none }),
    true,
    'a live gate: awaiting a human is in flight'
  );
  assert.equal(
    constructionInFlight({ rows: { a: inConstruction }, owed: owedAs('takeover'), ...none }),
    true,
    'a steer: awaiting a human is in flight'
  );
  assert.equal(
    constructionInFlight({ rows: { a: inConstruction }, owed: owedAs('failed'), ...none }),
    false,
    'the pump stopped on a recorded failure: Begin/Resume may be offered'
  );
  // Keyed by activity: another activity's failure does not settle this one.
  assert.equal(
    anyRowInFlight({ a: inConstruction }, new Map([['b', { reason: 'failed' as const }]])),
    true
  );
  // A live session is still the pump, whatever the owed set says.
  assert.equal(
    constructionInFlight({
      rows: { a: inConstruction },
      owed: owedAs('failed'),
      sessionStage: 'pipelineRunning',
    }),
    true
  );
});

void test('the newest-requested LIVE session among the probes, or none', () => {
  assert.equal(newestLiveSession([]), undefined);
  assert.equal(
    newestLiveSession([
      { stage: null, requestedAt: 9 },
      { stage: undefined, requestedAt: 9 },
      { stage: 'exited', requestedAt: 9 },
      { stage: 'paused', requestedAt: 9 },
      { stage: 'unknown', requestedAt: 9 },
    ]),
    undefined,
    'no session, no answer, or a stage with no pump: none is live'
  );
  assert.deepEqual(
    newestLiveSession([
      { stage: 'pipelineRunning', requestedAt: 5 },
      { stage: 'exited', requestedAt: 50 },
      { stage: 'awaitingApproval', requestedAt: 20 },
      { stage: 'reviewing', requestedAt: 10 },
    ]),
    { stage: 'awaitingApproval', requestedAt: 20 },
    'a newer read that is not live never wins'
  );
  // Judged by pumpEvidencedSince, the newest live read stands for them all.
  const live = newestLiveSession([
    { stage: 'pipelineRunning', requestedAt: 999 },
    { stage: 'dispatching', requestedAt: 1001 },
  ]);
  assert.equal(
    pumpEvidencedSince(1000, {
      ...NO_READS,
      sessionRequestedAt: live?.requestedAt ?? 0,
      sessionStage: live?.stage,
    }),
    true
  );
});

void test('running is a pending dispatch, work in flight by state, or an awaited pickup, and nothing else', () => {
  const run = (pending: boolean, inFlight: boolean, awaitingPickup: boolean): boolean =>
    beginRunning({ pending, inFlight, awaitingPickup });
  assert.equal(run(true, false, false), true, 'pending');
  assert.equal(run(false, true, false), true, 'in flight by state');
  assert.equal(run(false, false, true), true, 'a success still awaiting its pickup');
  assert.equal(run(false, false, false), false, 'idle: the read decides');
  // A running control is disabled and never claims Begin or Resume, whatever the
  // project read or the hold says.
  for (const awaitingPump of [true, false]) {
    const c = beginControlFor({
      constructionStarted: false,
      projectLoading: false,
      running: true,
      awaitingPump,
    });
    assert.equal(c.label, 'Construction running…');
    assert.equal(c.disabled, true);
  }
});

void test('the poll: fast while pending, awaiting the pump or cascading; slow while in flight; else off', () => {
  const poll = (
    pending: boolean,
    awaitsPump: boolean,
    cascading: boolean,
    inFlight: boolean
  ): number | false => consolePollMs({ pending, awaitsPump, cascading, inFlight });
  assert.equal(poll(true, false, false, false), CASCADE_POLL_MS, 'a pending dispatch');
  assert.equal(
    poll(false, true, false, false),
    CASCADE_POLL_MS,
    'a remount: memory awaits the pump'
  );
  assert.equal(poll(false, false, true, false), CASCADE_POLL_MS, 'a fresh Begin');
  assert.equal(poll(false, false, true, true), CASCADE_POLL_MS, 'cascading wins the cadence');
  // The watchdog cleared `cascading`: the poll slows, and does not stop, while
  // the state still shows work in flight.
  assert.equal(poll(false, false, false, true), IN_FLIGHT_POLL_MS);
  assert.equal(poll(false, false, false, false), false, 'idle');
});

void test('an evidenced failure leaves memory once nothing is in flight; nothing else does', () => {
  assert.equal(failureLeavesMemory('evidenced', false), true);
  assert.equal(failureLeavesMemory('evidenced', true), false, 'still running: keep it');
  for (const hold of ['none', 'held', 'expired'] as const) {
    for (const inFlight of [true, false]) {
      assert.equal(failureLeavesMemory(hold, inFlight), false, `${hold}, ${String(inFlight)}`);
    }
  }
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
