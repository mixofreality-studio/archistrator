package delivery

// answerjob.go is the ANSWER WORKFLOW (AnswerJobWorkflow): the durable execution behind one
// AskQuestions round (startAnswerJob, deliverymanager.go). It dispatches the agentic answer
// job, observes it to a terminal phase, lands a committed artifact's replies on main's
// question thread, and records the episode. It replaced a goroutine in the server process
// that a restart lost, together with the landing it ran (dct5 round-4 ruling).

import (
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// AnswerJobWorkflow is the answer workflow (startAnswerJob): converge the seated scaffold on a
// GitHub venue, dispatch the answer job, observe it to a terminal phase, land a committed
// artifact's replies on main, and record the episode. Every outcome that leaves the question
// unanswered — the venue unresolved, the scaffold sync or the dispatch refused, the job
// failing — is LOGGED and ends the workflow cleanly: the question stays recorded and open,
// which is the use case's remain-open branch, and re-asking starts the workflow again.
func (wf *csWorkflows) AnswerJobWorkflow(ctx workflow.Context, in answerJobInput) error {
	log := workflow.GetLogger(ctx)
	if wf.Repo == nil {
		log.Warn("answer job NOT dispatched: no repo resolver (rail dormant)", "projectId", string(in.ProjectID))
		return nil
	}
	repoRef, ok := wf.Repo(in.ProjectID)
	if !ok {
		log.Error("answer job NOT dispatched: could not resolve the project repo; re-run AskQuestions to retry", "projectId", string(in.ProjectID))
		return nil
	}
	// MANAGED-SCAFFOLD SYNC (sync-on-dispatch): on a GitHub venue an answer job runs the same
	// seated aiarch-design.yml as a draft, so it must never run against a stale scaffold. The
	// local venue seats no workflow, so there is nothing to converge (as for a design draft).
	if wf.RailEnabled(in.ProjectID) {
		cred, err := wf.mintCred(ctx, repoRef)
		if err != nil {
			log.Error("answer job NOT dispatched: could not mint the repo credential for the managed-scaffold sync; re-run AskQuestions to retry", "error", err.Error())
			return nil
		}
		if err := wf.railWithAuthRetry(ctx, func() error {
			_, e := wf.Acts.RailSyncManagedScaffold(ctx, repoRef, cred.toRail())
			return e
		}); err != nil {
			log.Error("answer job NOT dispatched: managed-scaffold sync failed — the seated design workflow could not be proven current; re-run AskQuestions to retry", "error", err.Error())
			return nil
		}
	}
	target, err := designRepoTarget(sourcecontrol.RepoRefString(repoRef))
	if err != nil {
		log.Error("answer job NOT dispatched: could not resolve the target repo; re-run AskQuestions to retry", "error", err.Error())
		return nil
	}
	h, err := wf.answerActs().PipelineSubmitAgenticJob(ctx, agenticjob.PipelineSpec{
		ProjectID: agenticjob.ProjectID(in.ProjectID),
		// A non-empty, well-formed step graph satisfies the RA's pre-condition; the answer
		// recipe lives in the project's own workflow file, so the step is a placeholder and
		// the parameters ride on DispatchInputs.
		Steps: []agenticjob.PipelineStep{{
			Name:      "design",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: map[string]string{
			dispatchInputArtifactKind:  in.Kind.WireName(),
			dispatchInputCommand:       in.Command,
			dispatchInputTargetBranch:  in.Branch,
			dispatchInputPriorStateRef: "",
			dispatchInputJobMode:       jobModeAnswer,
		},
		TargetRepo:   target,
		WorkflowFile: designWorkflowFileName,
	})
	if err != nil {
		log.Error("answer job dispatch FAILED — the question is recorded but not auto-answered; re-run AskQuestions with the same question to retry", "error", err.Error())
		return nil
	}
	if agenticjob.PipelineHandleIsZero(h) {
		log.Error("answer job dispatch returned an empty pipeline handle; the question stays open")
		return nil
	}
	handle := pipelineHandle{Name: agenticjob.PipelineHandleString(h)}
	obs := wf.observeAnswerJob(ctx, handle)
	// BUSINESS FIRST, EPISODE SECOND (captureEpisode's discipline).
	if obs.Phase == PipelineSucceeded && in.Landing != nil {
		wf.landAnswers(ctx, in)
	}
	wf.captureAnswerEpisode(ctx, in, handle, obs)
	return nil
}

// answerActs is the package's invokers with the answer job's own dispatch preset
// (answerSubmitActivityOptions); every other activity keeps the manifest's preset.
func (wf *csWorkflows) answerActs() genInvokers {
	acts := wf.Acts
	base := acts.Opts
	acts.Opts = func(name string) (workflow.ActivityOptions, bool) {
		if name == answerSubmitActivity {
			return answerSubmitActivityOptions(), true
		}
		if base == nil {
			return workflow.ActivityOptions{}, false
		}
		return base(name)
	}
	return acts
}

// observeAnswerJob polls the answer job to a terminal phase on the observe ladder. A failed
// observe is not the job's verdict (observeDispatchedJob's rule); a job that never ends comes
// back as an explicit PipelineFailed, never as success.
func (wf *csWorkflows) observeAnswerJob(ctx workflow.Context, handle pipelineHandle) csPipelineObservation {
	var last csPipelineObservation
	for poll := range maxObserveTotalPolls {
		obs, err := wf.observePipeline(ctx, handle)
		if err != nil {
			workflow.GetLogger(ctx).Warn("observing the answer job failed; the ladder keeps polling", "error", err.Error())
		} else {
			if obs.Phase == PipelineSucceeded || obs.Phase == PipelineFailed || obs.Phase == PipelineCancelled {
				return obs
			}
			last = obs
		}
		_ = workflow.Sleep(ctx, observeInterval(poll))
	}
	return csPipelineObservation{
		Phase:      PipelineFailed,
		Diagnostic: "the answer job did not reach a terminal phase within the observation window",
		RunURL:     last.RunURL,
		Episode:    last.Episode,
	}
}

// landAnswers records, on main, the replies the answer job posted on its answer branch — the
// use case's record-answers step for a committed artifact: each per-question response becomes
// a reply on its question in the question-only thread on main, and the question then reads
// answered (derived from the reply by the store's normaliser, never set by a status verb). The
// store skips a reply it already holds, so a re-run lands nothing twice. A failure is logged
// and the questions stay recorded, answered on the branch.
func (wf *csWorkflows) landAnswers(ctx workflow.Context, in answerJobInput) {
	log := workflow.GetLogger(ctx)
	l := in.Landing
	onBranch, err := wf.readProjectOnBranch(ctx, in.ProjectID, l.Branch)
	if err != nil {
		log.Error("answers NOT landed on main: the answer branch could not be read", "branch", l.Branch, "error", err.Error())
		return
	}
	var lastErr error
	for range askQuestionsMaxAttempts {
		main, rerr := wf.readProject(ctx, in.ProjectID)
		if rerr != nil {
			log.Error("answers NOT landed on main: the project could not be read", "error", rerr.Error())
			return
		}
		replies := unlandedReplies(pdSlotFor(onBranch, in.Kind).ReviewThread, pdSlotFor(main, in.Kind).ReviewThread, l.Comments)
		if len(replies) == 0 {
			return
		}
		_, err = wf.Acts.DesignSessionSeedReviewCommentsOnBranch(ctx, projectstate.ProjectID(in.ProjectID), main.Version, "",
			in.Kind, l.Round, nil, replies)
		if err == nil {
			log.Info("answers landed on main", "replies", len(replies))
			return
		}
		if !isConflict(err) {
			log.Error("answers NOT landed on main", "error", err.Error())
			return
		}
		lastErr = err
	}
	log.Error("answers NOT landed on main: the project kept changing", "error", lastErr.Error())
}

// captureAnswerEpisode appends the ONE ledger record the answer job owes: the mined summary,
// or an explicit GAP. A remote venue mines no episode in v1, so nothing is recorded there. The
// append never fails the workflow (captureEpisode's discipline).
func (wf *csWorkflows) captureAnswerEpisode(ctx workflow.Context, in answerJobInput, handle pipelineHandle, obs csPipelineObservation) {
	if episodeVenueIsRemote(obs.RunURL) {
		return
	}
	obs = wf.awaitLateEpisode(ctx, handle, obs)
	exec := workflow.GetInfo(ctx).WorkflowExecution
	lineage := &episode.EpisodeLineage{WorkflowID: exec.ID, RunID: exec.RunID}
	targetRef := in.Kind.String()
	var rec episode.EpisodeRecord
	if obs.Episode != nil {
		rec = episodeRecordFromSummary(*obs.Episode, episode.EpisodeKindAnswer, targetRef, lineage, obs.Diagnostic)
	} else {
		rec = episodeGapRecord(episode.EpisodeKindAnswer, targetRef, lineage, "gap-"+episodeIDSafe(handle.Name),
			episodeGapReason(episodeMissingSummaryReason, obs.Diagnostic), workflow.Now(ctx))
	}
	if err := wf.Acts.EpisodesAppendEpisode(ctx, episode.ProjectID(in.ProjectID), rec); err != nil {
		workflow.GetLogger(ctx).Error("answer-job episode append failed after its full retry envelope; this episode is NOT in the ledger",
			"episodeId", rec.EpisodeID, "error", err.Error())
	}
}
