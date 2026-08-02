# SP1 — Capture Seam + Trace UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture every local-venue agent episode (stream-json tee + supervisor-parsed `EpisodeSummary`), store it in a new `episodeAccess` sidecar ledger, and surface it in the archistrator SPA via a new thin `episodeManager` with JSON export (CSV client-side).

**Architecture:** Report/store split per spec §5 (`docs/superpowers/specs/2026-08-02-self-improvement-pipeline-design.md`): `agenticjob` observes (tee + parse) and reports the summary on the terminal `PipelineObservation`; a new `episodeAccess` RA owns the gitignored `.aiarch/traces/` sidecar (ledger + raw traces); the dispatching Managers persist via the generated activity rail; a new thin `episodeManager` serves reads to the SPA through the generated clientgen rail.

**Tech Stack:** Go 1.25 (`GOWORK=off` for all gen/test), modelgen/appgen/clientgen codegen rails, Temporal, React/TS (TanStack Router + MUI), Playwright (`uitests/`).

## Global Constraints

- All server codegen/test commands run from `server/` with `GOWORK=off` (Makefile targets already set it).
- **Never edit `*.gen.go` / `*.gen.ts`** — change contracts in `.aiarch/state/project.json` (repo root) and regenerate (`make gen-models`, `make gen-temporal`, `make gen-client`, `make gen-internal-tools`).
- Encapsulation gate: no new exported symbols in `internal/{engine,resourceaccess,manager}` beyond generated contracts; allowlist regen: `ENCAP_DUMP=1 GOWORK=off go test ./internal/ -run TestGeneratedOnlyPublic -v`.
- Layer file-layout standard (TestFileLayout): per package — Rule 1 one impl file, Rule 2 one file per Temporal workflow (name = lowercased workflow func minus `Workflow`), Rule 3 exactly one `*_test.go`, no hand-written Temporal activities.
- **Trust rule (spec §5):** no MCP verb, tool, or agent-reachable path may write or suppress episode data. Episode writes are supervisor/Manager-side only.
- Ledger location: gitignored `.aiarch/traces/` sidecar; **nothing episode-related in `project.json`** — not even tracePath pointers.
- SPA selectors: `data-testid` only, registered in `webApp/src/utilities/constants/UIIdentifiers`.
- webApp layer DAG (eslint-boundaries): routes → containers → components → hooks → api → contracts; components must not import hooks/api except via the allowed variants already in `webApp/eslint.platform.config.js`.
- UI changes stop for founder review (standing UI review loop) after the Playwright spec passes.
- Commit after every task; commit messages end with `Co-Authored-By:` trailer per repo convention.
- Full verification before final commit of each server task: `gofmt`, `GOWORK=off go vet ./...`, `GOWORK=off go build ./...`, `golangci-lint run ./...`, `GOWORK=off make test-short`, `make method-check`, all `gen-*-check` targets, `make encapsulation-check`.

---

### Task 1: Real stream-json fixtures

**Files:**
- Create: `server/internal/resourceaccess/agenticjob/testdata/streamjson/success_with_tools.jsonl`
- Create: `server/internal/resourceaccess/agenticjob/testdata/streamjson/success_with_subagent.jsonl`
- Create: `server/internal/resourceaccess/agenticjob/testdata/streamjson/failure.jsonl`

**Interfaces:**
- Produces: three real (never hand-authored — spec §10 fixture ruling) Claude CLI stream-json transcripts used by Task 6's parser tests.

- [ ] **Step 1: Capture a success-with-tools fixture.** From a scratch dir:

```bash
cd "$(mktemp -d)"
claude -p 'Create a file named hello.txt containing the word hi, then read it back.' \
  --output-format stream-json --verbose --max-turns 6 \
  > success_with_tools.jsonl
```

- [ ] **Step 2: Capture a subagent fixture** (exercises `parent_tool_use_id` sidechain events):

```bash
claude -p 'Use the Task tool to dispatch one subagent that answers: what is 2+2? Then report its answer.' \
  --output-format stream-json --verbose --max-turns 6 \
  > success_with_subagent.jsonl
```

- [ ] **Step 3: Capture a failure fixture** (terminal `result` with `is_error`/non-success subtype):

```bash
claude -p 'ignored' --output-format stream-json --verbose --max-turns 0 > failure.jsonl || true
# If --max-turns 0 does not yield an error-shaped terminal result, instead use an invalid
# --model name; keep whichever produces a terminal result event with a non-"success" subtype.
```

- [ ] **Step 4: Verify each fixture** has ≥1 line, every line parses as JSON, and the last line has `"type":"result"`:

```bash
for f in *.jsonl; do tail -1 "$f" | python3 -c 'import json,sys; e=json.load(sys.stdin); assert e["type"]=="result", e'; done
```

- [ ] **Step 5: Scrub + move.** Inspect fixtures for absolute home paths or tokens; replace with `/scrubbed` if present (content edits only — never restructure events). Move all three to `server/internal/resourceaccess/agenticjob/testdata/streamjson/`.

