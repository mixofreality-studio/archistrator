package construction

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// pipelineSpec is the Manager's infrastructure-neutral dispatch spec.
type pipelineSpec struct {
	ProjectID   ProjectID
	ActivityID  string
	ComponentID string
	RepoURL     string
	Ref         string
	// Phase is the ActivityMethodPhase.String() for the current activity phase.
	Phase string
	// Type/Variant are the activity's classification, COPIED from the dispatched
	// constructionActivity (classified once by the pump) rather than re-derived from
	// the id here. dispatchInputsFor resolves the slash command from this pair, so the
	// command is by construction drawn from the same pair the phase profile came from.
	Type    projectstate.ActivityType
	Variant projectstate.TestingVariant
	// OperatorNote is the rendered block of the operator notes this agent dispatch carries
	// (renderOperatorNotes, plan B1.4); empty when none is pending.
	OperatorNote string
}

// pipelineObservation is the Manager's neutral pipeline observation.
type pipelineObservation struct {
	Phase      PipelinePhase
	Diagnostic string
	// RunURL is the dispatched run's URL when the bound agenticJobAccess realisation
	// resolved one. It is carried for ONE reason here (construction has no run-link
	// view): it is the only VENUE signal a workflow ever sees — the GitHub-Actions arm
	// stamps the run's html URL on every observation, while the local executor and the
	// dry-run stub never set it. See episodeVenueIsRemote.
	RunURL string
	// Episode is the terminal run's captured agentic-episode summary (SP1 capture-seam):
	// the tokens/turns/tools the dispatched agent actually burned. Nil on every
	// non-terminal observation, on the GitHub-Actions arm (which mines no episode in
	// v1), on a non-agentic job (the local merge job spawns no agent), and — legitimately
	// — on a CANCELLED run's FIRST terminal observation, whose summary lands only once
	// the subprocess has unwound (see awaitLateEpisode).
	Episode *agenticjob.EpisodeSummary
}

// pipelineDefaultToolchain is the single logical build step the Manager's neutral
// pipelineSpec implies (the image map resolves it to a concrete image).
const pipelineDefaultToolchain = "go-1.23"

// constructWorkflowFileName is the per-project CONSTRUCTION workflow file the agentic
// construction job dispatches into (the gh-mode venue switch, B5). It rides on the
// contract PipelineSpec.WorkflowFile alongside the per-project TargetRepo; the RA's
// resolveTarget falls back to the configured central construction workflow file when
// this (and TargetRepo) is zero. The scaffold seats this file in every app repo (B4);
// archistrator's own repo keeps its hand-maintained copy (self-hosting divergence).
const constructWorkflowFileName = "aiarch-construct.yml"

// constructRepoTarget resolves the per-project construction venue: it runs the injected
// Repo resolver and DECODES the opaque RepoRef into the RA's infrastructure-neutral
// RepoTarget{Owner,Name} + the construct workflow file. A nil/unresolving resolver
// yields a ZERO RepoTarget + empty workflow file, so submitPipeline leaves the contract
// fields zero and the RA falls back to the configured central construction repo (the
// pre-B5 legacy behavior, preserved for unresolvable projects). A malformed RepoRef
// surfaces the RA's ContractMisuse — decoded via sourcecontrol's own OwnerRepo accessor
// so the RepoRef encoding stays owned by sourceControlAccess (no encoding leak here).
func (wf *workflows) constructRepoTarget(projectID ProjectID) (agenticjob.RepoTarget, string, error) {
	if wf.Repo == nil {
		return agenticjob.RepoTarget{}, "", nil
	}
	repoRef, ok := wf.Repo(projectID)
	if !ok {
		return agenticjob.RepoTarget{}, "", nil
	}
	owner, name, err := sourcecontrol.RepoRefOwnerRepo(repoRef)
	if err != nil {
		return agenticjob.RepoTarget{}, "", err
	}
	return agenticjob.RepoTarget{Owner: owner, Name: name}, constructWorkflowFileName, nil
}

// dispatchInputsFor builds the DispatchInputs bag for a construction pipeline dispatch.
// The `command` input is the thin slash-command the workflow runs; it is computed here
// from the activity's CARRIED type/variant (classified once by the pump, copied onto the
// spec) and the current phase, so the workflow itself holds no routing logic and no
// second derivation can disagree with the phase profile being walked. component_id is a
// Manager-resolved passthrough. (Moved workflow-side from the retired pipelineAdapter —
// it only reads workflow state + projectstate.CommandFor.)
func dispatchInputsFor(spec pipelineSpec) map[string]string {
	m := map[string]string{
		"activity_id":  spec.ActivityID,
		"component_id": spec.ComponentID,
	}
	if spec.Phase != "" {
		m["phase"] = spec.Phase
		m["command"] = projectstate.CommandFor(spec.Type, spec.Variant, projectstate.ActivityMethodPhase(spec.Phase))
	}
	// The operator's steer rides ONLY when a note is pending (B1.4): every no-note
	// dispatch's inputs stay byte-identical to before, so a seated workflow that predates
	// the operator_note input still accepts them. Both arms read the same key: the
	// GitHub arm passes it through as the construct workflow's input, the local arm
	// stamps it into the aiarch-state rig.
	if spec.OperatorNote != "" {
		m[dispatchInputOperatorNote] = spec.OperatorNote
	}
	return m
}

// dispatchInputOperatorNote is the dispatch input carrying the operator's notes. Like the
// other inputs it is a bare literal: the seated construct workflow template is the source
// of truth for the wire key (agenticjob's own constant documents it on the RA side).
const dispatchInputOperatorNote = "operator_note"

// managerPipelinePhase maps the contract PipelinePhase onto the Manager-neutral
// PipelinePhase (mapped here so a future re-order is safe). Moved workflow-side from the
// retired pipelineAdapter.
func managerPipelinePhase(p agenticjob.PipelinePhase) PipelinePhase {
	switch p {
	case agenticjob.PhasePending:
		return PipelinePending
	case agenticjob.PhaseRunning:
		return PipelineRunning
	case agenticjob.PhaseSucceeded:
		return PipelineSucceeded
	case agenticjob.PhaseFailed:
		return PipelineFailed
	case agenticjob.PhaseCancelled:
		return PipelineCancelled
	default:
		return PipelinePhaseUnknown
	}
}

// submitPipeline composes the contract PipelineSpec (default toolchain / single build step
// / workspaceRef / dispatch inputs) from the Manager's neutral pipelineSpec and calls the
// GENERATED submit invoker, mapping the opaque handle back to the neutral pipelineHandle.
func (wf *workflows) submitPipeline(ctx workflow.Context, spec pipelineSpec) (pipelineHandle, error) {
	// gh-mode venue switch (B5): retarget the dispatch to the project's OWN repo +
	// aiarch-construct.yml when the per-project Repo resolves; a zero target leaves the
	// central-repo fallback (resolveTarget) intact for unresolvable projects.
	target, workflowFile, terr := wf.constructRepoTarget(spec.ProjectID)
	if terr != nil {
		return pipelineHandle{}, terr
	}
	handle, err := wf.Acts.PipelineSubmitAgenticJob(ctx, agenticjob.PipelineSpec{
		ProjectID:  agenticjob.ProjectID(spec.ProjectID),
		ActivityID: agenticjob.ConstructionActivityID(spec.ActivityID),
		Steps: []agenticjob.PipelineStep{{
			Name:      "build",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		WorkspaceRef:   agenticjob.ArtifactRef(spec.RepoURL + "@" + spec.Ref),
		DispatchInputs: dispatchInputsFor(spec),
		TargetRepo:     target,
		WorkflowFile:   workflowFile,
	})
	if err != nil {
		return pipelineHandle{}, err
	}
	return pipelineHandle{Name: agenticjob.PipelineHandleString(handle)}, nil
}

// observePipeline calls the GENERATED observe invoker and maps the contract observation
// back to the Manager-neutral pipelineObservation.
func (wf *workflows) observePipeline(ctx workflow.Context, handle pipelineHandle) (pipelineObservation, error) {
	obs, err := wf.Acts.PipelineObserveAgenticJob(ctx, agenticjob.ParsePipelineHandle(handle.Name))
	if err != nil {
		return pipelineObservation{}, err
	}
	return pipelineObservation{
		Phase:      managerPipelinePhase(obs.Phase),
		Diagnostic: obs.Diagnostic,
		RunURL:     obs.RunURL,
		Episode:    obs.Episode,
	}, nil
}

// ---------------------------------------------------------------------------
// Episode capture (SP1 capture-seam, Task 7)
// ---------------------------------------------------------------------------
//
// EVERY terminal observation of an AGENTIC dispatch this Manager takes becomes
// EXACTLY ONE EpisodeRecord in the episode ledger — either the mined summary or an
// explicit GAP record. A missing record is never silently missing; the ledger is the
// only place the platform can later answer "what did this activity actually cost".
//
// THREE disciplines hold here, and each has a reason:
//
//   - BUSINESS FIRST, EPISODE SECOND. The append is the LAST thing a terminal poll
//     does: the in-loop business handling (the CI-rollup mirror, the phase stamp) has
//     already run, and the append's own failure is swallowed. A ledger line claiming
//     something happened when it did not is worse than a missing line.
//   - THE APPEND NEVER FAILS THE BUSINESS FLOW. appendEpisodeActivityOptions gives it
//     its own retry envelope, wholly independent of the business retries; when it is
//     still failing at the end of that envelope the error is LOGGED and dropped.
//     Construction must not fail because a bookkeeping write did.
//   - VENUE. The GitHub-Actions arm mines no episode in v1, so a nil summary there is
//     EXPECTED, not a gap — writing a gap record per GH run would fill the ledger with
//     noise that means nothing. The only venue signal a workflow can see is the run URL
//     (see pipelineObservation.RunURL), so that is what gates it.
//
// DETERMINISM: the append is a plain ExecuteActivity — a NEW command in an EXISTING
// workflow body. In-flight executions must be DRAINED before deploying (the standing
// convention; no GetVersion guard is carried — contrast pumpnextactivity.go:44, which
// documents the pure-addition case that needs none).

// maxLateEpisodePolls bounds the EXTRA observe polls a CANCELLED run is given before
// its episode is written off as a gap. Cancel flips the RA's phase SYNCHRONOUSLY while
// the agent subprocess is still unwinding, so a cancelled run's FIRST terminal
// observation legitimately carries no summary — the production RA guarantees it appears
// on a later poll. Four extra polls at lateEpisodePollInterval is the whole grace
// window; past it the run is recorded as a gap rather than waited on forever.
const maxLateEpisodePolls = 4

// lateEpisodePollInterval spaces the late-episode grace polls. DELIBERATELY tighter than
// the business poll interval: the wait is pure bookkeeping, but the workflow is blocked on
// it, so a cancelled run would otherwise sit visibly "generating" for a further minute
// before landing at its failure gate. Five seconds comfortably clears the executor's own
// subprocess wait, and four of them cap the whole grace window at 20s.
const lateEpisodePollInterval = 5 * time.Second

// episodeVenueIsRemote reports whether this observation came from the REMOTE
// (GitHub-Actions) venue, which mines no episode summary in v1. The run URL is the only
// venue fact an observation carries: the Actions arm stamps it on every observation it
// resolved, and neither the local executor nor the dry-run stub ever sets one. A GH run
// whose URL the RA could not resolve therefore reads as local and earns a gap record —
// deliberately the safe direction (a visible, labelled gap beats a silent loss).
func episodeVenueIsRemote(runURL string) bool {
	return runURL != ""
}

// awaitLateEpisode gives a CANCELLED run's episode summary a bounded chance to arrive
// (see maxLateEpisodePolls). It returns the observation to RECORD: the later one that
// carried a summary when it arrived, else the caller's original — which becomes a gap.
// The business phase is taken from the ORIGINAL observation either way; this only ever
// upgrades the episode payload.
func (wf *workflows) awaitLateEpisode(ctx workflow.Context, handle pipelineHandle, obs pipelineObservation) pipelineObservation {
	if obs.Phase != PipelineCancelled || obs.Episode != nil {
		return obs
	}
	for range maxLateEpisodePolls {
		if err := workflow.Sleep(ctx, lateEpisodePollInterval); err != nil {
			return obs
		}
		next, err := wf.observePipeline(ctx, handle)
		if err != nil {
			return obs
		}
		if next.Episode != nil {
			obs.Episode = next.Episode
			return obs
		}
	}
	return obs
}

// captureEpisode appends the ONE ledger record this terminal observation owes.
// agentic=false marks a dispatch that spawns no agent at all (the local merge job): such
// a run has no episode to lose, so a nil summary is recorded as NOTHING rather than as a
// gap. A REMOTE-venue run is skipped entirely for the same "nothing to lose" reason.
//
// task/attempt are the Figure A-1 attribution key the caller already has in hand — the
// task this dispatch's episode is burned on, and how many times that task has been
// dispatched for this activity (see episodeRecordFor / constructState.nextTaskAttempt,
// Task 10).
func (wf *workflows) captureEpisode(ctx workflow.Context, in constructActivityInput, handle pipelineHandle, obs pipelineObservation, agentic bool, task projectstate.MethodTask, attempt int) {
	if episodeVenueIsRemote(obs.RunURL) {
		return
	}
	if obs.Episode == nil && !agentic {
		return
	}
	// The cancel-race grace is worth waiting for ONLY on a dispatch that could have mined
	// an episode at all: a non-agentic job has nothing in flight to wait for.
	if agentic {
		obs = wf.awaitLateEpisode(ctx, handle, obs)
	}
	rec := episodeRecordFor(ctx, obs, episodeIDSeed(handle, in), string(in.ActivityID), task, attempt)
	if err := wf.Acts.EpisodesAppendEpisode(ctx, episode.ProjectID(in.ProjectID), rec); err != nil {
		// Swallowed BY DESIGN — see the "never fails the business flow" discipline above.
		workflow.GetLogger(ctx).Error("episode append failed after its full retry envelope; this episode is NOT in the ledger",
			"activityId", string(in.ActivityID), "episodeId", rec.EpisodeID, "error", err.Error())
	}
}

// episodeIDSeed is the deterministic, replay-stable seed a GAP record's EpisodeID is
// built from — the dispatch handle (unique per dispatch, and already in workflow
// history) with the activity id as the fallback for a zero handle.
func episodeIDSeed(handle pipelineHandle, in constructActivityInput) string {
	if handle.Name != "" {
		return handle.Name
	}
	return string(in.ActivityID)
}

// episodeRecordFor composes the ledger record for ONE terminal observation. With a
// summary it copies every mined field VERBATIM and stamps only what the Manager alone
// knows (Kind/TargetRef/Lineage); with no summary it composes an explicit GAP record so
// the loss is visible. Pure apart from workflow.GetInfo/Now, both replay-deterministic.
//
// TargetRef carries the ATTEMPT key (projectstate.AttemptID: "<activityId>:<task>:<n>"),
// NOT the bare activity id (Task 10). Episode CAPTURE itself stays deferred (SP1), but
// the KEY cannot wait: the (task, attempt) pair that joins this episode to the Figure A-1
// unit that burned it exists only HERE, at write time — supplied by the caller from the
// lifecycle phase and redraft/attempt count it already has in hand — and cannot be
// reconstructed once it is gone. Lineage.ActivityID is left as the bare Method activity
// id (a separate concern: joining the episode to the project network), so only TargetRef
// changes shape.
func episodeRecordFor(ctx workflow.Context, obs pipelineObservation, idSeed, activityID string, task projectstate.MethodTask, attempt int) episode.EpisodeRecord {
	exec := workflow.GetInfo(ctx).WorkflowExecution
	lineage := &episode.EpisodeLineage{
		WorkflowID: exec.ID,
		RunID:      exec.RunID,
		// The METHOD activity this episode was burned on (there is no way to read the
		// Temporal activity id from inside a workflow, and the Method id is the one that
		// makes the lineage joinable to the project network).
		ActivityID: &activityID,
	}
	targetRef := projectstate.AttemptID(activityID, task, attempt)
	// Construction dispatches are always EpisodeKindConstruction: the phase profile
	// (requirements/detailed_design/test_plan/construction/integration) draws no
	// review-vs-rework distinction, so there is nothing here to map onto the other kinds.
	const kind = episode.EpisodeKindConstruction
	if obs.Episode == nil {
		return episodeGapRecord(kind, targetRef, lineage, "gap-"+episodeIDSafe(idSeed),
			episodeGapReason(episodeMissingSummaryReason, obs.Diagnostic), workflow.Now(ctx))
	}
	return episodeRecordFromSummary(*obs.Episode, kind, targetRef, lineage, obs.Diagnostic)
}

// nextTaskAttempt returns the next 1-based attempt number for t, the Figure A-1 task a
// dispatch's episode attributes to (see runPipeline / runMergePipeline). It is the SAME
// counter across an activity's outer variance retries AND a gated phase's human-paced
// redrafts — both re-enter runPipeline for the SAME phase — so it is the single source
// of the attempt number projectstate.AttemptID needs (Task 10). Lazily initialized so a
// constructState built without ever dispatching a pipeline (ProjectSupervisionWorkflow's)
// allocates nothing. workflow-local and rebuilt deterministically on replay; it starts
// from the row's attempt ledger (seedResumeFromLedger), and no live writer appends to
// that ledger yet.
func (s *constructState) nextTaskAttempt(t projectstate.MethodTask) int {
	if s.taskAttempts == nil {
		s.taskAttempts = map[projectstate.MethodTask]int{}
	}
	s.taskAttempts[t]++
	return s.taskAttempts[t]
}

// episodeMissingSummaryReason is the GapReason for the "the run terminated and reported
// no episode at all" case — the one the never-silent rule exists for.
const episodeMissingSummaryReason = "terminal observation carried no episode summary"

// episodeRecordFromSummary copies a mined EpisodeSummary onto an EpisodeRecord field for
// field — VERBATIM, no recomputation — and stamps the Manager-known Kind/TargetRef/
// Lineage the RA cannot know. WorkerClass is left unset: construction's per-activity
// snapshot (constructionActivity) carries the component/layer/phases, NOT the Phase-2
// activity list's workerClass, so there is no honest value to put here.
// diagnostic supplies the GapReason when the RA itself reported a GAP outcome (a
// restart-lost run recovered from its orphaned trace) — the observation's diagnostic IS
// the explanation on that path, since EpisodeSummary carries no reason field.
func episodeRecordFromSummary(s agenticjob.EpisodeSummary, kind episode.EpisodeKind, targetRef string, lineage *episode.EpisodeLineage, diagnostic string) episode.EpisodeRecord {
	rec := episode.EpisodeRecord{
		EpisodeID:      s.EpisodeID,
		Kind:           kind,
		TargetRef:      targetRef,
		Lineage:        lineage,
		Model:          s.Model,
		Usage:          episode.EpisodeUsage(s.Usage),
		CostUSD:        s.CostUSD,
		NumTurns:       s.NumTurns,
		ToolCallCounts: s.ToolCallCounts,
		SubagentSpans:  episodeSubagentSpans(s.SubagentSpans),
		StartedAt:      s.StartedAt,
		EndedAt:        s.EndedAt,
		Outcome:        episodeOutcomeFrom(s.Outcome),
		TracePath:      s.TracePath,
	}
	if s.StreamedUsage != nil {
		u := episode.EpisodeUsage(*s.StreamedUsage)
		rec.StreamedUsage = &u
	}
	if rec.Outcome == episode.EpisodeGap {
		reason := episodeGapReason("the run reported a gap episode", diagnostic)
		rec.GapReason = &reason
	}
	return rec
}

// episodeGapRecord composes the SYNTHESIZED gap record for a terminal observation that
// carried no summary at all. now is supplied by the caller (workflow.Now on the
// replay-deterministic workflow paths) because the run's own clock is exactly what was
// lost.
func episodeGapRecord(kind episode.EpisodeKind, targetRef string, lineage *episode.EpisodeLineage, episodeID, reason string, now time.Time) episode.EpisodeRecord {
	return episode.EpisodeRecord{
		EpisodeID: episodeID,
		Kind:      kind,
		TargetRef: targetRef,
		Lineage:   lineage,
		StartedAt: now,
		EndedAt:   now,
		Outcome:   episode.EpisodeGap,
		GapReason: &reason,
	}
}

// episodeGapReason joins the Manager's own reason to the observation's diagnostic when
// the RA supplied one, so a gap says both WHAT was lost and what the rail reported.
func episodeGapReason(reason, diagnostic string) string {
	if strings.TrimSpace(diagnostic) == "" {
		return reason
	}
	return reason + " — " + diagnostic
}

// episodeSubagentSpans re-types the mined subagent spans onto the ledger contract's own
// span type (identical shapes, distinct contracts — contracts are self-contained).
func episodeSubagentSpans(in []agenticjob.SubagentSpan) []episode.SubagentSpan {
	if len(in) == 0 {
		return nil
	}
	out := make([]episode.SubagentSpan, 0, len(in))
	for _, s := range in {
		out = append(out, episode.SubagentSpan(s))
	}
	return out
}

// episodeOutcomeFrom maps the observation contract's outcome onto the ledger contract's.
// Written as a TOTAL switch rather than a numeric cast so a future divergence between the
// two independently-versioned contracts is a compile-time conversation, not silent drift.
func episodeOutcomeFrom(o agenticjob.EpisodeOutcome) episode.EpisodeOutcome {
	switch o {
	case agenticjob.EpisodeSucceeded:
		return episode.EpisodeSucceeded
	case agenticjob.EpisodeFailed:
		return episode.EpisodeFailed
	case agenticjob.EpisodeCancelled:
		return episode.EpisodeCancelled
	case agenticjob.EpisodeGap:
		return episode.EpisodeGap
	default:
		return episode.EpisodeGap
	}
}

// episodeIDSafe rewrites s into the [A-Za-z0-9._-] alphabet episodeAccess requires of an
// EpisodeID. A rejected id is ContractMisuse — non-retryable — so a gap record seeded
// from a raw pipeline handle (which carries a ':') would be dropped on the floor, exactly
// defeating the never-silent rule the gap record exists to serve.
func episodeIDSafe(s string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, s)
	if safe == "" {
		return "unknown"
	}
	return safe
}

// gitforward.go is the WORKFLOW-LEVEL wiring of the git-forward (branch→PR→CI→+1→
// merge) lifecycle into the per-activity construction spine (C-MCN-GIT; D-PA-GIT §5).
// It is the ONLY place that composes the two seams the constructionManager alone
// touches: the PR rail (sourceControlAccess / IPullRequestRail) and the per-activity
// git head-state mirror (projectStateAccess §GIT-HEAD-STATE). The division of labor
// (D-PA-GIT §5):
//
//   - the rail OWNS the git provider interaction (cut branch, open PR, read CI,
//     relay +1, perform merge) and RETURNS opaque handles + a status reflection;
//   - this Manager receives the opaque returns and MIRRORS them onto the head-state
//     via the additive Record* verbs;
//   - projectStateAccess stores the opaque strings + typed CI enum — it never calls
//     the rail (RA-never-calls-RA).
//
// The merge AUTHORITY split is preserved: interventionEngine DECIDES when to merge
// (the existing variance machinery), the Manager PERFORMS it here. The +1 is the
// architect's in-app approval; the existing reviewEngine fan-out is the technical
// review and is unchanged — the git +1 relay is the SEPARATE, audit-worthy human
// architecture sign-off the head-state records.
//
// CRASH-SAFETY / IDEMPOTENCY: every rail call is on a deterministic name (idempotent
// in the rail) and every Record* goes through applyRecovering — the workflow-level
// Conflict re-read→re-apply loop (§6.5) — with the per-Activity idempotency key, so a
// workflow retry re-running any step is a no-op (the rail's deterministic-name
// idempotency + the git store's dedup-first ledger). The cred is minted ONCE per
// activity lifecycle and threaded into every rail + record verb.

// gitForward is the per-activity git-lifecycle state the spine carries across its
// steps. It is workflow-local (rebuilt deterministically on replay) and holds the
// opaque handles the rail returned + the credential the Manager minted. headVersion
// is shared with the non-git transition records (read-your-writes; §6.5) — the caller
// passes a pointer to the spine's headVersion so both record families advance one
// monotonic token.
type gitForward struct {
	enabled   bool
	repoRef   sourcecontrol.RepoRef
	cred      railCredEnvelope
	branch    string
	branchRef string
	prRef     string
	crLabel   string
	isRevert  bool
}

// gitEnabled reports whether the git-forward slice is wired AND a repo resolves for
// this project. When false the spine runs unchanged (the live Postgres-store
// composition that predates the GitStore).
func (wf *workflows) gitEnabled(projectID ProjectID) (sourcecontrol.RepoRef, bool) {
	if !wf.RailEnabled || wf.GitStatus == nil || wf.Repo == nil {
		return sourcecontrol.RepoRef(""), false
	}
	return wf.Repo(projectID)
}

// openActivityBranchAndPR runs the dispatch-time half of the lifecycle: mint the
// credential, OpenBranch + OpenPullRequest on the rail, then RecordActivityBranchOpened
// (the PR-tolerant fused upsert — births the row with branch+PR and CICheck=Pending).
// It returns the populated gitForward and advances *headVersion. A nil/dormant slice
// returns a disabled gitForward and touches nothing.
func (wf *workflows) openActivityBranchAndPR(
	ctx workflow.Context,
	in constructActivityInput,
	preMintedCred railCredEnvelope,
	headVersion *projectstate.Version,
) (gitForward, error) {
	repoRef, ok := wf.gitEnabled(in.ProjectID)
	if !ok {
		return gitForward{enabled: false}, nil
	}

	gf := gitForward{
		enabled:  true,
		repoRef:  repoRef,
		branch:   activityBranchName(in.ActivityID),
		crLabel:  in.Activity.CRLabel,
		isRevert: in.Activity.IsRevert,
	}

	// REUSE the credential minted ONCE at the top of the spine for the started
	// record (Task 3) — one mint per activity git lifecycle, threaded into every
	// rail + record verb. (Empty when no started cred was minted, which only happens
	// if the slice is dormant — and then gitEnabled is false above and we never get
	// here.)
	gf.cred = preMintedCred
	cred := preMintedCred

	// Rail: cut the per-activity branch (GENERATED invoker).
	br, err := wf.Acts.RailOpenBranch(ctx, repoRef, sourcecontrol.BranchName(gf.branch), cred.toRail())
	if err != nil {
		return gitForward{}, err
	}
	gf.branchRef = sourcecontrol.BranchRefString(br)

	// Rail: open the PR (base = main; cr-NN label rides in Hints) (GENERATED invoker).
	pr, err := wf.Acts.RailOpenPullRequest(ctx, repoRef, sourcecontrol.PullRequestSpec{
		Head:  sourcecontrol.BranchName(gf.branch),
		Base:  sourcecontrol.BranchName(mainBranch),
		Title: prTitle(in.ActivityID),
		Body:  prBody(in.Activity),
		Hints: crLabelHints(gf.crLabel),
	}, cred.toRail())
	if err != nil {
		return gitForward{}, err
	}
	gf.prRef = sourcecontrol.PullRequestRefString(pr)

	// Mirror: birth the per-activity git head-state row (PR-tolerant fused upsert).
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityBranchOpened(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID),
			gf.branch, gf.branchRef, gf.prRef, gf.crLabel, gf.isRevert, cred.toProjectState())
	})
	if err != nil {
		return gitForward{}, err
	}
	*headVersion = v
	return gf, nil
}

