import type { ConstructionRow } from '../../contracts/types';

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
  | 'testing:qaProcess';

/**
 * Map a construction row to its artifact classification (the renderer key).
 * Undefined when the row is unclassified — never invent a kind to classify by —
 * and undefined for a testing row without a variant: there is no plan variant
 * to default to any more.
 */
export function classify(row: ConstructionRow): Classification | undefined {
  if (row.kind === undefined) return undefined;
  if (row.kind === 'testing') {
    return row.variant === undefined ? undefined : `testing:${row.variant}`;
  }
  return row.kind;
}
