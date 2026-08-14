package internal_test

// paramguard_arch_test.go is the standing gate for one invariant the JSON Schema
// cannot express and therefore nothing else enforces:
//
//	A Manager operation must INSPECT every required string it is handed.
//
// JSON Schema `required` is a PRESENCE check — `""` satisfies it — and the contract
// corpus deliberately carries no minLength/pattern (founder ruling 2026-08-13:
// non-emptiness belongs in Go, where the meaning lives, not in the schema). That
// division only holds if the Go side actually does its half. The 2026-08-13
// contract-strictness audit found three operations where it did not, each silently
// harmful rather than loudly broken:
//
//   - constructionManager.SubmitPhaseDecision's `phase` — signalled through to a
//     child workflow's phase gate, where "" could never match a real phase, so the
//     decision was a no-op that looked like a success.
//   - {systemDesign,projectDesign}Manager.AcknowledgeStaleBasis's `note` — the
//     reviewer's durable justification, which also KEYS the ack's idempotency, so
//     every blank ack collapsed onto one key.
//
// WHAT THIS CHECKS, HONESTLY: that the operation's body BRANCHES on the parameter
// (or, for an object parameter, on its schema-required string field) — an `if` or a
// `switch`, directly or through a variable derived from it. That is a PROXY for "it
// is validated", not a proof: a branch taken for another reason passes. It is a
// floor, and the three defects above cleared no branch at all. Nothing here inspects
// what the guard concludes; the per-manager tests do that.
//
// The contract corpus is the source of truth for WHICH params are required
// (a param is required iff it is not `"pointer": true`), so this gate tracks the
// contracts automatically — a newly authored required string arrives already
// covered.

