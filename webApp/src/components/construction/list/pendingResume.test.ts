/**
 * INTEGRATION-PENDING rows (architect (D), D.3; designer final pass blocker and
 * item 3): the server's pendingResume makes a row WAITING — not in flight, in every
 * lens and the pane — Begin is no longer held by it, and the owed set and the TASKS
 * lens never treat it as owed.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ConstructionRow, ConstructionRows, PhaseRow } from '../../../contracts/types';
import { pendingChipLabel, pendingSentence, waitsOnText } from './pendingResume.ts';
import { activityChipLabel, activityRowState, chipFor } from './activityRowPresentation.ts';
import {
  expandToCurrentPhaseControl,
  inFlightActivityIds,
  isInFlight,
  rowIsInFlight,
  scopePredicate,
} from './activityScope.ts';
import { buildActivityTree } from './activityTree.ts';
import { observedOnlyRow } from './observedOnly.ts';
import { taskDetailStateFor } from '../detail/detailPaneState.ts';
import { anyRowInFlight, beginControlFor, notStartedActivities } from '../lens/beginControl.ts';
import { owedWorkFor, probeCandidatesFor } from '../tasks/owedWork.ts';
import { owedMarksFor } from '../tasks/owedChip.ts';
import { computeActivityStatuses } from '../../../contracts/constructionAdapters.ts';
import { BILLING, doneRow, pendingRow } from './pendingResumeFixtures.ts';

// --- the copy --------------------------------------------------------------------

void test('the copy: the chip and the pane sentence', () => {
  assert.equal(pendingChipLabel(BILLING), 'Integration pending');
  assert.equal(
    pendingSentence(BILLING),
    'Integration pending — waits on C-billing-state-access (not built), C-merchant-gateway-access (not built)'
  );
  assert.equal(pendingSentence(pendingRow('C-next', [])), 'Integration pending — next in line');
  // Every reason reads as its own words.
  assert.equal(
    waitsOnText(
      {
        fromPhase: 'integration',
        waitsOn: [
          { id: 'a', reason: 'notBuilt' },
          { id: 'b', reason: 'builtNotIntegrated' },
          { id: 'M3', reason: 'milestoneNotReached' },
          { id: 'ghost', reason: 'unresolved' },
        ],
      },
      true
    ),
    'waits on a (not built), b (built but not integrated), M3 (not reached), ghost (unresolved)'
  );
  // A row that is not pending says nothing.
  assert.equal(pendingChipLabel(doneRow('x')), undefined);
  assert.equal(pendingSentence(doneRow('x')), undefined);
});

void test('the fromPhase label comes from the row, and falls back to the wire name', () => {
  const noPhases: ConstructionRow = { ...BILLING, phases: [] as PhaseRow[] };
  assert.equal(pendingChipLabel(noPhases), 'integration pending');
});

// --- the state: waiting, not in flight, in every lens and the pane ---------------

void test('a pending row reads WAITING — never running — in the list, the graph and the pane', () => {
  assert.equal(activityRowState(BILLING), 'waiting');
  assert.equal(taskDetailStateFor(BILLING, {}), 'waiting');
  // A PHASE selection reads the row's own state, as it does for every row.
  assert.equal(taskDetailStateFor(BILLING, { lifecyclePhase: 'integration' }), 'waiting');
  // A TASK selection is about that task's own attempt.
  assert.equal(taskDetailStateFor(BILLING, { task: 'srs' }), 'passed');
  // The waiting chip, and the phase the pane names.
  assert.equal(chipFor('waiting')?.state, 'waiting');
  assert.equal(activityChipLabel(BILLING, undefined), 'Integration pending');
  // The GRAPH's own hover-card chip used to be cross-asserted here
  // (`laneChipFor(BILLING, undefined)` === the same label, size 'xs', state
  // 'waiting'), so the list's wording and the graph's could not drift apart.
  // Task 13 deleted the graph lens and its hover card, so the cross-check has no
  // second party left; `activityChipLabel` above is the one remaining source of
  // that label and is asserted on its own.
  // Without pendingResume the same head-state reads running — the bug this fixes.
  const inReview: ConstructionRow = { ...BILLING };
  delete inReview.pendingResume;
  assert.equal(activityRowState(inReview), 'running');
});

void test('a pending row is not in flight: the chip scope, Expand to current phase, and Begin', () => {
  const rows: ConstructionRows = {
    'C-billing-manager': BILLING,
    'C-system-design-manager': pendingRow('C-system-design-manager', ['C-design-health-engine']),
    'C-billing-engine': doneRow('C-billing-engine'),
  };
  assert.equal(rowIsInFlight(BILLING), false);
  assert.equal(anyRowInFlight(rows), false);
  assert.equal(inFlightActivityIds({ rows }).size, 0);
  // Designer final pass, item 3: it read "Open the 2 activities in construction…".
  const tree = buildActivityTree(Object.values(rows));
  assert.equal(tree.filter((n) => isInFlight(n)).length, 0);
  assert.equal(tree.filter((n) => scopePredicate('inFlight', n)).length, 0);
  assert.equal(expandToCurrentPhaseControl(tree).enabled, false);
  // Begin reads the server's constructionStarted (false on 62efcafe), enabled.
  assert.deepEqual(
    beginControlFor({
      constructionStarted: false,
      projectLoading: false,
      running: inFlightActivityIds({ rows }).size > 0,
    }),
    { label: 'Begin construction', disabled: false, busy: false, verb: 'Begin' }
  );
  // A pending row is recorded, so a Begin does not list it as a fresh start.
  assert.deepEqual(
    notStartedActivities(rows, () => undefined),
    []
  );
});

void test('a pending row is never owed and never probed: the TASKS lens has nothing for it', () => {
  const rows: ConstructionRows = { 'C-billing-manager': BILLING };
  assert.deepEqual(probeCandidatesFor(rows), []);
  const work = owedWorkFor({ rows, sessions: {} });
  assert.deepEqual(work.items, []);
  assert.deepEqual(work.unchecked, { pending: [], errored: [] });
  assert.equal(owedMarksFor(work.items).size, 0);
});

// --- readiness ----------------------------------------------------------------------

void test('readiness comes from the server’s waitsOn: blocked while it waits, eligible when next', () => {
  const rows: ConstructionRows = {
    'C-billing-manager': BILLING,
    'C-next': pendingRow('C-next', []),
    // Planned, no record: classified, nothing recorded, so no status.
    'C-billing-state-access': {
      activityId: 'C-billing-state-access',
      kind: 'service',
      phases: [],
      attempts: [],
      classified: true,
      hasBuildEvidence: false,
      recorded: false,
    },
  };
  const statuses = computeActivityStatuses(
    {
      criticalPath: [],
      milestones: [],
      dependencies: [
        // C-next's own network dependency is unmet; the server's empty waitsOn wins,
        // because the pump's rule (not this mirror's done set) decides readiness.
        { activity: 'C-billing-manager', dependsOn: ['C-billing-state-access'] },
        { activity: 'C-next', dependsOn: ['C-billing-state-access'] },
        { activity: 'C-billing-state-access', dependsOn: [] },
      ],
    },
    () => undefined,
    undefined,
    'not-started',
    (id) => rows[id]
  );
  assert.equal(statuses.get('C-billing-manager'), 'blocked');
  assert.equal(statuses.get('C-next'), 'eligible');
});

// --- "Observed only" ----------------------------------------------------------------

void test('"Observed only" sets the claim aside with the ledger it came from', () => {
  const observed = observedOnlyRow(BILLING);
  assert.equal(observed.pendingResume, undefined);
  assert.equal(activityRowState(observed), 'notStarted');
});
