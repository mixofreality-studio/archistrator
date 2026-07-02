import type { ConstructionRow } from '../../api/types';

/**
 * The renderer dispatch key: an activity's artifact family. Testing splits by
 * variant because each testing sub-type has a distinct artifact/review surface.
 */
export type Classification =
  | 'service'
  | 'frontend'
  | 'deployment'
  | 'documentation'
  | 'testing:plan'
  | 'testing:harness'
  | 'testing:perf'
  | 'testing:systemTest'
  | 'testing:qaProcess';

/** Map a construction row to its artifact classification (the renderer key). */
export function classify(row: ConstructionRow): Classification {
  if (row.kind === 'testing') {
    return `testing:${row.variant ?? 'plan'}` as Classification;
  }
  return row.kind;
}
