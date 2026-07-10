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
//      pinned to FrameworkGoVersion, so a `go test` in the repo resolves methodcheck.
//   3. aiarch_method_test.go — the single test calling methodcheck.Check (the
//      all-in-one Method gate). Since 2026-07-06 the DESIGN workflow's REQUIRED
//      `validate` job runs `aiarch-state-mcp validate` (the pinned, self-updating
//      binary carrying the same design rules + the staleness-aware cross-artifact
//      severity policy) INSTEAD of this test; the scaffold remains seated for the
//      product repo's OWN CI once it has Go code (arch layer rules + design↔code
//      alignment need go/packages over the product module, which an installed
//      binary cannot run).
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
	"context"
	_ "embed"
	"fmt"
	"path"
	"text/template"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
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
// checklist. framework-go/v0.4.4 is published (C4-aware deployment model +
// methodcheck conformance, including the state-validation rule twins), so the seated
// gate resolves it from GOPROXY without a local replace. Updated here when framework-go
// is tagged.
const FrameworkGoVersion = "v0.4.4"

// StateMcpModulePath is the Go package path of the local stdio project-state MCP server
// the DESIGN workflow launches inside the GitHub Actions job (cmd/aiarch-state-mcp). The
// binary IS ProjectStateAccess code (agentic-managers spec §Construction application): it
// operates on the checked-out working tree and validates every drafted model through the
// SAME projectstate codec + methodcheck the server uses on read-back. It lives in the
// archistrator SERVER module (not framework-go) because it must reuse the strict codec in
// server/internal — a package only that module can import. The workflow obtains it the
// SAME way the seated go.mod scaffold obtains framework-go: `go install <path>@<pin>`
// resolved via GOPROXY (a published module), so it carries the identical trust/access
// profile. Since 2026-07-06 the workflow's REQUIRED `validate` job also runs this
// binary's `validate` subcommand as the Method-invariant PR gate (staleness-aware
// cross-artifact severity — the amendment-deadlock fix), so the gate's rule stack
// self-updates with this pin via the managed-scaffold sync.
const StateMcpModulePath = "github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp"

// StateMcpModulePin is the version the workflow installs the state-MCP binary at. It must
// be a git ref GOPROXY can resolve for the PUBLIC archistrator repo — a full commit SHA
// (resolved to its pseudo-version), a tag (server/vX.Y.Z → the @vX.Y.Z form here), or a
// branch name.
//
// SOURCE OF TRUTH (managed-scaffold sync, 2026-07-06): this SOURCE CONSTANT is the single
// place the control plane declares which state-MCP binary generation its validators and
// prompts are compatible with. The RELEASE PROCESS updates it when the binary's codec /
// methodcheck rules change, in the same commit that changes them — the two can never
// version independently because they live in one module. It is a `var` (not `const`) so a
// release pipeline may also stamp it at build time via
// `-ldflags "-X .../sourcecontrol.StateMcpModulePin=<sha>"`; the in-source default below
// is what an unstamped build seats and syncs.
//
// A full SHA is pinned (NOT `@main`): GOPROXY caches branch→pseudo-version resolutions,
// so `@main` can silently serve a stale binary for hours — the exact drift class the
// managed-scaffold sync exists to eliminate. Seated workflow copies rendered with an
// older pin are refreshed by the design Managers' sync-on-dispatch (SyncManagedScaffold)
// before every design job, so a seated repo can no longer run against a pin this server's
// validators do not understand.
var StateMcpModulePin = "748566bc1cdb93e32db91f3a829721cc4444632c"

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

// DesignWorkflowFile returns the claude-code-action DESIGN workflow rendered with
// the configured App slug as a provider-neutral ManagedFile — EXACTLY the file the
// birth seat commits. Exported (2026-07-06 managed-scaffold sync) so the design
// Managers' sync-on-dispatch can re-render the CURRENT template and converge the
// seated copy against it; the seat path (ManagedScaffoldFiles) and the sync path
// share this single rendering, so they can never disagree about the target bytes.
func DesignWorkflowFile(appSlug string) (ManagedFile, error) {
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

	workflow, err := DesignWorkflowFile(appSlug)
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

// syncManagedScaffoldMessage is the commit message a managed-scaffold SYNC commit
// carries (distinct from the birth-seat ManagedCommitMessage): it names the refreshed
// file AND the state-MCP pin the refreshed rendering installs, so the repo history
// records exactly when the scaffold was brought current and to which binary generation.
func syncManagedScaffoldMessage() string {
	return fmt.Sprintf("aiarch: sync managed scaffold (%s) to aiarch-state-mcp@%s",
		path.Base(DesignWorkflowPath), StateMcpModulePin)
}

// managedFileSyncer is the OPTIONAL hand-written auxiliary sync surface a concrete
// SourceControlAccess may expose (the GitHub-backed access does — see
// (*access).SyncManagedFiles): the seat write with a caller-supplied commit message
// and an explicit drifted/converged report. Same discovery pattern as RailAppSlug.
type managedFileSyncer interface {
	SyncManagedFiles(ctx context.Context, repo RepoRef, files []ManagedFile, message string, cred RepoCredential) (CommitRef, bool, error)
}

// SyncManagedScaffold ensures the SEATED design workflow (aiarch-design.yml on the
// repo's DEFAULT branch) matches the CURRENT template rendering — the managed-scaffold
// sync the design Managers run BEFORE every design-job dispatch (2026-07-06; closes
// the CreateProject-seats-once drift: the birth seat's constant idempotency key means
// a seated copy was otherwise never refreshed, so server upgrades stranded live repos
// on a stale aiarch-state-mcp pin whose binary the new validators reject).
//
// It re-renders the workflow exactly as seat-time would TODAY (DesignWorkflowFile with
// the rail's own App slug) and converges the seated copy through the rail:
//   - drifted   → ONE commit to the default branch (sync message naming the new pin),
//     changed=true
//   - identical → NO commit (the contents write is fetch-compare-put, byte-identical
//     short-circuits), changed=false
//
// SCOPE: the design workflow file ONLY. The rest of the birth scaffold (go.mod /
// aiarch_method_test.go / internal/.gitkeep) is deliberately NOT synced — go.mod is
// user-evolved after birth (their requires) and re-seating it would destroy user
// content; see docs/later.md for the earmarked follow-ups.
//
// When the concrete rail lacks the auxiliary sync surface (a test fake), it falls back
// to the FROZEN CommitManagedFiles verb — the identical converge semantics under the
// seat message — reporting changed=false (the frozen verb does not report drift).
// A sync error means the seated scaffold could not be proven current; the caller MUST
// fail the dispatch (never dispatch against a known-stale scaffold).
func SyncManagedScaffold(ctx context.Context, rail SourceControlAccess, repo RepoRef, cred RepoCredential) (bool, error) {
	if rail == nil {
		return false, fmt.Errorf("sourcecontrol: SyncManagedScaffold: nil rail")
	}
	file, err := DesignWorkflowFile(RailAppSlug(rail))
	if err != nil {
		return false, err
	}
	if s, ok := rail.(managedFileSyncer); ok {
		_, changed, serr := s.SyncManagedFiles(ctx, repo, []ManagedFile{file}, syncManagedScaffoldMessage(), cred)
		return changed, serr
	}
	_, err = rail.CommitManagedFiles(fwra.Context{Context: ctx}, repo, []ManagedFile{file}, cred)
	return false, err
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
