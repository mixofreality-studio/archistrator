package projectdesign

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
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

	branch := m.resolveQuestionBranch(ctx, projectID, kind)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)
	key := askQuestionsIdempotencyKey(projectID, kind, branch, qs)

	var lastErr error
	for attempt := 0; attempt < askQuestionsMaxAttempts; attempt++ {
		proj, err := m.readProjectMaybeBranch(ctx, psID, branch)
		if err != nil {
			return mapReadProjectError(err)
		}
		round := nextQuestionRound(slotFor(proj, psKind).ReviewThread)
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

func (m *projectDesignManager) resolveQuestionBranch(ctx context.Context, projectID ProjectID, kind ArtifactKind) string {
	view, err := m.reviewGateView(ctx, coAuthorWorkflowID(projectID, kind))
	if err != nil || !isLiveSessionStage(view.Stage) {
		return ""
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
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

func (m *projectDesignManager) dispatchAnswerJob(ctx context.Context, projectID ProjectID, kind ArtifactKind, branch, addressee string, qs []projectstate.ReviewComment) {
	if m.pipeline == nil || m.repo == nil {
		return
	}
	repoRef, ok := m.repo(projectID)
	if !ok {
		return
	}
	adapter := pipelineDispatchAdapter{inner: m.pipeline}
	inputs := map[string]string{
		dispatchInputArtifactKind:  artifactKindString(kind),
		dispatchInputDesignPrompt:  answerPrompt(toPSKind(kind), addressee, qs),
		dispatchInputTargetBranch:  branch,
		dispatchInputPriorStateRef: "",
		dispatchInputJobMode:       jobModeAnswer,
	}
	spec := pipelineSpec{
		ProjectID:      projectID,
		DispatchInputs: inputs,
		TargetRepo:     sourcecontrol.RepoRefString(repoRef),
		WorkflowFile:   designWorkflowFileName,
	}
	key := askQuestionsIdempotencyKey(projectID, kind, branch, qs) + ":answerJob"
	_, _ = adapter.SubmitConstructionPipeline(ctx, spec, key)
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
