package sourcecontrol

// agenticdesign.go supplies the aiarch-MANAGED project scaffold archistrator-server
// seats into each user project repo at project birth (CommitManagedFiles). The
// scaffold is FOUR files committed in one birth seat:
//
//   1. .github/workflows/aiarch-design.yml — the claude-code-action DESIGN workflow
//      (the DESIGN counterpart of the construction reference workflow
//      products/archistrator/deploy/construction-workflow/aiarch-construct.yml). It
//      is COMMITTED by the server (not hand-installed), so the template is embedded
//      (//go:embed) and wrapped in the RA's provider-neutral ManagedFile value type.
//      It is TEMPLATED with the configured GitHub App slug so the claude-code-action
//      step allow-lists that bot (allowed_bots) — the design workflow is ALWAYS
//      workflow_dispatch'ed by the aiarch App (a Bot actor), which the action refuses
//      unless allow-listed. The slug varies per deployment, so it is not hardcoded.
//   2. go.mod — `module <REPO_MODULE>` (github.com/<owner>/<repo>, derived from the
//      adopted RepoRef) + a go directive + `require github.com/mixofreality-studio/archistrator-platform/framework-go`
//      pinned to FrameworkGoVersion, so the seated `go test` resolves methodcheck.
//   3. aiarch_method_test.go — the single test calling methodcheck.Check (the
//      all-in-one Method gate). It is what `go test ./...` runs as the REQUIRED check.
//   4. internal/.gitkeep — a placeholder that keeps the internal/ directory PRESENT
//      in a fresh repo. The seated method test (3) runs methodcheck.Check, whose
//      arch.MethodSpec loads the `./internal/...` package pattern; on an empty birth
//      repo that pattern HARD-ERRORS ("lstat ./internal/: no such file or directory")
//      and reddens the required check on every fresh project until the directory is
//      hand-added. Seating an empty-ish internal/.gitkeep makes internal/ exist at
//      birth so the load pattern resolves (to zero packages, a valid no-op) and the
//      gate is green from the first commit. It is a static one-liner (not templated):
//      git needs a non-empty tracked file, and CommitManagedFiles rejects empty
//      content, so it carries a single explanatory comment line.
//
// (1)–(3) are TEMPLATED at birth: (2) and (3) with the repo's module path (and the
// pinned framework-go version); (1) with the configured GitHub App slug (allowed_bots).
// (1) uses custom Go-template delimiters [[ ]] so GitHub's own ${{ ... }} expressions
// are left untouched. (4) is static content.
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

// designWorkflowTmplText is the embedded DESIGN workflow text/template source. It is
// rendered (renderDesignWorkflow) with the configured GitHub App slug, then committed
// into the user repo at .github/workflows/aiarch-design.yml. It uses custom [[ ]]
// delimiters so GitHub Actions' own ${{ ... }} expressions pass through verbatim.
//
//go:embed assets/aiarch-design.yml.tmpl
var designWorkflowTmplText string

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

// internalGitkeepPath is the repo-root placeholder that keeps the internal/ directory
// present at project birth so the seated method test's arch.MethodSpec `./internal/...`
// load pattern resolves (to zero packages) instead of hard-erroring on a missing dir.
// It MUST match the sourcecontrol managed-file allowlist scaffold roots (github.go
// scaffoldRootPaths) so CommitManagedFiles accepts it — the allowlist lists this
// LITERAL path, not an internal/ prefix, so it stays tight.
const internalGitkeepPath = "internal/.gitkeep"

// internalGitkeepContent is the static, non-empty content of the internal/.gitkeep
// placeholder. git needs a tracked file (a bare empty directory cannot be committed)
// and CommitManagedFiles rejects empty content, so the file carries a single comment
// line explaining why it exists.
const internalGitkeepContent = "# keeps internal/ present for the Method arch gate (./internal/... load pattern)\n"

// GoVersion is the Go directive the seated go.mod declares. It tracks framework-go's
// own go.mod `go` line so the user module and framework-go agree on the language
// version (framework-go is go 1.25.0).
const GoVersion = "1.25.0"

// FrameworkGoVersion is the PINNED framework-go module version the seated go.mod
// requires. The user repo's `go test` must RESOLVE github.com/mixofreality-studio/archistrator-platform/framework-go
// at this version (published/tagged, or served via GOPROXY) — see the founder
// checklist. framework-go/v0.4.0 is published (C4-aware deployment model +
// methodcheck conformance), so the seated gate resolves it from GOPROXY without a
// local replace. Updated here when framework-go is tagged.
const FrameworkGoVersion = "v0.4.0"

// StateMcpModulePath is the Go package path of the local stdio project-state MCP server
// the DESIGN workflow launches inside the GitHub Actions job (cmd/aiarch-state-mcp). The
// binary IS ProjectStateAccess code (agentic-managers spec §Construction application): it
// operates on the checked-out working tree and validates every drafted model through the
// SAME projectstate codec + methodcheck the server uses on read-back. It lives in the
// archistrator SERVER module (not framework-go) because it must reuse the strict codec in
// server/internal — a package only that module can import. The workflow obtains it the
// SAME way the seated `go test` obtains framework-go: `go install <path>@<pin>` resolved
// via GOPROXY (a published module), so it carries the identical trust/access profile.
const StateMcpModulePath = "github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp"

