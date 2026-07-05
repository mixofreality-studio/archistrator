// Package mcpemit is archistrator's IN-REPO MCP tool-surface generator. It
// replaces the emission half of the published framework-go-mcp-generator
// (mcpgen) for this server so the generated tools carry the semantics an agentic
// MCP consumer needs WITHOUT reading server source (QA finding F13):
//
//  1. Real, per-operation tool DESCRIPTIONS (not "Op on the X manager."). The
//     text is contract metadata supplied by the caller (clientgen holds the
//     table); see Options.OpDoc.
//  2. Explicit tool INPUT SCHEMAS that name every integer/string ENUM's allowed
//     values and their meanings (from each $def's `enum` + `x-enum-varnames`),
//     instead of a bare `type: integer`.
//  3. OPTIONALITY / NULLABILITY that matches the REST surface: a parameter is
//     required iff it is a non-pointer param (pointer params — the nil-meaningful
//     ones — are optional and nullable, exactly as the http generator treats
//     them and as the OpenAPI requestBody `required` list shows).
//
// It reuses the published mcp `contract` package for PARSING and Go-type
// resolution (so the emitted input/output structs bind the exact manager
// signatures), and only OWNS the emission so it can enrich it. Output is
// deterministic (operations visited in contract order, which is already sorted
// by schemagen; enum schema helpers emitted in sorted order) so re-running is
// byte-idempotent, and it is gofmt'd before return.
package mcpemit

import (
	"encoding/json"
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/contract"
)

// Options configures a generation run.
type Options struct {
	// Package is the emitted package name (the manager's short package, e.g.
	// "systemdesign").
	Package string
	// ManagerImport is the import path of the manager package whose interface the
	// tools bind.
	ManagerImport string
	// OpDoc returns the human documentation for an operation by its Go method name
	// (e.g. "GetSessionState"). It MUST return a non-empty string for every
	// operation on the contract; Generate errors otherwise so a newly added op can
	// never silently ship with a boilerplate description again.
	OpDoc func(op string) string
}

// Result holds the generator output.
type Result struct {
	// ToolsGo is the gofmt'd Go source registering the enriched MCP tools.
	ToolsGo []byte
}

const (
	frameworkManagerImport = "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	securityImport         = "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	mcpImport              = "github.com/modelcontextprotocol/go-sdk/mcp"
	jsonschemaImport       = "github.com/google/jsonschema-go/jsonschema"
	managerAlias           = "mgr"
)

// enumDef is the parsed enum metadata for one $def that backs a param.
type enumDef struct {
	baseType string   // "integer" | "string"
	values   []string // raw JSON tokens (valid Go literals: 0, 1, "low", ...)
	varnames []string // x-enum-varnames, parallel to values (may be empty)
}

// Generate produces the enriched MCP tool registration source for a contract
// document (the raw .serviceContracts entry).
func Generate(entry json.RawMessage, opts Options) (Result, error) {
	doc, err := contract.Parse(entry)
	if err != nil {
		return Result{}, fmt.Errorf("mcpemit: parse: %w", err)
	}
	enums, err := parseEnumDefs(entry)
	if err != nil {
		return Result{}, err
	}
	src, err := genTools(doc, enums, opts)
	if err != nil {
		return Result{}, err
	}
	return Result{ToolsGo: src}, nil
}

// parseEnumDefs decodes every $def that is a bare integer/string enum into an
// enumDef, keyed by def name.
func parseEnumDefs(entry json.RawMessage) (map[string]enumDef, error) {
	var top struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(entry, &top); err != nil {
		return nil, fmt.Errorf("mcpemit: parse $defs: %w", err)
	}
	out := map[string]enumDef{}
	for name, raw := range top.Defs {
		var def struct {
			Type     string            `json:"type"`
			Enum     []json.RawMessage `json:"enum"`
			Varnames []string          `json:"x-enum-varnames"`
		}
		if err := json.Unmarshal(raw, &def); err != nil {
			continue // not an object we care about
		}
		if len(def.Enum) == 0 {
			continue
		}
		if def.Type != "integer" && def.Type != "string" {
			continue
		}
		vals := make([]string, len(def.Enum))
		for i, v := range def.Enum {
			vals[i] = strings.TrimSpace(string(v))
		}
		out[name] = enumDef{baseType: def.Type, values: vals, varnames: def.Varnames}
	}
	return out, nil
}

