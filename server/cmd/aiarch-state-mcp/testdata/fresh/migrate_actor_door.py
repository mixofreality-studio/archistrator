#!/usr/bin/env python3
"""Migrates a run*.project.json fixture to conform to framework-go v0.27.0 UC-ACTOR-DOOR and
UC-LANE-RESOLVES. Touches only node.roleName and node.linkedActorId in slots["4"]
(coreUseCases); node ids, edges, contracts, plans and dynamic views are read, never
written. See README.md. Idempotent.

    ./migrate_actor_door.py <fixture>.project.json
    ./migrate_actor_door.py ../fresh-systems/<name>.predecessors.json ../fresh-systems/<name>.json

The fixture is edited as TEXT, line by line, because it mixes literal and \\u-escaped
non-ASCII (a JSON round trip would rewrite bytes the migration does not own).

Rules, per use case (the same lane rules as migrate_run008_actor_door.jq):

  - a node in the "Caller" lane is redrawn in the lane of the use case's FIRST declared actor
  - a node in any other lane that is not a declared actor's role or a System component's name
    is redrawn in the System component that performs it: the callee of the first call of the
    dynamic-view step whose activityNodeId is the node, taken from the first call whose caller
    is NOT a declared actor (the component-to-component hop; an actor-to-client call only names
    the surface the actor used); a node with no step takes the Manager component's name
  - these recordings draw no action node in an actor's lane (their Caller-lane nodes are the
    start, the outcome notes and the end), so the actor's act is found from the dynamic view:
      * the FIRST action or decision node whose step carries a call FROM a declared actor; and
      * the diagram's ENTRY (the first action or decision after the start, past any notes and
        merges), because a branch can skip the node above and UC-ACTOR-DOOR wants every
        elementary path to pass through an actor node, provided its step (if it has one)
        touches an actor, as CC-ACTOR-LANE requires of a node laned to an actor
    Each such node is drawn in the lane of the declared actor the call names (else the first
    declared actor) and gets linkedActorId = that actor's id
"""
import json
import re
import sys

path = sys.argv[1]
text = open(path, encoding='utf-8').read()
doc = json.loads(text)
# The System the lanes resolve against: slots["5"] of the same file, or (a fresh-systems
# predecessors file carries only slots 0-4) the bare System model named by the second argument.
system = doc['slots']['5']['model'] if '5' in doc['slots'] else json.load(open(sys.argv[2], encoding='utf-8'))
cname = {c['id']: c['name'] for c in system['components']}
manager = next(c['name'] for c in system['components'] if c['layer'] == 'manager')
steps = {v['useCaseId']: v['steps'] for v in system['dynamicViews']}
known_components = set(cname.values()) | set(cname)

plan = []  # (roleName, linkedActorId) per node, in document order
for d in doc['slots']['4']['model']['decisions']:
    uc = d['useCase']
    actors = uc['actors']
    actor_ids = {a['id'] for a in actors}
    actor_roles = {a['role'] for a in actors}
    by_id = {a['id']: a for a in actors}
    first = actors[0]
    nodes = uc['activity']['nodes']
    node_steps = {s['activityNodeId']: s for s in steps.get(uc['id'], [])}

    # the actor's act: the first action (or decision) node whose dynamic-view step carries a call
    # FROM a declared actor (CC-ACTOR-LANE needs the step to touch the actor the node is laned to)
    act_node, act_actor = None, None
    for n in nodes:
        if n['kind'] not in ('action', 'decision'):
            continue
        s = node_steps.get(n['id'])
        caller = next((c['from'] for c in (s['calls'] if s else []) if c['from'] in actor_ids), None)
        if caller is not None:
            act_node, act_actor = n['id'], by_id[caller]
            break
    if act_node is None:
        act_node = next(n['id'] for n in nodes if n['kind'] == 'action')
        act_actor = first

    acts = {act_node: act_actor}

    # UC-ACTOR-DOOR also wants EVERY elementary path to pass through an actor node, and a branch
    # can skip the node above. The diagram's entry (the node the start flows into) is on every
    # path, so it is the actor's door too, provided its step (if it has one) touches an actor, as
    # CC-ACTOR-LANE requires of a node laned to an actor.
    start_id = next((n['id'] for n in nodes if n['kind'] == 'start'), None)
    by_node = {n['id']: n for n in nodes}
    entry = by_node.get(start_id)
    for _ in nodes:  # walk past the notes and merges the start flows through
        if entry is None or entry['kind'] not in ('start', 'note', 'merge'):
            break
        nxt = next((e['to'] for e in uc['activity']['edges'] if e['from'] == entry['id']), None)
        entry = by_node.get(nxt)
    if entry is not None and entry['kind'] in ('action', 'decision') and entry['id'] not in acts:
        s = node_steps.get(entry['id'])
        if s is None:
            acts[entry['id']] = first
        else:
            caller = next((c['from'] for c in s['calls'] if c['from'] in actor_ids), None)
            if caller is not None:
                acts[entry['id']] = by_id[caller]

    for n in nodes:
        role, link = n['roleName'], n['linkedActorId']
        if n['id'] in acts:
            role, link = acts[n['id']]['role'], acts[n['id']]['id']
        elif role == 'Caller':
            role = first['role']
        elif role not in actor_roles and role not in known_components:
            calls = (node_steps.get(n['id']) or {}).get('calls') or []
            hop = next((c for c in calls if c['from'] not in actor_ids), calls[0] if calls else None)
            role = cname[hop['to']] if hop else manager
        plan.append((role, link))

lines = text.split('\n')
role_re = re.compile(r'^(\s*"roleName": )"[^"]*"(,?)$')
link_re = re.compile(r'^(\s*"linkedActorId": )(null|"[^"]*")(,?)$')
i = 0
k = 0
while i < len(lines):
    m = role_re.match(lines[i])
    if m:
        role, link = plan[k]
        lines[i] = f'{m.group(1)}{json.dumps(role)}{m.group(2)}'
        lm = link_re.match(lines[i + 1])
        assert lm, lines[i + 1]
        lines[i + 1] = f'{lm.group(1)}{json.dumps(link)}{lm.group(3)}' if link else f'{lm.group(1)}null{lm.group(3)}'
        k += 1
        i += 2
        continue
    i += 1
assert k == len(plan), (k, len(plan))
open(path, 'w', encoding='utf-8').write('\n'.join(lines))
print(f'{k} nodes migrated')