- [ ] **Step 6: Commit** (`test: real stream-json fixtures for episode parser`).

---

### Task 2: `episodeAccess` contract + system-model registration

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.episodeAccess` (new), `.slots["5"].model.components` + `relationships`, `.slots["6"]` archistrator-server container components
- Modify: `server/internal/arch_test.go` (encapsulation allowlist; `appArchSpec()` dependency map)
- Generated: `server/internal/resourceaccess/episode/contract.gen.go`, `server/internal/resourceaccess/episode/fake/fake.gen.go`

**Interfaces:**
- Produces (Go, generated from the contract below): `EpisodeAccess` interface with `AppendEpisode(ctx, projectID ProjectID, record EpisodeRecord) error`, `ListEpisodes(ctx, query EpisodeQuery) ([]EpisodeRecord, error)`, `ReadTraceEvents(ctx, projectID ProjectID, episodeID string) ([]json.RawMessage, error)`; types `EpisodeRecord`, `EpisodeUsage`, `EpisodeLineage`, `SubagentSpan`, `EpisodeOutcome` (enum: `succeeded|failed|cancelled|gap`), `EpisodeKind` (enum: `design|construction|review|rework|answer`).

- [ ] **Step 1: Author the contract entry.** Add `.serviceContracts.episodeAccess`, mirroring the `usageAccess` skeleton (RA layer, PascalCase props). Ops are exactly 3 (Method 3–5 rule):

```json
{
  "component": "episodeAccess",
  "layer": "ResourceAccess",
  "goPackage": "internal/resourceaccess/episode",
  "infra": ["LocalFS"],
  "title": "episode contract",
  "$defs": {
    "ProjectID": { "type": "string" },
    "EpisodeOutcome": { "type": "integer", "enum": [0, 1, 2, 3],
      "x-enum-varnames": ["EpisodeSucceeded", "EpisodeFailed", "EpisodeCancelled", "EpisodeGap"],
      "x-go-base": "int" },
    "EpisodeKind": { "type": "integer", "enum": [0, 1, 2, 3, 4],
      "x-enum-varnames": ["EpisodeKindDesign", "EpisodeKindConstruction", "EpisodeKindReview", "EpisodeKindRework", "EpisodeKindAnswer"],
      "x-go-base": "int" },
    "EpisodeUsage": { "type": "object", "properties": {
        "In": { "type": "integer" }, "Out": { "type": "integer" },
        "CacheRead": { "type": "integer" }, "CacheCreate": { "type": "integer" } },
      "required": ["In", "Out", "CacheRead", "CacheCreate"], "additionalProperties": false },
    "EpisodeLineage": { "type": "object", "properties": {
        "WorkflowID": { "type": "string" }, "RunID": { "type": "string" },
        "ActivityID": { "type": "string" } },
      "required": ["WorkflowID", "RunID"], "additionalProperties": false },
    "SubagentSpan": { "type": "object", "properties": {
        "ToolUseID": { "type": "string" },
        "StartedAt": { "type": "string", "format": "date-time", "x-go-import": "time", "x-go-type": "time.Time" },
        "EndedAt":   { "type": "string", "format": "date-time", "x-go-import": "time", "x-go-type": "time.Time" } },
      "required": ["ToolUseID"], "additionalProperties": false },
    "EpisodeRecord": { "type": "object", "properties": {
        "EpisodeID": { "type": "string" },
        "Kind": { "$ref": "#/$defs/EpisodeKind" },
        "TargetRef": { "type": "string" },
        "Lineage": { "$ref": "#/$defs/EpisodeLineage" },
        "WorkerClass": { "type": "string" },
        "Model": { "type": "string" },
        "Usage": { "$ref": "#/$defs/EpisodeUsage" },
        "StreamedUsage": { "$ref": "#/$defs/EpisodeUsage" },
        "CostUSD": { "type": "number" },
        "NumTurns": { "type": "integer" },
        "ToolCallCounts": { "type": "object", "additionalProperties": { "type": "integer" } },
        "SubagentSpans": { "type": "array", "items": { "$ref": "#/$defs/SubagentSpan" } },
        "StartedAt": { "type": "string", "format": "date-time", "x-go-import": "time", "x-go-type": "time.Time" },
        "EndedAt":   { "type": "string", "format": "date-time", "x-go-import": "time", "x-go-type": "time.Time" },
        "Outcome": { "$ref": "#/$defs/EpisodeOutcome" },
        "GapReason": { "type": "string" },
        "TracePath": { "type": "string" } },
      "required": ["EpisodeID", "Kind", "TargetRef", "Usage", "StartedAt", "EndedAt", "Outcome"],
      "additionalProperties": false },
    "EpisodeQuery": { "type": "object", "properties": {
        "ProjectID": { "$ref": "#/$defs/ProjectID" },
        "TargetRef": { "type": "string" } },
      "required": ["ProjectID"], "additionalProperties": false }
  },
  "interface": {
    "name": "EpisodeAccess",
    "layer": "resourceaccess",
    "operations": [
      { "name": "AppendEpisode",
        "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                    { "name": "record", "schema": { "$ref": "#/$defs/EpisodeRecord" } } ],
        "error": true },
      { "name": "ListEpisodes",
        "params": [ { "name": "query", "schema": { "$ref": "#/$defs/EpisodeQuery" } } ],
        "result": { "type": "array", "items": { "$ref": "#/$defs/EpisodeRecord" } },
        "error": true },
      { "name": "ReadTraceEvents",
        "params": [ { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
                    { "name": "episodeID", "schema": { "type": "string" } } ],
        "result": { "type": "array", "items": { "type": "object",
                    "x-go-import": "encoding/json", "x-go-type": "json.RawMessage" } },
        "error": true }
    ]
  }
}
```

(If `x-go-type: json.RawMessage` is rejected by modelgen, fall back to `{"type":"string"}` per event line; record which in the commit message.)

- [ ] **Step 2: Register the component in the system model.** Add to `.slots["5"].model.components`: `{ "id": "episode-access", "kind": "resourceAccess", "layer": "resourceAccess", "contractKey": "episodeAccess", "name": "EpisodeAccess", "encapsulates": ["episode ledger + trace sidecar storage"], "encapsulatesVolatilities": ["how episode observations are retained locally vs deployed"], "atomicBusinessVerbs": ["append episode", "list episodes", "read trace"] }` (match neighboring components' exact field set — copy `usage-access` and edit). Add relationships from `construction-manager`, `systemdesign-manager`, `projectdesign-manager`, and (added in Task 9) `episode-manager` to `episode-access`. Add `"EpisodeAccess"` to the archistrator-server container's components in `.slots["6"]`.

- [ ] **Step 3: Regenerate + gates.**

```bash
cd server && make gen-models && make gen-internal-tools
GOWORK=off go build ./... 
GOWORK=off go test ./internal/ -run 'TestEveryBuiltContractHasModels|TestEveryBuiltContractJoinsAComponent|TestServiceContractsMatchSystemComponents|TestProjectJSONLoadsUnderPublishedProjectmodel' -v
```

Expected: build passes; the four guard tests pass. If `TestMethodLayering`/`appArchSpec()` fails on the new package, add `internal/resourceaccess/episode` with its allowed imports to the spec map in `server/internal/arch_test.go` (~line 197).

- [ ] **Step 4: Regenerate encapsulation allowlist** (`ENCAP_DUMP=1 GOWORK=off go test ./internal/ -run TestGeneratedOnlyPublic -v`, paste output into `arch_test.go` allowlist), run `make encapsulation-check`.

- [ ] **Step 5: Commit** (`feat(episode): episodeAccess contract + system-model registration`).

---

### Task 3: `episodeAccess` implementation (sidecar store)

**Files:**
- Create: `server/internal/resourceaccess/episode/episodeaccess.go` (Rule-1 single impl file)
- Create: `server/internal/resourceaccess/episode/access_test.go` (Rule-3 single test file)

**Interfaces:**
- Consumes: generated `EpisodeAccess` interface + types from Task 2.
- Produces: `NewEpisodeAccess(...)` constructor (exact exported name/signature dictated by the generated contract's constructor convention — copy the delegating-constructor pattern from `internal/resourceaccess/usage`'s impl). Store layout: `<projectRepoRoot>/.aiarch/traces/episodes.jsonl` (ledger, one `EpisodeRecord` JSON per line) + `<projectRepoRoot>/.aiarch/traces/<episodeId>.jsonl` (raw traces, written by agenticjob) + `<projectRepoRoot>/.aiarch/traces/.gitignore` containing `*\n` (self-ignoring — operated repos need no scaffold change).

- [ ] **Step 1: Resolve the project-repo path the same way the local git substrate does.** Read how `projectstate`'s GitLocal substrate maps `projectID → repo root` (start at `server/internal/resourceaccess/projectstate/projectstateaccess.go`, `statePathPrefix` at :49, and the substrate's constructor config). If that resolution helper is package-private, lift it into an existing shared utility package under `server/internal/utility/` (do NOT call projectstate from episode — no RA→RA calls) and have both use it. If lifting is invasive, duplicate the ~few-line resolution with a comment naming the source of truth.

- [ ] **Step 2: Write failing contract tests** in `access_test.go` against a `t.TempDir()` repo root:

```go
func TestAppendThenList(t *testing.T) {
    a := newTestAccess(t)                     // helper wiring tempdir as repo root for project "p1"
    rec := testRecord("ep-1", episode.EpisodeSucceeded) // helper: minimal valid record
    if err := a.AppendEpisode(ctx, "p1", rec); err != nil { t.Fatal(err) }
    got, err := a.ListEpisodes(ctx, episode.EpisodeQuery{ProjectID: "p1"})
    if err != nil || len(got) != 1 || got[0].EpisodeID != "ep-1" { t.Fatalf("got %v err %v", got, err) }
}
func TestListFiltersByTargetRef(t *testing.T)   { /* append 2 records w/ different TargetRef; query one */ }
func TestAppendIsAppendOnly(t *testing.T)       { /* append twice; ledger file has 2 lines; first unchanged */ }
func TestSelfIgnoringGitignore(t *testing.T)    { /* after first append, .aiarch/traces/.gitignore exists, content "*\n" */ }
func TestReadTraceEvents(t *testing.T)          { /* write a 3-line trace file; ReadTraceEvents returns 3 raw events */ }
func TestReadTraceMissingIsError(t *testing.T)  { /* unknown episodeID -> error, not empty */ }
func TestAppendDedupesOnEpisodeID(t *testing.T) { /* same EpisodeID twice -> second append is a no-op (at-least-once Temporal retries) */ }
```

Run: `GOWORK=off go test ./internal/resourceaccess/episode/ -v` — expect FAIL (no impl).

- [ ] **Step 3: Implement** `episodeaccess.go`: `os.MkdirAll(traces)`, write `.gitignore` if absent, append with `os.O_APPEND|os.O_CREATE` + one `json.Marshal`+`\n` per record, fsync; `ListEpisodes` = linear scan with dedupe-by-EpisodeID (last wins) + optional TargetRef filter, sorted by StartedAt; `ReadTraceEvents` = read `<episodeId>.jsonl` lines as `json.RawMessage`. Reject path-traversal in `episodeID` (must match `^[A-Za-z0-9._-]+$`).

- [ ] **Step 4: Run tests to green**, then `make gen-fakes && make encapsulation-check && GOWORK=off go build ./...`.

- [ ] **Step 5: Add `/.aiarch/traces/` to archistrator's own root `.gitignore`** (dogfood repo; operated repos rely on the self-ignoring file).

- [ ] **Step 6: Commit** (`feat(episode): sidecar ledger implementation`).

---

### Task 4: `agenticJobAccess` contract amendment (EpisodeSummary on the observation)

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.agenticJobAccess`
- Generated: `server/internal/resourceaccess/agenticjob/contract.gen.go`, `fake/fake.gen.go`

**Interfaces:**
- Produces: `PipelineObservation` gains optional `Episode *EpisodeSummary`; new `$defs`: `EpisodeSummary` = the observation-side subset of Task 2's record — `EpisodeID, Model, Usage EpisodeUsage, StreamedUsage EpisodeUsage, CostUSD, NumTurns, ToolCallCounts, SubagentSpans, StartedAt, EndedAt, Outcome, TracePath` (NO Kind/TargetRef/Lineage/WorkerClass — those are manager-known, added at persist time). Duplicate the `EpisodeUsage`/`SubagentSpan`/`EpisodeOutcome` defs inside this contract's `$defs` (contracts are self-contained; same shapes as Task 2, keep names identical).

- [ ] **Step 1:** Add the `$defs` + `"Episode": { "$ref": "#/$defs/EpisodeSummary" }` (optional — NOT in `required`) to `PipelineObservation` in the `agenticJobAccess` contract entry.
- [ ] **Step 2:** `make gen-models && make gen-fakes`; `GOWORK=off go build ./...` (expect green — additive optional field).
- [ ] **Step 3:** `GOWORK=off make test-short` for the agenticjob + manager packages; fix any fake-related compile fallout only by regenerating, never by editing `.gen.go`.
- [ ] **Step 4: Commit** (`feat(agenticjob): EpisodeSummary reported on PipelineObservation`).

---

### Task 5: stream-json switch + tee + bounded tail

**Files:**
- Modify: `server/internal/resourceaccess/agenticjob/agenticjobaccess.go` — `claudeArgv` (:2295), the stdout wiring around :1591–1607, `persistFailedRunOutput` (:2118)
- Test: `server/internal/resourceaccess/agenticjob/access_test.go` (existing Rule-3 file)

**Interfaces:**
- Produces (package-private): `type tailBuffer struct` implementing `io.Writer` keeping the last `tailBufferCap = 512 * 1024` bytes; trace file at `<repoPath>/.aiarch/traces/<episodeId>.jsonl`; episode ID minted at submit: `episodeID = "ep-" + <run id already minted at submit>` (deterministic against retries — reuse the existing run-identity the RA mints in `submitClaudeRun` :1350; do not introduce a second random source).

- [ ] **Step 1: Failing tests** (in existing `access_test.go`):

```go
func TestTailBufferKeepsSuffix(t *testing.T) {
    var tb tailBuffer
    big := bytes.Repeat([]byte("x"), 600*1024)
    tb.Write(big); tb.Write([]byte("TERMINAL"))
    s := tb.String()
    if len(s) > 512*1024 || !strings.HasSuffix(s, "TERMINAL") { t.Fatal(len(s)) }
}
func TestClaudeArgvStreamJSON(t *testing.T) {
    args := claudeArgv("p", "mcp", "sandbox")
    // must contain --output-format stream-json AND --verbose; must NOT contain plain "json"
}
```

- [ ] **Step 2: Implement.** In `claudeArgv`, replace `--output-format json` with `--output-format stream-json` + `--verbose`. At the spawn site (:1591 area): create `.aiarch/traces/` under the local checkout root (`repoPath`, resolved at :1172/:1216), open `<episodeId>.jsonl`, and set `cmd.Stdout = io.MultiWriter(traceFile, &tail)` where `tail` is the `tailBuffer` replacing the unbounded `bytes.Buffer`. All downstream consumers of stdout (`awaitCompletion` :1665, `claudeResultEnvelope` :1987, `envelopeDetail` :2024) read from `tail.String()` — they only need the last JSON lines, which the 512KB tail preserves. Close/fsync the trace file in `awaitCompletion` before parsing. `persistFailedRunOutput` (:2118) writes the trace-file *path* into its log dir instead of a verbatim `stdout.json` copy.

- [ ] **Step 3:** Run the package's existing tests + new ones: `GOWORK=off go test ./internal/resourceaccess/agenticjob/ -short -v`. Existing envelope-parsing tests must stay green (stream-json's terminal `result` event carries the same `subtype`/`is_error`/`result` fields — verify against Task 1 fixtures if any test needs its fixture swapped from json to stream-json format).

