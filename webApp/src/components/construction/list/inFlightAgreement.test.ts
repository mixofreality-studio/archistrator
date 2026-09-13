/**
 * THE in-flight set's readers agree in EVERY constructed state (the
 * inflight-residual round, after the D1-UI verification).
 *
 * inFlight.test.ts pins the agreement on one hand-built project. The verification
 * then enumerated the states one activity can be in and found 396 of 1920 where the
 * TASKS count disagreed with Begin, the "In flight" chip and "Expand to current
 * phase": its bucketing checked `waiting` before the set, and counted every under-way
 * status as in flight on its own authority. This test enumerates the same axes, so
 * no rule can override the set again without failing a named state:
 *
 *   owed mark × integration-pending × live session × pending probe × row shape ×
 *   evidence origin × "Observed only"
 *
 * In every state it asserts that the chip lists exactly the set, Expand opens
 * exactly the set (and is enabled exactly when it is not empty), the TASKS count is
 * the set's size, Begin is held exactly while the set is not empty, the toggle moves
 * none of them, and the TASKS buckets sum to the activity total.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ConstructionRow, ConstructionRows } from '../../../contracts/types';
import {
  applyToolbarToActivities,
  currentPhaseExpansionIds,
  expandToCurrentPhaseControl,
  inFlightActivityIds,
} from './activityScope.ts';
import { buildActivityTree } from './activityTree.ts';
import { rowsForEvidenceView } from './observedOnly.ts';
import { waitingActivityIds } from './pendingResume.ts';
import { PENDING_PHASES, doneRow } from './pendingResumeFixtures.ts';
import { beginControlFor, beginRunning } from '../lens/beginControl.ts';
import type { OwedMark, OwedMarks } from '../tasks/owedChip.ts';
import { emptyStateCounts } from '../tasks/tasksLensCopy.ts';
import { computeActivityStatuses } from '../../../contracts/constructionAdapters.ts';

const SUBJECT = 'C-subject';
/** A done neighbour, so the total is never 1 and a stray count would show. */
const NEIGHBOUR = 'C-neighbour';
const STARTED = '2026-09-13T10:00:00Z';

const SHAPES = [
  'unrecorded',
  'pickedUp',
  'inConstruction',
  'inReview',
  'integrated',
  'failed',
  'unclassified',
] as const;
type Shape = (typeof SHAPES)[number];

const PENDING = ['none', 'waiting', 'nextInLine'] as const;
type Pending = (typeof PENDING)[number];

const MARKS = ['none', 'gate', 'takeover', 'failed'] as const;
type Mark = (typeof MARKS)[number];

const ORIGINS = ['observed', 'backfilled'] as const;
type Origin = (typeof ORIGINS)[number];

const BOOLS = [false, true] as const;

type Attempt = ConstructionRow['attempts'][number];

function attempt(origin: Origin): Attempt {
  return {
    attemptId: `${SUBJECT}:srs:1`,
    task: 'srs',
    phase: 'requirements',
    attempt: 1,
    outcome: 'passed',
    evidence: { kind: 'git', ref: 'x' },
    provenance: { origin },
  };
}

/** The subject row in one shape, as the wire delivers it: `status` only where the
 *  row is classified and evidenced (wire.mapConstructionRow). */
