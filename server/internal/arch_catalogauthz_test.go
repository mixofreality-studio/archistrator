package internal_test

// arch_catalogauthz_test.go is the standing gate over ONE invariant of the generated
// client surface that nothing else can express:
//
//	An operation is authorized against the OWNER-SCOPED CATALOG only when it names no
//	resource of its own — and every op that DOES name one is bound to it.
//
// WHY IT EXISTS. framework-go-http-generator decides the authorization resource by a
// CONVENTION over the op's shape (httpgen/plan.go planOp): with >=1 ID path param the
// ref is `{Kind:<identity minus the trailing ID>, ID:<that value>}`; with none it falls
// back to `{Kind:"<manager>Catalog", ID:principal.Subject}`. The fallback is right for a
// catalog listing or a platform-wide sweep, which name nothing. It is WRONG, and
// silently so, for an op that names its resource in the BODY — the decision then asks
// "may this principal call this verb at all", never "may it touch THAT one", and no
// test fails, because from the generator's side nothing is missing.
//
// Stage 4a produced both halves of that failure within one wave. StartProject's
// projectID moved into the body because an ABSENT id is what means CREATE and net/http
// cannot match an empty path segment; QueryProjectView's moved in because the op folds
// thirteen readers into one and `projectId` became a member of a query OBJECT, which
// can never be a path segment. Each had been a `{project, id}` path route
// (`system-design/get-project/{projectID}`, `get-pump-status/{projectID}`, …). Both
// silently inherited the catalog fallback; both were found by review, not by a gate.
// The composition root now re-asks the project-scoped question for exactly those two
// (projectScopedDeliveryManager, cmd/server/hooks.go), over BOTH transports — the MCP
// tool handlers authorize nothing of their own, so wrapping the Manager is the only
// place that reaches them.
//
// WHAT THIS GATE PINS, so the NEXT body-carried id fails a test instead of inheriting:
//
//  1. the catalog-scoped verb set of EVERY generated web handler package, measured from
//     the generated source (not asserted about delivery alone — a new manager, or a new
//     catalog-scoped op on an existing one, must come here and be justified);
//  2. that every handler authorizes at all — exactly one Action + one ResourceRef;
//  3. that the ops projectScopedDeliveryManager guards are EXACTLY delivery's
//     catalog-scoped set, so the guard can neither fall behind a new body id nor keep
//     guarding an op that went back onto the path.
//
// It is a RATCHET, not a proof of correctness: it cannot tell a body-carried id from a
// genuinely resource-less op. What it guarantees is that the choice is made by someone
// editing this file, with the reason written down, rather than by the shape of a
// parameter list.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// webHandlerRoot is the generated web-client tree, relative to server/internal. Every
// package under it is DISCOVERED rather than listed, so a manager added later cannot
// slip past this gate by not being named in it.
const webHandlerRoot = "client/web"

// hooksPath is the composition root's policy seam, relative to server/internal. It is
// outside internal/ (pure wiring glue, unscanned by the Method arch checker), so it is
// read as SOURCE here rather than loaded as a package.
const hooksPath = "../cmd/server/hooks.go"

// catalogScopedGolden is the owner-scoped-catalog verb set, per generated web handler
// package. EVERY entry is a deliberate decision; adding one means answering "does this
// op name a resource in its body?" and, if it does, giving it an arm on the composition
// root's guard instead.
//
//   - delivery/start-project — creates (names nothing) OR adopts (names a project in the
//     body). The adopt arm is guarded; the catalog decision still answers "may this
//     principal start projects at all", which is the right question for a create.
//   - delivery/query-project-view — the `projects` kind names nothing (it IS the catalog
//     listing); the other six kinds name a project through `query.projectId` and are
//     guarded.
//   - operations/reconcile-operated-state — the platform-wide reconcile sweep. It takes
//     no parameters at all: there is no resource to name, in the path or the body, so
//     the catalog decision is the whole question. NOT guarded, and nothing to guard.
var catalogScopedGolden = map[string][]string{
	"delivery":   {"query-project-view", "start-project"},
	"operations": {"reconcile-operated-state"},
}

// compositionRootGuards names the receiver whose methods re-authorize a body-carried id,
// and the delivery package whose catalog set they must exactly cover.
const (
	guardReceiver   = "projectScopedDeliveryManager"
	guardedPackage  = "delivery"
	guardSourceFile = "cmd/server/hooks.go"
)

// authzSite is one generated handler's authorization decision.
type authzSite struct {
	handler   string // the Go func name, e.g. handleStartProject
	verb      string // security.Action{Verb: …}
	kind      string // security.ResourceRef{Kind: …}
	isCatalog bool
}

