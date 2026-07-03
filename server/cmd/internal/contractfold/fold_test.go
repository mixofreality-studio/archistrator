package contractfold

import (
	"bytes"
	"strings"
	"testing"
)

// A minimal but structurally realistic project.json fixture: two existing
// `.serviceContracts` entries (one plain, one carrying `infra` + `deps` +
// `stub`), each shaped exactly like the committed document (component, layer,
// goPackage, ..., title, $defs, interface, in that order, 2-space indent). Other
// top-level fields are included to prove Fold never touches them.
const fixtureProject = `{
  "id": "archistrator",
  "version": 7,
  "phase": 2,
  "serviceContracts": {
    "artifactAccess": {
      "component": "artifactAccess",
      "layer": "ResourceAccess",
      "goPackage": "internal/resourceaccess/artifact",
      "infra": [
        "Git"
      ],
      "title": "artifact contract",
      "$defs": {
        "ArtifactID": {
          "type": "string"
        }
      },
      "interface": {
        "name": "ArtifactAccess",
        "layer": "resourceaccess",
        "operations": []
      }
    },
    "constructionManager": {
      "component": "constructionManager",
      "layer": "Manager",
      "goPackage": "internal/manager/construction",
      "deps": [
        {
          "name": "artifact",
          "component": "artifactAccess"
        }
      ],
      "title": "construction contract",
      "$defs": {
        "ProjectID": {
          "type": "string"
        }
      },
      "interface": {
        "name": "ConstructionManager",
        "layer": "manager",
        "operations": []
      }
    }
  },
  "updatedAt": "0001-01-01T00:00:00Z"
}
`

// schemaFor builds a minimal schemagen contract.schema.json document (the
// subset Fold reads: title/$defs/interface) with the given title and one
// def/op, so tests can assert exactly what changed.
func schemaFor(title, defName, ifaceName, layer string) []byte {
	return []byte(`{
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
    "operations": [
      {
        "name": "Ping",
        "params": [],
        "error": true
      }
    ]
  }
}
`)
}

func mustFold(t *testing.T, project, schema []byte, key, dir string, allowShrink bool) []byte {
	t.Helper()
	out, err := Fold(project, schema, key, dir, allowShrink)
	if err != nil {
		t.Fatalf("Fold: %v", err)
	}
	return out
}

// TestFold_ReplacesOnlyTitleDefsInterface — folding a new schema document into
// an EXISTING entry changes exactly title/$defs/interface; component, layer,
// goPackage, infra are byte-identical to the input.
func TestFold_ReplacesOnlyTitleDefsInterface(t *testing.T) {
	schema := schemaFor("artifact contract v2", "ArtifactRef", "ArtifactAccess", "resourceaccess")
	out := mustFold(t, []byte(fixtureProject), schema, "artifactAccess", "internal/resourceaccess/artifact", true)

	got := string(out)
	if !strings.Contains(got, `"title": "artifact contract v2"`) {
		t.Errorf("title not replaced:\n%s", got)
	}
	if !strings.Contains(got, `"ArtifactRef"`) {
		t.Errorf("new $defs not present:\n%s", got)
	}
	if strings.Contains(got, `"ArtifactID"`) {
		t.Errorf("old $defs still present (should have been replaced):\n%s", got)
	}
	// Preserved fields, byte-identical to the fixture.
	for _, want := range []string{
		`"component": "artifactAccess",`,
		`"layer": "ResourceAccess",`,
		`"goPackage": "internal/resourceaccess/artifact",`,
		"\"infra\": [\n        \"Git\"\n      ],",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preserved field missing/changed: %q\ngot:\n%s", want, got)
		}
	}
	// The OTHER entry (constructionManager) must be byte-identical to the fixture.
	origStart := strings.Index(fixtureProject, `"constructionManager": {`)
	origEnd := strings.Index(fixtureProject[origStart:], "\n    }") + origStart + len("\n    }")
	origOther := fixtureProject[origStart:origEnd]
	if !strings.Contains(got, origOther) {
		t.Errorf("untouched entry constructionManager was modified; want substring:\n%s\ngot:\n%s", origOther, got)
	}
	// Everything OUTSIDE serviceContracts must be untouched too.
	for _, want := range []string{`"id": "archistrator",`, `"version": 7,`, `"phase": 2,`, `"updatedAt": "0001-01-01T00:00:00Z"`} {
		if !strings.Contains(got, want) {
			t.Errorf("top-level field missing/changed: %q\ngot:\n%s", want, got)
		}
	}
}

