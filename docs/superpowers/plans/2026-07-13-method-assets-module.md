# Plan 1: `method-assets` Platform Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create `archistrator-platform/method-assets` — the module that owns every file seated into an app repo (.claude agents/commands/skills, both workflow templates, scaffold file templates) — with the full prompt content pass applied, and release it as `method-assets/v0.1.0`.

**Architecture:** A Go module embedding an `assets/` tree via `go:embed`, exposing `ClaudeFiles()` and `ScaffoldFiles(data)` plus a `cmd/method-assets install` manifest-tracked materializer. Assets are lifted from `archistrator/.claude` (cruft excluded), then upgraded in place: Go prompt doctrine merged into skills, ~22 new design-rail commands added, per-role `tools:` frontmatter added to agents.

**Tech Stack:** Go 1.25, `text/template` with `[[ ]]` delimiters (matches archistrator's `renderDesignWorkflow` convention), `go:embed`.

**Spec:** `archistrator/docs/superpowers/specs/2026-07-13-method-prompt-pass-design.md` (§1–§4, §6, §7 module-side). Plans 2 (server refactor) and 3 (loading states) follow separately.

## Global Constraints

- Module path: `github.com/mixofreality-studio/archistrator-platform/method-assets`; package `methodassets`; repo dir `/Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets`.
- Source `.claude` tree read from `/Users/davidmarne/mixofrealitystudio/archistrator/.claude` (referred to as `$ARCH/.claude` below; `$ARCH` = `/Users/davidmarne/mixofrealitystudio/archistrator`).
- EXCLUDE from the lift: `skills/grillme/`, `hooks/`, `structurizr-serve`, `structurizr-validate`, `settings.json`, `settings.local.json` (spec §7 — cruft or archistrator-local).
- Go version in templates and module: `1.25.0`. Pinned dep versions (spec §6): `framework-go v0.5.2`; tool directives `framework-go-app-generator v0.6.1`, `framework-go-http-generator v0.3.0`, `framework-go-mcp-generator v0.2.0`, `framework-go-projectmodel v0.2.1`.
- Template delimiters are `[[` `]]` everywhere (GitHub `${{ }}` must pass through untouched).
- Doctrine merges copy Go string constants **verbatim in meaning** from archistrator server sources; strip Go escaping (`\n` → real newline, `\"` → `"`) and drop MCP-mechanics sentences that duplicate what [[the-method-project-state]] already teaches. Never invent new doctrine.
- All commits in archistrator-platform repo. Run module tests with `cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets && go test ./...`.
- Platform tag convention: per-module prefix — final tag is `method-assets/v0.1.0`.
- The final push/tag step publishes to a PUBLIC repo — requires explicit founder go-ahead at that step.

---

### Task 1: Module bootstrap — embed plumbing + `ClaudeFiles()`

**Files:**
- Create: `method-assets/go.mod`
- Create: `method-assets/methodassets.go`
- Create: `method-assets/assets/claude/.gitkeep` (placeholder so embed compiles before Task 2)
- Test: `method-assets/methodassets_test.go`
- Modify: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/go.work` (add `./method-assets`)

**Interfaces:**
- Produces: `func ClaudeFiles() (map[string][]byte, error)` — keys are repo-relative paths (`.claude/agents/system-architect.md`, …); values are file bytes. Used by Tasks 8–10 and the archistrator server (Plan 2).

- [ ] **Step 1: Write the failing test**

```go
// method-assets/methodassets_test.go
package methodassets

import (
	"strings"
	"testing"
)

func TestClaudeFilesKeysAreRepoRelative(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("ClaudeFiles returned no files")
	}
	for path, body := range files {
		if !strings.HasPrefix(path, ".claude/") {
			t.Errorf("key %q must start with .claude/", path)
		}
		if len(body) == 0 {
			t.Errorf("file %q is empty", path)
		}
	}
}
```

- [ ] **Step 2: Create go.mod, add to go.work, run test to verify it fails**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets
cat > go.mod <<'EOF'
module github.com/mixofreality-studio/archistrator-platform/method-assets

go 1.25.0
EOF
mkdir -p assets/claude && touch assets/claude/.gitkeep
cd .. && go work use ./method-assets
cd method-assets && go test ./...
```

Expected: FAIL — `undefined: ClaudeFiles`.

- [ ] **Step 3: Write minimal implementation**

```go
// method-assets/methodassets.go
// Package methodassets owns every file archistrator seats into an app repo:
// the .claude agents/commands/skills tree, both GitHub workflow templates,
// and the scaffold file templates. Consumers: the archistrator server
// (managed scaffold), the cmd/method-assets materializer, and archistrator's
// own repo (dogfooding via the materializer + a CI drift gate).
package methodassets

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed all:assets
var assetsFS embed.FS

// ClaudeFiles returns the full .claude tree as repo-relative path -> bytes
// (".claude/agents/system-architect.md", ...). The map is rebuilt per call;
// callers may mutate it.
func ClaudeFiles() (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(assetsFS, "assets/claude", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Base(p) == ".gitkeep" {
			return err
		}
		body, rerr := assetsFS.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel := ".claude/" + p[len("assets/claude/"):]
		out[rel] = body
		return nil
	})
	return out, err
}
```

Note: Step 1's test also requires `len(files) > 0`, which stays red until Task 2 lifts real files. To keep this task independently green, temporarily seed one real file:

```bash
mkdir -p assets/claude/skills && printf '# placeholder — replaced in Task 2\n' > assets/claude/skills/PLACEHOLDER.md
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
git add method-assets go.work
git commit -m "feat(method-assets): module bootstrap with embedded claude tree + ClaudeFiles()"
```

---

### Task 2: Lift the `.claude` assets (cruft-filtered)

**Files:**
- Create: `method-assets/assets/claude/agents/*.md` (10 files), `method-assets/assets/claude/commands/*.md` (35 files), `method-assets/assets/claude/skills/**` (27 skill dirs)
- Delete: `method-assets/assets/claude/skills/PLACEHOLDER.md`
- Test: extend `method-assets/methodassets_test.go`

**Interfaces:**
- Produces: the embedded asset inventory later tasks edit in place. Inventory counts asserted: 10 agents, 35 commands, 27 skill dirs.

- [ ] **Step 1: Write the failing inventory test**

