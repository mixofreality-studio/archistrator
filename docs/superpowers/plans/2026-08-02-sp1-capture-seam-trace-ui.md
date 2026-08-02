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
  --output-format stream-json --verbose --max-turns 6 --dangerously-skip-permissions \
  > success_with_tools.jsonl
```

(`--dangerously-skip-permissions` is required: headless `-p` otherwise denies Write/Read/Task
tool calls, and the fixture would capture permission refusals instead of `tool_use` events.
Scratch dir, trivial prompt — harmless.)

- [ ] **Step 2: Capture a subagent fixture** (exercises `parent_tool_use_id` sidechain events):

```bash
claude -p 'Use the Task tool to dispatch one subagent that answers: what is 2+2? Then report its answer.' \
  --output-format stream-json --verbose --max-turns 6 --dangerously-skip-permissions \
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
        "result": { "type": "array", "items": { "type": ["null"],
                    "x-go-import": "encoding/json", "x-go-type": "json.RawMessage" } },
        "error": true }
    ]
  }
}
```

**No `infra` entry** — modelgen's infra table is a closed platform allowlist
(`Git, GitHub, Postgres, Temporal, GitHubActions, Anthropic, Ollama, Replay, Profiled` —
framework-go-app-generator `modelgen/infra.go:79-205`; unknown names hard-fail
`emit_impl.go:80-82`; there is no filesystem binding). The RA gets a generated interface +
models only; the variant constructors are **hand-written in Task 3**
(`NewLocalFSEpisodeAccess(repoURL string) (EpisodeAccess, error)` +
`NewNoOpEpisodeAccess() EpisodeAccess`), exported, and added to the encapsulation allowlist —
exact precedent: `NewLocalExecAgenticJobAccess` / `NewNoOpOperatedSystemStateAccess`.

`x-go-type: json.RawMessage` with `"type": ["null"]` is the established shape — three live
precedents in project.json (e.g. `projectDesignManager/$defs/DraftModel/properties/model`),
one already web-exposed through clientgen.

- [ ] **Step 2: Register the component in the system model.** Add to `.slots["5"].model.components`: `{ "id": "episode-access", "kind": "resourceAccess", "layer": "resourceAccess", "contractKey": "episodeAccess", "name": "EpisodeAccess", "encapsulates": "episode ledger + trace sidecar storage", "encapsulatesVolatilities": [...], "atomicBusinessVerbs": [...] }` — note `encapsulates` is a **string** in real entries; match neighboring components' exact field set (copy `usage-access` and edit). Add relationships from `construction-manager`, `systemdesign-manager`, and `projectdesign-manager` to `episode-access` (these carry both the Task-7 writes and the Task-9 facet reads — no other component touches it, per the 2026-08-02 facet ruling). Add `"EpisodeAccess"` to the archistrator-server container's components in `.slots["6"]`. **Also add a deployment binding** (composegen has no construction recipe without one): `{ "component": "episodeAccess", "presence": "required", "settings": [], "perProfile": { "local": { "variant": "LocalFS", "infra": [] }, "cloud": { "variant": "NoOp", "infra": [] } } }` — the NoOp variant appends/lists nothing (explicit, logged) until the deferred deployed profile lands; never a nil RA under an unbounded-retry activity.

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
- Produces: hand-written variant constructors `NewLocalFSEpisodeAccess(repoURL string) (EpisodeAccess, error)` and `NewNoOpEpisodeAccess() EpisodeAccess` (precedent: `NewLocalExecAgenticJobAccess`; do NOT copy usage's delegating ctor — that one is generated from its `infra` entry). Store layout: `<projectRepoRoot>/.aiarch/traces/episodes.jsonl` (ledger, one `EpisodeRecord` JSON per line) + `<projectRepoRoot>/.aiarch/traces/<episodeId>.jsonl` (raw traces, written by agenticjob) + `<projectRepoRoot>/.aiarch/traces/.gitignore` containing `*\n` (self-ignoring — operated repos need no scaffold change).

- [ ] **Step 1: Resolve the repo root the way the local rail actually works.** The local rail is **one repo per server config** (`NewGitLocalProjectStateAccess(cfg.ProjectStateGitRepoURL)`, name-as-identity) — there is no projectID→path mapping. `NewLocalFSEpisodeAccess(repoURL)` performs the same `file://` URL → path translation as `localRepoPath` (`agenticjobaccess.go:1249`); `projectID` is recorded/validated on the record, never used for path resolution.

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

