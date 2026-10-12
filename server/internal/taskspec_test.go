package internal_test

import (
	"encoding/json"
	"maps"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/testgen"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// The agent task spec (projectstate.TaskSpecFor) over archistrator's own committed
// project.json. These tests live here, not beside the resolver, because a component
// package may hold only its generated scenarios file and its hooks file
// (TestFileLayout's scenario-tests-only rule).

func TestTaskSpecForServiceIntegration(t *testing.T) {
	p := ownProject(t)
	s, err := projectstate.TaskSpecFor(p, "C-agentic-job-access", "integration")
	if err != nil {
		t.Fatal(err)
	}
	if s.Identity.Command != "service-integration" || s.Model.ID == "" || s.Prompt.Text == "" {
		t.Fatalf("identity/model/prompt: %+v", s.Identity)
	}
	if s.EditScope.StateOnly || len(s.EditScope.Components) != 1 ||
		!slices.Contains(s.EditScope.Components[0].Paths, "server/internal/resourceaccess/agenticjob/**") {
		t.Fatalf("edit scope: %+v", s.EditScope)
	}
	// Review focus 1: a legitimate wiring change touches the composition root and the
	// module files, so integration's scope must admit them.
	for _, want := range []string{"server/cmd/server/hooks.go", "server/go.mod", "server/go.sum"} {
		if !slices.Contains(s.EditScope.Extra, want) {
			t.Fatalf("integration extras %v lack %s", s.EditScope.Extra, want)
		}
	}
	scen := scenarioCheckCount(t, s, "scenario:agenticJobAccess:")
	want, _ := projectstate.ComponentScenarios(p, "agenticJobAccess")
	if scen != len(want) || scen == 0 {
		t.Fatalf("scenario checks %d, projected %d", scen, len(want))
	}
	for _, id := range []string{"command:gofmt", "command:build", "command:vet", "command:lint", "command:arch", "command:gen-tests-check"} {
		if !hasCheck(s, id) {
			t.Errorf("integration lacks %s: %v", id, checkIDs(s))
		}
	}
	if !slices.IsSortedFunc(s.Checks, func(a, b projectstate.SpecCheck) int {
		return strings.Compare(checkSortKey(a), checkSortKey(b))
	}) {
		t.Errorf("checks not sorted by kind then id: %v", checkIDs(s))
	}
}

// scenarioCheckCount counts the spec's scenario checks, each of which must carry the
// contract's id prefix and the 900s scenario cap.
func scenarioCheckCount(t *testing.T, s projectstate.TaskSpec, idPrefix string) int {
	t.Helper()
	n := 0
	for _, c := range s.Checks {
		if c.Kind != projectstate.CheckKindScenario {
			continue
		}
		n++
		if !strings.HasPrefix(c.ID, idPrefix) {
			t.Errorf("scenario id %q", c.ID)
		}
		if c.TimeoutSec != 900 {
			t.Errorf("scenario %s timeout %d, want 900", c.ID, c.TimeoutSec)
		}
	}
	return n
}

func TestTaskSpecForServiceConstructionCommands(t *testing.T) {
	s, err := projectstate.TaskSpecFor(ownProject(t), "C-agentic-job-access", "construction")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(checkIDs(s), []string{"command:arch", "command:build", "command:gofmt", "command:lint", "command:vet"}) {
		t.Fatalf("construction checks %v", checkIDs(s))
	}
	build := checkByID(t, s, "command:build")
	if !slices.Equal(build.Source.Argv, []string{"go", "build", "./internal/resourceaccess/agenticjob/..."}) ||
		build.Source.Dir != "server" || build.TimeoutSec != 300 {
		t.Fatalf("build check %+v", build)
	}
	if lint := checkByID(t, s, "command:lint"); lint.TimeoutSec != 600 {
		t.Fatalf("lint timeout %d", lint.TimeoutSec)
	}
	if slices.Contains(s.EditScope.Extra, "server/cmd/server/hooks.go") ||
		!slices.Contains(s.EditScope.Extra, "server/go.mod") {
		t.Fatalf("construction extras %v", s.EditScope.Extra)
	}
	if s.Model.Estimate.TokensIn == 0 || s.Model.Estimate.USD == 0 {
		t.Fatalf("estimate %+v", s.Model.Estimate)
	}
}

func TestTaskSpecForDesignDraftIsStateOnlyWithSlotRules(t *testing.T) {
	p := ownProject(t)
	act := firstActivityOfType(t, p, "requirements") // the design prefix activity
	s, err := projectstate.TaskSpecFor(p, act, "glossaryDraft")
	if err != nil {
		t.Fatal(err)
	}
	if !s.EditScope.StateOnly || len(s.Checks) != 1 || s.Checks[0].ID != "rule:glossary" ||
		!slices.Equal(s.Checks[0].Source.RulePrefixes, []string{"GLOSS-"}) {
		t.Fatalf("design spec: %+v %+v", s.EditScope, s.Checks)
	}
	if s.Checks[0].Source.RuleSlot != "glossary" || s.Checks[0].TimeoutSec != 120 {
		t.Fatalf("rule check source: %+v", s.Checks[0])
	}
	if strings.Contains(s.Prompt.Text, "$ARGUMENTS") {
		t.Fatal("design prompt left $ARGUMENTS unsubstituted")
	}
}

func TestTaskSpecForStateTaskAndTestPlan(t *testing.T) {
	p := ownProject(t)
	srs, err := projectstate.TaskSpecFor(p, "C-agentic-job-access", "srs")
	if err != nil {
		t.Fatal(err)
	}
	if !srs.EditScope.StateOnly || !slices.Equal(checkIDs(srs), []string{"rule:project"}) {
		t.Fatalf("srs spec: %+v %v", srs.EditScope, checkIDs(srs))
	}
	if !strings.Contains(srs.Prompt.Text, "agentic-job-access C-agentic-job-access") {
		t.Fatal("construction-style prompt does not carry <component> <activity>")
	}
	stp, err := projectstate.TaskSpecFor(p, "C-agentic-job-access", "stp")
	if err != nil {
		t.Fatal(err)
	}
	if !stp.EditScope.StateOnly || !slices.Equal(checkIDs(stp), []string{"rule:tp"}) ||
		!slices.Equal(stp.Checks[0].Source.RulePrefixes, []string{"TP-"}) {
		t.Fatalf("stp spec: %+v %+v", stp.EditScope, stp.Checks)
	}
}

func TestTaskSpecForFrontendIntegration(t *testing.T) {
	s, err := projectstate.TaskSpecFor(ownProject(t), "U-SPA-web-client", "integration")
	if err != nil {
		t.Fatal(err)
	}
	if s.EditScope.StateOnly || len(s.EditScope.Components) != 1 ||
		!slices.Equal(s.EditScope.Components[0].Paths, []string{"webApp/src/**"}) {
		t.Fatalf("frontend scope: %+v", s.EditScope)
	}
	for _, want := range []string{"webApp/package.json", "webApp/package-lock.json", "uitests/generated/webClient/**", "uitests/**"} {
		if !slices.Contains(s.EditScope.Extra, want) {
			t.Errorf("frontend integration extras %v lack %s", s.EditScope.Extra, want)
		}
	}
	for _, id := range []string{"command:typecheck", "command:lint", "command:gen-tests-check"} {
		if !hasCheck(s, id) {
			t.Errorf("frontend integration lacks %s: %v", id, checkIDs(s))
		}
	}
}

// R19: a frontend contract with a Go package (the MCP-client case) puts that package in
// the integration scope; construction may touch the package manifests.
func TestTaskSpecFrontendScopeCarriesContractPackagesAndManifests(t *testing.T) {
	p := ownProject(t)
	cons, err := projectstate.TaskSpecFor(p, "U-SPA-web-client", "construction")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cons.EditScope.Extra, "webApp/package.json") || !slices.Contains(cons.EditScope.Extra, "webApp/package-lock.json") {
		t.Fatalf("frontend construction extras %v", cons.EditScope.Extra)
	}
	contracts := make(map[string]projectstate.ServiceContract, len(p.ServiceContracts)+1)
	maps.Copy(contracts, p.ServiceContracts)
	contracts["webClientMcp"] = projectstate.ServiceContract{Component: "webClient", GoPackage: "internal/client/mcp/webclient"}
	p.ServiceContracts = contracts
	integ, err := projectstate.TaskSpecFor(p, "U-SPA-web-client", "integration")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(integ.EditScope.Components[0].Paths, "server/internal/client/mcp/webclient/**") ||
		!slices.Contains(integ.EditScope.Components[0].Paths, "webApp/src/**") {
		t.Fatalf("frontend integration paths %v", integ.EditScope.Components[0].Paths)
	}
	if !slices.Contains(integ.EditScope.Extra, "uitests/generated/webClientMcp/**") {
		t.Fatalf("frontend integration extras %v", integ.EditScope.Extra)
	}
}

