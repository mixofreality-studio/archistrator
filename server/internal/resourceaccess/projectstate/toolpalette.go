package projectstate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// toolpalette.go — the INTERNAL MCP tool surface for archistrator's own
// ResourceAccess and Engine contracts, and (in a later section, resolver.go) the
// per-task tool-palette scoping the design/construction rail applies to it.
//
// DOCTRINE (agentic-managers spec item 3 + rule 3). Every ResourceAccess/Engine
// contract operation is TOOL-ELIGIBLE: it has a GENERATED internal MCP tool
// (toolcatalog.gen.go, emitted from .serviceContracts by cmd/internaltoolsgen).
// This surface is INTERNAL — it is NEVER part of the public OAS (cmd/clientgen);
// it is the tool catalog aiarch-state-mcp registers inside a design/construction
// GitHub job, scoped per task by the agentic sub-workflow's tool palette.
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

// --- per-task tool-palette resolution (agentic-managers spec item 5) ---------
//
// A design/construction task may use ONLY the tools documented in that task's
// agentic sub-workflow palette in the architecture DYNAMICS. The palette lives on
// the dynamic-view step (Relationship.Palette) — one step is one call-chain edge.
// The resolver reads archistrator's OWN committed System model, finds the use
// case's dynamic view, and unions the palettes its steps document.
//
// LINT (spec): a palette must be ⊆ the owning component's static dependency edges
// — a dynamic can never document a tool the architecture forbids. The owner of a
// step is its caller (edge.From); each palette tool targets a component that MUST
// be a static outbound dependency of that caller. Enforced as an ERROR here and
// mirrored as an app-side design-review finding (manager/systemdesign) so a bad
// palette is caught at the human gate too.

// PaletteViolation is one palette-⊄-edges lint failure.
type PaletteViolation struct {
	// UseCaseID is the dynamic view the offending step belongs to.
	UseCaseID string
	// From / To identify the step (the call-chain edge) carrying the palette.
	From string
	To   string
	// Tool is the offending palette tool name.
	Tool string
	// Reason is the human explanation.
	Reason string
}

// Resolution is the outcome of resolving a task's allowed internal-tool list.
type Resolution struct {
	// Tools is the resolved allowlist of MCP tool names (sorted, de-duplicated).
	// On fallback it is the caller's Fallback set.
	Tools []string
	// UsedFallback is true when no palette was documented for the use case and the
	// caller's Fallback set was used instead.
	UsedFallback bool
	// Warnings are human notices (e.g. the missing-palette fallback WARN).
	Warnings []string
	// Errors are lint ERRORS (palette ⊄ static edges / names an agent-hidden op).
	// A non-empty Errors means the resolution must be rejected.
	Errors []string
}

// ResolveToolPalette resolves the internal-tool allowlist for one agentic
// sub-workflow task from archistrator's OWN committed System model. It finds the
// use case's dynamic view, unions the tool palettes its steps (edges) document,
// and LINTS each against the static architecture. When no palette is documented
// (BOOTSTRAP: archistrator's dynamics do not carry palettes yet), it returns the
// caller's fallback set with UsedFallback + a WARN — strictness flips
// automatically the moment a palette exists. jobMode is used only to phrase the
// fallback warning (the fallback set itself is the caller's — the composed-verb
// set lives with aiarch-state-mcp, the single owner of that vocabulary).
func ResolveToolPalette(sys System, useCaseID, jobMode string, fallback []string) Resolution {
	var dv *DynamicView
	for i := range sys.DynamicViews {
		if sys.DynamicViews[i].UseCaseID == useCaseID {
			dv = &sys.DynamicViews[i]
			break
		}
	}

	names := map[string]bool{}
	if dv != nil {
		for _, e := range dv.Edges {
			for _, n := range e.Palette {
				names[n] = true
			}
		}
	}

	if len(names) == 0 {
		return Resolution{
			Tools:        append([]string(nil), fallback...),
			UsedFallback: true,
			Warnings: []string{fmt.Sprintf(
				"no tool palette documented for use case %q's agentic sub-workflow (dynamics carry no palette yet); falling back to the job-mode %q tool set",
				useCaseID, jobMode)},
		}
	}

	var res Resolution
	if dv != nil {
		for _, v := range lintDynamicViewPalettes(sys, *dv) {
			res.Errors = append(res.Errors, fmt.Sprintf("palette on step %s→%s: %s (tool %q)", v.From, v.To, v.Reason, v.Tool))
		}
	}
	res.Tools = sortedKeys(names)
	return res
}

// LintPalettesWithinEdges lints EVERY documented palette in the System against the
// static architecture edges. It is the authoritative palette-⊄-edges check, shared
// by ResolveToolPalette (dispatch-time ERROR) and the app-side design-review
// finding (manager/systemdesign) / its methodcheck twin.
func LintPalettesWithinEdges(sys System) []PaletteViolation {
	var out []PaletteViolation
	for _, dv := range sys.DynamicViews {
		out = append(out, lintDynamicViewPalettes(sys, dv)...)
	}
	return out
}

// lintDynamicViewPalettes lints one dynamic view's step palettes.
func lintDynamicViewPalettes(sys System, dv DynamicView) []PaletteViolation {
	var out []PaletteViolation
	for _, e := range dv.Edges {
		if len(e.Palette) == 0 {
			continue
		}
		outbound := canonicalOutbound(sys, e.From)
		for _, name := range e.Palette {
			tool, ok := InternalToolByName(name)
			if !ok {
				// Not a raw RA/Engine tool — a composed verb (governed by doctrine,
				// not by architecture edges) or an unknown name. The ⊆-edges rule
				// applies only to the generated raw surface; skip.
				continue
			}
			if tool.AgentHidden {
				out = append(out, PaletteViolation{
					UseCaseID: dv.UseCaseID, From: e.From, To: e.To, Tool: name,
					Reason: "names an agent-hidden operation (its authority stays on the server rail / a composed verb replaces it)",
				})
				continue
			}
			if !outbound[canonicalComponent(tool.Component)] {
				out = append(out, PaletteViolation{
					UseCaseID: dv.UseCaseID, From: e.From, To: e.To, Tool: name,
					Reason: fmt.Sprintf("targets component %q, which is not a static dependency of the step owner %q", tool.Component, e.From),
				})
			}
		}
	}
	return out
}

// canonicalOutbound returns the canonicalized set of components the given caller
// has a static outbound relationship to. Canonicalization bridges the contract
// key space ("projectStateAccess") and the System component-id space
// ("project-state-access").
func canonicalOutbound(sys System, from string) map[string]bool {
	out := map[string]bool{}
	for _, r := range sys.Relationships {
		if r.From == from {
			out[canonicalComponent(r.To)] = true
		}
	}
	return out
}

// canonicalComponent normalizes a component identifier for cross-space comparison:
// lowercased with '-', '_', and spaces removed (so "projectStateAccess",
// "project-state-access", and "Project State Access" all coincide).
func canonicalComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '-', '_', ' ':
			// drop
		default:
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
