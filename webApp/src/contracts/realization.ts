/**
 * Pure per-use-case call-chain realization: joins a System's step-keyed
 * DynamicView (dynamicViews[].steps, one CallStep per authored activity node)
 * onto a use case, plus the deterministic DFS linearization of those steps
 * over the use case's activity graph (global sequencing for the Architecture
 * step-through — see linearizeSteps below; adapters.ts' toDynamicView is the
 * only caller, assigning the final global `seq`).
 *
 * Kept import-free of adapters (type-only imports from './types') so it is
 * directly unit-testable under `node --test` (adapters.ts' extensionless
 * imports don't resolve there — see useCaseViews.ts for the same discipline).
 * This is also WHY linearizeSteps lives here rather than in adapters.ts: it is
 * the most complex logic Tasks 9-11 consume, and needs its own leaf-module
 * test coverage.
 */
import type { ActivityDiagram, ActivityEdge, CallMode, System, UseCase } from './types';

/** One realized call: a Relationship stripped to the fields the overlay needs. */
export interface RealizedCall {
  from: string;
  to: string;
  mode: CallMode;
  label: string;
}

/** One activity node's realized calls (authored order, unlinearized). */
export interface RealizedStep {
  nodeId: string;
  calls: RealizedCall[];
}

/**
 * Resolves the System's dynamic view linked to `useCaseId` (first match, same
 * convention as viewKeyForUseCase/useCaseViews.ts) and indexes its steps by
 * activityNodeId. A node absent from the returned map has no realized calls —
 * callers render no overlay for it. Empty map when the system is absent, the id
 * is blank, no view links back, or the view carries no steps.
 */
export function realizationByNode(
  system: System | undefined,
  useCaseId: string
): Map<string, RealizedStep> {
  const map = new Map<string, RealizedStep>();
  if (system === undefined) return map;
  const id = useCaseId.trim();
  if (id.length === 0) return map;

  const view = (system.dynamicViews ?? []).find((v) => v.useCaseId === id);
  if (view === undefined) return map;

  for (const step of view.steps ?? []) {
    const calls = (step.calls ?? []).map(
      (c): RealizedCall => ({ from: c.from, to: c.to, mode: c.mode, label: c.label })
    );
    map.set(step.activityNodeId, { nodeId: step.activityNodeId, calls });
  }
  return map;
}

/**
 * The owning use case's actors that appear as a call endpoint (`from` or `to`)
 * anywhere in its realized dynamic view — the "person" participants a sequence
 * view/overlay must render alongside the System's components. Actor order
 * mirrors `uc.actors` (not call-appearance order). Empty when the system or use
 * case is absent, or the use case links no dynamic view.
 */
export function personParticipants(
  system: System | undefined,
  uc: UseCase | undefined
): { id: string; role: string }[] {
  if (system === undefined || uc === undefined) return [];

  const view = (system.dynamicViews ?? []).find((v) => v.useCaseId === uc.id);
  if (view === undefined) return [];

  const endpoints = new Set<string>();
  for (const step of view.steps ?? []) {
    for (const call of step.calls ?? []) {
      endpoints.add(call.from);
      endpoints.add(call.to);
    }
  }

  return (uc.actors ?? [])
    .filter((a) => endpoints.has(a.id))
    .map((a) => ({ id: a.id, role: a.role }));
}

/** One authored call, still tagged with the step (activity node) it belongs to,
 *  in DFS-linearized order — everything adapters.ts' SequencedCall carries
 *  except the global `seq` (assigned by the caller once the full walk is
 *  known — it is the one field this leaf module, which knows nothing of the
 *  wider view, cannot itself compute). */
export interface LinearizedCall {
  from: string;
  to: string;
  mode: CallMode;
  label: string;
  /** The activity node whose CallStep authored this call. */
  stepNodeId: string;
  /** That node's activity-diagram label ('' / the node id when no diagram is linked). */
  stepLabel: string;
  /** 1-based position of this call within its own step. */
  callInStep: number;
  /** Total calls authored on this step. */
  callsInStep: number;
  /**
   * Alternative-group display label ("1a", "1b", …), when this step authors at
   * least one call with a non-empty `alt` value (call-chain rollout Task 5).
   * Calls sharing the SAME `alt` value on this step are concurrent
   * alternatives — cardinality rules read them as targeting the same
   * Manager — so they share the numeric part and take a letter suffix in
   * declared order; a plain call (no `alt`) on the SAME step still consumes
   * the next numeric position by itself, with no letter, so a group of N
   * alternatives compresses to ONE position rather than inflating the count
   * by N. A step authoring NO alt calls at all leaves every one of its
   * calls' altLabel undefined — those calls keep the plain global `seq`
   * adapters.ts' toDynamicView assigns, unchanged from before this field
   * existed.
   */
  altLabel?: string;
}