- [ ] **Step 4: Commit** (`feat(agenticjob): stream-json tee with bounded tail`).

---

### Task 6: stream parser → `EpisodeSummary`

**Files:**
- Modify: `server/internal/resourceaccess/agenticjob/agenticjobaccess.go` (package-private parser — spec: parser stays package-internal)
- Test: `server/internal/resourceaccess/agenticjob/access_test.go`

**Interfaces:**
- Consumes: Task 1 fixtures; Task 4's generated `EpisodeSummary` type.
- Produces (package-private): `func parseEpisodeStream(r io.Reader, episodeID, tracePath string, started, ended time.Time) (agenticjob.EpisodeSummary, error)`.

- [ ] **Step 1: Failing tests, driven by the real fixtures:**

```go
func TestParseEpisodeStreamSuccess(t *testing.T) {
    f, _ := os.Open("testdata/streamjson/success_with_tools.jsonl")
    sum, err := parseEpisodeStream(f, "ep-1", "trace/ep-1.jsonl", t0, t1)
    // assert: err nil; Outcome == succeeded; NumTurns > 0; Usage.In+Usage.Out > 0 (from terminal result);
    // StreamedUsage populated from summed assistant-event usage; ToolCallCounts["Write"] >= 1 (or the
    // actual tool the fixture used — read the fixture once and pin exact expected counts);
    // Model non-empty (from the init/system event or first assistant event).
}
func TestParseEpisodeStreamSubagent(t *testing.T) {
    // success_with_subagent.jsonl: ToolCallCounts["Task"] >= 1; len(SubagentSpans) >= 1;
    // events with parent_tool_use_id are attributed to the span, not the main-loop tool counts;
    // StreamedUsage != Usage is TOLERATED (record both; assert both non-zero, no equality assert).
}
func TestParseEpisodeStreamFailure(t *testing.T)   { /* failure.jsonl -> Outcome failed; error nil (parse succeeds) */ }
func TestParseEpisodeStreamNoTerminal(t *testing.T){ /* fixture minus last line -> Outcome gap, GapReason set, err nil */ }
func TestParseEpisodeStreamGarbage(t *testing.T)   { /* non-JSON lines interleaved are skipped, not fatal */ }
```

