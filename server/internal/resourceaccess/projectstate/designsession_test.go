package projectstate

import (
	"context"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// designsession_test.go unit-tests the capability-fallback chains in designsession.go:
// baseOnlyStore implements ONLY ProjectStateAccess (no optional extension) and proves
// the FALLBACK path is taken for every op; fullStore implements ProjectStateAccess plus
// every optional extension and proves the PRIMARY (extension) path is taken when a
// branch is supplied.

// baseOnlyStore implements ONLY ProjectStateAccess. Every call is recorded so a test
// can assert exactly which (base) method ran.
type baseOnlyStore struct {
	calls []string
}

func (s *baseOnlyStore) AdvancePhase(rc fwra.Context, projectID ProjectID, expectedVersion Version) (Version, error) {
	return 0, nil
}

func (s *baseOnlyStore) CommitArtifact(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind) (Version, error) {
	s.calls = append(s.calls, "CommitArtifact")
	return 10, nil
}

func (s *baseOnlyStore) CreateProject(rc fwra.Context, projectID ProjectID, owner OwnerScope, name string) (Version, error) {
	return 0, nil
}

func (s *baseOnlyStore) ListProjects(rc fwra.Context, owner OwnerScope) ([]ProjectSummary, error) {
	return nil, nil
}

func (s *baseOnlyStore) ReadProject(rc fwra.Context, projectID ProjectID) (Project, error) {
	s.calls = append(s.calls, "ReadProject")
	return Project{ID: projectID, Version: 1}, nil
}

func (s *baseOnlyStore) ReadProjectVersion(rc fwra.Context, projectID ProjectID) (Version, error) {
	return 0, nil
}

func (s *baseOnlyStore) RejectArtifact(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string) (Version, error) {
	s.calls = append(s.calls, "RejectArtifact")
	return 11, nil
}

func (s *baseOnlyStore) SetOperatingModel(rc fwra.Context, projectID ProjectID, expectedVersion Version, model OperatingModel) (Version, error) {
	return 0, nil
}

func (s *baseOnlyStore) SetResearchInput(rc fwra.Context, projectID ProjectID, expectedVersion Version, research ResearchInput) (Version, error) {
	return 0, nil
}

func (s *baseOnlyStore) StageArtifactForReview(rc fwra.Context, projectID ProjectID, expectedVersion Version, model ArtifactModel) (Version, error) {
	s.calls = append(s.calls, "StageArtifactForReview")
	return 12, nil
}

func (s *baseOnlyStore) WithdrawArtifact(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string) (Version, error) {
	s.calls = append(s.calls, "WithdrawArtifact")
	return 13, nil
}

var _ ProjectStateAccess = (*baseOnlyStore)(nil)

// fullStore implements ProjectStateAccess plus every optional extension. Every call is
// recorded so a test can assert exactly which (primary) method ran.
type fullStore struct {
	baseOnlyStore
}

func (s *fullStore) ReadProjectOnBranch(ctx context.Context, projectID ProjectID, branch string) (Project, error) {
	s.calls = append(s.calls, "ReadProjectOnBranch")
	return Project{ID: projectID, Version: 2}, nil
}

func (s *fullStore) StageArtifactForReviewOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, model ArtifactModel, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "StageArtifactForReviewOnBranch")
	return 20, nil
}

func (s *fullStore) RejectArtifactOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "RejectArtifactOnBranch")
	return 21, nil
}

func (s *fullStore) WithdrawArtifactOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "WithdrawArtifactOnBranch")
	return 22, nil
}

var _ BranchAwareProjectStateAccess = (*fullStore)(nil)

func (s *fullStore) RejectArtifactOnBranchWithComments(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "RejectArtifactOnBranchWithComments")
	return 30, nil
}

func (s *fullStore) SetReviewCommentStatusOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, commentID string, status string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "SetReviewCommentStatusOnBranch")
	return 31, nil
}

func (s *fullStore) SeedReviewCommentsOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "SeedReviewCommentsOnBranch")
	return 32, nil
}

var _ LedgerProjectStateAccess = (*fullStore)(nil)

func (s *fullStore) CommitArtifactWithProvenance(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, approvedBy, draftedBy string) (Version, error) {
	s.calls = append(s.calls, "CommitArtifactWithProvenance")
	return 40, nil
}

var _ ProvenanceCommitProjectStateAccess = (*fullStore)(nil)

