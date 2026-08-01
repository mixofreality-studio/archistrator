// Package designhealth is the app-side LIVE design-health rule layer: a pure
// function over the committed project.json that mints platform-typed
// methodcheck.Finding values for the mechanical Method rules that are cheap
// enough to run render-on-read (never committed). It is the Wave-2 reshape-3
// "live tier" (wave2-technical-design.md §3e).
//
// It follows the same posture as framework-go/methodcheck itself: the rules run
// over LIGHTWEIGHT STRUCTURAL data mirroring the committed JSON shape, NOT the
// server's typed projectstate models — so this package imports only the two
// published platform modules (framework-go/methodcheck for the Finding type and
// framework-go-projectmodel for the tolerant System/Contracts codegen slice) plus
// stdlib. That keeps it decoupled from the in-flight resourceaccess/manager
// packages and independently buildable and testable.
//
// LAYER: this is a Method ENGINE (internal/engine/designhealth), reclassified
// from Utility by founder ruling R-F (2026-08-01). The deciding argument: its call
// carries a BUSINESS VERDICT in a use-case call chain — uc1's ci-check decision
// node ("Draft valid?") is decided by this component, so it is an evaluation
// Strategy, not ambient infrastructure. A cappuccino machine does not adjudicate
// your design.
//
// It encapsulates the EVALUATION facet of the System Design Phase Workflow
// volatility (B-02, B-03): WHICH Method rules judge a draft and HOW findings are
// computed. That rule inventory turns over release by release with the Method
// doctrine while SystemDesignManager's gate choreography stays put — the two are
// shared typed owners of one volatility, split along the workflow/evaluation seam.
//
// Engine-legal by construction: pure computation over the typed project state the
// Manager passes in (no I/O, no Temporal), zero imports of any
// manager/engine/resourceaccess/client package, and a downward-only M→E edge from
// SystemDesignManager. The authoring gate and the CI harness reach the same
// EvaluateRaw entry point from the composition root, which sits outside the layer
// scan.
//
// The design (wave2-technical-design.md §3e) specifies ONE implementation with
// THREE call sites: getDesignHealth render-on-read (webApp), the putDraftModel
// authoring gate (the existing methodcheck seam in cmd/aiarch-state-mcp), and CI.
// EvaluateRaw is that one implementation; the seam appends its findings alongside
// the framework findings inside applyGateSeverityPolicies (see staleness.go).
//
// The rules that mint SeverityError findings are calibrated to pass ("green") on
// the committed archistrator project.json; App-C-guideline and cardinality bounds
// mint SeverityWarning/SeverityInfo so they surface in the Design Health view
// without blocking an authoring gate. Rules whose inputs are not yet in the state
// (behavior-id renumber, waiver/attestation host fields, per-finding basisRevision
// provenance, a pub/sub bus) are deferred — see the package README note in the
// task report, not encoded here.
package designhealth

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// ==========================================================================
// designhealth — folded here by the Engine file-layout standard (one handwritten
// impl file per Engine component). Contents verbatim; imports merged above.
// ==========================================================================

// Rule identifiers. All live-tier rules share the DH- namespace so the gate's
// slot-scoped severity policy (staleness.go ruleSlotAttributionPrefixes) can
// attribute the whole family in one entry, and so a reader can tell a live-tier
// finding from a framework one at a glance.
const (
	// Cardinality (App-C counts over the component inventory, plus the ch. 4
	// "smallest set" per-layer bands — advisory: the dogfood system legitimately
	// exceeds some, and the rules firing honestly on it is the point).
	RuleCardManagers    methodcheck.RuleID = "DH-CARD-MANAGERS"
	RuleCardManagersMin methodcheck.RuleID = "DH-CARD-MANAGERS-MIN"
	RuleCardEngines     methodcheck.RuleID = "DH-CARD-ENGINES"
	RuleCardRA          methodcheck.RuleID = "DH-CARD-RA"
	RuleCardResource    methodcheck.RuleID = "DH-CARD-RESOURCE"
	RuleCardRAResources methodcheck.RuleID = "DH-CARD-RA-RESOURCES"
	RuleCardUtilities   methodcheck.RuleID = "DH-CARD-UTILITIES"
	RuleCardCoreUC      methodcheck.RuleID = "DH-CARD-COREUC"
	RuleCardVolatility  methodcheck.RuleID = "DH-CARD-VOLATILITY"

	// Graph (closed-architecture directives + guidelines over the relationships).
	RuleGraphUpcall       methodcheck.RuleID = "DH-GRAPH-UPCALL"
	RuleGraphSidewaysSync methodcheck.RuleID = "DH-GRAPH-SIDEWAYS-SYNC"
	RuleGraphClientEntry  methodcheck.RuleID = "DH-GRAPH-CLIENT-ENTRY"
	RuleGraphQueuedTarget methodcheck.RuleID = "DH-GRAPH-QUEUED-TARGET"
	RuleGraphEngineIO     methodcheck.RuleID = "DH-GRAPH-ENGINE-IO"
	RuleGraphUtilReach    methodcheck.RuleID = "DH-GRAPH-UTIL-REACHABLE"
	RuleGraphMgrEmpty     methodcheck.RuleID = "DH-GRAPH-MANAGER-EMPTY"

	// Chains (per-use-case dynamic-view §6 don'ts).
	RuleChainEntryManager  methodcheck.RuleID = "DH-CHAIN-ENTRY-MANAGER"
	RuleChainQueuedManager methodcheck.RuleID = "DH-CHAIN-QUEUED-MANAGER"

	// Coverage / joins.
	RuleCovUCDynamic     methodcheck.RuleID = "DH-COV-UC-DYNAMIC"
	RuleUCEssenceMissing methodcheck.RuleID = "DH-UC-ESSENCE-MISSING"
	RuleVolEncapMissing  methodcheck.RuleID = "DH-VOL-ENCAP-MISSING"
	RuleVolEncapAmbig    methodcheck.RuleID = "DH-VOL-ENCAP-AMBIGUOUS"
	RuleVolTrace         methodcheck.RuleID = "DH-VOL-TRACE"
	RuleCompVolDangling  methodcheck.RuleID = "DH-COMP-VOL-DANGLING"
	RuleCompNoVolatility methodcheck.RuleID = "DH-COMP-NO-VOLATILITY"
	RuleObjResolve       methodcheck.RuleID = "DH-OBJ-RESOLVE"
	RuleObjCoverage      methodcheck.RuleID = "DH-OBJ-COVERAGE"

	// Contracts.
	RuleContractOpReject methodcheck.RuleID = "DH-CONTRACT-OPCOUNT-REJECT"
	RuleContractOpMax    methodcheck.RuleID = "DH-CONTRACT-OPCOUNT-MAX"
	RuleContractFacet    methodcheck.RuleID = "DH-CONTRACT-FACET"
	RuleContractDeadOp   methodcheck.RuleID = "DH-CONTRACT-DEADOP"

	// CC-* call-chain correspondence family (2026-07-30 callchain-realization): the
	// app-side live-tier mirror of framework-go/methodcheck's rules_callchain.go,
	// over this package's own tolerant slot-4/slot-5 slices (see rules_callchain.go
	// for the full family doctrine). Unlike the DH- namespace, these ids are NOT
	// package-prefixed — they are the SAME rule-id strings the platform framework
	// emits, because the webApp's Design Health surface joins on rule id across
	// both call sites (the authoring-gate seam and this live tier) and a finding
	// must read identically wherever it renders.
	RuleCCViewUseCase   methodcheck.RuleID = "CC-VIEW-USECASE"
	RuleCCStepNode      methodcheck.RuleID = "CC-STEP-NODE"
	RuleCCStepUnique    methodcheck.RuleID = "CC-STEP-UNIQUE"
	RuleCCCoverage      methodcheck.RuleID = "CC-COVERAGE"
	RuleCCStepNonempty  methodcheck.RuleID = "CC-STEP-NONEMPTY"
	RuleCCEndpoint      methodcheck.RuleID = "CC-ENDPOINT-RESOLVES"
	RuleCCActorEdge     methodcheck.RuleID = "CC-ACTOR-EDGE"
	RuleCCActorLane     methodcheck.RuleID = "CC-ACTOR-LANE"
	RuleCCDecidedBy     methodcheck.RuleID = "CC-DECIDED-BY"
	RuleCCTriggerEvent  methodcheck.RuleID = "CC-TRIGGER-EVENT"
	RuleCCPathConnected methodcheck.RuleID = "CC-PATH-CONNECTED"

	// RuleCUCActorRequired mirrors the platform's CoreUseCases-attributed
	// CUC-ACTOR-REQUIRED (rollout rulings 2026-07-31): a clientAction use case
	// must name who initiates it. It ships alongside CC-DECIDED-BY as the
	// rollout's other new rule, and lives in rules_callchain.go too, but unlike
	// the CC-* family above it is NOT dynamic-view-scoped — it fires for every
	// committed use case regardless of whether a dynamic view exists for it
	// yet. It is unnamespaced for the same reason the CC-* ids are: this is
	// the SAME rule-id string the platform framework emits, and the webApp
	// joins on rule id across both call sites. Unlike the platform (whose
	// registry can attribute a rule to the CoreUseCases artifact family),
	// designhealth findings carry no separate artifact-family attribution, so
	// this is just the rule id + section, like its CC-* siblings.
	RuleCUCActorRequired methodcheck.RuleID = "CUC-ACTOR-REQUIRED"
)

// Input is the decoded, rule-ready view of one project.json: the published
// projectmodel codegen slice (System + Contracts) plus the tolerant slot subset
// projectmodel deliberately does not parse (dynamic views, volatilities,
// objectives, requirements, core use cases). Rules are pure functions of an
// Input, which makes both the green fixture and the per-rule negative fixtures
// trivial to construct.
type Input struct {
	Model *projectmodel.Model
	Slots slotData
}

// Evaluate runs every live-tier rule over in and returns the findings in a
// stable family order (cardinality, graph, chains, coverage, call-chain,
// contracts). It never returns an error: a rule with nothing to say returns no
// findings.
func Evaluate(in Input) []methodcheck.Finding {
	var out []methodcheck.Finding
	out = append(out, cardinalityFindings(in)...)
	out = append(out, graphFindings(in)...)
	out = append(out, chainFindings(in)...)
	out = append(out, coverageFindings(in)...)
	out = append(out, callChainFindings(in)...)
	out = append(out, contractFindings(in)...)
	return out
}

// DesignHealthEngine is this component's Engine port — the one operation a
// Manager calls: judge a project's design against the live Method rule set and
// return the findings. It is the code that BACKS the modeled
// SystemDesignManager → DesignHealthEngine relationship
// ("EvaluateDesignHealth(project, systemModel) → findings"), an ordinary downward
// M→E call on the Strategy plane.
//
// Hand-written rather than generated: the component carries no service contract
// (contractKey null) because nothing outside the Manager consumes it and its
// argument is the raw project.json byte stream, not a schema-first data contract.
//
// The error result is part of the Engine calling convention (every Engine op
// returns one) and is ALWAYS nil today: evaluation is pure computation with no
// I/O and no failure mode of its own. A document that will not parse is a
// STRUCTURAL failure owned by the framework gate, which runs on the same bytes at
// every seam — this engine stays silent rather than double-reporting it, and
// returns no findings.
type DesignHealthEngine interface {
	EvaluateDesignHealth(rc fweng.Context, raw []byte) ([]methodcheck.Finding, error)
}

// designHealthEngine is the concrete engine. Engines are pure, so it carries no
// fields; it delegates to the package's own EvaluateRaw, which the composition
// root's authoring-gate seam also drives directly (cmd is outside the layer scan).
type designHealthEngine struct{}

// NewDesignHealthEngine returns the production DesignHealthEngine.
func NewDesignHealthEngine() DesignHealthEngine { return designHealthEngine{} }

var _ DesignHealthEngine = designHealthEngine{}

// EvaluateDesignHealth implements the port over EvaluateRaw. The call Context is
// accepted for calling-convention uniformity and carries nothing this pure
// computation needs.
func (designHealthEngine) EvaluateDesignHealth(_ fweng.Context, raw []byte) ([]methodcheck.Finding, error) {
	return EvaluateRaw(raw), nil
}

// EvaluateRaw is the single evaluation entry point every call site funnels
// through (the port above, and the composition root's authoring-gate + CI seams).
// It parses raw (the committed or drafted project.json bytes) into an Input and
// evaluates it. A document that will not parse under the published projectmodel
// codegen parser yields no findings — see the port's doc comment.
func EvaluateRaw(raw []byte) []methodcheck.Finding {
	model, err := projectmodel.Load(raw)
	if err != nil {
		return nil
	}
	slots, err := parseSlots(raw)
	if err != nil {
		return nil
	}
	return Evaluate(Input{Model: model, Slots: slots})
}

// componentIndex indexes the System components by id for the graph/chain joins.
func (in Input) componentIndex() map[string]projectmodel.SystemComponent {
	idx := map[string]projectmodel.SystemComponent{}
	if in.Model == nil || in.Model.System == nil {
		return idx
	}
	for _, c := range in.Model.System.Components {
		idx[c.ID] = c
	}
	return idx
}

// finding is a small constructor keeping the rule bodies terse.
func finding(id methodcheck.RuleID, sev methodcheck.Severity, ordinal int, section, msg string) methodcheck.Finding {
	return methodcheck.Finding{
		RuleID:   id,
		Severity: sev,
		Message:  msg,
		Location: &methodcheck.Location{Ordinal: ordinal, Section: section},
	}
}

