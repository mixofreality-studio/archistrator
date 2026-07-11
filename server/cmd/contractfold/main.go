// Command contractfold is step 2 of the schema-first codegen BOOTSTRAP pipeline —
// the mechanical fold that was previously a manual text splice. cmd/schemagen
// reflects a component's Go contract surface into a standalone
// `<dir>/contract.schema.json` document; contractfold takes that document and
// splices it into `.aiarch/state/project.json`'s `.serviceContracts[<key>]` entry,
// so `.aiarch/state/project.json` — the OWNER of every built component's
// contract (see cmd/modelgen's package doc) — never needs a hand-edit to receive
// a schemagen re-seed.
//
// It is a SURGICAL TEXT SPLICE (see cmd/internal/contractfold's package doc for
// why): it replaces exactly the target entry's `title` + `$defs` + `interface`,
// preserving every other byte of project.json — including every other entry's
// `component`/`layer`/`goPackage`/`infra`/`deps`/`stub`/`notes` fields —
// untouched. By default it also refuses to fold a schema document whose
// `$defs` would DROP defs the committed entry already has (see FOLD SAFETY in
// cmd/internal/contractfold's package doc); pass --allow-shrink to override.
//
// MULTI-COMPONENT-PER-PACKAGE FILENAME CONVENTION (STICKY): RA→RA imports are
// banned, so a secondary component sharing an existing package's goPackage
// (e.g. a second ResourceAccess re-homed onto a directory that already has a
// primary component) cannot also author that directory's
// `contract.schema.json` — the primary component already owns it. The
// designation is STICKY, resolved per key by content, never re-derived from
// the sibling key set (a lexical rule would let a lexically-earlier new key
// silently steal primary): both foldOne and foldAll look for
// `<dir>/contract.<key>.schema.json` FIRST; only when that keyed file is
// absent do they fall back to the plain `<dir>/contract.schema.json`, and the
// fallback is VERIFIED — the plain document's `interface.name` with a lowered
// first letter must equal the requested component key (the existing
// key↔interface convention, e.g. ProjectStateAccess ↔ projectStateAccess).
// A plain file whose interface maps to a different key is a loud error in
// foldOne and a skip in foldAll (the matching sibling entry consumes it
// instead — at most one component per dir can, by construction). Net effect:
// an existing primary keeps `contract.schema.json` forever; EVERY component
// added to the package later authors `contract.<key>.schema.json`, regardless
// of lexical order; primary is never reassigned. Note this is unrelated to
// framework-go-app-generator/modelgen's genGroup EMISSION order, which merges
// a shared package's entries into one contract.gen.go in ascending
// contract-key order — that ordering rule is deliberate and unchanged.
//
// Usage:
//
//	cd server && make gen-schemas                                   # schemagen (all) + fold (all existing)
//	cd server && go run ./cmd/contractfold                          # fold every component that already has BOTH
//	                                                                 # a .serviceContracts entry AND an on-disk
//	                                                                 # contract*.schema.json (steady-state re-seed)
//	cd server && go run ./cmd/contractfold <dir> <key>               # fold exactly one component, CREATING its
//	                                                                 # .serviceContracts[key] entry if it does not
//	                                                                 # exist yet (first-time bootstrap of a new
//	                                                                 # component — see schemagen's package doc)
//	cd server && go run ./cmd/contractfold --allow-shrink <dir> <key> # same, but permit the incoming schema to
//	                                                                 # DROP $defs the committed entry already has
//	                                                                 # (refused by default — see contractfold's
//	                                                                 # package doc, FOLD SAFETY)
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/mixofreality-studio/archistrator/server/cmd/internal/contractfold"
)

// projectFile is the default path (relative to the server module root, where the
// gen targets run) to the head-state document that owns the contracts.
const projectFile = "../.aiarch/state/project.json"

func main() {
	allowShrink := flag.Bool("allow-shrink", false, "permit folding a schema whose $defs are not a superset of the committed entry's (default: refuse)")
	flag.Parse()
	args := flag.Args()

	switch len(args) {
	case 0:
		if err := foldAll(projectFile, *allowShrink); err != nil {
			fatal("%v", err)
		}
	case 2:
		if err := foldOne(projectFile, args[0], args[1], *allowShrink); err != nil {
			fatal("%v", err)
		}
	default:
		fatal("usage: contractfold [--allow-shrink]                    (fold every existing component's schema.json)\n" +
			"   or: contractfold [--allow-shrink] <dir> <key>       (fold/create exactly one component)")
	}
}

// errPlainSchemaForeign marks a plain `<dir>/contract.schema.json` whose
// `interface.name` maps to a DIFFERENT component key than the one requested —
// the plain file belongs to the package's primary component, and a secondary
// key must never consume (steal) it. foldOne surfaces it loudly; foldAll
// skips it (the matching sibling entry folds it instead).
var errPlainSchemaForeign = errors.New("contract.schema.json belongs to a different component")

// keyFromInterfaceName maps a contract interface's Go name to its
// `.serviceContracts` key per the repo-wide key↔interface convention: the
// interface name with a lowered first letter (ProjectStateAccess ↔
// projectStateAccess). Verified to hold for every committed entry.
func keyFromInterfaceName(name string) string {
	if name == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToLower(r)) + name[size:]
}

// interfaceNameOf pulls `interface.name` out of a schemagen contract document.
func interfaceNameOf(schemaRaw []byte) (string, error) {
	var doc struct {
		Interface struct {
			Name string `json:"name"`
		} `json:"interface"`
	}
	if err := json.Unmarshal(schemaRaw, &doc); err != nil {
		return "", fmt.Errorf("parse schema document: %w", err)
	}
	if doc.Interface.Name == "" {
		return "", fmt.Errorf("schema document has no interface.name")
	}
	return doc.Interface.Name, nil
}

