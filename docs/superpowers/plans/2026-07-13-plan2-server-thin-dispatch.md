# Plan 2: Server Thin Dispatch + Scaffold Expansion + Venue Switch

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The archistrator server consumes `method-assets` for everything it seats, dispatches design jobs as slash-command names (deleting all Go prompt prose), dispatches construction into the project's own repo, and archistrator's root `.claude` becomes a drift-gated materialized copy.

**Architecture:** Two phases. Phase A (platform repo): normalize construction assets to layout-neutral language + add a seat-manifest API → release `method-assets/v0.1.1`. Phase B (archistrator repo, branch `method-prompt-pass`): `DesignCommandFor` in projectstate; thin dispatch in both design managers + prose deletion; `ManagedScaffoldFiles` → `methodassets.ScaffoldFiles` with allowlist expansion and a manifest-version sync fast-path; construction repo resolver + per-call TargetRepo; root materialization + cruft removal + CI drift gate.

**Tech Stack:** Go 1.25, methodassets v0.1.1, existing appgen/hooks composition pipeline (GOWORK=off).

**Spec:** `docs/superpowers/specs/2026-07-13-method-prompt-pass-design.md` (§2, §5, §6 server-side, §7) + Plan-1 earmarks (layout normalization, seat manifest).

## Global Constraints

- Archistrator work on branch `method-prompt-pass` off local `main` (main is ahead of origin; do NOT push archistrator). Platform work directly builds on main (already green @b54e5f89); release v0.1.1 push IS authorized (founder mandate covers completing the release chain).
- Server builds/tests: `cd server && GOWORK=off go build ./... / go vet ./... / make test-short`; lint `golangci-lint run ./...` (v2 config); plus the `gen-*-check` drift gates in server-checks.yml.
- Dispatch wire contract (module template v0.1.0 already defines it): design inputs = `idempotency_token`, `command`, `artifact_kind`, `target_branch`, `prior_state_ref`, `job_mode`. `design_prompt` ceases to exist.
- Command names (must match embedded assets exactly): draft = `<kind-kebab>-draft` for 16 kinds (SdpReview is NOT dispatchable — assembled server-side); critique = `mission-critique`, `glossary-critique`, `scrubbed-requirements-critique`, `core-use-cases-critique`; answer = `design-answer` (architect) / `design-answer-pm` (pm).
- ArtifactKind wire strings for `artifact_kind` stay PascalCase via `ArtifactKind.String()` (projectstateaccess.go:2486). Kebab mapping is DesignCommandFor's job.
- Never weaken gates (.golangci.yml, methodcheck, TestFileLayout). All new activities via the generated-activity pipeline — zero handwritten Temporal activities (layer-file-layout standard).
- Deploy + gtdapp re-seat + gtdapp Phase-2 amendment re-run are FOUNDER steps after merge (drain in-flight workflows first). Record them; do not attempt.

---

## Phase A — platform repo (`archistrator-platform`, on main)

### Task A1: Seat-manifest API in methodassets

**Files:**
- Modify: `method-assets/scaffold.go`, `method-assets/materialize.go` (share the manifest-building helper)
- Test: `method-assets/scaffold_test.go`

**Interfaces:**
- Produces: `ScaffoldFiles` output gains key `.claude/.method-assets-manifest.json` — same JSON shape the materializer writes (`{version, files[] sorted}` where files = the `.claude/**` keys, manifest not self-listed). New exported `func Version() string` (wraps the existing build-info helper) so the server can fingerprint the seated set. `ScaffoldData` unchanged.
- Why: the server's `SyncManagedScaffold` runs before EVERY design dispatch; a single manifest read lets it fast-path "already at version X" instead of ~100 per-file compares (Task B4).

- [ ] Step 1: failing test — `TestScaffoldFilesIncludesManifest`: unmarshal the manifest from `ScaffoldFiles(testData)`, assert `version == Version()`, `files` == sorted `.claude/**` keys of the output (excluding the manifest itself), and no other new keys appeared.
- [ ] Step 2: implement — extract the manifest-building code from `materialize.go` into a shared unexported helper `buildManifest(files map[string][]byte) manifest`; `ScaffoldFiles` marshals it under the manifest path; export `Version()` delegating to the existing `readBuildVersion`.
- [ ] Step 3: `GOWORK=off go test ./... && golangci-lint run ./...` → clean. Commit: `feat(method-assets): ScaffoldFiles emits the seat manifest + exported Version()`.

