package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

// modulePrefix is the import-path prefix for this module's internal packages.
const modulePrefix = "github.com/mixofreality-studio/archistrator/server/internal/"

// TestMethodLayering enforces The Method's layer model on the archistrator server's
// internal packages: strictly closed downward imports, Temporal only in the
// manager layer, Engine/ResourceAccess ports named *Engine/*Access with error-
// returning methods, and the operator-curated dependency allowlist.
//
// Layer layout (top→bottom):
//   - manager/systemdesign                       — Manager façade; OWNS Temporal.
//     The Manager's SEQUENCE owns the Phase-1 prompt corpus (2026-05-29 re-cut);
//     drafting AND PM-critique are the GENERIC workerAccess.GenerateTypedData[T]
//     wrapped in a Manager-owned Activity, so the worker I/O stays out of replayed
//     workflow code.
//   - engine/{estimation,operationestimation,settlement}
//     — Engine ports. The estimate Engines are pure deterministic Phase-2 computation.
//     (The former systemdesign Engine was WITHDRAWN by the 2026-05-29 re-cut —
//     drafting a Method artifact via an LLM is the Manager's activity, not an
//     Engine. Server-side rendering was likewise removed — the client renders
//     typed models. The artifactValidation Engine was REMOVED 2026-06-16 — the
//     Method gate moved to framework-go/methodcheck, the seated `go test` in the
//     user repo; the Managers retained only the small Finding value type, relocated
//     locally to systemdesign/projectdesign.)
//   - resourceaccess/{projectstate,artifact,worker}
//     — ResourceAccess components, each fronting a single Resource. NO RA→RA
//     imports: worker is the GENERIC typed LLM worker (Generate raw JSON +
//     package-level GenerateTypedData[T] + Cancel) with NO Method-model knowledge —
//     it imports neither projectstate nor artifact; the typed Method models live
//     in projectstate and the construction value types live in artifact; only
//     Engines and the Manager import those.
//
// The Method has NO "domain" layer — only Clients/Managers/Engines/
// ResourceAccess/Resources/Utilities. Shared typed Method models are owned by
// the RA that fronts them (projectstate); downstream Engines and the Manager
// import the owning RA's package as a normal downward edge.
func TestMethodLayering(t *testing.T) {
	arch.Check(t, appArchSpec())
}

// TestFileLayout enforces the 2026-07-11 layer file-layout standard (one impl
// file, one file per workflow, one test file, generated-only otherwise) —
// docs/superpowers/specs/2026-07-11-layer-file-layout-standard-design.md.
func TestFileLayout(t *testing.T) {
	arch.CheckFileLayout(t, appArchSpec())
}

// TestMessageBusManagersOnly enforces the ch. 5 RESTRICTED-UTILITY ruling
// (founder R-F, 2026-08-01): the message-bus utility may be imported ONLY by
// Manager-layer packages (its own subtree — the generated fake — aside).
//
// Unlike Security / Logging / Diagnostics, which any layer may call, this utility
// has a restricted clientele, and the reason is layer doctrine rather than taste:
// Engines are pure computation and do no messaging I/O; each ResourceAccess fronts
// exactly one resource, and a cross-execution signal is not it; Clients enter the
// system at a Manager. The composition root (cmd/server, outside internal/)
// constructs and injects it and is therefore out of the scan. The slot-5 edge set
// MIRRORS this rule — every inbound message-bus relationship originates at a
// Manager — so the model and the code cannot drift on who may call the bus.
//
// arch.Check cannot express this (its rules are per-layer, not per-package), so it
// lives here as an app-local test with the same packages.Load posture
// (Tests:false, ./internal/...). EARMARK: a reusable
// arch.CheckRestrictedImport(t, spec, target, allowedDirPrefixes) belongs in
// framework-go; note it in the release notes rather than blocking on it.
func TestMessageBusManagersOnly(t *testing.T) {
	const busPkg = modulePrefix + "utility/messagebus"
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedImports,
		Dir:   "..",
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./internal/...")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("%d package load error(s); fix the build before checking the clientele", n)
	}
	// Guard against a vacuous pass: if nothing loaded, or nothing imports the bus
	// at all, the rule is checking air.
	if len(pkgs) == 0 {
		t.Fatal("patterns matched no packages; this test would pass vacuously")
	}
	paths := make(map[string][]string, len(pkgs))
	for _, pkg := range pkgs {
		for ip := range pkg.Imports {
			paths[pkg.PkgPath] = append(paths[pkg.PkgPath], ip)
		}
	}
	violations, importers := restrictedImportViolations(paths, busPkg, messageBusAllowedDirs)
	if importers == 0 {
		t.Error("no package imports the message-bus utility; the restricted-clientele rule is checking air")
	}
	for _, v := range violations {
		t.Errorf("arch: %s imports the message-bus utility; only Manager-layer packages may (ch. 5 restricted clientele)", v)
	}
}

