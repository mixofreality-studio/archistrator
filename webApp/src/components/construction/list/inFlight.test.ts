/**
 * THE in-flight set (final review I1). "In flight" had three definitions: Begin read
 * the raw rows, the "In flight" chip and "Expand to current phase" read the
 * "Observed only" evidence view, and the TASKS empty state counted the probe
 * candidates. Now all four read activityScope.inFlightActivityIds, and these cases
 * pin that they agree — with Observed only on AND off — and that an
 * integration-pending (waiting) row is never in flight.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type {
  ConstructionRow,
  ConstructionRows,
  ConstructionSessionState,
} from '../../../contracts/types';
import {
  applyToolbarToActivities,
  currentPhaseExpansionIds,
  expandToCurrentPhaseControl,
  inFlightActivityIds,
} from './activityScope.ts';
import { buildActivityTree } from './activityTree.ts';
import { rowsForEvidenceView } from './observedOnly.ts';
import { beginControlFor, liveSessionIdsOf } from '../lens/beginControl.ts';
import { owedWorkFor, type SessionsByActivity } from '../tasks/owedWork.ts';
import { owedMarksFor } from '../tasks/owedChip.ts';
import { emptyStateCounts } from '../tasks/tasksLensCopy.ts';
import { computeActivityStatuses } from '../../../contracts/constructionAdapters.ts';
import { waitingActivityIds } from './pendingResume.ts';
import { PENDING_PHASES, doneRow, pendingRow } from './pendingResumeFixtures.ts';

const STARTED = '2026-09-13T10:00:00Z';

/** A row the pump wrote and is building now: stored in-construction, started. */
function running(id: string): ConstructionRow {
  return {
    activityId: id,
    kind: 'service',
    status: 'in-construction',
    phases: PENDING_PHASES.map((p) => ({ ...p, completed: p.phase === 'requirements' })),
    attempts: [],
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    startedAt: STARTED,
  };
}

/** Picked up a moment ago: started, nothing recorded yet, so it reads not started. */
function pickedUp(id: string): ConstructionRow {
  return {
    activityId: id,
    kind: 'service',
    phases: [],
    attempts: [],
    classified: true,
    hasBuildEvidence: false,
    recorded: true,
    startedAt: STARTED,
  };
}

const ROWS: ConstructionRows = {
  'C-running': running('C-running'),
  'C-gate': running('C-gate'),
  'C-pending': pendingRow('C-pending', ['C-running']),
  'C-done': doneRow('C-done'),
  'C-fresh': pickedUp('C-fresh'),
  'C-orphan': pickedUp('C-orphan'),
};

const SESSIONS: SessionsByActivity = {
  // A live gate: owed, awaiting a human — in flight.
  'C-gate': { stage: 'awaitingApproval', view: {} } as unknown as ConstructionSessionState,
  'C-running': { stage: 'pipelineRunning', view: {} } as unknown as ConstructionSessionState,
  // Asked and answered: no session. Not in flight.
  'C-orphan': null,
  // 'C-fresh': not answered yet — pending, so in flight.
};

const NETWORK = {
  criticalPath: [],
  milestones: [],
  dependencies: Object.keys(ROWS).map((id) => ({
    activity: id,
    dependsOn: id === 'C-pending' ? ['C-running'] : [],
  })),
};

const TOOLBAR = {
  scope: 'inFlight',
  kind: 'all',
  layer: 'all',
  search: '',
  sort: 'network',
} as const;

function readers(observedOnly: boolean): {
  set: string[];
  chip: string[];
  expand: string[];
  expandEnabled: boolean;
  tasks: number;
  begin: boolean;
} {
  const viewRows = rowsForEvidenceView(ROWS, observedOnly);
  const work = owedWorkFor({ rows: viewRows, sessions: SESSIONS });
  const owed = owedMarksFor(work.items);
  const set = inFlightActivityIds({
    rows: ROWS,
    owed,
    liveSessionIds: liveSessionIdsOf(SESSIONS),
    pendingProbeIds: work.unchecked.pending,
  });
  const tree = buildActivityTree(Object.values(viewRows ?? {}));
  const byNode = new Map(tree.map((n) => [n.nodeId, n.activityId]));
  const statuses = computeActivityStatuses(
    NETWORK,
    () => undefined,
    undefined,
    'not-started',
    (id) => viewRows?.[id]
  );
  const counts = emptyStateCounts(statuses, {
    inFlight: set,
    waiting: waitingActivityIds(viewRows),
  });
  return {
    set: [...set].sort(),
    chip: applyToolbarToActivities(tree, TOOLBAR, owed, set)
      .map((n) => n.activityId)
      .sort(),
    expand: currentPhaseExpansionIds(tree, owed, set)
      .map((id) => byNode.get(id) ?? id)
      .sort(),
    expandEnabled: expandToCurrentPhaseControl(tree, owed, set).enabled,
    tasks: counts.inFlight,
    begin: beginControlFor({
      constructionStarted: true,
      projectLoading: false,
      running: set.size > 0,
    }).disabled,
  };
}

void test('the in-flight set: running, a live gate, a live session and a pending probe — never waiting', () => {
  const r = readers(false);
  assert.deepEqual(r.set, ['C-fresh', 'C-gate', 'C-running']);
  assert.ok(!r.set.includes('C-pending'), 'an integration-pending row is not in flight');
  assert.ok(!r.set.includes('C-orphan'), 'a probe that answered "no session" is not');
  assert.ok(!r.set.includes('C-done'));
});

void test('the chip, Expand, TASKS and Begin agree — with Observed only on and off', () => {
  const off = readers(false);
  const on = readers(true);
  for (const r of [off, on]) {
    assert.deepEqual(r.chip, r.set, 'the In flight chip is the set');
    assert.deepEqual(r.expand, r.set, 'Expand opens the set');
    assert.equal(r.expandEnabled, r.set.length > 0);
    assert.equal(r.tasks, r.set.length, 'the TASKS count is the set');
    assert.equal(r.begin, r.set.length > 0, 'Begin is held exactly while the set is not empty');
  }
  // The toggle changes what is SHOWN, never what is in flight.
  assert.deepEqual(on, off);
});

void test('with only waiting rows left, nothing is in flight anywhere and Begin is enabled', () => {
  const rows: ConstructionRows = {
    'C-billing-manager': pendingRow('C-billing-manager', ['C-billing-state-access']),
    'C-system-design-manager': pendingRow('C-system-design-manager', ['C-design-health-engine']),
    'C-done': doneRow('C-done'),
  };
  for (const observedOnly of [false, true]) {
    const viewRows = rowsForEvidenceView(rows, observedOnly);
    const set = inFlightActivityIds({ rows });
    const tree = buildActivityTree(Object.values(viewRows ?? {}));
    assert.equal(set.size, 0);
    assert.equal(applyToolbarToActivities(tree, TOOLBAR, undefined, set).length, 0);
    assert.equal(expandToCurrentPhaseControl(tree, undefined, set).enabled, false);
    assert.equal(
      beginControlFor({ constructionStarted: false, projectLoading: false, running: false })
        .disabled,
      false
    );
  }
});
