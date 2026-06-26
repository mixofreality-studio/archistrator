// cmd/schemagen reflects each component's contract surface into a single
// self-contained contract document per component — the SEED for schema-first
// codegen (the strangler bootstrap: capture today's hand-written Go as the
// contract, then flip so the contract is the source of truth and the Go is
// generated).
//
// It captures TWO things per component:
//   - the I/O MODEL types → JSON Schema `$defs` (data shapes).
//   - the component INTERFACE → an `interface` descriptor (the RPC surface that
//     JSON Schema can't express: operations, params, result, error).
//
// Design rules (founder direction):
//   - ONE contract document per component, colocated as `contract.schema.json`.
//   - Defs may be SHARED WITHIN a component (`$ref` into `#/$defs`) but NEVER
//     BETWEEN components — each document carries only its own component's types.
//   - Param names (which Go reflection does not expose) are recovered from the
//     interface's source via go/ast.
//
// Usage:
//
//	cd server && go run ./cmd/schemagen          # write every registered component
//	cd server && go run ./cmd/schemagen review   # write only the named component(s)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"

	"github.com/mixofreality-studio/archistrator/server/cmd/internal/codegen"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/settlement"
)

// component declares one component's contract surface to capture.
type component struct {
	name      string
	dir       string
	models    []any        // zero-value instances of the I/O model types
	ifaceName string       // the interface's Go type name (for AST param-name lookup)
	iface     reflect.Type // the interface type, reflected for operations
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

// contextCarried are the cross-cutting types delivered by the per-layer call
// Context (the first param the generator prepends to every method), NOT data.
// schemagen strips any param of these types so the data schema stays pure and the
// generator can re-inject the single layer Context. Includes the layer Context
// types themselves so re-reflecting an already-generated interface is idempotent.
var contextCarried = map[reflect.Type]bool{
	reflect.TypeOf((*context.Context)(nil)).Elem(): true,
	reflect.TypeOf(security.SecurityPrincipal{}):   true,
	reflect.TypeOf(fwra.IdempotencyKey("")):        true,
	reflect.TypeOf(fweng.Context{}):                true,
	reflect.TypeOf(fwra.Context{}):                 true,
}

// layerFromDir maps a component dir to its Method layer (selects the call Context).
func layerFromDir(dir string) string {
	switch {
	case strings.Contains(dir, "/engine/"):
		return "engine"
	case strings.Contains(dir, "/resourceaccess/"):
		return "resourceaccess"
	case strings.Contains(dir, "/manager/"):
		return "manager"
	case strings.Contains(dir, "/client/"):
		return "client"
	}
	return ""
}

// wellKnownByType maps foundational non-domain types (stdlib/uuid) to a schema
// carrying a portable JSON shape (e.g. string+format) AND an `x-go-type` /
// `x-go-import` binding so modelgen regenerates the exact Go type. These are NOT
// inlined as $defs — they bind directly to their canonical Go type (and the
// generated file imports it). Unlike domain types, importing `time`/`uuid` in
// generated code is fine; they're foundational, not cross-component contract defs.
var wellKnownByType = map[reflect.Type]*jsonschema.Schema{
	reflect.TypeOf(time.Time{}): {
		Type: "string", Format: "date-time",
		Extra: map[string]any{"x-go-type": "time.Time", "x-go-import": "time"},
	},
	reflect.TypeOf(time.Duration(0)): {
		Type:  "integer",
		Extra: map[string]any{"x-go-type": "time.Duration", "x-go-import": "time"},
	},
	reflect.TypeOf(uuid.UUID{}): {
		Type: "string", Format: "uuid",
		Extra: map[string]any{"x-go-type": "uuid.UUID", "x-go-import": "github.com/google/uuid"},
	},
	// []byte → base64 string on the wire (matches Go's encoding/json).
	reflect.TypeOf([]byte(nil)): {
		Type: "string", ContentEncoding: "base64",
		Extra: map[string]any{"x-go-type": "[]byte"},
	},
	// json.RawMessage → arbitrary embedded JSON (no fixed shape).
	reflect.TypeOf(json.RawMessage(nil)): {
		Extra: map[string]any{"x-go-type": "json.RawMessage", "x-go-import": "encoding/json"},
	},
}

// modulePrefix maps a type's PkgPath back to a dir relative to the server module
// root (where the codegen tools run), so enum const sets can be captured from the
// defining package of an inlined external domain type.
const modulePrefix = "github.com/mixofreality-studio/archistrator/server/"

// externalDirs returns the module-relative dirs of any model types defined outside
// the component's own package (e.g. projectstate domain types), deduped.
func externalDirs(models []any) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, m := range models {
		pkg := reflect.TypeOf(m).PkgPath()
		if d := strings.TrimPrefix(pkg, modulePrefix); d != pkg && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// registry is the strangler work-list, leaf-first. engine/review is the first
// leaf (pure, zero importers, nothing persisted — see review.go package doc).
var registry = []component{
	{
		name: "review",
		dir:  "internal/engine/review",
		models: []any{
			review.ReviewChange{},
			review.Reviewer{},
			review.ReviewSet{},
		},
		ifaceName: "ReviewEngine",
		iface:     reflect.TypeOf((*review.ReviewEngine)(nil)).Elem(),
	},
	{
		name: "handoff",
		dir:  "internal/engine/handoff",
		models: []any{
			handoff.ConstructionActivity{},
			handoff.HandOffPolicy{},
			handoff.WorkerClass(0),
			handoff.ActivityKind(0),
		},
		ifaceName: "HandOffEngine",
		iface:     reflect.TypeOf((*handoff.HandOffEngine)(nil)).Elem(),
	},
	{
		name: "intervention",
		dir:  "internal/engine/intervention",
		models: []any{
			// Identifier newtypes (named strings; no const block).
			intervention.ProjectID(""),
			intervention.ActivityID(""),
			intervention.OperatedAppID(""),
			intervention.CustomerID(""),
			intervention.CycleID(""),
			intervention.PipelineRef(""),
			intervention.NotifyTarget(""),
			// Policy + I/O structs.
			intervention.InterventionPolicy{},
			intervention.ConstructionVariance{},
			intervention.HealthChange{},
			intervention.SettlementFailure{},
			intervention.PauseRequestContext{},
			intervention.PausePlan{},
			// Enums (one zero value each).
			intervention.InterventionMode(0),
			intervention.SLATier(0),
			intervention.VarianceKind(0),
			intervention.Severity(0),
			intervention.HealthStatus(0),
			intervention.SLOStatus(0),
			intervention.SettlementFailureKind(0),
			intervention.VarianceDirective(0),
			intervention.HealthDirective(0),
			intervention.SettlementFailureDirective(0),
		},
		ifaceName: "InterventionEngine",
		iface:     reflect.TypeOf((*intervention.InterventionEngine)(nil)).Elem(),
	},
	{
		name: "settlement",
		dir:  "internal/engine/settlement",
		models: []any{
			// Own I/O structs.
			settlement.CycleRevenue{},
			settlement.CycleUsage{},
			settlement.SettlementResult{},
			settlement.ReSettlementInput{},
			settlement.Projection{},
			settlement.ProjectOption{},
			// Own enums + scalar.
			settlement.RoutingDirective(0),
			settlement.OptionID(""),
			// Domain types redefined as this component's OWN defs (Option B full
			// encapsulation): Money/SettlementTerms structs plus the settlement-terms
			// enums they carry. These MIRROR projectstate (the canonical home owned by
			// projectStateAccess); the projectDesignManager converts at the call
			// boundary. They are registered as the component's own settlement.* types
			// (post-seed steady state) so nested field refs resolve and the contract
			// regenerates identically — idempotent (the enum const blocks are captured
			// from contract.gen.go in this dir, not from projectstate).
			settlement.Money{},
			settlement.SettlementTerms{},
			settlement.RevenueShareKind(0),
			settlement.ComputeCostKind(0),
			settlement.ScheduleKind(0),
		},
		ifaceName: "SettlementEngine",
		iface:     reflect.TypeOf((*settlement.SettlementEngine)(nil)).Elem(),
	},
	{
		name: "billing",
		dir:  "internal/engine/billing",
		models: []any{
			// Own I/O structs (the full transitive closure of BillingEngine).
			billing.PeriodUsage{},
			billing.ServicePricing{},
			billing.ProjectOption{},
			billing.ServiceInvoice{},
			billing.HostingRate{},
			billing.ServiceCostProjection{},
			// Own enum + named scalars.
			billing.ServicePricingKind(0),
			billing.CustomerID(""),
			billing.PeriodID(""),
			// Domain types redefined as this component's OWN defs (Option B full
			// encapsulation): Money struct + OptionID scalar. They MIRROR
			// projectstate (the canonical home owned by projectStateAccess); the
			// calling Managers convert at the call boundary. Registered as the
			// component's own billing.* types (post-seed steady state) so nested
			// field refs resolve and the contract regenerates identically —
			// idempotent (a projectstate.* registration would inline Money to
			// map[string]interface{} on re-run, because the component's own structs
			// reference the generated-local billing.Money, a different reflect.Type).
			billing.Money{},
			billing.OptionID(""),
		},
		ifaceName: "BillingEngine",
		iface:     reflect.TypeOf((*billing.BillingEngine)(nil)).Elem(),
	},
	{
		name: "operationestimation",
		dir:  "internal/engine/operationestimation",
		models: []any{
			// Own output value objects (the full transitive closure of the two ops).
			operationestimation.CostCurvePoint{},
			operationestimation.UsageCostCurve{},
			operationestimation.PayoutShortfallForecast{},
			operationestimation.OperationForecast{},
			operationestimation.ObservedUsage{},
			operationestimation.ScalePoint{},
			operationestimation.WhatIfPoint{},
			operationestimation.WhatIfCurve{},
			operationestimation.CostProjection{},
			// Domain types redefined as this component's OWN defs (Option B full
			// encapsulation): Money/SettlementTerms/UsageAssumption structs + the slim
			// ProjectOption, plus the InfrastructureKind/settlement-terms enums and the
			// OptionID scalar. They MIRROR projectstate (the canonical home owned by
			// projectStateAccess); the projectDesignManager converts at the call
			// boundary. Registered as the component's own operationestimation.* types
			// (post-seed steady state) so nested field refs resolve and the contract
			// regenerates identically — idempotent (a projectstate.* registration would
			// inline Money to map[string]interface{} on re-run, because the component's
			// own structs reference the generated-local operationestimation.Money, a
			// different reflect.Type).
			operationestimation.Money{},
			operationestimation.SettlementTerms{},
			operationestimation.UsageAssumption{},
			operationestimation.ProjectOption{},
			operationestimation.InfrastructureKind(0),
			operationestimation.RevenueShareKind(0),
			operationestimation.ComputeCostKind(0),
			operationestimation.ScheduleKind(0),
			operationestimation.OptionID(""),
		},
		ifaceName: "OperationEstimationEngine",
		iface:     reflect.TypeOf((*operationestimation.OperationEstimationEngine)(nil)).Elem(),
	},
	{
		name: "estimation",
		dir:  "internal/engine/estimation",
		models: []any{
			// Output value objects (owned by this Engine — computation results, not
			// persisted head-state). EstimateForOption → ConstructionEstimate{RiskScore};
			// ComputeNetwork → NetworkSolution{NetworkNode, NetworkMilestoneSolution,
			// NetworkSummary}.
			estimation.ConstructionEstimate{},
			estimation.RiskScore{},
			estimation.NetworkSolution{},
			estimation.NetworkNode{},
			estimation.NetworkMilestoneSolution{},
			estimation.NetworkSummary{},
			// Domain INPUT types redefined as this component's OWN defs (Option B full
			// encapsulation). They MIRROR projectstate (the canonical home owned by
			// projectStateAccess); the projectDesignManager (EstimateForOption) and the
			// projectManager (ComputeNetwork) convert at the call boundary. Registered as
			// the component's OWN estimation.* types (NOT projectstate.*) so nested field
			// refs resolve and the contract regenerates identically — idempotent (a
			// projectstate.* registration would inline WorkerMix.ClassRates' Money value to
			// map[string]interface{} on re-run, because the component's own structs
			// reference the generated-local estimation.Money, a different reflect.Type).
			// Per the settlement/billing/operationestimation precedent ProjectOption /
			// Network / ActivityItem are SLIM (only the fields THIS Engine reads).
			estimation.ProjectOption{},
			estimation.ActivityNetwork{},
			estimation.OptionActivity{},
			estimation.WorkerMix{},
			estimation.Money{},
			estimation.ActivityList{},
			estimation.ActivityItem{},
			estimation.Network{},
			estimation.NetworkDependency{},
			estimation.NetworkMilestone{},
			// Named scalar (no const block — a bare identifier newtype, carried for audit).
			estimation.OptionID(""),
		},
		ifaceName: "EstimationEngine",
		iface:     reflect.TypeOf((*estimation.EstimationEngine)(nil)).Elem(),
	},
	{
		name: "autoscaler",
		dir:  "internal/engine/autoscaler",
		models: []any{
			// Own I/O structs (the full transitive closure of ProposeDesiredState).
			autoscaler.Telemetry{},
			autoscaler.DesiredState{},
			autoscaler.AutoscalerPolicy{},
			autoscaler.DecisionReason{},
			autoscaler.Decision{},
			// Own enums (one zero value each). InfrastructureKind is redefined as
			// this component's OWN type (Option B full encapsulation) mirroring
			// projectstate's values/names, so the generated def imports no
			// projectstate; callers convert at the boundary. NOTE: OperatedAppID is a
			// `= uuid.UUID` alias, so fields of that type bind to uuid.UUID
			// automatically (wellKnownByType) — it is NOT registered separately.
			autoscaler.HealthStatus(0),
			autoscaler.SLOStatus(0),
			autoscaler.AutoscalerMode(0),
			autoscaler.SLATier(0),
			autoscaler.DecisionKind(0),
			autoscaler.ReasonCode(0),
			autoscaler.InfrastructureKind(0),
		},
		ifaceName: "AutoscalerEngine",
		iface:     reflect.TypeOf((*autoscaler.AutoscalerEngine)(nil)).Elem(),
	},
}

func main() {
	want := map[string]bool{}
	for _, a := range os.Args[1:] {
		want[a] = true
	}
	for _, c := range registry {
		if len(want) > 0 && !want[c.name] {
			continue
		}
		if err := writeComponent(c); err != nil {
			fmt.Fprintf(os.Stderr, "schemagen %s: %v\n", c.name, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "wrote %s/contract.schema.json (%d defs, iface %s)\n", c.dir, len(c.models), c.ifaceName)
	}
}

func writeComponent(c component) error {
	modelNames := map[string]bool{}
	for _, m := range c.models {
		modelNames[reflect.TypeOf(m).Name()] = true
	}

	// refForType maps each model type to a local `$ref` stub, so a model nested in
	// another model renders as a reference rather than being inlined.
	refForType := map[reflect.Type]*jsonschema.Schema{}
	for _, m := range c.models {
		t := reflect.TypeOf(m)
		refForType[t] = &jsonschema.Schema{Ref: "#/$defs/" + t.Name()}
	}

	// Capture enum const sets from the component's own dir AND from the defining
	// package of any referenced type (Option B: a component inlines external domain
	// types — incl. their enums — as its OWN defs, so generated code imports nothing).
	enums := map[string]enumInfo{}
	for _, dir := range append([]string{c.dir}, externalDirs(c.models)...) {
		captured, err := captureEnums(dir)
		if err != nil {
			return fmt.Errorf("capture enums in %s: %w", dir, err)
		}
		for k, v := range captured {
			enums[k] = v
		}
	}

	defs := map[string]*jsonschema.Schema{}
	for _, m := range c.models {
		t := reflect.TypeOf(m)
		// A named scalar with a captured const set is an enum, not a struct.
		if t.Kind() != reflect.Struct {
			if e, ok := enums[t.Name()]; ok {
				defs[t.Name()] = enumSchema(e)
				continue
			}
		}
		siblings := map[reflect.Type]*jsonschema.Schema{}
		for rt, ws := range wellKnownByType {
			siblings[rt] = ws
		}
		for rt, ref := range refForType {
			if rt != t {
				siblings[rt] = ref
			}
		}
		s, err := jsonschema.ForType(t, &jsonschema.ForOptions{
			IgnoreInvalidTypes: true,
			TypeSchemas:        siblings,
		})
		if err != nil {
			return fmt.Errorf("reflect model %s: %w", t.Name(), err)
		}
		defs[t.Name()] = s
	}

	doc := &jsonschema.Schema{
		Schema: "https://json-schema.org/draft/2020-12/schema",
		ID:     "archistrator://contract/" + c.name,
		Title:  c.name + " contract",
		Defs:   defs,
	}

	if c.iface != nil {
		iface, err := reflectInterface(c, modelNames)
		if err != nil {
			return fmt.Errorf("reflect interface %s: %w", c.ifaceName, err)
		}
		doc.Extra = map[string]any{"interface": iface}
	}

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	raw = append(raw, '\n')

	out := c.dir + "/contract.schema.json"
	if err := os.WriteFile(out, raw, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	return nil
}

// reflectInterface enumerates the interface's methods into operation descriptors,
// mapping each param/result type to a schema (`$ref` for model types, inline for
// primitives/arrays) and recovering param names from source.
func reflectInterface(c component, modelNames map[string]bool) (codegen.Interface, error) {
	names, err := paramNames(c.dir, c.ifaceName)
	if err != nil {
		return codegen.Interface{}, err
	}
	out := codegen.Interface{Name: c.ifaceName, Layer: layerFromDir(c.dir)}
	for i := 0; i < c.iface.NumMethod(); i++ {
		m := c.iface.Method(i)
		ft := m.Type // interface method type has no receiver
		op := codegen.Operation{Name: m.Name}
		pn := names[m.Name]
		for j := 0; j < ft.NumIn(); j++ {
			// Skip cross-cutting params delivered by the layer Context (ctx,
			// principal, idempotency, or an already-prepended layer Context); the
			// generator re-injects a single Context. Keeps the data schema pure.
			if contextCarried[ft.In(j)] {
				continue
			}
			nm := fmt.Sprintf("arg%d", j)
			if j < len(pn) {
				nm = pn[j]
			}
			op.Params = append(op.Params, codegen.Param{Name: nm, Schema: schemaForType(ft.In(j), modelNames)})
		}
		for j := 0; j < ft.NumOut(); j++ {
			ot := ft.Out(j)
			if ot == errorType {
				op.Error = true
				continue
			}
			op.Result = schemaForType(ot, modelNames)
		}
		out.Operations = append(out.Operations, op)
	}
	return out, nil
}

// schemaForType maps a Go type to a JSON Schema node: a `$ref` for model types,
// an array schema for slices, otherwise jsonschema-go's reflected schema.
func schemaForType(rt reflect.Type, modelNames map[string]bool) *jsonschema.Schema {
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	if ws, ok := wellKnownByType[rt]; ok {
		return ws
	}
	if rt.Name() != "" && modelNames[rt.Name()] {
		return &jsonschema.Schema{Ref: "#/$defs/" + rt.Name()}
	}
	if rt.Kind() == reflect.Slice {
		return &jsonschema.Schema{Type: "array", Items: schemaForType(rt.Elem(), modelNames)}
	}
	s, err := jsonschema.ForType(rt, &jsonschema.ForOptions{IgnoreInvalidTypes: true})
	if err != nil || s == nil {
		return &jsonschema.Schema{}
	}
	return s
}

// enumInfo is a captured named-scalar enum: its underlying Go base type and the
// ordered (const-name, value) pairs of its members.
type enumInfo struct {
	base   string
	names  []string
	values []any
}

// enumSchema renders a captured enum as a JSON Schema `enum` with the Go binding
// metadata modelgen needs to regenerate the type + const block.
func enumSchema(e enumInfo) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Enum:  e.values,
		Extra: map[string]any{"x-go-base": e.base, "x-enum-varnames": e.names},
	}
	switch {
	case e.base == "string":
		s.Type = "string"
	case strings.HasPrefix(e.base, "float"):
		s.Type = "number"
	default:
		s.Type = "integer"
	}
	return s
}

// captureEnums parses a package's source and returns its named-scalar enum types
// (a `type X <scalar>` plus a const block binding members to X). Handles the
// dominant Go idioms: `= iota` runs and explicit int/string literals.
func captureEnums(dir string) (map[string]enumInfo, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, err
	}
	base := map[string]string{} // type name -> scalar base, for named scalars
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, sp := range gd.Specs {
					ts := sp.(*ast.TypeSpec)
					if id, ok := ts.Type.(*ast.Ident); ok && isScalarBase(id.Name) {
						base[ts.Name.Name] = id.Name
					}
				}
			}
		}
	}
	enums := map[string]enumInfo{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				curType := ""
				for iota, sp := range gd.Specs {
					vs := sp.(*ast.ValueSpec)
					if vs.Type != nil {
						if id, ok := vs.Type.(*ast.Ident); ok {
							curType = id.Name
						}
					}
					b, isEnum := base[curType]
					if !isEnum {
						continue
					}
					for _, nameID := range vs.Names {
						e := enums[curType]
						e.base = b
						e.names = append(e.names, nameID.Name)
						e.values = append(e.values, evalConst(vs, iota, b))
						enums[curType] = e
					}
				}
			}
		}
	}
	return enums, nil
}

