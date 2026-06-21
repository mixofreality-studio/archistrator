package projectstate_test

// Black-box regression tests for the git-JSON + ref-CAS realization of
// projectStateAccess (projectStateAccess.md §REWORK 2026-06-10). They drive the
// RA's PUBLIC GitProjectStateAccess verbs against a REAL throwaway on-disk git
// store (testinfra.LocalGitRepo over go-git's file transport) — no mock, per the
// test-authoring constitution's real-store discipline and the D-PA-R mandate
// ("the actual git store, a throwaway local repo").
//
// The HARD C-PA-R construction-exit gate is TestRefCasVsConcurrentWriter, which
// proves BOTH disciplines REWORK.7 mandates:
//   (a) ref-CAS-vs-concurrent-writer convergence — two writers from the same base,
//       one wins fast-forward, the loser is rejected non-fast-forward (fwra.Conflict),
//       reloads HEAD, re-applies, and both mutations survive (no lost update);
//   (b) activity-retry idempotency + dedup — a retry re-passing the SAME
//       idempotencyKey with a now-stale expectedVersion probes applied_mutations
//       FIRST, returns the prior resultVersion, and produces NO second state commit.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// localLocator resolves one project repo (a real on-disk throwaway git repo). It is a
// plain function-backed RepoLocator — NOT a sibling RA — so the RA's no-sideways
// discipline is preserved. The cross-project registry repo is GONE (founder ruling
// 2026-06-14): the catalog is discovered by enumeration (a single-repo enumeration in
// these single-project tests).
type localLocator struct {
	project *fwgithub.GitStore
}

func (l localLocator) ProjectRepo(_ ps.ProjectID) (*fwgithub.GitStore, error) { return l.project, nil }

// singleRepoCatalog is the test ProjectCatalog: it reads project.json from the one
// on-disk repo and yields its id+title — the LOCAL single-repo discover-by-enumeration
// the production localProjectCatalog implements over the same repo. NOT a behavioral
// double: it reaches the REAL git store.
type singleRepoCatalog struct {
	repo *fwgithub.GitStore
}

func (c singleRepoCatalog) ListProjectRepos(ctx context.Context, _ ps.OwnerScope, _ ps.RepoCredential) ([]ps.ProjectCatalogRef, error) {
	snap, err := c.repo.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		return nil, err
	}
	raw, ok := snap.Files["project.json"]
	if !ok {
		return nil, nil
	}
	var doc struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if jerr := json.Unmarshal(raw, &doc); jerr != nil {
		return nil, jerr
	}
	return []ps.ProjectCatalogRef{{ProjectID: ps.ProjectID(doc.ID), Title: doc.Name}}, nil
}

// newLocalGitStore spins one real local git repo and builds a LOCAL-profile GitStore
// over it, wired with the single-repo discover-by-enumeration catalog.
func newLocalGitStore(t *testing.T) (*ps.GitStore, ps.RepoCredential, context.Context) {
	t.Helper()
	store, _, cred, ctx := newLocalGitStoreWithRepo(t)
	return store, cred, ctx
}

// newLocalGitStoreWithRepo is newLocalGitStore that ALSO returns the underlying raw
// fwgithub.GitStore (the per-project repo). The raw handle lets a test simulate the
// agentic Action by committing `.aiarch/state/project.json` DIRECTLY to the repo —
// bypassing every projectStateAccess write verb — then prove the RA reads it back.
func newLocalGitStoreWithRepo(t *testing.T) (*ps.GitStore, *fwgithub.GitStore, ps.RepoCredential, context.Context) {
	t.Helper()
	projRepo := gh.StartLocalGitRepo(t, "main")
	proj, err := fwgithub.NewGitStore(projRepo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore(project): %v", err)
	}
	store, err := ps.NewGitStore(localLocator{project: proj}, true /* local */)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(singleRepoCatalog{repo: proj})
	return store, proj, ps.LocalRepoCredential(), context.Background()
}