Pin exact expected numbers by reading each fixture once (`jq`), then hard-code them in the test.

- [ ] **Step 2: Implement** `parseEpisodeStream`: `bufio.Scanner` (1MB max line), per line decode `{type, subtype?, parent_tool_use_id?, message?{model?, usage?{input_tokens,output_tokens,cache_read_input_tokens,cache_creation_input_tokens}, content?[]}, usage?, total_cost_usd?, num_turns?, is_error?}` into a tolerant struct; accumulate: assistant-event usage → `StreamedUsage`; `content[].type=="tool_use"` → `ToolCallCounts[name]++` (main loop) or subagent span attribution when `parent_tool_use_id != ""` (span keyed by that id; first/last event timestamps as StartedAt/EndedAt); terminal `result` → `Usage` (its `usage`), `CostUSD`, `NumTurns`, Outcome (`success` → succeeded else failed). No terminal event → `Outcome: gap, GapReason: "stream ended without terminal result event"`.

- [ ] **Step 3: Wire into `awaitCompletion`:** on every completion path (success, failure), reopen the trace file and call `parseEpisodeStream`; attach the summary to the run state so `ObserveAgenticJob` (:1791) includes it on the terminal observation. On the cancel short-circuit (:1685–1696) and `CancelAgenticJob` (:1815): still parse whatever trace exists and report `Outcome: cancelled` (override the parsed outcome). On the restart-lost-run path (`localRunLostDiagnostic` :1776/:1800): report `Outcome: gap, GapReason: <diagnostic>` with whatever partial trace exists.

