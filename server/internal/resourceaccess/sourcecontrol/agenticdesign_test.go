package sourcecontrol

// agenticdesign_test.go — structural tests over the embedded DESIGN workflow asset
// (agenticdesign.go). It is an INTERNAL test package (package sourcecontrol) so it
// can read the unexported designWorkflowTmplText embed var + renderDesignWorkflow; the
// component's external service tests live in sourcecontrol_test.go (package
// sourcecontrol_test). Both test packages coexisting in one directory is permitted by
// go test.
//
// The workflow asset is now a Go text/template (custom [[ ]] delimiters) rendered with
// the GitHub App slug, so the structural tests assert against the RENDERED bytes
// (renderDesignWorkflow) rather than the raw template (which is not valid YAML on its
// own — the [[ if ]] control line is not YAML).
//
// These assert the asset WIRING (the contract anchors), not a live Actions run.
// The yaml.v3 + framework-go-infrastructure-github imports here are TEST-ONLY, so
// the Method layering checker (loaded with Tests:false) never scans them.

import (
	"bytes"
	"strings"
	"testing"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"gopkg.in/yaml.v3"
)

// testAppSlug is a representative configured App slug the structural tests render the
// workflow with.
const testAppSlug = "archistrator-bot"

// renderedDesignWorkflow renders the embedded template with the given slug or fails the
// test. Most structural assertions are slug-independent, so they use testAppSlug.
func renderedDesignWorkflow(t *testing.T, appSlug string) []byte {
	t.Helper()
	b, err := renderDesignWorkflow(appSlug)
	if err != nil {
		t.Fatalf("renderDesignWorkflow(%q): %v", appSlug, err)
	}
	return b
}

// expectedDispatchInputs is the CONTRACT between this template and the design
// Managers (C-MSD-Δ / C-MPD-Δ DispatchInputs on PipelineSpec). idempotency_token
// is the load-bearing dispatch anchor shared with the construction workflow; the
// other four are the additive DESIGN-job parameters.
var expectedDispatchInputs = []string{
	"idempotency_token",
	"artifact_kind",
	"design_prompt",
	"target_branch",
	"prior_state_ref",
}

// requiredDispatchInputs are the inputs that MUST be required:true. prior_state_ref
// is intentionally optional (empty on the first artifact of a fresh project).
var requiredDispatchInputs = []string{
	"idempotency_token",
	"artifact_kind",
	"design_prompt",
	"target_branch",
}

