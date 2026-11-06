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
	"go/token"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
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
	Payload map[string]any `json:"payload" jsonschema:"a PhaseArtifactPayload with EXACTLY ONE phase-artifact field set (e.g. SRS, UIDesign, IntegrationNote, ProvisioningSpec, DeployNote, DocOutline, DocNote). A text record's markdown goes in content: SRS and IntegrationNote {component, content}, UXRequirements and UIDesign {surface, content}, ProvisioningSpec and DeployNote {resource, content}, DocOutline and DocNote {doc, content}; a testPlan is {component, bindings}. A key the record does not declare is refused, never dropped"`
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
	// Every generator loads the state through projectmodel.Load before anything else, so a
	// state it refuses aborts the generation of every later task, none of which may change it.
	if _, lerr := projectmodel.Load(newBytes); lerr != nil {
		// projectmodel.Problems.Error() is one line per problem, each led by its closed PM-*
		// code, so the whole list is shown to the agent at once, never just the first.
		return fmt.Errorf("the %s change would leave a state no generator can load; fix every problem below and record again:\n%w", what, lerr)
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
	// Strict: a lenient decode drops a misspelled key and records its zero value. RUN-013's agent
	// sent interface.ops, and a Utility contract with no operations was written as passing.
	var sc projectstate.ServiceContract
	if err := decodeDraftStrict(b, &sc); err != nil {
		return fmt.Errorf("the service contract does not conform to its schema: %v"+
			" — its shape is %s; fix the field and call recordServiceContract again", err, contractShape)
	}
	return s.applyConstructionMutation("service contract", func(p *projectstate.Project) error {
		comp, key, err := contractComponent(*p, s.ComponentID)
		if err != nil {
			return err
		}
		if err := refuseClientContract(*p, s.ComponentID, sc); err != nil {
			return err
		}
		if err := layerFromSystem(comp, &sc); err != nil {
			return err
		}
		if err := goPackageFromSystem(*p, comp, key, &sc); err != nil {
			return err
		}
		if err := refuseOpLessContract(s.ComponentID, sc); err != nil {
			return err
		}
		if err := depsFromSystem(*p, comp, &sc); err != nil {
			return err
		}
		if err := canonicalizeContract(key, &sc); err != nil {
			return err
		}
		if p.ServiceContracts == nil {
			p.ServiceContracts = make(map[string]projectstate.ServiceContract)
		}
		p.ServiceContracts[key] = sc
		if strings.EqualFold(sc.Layer, "manager") || strings.EqualFold(sc.Interface.Layer, "manager") {
			return projectstate.RecordMachineClientContracts(p)
		}
		return nil
	})
}

// contractShape is the input recordServiceContract takes, named in its refusals and its tool
// description. The goPackage, the layer and a Manager's deps are derived from the System, so the
// shape leaves them out.
const contractShape = "{title, $defs, interface:{name, operations:[{name, params:[{name, schema}], result, error}]}}"

// goPackageFromSystem sets a built contract's Go package from its component, as layerFromSystem
// sets its layer: internal/<layer>/<leaf>, the layout every app's components follow (modelgen and
// the construction arch gate hold it). RUN-016's agent sent the advertised shape, with no
// goPackage; the verb stored it, every generator skipped it without saying so, and construction
// started with no generated contract. A supplied package is kept when it is one Go identifier
// under the layer's directory, even if it is not the derived one; anything else would pass
// modelgen's prefix check and fail to compile, so it is refused. A stub with none is left alone.
func goPackageFromSystem(p projectstate.Project, c projectstate.Component, key string, sc *projectstate.ServiceContract) error {
	layer, ok := modelgen.CanonicalLayer(sc.Layer)
	if !ok {
		return fmt.Errorf("recordServiceContract: component %q's layer %q has no package directory", c.ID, sc.Layer)
	}
	dir := "internal/" + layer + "/"
	if sc.GoPackage != "" {
		leaf, under := strings.CutPrefix(sc.GoPackage, dir)
		if !under || !goPackageLeaf(leaf) {
			return fmt.Errorf("recordServiceContract: goPackage %q must be one Go package directly under %s, named by a lowercase identifier (letters and digits, not a keyword); leave it out and it is derived from component %q, or correct it, and record again", sc.GoPackage, dir, c.ID)
		}
		return nil
	}
	if sc.Stub {
		return nil
	}
	pkg, err := derivedGoPackage(p, c, key)
	if err != nil {
		return err
	}
	sc.GoPackage = pkg
	return nil
}