// messageBusAllowedDirs are the ModulePrefix-relative DIRECTORIES allowed to
// import the bus: the Manager layer, plus the component's own subtree (its
// generated fake). Written without trailing slashes — restrictedImportViolations
// matches them as directories, not as raw string prefixes.
//
// scenariocompose is the generated TEST composition root (compose.gen.go): it builds
// messagebus.NewTemporalMessageBus exactly as the production root cmd/server/main.gen.go
// does (the cmd/ root is outside the scan; this one sits inside internal/). It is exempt on
// the same footing and ONLY while every file in it is generated -
// TestMessageBusExemptRootsStayGenerated fails the moment a hand-written file lands there,
// the same posture ARCH-SCENARIOCOMPOSE-TEST-ONLY takes on the package.
var messageBusAllowedDirs = []string{"manager", "utility/messagebus", messageBusGeneratedRootDir}

// messageBusGeneratedRootDir is the ModulePrefix-relative directory of the generated test
// composition root that messageBusAllowedDirs exempts.
const messageBusGeneratedRootDir = "scenariocompose"

// TestMessageBusExemptRootsStayGenerated ties the composition-root exemption to the package
// staying all-generated: a hand-written file in it would inherit a Manager's right to hold the
// bus with no review.
func TestMessageBusExemptRootsStayGenerated(t *testing.T) {
	entries, err := os.ReadDir(messageBusGeneratedRootDir)
	if err != nil {
		t.Fatalf("read %s: %v", messageBusGeneratedRootDir, err)
	}
	gen := 0
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("%s/%s: the exempt generated root has no subpackages", messageBusGeneratedRootDir, e.Name())
			continue
		}
		if !strings.HasSuffix(e.Name(), ".gen.go") {
			t.Errorf("%s/%s is hand-written; the message-bus exemption holds only while the package is all-generated", messageBusGeneratedRootDir, e.Name())
			continue
		}
		gen++
	}
	if gen == 0 {
		t.Errorf("%s holds no generated file; drop the exemption instead of keeping it vacuous", messageBusGeneratedRootDir)
	}
}

// restrictedImportViolations is the pure core of TestMessageBusManagersOnly:
// given each package's import list, it returns the importing package paths that
// no allowed directory prefix covers, plus the TOTAL number of importers found
// (the vacuity guard's input). Separated from packages.Load so the rule itself is
// unit-testable against a synthetic package set — see
// TestRestrictedImportViolations.
func restrictedImportViolations(imports map[string][]string, target string, allowedDirs []string) (violations []string, importers int) {
	for pkgPath, ips := range imports {
		rel := strings.TrimPrefix(pkgPath, modulePrefix)
		for _, ip := range ips {
			if !underDir(ip, target) {
				continue
			}
			importers++
			allowed := false
			for _, dir := range allowedDirs {
				if underDir(rel, dir) {
					allowed = true
					break
				}
			}
			if !allowed {
				violations = append(violations, pkgPath)
			}
			break // one verdict per importing package
		}
	}
	sort.Strings(violations)
	return violations, importers
}

