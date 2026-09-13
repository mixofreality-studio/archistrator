/**
 * Test fixtures for integration-pending rows, shared by the node tests of the
 * waiting state (pendingResume.test.ts), its phase line (waitsOnLine.test.ts) and
 * the TASKS counts (tasks/tasksLensCopy.test.ts). Rows are shaped like the
 * committed 62efcafe read: head-state in-review, a backfilled ledger, no start
 * stamp, and the server's pendingResume.
 */
import type { ConstructionRow, PhaseRow } from '../../../contracts/types';

export const PENDING_PHASES: readonly PhaseRow[] = [
  { phase: 'requirements', weight: 15, label: 'Requirements', completed: true },
  { phase: 'detailed_design', weight: 20, label: 'Detailed Design', completed: true },
  { phase: 'test_plan', weight: 10, label: 'Test Plan', completed: true },
  { phase: 'construction', weight: 40, label: 'Construction', completed: true },
  { phase: 'integration', weight: 15, label: 'Integration', completed: false },
];

export function pendingRow(id: string, waitsOn: readonly string[]): ConstructionRow {
  return {
    activityId: id,
    kind: 'service',
    status: 'in-review',
    phases: [...PENDING_PHASES],
    attempts: [
      {
        attemptId: `${id}:srs:1`,
        task: 'srs',
        phase: 'requirements',
        attempt: 1,
        outcome: 'passed',
        evidence: { kind: 'git', ref: 'x' },
        provenance: { origin: 'backfilled' },
      },
    ],
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    worstOrigin: 'backfilled',
    pendingResume: {
      fromPhase: 'integration',
      waitsOn: waitsOn.map((d) => ({ id: d, reason: 'notBuilt' as const })),
    },
  };
}

export function doneRow(id: string): ConstructionRow {
  return {
    activityId: id,
    kind: 'service',
    status: 'integrated',
    phases: PENDING_PHASES.map((p) => ({ ...p, completed: true })),
    attempts: [],
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
  };
}

/** C-billing-manager as 62efcafe carries it. */
export const BILLING = pendingRow('C-billing-manager', [
  'C-billing-state-access',
  'C-merchant-gateway-access',
]);
