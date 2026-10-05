// Hooks for designHealthEngine scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package designhealth_test

import (
	"encoding/json"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/designhealth"
)

// newSubject builds the component under test exactly as its caller
// (deliveryManager) wires it: through the generated public constructor. The
// design-health engine is a pure Engine with no downstream dependencies, so
// nothing on the host needs arranging.
func newSubject(t *testing.T, h *scenariohost.Host) designhealth.DesignHealthEngine {
	t.Helper()
	_ = h
	return designhealth.NewDesignHealthEngine()
}

// stepAddAUseCaseToAnInFlightProjectP3S1 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 1: EvaluateDesignHealth
// Projected stimulus (JSON): {"seq":1,"node":"revalidate","input":{"kind":"call","from":"delivery-manager","to":"designhealthengine","op":"EvaluateDesignHealth","call":"add-a-use-case-to-an-in-flight-project/revalidate/3"},"expectedOutputs":[{"node":"design-health-findings-signal","kind":"sendSignal","to":"architect-user","label":"Report design-health findings to the architect"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"EvaluateDesignHealth","inputs":[{"name":"project","value":"\"{\\\"slots\\\":{\\\"coreUseCases\\\":{\\\"kind\\\":4,\\\"model\\\":{\\\"decisions\\\":[{\\\"useCase\\\":{\\\"id\\\":\\\"export-a-report\\\",\\\"name\\\":\\\"Export a Report\\\",\\\"classification\\\":\\\"core\\\",\\\"trigger\\\":\\\"The architect asks for a report\\\",\\\"actors\\\":[{\\\"id\\\":\\\"architect-user\\\",\\\"name\\\":\\\"Architect\\\"}],\\\"activity\\\":{\\\"nodes\\\":[{\\\"id\\\":\\\"start\\\",\\\"kind\\\":\\\"start\\\",\\\"label\\\":\\\"start\\\"},{\\\"id\\\":\\\"export\\\",\\\"kind\\\":\\\"action\\\",\\\"label\\\":\\\"Export the report\\\",\\\"linkedActorId\\\":\\\"architect-user\\\"},{\\\"id\\\":\\\"end\\\",\\\"kind\\\":\\\"end\\\",\\\"label\\\":\\\"end\\\"}],\\\"edges\\\":[{\\\"from\\\":\\\"start\\\",\\\"to\\\":\\\"export\\\",\\\"kind\\\":\\\"controlFlow\\\"},{\\\"from\\\":\\\"export\\\",\\\"to\\\":\\\"end\\\",\\\"kind\\\":\\\"controlFlow\\\"}]}},\\\"essenceRationale\\\":\\\"Reporting is how the business is accountable\\\"}]}}}}\""}],"expect":{"result":"[{\"ruleId\":\"DH-COV-UC-DYNAMIC\",\"severity\":\"error\",\"location\":{\"ordinal\":0,\"section\":\"useCase export-a-report\"}},{\"ruleId\":\"USECASE-DYNAMIC-MISSING\",\"severity\":\"error\",\"location\":{\"ordinal\":0,\"section\":\"use case Export a Report\"}}]","errorExpected":false},"probes":[],"unobservable":["design-health-findings-signal"],"hook":true}
// The guards (core-check=[core: rare], replan-check=[no]) are the caller's branch, not this Engine's state: the
// Engine is pure, so the step is the call itself on the binding's project.
func stepAddAUseCaseToAnInFlightProjectP3S1(t *testing.T, h *scenariohost.Host, subject designhealth.DesignHealthEngine, in inputAddAUseCaseToAnInFlightProjectP3S1) (any, error) {
	t.Helper()
	return subject.EvaluateDesignHealth(callContext(t, h, "stepAddAUseCaseToAnInFlightProjectP3S1"), stringParam(in.Project))
}

