package main

// rawexec.go is the EXECUTION rail for the raw generated internal RA/Engine tools
// (the P1a catalog surface). registerRawTool (tools.go) used to register every raw
// tool with a bootstrap "not executable" stub; this file makes an allowlisted raw
// tool actually RUN inside the design/construction GitHub job — or return an HONEST
// "unavailable in this substrate" error when the operation's live implementation
// needs external creds/services the job does not provision.
//
// SUBSTRATE MODEL (agentic-managers spec item 1). The MCP binary runs INSIDE a
// checkout, so:
//
//   - ENGINES (all 7) are pure in-process computation with no outbound calls and
//     zero-arg constructors, so they are constructed directly and invoked in
//     process. Dispatch is generic (reflection over the constructed interface
//     value + the catalog's ordered Params), so a new Engine op becomes executable
//     the moment it is generated into the catalog — no hand-wiring per op.
//
//   - projectStateAccess READS operate on the working tree (the checked-out
//     .aiarch/state/project.json), so they are served directly from the session.
//     Its WRITES are AgentHidden (merge authority stays on the server rail /
//     composed verbs replace them) and never reach this rail.
//
//   - Every OTHER ResourceAccess component fronts an external service the job does
//     NOT have (GitHub App, Temporal, Postgres, a merchant gateway, an operated
//     runtime) — or is a server-side stub. Executing those here would be either an
//     I/O call that cannot succeed or a canned STUB SUCCESS that lies to the agent,
//     so each returns a typed unavailable-in-substrate error naming the missing
//     dependency. This is deliberate and documented (unavailableDeps below).

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	enginebilling "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// engineImpls is the constructed in-process value for every Engine component,
// keyed by its contract-key component name (the catalog's Component field). Each
// is a pure, dependency-free interface value; the generic engine executor
// dispatches an operation onto it by reflection.
func engineImpls() map[string]any {
	return map[string]any{
		"autoscalerEngine":          autoscaler.NewAutoscalerEngine(),
		"billingEngine":             enginebilling.NewBillingEngine(),
		"estimationEngine":          estimation.NewEstimationEngine(),
		"handOffEngine":             handoff.NewHandOffEngine(),
		"interventionEngine":        intervention.NewInterventionEngine(),
		"operationEstimationEngine": operationestimation.NewOperationEstimationEngine(),
		"reviewEngine":              review.NewReviewEngine(),
	}
}

// unavailableDeps documents, per external ResourceAccess component, the dependency
// the design/construction GitHub job does NOT provision — so its raw tools return a
// typed unavailable-in-substrate error rather than an I/O failure or a stub-success
// lie. This map is the authoritative "executes vs unavailable" ledger for the RA
// surface (Engines + projectStateAccess reads are the in-substrate set; everything
// listed here is out-of-substrate).
var unavailableDeps = map[string]string{
	"sourceControlAccess":        "a GitHub App client (installation credentials)",
	"artifactAccess":             "a GitHub blob store + auth resolver",
	"constructionPipelineAccess": "a GitHub Actions App installation",
	"durableExecutionAccess":     "a Temporal client",
	"usageAccess":                "a Postgres connection pool",
	"operatedSystemStateAccess":  "a Postgres connection pool",
	"operatedRuntimeAccess":      "an operated-runtime profile/infrastructure",
	"billingStateAccess":         "the billing-state store (a server-side stub, not a real substrate impl)",
	"merchantGatewayAccess":      "a merchant payment gateway (Stripe)",
	// B6: constructionTransitionAccess / gitActivityStatusAccess / designSessionAccess
	// share projectStateAccess's git substrate (the checkout IS available here), but
	// unlike projectStateAccess's plain reads, every op on these three is a
	// Manager-orchestrated head-state transition primitive (cred/idempotencyKey/
	// version threading the constructionManager or design Managers mint and check —
	// exactly the authority projectStateAccess's WRITES are AgentHidden to protect).
	// Not yet consumed by any Manager (B7-B10 land that); the raw MCP rail refuses
	// them for the same reason, not a missing infra dependency.
	"constructionTransitionAccess": "constructionManager-orchestrated transition authority (version/eligibility checks the raw MCP rail cannot replicate)",
	"gitActivityStatusAccess":      "constructionManager-orchestrated git head-state authority (version/eligibility checks the raw MCP rail cannot replicate)",
	"designSessionAccess":          "design-Manager-orchestrated branch/session authority (version/capability-fallback checks the raw MCP rail cannot replicate)",
	// revenueLedgerAccess is a permanent server-side no-op (charge-only, R-013 —
	// billingstate.NewRevenueLedgerAccess never persists), same category as
	// billingStateAccess above.
	"revenueLedgerAccess": "the revenue-ledger store (a permanent no-op stub, not a real substrate impl)",
}

// executeRawTool is the entry point registerRawTool binds each raw tool's handler
// to. It routes by the tool's owning component: Engines dispatch in-process,
// projectStateAccess reads serve from the checkout, and every external RA returns a
// documented unavailable-in-substrate error.
func executeRawTool(ctx context.Context, s *Session, tool projectstate.InternalTool, args map[string]any) (any, error) {
	if tool.AgentHidden {
		// Defensive: an agent-hidden op is never registered (registerFromAllowlist
		// refuses it), so this should be unreachable — but never execute one.
		return nil, fmt.Errorf("raw tool %q (%s.%s) is agent-hidden and must not execute; its authority stays on the server rail", tool.Name, tool.Component, tool.Operation)
	}
	if tool.Layer == "Engine" {
		return executeEngineTool(ctx, tool, args)
	}
	if tool.Component == "projectStateAccess" {
		return executeProjectStateRead(s, tool, args)
	}
	if dep, ok := unavailableDeps[tool.Component]; ok {
		return nil, fmt.Errorf("tool %q (%s.%s) is unavailable in this substrate: its live implementation needs %s, which the design/construction GitHub job does not provision", tool.Name, tool.Component, tool.Operation, dep)
	}
	return nil, fmt.Errorf("tool %q (%s.%s) has no in-substrate executor registered", tool.Name, tool.Component, tool.Operation)
}

