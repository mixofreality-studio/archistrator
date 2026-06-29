package projectstate_test

// servicecontract_test.go verifies the ServiceContract contract-document model
// survives a full EncodeProjectJSON → DecodeProjectJSON round-trip, including a
// byte-identical second pass. Mirrors the TestActivityConstruction_RoundTrip
// discipline: no git store, no mocks — just the public codec seam.

import (
	"bytes"
	"encoding/json"
	"testing"

	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestServiceContract_RoundTrip — a Project with one ServiceContracts["artifactAccess"]
// entry (a contract document: title + $defs + interface with a param + result)
// survives EncodeProjectJSON → DecodeProjectJSON intact and re-encodes byte-identically.
func TestServiceContract_RoundTrip(t *testing.T) {
	p := ps.Project{}
	p.ServiceContracts = map[string]ps.ServiceContract{
		"artifactAccess": {
			Component: "artifactAccess",
			Layer:     "ResourceAccess",
			GoPackage: "internal/resourceaccess/artifact",
			Title:     "artifact contract",
			Defs: map[string]json.RawMessage{
				"ArtifactID": json.RawMessage(`{"type":"string"}`),
				"Artifact":   json.RawMessage(`{"type":"object","properties":{"id":{"$ref":"#/$defs/ArtifactID"}},"required":["id"],"additionalProperties":false}`),
			},
			Interface: ps.ContractInterface{
				Name:  "ArtifactAccess",
				Layer: "resourceaccess",
				Operations: []ps.ContractOperation{
					{Name: "Cancel", Params: nil, Error: true},
					{
						Name: "Read",
						Params: []ps.ContractParam{
							{Name: "id", Schema: json.RawMessage(`{"$ref":"#/$defs/ArtifactID"}`)},
						},
						Result: json.RawMessage(`{"$ref":"#/$defs/Artifact"}`),
						Error:  true,
					},
				},
			},
		},
	}

	raw, err := ps.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	got, ok, err := ps.DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON: ok=false, want true")
	}

	sc, found := got.ServiceContracts["artifactAccess"]
	if !found {
		t.Fatal("ServiceContracts[artifactAccess] absent after round-trip")
	}
	if sc.Component != "artifactAccess" {
		t.Fatalf("Component = %q, want artifactAccess", sc.Component)
	}
	if sc.GoPackage != "internal/resourceaccess/artifact" {
		t.Fatalf("GoPackage = %q, want internal/resourceaccess/artifact", sc.GoPackage)
	}
	if sc.Title != "artifact contract" {
		t.Fatalf("Title = %q, want artifact contract", sc.Title)
	}
	if len(sc.Defs) != 2 {
		t.Fatalf("Defs len = %d, want 2", len(sc.Defs))
	}
	if len(sc.Interface.Operations) != 2 {
		t.Fatalf("Operations len = %d, want 2", len(sc.Interface.Operations))
	}
	read := sc.Interface.Operations[1]
	if read.Name != "Read" {
		t.Fatalf("Operations[1].Name = %q, want Read", read.Name)
	}
	if len(read.Params) != 1 || read.Params[0].Name != "id" {
		t.Fatalf("Operations[1].Params unexpected: %+v", read.Params)
	}
	if len(read.Result) == 0 {
		t.Fatal("Operations[1].Result absent after round-trip")
	}

	// BYTE-IDENTICAL second pass: re-encoding the decoded aggregate yields the
	// identical bytes (the persistence invariant).
	raw2, err := ps.EncodeProjectJSON(got)
	if err != nil {
		t.Fatalf("EncodeProjectJSON (2nd pass): %v", err)
	}
	if !bytes.Equal(raw, raw2) {
		t.Fatalf("round-trip not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", raw, raw2)
	}
}
