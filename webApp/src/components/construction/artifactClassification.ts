import type { ConstructionRow, TestingVariantName } from '../../contracts/types';

/**
 * The renderer dispatch key: an activity's artifact family. Testing splits by
 * variant because each testing sub-type has a distinct artifact/review surface.
 */
export type Classification =
  | 'service'
  | 'frontend'
  | 'deployment'
  | 'documentation'
  | 'uiDesign'
  | 'integration'
  | 'machineClient'
  // The three design kinds. Like deployment/documentation/integration they have no
  // bespoke renderer, so they fall back to the unknown body rather than showing an
  // empty frame — and today no row carries one at all.
  | 'requirements'
  | 'architecture'
  | 'projectDesign'
  // The three testing variants that remain. `testing:plan` (the system test plan)
  // and `testing:systemTest` (system testing) were retired with deterministic component testing
  // (spec §7): a component's scenario plan and run render on its OWN lifecycle.
  | 'testing:harness'
  | 'testing:perf'
  | 'testing:qaProcess'
  | 'testing:acceptancePlan'
  // The use-case acceptance milestone's run (M-ACCEPT): a recorded acceptance run, judged by its review.
  | 'milestone:acceptance';

/**
 * The classification of a testing row and of a milestone row, by the variant the server's one
 * TestingVariant enum carries for both. Each table is total over the variants, so a new variant
 * fails to compile here, and a pairing the server never sends (a testing row with the milestone's
 * variant) reads as unclassified rather than as a made-up kind.
 */
const TESTING_CLASSIFICATION: Readonly<Record<TestingVariantName, Classification | undefined>> = {
  harness: 'testing:harness',
  perf: 'testing:perf',
  qaProcess: 'testing:qaProcess',
  acceptancePlan: 'testing:acceptancePlan',
  acceptance: undefined,
};

const MILESTONE_CLASSIFICATION: Readonly<Record<TestingVariantName, Classification | undefined>> = {
  harness: undefined,
  perf: undefined,
  qaProcess: undefined,
  acceptancePlan: undefined,
  acceptance: 'milestone:acceptance',
};

/**
 * Map a construction row to its artifact classification (the renderer key).
 * Undefined when the row is unclassified — never invent a kind to classify by —
 * and undefined for a testing row without a variant: there is no plan variant
 * to default to any more.
 */
export function classify(row: ConstructionRow): Classification | undefined {
  if (row.kind === undefined) return undefined;
  if (row.kind === 'testing') {
    return row.variant === undefined ? undefined : TESTING_CLASSIFICATION[row.variant];
  }
  if (row.kind === 'milestone') {
    return row.variant === undefined ? undefined : MILESTONE_CLASSIFICATION[row.variant];
  }
  return row.kind;
}