```go
func TestClaudeFilesInventory(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	agents, commands, skills := 0, 0, map[string]bool{}
	for p := range files {
		switch {
		case strings.HasPrefix(p, ".claude/agents/"):
			agents++
		case strings.HasPrefix(p, ".claude/commands/"):
			commands++
		case strings.HasPrefix(p, ".claude/skills/"):
			parts := strings.Split(p, "/")
			skills[parts[2]] = true
		}
	}
	if agents != 10 {
		t.Errorf("agents = %d, want 10", agents)
	}
	if commands != 35 { // grows to 57 in Tasks 5-6; update there
		t.Errorf("commands = %d, want 35", commands)
	}
	if len(skills) != 27 {
		t.Errorf("skill dirs = %d, want 27", len(skills))
	}
	if skills["grillme"] {
		t.Error("grillme must NOT be lifted (archistrator-local)")
	}
	for p := range files {
		if strings.Contains(p, "structurizr") || strings.HasPrefix(p, ".claude/hooks/") {
			t.Errorf("cruft lifted: %s", p)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestClaudeFilesInventory`
Expected: FAIL — counts are 0/0/1 (placeholder only).

- [ ] **Step 3: Copy the assets**

```bash
ARCH=/Users/davidmarne/mixofrealitystudio/archistrator
MA=/Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets/assets/claude
rm "$MA/skills/PLACEHOLDER.md"
cp -R "$ARCH/.claude/agents"   "$MA/agents"
cp -R "$ARCH/.claude/commands" "$MA/commands"
cp -R "$ARCH/.claude/skills"   "$MA/skills"
rm -rf "$MA/skills/grillme"
```

- [ ] **Step 4: Fix the two stale phrasings (spec §7)**

In `$MA/commands/implement-project.md` line 7: replace the string `methodpoc/designs/<product>/*.md` with `designs/<product>/*.md` (it is a negation — "NOT in …" — keep the negation, drop the dead `methodpoc/` prefix).
In `$MA/agents/senior-developer.md` line 53: the sentence naming `designs/.../contracts/<X>.md` — rewrite the file reference to say "there is no contracts markdown file; the contract lives in `.aiarch/state/project.json` → `.serviceContracts`" preserving the sentence's negation intent.

- [ ] **Step 5: Run tests, commit**

Run: `go test ./...`
Expected: PASS

```bash
git add -A method-assets && git commit -m "feat(method-assets): lift .claude assets from archistrator, cruft-filtered"
```

---

### Task 3: Merge Phase-1 Go doctrine into the skills

The design-rail Go prompts (deleted in Plan 2) carry doctrine the skills must absorb first. Source file: `$ARCH/server/internal/manager/systemdesign/coauthorartifact.go`. All edits are to skill files under `method-assets/assets/claude/skills/`.

**Files:**
- Modify: `assets/claude/skills/the-method-business-alignment/SKILL.md`
- Modify: `assets/claude/skills/the-method-requirements-analysis/SKILL.md`
- Modify: `assets/claude/skills/the-method-volatility-identification/SKILL.md`
- Modify: `assets/claude/skills/the-method-core-use-cases/SKILL.md`
- Modify: `assets/claude/skills/the-method-architecture/SKILL.md`
- Modify: `assets/claude/skills/the-method-operational-concepts/SKILL.md`
- Modify: `assets/claude/skills/the-method-system-design-standard-check/SKILL.md`

**Interfaces:**
- Consumes: Go constants at verified anchors (below).
- Produces: skills that are self-sufficient — a draft command (Task 5) + skill must reproduce everything the old Go prompt said.

- [ ] **Step 1: Extract the source doctrine**

Read these from `coauthorartifact.go` (anchors verified 2026-07-13):
- `draftTask(kind)` at **line 2663** — per-kind task bodies for Mission (line 2666), Glossary, ScrubbedRequirements, Volatilities, CoreUseCases, System, OperationalConcepts, StandardCheck.
- `activityDiagramGuide` const at **line 2621** (multiline raw string).
- `operatingModelDeploymentConstraint(m)` at **line 2572** (per-operating-model constraint strings).

- [ ] **Step 2: Merge, one skill per kind**

For each kind, open the target skill and add a section `## Draft-job doctrine (CI dispatch)` containing the corresponding `draftTask` body converted to markdown prose (strip Go escaping). Mapping:

| draftTask kind | Target skill | Also merge |
|---|---|---|
| Mission | the-method-business-alignment | — |
| Glossary | the-method-requirements-analysis | — |
| ScrubbedRequirements | the-method-requirements-analysis (own subsection) | — |
| Volatilities | the-method-volatility-identification | — |
| CoreUseCases | the-method-core-use-cases | full `activityDiagramGuide` text as a `### Activity diagram rules` subsection |
| System | the-method-architecture | — |
| OperationalConcepts | the-method-operational-concepts | `operatingModelDeploymentConstraint` variants as `### Operating-model deployment constraint` — the Go source gates this on `KindOperationalConcepts` (coauthorartifact.go:2551), corrected from an earlier draft of this table |
| StandardCheck | the-method-system-design-standard-check | — |

