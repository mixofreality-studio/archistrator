package projectstate

import "encoding/json"

// toolpalette.go — the INTERNAL MCP tool surface for archistrator's own
// ResourceAccess and Engine contracts.
//
// DOCTRINE (agentic-managers spec item 3 + rule 3). Every ResourceAccess/Engine
// contract operation is TOOL-ELIGIBLE: it has a GENERATED internal MCP tool
// (toolcatalog.gen.go, emitted from .serviceContracts by cmd/internaltoolsgen).
// This surface is INTERNAL — it is NEVER part of the public OAS (cmd/clientgen);
// it is the tool catalog aiarch-state-mcp registers inside a design/construction
// GitHub job. aiarch-state-mcp exposes the non-hidden read-only + Engine tools in
// every per-mode set on top of the composed verbs (AgentHidden ops stay refused).
//
// The composed verbs (putDraftModel, publishDraft, respondToReviewComment,
// setCritiqueVerdict, getCommittedSlot, the research reads, reconcile) sit ON TOP
// of this raw surface: doctrine rule 3 says an invariant compiles into a composed
// verb, and a composed verb may SHADOW/replace the raw generated equivalent where
// the raw op would be unsafe for an agent. Ops that stay server-rail-only even
// though they are generated are marked AgentHidden (see cmd/internaltoolsgen) —
// e.g. every raw projectStateAccess WRITE (CommitArtifact/AdvancePhase/… — merge
// authority stays with the server rail; the composed verbs are the agent surface).
//
// projectstate is the home of this surface because it already OWNS the typed
// contract corpus (ServiceContract) AND the typed System model (dynamic views +
// static edges) the palette resolver reads — the same category as the pure
// derivation helpers (CommandFor, DeriveKind, ClassifyType) it already shares
// downward with the Managers. No client/manager import is introduced.

// InternalTool is one generated internal MCP tool descriptor: the durable,
// platform-neutral surface for a single ResourceAccess/Engine contract operation.
// It is DATA (schemas carried raw so they round-trip byte-for-byte); it binds no
// implementation — a design job registers it from this descriptor, and the
// construction rail (a later priority) attaches the executing handler.
type InternalTool struct {
	// Name is the MCP tool name: the owning component's base with its layer
	// suffix (Access/Engine) stripped, lowerFirst, + the operation name — e.g.
	// projectStateAccess.ReadProject → "projectStateReadProject".
	Name string `json:"name"`
	// Component is the owning contract key in .serviceContracts (e.g.
	// "projectStateAccess") — the target component the tool operates.
	Component string `json:"component"`
	// Layer is the Method layer of the owning component ("ResourceAccess" | "Engine").
	Layer string `json:"layer"`
	// Operation is the contract operation's Go method name (e.g. "ReadProject").
	Operation string `json:"operation"`
	// Params is the operation's business parameter names IN DECLARATION ORDER —
	// the ambient leading call Context (fwra.Context / fweng.Context) is NOT
	// included (it is not a business parameter and never appears in InputSchema).
	// The execution rail (cmd/aiarch-state-mcp) uses this ordered list to bind a
	// tool call's named arguments to the live Go method's positional parameters;
	// the order mirrors the schema-first Go signature the contract was generated
	// from, so args[Params[i]] decodes into method parameter i+1 (i+1 skips the
	// bound receiver's leading Context).
	Params []string `json:"params"`
	// ReadOnly is the MCP readOnlyHint. Every Engine operation is read-only
	// (Engines are pure, side-effect-free computation); a ResourceAccess
	// operation is read-only iff its name carries a read verb (Get/Read/List/
	// Query/Observe/Retrieve/Fetch). Derived by cmd/internaltoolsgen from the
	// Method naming convention — the contract's honest, existing read/write signal.
	ReadOnly bool `json:"readOnly"`
	// AgentHidden marks a generated op intentionally NOT exposed to agents even
	// though it is generated — its authority stays on the server rail, or a
	// composed verb replaces it. A tool palette may not name an AgentHidden op.
	AgentHidden bool `json:"agentHidden"`
	// Description is the human tool description an agentic consumer reads.
	Description string `json:"description"`
	// InputSchema and OutputSchema are the tool's self-contained JSON Schemas
	// ($defs inlined to the transitive closure the operation references), carried
	// raw so the generated file round-trips byte-for-byte.
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

// InternalToolCatalog returns the full generated internal tool surface for
// archistrator's ResourceAccess + Engine contracts — every operation, INCLUDING
// AgentHidden ones (documentation + the palette lint need the whole set).
func InternalToolCatalog() []InternalTool { return internalToolCatalog() }

// AgentExposableTools returns the catalog minus every AgentHidden op — the
// operations an agentic sub-workflow may be granted. A tool palette may name only
// a tool in this set.
func AgentExposableTools() []InternalTool {
	all := internalToolCatalog()
	out := make([]InternalTool, 0, len(all))
	for _, t := range all {
		if !t.AgentHidden {
			out = append(out, t)
		}
	}
	return out
}

// InternalToolByName looks up a generated tool by its MCP tool name.
func InternalToolByName(name string) (InternalTool, bool) {
	for _, t := range internalToolCatalog() {
		if t.Name == name {
			return t, true
		}
	}
	return InternalTool{}, false
}
