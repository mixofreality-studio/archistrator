package projectstate_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// Review-ledger GitStore verb tests (review-ledger feature). They exercise the durable
// comment ledger over the real local-git substrate, mirroring the branch-aware Reject tests.

func TestGitStore_RejectWithComments_AppendsOpenLedger(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReview(ctx, id, 1, &ps.MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	comments := []ps.ReviewComment{
		{Anchor: "$.vision", AnchorText: "v", Text: "sharpen the vision", AuthorRole: "architect"},
		{Anchor: "", Text: "a free-form note", AuthorRole: "architect"},
	}
	if _, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", ps.KindMission, "please revise", 1, comments, cred, "wf:reject"); err != nil {
		t.Fatalf("RejectArtifactOnBranchWithComments: %v", err)
	}
	proj, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Mission.Status != ps.ReviewRejected {
		t.Fatalf("mission status = %v, want Rejected", proj.Mission.Status)
	}
	thread := proj.Mission.ReviewThread
	if len(thread) != 2 {
		t.Fatalf("thread len = %d, want 2", len(thread))
	}
	if thread[0].ID != "r1c1" || thread[1].ID != "r1c2" {
		t.Fatalf("minted ids = %q,%q want r1c1,r1c2", thread[0].ID, thread[1].ID)
	}
	for i, c := range thread {
		if c.Status != ps.ReviewCommentOpen {
			t.Errorf("comment %d status = %q, want open", i, c.Status)
		}
	}
	if thread[0].AnchorText != "v" || thread[0].Text != "sharpen the vision" {
		t.Errorf("comment 0 fields not persisted: %+v", thread[0])
	}
}