import (
	"encoding/json"
	"go/ast"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// projectDocPath is the committed contract corpus, relative to server/internal.
const projectDocPath = "../../.aiarch/state/project.json"

// managerDirs maps a contract component key to the package directory implementing it.
var managerDirs = map[string]string{
	"billingManager":       "manager/billing",
	"constructionManager":  "manager/construction",
	"operationsManager":    "manager/operations",
	"projectDesignManager": "manager/projectdesign",
	"systemDesignManager":  "manager/systemdesign",
}

// requiredString names one required string a Manager operation must inspect: either
// a top-level param, or a schema-required string FIELD of a required object param
// (`override.notes`, `event.amount.currency`). want is what the body must reference —
// matched case-insensitively on the dotted tail, because Go initialisms ("GatewayEventID")
// and wire names ("gatewayEventId") differ only in case.
type requiredString struct {
	op   string
	want string
}

func TestManagerRequiredStringsAreInspected(t *testing.T) {
	doc := loadContractDoc(t)
	var checked int
	for component, dir := range managerDirs {
		contract, ok := doc.ServiceContracts[component]
		if !ok {
			t.Errorf("%s: no contract in %s — managerDirs is stale", component, projectDocPath)
			continue
		}
		wants := requiredStringsFor(contract)
		if len(wants) == 0 {
			t.Errorf("%s: no required strings found; this gate would check air for it", component)
			continue
		}
		bodies := managerOpBodies(t, dir)
		for _, w := range wants {
			body, found := bodies[w.op]
			if !found {
				t.Errorf("%s.%s: contract declares the operation but %s has no such method", component, w.op, dir)
				continue
			}
			checked++
			if !inspectsExpr(body, w.want) {
				t.Errorf("%s.%s: required string %q is never branched on — schema `required` only proves the key was SENT, so \"\" reaches the implementation unchallenged; guard it (see paramguard_arch_test.go)",
					component, w.op, w.want)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no required strings were checked; this gate would pass vacuously")
	}
	t.Logf("checked %d required string(s) across %d manager components", checked, len(managerDirs))
}

// ---------------------------------------------------------------------------
// Contract corpus reading.
// ---------------------------------------------------------------------------

type contractDoc struct {
	ServiceContracts map[string]contractComponent `json:"serviceContracts"`
}

type contractComponent struct {
	Defs      map[string]schemaNode `json:"$defs"`
	Interface struct {
		Operations []struct {
			Name   string `json:"name"`
			Params []struct {
				Name    string     `json:"name"`
				Pointer bool       `json:"pointer"`
				Schema  schemaNode `json:"schema"`
			} `json:"params"`
		} `json:"operations"`
	} `json:"interface"`
}

// schemaNode is the sliver of JSON Schema this gate reads. Type is json.RawMessage
// because it is a string OR an array of strings (the null-union spelling).
type schemaNode struct {
	Type       json.RawMessage       `json:"type"`
	Ref        string                `json:"$ref"`
	Required   []string              `json:"required"`
	Properties map[string]schemaNode `json:"properties"`
	// GoType is the x-go-type override. A schema-string that decodes to a Go
	// time.Time is not a Go string, so "" is not one of its values and there is
	// nothing for an emptiness guard to check.
	GoType string `json:"x-go-type"`
}

func (s schemaNode) isString() bool {
	if s.GoType != "" && s.GoType != "string" {
		return false
	}
	return s.hasType("string")
}

func (s schemaNode) hasType(want string) bool {
	if len(s.Type) == 0 {
		return false
	}
	var one string
	if err := json.Unmarshal(s.Type, &one); err == nil {
		return one == want
	}
	var many []string
	if err := json.Unmarshal(s.Type, &many); err != nil {
		return false
	}
	// A null-union is by definition allowed to be absent; it is not a required string.
	if slices.Contains(many, "null") {
		return false
	}
	return slices.Contains(many, want)
}

// defName reads the local def name out of a "#/$defs/X" reference.
func defName(ref string) string { return strings.TrimPrefix(ref, "#/$defs/") }

func loadContractDoc(t *testing.T) contractDoc {
	t.Helper()
	raw, err := os.ReadFile(projectDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", projectDocPath, err)
	}
	var doc contractDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", projectDocPath, err)
	}
	if len(doc.ServiceContracts) == 0 {
		t.Fatalf("%s carries no service contracts", projectDocPath)
	}
	return doc
}

// requiredStringsFor collects every required string a component's operations take:
// non-pointer string params, plus the required string fields of non-pointer object
// params (one level deep, following $refs — deep enough for money.currency).
func requiredStringsFor(c contractComponent) []requiredString {
	var out []requiredString
	for _, op := range c.Interface.Operations {
		for _, p := range op.Params {
			if p.Pointer {
				continue // optional by contract; nil is a legitimate value
			}
			schema := resolve(c, p.Schema)
			if schema.isString() {
				out = append(out, requiredString{op: op.Name, want: p.Name})
				continue
			}
			for _, field := range requiredStringFields(c, schema, p.Name, 0) {
				out = append(out, requiredString{op: op.Name, want: field})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].op != out[j].op {
			return out[i].op < out[j].op
		}
		return out[i].want < out[j].want
	})
	return out
}

// resolve follows a $ref into the component's own $defs. A ref this component does
// not define resolves to the empty node (checked as "not a string").
func resolve(c contractComponent, s schemaNode) schemaNode {
	if s.Ref == "" {
		return s
	}
	return c.Defs[defName(s.Ref)]
}

// requiredStringFields walks an object schema's REQUIRED string properties,
// returning dotted paths rooted at prefix. Bounded to two levels: deep enough for
// the nested money value, shallow enough that the gate stays a readable statement
// about an operation's own arguments.
func requiredStringFields(c contractComponent, s schemaNode, prefix string, depth int) []string {
	if depth > 1 {
		return nil
	}
	var out []string
	for _, name := range s.Required {
		prop, ok := s.Properties[name]
		if !ok {
			continue
		}
		resolved := resolve(c, prop)
		path := prefix + "." + name
		if resolved.isString() {
			out = append(out, path)
			continue
		}
		out = append(out, requiredStringFields(c, resolved, path, depth+1)...)
	}
	return out
}

// ---------------------------------------------------------------------------
// Go body reading.
// ---------------------------------------------------------------------------

// managerOpBodies loads dir and returns every METHOD body by method name. Methods
// on any receiver are included: a Manager package holds exactly one Manager type,
// and a name collision would only make the gate stricter. packages.Load (not
// parser.ParseDir) so build tags are honoured, matching the posture of the other
// arch tests in this package.
func managerOpBodies(t *testing.T, dir string) map[string]*ast.BlockStmt {
	t.Helper()
	cfg := &packages.Config{Mode: packages.NeedSyntax | packages.NeedFiles, Dir: "..", Tests: false}
	pkgs, err := packages.Load(cfg, "./internal/"+dir)
	if err != nil {
		t.Fatalf("packages.Load %s: %v", dir, err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("%s: %d package load error(s); fix the build before checking the guards", dir, n)
	}
	out := map[string]*ast.BlockStmt{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || fn.Body == nil {
					continue
				}
				out[fn.Name.Name] = fn.Body
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: loaded no methods; the gate would pass vacuously for it", dir)
	}
	return out
}

// inspectsExpr reports whether want (an identifier, or a dotted selector path) is
// referenced by a BRANCH in body: an `if` (condition or init statement, so the
// `if err := validate(x); err != nil` idiom counts) or a `switch` (tag or init, the
// idiom closed vocabularies use). References through a variable DERIVED from want
// count too — `psModel := projectstate.OperatingModel(model); if !psModel.Valid()`
// inspects model as surely as if it had named it.
func inspectsExpr(body *ast.BlockStmt, want string) bool {
	names := map[string]bool{strings.ToLower(want): true}
	// Two passes so an alias assigned before its use is seen either way.
	for range 2 {
		collectAliases(body, names)
	}
	return branchReferences(body, names)
}

// collectAliases adds every identifier assigned from an expression that mentions a
// name already in the set — one hop per pass. A COMPOSITE LITERAL is deliberately
// not an alias: `sig := phaseDecisionSignal{Phase: phase}` carries the value toward
// the work, it does not derive something to judge it by, and treating it as an alias
// let a later `if err := m.client.Signal(..., sig)` masquerade as a guard.
func collectAliases(body *ast.BlockStmt, names map[string]bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		lhs, ok := as.Lhs[0].(*ast.Ident)
		if !ok || containsCompositeLit(as.Rhs[0]) || !mentions(as.Rhs[0], names) {
			return true
		}
		names[strings.ToLower(lhs.Name)] = true
		return true
	})
}

func containsCompositeLit(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(inner ast.Node) bool {
		if _, ok := inner.(*ast.CompositeLit); ok {
			found = true
		}
		return !found
	})
	return found
}

