package main

import (
	"encoding/json"
	"testing"
)

// TestModelDocumentDropsClientContracts: a Client-layer contract is the test surface of
// a generated client (spec §12 B1 — web actions, MCP tools); its Go is clientgen's, so
// modelgen must emit no model, interface or fake for it, even when it names a
// goPackage (the MCP client's scenario tests live in one). Every other contract reaches
// modelgen byte-identical.
func TestModelDocumentDropsClientContracts(t *testing.T) {
	manager := `{"component":"orderManager","layer":"Manager","goPackage":"internal/manager/order","interface":{"name":"OrderManager","layer":"manager","operations":[]}}`
	raw := []byte(`{"id":"p","serviceContracts":{` +
		`"orderManager":` + manager + `,` +
		`"shopTools":{"component":"shopTools","layer":"Client","goPackage":"internal/client/mcp/order","interface":{"name":"ShopTools","layer":"client","operations":[{"name":"OrderGetOrder","params":[],"error":true}]}},` +
		`"shopClient":{"component":"shopClient","layer":"Client","interface":{"name":"ShopClient","layer":"client","operations":[]}}` +
		`}}`)
	got, err := modelDocument(raw)
	if err != nil {
		t.Fatalf("modelDocument: %v", err)
	}
	var doc struct {
		ID               string                     `json:"id"`
		ServiceContracts map[string]json.RawMessage `json:"serviceContracts"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.ID != "p" {
		t.Fatalf("the rest of the document must survive, got id %q", doc.ID)
	}
	if len(doc.ServiceContracts) != 1 {
		t.Fatalf("want only the manager contract, got %d: %v", len(doc.ServiceContracts), keys(doc.ServiceContracts))
	}
	if string(doc.ServiceContracts["orderManager"]) != manager {
		t.Fatalf("a non-client contract must reach modelgen byte-identical, got %s", doc.ServiceContracts["orderManager"])
	}
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