function subjectRow(shape: Shape, pending: Pending, origin: Origin): ConstructionRow {
  const evidenced: ConstructionRow = {
    activityId: SUBJECT,
    kind: 'service',
    phases: PENDING_PHASES.map((p) => ({ ...p, completed: p.phase === 'requirements' })),
    attempts: [attempt(origin)],
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    worstOrigin: origin,
    startedAt: STARTED,
  };
  let row: ConstructionRow;
  switch (shape) {
    case 'unrecorded':
      row = {
        activityId: SUBJECT,
        kind: 'service',
        phases: [],
        attempts: [],
        classified: true,
        hasBuildEvidence: false,
        recorded: false,
      };
      break;
    case 'pickedUp':
      row = {
        activityId: SUBJECT,
        kind: 'service',
        phases: [],
        attempts: [],
        classified: true,
        hasBuildEvidence: false,
        recorded: true,
        startedAt: STARTED,
      };
      break;
    case 'inConstruction':
      row = { ...evidenced, status: 'in-construction' };
      break;
    case 'inReview':
      row = { ...evidenced, status: 'in-review' };
      break;
    case 'integrated':
      row = {
        ...evidenced,
        status: 'integrated',
        phases: PENDING_PHASES.map((p) => ({ ...p, completed: true })),
      };
      break;
    case 'failed':
      row = { ...evidenced, status: 'failed', failureReason: 'pipelineFailed' };
      break;
    case 'unclassified':
      row = {
        activityId: SUBJECT,
        phases: [],
        attempts: [attempt(origin)],
        classified: false,
        hasBuildEvidence: false,
        recorded: true,
      };
      break;
  }
  if (pending !== 'none') {
    row.pendingResume = {
      fromPhase: 'integration',
      waitsOn: pending === 'waiting' ? [{ id: 'C-dependency', reason: 'notBuilt' }] : [],
    };
  }
  return row;
}

function markFor(mark: Mark): OwedMark | undefined {
  switch (mark) {
    case 'none':
      return undefined;
    case 'gate':
      return { reason: 'gate', lifecyclePhase: 'construction' };
    case 'takeover':
      return { reason: 'takeover' };
    case 'failed':
      return { reason: 'failed' };
  }
}

const NETWORK = {
  criticalPath: [],
  milestones: [],
  dependencies: [
    { activity: SUBJECT, dependsOn: [] },
    { activity: NEIGHBOUR, dependsOn: [] },
  ],
};

const TOOLBAR = {
  scope: 'inFlight',
  kind: 'all',
  layer: 'all',
  search: '',
  sort: 'network',
} as const;

interface Readers {
  set: string[];
  chip: string[];
  expand: string[];
  expandEnabled: boolean;
  tasks: number;
  total: number;
  begin: boolean;
}

/** Every in-flight reader, wired the way ConstructionConsole wires it. */
function readersFor(
  rows: ConstructionRows,
  owed: OwedMarks,
  liveSessionIds: string[],
  pendingProbeIds: string[],
  observedOnly: boolean
): Readers {
  const viewRows = rowsForEvidenceView(rows, observedOnly);
  const set = inFlightActivityIds({ rows, owed, liveSessionIds, pendingProbeIds });
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
  // Every bucket, named, so a new one cannot go uncounted.
  const total =
    counts.eligible +
    counts.waiting +
    counts.inFlight +
    counts.blocked +
    counts.done +
    counts.failed +
    counts.unclassified;
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
    total,
    begin: beginControlFor({
      constructionStarted: true,
      projectLoading: false,
      running: beginRunning({ pending: false, inFlight: set.size > 0, awaitingPickup: false }),
    }).disabled,
  };
}

/** What disagrees in one reading, or nothing. */
function disagreements(r: Readers, activities: number): string[] {
  const out: string[] = [];
  const set = JSON.stringify(r.set);
  if (JSON.stringify(r.chip) !== set) out.push(`chip ${JSON.stringify(r.chip)} ≠ set ${set}`);
  if (JSON.stringify(r.expand) !== set) out.push(`expand ${JSON.stringify(r.expand)} ≠ set ${set}`);
  if (r.expandEnabled !== r.set.length > 0) out.push(`expand enabled ${String(r.expandEnabled)}`);
  if (r.tasks !== r.set.length)
    out.push(`TASKS in flight ${String(r.tasks)} ≠ ${String(r.set.length)}`);
  if (r.begin !== r.set.length > 0) out.push(`Begin held ${String(r.begin)}`);
  if (r.total !== activities)
    out.push(`TASKS buckets sum ${String(r.total)} ≠ ${String(activities)}`);
  return out;
}

