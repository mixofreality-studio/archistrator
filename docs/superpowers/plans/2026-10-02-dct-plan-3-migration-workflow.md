# Deterministic Component Testing — Plan 3 of 3: Migration workflow

> **For agentic workers:** This plan is executed by the **Workflow tool** (founder opted in: "ultraplan a claude workflow … write and run it"). The script below is the deliverable; Tasks 1–2 are inline preparation, Task 3 launches it, Task 4 is the post-run merge gate.

**Goal:** Bring every construction activity of archistrator itself onto the new testing model: enrich the 17 use-case diagrams with I/O nodes, bind every projected scenario for all 28 components, generate and fill the black-box scenario tests, purge every non-generated test from component packages and the webApp, run the tests, record runs so the `testing` task renders, and drive `TestFileLayout` to zero.

**Architecture:** A fan-out over use cases (design enrichment, single-file barrier then one applier), then a per-component pipeline: bind → generate+fill+purge+run in an isolated worktree → adversarial verify → sequential integrate. Only one agent ever writes `project.json` at a time; component code changes are isolated per worktree and merged serially by one integrator. The script is deterministic; agents do the judgment.

**Tech Stack:** Workflow tool script (JS), aiarch-state MCP verbs (`listComponentScenarios`, `recordPhaseArtifact`, `record-test-run`), `make gen-tests` / `make test-scenarios` / `npm run test:scenarios`, git worktrees.

**Spec:** `docs/superpowers/specs/2026-10-02-deterministic-component-testing-design.md`. Prerequisites: Plan 1 released, Plan 2 merged into the `deterministic-component-testing` branch (worktree `../archistrator-dct`). The workflow runs **in that worktree**, not on main (another session is active on main).

## Global Constraints

- The workflow operates in `/Users/davidmarne/mixofrealitystudio/archistrator-dct` on branch `deterministic-component-testing`. Every agent prompt states that path; agents must not `cd` to the main checkout.
- `project.json` has exactly one writer at a time: the **applier** agents (Stage A3 and B1-apply). Binding and enrichment agents return JSON and never touch `.aiarch/`.
- Code-changing agents use `isolation: 'worktree'` and commit on their own branch (`dct/<component>`); the **integrator** merges serially and never force-pushes.
- No agent commits with `-a`/`-A`; explicit paths only.
- Agents never weaken a gate: no waivers added to `TestFileLayout`, no `//nolint` on arch tests, no `t.Skip` outside a `Skip` binding.
- Skips are allowed only per spec §3.1 (upstream not integrated). For archistrator every activity is Integrated, so **no skips**; a scenario an agent cannot bind is returned as `unbindable` with a reason and surfaces in the final report.
- Well above the 10-agent guideline: ≈ 17 + 1 + 28×3 + 2 ≈ **104 agents**. Founder-approved.
- `Date.now()`/`Math.random()` are unavailable in the script; the run stamp is passed via `args.stamp`.

## Review Focus

1. Two components in the **same Go package** (five in `projectstate`, two in `billingstate`) must not generate conflicting test files — handled by running the generate stage once per *package* (Stage B2 items are packages, not contracts) and pinned by the integrator's `make gen-tests-check` after each merge.
2. A binding agent that **invents** a scenario id not in `listComponentScenarios` must be rejected before apply — the applier validates ids against the derived set and the TP-BOUND rule rejects the write.
3. The webApp (`webClient`) binding must target existing `data-testid`s from `webApp/src/.../UIIdentifiers.ts`; a bound testid that does not exist fails at generation review (verify stage checks `grep`).
4. The purge must delete `webApp/src/**/*.test.ts` (131 files) and `uitests/tests/**` but **not** `server/cmd/**/*_test.go`, `server/internal/*_test.go` gates, or utility tests — pinned by the integrator's allowlist diff check.
5. A component whose scenario set is **empty** (no use case reaches it: `TP-OP-REACHED` warnings on every op) must not silently pass with zero tests — it is reported as `uncovered` for a founder ruling, and its package keeps a generated file with zero scenarios so the arch rule still holds.

---

