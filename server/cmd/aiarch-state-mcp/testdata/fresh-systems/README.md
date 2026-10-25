# Fresh-project System corpus

Each file is one paid bench run's slot-5 `System` model **exactly as the
architecture agent drafted it** — the shape the server receives at
`putDraftModel`, never a hand-keyed document like archistrator's own
`.aiarch/state/project.json`. Tests that reach a contract-keyed path
(`TestRig_FreshSystemReachesRecordServiceContract`, delivery's
`TestFreshSystem_EveryPlannedActivityResolvesItsContracts`) run over every file here.

| file | run | components |
|---|---|---|
| `todomvc-20261025.json` | run-20261025T171725Z-0dc4cb93 (walled on `no contractKey`, FIX-007) | 8 |
| `todomvc-20261017.json` | run-20261017T211949Z-30a6612c (3 clients) | 8 |

Each `<name>.json` has a `<name>.predecessors.json`: a minimal project document
carrying the run's committed Phase-1 predecessor slots (0, 1, 3, 4) the System
was drafted against — the draft gate's call-chain rules (CC-*) resolve the
System's dynamic views through them.

To add a run's System (do it for every paid run that commits an architecture):

    S='git -C <archive>/<run>/state-repo show main:.aiarch/state/project.json'
    $S | jq '.slots["5"].model' > todomvc-<yyyymmdd>.json
    $S | jq '{id:"rigproj", version:1, owner:"owner", name:"Test Project",
              slots: (.slots|with_entries(select(.key|IN("0","1","3","4"))))}' \
      > todomvc-<yyyymmdd>.predecessors.json

Keep it verbatim: do not add `contractKey` or any other server-derived field —
the point of the corpus is what the agent writes, not what the server stores.
