/**
 * Which BUILD-ORDER row of the plan graph an activity sits in (spec §7.3).
 *
 * The server cannot answer this. `LayerForActivity` gives a componentless
 * activity `("", "projectWide")`, so requirements, architecture,
 * projectDesign, N-STP and N-IT all arrive indistinguishable — front end,
 * side lane and system testing collapse into one bucket. The (type, variant)
 * pair is what tells them apart, and it is already on the row.
 *
 * The rows read TOP→DOWN in build order: what nothing depends on is built
 * first (resources), what depends on everything is built last (clients),
 * system testing closes. That is the exact REVERSE of the architecture
 * diagram's order (`activityGraphModel.LAYERED_ROWS` = client-first) — this
 * is a project network, not an architecture, and reusing that list would
 * draw the plan upside down.
 */
import type { ConstructionRow, Layer } from '../../contracts/types.ts';
import type { PlanRow } from './planGraphLayout.ts';

/** The Method layers that map 1:1 onto a build-order row. `utility` derives no activity. */
const LAYER_ROW: Readonly<Partial<Record<Layer, PlanRow>>> = {
  resource: 'resource',
  resourceAccess: 'resourceAccess',
  engine: 'engine',
  manager: 'manager',
  client: 'client',
};

/** Exactly the three members of `ConstructionRow` this rule reads — nothing else. */
export type PlanRowInput = Pick<ConstructionRow, 'kind' | 'variant' | 'layer'>;

/**
 * `undefined` means "this activity has no row" — it is LISTED in the LIST
 * lens (the committed activity list decides what exists) but not DRAWN in the
 * GRAPH, under planCopy.unplacedTilesNote. Inventing a row for it would
 * put a tile somewhere the model never said. An UNCLASSIFIED row (`kind`
 * absent) lands here too, and for the same reason: the server refused to
 * guess its type, so neither may this.
 */
export function planRowFor(input: PlanRowInput): PlanRow | undefined {
  if (
    input.kind === 'requirements' ||
    input.kind === 'architecture' ||
    input.kind === 'projectDesign'
  ) {
    return 'frontEnd';
  }
  if (input.kind === 'testing') {
    if (input.variant === 'plan') return 'sideLane';
    if (input.variant === 'systemTest') return 'systemTesting';
    return undefined; // harness / perf / qaProcess have no band
  }
  return input.layer === undefined ? undefined : LAYER_ROW[input.layer];
}
