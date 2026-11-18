# Migrates run008.project.json to conform to framework-go v0.27.0 UC-ACTOR-DOOR and
# UC-LANE-RESOLVES. Touches only node.roleName and node.linkedActorId in
# slots["4"] (coreUseCases); node ids, edges, contracts, plans and dynamic views are
# read, never written. See README.md. Idempotent.
#
#   - a node in the "Caller" / "Hosted Workspace" lane is redrawn in the lane of the use
#     case's FIRST declared actor (roleName = that actor's role)
#   - an action node in a declared actor's lane gets linkedActorId = that actor's id
#   - a node in the "Todo System" lane is redrawn in the System component that performs
#     it: the callee of the first call of the dynamic-view step whose activityNodeId is the
#     node; a node with no step falls back to the Manager component's name
.slots["5"].model as $sys
| ($sys.components | map({key: .id, value: .name}) | from_entries) as $name
| ($sys.components | map(select(.layer == "manager")) | .[0].name) as $manager
| ($sys.dynamicViews | map({key: .useCaseId, value: .steps}) | from_entries) as $steps
| .slots["4"].model.decisions |= map(
    .useCase as $uc
    | $uc.actors[0] as $first
    | .useCase.activity.nodes |= map(
        (if (.roleName == "Caller" or .roleName == "Hosted Workspace") then .roleName = $first.role else . end)
        | . as $n
        | (if $n.roleName == "Todo System" then
             ((($steps[$uc.id] // []) | map(select(.activityNodeId == $n.id)) | .[0].calls[0].to) as $to
              | .roleName = (if $to != null then $name[$to] else $manager end))
           else . end)
        | . as $m
        | ($uc.actors | map(select(.role == $m.roleName)) | .[0]) as $actor
        | (if $actor != null and $m.kind == "action" then .linkedActorId = $actor.id else . end)
      )
  )
