# Full Manual QA Pass — gtdapp e2e + per-screen expert reviews Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove archistrator prod-ready by resetting gtdapp and driving a complete System Design → Project Design → Construction journey through the real UI (browser automation), with pm/ux-reviewer/system-architect subagent reviews of every screen against The Method and the 7 qualities, fixing every defect found at its root.

**Architecture:** This is a QA *campaign*, not a feature build — defects are unknown until found, so tasks below are phases with hard exit criteria plus a fixed per-finding protocol (reproduce → root-cause → fix via subagent → blackbox regression → re-verify live). The rail is the product: gtdapp content moves ONLY through UI/MCP; archistrator code moves through subagents on disk.

**Tech Stack:** Go server (`GOWORK=off`, cloud profile, :8888) · React SPA (vite :5199) · claude-in-chrome browser automation · GitHub rail (claude-code-action runs in `mixofreality-studio/gtdapp`) · Temporal (:7233) · Postgres (:5432) · `.claude/agents` subagents (pm, ux-reviewer, system-architect, etc.)

## Global Constraints

- gtdapp is fully built with archistrator — NEVER edit gtdapp code/state by hand; UI or MCP interfaces only (research corpus upload included).
- Archistrator architecture changes (add/remove use case etc.) require system-architect to update archistrator's own dogfood `project.json` first, then implement.
- Every validation-catchable defect ⇒ add the validator, then align BOTH gtdapp (via rail amendment) and archistrator dogfood state with it.
- Every error state found ⇒ graceful handling in UI/server AND root-cause fix.
- Fix right over tech debt; never weaken gates.
- Server boot: `GOWORK=off` build; source `server/.env` then override `ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=false`, unset DRYRUN/repo-URL, `export ARCHISTRATOR_GITHUB_APP_SLUG=archistrator-bot` (empty slug now fails fast). Run via `nohup <bin> & disown`, never harness-tracked background.
- Research input: `gtd-qa2/gtdinfo.txt` in session scratchpad (670,992 bytes, GTD book full text) — re-provide during onboarding.
- Findings ledger: session scratchpad `gtd-qa2/qa-findings-2.md`, IDs `F-QA2-n`. Prior open findings carried in: F-GTD-6 (GH-run links on generating/failed views), F-GTD-11 (no Amend on clean committed artifact), F-GTD-14 (experience opens at step 1, stepper first-click tooltip flake), putDraftModel amendment field-drop disclosure, VolatilityMap/OpConcepts MCP-iframe hook bugs.
- Review matrix per screen: pm + ux-reviewer + system-architect subagents, judging (a) Method fidelity vs `research/rightingsoftware`, (b) accessibility, reliability, performance, maintainability, usability, observability, testability. Reviews are dispatched in ONE parallel batch per screen; findings deduped into the ledger before any fix work starts.

---

### Task 1: Rig boot (Phase 0)

**Files:** none (ops)

- [ ] Kill stale `.mcp-pilot/archistrator-server` (:8888) by exact PID; leave vite :5199 (live-reloads from main checkout).
- [ ] `GOWORK=off go build -o <scratch>/gtd-qa2/server-bin ./server/cmd/server` (repo main @e7aa826 or later).
- [ ] Write `<scratch>/gtd-qa2/boot-server.sh` per Global Constraints; `nohup … & disown`.
- [ ] Verify: boot log shows cloud profile RAs (projectStateAccess GitHub, constructionPipelineAccess GitHubActions), `list-projects` includes gtdapp, SPA loads at `http://localhost:5199`.

### Task 2: gtdapp reset + re-onboarding (Phase 1)

- [ ] Terminate all `gtdapp:*` Temporal workflows (design kinds + construction + pump) — `temporal workflow list`/terminate.
- [ ] Reset `mixofreality-studio/gtdapp` to the pre-onboarding shape CreateProject expects (recon-verified before executing; destructive step — repo is a QA sandbox by charter, its description says "built by archistrator (QA pass)").
- [ ] Clear residual server-side state (Postgres rows for the project, if recon finds any).
- [ ] In the SPA: create the project fresh; initial prompt = GTD-universe app (capture/clarify/organize/reflect/engage, horizons of focus, tickler, natural planning; webapp + HTTP API + MCP tools with MCP-Apps UI; to be Operated by archistrator later). Upload gtdinfo.txt as research source via the UI.
- [ ] Exit: project visible, phase = System Design, research corpus listed, zero stale/failed sessions.

### Task 3: System Design e2e + reviews (Phase 2)

For EACH of the 8 steps (mission, glossary, scrubbedRequirements, volatilities, coreUseCases, systemDesign, operationalConcepts, standardCheck):

- [ ] Drive draft from the UI; monitor the GH Actions run (background monitor, not foreground polling); verify Generating → AwaitingReview transitions render honestly (role lines, no fake progress).
- [ ] QA the review screen hands-on: comment rail, artifact rendering, diagrams (keyboard nav), approve/send-back/stale flows.
- [ ] Dispatch pm + ux-reviewer + system-architect review batch on the screen + committed artifact content (Method fidelity + 7 qualities).
- [ ] Triage findings → ledger; fix per protocol; content defects in gtdapp → send-back/amend via rail only.
- [ ] Exit: all 8 slots committed, zero stale, standardCheck pass/waived only, ledger triaged to closed/earmarked.

### Task 4: Project Design e2e + reviews (Phase 3)

- [ ] Same loop across planningAssumptions → activityList → network → normal → decompressed → subcritical → compressed → riskModel → sdpReview.
- [ ] Regression-watch the prior failure modes: rateCard key drift (PA-RATECARD-*), enum zeros (PA-INFRA-KIND, PA-TERMS-REGIME), invented human day-rates, decompressed staff cuts, amendment field drops.
- [ ] Exit: SDP committed with a bound option; project standard check pass/waived; ledger triaged.

### Task 5: Construction e2e (Phase 4)

- [ ] Begin construction from the app; ExecuteNextActivity; verify gh-mode venue (PRs in gtdapp repo), tracker screens, activity-type UIs (service contract / test contract / ui contract legibility is an explicit review dimension).
- [ ] Run per-activity review routing; earned-value tracking sanity.
- [ ] Exit: enough of the network constructed that gtdapp builds and runs locally (server boots, at least core use-case slice works).

### Task 6: Blackbox coverage (Phase 5)

- [ ] `systemtests` (REST + MCP transports) green; `uitests` (Playwright rig) green.
- [ ] Every code fix from Tasks 3–5 has a blackbox regression (system test or ui test); add missing ones.

### Task 7: Wrap-up (Phase 6)

- [ ] Full gate sweep: `GOWORK=off go build ./... && go test ./...` all modules, lint gate, `webApp npm run check` (known prettier drift excepted), methodcheck/validate clean on gtdapp AND archistrator dogfood state.
- [ ] Findings ledger finalized with status per finding; memory files updated.

## Self-Review

Spec coverage: user rules 1–5 map to Global Constraints; goals 1–6 map to Tasks 3–7 exit criteria; reset+research mapped to Task 2; playwright-MCP intent satisfied by claude-in-chrome (the browser-automation MCP available in this session). Placeholder scan: tasks are gated by exit criteria rather than pre-known diffs — inherent to QA; per-finding protocol is fully specified. Type consistency: n/a (ops plan).