func (s *fullStore) ReconcileBranchFromMain(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "ReconcileBranchFromMain")
	return 50, nil
}

var _ ReconcilingProjectStateAccess = (*fullStore)(nil)

func assertCalls(t *testing.T, calls []string, want string) {
	t.Helper()
	if len(calls) != 1 || calls[0] != want {
		t.Fatalf("calls = %v, want exactly [%s]", calls, want)
	}
}

// ---- ReadProjectOnBranch ----------------------------------------------------

func TestDesignSessionAccess_ReadProjectOnBranch_BaseFallback(t *testing.T) {
	base := &baseOnlyStore{}
	s := NewDesignSessionAccess(base)
	env, err := s.ReadProjectOnBranch(fwra.Context{Context: context.Background()}, "proj-1", "session-branch")
	if err != nil {
		t.Fatalf("ReadProjectOnBranch: %v", err)
	}
	assertCalls(t, base.calls, "ReadProject")
	if env.ID != "proj-1" || env.Version != 1 {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestDesignSessionAccess_ReadProjectOnBranch_Primary(t *testing.T) {
	full := &fullStore{}
	s := NewDesignSessionAccess(full)
	env, err := s.ReadProjectOnBranch(fwra.Context{Context: context.Background()}, "proj-1", "session-branch")
	if err != nil {
		t.Fatalf("ReadProjectOnBranch: %v", err)
	}
	assertCalls(t, full.calls, "ReadProjectOnBranch")
	if env.Version != 2 {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestDesignSessionAccess_ReadProjectOnBranch_EmptyBranchAlwaysBase(t *testing.T) {
	// Even a full-capability store falls back to the base read when branch=="" (mirrors
	// the deleted ReadProjectOnBranchActivity: an empty branch always reads main).
	full := &fullStore{}
	s := NewDesignSessionAccess(full)
	if _, err := s.ReadProjectOnBranch(fwra.Context{Context: context.Background()}, "proj-1", ""); err != nil {
		t.Fatalf("ReadProjectOnBranch: %v", err)
	}
	assertCalls(t, full.calls, "ReadProject")
}

// ---- StageArtifactForReviewOnBranch ------------------------------------------

func TestDesignSessionAccess_StageArtifactForReviewOnBranch_BaseFallback(t *testing.T) {
	base := &baseOnlyStore{}
	s := NewDesignSessionAccess(base)
	v, err := s.StageArtifactForReviewOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", nil, "idem-1")
	if err != nil {
		t.Fatalf("StageArtifactForReviewOnBranch: %v", err)
	}
	assertCalls(t, base.calls, "StageArtifactForReview")
	if v != 12 {
		t.Fatalf("Version = %d, want 12", v)
	}
}

func TestDesignSessionAccess_StageArtifactForReviewOnBranch_Primary(t *testing.T) {
	full := &fullStore{}
	s := NewDesignSessionAccess(full)
	v, err := s.StageArtifactForReviewOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", nil, "idem-1")
	if err != nil {
		t.Fatalf("StageArtifactForReviewOnBranch: %v", err)
	}
	assertCalls(t, full.calls, "StageArtifactForReviewOnBranch")
	if v != 20 {
		t.Fatalf("Version = %d, want 20", v)
	}
}

// ---- CommitArtifactWithProvenance ---------------------------------------------

func TestDesignSessionAccess_CommitArtifactWithProvenance_BaseFallback(t *testing.T) {
	base := &baseOnlyStore{}
	s := NewDesignSessionAccess(base)
	v, err := s.CommitArtifactWithProvenance(fwra.Context{Context: context.Background()}, "proj-1", 1, KindMission, "approver", "drafter")
	if err != nil {
		t.Fatalf("CommitArtifactWithProvenance: %v", err)
	}
	assertCalls(t, base.calls, "CommitArtifact")
	if v != 10 {
		t.Fatalf("Version = %d, want 10", v)
	}
}

func TestDesignSessionAccess_CommitArtifactWithProvenance_Primary(t *testing.T) {
	full := &fullStore{}
	s := NewDesignSessionAccess(full)
	v, err := s.CommitArtifactWithProvenance(fwra.Context{Context: context.Background()}, "proj-1", 1, KindMission, "approver", "drafter")
	if err != nil {
		t.Fatalf("CommitArtifactWithProvenance: %v", err)
	}
	assertCalls(t, full.calls, "CommitArtifactWithProvenance")
	if v != 40 {
		t.Fatalf("Version = %d, want 40", v)
	}
}

// ---- RejectArtifactOnBranchWithComments ---------------------------------------

func TestDesignSessionAccess_RejectArtifactOnBranchWithComments_BaseFallback(t *testing.T) {
	base := &baseOnlyStore{}
	s := NewDesignSessionAccess(base)
	v, err := s.RejectArtifactOnBranchWithComments(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "notes", 0, nil, "idem-1")
	if err != nil {
		t.Fatalf("RejectArtifactOnBranchWithComments: %v", err)
	}
	assertCalls(t, base.calls, "RejectArtifact")
	if v != 11 {
		t.Fatalf("Version = %d, want 11", v)
	}
}

func TestDesignSessionAccess_RejectArtifactOnBranchWithComments_LedgerPrimary(t *testing.T) {
	full := &fullStore{}
	s := NewDesignSessionAccess(full)
	v, err := s.RejectArtifactOnBranchWithComments(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "notes", 0, nil, "idem-1")
	if err != nil {
		t.Fatalf("RejectArtifactOnBranchWithComments: %v", err)
	}
	assertCalls(t, full.calls, "RejectArtifactOnBranchWithComments")
	if v != 30 {
		t.Fatalf("Version = %d, want 30", v)
	}
}

// branchAwareOnlyStore has BranchAware but NOT Ledger, proving the THREE-way fallback
// order (Ledger -> BranchAware -> base) lands on the middle rung.
type branchAwareOnlyStore struct {
	baseOnlyStore
}

func (s *branchAwareOnlyStore) ReadProjectOnBranch(ctx context.Context, projectID ProjectID, branch string) (Project, error) {
	return Project{}, nil
}

func (s *branchAwareOnlyStore) StageArtifactForReviewOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, model ArtifactModel, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return 0, nil
}

func (s *branchAwareOnlyStore) RejectArtifactOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "RejectArtifactOnBranch")
	return 60, nil
}

func (s *branchAwareOnlyStore) WithdrawArtifactOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return 0, nil
}

