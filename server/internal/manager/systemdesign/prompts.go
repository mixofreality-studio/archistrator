package systemdesign

import (
	"fmt"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
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
func architectDraftPrompt(kind projectstate.ArtifactKind, proj projectstate.Project, feedback ReviewFeedback, reviewThread []projectstate.ReviewComment, amendment int) string {
	var b strings.Builder
	b.WriteString(architectHeader)
	fmt.Fprintf(&b, "Target artifact: %s\n", kind)

	// F38 AMENDMENT: this session REOPENS an already-committed artifact. State that the agent
	// is AMENDING the committed version (its own base — read it from the checked-out state)
	// rather than drafting from scratch, and that the reopening reasons are the OPEN review
	// ledger entries below (the "why").
	if amendment > 0 {
		fmt.Fprintf(&b, "\nThis is an AMENDMENT (revision %d) of the already-COMMITTED %s. Start from the committed version in the checked-out .aiarch/state/project.json and REVISE it to address the reopening feedback — do NOT discard it and redraft from scratch. The specific reasons this artifact was reopened are the OPEN review-ledger comments listed below; address each and record your response per the ledger contract.\n", amendment, kind)
	}

	// Per-kind priors: name the committed predecessor artifacts the Method draws on,
	// by kind (the Action reads them from .aiarch/state/project.json in the repo).
	switch kind {
	case projectstate.KindMission:
		writeResearch(&b, proj.Research)
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
	case projectstate.KindPlanningAssumptions, projectstate.KindActivityList, projectstate.KindNetwork,
		projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution, projectstate.KindCompressedSolution,
		projectstate.KindDecompressedSolution, projectstate.KindRiskModel, projectstate.KindSdpReview:
		// Phase-2 kinds never reach this Phase-1-only prompt assembler (see the
		// doc comment above); no priors pointer to add — same no-op as before
		// this switch was made exhaustive.
	}

	writeFeedback(&b, feedback)
	// REVIEW LEDGER (review-ledger §3): on a redraft, weave in every OPEN durable ledger
	// comment (with its stable id + anchor + anchor-text snapshot) and the response-carrier
	// contract, mirroring the PM-critique carrier language. The agent commits a per-comment
	// response back into the slot's reviewThread; the server reads it back and decides the
	// effective status. Empty on the first draft (no ledger).
	writeReviewLedger(&b, reviewThread)
	fmt.Fprintf(&b, "\nTask: %s\n", draftTask(kind))
	// CLOSED-ENUM DISCIPLINE (QA F36): for kinds whose typed model carries closed enums,
	// enumerate the allowed wire names so the drafting agent never writes free prose into an
	// enum field. The CI validate check will NOT catch such a value (its Go mirror types
	// these enums as free strings — see the methodcheck-vs-codec asymmetry), so the ONLY
	// defense before read-back is telling the agent the exact allowed values here.
	if guide := closedEnumGuide(kind); guide != "" {
		b.WriteString("\n")
		b.WriteString(guide)
	}
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
	// Per-kind critique doctrine — kept in lockstep with draftTask so the
	// draft<->critique loop is CONVERGENT (QA finding F27, founder ruling 2026-07-05).
	// For the Mission the critique enforces exactly what the mission draft prompt now
	// instructs: business/user language only, no component/architecture terminology,
	// no pre-decided decomposition (that is derived later from volatility analysis).
	if kind == projectstate.KindMission {
		b.WriteString("\nMission doctrine you MUST enforce: the mission and vision must describe the BUSINESS CAPABILITY and USER-FACING VALUE in business and user language only. REVISE the draft if it uses the words component, module, service, subsystem, layer, or any other system-architecture / software-decomposition terminology, or if it asserts or implies any breakdown of the system into parts — the structural boundaries are derived LATER from volatility analysis, so pre-deciding a decomposition in the mission is a defect to send back. Do NOT ask the architect to ADD component or architecture language; that is exactly what must be kept out.\n")
	}
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
		return "Produce the mission from the research corpus. The vision is ONE terse sentence naming the future the system creates. First distill the 2-3 business pillars that DIFFERENTIATE this system from competitors; ground the vision, mission, and objectives in those. The mission narrative describes the BUSINESS CAPABILITY and USER-FACING VALUE of the end-to-end workflow — why it matters, and what outcome or trust it produces for the user — NOT a feature list. Write it PURELY in business and user language: you MUST NOT use the words component, module, service, subsystem, layer, or any system-architecture / software-decomposition terminology, and you MUST NOT assert or imply any breakdown of the system into parts. The structural boundaries are derived LATER from volatility analysis in the Structure artifact — pre-deciding a decomposition here is a defect. Each objective is a numbered, measurable BUSINESS outcome (not a feature deliverable)."

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
			"Then populate the deployment topology in C4-container shape. First declare the system's deliveryStyle (cloud, local, or both). The set of deployment environments is DERIVED from it and a test profile is ALWAYS present: cloud -> {cloud, test}; local -> {local, test}; both -> {cloud, local, test}. Emit exactly that set of environments — no more, no fewer. Next declare the top-level \"containers\" array — the deployable UNITS, not the components — each with a \"key\", \"name\", \"technology\", \"description\", and \"components\" listing the exact NAMES of the System components it packages (e.g. an application-server container packages the Managers, Engines, ResourceAccess, and Utilities; a web/SPA container packages the web Client). Every CODE component — every Client, Manager, Engine, and ResourceAccess, plus every Utility — MUST be packaged into EXACTLY ONE container; none may be left out and none may appear in two containers. Resources are NOT container members — they are deployment INFRASTRUCTURE, never packaged: model each Resource (database, queue, external API) as an infrastructureNode (a self-describing name/technology/description) or, for a genuinely external third-party system, as a softwareSystemInstance. The SAME logical Resource may be realized differently per environment (a managed Postgres cluster in cloud vs a local docker/sqlite instance in test) — that per-profile realization detail belongs on the infrastructure node, never on the abstract Resource. Each environment nests deploymentNodes (e.g. cluster -> namespace -> deployment) whose containerInstances reference a declared container BY ITS \"containerKey\" (not a component name) and set an \"instances\" integer for its replica count (e.g. 2); put infrastructureNodes and softwareSystemInstances on whichever deploymentNode they run alongside. CROSS-PROFILE INVARIANT: operating mode is configuration, not architecture — the set of deployed CONTAINERS MUST be IDENTICAL across the cloud and local environments (the underlying infrastructure MAY legitimately differ per profile — a managed database in cloud vs a local one in test is exactly the point of separate environments, not a violation). The test environment MUST instance EVERY container so every code component is covered; represent external systems and resources there as stubs. Reference containers in a deploymentNode's containerInstances by \"containerKey\", and reference System components inside a container's \"components\" list by their NAME exactly as they appear in the System context — you do NOT emit any id for either; the server resolves both by name/key."

	case projectstate.KindStandardCheck:
		return "Walk the App C design-standard checklist. For each guideline emit pass (the design satisfies it), waived (with a concrete justification why it does not apply to THIS system's context), or fail (the design violates it). Key items: no functional or domain decomposition, every component traces to a volatility, Managers do no I/O, cardinality limits respected, closed-layer rules respected. A waiver without a real justification is itself a fail."

	case projectstate.KindPlanningAssumptions, projectstate.KindActivityList, projectstate.KindNetwork,
		projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution, projectstate.KindCompressedSolution,
		projectstate.KindDecompressedSolution, projectstate.KindRiskModel, projectstate.KindSdpReview:
		// Phase-2 kinds are never drafted through this Phase-1-only task table;
		// same generic fallback as the default below.
		return "draft the artifact."

	default:
		return "draft the artifact."
	}
}

