package projectdesign

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
	"sync/atomic"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// askquestions.go implements the question-comments op for Project Design (twin of the
// systemdesign implementation; founder-ratified 2026-07-05): AskQuestions appends clarifying
// QUESTIONS to a Phase-2 artifact's review ledger WITHOUT a redraft and dispatches a
// lightweight ANSWER job so the addressed role answers each in place. Open questions do NOT
// block approve; asking works on a committed artifact (main) and on a live session (branch).

const askQuestionsMaxAttempts = 5

// Dispatch inputs for the answer job. Project Design has no PM-critique, so its dispatch
// path never carried a job_mode; the answer job introduces one (defaulting elsewhere to
// "draft"), so these two keys are defined here alongside the op that needs them.
const (
	dispatchInputJobMode = "job_mode"
	jobModeAnswer        = "answer"
)

// AskQuestions — the Project-Design question-comments op. See the systemdesign twin for the
// full contract; the only differences are the Phase-2 kind gate and the Phase-2 slotFor.
//
// DISPATCH RECOVERY (F82): the answer job is BEST-EFFORT — the questions are seeded durably
// first, then a lightweight answer job is dispatched. A dispatch MISS (pipeline/repo not
// configured, repo unresolved, or a workflow_dispatch fault) is now LOGGED LOUDLY server-side
// (it was previously discarded, and the construction-pipeline RA has no logger, so a miss
// vanished — an open question that would never be answered with zero operator signal). To
// RECOVER a dropped dispatch, simply CALL AskQuestions AGAIN with the same questions: the seed
// is idempotent on its content key, so NO ledger entry is duplicated (the existing entries'
// round is reused so the minted ids still match), while the answer-job dispatch RE-FIRES via a
// per-call-unique key.
func (m *projectDesignManager) AskQuestions(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, addressee string, questions []AnchoredComment) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
	}
	switch addressee {
	case projectstate.ReviewAddresseePM, projectstate.ReviewAddresseeArchitect:
		// ok
	default:
		return newError(fwmanager.ContractMisuse, "addressee must be \"pm\" or \"architect\"")
	}
	qs := questionsToLedger(addressee, questions)
	if len(qs) == 0 {
		return newError(fwmanager.ContractMisuse, "no questions to ask (every question needs text)")
	}

	led, ok := m.projectState.(projectstate.LedgerProjectStateAccess)
	if !ok {
		return newError(fwmanager.FailedPrecondition, "review ledger not supported by this substrate")
	}

	branch := m.resolveQuestionBranch(rc, projectID, kind)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)
	key := askQuestionsIdempotencyKey(projectID, kind, branch, qs)

	var lastErr error
	for attempt := 0; attempt < askQuestionsMaxAttempts; attempt++ {
		proj, err := m.readProjectMaybeBranch(ctx, psID, branch)
		if err != nil {
			return mapReadProjectError(err)
		}
		thread := slotFor(proj, psKind).ReviewThread
		round := nextQuestionRound(thread)
		if r, ok := existingQuestionRound(thread, qs); ok {
			// A prior ask already seeded these exact questions (its answer-job dispatch may
			// have been dropped — F82). Reuse their round so the minted ids match the EXISTING
			// ledger entries, and the re-fired answer job answers the right comments.
			round = r
		}
		_, err = led.SeedReviewCommentsOnBranch(ctx, psID, proj.Version, branch, psKind, round, qs, key)
		if err == nil {
			minted := make([]projectstate.ReviewComment, len(qs))
			for i := range qs {
				minted[i] = qs[i]
				minted[i].ID = projectstate.ReviewCommentID(round, i)
			}
			m.dispatchAnswerJob(ctx, projectID, kind, branch, addressee, minted)
			return nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue
		}
		return mapReadProjectError(err)
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AskQuestions: exhausted conflict retries")
}

// resolveQuestionBranch — twin of the systemdesign impl (see there for the full F73 rationale).
// A GENUINELY ACTIVE session (co-author workflow OPEN and in a non-terminal stage) keeps its
// ledger on the session branch; every closed/completed/withdrawn/failed/absent run falls back
// to main (""). Resolution reuses the P0-2 Describe-first machinery via GetSessionState rather
// than a bare sessionState Query, which would REPLAY a dead run's stale live stage and wrongly
// resolve an abandoned amendment's leftover branch.
func (m *projectDesignManager) resolveQuestionBranch(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) string {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil || !isLiveSessionStage(view.Stage) {
		return ""
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: rc.Context}, projectstate.ProjectID(projectID))
	if err != nil {
		return ""
	}
	return designBranch(projectID, kind, amendmentIndexFor(slotFor(proj, toPSKind(kind))))
}

