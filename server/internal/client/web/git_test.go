package web

// Wire-level black-box regression tests for the C-CW-GIT read projection: the
// per-activity git head-state (D-PA-GIT) surfaces on the project head-state read
// (GET /api/v1/projects/{projectId}) as the gitRows map the SPA's U-SPA-GIT row
// cluster consumes.
//
// DISCIPLINE (project_aiarch_testing_no_bdd / test-authoring constitution §7
// anti-cheat): these drive the WHOLE chain end-to-end against a REAL on-disk git
// store — seed ActivityGit via the C-PA-GIT Record* verbs → real project.Manager →
// real web.Client HTTP handler → JSON — and assert the wire DTO. No mock of the
// projection, no fake ProjectState hand-fed to the DTO. The only seam is a tiny
// cred-binding adapter from the cred-threaded GitStore.ReadProject to the no-cred
// project.ProjectStateAccess port (the composition-root adapter shape, NOT a
// behavioral double). They cover:
//   - the git fields project correctly (branch / opaque PR ref / arch-approved /
//     merged / CR label / isRevert / updatedAt), keyed by ActivityID
//   - the honest-empty case (a project with NO git head-state omits gitRows entirely;
//     an activity with no row is simply absent from the map)
//   - the 3-state CI mapping (Pending→in_progress, Success→success, Failure→failed)
//   - provider-opacity: the row carries the OPAQUE pullRequestRef and NO stored
//     prUrl / prNumber-as-int (the SPA derives both at read — OQ-3 RULED)

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// --- real-store harness ----------------------------------------------------

// gitLocalLocator resolves one project repo (a real on-disk throwaway git repo). A
// plain function-backed RepoLocator — NOT a sibling RA — so the RA's no-sideways
// discipline is preserved (mirrors the projectstate test localLocator). The
// cross-project registry repo is GONE (founder ruling 2026-06-14).
type gitLocalLocator struct {
	project *fwgithub.GitStore
}

func (l gitLocalLocator) ProjectRepo(_ ps.ProjectID) (*fwgithub.GitStore, error) {
	return l.project, nil
}

// credBoundStore adapts the cred-threaded *projectstate.GitStore to the no-cred
// project.ProjectStateAccess port the projectManager consumes. This is exactly the
// composition-root binding (the Manager port hides the credential the git substrate
// needs); in LOCAL profile the credential is the trivially-valid local credential.
// It is a thin parameter-binding adapter, NOT a behavioral double — every call
// reaches the REAL git store.
type credBoundStore struct {
	store *ps.GitStore
	cred  ps.RepoCredential
}

func (a credBoundStore) CreateProject(rc fwra.Context, projectID ps.ProjectID, owner ps.OwnerScope, name string) (ps.Version, error) {
	return a.store.CreateProject(rc.Context, projectID, owner, name, a.cred, rc.IdempotencyKey)
}

func (a credBoundStore) ListProjects(rc fwra.Context, owner ps.OwnerScope) ([]ps.ProjectSummary, error) {
	return a.store.ListProjects(rc.Context, owner, a.cred)
}

func (a credBoundStore) ReadProject(rc fwra.Context, projectID ps.ProjectID) (ps.Project, error) {
	return a.store.ReadProject(rc.Context, projectID, a.cred)
}

var _ project.ProjectStateAccess = credBoundStore{}

// newGitProjectHandler spins a real local git store, seeds a Phase-3 project, runs
// the supplied seed closure (the C-PA-GIT Record* verbs), then wires the REAL
// projectManager + REAL webClient over it. Returns the handler + the projectId.
func newGitProjectHandler(t *testing.T, clk time.Time, repoBase string, seed func(t *testing.T, store *ps.GitStore, ctx context.Context, id ps.ProjectID, v ps.Version, cred ps.RepoCredential)) (h http.Handler, projectID ps.ProjectID, durable func(t *testing.T) ps.Project) {
	t.Helper()
	projRepo := gh.StartLocalGitRepo(t, "main")
	proj, err := fwgithub.NewGitStore(projRepo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore(project): %v", err)
	}
	store, err := ps.NewGitStore(gitLocalLocator{project: proj}, true /* local */)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithClock(func() time.Time { return clk })

	cred := ps.LocalRepoCredential()
	ctx := context.Background()
	id := ps.ProjectID(uuid.NewString())
	v, err := store.CreateProject(ctx, id, "alice", "GitDemo", cred, "wf:create")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if seed != nil {
		seed(t, store, ctx, id, v, cred)
	}

	projectManager := project.NewManager(credBoundStore{store: store, cred: cred}, nil, estimation.New())
	c := NewClient(nil, nil, projectManager, nil, nil, fakeSecurity{permit: true}, repoBase)
	dev := DevConfig{Enabled: true, Principal: security.SecurityPrincipal{
		Kind:    security.PrincipalUser,
		Subject: "test-operator",
	}}
	durable = func(t *testing.T) ps.Project {
		t.Helper()
		p, err := store.ReadProject(ctx, id, cred)
		if err != nil {
			t.Fatalf("durable ReadProject: %v", err)
		}
		return p
	}
	return c.Routes(AuthMiddleware(dev, nil)), id, durable
}