// underDir reports whether an import path or ModulePrefix-relative package path
// IS dir or lives beneath it. It matches on the path SEPARATOR, never on the raw
// string, so a sibling whose name merely starts with dir — utility/messagebusfoo
// against utility/messagebus, or managerial against manager — is correctly
// excluded. A trailing slash on dir is tolerated so callers may write either form.
func underDir(p, dir string) bool {
	dir = strings.TrimSuffix(dir, "/")
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// TestRestrictedImportViolations is the NEGATIVE proof that
// TestMessageBusManagersOnly would actually fail: a synthetic package set where an
// Engine and a ResourceAccess import the bus must produce exactly those two
// violations, while the Manager importer and the component's own generated fake
// must not. Without this, a bug in the rule would make the gate silently green.
func TestRestrictedImportViolations(t *testing.T) {
	const bus = modulePrefix + "utility/messagebus"
	got, importers := restrictedImportViolations(map[string][]string{
		modulePrefix + "manager/billing":         {bus, modulePrefix + "resourceaccess/usage"},
		modulePrefix + "utility/messagebus/fake": {bus},
		modulePrefix + "engine/designhealth":     {bus},
		modulePrefix + "resourceaccess/usage":    {bus},
		modulePrefix + "client/web":              {modulePrefix + "manager/billing"},
		// Name-prefix impostors: neither is inside an allowed DIRECTORY, so a raw
		// strings.HasPrefix over the allow list would wrongly exempt both.
		modulePrefix + "utility/messagebusfoo": {bus},
		modulePrefix + "managerial":            {bus},
	}, bus, messageBusAllowedDirs)

	want := []string{
		modulePrefix + "engine/designhealth",
		modulePrefix + "managerial",
		modulePrefix + "resourceaccess/usage",
		modulePrefix + "utility/messagebusfoo",
	}
	if !slices.Equal(got, want) {
		t.Errorf("violations = %v, want %v", got, want)
	}
	if importers != 6 {
		t.Errorf("importers = %d, want 6 (manager + fake + engine + RA + 2 impostors)", importers)
	}
}

// TestBuildStatusVocabulariesAgree pins the two INDEPENDENT copies of the buildStatus
// closed vocabulary to each other: estimation's knownBuildStatus (estimationengine.go)
// and designhealth's DH-BUILDSTATUS-VOCAB rule, componentBuildStatusVocabularyFindings
// (designhealthengine.go). The two copies exist because designhealth may import only
// the two published platform modules plus stdlib (its own file header), so it cannot
// import the in-flight estimation package, and the platform's projectmodel.
// SystemComponent — the tolerant codegen slice designhealth reads — carries no
// buildStatus field to hang a SHARED check off (extending it is a platform release,
// earmarked, not done here). Without this test, a founder-ruled widening of one copy
// (e.g. adding "retired") would silently drift from the other: the derivation would
// build a component design-health calls a defect, or the reverse.
//
// Reads each function's own SOURCE via go/ast rather than importing either package, so
// it needs no export from estimation (knownBuildStatus is deliberately unexported —
// nothing outside its own package's tests calls it, see the encapsulation gate) and it
// adds no Go-level dependency either package's import-boundary doctrine would object
// to. A case naming a same-file const identifier (estimation's buildStatusPlanned/
// buildStatusExternal) is resolved back to its literal value; designhealth's case
// already spells its three values as raw string literals.
func TestBuildStatusVocabulariesAgree(t *testing.T) {
	estimationSet := buildStatusVocabularyFromSource(t,
		"engine/estimation/estimationengine.go", "knownBuildStatus")
	designhealthSet := buildStatusVocabularyFromSource(t,
		"engine/designhealth/designhealthengine.go", "componentBuildStatusVocabularyFindings")

	sort.Strings(estimationSet)
	sort.Strings(designhealthSet)
	if !slices.Equal(estimationSet, designhealthSet) {
		t.Errorf("buildStatus vocabularies drifted: estimation.knownBuildStatus = %v, "+
			"designhealth DH-BUILDSTATUS-VOCAB = %v — widen or narrow BOTH copies "+
			"together (see designhealthengine.go's import-boundary note for why they "+
			"cannot share one Go symbol)", estimationSet, designhealthSet)
	}
}

// buildStatusVocabularyFromSource parses path (a file path relative to this test's own
// package directory, internal/) and returns the constant string values of funcName's
// FIRST switch statement's first case clause — the closed-vocabulary list both
// knownBuildStatus and componentBuildStatusVocabularyFindings key their case on before
// any default/fallthrough. Fails loudly (t.Fatalf, never a silent empty result) the
// moment either function's shape moves out from under this scan, so a refactor that
// breaks the drift guard cannot pass unnoticed.
func buildStatusVocabularyFromSource(t *testing.T, path, funcName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	fn := funcDeclByName(file, funcName)
	if fn == nil {
		t.Fatalf("%s: func %s not found", path, funcName)
	}
	cc := leadingSwitchCase(fn)
	if cc == nil {
		t.Fatalf("%s: %s: no leading value case found in a switch statement — want the "+
			"vocabulary list first, default (or the loop's fallthrough) last", path, funcName)
	}

	consts := stringConstsInFile(file)
	values := make([]string, 0, len(cc.List))
	for _, expr := range cc.List {
		v, ok := caseExprStringValue(expr, consts)
		if !ok {
			t.Fatalf("%s: %s: case expr %v does not resolve to a same-file string literal or const",
				path, funcName, expr)
		}
		values = append(values, v)
	}
	if len(values) == 0 {
		t.Fatalf("%s: %s: resolved zero case values; this scan is checking air", path, funcName)
	}
	return values
}

// funcDeclByName returns file's top-level function declaration named name, or nil.
func funcDeclByName(file *ast.File, name string) *ast.FuncDecl {
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
			return fd
		}
	}
	return nil
}

