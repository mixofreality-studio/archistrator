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
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

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

// writeGateErrors is the ONE whole-document Method gate every state write is held to:
// methodcheck over the encoded document, then the gate severity policies in WHOLE-DOCUMENT
// mode (a construct session works on a component + activity, not a Phase-1/2 artifact slot,
// so there is no ambient slot to scope to — staleness applies, slot-scoping does not;
// earmark: if construction ever meets grandfathered design-slot defects the way the design
// rail did, extending the ambient concept to the construction targets is a founder
// decision), keeping only the Errors. applyConstructionMutation refuses on any of them, and
// a state-recording task's prefix-scoped rule check (runRule) reports the ones it would
// otherwise hide, so the check and the write cannot disagree.
func writeGateErrors(raw []byte, proj projectstate.Project) ([]methodcheck.Finding, error) {
	findings, err := methodcheck.ValidateProjectJSON(raw, methodcheckOptions())
	if err != nil {
		return nil, fmt.Errorf("the Method coherence check failed: %w", err)
	}
	return filterErrorFindings(applyGateSeverityPolicies(proj, 0, false, findings)), nil
}

// applyConstructionMutation is the shared validate-then-write core for the construction
// verbs — the construction analogue of putDraftModel's gates. It reads the checkout,
// applies the caller's typed mutation, then re-encodes and validates the WHOLE aggregate
// through the strict server codec (read-back parity) AND methodcheck (the CI gate),
// writing only if BOTH pass. On any failure NOTHING is written and a typed error is
// returned (surfaced to the agent as a self-correctable IsError result).
func (s *Session) applyConstructionMutation(what string, mutate func(*projectstate.Project) error, admitKinds ...string) error {
	return s.applyConstructionMutationThen(what, mutate, nil, admitKinds...)
}