// observeCIAndRecord reads the PR's CI rollup once and mirrors it onto the head-state
// (the poll-loop verb — D-PA-GIT §5). Called between the spine's durable waits while
// the pipeline runs. Returns the observed reflection so the caller can feed it into the
// variance machinery. A dormant slice is a no-op returning Pending.
func (wf *workflows) observeCIAndRecord(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
) (pullRequestStatusView, error) {
	if !gf.enabled {
		return pullRequestStatusView{CheckRollup: projectstate.CICheckPending}, nil
	}

	prStatus, err := wf.Acts.RailGetPullRequestStatus(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef), gf.cred.toRail())
	if err != nil {
		return pullRequestStatusView{}, err
	}
	st := pullRequestStatusView{
		CheckRollup:   mapCheckState(prStatus.CheckRollup),
		ApprovalCount: int(prStatus.ApprovalCount),
		Mergeable:     prStatus.Mergeable,
	}

	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityCIObserved(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID),
			st.CheckRollup, gf.cred.toProjectState())
	})
	if err != nil {
		return pullRequestStatusView{}, err
	}
	*headVersion = v
	return st, nil
}

// relayArchApprovalAndRecord relays the architecture +1 (PostReview Approve) to the PR
// and records the audit-worthy ArchApproved fact (D-PA-GIT §5). Called once the
// activity's review has passed (the architect's in-app sign-off). A dormant slice is a
// no-op.
func (wf *workflows) relayArchApprovalAndRecord(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
) error {
	if !gf.enabled {
		return nil
	}

	if err := wf.Acts.RailPostReview(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef),
		sourcecontrol.ReviewSubmission{Verdict: sourcecontrol.ReviewApprove, Body: archApprovalBody(in.ActivityID)},
		gf.cred.toRail()); err != nil {
		return err
	}

	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityArchApproved(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID), gf.cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// mergeAndRecord PERFORMS the gated merge (the interventionEngine gate already
// cleared in workflow code) and, on a Merged result, records the terminal git fact
// (D-PA-GIT §5). A dormant slice is a no-op. A non-Merged result (e.g. not yet
// mergeable) is surfaced as a non-retryable terminal so the spine does NOT record a
// false merge — the activity's variance machinery handles the not-yet-mergeable case.
func (wf *workflows) mergeAndRecord(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
) error {
	if !gf.enabled {
		return nil
	}

	mr, err := wf.Acts.RailMergePullRequest(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef), gf.cred.toRail())
	if err != nil {
		return err
	}
	if !mr.Merged {
		return temporal.NewNonRetryableApplicationError(
			"gated merge did not complete (PR not mergeable)", "MergeNotCompleted", nil)
	}

	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityMerged(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID), gf.cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// recordActivityStarted marks the activity Running in the per-activity construction
