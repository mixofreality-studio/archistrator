package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// schemaDocFor builds a minimal schemagen contract document (title/$defs/
// interface) carrying the given interface name, shaped exactly like schemagen
// output so it survives contractfold.Fold in the integration test.
func schemaDocFor(title, defName, ifaceName, layer string) string {
	return `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "archistrator://contract/x",
  "title": "` + title + `",
  "$defs": {
    "` + defName + `": {
      "type": "string"
    }
  },
  "interface": {
    "name": "` + ifaceName + `",
    "layer": "` + layer + `",
    "operations": []
  }
}
`
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestKeyFromInterfaceName — the repo-wide key↔interface convention: the
// interface name with a lowered first letter is the `.serviceContracts` key.
func TestKeyFromInterfaceName(t *testing.T) {
	cases := map[string]string{
		"ProjectStateAccess":           "projectStateAccess",
		"ConstructionTransitionAccess": "constructionTransitionAccess",
		"ReviewEngine":                 "reviewEngine",
		"McpClient":                    "mcpClient",
		"":                             "",
	}
	for in, want := range cases {
		if got := keyFromInterfaceName(in); got != want {
			t.Errorf("keyFromInterfaceName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestResolveSchemaDoc_KeyedFileWins — the bootstrap-new-key case: a
// component authoring `contract.<key>.schema.json` is resolved to that keyed
// file, even when the dir ALSO has a plain `contract.schema.json` (owned by
// the primary). This holds regardless of the new key's lexical position.
func TestResolveSchemaDoc_KeyedFileWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "contract.schema.json"),
		schemaDocFor("primary", "P", "ProjectStateAccess", "resourceaccess"))
	writeFile(t, filepath.Join(dir, "contract.constructionTransitionAccess.schema.json"),
		schemaDocFor("secondary", "S", "ConstructionTransitionAccess", "resourceaccess"))

	path, raw, err := resolveSchemaDoc(dir, "constructionTransitionAccess")
	if err != nil {
		t.Fatalf("resolveSchemaDoc: %v", err)
	}
	if want := filepath.Join(dir, "contract.constructionTransitionAccess.schema.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if !strings.Contains(string(raw), `"ConstructionTransitionAccess"`) {
		t.Errorf("resolved wrong document:\n%s", raw)
	}
}

// TestResolveSchemaDoc_PlainFileStaysWithPrimary — the sticky-primary case:
// the existing primary (projectStateAccess) keeps resolving to the plain
// `contract.schema.json` even when a lexically-EARLIER sibling key
// (constructionTransitionAccess) exists in the same dir with its own keyed
// file. Under the old lexical rule the earlier key would have been
// re-designated primary and projectStateAccess stranded.
func TestResolveSchemaDoc_PlainFileStaysWithPrimary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "contract.schema.json"),
		schemaDocFor("primary", "P", "ProjectStateAccess", "resourceaccess"))
	writeFile(t, filepath.Join(dir, "contract.constructionTransitionAccess.schema.json"),
		schemaDocFor("secondary", "S", "ConstructionTransitionAccess", "resourceaccess"))

	path, raw, err := resolveSchemaDoc(dir, "projectStateAccess")
	if err != nil {
		t.Fatalf("resolveSchemaDoc: %v", err)
	}
	if want := filepath.Join(dir, "contract.schema.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if !strings.Contains(string(raw), `"ProjectStateAccess"`) {
		t.Errorf("resolved wrong document:\n%s", raw)
	}
}

// TestResolveSchemaDoc_LexicallyEarlierKeyCannotStealPlain — the exact
// failure the reviewer flagged: a NEW, lexically-earlier secondary key
// (constructionTransitionAccess < projectStateAccess) with NO keyed file of
// its own must NOT be handed the primary's plain `contract.schema.json`. The
// plain file's interface (ProjectStateAccess) maps to a different key, so
// resolution fails loudly, naming both keys and the keyed filename to author.
func TestResolveSchemaDoc_LexicallyEarlierKeyCannotStealPlain(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "contract.schema.json"),
		schemaDocFor("primary", "P", "ProjectStateAccess", "resourceaccess"))

	_, _, err := resolveSchemaDoc(dir, "constructionTransitionAccess")
	if err == nil {
		t.Fatal("expected an error (plain file belongs to projectStateAccess), got nil")
	}
	if !errors.Is(err, errPlainSchemaForeign) {
		t.Fatalf("expected errPlainSchemaForeign, got: %v", err)
	}
	for _, want := range []string{
		"ProjectStateAccess",
		"projectStateAccess",
		"constructionTransitionAccess",
		"contract.constructionTransitionAccess.schema.json",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

// TestResolveSchemaDoc_PlainFallbackForMatchingKey — a fresh single-component
// package (the common bootstrap): only the plain file exists and its
// interface maps to the requested key, so it resolves. No behavior change
// for the existing single-component flow.
func TestResolveSchemaDoc_PlainFallbackForMatchingKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "contract.schema.json"),
		schemaDocFor("t", "X", "ReviewEngine", "engine"))

	path, _, err := resolveSchemaDoc(dir, "reviewEngine")
	if err != nil {
		t.Fatalf("resolveSchemaDoc: %v", err)
	}
	if want := filepath.Join(dir, "contract.schema.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

// TestResolveSchemaDoc_NeitherFile — no schema document at all resolves to a
// wrapped fs.ErrNotExist (foldAll's skip signal), naming both looked-for paths.
func TestResolveSchemaDoc_NeitherFile(t *testing.T) {
	dir := t.TempDir()
	_, _, err := resolveSchemaDoc(dir, "reviewEngine")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected wrapped fs.ErrNotExist, got: %v", err)
	}
	for _, want := range []string{"contract.reviewEngine.schema.json", "contract.schema.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}
}

// TestResolveSchemaDoc_PlainFileNoInterfaceName — a plain file with no
// interface.name cannot be ownership-verified, so it errors rather than
// silently folding into the wrong component.
func TestResolveSchemaDoc_PlainFileNoInterfaceName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "contract.schema.json"), `{"title":"x","$defs":{"X":{"type":"string"}}}`)
	_, _, err := resolveSchemaDoc(dir, "reviewEngine")
	if err == nil {
		t.Fatal("expected an error for a plain file with no interface.name, got nil")
	}
	if !strings.Contains(err.Error(), "interface.name") {
		t.Fatalf("expected error to name interface.name, got: %v", err)
	}
}