func TestCatalogScopedOpsArePinned(t *testing.T) {
	byPackage := parseWebHandlerAuthz(t)

	if len(byPackage) == 0 {
		t.Fatal("vacuity: no generated web handler package was found under " + webHandlerRoot +
			" — this gate asserted nothing")
	}

	// A package the golden does not know is a FAILURE, not a skip: a new web manager
	// arrives with its own catalog fallback and must be looked at.
	for pkg := range byPackage {
		if _, ok := catalogScopedGolden[pkg]; !ok {
			t.Errorf("web handler package %q has no entry in catalogScopedGolden — a new "+
				"generated client surface must declare its catalog-scoped op set here, with "+
				"the reason each op names no resource", pkg)
		}
	}
	for pkg := range catalogScopedGolden {
		if _, ok := byPackage[pkg]; !ok {
			t.Errorf("catalogScopedGolden names package %q, which no longer exists under %s — "+
				"remove the stale entry", pkg, webHandlerRoot)
		}
	}

	for pkg, sites := range byPackage {
		want, ok := catalogScopedGolden[pkg]
		if !ok {
			continue // already reported above
		}
		if len(sites) == 0 {
			t.Errorf("vacuity: package %q yielded zero authorization sites", pkg)
			continue
		}

		var got []string
		for _, s := range sites {
			// Every handler must authorize SOMETHING. A handler reaching the Manager with
			// no decision at all is the hole this whole family exists to prevent.
			if s.verb == "" || s.kind == "" {
				t.Errorf("%s.%s authorizes nothing (verb=%q kind=%q) — every generated "+
					"handler carries exactly one Action + one ResourceRef", pkg, s.handler, s.verb, s.kind)
				continue
			}
			if s.isCatalog {
				got = append(got, s.verb)
			}
		}
		sort.Strings(got)

		if !slices.Equal(got, want) {
			t.Errorf("package %q catalog-scoped ops = %v, want %v.\n"+
				"An op authorized against {Kind:%q} asks \"may this principal call this verb at "+
				"all\", never \"may it touch THAT resource\". If the new op names a resource in "+
				"its BODY (the id is not a path param), it must ALSO get an arm on %s in %s — "+
				"see the two stage-4a regressions this gate was written for. If it genuinely "+
				"names nothing, add it to catalogScopedGolden with that reason.",
				pkg, got, want, pkg+"Catalog", guardReceiver, guardSourceFile)
		}
	}
}

// TestCompositionRootGuardsEveryCatalogScopedDeliveryOp closes the other half: the guard
// and the generated surface must name the SAME ops. A body id added without an arm fails
// here even if someone remembers to update catalogScopedGolden; an arm left behind after
// an op moves back onto the path fails here too.
func TestCompositionRootGuardsEveryCatalogScopedDeliveryOp(t *testing.T) {
	guarded := parseGuardedMethods(t)
	if len(guarded) == 0 {
		t.Fatalf("vacuity: no method with receiver %s was found in %s — this gate asserted nothing",
			guardReceiver, hooksPath)
	}

	var got []string
	for _, method := range guarded {
		got = append(got, kebabOpName(method))
	}
	sort.Strings(got)

	want := slices.Clone(catalogScopedGolden[guardedPackage])
	sort.Strings(want)

	if !slices.Equal(got, want) {
		t.Errorf("%s guards %v, but the delivery catalog-scoped op set is %v.\n"+
			"These must match exactly: an op the generator authorizes against the catalog is an "+
			"op whose own resource (if it has one) nothing binds, and the guard is what binds it. "+
			"Missing here = a body-carried id inherited the catalog decision silently; extra here "+
			"= the op went back onto the path and the arm is now dead weight asking the same "+
			"question twice.", guardReceiver, got, want)
	}
}

// parseWebHandlerAuthz reads every generated *_handlers.gen.go under client/web and
// returns, per package directory name, one authzSite for each handler func.
//
// go/ast over the generated SOURCE rather than packages.Load: the assertion is about the
// literals the GENERATOR emitted (which resource ref it chose for which verb), and those
// are syntax. Loading types would also make this gate depend on the whole client tree
// compiling, which is a different test's job.
func parseWebHandlerAuthz(t *testing.T) map[string][]authzSite {
	t.Helper()

	entries, err := os.ReadDir(webHandlerRoot)
	if err != nil {
		t.Fatalf("read %s: %v", webHandlerRoot, err)
	}

	out := make(map[string][]authzSite)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(webHandlerRoot, entry.Name())
		files, err := filepath.Glob(filepath.Join(dir, "*_handlers.gen.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		for _, path := range files {
			out[entry.Name()] = append(out[entry.Name()], parseHandlerFile(t, path)...)
		}
	}
	return out
}

func parseHandlerFile(t *testing.T, path string) []authzSite {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var sites []authzSite
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "handle") {
			continue
		}
		site := authzSite{handler: fn.Name.Name}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Action":
				site.verb = stringFieldOf(lit, "Verb")
			case "ResourceRef":
				site.kind = stringFieldOf(lit, "Kind")
			}
			return true
		})
		site.isCatalog = strings.HasSuffix(site.kind, "Catalog")
		sites = append(sites, site)
	}
	if len(sites) == 0 {
		t.Fatalf("vacuity: %s yielded no handler funcs", path)
	}
	return sites
}

// stringFieldOf returns the STRING-LITERAL value of one field of a composite literal, or
// "" when the field is absent or computed. A computed Kind would itself be a finding —
// the convention emits literals — and reads here as "authorizes nothing", which the
// caller reports.
func stringFieldOf(lit *ast.CompositeLit, field string) string {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != field {
			continue
		}
		bl, ok := kv.Value.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return ""
		}
		v, err := strconv.Unquote(bl.Value)
		if err != nil {
			return ""
		}
		return v
	}
	return ""
}

// parseGuardedMethods returns the method names declared on the composition root's
// project-scoped guard receiver.
func parseGuardedMethods(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, hooksPath, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", hooksPath, err)
	}

	// receiverTypeName is paramguard_arch_test.go's — same package, same question.
	var methods []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if receiverTypeName(fn) == guardReceiver {
			methods = append(methods, fn.Name.Name)
		}
	}
	return methods
}

// kebabOpName renders a contract op name the way the http generator does when it derives
// a verb (projectmodel.Kebab: lowercase, '-' before each interior capital). Re-implemented
// in three lines rather than imported so this gate depends on no generator package —
// every op name in this contract is plain CamelCase with no initialism.
func kebabOpName(op string) string {
	var b strings.Builder
	for i, r := range op {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