var _ BranchAwareProjectStateAccess = (*branchAwareOnlyStore)(nil)

func TestDesignSessionAccess_RejectArtifactOnBranchWithComments_BranchAwareMiddleRung(t *testing.T) {
	mid := &branchAwareOnlyStore{}
	s := NewDesignSessionAccess(mid)
	v, err := s.RejectArtifactOnBranchWithComments(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "notes", 0, nil, "idem-1")
	if err != nil {
		t.Fatalf("RejectArtifactOnBranchWithComments: %v", err)
	}
	assertCalls(t, mid.calls, "RejectArtifactOnBranch")
	if v != 60 {
		t.Fatalf("Version = %d, want 60", v)
	}
}

// ---- WithdrawArtifactOnBranch --------------------------------------------------

func TestDesignSessionAccess_WithdrawArtifactOnBranch_BaseFallback(t *testing.T) {
	base := &baseOnlyStore{}
	s := NewDesignSessionAccess(base)
	v, err := s.WithdrawArtifactOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "notes", "idem-1")
	if err != nil {
		t.Fatalf("WithdrawArtifactOnBranch: %v", err)
	}
	assertCalls(t, base.calls, "WithdrawArtifact")
	if v != 13 {
		t.Fatalf("Version = %d, want 13", v)
	}
}

func TestDesignSessionAccess_WithdrawArtifactOnBranch_Primary(t *testing.T) {
	full := &fullStore{}
	s := NewDesignSessionAccess(full)
	v, err := s.WithdrawArtifactOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "notes", "idem-1")
	if err != nil {
		t.Fatalf("WithdrawArtifactOnBranch: %v", err)
	}
	assertCalls(t, full.calls, "WithdrawArtifactOnBranch")
	if v != 22 {
		t.Fatalf("Version = %d, want 22", v)
	}
}

// ---- ReconcileBranchFromMain ----------------------------------------------------

func TestDesignSessionAccess_ReconcileBranchFromMain_Primary(t *testing.T) {
	full := &fullStore{}
	s := NewDesignSessionAccess(full)
	v, err := s.ReconcileBranchFromMain(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "idem-1")
	if err != nil {
		t.Fatalf("ReconcileBranchFromMain: %v", err)
	}
	assertCalls(t, full.calls, "ReconcileBranchFromMain")
	if v != 50 {
		t.Fatalf("Version = %d, want 50", v)
	}
}

