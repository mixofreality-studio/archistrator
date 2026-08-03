# SP2 — Bench Repo + Deterministic Runner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the `archistrator-bench` sibling repo — a deterministic MCP-driven runner that builds a fixed-input reference app end-to-end with zero human interaction, an immutable per-run archive keyed to the archistrator commit that built it, and the todomvc benchmark (pinned inputs + frozen acceptance suite). gtd and archistrator benchmarks ship defined-but-unrun.

**Architecture:** Two layers per spec §6, refined by the 2026-08-02 recon + founder ruling. (1) A **deterministic runner** (TypeScript, Node) that provisions a scratch archistrator project, boots the local stack, drives the full rail through archistrator's `/mcp` streamable-HTTP surface via a fixed op sequence with **ruled policies** at the three caller-driven points (no LLM operator, no browser — the Playwright-fallback list is empty at 43↔43 MCP/REST parity), polls to construction completion, and harvests. (2) An **immutable archive** `runs/<benchmark>/<runId>/` holding the built-app snapshot, project.json, traces, acceptance results, metrics.json, and run.json (archistrator commit SHA + config epoch). A bench-repo CI gate forbids any mutation under `runs/**`.

**Tech Stack:** TypeScript/Node (runner + acceptance harness driver), the MCP TypeScript SDK (`@modelcontextprotocol/sdk`) streamable-HTTP client, Vitest (runner unit tests), Playwright (todomvc acceptance suite only — testing the *built app*, not archistrator), a checked-in archistrator build (or a path to a local checkout) as the system-under-test.

## Global Constraints

- **This is a NEW sibling repo** `archistrator-bench` (founder ruling), NOT inside the archistrator repo. Create it at `/Users/davidmarne/mixofrealitystudio/archistrator-bench` (sibling of `archistrator`). All paths below are relative to that repo root unless prefixed `archistrator/`.
- **Configuration identity = the archistrator commit SHA** that built the run. Recorded in `run.json`; every metric/comparison keys on it.
- **Operator skew = zero.** The runner is deterministic. The three caller-driven decision points use fixed ruled policies (below), never an LLM: SDP option = the assembled review's `.Recommendation`; construction escalation (`StageAwaitingTakeover`) = override `retry` once, then `skip`; risk-floor approval (`StageAwaitingApproval`) = approve. Every policy application is logged to `run.json.operatorActions[]` with the input state, so a policy firing is auditable and never silent.
- **Immutability:** nothing under `runs/**` is ever modified or deleted after a run seals. Enforced by a CI check (Task 9). Failed runs are archived too, marked `outcome: failed`.
- **Frozen acceptance suites** are versioned; a suite change starts a new comparability epoch recorded in `run.json.epoch`. The runner never edits a suite mid-run.
- **One project per local state repo; one `archistrator serve` per port** (recon §4). Each run gets a fresh scratch state repo.
- **`.aiarch/traces/` MUST be gitignored in the scratch state repo** or dispatch is refused (recon §4, `assertNoTrackedTraceFiles`). The state repo must be **non-bare** with `git config receive.denyCurrentBranch updateInstead` (recon §4).
- **Real local construction requires** (recon §4, all of): `ARCHISTRATOR_CONSTRUCTION_DRYRUN=false`, `ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true`, no `ARCHISTRATOR_CONSTRUCTION_REPO_OWNER/_NAME`, a `file://` state repo URL, an `aiarch-state-mcp` binary discoverable, and `claude` authenticated on the server process's PATH.
- **Consent-filter seam** (spec §3 learning-opt-in deferred): the archive-ingest boundary carries a one-line `learningConsent` field on `run.json` (bench runs = true); no consent infra beyond the field.
- Commit after every task; commit messages end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- The archistrator repo is NOT modified by this plan (SP2 is bench-repo-only). If a driving gap is found that needs an archistrator change, that is a NEEDS_CONTEXT escalation, not an archistrator edit.

## Exact archistrator drive surface (from recon — treat as ground truth)

MCP mount `http://127.0.0.1:<port>/mcp` (streamable HTTP, `Mcp-Session-Id`); `archistrator serve` forces `ARCHISTRATOR_AUTH_DEV_MODE=true` so no bearer token is needed; dev subject `dev-architect` is the `owner`. `ArtifactKind` is an **integer** on the wire. Phase-1 kinds auto-run; Phase-2 kinds `[8,9,10,11,14,12,13,15]` must be requested per-kind in that order. Tools used by the runner:

