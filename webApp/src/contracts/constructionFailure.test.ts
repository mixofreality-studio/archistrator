/// <reference types="node" />
/**
 * The terminal-fail construction row, end to end across the wire boundary.
 *
 * A pump that cannot dispatch an activity now records a DURABLE terminal failure
 * on head-state (ActivityConstructionStatus.BuildStatus = BuildFailed, plus a
 * FailureReason/FailureDetail) precisely so the operator sees it in the console
 * instead of in a warning nobody reads. Two app-side folds used to swallow that:
 * buildStatusRowFromOrdinal collapsed ordinal 3 into 'in-construction', and
 * mapConstructionRow dropped FailureReason/FailureDetail on the floor — so a dead
 * activity rendered as "In construction" forever.
 *
 * These checks pin both halves. Each one FAILS against the old fold, by
 * construction: the row-state assertions name 'failed' explicitly (and assert the
 * absence of the old 'in-construction' answer), and the wire assertions read the
 * two fields off the mapped ConstructionRow.
 *
 * mapProjectState is the seam under test — it is the real path every hook
 * takes to reach constructionRows (mapConstructionRow is exported too, for
 * direct unit tests of the row mapping itself; see constructionAdapters.test.ts).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { components } from './schema.ts';
import { buildStatusRowFromOrdinal } from './enumMappings.ts';
import { mapProjectState } from './wire.ts';

type Schemas = components['schemas'];
type WireConstructionStatus = Schemas['SystemDesignActivityConstructionStatus'];

/** ProjectActivityBuildStatus ordinals, as the Go consts order them. */
const BUILD_IN_CONSTRUCTION = 0;
const BUILD_IN_REVIEW = 1;
const BUILD_INTEGRATED = 2;
const BUILD_FAILED = 3;

/** FailureReason ordinal 6 — the pump could not resolve the activity's component. */
const REASON_COMPONENT_UNRESOLVED = 6;

function wireRow(over: Partial<WireConstructionStatus>): WireConstructionStatus {
  return {
    ActivityID: 'B-01',
    BuildStatus: BUILD_IN_CONSTRUCTION,
    CurrentPhase: 'construction',
    FailureDetail: '',
    FailureReason: 0,
    Kind: 0,
    Phase: 0,
    Phases: null,
    Produced: null,
    Type: 0,
    Variant: 0,
    // These rows are all classified by construction (Type/Kind 0 ⇒ 'service')
    // and carry build evidence — the classification/evidence-gate tests live in
    // constructionAdapters.test.ts, not here.
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    worstOrigin: 'observed',
    layer: '',
    layerBand: '',
    ...over,
  };
}

function wireProjectState(
  rows: Record<string, WireConstructionStatus>
): Schemas['SystemDesignProjectState'] {
  return {
    activityExecution: rows,
    GitRows: {},
    Name: 'fixture',
    Owner: 'fixture-owner',
    Phase: 2,
    PhaseName: 'construction',
    ProjectID: 'fixture-project',
    Research: { sources: null },
    ServiceContracts: {},
    Slots: null,
    Version: 1,
    operatingModel: 'local',
    constructionStarted: false,
    operatorPaused: false,
  };
}

void test('BuildFailed is its own terminal row state, not folded into in-construction', () => {
  const failed = buildStatusRowFromOrdinal(BUILD_FAILED);
  assert.equal(failed, 'failed');
  // The whole point of the change: ordinal 3 must NOT read as a live build.
  assert.notEqual(failed, 'in-construction');
});

void test('the other three build-status ordinals are unchanged', () => {
  assert.equal(buildStatusRowFromOrdinal(BUILD_IN_CONSTRUCTION), 'in-construction');
  assert.equal(buildStatusRowFromOrdinal(BUILD_IN_REVIEW), 'in-review');
  assert.equal(buildStatusRowFromOrdinal(BUILD_INTEGRATED), 'integrated');
  // Out-of-range (an ordinal a newer server invented) still degrades to a live build.
  assert.equal(buildStatusRowFromOrdinal(99), 'in-construction');
});

