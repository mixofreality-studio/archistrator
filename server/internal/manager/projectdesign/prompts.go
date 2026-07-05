package projectdesign

import (
	"fmt"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// The Manager OWNS the per-step Phase-2 prompt corpus (mirroring systemdesign's
// prompts.go, the agentic-pivot D-MPD-Δ §0.5.2). The fixed Phase-2 Method sequence
// drives WHICH role-prompt the Manager sends at each step — that is the
// ProjectDesignPhaseWorkflow volatility (the sequence), made explicit.
//
// Each prompt is plain text composed IN-MEMORY by the Manager and shipped as a
// DISPATCH INPUT to the claude-code-action DESIGN job (§0.5.2 step 2 — never
// aiarch-persisted). It carries a role header, the target artifact kind, a pointer to
// the prior committed state BY PATH/KIND (the Action runs IN the user's repo and reads
// .aiarch/state/ directly — priors are NOT embedded as bytes), and (optionally) a
// feedback block woven in verbatim on a redraft. The Action drafts the typed JSON into
// .aiarch/state/ and the required CI validation check enforces its shape — the
// schema/DTO injection the old in-process worker needed is GONE (validation is the CI
// check, §0.5.5).
//
// Phase 2 has ONLY architect-role draft prompts (one per draftable Phase-2 artifact
// kind — planning-assumptions / activity-list / network / the four solutions /
// risk-model). There is NO PM critique in Phase 2 (the SDP review is the architect's
// recommendation to management; the human architect is the gate). The SDP-review
// artifact itself is ASSEMBLED deterministically by the workflow from the three Engine
// outputs (workflow.go), not drafted by the worker — so it has no prompt.

const architectHeader = "You are the Architect agent drafting a typed Phase-2 (Project Design) Method artifact for an architecture project, following Juval Lowy's The Method to the letter. You are running inside the project repository; read the prior committed Method artifacts from .aiarch/state/project.json and commit your drafted artifact back into .aiarch/state/.\n"

// architectDraftPrompt assembles the architect-role draft prompt for the given Phase-2
// artifact kind. It points the Action at the prior committed state by path/kind (NOT
// embedded bytes — the Action reads .aiarch/state/ in the repo), carries the Method
// drafting doctrine, and weaves in any rejection feedback. The composed prompt is the
// DESIGN job's design_prompt dispatch input. (The proj parameter is retained for
// signature parity / future per-kind prior selection; priors are named by kind, not
// embedded.)
func architectDraftPrompt(kind projectstate.ArtifactKind, proj projectstate.Project, feedback string, reviewThread []projectstate.ReviewComment, amendment int) string {
	var b strings.Builder
	b.WriteString(architectHeader)
	fmt.Fprintf(&b, "Target artifact: %s\n", kind)

	// F38 AMENDMENT: this session REOPENS an already-committed Phase-2 artifact. Revise the
	// committed version (its own base) rather than drafting from scratch; the reopening
	// reasons are the OPEN review-ledger comments below.
	if amendment > 0 {
		fmt.Fprintf(&b, "\nThis is an AMENDMENT (revision %d) of the already-COMMITTED %s. Start from the committed version in the checked-out .aiarch/state/project.json and REVISE it to address the reopening feedback — do NOT discard it and redraft from scratch. The reasons this artifact was reopened are the OPEN review-ledger comments listed below; address each and record your response per the ledger contract.\n", amendment, kind)
	}

	// Per-kind priors: name the committed predecessor artifacts the Method draws on, by
	// kind (the Action reads them from .aiarch/state/project.json in the repo). The
	// architecture (Phase-1 System slot) anchors the activity list; planning assumptions
	// anchor the network and the solutions.
	switch kind {
	case projectstate.KindPlanningAssumptions:
		writePriorsPointer(&b, "System (architecture)")
	case projectstate.KindActivityList:
		writePriorsPointer(&b, "System (architecture)", "PlanningAssumptions")
	case projectstate.KindNetwork:
		writePriorsPointer(&b, "ActivityList", "PlanningAssumptions")
	case projectstate.KindNormalSolution,
		projectstate.KindSubcriticalSolution,
		projectstate.KindCompressedSolution,
		projectstate.KindDecompressedSolution:
		writePriorsPointer(&b, "PlanningAssumptions", "ActivityList", "Network")
	case projectstate.KindRiskModel:
		writePriorsPointer(&b, "Network", "NormalSolution", "DecompressedSolution", "SubcriticalSolution", "CompressedSolution")
	case projectstate.KindMission, projectstate.KindGlossary, projectstate.KindScrubbedRequirements,
		projectstate.KindVolatilities, projectstate.KindCoreUseCases, projectstate.KindSystem,
		projectstate.KindOperationalConcepts, projectstate.KindStandardCheck, projectstate.KindSdpReview:
		// Phase-1 kinds (and the deterministically-assembled SdpReview, see the
		// package doc above) never reach this Phase-2-only prompt assembler; no
		// priors to add — same no-op as before this switch was made exhaustive.
	}

	writeFeedback(&b, feedback)
	// REVIEW LEDGER (review-ledger §3): on a redraft, weave in every OPEN durable ledger
	// comment (with its stable id + anchor + anchor-text snapshot) and the response-carrier
	// contract, mirroring the critique carrier. Empty on the first draft (no ledger).
	writeReviewLedger(&b, reviewThread)
	fmt.Fprintf(&b, "\nTask: %s\n", draftTask(kind))
	// TYPED-SHAPE DISCIPLINE (QA F36 Phase-2 sibling): for each drafted Phase-2 kind whose
	// typed model has fields where an LLM would plausibly guess a RICHER SHAPE than the codec
	// accepts (an array of objects where []string is expected, a nested object where a scalar
	// is expected, an array where a string-keyed map is expected), enumerate those hotspots so
	// the drafting agent commits the exact shape. The CI validate check did NOT catch the live
	// incident (PlanningAssumptions.resources drafted as objects — a terminal read-back decode
	// failure) because its Go mirror types these fields loosely; the exact-shape instruction
	// here is the only defense before the server codec reads the draft back.
	if guide := shapeGuide(kind); guide != "" {
		b.WriteString("\n")
		b.WriteString(guide)
	}
	// OPERATING-MODEL CONSTRAINT (founder ruling 2026-07-05): when the project is
	// archistrator-operated the PlanningAssumptions launch infrastructure is
	// CONSTRAINED to the platform palette (CNPG Postgres, Temporal, Keycloak, the otel
	// stack, deployed to the platform k8s cluster via ArgoCD at software/k8s). This is
	// the Phase-2 sibling of the systemDesign OperationalConcepts constraint — the
	// deployment topology (Phase-1) and the launch infrastructure assumptions (Phase-2)
	// must agree. Self-operated emits nothing (today's open guidance is preserved).
	if kind == projectstate.KindPlanningAssumptions {
		if c := operatingModelInfrastructureConstraint(proj.OperatingModel); c != "" {
			b.WriteString("\n")
			b.WriteString(c)
		}
	}
	return b.String()
}

// operatingModelInfrastructureConstraint returns the launch-infrastructure constraint
// the PlanningAssumptions draft prompt carries for the project's operating model
// (founder ruling 2026-07-05). Archistrator-operated CONSTRAINS the launch
// infrastructure to the archistrator-platform palette ONLY and FORBIDS bespoke cloud;
// self-operated (the default) emits nothing (today's open guidance stands).
func operatingModelInfrastructureConstraint(m projectstate.OperatingModel) string {
	if m.OrDefault() != projectstate.OperatingModelArchistratorOperated {
		return ""
	}
	return "OPERATING MODEL — ARCHISTRATOR-OPERATED (platform-constrained infrastructure). " +
		"This project is OPERATED BY ARCHISTRATOR on the shared platform, so the launch-infrastructure assumption is FIXED, not a choice: the app runs on the archistrator-platform palette ONLY. " +
		"When you capture the launch infrastructure assumption you MUST assume EXACTLY these platform building blocks and MUST NOT assume any bespoke or third-party cloud infrastructure:\n" +
		"- Data / persistence: CloudNativePG (CNPG) Postgres — the framework-go-infrastructure-postgres module.\n" +
		"- Workflows / durable execution: Temporal — the framework-go-infrastructure-temporal module (the SHARED platform Temporal at software/k8s/shared/temporal).\n" +
		"- Authentication / identity: Keycloak — the framework-go-infrastructure-keycloak module (software/k8s/argocd/auth).\n" +
		"- Observability: the OpenTelemetry stack — the framework-go-infrastructure-otel module.\n" +
		"- Deploy target: the platform Kubernetes cluster via the ArgoCD stack at software/k8s (namespaces/apps under k8s/argocd/applications).\n" +
		"FORBIDDEN for this operating model: AWS (RDS, EKS, ECS, CloudFront, S3, Lambda), GCP, Azure, or any other bespoke / self-managed / third-party-managed cloud infrastructure or hosting — those are legitimate ONLY for self-operated projects. The launch infrastructure is the platform cluster; there is no per-project cloud-provider decision to assume."
}

// schemaConformancePreamble is the general typed-shape discipline every drafted Phase-2
// prompt carries (QA F36 Phase-2 sibling). It mirrors systemdesign's enumConformancePreamble
// but targets SHAPE rather than closed-enum wire names: the failure it prevents is a drafted
// value whose SHAPE (object vs scalar vs array vs string-keyed map) diverges from the typed
// codec. It points the drafting agent at the authoritative typed schema it can actually read
// in its checkout — the JSON Schema embedded in the committed .serviceContracts $defs blocks —
// AND at the already-committed prior artifacts as worked examples of the same layout.
// shapeGuide then appends the per-kind hotspot lines.
const schemaConformancePreamble = "SCHEMA CONFORMANCE — the typed JSON you commit MUST conform to this artifact's fixed schema EXACTLY. The authoritative shape is the typed JSON Schema committed in this repo's .aiarch/state/project.json under .serviceContracts — each component's \"$defs\" block (the Phase-2 model shapes are under .serviceContracts.projectStateAccess.$defs; the Network shape is under .serviceContracts.estimationEngine.$defs), and the already-committed prior artifacts in the same file are worked examples of the same layout. Conform EXACTLY: do NOT invent a nested object where a scalar or an array-of-scalars is expected, do NOT wrap a bare number in an object, and do NOT turn a string-keyed map into an array of objects. A shape the schema does not declare will be REJECTED by the server codec when it reads your draft back (the CI validate check did NOT catch this in the live incident that motivated this guidance — you alone are responsible for the exact shape). Typed-shape hotspots for this artifact:\n"

// shapeGuide returns the per-kind typed-shape hotspot block woven into the draft prompt, or
// "" for a kind that is not agent-drafted in Phase 2 (SdpReview is assembled deterministically
// by the workflow, not drafted — see the package doc above) or carries no shape trap. The
// hotspots are DERIVED FROM the projectstate Go types (contract.gen.go): []string vs
// []object, Money{minorUnits,currency} vs a bare number, string-keyed maps vs arrays, and the
// Network compute-at-read block that must not be authored. Keep this in lockstep with those
// types — prompts_test.go cross-checks representative hotspots against the marshalled/reflected
// type definitions to prevent drift.
func shapeGuide(kind projectstate.ArtifactKind) string {
	switch kind {
	case projectstate.KindPlanningAssumptions:
		return schemaConformancePreamble +
			"- \"resources\" is an array of STRINGS — the plain NAMES of the staff/resources (e.g. [\"Alice\",\"Bob\",\"Contractor-1\"]). It is NOT an array of objects; do NOT give a resource a nested {name, role, rate, ...} shape. (This exact field caused a terminal read-back decode failure when drafted as objects.)\n" +
			"- \"calendarDaysPerWeek\" is a single NUMBER (e.g. 5), not an object.\n" +
			"- \"indirectDailyRate\" is a Money OBJECT {\"minorUnits\": <integer minor units>, \"currency\": \"USD\"} — NOT a bare number and NOT a formatted string like \"$500\".\n" +
			"- \"rateCard\" is a STRING-KEYED MAP of worker-class name -> {\"modelId\", \"megatokensInPerDay\", \"megatokensOutPerDay\"} — an OBJECT keyed by class name, NOT an array of {class, ...} objects.\n" +
			"- \"declaredUsage\" and \"terms\" are each a single nested OBJECT (UsageAssumption / SettlementTerms) of scalar fields — not arrays.\n"
	case projectstate.KindActivityList:
		return schemaConformancePreamble +
			"- the artifact is an OBJECT {\"activities\": [ ... ]} — the activities live under the \"activities\" key; it is NOT a bare top-level array.\n" +
			"- each activity's \"effortDays\" is a NUMBER of person-days (e.g. 10), not an object like {\"value\":10,\"unit\":\"days\"}.\n" +
			"- \"riskBucket\" is a single INTEGER from the Fibonacci set (1,2,3,5,8,13), not an object and not a label string like \"high\".\n" +
			"- \"coding\" is a boolean; \"name\", \"workerClass\", \"title\" are plain strings.\n"
	case projectstate.KindNetwork:
		return schemaConformancePreamble +
			"- \"dependencies\" is an array of {\"activity\": <name string>, \"dependsOn\": [<name string>, ...]} — \"dependsOn\" is an array of plain activity-NAME STRINGS, not an array of objects.\n" +
			"- \"criticalPath\" is an array of plain activity-NAME STRINGS, not an array of objects.\n" +
			"- each milestone's \"dependsOn\" is likewise an array of predecessor activity-id STRINGS.\n" +
			"- Do NOT author the COMPUTED block: \"computed\", \"summary\", and each milestone's \"onCriticalPath\"/\"eventTime\" are filled in by the server at READ time — omit them entirely (authoring them is wrong).\n"
	case projectstate.KindNormalSolution,
		projectstate.KindSubcriticalSolution,
		projectstate.KindCompressedSolution,
		projectstate.KindDecompressedSolution:
		return schemaConformancePreamble +
			"- \"classRates\" is a STRING-KEYED MAP of worker-class name -> Money OBJECT {\"minorUnits\": <integer minor units>, \"currency\": \"USD\"} — an object keyed by class name whose VALUES are Money objects. It is NOT an array, and its values are NOT bare numbers.\n" +
			"- \"staffingCap\" is an INTEGER; \"calendarDaysPerWeek\", \"bufferDays\", \"criticalSpeedup\" are plain NUMBERS — none of them is an object.\n"
	case projectstate.KindRiskModel:
		return schemaConformancePreamble +
			"- \"rows\" is an array of per-option objects; each row's \"totalCost\" is a Money OBJECT {\"minorUnits\": <integer minor units>, \"currency\": \"USD\"}, NOT a bare number.\n" +
			"- \"criticalityRisk\", \"activityRisk\", \"composite\", \"durationDays\" and the \"tooRiskyThreshold\"/\"overSafeThreshold\"/\"maxCompressionPct\" thresholds are plain NUMBERS — not objects or percentage strings.\n" +
			"- \"included\" is a boolean; \"exclusionReason\" is a plain string.\n"
	case projectstate.KindMission, projectstate.KindGlossary, projectstate.KindScrubbedRequirements,
		projectstate.KindVolatilities, projectstate.KindCoreUseCases, projectstate.KindSystem,
		projectstate.KindOperationalConcepts, projectstate.KindStandardCheck, projectstate.KindSdpReview:
		// Phase-1 kinds never reach this Phase-2-only assembler, and the SdpReview is
		// ASSEMBLED deterministically by the workflow (not drafted, see the package doc) —
		// so neither gets a shape block. Same no-op as the default below.
		return ""
	default:
		return ""
	}
}

// writeReviewLedger weaves the OPEN durable review-ledger comments into a redraft prompt and
// states the response-carrier contract the drafting agent must honor (review-ledger §3): the
// agent commits a per-comment "response" (and proposed "addressed" status) back onto the SAME
// slot's "reviewThread" array in .aiarch/state/project.json, matched by the stable comment
// "id". The server, not the agent, decides the effective status on read-back (empty response
// keeps a comment open). Nothing is written when no comment is open (the first-draft case).
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

// draftTask returns the per-kind task instruction.
func draftTask(kind projectstate.ArtifactKind) string {
	switch kind {
	case projectstate.KindPlanningAssumptions:
		return "capture the explicit planning assumptions — the resources, working calendar (days/week), launch infrastructure, the customer's declared usage, and the settlement terms — that the project network and the SDP-review estimates are built on."
	case projectstate.KindActivityList:
		return "convert the architecture into the activity list. Emit exactly ONE coding activity per component of the committed System, named after that component — detailed design and construction are internal lifecycle phases of that single activity (a per-phase role hand-off), NOT separate network nodes; do NOT split a component into a D### design activity and a C### construction activity in the base list. Integration (I-*) and noncoding (N-*) activities — test plan, test harness, environment setup, etc. — are separate activities. Give each activity its effort in 5-day quanta, its worker class, and a Fibonacci risk bucket."
	case projectstate.KindNetwork:
		return "convert the activity list into a project network: declare each activity's predecessor dependencies and identify the critical path (the activity names on it)."
	case projectstate.KindNormalSolution:
		return "design the NORMAL solution: minimum staffing for unimpeded critical-path progress; set the staffing cap, calendar days/week, and per-worker-class build-cost rates. Zero schedule buffer."
	case projectstate.KindDecompressedSolution:
		return "design the DECOMPRESSED-NORMAL solution: extend the normal duration with a schedule buffer to drop criticality risk toward the tipping point without cutting staff. Set bufferDays > 0."
	case projectstate.KindSubcriticalSolution:
		return "design the SUBCRITICAL solution: deliberately understaffed (lower the staffing cap below normal). It is counterintuitively longer, costlier, and riskier — the point is to disprove the 'fewer people = cheaper' intuition for management."
	case projectstate.KindCompressedSolution:
		return "design the COMPRESSED solution: shorter duration via parallel work first and top resources second; raise the staffing cap and/or calendar days/week. Target a modest compression, stopping short of the death zone."
	case projectstate.KindRiskModel:
		return "quantify and compare risk across the four options: for each, decompose criticality risk and activity risk into a composite score for the SDP-review time-risk curve."
	case projectstate.KindMission, projectstate.KindGlossary, projectstate.KindScrubbedRequirements,
		projectstate.KindVolatilities, projectstate.KindCoreUseCases, projectstate.KindSystem,
		projectstate.KindOperationalConcepts, projectstate.KindStandardCheck, projectstate.KindSdpReview:
		// Phase-1 kinds (and the deterministically-assembled SdpReview) are never
		// drafted through this Phase-2-only task table; same generic fallback.
		return "draft the artifact."
	default:
		return "draft the artifact."
	}
}

// writePriorsPointer names the committed predecessor artifacts (by kind) the Action
// should read from .aiarch/state/project.json — NOT embedded as bytes.
func writePriorsPointer(b *strings.Builder, kinds ...string) {
	if len(kinds) == 0 {
		return
	}
	fmt.Fprintf(b, "Prior committed artifacts to read from .aiarch/state/project.json: %s\n", strings.Join(kinds, ", "))
}

// writeFeedback appends a revision-feedback block (architect rejection notes)
// verbatim.
func writeFeedback(b *strings.Builder, feedback string) {
	notes := strings.TrimSpace(feedback)
	if notes == "" {
		return
	}
	b.WriteString("\nThis is a revision. Address the following feedback:\n")
	fmt.Fprintf(b, "%s\n", notes)
}
