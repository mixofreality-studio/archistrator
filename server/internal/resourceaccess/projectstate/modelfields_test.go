package projectstate

import (
	"strings"
	"testing"
)

// validSystemJSON is a minimal, well-formed System model with explicit, consistent
// enum fields on every component/relationship/dynamic view.
const validSystemJSON = `{
  "components": [
    {"id":"web-client","name":"WebClient","kind":"client","layer":"client","encapsulates":"","atomicBusinessVerbs":[]},
    {"id":"order-mgr","name":"OrderManager","kind":"manager","layer":"manager","encapsulates":"the order workflow","atomicBusinessVerbs":[]},
    {"id":"pricing-eng","name":"PricingEngine","kind":"engine","layer":"engine","encapsulates":"pricing","atomicBusinessVerbs":[]}
  ],
  "relationships": [
    {"from":"web-client","to":"order-mgr","mode":"sync","label":"places order"},
    {"from":"order-mgr","to":"pricing-eng","mode":"sync","label":"prices"}
  ],
  "dynamicViews": [
    {"useCaseId":"uc1","key":"uc1-place-order","title":"Place order","participants":["web-client","order-mgr"],
     "edges":[{"from":"web-client","to":"order-mgr","mode":"sync","label":"places order"}]}
  ]
}`

func TestRequireModelFields_ValidSystem(t *testing.T) {
	if err := RequireModelFields(KindSystem, []byte(validSystemJSON)); err != nil {
		t.Fatalf("valid system should pass, got: %v", err)
	}
}

func TestRequireModelFields_MissingLayer(t *testing.T) {
	// The live F81 case: a manager component that omits "layer". The strict struct decode
	// would silently default it to LayerClient; the presence+consistency check must reject.
	j := `{
      "components": [
        {"id":"order-mgr","name":"OrderManager","kind":"manager","encapsulates":"x","atomicBusinessVerbs":[]}
      ],
      "relationships": [],
      "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil {
		t.Fatal("a manager component missing its layer must be rejected")
	}
	if !strings.Contains(err.Error(), "layer") {
		t.Fatalf("error should name the missing layer field, got: %v", err)
	}
}

func TestRequireModelFields_LayerKindMismatch(t *testing.T) {
	// layer present but inconsistent with kind (manager kind, client layer) — the
	// signature of an omitted-then-defaulted layer that happened to be re-serialized.
	j := `{
      "components": [
        {"id":"order-mgr","name":"OrderManager","kind":"manager","layer":"client","encapsulates":"x","atomicBusinessVerbs":[]}
      ],
      "relationships": [],
      "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil {
		t.Fatal("layer inconsistent with kind must be rejected")
	}
	if !strings.Contains(err.Error(), "manager") || !strings.Contains(err.Error(), "client") {
		t.Fatalf("error should explain the kind/layer mismatch, got: %v", err)
	}
}

func TestRequireModelFields_MissingMode(t *testing.T) {
	j := `{
      "components": [
        {"id":"a","name":"A","kind":"manager","layer":"manager"},
        {"id":"b","name":"B","kind":"engine","layer":"engine"}
      ],
      "relationships": [ {"from":"a","to":"b","label":"x"} ],
      "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("a relationship missing its mode must be rejected naming mode, got: %v", err)
	}
}

func TestRequireModelFields_MissingKind(t *testing.T) {
	j := `{
      "components": [
        {"id":"a","name":"A","layer":"manager"}
      ],
      "relationships": [],
      "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("a component missing its kind must be rejected naming kind, got: %v", err)
	}
}

func TestRequireModelFields_UnrecognizedLayer(t *testing.T) {
	j := `{
      "components": [
        {"id":"a","name":"A","kind":"manager","layer":"bogus"}
      ],
      "relationships": [],
      "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "layer") {
		t.Fatalf("an unrecognized layer wire value must be rejected, got: %v", err)
	}
}

func TestRequireModelFields_CoreUseCases(t *testing.T) {
	valid := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","kind":"start","label":""},{"id":"a","kind":"action","label":"do"}],
                      "edges":[{"from":"s","to":"a","kind":"controlFlow","guard":""}]}},
         "rejectionReason":""}
      ]
    }`
	if err := RequireModelFields(KindCoreUseCases, []byte(valid)); err != nil {
		t.Fatalf("valid core use cases should pass, got: %v", err)
	}

	missingTrigger := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"classification":"core"},"rejectionReason":""}
      ]
    }`
	if err := RequireModelFields(KindCoreUseCases, []byte(missingTrigger)); err == nil || !strings.Contains(err.Error(), "trigger") {
		t.Fatalf("a use case missing its trigger must be rejected naming trigger, got: %v", err)
	}

	missingNodeKind := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","label":""}],"edges":[]}},"rejectionReason":""}
      ]
    }`
	if err := RequireModelFields(KindCoreUseCases, []byte(missingNodeKind)); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("an activity node missing its kind must be rejected, got: %v", err)
	}
}

// TestRequireModelFields_ReadBackParity confirms the check integrates into the codec:
// a System draft that omits every component's layer (the live F81 corruption) fails to
// re-decode through DecodeProjectJSON, exactly as the write path rejects it.
func TestRequireModelFields_ReadBackParity(t *testing.T) {
	// Build a project doc whose system slot carries a layer-less component. We hand-craft
	// the slot map shape decodeSlotsMap consumes (kind 5 = System).
	doc := `{
      "schemaVersion": 1,
      "slots": {
        "5": {"status": 4, "kind": 5, "model": {
          "components": [ {"id":"m","name":"OrderManager","kind":"manager","encapsulates":"x","atomicBusinessVerbs":[]} ],
          "relationships": [], "dynamicViews": []
        }}
      }
    }`
	_, _, err := DecodeProjectJSON([]byte(doc), ProjectID("p"))
	if err == nil {
		t.Fatal("read-back of a system slot with a layer-less component must fail")
	}
	if !strings.Contains(err.Error(), "layer") {
		t.Fatalf("read-back error should name the missing layer, got: %v", err)
	}
}
