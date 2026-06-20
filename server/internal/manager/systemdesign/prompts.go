package systemdesign

import (
	"fmt"
	"strings"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

// The Manager OWNS the per-step prompt corpus (2026-05-29 rework §2.1; rework §6).
// The fixed Method sequence drives WHICH role-prompt the Manager sends at each
// step — that is the SystemDesignPhaseWorkflow volatility (the sequence), made
// explicit. The generic worker (workerAccess.generateTypedData[T]) holds NO
// Method-specific prompt corpus; the prompt + tool choice are the CALLER's. This
// file is where the deleted systemDesignEngine's prompts.go content belongs —
// owned by the sequence that drives it.
//
// Two role families:
//   - ARCHITECT-role draft prompts (one per Phase-1 artifact kind) — the worker
//     returns the typed <Kind> model.
//   - PM-role critique prompts (only for mission / glossary+scrubbed /
//     core-use-cases — the kinds the Method assigns a PM reviewer) — the worker
//     returns a typed Critique.
//
// Each prompt is plain text composed IN-MEMORY by the Manager and shipped as a
// DISPATCH INPUT to the claude-code-action DESIGN job (§0d.2 step 2 — never
// aiarch-persisted). It carries a role header, the target artifact kind, a pointer
// to the prior committed state BY PATH/KIND (the Action runs IN the user's repo and
// reads .aiarch/state/ directly — priors are NOT embedded as bytes), the Method
// doctrine for HOW to draft a good X, and (optionally) a feedback block woven in
// verbatim on a redraft. The Action drafts the typed JSON into .aiarch/state/ and
// the required CI validation check enforces its shape — the schema/DTO injection the
// old in-process worker needed is GONE (validation is the CI check, §0d.5).

const architectHeader = "You are the Architect agent drafting a typed Method artifact for an architecture project, following Juval Lowy's The Method to the letter. You are running inside the project repository; read the prior committed Method artifacts from .aiarch/state/project.json and commit your drafted artifact back into .aiarch/state/.\n"

const pmHeader = "You are the Product Manager agent critiquing a drafted Method artifact, following Juval Lowy's The Method. You are running inside the project repository; read the drafted artifact and the prior committed state from .aiarch/state/project.json.\n"

// architectDraftPrompt assembles the architect-role draft prompt for the given
// Phase-1 artifact kind. It points the Action at the prior committed state by
// path/kind (NOT embedded bytes — the Action reads .aiarch/state/ in the repo),
// carries the Method drafting doctrine, and weaves in any rejection / PM-revision
// feedback. The ResearchInput pointer is named for the MISSION step. The composed
// prompt is the DESIGN job's design_prompt dispatch input.
func architectDraftPrompt(kind projectstate.ArtifactKind, proj projectstate.Project, feedback ReviewFeedback) string {
	var b strings.Builder
	b.WriteString(architectHeader)
	fmt.Fprintf(&b, "Target artifact: %s\n", kind)

	// Per-kind priors: name the committed predecessor artifacts the Method draws on,
	// by kind (the Action reads them from .aiarch/state/project.json in the repo).
	switch kind {
	case projectstate.KindMission:
		writeResearch(&b, proj.ResearchInput)
	case projectstate.KindGlossary:
		writePriorsPointer(&b, "Mission")
	case projectstate.KindScrubbedRequirements:
		writePriorsPointer(&b, "Mission", "Glossary")
	case projectstate.KindVolatilities:
		writePriorsPointer(&b, "Mission", "Glossary", "ScrubbedRequirements")
	case projectstate.KindCoreUseCases:
		writePriorsPointer(&b, "Mission", "Glossary", "Volatilities")
	case projectstate.KindSystem:
		writePriorsPointer(&b, "Mission", "Glossary", "Volatilities", "CoreUseCases")
	case projectstate.KindOperationalConcepts:
		writePriorsPointer(&b, "Mission", "System")
	case projectstate.KindStandardCheck:
		writePriorsPointer(&b, "System", "OperationalConcepts")
	}

	writeFeedback(&b, feedback)
	fmt.Fprintf(&b, "\nTask: %s\n", draftTask(kind))
	return b.String()
}

// pmCritiquePrompt assembles the PM-role critique prompt for a drafted artifact.
// Only mission / glossary+scrubbed / core-use-cases route through PM-critique (the
// kinds the Method assigns a PM reviewer). The PM reads the just-committed draft from
// .aiarch/state/ and either ratifies it (Approve) or records concrete revision
// guidance (Revise); Revise loops back to the architect-role draft BEFORE the human
// gate. The composed prompt is the critique DESIGN job's design_prompt dispatch input.
func pmCritiquePrompt(kind projectstate.ArtifactKind, draft projectstate.ArtifactModel) string {
	var b strings.Builder
	b.WriteString(pmHeader)
	fmt.Fprintf(&b, "Artifact under review: %s (read its just-committed draft from .aiarch/state/project.json)\n", kind)
	b.WriteString("\nTask: as the Product Manager, ratify the draft (Approve) or request a concrete revision (Revise with notes naming the revision the architect should make). Ratify only what faithfully serves the business; the human makes the final commit decision.\n")
	// CRITIQUE READ-BACK CONTRACT (D-MSD-Δ amendment). The PM-critique job does NOT
	// rewrite the artifact model. It records its verdict into the SAME slot's
	// first-class critique carrier so the Manager reads it back: in
	// .aiarch/state/project.json, on this artifact's slot, set "critiqueVerdict" to
	// exactly "approve" or "revise", and on "revise" set "critiqueNotes" to the
	// revision guidance (leave it empty on "approve"). Do NOT touch the slot's
	// "notes" field (that is the human architect's reject/withdraw rationale). Commit
	// onto the critique branch and open a PR so the required validate check applies.
	b.WriteString("\nRecord your verdict on this artifact's slot in .aiarch/state/project.json: set \"critiqueVerdict\" to exactly \"approve\" or \"revise\". On \"revise\", set \"critiqueNotes\" to the concrete revision guidance; leave \"critiqueNotes\" empty on \"approve\". Do NOT modify the slot's \"notes\" field, and do NOT rewrite the artifact \"model\". A verdict is REQUIRED — never commit the critique with an empty \"critiqueVerdict\".\n")
	return b.String()
}

// activityDiagramGuide teaches the architect role HOW TO COMPOSE a well-formed UML
// activity diagram from the typed node/edge model — not just the per-field shape
// the JSON Schema already carries. It is woven into the Core Use Cases draft prompt.
// The rules mirror the artifactValidationEngine's UC-ACTDIAG checks, so the model is
// told exactly what the machine will reject (decision must branch >=2 and reconverge
// at a merge; fork is unguarded concurrency that joins; guards only on decisions).
// No backticks appear inside — JSON examples use their natural double quotes — so
// this stays a single raw string literal.
const activityDiagramGuide = `ACTIVITY DIAGRAM: when a use case BRANCHES or runs steps CONCURRENTLY, populate its "activity" as a WELL-FORMED UML activity diagram — a graph of "nodes" (each {ref, kind, label, roleName, linkedActor, linkedComp}) and "edges" (each {from, to, kind, guard}). A purely linear use case may leave "activity" null. NEVER emit a bare string for "activity" — it is an object or null.

IDENTITY BY NAME (no ids): you NEVER emit any opaque id or uuid. Give each node a short "ref" slug of your own (e.g. "n1", "n2") UNIQUE within the diagram; edges reference nodes by that "ref" in "from"/"to". "linkedActor" (optional) is an actor's ROLE name from this use case; "linkedComp" (optional) is a System component NAME. The server resolves all of these by name.

Node kinds and their edge cardinality:
- start: one per diagram; 0 incoming, exactly 1 outgoing.
- action: a step; 1 incoming, 1 outgoing.
- decision: a CHOICE; 1 incoming, >=2 outgoing.
- merge: rejoins a decision's alternative branches; >=2 incoming, 1 outgoing.
- fork: splits into CONCURRENT paths; 1 incoming, >=2 outgoing.
- join: synchronizes concurrent paths; >=2 incoming, 1 outgoing.
- end: a final node; >=1 incoming, 0 outgoing.
Put every node in its business-role swim-lane via "roleName" (e.g. "Customer", "Trusted System") — a business role or area of interest, NOT a Method layer or subsystem name.

Edge kinds:
- guardedFlow: carries a "guard" condition; used ONLY on the outgoing edges of a decision.
- controlFlow: no guard (set "guard" to ""); EVERY other edge, including ALL fork outgoing edges.

Composition rules you MUST follow (a violation is rejected and redrafted):
1. A decision is a CHOICE: it MUST have >=2 outgoing guardedFlow edges, each with a distinct, mutually-exclusive guard; give exactly ONE edge the guard "[else]" for the remaining case. Its branches MUST reconverge at a merge node before the flow continues — a branch must not run straight into the next step or dangle.
2. A fork is CONCURRENCY (not a choice): >=2 outgoing controlFlow (UNguarded) edges, ALL of which run; the concurrent paths MUST reconverge at a join. Never put a guard on a fork edge.
3. guardedFlow edges originate ONLY from decision nodes; every other node's outgoing edges are controlFlow.
4. A LOOP is a merge loop-head -> ...body... -> a decision whose "[repeat]" guarded edge BACK-EDGES to the loop-head merge and whose "[else]" guarded edge exits.
Decision/merge model an ALTERNATIVE (exactly one branch taken); fork/join model CONCURRENCY (all paths taken) — do not confuse them.

Worked examples (each node carries your own short "ref" slug — NOT a uuid; edges reference those refs):

if/else — a decision's two branches reconverge at a merge:
{"nodes":[{"ref":"n1","kind":"decision","label":"Is the item actionable?","roleName":"Trusted System"},{"ref":"n2","kind":"action","label":"Create next step and assign context","roleName":"Trusted System"},{"ref":"n3","kind":"action","label":"File or incubate item","roleName":"Trusted System"},{"ref":"n4","kind":"merge","label":"","roleName":"Trusted System"}],"edges":[{"from":"n1","to":"n2","kind":"guardedFlow","guard":"[actionable]"},{"from":"n1","to":"n3","kind":"guardedFlow","guard":"[else]"},{"from":"n2","to":"n4","kind":"controlFlow","guard":""},{"from":"n3","to":"n4","kind":"controlFlow","guard":""}]}

fork/join — two concurrent paths synchronize:
{"nodes":[{"ref":"n1","kind":"fork","label":"","roleName":"Marketplace"},{"ref":"n2","kind":"action","label":"Search the registry","roleName":"Marketplace"},{"ref":"n3","kind":"action","label":"Notify the tradesman","roleName":"Tradesman"},{"ref":"n4","kind":"join","label":"","roleName":"Marketplace"}],"edges":[{"from":"n1","to":"n2","kind":"controlFlow","guard":""},{"from":"n1","to":"n3","kind":"controlFlow","guard":""},{"from":"n2","to":"n4","kind":"controlFlow","guard":""},{"from":"n3","to":"n4","kind":"controlFlow","guard":""}]}

while-loop — a decision back-edges to the loop-head merge:
{"nodes":[{"ref":"n1","kind":"merge","label":"","roleName":"Trusted System"},{"ref":"n2","kind":"action","label":"Process the next item","roleName":"Trusted System"},{"ref":"n3","kind":"decision","label":"More items?","roleName":"Trusted System"},{"ref":"n4","kind":"end","label":"","roleName":"Trusted System"}],"edges":[{"from":"n1","to":"n2","kind":"controlFlow","guard":""},{"from":"n2","to":"n3","kind":"controlFlow","guard":""},{"from":"n3","to":"n1","kind":"guardedFlow","guard":"[more]"},{"from":"n3","to":"n4","kind":"guardedFlow","guard":"[else]"}]}`

// draftTask returns the per-kind task instruction — the Method doctrine for HOW to
// draft a good artifact of this kind, distilled from Juval Lowy's The Method (the
// the-method-* skills). The schema (draftSchema) already fixes the SHAPE; this prose
// teaches the architect role the THINKING the kind demands so the draft is sound,
// not merely well-typed.
func draftTask(kind projectstate.ArtifactKind) string {
	switch kind {
	case projectstate.KindMission:
		return "Produce the mission from the research corpus. The vision is ONE terse sentence naming the future the system creates. The mission is expressed in terms of the system's COMPONENTS and their evolving relationships — NOT a feature list. First distill the 2-3 business pillars that DIFFERENTIATE this system from competitors; ground the vision and objectives in those. Each objective is a numbered, measurable business outcome (not a feature deliverable)."

	case projectstate.KindGlossary:
		return "Extract the system's ubiquitous-language terms, each categorised by the Four Questions: Who interacts with the system, What is required of it, How (the business activity), Where (state lives). Define each term crisply in business language with NO solution/implementation wording. These terms are the shared vocabulary every later artifact must reuse verbatim."

	case projectstate.KindScrubbedRequirements:
		return "Scrub every solution out of the requirements and emit the underlying NEEDS only. A need states what the business requires; a solution states how to build it — strip the how. 'Users log in with OAuth' is a solution; 'the system authenticates users' is the need. Each item must be solution-free and traceable to the mission."

	case projectstate.KindVolatilities:
		return "Identify the areas of VOLATILITY the architecture must encapsulate, along TWO independent axes. Axis sameCustomerOverTime: for each requirement ask 'what in THIS customer's business will change in 1, 3, 5 years?'. Axis allCustomersAtOneTime: ask 'do ALL customers do this identically today, or do markets/regulations/languages/customer-types vary?'. Encapsulate the open-ended (VOLATILE); REJECT anything a simple conditional handles (that is merely VARIABLE). Reject by-reflex 'Logging'/'Reporting' blocks with no business volatility, speculative 'might-need-someday' encapsulation, and nature-of-the-business items competitors do identically. Aim for ~6-15 entries, each with a rationale paragraph and its axis."

	case projectstate.KindCoreUseCases:
		return "Select the CORE use cases by ABSTRACTION, not by listing what the customer asked for. For each candidate ask: does this capture the ESSENCE of the business (what differentiates it, what creates value), or is it a permutation/utility (onboarding, payment, account admin)? Could a single higher abstraction — often a NEW name not in the customer's vocabulary — subsume several raw use cases? Target 2-6 core use cases; if you have more than 6 you have not abstracted enough. Sanity check: a one-slide brochure for the system would have roughly this many bullets. Record each rejected permutation with its rejection reason and link it to the core it permutes by setting its \"variationOf\" to that core use case's NAME (exactly as you wrote it).\n\n" +
			"IDENTITY BY NAME: every use case and actor is identified by its human-readable NAME — you do NOT emit any id. Use case names must be UNIQUE; actor roles must be unique within a use case. Reference the core use case in \"variationOf\" by its name; the server assigns and resolves all internal ids.\n\n" +
			activityDiagramGuide

	case projectstate.KindSystem:
		return "Decompose the system by VOLATILITY into layered components, then validate by drawing the call chains. Bin each volatility with the Four Questions: Who -> Client, What -> Manager, How(activity) -> Engine, How(resource) -> ResourceAccess, Where(state) -> Resource, cross-cutting reuse -> Utility. Each component encapsulates EXACTLY ONE volatility and sits in EXACTLY ONE layer; Component.Layer MUST equal Component.Kind. Obey closed layering: calls go downward only, never upward, never sideways except queued Manager->Manager. REJECT functional decomposition (components named after features) and domain decomposition (components named after entities) — name components after the volatility they hide. Keep it small: order-of-magnitude ~10 components, Managers <=5, fewer Engines than Managers. Emit one dynamicView per CORE use case tracing its call chain (exactly one Manager entered from the Client; every edge labelled in the destination layer's vocabulary, not infrastructure terms). If a use case cannot be drawn cleanly, the DECOMPOSITION is wrong — fix the components, not the use case.\n\nIDENTITY BY NAME: every component is identified by its NAME — you do NOT emit any id, and you do NOT emit a component's layer (it is fixed by its kind and the server derives it). Component names must be UNIQUE. In \"relationships\" and a dynamic view's \"participants\"/\"edges\", reference components by their NAME (the from/to are component names). In each dynamic view set \"useCase\" to the CORE use case's NAME (exactly as it appears in the CoreUseCases context) — do NOT emit a view key; the server derives it. The server resolves every name to its internal id and rejects any name that does not match a component or use case."

	case projectstate.KindOperationalConcepts:
		return "Document the runtime/operational decisions that bring the static architecture to life: communication topology (direct vs message bus), manager-execution infrastructure (in-process vs durable workflow engine), the sync-vs-queued boundary for each cross-component edge (prefer queued for Manager<->Manager), and every pub/sub event (only Clients and Managers may publish or subscribe). Each decision MUST cite the numbered mission objective it serves and state its cost; if a decision cannot be justified against an objective, cut it as gratuitous complexity.\n\n" +
			"Then populate the deployment topology. First declare the system's deliveryStyle (cloud, local, or both). The set of deployment environments is DERIVED from it and a test profile is ALWAYS present: cloud -> {cloud, test}; local -> {local, test}; both -> {cloud, local, test}. Emit exactly that set of environments — no more, no fewer. Each environment nests deploymentNodes (e.g. cluster -> namespace) whose containerInstances reference a real System component BY NAME (set each instance's \"component\" to the System component's NAME exactly as it appears in the System context) — you do NOT emit any component id; the server resolves the name. CROSS-PROFILE INVARIANT: operating mode is configuration, not architecture — the set of deployed components MUST be IDENTICAL across the cloud and local environments (instances are swapped at the durable-execution / client-transport / artifact-target seams, not added or removed). The test environment MUST also include EVERY component — represent external systems as stub instances (present, annotated as stubs) and resources as ephemeral; never OMIT a component from test. Every running component (Client, Manager, Engine, ResourceAccess, Resource) appears in every environment."

	case projectstate.KindStandardCheck:
		return "Walk the App C design-standard checklist. For each guideline emit pass (the design satisfies it), waived (with a concrete justification why it does not apply to THIS system's context), or fail (the design violates it). Key items: no functional or domain decomposition, every component traces to a volatility, Managers do no I/O, cardinality limits respected, closed-layer rules respected. A waiver without a real justification is itself a fail."

	default:
		return "draft the artifact."
	}
}

// kindHasPMCritique reports whether the Method assigns a PM reviewer to this kind
// (mission / glossary+scrubbed / core-use-cases — rework §2.1, §6.6). The
// architect-owned steps (volatilities, architecture, standard-check) skip PM
// critique entirely.
func kindHasPMCritique(kind projectstate.ArtifactKind) bool {
	switch kind {
	case projectstate.KindMission,
		projectstate.KindGlossary,
		projectstate.KindScrubbedRequirements,
		projectstate.KindCoreUseCases:
		return true
	default:
		return false
	}
}

// writePriorsPointer names the committed predecessor artifacts (by kind) the Method
// step draws on, pointing the Action at .aiarch/state/project.json rather than
// embedding model bytes (§0d.2 step 2 — the Action runs in the repo and reads the
// priors by path/kind). An empty list writes nothing.
func writePriorsPointer(b *strings.Builder, kinds ...string) {
	if len(kinds) == 0 {
		return
	}
	fmt.Fprintf(b, "Read these prior committed artifacts from .aiarch/state/project.json as context: %s.\n", strings.Join(kinds, ", "))
}

// writeResearch weaves the Phase-1 research corpus into the mission-draft prompt
// (rework §2.6 / §8). An empty corpus is skipped.
func writeResearch(b *strings.Builder, research projectstate.ResearchInput) {
	if research.IsZero() {
		return
	}
	b.WriteString("\nResearch corpus (the raw material for the mission):\n")
	for _, s := range research.Sources {
		fmt.Fprintf(b, "- %s: %s\n", s.Title, s.Content)
	}
}

// writeFeedback appends a revision-feedback block weaving in the architect's
// free-text Notes (or PM-critique / validation notes) AND each JSONPath-anchored
// comment as a "- at {jsonPath}: {text}" guidance line beneath the notes. An empty
// ReviewFeedback (no notes, no comments) writes nothing. The JSONPath is carried
// verbatim — the server does not evaluate it (systemDesignManager.md §3.2).
func writeFeedback(b *strings.Builder, feedback ReviewFeedback) {
	notes := strings.TrimSpace(feedback.Notes)
	comments := nonEmptyComments(feedback.Comments)
	if notes == "" && len(comments) == 0 {
		return
	}
	b.WriteString("\nThis is a revision. Address the following feedback:\n")
	if notes != "" {
		fmt.Fprintf(b, "%s\n", notes)
	}
	for _, c := range comments {
		fmt.Fprintf(b, "- at %s: %s\n", c.JSONPath, strings.TrimSpace(c.Text))
	}
}

// nonEmptyComments filters out anchored comments with no text — defensive against
// a wire payload that sent an empty comment.
func nonEmptyComments(comments []AnchoredComment) []AnchoredComment {
	out := comments[:0:0]
	for _, c := range comments {
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		out = append(out, c)
	}
	return out
}