func TestDesignSessionAccess_ReconcileBranchFromMain_UnsupportedBySubstrate(t *testing.T) {
	base := &baseOnlyStore{}
	s := NewDesignSessionAccess(base)
	_, err := s.ReconcileBranchFromMain(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "idem-1")
	assertReconcileUnsupported(t, err)
	if len(base.calls) != 0 {
		t.Fatalf("no base verb should run on the unsupported path, got %v", base.calls)
	}
}

func TestDesignSessionAccess_ReconcileBranchFromMain_UnsupportedByEmptyBranch(t *testing.T) {
	// Even a full-capability store must honor "a non-empty branch is required"
	// (ReconcilingProjectStateAccess doc) — an empty branch is the honest
	// "cannot reconcile" case, not a silent main-path reconcile.
	full := &fullStore{}
	s := NewDesignSessionAccess(full)
	_, err := s.ReconcileBranchFromMain(fwra.Context{Context: context.Background()}, "proj-1", 1, "", KindMission, "idem-1")
	assertReconcileUnsupported(t, err)
	if len(full.calls) != 0 {
		t.Fatalf("no full-store verb should run on the unsupported path, got %v", full.calls)
	}
}

// assertReconcileUnsupported checks the DISTINGUISHABLE, non-retryable error shape a
// substrate/branch that cannot reconcile surfaces: an *fwra.Error of Kind NotFound
// (DefaultRetryable()==false, so fwmanager.MapError — applied Manager-side, once a
// consumer wires this component in — tags it a Temporal NonRetryableApplicationError,
// the same terminal behavior the deleted custom activity produced via its bespoke
// "ReconcileUnsupported" Type()).
func assertReconcileUnsupported(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("want a non-nil error, got nil")
	}
	rae, ok := err.(*fwra.Error)
	if !ok {
		t.Fatalf("want *fwra.Error, got %T: %v", err, err)
	}
	if rae.Kind != fwra.NotFound {
		t.Fatalf("Kind = %v, want %v", rae.Kind, fwra.NotFound)
	}
	if rae.Retryable {
		t.Fatal("a reconcile-unsupported error must be non-retryable")
	}
}

// ---- SetReviewCommentStatusOnBranch ---------------------------------------------

func TestDesignSessionAccess_SetReviewCommentStatusOnBranch_UnsupportedBySubstrate(t *testing.T) {
	base := &baseOnlyStore{}
	s := NewDesignSessionAccess(base)
	_, err := s.SetReviewCommentStatusOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "c1", "waived", "idem-1")
	rae, ok := err.(*fwra.Error)
	if !ok || rae.Kind != fwra.NotFound {
		t.Fatalf("want *fwra.Error{Kind: NotFound}, got %T: %v", err, err)
	}
}

func TestDesignSessionAccess_SetReviewCommentStatusOnBranch_Primary(t *testing.T) {
	full := &fullStore{}
	s := NewDesignSessionAccess(full)
	v, err := s.SetReviewCommentStatusOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "c1", "waived", "idem-1")
	if err != nil {
		t.Fatalf("SetReviewCommentStatusOnBranch: %v", err)
	}
	assertCalls(t, full.calls, "SetReviewCommentStatusOnBranch")
	if v != 31 {
		t.Fatalf("Version = %d, want 31", v)
	}
}

// ---- SeedReviewCommentsOnBranch --------------------------------------------------

func TestDesignSessionAccess_SeedReviewCommentsOnBranch_UnsupportedBySubstrate(t *testing.T) {
	base := &baseOnlyStore{}
	s := NewDesignSessionAccess(base)
	_, err := s.SeedReviewCommentsOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, 0, nil, "idem-1")
	rae, ok := err.(*fwra.Error)
	if !ok || rae.Kind != fwra.NotFound {
		t.Fatalf("want *fwra.Error{Kind: NotFound}, got %T: %v", err, err)
	}
}

func TestDesignSessionAccess_SeedReviewCommentsOnBranch_Primary(t *testing.T) {
	full := &fullStore{}
	s := NewDesignSessionAccess(full)
	v, err := s.SeedReviewCommentsOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, 0, nil, "idem-1")
	if err != nil {
		t.Fatalf("SeedReviewCommentsOnBranch: %v", err)
	}
	assertCalls(t, full.calls, "SeedReviewCommentsOnBranch")
	if v != 32 {
		t.Fatalf("Version = %d, want 32", v)
	}
}