Rules: if the skill already states a doctrine sentence (several overlap), keep ONE copy — prefer the Go wording where it is stricter (e.g. the Mission body's "you MUST NOT use the words component, module, service…" list). Do NOT copy the `architectHeader`/`pmHeader` MCP-mechanics sentences — [[the-method-project-state]] already teaches the tool flow; the commands (Task 5) reference it.

- [ ] **Step 3: Verify no doctrine left behind**

For each of the 8 kinds, diff-read the Go body against the edited skill and confirm every REQUIREMENT sentence (MUST/NEVER/reject conditions, enum rules, format rules) appears in the skill. Grep spot-check:

```bash
grep -rl "purely linear, so leave it null" method-assets/assets/claude/skills/the-method-core-use-cases/
```

Expected: the file is listed (guide text present).

- [ ] **Step 4: Commit**

```bash
git add -A method-assets && git commit -m "feat(method-assets): merge Phase-1 Go draft doctrine into the-method skills"
```

---

### Task 4: Merge Phase-2 Go doctrine into the skills

Source file: `$ARCH/server/internal/manager/projectdesign/coauthorphase2artifact.go`.

**Files:**
- Modify: `assets/claude/skills/the-method-planning-assumptions/SKILL.md`
- Modify: `assets/claude/skills/the-method-activity-list/SKILL.md`
- Modify: `assets/claude/skills/the-method-network-draft/SKILL.md`
- Modify: `assets/claude/skills/the-method-normal-solution/SKILL.md`
- Modify: `assets/claude/skills/the-method-subcritical-solution/SKILL.md`
- Modify: `assets/claude/skills/the-method-decompressed-solution/SKILL.md`
- Modify: `assets/claude/skills/the-method-compressed-solution/SKILL.md`
- Modify: `assets/claude/skills/the-method-risk-modeling/SKILL.md`
- Modify: `assets/claude/skills/the-method-sdp-review/SKILL.md`

**Interfaces:**
- Consumes: `draftTask(kind)` at **line 1977** (PlanningAssumptions line 1980, ActivityList line 1982, solutions lines 1986–1992, plus Network/RiskModel/SdpReview cases in the same switch); `methodTeamRosterDoctrine` const **line 1958**; `activityInventoryDoctrine` const **line 1967**; `solutionClassRatesDoctrine` const **line 1974**; `operatingModelInfrastructureConstraint` **line 1905**.

- [ ] **Step 1: Merge per kind** (same `## Draft-job doctrine (CI dispatch)` section pattern as Task 3)

| draftTask kind | Target skill | Also merge |
|---|---|---|
| PlanningAssumptions | the-method-planning-assumptions | `operatingModelInfrastructureConstraint` variants; roster — see step 2 |
| ActivityList | the-method-activity-list | inventory + roster — see step 2 |
| Network | the-method-network-draft | — |
| NormalSolution | the-method-normal-solution | `solutionClassRatesDoctrine` |
| SubcriticalSolution | the-method-subcritical-solution | `solutionClassRatesDoctrine` |
| DecompressedSolution | the-method-decompressed-solution | `solutionClassRatesDoctrine` |
| CompressedSolution | the-method-compressed-solution | `solutionClassRatesDoctrine` |
| RiskModel | the-method-risk-modeling | — |
| SdpReview | the-method-sdp-review | — |

- [ ] **Step 2: Dedupe against commit 4dc8d81's earlier mirror**

Commit `4dc8d81` (archistrator) already copied roster + inventory doctrine into `the-method-activity-list/SKILL.md` and `the-method-planning-assumptions/SKILL.md`. Read both lifted skills first; where the doctrine already exists, verify it matches the Go const text sentence-for-sentence and do NOT duplicate — reconcile to a single copy using the Go wording as canonical.

- [ ] **Step 3: Verify**

```bash
grep -l "WORKER CLASSES ARE A FIXED ROSTER" method-assets/assets/claude/skills/the-method-planning-assumptions/SKILL.md method-assets/assets/claude/skills/the-method-activity-list/SKILL.md
grep -c "classRates MUST be the PlanningAssumptions rateCard derivation" method-assets/assets/claude/skills/the-method-normal-solution/SKILL.md
```

Expected: both files listed; count `1`.

- [ ] **Step 4: Commit**

```bash
git add -A method-assets && git commit -m "feat(method-assets): merge Phase-2 Go draft doctrine into the-method skills"
```

---

### Task 5: The 16 per-kind draft commands

> CORRECTION (found during Task 4): `KindSdpReview` is REJECTED at the coauthor workflow façade (`coauthorphase2artifact.go:234`) — the SDP review is assembled server-side by `AssembleSDPReviewWorkflow`, never dispatched as a CI draft job. `sdp-review-draft` is therefore dropped (it could never be dispatched). Counts below adjusted 17→16, 52→51; Task 6 total 57→56.

**Files:**
- Create: `method-assets/assets/claude/commands/<name>.md` × 16 (names in the table)
- Test: extend `TestClaudeFilesInventory` (commands 35 → 51)

**Interfaces:**
- Produces: command names consumed verbatim by Plan 2's `DesignCommandFor(kind, mode)`: `mission-draft`, `glossary-draft`, `scrubbed-requirements-draft`, `volatilities-draft`, `core-use-cases-draft`, `system-draft`, `operational-concepts-draft`, `standard-check-draft`, `planning-assumptions-draft`, `activity-list-draft`, `network-draft`, `normal-solution-draft`, `subcritical-solution-draft`, `decompressed-solution-draft`, `compressed-solution-draft`, `risk-model-draft`.

- [ ] **Step 1: Update the inventory test** to `want 51` commands and add:

```go
func TestDesignDraftCommandsExist(t *testing.T) {
	files, _ := ClaudeFiles()
	for _, name := range []string{
		"mission-draft", "glossary-draft", "scrubbed-requirements-draft",
		"volatilities-draft", "core-use-cases-draft", "system-draft",
		"operational-concepts-draft", "standard-check-draft",
		"planning-assumptions-draft", "activity-list-draft", "network-draft",
		"normal-solution-draft", "subcritical-solution-draft",
		"decompressed-solution-draft", "compressed-solution-draft",
		"risk-model-draft",
	} {
		if _, ok := files[".claude/commands/"+name+".md"]; !ok {
			t.Errorf("missing draft command %s", name)
		}
	}
}
```

Run: `go test ./... -run TestDesignDraftCommandsExist` → Expected: FAIL (all 17 missing).

- [ ] **Step 2: Write the commands from this template**

Full content of `mission-draft.md` (the canonical instance — write it exactly like this):

```markdown
# /mission-draft

> Draft (or amend) the **Mission** artifact — vision, business objectives, mission statement — as one design-rail CI job. The job's ambient env fixes the artifact kind and target slot.

**Arguments** — none. Kind, job mode, branch, and project come from the ambient `AIARCH_*` env baked into this CI run.

**Agent + skills.** Work as the **`system-architect`** agent (`.claude/agents/system-architect.md`). Follow **[[the-method-business-alignment]]** (including its "Draft-job doctrine" section — that is your task statement) and **[[the-method-project-state]]** for the tool flow.

## Steps

> **State changes go through the `aiarch-state` MCP tools, not hand-edits.** Never hand-edit `.aiarch/state/project.json`; never run `git` for state.

1. **Read your inputs.** `listResearchSources`/`getResearchSource` for the research corpus; `getCommittedSlot` for every committed predecessor slot this artifact builds on; on an amendment, `getDraftSlot` for the current draft.
2. **Read the review ledger** with `getReviewThread`. If open comments exist, this is a redraft: your draft MUST address every open comment.
3. **Draft** the typed model per [[the-method-business-alignment]]. Submit with `putDraftModel` — it validates and returns actionable errors; fix and resubmit until accepted.
4. **Respond to every open ledger comment** with `respondToReviewComment` — accept (say what you changed) or rebut (say why not, grounded in the Method). Silent non-response is a defect.
5. **Finish** with `publishDraft` (exactly once). Do not open PRs, do not merge, do not touch phase status — the server owns the loop.
```

Generate the other 15 by substituting three fields; everything else is identical EXCEPT step 1's research clause: the `listResearchSources`/`getResearchSource` fragment appears ONLY in mission-draft (the Go kind-switch grants research access solely to KindMission, coauthorartifact.go:2510; downstream kinds draft from committed slots) — the other 15 open step 1 with `getCommittedSlot` directly:

| Command file | H1 + blockquote artifact phrase | Skill wikilink | Basis note appended to step 1 |
|---|---|---|---|
| glossary-draft | **Glossary** | [[the-method-requirements-analysis]] | basis: `.mission` |
| scrubbed-requirements-draft | **Scrubbed Requirements** | [[the-method-requirements-analysis]] | basis: `.mission`, `.glossary` |
| volatilities-draft | **Volatilities** | [[the-method-volatility-identification]] | basis: `.mission`, `.glossary`, `.scrubbedRequirements` |
| core-use-cases-draft | **Core Use Cases** | [[the-method-core-use-cases]] | basis: all Phase-1 predecessors |
| system-draft | **System (layered decomposition + dynamic views)** | [[the-method-architecture]] | basis: all Phase-1 predecessors |
| operational-concepts-draft | **Operational Concepts** | [[the-method-operational-concepts]] | basis: `.mission`, `.systemDesign` |
| standard-check-draft | **Design Standard Check** | [[the-method-system-design-standard-check]] | basis: all Phase-1 slots |
| planning-assumptions-draft | **Planning Assumptions** | [[the-method-planning-assumptions]] | basis: `.systemDesign` |
| activity-list-draft | **Activity List** | [[the-method-activity-list]] | basis: `.systemDesign`, `.planningAssumptions` |
| network-draft | **Project Network** | [[the-method-network-draft]] | basis: `.activityList`, `.planningAssumptions` |
| normal-solution-draft | **Normal Solution** | [[the-method-normal-solution]] | basis: `.network`, `.planningAssumptions` |
| subcritical-solution-draft | **Subcritical Solution** | [[the-method-subcritical-solution]] | basis: `.normalSolution`, `.network`, `.planningAssumptions` |
| decompressed-solution-draft | **Decompressed-Normal Solution** | [[the-method-decompressed-solution]] | basis: `.normalSolution` + its risk |
| compressed-solution-draft | **Compressed Solution** | [[the-method-compressed-solution]] | basis: `.normalSolution`, `.network`, `.planningAssumptions` |
| risk-model-draft | **Risk Model** | [[the-method-risk-modeling]] | basis: all four solution options |

For Phase-2 commands (planning-assumptions-draft onward) the agent line stays `system-architect`; add one sentence after it: "The **`project-manager`** agent's constraint data is already committed in the basis slots — you design; the PM slot-ownership rules in [[the-method-network-draft]] still apply."

- [ ] **Step 3: Run tests to verify pass, commit**

Run: `go test ./...` → Expected: PASS.

```bash
git add -A method-assets && git commit -m "feat(method-assets): 17 per-kind design draft commands"
```

---

### Task 6: Critique commands (4), `design-answer` + `design-answer-pm`, and anti-thrash doctrine

> CORRECTION (found during Task 6): the Go answer job is addressee-parameterized — `AskQuestions` accepts addressee "pm" | "architect" (systemdesignmanager.go:1521). One architect-pinned answer command cannot serve PM-addressed questions, and the roles hold different tool scopes. Add `design-answer-pm.md` (product-manager agent, same steps, answers grounded in customer/business reality). Counts: 51 → 57.

**Files:**
- Create: `assets/claude/commands/mission-critique.md`, `glossary-critique.md`, `scrubbed-requirements-critique.md`, `core-use-cases-critique.md`, `design-answer.md`, `design-answer-pm.md`
- Modify: `assets/claude/agents/product-manager.md` (critique discipline section)
- Test: extend inventory test (commands 51 → 57) + names test

**Interfaces:**
- Consumes: Go `pmCritiquePrompt` at `coauthorartifact.go:2593` and `answerPrompt` at `systemdesignmanager.go:1796` / `projectdesignmanager.go:1560` (read for content to preserve).
- Produces: command names for Plan 2: `mission-critique`, `glossary-critique`, `scrubbed-requirements-critique`, `core-use-cases-critique`, `design-answer`, `design-answer-pm`.

- [ ] **Step 1: Extend tests** (inventory `want 57`; add the 5 names to a `TestDesignReviewCommandsExist` copy of the Task 5 test). Run → FAIL.

- [ ] **Step 2: Write `mission-critique.md`** (canonical instance):

```markdown
# /mission-critique

> PM critique of the drafted **Mission** artifact — one design-rail CI job. Verdict only; the PM never rewrites the model.

**Arguments** — none. Kind, job mode, branch, and project come from the ambient `AIARCH_*` env.

**Agent + skills.** Work as the **`product-manager`** agent (`.claude/agents/product-manager.md`). Judge against **[[the-method-business-alignment]]** and [[the-method-project-state]] for the tool flow.

## Steps

1. **Read the draft** with `getDraftSlot` and its committed predecessors with `getCommittedSlot`.
2. **Read the ledger first** with `getReviewThread`. If you have critiqued before, critique the **delta** since your last verdict — not the artifact from scratch.
3. **Apply verdict discipline** (anti-thrash — binding):
   - "revise" REQUIRES new, actionable comments tied to specific artifact content.
   - Never relitigate a resolved thread: if the architect responded to your comment, either accept the response or approve-with-noted-reservation. Repeating an already-answered comment is a defect.
   - Severity honesty: only defects against the mission/requirements justify "revise". Taste-level preferences are recorded as comments on an **approve**.
4. **Record the verdict** with `setCritiqueVerdict` (approve/revise + comments).
5. **Finish** with `publishDraft` (exactly once). You have no `putDraftModel`; do not attempt to fix the model yourself.
```

Generate the other 3 critique commands substituting artifact phrase + judged-against skill: glossary-critique → **Glossary** / [[the-method-requirements-analysis]]; scrubbed-requirements-critique → **Scrubbed Requirements** / [[the-method-requirements-analysis]]; core-use-cases-critique → **Core Use Cases** / [[the-method-core-use-cases]] (add: "you co-discover; if you object, say which raw use case is core and why — customer reality is your authority, abstraction taste is the architect's").

Before finalizing, read `pmCritiquePrompt` (`coauthorartifact.go:2593`) and fold any judging criteria it states (beyond the header mechanics) into the relevant critique command's step 3.

- [ ] **Step 3: Write `design-answer.md`**:

```markdown
# /design-answer

> Answer the founder's open review questions on a staged design artifact — one design-rail CI job. Kind-agnostic: the ambient env fixes which artifact.

**Arguments** — none. Ambient `AIARCH_*` env fixes kind, branch, project.

**Agent + skills.** Work as the **`system-architect`** agent. Ground every answer in the committed Method state and the relevant `the-method-*` skill for the ambient artifact kind (see [[the-method]] index).

## Steps

1. `getReviewThread` — collect the OPEN questions addressed to you.
2. `getDraftSlot` / `getCommittedSlot` for the artifact and its basis — answers must cite actual state, never memory.
3. Answer each open question with `respondToReviewComment`: direct answer first, then the Method rationale (cite the book chapter the skill names). If a question exposes a real defect, say so plainly and state what an amendment would change — do NOT amend here (no `putDraftModel` in this mode).
4. `publishDraft` exactly once.
```

- [ ] **Step 4: Add the critique-discipline section to `product-manager.md`** — append under its existing body:

```markdown
## Critique discipline (design-rail CI)

When dispatched on a `*-critique` command you hold verdict authority, not
authorship. Binding rules: (1) "revise" requires new, actionable comments on
specific content; (2) never relitigate a thread the architect has responded
to — accept or approve-with-reservation; (3) only mission/requirements
defects justify "revise" — taste rides on an approve; (4) you have no
`putDraftModel` and never rewrite the model. The server caps redraft rounds
at 5 and escalates to the founder — your job is to converge well before that.
```

- [ ] **Step 5: Run tests, commit**

Run: `go test ./...` → Expected: PASS.

```bash
git add -A method-assets && git commit -m "feat(method-assets): critique + answer commands with anti-thrash doctrine"
```

---

### Task 7: Per-role `tools:` frontmatter on all 10 agents

**Files:**
- Modify: all 10 files under `method-assets/assets/claude/agents/`
- Test: `method-assets/frontmatter_test.go`

**Interfaces:**
- Produces: `tools:` frontmatter consumed by Claude Code at dispatch time. Scoping table is spec §4 verbatim.

- [ ] **Step 1: Write the failing test**

```go
// method-assets/frontmatter_test.go
package methodassets

import (
	"strings"
	"testing"
)

// wantWrites: the exact aiarch-state WRITE verbs each role may hold (spec §4).
var wantWrites = map[string][]string{
	"system-architect":  {"putDraftModel", "publishDraft", "respondToReviewComment"},
	"product-manager":   {"setCritiqueVerdict", "respondToReviewComment", "publishDraft"},
	"project-manager":   {"putDraftModel", "publishDraft"},
	"senior-developer":  {"recordServiceContract", "publishDraft", "respondToReviewComment"},
	"junior-developer":  {"recordPhaseArtifact", "publishDraft", "respondToReviewComment"},
	"ui-designer":       {"recordPhaseArtifact", "publishDraft", "respondToReviewComment"},
	"ux-reviewer":       {"respondToReviewComment"},
	"test-engineer":     {"recordTestingState", "publishDraft", "respondToReviewComment"},
	"software-tester":   {"recordTestingState", "publishDraft", "respondToReviewComment"},
	"qa-engineer":       {"recordTestingState", "publishDraft", "respondToReviewComment"},
}

var allWrites = []string{
	"putDraftModel", "setCritiqueVerdict", "recordServiceContract",
	"recordPhaseArtifact", "recordTestingState", "publishDraft",
	"respondToReviewComment",
}

func TestAgentToolScoping(t *testing.T) {
	files, _ := ClaudeFiles()
	for role, wants := range wantWrites {
		body := string(files[".claude/agents/"+role+".md"])
		fm := body[:strings.Index(body, "\n---")] // frontmatter block
		if !strings.Contains(fm, "tools:") {
			t.Errorf("%s: no tools: frontmatter", role)
			continue
		}
		allowed := map[string]bool{}
		for _, w := range wants {
			allowed[w] = true
			if !strings.Contains(fm, "mcp__aiarch-state__"+w) {
				t.Errorf("%s: missing sanctioned write %s", role, w)
			}
		}
		for _, w := range allWrites {
			if !allowed[w] && strings.Contains(fm, "mcp__aiarch-state__"+w) {
				t.Errorf("%s: holds UNSANCTIONED write %s", role, w)
			}
		}
	}
	// ux-reviewer reviews, never amends: no Edit/Write built-ins.
	fm := string(files[".claude/agents/ux-reviewer.md"])
	fm = fm[:strings.Index(fm, "\n---")]
	for _, banned := range []string{"Edit", "Write"} {
		for _, line := range strings.Split(fm, "\n") {
			l := strings.TrimSpace(strings.TrimPrefix(line, "-"))
			if l == banned {
				t.Errorf("ux-reviewer: banned built-in %s", banned)
			}
		}
	}
}
```

Run: `go test ./... -run TestAgentToolScoping` → Expected: FAIL (no `tools:` anywhere).

- [ ] **Step 2: Add `tools:` to each agent's frontmatter**

Shared read verbs — include in EVERY agent's list:
`mcp__aiarch-state__getCommittedSlot`, `mcp__aiarch-state__getDraftSlot`, `mcp__aiarch-state__getReviewThread`, `mcp__aiarch-state__listResearchSources`, `mcp__aiarch-state__getResearchSource`, `mcp__aiarch-state__projectStateReadProject`.

Built-ins: `Read, Grep, Glob, Bash` for every role; add `Edit, Write` for every role EXCEPT `ux-reviewer` and `product-manager` (neither authors files).

Canonical instance — `system-architect.md` frontmatter becomes:

```yaml
---
name: system-architect
description: <UNCHANGED — keep existing text>
model: fable
skills: the-method
tools:
  - Read
  - Grep
  - Glob
  - Bash
  - Edit
  - Write
  - mcp__aiarch-state__getCommittedSlot
  - mcp__aiarch-state__getDraftSlot
  - mcp__aiarch-state__getReviewThread
  - mcp__aiarch-state__listResearchSources
  - mcp__aiarch-state__getResearchSource
  - mcp__aiarch-state__projectStateReadProject
  - mcp__aiarch-state__putDraftModel
  - mcp__aiarch-state__publishDraft
  - mcp__aiarch-state__respondToReviewComment
  - mcp__aiarch-state__estimationComputeNetwork
  - mcp__aiarch-state__estimationEstimateForOption
  - mcp__aiarch-state__reviewProposeReviews
---
```

Per-role deltas from that canon (write verbs per the test table above; engine computes):
- **project-manager**: computes `estimationComputeEarnedValue`, `estimationComputeNetwork`, `estimationEstimateForOption`, `interventionDecideOnVariance`; writes per table.
- **product-manager**: no Edit/Write; no engine computes; writes per table.
- **senior/junior-developer, ui-designer, test-engineer, software-tester, qa-engineer**: no engine computes; writes per table.
- **ux-reviewer**: no Edit/Write; writes = `respondToReviewComment` only.

Also add ONE prose line under each testing-trio agent's body (field split frontmatter can't express): test-engineer — "your `recordTestingState` writes are systemTestPlan / harnessModule / perfHarness only"; software-tester — "…testRun / defect only"; qa-engineer — "…qualityGate / qualityAuditReport only".

