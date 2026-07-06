package main

// constructverbs.go holds the COMPOSED CONSTRUCTION VERBS — the Phase-3 analogue of the
// design verbs in state.go. A construction agent (senior-developer detailed-design,
// junior-developer construction, test-engineer, …) records its phase artifact into the
// flat construction targets of project.json THROUGH these tools instead of hand-editing
// the file + running git. They mirror putDraftModel's discipline exactly: read the
// checked-out project, apply the typed mutation, then validate the WHOLE re-encoded
// aggregate through the strict server codec AND methodcheck (the required CI gate) before
// writing — so a malformed contract / artifact is rejected in-loop with an actionable
// error, never committed to stall the rail. publishDraft is the single exactly-once
// commit+push (reused unchanged).
//
// The write TARGETS mirror the-method-project-state's construction write map:
//   - recordServiceContract → .serviceContracts[<ambient component>]  (detailed-design)
//   - recordPhaseArtifact   → .phaseArtifacts.<field>[<mapKey>]        (srs/uiDesign/…)
//   - recordTestingState    → .testingState.<field>                   (test plans/results)
//
// recordPhaseArtifact and recordTestingState both route through
// projectstate.ApplyPhaseArtifactPayload — the SAME pure routing the server RA's
// RecordPhaseArtifactProduced uses — so there is one source of truth for which payload
// field lands in which slot. The ambient component/activity come from the construct
// session context (AIARCH_COMPONENT_ID / AIARCH_ACTIVITY_ID); the agent never chooses a
// target slot, exactly as the design job's ambient kind fixes its slot.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// recordServiceContractInput carries the typed ServiceContract for the ambient
// component. The component key is NOT an input — the construct session's
// AIARCH_COMPONENT_ID fixes it.
type recordServiceContractInput struct {
	Contract map[string]any `json:"contract" jsonschema:"the complete typed ServiceContract for THIS activity's component — validated through the full server codec and the Method CI rules before it is accepted"`
}

// recordPhaseArtifactInput carries a phase-artifact payload (a PhaseArtifactPayload
// tagged union — exactly one field set) plus the mapKey it is stored under (the
// component/surface/resource/doc name).
type recordPhaseArtifactInput struct {
	MapKey  string         `json:"mapKey" jsonschema:"the map key this artifact is stored under (the component/surface/resource/doc name)"`
	Payload map[string]any `json:"payload" jsonschema:"a PhaseArtifactPayload with EXACTLY ONE phase-artifact field set (e.g. SRS, UIDesign, IntegrationNote, ProvisioningSpec, DeployNote, DocOutline, DocNote)"`
}

// recordTestingStateInput carries a testing-state payload (a PhaseArtifactPayload with
// exactly one project-level testing field set). mapKey is unused for testing-state
// singletons, so it is not an input.
type recordTestingStateInput struct {
	Payload map[string]any `json:"payload" jsonschema:"a PhaseArtifactPayload with EXACTLY ONE testing-state field set (SystemTestPlan, HarnessModule, PerfHarness, QualityGate, TestRun, Defect, or QualityAuditReport)"`
}

// applyConstructionMutation is the shared validate-then-write core for the construction
// verbs — the construction analogue of putDraftModel's gates. It reads the checkout,
// applies the caller's typed mutation, then re-encodes and validates the WHOLE aggregate
// through the strict server codec (read-back parity) AND methodcheck (the CI gate),
// writing only if BOTH pass. On any failure NOTHING is written and a typed error is
// returned (surfaced to the agent as a self-correctable IsError result).
func (s *Session) applyConstructionMutation(what string, mutate func(*projectstate.Project) error) error {
	proj, _, err := s.readProject()
	if err != nil {
		return err
	}
	if err := mutate(&proj); err != nil {
		return err
	}
	newBytes, err := projectstate.EncodeProjectJSON(proj)
	if err != nil {
		return fmt.Errorf("encode project state: %w", err)
	}
	if _, _, derr := projectstate.DecodeProjectJSON(newBytes, s.ProjectID); derr != nil {
		return fmt.Errorf("the %s change would be rejected by the server on read-back: %w", what, derr)
	}
	findings, ferr := methodcheck.ValidateProjectJSON(newBytes)
	if ferr != nil {
		return fmt.Errorf("the Method coherence check failed: %w", ferr)
	}
	if errs := filterErrorFindings(findings); len(errs) > 0 {
		return fmt.Errorf("the %s change violates %d Method rule(s) that the required CI check enforces — fix them and record again:\n%s",
			what, len(errs), formatFindings(errs))
	}
	if err := s.writeProjectBytes(newBytes); err != nil {
		return fmt.Errorf("write project state: %w", err)
	}
	s.wroteState = true
	return nil
}