### Task 1: Inline preparation (before the workflow)

- [ ] **Step 1: Confirm prerequisites in the worktree**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-dct
git log --oneline -1   # Plan 2's last commit
cd server && GOWORK=off go build ./... && make gen-models-check && GOWORK=off go run ./cmd/aiarch-state-mcp --help | grep -q record-test-run
```

- [ ] **Step 2: Snapshot the design-health baseline**

Run the methodcheck CLI (or `make method-check`) and save `UC-NO-IO`, `TP-OP-REACHED`, `CC-*` findings to `docs/bugs/2026-10-02-dct-baseline-findings.md`. This is the "before" the final report diffs against.

- [ ] **Step 3: Build the item lists** (inline `jq`, pasted into `args`)

```bash
jq -c '[.. | objects | select(has("id") and has("activity") and (.activity|type=="object")) | {id, name}]' .aiarch/state/project.json           # 17 use cases
jq -c '[.serviceContracts | to_entries[] | {component: .key, layer: .value.layer, goPackage: .value.goPackage, ops: (.value.interface.operations|map(.name))}]' .aiarch/state/project.json   # 28 contracts
```

- [ ] **Step 4: Re-derive project design** (Plan 2 Task 4 removed N-STP/N-IT from the derivation; the committed `.activityList`/`.network` still carry them). Run the project-design re-derivation through the app's own rail (`execute-a-project-activity` on Project Design is one review task, spec R7): `GOWORK=off go run ./cmd/archistrator plan rederive --project archistrator` (or whichever CLI entry Plan 2's Task 4 documents), approve the SDP review task in the webApp, confirm `jq '.activityList.activities[] | select(.name=="N-STP" or .name=="N-IT")'` is empty. Commit by explicit path.

---

### Task 2: The workflow script

Save as `docs/superpowers/workflows/dct-migration.js` (also passed inline on first launch; subsequent iterations use `scriptPath`).

```js
export const meta = {
  name: 'dct-migration',
  description: 'Migrate every archistrator construction activity to deterministic component scenario tests',
  phases: [
    { title: 'Enrich use cases', detail: 'one architect per use case: sendSignal/objectNode/note.anchor' },
    { title: 'Apply design', detail: 'single applier writes the 17 diagram patches through aiarch-state' },
    { title: 'Bind', detail: 'one test-engineer per component binds every projected scenario' },
    { title: 'Apply bindings', detail: 'single applier records test plans' },
    { title: 'Construct', detail: 'one junior per Go package / client: gen, fill hooks, purge, run (worktree)' },
    { title: 'Verify', detail: 'adversarial reviewer per package' },
    { title: 'Integrate', detail: 'serial merges with gates after each' },
    { title: 'Report', detail: 'completeness critic + founder report' },
  ],
}

const ROOT = args.root            // '/Users/davidmarne/mixofrealitystudio/archistrator-dct'
const USE_CASES = args.useCases   // [{id,name}]
const CONTRACTS = args.contracts  // [{component,layer,goPackage,ops}]
const STAMP = args.stamp          // 'YYYY-MM-DDTHH-MM' from the launcher

const SPEC = 'docs/superpowers/specs/2026-10-02-deterministic-component-testing-design.md'

// ---------- schemas ----------
const DIAGRAM_PATCH = {
  type: 'object',
  properties: {
    useCase: { type: 'string' },
    addNodes: { type: 'array', items: { type: 'object', properties: {
      id: { type: 'string' }, kind: { type: 'string', enum: ['sendSignal', 'objectNode', 'note'] },
      label: { type: 'string' }, linkedActorId: { type: 'string' }, inState: { type: 'string' }, anchor: { type: 'string' },
      after: { type: 'string', description: 'existing node id this node is spliced after on the control flow' },
    }, required: ['id', 'kind', 'label', 'after'] } },
    retypeNodes: { type: 'array', items: { type: 'object', properties: {
      id: { type: 'string' }, kind: { type: 'string', enum: ['sendSignal', 'acceptEvent'] }, linkedActorId: { type: 'string' },
      why: { type: 'string' } }, required: ['id', 'kind', 'why'] } },
    rationale: { type: 'string' },
  },
  required: ['useCase', 'addNodes', 'retypeNodes', 'rationale'],
}