- [ ] **Step 3: Run tests, commit**

Run: `go test ./...` → Expected: PASS.

```bash
git add -A method-assets && git commit -m "feat(method-assets): by-the-book per-role tools frontmatter on all agents"
```

---

### Task 8: Workflow templates (thin-prompt design + venue-portable construct)

**Files:**
- Create: `method-assets/assets/workflows/aiarch-design.yml.tmpl` (copied from `$ARCH/server/internal/resourceaccess/sourcecontrol/assets/aiarch-design.yml.tmpl`, then modified)
- Create: `method-assets/assets/workflows/aiarch-construct.yml.tmpl` (copied from `$ARCH/.github/workflows/aiarch-construct.yml`, then templatized)
- Test: `method-assets/workflows_test.go`

**Interfaces:**
- Produces: the dispatch contract Plan 2's server conforms to. Design inputs: `idempotency_token`, `command` (NEW — replaces `design_prompt`), `artifact_kind`, `target_branch`, `prior_state_ref`, `job_mode`. Construct inputs unchanged: `idempotency_token`, `activity_id`, `component_id`, `command`, `phase`, `role`. Template fields: `[[.AppSlug]]`, `[[.StateMcpModulePath]]`, `[[.StateMcpModuleVersion]]`.

- [ ] **Step 1: Write the failing test**