void test('Begin, the chip, Expand and the TASKS count agree in every constructed state, and the buckets sum to the total', () => {
  const failures: string[] = [];
  let states = 0;
  for (const shape of SHAPES) {
    for (const pending of PENDING) {
      for (const origin of ORIGINS) {
        for (const mark of MARKS) {
          for (const live of BOOLS) {
            for (const probe of BOOLS) {
              const rows: ConstructionRows = {
                [SUBJECT]: subjectRow(shape, pending, origin),
                [NEIGHBOUR]: doneRow(NEIGHBOUR),
              };
              const m = markFor(mark);
              const owed: OwedMarks = new Map(m !== undefined ? [[SUBJECT, m]] : []);
              const liveIds = live ? [SUBJECT] : [];
              const probeIds = probe ? [SUBJECT] : [];
              const byToggle = BOOLS.map((observedOnly) => {
                states += 1;
                const r = readersFor(rows, owed, liveIds, probeIds, observedOnly);
                const name = `${shape}/${pending}/${origin}/mark=${mark}/live=${String(live)}/probe=${String(probe)}/observedOnly=${String(observedOnly)}`;
                for (const d of disagreements(r, Object.keys(rows).length)) {
                  failures.push(`${name}: ${d}`);
                }
                return r;
              });
              const [off, on] = byToggle;
              if (JSON.stringify(off) !== JSON.stringify(on)) {
                failures.push(
                  `${shape}/${pending}/${origin}/mark=${mark}/live=${String(live)}/probe=${String(probe)}: Observed only moved a reader (${JSON.stringify(off)} vs ${JSON.stringify(on)})`
                );
              }
            }
          }
        }
      }
    }
  }
  // The enumeration cannot shrink silently: every axis above, every value.
  assert.equal(states, SHAPES.length * PENDING.length * ORIGINS.length * MARKS.length * 2 * 2 * 2);
  assert.deepEqual(
    failures.slice(0, 12),
    [],
    `${String(failures.length)} of ${String(states)} readings disagree; the first are shown`
  );
});

// The two override paths the verification found, named, so a regression reads as
// what it is rather than as a count.
void test('an integration-pending row the set holds counts as in flight, not waiting', () => {
  for (const [label, live, probe, mark] of [
    ['a live session', true, false, 'none'],
    ['a pending probe', false, true, 'none'],
    ['a live gate', false, false, 'gate'],
  ] as const) {
    const rows: ConstructionRows = {
      [SUBJECT]: subjectRow('inReview', 'waiting', 'observed'),
      [NEIGHBOUR]: doneRow(NEIGHBOUR),
    };
    const m = markFor(mark);
    const owed: OwedMarks = new Map(m !== undefined ? [[SUBJECT, m]] : []);
    const r = readersFor(rows, owed, live ? [SUBJECT] : [], probe ? [SUBJECT] : [], false);
    assert.deepEqual(r.set, [SUBJECT], label);
    assert.equal(r.tasks, 1, `${label}: the TASKS count is the set`);
    assert.equal(r.begin, true, `${label}: Begin is held`);
  }
});

void test('an under-way status the set does not hold (an owed failure) is not counted in flight', () => {
  for (const shape of ['inConstruction', 'inReview'] as const) {
    const rows: ConstructionRows = {
      [SUBJECT]: subjectRow(shape, 'none', 'observed'),
      [NEIGHBOUR]: doneRow(NEIGHBOUR),
    };
    const owed: OwedMarks = new Map([[SUBJECT, { reason: 'failed' }]]);
    const r = readersFor(rows, owed, [], [], false);
    assert.deepEqual(r.set, [], shape);
    assert.equal(r.tasks, 0, `${shape}: the TASKS count is the set`);
    assert.equal(r.begin, false, `${shape}: Begin is offered`);
  }
});