// readProjectState drives the REAL GET head-state route and decodes the wire DTO.
func readProjectState(t *testing.T, h http.Handler, id ps.ProjectID) projectStateResponse {
	t.Helper()
	rec := doJSON(t, h, http.MethodGet, "/api/v1/projects/"+id.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET project status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp projectStateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode projectStateResponse: %v", err)
	}
	return resp
}

// --- tests -----------------------------------------------------------------

// TestGitRows_ProjectFromHeadState — a construction activity with a full git
// lifecycle (branch+PR opened, CI success, arch +1, merged) projects every field
// onto the wire gitRow, keyed by ActivityID. This is the construction-session-view
// git row cluster.
func TestGitRows_ProjectFromHeadState(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	h, id, _ := newGitProjectHandler(t, now, "https://github.com/acme/proj", func(t *testing.T, store *ps.GitStore, ctx context.Context, id ps.ProjectID, v ps.Version, cred ps.RepoCredential) {
		v2, err := store.RecordActivityBranchOpened(ctx, id, v, "C-MST", "activity/C-MST", "ref-cmst", "44", "", false, cred, "wf:branch")
		if err != nil {
			t.Fatalf("RecordActivityBranchOpened: %v", err)
		}
		v3, err := store.RecordActivityCIObserved(ctx, id, v2, "C-MST", ps.CICheckSuccess, cred, "wf:ci")
		if err != nil {
			t.Fatalf("RecordActivityCIObserved: %v", err)
		}
		v4, err := store.RecordActivityArchApproved(ctx, id, v3, "C-MST", cred, "wf:arch")
		if err != nil {
			t.Fatalf("RecordActivityArchApproved: %v", err)
		}
		if _, err := store.RecordActivityMerged(ctx, id, v4, "C-MST", cred, "wf:merge"); err != nil {
			t.Fatalf("RecordActivityMerged: %v", err)
		}
	})

	resp := readProjectState(t, h, id)
	row, ok := resp.GitRows["C-MST"]
	if !ok {
		t.Fatalf("gitRows[C-MST] absent; have %+v", resp.GitRows)
	}
	if row.BranchName != "activity/C-MST" {
		t.Fatalf("branchName = %q, want activity/C-MST", row.BranchName)
	}
	if row.PullRequestRef != "44" {
		t.Fatalf("pullRequestRef = %q, want opaque 44", row.PullRequestRef)
	}
	// The read-time projection derives prNumber from the opaque ref and composes prUrl
	// from <repoBase>/pull/<ref> (D-PA-GIT-PRURL-ruling R1/R2) — construction-at-read.
	if row.PrNumber != 44 {
		t.Fatalf("prNumber = %d, want 44 (derived from opaque ref)", row.PrNumber)
	}
	if row.PrURL != "https://github.com/acme/proj/pull/44" {
		t.Fatalf("prUrl = %q, want https://github.com/acme/proj/pull/44", row.PrURL)
	}
	if row.CIStatus != "success" {
		t.Fatalf("ciStatus = %q, want success", row.CIStatus)
	}
	if !row.ArchitectureApproved {
		t.Fatalf("architectureApproved = false, want true")
	}
	if !row.Merged {
		t.Fatalf("merged = false, want true")
	}
	if row.CRLabel != "" || row.IsRevert {
		t.Fatalf("non-CR non-revert row leaked crLabel/isRevert: %+v", row)
	}
	if row.UpdatedAt != "2026-06-12T10:00:00Z" {
		t.Fatalf("updatedAt = %q, want server-resolved 2026-06-12T10:00:00Z", row.UpdatedAt)
	}
}