// leadingSwitchCase returns fn's FIRST switch statement's FIRST case clause — the
// closed-vocabulary list both knownBuildStatus and
// componentBuildStatusVocabularyFindings key their case on before any
// default/fallthrough — or nil if fn has no switch, or that switch's first clause is
// the default rather than a value case.
func leadingSwitchCase(fn *ast.FuncDecl) *ast.CaseClause {
	var sw *ast.SwitchStmt
	ast.Inspect(fn, func(n ast.Node) bool {
		if sw != nil {
			return false
		}
		if s, ok := n.(*ast.SwitchStmt); ok {
			sw = s
			return false
		}
		return true
	})
	if sw == nil || len(sw.Body.List) == 0 {
		return nil
	}
	cc, ok := sw.Body.List[0].(*ast.CaseClause)
	if !ok || cc.List == nil {
		return nil
	}
	return cc
}

// caseExprStringValue resolves one switch-case expression to its string value: a
// literal ("planned") unquotes directly, an identifier (buildStatusPlanned) resolves
// through consts (the same file's top-level string const declarations).
func caseExprStringValue(expr ast.Expr, consts map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		v, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return v, true
	case *ast.Ident:
		v, ok := consts[e.Name]
		return v, ok
	default:
		return "", false
	}
}

// stringConstsInFile maps every top-level `const NAME = "literal"` declaration in file
// to its string value — just enough to resolve knownBuildStatus's buildStatusPlanned/
// buildStatusExternal case identifiers back to what they actually spell.
func stringConstsInFile(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if v, err := strconv.Unquote(lit.Value); err == nil {
					out[name.Name] = v
				}
			}
		}
	}
	return out
}

