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
        {"id":"a","name":"A","kind":"manager","layer":"manager","encapsulates":"x"},
        {"id":"b","name":"B","kind":"engine","layer":"engine","encapsulates":"y"}
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

// ---- SYS-ENCAPSULATES (raw twin): M/E/RA must name a non-empty volatility; a client may be empty ----

func TestRequireModelFields_Encapsulates_ManagerMustBeNonEmpty(t *testing.T) {
	j := `{
      "components": [
        {"id":"m","name":"OrderManager","kind":"manager","layer":"manager","encapsulates":""}
      ],
      "relationships": [], "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "encapsulates") {
		t.Fatalf("a manager with empty encapsulates must be rejected naming encapsulates, got: %v", err)
	}
}

func TestRequireModelFields_Encapsulates_MissingKeyRejected(t *testing.T) {
	j := `{
      "components": [
        {"id":"c","name":"WebClient","kind":"client","layer":"client"}
      ],
      "relationships": [], "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "encapsulates") {
		t.Fatalf("a component omitting the encapsulates key must be rejected, got: %v", err)
	}
}

func TestRequireModelFields_Encapsulates_EmptyClientAllowed(t *testing.T) {
	// A CLIENT may carry empty encapsulates (transport owns no volatility); the non-empty
	// expectation for a client is a read-back finding, not a hard codec failure — this is
	// exactly what keeps committed state (empty-encapsulates clients) readable.
	j := `{
      "components": [
        {"id":"c","name":"WebClient","kind":"client","layer":"client","encapsulates":"","atomicBusinessVerbs":[]}
      ],
      "relationships": [], "dynamicViews": []
    }`
	if err := RequireModelFields(KindSystem, []byte(j)); err != nil {
		t.Fatalf("an empty-encapsulates client must be allowed on the write path, got: %v", err)
	}
}

// ---- UC-ACT-PRESENT: every use case needs a non-null activity with start + action ----

func TestRequireModelFields_ActivityPresent_NullRejected(t *testing.T) {
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core","activity":null},"rejectionReason":""}
      ]
    }`
	err := RequireModelFields(KindCoreUseCases, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "activity") {
		t.Fatalf("a use case with a null activity must now be rejected, got: %v", err)
	}
}

func TestRequireModelFields_ActivityPresent_NoActionRejected(t *testing.T) {
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","kind":"start","label":""}],"edges":[]}},"rejectionReason":""}
      ]
    }`
	err := RequireModelFields(KindCoreUseCases, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "structurally empty") {
		t.Fatalf("a start-only activity must be rejected as structurally empty, got: %v", err)
	}
}

// ---- UC-GUARD-LABEL: a guardedFlow edge must carry non-empty guard text ----

func TestRequireModelFields_GuardLabel_EmptyGuardRejected(t *testing.T) {
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","kind":"start"},{"id":"a","kind":"action","label":"do"}],
                      "edges":[{"from":"s","to":"a","kind":"guardedFlow","guard":""}]}},"rejectionReason":""}
      ]
    }`
	err := RequireModelFields(KindCoreUseCases, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "guard") {
		t.Fatalf("a guardedFlow edge with empty guard must be rejected, got: %v", err)
	}
}

// ---- STD-STATUS-EXPLICIT: every standard-check item must emit status ----

func TestRequireModelFields_StandardCheck(t *testing.T) {
	valid := `{"items":[{"section":"S","guideline":"G","status":"pass","justification":""}]}`
	if err := RequireModelFields(KindStandardCheck, []byte(valid)); err != nil {
		t.Fatalf("valid standard check should pass, got: %v", err)
	}
	missing := `{"items":[{"section":"S","guideline":"G","justification":""}]}`
	err := RequireModelFields(KindStandardCheck, []byte(missing))
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("a standard-check item omitting status must be rejected naming status, got: %v", err)
	}
}

// ---- VOL-AXIS-EXPLICIT: every volatility must emit axis ----

func TestRequireModelFields_Volatilities(t *testing.T) {
	valid := `{"items":[{"name":"V","rationale":"r","axis":"sameCustomerOverTime"}]}`
	if err := RequireModelFields(KindVolatilities, []byte(valid)); err != nil {
		t.Fatalf("valid volatilities should pass, got: %v", err)
	}
	missing := `{"items":[{"name":"V","rationale":"r"}]}`
	err := RequireModelFields(KindVolatilities, []byte(missing))
	if err == nil || !strings.Contains(err.Error(), "axis") {
		t.Fatalf("a volatility omitting axis must be rejected naming axis, got: %v", err)
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
