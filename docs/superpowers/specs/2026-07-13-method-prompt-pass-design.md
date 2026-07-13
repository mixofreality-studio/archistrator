# Method Prompt Pass + Shared `.claude` Distribution — Design

**Date:** 2026-07-13
**Status:** Approved by founder (brainstorming session); pending spec review
**Owners:** founder + Claude

## Problem

Apps built with archistrator (e.g. gtdapp) receive almost no Method guidance: the
aiarch-state MCP server is their only rail. Meanwhile archistrator's own `.claude`
tree carries 10 role agents, 35 commands, and 27 Method skills that never leave
this repo. Two symptoms:

1. **The design rail (Phases 1–2) dispatches raw prose prompts** composed from Go
   string constants (`systemdesign/coauthorartifact.go`,
   `projectdesign/coauthorphase2artifact.go`) — doctrine is forked between Go
   strings and `.claude` skills, and app repos never see any of it.
2. **The construction rail (Phase 3) already uses thin slash commands**
   (`/service-construction <component> <activity>`), but `aiarch-construct.yml`
   and the `.claude` assets it resolves against exist only in archistrator's own
   repo — apps cannot run construction in their own repo.

gtdapp's seated files are also stale generations (pre-MCP `aiarch-design.yml`,
`framework-go v0.4.0` vs current v0.5.2), because the managed scaffold's content
set is small and its pins drift.

## Ratified decisions

| Decision | Choice |
| --- | --- |
| Asset delivery | **Managed scaffold only** — no GitHub template repo. `SyncManagedScaffold` converges every app repo. |
| Design rail | **Full migration** to thin slash commands; all doctrine moves out of Go into `.claude` skills/commands. |
| Tool scoping | **Frontmatter only** — per-role `tools:` lists in `agents/*.md`; server job-mode scoping stays as the hard boundary. |
| go.mod seed | `framework-go` require + generator CLIs as Go 1.25 `tool` directives. |
| Construction venue | **Seat `aiarch-construct.yml` into app repos and dispatch construction to the project's own repo** — ratifies the "gh mode" slice of the multi-venue proposal. |
| Asset home | **New platform module** (founder direction): assets live in archistrator-platform and are embedded + materialized into consuming repos. |
| Design command shape | **Per-kind commands** (one draft command per artifact kind; per-kind critique commands), plus one shared `design-answer` (kind-agnostic Q&A — per-kind copies would be 17 identical files). |

## 1. New platform module: `archistrator-platform/method-assets`

A Go module that owns **every file seated into an app repo**, embedded via
`go:embed`:

- `claude/agents/*.md` — the 10 role agents, with `tools:` scoping (§4)
- `claude/commands/*.md` — 30 construction commands + 5 orchestrators
  (`system-design`, `project-design`, `sdp-review`, `add-use-case`,
  `implement-project`) + the new per-kind design commands (§3)
- `claude/skills/the-method*/**` — the 27 Method skills (`grillme` stays
  archistrator-local; the dead structurizr hook/scripts are deleted, §7)
- `workflows/aiarch-design.yml.tmpl` and `workflows/aiarch-construct.yml.tmpl`
- `go.mod.tmpl`, `aiarch_method_test.go.tmpl`
- ~~`project.json.tmpl`~~ DROPPED (2026-07-13 implementation finding):
  `projectStateAccess.CreateProject` already seeds the repo's `project.json`
  at Version 1 (projectstateaccess.go:516, git-as-DB) — the scaffold must not
  double-write a server-owned path. The "empty project.json id" annoyance was
  archistrator-local, not an app-repo gap.

**API:**

- `methodassets.ScaffoldFiles(data ScaffoldData) map[string][]byte` — the full
  seat set; archistrator's server calls this from `ManagedScaffoldFiles`.
- `cmd/method-assets install --dest <repo>` — materializer for the `.claude`
  tree. **Manifest-tracked**: it records the files it owns (a manifest file
  under `.claude/`) and only creates/updates/deletes those, so repo-local
  extras (`grillme`, `settings.local.json`, local hooks) survive re-runs.

**Consumers:**

1. archistrator server — imports the module; managed scaffold seats its output.
2. archistrator's own repo — root `.claude` becomes a **materialized copy**
   (dogfooding), with a CI drift gate like the other `gen()` surfaces.
3. app repos — receive the same files via scaffold seat + sync.

**Accepted trade-off:** prompt iteration costs a platform release + pin bump
(known scaffold-pin-staleness friction). Mitigations: assets-only module so
releases are trivial; `go.work` override for local iteration.

## 2. Design rail migrates to thin slash commands

