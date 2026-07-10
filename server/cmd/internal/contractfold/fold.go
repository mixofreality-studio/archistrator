// Package contractfold implements the mechanical splice step of the schema-first
// codegen bootstrap pipeline: it takes ONE component's schemagen output (a
// contract.schema.json document — `title` + `$defs` + `interface`) and folds it
// into `.aiarch/state/project.json`'s `.serviceContracts[<key>]` entry, so a
// brand-new (or re-seeded) component's contract document lands in project.json
// without a hand-edit.
//
// WHY A SURGICAL TEXT SPLICE, NOT decode+re-encode: the obvious "safe" approach —
// decode project.json through the typed projectstate codec
// (DecodeProjectJSON/EncodeProjectJSON) and re-encode it — was verified NOT to
// round-trip the committed `.aiarch/state/project.json` byte-identically: the
// document's TOP-LEVEL key order does not match the `projectDoc` struct's field
// declaration order (which is what `encoding/json` follows when marshaling a
// struct), and at least one entry (`projectStateAccess.infra`) carries an
// explicit `[]` that the struct's `omitempty` tag would drop on re-encode. Both
// are evidence the committed file was assembled by hand-splicing individual
// ServiceContract-shaped JSON objects into place rather than by a full
// `EncodeProjectJSON` pass — so a full decode/re-encode would rewrite the ENTIRE
// document (reordering every top-level key, and silently dropping the empty
// `infra: []`), violating the "must not reorder or reformat anything outside the
// folded entry" requirement. Fold instead never leaves the byte domain: it locates
// the target entry's EXACT byte span (via an ordered, RawMessage-preserving JSON
// walk — never a lossy decode into a plain `map[string]any`) and replaces only
// that span.
//
// FIELD ORDER: every `.serviceContracts` entry observed in the committed
// project.json follows one fixed field order — component, layer, goPackage,
// infra, deps, stub, title, $defs, interface, notes — which is exactly the
// `projectstate.ServiceContract` struct's field declaration order. Fold assumes
// (and Idempotent proves, against the live file) that this order holds; it always
// re-emits an entry in this canonical order rather than trying to preserve an
// arbitrary existing order.
//
// FOLD SAFETY: Fold refuses to silently REGRESS a contract. schemagen's
// component registry (see cmd/schemagen's package doc) is a hand-maintained,
// possibly-incomplete work-list — a schema document reflected from an
// incomplete registry entry can carry FEWER `$defs` than the committed entry
// already has. Folding that in would silently drop real, previously-captured
// contract shapes. So by default Fold errors out (naming the missing defs)
// whenever the incoming schema document's `$defs` are not a superset of the
// existing entry's `$defs`; callers that genuinely intend to shrink a contract
// (a real, deliberate removal) must opt in explicitly (see Fold's allowShrink
// parameter / the CLI's --allow-shrink flag).
package contractfold

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// canonicalEntryOrder is the fixed field order every `.serviceContracts` entry is
// written in (== projectstate.ServiceContract's json tag order). Fold rebuilds an
// entry's fields in exactly this order; fields absent from both the existing
// entry and the replacement set are simply omitted (matching the struct's
// `omitempty` tags for infra/deps/stub).
var canonicalEntryOrder = []string{"component", "layer", "goPackage", "infra", "deps", "stub", "title", "$defs", "interface", "notes"}

// replacedFields are the keys Fold ALWAYS overwrites from the schemagen document,
// regardless of whether the existing entry already carries a (possibly stale)
// value for them.
var replacedFields = map[string]bool{"title": true, "$defs": true, "interface": true}

// layerTitleCase maps a schemagen/codegen interface layer key (lowercase, as
// written by framework-go-app-generator/modelgen's layerContext) to the
// capitalized Method layer name project.json's entry-level `layer` field carries
// (e.g. "resourceaccess" -> "ResourceAccess"). Used ONLY when Fold creates a
// brand-new entry (an existing entry's `layer` is always preserved verbatim).
var layerTitleCase = map[string]string{
	"engine":         "Engine",
	"resourceaccess": "ResourceAccess",
	"manager":        "Manager",
	"client":         "Client",
}

// field is one ordered (key, raw value) pair of a JSON object, preserving the
// value's EXACT original bytes (whitespace, nested key order, everything) — the
// same discipline cmd/modelgen's propOrder/orderedKeys already rely on.
type field struct {
	key string
	raw json.RawMessage
}

