// cmd/schemagen reflects each component's contract surface into a single
// self-contained contract document per component.
//
// DEPRECATED / BOOTSTRAP-ONLY: schemagen is NO LONGER the authority. project.json
// `.serviceContracts` now OWNS each built component's contract document, and
// cmd/modelgen generates `contract.gen.go` straight from it. schemagen is kept
// only as a one-shot RE-SEED tool — to capture a brand-new component's
// hand-written surface as a contract document (which is then folded into
// project.json) when first bootstrapping it. It writes the legacy per-component
// `contract.schema.json` files, which are otherwise retired. Do NOT run it in the
// steady-state regen path (`make gen`); it is not part of the source of truth.
//
// It captures TWO things per component:
//   - the I/O MODEL types → JSON Schema `$defs` (data shapes).
//   - the component INTERFACE → an `interface` descriptor (the RPC surface that
//     JSON Schema can't express: operations, params, result, error).
//
// Design rules (founder direction):
//   - ONE contract document per component, colocated as `contract.schema.json`.
//   - Defs may be SHARED WITHIN a component (`$ref` into `#/$defs`) but NEVER
//     BETWEEN components — each document carries only its own component's types.
//   - Param names (which Go reflection does not expose) are recovered from the
//     interface's source via go/ast.
//
// Usage:
//
//	cd server && go run ./cmd/schemagen          # write every registered component
//	cd server && go run ./cmd/schemagen review   # write only the named component(s)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"

	"github.com/mixofreality-studio/archistrator/server/cmd/internal/codegen"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/settlement"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	mgrprojectdesign "github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	mgrsettlement "github.com/mixofreality-studio/archistrator/server/internal/manager/settlement"
	mgrsystemdesign "github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/worker"
)

// component declares one component's contract surface to capture.
type component struct {
	name      string
	dir       string
	models    []any         // zero-value instances of the I/O model types
	ifaceName string        // the interface's Go type name (for AST param-name lookup)
	iface     reflect.Type  // the interface type, reflected for operations
	sumTypes  []sumTypeDecl // sealed-interface (discriminated-union) declarations
	// bindByName lists OWN-PACKAGE types that this component's contract references
	// but does NOT regenerate: they bind to their bare Go name via {"x-go-type":
	// "<TypeName>"} (no x-go-import — same package) in BOTH struct-field and
	// interface param/result position, and emit NO `$defs`. The hand-written type
	// stays the source of truth. Entries are zero VALUES for concrete types
	// (projectstate.System{}) or a typed-nil POINTER for an interface type
	// ((*projectstate.ArtifactModel)(nil)) — the builder derefs a pointer to key the
	// map by the element (interface/struct) type.
	//
	// This is how the projectstate RA (the byte-identical persistence layer) keeps
	// the handful of types the generator cannot reproduce faithfully hand-written —
	// the value+omitempty domain structs (modelgen would pointerize the optional
	// value fields, changing the Go API and breaking consumers) and the ArtifactModel
	// sealed sum (its Solution variant's Kind() is a DYNAMIC field read, which
	// emitSumType's static per-variant Kind() cannot express) — while STILL emitting
	// $defs for the ~100 pure-data models the contract does regenerate. A strict
	// no-op when empty (every other registered component leaves it unset).
	bindByName []any
}

