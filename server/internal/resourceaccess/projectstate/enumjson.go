package projectstate

import (
	"encoding/json"
	"fmt"
)

// This file gives every closed ordinal enum the SPA reads a STRING wire encoding:
// MarshalJSON emits the canonical camelCase name; UnmarshalJSON accepts that name
// AND (for backward compatibility) a bare integer ordinal — so the architect-role
// draft prompts that still emit integer ordinals, and any pre-migration JSONB
// payload, keep decoding. The artifactValidationEngine reads these enums IN-MEMORY
// (Go values), so it is unaffected by the JSON encoding.
//
// The (name→ordinal, ordinal→name) tables are the single source of truth per enum;
// the camelCase names match the OpenAPI string-enum schemas exactly.

// marshalEnum encodes an ordinal enum value as its string name.
func marshalEnum[T ~int](v T, names map[T]string, what string) ([]byte, error) {
	name, ok := names[v]
	if !ok {
		return nil, fmt.Errorf("projectstate: %s(%d) has no wire name", what, int(v))
	}
	return json.Marshal(name)
}

// unmarshalEnum decodes a string name (or legacy integer ordinal) into an ordinal enum.
func unmarshalEnum[T ~int](data []byte, byName map[string]T, what string) (T, error) {
	var zero T
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		v, ok := byName[name]
		if !ok {
			return zero, fmt.Errorf("projectstate: %q is not a recognized %s wire name", name, what)
		}
		return v, nil
	}
	var ordinal int
	if err := json.Unmarshal(data, &ordinal); err != nil {
		return zero, fmt.Errorf("projectstate: %s must be a string wire name or integer ordinal: %w", what, err)
	}
	return T(ordinal), nil
}

// invert builds a name→value table from a value→name table once at init.
func invert[T ~int](names map[T]string) map[string]T {
	m := make(map[string]T, len(names))
	for v, n := range names {
		m[n] = v
	}
	return m
}

// ---- Axis ----

var axisNames = map[Axis]string{
	AxisSameCustomerOverTime:  "sameCustomerOverTime",
	AxisAllCustomersAtOneTime: "allCustomersAtOneTime",
}
var axisByName = invert(axisNames)

// MarshalJSON encodes the Axis as its camelCase wire name.
func (a Axis) MarshalJSON() ([]byte, error) { return marshalEnum(a, axisNames, "Axis") }

// UnmarshalJSON decodes a wire name (or legacy ordinal) into an Axis.
func (a *Axis) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, axisByName, "Axis")
	if err != nil {
		return err
	}
	*a = v
	return nil
}

// ---- CheckStatus ----

var checkStatusNames = map[CheckStatus]string{
	CheckPass:   "pass",
	CheckWaived: "waived",
	CheckFail:   "fail",
}
var checkStatusByName = invert(checkStatusNames)

// MarshalJSON encodes the CheckStatus as its camelCase wire name.
func (c CheckStatus) MarshalJSON() ([]byte, error) {
	return marshalEnum(c, checkStatusNames, "CheckStatus")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a CheckStatus.
func (c *CheckStatus) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, checkStatusByName, "CheckStatus")
	if err != nil {
		return err
	}
	*c = v
	return nil
}

// ---- ComponentKind ----

var componentKindNames = map[ComponentKind]string{
	CompClient:         "client",
	CompManager:        "manager",
	CompEngine:         "engine",
	CompResourceAccess: "resourceAccess",
	CompResource:       "resource",
	CompUtility:        "utility",
}
var componentKindByName = invert(componentKindNames)

// MarshalJSON encodes the ComponentKind as its camelCase wire name.
func (k ComponentKind) MarshalJSON() ([]byte, error) {
	return marshalEnum(k, componentKindNames, "ComponentKind")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a ComponentKind.
func (k *ComponentKind) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, componentKindByName, "ComponentKind")
	if err != nil {
		return err
	}
	*k = v
	return nil
}

// ---- Layer ----

var layerNames = map[Layer]string{
	LayerClient:         "client",
	LayerManager:        "manager",
	LayerEngine:         "engine",
	LayerResourceAccess: "resourceAccess",
	LayerResource:       "resource",
	LayerUtility:        "utility",
}
var layerByName = invert(layerNames)

// MarshalJSON encodes the Layer as its camelCase wire name.
func (l Layer) MarshalJSON() ([]byte, error) { return marshalEnum(l, layerNames, "Layer") }

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a Layer.
func (l *Layer) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, layerByName, "Layer")
	if err != nil {
		return err
	}
	*l = v
	return nil
}

// ---- CallMode ----