// R18: a construct-mode task whose scope cannot be derived gets an explicit "any path",
// never a stateOnly scope that would reject its legitimate output.
func TestTaskSpecUnderivableScopeIsAnyPath(t *testing.T) {
	s, err := projectstate.TaskSpecFor(ownProject(t), "R-github", "construction")
	if err != nil {
		t.Fatal(err)
	}
	if s.EditScope.StateOnly || !slices.Equal(s.EditScope.Extra, []string{"**"}) {
		t.Fatalf("R-github construction scope %+v", s.EditScope)
	}
}

// R20: every commanded task (dispatches and agentic critiques) shares its phase's weight,
// so for a lifecycle whose every phase has a commanded task the task estimates add up to
// the whole activity: Σ tokensIn / (class throughput) = effort days.
func TestTaskSpecCommandedSharesSumToTheActivity(t *testing.T) {
	p := ownProject(t)
	pa := p.PlanningAssumptions.Model.(*projectstate.PlanningAssumptions)
	list := p.ActivityList.Model.(*projectstate.ActivityList)
	days := map[string]float64{}
	for _, it := range list.Activities {
		days[it.Name] = it.EffortDays
	}
	checked := 0
	for _, a := range allActivities(t, p) {
		l, _ := methodassets.LifecycleFor(projectstate.LifecycleKeyFor(a.typ, a.variant))
		if !everyPhaseCommanded(l) {
			continue
		}
		var agentDays float64
		for _, task := range l.Tasks {
			if task.Command == "" {
				continue
			}
			s, err := projectstate.TaskSpecFor(p, a.id, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			rate, ok := pa.RateCard[s.Identity.WorkerClass]
			if !ok {
				rate = projectstate.DefaultRateSpec(s.Identity.WorkerClass)
			}
			agentDays += float64(s.Model.Estimate.TokensIn) / (rate.MegatokensInPerDay * 1e6)
		}
		if math.Abs(agentDays-days[a.id]) > 1e-3 {
			t.Errorf("%s: commanded tasks estimate %.4f agent-days, activity is %.4f", a.id, agentDays, days[a.id])
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no activity whose every phase has a commanded task")
	}
}

func everyPhaseCommanded(l methodassets.Lifecycle) bool {
	for _, ph := range l.Phases {
		if !slices.ContainsFunc(l.Tasks, func(t methodassets.LifecycleTask) bool { return t.Phase == ph.ID && t.Command != "" }) {
			return false
		}
	}
	return true
}

func TestTaskSpecForHumanReviewHasNoPromptOrChecks(t *testing.T) {
	s, err := projectstate.TaskSpecFor(ownProject(t), "C-agentic-job-access", "codeReview")
	if err != nil {
		t.Fatal(err)
	}
	if s.Prompt.Text != "" || len(s.Checks) != 0 || len(s.Tools.MCP) != 0 || !s.EditScope.StateOnly {
		t.Fatalf("review spec: %+v", s)
	}
}

func TestTaskSpecSkillsGrantTheSkillTool(t *testing.T) {
	s, err := projectstate.TaskSpecFor(ownProject(t), "C-agentic-job-access", "construction")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Prompt.Skills) == 0 {
		t.Skip("service-construction seats no skills")
	}
	if !slices.Contains(s.Tools.Builtin, "Skill") || !slices.IsSorted(s.Tools.Builtin) {
		t.Fatalf("builtin tools %v must hold Skill, sorted", s.Tools.Builtin)
	}
	if len(slices.Compact(slices.Clone(s.Tools.Builtin))) != len(s.Tools.Builtin) {
		t.Fatalf("builtin tools %v carry a duplicate", s.Tools.Builtin)
	}
}

func TestTaskSpecHashIsStableAndSensitive(t *testing.T) {
	p := ownProject(t)
	a, _ := projectstate.TaskSpecFor(p, "C-agentic-job-access", "construction")
	b, _ := projectstate.TaskSpecFor(p, "C-agentic-job-access", "construction")
	if a.Hash == "" || a.Hash != b.Hash || a.ComputeHash() != a.Hash {
		t.Fatal("hash unstable")
	}
	b.Model.ID = "haiku"
	if b.ComputeHash() == a.Hash {
		t.Fatal("hash ignores the model")
	}
}

// R13: the server and the job compute the hash from the same project.json, so it must
// survive the project's own encode/decode round trip and the spec's own JSON round trip.
func TestTaskSpecHashSurvivesJSONRoundTrips(t *testing.T) {
	p := ownProject(t)
	enc, err := projectstate.EncodeProjectJSON(*p)
	if err != nil {
		t.Fatal(err)
	}
	q, ok, err := projectstate.DecodeProjectJSON(enc, "")
	if err != nil || !ok {
		t.Fatalf("re-decode: ok=%v err=%v", ok, err)
	}
	for _, task := range []string{"glossaryDraft", "construction", "integration", "codeReview"} {
		act := "C-agentic-job-access"
		if task == "glossaryDraft" {
			act = firstActivityOfType(t, p, "requirements")
		}
		a, err := projectstate.TaskSpecFor(p, act, task)
		if err != nil {
			t.Fatal(err)
		}
		b, err := projectstate.TaskSpecFor(&q, act, task)
		if err != nil {
			t.Fatal(err)
		}
		if a.Hash != b.Hash {
			t.Errorf("%s: hash changed across the project round trip", task)
		}
		raw, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		var back projectstate.TaskSpec
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		if back.ComputeHash() != a.Hash {
			t.Errorf("%s: hash changed across the spec's JSON round trip", task)
		}
	}
}

func TestTaskSpecModelOverride(t *testing.T) {
	p := ownProject(t)
	setRateCardModel(t, p, "junior-developer", "haiku") // mutate the decoded PlanningAssumptions model
	s, _ := projectstate.TaskSpecFor(p, "C-agentic-job-access", "construction")
	if s.Model.ID != "haiku" || s.Model.Source != "projectOverride" {
		t.Fatalf("model = %+v", s.Model)
	}
}

func TestTaskSpecModelFallsBackToTheCharter(t *testing.T) {
	p := ownProject(t)
	setRateCardModel(t, p, "junior-developer", "")
	s, _ := projectstate.TaskSpecFor(p, "C-agentic-job-access", "construction")
	want, _ := methodassets.ModelForAgent("junior-developer")
	if s.Model.ID != want || s.Model.Source != "charter" {
		t.Fatalf("model = %+v, charter %q", s.Model, want)
	}
}

func TestTaskSpecForEveryActivityTask(t *testing.T) {
	p := ownProject(t)
	acts := allActivities(t, p)
	if len(acts) == 0 {
		t.Fatal("no committed activities to resolve")
	}
	for _, a := range acts {
		l, ok := methodassets.LifecycleFor(projectstate.LifecycleKeyFor(a.typ, a.variant))
		if !ok {
			t.Fatalf("%s: no lifecycle", a.id)
		}
		for _, task := range l.Tasks {
			if _, err := projectstate.TaskSpecFor(p, a.id, task.ID); err != nil {
				t.Errorf("%s/%s: %v", a.id, task.ID, err)
			}
		}
	}
}

// R52 (spec §5.2 step 5): an integration job's pre-step runs `make gen-tests`, which
// rewrites EVERY component's generated scenario tests (and seeds missing hooks files), and
// its gen-tests-check gate covers them all. So every path the real testgen produces for
// this repo's committed plans must lie inside every integration task's edit scope — this
// is the drift gate on projectstate's copy of testgen's output layout.
func TestIntegrationScopeAdmitsEveryGeneratedTestPath(t *testing.T) {
	_, raw := ownProjectJSON(t)
	p := ownProject(t)
	// archistrator's module is server/, and server/Makefile.scenarios.mk runs testgen with
	// its defaults from there: output paths are server-relative, Playwright under
	// ../uitests/generated.
	if p.ModuleDir != "server" {
		t.Fatalf("moduleDir %q: this test assumes archistrator's server/ module", p.ModuleDir)
	}
	generated := testgenOutputPaths(t, raw, p.ModuleDir)
	if !slices.ContainsFunc(generated, func(g string) bool { return strings.HasSuffix(g, "_scenarios.gen_test.go") }) ||
		!slices.ContainsFunc(generated, func(g string) bool { return strings.HasPrefix(g, "uitests/generated/") }) {
		t.Fatalf("testgen produced no Go and/or Playwright output for this repo: %v", generated)
	}
	integrations := 0
	for _, a := range allActivities(t, p) {
		s, err := projectstate.TaskSpecFor(p, a.id, "integration")
		if err != nil || (s.Identity.Command != "service-integration" && s.Identity.Command != "frontend-integration") {
			continue
		}
		integrations++
		for _, g := range generated {
			// Hand-filled hooks files are admitted only for the component's own contracts
			// (R61), asserted below for C-agentic-job-access.
			if strings.HasSuffix(g, "_hooks_test.go") || strings.HasSuffix(g, "/hooks.ts") {
				continue
			}
			if !scopeAdmits(s.EditScope, g) {
				t.Errorf("%s integration (%s): generated path %s lies outside its edit scope", a.id, s.Identity.Command, g)
			}
		}
	}
	if integrations == 0 {
		t.Fatal("no service or frontend integration task to check")
	}
	// The derivation is not a blanket: another component's hand-written code stays out.
	s, err := projectstate.TaskSpecFor(p, "C-agentic-job-access", "integration")
	if err != nil {
		t.Fatal(err)
	}
	if scopeAdmits(s.EditScope, "server/internal/manager/delivery/deliverymanager.go") {
		t.Error("agentic-job-access integration admits the delivery manager's hand-written code")
	}
	// R61: another component's once-seeded, hand-filled hooks stay out, the component's own
	// seeded hooks are in, and a web client's hooks.ts is not repo-wide admitted.
	for _, other := range []string{
		"server/internal/manager/delivery/manager_hooks_test.go",
		"uitests/generated/webClient/hooks.ts",
	} {
		if scopeAdmits(s.EditScope, other) {
			t.Errorf("agentic-job-access integration admits %s, another component's hand-written hooks", other)
		}
	}
	ownHooks := "server/internal/resourceaccess/agenticjob/access_hooks_test.go"
	if !slices.Contains(generated, ownHooks) {
		t.Fatalf("testgen no longer seeds %s", ownHooks)
	}
	if !scopeAdmits(s.EditScope, ownHooks) {
		t.Errorf("agentic-job-access integration does not admit its own seeded hooks file %s", ownHooks)
	}
}

// testgenOutputPaths is every path the real testgen writes for raw, repo-relative: the
// generated files and the seeded hooks files, sorted.
func testgenOutputPaths(t *testing.T, raw []byte, moduleDir string) []string {
	t.Helper()
	out, err := testgen.Generate(raw, testgen.Config{
		ModulePath: "github.com/mixofreality-studio/archistrator/server",
		UITestsDir: "../uitests/generated",
	})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, m := range []map[string][]byte{out.Generated, out.HooksOnce} {
		for k := range m {
			paths = append(paths, path.Join(moduleDir, k))
		}
	}
	slices.Sort(paths)
	return paths
}

// scopeAdmits matches p against the edit-scope grammar ("**", "<dir>/**", literal) the
// way the edit-scope gate does, over the spec's component paths and extras.
func scopeAdmits(scope projectstate.SpecEditScope, p string) bool {
	globs := slices.Clone(scope.Extra)
	for _, c := range scope.Components {
		globs = append(globs, c.Paths...)
	}
	return slices.ContainsFunc(globs, func(g string) bool { return projectstate.EditScopeGlobMatch(g, p) })
}

func TestTaskSpecForUnknownActivityOrTask(t *testing.T) {
	p := ownProject(t)
	if _, err := projectstate.TaskSpecFor(p, "no-such-activity", "construction"); err == nil {
		t.Error("an unknown activity resolved")
	}
	if _, err := projectstate.TaskSpecFor(p, "C-agentic-job-access", "no-such-task"); err == nil {
		t.Error("an unknown task resolved")
	}
}

func TestDefaultModelFollowsTheCharter(t *testing.T) {
	for _, class := range []string{"system-architect", "junior-developer", "project-manager"} {
		want, _ := methodassets.ModelForAgent(class)
		if got := projectstate.DefaultRateSpec(class).ModelID; got != want {
			t.Errorf("%s: %q, charter %q", class, got, want)
		}
	}
}

// ---- helpers ----

func ownProject(t *testing.T) *projectstate.Project {
	t.Helper()
	path, raw := ownProjectJSON(t)
	p, ok, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil || !ok {
		t.Fatalf("decode %s: ok=%v err=%v", path, ok, err)
	}
	return &p
}

// ownProjectJSON finds archistrator's committed project.json above the test directory
// and returns its path and bytes.
func ownProjectJSON(t *testing.T) (string, []byte) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		path := filepath.Join(dir, ".aiarch", "state", "project.json")
		if raw, err := os.ReadFile(path); err == nil {
			return path, raw
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no .aiarch/state/project.json above the test directory")
		}
		dir = parent
	}
}

