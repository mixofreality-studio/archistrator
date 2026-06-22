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
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
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

// GitProjectStateAccess is the §REWORK.4 port: every provider-touching verb gains
// a Manager-threaded `cred RepoCredential`. It is the git-substrate surface of the
// frozen verb vocabulary (stage/commit/reject/withdraw/advance/setResearch + the
// catalog create/list + readProject). Distinct from the Postgres-era
// ProjectStateAccess port (no cred) the downstream Managers still compile against
// until their own cred-threading re-cuts land (Manager wiring is downstream of
// C-PA-R).
type GitProjectStateAccess interface {
	CreateProject(ctx context.Context, projectID ProjectID, owner OwnerScope, name string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	ListProjects(ctx context.Context, owner OwnerScope, cred RepoCredential) ([]ProjectSummary, error)
	StageArtifactForReview(ctx context.Context, projectID ProjectID, expectedVersion Version, model ArtifactModel, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	CommitArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RejectArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	WithdrawArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	AdvancePhase(ctx context.Context, projectID ProjectID, expectedVersion Version, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	SetResearchInput(ctx context.Context, projectID ProjectID, expectedVersion Version, research ResearchInput, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	ReadProject(ctx context.Context, projectID ProjectID, cred RepoCredential) (Project, error)
}

// RepoLocator resolves a project to its per-project git repo URL + CAS target
// branch. The deterministic per-project repo NAME is implicit to the store (REWORK.5
// / review Q2: "a well-known deterministic repo name"); the LOCATOR is the seam where
// C-PA-R wiring (composition root) supplies the concrete URL scheme —
// github.com/<owner>/<repo>.git in cloud, a file:// path in LOCAL. It is a function,
// not an RA call: no sideways edge.
//
// REGISTRY REMOVED (founder ruling 2026-06-14): the cross-project registry index repo
// is gone. The project catalog is now DISCOVERED by enumerating the account's project
// repos (cloud: the GitHub App installation's repos filtered to the aiarch-project
// topic; local: the on-disk repos under the base dir). The enumeration capability is
// threaded in via ProjectCatalog, NOT a RegistryRepo() handle.
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
// It carries the logical project id (name-as-identity, C-PA-AD 2026-06-15: the repo NAME
// IS the project id — the old "aiarch-<id>" parse is gone) and the display title (from
// the repo description set at adopt), so ListProjects can build a ProjectSummary WITHOUT
// a per-repo read for the title. The phase/progress still come from project.json (the
// N+1 read below).
type ProjectCatalogRef struct {
	// ProjectID is the logical project id parsed from the deterministic repo name.
	ProjectID ProjectID
	// Title is the human display name (the repo description set at provision). May be
	// empty if the repo carried no description; the per-repo read then supplies it.
	Title string
}

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

// GitStore is the concrete git-JSON + ref-CAS implementation of
// GitProjectStateAccess. It holds only the repo locator + a flag for whether the
// substrate is local on-disk git (LOCAL profile). Every call clones fresh through
// the satellite (stateless → safe under concurrency + Temporal replay). NO IO at
// construction.
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

// Compile-time proof the concrete GitStore satisfies the §REWORK.4 port.
var _ GitProjectStateAccess = (*GitStore)(nil)

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

func (s *GitStore) StageArtifactForReview(ctx context.Context, projectID ProjectID, expectedVersion Version, model ArtifactModel, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.stageArtifactForReviewOnBranch(ctx, projectID, expectedVersion, "", model, cred, idempotencyKey)
}

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
		return nil
	})
}

func (s *GitStore) CommitArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "CommitArtifact", projectID, expectedVersion, cred, idempotencyKey, modeUpsert, statusTransition("CommitArtifact", kind, ReviewCommitted, ""))
}

func (s *GitStore) RejectArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "RejectArtifact", projectID, expectedVersion, cred, idempotencyKey, modeUpsert, statusTransition("RejectArtifact", kind, ReviewRejected, notes))
}

