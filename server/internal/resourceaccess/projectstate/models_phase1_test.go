package projectstate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestDeploymentTopology_JSONRoundTrip proves the typed deployment topology on
// OperationalConcepts serializes its enum fields as STRING wire names (matching
// the Layer/ComponentKind/CallMode convention via enumjson.go) and round-trips
// losslessly through json.Marshal/json.Unmarshal.
func TestDeploymentTopology_JSONRoundTrip(t *testing.T) {
	compID := Slug("ProjectStateAccess")

	original := &OperationalConcepts{
		Decisions: []OperationalDecision{
			{Topic: "communication topology", Decision: "synchronous", JustifyingObjective: 1},
		},
		Deployment: DeploymentTopology{
			DeliveryStyle: StyleBoth,
			Environments: []DeploymentEnvironment{
				{
					Profile: ProfileCloud,
					Title:   "Production (cloud)",
					Nodes: []DeploymentNode{
						{
							Name:       "k8s-cluster",
							Technology: "Kubernetes",
							Children: []DeploymentNode{
								{
									Name:       "archistrator-ns",
									Technology: "Namespace",
									Instances: []ContainerInstance{
										{ComponentID: compID, Note: "server pod"},
									},
								},
							},
						},
					},
				},
				{
					Profile: ProfileTest,
					Title:   "Test (ephemeral)",
					Nodes: []DeploymentNode{
						{Name: "test-harness", Technology: "in-memory"},
					},
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	js := string(data)
	// Enum fields must render as the expected STRING tokens, not integers.
	for _, want := range []string{
		`"deliveryStyle":"both"`,
		`"profile":"cloud"`,
		`"profile":"test"`,
		`"componentId":"` + compID + `"`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("marshalled JSON missing %s\nfull: %s", want, js)
		}
	}

	var back OperationalConcepts
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(*original, back) {
		t.Fatalf("round-trip mismatch:\n got  %#v\n want %#v", back, *original)
	}
}

// TestDeploymentEnums_WireTokens pins the exact string tokens for each enum value
// and confirms unknown wire names error the same way the existing enums do.
func TestDeploymentEnums_WireTokens(t *testing.T) {
	styleCases := map[DeliveryStyle]string{
		StyleCloud: "cloud",
		StyleLocal: "local",
		StyleBoth:  "both",
	}
	for v, want := range styleCases {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal DeliveryStyle(%d): %v", v, err)
		}
		if got := string(data); got != `"`+want+`"` {
			t.Fatalf("DeliveryStyle(%d) marshalled as %s, want %q", v, got, want)
		}
		var back DeliveryStyle
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if back != v {
			t.Fatalf("DeliveryStyle round-trip: got %d, want %d", back, v)
		}
	}

	profileCases := map[DeploymentProfile]string{
		ProfileCloud: "cloud",
		ProfileLocal: "local",
		ProfileTest:  "test",
	}
	for v, want := range profileCases {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal DeploymentProfile(%d): %v", v, err)
		}
		if got := string(data); got != `"`+want+`"` {
			t.Fatalf("DeploymentProfile(%d) marshalled as %s, want %q", v, got, want)
		}
		var back DeploymentProfile
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if back != v {
			t.Fatalf("DeploymentProfile round-trip: got %d, want %d", back, v)
		}
	}
}

// TestDeploymentEnums_InvalidWireName confirms an unrecognized string token errors,
// mirroring unmarshalEnum's "is not a recognized ... wire name" behaviour.
func TestDeploymentEnums_InvalidWireName(t *testing.T) {
	var s DeliveryStyle
	if err := json.Unmarshal([]byte(`"hybrid"`), &s); err == nil {
		t.Fatal("expected error unmarshalling invalid DeliveryStyle wire name, got nil")
	}
	var p DeploymentProfile
	if err := json.Unmarshal([]byte(`"staging"`), &p); err == nil {
		t.Fatal("expected error unmarshalling invalid DeploymentProfile wire name, got nil")
	}
}