// sumTypeDecl registers a sealed-interface / discriminated-union (sum) type to
// reflect into a `$def` carrying a JSON Schema `oneOf` (the variant `$ref` list)
// plus the `x-go-sumtype` codegen descriptor. The sealed interface, its variants,
// the discriminator method (called via reflection on each variant to recover its
// kind STRING), and the marker method are captured so modelgen can regenerate the
// interface, the per-variant marker + discriminator methods, and the envelope
// codec — byte-identical to the existing hand-written wire form.
type sumTypeDecl struct {
	// name is the sealed interface's Go type name AND the name of the emitted sum
	// `$def` (e.g. "ArtifactModel").
	name string
	// iface is the sealed interface type, reflected to confirm each variant
	// implements it.
	iface reflect.Type
	// marker is the unexported marker method name that seals the sum (e.g.
	// "isArtifactModel").
	marker string
	// discriminatorKey is the envelope JSON key carrying the kind string (e.g.
	// "kind"). The envelope is {discriminatorKey: <kindStr>, "model": <variant>}.
	discriminatorKey string
	// kindEnum is the Go type name of the discriminator value returned by each
	// variant's discriminator method (e.g. "ArtifactKind").
	kindEnum string
	// variants are zero-value POINTERS to the concrete variant structs, in wire
	// order (e.g. &MissionStatement{}). Each must implement iface.
	variants []any
	// kindString maps a variant's discriminator value (the result of calling its
	// discriminator method via reflection, as a comparable any) to the wire kind
	// STRING. This is the SINGLE source the generated envelope's kind value derives
	// from, so the emitted codec is byte-identical to the hand-written one.
	kindString func(any) string
	// kindConst maps a variant's discriminator value to the Go const naming it
	// (e.g. KindMission → "KindMission"), emitted as the variant's Kind() return.
	kindConst func(any) string
	// discriminatorMethod is the name of the method called via reflection on each
	// variant to recover its discriminator value (e.g. "Kind").
	discriminatorMethod string
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

// contextCarried are the cross-cutting types delivered by the per-layer call
// Context (the first param the generator prepends to every method), NOT data.
// schemagen strips any param of these types so the data schema stays pure and the
// generator can re-inject the single layer Context. Includes the layer Context
// types themselves so re-reflecting an already-generated interface is idempotent.
var contextCarried = map[reflect.Type]bool{
	reflect.TypeOf((*context.Context)(nil)).Elem(): true,
	reflect.TypeOf(security.SecurityPrincipal{}):   true,
	reflect.TypeOf(fwra.IdempotencyKey("")):        true,
	reflect.TypeOf(fweng.Context{}):                true,
	reflect.TypeOf(fwra.Context{}):                 true,
	reflect.TypeOf(fwm.Context{}):                  true,
}

// layerFromDir maps a component dir to its Method layer (selects the call Context).
func layerFromDir(dir string) string {
	switch {
	case strings.Contains(dir, "/engine/"):
		return "engine"
	case strings.Contains(dir, "/resourceaccess/"):
		return "resourceaccess"
	case strings.Contains(dir, "/manager/"):
		return "manager"
	case strings.Contains(dir, "/client/"):
		return "client"
	}
	return ""
}

// wellKnownByType maps foundational non-domain types (stdlib/uuid) to a schema
// carrying a portable JSON shape (e.g. string+format) AND an `x-go-type` /
// `x-go-import` binding so modelgen regenerates the exact Go type. These are NOT
// inlined as $defs — they bind directly to their canonical Go type (and the
// generated file imports it). Unlike domain types, importing `time`/`uuid` in
// generated code is fine; they're foundational, not cross-component contract defs.
var wellKnownByType = map[reflect.Type]*jsonschema.Schema{
	reflect.TypeOf(time.Time{}): {
		Type: "string", Format: "date-time",
		Extra: map[string]any{"x-go-type": "time.Time", "x-go-import": "time"},
	},
	reflect.TypeOf(time.Duration(0)): {
		Type:  "integer",
		Extra: map[string]any{"x-go-type": "time.Duration", "x-go-import": "time"},
	},
	reflect.TypeOf(uuid.UUID{}): {
		Type: "string", Format: "uuid",
		Extra: map[string]any{"x-go-type": "uuid.UUID", "x-go-import": "github.com/google/uuid"},
	},
	// []byte → base64 string on the wire (matches Go's encoding/json).
	reflect.TypeOf([]byte(nil)): {
		Type: "string", ContentEncoding: "base64",
		Extra: map[string]any{"x-go-type": "[]byte"},
	},
	// json.RawMessage → arbitrary embedded JSON (no fixed shape).
	reflect.TypeOf(json.RawMessage(nil)): {
		Extra: map[string]any{"x-go-type": "json.RawMessage", "x-go-import": "encoding/json"},
	},
}

// modulePrefix maps a type's PkgPath back to a dir relative to the server module
// root (where the codegen tools run), so enum const sets can be captured from the
// defining package of an inlined external domain type.
const modulePrefix = "github.com/mixofreality-studio/archistrator/server/"

// externalDirs returns the module-relative dirs of any model types defined outside
// the component's own package (e.g. projectstate domain types), deduped.
func externalDirs(models []any) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, m := range models {
		pkg := reflect.TypeOf(m).PkgPath()
		if d := strings.TrimPrefix(pkg, modulePrefix); d != pkg && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// registry is the strangler work-list, leaf-first. engine/review is the first
// leaf (pure, zero importers, nothing persisted — see review.go package doc).
var registry = []component{
	{
		name: "review",
		dir:  "internal/engine/review",
		models: []any{
			review.ReviewChange{},
			review.Reviewer{},
			review.ReviewSet{},
		},
		ifaceName: "ReviewEngine",
		iface:     reflect.TypeOf((*review.ReviewEngine)(nil)).Elem(),
	},
	{
		name: "handoff",
		dir:  "internal/engine/handoff",
		models: []any{
			handoff.ConstructionActivity{},
			handoff.HandOffPolicy{},
			handoff.WorkerClass(0),
			handoff.ActivityKind(0),
		},
		ifaceName: "HandOffEngine",
		iface:     reflect.TypeOf((*handoff.HandOffEngine)(nil)).Elem(),
	},
	{
		name: "intervention",
		dir:  "internal/engine/intervention",
		models: []any{
			// Identifier newtypes (named strings; no const block).
			intervention.ProjectID(""),
			intervention.ActivityID(""),
			intervention.OperatedAppID(""),
			intervention.CustomerID(""),
			intervention.CycleID(""),
			intervention.PipelineRef(""),
			intervention.NotifyTarget(""),
			// Policy + I/O structs.
			intervention.InterventionPolicy{},
			intervention.ConstructionVariance{},
			intervention.HealthChange{},
			intervention.SettlementFailure{},
			intervention.PauseRequestContext{},
			intervention.PausePlan{},
			// Enums (one zero value each).
			intervention.InterventionMode(0),
			intervention.SLATier(0),
			intervention.VarianceKind(0),
			intervention.Severity(0),
			intervention.HealthStatus(0),
			intervention.SLOStatus(0),
			intervention.SettlementFailureKind(0),
			intervention.VarianceDirective(0),
			intervention.HealthDirective(0),
			intervention.SettlementFailureDirective(0),
		},
		ifaceName: "InterventionEngine",
		iface:     reflect.TypeOf((*intervention.InterventionEngine)(nil)).Elem(),
	},
	{
		name: "settlement",
		dir:  "internal/engine/settlement",
		models: []any{
			// Own I/O structs.
			settlement.CycleRevenue{},
			settlement.CycleUsage{},
			settlement.SettlementResult{},
			settlement.ReSettlementInput{},
			settlement.Projection{},
			settlement.ProjectOption{},
			// Own enums + scalar.
			settlement.RoutingDirective(0),
			settlement.OptionID(""),
			// Domain types redefined as this component's OWN defs (Option B full
			// encapsulation): Money/SettlementTerms structs plus the settlement-terms
			// enums they carry. These MIRROR projectstate (the canonical home owned by
			// projectStateAccess); the projectDesignManager converts at the call
			// boundary. They are registered as the component's own settlement.* types
			// (post-seed steady state) so nested field refs resolve and the contract
			// regenerates identically — idempotent (the enum const blocks are captured
			// from contract.gen.go in this dir, not from projectstate).
			settlement.Money{},
			settlement.SettlementTerms{},
			settlement.RevenueShareKind(0),
			settlement.ComputeCostKind(0),
			settlement.ScheduleKind(0),
		},
		ifaceName: "SettlementEngine",
		iface:     reflect.TypeOf((*settlement.SettlementEngine)(nil)).Elem(),
	},
	{
		name: "billing",
		dir:  "internal/engine/billing",
		models: []any{
			// Own I/O structs (the full transitive closure of BillingEngine).
			billing.PeriodUsage{},
			billing.ServicePricing{},
			billing.ProjectOption{},
			billing.ServiceInvoice{},
			billing.HostingRate{},
			billing.ServiceCostProjection{},
			// Own enum + named scalars.
			billing.ServicePricingKind(0),
			billing.CustomerID(""),
			billing.PeriodID(""),
			// Domain types redefined as this component's OWN defs (Option B full
			// encapsulation): Money struct + OptionID scalar. They MIRROR
			// projectstate (the canonical home owned by projectStateAccess); the
			// calling Managers convert at the call boundary. Registered as the
			// component's own billing.* types (post-seed steady state) so nested
			// field refs resolve and the contract regenerates identically —
			// idempotent (a projectstate.* registration would inline Money to
			// map[string]interface{} on re-run, because the component's own structs
			// reference the generated-local billing.Money, a different reflect.Type).
			billing.Money{},
			billing.OptionID(""),
		},
		ifaceName: "BillingEngine",
		iface:     reflect.TypeOf((*billing.BillingEngine)(nil)).Elem(),
	},
	{
		name: "operationestimation",
		dir:  "internal/engine/operationestimation",
		models: []any{
			// Own output value objects (the full transitive closure of the two ops).
			operationestimation.CostCurvePoint{},
			operationestimation.UsageCostCurve{},
			operationestimation.PayoutShortfallForecast{},
			operationestimation.OperationForecast{},
			operationestimation.ObservedUsage{},
			operationestimation.ScalePoint{},
			operationestimation.WhatIfPoint{},
			operationestimation.WhatIfCurve{},
			operationestimation.CostProjection{},
			// Domain types redefined as this component's OWN defs (Option B full
			// encapsulation): Money/SettlementTerms/UsageAssumption structs + the slim
			// ProjectOption, plus the InfrastructureKind/settlement-terms enums and the
			// OptionID scalar. They MIRROR projectstate (the canonical home owned by
			// projectStateAccess); the projectDesignManager converts at the call
			// boundary. Registered as the component's own operationestimation.* types
			// (post-seed steady state) so nested field refs resolve and the contract
			// regenerates identically — idempotent (a projectstate.* registration would
			// inline Money to map[string]interface{} on re-run, because the component's
			// own structs reference the generated-local operationestimation.Money, a
			// different reflect.Type).
			operationestimation.Money{},
			operationestimation.SettlementTerms{},
			operationestimation.UsageAssumption{},
			operationestimation.ProjectOption{},
			operationestimation.InfrastructureKind(0),
			operationestimation.RevenueShareKind(0),
			operationestimation.ComputeCostKind(0),
			operationestimation.ScheduleKind(0),
			operationestimation.OptionID(""),
		},
		ifaceName: "OperationEstimationEngine",
		iface:     reflect.TypeOf((*operationestimation.OperationEstimationEngine)(nil)).Elem(),
	},
	{
		name: "estimation",
		dir:  "internal/engine/estimation",
		models: []any{
			// Output value objects (owned by this Engine — computation results, not
			// persisted head-state). EstimateForOption → ConstructionEstimate{RiskScore};
			// ComputeNetwork → NetworkSolution{NetworkNode, NetworkMilestoneSolution,
			// NetworkSummary}.
			estimation.ConstructionEstimate{},
			estimation.RiskScore{},
			estimation.NetworkSolution{},
			estimation.NetworkNode{},
			estimation.NetworkMilestoneSolution{},
			estimation.NetworkSummary{},
			// Domain INPUT types redefined as this component's OWN defs (Option B full
			// encapsulation). They MIRROR projectstate (the canonical home owned by
			// projectStateAccess); the projectDesignManager (EstimateForOption) and the
			// projectManager (ComputeNetwork) convert at the call boundary. Registered as
			// the component's OWN estimation.* types (NOT projectstate.*) so nested field
			// refs resolve and the contract regenerates identically — idempotent (a
			// projectstate.* registration would inline WorkerMix.ClassRates' Money value to
			// map[string]interface{} on re-run, because the component's own structs
			// reference the generated-local estimation.Money, a different reflect.Type).
			// Per the settlement/billing/operationestimation precedent ProjectOption /
			// Network / ActivityItem are SLIM (only the fields THIS Engine reads).
			estimation.ProjectOption{},
			estimation.ActivityNetwork{},
			estimation.OptionActivity{},
			estimation.WorkerMix{},
			estimation.Money{},
			estimation.ActivityList{},
			estimation.ActivityItem{},
			estimation.Network{},
			estimation.NetworkDependency{},
			estimation.NetworkMilestone{},
			// Named scalar (no const block — a bare identifier newtype, carried for audit).
			estimation.OptionID(""),
		},
		ifaceName: "EstimationEngine",
		iface:     reflect.TypeOf((*estimation.EstimationEngine)(nil)).Elem(),
	},
	{
		name: "autoscaler",
		dir:  "internal/engine/autoscaler",
		models: []any{
			// Own I/O structs (the full transitive closure of ProposeDesiredState).
			autoscaler.Telemetry{},
			autoscaler.DesiredState{},
			autoscaler.AutoscalerPolicy{},
			autoscaler.DecisionReason{},
			autoscaler.Decision{},
			// Own enums (one zero value each). InfrastructureKind is redefined as
			// this component's OWN type (Option B full encapsulation) mirroring
			// projectstate's values/names, so the generated def imports no
			// projectstate; callers convert at the boundary. NOTE: OperatedAppID is a
			// `= uuid.UUID` alias, so fields of that type bind to uuid.UUID
			// automatically (wellKnownByType) — it is NOT registered separately.
			autoscaler.HealthStatus(0),
			autoscaler.SLOStatus(0),
			autoscaler.AutoscalerMode(0),
			autoscaler.SLATier(0),
			autoscaler.DecisionKind(0),
			autoscaler.ReasonCode(0),
			autoscaler.InfrastructureKind(0),
		},
		ifaceName: "AutoscalerEngine",
		iface:     reflect.TypeOf((*autoscaler.AutoscalerEngine)(nil)).Elem(),
	},
	{
		name: "worker",
		dir:  "internal/resourceaccess/worker",
		models: []any{
			// The full transitive closure of WorkerAccess's OWN contract value types
			// (workerAccess.md §1f–§9f). All defined in this package — full
			// encapsulation: the contract pulls NO external (projectstate/artifact)
			// dep. Tool envelopes carry json.RawMessage (opaque schema/inputs) and
			// GenerateSpec carries []byte (opaque caller context); the generator binds
			// those to their exact Go types (wellKnownByType).
			worker.GenerateSpec{},
			worker.ToolSpec{},
			worker.ToolTurnSpec{},
			worker.Message{},
			worker.ToolCall{},
			worker.ToolResult{},
			worker.AssistantTurn{},
			// Named scalar (a bare identifier newtype — no const block; the logical
			// WorkerClass→model mapping lives behind the seam, never on the surface).
			worker.WorkerClass(""),
		},
		ifaceName: "WorkerAccess",
		iface:     reflect.TypeOf((*worker.WorkerAccess)(nil)).Elem(),
	},
	{
		name: "artifact",
		dir:  "internal/resourceaccess/artifact",
		models: []any{
			// The full transitive closure of ArtifactAccess's OWN contract value
			// types (artifactAccess.md §3). All defined in this package — full
			// encapsulation: the contract pulls NO external (projectstate/worker)
			// dep. ConstructionOutput carries []byte (opaque content bytes); the
			// generator binds it to its exact Go type (wellKnownByType). OutputTree's
			// Entries map is keyed by the named scalar OutputPath, registered here so
			// the map key resolves to the component's own type.
			artifact.ConstructionOutput{},
			artifact.OutputTree{},
			// Named scalar (a bare identifier newtype — no const block; a logical,
			// slash-separated path within an OutputTree, infrastructure-opaque).
			artifact.OutputPath(""),
		},
		ifaceName: "ArtifactAccess",
		iface:     reflect.TypeOf((*artifact.ArtifactAccess)(nil)).Elem(),
	},
	{
		name: "constructionpipeline",
		dir:  "internal/resourceaccess/constructionpipeline",
		models: []any{
			// The full transitive closure of ConstructionPipelineAccess's OWN contract
			// value types (constructionPipelineAccess.md §3). All defined in this package
			// — full encapsulation: the contract pulls NO external (projectstate) dep.
			constructionpipeline.PipelineSpec{},
			constructionpipeline.PipelineStep{},
			constructionpipeline.StepDependency{},
			constructionpipeline.ResourceRequest{},
			constructionpipeline.RepoTarget{},
			constructionpipeline.PipelineObservation{},
			constructionpipeline.StepObservation{},
			// Enums (one zero value each — const blocks captured from this dir).
			constructionpipeline.PipelinePhase(0),
			constructionpipeline.StepOutcome(0),
			// Named scalars (bare identifier newtypes — no const block). PipelineHandle
			// is the opaque pipeline identity, generated as a $def named scalar (its
			// behaviour lives in behavior.go as free functions — the OutputPath pattern).
			constructionpipeline.ProjectID(""),
			constructionpipeline.ConstructionActivityID(""),
			constructionpipeline.ArtifactRef(""),
			constructionpipeline.ToolchainRef(""),
			constructionpipeline.PipelineHandle(""),
		},
		ifaceName: "ConstructionPipelineAccess",
		iface:     reflect.TypeOf((*constructionpipeline.ConstructionPipelineAccess)(nil)).Elem(),
	},
	{
		name: "usagelog",
		dir:  "internal/resourceaccess/usage",
		models: []any{
			// The full transitive closure of UsageAccess's OWN contract value types
			// (usageAccess.md §2/§3). All defined in this package — full
			// encapsulation: the contract pulls NO external (projectstate) dep. The
			// time.Time window/timestamp fields bind to their exact Go type
			// (wellKnownByType). NOTE: CustomerID / OperatedAppID are `= uuid.UUID`
			// aliases, so fields of those types (incl. the *OperatedAppID pointer on
			// UsageRangeQuery) bind directly to uuid.UUID — they are NOT registered
			// separately. RawMeter is []byte (opaque source-meter payload), bound to
			// its exact Go type.
			usage.ComputeUnits{},
			usage.UsageRangeQuery{},
			usage.UsageEvent{},
			// Named scalars (bare identifier newtypes — no const block). CycleID is the
			// billing period; RuntimeEventID is the caller-supplied dedup token;
			// EntryRef is the opaque append-position ref returned by the write verbs.
			usage.CycleID(""),
			usage.RuntimeEventID(""),
			usage.EntryRef(""),
		},
		ifaceName: "UsageAccess",
		iface:     reflect.TypeOf((*usage.UsageAccess)(nil)).Elem(),
	},
	{
		name: "durableexecution",
		dir:  "internal/resourceaccess/durableexecution",
		models: []any{
			// The full transitive closure of DurableExecutionAccess's OWN contract value
			// types (durableExecutionAccess.md §3). All defined in this package — full
			// encapsulation: the contract pulls NO external (projectstate) dep AND NO
			// Temporal dep (the impl in temporal.go keeps Temporal; the contract surface
			// stays provider-opaque). ExecutionPayload.Bytes / ExecutionStateView.QueryResult
			// are []byte (opaque payload) and ExecutionStateView.ClosedAt is *time.Time; the
			// generator binds those to their exact Go types (wellKnownByType). Cadence.Every
			// is time.Duration, bound the same way.
			durableexecution.ExecutionPayload{},
			durableexecution.ScheduleSpec{},
			durableexecution.Cadence{},
			durableexecution.ExecutionStateView{},
			// Enum (one zero value — const block captured from this dir).
			durableexecution.ExecutionStatus(0),
			// Named scalars (bare identifier newtypes — no const block). ExecutionHandle is
			// the opaque execution identity, generated as a $def named scalar (the
			// constructionpipeline.PipelineHandle precedent): its behaviour lives in
			// behavior.go as free functions, keeping the opaque-handle Temporal mapping
			// confined to the impl. The runtime's (workflow-id, run-id) pair packs into the
			// string value, never exposed as such.
			durableexecution.ExecutionKind(""),
			durableexecution.ExecutionID(""),
			durableexecution.SignalName(""),
			durableexecution.QueryName(""),
			durableexecution.ScheduleID(""),
			durableexecution.ExecutionHandle(""),
		},
		ifaceName: "DurableExecutionAccess",
		iface:     reflect.TypeOf((*durableexecution.DurableExecutionAccess)(nil)).Elem(),
	},
	{
		name: "sourcecontrol",
		dir:  "internal/resourceaccess/sourcecontrol",
		models: []any{
			// The full transitive closure of SourceControlAccess's OWN contract value
			// types (sourceControlAccess.md §3 + sourceControlAccess-pullrequestrail.md §3).
			// All defined in this package — full encapsulation: the contract pulls NO
			// external (projectstate) dep AND no GitHub dep (the impl in github.go keeps
			// its GitHub/infra imports; the contract surface stays provider-opaque).
			// FOUNDER MERGE (2026-06-25): the former ISourceControlLifecycle +
			// IPullRequestRail are ONE flat SourceControlAccess (10 ops); reflection on it
			// yields all ten methods. RepoAdoptionSpec.Hints / ManagedFile.Content /
			// PullRequestSpec.Hints / RepoCredential.Bytes are []byte (opaque); the
			// generator binds those to their exact Go type (wellKnownByType).
			sourcecontrol.RepoAdoptionSpec{},
			sourcecontrol.ManagedFile{},
			sourcecontrol.RepoCredential{},
			sourcecontrol.PullRequestSpec{},
			sourcecontrol.ReviewSubmission{},
			sourcecontrol.MergeResult{},
			sourcecontrol.PullRequestStatus{},
			// Enums (one zero value each — const blocks captured from this dir).
			sourcecontrol.ReviewVerdict(0),
			sourcecontrol.CheckState(0),
			// Named scalars. ProjectID / AccountRef / BranchName are bare identifier
			// newtypes (no const block). Installation / RepoRef / CommitRef / BranchRef /
			// PullRequestRef are the opaque handles, generated as $def named scalars (the
			// durableexecution.ExecutionHandle / constructionpipeline.PipelineHandle
			// precedent): their behaviour lives in behavior.go as free functions, keeping
			// the opaque GitHub encoding confined to the impl.
			sourcecontrol.ProjectID(""),
			sourcecontrol.AccountRef(""),
			sourcecontrol.BranchName(""),
			sourcecontrol.Installation(""),
			sourcecontrol.RepoRef(""),
			sourcecontrol.CommitRef(""),
			sourcecontrol.BranchRef(""),
			sourcecontrol.PullRequestRef(""),
		},
		ifaceName: "SourceControlAccess",
		iface:     reflect.TypeOf((*sourcecontrol.SourceControlAccess)(nil)).Elem(),
	},
	{
		name: "construction",
		dir:  "internal/manager/construction",
		models: []any{
			// The full transitive closure of ConstructionManager's OWN port I/O types
			// (constructionManager.md §2/§3). All defined in this package — FULL
			// ENCAPSULATION: the contract pulls NO external (projectstate) dep and NO
			// Temporal dep (the Manager OWNS Temporal behind the port; the consumer-side
			// dependency interfaces + the Temporal Workflows struct stay hand-written and
			// are NOT part of this contract). ProjectID / ActivityID are this Manager's OWN
			// named-string types (converted to/from projectstate.ProjectID at the RA
			// boundary). OverrideKind's canonical-name lookup lives in contract.go as the
			// free function overrideKindName (the OutputPath/PipelineHandle precedent) so the
			// generated scalar carries no behavior. ReviewSet / Reviewer / PipelinePhase are
			// referenced by ConstructionSessionView (and re-used by the hand-written
			// reviewEngine / constructionPipelineAccess consumer mirrors in deps.go).
			construction.PumpResult{},
			construction.ReplanSweepResult{},
			construction.FlaggedVariance{},
			construction.ActivityOverride{},
			construction.ConstructionSessionView{},
			construction.ReviewSet{},
			construction.Reviewer{},
			// Enums (one zero value each — const blocks captured from this dir).
			construction.OverrideKind(0),
			construction.ConstructionStage(0),
			construction.PipelinePhase(0),
			// Named scalars (bare identifier newtypes — no const block). The Manager's OWN
			// contract identities; converted at the projectStateAccess boundary.
			construction.ProjectID(""),
			construction.ActivityID(""),
		},
		ifaceName: "ConstructionManager",
		iface:     reflect.TypeOf((*construction.ConstructionManager)(nil)).Elem(),
	},
	{
		name: "operations",
		dir:  "internal/manager/operations",
		models: []any{
			// The full transitive closure of OperationsManager's OWN port I/O types
			// (operationsManager.md §2/§3 + operationsRead-ruling.md §B). All defined in
			// this package — FULL ENCAPSULATION: the generated contract pulls NO external
			// (projectstate) dep and NO Temporal dep (the Manager OWNS Temporal behind the
			// port; the consumer-side dependency interfaces + the Temporal Workflows struct
			// stay hand-written and are NOT part of this contract). OperatedAppID / CustomerID
			// are `= uuid.UUID` aliases, so fields/params of those types bind directly to
			// uuid.UUID (wellKnownByType) — they are NOT registered separately (the autoscaler
			// OperatedAppID precedent). DesiredStateReason's / AutoscaleAction's canonical-name
			// lookups live in behavior.go as free functions (the OutputPath/PipelineHandle
			// precedent) so the generated enums carry no behavior. CostProjection is a `=
			// CostProjectionSeam` alias re-exported by contract.go; the generated $def is the
			// underlying CostProjectionSeam.
			operations.DesiredStateChange{},
			operations.DeployResult{},
			operations.ReconcileScope{},
			operations.ReconcileResult{},
			operations.WithdrawReason{},
			operations.WithdrawResult{},
			operations.ScalePoint{},
			operations.ScaleWhatIfPoints{},
			operations.CostProjectionSeam{},
			operations.Money{},
			operations.WhatIfPoint{},
			operations.WhatIfCurve{},
			operations.OperatedSystemView{},
			operations.HealthSnapshotView{},
			operations.SloRowView{},
			operations.RuntimeStatusEventView{},
			operations.AutoscalerView{},
			operations.AutoscaleDecisionView{},
			operations.DelinquencyContext{},
			// Enums (one zero value each — const blocks captured from this dir).
			operations.DesiredStateReason(0),
			operations.PatchKind(0),
			operations.RuntimeStatusSeam(0),
			operations.AutoscalerMode(0),
			operations.AutoscaleAction(0),
		},
		ifaceName: "OperationsManager",
		iface:     reflect.TypeOf((*operations.OperationsManager)(nil)).Elem(),
	},
	{
		name: "settlement-manager",
		dir:  "internal/manager/settlement",
		models: []any{
			// The full transitive closure of SettlementManager's OWN port I/O types
			// (settlementManager.md §2/§3). All defined in this package — FULL
			// ENCAPSULATION: the generated contract pulls NO external (projectstate) dep
			// and NO Temporal dep (the Manager OWNS Temporal behind the port; the
			// consumer-side dependency interfaces in deps.go + the Temporal Workflows
			// struct stay hand-written and are NOT part of this contract). CustomerID /
			// DeployedAppID are `= uuid.UUID` aliases, so fields/params of those types
			// bind directly to uuid.UUID (wellKnownByType) — they are NOT registered
			// separately (the operations OperatedAppID precedent). CycleID is a `= string`
			// alias, so CycleID fields reflect as plain string — also NOT registered.
			// RoutingDirective's canonical-name lookup lives in behavior.go as the free
			// function routingDirectiveName (the operations DesiredStateReason precedent)
			// so the generated enum carries no behavior. Money is shared by both the
			// façade contract and the deps.go seams; it lives in the generated file and
			// the seams reference it in-package.
			mgrsettlement.SettlementRef{},
			mgrsettlement.CloseCycleResult{},
			mgrsettlement.ShortfallSweepResult{},
			mgrsettlement.GatewayRevenueEvent{},
			mgrsettlement.GatewayReversalEvent{},
			mgrsettlement.Money{},
			// Enum (one zero value — const block captured from this dir).
			mgrsettlement.RoutingDirective(0),
		},
		ifaceName: "SettlementManager",
		iface:     reflect.TypeOf((*mgrsettlement.SettlementManager)(nil)).Elem(),
	},
	{
		name: "systemdesign",
		dir:  "internal/manager/systemdesign",
		models: []any{
			// The full transitive closure of SystemDesignManager's OWN port I/O types
			// (systemDesignManager.md §2/§3). All defined in this package — FULL
			// ENCAPSULATION: the generated contract pulls NO external (projectstate) dep
			// and NO Temporal dep (the Manager OWNS Temporal behind the port; the
			// consumer-side dependency interfaces + the Temporal Workflows struct stay
			// hand-written and are NOT part of this contract). The staged typed DRAFT is
			// carried OPAQUELY via DraftModel ({kind, model}) — systemdesign never
			// regenerates or shares projectstate's sealed ArtifactModel sum or its 17
			// variants. ProjectID / ArtifactKind / ResearchInput / Version are this
			// Manager's OWN named types (converted to/from projectstate at the RA
			// boundary; the ArtifactKind ordinals MIRROR projectstate for a
			// meaning-preserving cast). The enum/identity behavior lives in behavior.go as
			// free functions (the operations DesiredStateReason precedent) so the
			// generated types carry no behavior. SessionRef is the opaque continuity token
			// (a named string scalar; NewSessionRef is a free function). Critique /
			// CritiqueVerdict are NOT on the port surface — they stay hand-written.
			mgrsystemdesign.ResearchSource{},
			mgrsystemdesign.ResearchInput{},
			mgrsystemdesign.AnchoredComment{},
			mgrsystemdesign.ReviewFeedback{},
			mgrsystemdesign.PhaseAdvanceResult{},
			mgrsystemdesign.DraftModel{},
			mgrsystemdesign.Location{},
			mgrsystemdesign.Finding{},
			mgrsystemdesign.SessionStateView{},
			// Enums (one zero value each — const blocks captured from this dir). Severity
			// is a STRING enum (its value IS the camelCase wire name); the rest are int.
			mgrsystemdesign.ArtifactKind(0),
			mgrsystemdesign.ReviewDecision(0),
			mgrsystemdesign.SessionStage(0),
			mgrsystemdesign.Severity(""),
			// Named scalars (bare identifier newtypes — no const block). ProjectID is the
			// catalog identity; SessionRef is the opaque session continuity token; Version
			// is the head-state optimistic-concurrency token; RuleID is a finding's rule id.
			mgrsystemdesign.ProjectID(""),
			mgrsystemdesign.SessionRef(""),
			mgrsystemdesign.Version(0),
			mgrsystemdesign.RuleID(""),
		},
		ifaceName: "SystemDesignManager",
		iface:     reflect.TypeOf((*mgrsystemdesign.SystemDesignManager)(nil)).Elem(),
	},
	{
		name: "projectdesign",
		dir:  "internal/manager/projectdesign",
		models: []any{
			// The full transitive closure of ProjectDesignManager's OWN port I/O types
			// (projectDesignManager.md §2/§3). All defined in this package — FULL
			// ENCAPSULATION: the generated contract pulls NO external (projectstate) dep
			// and NO Temporal dep (the Manager OWNS Temporal behind the port; the
			// consumer-side dependency interfaces + the Temporal Workflows struct + the
			// internal SDP assembly stay hand-written and are NOT part of this contract).
			// The staged typed DRAFT (and the assembled SdpReview) is carried OPAQUELY via
			// DraftModel ({kind, model}) — projectdesign never regenerates or shares
			// projectstate's sealed ArtifactModel sum or its 17 variants. ProjectID /
			// ArtifactKind / OptionID are this Manager's OWN named types (converted to/from
			// projectstate at the RA boundary; the ArtifactKind ordinals MIRROR projectstate
			// for a meaning-preserving cast). The enum/identity behavior lives in behavior.go
			// as free functions (the systemdesign precedent) so the generated types carry no
			// behavior. SessionRef is the opaque continuity token (a named string scalar;
			// NewSessionRef is a free function).
			mgrprojectdesign.ReviewFeedback{},
			mgrprojectdesign.PhaseAdvanceResult{},
			mgrprojectdesign.DraftModel{},
			mgrprojectdesign.Location{},
			mgrprojectdesign.Finding{},
			mgrprojectdesign.SessionStateView{},
			// Enums (one zero value each — const blocks captured from this dir). Severity
			// is a STRING enum (its value IS the camelCase wire name); the rest are int.
			mgrprojectdesign.ArtifactKind(0),
			mgrprojectdesign.ReviewDecision(0),
			mgrprojectdesign.SDPDecision(0),
			mgrprojectdesign.SessionStage(0),
			mgrprojectdesign.Severity(""),
			// Named scalars (bare identifier newtypes — no const block). ProjectID is the
			// catalog identity; OptionID names a committed SDP option; SessionRef is the
			// opaque session continuity token; RuleID is a finding's rule id.
			mgrprojectdesign.ProjectID(""),
			mgrprojectdesign.OptionID(""),
			mgrprojectdesign.SessionRef(""),
			mgrprojectdesign.RuleID(""),
		},
		ifaceName: "ProjectDesignManager",
		iface:     reflect.TypeOf((*mgrprojectdesign.ProjectDesignManager)(nil)).Elem(),
	},
	{
		// projectstate is the LAST + highest-stakes RA: the project.json PERSISTENCE
		// layer (byte-identical round-trip invariant) AND the canonical owner of the
		// domain types. It is NO LONGER interface-only: the ~100 pure-data domain
		// models below are reflected into $defs and regenerated into contract.gen.go
		// like every other component, with their BEHAVIOR (String()/Kind()/the
		// enum MarshalJSON codecs/isArtifactModel markers) kept in sibling hand-written
		// files — Go binds a generated type's methods from any file in the package, and
		// contractstrip strips only the now-duplicate type/const declarations, never
		// the methods. modelgen preserves Go field DECLARATION order, so every
		// persisted model round-trips byte-identically.
		//
		// bindByName (below) keeps hand-written the handful the generator cannot
		// reproduce faithfully — see the field doc. Everything else regenerates.
		name: "projectstate",
		dir:  "internal/resourceaccess/projectstate",
		models: []any{
			projectstate.ActivityBuildStatus(0),
			projectstate.ActivityConstructionPhase(0),
			projectstate.ActivityDiagram{},
			projectstate.ActivityEdge{},
			projectstate.ActivityGitStatus{},
			projectstate.ActivityList{},
			projectstate.ActivityMethodPhase(""),
			projectstate.ActivityNetwork{},
			projectstate.ActivityNodeKind(0),
			projectstate.ActivityOutcome(0),
			projectstate.ActivityType(0),
			projectstate.Actor{},
			projectstate.ArtifactKind(0),
			projectstate.ArtifactReviewStatus(0),
			projectstate.ArtifactSlot{},
			projectstate.Axis(0),
			projectstate.CICheckState(0),
			projectstate.CallMode(0),
			projectstate.CheckItem{},
			projectstate.CheckStatus(0),
			projectstate.Classification(0),
			projectstate.Component{},
			projectstate.ComponentKind(0),
			projectstate.ComputeCostKind(0),
			projectstate.ConstructionProgress{},
			projectstate.ContainerInstance{},
			projectstate.ContractInterface{},
			projectstate.CoreUseCases{},
			projectstate.CorpusPresence{},
			projectstate.DeliveryStyle(0),
			projectstate.DeployContainer{},
			projectstate.DeployNoteRecord{},
			projectstate.DeploymentEnvironment{},
			projectstate.DeploymentProfile(0),
			projectstate.DeploymentTopology{},
			projectstate.DocNoteRecord{},
			projectstate.DocOutlineRecord{},
			projectstate.DynamicView{},
			projectstate.EdgeKind(0),
			projectstate.FailureReason(0),
			projectstate.Glossary{},
			projectstate.GlossaryItem{},
			projectstate.InfrastructureKind(0),
			projectstate.InfrastructureNode{},
			projectstate.IntegrationNoteRecord{},
			projectstate.Layer(0),
			projectstate.MissionStatement{},
			projectstate.Money{},
			projectstate.NetworkDependency{},
			projectstate.NetworkNodeCompute{},
			projectstate.NetworkSummary{},
			projectstate.Objective{},
			projectstate.OperationalConcepts{},
			projectstate.OperationalDecision{},
			projectstate.OptionActivity{},
			projectstate.OptionID(""),
			projectstate.OwnerScope(""),
			projectstate.Phase(0),
			projectstate.PhaseArtifacts{},
			projectstate.PlanningAssumptions{},
			projectstate.ProducedArtifact{},
			projectstate.Profile{},
			projectstate.ProfilePhase{},
			projectstate.ProjectCatalogRef{},
			projectstate.ProjectID(""),
			projectstate.ProjectOption{},
			projectstate.ProjectSummary{},
			projectstate.ProvisioningSpecRecord{},
			projectstate.QualityGate{},
			projectstate.Relationship{},
			projectstate.RepoCredential{},
			projectstate.Requirement{},
			projectstate.ResearchInput{},
			projectstate.ResearchSource{},
			projectstate.RevenueShareKind(0),
			projectstate.ReviewPolicy{},
			projectstate.RiskModel{},
			projectstate.RiskRow{},
			projectstate.SRSRecord{},
			projectstate.ScheduleKind(0),
			projectstate.ScrubbedRequirements{},
			projectstate.SdpOptionRow{},
			projectstate.SdpReview{},
			projectstate.SettlementTerms{},
			projectstate.SoftwareSystemInstance{},
			projectstate.Solution{},
			projectstate.StandardCheck{},
			projectstate.System{},
			projectstate.TestPlanRecord{},
			projectstate.TestingVariant(0),
			projectstate.Trigger(0),
			projectstate.UIDesignRecord{},
			projectstate.UXRequirementsRecord{},
			projectstate.UsageAssumption{},
			projectstate.UseCaseDecision{},
			projectstate.Version(0),
			projectstate.Volatilities{},
			projectstate.Volatility{},
			projectstate.WorkerMix{},
		},
		// bindByName — kept hand-written, referenced by bare Go name. TWO reasons:
		//   1. VALUE+OMITEMPTY structs: they carry `json:"x,omitempty"` on VALUE-typed
		//      fields; modelgen pointerizes optional value fields (*T), which changes
		//      the Go API and breaks the hand-written consumers (including out-of-package
		//      Managers) that assign those fields as values. Kept hand-written so the
		//      value+omitempty shape — and the consumers — are untouched.
		//   2. ArtifactModel (the sealed sum interface): its Solution variant's Kind()
		//      returns a DYNAMIC field (s.SlotKind, shared by the 4 solution slots),
		//      which emitSumType's static per-variant Kind() cannot express. The whole
		//      sum — the interface, every variant's Kind()/isArtifactModel(), and the
		//      bespoke integer-kind slot codec (slotcodec.go) — stays hand-written. The
		//      variant STRUCTS themselves (System, Glossary, …) are still regenerated as
		//      plain data models above; only the interface + its methods are hand-written.
		bindByName: []any{
			(*projectstate.ArtifactModel)(nil),
			// DeploymentNode is SELF-RECURSIVE (Children []DeploymentNode — a tree).
			// jsonschema-go's flat-$defs reflection cannot express self-recursion
			// without turning the root schema into a bare self-$ref, so the type stays
			// hand-written and DeploymentTopology references it by bare name.
			projectstate.DeploymentNode{},
			// ActivityNode, PhaseArtifactPayload, UseCase carry POINTER-to-named fields
			// WITHOUT omitempty (e.g. PhaseArtifactPayload.SRS *SRSRecord — a nil-means-null
			// optional sub-artifact). jsonschema-go can't record pointer-ness on a $ref, and
			// modelgen only pointerizes OPTIONAL (omitempty) value fields — so a required
			// *T field would regenerate as a value T, changing the Go API (nil checks) and
			// the null-vs-{} wire form. Kept hand-written; their referenced record structs
			// are still regenerated and bound by bare name.
			projectstate.ActivityNode{},
			projectstate.PhaseArtifactPayload{},
			projectstate.UseCase{},
			projectstate.ActivityConstructionStatus{},
			projectstate.ActivityItem{},
			projectstate.ContractDep{},
			projectstate.ContractOperation{},
			projectstate.ContractParam{},
			projectstate.DefectRecord{},
			projectstate.HarnessModule{},
			projectstate.Network{},
			projectstate.NetworkMilestone{},
			projectstate.PerfHarness{},
			projectstate.PhaseCompletion{},
			projectstate.Project{},
			projectstate.ServiceContract{},
			projectstate.SystemTestPlan{},
			projectstate.TestArg{},
			projectstate.TestCase{},
			projectstate.TestExpect{},
			projectstate.TestRun{},
			projectstate.TestScenario{},
			projectstate.TestStep{},
			projectstate.TestingState{},
		},
		ifaceName: "ProjectStateAccess",
		iface:     reflect.TypeOf((*projectstate.ProjectStateAccess)(nil)).Elem(),
	},
}

func main() {
	want := map[string]bool{}
	for _, a := range os.Args[1:] {
		want[a] = true
	}
	for _, c := range registry {
		if len(want) > 0 && !want[c.name] {
			continue
		}
		if err := writeComponent(c); err != nil {
			fmt.Fprintf(os.Stderr, "schemagen %s: %v\n", c.name, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "wrote %s/contract.schema.json (%d defs, iface %s)\n", c.dir, len(c.models), c.ifaceName)
	}
}

func writeComponent(c component) error {
	// bind-by-name: own-package types the contract references but does NOT
	// regenerate resolve to a bare {"x-go-type": "<TypeName>"} binding, in both
	// struct-field and interface param/result position. Built per-component and
	// exposed to schemaForType via currentBindByName (nil/empty for every component
	// that leaves bindByName unset — a strict no-op). A pointer entry is deref'd so
	// the map is keyed by the element (interface/struct) type.
	bindByType := map[reflect.Type]*jsonschema.Schema{}
	for _, x := range c.bindByName {
		t := reflect.TypeOf(x)
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		bindByType[t] = &jsonschema.Schema{Extra: map[string]any{"x-go-type": t.Name()}}
	}
	currentBindByName = bindByType

	modelNames := map[string]bool{}
	for _, m := range c.models {
		modelNames[reflect.TypeOf(m).Name()] = true
	}

	// refForType maps each model type to a local `$ref` stub, so a model nested in
	// another model renders as a reference rather than being inlined.
	refForType := map[reflect.Type]*jsonschema.Schema{}
	for _, m := range c.models {
		t := reflect.TypeOf(m)
		refForType[t] = &jsonschema.Schema{Ref: "#/$defs/" + t.Name()}
	}

	// Sum types contribute (a) the sealed interface type → a `$ref` to the sum
	// `$def` (so a struct/param field of the interface type renders as a reference,
	// not the `true`/any jsonschema-go would otherwise produce), and (b) each
	// variant struct as a normal model `$def`. Register both before reflecting any
	// model so nested refs resolve.
	sumRefByIface := map[reflect.Type]*jsonschema.Schema{}
	currentSumRefs = sumRefByIface // visible to schemaForType for sealed-interface params
	for _, st := range c.sumTypes {
		sumRefByIface[st.iface] = &jsonschema.Schema{Ref: "#/$defs/" + st.name}
		modelNames[st.name] = true
		for _, v := range st.variants {
			vt := reflect.TypeOf(v)
			if vt.Kind() == reflect.Pointer {
				vt = vt.Elem()
			}
			modelNames[vt.Name()] = true
			refForType[vt] = &jsonschema.Schema{Ref: "#/$defs/" + vt.Name()}
		}
	}

	// Capture enum const sets from the component's own dir AND from the defining
	// package of any referenced type (Option B: a component inlines external domain
	// types — incl. their enums — as its OWN defs, so generated code imports nothing).
	enums := map[string]enumInfo{}
	for _, dir := range append([]string{c.dir}, externalDirs(c.models)...) {
		captured, err := captureEnums(dir)
		if err != nil {
			return fmt.Errorf("capture enums in %s: %w", dir, err)
		}
		for k, v := range captured {
			enums[k] = v
		}
	}

	defs := map[string]*jsonschema.Schema{}
	for _, m := range c.models {
		t := reflect.TypeOf(m)
		// A named scalar with a captured const set is an enum, not a struct.
		if t.Kind() != reflect.Struct {
			if e, ok := enums[t.Name()]; ok {
				defs[t.Name()] = enumSchema(e)
				continue
			}
		}
		siblings := map[reflect.Type]*jsonschema.Schema{}
		for rt, ws := range wellKnownByType {
			siblings[rt] = ws
		}
		for rt, ref := range refForType {
			if rt != t {
				siblings[rt] = ref
			}
		}
		// A field whose Go type is a registered sealed interface resolves to a
		// `$ref` to its sum `$def` (not the open `true` jsonschema-go emits for an
		// interface).
		for rt, ref := range sumRefByIface {
			siblings[rt] = ref
		}
		// A field whose Go type is a bind-by-name type binds to its bare Go name
		// (x-go-type) rather than being reflected inline / regenerated as a $def.
		for rt, ref := range bindByType {
			siblings[rt] = ref
		}
		s, err := jsonschema.ForType(t, &jsonschema.ForOptions{
			IgnoreInvalidTypes: true,
			TypeSchemas:        siblings,
		})
		if err != nil {
			return fmt.Errorf("reflect model %s: %w", t.Name(), err)
		}
		// Preserve the original Go field name per property (json tag determines the
		// schema property key / wire name; the Go field name can differ, e.g.
		// `ProjectID` with `json:"projectId"`). Recorded as x-go-name so modelgen
		// regenerates the exact Go field identifier without changing the wire shape.
		injectGoNames(s, t)
		// Preserve the EXACT Go integer width. jsonschema-go collapses every integer
		// kind to `type:"integer"`, and modelgen defaults that to int64 — but a field
		// declared `int` (or int32/uint/…) must regenerate as THAT type, not int64, or
		// the generated struct diverges from its hand-written consumers. Record the Go
		// type as x-go-type (which modelgen already honors) for any integer field whose
		// width is not the int64 default. A no-op for int64 fields and for named
		// enum-typed fields (those resolve via $ref, not a bare integer).
		injectGoIntWidths(s, t)
		defs[t.Name()] = s
	}

	// Emit each sum type: reflect every variant struct as its own `$def`, then emit
	// the sum `$def` itself as a `oneOf` of variant `$ref`s carrying the
	// `x-go-sumtype` descriptor.
	for _, st := range c.sumTypes {
		if err := emitSumType(st, defs, refForType, sumRefByIface); err != nil {
			return fmt.Errorf("reflect sum type %s: %w", st.name, err)
		}
	}

	doc := &jsonschema.Schema{
		Schema: "https://json-schema.org/draft/2020-12/schema",
		ID:     "archistrator://contract/" + c.name,
		Title:  c.name + " contract",
		Defs:   defs,
	}

	if c.iface != nil {
		iface, err := reflectInterface(c, modelNames)
		if err != nil {
			return fmt.Errorf("reflect interface %s: %w", c.ifaceName, err)
		}
		doc.Extra = map[string]any{"interface": iface}
	}

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	raw = append(raw, '\n')

	out := c.dir + "/contract.schema.json"
	if err := os.WriteFile(out, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	return nil
}

// emitSumType reflects a sealed-interface declaration into the contract document:
// every variant struct becomes a normal model `$def`, and the sum itself becomes a
// `$def` whose `oneOf` lists the variant `$ref`s, carrying the `x-go-sumtype`
// descriptor modelgen reads to regenerate the interface + markers + envelope codec.
// The variant kind STRINGS are discovered by calling each variant's discriminator
// method via reflection, so the emitted descriptor (and thus the generated codec)
// is byte-identical to the hand-written wire form.
func emitSumType(st sumTypeDecl, defs map[string]*jsonschema.Schema, refForType, sumRefByIface map[reflect.Type]*jsonschema.Schema) error {
	desc := codegen.SumType{
		Iface:            st.name,
		Marker:           st.marker,
		DiscriminatorKey: st.discriminatorKey,
		KindEnum:         st.kindEnum,
	}
	oneOf := make([]*jsonschema.Schema, 0, len(st.variants))
	for _, v := range st.variants {
		vt := reflect.TypeOf(v)
		if vt.Kind() == reflect.Pointer {
			vt = vt.Elem()
		}
		// Confirm the variant satisfies the sealed interface (catches a stale
		// registration at generation time rather than emitting a broken contract).
		if !reflect.PointerTo(vt).Implements(st.iface) {
			return fmt.Errorf("variant %s does not implement %s", vt.Name(), st.name)
		}
		// Reflect the variant struct as a normal model $def (siblings: well-knowns,
		// every other registered model ref, and any sealed-interface ref).
		siblings := map[reflect.Type]*jsonschema.Schema{}
		for rt, ws := range wellKnownByType {
			siblings[rt] = ws
		}
		for rt, ref := range refForType {
			if rt != vt {
				siblings[rt] = ref
			}
		}
		for rt, ref := range sumRefByIface {
			siblings[rt] = ref
		}
		vs, err := jsonschema.ForType(vt, &jsonschema.ForOptions{
			IgnoreInvalidTypes: true,
			TypeSchemas:        siblings,
		})
		if err != nil {
			return fmt.Errorf("reflect variant %s: %w", vt.Name(), err)
		}
		injectGoNames(vs, vt)
		defs[vt.Name()] = vs

		// Discover the variant's discriminator value by calling its discriminator
		// method via reflection, then map it to the wire kind STRING + Go const.
		disc, err := callDiscriminator(v, st.discriminatorMethod)
		if err != nil {
			return fmt.Errorf("variant %s discriminator: %w", vt.Name(), err)
		}
		desc.Variants = append(desc.Variants, codegen.SumVariant{
			Kind:      st.kindString(disc),
			Type:      vt.Name(),
			KindConst: st.kindConst(disc),
		})
		oneOf = append(oneOf, &jsonschema.Schema{Ref: "#/$defs/" + vt.Name()})
	}

	// Round-trip the typed descriptor through JSON into the generic map shape the
	// schema's Extra carries (so it serializes under x-go-sumtype as plain JSON).
	raw, err := json.Marshal(desc)
	if err != nil {
		return err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return err
	}
	defs[st.name] = &jsonschema.Schema{
		OneOf: oneOf,
		Extra: map[string]any{"x-go-sumtype": generic},
	}
	return nil
}

// callDiscriminator invokes the named (zero-arg) discriminator method on a variant
// value via reflection and returns its single result as a comparable any.
func callDiscriminator(v any, method string) (any, error) {
	rv := reflect.ValueOf(v)
	m := rv.MethodByName(method)
	if !m.IsValid() {
		return nil, fmt.Errorf("no method %s on %T", method, v)
	}
	out := m.Call(nil)
	if len(out) != 1 {
		return nil, fmt.Errorf("method %s must return exactly one value", method)
	}
	return out[0].Interface(), nil
}

// reflectInterface enumerates the interface's methods into operation descriptors,
// mapping each param/result type to a schema (`$ref` for model types, inline for
// primitives/arrays) and recovering param names from source.
func reflectInterface(c component, modelNames map[string]bool) (codegen.Interface, error) {
	names, err := paramNames(c.dir, c.ifaceName)
	if err != nil {
		return codegen.Interface{}, err
	}
	out := codegen.Interface{Name: c.ifaceName, Layer: layerFromDir(c.dir)}
	for i := 0; i < c.iface.NumMethod(); i++ {
		m := c.iface.Method(i)
		ft := m.Type // interface method type has no receiver
		op := codegen.Operation{Name: m.Name}
		pn := names[m.Name]
		for j := 0; j < ft.NumIn(); j++ {
			// Skip cross-cutting params delivered by the layer Context (ctx,
			// principal, idempotency, or an already-prepended layer Context); the
			// generator re-injects a single Context. Keeps the data schema pure.
			if contextCarried[ft.In(j)] {
				continue
			}
			nm := fmt.Sprintf("arg%d", j)
			if j < len(pn) {
				nm = pn[j]
			}
			in := ft.In(j)
			op.Params = append(op.Params, codegen.Param{
				Name:    nm,
				Pointer: in.Kind() == reflect.Pointer, // nil-meaningful nullable param → emit *T
				Schema:  schemaForType(in, modelNames),
			})
		}
		for j := 0; j < ft.NumOut(); j++ {
			ot := ft.Out(j)
			if ot == errorType {
				op.Error = true
				continue
			}
			op.Result = schemaForType(ot, modelNames)
		}
		out.Operations = append(out.Operations, op)
	}
	return out, nil
}

// injectGoNames records each struct field's ORIGINAL Go field name as `x-go-name`
// on the corresponding property schema, keyed by the property's wire name (json
// tag). Lets modelgen emit the exact Go identifier (e.g. `ProjectID`) while the
// json tag keeps the wire key (e.g. `projectId`).
func injectGoNames(s *jsonschema.Schema, t reflect.Type) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || s == nil || len(s.Properties) == 0 {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		wire := f.Name
		if tag := f.Tag.Get("json"); tag != "" {
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if name != "" {
				wire = name
			}
		}
		// Only record when the Go name differs from what modelgen would derive from
		// the wire key (so PascalCase-tagged components stay byte-identical — no-op).
		if f.Name == exportTitle(wire) {
			continue
		}
		prop, ok := s.Properties[wire]
		if !ok || prop == nil {
			continue
		}
		// Clone before mutating: property schemas for well-known types (time/uuid/
		// []byte) are SHARED pointers, so mutating in place cross-contaminates other
		// fields of the same type. A shallow copy with a fresh Extra map is enough.
		cp := *prop
		cp.Extra = map[string]any{}
		for k, v := range prop.Extra {
			cp.Extra[k] = v
		}
		cp.Extra["x-go-name"] = f.Name
		s.Properties[wire] = &cp
	}
}

// injectGoIntWidths records x-go-type on each struct-field property whose Go type
// is a bare integer of a width OTHER than the int64 default modelgen assumes for a
// JSON `integer`. Without this, a field declared `int` (Instances, CommittedCount,
// Week, …) would regenerate as int64 and break every hand-written consumer that
// assigns/reads it as int. Only bare basic integer types are annotated (PkgPath ==
// "" — a named enum type like Layer is skipped, it resolves via $ref); arrays and
// pointers are unwrapped to reach the element/pointee field type + its schema node.
func injectGoIntWidths(s *jsonschema.Schema, t reflect.Type) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || s == nil || len(s.Properties) == 0 {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		wire := f.Name
		if tag := f.Tag.Get("json"); tag != "" {
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if name != "" {
				wire = name
			}
		}
		prop, ok := s.Properties[wire]
		if !ok || prop == nil {
			continue
		}
		// Unwrap slice/pointer on BOTH the Go type and the schema node so a
		// []int / *int field annotates the integer leaf.
		ft := f.Type
		node := prop
		for {
			switch ft.Kind() {
			case reflect.Pointer:
				ft = ft.Elem()
				continue
			case reflect.Slice, reflect.Array:
				ft = ft.Elem()
				if node.Items != nil {
					node = node.Items
				}
				continue
			case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
				reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func,
				reflect.Interface, reflect.Map, reflect.String, reflect.Struct, reflect.UnsafePointer:
				// Not a pointer/slice/array — nothing left to unwrap; same as falling
				// through to the break below.
			}
			break
		}
		if name, ok := nonDefaultIntGoType(ft); ok && node.Ref == "" {
			cp := *node
			cp.Extra = map[string]any{}
			for k, v := range node.Extra {
				cp.Extra[k] = v
			}
			if _, has := cp.Extra["x-go-type"]; !has {
				cp.Extra["x-go-type"] = name
			}
			*node = cp
		}
	}
}

// nonDefaultIntGoType returns (goTypeName, true) if ft is a BARE basic integer type
// (PkgPath == "") of a width other than int64 — the widths modelgen would otherwise
// mis-generate as int64. Returns ("", false) for int64, non-integers, and named
// (enum) integer types.
func nonDefaultIntGoType(ft reflect.Type) (string, bool) {
	if ft.PkgPath() != "" { // a named/defined type (enum) — resolved via $ref, not here
		return "", false
	}
	switch ft.Kind() {
	case reflect.Int:
		return "int", true
	case reflect.Int8:
		return "int8", true
	case reflect.Int16:
		return "int16", true
	case reflect.Int32:
		return "int32", true
	case reflect.Uint:
		return "uint", true
	case reflect.Uint8:
		return "uint8", true
	case reflect.Uint16:
		return "uint16", true
	case reflect.Uint32:
		return "uint32", true
	case reflect.Uint64:
		return "uint64", true
	case reflect.Invalid, reflect.Bool, reflect.Int64, reflect.Uintptr, reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128, reflect.Array, reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Map, reflect.Pointer, reflect.Slice, reflect.String, reflect.Struct, reflect.UnsafePointer:
		// int64, non-integers, and (unreachable here — named types return early
		// above) enum-like integer types — same as the default below.
		return "", false
	default:
		return "", false
	}
}

// exportTitle upper-cases the first byte (mirrors modelgen's exportName) — the
// default Go field name modelgen derives from a wire key.
func exportTitle(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// currentSumRefs holds the sealed-interface → sum-`$ref` map for the component
// being written, so schemaForType can resolve a sealed-interface PARAM/result to
// its sum `$ref` (jsonschema-go would otherwise emit `true`/any for an interface).
// Set per-component in writeComponent; nil for components with no sum types (a
// strict no-op for every existing component).
var currentSumRefs map[reflect.Type]*jsonschema.Schema

// currentBindByName holds the bind-by-name reflect.Type → x-go-type schema map for
// the component being written, so schemaForType can resolve a bound param/result to
// its bare Go name (rather than reflecting it inline or as a $def). Set per-component
// in writeComponent; nil/empty for components with no bindByName entries (a strict
// no-op for every existing component).
var currentBindByName map[reflect.Type]*jsonschema.Schema

// schemaForType maps a Go type to a JSON Schema node: a `$ref` for model types,
// an array schema for slices, otherwise jsonschema-go's reflected schema.
func schemaForType(rt reflect.Type, modelNames map[string]bool) *jsonschema.Schema {
	// A sealed-interface param/result resolves to its sum `$ref` (checked on the
	// interface type itself, before the pointer-deref below).
	if ref, ok := currentSumRefs[rt]; ok {
		return ref
	}
	// A bind-by-name param/result binds to its hand-written Go type by bare name
	// (no $def, no import — same package). Checked before the pointer-deref so a
	// *BoundType binds the same way (pointer-ness is carried by the Param.Pointer
	// flag, exactly like a $ref param); the interface type itself is also matched
	// here (an ArtifactModel result).
	if ref, ok := currentBindByName[rt]; ok {
		return ref
	}
	if rt.Kind() == reflect.Pointer {
		if ref, ok := currentBindByName[rt.Elem()]; ok {
			return ref
		}
	}
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if ws, ok := wellKnownByType[rt]; ok {
		return ws
	}
	if rt.Name() != "" && modelNames[rt.Name()] {
		return &jsonschema.Schema{Ref: "#/$defs/" + rt.Name()}
	}
	if rt.Kind() == reflect.Slice {
		return &jsonschema.Schema{Type: "array", Items: schemaForType(rt.Elem(), modelNames)}
	}
	s, err := jsonschema.ForType(rt, &jsonschema.ForOptions{IgnoreInvalidTypes: true})
	if err != nil || s == nil {
		return &jsonschema.Schema{}
	}
	return s
}

// enumInfo is a captured named-scalar enum: its underlying Go base type and the
// ordered (const-name, value) pairs of its members.
type enumInfo struct {
	base   string
	names  []string
	values []any
}

// enumSchema renders a captured enum as a JSON Schema `enum` with the Go binding
// metadata modelgen needs to regenerate the type + const block.
func enumSchema(e enumInfo) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Enum:  e.values,
		Extra: map[string]any{"x-go-base": e.base, "x-enum-varnames": e.names},
	}
	switch {
	case e.base == "string":
		s.Type = "string"
	case strings.HasPrefix(e.base, "float"):
		s.Type = "number"
	default:
		s.Type = "integer"
	}
	return s
}

// captureEnums parses a package's source and returns its named-scalar enum types
// (a `type X <scalar>` plus a const block binding members to X). Handles the
// dominant Go idioms: `= iota` runs and explicit int/string literals.
func captureEnums(dir string) (map[string]enumInfo, error) {
	fset := token.NewFileSet()
	files, err := parseGoDir(fset, dir)
	if err != nil {
		return nil, err
	}
	base := map[string]string{} // type name -> scalar base, for named scalars
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				ts := sp.(*ast.TypeSpec)
				if id, ok := ts.Type.(*ast.Ident); ok && isScalarBase(id.Name) {
					base[ts.Name.Name] = id.Name
				}
			}
		}
	}
	enums := map[string]enumInfo{}
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			curType := ""
			for iota, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				if vs.Type != nil {
					if id, ok := vs.Type.(*ast.Ident); ok {
						curType = id.Name
					}
				}
				b, isEnum := base[curType]
				if !isEnum {
					continue
				}
				for _, nameID := range vs.Names {
					e := enums[curType]
					e.base = b
					e.names = append(e.names, nameID.Name)
					e.values = append(e.values, evalConst(vs, iota, b))
					enums[curType] = e
				}
			}
		}
	}
	return enums, nil
}