```go
// method-assets/workflows_test.go
package methodassets

import (
	"strings"
	"testing"
)

func TestWorkflowTemplates(t *testing.T) {
	for _, name := range []string{"aiarch-design.yml.tmpl", "aiarch-construct.yml.tmpl"} {
		body, err := assetsFS.ReadFile("assets/workflows/" + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s := string(body)
		if strings.Contains(s, "design_prompt") {
			t.Errorf("%s: raw design_prompt input must be gone (thin dispatch)", name)
		}
		if !strings.Contains(s, "/${{ inputs.command }}") {
			t.Errorf("%s: prompt must be the thin slash-command invocation", name)
		}
		if !strings.Contains(s, "[[.AppSlug]]") {
			t.Errorf("%s: allowed_bots must be templated on [[.AppSlug]]", name)
		}
	}
	// Construct must NOT build the MCP from source (app repos have no server source).
	c, _ := assetsFS.ReadFile("assets/workflows/aiarch-construct.yml.tmpl")
	if strings.Contains(string(c), "go build ./cmd/aiarch-state-mcp") {
		t.Error("construct template still builds MCP from checked-out source; must go install [[.StateMcpModulePath]]@[[.StateMcpModuleVersion]]")
	}
}
```

Run → Expected: FAIL (files missing).

