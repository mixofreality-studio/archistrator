package usecases

import (
	"context"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// UC1 — "give feedback → regenerate" — driven BLACK-BOX through the running
// server's review route. This is the wire-level guard for the core co-author
// capability: the human SENDS BACK a draft WITH feedback and the gate redrafts.
//
// The load-bearing wiring assertion (hard) is that a "reject" carrying non-empty
// feedback NOTES is ACCEPTED by the review route + Manager and consumed by the
// running workflow — exactly the path that was broken when the SPA sent a reject
// with anchored comments but NO notes (the Manager refuses notes-less rejects). A
// reject WITHOUT feedback is the negative control and must be refused at the wire.
//
// Reaching the human gate, and the redraft reaching it a SECOND time, both depend
// on the local model converging — so those legs are BEST-EFFORT (TryReachStage,
// no failure on timeout), matching Test_UC1_CoauthorGlossary_WiringHappyPath.
func Test_UC1_SendBackRegenerate_Wiring(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	srv := startServer(t, true /* devAuth */)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	const kind = "glossary"

	projectID, err := tr.CreateProject(ctx, "UC1 send-back regenerate")
	if err != nil {
		t.Fatalf("[%s] createProject: %v", tr.Name(), err)
	}
	if _, err := tr.RequestArtifactDraft(ctx, projectID, kind); err != nil {
		t.Fatalf("[%s] draft: %v", tr.Name(), err)
	}
	_ = harness.WaitForStartedSession(ctx, t, tr, projectID, kind, 90*time.Second)

	// Best-effort: only exercise the send-back wire once the model reached the gate.
	if !harness.TryReachStage(ctx, tr, projectID, kind, "awaitingReview", 2*time.Minute) {
		t.Logf("[%s] model did not reach the human gate in window; skipping send-back leg", tr.Name())
		return
	}

	// Negative control (hard): a reject with NO feedback is refused — the Manager
	// requires notes so a redraft always carries guidance (this is the contract the
	// SPA upholds by synthesizing notes from anchored comments).
	if err := tr.SubmitReview(ctx, projectID, kind, "reject", ""); err == nil {
		t.Fatalf("[%s] reject without feedback was accepted; want refusal", tr.Name())
	}

	// Positive (hard): a reject WITH feedback notes is accepted and signaled into
	// the live workflow — the give-feedback path the SPA now always takes.
	if err := tr.SubmitReview(ctx, projectID, kind, "reject", "Tighten the definitions; several terms are circular."); err != nil {
		t.Fatalf("[%s] reject with feedback: %v", tr.Name(), err)
	}

	// Best-effort: the redraft loop should leave the gate (redrafting) and then
	// reach the gate AGAIN with a fresh draft — the "regenerate" half of the flow.
	if harness.TryReachStage(ctx, tr, projectID, kind, "redrafting", 30*time.Second) {
		t.Logf("[%s] send-back consumed: workflow re-entered drafting", tr.Name())
	}
	if !harness.TryReachStage(ctx, tr, projectID, kind, "awaitingReview", 2*time.Minute) {
		t.Logf("[%s] redraft did not reconverge to the gate in window; send-back wire already verified", tr.Name())
		return
	}
	// And the regenerated draft can be approved through the same route.
	if err := tr.SubmitReview(ctx, projectID, kind, "approve", ""); err != nil {
		t.Fatalf("[%s] approve after regenerate: %v", tr.Name(), err)
	}
	if harness.TryReachStage(ctx, tr, projectID, kind, "committed", 30*time.Second) {
		t.Logf("[%s] UC1 give-feedback→regenerate→approve→commit verified end to end", tr.Name())
	}
}
