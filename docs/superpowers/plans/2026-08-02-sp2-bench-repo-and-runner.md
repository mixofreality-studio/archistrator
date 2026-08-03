# SP2 — Bench Repo + Deterministic Runner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the `archistrator-bench` sibling repo — a deterministic MCP-driven runner that builds a fixed-input reference app end-to-end with zero human interaction, an immutable per-run archive keyed to the archistrator commit that built it, and the todomvc benchmark (pinned inputs + frozen acceptance suite). gtd and archistrator benchmarks ship defined-but-unrun.

**Architecture:** Two layers per spec §6, refined by the 2026-08-02 recon + founder ruling. (1) A **deterministic runner** (TypeScript, Node) that provisions a scratch archistrator project, boots the local stack, drives the full rail through archistrator's `/mcp` streamable-HTTP surface via a fixed op sequence with **ruled policies** at the three caller-driven points (no LLM operator, no browser — the Playwright-fallback list is empty at 43↔43 MCP/REST parity), polls to construction completion, and harvests. (2) An **immutable archive** `runs/<benchmark>/<runId>/` holding the built-app snapshot, project.json, traces, acceptance results, metrics.json, and run.json (archistrator commit SHA + config epoch). A bench-repo CI gate forbids any mutation under `runs/**`.

**Tech Stack:** TypeScript/Node (runner + acceptance harness driver), the MCP TypeScript SDK (`@modelcontextprotocol/sdk`) streamable-HTTP client, Vitest (runner unit tests), Playwright (todomvc acceptance suite only — testing the *built app*, not archistrator), a checked-in archistrator build (or a path to a local checkout) as the system-under-test.

## Global Constraints

