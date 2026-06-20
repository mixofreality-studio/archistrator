package main

// projectstate_git_adapter_test.go is the I-GIT-DESIGN PROOF: it drives the UC1/UC2
// design-artifact write path through the SAME no-cred projectstate.ProjectStateAccess
// surface the design Managers' Activities consume, bound over a real on-disk LOCAL git
// store (testinfra.StartLocalGitRepo over go-git's file transport — no real GitHub),
// and asserts the head-state lands as REAL GIT COMMITS in the repo.
//
// This exercises the composition-root cred-binding adapter (projectStateGitAdapter +
// localCredentialMinter + gitRepoLocator) — the load-bearing wiring that lets the
// Postgres-era Managers write to git. Each assertion re-reads through ReadProject /
// ListProjects, which CLONE FRESH from the remote, so a passing read proves the JSON is
// committed to the git repo (not merely held in memory).

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	ps "github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
	fwgithub "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github/testinfra"
)

// localProjectStateOverGit spins a real throwaway on-disk git repo (per-project) and
// builds the production composition-root adapter (projectStateGitAdapter with the LOCAL
// credential minter + the LOCAL discover-by-enumeration catalog) over it — the EXACT
// types main.go wires for the LOCAL profile. It returns the no-cred ProjectStateAccess
// the Managers consume. (The cross-project registry repo is GONE — founder ruling
// 2026-06-14; the catalog is discovered by scanning the on-disk project repo.)
func localProjectStateOverGit(t *testing.T) ps.ProjectStateAccess {
	t.Helper()
	projRepo := gh.StartLocalGitRepo(t, "main")

	// One project repo for the whole test (a single project) — the locator returns the
	// same handle for every projectID, which is exactly the LOCAL single-repo profile.
	// The locator hands the URL to the RA's own fwgithub.NewGitStore (gitRepoLocator),
	// so this drives the SAME construction path main.go's LOCAL profile uses.
	locator := gitRepoLocator{
		branch:            "main",
		perProjectRepoURL: func(ps.ProjectID) string { return projRepo.URL },
	}

	store, err := ps.NewGitStore(locator, true /* local */)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(localProjectCatalog{repoURL: projRepo.URL, branch: "main"})
	return &projectStateGitAdapter{store: store, minter: localCredentialMinter{}}
}

// TestProjectStateGitAdapter_UC1ArtifactLandsInGit proves a UC1 system-design artifact
// (the mission statement) created + staged + committed through the no-cred adapter
// surface lands as committed JSON in the per-project git repo, readable back via a fresh
// clone — the founder acceptance #3 write path (design output in the user's repo).
func TestProjectStateGitAdapter_UC1ArtifactLandsInGit(t *testing.T) {
	state := localProjectStateOverGit(t)
	ctx := context.Background()
	id := ps.ProjectID(uuid.NewString())

	// CreateProject — births the aggregate at version 1 (no registry index — the repo's
	// existence + project.json IS the catalog entry, founder ruling 2026-06-14).
	v1, err := state.CreateProject(ctx, id, "alice", "Demo", "wf:create")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("CreateProject version = %d, want 1", v1)
	}

	// Stage the mission typed model (UC1 step 1 — systemDesignManager stages the draft).
	mission := &ps.MissionStatement{Vision: "vision-text", Mission: "mission-text"}
	v2, err := state.StageArtifactForReview(ctx, id, v1, mission, "wf:stage-mission")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}

	// Commit the mission (architect approved at the review gate).
	v3, err := state.CommitArtifact(ctx, id, v2, ps.KindMission, "wf:commit-mission")
	if err != nil {
		t.Fatalf("CommitArtifact: %v", err)
	}

	// Re-read through a FRESH clone — proves the JSON is committed to the git repo.
	proj, err := state.ReadProject(ctx, id)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != v3 {
		t.Fatalf("ReadProject version = %d, want %d", proj.Version, v3)
	}
	if proj.Mission.Status != ps.ReviewCommitted {
		t.Fatalf("mission status = %v, want Committed", proj.Mission.Status)
	}
	got, ok := proj.Mission.Model.(*ps.MissionStatement)
	if !ok || got.Vision != "vision-text" || got.Mission != "mission-text" {
		t.Fatalf("mission model round-trip through git failed: %+v", proj.Mission.Model)
	}

	// The catalog read (ListProjects) surfaces the project by ENUMERATING the on-disk
	// project repo (discover-by-enumeration) — no registry index, the repo IS the row.
	summaries, err := state.ListProjects(ctx, "alice")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ProjectID != id || summaries[0].Name != "Demo" {
		t.Fatalf("ListProjects = %+v, want one Demo row", summaries)
	}
}

