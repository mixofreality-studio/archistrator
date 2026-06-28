package main

// ira_delta_test.go is the I-RA-Δ INTEGRATION PROOF: it re-proves the adopt → seat →
// create (and RESUME) path end-to-end after the 2026-06-16 permissive-resume adopt
// ruling, at the wiring level the test constitution requires — a REAL projectManager
// driving a REAL sourcecontrol.Access (over the EXTERNAL FakeGitHub REST seam) and a
// REAL projectStateGitAdapter (over an EXTERNAL on-disk git repo). NO internal
// component is faked: only the two external seams (GitHub REST + on-disk git) are.
//
// The production composition-root adapters are used verbatim: sourceControlAdapter
// (the Manager port ↔ concrete RA bridge) and projectStateGitAdapter (the cred-binding
// head-state bridge). So this exercises the SAME wiring main.go assembles.
//
// What it proves (the dispatch's I-RA-Δ deliverable):
//   - Adopt SUCCESS regardless of repo content (README/claude.yml present) + topic applied.
//   - NotUnderInstallation still errors (the App must be installed) → no create.
//   - Fresh create: empty-of-.aiarch repo → adopt → commitAgenticWorkflowFile →
//     createProject → project created with the repo NAME as identity; workflow committed.
//   - Resume: a repo that already has a committed .aiarch/state/project.json → CreateProject
//     RESUMES that project (returns existing state/version, does NOT clobber).
//   - Call order: adopt before createProject; commitAgenticWorkflowFile idempotent.

