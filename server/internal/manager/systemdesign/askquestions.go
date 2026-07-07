package systemdesign

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
	"sync/atomic"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// askquestions.go implements the question-comments op (founder-ratified 2026-07-05):
// AskQuestions appends one or more clarifying QUESTIONS to an artifact's review ledger
// WITHOUT sending the draft back for a redraft, and dispatches a lightweight ANSWER job so
// the addressed role (pm / architect) answers each in place via the aiarch-state MCP's
// respondToReviewComment. Unlike change-request comments, open questions do NOT block
// approve (they surface as a soft warning at the approve gate). It works on a COMMITTED
// artifact too — seeding a question-only thread on main without opening an amendment
// session — and on a live AwaitingReview session (appending on that session's branch).

// askQuestionsMaxAttempts bounds the sync-path OCC re-read/re-apply loop.
const askQuestionsMaxAttempts = 5

// AskQuestions — the question-comments op. Appends the given questions to the artifact's
// durable review ledger as type="question" entries addressed to `addressee`, then
// dispatches an answer job. Synchronous (no Temporal workflow): the append is the durable,
// user-visible effect; the answer job is best-effort (a dispatch miss leaves the questions
// recorded and unanswered, exactly as if the addressee has not answered yet).
//
// DISPATCH RECOVERY (F82): a dispatch MISS is now LOGGED LOUDLY server-side (it was
// previously discarded, and the construction-pipeline RA has no logger, so a miss vanished
// with zero operator signal). To RECOVER a dropped dispatch, simply CALL AskQuestions AGAIN
// with the same questions: the seed is idempotent on its content key, so NO ledger entry is
// duplicated (the existing entries' round is reused so the minted ids still match), while the
// answer-job dispatch RE-FIRES via a per-call-unique key.
func (m *systemDesignManager) AskQuestions(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, addressee string, questions []AnchoredComment) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase1(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
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

	// Resolve the branch the ledger lives on: a live drafting/review session keeps the
	// thread on its session branch; a committed (or absent) session keeps it on main ("").
	branch := m.resolveQuestionBranch(rc, projectID, kind)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)
	key := askQuestionsIdempotencyKey(projectID, kind, branch, qs)

	// Sync-path optimistic-concurrency loop (mirrors SetResearchInput): read the head
	// version on the resolved branch, compute a fresh question round from the live thread
	// so the minted ids never collide with prior entries, and append. Re-read on Conflict.
	var lastErr error
	for attempt := 0; attempt < askQuestionsMaxAttempts; attempt++ {
		proj, err := m.readProjectMaybeBranch(ctx, psID, branch)
		if err != nil {
			return mapReadProjectError(err)
		}
		thread := slotFor(proj, kind).ReviewThread
		round := nextQuestionRound(thread)
		if r, ok := existingQuestionRound(thread, qs); ok {
			// A prior ask already seeded these exact questions (its answer-job dispatch may
			// have been dropped — F82). Reuse their round so the minted ids match the EXISTING
			// ledger entries, and the re-fired answer job answers the right comments.
			round = r
		}
		_, err = led.SeedReviewCommentsOnBranch(ctx, psID, proj.Version, branch, psKind, round, qs, key)
		if err == nil {
			// Best-effort dispatch of the answer job. A dispatch failure is logged by the
			// pipeline access; the questions are already durably recorded, so we do not fail
			// the op — the addressee can be re-prompted, and the SPA already shows the asks.
			// Stamp the deterministic minted ids onto a copy so the answer prompt can name
			// each question by the id the addressee must call respondToReviewComment with.
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

// resolveQuestionBranch returns the branch the artifact's review ledger currently lives on:
// the session branch when a GENUINELY ACTIVE session exists (so questions land beside the
// draft under review), else "" (main) for a committed or session-less artifact. It is
// best-effort — any read/query miss falls back to main, the safe default.
//
// F73: an ACTIVE session means the co-author Temporal workflow is OPEN and in a non-terminal
// (live) stage. Resolution reuses the P0-2 Describe-first machinery via GetSessionState —
// NOT a bare sessionState Query. A bare query REPLAYS a CLOSED run's last in-memory stage,
// which for a completed/committed (or abandoned) amendment is a stale mid-flight LIVE stage.
// Trusting it wrongly resolved a DEAD amendment's leftover branch (e.g. .../2-amend-1) — and
// because amendmentIndexFor returns >=1 for any committed slot, that branch gets synthesized
// and the seeded questions land where nothing ever merges. GetSessionState synthesizes an
// honest terminal for every closed run (StageCommitted / StageWithdrawn / StageDraftFailed)
// and errors NotFound when there is no workflow — all of which fall back to main here.
func (m *systemDesignManager) resolveQuestionBranch(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) string {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil || !isLiveSessionStage(view.Stage) {
		return ""
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: rc.Context}, projectstate.ProjectID(projectID))
	if err != nil {
		return ""
	}
	return designBranch(projectID, kind, amendmentIndexFor(slotFor(proj, kind)))
}

// readProjectMaybeBranch reads the head-state aggregate from the given branch. On a
// branch-aware substrate it uses ReadProjectOnBranch (branch=="" reads main); otherwise it
// falls back to the main ReadProject (correct for the committed/main case and the only
// option a non-branch-aware dev substrate offers).
func (m *systemDesignManager) readProjectMaybeBranch(ctx context.Context, psID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	if branch != "" {
		if ba, ok := m.projectState.(projectstate.BranchAwareProjectStateAccess); ok {
			return ba.ReadProjectOnBranch(ctx, psID, branch)
		}
	}
	return m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
}

// isLiveSessionStage reports whether a co-author session is live (its ledger lives on the
// session branch, not main). SessionStageUnknown (no execution) and StageDraftFailed mean
// there is no live branch to append to → main.
func isLiveSessionStage(stage SessionStage) bool {
	switch stage {
	case StageDrafting, StageAwaitingReview, StageRedrafting, StageRefused:
		return true
	case SessionStageUnknown, StageCommitted, StageWithdrawn, StageDraftFailed:
		return false
	default:
		return false
	}
}

// questionsToLedger converts inbound anchored questions into the projectstate.ReviewComment
// shape the append verb stamps, marking each type="question" + addressee. An empty-text
// question is dropped (defensive). Id / round / open status / empty response are minted in
// appendReviewComments.
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

// nextQuestionRound returns a round number one past the highest round already present in the
// thread (min 1), so appendReviewComments mints fresh, non-colliding ids for a new batch of
// questions regardless of how many reject/amendment rounds preceded them.
func nextQuestionRound(thread []projectstate.ReviewComment) int64 {
	var max int64
	for _, c := range thread {
		if c.Round > max {
			max = c.Round
		}
	}
	return max + 1
}

// askQuestionsIdempotencyKey derives the stable logical key for "ask this batch of questions
// on this artifact/branch". Content-derived (no Temporal context on this sync op), so a
// retried identical Ask collapses to a no-op in the RA dedup ledger while a genuinely new
// batch is a distinct mutation.
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

// answerJobDispatchKey derives a per-call-unique answer-job idempotency key from the content
// base plus a monotonic nonce (see answerJobDispatchSeq).
func answerJobDispatchKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	base := askQuestionsIdempotencyKey(projectID, kind, branch, qs)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:answerJob:%d", base, answerJobDispatchSeq.Add(1)))
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

// dispatchAnswerJob dispatches ONE lightweight agentic ANSWER job (job_mode=answer) to the
// per-project design repo so the addressed role answers each question in place via the
// aiarch-state MCP. Best-effort and fire-and-forget (it does NOT wait for the job — questions
// are auxiliary and never gate anything). F82: every outcome is LOGGED LOUDLY server-side — a
// miss (rail not configured, repo unresolved, or a submit fault) was previously discarded and
// the construction-pipeline RA has no logger, so it vanished with zero operator signal. A miss
// is recoverable by re-calling AskQuestions (see the op doc) — never silent.
func (m *systemDesignManager) dispatchAnswerJob(ctx context.Context, projectID ProjectID, kind ArtifactKind, branch, addressee string, qs []projectstate.ReviewComment) {
	log := slog.Default().With(
		"op", "systemdesign.AskQuestions.dispatchAnswerJob",
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
	key := answerJobDispatchKey(projectID, kind, branch, qs)
	if _, err := adapter.SubmitConstructionPipeline(ctx, spec, key); err != nil {
		log.Error("answer job dispatch FAILED — the question is recorded but not auto-answered; re-run AskQuestions with the same question to retry",
			"err", err.Error(), "key", string(key))
		return
	}
	log.Info("answer job dispatched", "key", string(key))
}

// answerPrompt builds the agentic ANSWER job prompt: it puts the agent in the ADDRESSEE's
// role (pm / architect), lists the questions to answer, and instructs it to answer each in
// place via respondToReviewComment then publishDraft. It never rewrites the artifact model
// (the answer job has no putDraftModel tool).
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