// projectFixtureFor builds a two-entries-one-goPackage project.json (both
// entries homed on dir) in the canonical committed shape, for the foldAll
// integration test. constructionTransitionAccess sorts BEFORE
// projectStateAccess — the exact next-task shape.
func projectFixtureFor(dir string) string {
	return `{
  "serviceContracts": {
    "constructionTransitionAccess": {
      "component": "constructionTransitionAccess",
      "layer": "ResourceAccess",
      "goPackage": "` + dir + `",
      "title": "old secondary title",
      "$defs": {
        "S": {
          "type": "string"
        }
      },
      "interface": {
        "name": "ConstructionTransitionAccess",
        "layer": "resourceaccess",
        "operations": []
      }
    },
    "projectStateAccess": {
      "component": "projectStateAccess",
      "layer": "ResourceAccess",
      "goPackage": "` + dir + `",
      "title": "old primary title",
      "$defs": {
        "P": {
          "type": "string"
        }
      },
      "interface": {
        "name": "ProjectStateAccess",
        "layer": "resourceaccess",
        "operations": []
      }
    }
  }
}
`
}

// TestFoldAll_StickyPrimaryWithLexicallyEarlierSecondary — end-to-end foldAll
// over the next task's exact shape: two entries share one goPackage; the
// primary's re-seed sits at plain contract.schema.json, the (lexically
// earlier) secondary's at contract.constructionTransitionAccess.schema.json.
// foldAll must fold EACH document into ITS OWN entry — the secondary walks
// first (sorted key order) yet must not consume the plain file.
func TestFoldAll_StickyPrimaryWithLexicallyEarlierSecondary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "contract.schema.json"),
		schemaDocFor("new primary title", "P", "ProjectStateAccess", "resourceaccess"))
	writeFile(t, filepath.Join(dir, "contract.constructionTransitionAccess.schema.json"),
		schemaDocFor("new secondary title", "S", "ConstructionTransitionAccess", "resourceaccess"))

	projectPath := filepath.Join(t.TempDir(), "project.json")
	writeFile(t, projectPath, projectFixtureFor(dir))

	if err := foldAll(projectPath, false); err != nil {
		t.Fatalf("foldAll: %v", err)
	}

	out, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, `"title": "new primary title"`) {
		t.Errorf("primary entry not folded from plain contract.schema.json:\n%s", got)
	}
	if !strings.Contains(got, `"title": "new secondary title"`) {
		t.Errorf("secondary entry not folded from its keyed file:\n%s", got)
	}
	if strings.Contains(got, "old primary title") || strings.Contains(got, "old secondary title") {
		t.Errorf("stale titles remain — a document folded into the wrong entry:\n%s", got)
	}
}

// TestFoldAll_ForeignPlainFileSkippedForSecondary — foldAll with ONLY the
// primary's plain file on disk: the secondary entry (walked first, sorted
// order) must SKIP it, and the primary must still fold it. The secondary's
// committed entry stays untouched.
func TestFoldAll_ForeignPlainFileSkippedForSecondary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "contract.schema.json"),
		schemaDocFor("new primary title", "P", "ProjectStateAccess", "resourceaccess"))

	projectPath := filepath.Join(t.TempDir(), "project.json")
	writeFile(t, projectPath, projectFixtureFor(dir))

	if err := foldAll(projectPath, false); err != nil {
		t.Fatalf("foldAll: %v", err)
	}

	out, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, `"title": "new primary title"`) {
		t.Errorf("primary entry not folded:\n%s", got)
	}
	if !strings.Contains(got, `"title": "old secondary title"`) {
		t.Errorf("secondary entry should be untouched (no keyed file on disk):\n%s", got)
	}
	if n := strings.Count(got, `"name": "ProjectStateAccess"`); n != 1 {
		t.Errorf("primary interface appears %d times, want exactly 1 (folded into one entry only):\n%s", n, got)
	}
}
