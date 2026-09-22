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
  | 'testing:plan'
  | 'testing:harness'
  | 'testing:perf'
  | 'testing:systemTest'
  | 'testing:qaProcess';

/**
 * Map a construction row to its artifact classification (the renderer key).
 * Undefined when the row is unclassified — never invent a kind to classify by.
 */
export function classify(row: ConstructionRow): Classification | undefined {
  if (row.kind === undefined) return undefined;
  if (row.kind === 'testing') {
    return `testing:${row.variant ?? 'plan'}`;
  }
  return row.kind;
}
