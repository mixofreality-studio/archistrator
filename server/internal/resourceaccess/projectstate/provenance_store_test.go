package projectstate_test

import (
	"testing"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// provenance_store_test.go — the store-level PM-P2-4 provenance verb. CommitArtifactWithProvenance
// commits the slot exactly as CommitArtifact AND stamps the provenance record, with committedAt
// server-resolved from the store clock.
func TestGitStore_CommitArtifactWithProvenance_RecordsProvenance(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	// Pin the clock so committedAt is deterministic (RA server-resolves it from the clock).
	fixed := time.Date(2026, 7, 6, 8, 30, 0, 0, time.UTC)
	store = store.WithClock(func() time.Time { return fixed })

	id := ps.ProjectID("prov-demo")
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReview(ctx, id, 1, &ps.MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err := store.CommitArtifactWithProvenance(ctx, id, v2, ps.KindMission, "alice@example.com", "agentic-design-rail", cred, "wf:commit"); err != nil {
		t.Fatalf("CommitArtifactWithProvenance: %v", err)
	}

	p, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	pr := p.Mission.Provenance
	if pr == nil {
		t.Fatal("commit with provenance must record it on the slot")
	}
	if pr.CommittedAt != fixed.Format(time.RFC3339) {
		t.Fatalf("committedAt = %q, want %q (server-resolved from clock)", pr.CommittedAt, fixed.Format(time.RFC3339))
	}
	if pr.ApprovedBy != "alice@example.com" || pr.DraftedBy != "agentic-design-rail" {
		t.Fatalf("approvedBy/draftedBy not recorded: %+v", *pr)
	}
	// The slot is committed (rev 1) exactly as a plain commit.
	if p.Mission.Status != ps.ReviewCommitted || p.Mission.Revisions != 1 {
		t.Fatalf("provenance commit must still commit the slot: status=%v rev=%d", p.Mission.Status, p.Mission.Revisions)
	}
}