// stepAddAUseCaseToAnInFlightProjectP4S1 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 1: EvaluateDesignHealth
// Projected stimulus (JSON): {"seq":1,"node":"revalidate","input":{"kind":"call","from":"delivery-manager","to":"designhealthengine","op":"EvaluateDesignHealth","call":"add-a-use-case-to-an-in-flight-project/revalidate/3"},"expectedOutputs":[{"node":"design-health-findings-signal","kind":"sendSignal","to":"architect-user","label":"Report design-health findings to the architect"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"EvaluateDesignHealth","inputs":[{"name":"project","value":"\"{\\\"slots\\\":{\\\"coreUseCases\\\":{\\\"kind\\\":4,\\\"model\\\":{\\\"decisions\\\":[{\\\"useCase\\\":{\\\"id\\\":\\\"export-a-report\\\",\\\"name\\\":\\\"Export a Report\\\",\\\"classification\\\":\\\"core\\\",\\\"trigger\\\":\\\"The architect asks for a report\\\",\\\"actors\\\":[{\\\"id\\\":\\\"architect-user\\\",\\\"name\\\":\\\"Architect\\\"}],\\\"activity\\\":{\\\"nodes\\\":[{\\\"id\\\":\\\"start\\\",\\\"kind\\\":\\\"start\\\",\\\"label\\\":\\\"start\\\"},{\\\"id\\\":\\\"export\\\",\\\"kind\\\":\\\"action\\\",\\\"label\\\":\\\"Export the report\\\",\\\"linkedActorId\\\":\\\"architect-user\\\"},{\\\"id\\\":\\\"end\\\",\\\"kind\\\":\\\"end\\\",\\\"label\\\":\\\"end\\\"}],\\\"edges\\\":[{\\\"from\\\":\\\"start\\\",\\\"to\\\":\\\"export\\\",\\\"kind\\\":\\\"controlFlow\\\"},{\\\"from\\\":\\\"export\\\",\\\"to\\\":\\\"end\\\",\\\"kind\\\":\\\"controlFlow\\\"}]}},\\\"essenceRationale\\\":null}]}}}}\""}],"expect":{"result":"[{\"ruleId\":\"DH-COV-UC-DYNAMIC\",\"severity\":\"error\",\"location\":{\"ordinal\":0,\"section\":\"useCase export-a-report\"}},{\"ruleId\":\"DH-UC-ESSENCE-MISSING\",\"severity\":\"warning\",\"location\":{\"ordinal\":0,\"section\":\"useCase export-a-report\"}},{\"ruleId\":\"USECASE-DYNAMIC-MISSING\",\"severity\":\"error\",\"location\":{\"ordinal\":0,\"section\":\"use case Export a Report\"}}]","errorExpected":false},"probes":[],"unobservable":["design-health-findings-signal"],"hook":true}
// The guards (core-check=[core: rare], replan-check=[yes]) are the caller's branch, not this Engine's state: the
// Engine is pure, so the step is the call itself on the binding's project.
func stepAddAUseCaseToAnInFlightProjectP4S1(t *testing.T, h *scenariohost.Host, subject designhealth.DesignHealthEngine, in inputAddAUseCaseToAnInFlightProjectP4S1) (any, error) {
	t.Helper()
	return subject.EvaluateDesignHealth(callContext(t, h, "stepAddAUseCaseToAnInFlightProjectP4S1"), stringParam(in.Project))
}