// derivedGoPackage is the Go package a component's contract lands in: internal/<layer>/ and the
// component's kebab id joined, without its trailing layer word (caller-access ->
// internal/resourceaccess/caller, message-bus -> internal/utility/messagebus). The whole joined
// id is the leaf when the strip leaves nothing, a keyword, or a package another component's
// contract already holds.
func derivedGoPackage(p projectstate.Project, c projectstate.Component, key string) (string, error) {
	layer, ok := modelgen.CanonicalLayer(c.Kind.String())
	if !ok {
		return "", fmt.Errorf("recordServiceContract: component %q is a %s, which has no package directory", c.ID, c.Kind)
	}
	dir := "internal/" + layer + "/"
	parts := strings.Split(strings.ToLower(c.ID), "-")
	full := strings.Join(parts, "")
	held := func(pkg string) bool {
		for k, other := range p.ServiceContracts {
			if k != key && other.GoPackage == pkg {
				return true
			}
		}
		return false
	}
	leaf := full
	if word := layerWord[c.Kind]; word != "" && len(parts) > 1 && parts[len(parts)-1] == word {
		leaf = strings.Join(parts[:len(parts)-1], "")
	}
	if !goPackageLeaf(leaf) || held(dir+leaf) {
		leaf = full
	}
	if !goPackageLeaf(leaf) || held(dir+leaf) {
		return "", fmt.Errorf("recordServiceContract: component %q's contract has no package of its own to derive (%s%s is not a free Go package name); name one in goPackage, one lowercase identifier under %s, and record again", c.ID, dir, leaf, dir)
	}
	return dir + leaf, nil
}

// layerWord is the trailing id word that names a component's layer, dropped from its package leaf.
// A Utility's id carries no layer word, and a Client or a Resource records no built contract.
var layerWord = map[projectstate.ComponentKind]string{
	projectstate.CompResourceAccess: "access",
	projectstate.CompEngine:         "engine",
	projectstate.CompManager:        "manager",
	projectstate.CompUtility:        "",
	projectstate.CompClient:         "",
	projectstate.CompResource:       "",
}

var goPackageLeafRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// goPackageLeaf is whether leaf can name a component package: one lowercase identifier, not a keyword.
func goPackageLeaf(leaf string) bool {
	return goPackageLeafRE.MatchString(leaf) && !token.IsKeyword(leaf)
}

// refuseOpLessContract: a built component's contract is its operations. One with none generates an
// empty interface that construction cannot build against, and no later gate notices: modelgen
// allows zero ops, and the design-health rules count the System's verbs, not the contract's.
func refuseOpLessContract(componentID string, sc projectstate.ServiceContract) error {
	if sc.Stub || len(sc.Interface.Operations) > 0 {
		return nil
	}
	return fmt.Errorf("recordServiceContract: a %s contract must declare at least one operation in interface.operations; "+
		"each one a dynamic-view call into %s makes — its shape is %s", sc.Layer, componentID, contractShape)
}

// refuseClientContract: nobody authors a Client's contract. A browser surface's is derived from
// the design's labels when its UI design is recorded, and a machine client's from its Manager's.
func refuseClientContract(p projectstate.Project, componentID string, sc projectstate.ServiceContract) error {
	if !strings.EqualFold(sc.Layer, "client") && !strings.EqualFold(sc.Interface.Layer, "client") && !projectstate.IsClientComponent(&p, componentID) {
		return nil
	}
	return fmt.Errorf("recordServiceContract: a Client authors no contract: a browser surface's is derived from the actor calls into it when its UI design is recorded, and a machine client's from the Manager behind it, so do not record one")
}