### Task A2: Layout-neutral construction assets (the Plan-1 earmark)

**Files:**
- Modify: `method-assets/assets/claude/commands/*.md` (the 30 construction commands + `implement-project.md`), `assets/claude/agents/junior-developer.md`, `senior-developer.md`, and every skill under `assets/claude/skills/` carrying `server/internal/...` or `research/rightingsoftware` references
- Test: `method-assets/layoutneutral_test.go`

**Interfaces:**
- Produces: assets that work in BOTH archistrator's multi-dir repo and a generated app's module-root repo, by deriving layout from committed state instead of hardcoding.

- [ ] Step 1: failing test — scan every embedded `.claude` file; assert zero occurrences of `server/internal/` and zero relative `research/` book-file links (allow the phrase "Righting Software" and chapter citations — ban only file paths).
- [ ] Step 2: rewrite the references. Binding rules:
  - Path derivation: replace "Implement under `server/internal/<layer>/<pkg>/`"-style instructions with "Implement in the package the contract names: the component's `goPackage` in `.serviceContracts["<component_id>"]` is authoritative; place files per the repo's existing layout for that package (create it at the module path the contract implies if new)." Same treatment for `working-directory server` → "the Go module directory containing the target package (the repo root in generated apps)".
  - Verification commands: keep `gofmt`/`go build`/`go vet`/`go test` but scope them as "from the target package's module root, GOWORK=off".
  - Book links: `../../../research/rightingsoftware/...` file links → plain citations ("Löwy ch. N §M") — the doctrine text already carries the content.
  - Do NOT touch design-rail commands/skills' normative doctrine; this task is layout/citation language only. Any sentence you change must preserve its normative force — this is the same verbatim-in-meaning bar as the doctrine merges.
- [ ] Step 3: `GOWORK=off go test ./... && golangci-lint run ./...` clean. Commit: `feat(method-assets): layout-neutral construction assets (contract-derived paths, citation-only book refs)`.

### Task A3: Release v0.1.1

- [ ] Step 1: full local gate (`gofmt -l . ; go vet ./... ; go test ./... ; golangci-lint run ./...`) in method-assets.
- [ ] Step 2: push main, tag `method-assets/v0.1.1`, push tag. Watch platform-checks → green. Verify `go list -m .../method-assets@v0.1.1` resolves.

---

## Phase B — archistrator repo (branch `method-prompt-pass`)

