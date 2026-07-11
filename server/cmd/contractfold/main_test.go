package main

import (
	"strings"
	"testing"
)

// TestSchemaFilenameFor_SingleComponent — a goPackage with only one component
// (the overwhelmingly common case) always reads the plain "contract.schema.json"
// convention, regardless of key, whether sortedGroupKeys is a one-element slice
// or empty (the "no siblings" shorthand).
func TestSchemaFilenameFor_SingleComponent(t *testing.T) {
	cases := []struct {
		name            string
		sortedGroupKeys []string
	}{
		{"one-element group", []string{"projectStateAccess"}},
		{"empty group", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := schemaFilenameFor("internal/resourceaccess/projectstate", "projectStateAccess", tc.sortedGroupKeys)
			want := "internal/resourceaccess/projectstate/contract.schema.json"
			if got != want {
				t.Errorf("schemaFilenameFor() = %q, want %q", got, want)
			}
		})
	}
}

// TestSchemaFilenameFor_MultiComponent — a goPackage shared by two or more
// components: the alphabetically-first key (the PRIMARY component) reads
// "contract.schema.json"; every other (SECONDARY) key reads
// "contract.<key>.schema.json".
func TestSchemaFilenameFor_MultiComponent(t *testing.T) {
	dir := "internal/resourceaccess/projectstate"
	// "constructionTransitionAccess" < "projectStateAccess" alphabetically.
	group := []string{"constructionTransitionAccess", "projectStateAccess"}

	if got, want := schemaFilenameFor(dir, "constructionTransitionAccess", group), dir+"/contract.schema.json"; got != want {
		t.Errorf("primary key: schemaFilenameFor() = %q, want %q", got, want)
	}
	if got, want := schemaFilenameFor(dir, "projectStateAccess", group), dir+"/contract.projectStateAccess.schema.json"; got != want {
		t.Errorf("secondary key: schemaFilenameFor() = %q, want %q", got, want)
	}
}

// TestSchemaFilenameFor_ThreeComponents proves the convention scales past two
// siblings: exactly one primary, every other key gets its own <key>-suffixed
// filename.
func TestSchemaFilenameFor_ThreeComponents(t *testing.T) {
	dir := "internal/resourceaccess/projectstate"
	group := []string{"alphaAccess", "betaAccess", "gammaAccess"}

	want := map[string]string{
		"alphaAccess": dir + "/contract.schema.json",
		"betaAccess":  dir + "/contract.betaAccess.schema.json",
		"gammaAccess": dir + "/contract.gammaAccess.schema.json",
	}
	for key, want := range want {
		if got := schemaFilenameFor(dir, key, group); got != want {
			t.Errorf("schemaFilenameFor(%q) = %q, want %q", key, got, want)
		}
	}
}

// projectFixtureTwoSharedGoPackage declares two `.serviceContracts` entries
// sharing one goPackage (primaryAccess, secondaryAccess) plus one unrelated
// entry (otherManager) on a different goPackage, for siblingGoPackageKeys
// tests.
const projectFixtureTwoSharedGoPackage = `{
  "serviceContracts": {
    "primaryAccess": {
      "component": "primaryAccess",
      "layer": "ResourceAccess",
      "goPackage": "internal/resourceaccess/shared",
      "title": "t", "$defs": {"X": {"type": "string"}},
      "interface": {"name": "PrimaryAccess", "layer": "resourceaccess", "operations": []}
    },
    "secondaryAccess": {
      "component": "secondaryAccess",
      "layer": "ResourceAccess",
      "goPackage": "internal/resourceaccess/shared",
      "title": "t", "$defs": {"X": {"type": "string"}},
      "interface": {"name": "SecondaryAccess", "layer": "resourceaccess", "operations": []}
    },
    "otherManager": {
      "component": "otherManager",
      "layer": "Manager",
      "goPackage": "internal/manager/other",
      "title": "t", "$defs": {"X": {"type": "string"}},
      "interface": {"name": "OtherManager", "layer": "manager", "operations": []}
    }
  }
}`