// ==========================================================================
// layers — folded here by the Engine file-layout standard (one handwritten
// impl file per Engine component). Contents verbatim; imports merged above.
// ==========================================================================

// layers.go holds the Method layer ranking the graph and chain rules share.
// Ranks encode the closed layered architecture: a call is legal DOWN the ranks
// (Client → Manager → Engine → ResourceAccess → Resource), sideways only when
// queued, and never up. Utilities are cross-cutting — any layer may call them and
// they carry no lines in the static view — so they are exempt from the direction
// rules (rankUtility is a sentinel the rules test for explicitly).
const (
	rankClient         = 0
	rankManager        = 1
	rankEngine         = 2
	rankResourceAccess = 3
	rankResource       = 4
	rankUtility        = -1
	rankUnknown        = -2
)

// kindRank maps a component kind (projectmodel SystemComponent.Kind, the kebab/
// camel wire kind) to its layer rank.
func kindRank(kind string) int {
	switch kind {
	case "client":
		return rankClient
	case "manager":
		return rankManager
	case "engine":
		return rankEngine
	case "resourceAccess":
		return rankResourceAccess
	case "resource":
		return rankResource
	case "utility":
		return rankUtility
	default:
		return rankUnknown
	}
}

// isDirectional reports whether both endpoints participate in the directional
// layering (i.e. neither is a cross-cutting utility and both are known kinds).
func isDirectional(fromKind, toKind string) bool {
	fr, tr := kindRank(fromKind), kindRank(toKind)
	return fr >= rankClient && tr >= rankClient
}

// ==========================================================================
// parse — folded here by the Engine file-layout standard (one handwritten
// impl file per Engine component). Contents verbatim; imports merged above.
// ==========================================================================

// parse.go decodes the tolerant slot subset the rule layer needs beyond what the
// published projectmodel parses (projectmodel covers System + Contracts only:
// "dynamicViews and all other fields are deliberately not parsed"). Every struct
// here mirrors the committed JSON shape and ignores unknown fields, exactly like
// projectmodel — a shape addition upstream never breaks this parse.
//
// Slots are located by their numeric `kind`, the stable wire identity that
// wave2-technical-design.md §0.4 keeps fixed across the reshapes (labels rename,
// kinds never). The kind→artifact mapping used here:
//
//	0 businessAlignment (vision/objectives/mission)   3 volatilities
//	2 requirements (Required Behaviors)               4 core use cases
//	5 systemDesign (components/relationships/          6 operational concepts
//	  dynamicViews)                                       (objectiveLinks; legacy
//	                                                      decisions w/ justifyingObjective)
const (
	kindBusinessAlignment   = 0
	kindRequirements        = 2
	kindVolatilities        = 3
	kindCoreUseCases        = 4
	kindSystemDesign        = 5
	kindOperationalConcepts = 6
)

// slotData is the decoded slot subset. Absent slots decode to zero values; a rule
// that joins against an absent counterpart simply finds nothing to check (the same
// posture as the framework's cross-artifact rules).
type slotData struct {
	Objectives   []objective
	Requirements []requirement
	Volatilities []volatility
	CoreUseCases []coreUseCase
	DynamicViews []dynamicView
	// SystemRevision is the systemDesign slot's current revision counter, kept for
	// the deferred slot-revision drift rule (see task report); read now so the
	// wiring is in place when per-finding basisRevision provenance lands.
	SystemRevision int
	// justifyingObjectives is the flat list of objective numbers referenced by
	// LEGACY operational-concepts decisions[].justifyingObjective — the reshape
	// removed decisions[], but older states still carry it, so the harvest is kept.
	justifyingObjectives []int
	// objectiveLinkRefs is the flattened post-reshape objective join: the
	// DeploymentOperationsModel.objectiveLinks map (knob name, e.g.
	// "deploymentScenario" → objective numbers the knob serves), one entry per
	// referenced number, ordered by knob name for deterministic findings. Each
	// entry keeps its source knob so a dangling reference can name it.
	objectiveLinkRefs []objectiveLinkRef
	// encapsulatesBlurbs maps a component id to its systemDesign `encapsulates`
	// prose. projectmodel's SystemComponent does not carry the blurb, so the interim
	// name-in-blurb volatility-encapsulation join reads it from here.
	encapsulatesBlurbs map[string]string
	// encapsulatesVolatilities maps a component id to its typed
	// encapsulatesVolatilities list — the volatility↔component join authored on the
	// systemDesign component side (each entry is the EXACT name of a committed
	// volatility; the edge lives on the component because volatilities commit before
	// the architecture exists). Read from the raw slot because the published
	// projectmodel SystemComponent cannot grow fields without a platform release —
	// the same posture as encapsulatesBlurbs. Only non-empty lists are stored, so
	// len(encapsulatesVolatilities) > 0 means the typed join is authored somewhere.
	encapsulatesVolatilities map[string][]string
}

type objective struct {
	Number    int    `json:"number"`
	Statement string `json:"statement"`
}

type requirement struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
}

type volatility struct {
	Name   string   `json:"name"`
	Traces []string `json:"traces"`
}

type coreUseCase struct {
	ID             string
	Classification string
	// EssenceRationale is the decision-level essence-of-the-business argument for a
	// core classification (the symmetric twin of the nonCore rejectionReason). Nil
	// when the wire field is absent or null — the DH-UC-ESSENCE-MISSING subject.
	EssenceRationale *string
	// Trigger, Actors, and Activity are the CC-* call-chain family's slot-4 join
	// surface (2026-07-30 call-chain realization): the use case's trigger kind, its
	// declared actor roster, and its activity diagram — the Grammar B half of the
	// correspondence CC-* checks against a use case's step-keyed DynamicView
	// (Grammar A). A committed use case predating these fields decodes them to
	// their zero values (empty trigger, nil actors, nil activity), which the CC
	// rules treat as "nothing to check" exactly like an absent dynamic view.
	Trigger  string
	Actors   []ucActorRef
	Activity *activityDiagram
}

// ucActorRef is a use case's actor participant — just the id the CC-* endpoint/
// edge rules resolve calls against (the use-case-scoped actor namespace).
type ucActorRef struct {
	ID string `json:"id"`
}

// activityDiagram mirrors a use case's Grammar B activity diagram: the nodes and
// edges CC-* walks (activitypaths.go) to check the DynamicView call chain against
// it.
type activityDiagram struct {
	Nodes []activityNode `json:"nodes"`
	Edges []activityEdge `json:"edges"`
}

// activityNode is one activity-diagram node. RoleName/LinkedActorID are populated
// only for a swim-lane node (CC-ACTOR-LANE's join key); LinkedActorID mirrors the
// app's nullable *string as a plain string ("" when null/absent) — JSON null into
// a non-pointer string field is a no-op for encoding/json, leaving the zero value.
type activityNode struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Label         string `json:"label"`
	RoleName      string `json:"roleName"`
	LinkedActorID string `json:"linkedActorId"`

	// DecidedBy names WHO resolves this node's branch (rollout rulings
	// 2026-07-31). It is legal ONLY on a decision/switch kind, and it resolves
	// exactly like a call endpoint: against the System's components UNION the
	// owning use case's actors. Empty means absent (the shape every
	// pre-rulings committed node has) — the same zero-value mirroring as
	// LinkedActorID above. CC-DECIDED-BY (rules_callchain.go) checks both
	// halves; nothing here enforces them.
	DecidedBy string `json:"decidedBy"`
}

// activityEdge is one directed activity-diagram edge.
type activityEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Guard string `json:"guard"`
}

// coreUseCaseSlot mirrors the core-use-cases slot model: decisions[].useCase plus
// the decision-level essenceRationale sibling.
type coreUseCaseSlot struct {
	Decisions []struct {
		UseCase struct {
			ID             string           `json:"id"`
			Classification string           `json:"classification"`
			Trigger        string           `json:"trigger"`
			Actors         []ucActorRef     `json:"actors"`
			Activity       *activityDiagram `json:"activity"`
		} `json:"useCase"`
		EssenceRationale *string `json:"essenceRationale"`
	} `json:"decisions"`
}

// dynamicView mirrors the step-keyed DynamicView wire shape (2026-07-30
// call-chain realization reshape): one callStep per realized activity node,
// each naming the calls dispatched at that node. There is no separate
// participants list — participants are derived from the steps' call
// endpoints (see chainFindings). A committed view still on the OLD
// participants/edges shape decodes here as zero steps (tolerant decode:
// unknown JSON keys are ignored), which the chain rules treat as nothing to
// check, not an error.
type dynamicView struct {
	UseCaseID string     `json:"useCaseId"`
	Key       string     `json:"key"`
	Steps     []callStep `json:"steps"`
}

type callStep struct {
	ActivityNodeID string     `json:"activityNodeId"`
	Calls          []viewEdge `json:"calls"`
}

type viewEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Mode  string `json:"mode"`
	Label string `json:"label"`

	// Alt tags a surface-alternative group (rollout rulings 2026-07-31): calls
	// within one step sharing an Alt value are equivalent entries into the
	// same chain (e.g. the web and MCP surfaces of one use case), not a
	// sequence. Alt is decoded here for completeness but is INERT for every
	// CC-* verdict — see TestCCPathConnectedAltGroupBothSeedReached, the
	// designhealth twin of the platform's alt-both-seed-reached pin — because
	// no CC-* rule branches on it; every alternative is still checked (and
	// still seeds CC-PATH-CONNECTED's reached set) exactly like an ordinary
	// call. The per-view §6a/§6b chain rules (rules_chains.go) do NOT special-
	// case it either, which is deliberate: they read a view's calls as
	// concurrent, so an alt group entering two different Managers still fires
	// DH-CHAIN-ENTRY-MANAGER — alternatives are two doors into ONE chain (same
	// Manager), not two chains. Empty means absent — the same zero-value
	// mirroring as the other nullable fields in this file.
	Alt string `json:"alt"`
}

// opConceptsSlot mirrors the operational-concepts slot's two objective-reference
// surfaces: the typed post-reshape objectiveLinks map (knob → objective numbers)
// and the legacy decisions[].justifyingObjective back-reference — both feed the
// objective-coverage rules.
type opConceptsSlot struct {
	ObjectiveLinks map[string][]int64 `json:"objectiveLinks"`
	Decisions      []struct {
		JustifyingObjective int `json:"justifyingObjective"`
	} `json:"decisions"`
}

// objectiveLinkRef is one flattened objectiveLinks reference: the operational
// knob and the objective number it cites.
type objectiveLinkRef struct {
	Knob   string
	Number int
}

// rawProject is the tolerant top-level slice: the slots map keyed by slot number,
// each carrying its kind and raw model. Mirrors projectmodel.projectDoc.
type rawProject struct {
	Slots map[string]struct {
		Kind      int             `json:"kind"`
		Model     json.RawMessage `json:"model"`
		Revisions json.RawMessage `json:"revisions"`
	} `json:"slots"`
}

// parseSlots decodes the slot subset the rules need. It is tolerant: a missing or
// empty slot yields an empty section rather than an error. It returns an error
// only when the top-level document itself is not JSON (which EvaluateRaw treats as
// a structural failure the framework gate owns).
func parseSlots(raw []byte) (slotData, error) {
	var top rawProject
	if err := json.Unmarshal(raw, &top); err != nil {
		return slotData{}, fmt.Errorf("designhealth: parse slots: %w", err)
	}
	var out slotData

	for _, slot := range top.Slots {
		if len(slot.Model) == 0 {
			continue
		}
		switch slot.Kind {
		case kindBusinessAlignment:
			var m struct {
				Objectives []objective `json:"objectives"`
			}
			_ = json.Unmarshal(slot.Model, &m)
			out.Objectives = append(out.Objectives, m.Objectives...)
		case kindRequirements:
			var m struct {
				Items []requirement `json:"items"`
			}
			_ = json.Unmarshal(slot.Model, &m)
			out.Requirements = append(out.Requirements, m.Items...)
		case kindVolatilities:
			var m struct {
				Items []volatility `json:"items"`
			}
			_ = json.Unmarshal(slot.Model, &m)
			out.Volatilities = append(out.Volatilities, m.Items...)
		case kindCoreUseCases:
			var m coreUseCaseSlot
			_ = json.Unmarshal(slot.Model, &m)
			for _, d := range m.Decisions {
				out.CoreUseCases = append(out.CoreUseCases, coreUseCase{
					ID:               d.UseCase.ID,
					Classification:   d.UseCase.Classification,
					EssenceRationale: d.EssenceRationale,
					Trigger:          d.UseCase.Trigger,
					Actors:           d.UseCase.Actors,
					Activity:         d.UseCase.Activity,
				})
			}
		case kindSystemDesign:
			var m struct {
				Components []struct {
					ID                       string   `json:"id"`
					Encapsulates             string   `json:"encapsulates"`
					EncapsulatesVolatilities []string `json:"encapsulatesVolatilities"`
				} `json:"components"`
				DynamicViews []dynamicView `json:"dynamicViews"`
			}
			_ = json.Unmarshal(slot.Model, &m)
			out.DynamicViews = append(out.DynamicViews, m.DynamicViews...)
			if out.encapsulatesBlurbs == nil {
				out.encapsulatesBlurbs = map[string]string{}
			}
			if out.encapsulatesVolatilities == nil {
				out.encapsulatesVolatilities = map[string][]string{}
			}
			for _, c := range m.Components {
				if c.Encapsulates != "" {
					out.encapsulatesBlurbs[c.ID] = c.Encapsulates
				}
				if len(c.EncapsulatesVolatilities) > 0 {
					out.encapsulatesVolatilities[c.ID] = c.EncapsulatesVolatilities
				}
			}
			// revisions is a scalar counter; tolerate absence.
			var rev int
			if len(slot.Revisions) > 0 {
				_ = json.Unmarshal(slot.Revisions, &rev)
			}
			out.SystemRevision = rev
		case kindOperationalConcepts:
			var m opConceptsSlot
			_ = json.Unmarshal(slot.Model, &m)
			for _, d := range m.Decisions {
				if d.JustifyingObjective != 0 {
					out.justifyingObjectives = append(out.justifyingObjectives, d.JustifyingObjective)
				}
			}
			knobs := make([]string, 0, len(m.ObjectiveLinks))
			for knob := range m.ObjectiveLinks {
				knobs = append(knobs, knob)
			}
			sort.Strings(knobs)
			for _, knob := range knobs {
				for _, n := range m.ObjectiveLinks[knob] {
					out.objectiveLinkRefs = append(out.objectiveLinkRefs, objectiveLinkRef{Knob: knob, Number: int(n)})
				}
			}
		}
	}
	return out, nil
}

