package mcpemit

import (
	"strings"
	"testing"
)

// a minimal single-op contract entry with an integer enum param and an optional
// (pointer) param, exercising all three F13 enrichments.
const sampleEntry = `{
  "$id": "archistrator://contract/sample",
  "title": "sample contract",
  "$defs": {
    "ProjectID": {"type": "string"},
    "ArtifactKind": {
      "type": "integer",
      "enum": [0, 1, 2],
      "x-enum-varnames": ["KindMission", "KindGlossary", "KindVolatilities"]
    },
    "ReviewFeedback": {"type": "object", "properties": {"notes": {"type": "string"}}},
    "SessionRef": {"type": "string"}
  },
  "interface": {
    "name": "SampleManager",
    "layer": "manager",
    "operations": [
      {
        "name": "RequestArtifactDraft",
        "params": [
          {"name": "projectID", "schema": {"$ref": "#/$defs/ProjectID"}},
          {"name": "kind", "schema": {"$ref": "#/$defs/ArtifactKind"}},
          {"name": "feedback", "pointer": true, "schema": {"$ref": "#/$defs/ReviewFeedback"}}
        ],
        "result": {"$ref": "#/$defs/SessionRef"},
        "error": true
      }
    ]
  }
}`

func generate(t *testing.T, docFn func(string) string) string {
	t.Helper()
	res, err := Generate([]byte(sampleEntry), Options{
		Package:       "sample",
		ManagerImport: "example.com/mgr/sample",
		OpDoc:         docFn,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return string(res.ToolsGo)
}

func TestGenerate_ThreadsDescription(t *testing.T) {
	src := generate(t, func(string) string { return "Draft one artifact." })
	if !strings.Contains(src, `Description: "Draft one artifact.", InputSchema: requestArtifactDraftInputSchema()`) {
		t.Errorf("tool registration missing real description + input schema wiring:\n%s", src)
	}
	// The boilerplate must be gone.
	if strings.Contains(src, "on the Sample manager.") {
		t.Errorf("boilerplate description still present")
	}
}

func TestGenerate_EnumSchemaCarriesValuesAndMeanings(t *testing.T) {
	src := generate(t, func(string) string { return "doc" })
	if !strings.Contains(src, "func enumSchemaArtifactKind() *jsonschema.Schema") {
		t.Fatalf("no enum schema helper emitted:\n%s", src)
	}
	if !strings.Contains(src, "[]any{0, 1, 2}") {
		t.Errorf("enum values not emitted")
	}
	for _, want := range []string{"0=KindMission", "1=KindGlossary", "2=KindVolatilities"} {
		if !strings.Contains(src, want) {
			t.Errorf("enum description missing meaning %q", want)
		}
	}
	if !strings.Contains(src, `s.Properties["kind"] = enumSchemaArtifactKind()`) {
		t.Errorf("enum param property not overridden onto the input schema")
	}
}

func TestGenerate_RequiredMatchesNonPointerParams(t *testing.T) {
	src := generate(t, func(string) string { return "doc" })
	// feedback is a pointer → optional; projectID + kind required.
	if !strings.Contains(src, `s.Required = []string{"projectID", "kind"}`) {
		t.Errorf("required list should be exactly the non-pointer params:\n%s", src)
	}
	// pointer param carries omitempty so its struct JSON shape matches the optional schema.
	if !strings.Contains(src, "`json:\"feedback,omitempty\"`") {
		t.Errorf("pointer param should be tagged omitempty")
	}
}

func TestGenerate_MissingDocIsAnError(t *testing.T) {
	_, err := Generate([]byte(sampleEntry), Options{
		Package:       "sample",
		ManagerImport: "example.com/mgr/sample",
		OpDoc:         func(string) string { return "" },
	})
	if err == nil {
		t.Fatalf("expected an error when an op has no documentation")
	}
	if !strings.Contains(err.Error(), "no documentation for operation") {
		t.Errorf("unexpected error: %v", err)
	}
}