/**
 * Per-step alt-aware position labels, aligned 1:1 with `calls` in declared
 * order. A step with no `alt` value on ANY of its calls (the common case)
 * returns an array of `undefined`s — its calls fall back to plain global
 * `seq` numbering, untouched. Otherwise every call in the step gets a label:
 * an alt-group's members share the numeric part (assigned the first time
 * that `alt` value is seen, in declared order) with a letter suffix by
 * declared order within the group ('a', 'b', …); a plain call still advances
 * the numeric counter by one, alone.
 */
function altLabelsForStep(calls: readonly { alt?: string | null }[]): (string | undefined)[] {
  if (!calls.some((c) => c.alt != null && c.alt.length > 0)) {
    return calls.map(() => undefined);
  }
  const positionByAlt = new Map<string, number>();
  const nextLetterByAlt = new Map<string, number>();
  let position = 0;
  return calls.map((c) => {
    const alt = c.alt;
    if (alt == null || alt.length === 0) {
      position += 1;
      return String(position);
    }
    let pos = positionByAlt.get(alt);
    if (pos === undefined) {
      position += 1;
      pos = position;
      positionByAlt.set(alt, pos);
      nextLetterByAlt.set(alt, 0);
    }
    const letterIndex = nextLetterByAlt.get(alt) ?? 0;
    nextLetterByAlt.set(alt, letterIndex + 1);
    return `${String(pos)}${String.fromCharCode(97 + letterIndex)}`;
  });
}

/**
 * Deterministic linearization of a use case's step-keyed calls: a DFS over the
 * activity graph from its entry nodes (start ∪ event nodes, diagram-declared
 * order), following authored edge order, each edge traversed at most once. A
 * step's calls are emitted the FIRST time its node is visited. Steps whose node
 * the walk never reaches (dangling, off-path, or — when no activity diagram is
 * linked — every step) are appended afterward in AUTHORED step order, never
 * silently dropped. `activity` absent degrades to "no graph": every step is
 * emitted in authored order and stepLabel falls back to the node id.
 */
export function linearizeSteps(
  steps: NonNullable<System['dynamicViews']>[number]['steps'],
  activity: ActivityDiagram | null | undefined
): LinearizedCall[] {
  const stepList = steps ?? [];
  const stepsByNode = new Map<string, (typeof stepList)[number]>();
  for (const s of stepList) stepsByNode.set(s.activityNodeId, s);

  const nodes = activity?.nodes ?? [];
  const edges = activity?.edges ?? [];
  const labelById = new Map(nodes.map((n) => [n.id, n.label]));

  // Adjacency in authored edge order.
  const outgoing = new Map<string, ActivityEdge[]>();
  for (const e of edges) {
    const list = outgoing.get(e.from);
    if (list === undefined) outgoing.set(e.from, [e]);
    else list.push(e);
  }

  const visitedNodes = new Set<string>();
  const visitedEdges = new Set<ActivityEdge>();
  const visitOrder: string[] = [];

  function dfs(nodeId: string): void {
    if (!visitedNodes.has(nodeId)) {
      visitedNodes.add(nodeId);
      visitOrder.push(nodeId);
    }
    for (const edge of outgoing.get(nodeId) ?? []) {
      if (visitedEdges.has(edge)) continue;
      visitedEdges.add(edge);
      dfs(edge.to);
    }
  }

  const entries = nodes.filter(
    (n) => n.kind === 'start' || n.kind === 'timeEvent' || n.kind === 'acceptEvent'
  );
  for (const entry of entries) dfs(entry.id);

  const out: LinearizedCall[] = [];
  const emitted = new Set<string>();

  function emitStep(nodeId: string): void {
    if (emitted.has(nodeId)) return;
    const step = stepsByNode.get(nodeId);
    if (step === undefined) return;
    emitted.add(nodeId);
    const calls = step.calls ?? [];
    const stepLabel = labelById.get(nodeId) ?? nodeId;
    const altLabels = altLabelsForStep(calls);
    calls.forEach((c, i) => {
      out.push({
        from: c.from,
        to: c.to,
        mode: c.mode,
        label: c.label,
        stepNodeId: nodeId,
        stepLabel,
        callInStep: i + 1,
        callsInStep: calls.length,
        ...(altLabels[i] !== undefined ? { altLabel: altLabels[i] } : {}),
      });
    });
  }

  // First-visit order from the DFS walk, then every never-visited step
  // (dangling/off-path, or ALL steps when there is no activity diagram at all)
  // appended in its AUTHORED position.
  for (const nodeId of visitOrder) emitStep(nodeId);
  for (const step of stepList) emitStep(step.activityNodeId);

  return out;
}