func (m *projectDesignManager) readProjectMaybeBranch(ctx context.Context, psID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	if branch != "" {
		if ba, ok := m.projectState.(projectstate.BranchAwareProjectStateAccess); ok {
			return ba.ReadProjectOnBranch(ctx, psID, branch)
		}
	}
	return m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
}

// isLiveSessionStage reports whether a co-author session is live (its ledger lives on the
// session branch, not main).
func isLiveSessionStage(stage SessionStage) bool {
	switch stage {
	case StageDrafting, StageAwaitingReview, StageRedrafting, StageRefused:
		return true
	case SessionStageUnknown, StageAssemblingSDP, StageCommitted, StageWithdrawn, StageDraftFailed:
		return false
	default:
		return false
	}
}

func questionsToLedger(addressee string, questions []AnchoredComment) []projectstate.ReviewComment {
	out := make([]projectstate.ReviewComment, 0, len(questions))
	for _, q := range questions {
		if strings.TrimSpace(q.Text) == "" {
			continue
		}
		out = append(out, projectstate.ReviewComment{
			Anchor:     q.JSONPath,
			AnchorText: q.AnchorText,
			Text:       q.Text,
			AuthorRole: reviewAuthorRole,
			Type:       projectstate.ReviewCommentTypeQuestion,
			Addressee:  addressee,
		})
	}
	return out
}

func nextQuestionRound(thread []projectstate.ReviewComment) int64 {
	var max int64
	for _, c := range thread {
		if c.Round > max {
			max = c.Round
		}
	}
	return max + 1
}

func askQuestionsIdempotencyKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(branch))
	_, _ = h.Write([]byte{0})
	for _, q := range qs {
		_, _ = h.Write([]byte(q.Addressee))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(q.Anchor))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(q.Text))
		_, _ = h.Write([]byte{0})
	}
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:askQuestions:%x", projectID, int(kind), h.Sum64()))
}

// answerJobDispatchSeq makes each explicit AskQuestions call produce a UNIQUE answer-job
// dispatch key, so a re-ask RE-FIRES the answer job (the RA dedups on the whole key, so a
// content-only key would swallow the re-fire — F82). AskQuestions is a direct, non-retried
// manager op (exactly one dispatch per successful call), so a per-call nonce cannot
// double-fire a single logical ask; it only enables the re-ask recovery.
var answerJobDispatchSeq atomic.Uint64

// answerJobDispatchKey derives a per-call-unique answer-job idempotency key from the
// content base plus a monotonic nonce (see answerJobDispatchSeq).
func answerJobDispatchKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	base := askQuestionsIdempotencyKey(projectID, kind, branch, qs)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:answerJob:%d", base, answerJobDispatchSeq.Add(1)))
}

