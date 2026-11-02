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
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
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
	MapKey  string         `json:"mapKey" jsonschema:"the map key this artifact is stored under (the component/surface/resource/doc name; a testPlan is filed under its contract key)"`
	Payload map[string]any `json:"payload" jsonschema:"a PhaseArtifactPayload with EXACTLY ONE phase-artifact field set (e.g. SRS, UIDesign, IntegrationNote, ProvisioningSpec, DeployNote, DocOutline, DocNote)"`
}

// recordTestingStateInput carries a testing-state payload (a PhaseArtifactPayload with
// exactly one project-level testing field set). mapKey is unused for testing-state
// singletons, so it is not an input. CheckRun is NOT among the admitted fields: the run
// is the venue's record (record-check-run), never the agent's claim.
type recordTestingStateInput struct {
	Payload map[string]any `json:"payload" jsonschema:"a PhaseArtifactPayload with EXACTLY ONE testing-state field set (HarnessModule, PerfHarness, QualityGate, Defect, or QualityAuditReport); a CheckRun is refused — the venue's record-check-run step records runs"`
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
	// Same gate severity policy seam as putDraftModel and the CI `validate` subcommand
	// (staleness.go), in WHOLE-DOCUMENT mode: a construct session works on a component +
	// activity, not a Phase-1/2 artifact slot, so there is no ambient slot to scope to —
	// staleness applies, slot-scoping does not (earmark: if construction ever meets
	// grandfathered design-slot defects the way the design rail did, extending the
	// ambient concept to the construction targets is a founder decision).
	findings = applyGateSeverityPolicies(proj, 0, false, findings)
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

// applyEvidenceMutation writes venue EVIDENCE (a CheckRun) red or green (deviation 5):
// it reads the checkout, applies the mutation, and holds the re-encoded aggregate to the
// strict server codec's read-back alone. It deliberately skips methodcheck: a red rule
// check means the document carries Error findings, and the validated path
// (applyConstructionMutation) would refuse to record the very run that shows them.
func (s *Session) applyEvidenceMutation(what string, mutate func(*projectstate.Project) error) error {
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
		return fmt.Errorf("the %s would be rejected by the server on read-back: %w", what, derr)
	}
	if err := s.writeProjectBytes(newBytes); err != nil {
		return fmt.Errorf("write project state: %w", err)
	}
	s.wroteState = true
	return nil
}

// recordServiceContract writes the typed ServiceContract for the AMBIENT component into
// .serviceContracts, lazy-allocating the map — the detailed-design phase's artifact. The
// ambient component is the architecture component's id (AIARCH_COMPONENT_ID, kebab-case);
// the contract is filed under the component's contract key (projectstate.ContractKeyFor:
// lowerCamel(id)), the one key every reader of .serviceContracts uses. A Resource has no
// contract, and a component the System does not name has nothing to record.
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
		key, err := contractKeyFor(*p, s.ComponentID)
		if err != nil {
			return err
		}
		if err := canonicalizeContract(key, &sc); err != nil {
			return err
		}
		if p.ServiceContracts == nil {
			p.ServiceContracts = make(map[string]projectstate.ServiceContract)
		}
		p.ServiceContracts[key] = sc
		return nil
	})
}

// canonicalizeContract lowercases the contract's interface layer and holds it to what the
// generator can build, using the generator's own menu. A contract that gets past here but not
// past modelgen would abort generation for the whole project much later, far from the agent
// that can fix it; a capitalised layer used to silently emit a package with no call Context
// and no constructor.
func canonicalizeContract(key string, sc *projectstate.ServiceContract) error {
	if sc.Interface.Layer == "" {
		sc.Interface.Layer = sc.Layer
	}
	if layer, ok := modelgen.CanonicalLayer(sc.Interface.Layer); ok {
		sc.Interface.Layer = layer
	}
	raw, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("recordServiceContract: encode the contract: %w", err)
	}
	if err := modelgen.ValidateContract(key, raw); err != nil {
		return fmt.Errorf("recordServiceContract: %w — fix the field and call recordServiceContract again", err)
	}
	return nil
}

// contractKeyFor resolves the System component componentID to the key its contract is
// filed under (projectstate.ContractKeyFor). A project with no System model, a
// component the System does not name, or a Resource has no contract to record.
func contractKeyFor(p projectstate.Project, componentID string) (string, error) {
	sys, ok := p.SystemDesign.Model.(*projectstate.System)
	if !ok || sys == nil {
		return "", fmt.Errorf("recordServiceContract: the project has no System model, so component %q has no contract to record", componentID)
	}
	for _, c := range sys.Components {
		if c.ID != componentID {
			continue
		}
		key, ok := projectstate.ContractKeyFor(c)
		if !ok {
			return "", fmt.Errorf("recordServiceContract: component %q is a Resource — a Resource is provisioned, it has no service contract", componentID)
		}
		return key, nil
	}
	return "", fmt.Errorf("recordServiceContract: the System names no component %q", componentID)
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
		key := strings.TrimSpace(mapKey)
		if pl.TestPlan != nil {
			var err error
			if key, err = testPlanKey(p.ServiceContracts, key); err != nil {
				return err
			}
			pl.TestPlan.Component = key
		}
		projectstate.ApplyPhaseArtifactPayload(p, key, pl)
		return nil
	})
}

// testPlanKey resolves what a caller named a test plan by to the serviceContracts key the plan
// is filed under: the key names the generated test, its results directory and the checks that
// read them, so no other spelling may reach the document (methodcheck TP-KEY). A component's
// slot-5 id is accepted when it resolves to exactly one contract. A component with several
// facets has one plan per facet, so its id alone is refused, naming them.
func testPlanKey(contracts map[string]projectstate.ServiceContract, name string) (string, error) {
	if _, ok := contracts[name]; ok {
		return name, nil
	}
	want := scenario.Normalize(name)
	var found []string
	for key, sc := range contracts {
		if scenario.Normalize(key) == want || scenario.Normalize(sc.Component) == want {
			found = append(found, key)
		}
	}
	slices.Sort(found)
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no service contract is recorded for test plan %q; a plan is filed under its component's contract key, so record the contract first", name)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("component %q publishes %d contract facets, and each has its own plan: record this plan under one of %s", name, len(found), strings.Join(found, ", "))
}

// recordTestingState routes a testing-state payload into .testingState (project-level
// singletons/slices; mapKey unused), via the same server routing.
//
// THE RUN HALF IS REFUSED. A CheckRun is the venue's measured evidence (record-check-run
// writes it from the check gate's results); the testing gate judges that evidence, not
// an agent's claim, and a verb the agent can call that recorded a CheckRun would let a
// fabricated all-pass run, stamped with the judged attempt id, pass the gate. The
// routing (ApplyPhaseArtifactPayload) still carries CheckRun because the venue's
// subcommand goes through it; only this agent-facing entry closes the door.
func (s *Session) recordTestingState(payload map[string]any) error {
	if s.ActivityID == "" {
		return fmt.Errorf("recordTestingState needs the ambient activity (AIARCH_ACTIVITY_ID); this construct job has none")
	}
	pl, err := decodePhaseArtifactPayload(payload)
	if err != nil {
		return err
	}
	if pl.CheckRun != nil {
		return fmt.Errorf("recordTestingState does not record check runs: a run is the venue's measured evidence, " +
			"recorded by the workflow's `record-check-run` step from the check gate's results — " +
			"an agent cannot claim one (run_checks runs the checks without recording)")
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
