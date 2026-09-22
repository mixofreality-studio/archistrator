# Activity Experience — Stage 0 (Lifecycle Data + Read Contract) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the webApp a schema-stable, real-data read of any construction activity's task DAG with revisions (`constructionManager.QueryActivityView`), backed by platform-fixed lifecycle data in method-assets, and make review rosters real by fixing the reviewEngine `artifactKind` defect at its root.

**Architecture:** The per-activity-type task DAG becomes embedded data in the platform's method-assets module (`lifecycles.json` + loader + validator), pinned by the server and generated to `webApp/src/components/activity/lifecycles.gen.ts`; a parity test holds it equal to today's `ProfileFor`/`CommandFor` until stage 2 deletes those. `QueryActivityView` is a read-only op on the EXISTING constructionManager that derives tasks, states and revisions with pure functions over today's data (attempt ledger where present, otherwise episodes + send-back notes + stored completions + live session). Nothing stored changes shape; no Temporal workflow or activity is added, so no drain is needed.

**Tech Stack:** Go 1.26 (`GOWORK=off` always), method-assets (platform module, `//go:embed`), project.json contracts → modelgen/clientgen/appgen, React 19 + TypeScript, TanStack Query, `node:test`, preview fixtures validated by `webApp/scripts/fixture-schema.mjs`.

**Spec:** `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md` (§2 vocabulary, §3 lifecycles, §5.4 review engine, §8 stage 0). Executors read both.

## Global Constraints

- `GOWORK=off` on every `go`/`make` command in `server/` and in the platform modules. Gates run against PINNED platform tags, never a `replace`.
- Work in a git worktree of the app repo branched from `main` (the main checkout is shared with other sessions that switch branches). The platform repo work (Tasks 1–2) happens in `/Users/davidmarne/mixofrealitystudio/archistrator-platform` on a branch.
- **Task 2's tag + push is a STOP: it requires the founder's explicit confirmation.** Platform commits AND the annotated tag must be authored as `David Marne <davemarne@gmail.com>` (the platform repo-local identity is `uitests`; override with `--author` and `-c user.name= -c user.email=`).
- Never hand-edit state slots (9/10 move only via `make derived-plan-write`). `.serviceContracts` IS hand-edited, followed by the self-amendment gate loop: `make gen-models` → `make method-check` → `GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System` → `GOWORK=off make test-short` → `cd webApp && npm run check`.
- Never weaken, skip or allowlist around a gate. If a gate is red, fix the cause or stop and report.
- Never run `git restore`, `git clean`, `git stash`, `git checkout -- <path>` or any tree-wide reset. The SDD ledger under `.superpowers/sdd/` is gitignored — back it up to the session scratchpad after every append.
- `TestFileLayout`: one impl file + one file per workflow + ONE test file per package. No new `.go` files inside existing Manager/Engine/ResourceAccess packages.
- Lifecycle type key rule: `t.String()` for non-testing activity types, `"testing:" + v.String()` for testing (so QA is `testing:qaProcess`).
- The lifecycles data's per-task `artifactKind` has NO consumer until stage 2; in stage 0 the engine call uses Task 6's `(ActivityType, ActivityMethodPhase)` table only.
- Drift gates to run before every server commit that touches generated inputs: all `gen-*-check` targets in `server/Makefile`, including the new `gen-lifecycles-check`, plus `make sumtype-check` and `make derived-plan-check`.
- webApp layer DAG (routes → containers → components → hooks → api) is lint-enforced; `npm run check` must be green. `lifecycleTemplates.gen.ts` is NOT touched in this stage.
- Match the surrounding code's comment density, naming and idiom. Commit messages end with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- Out of scope: requirements / architecture / projectDesign activities do not exist as plan activities until stage 2 — `QueryActivityView` returns the not-found error for them.

## Task order

Tasks 1–5 (lifecycle data → release → pin + parity → generator → `lifecycles.gen.ts`) then Tasks 6–9 (review-kind fix → revision derivation → contract + façade → webApp hook + fixtures). Task 6 is independent of 1–5 and may run while Task 2 waits on the founder. Tasks 7–9 consume Task 3's pinned `methodassets.LifecycleFor`.

---

### Task 1: Author `lifecycles.json` in method-assets, with its loader and validator

**Repo:** `/Users/davidmarne/mixofrealitystudio/archistrator-platform` (absolute on purpose — the app side of this plan runs in a worktree, so `../archistrator-platform` does not resolve from there). Module `github.com/mixofreality-studio/archistrator-platform/method-assets`, `go 1.25.0`, current tag `method-assets/v0.8.0`.

**Decisions this task fixes (read before coding):**

- **D1 — plain `//go:embed` + parse at package init, NOT a `cmd/gen-*` generator.** `stepmanifest.gen.go` is generated because the manifest is *derived* from other assets (command bodies, charters, skill links) and the generator is the derivation. `lifecycles.json` is *authored* — it is the source of truth (spec R5), there is nothing to derive it from, and a generated Go mirror would only add a second copy plus a drift gate. The panic in `mustParseLifecycles` can only fire on a malformed embedded file, and `TestLifecycles_EveryShippedLifecycleIsValid` makes a tag with such a file unreleasable.
- **D2 — type keys are the app's own wire names.** `ActivityType.String()` for every type, and `"testing:" + TestingVariant.String()` for testing. That makes the key mechanical on both sides (Go and the webApp's `TestingVariantName`) with no mapping table. Consequence: the QA variant's key is **`testing:qaProcess`** (the wire name), not `testing:qa` (which is only the command-slug stem).
- **D3 — phase ids for the existing types are the canonical `ActivityMethodPhase` wire values** (`requirements`, `detailed_design`, `test_plan`, `construction`, `integration`), so `PhaseCompletion.Phase` in committed state keeps joining. Task ids are the ten non-conditional `MethodTask` ids, reused per phase exactly as `phaseTasks`/`gateTasks`/`AgentTaskFor` do today. `someConstruction` and `testClient` are NOT nodes (spec §3).
- **D4 — `LifecyclePhase` carries `exitCriterion`.** It is per-profile copy that lives in `profileRows` today (`ExitCriterionFor`); stage 2 deletes `profileRows`, so the words must already be in the data or they are lost. One field now beats a second platform release.
- **D5 — `workerClass` is the agent the task's command adopts** — it must equal `ManifestFor(command).Agent` (pinned by a test here). Construction review tasks carry no `command`/`workerClass`: who reviews is the review engine's decision (R8). Design review tasks carry today's critique command exactly where `DesignCommandFor(kind, DesignJobModeCritique, "")` is non-empty — so **`volatilitiesReview` has no command** even though a `volatilities-critique.md` file exists (`designKindHasCritique` excludes `KindVolatilities` today; no behaviour change in this stage).
- **D6 — `artifactKind` sits on dispatch tasks only** (a review judges its `reviews` target's artifact), except `projectDesign`'s gate, which has no dispatch to point at and so names `SdpReview` itself. Design kinds use `ArtifactKind.String()` (`Mission`, `Glossary`, `Volatilities`, `CoreUseCases`, `System`, `SdpReview`). Construction tasks use one profile-invariant name per task key: `srs→SRS`, `stp→STP`, `detailedDesign→DetailedDesign`, `construction→Construction`, `integration→Integration` — three of which are already `reviewEngine`'s `artifactKindByName` keys. Nothing consumes this field until stage 2.
- **D7 — acyclicity is enforced as an authored-order rule:** a task may depend only on a task that appears EARLIER in `tasks`. A cycle cannot be written with every edge pointing backwards, so the rule subsumes "acyclic", catches unknown ids and self-references with the same check, and is also the order the webApp's lane layout reads the trunk from (`lifecycleGraphLayout.ts`: "first-authored child inherits the source's lane"). The test file additionally runs an independent DFS cycle check over the shipped data.

**Files:**
- Create: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets/lifecycles.json`
- Create: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets/lifecycles.go`
- Create: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets/lifecycles_test.go`
- Untouched: `methodassets.go` (`//go:embed all:assets` embeds only `assets/`; the new file sits at the module root beside `stepmanifest.go` and gets its own embed directive), `stepmanifest.go`, `stepmanifest.gen.go`, `cmd/gen-stepmanifest`.

**Interfaces:**
- Consumes: `ManifestFor(command string) (StepManifest, bool)` (`stepmanifest.go:181`) — test only.
- Produces (package `methodassets`):
  ```go
  const LifecycleTaskDispatch = "dispatch"
  const LifecycleTaskReview   = "review"

  type LifecyclePhase struct {
      ID            string `json:"id"`
      Label         string `json:"label"`
      Weight        int    `json:"weight"`
      Gate          string `json:"gate"`
      ExitCriterion string `json:"exitCriterion"`
  }
  type LifecycleTask struct {
      ID           string   `json:"id"`
      Kind         string   `json:"kind"`
      Title        string   `json:"title"`
      Phase        string   `json:"phase"`
      DependsOn    []string `json:"dependsOn"`
      Reviews      string   `json:"reviews,omitempty"`
      Command      string   `json:"command,omitempty"`
      WorkerClass  string   `json:"workerClass,omitempty"`
      ArtifactKind string   `json:"artifactKind,omitempty"`
  }
  type Lifecycle struct {
      Type   string           `json:"type"`
      Phases []LifecyclePhase `json:"phases"`
      Tasks  []LifecycleTask  `json:"tasks"`
  }

  func LifecycleFor(typeKey string) (Lifecycle, bool) // deep copy; false for an unknown key
  func Lifecycles() []Lifecycle                       // deep copy, in lifecycles.json order
  func ValidateLifecycle(l Lifecycle) []string        // every structural defect, as sentences; empty = valid
  ```

- [ ] **Step 1: Verify first — the platform checkout is where this plan thinks it is.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
  git status --short            # expect: empty
  git branch --show-current     # expect: main
  git fetch origin && git status -sb | head -1   # expect: no "behind"
  git tag -l 'method-assets/*' | sort -V | tail -1   # expect: method-assets/v0.8.0
  ```
  A dirty tree, a branch other than `main`, or a `behind` count: stop and report. If the last tag is not `v0.8.0`, the release in Task 2 takes the next free MINOR after whatever is printed — substitute it everywhere `v0.9.0` appears in Tasks 2 and 3.

- [ ] **Step 2: Write the failing test.** Create `method-assets/lifecycles_test.go`:

  ```go
  package methodassets

  import (
  	"slices"
  	"strings"
  	"testing"
  )

  // wantLifecycleTypes is the closed, ordered set of activity-type keys. The keys
  // are the app's own wire names (ActivityType.String(), and "testing:" +
  // TestingVariant.String() for testing), so neither side needs a mapping table.
  var wantLifecycleTypes = []string{
  	"requirements", "architecture", "projectDesign",
  	"service", "frontend",
  	"testing:plan", "testing:harness", "testing:perf", "testing:systemTest", "testing:qaProcess",
  	"deployment", "documentation", "uiDesign", "integration",
  }

  // figureA1TaskIDs are the ten non-conditional Figure A-1 task keys the app's
  // append-only attempt ledger already uses (projectstate.MethodTask). The
  // construction-family lifecycles may use no other id; someConstruction and
  // testClient are conditional sub-attempts and are never nodes.
  var figureA1TaskIDs = []string{
  	"srs", "srsReview", "stp", "stpReview", "detailedDesign", "designReview",
  	"construction", "codeReview", "integration", "testing",
  }

  var designActivityTypes = []string{"requirements", "architecture", "projectDesign"}

  func TestLifecycles_ClosedOrderedTypeSet(t *testing.T) {
  	var got []string
  	for _, l := range Lifecycles() {
  		got = append(got, l.Type)
  	}
  	if !slices.Equal(got, wantLifecycleTypes) {
  		t.Fatalf("lifecycle types = %v, want %v", got, wantLifecycleTypes)
  	}
  	for _, key := range wantLifecycleTypes {
  		if _, ok := LifecycleFor(key); !ok {
  			t.Errorf("LifecycleFor(%q) not found", key)
  		}
  	}
  	if _, ok := LifecycleFor("nope"); ok {
  		t.Error("LifecycleFor must report false for an unknown type key")
  	}
  }

  func TestLifecycles_EveryShippedLifecycleIsValid(t *testing.T) {
  	for _, l := range Lifecycles() {
  		for _, problem := range ValidateLifecycle(l) {
  			t.Error(problem)
  		}
  	}
  }

  // An independent cycle check: ValidateLifecycle proves acyclicity through its
  // authored-order rule; this DFS proves it without relying on that rule.
  func TestLifecycles_Acyclic(t *testing.T) {
  	const visiting, done = 1, 2
  	for _, l := range Lifecycles() {
  		deps := map[string][]string{}
  		for _, task := range l.Tasks {
  			deps[task.ID] = task.DependsOn
  		}
  		state := map[string]int{}
  		var visit func(id string) bool
  		visit = func(id string) bool {
  			if state[id] == visiting {
  				return false
  			}
  			if state[id] == done {
  				return true
  			}
  			state[id] = visiting
  			for _, d := range deps[id] {
  				if !visit(d) {
  					return false
  				}
  			}
  			state[id] = done
  			return true
  		}
  		for id := range deps {
  			if !visit(id) {
  				t.Errorf("%s: dependency cycle through %q", l.Type, id)
  			}
  		}
  	}
  }

  func TestLifecycles_ServiceAndFrontendForkPerFigureA1(t *testing.T) {
  	want := map[string][]string{
  		"srs":            {},
  		"srsReview":      {"srs"},
  		"detailedDesign": {"srsReview"},
  		"designReview":   {"detailedDesign"},
  		"construction":   {"designReview"},
  		"codeReview":     {"construction"},
  		"integration":    {"codeReview"},
  		"stp":            {"srsReview"},
  		"stpReview":      {"stp"},
  		"testing":        {"integration", "stpReview"},
  	}
  	for _, key := range []string{"service", "frontend"} {
  		l, _ := LifecycleFor(key)
  		if len(l.Tasks) != len(want) {
  			t.Fatalf("%s: %d tasks, want %d", key, len(l.Tasks), len(want))
  		}
  		for _, task := range l.Tasks {
  			deps, ok := want[task.ID]
  			if !ok {
  				t.Errorf("%s: unexpected task %q (someConstruction and testClient are conditional sub-attempts, never nodes)", key, task.ID)
  				continue
  			}
  			if !slices.Equal(task.DependsOn, deps) {
  				t.Errorf("%s: %s dependsOn = %v, want %v", key, task.ID, task.DependsOn, deps)
  			}
  		}
  		// The trunk is authored first: the layout keeps the first-authored chain on lane 0.
  		if l.Tasks[2].ID != "detailedDesign" || l.Tasks[7].ID != "stp" {
  			t.Errorf("%s: the Detailed Design branch must be authored before the STP branch", key)
  		}
  	}
  }

  func TestLifecycles_LinearTypesAreOneDispatchAndOneGatePerPhase(t *testing.T) {
  	for _, l := range Lifecycles() {
  		if l.Type == "service" || l.Type == "frontend" || l.Type == "projectDesign" {
  			continue
  		}
  		if len(l.Tasks) != 2*len(l.Phases) {
  			t.Errorf("%s: %d tasks for %d phases, want exactly a dispatch and a review gate per phase", l.Type, len(l.Tasks), len(l.Phases))
  			continue
  		}
  		for i, task := range l.Tasks {
  			wantKind, wantDeps := LifecycleTaskDispatch, []string{}
  			if i%2 == 1 {
  				wantKind = LifecycleTaskReview
  			}
  			if i > 0 {
  				wantDeps = []string{l.Tasks[i-1].ID}
  			}
  			if task.Kind != wantKind || task.Phase != l.Phases[i/2].ID || !slices.Equal(task.DependsOn, wantDeps) {
  				t.Errorf("%s: task %d = %+v, want kind %s in phase %s depending on %v", l.Type, i, task, wantKind, l.Phases[i/2].ID, wantDeps)
  			}
  		}
  	}
  }

  func TestLifecycles_ConstructionFamilyReusesTheLedgerTaskIDs(t *testing.T) {
  	for _, l := range Lifecycles() {
  		if slices.Contains(designActivityTypes, l.Type) {
  			continue
  		}
  		for _, task := range l.Tasks {
  			if !slices.Contains(figureA1TaskIDs, task.ID) {
  				t.Errorf("%s: task id %q is not a Figure A-1 ledger key", l.Type, task.ID)
  			}
  		}
  	}
  }

  func TestLifecycles_DesignActivities(t *testing.T) {
  	req, _ := LifecycleFor("requirements")
  	var ids []string
  	var weights []int
  	for _, p := range req.Phases {
  		ids = append(ids, p.ID)
  		weights = append(weights, p.Weight)
  	}
  	if !slices.Equal(ids, []string{"mission", "glossary", "volatilities", "coreUseCases"}) {
  		t.Errorf("requirements phases = %v", ids)
  	}
  	if !slices.Equal(weights, []int{15, 20, 35, 30}) {
  		t.Errorf("requirements weights = %v, want 15/20/35/30", weights)
  	}

  	arch, _ := LifecycleFor("architecture")
  	if len(arch.Phases) != 1 || arch.Phases[0].Weight != 100 || len(arch.Tasks) != 2 {
  		t.Errorf("architecture must be one draft/review pair weighing 100, got %+v", arch)
  	}

  	pd, _ := LifecycleFor("projectDesign")
  	if len(pd.Tasks) != 1 {
  		t.Fatalf("projectDesign must be ONE task, got %d", len(pd.Tasks))
  	}
  	gate := pd.Tasks[0]
  	if gate.Kind != LifecycleTaskReview || gate.Command != "" || gate.Reviews != "" || len(gate.DependsOn) != 0 || gate.ArtifactKind != "SdpReview" {
  		t.Errorf("projectDesign's only task must be a command-less review of the computed SdpReview, got %+v", gate)
  	}
  }

  // A review that names no dispatch task is legal only where there is none to
  // name — and projectDesign is the only such lifecycle.
  func TestLifecycles_OnlyProjectDesignReviewsNothing(t *testing.T) {
  	for _, l := range Lifecycles() {
  		for _, task := range l.Tasks {
  			if task.Kind == LifecycleTaskReview && task.Reviews == "" && l.Type != "projectDesign" {
  				t.Errorf("%s: review task %q names no dispatch task", l.Type, task.ID)
  			}
  		}
  	}
  }

  // workerClass is not free text: it is the charter the task's command adopts.
  func TestLifecycles_CommandsAreDispatchableAndNameTheirAgent(t *testing.T) {
  	for _, l := range Lifecycles() {
  		for _, task := range l.Tasks {
  			if task.Command == "" {
  				if task.WorkerClass != "" {
  					t.Errorf("%s: task %q has a workerClass but no command", l.Type, task.ID)
  				}
  				continue
  			}
  			m, ok := ManifestFor(task.Command)
  			if !ok {
  				t.Errorf("%s: task %q names command %q, which has no step manifest", l.Type, task.ID, task.Command)
  				continue
  			}
  			if m.Agent != task.WorkerClass {
  				t.Errorf("%s: task %q workerClass = %q, but /%s adopts %q", l.Type, task.ID, task.WorkerClass, task.Command, m.Agent)
  			}
  		}
  	}
  }

  func TestLifecycles_ResultsAreCopies(t *testing.T) {
  	a, _ := LifecycleFor("service")
  	a.Phases[0].Weight = 99
  	a.Tasks[1].DependsOn[0] = "mutated"
  	b, _ := LifecycleFor("service")
  	if b.Phases[0].Weight != 15 || b.Tasks[1].DependsOn[0] != "srs" {
  		t.Errorf("a caller's mutation leaked into the package data: %+v", b)
  	}
  }

  func TestParseLifecycles_RejectsUnknownFields(t *testing.T) {
  	if _, err := parseLifecycles([]byte(`{"lifecycles":[{"type":"x","phases":[],"tasks":[],"wieght":1}]}`)); err == nil {
  		t.Error("an unknown field must be a parse error, so a typo cannot ship as a silently dropped value")
  	}
  }

  func validFixture() Lifecycle {
  	return Lifecycle{
  		Type: "fixture",
  		Phases: []LifecyclePhase{
  			{ID: "a", Label: "A", Weight: 60, Gate: "aReview", ExitCriterion: "A passes review"},
  			{ID: "b", Label: "B", Weight: 40, Gate: "bReview", ExitCriterion: "B passes review"},
  		},
  		Tasks: []LifecycleTask{
  			{ID: "aDraft", Kind: LifecycleTaskDispatch, Title: "Draft A", Phase: "a", DependsOn: []string{}, Command: "mission-draft", WorkerClass: "system-architect", ArtifactKind: "A"},
  			{ID: "aReview", Kind: LifecycleTaskReview, Title: "Review A", Phase: "a", DependsOn: []string{"aDraft"}, Reviews: "aDraft"},
  			{ID: "bDraft", Kind: LifecycleTaskDispatch, Title: "Draft B", Phase: "b", DependsOn: []string{"aReview"}, Command: "glossary-draft", WorkerClass: "system-architect", ArtifactKind: "B"},
  			{ID: "bReview", Kind: LifecycleTaskReview, Title: "Review B", Phase: "b", DependsOn: []string{"bDraft"}, Reviews: "bDraft"},
  		},
  	}
  }

  func TestValidateLifecycle_AcceptsTheFixtureAndAGateOnlyLifecycle(t *testing.T) {
  	if problems := ValidateLifecycle(validFixture()); len(problems) != 0 {
  		t.Fatalf("the valid fixture was rejected: %v", problems)
  	}
  	gateOnly := Lifecycle{
  		Type:   "gateOnly",
  		Phases: []LifecyclePhase{{ID: "g", Label: "G", Weight: 100, Gate: "gReview", ExitCriterion: "G is approved"}},
  		Tasks:  []LifecycleTask{{ID: "gReview", Kind: LifecycleTaskReview, Title: "Review G", Phase: "g", DependsOn: []string{}, ArtifactKind: "G"}},
  	}
  	if problems := ValidateLifecycle(gateOnly); len(problems) != 0 {
  		t.Fatalf("a lifecycle with no dispatch task may have a review that names none: %v", problems)
  	}
  }

  func TestValidateLifecycle_RejectsEachDefect(t *testing.T) {
  	cases := []struct {
  		name   string
  		mutate func(l *Lifecycle)
  		want   string
  	}{
  		{"cycle", func(l *Lifecycle) { l.Tasks[0].DependsOn = []string{"bReview"} }, "not an earlier task"},
  		{"self dependency", func(l *Lifecycle) { l.Tasks[1].DependsOn = []string{"aReview"} }, "not an earlier task"},
  		{"unknown dependency", func(l *Lifecycle) { l.Tasks[1].DependsOn = []string{"ghost"} }, "not an earlier task"},
  		{"duplicate task id", func(l *Lifecycle) { l.Tasks[2].ID = "aDraft" }, "empty or not unique"},
  		{"unknown kind", func(l *Lifecycle) { l.Tasks[0].Kind = "spike" }, "unknown kind"},
  		{"unknown phase", func(l *Lifecycle) { l.Tasks[0].Phase = "zzz" }, "unknown phase"},
  		{"dispatch without a command", func(l *Lifecycle) { l.Tasks[0].Command = "" }, "needs a command"},
  		{"two roots", func(l *Lifecycle) { l.Tasks[2].DependsOn = []string{} }, "root tasks"},
  		{"weights do not sum to 100", func(l *Lifecycle) { l.Phases[0].Weight = 50 }, "sum to 90"},
  		{"duplicate phase id", func(l *Lifecycle) { l.Phases[1].ID = "a" }, "phase id"},
  		{"gate is a dispatch task", func(l *Lifecycle) { l.Phases[0].Gate = "aDraft" }, "must be a review task in that phase"},
  		{"gate lives in another phase", func(l *Lifecycle) { l.Phases[0].Gate = "bReview" }, "must be a review task in that phase"},
  		{"no gate", func(l *Lifecycle) { l.Phases[0].Gate = "" }, "must be a review task in that phase"},
  		{"a phase task its gate does not wait for", func(l *Lifecycle) { l.Tasks[2].Phase = "a" }, "not upstream of its phase gate"},
  		{"reviews a non-ancestor", func(l *Lifecycle) { l.Tasks[1].Reviews = "bDraft" }, "not an ancestor dispatch task"},
  		{"reviews a review", func(l *Lifecycle) { l.Tasks[3].Reviews = "aReview" }, "not an ancestor dispatch task"},
  		{"review names nothing", func(l *Lifecycle) { l.Tasks[1].Reviews = "" }, "must name the dispatch task"},
  		{"dispatch claims to review", func(l *Lifecycle) { l.Tasks[2].Reviews = "aDraft" }, "only a review task"},
  	}
  	for _, c := range cases {
  		t.Run(c.name, func(t *testing.T) {
  			l := validFixture()
  			c.mutate(&l)
  			problems := ValidateLifecycle(l)
  			if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, c.want) }) {
  				t.Fatalf("problems = %v, want one containing %q", problems, c.want)
  			}
  		})
  	}
  }
  ```

- [ ] **Step 3: Run it; confirm it fails to compile.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets && GOWORK=off go test ./... 2>&1 | head -20
  ```
  Expected: `undefined: Lifecycles`, `undefined: LifecycleFor`, `undefined: ValidateLifecycle`, `undefined: Lifecycle`, `undefined: parseLifecycles` … and `FAIL … [build failed]`.

