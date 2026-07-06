package projectstate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// modelfields.go closes the ZERO-VALUE HOLE in the strict slot codec (F81).
//
// The closed ordinal enums (Layer, ComponentKind, CallMode, Trigger, Classification,
// ActivityNodeKind, …) carry a custom UnmarshalJSON that rejects an unrecognized wire
// name — but encoding/json NEVER invokes UnmarshalJSON for a field that is ABSENT from
// the JSON. An omitted "layer" therefore decodes to Layer(0)==LayerClient with no error,
// and an omitted "kind" to ComponentKind(0)==CompClient. The live F81 failure was a
// System draft that omitted every component's layer: the strict codec silently defaulted
// all 17 components to layer=client, methodcheck passed VACUOUSLY (an all-client system
// violates no layer-interaction rule), machine validation reported 0 ERR, and only an
// unrelated merge conflict prevented a corrupted architecture from committing.
//
// RequireModelFields walks the RAW slot-model JSON and demands that every REQUIRED
// closed-enum / identity field be PRESENT (and, for enum fields, a recognized wire
// value AND — for a component — consistent with its kind). It returns a typed,
// human-actionable error the drafting agent reads and corrects. It is deliberately a
// RAW-JSON pass (not a post-decode struct check) because presence is only observable
// before the struct decode collapses "absent" and "zero" into the same value.
//
// It is wired into BOTH gates that must agree byte-for-byte:
//   - putDraftModel (the MCP write path, agent-facing) — a bad draft is rejected before
//     it can commit, so the agent self-corrects.
//   - decodeSlotsMap (the server read-back codec) — a committed model that would carry a
//     defaulted enum is rejected on read-back with the same strictness, so the write and
//     read paths never disagree (the F36/F66 read-back-parity invariant).
func RequireModelFields(kind ArtifactKind, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	switch kind {
	case KindSystem:
		return requireSystemFields(raw)
	case KindCoreUseCases:
		return requireCoreUseCasesFields(raw)
	case KindStandardCheck:
		return requireStandardCheckFields(raw)
	case KindVolatilities:
		return requireVolatilitiesFields(raw)
	case KindMission, KindGlossary, KindScrubbedRequirements, KindOperationalConcepts,
		KindPlanningAssumptions, KindActivityList, KindNetwork, KindNormalSolution,
		KindSubcriticalSolution, KindCompressedSolution, KindDecompressedSolution,
		KindRiskModel, KindSdpReview:
		// Every other artifact's required-enum surface either has no zero-value hole that
		// silently corrupts a Method rule, or is fully guarded by methodcheck; add a case
		// above as new enum-bearing models acquire a hole.
		return nil
	}
	return nil
}