// requirementIDs returns the set of requirement ids for the trace-resolution join.
func (s slotData) requirementIDs() map[string]bool {
	ids := make(map[string]bool, len(s.Requirements))
	for _, r := range s.Requirements {
		if r.ID != "" {
			ids[r.ID] = true
		}
	}
	return ids
}

// objectiveNumbers returns the set of declared objective numbers.
func (s slotData) objectiveNumbers() map[int]bool {
	nums := make(map[int]bool, len(s.Objectives))
	for _, o := range s.Objectives {
		nums[o.Number] = true
	}
	return nums
}

// volatilityNames returns the set of committed volatility names — the join domain of
// the typed component↔volatility rules (exact-name membership).
func (s slotData) volatilityNames() map[string]bool {
	names := make(map[string]bool, len(s.Volatilities))
	for _, v := range s.Volatilities {
		if v.Name != "" {
			names[v.Name] = true
		}
	}
	return names
}

// typedEncapsulationActive reports whether ANY component carries a non-empty typed
// encapsulatesVolatilities list — the switch that makes the typed join authoritative
// for the whole system (parseSlots stores only non-empty lists).
func (s slotData) typedEncapsulationActive() bool {
	return len(s.encapsulatesVolatilities) > 0
}

// ==========================================================================
// rules_callchain — folded here by the Engine file-layout standard (one handwritten
// impl file per Engine component). Contents verbatim; imports merged above.
// ==========================================================================

// rules_callchain.go is the app-side LIVE-TIER mirror of the platform's CC-*
// CALL-CHAIN CORRESPONDENCE family (framework-go/methodcheck/rules_callchain.go
// + activitypaths.go, 2026-07-30 callchain-realization, extended 2026-07-31 by
// the rollout rulings with decidedBy resolution and a work-bounded walker): the
// machine check that every use case's step-keyed DynamicView realization
// CORRESPONDS to that use case's activity diagram. This is the tier the
// webApp's Design Health surface actually renders (render-on-read over the
// committed project.json — see the package doc comment in designhealth.go), so
// the eleven rules below are re-derived over this package's own tolerant slices
// (dynamicView/callStep/coreUseCase) — the same posture as the rest of this
// package relative to framework-go/methodcheck (structural mirror, no shared
// types).
//
// The eleven rules (nine correspondence checks, the dangling-join-key guard,
// and the decider-attribution check):
//
//	CC-VIEW-USECASE    a view's useCaseId must resolve to a use case in slot 4
//	CC-STEP-NODE       every step keys a node the diagram declares
//	CC-STEP-UNIQUE     at most one step per activity node
//	CC-COVERAGE        every step-REQUIRING node is realized, and every step keys a
//	                   step-ELIGIBLE node
//	CC-STEP-NONEMPTY   a realized step makes at least one call
//	CC-ENDPOINT-RESOLVES  every call endpoint resolves to exactly one of {Component, Actor}
//	CC-ACTOR-EDGE      an actor may only interact, synchronously, with a Client
//	CC-ACTOR-LANE      a lane-linked node's step must touch that actor
//	CC-DECIDED-BY      a node's decider sits on a branching node and resolves
//	CC-TRIGGER-EVENT   the use-case trigger and the diagram's entry nodes agree
//	CC-PATH-CONNECTED  every activity-diagram PATH is realized as a connected chain
//
// A twelfth rule, CUC-ACTOR-REQUIRED, lives in this file too (see
// actorRequiredFindings below) even though it is CoreUseCases-attributed rather
// than per-dynamic-view: the rollout rulings shipped it alongside CC-DECIDED-BY
// as the pass's two new rules, and designhealth has no separate
// artifact-family split the way the platform's rules.go/rules_callchain.go do.
//
// SEVERITY: the whole family is advisory in this PoC (ccLiveSeverity =
// SeverityWarning below) — the post-QA rollout flips it to Error, mirroring the
// platform's ccGateSeverity.
//
// SECTION GRAMMAR (binding — the webApp's Design Health surface joins its
// per-step finding badges on these exact strings, NOT on a title-first label):
// step-scoped findings use "dynamicView " + <view key, falling back to
// useCaseId only when the key is empty> + " step " + <activity node id>;
// use-case-scoped findings use "useCase " + <use case id>. See ccViewLabel and
// ccContext.stepLoc/ucLoc below. Both tiers now share this key-first grammar
// (rollout rulings 2026-07-31 brought the platform in line — see its
// ccKeyLabel), so the comparison this comment used to draw against the
// platform's title-first section grammar no longer applies; what's still true,
// and worth keeping in mind, is that this remains the OPPOSITE priority of
// dvLabel (useCaseId-first, used by the pre-existing DH-CHAIN-* rules in
// rules_chains.go) — the key, not the title or the use-case id, is the stable
// identity the app's join relies on. MESSAGE TEXT is a different matter: this
// tier has always led with the key there too (ccViewLabel), while the platform
// keeps its title-first viewLabel in message text — that divergence is
// intentional (only Section is the cross-tier join key) and is not "corrected"
// by this port.
//
// ACTORS: an endpoint id is resolved against the component index UNION the
// OWNING use case's Actors. Actors are per-use-case, so the same id may name an
// actor in one use case and nothing in another — resolution is always relative
// to the view's use case. ActivityNode.DecidedBy resolves in exactly the same
// two namespaces.
const ccLiveSeverity = methodcheck.SeverityWarning

// ccMustHaveStep is the set of activity-node kinds that MUST carry a realizing
// step: they are the nodes that DO something, so a call chain has to say what
// calls they make.
var ccMustHaveStep = map[string]bool{
	"action":      true,
	"timeEvent":   true,
	"acceptEvent": true,
}

// ccMayHaveStep is the set of node kinds a step is ALLOWED but not required to
// key: a decision/switch may itself make a call (asking an Engine for the
// verdict it branches on) or may branch purely on already-held state. Every
// other kind (start/end/merge/join/fork/swimLane/note/loop/goto/interruptEdge)
// is pure control flow and must NOT carry a step.
var ccMayHaveStep = map[string]bool{
	"decision": true,
	"switch":   true,
}

// callChainFindings validates every dynamic view's realization against its
// owning use case's activity diagram: the dangling-join-key guard, coverage,
// endpoint resolution, actor legality, trigger alignment, and per-path chain
// connectivity.
func callChainFindings(in Input) []methodcheck.Finding {
	idx := in.componentIndex()
	ucByID := useCaseIndex(in.Slots.CoreUseCases)
	var out []methodcheck.Finding
	out = append(out, actorRequiredFindings(in.Slots.CoreUseCases)...)
	for i, dv := range in.Slots.DynamicViews {
		uc, ok := ucByID[dv.UseCaseID]
		if !ok {
			// CC-VIEW-USECASE. A view whose UseCaseID resolves to NOTHING silently
			// disables every other CC-* rule for it, and no other rule notices —
			// report the dangling join key rather than no-op'ing the whole family.
			out = append(out, ccFinding(RuleCCViewUseCase, i, "dynamicView "+ccViewLabel(dv),
				"dynamic view %q references useCaseId %q, which resolves to no use case in the committed set; the call chain cannot be checked against any activity diagram until the join key is fixed",
				ccViewLabel(dv), dv.UseCaseID))
			continue
		}
		// A use case with no activity diagram has nothing to correspond TO — that
		// gap belongs to the activity-diagram-presence rule, not this family.
		if uc.Activity == nil {
			continue
		}
		out = append(out, newCCContext(dv, uc, idx, i).findings()...)
	}
	return out
}

// ---- CUC-ACTOR-REQUIRED ----

// actorRequiredFindings — CUC-ACTOR-REQUIRED (founder ruling R-A, rollout
// rulings 2026-07-31). A clientAction use case is, by definition, initiated BY
// somebody: declaring zero actors leaves the initiator unnamed, and leaves the
// realization with no legal chain root either (CC-PATH-CONNECTED roots a
// clientAction path on actor→Client). Timer- and busMessage-triggered use
// cases are started by the clock or the bus and legitimately declare none.
//
// Unlike the rest of this file's CC-* family, this rule is NOT per-dynamic-view
// — it runs over every committed use case regardless of whether a dynamic view
// exists for it yet — so callChainFindings calls it directly rather than
// folding it into ccContext.findings().
func actorRequiredFindings(ucs []coreUseCase) []methodcheck.Finding {
	var out []methodcheck.Finding
	for i, uc := range ucs {
		if uc.Trigger != "clientAction" || len(uc.Actors) > 0 {
			continue
		}
		out = append(out, ccFinding(RuleCUCActorRequired, i, "useCase "+uc.ID,
			"use case %q is clientAction-triggered but declares no actors; a client-initiated use case must name who initiates it (and its call chain needs that actor as its root)",
			uc.ID))
	}
	return out
}

// useCaseIndex maps use-case id → coreUseCase across the whole slot-4 decision
// set (core AND nonCore/variation — every one of them owns a dynamic view).
// Last decision wins on a duplicate id.
func useCaseIndex(ucs []coreUseCase) map[string]coreUseCase {
	idx := make(map[string]coreUseCase, len(ucs))
	for _, uc := range ucs {
		idx[uc.ID] = uc
	}
	return idx
}

// actorIDs returns the id set of a use case's declared actors.
func actorIDs(uc coreUseCase) map[string]bool {
	ids := make(map[string]bool, len(uc.Actors))
	for _, a := range uc.Actors {
		ids[a.ID] = true
	}
	return ids
}

// ccViewLabel is the CC-* section-grammar view identifier: the view's Key,
// falling back to its UseCaseID only when Key is empty.
func ccViewLabel(dv dynamicView) string {
	if dv.Key != "" {
		return dv.Key
	}
	return dv.UseCaseID
}

// ccContext is one dynamic view's evaluation context: the view, its owning use
// case, and the indices every CC rule joins on. Bundling them keeps each rule a
// method with no parameter train.
type ccContext struct {
	dv       dynamicView
	uc       coreUseCase
	idx      map[string]projectmodel.SystemComponent // component id → Component
	actors   map[string]bool                         // actor id (of THIS use case) → true
	nodes    map[string]activityNode                 // activity node id → node
	steps    map[string]callStep                     // activity node id → the step realizing it (first wins)
	incoming map[string]int                          // activity node id → count of incoming edges
	ordinal  int                                     // the dynamic view's position (finding order key)
}

func newCCContext(dv dynamicView, uc coreUseCase, idx map[string]projectmodel.SystemComponent, ordinal int) ccContext {
	nodes := make(map[string]activityNode, len(uc.Activity.Nodes))
	for _, n := range uc.Activity.Nodes {
		nodes[n.ID] = n
	}
	steps := make(map[string]callStep, len(dv.Steps))
	for _, st := range dv.Steps {
		if _, dup := steps[st.ActivityNodeID]; !dup {
			steps[st.ActivityNodeID] = st
		}
	}
	incoming := make(map[string]int, len(uc.Activity.Nodes))
	for _, e := range uc.Activity.Edges {
		incoming[e.To]++
	}
	return ccContext{
		dv: dv, uc: uc, idx: idx, actors: actorIDs(uc),
		nodes: nodes, steps: steps, incoming: incoming, ordinal: ordinal,
	}
}

// findings runs the whole family over one view, in stable rule order.
func (cc ccContext) findings() []methodcheck.Finding {
	var out []methodcheck.Finding
	out = append(out, cc.stepIdentity()...)
	out = append(out, cc.coverage()...)
	out = append(out, cc.stepNonempty()...)
	out = append(out, cc.endpointResolves()...)
	out = append(out, cc.actorEdges()...)
	out = append(out, cc.actorLane()...)
	out = append(out, cc.decidedBy()...)
	out = append(out, cc.triggerEvent()...)
	out = append(out, cc.pathConnected()...)
	return out
}

// stepLoc is the STEP-SCOPED section grammar. The webApp joins its per-step
// finding badges on this exact string — do not reshape it without the app side.
func (cc ccContext) stepLoc(nodeID string) string {
	return "dynamicView " + ccViewLabel(cc.dv) + " step " + nodeID
}

// ucLoc is the USE-CASE-SCOPED section grammar (coverage + trigger findings,
// which are statements about the use case as a whole, not about any one step).
func (cc ccContext) ucLoc() string {
	return "useCase " + cc.uc.ID
}

// ccFinding builds a finding at the family's shared PoC severity.
func ccFinding(id methodcheck.RuleID, ordinal int, section, format string, args ...any) methodcheck.Finding {
	return finding(id, ccLiveSeverity, ordinal, section, fmt.Sprintf(format, args...))
}

// ---- CC-STEP-NODE / CC-STEP-UNIQUE ----

