/**
 * The unknown body's pure half (taskBriefing.ts) and the provenance/evidence
 * scoping (detailPaneState.ts). Node's type-stripping test runner cannot load a
 * `.tsx` module at all, which is exactly why both are plain `.ts` siblings of the
 * components that render them.
 *
 * Task 13 removed the middle third of this file — the DISPATCH cases over
 * `bodyDispatch.ts` (`detailBodyFor`, `selectedTaskIsGate`,
 * `artifactRendererKeyFor`, `testingArtifactRendererKeyFor`). That module went
 * with the DetailPane it dispatched for; its mapping was PORTED into
 * `components/activity/taskArtifactFor.ts` in Task 9 and is covered by
 * `taskArtifactFor.test.ts`, so the rule is still pinned — on the module that
 * now owns it.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, TaskAttemptRow } from '../../../../contracts/types.ts';
import { worstOriginOf, provenanceBasesOf } from '../../provenanceAxis.ts';
import { evidencePointerFor, provenanceNodeFor, selectedAttemptOf } from '../detailPaneState.ts';
import {
  absenceFor,
  briefingFor,
  KIND_NOUN,
  NO_CURRENT_PHASE_NOTE,
  NO_PROFILE_NOTE,
  noBriefingNoteFor,
  NOT_STARTED_STATEMENT,
  NOT_STARTED_STATEMENT_UNSCOPED,
  unknownStatementFor,
  UNKNOWN_STATEMENT,
  UNKNOWN_STATEMENT_UNSCOPED,
} from './taskBriefing.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function row(overrides: Partial<ConstructionRow> = {}): ConstructionRow {
  return {
    activityId: 'C-x',
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    phases: [],
    attempts: [],
    ...overrides,
  };
}

function attempt(overrides: Partial<TaskAttemptRow> = {}): TaskAttemptRow {
  return {
    attemptId: 'C-x:srs:1',
    task: 'srs',
    phase: 'requirements',
    attempt: 1,
    outcome: 'passed',
    evidence: { kind: '', ref: '' },
    provenance: { origin: 'observed' },
    ...overrides,
  };
}

/** The real shape of a ruling-derived attempt: passed, backfilled, no evidence. */
function rulingAttempt(task: string): TaskAttemptRow {
  return attempt({
    attemptId: `C-artifact-access:${task}:1`,
    task,
    outcome: 'passed',
    evidence: { kind: '', ref: '' },
    provenance: {
      origin: 'backfilled',
      generator: 'cmd/backfill-attempts',
      basis:
        'serviceContracts[artifactAccess] + founderRuling[2026-09-09]=assume any component that is fully implemented is done and reviewed and integrated',
    },
  });
}

// ---------------------------------------------------------------------------
// The kind vocabulary
// ---------------------------------------------------------------------------

// KIND_NOUN is the ONE place a kind is named in a sentence (KindBadge.tsx's
// KIND_META is the chip's label, and a `.tsx` module cannot be loaded here), so it
// is where the vocabulary's totality is checked at run time. TypeScript already
// makes a missing entry a compile error; this catches the other half — an entry
// that exists but says nothing.
void test('every activity kind, the three design kinds included, has a noun', () => {
  const kinds: NonNullable<ConstructionRow['kind']>[] = [
    'service',
    'frontend',
    'testing',
    'deployment',
    'documentation',
    'uiDesign',
    'integration',
    'requirements',
    'architecture',
    'projectDesign',
  ];
  assert.equal(Object.keys(KIND_NOUN).length, kinds.length);
  for (const k of kinds) {
    assert.ok(KIND_NOUN[k].length > 0, `${k} has no noun`);
  }
});

// ---------------------------------------------------------------------------
// The briefing — composed, never authored per task
// ---------------------------------------------------------------------------

void test('composes the gate task briefing from the generated profile alone', () => {
  const r = row({ kind: 'service' });
  const b = briefingFor(r, { lifecyclePhase: 'detailed_design', task: 'designReview' });
  assert.ok(b !== undefined);
  assert.equal(b.title, 'Design Review');
  assert.equal(b.scope, 'task');
  // Gate-ness and the phase's display name are the only inputs to the sentence.
  assert.match(b.whatItIs, /gate task of Detailed Design/);
  assert.equal(b.weight, 'part of Detailed Design · 20% of this activity.');
  assert.equal(b.retryRule, 'A failing Design Review repeats Detailed Design.');
  assert.ok(b.exit.length > 0);
});

void test('a work task names the gate that actually decides its phase', () => {
  const b = briefingFor(row({ kind: 'service' }), {
    lifecyclePhase: 'construction',
    task: 'construction',
  });
  assert.ok(b !== undefined);
  assert.match(b.whatItIs, /work task within Construction/);
  assert.match(b.whatItIs, /exit rests on Code Review/);
  assert.match(b.retryRule, /re-runs Construction alone; Code Review still decides/);
});

