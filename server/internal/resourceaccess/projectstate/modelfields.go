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
	default:
		// Every other artifact's required-enum surface either has no zero-value hole that
		// silently corrupts a Method rule, or is fully guarded by methodcheck; add a case
		// here as new enum-bearing models acquire a hole.
		return nil
	}
}

// requireSystemFields enforces the presence + consistency of the System model's
// closed-enum / identity fields: every component's id/name/kind/layer, every
// relationship's from/to/mode, and every dynamic view's useCaseId (and its edges'
// from/to/mode). The load-bearing check is layer==canonicalLayer(kind): it catches the
// live F81 case (kind present, layer omitted→client) as a mismatch. The both-omitted
// case (kind AND layer absent → both client → self-consistent) is caught by the presence
// checks below and, at the whole-system level, by the SYSTEM-LAYER-DEGENERATE rule.
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
		// NOTE: "implementation" (coded|hybrid|agentic) is deliberately NOT required here.
		// Unlike layer/kind, its zero value (coded) is the semantically-safe default — an
		// omitted field correctly means "ordinary code" — so there is no silent-corruption
		// hole to close. The real hazard (an agentic component that forgot to declare
		// itself) is caught by the DV-AGENTIC-STEP-OWNER-NOT-AGENTIC lint, not by presence.
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
		if act, ok := uc["activity"]; ok && !isJSONNull(act) {
			if err := requireActivityFields(act, label); err != nil {
				return err
			}
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
	if nodes, ok := act["nodes"]; ok && !isJSONNull(nodes) {
		var nodeRaws []json.RawMessage
		if err := json.Unmarshal(nodes, &nodeRaws); err != nil {
			return fmt.Errorf("%s activity nodes is not a JSON array: %w", ucLabel, err)
		}
		for i, nRaw := range nodeRaws {
			obj, err := rawObject(nRaw)
			if err != nil {
				return fmt.Errorf("%s activity node %d is not a JSON object: %w", ucLabel, i+1, err)
			}
			label := fmt.Sprintf("%s activity node %d", ucLabel, i+1)
			if err := requirePresent(obj, "kind", label); err != nil {
				return err
			}
			var nk ActivityNodeKind
			if err := json.Unmarshal(obj["kind"], &nk); err != nil {
				return fmt.Errorf("%s has an unrecognized kind: %w", label, err)
			}
		}
	}
	if edges, ok := act["edges"]; ok && !isJSONNull(edges) {
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
