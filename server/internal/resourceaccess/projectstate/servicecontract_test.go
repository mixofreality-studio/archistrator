package projectstate_test

// servicecontract_test.go verifies the ServiceContract typed model survives a
// full EncodeProjectJSON → DecodeProjectJSON round-trip. Mirrors the
// TestActivityConstruction_RoundTrip discipline: no git store, no mocks —
// just the public codec seam.

import (
	"testing"

	ps "github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

// TestServiceContract_RoundTrip — a Project with one ServiceContracts["webClient"]
// entry (ops + revision + inbound + outbound) survives EncodeProjectJSON →
// DecodeProjectJSON intact.
func TestServiceContract_RoundTrip(t *testing.T) {
	p := ps.Project{}
	p.ServiceContracts = map[string]ps.ServiceContract{
		"webClient": {
			Component:  "webClient",
			Layer:      "Client",
			Stereotype: "SPA entrypoint",
			Volatility: "UI rendering surface",
			Status:     "FROZEN",
			Inbound: []ps.ContractParty{
				{Name: "User", Layer: "external", How: ""},
			},
			Outbound: []ps.ContractParty{
				{Name: "archistratorManager", Layer: "Manager", How: "Activity-wrapped · HTTP POST /api/projects"},
				{Name: "projectStateAccess", Layer: "ResourceAccess", How: "direct, by value"},
			},
			Ops: []ps.ContractOp{
				{Signature: "CreateProject(name string) (ProjectID, error)", Stereotype: "Manager Temporal workflow", Note: "idempotent via key"},
				{Signature: "ReadProject(id ProjectID) (Project, error)", Stereotype: "Manager query", Note: "cache-friendly"},
				{
					Signature:  "UpdateProject(cmd UpdateProjectCmd) error",
					Stereotype: "Manager Temporal workflow",
					Note:       "caller-minted idempotency key",
					Inputs: []ps.ContractStruct{
						{
							Name: "UpdateProjectCmd",
							Fields: []ps.GoField{
								{Name: "IdempotencyKey", Type: "string", Note: "caller-minted"},
								{Name: "ProjectID", Type: "ProjectID"},
								{Name: "Name", Type: "string"},
							},
						},
					},
					Outputs: []ps.ContractStruct{
						{
							Name: "UpdateProjectResponse",
							Fields: []ps.GoField{
								{Name: "UpdatedAt", Type: "time.Time"},
							},
						},
					},
				},
			},
			DataContracts: []string{"ProjectSummary", "ArtifactSlot"},
			ErrorModel:    "fwra.Error kind-discriminated",
			Idempotency:   "caller-minted UUID key",
			Revisions: []ps.ContractRevision{
				{Rev: "r1", At: "2026-06-17", By: "architect", ByActivity: "D-CW", Summary: "initial freeze"},
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

	sc, found := got.ServiceContracts["webClient"]
	if !found {
		t.Fatal("ServiceContracts[webClient] absent after round-trip")
	}

	// ops length
	if len(sc.Ops) != 3 {
		t.Fatalf("Ops len = %d, want 3", len(sc.Ops))
	}

	// third op carries Inputs/Outputs
	op := sc.Ops[2]
	if len(op.Inputs) != 1 {
		t.Fatalf("Ops[2].Inputs len = %d, want 1", len(op.Inputs))
	}
	if op.Inputs[0].Name != "UpdateProjectCmd" {
		t.Fatalf("Ops[2].Inputs[0].Name = %q, want UpdateProjectCmd", op.Inputs[0].Name)
	}
	if len(op.Inputs[0].Fields) != 3 {
		t.Fatalf("Ops[2].Inputs[0].Fields len = %d, want 3", len(op.Inputs[0].Fields))
	}
	if op.Inputs[0].Fields[0].Note != "caller-minted" {
		t.Fatalf("Ops[2].Inputs[0].Fields[0].Note = %q, want caller-minted", op.Inputs[0].Fields[0].Note)
	}
	if len(op.Outputs) != 1 {
		t.Fatalf("Ops[2].Outputs len = %d, want 1", len(op.Outputs))
	}
	if op.Outputs[0].Name != "UpdateProjectResponse" {
		t.Fatalf("Ops[2].Outputs[0].Name = %q, want UpdateProjectResponse", op.Outputs[0].Name)
	}

	// revision Rev
	if len(sc.Revisions) != 1 {
		t.Fatalf("Revisions len = %d, want 1", len(sc.Revisions))
	}
	if sc.Revisions[0].Rev != "r1" {
		t.Fatalf("Revisions[0].Rev = %q, want r1", sc.Revisions[0].Rev)
	}

	// outbound How
	if len(sc.Outbound) != 2 {
		t.Fatalf("Outbound len = %d, want 2", len(sc.Outbound))
	}
	if sc.Outbound[0].How != "Activity-wrapped · HTTP POST /api/projects" {
		t.Fatalf("Outbound[0].How = %q, unexpected", sc.Outbound[0].How)
	}

	// status + component identity
	if sc.Status != "FROZEN" {
		t.Fatalf("Status = %q, want FROZEN", sc.Status)
	}
	if sc.Component != "webClient" {
		t.Fatalf("Component = %q, want webClient", sc.Component)
	}
}
