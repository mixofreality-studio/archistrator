package projectstate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// This file pins the PUBLIC typed-wire contract the SPA consumes: camelCase field
// names on every Phase-1 model + nested type, STRING enum names (not integer
// ordinals) for the enums the SPA reads, and a STRING ArtifactKind discriminator.
// CODE is the source of truth; openapi.yaml follows these bytes.

// TestArtifactKind_JSONString proves an ArtifactKind marshals to its canonical
// camelCase wire name and round-trips, and that legacy integer ordinals still
// decode (backward compatibility for any pre-migration payload).
func TestArtifactKind_JSONString(t *testing.T) {
	for _, k := range AllArtifactKinds() {
		data, err := json.Marshal(k)
		if err != nil {
			t.Fatalf("marshal %v: %v", k, err)
		}
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatalf("kind %v did not marshal to a JSON string: %s", k, data)
		}
		if s != k.WireName() {
			t.Fatalf("kind %v marshalled as %q, want %q", k, s, k.WireName())
		}
		var back ArtifactKind
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %q: %v", data, err)
		}
		if back != k {
			t.Fatalf("round-trip kind: got %v, want %v", back, k)
		}
	}
	// Legacy integer ordinal still decodes.
	var legacy ArtifactKind
	if err := json.Unmarshal([]byte("4"), &legacy); err != nil {
		t.Fatalf("legacy ordinal: %v", err)
	}
	if legacy != KindCoreUseCases {
		t.Fatalf("legacy ordinal 4 = %v, want KindCoreUseCases", legacy)
	}
}

// TestMissionStatement_CamelCaseWire pins the literal camelCase field names of the
// MissionStatement model + its nested Objective.
func TestMissionStatement_CamelCaseWire(t *testing.T) {
	m := MissionStatement{
		Vision:     "ship value",
		Objectives: []Objective{{Number: 1, Statement: "be useful"}},
		Mission:    "components",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	for _, want := range []string{"vision", "objectives", "mission"} {
		if _, ok := generic[want]; !ok {
			t.Fatalf("missing camelCase field %q in %s", want, data)
		}
	}
	var obj []map[string]json.RawMessage
	if err := json.Unmarshal(generic["objectives"], &obj); err != nil {
		t.Fatalf("objectives: %v", err)
	}
	for _, want := range []string{"number", "statement"} {
		if _, ok := obj[0][want]; !ok {
			t.Fatalf("Objective missing camelCase field %q in %s", want, generic["objectives"])
		}
	}
	var back MissionStatement
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(m, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, m)
	}
}