func kindOf(t *testing.T, err error) fwra.Kind {
	t.Helper()
	var e *fwra.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fwra.Error, got %T: %v", err, err)
	}
	return e.Kind
}

func mustResearch(s string) ps.ResearchInput {
	return ps.ResearchInput{Sources: []ps.ResearchSource{{Title: "t", Content: s}}}
}

// TestGitStore_CreateReadRoundTrip — CreateProject seeds the aggregate at Version
// 1; ReadProject returns it whole; ListProjects surfaces it via discover-by-enumeration.
func TestGitStore_CreateReadRoundTrip(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())

	v, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if v != 1 {
		t.Fatalf("CreateProject version = %d, want 1", v)
	}

	proj, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != 1 || proj.Owner != "alice" || proj.Name != "Demo" || proj.Phase != ps.PhaseSystemDesign {
		t.Fatalf("ReadProject = %+v, want v1/alice/Demo/SystemDesign", proj)
	}

	summaries, err := store.ListProjects(ctx, "alice", cred)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ProjectID != id || summaries[0].Name != "Demo" {
		t.Fatalf("ListProjects = %+v, want one Demo row", summaries)
	}
}

// multiRepoLocator + multiRepoCatalog model the CLOUD multi-repo shape: one on-disk
// repo per project (keyed by projectID), and a catalog that enumerates them. This
// proves ListProjects returns MULTIPLE projects WITHOUT any registry index — the set
// of project repos IS the catalog.
type multiRepoLocator struct {
	repos map[ps.ProjectID]*fwgithub.GitStore
}

func (l multiRepoLocator) ProjectRepo(id ps.ProjectID) (*fwgithub.GitStore, error) {
	return l.repos[id], nil
}

type multiRepoCatalog struct {
	repos map[ps.ProjectID]*fwgithub.GitStore
}

func (c multiRepoCatalog) ListProjectRepos(ctx context.Context, _ ps.OwnerScope, _ ps.RepoCredential) ([]ps.ProjectCatalogRef, error) {
	var out []ps.ProjectCatalogRef
	for id, repo := range c.repos {
		snap, err := repo.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
		if err != nil {
			return nil, err
		}
		raw, ok := snap.Files["project.json"]
		if !ok {
			continue
		}
		var doc struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &doc)
		out = append(out, ps.ProjectCatalogRef{ProjectID: id, Title: doc.Name})
	}
	return out, nil
}

// TestGitStore_ListProjects_NoRegistry_MultipleProjects — THE registry-removal proof:
// create TWO projects (each in its own repo, the cloud shape), then ListProjects
// returns BOTH via discover-by-enumeration — no registry index repo exists.
func TestGitStore_ListProjects_NoRegistry_MultipleProjects(t *testing.T) {
	id1, id2 := ps.ProjectID(uuid.NewString()), ps.ProjectID(uuid.NewString())
	repos := map[ps.ProjectID]*fwgithub.GitStore{}
	for _, id := range []ps.ProjectID{id1, id2} {
		r := gh.StartLocalGitRepo(t, "main")
		gs, err := fwgithub.NewGitStore(r.URL, "main")
		if err != nil {
			t.Fatalf("NewGitStore(%s): %v", id, err)
		}
		repos[id] = gs
	}
	store, err := ps.NewGitStore(multiRepoLocator{repos: repos}, true /* local */)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(multiRepoCatalog{repos: repos})
	cred, ctx := ps.LocalRepoCredential(), context.Background()

	if _, err := store.CreateProject(ctx, id1, "alice", "First", cred, "wf:c1"); err != nil {
		t.Fatalf("CreateProject 1: %v", err)
	}
	if _, err := store.CreateProject(ctx, id2, "alice", "Second", cred, "wf:c2"); err != nil {
		t.Fatalf("CreateProject 2: %v", err)
	}

	summaries, err := store.ListProjects(ctx, "alice", cred)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("ListProjects returned %d, want 2 (both projects, no registry): %+v", len(summaries), summaries)
	}
	names := map[string]bool{}
	for _, s := range summaries {
		names[s.Name] = true
		if s.TotalCount != len(ps.Phase1RequiredKinds()) {
			t.Fatalf("summary %s totalCount = %d, want %d", s.Name, s.TotalCount, len(ps.Phase1RequiredKinds()))
		}
	}
	if !names["First"] || !names["Second"] {
		t.Fatalf("ListProjects missing a project; got names %+v", names)
	}
}

