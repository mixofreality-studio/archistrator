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
// LAYER: this is a Method UTILITY (internal/utility/designhealth) — cross-cutting
// diagnostics infrastructure with no service-contract port, no Temporal, and no
// upward dependency on any business layer (it imports zero manager/engine/
// resourceaccess/client package). That is the honest classification for a pure,
// port-less rule engine that the authoring gate, the CI harness, and the future
// getDesignHealth read-model op all consume as a downward edge; it is NOT a Method
// component (it encapsulates no product volatility), so it lives on the Utility bar
// rather than as an Engine, exactly as Security/Logging/Diagnostics do.
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
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

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

// EvaluateRaw is the single entry point the three call sites share. It parses raw
// (the committed or drafted project.json bytes) into an Input and evaluates it. A
// document that will not parse under the published projectmodel codegen parser is
// a STRUCTURAL failure owned by the framework gate (methodcheck.ValidateProjectJSON
// runs on the same bytes at every seam); design-health stays silent rather than
// double-reporting it, so the append at the seam is a no-op in that case.
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