// TestVolatilities_AxisStringEnum pins that the Axis enum serializes as a STRING
// name (not an integer ordinal) and round-trips, and that Glossary fields are camelCase.
func TestVolatilities_AxisStringEnum(t *testing.T) {
	v := Volatilities{Items: []Volatility{
		{Name: "tax rules", Rationale: "jurisdictions change", Axis: AxisAllCustomersAtOneTime},
	}}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic struct {
		Items []struct {
			Name      string          `json:"name"`
			Rationale string          `json:"rationale"`
			Axis      json.RawMessage `json:"axis"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	if len(generic.Items) != 1 {
		t.Fatalf("items: %s", data)
	}
	var axisStr string
	if err := json.Unmarshal(generic.Items[0].Axis, &axisStr); err != nil {
		t.Fatalf("axis must be a JSON string, got %s", generic.Items[0].Axis)
	}
	if axisStr != "allCustomersAtOneTime" {
		t.Fatalf("axis = %q, want %q", axisStr, "allCustomersAtOneTime")
	}
	var back Volatilities
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(v, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, v)
	}
}

// TestSystem_StringEnums_CamelCase pins string enum names (component kind, layer,
// call mode) and camelCase fields across the System model and its nested types,
// and a full round-trip.
func TestSystem_StringEnums_CamelCase(t *testing.T) {
	cid := Slug("ProjectStateAccess")
	ucid := Slug("Co-author")
	s := System{
		Components: []Component{{
			ID:                  cid,
			Name:                "ProjectStateAccess",
			Kind:                CompResourceAccess,
			Layer:               LayerResourceAccess,
			Encapsulates:        "project head-state",
			AtomicBusinessVerbs: []string{"createProject"},
		}},
		Relationships: []Relationship{{From: cid, To: cid, Mode: CallQueued, Label: "x"}},
		DynamicViews: []DynamicView{{
			UseCaseID:    ucid,
			Key:          "uc1",
			Title:        "Co-author",
			Participants: []ComponentID{cid},
			Edges:        []Relationship{{From: cid, To: cid, Mode: CallSync, Label: "y"}},
		}},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic struct {
		Components []struct {
			Kind                json.RawMessage `json:"kind"`
			Layer               json.RawMessage `json:"layer"`
			AtomicBusinessVerbs []string        `json:"atomicBusinessVerbs"`
		} `json:"components"`
		Relationships []struct {
			Mode json.RawMessage `json:"mode"`
		} `json:"relationships"`
		DynamicViews []map[string]json.RawMessage `json:"dynamicViews"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	assertStringEq(t, "component kind", generic.Components[0].Kind, "resourceAccess")
	assertStringEq(t, "component layer", generic.Components[0].Layer, "resourceAccess")
	assertStringEq(t, "relationship mode", generic.Relationships[0].Mode, "queued")
	if _, ok := generic.DynamicViews[0]["useCaseId"]; !ok {
		t.Fatalf("DynamicView missing camelCase useCaseId in %s", data)
	}
	var back System
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(s, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, s)
	}
}

// TestUseCase_StringEnums pins trigger / classification / node kind / edge kind
// string names and camelCase field names on the use-case grammar.
func TestUseCase_StringEnums(t *testing.T) {
	n1 := Slug("d")
	n2 := Slug("l")
	c := CoreUseCases{Decisions: []UseCaseDecision{{
		UseCase: UseCase{
			ID:             Slug("Co-author"),
			Name:           "Co-author",
			Actors:         []Actor{{ID: Slug("architect"), Role: "architect"}},
			Trigger:        TriggerBusMessage,
			Classification: ClassNonCore,
			Activity: &ActivityDiagram{
				Nodes: []ActivityNode{{ID: n1, Kind: NodeDecision, Label: "d"}, {ID: n2, Kind: NodeLoop, Label: "l"}},
				Edges: []ActivityEdge{{From: n1, To: n2, Kind: EdgeGuardedFlow, Guard: "g"}},
			},
		},
		RejectionReason: "permutation",
	}}}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic struct {
		Decisions []struct {
			UseCase struct {
				Trigger        json.RawMessage `json:"trigger"`
				Classification json.RawMessage `json:"classification"`
				Activity       struct {
					Nodes []struct {
						Kind json.RawMessage `json:"kind"`
					} `json:"nodes"`
					Edges []struct {
						Kind json.RawMessage `json:"kind"`
					} `json:"edges"`
				} `json:"activity"`
			} `json:"useCase"`
			RejectionReason string `json:"rejectionReason"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	uc := generic.Decisions[0].UseCase
	assertStringEq(t, "trigger", uc.Trigger, "busMessage")
	assertStringEq(t, "classification", uc.Classification, "nonCore")
	assertStringEq(t, "node kind", uc.Activity.Nodes[0].Kind, "decision")
	assertStringEq(t, "node kind", uc.Activity.Nodes[1].Kind, "loop")
	assertStringEq(t, "edge kind", uc.Activity.Edges[0].Kind, "guardedFlow")
	var back CoreUseCases
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(c, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, c)
	}
}

// TestStandardCheck_CheckStatusStringEnum pins the CheckStatus string name + camelCase.
func TestStandardCheck_CheckStatusStringEnum(t *testing.T) {
	sc := StandardCheck{Items: []CheckItem{{Section: "§3.4", Guideline: "g", Status: CheckWaived, Justification: "j"}}}
	data, err := json.Marshal(sc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic struct {
		Items []struct {
			Status json.RawMessage `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assertStringEq(t, "check status", generic.Items[0].Status, "waived")
	var back StandardCheck
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(sc, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, sc)
	}
}

// TestEnums_AcceptLegacyOrdinals proves every string enum still unmarshals a bare
// integer ordinal (backward-compat with the prompts that emit integers and any
// pre-migration JSONB payload).
func TestEnums_AcceptLegacyOrdinals(t *testing.T) {
	var a Axis
	mustUnmarshal(t, "1", &a)
	if a != AxisAllCustomersAtOneTime {
		t.Fatalf("axis legacy: %v", a)
	}
	var tr Trigger
	mustUnmarshal(t, "2", &tr)
	if tr != TriggerBusMessage {
		t.Fatalf("trigger legacy: %v", tr)
	}
	var cl Classification
	mustUnmarshal(t, "1", &cl)
	if cl != ClassNonCore {
		t.Fatalf("classification legacy: %v", cl)
	}
	var nk ActivityNodeKind
	mustUnmarshal(t, "2", &nk)
	if nk != NodeDecision {
		t.Fatalf("node kind legacy: %v", nk)
	}
	var ek EdgeKind
	mustUnmarshal(t, "1", &ek)
	if ek != EdgeGuardedFlow {
		t.Fatalf("edge kind legacy: %v", ek)
	}
	var ck ComponentKind
	mustUnmarshal(t, "3", &ck)
	if ck != CompResourceAccess {
		t.Fatalf("component kind legacy: %v", ck)
	}
	var ly Layer
	mustUnmarshal(t, "3", &ly)
	if ly != LayerResourceAccess {
		t.Fatalf("layer legacy: %v", ly)
	}
	var cm CallMode
	mustUnmarshal(t, "1", &cm)
	if cm != CallQueued {
		t.Fatalf("call mode legacy: %v", cm)
	}
	var cs CheckStatus
	mustUnmarshal(t, "1", &cs)
	if cs != CheckWaived {
		t.Fatalf("check status legacy: %v", cs)
	}
}

func assertStringEq(t *testing.T, what string, raw json.RawMessage, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%s must be a JSON string, got %s", what, raw)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", what, got, want)
	}
}