```
systemDesignCreateProject(owner, name) -> pid (== name)
constructionSetReviewPolicy(pid, "vibes")            # already default; explicit is free
systemDesignSetResearchInput(pid, {sources:[{title,content}]})   # HARD precondition
systemDesignStartSystemDesign(pid)                   # runs all 8 Phase-1 steps + seal
systemDesignGetProject(pid)                          # whole-project state probe (Phase, ActivityConstruction)
projectDesignRequestArtifactDraft(pid, kind)         # per Phase-2 kind
projectDesignGetSessionState(pid, kind)              # poll Stage==Committed(4)
projectDesignRequestSDPCommit(pid)                   # assemble 4 options + risk model
projectDesignGetSessionState(pid, 16)                # read .Draft for optionIDs + .Recommendation
projectDesignSubmitSDPDecision(pid, SDPCommit, optionID)   # UNAVOIDABLE ratification
projectDesignAdvanceToConstruction(pid, acknowledgeStale=true)
constructionExecuteNextActivity(pid, tickID)         # pump self-cascades
constructionGetSessionState(pid, activityID|"")      # per-activity / whole-network stage
constructionSubmitPhaseDecision(pid, activityID, phase, "approve", feedback)  # risk-floor gate
constructionOverrideActivity(pid, activityID, {kind, notes, comments})        # escalation
```

Stage enums: Phase-2 SessionStage `Committed=4`; ConstructionStage `{1 dispatching,2 pipelineRunning,3 reviewing,4 awaitingTakeover,5 paused,6 exited,7 awaitingApproval}`. Built app = `git log main` in the state repo (activity branches `--no-ff` merged under vibes). **Do NOT call AskQuestions** (keeps the review ledger clean so the vibes autogate never holds).

---

### Task 1: Bench repo scaffold + immutability contract

**Files:**
- Create: `archistrator-bench/package.json`, `tsconfig.json`, `.gitignore`, `.nvmrc`, `README.md`, `vitest.config.ts`
- Create: `archistrator-bench/schema/run.schema.json`, `archistrator-bench/schema/metrics.schema.json` (typed archive contracts)
- Create: `archistrator-bench/runs/.gitkeep`

**Interfaces:**
- Produces: the repo skeleton and the two JSON Schemas that every archived `run.json` / `metrics.json` validates against (later tasks import these). `RunRecord` fields: `runId, benchmark, archistratorCommit, archistratorDirty (bool), operatorPolicyVersion, epoch, suiteVersion, startedAt, endedAt, outcome (succeeded|failed), learningConsent, operatorActions[], gaps[], stackConfig`. `Metrics` is SP3's concern — here define only the envelope (`runId, benchmark, schemaVersion`) so the archive validates; SP3 fills the body.

- [ ] **Step 1:** `cd /Users/davidmarne/mixofrealitystudio && mkdir archistrator-bench && cd archistrator-bench && git init && git config receive.denyCurrentBranch updateInstead` (the bench repo itself; the SCRATCH state repos are separate, created per-run in Task 4).
- [ ] **Step 2:** Write `package.json` (type: module; scripts: `test` → vitest, `bench` → the runner CLI entry from Task 8, `lint` → eslint, `typecheck` → tsc --noEmit), `tsconfig.json` (strict, NodeNext), `.nvmrc` (node 22), `.gitignore` (`node_modules/`, `dist/`, `.scratch/`, `*.log`).
- [ ] **Step 3:** Author `schema/run.schema.json` and `schema/metrics.schema.json` (JSON Schema draft 2020-12) with the RunRecord fields above; `additionalProperties: false`.
- [ ] **Step 4:** `README.md` — one-paragraph purpose, the "immutable archive, never edit runs/**" rule stated prominently, and the run command.
- [ ] **Step 5: Failing test** `test/schema.test.ts`: a minimal valid RunRecord validates; a RunRecord missing `archistratorCommit` fails; an extra property fails. Use `ajv`.
- [ ] **Step 6:** `npm install`, run vitest → tests pass. **Commit** (`chore: bench repo scaffold + archive schemas`).

---

### Task 2: MCP client wrapper (typed archistrator drive surface)

**Files:**
- Create: `archistrator-bench/src/mcp/client.ts`, `archistrator-bench/src/mcp/ops.ts`
- Test: `archistrator-bench/test/mcp-ops.test.ts`