func (s *GitStore) WithdrawArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "WithdrawArtifact", projectID, expectedVersion, cred, idempotencyKey, modeUpsert, statusTransition("WithdrawArtifact", kind, ReviewWithdrawn, notes))
}

func (s *GitStore) AdvancePhase(ctx context.Context, projectID ProjectID, expectedVersion Version, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "AdvancePhase", projectID, expectedVersion, cred, idempotencyKey, modeUpsert, func(p *Project) error {
		p.Phase++
		return nil
	})
}

func (s *GitStore) SetResearchInput(ctx context.Context, projectID ProjectID, expectedVersion Version, research ResearchInput, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if research.IsZero() {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.SetResearchInput: empty research (no sources)")
	}
	return s.applyMutation(ctx, "SetResearchInput", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		p.ResearchInput = research
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
	existing, err := s.ReadProject(ctx, projectID, cred)
	if err == nil {
		// State already committed — RESUME (return the existing version, no write).
		return existing.Version, nil
	}
	if !isNotFound(err) {
		// A real read fault (auth/transient/infra) is surfaced; only NotFound (no state
		// yet) is the fresh-create signal.
		return 0, err
	}

	// Fresh init: no committed state — seed project.json at Version 1.
	return s.applyMutation(ctx, "CreateProject", projectID, 0, cred, idempotencyKey, modeCreateOnly, func(p *Project) error {
		p.Owner = owner
		p.Name = name
		p.Phase = PhaseSystemDesign
		return nil
	})
}

// ReadProject returns the whole head-state aggregate from the project repo's
// project.json. fwra.NotFound when the aggregate has not been created. It reads the
// locator's DEFAULT branch (main) — the canonical committed head.
func (s *GitStore) ReadProject(ctx context.Context, projectID ProjectID, cred RepoCredential) (Project, error) {
	return s.readProjectOnBranch(ctx, projectID, "", cred)
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
			summary.Phase = p.Phase
			// projectUpdatedAt checks ActivityGit entries; docUpdatedAt is the
			// doc-level stamp written on every mutation (the fallback for construction-
			// phase projects that have committed design slots but no git activity yet).
			summary.UpdatedAt = projectUpdatedAt(p)
			if summary.UpdatedAt.IsZero() {
				summary.UpdatedAt = docUpdatedAt
			}
			summary.CommittedCount, summary.TotalCount = phaseProgress(p)
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
	return s.applyMutationOnBranch(ctx, op, projectID, expectedVersion, "", cred, idempotencyKey, mode, mutate)
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

	// STEP 1 — dedup-first probe. A committed applied_mutations/<key>.json means a
	// prior attempt already landed: return its result_version, IGNORING
	// expectedVersion (a retry may re-pass a now-stale version; the dedup must win).
	if rec, found, derr := lookupAppliedInSnapshot(snap, idempotencyKey); derr != nil {
		return 0, derr
	} else if found {
		return rec.ResultVersion, nil
	}

	// STEP 2 — decode the aggregate (or open a fresh one) and run the mode gate.
	p, exists, err := decodeProjectFromSnapshot(snap, projectID)
	if err != nil {
		return 0, err
	}
	if exists && mode == modeCreateOnly {
		return 0, fwra.New(fwra.Conflict, fmt.Sprintf("projectstate.%s: project %s already exists", op, projectID))
	}
	if !exists {
		if mode == modeRequireExisting {
			return 0, fwra.New(fwra.NotFound, fmt.Sprintf("projectstate.%s: no aggregate for project %s (create it first)", op, projectID))
		}
		if expectedVersion != 0 {
			return 0, fwra.New(fwra.Conflict, fmt.Sprintf("projectstate.%s: no aggregate for project %s but expectedVersion %d != 0", op, projectID, expectedVersion))
		}
		p = Project{ID: projectID, Version: 0}
	}

	// STEP 3 — version guard (the same optimistic-concurrency check as Postgres; the
	// Version lives in the committed project.json — invariant iv: derivable from repo
	// state alone). The git ref-CAS at push time is the cross-process gate; this
	// guard catches a stale caller even before the push.
	if p.Version != expectedVersion {
		return 0, fwra.New(fwra.Conflict, fmt.Sprintf("projectstate.%s: stale version for project %s: have %d, expected %d", op, projectID, p.Version, expectedVersion))
	}

	// STEP 4 — pure in-memory transition + version bump.
	if mErr := mutate(&p); mErr != nil {
		return 0, mErr
	}
	p.Version = expectedVersion + 1

	// STEP 5 — build the new subtree (whole project.json + ALL dedup records,
	// carrying forward the existing ones) and write the new dedup record in the SAME
	// commit (REWORK.3 same-commit coupling — atomic per git ref update).
	files, err := buildStateFiles(snap, &p, idempotencyKey, p.Version, op, s.now())
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

// ---------------------------------------------------------------------------
// Snapshot codec — project.json + applied_mutations/*.json.
// ---------------------------------------------------------------------------

// projectDoc is the on-infrastructure JSON shape of the whole Project aggregate.
// It mirrors the head-state fields; the typed-model slots are encoded with the
// SAME kind-discriminated envelope the Postgres JSONB codec uses (slotJSON), so
// the two substrates serialize a slot identically and a model round-trips across
// either store.
type projectDoc struct {
	ID       string              `json:"id"`
	Version  uint64              `json:"version"`
	Phase    int                 `json:"phase"`
	Owner    string              `json:"owner"`
	Name     string              `json:"name"`
	Research ResearchInput       `json:"research"`
	Slots    map[string]slotJSON `json:"slots"`
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
	// UpdatedAt is the server-resolved timestamp of the last committed state
	// mutation (set by buildStateFiles on every write). omitempty so existing
	// project.json documents that pre-date this field decode cleanly as the zero
	// time — the catalog falls back to the ActivityGit tiebreak or projectID sort
	// for those. Populated once a mutation is applied after this field was added.
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
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
		return Project{}, false, fwra.Wrap(fwra.Infrastructure, err, "projectstate: decode project.json")
	}
	p := Project{
		ID:                   projectID,
		Version:              Version(doc.Version),
		Phase:                Phase(doc.Phase),
		Owner:                OwnerScope(doc.Owner),
		Name:                 doc.Name,
		ResearchInput:        doc.Research,
		ActivityGit:          doc.ActivityGit,
		ActivityConstruction: doc.ActivityConstruction,
		ConstructionProgress: doc.ConstructionProgress,
		ServiceContracts:     doc.ServiceContracts,
		PhaseArtifacts:       doc.PhaseArtifacts,
		TestingState:         doc.TestingState,
		OperatorPaused:       doc.OperatorPaused,
		PauseReason:          doc.PauseReason,
	}
	if err := decodeSlotsMap(doc.Slots, &p); err != nil {
		return Project{}, false, fwra.Wrap(fwra.Infrastructure, err, "projectstate: decode slots")
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
func buildStateFiles(snap fwgithub.GitSnapshot, p *Project, key fwra.IdempotencyKey, resultVersion Version, op string, now time.Time) (map[string][]byte, error) {
	files := map[string][]byte{}
	// Carry forward existing dedup records (whole-subtree write semantics).
	for path, b := range snap.Files {
		if strings.HasPrefix(path, appliedMutationsDir+"/") {
			files[path] = b
		}
	}
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
		Version:              uint64(p.Version),
		Phase:                int(p.Phase),
		Owner:                string(p.Owner),
		Name:                 p.Name,
		Research:             p.ResearchInput,
		Slots:                slots,
		ActivityGit:          p.ActivityGit,
		ActivityConstruction: p.ActivityConstruction,
		ConstructionProgress: p.ConstructionProgress,
		ServiceContracts:     p.ServiceContracts,
		PhaseArtifacts:       p.PhaseArtifacts,
		TestingState:         p.TestingState,
		OperatorPaused:       p.OperatorPaused,
		PauseReason:          p.PauseReason,
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
