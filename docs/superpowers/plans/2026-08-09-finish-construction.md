# Finish Construction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every activity in `.aiarch/state/project.json` marked Done+Integrated (69 after adding C-EA), the deployed app at https://archistrator.capture-gtd.com/ rendering the project as **Operating**, all tests and lints green.

**Architecture:** Five architect-ratified phases (spec: `docs/superpowers/specs/2026-08-09-finish-construction-design.md`): A repair the project.json codec round-trip corruption (hard gate before any rail write); B amend slot-9/slot-10 through the draft rail to clear `ACT-COMPONENT-COVERAGE`; C finish the activity record (rail writes first, then precedented hand-commit reconciliation); D derived "Operating" presentation state (no enum change); E verify, document, ship, browser-verify.

**Tech Stack:** Go 1.26 (`GOWORK=off` everywhere), TypeScript/React (webApp), jq/python for state surgery, `./aiarch-state-mcp` binary (state rail + `validate`), git-as-DB on `main`.

## Global Constraints

- **Never run a server-side rail write (aiarch-state-mcp putDraftModel/publishDraft/record*) before Task 3's round-trip test is green** — the unfixed codec re-deletes `deployment.{infrastructure,bindings,settings}` (architect hard gate).
- **Within Phase C: rail writes precede lifecycle hand-commits** (architect hard gate).
- All Go commands run with `GOWORK=off` from `server/` (or `systemtests/`).
- After EVERY edit of `.aiarch/state/project.json`: run `./aiarch-state-mcp validate --root .` from repo root. Until Task 5 lands, the expected result is exactly ONE Error (`ACT-COMPONENT-COVERAGE` on episode-access); after Task 5, zero Errors. Any NEW Error means the edit broke something — stop and fix.
- **No synthetic `applied_mutations` entries; no mass-backfill of phase artifacts** (architect prohibition). Hand-commits use plain messages: `state: <what> (<why>)`.
- Hand-edits of project.json must preserve the existing `version` field (do not bump — server writes CAS on it) and never introduce keys outside the `projectDoc` codec (they'd be destroyed on the next server write).
- Commit messages end with `Co-Authored-By:` per repo convention. Commit after every task.
- The working tree starts with a modified `server/cmd/server/config.gen.go` — that is corruption fallout; Task 1 discards it. Do not commit it.
- `.claude/worktrees/` is untracked scratch — leave it alone.

---

## Phase A — substrate repair

### Task 1: Restore the deleted deployment sections + discard fallout

**Files:**
- Modify: `.aiarch/state/project.json` (slot 6 `.model.deployment` only)
- Restore: `server/cmd/server/config.gen.go` (git checkout)

**Interfaces:**
- Produces: slot-6 `deployment` carrying `infrastructure` (5 entries), `bindings` (15), `settings` (18) — the exact objects from commit `f6d2a75`.

- [ ] **Step 1: Discard the corrupted generated file**

```bash
git checkout -- server/cmd/server/config.gen.go
git status --short   # expect: no modified tracked files
```

- [ ] **Step 2: Splice the last-good sections into the live document**

```bash
git show f6d2a75:.aiarch/state/project.json | jq '.slots["6"].model.deployment | {infrastructure, bindings, settings}' > /tmp/deploy-sections.json
jq --slurpfile s /tmp/deploy-sections.json '.slots["6"].model.deployment += $s[0]' .aiarch/state/project.json > /tmp/project-restored.json
# verify counts and that NOTHING else changed:
jq '.slots["6"].model.deployment | [(.infrastructure|length),(.bindings|length),(.settings|length)]' /tmp/project-restored.json   # expect [5,15,18]
diff <(jq 'del(.slots["6"].model.deployment)' .aiarch/state/project.json) <(jq 'del(.slots["6"].model.deployment)' /tmp/project-restored.json)   # expect empty
mv /tmp/project-restored.json .aiarch/state/project.json
```

- [ ] **Step 3: Validate state + confirm the appgen family is unblocked**

```bash
./aiarch-state-mcp validate --root .   # expect exactly 1 Error: ACT-COMPONENT-COVERAGE episode-access
cd server && GOWORK=off make gen-temporal-check && git diff --stat -- cmd/server/config.gen.go   # gen must PASS; config.gen.go diff must be EMPTY (regenerated identical to HEAD)
```

If `gen-temporal-check` reports drift in generated files, that drift is the point of the check — inspect: only acceptable diff is none.

- [ ] **Step 4: Commit**

```bash
git add .aiarch/state/project.json
git commit -m "state: restore deployment infrastructure/bindings/settings deleted by codec round-trip at 87e4fe9"
```

### Task 2: Failing round-trip test for the codec drop

**Files:**
- Create: `server/internal/resourceaccess/projectstate/deployment_roundtrip_test.go`

**Interfaces:**
- Consumes: `projectstate.Project` / the `projectDoc` decode-encode path. Use the package's existing test idioms (see `access_test.go`) — the test lives INSIDE package `projectstate` so it can reach the unexported codec (`buildStateFiles` / the decode helper `applyMutationOnBranchFiles` uses).
- Produces: `TestDeploymentSectionsSurviveRoundTrip` — red until Task 3.

- [ ] **Step 1: Write the failing test**

The test must: (1) build a minimal project JSON document whose slot-6 `model.deployment` carries one entry each of `infrastructure`/`bindings`/`settings` shaped exactly like the restored data (sample below), (2) decode it through the same path a server mutation uses, (3) re-encode via the same path `buildStateFiles` uses, (4) assert all three sections survive byte-meaningfully (compare via `json.Unmarshal` into `map[string]any` and check the three keys are present with the right lengths and the `postgres` infra key intact).

Sample shapes (from the restored state — embed as a raw-string fixture):

```json
{
  "infrastructure": [{"key":"postgres","substrate":"postgres","profiles":["cloud"],"presence":"required","env":{"URL":"ARCHISTRATOR_POSTGRES_URL"}}],
  "bindings": [{"component":"projectStateAccess","presence":"required","provides":[],"settings":[{"name":"projectStateGitRepoURL","type":"string","default":"","env":"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL","description":"projectStateAccess GitLocal on-disk head-state repo URL (file://)."}],"perProfile":{"local":{"variant":"GitLocal","infra":[]},"cloud":{"variant":"GitHub","infra":["github-app"]}}}],
  "settings": [{"name":"listenAddr","type":"string","default":":8080","env":"ARCHISTRATOR_LISTEN_ADDR","description":"HTTP listen address."}]
}
```

- [ ] **Step 2: Run it, confirm it fails for the right reason**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestDeploymentSectionsSurviveRoundTrip -v
```

Expected: FAIL — the re-encoded document lacks `infrastructure`/`bindings`/`settings` (the generated `DeploymentTopology` Go type has no fields for them).

- [ ] **Step 3: Commit the red test**

```bash
git add server/internal/resourceaccess/projectstate/deployment_roundtrip_test.go
git commit -m "test: red round-trip test for deployment sections dropped by projectstate codec"
```

### Task 3: Model the three sections in the contract `$defs` + regen

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.projectStateAccess["$defs"].DeploymentTopology` + new `$defs` entries
- Regenerate: `server/internal/resourceaccess/projectstate/contract.gen.go` (via `make gen-models`), then the whole gen family

**Interfaces:**
- Produces: `DeploymentTopology` Go type with `Infrastructure []DeploymentInfraNode`, `Bindings []DeploymentComponentBinding`, `Settings []DeploymentSettingSpec` (json tags `infrastructure`/`bindings`/`settings`, all `omitempty`), matching the wire shapes in Task 2's fixture.

- [ ] **Step 1: Extend the `$defs`**

Edit `.aiarch/state/project.json` `.serviceContracts.projectStateAccess["$defs"]`:
- `DeploymentTopology.properties` gains:
  - `"infrastructure": {"type":["null","array"],"items":{"$ref":"#/$defs/DeploymentInfraNode"}}`
  - `"bindings": {"type":["null","array"],"items":{"$ref":"#/$defs/DeploymentComponentBinding"}}`
  - `"settings": {"type":["null","array"],"items":{"$ref":"#/$defs/DeploymentSettingSpec"}}`
  (NOT added to `required` — they are optional; keep `additionalProperties: false`.)
- New `$defs` (mirror the authoritative consumer, `framework-go-projectmodel` `deployment.go`, and the restored data — verify field lists against BOTH before writing):

```json
"DeploymentInfraNode": {"type":"object","properties":{"key":{"type":"string"},"substrate":{"type":"string"},"profiles":{"type":["null","array"],"items":{"type":"string"}},"presence":{"type":"string"},"env":{"type":["null","object"],"additionalProperties":{"type":"string"}}},"required":["key","substrate"],"additionalProperties":false},
"DeploymentSettingSpec": {"type":"object","properties":{"name":{"type":"string"},"type":{"type":"string"},"default":{"type":"string"},"env":{"type":"string"},"description":{"type":"string"}},"required":["name","type","env"],"additionalProperties":false},
"DeploymentBindingProfile": {"type":"object","properties":{"variant":{"type":"string"},"infra":{"type":["null","array"],"items":{"type":"string"}}},"required":["variant"],"additionalProperties":false},
"DeploymentComponentBinding": {"type":"object","properties":{"component":{"type":"string"},"presence":{"type":"string"},"provides":{"type":["null","array"],"items":{"type":"string"}},"settings":{"type":["null","array"],"items":{"$ref":"#/$defs/DeploymentSettingSpec"}},"perProfile":{"type":["null","object"],"additionalProperties":{"$ref":"#/$defs/DeploymentBindingProfile"}}},"required":["component","presence"],"additionalProperties":false}
```

**Before finalizing:** dump every distinct key present in the restored data (`jq '.slots["6"].model.deployment.bindings[] | keys' …` etc.) and confirm the schemas admit all of them; widen if the real data has fields the samples lacked.

- [ ] **Step 2: Regen + test green**

```bash
cd server && GOWORK=off make gen-models && GOWORK=off go build ./... \
  && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run TestDeploymentSectionsSurviveRoundTrip -v
```

Expected: PASS. Then the full drift family:

```bash
GOWORK=off make gen-temporal-check gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-sdk-check gen-config-check gen-main-check gen-uiprofiles-check
```

If `gen-models` changed `contract.gen.go` in ways that break compilation elsewhere (e.g. new types colliding), fix forward — the shapes are additive so breakage should be nil.

- [ ] **Step 3: Validate + commit**

```bash
cd .. && ./aiarch-state-mcp validate --root .   # still exactly 1 Error (episode-access)
git add -A && git commit -m "fix: model deployment infrastructure/bindings/settings in projectStateAccess contract so the codec round-trips them"
```

### Task 4: Full baseline gate run

**Files:** none (verification only; fix-forward commits allowed if something is red)

- [ ] **Step 1: Server suite**

```bash
cd server && test -z "$(gofmt -l .)" && GOWORK=off go vet ./... && GOWORK=off make fix-check \
  && GOWORK=off go build ./... && make lint && GOWORK=off make test-short && GOWORK=off make method-check
```

- [ ] **Step 2: webApp + systemtests**

```bash
cd ../webApp && npm run check
cd ../systemtests && test -z "$(gofmt -l .)" && make fix-check && GOWORK=off go test ./constitution/... -count=1
```

- [ ] **Step 3: Anything red gets fixed and committed (`fix: <gate> — <cause>`), then re-run until green. Commit nothing if all green (no changes).**

---

## Phase B — slot-9 + slot-10 amendment (legal rail)

### Task 5: ActivityList amendment — C-EA + componentId backfill

**Files:**
- Create (scratch): `<scratchpad>/activitylist-amended.json` — the full 69-activity model
- Modify (via rail): `.aiarch/state/project.json` slot 9

**Interfaces:**
- Consumes: slot-9 model (`jq '.slots["9"].model'`), slot-5 components (`jq '[.slots["5"].model.components[] | {id, name, contractKey, layer}]'`).
- Produces: committed slot 9 with 69 activities; every `coding:true` activity carries `componentId` resolving to a committed slot-5 component id; C-EA present. `ACT-COMPONENT-COVERAGE` cleared.

- [ ] **Step 1: Author the full amended model**

Start from the current model. Changes:
1. Append C-EA (workerClass/effortDays/riskBucket copied from a sibling RA coding activity — use C-AA's values; verify with `jq '.slots["9"].model.activities[] | select(.name=="C-AA")'`):

```json
{"name":"C-EA","effortDays":<C-AA's>,"workerClass":"<C-AA's>","coding":true,"riskBucket":<C-AA's>,"title":"Build Episode Access","componentId":"episode-access"}
```

2. For every `coding:true` activity, add `componentId`. Derivation: normalize the activity title (lowercase, strip non-alphanumerics) and match against slot-5 component id/name/contractKey — i.e. reproduce `deriveActivityComponent` (`server/cmd/aiarch-state-mcp/crossartifact.go:221-254`) as a one-off script so the authored ids agree with what the rule derives. Known hand-rulings the fuzzy match cannot make (architect-ratified):
   - `U-SPA-S`, `U-SPA-1`…`U-SPA-6`, `U-SPA-TEAM`, `G-SPA` → `web-client`
   - `C-WIA`, `C-HE` → **no componentId** (descoped concepts; leave the field absent — Warnings are the honest state)
3. Non-coding activities: untouched.

- [ ] **Step 2: Pre-validate on a scratch checkout**

```bash
git worktree add /tmp/slot9-check HEAD
jq --slurpfile m <scratchpad>/activitylist-amended.json '.slots["9"].model = $m[0]' /tmp/slot9-check/.aiarch/state/project.json > /tmp/p.json && mv /tmp/p.json /tmp/slot9-check/.aiarch/state/project.json
(cd /tmp/slot9-check && /Users/davidmarne/mixofrealitystudio/archistrator/aiarch-state-mcp validate --root .)
git worktree remove --force /tmp/slot9-check
```

Expected: the episode-access Error is GONE; C-WIA/C-HE remain Warnings; zero new Errors.

- [ ] **Step 3: Write through the draft rail**

Run the state MCP binary in draft mode against the repo (mirror how prior amendments landed — check `applied_mutations` + `git log --author=aiarch-state-mcp` for the precedent shape first):

```bash
AIARCH_JOB_MODE=draft AIARCH_ARTIFACT_KIND=activityList AIARCH_PROJECT_ID=archistrator \
AIARCH_STATE_ROOT=<repo root per binary convention — verify with server/cmd/aiarch-state-mcp/main.go before running> \
AIARCH_TARGET_BRANCH=main ./aiarch-state-mcp
```

then drive `putDraftModel` with `{"model": {"activities": [...69 items...]}}` and `publishDraft {"message": "activityList amendment: add C-EA (episode-access coverage), backfill authored componentId"}` over its MCP stdio. If driving stdio is impractical, the fallback (architect-sanctioned reconciliation idiom) is the identical jq splice + hand-commit with the same message — the rail is preferred, the content is identical either way.

- [ ] **Step 4: Verify + note the cascade**

```bash
./aiarch-state-mcp validate --root .   # expect ZERO Errors
jq '[.slots | to_entries[] | select(.value.staleBasis == true) | .key]' .aiarch/state/project.json   # expect slots 10-16 stale
```

Commit if the rail did not (message above).

### Task 6: Network amendment — C-EA row in slot 10

**Files:**
- Modify (via rail, kind `network`): `.aiarch/state/project.json` slot 10

**Interfaces:**
- Produces: slot-10 `dependencies` gains one row for C-EA, `dependsOn` copied from C-AA's row (sibling RA — verify: `jq '.slots["10"].model.dependencies[] | select(.activity=="C-AA")'`); C-EA appears in no critical-path chain.

- [ ] **Step 1:** Author the amended network model (current model + the C-EA row). Same scratch-checkout pre-validation as Task 5 Step 2 (expect zero Errors, no float/critical-path fields to recompute by hand — the network model stores authored dependencies; verify by diffing which fields exist on rows: `jq '.slots["10"].model | keys'`. If the model carries computed float/critical-path data, recompute via the estimation seam the project-manager rail uses — `estimationComputeNetwork` — rather than hand-editing numbers).
- [ ] **Step 2:** Write through the draft rail (as Task 5 Step 3 with `AIARCH_ARTIFACT_KIND=network`), message `"network amendment: add C-EA dependency row (episode-access coverage wave)"`.
- [ ] **Step 3:** Validate (zero Errors) + commit if the rail did not.

### Task 7: Acknowledge stale basis on slots 11–16

**Files:**
- Modify: `.aiarch/state/project.json` (slot staleness flags via the RA verb)

- [ ] **Step 1:** Find the precedent mechanism: `grep -l AcknowledgeStaleBasis .aiarch/state/applied_mutations/*.json | head` and `jq .verb` those records; locate the caller (server REST op or a Go entry point calling `GitStore.AcknowledgeStaleBasis`, `server/internal/resourceaccess/projectstate/projectstateaccess.go:366-382`).
- [ ] **Step 2:** Replicate it for slots 11,12,13,14,15,16 (kinds: normalSolution, subcriticalSolution, compressedSolution, decompressedSolution, riskModel, sdpReview — confirm kind↔slot mapping via `jq '.slots["11"].kind'` etc.). If the precedent path is unreachable locally, a small `go run` script in `server/` calling the RA directly against the local git substrate is acceptable (it writes real `applied_mutations` records — not synthetic).
- [ ] **Step 3:** Verify: `jq '[.slots | to_entries[] | select(.value.staleBasis == true) | .key]'` → expect `[]` (slot 10 was re-committed in Task 6; 11–16 acked). `./aiarch-state-mcp validate --root .` → zero Errors. Commit if the writes were hand-applied.

---

## Phase C — finish the activity record

### Task 8: Billing runtime honesty + tests green

**Files:**
- Verify/Modify: `server/internal/resourceaccess/merchantgateway/` (generated stub), `server/internal/manager/billing/`, `server/internal/engine/billing/`, `server/internal/resourceaccess/billingstate/`

**Interfaces:**
- Consumes: frozen contracts (generated `contract.gen.go` in each package).
- Produces: `GOWORK=off go test ./internal/manager/billing/... ./internal/engine/billing/... ./internal/resourceaccess/billingstate/... ./internal/resourceaccess/merchantgateway/...` green; every merchantgateway stub op returns an explicit not-configured error (not a silent success) — verify by reading `contract.gen.go`'s `stubMerchantGatewayAccess`; if any op fakes success, replace with `fwra.New(fwra.FailedPrecondition, "merchantgateway: Stripe not configured — see docs/billing-setup.md")`-shaped errors in a NON-generated file (a real implementation file that satisfies the interface and gets wired in `hooks.go`'s Finalize seam if the generated stub cannot be edited — generated files must not be hand-edited).

- [ ] **Step 1:** Run the four package test suites; record current state.
- [ ] **Step 2:** Read the merchantgateway stub; if ops already error not-configured, no code change — document that in the task notes. Otherwise implement the not-configured wrapper + unit test asserting the error class and message reference to `docs/billing-setup.md`.
- [ ] **Step 3:** Re-run suites green; commit `feat: explicit not-configured errors for unprovisioned merchant gateway` (only if code changed).

### Task 9: Re-record the C-BG SRS + contract-coverage sweep (rail writes)

**Files:**
- Modify (via construct rail): `.aiarch/state/project.json` `.phaseArtifacts`, possibly `.serviceContracts`

**Interfaces:**
- Consumes: the preserved SRS markdown in memory file `~/.claude/projects/-Users-davidmarne-mixofrealitystudio-archistrator/memory/archistrator-cbg-billingstateaccess-srs-draft.md` (content between the `~~~markdown` fences, verbatim).
- Produces: `.phaseArtifacts` SRS entry keyed `billingStateAccess`; any genuinely-missing service contracts recorded.

- [ ] **Step 1:** Construct-mode rail session (`AIARCH_JOB_MODE=construct AIARCH_ACTIVITY_ID=C-BG AIARCH_COMPONENT_ID=billing-state-access AIARCH_ARTIFACT_KIND=` as the binary requires — read `server/cmd/aiarch-state-mcp/main.go` for exact construct-mode env): `recordPhaseArtifact` with mapKey `billingStateAccess`, payload `{"SRS": {"component": "billingStateAccess", "content": "<the preserved markdown>"}}`, then `publishDraft`. Expected: ACCEPTED (the Task-5 amendment cleared the blocker). Keep the SRS's two open questions verbatim (verb-name drift; revenue-ledger homing) — architect deferred both to the settlement re-plan.
- [ ] **Step 2:** Coverage sweep: for each of the 25 code-layer components in slot 5 (`kind` ∈ client/manager/engine/resourceAccess), resolve a serviceContracts entry directly by `contractKey` or via facet doctrine (a contract whose `component` field names the component). Script it; expected result per prior recon: zero genuinely missing (29 keys cover the roster via 4 facet contracts). If a gap surfaces, record the contract through the rail (`recordServiceContract`) from the component's generated `contract.gen.go` + slot-5 verbs — and flag it in the task report.
- [ ] **Step 3:** Validate zero Errors; commit if the rail did not.

### Task 10: N-DEP earned review

**Files:**
- Modify: `.aiarch/state/project.json` (`activityConstruction["N-DEP"]`)

- [ ] **Step 1:** Verify the deployment against the running system (this is the review): `curl -sf https://archistrator.capture-gtd.com/` returns the SPA (or the OIDC redirect — a 2xx/3xx, not 5xx); `curl -s -o /dev/null -w '%{http_code}' https://archistrator.capture-gtd.com/api/v1/capabilities` (expect 200 or 401 — the endpoint exists behind auth, not 404/5xx); Argo application file exists and pins a real image tag (`../software/k8s/argocd/applications/archistrator-server.yaml`). Record each command + result in the produced note.
- [ ] **Step 2:** Hand-commit: `buildStatus` 1→2 on N-DEP, append a produced entry `{"Kind":"note","Title":"N-DEP — deployment review","Source":"","Produced":true,"Note":"Reviewed against the running system 2026-08-09: SPA serves at archistrator.capture-gtd.com, API responds behind OIDC, Argo app pinned+synced. Verdict: integrated. <the evidence>"}`. Validate; commit `state: N-DEP review performed and recorded — integrated`.

### Task 11: Lifecycle reconciliation hand-commit (the six + C-EA)

**Files:**
- Create: `scripts/finish-construction-reconcile.py` (pattern: `scripts/align-construction-phase.py`)
- Modify: `.aiarch/state/project.json` `.activityConstruction`

**Interfaces:**
- Produces: all 69 rows `phase:2, buildStatus:2`. Specifically:
  - `C-BG`: phase 3→2, buildStatus 3→2, clear `failureReason`/`failureDetail`, append note: `"Reconciled 2026-08-09: the failure was the slot-9 ACT-COMPONENT-COVERAGE blocker (fixed in the C-EA amendment), not the component. Requirements SRS re-recorded under phaseArtifacts.billingStateAccess; billing state access built (see server/internal/resourceaccess/billingstate); settlement integration deferred to the settlement re-plan — see docs/billing-setup.md."`
  - `C-WIA`: phase 3→2, buildStatus 3→2, clear failure fields, note: `"Reconciled 2026-08-09: completed with zero scope — the Work Item concept was descoped by founder ruling 2026-07-20 (slot-3 rejected[] ledger); residual surface is recordActivityWorkItemOpened on projectStateAccess (built under C-PA)."`
  - `C-BS`: phase 1→2, buildStatus →2, `completedAt` set, note referencing the contract-only settlement deferral (keep existing produced entries).
  - `C-BM`: phase 0→2, buildStatus →2, note: `"Built: server/internal/manager/billing (onboard, register-customer, close-cycle, shortfall-sweep workflows) against the frozen billingManager contract; gateway integration deferred pending Stripe configuration — see docs/billing-setup.md."`
  - `C-EA`: new row, phase 2, buildStatus 2, produced entries for the contract (`serviceContracts["episodeAccess"]`) and code (`server/internal/resourceaccess/episode`), note marking it the coverage-amendment reconciliation.
  - `R-BG`: buildStatus 1→2, note: `"Completed with zero scope: Stripe vendor provisioning deferred to the settlement re-plan (founder billing→settlement deferral). Setup steps documented in docs/billing-setup.md."` (keep existing produced note).
- No `startedAt` invention — set `completedAt: "2026-08-09T00:00:00Z"`-style stamps only where the row transitions to Done and lacks one.

- [ ] **Step 1:** Write the script (idempotent, asserts before/after states, prints a diff summary).
- [ ] **Step 2:** Run; verify `jq '[.activityConstruction[] | select(.phase != 2 or .buildStatus != 2)] | length'` → `0` and total rows = 69.
- [ ] **Step 3:** `./aiarch-state-mcp validate --root .` zero Errors; commit script + state: `state: finish-construction reconciliation — all 69 activities Done+Integrated`.

### Task 12: Recompute constructionProgress from the rows

**Files:**
- Extend: `scripts/finish-construction-reconcile.py` (second subcommand) or a sibling script
- Modify: `.aiarch/state/project.json` `.constructionProgress`

- [ ] **Step 1:** Read the existing shape (`jq '.constructionProgress'`): `HandOffModel`, `SupervisionCap`, `TotalWeeks`, `Week`, `points[]` (with `earnedPct`/`plannedPct`). Recompute from the rows + slot-9 effort: earned = Σ effortDays of Done+Integrated activities / Σ all = 100%; set `Week = TotalWeeks`; append/adjust the final points entry so the series ends at `earnedPct: 100`. Do NOT hand-type figures — derive them in the script from slot-9 `effortDays`.
- [ ] **Step 2:** Validate + commit `state: constructionProgress recomputed from the completed activity record`.

---

## Phase D — derived "Operating" presentation state

### Task 13: Server-side derivation + catalog projection field

**Files:**
- Modify: `.aiarch/state/project.json` `$defs.ProjectSummary` (add `ConstructionComplete`), regen `contract.gen.go`
- Create: `server/internal/resourceaccess/projectstate/operating.go` + `operating_test.go`
- Create: `server/internal/resourceaccess/projectstate/testdata/operating_fixtures.json` (the SHARED fixtures — architect condition)
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` (`ListProjects`, in the `readProjectForList` success branch)

**Interfaces:**
- Produces:
  - `func IsConstructionComplete(p Project) bool` — true iff `p.Phase == PhaseConstruction`, `len(p.ActivityConstruction) > 0`, and every row has `Phase == ActivityConstructionDone && BuildStatus == BuildIntegrated`.
  - `ProjectSummary.ConstructionComplete *bool` (json `ConstructionComplete,omitempty`; set only when true, mirroring `OperatorPaused`).
  - `testdata/operating_fixtures.json`: array of `{"name": string, "rows": [{"phase": int, "buildStatus": int}], "projectPhase": int, "expect": bool}` — cases: `all-integrated` (true), `one-failed` (false), `one-in-review` (false), `empty` (false), `skipped-shaped-row` (a Done+InReview row — false), `not-construction-phase` (all integrated but projectPhase 1 — false).

- [ ] **Step 1:** Write fixtures + failing Go test iterating them against `IsConstructionComplete`.
- [ ] **Step 2:** Run red → implement `operating.go` → green.
- [ ] **Step 3:** `$defs.ProjectSummary` gains `"ConstructionComplete": {"type":["null","boolean"]}` (match `OperatorPaused`'s existing schema shape exactly — inspect it first); `make gen-models`; wire into `ListProjects` next to the `OperatorPaused` block:

```go
if IsConstructionComplete(p) {
    complete := true
    summary.ConstructionComplete = &complete
}
```

- [ ] **Step 4:** `make gen-client-check` will show the OAS/client drift — regenerate (`make gen-client`), plus webApp `npm run gen:api` for `schema.ts`. Full server suite green. Commit `feat: derived construction-complete projection on the catalog summary`.

### Task 14: webApp Operating rendering

**Files:**
- Create: `webApp/src/contracts/operating.ts` + `webApp/src/contracts/operating.test.ts`
- Copy fixtures: `webApp/src/contracts/testdata/operating_fixtures.json` — byte-identical to Task 13's (add a comment in both test files: "shared fixtures — keep in sync with <other path>"; a drift check is `diff` in the systemtests or a repo script if trivial)
- Modify: `webApp/src/components/projectFormat.ts` (label), `webApp/src/components/ProjectCard.tsx` (chip), `webApp/src/components/AppShell.tsx` (chip), `webApp/src/contracts/adapters.ts` + `webApp/src/components/HomeBaseParts.tsx` (construction card DONE), `webApp/src/components/construction/ConstructionTracker.tsx` (completion panel), `webApp/src/components/construction/ConstructionConsole.tsx` (button)
- Modify: `webApp/src/contracts/wire.ts` (map `ConstructionComplete`)

**Interfaces:**
- Consumes: `Schemas['ProjectSummary'].ConstructionComplete` (Task 13); construction rows in `constructionAdapters`.
- Produces:
  - `deriveOperating(rows: {phase: ActivityConstructionPhaseLike, buildStatus: BuildStatusLike}[], projectPhase: ProjectPhase): boolean` in `operating.ts` — same semantics as the Go function, tested against the shared fixtures.
  - Catalog tile + AppShell chip render **"Operating"** when the summary flag (catalog) / derivation (project detail) is true.
  - HomeBase construction card: DONE treatment (reuse the existing DONE chip styling from `HomeBaseParts.tsx:51-52`) when operating; "Resume Construction" hidden.
  - ConstructionTracker: when every activity is integrated, the awaiting-pump `AwaitingPanel` is replaced by a completion panel — copy exactly: **"Construction complete — all 69 activities integrated."** with the final earned-value figure from `constructionProgress` (render the count dynamically from the rows, not a literal 69 — the copy string is `Construction complete — all ${n} activities integrated.`).
  - ConstructionConsole button: hidden (not relabeled) in the operating state.

- [ ] **Step 1:** Fixtures + failing `operating.test.ts` (node --test) → implement `deriveOperating` → green.
- [ ] **Step 2:** Rendering changes, driven by existing component test patterns where present; `npm run check` green (typecheck, eslint, prettier, tests, gen:api drift).
- [ ] **Step 3:** Commit `feat: Operating presentation state (derived, no enum change)`.

### Task 15: Milestone dependency fix in the SPA

**Files:**
- Modify: `webApp/src/contracts/constructionAdapters.ts` (`computeActivityStatuses`, lines ~177-245)
- Test: `webApp/src/contracts/constructionAdapters.test.ts` (or the file's existing test home — find it first)

**Interfaces:**
- Consumes: `network.milestones` (each with its own `dependsOn`), `network.dependencies`.
- Produces: milestone ids appearing in an activity's `dependsOn` are resolved recursively — a milestone is satisfied iff ALL of its own `dependsOn` are satisfied (with a visiting-set cycle guard mirroring server `resolveDependencySatisfied`, `server/internal/manager/construction/constructionmanager.go:982-1016`); milestone ids no longer materialize as phantom zero-day activity nodes in `projectAdapters.ts:395-421` (exclude ids present in `network.milestones` from the activity universe).

- [ ] **Step 1:** Failing test: network fixture where activity X depends on milestone M3, M3's dependsOn are Done → expect X `eligible` (today: `blocked`); plus a cycle fixture (M→M) → no infinite loop, dependent stays `blocked`.
- [ ] **Step 2:** Implement (pass milestones into `computeActivityStatuses`; recursive resolver with `visiting: Set<string>`); green; `npm run check`.
- [ ] **Step 3:** Commit `fix: SPA milestone dependency resolution mirrors server resolveDependencySatisfied`.

---

## Phase E — verify, document, ship

### Task 16: Stripe/billing setup guide

**Files:**
- Create: `docs/billing-setup.md`

- [ ] **Step 1:** Author the guide. Required content (derive exact envvar names from the restored slot-6 `bindings` entries for billing components + `server/cmd/server/config.gen.go` fields — do not invent names):
  - What works today without Stripe (billing workflows compute against the stub gateway; merchantgateway returns not-configured errors).
  - Stripe Connect setup (charge-only doctrine, NOT merchant-of-record): account creation, API keys (secret + publishable), webhook signing secret, Connect onboarding.
  - Every envvar/secret: name, which component reads it, where it lands in deployment (helm values / k8s Secret in `../software` `products/archistrator/helm/archistrator-server`), local `.env` equivalents (`server/.env` is gitignored — instructions only, no values).
  - The R-BG note: vendor account provisioning was deferred; doing it = this guide.
  - Pointer: full billing/settlement construction returns with the settlement re-plan (earmark ledger).
- [ ] **Step 2:** Commit `docs: billing/Stripe setup guide (deferred provisioning runbook)`.

### Task 17: testingState + final gate run

**Files:**
- Modify (via rail): `.aiarch/state/project.json` `.testingState`

- [ ] **Step 1:** Run EVERY gate: Task 4's full list + `systemtests` `make gen-check`. All green (fix-forward if not).
- [ ] **Step 2:** Record the run through the construct rail (`recordTestingState` — read its schema from the tool registry first; record the suites run + green result + date). Validate; commit if the rail did not.
- [ ] **Step 3:** Update `docs/bugs/2026-08-09-construction-open-items.md`: mark the slot-9/coverage blocker resolved (C-EA amendment), the codec round-trip drop fixed, and append the earmark ledger from the spec (reopen seam → settlement re-plan; F4 operations bridge; slot-5 verb drift). Commit.

### Task 18: Ship + browser verification

**Files:** none in this repo beyond pushes; one line in `../software`

- [ ] **Step 1: Push state + code**

```bash
git log origin/main..main --oneline | wc -l   # review what ships (expect ~40+ commits)
git push origin main
```

The push alone updates the deployed app's STATE (it reads project.json from GitHub main).

- [ ] **Step 2: Release + pin the new server/webapp images**

Watch `release.yml` (`gh run watch`); note the new versions from `server/VERSION` / webapp tag; edit `/Users/davidmarne/mixofrealitystudio/software/k8s/argocd/applications/archistrator-server.yaml` (and the webapp application file) `image.tag` to the new versions; commit + push that repo; wait for Argo sync (`kubectl -n archistrator get pods` if kube access exists, else poll the site).

- [ ] **Step 3: Browser-verify** (claude-in-chrome): load https://archistrator.capture-gtd.com/, open the archistrator project. Expect: catalog chip **Operating**; HomeBase construction card DONE; construction console shows **"Construction complete — all 69 activities integrated."** with the EV figure and NO "Resume construction" / awaiting-pump panel. Screenshot for the report.
- [ ] **Step 4:** If anything renders wrong, fix-forward (new commits, re-release if code) until the deployed app matches the spec.

---

## Self-review notes (done at authoring time)

- Spec coverage: Phase A→Tasks 1-4, B→5-7, C→8-12, D→13-15, E→16-18. Architect conditions encoded: hard gate (Global Constraints bullet 1), rail-before-hand-commit (Task 9-10 before 11), shared fixtures (13/14), no-forgery prohibitions (Global Constraints), copy string (Task 14), earned N-DEP review (Task 10), earmarks (Task 17 Step 3).
- Known unknowns delegated with explicit verify-first instructions: aiarch-state-mcp env conventions (Tasks 5/9), AcknowledgeStaleBasis mechanism (Task 7), network computed fields (Task 6), merchantgateway stub behavior (Task 8), `constructionProgress` exact shape (Task 12).