`aiarch-design.yml` stops receiving a composed `design_prompt` blob. Mirroring
construction:

- The Go server computes only a **command name** via a new
  `DesignCommandFor(kind, mode)` (sibling of construction's `CommandFor`).
- The workflow prompt becomes `/${{ inputs.command }}` (plus a small args
  input where needed). `design_prompt` is removed from the dispatch contract.
- All doctrine currently inlined in Go — `draftTask` bodies (Phase 1 and
  Phase 2), `activityDiagramGuide`, `operatingModelDeploymentConstraint`,
  `operatingModelInfrastructureConstraint`, `methodTeamRosterDoctrine`,
  `activityInventoryDoctrine`, `solutionClassRatesDoctrine`, the
  architect/PM headers, and the critique/answer prompt bodies — **merges into
  the corresponding `the-method-*` skills and the new commands, then is
  deleted from Go.**
- Context the Go prompts used to inline (feedback, review ledger, priors,
  research corpus) is already reachable via existing MCP reads
  (`getReviewThread`, `getCommittedSlot`, `getDraftSlot`,
  `listResearchSources`/`getResearchSource`); the commands instruct those
  reads explicitly. Nothing is lost by going thin.
- Ambient env (`AIARCH_ARTIFACT_KIND`, `AIARCH_JOB_MODE`, etc.) is unchanged
  and remains the server-enforced scope for `putDraftModel`.

### 2a. Orchestration model: how architect and PM communicate

Two roles never share a Claude session. The server's durable design-session
workflow drives a draft → critique loop as **separate GH Actions dispatches**:

1. `job_mode=draft` → CI run as **system-architect**: reads basis slots +
   review ledger, writes the model via `putDraftModel`, commits via
   `publishDraft`.
2. Server observes completion, dispatches `job_mode=critique` → fresh CI run
   as **product-manager**: reads the draft via `getDraftSlot`, records
   approve/revise via `setCritiqueVerdict`. It can never rewrite the model.
3. On revise, the server dispatches another draft job; the architect reads the
   PM feedback from the ledger and amends. Loop until approve.

The communication medium is **project.json on the session branch plus the
review ledger** — git-as-DB, not conversation. Rationale for keeping this:

- **By the book** — the PM ratifies independently; a cold-start critique from
  committed artifacts avoids inheriting the architect's anchoring.
- **Durability** — each role turn is an idempotent, resumable pipeline step
  with its own `idempotency_token`.
- **Auditability** — every turn is a discrete Actions run; every exchange
  lands in the ledger.
- **Loop control lives in deterministic Temporal code** (§2b), immune to
  prompt regressions.

Within a single dispatch, a command MAY spawn subagents of its own role's team
(e.g. review routing during construction). The invariant: **anything that
writes a different role's slot crosses a dispatch boundary** — the server
schedules it, never a subagent call.

### 2b. Convergence: bounding the draft↔critique loop

Hard bound (existing, unchanged): `maxRedraftAttempts = 5`
(`coauthorartifact.go:23`). When redraftCount hits the cap the workflow stops
dispatching and **stages the artifact for human review** carrying the latest
unresolved critique note. The ledger's `reviewRound` preserves the full
argument history for that review. Terminal state of a fight is escalation to
the founder, never more CI runs.

Soft bound (new, in the migrated prompts): the current `pmCritiquePrompt` has
no anti-thrash doctrine. The critique commands/skills gain:

- **Verdict discipline** — "revise" requires new, actionable comments tied to
  specific artifact content. A critique that would repeat an
  already-responded-to comment must instead accept the response or
  approve-with-noted-reservation. No relitigating resolved threads.
- **Round awareness** — the PM reads the ledger first and critiques the
  *delta* since its last verdict, not the artifact from scratch.
- **Severity honesty** — only defects against mission/requirements justify
  "revise"; taste-level preferences are recorded as comments on an approve.
- **Architect mirror** — each redraft must respond to every open comment via
  `respondToReviewComment` (accept or rebut) before `publishDraft`; silent
  non-response fuels repeat critiques.

## 3. Per-kind design commands (~22 new command files)

**Draft** (system-architect agent), one per artifact kind:
`mission-draft`, `glossary-draft`, `scrubbed-requirements-draft`,
`volatilities-draft`, `core-use-cases-draft`, `system-draft`,
`operational-concepts-draft`, `standard-check-draft`,
`planning-assumptions-draft`, `activity-list-draft`, `network-draft`,
`normal-solution-draft`, `subcritical-solution-draft`,
`decompressed-solution-draft`, `compressed-solution-draft`,
`risk-model-draft`, `sdp-review-draft`.

**Critique** (product-manager agent) — the Phase-1 PM-critique kinds only:
`mission-critique`, `glossary-critique`, `scrubbed-requirements-critique`,
`core-use-cases-critique`.

**Answer** — kind-agnostic Q&A over the review ledger (deviation from strict
per-kind, ratified in session), split by addressee because the server's
AskQuestions addresses questions to "pm" or "architect" and the roles carry
different tool scopes: `design-answer` (system-architect) and
`design-answer-pm` (product-manager).

Each command is thin: names the agent, invokes its `the-method-*` skill, and
walks read-basis → draft via `putDraftModel` → `respondToReviewComment` →
`publishDraft`. Kind-specific doctrine lives in the skill, not the command.

A wire test asserts every dispatchable (kind × mode) pair maps via
`DesignCommandFor` to a command file present in the embedded assets.

## 4. Role-scoped tools via agent frontmatter

Each agent gains a `tools:` list: role-appropriate built-ins plus only its
sanctioned `mcp__aiarch-state__*` verbs, by the book. All read verbs stay open
to all roles; scoping is about write authority.

| Agent | State writes | Notes |
| --- | --- | --- |
| system-architect | `putDraftModel`, `publishDraft`, `respondToReviewComment` | + estimation/review engine computes |
| product-manager | `setCritiqueVerdict`, `respondToReviewComment`, `publishDraft` | **no** `putDraftModel` — PM never rewrites the model |
| project-manager | `putDraftModel`, `publishDraft` | + earned-value/network computes; owns `.network` |
| senior-developer | `recordServiceContract`, `publishDraft`, `respondToReviewComment` | contract designer |
| junior-developer | `recordPhaseArtifact`, `publishDraft`, `respondToReviewComment` | never designs contracts |
| ui-designer | `recordPhaseArtifact`, `publishDraft`, `respondToReviewComment` | `.phaseArtifacts.uiDesign` |
| ux-reviewer | `respondToReviewComment` only | read-only built-ins: no Edit/Write — reviews, never amends |
| test-engineer | `recordTestingState`, `publishDraft`, `respondToReviewComment` | plan/harness/perf fields |
| software-tester | `recordTestingState`, `publishDraft`, `respondToReviewComment` | testRun/defect fields |
| qa-engineer | `recordTestingState`, `publishDraft`, `respondToReviewComment` | qualityGate/audit fields |

**Known coarseness (accepted):** frontmatter cannot split one verb's fields
(`recordTestingState` across the three testing roles) or `putDraftModel` per
kind. Those stay governed by server-side job-mode ambient scoping plus agent
prose. The frontmatter layer is documentation + defense-in-depth, not the
security boundary.

## 5. Managed scaffold expansion + construction venue switch

`ManagedScaffoldFiles` delegates to `methodassets.ScaffoldFiles`. Seated set
grows from 4 files to:

- existing: `aiarch-design.yml`, `go.mod` (§6), `aiarch_method_test.go`,
  `internal/.gitkeep`
- new: `aiarch-construct.yml`, the full `.claude` tree, seed
  `.aiarch/state/project.json`

`scaffoldRootPaths` allowlist grows accordingly (`.claude/**`,
`.aiarch/state/project.json`). `SyncManagedScaffold` converges everything —
retroactively healing gtdapp's stale workflow and missing `.claude`.

Construction dispatch: the construction manager sets `TargetRepo` /
`WorkflowFile` to the project's own repo (the constructionpipeline RA already
resolves per call). This ratifies the "gh mode" slice of the multi-venue
proposal; local and platform-funded modes remain future work.

