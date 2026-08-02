package construction

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
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
// from the activity's derived type/variant and the current phase so the workflow itself
// holds no routing logic. component_id is a Manager-resolved passthrough. (Moved workflow-
// side from the retired pipelineAdapter — it only reads workflow state + projectstate.CommandFor.)
func dispatchInputsFor(spec pipelineSpec) map[string]string {
	m := map[string]string{
		"activity_id":  spec.ActivityID,
		"component_id": spec.ComponentID,
	}
	if spec.Phase != "" {
		m["phase"] = spec.Phase
		typ := projectstate.DeriveType(spec.ActivityID)
		variant := projectstate.DeriveVariant(spec.ActivityID)
		m["command"] = projectstate.CommandFor(typ, variant, projectstate.ActivityMethodPhase(spec.Phase))
	}
	return m
}

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
func (wf *workflows) captureEpisode(ctx workflow.Context, in constructActivityInput, handle pipelineHandle, obs pipelineObservation, agentic bool) {
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
	rec := episodeRecordFor(ctx, obs, episodeIDSeed(handle, in), string(in.ActivityID))
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
func episodeRecordFor(ctx workflow.Context, obs pipelineObservation, idSeed, activityID string) episode.EpisodeRecord {
	exec := workflow.GetInfo(ctx).WorkflowExecution
	lineage := &episode.EpisodeLineage{
		WorkflowID: exec.ID,
		RunID:      exec.RunID,
		// The METHOD activity this episode was burned on (there is no way to read the
		// Temporal activity id from inside a workflow, and the Method id is the one that
		// makes the lineage joinable to the project network).
		ActivityID: &activityID,
	}
	// Construction dispatches are always EpisodeKindConstruction: the phase profile
	// (requirements/detailed_design/test_plan/construction/integration) draws no
	// review-vs-rework distinction, so there is nothing here to map onto the other kinds.
	const kind = episode.EpisodeKindConstruction
	if obs.Episode == nil {
		return episodeGapRecord(kind, activityID, lineage, "gap-"+episodeIDSafe(idSeed),
			episodeGapReason(episodeMissingSummaryReason, obs.Diagnostic), workflow.Now(ctx))
	}
	return episodeRecordFromSummary(*obs.Episode, kind, activityID, lineage, obs.Diagnostic)
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
// (nextEligibleActivity over proj.ActivityConstruction) does not re-dispatch it on a
// concurrent/redundant tick. Cred-threaded like the four git head-state records; a
// dormant slice (git unwired) is a no-op (the live Postgres composition has no
// per-activity construction head-state, so the gate degrades to the child-workflow-id
// idempotency the pump already relies on). It mints a credential ONCE for the
// started+completed pair via the supplied gitForward.cred when the branch lifecycle
// has already minted one, else mints its own.
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
		return wf.Acts.GitStatusRecordActivityStarted(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID), cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// recordActivityCompleted marks the activity Done in the per-activity construction
// head-state at the END of the spine (Task 3), alongside RecordActivityExited. This
// is what unblocks dependents in the pump's eligibility selection (allDepsDone). A
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
	return ReviewSet{Reviewers: reviewers}
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
	if gitOn {
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

	// --- Step 1: dispatch (the former per-activity worker-class cast is retired). The
	// handOffEngine is gone: agent-class selection collapsed to the platform's single
	// "agent" dispatch default, and automated-vs-human routing is now the project's
	// review-policy preset, applied per phase by the runPhaseGate gate below (via
	// reviewPolicy.EffectiveGate) rather than an up-front worker-class decision. Every
	// activity dispatches; a human is inserted where the review policy requires one.

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
	if len(in.Activity.Phases) == 0 {
		in.Activity.Phases = projectstate.ProfileFor(projectstate.ActivityTypeService, 0).PhaseIDs()
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
	// consult the SAME EffectiveGate the construction dispatch uses (vibes → auto,
	// checkpoints/full → hold for approval, risk floor → always hold) and dispatch
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
	if acs, ok := snap.ActivityConstruction[string(in.ActivityID)]; ok {
		for _, pc := range acs.Phases {
			if pc.Completed {
				state.completedPhases[pc.Phase] = true
			}
		}
	}
	state.reviewContracts = snapshotContractKeys(snap)
	// Task 7 non-overridable floor: snapshot ONCE whether the activity's committed
	// contract touches deploy/spend/schema — never re-evaluated mid-loop, mirroring
	// reviewPolicy itself. A missing contract (nil map lookup) reads as the zero
	// ServiceContract, which never touches the floor.
	state.floorTouched = projectstate.ContractTouchesReviewFloor(snap.ServiceContracts[in.Activity.ComponentID])
	return reviewPolicy, nil
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
	v, e := wf.recordActivityFailed(ctx, in, *headVersion, projectstate.VarianceExhausted,
		"construction supervision exceeded max attempts", startedCred)
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
	v, e := wf.recordChangeReviewed(ctx, in, *headVersion, startedCred)
	if e != nil {
		return e
	}
	*headVersion = v

	// --- Step 6a: perform the gated merge and record it (git-forward). ---
	if err := wf.mergeAndRecord(ctx, in, gf, headVersion); err != nil {
		return err
	}

	// --- Step 8: record the binary activity exit (head-state). ---
	v2, e2 := wf.recordActivityExited(ctx, in, *headVersion, projectstate.ActivityOutcomeCompleted, startedCred)
	if e2 != nil {
		return e2
	}
	*headVersion = v2

	// --- Step 8a: record the per-activity construction COMPLETED (Task 3). Flip the
	// activity to Done so the pump's eligibility selection unblocks its dependents on the
	// next tick. Dormant (no-op) when the git slice is unwired. ---
	if gitOn {
		if err := wf.recordActivityCompleted(ctx, in, startedCred, headVersion); err != nil {
			return err
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
	handle, err := wf.submitPipeline(ctx, pipelineSpec{
		ProjectID:   in.ProjectID,
		ActivityID:  string(in.ActivityID),
		ComponentID: in.Activity.ComponentID,
		Phase:       phase.String(),
	})
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
			// Episode capture LAST, after this poll's business handling (§capture-seam).
			wf.captureEpisode(ctx, in, handle, obs, true)
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
	wf.captureEpisode(ctx, in, handle, exhausted, true)
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
		v, e := wf.recordPhaseStarted(ctx, in, phase, *headVersion, cred)
		if e != nil {
			return false, e
		}
		*headVersion = v
	}

	// Inert policy (or a phase this policy does not gate) → complete immediately (no
	// suspend). completePhase marks the in-memory set UNCONDITIONALLY. EffectiveGate
	// (Task 7) resolves the ReviewPolicy.Preset switch (vibes/checkpoints/full,
	// falling back to RequiresHuman's explicit map when Preset is unset) and THEN
	// applies the non-overridable floor via state.floorTouched — a
	// deploy/spend/schema-touching contract's construction dispatch stays gated
	// under every preset, including "vibes".
	if !policy.EffectiveGate(in.Activity.activityTypeName(), phase, state.floorTouched) {
		return false, wf.completePhase(ctx, in, phase, state, headVersion, gitOn, cred)
	}

	// Surface the reviewer set on the session view (display-only in v1; on engine error
	// leave it unset and still gate — the human Approve/SendBack is the enforced gate).
	if rs, e := wf.proposeReviewSet(in, phase, state); e == nil {
		state.reviewSet = &rs // NOTE: *ReviewSet (B6)
	}

	return wf.awaitPhaseDecision(ctx, in, phase, state, gf, headVersion, gitOn, cred)
}

// awaitPhaseDecision is the suspend + redraft loop of the gate (extracted so
// runPhaseGate stays under the gocognit budget). It drains the phase-multiplexed
// decision channel until a decision for THIS phase arrives, then acts on it: Approve
// completes the phase; SendBack redrafts THIS phase in place (its OWN redraft budget,
// NOT the variance budget); on redraft exhaustion it keeps awaiting the human.
func (wf *workflows) awaitPhaseDecision(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	state *constructState,
	gf *gitForward,
	headVersion *projectstate.Version,
	gitOn bool,
	cred railCredEnvelope,
) (bool, error) {
	ch := workflow.GetSignalChannel(ctx, signalPhaseDecision)
	redraft := 0
	for {
		state.stage = StageAwaitingApproval
		sig := receivePhaseDecision(ctx, ch, phase.String())
		switch sig.Decision {
		case PhaseDecisionUnknown:
			// zero-value sentinel, not a real decision — ignore and keep awaiting, same as default.
		case PhaseApprove:
			return false, wf.completePhase(ctx, in, phase, state, headVersion, gitOn, cred)
		case PhaseSendBack:
			redraft++
			if redraft >= maxPhaseRedrafts {
				// Exhausted the human-paced redraft budget. Do NOT fail the activity and do
				// NOT re-enter the variance loop — keep awaiting the human, surfacing that
				// redrafting is spent (mirrors systemdesign's anti-wedge staging).
				state.redraftExhausted = true
				workflow.GetLogger(ctx).Warn("phase redraft budget exhausted; keep awaiting human decision",
					"activityId", in.ActivityID, "phase", phase.String(), "exhausted", state.redraftExhausted)
				continue
			}
			state.stage = StagePipelineRunning
			if _, e := wf.runPipeline(ctx, in, phase, state, gf, headVersion); e != nil {
				return false, e
			}
		default:
			// Unknown decision: ignore and keep awaiting the human.
		}
	}
}

// receivePhaseDecision blocks on the decision channel, draining and DISCARDING
// decisions for other gate keys (stale/multiplexed), until one for THIS key
// arrives. key is a phase's wire name (phase.String()) or the merge gate's
// mergeGateKey — the signal payload's Phase field is a plain string pass-through
// (SubmitPhaseDecision), so the merge gate rides the same machinery.
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
// completion to head-state via the Task-5 RecordPhaseCompleted (artifactRef="") ONLY
// when gitOn.
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
// Task-7 machinery verbatim — ReviewPolicy.EffectiveGate at
// MethodPhaseConstruction with the non-overridable risk floor — so:
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
// ActivityMethodPhase — SubmitPhaseDecision passes the key through as a plain
// string, so the operator releases the merge with
// SubmitPhaseDecision(projectID, activityID, "merge", Approve).
const mergeGateKey = "merge"

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

	// The Task-7 gate, consulted at MethodPhaseConstruction: this is what makes
	// vibes auto-merge, checkpoints/full hold, and the risk floor hold ALWAYS.
	if policy.EffectiveGate(in.Activity.activityTypeName(), projectstate.MethodPhaseConstruction, state.floorTouched) {
		state.stage = StageAwaitingApproval
		ch := workflow.GetSignalChannel(ctx, signalPhaseDecision)
		for {
			sig := receivePhaseDecision(ctx, ch, mergeGateKey)
			if sig.Decision == PhaseApprove {
				break
			}
			// SendBack has no redraft meaning for a merge — keep awaiting Approve
			// (the operator steers the activity itself via operatorOverride).
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
			wf.captureEpisode(ctx, in, h, obs, false)
			return obs, nil
		}
		_ = workflow.Sleep(ctx, pipelinePollInterval)
	}
	return pipelineObservation{Phase: PipelineFailed, Diagnostic: "merge pipeline did not reach a terminal phase within the poll budget"}, nil
}

// recordPhaseStarted records the phase-started head-state transition (Task-5
// RecordPhaseStarted) through the §6.5 Conflict loop. Gated on gitOn by the caller.
func (wf *workflows) recordPhaseStarted(ctx workflow.Context, in constructActivityInput, phase projectstate.ActivityMethodPhase, seed projectstate.Version, cred railCredEnvelope) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordPhaseStarted(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), phase, cred.toProjectState())
	})
}