- [ ] **Step 4:** Tests green; full package test run; **Commit** (`feat(agenticjob): episode stream parser + terminal observation reporting`).

---

### Task 7: Manager persistence (construction, systemdesign, projectdesign)

**Files:**
- Modify: `.aiarch/state/project.json` — add `{ "name": "episodes", "component": "episodeAccess" }` to the `deps` of `constructionManager`, `systemDesignManager`, `projectDesignManager` contract entries
- Modify: `server/internal/manager/construction/constructactivity.go` (~:969 `runPipeline` loop and the second loop ~:1267)
- Modify: `server/internal/manager/systemdesign/coauthorartifact.go` (design-dispatch observe loop)
- Modify: `server/internal/manager/projectdesign/projectdesignmanager.go` (~:1744 `dispatchAnswerJob`)
- Generated: each manager's `activities.gen.go`, `invokers.gen.go`, `worker.gen.go` (via `make gen-temporal`)
- Test: each manager's existing `manager_test.go`

**Interfaces:**
- Consumes: `EpisodeAccess.AppendEpisode`; `PipelineObservation.Episode` from Task 4.
- Produces: every terminal observation becomes exactly one `EpisodeRecord` in the ledger, enriched manager-side with `Kind`, `TargetRef`, `WorkerClass`, and `Lineage` from `workflow.GetInfo(ctx)` (never parsed from idempotency keys). The answer-job path appends directly (non-workflow) with `Lineage: nil, Kind: EpisodeKindAnswer`.