// stepIdentity checks that each step names a REAL activity node (CC-STEP-NODE)
// and that no node is realized twice (CC-STEP-UNIQUE).
func (cc ccContext) stepIdentity() []methodcheck.Finding {
	var out []methodcheck.Finding
	seen := make(map[string]bool, len(cc.dv.Steps))
	for _, st := range cc.dv.Steps {
		if _, ok := cc.nodes[st.ActivityNodeID]; !ok {
			out = append(out, ccFinding(RuleCCStepNode, cc.ordinal, cc.stepLoc(st.ActivityNodeID),
				"dynamic view %q has a step keyed on %q, which is not a node of use case %s's activity diagram; every step must realize a declared activity node",
				ccViewLabel(cc.dv), st.ActivityNodeID, cc.uc.ID))
		}
		if seen[st.ActivityNodeID] {
			out = append(out, ccFinding(RuleCCStepUnique, cc.ordinal, cc.stepLoc(st.ActivityNodeID),
				"dynamic view %q realizes activity node %q with more than one step; a node's calls belong to exactly one step",
				ccViewLabel(cc.dv), st.ActivityNodeID))
		}
		seen[st.ActivityNodeID] = true
	}
	return out
}

// ---- CC-COVERAGE ----

// coverage is the BIDIRECTIONAL step/node correspondence: every node that must
// be realized IS (diagram → view), and every step keys a node that may legally
// carry one (view → diagram). Dangling steps are CC-STEP-NODE's business and
// are skipped here.
func (cc ccContext) coverage() []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, n := range cc.uc.Activity.Nodes {
		if !ccMustHaveStep[n.Kind] {
			continue
		}
		if _, ok := cc.steps[n.ID]; ok {
			continue
		}
		out = append(out, ccFinding(RuleCCCoverage, cc.ordinal, cc.ucLoc(),
			"activity node %q (%s) of use case %s is realized by no step of dynamic view %q; every action/timeEvent/acceptEvent node must say which calls it makes",
			n.ID, n.Kind, cc.uc.ID, ccViewLabel(cc.dv)))
	}
	for _, st := range cc.dv.Steps {
		n, ok := cc.nodes[st.ActivityNodeID]
		if !ok || ccMustHaveStep[n.Kind] || ccMayHaveStep[n.Kind] {
			continue
		}
		out = append(out, ccFinding(RuleCCCoverage, cc.ordinal, cc.ucLoc(),
			"dynamic view %q attaches a step to %s node %q; only action/timeEvent/acceptEvent nodes carry calls (decision/switch may), every other node is pure control flow",
			ccViewLabel(cc.dv), n.Kind, n.ID))
	}
	return out
}

// ---- CC-STEP-NONEMPTY ----

// stepNonempty: a realized step that makes no call says nothing — either the
// node makes calls (and they belong here) or it should carry no step at all.
func (cc ccContext) stepNonempty() []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, st := range cc.dv.Steps {
		if len(st.Calls) > 0 {
			continue
		}
		out = append(out, ccFinding(RuleCCStepNonempty, cc.ordinal, cc.stepLoc(st.ActivityNodeID),
			"dynamic view %q step %q makes no call; a realized step must carry at least one call (drop the step if the node makes none)",
			ccViewLabel(cc.dv), st.ActivityNodeID))
	}
	return out
}

// ---- CC-ENDPOINT-RESOLVES ----

// endpointResolves checks that every call endpoint resolves to EXACTLY ONE of
// the two namespaces a call chain draws from: the System's components, or the
// owning use case's actors. Reported once per distinct id per view, at the
// first step that names it.
func (cc ccContext) endpointResolves() []methodcheck.Finding {
	var out []methodcheck.Finding
	reported := map[string]bool{}
	for _, st := range cc.dv.Steps {
		for _, call := range st.Calls {
			out = append(out, cc.callEndpointFindings(call, st.ActivityNodeID, reported)...)
		}
	}
	return out
}

// callEndpointFindings resolves one call's two ends, skipping ids already
// reported for this view.
func (cc ccContext) callEndpointFindings(call viewEdge, nodeID string, reported map[string]bool) []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, id := range []string{call.From, call.To} {
		if reported[id] {
			continue
		}
		if f, bad := cc.endpointFinding(id, nodeID); bad {
			reported[id] = true
			out = append(out, f)
		}
	}
	return out
}

func (cc ccContext) endpointFinding(id, nodeID string) (methodcheck.Finding, bool) {
	_, isComponent := cc.idx[id]
	isActor := cc.actors[id]
	switch {
	case isComponent && isActor:
		return ccFinding(RuleCCEndpoint, cc.ordinal, cc.stepLoc(nodeID),
			"dynamic view %q names endpoint %q, which resolves to BOTH a System Component and an actor of use case %s; an endpoint id must denote exactly one of them",
			ccViewLabel(cc.dv), id, cc.uc.ID), true
	case !isComponent && !isActor:
		return ccFinding(RuleCCEndpoint, cc.ordinal, cc.stepLoc(nodeID),
			"dynamic view %q names endpoint %q, which is neither a System Component nor an actor of use case %s",
			ccViewLabel(cc.dv), id, cc.uc.ID), true
	}
	return methodcheck.Finding{}, false
}

// ---- CC-ACTOR-EDGE ----

// actorEdges enforces the actor-interaction grammar: a person touches the
// system only through a Client, and only synchronously. Two actors talking to
// each other is not a system call at all.
func (cc ccContext) actorEdges() []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, st := range cc.dv.Steps {
		for _, call := range st.Calls {
			out = append(out, cc.actorEdgeFindings(call, st.ActivityNodeID)...)
		}
	}
	return out
}

func (cc ccContext) actorEdgeFindings(call viewEdge, nodeID string) []methodcheck.Finding {
	fromActor, toActor := cc.actors[call.From], cc.actors[call.To]
	if !fromActor && !toActor {
		return nil
	}
	section := cc.stepLoc(nodeID)
	if fromActor && toActor {
		return []methodcheck.Finding{ccFinding(RuleCCActorEdge, cc.ordinal, section,
			"dynamic view %q step %q draws actor %s → actor %s; an actor edge models a person entering the system through a Client, not two people interacting",
			ccViewLabel(cc.dv), nodeID, call.From, call.To)}
	}
	component := call.To
	if toActor {
		component = call.From
	}
	var out []methodcheck.Finding
	if c, ok := cc.idx[component]; !ok || c.Kind != "client" {
		out = append(out, ccFinding(RuleCCActorEdge, cc.ordinal, section,
			"dynamic view %q step %q draws actor edge %s→%s, whose non-actor end %s is not a Client component; an actor enters the system only through a Client",
			ccViewLabel(cc.dv), nodeID, call.From, call.To, component))
	}
	if call.Mode != "sync" {
		out = append(out, ccFinding(RuleCCActorEdge, cc.ordinal, section,
			"dynamic view %q step %q draws actor edge %s→%s with mode %q; an actor interaction is always synchronous",
			ccViewLabel(cc.dv), nodeID, call.From, call.To, call.Mode))
	}
	return out
}

// ---- CC-ACTOR-LANE ----

// actorLane ties the diagram's swim-lane assignment to the realization: a node
// placed in an actor's lane claims that actor performs it, so the step
// realizing that node must actually touch the actor. A lane no call honors is
// decoration.
func (cc ccContext) actorLane() []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, n := range cc.uc.Activity.Nodes {
		if n.LinkedActorID == "" {
			continue
		}
		st, ok := cc.steps[n.ID]
		if !ok || stepTouches(st, n.LinkedActorID) {
			continue
		}
		out = append(out, ccFinding(RuleCCActorLane, cc.ordinal, cc.stepLoc(n.ID),
			"activity node %q is laned to actor %s, but dynamic view %q's step for it touches that actor in no call; realize the actor's participation or drop the lane link",
			n.ID, n.LinkedActorID, ccViewLabel(cc.dv)))
	}
	return out
}

// stepTouches reports whether any of a step's calls names id at either end.
func stepTouches(st callStep, id string) bool {
	for _, call := range st.Calls {
		if call.From == id || call.To == id {
			return true
		}
	}
	return false
}

// ---- CC-DECIDED-BY ----

// decidedBy checks the optional decider attribution an activity node may carry
// (rollout rulings 2026-07-31), in its two halves:
//
//   - PLACEMENT: only a decision/switch RESOLVES a branch, so only those kinds
//     can name who resolves it. A decidedBy anywhere else is misplaced even
//     when its value resolves perfectly well.
//   - RESOLUTION: the value resolves exactly like a call endpoint — against
//     the System's components UNION the owning use case's actors. Naming
//     neither is a dangling attribution; naming BOTH is ambiguous, for the
//     same reason CC-ENDPOINT-RESOLVES treats a both-match as one finding.
//
// The rule is USE-CASE-scoped: a node is not a step, so there is no step
// section to hang it on.
func (cc ccContext) decidedBy() []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, n := range cc.uc.Activity.Nodes {
		if n.DecidedBy == "" {
			continue
		}
		if !ccResolvesBranch(n.Kind) {
			out = append(out, ccFinding(RuleCCDecidedBy, cc.ordinal, cc.ucLoc(),
				"activity node %q (%s) of use case %s carries decidedBy %q; only a decision/switch node resolves a branch, so only those kinds may name who resolves it",
				n.ID, n.Kind, cc.uc.ID, n.DecidedBy))
			continue
		}
		if f, bad := cc.decidedByResolution(n); bad {
			out = append(out, f)
		}
	}
	return out
}

// ccResolvesBranch reports whether a node kind resolves a branch — the only
// kinds a decidedBy may sit on. It coincides with ccMayHaveStep's membership,
// but for a different reason (that set is about carrying CALLS), so the two
// are kept apart.
func ccResolvesBranch(kind string) bool {
	return kind == "decision" || kind == "switch"
}

// decidedByResolution resolves one branching node's decider against the two
// namespaces, mirroring endpointFinding.
func (cc ccContext) decidedByResolution(n activityNode) (methodcheck.Finding, bool) {
	_, isComponent := cc.idx[n.DecidedBy]
	isActor := cc.actors[n.DecidedBy]
	switch {
	case isComponent && isActor:
		return ccFinding(RuleCCDecidedBy, cc.ordinal, cc.ucLoc(),
			"activity node %q of use case %s is decidedBy %q, which resolves to BOTH a System Component and an actor of that use case; a decider id must denote exactly one of them",
			n.ID, cc.uc.ID, n.DecidedBy), true
	case !isComponent && !isActor:
		return ccFinding(RuleCCDecidedBy, cc.ordinal, cc.ucLoc(),
			"activity node %q of use case %s is decidedBy %q, which is neither a System Component nor an actor of that use case; name the component or the person who resolves the branch",
			n.ID, cc.uc.ID, n.DecidedBy), true
	}
	return methodcheck.Finding{}, false
}

// ---- CC-TRIGGER-EVENT ----

// triggerEvent aligns the use-case trigger with the diagram's ENTRY nodes: a
// timer trigger must enter on a timeEvent, a bus-message trigger on an
// acceptEvent, and a client-action trigger on neither. An "entry" here is an
// event node with no incoming edge — nothing leads into a trigger.
func (cc ccContext) triggerEvent() []methodcheck.Finding {
	hasTimeEntry, hasAcceptEntry := cc.eventEntries()
	switch cc.uc.Trigger {
	case "timer":
		if !hasTimeEntry {
			return []methodcheck.Finding{ccFinding(RuleCCTriggerEvent, cc.ordinal, cc.ucLoc(),
				"use case %s is timer-triggered but its activity diagram declares no timeEvent entry node; a scheduled use case enters on the timer that fires it",
				cc.uc.ID)}
		}
	case "busMessage":
		if !hasAcceptEntry {
			return []methodcheck.Finding{ccFinding(RuleCCTriggerEvent, cc.ordinal, cc.ucLoc(),
				"use case %s is busMessage-triggered but its activity diagram declares no acceptEvent entry node; a message-driven use case enters on the signal it accepts",
				cc.uc.ID)}
		}
	case "clientAction":
		if hasTimeEntry || hasAcceptEntry {
			return []methodcheck.Finding{ccFinding(RuleCCTriggerEvent, cc.ordinal, cc.ucLoc(),
				"use case %s is clientAction-triggered but its activity diagram enters on a UML event node; a client-initiated use case enters at a start node — reclassify the trigger as timer/busMessage or remove the event entry",
				cc.uc.ID)}
		}
	}
	return nil
}

// eventEntries reports whether the diagram carries a timeEvent (resp.
// acceptEvent) node with no incoming edge — the shape that makes an event node
// the diagram's entry. An event node WITH an incoming edge is a mid-flow event,
// not a trigger, and does not count.
func (cc ccContext) eventEntries() (timeEntry, acceptEntry bool) {
	for _, n := range cc.uc.Activity.Nodes {
		if cc.incoming[n.ID] > 0 {
			continue
		}
		switch n.Kind {
		case "timeEvent":
			timeEntry = true
		case "acceptEvent":
			acceptEntry = true
		}
	}
	return timeEntry, acceptEntry
}

// ---- CC-PATH-CONNECTED ----