- [ ] **Step 2: Build `aiarch-design.yml.tmpl`**

Copy the server's template verbatim, then apply exactly these changes:
1. In the `workflow_dispatch.inputs` block: delete the `design_prompt` input; add `command` (`description: "slash command for this design job"`, `required: true`, `type: string`). Keep `idempotency_token`, `artifact_kind`, `target_branch`, `prior_state_ref`, `job_mode` unchanged (the `AIARCH_*` env and MCP ambient scoping still key on them).
2. In the `anthropics/claude-code-action@v1` step (currently the `prompt:` at ~lines 404–418): replace the whole composed prompt with:

```yaml
          prompt: |
            /${{ inputs.command }}
```

   Delete the appended aiarch-state-MCP usage boilerplate paragraph — the commands + [[the-method-project-state]] now carry it.
3. Leave the MCP wiring (`--mcp-config`, `go install` of the state MCP at `[[.StateMcpModulePath]]@[[.StateMcpModulePin]]`, the `AIARCH_*` env block at ~lines 346–365) untouched, except rename the pin field to `[[.StateMcpModuleVersion]]` for API consistency.
4. `allowed_bots:` stays templated on the app slug — confirm the field name is `[[.AppSlug]]` (rename from the server template's field if it differs; Task 9's `ScaffoldData` defines the canonical name).

- [ ] **Step 3: Build `aiarch-construct.yml.tmpl`**

Copy `$ARCH/.github/workflows/aiarch-construct.yml`, then:
1. `allowed_bots: archistrator-bot` (line ~156) → `allowed_bots: [[.AppSlug]]`.
2. Replace the build-MCP-from-source step (lines ~112–120, `go build ./cmd/aiarch-state-mcp`) with the same `go install [[.StateMcpModulePath]]@[[.StateMcpModuleVersion]]` step the design template uses (copy that step across).
3. Prompt block (lines ~171–172) is already `/${{ inputs.command }} ${{ inputs.component_id }} ${{ inputs.activity_id }}` — keep it.
4. Any `working-directory: server` or archistrator-specific path assumptions: parameterize or drop — the app repo root is the module root. Read the full file while editing and list every archistrator-specific reference you removed in the commit message body.

- [ ] **Step 4: Run tests, commit**

Run: `go test ./...` → Expected: PASS.

```bash
git add -A method-assets && git commit -m "feat(method-assets): thin-prompt design + venue-portable construct workflow templates"
```

---

### Task 9: Scaffold templates + `ScaffoldFiles(data)`

**Files:**
- Create: `method-assets/assets/scaffold/go.mod.tmpl`, `assets/scaffold/aiarch_method_test.go.tmpl` (copy from `$ARCH/server/internal/resourceaccess/sourcecontrol/assets/aiarch_method_test.go.tmpl`), `assets/scaffold/project.json.tmpl`
- Create: `method-assets/scaffold.go`
- Test: `method-assets/scaffold_test.go`

