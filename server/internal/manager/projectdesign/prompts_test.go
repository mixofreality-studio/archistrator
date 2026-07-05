package projectdesign

// prompts_test.go — unit coverage for the Manager-owned Phase-2 draft-prompt composition,
// focused on the TYPED-SHAPE discipline (QA F36 Phase-2 sibling). The live incident: a drafted
// PlanningAssumptions committed "resources" as an array of OBJECTS where the typed codec expects
// []string — a terminal read-back decode failure the CI validate check did NOT catch. The
// draft prompt for each agent-drafted Phase-2 kind must carry the schema-conformance preamble
// (pointing at the typed schema the agent can read in its checkout) plus the per-kind shape
// hotspots. The expected hotspots are DERIVED FROM the projectstate Go types so this stays in
// lockstep with them rather than a hand-copy that could drift.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// draftedPhase2Kinds are the Phase-2 artifact kinds the architect role actually DRAFTS through
// architectDraftPrompt (SdpReview is excluded — it is assembled deterministically by the
// workflow, see prompts.go package doc).
var draftedPhase2Kinds = []projectstate.ArtifactKind{
	projectstate.KindPlanningAssumptions,
	projectstate.KindActivityList,
	projectstate.KindNetwork,
	projectstate.KindNormalSolution,
	projectstate.KindSubcriticalSolution,
	projectstate.KindCompressedSolution,
	projectstate.KindDecompressedSolution,
	projectstate.KindRiskModel,
}

// draftFor composes the first-draft prompt for a kind (no feedback, no ledger, no amendment).
func draftFor(kind projectstate.ArtifactKind) string {
	return architectDraftPrompt(kind, projectstate.Project{}, "", nil, 0)
}

// F68: the slot number rendered into the design prompt MUST equal the session's wire kind for
// EVERY drafted Phase-2 kind — the canonical mapping that flows identically through the prompt,
// the branch (int(kind)), and the server read-back. The live incident: a decompressed (14) draft
// whose prompt let the agent infer slot 12 (subcritical) — the four Solution siblings share one
// type and the DRAFT ORDER is non-monotonic in the slot values, so an implicit slot invites a
// positional mis-write. The directive names the authoritative number; this asserts it, and — the
// crux of the bug — that NO OTHER solution sibling's number leaks in for a solution kind.
func Test_SlotPlacement_PromptSlotNumberEqualsWireKind(t *testing.T) {
	solutionKinds := map[projectstate.ArtifactKind]bool{
		projectstate.KindNormalSolution:       true,
		projectstate.KindSubcriticalSolution:  true,
		projectstate.KindCompressedSolution:   true,
		projectstate.KindDecompressedSolution: true,
	}
	for _, kind := range draftedPhase2Kinds {
		prompt := draftFor(kind)
		want := fmt.Sprintf("slot keyed exactly %q", fmt.Sprintf("%d", int(kind)))
		if !strings.Contains(prompt, want) {
			t.Fatalf("kind %s (wire %d): prompt must name the authoritative slot key %q; got:\n%s",
				kind, int(kind), want, prompt)
		}
		// A solution kind must NOT mention any OTHER solution sibling's slot key (the exact
		// cross-write the incident produced: decompressed=14 drafted into slot 12).
		if solutionKinds[kind] {
			for sib := range solutionKinds {
				if sib == kind {
					continue
				}
				stray := fmt.Sprintf("slot keyed exactly %q", fmt.Sprintf("%d", int(sib)))
				if strings.Contains(prompt, stray) {
					t.Fatalf("kind %s (wire %d): prompt leaks sibling solution slot %d — the F68 cross-write",
						kind, int(kind), int(sib))
				}
			}
		}
	}
}

// jsonFieldName returns the JSON wire name of a struct field (the part before any comma in its
// json tag), so the shape assertions are DERIVED from the type definition, not hand-copied.
func jsonFieldName(t *testing.T, typ reflect.Type, goField string) string {
	t.Helper()
	f, ok := typ.FieldByName(goField)
	if !ok {
		t.Fatalf("%s has no field %s", typ.Name(), goField)
	}
	tag := f.Tag.Get("json")
	if tag == "" {
		t.Fatalf("%s.%s has no json tag", typ.Name(), goField)
	}
	return strings.Split(tag, ",")[0]
}

