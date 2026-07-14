package systemdesign

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// TestF26_GetSessionStateOutputAcceptsPopulatedModel is the regression guard for
// QA finding F26: the SDK infers the DraftModel.model json.RawMessage field as an
// array of 0-255 bytes, so a REAL artifact object in draft.model fails the tool's
// output validation. The emitted output schema must relax that carrier to a
// permissive schema so a populated object validates — while the un-relaxed
// inference must still reject it (proving the relaxation is load-bearing).
func TestF26_GetSessionStateOutputAcceptsPopulatedModel(t *testing.T) {
	// A session-state output whose draft.model carries a populated artifact object
	// (a mission), matching only the required keys (additionalProperties is false
	// throughout the inferred shape).
	payload := map[string]any{
		"result": map[string]any{
			"projectId":    "11111111-1111-1111-1111-111111111111",
			"artifactKind": 0,
			"stage":        0,
			"stageName":    "drafting",
			"activeRole":   0,
			"activeStep":   0,
			"round":        0,
			"draft": map[string]any{
				"kind":  "mission",
				"model": map[string]any{"mission": "Right software.", "vision": "..."},
			},
		},
	}

	// 1. The emitted (relaxed) output schema MUST accept the populated object.
	relaxed, err := getSessionStateOutputSchema().Resolve(nil)
	if err != nil {
		t.Fatalf("resolve emitted output schema: %v", err)
	}
	if err := relaxed.Validate(payload); err != nil {
		b, _ := json.MarshalIndent(getSessionStateOutputSchema(), "", "  ")
		t.Fatalf("emitted output schema rejected a populated draft.model (F26 not fixed): %v\n--- schema ---\n%s", err, b)
	}

	// 2. Guard: the RAW SDK inference (what the platform mcpgen relied on) MUST
	//    reject the same payload — otherwise the relaxation is a no-op and this
	//    test would not catch a regression.
	inferred, err := jsonschema.For[getSessionStateOutput](nil)
	if err != nil {
		t.Fatalf("infer raw output schema: %v", err)
	}
	rawResolved, err := inferred.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve raw output schema: %v", err)
	}
	if err := rawResolved.Validate(payload); err == nil {
		t.Fatalf("expected the un-relaxed inferred schema to REJECT a populated draft.model, but it accepted it — the relaxation is not load-bearing")
	}
}