import (
	"context"
	"strings"
	"testing"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

const iraDeltaAccount = "acme"

// iraRC wraps a request context in the Manager-layer call Context the projectManager
// ops lead with (zero principal stopgap in these composition-root tests).
func iraRC(ctx context.Context) fwm.Context { return fwm.Context{Context: ctx} }

// iraDeltaHarness bundles the wired-together pieces of one I-RA-Δ scenario.
type iraDeltaHarness struct {
	mgr     *project.Manager
	fakeGH  *gh.FakeGitHub
	gitRepo *fwgithub.GitStore // the per-project on-disk git repo (projectstate seam)
	ctx     context.Context
}

// newIRADeltaHarness wires a REAL projectManager over a REAL sourcecontrol.Access
// (against FakeGitHub) and a REAL projectStateGitAdapter (against one on-disk git
// repo), via the PRODUCTION composition-root adapters. The on-disk repo is shared by
// the per-project locator (single-project scenarios), exactly like the LOCAL profile.
func newIRADeltaHarness(t *testing.T) *iraDeltaHarness {
	t.Helper()

	// --- EXTERNAL seam 1: FakeGitHub (sourceControlAccess REST). ---
	fake := gh.Start()
	t.Cleanup(fake.Close)
	keyPEM, err := gh.GenerateAppKeyPEM()
	if err != nil {
		t.Fatalf("generate app key: %v", err)
	}
	ghClient, err := fwgithub.NewAppClient("12345", keyPEM, fake.BaseURL())
	if err != nil {
		t.Fatalf("NewAppClient: %v", err)
	}
	scAccess, err := sourcecontrol.New(ghClient, iraDeltaAccount, "aiarch-app", true)
	if err != nil {
		t.Fatalf("sourcecontrol.New: %v", err)
	}
	scAdapter := sourceControlAdapter{inner: scAccess}

	// --- EXTERNAL seam 2: a real on-disk git repo (projectStateAccess git substrate). ---
	projRepo := gh.StartLocalGitRepo(t, "main")
	rawRepo, err := fwgithub.NewGitStore(projRepo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore(project): %v", err)
	}
	locator := gitRepoLocator{
		branch:            "main",
		perProjectRepoURL: func(ps.ProjectID) string { return projRepo.URL },
	}
	store, err := ps.NewGitStore(locator, true /* local */)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(localProjectCatalog{repoURL: projRepo.URL, branch: "main"})
	stateAdapter := &projectStateGitAdapter{store: store, minter: localCredentialMinter{}}

	// --- REAL Manager over both real RAs. nil estimator: this harness exercises
	// project birth (CreateProject), not the GetProject compute-at-read path. ---
	mgr := project.NewManager(stateAdapter, scAdapter, nil, "")

	return &iraDeltaHarness{mgr: mgr, fakeGH: fake, gitRepo: rawRepo, ctx: context.Background()}
}

// seedInstallationFor scripts the App-installation discovery + token mint so the
// FakeGitHub resolves the account to an installation.
func seedInstallationFor(fake *gh.FakeGitHub, account string) {
	fake.On("GET", "/app/installations", gh.JSON(200, []map[string]any{
		{"id": 99, "account": map[string]any{"login": account}},
	}))
	fake.On("POST", "/app/installations/99/access_tokens", gh.JSON(201, map[string]any{
		"token":      "ghs_faketoken",
		"expires_at": "2999-01-01T00:00:00Z",
	}))
}

// TestIRADelta_AdoptSucceedsWithPreExistingContent_ThenCreates proves the permissive
// adopt over a repo that ALREADY has content (a README/claude.yml), end-to-end through
// the real Manager: adopt SUCCEEDS (topic applied), the workflow file is committed, and
// the project is created with the repo name as identity.
func TestIRADelta_AdoptSucceedsWithPreExistingContent_ThenCreates(t *testing.T) {
	h := newIRADeltaHarness(t)
	h.fakeGH.EnableRepoCatalog()
	seedInstallationFor(h.fakeGH, iraDeltaAccount)
	// The user's repo already has content (a README + a claude.yml from /install-github-app),
	// modeled as a non-empty repo with branches + a foreign topic. Permissive adopt is fine.
	h.fakeGH.SeedRepo(iraDeltaAccount, "my-system", "Pre-existing repo", []string{"misc"}, true)
	h.fakeGH.SeedRepoFile(iraDeltaAccount, "my-system", "README.md", []byte("# hello"))
	h.fakeGH.SeedRepoFile(iraDeltaAccount, "my-system", ".github/workflows/claude.yml", []byte("name: claude"))

	id, err := h.mgr.CreateProject(iraRC(h.ctx), project.OwnerScope("alice@example.com"), "my-system")
	if err != nil {
		t.Fatalf("CreateProject over a non-empty repo must SUCCEED (permissive adopt), got: %v", err)
	}
	if id != project.ProjectID("my-system") {
		t.Fatalf("project id = %q, want name-as-identity my-system", id)
	}

	// Adopt applied the aiarch-project topic on the wire (regardless of content).
	topicsReq := findIRARequest(t, h.fakeGH, "PUT", "/repos/acme/my-system/topics")
	if !strings.Contains(topicsReq.Body, "aiarch-project") {
		t.Fatalf("adopt should apply the aiarch-project topic; got %q", topicsReq.Body)
	}
	// adopt must NOT create a repo (the user supplied it).
	if countIRARequests(h.fakeGH, "POST", "/orgs/acme/repos") != 0 {
		t.Fatalf("adopt must not CREATE a repo")
	}
	// The agentic-design workflow file was committed to the user's repo (Contents PUT).
	if _, ok := h.fakeGH.RepoFile(iraDeltaAccount, "my-system", sourcecontrol.DesignWorkflowPath); !ok {
		t.Fatalf("the agentic-design workflow file was not committed to %s", sourcecontrol.DesignWorkflowPath)
	}
	// The project head-state was created in the on-disk git repo (the createProject seam).
	assertProjectStateCommitted(t, h, "my-system", "my-system")
}

// TestIRADelta_NotUnderInstallation_DoesNotCreate proves the one surviving adopt error:
// a repo NOT reachable under the App installation → NotUnderInstallation, and the
// project is NEVER created (adopt gates create).
func TestIRADelta_NotUnderInstallation_DoesNotCreate(t *testing.T) {
	h := newIRADeltaHarness(t)
	h.fakeGH.EnableRepoCatalog()
	seedInstallationFor(h.fakeGH, iraDeltaAccount)
	// The repo is NOT seeded → GET /repos/acme/ghost 404s under the installation.

	_, err := h.mgr.CreateProject(iraRC(h.ctx), project.OwnerScope("alice@example.com"), "ghost")
	if err == nil {
		t.Fatal("CreateProject must FAIL when the repo is not under the installation")
	}
	// No topic mutation, no create.
	if countIRARequests(h.fakeGH, "PUT", "/repos/acme/ghost/topics") != 0 {
		t.Fatalf("a not-under-installation adopt must not mutate topics")
	}
	if _, ok := h.fakeGH.RepoFile(iraDeltaAccount, "ghost", sourcecontrol.DesignWorkflowPath); ok {
		t.Fatalf("no workflow file should be committed when adopt fails")
	}
	// The on-disk project repo carries NO committed state.
	snap, err := h.gitRepo.ReadSubtree(h.ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("ReadSubtree: %v", err)
	}
	if _, ok := snap.Files["project.json"]; ok {
		t.Fatalf("project.json must NOT be committed when adopt fails (no orphan project)")
	}
}

// TestIRADelta_FreshCreate_EmptyRepo proves the clean fresh path: an empty repo (no
// .aiarch/) → adopt → commitAgenticWorkflowFile → createProject → a project created at
// version 1 with the repo name as identity, the workflow file committed.
func TestIRADelta_FreshCreate_EmptyRepo(t *testing.T) {
	h := newIRADeltaHarness(t)
	h.fakeGH.EnableRepoCatalog()
	seedInstallationFor(h.fakeGH, iraDeltaAccount)
	h.fakeGH.SeedEmptyRepo(iraDeltaAccount, "fresh-svc", true)

	id, err := h.mgr.CreateProject(iraRC(h.ctx), project.OwnerScope("bob@example.com"), "fresh-svc")
	if err != nil {
		t.Fatalf("CreateProject (fresh empty repo): %v", err)
	}
	if id != project.ProjectID("fresh-svc") {
		t.Fatalf("project id = %q, want fresh-svc", id)
	}

	// adopt → topic; seat → workflow committed.
	if !strings.Contains(findIRARequest(t, h.fakeGH, "PUT", "/repos/acme/fresh-svc/topics").Body, "aiarch-project") {
		t.Fatalf("adopt should apply the aiarch-project topic")
	}
	wf, ok := h.fakeGH.RepoFile(iraDeltaAccount, "fresh-svc", sourcecontrol.DesignWorkflowPath)
	if !ok {
		t.Fatalf("the design workflow file was not committed")
	}
	if len(wf) == 0 {
		t.Fatalf("the committed design workflow file is empty")
	}
	// createProject — the project is born at version 1 with name-as-identity.
	st, err := h.mgr.GetProject(iraRC(h.ctx), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if st.ProjectID != id || st.Version != 1 || st.Phase != project.PhaseSystemDesign {
		t.Fatalf("fresh project = id=%s v=%d phase=%v, want fresh-svc/1/SystemDesign", st.ProjectID, st.Version, st.Phase)
	}
	assertProjectStateCommitted(t, h, "fresh-svc", "fresh-svc")
}

// TestIRADelta_ResumeFromExistingAiarchState proves the RESUME behavior end-to-end:
// when the per-project repo ALREADY carries a committed .aiarch/state/project.json (a
// prior run's progress), CreateProject RESUMES — returning the existing project's
// state/phase/version, NOT clobbering it and NOT erroring on already-exists. The adopt
// (now permissive) succeeds over the repo-with-.aiarch as well.
func TestIRADelta_ResumeFromExistingAiarchState(t *testing.T) {
	h := newIRADeltaHarness(t)
	h.fakeGH.EnableRepoCatalog()
	seedInstallationFor(h.fakeGH, iraDeltaAccount)
	// The GitHub-side repo already carries content (a prior .aiarch/ tree) — permissive
	// adopt is fine.
	h.fakeGH.SeedRepo(iraDeltaAccount, "resumed-svc", "Resumed", []string{"aiarch-project"}, true)

	// Simulate the prior run: commit a project.json with real progress DIRECTLY to the
	// on-disk per-project repo (the prior run's seam — bypassing every write verb), the
	// way C-PA-RB's external-draft test seeds committed state.
	prior := ps.Project{
		ID:      ps.ProjectID("resumed-svc"),
		Version: 4,
		Phase:   ps.PhaseProjectDesign,
		Owner:   "carol@example.com",
		Name:    "Resumed Service",
		Mission: ps.ArtifactSlot{
			Status: ps.ReviewCommitted,
			Model:  &ps.MissionStatement{Vision: "prior-vision", Mission: "prior-mission"},
		},
	}
	raw, err := ps.EncodeProjectJSON(prior)
	if err != nil {
		t.Fatalf("EncodeProjectJSON (prior progress): %v", err)
	}
	snap, err := h.gitRepo.ReadSubtree(h.ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("ReadSubtree (observe base): %v", err)
	}
	if _, err := h.gitRepo.CommitSubtree(h.ctx, ".aiarch/state",
		map[string][]byte{"project.json": raw}, snap.Base,
		"prior run: commit progress", fwgithub.GitAuth{Local: true}); err != nil {
		t.Fatalf("CommitSubtree (simulate prior progress): %v", err)
	}

	// CreateProject against the repo with prior state → RESUME (no error, no clobber).
	id, err := h.mgr.CreateProject(iraRC(h.ctx), project.OwnerScope("carol@example.com"), "resumed-svc")
	if err != nil {
		t.Fatalf("CreateProject (resume) must NOT error on an existing .aiarch/ state, got: %v", err)
	}
	if id != project.ProjectID("resumed-svc") {
		t.Fatalf("resume project id = %q, want resumed-svc", id)
	}

	// The returned project reflects CURRENT PROGRESS — the prior version/phase/slot SURVIVE.
	st, err := h.mgr.GetProject(iraRC(h.ctx), id)
	if err != nil {
		t.Fatalf("GetProject (resumed): %v", err)
	}
	if st.Version != 4 || st.Phase != project.PhaseProjectDesign || st.Name != "Resumed Service" {
		t.Fatalf("resume clobbered/reset state: got v%d/%v/%q, want v4/ProjectDesign/Resumed Service",
			st.Version, st.Phase, st.Name)
	}
	// The committed Mission slot from the prior run survives the resume.
	var missionStage project.ArtifactStage = -1
	for _, slot := range st.Slots {
		if slot.Kind == ps.KindMission.WireName() {
			missionStage = slot.Stage
		}
	}
	if missionStage != project.StageCommitted {
		t.Fatalf("resume lost the prior committed Mission slot: stage = %v, want StageCommitted", missionStage)
	}

	// Adopt was still permissive (topic re-applied) over the with-content repo.
	if !strings.Contains(findIRARequest(t, h.fakeGH, "PUT", "/repos/acme/resumed-svc/topics").Body, "aiarch-project") {
		t.Fatalf("resume adopt should (re-)apply the aiarch-project topic")
	}
}

// TestIRADelta_WorkflowFileIdempotent proves commitAgenticWorkflowFile is idempotent:
// running CreateProject twice against the same fresh repo seats the workflow file the
// first time and is a byte-identical no-op the second (no second Contents PUT), and the
// project resumes on the second create rather than erroring.
func TestIRADelta_WorkflowFileIdempotent(t *testing.T) {
	h := newIRADeltaHarness(t)
	h.fakeGH.EnableRepoCatalog()
	seedInstallationFor(h.fakeGH, iraDeltaAccount)
	h.fakeGH.SeedEmptyRepo(iraDeltaAccount, "idem-svc", true)

	if _, err := h.mgr.CreateProject(iraRC(h.ctx), project.OwnerScope("alice"), "idem-svc"); err != nil {
		t.Fatalf("CreateProject (first): %v", err)
	}
	putsAfterFirst := countIRARequests(h.fakeGH, "PUT", "/repos/acme/idem-svc/contents/"+sourcecontrol.DesignWorkflowPath)
	if putsAfterFirst != 1 {
		t.Fatalf("first create should PUT the workflow file exactly once, got %d", putsAfterFirst)
	}

	// Second create against the SAME repo: the on-disk state already exists → RESUME;
	// the workflow file is byte-identical → no second Contents PUT.
	if _, err := h.mgr.CreateProject(iraRC(h.ctx), project.OwnerScope("alice"), "idem-svc"); err != nil {
		t.Fatalf("CreateProject (second, resume) must not error, got: %v", err)
	}
	putsAfterSecond := countIRARequests(h.fakeGH, "PUT", "/repos/acme/idem-svc/contents/"+sourcecontrol.DesignWorkflowPath)
	if putsAfterSecond != 1 {
		t.Fatalf("re-seating a byte-identical workflow file must be a no-op (no second PUT); got %d total PUTs", putsAfterSecond)
	}
}

// --- helpers ---------------------------------------------------------------

// assertProjectStateCommitted reads the per-project on-disk repo through a fresh clone
// and asserts a project.json was committed carrying the expected identity verbatim.
func assertProjectStateCommitted(t *testing.T, h *iraDeltaHarness, repoName, wantID string) {
	t.Helper()
	snap, err := h.gitRepo.ReadSubtree(h.ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("ReadSubtree: %v", err)
	}
	raw, ok := snap.Files["project.json"]
	if !ok {
		t.Fatalf("project.json not committed to the on-disk repo for %s", repoName)
	}
	if !strings.Contains(string(raw), "\"id\": \""+wantID+"\"") {
		t.Fatalf("committed project.json id is not %q verbatim: %s", wantID, string(raw))
	}
	if strings.Contains(string(raw), "aiarch-"+wantID) {
		t.Fatalf("committed project.json id carries the dropped aiarch- prefix: %s", string(raw))
	}
}

func findIRARequest(t *testing.T, fake *gh.FakeGitHub, method, path string) gh.RecordedRequest {
	t.Helper()
	for _, r := range fake.Requests() {
		if r.Method == method && r.Path == path {
			return r
		}
	}
	t.Fatalf("expected a %s %s request; none found", method, path)
	return gh.RecordedRequest{}
}

func countIRARequests(fake *gh.FakeGitHub, method, path string) int {
	n := 0
	for _, r := range fake.Requests() {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}