void test('reads the per-KIND phase name, not a canonical one', () => {
  // `test_plan` is "Test Plan" for a service and "Flows" for a frontend. A hand
  // table would have one name for both.
  const service = briefingFor(row({ kind: 'service' }), { lifecyclePhase: 'test_plan' });
  const frontend = briefingFor(row({ kind: 'frontend' }), { lifecyclePhase: 'test_plan' });
  assert.equal(service?.title, 'Test Plan');
  assert.equal(frontend?.title, 'Flows');
});

void test('falls back to a phase briefing when no task is selected', () => {
  const b = briefingFor(row({ kind: 'service' }), { lifecyclePhase: 'integration' });
  assert.ok(b !== undefined);
  assert.equal(b.scope, 'lifecyclePhase');
  assert.equal(b.title, 'Integration');
});

void test('an unclassified activity gets no briefing at all, never a borrowed one', () => {
  assert.equal(briefingFor(row({ classified: false }), { task: 'srs' }), undefined);
  assert.equal(briefingFor(undefined, { task: 'srs' }), undefined);
});

void test('the unknown statement carries no error tone and names both real causes', () => {
  assert.equal(unknownStatementFor('task', 0, 'unknown'), UNKNOWN_STATEMENT);
  assert.match(UNKNOWN_STATEMENT, /has not run, or it ran before per-task history was captured/);
  for (const word of ['error', 'failed', 'wrong', 'unable', 'problem']) {
    assert.equal(UNKNOWN_STATEMENT.toLowerCase().includes(word), false, `tone word: ${word}`);
  }
});

// Fix-D concern 1: a NOT STARTED task still said "…or it ran before per-task
// history was captured". That cause exists only where the history cannot say — an
// UNKNOWN selection — so a not-started one states the single cause left.
void test('a not-started selection says it has not run, without the pre-history clause', () => {
  assert.equal(unknownStatementFor('task', 0, 'notStarted'), NOT_STARTED_STATEMENT);
  assert.equal(
    unknownStatementFor('lifecyclePhase', 0, 'notStarted'),
    NOT_STARTED_STATEMENT_UNSCOPED
  );
  assert.equal(unknownStatementFor(undefined, 0, 'notStarted'), NOT_STARTED_STATEMENT_UNSCOPED);
  for (const s of [NOT_STARTED_STATEMENT, NOT_STARTED_STATEMENT_UNSCOPED]) {
    assert.match(s, /^Not started\. Nothing has run/);
    assert.doesNotMatch(s, /per-task history/);
  }
  // The two-cause sentence is kept for UNKNOWN, at every scope.
  assert.equal(unknownStatementFor(undefined, 0, 'unknown'), UNKNOWN_STATEMENT_UNSCOPED);
});

// ---------------------------------------------------------------------------
// ABSENT is not UNKNOWN — the whole point of the sibling body
// ---------------------------------------------------------------------------

// Designer re-check B1: with "Observed only" on, a recorded row whose evidence was
// set aside must not be told it "has not run" — a record exists and is hidden.
void test('a selection whose attempts Observed only hid says so, never "No record"', () => {
  assert.equal(
    unknownStatementFor('task', 10, 'notStarted'),
    'Nothing observed. 10 reconstructed attempts are hidden by Observed only — turn it off to see them.'
  );
  assert.equal(
    unknownStatementFor(undefined, 1, 'unknown'),
    'Nothing observed. 1 reconstructed attempt is hidden by Observed only — turn it off to see it.'
  );
  assert.equal(unknownStatementFor('task', 0, 'unknown'), UNKNOWN_STATEMENT);
  const classified = row({ kind: 'service', hasBuildEvidence: false });
  assert.equal(noBriefingNoteFor(classified, 0), NO_CURRENT_PHASE_NOTE);
  assert.doesNotMatch(noBriefingNoteFor(classified, 3), /Nothing is recorded/);
  assert.match(noBriefingNoteFor(classified, 3), /Nothing observed/);
  // An unclassified row has no profile to brief whatever was hidden.
  assert.equal(noBriefingNoteFor(row({ classified: false }), 3), NO_PROFILE_NOTE);
});

void test('a phase the profile does not carry is absent BY DESIGN, not missing data', () => {
  const absence = absenceFor(row({ kind: 'deployment' }), { lifecyclePhase: 'test_plan' });
  assert.ok(absence !== undefined);
  assert.equal(absence.scope, 'lifecyclePhase');
  assert.equal(
    absence.statement,
    'Deployment activities carry no Test Plan phase. This is by design, not missing data.'
  );
  // It shows what the profile DOES carry, so the reader's next question is answered.
  assert.deepEqual(
    absence.carried.map((p) => p.name),
    ['Provisioning Spec', 'Construction', 'Convergence Verification']
  );
});

