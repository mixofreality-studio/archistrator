# /service-construction

> The Construction step of a service activity: implement the component exactly to its frozen contract, verify, and add your commits to the activity PR.

**Arguments** — `$ARGUMENTS` is `<component_id> <activity_id>`. Parse once; do not swap. Commits land on the existing activity branch `activity/<activity_id>` and its single PR.

**Agent + skills.** Work to the standard of the **`junior-developer`** agent (`.claude/agents/junior-developer.md`). Follow **[[the-method-layers]]** (layer + call-direction rules) and **[[the-method-project-state]]** for reading the contract from state.

**Goal / intention.** Per Löwy's senior hand-off (ch. 14): once a senior developer has designed and the architect has reviewed a component's contract, a junior developer builds that one component against it — nothing more. Junior developers are not the unskilled; they are "not yet capable of doing detailed design correctly," so their job is construction, done well, one service at a time. Per ch. 14 §4: developers should never code more than one service at a time, and will spend considerable time testing and integrating that service. Any design refinement, however trivial, goes back to the senior developer who designed the contract — a junior never widens a contract silently. Once construction is finished, the junior proceeds to code review with that same senior developer, not a peer. "Done" means the frozen contract is satisfied and the component's own fast checks pass — Löwy's tracking is binary phase exit, not "almost done."

## Steps

> **State changes go through the `aiarch-state` MCP tools, not hand-edits.** Where a step below says to record a service contract, phase artifact, or testing artifact and "commit onto branch", do it with the matching tool — `recordServiceContract` / `recordPhaseArtifact` / `recordTestingState` — and finish with `publishDraft`. Do **not** hand-edit `.aiarch/state/project.json` or run `git` for state; only source/doc **files** (code, docs) are git-committed by you. See [[the-method-project-state]].

1. **Read the contract** from `.aiarch/state/project.json` → `.serviceContracts["<component_id>"]` per [[the-method-project-state]]. It carries `Layer`, `Ops`, `Inbound`/`Outbound`, `DataContracts`, `ErrorModel`, `Idempotency`. Implement exactly it. If it has a gap, do NOT widen it (see `junior-developer`).
2. **Implement** in the package the contract names — the `goPackage` in `.serviceContracts["<component_id>"]` (its `Layer` fixes the layer). Match existing code in that layer. Stay inside the component. Do NOT edit `*/generated/`. Commit onto `activity/<activity_id>`.
3. **Verify YOUR code** (from the Go module directory containing the target package — the repo root in a generated app; `GOWORK=off`): `gofmt -w .`; `GOWORK=off go build ./...`; `GOWORK=off go vet ./...`; `GOWORK=off go test ./<goPackage>/...`. Only your package — not `make test-short`.
4. **Stop.** Do not mark phase status (the Manager owns it) and do not merge. Leave the PR for the gate.