// workflowDoc is a minimal structural view of the workflow_dispatch surface we
// assert on — we are testing the asset wiring, not running Actions.
type workflowDoc struct {
	Name    string `yaml:"name"`
	RunName string `yaml:"run-name"`
	On      struct {
		WorkflowDispatch struct {
			Inputs map[string]struct {
				Description string `yaml:"description"`
				Required    bool   `yaml:"required"`
				Type        string `yaml:"type"`
			} `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
}

func TestEmbeddedTemplateNonEmpty(t *testing.T) {
	if len(designWorkflowTmplText) == 0 {
		t.Fatal("embedded aiarch-design.yml.tmpl is empty")
	}
}

func TestEmbeddedTemplateParsesAsYAML(t *testing.T) {
	var doc workflowDoc
	if err := yaml.Unmarshal(renderedDesignWorkflow(t, testAppSlug), &doc); err != nil {
		t.Fatalf("rendered template does not parse as YAML: %v", err)
	}
	if doc.Name == "" {
		t.Error("workflow has no top-level name")
	}
}

func TestDeclaresExpectedDispatchInputs(t *testing.T) {
	var doc workflowDoc
	if err := yaml.Unmarshal(renderedDesignWorkflow(t, testAppSlug), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	inputs := doc.On.WorkflowDispatch.Inputs
	if inputs == nil {
		t.Fatal("workflow declares no workflow_dispatch inputs")
	}
	for _, name := range expectedDispatchInputs {
		if _, ok := inputs[name]; !ok {
			t.Errorf("missing expected workflow_dispatch input %q", name)
		}
	}
	for _, name := range requiredDispatchInputs {
		in, ok := inputs[name]
		if !ok {
			continue // already reported above
		}
		if !in.Required {
			t.Errorf("input %q must be required:true", name)
		}
	}
	// prior_state_ref is the one intentionally-optional input.
	if in, ok := inputs["prior_state_ref"]; ok && in.Required {
		t.Error("prior_state_ref must be optional (required:false) — empty on a fresh project")
	}
}

func TestIdempotencyAnchorMatchesDispatchConstants(t *testing.T) {
	var doc workflowDoc
	if err := yaml.Unmarshal(renderedDesignWorkflow(t, testAppSlug), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The load-bearing input name MUST equal the satellite constant the
	// constructionPipelineAccess RA fills, or dispatch/observe/cancel break.
	if _, ok := doc.On.WorkflowDispatch.Inputs[fwgithub.DispatchInputKeyIdempotency]; !ok {
		t.Errorf("workflow must declare the %q input (DispatchInputKeyIdempotency)",
			fwgithub.DispatchInputKeyIdempotency)
	}
	// run-name MUST carry the RunNamePrefix so ListRunsByName can resolve runs.
	if !strings.HasPrefix(doc.RunName, fwgithub.RunNamePrefix) {
		t.Errorf("run-name %q must start with RunNamePrefix %q", doc.RunName, fwgithub.RunNamePrefix)
	}
	if !strings.Contains(doc.RunName, "${{ inputs."+fwgithub.DispatchInputKeyIdempotency+" }}") {
		t.Errorf("run-name %q must stamp the idempotency_token input", doc.RunName)
	}
}

func TestReferencesGoTestGateAndStatePath(t *testing.T) {
	body := string(renderedDesignWorkflow(t, testAppSlug))

	// The required check is now `go test ./...` (the seated aiarch_method_test.go →
	// methodcheck.Check), NOT a pinned aiarch-validate container. The container/CLI
	// must be fully gone.
	if !strings.Contains(body, "go test ./...") {
		t.Error("workflow's required check must run `go test ./...` (the seated methodcheck gate)")
	}
	if !strings.Contains(body, "actions/setup-go") {
		t.Error("workflow must set up Go before running the go-test gate")
	}
	if strings.Contains(body, "aiarch-validate") {
		t.Error("workflow must no longer reference the removed aiarch-validate CLI/container")
	}

	// Commits / validates under the .aiarch/state/ tree that methodcheck.Check and
	// projectStateAccess read.
	if !strings.Contains(body, ".aiarch/state/") {
		t.Error("workflow must commit/validate under the .aiarch/state/ tree")
	}

	// References claude-code-action authenticated by the named secret only (never
	// an inlined token value).
	if !strings.Contains(body, "claude-code-action") {
		t.Error("workflow must run claude-code-action")
	}
	if !strings.Contains(body, "secrets.CLAUDE_CODE_OAUTH_TOKEN") {
		t.Error("workflow must reference CLAUDE_CODE_OAUTH_TOKEN by secret name")
	}
}

// TestDesignWorkflowAllowedBots asserts the allowed_bots actor is templated from the
// configured App slug (never hardcoded) and, crucially, is OMITTED entirely when the
// slug is empty — an unconfigured deployment then still supports human-dispatched runs
// rather than emitting an empty/invalid allowed_bots value.
func TestDesignWorkflowAllowedBots(t *testing.T) {
	// With a configured slug, allowed_bots renders with exactly that slug, and it must
	// parse as valid YAML.
	withSlug := string(renderedDesignWorkflow(t, "acme-aiarch-bot"))
	if !strings.Contains(withSlug, "allowed_bots: acme-aiarch-bot") {
		t.Errorf("rendered workflow must set allowed_bots to the configured slug; got:\n%s", withSlug)
	}
	var doc workflowDoc
	if err := yaml.Unmarshal([]byte(withSlug), &doc); err != nil {
		t.Fatalf("rendered workflow (with slug) must parse as YAML: %v", err)
	}

	// With an empty slug, the allowed_bots KEY must be ABSENT (guard: omit, don't emit
	// empty). The result must still be a valid workflow (parses as YAML).
	empty := string(renderedDesignWorkflow(t, ""))
	if strings.Contains(empty, "allowed_bots:") {
		t.Errorf("empty slug must omit the allowed_bots key entirely; got:\n%s", empty)
	}
	if err := yaml.Unmarshal([]byte(empty), &doc); err != nil {
		t.Fatalf("rendered workflow (empty slug) must parse as YAML: %v", err)
	}
}

// TestManagedScaffoldFiles asserts the birth scaffold bundle: the design workflow +
// the templated go-test gate (go.mod + aiarch_method_test.go) + the internal/.gitkeep
// placeholder, all on the managed-file allowlist, with the repo's module path
// templated in.
func TestManagedScaffoldFiles(t *testing.T) {
	// owner|owner/repo encoding the RA produces (makeRepoRef): account=acme,
	// fullName=acme/widgets.
	repo := makeRepoRef("acme", "acme/widgets")
	files, err := ManagedScaffoldFiles(repo, testAppSlug)
	if err != nil {
		t.Fatalf("ManagedScaffoldFiles: %v", err)
	}
	if len(files) != 4 {
		t.Fatalf("want 4 managed files (workflow + go.mod + method test + internal/.gitkeep), got %d", len(files))
	}

	byPath := map[string]ManagedFile{}
	for _, f := range files {
		byPath[f.Path] = f
		// Every seated file MUST be on the managed-file allowlist (the verb rejects
		// anything else).
		if !isManagedFilePath(f.Path) {
			t.Errorf("scaffold file %q is not on the managed-file allowlist", f.Path)
		}
		if len(f.Content) == 0 {
			t.Errorf("scaffold file %q has empty content", f.Path)
		}
	}

	// (1) the design workflow is the template RENDERED with the App slug, under
	// .github/workflows/. It must equal renderDesignWorkflow(slug) and carry allowed_bots.
	wf, ok := byPath[DesignWorkflowPath]
	if !ok {
		t.Fatalf("missing %s in the scaffold bundle", DesignWorkflowPath)
	}
	if !bytes.Equal(wf.Content, renderedDesignWorkflow(t, testAppSlug)) {
		t.Error("workflow content must be the template rendered with the App slug")
	}
	if !strings.Contains(string(wf.Content), "allowed_bots: "+testAppSlug) {
		t.Errorf("seated workflow must allow-list the configured App slug; got:\n%s", wf.Content)
	}

	// (2) go.mod templated with the derived module path + the framework-go require pin.
	goMod, ok := byPath[GoModPath]
	if !ok {
		t.Fatalf("missing %s in the scaffold bundle", GoModPath)
	}
	gm := string(goMod.Content)
	if !strings.Contains(gm, "module github.com/acme/widgets") {
		t.Errorf("go.mod must declare the derived module path; got:\n%s", gm)
	}
	if !strings.Contains(gm, "require github.com/mixofreality-studio/archistrator-platform/framework-go "+FrameworkGoVersion) {
		t.Errorf("go.mod must require framework-go at the pinned version %q; got:\n%s", FrameworkGoVersion, gm)
	}

	// (3) the method test templates the module path into arch.MethodSpec + calls
	// methodcheck.Check.
	mt, ok := byPath[MethodTestPath]
	if !ok {
		t.Fatalf("missing %s in the scaffold bundle", MethodTestPath)
	}
	mts := string(mt.Content)
	if !strings.Contains(mts, "methodcheck.Check") {
		t.Error("method test must call methodcheck.Check")
	}
	if !strings.Contains(mts, `arch.MethodSpec(".", "github.com/acme/widgets/")`) {
		t.Errorf("method test must template the module path into arch.MethodSpec; got:\n%s", mts)
	}

	// (4) internal/.gitkeep keeps the internal/ directory present so the method gate's
	// arch.MethodSpec ./internal/... load pattern resolves instead of hard-erroring on a
	// fresh repo. It is static (not templated), non-empty (CommitManagedFiles rejects
	// empty content), and its path is on the managed-file allowlist (asserted above in
	// the per-file loop).
	gk, ok := byPath[InternalGitkeepPath]
	if !ok {
		t.Fatalf("missing %s in the scaffold bundle", InternalGitkeepPath)
	}
	if InternalGitkeepPath != "internal/.gitkeep" {
		t.Errorf("InternalGitkeepPath must be the literal internal/.gitkeep; got %q", InternalGitkeepPath)
	}
	if string(gk.Content) != InternalGitkeepContent {
		t.Errorf("internal/.gitkeep content = %q, want %q", gk.Content, InternalGitkeepContent)
	}
	if len(gk.Content) == 0 {
		t.Error("internal/.gitkeep must be non-empty (CommitManagedFiles rejects empty content)")
	}
}

// TestInternalGitkeepAcceptedByAllowlist proves the seeded placeholder path is on the
// managed-file allowlist (so CommitManagedFiles accepts it), while an arbitrary file
// under internal/ is NOT (the allowlist lists the literal internal/.gitkeep, not an
// internal/ prefix — keeping it tight).
func TestInternalGitkeepAcceptedByAllowlist(t *testing.T) {
	if !isManagedFilePath(InternalGitkeepPath) {
		t.Errorf("%q must be on the managed-file allowlist", InternalGitkeepPath)
	}
	if isManagedFilePath("internal/main.go") {
		t.Error("an arbitrary file under internal/ must NOT be on the allowlist — only the literal internal/.gitkeep is")
	}
}

// TestManagedScaffoldFilesRejectsZeroRepo proves a malformed RepoRef (no owner/repo)
// is a ContractMisuse the accessor surfaces, not a silent empty module path.
func TestManagedScaffoldFilesRejectsZeroRepo(t *testing.T) {
	if _, err := ManagedScaffoldFiles(RepoRef(""), testAppSlug); err == nil {
		t.Fatal("expected an error for a zero RepoRef (unresolvable module path)")
	}
}

// TestRailAppSlug proves the birth-scaffold caller can read the App slug off the
// concrete GitHub access (which knows its own slug), and that a rail NOT exposing it
// yields "" (so allowed_bots is omitted rather than emitted empty).
func TestRailAppSlug(t *testing.T) {
	// The concrete access exposes its configured slug via AppSlug(); RailAppSlug reads it.
	a := &access{appSlug: "cfg-app-slug"}
	if got := a.AppSlug(); got != "cfg-app-slug" {
		t.Errorf("access.AppSlug() = %q, want cfg-app-slug", got)
	}
	if got := RailAppSlug(a); got != "cfg-app-slug" {
		t.Errorf("RailAppSlug(access) = %q, want cfg-app-slug", got)
	}

	// A rail that does not expose AppSlug (any SourceControlAccess without the method)
	// yields "" — the omit-allowed_bots guard.
	if got := RailAppSlug(railWithoutSlug{}); got != "" {
		t.Errorf("RailAppSlug(rail-without-AppSlug) = %q, want empty", got)
	}
}

// railWithoutSlug is a SourceControlAccess that does NOT implement AppSlug() (like a
// test fake), used to prove RailAppSlug degrades to "". All methods panic — RailAppSlug
// only type-asserts, it never calls them.
type railWithoutSlug struct{}

func (railWithoutSlug) AdoptProjectRepo(fwra.Context, RepoAdoptionSpec) (RepoRef, error) {
	panic("unused")
}
func (railWithoutSlug) CommitManagedFiles(fwra.Context, RepoRef, []ManagedFile, RepoCredential) (CommitRef, error) {
	panic("unused")
}
func (railWithoutSlug) ConfigureBranchProtection(fwra.Context, RepoRef, RepoCredential) error {
	panic("unused")
}
func (railWithoutSlug) GetInstallationToken(fwra.Context, RepoRef) (RepoCredential, error) {
	panic("unused")
}
func (railWithoutSlug) GetPullRequestStatus(fwra.Context, RepoRef, PullRequestRef, RepoCredential) (PullRequestStatus, error) {
	panic("unused")
}
func (railWithoutSlug) InstallAuthorizeApp(fwra.Context, AccountRef) (Installation, error) {
	panic("unused")
}
func (railWithoutSlug) MergePullRequest(fwra.Context, RepoRef, PullRequestRef, RepoCredential) (MergeResult, error) {
	panic("unused")
}
func (railWithoutSlug) OpenBranch(fwra.Context, RepoRef, BranchName, RepoCredential) (BranchRef, error) {
	panic("unused")
}
func (railWithoutSlug) OpenPullRequest(fwra.Context, RepoRef, PullRequestSpec, RepoCredential) (PullRequestRef, error) {
	panic("unused")
}
func (railWithoutSlug) PostReview(fwra.Context, RepoRef, PullRequestRef, ReviewSubmission, RepoCredential) error {
	panic("unused")
}

var _ SourceControlAccess = railWithoutSlug{}