void test('a phase the profile DOES carry is never called absent, however empty', () => {
  assert.equal(
    absenceFor(row({ kind: 'deployment' }), { lifecyclePhase: 'construction' }),
    undefined
  );
  // ...and neither is a junk phase id from the URL: we cannot claim a profile
  // deliberately omits something that is not a Method phase at all.
  assert.equal(absenceFor(row({ kind: 'deployment' }), { lifecyclePhase: 'nonsense' }), undefined);
});

void test('a task the profile does not carry is absent too', () => {
  const absence = absenceFor(row({ kind: 'uiDesign' }), { task: 'codeReview' });
  assert.ok(absence !== undefined);
  assert.equal(absence.scope, 'task');
  assert.match(absence.statement, /by design, not missing data/);
});

// ---------------------------------------------------------------------------
// The pane's provenance scoping — the laundering fix
// ---------------------------------------------------------------------------

void test('a ruling-derived task reads RECONSTRUCTED in the pane and quotes its basis', () => {
  const r = row({
    activityId: 'C-artifact-access',
    kind: 'service',
    worstOrigin: 'backfilled',
    attempts: [rulingAttempt('srs'), rulingAttempt('srsReview')],
  });
  const node = provenanceNodeFor(r, { activityId: 'C-artifact-access', task: 'srs' });
  assert.equal(worstOriginOf(node), 'backfilled');
  const bases = provenanceBasesOf(node);
  assert.equal(bases.length, 1);
  assert.match(bases[0] ?? '', /founderRuling\[2026-09-09\]=/);
});

void test('an empty ledger is UNKNOWN provenance in the pane, never "observed"', () => {
  const r = row({ kind: 'service', attempts: [] });
  assert.equal(worstOriginOf(provenanceNodeFor(r, { task: 'srs' })), 'unknown');
  assert.equal(worstOriginOf(provenanceNodeFor(r, {})), 'unknown');
  assert.equal(worstOriginOf(provenanceNodeFor(undefined, {})), 'unknown');
});

void test('an activity-level reading folds in the server roll-up as well as the ledger', () => {
  // The ledger only carries tasks the profile names; an off-profile reconstructed
  // attempt would otherwise hide in the gap between the two.
  const r = row({ kind: 'service', worstOrigin: 'synthesized', attempts: [attempt()] });
  assert.equal(worstOriginOf(provenanceNodeFor(r, {})), 'synthesized');
});

void test('an empty evidence ref renders as absent, never as a target to click', () => {
  const r = row({ kind: 'service', attempts: [rulingAttempt('srs')] });
  const pointer = evidencePointerFor(r, { task: 'srs' });
  assert.deepEqual(pointer, { kind: '', ref: '', present: false });

  const withEvidence = row({
    kind: 'service',
    attempts: [
      attempt({ task: 'detailedDesign', evidence: { kind: 'contract', ref: 'artifactAccess' } }),
    ],
  });
  assert.deepEqual(evidencePointerFor(withEvidence, { task: 'detailedDesign' }), {
    kind: 'contract',
    ref: 'artifactAccess',
    present: true,
  });

  // No attempt at all is a THIRD answer, distinct from an empty ref.
  assert.equal(evidencePointerFor(r, { task: 'stp' }), undefined);
});

void test('selectedAttemptOf honours an explicit attempt, else the highest NUMBER', () => {
  const r = row({
    kind: 'service',
    attempts: [
      attempt({ attemptId: 'a2', attempt: 2 }),
      attempt({ attemptId: 'a1', attempt: 1, outcome: 'failed' }),
    ],
  });
  assert.equal(selectedAttemptOf(r, { task: 'srs' })?.attemptId, 'a2');
  assert.equal(selectedAttemptOf(r, { task: 'srs', attempt: 1 })?.attemptId, 'a1');
  assert.equal(selectedAttemptOf(r, {}), undefined);
});

// ---------------------------------------------------------------------------
// The note in place of the briefing table
// ---------------------------------------------------------------------------

// A planned-no-record row (the server classified it; nothing is recorded, so no
// current phase) selected at activity level resolves no briefing — and the card
// must not then claim the server could not classify it: the list is drawing its
// profile right beside the card.
void test('a classified row with no current phase gets the no-current-phase note, never "could not classify"', () => {
  const planned = row({
    activityId: 'U-SPA-web-client',
    kind: 'frontend',
    hasBuildEvidence: false,
  });
  assert.equal(briefingFor(planned, { activityId: 'U-SPA-web-client' }), undefined);
  assert.equal(noBriefingNoteFor(planned), NO_CURRENT_PHASE_NOTE);
  assert.doesNotMatch(noBriefingNoteFor(planned), /could not classify/);
});

void test('an unclassified row (no profile) keeps the could-not-classify note', () => {
  const unclassified = row({ classified: false, hasBuildEvidence: false });
  assert.equal(noBriefingNoteFor(unclassified), NO_PROFILE_NOTE);
  assert.equal(noBriefingNoteFor(undefined), NO_PROFILE_NOTE);
});