// pathConnected is the heart of the correspondence: for EVERY entry→end path
// the activity diagram admits (activityPaths below enumerates them), the steps
// realized along that path must compose into a CONNECTED call chain.
//
// A call is connected when any of these holds:
//   - it is an actor→Client call — a person entering the system always
//     re-seeds the chain, at any point;
//   - it is the path's FIRST call and matches the entry kind's root shape
//     (timeEvent → Client→Manager; acceptEvent → a queued call into a
//     Manager; a start/clientAction entry → the actor→Client case above);
//   - its From is already in the reached set.
//
// Findings are deduplicated across paths by (node, from, to): one disconnect
// reported once, not once per path that happens to traverse it.
//
// EVENT-ROOTED SUFFIX PATHS ARE SKIPPED (see ccIsSuffixPath): activityPaths
// treats every EVENT node as an enumeration root wherever it sits, so a
// mid-flow event node also yields the bare suffix path starting at it. Walking
// that suffix would restart with an empty reached set and misjudge the event's
// step against the wrong root shape. A START-rooted path is NEVER a suffix —
// it is always a primary entry, incoming back-edge or not.
func (cc ccContext) pathConnected() []methodcheck.Finding {
	var out []methodcheck.Finding
	reported := map[string]bool{}
	for _, p := range activityPaths(*cc.uc.Activity) {
		if cc.ccIsSuffixPath(p.Entry) {
			continue
		}
		out = append(out, cc.walkRealizedPath(p.Entry, p.Nodes, reported)...)
	}
	return out
}

// ccIsSuffixPath reports whether an enumerated path is a mere SUFFIX of
// another path this walk already covers. The predicate is membership in the
// EVENT kinds AND a non-zero incoming count — both, never the count alone (a
// start-rooted path with a back-edge is an ordinary authored "retry" shape and
// must always be walked as a primary entry).
func (cc ccContext) ccIsSuffixPath(entry pathEntry) bool {
	switch entry.Kind {
	case "timeEvent", "acceptEvent":
		return cc.incoming[entry.NodeID] > 0
	default: // "start" — always a primary entry, always walked.
		return false
	}
}

// ccPathWalk is the mutable state carried ALONG one path: which endpoints the
// chain has reached so far, and whether the next call is still the path's
// first.
type ccPathWalk struct {
	reached map[string]bool
	first   bool
}

func (cc ccContext) walkRealizedPath(entry pathEntry, nodeIDs []string, reported map[string]bool) []methodcheck.Finding {
	w := &ccPathWalk{reached: map[string]bool{}, first: true}
	var out []methodcheck.Finding
	for _, nodeID := range nodeIDs {
		st, ok := cc.steps[nodeID]
		if !ok {
			continue
		}
		out = append(out, cc.walkStepCalls(st, nodeID, entry, w, reported)...)
	}
	return out
}

// walkStepCalls threads one realized step's call fragment through the walk
// state, reporting each call that is neither legally rooted nor continuing
// from a reached endpoint.
func (cc ccContext) walkStepCalls(st callStep, nodeID string, entry pathEntry, w *ccPathWalk, reported map[string]bool) []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, call := range st.Calls {
		if !cc.callConnects(call, entry, w.first, w.reached) {
			if f, fresh := cc.disconnectFinding(call, nodeID, entry, reported); fresh {
				out = append(out, f)
			}
		}
		w.reached[call.From] = true
		w.reached[call.To] = true
		w.first = false
	}
	return out
}

// disconnectFinding builds the CC-PATH-CONNECTED finding for one disconnected
// call, returning fresh=false when this (node, from, to) was already reported
// on an earlier path.
func (cc ccContext) disconnectFinding(call viewEdge, nodeID string, entry pathEntry, reported map[string]bool) (methodcheck.Finding, bool) {
	key := nodeID + "\x00" + call.From + "\x00" + call.To
	if reported[key] {
		return methodcheck.Finding{}, false
	}
	reported[key] = true
	return ccFinding(RuleCCPathConnected, cc.ordinal, cc.stepLoc(nodeID),
		"dynamic view %q step %q calls %s→%s, but %s is not reached by any earlier call on the activity path entered at %q, and the call is not a legal chain root (actor→Client, or the entry's own root shape)",
		ccViewLabel(cc.dv), nodeID, call.From, call.To, call.From, entry.NodeID), true
}

func (cc ccContext) callConnects(call viewEdge, entry pathEntry, first bool, reached map[string]bool) bool {
	if cc.isActorToClient(call) {
		return true
	}
	if first {
		return cc.rootsEntry(call, entry)
	}
	return reached[call.From]
}

// isActorToClient reports the always-legal chain root: an actor of this use
// case entering a Client component.
func (cc ccContext) isActorToClient(call viewEdge) bool {
	if !cc.actors[call.From] {
		return false
	}
	to, ok := cc.idx[call.To]
	return ok && to.Kind == "client"
}

