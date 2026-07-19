# Local-First Init Funnel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `archistrator init` in any directory boots the full product locally — design (UC1+UC2) *and* construction — on the user's existing Claude Code subscription, with zero external dependencies (no Postgres, no GitHub App, no Anthropic API key, no cloud).

**Architecture:** Driver ≠ worker, and **Claude Code is the process manager** (Serena pattern): `.mcp.json` registers `archistrator mcp` as a **stdio** MCP server, so starting Claude Code in the repo auto-spawns the whole stack — the same process speaks MCP over stdio and boots the HTTP listener (embedded SPA + REST API) in-process. The local Go server orchestrates via embedded Temporal; all LLM work (drafting *and* construction) runs as headless `claude -p` subprocesses riding the user's Pro/Max OAuth — verified: headless mode uses subscription auth, no API key needed. The SPA (embedded in the binary, served at `/`) is the render surface; its URL is surfaced through MCP tool responses so the driver can hand it to the user. `.aiarch/` shape stays byte-identical to cloud — the repo itself is the migration artifact to hosted mode. MCP Apps is explicitly NOT a surface here (Claude Code doesn't render `ui://`; claude.ai/Desktop rendering has an open bug — ext-apps#671).

**Tech Stack:** Go (existing server + `go:embed` for SPA dist), embedded Temporal dev-server (existing local profile), `claude` CLI as the single certified engine (`workerAccess` seam stays engine-generic; no second engine in this plan), platform `framework-go-infrastructure-llm` for the new provider.

## Global Constraints

- **Zero external dependencies in local profile**: boots with nothing installed but `git` and `claude`. No `ARCHISTRATOR_POSTGRES_URL`, no Keycloak, no GitHub creds, no `ANTHROPIC_API_KEY` (v1 caveat: `temporal` CLI on PATH is currently required — spawned as managed dev-server; founder decision pending on embedding go.temporal.io/server vs vendoring).
- **`.aiarch/` layout identical to cloud** (same `statePathPrefix = ".aiarch/state"`, same codecs, passes `cmd/validate`). No local-only state dialects.
- **Design artifacts lead**: the dogfood project.json operational-concepts update (Task 1) lands before any code task. The Method applies to archistrator itself.
- **One certified engine**: `claude` CLI. The llm provider seam stays generic; do NOT add codex/copilot arms.
- **Never edit `*.gen.go`/`*.gen.ts` by hand**; change the generator input and re-run.
- **Auth floor even in vibes mode**: local server binds `127.0.0.1` only; `ARCHISTRATOR_AUTH_DEV_MODE=true` is acceptable ONLY on loopback.
- Gates before every commit — `server`: `GOWORK=off go test ./...`; `webApp`: `npm run lint && npm run build`. Conventional commits.

---

### Task 1: Represent the local deployment mode in archistrator's own design state

The dogfood `project.json` slot 6 (`KindOperationalConcepts`) currently declares `deployment.deliveryStyle: "cloud"` and a single `cloud` environment. The founder-ratified product thesis adds a first-class local mode; the design must say so before code does.

**Files:**
- Modify: `.aiarch/state/project.json` (slot `6` → `model.deployment` and `model.decisions`)

**Interfaces:**
- Produces: a `local` entry in `deployment.environments` and an amended decision; Tasks 2–7 implement what this declares.

- [ ] **Step 1: Add the `local` environment profile** to `slots.6.model.deployment.environments`, alongside the existing `cloud` entry:

```json
{
  "profile": "local",
  "title": "Local (developer laptop, archistrator init)",
  "nodes": [
    {
      "name": "Developer laptop",
      "technology": "single Go binary",
      "description": "archistrator init: server + embedded Temporal dev-server + embedded SPA on 127.0.0.1. Project state = on-disk git (.aiarch). All LLM work = headless claude CLI subprocesses on the user's own Claude subscription (driver=Claude Code interactive via /mcp; worker=claude -p). No Postgres, no Keycloak, no GitHub App, no Anthropic key held by archistrator.",
      "instances": 1,
      "tags": [],
      "children": [],
      "infrastructureNodes": [],
      "containerInstances": [
        { "containerKey": "archistrator-server", "note": "usageAccess=no-op; billing absent; construction executor=local headless claude", "tags": [] }
      ],
      "softwareSystemInstances": []
    }
  ]
}
```