// StateMcpModulePin is the version the workflow installs the state-MCP binary at. It must
// be a git ref GOPROXY can resolve for the archistrator repo — a tag (server/vX.Y.Z → the
// @vX.Y.Z form here) or a branch (@main, resolved to its pseudo-version).
//
// FOUNDER-GATED: the archistrator app repo must be PUBLIC for GOPROXY to resolve this
// (run PUSH-APP.sh), exactly as archistrator-platform is public for the framework-go pin.
// Until then the design workflow's MCP install step fails RED (the correct signal — a
// visible failed gate, never a silent skip). For reproducibility the founder may move this
// to a tagged `server/vX.Y.Z` release once cut; `main` works the moment the repo is public.
const StateMcpModulePin = "main"

// NOTE (2026-06-15 correction): the embedded DESIGN workflow reads
// ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }} to authenticate claude-code-action, but that
// Actions secret is provisioned by the Claude Code GitHub App when the USER runs
// /install-github-app on their repo — aiarch does NOT seat it.

// renderDesignWorkflow renders the embedded DESIGN workflow template with the given
// GitHub App slug. It uses custom [[ ]] delimiters so GitHub's ${{ ... }} expressions
// are literal text. An EMPTY appSlug omits the allowed_bots line entirely — the seated
// workflow then still runs for a human-dispatched run (a bot-dispatched run would fail
// the action's non-human-actor guard, which is the correct signal in an unconfigured
// deployment rather than an empty/invalid allowed_bots value).
func renderDesignWorkflow(appSlug string) ([]byte, error) {
	t, err := template.New("aiarch-design.yml").Delims("[[", "]]").Parse(designWorkflowTmplText)
	if err != nil {
		return nil, fmt.Errorf("sourcecontrol: parse aiarch-design.yml template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, scaffoldTemplateData{
		AppSlug:            appSlug,
		StateMcpModulePath: StateMcpModulePath,
		StateMcpModulePin:  StateMcpModulePin,
	}); err != nil {
		return nil, fmt.Errorf("sourcecontrol: render aiarch-design.yml template: %w", err)
	}
	return buf.Bytes(), nil
}

// designWorkflowManagedFile returns the claude-code-action DESIGN workflow rendered
// with the configured App slug as a provider-neutral ManagedFile.
func designWorkflowManagedFile(appSlug string) (ManagedFile, error) {
	content, err := renderDesignWorkflow(appSlug)
	if err != nil {
		return ManagedFile{}, err
	}
	return ManagedFile{Path: DesignWorkflowPath, Content: content}, nil
}

// scaffoldTemplateData is the render context for the birth-scaffold templates: the
// repo's Go module path + the Go + framework-go version pins (go.mod + method test),
// plus the configured GitHub App slug (the DESIGN workflow's allowed_bots actor). Each
// template uses only the fields it needs.
type scaffoldTemplateData struct {
	Module             string
	GoVersion          string
	FrameworkGoVersion string
	AppSlug            string
	// StateMcpModulePath / StateMcpModulePin template the `go install <path>@<pin>` the
	// DESIGN workflow uses to obtain the local project-state MCP server binary.
	StateMcpModulePath string
	StateMcpModulePin  string
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
// aiarch_method_test.go) + the internal/.gitkeep placeholder — templated with the
// adopted repo's Go module path (github.com/<owner>/<repo>, derived from repo via
// RepoRef.OwnerRepo), the pinned Go + framework-go versions, and the configured
// GitHub App slug (the design workflow's allowed_bots actor). The internal/.gitkeep
// file is static (the method gate's arch.MethodSpec `./internal/...` load pattern
// hard-errors on a missing internal/ dir, so the placeholder makes it exist at birth).
// The C-PM-Δ caller hands the returned slice to CommitManagedFiles, which seats all
// four in one birth seat.
//
// appSlug is the deployment's GitHub App slug (from the composition root). The caller
// reads it off the rail with RailAppSlug so it is never hardcoded. An EMPTY slug
// (unconfigured dev server) is valid: the design workflow simply omits allowed_bots.
//
// An empty/malformed RepoRef (owner/repo unresolvable) is a ContractMisuse the caller
// surfaces — the module path cannot be templated without the repo coordinates.
func ManagedScaffoldFiles(repo RepoRef, appSlug string) ([]ManagedFile, error) {
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

	workflow, err := designWorkflowManagedFile(appSlug)
	if err != nil {
		return nil, err
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
		workflow,
		{Path: GoModPath, Content: goMod},
		{Path: MethodTestPath, Content: methodTest},
		{Path: internalGitkeepPath, Content: []byte(internalGitkeepContent)},
	}, nil
}

// RailAppSlug reads the configured GitHub App slug off a SourceControlAccess when the
// concrete implementation exposes it (the GitHub-backed access does — see
// (*access).AppSlug). A rail that does not — e.g. a test fake or a repo-less dev
// server (nil rail) — yields "" so the seated design workflow omits allowed_bots (a
// human-dispatched run still works). This keeps the slug's source of truth inside the
// RA: the birth-scaffold caller obtains it FROM the rail rather than threading it
// through the Manager's generated constructor.
func RailAppSlug(rail SourceControlAccess) string {
	if p, ok := rail.(interface{ AppSlug() string }); ok {
		return p.AppSlug()
	}
	return ""
}
