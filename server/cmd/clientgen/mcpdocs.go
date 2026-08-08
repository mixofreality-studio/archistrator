package main

// mcpOpDocs is the per-operation human documentation woven into every generated
// MCP tool description (QA finding F13.1 — replacing the "<Op> on the <X>
// manager." boilerplate). It is keyed by the manager interface name, then the Go
// operation (method) name. project.json .serviceContracts is the structural
// OWNER of every contract, but its committed on-disk form is not the codec's
// canonical output and its `interface` node is replaced wholesale by contractfold
// on a bootstrap re-seed, so operation prose cannot yet live there durably; this
// table is the interim, version-controlled owner of that prose. See the F13
// earmark: move these into interface.operations[].description once schemagen
// harvests method doc comments and contractfold preserves them across a fold.
//
// Every operation on every generated (web-wired) manager MUST have an entry;
// mcpemit.Generate errors if an op has no non-empty doc, so a newly added op
// cannot silently ship boilerplate again.
var mcpOpDocs = map[string]map[string]string{
	"SystemDesignManager": {
		"AcknowledgeStaleBasis":   "Mark a stale committed System-Design artifact 'reviewed — unaffected': clear its stale-basis flag WITHOUT a redraft, recording the reviewer's note as a durable audit entry in the review thread. Use when an upstream change does not actually affect this artifact (so a reconcile amendment would be a byte-identical no-op).",
		"AdvancePhase":            "Advance the project from System Design (phase 1) to Project Design (phase 2). Requires every phase-1 artifact to be committed and reviewed; returns the resulting phase plus any reason the advance was gated.",
		"AskQuestions":            "Ask one or more clarifying QUESTIONS about a System-Design artifact (selected by kind) addressed to a role (pm or architect), WITHOUT sending the draft back for a redraft. The questions are appended to the artifact's review ledger as question-type entries and a lightweight answer job is dispatched so the addressed role answers each in place. Works on a committed artifact too (seeds a question-only thread without opening an amendment). Unlike change-request comments, open questions do NOT block approve.",
		"CreateProject":           "Create a new archistrator project owned by the given owner scope, with the given display name, and return its generated project ID.",
		"GetDesignHealth":         "Return the live Design Health for a project: the mechanical Method-rule findings evaluated render-on-read over the committed system design (each with a rule id, severity, and message), plus the committed waiver and attestation ledgers and the state revision the findings ran against. Read-only.",
		"GetEpisodeTimeline":      "Return one agentic episode's full timeline: its record (usage, cost, outcome, lineage) plus the sequenced trace events mined from its run. Read-only.",
		"GetProject":              "Return the project head-state: its ID, current Method phase, name, owner, and high-level progress.",
		"GetSessionState":         "Return the current draft/review session state for one System-Design artifact (selected by kind): its stage, the latest AI draft, and any review feedback. Read-only.",
		"ListEpisodesForArtifact": "List the agentic episode records (draft/critique/rework runs, or gaps) captured against one System-Design artifact (selected by kind). Read-only.",
		"ListProjects":            "List every project visible to the given owner scope, most-recently-updated first.",
		"RequestArtifactDraft":    "Kick off (or re-run) the AI drafting of one System-Design artifact (selected by kind, e.g. the mission, glossary, or volatilities). Pass feedback to re-draft an existing artifact against review notes. Returns a handle to the asynchronous drafting session.",
		"SetOperatingModel":       "Set the project's operating model — selfOperated (the customer runs the built app in their own infrastructure; the default) or archistratorOperated (archistrator operates the app on the platform, which constrains the deployment design to the platform palette: CNPG Postgres, Temporal, Keycloak, the otel stack, deployed to the platform Kubernetes cluster). Choose at creation, before starting System Design. Returns the new project state version.",
		"SetResearchInput":        "Attach or replace the raw research corpus the architect distils the mission, glossary, and volatilities from. Returns the new project state version.",
		"SetReviewCommentStatus":  "Change the status of one durable review-ledger comment on a System-Design artifact (selected by kind): waive an open comment to dismiss it, or reopen an addressed comment to send it back. Approve is blocked while any comment is still open.",
		"StartSystemDesign":       "Begin the System-Design phase for a project, seeding the artifact spine (mission first). Returns a handle to the kickoff session.",
		"SubmitReviewDecision":    "Record a review verdict (approve / reject / withdraw) on the current draft of a System-Design artifact (selected by kind). Reject and withdraw should carry feedback; approve commits the artifact and unblocks its successors.",
	},
	"ProjectDesignManager": {
		"AcknowledgeStaleBasis":   "Mark a stale committed Project-Design artifact 'reviewed — unaffected': clear its stale-basis flag WITHOUT a redraft, recording the reviewer's note as a durable audit entry in the review thread. Use when an upstream change does not actually affect this artifact (so a reconcile amendment would be a byte-identical no-op).",
		"AdvanceToConstruction":   "Advance the project from Project Design (phase 2) to Construction (phase 3), once the SDP has been committed. Returns the resulting phase plus any reason the advance was gated.",
		"AskQuestions":            "Ask one or more clarifying QUESTIONS about a Project-Design artifact (selected by kind) addressed to a role (pm or architect), WITHOUT sending the draft back for a redraft. The questions are appended to the artifact's review ledger as question-type entries and a lightweight answer job is dispatched so the addressed role answers each in place. Works on a committed artifact too (seeds a question-only thread without opening an amendment). Unlike change-request comments, open questions do NOT block approve.",
		"GetEpisodeTimeline":      "Return one agentic episode's full timeline: its record (usage, cost, outcome, lineage) plus the sequenced trace events mined from its run. Read-only.",
		"GetSessionState":         "Return the current draft/review session state for one Project-Design artifact (selected by kind): its stage, the latest AI draft, and any review feedback. Read-only.",
		"ListEpisodesForArtifact": "List the agentic episode records (draft/critique/rework runs, or gaps) captured against one Project-Design artifact (selected by kind). Read-only.",
		"RequestArtifactDraft":    "Kick off (or re-run) the AI drafting of one Project-Design artifact (selected by kind, e.g. the activity list, project network, or a solution option). Pass feedback to re-draft against review notes. Returns a handle to the asynchronous drafting session.",
		"RequestSDPCommit":        "Assemble the SDP Review (every solution option plus the risk model) for management sign-off. Returns a handle to the assembly session.",
		"SetReviewCommentStatus":  "Change the status of one durable review-ledger comment on a Project-Design artifact (selected by kind): waive an open comment to dismiss it, or reopen an addressed comment to send it back. Approve is blocked while any comment is still open.",
		"SubmitReviewDecision":    "Record a review verdict (approve / reject / withdraw) on the current draft of a Project-Design artifact (selected by kind). Reject and withdraw should carry feedback; approve commits the artifact.",
		"SubmitSDPDecision":       "Record management's decision on the SDP Review: commit one solution option (pass its optionID) or reject all options. Pass feedback to record the rationale.",
	},
	"ConstructionManager": {
		"ExecuteNextActivity":     "Advance construction by one tick: dispatch the next ready activity (or continue an in-flight one) along the project network. tickID idempotently identifies this pump step.",
		"GetEpisodeTimeline":      "Return one agentic episode's full timeline: its record (usage, cost, outcome, lineage) plus the sequenced trace events mined from its run. Read-only.",
		"GetSessionState":         "Return construction progress. With no activityID, the whole-network state; with an activityID, that one activity's detailed lifecycle, build, and review state. Read-only.",
		"ListEpisodesForActivity": "List the agentic episode records (dispatch runs, or gaps) captured against one construction activity. Read-only.",
		"OverrideActivity":        "Manually override one activity's state (e.g. force-complete, reopen, or reassign) — an operator escape hatch outside the normal construction pump.",
		"PauseProject":            "Pause the construction pump for a project so no further activities dispatch until it is resumed. reason is recorded for the audit trail.",
		"RunReplanSweep":          "Run the re-plan sweep that detects scope or variance drift and re-derives the project network. With no projectID it sweeps every active project; tickID idempotently identifies the sweep.",
		"SetReviewPolicy":         "Set the project's construction review-policy preset: vibes (auto-approve everything short of the deploy/spend/schema risk floor), checkpoints (approval at the contract commit + construction dispatch + merge), or full (approval at every step). Any other preset value is rejected.",
		"SubmitPhaseDecision":     "Record a review verdict (approve or send-back) for one construction phase of an activity. Send-back should carry feedback; approve advances the activity's lifecycle.",
		"UpdateReviewPolicy":      "Replace the construction review-routing policy (which reviewers gate which produced artifacts) for a project.",
	},
	"OperationsManager": {
		"ApplyDelinquencyPolicy":  "Apply the billing-delinquency policy to a customer (e.g. suspend or restore their operated systems) given the current delinquency context.",
		"DeployAfterConstruction": "Deploy a desired-state change to an operated application once its construction completes. Returns the deployment outcome.",
		"QueryCostProjection":     "Project the operating cost of an operated application, optionally across scale/what-if points. requestID idempotently identifies the query. Read-only.",
		"QueryDeploymentHealth":   "Return the deployment diagram's per-node health for an operated application: nodes it deploys colour healthy/unhealthy from the live cluster, every other node on the diagram (e.g. the architect's own laptop or browser) reads neutral. Read-only.",
		"QueryOperatedSystemView": "Return the live operated-system view (runtime status and topology) for an operated application. requestID idempotently identifies the query. Read-only.",
		"ReconcileOperatedState":  "Reconcile the actual operated state toward the desired state across the given scope (or everything when omitted). tickID idempotently identifies the reconcile tick.",
		"RegisterOperatedApp":     "Onboard a new operated application by seeding its head-state row — the customer, project, and deployable-bundle reference it will deploy from. Must run once before the first DeployAfterConstruction. Returns the new head-state version.",
		"WithdrawSystem":          "Withdraw and tear down an operated application (identified by its deploy changeID) for the given reason. Returns the withdrawal outcome.",
	},
}

// opDocFor returns an OpDoc lookup bound to one manager interface, for
// mcpemit.Options.OpDoc.
func opDocFor(iface string) func(op string) string {
	m := mcpOpDocs[iface]
	return func(op string) string { return m[op] }
}