const APPLY_RESULT = {
  type: 'object',
  properties: {
    applied: { type: 'array', items: { type: 'string' } },
    rejected: { type: 'array', items: { type: 'object', properties: { id: { type: 'string' }, reason: { type: 'string' } }, required: ['id', 'reason'] } },
    commit: { type: 'string' },
    findings: { type: 'array', items: { type: 'string' }, description: 'methodcheck findings after apply (UC-IO-KINDS, UC-NO-IO, CC-*)' },
  },
  required: ['applied', 'rejected', 'commit', 'findings'],
}

const BINDINGS = {
  type: 'object',
  properties: {
    component: { type: 'string' },
    scenarioCount: { type: 'integer' },
    testPlan: { type: 'object', description: 'exact TestPlanRecord payload: {component, bindings:[{scenario, steps:[{seq, operation, inputs, expect, probes, unobservable, hook}]}]}' },
    unbindable: { type: 'array', items: { type: 'object', properties: { scenario: { type: 'string' }, reason: { type: 'string' } }, required: ['scenario', 'reason'] } },
    deadOps: { type: 'array', items: { type: 'string' }, description: 'contract ops reached by no scenario (from TP-OP-REACHED)' },
  },
  required: ['component', 'scenarioCount', 'testPlan', 'unbindable', 'deadOps'],
}

const CONSTRUCT_RESULT = {
  type: 'object',
  properties: {
    pkg: { type: 'string' }, branch: { type: 'string' }, commit: { type: 'string' },
    generated: { type: 'array', items: { type: 'string' } },
    purged: { type: 'array', items: { type: 'string' } },
    scenarios: { type: 'object', properties: { pass: { type: 'integer' }, fail: { type: 'integer' }, skip: { type: 'integer' } }, required: ['pass', 'fail', 'skip'] },
    runRecorded: { type: 'boolean' },
    blockers: { type: 'array', items: { type: 'string' } },
  },
  required: ['pkg', 'branch', 'commit', 'generated', 'purged', 'scenarios', 'runRecorded', 'blockers'],
}

const VERDICT = {
  type: 'object',
  properties: {
    pkg: { type: 'string' },
    accept: { type: 'boolean' },
    problems: { type: 'array', items: { type: 'string' } },
    weakenedGate: { type: 'boolean' },
    hiddenWhiteBox: { type: 'boolean' },
  },
  required: ['pkg', 'accept', 'problems', 'weakenedGate', 'hiddenWhiteBox'],
}

const MERGE_RESULT = {
  type: 'object',
  properties: {
    merged: { type: 'array', items: { type: 'string' } },
    failed: { type: 'array', items: { type: 'object', properties: { branch: { type: 'string' }, reason: { type: 'string' } }, required: ['branch', 'reason'] } },
    fileLayoutViolations: { type: 'integer' },
    allTestsGreen: { type: 'boolean' },
  },
  required: ['merged', 'failed', 'fileLayoutViolations', 'allTestsGreen'],
}

// ---------- Phase A: enrich use cases (fan-out, barrier is correct: single file) ----------
phase('Enrich use cases')
const patches = (await parallel(USE_CASES.map(uc => () => agent(`
You are the system-architect. Repo: ${ROOT} (branch deterministic-component-testing). Read ${SPEC} §2.2 first.
Use case "${uc.name}" (id ${uc.id}) in .aiarch/state/project.json (committed core-use-cases slot). Read its activity diagram AND its dynamic view(s) in the system slot.
Return a DIAGRAM PATCH (do not edit any file):
- addNodes: objectNode (UML ObjectNode with inState) after every action whose outcome is an observable state change ("Record X", "Commit Y", "Close period"…): label = the domain object, inState = "[state]". sendSignal after/instead-of system actions whose target is an actor (notifications, escalations, invoices). Anchored notes only where a value convention matters for binding.
- retypeNodes: actions sitting in an ACTOR lane that are really outputs to that actor (e.g. "Escalate to the operator") → sendSignal. Actions in an actor lane that are inputs stay as they are.
- Keep ids kebab-case, unique within the diagram. Do not remove or rename existing nodes. Do not add decisions.
Rationale: 3-6 lines on which stimuli/outcomes the IOAD view will now see.
`, { label: `enrich:${uc.id}`, phase: 'Enrich use cases', schema: DIAGRAM_PATCH, agentType: 'system-architect' })))).filter(Boolean)
log(`enrichment patches: ${patches.length}/${USE_CASES.length}`)

