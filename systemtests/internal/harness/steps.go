package harness

import (
	"context"
	"testing"
	"time"
)

// Step helpers express UC1 as transport-agnostic actions a wire test composes.
// They poll the live system through the published surface ONLY — identical
// bodies run over HTTP or MCP, which is what lets the R4 equivalence test reuse
// them verbatim across surfaces.

// WaitForStartedSession polls GetSessionState until the started session is
// observable in a real active stage (not "" / "unknown"), proving
// draft → Manager Query → live durable session round-trips through the published
// read surface. Fails the test on timeout.
func WaitForStartedSession(ctx context.Context, t *testing.T, tr Transport, projectID, kind string, timeout time.Duration) SessionState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, found, _ := tr.GetSessionState(ctx, projectID, kind)
		if found && st.Stage != "" && st.Stage != "unknown" {
			return st
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("[%s] session %s/%s never became observable through the published surface", tr.Name(), projectID, kind)
	return SessionState{}
}

// TryReachStage best-effort polls for a target stage; returns false on timeout
// WITHOUT failing the test. Used for legs whose progress depends on the local
// model converging (model-quality dependent — must not flake a wiring test).
func TryReachStage(ctx context.Context, tr Transport, projectID, kind, wantStage string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, found, _ := tr.GetSessionState(ctx, projectID, kind)
		if found && st.Stage == wantStage {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// --- UC2 (project-design / Phase-2) step helpers ----------------------------
// The same poll-through-the-published-surface discipline as the UC1 helpers,
// but over the project-design read route (GetProjectSessionState). Kept separate
// (not parameterized over a phase) so each helper reads as one wire interaction.

// WaitForStartedProjectSession polls GetProjectSessionState until the started
// Phase-2 session is observable in a real active stage, proving
// requestProjectArtifactDraft → projectDesignManager Query → live durable session
// round-trips through the published read surface. Fails the test on timeout.
func WaitForStartedProjectSession(ctx context.Context, t *testing.T, tr Transport, projectID, kind string, timeout time.Duration) SessionState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, found, _ := tr.GetProjectSessionState(ctx, projectID, kind)
		if found && st.Stage != "" && st.Stage != "unknown" {
			return st
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("[%s] project-design session %s/%s never became observable through the published surface", tr.Name(), projectID, kind)
	return SessionState{}
}

// TryReachProjectStage best-effort polls a Phase-2 session for a target stage;
// returns false on timeout WITHOUT failing the test. Used for legs whose progress
// depends on the local model converging (model-quality dependent — must not flake
// a wiring test).
func TryReachProjectStage(ctx context.Context, tr Transport, projectID, kind, wantStage string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, found, _ := tr.GetProjectSessionState(ctx, projectID, kind)
		if found && st.Stage == wantStage {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// --- UC3 (construction / Phase-3) step helper -------------------------------
// The construction DRYRUN pump has NO model-quality dependency (every external
// effect is an instant, deterministic stub — server/cmd/server/construction_dryrun.go)
// so, unlike TryReachStage/TryReachProjectStage, reaching a target construction
// stage is a HARD (t.Fatalf on timeout) proof, not best-effort.

// TryReachConstructionStage polls GetConstructionSessionState until the named
// activity reaches wantStage or the timeout elapses. Fails the test on timeout —
// under CONSTRUCTION_DRYRUN every phase/pipeline transition is instant, so a
// bounded poll window that never reaches the target stage is a real defect, not a
// model-quality flake.
func TryReachConstructionStage(ctx context.Context, t *testing.T, tr Transport, projectID, activityID, wantStage string, timeout time.Duration) ConstructionSessionState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last ConstructionSessionState
	for time.Now().Before(deadline) {
		st, err := tr.GetConstructionSessionState(ctx, projectID, activityID)
		if err == nil {
			last = st
			if st.Stage == wantStage {
				return st
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("[%s] construction activity %s/%s never reached stage %q (last observed stage %q)",
		tr.Name(), projectID, activityID, wantStage, last.Stage)
	return ConstructionSessionState{}
}