// TestSiblingGoPackageKeys_ExistingPair — both keys of an already-committed
// shared-goPackage pair are returned, sorted, and the unrelated entry on a
// different goPackage is excluded.
func TestSiblingGoPackageKeys_ExistingPair(t *testing.T) {
	got, err := siblingGoPackageKeys([]byte(projectFixtureTwoSharedGoPackage), "internal/resourceaccess/shared", "secondaryAccess")
	if err != nil {
		t.Fatalf("siblingGoPackageKeys: %v", err)
	}
	want := []string{"primaryAccess", "secondaryAccess"}
	if !equalStrings(got, want) {
		t.Errorf("siblingGoPackageKeys() = %v, want %v", got, want)
	}
}

// TestSiblingGoPackageKeys_BootstrapNewKey — a key that does NOT yet have a
// `.serviceContracts` entry (the foldOne bootstrap case) still participates:
// it is included alongside any existing entries already sharing dir, so a
// brand-new secondary component is correctly recognized as secondary (or
// primary) before its entry exists.
func TestSiblingGoPackageKeys_BootstrapNewKey(t *testing.T) {
	got, err := siblingGoPackageKeys([]byte(projectFixtureTwoSharedGoPackage), "internal/resourceaccess/shared", "aBrandNewAccess")
	if err != nil {
		t.Fatalf("siblingGoPackageKeys: %v", err)
	}
	// "aBrandNewAccess" sorts before both existing keys.
	want := []string{"aBrandNewAccess", "primaryAccess", "secondaryAccess"}
	if !equalStrings(got, want) {
		t.Errorf("siblingGoPackageKeys() = %v, want %v", got, want)
	}
}

// TestSiblingGoPackageKeys_NoSiblings — a key/dir combination with no other
// entry sharing the goPackage returns just the key itself.
func TestSiblingGoPackageKeys_NoSiblings(t *testing.T) {
	got, err := siblingGoPackageKeys([]byte(projectFixtureTwoSharedGoPackage), "internal/resourceaccess/lonely", "lonelyAccess")
	if err != nil {
		t.Fatalf("siblingGoPackageKeys: %v", err)
	}
	want := []string{"lonelyAccess"}
	if !equalStrings(got, want) {
		t.Errorf("siblingGoPackageKeys() = %v, want %v", got, want)
	}
}

// TestSiblingGoPackageKeys_MalformedProjectRejected — malformed JSON is
// reported as an error, never silently swallowed into an empty result.
func TestSiblingGoPackageKeys_MalformedProjectRejected(t *testing.T) {
	_, err := siblingGoPackageKeys([]byte(`{"serviceContracts": "not an object"}`), "internal/resourceaccess/shared", "x")
	if err == nil {
		t.Fatal("expected an error for malformed .serviceContracts, got nil")
	}
	if !strings.Contains(err.Error(), "serviceContracts") {
		t.Fatalf("expected error to name .serviceContracts, got: %v", err)
	}
}

// TestSchemaFilenameFor_EndToEndWithSiblingGoPackageKeys wires the two
// helpers together the way foldOne does, over the two-component fixture, to
// prove the primary/secondary filename resolution end to end.
func TestSchemaFilenameFor_EndToEndWithSiblingGoPackageKeys(t *testing.T) {
	dir := "internal/resourceaccess/shared"
	for key, want := range map[string]string{
		"primaryAccess":   dir + "/contract.schema.json",
		"secondaryAccess": dir + "/contract.secondaryAccess.schema.json",
	} {
		groupKeys, err := siblingGoPackageKeys([]byte(projectFixtureTwoSharedGoPackage), dir, key)
		if err != nil {
			t.Fatalf("siblingGoPackageKeys(%q): %v", key, err)
		}
		if got := schemaFilenameFor(dir, key, groupKeys); got != want {
			t.Errorf("key %q: schemaFilenameFor() = %q, want %q", key, got, want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