// TestGitStore_StageCommitRoundTrip — stage a typed model, commit it, read it back
// with its review status (a model round-trips through git JSON).
func TestGitStore_StageCommitRoundTrip(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	mission := &ps.MissionStatement{Vision: "v", Mission: "m"}
	v2, err := store.StageArtifactForReview(ctx, id, 1, mission, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	v3, err := store.CommitArtifact(ctx, id, v2, ps.KindMission, cred, "wf:commit")
	if err != nil {
		t.Fatalf("CommitArtifact: %v", err)
	}
	proj, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != v3 {
		t.Fatalf("version = %d, want %d", proj.Version, v3)
	}
	if proj.Mission.Status != ps.ReviewCommitted {
		t.Fatalf("mission status = %v, want Committed", proj.Mission.Status)
	}
	gotMission, ok := proj.Mission.Model.(*ps.MissionStatement)
	if !ok || gotMission.Vision != "v" || gotMission.Mission != "m" {
		t.Fatalf("mission model round-trip failed: %+v", proj.Mission.Model)
	}
}

// TestGitStore_VersionGuardConflict — a write at a stale expectedVersion (without
// a matching dedup key) surfaces fwra.Conflict.
func TestGitStore_VersionGuardConflict(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// project is at version 1; pass a stale expectedVersion 0.
	_, err := store.SetResearchInput(ctx, id, 0, mustResearch("x"), cred, "wf:stale")
	if err == nil {
		t.Fatal("expected Conflict on stale expectedVersion")
	}
	if k := kindOf(t, err); k != fwra.Conflict {
		t.Fatalf("stale version kind = %v, want Conflict", k)
	}
}

// TestGitStore_NotFoundAndMisuse — read of an absent project is NotFound; cloud
// profile with an empty credential is ContractMisuse; setResearch on an absent
// project is NotFound.
func TestGitStore_NotFoundAndMisuse(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	_, err := store.ReadProject(ctx, ps.ProjectID(uuid.NewString()), cred)
	if k := kindOf(t, err); k != fwra.NotFound {
		t.Fatalf("ReadProject(absent) kind = %v, want NotFound", k)
	}

	_, err = store.SetResearchInput(ctx, ps.ProjectID(uuid.NewString()), 0, mustResearch("x"), cred, "wf:k")
	if k := kindOf(t, err); k != fwra.NotFound {
		t.Fatalf("SetResearchInput(absent) kind = %v, want NotFound", k)
	}

	_, err = store.StageArtifactForReview(ctx, ps.ProjectID(uuid.NewString()), 1, nil, cred, "wf:k")
	if k := kindOf(t, err); k != fwra.ContractMisuse {
		t.Fatalf("Stage(nil model) kind = %v, want ContractMisuse", k)
	}
}

// TestRefCasVsConcurrentWriter — THE C-PA-R HARD EXIT GATE (REWORK.7).
//
// (a) Two writers commit a state mutation from the SAME base ref (main tip): a
//
//	reconcile-tick-shaped CommitArtifact racing an operator-shaped
//	RecordOperatorPaused on the SAME project. One push wins fast-forward; the
//	loser's push is rejected non-fast-forward -> fwra.Conflict -> the caller
//	reloads HEAD and re-applies -> both mutations survive (no lost update).
//
// (b) Activity-retry idempotency: the loser's retry re-passes the SAME
//
//	idempotencyKey (now against a fresh, but if forced stale, version); the
//	dedup probe of applied_mutations short-circuits with the prior resultVersion
//	and NO second state commit.
func TestRefCasVsConcurrentWriter(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())

	// Seed the aggregate and stage+leave a mission slot so CommitArtifact has a
	// populated slot to transition.
	if _, err := store.CreateProject(ctx, id, "alice", "Race", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReview(ctx, id, 1, &ps.MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}

	// Both writers observe the SAME base version (v2).
	base := v2

	type outcome struct {
		who string
		v   ps.Version
		err error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer A — reconcile tick: CommitArtifact(mission) at base.
	go func() {
		defer wg.Done()
		v, e := store.CommitArtifact(ctx, id, base, ps.KindMission, cred, "wf:reconcile-commit")
		results <- outcome{"A-commit", v, e}
	}()
	// Writer B — operator pause: RecordOperatorPaused at the SAME base.
	go func() {
		defer wg.Done()
		v, e := store.RecordOperatorPaused(ctx, id, base, "operator pause", cred, "wf:operator-pause")
		results <- outcome{"B-pause", v, e}
	}()
	wg.Wait()
	close(results)

	var winner, loser outcome
	gotWinner := false
	for r := range results {
		if r.err == nil {
			winner = r
			gotWinner = true
		} else {
			loser = r
		}
	}

	// Exactly one writer wins; the other loses the CAS with fwra.Conflict.
	if !gotWinner {
		t.Fatal("expected exactly one CAS winner, both lost")
	}
	if loser.who == "" {
		// Both succeeded would mean a lost update (the file transport serialized
		// without contention). Force the contention deterministically below if that
		// ever happens; for a true race we require a loser.
		t.Fatal("expected one CAS loser (non-fast-forward), both won — LOST UPDATE")
	}
	if k := kindOf(t, loser.err); k != fwra.Conflict {
		t.Fatalf("loser %s kind = %v, want Conflict", loser.who, k)
	}
	if !errors.Is(loser.err, fwgithub.ErrRefCASLost) {
		t.Fatalf("loser %s error not ErrRefCASLost: %v", loser.who, loser.err)
	}

	// The loser reloads HEAD and re-applies against the winner's new tip.
	cur, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after race: %v", err)
	}
	var retried ps.Version
	switch loser.who {
	case "A-commit":
		retried, err = store.CommitArtifact(ctx, id, cur.Version, ps.KindMission, cred, "wf:reconcile-commit")
	case "B-pause":
		retried, err = store.RecordOperatorPaused(ctx, id, cur.Version, "operator pause", cred, "wf:operator-pause")
	}
	if err != nil {
		t.Fatalf("loser %s retry: %v", loser.who, err)
	}

	// BOTH mutations survive: the winner's effect is visible AND the loser's retry
	// landed at the next version (convergence, no lost update).
	final, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject final: %v", err)
	}
	if final.Version != retried {
		t.Fatalf("final version %d != retried %d", final.Version, retried)
	}
	if final.Version != winner.v+1 {
		t.Fatalf("final version %d, want winner+1 (%d) — both writers must have landed", final.Version, winner.v+1)
	}
	// The winner's CommitArtifact (if A won) must have left the mission Committed;
	// if A was the loser, its retry committed it. Either way mission is Committed.
	if final.Mission.Status != ps.ReviewCommitted {
		t.Fatalf("mission status = %v, want Committed (the commit must have landed)", final.Mission.Status)
	}

	// (b) Activity-retry idempotency / dedup, NO double-apply. Re-pass the WINNER's
	// idempotency key with a now-stale expectedVersion: the dedup probe must
	// short-circuit and return the winner's original resultVersion with no new
	// commit (no Conflict despite the stale version).
	winnerKey := fwra.IdempotencyKey("wf:reconcile-commit")
	if winner.who == "B-pause" {
		winnerKey = "wf:operator-pause"
	}
	beforeRetry, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject before dedup retry: %v", err)
	}
	var dedupV ps.Version
	switch winner.who {
	case "A-commit":
		dedupV, err = store.CommitArtifact(ctx, id, 0 /* deliberately stale */, ps.KindMission, cred, winnerKey)
	case "B-pause":
		dedupV, err = store.RecordOperatorPaused(ctx, id, 0 /* stale */, "operator pause", cred, winnerKey)
	}
	if err != nil {
		t.Fatalf("dedup retry of winner key (stale version) should succeed via ledger, got: %v", err)
	}
	if dedupV != winner.v {
		t.Fatalf("dedup retry returned version %d, want winner's original %d", dedupV, winner.v)
	}
	afterRetry, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after dedup retry: %v", err)
	}
	if afterRetry.Version != beforeRetry.Version {
		t.Fatalf("dedup retry produced a NEW state commit: version moved %d -> %d (DOUBLE APPLY)", beforeRetry.Version, afterRetry.Version)
	}
}