// head-state at the TOP of the spine (Task 3), BEFORE any dispatch. This is what
// flips the activity out of NotStarted so the pump's eligibility selection
// (nextEligibleActivity over proj.ActivityExecution) does not re-dispatch it on a
// concurrent/redundant tick. Cred-threaded like the four git head-state records; a
// dormant slice (git unwired) is a no-op (the live Postgres composition has no
// per-activity construction head-state, so the gate degrades to the child-workflow-id
// idempotency the pump already relies on). It mints a credential ONCE for the
// started+completed pair via the supplied gitForward.cred when the branch lifecycle
// has already minted one, else mints its own.
//
// It also stamps the activity's classified (Type, Variant) onto the head-state row —
// the RA seeds its Phases slice from that pair, so an unstamped row seeded the 5-phase
// SERVICE set for every activity and made earned value disagree with the profile the
// workflow walks.
func (wf *workflows) recordActivityStarted(
	ctx workflow.Context,
	in constructActivityInput,
	cred railCredEnvelope,
	headVersion *projectstate.Version,
) error {
	if wf.GitStatus == nil {
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityStarted(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID),
			in.Activity.Type, in.Activity.Variant, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// recordActivityCompleted marks the activity Done in the per-activity construction
// head-state at the END of the spine (Task 3), alongside RecordActivityExited. This
// is what unblocks dependents in the pump's eligibility selection (projectstate.AllDepsSatisfied). A
// dormant slice is a no-op.
func (wf *workflows) recordActivityCompleted(
	ctx workflow.Context,
	in constructActivityInput,
	cred railCredEnvelope,
	headVersion *projectstate.Version,
) error {
	if wf.GitStatus == nil {
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityCompleted(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID), cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// startedCred resolves the credential the construction started/completed records
// thread, and reports whether those records fire at all. It deliberately gates on the
// CONSTRUCTION-STATUS slice (GitStatus), NOT the full PR-rail slice — the per-activity
// Running/Done head-state is what drives the pump's eligibility cascade and is
// independent of the branch→PR→merge lifecycle:
//
//   - PR-rail wired (gitEnabled — Rail+GitStatus+Repo, the CLOUD GitHub profile): mint
//     the short-lived installation token via the rail; it is reused by the branch/PR
//     lifecycle AND the started/completed records.
//   - GitStatus wired but no PR rail (the LOCAL/dry-run profile — file:// repo, no
//     GitHub): the status records still fire so the cascade advances, threading a ZERO
//     credential. The local git store's gitAuth ignores the credential entirely
//     (GitAuth{Local:true}), so no token is needed; the PR-rail lifecycle stays dormant
//     (gitEnabled is false, so gf.enabled is false on every branch/PR/CI/merge step).
//   - GitStatus unwired (the legacy Postgres-store composition): false — the
//     started/completed records are no-ops and the pump degrades to child-workflow-id
//     idempotency.
//
// Minted/resolved ONCE at the top of the spine and reused for the completed record.
func (wf *workflows) startedCred(ctx workflow.Context, projectID ProjectID) (railCredEnvelope, bool, error) {
	if wf.GitStatus == nil {
		return railCredEnvelope{}, false, nil
	}
	// CLOUD profile: a PR rail + repo resolve ⇒ mint the real installation token.
	if repoRef, ok := wf.gitEnabled(projectID); ok {
		cred, err := wf.mintCred(ctx, repoRef)
		if err != nil {
			return railCredEnvelope{}, false, err
		}
		return cred, true, nil
	}
	// LOCAL/dry-run profile: status records fire with a zero (ignored) credential.
	return railCredEnvelope{}, true, nil
}

// mintCred runs the GENERATED getInstallationToken invoker → the short-lived credential
// the Manager threads into every rail + record verb for this activity's lifecycle.
func (wf *workflows) mintCred(ctx workflow.Context, repoRef sourcecontrol.RepoRef) (railCredEnvelope, error) {
	cred, err := wf.Acts.RailGetInstallationToken(ctx, repoRef)
	if err != nil {
		return railCredEnvelope{}, err
	}
	return railCredEnvelope{Bytes: cred.Bytes, ExpiresAt: cred.ExpiresAt}, nil
}

// gitnaming.go holds the Manager's provider-NEUTRAL, DETERMINISTIC naming + the git
// Activity option presets for the git-forward slice (C-MCN-GIT). The names are
// Manager-derived (the branch/PR/label vocabulary the rail maps to a git ref INSIDE
// the seam); determinism is load-bearing for the rail's deterministic-name idempotency
// (a workflow retry re-opening the same branch/PR is a no-op in the rail).

// mainBranch is the flat git-forward base every per-activity PR targets
// (op-concepts §15 — branch per activity, no long-lived integration branch).
const mainBranch = "main"

// activityBranchName derives the provider-neutral per-activity branch name
// "activity/<activityID>" (D-PA-GIT GIT.1 example). Deterministic in the activity id.
func activityBranchName(activityID ActivityID) string {
	return "activity/" + string(activityID)
}

// prTitle / prBody are the human-facing PR text the Manager's sequence owns.
func prTitle(activityID ActivityID) string {
	return fmt.Sprintf("aiarch: construction activity %s", activityID)
}

func prBody(activity constructionActivity) string {
	return fmt.Sprintf("Automated construction of component %s (%s, layer %s).",
		activity.ComponentID, activityKindName(activity.Kind), activity.Layer)
}

// activityKindName returns the canonical activity-kind name — a free function over
// the Manager-owned activityKind enum (the schema-first rule keeps enum types
// method-free, so the Stringer behaviour lives here). Produces the IDENTICAL strings
// the former handoff.ActivityKind Stringer did (PR body text — zero behavior change).
func activityKindName(k activityKind) string {
	switch k {
	case activityKindUnknown:
		// zero-value sentinel, not a real activity kind.
		return "Unknown"
	case activityKindDetailedDesign:
		return "DetailedDesign"
	case activityKindConstruction:
		return "Construction"
	case activityKindIntegration:
		return "Integration"
	case activityKindNoncoding:
		return "Noncoding"
	}
	// Unreachable for the five defined activityKind values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a defensive
	// fallback for an out-of-range ordinal.
	return "Unknown"
}

// archApprovalBody is the +1 relay's review body — the architect's in-app
// architecture sign-off relayed onto the PR.
func archApprovalBody(activityID ActivityID) string {
	return fmt.Sprintf("architecture +1 relayed for %s", activityID)
}

// crLabelHints encodes the cr-NN change-request group label into the rail's opaque
// PullRequestSpec.Hints (labels ride in Hints, not a first-class field —
// sourcecontrol.go §3). Empty label ⇒ nil hints.
func crLabelHints(crLabel string) []byte {
	if crLabel == "" {
		return nil
	}
	return []byte(crLabel)
}

// pullRequestStatusView is the Manager-local Activity-boundary projection of the
// rail's PullRequestStatus (a reflection the Manager feeds interventionEngine — NOT a
// gate). CheckRollup is the provider-neutral CI rollup the git head-state mirrors.
type pullRequestStatusView struct {
	CheckRollup   projectstate.CICheckState
	ApprovalCount int
	Mergeable     bool
}

// mapCheckState maps the rail's CheckState onto the git head-state's provider-neutral
// CICheckState (the two enums are aligned-by-identity, mapped here so a future re-order
// is safe). A DUMB reflection — it never gates any Approve control.
func mapCheckState(s sourcecontrol.CheckState) projectstate.CICheckState {
	switch s {
	case sourcecontrol.CheckPending:
		// explicit: pending check state maps directly, same as any unmapped value.
		return projectstate.CICheckPending
	case sourcecontrol.CheckSuccess:
		return projectstate.CICheckSuccess
	case sourcecontrol.CheckFailure:
		return projectstate.CICheckFailure
	default:
		return projectstate.CICheckPending
	}
}

// adapters.go holds the bridges between the Manager's OWN broader domain vocabulary
// (constructionActivity, this component's generated façade ReviewSet/Reviewer) and
// each dependency's PUBLISHED contract shape, for the calls that are NOT identity —
// either because the Manager's own type carries strictly more fields than the Engine
// needs, or because the target is this component's
// OWN generated public façade type with a real field-shape divergence
// (reviewSetFromEngine), or because the Manager derives a real config value from raw
// composition-root config (constructionInterventionPolicy).
//
// The two Engines (intervention.InterventionEngine / review.ReviewEngine) have NO
// adapter STRUCT — the workflow calls their published contracts DIRECTLY (workflow.go /
// signals.go), with fweng.Context{Context: context.Background()} supplied inline at each
// call site.

// ===========================================================================
// reviewEngine — reviewSetFromEngine bridges the published review.ReviewSet/Reviewer
// onto THIS component's OWN generated façade ReviewSet/Reviewer (contract.gen.go,
// off-limits — DO NOT EDIT). A REAL divergence, not an identity mirror: the façade's
// Reviewer.ReferenceArtifact is *string (optional, omitempty) while the Engine's own
// Reviewer.ReferenceArtifact is a plain string (empty ⇒ none) — the nil/empty-string
// boundary is exactly the kind of zero-value divergence that must be bridged
// explicitly, not cast.
// ===========================================================================

func reviewSetFromEngine(set review.ReviewSet) ReviewSet {
	reviewers := make([]Reviewer, 0, len(set.Reviewers))
	for _, r := range set.Reviewers {
		cr := Reviewer{
			Role:        r.Role,
			Perspective: r.Perspective,
			MayAmend:    r.MayAmend,
		}
		if r.ReferenceArtifact != "" {
			ref := r.ReferenceArtifact
			cr.ReferenceArtifact = &ref
		}
		reviewers = append(reviewers, cr)
	}
	// The gate verdict rides onto the façade beside the roster: the two are one answer
	// from one call. Both are *T on the façade (additive optional properties), so a
	// pre-stage-2 client that never reads them decodes unchanged.
	human, reason := set.RequiresHuman, set.Reason
	return ReviewSet{Reviewers: reviewers, RequiresHuman: &human, Reason: &reason}
}

// engineReviewPolicy converts the COMMITTED policy document (projectstate's storage
// shape) into the reviewEngine's own copy. An adapter rather than a cast for two real
// divergences: Preset is *string here and a plain string there (nil ⇒ "", the
// legacy/explicit mode), and GatedPhasesByType is keyed to the typed
// ActivityMethodPhase here and to that phase's WIRE NAME there — an Engine may not
// import projectstate (F3), so the engine keys on the strings the two rails already
// share.
//
// The two design-rail Managers carry a byte-identical copy of this function
// (systemdesign/coauthorartifact.go, projectdesign/coauthorphase2artifact.go): three
// packages with no shared home that would not cost an internal/arch_test.go allowlist
// entry for a five-line conversion. Edit all three together; their parity is pinned by
// Test_EngineReviewPolicy_CarriesTheStoredDocument in each package.
func engineReviewPolicy(p projectstate.ReviewPolicy) review.ReviewPolicy {
	out := review.ReviewPolicy{}
	if p.Preset != nil {
		out.Preset = *p.Preset
	}
	if len(p.GatedPhasesByType) > 0 {
		out.GatedPhasesByType = make(map[string][]string, len(p.GatedPhasesByType))
		for typ, phases := range p.GatedPhasesByType {
			names := make([]string, 0, len(phases))
			for _, ph := range phases {
				names = append(names, ph.String())
			}
			out.GatedPhasesByType[typ] = names
		}
	}
	return out
}

// maxVarianceAttempts bounds the dispatch→review→variance supervision loop
// before the Engine's Escalate/Takeover must terminate it.
const maxVarianceAttempts = 10

// maxPhaseRedrafts bounds a gated phase's human-paced SendBack redraft budget —
// SEPARATE from maxVarianceAttempts. SendBack is NOT a variance: it redrafts THIS
// phase in place; on exhaustion the gate keeps awaiting the human (it never
// re-enters the variance loop or fails the activity).
const maxPhaseRedrafts = 5

// pipelinePollInterval is the durable wait between observeAgenticJob
// polls (the Manager's own startTimer cadence; §6.3 step 3).
const pipelinePollInterval = 15 * time.Second

// maxPipelinePolls bounds the observe loop (a stuck pipeline escalates).
const maxPipelinePolls = 240

// ===========================================================================
// ConstructActivityWorkflow — the per-activity UC3 spine (constructionManager.md
// §6.3). Loop/supervise until exited.
// ===========================================================================

// constructActivityInput is the start payload for the per-activity child workflow.
type constructActivityInput struct {
	ProjectID  ProjectID
	ActivityID ActivityID
	Activity   constructionActivity
}

func (wf *workflows) ConstructActivityWorkflow(ctx workflow.Context, in constructActivityInput) error {
	state := &constructState{
		projectID:       in.ProjectID,
		activityID:      in.ActivityID,
		stage:           StageDispatching,
		completedPhases: map[projectstate.ActivityMethodPhase]bool{},
	}
	if err := workflow.SetQueryHandler(ctx, querySessionState, state.view); err != nil {
		return err
	}

	// Operator-override signal channel (constructionManager.md §6.3 override branch).
	overrideCh := workflow.GetSignalChannel(ctx, signalOperatorOverride)

	// Per-execution start snapshot (B5): capture the committed ReviewPolicy, seed the
	// completedPhases skip-guard, and capture the contract keys — replay-guarded.
	reviewPolicy, err := wf.loadReviewSnapshot(ctx, in, state)
	if err != nil {
		return err
	}

	// Carry expectedVersion forward (read-your-writes; §6.5).
	headVersion := wf.readVersion(ctx, in.ProjectID)

	// --- Step 0: record the activity STARTED (Task 3) ----------------------------
	// Mint the per-activity credential ONCE (reused by the branch/PR lifecycle below
	// and the completed record at the end) and flip the activity to Running in the
	// per-activity construction head-state BEFORE any dispatch. This is what removes
	// the activity from the pump's NotStarted eligibility set so a concurrent/redundant
	// pump tick does not re-dispatch it. Dormant (no-op) when the git slice is unwired.
	startedCred, gitOn, scErr := wf.startedCred(ctx, in.ProjectID)
	if scErr != nil {
		return scErr
	}
	switch {
	case state.executionLedger:
		// OpenActivity is the fold RecordActivityStarted became: the same StartedAt and the
		// same classified (type, variant), plus the LIFECYCLE PIN the started record had no
		// room for. It is deliberately NOT gated on gitOn — the execution ledger is the
		// durable record of the run itself, not a mirror of a git head-state, and it is
		// bound in every composition the composition root builds.
		if err := wf.openActivity(ctx, in, state, startedCred, &headVersion); err != nil {
			return err
		}
	case gitOn:
		if err := wf.recordActivityStarted(ctx, in, startedCred, &headVersion); err != nil {
			return err
		}
	}

	// git-forward lifecycle state (C-MCN-GIT). Opened lazily on the first non-
	// architectOnly dispatch and carried across supervision-loop iterations (a branch
	// + PR is born once per activity, not per retry). Dormant when the slice is unwired.
	var gf gitForward

	// Supervision loop: each attempt runs the UC3 spine once (constructionManager.md
	// §6.3). runAttempt reports back whether the activity terminally exited (attemptDone,
	// the workflow returns) or the loop should try again (attemptRetry).
	for attempt := 0; ; attempt++ {
		ctrl, err := wf.runAttempt(ctx, in, attempt, reviewPolicy, state, &gf, &headVersion, overrideCh, gitOn, startedCred)
		if err != nil {
			return err
		}
		if ctrl == attemptDone {
			return nil
		}
	}
}

// ---------------------------------------------------------------------------
// ConstructActivityWorkflow attempt helpers (mechanical decomposition of the UC3
// spine; NO change to the ORDER of workflow commands). Each helper runs its
// activities/timers/signals in the same sequence the inline loop did.
// ---------------------------------------------------------------------------

// attemptControl is the loop-control verb runAttempt hands back to the supervision loop.
type attemptControl int

const (
	// attemptRetry: re-enter the supervision loop for another attempt.
	attemptRetry attemptControl = iota
	// attemptDone: the activity reached a terminal exit; return from the workflow.
	attemptDone
)

// runAttempt executes ONE supervision attempt of the per-activity UC3 spine: guard the
// variance budget, open the branch/PR, walk the phase profile (each phase dispatched as
// an agent job, with the review-policy gate inserting a human where required), and (on a
// clean pass) finalize the activity. It returns attemptDone
// when the activity has terminally exited or attemptRetry when the supervision loop should
// try again. The ORDER of workflow commands is identical to the former inline loop body.
func (wf *workflows) runAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	attempt int,
	reviewPolicy projectstate.ReviewPolicy,
	state *constructState,
	gf *gitForward,
	headVersion *projectstate.Version,
	overrideCh workflow.ReceiveChannel,
	gitOn bool,
	startedCred railCredEnvelope,
) (attemptControl, error) {
	if attempt >= maxVarianceAttempts {
		// Terminal: the supervision loop exhausted its variance/retry budget. Record the
		// FAILURE in head-state (so the activity is no longer stuck Running) before exit.
		return attemptDone, wf.failVarianceExhausted(ctx, in, headVersion, state, startedCred)
	}
	state.attempt = attempt + 1

	// --- Step 1: dispatch (the former per-activity worker-class cast is retired). The
	// handOffEngine is gone: agent-class selection collapsed to the platform's single
	// "agent" dispatch default, and automated-vs-human routing is now the project's
	// review-policy preset, applied per phase by the runPhaseGate gate below (via the
	// reviewEngine's ProposeReviews/RequiresHuman verdict) rather than an up-front
	// worker-class decision. Every activity dispatches; a human is inserted where
	// the review policy requires one.

	// --- Step 2a: open the per-activity branch + PR and mirror it (git-forward,
	// C-MCN-GIT). Lazy + once: the row is born on the first dispatch and reused on
	// retries. Dormant (no-op) when the git slice is unwired. ----------------------
	if !gf.enabled {
		opened, oerr := wf.openActivityBranchAndPR(ctx, in, startedCred, headVersion)
		if oerr != nil {
			return attemptDone, oerr
		}
		*gf = opened
	}

	// --- Steps 2-5: walk the activity's profile phases, dispatching ONE GH-Actions
	// job per phase (the phase sequence is determined by the activity's resolved
	// profile — e.g. service: Requirements → Detailed Design → Test Plan →
	// Construction → Integration; testing-plan: Requirements → Test Plan →
	// Construction). A phase whose pipeline fails routes to intervention (App-A: a
	// failing review repeats the preceding task), then the activity retries from the
	// first phase. --------------------------------------------------------------
	//
	// A payload with no Phases is a workflow that started before the pump resolved
	// them; it derives from the CARRIED pair, whose zero value (Service, Plan) decodes
	// to exactly the canonical five this fallback used to hardcode — so legacy payloads
	// behave identically while a stamped pair now gets its real profile.
	if len(in.Activity.Phases) == 0 {
		in.Activity.Phases = projectstate.ProfileFor(in.Activity.Type, in.Activity.Variant).PhaseIDs()
	}
	phaseFailed, done, err := wf.walkPhases(ctx, in, attempt, reviewPolicy, state, gf, headVersion, overrideCh, gitOn, startedCred)
	if err != nil {
		return attemptDone, err
	}
	if done {
		return attemptDone, nil
	}
	if phaseFailed {
		// retry the activity; the completedPhases skip-guard resumes from the first
		// incomplete phase.
		return attemptRetry, nil
	}

	// --- Step 5b (local-merge-and-policy Commit 1): the policy-gated LOCAL merge.
	// In the rail-dormant local profile nothing else lands activity/<id> on main —
	// consult the SAME reviewEngine call (ProposeReviews/RequiresHuman) the
	// construction dispatch uses (vibes → auto, checkpoints/full → hold for
	// approval, risk floor → always hold) and dispatch
	// the merge job through the pipeline seam. A merge failure (conflict) routes
	// through the SAME intervention path a failed phase pipeline takes. -----------
	mergeFailed, mergeDone, mErr := wf.runLocalMergeStep(ctx, in, attempt, reviewPolicy, state, headVersion, overrideCh, gitOn, startedCred)
	if mErr != nil {
		return attemptDone, mErr
	}
	if mergeDone {
		return attemptDone, nil
	}
	if mergeFailed {
		// retry the activity; completedPhases skips the phases, so the retry
		// re-attempts only the merge.
		return attemptRetry, nil
	}

	// --- Steps 5a-8a: finalize (arch +1 relay, change reviewed, gated merge, binary
	// exit, per-activity COMPLETED). ---------------------------------------------
	if err := wf.finalizeActivity(ctx, in, gf, headVersion, state, gitOn, startedCred); err != nil {
		return attemptDone, err
	}
	return attemptDone, nil
}

// loadReviewSnapshot performs the per-execution start snapshot (B5): it reads the project
// ONCE (an Activity, recorded in history → replay-safe) and captures the committed
// ReviewPolicy BY VALUE (the gate's ONLY policy source; NEVER re-read mid-loop), seeds the
// LIVE completedPhases skip-guard (B2 resumability) from the activity's PhaseCompletion
// slice, and captures the contract keys for the gate's reviewer set.
//
// Temporal versioning guard (replay safety): this readProject call was ADDED by the
// construction-review-policy-snapshot feature AFTER the workflow was first shipped.
// Workflows already in flight at deploy time have no history event for this call; replaying
// them against new code would produce a non-determinism error. GetVersion guards the new
// block so pre-feature in-flight executions (DefaultVersion) skip it entirely — reviewPolicy
// stays zero (empty → inert → no gate) and completedPhases stays initialized-empty. The gate
// takes effect only for workflows started after the feature deployed (v >= 1).
func (wf *workflows) loadReviewSnapshot(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
) (projectstate.ReviewPolicy, error) {
	var reviewPolicy projectstate.ReviewPolicy
	v := workflow.GetVersion(ctx, "construction-review-policy-snapshot", workflow.DefaultVersion, 1)
	if v < 1 {
		return reviewPolicy, nil
	}
	snap, srErr := wf.readProject(ctx, in.ProjectID)
	if srErr != nil && !isReadNotFound(srErr) {
		return reviewPolicy, srErr
	}
	reviewPolicy = snap.ReviewPolicy
	// LEDGER-AWARE SEED (architect (D), D.1.3). The pump now dispatches an
	// integration-pending row — one whose history lives in the attempt ledger alone — so
	// the seed must read the row as the view does, or the run would redo phases the
	// ledger records as passed. GetVersion (always called, same change id as the pump's
	// selection) pins an execution that seeded from the stored Phases only to that seed.
	//
	// The GetVersion call stays unconditional (a recorded history must see the same
	// marker it recorded), but its DEFAULT arm is now empty: that arm read the row's
	// stored phase-completion slice, which stage-3 task 4 stopped storing — a lifecycle
	// phase is complete iff its gate task's latest attempt passed, and the ledger is the
	// only record of that. No pre-marker history it replays carries stored completions
	// anyway (the two ledger-seed fixtures hold attempts and no phase set), so the arm
	// seeds exactly what it seeded before: nothing.
	ledgerSeed := workflow.GetVersion(ctx, changeLedgerPartialResume, workflow.DefaultVersion, 1) >= 1
	if acs, ok := snap.ActivityExecution[string(in.ActivityID)]; ok && ledgerSeed {
		seedResumeFromLedger(state, in.Activity, acs)
	}
	// OPERATOR-NOTE DELIVERY (plan B1.4). GetVersion is always called here, so a new
	// execution records the marker before its first dispatch and an execution that
	// recorded none stays wholly old: no note recorded, carried or stamped, and no
	// scaffold sync. Notes still pending on the row (a re-queue's note, or one an
	// earlier run recorded but never dispatched) ride this run's first agent dispatch.
	state.noteDelivery = workflow.GetVersion(ctx, changeOperatorNoteDelivery, workflow.DefaultVersion, 1) >= 1
	if acs, ok := snap.ActivityExecution[string(in.ActivityID)]; ok && state.noteDelivery {
		state.pendingNotes = projectstate.PendingOperatorNotes(acs)
	}
	// EXECUTION LEDGER (stage 3). GetVersion is always called here, so a new execution
	// records the marker before its first dispatch and an execution that recorded none
	// stays wholly old: no activity opened, no attempt recorded, no round opened, no
	// verdict appended. Its history has no events for those Activities and never will.
	state.executionLedger = workflow.GetVersion(ctx, changeExecutionLedger, workflow.DefaultVersion, 1) >= 1
	// A send-back's feedback is the ROUND's now, not a stored note, so a run that ended
	// between the rejection and the redraft's dispatch must recover it from there. Without
	// this the next run re-walks the rejected phase and dispatches the redraft with NO
	// steer at all — strictly worse than the NoteSendBack this replaced, which survived a
	// run boundary because it was stored. Reads only; emits no command.
	if acs, ok := snap.ActivityExecution[string(in.ActivityID)]; ok && state.executionLedger {
		seedSendBackCarry(ctx, in, state, acs)
		// And the per-activity CAS token, off the SAME read — which is the whole reason
		// arming the guard needed no new Temporal command: the child already holds the row.
		// An activity with no row yet leaves it 0 (NoActivityVersionExpectation), the
		// posture of the writer that is about to birth it.
		state.activityVersion = acs.Version
	}
	state.reviewContracts = snapshotContractKeys(snap)
	// Task 7 non-overridable floor: snapshot ONCE whether the activity's committed
	// contract touches deploy/spend/schema — never re-evaluated mid-loop, mirroring
	// reviewPolicy itself. A missing contract (nil map lookup) reads as the zero
	// ServiceContract, which never touches the floor.
	state.floorTouched = projectstate.ContractTouchesReviewFloor(snap.ServiceContracts[in.Activity.ComponentID])
	return reviewPolicy, nil
}

// seedResumeFromLedger seeds a run's start state from its activity's stored row, read the
// way every other reader reads it (architect (D), D.1.3):
//   - completedPhases from projectstate.ResolvePhaseCompletions over the activity's profile:
//     the attempt ledger decides every phase it has decided (a passed gate completes the
//     phase, a rejected one leaves it incomplete), and a phase whose gate has no attempt
//     is a phase nothing is claimed about.
//   - taskAttempts from the highest attempt number the ledger records per task, so the
//     next dispatch of a task is attempt n+1 and its AttemptID (the episode TargetRef)
//     never collides with one the ledger already holds. A GATE task is counted off BOTH
//     ledgers (stage 3): its number is shared by its attempt and its review round, so a
//     resume that looked only at the attempts could mint a round id the review ledger
//     already holds — and OpenReviewRound is idempotent on that id, so the second gate
//     occurrence would silently vanish into the first.
//
// Pure over values already in workflow history (the snapshot's recorded readProject).
func seedResumeFromLedger(state *constructState, act constructionActivity, acs projectstate.ActivityExecution) {
	profile := projectstate.ProfileFor(act.Type, act.Variant)
	for _, pc := range projectstate.ResolvePhaseCompletions(profile, acs.Attempts) {
		if pc.Completed {
			state.completedPhases[pc.Phase] = true
		}
	}
	for _, a := range acs.Attempts {
		seedTaskCount(state, a.Task, a.Attempt)
	}
	for _, r := range acs.Reviews {
		seedTaskCount(state, r.TaskID, int(r.Round))
	}
}

// seedTaskCount raises the run's per-task counter to n when the ledger has gone further.
func seedTaskCount(state *constructState, task projectstate.MethodTask, n int) {
	if state.taskAttempts == nil {
		state.taskAttempts = map[projectstate.MethodTask]int{}
	}
	if n > state.taskAttempts[task] {
		state.taskAttempts[task] = n
	}
}

// failVarianceExhausted records the terminal FAILURE in head-state when the supervision
// loop exhausts its variance/retry budget (so the activity is no longer stuck Running).
func (wf *workflows) failVarianceExhausted(
	ctx workflow.Context,
	in constructActivityInput,
	headVersion *projectstate.Version,
	state *constructState,
	startedCred railCredEnvelope,
) error {
	const detail = "construction supervision exceeded max attempts"
	if state.executionLedger {
		if e := wf.recordExecutionOutcome(ctx, in, state, headVersion, startedCred,
			projectstate.ActivityOutcomeUnknown, projectstate.VarianceExhausted, detail); e != nil {
			return e
		}
		state.stage = StageExited
		workflow.GetLogger(ctx).Info("construction activity failed — variance budget exhausted", "activityId", in.ActivityID)
		return nil
	}
	v, e := wf.recordActivityFailed(ctx, in, *headVersion, projectstate.VarianceExhausted, detail, startedCred)
	if e != nil {
		return e
	}
	*headVersion = v
	state.stage = StageExited
	workflow.GetLogger(ctx).Info("construction activity failed — variance budget exhausted", "activityId", in.ActivityID)
	return nil
}

// walkPhases dispatches ONE GH-Actions job per profile phase, riding the CI poll cadence
// (observeCIAndRecord). The LIVE completedPhases skip-guard (B2) keeps the outer variance-
// retry (which re-walks from index 0) from re-dispatching or re-gating an already-completed
// phase. It returns (phaseFailed, done, err): done=true means a phase gate terminally
// recorded the activity (the workflow returns); phaseFailed=true means the caller should
// retry the activity.
func (wf *workflows) walkPhases(
	ctx workflow.Context,
	in constructActivityInput,
	attempt int,
	reviewPolicy projectstate.ReviewPolicy,
	state *constructState,
	gf *gitForward,
	headVersion *projectstate.Version,
	overrideCh workflow.ReceiveChannel,
	gitOn bool,
	startedCred railCredEnvelope,
) (bool, bool, error) {
	for _, phase := range in.Activity.Phases {
		if state.completedPhases[phase] {
			continue
		}
		state.stage = StagePipelineRunning
		obs, perr := wf.runPipeline(ctx, in, phase, state, gf, headVersion)
		if perr != nil {
			return false, false, perr
		}
		if obs.Phase == PipelineFailed || obs.Phase == PipelineCancelled {
			failReason := deriveFailureReason(obs.Phase, obs.Diagnostic)
			// intervention.WorkerMiss — the historical variancePipelineFailed → WorkerMiss
			// fold (the retired interventionVarianceKind many-to-one map, deps.go), the
			// only local variance kind this call site ever exercised.
			done, vErr := wf.handleVariance(ctx, in, intervention.WorkerMiss, obs.Diagnostic, failReason, attempt, headVersion, state, overrideCh, gitOn, startedCred)
			if vErr != nil {
				return false, false, vErr
			}
			if done {
				return false, true, nil
			}
			return true, false, nil
		}
		// Conditional per-phase approval gate (Task 6): records the phase start and — iff
		// the policy requires a human for this (activityType, phase) — suspends on the
		// phase-multiplexed decision signal. Approve/no-gate mark completion; a terminal
		// gate exit (done) has already recorded the activity.
		if done, gErr := wf.runPhaseGate(ctx, in, phase, reviewPolicy, state, gf, headVersion, gitOn, startedCred); gErr != nil {
			return false, false, gErr
		} else if done {
			return false, true, nil
		}
	}
	return false, false, nil
}

// finalizeActivity runs the clean-pass tail of an attempt (constructionManager.md §6.3
// steps 5a-8a): relay the architecture +1, record the change reviewed, perform the gated
// merge (interventionEngine is the App-only-merge authority), record the binary activity
// exit, and record the per-activity construction COMPLETED. The git-forward steps are
// no-ops when the slice is unwired.
func (wf *workflows) finalizeActivity(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
	state *constructState,
	gitOn bool,
	startedCred railCredEnvelope,
) error {
	// --- Step 5a: relay the architecture +1 and record it (git-forward). ---
	if err := wf.relayArchApprovalAndRecord(ctx, in, gf, headVersion); err != nil {
		return err
	}

	// --- Step 6: record the change reviewed (head-state). ---
	v, e := wf.recordChangeReviewed(ctx, in, state, *headVersion, startedCred)
	if e != nil {
		return e
	}
	*headVersion = v

	// --- Step 6a: perform the gated merge and record it (git-forward). ---
	if err := wf.mergeAndRecord(ctx, in, gf, headVersion); err != nil {
		return err
	}

	// --- Step 8: record the binary activity exit. Behind the fence RecordActivityOutcome
	// is the fold of the exited + completed pair below: both stamped the SAME write-once
	// CompletedAt, which is the whole of an exit now that the coarse roll-up is derived,
	// so the two calls became one. ---
	if state.executionLedger {
		if err := wf.recordExecutionOutcome(ctx, in, state, headVersion, startedCred,
			projectstate.ActivityOutcomeCompleted, projectstate.FailureReasonUnknown, ""); err != nil {
			return err
		}
	} else {
		v2, e2 := wf.recordActivityExited(ctx, in, *headVersion, projectstate.ActivityOutcomeCompleted, startedCred)
		if e2 != nil {
			return e2
		}
		*headVersion = v2

		// --- Step 8a: record the per-activity construction COMPLETED (Task 3). Flip the
		// activity to Done so the pump's eligibility selection unblocks its dependents on
		// the next tick. Dormant (no-op) when the git slice is unwired. ---
		if gitOn {
			if err := wf.recordActivityCompleted(ctx, in, startedCred, headVersion); err != nil {
				return err
			}
		}
	}

	state.stage = StageExited
	workflow.GetLogger(ctx).Info("construction activity exited", "activityId", in.ActivityID)
	return nil
}

// runPipeline submits the pipeline then polls observe between durable startTimer
// waits until the pipeline reaches a terminal phase (§6.3 step 3). On each observe it
// ALSO reads the PR's CI rollup and mirrors it onto the head-state (the git-forward
// poll-loop verb, C-MCN-GIT) — dormant when the git slice is unwired.
func (wf *workflows) runPipeline(ctx workflow.Context, in constructActivityInput, phase projectstate.ActivityMethodPhase, state *constructState, gf *gitForward, headVersion *projectstate.Version) (pipelineObservation, error) {
	// The Figure A-1 task this dispatch's episode attributes to (Task 10). runPipeline is
	// the AGENT-WORK dispatch — it submits the job that runs /service-detailed-design,
	// /service-construction, and so on — so the episode is burned on the phase's AI-work
	// task (projectstate.AgentTaskFor: srs / stp / detailedDesign / construction /
	// integration), NOT on its gate task. The gate is the REVIEW of this work and belongs
	// to awaitPhaseDecision; stamping it here would record the agent that WROTE the
	// detailed design as its reviewer, inverting spec R1 permanently and undetectably.
	// The attempt number is drawn from the SAME per-task counter regardless of why this
	// call is happening — the phase's first dispatch (walkPhases) or a gated phase's
	// SendBack redraft (awaitPhaseDecision) both land here, which is exactly what makes
	// a send-back render detailedDesign#1 → designReview#1 → detailedDesign#2.
	//
	// MANAGED-SCAFFOLD SYNC (amendment §C.1.4): on the GitHub venue the seated construct
	// workflow is brought current BEFORE the dispatch, so a note never rides into a YAML
	// that would reject it and every repo is re-seated automatically. A failed sync
	// dispatches NOTHING and reads as a failed run (the intervention path, as a failed
	// pipeline). Behind the operator-note-delivery version, like the note itself.
	if state.noteDelivery {
		if obs, ok := wf.syncScaffoldBeforeDispatch(ctx, in, gf); !ok {
			return obs, nil
		}
	}
	task := projectstate.AgentTaskFor(phase)
	attempt := state.nextTaskAttempt(task)
	// The attempt the ledger now KEEPS, not just the number this counter mints. It opens
	// pending, before the dispatch it describes, so a run that dies mid-dispatch leaves an
	// attempt that says it started and never resolved rather than leaving nothing at all.
	attemptID := projectstate.AttemptID(string(in.ActivityID), task, attempt)
	state.workAttemptID = attemptID
	if err := wf.openWorkAttempt(ctx, in, state, headVersion, gf.cred, task, attempt, attemptID); err != nil {
		return pipelineObservation{}, err
	}

	handle, err := wf.submitCarryingNotes(ctx, in, phase, state, attemptID, gf, headVersion)
	if err != nil {
		return pipelineObservation{}, err
	}

	var last pipelineObservation
	for range maxPipelinePolls {
		obs, err := wf.observePipeline(ctx, handle)
		if err != nil {
			return pipelineObservation{}, err
		}
		ph := obs.Phase
		state.pipelinePhase = &ph

		// Mirror the PR's CI rollup onto the head-state on the same cadence.
		if _, cerr := wf.observeCIAndRecord(ctx, in, gf, headVersion); cerr != nil {
			return pipelineObservation{}, cerr
		}

		if obs.Phase == PipelineSucceeded || obs.Phase == PipelineFailed {
			// Episode capture LAST, after this poll's business handling (§capture-seam) —
			// and BEFORE the attempt resolves, because the attempt cites that episode.
			wf.captureEpisode(ctx, in, handle, obs, true, task, attempt)
			if rerr := wf.resolveWorkAttempt(ctx, in, state, headVersion, gf.cred, task, attempt, attemptID, obs); rerr != nil {
				return pipelineObservation{}, rerr
			}
			return obs, nil
		}
		last = obs
		// Durable wait between polls (the Manager's own startTimer — category A).
		_ = workflow.Sleep(ctx, pipelinePollInterval)
	}
	// Poll budget exhausted without Succeeded/Failed (a stuck run, or a CANCELLED one —
	// this loop deliberately does not treat Cancelled as terminal). The dispatch still
	// burned tokens, so it still owes the ledger a record: carry whatever the LAST
	// observation held (usually nothing ⇒ a gap) under the exhaustion diagnostic.
	exhausted := pipelineObservation{
		Phase:      PipelineFailed,
		Diagnostic: "pipeline did not reach a terminal phase within the poll budget",
		RunURL:     last.RunURL,
		Episode:    last.Episode,
	}
	wf.captureEpisode(ctx, in, handle, exhausted, true, task, attempt)
	if rerr := wf.resolveWorkAttempt(ctx, in, state, headVersion, gf.cred, task, attempt, attemptID, exhausted); rerr != nil {
		return pipelineObservation{}, rerr
	}
	return pipelineObservation{Phase: exhausted.Phase, Diagnostic: exhausted.Diagnostic}, nil
}

// ---------------------------------------------------------------------------
// Conditional per-phase approval gate (Task 6). runPhaseGate records the phase
// start, and — iff the committed ReviewPolicy requires a human for this
// (activityType, phase) — suspends on the phase-multiplexed decision signal. Approve
// records completion; SendBack redrafts THIS phase up to maxPhaseRedrafts and then
// (mirroring systemdesign) KEEPS awaiting the human — it NEVER re-enters the variance
// loop and never fails the activity. Returns done=true only when the gate has
// terminally recorded this activity (there is no such terminal in v1, but the
// signature preserves that seam). The phase-start / head-state records are gated on
// gitOn; under the NON-GIT profile an empty policy is inert and produces no
// head-state writes (aside from the in-memory completedPhases bookkeeping). Under the
// GIT profile, RecordPhaseStarted and RecordPhaseCompleted are emitted for EVERY
// phase regardless of whether a human gate is active — this is intentional progress
// tracking (gitOn-gated) and is NOT byte-for-byte the non-git path.
func (wf *workflows) runPhaseGate(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	policy projectstate.ReviewPolicy,
	state *constructState,
	gf *gitForward,
	headVersion *projectstate.Version,
	gitOn bool,
	cred railCredEnvelope,
) (bool, error) {
	if gitOn {
		v, e := wf.recordPhaseStarted(ctx, in, phase, state, *headVersion, cred)
		if e != nil {
			return false, e
		}
		*headVersion = v
	}
	// The gate's own ledger state starts clear on every entry, so a phase that opens no
	// round (no review task, or off the fence) can never settle the previous phase's.
	state.gate = gateLedger{}

	// ONE call decides both halves of the review question: who reviews (the engine's
	// reviewer rows) and whether a human must sign off (the project's committed
	// ReviewPolicy plus the non-overridable deploy/spend/schema floor). It used to be
	// two lookups in two components, which is how the kind table and the gate policy
	// drifted apart. An engine refusal is LOGGED and SHOWN (reviewSetError) and opens
	// the gate rather than failing the activity — the set is display-only in v1 and a
	// roster defect must not cost the work. A call that only assigns and logs emits no
	// commands, so this still needs no version gate and every replay fixture replays
	// unchanged.
	//
	// I1 — the roster and the gate occurrence are the same fact: the set is published
	// exactly when the gate opens, on EVERY entry including a redraft's re-entry at the
	// same phase, and leaveHumanStage takes it down with the occurrence it belonged to.
	// The proposal is pure and deterministic over (type, phase, policy, floor,
	// snapshot contracts), so a re-entry re-derives the identical answer.
	set, err := wf.proposeReviewSet(in, phase, policy, state)
	if err != nil {
		workflow.GetLogger(ctx).Error("review engine refused to propose reviewers; the gate opens without a reviewer set",
			"activityId", in.ActivityID, "lifecyclePhase", phase.String(), "err", err.Error())
		state.reviewSet, state.reviewSetError = nil, err.Error()
		return false, wf.gateWithoutHuman(ctx, in, phase, state, ReviewSet{}, err.Error(), gf, headVersion, gitOn, cred)
	}
	if set.RequiresHuman == nil || !*set.RequiresHuman {
		state.reviewSet, state.reviewSetError = nil, ""
		return false, wf.gateWithoutHuman(ctx, in, phase, state, set, "", gf, headVersion, gitOn, cred)
	}
	state.reviewSet, state.reviewSetError = &set, "" // NOTE: *ReviewSet (B6)

	if oerr := wf.openGateRound(ctx, in, phase, state, set, gf, headVersion, cred); oerr != nil {
		return false, oerr
	}
	return wf.awaitPhaseDecision(ctx, in, phase, state, set, gf, headVersion, gitOn, cred)
}

// gateWithoutHuman closes a gate no person is asked to answer: the committed policy
// requires none here, or the engine refused to staff one and the gate opens rather than
// cost the work. It is still a gate that HAPPENED — before stage 3 it left no trace at
// all, which is exactly how a vibes preset auto-approved for two months with nothing in
// the data to show for it — so the ledger records the round, the roster (empty when the
// engine refused, with the refusal appended as the engine's own abstention so the reason
// is not lost to a log line), and the passing decision the policy made.
func (wf *workflows) gateWithoutHuman(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	state *constructState,
	set ReviewSet,
	refusal string,
	gf *gitForward,
	headVersion *projectstate.Version,
	gitOn bool,
	cred railCredEnvelope,
) error {
	if state.executionLedger {
		if err := wf.openGateRound(ctx, in, phase, state, set, gf, headVersion, cred); err != nil {
			return err
		}
		if refusal != "" {
			if err := wf.appendVerdict(ctx, in, state, headVersion, cred, projectstate.ReviewVerdict{
				ReviewerRole: gateRoleReviewEngine,
				Actor:        gateRoleReviewEngine,
				Verdict:      projectstate.VerdictAbstain,
				Summary:      refusal,
				AttemptID:    state.workAttemptID,
			}, nil); err != nil {
				return err
			}
		}
		if err := wf.decideRound(ctx, in, state, headVersion, cred, projectstate.RoundPassed, decidedByPolicy); err != nil {
			return err
		}
	}
	return wf.completePhase(ctx, in, phase, state, headVersion, gitOn, cred)
}

// awaitPhaseDecision is the suspend + redraft loop of the gate (extracted so
// runPhaseGate stays under the gocognit budget). It drains the phase-multiplexed
// decision channel until a decision for THIS phase arrives, then acts on it: Approve
// completes the phase; SendBack redrafts THIS phase in place (its OWN redraft budget,
// NOT the variance budget); on redraft exhaustion it keeps awaiting the human.
//
// set is the answer runPhaseGate already got from the engine. A re-entry RE-PUBLISHES it
// rather than re-asking: the engine is pure over inputs that do not change inside a
// gate, so the re-derived answer would be identical, and carrying it keeps I1 exact —
// the roster that goes back up is the one that belonged to this gate.
func (wf *workflows) awaitPhaseDecision(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	state *constructState,
	set ReviewSet,
	gf *gitForward,
	headVersion *projectstate.Version,
	gitOn bool,
	cred railCredEnvelope,
) (bool, error) {
	ch := workflow.GetSignalChannel(ctx, signalPhaseDecision)
	redraft := 0
	activityType := in.Activity.activityTypeName()
	state.enterPhaseGate(ctx, phase.String(), redraft)
	for {
		sig := receivePhaseDecision(ctx, ch, phase.String())
		switch sig.Decision {
		case PhaseDecisionUnknown:
			// zero-value sentinel, not a real decision — ignore and keep awaiting, same as default.
		case PhaseApprove:
			state.leaveHumanStage(ctx, activityType, gateOutcomeApproved)
			if e := wf.closeGateRound(ctx, in, state, headVersion, cred, projectstate.VerdictApprove, projectstate.RoundPassed, sig.Feedback); e != nil {
				return false, e
			}
			return false, wf.completePhase(ctx, in, phase, state, headVersion, gitOn, cred)
		case PhaseSendBack:
			redraft++
			if redraft >= maxPhaseRedrafts {
				// Exhausted the human-paced redraft budget. Do NOT fail the activity and do
				// NOT re-enter the variance loop — keep awaiting the human, surfacing that
				// redrafting is spent (mirrors systemdesign's anti-wedge staging).
				// DEFENSIVE since B1.3: SubmitPhaseDecision refuses a SendBack while
				// redraftExhausted, so only a signal that bypassed the façade lands here.
				workflow.GetLogger(ctx).Warn("phase redraft budget exhausted; keep awaiting human decision",
					"activityId", in.ActivityID, "phase", phase.String())
				state.leaveHumanStage(ctx, activityType, gateOutcomeSentBackExhausted)
				state.reviewSet, state.reviewSetError = &set, ""
				state.enterPhaseGate(ctx, phase.String(), redraft)
				continue
			}
			state.leaveHumanStage(ctx, activityType, gateOutcomeSentBack)
			if e := wf.sendBackGate(ctx, in, phase, state, headVersion, cred, sig.Feedback); e != nil {
				return false, e
			}
			state.stage = StagePipelineRunning
			if _, e := wf.runPipeline(ctx, in, phase, state, gf, headVersion); e != nil {
				return false, e
			}
			state.reviewSet, state.reviewSetError = &set, ""
			state.enterPhaseGate(ctx, phase.String(), redraft)
			// The redraft re-enters the gate, and a re-entry is a NEW round: round n+1 over
			// the attempt the redraft just produced, with the roster that belonged to it.
			if e := wf.openGateRound(ctx, in, phase, state, set, gf, headVersion, cred); e != nil {
				return false, e
			}
		default:
			// Unknown decision: ignore and keep awaiting the human.
		}
	}
}

// sendBackGate records a rejection, whichever rail the execution is on.
//
// Behind the fence it is THREE facts: the human's sendBack verdict with the comments that
// rode with it, the round decided sentBack, and the REJECTED attempt at the review task
// that leaves the phase incomplete — which is what makes the redraft render as App A's
// "a failing review causes the developer to repeat the preceding internal task". The
// feedback itself rides the redraft workflow-locally; it is no longer ALSO stored as a
// NoteSendBack, because the round is the record of the send-back now and the same fact
// stored twice is how the two disagree later.
//
// Off the fence it is what it always was: one OperatorNote, and nothing else anywhere.
func (wf *workflows) sendBackGate(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	fb *ReviewFeedback,
) error {
	if !state.executionLedger {
		return wf.recordOperatorNote(ctx, in, state, headVersion, cred, projectstate.NoteSendBack, phase.String(), feedbackText(fb))
	}
	if err := wf.closeGateRound(ctx, in, state, headVersion, cred, projectstate.VerdictSendBack, projectstate.RoundSentBack, fb); err != nil {
		return err
	}
	if err := wf.rejectGateAttempt(ctx, in, state, headVersion, cred); err != nil {
		return err
	}
	carrySendBackFeedback(ctx, in, state, phase.String(), fb)
	return nil
}

// ---------------------------------------------------------------------------
// The human stage (B1.2; ruled: workflow.Now, no GetVersion). Every place the workflow
// waits for a person — a phase gate, the local merge hold, an escalation — enters and
// leaves through ONE pair of helpers, so the session view, the construction_gate_wait
// metric and the construction.gate.decided log line cannot disagree about which gate,
// which occurrence, or how long it waited. Assignments, workflow.Now, the metrics handler
// and the workflow logger emit NO commands, so this needs no version gate: every replay
// fixture under testdata/replay/ replays unchanged, and a query served by replay
// rebuilds the original awaitingSince.
// ---------------------------------------------------------------------------

// takeoverGateKey is the awaitingGate an escalation waits at (the operator steers with
// OverrideActivity; no phase decision closes it).
const takeoverGateKey = "takeover"

// The closed outcome vocabulary a human stage ends in (the metric's outcome tag and the
// log line's). An override adds its kind: "override:retry", "override:skip", ….
const (
	gateOutcomeApproved          = "approved"
	gateOutcomeSentBack          = "sentBack"
	gateOutcomeSentBackExhausted = "sentBackExhausted"
	gateOutcomeTimedOut          = "timedOut"
	gateOutcomeOverridePrefix    = "override:"
)

// gateMetrics is the handler the gate-wait timer records through: the workflow's own
// metrics handler, which the SDK suppresses on replay (the OTel handler the composition
// root wires). A package-level seam only so a test can capture what is recorded — SDK
// v1.44's test environment exposes no metrics hook.
var gateMetrics = workflow.GetMetricsHandler

// enterPhaseGate enters a phase approval gate after redrafts SendBack redrafts: the gate
// has no budget left once a further SendBack could not redraft it.
func (s *constructState) enterPhaseGate(ctx workflow.Context, key string, redrafts int) {
	s.redraftExhausted = redrafts+1 >= maxPhaseRedrafts
	s.enterHumanStage(ctx, StageAwaitingApproval, key, 0)
}

// enterHumanStage starts one occurrence of a human stage: the stage, the gate it waits
// at, the occurrence identity (workflow.Now), and — when wait > 0 — when it gives up.
func (s *constructState) enterHumanStage(ctx workflow.Context, stage ConstructionStage, gate string, wait time.Duration) {
	s.stage = stage
	s.awaitingGate = gate
	s.awaitingSince = workflow.Now(ctx)
	s.awaitingUntil = nil
	if wait > 0 {
		until := s.awaitingSince.Add(wait)
		s.awaitingUntil = &until
	}
}

// leaveHumanStage ends the current occurrence: it records the construction_gate_wait
// timer (tags: the gate CLASS, the outcome and the activity type — never the activity
// id, which would make the series unbounded) and the construction.gate.decided log line
// (the dependable surface while the prod OTLP export is an open earmark), then clears
// the awaiting fields AND the reviewer set.
//
// I1: the roster (and an engine refusal) describes the OCCURRENCE, not the activity, so
// it comes down with it. It used to be cleared on gate ENTRY only, which left a decided
// gate's reviewers on the session view until the next gate opened — and the Activity
// Experience's takeover card read them as live. surfaceReviewSet puts a fresh roster up
// on every entry, including a redraft's re-entry, so the pair stays balanced.
func (s *constructState) leaveHumanStage(ctx workflow.Context, activityType, outcome string) {
	waited := workflow.Now(ctx).Sub(s.awaitingSince)
	gateMetrics(ctx).WithTags(map[string]string{
		"gate":          humanGateClass(s.awaitingGate),
		"outcome":       outcome,
		"activity_type": activityType,
	}).Timer("construction_gate_wait").Record(waited)
	workflow.GetLogger(ctx).Info("construction.gate.decided",
		"projectId", string(s.projectID), "activityId", string(s.activityID),
		"gate", s.awaitingGate, "outcome", outcome,
		"waitedMs", waited.Milliseconds(), "awaitingSince", s.awaitingSince)
	s.awaitingGate, s.awaitingSince, s.awaitingUntil = "", time.Time{}, nil
	s.reviewSet, s.reviewSetError = nil, ""
}

// humanGateClass is the bounded gate tag: phase, merge or takeover.
func humanGateClass(gate string) string {
	switch gate {
	case mergeGateKey:
		return "merge"
	case takeoverGateKey:
		return takeoverGateKey
	default:
		return "phase"
	}
}

// receivePhaseDecision blocks on the decision channel, draining and DISCARDING
// decisions for other gate keys (stale/multiplexed), until one for THIS key
// arrives. key is a phase's wire name (phase.String()) or the merge gate's
// mergeGateKey — the signal payload's Phase field is a plain string that
// SubmitPhaseDecision validates against exactly those keys, so the merge gate
// rides the same machinery.
func receivePhaseDecision(ctx workflow.Context, ch workflow.ReceiveChannel, key string) phaseDecisionSignal {
	var sig phaseDecisionSignal
	for {
		ch.Receive(ctx, &sig)
		if sig.Phase == key {
			return sig
		}
	}
}

// completePhase is the SINGLE phase-completion path (both the no-gate branch and the
// Approve branch call it). It MARKS the LIVE in-memory completedPhases set
// UNCONDITIONALLY (this is what closes the variance-retry re-gate and the non-git
// case where no head-state completion record exists to re-read), THEN records the
// completion durably.
//
// Behind the execution-ledger fence that record is the PASSED gate attempt this workflow
// now writes itself (passGateAttempt) — the one fact every reader derives a lifecycle
// phase's completion from. RecordPhaseCompleted is NOT also called there: its entire body
// is the synthesis of exactly that attempt on the workflow's behalf, so calling both
// would be the same completion written twice by two rails.
func (wf *workflows) completePhase(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	state *constructState,
	headVersion *projectstate.Version,
	gitOn bool,
	cred railCredEnvelope,
) error {
	state.completedPhases[phase] = true
	if state.executionLedger {
		return wf.passGateAttempt(ctx, in, state, headVersion, cred)
	}
	if !gitOn {
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordPhaseCompleted(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), phase, "", cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// ---------------------------------------------------------------------------
// Policy-gated LOCAL merge step (local-merge-and-policy Commit 1). In the
// rail-dormant local profile (the local construction executor — gitOn without a
// PR rail) the commits land on activity/<id> and NOTHING else merges them: this
// step is what finishes the git-forward story there. The DECISION reuses the
// Task-7 machinery verbatim — the reviewEngine's ProposeReviews/RequiresHuman
// verdict at MethodPhaseConstruction with the non-overridable risk floor — so:
//   - vibes (and the legacy empty policy)   → auto-merge, no hold;
//   - checkpoints / full                    → hold at an approval gate (the
//     SAME suspend/approve machinery as the phase gates, keyed mergeGateKey)
//     and merge on Approve;
//   - a risk-floor-flagged activity (deploy/spend/schema contract) → ALWAYS
//     hold, regardless of preset, including vibes.
// The merge itself is EXECUTED by the local pipeline arm via the frozen Submit
// surface (DispatchInputs["job"]="merge" — agenticjob.DispatchJobMerge):
// a --no-ff merge of activity/<id> into main + branch delete, atomic-on-intent
// (a conflict aborts in a throwaway clone; nothing partial ever lands). A merge
// failure flows through the SAME intervention path as a failed phase pipeline.
// ---------------------------------------------------------------------------

// mergeGateKey is the phaseDecision key the merge hold suspends on. It is NOT an
// ActivityMethodPhase — SubmitPhaseDecision's validatePhaseDecision admits it
// alongside the five phases (Approve only), so the operator releases the merge
// with SubmitPhaseDecision(projectID, activityID, mergeGateKey, Approve).
const mergeGateKey = "merge"

// ---------------------------------------------------------------------------
// Operator notes (plan B1.4). A note is PENDING from the moment it is recorded until an
// agent dispatch carries it: every pending note rides the NEXT agent dispatch of the
// activity and is stamped delivered to that dispatch's AttemptID (the key its episode
// carries as TargetRef). Recording, carrying, stamping and the scaffold sync are all
// behind ONE change id, so an execution is wholly old or wholly new.
//
// WHAT "DELIVERED" MEANS (B1 fix round, I1). Only a note the dispatch carried IN FULL is
// stamped. When the pending notes exceed maxRenderedOperatorNotesBytes, the OLDEST are
// withheld so the newest steer arrives whole; the block says how many were withheld, and
// they stay pending for a later attempt.
//
// DELIVERY IS AT-LEAST-ONCE (M4). The stamp follows a successful submit, and is its own
// write with its own bounded retry (noteStampRetryWindow). If it still fails, the run
// goes on — the job is already dispatched — and the note stays pending, so the next
// attempt carries it again: an agent may see one note twice, never zero times. A note
// is never carried twice into the SAME attempt (constructState.carriedTo), and the store
// refuses to stamp one note to two attempts.
//
// PENDING NOTES WITH NO DISPATCH AHEAD (M5). A retry whose phases are all complete (the
// local merge only), a takeover the operator finishes by hand, and a skip start no agent
// run, so their notes are kept and not stamped. They are neither expired nor dropped:
// they stay pending on the activity (the console counts them), and ride the activity's
// next agent dispatch if one ever comes (a re-queue). A skip note is never pending.
// ---------------------------------------------------------------------------

// changeOperatorNoteDelivery is the version marker gating note record/carry/stamp and
// the managed-scaffold sync before a GitHub-venue dispatch.
const changeOperatorNoteDelivery = "operator-note-delivery"

// ---------------------------------------------------------------------------
// THE EXECUTION LEDGER (stage 3). Until now this workflow wrote no attempt and no
// review data at all: nextTaskAttempt minted attempt numbers nothing recorded, every
// roster the review engine computed was thrown away once it had been displayed, and a
// send-back left a one-line OperatorNote — no roster, no verdict, no thread, no round
// number, no subject. This is where each of those becomes a real write, through
// activityExecutionAccess's twelve verbs.
//
// ONE CHANGE ID FOR ALL OF IT. Every write below is a Temporal Activity, so the command
// sequence moves at five points, and they are one feature: an execution is either on it
// or off it. Five ids would admit a half-fenced execution that opens a round and never
// decides it. The same argument changeLedgerPartialResume already makes by reusing its
// const at two call sites.
//
// WHAT THE OLD RAIL STOPS DOING BEHIND THE FENCE, so no fact is written twice:
//   - RecordActivityStarted  → OpenActivity (same StartedAt/type/variant, plus the pin)
//   - RecordPhaseCompleted   → the PASSED gate attempt this workflow now writes itself
//     (that verb's whole body is the synthesis of exactly that attempt)
//   - RecordActivityExited / RecordActivityFailed / RecordActivityCompleted
//     → RecordActivityOutcome (the fold of all three)
//   - the NoteSendBack record → the round, with the feedback carried to the redraft
//     workflow-locally (carrySendBackFeedback)
//
// RecordPhaseStarted and RecordChangeReviewed keep being called on BOTH paths: task 3
// retired their bodies in place, so they now write no fact at all and cannot duplicate
// one. Stage 4 deletes them with the fixtures that record them.
// ---------------------------------------------------------------------------

// changeExecutionLedger is the ONE version marker gating every execution-ledger write.
const changeExecutionLedger = "execution-ledger-writes"

// The gate's ledger vocabulary. A construction gate's human row has no named person
// behind it — the phaseDecision signal carries feedback, not an identity — so the ROLE is
// what the round records and "operator" is who the platform can honestly say answered it.
const (
	gateRoleHuman     = "human"
	gateActorOperator = "operator"
	// gateRoleReviewEngine owns the ABSTENTION a refused roster leaves on the round. A
	// gate the engine could not staff still happened, and the reason belongs where a
	// reader will meet it — on the round — not only in a log line.
	gateRoleReviewEngine = "reviewEngine"
	// The two things that close a construction gate: a person answering it, or the
	// committed review policy saying no person was needed here.
	decidedByOperator = "operator"
	decidedByPolicy   = "reviewPolicy"
)

// lifecyclePinFor is the lifecycle an activity's task DAG is resolved against for the
// whole of its run: the type key the row's profile is keyed on, and the method-assets
// release that key was read out of. Without it a release landing mid-flight re-shapes an
// activity that is already running, and every attempt and round already on the ledger
// would be read back against a DAG they were never written under.
func lifecyclePinFor(act constructionActivity) projectstate.LifecyclePin {
	return projectstate.LifecyclePin{
		TypeKey:       projectstate.LifecycleKeyFor(act.Type, act.Variant),
		AssetsVersion: methodassets.Version(),
	}
}

// openActivity births the activity's execution row and pins the lifecycle in force.
func (wf *workflows) openActivity(ctx workflow.Context, in constructActivityInput, state *constructState, cred railCredEnvelope, headVersion *projectstate.Version) error {
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionOpenActivity(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), in.Activity.Type, in.Activity.Variant, lifecyclePinFor(in.Activity), cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// recordAttempt appends or resolves ONE attempt on the append-only task ledger. One id
// names one attempt: the pending record this opens and the terminal that resolves it are
// the SAME AttemptID, which is also the key the episode ledger carries as TargetRef.
func (wf *workflows) recordAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	attempt projectstate.TaskAttemptInput,
) error {
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionRecordAttemptOutcome(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), attempt, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// openWorkAttempt records the PENDING agent-work attempt a dispatch is about to burn, so
// a run that dies mid-dispatch leaves an attempt that says it started and never resolved
// rather than nothing at all. A no-op off the fence.
func (wf *workflows) openWorkAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	task projectstate.MethodTask,
	attempt int,
	attemptID string,
) error {
	if !state.executionLedger || task == "" {
		return nil
	}
	return wf.recordAttempt(ctx, in, state, headVersion, cred, projectstate.TaskAttemptInput{
		AttemptID: attemptID,
		TaskID:    task,
		Attempt:   int64(attempt),
		Actor:     projectstate.ActorAgent,
		Outcome:   projectstate.OutcomePending,
	})
}

// resolveWorkAttempt resolves that attempt against the terminal observation, citing the
// episode the dispatch burned as its evidence. The episode append runs FIRST (the
// capture-seam's own ordering), so by the time this cites an episode id the ledger holds
// it; a dispatch that mined no summary cites nothing rather than a ref nobody can follow.
func (wf *workflows) resolveWorkAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	task projectstate.MethodTask,
	attempt int,
	attemptID string,
	obs pipelineObservation,
) error {
	if !state.executionLedger || task == "" {
		return nil
	}
	rec := projectstate.TaskAttemptInput{
		AttemptID: attemptID,
		TaskID:    task,
		Attempt:   int64(attempt),
		Actor:     projectstate.ActorAgent,
		Outcome:   attemptOutcomeFor(obs.Phase),
	}
	if obs.Episode != nil {
		rec.EvidenceKind, rec.EvidenceRef = projectstate.EvidenceEpisode, obs.Episode.EpisodeID
	}
	return wf.recordAttempt(ctx, in, state, headVersion, cred, rec)
}

// attemptOutcomeFor maps a terminal pipeline phase onto the attempt's terminal. Only a
// SUCCEEDED run passed; everything else — failed, cancelled, or a phase that is not
// terminal at all (the poll budget ran out with the run still going) — is a failed
// attempt, because the task it was dispatched for did not get done.
func attemptOutcomeFor(p PipelinePhase) projectstate.TaskOutcome {
	switch p {
	case PipelineSucceeded:
		return projectstate.OutcomePassed
	case PipelineFailed, PipelineCancelled, PipelinePending, PipelineRunning, PipelinePhaseUnknown:
		return projectstate.OutcomeFailed
	}
	// Unreachable for the six defined PipelinePhase values above; kept as a defensive
	// fallback for an out-of-range ordinal, which is a failure like any other.
	return projectstate.OutcomeFailed
}

// openGateRound opens the review round for the gate the workflow has just reached, with
// the roster the engine computed and the subject the round judges. It is called on EVERY
// entry to a gate — including a redraft's re-entry, which is a NEW round — and on the
// no-human and engine-refused paths too, because a gate that happened with no roster is
// still a gate that happened and the read model has to be able to say so.
//
// A lifecycle phase with no review task, or none whose work is dispatched, gets no round:
// there is nothing to judge and nothing that judged it, and inventing a round for it
// would put a review in the ledger that never took place.
//
// THE CRASH WINDOW, STATED. A run that dies between OpenReviewRound and DecideReviewRound
// leaves round n PENDING forever: the resume re-walks the incomplete phase, mints n+1 off
// the same counter (seedResumeFromLedger reads both ledgers) and opens a fresh round, so
// nothing is duplicated and no id collides — but nobody goes back to close n. That is the
// honest record of what happened (a review was opened and never decided), and it is
// deliberately not papered over here: withdrawing an abandoned round needs to know the run
// is gone, which a workflow cannot know about itself. A sweep owns it in stage 4, where
// RoundWithdrawn exists for exactly this. Until then a pending round with a later round on
// the same gate reads as abandoned, and every read path derives completion from the
// ATTEMPT ledger, so a stranded pending round cannot make a phase look complete or
// incomplete either way.
func (wf *workflows) openGateRound(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	state *constructState,
	set ReviewSet,
	gf *gitForward,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
) error {
	state.gate = gateLedger{}
	gate, work := projectstate.GateTaskFor(phase), projectstate.AgentTaskFor(phase)
	if !state.executionLedger || gate == "" || work == "" {
		return nil
	}
	n := state.nextTaskAttempt(gate)
	state.gate = gateLedger{
		task:    gate,
		number:  n,
		roundID: projectstate.AttemptID(string(in.ActivityID), gate, n),
		subject: gateSubjectRef(gf, state.workAttemptID),
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionOpenReviewRound(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID),
			projectstate.ReviewRoundInput{
				RoundID:    state.gate.roundID,
				TaskID:     gate,
				Reviews:    work,
				Round:      int64(n),
				SubjectRef: state.gate.subject,
				Reviewers:  roundReviewers(set),
			}, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// gateSubjectRef names WHAT the round judges. On the PR rail that is the pull request the
// activity's work is on — the thing a reviewer actually opens. With the rail dormant
// there is no such handle (construction stages no model: its output is a commit the agent
// pushed to the activity branch), so the round cites the work ATTEMPT it judged, which
// joins to both the attempt ledger and the episode that burned it.
func gateSubjectRef(gf *gitForward, workAttemptID string) projectstate.SubjectRef {
	if gf.enabled && gf.prRef != "" {
		return projectstate.SubjectRef{Kind: projectstate.SubjectPullRequest, Ref: gf.prRef}
	}
	return projectstate.SubjectRef{Kind: projectstate.SubjectArtifact, Ref: workAttemptID}
}

// roundReviewers is the roster the round persists: the engine's rows, plus the human row
// when the policy requires a person. The engine's rows are NOT Required — the reviewer
// set is advisory in v1 and nothing dispatches it, and a row marked required that nothing
// waits for would make the round claim a gate it never had.
//
// Actor is the role for an engine row because on this rail a role IS the agent charter
// dispatched for it; the human row has no name to give, so it carries the operator the
// platform can honestly attribute the decision to.
func roundReviewers(set ReviewSet) []projectstate.RoundReviewer {
	out := make([]projectstate.RoundReviewer, 0, len(set.Reviewers)+1)
	for _, r := range set.Reviewers {
		out = append(out, projectstate.RoundReviewer{Role: r.Role, Actor: r.Role, Required: false})
	}
	if set.RequiresHuman != nil && *set.RequiresHuman {
		out = append(out, projectstate.RoundReviewer{Role: gateRoleHuman, Actor: gateActorOperator, Required: true})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// appendVerdict lands one reviewer's judgement and the comments it cites in ONE commit.
// A no-op when this gate opened no round.
func (wf *workflows) appendVerdict(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	verdict projectstate.ReviewVerdict,
	comments []projectstate.ReviewComment,
) error {
	if state.gate.roundID == "" {
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionAppendReviewVerdict(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), state.gate.roundID, verdict, comments, nil, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// decideRound stamps the round's terminal — a separate, later fact from the verdicts on
// it, which is why it is a second verb and not a field of the first. It also records who
// the gate attempt this round settles will name as its actor.
func (wf *workflows) decideRound(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	outcome projectstate.ReviewRoundOutcome,
	decidedBy string,
) error {
	if state.gate.roundID == "" {
		return nil
	}
	state.gate.actor = projectstate.ActorSystem
	if decidedBy == decidedByOperator {
		state.gate.actor = projectstate.ActorHuman
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionDecideReviewRound(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), state.gate.roundID, outcome, decidedBy, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// closeGateRound appends the human's verdict — with the comments that rode with it — and
// then decides the round. Two verbs, not one: other reviewers append to the same round
// before the human's, and the decision is a separate terminal fact about the round.
func (wf *workflows) closeGateRound(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	verdict projectstate.VerdictKind,
	outcome projectstate.ReviewRoundOutcome,
	fb *ReviewFeedback,
) error {
	if !state.executionLedger || state.gate.roundID == "" {
		return nil
	}
	f := feedbackText(fb)
	if err := wf.appendVerdict(ctx, in, state, headVersion, cred, projectstate.ReviewVerdict{
		ReviewerRole: gateRoleHuman,
		Actor:        gateActorOperator,
		Verdict:      verdict,
		Summary:      f.text,
		AttemptID:    state.workAttemptID,
	}, roundComments(f.comments)); err != nil {
		return err
	}
	return wf.decideRound(ctx, in, state, headVersion, cred, outcome, decidedByOperator)
}

// roundComments re-types a decision's anchored comments onto the round thread's. The
// ids, the round number and the open/answered status are the store's to mint, so only
// what the operator actually wrote travels.
func roundComments(in []AnchoredComment) []projectstate.ReviewComment {
	if len(in) == 0 {
		return nil
	}
	out := make([]projectstate.ReviewComment, 0, len(in))
	for _, c := range in {
		out = append(out, projectstate.ReviewComment{Anchor: c.JSONPath, Text: c.Text, AuthorRole: gateRoleHuman})
	}
	return out
}

// passGateAttempt records the PASSED attempt at the phase's review task — App A's binary
// exit criterion, written where every reader derives phase completion from. It is the
// fact RecordPhaseCompleted used to synthesize on the workflow's behalf; the workflow
// writes it itself now, which is why that verb is no longer called behind the fence.
//
// Evidence is what the round judged, so a completion points at the thing that was
// reviewed rather than at the empty artifactRef the retired verb always passed.
func (wf *workflows) passGateAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
) error {
	if state.gate.task == "" {
		return nil
	}
	kind, ref := gateEvidence(state.gate.subject)
	return wf.recordAttempt(ctx, in, state, headVersion, cred, projectstate.TaskAttemptInput{
		AttemptID:    projectstate.AttemptID(string(in.ActivityID), state.gate.task, state.gate.number),
		TaskID:       state.gate.task,
		Attempt:      int64(state.gate.number),
		Actor:        state.gate.actor,
		Outcome:      projectstate.OutcomePassed,
		EvidenceKind: kind,
		EvidenceRef:  ref,
	})
}

// rejectGateAttempt is passGateAttempt's send-back twin: a REJECTED attempt at the review
// task, which is what leaves the phase incomplete and makes the redraft that follows
// render as Löwy's "a failing review repeats the preceding task".
func (wf *workflows) rejectGateAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
) error {
	if !state.executionLedger || state.gate.task == "" {
		return nil
	}
	kind, ref := gateEvidence(state.gate.subject)
	return wf.recordAttempt(ctx, in, state, headVersion, cred, projectstate.TaskAttemptInput{
		AttemptID:    projectstate.AttemptID(string(in.ActivityID), state.gate.task, state.gate.number),
		TaskID:       state.gate.task,
		Attempt:      int64(state.gate.number),
		Actor:        state.gate.actor,
		Outcome:      projectstate.OutcomeRejected,
		EvidenceKind: kind,
		EvidenceRef:  ref,
	})
}

// gateEvidence maps the round's subject onto the attempt's evidence vocabulary, so the
// UI's click dispatch opens the right thing from either ledger.
func gateEvidence(subject projectstate.SubjectRef) (projectstate.EvidenceKind, string) {
	switch subject.Kind {
	case projectstate.SubjectPullRequest, projectstate.SubjectCommit:
		return projectstate.EvidenceGit, subject.Ref
	case projectstate.SubjectArtifact:
		return projectstate.EvidenceArtifact, subject.Ref
	}
	// An unset subject (a round this workflow did not open) cites nothing.
	return projectstate.EvidenceNone, ""
}

// recordExecutionOutcome stamps the activity's terminal on the execution row — the fold
// of the three retired terminals (exited / failed / completed). A non-zero reason IS the
// failure arm.
func (wf *workflows) recordExecutionOutcome(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	outcome projectstate.ActivityOutcome,
	reason projectstate.FailureReason,
	detail string,
) error {
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionRecordActivityOutcome(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), outcome, reason, detail, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// carrySendBackFeedback puts the send-back's feedback in front of the redraft WITHOUT
// recording it. Before stage 3 this was a NoteSendBack on the activity, and it was the
// only trace a send-back left anywhere. The round now holds the verdict, its summary and
// its comments, so a note beside it would be one fact stored twice; what the redraft
// still needs is the TEXT, and that rides the next dispatch as a workflow-local note
// nothing ever stamps delivered. Emits no command.
// THE ROUND IS THE SOURCE OF TRUTH AND THIS IS A CACHE OF IT. The note lives only in
// workflow memory, so it does not survive the run that made it — seedSendBackCarry
// rebuilds it from the durable round at the start of the next one.
//
// It is gated on noteDelivery for the same reason the recorded notes are: an execution
// without THAT marker must not add the operator_note dispatch input, because the seated
// construct workflow it dispatches into may predate the key.
func carrySendBackFeedback(ctx workflow.Context, in constructActivityInput, state *constructState, gate string, fb *ReviewFeedback) {
	f := feedbackText(fb)
	if !state.noteDelivery || strings.TrimSpace(f.text) == "" {
		return
	}
	state.noteSeq++
	note := projectstate.OperatorNote{
		NoteID:     operatorNoteID(in.ActivityID, workflow.GetInfo(ctx).WorkflowExecution.RunID, state.noteSeq),
		Kind:       projectstate.NoteSendBack,
		Gate:       gate,
		Text:       f.text,
		Comments:   noteComments(f.comments),
		RecordedAt: workflow.Now(ctx),
	}
	if state.ephemeralNotes == nil {
		state.ephemeralNotes = map[string]bool{}
	}
	state.ephemeralNotes[note.NoteID] = true
	state.pendingNotes = append(state.pendingNotes, note)
}

// seedSendBackCarry rebuilds the carry note from the DURABLE round, for every lifecycle
// phase whose latest gate round was sent back and whose redraft never went out. It runs
// once, at the start of a run, and is what makes the send-back's feedback survive a run
// boundary now that no note is stored for it.
func seedSendBackCarry(ctx workflow.Context, in constructActivityInput, state *constructState, acs projectstate.ActivityExecution) {
	for _, lifecyclePhase := range projectstate.ProfileFor(in.Activity.Type, in.Activity.Variant).PhaseIDs() {
		r, owed := owedSendBackRound(acs, lifecyclePhase)
		if !owed {
			continue
		}
		carrySendBackFeedback(ctx, in, state, lifecyclePhase.String(), roundFeedback(r))
	}
}

// owedSendBackRound is the latest round on the phase's gate WHEN the next dispatch still
// owes it feedback: it was sent back, and nothing has been re-dispatched since.
//
// "since" is read off the ledger rather than remembered, because the run that would
// remember it is the one that died. The send-back verdict names the work attempt it
// judged, so a LATER attempt at that work task is the one honest signal that the redraft
// already went out — whoever sent it. That is also the delivered-once guard: once the
// redraft has run, its attempt outranks the judged one and the steer is not carried twice.
func owedSendBackRound(acs projectstate.ActivityExecution, lifecyclePhase projectstate.ActivityMethodPhase) (projectstate.ReviewRound, bool) {
	gate, work := projectstate.GateTaskFor(lifecyclePhase), projectstate.AgentTaskFor(lifecyclePhase)
	if gate == "" || work == "" {
		return projectstate.ReviewRound{}, false
	}
	var latest projectstate.ReviewRound
	found := false
	for _, r := range acs.Reviews {
		if r.TaskID == gate && (!found || r.Round > latest.Round) {
			latest, found = r, true
		}
	}
	if !found || latest.Outcome != projectstate.RoundSentBack {
		return projectstate.ReviewRound{}, false
	}
	return latest, !redraftDispatched(acs, work, latest)
}

// redraftDispatched reports whether the ledger holds a RESOLVED work attempt later than
// the one the round judged. The judged attempt is found by the id the verdict names; the
// round NUMBER is the fallback, since one counter mints both and they cannot drift.
//
// RESOLVED, not merely present, and the distinction is the whole point. runPipeline opens
// the attempt BEFORE the dispatch it describes, so a later attempt sitting pending is
// exactly the run that died on the way out — the redraft never reached an agent and the
// steer is still owed. Counting it as delivered is how the feedback would be lost in the
// one case this guard exists for.
//
// A run that died while the redraft was actually RUNNING leaves the same pending attempt
// and will carry the note again. That is at-least-once, which is the delivery contract
// operator notes already state: an agent may see one note twice, never zero times.
func redraftDispatched(acs projectstate.ActivityExecution, work projectstate.MethodTask, r projectstate.ReviewRound) bool {
	judged := int(r.Round)
	for _, a := range acs.Attempts {
		if a.AttemptID != "" && a.AttemptID == sendBackJudgedAttempt(r) {
			judged = a.Attempt
			break
		}
	}
	for _, a := range acs.Attempts {
		if a.Task == work && a.Attempt > judged && a.Outcome != projectstate.OutcomePending {
			return true
		}
	}
	return false
}

// sendBackJudgedAttempt is the AttemptID the round's send-back verdict named.
func sendBackJudgedAttempt(r projectstate.ReviewRound) string {
	for _, v := range r.Verdicts {
		if v.Verdict == projectstate.VerdictSendBack {
			return v.AttemptID
		}
	}
	return ""
}

// roundFeedback rebuilds the operator's steer from a decided round: the send-back
// verdict's summary, and the comments on its thread that are still OPEN — which is the
// spec's own definition of pending feedback (§5.3), now that it is answered from the
// round rather than from a note.
func roundFeedback(r projectstate.ReviewRound) *ReviewFeedback {
	fb := &ReviewFeedback{}
	for _, v := range r.Verdicts {
		if v.Verdict == projectstate.VerdictSendBack {
			fb.Notes = v.Summary
			break
		}
	}
	for _, c := range r.Thread {
		if c.Status == projectstate.ReviewCommentOpen {
			fb.Comments = append(fb.Comments, AnchoredComment{JSONPath: c.Anchor, Text: c.Text})
		}
	}
	return fb
}

// maxRenderedOperatorNotesBytes caps the rendered notes block one dispatch carries, far
// under GitHub's 65,535-character workflow_dispatch input cap. The façade caps one note
// (maxOperatorNoteRunes, and maxOperatorNoteBodyBytes once rendered), so one note always
// fits whole: only several pending notes can reach the cap.
const maxRenderedOperatorNotesBytes = 16 << 10

// noteFramingReserveBytes is the room kept for one note's header line and the
// withheld-notes line, so a note the façade accepted always fits the block whole.
const noteFramingReserveBytes = 1 << 10

// maxOperatorNoteBodyBytes caps one note's rendered body (its text and anchored comments,
// as renderNoteBody writes them) at the façade.
const maxOperatorNoteBodyBytes = maxRenderedOperatorNotesBytes - noteFramingReserveBytes

// noteStampRetryWindow bounds the delivery stamp's own retry envelope (M4): attempts are
// uncapped inside it, and past it the run goes on with the note still pending.
const noteStampRetryWindow = 2 * time.Minute

// noteFeedback is one note's operator text and anchored comments, from a send-back's
// feedback or an override.
type noteFeedback struct {
	text     string
	comments []AnchoredComment
}

// feedbackText is a send-back's note; a nil feedback (a signal that bypassed the
// façade) is an empty note, which recordOperatorNote skips.
func feedbackText(f *ReviewFeedback) noteFeedback {
	if f == nil {
		return noteFeedback{}
	}
	return noteFeedback{text: f.Notes, comments: f.Comments}
}

// overrideNoteKind maps an override onto the note kind it records.
func overrideNoteKind(k OverrideKind) (projectstate.OperatorNoteKind, bool) {
	switch k {
	case OverrideRetry:
		return projectstate.NoteRetry, true
	case OverrideTakeover:
		return projectstate.NoteTakeover, true
	case OverrideReassign:
		return projectstate.NoteReassign, true
	case OverrideSkip:
		return projectstate.NoteSkip, true
	case OverrideUnknown:
		return projectstate.OperatorNoteKindUnknown, false
	}
	return projectstate.OperatorNoteKindUnknown, false
}

// operatorNoteID is a note's deterministic id: the activity, this run, and the run's
// note sequence — replay-stable, and unique across runs of the same activity.
func operatorNoteID(activityID ActivityID, runID string, seq int) string {
	return fmt.Sprintf("%s:note:%s:%d", activityID, runID, seq)
}

// recordOperatorNote keeps one operator note on the activity and, when its kind is
// delivered at all, queues it for the next agent dispatch. A no-op on an execution
// without the operator-note-delivery marker, and for a blank note (only a signal that
// bypassed the façade's checks can carry one).
func (wf *workflows) recordOperatorNote(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	kind projectstate.OperatorNoteKind,
	gate string,
	fb noteFeedback,
) error {
	if !state.noteDelivery || strings.TrimSpace(fb.text) == "" {
		return nil
	}
	state.noteSeq++
	note := projectstate.OperatorNoteInput{
		NoteID:   operatorNoteID(in.ActivityID, workflow.GetInfo(ctx).WorkflowExecution.RunID, state.noteSeq),
		Kind:     kind,
		Gate:     gate,
		Text:     fb.text,
		Comments: noteComments(fb.comments),
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordOperatorNote(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), note, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	// A retired-facet write onto the row, fenced on note delivery rather than on the
	// execution ledger, so it lands on a ledger-on run too (see rowAdvanced).
	state.rowAdvanced()
	recorded := projectstate.OperatorNote{
		NoteID: note.NoteID, Kind: note.Kind, Gate: note.Gate, Text: note.Text, Comments: note.Comments,
		RecordedAt: workflow.Now(ctx),
	}
	// The RA's own rule decides what is pending (a skip note never is).
	state.pendingNotes = append(state.pendingNotes,
		projectstate.PendingOperatorNotes(projectstate.ActivityExecution{OperatorNotes: []projectstate.OperatorNote{recorded}})...)
	return nil
}

// noteComments re-types a decision's anchored comments onto the note's.
func noteComments(in []AnchoredComment) []projectstate.NoteComment {
	if len(in) == 0 {
		return nil
	}
	out := make([]projectstate.NoteComment, 0, len(in))
	for _, c := range in {
		out = append(out, projectstate.NoteComment{JSONPath: c.JSONPath, Text: c.Text})
	}
	return out
}

// submitCarryingNotes dispatches one agent job carrying the pending notes, then — only
// once the submit has succeeded — stamps delivered to attemptID each note the block
// carried IN FULL, and drops those from the queue. A note the cap withheld, a note whose
// stamp failed (at-least-once, see the section comment) and every note of a failed
// submit stay pending for the next dispatch. A note already carried into attemptID is
// never carried into it again.
func (wf *workflows) submitCarryingNotes(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	state *constructState,
	attemptID string,
	gf *gitForward,
	headVersion *projectstate.Version,
) (pipelineHandle, error) {
	var carry []projectstate.OperatorNote
	for _, n := range state.pendingNotes {
		if state.carriedTo[n.NoteID] != attemptID {
			carry = append(carry, n)
		}
	}
	notes := renderOperatorNotes(carry)
	handle, err := wf.submitPipeline(ctx, pipelineSpec{
		ProjectID:    in.ProjectID,
		ActivityID:   string(in.ActivityID),
		ComponentID:  in.Activity.ComponentID,
		Phase:        phase.String(),
		Type:         in.Activity.Type,
		Variant:      in.Activity.Variant,
		OperatorNote: notes.block,
	})
	if err != nil {
		return pipelineHandle{}, err
	}
	delivered := map[string]bool{}
	for _, n := range notes.whole {
		if state.carriedTo == nil {
			state.carriedTo = map[string]string{}
		}
		state.carriedTo[n.NoteID] = attemptID
		// A workflow-local send-back note (stage 3) has no stored note to stamp: the review
		// round is its record, and the block it rode is its delivery. It leaves the queue
		// having been carried, without a store call that would only fail NotFound.
		if state.ephemeralNotes[n.NoteID] {
			delivered[n.NoteID] = true
			continue
		}
		if wf.stampNoteDelivered(ctx, in, state, n.NoteID, attemptID, gf, headVersion) {
			delivered[n.NoteID] = true
		}
	}
	var still []projectstate.OperatorNote
	for _, n := range state.pendingNotes {
		if !delivered[n.NoteID] {
			still = append(still, n)
		}
	}
	state.pendingNotes = still
	if len(still) > 0 {
		workflow.GetLogger(ctx).Info("operator notes stay pending after this dispatch",
			"activityId", string(in.ActivityID), "attemptId", attemptID, "pending", len(still), "withheldByTheCap", notes.withheld)
	}
	return handle, nil
}

// stampNoteDelivered records noteID delivered to attemptID and reports whether the store
// now says so. It never fails the run: the job is already dispatched. A stamp that still
// fails after its retry window leaves the note pending (at-least-once); a stamp the store
// refuses because the note was already delivered to another attempt means it is no
// longer pending, so that reads as delivered.
func (wf *workflows) stampNoteDelivered(ctx workflow.Context, in constructActivityInput, state *constructState, noteID, attemptID string, gf *gitForward, headVersion *projectstate.Version) bool {
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordOperatorNoteDelivered(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), noteID, attemptID, gf.cred.toProjectState())
	})
	if err == nil {
		*headVersion = v
		// The stamp applied, so the row advanced (see rowAdvanced). The two arms below did
		// NOT apply one — a ContractMisuse refusal writes nothing — so neither advances it.
		state.rowAdvanced()
		return true
	}
	if isRAContractMisuse(err) {
		workflow.GetLogger(ctx).Warn("the store already holds this note as delivered to another attempt; it is not pending",
			"activityId", string(in.ActivityID), "noteId", noteID, "attemptId", attemptID, "error", err.Error())
		return true
	}
	workflow.GetLogger(ctx).Warn("the note rode the dispatch but its delivery stamp failed; it stays pending and the next attempt carries it again",
		"activityId", string(in.ActivityID), "noteId", noteID, "attemptId", attemptID, "error", err.Error())
	return false
}

// syncScaffoldBeforeDispatch converges the repo's seated managed scaffold (the construct
// workflow among it) onto this server's template before a GitHub-venue dispatch. The
// local venue has no seated workflow (gf is dormant), so it is a no-op there. ok=false
// means the sync failed and nothing may be dispatched; obs is the failed run to report.
func (wf *workflows) syncScaffoldBeforeDispatch(ctx workflow.Context, in constructActivityInput, gf *gitForward) (pipelineObservation, bool) {
	if !gf.enabled {
		return pipelineObservation{}, true
	}
	changed, err := wf.Acts.RailSyncManagedScaffold(ctx, gf.repoRef, gf.cred.toRail())
	if err != nil {
		workflow.GetLogger(ctx).Error("managed-scaffold sync failed; nothing was dispatched",
			"activityId", string(in.ActivityID), "error", err.Error())
		return pipelineObservation{
			Phase: PipelineFailed,
			Diagnostic: "managed-scaffold sync failed — the seated construct workflow could not be proven current, " +
				"so nothing was dispatched: " + err.Error(),
		}, false
	}
	if changed {
		workflow.GetLogger(ctx).Info("managed scaffold drifted; re-seated the construct workflow before dispatch",
			"activityId", string(in.ActivityID))
	}
	return pipelineObservation{}, true
}

// renderedNotes is what one dispatch carries: the block, the notes it carries IN FULL
// (the only ones stamped delivered), and how many older notes the cap withheld.
type renderedNotes struct {
	block    string
	whole    []projectstate.OperatorNote
	withheld int
}

// notesSeparator sits between two notes, and after the withheld-notes line.
const notesSeparator = "\n\n"

// renderOperatorNotes renders the pending notes as the one block a dispatch carries,
// oldest first, each headed by its id, kind and gate. Within maxRenderedOperatorNotesBytes
// it keeps the NEWEST notes whole and withholds the oldest, naming how many it withheld.
// No notes render nothing.
//
// A single note the block cannot hold whole (only a signal that bypassed the façade's
// maxOperatorNoteBodyBytes can carry one) is carried cut and marked, and is NOT among the
// whole notes: it stays pending rather than be stamped delivered in part.
func renderOperatorNotes(notes []projectstate.OperatorNote) renderedNotes {
	if len(notes) == 0 {
		return renderedNotes{}
	}
	sections := make([]string, len(notes))
	for i, n := range notes {
		sections[i] = renderNoteSection(n)
	}
	start, total := len(notes), 0
	for i := len(notes) - 1; i >= 0; i-- {
		add := len(sections[i])
		if start < len(notes) {
			add += len(notesSeparator)
		}
		framing := 0
		if i > 0 {
			framing = len(withheldNotesLine(i)) + len(notesSeparator)
		}
		if total+add+framing > maxRenderedOperatorNotesBytes {
			break
		}
		total += add
		start = i
	}
	var b strings.Builder
	if start == len(notes) {
		last := len(notes) - 1
		if last > 0 {
			b.WriteString(withheldNotesLine(last) + notesSeparator)
		}
		b.WriteString(sections[last])
		return renderedNotes{block: cutRenderedOperatorNotes(b.String()), withheld: last}
	}
	if start > 0 {
		b.WriteString(withheldNotesLine(start) + notesSeparator)
	}
	b.WriteString(strings.Join(sections[start:], notesSeparator))
	return renderedNotes{block: b.String(), whole: notes[start:], withheld: start}
}

// renderNoteSection renders one note: its header line, then its body.
func renderNoteSection(n projectstate.OperatorNote) string {
	header := fmt.Sprintf("[operator note %s — %s", n.NoteID, operatorNoteKindName(n.Kind))
	if n.Gate != "" {
		header += " at " + n.Gate
	}
	return header + "]\n" + renderNoteBody(n.Text, n.Comments)
}

// renderNoteBody renders a note's text and its anchored comments — the part the façade
// caps at maxOperatorNoteBodyBytes.
func renderNoteBody(text string, comments []projectstate.NoteComment) string {
	var b strings.Builder
	b.WriteString(text)
	for _, c := range comments {
		fmt.Fprintf(&b, "\n  comment on %s: %s", c.JSONPath, c.Text)
	}
	return b.String()
}

// withheldNotesLine opens a block that withholds the n oldest pending notes.
func withheldNotesLine(n int) string {
	if n == 1 {
		return "[1 older operator note is not shown: the notes exceed the 16 KiB one dispatch carries. It stays pending and rides a later attempt; every note is on the activity.]"
	}
	return fmt.Sprintf("[%d older operator notes are not shown: the notes exceed the 16 KiB one dispatch carries. They stay pending and ride a later attempt; every note is on the activity.]", n)
}

// renderedNotesTruncated ends a block cut at maxRenderedOperatorNotesBytes.
const renderedNotesTruncated = "\n[truncated: this operator note exceeds 16 KiB; it stays pending, and the full note is on the activity]"

// cutRenderedOperatorNotes cuts s to maxRenderedOperatorNotesBytes on a rune boundary,
// marking the cut.
func cutRenderedOperatorNotes(s string) string {
	if len(s) <= maxRenderedOperatorNotesBytes {
		return s
	}
	cut := maxRenderedOperatorNotesBytes - len(renderedNotesTruncated)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + renderedNotesTruncated
}

// operatorNoteKindName is a note kind's wire word, for the rendered block.
func operatorNoteKindName(k projectstate.OperatorNoteKind) string {
	switch k {
	case projectstate.NoteSendBack:
		return "sendBack"
	case projectstate.NoteRetry:
		return "retry"
	case projectstate.NoteTakeover:
		return "takeover"
	case projectstate.NoteReassign:
		return "reassign"
	case projectstate.NoteSkip:
		return "skip"
	case projectstate.NoteRequeue:
		return "requeue"
	case projectstate.OperatorNoteKindUnknown:
		return "unknown"
	}
	return "unknown"
}

// runLocalMergeStep runs the policy-gated local merge (see the section comment
// above). Returns (mergeFailed, done, err) with walkPhases' loop-control
// semantics: done=true means the failure path terminally recorded the activity;
// mergeFailed=true means the supervision loop should retry (completedPhases +
// mergeCompleted make the retry re-attempt ONLY the merge). A no-op (returns
// all-false) outside the rail-dormant git profile, on a pre-feature in-flight
// execution (GetVersion replay guard), or when the merge already landed.
func (wf *workflows) runLocalMergeStep(
	ctx workflow.Context,
	in constructActivityInput,
	attempt int,
	policy projectstate.ReviewPolicy,
	state *constructState,
	headVersion *projectstate.Version,
	overrideCh workflow.ReceiveChannel,
	gitOn bool,
	startedCred railCredEnvelope,
) (bool, bool, error) {
	// Replay guard (same discipline as construction-review-policy-snapshot):
	// workflows in flight before this feature deployed have no history for the
	// merge commands — they skip the step entirely.
	if workflow.GetVersion(ctx, "local-merge-step", workflow.DefaultVersion, 1) < 1 {
		return false, false, nil
	}
	// The local merge fires ONLY in the rail-dormant git profile: gitOn without a
	// PR rail (startedCred's LOCAL/dry-run arm). When the rail is wired the cloud
	// git-forward lifecycle (mergeAndRecord) owns the merge; when git is unwired
	// there is no branch to merge.
	if !gitOn || wf.RailEnabled || state.mergeCompleted {
		return false, false, nil
	}

	// The gate, consulted at MethodPhaseConstruction through the SAME engine call the
	// phase gates use: this is what makes vibes auto-merge, checkpoints/full hold, and
	// the risk floor hold ALWAYS. Only the verdict is read — the merge has no roster to
	// show. A refusal reads as "no hold", matching runPhaseGate's conservative arm: the
	// merge is what the vibes profile does unattended today, and an engine defect must
	// not strand the branch. It is logged either way.
	mergeSet, mErr := wf.proposeReviewSet(in, projectstate.MethodPhaseConstruction, policy, state)
	if mErr != nil {
		workflow.GetLogger(ctx).Error("review engine refused to decide the merge gate; the merge proceeds unheld",
			"activityId", in.ActivityID, "err", mErr.Error())
	}
	if mErr == nil && mergeSet.RequiresHuman != nil && *mergeSet.RequiresHuman {
		state.redraftExhausted = false
		state.enterHumanStage(ctx, StageAwaitingApproval, mergeGateKey, 0)
		ch := workflow.GetSignalChannel(ctx, signalPhaseDecision)
		for {
			sig := receivePhaseDecision(ctx, ch, mergeGateKey)
			if sig.Decision == PhaseApprove {
				state.leaveHumanStage(ctx, in.Activity.activityTypeName(), gateOutcomeApproved)
				break
			}
			// SendBack has no redraft meaning for a merge: the façade refuses it
			// (validatePhaseDecision), so this arm is defense-in-depth for a
			// signal that bypassed it — keep awaiting Approve (the operator steers
			// the activity itself via operatorOverride).
			workflow.GetLogger(ctx).Info("merge gate: ignoring non-approve decision; awaiting Approve",
				"activityId", in.ActivityID, "decision", sig.Decision)
		}
	}

	state.stage = StagePipelineRunning
	obs, err := wf.runMergePipeline(ctx, in, state)
	if err != nil {
		return false, false, err
	}
	if obs.Phase == PipelineSucceeded {
		state.mergeCompleted = true
		return false, false, nil
	}

	// Merge failed (conflict, missing branch, push fault) — the SAME
	// intervention/failure path a failed phase pipeline takes.
	failReason := deriveFailureReason(obs.Phase, obs.Diagnostic)
	done, vErr := wf.handleVariance(ctx, in, intervention.WorkerMiss, obs.Diagnostic, failReason, attempt, headVersion, state, overrideCh, gitOn, startedCred)
	if vErr != nil {
		return false, false, vErr
	}
	if done {
		return false, true, nil
	}
	return true, false, nil
}

// runMergePipeline dispatches the merge job through the pipeline seam and polls
// to a terminal observation (the local arm performs the merge synchronously, so
// the first observe is normally already terminal; the bounded poll mirrors
// runPipeline's discipline). The spec deliberately carries NO "command"/"phase"
// inputs — the job key routes it inside the local arm; it never spawns claude.
func (wf *workflows) runMergePipeline(ctx workflow.Context, in constructActivityInput, state *constructState) (pipelineObservation, error) {
	handle, err := wf.Acts.PipelineSubmitAgenticJob(ctx, agenticjob.PipelineSpec{
		ProjectID:  agenticjob.ProjectID(string(in.ProjectID)),
		ActivityID: agenticjob.ConstructionActivityID(string(in.ActivityID)),
		Steps: []agenticjob.PipelineStep{{
			Name:      "build",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: map[string]string{
			agenticjob.DispatchInputJobKey: agenticjob.DispatchJobMerge,
			"activity_id":                  string(in.ActivityID),
		},
	})
	if err != nil {
		return pipelineObservation{}, err
	}
	h := pipelineHandle{Name: agenticjob.PipelineHandleString(handle)}
	for range maxPipelinePolls {
		obs, oerr := wf.observePipeline(ctx, h)
		if oerr != nil {
			return pipelineObservation{}, oerr
		}
		ph := obs.Phase
		state.pipelinePhase = &ph
		if obs.Phase == PipelineSucceeded || obs.Phase == PipelineFailed || obs.Phase == PipelineCancelled {
			// agentic=false: the merge job merges a branch, it never spawns an agent, so a
			// missing summary here is not a loss and must not be recorded as a gap. The
			// capture stays wired so a merge job that ever DOES mine one is not dropped.
			// NOTE the late-episode grace is deliberately NOT taken here — it lives inside
			// captureEpisode, gated on agentic, so a cancelled merge never spends 20s
			// waiting for a summary a merge can by construction never produce.
			//
			// Task attribution (Task 10): the merge job has no Figure A-1 task of its own —
			// App A's twelve tasks stop at Code Review, and landing the reviewed branch on
			// main is this platform's own automation, not a Method task. TaskConstruction is
			// the best-available attribution (merge only runs after Construction's gate has
			// passed, as the mechanical tail of landing that task's work), sharing its
			// attempt counter rather than inventing a task that Figure A-1 does not have.
			mergeTask := projectstate.TaskConstruction
			wf.captureEpisode(ctx, in, h, obs, false, mergeTask, state.nextTaskAttempt(mergeTask))
			return obs, nil
		}
		_ = workflow.Sleep(ctx, pipelinePollInterval)
	}
	return pipelineObservation{Phase: PipelineFailed, Diagnostic: "merge pipeline did not reach a terminal phase within the poll budget"}, nil
}

// recordPhaseStarted records the phase-started head-state transition (Task-5
// RecordPhaseStarted) through the §6.5 Conflict loop. Gated on gitOn by the caller.
//
// It is a RETIRED-facet verb that still writes the activity's row, and it runs beside the
// execution ledger rather than instead of it (its gate is gitOn, not the ledger fence), so
// its applied transition advances the row's stored counter and this run's copy of it must
// follow — see constructState.rowAdvanced.
func (wf *workflows) recordPhaseStarted(ctx workflow.Context, in constructActivityInput, phase projectstate.ActivityMethodPhase, state *constructState, seed projectstate.Version, cred railCredEnvelope) (projectstate.Version, error) {
	v, err := wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordPhaseStarted(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), phase, cred.toProjectState())
	})
	if err != nil {
		return 0, err
	}
	state.rowAdvanced()
	return v, nil
}

// proposeReviewSet is the Manager's SINGLE review decision point: it hands the engine
// the activity's TYPE, the lifecycle phase's wire name, the committed policy document
// and the floor flag, and gets back the whole answer — the roster, whether a human must
// sign off, and the one-line reason. The call is to the PURE published
// review.ReviewEngine, directly (deterministic, replay-safe).
//
// The (activity type, lifecycle phase) → review kind table MOVED into
// internal/engine/review when ProposeReviews took the activity type (spec 2026-09-20
// §5.4, stage 2), taking the "no component degrades to a sign-off" rule with it; the
// engine owns the whole table now, so the Manager cannot pass a kind the engine does
// not know.
//
// The contracts are sourced from the start-snapshot project (B5). The architectureGraph
// parameter is GONE: every production call passed "" and the v1 policy ignored it — a
// parameter that is always empty is a lie the compiler cannot catch (earmark: feed the
// committed SystemDesign through `contracts`' successor when the reviewer set becomes
// enforcing). reviewSetFromEngine (adapters.go) bridges the Engine's own ReviewSet onto
// this component's generated façade ReviewSet (contract.gen.go) — a real divergence,
// not an identity mirror.
func (wf *workflows) proposeReviewSet(in constructActivityInput, phase projectstate.ActivityMethodPhase, policy projectstate.ReviewPolicy, state *constructState) (ReviewSet, error) {
	change := review.ReviewChange{ActivityID: string(in.ActivityID), ComponentID: in.Activity.ComponentID}
	set, err := wf.Review.ProposeReviews(fweng.Context{Context: context.Background()},
		change, review.ActivityType(in.Activity.activityTypeName()), phase.String(),
		in.Activity.ComponentID, engineReviewPolicy(policy), state.floorTouched, state.reviewContracts)
	if err != nil {
		return ReviewSet{}, err
	}
	return reviewSetFromEngine(set), nil
}

// snapshotContractKeys derives the deterministic (sorted) set of contract identifiers
// from the start-snapshot project's committed service contracts — the display input
// for the gate's reviewer set. Sorted so the derived slice is replay-stable (map
// iteration order is randomized).
func snapshotContractKeys(p projectstate.Project) []string {
	if len(p.ServiceContracts) == 0 {
		return nil
	}
	keys := make([]string, 0, len(p.ServiceContracts))
	for k := range p.ServiceContracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// handleVariance is the DECIDE→EXECUTE machinery for an automatically-detected
// variance (constructionManager.md §6.3 step 7). It calls interventionEngine
// (DECIDE) and EXECUTES the directive: Retry → loop again (return done=false);
// Escalate → await an operator override and execute it; Takeover → re-dispatch
// (loop). Returns done=true when the activity has reached a terminal exit.
func (wf *workflows) handleVariance(
	ctx workflow.Context,
	in constructActivityInput,
	kind intervention.VarianceKind,
	detail string,
	failReason projectstate.FailureReason,
	attempt int,
	headVersion *projectstate.Version,
	state *constructState,
	overrideCh workflow.ReceiveChannel,
	gitOn bool,
	startedCred railCredEnvelope,
) (bool, error) {
	state.variance = &FlaggedVariance{ProjectID: in.ProjectID, ActivityID: in.ActivityID, Summary: detail}

	// NOTE: ProjectID is deliberately fed from in.ActivityID, not in.ProjectID — do
	// not "fix" this to in.ProjectID; the intervention Engine's ConstructionVariance
	// only ever carried this value from ActivityID.
	directive, derr := wf.Intervention.DecideOnVariance(fweng.Context{Context: context.Background()}, intervention.ConstructionVariance{
		ProjectID:    intervention.ProjectID(in.ActivityID),
		ActivityID:   intervention.ActivityID(in.ActivityID),
		Kind:         kind,
		AttemptCount: int64(attempt),
		Policy:       wf.InterventionPolicy,
	})
	if derr != nil {
		return false, fwmanager.MapError(derr)
	}

	switch directive {
	case intervention.VarianceRetry:
		state.stage = StageDispatching
		return false, nil // loop to re-dispatch
	case intervention.VarianceTakeover:
		// EXECUTE takeover: loop to re-dispatch under a changed arrangement. The
		// prior phase pipeline already reached a terminal state before intervention
		// was consulted, so there is no in-flight dispatch to abandon here.
		state.stage = StageDispatching
		return false, nil
	case intervention.VarianceEscalate:
		// EXECUTE escalate: surface to the operator + await an override signal, BOUNDED
		// by EscalationWaitTimeout. On timeout (no operator answered the escalation), the
		// activity terminally FAILS (head-state reflects EscalationTimedOut) instead of
		// hanging forever waiting for an override that never comes.
		state.redraftExhausted = false
		state.enterHumanStage(ctx, StageAwaitingTakeover, takeoverGateKey, wf.EscalationWaitTimeout)
		sig, got := wf.awaitOverrideBounded(ctx, overrideCh)
		if !got {
			state.leaveHumanStage(ctx, in.Activity.activityTypeName(), gateOutcomeTimedOut)
			_ = failReason // underlying cause is carried in detail below; the terminal reason is EscalationTimedOut
			timedOut := "escalation timed out: no operator override within the escalation-wait window (underlying: " + detail + ")"
			if state.executionLedger {
				if e := wf.recordExecutionOutcome(ctx, in, state, headVersion, startedCred,
					projectstate.ActivityOutcomeUnknown, projectstate.EscalationTimedOut, timedOut); e != nil {
					return false, e
				}
				state.stage = StageExited
				return true, nil
			}
			v, e := wf.recordActivityFailed(ctx, in, *headVersion, projectstate.EscalationTimedOut, timedOut, startedCred)
			if e != nil {
				return false, e
			}
			*headVersion = v
			state.stage = StageExited
			return true, nil
		}
		state.leaveHumanStage(ctx, in.Activity.activityTypeName(), gateOutcomeOverridePrefix+strings.ToLower(overrideKindName(sig.Override.Kind)))
		return wf.executeOverride(ctx, in, sig.Override, headVersion, state, gitOn, startedCred)
	default:
		// intervention.VarianceDirective has no Unknown sentinel (VarianceRetry is its
		// zero value) — any value outside {VarianceRetry, VarianceTakeover,
		// VarianceEscalate} is an unrecognized engine decision, rejected as a
		// non-retryable error.
		return false, temporal.NewNonRetryableApplicationError(
			"intervention returned an unknown directive", "UnknownDirective", nil)
	}
}

// executeOverride runs the operator's manual steer through the same execute
// machinery as the automatic variance path (constructionManager.md §2.4 / §6.3
// override branch). Returns done=true when the override terminally exits the
// activity (Skip), false when it loops back into supervision (Retry/Takeover/Reassign).
func (wf *workflows) executeOverride(
	ctx workflow.Context,
	in constructActivityInput,
	override ActivityOverride,
	headVersion *projectstate.Version,
	state *constructState,
	gitOn bool,
	startedCred railCredEnvelope,
) (bool, error) {
	// The override's notes are the operator's steer (B1.4): kept on the activity, and —
	// for a retry, takeover or reassign — carried by the next agent dispatch. A skip's
	// note is kept and never pending: nothing runs after a skip.
	if kind, ok := overrideNoteKind(override.Kind); ok {
		if err := wf.recordOperatorNote(ctx, in, state, headVersion, startedCred, kind, takeoverGateKey,
			noteFeedback{text: override.Notes, comments: override.Comments}); err != nil {
			return false, err
		}
	}
	switch override.Kind {
	case OverrideUnknown:
		// zero-value sentinel, not a real override kind — same as any unmapped value.
		return false, temporal.NewNonRetryableApplicationError(
			"unknown operator override kind", "UnknownOverride", nil)
	case OverrideRetry, OverrideReassign:
		// Re-enter the dispatch path (Reassign re-casts via handOffEngine on the
		// next loop iteration — the committed constructionManager → handOffEngine
		// edge, OQ-3).
		state.stage = StageDispatching
		return false, nil
	case OverrideTakeover:
		// Loop to re-dispatch; the prior phase pipeline is already terminal (see the
		// directiveTakeover note), so there is no in-flight dispatch to abandon.
		state.stage = StageDispatching
		return false, nil
	case OverrideSkip:
		if state.executionLedger {
			if err := wf.recordExecutionOutcome(ctx, in, state, headVersion, startedCred,
				projectstate.ActivityOutcomeSkipped, projectstate.FailureReasonUnknown, ""); err != nil {
				return false, err
			}
			state.stage = StageExited
			return true, nil
		}
		v, e := wf.recordActivityExited(ctx, in, *headVersion, projectstate.ActivityOutcomeSkipped, startedCred)
		if e != nil {
			return false, e
		}
		*headVersion = v
		// Record the per-activity construction COMPLETED on a Skip terminal too
		// (Task 3): a skipped activity is Done from the pump's eligibility POV so its
		// dependents unblock. Dormant when the git slice is unwired.
		if gitOn {
			if err := wf.recordActivityCompleted(ctx, in, startedCred, headVersion); err != nil {
				return false, err
			}
		}
		state.stage = StageExited
		return true, nil
	default:
		return false, temporal.NewNonRetryableApplicationError(
			"unknown operator override kind", "UnknownOverride", nil)
	}
}

// deriveFailureReason maps a terminal pipeline phase + neutral diagnostic to the
// head-state FailureReason: a cancelled run → PipelineCancelled; a timed-out
// diagnostic (the RA's neutralDiagnostic for timed_out / the poll-budget exhaustion
// synthetic) → PipelineTimedOut; otherwise PipelineFailed.
func deriveFailureReason(phase PipelinePhase, diagnostic string) projectstate.FailureReason {
	if phase == PipelineCancelled {
		return projectstate.PipelineCancelled
	}
	if strings.Contains(diagnostic, "timed out") || strings.Contains(diagnostic, "did not reach a terminal phase") {
		return projectstate.PipelineTimedOut
	}
	return projectstate.PipelineFailed
}

// awaitOverrideBounded waits for an operator override on overrideCh, BOUNDED by the
// configured EscalationWaitTimeout. It returns (sig, true) when an override arrived,
// or (zero, false) when the bounded wait elapsed first. A timeout of 0 means
// wait-forever (the supervised EscalateEverything mode) — it blocks on the receive
// with no timer, preserving the legacy behaviour. The timer is a durable
// workflow.NewTimer (replay-safe), raced via a workflow.NewSelector.
func (wf *workflows) awaitOverrideBounded(ctx workflow.Context, overrideCh workflow.ReceiveChannel) (operatorOverrideSignal, bool) {
	var sig operatorOverrideSignal
	if wf.EscalationWaitTimeout <= 0 {
		// Supervised / wait-forever: block on the override receive (legacy behaviour).
		overrideCh.Receive(ctx, &sig)
		return sig, true
	}
	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	defer cancelTimer()
	timer := workflow.NewTimer(timerCtx, wf.EscalationWaitTimeout)
	got := false
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(overrideCh, func(ch workflow.ReceiveChannel, _ bool) {
		ch.Receive(ctx, &sig)
		got = true
	})
	sel.AddFuture(timer, func(workflow.Future) {
		got = false
	})
	sel.Select(ctx)
	return sig, got
}

// recordChangeReviewed applies the head-state transition with the Conflict loop. The
// Manager-minted cred is threaded into the write (empty/zero in dev/dry-run).
//
// Another retired-facet verb that writes the row unfenced (see recordPhaseStarted): it
// lands between the gate's last round write and the outcome the ledger records, so the
// run's copy of the row version follows its stamp.
func (wf *workflows) recordChangeReviewed(ctx workflow.Context, in constructActivityInput, state *constructState, seed projectstate.Version, cred railCredEnvelope) (projectstate.Version, error) {
	v, err := wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordChangeReviewed(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), cred.toProjectState())
	})
	if err != nil {
		return 0, err
	}
	state.rowAdvanced()
	return v, nil
}

// recordActivityExited applies the binary-exit head-state transition.
func (wf *workflows) recordActivityExited(ctx workflow.Context, in constructActivityInput, seed projectstate.Version, outcome projectstate.ActivityOutcome, cred railCredEnvelope) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordActivityExited(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), outcome, cred.toProjectState())
	})
}

// recordActivityFailed applies the terminal-FAILURE head-state transition (the
// bounded-wait / autonomous-retry fix) with the same head-version Conflict re-read
// loop as recordActivityExited. It lands Phase=Failed / BuildStatus=BuildFailed and
// records the reason+detail so head-state reflects the terminal instead of leaving
// the activity stuck Running.
func (wf *workflows) recordActivityFailed(ctx workflow.Context, in constructActivityInput, seed projectstate.Version, reason projectstate.FailureReason, detail string, cred railCredEnvelope) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordActivityFailed(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), reason, detail, cred.toProjectState())
	})
}