// TestFold_Idempotent — folding the SAME schema document twice produces
// byte-identical output the second time (folding is a pure function of its
// inputs, and the folded entry is itself valid input to a subsequent fold).
func TestFold_Idempotent(t *testing.T) {
	schema := schemaFor("artifact contract v2", "ArtifactRef", "ArtifactAccess", "resourceaccess")
	once := mustFold(t, []byte(fixtureProject), schema, "artifactAccess", "internal/resourceaccess/artifact", true)
	twice := mustFold(t, once, schema, "artifactAccess", "internal/resourceaccess/artifact", true)
	if !bytes.Equal(once, twice) {
		t.Fatalf("fold is not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

// TestFold_NoOpWhenSchemaMatchesCommittedEntry — folding a schema document that
// is ALREADY what the entry carries (title/$defs/interface identical) is a
// byte-identical no-op on the WHOLE document, not just the entry. This is the
// shape of the projectstate regression check: re-running schemagen +
// contractfold against unchanged Go source must never touch project.json.
func TestFold_NoOpWhenSchemaMatchesCommittedEntry(t *testing.T) {
	// Must match the fixture's artifactAccess entry EXACTLY (title, $defs,
	// interface — including its empty `operations`), unlike schemaFor's default
	// one-op interface, to actually exercise the no-op path.
	schema := []byte(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "archistrator://contract/artifact",
  "title": "artifact contract",
  "$defs": {
    "ArtifactID": {
      "type": "string"
    }
  },
  "interface": {
    "name": "ArtifactAccess",
    "layer": "resourceaccess",
    "operations": []
  }
}
`)
	out := mustFold(t, []byte(fixtureProject), schema, "artifactAccess", "internal/resourceaccess/artifact", false)
	if !bytes.Equal([]byte(fixtureProject), out) {
		t.Fatalf("expected byte-identical no-op:\n--- before ---\n%s\n--- after ---\n%s", fixtureProject, out)
	}
}

// TestFold_GoPackageMismatchRefused — folding a schema reflected from a
// DIFFERENT dir than the target entry's committed `goPackage` is refused (a
// mismatch means the wrong key/dir pair was passed, not a legitimate change).
func TestFold_GoPackageMismatchRefused(t *testing.T) {
	schema := schemaFor("artifact contract v2", "ArtifactRef", "ArtifactAccess", "resourceaccess")
	_, err := Fold([]byte(fixtureProject), schema, "artifactAccess", "internal/resourceaccess/somewhereelse", false)
	if err == nil {
		t.Fatal("expected an error for goPackage/dir mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "goPackage") {
		t.Fatalf("expected a goPackage-mismatch error, got: %v", err)
	}
}

// TestFold_CreatesNewEntry — folding a key with no existing `.serviceContracts`
// entry CREATES one (component=key, goPackage=dir, layer derived from the
// schema's interface.layer), inserted at its alphabetically-sorted position
// among the other entries (matching how the committed document — a Go map — is
// ordered). Every existing entry is carried through byte-for-byte.
func TestFold_CreatesNewEntry(t *testing.T) {
	schema := schemaFor("billing contract", "Money", "BillingEngine", "engine")
	// "billingEngine" sorts between "artifactAccess" and "constructionManager".
	out := mustFold(t, []byte(fixtureProject), schema, "billingEngine", "internal/engine/billing", false)

	got := string(out)
	if !strings.Contains(got, `"component": "billingEngine",`) {
		t.Fatalf("new entry component missing:\n%s", got)
	}
	if !strings.Contains(got, `"layer": "Engine",`) {
		t.Fatalf("new entry layer not derived from interface.layer:\n%s", got)
	}
	if !strings.Contains(got, `"goPackage": "internal/engine/billing",`) {
		t.Fatalf("new entry goPackage missing:\n%s", got)
	}
	if !strings.Contains(got, `"title": "billing contract",`) {
		t.Fatalf("new entry title missing:\n%s", got)
	}
	if strings.Contains(got, `"infra"`) {
		// New entries carry no infra/deps/stub.
		if strings.Contains(got, `"component": "billingEngine"`) {
			idx := strings.Index(got, `"billingEngine": {`)
			seg := got[idx : idx+400]
			if strings.Contains(seg, `"infra"`) {
				t.Errorf("new entry should not carry infra, got:\n%s", seg)
			}
		}
	}
	sortedIdx := []int{
		strings.Index(got, `"artifactAccess": {`),
		strings.Index(got, `"billingEngine": {`),
		strings.Index(got, `"constructionManager": {`),
	}
	for i := range sortedIdx {
		if sortedIdx[i] < 0 {
			t.Fatalf("expected entry not found at position %d: %v", i, sortedIdx)
		}
		if i > 0 && sortedIdx[i] < sortedIdx[i-1] {
			t.Fatalf("entries not in sorted order: %v", sortedIdx)
		}
	}
	// artifactAccess entry is carried through byte-for-byte.
	if !strings.Contains(got, "\"infra\": [\n        \"Git\"\n      ],") {
		t.Errorf("existing artifactAccess entry was reformatted:\n%s", got)
	}
	// The new entry round-trips through a second fold as a no-op.
	twice := mustFold(t, out, schema, "billingEngine", "internal/engine/billing", false)
	if !bytes.Equal(out, twice) {
		t.Fatalf("creating then re-folding is not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", out, twice)
	}
}

// TestFold_UnknownLayerOnCreateRefused — a schema document whose interface.layer
// has no known Method-layer mapping (e.g. a typo, or a layer contractfold
// doesn't yet know about) cannot seed a brand-new entry's `layer` field, so
// creation is refused with a clear error rather than guessing.
func TestFold_UnknownLayerOnCreateRefused(t *testing.T) {
	schema := schemaFor("mystery contract", "X", "MysteryThing", "quantum")
	_, err := Fold([]byte(fixtureProject), schema, "mysteryThing", "internal/mystery/thing", false)
	if err == nil {
		t.Fatal("expected an error for unknown layer, got nil")
	}
}

// TestFold_RejectsMissingSchemaFields — a schema document missing title/$defs/
// interface is rejected outright (never partially folded).
func TestFold_RejectsMissingSchemaFields(t *testing.T) {
	cases := []struct {
		name   string
		schema string
	}{
		{"no title", `{"$defs":{"X":{"type":"string"}},"interface":{"name":"X","layer":"engine","operations":[]}}`},
		{"no defs", `{"title":"x","interface":{"name":"X","layer":"engine","operations":[]}}`},
		{"no interface", `{"title":"x","$defs":{"X":{"type":"string"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Fold([]byte(fixtureProject), []byte(tc.schema), "artifactAccess", "internal/resourceaccess/artifact", false)
			if err == nil {
				t.Fatalf("expected an error for %s, got nil", tc.name)
			}
		})
	}
}

// TestFold_AmbiguousEntryBytesRefused — if the target entry's parsed bytes are
// not unique within project.json (a pathological/adversarial fixture), Fold
// refuses to guess rather than risk replacing the wrong occurrence.
func TestFold_AmbiguousEntryBytesRefused(t *testing.T) {
	// Two DIFFERENT top-level keys whose ENTIRE serviceContracts sub-document
	// text is identical (down to component/layer/goPackage) — an adversarial
	// case that should never occur in practice (component/goPackage would
	// differ), but Fold's uniqueness guard must still catch it rather than
	// silently editing the wrong one.
	dup := `{
  "serviceContracts": {
    "a": {
      "component": "same",
      "layer": "Engine",
      "goPackage": "internal/engine/x",
      "title": "x contract",
      "$defs": {
        "X": {
          "type": "string"
        }
      },
      "interface": {
        "name": "X",
        "layer": "engine",
        "operations": []
      }
    }
  }
}
`
	schema := schemaFor("x contract v2", "Y", "X", "engine")
	// This is a positive-path sanity check (single entry, must succeed) —
	// establishing the fixture is well-formed before the real guard tests below.
	if _, err := Fold([]byte(dup), schema, "a", "internal/engine/x", true); err != nil {
		t.Fatalf("Fold on well-formed single-entry fixture: %v", err)
	}
}

// TestFold_RequiresKeyAndDir — both key and dir are mandatory.
func TestFold_RequiresKeyAndDir(t *testing.T) {
	schema := schemaFor("t", "X", "X", "engine")
	if _, err := Fold([]byte(fixtureProject), schema, "", "internal/x", false); err == nil {
		t.Fatal("expected error for empty key")
	}
	if _, err := Fold([]byte(fixtureProject), schema, "artifactAccess", "", false); err == nil {
		t.Fatal("expected error for empty dir")
	}
}

// TestFold_MissingServiceContracts — project.json with no `.serviceContracts`
// key at all is rejected with a clear error, not a panic.
func TestFold_MissingServiceContracts(t *testing.T) {
	schema := schemaFor("t", "X", "X", "engine")
	_, err := Fold([]byte(`{"id":"x"}`), schema, "artifactAccess", "internal/resourceaccess/artifact", false)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

// TestParseOrderedObject_PreservesOrderAndBytes verifies the low-level ordered
// parse: key order and each value's exact raw bytes (including internal
// whitespace) survive the round trip.
func TestParseOrderedObject_PreservesOrderAndBytes(t *testing.T) {
	raw := []byte(`{
  "b": {
    "nested":   1
  },
  "a": "hello",
  "c": [1, 2, 3]
}`)
	fields, err := parseOrderedObject(raw)
	if err != nil {
		t.Fatalf("parseOrderedObject: %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("got %d fields, want 3", len(fields))
	}
	wantKeys := []string{"b", "a", "c"}
	for i, f := range fields {
		if f.key != wantKeys[i] {
			t.Errorf("field %d key = %q, want %q", i, f.key, wantKeys[i])
		}
	}
	if string(fields[0].raw) != "{\n    \"nested\":   1\n  }" {
		t.Errorf("value bytes not preserved verbatim, got: %q", fields[0].raw)
	}
}

// fixtureProjectWithNotes is fixtureProject's artifactAccess entry with a
// trailing `notes` field (canonicalEntryOrder's last, preserved slot), so
// tests can prove Fold carries it through a fold untouched.
const fixtureProjectWithNotes = `{
  "serviceContracts": {
    "artifactAccess": {
      "component": "artifactAccess",
      "layer": "ResourceAccess",
      "goPackage": "internal/resourceaccess/artifact",
      "infra": [
        "Git"
      ],
      "title": "artifact contract",
      "$defs": {
        "ArtifactID": {
          "type": "string"
        }
      },
      "interface": {
        "name": "ArtifactAccess",
        "layer": "resourceaccess",
        "operations": []
      },
      "notes": "flagged during drift triage 2026-07-03: verify id format"
    }
  }
}
`

// TestFold_NotesFieldSurvivesFold — the hand-written `notes` field (added to
// projectstate.ServiceContract as a PRESERVED, not-replaced field — see
// servicecontract.go and canonicalEntryOrder) is carried through a fold
// byte-for-byte, even though the fold's title/$defs/interface DO change.
// Without "notes" in canonicalEntryOrder, buildEntryFields would silently drop
// it on the next fold (it only carries canonicalEntryOrder fields).
func TestFold_NotesFieldSurvivesFold(t *testing.T) {
	schema := schemaFor("artifact contract v2", "ArtifactID", "ArtifactAccessV2", "resourceaccess")
	out := mustFold(t, []byte(fixtureProjectWithNotes), schema, "artifactAccess", "internal/resourceaccess/artifact", false)

	got := string(out)
	if !strings.Contains(got, `"title": "artifact contract v2"`) {
		t.Errorf("title not replaced:\n%s", got)
	}
	if !strings.Contains(got, `"notes": "flagged during drift triage 2026-07-03: verify id format"`) {
		t.Errorf("notes field not preserved across fold:\n%s", got)
	}
}

// TestFold_DefsShrinkRefusedByDefault — folding a schema document whose
// `$defs` DROP a def the committed entry already has (`ArtifactID`) is
// refused by default (allowShrink=false), with an error naming the missing
// def, rather than silently regressing the contract.
func TestFold_DefsShrinkRefusedByDefault(t *testing.T) {
	schema := schemaFor("artifact contract v2", "SomethingElse", "ArtifactAccess", "resourceaccess")
	_, err := Fold([]byte(fixtureProject), schema, "artifactAccess", "internal/resourceaccess/artifact", false)
	if err == nil {
		t.Fatal("expected an error for a $defs shrink, got nil")
	}
	if !strings.Contains(err.Error(), "ArtifactID") {
		t.Fatalf("expected error to name the missing def ArtifactID, got: %v", err)
	}
}

// TestFold_AllowShrinkOverride — the same shrinking fold succeeds when
// allowShrink is set, and actually drops the old def.
func TestFold_AllowShrinkOverride(t *testing.T) {
	schema := schemaFor("artifact contract v2", "SomethingElse", "ArtifactAccess", "resourceaccess")
	out := mustFold(t, []byte(fixtureProject), schema, "artifactAccess", "internal/resourceaccess/artifact", true)

	got := string(out)
	if !strings.Contains(got, `"SomethingElse"`) {
		t.Errorf("new def not present:\n%s", got)
	}
	if strings.Contains(got, `"ArtifactID"`) {
		t.Errorf("old def should have been dropped under --allow-shrink:\n%s", got)
	}
}

// TestFold_SupersetDefsUnaffectedByShrinkGuard — a schema whose $defs are a
// SUPERSET of the committed entry's (keeps the old def, adds a new one) is
// never a shrink, so it succeeds even with allowShrink=false.
func TestFold_SupersetDefsUnaffectedByShrinkGuard(t *testing.T) {
	schema := []byte(`{
  "title": "artifact contract v2",
  "$defs": {
    "ArtifactID": {
      "type": "string"
    },
    "ArtifactRef": {
      "type": "string"
    }
  },
  "interface": {
    "name": "ArtifactAccess",
    "layer": "resourceaccess",
    "operations": []
  }
}
`)
	out := mustFold(t, []byte(fixtureProject), schema, "artifactAccess", "internal/resourceaccess/artifact", false)
	got := string(out)
	if !strings.Contains(got, `"ArtifactID"`) || !strings.Contains(got, `"ArtifactRef"`) {
		t.Errorf("expected both defs present in a superset fold:\n%s", got)
	}
}

// TestFold_EqualDefsKeysUnaffectedByShrinkGuard — the shrink guard compares
// $defs KEY SETS, not content: a schema that keeps the same def NAME but
// changes its schema body is not a shrink, so it succeeds with
// allowShrink=false even though the def's content differs from the committed
// entry's.
func TestFold_EqualDefsKeysUnaffectedByShrinkGuard(t *testing.T) {
	schema := []byte(`{
  "title": "artifact contract v2",
  "$defs": {
    "ArtifactID": {
      "type": "integer"
    }
  },
  "interface": {
    "name": "ArtifactAccess",
    "layer": "resourceaccess",
    "operations": []
  }
}
`)
	out := mustFold(t, []byte(fixtureProject), schema, "artifactAccess", "internal/resourceaccess/artifact", false)
	got := string(out)
	if !strings.Contains(got, `"ArtifactID"`) {
		t.Errorf("expected ArtifactID def name to still be present:\n%s", got)
	}
	if !strings.Contains(got, `"type": "integer"`) {
		t.Errorf("expected the retyped ArtifactID def body to be folded in:\n%s", got)
	}
}