// appArchSpec is the app's Method layer model — the SINGLE source of truth shared by
// the standalone layer/encapsulation tests here AND the full methodcheck.Check gate
// in method_design_test.go (so the app validates its architecture ONE way, not two
// divergent ways). It is arch.MethodSpec plus the two app-specific relaxations: the
// durable-execution Temporal exemption and the operator-curated dependency allowlist.
func appArchSpec() arch.Spec {
	spec := arch.MethodSpec("..") // module root relative to internal/
	// There is no sideways escape hatch to configure. The typed-models rework
	// eliminated every sideways RA→RA import: worker is the generic typed LLM
	// worker that imports neither projectstate nor artifact; the Engines'
	// projectstate import is a legal Engine→RA downward edge. MethodSpec
	// makes every layer NoSideways and requires every internal package to
	// classify into client/manager/engine/resourceaccess, so an out-of-band
	// package (e.g. a "domain" dir) fails the build outright.

	// Temporal-isolation exemption — the single architecturally-sanctioned
	// exception to "Temporal lives only in the Manager layer". messageBus is the
	// component whose fronted substrate IS the durable-execution runtime (Temporal)
	// itself — "the architecturally hardest case in the corpus". Its concrete
	// implementation MUST speak the Temporal control-plane SDK, exactly as
	// projectstate's adapter speaks pgx and artifact's speaks go-git. The exemption
	// is PATH-SCOPED and LAYER-AGNOSTIC — it survived the component's
	// ResourceAccess → Utility reclassification unchanged, because what earns it is
	// the fronted substrate, not the layer. It relaxes ONLY the Temporal-isolation
	// rule for this one package; the package remains subject to classification,
	// downward-only imports, no-sideways, error returns, the dependency allowlist,
	// and (uniquely) the Managers-only clientele rule below. The CONTRACT surface
	// (the MessageBus port + value types) stays Temporal-free by the component's own
	// design and review. This list is the one and only place the exception is
	// granted; any OTHER RA/Engine/Utility importing Temporal still fails the build.
	//
	// The generated sibling utility/messagebus/fake is NOT covered by this prefix
	// (the match is exact-or-"/"-suffixed, not a directory prefix) and needs no
	// coverage: a generated fake imports only its contract package.
	spec.TemporalExemptPackages = []string{"utility/messagebus"}

	// The dependency menu is the framework's (arch.DefaultAllowedImportPrefixes, which
	// MethodSpec starts from), and every app gets the same one. The one app-specific extra:
	// typed-output JSON Schema derivation for LLM prompts (manager/systemdesign/schema.go).
	// An unsanctioned dependency (e.g. a MongoDB driver) fails this test. Test-only deps
	// (testcontainers, the Gitea SDK) are not scanned: Check loads with Tests:false.
	spec.AllowedImportPrefixes = append(spec.AllowedImportPrefixes, "github.com/invopop/jsonschema")
	return spec
}

// TestGeneratedOnlyPublic — the ENCAPSULATION GATE — delegates to the framework
// checker arch.CheckGeneratedSurface, which was PORTED VERBATIM from the app's own
// former hand-rolled implementation. Delegating means the app and every downstream
// consumer run ONE encapsulation gate, not two divergent copies (fix-right per the
// v0.4.0 platform release). The founder invariant is unchanged: the only exported
// symbols a generated-contract package (one carrying a *.gen.go file) may carry are
// its generated contract surface, the transitive closure of types it traffics in,
// and the documented per-package allowlist below.
func TestGeneratedOnlyPublic(t *testing.T) {
	arch.CheckGeneratedSurface(t, appArchSpec(), encapAllowlist(t))
}

// encapAllowlistFileName is the per-package file holding that package's encapsulation
// allowlist: one exported symbol per line, `#` starts a comment line. The file lives IN the
// package's own tree, so the component that owns the package owns its entries and the gate's
// code stays out of every construction task's edit scope.
const encapAllowlistFileName = "encapsulation.allowlist"

// encapAllowlist reads every package's encapsulation.allowlist under internal/ into the
// ModulePrefix-relative keys the framework checker expects ("engine/review" for the file
// internal/engine/review/encapsulation.allowlist). The SAME map feeds both
// TestGeneratedOnlyPublic and method_design_test.go's full methodcheck.Check gate via
// ProjectSpec.EncapsulationAllowlist.
func encapAllowlist(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != encapAllowlistFileName {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var syms []string
		for _, line := range strings.Split(string(raw), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				syms = append(syms, line)
			}
		}
		out[filepath.ToSlash(filepath.Dir(p))] = syms
		return nil
	})
	if err != nil {
		t.Fatalf("read encapsulation allowlists: %v", err)
	}
	return out
}