func genTools(doc *contract.Doc, enums map[string]enumDef, opts Options) ([]byte, error) {
	if opts.OpDoc == nil {
		return nil, fmt.Errorf("mcpemit: Options.OpDoc is required")
	}
	var b strings.Builder
	mgrPrefix := contract.LowerFirst(doc.ManagerBase())
	iface := doc.Interface.Name

	// Which enum $defs are actually referenced by a param — emit a schema helper
	// only for those, in sorted order for determinism.
	usedEnums := map[string]bool{}
	for _, op := range doc.Interface.Operations {
		for _, p := range op.Params {
			if _, ok := enums[p.Schema.RefName()]; ok {
				usedEnums[p.Schema.RefName()] = true
			}
		}
	}
	enumNames := make([]string, 0, len(usedEnums))
	for n := range usedEnums {
		enumNames = append(enumNames, n)
	}
	sort.Strings(enumNames)

	// --- header + imports ---
	fmt.Fprintf(&b, "// Code generated by cmd/clientgen (mcpemit). DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source contract: %s\n\n", doc.ID)
	fmt.Fprintf(&b, "package %s\n\n", opts.Package)
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"fmt\"\n\n")
	fmt.Fprintf(&b, "\t%q\n", jsonschemaImport)
	fmt.Fprintf(&b, "\t%q\n", mcpImport)
	extra := toolExtraImports(doc)
	if len(extra) > 0 {
		b.WriteString("\n")
		for _, imp := range extra {
			fmt.Fprintf(&b, "\t%q\n", imp)
		}
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "\tfwmanager %q\n", frameworkManagerImport)
	fmt.Fprintf(&b, "\t%q\n", securityImport)
	fmt.Fprintf(&b, "\t%s %q\n", managerAlias, opts.ManagerImport)
	b.WriteString(")\n\n")

	// --- Handler ---
	fmt.Fprintf(&b, "// Handler binds the %s manager to MCP tools.\n", iface)
	b.WriteString("type Handler struct {\n")
	fmt.Fprintf(&b, "\tManager %s.%s\n", managerAlias, iface)
	b.WriteString("}\n\n")

	// --- Register ---
	b.WriteString("// Register adds every operation as an mcp.Tool on srv, each carrying an\n")
	b.WriteString("// explicit human description and an explicit input JSON Schema (enum values +\n")
	b.WriteString("// meanings, REST-matching optionality) so an agentic consumer needs no source.\n")
	b.WriteString("func (h *Handler) Register(srv *mcp.Server) {\n")
	for _, op := range doc.Interface.Operations {
		toolName := mgrPrefix + op.Name
		desc := opts.OpDoc(op.Name)
		if strings.TrimSpace(desc) == "" {
			return nil, fmt.Errorf("mcpemit: no documentation for operation %q on %s (add it to clientgen's op-doc table)", op.Name, iface)
		}
		fmt.Fprintf(&b, "\tmcp.AddTool(srv, &mcp.Tool{Name: %q, Description: %q, InputSchema: %sInputSchema()}, h.handle%s)\n",
			toolName, desc, contract.LowerFirst(op.Name), op.Name)
	}
	b.WriteString("}\n\n")

	// --- input / output types ---
	for _, op := range doc.Interface.Operations {
		lower := contract.LowerFirst(op.Name)
		fmt.Fprintf(&b, "type %sInput struct {\n", lower)
		for _, p := range op.Params {
			tag := p.Name
			if p.Pointer {
				// Optional param: omitempty keeps the struct's own JSON round-trip
				// consistent with the schema's optionality (the schema's required
				// list is authoritative and set explicitly below).
				tag += ",omitempty"
			}
			fmt.Fprintf(&b, "\t%s %s `json:%q`\n",
				upperFirst(p.Name), contract.GoType(p.Schema, p.Pointer, managerAlias), tag)
		}
		b.WriteString("}\n\n")

		if op.Result != nil {
			fmt.Fprintf(&b, "type %sOutput struct {\n", lower)
			fmt.Fprintf(&b, "\tResult %s `json:\"result\"`\n", contract.GoType(op.Result, false, managerAlias))
			b.WriteString("}\n\n")
		} else {
			fmt.Fprintf(&b, "type %sOutput struct{}\n\n", lower)
		}
	}

	// --- per-op input schema builders ---
	for _, op := range doc.Interface.Operations {
		writeInputSchema(&b, op, enums)
	}

	// --- shared enum schema helpers ---
	for _, name := range enumNames {
		writeEnumSchema(&b, name, enums[name])
	}

	// --- handlers ---
	for _, op := range doc.Interface.Operations {
		writeToolHandler(&b, op)
	}

	writeHelpers(&b)

	src := []byte(b.String())
	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("mcpemit: emitted source does not parse: %w\n---\n%s", err, src)
	}
	return formatted, nil
}

