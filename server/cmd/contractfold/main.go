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
// MULTI-COMPONENT-PER-PACKAGE FILENAME CONVENTION: RA→RA imports are banned,
// so a secondary component sharing an existing package's goPackage (e.g. a
// second ResourceAccess re-homed onto a directory that already has a primary
// component) cannot also author that directory's `contract.schema.json` — the
// primary component already owns it. Both foldOne (the two-arg CLI form) and
// foldAll resolve this per key: among every `.serviceContracts` entry sharing
// one goPackage (existing entries, plus the key being folded if it is new),
// the alphabetically-first key is the PRIMARY component and reads
// `<dir>/contract.schema.json`; every other key is SECONDARY and reads
// `<dir>/contract.<key>.schema.json` instead (see schemaFilenameFor). This is
// the same ascending-key-order tie-break framework-go-app-generator/modelgen's
// genGroup uses to merge multiple contract entries sharing a goPackage into
// one contract.gen.go, so the two conventions stay aligned. A goPackage with
// only one component is always primary — no behavior change for the common
// case.
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
	"flag"
	"fmt"
	"os"
	"sort"

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

// schemaFilenameFor returns dir's schemagen output filename to read for key:
// the primary convention "<dir>/contract.schema.json" when key is the
// alphabetically-first key in sortedGroupKeys (the full set of contract keys
// sharing dir's goPackage, sorted ascending — see the package doc), or the
// secondary sibling convention "<dir>/contract.<key>.schema.json" otherwise.
// An empty or single-element sortedGroupKeys (the common, non-shared case) is
// always primary.
func schemaFilenameFor(dir, key string, sortedGroupKeys []string) string {
	if len(sortedGroupKeys) == 0 || sortedGroupKeys[0] == key {
		return dir + "/contract.schema.json"
	}
	return dir + "/contract." + key + ".schema.json"
}

// siblingGoPackageKeys scans projectRaw's `.serviceContracts` and returns, in
// ascending sorted order, every contract key that shares goPackage dir —
// including key itself even if it has no entry yet (the foldOne bootstrap
// case: a brand-new key still participates in its goPackage's primary/
// secondary determination). Used by foldOne, which — unlike foldAll — does not
// already have every key's goPackage grouped from a single upfront scan.
func siblingGoPackageKeys(projectRaw []byte, dir, key string) ([]string, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(projectRaw, &top); err != nil {
		return nil, fmt.Errorf("parse project.json: %w", err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		return nil, fmt.Errorf("parse .serviceContracts: %w", err)
	}
	keys := []string{key}
	for k, entry := range contracts {
		if k == key {
			continue
		}
		var meta struct {
			GoPackage string `json:"goPackage"`
		}
		if err := json.Unmarshal(entry, &meta); err != nil {
			return nil, fmt.Errorf("parse contract %q: %w", k, err)
		}
		if meta.GoPackage == dir {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// foldOne folds a single component's schemagen document — `<dir>/contract.schema.json`
// for a goPackage's primary component, `<dir>/contract.<key>.schema.json` for a
// secondary one sharing that goPackage (see schemaFilenameFor) — into
// project.json's `.serviceContracts[key]`, writing project.json back in place.
func foldOne(path, dir, key string, allowShrink bool) error {
	projectRaw, err := os.ReadFile(path) // #nosec G304 G703 -- path is a fixed constant or a developer-supplied CLI arg to a local codegen tool, no trust boundary
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	groupKeys, err := siblingGoPackageKeys(projectRaw, dir, key)
	if err != nil {
		return fmt.Errorf("determine schema filename for %s: %w", key, err)
	}
	schemaPath := schemaFilenameFor(dir, key, groupKeys)
	schemaRaw, err := os.ReadFile(schemaPath) // #nosec G304 G703 -- see above
	if err != nil {
		return fmt.Errorf("read %s (run `go run ./cmd/schemagen` first): %w", schemaPath, err)
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
// (found via its `goPackage`) with a matching on-disk schemagen document —
// `contract.schema.json` for a goPackage's primary component,
// `contract.<key>.schema.json` for a secondary one sharing that goPackage (see
// schemaFilenameFor). Components with no on-disk schema document (the
// steady-state norm — schemagen output is a transient bootstrap artifact, not
// committed) are skipped, not errored: this is the "re-fold whatever was
// re-seeded" bulk mode, never a surprise-creating one (creation requires the
// explicit two-arg form).
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
	goPkgKeys := map[string][]string{} // goPackage -> every contract key sharing it
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
		goPkgKeys[meta.GoPackage] = append(goPkgKeys[meta.GoPackage], k)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].key < targets[j].key })
	for _, keys := range goPkgKeys {
		sort.Strings(keys)
	}

	cur := projectRaw
	folded := 0
	for _, t := range targets {
		schemaPath := schemaFilenameFor(t.dir, t.key, goPkgKeys[t.dir])
		schemaRaw, err := os.ReadFile(schemaPath) // #nosec G304 G703 -- dir is a goPackage value already committed in project.json, no trust boundary
		if err != nil {
			if os.IsNotExist(err) {
				continue // steady state: no re-seed pending for this component
			}
			return fmt.Errorf("read %s: %w", schemaPath, err)
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