// requireSystemFields enforces the presence + consistency of the System model's
// closed-enum / identity fields: every component's id/name/kind/layer, every
// relationship's from/to/mode, and every dynamic view's useCaseId (and its edges'
// from/to/mode). The load-bearing check is layer==canonicalLayer(kind): it catches the
// live F81 case (kind present, layer omitted→client) as a mismatch. The both-omitted
// case (kind AND layer absent → both client → self-consistent) is caught by the presence
// checks below and, at the whole-system level, by the SYSTEM-LAYER-DEGENERATE rule.
// requireComponentFields enforces one component's identity + closed-enum + encapsulates
// surface (extracted from requireSystemFields to keep each function's cognitive
// complexity within the linter's floor).
func requireComponentFields(cRaw json.RawMessage, i int) error {
	obj, err := rawObject(cRaw)
	if err != nil {
		return fmt.Errorf("component %d is not a JSON object: %w", i+1, err)
	}
	label := componentLabel(obj, i)
	if err := requireNonEmptyString(obj, "id", label); err != nil {
		return err
	}
	if err := requireNonEmptyString(obj, "name", label); err != nil {
		return err
	}
	if err := requirePresent(obj, "kind", label); err != nil {
		return err
	}
	if err := requirePresent(obj, "layer", label); err != nil {
		return err
	}
	var kind ComponentKind
	if err := json.Unmarshal(obj["kind"], &kind); err != nil {
		return fmt.Errorf("%s has an unrecognized kind: %w — use one of client|manager|engine|resourceAccess|resource|utility", label, err)
	}
	var layer Layer
	if err := json.Unmarshal(obj["layer"], &layer); err != nil {
		return fmt.Errorf("%s has an unrecognized layer: %w — use one of client|manager|engine|resourceAccess|resource|utility", label, err)
	}
	if want := canonicalLayer(kind); layer != want {
		return fmt.Errorf("%s declares layer %q but its kind %q requires layer %q — the layer is 100%% derivable from the kind; set them to match (a missing layer field silently defaults to \"client\", which is the F81 corruption this rejects)",
			label, enumName(layerNames, layer), enumName(componentKindNames, kind), enumName(layerNames, want))
	}
	// SYS-ENCAPSULATES (raw twin). encapsulates is a plain string whose zero value is "" —
	// an omitted field is a silent hole (the component claims to encapsulate nothing).
	// Require the field PRESENT on every component, and NON-EMPTY on the three
	// volatility-owning kinds (Manager/Engine/ResourceAccess), which by definition each name
	// the volatility they own. Client/Resource/Utility legitimately carry "" (a transport
	// entry point, a physical store, a cappuccino-machine utility own no volatility), so
	// their emptiness is surfaced as a read-back FINDING (SYS-ENCAPSULATES) rather than
	// hard-failed here: a committed system may carry empty-encapsulates clients and its
	// reads must never break.
	if err := requirePresent(obj, "encapsulates", label); err != nil {
		return err
	}
	if kind == CompManager || kind == CompEngine || kind == CompResourceAccess {
		if err := requireNonEmptyString(obj, "encapsulates", label); err != nil {
			return fmt.Errorf("%s is a %s and must name the volatility it encapsulates: %w",
				label, enumName(componentKindNames, kind), err)
		}
	}
	return nil
}