// canonicalizeContract lowercases the contract's interface layer and holds it to what the
// generator can build, using the generator's own menu. A contract that gets past here but not
// past modelgen would abort generation for the whole project much later, far from the agent
// that can fix it; a capitalised layer used to silently emit a package with no call Context
// and no constructor.
//
// The top-level layer takes projectmodel's spelling, the one every generator's projectmodel.Load
// accepts: methodcheck and modelgen match a layer ignoring case, so "resourceaccess" passed
// every gate of the task that recorded it and aborted every generator after it.
func canonicalizeContract(key string, sc *projectstate.ServiceContract) error {
	if sc.Interface.Layer == "" {
		sc.Interface.Layer = sc.Layer
	}
	if layer, ok := modelgen.CanonicalLayer(sc.Interface.Layer); ok {
		sc.Interface.Layer = layer
	}
	if layer, ok := projectmodel.CanonicalLayer(sc.Layer); ok {
		sc.Layer = layer
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

// layerFromSystem sets the contract's layer from the component's kind in the System, spelled as
// projectmodel spells it. The layer is the design's, not the agent's: a supplied layer, top-level
// or the interface's, that names another one is refused rather than silently replaced.
func layerFromSystem(c projectstate.Component, sc *projectstate.ServiceContract) error {
	want, ok := projectmodel.CanonicalLayer(c.Kind.String())
	if !ok {
		return fmt.Errorf("recordServiceContract: component %q is a %s, which has no contract layer", c.ID, c.Kind)
	}
	for _, supplied := range []string{sc.Layer, sc.Interface.Layer} {
		if supplied != "" && !strings.EqualFold(supplied, want) {
			return fmt.Errorf("recordServiceContract: component %q is a %s in the System, so its contract's layer is %q, not %q; the layer follows the design, so leave it out or correct it, and record again", c.ID, c.Kind, want, supplied)
		}
	}
	sc.Layer = want
	return nil
}

// depsFromSystem sets a Manager contract's constructor deps from the System's edges
// (projectstate.ManagerDepsFromSystem), as layerFromSystem sets its layer: the Temporal client,
// then each Engine, ResourceAccess and Utility the Manager calls, by contract key. A plain dep
// the agent supplies (a config scalar, a resolver func) is kept after the client; the client
// itself is kept once. A supplied component dep is the design's to name, so one the System does
// not draw, or draws under another name, is refused rather than silently replaced.
func depsFromSystem(p projectstate.Project, c projectstate.Component, sc *projectstate.ServiceContract) error {
	if c.Kind != projectstate.CompManager {
		return nil
	}
	derived, err := projectstate.ManagerDepsFromSystem(p, c.ID)
	if err != nil {
		return fmt.Errorf("recordServiceContract: %w", err)
	}
	want := map[string]projectstate.ContractDep{}
	var drawn []string
	for _, d := range derived[1:] {
		want[d.Component] = d
		drawn = append(drawn, d.Component)
	}
	deps := []projectstate.ContractDep{derived[0]}
	for _, d := range sc.Deps {
		switch {
		case d.Component == "" && (d.Name == derived[0].Name || (d.GoType == derived[0].GoType && d.GoImport == derived[0].GoImport)):
			if d != derived[0] {
				return fmt.Errorf("recordServiceContract: Manager %q's dep %q is the Temporal client, which is %+v; leave it out and record again", c.ID, d.Name, derived[0])
			}
		case d.Component == "":
			deps = append(deps, d)
		case want[d.Component] != d:
			return fmt.Errorf("recordServiceContract: Manager %q's dep %+v is not one the System draws: its deps follow the design's edges, one per Engine, ResourceAccess and Utility it calls, named by contract key (%s); leave the component deps out, they are derived, and record again",
				c.ID, d, strings.Join(drawn, ", "))
		}
	}
	deps = append(deps, derived[1:]...)
	sc.Deps = deps
	return nil
}

// contractComponent resolves the System component componentID, and the key its contract is
// filed under (projectstate.ContractKeyFor). A project with no System model, a
// component the System does not name, or a Resource has no contract to record.
func contractComponent(p projectstate.Project, componentID string) (projectstate.Component, string, error) {
	sys, ok := p.SystemDesign.Model.(*projectstate.System)
	if !ok || sys == nil {
		return projectstate.Component{}, "", fmt.Errorf("recordServiceContract: the project has no System model, so component %q has no contract to record", componentID)
	}
	for _, c := range sys.Components {
		if c.ID != componentID {
			continue
		}
		key, ok := projectstate.ContractKeyFor(c)
		if !ok {
			return projectstate.Component{}, "", fmt.Errorf("recordServiceContract: component %q is a Resource — a Resource is provisioned, it has no service contract", componentID)
		}
		return c, key, nil
	}
	return projectstate.Component{}, "", fmt.Errorf("recordServiceContract: the System names no component %q", componentID)
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
	if err := refuseSystemEvidence("recordPhaseArtifact", pl); err != nil {
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
		if pl.UIDesign != nil {
			return projectstate.RecordBrowserContract(p, s.ComponentID)
		}
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
	if err := refuseSystemEvidence("recordTestingState", pl); err != nil {
		return err
	}
	return s.applyConstructionMutation("testing state", func(p *projectstate.Project) error {
		projectstate.ApplyPhaseArtifactPayload(p, "", pl)
		return nil
	})
}

// refuseSystemEvidence closes the agent-facing verbs to what only the system may stamp. Both verbs
// reach the same routing (ApplyPhaseArtifactPayload), which carries these fields for the writers
// that do stamp them, so a verb that left the door open would let an agent forge a run or a report.
func refuseSystemEvidence(verb string, pl projectstate.PhaseArtifactPayload) error {
	if pl.PlanDefect != nil {
		return fmt.Errorf("%s does not record plan defects: call reportPlanDefect, which stamps the attempt and the plan baseline", verb)
	}
	if pl.CheckRun != nil {
		return fmt.Errorf("%s does not record check runs: a run is the venue's measured evidence, "+
			"recorded by the workflow's `record-check-run` step from the check gate's results — "+
			"an agent cannot claim one (run_checks runs the checks without recording)", verb)
	}
	return nil
}

// reportPlanDefectInput is integration's allegation. The attempt and the plan baseline are not
// inputs: the verb stamps them, so an agent cannot report for another attempt.
type reportPlanDefectInput struct {
	Contract  string   `json:"contract" jsonschema:"the contract key whose test plan holds the scenarios (a .serviceContracts key)"`
	Scenarios []string `json:"scenarios" jsonschema:"the ids of the scenarios whose binding is wrong; each must have a recorded failing verdict in this attempt's run"`
	Reason    string   `json:"reason" jsonschema:"the contract or use-case clause the binding contradicts; never an observed value"`
}

// reportPlanDefect records integration's allegation that named scenarios' bindings are wrong.
// It writes through the evidence path: a report changes nothing a rule reads, and a document
// already red must still be able to carry it. Whether the report takes effect is the Manager's
// to judge against the attempt's run; this verb refuses only what no run could ever make good.
func (s *Session) reportPlanDefect(in reportPlanDefectInput) error {
	revision := strings.TrimSpace(os.Getenv(envRevision))
	if s.ActivityID == "" || s.TaskID == "" || revision == "" {
		return fmt.Errorf("reportPlanDefect needs the ambient activity, task and attempt; this job has none")
	}
	in.Contract, in.Reason = strings.TrimSpace(in.Contract), strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		return fmt.Errorf("reportPlanDefect needs a reason citing the contract or use-case clause the binding contradicts")
	}
	if len(in.Scenarios) == 0 {
		return fmt.Errorf("reportPlanDefect needs the scenarios whose binding is wrong")
	}
	return s.applyEvidenceMutation("plan defect report", func(p *projectstate.Project) error {
		if err := planDefectRouteDeclared(p, s.ActivityID, s.TaskID); err != nil {
			return err
		}
		contracts, err := componentContracts(p, s.ComponentID)
		if err != nil {
			return err
		}
		if !slices.Contains(contracts, in.Contract) {
			return fmt.Errorf("contract %q is not one of this activity's contracts: %s", in.Contract, strings.Join(contracts, ", "))
		}
		if err := planBindsScenarios(p, in.Contract, in.Scenarios); err != nil {
			return err
		}
		baseline, err := projectstate.PlanBindingDigests(p, contracts)
		if err != nil {
			return err
		}
		projectstate.ApplyPhaseArtifactPayload(p, "", projectstate.PhaseArtifactPayload{PlanDefect: &projectstate.PlanDefectReport{
			ID: revision + ":" + in.Contract, Activity: s.ActivityID, Task: s.TaskID, Revision: revision,
			Contract: in.Contract, Scenarios: slices.Sorted(slices.Values(in.Scenarios)), Reason: in.Reason, Baseline: baseline,
		}})
		return nil
	})
}

// planDefectRouteDeclared refuses a report from a task whose lifecycle has no route back to a
// plan, such as an integration activity sharing the command: there, the component is what to fix.
func planDefectRouteDeclared(p *projectstate.Project, activity, task string) error {
	lc, err := projectstate.ActivityLifecycle(p, activity)
	if err != nil {
		return err
	}
	for _, t := range lc.Tasks {
		if t.ID == task && t.SendsBackTo != "" {
			return nil
		}
	}
	return fmt.Errorf("activity %s's lifecycle has no test plan for task %q to send back to; fix the component", activity, task)
}

// componentContracts are the contracts of a component: its own and every facet naming it.
func componentContracts(p *projectstate.Project, componentID string) ([]string, error) {
	key, ok := projectstate.ContractKeyOf(p, componentID)
	if !ok {
		return nil, fmt.Errorf("the System gives component %q no contract key", componentID)
	}
	var out []string
	for name, sc := range p.ServiceContracts {
		if name == key || scenario.Normalize(sc.Component) == scenario.Normalize(key) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out, nil
}

// planBindsScenarios refuses a scenario the contract's plan does not bind, or names twice.
func planBindsScenarios(p *projectstate.Project, contract string, scenarios []string) error {
	if p.PhaseArtifacts == nil {
		return fmt.Errorf("no test plan is recorded for %s", contract)
	}
	bound := map[string]bool{}
	for _, b := range p.PhaseArtifacts.TestPlan[contract].Bindings {
		bound[b.Scenario] = true
	}
	for _, id := range scenarios {
		if !bound[id] {
			return fmt.Errorf("the plan of %s binds no scenario %q (or it is named twice)", contract, id)
		}
		delete(bound, id)
	}
	return nil
}

// decodePhaseArtifactPayload strictly decodes the agent-supplied payload map into the
// typed PhaseArtifactPayload tagged union.
func decodePhaseArtifactPayload(payload map[string]any) (projectstate.PhaseArtifactPayload, error) {
	b, err := marshalInputModel(payload)
	if err != nil {
		return projectstate.PhaseArtifactPayload{}, err
	}
	// Strict, as recordServiceContract is: RUN-011 and RUN-012 committed integration notes with
	// empty content because the agent's text was under summary or markdown and was dropped. The
	// payload's untagged field names still match case-insensitively (srs, testPlan).
	var pl projectstate.PhaseArtifactPayload
	if err := decodeDraftStrict(b, &pl); err != nil {
		hint := ""
		for k := range payload {
			if strings.EqualFold(k, "testPlan") {
				hint = "; a testPlan's shape is " + testPlanShape
			}
		}
		return projectstate.PhaseArtifactPayload{}, fmt.Errorf("the payload does not conform to PhaseArtifactPayload: %v"+
			" — set exactly one payload field, with only the fields its record declares%s, and try again", err, hint)
	}
	if err := refuseBlankContent(pl); err != nil {
		return projectstate.PhaseArtifactPayload{}, err
	}
	return pl, nil
}

// testPlanShape is a testPlan record's shape, named when one is refused: a strict decode names
// only the stray key, and agents have put hook on the binding and expected values beside result.
const testPlanShape = "{component, bindings:[{scenario, steps:[{seq, operation, inputs:[{name, value}], " +
	"expect:{result, errorExpected, errorCode}, probes:[{for, component, operation, inputs, expect}], unobservable:[nodeId], hook}], skip:{reason, until}}]}"

// refuseBlankContent: a content-bearing record is its markdown text, which the phases after it
// read. One recorded with no content records nothing but a passing write.
func refuseBlankContent(pl projectstate.PhaseArtifactPayload) error {
	type record struct{ field, content string }
	var records []record
	if pl.SRS != nil {
		records = append(records, record{"SRS", pl.SRS.Content})
	}
	if pl.IntegrationNote != nil {
		records = append(records, record{"IntegrationNote", pl.IntegrationNote.Content})
	}
	if pl.UXRequirements != nil {
		records = append(records, record{"UXRequirements", pl.UXRequirements.Content})
	}
	if pl.UIDesign != nil {
		records = append(records, record{"UIDesign", pl.UIDesign.Content})
	}
	if pl.ProvisioningSpec != nil {
		records = append(records, record{"ProvisioningSpec", pl.ProvisioningSpec.Content})
	}
	if pl.DeployNote != nil {
		records = append(records, record{"DeployNote", pl.DeployNote.Content})
	}
	if pl.DocOutline != nil {
		records = append(records, record{"DocOutline", pl.DocOutline.Content})
	}
	if pl.DocNote != nil {
		records = append(records, record{"DocNote", pl.DocNote.Content})
	}
	for _, r := range records {
		if strings.TrimSpace(r.content) == "" {
			return fmt.Errorf("the %s record has no content: the %s record's text goes in \"content\" (a markdown string)", r.field, r.field)
		}
	}
	return nil
}