- [ ] **Step 4: Author the data.** Create `method-assets/lifecycles.json` with EXACTLY this content (weights, labels, task titles and exit criteria for the eleven existing profiles are copied verbatim from `profileRows`/`serviceRows`/`testPlanRows`/`profileRowsForTestingVariant` in `server/internal/resourceaccess/projectstate/projectstateaccess.go` ~L7975–8072; commands are `profileSlug + "-" + kebabPhase`, ~L8726–8775; worker classes are each command's `Agent` in `stepmanifest.gen.go`):

  ```json
  {
    "lifecycles": [
      {
        "type": "requirements",
        "phases": [
          {"id": "mission", "label": "Mission", "weight": 15, "gate": "missionReview", "exitCriterion": "The mission statement is written and passes review"},
          {"id": "glossary", "label": "Glossary", "weight": 20, "gate": "glossaryReview", "exitCriterion": "The glossary is written and passes review"},
          {"id": "volatilities", "label": "Volatilities", "weight": 35, "gate": "volatilitiesReview", "exitCriterion": "The areas of volatility are identified and pass review"},
          {"id": "coreUseCases", "label": "Core Use Cases", "weight": 30, "gate": "coreUseCasesReview", "exitCriterion": "The core use cases are identified and pass review"}
        ],
        "tasks": [
          {"id": "missionDraft", "kind": "dispatch", "title": "Draft Mission", "phase": "mission", "dependsOn": [], "command": "mission-draft", "workerClass": "system-architect", "artifactKind": "Mission"},
          {"id": "missionReview", "kind": "review", "title": "Review Mission", "phase": "mission", "dependsOn": ["missionDraft"], "reviews": "missionDraft", "command": "mission-critique", "workerClass": "product-manager"},
          {"id": "glossaryDraft", "kind": "dispatch", "title": "Draft Glossary", "phase": "glossary", "dependsOn": ["missionReview"], "command": "glossary-draft", "workerClass": "system-architect", "artifactKind": "Glossary"},
          {"id": "glossaryReview", "kind": "review", "title": "Review Glossary", "phase": "glossary", "dependsOn": ["glossaryDraft"], "reviews": "glossaryDraft", "command": "glossary-critique", "workerClass": "product-manager"},
          {"id": "volatilitiesDraft", "kind": "dispatch", "title": "Draft Volatilities", "phase": "volatilities", "dependsOn": ["glossaryReview"], "command": "volatilities-draft", "workerClass": "system-architect", "artifactKind": "Volatilities"},
          {"id": "volatilitiesReview", "kind": "review", "title": "Review Volatilities", "phase": "volatilities", "dependsOn": ["volatilitiesDraft"], "reviews": "volatilitiesDraft"},
          {"id": "coreUseCasesDraft", "kind": "dispatch", "title": "Draft Core Use Cases", "phase": "coreUseCases", "dependsOn": ["volatilitiesReview"], "command": "core-use-cases-draft", "workerClass": "system-architect", "artifactKind": "CoreUseCases"},
          {"id": "coreUseCasesReview", "kind": "review", "title": "Review Core Use Cases", "phase": "coreUseCases", "dependsOn": ["coreUseCasesDraft"], "reviews": "coreUseCasesDraft", "command": "core-use-cases-critique", "workerClass": "product-manager"}
        ]
      },
      {
        "type": "architecture",
        "phases": [
          {"id": "architecture", "label": "Architecture", "weight": 100, "gate": "architectureReview", "exitCriterion": "The architecture and its call chains are drawn and pass review"}
        ],
        "tasks": [
          {"id": "architectureDraft", "kind": "dispatch", "title": "Draft Architecture & Call Chains", "phase": "architecture", "dependsOn": [], "command": "system-draft", "workerClass": "system-architect", "artifactKind": "System"},
          {"id": "architectureReview", "kind": "review", "title": "Review Architecture", "phase": "architecture", "dependsOn": ["architectureDraft"], "reviews": "architectureDraft", "command": "system-critique", "workerClass": "system-architect"}
        ]
      },
      {
        "type": "projectDesign",
        "phases": [
          {"id": "sdp", "label": "SDP Review · M0", "weight": 100, "gate": "sdpReview", "exitCriterion": "The derived plan and its cost are approved — M0"}
        ],
        "tasks": [
          {"id": "sdpReview", "kind": "review", "title": "SDP Review · M0", "phase": "sdp", "dependsOn": [], "artifactKind": "SdpReview"}
        ]
      },
      {
        "type": "service",
        "phases": [
          {"id": "requirements", "label": "Requirements", "weight": 15, "gate": "srsReview", "exitCriterion": "The SRS is written and passes SRS review"},
          {"id": "detailed_design", "label": "Detailed Design", "weight": 20, "gate": "designReview", "exitCriterion": "The service contract is designed and passes design review"},
          {"id": "test_plan", "label": "Test Plan", "weight": 10, "gate": "stpReview", "exitCriterion": "This component's test plan is written and passes STP review"},
          {"id": "construction", "label": "Construction", "weight": 40, "gate": "codeReview", "exitCriterion": "The code is written and passes code review, not merely checked in"},
          {"id": "integration", "label": "Integration", "weight": 15, "gate": "testing", "exitCriterion": "The component is integrated and its tests pass"}
        ],
        "tasks": [
          {"id": "srs", "kind": "dispatch", "title": "SRS", "phase": "requirements", "dependsOn": [], "command": "service-requirements", "workerClass": "senior-developer", "artifactKind": "SRS"},
          {"id": "srsReview", "kind": "review", "title": "SRS Review", "phase": "requirements", "dependsOn": ["srs"], "reviews": "srs"},
          {"id": "detailedDesign", "kind": "dispatch", "title": "Detailed Design", "phase": "detailed_design", "dependsOn": ["srsReview"], "command": "service-detailed-design", "workerClass": "senior-developer", "artifactKind": "DetailedDesign"},
          {"id": "designReview", "kind": "review", "title": "Design Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign"},
          {"id": "construction", "kind": "dispatch", "title": "Construction", "phase": "construction", "dependsOn": ["designReview"], "command": "service-construction", "workerClass": "junior-developer", "artifactKind": "Construction"},
          {"id": "codeReview", "kind": "review", "title": "Code Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction"},
          {"id": "integration", "kind": "dispatch", "title": "Integration", "phase": "integration", "dependsOn": ["codeReview"], "command": "service-integration", "workerClass": "system-architect", "artifactKind": "Integration"},
          {"id": "stp", "kind": "dispatch", "title": "STP", "phase": "test_plan", "dependsOn": ["srsReview"], "command": "service-test-plan", "workerClass": "test-engineer", "artifactKind": "STP"},
          {"id": "stpReview", "kind": "review", "title": "STP Review", "phase": "test_plan", "dependsOn": ["stp"], "reviews": "stp"},
          {"id": "testing", "kind": "review", "title": "Testing", "phase": "integration", "dependsOn": ["integration", "stpReview"], "reviews": "integration"}
        ]
      },
      {
        "type": "frontend",
        "phases": [
          {"id": "requirements", "label": "UX Requirements", "weight": 15, "gate": "srsReview", "exitCriterion": "The UX requirements for this surface are written and pass review"},
          {"id": "detailed_design", "label": "Design", "weight": 25, "gate": "designReview", "exitCriterion": "The UI design for this surface is drawn and passes design review"},
          {"id": "test_plan", "label": "Flows", "weight": 10, "gate": "stpReview", "exitCriterion": "The user flows this surface must support are written as tests and pass review"},
          {"id": "construction", "label": "Construction", "weight": 35, "gate": "codeReview", "exitCriterion": "The surface is built and passes code review, not merely checked in"},
          {"id": "integration", "label": "Integration", "weight": 15, "gate": "testing", "exitCriterion": "The surface is wired to its managers and its flows pass against the integrated system"}
        ],
        "tasks": [
          {"id": "srs", "kind": "dispatch", "title": "UX Requirements", "phase": "requirements", "dependsOn": [], "command": "frontend-requirements", "workerClass": "ui-designer", "artifactKind": "SRS"},
          {"id": "srsReview", "kind": "review", "title": "UX Requirements Review", "phase": "requirements", "dependsOn": ["srs"], "reviews": "srs"},
          {"id": "detailedDesign", "kind": "dispatch", "title": "Design", "phase": "detailed_design", "dependsOn": ["srsReview"], "command": "frontend-detailed-design", "workerClass": "ui-designer", "artifactKind": "DetailedDesign"},
          {"id": "designReview", "kind": "review", "title": "Design Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign"},
          {"id": "construction", "kind": "dispatch", "title": "Construction", "phase": "construction", "dependsOn": ["designReview"], "command": "frontend-construction", "workerClass": "junior-developer", "artifactKind": "Construction"},
          {"id": "codeReview", "kind": "review", "title": "Code Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction"},
          {"id": "integration", "kind": "dispatch", "title": "Integration", "phase": "integration", "dependsOn": ["codeReview"], "command": "frontend-integration", "workerClass": "system-architect", "artifactKind": "Integration"},
          {"id": "stp", "kind": "dispatch", "title": "Flows", "phase": "test_plan", "dependsOn": ["srsReview"], "command": "frontend-test-plan", "workerClass": "test-engineer", "artifactKind": "STP"},
          {"id": "stpReview", "kind": "review", "title": "Flow Review", "phase": "test_plan", "dependsOn": ["stp"], "reviews": "stp"},
          {"id": "testing", "kind": "review", "title": "Flow Testing", "phase": "integration", "dependsOn": ["integration", "stpReview"], "reviews": "integration"}
        ]
      },
      {
        "type": "testing:plan",
        "phases": [
          {"id": "requirements", "label": "Use-Case Trace", "weight": 20, "gate": "srsReview", "exitCriterion": "Every core use case is traced to the scenarios that will exercise it, and the trace passes review"},
          {"id": "construction", "label": "Plan Authoring", "weight": 45, "gate": "codeReview", "exitCriterion": "The black-box scenarios are written and pass scenario review"},
          {"id": "integration", "label": "Plan Review", "weight": 35, "gate": "testing", "exitCriterion": "The assembled system test plan passes review and is signed off"}
        ],
        "tasks": [
          {"id": "srs", "kind": "dispatch", "title": "Use-Case Trace", "phase": "requirements", "dependsOn": [], "command": "testing-plan-requirements", "workerClass": "test-engineer", "artifactKind": "SRS"},
          {"id": "srsReview", "kind": "review", "title": "Trace Review", "phase": "requirements", "dependsOn": ["srs"], "reviews": "srs"},
          {"id": "construction", "kind": "dispatch", "title": "Plan Authoring", "phase": "construction", "dependsOn": ["srsReview"], "command": "testing-plan-construction", "workerClass": "test-engineer", "artifactKind": "Construction"},
          {"id": "codeReview", "kind": "review", "title": "Scenario Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction"},
          {"id": "integration", "kind": "dispatch", "title": "Plan Assembly", "phase": "integration", "dependsOn": ["codeReview"], "command": "testing-plan-integration", "workerClass": "qa-engineer", "artifactKind": "Integration"},
          {"id": "testing", "kind": "review", "title": "Plan Review", "phase": "integration", "dependsOn": ["integration"], "reviews": "integration"}
        ]
      },
      {
        "type": "testing:harness",
        "phases": [
          {"id": "detailed_design", "label": "Harness Design", "weight": 15, "gate": "designReview", "exitCriterion": "The harness design is drawn and passes design review"},
          {"id": "construction", "label": "Harness Construction", "weight": 70, "gate": "codeReview", "exitCriterion": "The harness is built and passes code review"},
          {"id": "integration", "label": "Harness Review", "weight": 15, "gate": "testing", "exitCriterion": "The harness drives the plan's scenarios against the integrated system and passes review"}
        ],
        "tasks": [
          {"id": "detailedDesign", "kind": "dispatch", "title": "Harness Design", "phase": "detailed_design", "dependsOn": [], "command": "testing-harness-detailed-design", "workerClass": "test-engineer", "artifactKind": "DetailedDesign"},
          {"id": "designReview", "kind": "review", "title": "Design Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign"},
          {"id": "construction", "kind": "dispatch", "title": "Harness Construction", "phase": "construction", "dependsOn": ["designReview"], "command": "testing-harness-construction", "workerClass": "test-engineer", "artifactKind": "Construction"},
          {"id": "codeReview", "kind": "review", "title": "Code Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction"},
          {"id": "integration", "kind": "dispatch", "title": "Harness Integration", "phase": "integration", "dependsOn": ["codeReview"], "command": "testing-harness-integration", "workerClass": "qa-engineer", "artifactKind": "Integration"},
          {"id": "testing", "kind": "review", "title": "Harness Review", "phase": "integration", "dependsOn": ["integration"], "reviews": "integration"}
        ]
      },
      {
        "type": "testing:perf",
        "phases": [
          {"id": "detailed_design", "label": "Perf Scenario Design", "weight": 25, "gate": "designReview", "exitCriterion": "The performance scenarios and their targets are designed and pass review"},
          {"id": "construction", "label": "Rig Construction", "weight": 50, "gate": "codeReview", "exitCriterion": "The performance rig is built and passes code review"},
          {"id": "integration", "label": "Rig Review", "weight": 25, "gate": "testing", "exitCriterion": "The rig runs against the integrated system and its results pass review"}
        ],
        "tasks": [
          {"id": "detailedDesign", "kind": "dispatch", "title": "Perf Scenario Design", "phase": "detailed_design", "dependsOn": [], "command": "testing-perf-detailed-design", "workerClass": "test-engineer", "artifactKind": "DetailedDesign"},
          {"id": "designReview", "kind": "review", "title": "Scenario Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign"},
          {"id": "construction", "kind": "dispatch", "title": "Rig Construction", "phase": "construction", "dependsOn": ["designReview"], "command": "testing-perf-construction", "workerClass": "test-engineer", "artifactKind": "Construction"},
          {"id": "codeReview", "kind": "review", "title": "Code Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction"},
          {"id": "integration", "kind": "dispatch", "title": "Rig Integration", "phase": "integration", "dependsOn": ["codeReview"], "command": "testing-perf-integration", "workerClass": "qa-engineer", "artifactKind": "Integration"},
          {"id": "testing", "kind": "review", "title": "Rig Review", "phase": "integration", "dependsOn": ["integration"], "reviews": "integration"}
        ]
      },
      {
        "type": "testing:systemTest",
        "phases": [
          {"id": "requirements", "label": "Smoke Pass", "weight": 10, "gate": "srsReview", "exitCriterion": "A smoke pass runs over the integrated build and shows it is testable"},
          {"id": "construction", "label": "Use-Case Execution", "weight": 45, "gate": "codeReview", "exitCriterion": "Every use case has run against the real build and its results are reviewed"},
          {"id": "integration", "label": "Regression & Sign-off", "weight": 45, "gate": "testing", "exitCriterion": "The regression run is green and the system test is signed off"}
        ],
        "tasks": [
          {"id": "srs", "kind": "dispatch", "title": "Smoke Pass", "phase": "requirements", "dependsOn": [], "command": "testing-systemtest-requirements", "workerClass": "software-tester", "artifactKind": "SRS"},
          {"id": "srsReview", "kind": "review", "title": "Testability Check", "phase": "requirements", "dependsOn": ["srs"], "reviews": "srs"},
          {"id": "construction", "kind": "dispatch", "title": "Use-Case Execution", "phase": "construction", "dependsOn": ["srsReview"], "command": "testing-systemtest-construction", "workerClass": "software-tester", "artifactKind": "Construction"},
          {"id": "codeReview", "kind": "review", "title": "Results Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction"},
          {"id": "integration", "kind": "dispatch", "title": "Regression", "phase": "integration", "dependsOn": ["codeReview"], "command": "testing-systemtest-integration", "workerClass": "software-tester", "artifactKind": "Integration"},
          {"id": "testing", "kind": "review", "title": "Sign-off", "phase": "integration", "dependsOn": ["integration"], "reviews": "integration"}
        ]
      },
      {
        "type": "testing:qaProcess",
        "phases": [
          {"id": "detailed_design", "label": "Gate Definition", "weight": 40, "gate": "designReview", "exitCriterion": "The quality gates are defined and pass review"},
          {"id": "construction", "label": "Process Audit", "weight": 60, "gate": "codeReview", "exitCriterion": "The process audit is done and its findings pass review"}
        ],
        "tasks": [
          {"id": "detailedDesign", "kind": "dispatch", "title": "Gate Definition", "phase": "detailed_design", "dependsOn": [], "command": "testing-qa-detailed-design", "workerClass": "qa-engineer", "artifactKind": "DetailedDesign"},
          {"id": "designReview", "kind": "review", "title": "Gate Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign"},
          {"id": "construction", "kind": "dispatch", "title": "Process Audit", "phase": "construction", "dependsOn": ["designReview"], "command": "testing-qa-construction", "workerClass": "qa-engineer", "artifactKind": "Construction"},
          {"id": "codeReview", "kind": "review", "title": "Audit Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction"}
        ]
      },
      {
        "type": "deployment",
        "phases": [
          {"id": "detailed_design", "label": "Provisioning Spec", "weight": 25, "gate": "designReview", "exitCriterion": "The provisioning spec is written and passes review"},
          {"id": "construction", "label": "Construction", "weight": 50, "gate": "codeReview", "exitCriterion": "The infrastructure change is built and passes review"},
          {"id": "integration", "label": "Convergence Verification", "weight": 25, "gate": "testing", "exitCriterion": "The rollout converges on the desired state and is verified"}
        ],
        "tasks": [
          {"id": "detailedDesign", "kind": "dispatch", "title": "Provisioning Spec", "phase": "detailed_design", "dependsOn": [], "command": "deployment-detailed-design", "workerClass": "project-manager", "artifactKind": "DetailedDesign"},
          {"id": "designReview", "kind": "review", "title": "Spec Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign"},
          {"id": "construction", "kind": "dispatch", "title": "Construction", "phase": "construction", "dependsOn": ["designReview"], "command": "deployment-construction", "workerClass": "junior-developer", "artifactKind": "Construction"},
          {"id": "codeReview", "kind": "review", "title": "Change Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction"},
          {"id": "integration", "kind": "dispatch", "title": "Rollout", "phase": "integration", "dependsOn": ["codeReview"], "command": "deployment-integration", "workerClass": "system-architect", "artifactKind": "Integration"},
          {"id": "testing", "kind": "review", "title": "Convergence Verification", "phase": "integration", "dependsOn": ["integration"], "reviews": "integration"}
        ]
      },
      {
        "type": "documentation",
        "phases": [
          {"id": "detailed_design", "label": "Outline", "weight": 20, "gate": "designReview", "exitCriterion": "The outline is written and passes review"},
          {"id": "construction", "label": "Authoring", "weight": 60, "gate": "codeReview", "exitCriterion": "The document is written and passes editorial review"},
          {"id": "integration", "label": "Doc Review", "weight": 20, "gate": "testing", "exitCriterion": "The document is published beside the system it describes and signed off"}
        ],
        "tasks": [
          {"id": "detailedDesign", "kind": "dispatch", "title": "Outline", "phase": "detailed_design", "dependsOn": [], "command": "documentation-detailed-design", "workerClass": "system-architect", "artifactKind": "DetailedDesign"},
          {"id": "designReview", "kind": "review", "title": "Outline Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign"},
          {"id": "construction", "kind": "dispatch", "title": "Authoring", "phase": "construction", "dependsOn": ["designReview"], "command": "documentation-construction", "workerClass": "system-architect", "artifactKind": "Construction"},
          {"id": "codeReview", "kind": "review", "title": "Editorial Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction"},
          {"id": "integration", "kind": "dispatch", "title": "Publishing", "phase": "integration", "dependsOn": ["codeReview"], "command": "documentation-integration", "workerClass": "system-architect", "artifactKind": "Integration"},
          {"id": "testing", "kind": "review", "title": "Doc Review", "phase": "integration", "dependsOn": ["integration"], "reviews": "integration"}
        ]
      },
      {
        "type": "uiDesign",
        "phases": [
          {"id": "requirements", "label": "UX Requirements", "weight": 40, "gate": "srsReview", "exitCriterion": "The UX requirements are written and pass review"},
          {"id": "detailed_design", "label": "Design Concept", "weight": 60, "gate": "designReview", "exitCriterion": "The UI design concept is produced and passes concept review"}
        ],
        "tasks": [
          {"id": "srs", "kind": "dispatch", "title": "UX Requirements", "phase": "requirements", "dependsOn": [], "command": "frontend-requirements", "workerClass": "ui-designer", "artifactKind": "SRS"},
          {"id": "srsReview", "kind": "review", "title": "UX Requirements Review", "phase": "requirements", "dependsOn": ["srs"], "reviews": "srs"},
          {"id": "detailedDesign", "kind": "dispatch", "title": "Design Concept", "phase": "detailed_design", "dependsOn": ["srsReview"], "command": "frontend-detailed-design", "workerClass": "ui-designer", "artifactKind": "DetailedDesign"},
          {"id": "designReview", "kind": "review", "title": "Concept Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign"}
        ]
      },
      {
        "type": "integration",
        "phases": [
          {"id": "integration", "label": "Integration", "weight": 100, "gate": "testing", "exitCriterion": "The components are integrated and their integration tests pass"}
        ],
        "tasks": [
          {"id": "integration", "kind": "dispatch", "title": "Integration", "phase": "integration", "dependsOn": [], "command": "service-integration", "workerClass": "system-architect", "artifactKind": "Integration"},
          {"id": "testing", "kind": "review", "title": "Integration Testing", "phase": "integration", "dependsOn": ["integration"], "reviews": "integration"}
        ]
      }
    ]
  }
  ```

- [ ] **Step 5: Write the loader and the validator.** Create `method-assets/lifecycles.go`. The platform lint gate is stricter than the app's (`gocyclo`/`gocognit` 10, `funlen` 60 lines / 40 statements, `nestif` 4), which is why the validator is a small struct with one short method per rule:

  ```go
  package methodassets

  // lifecycles.go — THE PER-ACTIVITY-TYPE TASK DAG, as platform-fixed data.
  //
  // An activity advances through its own little life cycle (Righting Software,
  // Appendix A): a DAG of TASKS, each either an agentic DISPATCH that produces an
  // artifact or a REVIEW that judges one, grouped into LIFECYCLE PHASES that each
  // carry an earned-value weight and a binary exit criterion — their gate task.
  //
  // lifecycles.json is the source of truth and is AUTHORED, not derived, so —
  // unlike stepmanifest.gen.go — there is no generator: the file is embedded and
  // parsed once at package init. lifecycles_test.go makes a release with a
  // malformed or structurally invalid file impossible, which is what makes the
  // init-time panic below unreachable in a tagged build.
  //
  // Type keys are the consuming app's own wire names: its ActivityType name, and
  // "testing:<TestingVariant name>" for the testing variants.

  import (
  	"bytes"
  	_ "embed" // go:embed lifecycles.json
  	"encoding/json"
  	"fmt"
  	"slices"
  )

  //go:embed lifecycles.json
  var lifecyclesJSON []byte

  // The two kinds of task. A dispatch produces an artifact; a review judges one.
  const (
  	LifecycleTaskDispatch = "dispatch"
  	LifecycleTaskReview   = "review"
  )

  // LifecyclePhase is a Figure A-2 grouping of tasks: the earned-value unit.
  type LifecyclePhase struct {
  	ID    string `json:"id"`
  	Label string `json:"label"`
  	// Weight is the phase's Table A-1 share, in percent; a lifecycle's sum to 100.
  	Weight int `json:"weight"`
  	// Gate is the id of the review task whose success IS this phase's exit.
  	Gate string `json:"gate"`
  	// ExitCriterion states that exit as one sentence, in this lifecycle's words.
  	ExitCriterion string `json:"exitCriterion"`
  }

  // LifecycleTask is one node of a lifecycle's DAG.
  type LifecycleTask struct {
  	ID    string `json:"id"`
  	Kind  string `json:"kind"`
  	Title string `json:"title"`
  	// Phase is the LifecyclePhase.ID this task belongs to.
  	Phase string `json:"phase"`
  	// DependsOn names the tasks this one waits for. Each must appear EARLIER in
  	// Lifecycle.Tasks — see ValidateLifecycle.
  	DependsOn []string `json:"dependsOn"`
  	// Reviews names the dispatch task a review judges; that pair is what a
  	// send-back re-opens. Empty on a dispatch task.
  	Reviews string `json:"reviews,omitempty"`
  	// Command is the slash-command slug an agent runs for this task, if any.
  	Command string `json:"command,omitempty"`
  	// WorkerClass is the agent charter Command adopts (ManifestFor(Command).Agent).
  	WorkerClass string `json:"workerClass,omitempty"`
  	// ArtifactKind names what a dispatch produces (and what a review with no
  	// Reviews target judges).
  	ArtifactKind string `json:"artifactKind,omitempty"`
  }

  // Lifecycle is the task DAG every activity of one type walks.
  type Lifecycle struct {
  	Type   string           `json:"type"`
  	Phases []LifecyclePhase `json:"phases"`
  	Tasks  []LifecycleTask  `json:"tasks"`
  }

  type lifecyclesFile struct {
  	Lifecycles []Lifecycle `json:"lifecycles"`
  }

  var lifecycles = mustParseLifecycles(lifecyclesJSON)

  func mustParseLifecycles(raw []byte) []Lifecycle {
  	out, err := parseLifecycles(raw)
  	if err != nil {
  		panic("method-assets: embedded lifecycles.json: " + err.Error())
  	}
  	return out
  }

  // parseLifecycles decodes the data file strictly: an unknown field is an error,
  // so a typo cannot ship as a silently dropped value.
  func parseLifecycles(raw []byte) ([]Lifecycle, error) {
  	dec := json.NewDecoder(bytes.NewReader(raw))
  	dec.DisallowUnknownFields()
  	var file lifecyclesFile
  	if err := dec.Decode(&file); err != nil {
  		return nil, err
  	}
  	return file.Lifecycles, nil
  }

  // LifecycleFor returns the lifecycle for an activity-type key. The result is a
  // deep copy; callers may mutate it.
  func LifecycleFor(typeKey string) (Lifecycle, bool) {
  	for _, l := range lifecycles {
  		if l.Type == typeKey {
  			return l.clone(), true
  		}
  	}
  	return Lifecycle{}, false
  }

  // Lifecycles returns every lifecycle in lifecycles.json order. The result is a
  // deep copy; callers may mutate it.
  func Lifecycles() []Lifecycle {
  	out := make([]Lifecycle, len(lifecycles))
  	for i, l := range lifecycles {
  		out[i] = l.clone()
  	}
  	return out
  }

  func (l Lifecycle) clone() Lifecycle {
  	c := Lifecycle{Type: l.Type, Phases: slices.Clone(l.Phases), Tasks: make([]LifecycleTask, len(l.Tasks))}
  	for i, t := range l.Tasks {
  		t.DependsOn = slices.Clone(t.DependsOn)
  		c.Tasks[i] = t
  	}
  	return c
  }

  // ValidateLifecycle returns every structural defect of one lifecycle as a
  // sentence prefixed with its type key; an empty result means it is valid.
  //
  // The rules:
  //   - task ids are non-empty and unique; kinds are dispatch or review; every
  //     task names a declared phase; a dispatch carries command, workerClass and
  //     artifactKind;
  //   - a task depends only on tasks authored EARLIER. A cycle cannot be written
  //     with every edge pointing backwards, so this one rule is acyclicity, and
  //     it rejects unknown ids and self-references on the way;
  //   - there is exactly one root task;
  //   - phase ids are unique, weights are positive and sum to 100, and every
  //     phase's gate is a review task IN that phase which every other task of
  //     the phase is upstream of — so "gate passed" means "phase complete";
  //   - a review names (Reviews) a dispatch task it transitively depends on. Only
  //     a lifecycle with no dispatch task at all may have a review that names
  //     none, and that review then carries the artifactKind itself.
  func ValidateLifecycle(l Lifecycle) []string {
  	v := newLifecycleValidator(l)
  	v.checkTasks()
  	v.checkRoots()
  	v.checkPhases()
  	v.checkReviews()
  	return v.problems
  }

  type lifecycleValidator struct {
  	l        Lifecycle
  	phaseIDs map[string]bool
  	tasks    map[string]LifecycleTask
  	// ancestors[id] is every task id transitively depends on. An entry exists
  	// only for a task already accepted, which is what "earlier" is checked by.
  	ancestors map[string]map[string]bool
  	problems  []string
  }

  func newLifecycleValidator(l Lifecycle) *lifecycleValidator {
  	v := &lifecycleValidator{
  		l:         l,
  		phaseIDs:  map[string]bool{},
  		tasks:     map[string]LifecycleTask{},
  		ancestors: map[string]map[string]bool{},
  	}
  	for _, p := range l.Phases {
  		v.phaseIDs[p.ID] = true
  	}
  	return v
  }

  func (v *lifecycleValidator) addf(format string, args ...any) {
  	v.problems = append(v.problems, v.l.Type+": "+fmt.Sprintf(format, args...))
  }

  func (v *lifecycleValidator) checkTasks() {
  	for _, t := range v.l.Tasks {
  		if _, dup := v.tasks[t.ID]; dup || t.ID == "" {
  			v.addf("task id %q is empty or not unique", t.ID)
  			continue
  		}
  		v.checkTaskFields(t)
  		v.ancestors[t.ID] = v.ancestorsOf(t)
  		v.tasks[t.ID] = t
  	}
  }

  func (v *lifecycleValidator) checkTaskFields(t LifecycleTask) {
  	if t.Kind != LifecycleTaskDispatch && t.Kind != LifecycleTaskReview {
  		v.addf("task %q has unknown kind %q", t.ID, t.Kind)
  	}
  	if !v.phaseIDs[t.Phase] {
  		v.addf("task %q names unknown phase %q", t.ID, t.Phase)
  	}
  	if t.Title == "" {
  		v.addf("task %q has no title", t.ID)
  	}
  	if t.Kind == LifecycleTaskDispatch && !dispatchIsComplete(t) {
  		v.addf("dispatch task %q needs a command, a workerClass and an artifactKind", t.ID)
  	}
  }

  func dispatchIsComplete(t LifecycleTask) bool {
  	return t.Command != "" && t.WorkerClass != "" && t.ArtifactKind != ""
  }

  func (v *lifecycleValidator) ancestorsOf(t LifecycleTask) map[string]bool {
  	out := map[string]bool{}
  	for _, dep := range t.DependsOn {
  		upstream, earlier := v.ancestors[dep]
  		if !earlier {
  			v.addf("task %q depends on %q, which is not an earlier task (an unknown id, a self or forward reference, or a cycle)", t.ID, dep)
  			continue
  		}
  		out[dep] = true
  		for a := range upstream {
  			out[a] = true
  		}
  	}
  	return out
  }

  func (v *lifecycleValidator) checkRoots() {
  	roots := 0
  	for _, t := range v.l.Tasks {
  		if len(t.DependsOn) == 0 {
  			roots++
  		}
  	}
  	if roots != 1 {
  		v.addf("%d root tasks, want exactly 1", roots)
  	}
  }

  func (v *lifecycleValidator) checkPhases() {
  	total := 0
  	seen := map[string]bool{}
  	for _, p := range v.l.Phases {
  		if p.ID == "" || seen[p.ID] {
  			v.addf("phase id %q is empty or not unique", p.ID)
  		}
  		if p.Weight <= 0 {
  			v.addf("phase %q has weight %d, want a positive share", p.ID, p.Weight)
  		}
  		seen[p.ID] = true
  		total += p.Weight
  		v.checkGate(p)
  	}
  	if total != 100 {
  		v.addf("phase weights sum to %d, want 100", total)
  	}
  }

  func (v *lifecycleValidator) checkGate(p LifecyclePhase) {
  	gate, ok := v.tasks[p.Gate]
  	if !ok || gate.Kind != LifecycleTaskReview || gate.Phase != p.ID {
  		v.addf("phase %q gate %q must be a review task in that phase", p.ID, p.Gate)
  		return
  	}
  	for _, t := range v.l.Tasks {
  		if t.Phase == p.ID && t.ID != gate.ID && !v.ancestors[gate.ID][t.ID] {
  			v.addf("task %q is not upstream of its phase gate %q, so the gate could pass with it unfinished", t.ID, gate.ID)
  		}
  	}
  }

  func (v *lifecycleValidator) checkReviews() {
  	dispatches := 0
  	for _, t := range v.l.Tasks {
  		if t.Kind == LifecycleTaskDispatch {
  			dispatches++
  		}
  	}
  	for _, t := range v.l.Tasks {
  		if t.Kind == LifecycleTaskReview {
  			v.checkReview(t, dispatches)
  			continue
  		}
  		if t.Reviews != "" {
  			v.addf("task %q sets reviews, but only a review task may", t.ID)
  		}
  	}
  }

  func (v *lifecycleValidator) checkReview(t LifecycleTask, dispatches int) {
  	if t.Reviews == "" {
  		if dispatches > 0 || t.ArtifactKind == "" {
  			v.addf("review task %q must name the dispatch task it reviews (only a lifecycle with no dispatch task may omit it, and that review then carries the artifactKind)", t.ID)
  		}
  		return
  	}
  	target, ok := v.tasks[t.Reviews]
  	if !ok || target.Kind != LifecycleTaskDispatch || !v.ancestors[t.ID][t.Reviews] {
  		v.addf("review task %q reviews %q, which is not an ancestor dispatch task", t.ID, t.Reviews)
  	}
  }
  ```

  Note for `TestValidateLifecycle_RejectsEachDefect`'s `"dispatch claims to review"` case: its expected substring is `only a review task`; the message above reads "…sets reviews, but only a review task may" — keep the two in step if you reword either.

- [ ] **Step 6: Run the tests; confirm they pass.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets && GOWORK=off go test ./... 
  ```
  Expected: `ok  github.com/mixofreality-studio/archistrator-platform/method-assets` (and the `cmd/...` packages `ok` or `[no test files]`). If `TestLifecycles_CommandsAreDispatchableAndNameTheirAgent` fails, the DATA is wrong, not the test — fix `lifecycles.json` to the agent `stepmanifest.gen.go` names; never edit a charter or the manifest to fit.

- [ ] **Step 7: Gates.** The platform CI (`.github/workflows/platform-checks.yml`) runs build, lint and test per module with `GOWORK=off`; golangci-lint is pinned at **v2.13.2** there.
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets
  GOWORK=off go build ./...
  GOWORK=off go vet ./...
  gofmt -l .                                   # expect: no output
  golangci-lint --version                      # expect: 2.13.2 — a different version can disagree with CI
  GOWORK=off golangci-lint run ./...           # expect: 0 issues
  GOWORK=off go run ./cmd/gen-stepmanifest && git diff --exit-code -- stepmanifest.gen.go   # expect: no diff — this task must not move the step manifest
  ```
  A `gocognit`/`gocyclo`/`funlen` finding in `lifecycles.go` is fixed by splitting the method, never by a `//nolint` and never by touching `.golangci.yml`.

- [ ] **Step 8: Commit** — authored AND committed as the founder. The platform repo's local git identity is `uitests <uitests@aiarch.local>`; `--author` alone would still leave `uitests` as the committer on a public repo, so both are overridden:
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
  git add method-assets/lifecycles.json method-assets/lifecycles.go method-assets/lifecycles_test.go
  git -c user.name='David Marne' -c user.email='davemarne@gmail.com' \
    commit --author='David Marne <davemarne@gmail.com>' -F - <<'MSG'
  method-assets: the per-activity-type lifecycle task DAG, as data

  lifecycles.json carries, for each of the 14 activity types, its lifecycle
  phases (weight, gate, exit criterion) and its task DAG (dispatch | review,
  dependsOn, reviews, command, workerClass, artifactKind). Service and frontend
  fork per Figure A-1; every other type is one dispatch and one review gate per
  phase; projectDesign is a single review. LifecycleFor / Lifecycles read it,
  ValidateLifecycle states its structural rules.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  git log -1 --format='%an <%ae> | %cn <%ce>'
  ```
  Expected last line: `David Marne <davemarne@gmail.com> | David Marne <davemarne@gmail.com>`. Anything else: `git commit --amend --reset-author` is NOT the fix (it re-reads the repo-local identity) — re-run the commit command above with `--amend`.

---

### Task 2: Release `method-assets/v0.9.0`

**Repo:** `/Users/davidmarne/mixofrealitystudio/archistrator-platform`. Tags are `method-assets/vX.Y.Z`, **annotated** (`git show method-assets/v0.8.0` has a `Tagger:` line — and it reads `uitests`, the very leak Task 1 Step 8 avoids; the tag needs the same identity override as the commit).

**Files:** none (a tag and a push).

**Interfaces:**
- Consumes: the Task 1 commit on platform `main`.
- Produces: the module version `github.com/mixofreality-studio/archistrator-platform/method-assets@v0.9.0`, exporting the Task 1 API.

- [ ] **Step 1: Verify first — the version is free and `main` is exactly one commit ahead.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
  git tag -l 'method-assets/*' | sort -V | tail -3        # expect: … v0.7.1, v0.8.0 — and NO v0.9.0
  git ls-remote --tags origin 'method-assets/v0.9.0'       # expect: no output
  git fetch origin && git log --oneline origin/main..main  # expect: exactly the Task 1 commit
  ```
  If `v0.9.0` exists locally or on the remote, take the next free minor and substitute it in every command below and in Task 3. If `origin/main..main` lists anything besides the Task 1 commit, stop and report — this release must not carry someone else's unpushed work.

- [ ] **Step 2: Re-run the module's tests at the commit being tagged.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/method-assets && GOWORK=off go test ./...
  ```
  Expected: all `ok`.

- [ ] **Step 3: STOP — requires founder confirmation.** Tagging and pushing a public platform release is not reversible (the Go proxy caches a version forever; a bad `v0.9.0` can only be superseded, never replaced). Show the founder: the commit sha and subject, the `git log -1 --format='%an <%ae> | %cn <%ce>'` line, and the tag name. Do not run Step 4 until the founder says go.

- [ ] **Step 4: Tag and publish (founder-confirmed).**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
  git -c user.name='David Marne' -c user.email='davemarne@gmail.com' \
    tag -a method-assets/v0.9.0 -F - <<'MSG'
  method-assets v0.9.0

  The per-activity-type lifecycle task DAG ships as data.

  - lifecycles.json: 14 activity types — phases, weights, gates, exit criteria,
    and the dispatch/review task DAG (service and frontend fork per Figure A-1)
  - LifecycleFor / Lifecycles / ValidateLifecycle
  MSG
  git for-each-ref --format='%(taggername) %(taggeremail)' refs/tags/method-assets/v0.9.0   # expect: David Marne <davemarne@gmail.com>
  git push origin main method-assets/v0.9.0
  git ls-remote --tags origin 'method-assets/v0.9.0'   # must print the tag
  ```
  A rejected push means the remote moved: stop and report. Never force-push. If the tagger line is wrong, delete the LOCAL tag (`git tag -d method-assets/v0.9.0`) and re-create it before anything is pushed.

- [ ] **Step 5: Confirm platform CI is green for the pushed commit.**
  ```bash
  cd /Users/davidmarne/mixofrealitystudio/archistrator-platform && gh run list --branch main --limit 3
  ```
  Expected: the `platform-checks` run for the Task 1 commit concludes `success`. A red run is fixed forward on the platform (a `v0.9.1`), and Task 3 waits for it.

---

### Task 3: Consume `method-assets@v0.9.0` in the server and pin parity with `ProfileFor` / `CommandFor`

Until stage 2 deletes `profileRows` / `phaseTasks` / `gateTasks`, the lifecycle exists twice: as Go tables in `projectstate` and as `lifecycles.json`. This task makes that duplication safe — a parity test that fails the moment either side moves alone.

**Where the test lives, and why it is legal there.** `server/internal/resourceaccess/projectstate/access_test.go` — the package's single closed test file (`TestFileLayout`: one `<stereotype>_test.go` per leaf package, so NO new file). It already imports `methodassets` (L27, used by `TestDesignCommandsExistInMethodAssets` ~L7423). `TestMethodLayering` (`server/internal/arch_test.go:49`) has no rule about who may import method-assets: its `AllowedImportPrefixes` (~L244) admits the whole `github.com/mixofreality-studio/` family for every layer, the check loads with `Tests:false`, and three ResourceAccess packages (`agenticjob`, `sourcecontrol`, `projectstate`'s tests) plus `cmd/aiarch-state-mcp` import it today. The test needs unexported access to nothing, but it must sit beside `ProfileFor`, and the package's test file is in-package (`package projectstate`).

**Files:**
- Modify: `server/go.mod` (L12: `method-assets v0.8.0` → `v0.9.0`), `server/go.sum`
- Modify: `server/internal/resourceaccess/projectstate/access_test.go` — append at end of file (after `TestPendingOperatorNotes_UndeliveredDeliverableKindsInOrder`, currently the last function, ~L10085–10102)
- Re-materialized, gitignored, never committed: `.claude/agents/**`, `.claude/commands/**`, `.claude/skills/the-method-*/**`
- No new exported symbol in `projectstate` → no `arch_test.go` allowlist entry.

**Interfaces:**
- Consumes (method-assets v0.9.0): `methodassets.LifecycleFor(typeKey string) (Lifecycle, bool)`, `methodassets.Lifecycle`, `methodassets.LifecyclePhase`, `methodassets.LifecycleTask`, `methodassets.LifecycleTaskDispatch`, `methodassets.LifecycleTaskReview`.
- Consumes (projectstate, all existing): `ProfileFor(t ActivityType, v TestingVariant) Profile`, `CommandFor(t ActivityType, v TestingVariant, p ActivityMethodPhase) string`, `AgentTaskFor(p ActivityMethodPhase) MethodTask`, `GateTaskFor(p ActivityMethodPhase) MethodTask`, `TaskLabelFor(t ActivityType, v TestingVariant, task MethodTask) string`, `ExitCriterionFor(t ActivityType, v TestingVariant, p ActivityMethodPhase) string`, `Phase1RequiredKinds() []ArtifactKind`, `DesignCommandFor(k ArtifactKind, mode DesignJobMode, addressee string) string`, `ActivityType.String()`, `TestingVariant.String()`, `ArtifactKind.String()`, and the test helpers `allProfileCombos()` / `profileCombo` (~L7300–7317).
- Produces: test-only helper `lifecycleTypeKey(t ActivityType, v TestingVariant) string` — the key rule (D2) written down once on the app side. **The other half of this plan needs the same rule in production code; it must restate it there, not import it from a `_test.go`.**

- [ ] **Step 1: Verify first — the Go proxy serves the new tag.**
  ```bash
  cd server && GOWORK=off go list -m -versions github.com/mixofreality-studio/archistrator-platform/method-assets
  ```
  Expected: the list ends in `v0.9.0`. A fresh tag can take a minute to appear; retry. Never fall back to a pseudo-version or a `replace` directive.

- [ ] **Step 2: Write the failing parity tests.** Append to `server/internal/resourceaccess/projectstate/access_test.go`:

  ```go
  // ---- lifecycles.json parity (unified activity experience, stage 0) ----
  //
  // method-assets' lifecycles.json becomes the ONE source of the per-type lifecycle
  // in stage 2, when profileRows / phaseTasks / gateTasks leave this package. Until
  // then the lifecycle exists twice, and these tests are what makes that safe: a
  // weight, label, exit criterion, task title or command changed on one side alone
  // fails here.

  // lifecycleTypeKey is the method-assets lifecycle key of a profile: the activity
  // type's wire name, qualified by the testing variant's wire name for testing.
  func lifecycleTypeKey(t ActivityType, v TestingVariant) string {
  	if t == ActivityTypeTesting {
  		return t.String() + ":" + v.String()
  	}
  	return t.String()
  }

  // allLifecycleCombos is allProfileCombos plus the two single-profile types that
  // list leaves out.
  func allLifecycleCombos() []profileCombo {
  	return append(allProfileCombos(),
  		profileCombo{ActivityTypeUIDesign, 0},
  		profileCombo{ActivityTypeIntegration, 0})
  }

  // lifecycleTaskWords is the comparable projection of a methodassets.LifecycleTask
  // (which holds a slice, so cannot be compared with ==): everything the server's
  // tables also state about a task.
  type lifecycleTaskWords struct {
  	ID, Kind, Title, InLifecyclePhase, Command, Reviews string
  }

  func lifecycleTaskWordsOf(lc methodassets.Lifecycle, id MethodTask) lifecycleTaskWords {
  	for _, task := range lc.Tasks {
  		if task.ID == string(id) {
  			return lifecycleTaskWords{task.ID, task.Kind, task.Title, task.Phase, task.Command, task.Reviews}
  		}
  	}
  	return lifecycleTaskWords{}
  }

  func TestLifecyclesParity_EveryProfileEqualsItsLifecycle(t *testing.T) {
  	for _, combo := range allLifecycleCombos() {
  		key := lifecycleTypeKey(combo.t, combo.v)
  		t.Run(key, func(t *testing.T) {
  			lc, ok := methodassets.LifecycleFor(key)
  			if !ok {
  				t.Fatalf("method-assets carries no lifecycle %q", key)
  			}
  			profile := ProfileFor(combo.t, combo.v)
  			if len(lc.Phases) != len(profile.Phases) {
  				t.Fatalf("%d lifecycle phases, ProfileFor has %d", len(lc.Phases), len(profile.Phases))
  			}
  			if len(lc.Tasks) != 2*len(profile.Phases) {
  				t.Errorf("%d tasks, want one work task and one gate per phase (%d)", len(lc.Tasks), 2*len(profile.Phases))
  			}
  			for i, pp := range profile.Phases {
  				assertLifecyclePhaseParity(t, combo, lc, lc.Phases[i], pp)
  			}
  		})
  	}
  }

  func assertLifecyclePhaseParity(t *testing.T, combo profileCombo, lc methodassets.Lifecycle, got methodassets.LifecyclePhase, pp ProfilePhase) {
  	t.Helper()
  	work, gate := AgentTaskFor(pp.Phase), GateTaskFor(pp.Phase)

  	wantPhase := methodassets.LifecyclePhase{
  		ID:            string(pp.Phase),
  		Label:         pp.Label,
  		Weight:        pp.Weight,
  		Gate:          string(gate),
  		ExitCriterion: ExitCriterionFor(combo.t, combo.v, pp.Phase),
  	}
  	if got != wantPhase {
  		t.Errorf("lifecycle phase = %+v, want %+v", got, wantPhase)
  	}

  	wantWork := lifecycleTaskWords{
  		string(work), methodassets.LifecycleTaskDispatch, TaskLabelFor(combo.t, combo.v, work),
  		string(pp.Phase), CommandFor(combo.t, combo.v, pp.Phase), "",
  	}
  	if gotWork := lifecycleTaskWordsOf(lc, work); gotWork != wantWork {
  		t.Errorf("work task = %+v, want %+v", gotWork, wantWork)
  	}

  	// A construction gate carries no command: who reviews is the review engine's call.
  	wantGate := lifecycleTaskWords{
  		string(gate), methodassets.LifecycleTaskReview, TaskLabelFor(combo.t, combo.v, gate),
  		string(pp.Phase), "", string(work),
  	}
  	if gotGate := lifecycleTaskWordsOf(lc, gate); gotGate != wantGate {
  		t.Errorf("gate task = %+v, want %+v", gotGate, wantGate)
  	}
  }

  // The requirements and architecture lifecycles are today's design rail, in order:
  // one draft per Phase1RequiredKinds() kind, dispatched by DesignCommandFor's draft
  // command, and critiqued by exactly the command DesignCommandFor dispatches today —
  // "" for a kind designKindHasCritique excludes (volatilities).
  func TestLifecyclesParity_DesignActivitiesFollowTheDesignRail(t *testing.T) {
  	var drafts []methodassets.LifecycleTask
  	critiqueOf := map[string]string{}
  	for _, key := range []string{"requirements", "architecture"} {
  		lc, ok := methodassets.LifecycleFor(key)
  		if !ok {
  			t.Fatalf("method-assets carries no lifecycle %q", key)
  		}
  		for _, task := range lc.Tasks {
  			if task.Kind == methodassets.LifecycleTaskDispatch {
  				drafts = append(drafts, task)
  				continue
  			}
  			critiqueOf[task.Reviews] = task.Command
  		}
  	}
  	kinds := Phase1RequiredKinds()
  	if len(drafts) != len(kinds) {
  		t.Fatalf("%d design dispatch tasks, Phase1RequiredKinds has %d", len(drafts), len(kinds))
  	}
  	for i, k := range kinds {
  		draft := drafts[i]
  		if draft.ArtifactKind != k.String() {
  			t.Errorf("design dispatch %d produces %q, want %q", i, draft.ArtifactKind, k.String())
  		}
  		if want := DesignCommandFor(k, DesignJobModeDraft, ""); draft.Command != want {
  			t.Errorf("%s command = %q, want %q", draft.ID, draft.Command, want)
  		}
  		if want := DesignCommandFor(k, DesignJobModeCritique, ""); critiqueOf[draft.ID] != want {
  			t.Errorf("%s is critiqued by %q, want %q", draft.ID, critiqueOf[draft.ID], want)
  		}
  	}
  }

  // Project Design is deterministic (R7): one human gate over the computed SDP, and
  // nothing to dispatch — the same "" DesignCommandFor returns for KindSdpReview.
  func TestLifecyclesParity_ProjectDesignIsOneUndispatchedGate(t *testing.T) {
  	lc, ok := methodassets.LifecycleFor("projectDesign")
  	if !ok || len(lc.Tasks) != 1 {
  		t.Fatalf("projectDesign must be one task, got %+v", lc)
  	}
  	gate := lc.Tasks[0]
  	if gate.Kind != methodassets.LifecycleTaskReview || gate.ArtifactKind != KindSdpReview.String() {
  		t.Errorf("projectDesign gate = %+v, want a review of %s", gate, KindSdpReview)
  	}
  	if want := DesignCommandFor(KindSdpReview, DesignJobModeDraft, ""); gate.Command != want {
  		t.Errorf("projectDesign gate command = %q, want %q", gate.Command, want)
  	}
  }
  ```