**Interfaces:**
- Produces (Plan 2's server consumes):

```go
type ScaffoldData struct {
	ModulePath            string // github.com/<owner>/<repo>
	AppSlug               string // GitHub App slug for allowed_bots
	ProjectID             string // project.json id (repo name)
	Owner                 string // org/user login
	Name                  string // project display name
	StateMcpModulePath    string // archistrator state-MCP module path
	StateMcpModuleVersion string // pin (version or SHA)
}
func ScaffoldFiles(data ScaffoldData) (map[string][]byte, error)
```

Returned keys: `.github/workflows/aiarch-design.yml`, `.github/workflows/aiarch-construct.yml`, `go.mod`, `aiarch_method_test.go`, `internal/.gitkeep`, `.aiarch/state/project.json`, plus every `ClaudeFiles()` entry. Version pins exported as consts: `GoVersion = "1.25.0"`, `FrameworkGoVersion = "v0.5.2"`, `AppGeneratorVersion = "v0.6.1"`, `HTTPGeneratorVersion = "v0.3.0"`, `MCPGeneratorVersion = "v0.2.0"`, `ProjectModelVersion = "v0.2.1"`.

- [ ] **Step 1: Write the failing test**

```go
// method-assets/scaffold_test.go
package methodassets

import (
	"encoding/json"
	"strings"
	"testing"
)

var testData = ScaffoldData{
	ModulePath: "github.com/acme/widgets", AppSlug: "aiarch-app",
	ProjectID: "widgets", Owner: "acme", Name: "widgets",
	StateMcpModulePath:    "github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp",
	StateMcpModuleVersion: "v0.0.0-test",
}

func TestScaffoldFiles(t *testing.T) {
	files, err := ScaffoldFiles(testData)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		".github/workflows/aiarch-design.yml", ".github/workflows/aiarch-construct.yml",
		"go.mod", "aiarch_method_test.go", "internal/.gitkeep",
		".aiarch/state/project.json", ".claude/agents/system-architect.md",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s", want)
		}
	}
	for p, b := range files {
		if strings.Contains(string(b), "[[") {
			t.Errorf("%s: unrendered [[ ]] template field", p)
		}
	}
	gomod := string(files["go.mod"])
	for _, want := range []string{
		"module github.com/acme/widgets",
		"go 1.25.0",
		"require github.com/mixofreality-studio/archistrator-platform/framework-go " + FrameworkGoVersion,
		"tool github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator",
	} {
		if !strings.Contains(gomod, want) {
			t.Errorf("go.mod missing %q", want)
		}
	}
	var pj struct {
		ID    string         `json:"id"`
		Phase int            `json:"phase"`
		Slots map[string]any `json:"slots"`
	}
	if err := json.Unmarshal(files[".aiarch/state/project.json"], &pj); err != nil {
		t.Fatalf("project.json invalid: %v", err)
	}
	if pj.ID != "widgets" || pj.Phase != 0 || len(pj.Slots) != 0 {
		t.Errorf("project.json seed wrong: %+v", pj)
	}
}
```

Run → Expected: FAIL.

- [ ] **Step 2: Write the templates**

`assets/scaffold/go.mod.tmpl`:

```
module [[.ModulePath]]

go [[.GoVersion]]

require github.com/mixofreality-studio/archistrator-platform/framework-go [[.FrameworkGoVersion]]

tool (
	github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/cmd/app-generator
	github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/cmd/http-generator
	github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/cmd/mcp-generator
	github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel/cmd/modelgen
)
```

**Recon note for the implementer:** verify each generator module's actual `cmd/` binary path (`ls /Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go-app-generator/cmd/` etc.) and correct the four tool lines to the real main-package paths. `tool` directives don't carry versions — versions land in the require block; add the four generator modules as indirect requires pinned to the Global Constraints versions (a rendered-`go.mod` note comment above the block: `// tool deps pinned; go mod tidy keeps them via the tool directives`).

`assets/scaffold/project.json.tmpl`:

```json
{
  "id": "[[.ProjectID]]",
  "name": "[[.Name]]",
  "owner": "[[.Owner]]",
  "phase": 0,
  "version": 2,
  "slots": {}
}
```

**Recon note:** diff against gtdapp's real birth `project.json` top-level keys (`id,name,owner,phase,research,reviewPolicy,slots,updatedAt,version`) and include `research`/`reviewPolicy` defaults IF the state MCP requires them present — copy the empty-object shapes from gtdapp's file at `/Users/davidmarne/mixofrealitystudio/gtdapp/.aiarch/state/project.json`.

- [ ] **Step 3: Write `scaffold.go`**

```go
// method-assets/scaffold.go
package methodassets

import (
	"bytes"
	"embed"
	"text/template"
)

// Pinned platform versions the scaffold seeds (spec §6).
const (
	GoVersion           = "1.25.0"
	FrameworkGoVersion  = "v0.5.2"
	AppGeneratorVersion = "v0.6.1"
	HTTPGeneratorVersion = "v0.3.0"
	MCPGeneratorVersion  = "v0.2.0"
	ProjectModelVersion  = "v0.2.1"
)

type ScaffoldData struct {
	ModulePath            string
	AppSlug               string
	ProjectID             string
	Owner                 string
	Name                  string
	StateMcpModulePath    string
	StateMcpModuleVersion string
}

// internal render payload: ScaffoldData + version consts.
type renderData struct {
	ScaffoldData
	GoVersion, FrameworkGoVersion, AppGeneratorVersion,
	HTTPGeneratorVersion, MCPGeneratorVersion, ProjectModelVersion string
}

var renderedPaths = map[string]string{ // dest path -> template asset
	".github/workflows/aiarch-design.yml":    "assets/workflows/aiarch-design.yml.tmpl",
	".github/workflows/aiarch-construct.yml": "assets/workflows/aiarch-construct.yml.tmpl",
	"go.mod":                                 "assets/scaffold/go.mod.tmpl",
	"aiarch_method_test.go":                  "assets/scaffold/aiarch_method_test.go.tmpl",
	".aiarch/state/project.json":             "assets/scaffold/project.json.tmpl",
}

// ScaffoldFiles renders the complete managed-scaffold file set for one app
// repo: workflows + go.mod + method test + seed state + the .claude tree.
func ScaffoldFiles(data ScaffoldData) (map[string][]byte, error) {
	out, err := ClaudeFiles()
	if err != nil {
		return nil, err
	}
	rd := renderData{ScaffoldData: data, GoVersion: GoVersion,
		FrameworkGoVersion: FrameworkGoVersion, AppGeneratorVersion: AppGeneratorVersion,
		HTTPGeneratorVersion: HTTPGeneratorVersion, MCPGeneratorVersion: MCPGeneratorVersion,
		ProjectModelVersion: ProjectModelVersion}
	for dest, asset := range renderedPaths {
		b, err := renderAsset(assetsFS, asset, rd)
		if err != nil {
			return nil, err
		}
		out[dest] = b
	}
	out["internal/.gitkeep"] = []byte("")
	return out, nil
}

func renderAsset(fsys embed.FS, name string, data renderData) ([]byte, error) {
	raw, err := fsys.ReadFile(name)
	if err != nil {
		return nil, err
	}
	t, err := template.New(name).Delims("[[", "]]").Parse(string(raw))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 4: Run tests, commit**

Run: `go test ./...` → Expected: PASS.

```bash
git add -A method-assets && git commit -m "feat(method-assets): ScaffoldFiles renderer + go.mod/project.json seeds"
```

---

### Task 10: Manifest-tracked materializer (`cmd/method-assets install`)

**Files:**
- Create: `method-assets/materialize.go`
- Create: `method-assets/cmd/method-assets/main.go`
- Test: `method-assets/materialize_test.go`

**Interfaces:**
- Produces: `func Materialize(destRepo string) error` (library) + CLI `method-assets install --dest <repo>`. Manifest at `<repo>/.claude/.method-assets-manifest.json`: `{"version": "<module version or dev>", "files": ["<sorted repo-relative paths>"]}`. Owns ONLY manifest-listed files: writes all `ClaudeFiles()`, deletes manifest-listed files absent from the new set, never touches unlisted files (grillme, settings, hooks survive).

- [ ] **Step 1: Write the failing test**

```go
// method-assets/materialize_test.go
package methodassets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializePreservesLocalExtrasAndPrunesOrphans(t *testing.T) {
	dir := t.TempDir()
	// Local extra the materializer must never touch.
	extra := filepath.Join(dir, ".claude", "skills", "grillme", "SKILL.md")
	os.MkdirAll(filepath.Dir(extra), 0o755)
	os.WriteFile(extra, []byte("local"), 0o644)

	if err := Materialize(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "agents", "system-architect.md")); err != nil {
		t.Fatal("expected agent not materialized")
	}
	if b, _ := os.ReadFile(extra); string(b) != "local" {
		t.Fatal("local extra clobbered")
	}

	// Simulate an asset removed in a future version: plant an owned orphan
	// by appending a fake entry to the manifest, then re-materialize.
	orphan := filepath.Join(dir, ".claude", "commands", "dead-command.md")
	os.WriteFile(orphan, []byte("stale"), 0o644)
	appendToManifest(t, dir, ".claude/commands/dead-command.md")
	if err := Materialize(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("owned orphan not pruned")
	}
	if b, _ := os.ReadFile(extra); string(b) != "local" {
		t.Fatal("local extra clobbered on re-run")
	}
}
```

(`appendToManifest` is a small test helper: read the manifest JSON, append the path to `files`, write it back — write it in the test file.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestMaterialize` → Expected: FAIL — `undefined: Materialize`.

