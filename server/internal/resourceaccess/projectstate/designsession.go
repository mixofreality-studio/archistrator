package projectstate

import (
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// designsession.go implements the generated DesignSessionAccess contract
// (contract.designSessionAccess.schema.json) — the ONE component the
// branch/ledger/provenance/reconcile capability-fallback chains collapse into. Each
// method here is the SAME fallback chain the systemdesign/projectdesign Managers'
// custom activities (activities_custom.go, reviewledger.go) used to run INLINE over
// their ProjectStateAccess dependency, copied verbatim MINUS the Temporal/mapErr
// concerns (error mapping now stays Manager-side, in the generated activity layer, via
// fwmanager.MapError over the plain fwra.Error this IMPL returns).
//
// designSessionAccess WRAPS a base ProjectStateAccess rather than being implemented
// directly on *projectStateGitAdapter (the sole production ProjectStateAccess today,
// which already satisfies every optional capability interface unconditionally): the
// eight DesignSessionAccess verb NAMES collide with methods *projectStateGitAdapter
// already exports for the OLD calling convention (ctx context.Context, no
// idempotencyKey duplication) — e.g. ReadProjectOnBranch(ctx, projectID, branch)
// (Project, error) vs the new ReadProjectOnBranch(rc, projectID, branch)
// (ProjectEnvelope, error). Go does not allow two same-named methods with different
// signatures on one receiver, so this facade is a distinct type. Wrapping (rather than
// a trivial pass-through) also keeps the fallback semantics MEANINGFUL for any future
// ProjectStateAccess implementation that does not support every extension — exactly
// the scenario the capability-interface pattern exists for.
type designSessionAccess struct {
	base ProjectStateAccess
}

var _ DesignSessionAccess = (*designSessionAccess)(nil)

// NewDesignSessionAccess wraps base, running the SAME optional-capability fallback
// chains the design Managers' custom activities used to run inline against their
// ProjectStateAccess dependency directly (BranchAwareProjectStateAccess /
// LedgerProjectStateAccess / ProvenanceCommitProjectStateAccess /
// ReconcilingProjectStateAccess, each detected via a runtime type-assert on base).
func NewDesignSessionAccess(base ProjectStateAccess) DesignSessionAccess {
	return &designSessionAccess{base: base}
}

// NewGitLocalDesignSessionAccess builds the LOCAL git designSessionAccess port
// (composegen variant token GitLocal, B6) by wrapping its OWN LOCAL
// projectStateAccess instance — a second, functionally-equivalent *GitStore
// addressing the same repo, mirroring the constructionTransitionAccess /
// gitActivityStatusAccess variant constructors (gitadapter.go).
func NewGitLocalDesignSessionAccess(repoURL string) DesignSessionAccess {
	return NewDesignSessionAccess(NewGitLocalProjectStateAccess(repoURL))
}

// NewGitHubDesignSessionAccess builds the CLOUD git designSessionAccess port
// (composegen variant token GitHub, B6).
func NewGitHubDesignSessionAccess(webHost, account string, catalog ProjectCatalog, minter CredentialMinter) (DesignSessionAccess, error) {
	psa, err := NewGitHubProjectStateAccess(webHost, account, catalog, minter)
	if err != nil {
		return nil, err
	}
	return NewDesignSessionAccess(psa), nil
}

// ReadProjectOnBranch subsumes the old ReadProjectActivity (branch=="") and
// ReadProjectOnBranchActivity: routes to the branch-aware extension when base
// supports it AND a branch is supplied, else reads the default/main. Returns the
// envelope directly so the Temporal payload (once a Manager consumes this) stays a
// concrete projection.
func (s *designSessionAccess) ReadProjectOnBranch(rc fwra.Context, projectID ProjectID, branch string) (ProjectEnvelope, error) {
	var (
		proj Project
		err  error
	)
	if ba, ok := s.base.(BranchAwareProjectStateAccess); ok && branch != "" {
		proj, err = ba.ReadProjectOnBranch(rc.Context, projectID, branch)
	} else {
		proj, err = s.base.ReadProject(rc, projectID)
	}
	if err != nil {
		return ProjectEnvelope{}, err
	}
	return EncodeProject(proj)
}

// StageArtifactForReviewOnBranch decodes the wire envelope into the concrete typed
// model, then routes to the branch-aware extension when base supports it AND a branch
// is supplied, else stages on the default/main.
//
// The parameter is the codable ModelEnvelope (envelope.go), NOT the sealed
// ArtifactModel interface (B9 follow-up ruling): the op crosses a Temporal Activity
// boundary, and the default JSON DataConverter cannot decode into a non-empty
// interface parameter — the envelope is the SAME wire carrier the retired Manager-side
// custom activities always shipped across that boundary; the decode simply moved DOWN
// here, exactly where the old activity body ran it. A decode failure returns the plain
// Decode error unwrapped (NOT an fwra.Error) — byte-for-byte the class the old
// activity surfaced: fwmanager.MapError passes non-layer errors through untagged, so
// the Temporal error type/retryability is unchanged.
func (s *designSessionAccess) StageArtifactForReviewOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, model ModelEnvelope, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	decoded, err := model.Decode()
	if err != nil {
		return 0, err
	}
	if ba, ok := s.base.(BranchAwareProjectStateAccess); ok && branch != "" {
		return ba.StageArtifactForReviewOnBranch(rc.Context, projectID, expectedVersion, branch, decoded, idempotencyKey)
	}
	return s.base.StageArtifactForReview(rc, projectID, expectedVersion, decoded)
}