- [ ] **Step 3: Run them; confirm the package fails to compile against v0.8.0.**
  ```bash
  cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestLifecyclesParity' -count=1 2>&1 | head
  ```
  Expected: `undefined: methodassets.LifecycleFor`, `undefined: methodassets.Lifecycle`, … `FAIL … [build failed]`.

- [ ] **Step 4: Bump the pin.**
  ```bash
  cd server && GOWORK=off go get github.com/mixofreality-studio/archistrator-platform/method-assets@v0.9.0
  git diff --stat -- go.mod go.sum
  ```
  Expected: `go.mod` changes one line (`method-assets v0.8.0` → `v0.9.0`); `go.sum` swaps the two method-assets hashes. Any OTHER module moving means `go get` resolved a wider upgrade — stop, `git checkout -- go.mod go.sum`, and report.

- [ ] **Step 5: Re-materialize `.claude` from the new pin.**
  ```bash
  cd server && GOWORK=off make claude-assets
  git status --short -- ../.claude
  ```
  Expected: no output. v0.9.0 changes no asset under `assets/`, the materialized tree is gitignored, and the two committed app-local paths (`.claude/settings.json`, `.claude/skills/grillme`) are never written by `seat-assets`. A tracked diff here is a defect in the release — stop and report.

- [ ] **Step 6: Run the parity tests; confirm they pass.**
  ```bash
  cd server && GOWORK=off go test ./internal/resourceaccess/projectstate/ -run 'TestLifecyclesParity' -count=1 -v 2>&1 | tail -25
  ```
  Expected: `PASS` with eleven `TestLifecyclesParity_EveryProfileEqualsItsLifecycle/<key>` subtests (`service`, `frontend`, `deployment`, `documentation`, `testing:plan`, `testing:harness`, `testing:perf`, `testing:systemTest`, `testing:qaProcess`, `uiDesign`, `integration`) and the two design tests. **A failure here is a data defect in the released `lifecycles.json`**: fix it on the platform and release `v0.9.1` (Tasks 1–2 again, same STOP). Do not edit `profileRows` to agree with a wrong data file, and do not weaken the assertion.