// Every agent-drafted Phase-2 kind must carry the SCHEMA CONFORMANCE preamble AND point at the
// typed schema location the drafting agent can actually read in its checkout (the committed
// .serviceContracts $defs in .aiarch/state/project.json). This is the F36-sibling counterpart
// to systemdesign's enum-conformance preamble.
func Test_ShapeGuide_PreambleOnEveryDraftedPhase2Kind(t *testing.T) {
	for _, kind := range draftedPhase2Kinds {
		prompt := draftFor(kind)
		if !strings.Contains(prompt, "SCHEMA CONFORMANCE") {
			t.Errorf("%s draft prompt must carry the SCHEMA CONFORMANCE preamble; got:\n%s", kind, prompt)
		}
		// Points at the checked-out state file...
		if !strings.Contains(prompt, ".aiarch/state/project.json") {
			t.Errorf("%s draft prompt must point at the checked-out state file; got:\n%s", kind, prompt)
		}
		// ...and specifically at the embedded typed schema ($defs under .serviceContracts).
		if !strings.Contains(prompt, ".serviceContracts") || !strings.Contains(prompt, "$defs") {
			t.Errorf("%s draft prompt must point at the .serviceContracts $defs typed schema; got:\n%s", kind, prompt)
		}
		// The exact-shape mandate (the core instruction).
		if !strings.Contains(prompt, "Conform EXACTLY") {
			t.Errorf("%s draft prompt must mandate exact shape conformance; got:\n%s", kind, prompt)
		}
	}
}

// PlanningAssumptions — the live incident. The prompt must name "resources" (derived: the field
// IS a []string in the Go type) as an array of STRINGS, not objects, and frame the objects shape
// as the failure. This is the representative hotspot for this kind, derived from the type.
func Test_PlanningAssumptionsPrompt_ResourcesArrayOfStrings(t *testing.T) {
	// Anti-drift: assert the Go type still declares Resources as []string, so this test fails
	// loudly if the type changes shape (and the prompt guidance would need to change with it).
	pt := reflect.TypeOf(projectstate.PlanningAssumptions{})
	rf, ok := pt.FieldByName("Resources")
	if !ok || rf.Type.Kind() != reflect.Slice || rf.Type.Elem().Kind() != reflect.String {
		t.Fatalf("PlanningAssumptions.Resources is no longer []string (got %s) — update the shape guidance", rf.Type)
	}
	resources := jsonFieldName(t, pt, "Resources") // "resources"

	prompt := draftFor(projectstate.KindPlanningAssumptions)
	if !strings.Contains(prompt, "\""+resources+"\"") {
		t.Errorf("prompt must name the %q field; got:\n%s", resources, prompt)
	}
	if !strings.Contains(prompt, "array of STRINGS") {
		t.Errorf("prompt must state resources is an array of strings; got:\n%s", prompt)
	}
	// The exact failure mode must be framed as invalid.
	if !strings.Contains(prompt, "NOT an array of objects") {
		t.Errorf("prompt must forbid the array-of-objects shape that failed live; got:\n%s", prompt)
	}
}

// Money is a recurring shape trap: {minorUnits, currency} — a bare number is invalid. Derive the
// two Money wire field names from the type and assert every prompt that carries a Money-typed
// field (PlanningAssumptions.indirectDailyRate, Solution.classRates values, RiskRow.totalCost)
// names both — so the agent never emits a bare number where Money is expected.
func Test_ShapeGuide_MoneyFieldsNamedWhereverMoneyAppears(t *testing.T) {
	b, err := json.Marshal(projectstate.Money{MinorUnits: 500, Currency: "USD"})
	if err != nil {
		t.Fatalf("marshal Money: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal Money keys: %v", err)
	}
	if len(m) == 0 {
		t.Fatal("Money marshalled to no keys")
	}

	moneyKinds := []projectstate.ArtifactKind{
		projectstate.KindPlanningAssumptions, // indirectDailyRate
		projectstate.KindNormalSolution,      // classRates values
		projectstate.KindRiskModel,           // totalCost
	}
	for _, kind := range moneyKinds {
		prompt := draftFor(kind)
		for key := range m {
			if !strings.Contains(prompt, key) {
				t.Errorf("%s prompt must name Money wire field %q (a Money value is not a bare number); got:\n%s", kind, key, prompt)
			}
		}
		if !strings.Contains(prompt, "bare number") {
			t.Errorf("%s prompt must forbid a bare number where Money is expected; got:\n%s", kind, prompt)
		}
	}
}