// parseOrderedObject decodes a JSON object's raw bytes into its ordered
// (key, raw value) pairs. Object VALUES are captured as json.RawMessage, so their
// bytes are copied verbatim from the input — never reformatted, never re-sorted —
// exactly mirroring how modelgen's orderedKeys() recovers property order from raw
// bytes rather than from a decoded map.
func parseOrderedObject(raw []byte) ([]field, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("read opening token: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected JSON object, got %v", tok)
	}
	var fields []field
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("read key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("expected string key, got %v", keyTok)
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, fmt.Errorf("read value for %q: %w", key, err)
		}
		fields = append(fields, field{key: key, raw: val})
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return nil, fmt.Errorf("read closing token: %w", err)
	}
	return fields, nil
}

// indentUnit is project.json's (and schemagen's) canonical indent — both are
// written via `encoding/json`'s MarshalIndent(v, "", "  ")` — 2 spaces per level.
const indentUnit = "  "

func indent(depth int) string {
	return strings.Repeat(indentUnit, depth)
}

// reindentValue reformats a single JSON value's raw bytes so it renders correctly
// at childDepth — i.e. so that if this value's OWN opening token sits on the same
// line as a `"key": ` at depth (childDepth-1), its children fall at childDepth and
// its closing token lines up with childDepth-1. This is exactly
// `encoding/json.Indent`'s contract, so any valid JSON input (regardless of its
// OWN original indentation depth — a standalone contract.schema.json file's
// `$defs` sits 3 levels shallower than the same content folded into
// project.json) re-renders at the correct depth. Indent never touches string
// contents (it copies string tokens through byte-for-byte), so it preserves
// whatever escaping (HTML-escaped `<` etc.) the source already carries.
func reindentValue(raw []byte, childDepth int) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, indent(childDepth), indentUnit); err != nil {
		return nil, fmt.Errorf("reindent: %w", err)
	}
	return buf.Bytes(), nil
}

// renderObject re-serializes an ordered field list as a JSON object whose
// "key": line sits at depth childDepth (so its own closing brace lines up with
// childDepth-1) — the exact inverse of parseOrderedObject, byte-compatible with
// project.json's canonical MarshalIndent(_, "", "  ") style.
func renderObject(fields []field, childDepth int) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, f := range fields {
		keyJSON, err := json.Marshal(f.key)
		if err != nil {
			return nil, err
		}
		val, err := reindentValue(f.raw, childDepth)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.key, err)
		}
		buf.WriteString(indent(childDepth))
		buf.Write(keyJSON)
		buf.WriteString(": ")
		buf.Write(val)
		if i < len(fields)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString(indent(childDepth - 1))
	buf.WriteString("}")
	return buf.Bytes(), nil
}

// schemaDoc is the subset of a schemagen contract.schema.json document Fold
// reads: the three fields it folds into project.json.
type schemaDoc struct {
	Title     json.RawMessage `json:"title"`
	Defs      json.RawMessage `json:"$defs"`
	Interface json.RawMessage `json:"interface"`
}

// schemaInterfaceLayer is the minimal decode of a schemagen document's
// `interface` node, needed only to seed a BRAND NEW entry's entry-level `layer`.
type schemaInterfaceLayer struct {
	Layer string `json:"layer"`
}

// entryDepth / fieldDepth are the fixed nesting depths of a `.serviceContracts`
// entry and its fields in project.json's canonical layout:
// {  (depth 0)
//
//	"serviceContracts": {              (its own fields at depth 1)
//	  "<key>": {                       (entryDepth == 2)
//	    "component": ...,              (fieldDepth == 3)
const (
	entryDepth = 2
	fieldDepth = 3
)