// writeInputSchema emits the InputSchema builder for one op: it starts from the
// SDK's structural inference of the input struct (so nested object/array params
// keep their inferred shape) and then OVERRIDES the required list (non-pointer
// params) and replaces each enum param's property with an enriched enum schema.
func writeInputSchema(b *strings.Builder, op contract.Operation, enums map[string]enumDef) {
	lower := contract.LowerFirst(op.Name)
	fmt.Fprintf(b, "// %sInputSchema is the explicit MCP input schema for the %s operation.\n", lower, op.Name)
	fmt.Fprintf(b, "func %sInputSchema() *jsonschema.Schema {\n", lower)
	fmt.Fprintf(b, "\ts := objectSchema[%sInput]()\n", lower)

	// required = non-pointer params, in declaration order.
	var required []string
	for _, p := range op.Params {
		if !p.Pointer {
			required = append(required, p.Name)
		}
	}
	if len(required) == 0 {
		b.WriteString("\ts.Required = nil\n")
	} else {
		quoted := make([]string, len(required))
		for i, r := range required {
			quoted[i] = fmt.Sprintf("%q", r)
		}
		fmt.Fprintf(b, "\ts.Required = []string{%s}\n", strings.Join(quoted, ", "))
	}

	// enum param property overrides.
	for _, p := range op.Params {
		if _, ok := enums[p.Schema.RefName()]; !ok {
			continue
		}
		fmt.Fprintf(b, "\ts.Properties[%q] = enumSchema%s()\n", p.Name, p.Schema.RefName())
	}
	b.WriteString("\treturn s\n}\n\n")
}

// writeEnumSchema emits the shared schema helper for one enum $def: its base
// type, its allowed values, and a description mapping each value to its
// x-enum-varname so the agent knows what each integer/string means.
func writeEnumSchema(b *strings.Builder, name string, e enumDef) {
	fmt.Fprintf(b, "// enumSchema%s describes the %s enum: its allowed values and their meanings.\n", name, name)
	fmt.Fprintf(b, "func enumSchema%s() *jsonschema.Schema {\n", name)
	fmt.Fprintf(b, "\treturn &jsonschema.Schema{\n")
	fmt.Fprintf(b, "\t\tType: %q,\n", e.baseType)
	fmt.Fprintf(b, "\t\tEnum: []any{%s},\n", strings.Join(e.values, ", "))
	fmt.Fprintf(b, "\t\tDescription: %q,\n", enumDescription(name, e))
	b.WriteString("\t}\n}\n\n")
}

// enumDescription renders "Allowed values: 0=KindMission, 1=KindGlossary, ..."
// (falling back to a bare value list when a def carries no varnames).
func enumDescription(name string, e enumDef) string {
	pairs := make([]string, len(e.values))
	for i, v := range e.values {
		if i < len(e.varnames) && e.varnames[i] != "" {
			pairs[i] = fmt.Sprintf("%s=%s", v, e.varnames[i])
		} else {
			pairs[i] = v
		}
	}
	return fmt.Sprintf("%s. Allowed values: %s.", name, strings.Join(pairs, ", "))
}