func requireSystemFields(raw []byte) error {
	var top struct {
		Components    []json.RawMessage `json:"components"`
		Relationships []json.RawMessage `json:"relationships"`
		DynamicViews  []json.RawMessage `json:"dynamicViews"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("the system model is not a JSON object: %w", err)
	}
	if len(top.Components) == 0 {
		return fmt.Errorf("the system model declares no components; a System must decompose into at least one component")
	}
	for i, cRaw := range top.Components {
		if err := requireComponentFields(cRaw, i); err != nil {
			return err
		}
	}
	for i, rRaw := range top.Relationships {
		if err := requireRelationshipFields(rRaw, fmt.Sprintf("relationship %d", i+1)); err != nil {
			return err
		}
	}
	for i, dvRaw := range top.DynamicViews {
		obj, err := rawObject(dvRaw)
		if err != nil {
			return fmt.Errorf("dynamic view %d is not a JSON object: %w", i+1, err)
		}
		label := fmt.Sprintf("dynamic view %d", i+1)
		if err := requireNonEmptyString(obj, "useCaseId", label); err != nil {
			return err
		}
		if edges, ok := obj["edges"]; ok && !isJSONNull(edges) {
			var edgeRaws []json.RawMessage
			if err := json.Unmarshal(edges, &edgeRaws); err != nil {
				return fmt.Errorf("%s edges is not a JSON array: %w", label, err)
			}
			for j, eRaw := range edgeRaws {
				if err := requireRelationshipFields(eRaw, fmt.Sprintf("%s edge %d", label, j+1)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// requireRelationshipFields enforces from/to/mode on one Relationship (a top-level edge
// or a dynamic-view edge). mode is the CallMode closed enum whose zero value (CallSync)
// would silently absorb an omitted field.
func requireRelationshipFields(raw json.RawMessage, label string) error {
	obj, err := rawObject(raw)
	if err != nil {
		return fmt.Errorf("%s is not a JSON object: %w", label, err)
	}
	if err := requireNonEmptyString(obj, "from", label); err != nil {
		return err
	}
	if err := requireNonEmptyString(obj, "to", label); err != nil {
		return err
	}
	if err := requirePresent(obj, "mode", label); err != nil {
		return err
	}
	var mode CallMode
	if err := json.Unmarshal(obj["mode"], &mode); err != nil {
		return fmt.Errorf("%s has an unrecognized mode: %w — use one of sync|queued|eventPubSub", label, err)
	}
	return nil
}

// requireCoreUseCasesFields enforces the CoreUseCases model's closed-enum surface: every
// use case's trigger + classification (both have real zero values — clientAction / core —
// that would silently absorb an omitted field) and every activity node's / edge's kind.
func requireCoreUseCasesFields(raw []byte) error {
	var top struct {
		Decisions []json.RawMessage `json:"decisions"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("the core use cases model is not a JSON object: %w", err)
	}
	for i, dRaw := range top.Decisions {
		dObj, err := rawObject(dRaw)
		if err != nil {
			return fmt.Errorf("use-case decision %d is not a JSON object: %w", i+1, err)
		}
		ucRaw, ok := dObj["useCase"]
		if !ok || isJSONNull(ucRaw) {
			return fmt.Errorf("use-case decision %d is missing its required \"useCase\" object", i+1)
		}
		uc, err := rawObject(ucRaw)
		if err != nil {
			return fmt.Errorf("use-case decision %d useCase is not a JSON object: %w", i+1, err)
		}
		label := useCaseLabel(uc, i)
		if err := requirePresent(uc, "trigger", label); err != nil {
			return err
		}
		var trig Trigger
		if err := json.Unmarshal(uc["trigger"], &trig); err != nil {
			return fmt.Errorf("%s has an unrecognized trigger: %w — use one of clientAction|timer|busMessage", label, err)
		}
		if err := requirePresent(uc, "classification", label); err != nil {
			return err
		}
		var class Classification
		if err := json.Unmarshal(uc["classification"], &class); err != nil {
			return fmt.Errorf("%s has an unrecognized classification: %w — use one of core|nonCore", label, err)
		}
		// UC-ACT-PRESENT (promoted 2026-07-05 from the advisory USECASE-ACTIVITY-MISSING
		// read-back finding to a WRITE-PATH block). The strict codec previously SKIPPED a
		// null activity here, letting a diagram-less use case commit. Every use case — core
		// AND nonCore variation — must now carry a non-null activity diagram with at least
		// one start node and one action step; requireActivityFields enforces the floor.
		act, ok := uc["activity"]
		if !ok || isJSONNull(act) {
			return fmt.Errorf("%s is missing its required activity diagram (activity is null); every use case must carry a non-empty activity diagram with a start node and at least one action step", label)
		}
		if err := requireActivityFields(act, label); err != nil {
			return err
		}
	}
	return nil
}

// requireActivityFields enforces the kind enum on every node and edge of a use case's
// activity diagram (ActivityNodeKind / EdgeKind both have real zero values — start /
// controlFlow — that would silently absorb an omitted field).
func requireActivityFields(raw json.RawMessage, ucLabel string) error {
	act, err := rawObject(raw)
	if err != nil {
		return fmt.Errorf("%s activity is not a JSON object: %w", ucLabel, err)
	}
	hasStart, hasAction, err := requireActivityNodes(act, ucLabel)
	if err != nil {
		return err
	}
	// UC-ACT-PRESENT floor: a non-empty activity diagram carries at least a start node
	// and one action step (App C 1c). The write-path twin of the read-back activityDefect
	// classifier in the systemdesign Manager.
	if !hasStart || !hasAction {
		return fmt.Errorf("%s activity diagram is structurally empty: it must contain at least one start node and at least one action step", ucLabel)
	}
	return requireActivityEdges(act, ucLabel)
}