// rootsEntry reports whether a path's FIRST call is a legal root for that
// path's entry kind. A start entry (the clientAction shape) has only ONE legal
// root — the actor→Client call callConnects already accepted — so it is false
// here.
func (cc ccContext) rootsEntry(call viewEdge, entry pathEntry) bool {
	from, fromOK := cc.idx[call.From]
	to, toOK := cc.idx[call.To]
	switch entry.Kind {
	case "timeEvent":
		return fromOK && toOK && from.Kind == "client" && to.Kind == "manager"
	case "acceptEvent":
		return toOK && to.Kind == "manager" && call.Mode == "queued"
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// activityPaths — PURE, package-private plumbing enumerating every entry→end
// node-id path through an activityDiagram. This is a package-local copy of the
// platform's framework-go/methodcheck/activitypaths.go (the same
// designhealth-vs-methodcheck twin duplication the rest of this package
// already carries), ported verbatim including its bounding rules:
//
//   - entries are every "start" node PLUS every UML event node (timeEvent/
//     acceptEvent), wherever they sit; loops (back-edges) are traversed AT
//     MOST ONCE per path via a per-path visited-EDGE set; a fork's branches
//     cross-product (each branch's own alternative set computed exactly once)
//     and concatenate into the SAME path (fork-without-join legal).
//   - the OUTPUT cap (maxActivityPaths) truncates the returned set, applied
//     EXACTLY ONCE as a final truncation of whatever was enumerated.
//   - the WORK budget (maxWalkWork, 2026-07-31 rollout rulings) bounds the
//     RECURSION itself: designhealth runs this render-on-read over committed
//     state, so a pathological diagram is a CPU/memory sink even with the
//     output capped — truncating the output alone still requires
//     materializing the complete result first. The budget is charged per
//     WALK-STEP (one node id materialized into a sequence — see spend/carry),
//     never per final path.
//
// On exhaustion the walk stops EXPLORING; the walks it had already COMPLETED
// are still carried up to the caller, capped at maxActivityPaths per level
// (carry), so a blowup degrades to a smaller — often still cap-sized — answer
// instead of an empty or fabricated one. The degradation is deterministic
// (entries in declared order, branches in declared edge order) and only ever
// UNDER-approximates: every returned path is a real, complete path of the
// diagram, so CC-PATH-CONNECTED can lose a finding to a pathological diagram
// but can never gain a false one.
//
// carry's ONE exclusion: crossProduct is deliberately NOT wrapped in carry —
// it charges via spend directly, raw. Exempting it from the post-exhaustion
// escape hatch is what keeps the fork's per-combination visited-edge-set
// allocation (the memory-heaviest shape) from reopening the hole
// charge-by-length closed. One consequence, deliberate and NOT a bug: a
// pure-fork-only blowup — the diagram's only entry leads into a fork that
// never finishes folding — degrades to exhausted=true with a length-0 result,
// not a partial answer. That is the pre-existing all-or-nothing-fork design
// (walkFork returns nil the moment any branch or fold comes back empty),
// unchanged by this port; see TestPaths_PureForkOnlyBlowupYieldsEmptyResult.
// ---------------------------------------------------------------------------

// maxActivityPaths caps the total number of enumerated paths per diagram
// (across ALL entries).
const maxActivityPaths = 512

// maxWalkWork caps the enumeration's WALK-STEPS per diagram. One step is one
// node id MATERIALIZED into a walk sequence — see spend and carry, which
// between them charge every sequence the walk builds, both where one is
// created and where one is COPIED a level up. Charging the copies is what
// makes the budget a memory bound and not merely a CPU one.
//
// SIZING (re-derived from the platform's 2026-07-31 fix-round-1 measurement):
// the honest claim is stated in terms of the OUTPUT CAP, not node count — any
// diagram whose COMPLETE enumeration would fit the cap (<=512 paths, <=40
// nodes deep, widest admitted fan) costs at most ~550k steps, so it is never
// budget-truncated: its result is bit-identical to an unbounded walk's. A
// 22-decision reconverging chain (4.2M true paths, no fork) returns a full
// 512 paths at this budget having allocated well under the 256MB ceiling
// TestPaths_BudgetBoundsDecisionChain asserts; an 8-branch fork of 5-way
// decisions (390,625 combinations) trips the budget promptly. Raising the
// budget further is not free: the fork shape allocates far more per step than
// the decision shape (each combination unions two visited-edge SETS), so a
// budget generous enough to fully enumerate a several-thousand-path chain
// would put the fork-shaped worst case back into the memory-sink range this
// bound exists to prevent — and such a diagram is truncated to the 512-path
// cap either way, so nothing real is lost by holding the line here.
const maxWalkWork = 1_000_000

// pathEntry describes one enumeration root of an activity diagram.
type pathEntry struct {
	NodeID string
	Kind   string // "start", "timeEvent", "acceptEvent"
}

// activityPath is one enumerated entry→end path: its root and the node ids it
// visits, in walk order.
type activityPath struct {
	Entry pathEntry
	Nodes []string // node ids in walk order, Entry.NodeID first
}

// activityWalk is one in-progress (or completed) DFS walk: the node-id
// sequence produced so far, plus the set of edge indices already consumed
// along it.
type activityWalk struct {
	seq     []string
	visited map[int]bool
}

// walker carries the diagram-wide, walk-invariant enumeration state — the
// edge index, the node kinds, and the remaining work budget — so each step of
// the recursion is a method with no parameter train.
type walker struct {
	edges       []activityEdge
	kindByID    map[string]string
	edgesByFrom map[string][]int // edge INDICES by From node, in declared order
	remaining   int              // walk-steps left in this diagram's budget
	exhausted   bool             // sticky: set the first time a charge is refused
}

// activityPaths enumerates every entry→end node-id path of a (see the file
// header for the two bounds).
func activityPaths(a activityDiagram) []activityPath {
	paths, _ := boundedActivityPaths(a)
	return paths
}

// boundedActivityPaths is activityPaths plus the budget verdict: exhausted
// reports whether the walk stopped early because maxWalkWork ran out — the
// difference between "this diagram has 3 paths" and "this diagram has more
// paths than anyone can enumerate". Kept package-private (rules read
// activityPaths; the walker's own tests read this) so the tests can assert on
// the verdict instead of a flaky wall-clock assertion.
func boundedActivityPaths(a activityDiagram) (paths []activityPath, exhausted bool) {
	w := newWalker(a)
	var out []activityPath
	for _, entry := range diagramEntries(a) {
		for _, walk := range w.walkFrom(entry.NodeID, map[int]bool{}) {
			out = append(out, activityPath{Entry: entry, Nodes: walk.seq})
		}
	}
	if len(out) > maxActivityPaths {
		out = out[:maxActivityPaths]
	}
	return out, w.exhausted
}

func newWalker(a activityDiagram) *walker {
	kindByID := make(map[string]string, len(a.Nodes))
	for _, n := range a.Nodes {
		kindByID[n.ID] = n.Kind
	}
	// edgesByFrom groups edge INDICES (not copies) by their From node,
	// preserving the diagram's declared Edges order — that order decides both
	// branch order (decision/switch) and concatenation order (fork).
	edgesByFrom := make(map[string][]int, len(a.Nodes))
	for i, e := range a.Edges {
		edgesByFrom[e.From] = append(edgesByFrom[e.From], i)
	}
	return &walker{edges: a.Edges, kindByID: kindByID, edgesByFrom: edgesByFrom, remaining: maxWalkWork}
}

// diagramEntries lists the diagram's enumeration roots in declared node order.
func diagramEntries(a activityDiagram) []pathEntry {
	var entries []pathEntry
	for _, n := range a.Nodes {
		switch n.Kind {
		case "start", "timeEvent", "acceptEvent":
			entries = append(entries, pathEntry{NodeID: n.ID, Kind: n.Kind})
		}
	}
	return entries
}

// spend charges n walk-steps of MATERIALIZATION — a terminal walk, a fork
// combination, or (through carry) a completed sub-walk copied a level up.
// Every sequence the walk builds goes through here, which is what makes the
// budget a memory bound and not merely a CPU one. Refusal is sticky — once
// the walk is out of budget it stays out, so the result cannot depend on the
// order in which the remainder of the recursion happened to ask.
func (w *walker) spend(n int) bool {
	if w.exhausted || w.remaining < n {
		w.exhausted = true
		return false
	}
	w.remaining -= n
	return true
}

// carry charges n walk-steps of ASSEMBLY — copying an ALREADY-COMPLETED
// sub-walk up one level — where assembled is how many walks this level has
// carried so far.
//
// While the budget holds, assembly is charged exactly like exploration: the
// copy is real materialization, and NOT charging it collapses the budget to a
// walk-COUNT bound instead of a materialization bound (a decision-shaped
// blowup then spikes memory well past the intended ceiling — the same defect
// class the platform's fix-round-1 measured and fixed).
//
// Once exploration is exhausted, assembly continues UNBUDGETED but capped at
// maxActivityPaths per level. That is the graceful-degradation half: the
// walks already completed must be able to reach the caller instead of being
// stranded one frame below it (charging assembly with no escape hatch would
// drain the budget bottom-up and collapse every blowup to zero paths), and
// carrying more than the output cap is pure waste because the caller
// truncates to it anyway. The escape hatch is structurally bounded — at most
// cap x depth node ids per level, over at most depth levels — so it cannot
// reopen the memory hole the charging closed.
//
// crossProduct does NOT go through carry (it calls spend directly) — see the
// file header's carry-exclusion note.
func (w *walker) carry(assembled, n int) bool {
	if !w.exhausted && w.spend(n) {
		return true
	}
	return assembled < maxActivityPaths
}

// walkFrom performs the recursive DFS described on activityPaths, returning
// every completed walk starting at nodeID given the edges already visited on
// the path so far. An EMPTY return means the budget ran out before ANYTHING
// below this node completed (an unexhausted walk always yields at least one
// walk — worst case, the terminal one — and an exhausted one still carries up
// whatever did complete). walkFork relies on that invariant to tell "this
// branch contributed nothing" from "this branch contributed fewer
// alternatives than it would have".
func (w *walker) walkFrom(nodeID string, visited map[int]bool) []activityWalk {
	if w.exhausted {
		return nil
	}
	eligible := eligibleEdges(nodeID, w.edgesByFrom, visited)

	// An end node always terminates, even with an outgoing edge; otherwise a
	// node with no eligible (unvisited) outgoing edge left terminates too —
	// this bounds a loop to being traversed at most once.
	if w.kindByID[nodeID] == "end" || len(eligible) == 0 {
		if !w.spend(1) {
			return nil
		}
		return []activityWalk{{seq: []string{nodeID}, visited: visited}}
	}

	if w.kindByID[nodeID] == "fork" {
		return w.walkFork(nodeID, eligible, visited)
	}

	// Default: decision/switch (and, degenerately, any single-outgoing-edge
	// node) — one walk per eligible outgoing edge.
	return w.branchOverEdges(nodeID, eligible, visited)
}

// eligibleEdges lists nodeID's outgoing edge indices, in declared order,
// EXCLUDING any edge already consumed on this path (loop-once).
func eligibleEdges(nodeID string, edgesByFrom map[string][]int, visited map[int]bool) []int {
	var eligible []int
	for _, idx := range edgesByFrom[nodeID] {
		if !visited[idx] {
			eligible = append(eligible, idx)
		}
	}
	return eligible
}

// branchOverEdges implements the decision/switch (and single-eligible-edge
// default) case: one walk per eligible outgoing edge, each prefixed with
// nodeID. Alternatives are mutually exclusive, so each starts from an
// independent copy of the pre-branch visited set.
//
// A branch that comes back empty is one the budget cut short; its
// already-completed siblings are kept and returned. That is the graceful
// half of exhaustion: a decision blowup returns the alternatives enumerated
// before the budget ran out, in declared order, truncated to what carry
// still allows.
func (w *walker) branchOverEdges(nodeID string, eligible []int, visited map[int]bool) []activityWalk {
	var out []activityWalk
	for _, idx := range eligible {
		v := cloneVisited(visited)
		v[idx] = true
		for _, sub := range w.walkFrom(w.edges[idx].To, v) {
			if !w.carry(len(out), 1+len(sub.seq)) {
				return out
			}
			out = append(out, activityWalk{seq: append([]string{nodeID}, sub.seq...), visited: sub.visited})
		}
	}
	return out
}

// walkFork implements the fork's semantics: unlike a decision/switch, a
// fork's outgoing edges do NOT branch into alternatives — every branch is
// taken, so their walks are combined into the SAME path. A branch containing
// internal decision/switch branching contributes multiple alternative walks
// of its own, which are CROSS-PRODUCTED against the other branches'
// alternatives — each branch's own alternative set computed exactly once,
// independent of how many combinations have been accumulated from earlier
// branches so far.
//
// Exhaustion is ALL-OR-NOTHING at the BRANCH level, unlike a decision's
// graceful truncation: a fork path is only a real path of the diagram once
// EVERY branch is folded into it, so a fork whose branch (or whose fold) came
// back with NOTHING contributes no path at all, rather than a fabricated one
// missing a parallel branch (which could make a perfectly connected call
// chain look disconnected to CC-PATH-CONNECTED). This is what makes a
// pure-fork-only blowup degrade to exhausted=true, len=0 rather than a
// partial answer — pre-existing, deliberate, and not "fixed" by this bound.
func (w *walker) walkFork(nodeID string, eligible []int, visited map[int]bool) []activityWalk {
	partials := []activityWalk{{seq: nil, visited: visited}}
	for _, idx := range eligible {
		v := cloneVisited(visited)
		v[idx] = true
		branch := w.walkFrom(w.edges[idx].To, v)
		if len(branch) == 0 {
			return nil // the budget cut this branch short — see the doc comment
		}
		partials = w.crossProduct(partials, branch)
		if len(partials) == 0 {
			return nil
		}
	}

	out := make([]activityWalk, 0, len(partials))
	for _, p := range partials {
		if !w.carry(len(out), 1+len(p.seq)) {
			break
		}
		out = append(out, activityWalk{seq: append([]string{nodeID}, p.seq...), visited: p.visited})
	}
	return out
}

// crossProduct combines every in-progress partial with every alternative of
// the next branch, in order (partials outer, branch inner — so branch N's
// alternatives vary fastest), concatenating sequences and UNIONING
// visited-edge sets. Deliberately EXCLUDED from carry — charged via spend
// directly: the fork shape is the memory-heaviest case (each combination
// allocates a fresh visited-edge-set union), so wrapping it in the
// post-exhaustion unbudgeted escape hatch would reopen the memory hole
// charge-by-length closed.
func (w *walker) crossProduct(partials, branch []activityWalk) []activityWalk {
	var next []activityWalk
	for _, p := range partials {
		for _, b := range branch {
			if !w.spend(len(p.seq) + len(b.seq)) {
				return next
			}
			next = append(next, activityWalk{
				seq:     append(append([]string{}, p.seq...), b.seq...),
				visited: unionVisited(p.visited, b.visited),
			})
		}
	}
	return next
}

// cloneVisited copies a visited-edge set so a branch point can extend it
// independently without mutating the state its siblings (or its own caller)
// still hold.
func cloneVisited(v map[int]bool) map[int]bool {
	out := make(map[int]bool, len(v)+1)
	for k := range v {
		out[k] = true
	}
	return out
}

// unionVisited merges two visited-edge sets (see crossProduct).
func unionVisited(a, b map[int]bool) map[int]bool {
	out := make(map[int]bool, len(a)+len(b))
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// ==========================================================================
// rules_cardinality — folded here by the Engine file-layout standard (one handwritten
// impl file per Engine component). Contents verbatim; imports merged above.
// ==========================================================================

// rules_cardinality.go — App-C component-count guidelines over the systemDesign
// inventory. These are GUIDELINES (SeverityWarning) and an informational count
// (SeverityInfo), never an authoring-gate block: the sweet spots are advisory and
// a project can legitimately sit at the edge. The bounds are deliberately generous
// (the App-C "~10" is treated as an over-bound alarm at >12) so a healthy design
// stays quiet and only a real inventory blow-out surfaces.
func cardinalityFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	counts := map[string]int{}
	for _, c := range in.Model.System.Components {
		counts[c.Kind]++
	}

	var out []methodcheck.Finding
	// §2a: at most ~5 Managers — more than 5 usually means a missing subsystem cut.
	if n := counts["manager"]; n > 5 {
		out = append(out, finding(RuleCardManagers, methodcheck.SeverityWarning, n, "components",
			fmt.Sprintf("%d Manager components — The Method keeps Managers few (≈5); more usually signals a use-case explosion or a missing subsystem partition", n)))
	}
	// ch. 4 smallest-set band starts at 2 Managers: a single-Manager system cannot be
	// validated against the other use cases (there is nothing to swap or compare).
	// Skipped entirely on a zero-component system — mid-draft, nothing to judge yet.
	if len(in.Model.System.Components) > 0 && counts["manager"] == 1 {
		out = append(out, finding(RuleCardManagersMin, methodcheck.SeverityInfo, 1, "components",
			"1 Manager component — ch. 4's smallest-set band starts at 2 Managers; a single-Manager system is unvalidatable (no sibling use-case family to validate the decomposition against). Check for a missed family of use cases"))
	}
	// ch. 4 smallest-set band: 2–3 Engines.
	if n := counts["engine"]; n > 3 {
		out = append(out, finding(RuleCardEngines, methodcheck.SeverityWarning, n, "components",
			fmt.Sprintf("%d Engine components — past the ch. 4 smallest-set band of 2–3 Engines; check for functional decomposition wearing an Engine costume, or fold activities that are not genuinely volatile business rules", n)))
	}
	// §2f: ResourceAccess ≈10 — flag a clear over-bound.
	if n := counts["resourceAccess"]; n > 12 {
		out = append(out, finding(RuleCardRA, methodcheck.SeverityWarning, n, "components",
			fmt.Sprintf("%d ResourceAccess components — well past the ≈10 guideline; consider folding facet-duplicate access components", n)))
	}
	// §2g: Resources ≈10 — flag a clear over-bound.
	if n := counts["resource"]; n > 12 {
		out = append(out, finding(RuleCardResource, methodcheck.SeverityWarning, n, "components",
			fmt.Sprintf("%d Resource components — well past the ≈10 guideline", n)))
	}
	// ch. 4 bounds ResourceAccess + Resources TOGETHER at 3–8 (the individual >12
	// alarms above stay as the blow-out backstop).
	if n := counts["resourceAccess"] + counts["resource"]; n > 8 {
		out = append(out, finding(RuleCardRAResources, methodcheck.SeverityWarning, n, "components",
			fmt.Sprintf("%d combined ResourceAccess + Resource components — past the ch. 4 smallest-set band of 3–8 for the two storage layers together; consider folding facet-duplicate access components or consolidating storage", n)))
	}
	// ch. 4: "a half-dozen Utilities" — informational once past 6.
	if n := counts["utility"]; n > 6 {
		out = append(out, finding(RuleCardUtilities, methodcheck.SeverityInfo, n, "components",
			fmt.Sprintf("%d Utility components — past ch. 4's half-dozen; informational, but check whether any Utility is really a mislabeled business component or duplicated cross-cutting concern", n)))
	}
	// §2: core use case count in [2,6].
	core := 0
	for _, uc := range in.Slots.CoreUseCases {
		if uc.Classification == "core" {
			core++
		}
	}
	if core > 0 && (core < 2 || core > 6) {
		out = append(out, finding(RuleCardCoreUC, methodcheck.SeverityWarning, core, "coreUseCases",
			fmt.Sprintf("%d core use cases — The Method targets 2–6 core use cases as the architecture's load-bearing set", core)))
	}
	// §2h: volatility count is informational — waiver-backed, never blocking.
	if n := len(in.Slots.Volatilities); n > 0 {
		out = append(out, finding(RuleCardVolatility, methodcheck.SeverityInfo, n, "volatilities",
			fmt.Sprintf("%d volatilities identified — informational; the count is waiver-backed, not bounded", n)))
	}
	return out
}

// componentsByKind is a small shared helper for the graph rules.
func componentsByKind(sys *projectmodel.System, kind string) []projectmodel.SystemComponent {
	var out []projectmodel.SystemComponent
	for _, c := range sys.Components {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// ==========================================================================
// rules_chains — folded here by the Engine file-layout standard (one handwritten
// impl file per Engine component). Contents verbatim; imports merged above.
// ==========================================================================

// rules_chains.go — the §6 per-use-case dynamic-view rules. A dynamic view is one
// use-case call chain; The Method constrains how Managers appear in a chain:
//
//	§6a  exactly ONE Manager is the client's entry orchestrator per chain
//	§6b  at most ONE queued Manager→Manager hop per chain
//
// Both are SeverityWarning: a dynamic view is a modeling aid and an odd chain is
// worth a reviewer's eye, not an authoring-gate block. Endpoints that name no
// System component (external actors like a git repo or a human role) are ignored —
// only component participants carry a layer.
func chainFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	idx := in.componentIndex()

	var out []methodcheck.Finding
	for i, dv := range in.Slots.DynamicViews {
		section := "dynamicView " + dvLabel(dv)

		// §6a — count DISTINCT Managers a client directly calls in this chain. The
		// entry orchestrator is the Manager reached from a client edge; downstream
		// queued Managers (permitted by §6b) are not entry points.
		entryManagers := map[string]bool{}
		queuedManagerHops := 0
		for _, s := range dv.Steps {
			for _, e := range s.Calls {
				from, okF := idx[e.From]
				to, okT := idx[e.To]
				if okF && okT && from.Kind == "client" && to.Kind == "manager" {
					entryManagers[to.ID] = true
				}
				if okF && from.Kind == "manager" && e.Mode == "queued" {
					queuedManagerHops++
				}
			}
		}
		if len(entryManagers) > 1 {
			out = append(out, finding(RuleChainEntryManager, methodcheck.SeverityWarning, i, section,
				fmt.Sprintf("chain %q has %d distinct client-entry Managers (%s) — a use-case chain should be driven by exactly one entry Manager; downstream Managers must be reached by a queued hop, not a second client entry",
					dvLabel(dv), len(entryManagers), sortedKeys(entryManagers))))
		}
		// §6b — at most one queued Manager→Manager hop per chain.
		if queuedManagerHops > 1 {
			out = append(out, finding(RuleChainQueuedManager, methodcheck.SeverityWarning, i, section,
				fmt.Sprintf("chain %q has %d queued Manager hops — The Method permits at most one queued Manager→Manager handoff per use-case chain",
					dvLabel(dv), queuedManagerHops)))
		}
	}
	return out
}

// dvLabel picks the most human-readable identifier for a dynamic view.
func dvLabel(dv dynamicView) string {
	if dv.UseCaseID != "" {
		return dv.UseCaseID
	}
	return dv.Key
}

// ==========================================================================
// rules_contracts — folded here by the Engine file-layout standard (one handwritten
// impl file per Engine component). Contents verbatim; imports merged above.
// ==========================================================================

// rules_contracts.go — the service-contract App-C and facet rules over the
// projectmodel.Contracts slice.
//
//	DH-CONTRACT-OPCOUNT-REJECT (Error)   a contract with ≥20 operations — App-C's
//	                                     hard reject (a god interface).
//	DH-CONTRACT-OPCOUNT-MAX    (Warning) a contract past the max-12 guideline.
//	DH-CONTRACT-FACET          (Error)   the ratified facet-doctrine join (the
//	                                     family-D gate promoted to a live finding):
//	                                     a contract key that names no component is
//	                                     valid only as a FACET whose `component`
//	                                     field resolves to a component of the same
//	                                     layer; otherwise it is a fossil.
//	DH-CONTRACT-DEADOP         (Warning) the same operation name published by two
//	                                     contracts of one facet group (the D1-D4
//	                                     duplicate class).
func contractFindings(in Input) []methodcheck.Finding {
	if in.Model == nil {
		return nil
	}
	var out []methodcheck.Finding
	out = append(out, opCountFindings(in.Model)...)
	out = append(out, facetJoinFindings(in.Model)...)
	out = append(out, deadOpFindings(in.Model)...)
	return out
}

// opCountFindings applies the App-C operation-count metric per contract.
func opCountFindings(model *projectmodel.Model) []methodcheck.Finding {
	var out []methodcheck.Finding
	for i, key := range sortedContractKeys(model) {
		c := model.Contracts[key]
		if c.Doc == nil {
			continue
		}
		n := len(c.Doc.Interface.Operations)
		switch {
		case n >= 20:
			out = append(out, finding(RuleContractOpReject, methodcheck.SeverityError, i, "contract "+key,
				fmt.Sprintf("contract %q has %d operations — App-C rejects a contract at ≥20 ops (a god interface); split it by cohesion", key, n)))
		case n > 12:
			out = append(out, finding(RuleContractOpMax, methodcheck.SeverityWarning, i, "contract "+key,
				fmt.Sprintf("contract %q has %d operations — past the App-C max of 12 (sweet spot 3–5); factor down or sideways", key, n)))
		}
	}
	return out
}

// facetJoinFindings promotes the ratified family-D facet doctrine to a live
// finding. A contract whose key does not join a component directly is valid only
// as a FACET: its `component` field must resolve to a component, and the facet must
// declare that component's layer. A contract that resolves to nothing is a fossil.
func facetJoinFindings(model *projectmodel.Model) []methodcheck.Finding {
	if model.System == nil {
		return nil
	}
	var out []methodcheck.Finding
	for i, key := range sortedContractKeys(model) {
		c := model.Contracts[key]
		if _, ok := model.System.ComponentByContractKey(key); ok {
			continue // direct component contract
		}
		owner, ok := model.System.ComponentByContractKey(c.Component)
		if !ok {
			out = append(out, finding(RuleContractFacet, methodcheck.SeverityError, i, "contract "+key,
				fmt.Sprintf("service contract %q resolves to no component: its key is not a component contract key and its component field %q names no component — a stale/fossil entry", key, c.Component)))
			continue
		}
		if !strings.EqualFold(c.Layer, owner.Layer) {
			out = append(out, finding(RuleContractFacet, methodcheck.SeverityError, i, "contract "+key,
				fmt.Sprintf("contract facet %q declares layer %q but its owning component %q is layer %q — a facet must share its owner's layer", key, c.Layer, owner.ID, owner.Layer)))
		}
	}
	return out
}

// deadOpFindings detects the D1-D4 duplicate class: one operation name published by
// two DIFFERENT contracts that share an owning `component` (a facet group). The
// facets share one Go method set, so a duplicated op is worth review. Severity is
// signature-sensitive (architect ruling 2026-07-22):
//
//   - same NAME, DIFFERING param signature → SeverityWarning. This is a name
//     COLLISION, not proof of a dead op: a distinct-signature variant (e.g. a
//     cred-threaded git-substrate read alongside the plain read) can legitimately
//     coexist, and deciding one is redundant needs LIVENESS evidence (call sites),
//     not the name alone. (The reconciled state's ReadProject case — base {projectID}
//     vs facet {projectID, cred} — was exactly this: prunable only after verifying
//     both hand call sites passed an empty credential and no invoker needed the facet
//     read. The rule flags it for that judgment; it does not block on it.)
//   - same NAME, SAME param signature → SeverityError. An exact duplicate carries no
//     signature difference to justify coexistence; the facets share one method set,
//     so it is unambiguous documentation drift to collapse to one facet.
//
// (The tool-naming ruling prefixes generated tool names by contract key, so a
// same-name facet op no longer collides at the MCP surface; this rule governs the
// CONTRACT corpus, where the shared method set still makes an exact dup drift.)
func deadOpFindings(model *projectmodel.Model) []methodcheck.Finding {
	// group contract keys by owner component.
	groups := map[string][]string{}
	for _, key := range sortedContractKeys(model) {
		c := model.Contracts[key]
		if c.Component == "" {
			continue
		}
		groups[c.Component] = append(groups[c.Component], key)
	}

	var out []methodcheck.Finding
	ordinal := 0
	owners := make([]string, 0, len(groups))
	for owner := range groups {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		keys := groups[owner]
		if len(keys) < 2 {
			continue // not a facet group
		}
		// op name -> the contracts publishing it and the signatures they publish.
		type pub struct{ contract, sig string }
		opPubs := map[string][]pub{}
		for _, key := range keys {
			c := model.Contracts[key]
			if c.Doc == nil {
				continue
			}
			for _, op := range c.Doc.Interface.Operations {
				opPubs[op.Name] = append(opPubs[op.Name], pub{key, paramSignature(op)})
			}
		}
		opNames := make([]string, 0, len(opPubs))
		for name := range opPubs {
			opNames = append(opNames, name)
		}
		sort.Strings(opNames)
		for _, name := range opNames {
			pubs := opPubs[name]
			contractSet := map[string]bool{}
			sigSet := map[string]bool{}
			for _, p := range pubs {
				contractSet[p.contract] = true
				sigSet[p.sig] = true
			}
			if len(contractSet) < 2 {
				continue // published by only one contract of the group
			}
			publishers := make([]string, 0, len(contractSet))
			for k := range contractSet {
				publishers = append(publishers, k)
			}
			sort.Strings(publishers)
			if len(sigSet) == 1 {
				out = append(out, finding(RuleContractDeadOp, methodcheck.SeverityError, ordinal, "facet "+owner,
					fmt.Sprintf("operation %q is published with the SAME signature by %d contracts of the %q facet group (%s) — an exact duplicate; the facets share one Go method set, so collapse it to a single facet", name, len(contractSet), owner, strings.Join(publishers, ", "))))
			} else {
				out = append(out, finding(RuleContractDeadOp, methodcheck.SeverityWarning, ordinal, "facet "+owner,
					fmt.Sprintf("operation %q is published by %d contracts of the %q facet group (%s) with DIFFERING param signatures — a name collision, not a proven dead op; a distinct-signature variant may legitimately coexist, so verify call-site liveness before pruning either", name, len(contractSet), owner, strings.Join(publishers, ", "))))
			}
			ordinal++
		}
	}
	return out
}

// paramSignature renders an operation's param signature — the ordered param names —
// as the key the dead-op rule compares to tell an exact duplicate (Error) from a
// name collision with a distinct shape (Warning).
func paramSignature(op projectmodel.Operation) string {
	names := make([]string, len(op.Params))
	for i, p := range op.Params {
		names[i] = p.Name
	}
	return strings.Join(names, "|")
}

// sortedContractKeys returns the contract keys in deterministic order.
func sortedContractKeys(model *projectmodel.Model) []string {
	keys := make([]string, 0, len(model.Contracts))
	for k := range model.Contracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ==========================================================================
// rules_coverage — folded here by the Engine file-layout standard (one handwritten
// impl file per Engine component). Contents verbatim; imports merged above.
// ==========================================================================

// rules_coverage.go — the cross-artifact JOIN rules: use-case→dynamic-view,
// volatility→component (encapsulation), volatility→requirement (traces), and
// operational-concept→objective (objectiveLinks + legacy justification). A
// dangling reference is a real defect (SeverityError); a coverage gap is
// advisory (SeverityWarning at most — it flags an orphaned business need
// without blocking the gate).
func coverageFindings(in Input) []methodcheck.Finding {
	var out []methodcheck.Finding
	out = append(out, useCaseDynamicViewFindings(in)...)
	out = append(out, useCaseEssenceFindings(in)...)
	out = append(out, volatilityEncapsulationFindings(in)...)
	out = append(out, componentVolatilityDanglingFindings(in)...)
	out = append(out, componentNoVolatilityFindings(in)...)
	out = append(out, volatilityTraceFindings(in)...)
	out = append(out, objectiveFindings(in)...)
	return out
}

// useCaseDynamicViewFindings — Req 1e: every CORE use case has a dynamic view. A
// core use case with no chain is unarchitected. SeverityError.
func useCaseDynamicViewFindings(in Input) []methodcheck.Finding {
	viewed := map[string]bool{}
	for _, dv := range in.Slots.DynamicViews {
		if dv.UseCaseID != "" {
			viewed[dv.UseCaseID] = true
		}
	}
	var out []methodcheck.Finding
	for i, uc := range in.Slots.CoreUseCases {
		if uc.Classification != "core" || uc.ID == "" {
			continue
		}
		if !viewed[uc.ID] {
			out = append(out, finding(RuleCovUCDynamic, methodcheck.SeverityError, i, "useCase "+uc.ID,
				fmt.Sprintf("core use case %q has no dynamic view — every core use case must be realized by an architecture call chain (Req 1e)", uc.ID)))
		}
	}
	return out
}

// useCaseEssenceFindings — DH-UC-ESSENCE-MISSING: every decision classified core
// must carry a non-empty essenceRationale — the essence-of-the-business argument
// for WHY it is core (ch. 3: core use cases are the essence of the business; the
// field is the symmetric twin of the nonCore rejectionReason). Nil, empty, and
// whitespace-only all count as missing. Skipped while the coreUseCases slot is
// empty/uncommitted — nothing classified yet. SeverityWarning (advisory: the gap
// is an unargued classification, not a broken join).
func useCaseEssenceFindings(in Input) []methodcheck.Finding {
	if len(in.Slots.CoreUseCases) == 0 {
		return nil
	}
	var out []methodcheck.Finding
	for i, uc := range in.Slots.CoreUseCases {
		if uc.Classification != "core" {
			continue
		}
		if uc.EssenceRationale == nil || strings.TrimSpace(*uc.EssenceRationale) == "" {
			out = append(out, finding(RuleUCEssenceMissing, methodcheck.SeverityWarning, i, "useCase "+uc.ID,
				fmt.Sprintf("core use case %q carries no essenceRationale — every core classification must argue WHY the use case is the essence of the business (the symmetric twin of the nonCore rejectionReason)", uc.ID)))
		}
	}
	return out
}

// volatilityEncapsulationFindings — §mission: every volatility is encapsulated by
// a component. TWO regimes:
//
// TYPED JOIN (authoritative): the systemDesign components carry the typed
// encapsulatesVolatilities lists (each entry the EXACT name of a committed
// volatility — the edge lives on the component side because volatilities commit
// before the architecture exists). The moment ANY component carries a non-empty
// list, the typed join governs the WHOLE system: a volatility owned by zero typed
// lists is a real gap (SeverityError, DH-VOL-ENCAP-MISSING). Multiple typed owners
// are LEGITIMATE — the doctrine allows a shared component/facet group — so no
// ambiguity finding is minted for typed joins.
//
// FALLBACK (older states with no typed field anywhere): the interim name-in-blurb
// substring join over the component encapsulates text (the same interim posture as
// the volatility screen). A volatility no component mentions is the same real gap
// (SeverityError); a name that matches several blurbs is the known imprecision of
// the substring join over facet groups (SeverityInfo, DH-VOL-ENCAP-AMBIGUOUS)
// rather than a false "duplicate owner".
func volatilityEncapsulationFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	if in.Slots.typedEncapsulationActive() {
		return typedEncapsulationFindings(in)
	}
	comps := in.Model.System.Components
	var out []methodcheck.Finding
	for i, v := range in.Slots.Volatilities {
		if v.Name == "" {
			continue
		}
		var matches []string
		for _, c := range comps {
			// SystemComponent carries no encapsulates blurb in the projectmodel
			// slice; the interim join reads it from the raw slot (parse.go
			// encapsulatesBlurbs).
			if blurb, ok := in.Slots.encapsulatesBlurbs[c.ID]; ok && containsFold(blurb, v.Name) {
				matches = append(matches, c.ID)
			}
		}
		switch {
		case len(matches) == 0:
			out = append(out, finding(RuleVolEncapMissing, methodcheck.SeverityError, i, "volatility "+v.Name,
				fmt.Sprintf("volatility %q is encapsulated by no component — every volatility must be owned by exactly one component (or a ratified facet group)", v.Name)))
		case len(matches) > 1:
			sort.Strings(matches)
			out = append(out, finding(RuleVolEncapAmbig, methodcheck.SeverityInfo, i, "volatility "+v.Name,
				fmt.Sprintf("volatility %q matches %d component blurbs (%v) under the interim name-in-blurb join — resolve to a single owner or a ratified facet group via the typed encapsulatesVolatilities field", v.Name, len(matches), matches)))
		}
	}
	return out
}

// typedEncapsulationFindings is the authoritative regime of
// volatilityEncapsulationFindings: exact-name ownership over the union of every
// component's typed list. Shared owners are legitimate; only a volatility NO typed
// list names is a gap.
func typedEncapsulationFindings(in Input) []methodcheck.Finding {
	owned := map[string]bool{}
	for _, list := range in.Slots.encapsulatesVolatilities {
		for _, name := range list {
			owned[name] = true
		}
	}
	var out []methodcheck.Finding
	for i, v := range in.Slots.Volatilities {
		if v.Name == "" || owned[v.Name] {
			continue
		}
		out = append(out, finding(RuleVolEncapMissing, methodcheck.SeverityError, i, "volatility "+v.Name,
			fmt.Sprintf("volatility %q appears in no component's encapsulatesVolatilities — every volatility must be owned by at least one component (a shared component/facet group is legitimate)", v.Name)))
	}
	return out
}

// componentVolatilityDanglingFindings — DH-COMP-VOL-DANGLING: every entry of a
// component's typed encapsulatesVolatilities list must EXACTLY match a committed
// volatility name; a miss is a dangling reference (stale residue of a volatility
// rename/removal). Skipped while the volatilities slot is empty/uncommitted —
// nothing to join against yet (the same posture as the other joins). SeverityError.
func componentVolatilityDanglingFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	names := in.Slots.volatilityNames()
	if len(names) == 0 {
		return nil
	}
	var out []methodcheck.Finding
	for i, c := range in.Model.System.Components {
		for _, ref := range in.Slots.encapsulatesVolatilities[c.ID] {
			if !names[ref] {
				out = append(out, finding(RuleCompVolDangling, methodcheck.SeverityError, i, "component "+c.ID,
					fmt.Sprintf("component %q lists %q in encapsulatesVolatilities, which is not a committed volatility name — a dangling reference (stale after a volatility rename/removal)", c.ID, ref)))
			}
		}
	}
	return out
}

// componentNoVolatilityFindings — DH-COMP-NO-VOLATILITY: the REVERSE
// anti-functional-decomposition check (Righting Software ch. 2 — functional
// decomposition is the "siren song"): every Manager/Engine/ResourceAccess component
// must encapsulate at least one volatility, because a code-layer component owning no
// volatility is a functional block wearing a Method costume. Clients, Resources, and
// Utilities are exempt (they encapsulate entry channels, storage technology, and
// cross-cutting concerns rather than product volatility). Ownership is the typed
// list when the typed join is active; under the interim fallback, a blurb mentioning
// ≥1 committed volatility name (containsFold) counts. Skipped entirely while the
// volatilities slot is empty/uncommitted. SeverityWarning.
func componentNoVolatilityFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	if len(in.Slots.volatilityNames()) == 0 {
		return nil
	}
	typed := in.Slots.typedEncapsulationActive()
	var out []methodcheck.Finding
	for i, c := range in.Model.System.Components {
		switch c.Layer {
		case "manager", "engine", "resourceAccess":
			// the layers bound to encapsulate business/product volatility
		default:
			continue
		}
		owns := false
		if typed {
			owns = len(in.Slots.encapsulatesVolatilities[c.ID]) > 0
		} else {
			blurb := in.Slots.encapsulatesBlurbs[c.ID]
			for _, v := range in.Slots.Volatilities {
				if v.Name != "" && containsFold(blurb, v.Name) {
					owns = true
					break
				}
			}
		}
		if !owns {
			out = append(out, finding(RuleCompNoVolatility, methodcheck.SeverityWarning, i, "component "+c.ID,
				fmt.Sprintf("%s component %q encapsulates no volatility — every Manager/Engine/ResourceAccess must encapsulate at least one area of volatility (Righting Software ch. 2: a component owning none is functional decomposition, the siren song)", c.Layer, c.ID)))
		}
	}
	return out
}

// volatilityTraceFindings — every volatility trace resolves to a requirement id.
// A dangling trace is a stale reference left by a requirement renumber/removal.
// SeverityError.
func volatilityTraceFindings(in Input) []methodcheck.Finding {
	reqIDs := in.Slots.requirementIDs()
	if len(reqIDs) == 0 {
		return nil // no requirements committed to join against
	}
	var out []methodcheck.Finding
	for i, v := range in.Slots.Volatilities {
		for _, tr := range v.Traces {
			if !reqIDs[tr] {
				out = append(out, finding(RuleVolTrace, methodcheck.SeverityError, i, "volatility "+v.Name,
					fmt.Sprintf("volatility %q traces to %q, which is not a committed requirement id — a dangling trace (stale reference after a requirement renumber/removal)", v.Name, tr)))
			}
		}
	}
	return out
}

// objectiveFindings — the two objective-reference joins (ch. 5: bidirectional
// objective↔architecture traceability), over BOTH reference sources — the typed
// post-reshape home DeploymentOperationsModel.objectiveLinks (knob name →
// objective numbers) and the legacy decisions[].justifyingObjective older states
// still carry:
//
//	DH-OBJ-RESOLVE  (Error)   every objective reference resolves to a live objective
//	DH-OBJ-COVERAGE (Warning) every objective is referenced by ≥1 link/justification
//
// Coverage is Warning, not Error: with objectiveLinks the reference model has its
// post-reshape home, so an unreferenced objective is a real orphaned-business-need
// signal (ch. 5: "the alternatives are pointless designs and orphaned business
// needs") — but it stays advisory (non-blocking), a subject for the review rather
// than an authoring-gate failure.
func objectiveFindings(in Input) []methodcheck.Finding {
	objNums := in.Slots.objectiveNumbers()
	if len(objNums) == 0 {
		return nil
	}
	var out []methodcheck.Finding

	referenced := map[int]bool{}
	for i, n := range in.Slots.justifyingObjectives {
		referenced[n] = true
		if !objNums[n] {
			out = append(out, finding(RuleObjResolve, methodcheck.SeverityError, i, "justifyingObjective",
				fmt.Sprintf("a decision cites justifyingObjective %d, which is not a declared objective number — a dangling objective reference", n)))
		}
	}
	for i, l := range in.Slots.objectiveLinkRefs {
		referenced[l.Number] = true
		if !objNums[l.Number] {
			out = append(out, finding(RuleObjResolve, methodcheck.SeverityError, i, "objectiveLinks "+l.Knob,
				fmt.Sprintf("operational knob %q links objective %d, which is not a declared objective number — a dangling objective reference", l.Knob, l.Number)))
		}
	}

	nums := make([]int, 0, len(objNums))
	for n := range objNums {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var uncovered []int
	for _, n := range nums {
		if !referenced[n] {
			uncovered = append(uncovered, n)
		}
	}
	if len(uncovered) > 0 {
		out = append(out, finding(RuleObjCoverage, methodcheck.SeverityWarning, 0, "objectives",
			fmt.Sprintf("objective(s) %v are referenced by no objectiveLinks entry or justifyingObjective — an unreferenced objective is an orphaned business need (ch. 5); link every objective to at least one operational choice (advisory, non-blocking)", uncovered)))
	}
	return out
}

// ==========================================================================
// rules_graph — folded here by the Engine file-layout standard (one handwritten
// impl file per Engine component). Contents verbatim; imports merged above.
// ==========================================================================

// rules_graph.go — the closed-architecture directives (§4c) and guidelines
// (§5, §6c/§6d) over the systemDesign relationship edges. Directives that a legal
// Method design can never violate are SeverityError (they block the authoring
// gate); the reachability/coverage guidelines are SeverityWarning.
//
// Utilities are cross-cutting: an edge touching a utility is exempt from the
// DIRECTION rules (up/sideways/entry/engine-IO) — only the utility-reachability
// guideline concerns them.
func graphFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	sys := in.Model.System
	idx := in.componentIndex()

	var out []methodcheck.Finding
	for i, r := range sys.Relationships {
		from, okF := idx[r.From]
		to, okT := idx[r.To]
		if !okF || !okT {
			// projectmodel.Load already rejects unresolved endpoints before we run;
			// this guard is belt-and-suspenders for a directly-constructed Input.
			continue
		}
		fk, tk := from.Kind, to.Kind
		section := r.From + "→" + r.To

		// §4c.i — no up-calls. An edge whose target sits at a HIGHER layer than its
		// source inverts the closed architecture. Utilities exempt.
		if isDirectional(fk, tk) && kindRank(tk) < kindRank(fk) {
			out = append(out, finding(RuleGraphUpcall, methodcheck.SeverityError, i, section,
				fmt.Sprintf("up-call: %s (%s) calls %s (%s) — a higher layer; the closed architecture only calls DOWN the layers", r.From, fk, r.To, tk)))
		}
		// §4c.ii / §5d — sideways calls only when queued (M→M is the live case).
		if isDirectional(fk, tk) && kindRank(tk) == kindRank(fk) && r.Mode != "queued" {
			out = append(out, finding(RuleGraphSidewaysSync, methodcheck.SeverityError, i, section,
				fmt.Sprintf("sideways sync call: %s→%s are the same layer (%s) but the call is %q — same-layer calls (e.g. Manager→Manager) are permitted only when queued", r.From, r.To, fk, r.Mode)))
		}
		// §4c.iii — a Client may enter only at a Manager (or a cross-cutting utility).
		if fk == "client" && tk != "manager" && tk != "utility" {
			out = append(out, finding(RuleGraphClientEntry, methodcheck.SeverityError, i, section,
				fmt.Sprintf("client %s calls %s (%s) — a Client may enter the system only at a Manager, never at %s directly", r.From, r.To, tk, tk)))
		}
		// §6c / §6d — Engines and ResourceAccess receive no QUEUED calls (they are
		// synchronous, request/response building blocks).
		if r.Mode == "queued" && (tk == "engine" || tk == "resourceAccess") {
			out = append(out, finding(RuleGraphQueuedTarget, methodcheck.SeverityError, i, section,
				fmt.Sprintf("queued call into a %s (%s): Engines and ResourceAccess are synchronous — only Managers receive queued calls", tk, r.To)))
		}
		// §5b — Engines do no IO: no Engine→ResourceAccess / Engine→Resource edge
		// (Managers own all IO orchestration). SeverityWarning: some Method dialects
		// permit Engine→ResourceAccess, so this is a guideline for the archistrator
		// "Managers own IO" posture rather than a hard directive.
		if fk == "engine" && (tk == "resourceAccess" || tk == "resource") {
			out = append(out, finding(RuleGraphEngineIO, methodcheck.SeverityWarning, i, section,
				fmt.Sprintf("engine %s calls %s (%s): Engines are pure computation in this architecture — route IO through a Manager", r.From, r.To, tk)))
		}
	}

	out = append(out, utilityReachabilityFindings(in)...)
	out = append(out, managerOrchestrationFindings(in)...)
	return out
}

// utilityReachabilityFindings — §5a: every declared utility must be reachable
// (have ≥1 inbound edge); an unreferenced utility is dead architecture.
func utilityReachabilityFindings(in Input) []methodcheck.Finding {
	sys := in.Model.System
	inbound := map[string]bool{}
	for _, r := range sys.Relationships {
		inbound[r.To] = true
	}
	var out []methodcheck.Finding
	for i, u := range componentsByKind(sys, "utility") {
		if !inbound[u.ID] {
			out = append(out, finding(RuleGraphUtilReach, methodcheck.SeverityWarning, i, "utility "+u.ID,
				fmt.Sprintf("utility %q has no inbound edge — it is unreachable/dead in the architecture, or the caller relationships are missing", u.ID)))
		}
	}
	return out
}

// managerOrchestrationFindings — §5: a Manager must ORCHESTRATE something: it needs
// at least one outbound edge to an Engine OR a ResourceAccess. A Manager with an
// Engine (computation orchestration) or with ResourceAccess (IO orchestration) — or
// both — is a legitimate Manager; The Method does NOT require every Manager to call
// an Engine (a Manager sequencing several ResourceAccess components is orchestrating
// IO, which is exactly its job). The real defect this catches is a Manager that
// orchestrates NOTHING — no Engine and no ResourceAccess edge — i.e. an empty
// component (missing all its downstream edges, or a mislabeled leaf). SeverityWarning.
func managerOrchestrationFindings(in Input) []methodcheck.Finding {
	sys := in.Model.System
	idx := in.componentIndex()
	orchestrates := map[string]bool{}
	for _, r := range sys.Relationships {
		from, okF := idx[r.From]
		to, okT := idx[r.To]
		if okF && okT && from.Kind == "manager" && (to.Kind == "engine" || to.Kind == "resourceAccess") {
			orchestrates[from.ID] = true
		}
	}
	var out []methodcheck.Finding
	for i, m := range componentsByKind(sys, "manager") {
		if !orchestrates[m.ID] {
			out = append(out, finding(RuleGraphMgrEmpty, methodcheck.SeverityWarning, i, "manager "+m.ID,
				fmt.Sprintf("Manager %q has no outbound edge to an Engine or ResourceAccess — a Manager must orchestrate something; one with no downstream edges is empty (missing its edges, or a mislabeled component)", m.ID)))
		}
	}
	return out
}

// ==========================================================================
// util — folded here by the Engine file-layout standard (one handwritten
// impl file per Engine component). Contents verbatim; imports merged above.
// ==========================================================================

// sortedKeys returns the map's keys sorted, joined for a stable message rendering.
func sortedKeys(m map[string]bool) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ", ")
}

// containsFold reports whether haystack contains needle, case-insensitively. Used
// by the interim name-in-blurb volatility-encapsulation fallback join (the regime
// for older states in which no component carries the typed encapsulatesVolatilities
// list).
func containsFold(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