phase('Apply design')
const design = await agent(`
You are the single writer of .aiarch/state/project.json in ${ROOT}. Apply these diagram patches through the aiarch-state MCP (putDraftModel on the core-use-cases slot, then publishDraft; mode per CLAUDE.md), one use case at a time, in this order. For each addNode, splice it on the control flow after node "after" (edge after→new, new→old-successor; copy the guard onto the first edge only). For retypeNodes change kind (+linkedActorId). Reject (do not apply) any node whose "after"/"anchor" id does not exist, and list it.
After all: run make method-check in server/ and report every UC-IO-KINDS / UC-NO-IO / CC-* finding. Commit ONLY .aiarch/state/project.json with message "design(dct): I/O-explicit nodes on 17 use cases (${STAMP})".
PATCHES:
${JSON.stringify(patches, null, 1)}
`, { label: 'apply:design', phase: 'Apply design', schema: APPLY_RESULT, effort: 'high' })
if (!design) throw new Error('design apply failed')
log(`design applied: ${design.applied.length} patches, ${design.rejected.length} rejected, ${design.findings.length} findings, commit ${design.commit}`)

// ---------- Phase B: bind per component (fan-out), then one applier ----------
phase('Bind')
const BINDABLE = CONTRACTS.filter(c => c.goPackage || c.layer === 'Client')
const bindings = (await parallel(BINDABLE.map(c => () => agent(`
You are the test-engineer. Repo ${ROOT}. Read ${SPEC} §2.4, §3 and the command .claude/commands/service-test-plan.md (frontend-test-plan.md for the webClient).
Component: ${c.component} (layer ${c.layer}, package ${c.goPackage || 'n/a'}, ops: ${c.ops.join(', ')}).
1. Call mcp__aiarch-state__listComponentScenarios {"component":"${c.component}"}. That list is the ONLY set of scenarios; do not add or rename any.
2. For EVERY scenario produce a binding: per stimulus choose "operation" (must equal input.op when resolved; else the contract op the call means), concrete "inputs" valid against the op's param schemas in .serviceContracts.${c.component}.interface, "expect" (result subset or {errorExpected,errorCode}), "probes" with "for" = node id for each expectedState and observable expectedOutput (read-only ops only), "unobservable" for outputs nothing can observe, "hook": true when arranging the guards needs setup code.
${c.layer === 'Client' ? '3. For the webClient: inputs are user actions {name:"action", value:{kind:"click"|"fill"|"navigate", target:"<data-testid from webApp/src UIIdentifiers.ts>", value?}}; expect.result is {"visible":"<testid>"} | {"text":{"<testid>":"..."}} | {"url":"..."}. Every target MUST exist in UIIdentifiers.ts (grep it).' : ''}
Do NOT write to project.json or call recordPhaseArtifact. Return the TestPlanRecord payload. Scenarios you genuinely cannot bind go in "unbindable" with the reason. List ops the scenarios never reach in deadOps (these are TP-OP-REACHED warnings; do not try to fix the design).
`, { label: `bind:${c.component}`, phase: 'Bind', schema: BINDINGS, agentType: 'test-engineer' })))).filter(Boolean)
log(`bindings: ${bindings.length}/${BINDABLE.length}; unbindable ${bindings.reduce((n, b) => n + b.unbindable.length, 0)}; deadOps ${bindings.reduce((n, b) => n + b.deadOps.length, 0)}`)