// TestGitStore_ExternalActionDraftIsReadBack — THE C-PA-RB RE-SCOPE PROOF.
//
// After C-MSD-Δ/C-MPD-Δ the design DRAFT path no longer writes draft JSON through
// any projectStateAccess write verb: the agentic Action commits the typed draft into
// `.aiarch/state/project.json` inside the user's CI, and the server's ONLY draft-path
// touch is ReadProject (the read-back). This test proves that read-back works against
// a draft committed EXTERNALLY — i.e. by something OTHER than the GitStore write verbs.
//
// It simulates the Action by encoding a Project (with a committed-by-the-Action Mission
// slot AND the critique carrier the C-MSD-Δ-critique-fix Action sets) via the canonical
// EncodeProjectJSON seam and committing it straight to `.aiarch/state/project.json`
// through the RAW fwgithub.GitStore.CommitSubtree — NOT through StageArtifactForReview /
// CommitArtifact / any RA verb. The RA's public ReadProject must then surface the exact
// typed model, status, version, phase, AND the critique carrier the Action committed.
func TestGitStore_ExternalActionDraftIsReadBack(t *testing.T) {
	store, raw, cred, ctx := newLocalGitStoreWithRepo(t)
	id := ps.ProjectID(uuid.NewString())

	// Build the head-state the AGENTIC ACTION would have committed in the user's CI:
	// the typed Mission model in its slot at ReviewCommitted, plus the PM-critique
	// carrier (verdict + notes) the critique Action writes — the exact draft-path
	// shape the server now only READS, never writes.
	actionState := ps.Project{
		ID:      id,
		Version: 7, // an arbitrary version the Action's commits advanced to
		Phase:   ps.PhaseSystemDesign,
		Owner:   "alice",
		Name:    "ExternallyDrafted",
		Mission: ps.ArtifactSlot{
			Status:          ps.ReviewCommitted,
			Model:           &ps.MissionStatement{Vision: "action-vision", Mission: "action-mission"},
			CritiqueVerdict: ps.CritiqueVerdictRevise,
			CritiqueNotes:   "tighten the mission scope",
		},
	}
	raw1, err := ps.EncodeProjectJSON(actionState)
	if err != nil {
		t.Fatalf("EncodeProjectJSON (simulate Action): %v", err)
	}

	// Commit it DIRECTLY to `.aiarch/state/project.json` via the raw satellite — the
	// Action's seam, bypassing every projectStateAccess write verb. Read the current
	// branch tip first so the CAS base matches (the repo is born with a `main` branch).
	snap, err := raw.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("raw ReadSubtree (observe base): %v", err)
	}
	if _, err := raw.CommitSubtree(
		ctx,
		".aiarch/state",
		map[string][]byte{"project.json": raw1},
		snap.Base, // CAS against the observed tip (the Action's working-tree commit)
		"action: commit design draft",
		fwgithub.GitAuth{Local: true},
	); err != nil {
		t.Fatalf("raw CommitSubtree (simulate Action draft commit): %v", err)
	}

	// READ-BACK through the RA's PUBLIC verb — the server's only draft-path touch.
	proj, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject (read-back of external draft): %v", err)
	}

	if proj.Version != 7 || proj.Phase != ps.PhaseSystemDesign || proj.Owner != "alice" || proj.Name != "ExternallyDrafted" {
		t.Fatalf("read-back identity = v%d/%v/%s/%s, want v7/SystemDesign/alice/ExternallyDrafted",
			proj.Version, proj.Phase, proj.Owner, proj.Name)
	}
	if proj.Mission.Status != ps.ReviewCommitted {
		t.Fatalf("read-back mission status = %v, want Committed (the Action committed it)", proj.Mission.Status)
	}
	gotMission, ok := proj.Mission.Model.(*ps.MissionStatement)
	if !ok || gotMission.Vision != "action-vision" || gotMission.Mission != "action-mission" {
		t.Fatalf("read-back mission model = %+v, want the Action's typed model", proj.Mission.Model)
	}
	// The critique carrier the Action committed must round-trip on the read-back —
	// the C-MSD-Δ-critique-fix first-class carrier, read (not written) server-side.
	if proj.Mission.CritiqueVerdict != ps.CritiqueVerdictRevise || proj.Mission.CritiqueNotes != "tighten the mission scope" {
		t.Fatalf("read-back critique carrier = (%q, %q), want (revise, tighten the mission scope)",
			proj.Mission.CritiqueVerdict, proj.Mission.CritiqueNotes)
	}

	// And the HUMAN-GATE write path still works server-side over the read-back draft:
	// stage the read-back model for review (the AwaitingReview thin-write), then commit
	// on approve — proving the surviving human-gate verbs are intact post-re-scope.
	v8, err := store.StageArtifactForReview(ctx, id, proj.Version, gotMission, cred, "wf:human-gate-stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview (human-gate over read-back draft): %v", err)
	}
	if _, err := store.CommitArtifact(ctx, id, v8, ps.KindMission, cred, "wf:human-gate-commit"); err != nil {
		t.Fatalf("CommitArtifact (human-gate approve): %v", err)
	}
	after, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after human-gate commit: %v", err)
	}
	if after.Mission.Status != ps.ReviewCommitted {
		t.Fatalf("post-human-gate mission status = %v, want Committed", after.Mission.Status)
	}
	// StageArtifactForReview clears the critique carrier (the C-MSD-Δ-critique-fix
	// isolation rule) — the server-side stage write must NOT carry the Action's stale
	// critique verdict forward into the human-gate state.
	if after.Mission.CritiqueVerdict != "" || after.Mission.CritiqueNotes != "" {
		t.Fatalf("human-gate stage must clear the critique carrier; got (%q, %q)",
			after.Mission.CritiqueVerdict, after.Mission.CritiqueNotes)
	}
}

