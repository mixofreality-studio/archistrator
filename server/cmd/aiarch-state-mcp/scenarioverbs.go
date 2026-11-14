package main

// scenarioverbs.go holds the SCENARIO read verb (deterministic component testing,
// design §3.2): listComponentScenarios returns the canonical scenario set projected
// onto one component, derived server-side through projectstate.ComponentScenarios so
// the test-engineer agent binds the ids the gate and the emitter use and never
// re-derives paths itself. Read-only; shared across the design and construct modes.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

type listComponentScenariosInput struct {
	Component string `json:"component" jsonschema:"contract key or slot-5 id of the component whose projected scenarios to list"`
}

// listComponentScenarios derives the project's scenarios from the checked-out
// project.json and returns, as canonical JSON, those with at least one stimulus into
// component — each with only the stimuli addressed to it. An empty projection is
// returned as an empty JSON array, not an error: a component no use case drives has
// nothing to bind.
func (s *Session) listComponentScenarios(component string) (string, error) {
	component = strings.TrimSpace(component)
	if component == "" {
		return "", fmt.Errorf("listComponentScenarios requires the component (a serviceContracts key such as \"deliveryManager\" or its slot-5 id)")
	}
	p, _, err := s.readProject()
	if err != nil {
		return "", err
	}
	sc, err := projectstate.ComponentScenarios(&p, component)
	if err != nil {
		return "", err
	}
	b, err := scenario.CanonicalJSON(sc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// acceptanceResidualEntry is one residual acceptance variant and why it is residual, as the verb
// reports it: the reason is the {kind, model} envelope every sum type travels in.
type acceptanceResidualEntry struct {
	Scenario string           `json:"scenario"`
	Reason   acceptanceReason `json:"reason"`
}

type acceptanceReason struct {
	Kind  string `json:"kind"`
	Model any    `json:"model"`
}

// reasonOf is a residual reason as its envelope. The switch is exhaustive over the sealed
// scenario.ResidualReason (gochecksumtype), so a new reason cannot reach an agent unnamed.
func reasonOf(r scenario.ResidualReason) acceptanceReason {
	switch v := r.(type) {
	case scenario.CrossClient:
		return acceptanceReason{Kind: "crossClient", Model: struct {
			Clients []string `json:"clients"`
		}{v.Clients}}
	case scenario.EventEntered:
		return acceptanceReason{Kind: "eventEntered", Model: struct {
			Node string `json:"node"`
		}{v.Node}}
	case scenario.LaterOutcome:
		return acceptanceReason{Kind: "laterOutcome", Model: struct {
			OutcomeNode   string `json:"outcomeNode"`
			AfterStimulus string `json:"afterStimulus"`
		}{v.OutcomeNode, v.AfterStimulus}}
	}
	return acceptanceReason{}
}

// listAcceptanceScenarios returns the acceptance variants no single client plan covers (the
// residual the acceptance test plan binds, and nothing else), as canonical scenarios, with each
// variant's reason. An empty residual is an empty list, not an error: a project whose paths every
// client plan covers has nothing to accept beyond them.
func (s *Session) listAcceptanceScenarios() (string, error) {
	p, _, err := s.readProject()
	if err != nil {
		return "", err
	}
	scenarios, err := projectstate.AcceptanceScenarios(&p)
	if err != nil {
		return "", err
	}
	residual, err := projectstate.AcceptanceResidual(&p)
	if err != nil {
		return "", err
	}
	canonical, err := scenario.CanonicalJSON(scenarios)
	if err != nil {
		return "", err
	}
	entries := make([]acceptanceResidualEntry, 0, len(residual))
	for _, r := range residual {
		entries = append(entries, acceptanceResidualEntry{Scenario: r.Scenario, Reason: reasonOf(r.Reason)})
	}
	out, err := json.Marshal(struct {
		Scenarios json.RawMessage           `json:"scenarios"`
		Residual  []acceptanceResidualEntry `json:"residual"`
	}{canonical, entries})
	if err != nil {
		return "", err
	}
	return string(out), nil
}