// dispatchAnswerJob fires the BEST-EFFORT answer job for the freshly-seeded questions and
// LOGS the outcome loudly (F82). A dispatch miss previously vanished (the error was discarded
// and the construction-pipeline RA has no logger); now every failure mode is logged at ERROR
// (or WARN when the rail is simply not configured) with the projectID/kind/addressee/branch,
// and a success at INFO. The questions are already recorded, so a miss is recoverable by
// re-calling AskQuestions (see the op doc) — never silent.
func (m *projectDesignManager) dispatchAnswerJob(ctx context.Context, projectID ProjectID, kind ArtifactKind, branch, addressee string, qs []projectstate.ReviewComment) {
	log := slog.Default().With(
		"op", "projectdesign.AskQuestions.dispatchAnswerJob",
		"projectID", string(projectID), "artifactKind", artifactKindString(kind),
		"addressee", addressee, "branch", branch)
	if m.pipeline == nil || m.repo == nil {
		log.Warn("answer job NOT dispatched: design pipeline/repo not configured (rail dormant) — the question is recorded but will not be auto-answered")
		return
	}
	repoRef, ok := m.repo(projectID)
	if !ok {
		log.Error("answer job NOT dispatched: could not resolve the project repo — the question is recorded but will not be auto-answered; re-run AskQuestions to retry")
		return
	}
	// MANAGED-SCAFFOLD SYNC (sync-on-dispatch): an answer job runs the same seated
	// aiarch-design.yml (and installs the same aiarch-state-mcp binary) as a draft, so it
	// too must never run against a stale scaffold. Failure keeps the answer-job miss
	// semantics: recorded question, loud log, no dispatch — re-run AskQuestions to retry.
	if m.rail != nil {
		cred, cerr := m.rail.GetInstallationToken(fwra.Context{Context: ctx}, repoRef)
		if cerr != nil {
			log.Error("answer job NOT dispatched: could not mint the repo credential for the managed-scaffold sync; re-run AskQuestions to retry", "err", cerr.Error())
			return
		}
		if _, serr := sourcecontrol.SyncManagedScaffold(ctx, m.rail, repoRef, cred); serr != nil {
			log.Error("answer job NOT dispatched: managed-scaffold sync failed — the seated design workflow could not be proven current; re-run AskQuestions to retry", "err", serr.Error())
			return
		}
	}
	// Direct manager-side dispatch (NOT a Temporal workflow): the answer job is a
	// fire-and-forget submit over the PUBLISHED constructionPipelineAccess RA. The
	// RepoRef→RepoTarget decode + the placeholder step graph that the retired
	// pipelineDispatchAdapter added are inlined here (the workflow-side twin is
	// dispatchDesignJob in dispatch.go).
	target, terr := designRepoTarget(sourcecontrol.RepoRefString(repoRef))
	if terr != nil {
		log.Error("answer job NOT dispatched: could not resolve the target repo for the answer job; re-run AskQuestions to retry", "err", terr.Error())
		return
	}
	inputs := map[string]string{
		dispatchInputArtifactKind:  artifactKindString(kind),
		dispatchInputDesignPrompt:  answerPrompt(toPSKind(kind), addressee, qs),
		dispatchInputTargetBranch:  branch,
		dispatchInputPriorStateRef: "",
		dispatchInputJobMode:       jobModeAnswer,
	}
	spec := constructionpipeline.PipelineSpec{
		ProjectID: constructionpipeline.ProjectID(projectID),
		Steps: []constructionpipeline.PipelineStep{{
			Name:      "design",
			Toolchain: constructionpipeline.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: inputs,
		TargetRepo:     target,
		WorkflowFile:   designWorkflowFileName,
	}
	key := answerJobDispatchKey(projectID, kind, branch, qs)
	if _, err := m.pipeline.SubmitConstructionPipeline(fwra.Context{Context: ctx, IdempotencyKey: key}, spec); err != nil {
		log.Error("answer job dispatch FAILED — the question is recorded but not auto-answered; re-run AskQuestions with the same question to retry",
			"err", err.Error(), "key", string(key))
		return
	}
	log.Info("answer job dispatched", "key", string(key))
}

// existingQuestionRound returns the round of an EARLIER identical seeding of qs (matched by
// addressee + anchor + text of the first question), so a re-ask reuses that round rather than
// minting a fresh one — keeping the minted ids aligned with the already-seeded ledger entries
// (F82 re-dispatch correctness). ok=false when these questions were never seeded.
func existingQuestionRound(thread []projectstate.ReviewComment, qs []projectstate.ReviewComment) (int64, bool) {
	if len(qs) == 0 {
		return 0, false
	}
	first := qs[0]
	for _, c := range thread {
		if c.Type == projectstate.ReviewCommentTypeQuestion &&
			c.Addressee == first.Addressee && c.Anchor == first.Anchor && c.Text == first.Text {
			return c.Round, true
		}
	}
	return 0, false
}

func answerPrompt(kind projectstate.ArtifactKind, addressee string, qs []projectstate.ReviewComment) string {
	var b strings.Builder
	role := "Product Manager"
	if addressee == projectstate.ReviewAddresseeArchitect {
		role = "System Architect"
	}
	fmt.Fprintf(&b, "You are the %s agent, following Juval Lowy's The Method. You work ONLY through the aiarch-state MCP tools — never hand-edit files and never run git.\n", role)
	fmt.Fprintf(&b, "\nA reviewer has asked clarifying QUESTIONS about the %s artifact. Read the artifact with getCommittedSlot (or getDraftSlot if a draft is under review) and the full thread with getReviewThread for context.\n", kind.WireName())
	b.WriteString("\nAnswer EACH question below concisely and concretely, from your role's perspective. These are QUESTIONS, not change requests: do NOT rewrite the artifact — only answer. For each, call respondToReviewComment with the question's id and your answer.\n\nQuestions:\n")
	for _, q := range qs {
		if q.AnchorText != "" {
			fmt.Fprintf(&b, "- [%s] (re: %q) %s\n", q.ID, q.AnchorText, q.Text)
		} else {
			fmt.Fprintf(&b, "- [%s] %s\n", q.ID, q.Text)
		}
	}
	b.WriteString("\nWhen every question has a response, call publishDraft to commit your answers.\n")
	return b.String()
}

// isRAConflict reports whether err is the RA's stale-version Conflict on this sync write
// path (the fwra.Error form; the workflow's isConflict is for temporal-wrapped errors).
func isRAConflict(err error) bool {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		return raErr.Kind == fwra.Conflict
	}
	return false
}