func mustUnmarshal(t *testing.T, data string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), v); err != nil {
		t.Fatalf("unmarshal %q into %T: %v", data, v, err)
	}
}

// TestCritiqueCarrier_RoundTrip_And_Isolation pins the D-MSD-Δ amendment: the
// first-class PM-critique read-back carrier (ArtifactSlot.CritiqueVerdict /
// CritiqueNotes) round-trips through the canonical .aiarch/state/project.json codec
// (the single shape aiarch-validate decodes), is OMITTED when empty (decode-compat),
// and is CLEARED by the status transitions while the architect's Notes ride
// separately on Reject — the collision the senior review identified cannot recur.
func TestCritiqueCarrier_RoundTrip_And_Isolation(t *testing.T) {
	mission, err := NewMissionStatement("ship value", []Objective{{Number: 1, Statement: "be useful"}}, "components")
	if err != nil {
		t.Fatalf("NewMissionStatement: %v", err)
	}

	// A staged slot carrying a critique-revise read-back carrier (what the Action committed).
	p := Project{ID: ProjectID("p1"), Version: 2, Phase: PhaseSystemDesign, Owner: "o"}
	p.Mission = ArtifactSlot{
		Status:          ReviewAwaitingReview,
		Model:           mission,
		CritiqueVerdict: CritiqueVerdictRevise,
		CritiqueNotes:   "tighten the vision sentence",
	}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	// The carrier keys are present on disk under their camelCase JSON names.
	if !strings.Contains(string(raw), "critiqueVerdict") || !strings.Contains(string(raw), "critiqueNotes") {
		t.Fatalf("expected critiqueVerdict/critiqueNotes keys in project.json, got:\n%s", raw)
	}
	back, ok, err := DecodeProjectJSON(raw, ProjectID("p1"))
	if err != nil || !ok {
		t.Fatalf("DecodeProjectJSON: ok=%v err=%v", ok, err)
	}
	if back.Mission.CritiqueVerdict != CritiqueVerdictRevise || back.Mission.CritiqueNotes != "tighten the vision sentence" {
		t.Fatalf("critique carrier did not round-trip: %+v", back.Mission)
	}
	if back.Mission.Notes != "" {
		t.Fatalf("the architect Notes field must stay empty (the critique rode its own carrier), got %q", back.Mission.Notes)
	}

	// DECODE-COMPAT: a slot with no critique carrier must NOT emit the keys (omitempty),
	// so legacy rows + the aiarch-validate decode are byte-identical.
	clean := Project{ID: ProjectID("p2"), Version: 1, Owner: "o"}
	clean.Glossary = ArtifactSlot{Status: ReviewCommitted, Model: mustGlossaryWC(t)}
	craw, err := EncodeProjectJSON(clean)
	if err != nil {
		t.Fatalf("EncodeProjectJSON(clean): %v", err)
	}
	if strings.Contains(string(craw), "critiqueVerdict") || strings.Contains(string(craw), "critiqueNotes") {
		t.Fatalf("a slot with no critique must omit the carrier keys, got:\n%s", craw)
	}

	// ISOLATION + CLEAR: a status transition (Reject) writes Notes and CLEARS the
	// critique carrier — the architect's reject rationale never collides with a stale
	// critique verdict.
	transition := statusTransition("RejectArtifact", KindMission, ReviewRejected, "REJECT: rework the vision")
	if terr := transition(&p); terr != nil {
		t.Fatalf("statusTransition: %v", terr)
	}
	if p.Mission.Notes != "REJECT: rework the vision" {
		t.Fatalf("Reject must write the architect rationale to Notes, got %q", p.Mission.Notes)
	}
	if p.Mission.CritiqueVerdict != "" || p.Mission.CritiqueNotes != "" {
		t.Fatalf("a status transition must CLEAR the critique carrier, got verdict=%q notes=%q", p.Mission.CritiqueVerdict, p.Mission.CritiqueNotes)
	}
}

func mustGlossaryWC(t *testing.T) *Glossary {
	t.Helper()
	g, err := NewGlossary([]GlossaryItem{{Term: "Aggregate", Definition: "a consistency boundary"}})
	if err != nil {
		t.Fatalf("NewGlossary: %v", err)
	}
	return g
}