// Fold splices one component's schemagen contract document (schemaRaw — the full
// contract.schema.json bytes reflected from Go package dir) into projectRaw
// (the full `.aiarch/state/project.json` bytes), replacing
// `.serviceContracts[key]`'s `title` + `$defs` + `interface` with the schemagen
// document's versions. Every other entry field (component, layer, goPackage,
// infra, deps, stub, notes) is preserved BYTE-FOR-BYTE from the existing entry —
// Fold never regenerates them, so it is a strict no-op on those fields even if
// this package's understanding of their shape is incomplete.
//
// dir is the schemagen component dir schemaRaw was reflected from (schemagen's
// `component.dir`, e.g. "internal/resourceaccess/projectstate"). If key already
// has an entry, dir MUST equal that entry's `goPackage` — Fold refuses to fold a
// schema document into a differently-homed key (a mismatch means the caller
// passed the wrong key/dir pair, not a legitimate contract change). If key has no
// entry yet, Fold CREATES one: component=key, goPackage=dir, layer derived from
// the schema document's `interface.layer`, and no infra/deps/stub (a fresh
// entry starts minimal; those are added by hand once, same as today).
//
// Fold is idempotent: folding its own output a second time (same schemaRaw)
// produces byte-identical bytes.
//
// allowShrink opts in to folding a schema document whose `$defs` are NOT a
// superset of the existing entry's `$defs` (see FOLD SAFETY in the package
// doc). false is the safe default: Fold refuses (with an error naming the
// missing defs) rather than silently regress the committed contract.
// allowShrink has no effect when creating a brand-new entry (there is nothing
// to shrink from).
func Fold(projectRaw, schemaRaw []byte, key, dir string, allowShrink bool) ([]byte, error) {
	if key == "" {
		return nil, fmt.Errorf("contractfold: key is required")
	}
	if dir == "" {
		return nil, fmt.Errorf("contractfold: dir is required")
	}

	var doc schemaDoc
	if err := json.Unmarshal(schemaRaw, &doc); err != nil {
		return nil, fmt.Errorf("contractfold: parse schema document: %w", err)
	}
	if len(doc.Title) == 0 {
		return nil, fmt.Errorf("contractfold: schema document has no title")
	}
	if len(doc.Defs) == 0 {
		return nil, fmt.Errorf("contractfold: schema document has no $defs")
	}
	if len(doc.Interface) == 0 {
		return nil, fmt.Errorf("contractfold: schema document has no interface")
	}

	var top []field
	{
		var err error
		top, err = parseOrderedObject(projectRaw)
		if err != nil {
			return nil, fmt.Errorf("contractfold: parse project.json: %w", err)
		}
	}
	scIdx := indexOfField(top, "serviceContracts")
	if scIdx < 0 {
		return nil, fmt.Errorf("contractfold: project.json has no .serviceContracts")
	}
	scRaw := top[scIdx].raw

	entries, err := parseOrderedObject(scRaw)
	if err != nil {
		return nil, fmt.Errorf("contractfold: parse .serviceContracts: %w", err)
	}

	entryIdx := indexOfField(entries, key)
	if entryIdx >= 0 {
		return foldExisting(projectRaw, entries[entryIdx].raw, doc, key, dir, allowShrink)
	}
	return foldNew(projectRaw, scRaw, entries, doc, key, dir)
}

func indexOfField(fields []field, key string) int {
	for i, f := range fields {
		if f.key == key {
			return i
		}
	}
	return -1
}

// foldExisting replaces an EXISTING entry's title/$defs/interface in place. It
// locates the entry's exact byte span within projectRaw (the RawMessage bytes
// pulled out during parseOrderedObject are an exact substring of projectRaw — no
// decode/re-encode happened), verifies that span is UNIQUE (refuses to guess if
// it isn't), and replaces only that span — every byte of projectRaw outside it is
// untouched.
func foldExisting(projectRaw, oldEntryRaw []byte, doc schemaDoc, key, dir string, allowShrink bool) ([]byte, error) {
	entryFields, err := parseOrderedObject(oldEntryRaw)
	if err != nil {
		return nil, fmt.Errorf("contractfold: parse entry %q: %w", key, err)
	}
	existing := map[string]json.RawMessage{}
	for _, f := range entryFields {
		existing[f.key] = f.raw
	}

	if gp, ok := existing["goPackage"]; ok {
		var goPackage string
		if err := json.Unmarshal(gp, &goPackage); err != nil {
			return nil, fmt.Errorf("contractfold: entry %q goPackage: %w", key, err)
		}
		if goPackage != dir {
			return nil, fmt.Errorf("contractfold: entry %q has goPackage %q, but schema was reflected from %q — refusing to fold (wrong key/dir pair?)", key, goPackage, dir)
		}
	}

	if !allowShrink {
		missing, err := missingDefs(existing["$defs"], doc.Defs)
		if err != nil {
			return nil, fmt.Errorf("contractfold: entry %q: %w", key, err)
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("contractfold: entry %q: incoming schema is missing $defs %v that the committed entry already has — refusing to silently shrink the contract (pass allowShrink/--allow-shrink to override if this is an intentional removal)", key, missing)
		}
	}

	newFields, err := buildEntryFields(existing, doc, key, dir)
	if err != nil {
		return nil, err
	}
	newEntryRaw, err := renderObject(newFields, fieldDepth)
	if err != nil {
		return nil, fmt.Errorf("contractfold: render entry %q: %w", key, err)
	}

	n := bytes.Count(projectRaw, oldEntryRaw)
	if n == 0 {
		return nil, fmt.Errorf("contractfold: entry %q's parsed bytes were not found verbatim in project.json (unexpected)", key)
	}
	if n > 1 {
		return nil, fmt.Errorf("contractfold: entry %q's bytes are not unique in project.json (%d occurrences) — refusing to guess which to replace", key, n)
	}
	return bytes.Replace(projectRaw, oldEntryRaw, newEntryRaw, 1), nil
}