// String-keyed maps are a shape trap: an LLM would guess an array of objects. Derive that
// PlanningAssumptions.RateCard and Solution.ClassRates ARE maps and assert their prompts forbid
// the array shape and name the field.
func Test_ShapeGuide_StringKeyedMapsNotArrays(t *testing.T) {
	// PlanningAssumptions.rateCard
	pt := reflect.TypeOf(projectstate.PlanningAssumptions{})
	if f, _ := pt.FieldByName("RateCard"); f.Type.Kind() != reflect.Map {
		t.Fatalf("PlanningAssumptions.RateCard is no longer a map (got %s) — update the guidance", f.Type)
	}
	rateCard := jsonFieldName(t, pt, "RateCard")
	pa := draftFor(projectstate.KindPlanningAssumptions)
	if !strings.Contains(pa, "\""+rateCard+"\"") || !strings.Contains(pa, "STRING-KEYED MAP") {
		t.Errorf("planning-assumptions prompt must describe %q as a string-keyed map; got:\n%s", rateCard, pa)
	}

	// Solution.classRates
	st := reflect.TypeOf(projectstate.Solution{})
	if f, _ := st.FieldByName("ClassRates"); f.Type.Kind() != reflect.Map {
		t.Fatalf("Solution.ClassRates is no longer a map (got %s) — update the guidance", f.Type)
	}
	classRates := jsonFieldName(t, st, "ClassRates")
	for _, kind := range []projectstate.ArtifactKind{
		projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution,
		projectstate.KindCompressedSolution, projectstate.KindDecompressedSolution,
	} {
		prompt := draftFor(kind)
		if !strings.Contains(prompt, "\""+classRates+"\"") || !strings.Contains(prompt, "STRING-KEYED MAP") {
			t.Errorf("%s prompt must describe %q as a string-keyed map; got:\n%s", kind, classRates, prompt)
		}
	}
}

// ActivityList — riskBucket is a single integer (Fibonacci), effortDays a number; neither is an
// object. Derive the field names + the Go kinds and assert the representative hotspots.
func Test_ActivityListPrompt_ScalarActivityFields(t *testing.T) {
	at := reflect.TypeOf(projectstate.ActivityItem{})
	if f, _ := at.FieldByName("RiskBucket"); f.Type.Kind() != reflect.Int {
		t.Fatalf("ActivityItem.RiskBucket is no longer an int (got %s) — update the guidance", f.Type)
	}
	riskBucket := jsonFieldName(t, at, "RiskBucket")
	effortDays := jsonFieldName(t, at, "EffortDays")

	prompt := draftFor(projectstate.KindActivityList)
	if !strings.Contains(prompt, "\""+riskBucket+"\"") || !strings.Contains(prompt, "Fibonacci") {
		t.Errorf("activity-list prompt must describe %q as a Fibonacci integer; got:\n%s", riskBucket, prompt)
	}
	if !strings.Contains(prompt, "\""+effortDays+"\"") {
		t.Errorf("activity-list prompt must name %q; got:\n%s", effortDays, prompt)
	}
	// The activities-array-under-a-key shape (not a bare top-level array).
	activities := jsonFieldName(t, reflect.TypeOf(projectstate.ActivityList{}), "Activities")
	if !strings.Contains(prompt, "\""+activities+"\"") {
		t.Errorf("activity-list prompt must name the %q wrapper key; got:\n%s", activities, prompt)
	}
}