phase('Apply bindings')
const bindApply = await agent(`
You are the single writer of .aiarch/state/project.json in ${ROOT}. For each test plan below, in order: call mcp__aiarch-state__recordPhaseArtifact {"mapKey":"<component>","payload":{"testPlan":<record>}} (construct mode, activity = the component's construction activity id from .activityList). The verb validates TP-*; on failure, fix ONLY mechanical issues (seq numbering, a wrong param name you can read off the contract) and retry once; otherwise list it as rejected with the finding text. Then publishDraft once. Commit ONLY .aiarch/state/project.json: "testplan(dct): bindings for ${BINDABLE.length} components (${STAMP})".
Report methodcheck findings after apply (TP-* and TP-OP-REACHED warnings).
PLANS:
${JSON.stringify(bindings.map(b => b.testPlan), null, 1)}
`, { label: 'apply:bindings', phase: 'Apply bindings', schema: APPLY_RESULT, effort: 'high' })
if (!bindApply) throw new Error('binding apply failed')
log(`bindings applied: ${bindApply.applied.length}, rejected ${bindApply.rejected.length}, commit ${bindApply.commit}`)

// ---------- Phase C: construct per Go package / client, verify, integrate (pipeline) ----------
const pkgs = {}
for (const c of BINDABLE) {
  const key = c.layer === 'Client' ? `client:${c.component}` : c.goPackage
  pkgs[key] = pkgs[key] || { key, layer: c.layer, components: [] }
  pkgs[key].components.push(c.component)
}
const PKGS = Object.values(pkgs)
log(`construct units: ${PKGS.length} (${PKGS.filter(p => p.layer !== 'Client').length} Go packages + ${PKGS.filter(p => p.layer === 'Client').length} clients)`)

const built = await pipeline(PKGS,
  p => agent(`
You are the junior-developer. You are in an isolated worktree of ${ROOT}; create and stay on branch dct/${p.key.replace(/[^a-zA-Z0-9]+/g, '-')}. Read ${SPEC} §4, §5.1 and .claude/commands/${p.layer === 'Client' ? 'frontend' : 'service'}-construction.md.
Unit: ${p.key} — components ${p.components.join(', ')}.
1. cd server && make gen-tests. Confirm the generated ${p.layer === 'Client' ? 'uitests/generated/<client>/*.spec.gen.ts + hooks.ts' : '<stereotype>_scenarios_test.gen.go + <stereotype>_hooks_test.go in ' + p.key} exist.
2. Fill EVERY FILL in the hooks file: newSubject_<Contract> builds the real component against scenariohost.Host (Temporal dev server, Postgres testcontainer unless -short, FakeGitHub/LocalGitRepo); Step_* arranges the guards. Use only the component's public contract, generated fakes for OTHER repos' resources, and scenariohost. No internals.
3. Purge: delete every other _test.go in the package${p.layer === 'Client' ? ' / every webApp/src/**/*.test.ts and uitests/tests/**' : ''}. Do NOT touch server/cmd/**, server/internal/*_test.go gates, or utility tests. Move any behaviour those tests pinned that a scenario does not cover into the final "blockers" list (do not re-add tests).
4. Run: ${p.layer === 'Client' ? 'cd uitests && npm run test:scenarios' : 'cd server && make test-scenarios'} until green. Then record the run: server/bin/aiarch-state-mcp record-test-run --activity <construction activity id of the first component> --component <component> --run-id local-${STAMP}-${p.key.replace(/[^a-zA-Z0-9]+/g, '-')} --artifact "$PWD/test-results" --results test-results (once per component in this unit).
5. Lint: golangci-lint run ./internal/...; gofmt; go vet. Commit by explicit paths: "test(dct): ${p.key} scenario tests; purge in-package tests".
Never add waivers, nolint on arch tests, or t.Skip. If a scenario cannot pass without changing production code, STOP, list it in blockers, leave the test failing and commit anyway.
`, { label: `construct:${p.key}`, phase: 'Construct', schema: CONSTRUCT_RESULT, isolation: 'worktree', agentType: 'junior-developer', effort: 'high' }),
  (r, p) => r && agent(`
Adversarial review of branch ${r.branch} (commit ${r.commit}) in ${ROOT} for unit ${p.key}. Check out the branch read-only (git worktree add or git show). Default to accept=false unless you can prove each:
- Every test file in ${p.key} is <stereotype>_scenarios_test.gen.go or <stereotype>_hooks_test.go, both package *_test; no other _test.go remains. ${p.layer === 'Client' ? 'No webApp/src/**/*.test.ts or uitests/tests/** remain. Every data-testid used in generated specs exists in UIIdentifiers.ts (grep).' : ''}
- The hooks file imports only the component package, scenariohost, testinfra packages, generated fakes and stdlib — no internal imports, no reflection into unexported state (hiddenWhiteBox=true otherwise).
- make gen-tests-check is clean on the branch; the generated file matches project.json.
- No waiver, nolint on arch tests, or t.Skip added (weakenedGate=true otherwise).
- test-results/<component>/results.json has every projected scenario (compare with listComponentScenarios) and statuses match the reported counts ${JSON.stringify(r.scenarios)}.
List concrete problems with file:line.
`, { label: `verify:${p.key}`, phase: 'Verify', schema: VERDICT, effort: 'high' }).then(v => ({ build: r, verdict: v })),
)