// enumConformancePreamble is the closed-enum discipline every enum-bearing draft prompt
// carries (QA F36). The drafted CoreUseCases had free prose written into the "trigger"
// field — a CLOSED ENUM — which the CI validate check accepted (its Go mirror types the
// field as a free string) but the server codec rejected on read-back, stalling the design.
// This preamble names the failure mode explicitly and points the agent at the checked-out
// state as the shape reference; closedEnumGuide then enumerates the per-kind wire names.
const enumConformancePreamble = "SCHEMA CONFORMANCE — the typed JSON you commit MUST conform to this artifact's fixed schema exactly. Read the prior committed artifacts in .aiarch/state/project.json as the reference for the exact field layout. Every ENUM-typed field below accepts ONLY one of its fixed camelCase WIRE NAMES — writing a sentence, a phrase, or any free-text description into an enum field is INVALID and will be REJECTED by the server when it reads your draft back (the CI validate check does NOT catch this, so you alone are responsible for using the exact wire name).\n"

// closedEnumGuide returns the per-kind closed-enum wire-name block woven into the draft
// prompt, or "" for a kind whose drafted model carries no closed enum. The wire names are
// the SINGLE SOURCE OF TRUTH in projectstate/enumjson.go — keep this in lockstep with it
// (prompts_test.go cross-checks each block against the marshalled enum values). Phase-2
// design models (planning-assumptions / activity-list / network / solutions / risk-model)
// carry NO closed enums — their worker-class/risk fields are free strings/ints — so the
// projectdesign prompts need no counterpart block (audited for QA F36).
func closedEnumGuide(kind projectstate.ArtifactKind) string {
	switch kind {
	case projectstate.KindCoreUseCases:
		return enumConformancePreamble +
			"Closed enums in this artifact:\n" +
			"- each use case's \"trigger\" is EXACTLY one of: clientAction, timer, busMessage — the KIND of thing that initiates the use case (a client/user action, a scheduled timer, or an inbound bus/queue message). It is NOT a free-text sentence describing the trigger.\n" +
			"- each use case's \"classification\" is EXACTLY one of: core, nonCore.\n" +
			"- activity-diagram node \"kind\" and edge \"kind\" use the wire names given in the ACTIVITY DIAGRAM section above.\n"
	case projectstate.KindVolatilities:
		return enumConformancePreamble +
			"Closed enums in this artifact:\n" +
			"- each volatility's \"axis\" is EXACTLY one of: sameCustomerOverTime, allCustomersAtOneTime.\n"
	case projectstate.KindSystem:
		return enumConformancePreamble +
			"Closed enums in this artifact:\n" +
			"- each component's \"kind\" is EXACTLY one of: client, manager, engine, resourceAccess, resource, utility (do NOT emit a component \"layer\" — the server derives it from the kind).\n" +
			"- each relationship's \"mode\" is EXACTLY one of: sync, queued, eventPubSub.\n"
	case projectstate.KindOperationalConcepts:
		return enumConformancePreamble +
			"Closed enums in this artifact:\n" +
			"- the system \"deliveryStyle\" is EXACTLY one of: cloud, local, both.\n" +
			"- each deployment environment \"profile\" is EXACTLY one of: cloud, local, test.\n" +
			"- each cross-component edge \"mode\" is EXACTLY one of: sync, queued, eventPubSub.\n"
	case projectstate.KindStandardCheck:
		return enumConformancePreamble +
			"Closed enums in this artifact:\n" +
			"- each checklist item's \"status\" is EXACTLY one of: pass, waived, fail.\n"
	case projectstate.KindMission, projectstate.KindGlossary, projectstate.KindScrubbedRequirements,
		projectstate.KindPlanningAssumptions, projectstate.KindActivityList, projectstate.KindNetwork,
		projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution, projectstate.KindCompressedSolution,
		projectstate.KindDecompressedSolution, projectstate.KindRiskModel, projectstate.KindSdpReview:
		// No closed enum in the drafted model — no enum block.
		return ""
	default:
		return ""
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
	case projectstate.KindVolatilities, projectstate.KindSystem, projectstate.KindOperationalConcepts,
		projectstate.KindStandardCheck, projectstate.KindPlanningAssumptions, projectstate.KindActivityList,
		projectstate.KindNetwork, projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution,
		projectstate.KindCompressedSolution, projectstate.KindDecompressedSolution, projectstate.KindRiskModel,
		projectstate.KindSdpReview:
		// Architect-owned Phase-1 steps (no PM critique) and all Phase-2 kinds
		// (Phase 2 has no PM-critique step at all) — same as the default below.
		return false
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

// writeResearch POINTS the mission-draft prompt at the Phase-1 research corpus
// committed in .aiarch/state/project.json rather than INLINING the source content
// (rework §2.6 / §8; QA finding F11). The corpus is already committed at the JSON path
// .research.Sources[] on the checked-out project state (the Action runs IN the repo and
// prior_state_ref is always empty ⇒ github.ref, the default branch, which carries the
// committed research from the very first SetResearchInput). Inlining a book-sized corpus
// blew both the Temporal workflow-payload budget (TMPRL1103) and GitHub's 64KB
// workflow_dispatch input cap (422 ContractMisuse), making system design impossible. We
// UNIFORMLY point — never inline, no size cliff — listing only each source's short TITLE
// so the drafting agent knows what is there and can read the full text by title. An empty
// corpus is skipped (IsZero guard preserved).
func writeResearch(b *strings.Builder, research projectstate.ResearchCorpus) {
	if research.IsZero() {
		return
	}
	// F42: the corpus content lives as FILES in the checked-out repo (not inlined in
	// project.json). Point the drafting Action straight at each source's file path — simpler
	// for the agent than a JSON path, and the content never rides this prompt.
	b.WriteString("\nResearch corpus (the raw material for the mission): read the full text of each source from its FILE in the checked-out repository. Do NOT expect the content inline here. The sources present (title → file path) are:\n")
	for _, s := range research.Sources {
		fmt.Fprintf(b, "- %s → %s\n", s.Title, s.Path)
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

// writeReviewLedger weaves the OPEN durable review-ledger comments into a redraft prompt
// and states the response-carrier contract the drafting agent must honor (review-ledger §3).
// It mirrors the PM-critique carrier (pmCritiquePrompt): the agent does NOT invent or reorder
// comments — it commits a per-comment "response" (and a proposed "addressed" status) back onto
// the SAME slot's "reviewThread" array in .aiarch/state/project.json, matched by the stable
// comment "id". The server, not the agent, decides the effective status on read-back (an empty
// response keeps a comment open — so a comment the agent cannot honestly address must be left
// with an empty response or a reasoned pushback the human then waives). Nothing is written when
// no comment is open (the common first-draft case).
func writeReviewLedger(b *strings.Builder, thread []projectstate.ReviewComment) {
	var open []projectstate.ReviewComment
	for _, c := range thread {
		if c.Status == projectstate.ReviewCommentOpen && strings.TrimSpace(c.Text) != "" {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return
	}
	b.WriteString("\nThis artifact has OPEN reviewer comments in its durable review ledger. For EACH open comment listed below you MUST: (1) revise the draft to address it; and (2) in .aiarch/state/project.json, on this artifact's slot, in its \"reviewThread\" array, find the entry with the matching \"id\" and set its \"response\" to how you addressed it (or a concise, reasoned pushback if you disagree), and set its \"status\" to \"addressed\". Do NOT add, delete, reorder, or renumber reviewThread entries, and do NOT modify entries not listed here. A comment whose \"response\" you leave empty STAYS OPEN and blocks approval — so respond to every one.\n")
	for _, c := range open {
		anchor := c.Anchor
		if strings.TrimSpace(anchor) == "" {
			anchor = "(whole artifact)"
		}
		if strings.TrimSpace(c.AnchorText) != "" {
			fmt.Fprintf(b, "- comment %s at %s (%q): %s\n", c.ID, anchor, c.AnchorText, strings.TrimSpace(c.Text))
		} else {
			fmt.Fprintf(b, "- comment %s at %s: %s\n", c.ID, anchor, strings.TrimSpace(c.Text))
		}
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