// TestProjectStateGitAdapter_UC2AdvanceAndResearchLandsInGit proves the UC2 path also
// threads through: SetResearchInput (the Method input) and AdvancePhase (the seal) both
// land as git commits and are visible on a fresh read.
func TestProjectStateGitAdapter_UC2AdvanceAndResearchLandsInGit(t *testing.T) {
	state := localProjectStateOverGit(t)
	ctx := context.Background()
	id := ps.ProjectID(uuid.NewString())

	v1, err := state.CreateProject(ctx, id, "bob", "Proj2", "wf:create")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	research := ps.ResearchInput{Sources: []ps.ResearchSource{{Title: "src", Content: "research-corpus"}}}
	v2, err := state.SetResearchInput(ctx, id, v1, research, "wf:research")
	if err != nil {
		t.Fatalf("SetResearchInput: %v", err)
	}

	v3, err := state.AdvancePhase(ctx, id, v2, "wf:advance")
	if err != nil {
		t.Fatalf("AdvancePhase: %v", err)
	}

	proj, err := state.ReadProject(ctx, id)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != v3 {
		t.Fatalf("version = %d, want %d", proj.Version, v3)
	}
	if proj.Phase == ps.PhaseSystemDesign {
		t.Fatalf("phase did not advance past SystemDesign: %v", proj.Phase)
	}
	if len(proj.ResearchInput.Sources) != 1 || proj.ResearchInput.Sources[0].Content != "research-corpus" {
		t.Fatalf("research input did not round-trip through git: %+v", proj.ResearchInput)
	}
}

// ---------------------------------------------------------------------------
// C-PA-AD — NAME-AS-IDENTITY (the adopted repo name IS the project identity).
// ---------------------------------------------------------------------------

// TestCloudPerProjectRepoURL_NameAsIdentity is the sub-task-2 proof: the CLOUD
// per-project repo URL resolves to <webHost>/<account>/<name>.git — the project
// identity verbatim — and the dropped "aiarch-<id>" prefix never appears. This is the
// locator URL the per-project credential's repo scope must AGREE with (the credential
// minter re-derives the same repo name via the now-identity deterministicRepoName).
func TestCloudPerProjectRepoURL_NameAsIdentity(t *testing.T) {
	const (
		webHost = "https://github.com"
		account = "davidmarne"
		name    = "my-cool-system" // a USER-supplied repo name == the project identity
	)
	got := cloudPerProjectRepoURL(webHost, account, name)
	want := "https://github.com/davidmarne/my-cool-system.git"
	if got != want {
		t.Fatalf("cloudPerProjectRepoURL = %q, want %q", got, want)
	}
	if strings.Contains(got, "aiarch-") {
		t.Fatalf("per-project repo URL %q still carries the dropped aiarch- prefix", got)
	}
	if !strings.Contains(got, "/"+account+"/"+name+".git") {
		t.Fatalf("per-project repo URL %q is not <account>/<name>.git", got)
	}
}

