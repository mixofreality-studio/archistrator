/**
 * Pure (JSX-free, contracts-import-free) DECIDER resolution for a
 * decision/switch activity node with no realized step (founder QA round 3,
 * change 3 addendum): "if it's a decision shouldn't the person or engine
 * responsible for making that decision be highlighted?" Rather than muting
 * the whole diagram (DynamicViewFlow's `build()` `muteAll` — the by-design
 * control-flow treatment for merge/fork/join/start/end/…), highlight the ONE
 * participant responsible for making the branch choice.
 *
 *  - An ACTOR-lane node (`lane !== 'Machine'`, the ActivityNodeView sentinel
 *    for a machine/system-driven node — see useCaseViews.toUseCaseView): the
 *    actor whose `role` equals the lane string, among the owning use case's
 *    actors. No match falls through to the entry-Manager rule below (an
 *    actor lane naming nobody is presumably an authoring gap, not a reason
 *    to highlight nothing).
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
  lane: string,
  actors: readonly DeciderActor[],
  participants: readonly DeciderParticipant[],
  edges: readonly DeciderCall[]
): DeciderResult | undefined {
  if (lane !== 'Machine') {
    const actor = actors.find((a) => a.role === lane);
    if (actor !== undefined) return { id: actor.id, label: actor.role };
  }
  const layerOf = new Map(participants.map((p) => [p.id, p.layer]));
  const nameOf = new Map(participants.map((p) => [p.id, p.name]));
  const entry = edges.find(
    (e) => layerOf.get(e.from) === 'client' && layerOf.get(e.to) === 'manager'
  );
  if (entry === undefined) return undefined;
  return { id: entry.to, label: nameOf.get(entry.to) ?? entry.to };
}
