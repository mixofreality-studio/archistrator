# archistrator

The archistrator app (server + webApp) + its design state (.aiarch/state).
Built on @davidmarne/archistrator-platform. AGPL-3.0-only.

Local server-side git writes need: `git config receive.denyCurrentBranch updateInstead`.

## Testing

Component tests are generated. Each built component's tests are the scenarios
derived from the committed use-case and dynamic-view diagrams, bound in its
test plan in `.aiarch/state/project.json`. `make gen-tests` (in `server/`)
writes the `*_scenarios.gen_test.go` files and `uitests/generated/**`, and
`make gen-tests-check` is their drift gate in CI. Never edit a generated
file. The only hand-written test code allowed is the hooks file testgen
seeds once, which you then fill in. Don't add other test files: the
`scenario-tests-only` arch rule (`TestFileLayout`) and the webApp's eslint
ratchet refuse them. `make test-scenarios` runs every `TestScenario_*` into
`server/test-results/go-test.jsonl`. See
`docs/superpowers/specs/2026-10-02-deterministic-component-testing-design.md`.