void test('a failed row carries its failureReason and failureDetail across the wire', () => {
  const detail = 'B-07 names component OrderEngine, absent from the committed systemDesign';
  const state = mapProjectState(
    wireProjectState({
      'B-07': wireRow({
        ActivityID: 'B-07',
        BuildStatus: BUILD_FAILED,
        FailureReason: REASON_COMPONENT_UNRESOLVED,
        FailureDetail: detail,
      }),
    })
  );

  const row = state.constructionRows?.['B-07'];
  assert.ok(row !== undefined, 'expected a mapped construction row for B-07');
  assert.equal(row.status, 'failed');
  assert.equal(row.failureReason, 'componentUnresolved');
  assert.equal(row.failureDetail, detail);
});

void test('a healthy row carries no phantom failure', () => {
  const state = mapProjectState(
    wireProjectState({
      'B-02': wireRow({ ActivityID: 'B-02', BuildStatus: BUILD_IN_REVIEW }),
    })
  );

  const row = state.constructionRows?.['B-02'];
  assert.ok(row !== undefined, 'expected a mapped construction row for B-02');
  assert.equal(row.status, 'in-review');
  // The wire always sends the zero-value reason + an empty detail; neither may
  // reach the view model and decorate a live activity with a failure banner.
  assert.equal(row.failureReason, undefined);
  assert.equal(row.failureDetail, undefined);
});

void test('a failed row with an empty detail still names its reason', () => {
  const state = mapProjectState(
    wireProjectState({
      'B-09': wireRow({
        ActivityID: 'B-09',
        BuildStatus: BUILD_FAILED,
        FailureReason: 1,
        FailureDetail: '',
      }),
    })
  );

  const row = state.constructionRows?.['B-09'];
  assert.ok(row !== undefined, 'expected a mapped construction row for B-09');
  assert.equal(row.status, 'failed');
  assert.equal(row.failureReason, 'pipelineFailed');
  assert.equal(row.failureDetail, undefined);
});

// Fix round B, item 7: Begin versus Resume is the server's constructionStarted,
// passed through as-is — never re-derived in the SPA from rows or attempts.
void test('mapProjectState passes the server’s constructionStarted through', () => {
  const started = mapProjectState({ ...wireProjectState({}), constructionStarted: true });
  assert.equal(started.constructionStarted, true);
  // A project whose rows carry reconstructed attempts still reads false when the
  // server says so: the mapper does not second-guess it.
  const notStarted = mapProjectState({
    ...wireProjectState({ 'C-x': wireRow({ ActivityID: 'C-x', recorded: true }) }),
    constructionStarted: false,
  });
  assert.equal(notStarted.constructionStarted, false);
});

void test('pending operator notes cross the wire as a count: undelivered, never a skip note', () => {
  const at = '2026-09-14T10:00:00Z';
  const state = mapProjectState(
    wireProjectState({
      'B-03': wireRow({
        ActivityID: 'B-03',
        operatorNotes: [
          { noteId: 'n1', kind: 1, text: 'tighten it', recordedAt: at },
          { noteId: 'n2', kind: 5, text: 'skip it', recordedAt: at },
          {
            noteId: 'n3',
            kind: 2,
            text: 'retry',
            recordedAt: at,
            deliveredToAttemptId: 'B-03:srs:1',
            deliveredAt: at,
          },
        ],
      }),
      'B-04': wireRow({ ActivityID: 'B-04' }),
      'B-05': wireRow({ ActivityID: 'B-05', operatorNotes: null }),
    })
  );
  const b3 = state.constructionRows?.['B-03'];
  const b4 = state.constructionRows?.['B-04'];
  const b5 = state.constructionRows?.['B-05'];
  assert.ok(b3 !== undefined && b4 !== undefined && b5 !== undefined, 'expected three mapped rows');
  assert.equal(b3.pendingOperatorNotes, 1);
  // Zero is ABSENT, like every other zero-valued field on the row.
  assert.equal(b4.pendingOperatorNotes, undefined);
  assert.equal(b5.pendingOperatorNotes, undefined);
  assert.equal('pendingOperatorNotes' in b4, false);
});