// missingDefs returns the keys present in existingRaw's `$defs` object but
// ABSENT from incomingRaw's `$defs` object — i.e. the defs a fold would DROP —
// sorted for a deterministic error message. Either raw may be empty/nil (no
// `$defs` at all), which is treated as an empty def set.
func missingDefs(existingRaw, incomingRaw json.RawMessage) ([]string, error) {
	var existing map[string]json.RawMessage
	if len(existingRaw) > 0 {
		if err := json.Unmarshal(existingRaw, &existing); err != nil {
			return nil, fmt.Errorf("parse existing $defs: %w", err)
		}
	}
	var incoming map[string]json.RawMessage
	if len(incomingRaw) > 0 {
		if err := json.Unmarshal(incomingRaw, &incoming); err != nil {
			return nil, fmt.Errorf("parse incoming $defs: %w", err)
		}
	}
	var missing []string
	for k := range existing {
		if _, ok := incoming[k]; !ok {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// foldNew creates a brand-new entry for key (no existing `.serviceContracts[key]`)
// and inserts it into the `.serviceContracts` object at its sorted position,
// matching the alphabetical key order every OTHER entry in project.json already
// follows (`.serviceContracts` is a Go map, so Go's own encoder would sort it the
// same way). Every OTHER existing entry's bytes are carried through byte-for-byte.
func foldNew(projectRaw, scRaw []byte, entries []field, doc schemaDoc, key, dir string) ([]byte, error) {
	newFields, err := buildEntryFields(nil, doc, key, dir)
	if err != nil {
		return nil, err
	}
	newEntryRaw, err := renderObject(newFields, fieldDepth)
	if err != nil {
		return nil, fmt.Errorf("contractfold: render entry %q: %w", key, err)
	}

	inserted := make([]field, 0, len(entries)+1)
	placed := false
	for _, f := range entries {
		if !placed && key < f.key {
			inserted = append(inserted, field{key: key, raw: newEntryRaw})
			placed = true
		}
		inserted = append(inserted, f)
	}
	if !placed {
		inserted = append(inserted, field{key: key, raw: newEntryRaw})
	}

	newSCRaw, err := renderObject(inserted, entryDepth)
	if err != nil {
		return nil, fmt.Errorf("contractfold: render .serviceContracts: %w", err)
	}

	n := bytes.Count(projectRaw, scRaw)
	if n == 0 {
		return nil, fmt.Errorf("contractfold: .serviceContracts bytes were not found verbatim in project.json (unexpected)")
	}
	if n > 1 {
		return nil, fmt.Errorf("contractfold: .serviceContracts bytes are not unique in project.json (%d occurrences) — refusing to guess which to replace", n)
	}
	return bytes.Replace(projectRaw, scRaw, newSCRaw, 1), nil
}

// buildEntryFields assembles an entry's ordered field list in canonicalEntryOrder:
// existing (preserved, byte-for-byte) values for component/layer/goPackage/
// infra/deps/stub/notes, and the schema document's title/$defs/interface.
// existing is nil when creating a brand-new entry, in which case
// component/layer/goPackage are synthesized (see Fold's doc comment) and
// infra/deps/stub/notes are omitted.
func buildEntryFields(existing map[string]json.RawMessage, doc schemaDoc, key, dir string) ([]field, error) {
	replacement := map[string]json.RawMessage{
		"title":     doc.Title,
		"$defs":     doc.Defs,
		"interface": doc.Interface,
	}

	if existing == nil {
		var ifaceLayer schemaInterfaceLayer
		if err := json.Unmarshal(doc.Interface, &ifaceLayer); err != nil {
			return nil, fmt.Errorf("contractfold: parse interface.layer: %w", err)
		}
		layer, ok := layerTitleCase[ifaceLayer.Layer]
		if !ok {
			return nil, fmt.Errorf("contractfold: entry %q does not exist and interface.layer %q has no known Method-layer mapping — create the entry by hand first (component/layer/goPackage), then re-run Fold", key, ifaceLayer.Layer)
		}
		componentJSON, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		dirJSON, err := json.Marshal(dir)
		if err != nil {
			return nil, err
		}
		layerJSON, err := json.Marshal(layer)
		if err != nil {
			return nil, err
		}
		existing = map[string]json.RawMessage{
			"component": componentJSON,
			"layer":     layerJSON,
			"goPackage": dirJSON,
		}
	}

	fields := make([]field, 0, len(canonicalEntryOrder))
	for _, k := range canonicalEntryOrder {
		if replacedFields[k] {
			fields = append(fields, field{key: k, raw: replacement[k]})
			continue
		}
		if raw, ok := existing[k]; ok {
			fields = append(fields, field{key: k, raw: raw})
		}
	}
	return fields, nil
}