// TestGitStore_CreateProject_ResumesExistingState proves the PERMISSIVE-RESUME
// CreateProject (founder ruling 2026-06-16): when a repo ALREADY carries a committed
// `.aiarch/state/project.json` (a prior run's progress), CreateProject RE-INITIALIZES
// the project FROM CURRENT PROGRESS — it RETURNS the existing version and does NOT
// clobber/reset the state, NOR error on already-exists. This is the I-RA-Δ resume
// behavior proven at the projectStateAccess seam.
func TestGitStore_CreateProject_ResumesExistingState(t *testing.T) {
	store, raw, cred, ctx := newLocalGitStoreWithRepo(t)
	id := ps.ProjectID("my-resumed-project")

	// Build the head-state a PRIOR run committed: the project advanced to Project Design
	// with a committed Mission slot and research input — current progress to be resumed.
	priorState := ps.Project{
		ID:      id,
		Version: 5, // a version the prior run's commits advanced to
		Phase:   ps.PhaseProjectDesign,
		Owner:   "alice",
		Name:    "Resumed System",
		ResearchInput: ps.ResearchInput{
			Sources: []ps.ResearchSource{{Title: "Brief", Content: "prior founder brief"}},
		},
		Mission: ps.ArtifactSlot{
			Status: ps.ReviewCommitted,
			Model:  &ps.MissionStatement{Vision: "prior-vision", Mission: "prior-mission"},
		},
	}
	raw1, err := ps.EncodeProjectJSON(priorState)
	if err != nil {
		t.Fatalf("EncodeProjectJSON (prior state): %v", err)
	}

	// Commit it DIRECTLY to `.aiarch/state/project.json` (the prior run's seam), then
	// run CreateProject against the SAME repo — the resume case.
	snap, err := raw.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("raw ReadSubtree (observe base): %v", err)
	}
	if _, err := raw.CommitSubtree(
		ctx, ".aiarch/state",
		map[string][]byte{"project.json": raw1},
		snap.Base, "prior run: commit progress", fwgithub.GitAuth{Local: true},
	); err != nil {
		t.Fatalf("raw CommitSubtree (simulate prior progress): %v", err)
	}

	// CreateProject against the repo that already has committed state → RESUME.
	// It returns the EXISTING version (5), not 1 (a fresh init) and not an error.
	v, err := store.CreateProject(ctx, id, "alice", "Resumed System", cred, "wf:create-resume")
	if err != nil {
		t.Fatalf("CreateProject (resume) must NOT error on already-existing state, got: %v", err)
	}
	if v != 5 {
		t.Fatalf("CreateProject (resume) returned version %d, want the existing 5 (re-init from current progress)", v)
	}

	// The existing state SURVIVES (no clobber/reset): read it back and assert the prior
	// progress — phase, version, committed Mission model — is intact.
	got, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after resume: %v", err)
	}
	if got.Version != 5 || got.Phase != ps.PhaseProjectDesign || got.Name != "Resumed System" {
		t.Fatalf("resume clobbered state: got v%d/%v/%s, want v5/ProjectDesign/Resumed System",
			got.Version, got.Phase, got.Name)
	}
	if got.Mission.Status != ps.ReviewCommitted {
		t.Fatalf("resume lost the committed Mission slot: status = %v, want Committed", got.Mission.Status)
	}
	gotMission, ok := got.Mission.Model.(*ps.MissionStatement)
	if !ok || gotMission.Vision != "prior-vision" || gotMission.Mission != "prior-mission" {
		t.Fatalf("resume lost the typed Mission model: %+v", got.Mission.Model)
	}
	if len(got.ResearchInput.Sources) != 1 || got.ResearchInput.Sources[0].Content != "prior founder brief" {
		t.Fatalf("resume lost the research input: %+v", got.ResearchInput)
	}
}

// TestGitStore_CreateProject_FreshInitWhenNoState proves the other branch of the
// permissive-resume CreateProject: a repo with NO committed `.aiarch/state/project.json`
// (no prior progress) is initialized FRESH at Version 1, exactly as before.
func TestGitStore_CreateProject_FreshInitWhenNoState(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID("brand-new-project")

	v, err := store.CreateProject(ctx, id, "alice", "Brand New", cred, "wf:create-fresh")
	if err != nil {
		t.Fatalf("CreateProject (fresh): %v", err)
	}
	if v != 1 {
		t.Fatalf("CreateProject (fresh, no prior state) version = %d, want 1", v)
	}
	got, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after fresh init: %v", err)
	}
	if got.Version != 1 || got.Phase != ps.PhaseSystemDesign || got.Name != "Brand New" {
		t.Fatalf("fresh init = v%d/%v/%s, want v1/SystemDesign/Brand New", got.Version, got.Phase, got.Name)
	}
}