// applyConstructionMutationThen is applyConstructionMutation with an `after` refusal that runs
// once the Method-rule write gate has passed. A refusal a Method rule owns names its slot and
// routes the agent to that slot's amendment; a refusal the agent can act on itself must not
// shadow it, so it is ordered after the gate.
func (s *Session) applyConstructionMutationThen(what string, mutate func(*projectstate.Project) error, after func(*projectstate.Project) error, admitKinds ...string) error {
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
	errs, ferr := writeGateErrors(newBytes, proj)
	if ferr != nil {
		return ferr
	}
	if len(errs) > 0 {
		s.recordRefusal(what, errs)
		return fmt.Errorf("the %s change violates %d Method rule(s) that the required CI check enforces — fix them and record again:\n%s",
			what, len(errs), formatFindings(errs))
	}
	if after != nil {
		if err := after(&proj); err != nil {
			return err
		}
	}
	// Tier 1: every generator that reads what was just recorded emits it, and the siblings
	// typecheck (admission.go). Last, so the cheaper refusals above come first.
	if len(admitKinds) > 0 {
		if err := s.admitTier1(what, newBytes, admitKinds...); err != nil {
			return err
		}
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
	b, err := marshalInputModel(contract)
	if err != nil {
		return err
	}
	return s.recordServiceContractJSON(b)
}

// recordServiceContractJSON is recordServiceContract over the exact bytes the agent sent. The
// bytes are decoded, never re-marshalled from a map: a $defs schema's property order is the field
// order modelgen emits, so sorting it changes the generated code.
func (s *Session) recordServiceContractJSON(b []byte) error {
	if s.ComponentID == "" {
		return fmt.Errorf("recordServiceContract needs the ambient component (AIARCH_COMPONENT_ID); this construct job has none — it is not a component activity")
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
		if key, err = facetKey(*p, comp, key, sc); err != nil {
			return err
		}
		committed, hadCommitted := p.ServiceContracts[key]
		if err := keepCommittedOwner(key, committed, hadCommitted, &sc); err != nil {
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
		if err := depsFromSystem(*p, comp, committed, &sc); err != nil {
			return err
		}
		if err := canonicalizeAmendment(key, &sc, committed, hadCommitted); err != nil {
			return err
		}
		markReadOnlyOperations(&sc)
		if p.ServiceContracts == nil {
			p.ServiceContracts = make(map[string]projectstate.ServiceContract)
		}
		p.ServiceContracts[key] = sc
		if strings.EqualFold(sc.Layer, "manager") || strings.EqualFold(sc.Interface.Layer, "manager") {
			return projectstate.RecordMachineClientContracts(p)
		}
		return nil
	}, string(appgen.KindServiceContract))
}

// facetKey is the key the contract is filed under. A component may own several contract facets
// (projectStateAccess files projectStateAccess, activityExecutionAccess, designSessionAccess and
// gitActivityStatusAccess), each keyed by lowerCamel(its interface name); a contract whose
// interface names a committed facet of the ambient component amends that facet, not the
// component's primary key. A key a different component owns is refused. Any other name files under
// the component's own key, as before.
func facetKey(p projectstate.Project, c projectstate.Component, key string, sc projectstate.ServiceContract) (string, error) {
	name := sc.Interface.Name
	if name == "" {
		return key, nil
	}
	r, size := utf8.DecodeRuneInString(name)
	facet := string(unicode.ToLower(r)) + name[size:]
	if facet == key {
		return key, nil
	}
	existing, ok := p.ServiceContracts[facet]
	if !ok {
		return key, nil
	}
	if existing.Component != key && existing.Component != c.ID {
		return "", fmt.Errorf("recordServiceContract: interface %q is filed under contract key %q, which belongs to component %q, not to %q; record a contract for this component's own interface", name, facet, existing.Component, c.ID)
	}
	return facet, nil
}

// keepCommittedOwner sets the owner of an amended contract from the committed one. The owner is
// the contract key the component's facets hang off (a facet's component field), which the verb's
// advertised shape does not carry, so an agent amending a facet would otherwise store it with no
// owner and every rule that reads the owner would lose it. A supplied owner that names another
// component is refused rather than replaced.
func keepCommittedOwner(key string, committed projectstate.ServiceContract, hadCommitted bool, sc *projectstate.ServiceContract) error {
	if !hadCommitted || committed.Component == "" {
		return nil
	}
	if sc.Component != "" && sc.Component != committed.Component {
		return fmt.Errorf("recordServiceContract: contract %q belongs to component %q, not %q; the owner is the design's, so leave component out", key, committed.Component, sc.Component)
	}
	sc.Component = committed.Component
	return nil
}

// canonicalizeAmendment is canonicalizeContract for a record that may amend a committed contract.
// ValidateContract has no notion of "already committed", so a committed contract that predates a
// rule (a ResourceAccess with a goPackage and no infra; a param name the generator later
// reserved) would block every amendment of it. ValidateContract also reports only its FIRST
// finding, so no comparison of first findings can tell a new violation from an old one: a new
// one that sorts after an old one is masked. An amendment is therefore judged by its own parts,
// each held to the full ValidateContract rules exactly as a first record is: the probe keeps
// every new or changed operation and $def verbatim, and replaces each operation and $def that is
// byte-identical to the committed one (the part the amendment did not write) with a clean stand-in
// of the same name, so the committed contract's findings in those parts cannot mask or be
// mistaken for the amendment's. An infra-less ResourceAccess whose committed contract is also
// infra-less is probed with a placeholder infra: that hole is the one finding the amendment does
// not own, and the placeholder never reaches the recorded contract. Refused, as strictly as a
// first record: any finding in a changed part (a changed operation keeps its committed params, so
// one that carries a reserved param is refused until it is renamed), a committed name that is
// itself invalid (its stand-in would carry it), a $ref left dangling by a removed $def, and
// dropping every $def. A first record, and an amendment of a committed contract that raised no
// finding, are validated whole.
func canonicalizeAmendment(key string, sc *projectstate.ServiceContract, committed projectstate.ServiceContract, hadCommitted bool) error {
	err := canonicalizeContract(key, sc)
	if err == nil || !hadCommitted {
		return err
	}
	old := committed
	if canonicalizeContract(key, &old) == nil {
		return err
	}
	if len(committed.Defs) > 0 && len(sc.Defs) == 0 {
		return err
	}
	probe, perr := amendmentProbe(*sc, committed)
	if perr != nil {
		return perr
	}
	return canonicalizeContract(key, &probe)
}

// amendmentProbe is the contract canonicalizeAmendment validates in place of an amendment: the
// amendment's own parts verbatim and the committed ones neutralized (see canonicalizeAmendment).
func amendmentProbe(sc, committed projectstate.ServiceContract) (projectstate.ServiceContract, error) {
	probe := sc
	probe.Interface.Operations = make([]projectstate.ContractOperation, len(sc.Interface.Operations))
	probe.Defs = make(map[string]json.RawMessage, len(sc.Defs))
	var kept []any
	for i, op := range sc.Interface.Operations {
		// readOnly is derived after validation (markReadOnlyOperations), never authored.
		same := slices.ContainsFunc(committed.Interface.Operations, func(c projectstate.ContractOperation) bool {
			c.ReadOnly = op.ReadOnly
			return operationsEqual(c, op)
		})
		if !same {
			probe.Interface.Operations[i] = op
			continue
		}
		probe.Interface.Operations[i] = projectstate.ContractOperation{Name: op.Name, Error: true}
		kept = append(kept, op)
	}
	for name, def := range sc.Defs {
		if old, ok := committed.Defs[name]; ok && jsonEqual(old, def) {
			probe.Defs[name] = json.RawMessage(`{"type":"string"}`)
			kept = append(kept, def)
			continue
		}
		probe.Defs[name] = def
	}
	if err := danglingRef(kept, sc.Defs); err != nil {
		return probe, err
	}
	if len(probe.Defs) == 0 && sc.GoPackage != "" {
		// The committed contract had none either: that finding is the committed one's.
		probe.Defs["AmendmentProbe"] = json.RawMessage(`{"type":"string"}`)
	}
	if len(sc.Infra) == 0 && len(committed.Infra) == 0 && strings.EqualFold(sc.Layer, "resourceaccess") {
		probe.Infra = []string{modelgen.ApprovedInfra()[0]}
	}
	return probe, nil
}

// operationsEqual reports whether two operations are the same document, whatever the spacing of
// their raw param and result schemas: a committed operation is decoded from the pretty-printed
// project.json, an amendment arrives compact.
func operationsEqual(a, b projectstate.ContractOperation) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return jsonEqual(x, y)
}

