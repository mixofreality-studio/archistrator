// Package projectstate is the projectStateAccess component of the ResourceAccess
// layer — the git-as-DB port over the per-project repo's .aiarch/state/project.json,
// the single owner of all committed Method artifact slots (see gitstore notes below).
package projectstate

// gitstore.go is the GIT-JSON + REF-CAS realization of projectStateAccess
// (projectStateAccess.md §REWORK 2026-06-10, D-PA-R). It SUPERSEDES the Postgres
// head-state substrate: the Project aggregate is now serialized as JSON files in
// the per-project git repo under `.aiarch/state/`, optimistic concurrency is a git
// ref COMPARE-AND-SWAP (a non-fast-forward push → reload + retry) instead of a
// Postgres `version` column, and activity-retry idempotency is an in-repo
// committed `applied_mutations/<key>.json` file instead of a dedup table. The
// atomic business VERBS are unchanged but for the Manager-threaded
// `cred RepoCredential` parameter (REWORK.4); the typed Method models (§3) are
// unchanged; `Version`'s type/role are unchanged (its MEANING moved from a
// Postgres counter to an opaque per-aggregate state-commit token, REWORK.2).
//
// The raw git wire plumbing (clone / read-subtree / commit / CAS-push) lives in
// the github satellite's GitStore (framework-go-infrastructure-github/gitdata.go),
// per CustomerAppInfrastructure governance — this RA stays provider-opaque and
// names NO git lexeme (sha, ref, tree, branch) on its surface or returned types.
//
// LAYER DISCIPLINE: imports NO Temporal (expectedVersion/idempotencyKey/cred are
// ordinary parameters); calls NO sibling RA (the credential is threaded in by the
// Manager, never fetched from sourceControlAccess); the satellite is sanctioned
// infrastructure plumbing, not a ResourceAccess.

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// statePathPrefix is the reserved subtree under which aiarch's machine-state lives
// in the per-project repo, namespaced away from the human-facing construction
// outputs artifactAccess writes elsewhere in the same repo (REWORK.0).
const statePathPrefix = ".aiarch/state"

// projectFile is the whole-aggregate JSON document (one file = one aggregate = one
// consistency unit, the git analog of one Postgres row).
const projectFile = "project.json"

// appliedMutationsDir holds one committed dedup record per applied mutation
// (REWORK.3). Filenames are a filesystem-safe encoding of the idempotency key.
const appliedMutationsDir = "applied_mutations"

// RepoLocator resolves a project to its per-project git repo URL + CAS target
// branch. The deterministic per-project repo NAME is implicit to the store (REWORK.5
// / review Q2: "a well-known deterministic repo name"); the LOCATOR is the seam where
// C-PA-R wiring (composition root) supplies the concrete URL scheme —
// github.com/<owner>/<repo>.git in cloud, a file:// path in LOCAL. It is a function,
// not an RA call: no sideways edge.
//
// There is no cross-project registry index repo. The project catalog is DISCOVERED
// by enumerating the account's project repos (cloud: the GitHub App installation's
// repos filtered to the aiarch-project topic; local: the on-disk repos under the
// base dir). The enumeration capability is threaded in via ProjectCatalog, NOT a
// RegistryRepo() handle.
type RepoLocator interface {
	// ProjectRepo returns the git store handle for the given project's per-project
	// repo (the deterministic repo the App provisioned for projectID).
	ProjectRepo(projectID ProjectID) (*fwgithub.GitStore, error)
}

// BranchRepoLocator is an OPTIONAL capability a RepoLocator MAY implement to resolve a
// store handle bound to a CALLER-SUPPLIED branch (I-DESIGN-DISPATCH §2a). The GitStore
// uses it only when a non-empty branch override is threaded; a locator that does NOT
// implement it (or an empty branch) resolves the default-branch handle via ProjectRepo
// — so the branch-aware path is purely additive and the default path is unchanged.
type BranchRepoLocator interface {
	// ProjectRepoOnBranch returns the git store handle for projectID bound to branch
	// (the CAS target ref). The branch is a provider-neutral name; the locator maps it
	// to a git ref INSIDE the seam.
	ProjectRepoOnBranch(projectID ProjectID, branch string) (*fwgithub.GitStore, error)
}

// repoDescriber is an OPTIONAL capability a RepoLocator MAY implement to expose a
// human-readable identifier (the resolved repo's clone URL / local path) for
// DIAGNOSTIC TEXT ONLY — the project-identity guard (guardProjectIdentity, run
// from applyMutationOnBranchFiles' STEP 0) names it in its refused-write error
// so a human reading the failure can find the
// physical repo, not just the logical projectID. This is not a git lexeme (no
// ref/sha/tree crosses the seam) — the URL/path is already known plumbing detail
// surfaced back at NewGitLocal*/NewGitHub* construction time.
type repoDescriber interface {
	DescribeRepo(projectID ProjectID) string
}

// repoDescription returns a best-effort human-readable identifier for projectID's
// resolved repo, for error text only. Falls back to the projectID itself (per
// name-as-identity, REWORK.5 — the repo is deterministically named after it) when
// the locator does not implement repoDescriber.
func (s *GitStore) repoDescription(projectID ProjectID) string {
	if d, ok := s.locator.(repoDescriber); ok {
		if desc := d.DescribeRepo(projectID); desc != "" {
			return desc
		}
	}
	return string(projectID)
}

// projectRepo resolves the per-project store handle for a read/write. A non-empty
// branch override + a BranchRepoLocator-capable locator yields a branch-bound handle;
// otherwise the locator's default-branch ProjectRepo handle is returned (the original
// behavior). This is the SINGLE seam the branch-aware read-back + AwaitingReview-stage
// flow through — every other verb passes "" and is unperturbed.
func (s *GitStore) projectRepo(projectID ProjectID, branch string) (*fwgithub.GitStore, error) {
	if branch != "" {
		if bl, ok := s.locator.(BranchRepoLocator); ok {
			return bl.ProjectRepoOnBranch(projectID, branch)
		}
	}
	return s.locator.ProjectRepo(projectID)
}

// ProjectCatalogRef is one discovered project repo the catalog enumeration yields.
// It carries the logical project id (name-as-identity: the repo name IS the project
// id) and the display title (from the repo description set at adopt), so
// ListProjects can build a ProjectSummary WITHOUT a per-repo read for the title. The
// phase/progress still come from project.json (the N+1 read below).

// ProjectCatalog is the discover-by-enumeration seam ListProjects consumes in place
// of the deleted registry index. The composition root supplies the concrete
// enumeration (cloud: a sourceControlAccess.ListProjectRepos call mapped to refs;
// local: an on-disk scan of "aiarch-*" repos). It is a function-backed port, NOT a
// sibling RA the store calls directly — the no-sideways-edge discipline is preserved
// exactly as the cred is threaded in by the Manager's composition root. ENUMERATION
// IS THE CATALOG: there is no derived index to keep in sync.
type ProjectCatalog interface {
	// ListProjectRepos returns the project-repo refs for owner. The cred is threaded
	// for parity with the cloud token model; the local path ignores it.
	ListProjectRepos(ctx context.Context, owner OwnerScope, cred RepoCredential) ([]ProjectCatalogRef, error)
}

// GitStore is the concrete git-JSON + ref-CAS store the projectStateGitAdapter wraps
// to serve the generated ProjectStateAccess contract. It holds only the repo locator +
// a flag for whether the substrate is local on-disk git (LOCAL profile). Every call
// clones fresh through the satellite (stateless → safe under concurrency + Temporal
// replay). NO IO at construction.
type GitStore struct {
	locator RepoLocator
	// catalog is the discover-by-enumeration seam ListProjects uses (replaces the
	// deleted registry index). nil is permitted; ListProjects then returns an empty
	// catalog (a store wired for the write/read verbs but not the landing grid).
	catalog ProjectCatalog
	// local marks the LOCAL on-disk-git profile: the credential is a trivially-valid
	// local credential and the git transport attaches no HTTP auth. In cloud this is
	// false and a non-zero RepoCredential is required.
	local bool
	// clock server-resolves ActivityGitStatus.UpdatedAt (D-PA-GIT, GIT.1: the
	// timestamp is server-resolved at commit, never caller-minted). Defaults to
	// time.Now; injectable for deterministic tests.
	clock func() time.Time
}

// now returns the server-resolved current time (the injected clock, or time.Now).
func (s *GitStore) now() time.Time {
	if s.clock != nil {
		return s.clock().UTC()
	}
	return time.Now().UTC()
}

// NewGitStore builds the git-JSON store over a repo locator. `local` selects the
// LOCAL on-disk-git profile (no HTTP credential). The catalog (discover-by-
// enumeration seam for ListProjects) is wired separately via WithCatalog so the
// existing call sites (write/read verbs) keep compiling; a store with no catalog
// returns an empty landing grid. No IO.
func NewGitStore(locator RepoLocator, local bool) (*GitStore, error) {
	if locator == nil {
		return nil, fwra.New(fwra.ContractMisuse, "projectstate.NewGitStore: nil locator")
	}
	return &GitStore{locator: locator, local: local}, nil
}

// WithCatalog returns a copy of the store wired with the discover-by-enumeration
// catalog seam ListProjects consumes (the composition root supplies the concrete
// cloud/local enumeration). Kept separate from NewGitStore so the locator-only call
// sites are unaffected.
func (s *GitStore) WithCatalog(catalog ProjectCatalog) *GitStore {
	cp := *s
	cp.catalog = catalog
	return &cp
}

// WithClock returns a copy of the store using the supplied clock to server-resolve
// ActivityGitStatus.UpdatedAt (D-PA-GIT). For deterministic tests; production wiring
// leaves the default time.Now.
func (s *GitStore) WithClock(clock func() time.Time) *GitStore {
	cp := *s
	cp.clock = clock
	return &cp
}

// gitAuth folds the provider-neutral RepoCredential into the satellite's GitAuth.
// In LOCAL it is a no-op local credential; in cloud the credential Bytes become
// the bearer token. A zero credential against a cloud remote is a caller
// pre-condition violation.
func (s *GitStore) gitAuth(cred RepoCredential, op string) (fwgithub.GitAuth, error) {
	if s.local {
		return fwgithub.GitAuth{Local: true}, nil
	}
	if cred.IsZero() {
		return fwgithub.GitAuth{}, fwra.New(fwra.ContractMisuse, "projectstate."+op+": empty RepoCredential (cloud profile requires a minted credential)")
	}
	return fwgithub.GitAuth{Token: string(cred.Bytes)}, nil
}

// ---------------------------------------------------------------------------
// Atomic business verbs — each loads the project subtree, runs the dedup-first /
// version-guard / pure-transition discipline, and CAS-pushes the new state +
// dedup record in ONE commit (REWORK.3 same-commit coupling).
// ---------------------------------------------------------------------------

// StageArtifactForReviewOnBranch is the branch-aware AwaitingReview thin-write the
// design Managers use during the AwaitingReview window (I-DESIGN-DISPATCH §2a). The
// staged-slot status flip rides over the SESSION BRANCH the draft lives on so the
// review state sits with the draft (the settled "stage/commit-branch" nuance). An
// EMPTY branch behaves EXACTLY as StageArtifactForReview (the default/main) — zero
// perturbation to every existing caller.
func (s *GitStore) StageArtifactForReviewOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, model ArtifactModel, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.stageArtifactForReviewOnBranch(ctx, projectID, expectedVersion, branch, model, cred, idempotencyKey)
}

func (s *GitStore) stageArtifactForReviewOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, model ArtifactModel, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if model == nil {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.StageArtifactForReview: nil staged model")
	}
	kind := model.Kind()
	return s.applyMutationOnBranch(ctx, "StageArtifactForReview", projectID, expectedVersion, branch, cred, idempotencyKey, modeUpsert, func(p *Project) error {
		slot, ok := slotPtr(p, kind)
		if !ok {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.StageArtifactForReview: unknown kind %s", kind))
		}
		slot.Status = ReviewAwaitingReview
		slot.Model = model
		slot.Notes = ""
		// A fresh stage supersedes any prior-round critique read-back on this slot.
		slot.CritiqueVerdict = ""
		slot.CritiqueNotes = ""
		// DURABLE REVIEW LEDGER (review-ledger §3): the ReviewThread is NOT cleared on a
		// (re)stage — unlike the critique carrier it accumulates across redraft rounds. On a
		// redraft the drafting agent commits per-comment responses (+ a proposed status) into
		// the thread on this branch; reconcile every non-waived entry's effective status from
		// its response so the reviewer sees the truth the server decides, not the status the
		// agent proposed. A no-op on the first stage (empty thread).
		slot.ReviewThread = normalizeReviewThread(slot.ReviewThread)
		return nil
	})
}

// ReconcileBranchFromMain resolves a diverged session branch server-side (F80c): it reads
// main's committed aggregate and overlays every slot EXCEPT the session's OWN one (kind)
// onto the session-branch tip, then commits that reconciliation to the branch. project.json
// is a SERVER-OWNED, SINGLE-WRITER-PER-SLOT document, so the branch legitimately owns only
// `kind`; adopting main's other slots makes the branch's project.json differ from main only
// in `kind`, so the PR's 3-way merge (over the multi-line document) no longer conflicts and
// the approve-time merge can complete. It is the branch-write twin of the workflow's
// aiarch-state-mcp reconcile (both call the same overlay semantics). An EMPTY branch is a
// no-op error (reconciliation only makes sense against a real session branch).
func (s *GitStore) ReconcileBranchFromMain(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if branch == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.ReconcileBranchFromMain: empty branch (nothing to reconcile)")
	}
	// Read main's committed aggregate — the source of every OTHER slot's latest content.
	mainProj, err := s.readProjectOnBranch(ctx, projectID, "", cred)
	if err != nil {
		return 0, err
	}
	return s.applyMutationOnBranch(ctx, "ReconcileBranchFromMain", projectID, expectedVersion, branch, cred, idempotencyKey, modeUpsert, func(p *Project) error {
		// p is the session-branch tip; overlay main's slots for every kind but the
		// session's own, leaving the in-flight draft (+ its review ledger) intact.
		for _, e := range slotTable() {
			if e.kind == kind {
				continue
			}
			*e.ptr(p) = *e.ptr(&mainProj)
		}
		return nil
	})
}

// CommitArtifact flips a reviewed slot to Committed on main, without PM-P2-4
// provenance (the provenance-carrying variant is CommitArtifactWithProvenance).
func (s *GitStore) CommitArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	// commitTransition (F38) flips to Committed AND bumps Revisions + clears this slot's
	// StaleBasis + flags downstream committed slots stale — all in one atomic commit on main.
	// nil prov: this plain path records no PM-P2-4 provenance.
	return s.applyMutation(ctx, "CommitArtifact", projectID, expectedVersion, cred, idempotencyKey, modeUpsert, commitTransition(kind, nil))
}

// CommitArtifactWithProvenance is the provenance-recording Commit (PM-P2-4): the SAME atomic
// commit-on-main as CommitArtifact, plus it stamps a Provenance record onto the committed
// slot — committedAt server-resolved from the store clock (RA code, time.Now() is fine),
// approvedBy/draftedBy threaded from the manager's approve→commit path.
func (s *GitStore) CommitArtifactWithProvenance(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, approvedBy, draftedBy string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	prov := &Provenance{
		CommittedAt: s.now().UTC().Format(time.RFC3339),
		ApprovedBy:  approvedBy,
		DraftedBy:   draftedBy,
	}
	return s.applyMutation(ctx, "CommitArtifact", projectID, expectedVersion, cred, idempotencyKey, modeUpsert, commitTransition(kind, prov))
}

// WithdrawArtifactOnBranch is the branch-aware Withdraw the design Managers use during the
// AwaitingReview window (I-DESIGN-DISPATCH §2a) — the symmetric counterpart of
// RejectArtifactOnBranch. The Withdrawn status flip + notes ride over the SESSION BRANCH
// the draft was staged on, where the staged model exists and the session-branch version
// matches (main trails and carries no staged model until an approved draft merges). An
// EMPTY branch behaves EXACTLY as WithdrawArtifact (the default/main) — zero perturbation
// to every existing caller.
func (s *GitStore) WithdrawArtifactOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.withdrawArtifactOnBranch(ctx, projectID, expectedVersion, branch, kind, notes, cred, idempotencyKey)
}

func (s *GitStore) withdrawArtifactOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutationOnBranch(ctx, "WithdrawArtifact", projectID, expectedVersion, branch, cred, idempotencyKey, modeUpsert, statusTransition("WithdrawArtifact", kind, ReviewWithdrawn, notes))
}

// ---------------------------------------------------------------------------
// Review-ledger verbs (review-ledger feature, founder-ratified 2026-07-05).
// The durable comment ledger lives on the ArtifactSlot; these verbs mutate it on the
// session branch during the AwaitingReview window (same substrate routing as Reject),
// with the branch=="" main-path fallback every existing verb preserves.
// ---------------------------------------------------------------------------

// SeedReviewCommentsOnBranch appends OPEN ledger comments to a slot's ReviewThread WITHOUT
// any status change (F38 amendments). At an amendment session's start the reopening feedback
// is seeded here as round-0 open entries — the "why" the drafting agent must address and the
// reviewer tracks — on the SAME session branch the draft was staged on. It reuses the same
// deterministic, idempotent append as the reject path (appendReviewComments dedups on id).
func (s *GitStore) SeedReviewCommentsOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, round int64, comments []ReviewComment, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutationOnBranch(ctx, "SeedReviewComments", projectID, expectedVersion, branch, cred, idempotencyKey, modeUpsert, func(p *Project) error {
		slot, ok := slotPtr(p, kind)
		if !ok {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.SeedReviewComments: unknown kind %s", kind))
		}
		if slot.Status == ReviewNone || slot.Model == nil {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.SeedReviewComments: slot %s is unpopulated (stage a model first)", kind))
		}
		slot.ReviewThread = appendReviewComments(slot.ReviewThread, round, comments)
		return nil
	})
}

// AcknowledgeStaleBasis clears a committed slot's StaleBasis flag and records the reviewer's
// "reviewed — unaffected" decision as a durable staleAck audit entry, in one atomic commit on
// main (F45). It is the non-redraft counterpart to reconcile-via-amendment: a basis change
// that does NOT affect the artifact would otherwise produce a byte-identical redraft that
// dies at the no-change gate, so this lets the reviewer clear the flag with an audit trail
// instead. Idempotent: a slot that is already un-stale (a repeat ack, or a concurrent
// reconcile) is a no-op success — no second audit entry. Errors: unknown kind or an
// uncommitted slot → ContractMisuse.
func (s *GitStore) AcknowledgeStaleBasis(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, note string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutationOnBranch(ctx, "AcknowledgeStaleBasis", projectID, expectedVersion, "", cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		slot, ok := slotPtr(p, kind)
		if !ok {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.AcknowledgeStaleBasis: unknown kind %s", kind))
		}
		if slot.Status != ReviewCommitted {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.AcknowledgeStaleBasis: slot %s is not committed (only a committed, stale artifact can be acknowledged)", kind))
		}
		if !slot.StaleBasis {
			// Already un-stale (repeat ack / raced reconcile): a no-op success, no duplicate audit entry.
			return nil
		}
		slot.StaleBasis = false
		slot.ReviewThread = appendStaleAck(slot.ReviewThread, staleAckAuthorRole, note)
		return nil
	})
}

// staleAckAuthorRole is the reviewer role stamped on a staleAck audit entry. At the design
// review gate the reviewer who acknowledges staleness is the architect.
const staleAckAuthorRole = "architect"

// RejectArtifactOnBranchWithComments is the review-ledger extension of RejectArtifactOnBranch:
// it records the architect's Reject AND appends the reviewer's comments to the slot's durable
// ReviewThread in ONE atomic commit (review-ledger §2). Folding the status flip and the
// ledger append into a single mutation makes the reject crash-safe (no partial state) and
// idempotent under Temporal retry (the deterministic per-(round,index) ids dedup — see
// appendReviewComments). Each comment supplies Anchor / AnchorText / Text / AuthorRole; the
// id / round / open status are server-minted here. branch=="" behaves exactly as the main-path
// reject (the dormant-rail fallback), still appending the comments.
func (s *GitStore) RejectArtifactOnBranchWithComments(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, round int64, comments []ReviewComment, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutationOnBranch(ctx, "RejectArtifact", projectID, expectedVersion, branch, cred, idempotencyKey, modeUpsert, func(p *Project) error {
		if err := statusTransition("RejectArtifact", kind, ReviewRejected, notes)(p); err != nil {
			return err
		}
		slot, ok := slotPtr(p, kind)
		if !ok {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.RejectArtifact: unknown kind %s", kind))
		}
		slot.ReviewThread = appendReviewComments(slot.ReviewThread, round, comments)
		return nil
	})
}

// SetReviewCommentStatusOnBranch applies a HUMAN status transition (open->waived to dismiss,
// addressed->open to reopen) to a single ledger entry on the session branch during the
// AwaitingReview window (review-ledger §4). The transition legality + reopen-clears-response
// rule live in applyReviewCommentStatus (reviewthread.go). branch=="" behaves exactly as the
// main path. An unknown id surfaces NotFound; an illegal transition surfaces ContractMisuse.
func (s *GitStore) SetReviewCommentStatusOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, commentID string, status string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if !validReviewCommentStatus(status) {
		return 0, fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.SetReviewCommentStatus: unknown status %q", status))
	}
	return s.applyMutationOnBranch(ctx, "SetReviewCommentStatus", projectID, expectedVersion, branch, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		slot, ok := slotPtr(p, kind)
		if !ok {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.SetReviewCommentStatus: unknown kind %s", kind))
		}
		if slot.Status == ReviewNone || slot.Model == nil {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.SetReviewCommentStatus: slot %s is unpopulated", kind))
		}
		updated, err := applyReviewCommentStatus(slot.ReviewThread, commentID, status)
		if err != nil {
			return err
		}
		slot.ReviewThread = updated
		return nil
	})
}

// SetOperatingModel records the project-level WHO-OPERATES choice (founder ruling
// 2026-07-05). Like SetResearchInput it is a Method-INPUT head-state write, NOT a
// co-authored artifact: modeRequireExisting (the project must already exist), an
// idempotent CAS mutation, no slot transition. The value MUST be one of the two known
// models (Valid) — an unknown wire value is a terminal ContractMisuse, never persisted.
func (s *GitStore) SetOperatingModel(ctx context.Context, projectID ProjectID, expectedVersion Version, model OperatingModel, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if !model.Valid() {
		return 0, fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.SetOperatingModel: unknown operating model %q", string(model)))
	}
	return s.applyMutation(ctx, "SetOperatingModel", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		p.OperatingModel = model
		return nil
	})
}

// AdvancePhase moves the project to the next Method phase (system design →
// project design → construction) in one atomic version-guarded commit.
func (s *GitStore) AdvancePhase(ctx context.Context, projectID ProjectID, expectedVersion Version, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "AdvancePhase", projectID, expectedVersion, cred, idempotencyKey, modeUpsert, func(p *Project) error {
		p.Phase++
		return nil
	})
}

// SetResearchInput takes the wire {Title, Content} corpus (unchanged) but persists it as
// FILES (F42, founder ruling 2026-07-05): each source's Content is written to
// .aiarch/state/research/<NN>-<slug>.txt and project.json stores only the {Title, Path,
// ContentBytes} pointer (content structurally absent). The corpus files and the project.json
// pointer land in ONE atomic commit sharing the same idempotency ledger — no CommitManagedFiles
// allowlist, no platform change. A re-run with the same key dedups to the prior version.
func (s *GitStore) SetResearchInput(ctx context.Context, projectID ProjectID, expectedVersion Version, research ResearchInput, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if research.IsZero() {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.SetResearchInput: empty research (no sources)")
	}
	// Compute the corpus files (keyed RELATIVE to statePathPrefix) + the persisted pointers
	// deterministically from the input, up-front. Both ride the same atomic commit.
	files := map[string][]byte{}
	corpus := ResearchCorpus{Sources: make([]ResearchSourceRef, len(research.Sources))}
	for i, src := range research.Sources {
		files[researchFileRel(i, src.Title)] = []byte(src.Content)
		corpus.Sources[i] = ResearchSourceRef{
			Title:        src.Title,
			Path:         researchPath(i, src.Title),
			ContentBytes: int64(len(src.Content)),
		}
	}
	return s.applyMutationOnBranchFiles(ctx, "SetResearchInput", projectID, expectedVersion, "", cred, idempotencyKey, modeRequireExisting, files, func(p *Project) error {
		// Replace the whole corpus pointer set. Stale corpus files from a prior
		// SetResearchInput at other indices/slugs are dropped implicitly: buildStateFiles
		// carries forward research/* from the snapshot, but the pointer set is authoritative
		// for what the drafting Action reads, and a fresh full set supersedes the old.
		p.Research = corpus
		return nil
	})
}

// CreateProject seeds the project repo's project.json at Version 1, OR RESUMES an
// existing project. There is NO SECOND WRITE: the founder-ruled
// discover-by-enumeration model (2026-06-14) made the per-project repo itself the
// catalog entry — the repo's existence + its aiarch-project topic + its description
// (the title) ARE the catalog row that ListProjects enumerates. The repo is ADOPTED
// (with topic + description) by the projectManager's
// sourceControlAccess.AdoptProjectRepo BEFORE this call. CreateProject persists the
// supplied projectID VERBATIM as the project.json `id` — it never re-encodes it with
// an "aiarch-" prefix.
//
// PERMISSIVE-RESUME (founder ruling 2026-06-16): adopt is no longer strict-empty, so a
// repo handed to CreateProject MAY already carry a committed `.aiarch/state/project.json`
// from a prior run (the agentic Action's commits, or an earlier create). In that case
// this verb RE-INITIALIZES the project FROM CURRENT PROGRESS — it READS the existing
// committed state and returns its Version (an idempotent RESUME), NOT an already-exists
// error and NOT a clobber/reset. A repo with no committed project.json → fresh init at
// Version 1 (the original behavior). The read is the SAME branch-tip read the write path
// uses (the observed CAS base), so the resume reflects the latest committed progress.
func (s *GitStore) CreateProject(ctx context.Context, projectID ProjectID, owner OwnerScope, name string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if owner == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.CreateProject: empty owner")
	}
	if projectID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.CreateProject: zero projectID")
	}

	// RESUME probe: if the repo already carries a committed project.json, re-initialize
	// the project FROM CURRENT PROGRESS — return its existing Version without writing.
	// This is the permissive-resume path: it preserves the existing state (no clobber)
	// and is idempotent (a re-run against an already-created repo returns the same
	// project). A genuine NotFound (no state yet) falls through to the fresh init below.
	//
	// This does NOT delegate to s.ReadProject: that helper (via decodeProjectFromSnapshot)
	// STAMPS the requested projectID onto whatever it decodes and never compares it to the
	// on-disk `id` — so a bare ReadProject-based resume probe would report a FABRICATED
	// SUCCESS (existing.Version, nil) for a foreign projectID against an already-occupied
	// repo, without ever reaching applyMutationOnBranchFiles' STEP 0 identity guard
	// (guardProjectIdentity).
	// That is worse than the byte-corruption the guard elsewhere prevents: no bytes are
	// written, but the caller walks away believing a project was created that never was.
	// Read the raw snapshot ourselves instead and run the SAME guardProjectIdentity check
	// the write path uses, before any decode.
	auth, err := s.gitAuth(cred, "CreateProject")
	if err != nil {
		return 0, err
	}
	repo, err := s.projectRepo(projectID, "")
	if err != nil {
		return 0, err
	}
	snap, err := repo.ReadSubtree(ctx, statePathPrefix, auth)
	if err != nil {
		return 0, err
	}
	if err := guardProjectIdentity(snap, "CreateProject", projectID, s.repoDescription(projectID)); err != nil {
		return 0, err
	}
	existing, exists, err := decodeProjectFromSnapshot(snap, projectID)
	if err != nil {
		// A real decode fault (malformed committed JSON/slots) is surfaced; classification
		// is already fwra.ContractMisuse from decodeProjectDoc.
		return 0, err
	}
	if exists {
		// State already committed (and, per the guard above, committed to THIS projectID)
		// — RESUME (return the existing version, no write).
		return existing.Version, nil
	}

	// Fresh init: no committed state — seed project.json at Version 1.
	return s.applyMutation(ctx, "CreateProject", projectID, 0, cred, idempotencyKey, modeCreateOnly, func(p *Project) error {
		p.Owner = owner
		p.Name = name
		p.Phase = PhaseSystemDesign
		// A fresh project is born EXPLICITLY self-operated (founder ruling 2026-07-05),
		// the back-compat operating model — the customer runs the built app in their own
		// infra. The UI/MCP may flip it to archistrator-operated before StartSystemDesign
		// via SetOperatingModel. Only pre-field legacy project.json documents are ever
		// empty; those read as self-operated via OperatingModel.OrDefault.
		p.OperatingModel = OperatingModelSelfOperated
		// A fresh project defaults its review-policy sophistication dial to "vibes"
		// (Task 7): behavior-preserving — an empty GatedPhasesByType already gated
		// nothing (RequiresHuman's zero-value "pure vibes"), so this only makes the
		// default explicit. project.json is FIRST MATERIALIZED here (not by
		// `archistrator init`'s deliberately-empty .aiarch/state/ scaffold — see
		// cmd/archistrator/init.go and docs/superpowers/sdd/task-7-report.md), so this
		// is the one place the local-first funnel's default preset can be seeded
		// unconditionally for every project, local or hosted.
		preset := ReviewPresetVibes
		p.ReviewPolicy.Preset = &preset
		return nil
	})
}

// ReadProject returns the whole head-state aggregate from the project repo's
// project.json. fwra.NotFound when the aggregate has not been created. It reads the
// locator's DEFAULT branch (main) — the canonical committed head.
func (s *GitStore) ReadProject(rc fwra.Context, projectID ProjectID, cred RepoCredential) (Project, error) {
	return s.readProjectOnBranch(rc.Context, projectID, "", cred)
}

// ReadProjectOnBranch is the branch-aware read-back the design Managers use during
// the AwaitingReview window (I-DESIGN-DISPATCH §2a): an OPTIONAL per-read branch
// override resolves a per-branch GitStore handle so the read reflects the
// not-yet-merged draft the Action committed on the session branch. An EMPTY branch
// behaves EXACTLY as ReadProject (the locator's default/main) — zero perturbation to
// every existing caller. The branch override is a Manager-threaded provider-NEUTRAL
// name; the locator maps it to a git ref INSIDE the seam.
func (s *GitStore) ReadProjectOnBranch(ctx context.Context, projectID ProjectID, branch string, cred RepoCredential) (Project, error) {
	return s.readProjectOnBranch(ctx, projectID, branch, cred)
}

func (s *GitStore) readProjectOnBranch(ctx context.Context, projectID ProjectID, branch string, cred RepoCredential) (Project, error) {
	if projectID == "" {
		return Project{}, fwra.New(fwra.ContractMisuse, "projectstate.ReadProject: zero projectID")
	}
	auth, err := s.gitAuth(cred, "ReadProject")
	if err != nil {
		return Project{}, err
	}
	repo, err := s.projectRepo(projectID, branch)
	if err != nil {
		return Project{}, err
	}
	snap, err := repo.ReadSubtree(ctx, statePathPrefix, auth)
	if err != nil {
		return Project{}, err
	}
	p, exists, err := decodeProjectFromSnapshot(snap, projectID)
	if err != nil {
		return Project{}, err
	}
	if !exists {
		return Project{}, fwra.New(fwra.NotFound, fmt.Sprintf("projectstate.ReadProject: no state for project %s", projectID))
	}
	return p, nil
}

// ListProjects builds the landing-grid catalog by ENUMERATING owner's project repos
// (founder ruling 2026-06-14 — the registry index is removed). It is an N+1 read: ONE
// enumeration (the ProjectCatalog seam: cloud lists the GitHub App installation's
// aiarch-project repos; local scans the on-disk base dir) PLUS one project.json read
// per discovered project to recover phase + slot progress. The title comes from the
// repo description carried on the catalog ref (no extra read for it). This is
// ACCEPTABLE at current scale; the optimization path if it ever matters is to carry
// phase/progress in the repo topic/description so the per-repo read can be dropped.
//
// An owner with no project repos (or a store with no catalog wired) yields an empty
// slice. A project repo whose project.json cannot yet be read (provisioned but
// CreateProject not yet committed) is included with the catalog title + zero progress
// rather than dropped — the repo's existence already means the project exists.
func (s *GitStore) ListProjects(ctx context.Context, owner OwnerScope, cred RepoCredential) ([]ProjectSummary, error) {
	if owner == "" {
		return nil, fwra.New(fwra.ContractMisuse, "projectstate.ListProjects: empty owner")
	}
	if s.catalog == nil {
		return []ProjectSummary{}, nil
	}
	refs, err := s.catalog.ListProjectRepos(ctx, owner, cred)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectSummary, 0, len(refs))
	for _, ref := range refs {
		if ref.ProjectID == "" {
			continue // a repo whose name carried no parseable project id — skip defensively
		}
		summary := ProjectSummary{
			ProjectID:  ref.ProjectID,
			Name:       ref.Title,
			Owner:      owner,
			Phase:      PhaseSystemDesign,
			TotalCount: len(Phase1RequiredKinds()),
		}
		// N+1: read the per-project head-state for phase + progress + (fallback) title.
		if p, docUpdatedAt, perr := s.readProjectForList(ctx, ref.ProjectID, cred); perr == nil {
			if summary.Name == "" {
				summary.Name = p.Name
			}
			// Report the project's CANONICAL STORED owner, not the caller's requested
			// owner scope (the enumeration key). The two normally coincide, but a caller
			// may pass a wildcard/placeholder scope (e.g. "{}") and must still see each
			// project's real owner — the same value get-project returns. Fall back to the
			// enumeration scope only when the head-state carries no owner yet.
			if p.Owner != "" {
				summary.Owner = p.Owner
			}
			summary.Phase = p.Phase
			// projectUpdatedAt checks ActivityGit entries; docUpdatedAt is the
			// doc-level stamp written on every mutation (the fallback for construction-
			// phase projects that have committed design slots but no git activity yet).
			summary.UpdatedAt = projectUpdatedAt(p)
			if summary.UpdatedAt.IsZero() {
				summary.UpdatedAt = docUpdatedAt
			}
			summary.CommittedCount, summary.TotalCount = phaseProgress(p)
			// OperatorPaused (fix round 1, Task 7c): surfaces the SAME head-state flag
			// PumpSweepWorkflow's eligibility filter reads, at zero extra I/O cost — p
			// is already the full per-project read this N+1 pass performs. Omitted
			// (nil) rather than always-set-false, mirroring the doc field's own
			// "omitted when false" convention (Project.OperatorPaused).
			if p.OperatorPaused {
				paused := true
				summary.OperatorPaused = &paused
			}
			// ConstructionComplete (Task 13, finish-construction): surfaces the SAME
			// derived construction-complete signal on the catalog row, at zero extra
			// I/O cost — p is already the full per-project read this N+1 pass performs.
			// Omitted (nil) unless true, mirroring the OperatorPaused convention above.
			if isConstructionComplete(p) {
				complete := true
				summary.ConstructionComplete = &complete
			}
		} else if !isNotFound(perr) {
			// A real read fault (auth/transient/infra) on a discovered repo is surfaced;
			// a NotFound (repo provisioned, project.json not yet committed) is tolerated —
			// the catalog row stands on the repo's existence + title.
			return nil, perr
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ProjectID.String() > out[j].ProjectID.String()
	})
	return out, nil
}

// isConstructionComplete derives the catalog's construction-complete signal
// (Task 13, finish-construction): true iff the project has entered Construction
// (Phase == PhaseConstruction), has at least one ActivityConstruction row (an
// empty/nil map — construction started but nothing dispatched yet, or a project
// that never reached Construction — is never "complete"), and EVERY row has
// BOTH reached Phase == ActivityConstructionDone AND BuildStatus ==
// BuildIntegrated. A row that is merely Done-but-not-integrated (the
// Skipped/TakenOver shape RecordActivityExited leaves, projectstateaccess.go
// ~1888) or still Running/Failed keeps the project incomplete — Phase alone is
// not sufficient, matching the fixture corpus at testdata/operating_fixtures.json
// (shared byte-identically with the webApp TS side; see that file's sync
// comment). Unexported: only ListProjects (same package) calls it today.
func isConstructionComplete(p Project) bool {
	if p.Phase != PhaseConstruction || len(p.ActivityConstruction) == 0 {
		return false
	}
	for _, cs := range p.ActivityConstruction {
		if cs.Phase != ActivityConstructionDone || cs.BuildStatus != BuildIntegrated {
			return false
		}
	}
	return true
}

// readProjectForList reads the project head-state for the ListProjects N+1 pass.
// It returns the decoded Project and the best-available catalog timestamp, in
// priority order:
//  1. The doc-level UpdatedAt field (stamped by buildStateFiles on every write
//     after this field was added; zero for older project.json docs).
//  2. The git branch-tip commit time from the snapshot (the author timestamp of
//     the most recent commit to the state subtree — always available when the
//     repo has at least one commit).
//
// The zero time is returned only when neither source has data (e.g. a repo
// whose branch has no commits yet). The catalog's project-id tiebreak then
// orders the row. This private helper exists so ListProjects can carry the
// doc-level and commit-level timestamps without changing the public ReadProject
// signature or the Project aggregate type (which lives in a no-touch file).
func (s *GitStore) readProjectForList(ctx context.Context, projectID ProjectID, cred RepoCredential) (Project, time.Time, error) {
	if projectID == "" {
		return Project{}, time.Time{}, fwra.New(fwra.ContractMisuse, "projectstate.readProjectForList: zero projectID")
	}
	auth, err := s.gitAuth(cred, "ReadProject")
	if err != nil {
		return Project{}, time.Time{}, err
	}
	repo, err := s.projectRepo(projectID, "")
	if err != nil {
		return Project{}, time.Time{}, err
	}
	snap, err := repo.ReadSubtree(ctx, statePathPrefix, auth)
	if err != nil {
		return Project{}, time.Time{}, err
	}
	raw, ok := snap.Files[projectFile]
	if !ok {
		return Project{}, time.Time{}, fwra.New(fwra.NotFound, fmt.Sprintf("projectstate.ReadProject: no state for project %s", projectID))
	}
	// Decode the doc-level UpdatedAt from the raw bytes (best-effort: zero when
	// the field is absent in older project.json documents).
	var docTime struct {
		UpdatedAt time.Time `json:"updatedAt"`
	}
	_ = json.Unmarshal(raw, &docTime)
	// Pick the best-available timestamp: doc-level stamp (most precise, written
	// on every mutation after the field was introduced) > git commit time
	// (always available for an existing repo, coarser-grained) > zero.
	ts := docTime.UpdatedAt
	if ts.IsZero() {
		ts = snap.CommitTime // zero when no commits yet; non-fatal
	}
	p, exists, err := decodeProjectDoc(raw, projectID)
	if err != nil {
		return Project{}, time.Time{}, err
	}
	if !exists {
		return Project{}, time.Time{}, fwra.New(fwra.NotFound, fmt.Sprintf("projectstate.ReadProject: no state for project %s", projectID))
	}
	return p, ts, nil
}

// phaseProgress reports, for the project's current phase, how many of the
// required artifact slots are committed and the total required count.
//
//   - PhaseSystemDesign: Phase 1 required kinds only.
//   - PhaseProjectDesign: Phase 2 required kinds only (the Phase-1 baseline is
//     already fully committed to advance here; the SPA progress badge tracks
//     the CURRENT phase's work).
//   - PhaseConstruction: the full Phase-1 + Phase-2 design baseline (all 17
//     kinds), which must be entirely committed before construction begins. This
//     gives the catalog a meaningful 17/17 progress badge that reflects the
//     completed design work rather than returning 0/0 (the old default).
//   - Phases beyond construction: no defined required set; returns (0, 0).
//
// Mirrors the Postgres phaseSlotCounts over the in-memory aggregate (the git
// path already has the decoded Project).
func phaseProgress(p Project) (committed, total int) {
	var required []ArtifactKind
	switch p.Phase {
	case PhaseSystemDesign:
		required = Phase1RequiredKinds()
	case PhaseProjectDesign:
		required = Phase2RequiredKinds()
	case PhaseConstruction:
		// A project in construction has passed both Phase-1 and Phase-2 gates;
		// expose the full design baseline (Phase1 + Phase2) so the catalog row
		// shows meaningful progress rather than 0/0.
		required = append(Phase1RequiredKinds(), Phase2RequiredKinds()...)
	default:
		return 0, 0
	}
	total = len(required)
	for _, kind := range required {
		if slot, ok := slotPtr(&p, kind); ok && slot.Status == ReviewCommitted {
			committed++
		}
	}
	return committed, total
}

// projectUpdatedAt derives a catalog ordering timestamp from the aggregate. The git
// head-state has no stored row timestamp; the most-recent activity-git UpdatedAt is
// the freshest signal when present, else the zero time (the catalog then falls back
// to the project-id tiebreak in the sort). Cheap and deterministic.
func projectUpdatedAt(p Project) time.Time {
	var latest time.Time
	for _, g := range p.ActivityGit {
		if g.UpdatedAt.After(latest) {
			latest = g.UpdatedAt
		}
	}
	return latest
}

// ---------------------------------------------------------------------------
// Shared write path — the git analog of the Postgres applyMutation. Dedup-first,
// version guard, pure transition, then CAS-push (state + dedup record in ONE
// commit). A non-fast-forward push (the CAS loss) surfaces fwra.Conflict; the
// Manager's workflow re-reads and re-applies.
// ---------------------------------------------------------------------------

func (s *GitStore) applyMutation(
	ctx context.Context,
	op string,
	projectID ProjectID,
	expectedVersion Version,
	cred RepoCredential,
	idempotencyKey fwra.IdempotencyKey,
	mode mutationMode,
	mutate func(p *Project) error,
) (Version, error) {
	return s.applyMutationOnBranchFiles(ctx, op, projectID, expectedVersion, "", cred, idempotencyKey, mode, nil, mutate)
}

// applyMutationOnBranch is applyMutation parameterized by an OPTIONAL session-branch
// override (I-DESIGN-DISPATCH §2a). An EMPTY branch resolves the locator's default
// (main) — the original, unperturbed behavior every existing caller relies on. A
// non-empty branch resolves a per-branch GitStore handle so the CAS load + push ride
// over the session branch (the draft's branch), keeping the AwaitingReview thin-write
// coherent with the draft the Action committed there.
func (s *GitStore) applyMutationOnBranch(
	ctx context.Context,
	op string,
	projectID ProjectID,
	expectedVersion Version,
	branch string,
	cred RepoCredential,
	idempotencyKey fwra.IdempotencyKey,
	mode mutationMode,
	mutate func(p *Project) error,
) (Version, error) {
	return s.applyMutationOnBranchFiles(ctx, op, projectID, expectedVersion, branch, cred, idempotencyKey, mode, nil, mutate)
}

// applyMutationOnBranchFiles is applyMutationOnBranch that ALSO writes extraFiles (each
// keyed RELATIVE to statePathPrefix, e.g. "research/00-brief.txt") atomically in the SAME
// commit as project.json + the dedup record (F42). Only SetResearchInput passes non-nil
// extraFiles — the corpus files; every other verb passes nil and behaves identically to
// before. On a dedup hit (retry) the write short-circuits BEFORE any file is written, so
// the atomic first-write's files are what persist.
func (s *GitStore) applyMutationOnBranchFiles(
	ctx context.Context,
	op string,
	projectID ProjectID,
	expectedVersion Version,
	branch string,
	cred RepoCredential,
	idempotencyKey fwra.IdempotencyKey,
	mode mutationMode,
	extraFiles map[string][]byte,
	mutate func(p *Project) error,
) (Version, error) {
	if projectID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate."+op+": zero projectID")
	}
	if idempotencyKey.IsZero() {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate."+op+": empty idempotencyKey")
	}
	auth, err := s.gitAuth(cred, op)
	if err != nil {
		return 0, err
	}
	repo, err := s.projectRepo(projectID, branch)
	if err != nil {
		return 0, err
	}

	// Load the subtree at the current branch tip (the observed CAS base). The dedup
	// probe runs against THIS fetched remote tip, never a stale local clone
	// (C-PA-R invariant i).
	snap, err := repo.ReadSubtree(ctx, statePathPrefix, auth)
	if err != nil {
		return 0, err
	}

	// STEP 0 — project-identity guard, BEFORE the dedup probe. It must run first: the
	// dedup ledger (applied_mutations/<key>.json) is keyed ONLY by idempotencyKey, with
	// NO projectID scoping in the record at all (appliedRecord carries no project field).
	// If a cross-project idempotencyKey collision ever landed a dedup hit for the WRONG
	// project, STEP 1 below would return rec.ResultVersion and short-circuit — reaching
	// neither this guard nor loadAggregateForMutation's decode/mode/version gates — the
	// exact same "fabricated success, nothing written, caller told it worked" shape the
	// CreateProject resume-probe gap had. Every current idempotencyKey minted in this
	// codebase embeds a Temporal-server-assigned WorkflowExecution.RunID (a fresh UUID
	// per workflow run, globally unique across the whole Temporal server — see
	// genActivityIdempotencyKey in each manager's activities.gen.go) or a freshly minted
	// uuid.NewString() (constructionmanager.go), so a real collision is not reachable
	// today; run the guard first anyway — it is a no-op for every legitimate same-project
	// call (matching or absent id) and costs nothing.
	if err := guardProjectIdentity(snap, op, projectID, s.repoDescription(projectID)); err != nil {
		return 0, err
	}

	// STEP 1 — dedup-first probe. A committed applied_mutations/<key>.json means a
	// prior attempt already landed: return its result_version, IGNORING
	// expectedVersion (a retry may re-pass a now-stale version; the dedup must win).
	if rec, found, derr := lookupAppliedInSnapshot(snap, idempotencyKey); derr != nil {
		return 0, derr
	} else if found {
		return rec.ResultVersion, nil
	}

	// STEPS 2–3 — decode + mode gate + version guard.
	p, err := loadAggregateForMutation(snap, op, projectID, expectedVersion, mode)
	if err != nil {
		return 0, err
	}

	// STEP 4 — pure in-memory transition + version bump.
	if mErr := mutate(&p); mErr != nil {
		return 0, mErr
	}
	p.Version = expectedVersion + 1

	// STEP 5 — build the new subtree (whole project.json + ALL dedup records,
	// carrying forward the existing ones) and write the new dedup record in the SAME
	// commit (REWORK.3 same-commit coupling — atomic per git ref update).
	files, err := buildStateFiles(snap, &p, idempotencyKey, p.Version, op, s.now(), extraFiles)
	if err != nil {
		return 0, err
	}
	res, err := repo.CommitSubtree(ctx, statePathPrefix, files, snap.Base, commitMessage(op, idempotencyKey), auth)
	if err != nil {
		// A non-fast-forward CAS loss is already fwra.Conflict from the satellite.
		return 0, err
	}
	_ = res // Base token is the satellite's; Version is the caller-visible token.
	return p.Version, nil
}

// loadAggregateForMutation is applyMutationOnBranchFiles' STEP 2 + STEP 3,
// extracted verbatim. The project-identity guard (guardProjectIdentity) runs
// EARLIER, in applyMutationOnBranchFiles' STEP 0 — before the dedup probe, not
// here — so by the time this function runs, projectID is already known to either
// match the on-disk `id` or the document doesn't exist/carries no id yet. See STEP
// 0's comment in applyMutationOnBranchFiles for why it must precede the dedup
// probe (the dedup ledger has no per-project scoping at all).
//
// STEP 2 — decode the aggregate (or open a fresh one) and run the mode gate.
//
// STEP 3 — version guard (the same optimistic-concurrency check as Postgres; the
// Version lives in the committed project.json — invariant iv: derivable from repo
// state alone). The git ref-CAS at push time is the cross-process gate; this
// guard catches a stale caller even before the push.
func loadAggregateForMutation(snap fwgithub.GitSnapshot, op string, projectID ProjectID, expectedVersion Version, mode mutationMode) (Project, error) {
	p, exists, err := decodeProjectFromSnapshot(snap, projectID)
	if err != nil {
		return Project{}, err
	}
	if exists && mode == modeCreateOnly {
		return Project{}, fwra.New(fwra.Conflict, fmt.Sprintf("projectstate.%s: project %s already exists", op, projectID))
	}
	if !exists {
		if mode == modeRequireExisting {
			return Project{}, fwra.New(fwra.NotFound, fmt.Sprintf("projectstate.%s: no aggregate for project %s (create it first)", op, projectID))
		}
		if expectedVersion != 0 {
			return Project{}, fwra.New(fwra.Conflict, fmt.Sprintf("projectstate.%s: no aggregate for project %s but expectedVersion %d != 0", op, projectID, expectedVersion))
		}
		p = Project{ID: projectID, Version: 0}
	}
	if p.Version != expectedVersion {
		return Project{}, fwra.New(fwra.Conflict, fmt.Sprintf("projectstate.%s: stale version for project %s: have %d, expected %d", op, projectID, p.Version, expectedVersion))
	}
	return p, nil
}

// guardProjectIdentity refuses a mutation whose target projectID does not match the
// `id` already committed in the repo's project.json (applyMutationOnBranchFiles'
// STEP 0, before the dedup probe). It peeks the
// RAW on-disk id directly — deliberately BEFORE decodeProjectFromSnapshot runs,
// because that decoder stamps the CALLER's projectID onto the result and would
// otherwise erase the very mismatch this guard exists to catch. A missing
// project.json (fresh repo) or an empty `id` (pre-identity document / not yet
// created) is not a mismatch and passes through untouched.
//
// Classification: fwra.ContractMisuse, not fwra.Conflict. This is caller/infra
// misuse crossing a repo-identity boundary — the wrong project's caller writing
// into this repo — not an optimistic-concurrency race. A version Conflict is
// something a caller can legitimately resolve by re-reading the current version
// and reissuing the same logical mutation; an identity mismatch cannot be resolved
// that way — re-reading and retrying with the "corrected" version would just
// reproduce the identical mismatch (and, worse, a caller/framework layer that
// treats Conflict as auto-retryable-after-refresh would spin forever). Both Kinds
// report DefaultRetryable()==false at the fwra layer, but ContractMisuse is the one
// this codebase already reserves for terminal, human-recovery-gate failures (see
// decodeProjectDoc's malformed-JSON / malformed-slot classification just below,
// "QA F36": retry cannot fix bytes already at rest).
func guardProjectIdentity(snap fwgithub.GitSnapshot, op string, projectID ProjectID, repoDesc string) error {
	raw, ok := snap.Files[projectFile]
	if !ok {
		return nil // no committed document yet — nothing to protect.
	}
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		// Malformed JSON is surfaced properly (as fwra.ContractMisuse) by
		// decodeProjectFromSnapshot immediately after this guard runs; swallow it
		// here rather than duplicate that classification.
		return nil
	}
	if head.ID != "" && head.ID != string(projectID) {
		return fwra.New(fwra.ContractMisuse, fmt.Sprintf(
			"projectstate.%s: refusing to write project %s into repo %s, which already holds a document for project %s (identity mismatch — no retry can fix this; check for cross-project worker/namespace contamination)",
			op, projectID, repoDesc, head.ID))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Snapshot codec — project.json + applied_mutations/*.json.
// ---------------------------------------------------------------------------

// projectDoc is the on-infrastructure JSON shape of the whole Project aggregate.
// It mirrors the head-state fields; the typed-model slots are encoded with the
// SAME kind-discriminated envelope the Postgres JSONB codec uses (slotJSON), so
// the two substrates serialize a slot identically and a model round-trips across
// either store.
type projectDoc struct {
	ID       string         `json:"id"`
	Version  int64          `json:"version"`
	Phase    int            `json:"phase"`
	Owner    string         `json:"owner"`
	Name     string         `json:"name"`
	Research ResearchCorpus `json:"research"`
	// OperatingModel is the project-level WHO-OPERATES choice (selfOperated |
	// archistratorOperated), founder ruling 2026-07-05. omitempty so a project.json
	// that pre-dates the field decodes cleanly as the empty value — decodeProjectDoc
	// then defaults it to selfOperated (the back-compat operating model).
	OperatingModel OperatingModel      `json:"operatingModel,omitempty"`
	Slots          map[string]slotJSON `json:"slots"`
	// ActivityGit is the per-activity git-forward head-state (D-PA-GIT, GIT.1),
	// keyed by ActivityID. Omitted entirely until the first Record* git verb
	// populates it (the additive populated-in-Phase-3 posture). The map value's
	// JSON shape is ActivityGitStatus directly — every field is a JSON scalar /
	// time.Time, no provider lexeme.
	ActivityGit map[string]ActivityGitStatus `json:"activityGit,omitempty"`
	// ActivityConstruction is the per-activity construction head-state (Task 1:
	// seed-archistrator-design-state), keyed by ActivityID. Omitted entirely until
	// the first RecordActivityStarted populates it (same additive posture as ActivityGit).
	// The map value's JSON shape is ActivityConstructionStatus directly.
	ActivityConstruction map[string]ActivityConstructionStatus `json:"activityConstruction,omitempty"`
	// ConstructionProgress is the project-level tracking snapshot (Task 1 parity).
	// Omitted until seeded.
	ConstructionProgress *ConstructionProgress `json:"constructionProgress,omitempty"`
	// ServiceContracts is the per-component typed service-contract corpus, keyed by
	// component name. Omitted until seeded.
	ServiceContracts map[string]ServiceContract `json:"serviceContracts,omitempty"`
	// PhaseArtifacts holds the typed phase-scoped artifacts produced during Phase-3
	// construction (SRS, test plans, integration notes, UI designs, etc.). Omitted until
	// the first RecordPhaseArtifactProduced call populates it.
	PhaseArtifacts *PhaseArtifacts `json:"phaseArtifacts,omitempty"`
	// TestingState holds the project-level testing artifacts produced by N-* activities
	// (system test plan, harness, perf rig, quality gates, test runs, defects). Omitted
	// until the first testing activity produces output.
	TestingState *TestingState `json:"testingState,omitempty"`
	// OperatorPaused is set when an operator pauses the project's construction
	// (RecordOperatorPaused). Omitted when false so older project.json documents
	// decode cleanly as the zero value (false = not paused).
	OperatorPaused bool `json:"operatorPaused,omitempty"`
	// PauseReason is the operator-supplied reason for the pause.
	PauseReason string `json:"pauseReason,omitempty"`
	// ReviewPolicy is the per-project committed configuration of which phases require
	// human approval. Omitted when empty so existing project.json documents decode as
	// the zero value (inert — no phases gated).
	ReviewPolicy ReviewPolicy `json:"reviewPolicy"`
	// UpdatedAt is the server-resolved timestamp of the last committed state
	// mutation (set by buildStateFiles on every write). omitempty so existing
	// project.json documents that pre-date this field decode cleanly as the zero
	// time — the catalog falls back to the ActivityGit tiebreak or projectID sort
	// for those. Populated once a mutation is applied after this field was added.
	UpdatedAt time.Time `json:"updatedAt"`
}

// appliedRecord is the committed dedup record (REWORK.3). ResultVersion is the
// load-bearing field — the Version the original attempt returned, so a retry
// returns the identical result without a second state commit.
type appliedRecord struct {
	IdempotencyKey string  `json:"idempotencyKey"`
	ResultVersion  Version `json:"resultVersion"`
	Verb           string  `json:"verb"`
}

// decodeProjectFromSnapshot decodes project.json from the state subtree. exists=false
// (no project.json) means the aggregate has never been created.
func decodeProjectFromSnapshot(snap fwgithub.GitSnapshot, projectID ProjectID) (Project, bool, error) {
	raw, ok := snap.Files[projectFile]
	if !ok {
		return Project{}, false, nil
	}
	return decodeProjectDoc(raw, projectID)
}

// decodeProjectDoc is the shared raw-bytes → Project decoder over the canonical
// projectDoc shape + the substrate-neutral slot codec. It is the SINGLE codec used
// by both the live git store (decodeProjectFromSnapshot) and the out-of-process CI
// validator (DecodeProjectJSON), so the `.aiarch/state/project.json` on-disk shape
// has exactly one reader — no fork.
func decodeProjectDoc(raw []byte, projectID ProjectID) (Project, bool, error) {
	var doc projectDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		// A committed project.json that will not parse is MALFORMED COMMITTED STATE, not a
		// transient infrastructure blip: retry cannot fix bytes already at rest on a commit
		// (QA F36). Classify it TERMINAL (ContractMisuse) so the Manager's read-back retry
		// policy stops looping and routes it to the human recovery gate.
		return Project{}, false, fwra.Wrap(fwra.ContractMisuse, err, "projectstate: decode project.json")
	}
	p := Project{
		ID:       projectID,
		Version:  Version(doc.Version),
		Phase:    Phase(doc.Phase),
		Owner:    OwnerScope(doc.Owner),
		Name:     doc.Name,
		Research: doc.Research,
		// PRE-FIELD BACK-COMPAT (founder ruling 2026-07-05): a project.json committed
		// before OperatingModel existed decodes as the EMPTY value, preserved VERBATIM
		// here so the encode → decode → encode round-trip stays byte-identical (the
		// ServiceContract round-trip invariant). Readers interpret an empty model as the
		// DEFAULT (selfOperated) via OperatingModel.OrDefault — the prompts and the wire
		// mapping do exactly that — so an existing project behaves as self-operated
		// without a lazy on-read rewrite. Fresh projects are born explicit (CreateProject
		// seeds selfOperated), so only pre-field legacy documents are ever empty.
		OperatingModel:       doc.OperatingModel,
		ActivityGit:          doc.ActivityGit,
		ActivityConstruction: doc.ActivityConstruction,
		ConstructionProgress: doc.ConstructionProgress,
		ServiceContracts:     doc.ServiceContracts,
		PhaseArtifacts:       doc.PhaseArtifacts,
		TestingState:         doc.TestingState,
		OperatorPaused:       doc.OperatorPaused,
		PauseReason:          doc.PauseReason,
		ReviewPolicy:         doc.ReviewPolicy,
	}
	if err := decodeSlotsMap(doc.Slots, &p); err != nil {
		// A committed slot model that will not decode — e.g. free prose in a CLOSED-ENUM
		// field (a Trigger/Axis/CallMode wire name), a type mismatch — is MALFORMED
		// COMMITTED STATE, terminal by construction: no amount of retry decodes the same
		// bytes differently (QA F36). Classify it TERMINAL (ContractMisuse), carrying the
		// decode diagnostic, so the Manager read-back stops the infinite retry loop and
		// lands the session at the human StageDraftFailed gate WITH this reason visible.
		return Project{}, false, fwra.Wrap(fwra.ContractMisuse, err, "projectstate: decode slots")
	}
	return p, true, nil
}

// DecodeProjectJSON decodes a raw `.aiarch/state/project.json` document into the
// Project head-state aggregate. It is the exported seam the out-of-process CI
// validator consumes (historically the cmd/aiarch-validate CLI; since 2026-06-16 the
// `framework-go/methodcheck` go-test gate reads the same on-disk shape): a checked-out
// repo's committed typed state is read off disk and decoded through the SAME codec the
// live store uses, so the CI check validates the identical typed models the server
// would — the rule-set stays the single source of truth and the on-disk JSON shape has
// one reader.
//
// It deliberately takes NO RepoCredential, git satellite, or context: the CI
// validator runs over a checked-out working tree with no provider I/O. The
// aggregate ID is irrelevant to the cross-artifact rules (they read the typed
// slot models, never p.ID); callers that have no logical id may pass the zero
// ProjectID. ok=false means the bytes carried no project document.
func DecodeProjectJSON(raw []byte, projectID ProjectID) (Project, bool, error) {
	if len(raw) == 0 {
		return Project{}, false, nil
	}
	return decodeProjectDoc(raw, projectID)
}

// EncodeProjectJSON serializes a Project aggregate to the canonical
// `.aiarch/state/project.json` document shape — the exact inverse of
// DecodeProjectJSON, over the SAME projectDoc codec the live git store commits.
// It is the seam tooling/tests use to MATERIALIZE committed typed state on disk
// without the git satellite (e.g. the CI validator's regression fixtures), so the
// on-disk JSON shape has one writer as well as one reader.
func EncodeProjectJSON(p Project) ([]byte, error) {
	return encodeProjectDoc(&p, time.Time{})
}

// lookupAppliedInSnapshot probes the committed dedup records for idempotencyKey.
func lookupAppliedInSnapshot(snap fwgithub.GitSnapshot, key fwra.IdempotencyKey) (appliedRecord, bool, error) {
	path := appliedFileRel(key)
	raw, ok := snap.Files[path]
	if !ok {
		return appliedRecord{}, false, nil
	}
	var rec appliedRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return appliedRecord{}, false, fwra.Wrap(fwra.Infrastructure, err, "projectstate: decode applied_mutation")
	}
	return rec, true, nil
}

// buildStateFiles assembles the FULL state subtree for the next commit: the
// rewritten project.json, every PRE-EXISTING dedup record (carried forward so the
// whole-subtree write does not drop history), and the NEW dedup record for this
// mutation — all in one file set the satellite commits atomically.
// now is the server-resolved mutation timestamp stamped into projectDoc.UpdatedAt.
func buildStateFiles(snap fwgithub.GitSnapshot, p *Project, key fwra.IdempotencyKey, resultVersion Version, op string, now time.Time, extraFiles map[string][]byte) (map[string][]byte, error) {
	files := map[string][]byte{}
	// Carry forward existing dedup records AND corpus files (whole-subtree write semantics:
	// CommitSubtree removeDirAll's the prefix, so anything not in `files` is deleted). The
	// research/ corpus files (F42) must survive EVERY unrelated mutation — like the dedup
	// ledger — so a stage/commit/reject never wipes the corpus a prior SetResearchInput wrote.
	for path, b := range snap.Files {
		if strings.HasPrefix(path, appliedMutationsDir+"/") || strings.HasPrefix(path, researchDir+"/") {
			files[path] = b
		}
	}
	// Merge the mutation's own extra files (F42: SetResearchInput's fresh corpus). These
	// OVERWRITE any carried-forward file at the same key (a re-provisioned corpus supersedes
	// the old), keeping the corpus files + project.json pointer coherent in ONE commit.
	maps.Copy(files, extraFiles)
	// Encode the rewritten aggregate, stamping the mutation time into the doc.
	pj, err := encodeProjectDoc(p, now)
	if err != nil {
		return nil, err
	}
	files[projectFile] = pj
	// The new dedup record (same commit as the state change).
	rec := appliedRecord{IdempotencyKey: string(key), ResultVersion: resultVersion, Verb: op}
	rb, err := json.Marshal(rec)
	if err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "projectstate: encode applied_mutation")
	}
	files[appliedFileRel(key)] = rb
	return files, nil
}

// appliedFileRel is the prefix-relative path of a dedup record under the managed
// state subtree (applied_mutations/<encoded-key>.json).
func appliedFileRel(key fwra.IdempotencyKey) string {
	return appliedMutationsDir + "/" + encodeKeyFilename(key) + ".json"
}

// encodeKeyFilename maps an idempotency key (or any opaque string) to a
// filesystem-safe base file name (NO extension): lowercase base32 (no padding) of the
// key bytes. Base32 keeps it compact AND case-insensitive-filesystem-safe (REWORK.3
// "filesystem-safe encoding of the key"). Callers append the extension. (Formerly in
// gitregistry.go, which the registry-removal deleted; the dedup-record path still
// needs it.)
func encodeKeyFilename(key fwra.IdempotencyKey) string {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(key))
	return strings.ToLower(enc)
}

// isNotFound reports whether err is a fwra NotFound (the tolerated
// project.json-not-yet-committed case in ListProjects).
func isNotFound(err error) bool {
	var e *fwra.Error
	return errors.As(err, &e) && e.Kind == fwra.NotFound
}

// encodeProjectDoc serializes the Project aggregate to its on-infrastructure JSON.
// updatedAt is the server-resolved mutation timestamp; pass the zero time to
// preserve whatever updatedAt is already present in the doc (the decode will not
// have threaded it through to Project, so callers that do not need to stamp a
// fresh time pass time.Time{}). The non-zero value is always preferred.
func encodeProjectDoc(p *Project, updatedAt time.Time) ([]byte, error) {
	slots, err := encodeSlotsMap(p)
	if err != nil {
		return nil, err
	}
	doc := projectDoc{
		ID:                   p.ID.String(),
		Version:              int64(p.Version),
		Phase:                int(p.Phase),
		Owner:                string(p.Owner),
		Name:                 p.Name,
		Research:             p.Research,
		OperatingModel:       p.OperatingModel,
		Slots:                slots,
		ActivityGit:          p.ActivityGit,
		ActivityConstruction: p.ActivityConstruction,
		ConstructionProgress: p.ConstructionProgress,
		ServiceContracts:     p.ServiceContracts,
		PhaseArtifacts:       p.PhaseArtifacts,
		TestingState:         p.TestingState,
		OperatorPaused:       p.OperatorPaused,
		PauseReason:          p.PauseReason,
		ReviewPolicy:         p.ReviewPolicy,
		UpdatedAt:            updatedAt,
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "projectstate: encode project.json")
	}
	return b, nil
}

// commitMessage embeds the verb + idempotency key in the state commit message.
func commitMessage(op string, key fwra.IdempotencyKey) string {
	return "aiarch: " + op + " " + string(key)
}

// gitadapter.go holds the cred-BINDING adapter + the LOCAL deployment ports + the
// deployment VARIANT CONSTRUCTORS for projectStateAccess — the composition-root policy
// that used to live in cmd/server (buildDesignProjectState + projectstate_git_adapter.go)
// folded into the owning package (step-8 fold).
//
// THE CONTRACT-SHAPE GAP (I-GIT-DESIGN). The git substrate, *GitStore, is cred-threaded:
// every provider-touching verb carries an extra `cred RepoCredential`. The design
// Managers consume the NO-cred ProjectStateAccess
// (CreateProject / ListProjects / Stage / Commit / Reject / Withdraw / AdvancePhase /
// SetResearchInput / ReadProject WITHOUT a cred). The two surfaces cannot be mechanically
// substituted.
//
// RESOLUTION — a cred-BINDING adapter. projectStateGitAdapter presents the Managers'
// existing no-cred ProjectStateAccess and, per call, MINTS the project-scoped
// RepoCredential (via a CredentialMinter port) and injects it into the GitStore verb.
// This keeps the Manager→RA contract honest and places the credential threading EXACTLY
// where architecture.dsl puts it: "the Manager mints the credential via
// getInstallationToken(repo) and threads cred into projectStateAccess."
//
// NO SIDEWAYS EDGE. The CredentialMinter + ProjectCatalog are function/interface PORTS the
// composition root supplies — the projectstate RA NEVER imports or calls sourceControlAccess
// (D-SC §1.1 returned-not-recorded). The LOCAL profile's ports (localCredentialMinter,
// localProjectCatalog, gitRepoLocator) need no GitHub and live HERE; the CLOUD profile's
// sourcecontrol-backed ports stay at the composition root (importing sourcecontrol would be
// a forbidden RA→RA sideways edge) and are passed into NewGitHubProjectStateAccess.

// ---------------------------------------------------------------------------
// VARIANT CONSTRUCTORS — the two live projectStateAccess deployment profiles.
// ---------------------------------------------------------------------------

// NewGitLocalProjectStateAccess builds the LOCAL git projectStateAccess: file:// on-disk
// repos, no credential. The per-project repo URL is taken verbatim (the embedded profile
// drives one throwaway on-disk repo); the catalog is discovered by scanning that repo.
//
// This is the step-8 A2 composegen VARIANT constructor (variant token GitLocal):
// infra-free, so the generated composition root calls it WITHOUT an error return.
// NewGitStore's only error is a nil locator, which is unreachable here (the locator
// is always a constructed non-nil value), so it is panic-guarded as a can't-happen.
func NewGitLocalProjectStateAccess(repoURL string) ProjectStateAccess {
	locator := gitRepoLocator{
		branch:            "main",
		perProjectRepoURL: func(ProjectID) string { return repoURL },
	}
	store, err := NewGitStore(locator, true /* local */)
	if err != nil {
		// Unreachable: locator is a non-nil value. A panic here means the invariant
		// was broken by a code change, not a runtime/config condition.
		panic("projectstate.NewGitLocalProjectStateAccess: " + err.Error())
	}
	// Discover-by-enumeration over the single on-disk project repo (no GitHub
	// installation API in local mode — founder ruling 2026-06-14).
	store = store.WithCatalog(localProjectCatalog{repoURL: repoURL, branch: "main"})
	return &projectStateGitAdapter{store: store, minter: localCredentialMinter{}}
}

// NewGitHubProjectStateAccess builds the CLOUD git projectStateAccess: the per-project
// repos live under the account as <webHost>/<account>/<name>.git, where <name> IS the
// project identity (name-as-identity, C-PA-AD 2026-06-15). The catalog + credential minter
// are sourcecontrol-backed PORTS supplied by the composition root (importing sourcecontrol
// here would be a forbidden RA→RA sideways edge); the installation token is minted in-seam
// through the minter.
func NewGitHubProjectStateAccess(webHost, account string, catalog ProjectCatalog, minter CredentialMinter) (ProjectStateAccess, error) {
	locator := gitRepoLocator{
		branch: "main",
		perProjectRepoURL: func(projectID ProjectID) string {
			return cloudPerProjectRepoURL(webHost, account, projectID.String())
		},
	}
	store, err := NewGitStore(locator, false /* cloud */)
	if err != nil {
		return nil, err
	}
	// Discover-by-enumeration: list the GitHub App installation's aiarch-project repos
	// (founder ruling 2026-06-14 — the registry index repo is removed).
	store = store.WithCatalog(catalog)
	return &projectStateGitAdapter{store: store, minter: minter}, nil
}

// GitConstructionPorts type-asserts a ProjectStateAccess built by the Git* variants back to
// the construction-transition + git-activity-status ports its backing *GitStore serves
// (the construction Manager shares the design head-state store; status cascade live). ok is
// false for a non-git ProjectStateAccess. This encapsulates the composition root's former
// private-field type-assertion so the adapter stays unexported.
func GitConstructionPorts(psa ProjectStateAccess) (ConstructionTransitionAccess, GitActivityStatusAccess, bool) {
	if a, ok := psa.(*projectStateGitAdapter); ok {
		return a.store, a.store, true
	}
	return nil, nil, false
}

// ---------------------------------------------------------------------------
// constructionTransitionAccess / gitActivityStatusAccess VARIANT CONSTRUCTORS
// (B6). These are composegen DI entry points for the two secondary contracts
// promoted off the SAME git substrate as projectStateAccess (B2/B3) — each
// builds its OWN *GitStore via the identical Git* projectStateAccess
// constructor (a second, functionally-equivalent instance addressing the same
// repo; GitStore holds no mutable connection state, so this mirrors the
// composition root's pre-existing "SEPARATE instance, consistent because both
// address the same git repo" pattern, formerly in cmd/server/hooks.go) and
// narrows it via GitConstructionPorts.
// ---------------------------------------------------------------------------

// NewGitLocalConstructionTransitionAccess builds the LOCAL git
// constructionTransitionAccess port (composegen variant token GitLocal).
func NewGitLocalConstructionTransitionAccess(repoURL string) ConstructionTransitionAccess {
	t, _, _ := GitConstructionPorts(NewGitLocalProjectStateAccess(repoURL))
	return t
}

// NewGitLocalGitActivityStatusAccess builds the LOCAL git gitActivityStatusAccess
// port (composegen variant token GitLocal).
func NewGitLocalGitActivityStatusAccess(repoURL string) GitActivityStatusAccess {
	_, s, _ := GitConstructionPorts(NewGitLocalProjectStateAccess(repoURL))
	return s
}

// NewGitHubConstructionTransitionAccess builds the CLOUD git
// constructionTransitionAccess port (composegen variant token GitHub).
func NewGitHubConstructionTransitionAccess(webHost, account string, catalog ProjectCatalog, minter CredentialMinter) (ConstructionTransitionAccess, error) {
	psa, err := NewGitHubProjectStateAccess(webHost, account, catalog, minter)
	if err != nil {
		return nil, err
	}
	t, _, _ := GitConstructionPorts(psa)
	return t, nil
}

// NewGitHubGitActivityStatusAccess builds the CLOUD git gitActivityStatusAccess
// port (composegen variant token GitHub).
func NewGitHubGitActivityStatusAccess(webHost, account string, catalog ProjectCatalog, minter CredentialMinter) (GitActivityStatusAccess, error) {
	psa, err := NewGitHubProjectStateAccess(webHost, account, catalog, minter)
	if err != nil {
		return nil, err
	}
	_, s, _ := GitConstructionPorts(psa)
	return s, nil
}

// cloudPerProjectRepoURL composes the clone URL of a project's per-project repo in the
// CLOUD profile. Under NAME-AS-IDENTITY (C-PA-AD, 2026-06-15) the project identity IS the
// (user-supplied) repo name, so the URL is <webHost>/<account>/<name>.git — the old
// "aiarch-<id>" prefix is DROPPED. This MUST agree with the repo-name the per-project
// credential is scoped to (the credential minter scopes the installation token to
// <account>/<name>.git), so the locator (this URL) and the credential scope address the
// SAME adopted repo verbatim.
func cloudPerProjectRepoURL(webHost, account, name string) string {
	return fmt.Sprintf("%s/%s/%s.git", webHost, account, name)
}

// ---------------------------------------------------------------------------
// CredentialMinter — the port the adapter mints per-call credentials through.
// ---------------------------------------------------------------------------

// CredentialMinter mints the per-project RepoCredential the GitStore's head-state
// writes/reads need. LOCAL returns the no-op local credential; CLOUD mints via the
// sourcecontrol-backed minter the composition root supplies. It is a PORT (not a sibling
// RA the store calls) — no sideways edge.
type CredentialMinter interface {
	// CredentialFor mints the credential a per-project verb runs under.
	CredentialFor(ctx context.Context, projectID ProjectID) (RepoCredential, error)
	// CatalogCredential mints the credential ListProjects runs under (the enumeration +
	// its N+1 per-project head-state reads). CLOUD: an installation-scoped token covering
	// every project repo. LOCAL: the no-op local credential.
	CatalogCredential(ctx context.Context) (RepoCredential, error)
}

// localCredentialMinter is the LOCAL on-disk-git profile: every project repo resolves to
// the trivially-valid local credential, no GitHub.
type localCredentialMinter struct{}

func (localCredentialMinter) CredentialFor(context.Context, ProjectID) (RepoCredential, error) {
	return LocalRepoCredential(), nil
}

func (localCredentialMinter) CatalogCredential(context.Context) (RepoCredential, error) {
	return LocalRepoCredential(), nil
}

// ---------------------------------------------------------------------------
// projectStateGitAdapter — the cred-binding adapter over *GitStore.
// ---------------------------------------------------------------------------

// projectStateGitAdapter binds a CredentialMinter over the cred-threaded *GitStore and
// presents the no-cred ProjectStateAccess the design Managers consume. Each verb mints the
// appropriate-scoped credential just-in-time and injects it: per-project verbs use
// CredentialFor(projectID); the catalog read (ListProjects) uses CatalogCredential.
type projectStateGitAdapter struct {
	store  *GitStore
	minter CredentialMinter
}

var _ ProjectStateAccess = (*projectStateGitAdapter)(nil)

func (a *projectStateGitAdapter) CreateProject(rc fwra.Context, projectID ProjectID, owner OwnerScope, name string) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.CreateProject(ctx, projectID, owner, name, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) ListProjects(rc fwra.Context, owner OwnerScope) ([]ProjectSummary, error) {
	ctx := rc.Context
	// Discover-by-enumeration: the catalog seam (wired into the GitStore via WithCatalog)
	// enumerates the project repos itself; the per-project head-state reads inside
	// ListProjects mint their own per-project credential through this same minter. Pass the
	// owner's catalog credential (cloud: a token scoped to the installation; local: no-op).
	cred, err := a.catalogCredential(ctx)
	if err != nil {
		return nil, err
	}
	return a.store.ListProjects(ctx, owner, cred)
}

// catalogCredential mints the credential the ListProjects enumeration + its per-project
// head-state reads run under. LOCAL is the no-op local credential. CLOUD mints an
// installation-scoped token (the project-repo reads inside ListProjects share the same
// installation token — the App installation spans every project repo under the account).
func (a *projectStateGitAdapter) catalogCredential(ctx context.Context) (RepoCredential, error) {
	return a.minter.CatalogCredential(ctx)
}

func (a *projectStateGitAdapter) CommitArtifact(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.CommitArtifact(ctx, projectID, expectedVersion, kind, cred, rc.IdempotencyKey)
}

// Compile-time proof the git adapter also serves the commit-provenance capability
// (PM-P2-4): the design Managers record committedAt/approvedBy/draftedBy atomically with
// the commit.
var _ provenanceCommitter = (*projectStateGitAdapter)(nil)

// CommitArtifactWithProvenance is the provenance-recording Commit (PM-P2-4): the cred is
// minted just-in-time, exactly like the no-cred CommitArtifact, and the acting/rail identity
// threaded from the manager approve→commit path is stamped onto the committed slot.
func (a *projectStateGitAdapter) CommitArtifactWithProvenance(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, approvedBy, draftedBy string) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.CommitArtifactWithProvenance(ctx, projectID, expectedVersion, kind, approvedBy, draftedBy, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) AdvancePhase(rc fwra.Context, projectID ProjectID, expectedVersion Version) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.AdvancePhase(ctx, projectID, expectedVersion, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) SetResearchInput(rc fwra.Context, projectID ProjectID, expectedVersion Version, research ResearchInput) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.SetResearchInput(ctx, projectID, expectedVersion, research, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) SetOperatingModel(rc fwra.Context, projectID ProjectID, expectedVersion Version, model OperatingModel) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.SetOperatingModel(ctx, projectID, expectedVersion, model, cred, rc.IdempotencyKey)
}

func (a *projectStateGitAdapter) ReadProject(rc fwra.Context, projectID ProjectID) (Project, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return Project{}, err
	}
	return a.store.ReadProject(rc, projectID, cred)
}

// ReadProjectVersion serves the cheap version-only read over the git substrate. The git
// head-state read still hydrates the whole project.json blob, but the verb keeps the
// Manager↔RA contract honest: the Temporal Activity returns only the uint64 Version across
// the boundary. Absence stays fwra.NotFound via the underlying ReadProject.
func (a *projectStateGitAdapter) ReadProjectVersion(rc fwra.Context, projectID ProjectID) (Version, error) {
	p, err := a.ReadProject(rc, projectID)
	if err != nil {
		return 0, err
	}
	return p.Version, nil
}

// ReadProjectOnBranch is the branch-aware read-back (I-DESIGN-DISPATCH §2a). An empty
// branch reads the default/main exactly as ReadProject; a non-empty branch reads the
// not-yet-merged draft on the session branch. The cred is minted just-in-time.
func (a *projectStateGitAdapter) ReadProjectOnBranch(rc fwra.Context, projectID ProjectID, branch string) (Project, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return Project{}, err
	}
	return a.store.ReadProjectOnBranch(ctx, projectID, branch, cred)
}

// StageArtifactForReviewOnBranch is the branch-aware AwaitingReview thin-write
// (I-DESIGN-DISPATCH §2a): an empty branch behaves exactly as StageArtifactForReview
// (main); a non-empty branch lands the staged-slot status flip on the session branch the
// draft lives on.
func (a *projectStateGitAdapter) StageArtifactForReviewOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, model ArtifactModel, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.StageArtifactForReviewOnBranch(ctx, projectID, expectedVersion, branch, model, cred, idempotencyKey)
}

// WithdrawArtifactOnBranch is the branch-aware Withdraw (I-DESIGN-DISPATCH §2a): an empty
// branch behaves exactly as WithdrawArtifact (main); a non-empty branch lands the Withdrawn
// status flip + notes on the session branch the draft was staged on. The cred is minted
// just-in-time.
func (a *projectStateGitAdapter) WithdrawArtifactOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.WithdrawArtifactOnBranch(ctx, projectID, expectedVersion, branch, kind, notes, cred, idempotencyKey)
}

// RejectArtifactOnBranchWithComments is the review-ledger Reject: it lands the Rejected
// status flip + notes AND appends the reviewer's comments to the slot's durable ReviewThread
// in one atomic commit on the session branch (empty branch ⇒ main). The cred is minted
// just-in-time.
func (a *projectStateGitAdapter) RejectArtifactOnBranchWithComments(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.RejectArtifactOnBranchWithComments(ctx, projectID, expectedVersion, branch, kind, notes, round, comments, cred, idempotencyKey)
}

// SeedReviewCommentsOnBranch is the F38 amendment ledger-seed (append open comments, no
// status change). The cred is minted just-in-time, exactly like the other ledger verbs.
func (a *projectStateGitAdapter) SeedReviewCommentsOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.SeedReviewCommentsOnBranch(ctx, projectID, expectedVersion, branch, kind, round, comments, cred, idempotencyKey)
}

// SetReviewCommentStatusOnBranch applies a human status transition to one ledger entry on
// the session branch (empty branch ⇒ main). The cred is minted just-in-time.
func (a *projectStateGitAdapter) SetReviewCommentStatusOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, commentID string, status string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.SetReviewCommentStatusOnBranch(ctx, projectID, expectedVersion, branch, kind, commentID, status, cred, idempotencyKey)
}

// AcknowledgeStaleBasis clears a committed slot's StaleBasis + records the reviewer's
// "reviewed — unaffected" audit entry on main (F45). The cred is minted just-in-time.
func (a *projectStateGitAdapter) AcknowledgeStaleBasis(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, note string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.AcknowledgeStaleBasis(ctx, projectID, expectedVersion, kind, note, cred, idempotencyKey)
}

// ReconcileBranchFromMain is the branch-reconcile verb (F80c): it overlays main's slots
// (bar the session's own) onto the session-branch tip so a diverged PR becomes mergeable.
// The cred is minted just-in-time.
func (a *projectStateGitAdapter) ReconcileBranchFromMain(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	ctx := rc.Context
	cred, err := a.minter.CredentialFor(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return a.store.ReconcileBranchFromMain(ctx, projectID, expectedVersion, branch, kind, cred, idempotencyKey)
}

// ---------------------------------------------------------------------------
// RepoLocator — resolves a projectID to a per-project git store handle. It is the seam
// where the deployment profile supplies the concrete URL scheme
// (github.com/<account>/<name>.git in cloud — name-as-identity, C-PA-AD; a file:// path in
// LOCAL). A plain function-backed value, NOT a sibling RA — no sideways edge.
// ---------------------------------------------------------------------------

// gitRepoLocator builds *fwgithub.GitStore handles for per-project repos.
// perProjectRepoURL maps a projectID to its clone URL. The cross-project registry repo is
// GONE (founder ruling 2026-06-14): the catalog is discovered by enumeration.
type gitRepoLocator struct {
	branch            string
	perProjectRepoURL func(projectID ProjectID) string
}

func (l gitRepoLocator) ProjectRepo(projectID ProjectID) (*fwgithub.GitStore, error) {
	url := l.perProjectRepoURL(projectID)
	if url == "" {
		return nil, fwra.New(fwra.ContractMisuse, fmt.Sprintf("gitRepoLocator: empty repo URL for project %s", projectID))
	}
	return fwgithub.NewGitStore(url, l.branch)
}

// DescribeRepo satisfies repoDescriber: exposes the resolved repo's clone URL /
// local path for diagnostics (the project-identity guard's error text). In the
// LOCAL profile this is the ONE fixed on-disk repo every projectID resolves to
// (perProjectRepoURL ignores its argument there) — exactly the identifier a human
// needs to find the corrupted repo after a cross-project write was refused.
func (l gitRepoLocator) DescribeRepo(projectID ProjectID) string {
	return l.perProjectRepoURL(projectID)
}

// ProjectRepoOnBranch satisfies BranchRepoLocator (I-DESIGN-DISPATCH §2a): it binds the
// per-project GitStore handle to a CALLER-SUPPLIED branch instead of the locator's default.
// The design Managers thread the SESSION BRANCH here so the read-back + the AwaitingReview
// thin-write ride over the branch the Action committed the draft on; after merge they pass
// "" (the default ProjectRepo, main).
func (l gitRepoLocator) ProjectRepoOnBranch(projectID ProjectID, branch string) (*fwgithub.GitStore, error) {
	if branch == "" {
		return l.ProjectRepo(projectID)
	}
	url := l.perProjectRepoURL(projectID)
	if url == "" {
		return nil, fwra.New(fwra.ContractMisuse, fmt.Sprintf("gitRepoLocator: empty repo URL for project %s", projectID))
	}
	return fwgithub.NewGitStore(url, branch)
}

// ---------------------------------------------------------------------------
// ProjectCatalog — the discover-by-enumeration seam ListProjects consumes. The LOCAL
// profile's catalog needs no GitHub and lives here; the CLOUD catalog is a
// sourcecontrol-backed port supplied by the composition root.
// ---------------------------------------------------------------------------

// localProjectCatalog enumerates the LOCAL on-disk-git profile's project repos. In the
// local/embedded profile there is no GitHub installation API; the local substrate is a
// single fixed per-project repo URL (the embedded profile drives ONE project at a time
// through the wedge). The catalog reads the one known repo's project.json head and yields
// its id+title if present. scanRepoURL is the file:// URL of that repo.
type localProjectCatalog struct {
	repoURL string
	branch  string
}

func (c localProjectCatalog) ListProjectRepos(ctx context.Context, _ OwnerScope, cred RepoCredential) ([]ProjectCatalogRef, error) {
	if c.repoURL == "" {
		return nil, nil
	}
	store, err := fwgithub.NewGitStore(c.repoURL, c.branch)
	if err != nil {
		return nil, err
	}
	snap, err := store.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		return nil, err
	}
	raw, ok := snap.Files["project.json"]
	if !ok {
		return nil, nil // repo exists but no project committed yet — empty catalog
	}
	var doc struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if jerr := json.Unmarshal(raw, &doc); jerr != nil {
		return nil, jerr
	}
	if doc.ID == "" {
		return nil, nil // committed project.json with no id — nothing to surface
	}
	_ = cred
	// NAME-AS-IDENTITY (C-PM-Δ): the stored id IS the project identity (the repo name),
	// carried verbatim — no uuid.Parse.
	return []ProjectCatalogRef{{ProjectID: ProjectID(doc.ID), Title: doc.Name}}, nil
}

// gitconstruction.go is the git-substrate realization of the additive Phase-3
// construction-transition verbs (constructionManager.md §5.3; see construction.go
// for the Postgres-era port + rationale). The git GitStore satisfies the SAME
// ConstructionTransitionAccess facet — re-cut with the Manager-threaded
// `cred RepoCredential` (REWORK.4) the substrate swap forces, exactly as the
// Phase-1/2 verbs are. v1 records the transition through the shared ref-CAS +
// in-repo-dedup applyMutation path so it is durable and replay-idempotent; the
// richer per-activity head-state status aggregate is populated from Task 4.

// PhaseArtifactPayload is a tagged union of all phase artifact types.
// Exactly one field should be set. RecordPhaseArtifactProduced inspects which
// field is non-zero and routes it to the right map in PhaseArtifacts or
// TestingState. mapKey is the component/surface/resource/doc name used as the
// map key for PhaseArtifacts fields; it is unused for pointer-scalar TestingState
// fields (SystemTestPlan, HarnessModule, PerfHarness, QualityAuditReport).
// Convention: exactly one field per call. This is NOT enforced at runtime (multiple
// set fields will route all of them). A typed sum type would enforce the invariant,
// which now may be feasible since cs.Type/cs.Variant are populated by seed-construction.
type PhaseArtifactPayload struct {
	// PhaseArtifacts fields (keyed by mapKey)
	SRS              *SRSRecord
	TestPlan         *TestPlanRecord
	IntegrationNote  *IntegrationNoteRecord
	UXRequirements   *UXRequirementsRecord
	UIDesign         *UIDesignRecord
	ProvisioningSpec *ProvisioningSpecRecord
	DeployNote       *DeployNoteRecord
	DocOutline       *DocOutlineRecord
	DocNote          *DocNoteRecord
	// TestingState fields (project-level singletons / slices)
	SystemTestPlan *SystemTestPlan
	HarnessModule  *HarnessModule
	PerfHarness    *PerfHarness
	QualityGate    *QualityGate
	TestRun        *TestRun
	Defect         *DefectRecord
	// QualityAuditReport replaces the string in TestingState when non-empty.
	QualityAuditReport string
}

// RecordChangeReviewed records the review transition for activityID by setting
// BuildStatus = BuildInReview. Uses modeRequireExisting (project row exists by
// Phase 3, same discipline as gitactivity.go verbs).
func (s *GitStore) RecordChangeReviewed(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordChangeReviewed: empty activityID")
	}
	return s.applyMutation(rc.Context, "RecordChangeReviewed", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			cs.BuildStatus = BuildInReview
		})
		return nil
	})
}

// RecordActivityExited records the binary activity exit for activityID. On
// ActivityOutcomeCompleted: Phase = ActivityConstructionDone, BuildStatus =
// BuildIntegrated. On other outcomes: Phase = ActivityConstructionDone,
// BuildStatus = BuildInReview (skipped/taken-over land done but not integrated).
// CompletedAt is server-resolved if not already set.
func (s *GitStore) RecordActivityExited(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, outcome ActivityOutcome, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityExited: empty activityID")
	}
	now := s.now()
	return s.applyMutation(rc.Context, "RecordActivityExited", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			cs.Phase = ActivityConstructionDone
			if cs.CompletedAt == nil {
				t := now
				cs.CompletedAt = &t
			}
			switch outcome {
			case ActivityOutcomeCompleted:
				cs.BuildStatus = BuildIntegrated
			case ActivityOutcomeUnknown, ActivityOutcomeSkipped, ActivityOutcomeTakenOver:
				// Skipped / TakenOver (and the zero-value Unknown, which should never
				// reach here but is handled the same defensively): activity is done
				// but was not reviewed+integrated. Same as the default below.
				cs.BuildStatus = BuildInReview
			default:
				// Skipped / TakenOver: activity is done but was not reviewed+integrated.
				cs.BuildStatus = BuildInReview
			}
		})
		return nil
	})
}

// RecordActivityFailed records the TERMINAL-FAILURE binary exit for activityID. It
// MIRRORS RecordActivityExited exactly (same applyMutation + idempotency ledger +
// ref-CAS pattern) but lands a distinct terminal: Phase = ActivityConstructionFailed,
// BuildStatus = BuildFailed, CompletedAt server-resolved, and the FailureReason +
// FailureDetail recorded so the console can explain WHY the activity is no longer
// pending. This is what stops a cancelled/failed/timed-out GH-Actions run (or an
// exhausted variance budget / unanswered escalation) from leaving the activity stuck
// Running forever.
func (s *GitStore) RecordActivityFailed(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, reason FailureReason, detail string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityFailed: empty activityID")
	}
	now := s.now()
	return s.applyMutation(rc.Context, "RecordActivityFailed", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			cs.Phase = ActivityConstructionFailed
			cs.BuildStatus = BuildFailed
			if cs.CompletedAt == nil {
				t := now
				cs.CompletedAt = &t
			}
			cs.FailureReason = reason
			cs.FailureDetail = detail
		})
		return nil
	})
}

// RecordOperatorPaused records the operator-paused head-state transition by
// setting Project.OperatorPaused = true and Project.PauseReason = reason.
func (s *GitStore) RecordOperatorPaused(rc fwra.Context, projectID ProjectID, expectedVersion Version, reason string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(rc.Context, "RecordOperatorPaused", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		p.OperatorPaused = true
		p.PauseReason = reason
		return nil
	})
}

// RecordReviewPolicy persists the per-project ReviewPolicy by setting
// Project.ReviewPolicy = policy.
func (s *GitStore) RecordReviewPolicy(rc fwra.Context, projectID ProjectID, expectedVersion Version, policy ReviewPolicy, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(rc.Context, "RecordReviewPolicy", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		p.ReviewPolicy = policy
		return nil
	})
}

// RecordPhaseStarted records that activityID's construction agent has entered the
// given phase. It seeds the Phases slice from phaseSetFor if not yet populated,
// sets CurrentPhase = phase, and advances the coarse Phase to Running (if not
// already Done).
func (s *GitStore) RecordPhaseStarted(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, phase ActivityMethodPhase, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordPhaseStarted: empty activityID")
	}
	if phase == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordPhaseStarted: empty phase")
	}
	return s.applyMutation(rc.Context, "RecordPhaseStarted", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			if len(cs.Phases) == 0 {
				cs.Phases = phaseSetFor(cs.Type, cs.Variant)
			}
			cs.CurrentPhase = phase
			if cs.Phase != ActivityConstructionDone {
				cs.Phase = ActivityConstructionRunning
			}
		})
		return nil
	})
}

// RecordPhaseCompleted marks the given phase Completed = true, records the
// server-resolved CompletedAt, and optionally sets ArtifactRef. It recomputes the
// coarse Phase via CoarsePhase over the updated Phases slice so the tracker
// advances atomically with the phase completion.
func (s *GitStore) RecordPhaseCompleted(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, phase ActivityMethodPhase, artifactRef string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) { //nolint:gocognit // phase transition requires checking all phase states
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordPhaseCompleted: empty activityID")
	}
	if phase == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordPhaseCompleted: empty phase")
	}
	now := s.now()
	return s.applyMutation(rc.Context, "RecordPhaseCompleted", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			applyPhaseCompletion(cs, phase, artifactRef, now)
		})
		return nil
	})
}

// applyPhaseCompletion marks the matching phase entry Completed, sets CompletedAt and
// optionally ArtifactRef, then recomputes the coarse Phase.
func applyPhaseCompletion(cs *ActivityConstructionStatus, phase ActivityMethodPhase, artifactRef string, now time.Time) {
	if len(cs.Phases) == 0 {
		cs.Phases = phaseSetFor(cs.Type, cs.Variant)
	}
	for i := range cs.Phases {
		if cs.Phases[i].Phase == phase {
			t := now
			cs.Phases[i].Completed = true
			cs.Phases[i].CompletedAt = &t
			if artifactRef != "" {
				cs.Phases[i].ArtifactRef = artifactRef
			}
			break
		}
	}
	// GUARD: a stored terminal-failure Phase is sticky — never recompute it back to
	// Running/Done from the Phases slice (a late phase-completion record after a
	// RecordActivityFailed must not resurrect the activity). BuildFailed is likewise
	// preserved.
	if cs.Phase == ActivityConstructionFailed || cs.BuildStatus == BuildFailed {
		return
	}
	cs.Phase = CoarsePhase(cs.Phases)
}

// RecordServiceContractProduced writes the typed ServiceContract for component
// into Project.ServiceContracts, lazy-allocating the map on first write.
func (s *GitStore) RecordServiceContractProduced(rc fwra.Context, projectID ProjectID, expectedVersion Version, component string, contract ServiceContract, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if component == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordServiceContractProduced: empty component")
	}
	return s.applyMutation(rc.Context, "RecordServiceContractProduced", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		if p.ServiceContracts == nil {
			p.ServiceContracts = make(map[string]ServiceContract)
		}
		p.ServiceContracts[component] = contract
		return nil
	})
}

// RecordPhaseArtifactProduced writes the typed artifact carried in payload into
// the correct PhaseArtifacts / TestingState slot of the Project aggregate.
// mapKey is used as the per-component/surface/resource/doc map key for
// PhaseArtifacts fields; it is unused for singleton TestingState fields.
// Exactly one payload field should be set; if none is set the verb is a no-op
// (idempotent empty payload is tolerated — the ledger dedup will still fire).
func (s *GitStore) RecordPhaseArtifactProduced(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, mapKey string, payload PhaseArtifactPayload, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordPhaseArtifactProduced: empty activityID")
	}
	return s.applyMutation(rc.Context, "RecordPhaseArtifactProduced", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		applyPhaseArtifactPayload(p, mapKey, payload)
		return nil
	})
}

// ApplyPhaseArtifactPayload routes a phase-artifact / testing-state payload into the
// Project aggregate — the pure, I/O-free core of RecordPhaseArtifactProduced, exported
// so the aiarch-state MCP construction verbs (cmd/aiarch-state-mcp) reuse the SAME
// routing the server RA uses. One source of truth for which payload field lands in
// which PhaseArtifacts / TestingState slot; the caller validates the mutated aggregate
// through the codec + methodcheck exactly as the server does.
func ApplyPhaseArtifactPayload(p *Project, mapKey string, payload PhaseArtifactPayload) {
	applyPhaseArtifactPayload(p, mapKey, payload)
}

// applyPhaseArtifactPayload routes the payload to the correct Project field.
// It is a pure function (no I/O) extracted for testability.
func applyPhaseArtifactPayload(p *Project, mapKey string, payload PhaseArtifactPayload) {
	applyPhaseArtifactsPayload(p, mapKey, payload)
	applyTestingStatePayload(p, payload)
}

// applyPhaseArtifactsPayload routes the PhaseArtifacts-group fields (keyed by
// mapKey) from the payload into p.PhaseArtifacts, lazy-allocating as needed.
// Split into two halves to keep each helper under the funlen threshold.
func applyPhaseArtifactsPayload(p *Project, mapKey string, payload PhaseArtifactPayload) {
	applyPhaseArtifactsSpecDesign(p, mapKey, payload)
	applyPhaseArtifactsDeployDoc(p, mapKey, payload)
}

// ensurePhaseArtifacts lazy-inits p.PhaseArtifacts and returns it.
func ensurePhaseArtifacts(p *Project) *PhaseArtifacts {
	if p.PhaseArtifacts == nil {
		p.PhaseArtifacts = &PhaseArtifacts{}
	}
	return p.PhaseArtifacts
}

// applyPhaseArtifactsSpecDesign handles the spec/design half of PhaseArtifacts
// fields: SRS, TestPlan, IntegrationNote, UXRequirements, UIDesign.
func applyPhaseArtifactsSpecDesign(p *Project, mapKey string, payload PhaseArtifactPayload) { //nolint:gocyclo // exhaustive nil-check per phase artifact field
	if payload.SRS != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.SRS == nil {
			pa.SRS = make(map[string]SRSRecord)
		}
		pa.SRS[mapKey] = *payload.SRS
	}
	if payload.TestPlan != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.TestPlan == nil {
			pa.TestPlan = make(map[string]TestPlanRecord)
		}
		pa.TestPlan[mapKey] = *payload.TestPlan
	}
	if payload.IntegrationNote != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.IntegrationNote == nil {
			pa.IntegrationNote = make(map[string]IntegrationNoteRecord)
		}
		pa.IntegrationNote[mapKey] = *payload.IntegrationNote
	}
	if payload.UXRequirements != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.UXRequirements == nil {
			pa.UXRequirements = make(map[string]UXRequirementsRecord)
		}
		pa.UXRequirements[mapKey] = *payload.UXRequirements
	}
	if payload.UIDesign != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.UIDesign == nil {
			pa.UIDesign = make(map[string]UIDesignRecord)
		}
		pa.UIDesign[mapKey] = *payload.UIDesign
	}
}

// applyPhaseArtifactsDeployDoc handles the infra/doc half of PhaseArtifacts
// fields: ProvisioningSpec, DeployNote, DocOutline, DocNote.
func applyPhaseArtifactsDeployDoc(p *Project, mapKey string, payload PhaseArtifactPayload) {
	if payload.ProvisioningSpec != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.ProvisioningSpec == nil {
			pa.ProvisioningSpec = make(map[string]ProvisioningSpecRecord)
		}
		pa.ProvisioningSpec[mapKey] = *payload.ProvisioningSpec
	}
	if payload.DeployNote != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.DeployNote == nil {
			pa.DeployNote = make(map[string]DeployNoteRecord)
		}
		pa.DeployNote[mapKey] = *payload.DeployNote
	}
	if payload.DocOutline != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.DocOutline == nil {
			pa.DocOutline = make(map[string]DocOutlineRecord)
		}
		pa.DocOutline[mapKey] = *payload.DocOutline
	}
	if payload.DocNote != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.DocNote == nil {
			pa.DocNote = make(map[string]DocNoteRecord)
		}
		pa.DocNote[mapKey] = *payload.DocNote
	}
}

// applyTestingStatePayload routes the TestingState-group fields (project-level
// singletons and append-slices) from the payload into p.TestingState,
// lazy-allocating as needed.
func applyTestingStatePayload(p *Project, payload PhaseArtifactPayload) {
	ensureTestingState := func() *TestingState {
		if p.TestingState == nil {
			p.TestingState = &TestingState{}
		}
		return p.TestingState
	}
	if payload.SystemTestPlan != nil {
		ensureTestingState().SystemTestPlan = payload.SystemTestPlan
	}
	if payload.HarnessModule != nil {
		ensureTestingState().HarnessModule = payload.HarnessModule
	}
	if payload.PerfHarness != nil {
		ensureTestingState().PerfHarness = payload.PerfHarness
	}
	if payload.QualityGate != nil {
		ts := ensureTestingState()
		ts.QualityGates = append(ts.QualityGates, *payload.QualityGate)
	}
	if payload.TestRun != nil {
		ts := ensureTestingState()
		ts.TestRuns = append(ts.TestRuns, *payload.TestRun)
	}
	if payload.Defect != nil {
		ts := ensureTestingState()
		ts.Defects = append(ts.Defects, *payload.Defect)
	}
	if payload.QualityAuditReport != "" {
		ensureTestingState().QualityAuditReport = payload.QualityAuditReport
	}
}

// The Phase-3 construction-transition surface is ConstructionTransitionAccess,
// satisfied by the git store (gitconstruction.go).

// String returns the canonical name for the outcome.
func (o ActivityOutcome) String() string {
	switch o {
	case ActivityOutcomeCompleted:
		return "Completed"
	case ActivityOutcomeSkipped:
		return "Skipped"
	case ActivityOutcomeTakenOver:
		return "TakenOver"
	case ActivityOutcomeUnknown:
		return "Unknown"
	}
	// Unreachable for the four defined ActivityOutcome values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a defensive
	// fallback for an out-of-range ordinal.
	return "Unknown"
}

// reconcile.go holds the DETERMINISTIC project.json reconciliation the design rail uses
// when a session branch has diverged from main (F80).
//
// A design session branch is AwaitingReview while main can keep advancing (staleness
// acknowledgements, question seeds, a sibling artifact committing) — every one of those
// touches the SAME single file, .aiarch/state/project.json. A plain `git merge` of main
// into the session branch then conflicts on that file, which today dead-ends the
// redraft/answer refresh job (a RED merge-conflict) and leaves the PR mergeable_state=
// dirty so the approve-time merge cannot complete.
//
// The resolution is deterministic because project.json is a SERVER-OWNED,
// SINGLE-WRITER-PER-SLOT document: a design session legitimately owns exactly ONE slot —
// the artifact kind it is authoring — and never writes any other slot. So the reconciled
// document is unambiguously main's document (which carries every OTHER slot's latest
// committed content) with the session's OWN slot overlaid from the session-branch
// document. No content is guessed and no human merge is needed.

// ReconcileSlotOntoBase returns the reconciled project.json for a diverged design session
// branch: the `base` document (main's latest project.json) with the session's OWN slot —
// the one named by `kind` — overlaid from the `ours` document (the session-branch
// project.json). It is the single deterministic resolver shared by the workflow refresh
// step (via the aiarch-state-mcp `reconcile` subcommand) and the approve-time merge
// window (via the server's branch-reconcile activity), so both paths reconcile
// identically.
//
// The overlay is the WHOLE ArtifactSlot for `kind` (status, model, notes, the PM-critique
// carrier, the durable review thread, revisions, staleBasis): the session branch is the
// authoritative home of the in-flight draft AND its review ledger, so its slot wins
// wholesale, while every other slot comes from `base` (which may have advanced). Both
// documents are decoded and the result re-encoded through the SAME strict server codec,
// so a reconciled document is byte-for-byte what the server accepts on read-back (and it
// re-runs the F81 required-field gate over both inputs).
func ReconcileSlotOntoBase(base, ours []byte, projectID ProjectID, kind ArtifactKind) ([]byte, error) {
	baseProj, ok, err := DecodeProjectJSON(base, projectID)
	if err != nil {
		return nil, fmt.Errorf("reconcile: decode base (main) project state: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("reconcile: base (main) carried no project document")
	}
	ourProj, ok, err := DecodeProjectJSON(ours, projectID)
	if err != nil {
		return nil, fmt.Errorf("reconcile: decode session-branch project state: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("reconcile: session branch carried no project document")
	}

	baseSlot, ok := slotPtr(&baseProj, kind)
	if !ok {
		return nil, fmt.Errorf("reconcile: no slot for artifact kind %s", kind)
	}
	ourSlot, ok := slotPtr(&ourProj, kind)
	if !ok {
		return nil, fmt.Errorf("reconcile: no slot for artifact kind %s", kind)
	}
	// Overlay the session's OWN slot onto main's document. Every other slot is left as
	// main has it (the whole point: pick up main's concurrent advances), and this slot
	// takes the session-branch value wholesale (the in-flight draft + its review ledger).
	*baseSlot = *ourSlot

	return EncodeProjectJSON(baseProj)
}

// OverlaySlotFromBranchOntoMain is the in-memory twin of ReconcileSlotOntoBase for the
// server's branch-reconcile activity, which already holds decoded Projects (read via
// ReadProject / ReadProjectOnBranch) rather than raw bytes. It mutates `mainProj` in
// place, overlaying the session's OWN slot (kind) from `branchProj`, so the caller can
// commit the reconciled aggregate to the branch tip through the normal branch write path.
func OverlaySlotFromBranchOntoMain(mainProj *Project, branchProj *Project, kind ArtifactKind) error {
	mainSlot, ok := slotPtr(mainProj, kind)
	if !ok {
		return fmt.Errorf("reconcile: no slot for artifact kind %s", kind)
	}
	branchSlot, ok := slotPtr(branchProj, kind)
	if !ok {
		return fmt.Errorf("reconcile: no slot for artifact kind %s", kind)
	}
	*mainSlot = *branchSlot
	return nil
}

// Provenance is the ADDITIVE commit-provenance record for a committed artifact
// slot (PM-P2-4). It records WHO committed and WHEN, captured at the rail's approve→commit
// transition, so the read model can render "committed <date> · approved by X · drafted by Y"
// under the committed strip.
//
// It follows the staleBasisCause pattern (a19a25b + a9867cf) exactly:
//   - omitempty everywhere: an uncommitted slot carries no provenance, and every field is
//     independently optional so a commit with no acting identity still records committedAt.
//   - NO back-fill: a slot committed BEFORE this field existed reads back with a nil
//     Provenance. Absent provenance is allowed — the read model simply omits the line.
//   - The record is refreshed on every commit (a re-commit / amendment restamps it with the
//     new commit's committedAt + acting identity).
type Provenance struct {
	// CommittedAt is the RFC3339 wall-clock instant the commit landed, server-resolved from
	// the store's clock at commit time (RA code, time.Now() is fine). Always present on a
	// provenance-recorded commit.
	CommittedAt string `json:"committedAt,omitempty"`
	// ApprovedBy is a human-facing label for the acting identity that approved the commit
	// (the reviewer's username / email / subject, derived from the caller's security.Principal
	// at the manager boundary and threaded down). Empty when no identity reached the commit
	// path (e.g. a dev-mode zero principal) — absence is allowed.
	ApprovedBy string `json:"approvedBy,omitempty"`
	// DraftedBy is a human-facing label for the drafting agent/rail that produced the draft
	// (v1: the agentic design rail identity, plus the amendment-session marker when known).
	// Empty on a substrate that records no rail identity.
	DraftedBy string `json:"draftedBy,omitempty"`
}

// AllArtifactKinds returns every defined ArtifactKind in the stable slot order
// (Phase 1 then Phase 2). This is the single enumeration of the closed set —
// used by codecs and the coverage test to ensure a new kind added to the iota
// block is also covered by NewModelForKind.
func AllArtifactKinds() []ArtifactKind {
	return []ArtifactKind{
		// Phase 1
		KindMission,
		KindGlossary,
		KindScrubbedRequirements,
		KindVolatilities,
		KindCoreUseCases,
		KindSystem,
		KindOperationalConcepts,
		KindStandardCheck,
		// Phase 2
		KindPlanningAssumptions,
		KindActivityList,
		KindNetwork,
		KindNormalSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
		KindDecompressedSolution,
		KindRiskModel,
		KindSdpReview,
	}
}

// NewModelForKind returns a freshly-allocated zero-value concrete pointer that
// implements ArtifactModel for kind, suitable for JSON unmarshalling into. For
// the four Solution slot kinds (KindNormalSolution, KindSubcriticalSolution,
// KindCompressedSolution, KindDecompressedSolution) the returned *Solution has
// SlotKind pre-set to kind so the slot identity is preserved across a codec
// round-trip even if the persisted JSON omits the field.
//
// Returns (nil, false) for any kind not in AllArtifactKinds().
// This is the canonical factory: both the Temporal payload codec and the JSONB
// codec delegate here so a new ArtifactKind missed from this switch is caught
// by TestNewModelForKindCoversAllKinds rather than silently crashing at runtime.
func NewModelForKind(kind ArtifactKind) (ArtifactModel, bool) {
	switch kind {
	case KindMission:
		return &MissionStatement{}, true
	case KindGlossary:
		return &Glossary{}, true
	case KindScrubbedRequirements:
		return &ScrubbedRequirements{}, true
	case KindVolatilities:
		return &Volatilities{}, true
	case KindCoreUseCases:
		return &CoreUseCases{}, true
	case KindSystem:
		return &System{}, true
	case KindOperationalConcepts:
		return &DeploymentOperationsModel{}, true
	case KindStandardCheck:
		return &StandardCheck{}, true
	case KindPlanningAssumptions:
		return &PlanningAssumptions{}, true
	case KindActivityList:
		return &ActivityList{}, true
	case KindNetwork:
		return &Network{}, true
	case KindNormalSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
		KindDecompressedSolution:
		return &Solution{SlotKind: kind}, true
	case KindRiskModel:
		return &RiskModel{}, true
	case KindSdpReview:
		return &SdpReview{}, true
	default:
		return nil, false
	}
}

// ProjectID is the project aggregate identifier — NAME-AS-IDENTITY: a string
// DEFINED type whose value IS the user-supplied adopted repo name (project name ==
// repo name, server-resolved), threaded verbatim through adopt → seat →
// createProject and round-tripped as the persisted `.aiarch/state/` `id`. The
// empty string is the zero value (the "no project" sentinel). The git catalog
// enumerates by the `aiarch-project` topic and returns repo name == this identity
// (sourceControlAccess.md §10.1 Q7 — re-derivation degenerates to identity, so no
// head-state repo-ref column).

// String returns the project identity as a plain string.
func (p ProjectID) String() string { return string(p) }

// Version is the optimistic-concurrency token: per-aggregate mutation count.
// 0 == no row yet. Bumped by one on each successful write verb. NOT a row id or
// timestamp. (projectStateAccess.md §3.0)

// OwnerScope identifies the owning principal of a project (e.g. the subject or
// email of the authenticated user). It scopes the project catalog so ListProjects
// returns only the rows a principal owns. A plain string newtype — the RA stores
// it verbatim and never interprets it. (Task 2.3)

// ComponentID is the stable identifier for a System component.
//
// NAME-AS-IDENTITY (founder decision 2026-06-04): a Component's identity is its
// human-readable Name, carried as a JSONPath/React-key-safe SLUG string. The
// server assigns ComponentID = Slug(Component.Name) in the LLM-draft finalize
// pass (systemdesign.finalize*); the LLM never emits an id. Cross-references
// (Relationship.From/To, a DynamicView step's call endpoints,
// DeployContainer.Components) carry this same slug. It is a plain string
// alias — validators use it as an opaque map key and format it directly; the
// persisted/served `id` field and the webApp JSONPath anchors ($.components[id=…])
// are unchanged in SHAPE (still a string `id`), only the VALUE moved from a uuid
// to a name-slug.
type ComponentID = string

// UseCaseID is the stable identifier for a UseCase — see ComponentID. The server
// assigns UseCaseID = Slug(UseCase.Name); DynamicView.UseCaseID and
// UseCase.VariationOf carry this slug. A plain string alias.
type UseCaseID = string

// slugNonAlnum collapses every run of non-alphanumeric characters into a single
// hyphen for Slug.
var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Slug converts a human-readable entity name into a stable, JSONPath/React-key-safe
// identity token: lowercased, non-alphanumeric runs collapsed to single hyphens,
// leading/trailing hyphens trimmed. It is the deterministic name→id function used
// by the systemDesign finalize pass to assign Component/UseCase/Actor/ActivityNode
// identities from the names the LLM authored (founder decision 2026-06-04:
// name-as-identity, no UUIDs). A name that slugs to "" (e.g. all punctuation)
// yields "" — the finalize pass treats that as an actionable error.
func Slug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// ArtifactKind discriminates the named Project slots and is used by the generic
// write verbs (commit/reject/withdraw/advancePhase) and the ArtifactModel sealed sum.
// One constant per slot defined across §3.3 and §3.4.
// (projectStateAccess.md §3.1)
//
// NOTE — vocabulary coexistence: this projectstate.ArtifactKind (and
// projectstate.ProjectID) is the TARGET typed-model vocabulary. The older
// internal/resourceaccess ArtifactKind (string-blob names) and ProjectID string
// are being migrated out across tasks T4–T10 and will be deleted when done —
// until then the two coexist intentionally and consumers are migrated
// package-by-package.

// artifactKindStrings backs String — a table lookup rather than a switch (the
// gocyclo-friendly form of flat enum→value dispatch; the exhaustive linter's
// map check enforces a key per variant exactly as it would enforce a case).
var artifactKindStrings = map[ArtifactKind]string{
	KindMission:              "Mission",
	KindGlossary:             "Glossary",
	KindScrubbedRequirements: "ScrubbedRequirements",
	KindVolatilities:         "Volatilities",
	KindCoreUseCases:         "CoreUseCases",
	KindSystem:               "System",
	KindOperationalConcepts:  "OperationalConcepts",
	KindStandardCheck:        "StandardCheck",
	KindPlanningAssumptions:  "PlanningAssumptions",
	KindActivityList:         "ActivityList",
	KindNetwork:              "Network",
	KindNormalSolution:       "NormalSolution",
	KindSubcriticalSolution:  "SubcriticalSolution",
	KindCompressedSolution:   "CompressedSolution",
	KindDecompressedSolution: "DecompressedSolution",
	KindRiskModel:            "RiskModel",
	KindSdpReview:            "SdpReview",
}

// String returns a stable human-readable name for the ArtifactKind.
// Used in error messages and arch-test output.
func (k ArtifactKind) String() string {
	if s, ok := artifactKindStrings[k]; ok {
		return s
	}
	// Unreachable for the 17 defined ArtifactKind values above (the exhaustive
	// linter enforces that every real variant has its own key); kept as a
	// defensive fallback for an out-of-range ordinal.
	return fmt.Sprintf("ArtifactKind(%d)", int(k))
}

// WireName returns the canonical camelCase wire name for the ArtifactKind — the
// STRING discriminator the public typed wire contract uses (the SPA reads
// {"kind":"mission","model":{…}}). This is the single source of truth for the
// kind↔name mapping; the REST DTO layer, the model envelopes, and the OpenAPI
// `ArtifactModelEnvelope.kind` enum all derive from it. Distinct from String(),
// which yields the PascalCase Go-identifier name used in error/diagnostic text.
func (k ArtifactKind) WireName() string {
	// Unknown (out-of-range) ordinals yield "" — MarshalJSON treats "" as
	// "no wire name" and errors.
	return artifactKindWireNames[k]
}

// artifactKindWireNames backs WireName — a table lookup rather than a switch
// (the gocyclo-friendly form of flat enum→value dispatch; the exhaustive
// linter's map check enforces a key per variant exactly as it would enforce a
// case).
var artifactKindWireNames = map[ArtifactKind]string{
	// ---- Phase 1 ----
	KindMission:              "mission",
	KindGlossary:             "glossary",
	KindScrubbedRequirements: "scrubbedRequirements",
	KindVolatilities:         "volatilities",
	KindCoreUseCases:         "coreUseCases",
	KindSystem:               "system",
	KindOperationalConcepts:  "operationalConcepts",
	KindStandardCheck:        "standardCheck",
	// ---- Phase 2 ----
	KindPlanningAssumptions:  "planningAssumptions",
	KindActivityList:         "activityList",
	KindNetwork:              "network",
	KindNormalSolution:       "normalSolution",
	KindSubcriticalSolution:  "subcriticalSolution",
	KindCompressedSolution:   "compressedSolution",
	KindDecompressedSolution: "decompressedSolution",
	KindRiskModel:            "riskModel",
	KindSdpReview:            "sdpReview",
}

// artifactKindByWireName is the inverse of WireName, built once from
// AllArtifactKinds so the two directions can never drift.
var artifactKindByWireName = func() map[string]ArtifactKind {
	m := make(map[string]ArtifactKind, len(AllArtifactKinds()))
	for _, k := range AllArtifactKinds() {
		m[k.WireName()] = k
	}
	return m
}()

// ArtifactKindFromWireName maps a canonical camelCase wire name back to its
// ArtifactKind. Returns (0, false) for an unrecognized name.
func ArtifactKindFromWireName(name string) (ArtifactKind, bool) {
	k, ok := artifactKindByWireName[name]
	return k, ok
}

// MarshalJSON encodes the ArtifactKind as its canonical camelCase wire name
// (a STRING discriminator), so the public typed contract reads
// {"kind":"mission",…} rather than an opaque integer ordinal.
func (k ArtifactKind) MarshalJSON() ([]byte, error) {
	name := k.WireName()
	if name == "" {
		return nil, fmt.Errorf("projectstate: ArtifactKind(%d) has no wire name", int(k))
	}
	return json.Marshal(name)
}

// UnmarshalJSON decodes the string wire name back into the ArtifactKind. For
// backward compatibility with any persisted/legacy integer-ordinal payload it
// also accepts a bare integer.
func (k *ArtifactKind) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		kind, ok := ArtifactKindFromWireName(name)
		if !ok {
			return fmt.Errorf("projectstate: %q is not a recognized ArtifactKind wire name", name)
		}
		*k = kind
		return nil
	}
	var ordinal int
	if err := json.Unmarshal(data, &ordinal); err != nil {
		return fmt.Errorf("projectstate: ArtifactKind must be a string wire name or integer ordinal: %w", err)
	}
	*k = ArtifactKind(ordinal)
	return nil
}

// IsPhase1 reports whether the kind belongs to The Method's Phase 1 (System Design)
// — the phase driven by systemDesignManager. (projectStateAccess.md §3.1)
func (k ArtifactKind) IsPhase1() bool {
	switch k {
	case KindMission,
		KindGlossary,
		KindScrubbedRequirements,
		KindVolatilities,
		KindCoreUseCases,
		KindSystem,
		KindOperationalConcepts,
		KindStandardCheck:
		return true
	case KindPlanningAssumptions, KindActivityList, KindNetwork, KindNormalSolution,
		KindSubcriticalSolution, KindCompressedSolution, KindDecompressedSolution,
		KindRiskModel, KindSdpReview:
		// Phase-2 kinds — same as the default below.
		return false
	default:
		return false
	}
}

// Phase1RequiredKinds returns the ordered set of artifact kinds that must all be
// Committed before Phase 1 can be sealed via advancePhase. Ordering follows the
// Phase-1 design sequence (projectStateAccess.md §3.1, systemDesignManager.md §1.7).
func Phase1RequiredKinds() []ArtifactKind {
	return []ArtifactKind{
		KindMission,
		KindGlossary,
		KindScrubbedRequirements,
		KindVolatilities,
		KindCoreUseCases,
		KindSystem,
		KindOperationalConcepts,
		KindStandardCheck,
	}
}

// IsPhase2 reports whether the kind belongs to The Method's Phase 2 (Project Design)
// — the phase driven by projectDesignManager. (projectStateAccess.md §3.1,
// projectDesignManager.md §2.1)
func (k ArtifactKind) IsPhase2() bool {
	switch k {
	case KindPlanningAssumptions,
		KindActivityList,
		KindNetwork,
		KindNormalSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
		KindDecompressedSolution,
		KindRiskModel,
		KindSdpReview:
		return true
	case KindMission, KindGlossary, KindScrubbedRequirements, KindVolatilities,
		KindCoreUseCases, KindSystem, KindOperationalConcepts, KindStandardCheck:
		// Phase-1 kinds — same as the default below.
		return false
	default:
		return false
	}
}

// IsSolutionKind reports whether the kind is one of the four solution-option slots
// (the four Solution models distinguished by SlotKind). (projectStateAccess.md §3.6)
func (k ArtifactKind) IsSolutionKind() bool {
	switch k {
	case KindNormalSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
		KindDecompressedSolution:
		return true
	case KindMission, KindGlossary, KindScrubbedRequirements, KindVolatilities,
		KindCoreUseCases, KindSystem, KindOperationalConcepts, KindStandardCheck,
		KindPlanningAssumptions, KindActivityList, KindNetwork, KindRiskModel, KindSdpReview:
		// Every non-solution kind (Phase-1 and the non-solution Phase-2 kinds) —
		// same as the default below.
		return false
	default:
		return false
	}
}

// Phase2RequiredKinds returns the ordered set of artifact kinds that must all be
// Committed before Phase 2 can be sealed via advanceToConstruction. Ordering follows
// the Phase-2 design sequence: planning assumptions → activity list → network → the
// four solution options → risk model → SDP review (projectDesignManager.md §2.4/§6.3,
// the-method-* Project-Design phase order).
func Phase2RequiredKinds() []ArtifactKind {
	return []ArtifactKind{
		KindPlanningAssumptions,
		KindActivityList,
		KindNetwork,
		KindNormalSolution,
		KindDecompressedSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
		KindRiskModel,
		KindSdpReview,
	}
}

// downstreamKinds returns the artifact kinds that DEPEND on kind — its transitive
// successors in the Method design DAG (F38 staleness, founder ruling 2026-07-05). When an
// artifact re-commits (an amendment), every ALREADY-committed downstream kind has its
// basis shifted and is flagged StaleBasis. The spine is a linear chain across Phase 1 then
// Phase 2 (Phase-2 artifacts depend on the whole Phase-1 design), so downstream = every
// kind strictly AFTER this one in the combined ordering — EXCEPT the four Solution options,
// which are SIBLINGS (all fed by Network, none by each other): amending one Solution does
// not stale the others, only their shared successors (RiskModel, SdpReview).
func downstreamKinds(kind ArtifactKind) []ArtifactKind {
	order := append(Phase1RequiredKinds(), Phase2RequiredKinds()...)
	idx := -1
	for i, k := range order {
		if k == kind {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	out := make([]ArtifactKind, 0, len(order)-idx-1)
	for _, k := range order[idx+1:] {
		if kind.IsSolutionKind() && k.IsSolutionKind() {
			continue // sibling solution options do not depend on each other
		}
		out = append(out, k)
	}
	return out
}

// SolutionKinds returns the four solution-option slot kinds in the SDP-review row
// order (normal, decompressed-normal, subcritical, compressed).
func SolutionKinds() []ArtifactKind {
	return []ArtifactKind{
		KindNormalSolution,
		KindDecompressedSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
	}
}

// RepoCredential is the provider-neutral, SHORT-LIVED bearer credential the
// Manager threads into every provider-touching projectStateAccess verb
// (projectStateAccess.md §REWORK.4). It is the credential
// sourceControlAccess.GetInstallationToken MINTS, and the Manager carries it down
// as a caller-supplied parameter — projectStateAccess NEVER calls
// sourceControlAccess itself (RA-never-calls-RA / NoSideways).
//
// SHAPE-MATCHED, NOT IMPORTED. The contract says RepoCredential is "the same
// opaque value type sourceControlAccess.md §3.2 defines — referenced, not
// redefined." The Method's layer rule (NoSideways: a ResourceAccess imports no
// sibling ResourceAccess, arch-checker-enforced) forbids projectstate importing
// the sourcecontrol package, so the layer-legal realization of "referenced, not
// redefined" is a LOCAL value type of the IDENTICAL shape ({Bytes, ExpiresAt}).
// The two are structurally identical opaque carriers; the Manager constructs this
// one from the credential getInstallationToken returned. (See C-PA-R log §"contract
// gaps flagged" — the architect may prefer promoting RepoCredential to framework-go
// so both RAs reference one definition; until then the shape-match is the
// layer-legal equivalent.)
//
// PROVIDER-NEUTRAL: carries NO ghs_ prefix, NO installation id, NO App JWT. Bytes
// is write-only at this consumer — presented to the git transport, never logged,
// persisted, parsed, or compared.

// Bytes is the opaque bearer secret (the installation token's bytes). Presented
// to the git remote; never inspected here.

// ExpiresAt is when the Manager re-mints (calls getInstallationToken again).
// Carried for parity with the source type; this RA does not act on it (the
// Manager owns re-mint timing).

// IsZero reports whether the credential is empty. A zero credential is only valid
// for the LOCAL on-disk-git profile (a trivially-valid local credential,
// projectStateAccess.md §REWORK.4 LOCAL note); against a remote GitHub it is a
// caller pre-condition violation surfaced as fwra.ContractMisuse.
func (c RepoCredential) IsZero() bool { return len(c.Bytes) == 0 }

// LocalRepoCredential is the trivially-valid credential the LOCAL deployment
// profile (on-disk git) threads — the same parameter, a no-op value. It signals
// the git-data layer to attach no HTTP auth (a file:// remote needs none).
func LocalRepoCredential() RepoCredential { return RepoCredential{Bytes: []byte("local")} }

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
// directly on *projectStateGitAdapter (the sole production ProjectStateAccess): the
// eight DesignSessionAccess verb NAMES collide with methods *projectStateGitAdapter
// already exports for a DIFFERENT wire shape — e.g. ReadProjectOnBranch(rc, projectID,
// branch) (Project, error) on ProjectStateAccess vs ReadProjectOnBranch(rc, projectID,
// branch) (ProjectEnvelope, error) here. Go does not allow two same-named methods with
// different signatures on one receiver, so this facade is a distinct type.
//
// The branch/ledger/reconcile/stale-ack verbs are REQUIRED members of the generated
// ProjectStateAccess contract (project.json), so every method below is a direct
// forward — no capability assert, no fallback. CommitArtifactWithProvenance is the
// one exception: it is not a session/branch verb (commit-time provenance stamping
// is a git-substrate concern), so it keeps a narrow capability check against the
// unexported provenanceCommitter interface below.
type designSessionAccess struct {
	base designSessionBase
}

// designSessionBase is the design-session facet's view of its base: the public
// (post-C-cleanup, pruned) ProjectStateAccess contract PLUS the on-branch design-session
// verbs. Those verbs moved off the projectStateAccess contract surface onto the
// designSessionAccess facet contract (state reconciliation, Wave 1), but the SAME concrete
// GitStore adapter still implements them — this facet reaches them through the wider base
// method set rather than the narrowed public port. This is a port/interface bookkeeping
// split, not a behavior change.
type designSessionBase interface {
	ProjectStateAccess
	ReadProjectOnBranch(rc fwra.Context, projectID ProjectID, branch string) (Project, error)
	StageArtifactForReviewOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, model ArtifactModel, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RejectArtifactOnBranchWithComments(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error)
	WithdrawArtifactOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error)
	ReconcileBranchFromMain(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, idempotencyKey fwra.IdempotencyKey) (Version, error)
	SetReviewCommentStatusOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, commentID string, status string, idempotencyKey fwra.IdempotencyKey) (Version, error)
	SeedReviewCommentsOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error)
}

var _ DesignSessionAccess = (*designSessionAccess)(nil)

// provenanceCommitter is the ONE remaining optional capability designSessionAccess
// checks for: CommitArtifactWithProvenance was never folded into the generated
// ProjectStateAccess contract (see the C2 fold note on designSessionAccess above), so a
// base that doesn't stamp provenance is a legitimate (if currently unexercised in
// production) case, and the plain CommitArtifact fallback stays meaningful.
type provenanceCommitter interface {
	CommitArtifactWithProvenance(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, approvedBy, draftedBy string) (Version, error)
}

// NewDesignSessionAccess wraps base. Every folded verb forwards straight to base (the
// generated ProjectStateAccess contract now requires all of them); CommitArtifactWithProvenance
// alone still checks base's optional provenanceCommitter capability.
func NewDesignSessionAccess(base ProjectStateAccess) DesignSessionAccess {
	// The design-session facet REQUIRES a base that also implements the on-branch verbs
	// (see designSessionBase). Every production ProjectStateAccess is a *GitStore adapter
	// that does; a base without them is a wiring error, caught here at construction.
	full, ok := base.(designSessionBase)
	if !ok {
		panic("projectstate.NewDesignSessionAccess: base does not implement the on-branch design-session verbs")
	}
	return &designSessionAccess{base: full}
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
// ReadProjectOnBranchActivity: forwards straight to base (branch=="" reads the
// default/main — a guarantee the generated ProjectStateAccess contract now makes
// uniformly). Returns the envelope directly so the Temporal payload (once a Manager
// consumes this) stays a concrete projection.
func (s *designSessionAccess) ReadProjectOnBranch(rc fwra.Context, projectID ProjectID, branch string) (ProjectEnvelope, error) {
	proj, err := s.base.ReadProjectOnBranch(rc, projectID, branch)
	if err != nil {
		return ProjectEnvelope{}, err
	}
	return EncodeProject(proj)
}

// StageArtifactForReviewOnBranch decodes the wire envelope into the concrete typed
// model, then forwards straight to base.
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
	return s.base.StageArtifactForReviewOnBranch(rc, projectID, expectedVersion, branch, decoded, idempotencyKey)
}

// CommitArtifactWithProvenance records commit provenance (committedAt/approvedBy/
// draftedBy) atomically with the commit when base implements the optional
// provenanceCommitter capability; otherwise falls back to the plain commit (absent
// provenance is allowed). This is the ONE folded verb that stayed a genuine capability
// check post-C2 (see the designSessionAccess doc comment above) — CommitArtifactWithProvenance
// was never added to the generated ProjectStateAccess contract.
func (s *designSessionAccess) CommitArtifactWithProvenance(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, approvedBy, draftedBy string) (Version, error) {
	if pc, ok := s.base.(provenanceCommitter); ok {
		return pc.CommitArtifactWithProvenance(rc, projectID, expectedVersion, kind, approvedBy, draftedBy)
	}
	return s.base.CommitArtifact(rc, projectID, expectedVersion, kind)
}

// RejectArtifactOnBranchWithComments forwards straight to base: it lands the Rejected
// status flip + notes AND appends the reviewer's comments to the slot's durable
// ReviewThread in one atomic commit.
func (s *designSessionAccess) RejectArtifactOnBranchWithComments(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.base.RejectArtifactOnBranchWithComments(rc, projectID, expectedVersion, branch, kind, notes, round, comments, idempotencyKey)
}

// WithdrawArtifactOnBranch forwards straight to base (branch=="" withdraws on the
// default/main).
func (s *designSessionAccess) WithdrawArtifactOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.base.WithdrawArtifactOnBranch(rc, projectID, expectedVersion, branch, kind, notes, idempotencyKey)
}

// ReconcileBranchFromMain overlays main's every slot except kind's own onto the
// session-branch tip (F80c). Forwards straight to base; the "a non-empty branch is
// required" invariant is now the CONCRETE substrate's business rule (GitStore rejects
// branch=="" as fwra.ContractMisuse), not a capability the wrapper synthesizes — the
// old NotFound-when-unsupported-or-empty fallback here was permanently dormant (every
// production ProjectStateAccess supported reconcile unconditionally).
func (s *designSessionAccess) ReconcileBranchFromMain(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.base.ReconcileBranchFromMain(rc, projectID, expectedVersion, branch, kind, idempotencyKey)
}

// SetReviewCommentStatusOnBranch applies a human review-ledger transition (waive/
// reopen) on the session branch. Forwards straight to base.
func (s *designSessionAccess) SetReviewCommentStatusOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, commentID string, status string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.base.SetReviewCommentStatusOnBranch(rc, projectID, expectedVersion, branch, kind, commentID, status, idempotencyKey)
}

// SeedReviewCommentsOnBranch appends the F38 amendment reopening feedback as OPEN
// ledger entries (no status change). Forwards straight to base.
func (s *designSessionAccess) SeedReviewCommentsOnBranch(rc fwra.Context, projectID ProjectID, expectedVersion Version, branch string, kind ArtifactKind, round int64, comments []ReviewComment, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.base.SeedReviewCommentsOnBranch(rc, projectID, expectedVersion, branch, kind, round, comments, idempotencyKey)
}

// envelope.go is the ONE Manager-Temporal-boundary wire codec for the sealed
// ArtifactModel sum + the head-state Project aggregate, promoted down from the
// near-duplicate codec.go the projectdesign and systemdesign Managers each carried
// (designSessionAccess promotion — the branch/ledger/provenance/reconcile capability
// chains those two Managers' custom activities ran over optional ProjectStateAccess
// extensions move into this package's own IMPL; ReadProjectOnBranch returns
// ProjectEnvelope directly so the Temporal payload stays a concrete projection and the
// Manager layer no longer owns this decode).
//
// Reconciled from projectdesign/codec.go + systemdesign/codec.go: modelEnvelope was
// byte-identical between the two; projectEnvelope/slotEnvelope were near-identical
// with two deliberate systemdesign-only additions, both preserved here:
//
//   - Research (F16 payload-slimming): Phase-2 project design deliberately does NOT
//     carry the research corpus across the Activity boundary — a single source can be
//     a whole 660KB book, and Phase-2 never reads it. projectdesign's OWN codec.go
//     never declared the field at all, and its own test
//     (Test_encodeProject_DropsResearchCorpus) asserts the wire payload never even
//     contains a "research" KEY, not merely an empty one — a plain (non-pointer)
//     struct field's `omitempty` does NOT suppress the key in encoding/json, so the
//     field here is a POINTER: nil unless a caller opts in. EncodeProject leaves it
//     nil; systemdesign's codec.go assigns `env.Research = &p.Research` itself after
//     calling EncodeProject (its mission-draft step legitimately weaves the corpus
//     titles in).
//   - CritiqueVerdict / CritiqueNotes (D-MSD-Δ PM-critique carrier): systemdesign-only
//     feature, but carried HERE unconditionally (unlike Research) because they are
//     small strings, always empty for a Manager that never sets them (projectdesign),
//     and cleared on every status transition regardless (statusTransition,
//     slotcodec.go) — no payload-size or wire-shape hazard in sharing them.
//
// construction/codec.go's projectEnvelope/encodeProject was DELIBERATELY NOT part of
// the original move (a structurally different, far narrower flat projection of exactly
// the concrete fields the construction pump's eligibility selection reads) — but the
// B8 follow-up FOLDED it in rather than keep a purpose-specific parallel envelope
// forking the codec: ProjectEnvelope now also carries the three Phase-3
// construction-fidelity sections construction's pump needs and the Slots map could not
// express (they are top-level Project fields OUTSIDE slotTable, not ArtifactSlots):
//
//   - ActivityConstruction / ServiceContracts (maps, omitempty): the per-activity
//     construction head-state (NotStarted/Running/Done + phase completions) the pump's
//     eligibility selection walks, and the per-component contract corpus its hydrate
//     step resolves against. nil for every project construction never touched, so the
//     keys are structurally ABSENT from every pd/sd wire payload (byte-identical to the
//     pre-B8 envelope — pinned by TestProjectEnvelope_NoConstructionState_OmitsConstructionKeys).
//   - ReviewPolicy (*ReviewPolicy, omitempty): the committed human-approval-gate
//     configuration the per-activity spine's phase gate snapshots at workflow start. A
//     POINTER for the same reason Research is (a plain struct field's omitempty does
//     NOT suppress the key in encoding/json); nil unless the policy is non-zero.
//
// Unlike Research (caller-opt-in, F16 payload-slimming — a single corpus source can be
// a whole 660KB book), these three are SMALL and EncodeProject populates them
// unconditionally when present: the construction Manager reads through the generated
// designSessionAccess.readProjectOnBranch invoker and has no seam to opt in after the
// fact, and every non-construction project carries none of them anyway.
// construction/codec.go is deleted; its former decode semantics (committed-slot
// restore for Network/ActivityList) are subsumed by the Slots map's own
// status-faithful round-trip.

// ModelEnvelope is the wire form of one typed ArtifactModel: the STRING kind
// discriminator + the concrete model's own JSON under "model"
// ({"kind":"mission","model":{…}}). A nil model encodes as the zero envelope (Model
// empty), which decodes back to a nil model.
type ModelEnvelope struct {
	Kind  ArtifactKind    `json:"kind"`
	Model json.RawMessage `json:"model,omitempty"`
}

// EncodeModel wraps a (possibly nil) typed model into its envelope.
func EncodeModel(model ArtifactModel) (ModelEnvelope, error) {
	if model == nil {
		return ModelEnvelope{}, nil
	}
	raw, err := json.Marshal(model)
	if err != nil {
		return ModelEnvelope{}, fmt.Errorf("encode model %s: %w", model.Kind(), err)
	}
	return ModelEnvelope{Kind: model.Kind(), Model: raw}, nil
}

// Decode reconstructs the concrete typed model from its envelope.
func (e ModelEnvelope) Decode() (ArtifactModel, error) {
	if len(e.Model) == 0 {
		// Not an error: an empty payload IS the documented "no model yet" state
		// (e.g. a slot that has never been drafted). Every call site checks err
		// first, then uses a nil model as a legitimate value — a typed sentinel
		// error would force every caller to unwrap-and-ignore it, which is exactly
		// what returning a plain nil model already achieves.
		return nil, nil //nolint:nilnil // (nil model, nil err) is the documented "no model yet" value; see the block comment above.
	}
	model, ok := NewModelForKind(e.Kind)
	if !ok {
		return nil, fmt.Errorf("decode model: no concrete type for kind %s", e.Kind)
	}
	if err := json.Unmarshal(e.Model, model); err != nil {
		return nil, fmt.Errorf("decode model %s: %w", e.Kind, err)
	}
	if sol, isSol := model.(*Solution); isSol {
		// The four Solution slots share one concrete type distinguished by SlotKind;
		// the envelope Kind is authoritative. NewModelForKind pre-sets SlotKind, but
		// belt-and-suspenders: re-apply it after unmarshal in case the JSON had a
		// stale or differing value.
		sol.SlotKind = e.Kind
	}
	return model, nil
}

// SameArtifactModel reports whether two typed models are byte-identical in their
// canonical JSON form. Go marshals a given concrete struct deterministically (field
// order is declaration order; map keys are sorted), so this is a stable, replay-safe
// value comparison the workflow goroutine may call directly (no I/O). Used by the
// amendment no-change guard: when an amendment session's branch read-back is identical
// to the committed main model, the draft advanced the branch by nothing, so there is
// no change to review or merge and the session must land at the failed gate rather than
// 422 on an effectively-empty PR.
//
// PROMOTED CO-AUTHOR HELPER (code-health-phase-bd task D3): moved down from the
// near-duplicate coauthorartifact.go/coauthorphase2artifact.go the systemdesign and
// projectdesign Managers each carried, same category as the ModelEnvelope codec above.
func SameArtifactModel(a, b ArtifactModel) (bool, error) {
	ea, err := EncodeModel(a)
	if err != nil {
		return false, err
	}
	eb, err := EncodeModel(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ea.Model, eb.Model), nil
}

// artifactSlotWire is ArtifactSlot's plain-JSON shadow: every field verbatim
// EXCEPT Model, which is re-typed to ModelEnvelope. ArtifactModel is a sealed sum
// (Kind() + isArtifactModel()) with no exported concrete type on the wire, so a
// bare json.Marshal/Unmarshal of ArtifactSlot cannot round-trip a populated Model:
// Marshal flattens the concrete model's fields with no type discriminator, and
// Unmarshal then has no way to know which concrete type to allocate for the
// interface field ("cannot unmarshal object into Go struct field ...Model of
// type projectstate.ArtifactModel"). The git/postgres substrates never hit this —
// they carry their OWN equivalent envelope logic (slotJSON, encodeSlotsMap/
// decodeSlotsMap) and never call plain json.Marshal on a whole Project or
// ArtifactSlot. The gap is real for any OTHER consumer that does: notably a
// Temporal activity boundary (the default DataConverter is encoding/json), the
// first of which is operationsManager's projectStateAccess.readProject invoker
// (Task 2, operations/deploy.go — assembleDesiredState).
type artifactSlotWire struct {
	Status ArtifactReviewStatus `json:"Status"`
	// Model is carried as raw JSON (NOT ModelEnvelope) so UnmarshalJSON below can
	// distinguish "no model" (absent/null) from a malformed non-envelope shape
	// (review finding: decoding a flattened {"Model":{...}} with no "kind"
	// discriminator must not silently drop the model) BEFORE any zero-value
	// ambiguity creeps in — ArtifactKind's zero value is KindMission, a real,
	// meaningful kind, so a decoded-but-defaulted Kind is not itself distinguishable
	// from a genuine Mission-kind envelope; only the RAW shape (does a "kind" key
	// exist at all?) can tell the two apart.
	Model           json.RawMessage `json:"Model"`
	Notes           string          `json:"Notes"`
	CritiqueVerdict string          `json:"CritiqueVerdict"`
	CritiqueNotes   string          `json:"CritiqueNotes"`
	ReviewThread    []ReviewComment `json:"reviewThread,omitempty"`
	Revisions       int64           `json:"Revisions"`
	StaleBasis      bool            `json:"StaleBasis"`
	StaleBasisCause *StaleCause     `json:"StaleBasisCause,omitempty"`
	Provenance      *Provenance     `json:"Provenance,omitempty"`
}

// MarshalJSON gives ArtifactSlot a safe, self-contained JSON round-trip (see
// artifactSlotWire): Model is re-encoded as its {kind, model} ModelEnvelope via
// EncodeModel — the SAME mechanism the git/postgres substrate codecs use — rather
// than encoding/json's default interface-flattening behavior, which
// UnmarshalJSON below could never reconstruct.
func (s ArtifactSlot) MarshalJSON() ([]byte, error) {
	env, err := EncodeModel(s.Model)
	if err != nil {
		return nil, err
	}
	envRaw, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("encode ArtifactSlot.Model envelope: %w", err)
	}
	return json.Marshal(artifactSlotWire{
		Status:          s.Status,
		Model:           envRaw,
		Notes:           s.Notes,
		CritiqueVerdict: s.CritiqueVerdict,
		CritiqueNotes:   s.CritiqueNotes,
		ReviewThread:    s.ReviewThread,
		Revisions:       s.Revisions,
		StaleBasis:      s.StaleBasis,
		StaleBasisCause: s.StaleBasisCause,
		Provenance:      s.Provenance,
	})
}

// decodeArtifactModelWireField decodes artifactSlotWire.Model's raw bytes into an
// ArtifactModel, distinguishing three cases: (1) absent/JSON-null — no model yet,
// (nil, nil); (2) a well-formed {"kind":...,"model":...} envelope — decoded via
// ModelEnvelope.Decode; (3) anything else (a bare/flattened object with no "kind"
// discriminator, e.g. a producer that wrote json.Marshal(concreteModel) directly
// instead of through EncodeModel) — an explicit error rather than silently
// discarding the model (review finding: silent data loss is the wrong failure
// mode; no such producer exists today, but decoding must fail loudly if one ever
// does). Presence of "kind" is checked structurally (a map probe) rather than by
// the DECODED Kind value, because ArtifactKind's zero value (KindMission) is a
// real kind — an absent key and a genuine Mission envelope are NOT
// distinguishable after decoding into ModelEnvelope, only before.
func decodeArtifactModelWireField(raw json.RawMessage) (ArtifactModel, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil //nolint:nilnil // absent/null IS the documented "no model yet" value.
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("decode ArtifactSlot.Model: %w", err)
	}
	if _, hasKind := probe["kind"]; !hasKind {
		return nil, fmt.Errorf("decode ArtifactSlot.Model: present but not a {kind,model} envelope (no \"kind\" discriminator) — refusing to silently drop it")
	}
	var env ModelEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode ArtifactSlot.Model: %w", err)
	}
	return env.Decode()
}

// UnmarshalJSON is MarshalJSON's inverse: decode into the wire shadow, then
// reconstruct the concrete ArtifactModel from its raw Model bytes
// (decodeArtifactModelWireField, which dispatches on the envelope's own Kind —
// the same NewModelForKind lookup decodeSlotsMap uses, just driven by the
// envelope's kind field instead of an out-of-band map key).
func (s *ArtifactSlot) UnmarshalJSON(data []byte) error {
	var w artifactSlotWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	model, err := decodeArtifactModelWireField(w.Model)
	if err != nil {
		return err
	}
	s.Status = w.Status
	s.Model = model
	s.Notes = w.Notes
	s.CritiqueVerdict = w.CritiqueVerdict
	s.CritiqueNotes = w.CritiqueNotes
	s.ReviewThread = w.ReviewThread
	s.Revisions = w.Revisions
	s.StaleBasis = w.StaleBasis
	s.StaleBasisCause = w.StaleBasisCause
	s.Provenance = w.Provenance
	return nil
}

// AmendmentNoChangeReason renders the human "why" for the StageDraftFailed screen when
// an amendment session's draft committed nothing that changed the artifact — the branch
// read-back is byte-identical to the committed main model, so there is no advancement to
// open a PR on (opening one would 422 "no commits between base and head"). A Retry
// re-runs the amendment; a Withdraw abandons it.
//
// PROMOTED CO-AUTHOR HELPER (code-health-phase-bd task D3): see SameArtifactModel above.
func AmendmentNoChangeReason() string {
	return "the amendment draft committed no changes to the artifact — there is nothing to review or merge — retry or withdraw"
}

// ReadBackDecodeFailedReason renders the human "why" for the StageDraftFailed screen when
// the committed draft READS BACK MALFORMED (QA F36): the CI validate went GREEN (its Go
// mirror types the offending enum as a free string) but the server codec rejects the value
// on read-back (a closed-enum field carrying free prose). It frames it distinctly from a
// CI failure and carries the decode diagnostic so a Retry redrafts with full visibility.
//
// PROMOTED CO-AUTHOR HELPER (code-health-phase-bd task D3): see SameArtifactModel above.
func ReadBackDecodeFailedReason(decodeMsg string) string {
	if strings.TrimSpace(decodeMsg) == "" {
		return "the committed draft could not be read back — its typed shape is invalid — retry or withdraw"
	}
	return "the committed draft could not be read back — its typed shape is invalid: " + decodeMsg + " — retry or withdraw"
}

// OpenReviewCommentIDs returns the ids of every OPEN CHANGE-REQUEST — the comments that
// gate approve (review-ledger §4). Open QUESTIONS never gate (question-comments §approve),
// so they are excluded. Empty ⇒ approve is unblocked.
//
// PROMOTED CO-AUTHOR HELPER (code-health-phase-bd task D3): see SameArtifactModel above.
func OpenReviewCommentIDs(thread []ReviewComment) []string {
	var ids []string
	for _, c := range thread {
		if ReviewCommentBlocksApprove(c) {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// AmendmentIndexFor derives the amendment index (revision number) a fresh reopening of a
// COMMITTED artifact slot should carry (the review-ledger SEED of the reopening
// feedback). It keys off THE AMENDMENT CONDITION — the slot is COMMITTED — NOT off any
// Revisions magnitude. A committed slot is an amendment even when its Revisions reads 0
// (a slot committed BEFORE the Revisions field existed): the floor of 1 guarantees every
// committed slot yields an index >= 1, so a workflow's Amendment>0 checks are a faithful
// proxy for "committed at request time." A non-committed slot
// (drafting/awaiting/rejected/withdrawn/none) returns 0 — the normal (non-amendment) path.
//
// PROMOTED CO-AUTHOR HELPER (code-health-phase-bd task D3): see SameArtifactModel above.
func AmendmentIndexFor(slot ArtifactSlot) int {
	if slot.Status != ReviewCommitted {
		return 0
	}
	if slot.Revisions < 1 {
		return 1 // pre-field committed slot: grandfathered to revision 1
	}
	return int(slot.Revisions)
}

// DesignBranch derives the ONE persistent design SESSION branch per artifact review
// session (F40 founder ruling 2026-07-05: "we should be committing to the same branch,
// and improving that, until it merges. not a pr per draft. i want the history of changes
// in git."). ALL jobs of a session commit here sequentially — the initial draft, the
// PM/architect critique, and every redraft — and ONE PR (opened once, idempotent on head)
// merges it on approve. The name is deterministic from project + kind, so within a
// session it is STABLE across every redraft/reject round (no per-attempt suffix — the F32
// branch-per-attempt topology is unwound; the stale-base problem it solved is now handled
// by the workflow template's refresh-from-main git step).
//
// amendment > 0 selects a FRESH branch for an AMENDMENT session (F38): reopening a
// COMMITTED artifact starts session v2+ whose v1 branch/PR already merged (and may be
// deleted), so it cannot be reused. The "-amend-N" suffix is the only place the attempt
// counter survives, and only for amendments.
//
// PROMOTED CO-AUTHOR HELPER (code-health-phase-bd task D3): see SameArtifactModel above.
func DesignBranch(projectID ProjectID, kind ArtifactKind, amendment int) string {
	base := fmt.Sprintf("aiarch-design/%s/%d", projectID, int(kind))
	if amendment > 0 {
		return fmt.Sprintf("%s-amend-%d", base, amendment)
	}
	return base
}

// SlotEnvelope is the wire form of one Project slot across a Temporal boundary: the
// review status + the model envelope.
type SlotEnvelope struct {
	Status ArtifactReviewStatus `json:"status"`
	Notes  string               `json:"notes,omitempty"`
	Model  ModelEnvelope        `json:"model"`
	// CritiqueVerdict / CritiqueNotes carry the first-class PM-critique read-back
	// carrier (D-MSD-Δ amendment) across the boundary — see the package doc above for
	// why these are shared unconditionally while Research is not. omitempty keeps the
	// payload byte-identical for every slot a critique never touched.
	CritiqueVerdict string `json:"critiqueVerdict,omitempty"`
	CritiqueNotes   string `json:"critiqueNotes,omitempty"`
	// ReviewThread carries the DURABLE review ledger across the Temporal boundary
	// (F48): without it, a branch-aware read silently drops the reject-with-comments
	// append even though it lives in the branch git. omitempty keeps the payload
	// byte-identical for every slot the ledger never touched.
	ReviewThread []ReviewComment `json:"reviewThread,omitempty"`
}

// ProjectEnvelope is the wire form of the head-state Project across the
// ReadProjectActivity / DesignSessionAccess.ReadProjectOnBranch boundary: the
// identity/version/phase plus every populated slot keyed by kind ordinal. Empty slots
// are omitted. See the package doc above for why Research is a pointer.
type ProjectEnvelope struct {
	ID      ProjectID `json:"id"`
	Version Version   `json:"version"`
	Phase   Phase     `json:"phase"`
	// Research is nil unless a caller opts in (systemdesign's codec.go does, right
	// after calling EncodeProject); nil ⇒ omitempty drops the "research" key from the
	// wire payload entirely, matching projectdesign's own never-declared-the-field
	// behavior byte-for-byte.
	Research *ResearchCorpus               `json:"research,omitempty"`
	Slots    map[ArtifactKind]SlotEnvelope `json:"slots,omitempty"`

	// ActivityConstruction / ServiceContracts / ReviewPolicy are the Phase-3
	// construction-fidelity sections (B8 follow-up — see the package doc above): the
	// top-level Project fields outside slotTable() that construction's pump reads
	// across the designSessionAccess.readProjectOnBranch boundary. All three are
	// structurally absent from the wire payload (omitempty) for every project
	// construction never touched, keeping the pd/sd payloads byte-identical to the
	// pre-B8 envelope.
	ActivityConstruction map[string]ActivityConstructionStatus `json:"activityConstruction,omitempty"`
	ServiceContracts     map[string]ServiceContract            `json:"serviceContracts,omitempty"`
	ReviewPolicy         *ReviewPolicy                         `json:"reviewPolicy,omitempty"`
}

// EncodeProject wraps the head-state aggregate for the Temporal boundary, using the
// SAME canonical kind↔field slot table the substrate persistence codec uses
// (slotTable, slotcodec.go) so a Manager's envelope and the on-disk persistence never
// drift apart. Does NOT populate Research (see ProjectEnvelope doc) — a caller that
// needs the corpus in its envelope assigns `env.Research = &p.Research` itself after
// calling this.
func EncodeProject(p Project) (ProjectEnvelope, error) {
	out := ProjectEnvelope{ID: p.ID, Version: p.Version, Phase: p.Phase, Slots: map[ArtifactKind]SlotEnvelope{}}
	// Construction-fidelity sections (B8 follow-up): carried unconditionally when
	// present — nil maps / a zero policy stay structurally absent from the wire
	// (omitempty), so non-construction payloads are byte-identical to before.
	out.ActivityConstruction = p.ActivityConstruction
	out.ServiceContracts = p.ServiceContracts
	if len(p.ReviewPolicy.GatedPhasesByType) != 0 || p.ReviewPolicy.Preset != nil {
		rp := p.ReviewPolicy
		out.ReviewPolicy = &rp
	}
	for _, e := range slotTable() {
		slot := *e.ptr(&p)
		if slot.Status == ReviewNone && slot.Model == nil {
			continue
		}
		me, err := EncodeModel(slot.Model)
		if err != nil {
			return ProjectEnvelope{}, err
		}
		out.Slots[e.kind] = SlotEnvelope{
			Status:          slot.Status,
			Notes:           slot.Notes,
			Model:           me,
			CritiqueVerdict: slot.CritiqueVerdict,
			CritiqueNotes:   slot.CritiqueNotes,
			ReviewThread:    slot.ReviewThread,
		}
	}
	return out, nil
}

// Decode reconstructs the head-state aggregate from its envelope.
func (e ProjectEnvelope) Decode() (Project, error) {
	p := Project{ID: e.ID, Version: e.Version, Phase: e.Phase}
	if e.Research != nil {
		p.Research = *e.Research
	}
	// Construction-fidelity sections (B8 follow-up): restored verbatim; absent keys
	// decode to nil/zero, exactly the pre-construction Project state.
	p.ActivityConstruction = e.ActivityConstruction
	p.ServiceContracts = e.ServiceContracts
	if e.ReviewPolicy != nil {
		p.ReviewPolicy = *e.ReviewPolicy
	}
	for kind, se := range e.Slots {
		model, err := se.Model.Decode()
		if err != nil {
			return Project{}, err
		}
		slot, ok := slotPtr(&p, kind)
		if !ok {
			return Project{}, fmt.Errorf("decode project envelope: unknown kind ordinal %d", int(kind))
		}
		*slot = ArtifactSlot{
			Status:          se.Status,
			Model:           model,
			Notes:           se.Notes,
			CritiqueVerdict: se.CritiqueVerdict,
			CritiqueNotes:   se.CritiqueNotes,
			ReviewThread:    se.ReviewThread,
		}
	}
	return p, nil
}

// gitactivity.go holds the ADDITIVE per-activity git-forward Record* verbs
// (projectStateAccess.md §GIT-HEAD-STATE / GIT.2, D-PA-GIT, FROZEN 2026-06-12).
// They JOIN the existing additive construction-transition verbs
// (RecordChangeReviewed/RecordActivityExited/RecordOperatorPaused, gitconstruction.go)
// on the SAME additive facet — NOT the core ProjectStateAccess (Phase-1/2)
// interface, which is unchanged. Each carries the Manager-threaded
// `cred RepoCredential` (REWORK.4), `expectedVersion` + `idempotencyKey`, and
// `activityID`, and routes through the identical applyMutation ref-CAS + dedup-first
// path with modeRequireExisting (OQ-mode RULED: the project row exists by Phase 3 —
// same mode as SetResearchInput; do NOT inherit the v1 no-ops' modeUpsert).
//
// The ONLY change vs the v1 no-op mutators is that the closure now UPSERTS
// p.ActivityGit[activityID] — a PARTIAL map update (mutate one key, leave the rest
// byte-identical), the load-bearing convergence invariant (GIT.4): two records for
// two DIFFERENT activities converge under ref-CAS rather than clobber.
//
// PROVIDER-OPACITY: the Manager threads the rail's opaque String() handles + a
// typed CICheckState in; this RA stores them verbatim. No provider lexeme; no edge
// to sourceControlAccess.

// GitActivityStatusAccess is the GENERATED service-contract interface
// (contract.gen.go, contract.gitActivityStatusAccess.schema.json) covering the
// §GIT-HEAD-STATE branch/CI/+1/merge verbs plus the started/completed
// construction-status verbs, as ONE 6-op contract. Kept off the core
// ProjectStateAccess (Phase-1/2) interface, which is unchanged; the concrete
// GitStore satisfies this plus the cred-threaded construction-transition verbs
// (RecordChangeReviewed / RecordActivityExited / RecordActivityFailed / etc., below).

// Compile-time proof the concrete GitStore satisfies the generated git-status facet.
var _ GitActivityStatusAccess = (*GitStore)(nil)

// upsertActivity fetches (or initialises) the per-activity row, applies the supplied
// in-place mutation, server-resolves UpdatedAt, and writes the SINGLE map key back.
// The map is lazily allocated. This is a PARTIAL map-key update (GIT.4): only the
// named key is touched; every other ActivityGit entry is left byte-identical, so two
// records on DIFFERENT activityIds converge under ref-CAS instead of clobbering.
func (s *GitStore) upsertActivity(p *Project, activityID string, mutate func(g *ActivityGitStatus)) {
	if p.ActivityGit == nil {
		p.ActivityGit = map[string]ActivityGitStatus{}
	}
	g := p.ActivityGit[activityID] // zero value on first touch — births the row
	g.ActivityID = activityID
	mutate(&g)
	g.UpdatedAt = s.now()
	p.ActivityGit[activityID] = g
}

// RecordActivityBranchOpened upserts the per-activity git-status row with its
// branch (and, when carried, PR) coordinates. First touch also seeds CICheck to
// Pending; PR-side fields are never clobbered back to empty (see the in-body
// PR-tolerant-upsert note).
func (s *GitStore) RecordActivityBranchOpened(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID, branch, branchRef, prRef, crLabel string, isRevert bool, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityBranchOpened: empty activityID")
	}
	return s.applyMutation(rc.Context, "RecordActivityBranchOpened", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		first := true
		if p.ActivityGit != nil {
			_, first = p.ActivityGit[activityID]
			first = !first
		}
		s.upsertActivity(p, activityID, func(g *ActivityGitStatus) {
			g.BranchName = branch
			g.BranchRef = branchRef
			// PR-TOLERANT UPSERT: only overwrite the PR-side fields when the caller
			// carries them (the OpenPullRequest touch). A branch-only first touch leaves
			// a transient empty prRef that converges on the second call; never clobber a
			// previously-recorded prRef back to empty.
			if prRef != "" {
				g.PullRequestRef = prRef
			}
			if crLabel != "" {
				g.CRLabel = crLabel
			}
			if isRevert {
				g.IsRevert = true
			}
			if first {
				g.CICheck = CICheckPending
			}
		})
		return nil
	})
}

// RecordActivityCIObserved records the latest observed CI check state on the
// activity's git-status row.
func (s *GitStore) RecordActivityCIObserved(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, ci CICheckState, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityCIObserved: empty activityID")
	}
	return s.applyMutation(rc.Context, "RecordActivityCIObserved", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		s.upsertActivity(p, activityID, func(g *ActivityGitStatus) {
			g.CICheck = ci
		})
		return nil
	})
}

// RecordActivityArchApproved marks the architect's PR approval on the
// activity's git-status row.
func (s *GitStore) RecordActivityArchApproved(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityArchApproved: empty activityID")
	}
	return s.applyMutation(rc.Context, "RecordActivityArchApproved", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		s.upsertActivity(p, activityID, func(g *ActivityGitStatus) {
			g.ArchApproved = true
		})
		return nil
	})
}

// RecordActivityMerged marks the activity's PR as merged on its git-status row.
func (s *GitStore) RecordActivityMerged(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityMerged: empty activityID")
	}
	return s.applyMutation(rc.Context, "RecordActivityMerged", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		s.upsertActivity(p, activityID, func(g *ActivityGitStatus) {
			g.Merged = true
		})
		return nil
	})
}

// gitactivityconstruction.go holds the per-activity construction status Record* verbs.
// They mirror the gitactivity.go pattern exactly: modeRequireExisting, optimistic-version
// CAS via applyMutation, idempotency dedup via the in-repo ledger, and partial map-key
// upsert so two records for two DIFFERENT activities converge under ref-CAS (the GIT.4
// convergence invariant applies here too).
//
// RecordActivityStarted/RecordActivityCompleted are folded onto the SAME generated
// GitActivityStatusAccess contract as the gitactivity.go verbs (one 6-op component,
// contract.gitActivityStatusAccess.schema.json) — see gitactivity.go for the interface
// + its compile-time assertion.

// upsertActivityConstruction fetches (or initialises) the per-activity construction row,
// applies the supplied in-place mutation, and writes the SINGLE map key back. The map is
// lazily allocated. This is a PARTIAL map-key update (mirrors upsertActivity in
// gitactivity.go — GIT.4): only the named key is touched; every other
// ActivityConstruction entry is left byte-identical, so two records on DIFFERENT
// activityIds converge under ref-CAS instead of clobbering.
func upsertActivityConstruction(p *Project, activityID string, mutate func(s *ActivityConstructionStatus)) {
	if p.ActivityConstruction == nil {
		p.ActivityConstruction = map[string]ActivityConstructionStatus{}
	}
	s := p.ActivityConstruction[activityID] // zero value on first touch — births the row
	s.ActivityID = activityID
	mutate(&s)
	p.ActivityConstruction[activityID] = s
}

// RecordActivityStarted records that activityID's construction agent has been dispatched
// (Phase → Running, StartedAt server-resolved). Uses modeRequireExisting (project row
// exists by Phase 3, same as gitactivity.go verbs).
//
// typ/variant are the pair the DISPATCHER classified (ClassifyActivity) and is about to
// walk. Stamping them here is load-bearing, not decorative: phaseSetFor(cs.Type,
// cs.Variant) seeds the head-state Phases slice on the first RecordPhaseStarted, so
// leaving Type at its zero value seeded the 5-phase SERVICE set for every live activity
// — including correctly-dispatched testing ones — and earned value silently diverged
// from the profile the workflow actually walks.
func (s *GitStore) RecordActivityStarted(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, typ ActivityType, variant TestingVariant, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityStarted: empty activityID")
	}
	now := s.now()
	return s.applyMutation(rc.Context, "RecordActivityStarted", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			cs.Phase = ActivityConstructionRunning
			cs.Type = typ
			cs.Variant = variant
			// Advance the finer BuildStatus lens in lock-step with the coarse Phase so the
			// SINGLE constructionRows projection (catalog.go) tells the whole cascade story:
			// a dispatched activity is being built now → in-construction (the SPA's tracker
			// keys node color off BuildStatus). The pump only ever touches a NotStarted/absent
			// row (eligibility gate), so this never clobbers a seeded corpus BuildStatus.
			cs.BuildStatus = BuildInConstruction
			t := now
			cs.StartedAt = &t
		})
		return nil
	})
}

// RecordActivityCompleted records that activityID's construction agent has finished
// (Phase → Done, CompletedAt server-resolved). Uses modeRequireExisting.
func (s *GitStore) RecordActivityCompleted(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityCompleted: empty activityID")
	}
	now := s.now()
	return s.applyMutation(rc.Context, "RecordActivityCompleted", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			cs.Phase = ActivityConstructionDone
			// The per-activity construction spine completes only AFTER its review passed
			// and the change merged (workflow.go steps 5–8a), so a completed activity IS
			// integrated — advance BuildStatus to Integrated. This is what adds the activity
			// to the SPA's done-set (constructionAdapters: status==='integrated'), turning its
			// node green AND unblocking its dependents so the frontier cascades forward.
			cs.BuildStatus = BuildIntegrated
			t := now
			cs.CompletedAt = &t
		})
		return nil
	})
}

// gitactivitystatus.go holds the per-activity git-forward head-state types
// (projectStateAccess.md §GIT-HEAD-STATE, D-PA-GIT, FROZEN 2026-06-12). It is the
// durable mirror of what IPullRequestRail (sourceControlAccess) returns as the
// branch→PR→CI→+1→merge lifecycle advances per construction-network activity.
//
// PROVIDER-OPACITY is the load-bearing constraint: this RA stores the rail's
// opaque String() handles + a typed CI enum. It names no provider, parses no
// handle, and calls no other RA — the constructionManager (the only both-seam
// toucher) receives the opaque rail returns and threads them into the Record*
// verbs (gitactivity.go), exactly as it threads the resolved typed model into
// StageArtifactForReview. RA-never-calls-RA holds: there is no
// projectStateAccess → sourceControlAccess edge.

// CICheckState is the provider-neutral CI rollup the SPA renders (3 states),
// mirroring sourcecontrol.CheckState + the ux-mock CiStatus. A DUMB reflection of
// the Actions run — it NEVER gates any Approve control. (GIT.1)

// String returns the canonical name for the CI rollup state.
func (c CICheckState) String() string {
	switch c {
	case CICheckSuccess:
		return "Success"
	case CICheckFailure:
		return "Failure"
	case CICheckPending:
		return "Pending"
	}
	// Unreachable for the three defined CICheckState values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "Pending"
}

// ActivityGitStatus is the per-activity git-forward head-state record (D-PA-GIT).
// One per construction-network activity, keyed by ActivityID — the durable mirror
// of what IPullRequestRail returns. PROVIDER-OPAQUE: every handle is the rail's
// opaque String() form; CICheck mirrors the rail's CheckState; NO provider lexeme
// is stored. (GIT.1)
//
// feedback_agent_friendly_typed_schemas: the key is ActivityID (a stable NAME, not
// a minted UUID); BranchRef/PullRequestRef are resolved by the rail and threaded
// through the Manager (this RA stores, never derives); the SPA's display #N AND
// clickable prUrl are derived/constructed AT READ from the opaque PullRequestRef +
// the per-project repo base the webClient holds (neither is a stored duplicate, and
// the repo-base construction keeps the head-state free of any provider host);
// UpdatedAt is server-resolved. prNumber-as-int and prUrl-as-string are
// deliberately NOT stored (derivable; the rail returns no url — OQ-3 RULED).

// ActivityID is the network activity id (D-CW, C-MST, I-UC1, cr-021-export…) —
// the map key (NAME-as-identity).

// BranchName is the per-activity branch (Manager-derived; provider-neutral,
// e.g. "activity/C-MST").

// BranchRef is the opaque BranchRef.String() (today a git ref; never parsed).

// PullRequestRef is the opaque PullRequestRef.String() (today a PR number; never
// parsed). The SPA constructs the clickable prUrl from THIS + the per-project repo
// base (OQ-3 RULED: no url stored — the rail returns none, and storing a
// github.com/owner/repo url would leak a provider host). Empty until the PR opens
// (branch-only first touch).

// CICheck is the last-observed CI rollup reflection (mirrors rail CheckState); a
// DUMB reflection, never a gate.

// ArchApproved is set once the human's architecture +1 was relayed (postReview
// Approve) — the ArchApprovedTag.

// Merged is set once the gated merge to main completed (MergeResult.Merged).

// CRLabel is the cr-NN change-request group label, "" when not a CR activity
// (GitRowMeta crLabel).

// IsRevert marks a PR that carries inverse commits (a revert PR) — op-concepts §15.

// UpdatedAt is the last Record* touch — SERVER-RESOLVED at commit, never
// caller-minted.

// ConstructionTransitionAccess (contract.gen.go) is the Port interface for the
// Phase-3 construction transition verbs (App-C §6 adjudicated: 10 ops ≤ 12 cap,
// per conformance gate lifecycle-2 T3 analysis). The interface itself is
// GENERATED from project.json's .serviceContracts.constructionTransitionAccess
// entry (authored via contract.constructionTransitionAccess.schema.json), so
// like the sibling ProjectStateAccess it takes the RA-layer call context
// (rc fwra.Context) as every op's first parameter.
//
// WARNING — ReadProject (the 10th op) is IN-PROCESS-ONLY (B8-follow-up ruling): it
// returns the raw Project aggregate, whose ArtifactSlots carry the SEALED
// ArtifactModel interface that Temporal's default JSON data converter cannot decode
// across an Activity boundary. The op nevertheless has a generated Temporal activity
// ("constructionTransitionAccess.readProject", activities.gen.go/worker.gen.go —
// codegen emits one per contract op unconditionally) and a generated invoker
// (genInvokers.ConstructionTransitionReadProject) — DO NOT invoke either from a
// workflow: the result would decode with every slot's Model silently nil-ed/mangled.
// Workflows needing the whole aggregate must read through
// designSessionAccess.readProjectOnBranch, which returns the codec-safe
// ProjectEnvelope (envelope.go). The op is KEPT on the contract because it has a live
// in-process consumer: constructionManager.UpdateReviewPolicy
// (internal/manager/construction/constructionmanager.go) reads the current head
// Version through it before RecordReviewPolicy — a façade-side direct call that
// never crosses Temporal.

// Compile-time assertion: GitStore must satisfy the full 10-op port.
var _ ConstructionTransitionAccess = (*GitStore)(nil)

// ---- MissionStatement — ch. 5 business alignment ----

// Objective is a single numbered business objective in a MissionStatement.
// (projectStateAccess.md §3.5)

// MissionStatement is the typed artifact for ch. 5 business alignment.
// Vision is ONE terse sentence; Objectives are from the business perspective,
// numbered; Mission is expressed in components, not features.
// (projectStateAccess.md §3.5)

// ONE terse sentence
// business perspective only; numbered
// expressed in components, not features

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (m *MissionStatement) Kind() ArtifactKind { return KindMission }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (m *MissionStatement) isArtifactModel() {}

// NewMissionStatement constructs a MissionStatement after validating SHAPE only.
// Shape check: Vision must be non-empty. Semantic rules (e.g. objective count,
// wording) are enforced by artifactValidationEngine, not here.
// (projectStateAccess.md §3.5 "smart constructors enforce SHAPE only")
func NewMissionStatement(vision string, objectives []Objective, mission string) (*MissionStatement, error) {
	if vision == "" {
		return nil, errors.New("projectstate.NewMissionStatement: Vision must not be empty")
	}
	return &MissionStatement{
		Vision:     vision,
		Objectives: objectives,
		Mission:    mission,
	}, nil
}

// ---- Glossary — ch. 3 "What's in a Name" ----

// GlossaryItem is one entry in the system Glossary.
// Category aligns with the Four Questions: Who / What / How-activity / Where.
// (projectStateAccess.md §3.5)

// Who / What / How-activity / Where (the Four Questions), optional

// Glossary is the typed artifact for the system ubiquitous language per ch. 3.
// (projectStateAccess.md §3.5)

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (g *Glossary) Kind() ArtifactKind { return KindGlossary }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (g *Glossary) isArtifactModel() {}

// NewGlossary constructs a Glossary after validating SHAPE only.
// Shape checks: no item may have an empty Term or empty Definition.
// Semantic rules (completeness, Four-Question coverage) are enforced by
// artifactValidationEngine, not here.
// (projectStateAccess.md §3.5 "smart constructors enforce SHAPE only")
func NewGlossary(items []GlossaryItem) (*Glossary, error) {
	for i, item := range items {
		if item.Term == "" {
			return nil, fmt.Errorf("projectstate.NewGlossary: item at index %d has empty Term", i)
		}
		if item.Definition == "" {
			return nil, fmt.Errorf("projectstate.NewGlossary: item at index %d has empty Definition", i)
		}
	}
	return &Glossary{Items: items}, nil
}

// ---- Volatilities — ch. 2, the two axes ----

// Axis is the closed 2-set of volatility axes per ch. 2.
// (projectStateAccess.md §3.5)

// one customer over time
// all customers at one time

// Volatility is a single identified volatility with its axis and rationale.
// (projectStateAccess.md §3.5)

// bolded volatility name

// Volatilities is the typed artifact for the ch. 2 volatility analysis.
// Grouped by Axis on render via artifactRenderingAccess.
// (projectStateAccess.md §3.5)

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (v *Volatilities) Kind() ArtifactKind { return KindVolatilities }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (v *Volatilities) isArtifactModel() {}

// ---- CoreUseCases — ch. 4 ----

// UseCaseDecision pairs a UseCase with its inclusion/exclusion rationale.
// RejectionReason is empty when the use case is core; it carries the reason
// when the use case was evaluated and rejected as a permutation.
// (projectStateAccess.md §3.5)

// "" when core; reason when rejected as a permutation

// CoreUseCases is the slot-level typed artifact for the ch. 4 core use-case
// selection. It holds the raw list and the core selection.
//
// Constraint (enforced by artifactValidationEngine, not here): a CoreUseCases
// collection must hold 2–6 UseCase values with Classification==ClassCore.
// (projectStateAccess.md §3.5)

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (c *CoreUseCases) Kind() ArtifactKind { return KindCoreUseCases }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (c *CoreUseCases) isArtifactModel() {}

// ---- DeploymentOperationsModel — operational concepts (Wave-2 typed model) ----

// DeliveryStyle is the closed set of system delivery styles. The set of deployment
// environments is DERIVED from it (test is always present): cloud→{cloud,test},
// local→{local,test}, both→{cloud,local,test}. (spec-2026-06-03 Decision 4)

// DeploymentProfile is the closed set of deployment environment profiles.
// (spec-2026-06-03 Decision 4)

// DeployContainer is a deployable unit (C4 Container) packaging System Components by name.

// System Component NAMES

// ContainerInstance instances a declared DeployContainer inside a node. Its Key
// is the INSTANCE's identity, distinct from ContainerKey (which container is
// instanced): one container may legitimately be instanced in more than one node
// — a single-page application is both delivered by the web server and executed
// in the browser — and those two instances are distinct edge endpoints.

// InfrastructureNode is a C4 deployment infrastructure node (e.g. a load balancer,
// firewall, or managed service) that does not itself host a DeployContainer.

// SoftwareSystemInstance is a C4 external software system instance placed inside a
// deployment node (e.g. a third-party SaaS dependency).

// ContainerSurface values — how a container is CONSUMED. The three human-facing
// surfaces plus AgentHarness are the "frontend" set DEP-FRONTEND-PRESENT
// requires; SurfaceService is the back-end default, which is what a document
// authored before the field existed decodes to.
const (
	SurfaceSPA          ContainerSurface = "spa"
	SurfaceMobile       ContainerSurface = "mobile"
	SurfaceAgentHarness ContainerSurface = "agentHarness"
	SurfaceCLI          ContainerSurface = "cli"
	SurfaceService      ContainerSurface = "service"
)

// ElementRole values — what an infrastructure node or external software system
// DOES. Typed so the deployment rules can find the edge gateway and the identity
// provider structurally rather than by string-matching whatever name the drafting
// agent chose. RoleOther is the default.
const (
	RoleGateway          ElementRole = "gateway"
	RoleIdentityProvider ElementRole = "identityProvider"
	RoleDatabase         ElementRole = "database"
	RoleObjectStore      ElementRole = "objectStore"
	RoleMessaging        ElementRole = "messaging"
	RoleObservability    ElementRole = "observability"
	RoleAgentHarness     ElementRole = "agentHarness"
	RoleSourceControl    ElementRole = "sourceControl"
	RolePaymentGateway   ElementRole = "paymentGateway"
	RoleOther            ElementRole = "other"
)

// DeploymentNode is nestable: cluster → namespace → instance.
//
// Key is the node's identity within its environment — the handle a
// DeploymentRelationship addresses. Every deployment element carries one; the
// node's is declared here rather than in the schema because DeploymentNode is
// self-referential (Children []DeploymentNode) and so is hand-held rather than
// emitted by modelgen.
type DeploymentNode struct {
	Key                     string                   `json:"key"`
	Name                    string                   `json:"name"`
	Technology              string                   `json:"technology"`
	Description             string                   `json:"description"`
	Instances               int                      `json:"instances"`
	Tags                    []string                 `json:"tags"`
	Children                []DeploymentNode         `json:"children"`
	InfrastructureNodes     []InfrastructureNode     `json:"infrastructureNodes"`
	ContainerInstances      []ContainerInstance      `json:"containerInstances"`
	SoftwareSystemInstances []SoftwareSystemInstance `json:"softwareSystemInstances"`
}

// DeploymentEnvironment is the set of nodes for one DeploymentProfile.

// DeploymentTopology is the typed deployment model carried by OperationalConcepts.
// The deployed component graph is identical across profiles (instances swapped at
// the durable-execution / client-transport / git seams); enforced by the
// artifactValidationEngine's DEP-* predicates, not here.

// DeploymentOperationsModel is the typed artifact for the operational-concepts slot
// (Wave-2: the former OperationalConcepts/decisions[] shape is replaced by the typed
// deployment-operations model — deployment scenario, construction venue, review-policy
// ref, scaling/infra blocks, trust summaries, and the deployment topology).

// Kind implements ArtifactModel.
func (o *DeploymentOperationsModel) Kind() ArtifactKind { return KindOperationalConcepts }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (o *DeploymentOperationsModel) isArtifactModel() {}

// ---- StandardCheck — App C design-standard walk ----

// CheckStatus is the outcome of a single App C design-standard item.
// (projectStateAccess.md §3.5)

// item passes the guideline
// item waived with written justification
// item fails

// CheckItem is one row of the App C design-standard walk.
// Justification is required when Status == CheckWaived.
// (projectStateAccess.md §3.5)

// App C section, e.g. "§3.4"

// required when CheckWaived

// StandardCheck is the typed artifact for the App C design-standard walk.
// (projectStateAccess.md §3.5)

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (s *StandardCheck) Kind() ArtifactKind { return KindStandardCheck }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (s *StandardCheck) isArtifactModel() {}

// ---- ScrubbedRequirements — OQ-2 (artifactValidationEngine.md) ----

// Requirement is a single scrubbed requirement item.
// (artifactValidationEngine.md OQ-2; projectStateAccess.md KindScrubbedRequirements)

// ScrubbedRequirements is the typed artifact holding the set of scrubbed
// requirements that the validation Engine cross-references.
// (artifactValidationEngine.md OQ-2; identity.go KindScrubbedRequirements)

// Kind implements ArtifactModel. (identity.go KindScrubbedRequirements)
func (r *ScrubbedRequirements) Kind() ArtifactKind { return KindScrubbedRequirements }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (r *ScrubbedRequirements) isArtifactModel() {}

// Phase-2 typed artifact models — the head-state slot models the projectDesignManager
// co-authors in Phase 2 (projectStateAccess.md §3.6, projectDesignManager.md §3).
//
// These are the STORED slot models (each implements ArtifactModel, routed to its
// named slot by Kind()). They are distinct from the transient assembled-option value
// types in estimation.go (ProjectOption et al.) that the Manager feeds the Engines.
//
// Grammar is intentionally lean-but-real: enough to assemble the four ProjectOptions
// (from PlanningAssumptions + ActivityList + Network + the per-option Solution) and
// to join the three Engine outputs into the SDP-review rows. Fields are additive.
//
// Each model keeps the pointer-receiver KindXxx + isArtifactModel() convention used
// by every other model in this package (system.go, models_phase1.go).

// PlanningAssumptions holds the Phase-2 planning assumptions artifact: the resources,
// calendar, infrastructure, declared usage, and settlement terms the project network
// and the SDP-review estimates are built on. (projectStateAccess.md §3.6)

// named staff/resources available
// working days/week (5 normal, 2 moonlight, …)

// Kind implements ArtifactModel.
func (p *PlanningAssumptions) Kind() ArtifactKind { return KindPlanningAssumptions }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (p *PlanningAssumptions) isArtifactModel() {}

// ActivityItem is one activity in the activity list — effort in 5-day quanta, its
// worker class, whether it is coding (vs noncoding/integration), and its Fibonacci
// risk bucket.
type ActivityItem struct {
	Name        string  `json:"name"`
	EffortDays  float64 `json:"effortDays"`
	WorkerClass string  `json:"workerClass"`
	Coding      bool    `json:"coding"`
	RiskBucket  int     `json:"riskBucket"` // 1,2,3,5,8,13 (Fibonacci)
	// Title is the human-readable activity description (e.g. "Build Web Client") —
	// additive, omitempty for back-compat with documents that pre-date it. Name stays
	// the network id (the load-bearing dependency/head-state key); Title is display-only.
	Title string `json:"title,omitempty"`
	// ComponentID names the committed System component (systemDesign Components[].id)
	// this activity builds. AUTHORED at Phase-2 draft time — never derived by matching.
	// Its PRESENCE declares the activity STRUCTURAL (Löwy ch.13 Table 13-1, one per
	// architecture component); its absence declares it nonstructural (Table 13-2:
	// harnesses, base services) or noncoding. When present it must resolve to a
	// committed component. A noncoding provisioning activity (R-*) may name the
	// Resource component it provisions.
	ComponentID string `json:"componentId,omitempty"`
}

// ActivityList holds the Phase-2 activity list artifact — the coding + noncoding
// activities in 5-day quanta. (projectStateAccess.md §3.6)

// Kind implements ArtifactModel.
func (a *ActivityList) Kind() ArtifactKind { return KindActivityList }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (a *ActivityList) isArtifactModel() {}

// NetworkDependency declares that one activity depends on a set of predecessors.

// Network holds the Phase-2 project network artifact — the activity dependencies and
// the computed critical path (the activity names on it). (projectStateAccess.md §3.6)
//
// AUTHORED vs COMPUTED (2026-06-19, founder gate — move CPM compute server-side):
//   - Dependencies, CriticalPath, Milestones are AUTHORED inputs (stored, co-authored
//     in Phase 2; the SPA must never invent them).
//   - Computed, Summary are the COMPUTE-AT-READ block the projectManager populates by
//     running constructionEstimationEngine.ComputeNetwork over (Dependencies ×
//     ActivityList) on every read. They are `omitempty` so the AUTHORED document on
//     disk never carries them — they exist only on the wire the SPA reads. The web
//     client's former client-side CPM (toNetworkView) is RETIRED in favour of these.
type Network struct {
	// --- AUTHORED inputs (stored on disk) ---
	Dependencies []NetworkDependency `json:"dependencies"`
	// CriticalPath, for a DERIVED network (Task 10b, 2026-08-09), is written as the
	// alphabetically-sorted SET of zero-float activity names from a ComputeNetwork
	// solve over the derivation — not an ORDERED path through the graph, despite the
	// field name. Do not read adjacency or sequence into its element order.
	CriticalPath []string `json:"criticalPath"` // activity names on the critical path
	// Milestones are the authored zero-duration event nodes (M0–M5 + N-DOGFOOD): the
	// id/name/public/dependsOn are authored; OnCriticalPath + EventTime are computed at
	// read. omitempty so a network with none round-trips unchanged.
	Milestones []NetworkMilestone `json:"milestones,omitempty"`

	// --- COMPUTED block (compute-at-read; absent on disk, present on the wire) ---
	// Computed is the per-activity CPM result, keyed by activity id. Populated only by
	// the projectManager's compute-at-read pass; nil/absent in the stored document.
	Computed map[string]NetworkNodeCompute `json:"computed,omitempty"`
	// Summary is the project-level CPM roll-up. Populated only at read; nil on disk.
	Summary *NetworkSummary `json:"summary,omitempty"`
}

// NetworkMilestone is one authored zero-duration event node on the project network
// (M0–M5 + N-DOGFOOD). The id/name/public/dependsOn are AUTHORED; OnCriticalPath and
// EventTime are COMPUTED at read (EventTime = max predecessor earliest-finish; a
// milestone with no predecessors has EventTime 0 — the project-start gate). Milestones
// are EXCLUDED from the risk decomposition (they carry no effort and no risk bucket).
type NetworkMilestone struct {
	// --- AUTHORED ---
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Public    bool     `json:"public"`              // a demo-to-management gate vs an internal hurdle
	DependsOn []string `json:"dependsOn,omitempty"` // predecessor activity ids (the fan-in)
	// --- COMPUTED at read ---
	// POINTERS (not bare bool/float64) so they are ABSENT on the authored on-disk document
	// — a stored milestone carries only id/name/public/dependsOn; a bare bool/float64 would
	// persist a misleading onCriticalPath:false / eventTime:0. The compute-at-read pass
	// sets them non-nil, so they are ALWAYS emitted on the wire (even a computed false / 0),
	// which a bare-type omitempty would wrongly drop. This keeps the authored↔computed split
	// faithful on BOTH the disk and the wire.
	OnCriticalPath *bool    `json:"onCriticalPath,omitempty"`
	EventTime      *float64 `json:"eventTime,omitempty"` // sim-days; = max predecessor earliestFinish (0 with no preds)
}

// NetworkNodeCompute is the per-activity CPM result the compute-at-read pass derives
// for one dependency-graph node. It mirrors the figures the retired client-side
// toNetworkView produced, now authoritative and server-computed.

// off-CP but within the near-critical float band
// float-criticality band: critical|red|yellow|green
// topological depth (longest-path layer) for the swimlane layout

// NetworkSummary is the project-level CPM roll-up the SPA renders above the graph.

// project duration = longest path
// count of on-CP activities (not the CP day-sum)
// = TotalDurationDays (the longest path is the CP length)
// the loosest slack across all nodes
// off-CP nodes inside the near-critical band

// Kind implements ArtifactModel.
func (n *Network) Kind() ArtifactKind { return KindNetwork }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (n *Network) isArtifactModel() {}

// Solution holds one Phase-2 solution-option artifact — the option's defining knobs
// (staffing cap, calendar, worker-class rates, optional schedule buffer). Duration,
// cost, and risk are NOT stored here: they are computed by the estimate Engines from
// the assembled ProjectOption and joined into the SDP review.
//
// The four solution slots (NormalSolution, DecompressedSolution, SubcriticalSolution,
// CompressedSolution) are the SAME struct distinguished by SlotKind, so the generic
// stageArtifactForReview routing works for all four without a type switch.
// (projectStateAccess.md §3.2 §3.6)

// one of the four KindXxxSolution

// build cost per person-day, by worker class
// schedule buffer (decompressed-normal); 0 otherwise

// NewSolution constructs a Solution for the given slot kind. slotKind must be one of
// the four KindXxxSolution constants.
func NewSolution(slotKind ArtifactKind) *Solution {
	return &Solution{SlotKind: slotKind}
}

// Kind implements ArtifactModel. Returns SlotKind so the generic
// stageArtifactForReview verb can route all four solution slots without a type
// switch. (projectStateAccess.md §3.6 "kind tag")
func (s *Solution) Kind() ArtifactKind { return s.SlotKind }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (s *Solution) isArtifactModel() {}

// RiskRow is the per-option risk decomposition (criticality + activity risk →
// composite) used in the SDP-review time-risk curve.

// RiskModel holds the Phase-2 risk model artifact — the per-option criticality +
// activity risk decomposition. (projectStateAccess.md §3.6)

// Kind implements ArtifactModel.
func (r *RiskModel) Kind() ArtifactKind { return KindRiskModel }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (r *RiskModel) isArtifactModel() {}

// SdpOptionRow is one row of the SDP-review options table — the JOIN of the three
// Engine outputs for one option, flattened to plain values (the Manager joins the
// Engine value objects; this slot model never imports the Engine output types, which
// would be an upward dependency).

// estimationEngine: construction-side duration
// estimationEngine: construction-side build cost
// estimationEngine: composite construction risk
// operationEstimationEngine: operation cost at declared load
// operationEstimationEngine: payout(+)/shortfall(-) forecast
// settlementEngine: projected revenue-share regime rate

// SdpReview holds the Phase-2 SDP review artifact — the options table (the four joined
// rows) plus the architect's recommendation. This is the model surfaced at the
// option-commitment gate. (projectStateAccess.md §3.6, projectDesignManager.md §6.3)

// the option the assembly recommends

// Kind implements ArtifactModel.
func (s *SdpReview) Kind() ArtifactKind { return KindSdpReview }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (s *SdpReview) isArtifactModel() {}

// Layer is the closed, ordered layer set per ch. 3 of The Method.
// Manager and Engine share the "Business Logic" rank: a Manager→Engine edge is
// DOWNWARD, not sideways. (projectStateAccess.md §3.3)

// rank 0
// rank 1  (Business Logic)
// rank 1  (Business Logic) — same rank as Manager
// rank 2
// rank 3
// utilities bar — spans all ranks, callable by anyone

// Rank collapses Manager+Engine so legality predicates treat M→E as downward.
// Returns -1 for Utility (rank-less, excluded from up/down legality checks).
// (projectStateAccess.md §3.3)
func (l Layer) Rank() int {
	switch l {
	case LayerClient:
		return 0
	case LayerManager, LayerEngine:
		return 1
	case LayerResourceAccess:
		return 2
	case LayerResource:
		return 3
	case LayerUtility:
		// Rank-less by design (spans all ranks) — same as the default below.
		return -1
	default:
		return -1 // Utility: rank-less, excluded from up/down legality
	}
}

// ComponentKind is the closed component taxonomy per ch. 3 of The Method.
// Naming conventions and distinguishing attributes are documented as invariants;
// the legality predicates enforcing them live in artifactValidationEngine.
// (projectStateAccess.md §3.3)

// <Noun>Client; a transport entry point
// <Noun>Manager; encapsulates a workflow volatility; "almost expendable"
// <Gerund>Engine; encapsulates an activity volatility; NO I/O
// <Noun>Access; encapsulates a Resource; ops are atomic business verbs
// a physical store / queue / external system
// passes the cappuccino-machine test

// Component is a node in the System static architecture model.
// (projectStateAccess.md §3.3)

// server-assigned Slug(Name); not LLM-authored
// e.g. "ProjectStateAccess"; naming rule per Kind (see below)

// must be the canonical Layer for Kind (checked by NewSystem)
// the volatility this component owns (Manager/Engine/RA); "" for Resource/Utility
// AtomicBusinessVerbs is an ATTRIBUTE OF A RESOURCEACCESS, not a component kind.
// Non-empty only when Kind == CompResourceAccess; lists the verb names.

// CanonicalLayer returns the canonical Layer for a ComponentKind. It is the
// single source of truth for the Kind→Layer derivation: NewSystem uses it to
// enforce the shape invariant, and the systemDesign finalize pass uses it to
// DERIVE Component.Layer server-side (the LLM never emits a layer — it is 100%
// derivable from Kind). (projectStateAccess.md §3.3)
func CanonicalLayer(k ComponentKind) Layer { return canonicalLayer(k) }

// canonicalLayer returns the canonical Layer for a ComponentKind.
// Used by NewSystem to enforce the shape invariant that a component's Layer
// matches its Kind. (projectStateAccess.md §3.3 "NewSystem validates … canonical layer")
func canonicalLayer(k ComponentKind) Layer {
	switch k {
	case CompClient:
		return LayerClient
	case CompManager:
		return LayerManager
	case CompEngine:
		return LayerEngine
	case CompResourceAccess:
		return LayerResourceAccess
	case CompResource:
		return LayerResource
	case CompUtility:
		// The zero value — same as the default below.
		return LayerUtility
	default:
		return LayerUtility
	}
}

// CallMode is the closed edge-mode set. (projectStateAccess.md §3.3)

// synchronous, in-process method call
// queued (the closed-layer M→M sideways exception)
// event / pub-sub (only Clients & Managers may publish/subscribe)

// Relationship is a directed edge between two Components in the System model.
// (projectStateAccess.md §3.3)

// destination-layer vocabulary (STRUCTURIZR-CONVENTIONS "Edge-label conventions")

// DynamicView is one call-chain realization per use case (ch. 4): STEP-KEYED, one
// CallStep per realized activity node (Grammar B ActivityNode.ID), each step naming
// the Relationship calls dispatched at that node. Participants are DERIVED from the
// steps' call endpoints — there is no separate participants list; each endpoint
// resolves to either a Component.ID or an Actor.ID owned by the view's use case.
// Maps 1:1 to a Structurizr dynamic view on render via artifactRenderingAccess.
// (projectStateAccess.md §3.3)

// links to a UseCase (Grammar B)
// stable view key, e.g. "uc1-coauthor-method-artifact"

// CallStep.ActivityNodeID; CallStep.Calls Mode ∈ {CallSync, CallQueued}; ordered

// ParticipantIDs (the derived-participants helper over Steps/Calls) was RETIRED
// 2026-07-30 (callchain-realization Task 6) along with its sole caller,
// systemdesign's dvChainFindings (DV-CHAIN-CONNECTED) — the rule moved to platform
// methodcheck as CC-PATH-CONNECTED, which derives participants itself.

// System is the canonical typed static-architecture model (Grammar A, ch. 3/4).
// The .dsl/Structurizr text is a rendering produced by artifactRenderingAccess from
// this model — never stored separately, never the source of truth.
// (projectStateAccess.md §3.3)

// Kind implements ArtifactModel. (projectStateAccess.md §3.3)
func (s *System) Kind() ArtifactKind { return KindSystem }

// isArtifactModel seals the ArtifactModel sum to this package's models.
// (projectStateAccess.md §3.1)
func (s *System) isArtifactModel() {}

// NewSystem constructs a System after validating SHAPE only (not semantic legality).
// Shape checks (projectStateAccess.md §3.3):
//   - each Component.Name must be non-empty
//   - each Component.Layer must be the canonical Layer for its Kind
//     (Client→LayerClient, Manager→LayerManager, Engine→LayerEngine,
//     ResourceAccess→LayerResourceAccess, Resource→LayerResource, Utility→LayerUtility)
//
// Semantic legality (no calling up, no sideways except queued M→M, no layer-skipping,
// pub/sub origin/destination rules, the 12 Design Don'ts, cardinality) are predicates
// in artifactValidationEngine — NOT enforced here.
func NewSystem(components []Component, relationships []Relationship, dynamicViews []DynamicView) (*System, error) {
	for _, c := range components {
		if c.Name == "" {
			return nil, errors.New("projectstate.NewSystem: component Name must not be empty")
		}
		canonical := canonicalLayer(c.Kind)
		if c.Layer != canonical {
			return nil, errors.New("projectstate.NewSystem: component " + c.Name + " has Layer inconsistent with its Kind")
		}
	}
	return &System{
		Components:    components,
		Relationships: relationships,
		DynamicViews:  dynamicViews,
	}, nil
}

// Trigger is the closed 3-set of use-case trigger kinds per ch. 4.
// (projectStateAccess.md §3.4)

// a Client-initiated request
// a scheduled/timer tick
// an inbound bus/queue message

// Classification distinguishes Core from NonCore use cases per ch. 4.
// NO UML include/extend/generalize — the book does not use them.
// (projectStateAccess.md §3.4)

// use VariationOf to link to the core UC it permutes

// ActivityNodeKind is the closed node set the book enumerates plus UML-general
// nodes admitted for repo use and tagged as such. (projectStateAccess.md §3.4)

// ---- book-enumerated (ch. 4 / App C 1c) ----

// a step / action
// guarded outgoing edges

// a.k.a. Partition; Name = role, optional link to actor/component

// ---- UML-general, NOT book-enumerated (admitted for repo use, TAGGED) ----
// UML-general
// UML-general
// UML-general
// UML-general
// UML-general, a UML time event (an elapsed-timer entry point)
// UML-general, a UML accept-event action (a bus/queue-message entry point)

// BookEnumerated reports whether a node kind is in the book's closed set (vs UML-general).
// WARNING: the iota ordering is load-bearing — book-enumerated node kinds must be
// declared before NodeNote; do not insert non-book nodes before it. NodeTimeEvent and
// NodeAcceptEvent are appended AFTER NodeInterruptEdge and are, like it, UML-general
// (NOT book-enumerated); k <= NodeNote already excludes them, so no change to the
// comparison itself is needed.
// (projectStateAccess.md §3.4)
func (k ActivityNodeKind) BookEnumerated() bool { return k <= NodeNote }

// ActivityNode is a node in an ActivityDiagram.
//
// NAME-AS-IDENTITY (2026-06-04): ID is a server-assigned SLUG of the node Label
// (or a positional fallback for unlabeled structural nodes), NOT a UUID and NOT
// LLM-authored. ActivityEdge.From/To carry this same slug. LinkedActorID is a
// NAME-slug reference (the linked Actor's role-slug) resolved server-side from
// the name the LLM emitted.
// (projectStateAccess.md §3.4)
//
// DecidedBy (rollout rulings 2026-07-31): optional, legal ONLY on decision/switch
// kinds — who resolves the branch. Resolves endpoint-style, like a call endpoint:
// a Component.ID or the owning use case's Actor.ID. Illegal placement (any other
// kind) or a value resolving to neither is CC-DECIDED-BY (methodcheck/designhealth,
// not enforced by this write-path shape validator — see requireActivityNodes).
type ActivityNode struct {
	ID    string           `json:"id"`
	Kind  ActivityNodeKind `json:"kind"`
	Label string           `json:"label"`
	// For NodeSwimLane: the role name and an optional link to an actor.
	RoleName      string  `json:"roleName"`
	LinkedActorID *string `json:"linkedActorId"`
	// DecidedBy: see doc comment above.
	DecidedBy *string `json:"decidedBy,omitempty"`
}

// EdgeKind is the closed set of activity-edge kinds. (projectStateAccess.md §3.4)

// plain control flow
// outgoing edge of a Decision; Guard non-empty

// ActivityEdge is a directed edge in an ActivityDiagram.
// (projectStateAccess.md §3.4)

// an ActivityNode.ID (node-label slug)
// an ActivityNode.ID (node-label slug)

// non-empty only for EdgeGuardedFlow

// ActivityDiagram is the activity-diagram model for a UseCase that has nested
// conditions (App C 1c). Required when the use case's flow contains a NodeDecision;
// that rule is enforced by artifactValidationEngine, not here.
// (projectStateAccess.md §3.4)

// Actor is a participant role in a UseCase per ch. 4 (the "Who" of the Four Questions).
// (projectStateAccess.md §3.4)

// server-assigned Slug(Role); not LLM-authored
// the role/actor name

// UseCase is the canonical typed use-case model (Grammar B, ch. 4).
// UseCase and ActivityDiagram are PLAIN VALUE TYPES — they do NOT implement
// ArtifactModel. The slot-level model CoreUseCases (defined in Task 3) holds a
// collection of UseCaseDecision values and implements ArtifactModel with KindCoreUseCases.
//
// Constraints (enforced by artifactValidationEngine, not here):
//   - a CoreUseCases collection must hold 2–6 UseCase values with Classification==ClassCore
//   - any UseCase whose flow contains a NodeDecision must have a non-nil Activity
//
// (projectStateAccess.md §3.4)
type UseCase struct {
	ID             UseCaseID        `json:"id"`   // server-assigned Slug(Name); not LLM-authored
	Name           string           `json:"name"` // the human-readable identity
	Actors         []Actor          `json:"actors"`
	Trigger        Trigger          `json:"trigger"`
	Classification Classification   `json:"classification"`
	VariationOf    *UseCaseID       `json:"variationOf"` // the core UC's id (Slug of its name); set when ClassNonCore
	Activity       *ActivityDiagram `json:"activity"`    // required when the use case has nested conditions (App C 1c)
}

// NewUseCase validates SHAPE only and returns a copy of the UseCase if valid.
// Shape check: Name must be non-empty.
// The "NodeDecision ⇒ non-nil Activity" rule is a SEMANTIC constraint enforced by
// artifactValidationEngine, NOT here.
// (projectStateAccess.md §3.3 "smart constructors enforce SHAPE only")
func NewUseCase(uc UseCase) (*UseCase, error) {
	if uc.Name == "" {
		return nil, errors.New("projectstate.NewUseCase: Name must not be empty")
	}
	out := uc
	return &out, nil
}

// servicecontract.go holds the typed service-contract corpus model for the
// construction head-state. project.json (.serviceContracts) is the OWNER of each
// built component's service contract; the value is a contract DOCUMENT — the same
// shape the per-component `contract.schema.json` carries (a `title`, a `$defs` map
// of JSON Schemas, and an `interface` describing the RPC surface), plus the
// self-describing metadata (component key, Method layer, target Go package) that
// makes the document buildable by modelgen straight from project.json.
//
// DESIGN: data shapes round-trip as raw JSON (json.RawMessage) so the stored
// document is byte-identical across an EncodeProjectJSON → DecodeProjectJSON cycle;
// the Go encoder's MarshalIndent pass re-indents every raw node uniformly, so the
// committed project.json is canonical. The Interface mirrors codegen.Interface but
// keeps each schema node as raw JSON for the same byte-fidelity reason.

// ServiceContract is one component's service contract, stored as a contract
// document in Project.ServiceContracts (keyed by component name). It is the OWNER
// of the contract for the built components — the per-component contract.schema.json
// is a render-on-read of this entry (and is removed in a later stage). Additive,
// nil until seeded.
type ServiceContract struct {
	// Component is the contract key/name (e.g. "artifactAccess").
	Component string `json:"component"`
	// Layer is the Method layer (Client | Manager | Engine | ResourceAccess | Utility).
	Layer string `json:"layer"`
	// GoPackage is the target Go package for codegen (e.g.
	// "internal/resourceaccess/artifact"). Empty for un-migrated stub entries.
	GoPackage string `json:"goPackage,omitempty"`
	// Infra is the approved framework-* infrastructure each ResourceAccess binds to
	// (e.g. ["Git"], ["Postgres"], or both for projectstate). modelgen emits one
	// New<Infra><Component> impl struct + DI constructor per infra. Empty for
	// engines/managers/stubs. Carried on the struct so the codec preserves it across
	// the EncodeProjectJSON → DecodeProjectJSON round-trip.
	Infra []string `json:"infra,omitempty"`
	// Deps is a MANAGER contract's ordered constructor dependency list. Each entry is
	// either a COMPONENT dep ({name, component} — `component` is the dependency's
	// contract key, resolved by modelgen to its published interface type) or a PLAIN
	// dep ({name, goType[, goImport]} — a non-component constructor param such as a
	// Temporal client, a config scalar, or a resolver func the generator cannot derive
	// from a component). modelgen turns this list into the generated
	// New<Iface>(deps…) <Iface> DI constructor. Empty for engines/RAs/stubs and for
	// un-migrated managers. Carried on the struct so the codec preserves it across the
	// EncodeProjectJSON → DecodeProjectJSON round-trip.
	Deps []ContractDep `json:"deps,omitempty"`
	// Stub marks an unbuilt component whose contract is authored (goPackage + $defs +
	// interface) but whose implementation does not yet exist. modelgen emits a fully
	// generated not-implemented impl (an unexported impl struct + no-arg public
	// constructor whose every method returns the layer's not-implemented error) so the
	// component compiles before it is built; as it is constructed later the generated
	// bodies are replaced and this flag is cleared. Built entries omit it.
	Stub bool `json:"stub,omitempty"`
	// RailAuthority marks a ResourceAccess as the merge-authority for its writes: its
	// non-read-only ops stay AgentHidden in the internal MCP tool surface (internaltoolsgen),
	// replaced for agents by composed Manager verbs.
	RailAuthority bool `json:"railAuthority,omitempty"`
	// Title is the contract document title (e.g. "artifact contract").
	Title string `json:"title"`
	// Defs is the document's `$defs` — each value is a JSON Schema, stored raw so
	// it round-trips exactly. Omitted when empty (un-migrated stubs).
	Defs map[string]json.RawMessage `json:"$defs,omitempty"`
	// Interface is the component's interface (the RPC surface): name, layer, ops.
	Interface ContractInterface `json:"interface"`
	// Notes is freeform human-authored commentary attached to the contract entry
	// (e.g. drift/annotation notes from a review pass). It is NEVER produced by
	// schemagen and is not part of the contract document proper (title/$defs/
	// interface) — contractfold preserves it byte-for-byte across a fold rather
	// than replacing it. Empty/absent for entries with no notes.
	Notes string `json:"notes,omitempty"`
}

// ContractDep is one manager constructor dependency stored in a MANAGER
// ServiceContract's Deps. A COMPONENT dep names a dependency contract by its key in
// Component (resolved by modelgen to that component's published interface type); a
// PLAIN dep instead carries an explicit GoType (+ optional GoImport) for a
// constructor param that is not itself a component (a Temporal client, a config
// scalar, a resolver func). Exactly one of Component / GoType is set.
type ContractDep struct {
	// Name is the constructor parameter name (and the builder argument name).
	Name string `json:"name"`
	// Component is the dependency's contract key (COMPONENT dep). Empty for a PLAIN dep.
	Component string `json:"component,omitempty"`
	// GoType is the verbatim Go type of a PLAIN dep. Empty for a COMPONENT dep.
	GoType string `json:"goType,omitempty"`
	// GoImport is the single import path a PLAIN dep's GoType needs (optional).
	GoImport string `json:"goImport,omitempty"`
}

// ContractInterface mirrors codegen.Interface: the generated Go interface's name,
// its Method layer, and its operations.

// ContractOperation is one method on the interface: its name, ordered parameters,
// an optional result schema, and whether it returns an error.
type ContractOperation struct {
	Name string `json:"name"`
	// Params is the ordered parameter list. A nil slice encodes as `null` (no
	// omitempty) to match the contract-document shape codegen writes.
	Params []ContractParam `json:"params"`
	// Result is the result type's JSON Schema node (raw); omitted when the op has
	// no result.
	Result json.RawMessage `json:"result,omitempty"`
	Error  bool            `json:"error"`
	// UI optionally marks this operation as rendering an MCP App view.
	// Ops without it stay plain tools. See docs/superpowers/specs/2026-07-13-mcp-apps-design.md §3.5.
	UI *OpUI `json:"ui,omitempty"`
}

// OpUI is the MCP-Apps view annotation on a service-contract operation.
type OpUI struct {
	// View is the webApp view-registry id rendered when this op's tool fires.
	View string `json:"view"`
}

// ContractParam is one operation parameter. Schema is a JSON Schema node (raw) —
// either a `$ref` into the contract's `$defs` or an inline schema. Pointer marks a
// nullable pointer parameter.
type ContractParam struct {
	Name    string          `json:"name"`
	Pointer bool            `json:"pointer,omitempty"`
	Schema  json.RawMessage `json:"schema"`
}

// phasearchitects.go holds the typed named artifact and testing-state records
// owned by the projectstate RA (feedback_method_models_owned_by_ra). All records
// are plain Go structs with json tags; they live in project.json under
// .phaseArtifacts and .testingState respectively.

// --- Phase artifact records ---

// SRSRecord is the Requirements phase artifact for a service or deployment activity.

// TestPlanRecord is the TestPlan phase artifact (per-service/frontend slice).
// Author per Correction 1: the constructing developer (junior under senior hand-off),
// NOT the test-engineer. System-level test activities use TestingState.SystemTestPlan.

// IntegrationNoteRecord is the Integration phase artifact produced when the
// senior-developer integrates the component and merges the integration PR.

// UXRequirementsRecord is the Requirements phase artifact for frontend activities.

// UIDesignRecord is the DetailedDesign phase artifact for frontend activities
// (UI designs, wireframes, component specs). Review: founder + ux-reviewer + PM + architect.

// ProvisioningSpecRecord is the Requirements phase artifact for deployment activities.

// DeployNoteRecord is the Integration phase artifact for deployment activities
// (convergence verification output).

// DocOutlineRecord is the Requirements phase artifact for documentation activities.

// DocNoteRecord is the Integration phase artifact for documentation activities
// (review completion note).

// PhaseArtifacts holds all phase-scoped artifacts produced during Phase-3 construction.
// Keyed by component/surface/resource/doc name (the same key used in ServiceContracts).
// Additive: nil until the first RecordPhaseArtifactProduced call.

// --- Testing state records (§1c / design §2.3) ---

// QualityGate is one human-escalation gate defined by the N-QA activity (§4).
// When the construction Manager encounters a gate matching the current activity+phase,
// it consults interventionEngine: Before mode pauses before dispatch; After mode
// pauses after merge; OnReviewFail forces escalate on any review failure.

// e.g. "C-PE" or ActivityType.String()
// ActivityMethodPhase.String()
// "before" | "after" | "onReviewFail"
// "escalate" | "takeover"

// DefectRecord is one defect filed during system testing (N-IT / §1c).
type DefectRecord struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Severity   string     `json:"severity"` // "critical" | "high" | "medium" | "low"
	FiledAt    *time.Time `json:"filedAt,omitempty"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
	Note       string     `json:"note,omitempty"`
}

// TestRun is one system-test execution record (N-IT / §1c).
type TestRun struct {
	ID        string     `json:"id"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Passed    int        `json:"passed"`
	Failed    int        `json:"failed"`
	Note      string     `json:"note,omitempty"`
}

// TestArg is one concrete input argument to a step's operation call: the param
// name, a concrete VALUE (JSON-encoded text, so the N-STH harness generator can emit
// runnable code), and an optional schemaRef naming the contract param's type.
type TestArg struct {
	Name      string `json:"name"`
	Value     string `json:"value"`               // concrete value as JSON/text, e.g. "\"approve\""
	SchemaRef string `json:"schemaRef,omitempty"` // contract param type name ($def), optional
}

// TestExpect is the expected outcome of a step: either an expected result value
// (Result, JSON/text) OR an expected error (ErrorExpected + ErrorCode). A negative
// case asserts the system produces the specific failure; a happy step asserts the
// result. Per Righting Software App A: cover "every parameter, condition, and
// error-handling path".
type TestExpect struct {
	Result        string `json:"result,omitempty"`    // expected result value/shape (empty when error-expected)
	ErrorExpected bool   `json:"errorExpected"`       // true → the call is expected to fail
	ErrorCode     string `json:"errorCode,omitempty"` // expected error code / type
}

// TestStep is one black-box step in a system-test case: a single manager operation
// call with its concrete inputs and expected outcome. It is TRANSPORT-AGNOSTIC — it
// names the {component, operation} (the manager method), NOT an HTTP route, because
// the REST/MCP/(future) gRPC clients are all generated bindings of the same manager
// operation. The N-STH harness generator turns these steps into per-transport code.
type TestStep struct {
	Seq       int        `json:"seq"`                 // 1-based order within the case
	Component string     `json:"component"`           // manager/component that owns the operation
	Operation string     `json:"operation"`           // manager method name (e.g. "startSystemDesign")
	Status    string     `json:"status,omitempty"`    // last-run result: "" | "red" (failing) | "green" (passing)
	Inputs    []TestArg  `json:"inputs,omitempty"`    // concrete input arguments
	Expect    TestExpect `json:"expect"`              // expected result or expected error
	Assertion string     `json:"assertion,omitempty"` // human-readable assertion
}

// TestCase is one falsification attempt within a scenario: an ordered sequence of
// manager-operation calls asserting a specific expected outcome — a happy path, a
// negative (error-path) case, or a boundary case. Per Righting Software ch.14 the
// value of the plan is the adversarial coverage ("all the ways to break the system
// and prove it does not work"), so negative/boundary cases are first-class.
type TestCase struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // "happy" | "negative" | "boundary"
	Title string `json:"title"`
	// Proves is "what this proves" — the failure mode this case is designed to expose.
	Proves          string     `json:"proves,omitempty"`
	ExpectedOutcome string     `json:"expectedOutcome,omitempty"` // overall success OR the specific expected failure
	Steps           []TestStep `json:"steps,omitempty"`
}

// TestScenario is one black-box system-test scenario: a core use case and the set of
// test cases (happy + adversarial) that prove it holds — or catch where it breaks —
// end-to-end, black-box at the client surface.
type TestScenario struct {
	ID      string `json:"id"`
	UseCase string `json:"useCase"` // the core use case this scenario traces to
	Title   string `json:"title"`
	// Description is the "what this proves and why it matters" — the failure modes
	// the scenario is designed to expose (system-level QC per Righting Software
	// ch.14: prove the integrated system fails, don't unit-test in isolation).
	Description string     `json:"description,omitempty"`
	Cases       []TestCase `json:"cases,omitempty"`
}

// SystemTestPlan is the output of the N-STP activity (§1c TestVariantPlan).
type SystemTestPlan struct {
	UseCaseIndex []string       `json:"useCaseIndex,omitempty"` // traced UC ids
	Entries      []string       `json:"entries,omitempty"`      // plan entry descriptions
	Scenarios    []TestScenario `json:"scenarios,omitempty"`    // black-box operation sequences
	Status       string         `json:"status,omitempty"`       // "" | "approved"
	ApprovedAt   *time.Time     `json:"approvedAt,omitempty"`
}

// HarnessModule is the output of the N-STH activity (§1c TestVariantHarness).
type HarnessModule struct {
	RepoRef    string     `json:"repoRef,omitempty"` // corpus path / PR ref
	Status     string     `json:"status,omitempty"`  // "" | "approved"
	ApprovedAt *time.Time `json:"approvedAt,omitempty"`
}

// PerfHarness is the output of the N-PERF activity (§1c TestVariantPerf).
type PerfHarness struct {
	RepoRef    string     `json:"repoRef,omitempty"`
	Status     string     `json:"status,omitempty"`
	ApprovedAt *time.Time `json:"approvedAt,omitempty"`
}

// TestingState holds the project-level testing artifacts produced by N-* activities.
// Additive: nil until the first testing activity produces output.
type TestingState struct {
	SystemTestPlan     *SystemTestPlan `json:"systemTestPlan,omitempty"`
	HarnessModule      *HarnessModule  `json:"harnessModule,omitempty"`
	PerfHarness        *PerfHarness    `json:"perfHarness,omitempty"`
	QualityGates       []QualityGate   `json:"qualityGates,omitempty"`
	QualityAuditReport string          `json:"qualityAuditReport,omitempty"`
	TestRuns           []TestRun       `json:"testRuns,omitempty"`
	Defects            []DefectRecord  `json:"defects,omitempty"`
}

// OperatingModel declares WHO OPERATES the built application — a PROJECT-LEVEL
// choice made at creation (founder ruling 2026-07-05, from live QA: the gtdapp
// deployment artifact drafted an arbitrary AWS EKS/RDS/CloudFront topology, which
// is only legitimate when the customer runs the app themselves).
//
// It is a Method INPUT, deliberately distinguished from the review-gated artifacts:
// it is NOT an ArtifactModel, is NOT held in an ArtifactSlot, and carries NO
// AwaitingReview/Committed lifecycle — a plain, settable field on Project (the same
// posture as ResearchInput / OperatorPaused), set once at creation and read by the
// OperationalConcepts (deployment topology) and PlanningAssumptions (launch
// infrastructure) draft prompts to CONSTRAIN what infrastructure the design may draw.
//
// String-encoded (not an ordinal enum) so the on-disk JSON is self-describing and a
// pre-field project.json decodes cleanly to the DEFAULT (selfOperated) — see
// OrDefault + the decodeProjectDoc default.
type OperatingModel string

const (
	// OperatingModelSelfOperated — the customer runs the built app in their OWN
	// infrastructure (today's behavior; any cloud/infra the design justifies). This
	// is the DEFAULT and the back-compat value for every project that pre-dates the
	// field.
	OperatingModelSelfOperated OperatingModel = "selfOperated"

	// OperatingModelArchistratorOperated — archistrator OPERATES the app on the
	// platform. The design is then CONSTRAINED to the archistrator-platform palette
	// ONLY: CloudNativePG Postgres (framework-go-infrastructure-postgres), Temporal
	// (framework-go-infrastructure-temporal), Keycloak auth
	// (framework-go-infrastructure-keycloak), the otel stack
	// (framework-go-infrastructure-otel), deployed to the platform Kubernetes cluster
	// via the ArgoCD stack at software/k8s. NO AWS RDS/EKS/CloudFront/bespoke cloud —
	// those are self-operated-only.
	OperatingModelArchistratorOperated OperatingModel = "archistratorOperated"
)

// DefaultOperatingModel is the back-compat default applied on read to any project.json
// that pre-dates the field (an empty value). Self-operated preserves today's open
// deployment guidance for every existing project.
const DefaultOperatingModel = OperatingModelSelfOperated

// IsZero reports whether the model is unset (an empty value — a pre-field project).
func (m OperatingModel) IsZero() bool { return m == "" }

// OrDefault returns the model, substituting the DEFAULT (selfOperated) for an empty
// value. Applied on decode so every in-memory Project carries a concrete model and
// every reader (prompts, wire) sees selfOperated for pre-field projects.
func (m OperatingModel) OrDefault() OperatingModel {
	if m == "" {
		return DefaultOperatingModel
	}
	return m
}

// Valid reports whether the model is one of the two known values. Used by the
// SetOperatingModel write pre-condition to reject an unknown wire value.
func (m OperatingModel) Valid() bool {
	switch m {
	case OperatingModelSelfOperated, OperatingModelArchistratorOperated:
		return true
	default:
		return false
	}
}

// ProjectSummary is the catalog row for the landing grid (Task 2.3). It is a
// derived projection of the head-state — NOT a stored shape — returned by
// ListProjects: identity + display fields plus the current-phase progress
// (committed vs total artifact slots) so the grid can render a progress badge
// without loading every project's full slot set.

// committed artifact slots in the current phase
// total required artifact slots in the current phase

// Phase identifies the lifecycle phase the project currently sits in.
// Additive as later Managers come online. (projectStateAccess.md §3.1)

// PhaseSystemDesign is Phase 1 — driven by systemDesignManager.

// PhaseProjectDesign is Phase 2 — reachable once Phase 1 is sealed (advancePhase).

// PhaseConstruction is Phase 3 — reachable once Phase 2 is sealed by
// projectDesignManager.advanceToConstruction (which seals the SDP-review option
// gate). The Phase-3 work itself is owned by the constructionManager; this
// constant only gives AdvancePhase a clean target beyond PhaseProjectDesign so
// the Phase-2 seal increments into a named phase rather than an unnamed ordinal.
// (projectDesignManager.md §2.4 / PHASE NOTE — additive.)

// additive as later phases come online (Operations)

// ArtifactReviewStatus is the per-slot review state in the Project head-state
// aggregate. (projectStateAccess.md §3.1)

// ReviewNone — the slot has never been staged (zero value).

// ReviewAwaitingReview — staged, suspended at the review gate.

// ReviewCommitted — architect approved.

// ReviewRejected — architect rejected (will redraft); model retained for the redraft baseline.

// ReviewWithdrawn — architect abandoned at the gate.

// ArtifactModel is the closed interface every typed Method model implements.
// Kind() lets stageArtifactForReview route a model to its named slot by concrete
// Go type; isArtifactModel() is unexported and seals the sum — only the models
// enumerated in this package (System, and the models in Task 3) satisfy it.
//
// This is NOT an open extension point — extending it would resurrect the retired
// ArtifactSchema volatility. The Method is a stable book; the artifact set is closed.
// (projectStateAccess.md §3.1)
type ArtifactModel interface {
	Kind() ArtifactKind
	isArtifactModel() // unexported: closes the sum to this package's models
}

// ArtifactSlot pairs a review status with the typed model and the architect's
// notes for a single named artifact slot in the Project aggregate.
// (projectStateAccess.md §3.1)
//
// CRITIQUE READ-BACK CARRIER (additive, D-MSD-Δ amendment ratified 2026-06-15).
// CritiqueVerdict + CritiqueNotes are a FIRST-CLASS, optional, defaulted-empty
// read-back location for the PM-critique round-trip the systemDesignManager runs
// before the human gate. They are DISTINCT from Notes (whose frozen meaning —
// architect reject/withdraw rationale — is 100% preserved): the senior review of
// C-MSD-Δ found that overloading Notes as the critique carrier produces a concrete
// misread on the PM-kind reject loop (a RejectArtifact writes slot.Notes, then a
// critique read-back with no intervening Stage would misclassify the architect's
// reject notes as the PM verdict) and that "empty Notes = approve" is ambiguous.
// These dedicated fields remove that overload.
//
//   - SINGLE WRITER: the PM-critique agentic Action, via the committed
//     .aiarch/state/project.json (the slot's critiqueVerdict / critiqueNotes JSON
//     keys). aiarch's server-side thin-write verbs NEVER set them — and
//     StageArtifactForReview / the status-transition verbs CLEAR them (so a stale
//     critique from a prior round can never leak across a redraft/reject loop).
//   - SINGLE READER: the systemDesignManager's readBackCritique (after a critique
//     dispatch reaches PhaseSucceeded). CritiqueVerdict drives the verdict; the
//     "empty Notes = approve" ambiguity is gone — see readBackCritique for the
//     missing-verdict safe-default rule (a critique-expected read-back with an
//     empty verdict is a draft failure, NOT a silent approve).
//
// Defaulted-empty (omitempty in the slot codec) so every existing reader/writer —
// and the out-of-process aiarch-validate CLI decode — is unaffected.

// the canonical typed model; nil only while ReviewNone
// architect rationale; populated by the ResourceAccess layer on Reject/Withdraw

// CritiqueVerdict is the PM-critique read-back verdict for this slot
// ("" | CritiqueVerdictApprove | CritiqueVerdictRevise). Empty == no critique
// committed for the current draft. Written ONLY by the PM-critique Action;
// cleared by StageArtifactForReview / the status-transition verbs.

// CritiqueNotes is the PM-critique read-back revision guidance, carried on a
// Revise verdict. Distinct from Notes (the architect's reject/withdraw rationale).

// Canonical CritiqueVerdict carrier values written into ArtifactSlot.CritiqueVerdict
// by the PM-critique Action and read back by the systemDesignManager. They are the
// projectStateAccess-layer string encoding of the Manager's CritiqueVerdict enum;
// the Manager maps between the two so the typed enum stays the Manager's own surface.
const (
	// CritiqueVerdictApprove ratifies the just-committed draft unchanged.
	CritiqueVerdictApprove = "approve"
	// CritiqueVerdictRevise asks for a redraft; CritiqueNotes carries the guidance.
	CritiqueVerdictRevise = "revise"
)

// Project is the head-state aggregate — the "sane state object" the contract is
// built around. Read whole; never folded. Each Phase-1/2 artifact is a NAMED
// TYPED SLOT, not a map[ArtifactKind]ArtifactRef. Named slots over a map: the
// set of Method artifacts is closed and known (the book defines exactly these),
// so a struct of named fields is the faithful, self-documenting encoding and
// prevents an unknown ArtifactKind ever appearing.
//
// The ArtifactKind enum is retained for the generic write verbs and internal
// slot-by-kind lookup used by the RA implementation. (projectStateAccess.md §3.2)
type Project struct {
	ID      ProjectID
	Version Version
	Phase   Phase

	// Owner is the principal that owns this project — the catalog scope used by
	// ListProjects. Set once at CreateProject; the project is born explicitly with
	// an owner rather than implicitly on first write. (Task 2.3)
	Owner OwnerScope
	// Name is the human-readable project name shown in the landing grid. Set at
	// CreateProject. (Task 2.3)
	Name string

	// OperatingModel declares WHO OPERATES the built app — selfOperated (the
	// customer runs it in their own infra; the DEFAULT + back-compat value) or
	// archistratorOperated (archistrator operates it on the platform, which
	// CONSTRAINS the deployment design to the platform palette). A Method INPUT,
	// NOT an ArtifactModel and NOT review-gated (founder ruling 2026-07-05). Set at
	// creation via SetOperatingModel; read by the OperationalConcepts + Planning
	// Assumptions draft prompts to constrain infrastructure. A pre-field project.json
	// decodes to the DEFAULT (selfOperated) — see decodeProjectDoc.
	OperatingModel OperatingModel

	// Research is the Phase-1 research corpus the system-design sequence STARTS from
	// (➕ 2026-05-29; F42 files-not-JSON 2026-07-05). A Method INPUT, NOT an
	// ArtifactModel and NOT review-gated. The corpus CONTENT lives as files at
	// .aiarch/state/research/<slug>.txt in the project repo (F42): SetResearchInput
	// takes the wire {title,content} but persists ONLY the {Title, Path, ContentBytes}
	// POINTER here (content structurally absent from project.json), writing the file +
	// pointer in ONE atomic commit. Zero value (no Sources) == not yet provided.
	Research ResearchCorpus

	// ActivityGit is the per-activity git-forward head-state, keyed by ActivityID
	// (➕ 2026-06-12, D-PA-GIT, projectStateAccess.md §GIT-HEAD-STATE). Additive,
	// populated only in Phase 3 (nil until the first Record* git verb) — the same
	// posture as the §2-DELTA Construction facet and ResearchInput. The durable
	// mirror of what IPullRequestRail returns; PROVIDER-OPAQUE (opaque String()
	// handles + a typed CI enum, no provider lexeme). Read whole via readProject so
	// the webClient (C-CW-GIT) can project each row onto the ux-mock GitRef.
	ActivityGit map[string]ActivityGitStatus

	// ActivityConstruction is the per-activity construction head-state, keyed by
	// ActivityID (➕ 2026-06-17, Task 1: seed-archistrator-design-state). Additive,
	// populated only in Phase 3 (nil until the first RecordActivityStarted call) —
	// same posture as ActivityGit. Tracks the coarse lifecycle (NotStarted/Running/Done)
	// and server-resolved timestamps for the dry-run construction pump.
	ActivityConstruction map[string]ActivityConstructionStatus

	// ConstructionProgress is the project-level Phase-3 tracking snapshot (ux-mock
	// Tracker framing scalars). Additive, nil until seeded by the bootstrap generator.
	// EV curves are NOT stored here — they are derived at read time from the per-activity
	// status + the network. Only the framing scalars are seeded.
	ConstructionProgress *ConstructionProgress

	// ServiceContracts is the per-component typed service-contract corpus (extracted
	// from the real contract markdown). Additive, keyed by component name, nil until seeded.
	ServiceContracts map[string]ServiceContract

	// PhaseArtifacts holds the typed phase-scoped artifacts produced during Phase-3
	// construction (SRS, test plans, integration notes, UI designs, etc.). Additive,
	// nil until the first RecordPhaseArtifactProduced call.
	PhaseArtifacts *PhaseArtifacts `json:"phaseArtifacts,omitempty"`

	// TestingState holds the project-level testing artifacts produced by N-* activities
	// (system test plan, harness, perf rig, quality gates, test runs, defects). Additive,
	// nil until the first testing activity produces output.
	TestingState *TestingState `json:"testingState,omitempty"`

	// OperatorPaused is set when an operator pauses the project's construction
	// (RecordOperatorPaused). Cleared when construction resumes (not yet a verb in
	// the v1 contract; the field is additive and defaults false).
	OperatorPaused bool
	// PauseReason is the operator-supplied reason for the pause. Empty when not paused.
	PauseReason string

	// ReviewPolicy is the per-project committed configuration of which (activity-type,
	// phase) pairs require human approval during construction. The zero value gates
	// nothing — the construction loop behaves as before this feature was introduced.
	ReviewPolicy ReviewPolicy `json:"reviewPolicy"`

	// ---- Phase 1 slots ----
	Mission              ArtifactSlot // Model is *MissionStatement when populated
	Glossary             ArtifactSlot // Model is *Glossary
	ScrubbedRequirements ArtifactSlot // Model is *ScrubbedRequirements (OQ-2)
	Volatilities         ArtifactSlot // Model is *Volatilities
	CoreUseCases         ArtifactSlot // Model is *CoreUseCases
	SystemDesign         ArtifactSlot // Model is *System (Grammar A)
	OperationalConcepts  ArtifactSlot // Model is *DeploymentOperationsModel
	StandardCheck        ArtifactSlot // Model is *StandardCheck

	// ---- Phase 2 slots (additive; design-only until projectDesignManager is built) ----
	PlanningAssumptions  ArtifactSlot // Model is *PlanningAssumptions
	ActivityList         ArtifactSlot // Model is *ActivityList
	Network              ArtifactSlot // Model is *Network
	NormalSolution       ArtifactSlot // Model is *Solution
	SubcriticalSolution  ArtifactSlot // Model is *Solution
	CompressedSolution   ArtifactSlot // Model is *Solution
	DecompressedSolution ArtifactSlot // Model is *Solution
	RiskModel            ArtifactSlot // Model is *RiskModel
	SdpReview            ArtifactSlot // Model is *SdpReview
}

// Phase-2 estimation INPUT value types (projectStateAccess.md §3.6; projectDesignManager.md §3).
//
// ProjectOption is the value snapshot the projectDesignManager ASSEMBLES from the
// committed Phase-2 head-state slots (PlanningAssumptions + ActivityList + Network +
// the per-option Solution) and feeds BY VALUE to the three estimate Engines
// (estimationEngine, operationEstimationEngine, settlementEngine). It is NOT an
// ArtifactModel and is NOT a stored slot — it never appears in the Project
// aggregate. The Engines read it; they never re-fetch it (Engines do no I/O).
//
// These value types are CANONICAL HERE (the Phase-2 project models owned by
// projectStateAccess) so the downward Engine import (engine → projectstate, the
// same edge artifactValidationEngine already uses) carries them without an upward
// dependency. The Engine OUTPUT value objects (ConstructionEstimate / OperationForecast
// / Projection) live in their owning Engine packages.

// Money is an exact integer-minor-units amount plus an ISO-4217 currency. NEVER a
// float (settlementEngine.md §3). Signed: a positive net is a payout, a negative
// net is a shortfall charge. (A package-local money type for the Phase-2 estimation
// path; the cross-component canonical-Money consolidation into framework-go is a
// noted follow-up, out of scope for C-MPD.)

// signed minor units, e.g. 1299 == 12.99
// ISO-4217, e.g. "USD"

// OptionID identifies one assembled ProjectOption within an SDP review.

// InfrastructureKind is the opaque discriminator the operationEstimationEngine
// pivots on (operationEstimationEngine.md §3). The launch infrastructure is
// Go + Temporal + Postgres; future kinds are additive.

// InfrastructureKindGoTemporalPostgres is the launch infrastructure.

// UsageAssumption is the customer's DECLARED expectation of end-user load, fed to
// operationEstimationEngine.estimateForOption for the operation-side forecast
// (operationEstimationEngine.md §3).

// RevenueShareKind is the closed set of aiarch revenue-share regimes
// (settlementEngine.md §3). Launch is a flat 10% cut.

// ComputeCostKind is the closed set of compute pass-through pricing regimes
// (settlementEngine.md §3).

// ScheduleKind is the settlement cadence (settlementEngine.md §3).

// SettlementTerms is the customer's settlement-terms snapshot carried BY VALUE on
// the option (settlementEngine.md §3; operationEstimationEngine OQ-2/FU-OE-A — the
// option carries the terms). settlementEngine.projectCommitTimeRevenueShareAndComputeCost
// reads only this.

// e.g. 10.0 for launch flat 10%

// markup on metered compute cost

// OptionActivity is one activity in an option's CPM network — effort in 5-day
// quanta, its worker class, whether it sits on the critical path, and its
// Fibonacci risk bucket. (Named OptionActivity to avoid colliding with the
// activity-diagram ActivityNode in usecase.go.)

// 1,2,3,5,8,13 (Fibonacci) — higher == riskier

// ActivityNetwork is the option's activity graph as the Engine needs it: the flat
// activity set with effort, worker class, critical-path membership and risk bucket.

// WorkerMix is the option's worker-class build-cost rates (per person-day) plus
// the staffing cap that bounds parallelism.

// build cost per person-day, by worker class
// max concurrent staff (parallelism bound)

// ProjectOption is one of the four assembled solution options (normal /
// decompressed-normal / subcritical / compressed). The Manager assembles it from
// the committed Phase-2 slots and feeds it by value to the three Engines.

// one of the four KindXxxSolution

// ResearchCorpus is the PERSISTED Phase-1 research corpus on Project (F42 files-not-JSON,
// founder ruling 2026-07-05). Unlike the ResearchInput verb INPUT (which carries the raw
// {Title, Content}), the persisted corpus stores only a POINTER per source — the CONTENT
// lives as a file at .aiarch/state/research/<slug>.txt in the project repo, NOT inside
// project.json. SetResearchInput writes the file + this pointer in ONE atomic commit.
type ResearchCorpus struct {
	Sources []ResearchSourceRef `json:"sources"`
}

// ResearchSourceRef is one persisted research pointer: the human Title, the repo-relative
// Path of the corpus file (e.g. ".aiarch/state/research/00-founder-brief.txt" — the
// drafting Action reads it straight off the checked-out repo), and ContentBytes (the byte
// size, so the read model can show "N KB loaded" without shipping the corpus). The raw
// content is deliberately ABSENT — it is structurally gone from project.json (F42/F22).
// JSON keys are lowerCamel per the project.json schema-first casing convention (QA
// defect fix 2026-07-16); project.json documents written before the fix carry the
// legacy capitalized keys ("Sources"/"Title"/"Path"/"ContentBytes") and still decode —
// Go's json.Unmarshal matches field names case-insensitively (regression-tested in
// access_test.go Test_ResearchCorpus_LegacyCapitalizedKeysStillDecode).
type ResearchSourceRef struct {
	Title        string `json:"title"`
	Path         string `json:"path"`
	ContentBytes int64  `json:"contentBytes"`
}

// IsZero reports whether the persisted corpus is unprovided (no sources).
func (r ResearchCorpus) IsZero() bool { return len(r.Sources) == 0 }

// researchDir is the corpus-file directory, RELATIVE to statePathPrefix (.aiarch/state).
// Files ride the projectstate substrate — one atomic CommitSubtree, the same idempotency
// ledger — so no CommitManagedFiles allowlist applies (F42).
const researchDir = "research"

// researchFileRel returns the corpus file key RELATIVE to statePathPrefix for source
// index i with the given title: "research/<NN>-<slug>.txt". The zero-padded index makes
// it deterministic + collision-free even when two sources share a title/slug.
func researchFileRel(i int, title string) string {
	return fmt.Sprintf("%s/%02d-%s.txt", researchDir, i, researchSlug(title))
}

// researchPath returns the REPO-RELATIVE path stored in a ResearchSourceRef (prefixed with
// statePathPrefix), so the drafting Action can open it directly from the repo root.
func researchPath(i int, title string) string {
	return statePathPrefix + "/" + researchFileRel(i, title)
}

// researchSlug lowercases a title and collapses every run of non-alphanumeric characters to
// a single "-", trimming leading/trailing dashes. An empty/symbol-only title yields "source".
func researchSlug(title string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "source"
	}
	return s
}

// ResearchInput is the Phase-1 research corpus the system-design sequence STARTS
// from — the founder brief, competitor analysis, and customer interviews that
// conceptually populate designs/<product>/research/ (projectStateAccess.md §3.8,
// rework-2026-05-29 §2.6).
//
// It is a Method INPUT, deliberately distinguished from the seven co-authored,
// review-gated Method artifacts:
//   - It does NOT implement ArtifactModel (no Kind(), no isArtifactModel()) — it
//     is not part of the closed artifact sum.
//   - It is NOT held in an ArtifactSlot and carries NO ArtifactReviewStatus —
//     there is no AwaitingReview/Committed/Rejected/Withdrawn lifecycle. The
//     architect does not draft it, the PM does not ratify it, the human does not
//     commit it.
//   - It is a plain field on Project (§3.2), set via setResearchInput, read whole
//     via readProject.
//
// The shape is intentionally minimal and design-level — its exact internal layout
// is construction-refinable. The frozen surface is the field + the verb +
// read-whole; not the precise field set.

// Sources is the set of named research documents/sources feeding Phase-1.
// Zero value (no Sources) == not yet provided.

// ResearchSource is one named research document/source feeding Phase-1 system
// design. Title is human-meaningful; Content is the corpus text the mission-draft
// prompt consumes (or a reference resolvable at construction time — refinable).

// IsZero reports whether the ResearchInput is unprovided (no Sources). The
// setResearchInput pre-condition rejects a zero value (projectStateAccess.md §2).
func (r ResearchInput) IsZero() bool { return len(r.Sources) == 0 }

// toolpalette.go — the INTERNAL MCP tool surface for archistrator's own
// ResourceAccess and Engine contracts.
//
// DOCTRINE (agentic-managers spec item 3 + rule 3). Every ResourceAccess/Engine
// contract operation is TOOL-ELIGIBLE: it has a GENERATED internal MCP tool
// (toolcatalog.gen.go, emitted from .serviceContracts by cmd/internaltoolsgen).
// This surface is INTERNAL — it is NEVER part of the public OAS (cmd/clientgen);
// it is the tool catalog aiarch-state-mcp registers inside a design/construction
// GitHub job. aiarch-state-mcp exposes the non-hidden read-only + Engine tools in
// every per-mode set on top of the composed verbs (AgentHidden ops stay refused).
//
// The composed verbs (putDraftModel, publishDraft, respondToReviewComment,
// setCritiqueVerdict, getCommittedSlot, the research reads, reconcile) sit ON TOP
// of this raw surface: doctrine rule 3 says an invariant compiles into a composed
// verb, and a composed verb may SHADOW/replace the raw generated equivalent where
// the raw op would be unsafe for an agent. Ops that stay server-rail-only even
// though they are generated are marked AgentHidden (see cmd/internaltoolsgen) —
// e.g. every raw projectStateAccess WRITE (CommitArtifact/AdvancePhase/… — merge
// authority stays with the server rail; the composed verbs are the agent surface).
//
// projectstate is the home of this surface because it already OWNS the typed
// contract corpus (ServiceContract) AND the typed System model (dynamic views +
// static edges) the palette resolver reads — the same category as the pure
// derivation helpers (CommandFor, DeriveKind, ClassifyType) it already shares
// downward with the Managers. No client/manager import is introduced.

// InternalTool is one generated internal MCP tool descriptor: the durable,
// platform-neutral surface for a single ResourceAccess/Engine contract operation.
// It is DATA (schemas carried raw so they round-trip byte-for-byte); it binds no
// implementation — a design job registers it from this descriptor, and the
// construction rail (a later priority) attaches the executing handler.
type InternalTool struct {
	// Name is the MCP tool name: the owning component's base with its layer
	// suffix (Access/Engine) stripped, lowerFirst, + the operation name — e.g.
	// projectStateAccess.ReadProject → "projectStateReadProject".
	Name string `json:"name"`
	// Component is the owning contract key in .serviceContracts (e.g.
	// "projectStateAccess") — the target component the tool operates.
	Component string `json:"component"`
	// Layer is the Method layer of the owning component ("ResourceAccess" | "Engine").
	Layer string `json:"layer"`
	// Operation is the contract operation's Go method name (e.g. "ReadProject").
	Operation string `json:"operation"`
	// Params is the operation's business parameter names IN DECLARATION ORDER —
	// the ambient leading call Context (fwra.Context / fweng.Context) is NOT
	// included (it is not a business parameter and never appears in InputSchema).
	// The execution rail (cmd/aiarch-state-mcp) uses this ordered list to bind a
	// tool call's named arguments to the live Go method's positional parameters;
	// the order mirrors the schema-first Go signature the contract was generated
	// from, so args[Params[i]] decodes into method parameter i+1 (i+1 skips the
	// bound receiver's leading Context).
	Params []string `json:"params"`
	// ReadOnly is the MCP readOnlyHint. Every Engine operation is read-only
	// (Engines are pure, side-effect-free computation); a ResourceAccess
	// operation is read-only iff its name carries a read verb (Get/Read/List/
	// Query/Observe/Retrieve/Fetch). Derived by cmd/internaltoolsgen from the
	// Method naming convention — the contract's honest, existing read/write signal.
	ReadOnly bool `json:"readOnly"`
	// AgentHidden marks a generated op intentionally NOT exposed to agents even
	// though it is generated — its authority stays on the server rail, or a
	// composed verb replaces it. A tool palette may not name an AgentHidden op.
	AgentHidden bool `json:"agentHidden"`
	// Description is the human tool description an agentic consumer reads.
	Description string `json:"description"`
	// InputSchema and OutputSchema are the tool's self-contained JSON Schemas
	// ($defs inlined to the transitive closure the operation references), carried
	// raw so the generated file round-trips byte-for-byte.
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

// InternalToolCatalog returns the full generated internal tool surface for
// archistrator's ResourceAccess + Engine contracts — every operation, INCLUDING
// AgentHidden ones (documentation + the palette lint need the whole set).
func InternalToolCatalog() []InternalTool { return internalToolCatalog() }

// AgentExposableTools returns the catalog minus every AgentHidden op — the
// operations an agentic sub-workflow may be granted. A tool palette may name only
// a tool in this set.
func AgentExposableTools() []InternalTool {
	all := internalToolCatalog()
	out := make([]InternalTool, 0, len(all))
	for _, t := range all {
		if !t.AgentHidden {
			out = append(out, t)
		}
	}
	return out
}

// InternalToolByName looks up a generated tool by its MCP tool name.
func InternalToolByName(name string) (InternalTool, bool) {
	for _, t := range internalToolCatalog() {
		if t.Name == name {
			return t, true
		}
	}
	return InternalTool{}, false
}

// RequireModelFields closes the ZERO-VALUE HOLE in the strict slot codec (F81).
//
// The closed ordinal enums (Layer, ComponentKind, CallMode, Trigger, Classification,
// ActivityNodeKind, …) carry a custom UnmarshalJSON that rejects an unrecognized wire
// name — but encoding/json NEVER invokes UnmarshalJSON for a field that is ABSENT from
// the JSON. An omitted "layer" therefore decodes to Layer(0)==LayerClient with no error,
// and an omitted "kind" to ComponentKind(0)==CompClient. The live F81 failure was a
// System draft that omitted every component's layer: the strict codec silently defaulted
// all 17 components to layer=client, methodcheck passed VACUOUSLY (an all-client system
// violates no layer-interaction rule), machine validation reported 0 ERR, and only an
// unrelated merge conflict prevented a corrupted architecture from committing.
//
// RequireModelFields walks the RAW slot-model JSON and demands that every REQUIRED
// closed-enum / identity field be PRESENT (and, for enum fields, a recognized wire
// value AND — for a component — consistent with its kind). It returns a typed,
// human-actionable error the drafting agent reads and corrects. It is deliberately a
// RAW-JSON pass (not a post-decode struct check) because presence is only observable
// before the struct decode collapses "absent" and "zero" into the same value.
//
// It is wired into BOTH gates that must agree byte-for-byte:
//   - putDraftModel (the MCP write path, agent-facing) — a bad draft is rejected before
//     it can commit, so the agent self-corrects.
//   - decodeSlotsMap (the server read-back codec) — a committed model that would carry a
//     defaulted enum is rejected on read-back with the same strictness, so the write and
//     read paths never disagree (the F36/F66 read-back-parity invariant).
func RequireModelFields(kind ArtifactKind, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	switch kind {
	case KindSystem:
		return requireSystemFields(raw)
	case KindCoreUseCases:
		return requireCoreUseCasesFields(raw)
	case KindStandardCheck:
		return requireStandardCheckFields(raw)
	case KindVolatilities:
		return requireVolatilitiesFields(raw)
	case KindMission, KindGlossary, KindScrubbedRequirements, KindOperationalConcepts,
		KindPlanningAssumptions, KindActivityList, KindNetwork, KindNormalSolution,
		KindSubcriticalSolution, KindCompressedSolution, KindDecompressedSolution,
		KindRiskModel, KindSdpReview:
		// Every other artifact's required-enum surface either has no zero-value hole that
		// silently corrupts a Method rule, or is fully guarded by methodcheck; add a case
		// above as new enum-bearing models acquire a hole.
		return nil
	}
	return nil
}

// requireSystemFields enforces the presence + consistency of the System model's
// closed-enum / identity fields: every component's id/name/kind/layer, every
// relationship's from/to/mode, and every dynamic view's useCaseId (and each of its
// steps' activityNodeId + its calls' from/to/mode). The load-bearing check is
// layer==canonicalLayer(kind): it catches the live F81 case (kind present, layer
// omitted→client) as a mismatch. The both-omitted case (kind AND layer absent → both
// client → self-consistent) is caught by the presence checks below and, at the
// whole-system level, by the SYSTEM-LAYER-DEGENERATE rule.
// requireComponentFields enforces one component's identity + closed-enum + encapsulates
// surface (extracted from requireSystemFields to keep each function's cognitive
// complexity within the linter's floor).
func requireComponentFields(cRaw json.RawMessage, i int) error {
	obj, err := rawObject(cRaw)
	if err != nil {
		return fmt.Errorf("component %d is not a JSON object: %w", i+1, err)
	}
	label := componentLabel(obj, i)
	if err := requireNonEmptyString(obj, "id", label); err != nil {
		return err
	}
	if err := requireNonEmptyString(obj, "name", label); err != nil {
		return err
	}
	if err := requirePresent(obj, "kind", label); err != nil {
		return err
	}
	if err := requirePresent(obj, "layer", label); err != nil {
		return err
	}
	var kind ComponentKind
	if err := json.Unmarshal(obj["kind"], &kind); err != nil {
		return fmt.Errorf("%s has an unrecognized kind: %w — use one of client|manager|engine|resourceAccess|resource|utility", label, err)
	}
	var layer Layer
	if err := json.Unmarshal(obj["layer"], &layer); err != nil {
		return fmt.Errorf("%s has an unrecognized layer: %w — use one of client|manager|engine|resourceAccess|resource|utility", label, err)
	}
	if want := canonicalLayer(kind); layer != want {
		return fmt.Errorf("%s declares layer %q but its kind %q requires layer %q — the layer is 100%% derivable from the kind; set them to match (a missing layer field silently defaults to \"client\", which is the F81 corruption this rejects)",
			label, enumName(layerNames, layer), enumName(componentKindNames, kind), enumName(layerNames, want))
	}
	// SYS-ENCAPSULATES (raw twin). encapsulates is a plain string whose zero value is "" —
	// an omitted field is a silent hole (the component claims to encapsulate nothing).
	// Require the field PRESENT on every component, and NON-EMPTY on the three
	// volatility-owning kinds (Manager/Engine/ResourceAccess), which by definition each name
	// the volatility they own. Client/Resource/Utility legitimately carry "" (a transport
	// entry point, a physical store, a cappuccino-machine utility own no volatility), so
	// their emptiness is surfaced as a read-back FINDING (SYS-ENCAPSULATES) rather than
	// hard-failed here: a committed system may carry empty-encapsulates clients and its
	// reads must never break.
	if err := requirePresent(obj, "encapsulates", label); err != nil {
		return err
	}
	if kind == CompManager || kind == CompEngine || kind == CompResourceAccess {
		if err := requireNonEmptyString(obj, "encapsulates", label); err != nil {
			return fmt.Errorf("%s is a %s and must name the volatility it encapsulates: %w",
				label, enumName(componentKindNames, kind), err)
		}
	}
	return nil
}

func requireSystemFields(raw []byte) error {
	var top struct {
		Components    []json.RawMessage `json:"components"`
		Relationships []json.RawMessage `json:"relationships"`
		DynamicViews  []json.RawMessage `json:"dynamicViews"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("the system model is not a JSON object: %w", err)
	}
	if len(top.Components) == 0 {
		return fmt.Errorf("the system model declares no components; a System must decompose into at least one component")
	}
	for i, cRaw := range top.Components {
		if err := requireComponentFields(cRaw, i); err != nil {
			return err
		}
	}
	for i, rRaw := range top.Relationships {
		if err := requireRelationshipFields(rRaw, fmt.Sprintf("relationship %d", i+1)); err != nil {
			return err
		}
	}
	for i, dvRaw := range top.DynamicViews {
		obj, err := rawObject(dvRaw)
		if err != nil {
			return fmt.Errorf("dynamic view %d is not a JSON object: %w", i+1, err)
		}
		label := fmt.Sprintf("dynamic view %d", i+1)
		if err := requireNonEmptyString(obj, "useCaseId", label); err != nil {
			return err
		}
		if err := requireDynamicViewSteps(obj, label); err != nil {
			return err
		}
	}
	return nil
}

// requireDynamicViewSteps enforces the step-keyed shape introduced by the
// call-chain realization model: every dynamic view step must carry a
// non-empty activityNodeId, and every call on a step must satisfy the same
// from/to/mode contract as a top-level relationship. Split out of
// requireSystemFields to keep that function's cyclomatic complexity within
// the project's gocyclo gate.
func requireDynamicViewSteps(obj map[string]json.RawMessage, label string) error {
	steps, ok := obj["steps"]
	if !ok || isJSONNull(steps) {
		return nil
	}
	var stepRaws []json.RawMessage
	if err := json.Unmarshal(steps, &stepRaws); err != nil {
		return fmt.Errorf("%s steps is not a JSON array: %w", label, err)
	}
	for j, sRaw := range stepRaws {
		sObj, err := rawObject(sRaw)
		if err != nil {
			return fmt.Errorf("%s step %d is not a JSON object: %w", label, j+1, err)
		}
		stepLabel := fmt.Sprintf("%s step %d", label, j+1)
		if err := requireNonEmptyString(sObj, "activityNodeId", stepLabel); err != nil {
			return err
		}
		calls, ok := sObj["calls"]
		if !ok || isJSONNull(calls) {
			continue
		}
		var callRaws []json.RawMessage
		if err := json.Unmarshal(calls, &callRaws); err != nil {
			return fmt.Errorf("%s calls is not a JSON array: %w", stepLabel, err)
		}
		for k, cRaw := range callRaws {
			callLabel := fmt.Sprintf("%s call %d", stepLabel, k+1)
			// Parsed once (requireActivityNodes' idiom: rawObject up front, every
			// subsequent check reads the same obj) rather than letting
			// requireRelationshipFields re-parse cRaw for its own from/to/mode
			// checks.
			cObj, err := rawObject(cRaw)
			if err != nil {
				return fmt.Errorf("%s is not a JSON object: %w", callLabel, err)
			}
			if err := requireRelationshipFieldsObj(cObj, callLabel); err != nil {
				return err
			}
			// TraceCall.Alt (rollout rulings 2026-07-31): optional alt-group tag, not on
			// the shared Relationship shape, so checked here rather than inside
			// requireRelationshipFieldsObj (which also validates top-level relationships
			// that have no alt field). Tolerant: absent is fine; wrong type is an error.
			if err := requireOptionalStringField(cObj, "alt", callLabel); err != nil {
				return err
			}
		}
	}
	return nil
}

// requireRelationshipFields enforces from/to/mode on one Relationship (a top-level edge
// or a dynamic-view edge). mode is the CallMode closed enum whose zero value (CallSync)
// would silently absorb an omitted field. Parses raw once and delegates to
// requireRelationshipFieldsObj; callers that already hold the parsed object (e.g. a
// dynamic-view call that also needs its optional alt field) should call that directly
// instead of re-parsing.
func requireRelationshipFields(raw json.RawMessage, label string) error {
	obj, err := rawObject(raw)
	if err != nil {
		return fmt.Errorf("%s is not a JSON object: %w", label, err)
	}
	return requireRelationshipFieldsObj(obj, label)
}

// requireRelationshipFieldsObj is requireRelationshipFields' check body, split out so a
// caller that already parsed the relationship object (requireDynamicViewSteps, which
// also reads the call's optional alt field) does the from/to/mode checks without a
// second rawObject parse of the same bytes.
func requireRelationshipFieldsObj(obj map[string]json.RawMessage, label string) error {
	if err := requireNonEmptyString(obj, "from", label); err != nil {
		return err
	}
	if err := requireNonEmptyString(obj, "to", label); err != nil {
		return err
	}
	if err := requirePresent(obj, "mode", label); err != nil {
		return err
	}
	var mode CallMode
	if err := json.Unmarshal(obj["mode"], &mode); err != nil {
		return fmt.Errorf("%s has an unrecognized mode: %w — use one of sync|queued|eventPubSub", label, err)
	}
	return nil
}

// requireCoreUseCasesFields enforces the CoreUseCases model's closed-enum surface: every
// use case's trigger + classification (both have real zero values — clientAction / core —
// that would silently absorb an omitted field) and every activity node's / edge's kind.
func requireCoreUseCasesFields(raw []byte) error {
	var top struct {
		Decisions []json.RawMessage `json:"decisions"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("the core use cases model is not a JSON object: %w", err)
	}
	for i, dRaw := range top.Decisions {
		dObj, err := rawObject(dRaw)
		if err != nil {
			return fmt.Errorf("use-case decision %d is not a JSON object: %w", i+1, err)
		}
		ucRaw, ok := dObj["useCase"]
		if !ok || isJSONNull(ucRaw) {
			return fmt.Errorf("use-case decision %d is missing its required \"useCase\" object", i+1)
		}
		uc, err := rawObject(ucRaw)
		if err != nil {
			return fmt.Errorf("use-case decision %d useCase is not a JSON object: %w", i+1, err)
		}
		label := useCaseLabel(uc, i)
		if err := requirePresent(uc, "trigger", label); err != nil {
			return err
		}
		var trig Trigger
		if err := json.Unmarshal(uc["trigger"], &trig); err != nil {
			return fmt.Errorf("%s has an unrecognized trigger: %w — use one of clientAction|timer|busMessage", label, err)
		}
		if err := requirePresent(uc, "classification", label); err != nil {
			return err
		}
		var class Classification
		if err := json.Unmarshal(uc["classification"], &class); err != nil {
			return fmt.Errorf("%s has an unrecognized classification: %w — use one of core|nonCore", label, err)
		}
		// UC-ACT-PRESENT (promoted 2026-07-05 from the advisory USECASE-ACTIVITY-MISSING
		// read-back finding to a WRITE-PATH block). The strict codec previously SKIPPED a
		// null activity here, letting a diagram-less use case commit. Every use case — core
		// AND nonCore variation — must now carry a non-null activity diagram with at least
		// one ENTRY (a start node, or a timeEvent/acceptEvent node with no incoming edge —
		// tier parity with methodcheck's activityHasEntryAndAction, framework-go/methodcheck/
		// rules_statevalidation.go, ratified 2026-07-30) and one action step;
		// requireActivityFields enforces the floor.
		act, ok := uc["activity"]
		if !ok || isJSONNull(act) {
			return fmt.Errorf("%s is missing its required activity diagram (activity is null); every use case must carry a non-empty activity diagram with an entry (a start node, or an edge-less timeEvent/acceptEvent) and at least one action step", label)
		}
		if err := requireActivityFields(act, label); err != nil {
			return err
		}
	}
	return nil
}

// requireActivityFields enforces the kind enum on every node and edge of a use case's
// activity diagram (ActivityNodeKind / EdgeKind both have real zero values — start /
// controlFlow — that would silently absorb an omitted field).
func requireActivityFields(raw json.RawMessage, ucLabel string) error {
	act, err := rawObject(raw)
	if err != nil {
		return fmt.Errorf("%s activity is not a JSON object: %w", ucLabel, err)
	}
	hasEntry, hasAction, err := requireActivityNodes(act, ucLabel)
	if err != nil {
		return err
	}
	// UC-ACT-PRESENT floor: a non-empty activity diagram carries at least one ENTRY — a
	// start node, OR a timeEvent/acceptEvent node with no incoming edge (tier parity with
	// methodcheck's activityHasEntryAndAction, framework-go/methodcheck/
	// rules_statevalidation.go, ratified 2026-07-30) — and one action step (App C 1c). The
	// write-path twin of the read-back activityDefect classifier in the systemdesign
	// Manager.
	if !hasEntry || !hasAction {
		return fmt.Errorf("%s activity diagram is structurally empty: it must contain at least one entry — a start node or an edge-less timeEvent/acceptEvent — and at least one action step", ucLabel)
	}
	return requireActivityEdges(act, ucLabel)
}

// requireActivityNodes validates every node's kind enum and reports whether the diagram
// carries an ENTRY (a start node, or a timeEvent/acceptEvent node with no incoming edge —
// see activityIncomingEdgeCounts) and an action node (the UC-ACT-PRESENT floor inputs).
func requireActivityNodes(act map[string]json.RawMessage, ucLabel string) (hasEntry, hasAction bool, err error) {
	incoming := activityIncomingEdgeCounts(act)
	var nodeRaws []json.RawMessage
	if nodes, ok := act["nodes"]; ok && !isJSONNull(nodes) {
		if e := json.Unmarshal(nodes, &nodeRaws); e != nil {
			return false, false, fmt.Errorf("%s activity nodes is not a JSON array: %w", ucLabel, e)
		}
	}
	for i, nRaw := range nodeRaws {
		obj, e := rawObject(nRaw)
		if e != nil {
			return false, false, fmt.Errorf("%s activity node %d is not a JSON object: %w", ucLabel, i+1, e)
		}
		label := fmt.Sprintf("%s activity node %d", ucLabel, i+1)
		if e := requirePresent(obj, "kind", label); e != nil {
			return false, false, e
		}
		var nk ActivityNodeKind
		if e := json.Unmarshal(obj["kind"], &nk); e != nil {
			return false, false, fmt.Errorf("%s has an unrecognized kind: %w", label, e)
		}
		if nk == NodeStart {
			hasEntry = true
		}
		if nk == NodeTimeEvent || nk == NodeAcceptEvent {
			var id string
			_ = json.Unmarshal(obj["id"], &id)
			if incoming[id] == 0 {
				hasEntry = true
			}
		}
		if nk == NodeAction {
			hasAction = true
		}
		// ActivityNode.DecidedBy (rollout rulings 2026-07-31): optional endpoint-resolving
		// string. Tolerant: absent is fine (old committed nodes have no decidedBy); wrong
		// type is an error. Legality (decision/switch-only, resolves to a real endpoint) is
		// CC-DECIDED-BY (methodcheck/designhealth), not this write-path shape validator.
		if e := requireOptionalStringField(obj, "decidedBy", label); e != nil {
			return false, false, e
		}
	}
	return hasEntry, hasAction, nil
}

// activityIncomingEdgeCounts returns, for each node ID targeted by an edge's "to", the
// number of edges pointing at it — used only to detect an edge-less UML event node (the
// entry alternative to a literal start node; tier parity with methodcheck's
// activityHasEntryAndAction, framework-go/methodcheck/rules_statevalidation.go, ratified
// 2026-07-30). Tolerant of a missing, null, or malformed edges array: the authoritative
// edge SHAPE validation is requireActivityEdges, called after the structural
// (entry+action) check succeeds, so a malformed edges array still surfaces its own error
// there.
func activityIncomingEdgeCounts(act map[string]json.RawMessage) map[string]int {
	incoming := map[string]int{}
	edges, ok := act["edges"]
	if !ok || isJSONNull(edges) {
		return incoming
	}
	var edgeRaws []json.RawMessage
	if err := json.Unmarshal(edges, &edgeRaws); err != nil {
		return incoming
	}
	for _, eRaw := range edgeRaws {
		obj, err := rawObject(eRaw)
		if err != nil {
			continue
		}
		var to string
		if err := json.Unmarshal(obj["to"], &to); err != nil {
			continue
		}
		incoming[to]++
	}
	return incoming
}

// requireActivityEdges validates every edge's kind enum and enforces UC-GUARD-LABEL (a
// guardedFlow edge must carry non-empty guard text).
func requireActivityEdges(act map[string]json.RawMessage, ucLabel string) error {
	edges, ok := act["edges"]
	if !ok || isJSONNull(edges) {
		return nil
	}
	var edgeRaws []json.RawMessage
	if err := json.Unmarshal(edges, &edgeRaws); err != nil {
		return fmt.Errorf("%s activity edges is not a JSON array: %w", ucLabel, err)
	}
	for i, eRaw := range edgeRaws {
		obj, err := rawObject(eRaw)
		if err != nil {
			return fmt.Errorf("%s activity edge %d is not a JSON object: %w", ucLabel, i+1, err)
		}
		label := fmt.Sprintf("%s activity edge %d", ucLabel, i+1)
		if err := requirePresent(obj, "kind", label); err != nil {
			return err
		}
		var ek EdgeKind
		if err := json.Unmarshal(obj["kind"], &ek); err != nil {
			return fmt.Errorf("%s has an unrecognized kind: %w", label, err)
		}
		// UC-GUARD-LABEL: a guardedFlow edge (the outgoing edge of a decision) must carry
		// non-empty guard text — an unlabeled guard makes the branch condition unreadable.
		// Plain controlFlow edges carry no guard.
		if ek == EdgeGuardedFlow {
			if err := requireNonEmptyString(obj, "guard", label); err != nil {
				return fmt.Errorf("%s is a guardedFlow edge and must carry non-empty guard text: %w", label, err)
			}
		}
	}
	return nil
}

// requireStandardCheckFields enforces STD-STATUS-EXPLICIT: every standard-check item
// must emit its status EXPLICITLY. CheckStatus's zero value is CheckPass, so an omitted
// "status" silently reads as PASS (the F81 class) — a failing or waived guideline would
// masquerade as satisfied. Demand the field present and a recognized enum on every item.
func requireStandardCheckFields(raw []byte) error {
	var top struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("the standard-check model is not a JSON object: %w", err)
	}
	for i, iRaw := range top.Items {
		obj, err := rawObject(iRaw)
		if err != nil {
			return fmt.Errorf("standard-check item %d is not a JSON object: %w", i+1, err)
		}
		label := checkItemLabel(obj, i)
		if err := requirePresent(obj, "status", label); err != nil {
			return err
		}
		var st CheckStatus
		if err := json.Unmarshal(obj["status"], &st); err != nil {
			return fmt.Errorf("%s has an unrecognized status: %w — use one of pass|waived|fail", label, err)
		}
	}
	return nil
}

// requireVolatilitiesFields enforces VOL-AXIS-EXPLICIT: every volatility must emit its
// axis EXPLICITLY. Axis's zero value is AxisSameCustomerOverTime, so an omitted "axis"
// silently reads as that axis (the F81 class) — a volatility placed on the wrong axis
// masquerades as deliberately placed. Demand the field present and a recognized enum on
// every item.
//
// It also guards the OPTIONAL rejected[] roster (the ch. 2 false-volatility record):
// each rejected candidate must carry a non-empty name, a non-empty reason, and an
// EXPLICIT recognized class — RejectionClass's zero value is variableNotVolatile, so an
// omitted "class" would silently file every rejection under that filter (the same F81
// zero-value hole axis closes above). An absent/null rejected field stays legal: older
// committed Volatilities carry no rejected roster and must keep decoding.
func requireVolatilitiesFields(raw []byte) error {
	var top struct {
		Items    []json.RawMessage `json:"items"`
		Rejected []json.RawMessage `json:"rejected"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("the volatilities model is not a JSON object: %w", err)
	}
	for i, iRaw := range top.Items {
		obj, err := rawObject(iRaw)
		if err != nil {
			return fmt.Errorf("volatility %d is not a JSON object: %w", i+1, err)
		}
		label := volatilityLabel(obj, i)
		if err := requirePresent(obj, "axis", label); err != nil {
			return err
		}
		var ax Axis
		if err := json.Unmarshal(obj["axis"], &ax); err != nil {
			return fmt.Errorf("%s has an unrecognized axis: %w — use one of sameCustomerOverTime|allCustomersAtOneTime", label, err)
		}
	}
	for i, rRaw := range top.Rejected {
		obj, err := rawObject(rRaw)
		if err != nil {
			return fmt.Errorf("rejected volatility %d is not a JSON object: %w", i+1, err)
		}
		label := rejectedVolatilityLabel(obj, i)
		if err := requireNonEmptyString(obj, "name", label); err != nil {
			return err
		}
		if err := requireNonEmptyString(obj, "reason", label); err != nil {
			return err
		}
		if err := requirePresent(obj, "class", label); err != nil {
			return err
		}
		var rc RejectionClass
		if err := json.Unmarshal(obj["class"], &rc); err != nil {
			return fmt.Errorf("%s has an unrecognized class: %w — use one of variableNotVolatile|natureOfTheBusiness|speculative|foldedInto", label, err)
		}
	}
	return nil
}

// ---- raw-JSON presence helpers ----

// rawObject decodes one JSON value into a key→raw map, so we can test key presence.
func rawObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// requirePresent asserts key exists and is not JSON null.
func requirePresent(obj map[string]json.RawMessage, key, label string) error {
	v, ok := obj[key]
	if !ok || isJSONNull(v) {
		return fmt.Errorf("%s is missing required field %q — the strict codec would silently default the absent field to its enum zero value; emit an explicit value", label, key)
	}
	return nil
}

// requireNonEmptyString asserts key exists and is a non-empty (trimmed) JSON string.
func requireNonEmptyString(obj map[string]json.RawMessage, key, label string) error {
	if err := requirePresent(obj, key, label); err != nil {
		return err
	}
	var s string
	if err := json.Unmarshal(obj[key], &s); err != nil {
		return fmt.Errorf("%s field %q must be a string: %w", label, key, err)
	}
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s field %q must not be empty", label, key)
	}
	return nil
}

// requireOptionalStringField asserts that IF key is present and non-null, its value
// decodes as a JSON string. Absent or null is tolerant (the field is optional on the
// wire — old committed data without it must decode unchanged); only a wrong TYPE is
// an error. Used for the call-chain realization's optional endpoint-resolving fields
// (TraceCall.Alt, ActivityNode.DecidedBy) — neither has a real zero value to silently
// absorb an omission, so unlike requirePresent's fields there is nothing to enforce
// here beyond shape.
func requireOptionalStringField(obj map[string]json.RawMessage, key, label string) error {
	v, ok := obj[key]
	if !ok || isJSONNull(v) {
		return nil
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return fmt.Errorf("%s field %q must be a string: %w", label, key, err)
	}
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return string(raw) == "null"
}

// componentLabel builds a stable, human-readable component label for error messages,
// preferring the emitted name.
func componentLabel(obj map[string]json.RawMessage, i int) string {
	if v, ok := obj["name"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
			return fmt.Sprintf("component %d (%q)", i+1, s)
		}
	}
	return fmt.Sprintf("component %d", i+1)
}

// checkItemLabel builds a human label for a standard-check item, preferring its
// guideline text then its section.
func checkItemLabel(obj map[string]json.RawMessage, i int) string {
	for _, key := range []string{"guideline", "section"} {
		if v, ok := obj[key]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
				return fmt.Sprintf("standard-check item %d (%q)", i+1, s)
			}
		}
	}
	return fmt.Sprintf("standard-check item %d", i+1)
}

// volatilityLabel builds a human label for a volatility, preferring its name.
func volatilityLabel(obj map[string]json.RawMessage, i int) string {
	if v, ok := obj["name"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
			return fmt.Sprintf("volatility %d (%q)", i+1, s)
		}
	}
	return fmt.Sprintf("volatility %d", i+1)
}

func rejectedVolatilityLabel(obj map[string]json.RawMessage, i int) string {
	if v, ok := obj["name"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
			return fmt.Sprintf("rejected volatility %d (%q)", i+1, s)
		}
	}
	return fmt.Sprintf("rejected volatility %d", i+1)
}

func useCaseLabel(obj map[string]json.RawMessage, i int) string {
	if v, ok := obj["name"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
			return fmt.Sprintf("use case %d (%q)", i+1, s)
		}
	}
	return fmt.Sprintf("use case %d", i+1)
}

// enumName renders an ordinal enum value as its wire name for error messages, falling
// back to the ordinal if unnamed.
func enumName[T ~int](names map[T]string, v T) string {
	if n, ok := names[v]; ok {
		return n
	}
	return fmt.Sprintf("%d", int(v))
}

// slotcodec.go holds the SUBSTRATE-NEUTRAL slot codec + the shared write-path helpers
// the git store (gitstore.go) builds on. It was carved out of the retired Postgres
// store (postgres.go) so the git substrate keeps the one canonical slot encoding: a
// model written by either store round-trips identically.
//
// ArtifactModel is an INTERFACE, so round-tripping it needs a kind-discriminated
// envelope: each populated slot is stored as {status, notes, kind, model} where
// `model` is the concrete typed model's own JSON. On read we switch on the stored kind
// to unmarshal into the right concrete *Xxx and assign it to the named slot.

// slotJSON is the on-infrastructure JSON shape for one populated ArtifactSlot.
// The kind discriminator lets the read codec pick the concrete model type and
// the destination slot. model is the concrete model's own json.Marshal output.
type slotJSON struct {
	Status int             `json:"status"`
	Notes  string          `json:"notes,omitempty"`
	Kind   int             `json:"kind"`
	Model  json.RawMessage `json:"model,omitempty"`
	// CritiqueVerdict / CritiqueNotes are the additive, optional, defaulted-empty
	// PM-critique read-back carrier (D-MSD-Δ amendment, ArtifactSlot doc). omitempty
	// keeps the on-disk shape byte-identical for every slot a critique never touched.
	CritiqueVerdict string `json:"critiqueVerdict,omitempty"`
	CritiqueNotes   string `json:"critiqueNotes,omitempty"`
	// ReviewThread is the DURABLE review ledger for this slot (the review-ledger
	// feature, founder-ratified 2026-07-05): the ordered, server-minted, round-stamped
	// per-comment record that replaces the ephemeral client-only comment model. Unlike
	// the CritiqueVerdict/CritiqueNotes carrier (cleared on every stage / status
	// transition), the thread is DURABLE — it accumulates across redraft rounds and
	// survives Stage/Reject/Withdraw so a reviewer always sees the full comment history
	// (and each comment's response/status). omitempty keeps the on-disk shape
	// byte-identical for every slot the ledger never touched.
	ReviewThread []ReviewComment `json:"reviewThread,omitempty"`
	// Revisions counts how many times this slot has been COMMITTED (F38 amendments,
	// founder ruling 2026-07-05); the FIRST commit yields 1. Bumped on every
	// CommitArtifact (commitTransition); it is the source for the AMENDMENT index a
	// reopening seeds into the new session's branch (…-amend-N). RULE: a COMMITTED slot is
	// always >= 1. Slots committed BEFORE this field existed persist as 0 (omitempty) and
	// are GRANDFATHERED to 1 on read (decodeSlotsMap) — a committed artifact is by
	// definition revision 1. This keeps the amendment index (max(1,Revisions)) and the
	// commit bump monotonic and -amend-N branch names unique even for pre-field slots (a
	// pre-field re-commit reads base 1 → ++ → lands at 2). omitempty keeps the on-disk
	// shape byte-identical for slots that were never committed (which stay 0).
	Revisions int64 `json:"revisions,omitempty"`
	// StaleBasis is set true on an already-COMMITTED slot when an UPSTREAM artifact it
	// depends on re-commits (an amendment shifted its basis, F38). It is a NON-blocking
	// UI signal — never an auto-invalidate/auto-redraft — cleared when THIS slot itself
	// re-commits (its own amendment is the reconcile). omitempty keeps the on-disk shape
	// byte-identical for slots that are not stale.
	StaleBasis bool `json:"staleBasis,omitempty"`
	// StaleBasisCause is the ADDITIVE record of WHY this slot went stale (the upstream slot
	// kind + its new revision after the amendment). Set alongside StaleBasis in
	// commitTransition, cleared alongside it when THIS slot re-commits. nil (omitempty) for
	// non-stale slots AND for slots that went stale before this field existed (no
	// migration — absent cause is allowed). Keeps the on-disk shape byte-identical for
	// every untouched slot.
	StaleBasisCause *StaleCause `json:"staleBasisCause,omitempty"`
	// Provenance is the ADDITIVE commit-provenance record (PM-P2-4): who committed / when /
	// which rail drafted it, captured at the approve→commit transition. Set in
	// commitTransition when the commit path supplied it (the provenance extension), refreshed
	// on every commit. nil (omitempty) for uncommitted slots AND for slots committed before
	// this field existed (no back-fill — absent provenance is allowed). Keeps the on-disk
	// shape byte-identical for every untouched slot.
	Provenance *Provenance `json:"provenance,omitempty"`
}

// slotEntry pairs a named-slot accessor with the kind that selects it. The
// ordered list is the single source of truth for the named-slot ↔ kind mapping
// used by both the read codec and the write-path slot routing.
type slotEntry struct {
	kind ArtifactKind
	ptr  func(p *Project) *ArtifactSlot
}

// slotTable enumerates every named slot on Project paired with its kind.
// Iterated to encode (all populated slots) and indexed by kind to route a write.
func slotTable() []slotEntry {
	return []slotEntry{
		{KindMission, func(p *Project) *ArtifactSlot { return &p.Mission }},
		{KindGlossary, func(p *Project) *ArtifactSlot { return &p.Glossary }},
		{KindScrubbedRequirements, func(p *Project) *ArtifactSlot { return &p.ScrubbedRequirements }},
		{KindVolatilities, func(p *Project) *ArtifactSlot { return &p.Volatilities }},
		{KindCoreUseCases, func(p *Project) *ArtifactSlot { return &p.CoreUseCases }},
		{KindSystem, func(p *Project) *ArtifactSlot { return &p.SystemDesign }},
		{KindOperationalConcepts, func(p *Project) *ArtifactSlot { return &p.OperationalConcepts }},
		{KindStandardCheck, func(p *Project) *ArtifactSlot { return &p.StandardCheck }},
		{KindPlanningAssumptions, func(p *Project) *ArtifactSlot { return &p.PlanningAssumptions }},
		{KindActivityList, func(p *Project) *ArtifactSlot { return &p.ActivityList }},
		{KindNetwork, func(p *Project) *ArtifactSlot { return &p.Network }},
		{KindNormalSolution, func(p *Project) *ArtifactSlot { return &p.NormalSolution }},
		{KindSubcriticalSolution, func(p *Project) *ArtifactSlot { return &p.SubcriticalSolution }},
		{KindCompressedSolution, func(p *Project) *ArtifactSlot { return &p.CompressedSolution }},
		{KindDecompressedSolution, func(p *Project) *ArtifactSlot { return &p.DecompressedSolution }},
		{KindRiskModel, func(p *Project) *ArtifactSlot { return &p.RiskModel }},
		{KindSdpReview, func(p *Project) *ArtifactSlot { return &p.SdpReview }},
	}
}

// slotPtr returns the named-slot accessor for kind, or false if kind names no
// known slot (an unrepresentable case given the closed enum, guarded anyway).
func slotPtr(p *Project, kind ArtifactKind) (*ArtifactSlot, bool) {
	for _, e := range slotTable() {
		if e.kind == kind {
			return e.ptr(p), true
		}
	}
	return nil, false
}

// encodeSlotsMap is the substrate-neutral slot codec: it returns the kind-keyed
// slotJSON map both substrates embed, so a slot serialises identically across either
// store (a model written by one substrate round-trips through the other).
func encodeSlotsMap(p *Project) (map[string]slotJSON, error) {
	out := map[string]slotJSON{}
	for _, e := range slotTable() {
		slot := e.ptr(p)
		if slot.Status == ReviewNone {
			continue
		}
		entry := slotJSON{
			Status:          int(slot.Status),
			Notes:           slot.Notes,
			Kind:            int(e.kind),
			CritiqueVerdict: slot.CritiqueVerdict,
			CritiqueNotes:   slot.CritiqueNotes,
			ReviewThread:    slot.ReviewThread,
			Revisions:       slot.Revisions,
			StaleBasis:      slot.StaleBasis,
			StaleBasisCause: slot.StaleBasisCause,
			Provenance:      slot.Provenance,
		}
		if slot.Model != nil {
			mb, err := json.Marshal(slot.Model)
			if err != nil {
				return nil, fmt.Errorf("encode slot %s model: %w", e.kind, err)
			}
			entry.Model = mb
		}
		out[fmt.Sprintf("%d", int(e.kind))] = entry
	}
	return out, nil
}

// decodeSlotsMap is the substrate-neutral slot decoder (the inverse of encodeSlotsMap).
func decodeSlotsMap(w map[string]slotJSON, p *Project) error {
	for _, entry := range w {
		kind := ArtifactKind(entry.Kind)
		slot, ok := slotPtr(p, kind)
		if !ok {
			return fmt.Errorf("decode slots: unknown kind ordinal %d", entry.Kind)
		}
		slot.Status = ArtifactReviewStatus(entry.Status)
		slot.Notes = entry.Notes
		slot.CritiqueVerdict = entry.CritiqueVerdict
		slot.CritiqueNotes = entry.CritiqueNotes
		slot.ReviewThread = entry.ReviewThread
		slot.Revisions = entry.Revisions
		// PRE-FIELD GRANDFATHER (F38 follow-up 2026-07-05): a slot committed BEFORE the
		// Revisions field existed reads back as 0 (the zero-value / omitempty gap), yet a
		// committed artifact is by definition at least revision 1. Normalize it to 1 on read
		// so every Revisions consumer is consistent: the amendment index (max(1,Revisions))
		// selects a real -amend-N branch, and commitTransition's ++ lands a pre-field
		// re-commit at 2 (base read as 1 → ++), keeping successive -amend-N names unique. A
		// never-committed slot (Status != Committed) is left at 0 so its FIRST commit still
		// lands at 1. This is a lazy migration: the value persists as 1 on the next aggregate
		// write.
		if slot.Status == ReviewCommitted && slot.Revisions == 0 {
			slot.Revisions = 1
		}
		slot.StaleBasis = entry.StaleBasis
		slot.StaleBasisCause = entry.StaleBasisCause
		slot.Provenance = entry.Provenance
		if len(entry.Model) > 0 {
			model, ok := NewModelForKind(kind)
			if !ok {
				return fmt.Errorf("decode slots: no model type for kind %s", kind)
			}
			if err := json.Unmarshal(entry.Model, model); err != nil {
				return fmt.Errorf("decode slot %s model: %w", kind, err)
			}
			// F81 ZERO-VALUE HOLE: encoding/json never invokes an enum's UnmarshalJSON for
			// an ABSENT field, so a component that omits "layer"/"kind" (or a use case that
			// omits "trigger"/"classification") decodes to the enum zero value with no error.
			// Reject a committed model carrying such a defaulted-required field on read-back
			// with the SAME strictness the write path (putDraftModel) applies, so the two
			// never disagree. Only enforced for populated slots (len(entry.Model) > 0).
			if err := RequireModelFields(kind, entry.Model); err != nil {
				return fmt.Errorf("decode slot %s model: %w", kind, err)
			}
			// Restore Solution SlotKind: the four share one concrete type; the
			// destination slot's kind is authoritative.
			if sol, isSol := model.(*Solution); isSol {
				sol.SlotKind = kind
			}
			slot.Model = model
		}
	}
	return nil
}

// statusTransition builds the pure in-memory transition for commit/reject/withdraw:
// a status flip on the slot named by kind, keeping the model already staged there.
// ContractMisuse if the slot is unpopulated (no model was ever staged).
func statusTransition(op string, kind ArtifactKind, to ArtifactReviewStatus, notes string) func(*Project) error {
	return func(p *Project) error {
		slot, ok := slotPtr(p, kind)
		if !ok {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.%s: unknown kind %s", op, kind))
		}
		if slot.Status == ReviewNone || slot.Model == nil {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.%s: slot %s is unpopulated (stage a model first)", op, kind))
		}
		slot.Status = to
		slot.Notes = notes
		// Clear the PM-critique read-back carrier on every status transition.
		slot.CritiqueVerdict = ""
		slot.CritiqueNotes = ""
		return nil
	}
}

// commitTransition is the CommitArtifact-specific transition (F38 amendments/staleness). It
// flips the slot to ReviewCommitted (the statusTransition contract) and then, in the SAME
// atomic mutation over the whole Project:
//   - bumps the committed slot's Revisions (the count of commits; the amendment index a
//     reopening seeds into the next session's …-amend-N branch),
//   - CLEARS the committed slot's own StaleBasis (re-committing IS the reconcile), and
//   - sets StaleBasis=true on every ALREADY-committed DOWNSTREAM slot (its basis shifted).
//
// On a FIRST commit no downstream slot is committed yet, so the downstream marking is a
// no-op; only a re-commit (amendment) actually flags anything.
//
// prov carries the ADDITIVE commit-provenance record (PM-P2-4). When non-nil it is stamped
// onto the committed slot (refreshing any prior provenance on a re-commit); a nil prov (the
// plain CommitArtifact path / a substrate that records no provenance) leaves the slot's
// provenance untouched — absent provenance is allowed.
func commitTransition(kind ArtifactKind, prov *Provenance) func(*Project) error {
	flip := statusTransition("CommitArtifact", kind, ReviewCommitted, "")
	return func(p *Project) error {
		if err := flip(p); err != nil {
			return err
		}
		slot, _ := slotPtr(p, kind)
		// Stamp the commit provenance (PM-P2-4) when the commit path supplied it.
		if prov != nil {
			slot.Provenance = prov
		}
		// Bump the commit count. First commit: 0 → 1. A re-commit (amendment) bumps from
		// the prior count; pre-field committed bases are grandfathered to 1 on read
		// (decodeSlotsMap / ArtifactSlot.Revisions doc), so a pre-field slot's first
		// re-commit lands at 2 — keeping successive -amend-N branch names unique.
		slot.Revisions++
		// Re-committing IS the reconcile: clear this slot's own staleness AND its cause.
		slot.StaleBasis = false
		slot.StaleBasisCause = nil
		// Flag every already-committed downstream slot stale, and RECORD THE CAUSE (this
		// upstream kind + its new revision) so the read model can name what shifted. On a
		// FIRST commit no downstream slot is committed yet, so this is a no-op.
		cause := &StaleCause{UpstreamKind: kind.WireName(), UpstreamRevision: slot.Revisions}
		for _, dk := range downstreamKinds(kind) {
			if ds, ok := slotPtr(p, dk); ok && ds.Status == ReviewCommitted {
				ds.StaleBasis = true
				ds.StaleBasisCause = cause
			}
		}
		return nil
	}
}

// mutationMode tunes how a store's applyMutation treats the brand-new-row case.
type mutationMode int

const (
	// modeUpsert is the legacy default: an absent row at expectedVersion 0 is
	// created (the slot verbs are tolerant of being the first write).
	modeUpsert mutationMode = iota
	// modeRequireExisting fails with fwra.NotFound when no row exists. Used by
	// verbs that may only run on an already-created project (SetResearchInput).
	modeRequireExisting
	// modeCreateOnly fails with fwra.Conflict when a row already exists. Used by
	// CreateProject so a project is born exactly once.
	modeCreateOnly
)

// This file gives every closed ordinal enum the SPA reads a STRING wire encoding:
// MarshalJSON emits the canonical camelCase name; UnmarshalJSON accepts that name
// AND (for backward compatibility) a bare integer ordinal — so the architect-role
// draft prompts that still emit integer ordinals, and any previously-persisted
// JSONB payload written before this string encoding existed, keep decoding. The
// artifactValidationEngine reads these enums IN-MEMORY (Go values), so it is
// unaffected by the JSON encoding.
//
// The (name→ordinal, ordinal→name) tables are the single source of truth per enum;
// the camelCase names match the OpenAPI string-enum schemas exactly.

// marshalEnum encodes an ordinal enum value as its string name.
func marshalEnum[T ~int](v T, names map[T]string, what string) ([]byte, error) {
	name, ok := names[v]
	if !ok {
		return nil, fmt.Errorf("projectstate: %s(%d) has no wire name", what, int(v))
	}
	return json.Marshal(name)
}

// unmarshalEnum decodes a string name (or legacy integer ordinal) into an ordinal enum.
func unmarshalEnum[T ~int](data []byte, byName map[string]T, what string) (T, error) {
	var zero T
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		v, ok := byName[name]
		if !ok {
			return zero, fmt.Errorf("projectstate: %q is not a recognized %s wire name", name, what)
		}
		return v, nil
	}
	var ordinal int
	if err := json.Unmarshal(data, &ordinal); err != nil {
		return zero, fmt.Errorf("projectstate: %s must be a string wire name or integer ordinal: %w", what, err)
	}
	return T(ordinal), nil
}

// invert builds a name→value table from a value→name table once at init.
func invert[T ~int](names map[T]string) map[string]T {
	m := make(map[string]T, len(names))
	for v, n := range names {
		m[n] = v
	}
	return m
}

// ---- Axis ----

var axisNames = map[Axis]string{
	AxisSameCustomerOverTime:  "sameCustomerOverTime",
	AxisAllCustomersAtOneTime: "allCustomersAtOneTime",
}
var axisByName = invert(axisNames)

// MarshalJSON encodes the Axis as its camelCase wire name.
func (a Axis) MarshalJSON() ([]byte, error) { return marshalEnum(a, axisNames, "Axis") }

// UnmarshalJSON decodes a wire name (or legacy ordinal) into an Axis.
func (a *Axis) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, axisByName, "Axis")
	if err != nil {
		return err
	}
	*a = v
	return nil
}

// ---- RejectionClass ----

var rejectionClassNames = map[RejectionClass]string{
	RejectionVariableNotVolatile: "variableNotVolatile",
	RejectionNatureOfTheBusiness: "natureOfTheBusiness",
	RejectionSpeculative:         "speculative",
	RejectionFoldedInto:          "foldedInto",
}
var rejectionClassByName = invert(rejectionClassNames)

// MarshalJSON encodes the RejectionClass as its camelCase wire name.
func (r RejectionClass) MarshalJSON() ([]byte, error) {
	return marshalEnum(r, rejectionClassNames, "RejectionClass")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a RejectionClass.
func (r *RejectionClass) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, rejectionClassByName, "RejectionClass")
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// ---- CheckStatus ----

var checkStatusNames = map[CheckStatus]string{
	CheckPass:   "pass",
	CheckWaived: "waived",
	CheckFail:   "fail",
}
var checkStatusByName = invert(checkStatusNames)

// MarshalJSON encodes the CheckStatus as its camelCase wire name.
func (c CheckStatus) MarshalJSON() ([]byte, error) {
	return marshalEnum(c, checkStatusNames, "CheckStatus")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a CheckStatus.
func (c *CheckStatus) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, checkStatusByName, "CheckStatus")
	if err != nil {
		return err
	}
	*c = v
	return nil
}

// ---- ComponentKind ----

var componentKindNames = map[ComponentKind]string{
	CompClient:         "client",
	CompManager:        "manager",
	CompEngine:         "engine",
	CompResourceAccess: "resourceAccess",
	CompResource:       "resource",
	CompUtility:        "utility",
}
var componentKindByName = invert(componentKindNames)

// String returns the ComponentKind's camelCase wire name — the same name the
// JSON encoding uses, so callers mapping this aggregate onto the platform's
// string-typed model do not re-derive the vocabulary.
func (k ComponentKind) String() string { return enumName(componentKindNames, k) }

// MarshalJSON encodes the ComponentKind as its camelCase wire name.
func (k ComponentKind) MarshalJSON() ([]byte, error) {
	return marshalEnum(k, componentKindNames, "ComponentKind")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a ComponentKind.
func (k *ComponentKind) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, componentKindByName, "ComponentKind")
	if err != nil {
		return err
	}
	*k = v
	return nil
}

// ---- Layer ----

var layerNames = map[Layer]string{
	LayerClient:         "client",
	LayerManager:        "manager",
	LayerEngine:         "engine",
	LayerResourceAccess: "resourceAccess",
	LayerResource:       "resource",
	LayerUtility:        "utility",
}
var layerByName = invert(layerNames)

// String returns the canonical lowercase layer name — the same spelling layerNames
// carries on the wire. Defined so callers OUTSIDE this package (the construction
// Manager hydrating an activity's layer from its component) can name a Layer
// without reaching for the unexported map.
func (l Layer) String() string { return enumName(layerNames, l) }

// MarshalJSON encodes the Layer as its camelCase wire name.
func (l Layer) MarshalJSON() ([]byte, error) { return marshalEnum(l, layerNames, "Layer") }

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a Layer.
func (l *Layer) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, layerByName, "Layer")
	if err != nil {
		return err
	}
	*l = v
	return nil
}

// ---- CallMode ----

var callModeNames = map[CallMode]string{
	CallSync:        "sync",
	CallQueued:      "queued",
	CallEventPubSub: "eventPubSub",
}
var callModeByName = invert(callModeNames)

// String returns the CallMode's camelCase wire name.
func (m CallMode) String() string { return enumName(callModeNames, m) }

// MarshalJSON encodes the CallMode as its camelCase wire name.
func (m CallMode) MarshalJSON() ([]byte, error) { return marshalEnum(m, callModeNames, "CallMode") }

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a CallMode.
func (m *CallMode) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, callModeByName, "CallMode")
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// ---- Trigger ----

var triggerNames = map[Trigger]string{
	TriggerClientAction: "clientAction",
	TriggerTimer:        "timer",
	TriggerBusMessage:   "busMessage",
}
var triggerByName = invert(triggerNames)

// MarshalJSON encodes the Trigger as its camelCase wire name.
func (t Trigger) MarshalJSON() ([]byte, error) { return marshalEnum(t, triggerNames, "Trigger") }

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a Trigger.
func (t *Trigger) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, triggerByName, "Trigger")
	if err != nil {
		return err
	}
	*t = v
	return nil
}

// ---- Classification ----

var classificationNames = map[Classification]string{
	ClassCore:    "core",
	ClassNonCore: "nonCore",
}
var classificationByName = invert(classificationNames)

// MarshalJSON encodes the Classification as its camelCase wire name.
func (c Classification) MarshalJSON() ([]byte, error) {
	return marshalEnum(c, classificationNames, "Classification")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a Classification.
func (c *Classification) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, classificationByName, "Classification")
	if err != nil {
		return err
	}
	*c = v
	return nil
}

// ---- ActivityNodeKind ----

var activityNodeKindNames = map[ActivityNodeKind]string{
	NodeStart:         "start",
	NodeAction:        "action",
	NodeDecision:      "decision",
	NodeMerge:         "merge",
	NodeFork:          "fork",
	NodeJoin:          "join",
	NodeEnd:           "end",
	NodeSwimLane:      "swimLane",
	NodeNote:          "note",
	NodeLoop:          "loop",
	NodeSwitch:        "switch",
	NodeGoto:          "goto",
	NodeInterruptEdge: "interruptEdge",
	NodeTimeEvent:     "timeEvent",
	NodeAcceptEvent:   "acceptEvent",
}
var activityNodeKindByName = invert(activityNodeKindNames)

// MarshalJSON encodes the ActivityNodeKind as its camelCase wire name.
func (k ActivityNodeKind) MarshalJSON() ([]byte, error) {
	return marshalEnum(k, activityNodeKindNames, "ActivityNodeKind")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into an ActivityNodeKind.
func (k *ActivityNodeKind) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, activityNodeKindByName, "ActivityNodeKind")
	if err != nil {
		return err
	}
	*k = v
	return nil
}

// ---- DeliveryStyle ----

var deliveryStyleNames = map[DeliveryStyle]string{
	StyleCloud: "cloud",
	StyleLocal: "local",
	StyleBoth:  "both",
}
var deliveryStyleByName = invert(deliveryStyleNames)

// String returns the DeliveryStyle's camelCase wire name.
func (s DeliveryStyle) String() string { return enumName(deliveryStyleNames, s) }

// MarshalJSON encodes the DeliveryStyle as its camelCase wire name.
func (s DeliveryStyle) MarshalJSON() ([]byte, error) {
	return marshalEnum(s, deliveryStyleNames, "DeliveryStyle")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a DeliveryStyle.
func (s *DeliveryStyle) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, deliveryStyleByName, "DeliveryStyle")
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// ---- DeploymentProfile ----

var deploymentProfileNames = map[DeploymentProfile]string{
	ProfileCloud: "cloud",
	ProfileLocal: "local",
	ProfileTest:  "test",
}
var deploymentProfileByName = invert(deploymentProfileNames)

// String returns the DeploymentProfile's camelCase wire name.
func (p DeploymentProfile) String() string { return enumName(deploymentProfileNames, p) }

// MarshalJSON encodes the DeploymentProfile as its camelCase wire name.
func (p DeploymentProfile) MarshalJSON() ([]byte, error) {
	return marshalEnum(p, deploymentProfileNames, "DeploymentProfile")
}

// UnmarshalJSON decodes a wire name (or legacy ordinal) into a DeploymentProfile.
func (p *DeploymentProfile) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, deploymentProfileByName, "DeploymentProfile")
	if err != nil {
		return err
	}
	*p = v
	return nil
}

// ---- EdgeKind ----

var edgeKindNames = map[EdgeKind]string{
	EdgeControlFlow: "controlFlow",
	EdgeGuardedFlow: "guardedFlow",
}
var edgeKindByName = invert(edgeKindNames)

// MarshalJSON encodes the EdgeKind as its camelCase wire name.
func (k EdgeKind) MarshalJSON() ([]byte, error) { return marshalEnum(k, edgeKindNames, "EdgeKind") }

// UnmarshalJSON decodes a wire name (or legacy ordinal) into an EdgeKind.
func (k *EdgeKind) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, edgeKindByName, "EdgeKind")
	if err != nil {
		return err
	}
	*k = v
	return nil
}

// ---- ActivityType ----

var activityTypeNames = map[ActivityType]string{
	ActivityTypeService:       "service",
	ActivityTypeFrontend:      "frontend",
	ActivityTypeTesting:       "testing",
	ActivityTypeDeployment:    "deployment",
	ActivityTypeDocumentation: "documentation",
	ActivityTypeUIDesign:      "uiDesign",
	ActivityTypeIntegration:   "integration",
}
var activityTypeByName = invert(activityTypeNames)

// MarshalJSON encodes the ActivityType as its canonical wire name.
// NOTE: ActivityKind is a type alias for ActivityType, so this also handles
// ActivityKind fields — they share the same underlying type and method set.
func (t ActivityType) MarshalJSON() ([]byte, error) {
	return marshalEnum(t, activityTypeNames, "ActivityType")
}

// UnmarshalJSON decodes a wire name (or legacy integer ordinal) into an ActivityType.
// The legacy-ordinal path ensures existing project.json Kind values (0=service,
// 1=frontend, 2=testing) continue to decode correctly after the rename.
func (t *ActivityType) UnmarshalJSON(data []byte) error {
	v, err := unmarshalEnum(data, activityTypeByName, "ActivityType")
	if err != nil {
		return err
	}
	*t = v
	return nil
}

// ---- TestingVariant ----

var testingVariantNames = map[TestingVariant]string{
	TestVariantPlan:       "plan",
	TestVariantHarness:    "harness",
	TestVariantPerf:       "perf",
	TestVariantSystemTest: "systemTest",
	TestVariantQAProcess:  "qaProcess",
}
var testingVariantByName = invert(testingVariantNames)

// MarshalJSON encodes the TestingVariant as its canonical wire name.
func (v TestingVariant) MarshalJSON() ([]byte, error) {
	return marshalEnum(v, testingVariantNames, "TestingVariant")
}

// UnmarshalJSON decodes a wire name (or legacy integer ordinal) into a TestingVariant.
func (v *TestingVariant) UnmarshalJSON(data []byte) error {
	got, err := unmarshalEnum(data, testingVariantByName, "TestingVariant")
	if err != nil {
		return err
	}
	*v = got
	return nil
}

// activityconstructionstatus.go holds the per-activity construction head-state types
// (Task 1: seed-archistrator-design-state). It mirrors the gitactivitystatus.go
// pattern precisely: a typed enum + a status record keyed by ActivityID, stored in
// Project.ActivityConstruction, populated only in Phase 3.
//
// DESIGN: this is the dry-run construction pump's foundation. The Phase enum captures
// the coarse lifecycle (not started / running / done); the timestamps (StartedAt,
// CompletedAt) are SERVER-RESOLVED at commit time (this is RA code, time.Now() is fine
// here — no Temporal). The map is lazily allocated: nil until the first Record* verb.

// ActivityConstructionPhase is the coarse per-activity construction lifecycle.

// ActivityConstructionNotStarted is the zero value — the activity has not yet
// been dispatched by the construction pump.

// ActivityConstructionRunning — the activity's construction agent is in progress.

// ActivityConstructionDone — the activity's construction completed (agent finished).

// ActivityConstructionFailed — the activity's construction reached a terminal
// FAILURE (a cancelled/failed/timed-out pipeline, an exhausted variance budget, or
// an escalation that timed out). Distinct from Done: the work did NOT integrate.
// This is a STORED terminal — the CoarsePhase deriver short-circuits on it so it is
// never recomputed back to Running/Done (see CoarsePhase's guard).

// String returns the canonical wire name for the construction phase (used in JSON
// and log output). Mirrors CICheckState.String() and ActivityOutcome.String().
func (p ActivityConstructionPhase) String() string {
	switch p {
	case ActivityConstructionRunning:
		return "running"
	case ActivityConstructionDone:
		return "done"
	case ActivityConstructionFailed:
		return "failed"
	case ActivityConstructionNotStarted:
		return "notStarted"
	}
	// Unreachable for the four defined ActivityConstructionPhase values above (the
	// exhaustive linter enforces that every real variant has its own case); kept
	// as a defensive fallback for an out-of-range ordinal.
	return "notStarted"
}

// FailureReason is the closed enum of terminal-failure causes recorded on an
// activity's construction head-state when it reaches ActivityConstructionFailed.
// It lets the console explain WHY the activity is no longer pending (a cancelled
// run, an exhausted retry budget, an escalation nobody answered, …) rather than
// leaving it stuck Running forever.

// FailureReasonUnknown is the zero value (no failure recorded).

// PipelineFailed — the construction pipeline reached a terminal FAILURE conclusion.

// PipelineCancelled — the construction pipeline run was cancelled.

// PipelineTimedOut — the construction pipeline timed out (or the observe poll
// budget was exhausted without a terminal phase).

// VarianceExhausted — the supervision loop exhausted its variance/retry budget.

// EscalationTimedOut — an escalation waited for an operator override that never
// came within the bounded escalation-wait window.

// ComponentUnresolved — an eligible activity could not be dispatched because its
// authored componentId names no component in the committed systemDesign. A plan
// defect, terminal until a human amends the activity list — deliberately NOT routed
// through the interventionEngine, which adjudicates variance for work legitimately
// in flight. Reserved for the componentId case; a dependency-graph defect on the
// same activity is DependencyUnresolved or DependencyCycle instead.

// DependencyUnresolved — an eligible activity could not be dispatched because one
// of its dependency ids names neither an activity in the committed activity list
// nor a milestone in the committed network. A dangling reference. FailureDetail
// carries the activity and the dangling id. Repair: fix the id, or author the
// missing node. Same terminal/non-intervention treatment as ComponentUnresolved.

// DependencyCycle — an eligible activity could not be dispatched because
// dependency resolution found a cycle through activities/milestones. Every id
// resolves; the topology is wrong. FailureDetail carries the cycle path. Repair:
// break the cycle by removing an edge. Same terminal/non-intervention treatment
// as ComponentUnresolved.

// ActivityUnclassifiable — an eligible activity could not be dispatched because its
// authored (workerClass, coding) pair matches no ClassifyActivity rule, so no
// ActivityType — and therefore no phase profile and no slash command — can be
// resolved for it. A plan defect: dispatching anyway is what handed an infra activity
// a testing command. FailureDetail carries the activity id, its workerClass and its
// coding flag. Repair: amend workerClass or coding in the committed activity list.
// Same terminal/non-intervention treatment as ComponentUnresolved.

// String returns the canonical wire name for the failure reason.
func (r FailureReason) String() string {
	switch r {
	case PipelineFailed:
		return "pipelineFailed"
	case PipelineCancelled:
		return "pipelineCancelled"
	case PipelineTimedOut:
		return "pipelineTimedOut"
	case VarianceExhausted:
		return "varianceExhausted"
	case EscalationTimedOut:
		return "escalationTimedOut"
	case ComponentUnresolved:
		return "componentUnresolved"
	case DependencyUnresolved:
		return "dependencyUnresolved"
	case DependencyCycle:
		return "dependencyCycle"
	case ActivityUnclassifiable:
		return "activityUnclassifiable"
	case FailureReasonUnknown:
		return "unknown"
	}
	// Unreachable for the ten defined FailureReason values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "unknown"
}

// PhaseCompletion is one App-A internal phase record within an activity's Phases slice.
// Binary exit (App A §1.1): Completed=true only when the review gate passed. Weight
// is the fraction of activity progress this phase carries (sums to 100 per type).
// ArtifactRef is a pointer into phaseArtifacts/serviceContracts/Produced — set by
// RecordPhaseCompleted.
type PhaseCompletion struct {
	Phase       ActivityMethodPhase `json:"phase"`
	Weight      int                 `json:"weight"`
	Label       string              `json:"label,omitempty"`
	Completed   bool                `json:"completed,omitempty"`
	CompletedAt *time.Time          `json:"completedAt,omitempty"`
	ArtifactRef string              `json:"artifactRef,omitempty"`
}

// ActivityConstructionStatus is the per-activity construction head-state record.
// One per construction-network activity, keyed by ActivityID in
// Project.ActivityConstruction. Additive, populated only in Phase 3.
type ActivityConstructionStatus struct {
	// ActivityID is the network activity id — the map key (NAME-as-identity).
	ActivityID string `json:"activityID"`
	// Type is the canonical activity-type axis (§2.1 design). Replaces Kind.
	Type ActivityType `json:"type,omitempty"`
	// Variant discriminates testing sub-types (only set when Type==ActivityTypeTesting).
	Variant TestingVariant `json:"variant,omitempty"`
	// Phase is the COMPUTED coarse lifecycle (NotStarted/Running/Done). Derived from
	// Phases at read time via CoarsePhase — kept for back-compat with existing readers.
	Phase ActivityConstructionPhase `json:"phase"`
	// Phases is the App-A internal phase set. Set once by phaseSetFor at activity start;
	// individual entries are marked Completed by RecordPhaseCompleted.
	Phases []PhaseCompletion `json:"phases,omitempty"`
	// CurrentPhase is the phase the workflow loop is currently executing.
	CurrentPhase ActivityMethodPhase `json:"currentPhase,omitempty"`
	// StartedAt is the server-resolved timestamp when RecordActivityStarted committed.
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// CompletedAt is the server-resolved timestamp when RecordActivityCompleted committed.
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	// Kind is the legacy field — kept for JSON back-compat with seeded project.json entries.
	// New code reads Type instead. The two fields share the same underlying int encoding
	// (ActivityKind = ActivityType alias from Task 1), so existing seeded values decode correctly.
	Kind ActivityKind `json:"kind,omitempty"`
	// BuildStatus is the COMPUTED finer build-status lens. Derived from Phases+CurrentPhase
	// at read time via CoarseBuildStatus — kept for back-compat.
	BuildStatus ActivityBuildStatus `json:"buildStatus,omitempty"`
	// Produced is the seeded list of artifacts this activity produced (contracts/code).
	Produced []ProducedArtifact `json:"produced,omitempty"`
	// FailureReason is set when Phase == ActivityConstructionFailed — the closed-enum
	// cause of the terminal failure (cancelled/failed/timed-out pipeline, exhausted
	// variance budget, escalation timeout). Zero (FailureReasonUnknown) otherwise.
	FailureReason FailureReason `json:"failureReason,omitempty"`
	// FailureDetail is the human-readable diagnostic captured alongside FailureReason
	// (the pipeline's neutral diagnostic / a short escalation note). Empty otherwise.
	FailureDetail string `json:"failureDetail,omitempty"`
}

// phaseSetFor returns the seeded PhaseCompletion slice for an activity type/variant.
// It is a thin adapter over ProfileFor — the single source of truth for the phase
// tables. Kept for its existing call sites in gitconstruction.go.
func phaseSetFor(t ActivityType, v TestingVariant) []PhaseCompletion {
	return ProfileFor(t, v).toPhaseCompletions()
}

// CoarsePhaseFor is the stored-phase-aware compute-at-read entry point: a stored
// terminal-FAILURE phase (ActivityConstructionFailed) is STICKY and short-circuits —
// it is never recomputed back to Running/Done from the Phases slice (a late
// phase-completion record after a RecordActivityFailed must not resurrect the
// activity). Otherwise it falls through to CoarsePhase over the Phases slice.
func CoarsePhaseFor(stored ActivityConstructionPhase, phases []PhaseCompletion) ActivityConstructionPhase {
	if stored == ActivityConstructionFailed {
		return ActivityConstructionFailed
	}
	return CoarsePhase(phases)
}

// CoarseBuildStatusFor is the stored-status-aware compute-at-read entry point: a
// stored terminal-FAILURE build status (BuildFailed) is STICKY and short-circuits —
// it is never recomputed back to in-construction/in-review/integrated. Otherwise it
// falls through to CoarseBuildStatus over the Phases slice.
func CoarseBuildStatusFor(stored ActivityBuildStatus, phases []PhaseCompletion, current ActivityMethodPhase) ActivityBuildStatus {
	if stored == BuildFailed {
		return BuildFailed
	}
	return CoarseBuildStatus(phases, current)
}

// CoarsePhase derives the coarse ActivityConstructionPhase from the Phases slice
// (compute-at-read; kept for back-compat). Empty/nil phases → NotStarted.
// A stored terminal-failure phase is preserved by callers via CoarsePhaseFor /
// the applyPhaseCompletion guard, NOT here (this derives purely from Phases).
func CoarsePhase(phases []PhaseCompletion) ActivityConstructionPhase {
	if len(phases) == 0 {
		return ActivityConstructionNotStarted
	}
	allDone := true
	anyDone := false
	for _, p := range phases {
		if p.Completed {
			anyDone = true
		} else {
			allDone = false
		}
	}
	if allDone {
		return ActivityConstructionDone
	}
	if anyDone {
		return ActivityConstructionRunning
	}
	return ActivityConstructionNotStarted
}

// CoarseBuildStatus derives the ActivityBuildStatus from the phase set and current
// phase (compute-at-read; kept for back-compat). Rules: Integration phase done →
// BuildIntegrated; Construction phase done but Integration not → BuildInReview;
// otherwise → BuildInConstruction.
// The second (phase) parameter is reserved for future use (fine-grained phase
// display) and is ignored; coarse status is derived solely from Phases completion.
func CoarseBuildStatus(phases []PhaseCompletion, _ ActivityMethodPhase) ActivityBuildStatus {
	constructionDone := false
	integrationDone := false
	for _, p := range phases {
		if p.Phase == MethodPhaseConstruction && p.Completed {
			constructionDone = true
		}
		if p.Phase == MethodPhaseIntegration && p.Completed {
			integrationDone = true
		}
	}
	if integrationDone {
		return BuildIntegrated
	}
	if constructionDone {
		return BuildInReview
	}
	return BuildInConstruction
}

// ActivityType is the canonical persisted activity-type axis (what kind of thing
// is built). Replaces the 3-value ActivityKind; used to derive the phase set a
// C-* activity walks (v3 design §1 tables). SEEDED by the bootstrap generator.
// Existing project.json Kind values 0/1/2 decode verbatim (Service/Frontend/Testing);
// 3/4 (Deployment/Documentation) are additive with no data migration needed.

// ActivityTypeService — a Manager/Engine/ResourceAccess/Client component build.

// ActivityTypeFrontend — a SPA / web UI surface build.

// ActivityTypeTesting — a system-test / CI activity (variant selected by TestingVariant).

// ActivityTypeDeployment — a devops / provisioning activity (R-* prefix, coding=false).

// ActivityTypeDocumentation — a tech-writing / ADR / runbook activity (N-ADR etc.).

// ActivityTypeUIDesign — a UI-design CONCEPT activity (G-* prefix, ui-designer,
// coding=false). It stops at the design artifact; the SPA surfaces realizing it are
// separate ActivityTypeFrontend activities.

// ActivityTypeIntegration — an I-* use-case integration activity: wiring and verifying
// already-constructed components end-to-end. It has no construction of its own.

// String returns the canonical wire name.
func (t ActivityType) String() string {
	switch t {
	case ActivityTypeFrontend:
		return "frontend"
	case ActivityTypeTesting:
		return "testing"
	case ActivityTypeDeployment:
		return "deployment"
	case ActivityTypeDocumentation:
		return "documentation"
	case ActivityTypeUIDesign:
		return "uiDesign"
	case ActivityTypeIntegration:
		return "integration"
	case ActivityTypeService:
		// The zero value (== ActivityKindService, the legacy alias).
		return "service"
	}
	// Unreachable for the seven defined ActivityType values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "service"
}

// ActivityKind is a type alias for ActivityType kept for JSON back-compat.
// Existing project.json entries seeded with Kind use the same integer encoding;
// the legacy 3 values (0=Service/1=Frontend/2=Testing) decode correctly through
// ActivityType. New code should use ActivityType; ActivityKind remains valid as
// a field type so no renaming is required at call sites.
type ActivityKind = ActivityType

// ActivityKindService / ActivityKindFrontend / ActivityKindTesting are preserved
// as aliases to the ActivityType constants so existing code referencing the old
// three-value names continues to compile without modification.
const (
	ActivityKindService  = ActivityTypeService
	ActivityKindFrontend = ActivityTypeFrontend
	ActivityKindTesting  = ActivityTypeTesting
)

// TestingVariant discriminates the five N-* testing activity sub-types. Only
// meaningful when ActivityType == ActivityTypeTesting. Variant is chosen from the
// activity name prefix (N-STP → Plan, N-STH → Harness, N-PERF → Perf,
// N-IT → SystemTest, N-QA → QAProcess).

// N-STP: system test plan
// N-STH: test harness construction
// N-PERF: performance rig
// N-IT: system test execution (terminal/critical)
// N-QA: QA process definition

// String returns the canonical wire name.
func (v TestingVariant) String() string {
	switch v {
	case TestVariantHarness:
		return "harness"
	case TestVariantPerf:
		return "perf"
	case TestVariantSystemTest:
		return "systemTest"
	case TestVariantQAProcess:
		return "qaProcess"
	case TestVariantPlan:
		return "plan"
	}
	// Unreachable for the five defined TestingVariant values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "plan"
}

// ActivityMethodPhase is one App-A internal phase within a construction activity.
// It is a canonical lowercase phase-id string (not an ordinal enum). The ordered
// phase SET for a given ActivityType is defined by phaseSetFor (Task 2); this file
// only declares the type and all known phase-id constants.
//
// Using a string type (rather than int) means the JSON wire encoding is the
// constant value itself — no MarshalJSON/UnmarshalJSON boilerplate needed.

// String returns the phase id (the underlying string value).
func (p ActivityMethodPhase) String() string { return string(p) }

// Service / shared phase ids (v3 design §1a and §1b).
// NOTE: the "Phase" prefix is shared with the project-lifecycle Phase type
// (artifactmodel.go). To avoid name collision, these constants use the
// "MethodPhase" prefix.

// SRS / UX requirements / provisioning spec / doc outline
// service contract (Service only); maps to DD cast
// test plan slice (Service/Frontend only)
// code / manifest / harness / doc authoring
// integration + convergence verification

// ActivityBuildStatus is the finer build-status lens (ux-mock parity) for activities
// that have a corpus presence. Coarser eligible/blocked/not-started are DERIVED in the
// webApp from the network + done-set and are not seeded here.

// BuildInConstruction — a construction log exists, work in progress (zero value).

// BuildInReview — a construction log exists without a passing review.

// BuildIntegrated — construction log + a passing review exist.

// BuildFailed — the build reached a terminal FAILURE (paired with
// ActivityConstructionFailed). The work did not integrate; the node is failed,
// not in-construction/in-review/integrated.

// String returns the canonical wire name (matches the ux-mock BuildStatus union).
func (s ActivityBuildStatus) String() string {
	switch s {
	case BuildInReview:
		return "in-review"
	case BuildIntegrated:
		return "integrated"
	case BuildFailed:
		return "failed"
	case BuildInConstruction:
		return "in-construction"
	}
	// Unreachable for the four defined ActivityBuildStatus values above (the
	// exhaustive linter enforces that every real variant has its own case); kept
	// as a defensive fallback for an out-of-range ordinal.
	return "in-construction"
}

// ProducedArtifact is one artifact a construction activity produced (a frozen service
// contract, the built code). SEEDED from the corpus (a contract file / a construction
// log). Mirrors the ux-mock ActivityArtifact card fields.

// "service-contract" | "code"

// corpus-relative path, e.g. "implementation/contracts/webClient.md"

// ConstructionProgress holds the project-level construction tracking framing scalars
// (ux-mock CONSTRUCTION_SUMMARY subset). Seeded; EV is derived, not stored.

// ProfilePhase pairs a canonical ActivityMethodPhase with its per-profile weight
// and human-facing display label. The phase id is ALWAYS one of the five canonical
// ids (Requirements/DetailedDesign/TestPlan/Construction/Integration) so the shared
// earned-value/progress formula (Appendix A) stays uniform across all activity
// types; only the label and weight vary per profile.

// Profile is the per-activity-type preset over the ONE canonical lifecycle: an
// ordered subset of the five canonical phases with weights and display labels.
// It is NOT a distinct lifecycle — it is weights + labels + a phase subset over
// the single shared phase vocabulary (Righting Software, Appendix A / Table A-1).

// PhaseIDs returns the ordered canonical phase ids for this profile — the sequence
// the construction pump dispatches.
func (pr Profile) PhaseIDs() []ActivityMethodPhase {
	ids := make([]ActivityMethodPhase, len(pr.Phases))
	for i, p := range pr.Phases {
		ids[i] = p.Phase
	}
	return ids
}

// toPhaseCompletions materializes the profile into the store's PhaseCompletion
// slice (seeded, all Completed=false).
func (pr Profile) toPhaseCompletions() []PhaseCompletion {
	out := make([]PhaseCompletion, len(pr.Phases))
	for i, p := range pr.Phases {
		out[i] = PhaseCompletion{Phase: p.Phase, Weight: p.Weight, Label: p.Label}
	}
	return out
}

// ProfileFor returns the canonical-phase profile for an activity type (and testing
// variant, meaningful only when t == ActivityTypeTesting). All ids are canonical;
// bespoke phase ids are gone. Weights sum to 100 within each profile.
func ProfileFor(t ActivityType, v TestingVariant) Profile {
	switch t {
	case ActivityTypeFrontend:
		// Code-as-design: design-heavy, construction is data-wiring.
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 15, "UX Requirements"},
			{MethodPhaseDetailedDesign, 25, "Design"},
			{MethodPhaseTestPlan, 10, "Flows"},
			{MethodPhaseConstruction, 35, "Construction"},
			{MethodPhaseIntegration, 15, "Integration"},
		}}
	case ActivityTypeTesting:
		return profileForTestingVariant(v)
	case ActivityTypeDeployment:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 25, "Provisioning Spec"},
			{MethodPhaseConstruction, 50, "Construction"},
			{MethodPhaseIntegration, 25, "Convergence Verification"},
		}}
	case ActivityTypeDocumentation:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 20, "Outline"},
			{MethodPhaseConstruction, 60, "Authoring"},
			{MethodPhaseIntegration, 20, "Doc Review"},
		}}
	case ActivityTypeUIDesign:
		// A UI-design activity produces a CONCEPT, not code: it stops at the design
		// artifact, and the SPA surfaces that realize it are separate Frontend
		// activities. Hence no test-plan/construction/integration phases at all.
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 40, "UX Requirements"},
			{MethodPhaseDetailedDesign, 60, "Design Concept"},
		}}
	case ActivityTypeIntegration:
		// An I-* activity IS the integration of already-constructed components: it has
		// no requirements/design/construction of its own, only the integration pass.
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseIntegration, 100, "Integration"},
		}}
	case ActivityTypeService: // the zero value — the canonical five, same as default.
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 15, "Requirements"},
			{MethodPhaseDetailedDesign, 20, "Detailed Design"},
			{MethodPhaseTestPlan, 10, "Test Plan"},
			{MethodPhaseConstruction, 40, "Construction"},
			{MethodPhaseIntegration, 15, "Integration"},
		}}
	default: // ActivityTypeService — the canonical five.
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 15, "Requirements"},
			{MethodPhaseDetailedDesign, 20, "Detailed Design"},
			{MethodPhaseTestPlan, 10, "Test Plan"},
			{MethodPhaseConstruction, 40, "Construction"},
			{MethodPhaseIntegration, 15, "Integration"},
		}}
	}
}

func profileForTestingVariant(v TestingVariant) Profile {
	switch v {
	case TestVariantHarness:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 15, "Harness Design"},
			{MethodPhaseConstruction, 70, "Harness Construction"},
			{MethodPhaseIntegration, 15, "Harness Review"},
		}}
	case TestVariantPerf:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 25, "Perf Scenario Design"},
			{MethodPhaseConstruction, 50, "Rig Construction"},
			{MethodPhaseIntegration, 25, "Rig Review"},
		}}
	case TestVariantSystemTest:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 10, "Smoke Pass"},
			{MethodPhaseConstruction, 45, "Use-Case Execution"},
			{MethodPhaseIntegration, 45, "Regression & Sign-off"},
		}}
	case TestVariantQAProcess:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 40, "Gate Definition"},
			{MethodPhaseConstruction, 60, "Process Audit"},
		}}
	case TestVariantPlan: // the zero value (N-STP) — same as default.
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 20, "Use-Case Trace"},
			{MethodPhaseConstruction, 45, "Plan Authoring"},
			{MethodPhaseIntegration, 35, "Plan Review"},
		}}
	default: // TestVariantPlan (N-STP)
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 20, "Use-Case Trace"},
			{MethodPhaseConstruction, 45, "Plan Authoring"},
			{MethodPhaseIntegration, 35, "Plan Review"},
		}}
	}
}

// constructionprogress.go implements the App-A §2 weighted-progress formulas as
// pure functions (no I/O, no Temporal). Called at read time by the caller
// (projectManager.GetProject / constructionManager) — never persisted, so progress
// can never drift from the Phases completion record.

// ActivityProgress returns the App-A §2 progress for one activity: the sum of
// Phase.Weight for all Completed phases (0–100). Returns 0 for an activity with
// no phases yet (NotStarted). Pure: no I/O.
func ActivityProgress(status ActivityConstructionStatus) int {
	sum := 0
	for _, pc := range status.Phases {
		if pc.Completed {
			sum += pc.Weight
		}
	}
	return sum
}

// ProjectEarnedValue returns the App-A §2 project-level earned value as a fraction
// in [0, 1]: Σ(E_i × A_i(t)) / Σ E_i, where E_i = effortDays[activityID] and
// A_i(t) = ActivityProgress / 100. Activities not present in effortDays contribute
// E_i = 1.0 (equal weighting default). Returns 0 for empty input or zero total effort.
// Pure: no I/O.
func ProjectEarnedValue(statuses []ActivityConstructionStatus, effortDays map[string]float64) float64 {
	if len(statuses) == 0 {
		return 0.0
	}
	var totalEffort float64
	var earnedEffort float64
	for _, st := range statuses {
		e := 1.0
		if effortDays != nil {
			if v, ok := effortDays[st.ActivityID]; ok {
				e = v
			}
		}
		a := float64(ActivityProgress(st)) / 100.0
		earnedEffort += e * a
		totalEffort += e
	}
	if totalEffort == 0 {
		return 0.0
	}
	return earnedEffort / totalEffort
}

// StaleCause is the ADDITIVE stale-cause record for the F38 staleness rail.
//
// When an upstream artifact re-commits (an amendment), commitTransition flags every
// already-committed DOWNSTREAM slot StaleBasis. StaleBasis alone answers "is this slot's
// basis shifted?" but not "shifted BY WHAT?" — so the UI can only show a bare "stale"
// chip. StaleCause records the CAUSE: which upstream slot amended, and its NEW revision
// after that amendment, so the read model can say e.g. "Volatilities rev 2 changed after
// this was committed".
//
// It is purely additive and backward-compatible:
//   - omitempty everywhere: a non-stale slot carries no cause, and the on-disk shape is
//     byte-identical for every slot the rail never touched.
//   - NO migration: a slot that went stale BEFORE this field existed reads back with a
//     nil cause (StaleBasis true, StaleBasisCause nil). Absent cause is allowed — readers
//     treat it as "stale, cause unknown".
type StaleCause struct {
	// UpstreamKind is the wire name of the upstream artifact kind whose amendment shifted
	// this slot's basis (e.g. "volatilities").
	UpstreamKind string `json:"upstreamKind"`
	// UpstreamRevision is that upstream slot's commit/revision count AFTER the amendment
	// that caused this slot to go stale (e.g. 2 — "rev 2 changed after this was committed").
	UpstreamRevision int64 `json:"upstreamRevision"`
}

// corpusderive.go holds the PURE corpus→typed-state derivation rules (Task 2). No
// filesystem access — Task 3 (cmd/seed-construction) does the IO and feeds these the
// observed CorpusPresence. Reproducible, deterministic, unit-testable.

// CorpusPresence is what the corpus scanner observed for one activity id.

// a log/<id>.md exists
// a matching *-review.md / -R log exists (passing)
// a contracts/<component>.md exists
// corpus-relative path to the contract, when HasContract

// DeriveKind maps an activity to its kind from the activity-id family.
// Only U-SPA* activities are "frontend" — SPA UI-design activities are the sole
// frontend kind. N-* activities are testing. Everything else (including all
// *Client / *Manager / *Engine / *Access components and infra/integration) is
// service, because a Client component exposes a service contract just like any
// other service-layer component.
func DeriveKind(activityID, componentName string) ActivityKind {
	_ = componentName // caller passes it; classification is id-based only
	id := strings.ToUpper(activityID)
	switch {
	case strings.HasPrefix(id, "U-SPA"):
		return ActivityKindFrontend
	case strings.HasPrefix(id, "N-"):
		return ActivityKindTesting
	default:
		return ActivityKindService
	}
}

// DeriveType maps an activity id prefix to its canonical ActivityType. Mirrors
// DeriveKind's prefix logic (U-SPA → Frontend, N- → Testing, else Service) but is
// the forward-looking name (DeriveKind is retained for the legacy Kind field).
func DeriveType(activityID string) ActivityType {
	id := strings.ToUpper(activityID)
	switch {
	case strings.HasPrefix(id, "U-SPA"):
		return ActivityTypeFrontend
	case strings.HasPrefix(id, "N-"):
		return ActivityTypeTesting
	default:
		return ActivityTypeService
	}
}

// DeriveVariant maps a testing activity id prefix to its TestingVariant. Meaningful
// only when DeriveType == ActivityTypeTesting; unknown N- ids fall back to Plan.
// Order matters: N-STH / N-STP share the "N-ST" stem, so match the longer first.
func DeriveVariant(activityID string) TestingVariant {
	id := strings.ToUpper(activityID)
	switch {
	case strings.HasPrefix(id, "N-STH"):
		return TestVariantHarness
	case strings.HasPrefix(id, "N-STP"):
		return TestVariantPlan
	case strings.HasPrefix(id, "N-PERF"):
		return TestVariantPerf
	case strings.HasPrefix(id, "N-IT"):
		return TestVariantSystemTest
	case strings.HasPrefix(id, "N-QA"):
		return TestVariantQAProcess
	default:
		return TestVariantPlan
	}
}

// ClassifyActivity determines an activity's canonical (ActivityType, TestingVariant)
// pair from the three facts that EXIST AT DISPATCH TIME: its id, the owning
// workerClass, and the coding flag — all three authored into the committed Phase-2
// activity list. It supersedes the id-prefix-only DeriveType, whose N-* prefix arm
// conflated genuine testing (N-IT/N-STP/N-PERF/N-QA/N-STH) with infra (N-ENV/N-SC/
// N-CI), deployment (N-DEPLOY/N-HARD) and documentation (N-DOC/N-SCHEMA/N-ADR), and
// so handed an infra activity a testing slash-command (the N-ENV defect: the agent
// correctly refused to author a testing artifact, the executor read "no commits" as
// failure, and the activity died of VarianceExhausted).
//
// It deliberately does NOT take hasServiceContract. A service contract is a
// BACKWARD-LOOKING corpus observation: it is produced by the detailed-design PHASE of
// the very activity being dispatched, so it cannot exist when the dispatch decision is
// made. Consulting it here would be the same category error as the original bug.
//
// Precedence, in order — the first matching rule wins, and there is NO default arm:
//
//  1. workerClass ∈ {software-tester, test-engineer, qa-engineer} → Testing, with the
//     variant read off the id (DeriveVariant)
//  2. workerClass == "ui-designer"    → Frontend when coding, else UIDesign
//  3. id prefix U-SPA                 → Frontend
//  4. id prefix I-                    → Integration
//  5. coding == true                  → Service
//  6. workerClass ∈ {system-architect, project-manager, product-manager} → Documentation
//  7. workerClass ∈ {senior-developer, junior-developer}                 → Deployment
//  8. otherwise                       → error (the activity is unclassifiable; repair
//     is to amend workerClass or coding in the committed activity list)
//
// The returned TestingVariant is meaningful only when the type is Testing; it is the
// zero value (TestVariantPlan) otherwise.
func ClassifyActivity(id, workerClass string, coding bool) (ActivityType, TestingVariant, error) {
	switch workerClass {
	case "software-tester", "test-engineer", "qa-engineer":
		return ActivityTypeTesting, DeriveVariant(id), nil
	case "ui-designer":
		if coding {
			return ActivityTypeFrontend, TestVariantPlan, nil
		}
		return ActivityTypeUIDesign, TestVariantPlan, nil
	}
	upper := strings.ToUpper(id)
	if strings.HasPrefix(upper, "U-SPA") {
		return ActivityTypeFrontend, TestVariantPlan, nil
	}
	if strings.HasPrefix(upper, "I-") {
		return ActivityTypeIntegration, TestVariantPlan, nil
	}
	if coding {
		return ActivityTypeService, TestVariantPlan, nil
	}
	switch workerClass {
	case "system-architect", "project-manager", "product-manager":
		return ActivityTypeDocumentation, TestVariantPlan, nil
	case "senior-developer", "junior-developer":
		return ActivityTypeDeployment, TestVariantPlan, nil
	}
	return ActivityTypeService, TestVariantPlan, fmt.Errorf(
		"activity %s: workerClass %q with coding=%v matches no classification rule", id, workerClass, coding)
}

// ClassifyType is the VIEW-MODEL classification lens over ClassifyActivity. It adds the
// one signal a read-time view has and a dispatch does not: hasServiceContract, a
// backward-looking corpus observation that an activity DID build a component, which
// wins over every rule below it.
//
// It is deliberately LENIENT where ClassifyActivity is strict: a row the rules cannot
// classify still has to render, so an unclassifiable activity falls back to Deployment
// (the pre-existing render-only fallback for a noncoding activity of unknown worker
// class). Dispatch must never take that fallback — it blocks instead (see
// nextEligibleActivity / ActivityUnclassifiable). Sharing the one classifier is what
// keeps the view and the dispatch from drifting.
func ClassifyType(id, workerClass string, coding, hasServiceContract bool) ActivityType {
	if hasServiceContract {
		return ActivityTypeService
	}
	typ, _, err := ClassifyActivity(id, workerClass, coding)
	if err != nil {
		return ActivityTypeDeployment
	}
	return typ
}

// DeriveBuildStatus maps corpus presence to the finer build-status lens. integrated is
// true only when a log AND a passing review both exist.
func DeriveBuildStatus(p CorpusPresence) (ActivityBuildStatus, bool) {
	switch {
	case p.HasLog && p.HasPassingReview:
		return BuildIntegrated, true
	case p.HasLog:
		return BuildInReview, false
	default:
		return BuildInConstruction, false
	}
}

// DeriveProduced builds the produced-artifact list from corpus evidence, KIND-SPECIFIC
// by the activity type. A frozen contract is emitted for any component that produced
// one (a Client exposes one too). The built work-product then differs by type — this is
// the fix for the "generic code stub" that made frontend/deployment activities render a
// thin, wrong Artifacts-tab experience:
//
//   - Frontend    → a ui-design CONCEPT artifact + a ui-code artifact whose Source
//     carries the SPA preview ROUTE (a "/project/..." path). The
//     Artifacts-tab frontend renderer frames that route as a live
//     same-origin iframe. Source is backfilled with the real route per
//     surface (seed leaves it empty; the renderer degrades gracefully).
//   - Deployment  → a single deployment artifact (the applied provisioning change).
//   - everything  → the generic built-component "code" artifact (unchanged).
func DeriveProduced(p CorpusPresence, componentName string, typ ActivityType) []ProducedArtifact {
	var out []ProducedArtifact
	if p.HasContract {
		out = append(out, ProducedArtifact{
			Kind:     "service-contract",
			Title:    componentName + " — service contract",
			Source:   p.ContractFile,
			Produced: true,
			Note:     "Frozen App-B service contract.",
		})
	}
	if !p.HasLog {
		return out
	}
	switch typ {
	case ActivityTypeFrontend:
		out = append(out,
			ProducedArtifact{
				Kind:     "ui-design",
				Title:    componentName + " — UI design concept",
				Source:   "implementation/log",
				Produced: true,
				Note:     "UI-design concept: personas, screens, layout, and flows for this surface.",
			},
			ProducedArtifact{
				Kind:     "ui-code",
				Title:    componentName + " — built UI",
				Source:   "", // backfilled with the SPA preview route, e.g. /project/archistrator/design/system
				Produced: true,
				Note:     "SPA surface built against the approved design; preview at the route in Source.",
			},
		)
	case ActivityTypeUIDesign:
		// A UI-design activity stops at the CONCEPT — the ui-code stub belongs to the
		// Frontend activities that realize it, so emitting one here would advertise a
		// built surface that this activity never produced.
		out = append(out, ProducedArtifact{
			Kind:     "ui-design",
			Title:    componentName + " — UI design concept",
			Source:   "implementation/log",
			Produced: true,
			Note:     "UI-design concept: personas, screens, layout, and flows for this surface.",
		})
	case ActivityTypeDeployment:
		out = append(out, ProducedArtifact{
			Kind:     "deployment",
			Title:    componentName + " — deployment change",
			Source:   "implementation/log",
			Produced: true,
			Note:     "Provisioning/deployment change applied and verified against the target environment.",
		})
	case ActivityTypeService, ActivityTypeTesting, ActivityTypeDocumentation, ActivityTypeIntegration:
		out = append(out, ProducedArtifact{
			Kind:     "code",
			Title:    componentName + " — built component",
			Source:   "implementation/log",
			Produced: true,
			Note:     "Construction output recorded in the implementation log.",
		})
	default:
		out = append(out, ProducedArtifact{
			Kind:     "code",
			Title:    componentName + " — built component",
			Source:   "implementation/log",
			Produced: true,
			Note:     "Construction output recorded in the implementation log.",
		})
	}
	return out
}

// profileSlug is the .claude/commands filename stem for an activity profile.
// For testing it encodes the variant (testing-plan/harness/perf/systemtest/qa);
// all other types map 1:1 to their wire name.
func profileSlug(t ActivityType, v TestingVariant) string {
	switch t {
	case ActivityTypeFrontend:
		return "frontend"
	case ActivityTypeUIDesign:
		// A UI-design activity walks the FRONTEND command family's first two phases
		// (/frontend-requirements, /frontend-detailed-design) — the same prompts, run
		// by the ui-designer worker class. No new command files are needed.
		return "frontend"
	case ActivityTypeIntegration:
		// An I-* activity walks /service-integration — the generic integration prompt.
		return "service"
	case ActivityTypeDeployment:
		return "deployment"
	case ActivityTypeDocumentation:
		return "documentation"
	case ActivityTypeTesting:
		switch v {
		case TestVariantHarness:
			return "testing-harness"
		case TestVariantPerf:
			return "testing-perf"
		case TestVariantSystemTest:
			return "testing-systemtest"
		case TestVariantQAProcess:
			return "testing-qa"
		case TestVariantPlan: // the zero value — same as default.
			return "testing-plan"
		default: // TestVariantPlan
			return "testing-plan"
		}
	case ActivityTypeService: // the zero value — same as default.
		return "service"
	default: // ActivityTypeService
		return "service"
	}
}

// kebabPhase renders a canonical phase id as a command slug segment
// (detailed_design -> detailed-design).
func kebabPhase(p ActivityMethodPhase) string {
	return strings.ReplaceAll(string(p), "_", "-")
}

// CommandFor returns the .claude slash-command name for a (type, variant, phase)
// cell: "<profileSlug>-<phaseSlug>". It is total over exactly the phases
// ProfileFor(t, v) emits, and matches a .claude/commands/<name>.md file.
func CommandFor(t ActivityType, v TestingVariant, p ActivityMethodPhase) string {
	return profileSlug(t, v) + "-" + kebabPhase(p)
}

// DesignJobMode is the dispatch shape of a design job (Plan-2 Task B1): a fresh
// draft, a PM-critique pass over a drafted artifact, or an answer to open review
// questions. It is a NEW dispatch-wire concept, not derived from ArtifactKind —
// the same kind is drafted, critiqued, and answered-against at different points
// in a design session.
type DesignJobMode string

// The three dispatch shapes a design job can take (see DesignJobMode).
const (
	DesignJobModeDraft    DesignJobMode = "draft"
	DesignJobModeCritique DesignJobMode = "critique"
	DesignJobModeAnswer   DesignJobMode = "answer"
)

// designKindSlugs backs designKindSlug — a table lookup (the gocyclo-friendly
// form of flat enum→value dispatch; the exhaustive linter's map check enforces
// a key per variant exactly as it would enforce a case).
var designKindSlugs = map[ArtifactKind]string{
	KindMission:              "mission",
	KindGlossary:             "glossary",
	KindScrubbedRequirements: "scrubbed-requirements",
	KindVolatilities:         "volatilities",
	KindCoreUseCases:         "core-use-cases",
	KindSystem:               "system",
	KindOperationalConcepts:  "operational-concepts",
	KindStandardCheck:        "standard-check",
	KindPlanningAssumptions:  "planning-assumptions",
	KindActivityList:         "activity-list",
	KindNetwork:              "network",
	KindNormalSolution:       "normal-solution",
	KindSubcriticalSolution:  "subcritical-solution",
	KindCompressedSolution:   "compressed-solution",
	KindDecompressedSolution: "decompressed-solution",
	KindRiskModel:            "risk-model",
	KindSdpReview:            "sdp-review",
}

// designKindSlug is the ArtifactKind -> kebab .claude command-slug stem for
// design jobs. Distinct from WireName (camelCase, JSON discriminator) and
// String (PascalCase, diagnostics) — this is the THIRD, kebab-case rendering,
// and DesignCommandFor is its only caller. Unknown kinds yield "".
func designKindSlug(k ArtifactKind) string {
	return designKindSlugs[k]
}

// designKindHasCritique reports whether kind takes a critique design job at all:
// the PM critiques the four business-alignment kinds, and the ARCHITECT
// self-critiques the architecture (KindSystem — the system-critique command is
// architect-role; QA amendment 2026-07-17: the architecture reached the human
// gate with three blockers and zero internal critique, and the ratified "PM must
// not critique architecture" doctrine stands, so the critic is the architect).
// The remaining architect-owned Phase-1 kinds (volatilities, operational
// concepts, standard-check) and every Phase-2 kind still skip critique entirely
// (EARMARK: extend only on live QA evidence).
// LOCKSTEP PIN: this switch's case list is a DELIBERATE, non-imported duplicate
// of critiqueCriticFor (manager/systemdesign/coauthorartifact.go) — projectstate
// is a ResourceAccess and sits BELOW manager/systemdesign in the layer graph, so
// it cannot import that package's func; the two switches must be edited together.
// critiqueCriticFor carries the matching lockstep pointer back to this func.
func designKindHasCritique(k ArtifactKind) bool {
	switch k {
	case KindMission, KindGlossary, KindScrubbedRequirements, KindCoreUseCases,
		KindSystem:
		return true
	case KindVolatilities, KindOperationalConcepts, KindStandardCheck,
		KindPlanningAssumptions, KindActivityList, KindNetwork, KindNormalSolution,
		KindSubcriticalSolution, KindCompressedSolution, KindDecompressedSolution,
		KindRiskModel, KindSdpReview:
		return false
	default:
		return false
	}
}

// DesignCommandFor maps a design job (kind, mode, addressee) to its .claude
// command slug — the design-dispatch counterpart to CommandFor's
// construction-activity mapping above. addressee is consulted ONLY for
// DesignJobModeAnswer ("pm" | "architect" select design-answer-pm vs
// design-answer); it is ignored for draft/critique. Returns "" for
// undispatchable combinations — SdpReview in any mode (assembled server-side,
// never dispatched), critique for a kind designKindHasCritique excludes, or
// an unrecognized addressee in answer mode. Callers treat "" as a
// contract-misuse error: the caller asked for a job shape the Method doesn't
// produce.
func DesignCommandFor(k ArtifactKind, mode DesignJobMode, addressee string) string {
	if k == KindSdpReview {
		return ""
	}
	switch mode {
	case DesignJobModeDraft:
		if slug := designKindSlug(k); slug != "" {
			return slug + "-draft"
		}
		return ""
	case DesignJobModeCritique:
		if !designKindHasCritique(k) {
			return ""
		}
		if slug := designKindSlug(k); slug != "" {
			return slug + "-critique"
		}
		return ""
	case DesignJobModeAnswer:
		switch addressee {
		case "architect":
			return "design-answer"
		case "pm":
			return "design-answer-pm"
		default:
			return ""
		}
	default:
		return ""
	}
}

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
	var maxRound int64
	for _, c := range thread {
		if c.Round > maxRound {
			maxRound = c.Round
		}
	}
	return maxRound + 1
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

// ReviewPolicy is the per-project, committed configuration of WHICH phases require a
// human approval gate during construction. It composes with the reviewEngine (which
// computes WHO reviews): the engine gives the reviewer set; this policy says whether a
// human must sign off before the phase advances. The zero value gates nothing — the
// construction loop then behaves exactly as before this feature ("pure vibes").

// GatedPhasesByType maps an ActivityType wire name ("service"/"frontend"/"testing"/...)
// to the canonical phases that require human approval for that type.

// RequiresHuman reports whether a phase of the given activity type requires human approval.
func (p ReviewPolicy) RequiresHuman(activityType string, phase ActivityMethodPhase) bool {
	return slices.Contains(p.GatedPhasesByType[activityType], phase)
}

// Review preset values for ReviewPolicy.Preset (Task 7, local-first sophistication
// dial). "" (nil/unset) is the legacy/explicit mode — RequiresHuman's committed
// GatedPhasesByType map, unchanged pre-preset behavior (e.g. the webApp PolicyPanel's
// ReviewPolicyFromGateIDs output).
const (
	// ReviewPresetVibes auto-approves every draft/step — nothing gated beyond the
	// non-overridable floor (see ContractTouchesReviewFloor).
	ReviewPresetVibes = "vibes"
	// ReviewPresetCheckpoints gates only the per-activity contract/architecture commit
	// (MethodPhaseDetailedDesign) and the construction dispatch (MethodPhaseConstruction).
	ReviewPresetCheckpoints = "checkpoints"
	// ReviewPresetFull gates every phase — today's approve-everything behavior.
	ReviewPresetFull = "full"
)

// reviewFloorKeywords is the NON-OVERRIDABLE construction-dispatch gate list (data,
// not policy, per task-7-brief.md): a construction-phase dispatch of ANY activity
// whose contract touches one of these keywords always requires human approval —
// deploy/spend/schema-shaped operations stay gated under every preset, including
// "vibes". Case-insensitive substring match against each contract operation's Name.
// No preset value can widen or narrow this list.
var reviewFloorKeywords = []string{"deploy", "spend", "schema"}

// ContractTouchesReviewFloor reports whether contract carries an operation whose name
// matches a reviewFloorKeywords entry. It is the floor's sole input — computed ONCE
// per construction execution (constructactivity.go's loadReviewSnapshot start-
// snapshot) and never re-evaluated mid-loop, mirroring the policy snapshot itself.
func ContractTouchesReviewFloor(contract ServiceContract) bool {
	for _, op := range contract.Interface.Operations {
		name := strings.ToLower(op.Name)
		for _, kw := range reviewFloorKeywords {
			if strings.Contains(name, kw) {
				return true
			}
		}
	}
	return false
}

// EffectiveGate resolves the Preset switch for (activityType, phase) and THEN applies
// the non-overridable floor: a MethodPhaseConstruction dispatch with floorTouched=true
// (the activity's committed contract touches deploy/spend/schema, per
// ContractTouchesReviewFloor) always requires human approval — no preset, including
// "vibes", can bypass it. This is the construction phase gate's ONLY preset-aware
// entry point (constructactivity.go's runPhaseGate); RequiresHuman stays the pure
// explicit-map lookup for backward compatibility (webApp PolicyPanel gate ids, and the
// legacy/explicit "" preset fallback below).
//
// checkpoints gates the per-activity contract/architecture commit
// (MethodPhaseDetailedDesign), the construction dispatch (MethodPhaseConstruction) and
// the integration pass (MethodPhaseIntegration) — the funnel checkpoints this
// per-activity, per-phase mechanism can express.
// The funnel's remaining checkpoint ("SDP commit") is a projectDesignManager artifact
// commit with no ActivityMethodPhase analog; that workflow gates it unconditionally
// today, independent of ReviewPolicy — see docs/superpowers/sdd/task-7-report.md.
//
// MethodPhaseIntegration is in the list because ActivityTypeIntegration's profile is
// integration-ONLY (100% weight, one phase): without it an I-* activity would be the
// one activity family that runs entirely ungated under "checkpoints", which is exactly
// backwards — an integration activity is where a use case is first exercised end to
// end (founder-ratified).
func (p ReviewPolicy) EffectiveGate(activityType string, phase ActivityMethodPhase, floorTouched bool) bool {
	if phase == MethodPhaseConstruction && floorTouched {
		return true
	}
	preset := ""
	if p.Preset != nil {
		preset = *p.Preset
	}
	switch preset {
	case ReviewPresetVibes:
		return false
	case ReviewPresetFull:
		return true
	case ReviewPresetCheckpoints:
		return phase == MethodPhaseDetailedDesign || phase == MethodPhaseConstruction ||
			phase == MethodPhaseIntegration
	default:
		return p.RequiresHuman(activityType, phase)
	}
}

// gateIDToPhase maps the webApp PolicyPanel's ad-hoc gate ids to canonical phases, so the
// mock vocabulary never reaches head-state. Canonical ids pass through in ReviewPolicyFromGateIDs.
var gateIDToPhase = map[string]ActivityMethodPhase{
	"svc-contract": MethodPhaseDetailedDesign,
	"svc-review":   MethodPhaseIntegration,
	"fe-approve":   MethodPhaseDetailedDesign,
	"test-plan":    MethodPhaseTestPlan,
}

// ReviewPolicyFromGateIDs builds a policy from per-type gate-id lists (canonical or ad-hoc).
func ReviewPolicyFromGateIDs(byType map[string][]string) ReviewPolicy {
	out := ReviewPolicy{GatedPhasesByType: map[string][]ActivityMethodPhase{}}
	for typ, ids := range byType {
		for _, id := range ids {
			ph, ok := gateIDToPhase[id]
			if !ok {
				ph = ActivityMethodPhase(id)
			}
			switch ph {
			case MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
				MethodPhaseConstruction, MethodPhaseIntegration:
				out.GatedPhasesByType[typ] = append(out.GatedPhasesByType[typ], ph)
			}
		}
	}
	return out
}

// Package projectstate is the projectStateAccess component of the aiarch
// server's ResourceAccess layer — the Temporal-free port over the project's
// HEAD-STATE aggregate (projectStateAccess.md). The Project aggregate is a
// single stored row holding current state, mutated in place by atomic business
// verbs under optimistic concurrency + idempotency. There is NO event log, NO
// projection, NO fold: the stored row IS the truth.
//
// Re-cut 2026-05-26 to typed Method models (projectStateAccess.md §0): each
// Phase-1/2 artifact is a NAMED TYPED SLOT on Project holding the canonical
// typed ArtifactModel plus its review status — not an opaque ArtifactRef.
// stageArtifactForReview carries the typed model (routed to its slot by Kind());
// commit/reject/withdraw key by ArtifactKind (the model is already in the slot).
// ArtifactRef is gone.
//
// Per The Method's layer model ([[the-method-layers]]): ResourceAccess
// components import NO Temporal. The typed Method models and the head-state
// aggregate are OWNED HERE — this is the RA that fronts them — and reached by
// every downstream layer (Manager, Engine) as a downward import. The component
// also imports framework-go/resourceaccess for the shared error model and
// IdempotencyKey, both acyclic, layer-internal edges.
//
// The component records facts; it does NOT author business decisions (Non-goal:
// no business-decision logic). The systemDesignManager reads the head-state,
// applies its Phase-1 transition gate, asks artifactValidationEngine to validate
// content, and only then calls the atomic verb here to persist the outcome.

// ProjectStateAccess — the Temporal-free port over the project head-state
// aggregate (projectStateAccess.md §2) — is now GENERATED into contract.gen.go
// from the projectStateAccess `.serviceContracts` entry in .aiarch/state/project.json
// (schema-first: the PORT + domain model types are
// regenerated, but the persistence codec stays HAND-WRITTEN here,
// the canonical source of truth). Its 8 atomic verbs take rc fwra.Context first
// (carrying ctx + idempotency key). Every write verb honours optimistic
// concurrency (expectedVersion → fwra.Conflict on a stale value) AND idempotency
// (rc.IdempotencyKey deduped in a ledger; a retry collapses to the committed
// version). The verbs record facts; they do NOT re-decide whether a transition is
// allowed (the Manager's gate) nor whether the typed model is semantically valid
// (artifactValidationEngine).

// Error is the shared ResourceAccess error model (framework-go), re-exported as
// an alias so this component's contract reads in its own terms while every RA
// component shares one fixed enum. Construct with fwra.New / fwra.Wrap using the
// shared kinds (fwra.NotFound, fwra.Conflict, fwra.Transient, fwra.Infrastructure,
// fwra.ContractMisuse).
type Error = fwra.Error

// --- Historical activity-ID alias map -------------------------------------
//
// Maps the HISTORICAL hand-chosen activity short names (C-BM, C-AA, …) to the
// DERIVED canonical ids (C-<component-id>).
//
// Why an alias map and not a key rewrite: all 69 rows in .activityConstruction
// are keyed by the historical short names and every one is Done+Integrated.
// Rewriting completed construction records to gain cosmetic key consistency is
// risk with no payoff (founder ruling, 2026-08-09). The short name survives as
// a render label; the canonical id is what the derivation produces and what
// new state keys off.
//
// ONE SIGNATURE FOR "NO DERIVED COUNTERPART" (2026-08-10 review fix): an
// entry belongs in this map ONLY when its canonical id names an activity the
// derivation actually emits TODAY. A historical key whose only possible
// canonical target is something the derivation will never (again) produce —
// because the component is gone, because it is deliberately componentless,
// or because a founder ruling removed that whole activity category — is left
// OUT of the map entirely, so ResolveActivityAlias reports ok=false for it,
// the SAME signal a genuine typo gets. Before this fix the same underlying
// fact ("no derived counterpart") was reported two different ways depending
// on whether anyone had bothered to type the entry in: the three zombies
// (C-HE, C-WIA, R-WIT — components that no longer exist) and R-DER
// (componentless) were simply absent (ok=false), while 12 other equally
// uncounterparted keys — the three generated-transport clients (C-CW, C-CM,
// C-CS), the four "provided" utilities (C-SE, C-LG, C-DG, C-DA), and all
// five former integration activities (I-UC1..I-UC5, eliminated entirely by
// the 2026-08-09 founder ruling that folds integration into every activity's
// own lifecycle) — were IN the map and returned ok=true for a canonical id
// nothing derives. All 16 are now absent, uniformly ok=false; see the
// canonical id each used to carry in the git history of this file if that
// mapping is needed again.
//
// Only C-* / R-* activities that carried a componentId AND whose component
// the current architecture still builds get a 1:1 alias. U-SPA-1..U-SPA-5
// get a 1:1 alias too (Task 10 completion, 2026-08-10): the spec tabulates
// them against the five managers 1:1, so — unlike the rest of the U-SPA-*
// set, which is re-derived per MANAGER as a genuinely different
// decomposition and needs no alias — these five ARE plain renames, resolved
// by hand from each activity's committed title against the five manager
// components (e.g. U-SPA-1's title, "SPA — Phase-1 system-design screens",
// names the one manager that owns Phase-1 system design). U-SPA-6 and
// U-SPA-TEAM stay unaliased: both are genuinely cross-cutting screens (a
// change-request re-entry flow; a Team/Agents roster) that no single manager
// owns, so — like N-* and the rest of U-SPA-* — they resolve through the
// prefix fallback in TestEveryHistoricalConstructionKeyResolvesToADerivedActivity
// rather than a 1:1 rename entry here.

// activityAliases maps historical short name → derived canonical id.
var activityAliases = map[string]string{
	"C-AA":  "C-artifact-access",
	"C-AE":  "C-autoscaler-engine",
	"C-BE":  "C-billing-engine",
	"C-BG":  "C-merchant-gateway-access",
	"C-BM":  "C-billing-manager",
	"C-BS":  "C-billing-state-access",
	"C-CP":  "C-agentic-job-access",
	"C-DH":  "C-design-health-engine",
	"C-EA":  "C-episode-access",
	"C-EE":  "C-estimation-engine",
	"C-IE":  "C-intervention-engine",
	"C-MCN": "C-construction-manager",
	"C-MOP": "C-operations-manager",
	"C-MPD": "C-project-design-manager",
	"C-MSD": "C-system-design-manager",
	"C-OE":  "C-operation-estimation-engine",
	"C-OR":  "C-operated-runtime-access",
	"C-OSA": "C-operated-system-state-access",
	"C-PA":  "C-project-state-access",
	"C-RE":  "C-review-engine",
	"C-SC":  "C-source-control-access",
	"C-UA":  "C-usage-access",

	// HAND-DERIVED: the generator keys on componentId, and no R-* activity in slot 9
	// carries one (never backfilled), so these four were resolved by hand from the
	// activity titles against the committed System's four provisioning:"vendor"
	// components, each of which the deriver emits an R-<component-id> activity for.
	// R-DER ("Durable Execution Runtime") and R-WIT ("Work Item Tracker") are
	// deliberately excluded: R-DER is componentless (an additive delta, same category
	// as the N-* checklist activities) and R-WIT is a zombie (no such resource exists).
	"R-GH":  "R-github",                        // "Provision / register the GitHub App"
	"R-BG":  "R-merchant-gateway",              // "Provision Stripe vendor account (BillingGateway)"
	"R-CPR": "R-construction-pipeline-runtime", // "Select + provision Construction Pipeline Runtime"
	"R-ORS": "R-operated-runtime",              // "Select + provision Operated Runtime Infrastructure"

	// HAND-DERIVED (Task 10 completion, 2026-08-10): the ordinal U-SPA-<n> names were
	// never backfilled against the derivation's per-MANAGER decomposition, so these
	// five are resolved by hand from each activity's committed title against the five
	// manager components in slot 5.
	"U-SPA-1": "U-SPA-system-design-manager",  // title: "...Phase-1 system-design screens..."
	"U-SPA-2": "U-SPA-project-design-manager", // title: "...Phase-2 project-design screens..."
	"U-SPA-3": "U-SPA-construction-manager",   // title: "...Construction tracking + artifacts console..."
	"U-SPA-4": "U-SPA-operations-manager",     // title: "...Operations console..."
	"U-SPA-5": "U-SPA-billing-manager",        // title: "...Billing screens..."

	// Trivial self-alias — see the file doc above for why G-SPA is entered here
	// even though it is not a rename.
	"G-SPA": "G-SPA",
}

// canonicalToHistorical is the reverse index, built once at init from activityAliases so
// the two directions can never drift apart.
var canonicalToHistorical = func() map[string]string {
	m := make(map[string]string, len(activityAliases))
	for historical, canonical := range activityAliases {
		m[canonical] = historical
	}
	return m
}()

// ResolveActivityAlias maps a historical activity key to its derived canonical id.
// ok is false for an unknown key — never a silent pass-through, which would make a typo
// look like a valid activity.
func ResolveActivityAlias(historical string) (string, bool) {
	c, ok := activityAliases[historical]
	return c, ok
}

// HistoricalAliasFor maps a derived canonical id back to the historical short name that
// existing .activityConstruction rows are keyed by.
func HistoricalAliasFor(canonical string) (string, bool) {
	h, ok := canonicalToHistorical[canonical]
	return h, ok
}
