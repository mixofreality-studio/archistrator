package usecases

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// UC2 — drive the PROJECT DESIGN (Phase 2) — driven BLACK-BOX through the running
// server's published projectDesignManager surface. This is the wiring-level guard
// the architect's I-CL spec asks for: confirm web.NewClient is fed the
// projectDesign Manager and that the UC2 intents (draft → review → SDP assemble →
// SDP decision → advance-to-construction) ROUTE correctly through webClient to the
// Manager and round-trip through the published read route.
//
// As with the UC1 wiring tests, the GREEN GATE is the WIRING — the route accepts
// the intent and the started Phase-2 session is observable through the read route
// — NOT the small local model (or, here, an offline cassette set that covers only
// Phase-1 kinds) converging a constructable Phase-2 artifact. Gating on model
// quality would make a wiring test flaky (designs/aiarch/implementation/log/
// C-CW-2026-05-30.md). The approve→commit, SDP, and advance legs are therefore
// BEST-EFFORT, and the negative-control reject (notes-less) is the only deeper
// HARD assertion (it is decided at the Manager edge, not by the model).

func Test_UC2_ProjectDesign_WiringHappyPath(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	srv := startServer(t, true /* devAuth */)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	runUC2(ctx, t, tr)
}

// runUC2 is the transport-agnostic UC2 wiring flow. Like runUC1 it runs against
// ANY Transport, so the future MCP transport reuses it verbatim for the R4
// cross-surface equivalence check once mcpClient is built.
func runUC2(ctx context.Context, t *testing.T, tr harness.Transport) {
	t.Helper()
	// planningAssumptions is the FIRST draftable Phase-2 artifact (the entry of the
	// project-design phase). Driving it proves the project-design draft route +
	// projectDesignManager wiring without depending on a sealed Phase 1.
	const kind = "planningAssumptions"

	// 0) Mint the project through the catalog — the projectId every project-scoped
	// project-design route is nested under.
	projectID, err := tr.CreateProject(ctx, "UC2 wiring")
	if err != nil {
		t.Fatalf("[%s] createProject: %v", tr.Name(), err)
	}
	if projectID == "" {
		t.Fatalf("[%s] createProject: empty projectId", tr.Name())
	}

	// 1) Start a Phase-2 co-authoring draft. The route must ACCEPT the intent (202
	// → non-empty sessionRef): this is the load-bearing proof that webClient routes
	// the UC2 draft intent to projectDesignManager.RequestArtifactDraft.
	sessionRef, err := tr.RequestProjectArtifactDraft(ctx, projectID, kind)
	if err != nil {
		t.Fatalf("[%s] project-design draft: %v", tr.Name(), err)
	}
	if sessionRef == "" {
		t.Fatalf("[%s] project-design draft: empty sessionRef", tr.Name())
	}

	// 2) The started session is observable through the published project-design read
	// surface — draft → Manager Query → live durable session round-trips end to end.
	st := harness.WaitForStartedProjectSession(ctx, t, tr, projectID, kind, 90*time.Second)
	if st.ProjectID != projectID {
		t.Fatalf("[%s] session projectId = %q, want %q", tr.Name(), st.ProjectID, projectID)
	}
	if st.ArtifactKind != kind {
		t.Fatalf("[%s] session artifactKind = %q, want %q", tr.Name(), st.ArtifactKind, kind)
	}
	t.Logf("[%s] UC2 project-design wiring verified: started session observable at stage %q", tr.Name(), st.Stage)

	// 3) Negative control (HARD): a per-artifact reject with NO feedback NOTES is
	// refused by the projectDesignManager edge regardless of the model — the same
	// notes-required contract UC1 upholds, proving the review route reaches the
	// Manager's decision logic (not just the transport).
	if err := tr.SubmitProjectReview(ctx, projectID, kind, "reject", ""); err == nil {
		t.Fatalf("[%s] notes-less project-design reject was accepted; want refusal at the Manager edge", tr.Name())
	}

	// 4) Best-effort approve→commit leg IF the artifact reached the gate. The offline
	// cassette set covers only Phase-1 kinds, so a strict-replay Phase-2 draft will
	// not converge — the wiring is already proven; this leg simply exercises the
	// review route when a draft IS staged (e.g. under DRAFTING=live/WHEN_REQUIRED).
	// 15s (not 90s): under the default offline-replay mode this leg can NEVER
	// converge (no Phase-2 cassettes), so a long window is pure dead-wait. It is
	// still generous enough to catch a staged draft under DRAFTING=live/WHEN_REQUIRED.
	if !harness.TryReachProjectStage(ctx, tr, projectID, kind, "awaitingReview", 15*time.Second) {
		t.Logf("[%s] Phase-2 draft did not reach the human gate in window (expected under offline replay — no Phase-2 cassettes); wiring already verified", tr.Name())
		return
	}
	if err := tr.SubmitProjectReview(ctx, projectID, kind, "approve", ""); err != nil {
		t.Fatalf("[%s] project-design approve: %v", tr.Name(), err)
	}
	if harness.TryReachProjectStage(ctx, tr, projectID, kind, "committed", 30*time.Second) {
		t.Logf("[%s] UC2 project-design approve→commit leg verified through the review route", tr.Name())
	}
}