type committedActivity struct {
	id      string
	typ     projectstate.ActivityType
	variant projectstate.TestingVariant
}

// allActivities classifies every committed activity-list item the way QueryActivityView
// does (ResolveConstructionRow over the plan's item).
func allActivities(t *testing.T, p *projectstate.Project) []committedActivity {
	t.Helper()
	list, ok := p.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || list == nil {
		t.Fatal("the committed activity list did not decode")
	}
	var out []committedActivity
	for _, it := range list.Activities {
		row := p.ActivityExecution[it.Name]
		row.ActivityID = it.Name
		typ, variant, _, classified := projectstate.ResolveConstructionRow(row, it)
		if !classified {
			t.Fatalf("activity %s does not classify", it.Name)
		}
		out = append(out, committedActivity{id: it.Name, typ: typ, variant: variant})
	}
	return out
}

func firstActivityOfType(t *testing.T, p *projectstate.Project, typ string) string {
	t.Helper()
	for _, a := range allActivities(t, p) {
		if a.typ.String() == typ {
			return a.id
		}
	}
	t.Fatalf("no committed activity of type %s", typ)
	return ""
}

func setRateCardModel(t *testing.T, p *projectstate.Project, class, model string) {
	t.Helper()
	pa, ok := p.PlanningAssumptions.Model.(*projectstate.PlanningAssumptions)
	if !ok || pa == nil {
		t.Fatal("the committed planning assumptions did not decode")
	}
	card := make(map[string]projectstate.WorkerRateSpec, len(pa.RateCard)+1)
	maps.Copy(card, pa.RateCard)
	spec := card[class]
	spec.ModelID = model
	card[class] = spec
	cp := *pa
	cp.RateCard = card
	p.PlanningAssumptions.Model = &cp
}