// recordServiceContract writes the typed ServiceContract for the AMBIENT component into
// .serviceContracts, lazy-allocating the map — the detailed-design phase's artifact.
func (s *Session) recordServiceContract(contract map[string]any) error {
	if s.ComponentID == "" {
		return fmt.Errorf("recordServiceContract needs the ambient component (AIARCH_COMPONENT_ID); this construct job has none — it is not a component activity")
	}
	b, err := marshalInputModel(contract)
	if err != nil {
		return err
	}
	var sc projectstate.ServiceContract
	if err := json.Unmarshal(b, &sc); err != nil {
		return fmt.Errorf("the service contract does not conform to its schema (the server would reject this on read-back): %v"+
			" — fix the field and call recordServiceContract again", err)
	}
	return s.applyConstructionMutation("service contract", func(p *projectstate.Project) error {
		if p.ServiceContracts == nil {
			p.ServiceContracts = make(map[string]projectstate.ServiceContract)
		}
		p.ServiceContracts[s.ComponentID] = sc
		return nil
	})
}

// recordPhaseArtifact routes a phase-artifact payload into .phaseArtifacts under mapKey,
// via the same server routing (ApplyPhaseArtifactPayload). Requires the ambient activity.
func (s *Session) recordPhaseArtifact(mapKey string, payload map[string]any) error {
	if s.ActivityID == "" {
		return fmt.Errorf("recordPhaseArtifact needs the ambient activity (AIARCH_ACTIVITY_ID); this construct job has none")
	}
	pl, err := decodePhaseArtifactPayload(payload)
	if err != nil {
		return err
	}
	return s.applyConstructionMutation("phase artifact", func(p *projectstate.Project) error {
		projectstate.ApplyPhaseArtifactPayload(p, strings.TrimSpace(mapKey), pl)
		return nil
	})
}

// recordTestingState routes a testing-state payload into .testingState (project-level
// singletons/slices; mapKey unused), via the same server routing.
func (s *Session) recordTestingState(payload map[string]any) error {
	if s.ActivityID == "" {
		return fmt.Errorf("recordTestingState needs the ambient activity (AIARCH_ACTIVITY_ID); this construct job has none")
	}
	pl, err := decodePhaseArtifactPayload(payload)
	if err != nil {
		return err
	}
	return s.applyConstructionMutation("testing state", func(p *projectstate.Project) error {
		projectstate.ApplyPhaseArtifactPayload(p, "", pl)
		return nil
	})
}

// decodePhaseArtifactPayload strictly decodes the agent-supplied payload map into the
// typed PhaseArtifactPayload tagged union.
func decodePhaseArtifactPayload(payload map[string]any) (projectstate.PhaseArtifactPayload, error) {
	b, err := marshalInputModel(payload)
	if err != nil {
		return projectstate.PhaseArtifactPayload{}, err
	}
	var pl projectstate.PhaseArtifactPayload
	if err := json.Unmarshal(b, &pl); err != nil {
		return projectstate.PhaseArtifactPayload{}, fmt.Errorf("the payload does not conform to PhaseArtifactPayload (the server would reject this on read-back): %v"+
			" — set exactly one payload field and try again", err)
	}
	return pl, nil
}
