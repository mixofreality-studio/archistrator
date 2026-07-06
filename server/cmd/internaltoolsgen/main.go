// Command internaltoolsgen generates the INTERNAL MCP tool surface for
// archistrator's own ResourceAccess and Engine contracts, 100% from project.json
// .serviceContracts. It is the schema-first codegen behind `make gen-internal-tools`:
//
//   - reads every .serviceContracts entry whose layer is ResourceAccess or Engine
//     and that has a goPackage (built or authored-stub components; the withdrawn
//     legacy entries with no goPackage are skipped),
//   - emits one InternalTool descriptor per contract operation — name, component,
//     layer, operation, readOnlyHint, agent-hidden flag, description, and a
//     self-contained input/output JSON Schema ($defs closed to what the op
//     references) — into
//     internal/resourceaccess/projectstate/toolcatalog.gen.go.
//
// This surface is INTERNAL: it is NEVER merged into api/openapi.yaml (that is
// cmd/clientgen's public Manager surface). It is the catalog aiarch-state-mcp
// registers — the non-hidden read-only + Engine tools in every per-mode set.
//
// Output is deterministic (contracts + operations visited in sorted order,
// schema map keys sorted by encoding/json) so re-running is byte-idempotent, and
// it is gofmt'd before it is written.
//
// Usage:
//
//	cd server && make gen-internal-tools
//	cd server && go run ./cmd/internaltoolsgen ../.aiarch/state/project.json
package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// projectFile is the default path (relative to the server module root, where the
// gen targets run) to the head-state document that owns the contracts.
const projectFile = "../.aiarch/state/project.json"

// outPath is the generated catalog's destination, relative to the server module root.
const outPath = "internal/resourceaccess/projectstate/toolcatalog.gen.go"

// readVerbs are the Method operation-name prefixes that mark a side-effect-free
// ResourceAccess read (the honest read/write signal the contracts already carry).
var readVerbs = []string{"Get", "Read", "List", "Query", "Observe", "Retrieve", "Fetch"}

// refPattern matches a JSON-Schema local $ref into the contract's $defs.
var refPattern = regexp.MustCompile(`#/\$defs/([A-Za-z0-9_]+)`)

