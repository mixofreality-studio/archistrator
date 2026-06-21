package sourcecontrol

// agenticdesign_test.go — structural tests over the embedded DESIGN workflow asset
// (agenticdesign.go). It is an INTERNAL test package (package sourcecontrol) so it
// can read the unexported designWorkflowYAML embed var; the component's external
// service tests live in sourcecontrol_test.go (package sourcecontrol_test). Both
// test packages coexisting in one directory is permitted by go test.
//
// These assert the asset WIRING (the contract anchors), not a live Actions run.
// The yaml.v3 + framework-go-infrastructure-github imports here are TEST-ONLY, so
// the Method layering checker (loaded with Tests:false) never scans them.

import (
	"bytes"
	"strings"
	"testing"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	"gopkg.in/yaml.v3"
)

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
	if len(designWorkflowYAML) == 0 {
		t.Fatal("embedded aiarch-design.yml is empty")
	}
}

func TestEmbeddedTemplateParsesAsYAML(t *testing.T) {
	var doc workflowDoc
	if err := yaml.Unmarshal(designWorkflowYAML, &doc); err != nil {
		t.Fatalf("embedded template does not parse as YAML: %v", err)
	}
	if doc.Name == "" {
		t.Error("workflow has no top-level name")
	}
}

func TestDeclaresExpectedDispatchInputs(t *testing.T) {
	var doc workflowDoc
	if err := yaml.Unmarshal(designWorkflowYAML, &doc); err != nil {
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
	if err := yaml.Unmarshal(designWorkflowYAML, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The load-bearing input name MUST equal the satellite constant the
	// constructionPipelineAccess RA fills, or dispatch/observe/cancel break.
	if _, ok := doc.On.WorkflowDispatch.Inputs[fwgithub.DispatchInputKeyIdempotencyToken]; !ok {
		t.Errorf("workflow must declare the %q input (DispatchInputKeyIdempotencyToken)",
			fwgithub.DispatchInputKeyIdempotencyToken)
	}
	// run-name MUST carry the RunNamePrefix so ListRunsByName can resolve runs.
	if !strings.HasPrefix(doc.RunName, fwgithub.RunNamePrefix) {
		t.Errorf("run-name %q must start with RunNamePrefix %q", doc.RunName, fwgithub.RunNamePrefix)
	}
	if !strings.Contains(doc.RunName, "${{ inputs."+fwgithub.DispatchInputKeyIdempotencyToken+" }}") {
		t.Errorf("run-name %q must stamp the idempotency_token input", doc.RunName)
	}
}

func TestReferencesGoTestGateAndStatePath(t *testing.T) {
	body := string(designWorkflowYAML)

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

// TestManagedScaffoldFiles asserts the birth scaffold bundle: the design workflow +
// the templated go-test gate (go.mod + aiarch_method_test.go), all on the managed-
// file allowlist, with the repo's module path templated in.
func TestManagedScaffoldFiles(t *testing.T) {
	// owner|owner/repo encoding the RA produces (makeRepoRef): account=acme,
	// fullName=acme/widgets.
	repo := makeRepoRef("acme", "acme/widgets")
	files, err := ManagedScaffoldFiles(repo)
	if err != nil {
		t.Fatalf("ManagedScaffoldFiles: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("want 3 managed files (workflow + go.mod + method test), got %d", len(files))
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

	// (1) the design workflow is the embedded template bytes under .github/workflows/.
	wf, ok := byPath[DesignWorkflowPath]
	if !ok {
		t.Fatalf("missing %s in the scaffold bundle", DesignWorkflowPath)
	}
	if !bytes.Equal(wf.Content, designWorkflowYAML) {
		t.Error("workflow content must be the embedded template bytes")
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
}

// TestManagedScaffoldFilesRejectsZeroRepo proves a malformed RepoRef (no owner/repo)
// is a ContractMisuse the accessor surfaces, not a silent empty module path.
func TestManagedScaffoldFilesRejectsZeroRepo(t *testing.T) {
	if _, err := ManagedScaffoldFiles(RepoRef{}); err == nil {
		t.Fatal("expected an error for a zero RepoRef (unresolvable module path)")
	}
}