func checkIDs(s projectstate.TaskSpec) []string {
	out := make([]string, len(s.Checks))
	for i, c := range s.Checks {
		out[i] = c.ID
	}
	slices.Sort(out)
	return out
}

func hasCheck(s projectstate.TaskSpec, id string) bool {
	return slices.ContainsFunc(s.Checks, func(c projectstate.SpecCheck) bool { return c.ID == id })
}

func checkByID(t *testing.T, s projectstate.TaskSpec, id string) projectstate.SpecCheck {
	t.Helper()
	for _, c := range s.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check %s in %v", id, checkIDs(s))
	return projectstate.SpecCheck{}
}

func checkSortKey(c projectstate.SpecCheck) string {
	order := map[string]string{projectstate.CheckKindRule: "0", projectstate.CheckKindCommand: "1", projectstate.CheckKindScenario: "2"}
	return order[c.Kind] + c.ID
}

// TestEditScopeGlobGrammar pins the one glob grammar TaskSpecFor emits and the
// edit-scope gate matches (R25): "**", "<dir>/**", or a literal repo-relative path.
func TestEditScopeGlobGrammar(t *testing.T) {
	for _, g := range []string{"**", "webApp/src/**", "server/go.mod", "server/cmd/server/hooks.go"} {
		if !projectstate.ValidEditScopeGlob(g) {
			t.Errorf("%q must be in the grammar", g)
		}
	}
	for _, g := range []string{"", "server/*.go", "server/**/x.go", "a?/**", "a[b]/**", "/abs/**", "dir/", "../x/**", "**/x"} {
		if projectstate.ValidEditScopeGlob(g) {
			t.Errorf("%q must be outside the grammar", g)
		}
	}
}

// TestTaskSpecForRefusesAGlobOutsideTheGrammar: a derived scope glob the gate could not
// match exactly (here, a contract's goPackage carrying a wildcard) fails the resolve.
func TestTaskSpecForRefusesAGlobOutsideTheGrammar(t *testing.T) {
	p := ownProject(t)
	sc := p.ServiceContracts["agenticJobAccess"]
	if sc.GoPackage == "" {
		t.Fatal("fixture: agenticJobAccess has no goPackage")
	}
	sc.GoPackage = "internal/resourceaccess/agentic*"
	p.ServiceContracts["agenticJobAccess"] = sc
	if _, err := projectstate.TaskSpecFor(p, "C-agentic-job-access", "construction"); err == nil || !strings.Contains(err.Error(), "edit-scope glob") {
		t.Fatalf("want an edit-scope glob error, got %v", err)
	}
}