// evalConst resolves a const member's value: an explicit int/string literal, or
// the iota index for `= iota` runs (and bare continuation specs).
func evalConst(vs *ast.ValueSpec, iota int, base string) any {
	if len(vs.Values) == 1 {
		switch v := vs.Values[0].(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if s, err := strconv.Unquote(v.Value); err == nil {
					return s
				}
				return v.Value
			}
			if n, err := strconv.Atoi(v.Value); err == nil {
				return n
			}
		}
	}
	if base == "string" {
		return ""
	}
	return iota
}

func isScalarBase(name string) bool {
	switch name {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "byte", "rune",
		"float32", "float64", "string", "bool":
		return true
	}
	return false
}

// paramNames parses the package source and returns, per method of ifaceName, the
// ordered parameter names (Go reflection does not expose them).
func paramNames(dir, ifaceName string) (map[string][]string, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, err
	}
	res := map[string][]string{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok || ts.Name.Name != ifaceName {
					return true
				}
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					return true
				}
				for _, fld := range it.Methods.List {
					if len(fld.Names) == 0 {
						continue
					}
					ft, ok := fld.Type.(*ast.FuncType)
					if !ok || ft.Params == nil {
						continue
					}
					var pn []string
					for i, p := range ft.Params.List {
						if len(p.Names) == 0 {
							pn = append(pn, fmt.Sprintf("arg%d", i))
							continue
						}
						for _, nm := range p.Names {
							pn = append(pn, nm.Name)
						}
					}
					res[fld.Names[0].Name] = pn
				}
				return false
			})
		}
	}
	return res, nil
}