- **This is a NEW sibling repo** `archistrator-bench` (founder ruling), NOT inside the archistrator repo. Create it at `/Users/davidmarne/mixofrealitystudio/archistrator-bench` (sibling of `archistrator`). All paths below are relative to that repo root unless prefixed `archistrator/`.
- **Configuration identity = the archistrator commit SHA** that built the run. Recorded in `run.json`; every metric/comparison keys on it.
- **Operator skew = zero.** The runner is deterministic. The caller-driven decision points use fixed ruled policies (Task 3), never an LLM: SDP option = the assembled review's `.Recommendation`; held design gate (`StageAwaitingReview` unchanged under vibes) = waive open change-requests then approve; failed design draft (`StageDraftFailed`) = re-request once then gap; construction escalation (`StageAwaitingTakeover`) = override `retry` once then `skip`; risk-floor approval (`StageAwaitingApproval`) = approve. Every policy application is logged to `run.json.operatorActions[]` with the input state digest, so a policy firing is auditable and never silent.
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
systemDesignGetSessionState(pid, kind)               # per-Phase-1-kind session (reachable while the parent runs)
systemDesignSetReviewCommentStatus(pid, kind, commentID, status)   # waive open change-requests (int status)
systemDesignSubmitReviewDecision(pid, kind, decision, feedback)    # approve a held gate (int decision)
projectDesignRequestArtifactDraft(pid, kind)         # per Phase-2 kind
projectDesignGetSessionState(pid, kind)              # poll Stage==Committed(5)
projectDesignSetReviewCommentStatus / projectDesignSubmitReviewDecision   # Phase-2 twins
projectDesignRequestSDPCommit(pid)                   # assemble 4 options + risk model
projectDesignGetSessionState(pid, 16)                # read .Draft for optionIDs + .Recommendation
projectDesignSubmitSDPDecision(pid, SDPCommit, optionID)   # UNAVOIDABLE ratification (async commit)
projectDesignAdvanceToConstruction(pid, acknowledgeStale=true)    # assert result.Advanced==true
constructionExecuteNextActivity(pid, tickID)         # pump self-cascades; re-tickable with fresh UUID
constructionGetSessionState(pid, activityID|"")      # per-activity / whole-network stage
constructionSubmitPhaseDecision(pid, activityID, phase, approve=1, feedback)  # risk-floor gate (int)
constructionOverrideActivity(pid, activityID, {kind, notes, comments})        # escalation (kind int)
```

**Wire enums are ALL integers** (same as ArtifactKind): Phase-2 `SessionStage {1 drafting, 2 assemblingSDP, 3 awaitingReview, 4 redrafting, 5 committed, 6 withdrawn, 7 refused, 8 draftFailed}` — **committed is 5** (4 is redrafting; polling for 4 exits on a mid-redraft session and the next kind's predecessor gate then fails). `ReviewDecision {1 approve, 2 reject, 3 withdraw}`. Construction `PhaseDecision {1 approve, 2 sendBack}`. `OverrideKind {1 takeover, 2 retry, 3 skip, 4 reassign}`. `ConstructionStage {1 dispatching, 2 pipelineRunning, 3 reviewing, 4 awaitingTakeover, 5 paused, 6 exited, 7 awaitingApproval}`. Built app = `git log main` in the state repo (activity branches `--no-ff` merged under vibes).

**Do NOT call AskQuestions** — but that alone does NOT keep the ledger clean: **critique-revise feedback is seeded into the durable review ledger as open change-request comments** on the five critiqued Phase-1 kinds (mission/glossary/scrubbedRequirements/coreUseCases via PM critique, system via architect self-critique) before the redraft dispatch, and they only auto-clear if the redrafting agent commits a non-empty response per comment. An open comment holds the vibes autogate and hard-blocks approve. This is probabilistic (LLM behavior) and is the drive's main stall risk — cleared deterministically by ruled policy #4 (Task 3), no archistrator change needed.

---

### Task 1: Bench repo scaffold + immutability contract

**Files:**
- Create: `archistrator-bench/package.json`, `tsconfig.json`, `.gitignore`, `.nvmrc`, `README.md`, `vitest.config.ts`
- Create: `archistrator-bench/schema/run.schema.json`, `archistrator-bench/schema/metrics.schema.json` (typed archive contracts)
- Create: `archistrator-bench/runs/.gitkeep`

**Interfaces:**
- Produces: the repo skeleton and the two JSON Schemas that every archived `run.json` / `metrics.json` validates against (later tasks import these). `RunRecord` fields: `runId, benchmark, archistratorCommit, archistratorDirty ("true"|"false"|"unknown" — "unknown" when --archistrator is a bare binary with no repo to inspect), operatorPolicyVersion, epoch, suiteVersion, startedAt, endedAt, outcome (succeeded|failed), learningConsent, operatorActions[], gaps[], stackConfig, modelIds`. `modelIds` = a **worker-model + toolchain reproducibility snapshot** (recon: an Anthropic-side model change silently shifts results at a fixed archistrator SHA): `{ claudeCliVersion (from `claude --version`), workerModelRoster (any ARCHISTRATOR_* model env the server ran with, else "ambient-subscription"), temporalCliVersion }`. `Metrics` is SP3's concern — here define only the envelope (`runId, benchmark, schemaVersion`) so the archive validates; SP3 fills the body.

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
- Produces: `class ArchistratorMcp` wrapping the SDK client — `connect(mcpUrl)`, `call(tool, args): Promise<result>`, `close()`. Plus `ops.ts`: thin typed wrappers for exactly the tools in the drive surface above (e.g. `createProject(owner, name)`, `setResearchInput(pid, sources)`, `startSystemDesign(pid)`, `getProject(pid)`, `getP1Session(pid, kind)`, `setReviewCommentStatus(pid, kind, commentId, status)`, `submitReviewDecision(pid, kind, decision, feedback)`, `requestPhase2Draft(pid, kind)`, `getPhase2Session(pid, kind)`, `requestSdpCommit(pid)`, `submitSdpDecision(pid, optionId)`, `advanceToConstruction(pid)`, `executeNextActivity(pid, tickId)`, `getConstructionSession(pid, activityId)`, `submitPhaseDecision(...)`, `overrideActivity(...)`). Each returns the parsed tool result. **All enum args are integers on the wire** — ArtifactKind, ReviewDecision (approve=1), PhaseDecision (approve=1), OverrideKind (retry=2, skip=3), review-comment status (waived/addressed per the projectstate enum — read it from the OAS). ops.ts owns the string-name→integer mapping so callers pass symbolic constants.

- [ ] **Step 1: Failing test** `mcp-ops.test.ts`: against a stub MCP server (a tiny in-test `http.Server` speaking the streamable-HTTP tool-call protocol, or the SDK's in-memory transport if available), assert `createProject` sends tool name `systemDesignCreateProject` with `{owner, name}` and returns the parsed pid; assert `requestPhase2Draft(pid, 8)` sends integer kind `8`. (If a faithful stub is too heavy, assert the request envelopes via a mock transport that records calls.)
- [ ] **Step 2:** Implement `client.ts` (SDK connect with `Mcp-Session-Id` handling, no auth header — dev mode) and `ops.ts` typed wrappers.
- [ ] **Step 3:** Tests green; `npm run typecheck` clean. **Commit** (`feat(mcp): typed archistrator drive-surface client`).

---

### Task 3: Ruled-policy decision module (the entire "operator", deterministic)

**Files:**
- Create: `archistrator-bench/src/operator/policies.ts`
- Test: `archistrator-bench/test/policies.test.ts`

**Interfaces:**
- Produces: pure functions, no I/O, so they are trivially testable and carry zero skew. All enum outputs are the integer wire values (Task 2's constants):
  - `chooseSdpOption(sdpDraft): { optionId, basis: 'recommendation' }` — returns the `.Recommendation`'s optionId; throws `NoRecommendation` if absent (recon confirms an out-of-band fallback always populates it, so absence is a real archistrator defect — surface, don't guess).
  - **`decideDesignGateUnblock(sessionView): { waives: commentId[], thenApprove: true }`** (ruled policy #4 — the BLOCKER fix). Given a design session (either manager) stuck at `StageAwaitingReview(3)` under vibes: return the ids of every OPEN change-request comment in `sessionView.ReviewThread` to waive (status→waived), then approve. Approve is hard-blocked while any comment is open, so the caller waives first, then submits ReviewDecision approve. Empty open-set + still-awaiting is itself a signal (a contained approve/merge fault fell through to the human selector) → approve directly.
  - **`decideDraftFailedRetry(kind, priorRetries): { action: 'redraft' | 'gap' }`** (ruled policy #5) — on `StageDraftFailed(8)`, `redraft` (re-`RequestArtifactDraft`, the documented revival path) if this kind hasn't been policy-retried, else `gap`.
  - `decideEscalation(activityState, priorOverrides): { kind: 2|3, notes }` — override `retry`(2) on first policy encounter of an activityId, else `skip`(3).
  - `decideApproval(activityState): { decision: 1, feedback: {} }` — always approve (bench autonomy).
  - Each returns a serializable `OperatorAction` `{ point, input (state digest), output, ruleId, at }` for `run.json.operatorActions[]`.

- [ ] **Step 1: Failing tests:** `chooseSdpOption` returns the recommended option; throws on no-recommendation. `decideDesignGateUnblock` returns all open change-request ids from a fixture ReviewThread then approve; returns approve-directly on an empty open-set. `decideDraftFailedRetry` redrafts first, gaps second. `decideEscalation` retry(2) then skip(3). `decideApproval` approves(1). Each emits an OperatorAction with a stable ruleId.
- [ ] **Step 2:** Implement. **Commit** (`feat(operator): deterministic ruled-policy decisions`).

---

### Task 4: Scratch stack provisioner

**Files:**
- Create: `archistrator-bench/src/stack/provision.ts`, `archistrator-bench/src/stack/ports.ts`
- Test: `archistrator-bench/test/provision.test.ts` (unit-level: repo prep + env assembly; the full boot is exercised in Task 8's smoke)

**Interfaces:**
- Consumes: a path to a built archistrator (`archistrator serve` binary or the archistrator repo to `go build`). **Prerequisites on PATH: `git`, `claude` (authenticated), the `temporal` CLI, `aiarch-state-mcp`** (recon §4 corrected: `archistrator serve` spawns `temporal server start-dev` and hard-errors without the CLI; Postgres is genuinely not needed on the local profile).
- Produces: `provisionStack({ archistratorBin, benchmark, runId, scratchRoot, dryRun }): Promise<StackHandle>` where `StackHandle = { mcpUrl, stateRepoPath, serveProc, port, projectName, teardown() }`. Steps:
  1. `mkdir <scratchRoot>/<runId>/state-repo`; `git init`; write `.gitignore` containing `.aiarch/traces/`; initial commit on `main` (non-bare, has a working tree).
  2. Run `archistrator init` in that dir (creates empty `.aiarch/state/`, `.mcp.json`, sets `receive.denyCurrentBranch updateInstead` idempotently).
  3. Pick a free port (`ports.ts`, OS-assigned then released).
  4. Spawn **`archistrator serve --port <p>`** with **cwd = the state repo** (recon §4b: `serve` uses its CWD as the state repo and FORCES `GIT_LOCAL=true` + `file://<cwd>` onto the child, overriding any runner-set repo env — so cwd is the control surface, not env). Pass through env: `ARCHISTRATOR_CONSTRUCTION_DRYRUN=<dryRun>` and `ARCHISTRATOR_AUTH_DEV_MODE=true`; ensure NO `ARCHISTRATOR_CONSTRUCTION_REPO_OWNER/_NAME` (else the GH pipeline wins over local). Add `--skip-auth-check` **only when dryRun** (a real run wants the boot-time `claude -p` auth probe to fail fast). `serve` manages its own Temporal (machine-wide dev server).
  5. `projectName = <benchmark>-<runId>` (recon §4c: the dev Temporal is machine-wide + persistent with projectID-keyed workflow ids under USE_EXISTING policies — a constant name could adopt a prior run's open workflow against the wrong state repo; a per-run name gives each run a fresh workflow-id space).
  6. Poll `GET http://127.0.0.1:<p>/healthz` until ready or timeout.
  7. Return the handle; `teardown()` SIGTERMs the process group and leaves the state repo for harvest.

- [ ] **Step 1: Failing test** on the pure parts: repo-prep writes the `.aiarch/traces/` gitignore (assert via `git check-ignore`); the spawn spec sets cwd=state-repo, adds `--skip-auth-check` iff dryRun, omits GH creds, sets DRYRUN + AUTH_DEV_MODE; projectName == `<benchmark>-<runId>`; a missing `temporal`/`claude`/`aiarch-state-mcp` on PATH is a pre-spawn error naming the missing tool.
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
  2. startSystemDesign; poll `getProject(pid)` until `Phase` advances to project-design (bounded, gap on timeout). **During this poll, also watch each Phase-1 kind's session via `getP1Session(pid, kind)`**: on `StageAwaitingReview(3)` held > 60s under vibes → `decideDesignGateUnblock` → waive open change-request ids then submitReviewDecision(approve); on `StageDraftFailed(8)` → `decideDraftFailedRetry` → re-request once then gap. These are the ruled-policy #4/#5 stall clears — the critique-revise held-gate is the main real-world stall.
  3. Phase-2 loop over kinds `[8,9,10,11,14,12,13,15]`: requestPhase2Draft(kind) → poll getPhase2Session(kind) until **`Stage==5` (committed; 4 is redrafting — do not exit on 4)**; apply the same held-gate/draft-failed policies against the Phase-2 session; respect the in-flight concurrency guard (poll before the next request).
  4. requestSdpCommit → getPhase2Session(16) → `chooseSdpOption(.Draft)` → submitSdpDecision(optionId) → **poll getPhase2Session(16) until Stage==5** (the SDP commit is async: signal → re-run engines → re-stage → commit) → advanceToConstruction(acknowledgeStale=true) → **assert `result.Advanced==true`** (a gated advance returns `{Advanced:false, MissingArtifacts}` as a normal RESULT, not an error; on false, re-poll and retry once, then gap).
  5. executeNextActivity(pid, uuid); poll `getProject(pid).ActivityConstruction` until every activity is terminal. On `StageAwaitingApproval(7)` → `decideApproval` → submitPhaseDecision(approve=1). On `StageAwaitingTakeover(4)` → `decideEscalation` → overrideActivity(retry=2/skip=3), **inside the 30m escalation window** (~10s poll cadence). **Quiescence guard:** if no activity is in-flight AND non-terminal activities remain, re-tick `executeNextActivity` with a fresh UUID (each tick is its own pump workflow keyed by tickID — safe and idempotent, and recovers a cascade that died on a terminally-failed activity stranding dependents); if a re-tick reports nothing dispatched and work still remains, fail the run immediately rather than waiting out the poll budget. Record every policy firing.
  6. Return `{ outcome, operatorActions, gaps, activitySummary }`.

- [ ] **Step 1: Failing tests** with a scripted fake MCP: happy path drives all steps and returns succeeded; a Phase-2 kind stuck at Stage 4 (redrafting) is NOT treated as committed; a kind that never reaches 5 → gap (not a hang); a held `AwaitingReview(3)` session with open change-request comments → waive-all-then-approve fires; a `StageDraftFailed(8)` → one redraft then gap; `advanceToConstruction` returning `Advanced:false` → retry-once-then-gap (not silent success); `awaitingApproval` → one submitPhaseDecision(1); `awaitingTakeover` → retry(2)-then-skip(3) across two encounters; a stranded cascade (in-flight empty, work remains) → re-tick then fail; missing SDP recommendation → `NoRecommendation` run failure.
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
  - `run.json` — the RunRecord (validates against Task 1's schema): commit SHA, `archistratorDirty` (from `git -C <repo> status --porcelain`; `"unknown"` when `--archistrator` is a bare binary with no repo), `modelIds` (claude CLI version, worker model roster, temporal CLI version), operatorPolicyVersion, epoch, suiteVersion, timestamps, outcome, operatorActions, gaps, stackConfig.
  - `app/` note: `git archive main` cannot exclude `.aiarch/` by naive pathspec — implement as **archive-then-strip** (extract, then `rm -rf app/.aiarch`) or export-ignore attributes.
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
- Produces: `bench run todomvc --archistrator <bin-or-repo> [--dry-run] [--out runs/]` orchestrating: resolve archistrator commit SHA (`git -C <repo> rev-parse HEAD`) + modelIds → provisionStack → connect MCP → driveToBuiltApp(pinned corpus) → seal stack → harvest → loadBenchmark.runAcceptance(app snapshot) → write acceptance/ + fold pass/fail into run.json outcome → teardown. `--dry-run` sets `CONSTRUCTION_DRYRUN=true`.

  **Honest dry-run scope (recon §4e):** the DRYRUN stub fake-succeeds *without committing any draft*, and it backs the DESIGN dispatches too — so a dry-run wedges at Phase-1 step 1 (mission read-back finds nothing). A `--dry-run` run therefore validates ONLY: provision + MCP connect + createProject/setResearchInput/startSystemDesign + the bounded-poll/gap machinery + failed-run harvest. It does NOT exercise Phase-2/SDP/construction against a real archistrator. That is expected: real end-to-end hardening lands in **AC iteration 0**, which spec §9 explicitly designates as the harness-hardening run. Everything past Phase-1 step 1 is covered here only against the scripted-fake MCP (Task 5).

- [ ] **Step 1: Failing test** (`cli.test.ts`): arg parsing; orchestration order with all deps faked (provision→drive→harvest→accept in sequence; a drive failure still harvests a `failed` run — failures are data); modelIds captured into run.json.
- [ ] **Step 2:** Implement the orchestration.
- [ ] **Step 3: Gated integration smoke** (`full-run.test.ts`, skipped unless `BENCH_ARCHISTRATOR_BIN` present): a `--dry-run` run against a real archistrator build — proves provision + MCP connect + the Phase-1-entry drive + gap handling + failed-run harvest (per the honest scope above), landing a sealed `runs/todomvc/<runId>/` with a schema-valid run.json. Optionally, a `BENCH_REAL_SMOKE=1` variant that lets a single REAL Phase-1 draft (mission) actually commit — the cheapest real-dispatch probe — if a `claude`-authed box is available.
- [ ] **Step 4:** Run `bench run todomvc --dry-run` against the local archistrator build; capture the result in the report. **Commit** (`feat(runner): bench-run CLI orchestration`).

---

### Task 9: Archive immutability CI gate + gtd/archistrator benchmark stubs

**Files:**
- Create: `archistrator-bench/.github/workflows/ci.yml` (typecheck + lint + vitest + the immutability check)
- Create: `archistrator-bench/scripts/check-runs-immutable.mjs`
- Create: `archistrator-bench/benchmarks/gtd/benchmark.json` + `research/` (DEFINED, unrun), `archistrator-bench/benchmarks/archistrator/benchmark.json` + `research/` (DEFINED, unrun)

**Interfaces:**
- Produces: a CI gate that fails any PR whose diff **modifies or deletes** an existing path under `runs/**` (additions of new run dirs are allowed; touching a sealed one is not). Plus the two additional benchmark definitions per spec §6, shipped but not executed (their acceptance suites may be stubs marked `suiteVersion: "draft"`).

- [ ] **Step 1:** `check-runs-immutable.mjs`: `git diff --name-status <base>..<head>` — fail on any `M`/`D`/`R`/`C` status under `runs/`; pass on `A` (additions of new run dirs). Note in the README that the gate guards PRs only — a direct push to `main` bypasses it (accepted residual). Test it locally against a synthetic diff.
- [ ] **Step 2:** `ci.yml`: node setup → `npm ci` → typecheck → lint → vitest → run the immutability script against the PR base.
- [ ] **Step 3:** gtd benchmark: pinned corpus derived from `archistrator/../software/products/gtd` docs (capture/clarify/organize/reflect/tickler/horizons per gtdinfo.txt) — research files + benchmark.json, acceptance suite stubbed (`draft`). archistrator benchmark: corpus = archistrator's own mission/requirements, stubbed suite. Both marked `unrun: true` in benchmark.json.
- [ ] **Step 4:** **Commit** (`ci: runs immutability gate + gtd/archistrator benchmark definitions`).

---

### Task 10: Full closeout + real todomvc plumbing validation

- [ ] **Step 1:** Full bench-repo gates: `npm run typecheck`, `npm run lint`, `npm test`, the immutability script against `main`.
- [ ] **Step 2:** Run `bench run todomvc --dry-run --archistrator <local archistrator build>` once more end-to-end; confirm a sealed, schema-valid archive directory. (The REAL, non-dry todomvc x3 iterations are SP5/AC, not this plan — SP2 delivers the machine that can do it, validated in dry-run.)
- [ ] **Step 3:** Update the spec §6 status: SP2 implemented; record the founder-ratified deviation (operator = deterministic driver + ruled policies, no LLM operator / no Playwright — recon proved the fallback list empty and the drive near-deterministic; skew-minimization decided it) + the five ruled policies incl. #4's held-gate clear, as a dated amendment. **This edits the archistrator repo (the spec lives there), the one sanctioned exception to this plan's "archistrator is NOT modified" constraint — a docs-only commit, no code.** Commit the spec amendment in the ARCHISTRATOR repo (with the Co-Authored-By trailer); everything else in this plan is bench-repo-only.
- [ ] **Step 4: Commit** the bench repo (`chore: SP2 bench repo + runner complete`).

---

## Explicitly deferred (per spec / founder ruling)

- The REAL todomvc x3 improvement iterations — that is SP5/AC, driven after SP3 (analysis) and SP4 (dashboard) exist.
- SP3 metrics body (metrics.json is an envelope here; the extractor fills it).
- SP4 dashboard.
- A real LLM operator agent — reserved for a future benchmark that needs interactive judgment; todomvc/gtd/archistrator drive deterministically.
- gtd + archistrator benchmark RUNS (defined-but-unrun).
- Learning opt-in infra beyond the `learningConsent` field.