## 6. go.mod template

- `require github.com/mixofreality-studio/archistrator-platform/framework-go v0.5.2`
- Go 1.25 `tool` directives for the generator CLIs that actually ship `cmd/`
  mains today: `framework-go-http-generator/cmd/httpgen v0.3.0`,
  `framework-go-mcp-generator/cmd/mcpgen v0.2.0`. EARMARK (2026-07-13):
  app-generator and projectmodel expose only library packages — their tool
  directives land when the platform ships CLI wrappers; a seeded directive
  today would break `go mod tidy` in every new app.
- Infra modules (postgres/temporal/keycloak/otel/github/llm) still arrive via
  generated code when needed.

## 7. Cruft removal (archistrator repo)

- Delete `.claude/hooks/validate-structurizr.sh` (keyed to the removed
  `methodpoc/designs/**` layout; silently no-ops today), and
  `.claude/structurizr-validate`, `.claude/structurizr-serve` (same dead
  layout). Remove the corresponding hook entry from `settings.local.json`.
- Normalize the lone `methodpoc/` phrasing in `commands/implement-project.md`
  and the dead contract-path phrasing in `agents/senior-developer.md`.
- `grillme` is excluded from the platform module (archistrator-dev tool) and
  survives materialization via the manifest.

## 8. Verification & rollout

