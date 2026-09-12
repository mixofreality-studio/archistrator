package mcpemit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// fieldDescriptions is a contract's own property documentation: $def name ->
// wire key -> description, for every object $def that documents at least one of
// its properties.
//
// WHY IT EXISTS: an MCP tool's output schema is INFERRED (jsonschema.For) from the
// Go types modelgen emits, and modelgen carries no property `description` into
// those types — it emits a json tag and nothing else. So a contract that states
// "this field is meaningless unless that one is true" reached the HTTP/TS surface
// and never an agent reading the tool: the agent saw the field's shape and not
// the sentence that says when to ignore it. The table below closes that, generated
// from the same contract document everything else here is.
type fieldDescriptions map[string]map[string]string

// parseFieldDescriptions collects every non-empty `description` a contract $def
// carries on one of its properties. A def bound to a type modelgen does not emit
// as mgr.<Def> (x-go-type) or emitted as a sealed sum (x-go-sumtype) is skipped:
// there is no struct for the table to key on.
func parseFieldDescriptions(entry json.RawMessage) (fieldDescriptions, error) {
	var top struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(entry, &top); err != nil {
		return nil, fmt.Errorf("mcpemit: parse $defs: %w", err)
	}
	out := fieldDescriptions{}
	for name, raw := range top.Defs {
		var def struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
			GoType    string          `json:"x-go-type"`
			GoSumtype json.RawMessage `json:"x-go-sumtype"`
		}
		if err := json.Unmarshal(raw, &def); err != nil {
			continue // not an object def with object-shaped properties
		}
		if def.GoType != "" || len(def.GoSumtype) > 0 {
			continue
		}
		for prop, p := range def.Properties {
			d := strings.TrimSpace(p.Description)
			if d == "" {
				continue
			}
			if out[name] == nil {
				out[name] = map[string]string{}
			}
			out[name][prop] = d
		}
	}
	return out, nil
}

func sortedDescriptionKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writeFieldDescriptions emits the description table and the walker that stamps
// it onto an inferred output schema. It is emitted ONLY when the contract
// documents a property, so a contract that documents none generates exactly what
// it generated before.
func writeFieldDescriptions(b *strings.Builder, descs fieldDescriptions) {
	b.WriteString("// contractFieldDescriptions is the contract's own property documentation,\n")
	b.WriteString("// keyed by the Go type modelgen emits for each documenting $def. modelgen\n")
	b.WriteString("// carries no description into those types, so the inferred output schema\n")
	b.WriteString("// would otherwise show an agent a field's shape but never the contract's\n")
	b.WriteString("// statement of when that field means nothing.\n")
	b.WriteString("var contractFieldDescriptions = map[reflect.Type]map[string]string{\n")
	for _, def := range sortedDescriptionKeys(descs) {
		fmt.Fprintf(b, "\treflect.TypeFor[%s.%s](): {\n", managerAlias, def)
		for _, prop := range sortedDescriptionKeys(descs[def]) {
			fmt.Fprintf(b, "\t\t%q: %q,\n", prop, descs[def][prop])
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n\n")

	b.WriteString("// describeContractFields stamps contractFieldDescriptions onto an inferred\n")
	b.WriteString("// schema, walking it in step with the Go type it was inferred from, so a\n")
	b.WriteString("// description lands only on the property the contract documents — wherever\n")
	b.WriteString("// that type appears in the output (a map value, a slice element, a field).\n")
	b.WriteString("// It runs LAST: relaxRawJSON may replace a node wholesale, and a description\n")
	b.WriteString("// written before that would be lost with it.\n")
	b.WriteString("func describeContractFields(s *jsonschema.Schema, t reflect.Type) {\n")
	b.WriteString("\tif s == nil {\n\t\treturn\n\t}\n")
	b.WriteString("\tfor t.Kind() == reflect.Pointer {\n\t\tt = t.Elem()\n\t}\n")
	// An if-chain, not a switch over reflect.Kind: only three kinds carry nested
	// schemas, and a switch would have to name all ~26 to satisfy the exhaustive
	// gate for no behavioural gain.
	b.WriteString("\tk := t.Kind()\n")
	b.WriteString("\tif k == reflect.Slice || k == reflect.Array {\n\t\tdescribeContractFields(s.Items, t.Elem())\n\t\treturn\n\t}\n")
	b.WriteString("\tif k == reflect.Map {\n\t\tdescribeContractFields(s.AdditionalProperties, t.Elem())\n\t\treturn\n\t}\n")
	b.WriteString("\tif k != reflect.Struct {\n\t\treturn\n\t}\n")
	b.WriteString("\tdescs := contractFieldDescriptions[t]\n")
	b.WriteString("\tfor i := range t.NumField() {\n")
	b.WriteString("\t\tf := t.Field(i)\n")
	b.WriteString("\t\tname := jsonFieldName(f)\n")
	b.WriteString("\t\tp, ok := s.Properties[name]\n")
	b.WriteString("\t\tif name == \"\" || !ok {\n\t\t\tcontinue\n\t\t}\n")
	b.WriteString("\t\tif d, ok := descs[name]; ok {\n\t\t\tp.Description = d\n\t\t}\n")
	b.WriteString("\t\tdescribeContractFields(p, f.Type)\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n\n")

	b.WriteString("// jsonFieldName is the wire key encoding/json uses for a struct field (\"\" for\n")
	b.WriteString("// an unexported or json:\"-\" field, which no schema property stands for).\n")
	b.WriteString("func jsonFieldName(f reflect.StructField) string {\n")
	b.WriteString("\tif !f.IsExported() {\n\t\treturn \"\"\n\t}\n")
	b.WriteString("\tname, _, _ := strings.Cut(f.Tag.Get(\"json\"), \",\")\n")
	b.WriteString("\tswitch name {\n")
	b.WriteString("\tcase \"-\":\n\t\treturn \"\"\n")
	b.WriteString("\tcase \"\":\n\t\treturn f.Name\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn name\n")
	b.WriteString("}\n\n")
}