- [ ] **Step 2: Amend the "deployment scenarios are configuration, not architecture" decision** — append to its `decision` text: local-first is the free acquisition funnel (design + construction on the user's own subscription); hosted deploy/operate is the paid tier; the `.aiarch` repo is the migration artifact between them. Do not add components — this is a profile, not architecture (consistent with the existing local-profile design conclusion "no system changes").

- [ ] **Step 3: Validate and note the drift rule**

Run: `cd server && GOWORK=off go run ./cmd/validate ../.aiarch/state/project.json` (locate exact invocation via `ls cmd/validate`).
Expected: PASS. Note: direct edit + validate is the established seed-repo pattern for the dogfood state; a Begin/co-author amend rail round-trip is NOT required here.

- [ ] **Step 4: Commit** — `docs(design): operational concepts — local-first deployment mode (init funnel)`

---

### Task 2: De-Postgres the local profile (no database, period)

Project state is git; Postgres exists only for usage metering, which local mode does not do.

**Files:**
- Modify: the config generator input that produces `cmd/server/config.gen.go` (locate: `grep -rn "requiredEnvByProfile" cmd/ --include='*.go' -l` then find its generator — likely `cmd/internaltoolsgen` or a `//go:generate` directive at the top of `config.gen.go`)
- Modify: `cmd/server/hooks.go` (usage-store profile arm)
- Test: alongside existing hooks/config tests

**Interfaces:**
- Produces: `requiredEnvByProfile["local"] == []`; a `usageAccess` no-op arm selected by the local profile (every method returns success/zero — metering is a cloud concern).

- [ ] **Step 1: Failing test** — construct config with only `ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true` set; assert `MissingFor("local")` is empty and the composition root builds without Postgres. Expected now: FAIL (missing `ARCHISTRATOR_POSTGRES_URL`).
- [ ] **Step 2: Regenerate config** with `local: {}` in the generator's profile table; add the no-op usage arm in `hooks.go` following the existing stub-arm pattern (`revenueLedgerAccess (stub)` precedent, visible in boot logs).
- [ ] **Step 3: Also fix the dead check while in the generator**: `LoadConfig`'s `var missing []string` is never appended to (`config.gen.go:96-98`) — make the generator emit the loop that fills it, or delete the vestigial block so enforcement is honestly `MissingFor`-only.
- [ ] **Step 4: Tests pass; boot the server locally with ONLY the git-local flag; verify clean startup log (no Postgres line).**
- [ ] **Step 5: Commit** — `feat(server): local profile boots with zero external dependencies`

---

### Task 3: `claude-local` worker provider (headless Claude Code as the engine)

**Files:**
- Create: `framework-go-infrastructure-llm/claudecli.go` (+ `claudecli_test.go`) in `archistrator-platform` (consumed via the existing `go.work` overlay)
- Modify: `cmd/server/hooks.go` — worker provider selection: local profile + no `ANTHROPIC_API_KEY` → claude-local

**Interfaces:**
- Consumes: the module's existing provider surface — mirror `anthropic.go`'s implemented method set exactly (`Generate`, `GenerateToolTurn`, same request/response structs from `llm.go`).
- Produces: a provider that shells `claude -p --output-format json` with prompt on stdin; subscription OAuth is ambient (no key env passed). Errors map into the existing taxonomy: non-zero exit + auth-shaped stderr → Terminal (mirror the documented 400-credit-error classification in `anthropic.go:154`); timeouts/5xx-shaped → Transient (Temporal retries, same as cloud).

- [ ] **Step 1: Read `anthropic.go` and `ollama_tools.go` end-to-end first** — the tool-turn contract (does the provider return tool_use blocks to the caller loop?) dictates the design below; confirm before coding.
- [ ] **Step 2: Failing tests** using a fake `claude` shim script on `PATH` (temp dir): happy JSON, malformed JSON (goes through the existing `trimJSONFences` path), non-zero exit auth error, timeout kill. This is the local analog of the VCR cassettes — no real subscription in CI.
- [ ] **Step 3: Implement `Generate`**: `exec.CommandContext(ctx, "claude", "-p", "--output-format", "json", ...)`, prompt via stdin, parse the result envelope, surface token/cost fields into the usual response metadata.
- [ ] **Step 4: Implement `GenerateToolTurn`** — the tool-loop drafts (e.g. coreUseCases `submit_use_case`) require it on all providers. Approach: pass the turn's tools to headless claude via `--mcp-config` pointing at an ephemeral stdio MCP server that records invocations and feeds them back as the provider's tool-call results. If Step 1 shows the caller-loop contract makes this shape wrong, STOP and surface the alternative (single-shot JSON emulation of tool choice) to the founder before proceeding.
- [ ] **Step 5: Wire provider selection in `hooks.go`; preflight at boot** (`claude --version` on PATH; friendly error naming the install command if absent).
- [ ] **Step 6: Full gates; commit** — `feat(platform/llm): claude-local provider — headless Claude Code on subscription auth`

---

### Task 4: Embed the SPA; single-binary serve

**Files:**
- Create: `cmd/server/spa_embed.go` (`//go:embed` of the built `webApp/dist`, behind a build tag `localdist` so cloud images keep nginx-served SPA unchanged)
- Modify: server router — serve embedded SPA at `/` in local profile; `/api` + `/mcp` unchanged

- [ ] **Step 1: Failing test**: local-profile server returns `index.html` at `/` and the JS bundle with correct content-type.
- [ ] **Step 2: Implement; make the build step** (`npm run build` → `go build -tags localdist`) a `scripts/build-local.sh` producing the one artifact `archistrator`.
- [ ] **Step 3: Check the SPA's API base**: it must call same-origin `/api/v1/...` (the vite-proxy pattern observed in dev suggests it already does; verify in `webApp/src` and fix any absolute-origin assumption).
- [ ] **Step 4: Gates; commit** — `feat(server): embedded SPA, single local binary`

---

### Task 5: `archistrator init` (scaffold-only) + `archistrator mcp` (stdio serve, auto-started by Claude Code)

Serena pattern: Claude Code is the process manager. `init` never starts anything long-running.

**Files:**
- Create: `cmd/archistrator/main.go` (subcommands: `init`, `mcp`)
- Create: `cmd/archistrator/init.go` + test; `cmd/archistrator/mcpserve.go` + test

**Interfaces:**
- `init` produces, in the target directory: git repo (init if absent) with `receive.denyCurrentBranch=updateInstead`; scaffolded `.aiarch/state/` (empty-project shape that passes `cmd/validate`); `.mcp.json` registering `{"mcpServers": {"archistrator": {"command": "archistrator", "args": ["mcp"]}}}` (stdio — no port in the registration); prints: **"Start Claude Code in this directory and say: design my app."**
- `mcp` produces: the full local stack in one process — stdio MCP transport (reusing the existing MCP tool catalog behind the `/mcp` mount; only the transport differs) + the Task-4 HTTP listener (embedded SPA + `/api`) on `127.0.0.1:8877` (`--port`/env override) + embedded Temporal + local-profile composition. Tool responses that reference session/project state include the SPA URL so the driver surfaces it. Clean shutdown on stdin close (Claude Code session end) — SIGTERM the construction subprocesses, drain Temporal.

- [ ] **Step 1: Failing tests**: (a) `init` in an empty temp dir produces the artifacts above; idempotent on re-run (adopts existing repo, never clobbers existing `.aiarch`); (b) `mcp` responds to an MCP `initialize` over stdio AND serves `/` on the HTTP port in the same process.
- [ ] **Step 2: Implement `init` scaffold.** No exec, no daemon.
- [ ] **Step 3: Implement `mcp`**: wire the stdio transport to the same tool registry as the HTTP `/mcp` mount (discover its construction in `cmd/server/mcp_mount.go`; the go-sdk supports both transports over one server instance). Singleton guard: if the HTTP port is already bound by a live archistrator instance for this repo, exit with a clear one-session-per-project message (v1; no proxy cleverness).
- [ ] **Step 4: Preflight checks inside `mcp` startup with actionable messages surfaced as MCP server instructions**: `git` present, `claude` present, `claude` authenticated (cheapest probe available — discover one; worst case run a 1-token `claude -p "ok"` behind `--skip-auth-check`).
- [ ] **Step 5: Gates; commit** — `feat(cli): archistrator init + stdio mcp serve — Claude Code auto-starts the local stack`

---

### Task 6: Local construction executor (headless claude replaces GH Actions dispatch)

The local profile's construction pipeline is currently the GitHub-creds-gated real rail (`hooks.go` — orthogonal to the projectstate substrate). Local mode needs a third arm: same lifecycle, different executor.

**Files:**
- Create: `internal/resourceaccess/constructionpipeline/localexec.go` (+ test) — implements the same contract as the GH-Actions-backed access
- Modify: `cmd/server/hooks.go` — Finalize arm: local profile without GitHub creds → localexec (creds present keeps the existing behavior)

**Interfaces:**
- Consumes: the contract in `constructionpipeline` (same `DispatchInputs` — activity/component_id — as the aiarch-construct.yml rail).
- Produces: dispatch = spawn headless `claude` in the project repo on the activity branch, with `--mcp-config` attaching `cmd/aiarch-state-mcp` (construct verbs, same rig the Actions agent uses) and the same prompt contract the workflow passes today (read it from `aiarch-construct.yml` / the dispatch site); "runs" tracked as local process records (in-memory + state commits), so status/poll/intervention surfaces keep working; cancel = SIGTERM the subprocess.

- [ ] **Step 1: Read `constructionpipelineaccess.go`'s contract + `constructactivity.go`'s poll loop first**; enumerate every method the manager calls (dispatch, resolve, status, cancel, logs) — the local arm must satisfy all of them, honestly (no fake "success" states).
- [ ] **Step 2: Failing systemtest**: UC3 slice — begin construction on a seeded local project, pump, assert the fake-shim `claude` gets invoked with the state-mcp attached and the activity transitions phase on completion.
- [ ] **Step 3: Implement; map subprocess exit codes into the run-status vocabulary the poll loop expects.**
- [ ] **Step 4: One REAL end-to-end dogfood run** (founder present, real subscription): `init` a scratch project, design a toy app through UC1/UC2, construct one activity. This is the funnel's acceptance test.
- [ ] **Step 5: Gates; commit** — `feat(construction): local executor — headless claude with state-mcp rig`

---

### Task 7: Review-policy floor for local mode (vibes default)

`project.json` already carries a top-level `reviewPolicy` key — extend, don't invent.

**Files:**
- Discover then modify: the reader of `reviewPolicy` (`grep -rn "reviewPolicy" server/internal --include='*.go' -l`) + the projectstate model if the shape grows
- Modify: `cmd/archistrator/init.go` — scaffold writes `"reviewPolicy": {"preset": "vibes"}`

**Interfaces:**
- Produces: three presets — `vibes` (auto-approve all drafts/steps), `checkpoints` (approval at: architecture commit, SDP commit, first construction dispatch), `full` (current behavior, approval every step). Hard floor regardless of preset: construction dispatch of any activity whose contract touches deploy/spend/schema still requires explicit approval — encode as a non-overridable gate list, not a preset property.

- [ ] **Step 1: Read the current reviewPolicy shape and its enforcement points; write the failing test for preset resolution (vibes auto-approves a draft commit; floor gate still blocks a flagged dispatch).**
- [ ] **Step 2: Implement minimal preset resolution at those enforcement points. No policy engine — a switch, not a framework.**
- [ ] **Step 3: Gates; commit** — `feat(review): policy presets vibes/checkpoints/full with non-overridable risk floor`

---

## Explicitly out of scope (this plan)

Claude Desktop / MCP Apps surface (blocked upstream: ext-apps#671; pilot continues separately) · codex/copilot engines · npx/brew packaging polish (go build script only) · hosted-migration command (`archistrator deploy`) · billing/usage anything · multi-project local workspaces.

## Sequencing note

Tasks 2→5 are the design-only funnel and independently shippable (matches the original UC1+UC2 wedge). Task 6 is the founder-ratified construction extension; Task 7 rides last because its enforcement points are exercised by 5 and 6. If Task 3 Step 4 (tool-turn) stalls, ship 2–5 with typed-generate-only drafts and surface the limitation.

The Task 2 composegen fix (profile-gated shared Postgres pool, archistrator-platform branch `composegen-profile-gated-pool`) was developed workspace-ACTIVE against the local platform checkout and the app repo's regenerated `main.gen.go` was verified to compile clean under `GOWORK=off` against the still-PINNED `framework-go-app-generator v0.8.0` — but the platform release (this patch + any llm-provider changes) + `server/go.mod` re-pin to the released version is a founder-gated finishing step before this branch merges.

**HARD MERGE BLOCKER (founder-gated):** release framework-go-app-generator (composegen profile-gated pool, commit e3ce3da0) + any llm-provider additions, then re-pin server/go.mod — until then any regen without the local platform checkout silently reverts the Postgres gating.