- [ ] **Step 3: Implement**

```go
// method-assets/materialize.go
package methodassets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

const manifestPath = ".claude/.method-assets-manifest.json"

type manifest struct {
	Version string   `json:"version"`
	Files   []string `json:"files"`
}

// Materialize writes the embedded .claude tree into destRepo, prunes files
// the PREVIOUS manifest owned that no longer exist in the asset set, and
// rewrites the manifest. Files not listed in the manifest are never touched.
func Materialize(destRepo string) error {
	files, err := ClaudeFiles()
	if err != nil {
		return err
	}
	var prev manifest
	if b, err := os.ReadFile(filepath.Join(destRepo, manifestPath)); err == nil {
		_ = json.Unmarshal(b, &prev) // corrupt manifest = treat as empty
	}
	owned := map[string]bool{}
	for p, body := range files {
		owned[p] = true
		abs := filepath.Join(destRepo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, body, 0o644); err != nil {
			return err
		}
	}
	for _, p := range prev.Files {
		if !owned[p] {
			_ = os.Remove(filepath.Join(destRepo, filepath.FromSlash(p)))
		}
	}
	next := manifest{Version: moduleVersion(), Files: make([]string, 0, len(owned))}
	for p := range owned {
		next.Files = append(next.Files, p)
	}
	sort.Strings(next.Files)
	b, _ := json.MarshalIndent(next, "", "  ")
	return os.WriteFile(filepath.Join(destRepo, manifestPath), append(b, '\n'), 0o644)
}

// moduleVersion reports this module's version via build info ("(devel)" in tests).
func moduleVersion() string { return readBuildVersion() }
```

`readBuildVersion` (same file): use `runtime/debug.ReadBuildInfo()`, find the `method-assets` module dep or main module, return its `Version`, defaulting to `"devel"`.

```go
// method-assets/cmd/method-assets/main.go
package main

import (
	"flag"
	"fmt"
	"os"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "install" {
		fmt.Fprintln(os.Stderr, "usage: method-assets install --dest <repo>")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	dest := fs.String("dest", "", "target repo root")
	_ = fs.Parse(os.Args[2:])
	if *dest == "" {
		fmt.Fprintln(os.Stderr, "install: --dest is required")
		os.Exit(2)
	}
	if err := methodassets.Materialize(*dest); err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		os.Exit(1)
	}
	fmt.Println("materialized .claude into", *dest)
}
```

- [ ] **Step 4: Run tests + build the CLI, commit**

Run: `go test ./... && go build ./cmd/method-assets`
Expected: PASS, clean build.

```bash
git add -A method-assets && git commit -m "feat(method-assets): manifest-tracked materializer + install CLI"
```

---

### Task 11: Platform CI + release `method-assets/v0.1.0`

**Files:**
- Verify: `.github/workflows/platform-checks.yml` picks up the module (it builds all go.work modules; confirm by reading the matrix/step at lines ~23–41)

**Interfaces:**
- Produces: tag `method-assets/v0.1.0` — the pin Plan 2's server imports.

- [ ] **Step 1: Full local gate**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets
gofmt -l . && go vet ./... && go test ./...
```

Expected: no gofmt output, vet clean, all tests PASS.

- [ ] **Step 2: Confirm platform-checks covers the module** — read `.github/workflows/platform-checks.yml`; if its build/lint/test steps iterate go.work modules, nothing to do; if it hardcodes a module list, add `method-assets`.

- [ ] **Step 3: STOP — founder go-ahead required.** Pushing publishes the module (and every prompt in it) in the PUBLIC archistrator-platform repo. Confirm with the founder before this step.

- [ ] **Step 4: Push + tag**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
git push origin main
git tag method-assets/v0.1.0 && git push origin method-assets/v0.1.0
```

Expected: platform-checks green on the pushed commit; `go list -m github.com/mixofreality-studio/archistrator-platform/method-assets@v0.1.0` resolves from a scratch dir.

---

## Deferred to Plan 2 (server refactor — archistrator repo)

Recorded so nothing is lost: `DesignCommandFor(kind, mode)` + wire test against module assets; delete Go doctrine constants + `design_prompt` composition; `ManagedScaffoldFiles` → `methodassets.ScaffoldFiles`; `scaffoldRootPaths` allowlist expansion (`.claude/**`, `.aiarch/state/project.json`); construction `TargetRepo` switch to the app repo; root `.claude` materialization + CI drift gate; delete archistrator's now-duplicated `.claude` cruft (hooks/structurizr per spec §7); manager test updates; re-seat gtdapp. Plan 3 covers spec §9 (loading states).