- [ ] **Step 7: Gates** (the pin moved, so the full drift set runs — same list as the 2026-09-12 plan's Global Constraints):
  ```bash
  cd server
  GOWORK=off go build ./...
  GOWORK=off make test-short
  GOWORK=off make lint                     # 0 issues; gocyclo 15 applies to _test.go here
  GOWORK=off make fix-check
  GOWORK=off go test ./internal/ -run 'TestMethodLayering|TestFileLayout|TestGeneratedOnlyPublic|TestRepoStructureCmdIsClosed|TestNoBannedPhaseIdentifier' -count=1
  GOWORK=off make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-uiprofiles-check derived-plan-check
  ```
  All green. `make test-short` includes the tests that read the materialized `.claude` prompt surface; they must pass unchanged, because v0.9.0 changed no prompt text.

- [ ] **Step 8: Commit.**
  ```bash
  git add server/go.mod server/go.sum server/internal/resourceaccess/projectstate/access_test.go
  git commit -F - <<'MSG'
  chore(method-assets): consume v0.9.0; pin lifecycles.json to ProfileFor and CommandFor

  The per-type lifecycle now exists twice until stage 2 deletes profileRows:
  as Go tables in projectstate and as method-assets' lifecycles.json. Parity
  tests hold them equal — phases, weights, labels, exit criteria, task titles
  and commands for all eleven profiles, the design rail's kinds and commands
  for requirements/architecture, and projectDesign's single undispatched gate.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 4: `server/cmd/gen-lifecycles` — render the pinned lifecycles as TypeScript

Mirrors `server/cmd/gen-uiprofiles` (one `main.go` + `main_test.go`, no flags but `-out`, no `project.json` read). The difference is the source: gen-uiprofiles reads `projectstate.ProfileFor`; this reads `methodassets.Lifecycles()` at the version pinned in `server/go.mod`, so the webApp and the server can never disagree about which release they describe.

**The output must be byte-identical to what prettier would write** — `webApp`'s `format:check` covers `src/**/*.ts` and `.prettierignore` does NOT exempt `*.gen.ts` (only `src/api/schema.ts`), and eslint lints it too (`eslint.config.js` ignores only `src/contracts/schema.ts`). That is how `lifecycleTemplates.gen.ts` is handled today, and the same three prettier behaviours are reproduced here: (1) a property whose line would exceed `printWidth: 100` breaks after the colon with the literal indented two further spaces (`writeProp`, measured in characters not bytes); (2) a string takes double quotes when it holds more `'` than `"` (`tsString`); (3) a union that does not fit on one line breaks to one `| member` per line. The exact text below was run through `prettier --stdin-filepath` and `eslint --stdin` against this repo's config with the Task 1 data while writing this plan: prettier reported no diff, eslint no findings.

`tsString` / `writeProp` are deliberately COPIED from `cmd/gen-uiprofiles/main.go` (~L268–291), not shared: both are `package main`, and `TestMethodLayering` requires every `internal/` package to classify as client/manager/engine/resourceaccess, so there is no legal home for a shared helper. The duplication is temporary — stage 2 deletes `gen-uiprofiles` when `lifecycles.gen.ts` replaces `lifecycleTemplates.gen.ts`. **This stage does not touch `lifecycleTemplates.gen.ts`, `cmd/gen-uiprofiles`, or any of their consumers.**

**Files:**
- Create: `server/cmd/gen-lifecycles/main.go`
- Create: `server/cmd/gen-lifecycles/main_test.go`
- Modify: `server/internal/repostructure_test.go` — `allowedCmd` (~L42–55) gains `"gen-lifecycles": true,` between `"clientgen"` and `"gen-systemtests"` (the map is alphabetical)

**Interfaces:**
- Consumes: `methodassets.Lifecycles() []methodassets.Lifecycle`, `methodassets.ValidateLifecycle(l methodassets.Lifecycle) []string` (v0.9.0, pinned by Task 3).
- Produces: `func render(all []methodassets.Lifecycle) (string, error)` (package `main`, unexported — the testable seam), and the CLI `GOWORK=off go run ./cmd/gen-lifecycles [-out <path>]`, default `-out ../webApp/src/components/activity/lifecycles.gen.ts` (relative to `server/`). It creates the output directory if missing — `webApp/src/components/activity/` exists only on the uncommitted `activity-experience-proto` checkout, not on `main`.
- Produces (emitted TS — Task 5 tests it): `LifecycleTaskKind`, `LifecycleTypeKey`, `LifecyclePhaseDef`, `LifecycleTaskDef`, `LifecycleDef`, `LIFECYCLES: readonly LifecycleDef[]`, `lifecycleFor(typeKey: string): LifecycleDef | undefined`.

**How the emitted types line up with the prototype's `webApp/src/components/activity/lifecycleGraphTypes.ts`.** That file (uncommitted, branch `activity-experience-proto`; its vocabulary is already `revision`, not `iteration`) types a graph node as STATIC shape + project STATE: `LifecycleNode { id, kind: 'dispatch'|'review', title, phase, dependsOn: readonly string[], revisionGroup?, laneLabel?, state, revisions }` and `LifecyclePhase { id, label, weight?, passed }`. The generated file carries exactly the static half under the same field names, so a container writes `{ ...taskDef, state, revisions }` and `{ id, label, weight, passed }` with no renaming. The generated names end in `Def` (`LifecycleTaskDef`, `LifecyclePhaseDef`) precisely so they do not collide with the prototype's `LifecycleNode` / `LifecyclePhase` when both are imported. `revisionGroup` is DERIVED by the generator — a review's group is its `reviews` target, any other task's is its own id — which is the prototype's rule ("a draft task and the review task that gates it number their revisions together"). `laneLabel` is not emitted: Figure A-1's only branch is already named by its phase label, which is the case the prototype's doc says to leave it off for.

- [ ] **Step 1: Write the failing tests.** Create `server/cmd/gen-lifecycles/main_test.go`:

  ```go
  package main

  import (
  	"strings"
  	"testing"

  	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
  )

  // fixture is one valid lifecycle that exercises every formatting branch: a
  // property over prettier's width (broken after the colon), a string holding an
  // apostrophe (double-quoted), an empty and a non-empty dependsOn, and the
  // optional properties present on one task and absent on the other.
  func fixture() methodassets.Lifecycle {
  	return methodassets.Lifecycle{
  		Type: "fixture",
  		Phases: []methodassets.LifecyclePhase{{
  			ID: "a", Label: "A", Weight: 100, Gate: "aReview",
  			ExitCriterion: "The plan's scenarios are written, every one of them traced to a use case, and the trace passes review",
  		}},
  		Tasks: []methodassets.LifecycleTask{
  			{ID: "aDraft", Kind: methodassets.LifecycleTaskDispatch, Title: "Draft A", Phase: "a", DependsOn: []string{}, Command: "mission-draft", WorkerClass: "system-architect", ArtifactKind: "A"},
  			{ID: "aReview", Kind: methodassets.LifecycleTaskReview, Title: "Review A", Phase: "a", DependsOn: []string{"aDraft"}, Reviews: "aDraft"},
  		},
  	}
  }

  const wantFixtureData = `export const LIFECYCLES: readonly LifecycleDef[] = [
    {
      type: 'fixture',
      phases: [
        {
          id: 'a',
          label: 'A',
          weight: 100,
          gate: 'aReview',
          exitCriterion:
            "The plan's scenarios are written, every one of them traced to a use case, and the trace passes review",
        },
      ],
      tasks: [
        {
          id: 'aDraft',
          kind: 'dispatch',
          title: 'Draft A',
          phase: 'a',
          dependsOn: [],
          revisionGroup: 'aDraft',
          command: 'mission-draft',
          workerClass: 'system-architect',
          artifactKind: 'A',
        },
        {
          id: 'aReview',
          kind: 'review',
          title: 'Review A',
          phase: 'a',
          dependsOn: ['aDraft'],
          revisionGroup: 'aDraft',
          reviews: 'aDraft',
        },
      ],
    },
  ];
  `

  func TestRender_FixtureDataIsPrettierShaped(t *testing.T) {
  	got, err := render([]methodassets.Lifecycle{fixture()})
  	if err != nil {
  		t.Fatal(err)
  	}
  	if !strings.HasPrefix(got, "// Code generated by server/cmd/gen-lifecycles. DO NOT EDIT.\n") {
  		t.Errorf("missing the generated-code header, got %q…", got[:60])
  	}
  	if !strings.Contains(got, "\nexport type LifecycleTypeKey = 'fixture';\n") {
  		t.Error("a union that fits on one line must stay on one line, as prettier keeps it")
  	}
  	from := strings.Index(got, "export const LIFECYCLES")
  	to := strings.Index(got, "\n/** The lifecycle for an activity-type key")
  	if from < 0 || to < from {
  		t.Fatalf("LIFECYCLES const or lifecycleFor missing:\n%s", got)
  	}
  	if data := got[from:to]; data != wantFixtureData {
  		t.Errorf("LIFECYCLES data =\n%s\nwant\n%s", data, wantFixtureData)
  	}
  	if !strings.HasSuffix(got, "  return LIFECYCLES.find((l) => l.type === typeKey);\n}\n") {
  		t.Error("lifecycleFor must close the file, newline-terminated")
  	}
  }

  func TestRender_RefusesInvalidData(t *testing.T) {
  	bad := fixture()
  	bad.Phases[0].Weight = 90
  	if _, err := render([]methodassets.Lifecycle{bad}); err == nil || !strings.Contains(err.Error(), "sum to 90") {
  		t.Errorf("render must refuse data ValidateLifecycle rejects, got err = %v", err)
  	}
  	if _, err := render(nil); err == nil {
  		t.Error("render must refuse an empty lifecycle set rather than emit an empty table")
  	}
  }

  func TestRender_PinnedDataIsCompleteAndDeterministic(t *testing.T) {
  	first, err := render(methodassets.Lifecycles())
  	if err != nil {
  		t.Fatal(err)
  	}
  	second, _ := render(methodassets.Lifecycles())
  	if first != second {
  		t.Error("two renders of the same data differ")
  	}
  	if !strings.Contains(first, "export type LifecycleTypeKey =\n  | 'requirements'\n  | 'architecture'\n") {
  		t.Error("the fourteen-member key union is wider than 100 characters and must break one member per line")
  	}
  	for _, l := range methodassets.Lifecycles() {
  		if !strings.Contains(first, "    type: "+tsString(l.Type)+",\n") {
  			t.Errorf("lifecycle %q was not emitted", l.Type)
  		}
  	}
  	for _, never := range []string{"someConstruction", "testClient"} {
  		if strings.Contains(first, never) {
  			t.Errorf("%s is a conditional sub-attempt, never a lifecycle node", never)
  		}
  	}
  }

  func TestTsString_QuotesTheWayPrettierDoes(t *testing.T) {
  	cases := map[string]string{
  		"SRS Review":          `'SRS Review'`,
  		"the plan's trace":    `"the plan's trace"`,
  		`say "go"`:            `'say "go"'`,
  		`'tis 'x' "y"`:        `"'tis 'x' \"y\""`,
  		`a\b`:                 `'a\\b'`,
  	}
  	for in, want := range cases {
  		if got := tsString(in); got != want {
  			t.Errorf("tsString(%q) = %s, want %s", in, got, want)
  		}
  	}
  }

  // Prettier measures a line in characters. The copy carries "—" and "·" (multi-byte
  // in UTF-8), so a byte count would break lines prettier keeps whole.
  func TestWriteProp_MeasuresCharactersNotBytes(t *testing.T) {
  	// 4 indent + "k" + ": " + quoted literal (2 + 80 + 10) + "," = exactly 100 characters.
  	literal := tsString(strings.Repeat("a", 80) + strings.Repeat("—", 10))
  	var b strings.Builder
  	writeProp(&b, "    ", "k", literal)
  	if want := "    k: " + literal + ",\n"; b.String() != want {
  		t.Errorf("a 100-character line was broken:\n%q\nwant\n%q", b.String(), want)
  	}
  	longer := tsString(strings.Repeat("a", 81) + strings.Repeat("—", 10))
  	b.Reset()
  	writeProp(&b, "    ", "k", longer)
  	if want := "    k:\n      " + longer + ",\n"; b.String() != want {
  		t.Errorf("a 101-character line was not broken:\n%q\nwant\n%q", b.String(), want)
  	}
  }
  ```

- [ ] **Step 2: Run them; confirm they fail to compile.**
  ```bash
  cd server && GOWORK=off go test ./cmd/gen-lifecycles/ 2>&1 | head
  ```
  Expected: `undefined: render`, `undefined: tsString`, `undefined: writeProp` → `FAIL … [build failed]`.

- [ ] **Step 3: Write the generator.** Create `server/cmd/gen-lifecycles/main.go`. (The TS comments inside the Go raw strings contain no backtick on purpose — a raw string cannot hold one.)

  ```go
  // cmd/gen-lifecycles emits the per-activity-type lifecycle — phases (weight, gate,
  // exit criterion) and the dispatch/review task DAG — into
  // webApp/src/components/activity/lifecycles.gen.ts, straight from the
  // lifecycles.json shipped in the method-assets release pinned in go.mod
  // (methodassets.Lifecycles). The data is platform-fixed (the same for every
  // project), so this tool reads no project.json and takes no flag but -out.
  //
  // The output is byte-identical to what the webApp's prettier config would write,
  // because the webApp's format check covers generated files too: an over-wide
  // property breaks after its colon (writeProp), a string is quoted the way prettier
  // quotes it (tsString), and the type-key union breaks one member per line once it
  // no longer fits on one.
  //
  // It refuses to generate from data methodassets.ValidateLifecycle rejects.
  //
  // Usage (matching the Makefile gen-lifecycles / gen-lifecycles-check targets, run
  // from server/):
  //
  //	GOWORK=off go run ./cmd/gen-lifecycles \
  //	  -out ../webApp/src/components/activity/lifecycles.gen.ts
  package main

  import (
  	"errors"
  	"flag"
  	"fmt"
  	"os"
  	"path/filepath"
  	"strings"
  	"unicode/utf8"

  	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
  )

  const defaultOutPath = "../webApp/src/components/activity/lifecycles.gen.ts"

  func main() {
  	out := flag.String("out", defaultOutPath, "output path for the generated TS file")
  	flag.Parse()

  	if err := run(*out); err != nil {
  		fmt.Fprintf(os.Stderr, "gen-lifecycles: %v\n", err)
  		os.Exit(1)
  	}
  }

  func run(outPath string) error {
  	src, err := render(methodassets.Lifecycles())
  	if err != nil {
  		return err
  	}
  	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
  		return err
  	}
  	return os.WriteFile(outPath, []byte(src), 0o644) //nolint:gosec // generated TS, not a secret
  }

  // render is the whole generator as a pure function: lifecycles in, TS source out.
  func render(all []methodassets.Lifecycle) (string, error) {
  	if len(all) == 0 {
  		return "", errors.New("method-assets carries no lifecycles; refusing to emit an empty table")
  	}
  	var problems []string
  	for _, l := range all {
  		problems = append(problems, methodassets.ValidateLifecycle(l)...)
  	}
  	if len(problems) > 0 {
  		return "", fmt.Errorf("refusing to generate from invalid lifecycle data:\n  %s", strings.Join(problems, "\n  "))
  	}

  	var b strings.Builder
  	b.WriteString(tsHeader)
  	writeTypeKeys(&b, all)
  	b.WriteString(tsTypeDecls)
  	for _, l := range all {
  		if err := writeLifecycle(&b, l); err != nil {
  			return "", err
  		}
  	}
  	b.WriteString(tsFooter)
  	return b.String(), nil
  }

  const tsHeader = `// Code generated by server/cmd/gen-lifecycles. DO NOT EDIT.
  // Source: lifecycles.json in github.com/mixofreality-studio/archistrator-platform/method-assets,
  // at the version pinned in server/go.mod.
  //
  // The per-activity-type lifecycle: its phases (earned-value weight, gate, exit criterion) and
  // its task DAG. Platform-fixed data, the same for every project, so it is compiled into the
  // webApp rather than fetched. STATIC shape only: a task's state and its revisions are project
  // data and arrive from the server.
  //
  // Field names line up with the lifecycle graph's props vocabulary (lifecycleGraphTypes.ts:
  // LifecycleNode id/kind/title/phase/dependsOn/revisionGroup, LifecyclePhase id/label/weight),
  // so a container builds a graph node by spreading a task and adding its state and revisions.

  /** A task either dispatches an agent, which produces an artifact, or reviews one. */
  export type LifecycleTaskKind = 'dispatch' | 'review';

  `

  const tsTypeDecls = `
  /** A Figure A-2 grouping of tasks: the earned-value unit. */
  export interface LifecyclePhaseDef {
    id: string;
    label: string;
    /** Table A-1 earned-value weight, in percent; a lifecycle's weights sum to 100. */
    weight: number;
    /** Id of the review task whose success IS this phase's exit. */
    gate: string;
    /** That exit, as one sentence in this lifecycle's own words. */
    exitCriterion: string;
  }

  /** One node of a lifecycle's task DAG. */
  export interface LifecycleTaskDef {
    id: string;
    kind: LifecycleTaskKind;
    title: string;
    /** The {@link LifecyclePhaseDef.id} this task belongs to. */
    phase: string;
    /** Ids of the tasks this one waits on. Tasks are in authored order: the trunk comes first. */
    dependsOn: readonly string[];
    /** A dispatch and the review that judges it revise ONE artifact together: they share this. */
    revisionGroup: string;
    /** Review only: the dispatch task it judges. A send-back re-opens that pair. */
    reviews?: string;
    /** The slash command an agent runs for this task, when one does. */
    command?: string;
    /** The agent charter that command adopts. */
    workerClass?: string;
    /** What a dispatch produces (or what a review that names no dispatch task judges). */
    artifactKind?: string;
  }

  /** The task DAG every activity of one type walks. */
  export interface LifecycleDef {
    type: LifecycleTypeKey;
    phases: readonly LifecyclePhaseDef[];
    tasks: readonly LifecycleTaskDef[];
  }

  export const LIFECYCLES: readonly LifecycleDef[] = [
  `

  const tsFooter = `];

  /** The lifecycle for an activity-type key; undefined for a key this build does not carry. */
  export function lifecycleFor(typeKey: string): LifecycleDef | undefined {
    return LIFECYCLES.find((l) => l.type === typeKey);
  }
  `

  // writeTypeKeys emits the closed union of type keys: on one line when it fits
  // prettier's width, otherwise one member per line — prettier's own two forms.
  func writeTypeKeys(b *strings.Builder, all []methodassets.Lifecycle) {
  	keys := make([]string, len(all))
  	for i, l := range all {
  		keys[i] = tsString(l.Type)
  	}
  	b.WriteString("/** The closed set of activity-type keys: the ActivityType wire name, or testing:<variant>. */\n")
  	oneLine := "export type LifecycleTypeKey = " + strings.Join(keys, " | ") + ";"
  	if utf8.RuneCountInString(oneLine) <= prettierPrintWidth {
  		b.WriteString(oneLine + "\n")
  		return
  	}
  	b.WriteString("export type LifecycleTypeKey =\n  | " + strings.Join(keys, "\n  | ") + ";\n")
  }

  func writeLifecycle(b *strings.Builder, l methodassets.Lifecycle) error {
  	b.WriteString("  {\n")
  	writeProp(b, "    ", "type", tsString(l.Type))
  	b.WriteString("    phases: [\n")
  	for _, lp := range l.Phases {
  		b.WriteString("      {\n")
  		writeProp(b, memberIndent, "id", tsString(lp.ID))
  		writeProp(b, memberIndent, "label", tsString(lp.Label))
  		writeProp(b, memberIndent, "weight", fmt.Sprintf("%d", lp.Weight))
  		writeProp(b, memberIndent, "gate", tsString(lp.Gate))
  		writeProp(b, memberIndent, "exitCriterion", tsString(lp.ExitCriterion))
  		b.WriteString("      },\n")
  	}
  	b.WriteString("    ],\n    tasks: [\n")
  	for _, task := range l.Tasks {
  		if err := writeTask(b, task); err != nil {
  			return fmt.Errorf("%s: %w", l.Type, err)
  		}
  	}
  	b.WriteString("    ],\n  },\n")
  	return nil
  }

  // memberIndent is the indent of a property inside a phases[] / tasks[] member.
  const memberIndent = "        "

  func writeTask(b *strings.Builder, task methodassets.LifecycleTask) error {
  	deps := make([]string, len(task.DependsOn))
  	for i, d := range task.DependsOn {
  		deps[i] = tsString(d)
  	}
  	depsLine := memberIndent + "dependsOn: [" + strings.Join(deps, ", ") + "],"
  	if n := utf8.RuneCountInString(depsLine); n > prettierPrintWidth {
  		return fmt.Errorf("task %q: its dependsOn line is %d characters; prettier would break that array one id per line and this generator does not — teach writeTask that form before shipping a task this wide", task.ID, n)
  	}

  	b.WriteString("      {\n")
  	writeProp(b, memberIndent, "id", tsString(task.ID))
  	writeProp(b, memberIndent, "kind", tsString(task.Kind))
  	writeProp(b, memberIndent, "title", tsString(task.Title))
  	writeProp(b, memberIndent, "phase", tsString(task.Phase))
  	b.WriteString(depsLine + "\n")
  	writeProp(b, memberIndent, "revisionGroup", tsString(revisionGroupOf(task)))
  	writeOptionalProp(b, "reviews", task.Reviews)
  	writeOptionalProp(b, "command", task.Command)
  	writeOptionalProp(b, "workerClass", task.WorkerClass)
  	writeOptionalProp(b, "artifactKind", task.ArtifactKind)
  	b.WriteString("      },\n")
  	return nil
  }

  // revisionGroupOf is the artifact a task revises: a review revises the artifact of
  // the dispatch task it judges, so the pair numbers its revisions together; any other
  // task numbers its revisions alone.
  func revisionGroupOf(task methodassets.LifecycleTask) string {
  	if task.Reviews != "" {
  		return task.Reviews
  	}
  	return task.ID
  }

  // writeOptionalProp omits an empty value entirely: the TS property is optional, and
  // under exactOptionalPropertyTypes an absent key is not the same as an empty string.
  func writeOptionalProp(b *strings.Builder, key, value string) {
  	if value != "" {
  		writeProp(b, memberIndent, key, tsString(value))
  	}
  }

  // prettierPrintWidth is the webApp's prettier printWidth (webApp/.prettierrc).
  const prettierPrintWidth = 100

  // writeProp writes "<indent><key>: <literal>," — or, when that line would exceed
  // prettier's print width, the key alone with the literal on the next line indented
  // two further spaces, which is how prettier breaks an over-long property. Width is
  // measured in CHARACTERS, as prettier measures it, not bytes.
  //
  // Copied from cmd/gen-uiprofiles (two package mains cannot share a helper, and the
  // layering gate leaves no internal package to hold one); that copy dies in stage 2.
  func writeProp(b *strings.Builder, indent, key, literal string) {
  	line := indent + key + ": " + literal + ","
  	if utf8.RuneCountInString(line) <= prettierPrintWidth {
  		b.WriteString(line + "\n")
  		return
  	}
  	fmt.Fprintf(b, "%s%s:\n%s  %s,\n", indent, key, indent, literal)
  }

  // tsString renders a Go string as a TS string literal the way prettier does under the
  // repo's singleQuote config: single-quoted, unless the text holds more single quotes
  // than double quotes, in which case prettier prefers double quotes to save escapes.
  func tsString(s string) string {
  	quote := "'"
  	if strings.Count(s, "'") > strings.Count(s, `"`) {
  		quote = `"`
  	}
  	escaped := strings.ReplaceAll(s, `\`, `\\`)
  	escaped = strings.ReplaceAll(escaped, quote, `\`+quote)
  	return quote + escaped + quote
  }
  ```

  **Indentation warning for whoever pastes this:** inside the three raw-string constants (`tsHeader`, `tsTypeDecls`, `tsFooter`) and inside `wantFixtureData` in the test, the text is TypeScript at its real column — after removing this plan's two-space list indent, `// Code generated…`, `export …` and `];` start at column 0, and TS bodies are indented with SPACES (two per level), never tabs. Everything outside those constants is ordinary tab-indented Go. `gofmt` will not flag a wrong raw string; `TestRender_FixtureDataIsPrettierShaped` and Task 5's `format:check` will.

- [ ] **Step 4: Run the tests; confirm they pass.**
  ```bash
  cd server && gofmt -l cmd/gen-lifecycles && GOWORK=off go test ./cmd/gen-lifecycles/ -count=1
  ```
  Expected: no gofmt output, then `ok  github.com/mixofreality-studio/archistrator/server/cmd/gen-lifecycles`.

- [ ] **Step 5: Register the command in the closed cmd allowlist.** In `server/internal/repostructure_test.go`, `allowedCmd` (~L42):
  ```go
  	"clientgen":            true,
  	"gen-lifecycles":       true,
  	"gen-systemtests":      true,
  ```
  The gate reads `git ls-files`, so it only sees the new directory once it is staged:
  ```bash
  git add server/cmd/gen-lifecycles server/internal/repostructure_test.go
  cd server && GOWORK=off go test ./internal/ -run 'TestRepoStructureCmdIsClosed' -count=1
  ```
  Expected: `ok`. (To see the gate bite first, stage only `server/cmd/gen-lifecycles` and run it before the edit: it fails naming `gen-lifecycles` as an unlisted cmd.)

- [ ] **Step 6: Gates.**
  ```bash
  cd server
  GOWORK=off go build ./...
  GOWORK=off go vet ./cmd/gen-lifecycles/
  GOWORK=off make lint          # 0 issues — gocyclo 15, revive, gocritic, gosec all apply to this cmd
  GOWORK=off make fix-check
  GOWORK=off go test ./internal/ -run 'TestMethodLayering|TestFileLayout|TestRepoStructureCmdIsClosed|TestNoBannedPhaseIdentifier' -count=1
  ```
  Note for `TestNoBannedPhaseIdentifier`: the generator names its loop variable `lp`, never `phase` — keep it that way.

- [ ] **Step 7: Commit.**
  ```bash
  git add server/cmd/gen-lifecycles server/internal/repostructure_test.go
  git commit -F - <<'MSG'
  feat(gen-lifecycles): render the pinned method-assets lifecycles as TypeScript

  A pure render(lifecycles) -> TS source, byte-identical to prettier's output
  for the webApp config, that refuses data ValidateLifecycle rejects. Emits
  LifecycleTypeKey, the *Def types, LIFECYCLES and lifecycleFor; derives each
  task's revisionGroup from its reviews target. The output file, its drift
  gate and its webApp test land in the next commit.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

### Task 5: Generate `lifecycles.gen.ts`, gate its drift, and test it in the webApp

**Files:**
- Create (tool output, never hand-edited): `webApp/src/components/activity/lifecycles.gen.ts`
- Create: `webApp/src/components/activity/lifecycles.gen.test.ts`
- Modify: `server/Makefile` — `.PHONY` (L3), the `gen:` aggregate (L81), and two new targets after `gen-uiprofiles-check` (~L167–168)
- Modify: `.github/workflows/server-checks.yml` — one step after "Generated UI lifecycle-profile drift check" (~L149–150)
- **Untouched: `webApp/src/components/construction/lifecycleTemplates.gen.ts`**, its test, `cmd/gen-uiprofiles`, and all five of its consumers (`graph/laneSpine.ts`, `detail/detailPaneState.ts`, `detail/bodies/taskBriefing.ts`, `detail/bodies/bodyDispatch.ts`, `list/activityTree.ts`). The console keeps rendering from it; stage 2 makes the swap. The new test reads it (read-only) to prove the two generated tables agree.

**Interfaces:**
- Consumes: the Task 4 CLI; `GENERATED_TEMPLATES: Record<ActivityKind, readonly GeneratedPhase[]>`, `GENERATED_TESTING_VARIANTS: Record<TestingVariantName, readonly GeneratedPhase[]>`, `GeneratedPhase` from `../construction/lifecycleTemplates.gen.ts` (test only).
- Produces: `make gen-lifecycles`, `make gen-lifecycles-check`; and from `lifecycles.gen.ts`:
  ```ts
  export type LifecycleTaskKind = 'dispatch' | 'review';
  export type LifecycleTypeKey =
    | 'requirements' | 'architecture' | 'projectDesign' | 'service' | 'frontend'
    | 'testing:plan' | 'testing:harness' | 'testing:perf' | 'testing:systemTest' | 'testing:qaProcess'
    | 'deployment' | 'documentation' | 'uiDesign' | 'integration';
  export interface LifecyclePhaseDef { id: string; label: string; weight: number; gate: string; exitCriterion: string }
  export interface LifecycleTaskDef {
    id: string; kind: LifecycleTaskKind; title: string; phase: string;
    dependsOn: readonly string[]; revisionGroup: string;
    reviews?: string; command?: string; workerClass?: string; artifactKind?: string;
  }
  export interface LifecycleDef { type: LifecycleTypeKey; phases: readonly LifecyclePhaseDef[]; tasks: readonly LifecycleTaskDef[] }
  export const LIFECYCLES: readonly LifecycleDef[];
  export function lifecycleFor(typeKey: string): LifecycleDef | undefined;
  ```
  (Shown compactly; the file itself is in prettier's layout, one union member and one property per line — Task 4's constants are the exact text.) A webApp caller derives the key as ``kind === 'testing' ? `testing:${variant}` : kind``, with `variant: TestingVariantName` from `src/contracts/types.ts` (L678) — the D2 rule, which the test below proves against all five variants.

- [ ] **Step 1: Write the failing test.** Create `webApp/src/components/activity/lifecycles.gen.test.ts` (this text is already in prettier's layout for the repo's `.prettierrc`):

  ```ts
  /**
   * The generated lifecycles (lifecycles.gen.ts, from server/cmd/gen-lifecycles, which
   * reads the method-assets release pinned in server/go.mod): what every consumer may
   * assume about them.
   *
   * Structural validity is proven where the data is authored (method-assets'
   * ValidateLifecycle) and again by the generator; these pins are about what reaches
   * the webApp — the shapes the graph draws, the key rule callers use, and agreement
   * with lifecycleTemplates.gen.ts, which the construction console still renders from
   * until stage 2 replaces it.
   */
  /// <reference types="node" />
  import { test } from 'node:test';
  import assert from 'node:assert/strict';

  import { LIFECYCLES, lifecycleFor, type LifecycleDef } from './lifecycles.gen.ts';
  import {
    GENERATED_TEMPLATES,
    GENERATED_TESTING_VARIANTS,
    type GeneratedPhase,
  } from '../construction/lifecycleTemplates.gen.ts';

  function mustLifecycle(typeKey: string): LifecycleDef {
    const found = lifecycleFor(typeKey);
    assert.ok(found !== undefined, `no lifecycle for ${typeKey}`);
    return found;
  }

  const dependsOnOf = (l: LifecycleDef, taskId: string): readonly string[] | undefined =>
    l.tasks.find((t) => t.id === taskId)?.dependsOn;

  void test('the type keys are the closed, ordered set the platform ships', () => {
    assert.deepEqual(
      LIFECYCLES.map((l) => l.type),
      [
        'requirements',
        'architecture',
        'projectDesign',
        'service',
        'frontend',
        'testing:plan',
        'testing:harness',
        'testing:perf',
        'testing:systemTest',
        'testing:qaProcess',
        'deployment',
        'documentation',
        'uiDesign',
        'integration',
      ]
    );
    assert.equal(lifecycleFor('nope'), undefined);
  });

  void test('every lifecycle is drawable: weights total 100, gates review, edges point back', () => {
    for (const l of LIFECYCLES) {
      const total = l.phases.reduce((sum, p) => sum + p.weight, 0);
      assert.equal(total, 100, `${l.type}: phase weights`);
      const earlier = new Set<string>();
      for (const t of l.tasks) {
        for (const dep of t.dependsOn) {
          assert.ok(earlier.has(dep), `${l.type}: ${t.id} depends on ${dep}, not an earlier task`);
        }
        earlier.add(t.id);
      }
      for (const p of l.phases) {
        const gate = l.tasks.find((t) => t.id === p.gate);
        assert.ok(gate !== undefined, `${l.type}: phase ${p.id} has no gate task`);
        assert.equal(gate.kind, 'review', `${l.type}: gate ${gate.id}`);
        assert.equal(gate.phase, p.id, `${l.type}: gate ${gate.id}`);
      }
    }
  });

  void test('service and frontend fork after the first gate and rejoin at testing (Figure A-1)', () => {
    for (const key of ['service', 'frontend']) {
      const l = mustLifecycle(key);
      assert.deepEqual(dependsOnOf(l, 'detailedDesign'), ['srsReview'], key);
      assert.deepEqual(dependsOnOf(l, 'stp'), ['srsReview'], key);
      assert.deepEqual(dependsOnOf(l, 'testing'), ['integration', 'stpReview'], key);
      // Authored order decides the trunk: the design→construction chain stays on lane 0.
      const ids = l.tasks.map((t) => t.id);
      assert.ok(
        ids.indexOf('detailedDesign') < ids.indexOf('stp'),
        `${key}: trunk is authored first`
      );
    }
  });

  void test('a conditional sub-attempt is never a node', () => {
    for (const l of LIFECYCLES) {
      for (const t of l.tasks) {
        assert.ok(t.id !== 'someConstruction' && t.id !== 'testClient', `${l.type}: ${t.id}`);
      }
    }
  });

  void test('project design is one review and nothing to dispatch', () => {
    const l = mustLifecycle('projectDesign');
    assert.equal(l.tasks.length, 1);
    const [gate] = l.tasks;
    assert.ok(gate !== undefined);
    assert.equal(gate.kind, 'review');
    assert.equal(gate.reviews, undefined);
    assert.equal(gate.command, undefined);
    assert.equal(gate.artifactKind, 'SdpReview');
    assert.deepEqual(gate.dependsOn, []);
  });

  void test('the requirements weights are 15/20/35/30 and architecture is one pair', () => {
    assert.deepEqual(
      mustLifecycle('requirements').phases.map((p) => [p.id, p.weight]),
      [
        ['mission', 15],
        ['glossary', 20],
        ['volatilities', 35],
        ['coreUseCases', 30],
      ]
    );
    const arch = mustLifecycle('architecture');
    assert.deepEqual(
      arch.tasks.map((t) => [t.id, t.kind]),
      [
        ['architectureDraft', 'dispatch'],
        ['architectureReview', 'review'],
      ]
    );
  });

  void test('a review shares its revision group with the dispatch it judges', () => {
    for (const l of LIFECYCLES) {
      for (const t of l.tasks) {
        assert.equal(t.revisionGroup, t.reviews ?? t.id, `${l.type}: ${t.id}`);
        if (t.reviews === undefined) continue;
        const judged = l.tasks.find((d) => d.id === t.reviews);
        assert.ok(judged !== undefined, `${l.type}: ${t.id} reviews a task that does not exist`);
        assert.equal(judged.kind, 'dispatch', `${l.type}: ${t.id}`);
        assert.equal(judged.revisionGroup, t.revisionGroup, `${l.type}: ${t.id}`);
      }
    }
  });

  /** A lifecycle, projected onto what lifecycleTemplates.gen.ts also states. */
  function asTemplate(l: LifecycleDef): unknown[] {
    return l.phases.map((p) => ({
      id: l.tasks.find((t) => t.phase === p.id && t.kind === 'dispatch')?.command,
      phase: p.id,
      name: p.label,
      weight: p.weight,
      exitCriterion: p.exitCriterion,
      tasks: l.tasks.filter((t) => t.phase === p.id).map((t) => [t.id, t.title, t.id === p.gate]),
    }));
  }

  /** The same projection of a generated phase table; conditional tasks are not nodes. */
  function templateOf(phases: readonly GeneratedPhase[]): unknown[] {
    return phases.map((p) => ({
      id: p.id,
      phase: p.phase,
      name: p.name,
      weight: p.weight,
      exitCriterion: p.exitCriterion,
      tasks: p.tasks.filter((t) => !t.conditional).map((t) => [t.task, t.label, t.gate]),
    }));
  }

  void test('each lifecycle agrees with the phase table the console still renders from', () => {
    for (const [kind, phases] of Object.entries(GENERATED_TEMPLATES)) {
      const key = kind === 'testing' ? 'testing:plan' : kind;
      assert.deepEqual(asTemplate(mustLifecycle(key)), templateOf(phases), key);
    }
    // The key rule callers use: `testing:${TestingVariantName}`, for all five variants.
    for (const [variant, phases] of Object.entries(GENERATED_TESTING_VARIANTS)) {
      const key = `testing:${variant}`;
      assert.deepEqual(asTemplate(mustLifecycle(key)), templateOf(phases), key);
    }
  });
  ```

- [ ] **Step 2: Run it; confirm it fails.**
  ```bash
  cd webApp && node --test src/components/activity/lifecycles.gen.test.ts 2>&1 | tail -15
  ```
  Expected: `ERR_MODULE_NOT_FOUND` … `lifecycles.gen.ts` — the file has not been generated yet.

- [ ] **Step 3: Add the Makefile targets.** In `server/Makefile`:
  - L3 `.PHONY`: insert `gen-lifecycles gen-lifecycles-check` after `gen-uiprofiles-check`.
  - L81: `gen: gen-models gen-fakes gen-client gen-internal-tools gen-temporal gen-sdk gen-config gen-uiprofiles gen-lifecycles`
  - After the `gen-uiprofiles-check` recipe (~L168), add:
    ```make
    # gen-lifecycles — the per-activity-type lifecycle (phases + the dispatch/review
    # task DAG, cmd/gen-lifecycles) emitted into the sibling webApp module at
    # ../webApp/src/components/activity/lifecycles.gen.ts, straight from the
    # lifecycles.json shipped in the method-assets release pinned in go.mod. No
    # project.json read (the data is platform-fixed), so no -project/-id flags.
    gen-lifecycles:
    	GOWORK=off go run ./cmd/gen-lifecycles

    # gen-lifecycles-check — drift gate for the generated lifecycles, the same
    # regenerate-then-diff shape as gen-uiprofiles-check. `git diff` is blind to an
    # UNTRACKED file, so the ls-files line fails the gate when the generated file was
    # never committed at all. A method-assets pin bump that changes lifecycles.json
    # turns this red until `make gen-lifecycles` is re-run and committed.
    gen-lifecycles-check: gen-lifecycles
    	git ls-files --error-unmatch ../webApp/src/components/activity/lifecycles.gen.ts >/dev/null
    	git diff --exit-code -- ../webApp/src/components/activity/lifecycles.gen.ts
    ```
    (Recipe lines are TAB-indented.)

- [ ] **Step 4: Generate the file; confirm the test passes.**
  ```bash
  cd server && GOWORK=off make gen-lifecycles
  cd ../webApp && node --test src/components/activity/lifecycles.gen.test.ts 2>&1 | tail -15
  ```
  Expected: `webApp/src/components/activity/lifecycles.gen.ts` exists (~1,230 lines, first line `// Code generated by server/cmd/gen-lifecycles. DO NOT EDIT.`), and the run's summary reads `pass 8`, `fail 0` (eight tests; nine if Step 5 applies later). The test text and the generator's exact output were executed together while writing this plan — all eight passed, including the agreement with `lifecycleTemplates.gen.ts`, which is itself generated from `ProfileFor`/`CommandFor`/`TaskLabelFor`/`ExitCriterionFor`. If "agrees with the phase table" fails, do NOT edit either generated file: the two generators read two sources that Task 3's parity test holds equal, so a disagreement here means Task 3 is red or the pin is not v0.9.0.

- [ ] **Step 5: Verify first — is the prototype's `lifecycleGraphTypes.ts` on this branch?**
  ```bash
  git ls-files --error-unmatch webApp/src/components/activity/lifecycleGraphTypes.ts
  ```
  It is uncommitted on `activity-experience-proto` today, so on a worktree cut from `main` this prints `error: pathspec … did not match` — then SKIP this step; stage 5 adds the assertion when it commits the graph. If it IS tracked, make the line-up a compile-time fact by appending to `lifecycles.gen.test.ts`:
  ```ts
  void test('a task def is the static half of a lifecycle graph node', () => {
    const nodes: LifecycleNode[] = mustLifecycle('service').tasks.map((t) => ({
      ...t,
      state: 'pending',
      revisions: [],
    }));
    assert.equal(nodes.length, 10);
  });
  ```
  with `import type { LifecycleNode } from './lifecycleGraphTypes.ts';` added beside the other imports. It is `npm run typecheck`, not the test runner, that proves it (node strips types without checking them).

- [ ] **Step 6: webApp gates.** Generated files are NOT exempt from prettier or eslint here.
  ```bash
  cd webApp
  npx prettier --check src/components/activity/lifecycles.gen.ts src/components/activity/lifecycles.gen.test.ts
  npx eslint src/components/activity/lifecycles.gen.ts src/components/activity/lifecycles.gen.test.ts
  npm run check        # typecheck (tsc -b) + lint + format:check + node --test — the real gate
  ```
  All green. **If prettier flags `lifecycles.gen.ts`, fix the GENERATOR** (`server/cmd/gen-lifecycles/main.go` + its golden test), re-run `make gen-lifecycles`, and never run `prettier --write` on the generated file — a hand-formatted file fails `gen-lifecycles-check` on the next run. If eslint flags the test (the config is type-aware and strict), fix the test, not the config.

- [ ] **Step 7: Wire the drift gate into CI.** In `.github/workflows/server-checks.yml`, directly after the `make gen-uiprofiles-check` step (~L149–150):
  ```yaml
        # Drift gate for the generated per-activity-type lifecycles emitted into the
        # sibling webApp module (cmd/gen-lifecycles, reading the lifecycles.json of
        # the method-assets release pinned in go.mod). Same regenerate-then-diff
        # shape as the other gen-*-check gates; also fails if the file is untracked.
        - name: Generated lifecycles drift check
          run: make gen-lifecycles-check
  ```

- [ ] **Step 8: Prove the drift gate — green when clean, red when drifted.** The generated file must be staged for `ls-files` to see it:
  ```bash
  git add webApp/src/components/activity/lifecycles.gen.ts
  cd server && GOWORK=off make gen-lifecycles-check && echo GREEN
  printf '\n// drift\n' >> ../webApp/src/components/activity/lifecycles.gen.ts
  git add ../webApp/src/components/activity/lifecycles.gen.ts
  GOWORK=off make gen-lifecycles-check || echo RED-AS-EXPECTED
  git add ../webApp/src/components/activity/lifecycles.gen.ts && git diff --cached --stat
  ```
  Expected: `GREEN`; then `RED-AS-EXPECTED` (the regenerate step rewrote the file, so the working tree differs from the drifted index); then, after the final `git add`, the staged file is the clean generated one again (no `// drift` line: `git diff --cached -- ../webApp/src/components/activity/lifecycles.gen.ts | grep -c drift` prints `0`).

- [ ] **Step 9: Server gates** (the Makefile and a workflow changed; nothing in Go did since Task 4):
  ```bash
  cd server
  GOWORK=off make gen-uiprofiles-check gen-lifecycles-check
  git diff --exit-code -- ../webApp/src/components/construction/lifecycleTemplates.gen.ts   # must be untouched by this stage
  GOWORK=off go test ./internal/ -run 'TestRepoStructure' -count=1
  ```

- [ ] **Step 10: Commit.**
  ```bash
  git add webApp/src/components/activity/lifecycles.gen.ts webApp/src/components/activity/lifecycles.gen.test.ts server/Makefile .github/workflows/server-checks.yml
  git commit -F - <<'MSG'
  feat(webapp): generated per-activity-type lifecycles, drift-gated

  lifecycles.gen.ts carries the 14 lifecycles from the pinned method-assets
  release: phases, gates, exit criteria and the dispatch/review task DAG, with
  field names that line up with the lifecycle graph's node vocabulary.
  make gen-lifecycles-check (and its CI step) keep it in step with the pin.
  Its test pins the shapes, the testing:<variant> key rule, and agreement
  with lifecycleTemplates.gen.ts, which stays the console's source until
  stage 2.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  MSG
  ```

---

## Part A+B self-review

**A — lifecycle task-DAG data, platform-fixed**

- [ ] `lifecycles.json` lives in method-assets beside `stepmanifest.go`; embed-and-parse chosen over a `gen-*` mirror, with the reason stated (authored, not derived) — Task 1, D1.
- [ ] Exported Go API with exact signatures: `Lifecycle`, `LifecyclePhase` (+ `ExitCriterion`, D4), `LifecycleTask`, `LifecycleTaskDispatch`/`LifecycleTaskReview`, `LifecycleFor`, `Lifecycles`, `ValidateLifecycle` — Task 1 Interfaces + Step 5.
- [ ] All 14 type keys present, in order: `requirements`, `architecture`, `projectDesign`, `service`, `frontend`, `testing:plan|harness|perf|systemTest|qaProcess`, `deployment`, `documentation`, `uiDesign`, `integration` — Task 1 Step 4 (COMPLETE JSON, no elisions), pinned by `TestLifecycles_ClosedOrderedTypeSet`.
- [ ] **Deviation from the brief, deliberate:** the QA key is `testing:qaProcess` (the `TestingVariant` wire name), not `testing:qa` — D2. The other half of the plan must use the same rule (`t.String()`, or `"testing:" + v.String()`).
- [ ] service/frontend = Figure A-1 fork, `someConstruction`/`testClient` not nodes, trunk authored first — `TestLifecycles_ServiceAndFrontendForkPerFigureA1`; linear types = one dispatch + one review gate per phase — `TestLifecycles_LinearTypesAreOneDispatchAndOneGatePerPhase`; projectDesign = ONE review, no dispatch; requirements 15/20/35/30; architecture one pair, 100 — `TestLifecycles_DesignActivities`.
- [ ] Existing types reuse the ten ledger `MethodTask` ids and the canonical phase ids — D3, `TestLifecycles_ConstructionFamilyReusesTheLedgerTaskIDs`.
- [ ] Weights, labels, task titles, exit criteria, commands copied verbatim from `profileRows`/`CommandFor` — Task 1 Step 4; proven from the app side by Task 3 and again from the webApp side by Task 5's agreement test.
- [ ] Validation tests, real code: acyclic (authored-order rule D7 + an independent DFS), exactly one gate per phase and it is a review task in that phase, weights sum to 100, every `reviews` target is an ancestor dispatch task (empty only where no dispatch exists — and only projectDesign is such), unique ids, known kinds; plus single root, dispatch completeness, gate-closes-its-phase, `workerClass == ManifestFor(command).Agent` — Task 1 Step 2.
- [ ] App-side parity test, real code, in a layer-legal place (`projectstate/access_test.go`; `TestMethodLayering` has no method-assets import rule — reasoning in Task 3's preamble): every (ActivityType, TestingVariant) incl. `uiDesign` and `integration` — Task 3 Step 2.
- [ ] Platform release: tests, commit with `--author` AND `-c user.name/-c user.email`, ANNOTATED tag with the same identity override (v0.8.0's tagger leaked `uitests`), next-free-version check, **STOP: requires founder confirmation** before tag + push — Task 1 Step 8, Task 2.
- [ ] App pin bump: real module path, `go get …@v0.9.0`, `make claude-assets`, full drift-target list from the 2026-09-12 precedent — Task 3 Steps 4–7.

**B — webApp generated lifecycles**

- [ ] `server/cmd/gen-lifecycles` mirrors `gen-uiprofiles` (main.go + main_test.go, `-out` only) — Task 4; `repostructure_test.go` `allowedCmd` gains it — Task 4 Step 5; Makefile `gen-lifecycles` / `gen-lifecycles-check` + `.PHONY` + `gen:` aggregate — Task 5 Step 3; CI step in `server-checks.yml` — Task 5 Step 7.
- [ ] Exact emitted TS specified: types + `LIFECYCLES` + `lifecycleFor(typeKey)` — Task 4 Step 3 constants (verbatim), Task 5 Interfaces (compact).
- [ ] Lines up with the prototype's `lifecycleGraphTypes.ts` (same static field names, derived `revisionGroup`, `Def`-suffixed names to avoid the collision, already `revision` not `iteration`) — Task 4 preamble; compile-time assertion is conditional on the prototype being tracked — Task 5 Step 5.
- [ ] node:test for the generated file — Task 5 Step 1; prettier/eslint expectations for generated files (NOT exempt; fix the generator, never `prettier --write`) — Task 4 preamble, Task 5 Step 6.
- [ ] `lifecycleTemplates.gen.ts` stays untouched — stated in Tasks 4 and 5, enforced by Task 5 Step 9's `git diff --exit-code`.

**For whoever assembles the full plan**

- [ ] Add `gen-lifecycles-check` to the plan-wide "every task ends green" drift list.
- [ ] The production `(ActivityType, TestingVariant) → type key` function is NOT produced here (Task 3's `lifecycleTypeKey` is test-only); the half that implements `QueryActivityView` owns it and must follow D2.

**What was verified while writing, and what was not**

- Verified by execution (no repo file written): every Go block is `gofmt`-clean; `lifecycles.json` parses, and a Python mirror of every `ValidateLifecycle` rule passes on it; the generator's exact TS constants + the data are prettier-stable and eslint-clean under this repo's config; the webApp test text is prettier-clean, type-aware-eslint-clean, and all eight tests PASS against the generator's expected output — including agreement with the committed `lifecycleTemplates.gen.ts` for all eleven profiles.
- NOT verified (nothing was compiled — this half was written read-only): that the Go code type-checks and the platform's `gocognit`/`gocyclo` 10 and `funlen` limits pass on `lifecycles.go` (methods were kept small for it; Task 1 Step 7 is the proof); that the server's `revive`/`gocritic` accept `cmd/gen-lifecycles` and the parity tests (Task 3 Step 7, Task 4 Step 6); the artifactKind vocabulary for construction tasks (D6) has no consumer until stage 2 and should be confirmed there.

---

## Parts C + D — recon findings that shape Tasks 6–9

Verified by reading the code on `activity-experience-proto` @ `cefedfd2`. Every task below leans on these; none is a guess.

- **F1 — the live construction workflow never writes the attempt ledger.** `ActivityConstructionStatus.Attempts` has exactly one production writer: `cmd/backfill-attempts/main.go:863`. `constructactivity.go` only COUNTS attempts in memory (`constructState.taskAttempts`, `nextTaskAttempt` ~L358) to mint the `AttemptID` it stamps on the episode's `TargetRef` (`runPipeline` ~L1384–1387). Gate attempts (`designReview#n` …) are written by nothing at all. So "today's data" for a live-run activity is: **episodes** (`TargetRef = "<activityId>:<workTask>:<n>"`, work tasks only), **`OperatorNotes`** (`Kind: sendBack`, `Gate: <phase wire name>`, `RecordedAt`, `DeliveredToAttemptID`), the stored **`Phases[]`** (`Completed`/`CompletedAt`, written by `RecordPhaseCompleted`), and the **live session view**. A backfilled activity has the ledger (work AND gate attempts) and usually none of the rest. Task 7's derivation therefore runs over a NORMALIZED attempt list built from both sources (rule N1–N4 there); it does not assume the ledger is complete.
- **F2 — `phase.String()` is not the only reason `ProposeReviews` errors.** `reviewengine.go` ~L158 rejects an empty `componentID`, and `N-STP` / `N-IT` carry none (`hydrateConstructionActivity` ~L1545 sets `ComponentID` only when the plan item names a component; slot 9 confirms both are empty). Fixing the kind alone would leave every gate of both noncoding activities with a nil `ReviewSet`. Task 6 fixes both.
- **F3 — no production Engine imports `projectstate`** (`grep -l 'resourceaccess/projectstate"' internal/engine/*/*.go` minus tests = empty; Engines carry their own `$defs` copies), and an Engine package may export nothing but its generated surface (`TestGeneratedOnlyPublic`, `arch_test.go` ~L265; no `internal/engine/*` key exists in `encapsulationAllowlistData`). So the (ActivityType, ActivityMethodPhase) → kind table cannot live in `internal/engine/review` without either a new sideways-looking import or a new allowlist entry. Task 6 puts the **vocabulary** in the engine's contract as a generated typed enum and the **table** in the Manager beside its one caller, typed on both sides; spec §8 stage 2 moves the table into the engine when `ProposeReviews` takes `activityType` itself.
- **F4 — file layout is a gate** (`TestFileLayout`): one impl file + one file per workflow + ONE test file per package. All new Manager code goes in `constructionmanager.go` (façade/pure helpers) or `constructactivity.go` (workflow-side), all new Manager tests in `manager_test.go`, all engine tests in `engine_test.go`. No new `.go` files anywhere in Parts C + D.
- **F5 — `TestGreenFixtureAdvisoriesFire` needs a comment edit only.** It indexes findings by `RuleID → Severity` (`indexBySeverity`), and `DH-CONTRACT-OPCOUNT-MAX` already fires at Warning for `systemDesignManager` (13 ops; `engine_test.go:59–60`). A second 13-op contract changes neither the key nor the severity, so the assertion stays green. No test counts findings (`grep -n 'len(findings)' engine_test.go` = the non-empty guard at :29 only).
- **F6 — `constructionManager` is REST + MCP exposed** (`cmd/clientgen/main.go:62` `exposedManagers`), a `Query*` op with only scalar params is emitted as `GET` (`operationsQueryOperatedSystemView`), and a param whose schema is `$ref: ProjectID|ActivityID` becomes a PATH segment while a bare `{"type":"string"}` becomes a query parameter (`get-session-state/{projectID}/{activityID}` vs `list-episodes-for-activity/{projectID}`).
- **F7 — adding a Manager op adds no Temporal activity or workflow** (`activities.gen.go`/`invokers.gen.go` are generated from the Manager's DEPS' ops, not its own), so `registered_names_test.go`'s golden is untouched by Parts C + D.

---

### Task 6: Type the review artifact kind, map it totally, and stop swallowing the engine's error (Part D)

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.reviewEngine` (`$defs` + the `artifactKind` param) and `.serviceContracts.constructionManager["$defs"].ConstructionSessionView.properties` (one additive property). Hand-edit, self-amendment procedure.
- Regenerate (never hand-edit): `server/internal/engine/review/contract.gen.go`, `server/internal/engine/review/fake/fake.gen.go`, `server/internal/manager/construction/contract.gen.go`, `server/internal/manager/construction/fake/fake.gen.go`, `server/internal/resourceaccess/projectstate/toolcatalog.gen.go`, `server/api/openapi.yaml`, `server/internal/client/{web,mcp}/**`, `systemtests/internal/sdk/**`, `webApp/src/contracts/schema.ts`.
- Modify: `server/internal/engine/review/reviewengine.go` (package doc L1–50, `reviewKind`/`artifactKindByName` L81–117, `ProposeReviews` L145–178, `reviewersFor` L183–241)
- Modify: `server/internal/engine/review/engine_test.go` (all five tests)
- Modify: `server/internal/manager/construction/constructactivity.go` (`runPhaseGate` L1473–1477; `proposeReviewSet` L2205–2225)
- Modify: `server/internal/manager/construction/constructionmanager.go` (`constructState` L1838; `view()` L1919–1941)
- Modify: `server/internal/manager/construction/manager_test.go` (`fakeReview` L2981–2993; new tests after `Test_SessionView_PhaseGate_ReportsTheOccurrenceAndClearsOnDecision` ~L7525)

**Interfaces:**
- Consumes: `review.ReviewEngine.ProposeReviews` (today: `artifactKind string`); `projectstate.ActivityType` (7 values), `projectstate.ActivityMethodPhase` (5 values), `projectstate.ProfileFor(t, v).PhaseIDs()`; `constructionActivity{Type, ComponentID}`; the test harness `gateDeps`, `registerConstruct`, `newFakeProjectStateWithPolicy`, `replayGatedOn`, `b12View`, `b12Decide`, `b12Run` (all in `manager_test.go`).
- Produces:
  - generated `review.ReviewArtifactKind string` with constants `ReviewKindDetailedDesign|ReviewKindConstruction|ReviewKindIntegration|ReviewKindNoncoding|ReviewKindUIDesign|ReviewKindUICode` (wire values unchanged: `"DetailedDesign"` …);
  - `ProposeReviews(rc fweng.Context, change ReviewChange, componentID string, artifactKind ReviewArtifactKind, architectureGraph string, contracts []string) (ReviewSet, error)`;
  - Manager-local `func reviewArtifactKindFor(act constructionActivity, p projectstate.ActivityMethodPhase) review.ReviewArtifactKind` — total, no error, no bool;
  - `ConstructionSessionView.ReviewSetError *string` (wire `reviewSetError`, omitted when the engine answered).

**The table (35 cells = 7 types × 5 canonical phases, every one decided; justified from `reviewersFor`'s own words):**

| Activity type | requirements | detailed_design | test_plan | construction | integration | Why |
|---|---|---|---|---|---|---|
| service | Noncoding | DetailedDesign | Noncoding | Construction | Integration | Figure A-1 verbatim. SRS and STP are documents: "a single architect sign-off". The contract is reviewed "against the architecture" and may be amended by agreement; code "against the committed detailed-design"; integration "against the architecture call-chains". |
| deployment | Noncoding | DetailedDesign | Noncoding | Construction | Integration | A provisioning spec is this activity's detailed design of a Resource component (every derived `R-*` names one); the change is reviewed against that spec; convergence is its integration. |
| frontend | Noncoding | UIDesign | Noncoding | UICode | Integration | "UI designer reviews the concept" / "senior reviews the UI code against the UI-design". UX requirements and flows are documents. |
| uiDesign | Noncoding | UIDesign | Noncoding | UICode | Integration | Carries only requirements + detailed_design today; the off-profile cells take frontend's values so a future profile change cannot make the function partial. |
| integration | Noncoding | Noncoding | Noncoding | Noncoding | Integration | Carries only `integration`. Off-profile cells: architect sign-off, the most conservative reviewer. |
| testing (all 5 variants) | Noncoding | Noncoding | Noncoding | Noncoding | Noncoding | No architecture component exists to review against (`N-STP`/`N-IT` have no `componentId`), and their `integration` phase is a LABEL over the canonical id ("Plan Review", "Sign-off"), not a call-chain integration. |
| documentation | Noncoding | Noncoding | Noncoding | Noncoding | Noncoding | Same: a document, signed off by the architect. |

**Do not wire `lifecycles.json`'s `artifactKind` into this call.** partAB's lifecycle tasks carry an `artifactKind` (`SRS`, `STP`, `DetailedDesign`, `Construction`, `Integration`, …). It names the artifact a task PRODUCES and has no consumer until stage 2; it is a different vocabulary from `ReviewArtifactKind` (`SRS` and `STP` are not review kinds at all), so passing it to `ProposeReviews` would recreate this defect with new strings. In stage 0 the table above — `reviewArtifactKindFor` — is the single source of truth for the engine call. Stage 2's generalized `ProposeReviews(activityType, taskId, artifactKind, …)` is where the data field gets its consumer.

**Component rule (F2):** `DetailedDesign`, `Construction`, `UIDesign`, `UICode` review a COMPONENT's artifact and keep the engine's non-empty-`componentID` precondition. `Integration` and `Noncoding` review against the system-level architecture and drop it. A component-scoped cell for an activity with NO component (a nonstructural coding activity — `ActivityItem.ComponentID` doc, `projectstateaccess.go` ~L4318) degrades to `Noncoding` in the Manager's table: there is no contract or UI design to hold it against, so the architect signs it off.

**Surfacing decision (replay-safe, no `GetVersion`):** the error is LOGGED at Error through `workflow.GetLogger` and SHOWN on the session view as `reviewSetError`; the gate still opens. It is not returned from `runPhaseGate`, for two reasons. (1) The reviewer set is display-only in v1 (`proposeReviewSet` doc, ~L2211) — failing a construction activity because a roster could not be drawn would turn a display defect into lost work. (2) Returning it would end in-flight executions at a point their history does not, which needs a `GetVersion` fence; an assignment and a log line emit NO commands, the same argument `constructactivity.go` ~L1541–1548 ("The human stage … no GetVersion") already makes for `awaitingSince`, so every fixture under `testdata/replay/{pre-b1,post-b1,post-b17,pre-d}` replays unchanged. `noteDelivery` (`changeOperatorNoteDelivery`, ~L1188) is the precedent for the OTHER case — a change that adds an Activity call — and is deliberately not followed here because nothing is added to history. With the typed enum and the total table, the only errors left are programmer errors (empty `ActivityID`, an engine invariant), which is exactly what a loud view field is for.

- [ ] **Step 1: Write the failing regression test** — add to `server/internal/manager/construction/manager_test.go`, directly after `Test_SessionView_PhaseGate_ReportsTheOccurrenceAndClearsOnDecision`. It wires the REAL engine, because the permissive `fakeReview` is what hid the defect. It compiles on today's code.

  ```go
  // A gated phase must show its reviewer set. The Manager used to pass the phase's wire
  // name ("detailed_design") where the engine accepts only its own kind vocabulary
  // ("DetailedDesign"), and runPhaseGate dropped the error, so ReviewSet was nil at every
  // gate of every activity. The REAL engine is wired here: a fake that accepts any kind is
  // how the defect stayed hidden.
  func Test_SessionView_PhaseGate_CarriesTheReviewerSet(t *testing.T) {
  	var ts testsuite.WorkflowTestSuite
  	env := ts.NewTestWorkflowEnvironment()
  	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseDetailedDesign))
  	deps := gateDeps(ps)
  	deps.Review = review.NewReviewEngine()
  	registerConstruct(env, newWorkflows(deps), ps, newFakePipeline())
  	var atGate ConstructionSessionView
  	env.RegisterDelayedCallback(func() { atGate = b12View(t, env) }, 10*time.Second)
  	env.RegisterDelayedCallback(b12Decide(env, "detailed_design", PhaseApprove), 30*time.Second)
  	b12Run(env)
  	if err := env.GetWorkflowError(); err != nil {
  		t.Fatalf("workflow error: %v", err)
  	}
  	if atGate.Stage != StageAwaitingApproval || b12Gate(atGate) != "detailed_design" {
  		t.Fatalf("not at the detailed_design gate: stage=%v gate=%q", atGate.Stage, b12Gate(atGate))
  	}
  	if atGate.ReviewSet == nil || len(atGate.ReviewSet.Reviewers) == 0 {
  		t.Fatalf("the detailed_design gate shows no reviewers: ReviewSet=%+v", atGate.ReviewSet)
  	}
  	if r := atGate.ReviewSet.Reviewers[0]; r.Role != "architect" || !r.MayAmend {
  		t.Fatalf("a service's detailed design is reviewed by the architect, who may amend; got %+v", r)
  	}
  }
  ```

- [ ] **Step 2: Run it; confirm it fails for the right reason.**
  ```bash
  cd server && GOWORK=off go test ./internal/manager/construction/ -run 'Test_SessionView_PhaseGate_CarriesTheReviewerSet' -count=1
  ```
  Expected: `FAIL` with `the detailed_design gate shows no reviewers: ReviewSet=<nil>`. If it fails at "not at the detailed_design gate" instead, the harness moved — stop and re-read `b12Run`.

- [ ] **Step 3: Amend the two contracts in `.aiarch/state/project.json`.**

  (a) `.serviceContracts.reviewEngine.interface.operations[0].params` — replace the third param
  ```json
  { "name": "artifactKind", "schema": { "type": "string" } }
  ```
  with
  ```json
  { "name": "artifactKind", "schema": { "$ref": "#/$defs/ReviewArtifactKind" } }
  ```
  (b) `.serviceContracts.reviewEngine["$defs"]` — add, beside `ReviewChange` (shape copied from the repo's string-enum idiom, e.g. `ActivityMethodPhase`):
  ```json
  "ReviewArtifactKind": {
    "type": "string",
    "enum": ["DetailedDesign", "Construction", "Integration", "Noncoding", "UIDesign", "UICode"],
    "x-enum-varnames": ["ReviewKindDetailedDesign", "ReviewKindConstruction", "ReviewKindIntegration", "ReviewKindNoncoding", "ReviewKindUIDesign", "ReviewKindUICode"],
    "x-go-base": "string"
  }
  ```
  The six wire strings are today's `artifactKindByName` keys, unchanged, so the internal MCP tool `reviewProposeReviews` (agents' review routing) keeps accepting what it accepts now and starts rejecting everything else at the schema.

  (c) `.serviceContracts.constructionManager["$defs"].ConstructionSessionView.properties` — add directly after the `"reviewSet"` property (NOT to `required`):
  ```json
  "reviewSetError": {
    "type": "string",
    "description": "Why reviewSet is absent at a gate: the review engine refused to propose reviewers. It is a defect in the Manager's call or in the engine, never an operator error, and the gate itself is unaffected — Approve and SendBack work. Omitted whenever the engine answered."
  }
  ```

- [ ] **Step 4: Regenerate the Go contract layer.**
  ```bash
  cd server && make gen-models
  git status --short -- internal/engine/review internal/manager/construction
  ```
  Expected modified: `internal/engine/review/contract.gen.go` (new `type ReviewArtifactKind string` + six constants; `ProposeReviews(... artifactKind ReviewArtifactKind ...)`), `internal/engine/review/fake/fake.gen.go`, `internal/manager/construction/contract.gen.go` (`ReviewSetError *string` on `ConstructionSessionView`). `GOWORK=off go build ./...` now FAILS in `reviewengine.go`, `engine_test.go`, `constructactivity.go` and `manager_test.go` — that is the compiler doing the fix's job: `phase.String()` is no longer assignable.
  - [ ] **Verify first:** open `contract.gen.go` and confirm the generated names are exactly `ReviewArtifactKind` / `ReviewKind*` and the field is `ReviewSetError *string`. If modelgen renders the optional string differently (it renders `awaitingGate` as `AwaitingGate *string` today, so a pointer is expected), use what it generated in Steps 6–8.

- [ ] **Step 5: Rewrite the engine on its generated vocabulary** — `server/internal/engine/review/reviewengine.go`.

  Delete `type reviewKind`, its seven constants and `artifactKindByName` (L81–117). Replace `ProposeReviews` and `reviewersFor` with:

  ```go
  // ProposeReviews implements ReviewEngine. It validates the input and computes the
  // policy's reviewer set for the artifact kind.
  func (ReviewEngineImpl) ProposeReviews(
  	_ fweng.Context, // pure engine: carries identity/cancellation, ignored by v1 policy
  	change ReviewChange,
  	componentID string,
  	artifactKind ReviewArtifactKind,
  	_ string, // architectureGraph — reserved for a future policy refinement (v1 ignores)
  	_ []string, // contracts — reserved for a future policy refinement (v1 ignores)
  ) (ReviewSet, error) {
  	if change.ActivityID == "" {
  		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
  			"ProposeReviews: change has empty ActivityID (Manager failed to assemble a valid ReviewChange)")
  	}
  	reviewers, known := reviewersFor(artifactKind)
  	if !known {
  		// The type is a string on the wire (the internal MCP tool decodes JSON into it), so
  		// an out-of-vocabulary value is still reachable and still refused here.
  		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
  			"ProposeReviews: unrecognised artifactKind "+quote(string(artifactKind)))
  	}
  	if componentScoped(artifactKind) && componentID == "" && change.ComponentID == "" {
  		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
  			"ProposeReviews: "+string(artifactKind)+" reviews one component's artifact and no componentID was given")
  	}
  	if len(reviewers) == 0 {
  		return ReviewSet{}, fweng.New(fweng.InternalInvariant,
  			"ProposeReviews: policy produced an empty reviewer set for a recognised kind "+quote(string(artifactKind)))
  	}
  	return ReviewSet{Reviewers: reviewers}, nil
  }

  // componentScoped reports whether a kind reviews ONE component's artifact (its service
  // contract, its code, its UI design, its UI code) and so cannot be proposed without a
  // component. Integration and Noncoding review against the system-level architecture:
  // the system test plan and system testing have no component at all.
  func componentScoped(kind ReviewArtifactKind) bool {
  	switch kind {
  	case ReviewKindDetailedDesign, ReviewKindConstruction, ReviewKindUIDesign, ReviewKindUICode:
  		return true
  	case ReviewKindIntegration, ReviewKindNoncoding:
  		return false
  	}
  	return false
  }

  // reviewersFor is the package-internal ReviewPolicy: the deterministic
  // artifactKind → reviewer-set mapping (the-method-review-routing). known is false for
  // a value outside the generated vocabulary.
  func reviewersFor(kind ReviewArtifactKind) (reviewers []Reviewer, known bool) {
  	switch kind {
  	case ReviewKindDetailedDesign:
  		// The architect reviews the service-contract against the architecture; the
  		// architect+constructor may re-stage an amended contract by agreement.
  		return []Reviewer{{Role: roleArchitect, Perspective: perspectiveArchitecture, ReferenceArtifact: "architecture", MayAmend: true}}, true
  	case ReviewKindConstruction:
  		// A senior reviews the code against the committed detailed-design.
  		return []Reviewer{{Role: roleSeniorReviewer, Perspective: perspectiveDetailedDesign, ReferenceArtifact: "detailedDesign", MayAmend: false}}, true
  	case ReviewKindIntegration:
  		// A senior reviews integration against the architecture call-chains.
  		return []Reviewer{{Role: roleSeniorReviewer, Perspective: perspectiveArchitecture, ReferenceArtifact: "architecture", MayAmend: false}}, true
  	case ReviewKindNoncoding:
  		// A single architect sign-off.
  		return []Reviewer{{Role: roleArchitect, Perspective: perspectiveArchitecture, ReferenceArtifact: "architecture", MayAmend: false}}, true
  	case ReviewKindUIDesign:
  		// A UI designer reviews the concept; designer+constructor may re-stage.
  		return []Reviewer{{Role: roleUIDesigner, Perspective: perspectiveUIDesign, ReferenceArtifact: "uiDesign", MayAmend: true}}, true
  	case ReviewKindUICode:
  		// A senior reviews the UI code against the committed UI-design.
  		return []Reviewer{{Role: roleSeniorReviewer, Perspective: perspectiveUIDesign, ReferenceArtifact: "uiDesign", MayAmend: false}}, true
  	}
  	return nil, false
  }
  ```
  `exhaustive` (`.golangci.yml`: `check: [switch, map]`) now fails the build the day a seventh kind is added to the contract without a reviewer rule — the protection `artifactKindByName`'s string keys could not give. In the package doc, replace the paragraph describing `artifactKindByName` ("mirrors constructionManager's ActivityKind.String()…") with one sentence: the kind vocabulary is the generated `ReviewArtifactKind`; the Manager owns the (activity type, lifecycle phase) → kind table until `ProposeReviews` takes the activity type itself (spec 2026-09-20 §5.4, stage 2).

- [ ] **Step 6: Update `engine_test.go`.** Change `Test_ProposeReviews_PerKind`'s map key type from `string` to `ReviewArtifactKind` and its six keys to the generated constants; leave the other call sites' untyped string literals (`"Construction"`, `"NotAKind"`) as they are — an untyped constant converts. Replace `Test_ProposeReviews_EmptyComponent_ContractMisuse` with:

  ```go
  // A component-scoped kind needs a component; the two system-level kinds do not — the
  // system test plan and system testing have none, and used to get no reviewers at all.
  func Test_ProposeReviews_ComponentIsRequiredOnlyWhereThereIsOne(t *testing.T) {
  	e := NewReviewEngine()
  	noComponent := ReviewChange{ActivityID: "N-STP"}
  	for _, kind := range []ReviewArtifactKind{ReviewKindDetailedDesign, ReviewKindConstruction, ReviewKindUIDesign, ReviewKindUICode} {
  		_, err := e.ProposeReviews(fweng.Context{}, noComponent, "", kind, "", nil)
  		if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
  			t.Fatalf("%s with no component: want ContractMisuse, got %s", kind, got)
  		}
  	}
  	for _, kind := range []ReviewArtifactKind{ReviewKindIntegration, ReviewKindNoncoding} {
  		set, err := e.ProposeReviews(fweng.Context{}, noComponent, "", kind, "", nil)
  		if err != nil || len(set.Reviewers) == 0 {
  			t.Fatalf("%s with no component: want reviewers, got %+v, %v", kind, set, err)
  		}
  	}
  }

  // The Manager's old argument, pinned: a lifecycle phase's wire name is not a kind.
  func Test_ProposeReviews_APhaseWireNameIsNotAKind(t *testing.T) {
  	_, err := NewReviewEngine().ProposeReviews(fweng.Context{}, validChange(), "c", "detailed_design", "", nil)
  	if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
  		t.Fatalf("want ContractMisuse, got %s", got)
  	}
  }
  ```
  Run: `GOWORK=off go test ./internal/engine/review/ -count=1` → `ok`.

- [ ] **Step 7: The Manager's table and the surfaced error** — `constructactivity.go`. Replace `proposeReviewSet` (L2217–2225) and add the table directly below it:

  ```go
  func (wf *workflows) proposeReviewSet(in constructActivityInput, phase projectstate.ActivityMethodPhase, state *constructState) (ReviewSet, error) {
  	change := review.ReviewChange{ActivityID: string(in.ActivityID), ComponentID: in.Activity.ComponentID}
  	set, err := wf.Review.ProposeReviews(fweng.Context{Context: context.Background()},
  		change, in.Activity.ComponentID, reviewArtifactKindFor(in.Activity, phase), "", state.reviewContracts)
  	if err != nil {
  		return ReviewSet{}, err
  	}
  	return reviewSetFromEngine(set), nil
  }

  // reviewArtifactKindFor is the TOTAL (activity type, lifecycle phase) → review kind
  // table: all 35 cells are decided, so it returns no error and no bool, and a gate can
  // never again go without reviewers because of what the Manager passed. It replaces
  // phase.String(), whose wire names ("detailed_design") were never in the engine's
  // vocabulary ("DetailedDesign") — the engine refused every call and the gate dropped
  // the error.
  //
  // It lives here, not in the engine, because its inputs are projectstate types no Engine
  // imports and an Engine package exports only its generated surface; the engine owns the
  // VOCABULARY (the generated review.ReviewArtifactKind), so a wrong value does not
  // compile. Spec 2026-09-20 §5.4 moves the table into the engine with the generalized
  // ProposeReviews(activityType, …) signature (stage 2).
  //
  // A component-scoped kind for an activity with NO component degrades to Noncoding:
  // there is no contract or UI design to hold the work against, so the architect signs it
  // off. Off-profile cells (a phase the type's profile does not carry) are decided too, so
  // a profile change cannot make this partial. The rows are justified in the plan
  // (docs/superpowers/plans/…stage-0…, Task 6).
  func reviewArtifactKindFor(act constructionActivity, p projectstate.ActivityMethodPhase) review.ReviewArtifactKind {
  	kind := review.ReviewKindNoncoding
  	switch act.Type {
  	case projectstate.ActivityTypeService, projectstate.ActivityTypeDeployment:
  		kind = componentReviewKind(p, review.ReviewKindDetailedDesign, review.ReviewKindConstruction)
  	case projectstate.ActivityTypeFrontend, projectstate.ActivityTypeUIDesign:
  		kind = componentReviewKind(p, review.ReviewKindUIDesign, review.ReviewKindUICode)
  	case projectstate.ActivityTypeIntegration:
  		kind = componentReviewKind(p, review.ReviewKindNoncoding, review.ReviewKindNoncoding)
  	case projectstate.ActivityTypeTesting, projectstate.ActivityTypeDocumentation:
  		// A document or a test asset with no architecture component; its "integration"
  		// phase is a label (Plan Review, Sign-off, Doc Review), not a call-chain integration.
  	}
  	if act.ComponentID == "" && kind != review.ReviewKindIntegration {
  		return review.ReviewKindNoncoding
  	}
  	return kind
  }

  // componentReviewKind is one row of the table for a type that designs and then builds:
  // its documents (requirements, test plan) are signed off, its design and its build take
  // the row's two kinds, and its integration is reviewed against the call chains.
  func componentReviewKind(p projectstate.ActivityMethodPhase, design, build review.ReviewArtifactKind) review.ReviewArtifactKind {
  	switch p {
  	case projectstate.MethodPhaseRequirements, projectstate.MethodPhaseTestPlan:
  		return review.ReviewKindNoncoding
  	case projectstate.MethodPhaseDetailedDesign:
  		return design
  	case projectstate.MethodPhaseConstruction:
  		return build
  	case projectstate.MethodPhaseIntegration:
  		return review.ReviewKindIntegration
  	}
  	return review.ReviewKindNoncoding
  }
  ```

  In `runPhaseGate`, replace L1473–1477 (the comment and the `if rs, e := …; e == nil` block) with:

  ```go
  	// Surface the reviewer set on the session view. The set is display-only in v1 — the
  	// human Approve/SendBack is the enforced gate — so an engine refusal does not fail the
  	// activity; it is LOGGED and SHOWN (reviewSetError), never dropped. An assignment and a
  	// log line emit no commands, so this needs no version gate (same argument as the human
  	// stage below) and every replay fixture replays unchanged.
  	state.reviewSet, state.reviewSetError = nil, ""
  	if rs, e := wf.proposeReviewSet(in, phase, state); e != nil {
  		state.reviewSetError = e.Error()
  		workflow.GetLogger(ctx).Error("review engine refused to propose reviewers; the gate opens without a reviewer set",
  			"activityId", in.ActivityID, "phase", phase.String(), "err", e.Error())
  	} else {
  		state.reviewSet = &rs // NOTE: *ReviewSet (B6)
  	}
  ```
  Resetting both fields on entry also ends a smaller lie: `reviewSet` was never cleared, so a later gate that failed to propose would have shown the PREVIOUS gate's reviewers.

  In `constructionmanager.go`: add `reviewSetError string` to `constructState` directly under `reviewSet *ReviewSet` (L1838) with the doc line `// reviewSetError is why reviewSet is nil at the current gate ("" when the engine answered).`, and in `view()` after the `ReviewSet:` initialisation add:
  ```go
  	if s.reviewSetError != "" {
  		e := s.reviewSetError
  		v.ReviewSetError = &e
  	}
  ```

- [ ] **Step 8: Make the Manager's fake unable to hide this class again** — `manager_test.go` L2981–2993. The fake now VALIDATES through the real engine before answering with its script, and records what it was asked:

  ```go
  // fakeReview returns a scripted reviewer set — but only for a call the REAL engine
  // accepts. It used to accept anything, which is how the Manager passed a lifecycle
  // phase's wire name as the artifact kind for months with every test green. kinds
  // records each artifactKind the Manager passed, in call order; err, when set, is
  // returned instead (the engine-refusal path).
  type fakeReview struct {
  	set   review.ReviewSet
  	err   error
  	kinds []review.ReviewArtifactKind
  }

  func (r *fakeReview) ProposeReviews(rc fweng.Context, change review.ReviewChange, componentID string, artifactKind review.ReviewArtifactKind, graph string, contracts []string) (review.ReviewSet, error) {
  	r.kinds = append(r.kinds, artifactKind)
  	if _, err := review.NewReviewEngine().ProposeReviews(rc, change, componentID, artifactKind, graph, contracts); err != nil {
  		return review.ReviewSet{}, fmt.Errorf("fakeReview: the real engine refuses this call: %w", err)
  	}
  	if r.err != nil {
  		return review.ReviewSet{}, r.err
  	}
  	return r.set, nil
  }
  ```
  Then add the table's tests and the refusal test beside the Step-1 test:

  ```go
  // Every (type, variant, profile phase) the pump can dispatch — with and without a
  // component — gets reviewers from the REAL engine. This is the totality proof: no gate
  // can lose its reviewer set to the Manager's argument again.
  func Test_ReviewArtifactKindFor_IsTotalOverEveryDispatchablePhase(t *testing.T) {
  	types := []projectstate.ActivityType{
  		projectstate.ActivityTypeService, projectstate.ActivityTypeFrontend, projectstate.ActivityTypeTesting,
  		projectstate.ActivityTypeDeployment, projectstate.ActivityTypeDocumentation,
  		projectstate.ActivityTypeUIDesign, projectstate.ActivityTypeIntegration,
  	}
  	variants := []projectstate.TestingVariant{
  		projectstate.TestVariantPlan, projectstate.TestVariantHarness, projectstate.TestVariantPerf,
  		projectstate.TestVariantSystemTest, projectstate.TestVariantQAProcess,
  	}
  	eng := review.NewReviewEngine()
  	for _, typ := range types {
  		for _, v := range variants {
  			for _, p := range projectstate.ProfileFor(typ, v).PhaseIDs() {
  				for _, componentID := range []string{"comp-1", ""} {
  					act := constructionActivity{ActivityID: "A", Type: typ, Variant: v, ComponentID: componentID}
  					kind := reviewArtifactKindFor(act, p)
  					set, err := eng.ProposeReviews(fweng.Context{}, review.ReviewChange{ActivityID: "A", ComponentID: componentID}, componentID, kind, "", nil)
  					if err != nil || len(set.Reviewers) == 0 {
  						t.Errorf("%s/%s/%s component=%q → %s: reviewers=%v err=%v", typ, v, p, componentID, kind, set.Reviewers, err)
  					}
  				}
  			}
  		}
  	}
  }

  // The rows a reader would check by hand.
  func Test_ReviewArtifactKindFor_PinnedRows(t *testing.T) {
  	svc := constructionActivity{Type: projectstate.ActivityTypeService, ComponentID: "c"}
  	spa := constructionActivity{Type: projectstate.ActivityTypeFrontend, ComponentID: "web-client"}
  	ui := constructionActivity{Type: projectstate.ActivityTypeUIDesign, ComponentID: "web-client"}
  	res := constructionActivity{Type: projectstate.ActivityTypeDeployment, ComponentID: "github"}
  	stp := constructionActivity{Type: projectstate.ActivityTypeTesting, Variant: projectstate.TestVariantPlan}
  	bare := constructionActivity{Type: projectstate.ActivityTypeService} // nonstructural: no component
  	cases := []struct {
  		name string
  		act  constructionActivity
  		p    projectstate.ActivityMethodPhase
  		want review.ReviewArtifactKind
  	}{
  		{"service requirements", svc, projectstate.MethodPhaseRequirements, review.ReviewKindNoncoding},
  		{"service detailed design", svc, projectstate.MethodPhaseDetailedDesign, review.ReviewKindDetailedDesign},
  		{"service test plan", svc, projectstate.MethodPhaseTestPlan, review.ReviewKindNoncoding},
  		{"service construction", svc, projectstate.MethodPhaseConstruction, review.ReviewKindConstruction},
  		{"service integration", svc, projectstate.MethodPhaseIntegration, review.ReviewKindIntegration},
  		{"frontend design", spa, projectstate.MethodPhaseDetailedDesign, review.ReviewKindUIDesign},
  		{"frontend construction", spa, projectstate.MethodPhaseConstruction, review.ReviewKindUICode},
  		{"uiDesign concept", ui, projectstate.MethodPhaseDetailedDesign, review.ReviewKindUIDesign},
  		{"deployment spec", res, projectstate.MethodPhaseDetailedDesign, review.ReviewKindDetailedDesign},
  		{"deployment convergence", res, projectstate.MethodPhaseIntegration, review.ReviewKindIntegration},
  		{"N-STP plan review", stp, projectstate.MethodPhaseIntegration, review.ReviewKindNoncoding},
  		{"no component: design degrades", bare, projectstate.MethodPhaseDetailedDesign, review.ReviewKindNoncoding},
  		{"no component: integration stands", bare, projectstate.MethodPhaseIntegration, review.ReviewKindIntegration},
  	}
  	for _, c := range cases {
  		if got := reviewArtifactKindFor(c.act, c.p); got != c.want {
  			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
  		}
  	}
  }

  // An engine refusal is shown on the view and the gate still works.
  func Test_SessionView_PhaseGate_ShowsAnEngineRefusalAndStillGates(t *testing.T) {
  	var ts testsuite.WorkflowTestSuite
  	env := ts.NewTestWorkflowEnvironment()
  	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseDetailedDesign))
  	deps := gateDeps(ps)
  	deps.Review = &fakeReview{err: errors.New("policy produced an empty reviewer set")}
  	registerConstruct(env, newWorkflows(deps), ps, newFakePipeline())
  	var atGate ConstructionSessionView
  	env.RegisterDelayedCallback(func() { atGate = b12View(t, env) }, 10*time.Second)
  	env.RegisterDelayedCallback(b12Decide(env, "detailed_design", PhaseApprove), 30*time.Second)
  	b12Run(env)
  	if err := env.GetWorkflowError(); err != nil {
  		t.Fatalf("an engine refusal must not fail the activity: %v", err)
  	}
  	if atGate.Stage != StageAwaitingApproval || atGate.ReviewSet != nil {
  		t.Fatalf("stage=%v reviewSet=%+v, want awaitingApproval with no set", atGate.Stage, atGate.ReviewSet)
  	}
  	if atGate.ReviewSetError == nil || !strings.Contains(*atGate.ReviewSetError, "empty reviewer set") {
  		t.Fatalf("reviewSetError = %v, want the engine's reason", atGate.ReviewSetError)
  	}
  }
  ```

- [ ] **Step 9: Run the Manager suite, replay fixtures included.**
  ```bash
  cd server && GOWORK=off go test ./internal/manager/construction/ ./internal/engine/review/ -count=1
  ```
  Expected: `ok` for both. The Step-1 test now passes. If any `testdata/replay` replay test reports non-determinism, STOP: Step 7 must have added a command — it may only assign and log.

- [ ] **Step 10: Regenerate everything downstream of the two contracts.**
  ```bash
  cd server && make gen-client gen-internal-tools gen-temporal
  cd ../webApp && npm run gen:api && npm run gen:ops
  git status --short
  ```
  Expected: `server/api/openapi.yaml` + `server/internal/client/**` (`reviewSetError` on `ConstructionConstructionSessionView`), `toolcatalog.gen.go` (`reviewProposeReviews` input schema gains the enum), `systemtests/internal/sdk/**` (the SDK's session view), `webApp/src/contracts/schema.ts`. `ops.gen.ts` unchanged (no op added).

- [ ] **Step 11: Gates.**
  ```bash
  cd server && git add ../.aiarch/state/project.json . ../systemtests/internal/sdk ../webApp/src/contracts/schema.ts ../webApp/src/api/ops.gen.ts
  make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-uiprofiles-check gen-lifecycles-check derived-plan-check
  make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off make test-short && make lint && make sumtype-check && make fix-check
  cd ../webApp && npm run check
  ```
  `gen-lifecycles-check` is the drift gate partAB Task 5 adds (`lifecycles.gen.ts` vs the pinned method-assets); it exists once Tasks 1–5 have landed, which they have before this task starts. The `*-check` targets diff the working tree against the index, which is why the first line stages exactly this task's paths (never `git add -A` at the repo root — the checkout may hold unrelated untracked work); unstaged, they would report the very files this task regenerated; what must hold is that a SECOND `make gen …` produces no further diff.

- [ ] **Step 12: Commit.**
  ```bash
  git add .aiarch/state/project.json server systemtests/internal/sdk webApp/src/contracts/schema.ts
  git commit -m "$(cat <<'EOF'
  fix(construction): give every phase gate its reviewer set

  The Manager passed the lifecycle phase's wire name as the review artifact kind,
  the engine refused it, and runPhaseGate dropped the error: ReviewSet was nil at
  every gate. The kind is now a generated typed enum, the (type, phase) -> kind
  table is total, system-level kinds no longer need a component (N-STP, N-IT),
  and a refusal is logged and shown as reviewSetError instead of swallowed.
  The Manager's fake review engine validates through the real one.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  EOF
  )"
  ```

---

### Task 7: Derive revisions and task states from today's data — two pure functions (Part C, spec §2)

**Depends on:** Task 3 of this plan (the `method-assets` pin that carries `methodassets.Lifecycle`). Nothing here reads `lifecycles.json` — the tests build their lifecycles as literals — but the TYPES come from that release.

**Files:**
- Modify: `server/internal/manager/construction/constructionmanager.go` — append one new section after the episode facet read ops (after `isEpisodeTraceNotFound`, end of file ~L2503). No new file (F4).
- Modify: `server/internal/manager/construction/manager_test.go` — append one new section at end of file.
- Modify: `server/go.mod` only if Task 3 has not already made `method-assets` a direct requirement (it is already `require`d at `go.mod:12`).

**Layer placement (verified):** `TestMethodLayering` (`arch_test.go` `appArchSpec`) allows any layer to import `github.com/mixofreality-studio/…` (`AllowedImportPrefixes[0]`); it carries no per-layer rule about `method-assets`. Today's production importers are both ResourceAccess (`agenticjob` — `methodassets.ManifestFor`, `sourcecontrol`); no Manager or Engine imports it yet. The Manager is the right first consumer here: the lifecycle is joined with `projectstate` rows, the episode ledger and a Temporal query — three things only a Manager may hold at once — and the façade is ordinary Go, not replayed workflow code, so a data-file read has no determinism cost.
- [ ] **Verify first:** after adding the import in Step 3, `GOWORK=off go test ./internal/ -run 'TestMethodLayering|TestFileLayout|TestGeneratedOnlyPublic' -count=1` must be green. If `arch.Check` rejects the import, STOP and report — do not move the code to an Engine (F3) or add an allowlist entry without a ruling.

**Interfaces:**
- Consumes (method-assets v0.9.0, exactly as partAB Task 1 produces it): `methodassets.Lifecycle{Type string; Phases []LifecyclePhase; Tasks []LifecycleTask}`, `LifecyclePhase{ID, Label string; Weight int; Gate, ExitCriterion string}`, `LifecycleTask{ID, Kind, Title, Phase string; DependsOn []string; Reviews, Command, WorkerClass, ArtifactKind string}`, and the kind constants `methodassets.LifecycleTaskDispatch = "dispatch"` / `methodassets.LifecycleTaskReview = "review"` (phase ids are the `ActivityMethodPhase` wire values and task ids the ten non-conditional `MethodTask` ids — partAB decisions D2/D3). `ExitCriterion`, `Command`, `WorkerClass` and `ArtifactKind` are NOT read here and NOT re-served by `QueryActivityView`: the webApp takes those static facts from `lifecycles.gen.ts` (partAB Task 5); `projectstate.TaskAttempt`, `TaskOutcome` (`OutcomePending|Passed|Rejected|Failed|Skipped`), `AttemptProvenance`, `RecordOrigin`, `AttemptsWorstOrigin` (the exported face of the unexported `worstOrigin` contagion rule), `OperatorNote`, `NoteSendBack`, `NoteComment`, `PhaseCompletion`, `ActivityConstructionStatus{Attempts, OperatorNotes, Phases, CurrentPhase}`, `AttemptID`, `AgentTaskFor`, `GateTaskFor`, `EvidenceRef`/`EvidenceEpisode`; `episode.EpisodeRecord{EpisodeID, TargetRef, StartedAt, EndedAt, Outcome}`; `ConstructionSessionView{Stage, AwaitingGate, AwaitingSince}`.
- Produces (all unexported, package `construction`):
  ```go
  type taskRevision struct {
  	N          int
  	Outcome    string // revRunning|revAwaitingHuman|revPassed|revSentBack|revFailed|revSkipped
  	StartedAt  *time.Time
  	EndedAt    *time.Time
  	AttemptIDs []string
  	EpisodeID  string
  	Note       string
  	Comments   []projectstate.NoteComment
  	Provenance projectstate.RecordOrigin
  }
  type taskView struct {
  	ID        string
  	State     string // taskPending|taskLocked|taskRunning|taskAwaitingHuman|taskPassed|taskSentBack|taskFailed
  	Revisions []taskRevision
  }
  func normalizeAttempts(activityID string, row projectstate.ActivityConstructionStatus, episodes []episode.EpisodeRecord, live *ConstructionSessionView) []projectstate.TaskAttempt
  func deriveTaskViews(lc methodassets.Lifecycle, attempts []projectstate.TaskAttempt, notes []projectstate.OperatorNote, liveGate string) []taskView
  ```

**N-rules — `normalizeAttempts` (why it exists: F1).** Output order: ledger order, then N2 in episode append order, then N3, then N4 in canonical phase order (requirements, detailed_design, test_plan, construction, integration).
- **N1** Every ledger attempt is kept verbatim. One with no evidence gains `Evidence{episode, <EpisodeID>}` when an episode's `TargetRef` equals its `AttemptID`; existing evidence is never overwritten.
- **N2** Every episode whose `TargetRef` parses as `<activityId>:<task>:<n>` and names no ledger attempt becomes a work attempt: outcome `Succeeded→passed`, `Failed|Cancelled|Gap→failed`; `StartedAt`/`EndedAt` from the record; evidence = the episode.
- **N3** A live session in `StageDispatching|StagePipelineRunning` with a stored `CurrentPhase` adds ONE pending attempt for `AgentTaskFor(CurrentPhase)`, numbered after that task's highest — unless one is already pending. (`CurrentPhase` is written by `RecordPhaseStarted`, which the workflow calls only under `gitOn`; without it the activity still reads `running` but no task does. Stage 3 removes the gap by persisting attempts.)
- **N4** Gate attempts are reconstructed per canonical phase `P` (all five are walked, NOT the stored `Phases[]`: `RecordPhaseStarted` seeds that slice only under `gitOn`, and a live gate must show without it) with gate task `G = GateTaskFor(P)`, numbered after the ledger's highest `G`: one **rejected** attempt per `OperatorNote{Kind: sendBack, Gate: P}` in recorded order (`EndedAt = RecordedAt`); then one **passed** attempt (`EndedAt = CompletedAt`) if the STORED phase is `Completed` and the ledger holds no passed `G`; else one **pending** attempt (`StartedAt = AwaitingSince`) if the live session awaits approval at `P`.
- Every N2–N4 attempt is stamped `Origin: backfilled` — the type's own definition, "reconstructed from real evidence recorded elsewhere" — with `Generator: "constructionManager.normalizeAttempts"` and a `Basis` naming the evidence (`episodes[<id>]`, `operatorNotes[<noteId>]`, `phases[<P>].completed`, `session.awaitingGate`, `session.stage`). They are never stamped `observed`: no running system wrote them as attempts.

**R-rules — `deriveTaskViews`, per LifecyclePhase `P` with work task `W` (the phase's `kind: dispatch` task) and gate task `G` (`P.Gate`):**
- **R1 sub-attempts.** `W`'s attempts in `Attempt` order are cut into segments; a segment CLOSES at an attempt that **reached the gate** — outcome `passed` (or `skipped`: nothing more will happen in it). Failed and pending attempts never close one, so every failed/retried attempt before the one that reached the gate lands in the same segment. Segment *n* is revision *n*'s `attemptIds[]`. A trailing open segment is the revision in progress.
- **R2 conditional tasks.** An attempt in phase `P` whose task is neither `W` nor `G` (`someConstruction`, `testClient`) is a sub-attempt of `W`: it joins the first revision whose reaching attempt ended at or after it started, else the last revision.
- **R3 shared numbering.** `G`'s *n*-th attempt (by `Attempt`) is revision *n* of the review task. `W` and `G` number independently but from the same cut, so revision *n* of one is revision *n* of the other; either side may be missing (a partial backfill has gate attempts with no work attempts — silence is not denial).
- **R4 send-back note.** Let `S` = the activity's `sendBack` notes with `Gate == P.ID`, in slice order (append-only = `RecordedAt` order), and `R` = `G`'s `rejected` attempts in `Attempt` order. They are matched **by order, tails aligned**: `R[len(R)-1] ↔ S[len(S)-1]`, `R[len(R)-2] ↔ S[len(S)-2]`, …; whichever side is longer leaves its OLDEST elements unmatched. Time never reorders the match. Why order is exact: `awaitPhaseDecision` records exactly one note per honoured send-back, immediately after leaving the gate and before the redraft (`constructactivity.go` ~L1523–1530), and an exhausted send-back (~L1511–1521) records no note and opens no revision. Why tails: notes exist only since B1.1, so an older (backfilled) rejection has none. The matched note supplies the review revision's `note` (`Text`), `comments` and `commentCount`.
- **R5 provenance.** A revision's provenance is `projectstate.AttemptsWorstOrigin` over exactly its member attempts — `worstOrigin`'s contagion rule, unchanged: one synthesized sub-attempt makes the revision synthesized and leaves its neighbours alone.
- **R6 times and episode.** `startedAt` = earliest member `StartedAt`; `endedAt` = latest member `EndedAt`, nil while any member is pending. A dispatch revision's `episodeId` is the episode evidence of its LAST member that has one (the attempt that reached the gate, else the latest). Review revisions carry none.
- **Revision outcome.** Dispatch: any pending member → `running`; else by the last member: `passed`, `skipped`, otherwise `failed`. Review, by its gate attempt: pending → `awaitingHuman` when `liveGate == P.ID`, else `running`; `passed`; `rejected → sentBack`; `failed`; `skipped`.

**Task state table (first match wins; evaluated over the lifecycle DAG, memoized):**

| # | Condition | State |
|---|---|---|
| 1 | review task, its phase's gate, and `liveGate == task.Phase` | `awaitingHuman` |
| 2 | latest revision outcome is `running` | `running` |
| 3 | review task: latest revision `sentBack` and the reviewed task has a NEWER revision (`len(work) > n`) | `pending` — the redraft is under way |
| 4 | review task: latest revision `sentBack` | `sentBack` |
| 5 | dispatch task: the review task that `reviews` it has latest revision `sentBack` at `n` and this task has no revision past `n` | `sentBack` |
| 6 | latest revision `failed` | `failed` |
| 7 | latest revision `passed` or `skipped` | `passed` |
| 8 | dispatch task with no revisions whose reviewing gate is `passed` (App A: the gate IS the exit criterion; silence is not denial) | `passed` |
| 9 | no revisions and some `dependsOn` task is not `passed` | `locked` |
| 10 | otherwise | `pending` |

`locked` is rule 9, not rule 1, on purpose: evidence beats structure. A partial backfill (a passed Design Review and nothing recorded for Requirements) must not render a passed task as locked.

**Out of scope:** `requirements`, `architecture`, `projectDesign` lifecycles. They are not construction activities until stage 2; Task 8 refuses them before this code runs.

- [ ] **Step 1: Write the failing tests** — append to `server/internal/manager/construction/manager_test.go` (add `methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"` to its imports):

  ```go
  // ===========================================================================
  // QueryActivityView — revision derivation (spec 2026-09-20 §2). Pure functions.
  // ===========================================================================

  // avServiceLifecycle is Figure A-1 as the lifecycle data states it: SRS → SRS Review →
  // fork { Detailed Design → Design Review → Construction → Code Review → Integration }
  // ∥ { STP → STP Review } → join Testing. A literal, so these tests pin the DERIVATION
  // and not the data file.
  func avServiceLifecycle() methodassets.Lifecycle {
  	d := func(id, phase string, deps ...string) methodassets.LifecycleTask {
  		return methodassets.LifecycleTask{ID: id, Kind: methodassets.LifecycleTaskDispatch, Title: id, Phase: phase, DependsOn: deps}
  	}
  	r := func(id, phase, reviews string, deps ...string) methodassets.LifecycleTask {
  		return methodassets.LifecycleTask{ID: id, Kind: methodassets.LifecycleTaskReview, Title: id, Phase: phase, DependsOn: deps, Reviews: reviews}
  	}
  	return methodassets.Lifecycle{
  		Type: "service",
  		Phases: []methodassets.LifecyclePhase{
  			{ID: "requirements", Label: "Requirements", Weight: 15, Gate: "srsReview"},
  			{ID: "detailed_design", Label: "Detailed Design", Weight: 20, Gate: "designReview"},
  			{ID: "test_plan", Label: "Test Plan", Weight: 10, Gate: "stpReview"},
  			{ID: "construction", Label: "Construction", Weight: 40, Gate: "codeReview"},
  			{ID: "integration", Label: "Integration", Weight: 15, Gate: "testing"},
  		},
  		Tasks: []methodassets.LifecycleTask{
  			d("srs", "requirements"), r("srsReview", "requirements", "srs", "srs"),
  			d("detailedDesign", "detailed_design", "srsReview"), r("designReview", "detailed_design", "detailedDesign", "detailedDesign"),
  			d("stp", "test_plan", "srsReview"), r("stpReview", "test_plan", "stp", "stp"),
  			d("construction", "construction", "designReview"), r("codeReview", "construction", "construction", "construction"),
  			d("integration", "integration", "codeReview"), r("testing", "integration", "integration", "integration", "stpReview"),
  		},
  	}
  }

  func avAttempt(task projectstate.MethodTask, n int, outcome projectstate.TaskOutcome, origin projectstate.RecordOrigin) projectstate.TaskAttempt {
  	a := ledgerAttempt("C-X", task, n, outcome)
  	a.Provenance = projectstate.AttemptProvenance{Origin: origin, Basis: "test"}
  	return a
  }

  func avObserved(task projectstate.MethodTask, n int, outcome projectstate.TaskOutcome) projectstate.TaskAttempt {
  	return avAttempt(task, n, outcome, projectstate.OriginObserved)
  }

  func avSendBack(gate, text string, comments ...projectstate.NoteComment) projectstate.OperatorNote {
  	return projectstate.OperatorNote{NoteID: "n-" + text, Kind: projectstate.NoteSendBack, Gate: gate, Text: text, Comments: comments}
  }

  func avTask(t *testing.T, views []taskView, id string) taskView {
  	t.Helper()
  	for _, v := range views {
  		if v.ID == id {
  			return v
  		}
  	}
  	t.Fatalf("no task %q in %+v", id, views)
  	return taskView{}
  }

  func avOutcomes(v taskView) []string {
  	out := make([]string, 0, len(v.Revisions))
  	for _, r := range v.Revisions {
  		out = append(out, r.Outcome)
  	}
  	return out
  }

  var avSRSPassed = []projectstate.TaskAttempt{
  	avObserved(projectstate.TaskSRS, 1, projectstate.OutcomePassed),
  	avObserved(projectstate.TaskSRSReview, 1, projectstate.OutcomePassed),
  }

  func TestDeriveTaskViews_StatesAndRevisions(t *testing.T) {
  	passed, rejected, failed, pending := projectstate.OutcomePassed, projectstate.OutcomeRejected, projectstate.OutcomeFailed, projectstate.OutcomePending
  	with := func(extra ...projectstate.TaskAttempt) []projectstate.TaskAttempt {
  		return append(slices.Clone(avSRSPassed), extra...)
  	}
  	cases := []struct {
  		name     string
  		attempts []projectstate.TaskAttempt
  		notes    []projectstate.OperatorNote
  		liveGate string
  		want     map[string]string   // task id → state
  		revs     map[string][]string // task id → revision outcomes
  	}{
  		{
  			name: "nothing recorded: the root is pending, everything else locked",
  			want: map[string]string{"srs": taskPending, "srsReview": taskLocked, "detailedDesign": taskLocked, "testing": taskLocked},
  		},
  		{
  			name:     "the fork: a passed SRS Review unlocks BOTH branches; the join stays locked",
  			attempts: with(),
  			want:     map[string]string{"srsReview": taskPassed, "detailedDesign": taskPending, "stp": taskPending, "designReview": taskLocked, "testing": taskLocked},
  		},
  		{
  			name: "a send-back opens revision 2; the dispatch and its review share the numbers",
  			attempts: with(
  				avObserved(projectstate.TaskDetailedDesign, 1, passed), avObserved(projectstate.TaskDesignReview, 1, rejected),
  				avObserved(projectstate.TaskDetailedDesign, 2, passed), avObserved(projectstate.TaskDesignReview, 2, passed)),
  			notes: []projectstate.OperatorNote{avSendBack("detailed_design", "split the op")},
  			want:  map[string]string{"detailedDesign": taskPassed, "designReview": taskPassed, "construction": taskPending},
  			revs:  map[string][]string{"detailedDesign": {revPassed, revPassed}, "designReview": {revSentBack, revPassed}},
  		},
  		{
  			name: "failed and retried work before the gate is ONE revision",
  			attempts: with(
  				avObserved(projectstate.TaskDetailedDesign, 1, passed), avObserved(projectstate.TaskDesignReview, 1, passed),
  				avObserved(projectstate.TaskConstruction, 1, failed), avObserved(projectstate.TaskConstruction, 2, failed),
  				avObserved(projectstate.TaskConstruction, 3, passed), avObserved(projectstate.TaskCodeReview, 1, passed)),
  			want: map[string]string{"construction": taskPassed, "codeReview": taskPassed},
  			revs: map[string][]string{"construction": {revPassed}, "codeReview": {revPassed}},
  		},
  		{
  			name: "sent back and not yet redrafted: both tasks read sentBack",
  			attempts: with(avObserved(projectstate.TaskDetailedDesign, 1, passed), avObserved(projectstate.TaskDesignReview, 1, rejected)),
  			notes:    []projectstate.OperatorNote{avSendBack("detailed_design", "redo")},
  			want:     map[string]string{"detailedDesign": taskSentBack, "designReview": taskSentBack, "construction": taskLocked},
  		},
  		{
  			name: "the redraft is running: the dispatch runs, its review waits, the other branch is untouched",
  			attempts: with(
  				avObserved(projectstate.TaskDetailedDesign, 1, passed), avObserved(projectstate.TaskDesignReview, 1, rejected),
  				avObserved(projectstate.TaskDetailedDesign, 2, pending), avObserved(projectstate.TaskSTP, 1, pending)),
  			notes: []projectstate.OperatorNote{avSendBack("detailed_design", "redo")},
  			want:  map[string]string{"detailedDesign": taskRunning, "designReview": taskPending, "stp": taskRunning, "stpReview": taskLocked},
  			revs:  map[string][]string{"detailedDesign": {revPassed, revRunning}, "designReview": {revSentBack}},
  		},
  		{
  			name:     "a live gate: the review task awaits the human",
  			attempts: with(avObserved(projectstate.TaskDetailedDesign, 1, passed), avObserved(projectstate.TaskDesignReview, 1, pending)),
  			liveGate: "detailed_design",
  			want:     map[string]string{"detailedDesign": taskPassed, "designReview": taskAwaitingHuman},
  			revs:     map[string][]string{"designReview": {revAwaitingHuman}},
  		},
  		{
  			name:     "work that failed and was not retried",
  			attempts: with(avObserved(projectstate.TaskDetailedDesign, 1, failed)),
  			want:     map[string]string{"detailedDesign": taskFailed, "designReview": taskLocked},
  			revs:     map[string][]string{"detailedDesign": {revFailed}},
  		},
  		{
  			name:     "a partial backfill: a passed gate with no work attempt passes its work, and is not locked by silent ancestors",
  			attempts: []projectstate.TaskAttempt{avAttempt(projectstate.TaskDesignReview, 1, passed, projectstate.OriginBackfilled)},
  			want:     map[string]string{"srs": taskPending, "designReview": taskPassed, "detailedDesign": taskPassed, "construction": taskPending},
  		},
  	}
  	for _, c := range cases {
  		t.Run(c.name, func(t *testing.T) {
  			views := deriveTaskViews(avServiceLifecycle(), c.attempts, c.notes, c.liveGate)
  			if len(views) != 10 {
  				t.Fatalf("want one view per lifecycle task (10), got %d", len(views))
  			}
  			for id, want := range c.want {
  				if got := avTask(t, views, id).State; got != want {
  					t.Errorf("%s state = %s, want %s", id, got, want)
  				}
  			}
  			for id, want := range c.revs {
  				if got := avOutcomes(avTask(t, views, id)); !slices.Equal(got, want) {
  					t.Errorf("%s revisions = %v, want %v", id, got, want)
  				}
  			}
  		})
  	}
  }

  func TestDeriveTaskViews_RevisionMembersNoteAndProvenance(t *testing.T) {
  	passed, rejected, failed := projectstate.OutcomePassed, projectstate.OutcomeRejected, projectstate.OutcomeFailed
  	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
  	at := func(a projectstate.TaskAttempt, startMin, endMin int, episodeID string) projectstate.TaskAttempt {
  		s, e := t0.Add(time.Duration(startMin)*time.Minute), t0.Add(time.Duration(endMin)*time.Minute)
  		a.StartedAt, a.EndedAt = &s, &e
  		if episodeID != "" {
  			a.Evidence = projectstate.EvidenceRef{Kind: projectstate.EvidenceEpisode, Ref: episodeID}
  		}
  		return a
  	}
  	attempts := append(slices.Clone(avSRSPassed),
  		at(avObserved(projectstate.TaskConstruction, 1, failed), 0, 10, "ep-c1"),
  		at(avAttempt(projectstate.TaskConstruction, 2, passed, projectstate.OriginSynthesized), 11, 20, "ep-c2"),
  		at(avObserved(projectstate.TaskTestClient, 1, passed), 12, 18, ""),
  		at(avObserved(projectstate.TaskCodeReview, 1, rejected), 21, 30, ""),
  		at(avObserved(projectstate.TaskConstruction, 3, passed), 31, 40, "ep-c3"),
  		at(avObserved(projectstate.TaskCodeReview, 2, passed), 41, 45, ""),
  	)
  	older := avSendBack("construction", "predates the gate attempts — must stay unmatched")
  	matched := avSendBack("construction", "handle the nil map",
  		projectstate.NoteComment{JSONPath: "$.ops[0]", Text: "nil map"}, projectstate.NoteComment{JSONPath: "$.ops[1]", Text: "no test"})
  	elsewhere := avSendBack("detailed_design", "another gate's note")
  	views := deriveTaskViews(avServiceLifecycle(), attempts, []projectstate.OperatorNote{older, elsewhere, matched}, "")

  	work := avTask(t, views, "construction").Revisions
  	if len(work) != 2 {
  		t.Fatalf("construction revisions = %d, want 2", len(work))
  	}
  	if got, want := work[0].AttemptIDs, []string{"C-X:construction:1", "C-X:construction:2", "C-X:testClient:1"}; !slices.Equal(got, want) {
  		t.Errorf("revision 1 members = %v, want %v (the failed retry and the tandem test client are sub-attempts)", got, want)
  	}
  	if work[0].EpisodeID != "ep-c2" || work[1].EpisodeID != "ep-c3" {
  		t.Errorf("episodes = %q, %q; want the attempt that reached the gate: ep-c2, ep-c3", work[0].EpisodeID, work[1].EpisodeID)
  	}
  	if work[0].Provenance != projectstate.OriginSynthesized || work[1].Provenance != projectstate.OriginObserved {
  		t.Errorf("provenance = %q, %q; want synthesized (contagion from construction#2) then observed", work[0].Provenance, work[1].Provenance)
  	}
  	if work[0].StartedAt == nil || !work[0].StartedAt.Equal(t0) || work[0].EndedAt == nil || !work[0].EndedAt.Equal(t0.Add(20*time.Minute)) {
  		t.Errorf("revision 1 spans %v–%v, want %v–%v", work[0].StartedAt, work[0].EndedAt, t0, t0.Add(20*time.Minute))
  	}

  	gate := avTask(t, views, "codeReview").Revisions
  	if len(gate) != 2 || gate[0].N != 1 || gate[1].N != 2 {
  		t.Fatalf("codeReview revisions = %+v, want n=1,2", gate)
  	}
  	if gate[0].Outcome != revSentBack || gate[0].Note != "handle the nil map" || len(gate[0].Comments) != 2 {
  		t.Errorf("revision 1 = %+v, want sentBack carrying the LAST construction send-back note and its 2 comments", gate[0])
  	}
  	if gate[1].Note != "" || gate[0].EpisodeID != "" {
  		t.Errorf("a passed review carries no note and a review revision no episode: %+v", gate)
  	}
  }

  func TestNormalizeAttempts_ReconstructsALiveRun(t *testing.T) {
  	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
  	done := t0.Add(50 * time.Minute)
  	row := projectstate.ActivityConstructionStatus{
  		ActivityID:   "C-X",
  		CurrentPhase: projectstate.MethodPhaseConstruction,
  		Attempts:     []projectstate.TaskAttempt{avAttempt(projectstate.TaskSRS, 1, projectstate.OutcomePassed, projectstate.OriginBackfilled)},
  		Phases: []projectstate.PhaseCompletion{
  			{Phase: projectstate.MethodPhaseDetailedDesign, Completed: true, CompletedAt: &done},
  			{Phase: projectstate.MethodPhaseConstruction},
  		},
  		OperatorNotes: []projectstate.OperatorNote{{NoteID: "n1", Kind: projectstate.NoteSendBack, Gate: "detailed_design", Text: "redo", RecordedAt: t0.Add(20 * time.Minute)}},
  	}
  	episodes := []episode.EpisodeRecord{
  		{EpisodeID: "ep-srs", TargetRef: "C-X:srs:1", StartedAt: t0, EndedAt: t0.Add(5 * time.Minute)},
  		{EpisodeID: "ep-dd1", TargetRef: "C-X:detailedDesign:1", StartedAt: t0.Add(6 * time.Minute), EndedAt: t0.Add(15 * time.Minute)},
  		{EpisodeID: "ep-dd2", TargetRef: "C-X:detailedDesign:2", StartedAt: t0.Add(21 * time.Minute), EndedAt: t0.Add(40 * time.Minute)},
  		{EpisodeID: "ep-gap", TargetRef: "C-X:construction:1", StartedAt: t0.Add(51 * time.Minute), EndedAt: t0.Add(52 * time.Minute), Outcome: episode.EpisodeGap},
  		{EpisodeID: "ep-legacy", TargetRef: "C-X"},            // pre-attempt-key record: not an attempt
  		{EpisodeID: "ep-other", TargetRef: "C-XY:srs:1"},      // another activity sharing the prefix
  	}
  	live := &ConstructionSessionView{Stage: StagePipelineRunning}
  	got := normalizeAttempts("C-X", row, episodes, live)

  	type key struct {
  		id      string
  		outcome projectstate.TaskOutcome
  		origin  projectstate.RecordOrigin
  	}
  	var have []key
  	for _, a := range got {
  		have = append(have, key{a.AttemptID, a.Outcome, a.Provenance.Origin})
  	}
  	bf := projectstate.OriginBackfilled
  	want := []key{
  		{"C-X:srs:1", projectstate.OutcomePassed, bf},            // N1: the ledger row, verbatim
  		{"C-X:detailedDesign:1", projectstate.OutcomePassed, bf}, // N2
  		{"C-X:detailedDesign:2", projectstate.OutcomePassed, bf}, // N2
  		{"C-X:construction:1", projectstate.OutcomeFailed, bf},   // N2: a gap is a failed attempt
  		{"C-X:construction:2", projectstate.OutcomePending, bf},  // N3: the dispatch running now
  		{"C-X:designReview:1", projectstate.OutcomeRejected, bf}, // N4: the send-back note
  		{"C-X:designReview:2", projectstate.OutcomePassed, bf},   // N4: the stored completion
  	}
  	if !slices.Equal(have, want) {
  		t.Fatalf("normalized attempts:\n got %v\nwant %v", have, want)
  	}
  	if got[0].Evidence.Ref != "ep-srs" {
  		t.Errorf("N1: the ledger attempt must gain its episode as evidence, got %+v", got[0].Evidence)
  	}
  	for _, a := range got[1:] {
  		if a.Provenance.Basis == "" {
  			t.Errorf("%s: a reconstructed attempt must name its basis", a.AttemptID)
  		}
  	}
  }

  func TestNormalizeAttempts_ALiveGateIsAPendingGateAttempt(t *testing.T) {
  	since := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
  	row := projectstate.ActivityConstructionStatus{ActivityID: "C-X", Phases: []projectstate.PhaseCompletion{{Phase: projectstate.MethodPhaseDetailedDesign}}}
  	live := &ConstructionSessionView{Stage: StageAwaitingApproval, AwaitingGate: ptrTo("detailed_design"), AwaitingSince: &since}
  	got := normalizeAttempts("C-X", row, nil, live)
  	if len(got) != 1 || got[0].AttemptID != "C-X:designReview:1" || got[0].Outcome != projectstate.OutcomePending || got[0].StartedAt == nil || !got[0].StartedAt.Equal(since) {
  		t.Fatalf("want one pending designReview#1 since %v, got %+v", since, got)
  	}
  }
  ```

- [ ] **Step 2: Run; confirm the build fails on the missing symbols.**
  ```bash
  cd server && GOWORK=off go test ./internal/manager/construction/ -run 'TestDeriveTaskViews|TestNormalizeAttempts' -count=1
  ```
  Expected: `undefined: deriveTaskViews`, `undefined: normalizeAttempts`, `undefined: taskView`, `undefined: taskPending`, `undefined: revPassed` … `FAIL … [build failed]`. If it reports `undefined: methodassets.Lifecycle` instead, Task 3's pin bump has not landed — stop.

- [ ] **Step 3: Implement** — append to `server/internal/manager/construction/constructionmanager.go`. Add to its import block whichever of `cmp`, `slices`, `strconv`, `strings`, `time` it lacks, plus `methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"`.

  ```go
  // ---------------------------------------------------------------------------
  // QueryActivityView — revision derivation (spec 2026-09-20 §2). PURE: no I/O, no
  // clock. normalizeAttempts turns today's scattered evidence into one attempt list;
  // deriveTaskViews cuts that list into revisions and decides every task's state.
  //
  // WHY NORMALIZE. The running construct workflow does not write the attempt ledger: it
  // counts attempts in memory to mint the AttemptID it stamps on the episode's TargetRef,
  // and a gate attempt is written by nothing. Only cmd/backfill-attempts appends to
  // ActivityConstructionStatus.Attempts. A live run's history is its episodes, its
  // send-back OperatorNotes, its stored phase completions and its live session. Stage 3
  // of the unified-activity spec persists revisions; this block is deleted then.
  // ---------------------------------------------------------------------------

  // Revision outcomes and task states: the wire values of the ActivityView contract.
  const (
  	revRunning       = "running"
  	revAwaitingHuman = "awaitingHuman"
  	revPassed        = "passed"
  	revSentBack      = "sentBack"
  	revFailed        = "failed"
  	revSkipped       = "skipped"

  	taskPending       = "pending"
  	taskLocked        = "locked"
  	taskRunning       = "running"
  	taskAwaitingHuman = "awaitingHuman"
  	taskPassed        = "passed"
  	taskSentBack      = "sentBack"
  	taskFailed        = "failed"


  	normalizeGenerator = "constructionManager.normalizeAttempts"
  )

  // taskRevision is revision n of one lifecycle task: the n-th work that reached the gate
  // (with every failed or retried attempt before it) for a dispatch task, the n-th gate
  // attempt for a review task. The two share n.
  type taskRevision struct {
  	N          int
  	Outcome    string
  	StartedAt  *time.Time
  	EndedAt    *time.Time
  	AttemptIDs []string
  	EpisodeID  string
  	Note       string
  	Comments   []projectstate.NoteComment
  	Provenance projectstate.RecordOrigin
  }

  // taskView is one lifecycle task's derived state and history.
  type taskView struct {
  	ID        string
  	State     string
  	Revisions []taskRevision
  }

  // canonicalMethodPhases is the order reconstructed gate attempts are emitted in.
  var canonicalMethodPhases = []projectstate.ActivityMethodPhase{
  	projectstate.MethodPhaseRequirements, projectstate.MethodPhaseDetailedDesign, projectstate.MethodPhaseTestPlan,
  	projectstate.MethodPhaseConstruction, projectstate.MethodPhaseIntegration,
  }

  // normalizeAttempts builds the one attempt list the derivation reads (rules N1–N4): the
  // ledger verbatim, then a work attempt per episode the ledger does not hold, then the
  // dispatch running now, then the gate attempts the send-back notes, the stored phase
  // completions and the live gate imply. Everything it adds is stamped backfilled —
  // "reconstructed from real evidence recorded elsewhere" — with the evidence as its basis.
  func normalizeAttempts(activityID string, row projectstate.ActivityConstructionStatus, episodes []episode.EpisodeRecord, live *ConstructionSessionView) []projectstate.TaskAttempt {
  	out := slices.Clone(row.Attempts)
  	index := make(map[string]int, len(out))
  	for i, a := range out {
  		index[a.AttemptID] = i
  	}
  	for _, ep := range episodes {
  		task, n, ok := parseAttemptRef(activityID, ep.TargetRef)
  		if !ok {
  			continue
  		}
  		evidence := projectstate.EvidenceRef{Kind: projectstate.EvidenceEpisode, Ref: ep.EpisodeID}
  		if i, held := index[ep.TargetRef]; held {
  			if out[i].Evidence.Kind == projectstate.EvidenceNone {
  				out[i].Evidence = evidence // N1
  			}
  			continue
  		}
  		started, ended := ep.StartedAt, ep.EndedAt
  		index[ep.TargetRef] = len(out)
  		out = append(out, projectstate.TaskAttempt{ // N2
  			AttemptID: ep.TargetRef, Task: task, Phase: projectstate.PhaseForTask(task), Attempt: n,
  			Actor: projectstate.ActorAgent, StartedAt: &started, EndedAt: &ended,
  			Outcome: taskOutcomeOfEpisode(ep.Outcome), Evidence: evidence,
  			Provenance: reconstructed("episodes[" + ep.EpisodeID + "]"),
  		})
  	}
  	out = appendRunningAttempt(out, activityID, row, live)
  	return appendGateAttempts(out, activityID, row, live)
  }

  // parseAttemptRef reads "<activityId>:<task>:<n>" (projectstate.AttemptID). A legacy
  // TargetRef (the bare activity id) and another activity's ref are not attempts of this one.
  func parseAttemptRef(activityID, ref string) (projectstate.MethodTask, int, bool) {
  	rest, ok := strings.CutPrefix(ref, activityID+":")
  	if !ok {
  		return "", 0, false
  	}
  	name, num, ok := strings.Cut(rest, ":")
  	if !ok {
  		return "", 0, false
  	}
  	n, err := strconv.Atoi(num)
  	task := projectstate.MethodTask(name)
  	if err != nil || n < 1 || projectstate.PhaseForTask(task) == "" {
  		return "", 0, false
  	}
  	return task, n, true
  }

  // taskOutcomeOfEpisode: an episode that did not succeed — failed, cancelled, or a gap
  // with no summary at all — is a failed attempt; the dispatch burned and produced nothing.
  func taskOutcomeOfEpisode(o episode.EpisodeOutcome) projectstate.TaskOutcome {
  	switch o {
  	case episode.EpisodeSucceeded:
  		return projectstate.OutcomePassed
  	case episode.EpisodeFailed, episode.EpisodeCancelled, episode.EpisodeGap:
  		return projectstate.OutcomeFailed
  	}
  	return projectstate.OutcomeFailed
  }

  func reconstructed(basis string) projectstate.AttemptProvenance {
  	return projectstate.AttemptProvenance{Origin: projectstate.OriginBackfilled, Generator: normalizeGenerator, Basis: basis}
  }

  func highestAttempt(attempts []projectstate.TaskAttempt, task projectstate.MethodTask) int {
  	n := 0
  	for _, a := range attempts {
  		if a.Task == task && a.Attempt > n {
  			n = a.Attempt
  		}
  	}
  	return n
  }

  // appendRunningAttempt is N3: the dispatch a live session is running now, which has no
  // episode until it ends.
  func appendRunningAttempt(out []projectstate.TaskAttempt, activityID string, row projectstate.ActivityConstructionStatus, live *ConstructionSessionView) []projectstate.TaskAttempt {
  	if live == nil || (live.Stage != StageDispatching && live.Stage != StagePipelineRunning) {
  		return out
  	}
  	task := projectstate.AgentTaskFor(row.CurrentPhase)
  	if task == "" {
  		return out
  	}
  	for _, a := range out {
  		if a.Task == task && a.Outcome == projectstate.OutcomePending {
  			return out
  		}
  	}
  	n := highestAttempt(out, task) + 1
  	return append(out, projectstate.TaskAttempt{
  		AttemptID: projectstate.AttemptID(activityID, task, n), Task: task, Phase: row.CurrentPhase, Attempt: n,
  		Actor: projectstate.ActorAgent, Provenance: reconstructed("session.stage"),
  	})
  }

  // liveApprovalGate is the lifecycle phase a live session awaits approval at, if any. The
  // merge hold and an escalation are not phase gates and never match a phase id.
  func liveApprovalGate(live *ConstructionSessionView) (string, *time.Time) {
  	if live == nil || live.Stage != StageAwaitingApproval || live.AwaitingGate == nil {
  		return "", nil
  	}
  	return *live.AwaitingGate, live.AwaitingSince
  }

  // appendGateAttempts is N4, in canonical phase order.
  func appendGateAttempts(out []projectstate.TaskAttempt, activityID string, row projectstate.ActivityConstructionStatus, live *ConstructionSessionView) []projectstate.TaskAttempt {
  	liveGate, liveSince := liveApprovalGate(live)
  	stored := make(map[projectstate.ActivityMethodPhase]projectstate.PhaseCompletion, len(row.Phases))
  	for _, pc := range row.Phases {
  		stored[pc.Phase] = pc
  	}
  	for _, p := range canonicalMethodPhases {
  		gate := projectstate.GateTaskFor(p)
  		n, passed := highestAttempt(out, gate), false
  		for _, a := range out {
  			passed = passed || (a.Task == gate && a.Outcome == projectstate.OutcomePassed)
  		}
  		add := func(outcome projectstate.TaskOutcome, started, ended *time.Time, basis string) {
  			n++
  			out = append(out, projectstate.TaskAttempt{
  				AttemptID: projectstate.AttemptID(activityID, gate, n), Task: gate, Phase: p, Attempt: n,
  				StartedAt: started, EndedAt: ended, Outcome: outcome, Provenance: reconstructed(basis),
  			})
  		}
  		for _, note := range row.OperatorNotes {
  			if note.Kind == projectstate.NoteSendBack && note.Gate == string(p) {
  				at := note.RecordedAt
  				add(projectstate.OutcomeRejected, nil, &at, "operatorNotes["+note.NoteID+"]")
  			}
  		}
  		switch {
  		case stored[p].Completed && !passed:
  			add(projectstate.OutcomePassed, nil, stored[p].CompletedAt, "phases["+string(p)+"].completed")
  		case liveGate == string(p):
  			add(projectstate.OutcomePending, liveSince, nil, "session.awaitingGate")
  		}
  	}
  	return out
  }

  // phaseSegment is one revision's work: the phase's work-task attempts up to the one
  // that reached the gate, plus any conditional-task attempts (someConstruction,
  // testClient) made alongside them.
  type phaseSegment struct {
  	main, extra []projectstate.TaskAttempt
  }

  func (s phaseSegment) members() []projectstate.TaskAttempt {
  	return append(slices.Clone(s.main), s.extra...)
  }

  // deriveTaskViews returns one view per lifecycle task, in lifecycle order (rules R1–R6
  // and the task state table; both are spelled out in the stage-0 plan, Task 7).
  func deriveTaskViews(lc methodassets.Lifecycle, attempts []projectstate.TaskAttempt, notes []projectstate.OperatorNote, liveGate string) []taskView {
  	revs := make(map[string][]taskRevision, len(lc.Tasks))
  	gates := make(map[string]bool, len(lc.Phases))
  	for _, ph := range lc.Phases {
  		work := phaseWorkTask(lc, ph.ID)
  		workRevs, gateRevs := phaseRevisions(ph, work, attempts, notes, liveGate)
  		if work != "" {
  			revs[work] = workRevs
  		}
  		revs[ph.Gate], gates[ph.Gate] = gateRevs, true
  	}
  	byID := make(map[string]methodassets.LifecycleTask, len(lc.Tasks))
  	reviewerOf := make(map[string]string, len(lc.Tasks))
  	for _, t := range lc.Tasks {
  		byID[t.ID] = t
  		if t.Kind == methodassets.LifecycleTaskReview && t.Reviews != "" {
  			reviewerOf[t.Reviews] = t.ID
  		}
  	}
  	states := make(map[string]string, len(lc.Tasks))
  	var stateOf func(id string) string
  	stateOf = func(id string) string {
  		if s, ok := states[id]; ok {
  			return s
  		}
  		states[id] = taskLocked // a cycle reads locked; a validated lifecycle has none
  		t := byID[id]
  		s := evidenceState(t, gates[id], revs, reviewerOf, liveGate)
  		if s == "" {
  			s = taskPending
  			for _, dep := range t.DependsOn {
  				if stateOf(dep) != taskPassed {
  					s = taskLocked // rule 9
  					break
  				}
  			}
  		}
  		states[id] = s
  		return s
  	}
  	out := make([]taskView, 0, len(lc.Tasks))
  	for _, t := range lc.Tasks {
  		out = append(out, taskView{ID: t.ID, State: stateOf(t.ID), Revisions: revs[t.ID]})
  	}
  	return out
  }

  // phaseWorkTask is the phase's dispatch task ("" for a phase with none, e.g. the
  // projectDesign activity's review-only phase).
  func phaseWorkTask(lc methodassets.Lifecycle, phaseID string) string {
  	for _, t := range lc.Tasks {
  		if t.Phase == phaseID && t.Kind == methodassets.LifecycleTaskDispatch {
  			return t.ID
  		}
  	}
  	return ""
  }

  // phaseRevisions cuts one lifecycle phase's attempts into the work task's revisions and
  // the gate task's revisions (R1–R4).
  func phaseRevisions(ph methodassets.LifecyclePhase, work string, attempts []projectstate.TaskAttempt, notes []projectstate.OperatorNote, liveGate string) (workRevs, gateRevs []taskRevision) {
  	var main, extra, gate []projectstate.TaskAttempt
  	for _, a := range attempts {
  		switch {
  		case string(a.Phase) != ph.ID:
  		case string(a.Task) == ph.Gate:
  			gate = append(gate, a)
  		case string(a.Task) == work:
  			main = append(main, a)
  		default:
  			extra = append(extra, a) // R2: a conditional task is a sub-attempt of the work task
  		}
  	}
  	byAttempt := func(x, y projectstate.TaskAttempt) int { return cmp.Compare(x.Attempt, y.Attempt) }
  	slices.SortStableFunc(main, byAttempt)
  	slices.SortStableFunc(gate, byAttempt)
  	for i, seg := range foldConditional(cutSegments(main), extra) {
  		workRevs = append(workRevs, dispatchRevision(i+1, seg))
  	}
  	var sendBacks []projectstate.OperatorNote
  	for _, n := range notes {
  		if n.Kind == projectstate.NoteSendBack && n.Gate == ph.ID {
  			sendBacks = append(sendBacks, n)
  		}
  	}
  	rejected := 0
  	for _, g := range gate {
  		if g.Outcome == projectstate.OutcomeRejected {
  			rejected++
  		}
  	}
  	// R4: the j-th rejection takes note j + (len(sendBacks) - rejected) — tails aligned.
  	next := len(sendBacks) - rejected
  	for i, g := range gate {
  		rev := reviewRevision(i+1, g, liveGate == ph.ID)
  		if g.Outcome == projectstate.OutcomeRejected {
  			if next >= 0 && next < len(sendBacks) {
  				rev.Note, rev.Comments = sendBacks[next].Text, sendBacks[next].Comments
  			}
  			next++
  		}
  		gateRevs = append(gateRevs, rev)
  	}
  	return workRevs, gateRevs
  }

  // cutSegments is R1: a segment closes at the attempt that reached the gate.
  func cutSegments(main []projectstate.TaskAttempt) []phaseSegment {
  	var out []phaseSegment
  	var cur []projectstate.TaskAttempt
  	for _, a := range main {
  		cur = append(cur, a)
  		if a.Outcome == projectstate.OutcomePassed || a.Outcome == projectstate.OutcomeSkipped {
  			out, cur = append(out, phaseSegment{main: cur}), nil
  		}
  	}
  	if len(cur) > 0 {
  		out = append(out, phaseSegment{main: cur})
  	}
  	return out
  }

  // foldConditional is R2: each conditional-task attempt joins the first revision whose
  // reaching attempt ended at or after it started, else the last revision.
  func foldConditional(segs []phaseSegment, extra []projectstate.TaskAttempt) []phaseSegment {
  	for _, x := range extra {
  		if len(segs) == 0 {
  			segs = append(segs, phaseSegment{})
  		}
  		at := len(segs) - 1
  		for i, s := range segs {
  			if len(s.main) == 0 {
  				continue
  			}
  			reached := s.main[len(s.main)-1]
  			if x.StartedAt != nil && reached.EndedAt != nil && !x.StartedAt.After(*reached.EndedAt) {
  				at = i
  				break
  			}
  		}
  		segs[at].extra = append(segs[at].extra, x)
  	}
  	return segs
  }

  func dispatchRevision(n int, seg phaseSegment) taskRevision {
  	members := seg.members()
  	rev := taskRevision{N: n, Outcome: revFailed, Provenance: projectstate.AttemptsWorstOrigin(members)} // R5
  	decisive := members[len(members)-1]
  	if len(seg.main) > 0 {
  		decisive = seg.main[len(seg.main)-1]
  	}
  	switch decisive.Outcome {
  	case projectstate.OutcomePassed:
  		rev.Outcome = revPassed
  	case projectstate.OutcomeSkipped:
  		rev.Outcome = revSkipped
  	case projectstate.OutcomePending, projectstate.OutcomeRejected, projectstate.OutcomeFailed:
  		// pending is decided below over every member; a work attempt is never "rejected".
  	}
  	for _, a := range members {
  		rev.AttemptIDs = append(rev.AttemptIDs, a.AttemptID)
  		if a.Outcome == projectstate.OutcomePending {
  			rev.Outcome = revRunning
  		}
  	}
  	for _, a := range slices.Backward(members) { // R6: main members first, so search them last-to-first
  		if a.Evidence.Kind == projectstate.EvidenceEpisode && a.Task == decisive.Task {
  			rev.EpisodeID = a.Evidence.Ref
  			break
  		}
  	}
  	rev.StartedAt, rev.EndedAt = attemptSpan(members)
  	return rev
  }

  func reviewRevision(n int, g projectstate.TaskAttempt, live bool) taskRevision {
  	rev := taskRevision{N: n, AttemptIDs: []string{g.AttemptID}, Provenance: projectstate.AttemptsWorstOrigin([]projectstate.TaskAttempt{g})}
  	switch g.Outcome {
  	case projectstate.OutcomePending:
  		rev.Outcome = revRunning
  		if live {
  			rev.Outcome = revAwaitingHuman
  		}
  	case projectstate.OutcomePassed:
  		rev.Outcome = revPassed
  	case projectstate.OutcomeRejected:
  		rev.Outcome = revSentBack
  	case projectstate.OutcomeFailed:
  		rev.Outcome = revFailed
  	case projectstate.OutcomeSkipped:
  		rev.Outcome = revSkipped
  	}
  	rev.StartedAt, rev.EndedAt = attemptSpan([]projectstate.TaskAttempt{g})
  	return rev
  }

  // attemptSpan is R6's times: the earliest start, and the latest end once nothing is pending.
  func attemptSpan(members []projectstate.TaskAttempt) (started, ended *time.Time) {
  	open := false
  	for _, a := range members {
  		if a.StartedAt != nil && (started == nil || a.StartedAt.Before(*started)) {
  			started = a.StartedAt
  		}
  		if a.EndedAt != nil && (ended == nil || a.EndedAt.After(*ended)) {
  			ended = a.EndedAt
  		}
  		open = open || a.Outcome == projectstate.OutcomePending
  	}
  	if open {
  		ended = nil
  	}
  	return started, ended
  }

  // evidenceState applies state rules 1–8; "" means the task has no evidence and its
  // dependsOn decide between locked and pending.
  func evidenceState(t methodassets.LifecycleTask, isGate bool, revs map[string][]taskRevision, reviewerOf map[string]string, liveGate string) string {
  	if t.Kind == methodassets.LifecycleTaskReview {
  		if isGate && liveGate != "" && liveGate == t.Phase {
  			return taskAwaitingHuman // 1
  		}
  		return reviewEvidenceState(revs[t.ID], len(revs[t.Reviews]))
  	}
  	return dispatchEvidenceState(revs[t.ID], revs[reviewerOf[t.ID]])
  }

  func reviewEvidenceState(mine []taskRevision, workRevisions int) string {
  	if len(mine) == 0 {
  		return ""
  	}
  	switch last := mine[len(mine)-1]; last.Outcome {
  	case revRunning, revAwaitingHuman:
  		return taskRunning // 2
  	case revSentBack:
  		if workRevisions > last.N {
  			return taskPending // 3: the redraft is under way
  		}
  		return taskSentBack // 4
  	case revFailed:
  		return taskFailed // 6
  	}
  	return taskPassed // 7
  }

  func dispatchEvidenceState(mine, reviewer []taskRevision) string {
  	var verdict *taskRevision
  	if len(reviewer) > 0 {
  		verdict = &reviewer[len(reviewer)-1]
  	}
  	if len(mine) == 0 {
  		if verdict != nil && verdict.Outcome == revPassed {
  			return taskPassed // 8: the gate is the exit criterion; silence is not denial
  		}
  		return ""
  	}
  	last := mine[len(mine)-1]
  	switch {
  	case last.Outcome == revRunning:
  		return taskRunning // 2
  	case verdict != nil && verdict.Outcome == revSentBack && last.N <= verdict.N:
  		return taskSentBack // 5
  	case last.Outcome == revFailed:
  		return taskFailed // 6
  	}
  	return taskPassed // 7
  }
  ```
  Note on `dispatchRevision`'s episode search: it matches `a.Task == decisive.Task`, so a conditional task's episode never displaces the work task's — the revision shows the episode of the attempt that reached the gate.

- [ ] **Step 4: Run the new tests, then the package.**
  ```bash
  cd server && GOWORK=off go test ./internal/manager/construction/ -run 'TestDeriveTaskViews|TestNormalizeAttempts' -count=1 -v
  GOWORK=off go test ./internal/manager/construction/ -count=1
  ```
  Expected: every subtest `PASS`; package `ok`. `unused` will flag nothing: every helper is reached from the two tested entry points (the façade in Task 8 is their production caller — if `make lint` reports `normalizeAttempts`/`deriveTaskViews` unused because lint runs with tests excluded, land Tasks 7 and 8 as ONE commit rather than adding a `//nolint`).
  - [ ] **Verify first:** `make lint` on this task alone. `.golangci.yml` does not set `run.tests: false`, so test callers should count — confirm before deciding between one commit and two.

- [ ] **Step 5: Gates.**
  ```bash
  cd server && GOWORK=off go test ./internal/ -run 'TestMethodLayering|TestFileLayout|TestGeneratedOnlyPublic|TestNoBannedPhaseIdentifier' -count=1
  GOWORK=off make test-short && make lint && make fix-check
  ```
  `TestNoBannedPhaseIdentifier` (`arch_bannedphase_test.go`) bans a TOP-LEVEL type/const/var named exactly `phase` or `Phase` in `manager/construction`; locals, parameters and compound names are out of its grain. The code above declares `phaseSegment`, `phaseRevisions`, `phaseWorkTask` (compound) and no bare one.

- [ ] **Step 6: Commit.**
  ```bash
  git add server/internal/manager/construction/constructionmanager.go server/internal/manager/construction/manager_test.go
  git commit -m "$(cat <<'EOF'
  feat(construction): derive task revisions and states from today's records

  Two pure functions behind QueryActivityView. normalizeAttempts rebuilds one
  attempt list from the ledger, the episode ledger (TargetRef = AttemptID), the
  send-back notes, the stored phase completions and the live session, because
  the running workflow never writes the attempt ledger. deriveTaskViews cuts it
  into revisions (spec 2026-09-20 section 2) and decides each task's state.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  EOF
  )"
  ```

---

### Task 8: Amend the `constructionManager` contract with `QueryActivityView`, regenerate, and write the façade (Part C)

**Depends on:** Task 7 (the two pure functions) and Task 3 (`methodassets.LifecycleFor`).

**Files:**
- Modify: `.aiarch/state/project.json` — `.serviceContracts.constructionManager.interface.operations` (append a 13th element after `GetEpisodeTimeline`) and `.serviceContracts.constructionManager["$defs"]` (nine new defs). Hand-edit, self-amendment procedure. No other slot changes; slots 9/10 are untouched, so `derived-plan-check` is not in play.
- Regenerate (never hand-edit): `server/internal/manager/construction/contract.gen.go`, `server/internal/manager/construction/fake/fake.gen.go`, `server/api/openapi.yaml`, `server/internal/client/web/**`, `server/internal/client/mcp/**`, `systemtests/internal/sdk/**`, `webApp/src/contracts/schema.ts`, `webApp/src/api/ops.gen.ts`.
- Modify: `server/internal/manager/construction/constructionmanager.go` — the façade method directly after `GetSessionState` (~L756), its helpers in the Task-7 section at end of file.
- Modify: `server/internal/manager/construction/manager_test.go` — façade tests appended to the Task-7 section.
- Modify: `server/internal/engine/designhealth/engine_test.go:59` — comment only (F5).

**How an op runs (verified, mirrors the two existing read shapes):** `GetSessionState` is a Temporal **Query** against the per-activity child (`m.client.QueryWorkflow(ctx, constructActivityWorkflowID(p, a), "", querySessionState)`), with Temporal's not-found mapped to `newError(fwm.NotFound, "no construction session for activity …: the pump has not dispatched it")`. `ListEpisodesForActivity` is a **plain method** over a ResourceAccess (`m.episodes.ListEpisodes`), errors through `mapRAError`. `QueryActivityView` is a plain method that composes both: `m.projectState.ReadProject` (precedent: `refuseWhilePaused` ~L277) + `m.episodes.ListEpisodes` + — only while the activity is Running — the session Query through the existing `m.activitySession` helper (~L882). It starts no workflow, registers no activity and sends no signal, so: no entry in `registered_names_test.go`'s golden (F7), no `activityOptions()` row, no drain.

**Error mapping:**

| Condition | Error |
|---|---|
| `projectID == ""` / `activityID == ""` | `ContractMisuse` `"empty projectId"` / `"empty activityId"` — also what satisfies `TestManagerRequiredStringsAreInspected` (`paramguard_arch_test.go`): both params are non-pointer `$ref`s to string defs, so the body must BRANCH on each |
| `ReadProject` / `ListEpisodes` fault | `mapRAError(err, "<ra>.<op>")` (RA `NotFound` → `NotFound`, else `Infrastructure`) |
| id not in the committed activity list — which today includes `requirements`, `architecture`, `projectDesign` (they become activities in stage 2) | `newError(fwm.NotFound, …)` — the same idiom and Kind `GetSessionState` uses for an activity it has no session for |
| the row cannot be classified (`ResolveConstructionRow` → `classified == false`) | `FailedPrecondition`, naming the plan repair (workerClass/coding) |
| classified, but method-assets has no lifecycle for the type key | `Infrastructure` — a platform-release defect, never the caller's; pinned impossible by partAB's validator |
| session Query fault other than not-found, while Running | whatever `GetSessionState` returns (`mapQueryError`) |

**Interfaces:**
- Consumes: `normalizeAttempts`, `deriveTaskViews`, `liveApprovalGate`, `taskView`, `taskRevision` (Task 7); `methodassets.LifecycleFor(typeKey string) (Lifecycle, bool)`; `projectstate.ResolveConstructionRow(r, meta) (typ, variant, resolved, classified)`, `projectstate.EffectiveConstructionPhase(r, meta)`, `ActivityType.String()`, `TestingVariant.String()`; `committedPlanInputs(proj)` (~L1473); `m.activitySession`; `isNotFound`-style check via `errors.As(err, *fwm.Error)`.
- Produces: **the production lifecycle-key rule, defined ONCE for the app** — `func lifecycleTypeKey(t projectstate.ActivityType, v projectstate.TestingVariant) string` in `server/internal/manager/construction/constructionmanager.go` (unexported; partAB decision D2: `t.String()` for a non-testing type, `"testing:" + v.String()` for testing, so the QA variant is `testing:qaProcess`). partAB Task 3 keeps a test-only copy in `projectstate`'s `access_test.go` for its parity test and explicitly does not produce a production one. It lives in the Manager because that is its only production consumer in stage 0, `TestMethodLayering` lets a Manager import method-assets (see Task 7), and exporting it from `projectstate` would cost an `encapsulationAllowlistData` entry for a rule stage 2 relocates anyway (when the estimation engine's plan derivation becomes the second consumer, move it beside `ProfileFor` and delete both copies);
- Produces: `ConstructionManager.QueryActivityView(rc fwm.Context, projectID ProjectID, activityID ActivityID) (ActivityView, error)`; REST `GET /api/v1/construction/query-activity-view/{projectID}/{activityID}`; MCP tool + OpId `constructionQueryActivityView`; OAS schema `ConstructionActivityView`.

- [ ] **Step 1: Write the failing façade tests** — append to `manager_test.go`:

  ```go
  // ===========================================================================
  // QueryActivityView — the façade (contract amendment, stage 0).
  // ===========================================================================

  // avManager builds a façade over a project, an episode ledger and a strict client.
  func avManager(c client.Client, proj projectstate.Project, eps *fakeEpisodes) *constructionManager {
  	ps := &fakeProjectState{project: proj}
  	return newConstructionManager(c, fakeFullProjectState{ps}, nil, nil, nil, nil, nil, fakeConstructionTransition{ps}, nil, nil, nil, eps, 0, "", nil)
  }

  func TestQueryActivityView_RefusesBlankIDs(t *testing.T) {
  	m := avManager(nil, ledgerChain(), &fakeEpisodes{})
  	for _, c := range []struct{ project, activity string }{{"", "A"}, {"p", ""}} {
  		_, err := m.QueryActivityView(testCtx(), ProjectID(c.project), ActivityID(c.activity))
  		if e := asConstructionError(t, err); e.Kind != fwmanager.ContractMisuse {
  			t.Fatalf("(%q,%q): want ContractMisuse, got %s", c.project, c.activity, e.Kind)
  		}
  	}
  }

  // An id the committed plan does not hold is NotFound — which is also the answer for the
  // requirements, architecture and projectDesign activities until stage 2 makes them real.
  func TestQueryActivityView_UnknownActivityIsNotFound(t *testing.T) {
  	m := avManager(nil, ledgerChain(), &fakeEpisodes{})
  	for _, id := range []string{"C-nope", "requirements", "architecture", "projectDesign"} {
  		_, err := m.QueryActivityView(testCtx(), "p", ActivityID(id))
  		if e := asConstructionError(t, err); e.Kind != fwmanager.NotFound || !strings.Contains(e.Detail, id) {
  			t.Fatalf("%s: want NotFound naming it, got %s %q", id, e.Kind, e.Detail)
  		}
  	}
  }

  // Not started: the whole lifecycle is returned, nothing has a revision, only the root is
  // pending, and no session is asked for (a nil client would panic if it were).
  func TestQueryActivityView_NotStarted_ReturnsTheWholeLifecycle(t *testing.T) {
  	v, err := avManager(nil, ledgerChain(), &fakeEpisodes{}).QueryActivityView(testCtx(), "p", "A")
  	if err != nil {
  		t.Fatalf("unexpected error: %v", err)
  	}
  	if v.State != ActivityViewNotStarted || v.Type != "service" || v.Name != "A" || v.ReviewSet != nil {
  		t.Fatalf("view = %+v, want a not-started service activity with no review set", v)
  	}
  	if len(v.Phases) != 5 || len(v.Tasks) != 10 {
  		t.Fatalf("want Figure A-1's 5 phases and 10 tasks, got %d and %d", len(v.Phases), len(v.Tasks))
  	}
  	for _, task := range v.Tasks {
  		want := ActivityTaskLocked
  		if len(task.DependsOn) == 0 {
  			want = ActivityTaskPending
  		}
  		if task.State != want || task.Revisions == nil || len(task.Revisions) != 0 || task.DependsOn == nil {
  			t.Errorf("%s: state=%s revisions=%v dependsOn=%v; want %s and empty, non-nil arrays", task.ID, task.State, task.Revisions, task.DependsOn, want)
  		}
  	}
  }

  // Done by its backfilled ledger: every task passed, every phase completed, provenance carried.
  func TestQueryActivityView_Done_FromTheLedger(t *testing.T) {
  	proj := ledgerChain()
  	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
  		"A": {ActivityID: "A", Attempts: passedLedger("A", servicePhases...)},
  	}
  	v, err := avManager(nil, proj, &fakeEpisodes{}).QueryActivityView(testCtx(), "p", "A")
  	if err != nil {
  		t.Fatalf("unexpected error: %v", err)
  	}
  	if v.State != ActivityViewDone {
  		t.Fatalf("state = %s, want done", v.State)
  	}
  	for _, ph := range v.Phases {
  		if !ph.Completed {
  			t.Errorf("lifecycle phase %s not completed", ph.ID)
  		}
  	}
  	for _, task := range v.Tasks {
  		if task.State != ActivityTaskPassed || len(task.Revisions) != 1 || task.Revisions[0].Provenance != TaskRevisionBackfilled {
  			t.Errorf("%s: %+v, want passed with one backfilled revision", task.ID, task)
  		}
  	}
  }

  // A live gate: the session is asked (the row is Running), the review task awaits the
  // human, the send-back is revision 1 with its note and comments, the episode is joined
  // by TargetRef = AttemptID, and the review set rides along.
  func TestQueryActivityView_LiveGate_AfterASendBack(t *testing.T) {
  	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
  	proj := ledgerChain()
  	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{"A": {
  		ActivityID: "A", Phase: projectstate.ActivityConstructionRunning, CurrentPhase: projectstate.MethodPhaseDetailedDesign,
  		Attempts: passedLedger("A", projectstate.MethodPhaseRequirements),
  		OperatorNotes: []projectstate.OperatorNote{{
  			NoteID: "n1", Kind: projectstate.NoteSendBack, Gate: "detailed_design", Text: "split the op", RecordedAt: t0.Add(20 * time.Minute),
  			Comments: []projectstate.NoteComment{{JSONPath: "$.ops[0]", Text: "too wide"}},
  		}},
  	}}
  	eps := &fakeEpisodes{listRecords: []episode.EpisodeRecord{
  		{EpisodeID: "ep-1", TargetRef: "A:detailedDesign:1", StartedAt: t0, EndedAt: t0.Add(10 * time.Minute)},
  		{EpisodeID: "ep-2", TargetRef: "A:detailedDesign:2", StartedAt: t0.Add(21 * time.Minute), EndedAt: t0.Add(30 * time.Minute)},
  	}}
  	since := t0.Add(31 * time.Minute)
  	live := awaitingAt("detailed_design")
  	live.AwaitingSince = &since
  	live.ReviewSet = &ReviewSet{Reviewers: []Reviewer{{Role: "architect", Perspective: "architecture", MayAmend: true}}}
  	mc := &temporalmocks.Client{}
  	mc.On("QueryWorkflow", mock.Anything, constructActivityWorkflowID("p", "A"), "", querySessionState).Return(encodedJSON{v: live}, nil)

  	v, err := avManager(mc, proj, eps).QueryActivityView(testCtx(), "p", "A")
  	if err != nil {
  		t.Fatalf("unexpected error: %v", err)
  	}
  	if eps.lastQuery.TargetRef == nil || *eps.lastQuery.TargetRef != "A" {
  		t.Fatalf("episodes must be listed for the activity, got %+v", eps.lastQuery)
  	}
  	if v.State != ActivityViewAwaitingHuman || v.ReviewSet == nil || len(v.ReviewSet.Reviewers) != 1 {
  		t.Fatalf("state=%s reviewSet=%+v, want awaitingHuman with the live review set", v.State, v.ReviewSet)
  	}
  	byID := map[string]ActivityTaskView{}
  	for _, task := range v.Tasks {
  		byID[task.ID] = task
  	}
  	work, gate := byID["detailedDesign"], byID["designReview"]
  	if work.State != ActivityTaskPassed || len(work.Revisions) != 2 || work.Revisions[1].EpisodeID == nil || *work.Revisions[1].EpisodeID != "ep-2" {
  		t.Fatalf("detailedDesign = %+v, want passed with revision 2 joined to ep-2", work)
  	}
  	if gate.State != ActivityTaskAwaitingHuman || len(gate.Revisions) != 2 {
  		t.Fatalf("designReview = %+v, want awaitingHuman with 2 revisions", gate)
  	}
  	r1, r2 := gate.Revisions[0], gate.Revisions[1]
  	if r1.Outcome != TaskRevisionSentBack || r1.Note == nil || *r1.Note != "split the op" || r1.CommentCount != 1 || len(r1.Comments) != 1 || r1.Comments[0].JSONPath != "$.ops[0]" {
  		t.Errorf("revision 1 = %+v, want sentBack carrying the note and its one comment", r1)
  	}
  	if r2.Outcome != TaskRevisionAwaitingHuman || r2.StartedAt == nil || !r2.StartedAt.Equal(since) {
  		t.Errorf("revision 2 = %+v, want awaitingHuman since %v", r2, since)
  	}
  	if byID["stp"].State != ActivityTaskPending || byID["construction"].State != ActivityTaskLocked {
  		t.Errorf("the test-plan branch is open (%s) and construction locked (%s) behind the design gate", byID["stp"].State, byID["construction"].State)
  	}
  }

  // A Running row whose session is gone (past retention) still reads — without a live gate.
  func TestQueryActivityView_RunningWithNoSession_StillReads(t *testing.T) {
  	proj := ledgerChain()
  	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
  		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionRunning},
  	}
  	mc := &temporalmocks.Client{}
  	mc.On("QueryWorkflow", mock.Anything, constructActivityWorkflowID("p", "A"), "", querySessionState).
  		Return(nil, serviceerror.NewNotFound("workflow not found"))
  	v, err := avManager(mc, proj, &fakeEpisodes{}).QueryActivityView(testCtx(), "p", "A")
  	if err != nil || v.State != ActivityViewRunning || v.ReviewSet != nil {
  		t.Fatalf("view=%+v err=%v, want a running view with no review set", v, err)
  	}
  }

  // The lifecycle key rule, over EVERY activity type and testing variant, and every key
  // resolves in the pinned method-assets. A stray variant on a non-testing type is ignored
  // (the zero variant is what every non-testing activity carries).
  func TestLifecycleTypeKey_CoversEveryTypeAndVariant(t *testing.T) {
  	cases := []struct {
  		typ     projectstate.ActivityType
  		variant projectstate.TestingVariant
  		want    string
  	}{
  		{projectstate.ActivityTypeService, projectstate.TestVariantPlan, "service"},
  		{projectstate.ActivityTypeFrontend, projectstate.TestVariantPlan, "frontend"},
  		{projectstate.ActivityTypeDeployment, projectstate.TestVariantPlan, "deployment"},
  		{projectstate.ActivityTypeDocumentation, projectstate.TestVariantPlan, "documentation"},
  		{projectstate.ActivityTypeUIDesign, projectstate.TestVariantPlan, "uiDesign"},
  		{projectstate.ActivityTypeIntegration, projectstate.TestVariantPlan, "integration"},
  		{projectstate.ActivityTypeTesting, projectstate.TestVariantPlan, "testing:plan"},
  		{projectstate.ActivityTypeTesting, projectstate.TestVariantHarness, "testing:harness"},
  		{projectstate.ActivityTypeTesting, projectstate.TestVariantPerf, "testing:perf"},
  		{projectstate.ActivityTypeTesting, projectstate.TestVariantSystemTest, "testing:systemTest"},
  		{projectstate.ActivityTypeTesting, projectstate.TestVariantQAProcess, "testing:qaProcess"},
  		{projectstate.ActivityTypeService, projectstate.TestVariantHarness, "service"},
  	}
  	for _, c := range cases {
  		got := lifecycleTypeKey(c.typ, c.variant)
  		if got != c.want {
  			t.Errorf("lifecycleTypeKey(%s, %s) = %q, want %q", c.typ, c.variant, got, c.want)
  		}
  		if _, ok := methodassets.LifecycleFor(got); !ok {
  			t.Errorf("method-assets has no lifecycle for key %q", got)
  		}
  	}
  }

  // Task 7's wire strings ARE the contract's enum values; this is what lets the façade
  // convert instead of mapping.
  func TestActivityViewWireStringsMatchTheContract(t *testing.T) {
  	pairs := map[string]string{
  		taskPending: string(ActivityTaskPending), taskLocked: string(ActivityTaskLocked), taskRunning: string(ActivityTaskRunning),
  		taskAwaitingHuman: string(ActivityTaskAwaitingHuman), taskPassed: string(ActivityTaskPassed),
  		taskSentBack: string(ActivityTaskSentBack), taskFailed: string(ActivityTaskFailed),
  		"rev:" + revRunning: "rev:" + string(TaskRevisionRunning), "rev:" + revAwaitingHuman: "rev:" + string(TaskRevisionAwaitingHuman),
  		"rev:" + revPassed: "rev:" + string(TaskRevisionPassed), "rev:" + revSentBack: "rev:" + string(TaskRevisionSentBack),
  		"rev:" + revFailed: "rev:" + string(TaskRevisionFailed), "rev:" + revSkipped: "rev:" + string(TaskRevisionSkipped),
  		methodassets.LifecycleTaskDispatch: string(ActivityTaskDispatch), methodassets.LifecycleTaskReview: string(ActivityTaskReview),
  	}
  	for internal, wire := range pairs {
  		if internal != wire {
  			t.Errorf("internal %q != contract %q", internal, wire)
  		}
  	}
  }
  ```
  `serviceerror.NewNotFound` is what this file's existing not-found tests build (~L2386, import at L23), and `isNotFound` (`constructionmanager.go` ~L1069) is what recognises it inside `GetSessionState`.

- [ ] **Step 2: Run; confirm the build fails.**
  ```bash
  cd server && GOWORK=off go test ./internal/manager/construction/ -run 'TestQueryActivityView|TestActivityViewWireStrings|TestLifecycleTypeKey' -count=1
  ```
  Expected: `m.QueryActivityView undefined`, `undefined: lifecycleTypeKey`, `undefined: ActivityViewNotStarted`, `undefined: ActivityTaskView` … `[build failed]`.

- [ ] **Step 3: Amend the contract** — `.aiarch/state/project.json`.

  (a) Append to `.serviceContracts.constructionManager.interface.operations`, after the `GetEpisodeTimeline` element (the shape is `GetSessionState`'s, minus `"pointer": true` — both ids are required, and a `$ref` to `ProjectID`/`ActivityID` is what makes each a PATH segment, F6):
  ```json
  {
    "name": "QueryActivityView",
    "params": [
      { "name": "projectID", "schema": { "$ref": "#/$defs/ProjectID" } },
      { "name": "activityID", "schema": { "$ref": "#/$defs/ActivityID" } }
    ],
    "result": { "$ref": "#/$defs/ActivityView" },
    "error": true
  }
  ```

  (b) Add to `.serviceContracts.constructionManager["$defs"]` (keep the file's key order convention — alphabetical within `$defs` is NOT enforced; place them together after `ActivityOverride`). `ReviewSet`, `ActivityID` and `ProjectID` are the existing defs, reused:
  ```json
  "ActivityView": {
    "type": "object",
    "description": "One activity's lifecycle, per-task revisions and live review set: the Activity Experience's single read. Derived on read from the attempt ledger, the episode ledger, the operator notes, the stored lifecycle-phase completions and the live session; nothing here is stored.",
    "properties": {
      "activityId": { "$ref": "#/$defs/ActivityID", "x-go-name": "ActivityID" },
      "name": { "type": "string", "description": "The activity's display title from the committed activity list; its id when the list carries no title." },
      "type": { "type": "string", "description": "The activity type's wire name: service, frontend, testing, deployment, documentation, uiDesign or integration." },
      "variant": { "type": "string", "description": "The testing variant's wire name (plan, harness, perf, systemTest or qaProcess). Omitted unless type is testing." },
      "componentId": { "type": "string", "x-go-name": "ComponentID", "description": "The architecture component this activity builds. Omitted for an activity with none (the system test plan, system testing)." },
      "state": { "$ref": "#/$defs/ActivityViewState" },
      "phases": { "type": "array", "items": { "$ref": "#/$defs/ActivityLifecyclePhase" }, "description": "The lifecycle phases (Figure A-2) in lifecycle order." },
      "tasks": { "type": "array", "items": { "$ref": "#/$defs/ActivityTaskView" }, "description": "Every task of the lifecycle DAG, in lifecycle order — including the tasks nothing has happened on yet." },
      "reviewSet": { "type": ["null"], "$ref": "#/$defs/ReviewSet", "description": "Who reviews the artifact at the gate the activity is waiting at. Present only while its live session awaits approval at a lifecycle-phase gate." }
    },
    "required": ["activityId", "name", "type", "state", "phases", "tasks"],
    "additionalProperties": false
  },
  "ActivityViewState": {
    "type": "string",
    "enum": ["notStarted", "running", "awaitingHuman", "done", "failed"],
    "x-enum-varnames": ["ActivityViewNotStarted", "ActivityViewRunning", "ActivityViewAwaitingHuman", "ActivityViewDone", "ActivityViewFailed"],
    "x-go-base": "string"
  },
  "ActivityLifecyclePhase": {
    "type": "object",
    "properties": {
      "id": { "type": "string", "x-go-name": "ID", "description": "The lifecycle phase's wire name: requirements, detailed_design, test_plan, construction or integration." },
      "label": { "type": "string" },
      "weight": { "type": "integer", "description": "The share of the activity's progress this lifecycle phase carries; the weights of one activity sum to 100." },
      "gateTaskId": { "type": "string", "x-go-name": "GateTaskID", "description": "The review task whose pass IS this lifecycle phase's binary exit criterion." },
      "completed": { "type": "boolean", "description": "True iff the gate task's state is passed." }
    },
    "required": ["id", "label", "weight", "gateTaskId", "completed"],
    "additionalProperties": false
  },
  "ActivityTaskKind": {
    "type": "string",
    "enum": ["dispatch", "review"],
    "x-enum-varnames": ["ActivityTaskDispatch", "ActivityTaskReview"],
    "x-go-base": "string"
  },
  "ActivityTaskState": {
    "type": "string",
    "enum": ["pending", "locked", "running", "awaitingHuman", "passed", "sentBack", "failed"],
    "x-enum-varnames": ["ActivityTaskPending", "ActivityTaskLocked", "ActivityTaskRunning", "ActivityTaskAwaitingHuman", "ActivityTaskPassed", "ActivityTaskSentBack", "ActivityTaskFailed"],
    "x-go-base": "string"
  },
  "ActivityTaskView": {
    "type": "object",
    "properties": {
      "id": { "type": "string", "x-go-name": "ID", "description": "The task id within the lifecycle (a Figure A-1 task id for a construction activity)." },
      "kind": { "$ref": "#/$defs/ActivityTaskKind" },
      "title": { "type": "string" },
      "phase": { "type": "string", "x-go-name": "LifecyclePhaseID", "description": "The id of the lifecycle phase this task belongs to." },
      "dependsOn": { "type": "array", "items": { "type": "string" }, "description": "The task ids that must pass before this one may start. Two tasks that share a predecessor run in parallel; a task with several waits for all of them." },
      "reviews": { "type": "string", "description": "For a review task, the dispatch task it judges — the pair a send-back re-opens. Omitted on a dispatch task." },
      "state": { "$ref": "#/$defs/ActivityTaskState" },
      "revisions": { "type": "array", "items": { "$ref": "#/$defs/TaskRevisionView" }, "description": "Oldest first. A dispatch task and the review task that judges it share revision numbers." }
    },
    "required": ["id", "kind", "title", "phase", "dependsOn", "state", "revisions"],
    "additionalProperties": false
  },
  "TaskRevisionOutcome": {
    "type": "string",
    "enum": ["running", "awaitingHuman", "passed", "sentBack", "failed", "skipped"],
    "x-enum-varnames": ["TaskRevisionRunning", "TaskRevisionAwaitingHuman", "TaskRevisionPassed", "TaskRevisionSentBack", "TaskRevisionFailed", "TaskRevisionSkipped"],
    "x-go-base": "string"
  },
  "TaskRevisionProvenance": {
    "type": "string",
    "enum": ["synthesized", "backfilled", "observed"],
    "x-enum-varnames": ["TaskRevisionSynthesized", "TaskRevisionBackfilled", "TaskRevisionObserved"],
    "x-go-base": "string",
    "description": "The worst origin among the revision's attempts: synthesized if any was fabricated, else backfilled if any was reconstructed from evidence recorded elsewhere, else observed."
  },
  "TaskRevisionComment": {
    "type": "object",
    "properties": {
      "jsonPath": { "type": "string", "x-go-name": "JSONPath" },
      "text": { "type": "string" }
    },
    "required": ["jsonPath", "text"],
    "additionalProperties": false
  },
  "TaskRevisionView": {
    "type": "object",
    "properties": {
      "n": { "type": "integer", "description": "1-based. Revision n is the n-th work that reached the gate, with every failed or retried attempt before it, and the n-th gate attempt that judged it." },
      "outcome": { "$ref": "#/$defs/TaskRevisionOutcome" },
      "startedAt": { "type": ["null", "string"], "format": "date-time", "x-go-import": "time", "x-go-type": "time.Time" },
      "endedAt": { "type": ["null", "string"], "format": "date-time", "x-go-import": "time", "x-go-type": "time.Time", "description": "Omitted while any attempt of the revision is unresolved." },
      "attemptIds": { "type": "array", "items": { "type": "string" }, "x-go-name": "AttemptIDs", "description": "Every attempt of the revision, as \"<activityId>:<task>:<n>\" — the TargetRef of each attempt's episode. More than one means the work was retried before it reached the gate." },
      "episodeId": { "type": "string", "x-go-name": "EpisodeID", "description": "The episode of the attempt that reached the gate (else the latest). Omitted on a review task and where no episode was captured." },
      "commentCount": { "type": "integer" },
      "comments": { "type": "array", "items": { "$ref": "#/$defs/TaskRevisionComment" }, "description": "The anchored comments that rode with a send-back. Empty unless outcome is sentBack." },
      "note": { "type": "string", "description": "The reviewer's send-back note, verbatim. Omitted unless outcome is sentBack and a note was recorded." },
      "provenance": { "$ref": "#/$defs/TaskRevisionProvenance" }
    },
    "required": ["n", "outcome", "attemptIds", "commentCount", "comments", "provenance"],
    "additionalProperties": false
  }
  ```
  The task's lifecycle-phase property is named `phase` on the wire (the spec's word) with `x-go-name: LifecyclePhaseID`, keeping the bare identifier out of new Go.

- [ ] **Step 4: Regenerate the Go contract.**
  ```bash
  cd server && make gen-models
  git diff --stat -- internal/manager/construction/contract.gen.go internal/manager/construction/fake/fake.gen.go
  ```
  - [ ] **Verify first:** read the generated `ActivityView`, `ActivityTaskView`, `TaskRevisionView` structs. The façade in Step 5 assumes: optional strings are pointers (`Variant, ComponentID, Reviews, EpisodeID, Note *string` — as `AwaitingGate *string` is today), integers are `int64` (as `Attempt int64` is), required arrays are plain slices, `ReviewSet *ReviewSet`. Where modelgen emitted something else, change Step 5's mapper to match the GENERATED shape — never the schema to match the mapper.

- [ ] **Step 5: Write the façade** — in `constructionmanager.go`, directly after `GetSessionState`:

  ```go
  // QueryActivityView — op 2.13 (unified-activity spec 2026-09-20, stage 0). The Activity
  // Experience's single read: one activity's platform-fixed lifecycle (method-assets),
  // each task's state and revisions, and the live review set. A PLAIN METHOD like the
  // episode reads — no workflow, no signal; it asks the activity's session (the same
  // Temporal Query GetSessionState serves) only while the row is Running, so a Done or
  // not-started activity reads with Temporal down.
  //
  // Everything is DERIVED on read (normalizeAttempts, deriveTaskViews); stage 3 stores
  // revisions and this becomes a projection. An id the committed activity list does not
  // hold is NotFound — today that includes the requirements, architecture and
  // projectDesign activities, which become real in stage 2.
  func (m *constructionManager) QueryActivityView(rc fwm.Context, projectID ProjectID, activityID ActivityID) (ActivityView, error) {
  	ctx := rc.Context
  	if projectID == "" {
  		return ActivityView{}, newError(fwm.ContractMisuse, "empty projectId")
  	}
  	if activityID == "" {
  		return ActivityView{}, newError(fwm.ContractMisuse, "empty activityId")
  	}
  	id := string(activityID)
  	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
  	if err != nil {
  		return ActivityView{}, mapRAError(err, "projectStateAccess.ReadProject")
  	}
  	item, ok := committedActivityItem(proj, id)
  	if !ok {
  		return ActivityView{}, newError(fwm.NotFound, "no activity "+id+" in the committed activity list")
  	}
  	row := proj.ActivityConstruction[id]
  	row.ActivityID = id
  	typ, variant, _, classified := projectstate.ResolveConstructionRow(row, item)
  	if !classified {
  		return ActivityView{}, newError(fwm.FailedPrecondition, fmt.Sprintf(
  			"activity %s (workerClass %q, coding=%v) matches no activity-classification rule, so it has no lifecycle — amend workerClass or coding in the committed activity list",
  			id, item.WorkerClass, item.Coding))
  	}
  	key := lifecycleTypeKey(typ, variant)
  	lc, ok := methodassets.LifecycleFor(key)
  	if !ok {
  		return ActivityView{}, newError(fwm.Infrastructure, "the platform's method assets carry no lifecycle for activity type "+key)
  	}
  	coarse, _ := projectstate.EffectiveConstructionPhase(row, item)
  	live, err := m.liveSessionFor(ctx, projectID, activityID, coarse)
  	if err != nil {
  		return ActivityView{}, err
  	}
  	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{ProjectID: episode.ProjectID(projectID), TargetRef: &id})
  	if err != nil {
  		return ActivityView{}, mapRAError(err, "episodeAccess.ListEpisodes")
  	}
  	liveGate, _ := liveApprovalGate(live)
  	tasks := deriveTaskViews(lc, normalizeAttempts(id, row, records, live), row.OperatorNotes, liveGate)
  	view := activityViewFrom(activityID, item, typ, variant, lc, tasks)
  	view.State = activityViewState(coarse, live)
  	if liveGate != "" {
  		view.ReviewSet = live.ReviewSet
  	}
  	return view, nil
  }
  ```
  and, in the Task-7 section at the end of the file:

  ```go
  // committedActivityItem finds an activity in the committed Phase-2 activity list.
  func committedActivityItem(proj projectstate.Project, id string) (projectstate.ActivityItem, bool) {
  	_, list, ok := committedPlanInputs(proj)
  	if !ok {
  		return projectstate.ActivityItem{}, false
  	}
  	for _, it := range list.Activities {
  		if it.Name == id {
  			return it, true
  		}
  	}
  	return projectstate.ActivityItem{}, false
  }

  // lifecycleTypeKey is the method-assets lifecycle key: the activity type's wire name,
  // and "testing:<variant wire name>" for a testing activity (partAB decision D2).
  func lifecycleTypeKey(t projectstate.ActivityType, v projectstate.TestingVariant) string {
  	if t == projectstate.ActivityTypeTesting {
  		return t.String() + ":" + v.String()
  	}
  	return t.String()
  }

  // liveSessionFor asks the activity's session only while its row is Running: a
  // not-started, done or failed activity has no gate to show, and must read with Temporal
  // down. No session (never dispatched, or past retention) is not an error.
  func (m *constructionManager) liveSessionFor(ctx context.Context, projectID ProjectID, activityID ActivityID, coarse projectstate.ActivityConstructionPhase) (*ConstructionSessionView, error) {
  	if coarse != projectstate.ActivityConstructionRunning {
  		return nil, nil //nolint:nilnil // "no live session" is a value here, not a failure
  	}
  	v, err := m.activitySession(ctx, projectID, activityID)
  	if err != nil {
  		var fe *fwm.Error
  		if errors.As(err, &fe) && fe.Kind == fwm.NotFound {
  			return nil, nil //nolint:nilnil // see above
  		}
  		return nil, err
  	}
  	return &v, nil
  }

  // activityViewState folds the row's effective coarse state and the live session into
  // the view's five states.
  func activityViewState(coarse projectstate.ActivityConstructionPhase, live *ConstructionSessionView) ActivityViewState {
  	switch coarse {
  	case projectstate.ActivityConstructionNotStarted:
  		return ActivityViewNotStarted
  	case projectstate.ActivityConstructionDone:
  		return ActivityViewDone
  	case projectstate.ActivityConstructionFailed:
  		return ActivityViewFailed
  	case projectstate.ActivityConstructionRunning:
  		if live != nil && (live.Stage == StageAwaitingApproval || live.Stage == StageAwaitingTakeover) {
  			return ActivityViewAwaitingHuman
  		}
  	}
  	return ActivityViewRunning
  }

  // activityViewFrom assembles the contract view. Every array is non-nil: the wire carries
  // [] for "none", never null.
  func activityViewFrom(activityID ActivityID, item projectstate.ActivityItem, typ projectstate.ActivityType, variant projectstate.TestingVariant, lc methodassets.Lifecycle, tasks []taskView) ActivityView {
  	states := make(map[string]string, len(tasks))
  	revisions := make(map[string][]taskRevision, len(tasks))
  	for _, t := range tasks {
  		states[t.ID], revisions[t.ID] = t.State, t.Revisions
  	}
  	view := ActivityView{ActivityID: activityID, Name: item.Title, Type: typ.String(),
  		Phases: make([]ActivityLifecyclePhase, 0, len(lc.Phases)), Tasks: make([]ActivityTaskView, 0, len(lc.Tasks))}
  	if view.Name == "" {
  		view.Name = item.Name
  	}
  	if typ == projectstate.ActivityTypeTesting {
  		view.Variant = strPtrOrNil(variant.String())
  	}
  	view.ComponentID = strPtrOrNil(item.ComponentID)
  	for _, ph := range lc.Phases {
  		view.Phases = append(view.Phases, ActivityLifecyclePhase{
  			ID: ph.ID, Label: ph.Label, Weight: int64(ph.Weight), GateTaskID: ph.Gate, Completed: states[ph.Gate] == taskPassed,
  		})
  	}
  	for _, t := range lc.Tasks {
  		view.Tasks = append(view.Tasks, ActivityTaskView{
  			ID: t.ID, Kind: ActivityTaskKind(t.Kind), Title: t.Title, LifecyclePhaseID: t.Phase,
  			DependsOn: append([]string{}, t.DependsOn...), Reviews: strPtrOrNil(t.Reviews),
  			State: ActivityTaskState(states[t.ID]), Revisions: revisionViews(revisions[t.ID]),
  		})
  	}
  	return view
  }

  func revisionViews(revs []taskRevision) []TaskRevisionView {
  	out := make([]TaskRevisionView, 0, len(revs))
  	for _, r := range revs {
  		comments := make([]TaskRevisionComment, 0, len(r.Comments))
  		for _, c := range r.Comments {
  			comments = append(comments, TaskRevisionComment{JSONPath: c.JSONPath, Text: c.Text})
  		}
  		out = append(out, TaskRevisionView{
  			N: int64(r.N), Outcome: TaskRevisionOutcome(r.Outcome), StartedAt: r.StartedAt, EndedAt: r.EndedAt,
  			AttemptIDs: append([]string{}, r.AttemptIDs...), EpisodeID: strPtrOrNil(r.EpisodeID),
  			CommentCount: int64(len(r.Comments)), Comments: comments, Note: strPtrOrNil(r.Note),
  			Provenance: revisionProvenance(r.Provenance),
  		})
  	}
  	return out
  }

  // revisionProvenance names the origin on the wire. OriginSynthesized is the EMPTY string
  // in storage on purpose (a dropped stamp fails suspicious); the wire spells it out.
  func revisionProvenance(o projectstate.RecordOrigin) TaskRevisionProvenance {
  	switch o {
  	case projectstate.OriginObserved:
  		return TaskRevisionObserved
  	case projectstate.OriginBackfilled:
  		return TaskRevisionBackfilled
  	case projectstate.OriginSynthesized:
  		return TaskRevisionSynthesized
  	}
  	return TaskRevisionSynthesized // an unknown origin is as bad as synthesized (originRank)
  }
  ```
  `strPtrOrNil` does not exist in this package (verified: only `manager/systemdesign` ~L1615 and `manager/projectdesign` ~L1391 declare one, each package-private). Add it beside `revisionProvenance`:
  ```go
  // strPtrOrNil is the optional-string idiom of the generated contract: "" is omitted.
  func strPtrOrNil(s string) *string {
  	if s == "" {
  		return nil
  	}
  	return &s
  }
  ```
  `//nolint:nilnil // <reason>` is the repo's accepted form for a documented (nil, nil) value (`projectstateaccess.go` ~L3341, ~L3461).

- [ ] **Step 6: Run the façade tests and the package.**
  ```bash
  cd server && GOWORK=off go test ./internal/manager/construction/ -run 'TestQueryActivityView|TestActivityViewWireStrings|TestLifecycleTypeKey|TestDeriveTaskViews|TestNormalizeAttempts' -count=1 -v
  GOWORK=off go test ./internal/manager/construction/ -count=1
  ```
  Expected: all `PASS`. `TestQueryActivityView_NotStarted…` and `…Done…` exercise the REAL `methodassets.LifecycleFor("service")`; a 10-task/5-phase mismatch there means the pinned method-assets release and partAB's `lifecycles.json` disagree with spec §3 — report it, do not loosen the assertion.

- [ ] **Step 7: Regenerate the client layer, the SDK and the webApp contracts.**
  ```bash
  cd server && make gen-client gen-temporal
  grep -n 'query-activity-view' api/openapi.yaml
  cd ../webApp && npm run gen:api && npm run gen:ops
  grep -n 'constructionQueryActivityView' -A4 src/api/ops.gen.ts
  ```
  Expected: `api/openapi.yaml` gains `/api/v1/construction/query-activity-view/{projectID}/{activityID}` (`get`) and the `Construction*` schemas for the nine defs; `internal/client/web` a handler, `internal/client/mcp` a `constructionQueryActivityView` tool; `systemtests/internal/sdk` the SDK method; `ops.gen.ts`:
  ```ts
  constructionQueryActivityView: {
    method: 'GET',
    path: '/api/v1/construction/query-activity-view/{projectID}/{activityID}',
    tool: 'constructionQueryActivityView',
  },
  ```
  `make gen-internal-tools` is NOT needed (it covers ResourceAccess/Engine ops only) and `make gen-uiprofiles` is untouched. If the verb is `POST` or `activityID` lands in the query string, the contract edit in Step 3(a) differs from `GetSessionState`'s param shape — fix the JSON, not the generator.

- [ ] **Step 8: Update the design-health pin's comment** — `server/internal/engine/designhealth/engine_test.go:59`. Replace
  ```go
  	// systemDesignManager carries 13 ops — past the App-C max of 12 (Warning).
  ```
  with
  ```go
  	// systemDesignManager and constructionManager each carry 13 ops — past the App-C
  	// max of 12 (Warning). constructionManager's 13th is the read-only QueryActivityView
  	// (unified-activity spec 2026-09-20, stage 0); both contracts dissolve into the 12-op
  	// deliveryManager in stage 4. The index is by rule id, so one assertion covers both.
  ```
  No assertion changes (F5). Run `GOWORK=off go test ./internal/engine/designhealth/ -count=1` → `ok`, and confirm the new Warning is the only new finding:
  ```bash
  cd server && GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System 2>&1 | grep -c 'DH-CONTRACT-OPCOUNT-MAX'
  ```
  Expected: `2` (was `1`), exit status 0 — a Warning does not fail validate.
  - [ ] **Verify first:** if `validate --slot System` does not print contract-family findings at all (they may be scoped out of the System slot), run the unscoped `validate --root ..` and compare its `OPCOUNT` lines before/after instead.

- [ ] **Step 9: The self-amendment gate loop + every drift gate.**
  ```bash
  cd server && git add ../.aiarch/state/project.json . ../systemtests/internal/sdk ../webApp/src/contracts/schema.ts ../webApp/src/api/ops.gen.ts
  make gen-models-check gen-fakes-check gen-client-check gen-internal-tools-check gen-temporal-check gen-sdk-check gen-config-check gen-main-check gen-uiprofiles-check gen-lifecycles-check derived-plan-check
  make method-check
  GOWORK=off go run ./cmd/aiarch-state-mcp validate --root .. --slot System
  GOWORK=off make test-short
  make lint && make sumtype-check && make fix-check && make encapsulation-check
  cd ../webApp && npm run check
  ```
  Expected: all green. `method-check` stays green because a new op on an existing built contract touches no ALIGN-*/CC-* rule (no component, package or call-chain edge changes — spec §8 stage 0). `TestManagerRequiredStringsAreInspected` passes on the two `== ""` branches. `TestRegisteredTemporalNamesGolden` is unchanged (F7) — if it fails, a generator emitted a Temporal name for the new op; stop and report rather than editing the golden.

- [ ] **Step 10: Commit.**
  ```bash
  git add .aiarch/state/project.json server systemtests/internal/sdk webApp/src/contracts/schema.ts webApp/src/api/ops.gen.ts
  git commit -m "$(cat <<'EOF'
  feat(construction): QueryActivityView, the activity experience's single read (design amendment)

  A read-only 13th op on constructionManager: one activity's platform-fixed
  lifecycle DAG, each task's state and revisions, and the live review set.
  Derived on read from the attempt ledger, the episode ledger, operator notes,
  stored lifecycle-phase completions and the live session. Unknown activities,
  including the not-yet-real requirements/architecture/projectDesign, are
  NotFound. 12 to 13 ops raises one DH-CONTRACT-OPCOUNT-MAX warning.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  EOF
  )"
  ```

---

### Task 9: webApp — `useActivityView`, its polling rule, and the recorded preview fixtures (Part C)

**Depends on:** Task 8 (`schema.ts` and `ops.gen.ts` carry `constructionQueryActivityView`).

**Files:**
- Create: `webApp/src/hooks/activityViewPolling.ts` — the pure cadence rule
- Create: `webApp/src/hooks/activityViewPolling.test.ts` — `node:test`
- Create: `webApp/src/hooks/useActivityView.ts`
- Modify: `webApp/src/hooks/useConstructionMutations.ts` — wherever `constructionSessionsKey(projectId)` is invalidated (~L95, ~L170), also invalidate `activityViewsKey(projectId)`
- Create: `webApp/preview/fixtures/web-client/activity-experience/deployment-linear.json`, `service-fork-sent-back.json`, `done.json`, `not-started.json`
- Modify: `webApp/scripts/fixture-schema.test.mjs` — one added test

**Layer check (lint-enforced DAG routes→containers→components→hooks→api):** the three new `src/hooks/` files import only `../api/*`, `../contracts/*`, `@tanstack/react-query` and each other — the same edges `useConstructionSession.ts` uses. No component, container or route is touched; the Activity Experience screen is stage 5.

**Where fixtures live and how an op is keyed (verified):** one file is ONE screen state at `<root>/<surface>/<screenId>/<stateId>.json`, surface `web-client`, screen and state ids kebab (`fixture-schema.mjs` `validateFixtureTree`, `KEBAB`). Shape: `{ "route": "/…", "title"?, "note"?, "ops": { "<OpId>": { "result": <the op's 200 body> } | { "error": {…} } | { "pending": true } } }`. The `ops` keys are exactly the OpsClient OpIds (`op-bindings.mjs`, same derivation as `ops.gen.ts`), and each `result` is validated against that op's OAS 200 schema — here `ConstructionActivityView`. `src/api/fixtureOps.ts` answers a call by OpId from that map and rejects loudly (`FixtureMissError`, status 500) for an op the file does not answer. Two roots exist: `uitests/preview-fixtures` (Playwright's, test-local) and **`webApp/preview/fixtures`** — "the recorded-design location the U-SPA-web-client Design phase will fill" (`fixture-schema.test.mjs` `DESIGN_FIXTURES`), validated by `npm test` whenever it exists and by `vite.preview.config.ts` on a preview build. It does not exist yet; this task creates it.

**The format allows a fixture for a screen that does not exist.** Validation checks only `route` (`^/`) and each op's result; nothing resolves the route. So the four states are recorded NOW, under screen id `activity-experience`, at the route spec §7.2 fixes (`/project/$projectId/activity/$activityId`). Until stage 5 adds that route, opening one in the preview shell lands on the router's not-found page — expected, and harmless to `npm run check` and to the uitests (which build over their own root). Each file answers ONLY `constructionQueryActivityView`; stage 5 adds the screen's other reads (`compositionGetUserinfo`, `systemDesignGetProject`, …) when it knows what the screen calls.

**Interfaces:**
- Consumes: `OpResult<'constructionQueryActivityView'>` (`src/api/opTypes.ts`), `OpsClient.callForBody`, `useOpsClient`, `isNoSessionError` (`sessionPolling.ts` — "an `ApiError` with status 404"), `ApiError`.
- Produces:
  ```ts
  // activityViewPolling.ts
  export const ACTIVITY_LIVE_POLL_MS = 2000;
  export const ACTIVITY_GATE_POLL_MS = 8000;
  export const ACTIVITY_DEGRADED_POLL_MS = 5000;
  export interface ActivityViewPollInput { readonly state: string; readonly tasks: readonly { readonly state: string }[] }
  export function activityViewPollIntervalMs(data: ActivityViewPollInput | undefined, error: unknown, notStartedPollMs?: number | false): number | false;
  // useActivityView.ts
  export type ActivityView = OpResult<'constructionQueryActivityView'>;
  export function activityViewKey(projectId: string, activityId: string): readonly unknown[];   // ['activityView', projectId, activityId]
  export function activityViewsKey(projectId: string): readonly unknown[];                        // ['activityView', projectId]
  export function activityViewQueryOptions(ops: OpsClient, projectId: string, activityId: string | undefined, enabled: boolean, notStartedPollMs?: number | false): UseQueryOptions<ActivityView, Error, ActivityView>;
  export function useActivityView(projectId: string, activityId?: string, enabled?: boolean): UseQueryResult<ActivityView>;
  ```

**Cadence decision table (first match wins):**

| data | error | interval | why |
|---|---|---|---|
| any | 404 | stop | the activity is not in the committed plan; only a plan change (which invalidates) can alter that |
| any | other | 5s | transient fault: degrade, never stop — this query is its own refresh authority and a stopped poll is permanent (F-QA2-28) |
| none | none | stop | pristine mount; the first settle re-evaluates |
| any task `running` | none | 2s | an agent is working — with the fork this holds even while another branch waits at a gate |
| state `running` | none | 2s | between tasks: the next dispatch is seconds away |
| state `awaitingHuman` | none | 8s | the human is the actor; the poll is the safety net for another tab's decision or a lost response (F-QA2-48) |
| state `done` / `failed` | none | stop | terminal; Retry/override are mutations and invalidate |
| state `notStarted` | none | `notStartedPollMs` (default stop) | the brief's rule; a caller that must notice the sweep dispatching it passes a cadence — same seam as `sessionQueryOptions`' `absentPollMs` |

- [ ] **Step 1: Write the failing polling test** — `webApp/src/hooks/activityViewPolling.test.ts`:

  ```ts
  /// <reference types="node" />
  /**
   * Unit tests for the activity-view poll cadence (src/hooks/activityViewPolling.ts) —
   * one case per row of its decision table.
   */
  import { test } from 'node:test';
  import assert from 'node:assert/strict';
  import { ApiError } from '../contracts/errors.ts';
  import {
    ACTIVITY_DEGRADED_POLL_MS,
    ACTIVITY_GATE_POLL_MS,
    ACTIVITY_LIVE_POLL_MS,
    activityViewPollIntervalMs,
  } from './activityViewPolling.ts';

  const view = (state: string, ...taskStates: string[]) => ({
    state,
    tasks: taskStates.map((s) => ({ state: s })),
  });

  void test('a running task polls at the live cadence, even while another branch waits at a gate', () => {
    assert.equal(
      activityViewPollIntervalMs(view('awaitingHuman', 'passed', 'awaitingHuman', 'running'), null),
      ACTIVITY_LIVE_POLL_MS
    );
    assert.equal(activityViewPollIntervalMs(view('running', 'passed', 'running'), null), ACTIVITY_LIVE_POLL_MS);
  });

  void test('a running activity between tasks still polls at the live cadence', () => {
    assert.equal(activityViewPollIntervalMs(view('running', 'passed', 'pending'), null), ACTIVITY_LIVE_POLL_MS);
  });

  void test('a gate with nothing running polls slowly: the human is the actor', () => {
    assert.equal(
      activityViewPollIntervalMs(view('awaitingHuman', 'passed', 'awaitingHuman', 'locked'), null),
      ACTIVITY_GATE_POLL_MS
    );
  });

  void test('done, failed and not-started stop; not-started polls when the caller asks', () => {
    for (const state of ['done', 'failed', 'notStarted']) {
      assert.equal(activityViewPollIntervalMs(view(state, 'passed'), null), false, state);
    }
    assert.equal(activityViewPollIntervalMs(view('notStarted', 'pending'), null, 4000), 4000);
    assert.equal(activityViewPollIntervalMs(view('done', 'passed'), null, 4000), false);
  });

  void test('a pristine mount adds no interval', () => {
    assert.equal(activityViewPollIntervalMs(undefined, null), false);
    assert.equal(activityViewPollIntervalMs(undefined, undefined), false);
  });

  void test('an unknown activity (404) stops; any other fault degrades and never stops', () => {
    const notFound = new ApiError(404, 'not_found', 'no activity C-nope in the committed activity list');
    const blip = new ApiError(503, 'unavailable', 'blip');
    assert.equal(activityViewPollIntervalMs(undefined, notFound), false);
    assert.equal(activityViewPollIntervalMs(view('running', 'running'), notFound), false);
    assert.equal(activityViewPollIntervalMs(undefined, blip), ACTIVITY_DEGRADED_POLL_MS);
    assert.equal(activityViewPollIntervalMs(view('done', 'passed'), blip), ACTIVITY_DEGRADED_POLL_MS);
    assert.equal(activityViewPollIntervalMs(view('running', 'running'), new Error('ECONNREFUSED')), ACTIVITY_DEGRADED_POLL_MS);
  });
  ```

- [ ] **Step 2: Run; confirm it fails.**
  ```bash
  cd webApp && node --test src/hooks/activityViewPolling.test.ts
  ```
  Expected: `ERR_MODULE_NOT_FOUND` for `./activityViewPolling.ts`.

- [ ] **Step 3: Implement the rule** — `webApp/src/hooks/activityViewPolling.ts`:

  ```ts
  /**
   * Pure poll-cadence logic for the activity-view query (useActivityView) — the
   * sessionPolling.ts pattern, for the Activity Experience's single read.
   *
   * DECISION TABLE (first match wins; data = the last ActivityView, error = the
   * last fetch error):
   *
   *   data                   | error | interval          | why
   *   -----------------------|-------|-------------------|-------------------------------
   *   (any)                  | 404   | stop              | not in the committed plan; a
   *                          |       |                   | plan change invalidates
   *   (any)                  | other | 5s                | transient: degrade, NEVER stop —
   *                          |       |                   | this query is its own refresh
   *                          |       |                   | authority (F-QA2-28)
   *   (none)                 | none  | stop              | pristine mount
   *   any task running       | none  | 2s                | an agent is working; with the
   *                          |       |                   | fork, even beside an owed gate
   *   state running          | none  | 2s                | between tasks
   *   state awaitingHuman    | none  | 8s                | the human is the actor; the poll
   *                          |       |                   | is the safety net (F-QA2-48)
   *   state done / failed    | none  | stop              | terminal; mutations invalidate
   *   state notStarted       | none  | notStartedPollMs  | default stop; a caller that must
   *                          |       |                   | see the sweep dispatch it opts in
   */
  // Runtime imports carry explicit .ts extensions so this module also loads under
  // node:test's type-stripping (activityViewPolling.test.ts).
  import { isNoSessionError } from './sessionPolling.ts';

  export const ACTIVITY_LIVE_POLL_MS = 2000;
  export const ACTIVITY_GATE_POLL_MS = 8000;
  export const ACTIVITY_DEGRADED_POLL_MS = 5000;

  /** The slice of an ActivityView the cadence reads (structural, so node:test needs no schema). */
  export interface ActivityViewPollInput {
    readonly state: string;
    readonly tasks: readonly { readonly state: string }[];
  }

  export function activityViewPollIntervalMs(
    data: ActivityViewPollInput | undefined,
    error: unknown,
    notStartedPollMs: number | false = false
  ): number | false {
    if (error !== null && error !== undefined) {
      // isNoSessionError is "an ApiError with status 404": here, an activity the
      // committed plan does not hold.
      return isNoSessionError(error) ? false : ACTIVITY_DEGRADED_POLL_MS;
    }
    if (data === undefined) return false;
    if (data.tasks.some((t) => t.state === 'running') || data.state === 'running') {
      return ACTIVITY_LIVE_POLL_MS;
    }
    if (data.state === 'awaitingHuman') return ACTIVITY_GATE_POLL_MS;
    if (data.state === 'notStarted') return notStartedPollMs;
    return false;
  }
  ```
  Run: `node --test src/hooks/activityViewPolling.test.ts` → 6 passing.

- [ ] **Step 4: Write the hook** — `webApp/src/hooks/useActivityView.ts`:

  ```ts
  /**
   * The Activity Experience's single read: one activity's lifecycle DAG, each task's
   * state and revisions, and the live review set (constructionManager.QueryActivityView).
   * Follows useConstructionSession's conventions: a key factory, exported query options
   * (so a fan-out can share the cache), and a pure cadence rule (activityViewPolling.ts).
   *
   * Unlike the session probe, absence is an ERROR here, not a value: a 404 means the
   * activity is not in the committed plan, which the screen must say, and it never
   * flips back on its own — so it is neither retried nor polled.
   */
  import { useQuery, type UseQueryOptions, type UseQueryResult } from '@tanstack/react-query';
  import type { OpsClient } from '../api/ops.gen';
  import { useOpsClient } from '../api/opsContext';
  import type { OpResult } from '../api/opTypes';
  import { activityViewPollIntervalMs } from './activityViewPolling';
  import { isNoSessionError } from './sessionPolling';

  export type ActivityView = OpResult<'constructionQueryActivityView'>;

  export function activityViewKey(projectId: string, activityId: string): readonly unknown[] {
    return ['activityView', projectId, activityId];
  }

  /** The prefix every activity view of one project shares — what a decision, an
   *  override or a Begin invalidates. */
  export function activityViewsKey(projectId: string): readonly unknown[] {
    return ['activityView', projectId];
  }

  export function activityViewQueryOptions(
    /** The transport the read rides (useOpsClient().ops). */
    ops: OpsClient,
    projectId: string,
    activityId: string | undefined,
    enabled: boolean,
    notStartedPollMs: number | false = false
  ): UseQueryOptions<ActivityView, Error, ActivityView> {
    const id = activityId ?? '';
    return {
      queryKey: activityViewKey(projectId, id),
      queryFn: () =>
        ops.callForBody<ActivityView>('constructionQueryActivityView', {
          path: { projectID: projectId, activityID: id },
        }),
      enabled: enabled && projectId.length > 0 && id.length > 0,
      retry: (count, error) => !isNoSessionError(error) && count < 1,
      refetchInterval: (query): number | false =>
        activityViewPollIntervalMs(query.state.data, query.state.error, notStartedPollMs),
    };
  }

  export function useActivityView(
    projectId: string,
    activityId?: string,
    enabled = true
  ): UseQueryResult<ActivityView> {
    const { ops } = useOpsClient();
    return useQuery(activityViewQueryOptions(ops, projectId, activityId, enabled));
  }
  ```
  - [ ] **Verify first:** `npm run typecheck`. `OpResult<'constructionQueryActivityView'>` must resolve to the generated `ConstructionActivityView` shape, not `never` — `never` means `ops.gen.ts`'s path and `schema.ts`'s `paths` key disagree (re-run `npm run gen:api && npm run gen:ops` from Task 8 Step 7). If `state`/`tasks[].state` are typed as string-literal unions, `ActivityViewPollInput`'s `string` fields accept them structurally — no cast.

- [ ] **Step 5: Invalidate the view where the session is invalidated** — `webApp/src/hooks/useConstructionMutations.ts`. Import `activityViewsKey` from `./useActivityView`, and at each site that invalidates `constructionSessionsKey(projectId)` (today ~L95 in the Begin mutation and ~L170 in the decision/override path) add the sibling call, e.g.:
  ```ts
        client.invalidateQueries({ queryKey: constructionSessionsKey(projectId) }),
        client.invalidateQueries({ queryKey: activityViewsKey(projectId) }),
  ```
  - [ ] **Verify first:** `grep -n "constructionSessionsKey(projectId)\|\['constructionSession', projectId\]" src/hooks/useConstructionMutations.ts` and cover EVERY hit (one site, ~L148, spells the key inline). Match each site's form — element of a `Promise.all([...])` array vs an `await`ed statement.

- [ ] **Step 6: Record the four fixtures.** Create the directory `webApp/preview/fixtures/web-client/activity-experience/` and these files.

  `deployment-linear.json` — a linear (no fork) deployment activity mid-construction:
  ```json
  {
    "route": "/project/archistrator/activity/R-github",
    "title": "Activity Experience — linear deployment activity, construction running",
    "note": "R-github: the provisioning spec passed its review first time; the infrastructure change is being built now. No fork: every task has exactly one predecessor. Live-run revisions read backfilled because stage 0 reconstructs them from the episode ledger and the stored lifecycle-phase completions.",
    "ops": {
      "constructionQueryActivityView": {
        "result": {
          "activityId": "R-github",
          "name": "Provision GitHub",
          "type": "deployment",
          "componentId": "github",
          "state": "running",
          "phases": [
            { "id": "detailed_design", "label": "Provisioning Spec", "weight": 25, "gateTaskId": "designReview", "completed": true },
            { "id": "construction", "label": "Construction", "weight": 50, "gateTaskId": "codeReview", "completed": false },
            { "id": "integration", "label": "Convergence Verification", "weight": 25, "gateTaskId": "testing", "completed": false }
          ],
          "tasks": [
            { "id": "detailedDesign", "kind": "dispatch", "title": "Provisioning Spec", "phase": "detailed_design", "dependsOn": [], "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "startedAt": "2026-09-18T14:00:00Z", "endedAt": "2026-09-18T14:21:40Z", "attemptIds": ["R-github:detailedDesign:1"], "episodeId": "ep-rgh-dd-1", "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "designReview", "kind": "review", "title": "Spec Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign", "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "endedAt": "2026-09-18T15:02:10Z", "attemptIds": ["R-github:designReview:1"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "construction", "kind": "dispatch", "title": "Construction", "phase": "construction", "dependsOn": ["designReview"], "state": "running", "revisions": [
              { "n": 1, "outcome": "running", "attemptIds": ["R-github:construction:1"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "codeReview", "kind": "review", "title": "Change Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction", "state": "locked", "revisions": [] },
            { "id": "integration", "kind": "dispatch", "title": "Rollout", "phase": "integration", "dependsOn": ["codeReview"], "state": "locked", "revisions": [] },
            { "id": "testing", "kind": "review", "title": "Convergence Verification", "phase": "integration", "dependsOn": ["integration"], "reviews": "integration", "state": "locked", "revisions": [] }
          ]
        }
      }
    }
  }
  ```

  `service-fork-sent-back.json` — mid-fork: the design review was sent back once and is at the gate again (2 revisions) while the test-plan branch runs:
  ```json
  {
    "route": "/project/archistrator/activity/C-billing-state-access?task=designReview",
    "title": "Activity Experience — service activity mid-fork, design review sent back once, STP running",
    "note": "Both branches of Figure A-1's fork are live: Design Review is at revision 2 awaiting the human (revision 1 was sent back with two anchored comments) while STP runs in parallel. Today's construct workflow walks lifecycle phases one at a time and cannot produce this; it is the stage-4 parallel child's shape, which the stage-0 contract already carries. A running task polls at 2s even though a gate is owed.",
    "ops": {
      "constructionQueryActivityView": {
        "result": {
          "activityId": "C-billing-state-access",
          "name": "Build BillingStateAccess",
          "type": "service",
          "componentId": "billing-state-access",
          "state": "awaitingHuman",
          "phases": [
            { "id": "requirements", "label": "Requirements", "weight": 15, "gateTaskId": "srsReview", "completed": true },
            { "id": "detailed_design", "label": "Detailed Design", "weight": 20, "gateTaskId": "designReview", "completed": false },
            { "id": "test_plan", "label": "Test Plan", "weight": 10, "gateTaskId": "stpReview", "completed": false },
            { "id": "construction", "label": "Construction", "weight": 40, "gateTaskId": "codeReview", "completed": false },
            { "id": "integration", "label": "Integration", "weight": 15, "gateTaskId": "testing", "completed": false }
          ],
          "tasks": [
            { "id": "srs", "kind": "dispatch", "title": "SRS", "phase": "requirements", "dependsOn": [], "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "startedAt": "2026-09-19T09:00:00Z", "endedAt": "2026-09-19T09:12:05Z", "attemptIds": ["C-billing-state-access:srs:1"], "episodeId": "ep-bsa-srs-1", "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "srsReview", "kind": "review", "title": "SRS Review", "phase": "requirements", "dependsOn": ["srs"], "reviews": "srs", "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "endedAt": "2026-09-19T09:40:00Z", "attemptIds": ["C-billing-state-access:srsReview:1"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "detailedDesign", "kind": "dispatch", "title": "Detailed Design", "phase": "detailed_design", "dependsOn": ["srsReview"], "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "startedAt": "2026-09-19T09:41:00Z", "endedAt": "2026-09-19T10:05:30Z", "attemptIds": ["C-billing-state-access:detailedDesign:1", "C-billing-state-access:detailedDesign:2"], "episodeId": "ep-bsa-dd-2", "commentCount": 0, "comments": [], "provenance": "backfilled" },
              { "n": 2, "outcome": "passed", "startedAt": "2026-09-19T11:31:00Z", "endedAt": "2026-09-19T11:52:45Z", "attemptIds": ["C-billing-state-access:detailedDesign:3"], "episodeId": "ep-bsa-dd-3", "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "designReview", "kind": "review", "title": "Design Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign", "state": "awaitingHuman", "revisions": [
              { "n": 1, "outcome": "sentBack", "endedAt": "2026-09-19T11:30:12Z", "attemptIds": ["C-billing-state-access:designReview:1"], "commentCount": 2, "comments": [
                { "jsonPath": "$.interface.operations[2]", "text": "RecordCharge and RecordRefund are one verb with a sign — fold them." },
                { "jsonPath": "$['$defs'].LedgerEntry.properties.amount", "text": "Minor units, not a float." } ],
                "note": "Two ops are the same verb, and money is a float. Fold the pair and move to minor units, then send it back to me.", "provenance": "backfilled" },
              { "n": 2, "outcome": "awaitingHuman", "startedAt": "2026-09-19T11:53:00Z", "attemptIds": ["C-billing-state-access:designReview:2"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "stp", "kind": "dispatch", "title": "STP", "phase": "test_plan", "dependsOn": ["srsReview"], "state": "running", "revisions": [
              { "n": 1, "outcome": "running", "attemptIds": ["C-billing-state-access:stp:1"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "stpReview", "kind": "review", "title": "STP Review", "phase": "test_plan", "dependsOn": ["stp"], "reviews": "stp", "state": "locked", "revisions": [] },
            { "id": "construction", "kind": "dispatch", "title": "Construction", "phase": "construction", "dependsOn": ["designReview"], "state": "locked", "revisions": [] },
            { "id": "codeReview", "kind": "review", "title": "Code Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction", "state": "locked", "revisions": [] },
            { "id": "integration", "kind": "dispatch", "title": "Integration", "phase": "integration", "dependsOn": ["codeReview"], "state": "locked", "revisions": [] },
            { "id": "testing", "kind": "review", "title": "Testing", "phase": "integration", "dependsOn": ["integration", "stpReview"], "reviews": "integration", "state": "locked", "revisions": [] }
          ],
          "reviewSet": { "reviewers": [ { "role": "architect", "perspective": "architecture", "referenceArtifact": "architecture", "mayAmend": true } ] }
        }
      }
    }
  }
  ```
  (Revision 1 of `detailedDesign` carries two `attemptIds`: the first dispatch failed and was retried before it reached the gate — the sub-attempt case, R1.)

  `done.json` — a finished activity, and the `variant` field:
  ```json
  {
    "route": "/project/archistrator/activity/N-STP",
    "title": "Activity Experience — done activity (system test plan)",
    "note": "N-STP, signed off 2026-09-12. Every task passed at revision 1; every lifecycle phase is completed; no review set, no polling.",
    "ops": {
      "constructionQueryActivityView": {
        "result": {
          "activityId": "N-STP",
          "name": "System test plan (all core use cases)",
          "type": "testing",
          "variant": "plan",
          "state": "done",
          "phases": [
            { "id": "requirements", "label": "Use-Case Trace", "weight": 20, "gateTaskId": "srsReview", "completed": true },
            { "id": "construction", "label": "Plan Authoring", "weight": 45, "gateTaskId": "codeReview", "completed": true },
            { "id": "integration", "label": "Plan Review", "weight": 35, "gateTaskId": "testing", "completed": true }
          ],
          "tasks": [
            { "id": "srs", "kind": "dispatch", "title": "Use-Case Trace", "phase": "requirements", "dependsOn": [], "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "attemptIds": ["N-STP:srs:1"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "srsReview", "kind": "review", "title": "Trace Review", "phase": "requirements", "dependsOn": ["srs"], "reviews": "srs", "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "attemptIds": ["N-STP:srsReview:1"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "construction", "kind": "dispatch", "title": "Plan Authoring", "phase": "construction", "dependsOn": ["srsReview"], "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "attemptIds": ["N-STP:construction:1"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "codeReview", "kind": "review", "title": "Scenario Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction", "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "attemptIds": ["N-STP:codeReview:1"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "integration", "kind": "dispatch", "title": "Plan Assembly", "phase": "integration", "dependsOn": ["codeReview"], "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "attemptIds": ["N-STP:integration:1"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] },
            { "id": "testing", "kind": "review", "title": "Plan Review", "phase": "integration", "dependsOn": ["integration"], "reviews": "integration", "state": "passed", "revisions": [
              { "n": 1, "outcome": "passed", "attemptIds": ["N-STP:testing:1"], "commentCount": 0, "comments": [], "provenance": "backfilled" } ] }
          ]
        }
      }
    }
  }
  ```

  `not-started.json` — the whole lifecycle with nothing on it:
  ```json
  {
    "route": "/project/archistrator/activity/U-SPA-web-client",
    "title": "Activity Experience — not-started activity (web client)",
    "note": "U-SPA-web-client waits on its three managers. The full lifecycle DAG is served with no revisions: only the root task is pending, everything else is locked. No polling.",
    "ops": {
      "constructionQueryActivityView": {
        "result": {
          "activityId": "U-SPA-web-client",
          "name": "Build Web Client (SPA)",
          "type": "frontend",
          "componentId": "web-client",
          "state": "notStarted",
          "phases": [
            { "id": "requirements", "label": "UX Requirements", "weight": 15, "gateTaskId": "srsReview", "completed": false },
            { "id": "detailed_design", "label": "Design", "weight": 25, "gateTaskId": "designReview", "completed": false },
            { "id": "test_plan", "label": "Flows", "weight": 10, "gateTaskId": "stpReview", "completed": false },
            { "id": "construction", "label": "Construction", "weight": 35, "gateTaskId": "codeReview", "completed": false },
            { "id": "integration", "label": "Integration", "weight": 15, "gateTaskId": "testing", "completed": false }
          ],
          "tasks": [
            { "id": "srs", "kind": "dispatch", "title": "UX Requirements", "phase": "requirements", "dependsOn": [], "state": "pending", "revisions": [] },
            { "id": "srsReview", "kind": "review", "title": "UX Requirements Review", "phase": "requirements", "dependsOn": ["srs"], "reviews": "srs", "state": "locked", "revisions": [] },
            { "id": "detailedDesign", "kind": "dispatch", "title": "Design", "phase": "detailed_design", "dependsOn": ["srsReview"], "state": "locked", "revisions": [] },
            { "id": "designReview", "kind": "review", "title": "Design Review", "phase": "detailed_design", "dependsOn": ["detailedDesign"], "reviews": "detailedDesign", "state": "locked", "revisions": [] },
            { "id": "stp", "kind": "dispatch", "title": "Flows", "phase": "test_plan", "dependsOn": ["srsReview"], "state": "locked", "revisions": [] },
            { "id": "stpReview", "kind": "review", "title": "Flow Review", "phase": "test_plan", "dependsOn": ["stp"], "reviews": "stp", "state": "locked", "revisions": [] },
            { "id": "construction", "kind": "dispatch", "title": "Construction", "phase": "construction", "dependsOn": ["designReview"], "state": "locked", "revisions": [] },
            { "id": "codeReview", "kind": "review", "title": "Code Review", "phase": "construction", "dependsOn": ["construction"], "reviews": "construction", "state": "locked", "revisions": [] },
            { "id": "integration", "kind": "dispatch", "title": "Integration", "phase": "integration", "dependsOn": ["codeReview"], "state": "locked", "revisions": [] },
            { "id": "testing", "kind": "review", "title": "Flow Testing", "phase": "integration", "dependsOn": ["integration", "stpReview"], "reviews": "integration", "state": "locked", "revisions": [] }
          ]
        }
      }
    }
  }
  ```
  - [ ] **Verify first:** the `title`/`label`/`dependsOn` values above are transcribed from today's `profileRows` (`projectstateaccess.go` ~L7975–8072) and spec §3's Figure A-1 shape. partAB's `lifecycles.json` is the authority once it lands: diff each fixture's `phases`/`tasks` skeleton against `lifecycles.gen.ts` for `deployment`, `service`, `testing:plan` and `frontend`, and correct the FIXTURE to match the data (ids, titles, weights, `dependsOn`, `reviews`). The four activity `name`s ARE slot 9's committed `title`s as of `cefedfd2` (verified with `jq` over `.slots["9"].model.activities`).

- [ ] **Step 7: Pin the four states in the fixture test** — append to `webApp/scripts/fixture-schema.test.mjs`:

  ```js
  void test('the activity-experience design fixtures are recorded, valid, and answer the one read', () => {
    const { files, errors } = validateFixtureTree(DESIGN_FIXTURES, { validate });
    assert.deepEqual(errors, []);
    const states = files
      .filter((f) => f.includes(join(SURFACE, 'activity-experience')))
      .map((f) => f.slice(f.lastIndexOf('/') + 1))
      .sort();
    assert.deepEqual(states, [
      'deployment-linear.json',
      'done.json',
      'not-started.json',
      'service-fork-sent-back.json',
    ]);
    for (const f of files.filter((p) => p.includes(join(SURFACE, 'activity-experience')))) {
      const view = JSON.parse(readFileSync(f, 'utf8')).ops.constructionQueryActivityView.result;
      const ids = new Set(view.tasks.map((t) => t.id));
      for (const t of view.tasks) {
        for (const dep of t.dependsOn) assert.ok(ids.has(dep), `${f}: ${t.id} depends on unknown ${dep}`);
        if (t.reviews !== undefined) assert.ok(ids.has(t.reviews), `${f}: ${t.id} reviews unknown ${t.reviews}`);
      }
      assert.equal(view.phases.reduce((sum, p) => sum + p.weight, 0), 100, `${f}: weights`);
    }
  });
  ```
  - [ ] **Verify first:** the test file's existing imports. It already imports `existsSync`, `join`, `SURFACE`, `validateFixtureTree`; add `readFileSync` to its `node:fs` import if absent.

- [ ] **Step 8: Prove the schema actually bites.** Temporarily change `"state": "done"` to `"state": "finished"` in `done.json`, run `cd webApp && node --test scripts/fixture-schema.test.mjs`, and confirm a failure naming `web-client/activity-experience/done.json` and the `state` enum. Revert. (A fixture test that cannot fail is the permissive-fake defect again, in JSON.)

- [ ] **Step 9: Gates.**
  ```bash
  cd webApp && npm run check
  npm run build:preview
  ```
  Expected: `check` green (typecheck `tsc -b`, eslint incl. the layer boundaries, prettier over `src/**`, `node --test`). `build:preview` logs `4 fixture state(s) from …/preview/fixtures/web-client` and passes `check-prod-bundle --preview`. The fixtures contain neither bundle marker (`FixtureMissError`, `archistrator-preview-network-guard` — `scripts/bundle-markers.mjs`); keep it that way when editing a `note`.
  ```bash
  cd ../server && make gen-lifecycles-check gen-uiprofiles-check
  ```
  (both untouched by this task; run to show the webApp tree is drift-clean before committing).

- [ ] **Step 10: Commit.**
  ```bash
  git add webApp/src/hooks/activityViewPolling.ts webApp/src/hooks/activityViewPolling.test.ts webApp/src/hooks/useActivityView.ts webApp/src/hooks/useConstructionMutations.ts webApp/preview/fixtures webApp/scripts/fixture-schema.test.mjs
  git commit -m "$(cat <<'EOF'
  feat(webapp): useActivityView, its poll cadence, and four recorded view states

  The hook over constructionManager.QueryActivityView, with a pure cadence rule
  (2s while a task runs, 8s at a gate, stop when done or not started, degrade
  on a transient fault) and four OAS-validated preview fixtures for the stage-5
  Activity Experience: a linear deployment, a service mid-fork after a
  send-back, a done activity and a not-started one.

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  EOF
  )"
  ```

---

## Part C+D self-review

**Part C — `constructionManager.QueryActivityView`**

- [ ] Read-only op added to the EXISTING contract, 12→13 ops, exact JSON + anchor shown (copied from `GetSessionState`'s param shape) → Task 8 Step 3(a)
- [ ] Response `ActivityView` with every field of the brief — `activityId, name, type, variant, componentId, state`; `phases[{id,label,weight,gateTaskId,completed}]`; `tasks[{id,kind,title,phase,dependsOn[],reviews,state,revisions[…]}]`; `revisions[{n,outcome,startedAt,endedAt,attemptIds[],episodeId,commentCount,comments[{jsonPath,text}],note,provenance}]`; `reviewSet` only at a live gate → Task 8 Step 3(b), Step 5 (`liveGate != ""`)
- [ ] Only design-health exposure is the `DH-CONTRACT-OPCOUNT-MAX` Warning; `TestGreenFixtureAdvisoriesFire` needs a comment edit and NO assertion change, with the reason (rule-id-indexed, already firing for `systemDesignManager`) → F5, Task 8 Step 8
- [ ] Lifecycle DAG consumed from `methodassets.LifecycleFor`, interface stated exactly as partAB produces it (incl. `ExitCriterion`, the kind constants, `testing:qaProcess`) → Task 7 Interfaces, Task 8 Interfaces
- [ ] Production `(ActivityType, TestingVariant) → lifecycle type key` helper owned HERE, defined once, exact signature, table test over all 7 types × 5 variants, used by the façade → Task 8 (`lifecycleTypeKey`, `TestLifecycleTypeKey_CoversEveryTypeAndVariant`)
- [ ] Layer placement checked against `TestMethodLayering` with the stepmanifest consumers as precedent (RA-only today; the allowlist is module-wide) and a Verify-first gate → Task 7 "Layer placement"
- [ ] Revision derivation is PURE, with real table tests: revision n = n-th work that reached the gate + n-th gate attempt (R1, R3); failed/retried work = sub-attempts in `attemptIds[]` (R1); conditional tasks fold in (R2); rejected gate closes n as `sentBack`; note matched BY ORDER, tails aligned, rule and its justification stated (R4); dispatch and review share numbers (R3); provenance by `worstOrigin` via `AttemptsWorstOrigin` (R5); `episodeId` via `TargetRef = AttemptID` (N1/N2, R6) → Task 7
- [ ] The brief's premise corrected where the code disagrees: the live workflow never writes `Attempts`; `normalizeAttempts` (N1–N4) reconstructs from episodes, send-back notes, stored completions and the live session, stamped `backfilled` with a basis → F1, Task 7
- [ ] Task state table incl. `locked`, `pending`, `running`, `awaitingHuman` (live session at this gate), `passed`, `sentBack` (latest gate rejected, no newer work), `failed` — plus the evidence-beats-structure ordering → Task 7 table + `TestDeriveTaskViews_StatesAndRevisions`
- [ ] requirements/architecture/projectDesign out of scope → `NotFound`, the idiom `GetSessionState` uses for an activity it has no session for → Task 8 error table + `TestQueryActivityView_UnknownActivityIsNotFound`
- [ ] Every generator and every drift target named: `gen-models` (+fakes), `gen-client`, `gen-temporal` (+sdk/config/main), NOT `gen-internal-tools`/`gen-uiprofiles` for the Manager op; all ten `*-check` targets incl. `gen-lifecycles-check` and `derived-plan-check` → Task 8 Steps 4, 7, 9
- [ ] Hand code: façade mirrors `GetSessionState` (Query, only while Running) + `ListEpisodesForActivity` (plain RA read); error mapping table; paramguard branches on both required strings; registered-names golden untouched and why; REST+MCP via `exposedManagers` → Task 8 header, F6, F7
- [ ] Self-amendment gate loop verbatim: `make gen-models` → `make method-check` → `validate --root .. --slot System` → `GOWORK=off make test-short` → `cd webApp && npm run check` → Task 6 Step 11, Task 8 Step 9
- [ ] webApp `npm run gen:api && npm run gen:ops`; `useActivityView(projectId, activityId)` on `useConstructionSession`'s conventions (key factory, exported query options, pure cadence file); 2s running / 8s awaitingHuman / stop done+notStarted; `node:test` for the pure function → Task 8 Step 7, Task 9 Steps 1–4
- [ ] Preview fixtures: location, file shape and OpId keying explained from `fixture-schema.mjs` + `fixtureOps.ts`; the four states recorded as full JSON; the screen-does-not-exist-until-stage-5 question answered (the format allows it — recorded now, route renders in stage 5); a test that pins them and a mutation check that the schema bites → Task 9 header, Steps 6–8

**Part D — reviewEngine `artifactKind` defect**

- [ ] Root cause fixed, not patched: the kind is a generated typed enum on the engine's contract, so `phase.String()` no longer compiles → Task 6 Steps 3–5
- [ ] Total, exhaustive (ActivityType, ActivityMethodPhase) → kind mapping, all 35 cells decided and each row justified from `reviewersFor`'s semantics; typed on both sides; `exhaustive` covers every switch (no string matching) → Task 6 table + Step 7
- [ ] Placement decided against the repo's gates, with the deviation from "next to the engine's vocabulary" stated and bounded (vocabulary in the engine contract; table in the Manager until stage 2) → F3, Task 6 Step 7 doc comment
- [ ] Second latent cause found and fixed: the engine's blanket `componentID` precondition silenced `N-STP`/`N-IT` → F2, Task 6 Step 5 (`componentScoped`), Step 6 test
- [ ] Error surfaced, decision justified, replay-safe: logged + `reviewSetError` on the session view; no `GetVersion` because nothing is added to history (human-stage precedent), with `noteDelivery` named as the precedent for the other case; replay fixtures must replay unchanged → Task 6 "Surfacing decision", Steps 7, 9
- [ ] Manager's fake review engine VALIDATES `artifactKind` — by delegating to the real engine — so the class cannot hide again → Task 6 Step 8
- [ ] Regression test that FAILS on today's code (real engine wired at a gated phase; expected failure text given) → Task 6 Steps 1–2
- [ ] Test asserting `ConstructionSessionView.ReviewSet` is non-empty at a gated phase → the same test, plus the totality test over every dispatchable (type, variant, lifecycle phase, ±component) → Task 6 Steps 1, 8
- [ ] `lifecycles.json`'s task `artifactKind` explicitly fenced off until stage 2 → Task 6 "Do not wire…"

**Cross-cutting**

- [ ] No new `.go` file; one test file per package (`TestFileLayout`) → F4
- [ ] No new exported hand-written symbol in an Engine/RA/Manager package (`TestGeneratedOnlyPublic`) — everything added is unexported or generated
- [ ] No bare top-level `phase`/`Phase` identifier (`TestNoBannedPhaseIdentifier`); the wire property `phase` maps to Go `LifecyclePhaseID`
- [ ] Every commit message ends with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`
- [ ] Every place the plan could not be settled by reading code carries a `Verify first` step: generated Go names/pointer-ness (T6 S4, T8 S4), Manager→method-assets import under `arch.Check` (T7), lint's view of test-only callers (T7 S4), whether scoped `validate` prints contract findings (T8 S8), `OpResult` resolving (T9 S4), invalidation sites (T9 S5), fixture skeletons vs partAB's data (T9 S6), test imports (T9 S7)