// TestGitRows_ProviderOpacity — proves CONSTRUCTION-AT-READ, not storage
// (D-PA-GIT-PRURL-ruling R1/R2 re-targets D-PA-GIT-review Ruling 3):
//
//   - DURABLE side: the STORED aggregate (ps.Project.ActivityGit, what ReadProject
//     returns) carries ZERO provider host and NO prUrl / prNumber — only the opaque
//     PullRequestRef. Asserted against the marshaled durable aggregate so a future
//     accidental host/url leak INTO STORAGE is caught.
//   - WIRE side: the webClient read response legitimately carries prNumber (parsed from
//     the opaque ref) and prUrl (composed <repoBase>/pull/<ref>) as READ-TIME
//     PROJECTIONS — proving the host appears only transiently in the read, composed from
//     the Client-held config, never in durable state. The opaque pullRequestRef remains
//     on the wire (the durable truth; prNumber/prUrl are its projections).
func TestGitRows_ProviderOpacity(t *testing.T) {
	now := time.Date(2026, 6, 12, 11, 0, 0, 0, time.UTC)
	const repoBase = "https://github.com/acme/proj"
	h, id, durable := newGitProjectHandler(t, now, repoBase, func(t *testing.T, store *ps.GitStore, ctx context.Context, id ps.ProjectID, v ps.Version, cred ps.RepoCredential) {
		if _, err := store.RecordActivityBranchOpened(ctx, id, v, "C-CW", "activity/C-CW", "ref-ccw", "42", "", false, cred, "wf:branch"); err != nil {
			t.Fatalf("RecordActivityBranchOpened: %v", err)
		}
	})

	// --- DURABLE: the stored aggregate carries no provider host, no prUrl/prNumber. ----
	stored := durable(t)
	storedJSON, err := json.Marshal(stored.ActivityGit)
	if err != nil {
		t.Fatalf("marshal durable ActivityGit: %v", err)
	}
	for _, leak := range []string{"github.com", "/pull/", "prUrl", "prNumber"} {
		if strings.Contains(string(storedJSON), leak) {
			t.Fatalf("durable git head-state leaked %q (must stay provider-opaque, no stored url/number): %s", leak, storedJSON)
		}
	}
	if g, ok := stored.ActivityGit["C-CW"]; !ok || g.PullRequestRef != "42" {
		t.Fatalf("durable aggregate must keep the opaque PullRequestRef=42; got %+v (ok=%v)", g, ok)
	}

	// --- WIRE: prNumber/prUrl are constructed at read; opaque ref still present. -------
	rec := doJSON(t, h, http.MethodGet, "/api/v1/projects/"+id.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Decode loosely to inspect the exact JSON keys of the C-CW row.
	var loose struct {
		GitRows map[string]map[string]json.RawMessage `json:"gitRows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loose); err != nil {
		t.Fatalf("decode loose: %v", err)
	}
	row, ok := loose.GitRows["C-CW"]
	if !ok {
		t.Fatalf("gitRows[C-CW] absent; body=%s", rec.Body.String())
	}
	if _, present := row["pullRequestRef"]; !present {
		t.Fatalf("opaque pullRequestRef missing from the wire row: %s", rec.Body.String())
	}
	var gotNumber int
	if err := json.Unmarshal(row["prNumber"], &gotNumber); err != nil {
		t.Fatalf("prNumber must be projected at read (parsed from the opaque ref): %v; body=%s", err, rec.Body.String())
	}
	if gotNumber != 42 {
		t.Fatalf("prNumber = %d, want 42 (parsed from opaque ref)", gotNumber)
	}
	var gotURL string
	if err := json.Unmarshal(row["prUrl"], &gotURL); err != nil {
		t.Fatalf("prUrl must be projected at read (<repoBase>/pull/<ref>): %v; body=%s", err, rec.Body.String())
	}
	if gotURL != repoBase+"/pull/42" {
		t.Fatalf("prUrl = %q, want %q (composed at read, not stored)", gotURL, repoBase+"/pull/42")
	}
}

// TestGitRows_HonestEmpty — a project with NO git head-state (no Record* verb ever
// ran) omits gitRows ENTIRELY from the JSON (the omitempty honest-empty convention),
// so the SPA's gitFor(id) returns undefined for every activity rather than reading a
// fabricated empty row.
func TestGitRows_HonestEmpty(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	h, id, _ := newGitProjectHandler(t, now, "https://github.com/acme/proj", nil) // no git seed

	rec := doJSON(t, h, http.MethodGet, "/api/v1/projects/"+id.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &present); err != nil {
		t.Fatalf("decode top-level: %v", err)
	}
	if _, has := present["gitRows"]; has {
		t.Fatalf("gitRows present on a project with no git head-state (want omitted): %s", rec.Body.String())
	}

	// And the typed decode yields a nil/empty map (no fabricated rows).
	resp := readProjectState(t, h, id)
	if len(resp.GitRows) != 0 {
		t.Fatalf("gitRows = %+v, want empty on honest-empty", resp.GitRows)
	}
}

// TestGitRows_CIThreeStateMapping — each of the three CI states maps onto the stable
// ux-mock CiStatus wire string (Pending→in_progress, Success→success, Failure→failed),
// across three activities in ONE project (so the change-request / multi-row view sees
// all three icons). Also exercises a CR-labelled + revert row.
func TestGitRows_CIThreeStateMapping(t *testing.T) {
	now := time.Date(2026, 6, 12, 13, 0, 0, 0, time.UTC)
	h, id, _ := newGitProjectHandler(t, now, "https://github.com/acme/proj", func(t *testing.T, store *ps.GitStore, ctx context.Context, id ps.ProjectID, v ps.Version, cred ps.RepoCredential) {
		// Pending: born, no CI observed yet (CICheck=Pending on birth).
		v, err := store.RecordActivityBranchOpened(ctx, id, v, "A-PENDING", "activity/A-PENDING", "ref-p", "60", "", false, cred, "wf:p")
		if err != nil {
			t.Fatalf("branch A-PENDING: %v", err)
		}
		// Success.
		v, err = store.RecordActivityBranchOpened(ctx, id, v, "A-SUCCESS", "activity/A-SUCCESS", "ref-s", "61", "", false, cred, "wf:s")
		if err != nil {
			t.Fatalf("branch A-SUCCESS: %v", err)
		}
		v, err = store.RecordActivityCIObserved(ctx, id, v, "A-SUCCESS", ps.CICheckSuccess, cred, "wf:s-ci")
		if err != nil {
			t.Fatalf("ci A-SUCCESS: %v", err)
		}
		// Failure — a CR-labelled revert row.
		v, err = store.RecordActivityBranchOpened(ctx, id, v, "CR-018", "activity/cr-018-analytics", "ref-cr", "63", "cr-018", true, cred, "wf:cr")
		if err != nil {
			t.Fatalf("branch CR-018: %v", err)
		}
		if _, err := store.RecordActivityCIObserved(ctx, id, v, "CR-018", ps.CICheckFailure, cred, "wf:cr-ci"); err != nil {
			t.Fatalf("ci CR-018: %v", err)
		}
	})

	resp := readProjectState(t, h, id)
	cases := map[string]string{
		"A-PENDING": "in_progress",
		"A-SUCCESS": "success",
		"CR-018":    "failed",
	}
	for activityID, wantCI := range cases {
		row, ok := resp.GitRows[activityID]
		if !ok {
			t.Fatalf("gitRows[%s] absent; have %+v", activityID, resp.GitRows)
		}
		if row.CIStatus != wantCI {
			t.Fatalf("gitRows[%s].ciStatus = %q, want %q", activityID, row.CIStatus, wantCI)
		}
	}
	// The CR row carries its label + revert flag through the projection.
	cr := resp.GitRows["CR-018"]
	if cr.CRLabel != "cr-018" {
		t.Fatalf("CR row crLabel = %q, want cr-018", cr.CRLabel)
	}
	if !cr.IsRevert {
		t.Fatalf("CR row isRevert = false, want true")
	}
}

// TestGitRows_UnconfiguredRepoBaseOmitsURL — when the construction repo is unconfigured
// (repoBase == ""; the nil-pipeline empty-session state), the projection OMITS prUrl (no
// fabricated host) but STILL derives prNumber from the opaque ref — the prNumber
// derivation does not depend on repoBase (D-PA-GIT-PRURL-ruling §"The exact DTO change").
func TestGitRows_UnconfiguredRepoBaseOmitsURL(t *testing.T) {
	now := time.Date(2026, 6, 12, 14, 0, 0, 0, time.UTC)
	h, id, _ := newGitProjectHandler(t, now, "" /* unconfigured construction repo */, func(t *testing.T, store *ps.GitStore, ctx context.Context, id ps.ProjectID, v ps.Version, cred ps.RepoCredential) {
		if _, err := store.RecordActivityBranchOpened(ctx, id, v, "C-CW", "activity/C-CW", "ref-ccw", "42", "", false, cred, "wf:branch"); err != nil {
			t.Fatalf("RecordActivityBranchOpened: %v", err)
		}
	})

	rec := doJSON(t, h, http.MethodGet, "/api/v1/projects/"+id.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var loose struct {
		GitRows map[string]map[string]json.RawMessage `json:"gitRows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loose); err != nil {
		t.Fatalf("decode loose: %v", err)
	}
	row, ok := loose.GitRows["C-CW"]
	if !ok {
		t.Fatalf("gitRows[C-CW] absent; body=%s", rec.Body.String())
	}
	if _, present := row["prUrl"]; present {
		t.Fatalf("prUrl must be OMITTED when repoBase is unconfigured (no fabricated host): %s", rec.Body.String())
	}
	// prNumber is still derived (does not depend on repoBase).
	var gotNumber int
	if err := json.Unmarshal(row["prNumber"], &gotNumber); err != nil {
		t.Fatalf("prNumber must still project with no repoBase: %v; body=%s", err, rec.Body.String())
	}
	if gotNumber != 42 {
		t.Fatalf("prNumber = %d, want 42", gotNumber)
	}
}