// proposeReviewSet builds the review.ReviewChange + artifactKind for this
// activity/phase and calls the PURE published review.ReviewEngine directly
// (deterministic, replay-safe). The contracts are sourced from the start-snapshot
// project (B5); the architecture graph is still passed empty — historically it was
// not carried across the pump's local envelope, and although the shared
// projectstate.ProjectEnvelope (B8 follow-up) now carries every populated slot, the
// reviewer set is display-only in v1, so the empty-graph behavior is deliberately
// preserved (earmark: feed the committed SystemDesign here when the reviewer set
// becomes enforcing).
// reviewSetFromEngine (adapters.go) bridges the Engine's own ReviewSet onto this
// component's generated façade ReviewSet (contract.gen.go) — a real divergence, not
// an identity mirror (see deps.go's reviewEngine retirement note).
func (wf *workflows) proposeReviewSet(in constructActivityInput, phase projectstate.ActivityMethodPhase, state *constructState) (ReviewSet, error) {
	change := review.ReviewChange{ActivityID: string(in.ActivityID), ComponentID: in.Activity.ComponentID}
	set, err := wf.Review.ProposeReviews(fweng.Context{Context: context.Background()},
		change, in.Activity.ComponentID, phase.String(), "", state.reviewContracts)
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
		state.stage = StageAwaitingTakeover
		sig, got := wf.awaitOverrideBounded(ctx, overrideCh)
		if !got {
			_ = failReason // underlying cause is carried in detail below; the terminal reason is EscalationTimedOut
			v, e := wf.recordActivityFailed(ctx, in, *headVersion, projectstate.EscalationTimedOut,
				"escalation timed out: no operator override within the escalation-wait window (underlying: "+detail+")", startedCred)
			if e != nil {
				return false, e
			}
			*headVersion = v
			state.stage = StageExited
			return true, nil
		}
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
func (wf *workflows) recordChangeReviewed(ctx workflow.Context, in constructActivityInput, seed projectstate.Version, cred railCredEnvelope) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordChangeReviewed(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), cred.toProjectState())
	})
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