func main() {
	path := projectFile
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the CLI argument to a developer-run codegen tool, no trust boundary
	if err != nil {
		fatal("read %s: %v", path, err)
	}
	var top struct {
		ServiceContracts map[string]json.RawMessage `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		fatal("parse %s: %v", path, err)
	}

	keys := make([]string, 0, len(top.ServiceContracts))
	for k := range top.ServiceContracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var tools []projectstate.InternalTool
	for _, key := range keys {
		var sc projectstate.ServiceContract
		if err := json.Unmarshal(top.ServiceContracts[key], &sc); err != nil {
			fatal("contract %q: parse: %v", key, err)
		}
		if sc.Layer != "ResourceAccess" && sc.Layer != "Engine" {
			continue // Managers get the public client surface; only RA/Engine get the internal one.
		}
		if sc.GoPackage == "" {
			continue // withdrawn/legacy entry with no target package — not a real component.
		}
		base := toolBase(sc.Component)
		for _, op := range sc.Interface.Operations {
			ro := readOnly(sc.Layer, op.Name)
			tools = append(tools, projectstate.InternalTool{
				Name:         base + op.Name,
				Component:    sc.Component,
				Layer:        sc.Layer,
				Operation:    op.Name,
				Params:       paramNames(op),
				ReadOnly:     ro,
				AgentHidden:  agentHidden(sc.Component, ro),
				Description:  describe(sc.Component, sc.Layer, op.Name, ro),
				InputSchema:  inputSchema(op, sc.Defs),
				OutputSchema: outputSchema(op, sc.Defs),
			})
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })

	src, err := emit(tools)
	if err != nil {
		fatal("emit: %v", err)
	}
	if err := os.WriteFile(outPath, src, 0o600); err != nil {
		fatal("write %s: %v", outPath, err)
	}
	fmt.Printf("wrote %s (%d internal tools)\n", outPath, len(tools))
}

// toolBase is the tool-name prefix for a component: its key with the layer
// stereotype suffix (Access/Engine) stripped (e.g. "projectStateAccess" →
// "projectState", "reviewEngine" → "review"). The key is already lowerFirst.
func toolBase(component string) string {
	for _, suffix := range []string{"Access", "Engine"} {
		if strings.HasSuffix(component, suffix) && len(component) > len(suffix) {
			return strings.TrimSuffix(component, suffix)
		}
	}
	return component
}

// paramNames returns the operation's business parameter names in declaration
// order (the leading ambient call Context is not a contract parameter, so it does
// not appear in op.Params). The execution rail binds a call's named args to the
// live method's positional params using this order.
func paramNames(op projectstate.ContractOperation) []string {
	if len(op.Params) == 0 {
		return nil
	}
	out := make([]string, 0, len(op.Params))
	for _, p := range op.Params {
		out = append(out, p.Name)
	}
	return out
}

// readOnly derives the MCP readOnlyHint. Every Engine op is read-only (Engines
// are pure computation); a ResourceAccess op is read-only iff its name carries a
// read verb.
func readOnly(layer, op string) bool {
	if layer == "Engine" {
		return true
	}
	for _, v := range readVerbs {
		if strings.HasPrefix(op, v) {
			return true
		}
	}
	return false
}

// agentHidden marks the raw ops kept OFF the agent surface even though generated.
// projectStateAccess is the merge-authority / rail ResourceAccess: its WRITES
// (CommitArtifact/AdvancePhase/StageArtifactForReview/…) land approved artifacts
// onto main with cross-slot staleness side effects, so they stay server-rail-only
// and are replaced for agents by the composed verbs (putDraftModel/publishDraft/
// setCritiqueVerdict/respondToReviewComment). Its READS remain exposable (and are
// themselves shadowed by getCommittedSlot/getDraftSlot). Every other RA/Engine op
// is exposable — that is the whole point of the internal surface.
func agentHidden(component string, ro bool) bool {
	return component == "projectStateAccess" && !ro
}

// describe renders the default tool description for a raw RA/Engine op.
func describe(component, layer, op string, ro bool) string {
	effect := "state-changing"
	if ro {
		effect = "read-only"
	}
	return fmt.Sprintf("%s on the %s %s contract (%s). Raw generated internal tool.", op, component, layer, effect)
}

// inputSchema builds the self-contained JSON Schema for an operation's inputs: an
// object with one property per parameter (its contract schema node), a required
// list of the non-pointer params, and the $defs transitively reachable from the
// params.
func inputSchema(op projectstate.ContractOperation, defs map[string]json.RawMessage) json.RawMessage {
	props := make(map[string]json.RawMessage, len(op.Params))
	var required []string
	var refRoots []json.RawMessage
	for _, p := range op.Params {
		props[p.Name] = p.Schema
		refRoots = append(refRoots, p.Schema)
		if !p.Pointer {
			required = append(required, p.Name)
		}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	if closure := reachableDefs(refRoots, defs); len(closure) > 0 {
		schema["$defs"] = closure
	}
	return mustMarshal(schema)
}

// outputSchema builds the self-contained JSON Schema for an operation's result
// (the raw result node plus its reachable $defs), or a bare object when the op
// returns nothing.
func outputSchema(op projectstate.ContractOperation, defs map[string]json.RawMessage) json.RawMessage {
	if len(op.Result) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	var node map[string]any
	if err := json.Unmarshal(op.Result, &node); err != nil {
		// A non-object result node (unusual): carry it verbatim.
		return op.Result
	}
	if closure := reachableDefs([]json.RawMessage{op.Result}, defs); len(closure) > 0 {
		node["$defs"] = closure
	}
	return mustMarshal(node)
}

// reachableDefs returns the transitive closure of $defs entries referenced from
// the given schema roots, so a tool schema resolves standalone.
func reachableDefs(roots []json.RawMessage, defs map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	var visit func(raw json.RawMessage)
	visit = func(raw json.RawMessage) {
		for _, m := range refPattern.FindAllStringSubmatch(string(raw), -1) {
			name := m[1]
			if _, seen := out[name]; seen {
				continue
			}
			def, ok := defs[name]
			if !ok {
				continue
			}
			out[name] = def
			visit(def)
		}
	}
	for _, r := range roots {
		visit(r)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// goStringSlice renders a []string as a Go composite literal (nil ⇒ "nil") for the
// generated catalog source.
func goStringSlice(xs []string) string {
	if len(xs) == 0 {
		return "nil"
	}
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Quote(x)
	}
	return "[]string{" + strings.Join(parts, ", ") + "}"
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		fatal("marshal schema: %v", err)
	}
	return b
}

// emit renders the generated catalog source.
func emit(tools []projectstate.InternalTool) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by cmd/internaltoolsgen. DO NOT EDIT.\n")
	b.WriteString("// The INTERNAL MCP tool surface for archistrator's ResourceAccess + Engine\n")
	b.WriteString("// contracts, emitted from project.json .serviceContracts. NEVER in the public OAS.\n\n")
	b.WriteString("package projectstate\n\n")
	b.WriteString("import \"encoding/json\"\n\n")
	b.WriteString("// internalToolCatalog is the generated internal tool surface. InternalToolCatalog\n")
	b.WriteString("// (toolpalette.go) is the exported accessor.\n")
	b.WriteString("func internalToolCatalog() []InternalTool {\n")
	b.WriteString("\treturn []InternalTool{\n")
	for _, t := range tools {
		b.WriteString("\t\t{\n")
		fmt.Fprintf(&b, "\t\t\tName:        %q,\n", t.Name)
		fmt.Fprintf(&b, "\t\t\tComponent:   %q,\n", t.Component)
		fmt.Fprintf(&b, "\t\t\tLayer:       %q,\n", t.Layer)
		fmt.Fprintf(&b, "\t\t\tOperation:   %q,\n", t.Operation)
		fmt.Fprintf(&b, "\t\t\tParams:      %s,\n", goStringSlice(t.Params))
		fmt.Fprintf(&b, "\t\t\tReadOnly:    %t,\n", t.ReadOnly)
		fmt.Fprintf(&b, "\t\t\tAgentHidden: %t,\n", t.AgentHidden)
		fmt.Fprintf(&b, "\t\t\tDescription: %q,\n", t.Description)
		fmt.Fprintf(&b, "\t\t\tInputSchema:  json.RawMessage(%s),\n", strconv.Quote(string(t.InputSchema)))
		fmt.Fprintf(&b, "\t\t\tOutputSchema: json.RawMessage(%s),\n", strconv.Quote(string(t.OutputSchema)))
		b.WriteString("\t\t},\n")
	}
	b.WriteString("\t}\n}\n")

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("emitted source does not parse: %w\n---\n%s", err, b.String())
	}
	return formatted, nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "internaltoolsgen: "+format+"\n", args...)
	os.Exit(1)
}
