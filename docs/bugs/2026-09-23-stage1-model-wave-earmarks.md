# Stage 1 (model wave) — earmarks and carry-forwards (2026-09-23)

Stage 1 shipped the volatility merge and the planned-component derivation rule only. Four Error gates (SYS-CARD-MGR, DV-SINGLE-MGR, DH-CONTRACT-FACET, ALIGN-EXTRA-PKG) make the Manager collapse one indivisible commit with its code — see spec §8. What could not ship here:

## Post-merge step (main only)
- `uitests/testdata/coreUseCasesProject.json` is already drifted on main (16 decisions vs the model's 18) and `npm run check:core-use-cases-fixture` is red today; it is wired into no CI workflow. The regen reads the committed **main** branch (`cmd/gen-uitests-fixtures/main.go:96` → `gitRepoLocator{branch: "main"}`), so it cannot be run in a worktree. After each merge to main: `cd uitests && npm run regen:core-use-cases-fixture` and commit.

## Platform (method-assets) — founder STOP
- `.claude/skills/the-method-project-tracking/SKILL.md:56` names `systemDesignManager`; `.claude/` is materialized from method-assets. Re-run `grep -rln "systemDesignManager\|constructionManager\|projectDesignManager" .claude/skills .claude/commands .claude/agents` at stage 4 when the names stop existing.
- `the-method-review-routing` SKILL.md drift vs the stage-2 review engine (who reviews + whether human) — same release.

## Carry-forward to stage 4's first commit
- Plan `docs/superpowers/plans/2026-09-23-activity-experience-stage1.md` Tasks 2 and 3, verbatim (component, 12-op contract with `$defs` provenance, 14 relationships, merged activity diagram, `alt`-group dynamic view), plus `cmd/clientgen/main.go` `exposedManagers`, `cmd/appgen/main.go` `WebExposedManagers`, `server/internal/arch_test.go` allowlists, `registered_names_test.go` golden, `engine_test.go` `realizedViews`, and the drain of `*:nextActivity:*` and the design sessions.
- Stage-4 entry criterion: `buildStatus` enum/vocabulary rule (today a free string — `"Planned"` would silently re-derive the activity); note the agent-callable `estimationDerivePlan` MCP tool now requires `buildStatus` on every component of a hand-authored payload (in-repo callers always emit it).

## Deferred to stage 3
- `activityExecutionAccess` (spec §5.3): reproduced — as a `planned` RA without relationships it fires SYS-RA-ORPHAN (Error); with relationships, DV-REL-COVERAGE (Error) until a dynamic view exercises them, which cannot be authored honestly while `gitActivityStatusAccess`/`constructionTransitionAccess`/`designSessionAccess` are what the code calls. Model it with its code.

## Model hygiene (next System pass)
- `message-bus` carries no `buildStatus` while the other three `provided` utilities carry `"external"`; nothing reads it, but it is inconsistent.
- `designhealthengine.go:22` and `systemdesignmanager.go:132` comments name the retired "System Design Phase Workflow" volatility; six `uitests/preview-fixtures/**` snapshots embed the 19-volatility world.
- Slot-3 `Design Conformance Rules` traces B-02 without a reciprocal `volatilityHint` on B-02 (deliberate asymmetry; no gate reads it).