func writeToolHandler(b *strings.Builder, op contract.Operation) {
	lower := contract.LowerFirst(op.Name)
	fmt.Fprintf(b, "// handle%s is the MCP tool handler for the %s operation.\n", op.Name, op.Name)
	fmt.Fprintf(b, "func (h *Handler) handle%s(ctx context.Context, _ *mcp.CallToolRequest, in %sInput) (*mcp.CallToolResult, %sOutput, error) {\n",
		op.Name, lower, lower)
	fmt.Fprintf(b, "\tvar out %sOutput\n", lower)
	b.WriteString("\tprincipal, _ := security.PrincipalFrom(ctx)\n")
	b.WriteString("\trc := fwmanager.Context{Context: ctx, Principal: principal}\n")

	args := []string{"rc"}
	for _, p := range op.Params {
		args = append(args, "in."+upperFirst(p.Name))
	}
	call := fmt.Sprintf("h.Manager.%s(%s)", op.Name, strings.Join(args, ", "))
	if op.Result != nil {
		fmt.Fprintf(b, "\tresult, err := %s\n", call)
		b.WriteString("\tif err != nil {\n\t\treturn nil, out, mapManagerError(err)\n\t}\n")
		b.WriteString("\tout.Result = result\n")
		b.WriteString("\treturn nil, out, nil\n")
	} else {
		fmt.Fprintf(b, "\tif err := %s; err != nil {\n\t\treturn nil, out, mapManagerError(err)\n\t}\n", call)
		b.WriteString("\treturn nil, out, nil\n")
	}
	b.WriteString("}\n\n")
}

// writeHelpers emits the per-file support helpers: the generic object-schema
// inference wrapper and the framework manager.Error mapper.
func writeHelpers(b *strings.Builder) {
	b.WriteString("// --- helpers ---------------------------------------------------------------\n\n")
	b.WriteString("// objectSchema infers the base object JSON Schema for a tool input struct via\n")
	b.WriteString("// the SDK's reflection (the same inference the SDK would run for a nil input\n")
	b.WriteString("// schema), so nested object/array params keep their structural shape; callers\n")
	b.WriteString("// then override the required list and any enum properties. It panics on an\n")
	b.WriteString("// un-inferrable type — a build-time contract error surfaced at server start.\n")
	b.WriteString("func objectSchema[T any]() *jsonschema.Schema {\n")
	b.WriteString("\ts, err := jsonschema.For[T](nil)\n")
	b.WriteString("\tif err != nil {\n\t\tpanic(fmt.Sprintf(\"mcp input schema inference: %v\", err))\n\t}\n")
	b.WriteString("\tif s.Properties == nil {\n\t\ts.Properties = map[string]*jsonschema.Schema{}\n\t}\n")
	b.WriteString("\treturn s\n}\n\n")

	b.WriteString("func mapManagerError(err error) error {\n")
	b.WriteString("\tvar me *fwmanager.Error\n")
	b.WriteString("\tif errors.As(err, &me) {\n")
	b.WriteString("\t\treturn fmt.Errorf(\"%s: %s\", me.Kind.String(), me.Detail)\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn err\n}\n")
}

// toolExtraImports collects the sorted, de-duplicated x-go-import paths the
// generated input/output structs reference, so the tool types compile against
// the real manager signatures.
func toolExtraImports(doc *contract.Doc) []string {
	set := map[string]bool{}
	add := func(n *contract.SchemaNode) {
		for _, imp := range contract.GoImports(n) {
			set[imp] = true
		}
		if kind, ok := doc.ScalarKind(n); ok && kind == "uuid" {
			set["github.com/google/uuid"] = true
		}
	}
	for _, op := range doc.Interface.Operations {
		for _, p := range op.Params {
			add(p.Schema)
		}
		add(op.Result)
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}