- [ ] **Step 2: Implement.** In `claudeArgv`, replace `--output-format json` with `--output-format stream-json` + `--verbose`. At the spawn site (:1591 area): create `.aiarch/traces/` under the local checkout root (`repoPath`, resolved at :1172/:1216), open `<episodeId>.jsonl`, and set `cmd.Stdout = io.MultiWriter(traceFile, &tail)` where `tail` is the `tailBuffer` replacing the unbounded `bytes.Buffer`. All downstream consumers of stdout (`awaitCompletion` :1665, `claudeResultEnvelope` :1987, `envelopeDetail` :2024) read from `tail.String()` — they only need the last JSON lines, which the 512KB tail preserves. Close/fsync the trace file in `awaitCompletion` before parsing. `persistFailedRunOutput` (:2118) writes the trace-file *path* into its log dir instead of a verbatim `stdout.json` copy. Open the trace file with `O_TRUNC` at dispatch so a retry-reused episode id can never interleave two runs' streams (verify whether idempotency keys are attempt-scoped; truncate regardless). **Trust-rule assertion:** the agent sandbox write allowlist is `[workDir, gitDir]` (`agenticjobaccess.go:1578-1581`, `gitDir` from `git rev-parse --absolute-git-dir` at :2549) — add a test + runtime check that the resolved traces dir is NOT under `gitDir`; if the shared repo is bare (`gitDir == repoPath`), fail loudly rather than tee into agent-writable space.

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

- [ ] **Step 3: Wire into `awaitCompletion`:** on every completion path (success, failure), reopen the trace file and call `parseEpisodeStream`; attach the summary to the run state so `ObserveAgenticJob` (:1786) includes it on the terminal observation. **On cancel: do NOT parse inside `CancelAgenticJob` (the subprocess is still exiting there — trace mid-write); modify `awaitCompletion`'s `alreadyCancelled` short-circuit (~:1706), which today returns before doing any work, to close the trace file, parse, and attach the summary with `Outcome: cancelled` (overriding the parsed outcome) before returning.** On the restart-lost-run path (`localRunLostDiagnostic` :1776/:1800): report `Outcome: gap, GapReason: <diagnostic>` with whatever partial trace exists (the episode id is recoverable from the handle via `localTokenFromHandle` :1861).

- [ ] **Step 4:** Tests green; full package test run; **Commit** (`feat(agenticjob): episode stream parser + terminal observation reporting`).

---

### Task 7: Manager persistence (construction, systemdesign, projectdesign)

