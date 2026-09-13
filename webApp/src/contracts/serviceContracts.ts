/**
 * Service-contract lookup — the activity → contract JOIN, and its honest absences.
 *
 * THE JOIN (architect ruling, design-renderer-data.md §3.1)
 * --------------------------------------------------------
 *   activity-list slot  activity.componentId            (kebab, e.g. "construction-manager")
 *   → system slot       component whose id === componentId
 *   → that component's  EXPLICIT contractKey            (camel, e.g. "constructionManager")
 *   → project.serviceContracts[contractKey]
 *
 * Every link is COMMITTED data the component document owns. There is deliberately
 * no camel/kebab conversion anywhere on this path: a naming heuristic would
 * silently mis-join the day a name breaks convention, and it could not tell a
 * missing contract from one that never exists by design.
 *
 * The join used to read `constructionRows[id].produced[kind=service-contract]`.
 * A state reset (f20b7226) emptied every `produced` list and nothing re-creates
 * them, so every activity landed on "no service contract" while 29 contracts sat
 * on the wire. `produced` is not read here any more.
 *
 * THE ABSENCES ARE THREE DIFFERENT FACTS, AND THE TYPE SAYS WHICH
 * --------------------------------------------------------------
 *   missing      a component that is BUILT BY CODE (client / manager / engine /
 *                resourceAccess) with no contractKey, or a contractKey naming no
 *                contract. A real gap a person should fix (C-design-health-engine).
 *   byDesign     a Resource or Utility: provisioned or shared, never contracted
 *                here (the four R-* rows).
 *   noComponent  the activity builds no component at all (N-STP, N-IT).
 *   unresolved   the committed data this join reads is not loaded, or does not
 *                place the activity — the console cannot tell, so it says so.
 */
import type { C4Component } from './adapters';
import type { ActivityItem, ProjectStateWithGit, ServiceContract, ServiceContracts } from './types';

/**
 * Look up a service contract by its contract key.
 * Returns undefined when the project has no contracts or the key is absent
 * (honest-empty — no fabricated default).
 */
export function contractForComponent(
  project: ProjectStateWithGit | undefined,
  contractKey: string
): ServiceContract | undefined {
  if (project?.serviceContracts === undefined || contractKey.length === 0) return undefined;
  return project.serviceContracts[contractKey];
}

/** The component kinds whose contract is built by an activity — a missing one is a gap. */
const CONTRACTED_KINDS: ReadonlySet<string> = new Set([
  'client',
  'manager',
  'engine',
  'resourceAccess',
]);

export type ContractJoin =
  | {
      kind: 'contract';
      componentId: string;
      component: C4Component;
      contractKey: string;
      contract: ServiceContract;
    }
  | {
      kind: 'missing';
      componentId: string;
      component: C4Component;
      /** Set when the component names a key the contracts map does not hold. */
      contractKey?: string;
    }
  | { kind: 'byDesign'; componentId: string; component: C4Component }
  | { kind: 'noComponent' }
  | { kind: 'unresolved'; reason: 'noActivityList' | 'notInActivityList' | 'noSuchComponent' };

export interface ContractJoinInput {
  /** The committed activity-list slot's activities; undefined when not loaded. */
  activities: readonly Pick<ActivityItem, 'name' | 'componentId'>[] | undefined;
  /** The committed system slot's components (toC4View). */
  components: readonly C4Component[];
  /** project.serviceContracts, keyed by contractKey. */
  contracts: ServiceContracts | undefined;
}

/** The activity → component → contractKey → contract join, with its typed absences. */
export function contractJoinFor(input: ContractJoinInput, activityId: string): ContractJoin {
  if (input.activities === undefined) return { kind: 'unresolved', reason: 'noActivityList' };
  const activity = input.activities.find((a) => a.name === activityId);
  if (activity === undefined) return { kind: 'unresolved', reason: 'notInActivityList' };
  const componentId = activity.componentId ?? '';
  if (componentId.length === 0) return { kind: 'noComponent' };
  const component = input.components.find((c) => c.id === componentId);
  if (component === undefined) return { kind: 'unresolved', reason: 'noSuchComponent' };
  if (!CONTRACTED_KINDS.has(component.kind)) {
    // A Resource or Utility. Should one ever carry a contract, show it — the
    // absence is by design, the presence is still a fact.
    const key = component.contractKey;
    const contract = key.length > 0 ? input.contracts?.[key] : undefined;
    return contract !== undefined
      ? { kind: 'contract', componentId, component, contractKey: key, contract }
      : { kind: 'byDesign', componentId, component };
  }
  const key = component.contractKey;
  if (key.length === 0) return { kind: 'missing', componentId, component };
  const contract = input.contracts?.[key];
  if (contract === undefined) return { kind: 'missing', componentId, component, contractKey: key };
  return { kind: 'contract', componentId, component, contractKey: key, contract };
}

/**
 * The activity that builds `componentId`, or undefined. The inverse of the first
 * hop of the join — used to move the selection onto a neighbour's activity.
 */
export function activityForComponent(
  activities: readonly Pick<ActivityItem, 'name' | 'componentId'>[] | undefined,
  componentId: string
): string | undefined {
  return activities?.find((a) => a.componentId === componentId)?.name;
}