// ActivityList doctrine — the base list is ONE coding activity per component (detailed design
// and construction are internal lifecycle phases of that single activity, NOT separate network
// nodes); integration and noncoding activities remain separate. The live finding: a draft split
// detailed-design and construction into separate activities (40 activities for an 18-component
// system). The draft-task text must carry the one-activity-per-component rule so the drafting
// agent does not re-emit the split.
func Test_ActivityListPrompt_OneCodingActivityPerComponent(t *testing.T) {
	prompt := draftFor(projectstate.KindActivityList)
	// The core rule: exactly one coding activity per component.
	if !strings.Contains(prompt, "ONE coding activity per component") {
		t.Errorf("activity-list prompt must state one coding activity per component; got:\n%s", prompt)
	}
	// Detailed design + construction are internal phases, not separate nodes.
	if !strings.Contains(prompt, "internal lifecycle phases") {
		t.Errorf("activity-list prompt must frame detailed-design/construction as internal lifecycle phases; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "NOT separate network nodes") {
		t.Errorf("activity-list prompt must forbid splitting a component into separate network nodes; got:\n%s", prompt)
	}
	// Integration and noncoding activities stay separate.
	if !strings.Contains(prompt, "Integration") || !strings.Contains(prompt, "noncoding") {
		t.Errorf("activity-list prompt must keep integration and noncoding activities separate; got:\n%s", prompt)
	}
	// Effort in 5-day quanta.
	if !strings.Contains(prompt, "5-day quanta") {
		t.Errorf("activity-list prompt must state effort in 5-day quanta; got:\n%s", prompt)
	}
}

// Network — dependsOn/criticalPath are arrays of name STRINGS, and the compute-at-read block
// (computed/summary) must NOT be authored. These are the representative hotspots for this kind.
func Test_NetworkPrompt_NameStringArrays_And_NoComputedBlock(t *testing.T) {
	prompt := draftFor(projectstate.KindNetwork)

	depType := reflect.TypeOf(projectstate.NetworkDependency{})
	dependsOn := jsonFieldName(t, depType, "DependsOn")
	if f, _ := depType.FieldByName("DependsOn"); f.Type.Kind() != reflect.Slice || f.Type.Elem().Kind() != reflect.String {
		t.Fatalf("NetworkDependency.DependsOn is no longer []string (got %s) — update the guidance", f.Type)
	}
	criticalPath := jsonFieldName(t, reflect.TypeOf(projectstate.Network{}), "CriticalPath")

	for _, want := range []string{"\"" + dependsOn + "\"", "\"" + criticalPath + "\"", "activity-NAME STRINGS"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("network prompt missing %q; got:\n%s", want, prompt)
		}
	}
	// Compute-at-read fields must be called out as server-populated (do not author).
	if !strings.Contains(prompt, "computed") || !strings.Contains(prompt, "READ time") {
		t.Errorf("network prompt must warn not to author the compute-at-read block; got:\n%s", prompt)
	}
}

// The assembled-not-drafted SdpReview must NOT get a shape block, and a Phase-1 kind (Mission)
// must not either — the guidance is scoped to the agent-drafted Phase-2 kinds so unrelated
// prompts stay lean (mirrors the systemdesign Test_MissionPrompt_HasNoClosedEnumBlock guard).
func Test_ShapeGuide_ScopedToDraftedPhase2Kinds(t *testing.T) {
	for _, kind := range []projectstate.ArtifactKind{projectstate.KindSdpReview, projectstate.KindMission} {
		prompt := draftFor(kind)
		if strings.Contains(prompt, "SCHEMA CONFORMANCE") {
			t.Errorf("%s prompt must NOT carry the typed-shape block (not an agent-drafted Phase-2 kind); got:\n%s", kind, prompt)
		}
	}
	// Direct unit assertion on the guide function too.
	if shapeGuide(projectstate.KindSdpReview) != "" {
		t.Errorf("shapeGuide(SdpReview) must be empty (assembled deterministically, not drafted)")
	}
}
