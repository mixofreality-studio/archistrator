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
	"DeliveryManager": {
		"AcknowledgeStaleBasis":     "Mark a stale committed artifact 'reviewed — unaffected': clear its stale-basis flag WITHOUT a redraft, recording the reviewer's note as a durable audit entry in the review thread. Use when an upstream change does not actually affect this artifact (so a reconcile amendment would be a byte-identical no-op). The activity's rail decides which artifact the task names.",
		"AskQuestions":              "Ask one or more clarifying QUESTIONS about one task's artifact, addressed to a role (pm or architect), WITHOUT sending the draft back for a redraft. The questions are appended to the task's review ledger as question-type entries and a lightweight answer job is dispatched so the addressed role answers each in place. Works on a committed artifact too (seeds a question-only thread without opening an amendment). Unlike change-request comments, open questions do NOT block approve.",
		"DispatchActivityTask":      "Kick off (or re-run) one task of one activity: the AI drafting of the artifact the task produces, or — at the project-design activity's single M0 gate — the assembly of the SDP Review from every solution option plus the risk model. Pass feedback to re-draft against review notes. Returns a handle to the asynchronous session.",
		"ExecuteNextActivity":       "Advance the project by one delivery tick: dispatch the next ready activity (or continue an in-flight one) along the committed project network. tickID correlates this request; the pump is one per project — a call while it runs joins it. Refused as FailedPrecondition while the project is paused: resume it with SetProjectRunState.",
		"OverrideActivity":          "Steer one activity that is waiting at an escalation: retry it, skip it, take it over, or reassign it. Notes are required. Refused as FailedPrecondition while the activity is not awaiting a takeover.",
		"QueryActivityView":         "Return one activity's whole lifecycle in one read: its task DAG (every task, its dependencies, its state) grouped into the lifecycle phases and their earned-value weights, each task's revision history, and the reviewer set at the gate it is waiting at. A review revision backed by a persisted round carries that round's verdicts, comment thread with replies and resolutions, reviewer roster, subject and round number, and who decided it and when; one reconstructed from a row that predates the round ledger carries the episode, send-back note and anchored comments behind it instead, and says so in its provenance. Read-only.",
		"QueryProjectView":          "Return one composed view of a project, selected by kind: summary (head state), projects (every project for an owner), session (the live draft/review session for one artifact kind or one activity), pump (the delivery pump's dispatch status), designHealth (the live Method-rule findings), episodes (the agentic episode records for one artifact kind or activity) or timeline (one episode's full trace). Read-only; the query object carries the selector each kind needs.",
		"ReplanProject":             "Run the re-plan sweep that detects scope or variance drift and re-derives the project network. With no projectID it sweeps every active project; tickID idempotently identifies the sweep.",
		"SetProjectExecutionPolicy": "Set how much of the project's work is gated by a human. Pass a preset — vibes (auto-approve everything short of the deploy/spend/schema risk floor), checkpoints (approval at the contract commit, the construction dispatch and the merge), or full (approval at every step) — or an explicit policy that replaces the review routing outright.",
		"SetProjectRunState":        "Pause or resume the project's delivery pump. Pausing stops any further activity dispatching and records the reason for the audit trail; resuming clears the recorded pause and starts (or joins) the pump, so work continues within 30 seconds. Refused as FailedPrecondition while a pause is still being applied (retry in a moment).",
		"StartProject":              "Create a project and start it, in one op: with no projectID it creates the project under the given owner and name; an operating model (selfOperated, the default, or archistratorOperated, which constrains the deployment design to the platform palette) and a research corpus are attached when given; start begins the first design activity. Returns the project id, the resulting state version, and a handle to the kickoff session when one was started.",
		"SubmitReviewDecision":      "Record a human decision on one review task of one activity: approve, send back with change requests, withdraw, advance past the phase seal, or set one review comment's status. The decision object carries the extras a particular decision needs — the chosen option id at the M0 spend gate, the comment id and status for a comment transition, and whether a stale basis is being acknowledged. This is the single write behind every gate in the product.",
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