// resolveSchemaDoc locates and reads key's schemagen document in dir per the
// STICKY filename convention (see the package doc): the keyed
// `contract.<key>.schema.json` wins whenever present; the plain
// `contract.schema.json` is a fallback ONLY when its own `interface.name`
// maps back to key (keyFromInterfaceName), so a plain file always folds into
// exactly the component that owns it, never a lexically-reassigned one.
// Returns fs.ErrNotExist (wrapped) when neither file is present, and
// errPlainSchemaForeign (wrapped) when only the plain file is present but
// belongs to a different key.
func resolveSchemaDoc(dir, key string) (string, []byte, error) {
	keyed := dir + "/contract." + key + ".schema.json"
	raw, err := os.ReadFile(keyed) // #nosec G304 G703 -- dir/key are a goPackage value committed in project.json or developer-supplied CLI args to a local codegen tool, no trust boundary
	if err == nil {
		return keyed, raw, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", nil, fmt.Errorf("read %s: %w", keyed, err)
	}

	plain := dir + "/contract.schema.json"
	raw, err = os.ReadFile(plain) // #nosec G304 G703 -- see above
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil, fmt.Errorf("no schema document for %s (looked for %s, then %s): %w", key, keyed, plain, fs.ErrNotExist)
		}
		return "", nil, fmt.Errorf("read %s: %w", plain, err)
	}

	ifaceName, err := interfaceNameOf(raw)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", plain, err)
	}
	if owner := keyFromInterfaceName(ifaceName); owner != key {
		return "", nil, fmt.Errorf("%w: %s carries interface %q (owning component key %q), but key %q was requested — a secondary component must author %s instead", errPlainSchemaForeign, plain, ifaceName, owner, key, keyed)
	}
	return plain, raw, nil
}

// foldOne folds a single component's schemagen document — resolved per the
// sticky filename convention (see resolveSchemaDoc) — into project.json's
// `.serviceContracts[key]`, writing project.json back in place. Every
// resolution failure, including a plain file owned by a different component,
// is a loud error.
func foldOne(path, dir, key string, allowShrink bool) error {
	projectRaw, err := os.ReadFile(path) // #nosec G304 G703 -- path is a fixed constant or a developer-supplied CLI arg to a local codegen tool, no trust boundary
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	schemaPath, schemaRaw, err := resolveSchemaDoc(dir, key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w (run `go run ./cmd/schemagen` first)", err)
		}
		return err
	}
	out, err := contractfold.Fold(projectRaw, schemaRaw, key, dir, allowShrink)
	if err != nil {
		return fmt.Errorf("fold %s: %w", key, err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil { // #nosec G703 -- path is a fixed constant or a developer-supplied CLI arg, no trust boundary
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "folded %s (%s) into %s\n", key, schemaPath, path)
	return nil
}

// foldAll folds every component that ALREADY has a `.serviceContracts` entry
// (found via its `goPackage`) with a matching on-disk schemagen document,
// resolved per the sticky filename convention (see resolveSchemaDoc): the
// keyed `contract.<key>.schema.json` wins; the plain `contract.schema.json`
// folds only into the entry whose key its `interface.name` maps to. Entries
// with no on-disk document (the steady-state norm — schemagen output is a
// transient bootstrap artifact, not committed) are skipped, not errored, and
// so is a plain file owned by a DIFFERENT sibling entry (that sibling
// consumes it): this is the "re-fold whatever was re-seeded" bulk mode, never
// a surprise-creating one (creation requires the explicit two-arg form).
func foldAll(path string, allowShrink bool) error {
	projectRaw, err := os.ReadFile(path) // #nosec G304 -- fixed constant path
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(projectRaw, &top); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		return fmt.Errorf("parse .serviceContracts in %s: %w", path, err)
	}

	type dirKey struct{ dir, key string }
	var targets []dirKey
	for k, entry := range contracts {
		var meta struct {
			GoPackage string `json:"goPackage"`
		}
		if err := json.Unmarshal(entry, &meta); err != nil {
			return fmt.Errorf("parse contract %q: %w", k, err)
		}
		if meta.GoPackage == "" {
			continue
		}
		targets = append(targets, dirKey{dir: meta.GoPackage, key: k})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].key < targets[j].key })

	cur := projectRaw
	folded := 0
	for _, t := range targets {
		schemaPath, schemaRaw, err := resolveSchemaDoc(t.dir, t.key)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // steady state: no re-seed pending for this component
			}
			if errors.Is(err, errPlainSchemaForeign) {
				continue // plain file belongs to a sibling entry, which folds it itself
			}
			return err
		}
		out, err := contractfold.Fold(cur, schemaRaw, t.key, t.dir, allowShrink)
		if err != nil {
			return fmt.Errorf("fold %s: %w", t.key, err)
		}
		if string(out) != string(cur) {
			fmt.Fprintf(os.Stderr, "folded %s (%s) — changed\n", t.key, schemaPath)
		} else {
			fmt.Fprintf(os.Stderr, "folded %s (%s) — no-op\n", t.key, schemaPath)
		}
		cur = out
		folded++
	}
	if folded == 0 {
		fmt.Fprintln(os.Stderr, "contractfold: no contract*.schema.json files found next to a built .serviceContracts entry — nothing to fold")
		return nil
	}
	if err := os.WriteFile(path, cur, 0o600); err != nil { // #nosec G703 -- path is a fixed constant, no trust boundary
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