// TestGitStore_SeedReviewComments_AppendsOpenNoStatusChange proves the F38 amendment seed:
// it appends OPEN ledger entries WITHOUT flipping the slot status (unlike reject), so an
// amendment session starts with the reopening feedback as tracked open comments while the
// freshly-staged draft stays AwaitingReview.
func TestGitStore_SeedReviewComments_AppendsOpenNoStatusChange(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReview(ctx, id, 1, &ps.MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	comments := []ps.ReviewComment{{Anchor: "$.vision", AnchorText: "v", Text: "the reopening reason", AuthorRole: "architect"}}
	if _, err := store.SeedReviewCommentsOnBranch(ctx, id, v2, "", ps.KindMission, 0, comments, cred, "wf:seed"); err != nil {
		t.Fatalf("SeedReviewCommentsOnBranch: %v", err)
	}
	proj, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	// Status UNCHANGED (still AwaitingReview — the seed does not reject).
	if proj.Mission.Status != ps.ReviewAwaitingReview {
		t.Fatalf("seed must NOT change status; got %v, want AwaitingReview", proj.Mission.Status)
	}
	thread := proj.Mission.ReviewThread
	if len(thread) != 1 || thread[0].ID != "r0c1" || thread[0].Status != ps.ReviewCommentOpen {
		t.Fatalf("seed must append one OPEN round-0 entry r0c1, got %+v", thread)
	}
	if thread[0].Text != "the reopening reason" {
		t.Fatalf("seeded comment text not persisted: %+v", thread[0])
	}
}

func TestGitStore_RejectWithComments_IdempotentOnSameKey(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReview(ctx, id, 1, &ps.MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	comments := []ps.ReviewComment{{Anchor: "$.a", Text: "one", AuthorRole: "architect"}}
	// Same idempotency key twice (a Temporal activity retry): the second collapses to the
	// committed version and MUST NOT duplicate the ledger entry (review-ledger §5).
	v3, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", ps.KindMission, "n", 1, comments, cred, "wf:reject")
	if err != nil {
		t.Fatalf("first reject: %v", err)
	}
	if _, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", ps.KindMission, "n", 1, comments, cred, "wf:reject"); err != nil {
		t.Fatalf("retry reject (same key): %v", err)
	}
	proj, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != v3 {
		t.Fatalf("retry bumped version to %d, want the committed %d (idempotent no-op)", proj.Version, v3)
	}
	if len(proj.Mission.ReviewThread) != 1 {
		t.Fatalf("ledger duplicated on retry: len = %d, want 1", len(proj.Mission.ReviewThread))
	}
}

func TestGitStore_SetReviewCommentStatus_WaiveAndReopen(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReview(ctx, id, 1, &ps.MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	comments := []ps.ReviewComment{{Anchor: "$.a", Text: "one", AuthorRole: "architect"}}
	v3, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", ps.KindMission, "n", 1, comments, cred, "wf:reject")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	// waive the open comment.
	v4, err := store.SetReviewCommentStatusOnBranch(ctx, id, v3, "", ps.KindMission, "r1c1", ps.ReviewCommentWaived, cred, "wf:waive")
	if err != nil {
		t.Fatalf("waive: %v", err)
	}
	proj, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Mission.ReviewThread[0].Status != ps.ReviewCommentWaived {
		t.Fatalf("status after waive = %q, want waived", proj.Mission.ReviewThread[0].Status)
	}
	// waived->open is not a legal transition (only open->waived / addressed->open).
	if _, err := store.SetReviewCommentStatusOnBranch(ctx, id, v4, "", ps.KindMission, "r1c1", ps.ReviewCommentOpen, cred, "wf:reopen"); kindOf(t, err) != fwra.ContractMisuse {
		t.Fatalf("waived->open kind = %v, want ContractMisuse", kindOf(t, err))
	}
}

func TestGitStore_SetReviewCommentStatus_UnknownIDNotFound(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReview(ctx, id, 1, &ps.MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	if _, err := store.SetReviewCommentStatusOnBranch(ctx, id, v2, "", ps.KindMission, "nope", ps.ReviewCommentWaived, cred, "wf:waive"); kindOf(t, err) != fwra.NotFound {
		t.Fatalf("unknown id kind = %v, want NotFound", kindOf(t, err))
	}
}

// TestGitStore_ReviewThread_SurvivesRestage proves the ledger is DURABLE across a re-stage
// (unlike the critique carrier, which a stage clears): the open comment persists, and the
// normalize-on-stage keeps it open while its response is empty (review-ledger §3).
func TestGitStore_ReviewThread_SurvivesRestage(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReview(ctx, id, 1, &ps.MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	v3, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", ps.KindMission, "n", 1, []ps.ReviewComment{{Anchor: "$.a", Text: "one"}}, cred, "wf:reject")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	// Re-stage a fresh draft (the redraft) — the thread must persist (not be cleared).
	if _, err := store.StageArtifactForReview(ctx, id, v3, &ps.MissionStatement{Vision: "v2", Mission: "m2"}, cred, "wf:restage"); err != nil {
		t.Fatalf("re-stage: %v", err)
	}
	proj, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if len(proj.Mission.ReviewThread) != 1 {
		t.Fatalf("thread lost on restage: len = %d, want 1", len(proj.Mission.ReviewThread))
	}
	if proj.Mission.ReviewThread[0].Status != ps.ReviewCommentOpen {
		t.Fatalf("open comment (empty response) normalized to %q, want open", proj.Mission.ReviewThread[0].Status)
	}
}

// TestGitStore_AcknowledgeStaleBasis proves F45: acknowledging a stale committed slot clears
// its StaleBasis and records a durable, non-blocking staleAck audit entry — and is idempotent.
func TestGitStore_AcknowledgeStaleBasis(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	stageCommit := func(v ps.Version, kind ps.ArtifactKind, model ps.ArtifactModel, tag string) ps.Version {
		v2, err := store.StageArtifactForReview(ctx, id, v, model, cred, fwra.IdempotencyKey("wf:stage:"+tag))
		if err != nil {
			t.Fatalf("stage %s: %v", tag, err)
		}
		v3, err := store.CommitArtifact(ctx, id, v2, kind, cred, fwra.IdempotencyKey("wf:commit:"+tag))
		if err != nil {
			t.Fatalf("commit %s: %v", tag, err)
		}
		return v3
	}
	// Commit Mission + Glossary, then AMEND Mission → the committed downstream Glossary goes stale.
	v := stageCommit(1, ps.KindMission, &ps.MissionStatement{Vision: "v1", Mission: "m1"}, "mission1")
	v = stageCommit(v, ps.KindGlossary, &ps.Glossary{}, "glossary1")
	v = stageCommit(v, ps.KindMission, &ps.MissionStatement{Vision: "v2", Mission: "m2"}, "mission2")
	p, _ := store.ReadProject(ctx, id, cred)
	if !p.Glossary.StaleBasis {
		t.Fatal("precondition: Glossary must be stale after Mission amend")
	}

	// ACK the stale Glossary "reviewed — unaffected".
	v2, err := store.AcknowledgeStaleBasis(ctx, id, p.Version, ps.KindGlossary, "diagrams only, no term changes", cred, "wf:ack1")
	if err != nil {
		t.Fatalf("AcknowledgeStaleBasis: %v", err)
	}
	p, _ = store.ReadProject(ctx, id, cred)
	if p.Glossary.StaleBasis {
		t.Fatal("StaleBasis must be cleared after acknowledge")
	}
	thread := p.Glossary.ReviewThread
	if len(thread) != 1 {
		t.Fatalf("want 1 staleAck audit entry, got %d", len(thread))
	}
	ack := thread[0]
	if ack.Type != ps.ReviewCommentTypeStaleAck || ack.Status != ps.ReviewCommentAddressed || ack.AuthorRole != "architect" {
		t.Errorf("audit entry shape wrong: %+v", ack)
	}
	if want := "diagrams only, no term changes"; !strings.Contains(ack.Text, want) {
		t.Errorf("audit entry text %q must carry the note %q", ack.Text, want)
	}

	// IDEMPOTENT: a repeat ack on an already-un-stale slot is a no-op — no second audit entry.
	if _, err := store.AcknowledgeStaleBasis(ctx, id, v2, ps.KindGlossary, "again", cred, "wf:ack2"); err != nil {
		t.Fatalf("repeat AcknowledgeStaleBasis: %v", err)
	}
	p, _ = store.ReadProject(ctx, id, cred)
	if len(p.Glossary.ReviewThread) != 1 {
		t.Fatalf("repeat ack must NOT append a second entry; got %d", len(p.Glossary.ReviewThread))
	}
}