const accepted = built.filter(Boolean).filter(x => x.verdict && x.verdict.accept && !x.verdict.weakenedGate && !x.verdict.hiddenWhiteBox)
const rejected = built.filter(Boolean).filter(x => !(x.verdict && x.verdict.accept && !x.verdict.weakenedGate && !x.verdict.hiddenWhiteBox))
log(`construct: ${accepted.length} accepted, ${rejected.length} rejected, ${PKGS.length - built.filter(Boolean).length} lost`)

phase('Integrate')
const merge = await agent(`
You are the integrator in ${ROOT} on branch deterministic-component-testing. Merge these branches ONE AT A TIME in the given order (no squash, no force): ${JSON.stringify(accepted.map(a => a.build.branch))}.
After EACH merge run: cd server && make gen-tests-check && GOWORK=off go test ./internal/ -run 'TestFileLayout|TestGeneratedOnlyPublic|TestMethodLayering' && GOWORK=off go build ./... . If a merge conflicts or a gate fails, abort that merge (git merge --abort), record {branch, reason}, continue with the next.
At the end run the full suite: GOWORK=off go test ./... -short in server/; cd webApp && npm run check; cd uitests && npm run test:scenarios. Report fileLayoutViolations = the count of scenario-tests-only/test-package-not-external lines in TestFileLayout output (0 is the goal), and allTestsGreen.
Also assert the purge allowlist: git diff --stat main..HEAD -- server/cmd server/internal/arch_test.go 'server/internal/*_test.go' server/internal/utility | grep -c _test.go must be 0 deletions outside component packages — report as a failed item if not.
`, { label: 'integrate', phase: 'Integrate', schema: MERGE_RESULT, effort: 'high' })

phase('Report')
const critic = await agent(`
Completeness critic for the DCT migration in ${ROOT}. Inputs:
- design apply: ${JSON.stringify(design)}
- binding apply: ${JSON.stringify({ applied: bindApply.applied, rejected: bindApply.rejected, findings: bindApply.findings })}
- unbindable: ${JSON.stringify(bindings.flatMap(b => b.unbindable.map(u => ({ component: b.component, ...u }))))}
- deadOps: ${JSON.stringify(bindings.flatMap(b => b.deadOps.map(o => b.component + '.' + o)))}
- rejected units: ${JSON.stringify(rejected.map(x => ({ pkg: x.build.pkg, problems: x.verdict ? x.verdict.problems : ['no verdict'] })))}
- blockers: ${JSON.stringify(accepted.flatMap(a => a.build.blockers.map(b => a.build.pkg + ': ' + b)))}
- merge: ${JSON.stringify(merge)}
Write docs/bugs/2026-10-02-dct-migration-report.md: (1) what is green, (2) every founder ruling needed (dead ops vs missing use cases, unbindable scenarios, behaviours the purged tests pinned that no scenario covers), (3) exact remaining red (TestFileLayout count, failing scenarios by id), (4) the ordered checklist to merge the branch. Diff against docs/bugs/2026-10-02-dct-baseline-findings.md. Commit that one file.
Return the report path and a 10-line summary.
`, { label: 'report', phase: 'Report', effort: 'high' })

