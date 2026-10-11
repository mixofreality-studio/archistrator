package main

// scenarioverbs.go holds the SCENARIO read verb (deterministic component testing,
// design §3.2): listComponentScenarios returns the canonical scenario set projected
// onto one component, derived server-side through projectstate.ComponentScenarios so
// the test-engineer agent binds the ids the gate and the emitter use and never
// re-derives paths itself. Read-only; shared across the design and construct modes.

import (
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
