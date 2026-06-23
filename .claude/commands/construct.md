# Construct

> Phase-3 construction entry point for ONE component. Implement the component named by
> its service contract in project state, verify it, and open a PR. This is what the
> `aiarch-construct.yml` GitHub Actions workflow invokes — the workflow carries no prompt
> logic, it just runs this command. One component, one activity, one PR.

**Usage:** `/construct <component_id> <activity_id>`

- `$1` = `component_id` — a key in `.aiarch/state/project.json` → `.serviceContracts`.
- `$2` = `activity_id` — the construction activity (e.g. `C-BG`); work lands on branch `aiarch/construct/$2`.

**Agent + skills.** Implement to the standard of the **`junior-developer`** agent
(`.claude/agents/junior-developer.md`). Follow **[[the-method-layers]]** (layer + call-direction
rules) and **[[the-method-service-contract]]** (contract shape + what each field means).

**State is git-as-DB.** The canonical project state is `.aiarch/state/project.json`. Never write
`designs/*.md` or any parallel markdown copy of the contract — the JSON in state is the source of
truth; markdown is only ever a render-on-read.

## Steps

1. **Read the contract.** It is the typed entry `.aiarch/state/project.json` →
   `.serviceContracts["$1"]`. In CI it is also pre-extracted to `service-contract.json` at the repo
   root. It carries `Layer`, `Ops` (operation signatures + I/O structs), `Inbound`/`Outbound`
   parties, `DataContracts`, `ErrorModel`, and `Idempotency`. Implement exactly what it specifies —
   no more, no less. If the contract has a gap, do NOT silently widen it (see `junior-developer`).

2. **Implement** under `server/internal/<layer>/<pkg>/`, where `<layer>` comes from the contract's
   `Layer` (engine / manager / resourceaccess / client / utility). Match the conventions of existing
   code in the same layer. Stay inside the component — no calls up or sideways. Do **NOT** edit
   anything under `*/generated/`.

3. **Verify YOUR code** (working-directory: `server`, fast checks only):
   - `gofmt -w .`
   - `GOWORK=off go build ./...`
   - `GOWORK=off go vet ./...`
   - `GOWORK=off go test ./internal/<layer>/<pkg>/...`

   Run tests ONLY for the package you created — NOT `make test-short` (it spins up containers and is
   far too slow). Fix only failures in your own code; do not chase pre-existing repo issues. You have
   a ~20-minute budget — keep moving and land the PR.

4. **Commit** all changes onto branch `aiarch/construct/$2` and **open a PR**:
   - Title: `construct($2): implement $1`
   - Body: the activity id, the component id, the contract reference
     (`.aiarch/state/project.json → .serviceContracts["$1"]`), and a checklist (contract satisfied /
     gofmt·vet·build·test pass / no `*/generated/` edits).
   - Put implementation notes (what you built, any deviation — should be none, test results) in the
     **PR body and commit messages**, not a `designs/*.md` log.

   Stop after opening the PR. Do not merge.
