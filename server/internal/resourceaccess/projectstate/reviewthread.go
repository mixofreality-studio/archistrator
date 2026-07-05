package projectstate

import (
	"fmt"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// reviewthread.go holds the DURABLE review-ledger logic for an ArtifactSlot — the
// pure, substrate-neutral helpers the GitStore review-ledger verbs build on
// (review-ledger feature, founder-ratified 2026-07-05). The ledger replaces the
// ephemeral client-only comment model: instead of comments living for one redraft
// round in workflow memory and being discarded on approve, they are appended to the
// slot's ReviewThread as server-minted, round-stamped entries that survive
// Stage/Reject/Withdraw and merge to main on approve.
//
// The ReviewComment type itself is GENERATED (contract.gen.go, from the projectStateAccess
// $defs) — this file owns only its behavior + status vocabulary, kept hand-written the
// same way the enum codecs (enumjson.go) and the ArtifactModel sum (slotcodec.go) are.

// ReviewComment.Status wire values — the closed status enum of a ledger entry.
// Kept as plain string constants (the CritiqueVerdictApprove/Revise precedent) rather
// than a typed ordinal enum: the value IS the camelCase wire name, the manager mirrors
// them on its own contract, and the transition legality is enforced in the verbs below.
const (
	// ReviewCommentOpen — filed by a reviewer, not yet addressed. Blocks approve.
	ReviewCommentOpen = "open"
	// ReviewCommentAddressed — the drafting agent committed a non-empty response on the
	// redraft (server-computed from Response presence, see normalizeReviewThread).
	ReviewCommentAddressed = "addressed"
	// ReviewCommentWaived — the human dismissed the comment without a redraft. Sticky:
	// normalization never reconsiders a waived entry.
	ReviewCommentWaived = "waived"
)

// ReviewComment.Type wire values — the closed type enum of a ledger entry
// (question-comments feature, founder-ratified 2026-07-05). The empty string is the
// MIGRATION-SAFE zero value: every legacy entry (and every reject/amendment comment)
// decodes to "" and is treated as a change-request. Only "question" entries are
// non-blocking asks routed to an addressee.
const (
	// ReviewCommentTypeChangeRequest — a comment that must be addressed by a redraft
	// (or waived) before approve. The default; "" normalizes to this.
	ReviewCommentTypeChangeRequest = "changeRequest"
	// ReviewCommentTypeQuestion — a clarifying question to an addressee (pm/architect)
	// answered in place WITHOUT a redraft; does NOT block approve.
	ReviewCommentTypeQuestion = "question"
	// ReviewCommentTypeStaleAck — an AUDIT entry recording that a reviewer marked a stale
	// committed artifact "reviewed — unaffected" (F45). It carries the reviewer's note, is
	// born addressed, and normalization never reconsiders it (like a waived entry) so it
	// stays a permanent, non-blocking trail entry rather than flipping open on a later stage.
	ReviewCommentTypeStaleAck = "staleAck"
)

// Review-comment addressee roles for question-type entries. Empty for change-requests.
const (
	ReviewAddresseePM        = "pm"
	ReviewAddresseeArchitect = "architect"
)

// ReviewCommentIsQuestion reports whether an entry is a question (migration-safe:
// the empty/legacy type is a change-request, never a question).
func ReviewCommentIsQuestion(c ReviewComment) bool {
	return c.Type == ReviewCommentTypeQuestion
}

// ReviewCommentBlocksApprove reports whether an OPEN entry gates approve: only an open
// CHANGE-REQUEST blocks. An open (unanswered) QUESTION is surfaced as a soft warning at
// the approve gate, never a hard block (question-comments §approve).
func ReviewCommentBlocksApprove(c ReviewComment) bool {
	return c.Status == ReviewCommentOpen && !ReviewCommentIsQuestion(c)
}

// reviewCommentID mints the STABLE, deterministic id for the comment filed at
// (round, index). Deterministic minting is what makes RejectArtifactOnBranchWithComments
// idempotent: a Temporal activity retry re-appends the SAME ids and appendReviewComments
// dedups on id, so the same reject never duplicates ledger entries (review-ledger §5).
// Index is 1-based for a friendlier id (r2c1 = round 2, first comment).
func reviewCommentID(round int64, index int) string {
	return fmt.Sprintf("r%dc%d", round, index+1)
}

// ReviewCommentID exposes the deterministic id minting so a caller that appends a fresh
// round (guaranteed collision-free) can predict the ids the append will stamp — e.g. the
// AskQuestions dispatch, which must name each question's id in the answer-job prompt.
func ReviewCommentID(round int64, index int) string {
	return reviewCommentID(round, index)
}

// appendStaleAck appends one ADDRESSED staleAck audit entry (F45) recording that a reviewer
// marked the artifact "reviewed — unaffected", carrying the reviewer's note. It mints a fresh
// round (past the highest present) so its id never collides, and is born addressed so it is a
// permanent non-blocking trail entry (normalization skips it). The authorRole is the reviewer.
func appendStaleAck(thread []ReviewComment, authorRole, note string) []ReviewComment {
	round := nextThreadRound(thread)
	return append(thread, ReviewComment{
		ID:         reviewCommentID(round, 0),
		Text:       staleAckText(note),
		AuthorRole: authorRole,
		Round:      round,
		Status:     ReviewCommentAddressed,
		Type:       ReviewCommentTypeStaleAck,
	})
}

// nextThreadRound returns one past the highest round present in the thread (min 1), so a
// fresh append mints collision-free ids regardless of prior reject/question rounds.
func nextThreadRound(thread []ReviewComment) int64 {
	var max int64
	for _, c := range thread {
		if c.Round > max {
			max = c.Round
		}
	}
	return max + 1
}

// staleAckText renders the audit entry body: the reviewer's note when given, else a default.
func staleAckText(note string) string {
	if note == "" {
		return "Reviewed the upstream basis change — it does not affect this artifact."
	}
	return "Reviewed — unaffected: " + note
}

// appendReviewComments appends the given round's comments to thread as OPEN entries,
// server-minting a deterministic id per (round, index), stamping the round + open status,
// and SKIPPING any id already present. The skip makes the append idempotent under Temporal
// activity retry (the same round re-appends the same ids → no duplicates). The caller
// supplies each comment's Anchor / AnchorText / Text / AuthorRole; ID / Round / Status /
// Response are authored here. Returns the grown thread.
func appendReviewComments(thread []ReviewComment, round int64, comments []ReviewComment) []ReviewComment {
	present := make(map[string]bool, len(thread))
	for _, c := range thread {
		present[c.ID] = true
	}
	for i, c := range comments {
		id := reviewCommentID(round, i)
		if present[id] {
			continue
		}
		thread = append(thread, ReviewComment{
			ID:         id,
			Anchor:     c.Anchor,
			AnchorText: c.AnchorText,
			Text:       c.Text,
			AuthorRole: c.AuthorRole,
			Round:      round,
			Status:     ReviewCommentOpen,
			Response:   "",
			// Carry the caller-supplied type/addressee (question-comments): a seeded
			// question keeps its "question" type + addressee; a reject/amendment comment
			// leaves them "" (a change-request, the migration-safe default).
			Type:      c.Type,
			Addressee: c.Addressee,
		})
		present[id] = true
	}
	return thread
}

// normalizeReviewThread reconciles every non-waived entry's Status against its Response:
// a non-empty Response means the drafting agent addressed the comment (Addressed); an
// empty Response means it is still open. This is the server's authority over the status
// the drafting agent PROPOSES on a redraft (review-ledger §3: "entries whose response came
// back empty STAY open") — the agent commits a response + a proposed addressed status into
// project.json, but the server, not the agent, decides the effective status. Waived is
// sticky (a human decision) and never reconsidered. Applied on every (re)stage so the
// ledger the reviewer sees always reflects the responses actually committed. A no-op for a
// slot with no thread (the common case).
func normalizeReviewThread(thread []ReviewComment) []ReviewComment {
	for i := range thread {
		// Waived (a human dismissal) and staleAck (an audit record) are sticky — normalization
		// never reconsiders them, so a staleAck stays addressed rather than flipping open.
		if thread[i].Status == ReviewCommentWaived || thread[i].Type == ReviewCommentTypeStaleAck {
			continue
		}
		if thread[i].Response != "" {
			thread[i].Status = ReviewCommentAddressed
		} else {
			thread[i].Status = ReviewCommentOpen
		}
	}
	return thread
}

// applyReviewCommentStatus applies a HUMAN status transition to the entry with id in
// thread. Only two transitions are legal (review-ledger §4): open→waived (dismiss) and
// addressed→open (reopen). A reopen CLEARS the response so the next redraft's
// normalizeReviewThread keeps the entry open until the agent commits a fresh response
// (otherwise the stale response would immediately re-normalize it back to addressed and
// silently undo the reopen). An unknown id is NotFound; any other transition is
// ContractMisuse. Both surface upward as a FailedPrecondition at the manager.
func applyReviewCommentStatus(thread []ReviewComment, id, status string) ([]ReviewComment, error) {
	for i := range thread {
		if thread[i].ID != id {
			continue
		}
		from := thread[i].Status
		switch {
		case from == ReviewCommentOpen && status == ReviewCommentWaived:
			thread[i].Status = ReviewCommentWaived
		case from == ReviewCommentAddressed && status == ReviewCommentOpen:
			thread[i].Status = ReviewCommentOpen
			thread[i].Response = ""
		default:
			return nil, fwra.New(fwra.ContractMisuse, fmt.Sprintf(
				"projectstate.SetReviewCommentStatus: illegal transition %q -> %q for comment %s (allowed: open->waived, addressed->open)", from, status, id))
		}
		return thread, nil
	}
	return nil, fwra.New(fwra.NotFound, fmt.Sprintf("projectstate.SetReviewCommentStatus: comment %s not found in review thread", id))
}

// validReviewCommentStatus reports whether s is one of the closed wire values.
func validReviewCommentStatus(s string) bool {
	switch s {
	case ReviewCommentOpen, ReviewCommentAddressed, ReviewCommentWaived:
		return true
	default:
		return false
	}
}