// mentions reports whether any expression path inside n is one of names.
func mentions(n ast.Node, names map[string]bool) bool {
	hit := false
	ast.Inspect(n, func(inner ast.Node) bool {
		if hit {
			return false
		}
		if expr, ok := inner.(ast.Expr); ok && names[strings.ToLower(exprPath(expr))] {
			hit = true
			return false
		}
		return true
	})
	return hit
}

// branchReferences reports whether any if/switch in body branches on one of names.
//
// A CONDITION (or switch tag) counts outright. An INIT statement counts only where
// the name is an argument to a PACKAGE-LOCAL function — `if err := validatePhase(phase); err != nil`.
// A call through one of the receiver's dependencies (`m.client.SignalWorkflow(...)`)
// does NOT count however it is spelled: that is the operation doing its work, and
// accepting it would let every unguarded param claim to be guarded by the very call
// that consumes it. That distinction is what a mutation test caught.
func branchReferences(body *ast.BlockStmt, names map[string]bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		var conds, inits []ast.Node
		switch v := n.(type) {
		case *ast.IfStmt:
			conds, inits = []ast.Node{v.Cond}, []ast.Node{v.Init}
		case *ast.SwitchStmt:
			conds, inits = []ast.Node{v.Tag}, []ast.Node{v.Init}
		case *ast.TypeSwitchStmt:
			conds, inits = []ast.Node{v.Assign}, []ast.Node{v.Init}
		default:
			return true
		}
		for _, c := range conds {
			if c != nil && mentions(c, names) {
				found = true
				return false
			}
		}
		for _, i := range inits {
			if i != nil && mentionsInLocalCall(i, names) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// mentionsInLocalCall reports whether one of names is an argument to a call on a
// PLAIN IDENTIFIER function (a package-local helper) anywhere inside n.
func mentionsInLocalCall(n ast.Node, names map[string]bool) bool {
	found := false
	ast.Inspect(n, func(inner ast.Node) bool {
		if found {
			return false
		}
		call, ok := inner.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, isLocal := call.Fun.(*ast.Ident); !isLocal {
			return true // a dependency call: its arguments are work, not judgment
		}
		for _, arg := range call.Args {
			if mentions(arg, names) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// exprPath renders an identifier or selector chain as a dotted path ("event.Amount.Currency"),
// and anything else as "". Pointer derefs are transparent: (*p).X and p.X are the same claim.
func exprPath(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return exprPath(v.X)
	case *ast.ParenExpr:
		return exprPath(v.X)
	case *ast.SelectorExpr:
		base := exprPath(v.X)
		if base == "" {
			return ""
		}
		return base + "." + v.Sel.Name
	default:
		return ""
	}
}
