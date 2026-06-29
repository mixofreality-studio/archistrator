package sourcecontrol

// agenticdesign.go supplies the aiarch-MANAGED project scaffold archistrator-server
// seats into each user project repo at project birth (CommitManagedFiles). The
// scaffold is THREE files committed in one birth seat:
//
//   1. .github/workflows/aiarch-design.yml — the claude-code-action DESIGN workflow
//      (the DESIGN counterpart of the construction reference workflow
//      products/archistrator/deploy/construction-workflow/aiarch-construct.yml). It
//      is COMMITTED by the server (not hand-installed), so the template is embedded
//      (//go:embed) and wrapped in the RA's provider-neutral ManagedFile value type.
//   2. go.mod — `module <REPO_MODULE>` (github.com/<owner>/<repo>, derived from the
//      adopted RepoRef) + a go directive + `require github.com/mixofreality-studio/archistrator-platform/framework-go`
//      pinned to FrameworkGoVersion, so the seated `go test` resolves methodcheck.
//   3. aiarch_method_test.go — the single test calling methodcheck.Check (the
//      all-in-one Method gate). It is what `go test ./...` runs as the REQUIRED check.
//
// (2) and (3) are TEMPLATED with the repo's module path (and the pinned framework-go
// version) at birth; (1) is a static embedded asset.
//
// This asset accessor lives DIRECTLY in the sourceControlAccess package (not a
// sub-package) on purpose: the embedded templates are consumed only by this RA's own
// frozen CommitManagedFiles verb and are wrapped in this RA's own ManagedFile value
// type, so they are part of the sourceControlAccess component, not a peer of it. A
// sub-package would classify as a SECOND ResourceAccess component and its import of
// the ManagedFile type would be a forbidden RA→RA sideways edge (the-method-layers);
// folding it in keeps a single, correctly-classified RA.
//
// It adds NO ResourceAccess verb and speaks NO GitHub wire lexicon: it is a pure
// asset accessor. The COMMIT is performed by the C-PM-Δ caller through the
// already-built CommitManagedFiles verb; the DISPATCH is performed by the design
// Managers (C-MSD-Δ / C-MPD-Δ) through the frozen
// constructionPipelineAccess.SubmitConstructionPipeline verb. The workflow_dispatch
// input names the workflow template declares are a CONTRACT with those Managers —
// see designs/aiarch/implementation/log/C-WF-DESIGN.md.

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
)

// designWorkflowYAML is the embedded DESIGN workflow template bytes. It is the exact
// content the server commits into the user repo at .github/workflows/.
//
//go:embed assets/aiarch-design.yml
var designWorkflowYAML []byte

// goModTemplateText / methodTestTemplateText are the embedded text/template sources
// for the go-test gate scaffold, rendered with the adopted repo's module path (+ the
// pinned framework-go version) at project birth.
//
//go:embed assets/go.mod.tmpl
var goModTemplateText string

//go:embed assets/aiarch_method_test.go.tmpl
var methodTestTemplateText string

// DesignWorkflowPath is the path under .github/workflows/ the DESIGN workflow is
// committed to. It satisfies the managed-file allowlist's .github/workflows/ prefix.
const DesignWorkflowPath = ".github/workflows/aiarch-design.yml"

// GoModPath / MethodTestPath are the repo-root scaffold paths the go-test gate is
// seated to. They MUST match the sourcecontrol managed-file allowlist scaffold roots
// (github.go scaffoldRootPaths) so CommitManagedFiles accepts them.
const (
	GoModPath      = "go.mod"
	MethodTestPath = "aiarch_method_test.go"
)

// GoVersion is the Go directive the seated go.mod declares. It tracks framework-go's
// own go.mod `go` line so the user module and framework-go agree on the language
// version (framework-go is go 1.25.0).
const GoVersion = "1.25.0"

// FrameworkGoVersion is the PINNED framework-go module version the seated go.mod
// requires. The user repo's `go test` must RESOLVE github.com/mixofreality-studio/archistrator-platform/framework-go
// at this version (published/tagged, or served via GOPROXY) — see the founder
// checklist. framework-go is currently consumed inside the monorepo via a local
// `replace` (v0.0.0); a published tag must back this pin before the seated gate works
// in the user's CI. Updated here when framework-go is tagged.
const FrameworkGoVersion = "v0.1.0"

// NOTE (2026-06-15 correction): the embedded DESIGN workflow reads
// ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }} to authenticate claude-code-action, but that
// Actions secret is provisioned by the Claude Code GitHub App when the USER runs
// /install-github-app on their repo — aiarch does NOT seat it.

// designWorkflowManagedFile returns the embedded claude-code-action DESIGN workflow
// as a provider-neutral ManagedFile (static; no templating).
func designWorkflowManagedFile() ManagedFile {
	return ManagedFile{Path: DesignWorkflowPath, Content: designWorkflowYAML}
}

// scaffoldTemplateData is the render context for the go-test gate templates: the
// repo's Go module path + the Go + framework-go version pins.
type scaffoldTemplateData struct {
	Module             string
	GoVersion          string
	FrameworkGoVersion string
}

// renderScaffoldFile renders one embedded text/template with the module path + pins.
func renderScaffoldFile(name, tmplText string, data scaffoldTemplateData) ([]byte, error) {
	t, err := template.New(name).Parse(tmplText)
	if err != nil {
		return nil, fmt.Errorf("sourcecontrol: parse %s template: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("sourcecontrol: render %s template: %w", name, err)
	}
	return buf.Bytes(), nil
}

// ManagedScaffoldFiles returns the FULL aiarch-managed project scaffold bundle to
// seat at project birth — the design workflow + the go-test gate (go.mod +
// aiarch_method_test.go) — templated with the adopted repo's Go module path
// (github.com/<owner>/<repo>, derived from repo via RepoRef.OwnerRepo) and the pinned
// Go + framework-go versions. The C-PM-Δ caller hands the returned slice to
// CommitManagedFiles, which seats all three in one birth seat.
//
// An empty/malformed RepoRef (owner/repo unresolvable) is a ContractMisuse the caller
// surfaces — the module path cannot be templated without the repo coordinates.
func ManagedScaffoldFiles(repo RepoRef) ([]ManagedFile, error) {
	owner, name, err := RepoRefOwnerRepo(repo)
	if err != nil {
		return nil, err
	}
	module := fmt.Sprintf("github.com/%s/%s", owner, name)
	data := scaffoldTemplateData{
		Module:             module,
		GoVersion:          GoVersion,
		FrameworkGoVersion: FrameworkGoVersion,
	}

	goMod, err := renderScaffoldFile("go.mod", goModTemplateText, data)
	if err != nil {
		return nil, err
	}
	methodTest, err := renderScaffoldFile("aiarch_method_test.go", methodTestTemplateText, data)
	if err != nil {
		return nil, err
	}

	return []ManagedFile{
		designWorkflowManagedFile(),
		{Path: GoModPath, Content: goMod},
		{Path: MethodTestPath, Content: methodTest},
	}, nil
}