**Tests:**

- Wire test: every (kind × mode) dispatchable maps to an embedded command file.
- Module manifest test: embedded tree matches the manifest; no orphans.
- Frontmatter validity test: every agent's `tools:` entries name real
  built-ins or catalog verbs.
- Archistrator CI drift gate: materialized root `.claude` == module output.
- Existing systemdesign/projectdesign manager tests updated for thin dispatch
  (command-name assertions replace prompt-substring assertions).

**Release order:**

1. Platform: publish `method-assets v0.1.0` (assets lifted from archistrator's
   `.claude` + doctrine merged from Go constants).
2. Archistrator: import module; refactor design managers to thin dispatch;
   delete Go doctrine constants; expand scaffold + allowlist; switch
   construction venue; materialize root `.claude` + drift gate; cruft removal.
3. Re-seat/sync gtdapp (heals stale workflow, missing `.claude`, go.mod pin).
4. Follow-up (separate work, already earmarked): re-run the gtdapp Phase-2
   amendment with the improved prompts; drain in-flight workflows before
   deploying server changes.

## 9. Honest role-driven loading states

**Requirement (founder):** loading screens must say who is doing what —
"Architect is crafting the vision and mission statement", then "Product
manager is reviewing the vision and mission statement" — driven by the
workflow's REAL state, never simulated.

**What exists:** the design-session workflow exposes a Temporal-queried
`SessionStateView.Stage` (drafting / awaitingReview / redrafting / committed /
withdrawn / refused / draftFailed; Phase 2 adds assemblingSdp) that the SPA
polls (`useSessionState` → `GeneratingScene`). The stage is too coarse to
distinguish the architect's draft dispatch from the PM's critique dispatch —
both report `drafting`/`redrafting`. QA F15 already removed a fake wall-clock
phase ticker from `GeneratingScene`; that honesty bar is binding here.

**Change — server:**

- Extend the session-state contract (schema-first: contract change →
  regenerated Go + TS) with sub-step fields on `SessionStateView`:
  - `activeRole`: `none | architect | productManager`
  - `activeStep`: `none | drafting | critiquing | revising`
  - `round`: the current review round (int)
- The workflow sets these at **dispatch boundaries only** — immediately before
  submitting a pipeline job (it knows the job_mode and therefore the role) and
  clears/advances them when it observes completion. No timers, no inference.
  Transitions: draft dispatch → (architect, drafting); critique dispatch →
  (productManager, critiquing); redraft dispatch → (architect, revising,
  round N); awaitingReview / terminal stages → (none, none).
- Mirror in the Phase-2 projectdesign session workflow (no critique step
  there; architect-only labels, plus assemblingSdp).

**Change — webApp:**

- `GeneratingScene` renders the role-driven status line from the polled
  sub-step: `RoleAvatar` (exists) + verb + per-kind display phrase, e.g.
  "Architect is crafting the vision and mission statement", "Product manager
  is reviewing the vision and mission statement", "Architect is revising the
  vision and mission statement (round 2)". Kind → display-phrase map lives
  with the existing artifact-name mapping in the SPA.
- The indeterminate pulse and the tracked-CI-job affordance stay. When
  sub-step fields are absent/`none` (e.g. old server), the scene falls back to
  today's honest "DRAFTING…" — never a fabricated role line.
- `awaitingReview` is not a loading state (it is the founder's turn) and is
  unaffected.

**Construction rail:** construction dispatch inputs already carry `role` +
`command` + `component_id`; the supervision surface applies the same labeling
scheme from observed pipeline state ("Junior developer is constructing
NotificationEngine") — same honesty rule: labels derive from the dispatched
run the server actually observes, no simulated progress.

**Tests:** manager unit tests assert the sub-step transition sequence across a
draft → critique → revise → approve session (including draftFailed clearing
to `none`); a webApp test asserts the scene copy for each (role, step) pair
and the fallback when sub-step is absent.

## Out of scope

- Local and platform-funded construction venues (multi-venue rev 2 remainder).
- Server-enforced per-role MCP filtering (`AIARCH_ROLE`) — explicitly declined
  in favor of frontmatter-only for this pass.
- Per-field verb splitting (`recordTestingState`) — would require new composed
  verbs; earmarked, not planned.