- [ ] **Step 1:** Add the dep to the three manager contracts; run `make gen-models && make gen-temporal`. Confirm `invokers.gen.go` now exposes typed `EpisodesAppendEpisode` invokers (name per generator convention — read the generated file).
- [ ] **Step 2: Failing workflow tests** (Temporal test framework, in each `manager_test.go` — copy the package's existing workflow-test setup):

```go
func TestRunPipelinePersistsEpisode(t *testing.T) {
    // fake agenticjob returns terminal observation with Episode set;
    // assert episodeAccess fake received exactly one AppendEpisode with:
    // Kind=EpisodeKindConstruction, TargetRef=<activityID>, Lineage.WorkflowID != "",
    // record fields copied verbatim from the observation summary.
}
func TestRunPipelinePersistsGapWhenSummaryMissing(t *testing.T) {
    // terminal observation with Episode == nil -> AppendEpisode with Outcome=EpisodeGap,
    // GapReason mentioning missing summary. A missing record is never silently missing.
}
func TestAppendFailureDoesNotFailBusinessFlow(t *testing.T) {
    // episodeAccess fake errors -> workflow still completes; append retried per activity
    // retry policy (assert ≥1 retry attempt recorded by the fake).
}
```

- [ ] **Step 3: Implement.** In `runPipeline` (both loops) and the systemdesign observe loop: after a terminal phase (`Succeeded|Failed|Cancelled`), build the `EpisodeRecord` from `obs.Episode` (or gap record if nil), fill `Kind/TargetRef/WorkerClass` from workflow state and `Lineage` from `workflow.GetInfo(ctx).WorkflowExecution`, and call the generated append invoker. Schedule the append AFTER the business handling of the observation (business first, episode second — spec ordering), with its own activity options (unlimited-ish retry, `ScheduleToCloseTimeout` 0, independent of business retries). In `dispatchAnswerJob`: after the fire-and-forget submit completes its observation (follow that function's existing completion callback/goroutine), append directly via the RA dependency with `Kind: EpisodeKindAnswer, Lineage: nil`.
- [ ] **Step 4:** Tests green; `make gen-temporal-check`; update `registeredTemporalNamesGolden` in `server/internal/registered_names_test.go` if new activity names registered (expected: `episodeAccess.appendEpisode` per manager queue); run `make method-check`.
- [ ] **Step 5: Commit** (`feat(managers): persist episode records at terminal observations`).

---

### Task 8: Wire `episodeAccess` into the composition root

**Files:**
- Modify: `.aiarch/state/project.json` (already registered in Task 2 slots; verify)
- Generated: `server/cmd/server/main.gen.go` via `make gen-main` (composegen)
- Possibly modify: `server/cmd/server/hooks.go` (escape hatch), `server/cmd/appgen/main.go` (constructor variant lists)

**Interfaces:**
- Produces: a booting server with `episodeAccess` constructed and injected into the three managers.

- [ ] **Step 1:** `make gen-temporal` (regenerates main.gen.go/config.gen.go). If composegen cannot construct `episodeAccess` (unknown constructor arity for the repo-root config), register the variant in `appgen`'s `generateMain()` variant lists (`VariantConstructorNoError` / `VariantHookArgs`, `server/cmd/appgen/main.go` ~:155 area) or supply the argument via `server/cmd/server/hooks.go` — copy how an existing file-backed component gets its config. Config value: reuse the same env-derived projects-root the GitLocal substrate uses (found in Task 3 Step 1); if a new env var is unavoidable, add it through the deployment-model config so `config.gen.go`/`envnames.gen.go` stay generated.
- [ ] **Step 2:** `GOWORK=off go build ./... && make gen-main-check && make gen-config-check`.
- [ ] **Step 3: Boot smoke:** `scripts/build-local.sh`, start the local stack per `docs`' run-app-locally flow, confirm clean startup logs (no episode wiring panic).
- [ ] **Step 4: Commit** (`feat(server): wire episodeAccess into composition root`).

---

### Task 9: `episodeManager` (thin read manager) + client rail

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.episodeManager` (new), slot-5 component `episode-manager` + relationship to `episode-access`, slot-6 container entry
- Modify: `server/cmd/appgen/main.go:84` (`managers` list) and `:155` (`WebExposedManagers`); `server/cmd/clientgen/main.go:64` (`exposedManagers`)
- Create: `server/internal/manager/episode/episodemanager.go` (Rule-1), `server/internal/manager/episode/manager_test.go` (Rule-3)
- Modify: `server/internal/registered_names_test.go` (golden), `server/internal/arch_test.go` (allowlists/spec map)
- Generated: `server/internal/manager/episode/{contract,activities,invokers,worker}.gen.go`, `server/internal/client/web/episode/episode_handlers.gen.go`, `server/internal/client/mcp/episode/episode_tools.gen.go`, `server/api/openapi.yaml`

**Interfaces:**
- Produces (manager contract, lowerCamel wire props like `billingManager`): `EpisodeManager` with exactly 3 ops —
  - `ListEpisodesForTarget(projectID string, targetRef string) ([]EpisodeRecordView, error)`
  - `GetEpisodeTimeline(projectID string, episodeID string) (EpisodeTimeline, error)` — `EpisodeTimeline { record EpisodeRecordView, events []TimelineEvent }`, `TimelineEvent { seq int, eventType string, raw <json.RawMessage or string> }`
  - `ExportEpisodes(projectID string, targetRef string) (EpisodeExport, error)` — JSON only (`EpisodeExport { records []EpisodeRecordView, traces map[string][]TimelineEvent }`); **CSV is client-side** (spec ruling).
  - `EpisodeRecordView` mirrors Task 2's `EpisodeRecord` with lowerCamel wire props (`x-go-name` for Go casing), and MUST NOT add OCSF/audit fields.
- Consumes: `episodeAccess` (sole dep).

- [ ] **Step 1:** Author the contract (deps: `[{"name":"episodes","component":"episodeAccess"}]`; copy `billingManager` skeleton). Register component/relationships/deployment as in Task 2 Step 2. Add to the three hard-coded lists. Record the **manager-cardinality waiver (5 → 6)** as a note on the slot-5 component entry, per spec §5.
- [ ] **Step 2:** `make gen-models && make gen-temporal && make gen-client && make gen-fakes`. Read the generated files: if the generators require every manager op to be a Temporal workflow, implement each read as a trivial workflow (Rule-2 files `listepisodesfortarget.go`, `getepisodetimeline.go`, `exportepisodes.go`) whose only step is the generated append/list invoker; if plain read methods are supported (check how existing at-read ops in `systemDesignManager` are declared), implement them as plain methods in `episodemanager.go`. Follow whatever the generated `worker.gen.go`/handlers dictate — do not fight the rail.
- [ ] **Step 3: Failing manager tests** in `manager_test.go` (fake `episodeAccess`): list maps records through; timeline stitches record + trace events with sequential `seq`; export bundles both; unknown episode → error surfaced.
- [ ] **Step 4:** Implement to green. Update `registeredTemporalNamesGolden`, encapsulation allowlist, `appArchSpec()`. Run full server verification suite (Global Constraints list).
- [ ] **Step 5:** `make gen-client-check`; confirm `api/openapi.yaml` now carries the three ops.
- [ ] **Step 6: Commit** (`feat(episode): episodeManager read surface through generated client rail`).

---

### Task 10: SPA — Episodes panel, timeline, export

**Files:**
- Generated: `webApp/src/contracts/schema.ts`, `webApp/src/contracts/enums.gen.ts`, `webApp/src/api/ops.gen.ts` (via `npm run gen:api && npm run gen:ops`)
- Create: `webApp/src/components/episodes/EpisodesPanel.tsx`, `webApp/src/components/episodes/EpisodeTimeline.tsx`, `webApp/src/utilities/episodeCsv.ts`, `webApp/src/hooks/useEpisodes.ts`
- Modify: `webApp/src/components/design/SystemDesignView.tsx` (mount panel per artifact page), `webApp/src/components/construction/ActivityTrackingDetail.tsx` and `ArtifactActivityDetail.tsx` (mount per activity), `webApp/src/utilities/constants/UIIdentifiers` (new testids)
- Test: `webApp/src/utilities/episodeCsv.test.ts`

**Interfaces:**
- Consumes: generated ops for `listEpisodesForTarget` / `getEpisodeTimeline` / `exportEpisodes` from `ops.gen.ts` (exact op ids appear after Task 9's regen — read `ops.gen.ts`).
- Produces: `<EpisodesPanel projectId targetRef />` — collapsible panel listing episodes (outcome chip incl. `cancelled`/`gap`, duration, model, tokens in/out/cache, turns, tool count, subagent count) with an **optional `badges` render-prop slot** (spec: audit spine adds assurance/completeness later without forking); row-click expands `<EpisodeTimeline />` (per-turn tokens, tool rows with name + metadata, subagent spans, filter-by-event-type dropdown — dropdown per UI selection convention); **Export** menu: "JSON" (download of `exportEpisodes` result) and "CSV" (client-side flatten via `episodeCsv.ts`).

- [ ] **Step 1:** `cd webApp && npm run gen:api && npm run gen:ops`; commit the regenerated files separately (`chore(webApp): regen API surface for episodeManager`).
- [ ] **Step 2: Failing test for the CSV flattener** (`node --test` — repo has no vitest):

```ts
// episodeCsv.test.ts — flattenEpisodesToCsv(export: EpisodeExport): string
// asserts: header row "episodeId,kind,targetRef,outcome,model,tokensIn,tokensOut,cacheRead,cacheCreate,costUsd,numTurns,startedAt,endedAt";
// one row per record; fields containing commas/quotes are RFC-4180 quoted; \n line endings.
```

- [ ] **Step 3:** Implement `episodeCsv.ts` (pure function, `utilities` layer — no imports above it), run `npm run test` to green.
- [ ] **Step 4:** Implement `useEpisodes.ts` (hooks layer: wraps the two read ops with loading/error state, following an existing hook in `src/hooks/` for the fetch pattern), then the two components (components layer; MUI `Paper` panel modeled on `construction/PhaseGatePanel.tsx`; theme via `useTokens()`). Register testids: `episodes-panel`, `episodes-row`, `episode-timeline`, `episode-export-json`, `episode-export-csv`, `episode-outcome-chip` in `UI_IDENTIFIERS`.
- [ ] **Step 5:** Mount: in `SystemDesignView.tsx` below the artifact renderer with `targetRef = <artifactKind slug>`; in `ActivityTrackingDetail.tsx` + `ArtifactActivityDetail.tsx` with `targetRef = activityId`.
- [ ] **Step 6:** `npm run check` (typecheck, lint incl. boundaries DAG, format, test) — green.
- [ ] **Step 7: Commit** (`feat(webApp): episodes panel + timeline + JSON/CSV export`).

---

### Task 11: Playwright spec + real-state validation + founder review stop

**Files:**
- Create: `uitests/tests/episodes-panel.spec.ts`
- Modify (if fixture regen needed): per `uitests/README`/existing fixture scripts

**Interfaces:**
- Consumes: testids from Task 10; a locally provisioned stack (recipe: `.github/workflows/uitests.yml` — Postgres + Temporal + `go build ./cmd/server` + `ARCHISTRATOR_*` env; SPA auto-started by Playwright config).

- [ ] **Step 1: Generate real episode state:** with the local stack up, run one real dry-run-off local design dispatch on the dogfood project (smallest available: a design-artifact draft) so the ledger holds ≥1 real episode. If a full real episode is impractical in the loop, append a **captured-fixture-derived** record via a small Go seeding helper in `uitests` support (fed from Task 1 fixtures — still not hand-authored).
- [ ] **Step 2: Spec** (model on `tests/design-experience.spec.ts` structure, `data-testid` selectors only): panel renders ≥1 row on a design artifact page; row expands to timeline with ≥1 tool event; **gap/cancelled outcome renders its chip** (seed one gap record — the spec's "badges are the point" acceptance); export JSON click downloads a file whose parsed content has `records.length ≥ 1`; export CSV click downloads a file whose first line is the exact Task-10 header.
- [ ] **Step 3:** `cd uitests && npm test -- episodes-panel.spec.ts` → green.
- [ ] **Step 4: STOP for founder review** of the rendered UI (standing UI review loop). Do not proceed to Task 12 until reviewed.
- [ ] **Step 5: Commit** (`test(uitests): episodes panel coverage incl. gap outcome`).

---

### Task 12: Full-suite verification + spec/status closeout

- [ ] **Step 1:** From `server/`: the entire Global Constraints verification list, including every `gen-*-check`, `make encapsulation-check`, `make sumtype-check`, `make method-check`.
- [ ] **Step 2:** From `webApp/`: `npm run check`. From `uitests/`: full `npm test`. From `systemtests/`: `make test-short`.
- [ ] **Step 3:** Update `docs/superpowers/specs/2026-08-02-self-improvement-pipeline-design.md` §5 status note: SP1 implemented; record any deviations discovered during implementation (e.g. json.RawMessage fallback, workflow-vs-plain read decision from Task 9) as dated amendments.
- [ ] **Step 4: Commit** (`chore: SP1 capture seam + trace UI complete`).

---

## Explicitly deferred (per spec)

- GH-venue capture (upload-artifact + server pull) — seam-noted, off the AC critical path (no local checkout to hold a sidecar).
- Deployed substrate profile for `episodeAccess` — local sidecar only in v1.
- Any audit-spine machinery (child workflows, sealing, OCSF) — separate project.