// parseGoDir parses every .go file directly in dir (non-recursively) and returns
// the parsed files. It replaces the deprecated parser.ParseDir; build tags are
// irrelevant for schemagen's single-package component directories.
func parseGoDir(fset *token.FileSet, dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			return nil, perr
		}
		files = append(files, f)
	}
	return files, nil
}

// evalConst resolves a const member's value: an explicit int/string literal, or
// the iota index for `= iota` runs (and bare continuation specs).
func evalConst(vs *ast.ValueSpec, iota int, base string) any {
	if len(vs.Values) == 1 {
		switch v := vs.Values[0].(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if s, err := strconv.Unquote(v.Value); err == nil {
					return s
				}
				return v.Value
			}
			if n, err := strconv.Atoi(v.Value); err == nil {
				return n
			}
		}
	}
	if base == "string" {
		return ""
	}
	return iota
}

func isScalarBase(name string) bool {
	switch name {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "byte", "rune",
		"float32", "float64", "string", "bool":
		return true
	}
	return false
}

// paramNames parses the package source and returns, per method of ifaceName, the
// ordered parameter names (Go reflection does not expose them).
func paramNames(dir, ifaceName string) (map[string][]string, error) {
	fset := token.NewFileSet()
	files, err := parseGoDir(fset, dir)
	if err != nil {
		return nil, err
	}
	res := map[string][]string{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != ifaceName {
				return true
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				return true
			}
			for _, fld := range it.Methods.List {
				if len(fld.Names) == 0 {
					continue
				}
				ft, ok := fld.Type.(*ast.FuncType)
				if !ok || ft.Params == nil {
					continue
				}
				var pn []string
				for i, p := range ft.Params.List {
					if len(p.Names) == 0 {
						pn = append(pn, fmt.Sprintf("arg%d", i))
						continue
					}
					for _, nm := range p.Names {
						pn = append(pn, nm.Name)
					}
				}
				res[fld.Names[0].Name] = pn
			}
			return false
		})
	}
	return res, nil
}
