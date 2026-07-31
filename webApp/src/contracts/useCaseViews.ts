/**
 * Pure per-use-case view mapping for the Core Use Cases carousel, plus the
 * use-case → dynamic-view resolution behind the "View call chain" join to the
 * Architecture step. Kept import-free of adapters (type-only imports) so it is
 * directly unit-testable under node --test (adapters.ts' extensionless imports
 * don't resolve there); adapters.toCoreUseCasesView / dynamicViewKeyForUseCase
 * delegate here.
 */
import type { ActivityNodeKind, Classification, EdgeKind, System, UseCaseDecision } from './types';

export interface ActivityNodeView {
  id: string;
  kind: ActivityNodeKind;
  label: string;
  /** The swim-lane (role) this node sits in. */
  lane: string;
}

export interface ActivityEdgeView {
  from: string;
  to: string;
  kind: EdgeKind;
  /** Guard text on a guardedFlow edge (empty otherwise). */
  guard: string;
}

export interface UseCaseView {
  id: string;
  name: string;
  classification: Classification;
  rejectionReason: string;
  /** The WHY-core argument for a CORE use case (the essence-of-the-business
   *  rationale, symmetric to the nonCore rejectionReason). '' when the
   *  committed state predates the field — callers render no chrome then. */
  essenceRationale: string;
  /** The id of the use case this one is a variation of (shares its activity
   *  diagram), or empty when this use case owns its own diagram. */
  variationOf: string;
  /** Distinct swim-lanes, in first-seen order. */
  lanes: string[];
  nodes: ActivityNodeView[];
  edges: ActivityEdgeView[];
}

/** Maps one typed UseCaseDecision into its render-ready activity view. */
export function toUseCaseView(decision: UseCaseDecision): UseCaseView {
  const uc = decision.useCase;
  const activity = uc.activity;
  const rawNodes = activity?.nodes ?? [];
  const rawEdges = activity?.edges ?? [];

  const nodes = rawNodes.map(
    (n): ActivityNodeView => ({
      id: n.id,
      kind: n.kind,
      label: n.label,
      lane: n.roleName.length > 0 ? n.roleName : 'Machine',
    })
  );
  const edges = rawEdges.map(
    (e): ActivityEdgeView => ({ from: e.from, to: e.to, kind: e.kind, guard: e.guard })
  );

  const lanes: string[] = [];
  for (const node of nodes) {
    if (!lanes.includes(node.lane)) lanes.push(node.lane);
  }

  return {
    id: uc.id,
    name: uc.name,
    classification: uc.classification,
    rejectionReason: decision.rejectionReason,
    essenceRationale: decision.essenceRationale ?? '',
    variationOf: uc.variationOf ?? '',
    lanes,
    nodes,
    edges,
  };
}

/**
 * Resolves the System dynamic view that renders the given use case's call chain
 * (every dynamic view carries a useCaseId back-link). Returns the FIRST keyed
 * matching view's key — the ?view= deep-link target on the Architecture step —
 * or undefined when the system model is absent, the id is blank, or no keyed
 * view links back; callers render no affordance then.
 */
export function viewKeyForUseCase(
  model: System | undefined,
  useCaseId: string
): string | undefined {
  if (model === undefined) return undefined;
  const id = useCaseId.trim();
  if (id.length === 0) return undefined;
  return (model.dynamicViews ?? []).find((v) => v.useCaseId === id && v.key.length > 0)?.key;
}

/**
 * The inverse join: the use case a given dynamic view realizes. Used by the
 * Architecture step's dynamic lens to render that use case's activity diagram
 * beside the call chain. Undefined when the model is absent, the key is blank /
 * unknown, or the view carries no back-link (a synthetic view) — the lens then
 * renders the chain alone.
 *
 * (Named for the OWNER rather than `useCaseIdFor…`: a `use`-prefixed export
 * reads as a React hook to the rules-of-hooks lint at every call site.)
 */
export function ownerUseCaseId(model: System | undefined, key: string): string | undefined {
  if (model === undefined) return undefined;
  const k = key.trim();
  if (k.length === 0) return undefined;
  const id = (model.dynamicViews ?? []).find((v) => v.key === k)?.useCaseId ?? '';
  return id.length > 0 ? id : undefined;
}