return { design, bindApply: { applied: bindApply.applied.length, rejected: bindApply.rejected }, units: PKGS.length, accepted: accepted.length, rejected: rejected.map(x => x.build.pkg), merge, critic }
```

**Why barriers where they are:** `parallel` before `apply:design` and `apply:bindings` is correct — both write one file and need the complete set. `pipeline` for construct→verify so a slow package (projectstate, five contracts) does not hold the others. `integrate` is a single sequential agent on purpose: merges and gates must be serial.

---

### Task 3: Launch

- [ ] **Step 1:** In this session, with the worktree on Plan 2's final commit:

```
Workflow({ script: <Task 2 script>, args: { root: '/Users/davidmarne/mixofrealitystudio/archistrator-dct', useCases: <jq list>, contracts: <jq list>, stamp: '<date +%Y-%m-%dT%H-%M>' } })
```

- [ ] **Step 2:** Watch `/workflows`. If an agent dies, the result is `null` and the script continues; resume with `resumeFromRunId` after fixing the cause — cached stages replay instantly.

- [ ] **Step 3:** Read `journal.jsonl` before trusting the return value (an empty `accepted` means the construct stage returned nulls, not that nothing needed doing).

---

### Task 4: Post-run merge gate (inline)

- [ ] **Step 1:** Open `docs/bugs/2026-10-02-dct-migration-report.md`; resolve founder rulings (dead ops, unbindable, uncovered behaviours). Each ruling either adds a use case / objectNode (re-run Phase A for that use case via a targeted `resumeFromRunId` with an edited item list) or deletes an op (a model edit).
- [ ] **Step 2:** `TestFileLayout` must be **0**; `make gen-tests-check` clean; `go test ./... -short`, `npm run check`, `npm run test:scenarios` green in the worktree.
- [ ] **Step 3:** Message session archistrator-6b (per the agreement): what Plan 2 changed in `deliverymanager.go`/`deliveryactivity.go`/`estimationengine.go`, and ask what landed from their side; rebase `deterministic-component-testing` onto main; re-run step 2.
- [ ] **Step 4:** Merge to main by explicit fast-forward or `--no-ff` merge commit; drain in-flight lifecycle workflows for removed activity types before deploy (memory: `temporal schedule list` shows four+ on a healthy deploy); release server + webapp tags.
- [ ] **Step 5:** Update memory: `archistrator-unified-activity-experience.md` NEXT pointer → testing wave shipped; new memory file for this wave's earmarks.

---

## Self-review

- **Spec coverage:** §2.2 additions on real diagrams → Phase A; §3 bindings → Phase B; §4 generation + hooks → Construct; §5.1 purge + gate → Construct + Integrate; §6 runs recorded so the testing task renders → Construct step 4 (`record-test-run`, local artifact dir); §7 removal of hand-written tests → Construct step 3; §9.3–9.4 → Tasks 3–4.
- **Placeholder scan:** the only open input is the project-design re-derivation CLI entry in Task 1 step 4, which Plan 2 Task 4 must name; executors use whatever it documents.
- **Type consistency:** `BINDINGS.testPlan` is the `TestPlanRecord` from Plan 2 Task 1; `record-test-run` flags match Plan 2 Task 3; branch names `dct/<key>` are shared between construct and integrate via `CONSTRUCT_RESULT.branch`.
- **Review Focus:** 1 → `PKGS` grouping + integrator gen-check; 2 → applier validation + TP-BOUND; 3 → verify stage grep; 4 → integrator allowlist diff; 5 → critic's "uncovered" section and the zero-scenario generated file (testgen emits the file even with zero bindings — Plan 1 Task 7 must honour that: emit `TestMain` + no `TestScenario_*` when the plan has zero scenarios).
