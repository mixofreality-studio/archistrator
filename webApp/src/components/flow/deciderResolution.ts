/**
 * Pure (JSX-free, contracts-import-free) DECIDER resolution for a
 * decision/switch activity node with no realized step (founder QA round 3,
 * change 3 addendum): "if it's a decision shouldn't the person or engine
 * responsible for making that decision be highlighted?" Rather than lighting no
 * current fragment at all (the by-design control-flow treatment for
 * merge/fork/join/start/end/…), highlight the ONE participant responsible for
 * making the branch choice. Since founder QA round 4 that highlight lands ON TOP
 * of the visited trail: the decider burns at focus strength over the chain the
 * reader has already walked, rather than being the only thing on a blank canvas.
 *
 *  - AN EXPLICIT `decidedBy` (call-chain rollout Task 5 — the node's authored
 *    decider id, an actor id or a component id): resolved against persons
 *    (`actors`) first, then components (`participants`), under the SAME
 *    placement guard as the actor-lane rule below — the resolved participant
 *    must appear as a call endpoint on some edge in this view. An id that
 *    resolves to neither, or resolves but isn't placed, is unresolvable and
 *    falls through to the inference chain below (it takes no precedence over
 *    the rest, only over the "no explicit decider" case).
 *  - An ACTOR-lane node (`lane !== 'Machine'`, the ActivityNodeView sentinel
 *    for a machine/system-driven node — see useCaseViews.toUseCaseView): the
 *    actor whose `role` equals the lane string, among the owning use case's
 *    actors — PROVIDED that actor is actually PLACED in this dynamic view,
 *    i.e. appears as a call endpoint (`from`/`to`) on some edge. This is the
 *    exact same test `dv.persons` itself is built from (personParticipants,
 *    contracts/realization.ts) — a role match with no placement would name a
 *    node that does not exist in this view's rendered persons, and
 *    DynamicViewFlow's focusNodeId would then match nothing (review fix
 *    round 1: everything dims, nothing lights). No role match, OR a role
 *    match that isn't placed, falls through to the entry-Manager rule below
 *    (an actor lane naming nobody present is presumably an authoring gap,
 *    not a reason to highlight nothing).
 *  - A MACHINE-lane node (or the actor fallback above): the use case's ENTRY
 *    MANAGER — the `to` of the FIRST call in the view whose from-side
 *    participant layer is `client` and to-side layer is `manager`
 *    (DV-SINGLE-MGR: exactly one entry Manager per dynamic view). undefined
 *    when no such call exists (a zero-step / synthetic view) — callers fall
 *    back to mute-all.
 *
 * Structurally typed (not `DynamicViewModel`/`Actor` from contracts/*) so
 * fixtures stay tiny under `node --test` — a real dynamic-view model's
 * `participants`/`edges` and a use case's `actors` all satisfy these shapes
 * with room to spare.
 */
export interface DeciderActor {
  id: string;
  role: string;
}

export interface DeciderParticipant {
  id: string;
  name: string;
  layer: string;
}

export interface DeciderCall {
  from: string;
  to: string;
}

export interface DeciderResult {
  /** The decider's participant id — an actor id or a component id. */
  id: string;
  /** Display label for the caption ("Decided by <label>"): the actor's role,
   *  or the entry Manager's component name. */
  label: string;
}

export function resolveDecider(
  decidedBy: string | undefined,
  lane: string,
  actors: readonly DeciderActor[],
  participants: readonly DeciderParticipant[],
  edges: readonly DeciderCall[]
): DeciderResult | undefined {
  const placed = (id: string): boolean => edges.some((e) => e.from === id || e.to === id);

  if (decidedBy !== undefined && decidedBy.length > 0) {
    // Actor ids and component ids are authored from disjoint id spaces in
    // practice; checking actors first is only a tie-break should one ever
    // coincide with a component id — a named PERSON is the more specific,
    // more intentional decider of the two.
    const actor = actors.find((a) => a.id === decidedBy);
    if (actor !== undefined && placed(actor.id)) {
      return { id: actor.id, label: actor.role };
    }
    const participant = participants.find((p) => p.id === decidedBy);
    if (participant !== undefined && placed(participant.id)) {
      return { id: participant.id, label: participant.name };
    }
    // Unresolvable (unknown id, or a known id that isn't a call endpoint in
    // this view) — fall through to the lane/entry-Manager inference chain.
  }

  if (lane !== 'Machine') {
    const actor = actors.find((a) => a.role === lane);
    if (actor !== undefined && placed(actor.id)) {
      return { id: actor.id, label: actor.role };
    }
  }
  const layerOf = new Map(participants.map((p) => [p.id, p.layer]));
  const nameOf = new Map(participants.map((p) => [p.id, p.name]));
  const entry = edges.find(
    (e) => layerOf.get(e.from) === 'client' && layerOf.get(e.to) === 'manager'
  );
  if (entry === undefined) return undefined;
  return { id: entry.to, label: nameOf.get(entry.to) ?? entry.to };
}