// operatorOverrideSignal is the operatorOverride payload (constructionManager.md
// §2.4). Delivered to the per-activity child {projectId}:{activityId}.
type operatorOverrideSignal struct {
	Override ActivityOverride
}

// phaseDecisionSignal is the phaseDecision payload (constructionManager.md §2.6).
// Delivered to the per-activity child {projectId}:{activityId}; Phase identifies
// which review gate the decision closes (e.g. "detailed_design").
type phaseDecisionSignal struct {
	Phase    string
	Decision PhaseDecision
	Feedback *ReviewFeedback
}

// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
// readVersionE runs the cheap ReadProjectVersion GENERATED invoker (B8: migrated off the
// custom ReadProjectVersionActivity) and returns ONLY the head-state optimistic-
// concurrency token, surfacing errors (including the brand-new project's fwra.NotFound)
// to the caller. Replaces the wasteful whole-aggregate read that shipped the entire
// encoded Project across the Temporal Activity boundary for a uint64 (architect's
// fast-follow). The invoker's Opts hook applies readProjectActivityOptions (identical
// preset, keyed "projectStateAccess.readProjectVersion" — workermanifest.go).
func (wf *workflows) readVersionE(ctx workflow.Context, projectID ProjectID) (projectstate.Version, error) {
	return wf.Acts.ProjectStateReadProjectVersion(ctx, projectstate.ProjectID(projectID))
}

// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
// readVersion reads the current head Version (0 for a brand-new project or on any
// read error — the read-your-writes seed treats absence as version 0).
func (wf *workflows) readVersion(ctx workflow.Context, projectID ProjectID) projectstate.Version {
	v, err := wf.readVersionE(ctx, projectID)
	if err != nil {
		return 0
	}
	return v
}

// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to systemdesign).
func (wf *workflows) applyRecovering(
	ctx workflow.Context,
	projectID ProjectID,
	seed projectstate.Version,
	apply func(expected projectstate.Version) (projectstate.Version, error),
) (projectstate.Version, error) {
	expected := seed
	for attempt := 0; ; attempt++ {
		v, err := apply(expected)
		if err == nil {
			return v, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		if attempt+1 >= maxMutateConflictAttempts {
			return 0, temporal.NewNonRetryableApplicationError(
				"head-state conflict did not converge within bounded attempts",
				"MutateConflictExhausted", err)
		}
		v, rerr := wf.readVersionE(ctx, projectID)
		if rerr != nil {
			if isReadNotFound(rerr) {
				expected = 0
				continue
			}
			return 0, rerr
		}
		expected = v
		workflow.GetLogger(ctx).Info("head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "nextExpectedVersion", expected)
	}
}