### Task B1: `DesignCommandFor` + cross-repo wire test

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/projectstateaccess.go` (next to `CommandFor` at :6962)
- Modify: `server/go.mod` (+ `github.com/mixofreality-studio/archistrator-platform/method-assets v0.1.1`)
- Test: `server/internal/resourceaccess/projectstate/designcommand_test.go`

**Interfaces:**
- Produces:

```go
type DesignJobMode string
const (
    DesignJobModeDraft    DesignJobMode = "draft"
    DesignJobModeCritique DesignJobMode = "critique"
    DesignJobModeAnswer   DesignJobMode = "answer"
)
// DesignCommandFor maps a design job to its .claude command slug.
// addressee is consulted ONLY for answer mode ("pm" | "architect").
// Returns "" for undispatchable combinations (SdpReview any mode;
// critique for kinds without PM critique) — callers treat "" as a
// contract-misuse error.
func DesignCommandFor(k ArtifactKind, mode DesignJobMode, addressee string) string
```

Kind→kebab helper unexported (`designKindSlug`), mirroring `profileSlug`. Critique kinds = exactly the set `kindHasPMCritique` (coauthorartifact.go:2709) allows — read it and keep the two in lockstep (add a comment on each pointing at the other).

- [ ] Step 1: failing table test — all 16 draft slugs verbatim; 4 critique slugs; answer × both addressees; `""` for SdpReview-draft, SdpReview-critique, Volatilities-critique (non-critique kind), unknown addressee.
- [ ] Step 2: cross-repo wire test in the same file:

```go
func TestDesignCommandsExistInMethodAssets(t *testing.T) {
    files, err := methodassets.ClaudeFiles()
    // for every kind × mode × addressee combination with non-"" slug:
    // assert files[".claude/commands/"+slug+".md"] exists
}
```

- [ ] Step 3: implement; `GOWORK=off go build ./... && go test ./internal/resourceaccess/projectstate/ -run 'DesignCommand'` green. Commit.

### Task B2: Thin dispatch — systemdesign manager

**Files:**
- Modify: `server/internal/manager/systemdesign/coauthorartifact.go`, `systemdesignmanager.go`
- Test: `server/internal/manager/systemdesign/manager_test.go`

**Interfaces:**
- Consumes: `projectstate.DesignCommandFor`, dispatch constants at systemdesignmanager.go:3002-3025.
- Produces: dispatch input `command` replaces `design_prompt` at both write sites.

- [ ] Step 1: replace `dispatchInputDesignPrompt = "design_prompt"` (:3004) with `dispatchInputCommand = "command"`. In `dispatchDesignJob` (coauthorartifact.go:1779-1785): drop the Prompt line; add `dispatchInputCommand: projectstate.DesignCommandFor(toPSKind(a.ArtifactKind), designModeFor(a.Target), "")` where `designModeFor` maps the existing `dispatchTarget` values. In `dispatchAnswerJob` (systemdesignmanager.go:1765-1771): `dispatchInputCommand: projectstate.DesignCommandFor(toPSKind(kind), projectstate.DesignJobModeAnswer, addressee)`. Empty-slug ⇒ return contract-misuse error before dispatch.
- [ ] Step 2: remove the `Prompt` field from `dispatchDesignJobArgs` (:1756-1768) and both composition sites (:638-649 draft, :706-716 critique — the `architectDraftPrompt`/`pmCritiquePrompt` calls go with it).
- [ ] Step 3: delete the prose-only code (recon-verified delete list, coauthorartifact.go): `architectHeader` :2485, `pmHeader` :2487, `architectDraftPrompt` :2495-2559, `operatingModelDeploymentConstraint` :2572-2585, `pmCritiquePrompt` :2593-2611, `activityDiagramGuide` :2621, `draftTask` :2663-2703, `writePriorsPointer` :2733, `writeResearch` :2751, `writeFeedback` :2766, `writeReviewLedger` :2790. KEEP: `kindHasPMCritique` (:2709 — now also feeds DesignCommandFor's lockstep comment), `activityDefect`, `signalNotes`, `answerPrompt` deletion happens in systemdesignmanager.go (:1796-1814) — delete it too (addressee now rides the command name).
- [ ] Step 4: tests — delete the prompt-content test blocks (~26 assertions; recon names `TestAnswerPrompt_RoleAndIDs` :2236, `Test_OperationalConceptsPrompt_*` :5759, the :5761-6231 block, `pmCritiquePrompt` checks :5907/:5916); re-point the dispatch-shape assertions (e.g. :4517-4520 feedback-carry) at `command`: a redraft still dispatches — assert `inputs["command"] == "mission-draft"` etc. and that feedback now flows via the ledger (the ledger-seeding code is UNTOUCHED — assert it still records). Every deleted assertion must either be re-pointed or be prompt-prose-only; list the disposition per test in the report.
- [ ] Step 5: `GOWORK=off go build ./... && go vet ./... && go test ./internal/manager/systemdesign/...` green. Commit.

### Task B3: Thin dispatch — projectdesign manager

Mirror of B2. **Files:** `coauthorphase2artifact.go`, `projectdesignmanager.go`, `manager_test.go`.

- [ ] Step 1: constants (projectdesignmanager.go:1617-1622 + :1295): replace `dispatchInputDesignPrompt` with `dispatchInputCommand`. Write sites: coauthorphase2artifact.go:1127 (draft — also ADD `dispatchInputJobMode: "draft"` which Phase-2 draft dispatch currently omits; the template's job_mode drives MCP ambient scoping) and projectdesignmanager.go:1517 (answer).
- [ ] Step 2: delete prose-only code (recon list, coauthorphase2artifact.go): `architectHeader` :1820, `architectDraftPrompt` :1829-1904, `operatingModelInfrastructureConstraint` :1905-1924, `writeReviewLedger` :1926-1956, roster/inventory/classRates consts :1958/:1967/:1974, `draftTask` :1977-2004, `writePriorsPointer` :2008, `writeFeedback` :2017; `answerPrompt` in projectdesignmanager.go:1560+. KEEP: `feedbackToLedgerComments`, `seedAmendmentLedger`, `signalNotes`, `sameArtifactModel`. The SdpReview façade rejection (:234) is untouched.
- [ ] Step 3: tests — same disposition discipline as B2 (~14 assertions; `Test_DraftPrompt_*` :3381-3418, `Test_PlanningAssumptionsPrompt_*` :3301, dispatch substrings :1090/:2082/:2144/:2339/:2632/:2817/:3185).
- [ ] Step 4: package green. Commit.

### Task B4: Scaffold delegation + sync fast-path

**Files:**
- Modify: `server/internal/resourceaccess/sourcecontrol/sourcecontrolaccess.go`
- Delete: `server/internal/resourceaccess/sourcecontrol/assets/*.tmpl` (all three)
- Test: existing sourcecontrol tests + new ones

**Interfaces:**
- Consumes: `methodassets.ScaffoldFiles(ScaffoldData)`, `methodassets.Version()`.
- Produces: `ManagedScaffoldFiles(repo, appSlug)` (:1140) returns the FULL module set mapped to `[]ManagedFile`; `scaffoldRootPaths`/`isManagedFilePath` (:58/:67) additionally allow the `.claude/` prefix and `.aiarch/state/` is NOT added (CreateProject owns project.json). `SyncManagedScaffold` (:1214) gains the fast-path.

- [ ] Step 1: `ScaffoldData` mapping — `ModulePath` = `github.com/<owner>/<repo>` from the RepoRef (existing derivation), `AppSlug` passes through, `StateMcpModulePath` = existing const :1030, `StateMcpModuleVersion` = existing `StateMcpModulePin` var :1052 (keep the var + its ldflags stampability; delete `GoVersion`/`FrameworkGoVersion` consts :1001/:1015 and the three `go:embed` templates + `renderDesignWorkflow`/`scaffoldTemplateData` :1065-1108 — the module owns rendering now). Keep `DesignWorkflowPath`/`GoModPath`/`MethodTestPath` consts if referenced elsewhere (grep first; re-point to module-returned paths where trivial).
- [ ] Step 2: sync fast-path — `SyncManagedScaffold` first GETs `.claude/.method-assets-manifest.json` from the repo; if it parses and `version == methodassets.Version()` AND the rendered workflow files' pins are current (compare the ONE design-workflow file as today), return `changed=false` without touching the other ~100 files. Otherwise seat the full `ManagedScaffoldFiles` set via `putManagedFiles` (existing overwrite-if-changed) and report changed. Preserve the existing invariant "exactly one sync per dispatch-time session begin" (manager_test.go:5501) — the fast path must still count as the sync.
  - Note: `methodassets.Version()` uses build info — in server binaries it reports the module's pinned version; in `go test` it may report `(devel)`/pseudo-version. Tests must not assert a specific version string, only fast-path behavior with a matching manifest.
- [ ] Step 3: tests — update every sourcecontrol test touching ManagedScaffoldFiles/allowlist/sync (grep `ManagedScaffoldFiles\|scaffoldRootPaths\|SyncManagedScaffold` in `*_test.go`); add: full-set seat contains `.claude/commands/mission-draft.md` + both workflows + manifest; allowlist rejects `.claude/../x` and accepts `.claude/agents/x.md`; fast-path no-op when manifest version matches.
- [ ] Step 4: package + dependent packages green (`go build ./... && go test ./internal/resourceaccess/sourcecontrol/... ./internal/manager/systemdesign/... ./internal/manager/projectdesign/...`). Commit.

### Task B5: Construction venue switch

**Files:**
- Modify: `server/cmd/server/hooks.go` (new `ConstructionManagerRepo()` mirroring `SystemDesignManagerRepo` :446-462 → `repoForProject` :466)
- Modify: construction manager wiring (follow the appgen/hooks pipeline — `NewConstructionManager` currently takes no repo resolver, main.gen.go:534; whatever regen step the design managers' resolver wiring used, replicate it — inspect how `SystemDesignManagerRepo` reaches `NewSystemDesignManager` and copy that path exactly; if main.gen.go must change, change the GENERATOR input and regen, never hand-edit gen files)
- Modify: `server/internal/manager/construction/constructionmanager.go` (:1350-1356 — set `Repo` from the resolver) and `constructactivity.go` `submitPipeline` (:90-105 — set `PipelineSpec.TargetRepo` via the `designRepoTarget` pattern: `sourcecontrol.RepoRefOwnerRepo` → `RepoTarget{Owner, Name}`, and `WorkflowFile: "aiarch-construct.yml"`)
- Test: `server/internal/manager/construction/*_test.go`

**Interfaces:**
- Consumes: `hooks.repoForProject`, `sourcecontrol.RepoRefOwnerRepo` (contract.gen.go:1302), `PipelineSpec.TargetRepo`/`WorkflowFile` (constructionpipeline contract.gen.go:46-47).
- Produces: construction dispatches into the project's own repo; **`wf.Repo` non-nil also activates the dormant `gitEnabled` PR rail** (constructactivity.go:164-169) — this is the ratified gh-mode behavior (activity branches + PRs in the app repo), not an accident. Tests covering the dormant case must be updated to the active case, and one test must pin the fallback: resolver failure ⇒ zero TargetRepo ⇒ configured central repo (resolveTarget's documented legacy behavior).
- [ ] Steps: failing test (dispatch inputs carry project repo TargetRepo + aiarch-construct.yml) → wire resolver → activate → update dormant-case tests → package green → commit. If the appgen wiring for the resolver proves deeper than the SystemDesign precedent suggests, STOP and report BLOCKED with what you found (do not improvise generated-file edits).

### Task B6: Root `.claude` materialization + cruft removal + drift gate

**Files:**
- Modify: archistrator root `.claude/**` (materialized), `.claude/settings.local.json` (drop the validate-structurizr hook entry)
- Delete: `.claude/hooks/validate-structurizr.sh`, `.claude/structurizr-serve/`, `.claude/structurizr-validate/` (empty untracked dirs — just remove)
- Modify: `.github/workflows/server-checks.yml` (drift-gate step + `.claude/**` path trigger)

- [ ] Step 1: delete cruft (spec §7): the hook script, the settings.local.json hook block, the two empty dirs.
- [ ] Step 2: materialize: `go run github.com/mixofreality-studio/archistrator-platform/method-assets/cmd/method-assets@v0.1.1 install --dest .` from repo root. Verify: `git status` shows updates ONLY under `.claude/` managed set + the new manifest; `grillme/`, `settings.json`, `settings.local.json` survive untouched.
- [ ] Step 3: drift gate in server-checks.yml, alongside the `gen-*-check` block, plus add `.claude/**` to the trigger paths:

```yaml
      - name: claude-assets drift check
        working-directory: ${{ github.workspace }}
        run: |
          go run github.com/mixofreality-studio/archistrator-platform/method-assets/cmd/method-assets@v0.1.1 install --dest .
          git diff --exit-code -- .claude
```

  (Pin literal v0.1.1; bumping the pin is part of every future method-assets upgrade.)
- [ ] Step 4: commit (materialized tree + cruft removal + CI in one commit; the diff is large but mechanical — note in the message that content equals method-assets v0.1.1).

### Task B7: Full server gate + branch wrap

- [ ] Step 1: `cd server && gofmt -l . && GOWORK=off go build ./... && go vet ./... && golangci-lint run ./... && make test-short` → all clean.
- [ ] Step 2: run the `gen-*-check` drift commands from server-checks.yml locally (temporal/models/client/sdk/config/main/uiprofiles) — any drift means a generator input changed without regen; fix by regenerating.
- [ ] Step 3: grep gate: `grep -rn "design_prompt" server/ --include="*.go"` → zero production hits; `grep -rn "draftTask\|architectDraftPrompt\|pmCritiquePrompt\|answerPrompt" server/internal/manager/ --include="*.go" | grep -v _test` → empty.
- [ ] Step 4: final whole-branch review (SDD), fix wave, then merge `method-prompt-pass` → local `main` (NO push — founder pushes archistrator).

## Founder steps after merge (recorded, not executed)

1. Push archistrator main (PUSH-APP.sh or manual).
2. DRAIN in-flight design/construction workflows, then deploy the server (new dispatch contract + new scaffold set go live together; the seated aiarch-design.yml in app repos updates automatically via sync-on-next-dispatch).
3. gtdapp heals on its next design dispatch (sync seats `.claude` + both workflows + new go.mod). Then re-run the gtdapp Phase-2 amendment with the improved prompts.