**Interfaces:**
- Consumes: `@modelcontextprotocol/sdk` streamable-HTTP client.
- Produces: `class ArchistratorMcp` wrapping the SDK client — `connect(mcpUrl)`, `call(tool, args): Promise<result>`, `close()`. Plus `ops.ts`: thin typed wrappers for exactly the tools in the drive surface above (e.g. `createProject(owner, name)`, `setResearchInput(pid, sources)`, `startSystemDesign(pid)`, `getProject(pid)`, `requestPhase2Draft(pid, kind)`, `getPhase2Session(pid, kind)`, `requestSdpCommit(pid)`, `submitSdpDecision(pid, optionId)`, `advanceToConstruction(pid)`, `executeNextActivity(pid, tickId)`, `getConstructionSession(pid, activityId)`, `submitPhaseDecision(...)`, `overrideActivity(...)`). Each returns the parsed tool result; ArtifactKind passed as integer.

- [ ] **Step 1: Failing test** `mcp-ops.test.ts`: against a stub MCP server (a tiny in-test `http.Server` speaking the streamable-HTTP tool-call protocol, or the SDK's in-memory transport if available), assert `createProject` sends tool name `systemDesignCreateProject` with `{owner, name}` and returns the parsed pid; assert `requestPhase2Draft(pid, 8)` sends integer kind `8`. (If a faithful stub is too heavy, assert the request envelopes via a mock transport that records calls.)
- [ ] **Step 2:** Implement `client.ts` (SDK connect with `Mcp-Session-Id` handling, no auth header — dev mode) and `ops.ts` typed wrappers.
- [ ] **Step 3:** Tests green; `npm run typecheck` clean. **Commit** (`feat(mcp): typed archistrator drive-surface client`).

---

### Task 3: Ruled-policy decision module (the entire "operator", deterministic)

**Files:**
- Create: `archistrator-bench/src/operator/policies.ts`
- Test: `archistrator-bench/test/policies.test.ts`

**Interfaces:**
- Produces: pure functions, no I/O, so they are trivially testable and carry zero skew:
  - `chooseSdpOption(sdpDraft): { optionId: string, basis: 'recommendation' }` — returns the `.Recommendation`'s optionId; throws `NoRecommendation` if absent (a real archistrator defect, must surface, not guess).
  - `decideEscalation(activityState, priorOverrides): { kind: 'retry' | 'skip', notes: string }` — `retry` if this activity has not yet been retried by policy, else `skip`; notes name the rule.
  - `decideApproval(activityState): { decision: 'approve', feedback: {} }` — always approve (bench autonomy).
  - Each returns a serializable `OperatorAction` record `{ point, input (state digest), output, ruleId, at }` for `run.json.operatorActions[]`.

- [ ] **Step 1: Failing tests:** `chooseSdpOption` returns the recommended option from a fixture SDP draft; throws on a draft with no recommendation. `decideEscalation` returns retry on first encounter of an activityId, skip on the second (given priorOverrides). `decideApproval` always approves. Each emits an OperatorAction with a stable ruleId.
- [ ] **Step 2:** Implement. **Commit** (`feat(operator): deterministic ruled-policy decisions`).

---

### Task 4: Scratch stack provisioner

**Files:**
- Create: `archistrator-bench/src/stack/provision.ts`, `archistrator-bench/src/stack/ports.ts`
- Test: `archistrator-bench/test/provision.test.ts` (unit-level: repo prep + env assembly; the full boot is exercised in Task 8's smoke)

**Interfaces:**
- Consumes: a path to a built archistrator (`archistrator serve` binary or the archistrator repo to `go build`); Postgres/Temporal are NOT needed on the `local` profile (recon §4 — local requires nothing beyond the file state repo).
- Produces: `provisionStack({ archistratorBin, benchmark, runId, scratchRoot }): Promise<StackHandle>` where `StackHandle = { mcpUrl, statRepoPath, serveProc, port, teardown() }`. Steps it performs:
  1. `mkdir <scratchRoot>/<runId>/state-repo`; `git init`; `git config receive.denyCurrentBranch updateInstead`; write `.gitignore` containing `.aiarch/traces/`; initial commit on `main` (non-bare, has a working tree).
  2. Run `archistrator init` in that dir (creates empty `.aiarch/state/`, `.mcp.json`, applies the git config idempotently).
  3. Pick a free port (`ports.ts`, OS-assigned then released).
  4. Spawn the server child directly (bypass the `serve` wrapper's Temporal auto-spawn only if a Temporal is already provided; otherwise use `archistrator serve --port <p> --skip-auth-check` and let it manage Temporal) with env: `ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true`, `ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL=file://<state-repo>`, `ARCHISTRATOR_CONSTRUCTION_DRYRUN=<dryRun>`, `ARCHISTRATOR_AUTH_DEV_MODE=true`, and NO `ARCHISTRATOR_CONSTRUCTION_REPO_OWNER/_NAME`.
  5. Poll `GET http://127.0.0.1:<p>/healthz` until ready or timeout.
  6. Return the handle; `teardown()` SIGTERMs the process group and leaves the state repo for harvest.

- [ ] **Step 1: Failing test** on the pure parts: repo-prep writes the `.aiarch/traces/` gitignore and sets the git config (assert via `git check-ignore` and `git config --get`); env assembly includes all six real-construction requirements and omits GH creds; a network state URL is rejected before spawn.
- [ ] **Step 2:** Implement; the spawn/health-poll path is integration-covered in Task 8 (guarded behind an env flag so unit `npm test` doesn't require a real archistrator).
- [ ] **Step 3:** typecheck + unit tests green. **Commit** (`feat(stack): scratch local-profile provisioner`).

---

### Task 5: The deterministic drive sequence

**Files:**
- Create: `archistrator-bench/src/runner/drive.ts`
- Test: `archistrator-bench/test/drive.test.ts` (against a scripted fake `ArchistratorMcp` returning staged states)

**Interfaces:**
- Consumes: `ArchistratorMcp` (Task 2), the ruled policies (Task 3).
- Produces: `driveToBuiltApp(mcp, { pid, owner, researchSources, policies, log }): Promise<DriveResult>` implementing the recon call script exactly:
  1. createProject → setReviewPolicy("vibes") → setResearchInput.
  2. startSystemDesign; poll `getProject(pid)` until `Phase` advances to project-design (bounded, with a max-wait + a `gap` record on timeout).
  3. Phase-2 loop over kinds `[8,9,10,11,14,12,13,15]`: requestPhase2Draft(kind) → poll getPhase2Session(kind) until `Stage==4`; respect the `Drafting/Redrafting` concurrency guard (poll before the next request).
  4. requestSdpCommit → getPhase2Session(16) → `chooseSdpOption(.Draft)` → submitSdpDecision(optionId) → advanceToConstruction(acknowledgeStale=true).
  5. executeNextActivity(pid, uuid); then poll `getProject(pid).ActivityConstruction` until every activity is terminal (completed/failed). On `StageAwaitingApproval` → `decideApproval` → submitPhaseDecision. On `StageAwaitingTakeover` → `decideEscalation` → overrideActivity, **inside the 30m escalation window** (poll cadence must react well under 30m; use ~10s). Record every policy firing.
  6. Return `{ outcome, operatorActions, gaps, activitySummary }`.

- [ ] **Step 1: Failing tests** with a scripted fake MCP: happy path drives all steps and returns succeeded; a Phase-2 kind that never commits → drive times out with a gap record (not a hang); an `awaitingApproval` state triggers exactly one submitPhaseDecision; an `awaitingTakeover` triggers retry-then-skip across two encounters; a missing SDP recommendation surfaces `NoRecommendation` as a run failure (not a crash).
- [ ] **Step 2:** Implement with explicit bounded polls (every wait has a max + gap-on-timeout — never an unbounded loop). **Commit** (`feat(runner): deterministic end-to-end drive sequence`).

---

### Task 6: Harvester — snapshot the immutable archive

**Files:**
- Create: `archistrator-bench/src/runner/harvest.ts`
- Test: `archistrator-bench/test/harvest.test.ts`

**Interfaces:**
- Consumes: a sealed `StackHandle` (state repo path) + `DriveResult` + the archistrator commit SHA.
- Produces: `harvest({ stateRepoPath, runId, benchmark, driveResult, archistratorCommit, epoch, suiteVersion, outDir }): Promise<string>` writing the immutable `runs/<benchmark>/<runId>/`:
  - `app/` — the built-app snapshot: `git archive main` from the state repo (working tree at head of main, EXCLUDING `.aiarch/`), extracted here. (Never a live `.git` — a static snapshot.)
  - `project.json` — copied from `<stateRepo>/.aiarch/state/project.json`.
  - `traces/` — copied from `<stateRepo>/.aiarch/traces/` (the gitignored sidecar: `episodes.jsonl` + per-episode jsonl).
  - `run.json` — the RunRecord (validates against Task 1's schema): commit SHA, `archistratorDirty` (from `git -C archistrator status --porcelain`), operatorPolicyVersion, epoch, suiteVersion, timestamps, outcome, operatorActions, gaps, stackConfig.
  - `run.log` — the full runner log.
  - After writing, the directory is treated as sealed (Task 9's CI enforces it; the harvester itself just writes once and never rewrites).

- [ ] **Step 1: Failing test:** given a fixture state-repo (a tiny git repo with a committed `app/` tree, a `.aiarch/state/project.json`, and a `.aiarch/traces/episodes.jsonl`), harvest produces the four artifacts; `app/` contains the app files and NOT `.aiarch/`; `run.json` validates against the schema; a second harvest of the same runId refuses (no overwrite).
- [ ] **Step 2:** Implement. **Commit** (`feat(runner): immutable archive harvester`).

---

### Task 7: todomvc benchmark definition (pinned inputs + frozen acceptance suite)

**Files:**
- Create: `archistrator-bench/benchmarks/todomvc/research/00-founder-brief.txt`, `.../research/01-todomvc-spec.txt` (pinned research corpus — the constant input)
- Create: `archistrator-bench/benchmarks/todomvc/benchmark.json` (metadata: name, difficulty, researchSources manifest, suiteVersion, expected feature checklist)
- Create: `archistrator-bench/benchmarks/todomvc/acceptance/` — the frozen Playwright suite that tests a BUILT todo app (not archistrator), plus `acceptance/harness.ts` to boot a built app snapshot and run the suite against it
- Test: `archistrator-bench/test/todomvc-benchmark.test.ts` (validates the benchmark.json shape + that research files exist and are non-empty)

**Interfaces:**
- Produces: `loadBenchmark('todomvc'): Benchmark` = `{ name, difficulty, researchSources: {title, content}[], suiteVersion, featureChecklist, runAcceptance(appSnapshotPath): Promise<AcceptanceResult> }`. `AcceptanceResult = { buildOk, testsPass, testsFailed, acceptancePassRate, featureChecklist: {id, pass}[], soft: {rubricScores?} }` — the soft/LLM-rubric block is left null in v1 (SP3 owns scoring depth; here we produce the hard deterministic signal only).
- The todomvc corpus pins the TodoMVC feature set: add/edit/delete/toggle/clear-completed/filter/counter/persistence, backend (web + MCP) and frontend (web + mcpapps) per the founder's original AC. The acceptance suite is a **frozen** external suite (spec ruling) — versioned `v1`, never edited within an experiment series.

- [ ] **Step 1:** Author the pinned research corpus (founder brief + a precise TodoMVC behavior spec, in the `.txt` shape `SetResearchInput` consumes). Keep it fixed and minimal-but-complete — this is THE constant input; it must not drift.
- [ ] **Step 2:** Author `benchmark.json` (suiteVersion "v1", feature checklist ids).
- [ ] **Step 3:** Author the acceptance Playwright suite + `harness.ts`: boot the built-app snapshot (detect its run command from the snapshot — a built archistrator app has a known shape; if the shape is unknown at plan time, `harness.ts` documents the assumption and the suite asserts build+serve first). Assertions cover the feature checklist. This suite is the primary hard-science quality metric.
- [ ] **Step 4:** Test validates the benchmark loads + corpus non-empty + suite files present. **Commit** (`feat(benchmarks): todomvc pinned inputs + frozen v1 acceptance suite`).

---

### Task 8: Runner CLI — wire provision → drive → harvest → accept

**Files:**
- Create: `archistrator-bench/src/runner/cli.ts` (the `bench run <benchmark>` entry), `archistrator-bench/src/runner/index.ts`
- Test: `archistrator-bench/test/cli.test.ts` (arg parsing + orchestration with fakes); plus a **gated integration smoke** `test/integration/full-run.test.ts` (runs only when `BENCH_ARCHISTRATOR_BIN` is set)

**Interfaces:**
- Produces: `bench run todomvc --archistrator <bin-or-repo> [--dry-run] [--out runs/]` orchestrating: resolve archistrator commit SHA (`git -C <repo> rev-parse HEAD`) → provisionStack → connect MCP → driveToBuiltApp(pinned corpus) → seal stack → harvest → loadBenchmark.runAcceptance(app snapshot) → write acceptance/ + fold pass/fail into run.json outcome → teardown. `--dry-run` sets `CONSTRUCTION_DRYRUN=true` for a fast plumbing smoke (design rail also stubs — documents that a dry-run produces no real app, only validates the drive/harvest wiring).

- [ ] **Step 1: Failing test** (`cli.test.ts`): arg parsing; orchestration order with all deps faked (provision→drive→harvest→accept called in sequence; a drive failure still harvests a `failed` run — failures are data).
- [ ] **Step 2:** Implement the orchestration.
- [ ] **Step 3: Gated integration smoke** (`full-run.test.ts`, skipped unless `BENCH_ARCHISTRATOR_BIN` present): a **`--dry-run` full run** against a real archistrator build — proves provision + MCP connect + the full drive sequence + harvest wiring end-to-end without waiting on real construction. Assert a sealed `runs/todomvc/<runId>/` with a schema-valid run.json (outcome may be a dry-run gap for the app itself; the point is the plumbing).
- [ ] **Step 4:** Run `bench run todomvc --dry-run` against the local archistrator build; capture the result in the report. **Commit** (`feat(runner): bench-run CLI orchestration`).

---

### Task 9: Archive immutability CI gate + gtd/archistrator benchmark stubs

**Files:**
- Create: `archistrator-bench/.github/workflows/ci.yml` (typecheck + lint + vitest + the immutability check)
- Create: `archistrator-bench/scripts/check-runs-immutable.mjs`
- Create: `archistrator-bench/benchmarks/gtd/benchmark.json` + `research/` (DEFINED, unrun), `archistrator-bench/benchmarks/archistrator/benchmark.json` + `research/` (DEFINED, unrun)

**Interfaces:**
- Produces: a CI gate that fails any PR whose diff **modifies or deletes** an existing path under `runs/**` (additions of new run dirs are allowed; touching a sealed one is not). Plus the two additional benchmark definitions per spec §6, shipped but not executed (their acceptance suites may be stubs marked `suiteVersion: "draft"`).

- [ ] **Step 1:** `check-runs-immutable.mjs`: `git diff --name-status <base>..<head>` — fail on any `M`/`D`/`R` under `runs/`; pass on `A`. Test it locally against a synthetic diff.
- [ ] **Step 2:** `ci.yml`: node setup → `npm ci` → typecheck → lint → vitest → run the immutability script against the PR base.
- [ ] **Step 3:** gtd benchmark: pinned corpus derived from `archistrator/../software/products/gtd` docs (capture/clarify/organize/reflect/tickler/horizons per gtdinfo.txt) — research files + benchmark.json, acceptance suite stubbed (`draft`). archistrator benchmark: corpus = archistrator's own mission/requirements, stubbed suite. Both marked `unrun: true` in benchmark.json.
- [ ] **Step 4:** **Commit** (`ci: runs immutability gate + gtd/archistrator benchmark definitions`).

---

### Task 10: Full closeout + real todomvc plumbing validation

- [ ] **Step 1:** Full bench-repo gates: `npm run typecheck`, `npm run lint`, `npm test`, the immutability script against `main`.
- [ ] **Step 2:** Run `bench run todomvc --dry-run --archistrator <local archistrator build>` once more end-to-end; confirm a sealed, schema-valid archive directory. (The REAL, non-dry todomvc x3 iterations are SP5/AC, not this plan — SP2 delivers the machine that can do it, validated in dry-run.)
- [ ] **Step 3:** Update the spec §6 status: SP2 implemented; record the deviation ratified by the founder (operator = deterministic driver + ruled policies, no LLM operator / no Playwright — the recon proved the fallback list empty and the drive near-deterministic; skew-minimization was the deciding factor) as a dated amendment.
- [ ] **Step 4: Commit** (`chore: SP2 bench repo + runner complete`).

---

## Explicitly deferred (per spec / founder ruling)

- The REAL todomvc x3 improvement iterations — that is SP5/AC, driven after SP3 (analysis) and SP4 (dashboard) exist.
- SP3 metrics body (metrics.json is an envelope here; the extractor fills it).
- SP4 dashboard.
- A real LLM operator agent — reserved for a future benchmark that needs interactive judgment; todomvc/gtd/archistrator drive deterministically.
- gtd + archistrator benchmark RUNS (defined-but-unrun).
- Learning opt-in infra beyond the `learningConsent` field.