// stepExecuteAProjectActivityP3S1 — Execute a Project Activity — review · agents only · approve, step 1: EvaluateDesignHealth
// Projected stimulus (JSON): {"seq":1,"node":"commit-artifact","input":{"kind":"call","from":"delivery-manager","to":"designhealthengine","op":"EvaluateDesignHealth","call":"execute-a-project-activity/commit-artifact/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"EvaluateDesignHealth","inputs":[{"name":"project","value":"\"{\\\"slots\\\":{\\\"glossary\\\":{\\\"kind\\\":1,\\\"model\\\":{\\\"items\\\":[{\\\"term\\\":\\\"Architect\\\",\\\"definition\\\":\\\"The user who designs the system\\\",\\\"category\\\":\\\"Who\\\"},{\\\"term\\\":\\\"Use Case\\\",\\\"definition\\\":\\\"A required behavior of the system\\\",\\\"category\\\":\\\"What\\\"},{\\\"term\\\":\\\"Review\\\",\\\"definition\\\":\\\"How an artifact is approved\\\",\\\"category\\\":\\\"How\\\"},{\\\"term\\\":\\\"Repository\\\",\\\"definition\\\":\\\"Where project state lives\\\",\\\"category\\\":\\\"Where\\\"}]}},\\\"requirements\\\":{\\\"kind\\\":2,\\\"model\\\":{\\\"items\\\":[{\\\"id\\\":\\\"R1\\\",\\\"statement\\\":\\\"The system shall commit a reviewed artifact.\\\"}]}}}}\""}],"expect":{"result":"[]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The guards (task-kind=review, requires-human=agents only, verdict=approve) are the caller's branch, not this Engine's state: the
// Engine is pure, so the step is the call itself on the binding's project.
func stepExecuteAProjectActivityP3S1(t *testing.T, h *scenariohost.Host, subject designhealth.DesignHealthEngine, in inputExecuteAProjectActivityP3S1) (any, error) {
	t.Helper()
	return subject.EvaluateDesignHealth(callContext(t, h, "stepExecuteAProjectActivityP3S1"), stringParam(in.Project))
}

// stepExecuteAProjectActivityP4S1 — Execute a Project Activity — review · human required · approve, step 1: EvaluateDesignHealth
// Projected stimulus (JSON): {"seq":1,"node":"commit-artifact","input":{"kind":"call","from":"delivery-manager","to":"designhealthengine","op":"EvaluateDesignHealth","call":"execute-a-project-activity/commit-artifact/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"EvaluateDesignHealth","inputs":[{"name":"project","value":"\"{\\\"slots\\\":{\\\"glossary\\\":{\\\"kind\\\":1,\\\"model\\\":{\\\"items\\\":[{\\\"term\\\":\\\"Architect\\\",\\\"definition\\\":\\\"The user who designs the system\\\",\\\"category\\\":\\\"Who\\\"},{\\\"term\\\":\\\"Use Case\\\",\\\"definition\\\":\\\"A required behavior of the system\\\",\\\"category\\\":\\\"What\\\"},{\\\"term\\\":\\\"Review\\\",\\\"definition\\\":\\\"How an artifact is approved\\\",\\\"category\\\":\\\"How\\\"}]}},\\\"requirements\\\":{\\\"kind\\\":2,\\\"model\\\":{\\\"items\\\":[{\\\"id\\\":\\\"R1\\\",\\\"statement\\\":\\\"The system shall commit a reviewed artifact.\\\"}]}}}}\""}],"expect":{"result":"[{\"ruleId\":\"GLOSS-FOURQ\",\"severity\":\"warning\",\"location\":{\"ordinal\":0,\"section\":\"glossary\"}}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The guards (task-kind=review, requires-human=human required, verdict=approve) are the caller's branch, not this Engine's state: the
// Engine is pure, so the step is the call itself on the binding's project.
func stepExecuteAProjectActivityP4S1(t *testing.T, h *scenariohost.Host, subject designhealth.DesignHealthEngine, in inputExecuteAProjectActivityP4S1) (any, error) {
	t.Helper()
	return subject.EvaluateDesignHealth(callContext(t, h, "stepExecuteAProjectActivityP4S1"), stringParam(in.Project))
}

// stringParam reads a string-typed binding input the way methodcheck's
// TP-ARG-TYPE reads it: a value that is valid JSON is the JSON value it spells, so
// a string parameter whose value is a JSON string literal ("\"{…}\"") carries
// that literal's contents. The binding spells the project document that way
// because TP-ARG-TYPE rejects an unquoted document (it parses as an object, not a
// string); testgen v0.12.0, though, quotes every string-typed value whole, so the
// decoded input still holds the literal. A value that is not a JSON string
// literal is the parameter itself.
func stringParam(value string) string {
	var s string
	if json.Unmarshal([]byte(value), &s) != nil {
		return value
	}
	return s
}
