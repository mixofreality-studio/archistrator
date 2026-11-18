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