// CommitArtifactWithProvenance records commit provenance (committedAt/approvedBy/
// draftedBy) atomically with the commit when base supports the extension; otherwise
// falls back to the plain commit (absent provenance is allowed).
func (s *designSessionAccess) CommitArtifactWithProvenance(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, approvedBy, draftedBy string) (Version, error) {
	if pc, ok := s.base.(ProvenanceCommitProjectStateAccess); ok {
		return pc.CommitArtifactWithProvenance(rc, projectID, expectedVersion, kind, approvedBy, draftedBy)
	}
	return s.base.CommitArtifact(rc, projectID, expectedVersion, kind)
}

// RejectArtifactOnBranchWithComments prefers the durable-ledger extension (records
// the Rejected status flip AND appends the reviewer's comments in one atomic commit);
// falls back to the branch-aware reject (comments dropped) when base supports branches
// but not the ledger; falls back further to the plain main-path reject.
func (s *designSessionAccess) RejectArtifactOnBranchWithComments(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if led, ok := s.base.(LedgerProjectStateAccess); ok {
		return led.RejectArtifactOnBranchWithComments(rc.Context, projectID, expectedVersion, branch, kind, notes, round, comments, idempotencyKey)
	}
	if ba, ok := s.base.(BranchAwareProjectStateAccess); ok && branch != "" {
		return ba.RejectArtifactOnBranch(rc.Context, projectID, expectedVersion, branch, kind, notes, idempotencyKey)
	}
	return s.base.RejectArtifact(rc, projectID, expectedVersion, kind, notes)
}

// WithdrawArtifactOnBranch routes to the branch-aware extension when base supports it
// AND a branch is supplied, else withdraws on the default/main.
func (s *designSessionAccess) WithdrawArtifactOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if ba, ok := s.base.(BranchAwareProjectStateAccess); ok && branch != "" {
		return ba.WithdrawArtifactOnBranch(rc.Context, projectID, expectedVersion, branch, kind, notes, idempotencyKey)
	}
	return s.base.WithdrawArtifact(rc, projectID, expectedVersion, kind, notes)
}

// ReconcileBranchFromMain overlays main's every slot except kind's own onto the
// session-branch tip (F80c). Requires BOTH the optional Reconciling extension AND a
// non-empty branch; a substrate/call that lacks either is an HONEST "cannot
// reconcile" — surfaced as fwra.NotFound (non-retryable per Kind.DefaultRetryable,
// same as the sibling ledger-unsupported fallbacks below) rather than the bespoke
// Temporal "ReconcileUnsupported" Type() the old custom activity minted directly:
// nothing downstream asserts on that literal string (grepped — only the activity that
// produced it and its own doc comment referenced it), so converging onto the
// standard fwra.Error->fwmanager.MapError path is behavior-preserving for the
// workflow (which only ever checks err != nil here) while keeping this RA Temporal-
// free, per the layer rule.
func (s *designSessionAccess) ReconcileBranchFromMain(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	rec, ok := s.base.(ReconcilingProjectStateAccess)
	if !ok || branch == "" {
		return 0, fwra.New(fwra.NotFound, "branch reconcile unsupported by the substrate (or no session branch)")
	}
	return rec.ReconcileBranchFromMain(rc.Context, projectID, expectedVersion, branch, kind, idempotencyKey)
}

// SetReviewCommentStatusOnBranch applies a human review-ledger transition (waive/
// reopen) on the session branch. A substrate without the ledger extension has no
// thread to mutate — fwra.NotFound, which the Manager surfaces as FailedPrecondition
// (mirrors the deleted SetReviewCommentStatusActivity exactly).
func (s *designSessionAccess) SetReviewCommentStatusOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, commentID string, status string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if led, ok := s.base.(LedgerProjectStateAccess); ok {
		return led.SetReviewCommentStatusOnBranch(rc.Context, projectID, expectedVersion, branch, kind, commentID, status, idempotencyKey)
	}
	return 0, fwra.New(fwra.NotFound, "review ledger not supported by this substrate")
}

// SeedReviewCommentsOnBranch appends the F38 amendment reopening feedback as OPEN
// ledger entries (no status change). A substrate without the ledger extension has no
// thread to seed — fwra.NotFound (mirrors the deleted SeedReviewCommentsActivity
// exactly; the amendment still proceeds with the feedback woven into the prompt).
func (s *designSessionAccess) SeedReviewCommentsOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if led, ok := s.base.(LedgerProjectStateAccess); ok {
		return led.SeedReviewCommentsOnBranch(rc.Context, projectID, expectedVersion, branch, kind, round, comments, idempotencyKey)
	}
	return 0, fwra.New(fwra.NotFound, "review ledger not supported by this substrate")
}