var callModeNames = map[CallMode]string{
	CallSync:        "sync",
	CallQueued:      "queued",
	CallEventPubSub: "eventPubSub",
}
var callModeByName = invert(callModeNames)

// MarshalJSON encodes the CallMode as its camelCase wire name.
func (m CallMode) MarshalJSON() ([]byte, error) { return marshalEnum(m, callModeNames, "CallMode") }

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a CallMode.
func (m *CallMode) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, callModeByName, "CallMode")
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// ---- Trigger ----

var triggerNames = map[Trigger]string{
	TriggerClientAction: "clientAction",
	TriggerTimer:        "timer",
	TriggerBusMessage:   "busMessage",
}
var triggerByName = invert(triggerNames)

// MarshalJSON encodes the Trigger as its camelCase wire name.
func (t Trigger) MarshalJSON() ([]byte, error) { return marshalEnum(t, triggerNames, "Trigger") }

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a Trigger.
func (t *Trigger) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, triggerByName, "Trigger")
	if err != nil {
		return err
	}
	*t = v
	return nil
}

// ---- Classification ----

var classificationNames = map[Classification]string{
	ClassCore:    "core",
	ClassNonCore: "nonCore",
}
var classificationByName = invert(classificationNames)

// MarshalJSON encodes the Classification as its camelCase wire name.
func (c Classification) MarshalJSON() ([]byte, error) {
	return marshalEnum(c, classificationNames, "Classification")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a Classification.
func (c *Classification) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, classificationByName, "Classification")
	if err != nil {
		return err
	}
	*c = v
	return nil
}

// ---- ActivityNodeKind ----

var activityNodeKindNames = map[ActivityNodeKind]string{
	NodeStart:         "start",
	NodeAction:        "action",
	NodeDecision:      "decision",
	NodeMerge:         "merge",
	NodeFork:          "fork",
	NodeJoin:          "join",
	NodeEnd:           "end",
	NodeSwimLane:      "swimLane",
	NodeNote:          "note",
	NodeLoop:          "loop",
	NodeSwitch:        "switch",
	NodeGoto:          "goto",
	NodeInterruptEdge: "interruptEdge",
}
var activityNodeKindByName = invert(activityNodeKindNames)

// MarshalJSON encodes the ActivityNodeKind as its camelCase wire name.
func (k ActivityNodeKind) MarshalJSON() ([]byte, error) {
	return marshalEnum(k, activityNodeKindNames, "ActivityNodeKind")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into an ActivityNodeKind.
func (k *ActivityNodeKind) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, activityNodeKindByName, "ActivityNodeKind")
	if err != nil {
		return err
	}
	*k = v
	return nil
}

// ---- DeliveryStyle ----

var deliveryStyleNames = map[DeliveryStyle]string{
	StyleCloud: "cloud",
	StyleLocal: "local",
	StyleBoth:  "both",
}
var deliveryStyleByName = invert(deliveryStyleNames)

// MarshalJSON encodes the DeliveryStyle as its camelCase wire name.
func (s DeliveryStyle) MarshalJSON() ([]byte, error) {
	return marshalEnum(s, deliveryStyleNames, "DeliveryStyle")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a DeliveryStyle.
func (s *DeliveryStyle) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, deliveryStyleByName, "DeliveryStyle")
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// ---- DeploymentProfile ----

var deploymentProfileNames = map[DeploymentProfile]string{
	ProfileCloud: "cloud",
	ProfileLocal: "local",
	ProfileTest:  "test",
}
var deploymentProfileByName = invert(deploymentProfileNames)

// MarshalJSON encodes the DeploymentProfile as its camelCase wire name.
func (p DeploymentProfile) MarshalJSON() ([]byte, error) {
	return marshalEnum(p, deploymentProfileNames, "DeploymentProfile")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a DeploymentProfile.
func (p *DeploymentProfile) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, deploymentProfileByName, "DeploymentProfile")
	if err != nil {
		return err
	}
	*p = v
	return nil
}

// ---- EdgeKind ----

var edgeKindNames = map[EdgeKind]string{
	EdgeControlFlow: "controlFlow",
	EdgeGuardedFlow: "guardedFlow",
}
var edgeKindByName = invert(edgeKindNames)

// MarshalJSON encodes the EdgeKind as its camelCase wire name.
func (k EdgeKind) MarshalJSON() ([]byte, error) { return marshalEnum(k, edgeKindNames, "EdgeKind") }

// UnmarshalJSON decodes a wire name (or legacy ordinal) into an EdgeKind.
func (k *EdgeKind) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, edgeKindByName, "EdgeKind")
	if err != nil {
		return err
	}
	*k = v
	return nil
}