// executeEngineTool dispatches one Engine operation onto its constructed in-process
// value by reflection. Engine methods have the uniform shape
// (rc <Layer>Context, businessParams…) (result, error): the leading ambient call
// Context is constructed here (it is not a business parameter and never appears in
// the tool's args), and each business parameter is bound positionally from the
// catalog's ordered Params via a JSON round-trip into the method's Go parameter type.
func executeEngineTool(ctx context.Context, tool projectstate.InternalTool, args map[string]any) (any, error) {
	impl, ok := engineImpls()[tool.Component]
	if !ok {
		return nil, fmt.Errorf("no in-process engine constructed for component %q", tool.Component)
	}
	method := reflect.ValueOf(impl).MethodByName(tool.Operation)
	if !method.IsValid() {
		return nil, fmt.Errorf("engine %q has no operation %q", tool.Component, tool.Operation)
	}
	mt := method.Type()
	if mt.NumIn() != len(tool.Params)+1 {
		// +1 is the leading ambient Context, which is not in Params.
		return nil, fmt.Errorf("engine %q op %q expects %d business params but the catalog carries %d", tool.Component, tool.Operation, mt.NumIn()-1, len(tool.Params))
	}

	in := make([]reflect.Value, mt.NumIn())
	// in[0] — the ambient call Context. Constructed generically by setting the
	// embedded context.Context field, so this rail needs no per-layer knowledge of
	// the concrete Context struct.
	ctxVal, err := newCallContext(mt.In(0), ctx)
	if err != nil {
		return nil, err
	}
	in[0] = ctxVal

	// in[1..] — the business params, bound positionally from the ordered Params.
	for i, name := range tool.Params {
		argVal := reflect.New(mt.In(i + 1))
		if raw, present := args[name]; present && raw != nil {
			b, merr := json.Marshal(raw)
			if merr != nil {
				return nil, fmt.Errorf("encode argument %q: %w", name, merr)
			}
			if uerr := json.Unmarshal(b, argVal.Interface()); uerr != nil {
				return nil, fmt.Errorf("argument %q does not fit parameter %d of %s.%s: %w", name, i+1, tool.Component, tool.Operation, uerr)
			}
		}
		in[i+1] = argVal.Elem()
	}

	out := method.Call(in)
	// The last return value is the error; a preceding value (if any) is the result.
	if n := len(out); n > 0 {
		if errVal := out[n-1]; errVal.Type().Implements(errorType) && !errVal.IsNil() {
			return nil, errVal.Interface().(error)
		}
		if n >= 2 {
			return out[0].Interface(), nil
		}
	}
	return map[string]any{}, nil
}

// errorType is the reflect.Type of the error interface, used to detect an
// operation's trailing error return.
var errorType = reflect.TypeOf((*error)(nil)).Elem()

// newCallContext constructs a value of the method's leading Context parameter type
// (fweng.Context / fwra.Context) carrying the given ctx. Both layer Contexts embed
// context.Context under the exported field name "Context", so this is done
// generically by reflection — no per-layer import beyond the compile-time anchor.
func newCallContext(t reflect.Type, ctx context.Context) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	f := v.FieldByName("Context")
	if !f.IsValid() || !f.CanSet() {
		return reflect.Value{}, fmt.Errorf("leading parameter type %s is not a recognized call Context (no settable embedded Context)", t)
	}
	f.Set(reflect.ValueOf(ctx))
	return v, nil
}

// engineContextAnchor keeps the fweng import compile-anchored: newCallContext builds
// the Context generically, but importing the package documents the layer this rail
// serves and guarantees the module is present.
var _ = fweng.Context{}

// executeProjectStateRead serves a projectStateAccess READ operation directly from
// the checkout (the working tree is one committed version of the project). Writes
// are AgentHidden and never reach this rail. The result is the decoded typed value,
// marshaled to JSON text by the caller.
func executeProjectStateRead(s *Session, tool projectstate.InternalTool, _ map[string]any) (any, error) {
	proj, _, err := s.readProject()
	if err != nil {
		return nil, err
	}
	switch tool.Operation {
	case "ReadProject", "ReadProjectVersion":
		// The checkout is a single committed version; both reads return it. (A
		// specific historical version is not addressable from a single working tree —
		// the palette targets the current committed state the job operates on.)
		return proj, nil
	case "ListProjects":
		return map[string]any{"projects": []projectstate.ProjectID{proj.ID}}, nil
	default:
		return nil, fmt.Errorf("projectStateAccess op %q is not an in-substrate read (only ReadProject/ReadProjectVersion/ListProjects execute here; writes stay on the server rail)", tool.Operation)
	}
}

// inSubstrateComponents returns the sorted set of components whose raw tools execute
// in this substrate (Engines + projectStateAccess) — the ledger's positive side,
// used by tests and diagnostics.
func inSubstrateComponents() []string {
	set := map[string]bool{"projectStateAccess": true}
	for c := range engineImpls() {
		set[c] = true
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