// requireActivityNodes validates every node's kind enum and reports whether the diagram
// carries a start node and an action node (the UC-ACT-PRESENT floor inputs).
func requireActivityNodes(act map[string]json.RawMessage, ucLabel string) (hasStart, hasAction bool, err error) {
	var nodeRaws []json.RawMessage
	if nodes, ok := act["nodes"]; ok && !isJSONNull(nodes) {
		if e := json.Unmarshal(nodes, &nodeRaws); e != nil {
			return false, false, fmt.Errorf("%s activity nodes is not a JSON array: %w", ucLabel, e)
		}
	}
	for i, nRaw := range nodeRaws {
		obj, e := rawObject(nRaw)
		if e != nil {
			return false, false, fmt.Errorf("%s activity node %d is not a JSON object: %w", ucLabel, i+1, e)
		}
		label := fmt.Sprintf("%s activity node %d", ucLabel, i+1)
		if e := requirePresent(obj, "kind", label); e != nil {
			return false, false, e
		}
		var nk ActivityNodeKind
		if e := json.Unmarshal(obj["kind"], &nk); e != nil {
			return false, false, fmt.Errorf("%s has an unrecognized kind: %w", label, e)
		}
		if nk == NodeStart {
			hasStart = true
		}
		if nk == NodeAction {
			hasAction = true
		}
	}
	return hasStart, hasAction, nil
}

// requireActivityEdges validates every edge's kind enum and enforces UC-GUARD-LABEL (a
// guardedFlow edge must carry non-empty guard text).
func requireActivityEdges(act map[string]json.RawMessage, ucLabel string) error {
	edges, ok := act["edges"]
	if !ok || isJSONNull(edges) {
		return nil
	}
	var edgeRaws []json.RawMessage
	if err := json.Unmarshal(edges, &edgeRaws); err != nil {
		return fmt.Errorf("%s activity edges is not a JSON array: %w", ucLabel, err)
	}
	for i, eRaw := range edgeRaws {
		obj, err := rawObject(eRaw)
		if err != nil {
			return fmt.Errorf("%s activity edge %d is not a JSON object: %w", ucLabel, i+1, err)
		}
		label := fmt.Sprintf("%s activity edge %d", ucLabel, i+1)
		if err := requirePresent(obj, "kind", label); err != nil {
			return err
		}
		var ek EdgeKind
		if err := json.Unmarshal(obj["kind"], &ek); err != nil {
			return fmt.Errorf("%s has an unrecognized kind: %w", label, err)
		}
		// UC-GUARD-LABEL: a guardedFlow edge (the outgoing edge of a decision) must carry
		// non-empty guard text — an unlabeled guard makes the branch condition unreadable.
		// Plain controlFlow edges carry no guard.
		if ek == EdgeGuardedFlow {
			if err := requireNonEmptyString(obj, "guard", label); err != nil {
				return fmt.Errorf("%s is a guardedFlow edge and must carry non-empty guard text: %w", label, err)
			}
		}
	}
	return nil
}

// requireStandardCheckFields enforces STD-STATUS-EXPLICIT: every standard-check item
// must emit its status EXPLICITLY. CheckStatus's zero value is CheckPass, so an omitted
// "status" silently reads as PASS (the F81 class) — a failing or waived guideline would
// masquerade as satisfied. Demand the field present and a recognized enum on every item.
func requireStandardCheckFields(raw []byte) error {
	var top struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("the standard-check model is not a JSON object: %w", err)
	}
	for i, iRaw := range top.Items {
		obj, err := rawObject(iRaw)
		if err != nil {
			return fmt.Errorf("standard-check item %d is not a JSON object: %w", i+1, err)
		}
		label := checkItemLabel(obj, i)
		if err := requirePresent(obj, "status", label); err != nil {
			return err
		}
		var st CheckStatus
		if err := json.Unmarshal(obj["status"], &st); err != nil {
			return fmt.Errorf("%s has an unrecognized status: %w — use one of pass|waived|fail", label, err)
		}
	}
	return nil
}