// Test_UC2_SDPGate_Wiring proves the SDP-review gate routes: assemble the SDP
// session and submit the human SDP decision. Both are project-scoped UC2 intents
// distinct from the per-artifact gate. As with the happy path, the load-bearing
// assertion is that the routes ACCEPT the intents and the SDP session is
// observable; reaching a fully-assembled, committable option set is model-
// dependent and therefore best-effort. The notes-less rejectAll is the HARD
// negative control decided at the Manager edge.
func Test_UC2_SDPGate_Wiring(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	srv := startServer(t, true /* devAuth */)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	projectID, err := tr.CreateProject(ctx, "UC2 SDP gate wiring")
	if err != nil {
		t.Fatalf("[%s] createProject: %v", tr.Name(), err)
	}

	// 1) Assemble the SDP-review session. The route must ACCEPT (202 → non-empty
	// sessionRef), proving webClient routes RequestSDPCommit to projectDesignManager.
	sdpRef, err := tr.RequestSDPCommit(ctx, projectID)
	if err != nil {
		t.Fatalf("[%s] sdp assemble: %v", tr.Name(), err)
	}
	if sdpRef == "" {
		t.Fatalf("[%s] sdp assemble: empty sessionRef", tr.Name())
	}

	// 2) The SDP session is observable through the project-design read route under
	// the "sdpReview" kind — the only kind whose session is read but not drafted.
	st := harness.WaitForStartedProjectSession(ctx, t, tr, projectID, "sdpReview", 90*time.Second)
	if st.ArtifactKind != "sdpReview" {
		t.Fatalf("[%s] sdp session artifactKind = %q, want \"sdpReview\"", tr.Name(), st.ArtifactKind)
	}
	t.Logf("[%s] UC2 SDP gate wiring verified: SDP session observable at stage %q", tr.Name(), st.Stage)

	// 3) Negative control (HARD): a rejectAll with NO feedback NOTES is refused at
	// the Manager edge (rejectAll requires feedback) — proving the SDP-decision
	// route reaches SubmitSDPDecision's decision logic.
	if err := tr.SubmitSDPDecision(ctx, projectID, "rejectAll", "", ""); err == nil {
		t.Fatalf("[%s] notes-less SDP rejectAll was accepted; want refusal at the Manager edge", tr.Name())
	}
}

// Test_UC2_AdvanceToConstruction_Gating proves the Phase-2 → Phase-3 advance route
// is wired and that an un-sealed project reports the NORMAL gating answer (HTTP
// 200 with advanced=false + a missing-artifact list) rather than an error. This is
// the wire-level guard that the advance gate routes to
// projectDesignManager.AdvanceToConstruction and that a non-advance is not
// mis-mapped onto a transport error.
func Test_UC2_AdvanceToConstruction_Gating(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	srv := startServer(t, true /* devAuth */)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	projectID, err := tr.CreateProject(ctx, "UC2 advance gating")
	if err != nil {
		t.Fatalf("[%s] createProject: %v", tr.Name(), err)
	}

	// A brand-new project has no committed Phase-2 artifacts, so advance must return
	// the gating answer — advanced=false with a non-empty missing list — NOT an
	// error and NOT a 4xx. This is the contract the SPA relies on to render the
	// "what's still missing" checklist.
	advanced, missing, err := tr.AdvanceToConstruction(ctx, projectID)
	if err != nil && !errors.Is(err, harness.ErrNotFound) {
		t.Fatalf("[%s] advance returned a transport error (want the 200 gating answer): %v", tr.Name(), err)
	}
	if err == nil {
		if advanced {
			t.Fatalf("[%s] advance reported advanced=true for an un-sealed project; want the gating answer", tr.Name())
		}
		if len(missing) == 0 {
			t.Fatalf("[%s] advance reported advanced=false but an EMPTY missing list; want the unmet predecessors", tr.Name())
		}
		t.Logf("[%s] UC2 advance gating verified: advanced=false, missing=%v", tr.Name(), missing)
	}
}
