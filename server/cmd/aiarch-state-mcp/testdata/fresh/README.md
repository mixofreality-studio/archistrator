# testdata/fresh

Recorded state from real bench runs, used as fixtures by the `aiarch-state-mcp` fresh-project tests.

## run008.project.json: actor-door migration

framework-go v0.27.0 added two Tier-1 rules over the core use cases, which the run008 recording
(made before them) failed at admission with 40 findings:

- `UC-ACTOR-DOOR` (14): a clientAction use case needs a node linked to a declared actor
  (`linkedActorId`/`decidedBy`); the recording linked none.
- `UC-LANE-RESOLVES` (26): a node's lane (`roleName`) must name a declared actor of the use case
  or a System component; the recording used the lanes `Caller`, `Hosted Workspace` and `Todo System`.

The fixture data was migrated, not waived, by `migrate_run008_actor_door.sh` (which runs
`migrate_run008_actor_door.jq`). It changes only `roleName` and `linkedActorId` on the activity nodes
in `slots["4"]`; node ids, edges, contracts, plans and dynamic views are untouched. Re-running it is a
no-op.

Rules applied, per use case:

- A node in the `Caller` or `Hosted Workspace` lane is redrawn in the lane of the use case's FIRST
  declared actor (`roleName` = that actor's role). Where a use case declares several actors the lane is
  ambiguous: `tend-the-list` and `take-stock-of-the-list` declare `user` and `agent-tooling`, and both are
  linked to `user` (the first declared).
- An `action` node in a declared actor's lane gets `linkedActorId` = that actor's id (14 nodes, one per
  use case: its `n2`). The other nodes of the lane (start, note, end) only change lane.
- A node in the `Todo System` lane is redrawn in the System component that performs it: the callee of
  the first call of the dynamic-view step whose `activityNodeId` is the node. A node with no step
  (decisions, merges, notes, and actions without a step) takes the Manager component's name, `ListManager`.

Reproduce from the pre-migration file (git history) with
`./migrate_run008_actor_door.sh run008.project.json` (needs `jq` and `perl`).

## run010, run011, run013, run016 and the fresh-systems predecessors: actor-door migration

Same two rules as run008 (`UC-ACTOR-DOOR`, `UC-LANE-RESOLVES`, framework-go v0.27.0), same answer:
the fixture data was migrated, not waived, by `migrate_actor_door.py` (`./migrate_actor_door.py
run013.project.json`; for a `fresh-systems/<name>.predecessors.json`, which carries only slots 0-4,
name the System file as the second argument). The migrated files are `run010`, `run011`, `run013`
and `run016` here and the two `../fresh-systems/*.predecessors.json` (their System files are
untouched, verbatim as the agent drafted them). It edits only `roleName` and `linkedActorId` on the activity nodes in
`slots["4"]`, as text (the files mix literal and `\u`-escaped non-ASCII, so a JSON round trip would
rewrite bytes the migration does not own). Node ids, edges, contracts, plans and dynamic views are
untouched. Re-running it is a no-op.

These recordings draw no action node in an actor's lane (their `Caller`-lane nodes are the start,
the outcome notes and the end), so the run008 rule "an action node in an actor's lane gets the link"
has nothing to link. The rules applied, per use case:

- A node in the `Caller` lane is redrawn in the lane of the use case's FIRST declared actor.
- A node in any other lane that is neither a declared actor's role nor a System component's name
  (`Todo List`, `Other Open Views`, `Team Todo Record`, ...) is redrawn in the System component that
  performs it: the callee of the first call, of the dynamic-view step whose `activityNodeId` is the
  node, whose caller is not a declared actor (an actor-to-client call only names the surface the
  actor used); a node with no step takes the Manager component's name.
- The actor's act is found from the dynamic view: the use case's FIRST action or decision node whose
  step carries a call from a declared actor, and the diagram's ENTRY (the first action or decision
  after the start, past notes and merges; a branch can skip the first node, and `UC-ACTOR-DOOR` wants
  every elementary path through an actor node), when its step, if it has one, touches an actor
  (`CC-ACTOR-LANE` requires that of a node laned to an actor). Each such node is drawn in the lane of
  the actor the call names (else the first declared actor) and gets `linkedActorId` = that actor's
  id. (The recorded diagrams put a check or a lookup there; the step is where the actor's request
  enters, which is what the door rule asks the diagram to show.)