// TestProjectStateGitAdapter_CreateReadList_IdentityVerbatim is the sub-task-1/3/4
// proof at the layer projectStateAccess controls: createProject persists the project
// identity VERBATIM as the .aiarch/state/project.json `id` (no "aiarch-" rewriting),
// ReadProject returns it whole, ListProjects (discover-by-enumeration) surfaces it as a
// project whose identity == the stored id, and the expectedVersion + idempotencyKey
// write discipline is intact across the round-trip.
//
// NAME-AS-IDENTITY (C-PM-Δ, LIVE 2026-06-15): projectstate.ProjectID is now a string
// DEFINED type (no longer a uuid.UUID alias), so this test passes a literal user-name
// STRING ("my-cool-system") as the identity verbatim — exactly the repo name C-PM-Δ
// threads through adopt → seat → createProject. The projectstate RA was ALREADY
// name-as-identity-CLEAN: it stores whatever identity it is handed verbatim and never
// re-encodes it with an "aiarch-" prefix, so the round-trip holds with the literal name.
// The on-disk `id` here is that identity string, persisted unrewritten.
func TestProjectStateGitAdapter_CreateReadList_IdentityVerbatim(t *testing.T) {
	projRepo := gh.StartLocalGitRepo(t, "main")
	locator := gitRepoLocator{
		branch:            "main",
		perProjectRepoURL: func(ps.ProjectID) string { return projRepo.URL },
	}
	store, err := ps.NewGitStore(locator, true /* local */)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(localProjectCatalog{repoURL: projRepo.URL, branch: "main"})
	state := &projectStateGitAdapter{store: store, minter: localCredentialMinter{}}
	ctx := context.Background()

	id := ps.ProjectID("my-cool-system") // a USER-supplied repo name == the project identity
	identity := id.String()              // the verbatim identity string that must persist unrewritten

	// createProject — expectedVersion discipline: births at version 1.
	v1, err := state.CreateProject(ctx, id, "alice", "My Cool System", "wf:create")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("CreateProject version = %d, want 1", v1)
	}

	// ReadProject — the identity round-trips whole.
	proj, err := state.ReadProject(ctx, id)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.ID != id || proj.Name != "My Cool System" {
		t.Fatalf("ReadProject identity/name mismatch: id=%s name=%q", proj.ID, proj.Name)
	}

	// The persisted `id` in .aiarch/state/project.json is the identity VERBATIM — no
	// "aiarch-" prefix is ever applied by the createProject path. Read the raw committed
	// JSON through a fresh clone to assert the on-disk shape.
	gs, err := fwgithub.NewGitStore(projRepo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore(raw): %v", err)
	}
	snap, err := gs.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("ReadSubtree: %v", err)
	}
	raw, ok := snap.Files["project.json"]
	if !ok {
		t.Fatal("project.json not committed to the repo")
	}
	if !strings.Contains(string(raw), "\"id\": \""+identity+"\"") {
		t.Fatalf("project.json `id` is not the identity verbatim: %s", string(raw))
	}
	if strings.Contains(string(raw), "aiarch-"+identity) {
		t.Fatalf("project.json `id` carries the dropped aiarch- prefix: %s", string(raw))
	}

	// ListProjects (discover-by-enumeration) surfaces the project keyed by the SAME
	// identity — the repo IS the catalog row, name-as-identity end to end.
	summaries, err := state.ListProjects(ctx, "alice")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ProjectID != id {
		t.Fatalf("ListProjects = %+v, want one row with identity %s", summaries, identity)
	}

	// idempotencyKey discipline: a retried createProject with the SAME key is a no-op
	// that returns the prior version (no double-create, no Conflict).
	vDup, err := state.CreateProject(ctx, id, "alice", "My Cool System", "wf:create")
	if err != nil {
		t.Fatalf("CreateProject retry (same key) should dedup, got: %v", err)
	}
	if vDup != v1 {
		t.Fatalf("CreateProject retry returned version %d, want the prior %d (dedup)", vDup, v1)
	}
}