// requireVolatilitiesFields enforces VOL-AXIS-EXPLICIT: every volatility must emit its
// axis EXPLICITLY. Axis's zero value is AxisSameCustomerOverTime, so an omitted "axis"
// silently reads as that axis (the F81 class) — a volatility placed on the wrong axis
// masquerades as deliberately placed. Demand the field present and a recognized enum on
// every item.
func requireVolatilitiesFields(raw []byte) error {
	var top struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("the volatilities model is not a JSON object: %w", err)
	}
	for i, iRaw := range top.Items {
		obj, err := rawObject(iRaw)
		if err != nil {
			return fmt.Errorf("volatility %d is not a JSON object: %w", i+1, err)
		}
		label := volatilityLabel(obj, i)
		if err := requirePresent(obj, "axis", label); err != nil {
			return err
		}
		var ax Axis
		if err := json.Unmarshal(obj["axis"], &ax); err != nil {
			return fmt.Errorf("%s has an unrecognized axis: %w — use one of sameCustomerOverTime|allCustomersAtOneTime", label, err)
		}
	}
	return nil
}

// ---- raw-JSON presence helpers ----

// rawObject decodes one JSON value into a key→raw map, so we can test key presence.
func rawObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// requirePresent asserts key exists and is not JSON null.
func requirePresent(obj map[string]json.RawMessage, key, label string) error {
	v, ok := obj[key]
	if !ok || isJSONNull(v) {
		return fmt.Errorf("%s is missing required field %q — the strict codec would silently default the absent field to its enum zero value; emit an explicit value", label, key)
	}
	return nil
}

// requireNonEmptyString asserts key exists and is a non-empty (trimmed) JSON string.
func requireNonEmptyString(obj map[string]json.RawMessage, key, label string) error {
	if err := requirePresent(obj, key, label); err != nil {
		return err
	}
	var s string
	if err := json.Unmarshal(obj[key], &s); err != nil {
		return fmt.Errorf("%s field %q must be a string: %w", label, key, err)
	}
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s field %q must not be empty", label, key)
	}
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return string(raw) == "null"
}

// componentLabel builds a stable, human-readable component label for error messages,
// preferring the emitted name.
func componentLabel(obj map[string]json.RawMessage, i int) string {
	if v, ok := obj["name"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
			return fmt.Sprintf("component %d (%q)", i+1, s)
		}
	}
	return fmt.Sprintf("component %d", i+1)
}

// checkItemLabel builds a human label for a standard-check item, preferring its
// guideline text then its section.
func checkItemLabel(obj map[string]json.RawMessage, i int) string {
	for _, key := range []string{"guideline", "section"} {
		if v, ok := obj[key]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
				return fmt.Sprintf("standard-check item %d (%q)", i+1, s)
			}
		}
	}
	return fmt.Sprintf("standard-check item %d", i+1)
}

// volatilityLabel builds a human label for a volatility, preferring its name.
func volatilityLabel(obj map[string]json.RawMessage, i int) string {
	if v, ok := obj["name"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
			return fmt.Sprintf("volatility %d (%q)", i+1, s)
		}
	}
	return fmt.Sprintf("volatility %d", i+1)
}

func useCaseLabel(obj map[string]json.RawMessage, i int) string {
	if v, ok := obj["name"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
			return fmt.Sprintf("use case %d (%q)", i+1, s)
		}
	}
	return fmt.Sprintf("use case %d", i+1)
}

// enumName renders an ordinal enum value as its wire name for error messages, falling
// back to the ordinal if unnamed.
func enumName[T ~int](names map[T]string, v T) string {
	if n, ok := names[v]; ok {
		return n
	}
	return fmt.Sprintf("%d", int(v))
}