// danglingRef refuses a $ref, in a part of the contract an amendment left alone, that names no
// $def of the amended contract (the amendment removed or renamed it).
func danglingRef(untouched []any, defs map[string]json.RawMessage) error {
	for _, part := range untouched {
		raw, err := json.Marshal(part)
		if err != nil {
			return fmt.Errorf("recordServiceContract: encode the contract: %w", err)
		}
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("recordServiceContract: encode the contract: %w", err)
		}
		for _, ref := range refsIn(doc) {
			if _, ok := defs[ref[strings.LastIndex(ref, "/")+1:]]; !ok {
				return fmt.Errorf("recordServiceContract: $ref %q names no type in this contract's $defs; declare it there or correct the reference — fix the field and call recordServiceContract again", ref)
			}
		}
	}
	return nil
}

// refsIn is every "$ref" string anywhere in a decoded JSON document.
func refsIn(node any) []string {
	var out []string
	switch n := node.(type) {
	case map[string]any:
		if ref, ok := n["$ref"].(string); ok {
			out = append(out, ref)
		}
		for _, v := range n {
			out = append(out, refsIn(v)...)
		}
	case []any:
		for _, v := range n {
			out = append(out, refsIn(v)...)
		}
	}
	return out
}

// jsonEqual reports whether two raw JSON values are the same document, whatever their spacing.
func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// markReadOnlyOperations sets each operation's readOnly flag from its name and layer, the one
// classifier the internal tool catalog's readOnlyHint uses. Nobody authors it: methodcheck holds a
// test probe to a read-only operation (TP-PROBE), so a flag an author could set would be a way to
// probe a write.
func markReadOnlyOperations(sc *projectstate.ServiceContract) {
	layer := sc.Layer
	if layer == "" {
		layer = sc.Interface.Layer
	}
	for i := range sc.Interface.Operations {
		sc.Interface.Operations[i].ReadOnly = projectstate.OperationReadOnly(layer, sc.Interface.Operations[i].Name)
	}
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
	if committed, ok := p.ServiceContracts[key]; ok && sc.GoPackage == "" && committed.GoPackage != "" {
		// An amendment keeps the committed package: a component's facets share one, which the
		// derivation below would call held by the others.
		sc.GoPackage = committed.GoPackage
	}
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
//
// An amendment of a committed Manager contract starts from that contract's deps (committedDeps):
// its constructor is built by hand against them, so their names, their order and the facets of a
// drawn component they take (the System draws one edge to a component, whatever number of contract
// facets it owns) survive a record that did not change the design. A committed component dep that
// no longer follows a drawn edge is dropped; a drawn edge the committed deps do not cover is
// appended as derived.
func depsFromSystem(p projectstate.Project, c projectstate.Component, committed projectstate.ServiceContract, sc *projectstate.ServiceContract) error {
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
	kept := committedDeps(p, committed, want)
	covered := map[string]bool{}
	for _, d := range kept {
		if d.Component != "" {
			covered[drawnOwner(p, d.Component)] = true
		}
	}
	kept, err = suppliedDeps(c.ID, sc.Deps, derived[0], want, drawn, kept)
	if err != nil {
		return err
	}
	deps := append([]projectstate.ContractDep{derived[0]}, kept...)
	for _, d := range derived[1:] {
		if !covered[d.Component] {
			deps = append(deps, d)
		}
	}
	sc.Deps = deps
	return nil
}

// suppliedDeps folds the deps the agent sent into kept: a plain dep the committed contract does not
// already carry by name is appended, the client must be the derived one, and a component dep must be
// a derived dep or one already kept; any other is refused.
func suppliedDeps(managerID string, supplied []projectstate.ContractDep, client projectstate.ContractDep, want map[string]projectstate.ContractDep, drawn []string, kept []projectstate.ContractDep) ([]projectstate.ContractDep, error) {
	byName := map[string]projectstate.ContractDep{}
	for _, d := range kept {
		byName[d.Name] = d
	}
	for _, d := range supplied {
		switch {
		case d.Component == "" && (d.Name == client.Name || (d.GoType == client.GoType && d.GoImport == client.GoImport)):
			if d != client {
				return nil, fmt.Errorf("recordServiceContract: Manager %q's dep %q is the Temporal client, which is %+v; leave it out and record again", managerID, d.Name, client)
			}
		case d.Component == "":
			if _, ok := byName[d.Name]; !ok {
				kept = append(kept, d)
				byName[d.Name] = d
			}
		case want[d.Component] == d, byName[d.Name] == d:
		default:
			return nil, fmt.Errorf("recordServiceContract: Manager %q's dep %+v is not one the System draws: its deps follow the design's edges, one per Engine, ResourceAccess and Utility it calls, named by contract key (%s); leave the component deps out, they are derived, and record again",
				managerID, d, strings.Join(drawn, ", "))
		}
	}
	return kept, nil
}

// committedDeps is the committed Manager contract's deps but the Temporal client: each plain dep,
// and each component dep that still follows a drawn edge (its component is a drawn target, or a
// contract facet that target owns). A project with no committed contract has none.
func committedDeps(p projectstate.Project, committed projectstate.ServiceContract, drawn map[string]projectstate.ContractDep) []projectstate.ContractDep {
	var out []projectstate.ContractDep
	for _, d := range committed.Deps {
		switch {
		case d.Component == "" && d.GoType == projectstate.TemporalClientDep.GoType && d.GoImport == projectstate.TemporalClientDep.GoImport:
		case d.Component == "":
			out = append(out, d)
		default:
			if _, ok := drawn[drawnOwner(p, d.Component)]; ok {
				out = append(out, d)
			}
		}
	}
	return out
}

// drawnOwner is the contract key a dep on contract key k is drawn by: k itself, or, when k is a
// facet another recorded contract's component owns under a different key, that component's key.
func drawnOwner(p projectstate.Project, k string) string {
	sc, ok := p.ServiceContracts[k]
	if !ok || sc.Component == "" || sc.Component == k {
		return k
	}
	if comp, _, err := contractComponent(p, sc.Component); err == nil {
		if key, ok := projectstate.ContractKeyFor(comp); ok {
			return key
		}
	}
	return sc.Component
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
	return s.applyConstructionMutationThen("phase artifact", func(p *projectstate.Project) error {
		key := strings.TrimSpace(mapKey)
		if pl.AcceptancePlan != nil {
			// The one acceptance plan belongs to no component: whatever key the agent sent, it is
			// filed as .phaseArtifacts.acceptancePlan and the key it is spoken of by is acceptance.
			key = projectstate.AcceptanceContract
		}
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
	}, func(p *projectstate.Project) error {
		// After the Method-rule gate: a residual a design slot owns is reported by its rule.
		if pl.AcceptancePlan != nil {
			return projectstate.VacuousAcceptancePlanRefusal(p, *pl.AcceptancePlan)
		}
		return nil
	}, phaseArtifactKinds(pl)...)
}

// phaseArtifactKinds are the artifact kinds a payload records that a generator reads (Tier 1):
// the test plan, the UI design and the acceptance plan. The other phase artifacts (an SRS, an
// integration note) feed no generator.
func phaseArtifactKinds(pl projectstate.PhaseArtifactPayload) []string {
	var kinds []string
	if pl.TestPlan != nil {
		kinds = append(kinds, string(appgen.KindTestPlan))
	}
	if pl.UIDesign != nil {
		kinds = append(kinds, string(appgen.KindUIDesign))
	}
	if pl.AcceptancePlan != nil {
		kinds = append(kinds, string(appgen.KindAcceptancePlan))
	}
	return kinds
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
	components := make(map[string]string, len(p.ServiceContracts))
	for name, sc := range p.ServiceContracts {
		components[name] = sc.Component
	}
	return methodcheck.ContractsOf(key, components), nil
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
			switch {
			case strings.EqualFold(k, "testPlan"):
				hint = "; a testPlan's shape is " + testPlanShape
			case strings.EqualFold(k, "acceptancePlan"):
				hint = "; an acceptancePlan's shape is " + acceptancePlanShape
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
// There is no hook and no unobservable list (TP-HOOK): a record is data, and the platform writes the test.
const testPlanShape = "{component, principals:{<name>:{subject, roles}}, bindings:[{scenario, arrange:[{kind, model}], " +
	"steps:[{seq, operation, inputs:[{name, value:{kind:literal|ref, model}}], expect:{result, errorExpected, errorCode}, " +
	"probes:[{for, component, operation, inputs, expect}], before}], skip:{reason, until}}]}"

// acceptancePlanShape is the acceptance plan record's shape, named when one is refused. It is a
// testPlan without the component (it is one per project) and with the browser actor, and a step's
// input may be a planRef to the literal a client plan already binds, a probe may run as another actor.
const acceptancePlanShape = "{principals:{<name>:{subject, roles, onBehalfOf}}, browserActor, bindings:[{scenario:\"<scenario>@<client>\", arrange:[{kind, model}], " +
	"steps:[{seq, operation, inputs:[{name, value:{kind:literal|ref|planRef, model}}], expect:{result, errorExpected, errorCode}, " +
	"probes:[{for, component, operation, inputs, expect, as}], before}], skip:{reason, until}}]}; a planRef's model is {component, scenario, seq, input}"

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
		if len(pl.UIDesign.Screens) == 0 {
			return fmt.Errorf("the UIDesign record has no screens: a UI design is the typed screens and elements a user sees (screens:[{id, route, elements:[{id, role, name, within, repeats}]}]), which the UI test steps target by element id; prose alone in \"content\" gives them nothing to target")
		}
		for _, sc := range pl.UIDesign.Screens {
			if len(sc.Elements) == 0 {
				return fmt.Errorf("the UIDesign screen %q has no elements: a screen is the elements a user sees on its route (elements:[{id, role, name, within, repeats}]), which the UI test steps target by element id", sc.ID)
			}
		}
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