**Files:**
- Modify: `.aiarch/state/project.json` — add `{ "name": "episodes", "component": "episodeAccess" }` to the `deps` of `constructionManager`, `systemDesignManager`, `projectDesignManager` contract entries
- Modify: `server/internal/manager/construction/constructactivity.go` (~:969 `runPipeline` loop and the second loop ~:1267)
- Modify: `server/internal/manager/systemdesign/coauthorartifact.go` (`dispatchAndObserve` ~:2465 — one choke point covers draft AND critique dispatches)
- Modify: `server/internal/manager/projectdesign/coauthorphase2artifact.go` (~:1475 `dispatchAndObserve` — the Phase-2 twin of systemdesign's; hook the append at the same terminal-observation point. Omitting this silently loses every Phase-2 drafting episode)
- Modify: `server/internal/manager/projectdesign/projectdesignmanager.go` (:1715 `dispatchAnswerJob`)
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

- [ ] **Step 3: Implement.** In `runPipeline` (both loops), systemdesign's `dispatchAndObserve` (coauthorartifact.go:2465), and projectdesign's `dispatchAndObserve` (coauthorphase2artifact.go:1475): after a terminal phase (`Succeeded|Failed|Cancelled`), build the `EpisodeRecord` from `obs.Episode` (or gap record if nil), fill `Kind/TargetRef/WorkerClass` from workflow state and `Lineage` from `workflow.GetInfo(ctx).WorkflowExecution`, and call the generated append invoker. Schedule the append AFTER the business handling of the observation (business first, episode second — spec ordering), with its own activity options (unlimited-ish retry, `ScheduleToCloseTimeout` 0, independent of business retries). In `dispatchAnswerJob` (:1715): **there is NO existing observation of the answer job — it is genuinely fire-and-forget.** After a successful submit, spawn a bounded manager-side goroutine that polls `m.pipeline.ObserveAgenticJob(handle)` until a terminal phase or a hard deadline (reuse the local run-timeout bound), then appends via the RA dep with `Kind: EpisodeKindAnswer, Lineage: nil`; on deadline without a terminal phase, append an explicit gap record. This path is non-durable (a server restart loses the goroutine) — acceptable and documented: only the workflow-side paths carry the durable never-silent guarantee.
- [ ] **Step 4:** Tests green; `make gen-temporal-check`; update `registeredTemporalNamesGolden` in `server/internal/registered_names_test.go` if new activity names registered (expected: `episodeAccess.appendEpisode` per manager queue); run `make method-check`.
- [ ] **Step 5: Release note + commit.** Add to the commit body and to Task 12's closeout notes: **DRAIN in-flight construction/coauthor workflows before deploying this change** — it inserts a new episode-append activity command into existing workflow bodies (not the pure-addition case `pumpnextactivity.go:44` describes; no `GetVersion` guard is carried), so in-flight executions would replay against a changed command sequence. Same standing convention as the callchain and layer-layout releases. Commit (`feat(managers): persist episode records at terminal observations`).

---

### Task 8: Wire `episodeAccess` into the composition root

**Files:**
- Modify: `.aiarch/state/project.json` (already registered in Task 2 slots; verify)
- Generated: `server/cmd/server/main.gen.go` via `make gen-main` (composegen)
- Possibly modify: `server/cmd/server/hooks.go` (escape hatch), `server/cmd/appgen/main.go` (constructor variant lists)

**Interfaces:**
- Produces: a booting server with `episodeAccess` constructed and injected into the three managers.

- [ ] **Step 1:** `make gen-temporal` (regenerates main.gen.go/config.gen.go). Register the constructor variant exactly as the existing file-backed precedent: `VariantHookArgs["episodeAccess/LocalFS"] = [{GoType: "string"}]` in `generateMain()` (`server/cmd/appgen/main.go` ~:177 — `constructionTransitionAccess/GitLocal` is the copy-from), with the hook returning `cfg.ProjectStateGitRepoURL` — **no new env var**. The cloud profile constructs the NoOp variant per Task 2's binding.
- [ ] **Step 2:** `GOWORK=off go build ./... && make gen-main-check && make gen-config-check`.
- [ ] **Step 3: Boot smoke:** `scripts/build-local.sh`, start the local stack per `docs`' run-app-locally flow, confirm clean startup logs (no episode wiring panic).
- [ ] **Step 4: Commit** (`feat(server): wire episodeAccess into composition root`).

---

### Task 9: Facet read ops on the three dispatching managers (founder ruling 2026-08-02: NO new `episodeManager`)

**Ruling context:** episode observability is a facet of existing use cases, not a Manager-layer
volatility (spec §5 amendment, 2026-08-02). The reads live on the managers that already persist
episodes (Task 7 gave each the `episodeAccess` dep). Roster stays at 5 — no cardinality waiver,
no framework-go release. The whole-project `exportEpisodes` REST op is **cut from v1**
(per-target export is client-side in Task 10; the bench harness reads the sidecar directly).

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.{constructionManager,systemDesignManager,projectDesignManager}` (add facet ops + episode view `$defs`; the `episodeAccess` deps already exist from Task 7)
- Modify: `server/internal/manager/construction/constructionmanager.go`, `server/internal/manager/systemdesign/systemdesignmanager.go`, `server/internal/manager/projectdesign/projectdesignmanager.go` (plain-method implementations)
- Modify: each manager's `manager_test.go`
- Generated: each manager's `contract.gen.go` + `fake/fake.gen.go`; existing handler/tool files under `server/internal/client/web/*` and `server/internal/client/mcp/*`; `server/api/openapi.yaml`; `../systemtests/internal/sdk` (regenerated by the same appgen run)

**Interfaces:**
- Produces (manager contracts, lowerCamel wire props + `x-go-name` like `billingManager`):
  - `constructionManager`: `ListEpisodesForActivity(projectID string, activityID string) ([]EpisodeRecordView, error)` + `GetEpisodeTimeline(projectID string, episodeID string) (EpisodeTimeline, error)`
  - `systemDesignManager`: `ListEpisodesForArtifact(projectID string, artifactKind string) ([]EpisodeRecordView, error)` + `GetEpisodeTimeline(projectID string, episodeID string) (EpisodeTimeline, error)`
  - `projectDesignManager`: same two ops as systemDesignManager (Phase-2 session views; these facets collapse into one when the ratified DesignManager merge lands)
  - Shared view shapes, defined per contract (self-contained contracts repeat view types — `PipelinePhase` exists in four contracts today): `EpisodeRecordView` mirrors Task 2's `EpisodeRecord` with lowerCamel wire props, MUST NOT add OCSF/audit fields; `EpisodeTimeline { record EpisodeRecordView, events []TimelineEvent }`; `TimelineEvent { seq int, eventType string, raw json.RawMessage }` (the `["null"]` + `x-go-type` shape from Task 2 — no string fallback).
- Consumes: each manager's existing `episodeAccess` dep (from Task 7).

- [ ] **Step 1:** Add the facet ops + episode view `$defs` to the three existing manager contract entries in `.serviceContracts` (lowerCamel wire props + `x-go-name`; copy the `$defs` shapes from Task 2, converting casing). **No new component, no slot-5/slot-6 entries, no hard-coded list changes, no new task queue** — the managers already exist on the rail. Op-count note: systemDesignManager is already the fattest contract (14 ops); +2 may trip the App-C avoid-12 *warning* — that one IS Warning-severity and waivable; add the waiver entry if methodcheck flags it, justified by the pending DesignManager merge.
- [ ] **Step 2:** `make gen-models && make gen-fakes && make gen-client && make gen-temporal`. **All facet ops are plain methods** — generated web handlers call the manager directly (`h.Manager.AdvancePhase(rc, …)`, `web/systemdesign/system-design_handlers.gen.go:106`), and existing manager read ops are plain methods calling the RA (`systemDesignManager.ListProjects`/`GetProject`, systemdesignmanager.go:2164/:2183 — no Temporal). No Rule-2 workflow files, no new workflow registrations, no `registeredTemporalNamesGolden` workflow changes (activity names were already updated in Task 7). **Note:** `make gen-temporal` also regenerates the systemtests SDK (`../systemtests/internal/sdk` — `gen-sdk-check` diffs it); commit those files with this task.
- [ ] **Step 3: Failing manager tests** in each `manager_test.go` (fake `episodeAccess`): list maps records through with the manager's targetRef semantics (activityID vs artifactKind); timeline stitches record + trace events with sequential `seq`; unknown episode → error surfaced.
- [ ] **Step 4:** Implement to green (plain methods in each manager's Rule-1 impl file). Run full server verification suite (Global Constraints list).
- [ ] **Step 5:** `make gen-client-check`; confirm `api/openapi.yaml` carries the six ops across the three existing surfaces.
- [ ] **Step 6: Commit** (`feat(managers): episode facet read ops on construction/systemdesign/projectdesign`).

---

### Task 9b: System-model dynamic-view realization

**Files:**
- Modify: `.aiarch/state/project.json` — `.slots["5"].model.dynamicViews`

**Why this task exists:** `DV-STATIC-COVERAGE`/`DV-REL-COVERAGE` run at **SeverityError**
(`methodcheck/rules_dynamic.go:127-131`) — every non-Resource/non-Utility component must
appear in ≥1 dynamic view and every static sync relationship must be exercised by one.
`episode-access` and its new relationships appear in no view until this task.
(The former SYS-CARD-MGR founder gate is **resolved**: founder ruling 2026-08-02 = facet
reads, no 6th manager — the >5-managers Error never fires. Roster stays 5.)

- [ ] **Step 1: Dynamic-view coverage.** Extend the affected use-case realizations in `.slots["5"].model.dynamicViews`: add one call fragment `{from: <dispatching manager>, to: "episode-access", mode: "sync", label: "appendEpisode (terminal-observation episode record)"}` on the terminal-observation step of each agentic-dispatch view (construction activity, design draft, Phase-2 draft), and extend the relevant page-read realizations with the facet-read fragments (actor → webClient → <owning manager> → episode-access) keyed to their use cases' activity diagrams in `.coreUseCases`. Run `make method-check` — DV rules green, no new waivers expected (only the possible SDM op-count warning from Task 9 Step 1).
- [ ] **Step 2: Commit** (`docs(model): realize episode-access in dynamic views`).

---

### Task 10: SPA — Episodes panel, timeline, export

**Files:**
- Generated: `webApp/src/contracts/schema.ts`, `webApp/src/contracts/enums.gen.ts`, `webApp/src/api/ops.gen.ts` (via `npm run gen:api && npm run gen:ops`)
- Create: `webApp/src/components/episodes/EpisodesPanel.tsx`, `webApp/src/components/episodes/EpisodeTimeline.tsx` (both **pure, props-only** — components layer cannot import hooks: `eslint.platform.config.js:53/:60`), `webApp/src/containers/EpisodesPanelContainer.tsx` (hooks wiring), `webApp/src/utilities/episodeCsv.ts`, `webApp/src/hooks/useEpisodes.ts`
- Modify: `webApp/src/containers/SystemDesignContainer.tsx` + the Phase-2 equivalent reached from `webApp/src/routes/ProjectDesignExperience.tsx` (mount container per design-artifact page, Phase 1 AND Phase 2), the construction route/container that renders `ActivityTrackingDetail`/`ArtifactActivityDetail` (mount per activity — never from inside a components-layer file; if inline placement inside those detail components' layout is required, pass `episodesSlot?: ReactNode` down as a prop), `webApp/src/utilities/constants/UIIdentifiers` (new testids)
- Test: `webApp/src/utilities/episodeCsv.test.ts`

**Interfaces:**
- Consumes: generated ops from `ops.gen.ts` (exact op ids appear after Task 9's regen — read `ops.gen.ts`): `listEpisodesForActivity` + construction `getEpisodeTimeline` on activity pages; `listEpisodesForArtifact` + the owning design manager's `getEpisodeTimeline` on Phase-1/Phase-2 design pages. There is **no** `exportEpisodes` op (cut per the 2026-08-02 facet ruling) — the export button assembles its payload client-side from the list + timeline ops already fetched.
- Produces: `<EpisodesPanelContainer projectId targetRef />` (container) rendering `<EpisodesPanel />` (pure) — collapsible panel listing episodes (outcome chip incl. `cancelled`/`gap`, duration, model, **worker class**, tokens in/out/cache, turns, tool count, subagent count, and the **lineage tree**: workflow → activity → episode → subagent spans, per spec §5) with an **optional `badges` render-prop slot** (spec: audit spine adds assurance/completeness later without forking); row-click expands `<EpisodeTimeline />` (per-turn tokens, tool rows with name + metadata, subagent spans, filter-by-event-type dropdown — dropdown per UI selection convention); **Export** menu: "JSON" (download of a client-assembled `EpisodeExport { records, traces }` built from the list + timeline ops for the current target) and "CSV" (client-side flatten via `episodeCsv.ts` over the same assembled value).

- [ ] **Step 1:** `cd webApp && npm run gen:api && npm run gen:ops`; commit the regenerated files separately (`chore(webApp): regen API surface for episodeManager`).
- [ ] **Step 2: Failing test for the CSV flattener** (`node --test` — repo has no vitest):

```ts
// episodeCsv.test.ts — flattenEpisodesToCsv(exp: EpisodeExport): string
// EpisodeExport is a client-side type in episodeCsv.ts: { records: EpisodeRecordView[], traces: Record<string, TimelineEvent[]> }
// asserts: header row "episodeId,kind,targetRef,outcome,model,workerClass,tokensIn,tokensOut,cacheRead,cacheCreate,costUsd,numTurns,startedAt,endedAt";
// one row per record; fields containing commas/quotes are RFC-4180 quoted; \n line endings.
```

- [ ] **Step 3:** Implement `episodeCsv.ts` (pure function, `utilities` layer — no imports above it), run `npm run test` to green.
- [ ] **Step 4:** Implement `useEpisodes.ts` (hooks layer: wraps the two read ops with loading/error state, following an existing hook in `src/hooks/` for the fetch pattern), `EpisodesPanelContainer.tsx` (containers layer: calls `useEpisodes`, passes data down), then the two pure components (components layer; props-only; MUI `Paper` panel modeled on `construction/PhaseGatePanel.tsx`; theme via `useTokens()`). Register testids: `episodes-panel`, `episodes-row`, `episode-timeline`, `episode-lineage-tree`, `episode-export-json`, `episode-export-csv`, `episode-outcome-chip` in `UI_IDENTIFIERS`.
- [ ] **Step 5:** Mount the **container** from containers/routes only: `SystemDesignContainer.tsx` below the artifact renderer with `targetRef = <artifactKind slug>` (Phase 1), the Phase-2 design container reached from `ProjectDesignExperience.tsx` (same pattern), and the construction route/container that renders the activity detail components with `targetRef = activityId` (pass `episodesSlot` prop if inline placement is needed).
- [ ] **Step 6:** `npm run check` (typecheck, lint incl. boundaries DAG, format, test) — green.
- [ ] **Step 7: Commit** (`feat(webApp): episodes panel + timeline + JSON/CSV export`).

---

### Task 11: Playwright spec + real-state validation + founder review stop

**Files:**
- Create: `uitests/tests/episodes-panel.spec.ts`
- Modify (if fixture regen needed): per `uitests/README`/existing fixture scripts

**Interfaces:**
- Consumes: testids from Task 10; a locally provisioned stack (recipe: `.github/workflows/uitests.yml` — Postgres + Temporal + `go build ./cmd/server` + `ARCHISTRATOR_*` env; SPA auto-started by Playwright config).

- [ ] **Step 1: Generate real episode state:** with the local stack up, run one real dry-run-off local design dispatch on the dogfood project (smallest available: a design-artifact draft) so the ledger holds ≥1 real episode. If a full real episode is impractical in the loop, append a **captured-fixture-derived** record via a small Go seeding helper housed with the existing fixture tooling at `server/cmd/gen-uitests-fixtures` (fed from Task 1 fixtures — still not hand-authored).
- [ ] **Step 2: Spec** (model on `tests/design-experience.spec.ts` structure, `data-testid` selectors only): panel renders ≥1 row on a design artifact page; row expands to timeline with ≥1 tool event; **gap/cancelled outcome renders its chip** (seed one gap record — the spec's "badges are the point" acceptance); export JSON click downloads a file whose parsed content has `records.length ≥ 1`; export CSV click downloads a file whose first line is the exact Task-10 header.
- [ ] **Step 3:** `cd uitests && npm test -- episodes-panel.spec.ts` → green.
- [ ] **Step 4: STOP for founder review** of the rendered UI (standing UI review loop). Do not proceed to Task 12 until reviewed.
- [ ] **Step 5: Commit** (`test(uitests): episodes panel coverage incl. gap outcome`).

---

### Task 12: Full-suite verification + spec/status closeout

- [ ] **Step 1:** From `server/`: the entire Global Constraints verification list, including every `gen-*-check`, `make encapsulation-check`, `make sumtype-check`, `make method-check`.
- [ ] **Step 2:** From `webApp/`: `npm run check`. From `uitests/`: full `npm test`. From `systemtests/`: `make test-short`.
- [ ] **Step 3:** Update `docs/superpowers/specs/2026-08-02-self-improvement-pipeline-design.md` §5 status note: SP1 implemented; record the deviations as dated amendments — at minimum: self-ignoring sidecar `.gitignore` instead of the method-assets scaffold change (system-architect endorsed), the facet-reads ruling already amended into §5 (no episodeManager, export op cut), the NoOp cloud variant, and the **DRAIN-before-deploy** release note from Task 7.
- [ ] **Step 4: Commit** (`chore: SP1 capture seam + trace UI complete`).

---

## Explicitly deferred (per spec)

- GH-venue capture (upload-artifact + server pull) — seam-noted, off the AC critical path (no local checkout to hold a sidecar).
- Deployed substrate profile for `episodeAccess` — local sidecar only in v1.
- Any audit-spine machinery (child workflows, sealing, OCSF) — separate project.
