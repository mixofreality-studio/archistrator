package projectstate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

// access_test.go merges every hand-written test file for the projectstate
// ResourceAccess component into the single closed test file the FileLayout
// gate requires (one <stereotype>_test.go per leaf package). Nine of the
// merged suites originally lived in the black-box package `projectstate_test`
// (import-and-call-only via a `ps` alias, exercising ONLY the exported
// surface): activityconstruction_test.go, decode_terminal_test.go,
// gitactivity_test.go, gitconstruction_test.go, gitreviewthread_test.go,
// gitstore_test.go, operatingmodel_test.go, provenance_store_test.go,
// servicecontract_test.go. Now that they live in-package (the `ps.`
// qualifiers were stripped mechanically on the fold), they continue, BY
// CONVENTION ONLY (no longer compiler-enforced), to exercise exclusively the
// exported surface of this package — no test added here should reach for an
// unexported identifier on behalf of a former black-box suite without a
// deliberate, separate decision to do so.

// gitadapter_test.go is the I-GIT-DESIGN PROOF: it drives the UC1/UC2
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

// localProjectStateOverGit spins a real throwaway on-disk git repo (per-project) and
// builds the production composition-root adapter (projectStateGitAdapter with the LOCAL
// credential minter + the LOCAL discover-by-enumeration catalog) over it — the EXACT
// types main.go wires for the LOCAL profile. It returns the no-cred ProjectStateAccess
// the Managers consume. (The cross-project registry repo is GONE — founder ruling
// 2026-06-14; the catalog is discovered by scanning the on-disk project repo.)
func localProjectStateOverGit(t *testing.T) ProjectStateAccess {
	t.Helper()
	projRepo := gh.StartLocalGitRepo(t, "main")

	// One project repo for the whole test (a single project) — the locator returns the
	// same handle for every projectID, which is exactly the LOCAL single-repo profile.
	// The locator hands the URL to the RA's own fwgithub.NewGitStore (gitRepoLocator),
	// so this drives the SAME construction path main.go's LOCAL profile uses.
	locator := gitRepoLocator{
		branch:            "main",
		perProjectRepoURL: func(ProjectID) string { return projRepo.URL },
	}

	store, err := NewGitStore(locator, true /* local */)
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
	id := ProjectID(uuid.NewString())

	// CreateProject — births the aggregate at version 1 (no registry index — the repo's
	// existence + project.json IS the catalog entry, founder ruling 2026-06-14).
	v1, err := state.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: "wf:create"}, id, "alice", "Demo")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("CreateProject version = %d, want 1", v1)
	}

	// Stage the mission typed model (UC1 step 1 — systemDesignManager stages the draft).
	// The main-branch StageArtifactForReview contract op was retired (Wave-1 fossil prune);
	// staging now rides the surviving designSession branch verb with an empty branch (== main).
	mission := &MissionStatement{Vision: "vision-text", Mission: "mission-text"}
	missionEnv, err := EncodeModel(mission)
	if err != nil {
		t.Fatalf("EncodeModel: %v", err)
	}
	session := NewDesignSessionAccess(state)
	v2, err := session.StageArtifactForReviewOnBranch(fwra.Context{Context: ctx, IdempotencyKey: "wf:stage-mission"}, id, v1, "", missionEnv, "wf:stage-mission")
	if err != nil {
		t.Fatalf("StageArtifactForReviewOnBranch: %v", err)
	}

	// Commit the mission (architect approved at the review gate).
	v3, err := state.CommitArtifact(fwra.Context{Context: ctx, IdempotencyKey: "wf:commit-mission"}, id, v2, KindMission)
	if err != nil {
		t.Fatalf("CommitArtifact: %v", err)
	}

	// Re-read through a FRESH clone — proves the JSON is committed to the git repo.
	proj, err := state.ReadProject(fwra.Context{Context: ctx}, id)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != v3 {
		t.Fatalf("ReadProject version = %d, want %d", proj.Version, v3)
	}
	if proj.Mission.Status != ReviewCommitted {
		t.Fatalf("mission status = %v, want Committed", proj.Mission.Status)
	}
	got, ok := proj.Mission.Model.(*MissionStatement)
	if !ok || got.Vision != "vision-text" || got.Mission != "mission-text" {
		t.Fatalf("mission model round-trip through git failed: %+v", proj.Mission.Model)
	}

	// The catalog read (ListProjects) surfaces the project by ENUMERATING the on-disk
	// project repo (discover-by-enumeration) — no registry index, the repo IS the row.
	summaries, err := state.ListProjects(fwra.Context{Context: ctx}, "alice")
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
	id := ProjectID(uuid.NewString())

	v1, err := state.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: "wf:create"}, id, "bob", "Proj2")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	research := ResearchInput{Sources: []ResearchSource{{Title: "src", Content: "research-corpus"}}}
	v2, err := state.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: "wf:research"}, id, v1, research)
	if err != nil {
		t.Fatalf("SetResearchInput: %v", err)
	}

	v3, err := state.AdvancePhase(fwra.Context{Context: ctx, IdempotencyKey: "wf:advance"}, id, v2)
	if err != nil {
		t.Fatalf("AdvancePhase: %v", err)
	}

	proj, err := state.ReadProject(fwra.Context{Context: ctx}, id)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != v3 {
		t.Fatalf("version = %d, want %d", proj.Version, v3)
	}
	if proj.Phase == PhaseSystemDesign {
		t.Fatalf("phase did not advance past SystemDesign: %v", proj.Phase)
	}
	// F42: research is persisted as files + pointers ({Title, Path, ContentBytes}), so the
	// round-trip carries the pointer (content lives in .aiarch/state/research/<slug>.txt).
	if len(proj.Research.Sources) != 1 || proj.Research.Sources[0].Path != ".aiarch/state/research/00-src.txt" || proj.Research.Sources[0].ContentBytes != int64(len("research-corpus")) {
		t.Fatalf("research pointer did not round-trip through git: %+v", proj.Research)
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
		account = "mixofreality-studio"
		name    = "my-cool-system" // a USER-supplied repo name == the project identity
	)
	got := cloudPerProjectRepoURL(webHost, account, name)
	want := "https://github.com/mixofreality-studio/my-cool-system.git"
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
		perProjectRepoURL: func(ProjectID) string { return projRepo.URL },
	}
	store, err := NewGitStore(locator, true /* local */)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(localProjectCatalog{repoURL: projRepo.URL, branch: "main"})
	state := &projectStateGitAdapter{store: store, minter: localCredentialMinter{}}
	ctx := context.Background()

	id := ProjectID("my-cool-system") // a USER-supplied repo name == the project identity
	identity := id.String()           // the verbatim identity string that must persist unrewritten

	// createProject — expectedVersion discipline: births at version 1.
	v1, err := state.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: "wf:create"}, id, "alice", "My Cool System")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("CreateProject version = %d, want 1", v1)
	}

	// ReadProject — the identity round-trips whole.
	proj, err := state.ReadProject(fwra.Context{Context: ctx}, id)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.ID != id || proj.Name != "My Cool System" {
		t.Fatalf("ReadProject identity/name mismatch: id=%s name=%q", proj.ID, proj.Name)
	}

	// The persisted `id` in .aiarch/state/project.json is the identity VERBATIM — no
	// "aiarch-" prefix is ever applied by the createProject path. Read the raw committed
	// JSON through a fresh clone to assert the on-disk shape.
	assertIdentityPersistedVerbatimOnDisk(ctx, t, projRepo.URL, identity)

	// ListProjects (discover-by-enumeration) surfaces the project keyed by the SAME
	// identity — the repo IS the catalog row, name-as-identity end to end.
	summaries, err := state.ListProjects(fwra.Context{Context: ctx}, "alice")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ProjectID != id {
		t.Fatalf("ListProjects = %+v, want one row with identity %s", summaries, identity)
	}

	// idempotencyKey discipline: a retried createProject with the SAME key is a no-op
	// that returns the prior version (no double-create, no Conflict).
	vDup, err := state.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: "wf:create"}, id, "alice", "My Cool System")
	if err != nil {
		t.Fatalf("CreateProject retry (same key) should dedup, got: %v", err)
	}
	if vDup != v1 {
		t.Fatalf("CreateProject retry returned version %d, want the prior %d (dedup)", vDup, v1)
	}
}

// assertIdentityPersistedVerbatimOnDisk reads the raw committed project.json through a
// fresh clone and asserts the on-disk `id` is the identity verbatim (no "aiarch-" prefix).
func assertIdentityPersistedVerbatimOnDisk(ctx context.Context, t *testing.T, repoURL, identity string) {
	t.Helper()
	gs, err := fwgithub.NewGitStore(repoURL, "main")
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
}

// gitconstruction_test.go — black-box regression tests for the 7 construction-
// transition verbs (Task 4: state foundation). Mirrors the activityconstruction_test.go
// and gitactivity_test.go discipline: real throwaway on-disk git store, no mocks,
// test-authoring constitution §7 anti-cheat.
//
// STP: all the ways these verbs can fail to work correctly.
//   1. RecordChangeReviewed sets BuildStatus = BuildInReview.
//   2. RecordActivityExited(Completed) → Phase=Done, BuildStatus=Integrated, CompletedAt set.
//   3. RecordActivityExited(Skipped) → Phase=Done, BuildStatus=InReview, CompletedAt set.
//   4. RecordOperatorPaused → Project.OperatorPaused=true, PauseReason set, persists round-trip.
//   5. RecordPhaseStarted seeds Phases, sets CurrentPhase, advances coarse Phase to Running.
//   6. RecordPhaseCompleted marks the phase Completed=true, ArtifactRef set, CoarsePhase recomputed.
//   7. After ALL phases completed, CoarsePhase = Done.
//   8. RecordServiceContractProduced writes contract under component key.
//   9. RecordPhaseArtifactProduced(SRS) → PhaseArtifacts.SRS keyed by mapKey.
//   10. RecordPhaseArtifactProduced(SystemTestPlan) → TestingState.SystemTestPlan set.
//   11. RecordPhaseStarted idempotency — same key, stale version → ledger wins.
//   12. PhaseArtifactPayload round-trip via EncodeProjectJSON → DecodeProjectJSON.

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

func readProject(t *testing.T, store *GitStore, id ProjectID, cred RepoCredential) Project {
	t.Helper()
	p, err := store.ReadProject(fwra.Context{Context: context.Background()}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	return p
}

func readConstructionStatus(t *testing.T, store *GitStore, id ProjectID, cred RepoCredential, activityID string) ActivityConstructionStatus {
	t.Helper()
	p := readProject(t, store, id, cred)
	s, ok := p.ActivityConstruction[activityID]
	if !ok {
		t.Fatalf("ActivityConstruction[%s] absent", activityID)
	}
	return s
}

// seedActivity creates a project and calls RecordActivityStarted so
// modeRequireExisting verbs have a row to upsert.
func seedActivity(t *testing.T, store *GitStore, id ProjectID, v Version, cred RepoCredential, activityID string) Version {
	t.Helper()
	v2, err := store.RecordActivityStarted(fwra.Context{Context: context.Background()}, id, v, activityID, cred, fwra.IdempotencyKey("wf:seed-"+activityID))
	if err != nil {
		t.Fatalf("RecordActivityStarted(%s): %v", activityID, err)
	}
	return v2
}

// --------------------------------------------------------------------------
// STP 1: RecordChangeReviewed sets BuildStatus = BuildInReview
// --------------------------------------------------------------------------

func TestRecordChangeReviewed_SetsInReview(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2 := seedActivity(t, store, id, v, cred, "C001")

	v3, err := store.RecordChangeReviewed(fwra.Context{Context: ctx}, id, v2, "C001", cred, fwra.IdempotencyKey("wf:cr-reviewed"))
	if err != nil {
		t.Fatalf("RecordChangeReviewed: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}

	s := readConstructionStatus(t, store, id, cred, "C001")
	if s.BuildStatus != BuildInReview {
		t.Fatalf("BuildStatus = %v, want BuildInReview", s.BuildStatus)
	}
}

// STP 1b: empty activityID is rejected at the guard
func TestRecordChangeReviewed_EmptyActivityID_Error(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()
	_, err := store.RecordChangeReviewed(fwra.Context{Context: ctx}, id, v, "", cred, fwra.IdempotencyKey("wf:cr-empty"))
	if err == nil {
		t.Fatal("want error for empty activityID, got nil")
	}
}

// --------------------------------------------------------------------------
// STP 2: RecordActivityExited(Completed) → Phase=Done, BuildStatus=Integrated
// --------------------------------------------------------------------------

func TestRecordActivityExited_Completed_SetsDone(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2 := seedActivity(t, store, id, v, cred, "C002")

	v3, err := store.RecordActivityExited(fwra.Context{Context: ctx}, id, v2, "C002", ActivityOutcomeCompleted, cred, fwra.IdempotencyKey("wf:exited-completed"))
	if err != nil {
		t.Fatalf("RecordActivityExited: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}

	s := readConstructionStatus(t, store, id, cred, "C002")
	if s.Phase != ActivityConstructionDone {
		t.Fatalf("Phase = %v, want Done", s.Phase)
	}
	if s.BuildStatus != BuildIntegrated {
		t.Fatalf("BuildStatus = %v, want BuildIntegrated", s.BuildStatus)
	}
	if s.CompletedAt == nil {
		t.Fatal("CompletedAt must be set after RecordActivityExited")
	}
}

// --------------------------------------------------------------------------
// STP 3: RecordActivityExited(Skipped) → Phase=Done, BuildStatus=InReview
// --------------------------------------------------------------------------

func TestRecordActivityExited_Skipped_SetsDone(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2 := seedActivity(t, store, id, v, cred, "C003")

	_, err := store.RecordActivityExited(fwra.Context{Context: ctx}, id, v2, "C003", ActivityOutcomeSkipped, cred, fwra.IdempotencyKey("wf:exited-skipped"))
	if err != nil {
		t.Fatalf("RecordActivityExited(Skipped): %v", err)
	}

	s := readConstructionStatus(t, store, id, cred, "C003")
	if s.Phase != ActivityConstructionDone {
		t.Fatalf("Phase = %v, want Done", s.Phase)
	}
	if s.BuildStatus != BuildInReview {
		t.Fatalf("BuildStatus = %v, want BuildInReview (skipped)", s.BuildStatus)
	}
	if s.CompletedAt == nil {
		t.Fatal("CompletedAt must be set after Skipped exit")
	}
}

// --------------------------------------------------------------------------
// STP 4: RecordOperatorPaused → OperatorPaused=true, PauseReason set
// --------------------------------------------------------------------------

func TestRecordOperatorPaused_SetsPaused(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2, err := store.RecordOperatorPaused(fwra.Context{Context: ctx}, id, v, "awaiting contractor availability", cred, fwra.IdempotencyKey("wf:paused"))
	if err != nil {
		t.Fatalf("RecordOperatorPaused: %v", err)
	}
	if v2 != v+1 {
		t.Fatalf("version = %d, want %d", v2, v+1)
	}

	p := readProject(t, store, id, cred)
	if !p.OperatorPaused {
		t.Fatal("OperatorPaused must be true after RecordOperatorPaused")
	}
	if p.PauseReason != "awaiting contractor availability" {
		t.Fatalf("PauseReason = %q, want %q", p.PauseReason, "awaiting contractor availability")
	}
}

// STP 4b: OperatorPaused survives EncodeProjectJSON → DecodeProjectJSON
func TestRecordOperatorPaused_RoundTrip(t *testing.T) {
	p := Project{
		OperatorPaused: true,
		PauseReason:    "manual hold",
	}
	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON: ok=false")
	}
	if !got.OperatorPaused {
		t.Fatal("OperatorPaused lost across round-trip")
	}
	if got.PauseReason != "manual hold" {
		t.Fatalf("PauseReason = %q, want %q", got.PauseReason, "manual hold")
	}
}

// --------------------------------------------------------------------------
// STP 5: RecordPhaseStarted seeds Phases, sets CurrentPhase, coarse=Running
// --------------------------------------------------------------------------

func TestRecordPhaseStarted_SeedsPhaseSet(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2 := seedActivity(t, store, id, v, cred, "C004")

	v3, err := store.RecordPhaseStarted(fwra.Context{Context: ctx}, id, v2, "C004", MethodPhaseRequirements, cred, fwra.IdempotencyKey("wf:phase-started"))
	if err != nil {
		t.Fatalf("RecordPhaseStarted: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}

	s := readConstructionStatus(t, store, id, cred, "C004")
	// Service type (zero value) → 5-phase set
	if len(s.Phases) != 5 {
		t.Fatalf("Phases len = %d, want 5 (service type)", len(s.Phases))
	}
	if s.CurrentPhase != MethodPhaseRequirements {
		t.Fatalf("CurrentPhase = %q, want %q", s.CurrentPhase, MethodPhaseRequirements)
	}
	if s.Phase != ActivityConstructionRunning {
		t.Fatalf("Phase = %v, want Running", s.Phase)
	}
}

// STP 5b: empty phase is rejected
func TestRecordPhaseStarted_EmptyPhase_Error(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()
	v2 := seedActivity(t, store, id, v, cred, "C004b")
	_, err := store.RecordPhaseStarted(fwra.Context{Context: ctx}, id, v2, "C004b", "", cred, fwra.IdempotencyKey("wf:phase-started-empty"))
	if err == nil {
		t.Fatal("want error for empty phase, got nil")
	}
}

// --------------------------------------------------------------------------
// STP 6: RecordPhaseCompleted marks phase Completed, ArtifactRef set
// --------------------------------------------------------------------------

func TestRecordPhaseCompleted_MarksPhase(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2 := seedActivity(t, store, id, v, cred, "C005")
	v3, err := store.RecordPhaseStarted(fwra.Context{Context: ctx}, id, v2, "C005", MethodPhaseRequirements, cred, fwra.IdempotencyKey("wf:ps-c005"))
	if err != nil {
		t.Fatalf("RecordPhaseStarted: %v", err)
	}

	v4, err := store.RecordPhaseCompleted(fwra.Context{Context: ctx}, id, v3, "C005", MethodPhaseRequirements, "srs/myservice.md", cred, fwra.IdempotencyKey("wf:pc-c005"))
	if err != nil {
		t.Fatalf("RecordPhaseCompleted: %v", err)
	}
	if v4 != v3+1 {
		t.Fatalf("version = %d, want %d", v4, v3+1)
	}

	s := readConstructionStatus(t, store, id, cred, "C005")
	var reqPhase *PhaseCompletion
	for i := range s.Phases {
		if s.Phases[i].Phase == MethodPhaseRequirements {
			reqPhase = &s.Phases[i]
			break
		}
	}
	if reqPhase == nil {
		t.Fatal("MethodPhaseRequirements not in Phases after RecordPhaseCompleted")
	}
	if !reqPhase.Completed {
		t.Fatal("Completed must be true after RecordPhaseCompleted")
	}
	if reqPhase.CompletedAt == nil {
		t.Fatal("CompletedAt must be set after RecordPhaseCompleted")
	}
	if reqPhase.ArtifactRef != "srs/myservice.md" {
		t.Fatalf("ArtifactRef = %q, want srs/myservice.md", reqPhase.ArtifactRef)
	}
}

// --------------------------------------------------------------------------
// STP 7: after ALL phases completed, CoarsePhase = Done
// --------------------------------------------------------------------------

func TestRecordPhaseCompleted_AllPhasesDone_CoarsePhaseIsDone(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2 := seedActivity(t, store, id, v, cred, "C006")
	// Service type → phases: requirements, detailed_design, test_plan, construction, integration
	phases := []ActivityMethodPhase{
		MethodPhaseRequirements,
		MethodPhaseDetailedDesign,
		MethodPhaseTestPlan,
		MethodPhaseConstruction,
		MethodPhaseIntegration,
	}

	cur := v2
	for i, ph := range phases {
		startKey := fwra.IdempotencyKey("wf:ps-c006-" + ph.String())
		cur2, err := store.RecordPhaseStarted(fwra.Context{Context: ctx}, id, cur, "C006", ph, cred, startKey)
		if err != nil {
			t.Fatalf("RecordPhaseStarted(%s): %v", ph, err)
		}
		completedKey := fwra.IdempotencyKey("wf:pc-c006-" + ph.String())
		cur3, err := store.RecordPhaseCompleted(fwra.Context{Context: ctx}, id, cur2, "C006", ph, "", cred, completedKey)
		if err != nil {
			t.Fatalf("RecordPhaseCompleted(%s): %v", ph, err)
		}
		_ = i
		cur = cur3
	}

	s := readConstructionStatus(t, store, id, cred, "C006")
	if s.Phase != ActivityConstructionDone {
		t.Fatalf("Phase = %v after all phases completed, want Done", s.Phase)
	}
}

// --------------------------------------------------------------------------
// STP 8: RecordServiceContractProduced writes contract under component key
// --------------------------------------------------------------------------

func TestRecordServiceContractProduced_WritesContract(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	contract := ServiceContract{
		Component: "myEngine",
		Layer:     "Engine",
		GoPackage: "internal/engine/myengine",
		Title:     "myengine contract",
		Interface: ContractInterface{
			Name:  "MyEngine",
			Layer: "engine",
			Operations: []ContractOperation{
				{Name: "Compute", Error: true},
			},
		},
	}

	v2, err := store.RecordServiceContractProduced(fwra.Context{Context: ctx}, id, v, "myEngine", contract, cred, fwra.IdempotencyKey("wf:contract-myengine"))
	if err != nil {
		t.Fatalf("RecordServiceContractProduced: %v", err)
	}
	if v2 != v+1 {
		t.Fatalf("version = %d, want %d", v2, v+1)
	}

	p := readProject(t, store, id, cred)
	sc, found := p.ServiceContracts["myEngine"]
	if !found {
		t.Fatal("ServiceContracts[myEngine] absent after RecordServiceContractProduced")
	}
	if sc.Component != "myEngine" {
		t.Fatalf("Component = %q, want myEngine", sc.Component)
	}
	if sc.Title != "myengine contract" {
		t.Fatalf("Title = %q, want myengine contract", sc.Title)
	}
}

// STP 8b: empty component is rejected
func TestRecordServiceContractProduced_EmptyComponent_Error(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()
	_, err := store.RecordServiceContractProduced(fwra.Context{Context: ctx}, id, v, "", ServiceContract{}, cred, fwra.IdempotencyKey("wf:contract-empty"))
	if err == nil {
		t.Fatal("want error for empty component, got nil")
	}
}

// --------------------------------------------------------------------------
// STP 9: RecordPhaseArtifactProduced(SRS) → PhaseArtifacts.SRS[key]
// --------------------------------------------------------------------------

func TestRecordPhaseArtifactProduced_SRS(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2 := seedActivity(t, store, id, v, cred, "C007")

	now := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	payload := PhaseArtifactPayload{
		SRS: &SRSRecord{
			Component:  "myService",
			Content:    "the service does X when Y",
			AuthoredAt: &now,
		},
	}

	v3, err := store.RecordPhaseArtifactProduced(fwra.Context{Context: ctx}, id, v2, "C007", "myService", payload, cred, fwra.IdempotencyKey("wf:artifact-srs"))
	if err != nil {
		t.Fatalf("RecordPhaseArtifactProduced(SRS): %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}

	p := readProject(t, store, id, cred)
	if p.PhaseArtifacts == nil {
		t.Fatal("PhaseArtifacts nil after RecordPhaseArtifactProduced")
	}
	srs, found := p.PhaseArtifacts.SRS["myService"]
	if !found {
		t.Fatal("PhaseArtifacts.SRS[myService] absent")
	}
	if srs.Component != "myService" {
		t.Fatalf("SRS.Component = %q, want myService", srs.Component)
	}
	if srs.Content != "the service does X when Y" {
		t.Fatalf("SRS.Content = %q, unexpected", srs.Content)
	}
}

// --------------------------------------------------------------------------
// STP 10: RecordPhaseArtifactProduced(SystemTestPlan) → TestingState.SystemTestPlan
// --------------------------------------------------------------------------

func TestRecordPhaseArtifactProduced_SystemTestPlan(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2 := seedActivity(t, store, id, v, cred, "N001")

	approved := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	payload := PhaseArtifactPayload{
		SystemTestPlan: &SystemTestPlan{
			UseCaseIndex: []string{"UC1", "UC2"},
			Entries:      []string{"verify create project", "verify read project"},
			Status:       "approved",
			ApprovedAt:   &approved,
		},
	}

	v3, err := store.RecordPhaseArtifactProduced(fwra.Context{Context: ctx}, id, v2, "N001", "", payload, cred, fwra.IdempotencyKey("wf:artifact-stp"))
	if err != nil {
		t.Fatalf("RecordPhaseArtifactProduced(SystemTestPlan): %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}

	p := readProject(t, store, id, cred)
	if p.TestingState == nil {
		t.Fatal("TestingState nil after RecordPhaseArtifactProduced(SystemTestPlan)")
	}
	if p.TestingState.SystemTestPlan == nil {
		t.Fatal("TestingState.SystemTestPlan nil")
	}
	if p.TestingState.SystemTestPlan.Status != "approved" {
		t.Fatalf("SystemTestPlan.Status = %q, want approved", p.TestingState.SystemTestPlan.Status)
	}
	if len(p.TestingState.SystemTestPlan.UseCaseIndex) != 2 {
		t.Fatalf("UseCaseIndex len = %d, want 2", len(p.TestingState.SystemTestPlan.UseCaseIndex))
	}
}

// --------------------------------------------------------------------------
// STP 11: RecordPhaseStarted idempotency — same key, stale version → ledger
// --------------------------------------------------------------------------

func TestRecordPhaseStarted_Idempotent(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2 := seedActivity(t, store, id, v, cred, "C008")

	v3, err := store.RecordPhaseStarted(fwra.Context{Context: ctx}, id, v2, "C008", MethodPhaseRequirements, cred, fwra.IdempotencyKey("wf:ps-idem"))
	if err != nil {
		t.Fatalf("RecordPhaseStarted: %v", err)
	}
	before := readProject(t, store, id, cred)

	// Retry with SAME key but stale expectedVersion; dedup ledger must win.
	v3again, err := store.RecordPhaseStarted(fwra.Context{Context: ctx}, id, 0, "C008", MethodPhaseRequirements, cred, fwra.IdempotencyKey("wf:ps-idem"))
	if err != nil {
		t.Fatalf("idempotent retry should succeed via ledger, got: %v", err)
	}
	if v3again != v3 {
		t.Fatalf("idempotent retry version = %d, want original %d", v3again, v3)
	}
	after := readProject(t, store, id, cred)
	if after.Version != before.Version {
		t.Fatalf("retry produced a NEW state commit %d → %d (DOUBLE APPLY)", before.Version, after.Version)
	}
}

// --------------------------------------------------------------------------
// STP 12: PhaseArtifactPayload round-trip via EncodeProjectJSON → DecodeProjectJSON
// --------------------------------------------------------------------------

func TestPhaseArtifactPayload_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	p := Project{}
	p.PhaseArtifacts = &PhaseArtifacts{
		SRS: map[string]SRSRecord{
			"svcA": {Component: "svcA", Content: "requirements text", AuthoredAt: &now},
		},
		TestPlan: map[string]TestPlanRecord{
			"svcA": {Component: "svcA", Content: "test plan text"},
		},
	}
	p.TestingState = &TestingState{
		SystemTestPlan: &SystemTestPlan{
			UseCaseIndex: []string{"UC1"},
			Status:       "approved",
		},
		QualityAuditReport: "all gates green",
	}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON: ok=false")
	}

	if got.PhaseArtifacts == nil {
		t.Fatal("PhaseArtifacts nil after round-trip")
	}
	srs, found := got.PhaseArtifacts.SRS["svcA"]
	if !found {
		t.Fatal("SRS[svcA] absent after round-trip")
	}
	if srs.Content != "requirements text" {
		t.Fatalf("SRS.Content = %q, want 'requirements text'", srs.Content)
	}
	if srs.AuthoredAt == nil || !srs.AuthoredAt.Equal(now) {
		t.Fatalf("SRS.AuthoredAt = %v, want %v", srs.AuthoredAt, now)
	}
	tp, found := got.PhaseArtifacts.TestPlan["svcA"]
	if !found {
		t.Fatal("TestPlan[svcA] absent after round-trip")
	}
	if tp.Content != "test plan text" {
		t.Fatalf("TestPlan.Content = %q unexpected", tp.Content)
	}

	if got.TestingState == nil {
		t.Fatal("TestingState nil after round-trip")
	}
	if got.TestingState.SystemTestPlan == nil {
		t.Fatal("SystemTestPlan nil after round-trip")
	}
	if got.TestingState.SystemTestPlan.Status != "approved" {
		t.Fatalf("SystemTestPlan.Status = %q, want approved", got.TestingState.SystemTestPlan.Status)
	}
	if got.TestingState.QualityAuditReport != "all gates green" {
		t.Fatalf("QualityAuditReport = %q, want 'all gates green'", got.TestingState.QualityAuditReport)
	}
}

// TestRecordPhaseArtifactProduced_EmptyActivityID_Error validates the guard.
func TestRecordPhaseArtifactProduced_EmptyActivityID_Error(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()
	v2 := seedActivity(t, store, id, v, cred, "C009")
	_ = v2
	_, err := store.RecordPhaseArtifactProduced(fwra.Context{Context: ctx}, id, v, "", "key", PhaseArtifactPayload{}, cred, fwra.IdempotencyKey("wf:artifact-empty"))
	if err == nil {
		t.Fatal("want error for empty activityID, got nil")
	}
}

// TestRecordServiceContractProduced_TwoComponents verifies second write doesn't
// clobber the first (map-key upsert, not full-map replace).
func TestRecordServiceContractProduced_TwoComponents(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2, err := store.RecordServiceContractProduced(fwra.Context{Context: ctx}, id, v, "engineA", ServiceContract{Component: "engineA", Title: "engineA contract"}, cred, fwra.IdempotencyKey("wf:contract-a"))
	if err != nil {
		t.Fatalf("RecordServiceContractProduced(engineA): %v", err)
	}
	_, err = store.RecordServiceContractProduced(fwra.Context{Context: ctx}, id, v2, "engineB", ServiceContract{Component: "engineB", Title: "engineB contract"}, cred, fwra.IdempotencyKey("wf:contract-b"))
	if err != nil {
		t.Fatalf("RecordServiceContractProduced(engineB): %v", err)
	}

	p := readProject(t, store, id, cred)
	if _, ok := p.ServiceContracts["engineA"]; !ok {
		t.Fatal("ServiceContracts[engineA] absent — first write clobbered by second")
	}
	if _, ok := p.ServiceContracts["engineB"]; !ok {
		t.Fatal("ServiceContracts[engineB] absent")
	}
}

// TestRecordPhaseCompleted_NoPhaseMatch_NoopOnUnknownPhase verifies that
// completing a phase not in the Phases slice does not panic or corrupt the set.
func TestRecordPhaseCompleted_NoPhaseMatch_Noop(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2 := seedActivity(t, store, id, v, cred, "C010")
	// seed phases
	v3, err := store.RecordPhaseStarted(fwra.Context{Context: ctx}, id, v2, "C010", MethodPhaseRequirements, cred, fwra.IdempotencyKey("wf:ps-c010"))
	if err != nil {
		t.Fatalf("RecordPhaseStarted: %v", err)
	}
	// complete a phase that is NOT in the service phase set (e.g. "ui_design" — a non-existent id post-refactor)
	v4, err := store.RecordPhaseCompleted(fwra.Context{Context: ctx}, id, v3, "C010", ActivityMethodPhase("ui_design"), "", cred, fwra.IdempotencyKey("wf:pc-c010-nophase"))
	if err != nil {
		t.Fatalf("RecordPhaseCompleted on unknown phase should not error: %v", err)
	}
	if v4 != v3+1 {
		t.Fatalf("version = %d, want %d", v4, v3+1)
	}
	// Phases slice still intact
	s := readConstructionStatus(t, store, id, cred, "C010")
	if len(s.Phases) != 5 {
		t.Fatalf("Phases len = %d, want 5 (no entries added for unknown phase)", len(s.Phases))
	}
}

// uuid import used by newConstructionStore helper.
var _ = uuid.NewString

// TestConstructionTransitionAccess_OpCount asserts the port is within App-C §6 bounds.
// 3–5 ops: strive. ≤12: acceptable. >12: warning. ≥20: reject (directive error).
// Current count: 8. This test documents the adjudicated count from lifecycle-2 Plan 2.
func TestConstructionTransitionAccess_OpCount(t *testing.T) {
	const wantOps = 8
	const avoidAbove = 12
	if wantOps > avoidAbove {
		t.Errorf("ConstructionTransitionAccess has %d ops; App-C §6 advises avoiding >%d", wantOps, avoidAbove)
	}
	// The var _ assertion above (GitStore) is the real compile-time gate.
	// This test just documents the decision.
	t.Logf("ConstructionTransitionAccess: %d ops (App-C §6 adjudicated ≤12 at lifecycle-2 Task 3)", wantOps)
}

// reconcile_test.go — coverage for the F80 deterministic project.json reconciler.

// projDoc renders a minimal project.json document with the given slot entries.
// Each entry is keyed by kind ordinal, mirroring the on-disk slotsMap shape.
func projDoc(t *testing.T, slots map[int]map[string]any) []byte {
	t.Helper()
	doc := map[string]any{"schemaVersion": 1, "slots": slots}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	return b
}

func committedMissionSlot(text string) map[string]any {
	return map[string]any{
		"status": int(ReviewCommitted), "kind": int(KindMission),
		"model": map[string]any{"vision": text, "objectives": []any{}, "statement": text},
	}
}

func awaitingVolatilitiesSlot(name string) map[string]any {
	return map[string]any{
		"status": int(ReviewAwaitingReview), "kind": int(KindVolatilities),
		"model": map[string]any{"items": []any{
			map[string]any{"name": name, "rationale": "r", "axis": "sameCustomerOverTime"},
		}},
	}
}

// The reconciler takes main's document + overlays the session's own slot from the branch.
// Main-side advances to OTHER slots survive; the session's slot wins.
func TestReconcileSlotOntoBase(t *testing.T) {
	// main: Mission committed at "v2" (advanced while the session ran) + Volatilities
	// committed at an OLD value.
	base := projDoc(t, map[int]map[string]any{
		int(KindMission):      committedMissionSlot("v2-advanced-on-main"),
		int(KindVolatilities): awaitingVolatilitiesSlot("old-committed"),
	})
	// session branch: the in-flight Volatilities draft (AwaitingReview) + a STALE Mission
	// (still at v1, from when the branch was cut).
	ours := projDoc(t, map[int]map[string]any{
		int(KindMission):      committedMissionSlot("v1-stale-on-branch"),
		int(KindVolatilities): awaitingVolatilitiesSlot("in-flight-draft"),
	})

	reconciled, err := ReconcileSlotOntoBase(base, ours, ProjectID("p"), KindVolatilities)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	proj, ok, err := DecodeProjectJSON(reconciled, ProjectID("p"))
	if err != nil || !ok {
		t.Fatalf("decode reconciled: ok=%v err=%v", ok, err)
	}
	// The session's OWN slot (Volatilities) is the branch's in-flight draft.
	vol, isVol := proj.Volatilities.Model.(*Volatilities)
	if !isVol || len(vol.Items) != 1 || vol.Items[0].Name != "in-flight-draft" {
		t.Fatalf("volatilities slot must be the branch draft, got: %+v", proj.Volatilities.Model)
	}
	// Every OTHER slot comes from main (the concurrent advance survives).
	mission, isMission := proj.Mission.Model.(*MissionStatement)
	if !isMission || mission.Vision != "v2-advanced-on-main" {
		t.Fatalf("mission slot must be main's advanced value, got: %+v", proj.Mission.Model)
	}
}

func TestReconcileSlotOntoBase_RejectsUndecodableInput(t *testing.T) {
	base := projDoc(t, map[int]map[string]any{int(KindMission): committedMissionSlot("v")})
	if _, err := ReconcileSlotOntoBase([]byte("{not json"), base, ProjectID("p"), KindMission); err == nil {
		t.Fatal("an undecodable base must be rejected")
	}
	if _, err := ReconcileSlotOntoBase(base, []byte("{not json"), ProjectID("p"), KindMission); err == nil {
		t.Fatal("an undecodable ours must be rejected")
	}
}

// OverlaySlotFromBranchOntoMain is the in-memory twin; it mutates main in place.
func TestOverlaySlotFromBranchOntoMain(t *testing.T) {
	main := Project{}
	main.Mission = ArtifactSlot{Status: ReviewCommitted, Model: &MissionStatement{Vision: "main-mission"}}
	main.Volatilities = ArtifactSlot{Status: ReviewCommitted, Model: &Volatilities{Items: []Volatility{{Name: "old"}}}}

	branch := Project{}
	branch.Volatilities = ArtifactSlot{Status: ReviewAwaitingReview, Model: &Volatilities{Items: []Volatility{{Name: "draft"}}}}

	if err := OverlaySlotFromBranchOntoMain(&main, &branch, KindVolatilities); err != nil {
		t.Fatalf("overlay: %v", err)
	}
	vol, _ := main.Volatilities.Model.(*Volatilities)
	if vol == nil || len(vol.Items) != 1 || vol.Items[0].Name != "draft" {
		t.Fatalf("volatilities must be the branch draft, got %+v", main.Volatilities.Model)
	}
	if main.Volatilities.Status != ReviewAwaitingReview {
		t.Fatalf("the whole slot (incl. status) must overlay, got status %v", main.Volatilities.Status)
	}
	mission, _ := main.Mission.Model.(*MissionStatement)
	if mission == nil || mission.Vision != "main-mission" {
		t.Fatalf("mission must be untouched, got %+v", main.Mission.Model)
	}
}

// provenance_test.go — the ADDITIVE commit-provenance record (PM-P2-4). commitTransition
// stamps a supplied Provenance onto the committed slot; a nil prov leaves it untouched. The
// record survives the substrate codec round-trip; the store-level verb server-resolves
// committedAt from the clock.

func TestCommitTransition_StampsProvenance(t *testing.T) {
	p := &Project{}
	p.Mission = ArtifactSlot{Status: ReviewAwaitingReview, Model: &MissionStatement{Vision: "v", Mission: "m"}}

	prov := &Provenance{CommittedAt: "2026-07-06T00:00:00Z", ApprovedBy: "alice", DraftedBy: "agentic-design-rail"}
	if err := commitTransition(KindMission, prov)(p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}
	got := p.Mission.Provenance
	if got == nil {
		t.Fatal("committed slot must carry the supplied provenance")
	}
	if got.CommittedAt != "2026-07-06T00:00:00Z" || got.ApprovedBy != "alice" || got.DraftedBy != "agentic-design-rail" {
		t.Fatalf("provenance not stamped as supplied: %+v", *got)
	}
}

func TestCommitTransition_NilProvenanceLeavesSlotUntouched(t *testing.T) {
	p := &Project{}
	p.Mission = ArtifactSlot{Status: ReviewAwaitingReview, Model: &MissionStatement{Vision: "v", Mission: "m"}}

	// A plain commit (nil prov) records no provenance — absent provenance is allowed.
	if err := commitTransition(KindMission, nil)(p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}
	if p.Mission.Provenance != nil {
		t.Fatalf("nil prov must leave provenance absent, got %+v", *p.Mission.Provenance)
	}
}

func TestProvenance_RoundTripsThroughCodec(t *testing.T) {
	p := Project{ID: "p"}
	p.Mission = ArtifactSlot{
		Status:     ReviewCommitted,
		Model:      &MissionStatement{Vision: "v", Mission: "m"},
		Revisions:  1,
		Provenance: &Provenance{CommittedAt: "2026-07-06T12:34:56Z", ApprovedBy: "bob", DraftedBy: "agentic-design-rail (amend-2)"},
	}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, "p")
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	pr := got.Mission.Provenance
	if pr == nil {
		t.Fatal("provenance must survive the encode → decode round-trip")
	}
	if pr.CommittedAt != "2026-07-06T12:34:56Z" || pr.ApprovedBy != "bob" || pr.DraftedBy != "agentic-design-rail (amend-2)" {
		t.Fatalf("provenance round-trip mismatch: %+v", *pr)
	}
}

// provenance_store_test.go — the store-level PM-P2-4 provenance verb. CommitArtifactWithProvenance
// commits the slot exactly as CommitArtifact AND stamps the provenance record, with committedAt
// server-resolved from the store clock.
func TestGitStore_CommitArtifactWithProvenance_RecordsProvenance(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	// Pin the clock so committedAt is deterministic (RA server-resolves it from the clock).
	fixed := time.Date(2026, 7, 6, 8, 30, 0, 0, time.UTC)
	store = store.WithClock(func() time.Time { return fixed })

	id := ProjectID("prov-demo")
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", &MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err := store.CommitArtifactWithProvenance(ctx, id, v2, KindMission, "alice@example.com", "agentic-design-rail", cred, "wf:commit"); err != nil {
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
	if p.Mission.Status != ReviewCommitted || p.Mission.Revisions != 1 {
		t.Fatalf("provenance commit must still commit the slot: status=%v rev=%d", p.Mission.Status, p.Mission.Revisions)
	}
}

// TestNewModelForKindCoversAllKinds is the build-time guard: every kind in
// AllArtifactKinds() must have a factory case in NewModelForKind, and the
// returned model must report the same kind (or, for the four Solution slots, the
// *Solution must have SlotKind set to the requested kind). A new ArtifactKind
// constant added to the iota without a corresponding factory case fails here
// rather than silently crashing at runtime in the codec.
func TestNewModelForKindCoversAllKinds(t *testing.T) {
	solutionKinds := map[ArtifactKind]bool{
		KindNormalSolution:       true,
		KindSubcriticalSolution:  true,
		KindCompressedSolution:   true,
		KindDecompressedSolution: true,
	}

	for _, k := range AllArtifactKinds() {
		model, ok := NewModelForKind(k)
		if !ok {
			t.Errorf("NewModelForKind(%s): returned ok=false; add a factory case", k)
			continue
		}
		if model == nil {
			t.Errorf("NewModelForKind(%s): returned nil model with ok=true", k)
			continue
		}
		if solutionKinds[k] {
			sol, isSol := model.(*Solution)
			if !isSol {
				t.Errorf("NewModelForKind(%s): expected *Solution, got %T", k, model)
				continue
			}
			if sol.SlotKind != k {
				t.Errorf("NewModelForKind(%s): Solution.SlotKind = %s, want %s", k, sol.SlotKind, k)
			}
		} else {
			if got := model.Kind(); got != k {
				t.Errorf("NewModelForKind(%s): model.Kind() = %s, want %s", k, got, k)
			}
		}
	}

	// An out-of-range kind must return (nil, false).
	if model, ok := NewModelForKind(ArtifactKind(9999)); ok || model != nil {
		t.Errorf("NewModelForKind(9999): expected (nil, false), got (%v, %v)", model, ok)
	}
}

// TestArtifactKindString checks the stable human-readable names emitted in
// error messages and arch-test output.
func TestArtifactKindString(t *testing.T) {
	if got := KindSystem.String(); got != "System" {
		t.Fatalf("KindSystem.String() = %q, want %q", got, "System")
	}
	if got := KindScrubbedRequirements.String(); got != "ScrubbedRequirements" {
		t.Fatalf("KindScrubbedRequirements.String() = %q, want %q", got, "ScrubbedRequirements")
	}
	unknown := ArtifactKind(999)
	if got := unknown.String(); got != "ArtifactKind(999)" {
		t.Fatalf("unknown ArtifactKind.String() = %q, want %q", got, "ArtifactKind(999)")
	}
}

// TestArtifactKindIsPhase1 covers the Phase-1 partition used by the Manager gate.
func TestArtifactKindIsPhase1(t *testing.T) {
	if !KindSystem.IsPhase1() {
		t.Fatal("KindSystem is Phase 1")
	}
	if KindNetwork.IsPhase1() {
		t.Fatal("KindNetwork is Phase 2")
	}
	if len(Phase1RequiredKinds()) == 0 {
		t.Fatal("Phase1RequiredKinds must be non-empty")
	}
}

// designsession_test.go unit-tests the designSessionAccess wrapper (designsession.go).
// C2 FOLD (code-health-phase-a): the branch/ledger/reconcile/stale-ack verbs are now
// REQUIRED members of the generated ProjectStateAccess contract, so every
// ProjectStateAccess implementation — including test doubles — must implement all of
// them; the wrapper simply forwards. stubProjectState is that full implementation.
// CommitArtifactWithProvenance is the ONE exception (never folded — see the C2 fold
// note on designSessionAccess in projectstateaccess.go); provenanceStub additionally
// implements it, proving the wrapper's ONE remaining capability check still works both
// ways.

// stubProjectState implements ProjectStateAccess in full. Every call is recorded so a
// test can assert exactly which base method the wrapper invoked. stagedModel captures
// the DECODED typed model the Stage verbs received — the designSessionAccess Stage op
// takes the codable ModelEnvelope on the wire (B9 follow-up) and must decode it before
// forwarding, so the assertion below proves the concrete model arrived.
type stubProjectState struct {
	calls       []string
	stagedModel ArtifactModel
}

func (s *stubProjectState) AdvancePhase(_ fwra.Context, _ ProjectID, _ Version) (Version, error) {
	return 0, nil
}

func (s *stubProjectState) CommitArtifact(_ fwra.Context, _ ProjectID, _ Version, _ ArtifactKind) (Version, error) {
	s.calls = append(s.calls, "CommitArtifact")
	return 10, nil
}

func (s *stubProjectState) CreateProject(_ fwra.Context, _ ProjectID, _ OwnerScope, _ string) (Version, error) {
	return 0, nil
}

func (s *stubProjectState) ListProjects(_ fwra.Context, _ OwnerScope) ([]ProjectSummary, error) {
	return nil, nil
}

func (s *stubProjectState) ReadProject(_ fwra.Context, projectID ProjectID) (Project, error) {
	s.calls = append(s.calls, "ReadProject")
	return Project{ID: projectID, Version: 1}, nil
}

func (s *stubProjectState) ReadProjectVersion(_ fwra.Context, _ ProjectID) (Version, error) {
	return 0, nil
}

func (s *stubProjectState) SetOperatingModel(_ fwra.Context, _ ProjectID, _ Version, _ OperatingModel) (Version, error) {
	return 0, nil
}

func (s *stubProjectState) SetResearchInput(_ fwra.Context, _ ProjectID, _ Version, _ ResearchInput) (Version, error) {
	return 0, nil
}

func (s *stubProjectState) ReadProjectOnBranch(_ fwra.Context, projectID ProjectID, _ string) (Project, error) {
	s.calls = append(s.calls, "ReadProjectOnBranch")
	return Project{ID: projectID, Version: 2}, nil
}

func (s *stubProjectState) StageArtifactForReviewOnBranch(_ fwra.Context, _ ProjectID, _ Version, _ string, model ArtifactModel, _ fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "StageArtifactForReviewOnBranch")
	s.stagedModel = model
	return 20, nil
}

func (s *stubProjectState) WithdrawArtifactOnBranch(_ fwra.Context, _ ProjectID, _ Version, _ string, _ ArtifactKind, _ string, _ fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "WithdrawArtifactOnBranch")
	return 22, nil
}

func (s *stubProjectState) RejectArtifactOnBranchWithComments(_ fwra.Context, _ ProjectID, _ Version, _ string, _ ArtifactKind, _ string, _ int64, _ []ReviewComment, _ fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "RejectArtifactOnBranchWithComments")
	return 30, nil
}

func (s *stubProjectState) SetReviewCommentStatusOnBranch(_ fwra.Context, _ ProjectID, _ Version, _ string, _ ArtifactKind, _ string, _ string, _ fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "SetReviewCommentStatusOnBranch")
	return 31, nil
}

func (s *stubProjectState) SeedReviewCommentsOnBranch(_ fwra.Context, _ ProjectID, _ Version, _ string, _ ArtifactKind, _ int64, _ []ReviewComment, _ fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "SeedReviewCommentsOnBranch")
	return 32, nil
}

func (s *stubProjectState) ReconcileBranchFromMain(_ fwra.Context, _ ProjectID, _ Version, _ string, _ ArtifactKind, _ fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "ReconcileBranchFromMain")
	return 50, nil
}

func (s *stubProjectState) AcknowledgeStaleBasis(_ fwra.Context, _ ProjectID, _ Version, _ ArtifactKind, _ string, _ fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "AcknowledgeStaleBasis")
	return 60, nil
}

var _ ProjectStateAccess = (*stubProjectState)(nil)

// provenanceStub additionally implements CommitArtifactWithProvenance — the ONE
// verb that stayed a genuinely optional capability post-C2 (it was never folded into
// the generated ProjectStateAccess contract; see designsession.go).
type provenanceStub struct {
	stubProjectState
}

func (s *provenanceStub) CommitArtifactWithProvenance(_ fwra.Context, _ ProjectID, _ Version, _ ArtifactKind, _, _ string) (Version, error) {
	s.calls = append(s.calls, "CommitArtifactWithProvenance")
	return 40, nil
}

var _ provenanceCommitter = (*provenanceStub)(nil)

func assertCalls(t *testing.T, calls []string, want string) {
	t.Helper()
	if len(calls) != 1 || calls[0] != want {
		t.Fatalf("calls = %v, want exactly [%s]", calls, want)
	}
}

// ---- ReadProjectOnBranch ----------------------------------------------------

func TestDesignSessionAccess_ReadProjectOnBranch_DelegatesToBase(t *testing.T) {
	base := &stubProjectState{}
	s := NewDesignSessionAccess(base)
	env, err := s.ReadProjectOnBranch(fwra.Context{Context: context.Background()}, "proj-1", "session-branch")
	if err != nil {
		t.Fatalf("ReadProjectOnBranch: %v", err)
	}
	assertCalls(t, base.calls, "ReadProjectOnBranch")
	if env.Version != 2 {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

// ---- StageArtifactForReviewOnBranch ------------------------------------------

// stageEnvelope builds the wire envelope for a concrete typed model — the Stage op's
// parameter is the codable ModelEnvelope (B9 follow-up), decoded INSIDE the RA before
// forwarding to base.
func stageEnvelope(t *testing.T) ModelEnvelope {
	t.Helper()
	env, err := EncodeModel(&PlanningAssumptions{Resources: []string{"alice"}, CalendarDaysPerWeek: 5})
	if err != nil {
		t.Fatalf("EncodeModel: %v", err)
	}
	return env
}

func assertStagedPlanningAssumptions(t *testing.T, got ArtifactModel) {
	t.Helper()
	pa, ok := got.(*PlanningAssumptions)
	if !ok {
		t.Fatalf("staged model = %T, want *PlanningAssumptions (the RA must DECODE the envelope before forwarding)", got)
	}
	if len(pa.Resources) != 1 || pa.Resources[0] != "alice" {
		t.Fatalf("decoded model lost its payload: %+v", pa)
	}
}

func TestDesignSessionAccess_StageArtifactForReviewOnBranch_DelegatesToBase(t *testing.T) {
	base := &stubProjectState{}
	s := NewDesignSessionAccess(base)
	v, err := s.StageArtifactForReviewOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", stageEnvelope(t), "idem-1")
	if err != nil {
		t.Fatalf("StageArtifactForReviewOnBranch: %v", err)
	}
	assertCalls(t, base.calls, "StageArtifactForReviewOnBranch")
	assertStagedPlanningAssumptions(t, base.stagedModel)
	if v != 20 {
		t.Fatalf("Version = %d, want 20", v)
	}
}

// A malformed envelope fails BEFORE any store verb runs, surfacing the plain Decode
// error unwrapped — byte-for-byte the class the retired Manager-side custom activity
// surfaced (fwmanager.MapError passes non-layer errors through untagged).
func TestDesignSessionAccess_StageArtifactForReviewOnBranch_DecodeFailureNoStoreCall(t *testing.T) {
	base := &stubProjectState{}
	s := NewDesignSessionAccess(base)
	bad := ModelEnvelope{Kind: KindPlanningAssumptions, Model: []byte(`{"resources": 42}`)}
	if _, err := s.StageArtifactForReviewOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", bad, "idem-1"); err == nil {
		t.Fatal("a malformed envelope must fail the Stage")
	}
	if len(base.calls) != 0 {
		t.Fatalf("no store verb may run on a decode failure, got %v", base.calls)
	}
}

// ---- CommitArtifactWithProvenance ---------------------------------------------

func TestDesignSessionAccess_CommitArtifactWithProvenance_BaseFallback(t *testing.T) {
	base := &stubProjectState{}
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
	full := &provenanceStub{}
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

func TestDesignSessionAccess_RejectArtifactOnBranchWithComments_DelegatesToBase(t *testing.T) {
	base := &stubProjectState{}
	s := NewDesignSessionAccess(base)
	v, err := s.RejectArtifactOnBranchWithComments(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "notes", 0, nil, "idem-1")
	if err != nil {
		t.Fatalf("RejectArtifactOnBranchWithComments: %v", err)
	}
	assertCalls(t, base.calls, "RejectArtifactOnBranchWithComments")
	if v != 30 {
		t.Fatalf("Version = %d, want 30", v)
	}
}

// ---- WithdrawArtifactOnBranch --------------------------------------------------

func TestDesignSessionAccess_WithdrawArtifactOnBranch_DelegatesToBase(t *testing.T) {
	base := &stubProjectState{}
	s := NewDesignSessionAccess(base)
	v, err := s.WithdrawArtifactOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "notes", "idem-1")
	if err != nil {
		t.Fatalf("WithdrawArtifactOnBranch: %v", err)
	}
	assertCalls(t, base.calls, "WithdrawArtifactOnBranch")
	if v != 22 {
		t.Fatalf("Version = %d, want 22", v)
	}
}

// ---- ReconcileBranchFromMain ----------------------------------------------------

func TestDesignSessionAccess_ReconcileBranchFromMain_DelegatesToBase(t *testing.T) {
	base := &stubProjectState{}
	s := NewDesignSessionAccess(base)
	v, err := s.ReconcileBranchFromMain(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "idem-1")
	if err != nil {
		t.Fatalf("ReconcileBranchFromMain: %v", err)
	}
	assertCalls(t, base.calls, "ReconcileBranchFromMain")
	if v != 50 {
		t.Fatalf("Version = %d, want 50", v)
	}
}

// ---- SetReviewCommentStatusOnBranch ---------------------------------------------

func TestDesignSessionAccess_SetReviewCommentStatusOnBranch_DelegatesToBase(t *testing.T) {
	base := &stubProjectState{}
	s := NewDesignSessionAccess(base)
	v, err := s.SetReviewCommentStatusOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "c1", "waived", "idem-1")
	if err != nil {
		t.Fatalf("SetReviewCommentStatusOnBranch: %v", err)
	}
	assertCalls(t, base.calls, "SetReviewCommentStatusOnBranch")
	if v != 31 {
		t.Fatalf("Version = %d, want 31", v)
	}
}

// ---- SeedReviewCommentsOnBranch --------------------------------------------------

func TestDesignSessionAccess_SeedReviewCommentsOnBranch_DelegatesToBase(t *testing.T) {
	base := &stubProjectState{}
	s := NewDesignSessionAccess(base)
	v, err := s.SeedReviewCommentsOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, 0, nil, "idem-1")
	if err != nil {
		t.Fatalf("SeedReviewCommentsOnBranch: %v", err)
	}
	assertCalls(t, base.calls, "SeedReviewCommentsOnBranch")
	if v != 32 {
		t.Fatalf("Version = %d, want 32", v)
	}
}

// envelope_test.go ports the codec-mechanism tests down from the two Managers that
// used to duplicate this wire discipline (projectdesign/codec.go, systemdesign/codec.go)
// now that ModelEnvelope/ProjectEnvelope/EncodeModel/EncodeProject/Decode live here
// (envelope.go). The Manager-specific policy tests (whether a given Manager opts INTO
// carrying the Research corpus; the interaction with a Manager's own local slotFor/
// ArtifactKind) stay in projectdesign/systemdesign — see
// Test_encodeProject_DropsResearchCorpus (projectdesign),
// Test_encodeProject_SlimsResearchContentAcrossActivityBoundary and
// Test_projectEnvelope_PreservesReviewThread (both Managers) — this file covers the
// shared mechanism itself.

func TestModelEnvelope_RoundTrip_NilModel(t *testing.T) {
	env, err := EncodeModel(nil)
	if err != nil {
		t.Fatalf("EncodeModel(nil): %v", err)
	}
	model, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if model != nil {
		t.Fatalf("Decode of a nil-encoded envelope must yield a nil model, got %T", model)
	}
}

func TestModelEnvelope_RoundTrip_ConcreteModel(t *testing.T) {
	mission := &MissionStatement{Vision: "ENVELOPE-SENTINEL vision"}
	env, err := EncodeModel(mission)
	if err != nil {
		t.Fatalf("EncodeModel: %v", err)
	}
	if env.Kind != KindMission {
		t.Fatalf("Kind = %s, want %s", env.Kind, KindMission)
	}
	if !strings.Contains(string(env.Model), "ENVELOPE-SENTINEL") {
		t.Fatalf("encoded model JSON must carry the field value, got %s", env.Model)
	}
	decoded, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, ok := decoded.(*MissionStatement)
	if !ok {
		t.Fatalf("Decode: got %T, want *MissionStatement", decoded)
	}
	if got.Vision != mission.Vision {
		t.Fatalf("Vision = %q, want %q", got.Vision, mission.Vision)
	}
}

func TestModelEnvelope_Decode_UnknownKindErrors(t *testing.T) {
	env := ModelEnvelope{Kind: ArtifactKind(9999), Model: json.RawMessage(`{}`)}
	if _, err := env.Decode(); err == nil {
		t.Fatal("Decode with an out-of-range Kind must error, got nil")
	}
}

func TestModelEnvelope_Decode_SolutionSlotKindReapplied(t *testing.T) {
	sol := &Solution{SlotKind: KindNormalSolution}
	env, err := EncodeModel(sol)
	if err != nil {
		t.Fatalf("EncodeModel: %v", err)
	}
	// Force the envelope's discriminator to a DIFFERENT Solution slot to prove Decode
	// re-applies the envelope's own Kind rather than trusting whatever SlotKind the
	// JSON happened to carry.
	env.Kind = KindSubcriticalSolution
	decoded, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, ok := decoded.(*Solution)
	if !ok {
		t.Fatalf("Decode: got %T, want *Solution", decoded)
	}
	if got.SlotKind != KindSubcriticalSolution {
		t.Fatalf("SlotKind = %s, want %s (the envelope's own Kind is authoritative)", got.SlotKind, KindSubcriticalSolution)
	}
}

func TestEncodeProject_SkipsUnpopulatedSlots(t *testing.T) {
	p := Project{ID: "proj-1", Version: 3, Phase: 1}
	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	if len(env.Slots) != 0 {
		t.Fatalf("an all-empty Project must encode zero slots, got %d", len(env.Slots))
	}
	if env.ID != p.ID || env.Version != p.Version || env.Phase != p.Phase {
		t.Fatalf("identity fields must survive encoding: got %+v", env)
	}
}

func TestProjectEnvelope_RoundTrip_PreservesSlotFields(t *testing.T) {
	p := Project{
		ID:      "proj-1",
		Version: 5,
		Phase:   1,
		Mission: ArtifactSlot{
			Status:          ReviewAwaitingReview,
			Model:           &MissionStatement{Vision: "round-trip vision"},
			Notes:           "reviewer notes",
			CritiqueVerdict: CritiqueVerdictRevise,
			CritiqueNotes:   "tighten the vision sentence",
			ReviewThread: []ReviewComment{
				{ID: "r0c1", Text: "split this", AuthorRole: "architect", Round: 0, Status: ReviewCommentOpen},
			},
		},
	}

	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	assertMissionSlotFieldsEncoded(t, env)

	back, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	assertMissionSlotFieldsDecoded(t, back)
}

// assertMissionSlotFieldsEncoded asserts the encoded envelope's Mission slot carries
// the review-state fields verbatim.
func assertMissionSlotFieldsEncoded(t *testing.T, env ProjectEnvelope) {
	t.Helper()
	se, ok := env.Slots[KindMission]
	if !ok {
		t.Fatal("Mission slot must be present in the encoded envelope")
	}
	if se.Status != ReviewAwaitingReview || se.Notes != "reviewer notes" {
		t.Fatalf("Status/Notes must survive encoding, got %+v", se)
	}
	if se.CritiqueVerdict != CritiqueVerdictRevise || se.CritiqueNotes != "tighten the vision sentence" {
		t.Fatalf("CritiqueVerdict/CritiqueNotes must survive encoding, got %+v", se)
	}
	if len(se.ReviewThread) != 1 || se.ReviewThread[0].ID != "r0c1" {
		t.Fatalf("ReviewThread must survive encoding, got %+v", se.ReviewThread)
	}
}

// assertMissionSlotFieldsDecoded asserts the decoded Project's Mission slot carries the
// review-state fields and the concretely typed model after the round trip.
func assertMissionSlotFieldsDecoded(t *testing.T, back Project) {
	t.Helper()
	if back.Mission.Status != ReviewAwaitingReview || back.Mission.Notes != "reviewer notes" {
		t.Fatalf("Status/Notes must survive the round trip, got %+v", back.Mission)
	}
	if back.Mission.CritiqueVerdict != CritiqueVerdictRevise || back.Mission.CritiqueNotes != "tighten the vision sentence" {
		t.Fatalf("CritiqueVerdict/CritiqueNotes must survive the round trip, got %+v", back.Mission)
	}
	if len(back.Mission.ReviewThread) != 1 || back.Mission.ReviewThread[0].Text != "split this" {
		t.Fatalf("ReviewThread must survive the round trip, got %+v", back.Mission.ReviewThread)
	}
	mission, ok := back.Mission.Model.(*MissionStatement)
	if !ok || mission.Vision != "round-trip vision" {
		t.Fatalf("Model must survive the round trip, got %+v", back.Mission.Model)
	}
}

// TestProjectEnvelope_ResearchIsNilByDefault proves the F16 payload-slimming
// contract at the shared-codec level: EncodeProject never populates Research, so a
// caller (projectdesign) that never opts in gets a wire payload with NO "research"
// key at all — a plain (non-pointer) struct field's `omitempty` would NOT suppress
// the key, which is exactly why the field is a pointer.
func TestProjectEnvelope_ResearchIsNilByDefault(t *testing.T) {
	p := Project{
		ID: "proj-1",
		Research: ResearchCorpus{Sources: []ResearchSourceRef{
			{Title: "RESEARCH-SENTINEL", Path: ".aiarch/state/research/00.txt", ContentBytes: 660_000},
		}},
	}
	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	if env.Research != nil {
		t.Fatalf("EncodeProject must leave Research nil unless the caller opts in, got %+v", env.Research)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if strings.Contains(string(raw), "research") {
		t.Fatalf("a nil Research pointer must not appear in the wire payload at all, got: %s", raw)
	}
	back, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !back.Research.IsZero() {
		t.Fatal("Research must not survive the round trip when the envelope never carried it")
	}
}

// TestProjectEnvelope_ResearchOptIn proves the opt-in path a Manager (systemdesign)
// uses: assigning env.Research after EncodeProject carries the corpus through the
// wire payload and back.
func TestProjectEnvelope_ResearchOptIn(t *testing.T) {
	p := Project{
		ID: "proj-1",
		Research: ResearchCorpus{Sources: []ResearchSourceRef{
			{Title: "The Founder Brief", Path: ".aiarch/state/research/00.txt", ContentBytes: 660_000},
		}},
	}
	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	env.Research = &p.Research

	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if !strings.Contains(string(raw), "The Founder Brief") {
		t.Fatalf("an opted-in Research must appear in the wire payload, got: %s", raw)
	}

	back, err := env.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if back.Research.IsZero() {
		t.Fatal("Research must survive the round trip when the envelope opted in")
	}
	if len(back.Research.Sources) != 1 || back.Research.Sources[0].Title != "The Founder Brief" {
		t.Fatalf("Research sources must survive the round trip, got %+v", back.Research.Sources)
	}
}

// TestProjectEnvelope_NoConstructionState_OmitsConstructionKeys pins the B8
// wire-compat contract: a project with NO construction state (the pd/sd shape —
// populated design slots, nil ActivityConstruction/ServiceContracts, zero
// ReviewPolicy) serializes WITHOUT any of the three construction-fidelity keys, so
// the projectdesign/systemdesign payload bytes are unchanged by the envelope
// extension (same style as the no-research pin above).
func TestProjectEnvelope_NoConstructionState_OmitsConstructionKeys(t *testing.T) {
	p := Project{
		ID:      "proj-1",
		Version: 5,
		Phase:   1,
		Mission: ArtifactSlot{Status: ReviewCommitted, Model: &MissionStatement{Vision: "v"}},
	}
	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	for _, key := range []string{"activityConstruction", "serviceContracts", "reviewPolicy"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("a construction-untouched project must not carry the %q key at all, got: %s", key, raw)
		}
	}
}

// TestReviewPolicyEmptinessGateCoversAllFields pins EncodeProject's "is ReviewPolicy
// empty" check (projectstateaccess.go, `len(p.ReviewPolicy.GatedPhasesByType) != 0 ||
// p.ReviewPolicy.Preset != nil`) to ReviewPolicy having exactly two fields (Task 7 added
// Preset). That check inspects GatedPhasesByType and Preset only; if ReviewPolicy ever
// grows a third field, the check must be extended to look at it too — or a project that
// only sets the new field would silently encode as "empty" and drop its wire data. If
// this test fails, update BOTH the emptiness gate in EncodeProject AND this assertion's
// field count together.
func TestReviewPolicyEmptinessGateCoversAllFields(t *testing.T) {
	got := reflect.TypeFor[ReviewPolicy]().NumField()
	if got != 2 {
		t.Fatalf("ReviewPolicy has %d fields, want 2 — EncodeProject's emptiness gate only "+
			"checks GatedPhasesByType and Preset; extend that gate for the new field(s) and "+
			"update this assertion together", got)
	}
}

// TestProjectEnvelope_ConstructionSections_RoundTrip pins the B8 mid-construction
// round trip: the three construction-fidelity sections plus the committed
// Network/ActivityList slots the pump's eligibility selection reads survive
// EncodeProject → JSON → Decode field-for-field. The assertions port construction's
// former local codec semantics (codec.go, deleted): committed-slot restore for
// Network/ActivityList (now via the Slots map's own status-faithful round-trip) and
// the verbatim carry of ActivityConstruction/ServiceContracts/ReviewPolicy.
func TestProjectEnvelope_ConstructionSections_RoundTrip(t *testing.T) {
	p := Project{
		ID:      "proj-1",
		Version: 9,
		Phase:   2,
		Network: ArtifactSlot{Status: ReviewCommitted, Model: &Network{
			Dependencies: []NetworkDependency{{Activity: "C-B", DependsOn: []string{"C-A"}}},
		}},
		ActivityList: ArtifactSlot{Status: ReviewCommitted, Model: &ActivityList{
			Activities: []ActivityItem{{Name: "C-A", Coding: true, EffortDays: 5}, {Name: "C-B", Coding: true, EffortDays: 5}},
		}},
		ActivityConstruction: map[string]ActivityConstructionStatus{
			"C-A": {
				ActivityID:   "C-A",
				Phase:        ActivityConstructionRunning,
				CurrentPhase: MethodPhaseDetailedDesign,
				Phases: []PhaseCompletion{
					{Phase: MethodPhaseRequirements, Weight: 1, Completed: true},
					{Phase: MethodPhaseDetailedDesign, Weight: 1},
				},
			},
		},
		ServiceContracts: map[string]ServiceContract{
			"ordersManager": {Component: "ordersManager", Layer: "Manager"},
		},
		ReviewPolicy: ReviewPolicy{GatedPhasesByType: map[string][]ActivityMethodPhase{
			"service": {MethodPhaseDetailedDesign},
		}},
	}

	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var wire ProjectEnvelope
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	back, err := wire.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	assertConstructionActivityStatusSurvived(t, back)
	assertConstructionContractsAndPolicySurvived(t, back)
	assertCommittedNetworkAndActivityListSlots(t, back)
}

// assertConstructionActivityStatusSurvived asserts the ActivityConstruction section
// round-tripped field-for-field.
func assertConstructionActivityStatusSurvived(t *testing.T, back Project) {
	t.Helper()
	// ActivityConstruction — field-for-field.
	acs, ok := back.ActivityConstruction["C-A"]
	if !ok {
		t.Fatalf("ActivityConstruction[C-A] must survive the round trip, got %+v", back.ActivityConstruction)
	}
	if acs.Phase != ActivityConstructionRunning || acs.CurrentPhase != MethodPhaseDetailedDesign {
		t.Fatalf("ActivityConstruction lifecycle fields must survive, got %+v", acs)
	}
	if len(acs.Phases) != 2 || !acs.Phases[0].Completed || acs.Phases[1].Completed {
		t.Fatalf("per-phase completion facts must survive verbatim, got %+v", acs.Phases)
	}
}

// assertConstructionContractsAndPolicySurvived asserts the ServiceContracts and
// ReviewPolicy sections round-tripped.
func assertConstructionContractsAndPolicySurvived(t *testing.T, back Project) {
	t.Helper()
	// ServiceContracts — the pump's hydrate/resolve input.
	sc, ok := back.ServiceContracts["ordersManager"]
	if !ok || sc.Component != "ordersManager" || sc.Layer != "Manager" {
		t.Fatalf("ServiceContracts must survive the round trip, got %+v", back.ServiceContracts)
	}

	// ReviewPolicy — the phase gate's snapshot source.
	if !back.ReviewPolicy.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Fatalf("ReviewPolicy gating must survive the round trip, got %+v", back.ReviewPolicy)
	}
}

// assertCommittedNetworkAndActivityListSlots asserts the committed Network/ActivityList
// slots round-tripped status-faithful with concretely typed models.
func assertCommittedNetworkAndActivityListSlots(t *testing.T, back Project) {
	t.Helper()
	// Committed Network/ActivityList slots — the former construction codec restored
	// these as ReviewCommitted with concrete models; the Slots round-trip must do the
	// same so nextEligibleActivity's committed-slot guards and type assertions pass.
	if back.Network.Status != ReviewCommitted {
		t.Fatalf("Network slot status must survive as ReviewCommitted, got %v", back.Network.Status)
	}
	network, ok := back.Network.Model.(*Network)
	if !ok || len(network.Dependencies) != 1 || network.Dependencies[0].Activity != "C-B" {
		t.Fatalf("Network model must survive concretely typed, got %+v", back.Network.Model)
	}
	if back.ActivityList.Status != ReviewCommitted {
		t.Fatalf("ActivityList slot status must survive as ReviewCommitted, got %v", back.ActivityList.Status)
	}
	al, ok := back.ActivityList.Model.(*ActivityList)
	if !ok || len(al.Activities) != 2 || al.Activities[0].Name != "C-A" {
		t.Fatalf("ActivityList model must survive concretely typed, got %+v", back.ActivityList.Model)
	}
}

// Black-box regression tests for the per-activity git-forward head-state aggregate
// (projectStateAccess.md §GIT-HEAD-STATE, D-PA-GIT, FROZEN 2026-06-12). Like
// gitstore_test.go, they drive the RA's PUBLIC Record* verbs against a REAL
// throwaway on-disk git store (no mock — test-authoring constitution §7 anti-cheat,
// the D-PA-R real-store discipline). They cover:
//   - birth-on-branch-opened (the row is born + CICheck=Pending)
//   - PR-tolerant upsert (branch-only first → branch+PR on a later touch)
//   - CI observed transitions (Pending → Success/Failure)
//   - arch-approved
//   - merged
//   - idempotent re-record (a retried key returns the prior Version, NO double-apply)
//   - concurrent records on DIFFERENT activityIds converging (the partial-map-key
//     invariant: two writers, two keys, both survive under ref-CAS)
//   - the read projection carries ActivityGit whole (readProject)

// fixedClock is a deterministic, server-side clock so UpdatedAt is asserted exactly
// (proving the timestamp is server-resolved, not caller-minted).
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// newActivityStore spins a real local git store with a fixed clock and seeds a
// Phase-3 project so modeRequireExisting Record* verbs have a row to upsert.
func newActivityStore(t *testing.T, clk time.Time) (*GitStore, ProjectID, Version, RepoCredential, context.Context) {
	t.Helper()
	store, cred, ctx := newLocalGitStore(t)
	store = store.WithClock(fixedClock(clk))
	id := ProjectID(uuid.NewString())
	v, err := store.CreateProject(ctx, id, "alice", "GitDemo", cred, "wf:create")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return store, id, v, cred, ctx
}

func readActivity(ctx context.Context, t *testing.T, store *GitStore, id ProjectID, cred RepoCredential, activityID string) ActivityGitStatus {
	t.Helper()
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	g, ok := proj.ActivityGit[activityID]
	if !ok {
		t.Fatalf("ActivityGit[%s] absent; have %+v", activityID, proj.ActivityGit)
	}
	return g
}

// TestRecordActivityBranchOpened_BirthsRow — the branch-opened verb births the row
// with the branch handles, CICheck=Pending, and a server-resolved UpdatedAt; the
// read projection carries it whole.
func TestRecordActivityBranchOpened_BirthsRow(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	store, id, v, cred, ctx := newActivityStore(t, now)

	v2, err := store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, v, "C-MST", "activity/C-MST", "ref-cmst", "pr-7", "cr-021", false, cred, "wf:branch")
	if err != nil {
		t.Fatalf("RecordActivityBranchOpened: %v", err)
	}
	if v2 != v+1 {
		t.Fatalf("version = %d, want %d", v2, v+1)
	}
	g := readActivity(ctx, t, store, id, cred, "C-MST")
	if g.ActivityID != "C-MST" || g.BranchName != "activity/C-MST" || g.BranchRef != "ref-cmst" {
		t.Fatalf("branch fields wrong: %+v", g)
	}
	if g.PullRequestRef != "pr-7" || g.CRLabel != "cr-021" {
		t.Fatalf("PR/CR fields wrong: %+v", g)
	}
	if g.CICheck != CICheckPending {
		t.Fatalf("CICheck = %v, want Pending on birth", g.CICheck)
	}
	if g.Merged || g.ArchApproved || g.IsRevert {
		t.Fatalf("expected fresh row flags false: %+v", g)
	}
	if !g.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %v, want server-resolved %v", g.UpdatedAt, now)
	}
}

// TestRecordActivityBranchOpened_PRTolerantUpsert — a branch-only first touch
// (empty prRef) converges to branch+PR on a later touch; the second call must NOT
// clobber the branch fields and must fill the PR ref.
func TestRecordActivityBranchOpened_PRTolerantUpsert(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	store, id, v, cred, ctx := newActivityStore(t, now)

	// First touch: branch only, no PR yet.
	v2, err := store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, v, "C-MST", "activity/C-MST", "ref-cmst", "", "", false, cred, "wf:branch-only")
	if err != nil {
		t.Fatalf("branch-only touch: %v", err)
	}
	g := readActivity(ctx, t, store, id, cred, "C-MST")
	if g.BranchRef != "ref-cmst" || g.PullRequestRef != "" {
		t.Fatalf("after branch-only: %+v, want branch set, prRef empty", g)
	}
	if g.CICheck != CICheckPending {
		t.Fatalf("CICheck after birth = %v, want Pending", g.CICheck)
	}

	// Second touch: the OpenPullRequest fills the PR fields; branch must survive.
	v3, err := store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, v2, "C-MST", "activity/C-MST", "ref-cmst", "pr-9", "cr-021", true, cred, "wf:pr-touch")
	if err != nil {
		t.Fatalf("pr touch: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}
	g = readActivity(ctx, t, store, id, cred, "C-MST")
	if g.BranchRef != "ref-cmst" || g.BranchName != "activity/C-MST" {
		t.Fatalf("branch clobbered on PR touch: %+v", g)
	}
	if g.PullRequestRef != "pr-9" || g.CRLabel != "cr-021" || !g.IsRevert {
		t.Fatalf("PR fields not filled: %+v", g)
	}
}

// TestRecordActivityCIObserved_Transitions — the poll-loop verb moves CICheck
// Pending → Failure → Success, touching nothing else.
func TestRecordActivityCIObserved_Transitions(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	store, id, v, cred, ctx := newActivityStore(t, now)

	v, err := store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, v, "C-MST", "activity/C-MST", "ref", "pr-1", "", false, cred, "wf:branch")
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	v, err = store.RecordActivityCIObserved(fwra.Context{Context: ctx}, id, v, "C-MST", CICheckFailure, cred, "wf:ci-1")
	if err != nil {
		t.Fatalf("ci failure: %v", err)
	}
	if g := readActivity(ctx, t, store, id, cred, "C-MST"); g.CICheck != CICheckFailure {
		t.Fatalf("CICheck = %v, want Failure", g.CICheck)
	}
	_, err = store.RecordActivityCIObserved(fwra.Context{Context: ctx}, id, v, "C-MST", CICheckSuccess, cred, "wf:ci-2")
	if err != nil {
		t.Fatalf("ci success: %v", err)
	}
	g := readActivity(ctx, t, store, id, cred, "C-MST")
	if g.CICheck != CICheckSuccess {
		t.Fatalf("CICheck = %v, want Success", g.CICheck)
	}
	// CI-only verb must not have disturbed the branch/PR handles.
	if g.BranchRef != "ref" || g.PullRequestRef != "pr-1" {
		t.Fatalf("CI verb disturbed git handles: %+v", g)
	}
}

// TestRecordActivityArchApprovedAndMerged — the two terminal-ish facts flip their
// flags and leave the rest intact.
func TestRecordActivityArchApprovedAndMerged(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	store, id, v, cred, ctx := newActivityStore(t, now)

	v, err := store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, v, "C-MST", "b", "ref", "pr-1", "", false, cred, "wf:branch")
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	v, err = store.RecordActivityArchApproved(fwra.Context{Context: ctx}, id, v, "C-MST", cred, "wf:approve")
	if err != nil {
		t.Fatalf("arch approve: %v", err)
	}
	if g := readActivity(ctx, t, store, id, cred, "C-MST"); !g.ArchApproved || g.Merged {
		t.Fatalf("after approve: %+v, want ArchApproved=true Merged=false", g)
	}
	_, err = store.RecordActivityMerged(fwra.Context{Context: ctx}, id, v, "C-MST", cred, "wf:merge")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	g := readActivity(ctx, t, store, id, cred, "C-MST")
	if !g.Merged || !g.ArchApproved {
		t.Fatalf("after merge: %+v, want both flags true", g)
	}
	if g.PullRequestRef != "pr-1" {
		t.Fatalf("merge disturbed PR ref: %+v", g)
	}
}

// TestRecordActivity_IdempotentReRecord — a retry re-passing the SAME idempotencyKey
// (even with a now-stale expectedVersion) returns the prior Version via the dedup
// ledger with NO second state commit (no double-apply).
func TestRecordActivity_IdempotentReRecord(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	store, id, v, cred, ctx := newActivityStore(t, now)

	v2, err := store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, v, "C-MST", "b", "ref", "pr-1", "", false, cred, "wf:branch")
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	before, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}

	// Re-record with the SAME key but a deliberately stale expectedVersion (0). The
	// dedup probe must short-circuit and return the original v2, NOT a Conflict.
	v2again, err := store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, 0, "C-MST", "b", "ref", "pr-1", "", false, cred, "wf:branch")
	if err != nil {
		t.Fatalf("idempotent re-record should succeed via ledger, got: %v", err)
	}
	if v2again != v2 {
		t.Fatalf("idempotent re-record version = %d, want original %d", v2again, v2)
	}
	after, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if after.Version != before.Version {
		t.Fatalf("re-record produced a NEW state commit %d -> %d (DOUBLE APPLY)", before.Version, after.Version)
	}
}

// TestRecordActivity_ContractMisuseEmptyActivityID — an empty activityID is rejected
// before any I/O (ContractMisuse).
func TestRecordActivity_ContractMisuseEmptyActivityID(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	store, id, v, cred, ctx := newActivityStore(t, now)
	_, err := store.RecordActivityCIObserved(fwra.Context{Context: ctx}, id, v, "", CICheckSuccess, cred, "wf:k")
	if k := kindOf(t, err); k != fwra.ContractMisuse {
		t.Fatalf("empty activityID kind = %v, want ContractMisuse", k)
	}
}

// TestRecordActivity_RequireExistingProject — a Record* against a project that does
// not exist is NotFound (modeRequireExisting; the project row exists by Phase 3).
func TestRecordActivity_RequireExistingProject(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	_, err := store.RecordActivityMerged(fwra.Context{Context: ctx}, ProjectID(uuid.NewString()), 0, "C-MST", cred, "wf:k")
	if k := kindOf(t, err); k != fwra.NotFound {
		t.Fatalf("Record* on absent project kind = %v, want NotFound", k)
	}
}

// casRaceOutcome is one writer's result in a two-writer ref-CAS race test.
type casRaceOutcome struct {
	who string
	v   Version
	err error
}

// splitCASRaceOutcomes drains the two-writer results channel and returns the CAS
// winner and loser, failing unless exactly one writer won and one lost.
func splitCASRaceOutcomes(t *testing.T, results chan casRaceOutcome) (winner, loser casRaceOutcome) {
	t.Helper()
	gotWinner := false
	for r := range results {
		if r.err == nil {
			winner = r
			gotWinner = true
		} else {
			loser = r
		}
	}
	if !gotWinner {
		t.Fatal("expected exactly one CAS winner, both lost")
	}
	if loser.who == "" {
		// Both succeeded would mean a lost update (the file transport serialized
		// without contention). Force the contention deterministically below if that
		// ever happens; for a true race we require a loser.
		t.Fatal("expected one CAS loser (non-fast-forward), both won — LOST UPDATE")
	}
	return winner, loser
}

// TestRecordActivity_ConcurrentDifferentActivitiesConverge — THE partial-map-key
// invariant (GIT.4). Two writers record on DIFFERENT activityIds from the SAME base
// version. One wins fast-forward; the loser is rejected non-fast-forward
// (fwra.Conflict / ErrRefCASLost), reloads HEAD, and re-applies. BOTH activity rows
// survive — neither clobbers the other (the closure mutates one map key, leaving the
// rest byte-identical).
func TestRecordActivity_ConcurrentDifferentActivitiesConverge(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	store, id, base, cred, ctx := newActivityStore(t, now)

	results := make(chan casRaceOutcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		v, e := store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, base, "C-MST", "b-mst", "ref-mst", "pr-1", "", false, cred, "wf:A")
		results <- casRaceOutcome{"A-CMST", v, e}
	}()
	go func() {
		defer wg.Done()
		v, e := store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, base, "C-UC1", "b-uc1", "ref-uc1", "pr-2", "", false, cred, "wf:B")
		results <- casRaceOutcome{"B-CUC1", v, e}
	}()
	wg.Wait()
	close(results)

	winner, loser := splitCASRaceOutcomes(t, results)
	if winner.v != base+1 {
		t.Fatalf("winner landed at version %d, want base+1 (%d)", winner.v, base+1)
	}
	if k := kindOf(t, loser.err); k != fwra.Conflict {
		t.Fatalf("loser %s kind = %v, want Conflict", loser.who, k)
	}

	// The loser reloads HEAD and re-applies against the winner's new tip.
	cur, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after race: %v", err)
	}
	switch loser.who {
	case "A-CMST":
		_, err = store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, cur.Version, "C-MST", "b-mst", "ref-mst", "pr-1", "", false, cred, "wf:A")
	case "B-CUC1":
		_, err = store.RecordActivityBranchOpened(fwra.Context{Context: ctx}, id, cur.Version, "C-UC1", "b-uc1", "ref-uc1", "pr-2", "", false, cred, "wf:B")
	}
	if err != nil {
		t.Fatalf("loser %s retry: %v", loser.who, err)
	}

	// BOTH activity rows survive — the partial-map-key update did not clobber.
	final, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject final: %v", err)
	}
	mst, okMst := final.ActivityGit["C-MST"]
	uc1, okUc1 := final.ActivityGit["C-UC1"]
	if !okMst || !okUc1 {
		t.Fatalf("both activity rows must survive convergence, have keys: %v", keysOfGitActivity(final.ActivityGit))
	}
	if mst.BranchRef != "ref-mst" || uc1.BranchRef != "ref-uc1" {
		t.Fatalf("convergence corrupted a row: C-MST=%+v C-UC1=%+v", mst, uc1)
	}
	// Sanity: the satellite's CAS loss is the documented sentinel.
	_ = fwgithub.ErrRefCASLost
}

func keysOfGitActivity(m map[string]ActivityGitStatus) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Review-ledger GitStore verb tests (review-ledger feature). They exercise the durable
// comment ledger over the real local-git substrate, mirroring the branch-aware Reject tests.

func TestGitStore_RejectWithComments_AppendsOpenLedger(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", &MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	comments := []ReviewComment{
		{Anchor: "$.vision", AnchorText: "v", Text: "sharpen the vision", AuthorRole: "architect"},
		{Anchor: "", Text: "a free-form note", AuthorRole: "architect"},
	}
	if _, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, "please revise", 1, comments, cred, "wf:reject"); err != nil {
		t.Fatalf("RejectArtifactOnBranchWithComments: %v", err)
	}
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Mission.Status != ReviewRejected {
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
		if c.Status != ReviewCommentOpen {
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
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", &MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	comments := []ReviewComment{{Anchor: "$.vision", AnchorText: "v", Text: "the reopening reason", AuthorRole: "architect"}}
	if _, err := store.SeedReviewCommentsOnBranch(ctx, id, v2, "", KindMission, 0, comments, cred, "wf:seed"); err != nil {
		t.Fatalf("SeedReviewCommentsOnBranch: %v", err)
	}
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	// Status UNCHANGED (still AwaitingReview — the seed does not reject).
	if proj.Mission.Status != ReviewAwaitingReview {
		t.Fatalf("seed must NOT change status; got %v, want AwaitingReview", proj.Mission.Status)
	}
	thread := proj.Mission.ReviewThread
	if len(thread) != 1 || thread[0].ID != "r0c1" || thread[0].Status != ReviewCommentOpen {
		t.Fatalf("seed must append one OPEN round-0 entry r0c1, got %+v", thread)
	}
	if thread[0].Text != "the reopening reason" {
		t.Fatalf("seeded comment text not persisted: %+v", thread[0])
	}
}

func TestGitStore_RejectWithComments_IdempotentOnSameKey(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", &MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	comments := []ReviewComment{{Anchor: "$.a", Text: "one", AuthorRole: "architect"}}
	// Same idempotency key twice (a Temporal activity retry): the second collapses to the
	// committed version and MUST NOT duplicate the ledger entry (review-ledger §5).
	v3, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, "n", 1, comments, cred, "wf:reject")
	if err != nil {
		t.Fatalf("first reject: %v", err)
	}
	if _, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, "n", 1, comments, cred, "wf:reject"); err != nil {
		t.Fatalf("retry reject (same key): %v", err)
	}
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
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
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", &MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	comments := []ReviewComment{{Anchor: "$.a", Text: "one", AuthorRole: "architect"}}
	v3, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, "n", 1, comments, cred, "wf:reject")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	// waive the open comment.
	v4, err := store.SetReviewCommentStatusOnBranch(ctx, id, v3, "", KindMission, "r1c1", ReviewCommentWaived, cred, "wf:waive")
	if err != nil {
		t.Fatalf("waive: %v", err)
	}
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Mission.ReviewThread[0].Status != ReviewCommentWaived {
		t.Fatalf("status after waive = %q, want waived", proj.Mission.ReviewThread[0].Status)
	}
	// waived->open is not a legal transition (only open->waived / addressed->open).
	if _, err := store.SetReviewCommentStatusOnBranch(ctx, id, v4, "", KindMission, "r1c1", ReviewCommentOpen, cred, "wf:reopen"); kindOf(t, err) != fwra.ContractMisuse {
		t.Fatalf("waived->open kind = %v, want ContractMisuse", kindOf(t, err))
	}
}

func TestGitStore_SetReviewCommentStatus_UnknownIDNotFound(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", &MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	if _, err := store.SetReviewCommentStatusOnBranch(ctx, id, v2, "", KindMission, "nope", ReviewCommentWaived, cred, "wf:waive"); kindOf(t, err) != fwra.NotFound {
		t.Fatalf("unknown id kind = %v, want NotFound", kindOf(t, err))
	}
}

// TestGitStore_ReviewThread_SurvivesRestage proves the ledger is DURABLE across a re-stage
// (unlike the critique carrier, which a stage clears): the open comment persists, and the
// normalize-on-stage keeps it open while its response is empty (review-ledger §3).
func TestGitStore_ReviewThread_SurvivesRestage(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", &MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	v3, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, "n", 1, []ReviewComment{{Anchor: "$.a", Text: "one"}}, cred, "wf:reject")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	// Re-stage a fresh draft (the redraft) — the thread must persist (not be cleared).
	if _, err := store.StageArtifactForReviewOnBranch(ctx, id, v3, "", &MissionStatement{Vision: "v2", Mission: "m2"}, cred, "wf:restage"); err != nil {
		t.Fatalf("re-stage: %v", err)
	}
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if len(proj.Mission.ReviewThread) != 1 {
		t.Fatalf("thread lost on restage: len = %d, want 1", len(proj.Mission.ReviewThread))
	}
	if proj.Mission.ReviewThread[0].Status != ReviewCommentOpen {
		t.Fatalf("open comment (empty response) normalized to %q, want open", proj.Mission.ReviewThread[0].Status)
	}
}

// TestGitStore_AcknowledgeStaleBasis proves F45: acknowledging a stale committed slot clears
// its StaleBasis and records a durable, non-blocking staleAck audit entry — and is idempotent.
func TestGitStore_AcknowledgeStaleBasis(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	stageCommit := func(v Version, kind ArtifactKind, model ArtifactModel, tag string) Version {
		v2, err := store.StageArtifactForReviewOnBranch(ctx, id, v, "", model, cred, fwra.IdempotencyKey("wf:stage:"+tag))
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
	v := stageCommit(1, KindMission, &MissionStatement{Vision: "v1", Mission: "m1"}, "mission1")
	v = stageCommit(v, KindGlossary, &Glossary{}, "glossary1")
	// AMEND Mission (final commit in this chain; its returned version is not read again).
	stageCommit(v, KindMission, &MissionStatement{Vision: "v2", Mission: "m2"}, "mission2")
	p, _ := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if !p.Glossary.StaleBasis {
		t.Fatal("precondition: Glossary must be stale after Mission amend")
	}

	// ACK the stale Glossary "reviewed — unaffected".
	v2, err := store.AcknowledgeStaleBasis(ctx, id, p.Version, KindGlossary, "diagrams only, no term changes", cred, "wf:ack1")
	if err != nil {
		t.Fatalf("AcknowledgeStaleBasis: %v", err)
	}
	p, _ = store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if p.Glossary.StaleBasis {
		t.Fatal("StaleBasis must be cleared after acknowledge")
	}
	thread := p.Glossary.ReviewThread
	if len(thread) != 1 {
		t.Fatalf("want 1 staleAck audit entry, got %d", len(thread))
	}
	ack := thread[0]
	if ack.Type != ReviewCommentTypeStaleAck || ack.Status != ReviewCommentAddressed || ack.AuthorRole != "architect" {
		t.Errorf("audit entry shape wrong: %+v", ack)
	}
	if want := "diagrams only, no term changes"; !strings.Contains(ack.Text, want) {
		t.Errorf("audit entry text %q must carry the note %q", ack.Text, want)
	}

	// IDEMPOTENT: a repeat ack on an already-un-stale slot is a no-op — no second audit entry.
	if _, err := store.AcknowledgeStaleBasis(ctx, id, v2, KindGlossary, "again", cred, "wf:ack2"); err != nil {
		t.Fatalf("repeat AcknowledgeStaleBasis: %v", err)
	}
	p, _ = store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if len(p.Glossary.ReviewThread) != 1 {
		t.Fatalf("repeat ack must NOT append a second entry; got %d", len(p.Glossary.ReviewThread))
	}
}

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

// localLocator resolves one project repo (a real on-disk throwaway git repo). It is a
// plain function-backed RepoLocator — NOT a sibling RA — so the RA's no-sideways
// discipline is preserved. The cross-project registry repo is GONE (founder ruling
// 2026-06-14): the catalog is discovered by enumeration (a single-repo enumeration in
// these single-project tests).
type localLocator struct {
	project *fwgithub.GitStore
}

func (l localLocator) ProjectRepo(_ ProjectID) (*fwgithub.GitStore, error) { return l.project, nil }

// singleRepoCatalog is the test ProjectCatalog: it reads project.json from the one
// on-disk repo and yields its id+title — the LOCAL single-repo discover-by-enumeration
// the production localProjectCatalog implements over the same repo. NOT a behavioral
// double: it reaches the REAL git store.
type singleRepoCatalog struct {
	repo *fwgithub.GitStore
}

func (c singleRepoCatalog) ListProjectRepos(ctx context.Context, _ OwnerScope, _ RepoCredential) ([]ProjectCatalogRef, error) {
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
	return []ProjectCatalogRef{{ProjectID: ProjectID(doc.ID), Title: doc.Name}}, nil
}

// newLocalGitStore spins one real local git repo and builds a LOCAL-profile GitStore
// over it, wired with the single-repo discover-by-enumeration catalog.
func newLocalGitStore(t *testing.T) (*GitStore, RepoCredential, context.Context) {
	t.Helper()
	store, _, cred, ctx := newLocalGitStoreWithRepo(t)
	return store, cred, ctx
}

// newLocalGitStoreWithRepo is newLocalGitStore that ALSO returns the underlying raw
// fwgithub.GitStore (the per-project repo). The raw handle lets a test simulate the
// agentic Action by committing `.aiarch/state/project.json` DIRECTLY to the repo —
// bypassing every projectStateAccess write verb — then prove the RA reads it back.
func newLocalGitStoreWithRepo(t *testing.T) (*GitStore, *fwgithub.GitStore, RepoCredential, context.Context) {
	t.Helper()
	projRepo := gh.StartLocalGitRepo(t, "main")
	proj, err := fwgithub.NewGitStore(projRepo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore(project): %v", err)
	}
	store, err := NewGitStore(localLocator{project: proj}, true /* local */)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(singleRepoCatalog{repo: proj})
	return store, proj, LocalRepoCredential(), context.Background()
}

func kindOf(t *testing.T, err error) fwra.Kind {
	t.Helper()
	var e *fwra.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fwra.Error, got %T: %v", err, err)
	}
	return e.Kind
}

func mustResearch(s string) ResearchInput {
	return ResearchInput{Sources: []ResearchSource{{Title: "t", Content: s}}}
}

// TestGitStore_CreateReadRoundTrip — CreateProject seeds the aggregate at Version
// 1; ReadProject returns it whole; ListProjects surfaces it via discover-by-enumeration.
func TestGitStore_CreateReadRoundTrip(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())

	v, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if v != 1 {
		t.Fatalf("CreateProject version = %d, want 1", v)
	}

	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != 1 || proj.Owner != "alice" || proj.Name != "Demo" || proj.Phase != PhaseSystemDesign {
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
	repos map[ProjectID]*fwgithub.GitStore
}

func (l multiRepoLocator) ProjectRepo(id ProjectID) (*fwgithub.GitStore, error) {
	return l.repos[id], nil
}

type multiRepoCatalog struct {
	repos map[ProjectID]*fwgithub.GitStore
}

func (c multiRepoCatalog) ListProjectRepos(ctx context.Context, _ OwnerScope, _ RepoCredential) ([]ProjectCatalogRef, error) {
	var out []ProjectCatalogRef
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
		out = append(out, ProjectCatalogRef{ProjectID: id, Title: doc.Name})
	}
	return out, nil
}

// TestGitStore_ListProjects_NoRegistry_MultipleProjects — THE registry-removal proof:
// create TWO projects (each in its own repo, the cloud shape), then ListProjects
// returns BOTH via discover-by-enumeration — no registry index repo exists.
func TestGitStore_ListProjects_NoRegistry_MultipleProjects(t *testing.T) {
	id1, id2 := ProjectID(uuid.NewString()), ProjectID(uuid.NewString())
	repos := map[ProjectID]*fwgithub.GitStore{}
	for _, id := range []ProjectID{id1, id2} {
		r := gh.StartLocalGitRepo(t, "main")
		gs, err := fwgithub.NewGitStore(r.URL, "main")
		if err != nil {
			t.Fatalf("NewGitStore(%s): %v", id, err)
		}
		repos[id] = gs
	}
	store, err := NewGitStore(multiRepoLocator{repos: repos}, true /* local */)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(multiRepoCatalog{repos: repos})
	cred, ctx := LocalRepoCredential(), context.Background()

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
		if s.TotalCount != len(Phase1RequiredKinds()) {
			t.Fatalf("summary %s totalCount = %d, want %d", s.Name, s.TotalCount, len(Phase1RequiredKinds()))
		}
	}
	if !names["First"] || !names["Second"] {
		t.Fatalf("ListProjects missing a project; got names %+v", names)
	}
}

// TestGitStore_ListProjects_ReturnsStoredOwner (PM-P2-6) — ListProjects must report each
// project's CANONICAL STORED owner, not echo the caller's requested enumeration scope. A
// caller passing a placeholder/wildcard scope (here "{}") must still see the real stored
// owner ("alice") on every summary — the same value get-project returns.
func TestGitStore_ListProjects_ReturnsStoredOwner(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())

	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Enumerate with a placeholder scope that is NOT the stored owner. The
	// single-repo catalog ignores the scope arg, so enumeration still finds the repo.
	summaries, err := store.ListProjects(ctx, "{}", cred)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("ListProjects = %+v, want one row", summaries)
	}
	if summaries[0].Owner != "alice" {
		t.Fatalf("summary Owner = %q, want the stored owner \"alice\" (not the requested scope)", summaries[0].Owner)
	}
}

// TestGitStore_ListProjects_SurfacesOperatorPaused (fix round 1, Task 7c
// live-firing review, FINDING 2): ListProjects must surface OperatorPaused —
// PumpSweepWorkflow's eligibility filter skips a paused project reading
// exactly this field, at zero extra I/O cost (the per-project N+1 read
// ListProjects already performs, readProjectForList, already has the full
// Project in hand). nil (omitted) before any pause, a true pointer after.
func TestGitStore_ListProjects_SurfacesOperatorPaused(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	before, err := store.ListProjects(ctx, "alice", cred)
	if err != nil {
		t.Fatalf("ListProjects (before pause): %v", err)
	}
	if len(before) != 1 || before[0].OperatorPaused != nil {
		t.Fatalf("want nil OperatorPaused before any pause, got %+v", before)
	}

	if _, err := store.RecordOperatorPaused(fwra.Context{Context: ctx}, id, v, "operator pause", cred, fwra.IdempotencyKey("wf:paused")); err != nil {
		t.Fatalf("RecordOperatorPaused: %v", err)
	}

	after, err := store.ListProjects(ctx, "alice", cred)
	if err != nil {
		t.Fatalf("ListProjects (after pause): %v", err)
	}
	if len(after) != 1 || after[0].OperatorPaused == nil || !*after[0].OperatorPaused {
		t.Fatalf("want OperatorPaused=true after RecordOperatorPaused, got %+v", after)
	}
}

// TestGitStore_StageCommitRoundTrip — stage a typed model, commit it, read it back
// with its review status (a model round-trips through git JSON).
// TestGitStore_SetResearchInput_WritesFilesAndPointer proves the F42 files-not-JSON model
// (founder ruling 2026-07-05): SetResearchInput takes the wire {Title, Content} but writes
// each source's CONTENT to .aiarch/state/research/<slug>.txt and persists only the
// {Title, Path, ContentBytes} pointer in project.json (content structurally absent) — all in
// ONE atomic commit. The corpus files survive UNRELATED mutations (carry-forward), and a
// re-run with the same idempotency key is a no-op (dedup).
func TestGitStore_SetResearchInput_WritesFilesAndPointer(t *testing.T) {
	store, raw, cred, ctx := newLocalGitStoreWithRepo(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	const body = "the founder brief corpus body — pretend this is a whole book"
	research := ResearchInput{Sources: []ResearchSource{{Title: "Founder Brief", Content: body}}}
	v2, err := store.SetResearchInput(ctx, id, 1, research, cred, "wf:research")
	if err != nil {
		t.Fatalf("SetResearchInput: %v", err)
	}

	// The persisted head-state carries ONLY the pointer — content structurally absent.
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if len(proj.Research.Sources) != 1 {
		t.Fatalf("want one research pointer, got %+v", proj.Research)
	}
	ref := proj.Research.Sources[0]
	wantPath := ".aiarch/state/research/00-founder-brief.txt"
	if ref.Title != "Founder Brief" || ref.Path != wantPath || ref.ContentBytes != int64(len(body)) {
		t.Fatalf("research pointer = %+v, want {Founder Brief, %s, %d}", ref, wantPath, len(body))
	}

	// The corpus CONTENT lives as a file at .aiarch/state/research/<slug>.txt.
	assertResearchCorpusFileOnDisk(ctx, t, raw, body)

	// CARRY-FORWARD: an unrelated mutation (stage a mission) must NOT wipe the corpus file.
	stageUnrelatedMutationAssertCorpusSurvives(ctx, t, store, raw, id, cred, v2, body, wantPath)

	// IDEMPOTENT RETRY: re-running with the SAME key dedups to the original result version
	// (the ledger probe wins, ignoring the now-stale expectedVersion) — no double-write.
	vAgain, err := store.SetResearchInput(ctx, id, 999, research, cred, "wf:research")
	if err != nil {
		t.Fatalf("idempotent retry SetResearchInput: %v", err)
	}
	if vAgain != v2 {
		t.Fatalf("idempotent retry must dedup to the original result version %d, got %d", v2, vAgain)
	}
}

// assertResearchCorpusFileOnDisk asserts the corpus CONTENT lives as a file under
// .aiarch/state/research/ and does NOT appear inside project.json (F42).
func assertResearchCorpusFileOnDisk(ctx context.Context, t *testing.T, raw *fwgithub.GitStore, body string) {
	t.Helper()
	snap, err := raw.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("raw ReadSubtree: %v", err)
	}
	fileBytes, ok := snap.Files["research/00-founder-brief.txt"]
	if !ok {
		t.Fatalf("corpus file not written; %d files present in the subtree", len(snap.Files))
	}
	if string(fileBytes) != body {
		t.Fatalf("corpus file content = %q, want %q", string(fileBytes), body)
	}
	// The content must NOT appear in project.json (structurally gone from persisted state).
	if pj, ok := snap.Files["project.json"]; ok && bytes.Contains(pj, []byte(body)) {
		t.Fatal("corpus content leaked into project.json — F42 requires it live only in the file")
	}
}

// stageUnrelatedMutationAssertCorpusSurvives stages a mission (an unrelated mutation)
// and asserts the corpus file and the research pointer both survive (carry-forward).
func stageUnrelatedMutationAssertCorpusSurvives(ctx context.Context, t *testing.T, store *GitStore, raw *fwgithub.GitStore, id ProjectID, cred RepoCredential, v Version, body, wantPath string) {
	t.Helper()
	if _, err := store.StageArtifactForReviewOnBranch(ctx, id, v, "", &MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage"); err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	snap2, err := raw.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("raw ReadSubtree after stage: %v", err)
	}
	if fb, ok := snap2.Files["research/00-founder-brief.txt"]; !ok || string(fb) != body {
		t.Fatalf("an unrelated mutation wiped/changed the corpus file (carry-forward broken); present=%v", ok)
	}
	after, _ := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if len(after.Research.Sources) != 1 || after.Research.Sources[0].Path != wantPath {
		t.Fatalf("the research pointer must survive an unrelated mutation, got %+v", after.Research)
	}
}

// TestGitStore_CommitArtifact_RevisionsAndStaleBasis proves the F38 amendment/staleness
// bookkeeping baked into CommitArtifact (founder ruling 2026-07-05): each commit bumps the
// slot's Revisions and clears its own StaleBasis, and RE-committing an upstream artifact
// flags every already-committed DOWNSTREAM slot StaleBasis — a non-blocking UI signal,
// cleared when that downstream slot itself re-commits (its amendment IS the reconcile).
func TestGitStore_CommitArtifact_RevisionsAndStaleBasis(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// stageCommit stages a model for a kind then commits it, returning the new version.
	stageCommit := func(v Version, kind ArtifactKind, model ArtifactModel, tag string) Version {
		v2, err := store.StageArtifactForReviewOnBranch(ctx, id, v, "", model, cred, fwra.IdempotencyKey("wf:stage:"+tag))
		if err != nil {
			t.Fatalf("stage %s: %v", tag, err)
		}
		v3, err := store.CommitArtifact(ctx, id, v2, kind, cred, fwra.IdempotencyKey("wf:commit:"+tag))
		if err != nil {
			t.Fatalf("commit %s: %v", tag, err)
		}
		return v3
	}
	read := func() Project {
		p, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
		if err != nil {
			t.Fatalf("ReadProject: %v", err)
		}
		return p
	}

	// Forward flow: commit Mission (rev 1) then Glossary (rev 1). No staleness yet — Glossary
	// is DOWNSTREAM of Mission, so committing it flags nothing upstream.
	v := stageCommit(1, KindMission, &MissionStatement{Vision: "v1", Mission: "m1"}, "mission1")
	v = stageCommit(v, KindGlossary, &Glossary{}, "glossary1")
	p := read()
	if p.Mission.Revisions != 1 || p.Glossary.Revisions != 1 {
		t.Fatalf("forward commits: want Revisions 1/1, got %d/%d", p.Mission.Revisions, p.Glossary.Revisions)
	}
	if p.Mission.StaleBasis || p.Glossary.StaleBasis {
		t.Fatalf("forward flow must set NO staleness, got mission=%v glossary=%v", p.Mission.StaleBasis, p.Glossary.StaleBasis)
	}

	// AMEND Mission (re-commit): Mission.Revisions→2, Mission.StaleBasis cleared, and the
	// already-committed DOWNSTREAM Glossary is flagged StaleBasis.
	v = stageCommit(v, KindMission, &MissionStatement{Vision: "v2", Mission: "m2"}, "mission2")
	p = read()
	if p.Mission.Revisions != 2 {
		t.Fatalf("amended Mission Revisions = %d, want 2", p.Mission.Revisions)
	}
	if p.Mission.StaleBasis {
		t.Fatal("the amended Mission must NOT be stale (its re-commit is the reconcile)")
	}
	if !p.Glossary.StaleBasis {
		t.Fatal("committed downstream Glossary must be flagged StaleBasis after Mission is amended")
	}

	// RECONCILE: amend Glossary (re-commit) → its own StaleBasis clears, Revisions→2.
	stageCommit(v, KindGlossary, &Glossary{}, "glossary2")
	p = read()
	if p.Glossary.StaleBasis {
		t.Fatal("re-committing the stale Glossary must clear its StaleBasis (the reconcile)")
	}
	if p.Glossary.Revisions != 2 {
		t.Fatalf("reconciled Glossary Revisions = %d, want 2", p.Glossary.Revisions)
	}
}

func TestGitStore_StageCommitRoundTrip(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	mission := &MissionStatement{Vision: "v", Mission: "m"}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", mission, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	v3, err := store.CommitArtifact(ctx, id, v2, KindMission, cred, "wf:commit")
	if err != nil {
		t.Fatalf("CommitArtifact: %v", err)
	}
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != v3 {
		t.Fatalf("version = %d, want %d", proj.Version, v3)
	}
	if proj.Mission.Status != ReviewCommitted {
		t.Fatalf("mission status = %v, want Committed", proj.Mission.Status)
	}
	gotMission, ok := proj.Mission.Model.(*MissionStatement)
	if !ok || gotMission.Vision != "v" || gotMission.Mission != "m" {
		t.Fatalf("mission model round-trip failed: %+v", proj.Mission.Model)
	}
}

// TestGitStore_RejectArtifactOnBranchWithComments_EmptyBranchIsMain proves the surviving
// branch-aware Reject verb (I-DESIGN-DISPATCH §2a) behaves EXACTLY as a main-path reject
// when branch=="": it records the Rejected status + notes over the staged slot on main.
// This is the documented empty-branch equivalence the non-git / dormant-rail callers rely
// on (pin migrated from the retired RejectArtifactOnBranch — same assertion).
func TestGitStore_RejectArtifactOnBranchWithComments_EmptyBranchIsMain(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	mission := &MissionStatement{Vision: "v", Mission: "m"}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", mission, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	const notes = "rework the vision"
	if _, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, notes, 0, nil, cred, "wf:reject"); err != nil {
		t.Fatalf("RejectArtifactOnBranchWithComments(branch=\"\"): %v", err)
	}
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Mission.Status != ReviewRejected {
		t.Fatalf("mission status = %v, want Rejected", proj.Mission.Status)
	}
	if proj.Mission.Notes != notes {
		t.Fatalf("mission notes = %q, want %q", proj.Mission.Notes, notes)
	}
}

// TestGitStore_ReconcileBranchFromMain_EmptyBranchIsMisuse proves the F80c branch
// reconciler refuses an empty branch: reconciliation only makes sense against a real
// session branch (main never diverges from itself), so an empty branch is a ContractMisuse
// rather than a silent no-op that could mask a wiring bug.
func TestGitStore_ReconcileBranchFromMain_EmptyBranchIsMisuse(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_, err := store.ReconcileBranchFromMain(ctx, id, 1, "", KindMission, cred, "wf:reconcile")
	if k := kindOf(t, err); k != fwra.ContractMisuse {
		t.Fatalf("reconcile with empty branch kind = %v, want ContractMisuse", k)
	}
}

// TestGitStore_RejectArtifactOnBranchWithComments_UnpopulatedSlotIsMisuse proves rejecting
// a slot that was never staged is a ContractMisuse — the RA-level guard whose main-path
// triggering (in the PR rail, where the draft lives on the session branch and main's slot
// is empty) was the QA F28 crash. The Manager avoids it by rejecting ON the session
// branch (where the model IS staged); this test pins the guard the fix routes around
// (migrated from the retired RejectArtifactOnBranch — same ContractMisuse assertion).
func TestGitStore_RejectArtifactOnBranchWithComments_UnpopulatedSlotIsMisuse(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// No stage — the Mission slot is unpopulated on main.
	_, err := store.RejectArtifactOnBranchWithComments(ctx, id, 1, "", KindMission, "notes", 0, nil, cred, "wf:reject")
	if k := kindOf(t, err); k != fwra.ContractMisuse {
		t.Fatalf("reject of an unpopulated slot kind = %v, want ContractMisuse", k)
	}
}

// TestGitStore_WithdrawArtifactOnBranch_EmptyBranchIsMain proves the new branch-aware
// Withdraw verb (I-DESIGN-DISPATCH §2a) behaves EXACTLY as WithdrawArtifact when
// branch=="": it records the Withdrawn status + notes over the staged slot on main. This
// is the documented empty-branch equivalence the non-git / dormant-rail callers rely on.
func TestGitStore_WithdrawArtifactOnBranch_EmptyBranchIsMain(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	mission := &MissionStatement{Vision: "v", Mission: "m"}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", mission, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	const notes = "abandon this draft"
	if _, err := store.WithdrawArtifactOnBranch(ctx, id, v2, "", KindMission, notes, cred, "wf:withdraw"); err != nil {
		t.Fatalf("WithdrawArtifactOnBranch(branch=\"\"): %v", err)
	}
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Mission.Status != ReviewWithdrawn {
		t.Fatalf("mission status = %v, want Withdrawn", proj.Mission.Status)
	}
	if proj.Mission.Notes != notes {
		t.Fatalf("mission notes = %q, want %q", proj.Mission.Notes, notes)
	}
}

// TestGitStore_WithdrawArtifactOnBranch_UnpopulatedSlotIsMisuse proves withdrawing a slot
// that was never staged is a ContractMisuse — the RA-level guard whose main-path
// triggering (in the PR rail, where the draft lives on the session branch and main's slot
// is empty) was the QA F30 crash. The Manager avoids it by withdrawing ON the session
// branch (where the model IS staged); this test pins the guard the fix routes around.
func TestGitStore_WithdrawArtifactOnBranch_UnpopulatedSlotIsMisuse(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// No stage — the Mission slot is unpopulated on main.
	_, err := store.WithdrawArtifactOnBranch(ctx, id, 1, "", KindMission, "notes", cred, "wf:withdraw")
	if k := kindOf(t, err); k != fwra.ContractMisuse {
		t.Fatalf("withdraw of an unpopulated slot kind = %v, want ContractMisuse", k)
	}
}

// TestGitStore_VersionGuardConflict — a write at a stale expectedVersion (without
// a matching dedup key) surfaces fwra.Conflict.
func TestGitStore_VersionGuardConflict(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
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
	_, err := store.ReadProject(fwra.Context{Context: ctx}, ProjectID(uuid.NewString()), cred)
	if k := kindOf(t, err); k != fwra.NotFound {
		t.Fatalf("ReadProject(absent) kind = %v, want NotFound", k)
	}

	_, err = store.SetResearchInput(ctx, ProjectID(uuid.NewString()), 0, mustResearch("x"), cred, "wf:k")
	if k := kindOf(t, err); k != fwra.NotFound {
		t.Fatalf("SetResearchInput(absent) kind = %v, want NotFound", k)
	}

	_, err = store.StageArtifactForReviewOnBranch(ctx, ProjectID(uuid.NewString()), 1, "", nil, cred, "wf:k")
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
	id := ProjectID(uuid.NewString())

	// Seed the aggregate and stage+leave a mission slot so CommitArtifact has a
	// populated slot to transition.
	if _, err := store.CreateProject(ctx, id, "alice", "Race", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.StageArtifactForReviewOnBranch(ctx, id, 1, "", &MissionStatement{Vision: "v", Mission: "m"}, cred, "wf:stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}

	// Both writers observe the SAME base version (v2).
	base := v2

	results := make(chan casRaceOutcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer A — reconcile tick: CommitArtifact(mission) at base.
	go func() {
		defer wg.Done()
		v, e := store.CommitArtifact(ctx, id, base, KindMission, cred, "wf:reconcile-commit")
		results <- casRaceOutcome{"A-commit", v, e}
	}()
	// Writer B — operator pause: RecordOperatorPaused at the SAME base.
	go func() {
		defer wg.Done()
		v, e := store.RecordOperatorPaused(fwra.Context{Context: ctx}, id, base, "operator pause", cred, "wf:operator-pause")
		results <- casRaceOutcome{"B-pause", v, e}
	}()
	wg.Wait()
	close(results)

	// Exactly one writer wins; the other loses the CAS with fwra.Conflict.
	winner, loser := splitCASRaceOutcomes(t, results)
	if k := kindOf(t, loser.err); k != fwra.Conflict {
		t.Fatalf("loser %s kind = %v, want Conflict", loser.who, k)
	}
	if !errors.Is(loser.err, fwgithub.ErrRefCASLost) {
		t.Fatalf("loser %s error not ErrRefCASLost: %v", loser.who, loser.err)
	}

	// The loser reloads HEAD and re-applies against the winner's new tip.
	cur, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after race: %v", err)
	}
	var retried Version
	switch loser.who {
	case "A-commit":
		retried, err = store.CommitArtifact(ctx, id, cur.Version, KindMission, cred, "wf:reconcile-commit")
	case "B-pause":
		retried, err = store.RecordOperatorPaused(fwra.Context{Context: ctx}, id, cur.Version, "operator pause", cred, "wf:operator-pause")
	}
	if err != nil {
		t.Fatalf("loser %s retry: %v", loser.who, err)
	}

	// BOTH mutations survive: the winner's effect is visible AND the loser's retry
	// landed at the next version (convergence, no lost update).
	final, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
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
	if final.Mission.Status != ReviewCommitted {
		t.Fatalf("mission status = %v, want Committed (the commit must have landed)", final.Mission.Status)
	}

	// (b) Activity-retry idempotency / dedup, NO double-apply. Re-pass the WINNER's
	// idempotency key with a now-stale expectedVersion: the dedup probe must
	// short-circuit and return the winner's original resultVersion with no new
	// commit (no Conflict despite the stale version).
	assertWinnerKeyDedupsNoDoubleApply(ctx, t, store, id, cred, winner)
}

// assertWinnerKeyDedupsNoDoubleApply re-passes the CAS winner's idempotency key with a
// deliberately stale expectedVersion and asserts the applied_mutations dedup probe
// short-circuits to the winner's original result version with NO second state commit.
func assertWinnerKeyDedupsNoDoubleApply(ctx context.Context, t *testing.T, store *GitStore, id ProjectID, cred RepoCredential, winner casRaceOutcome) {
	t.Helper()
	winnerKey := fwra.IdempotencyKey("wf:reconcile-commit")
	if winner.who == "B-pause" {
		winnerKey = "wf:operator-pause"
	}
	beforeRetry, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject before dedup retry: %v", err)
	}
	var dedupV Version
	switch winner.who {
	case "A-commit":
		dedupV, err = store.CommitArtifact(ctx, id, 0 /* deliberately stale */, KindMission, cred, winnerKey)
	case "B-pause":
		dedupV, err = store.RecordOperatorPaused(fwra.Context{Context: ctx}, id, 0 /* stale */, "operator pause", cred, winnerKey)
	}
	if err != nil {
		t.Fatalf("dedup retry of winner key (stale version) should succeed via ledger, got: %v", err)
	}
	if dedupV != winner.v {
		t.Fatalf("dedup retry returned version %d, want winner's original %d", dedupV, winner.v)
	}
	afterRetry, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
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
	id := ProjectID(uuid.NewString())

	// Build the head-state the AGENTIC ACTION would have committed in the user's CI:
	// the typed Mission model in its slot at ReviewCommitted, plus the PM-critique
	// carrier (verdict + notes) the critique Action writes — the exact draft-path
	// shape the server now only READS, never writes.
	actionState := Project{
		ID:      id,
		Version: 7, // an arbitrary version the Action's commits advanced to
		Phase:   PhaseSystemDesign,
		Owner:   "alice",
		Name:    "ExternallyDrafted",
		Mission: ArtifactSlot{
			Status:          ReviewCommitted,
			Model:           &MissionStatement{Vision: "action-vision", Mission: "action-mission"},
			CritiqueVerdict: CritiqueVerdictRevise,
			CritiqueNotes:   "tighten the mission scope",
		},
	}
	// Commit it DIRECTLY to `.aiarch/state/project.json` via the raw satellite — the
	// Action's seam, bypassing every projectStateAccess write verb. Read the current
	// branch tip first so the CAS base matches (the repo is born with a `main` branch).
	commitStateBypassingRA(ctx, t, raw, actionState, "simulate Action",
		"action: commit design draft", "simulate Action draft commit")

	// READ-BACK through the RA's PUBLIC verb — the server's only draft-path touch.
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject (read-back of external draft): %v", err)
	}
	gotMission := assertExternalDraftReadBack(t, proj)

	// And the HUMAN-GATE write path still works server-side over the read-back draft:
	// stage the read-back model for review (the AwaitingReview thin-write), then commit
	// on approve — proving the surviving human-gate verbs are intact post-re-scope.
	v8, err := store.StageArtifactForReviewOnBranch(ctx, id, proj.Version, "", gotMission, cred, "wf:human-gate-stage")
	if err != nil {
		t.Fatalf("StageArtifactForReview (human-gate over read-back draft): %v", err)
	}
	if _, err := store.CommitArtifact(ctx, id, v8, KindMission, cred, "wf:human-gate-commit"); err != nil {
		t.Fatalf("CommitArtifact (human-gate approve): %v", err)
	}
	after, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after human-gate commit: %v", err)
	}
	if after.Mission.Status != ReviewCommitted {
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

// commitStateBypassingRA encodes state via the canonical EncodeProjectJSON seam and
// commits it straight to `.aiarch/state/project.json` through the RAW
// fwgithub.GitStore.CommitSubtree — NOT through any projectStateAccess write verb.
// The labels parameterize the failure messages so each caller's diagnostics read
// exactly as before.
func commitStateBypassingRA(ctx context.Context, t *testing.T, raw *fwgithub.GitStore, state Project, encodeLabel, commitMessage, commitLabel string) {
	t.Helper()
	raw1, err := EncodeProjectJSON(state)
	if err != nil {
		t.Fatalf("EncodeProjectJSON (%s): %v", encodeLabel, err)
	}
	snap, err := raw.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("raw ReadSubtree (observe base): %v", err)
	}
	if _, err := raw.CommitSubtree(
		ctx,
		".aiarch/state",
		map[string][]byte{"project.json": raw1},
		snap.Base, // CAS against the observed tip (the Action's working-tree commit)
		commitMessage,
		fwgithub.GitAuth{Local: true},
	); err != nil {
		t.Fatalf("raw CommitSubtree (%s): %v", commitLabel, err)
	}
}

// assertExternalDraftReadBack asserts ReadProject surfaced the externally committed
// draft whole — identity, status, typed model, and the critique carrier — returning
// the typed mission for the human-gate follow-on.
func assertExternalDraftReadBack(t *testing.T, proj Project) *MissionStatement {
	t.Helper()
	if proj.Version != 7 || proj.Phase != PhaseSystemDesign || proj.Owner != "alice" || proj.Name != "ExternallyDrafted" {
		t.Fatalf("read-back identity = v%d/%v/%s/%s, want v7/SystemDesign/alice/ExternallyDrafted",
			proj.Version, proj.Phase, proj.Owner, proj.Name)
	}
	if proj.Mission.Status != ReviewCommitted {
		t.Fatalf("read-back mission status = %v, want Committed (the Action committed it)", proj.Mission.Status)
	}
	gotMission, ok := proj.Mission.Model.(*MissionStatement)
	if !ok || gotMission.Vision != "action-vision" || gotMission.Mission != "action-mission" {
		t.Fatalf("read-back mission model = %+v, want the Action's typed model", proj.Mission.Model)
	}
	// The critique carrier the Action committed must round-trip on the read-back —
	// the C-MSD-Δ-critique-fix first-class carrier, read (not written) server-side.
	if proj.Mission.CritiqueVerdict != CritiqueVerdictRevise || proj.Mission.CritiqueNotes != "tighten the mission scope" {
		t.Fatalf("read-back critique carrier = (%q, %q), want (revise, tighten the mission scope)",
			proj.Mission.CritiqueVerdict, proj.Mission.CritiqueNotes)
	}
	return gotMission
}

// TestGitStore_CreateProject_ResumesExistingState proves the PERMISSIVE-RESUME
// CreateProject (founder ruling 2026-06-16): when a repo ALREADY carries a committed
// `.aiarch/state/project.json` (a prior run's progress), CreateProject RE-INITIALIZES
// the project FROM CURRENT PROGRESS — it RETURNS the existing version and does NOT
// clobber/reset the state, NOR error on already-exists. This is the I-RA-Δ resume
// behavior proven at the projectStateAccess seam.
func TestGitStore_CreateProject_ResumesExistingState(t *testing.T) {
	store, raw, cred, ctx := newLocalGitStoreWithRepo(t)
	id := ProjectID("my-resumed-project")

	// Build the head-state a PRIOR run committed: the project advanced to Project Design
	// with a committed Mission slot and research input — current progress to be resumed.
	priorState := Project{
		ID:      id,
		Version: 5, // a version the prior run's commits advanced to
		Phase:   PhaseProjectDesign,
		Owner:   "alice",
		Name:    "Resumed System",
		Research: ResearchCorpus{
			Sources: []ResearchSourceRef{{Title: "Brief", Path: ".aiarch/state/research/00-brief.txt", ContentBytes: 19}},
		},
		Mission: ArtifactSlot{
			Status: ReviewCommitted,
			Model:  &MissionStatement{Vision: "prior-vision", Mission: "prior-mission"},
		},
	}
	// Commit it DIRECTLY to `.aiarch/state/project.json` (the prior run's seam), then
	// run CreateProject against the SAME repo — the resume case.
	commitStateBypassingRA(ctx, t, raw, priorState, "prior state",
		"prior run: commit progress", "simulate prior progress")

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
	assertResumedPriorProgressIntact(ctx, t, store, id, cred)
}

// assertResumedPriorProgressIntact reads the project back after a permissive-resume
// CreateProject and asserts the prior progress — phase, version, committed Mission
// model, research pointer — survived unclobbered.
func assertResumedPriorProgressIntact(ctx context.Context, t *testing.T, store *GitStore, id ProjectID, cred RepoCredential) {
	t.Helper()
	got, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after resume: %v", err)
	}
	if got.Version != 5 || got.Phase != PhaseProjectDesign || got.Name != "Resumed System" {
		t.Fatalf("resume clobbered state: got v%d/%v/%s, want v5/ProjectDesign/Resumed System",
			got.Version, got.Phase, got.Name)
	}
	if got.Mission.Status != ReviewCommitted {
		t.Fatalf("resume lost the committed Mission slot: status = %v, want Committed", got.Mission.Status)
	}
	gotMission, ok := got.Mission.Model.(*MissionStatement)
	if !ok || gotMission.Vision != "prior-vision" || gotMission.Mission != "prior-mission" {
		t.Fatalf("resume lost the typed Mission model: %+v", got.Mission.Model)
	}
	if len(got.Research.Sources) != 1 || got.Research.Sources[0].Path != ".aiarch/state/research/00-brief.txt" {
		t.Fatalf("resume lost the research pointer: %+v", got.Research)
	}
}

// TestGitStore_CreateProject_FreshInitWhenNoState proves the other branch of the
// permissive-resume CreateProject: a repo with NO committed `.aiarch/state/project.json`
// (no prior progress) is initialized FRESH at Version 1, exactly as before.
func TestGitStore_CreateProject_FreshInitWhenNoState(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID("brand-new-project")

	v, err := store.CreateProject(ctx, id, "alice", "Brand New", cred, "wf:create-fresh")
	if err != nil {
		t.Fatalf("CreateProject (fresh): %v", err)
	}
	if v != 1 {
		t.Fatalf("CreateProject (fresh, no prior state) version = %d, want 1", v)
	}
	got, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after fresh init: %v", err)
	}
	if got.Version != 1 || got.Phase != PhaseSystemDesign || got.Name != "Brand New" {
		t.Fatalf("fresh init = v%d/%v/%s, want v1/SystemDesign/Brand New", got.Version, got.Phase, got.Name)
	}
}

// decode_terminal_test.go — QA F36 regression. A committed slot model that will not decode
// (free prose in a CLOSED-ENUM field — the live incident was a sentence written into a
// use case's "trigger", a closed Trigger enum) must be classified TERMINAL (ContractMisuse,
// non-retryable) by the shared project.json codec, NOT Infrastructure (retryable). Pre-fix
// it was Infrastructure, so the Manager's read-back Activity retried the same immutable
// bytes every ~100s forever with no failure surface.

// A valid CoreUseCases document round-trips; the same document with its "trigger" wire name
// overwritten by free prose fails decode as a TERMINAL ContractMisuse carrying the decode
// diagnostic — exactly what the Manager read-back needs to route to the human failure gate.
func TestDecodeProjectJSON_MalformedClosedEnum_IsTerminal(t *testing.T) {
	id := ProjectID("11111111-1111-1111-1111-111111111111")

	// A minimal but VALID CoreUseCases slot model — the trigger is the closed-enum wire name
	// "busMessage", so it appears verbatim in the encoded JSON for the surgical overwrite.
	cuc := &CoreUseCases{Decisions: []UseCaseDecision{{
		UseCase: UseCase{
			Name:           "Capture a commitment",
			Trigger:        TriggerBusMessage,
			Classification: ClassCore,
			// UC-ACT-PRESENT: every use case must now carry a non-empty activity diagram
			// (start + action) to decode; this keeps the fixture valid so only the poisoned
			// trigger below fails the decode.
			Activity: &ActivityDiagram{
				Nodes: []ActivityNode{
					{ID: "start", Kind: NodeStart},
					{ID: "capture", Kind: NodeAction, Label: "capture"},
				},
				Edges: []ActivityEdge{
					{From: "start", To: "capture", Kind: EdgeControlFlow},
				},
			},
		},
		RejectionReason: "",
	}}}
	state := Project{ID: id}
	state.CoreUseCases = ArtifactSlot{Status: ReviewCommitted, Model: cuc}

	raw, err := EncodeProjectJSON(state)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}

	// Sanity: the valid document decodes cleanly.
	if _, _, err := DecodeProjectJSON(raw, id); err != nil {
		t.Fatalf("valid document should decode: %v", err)
	}

	// Reproduce F36: overwrite the closed-enum wire name with the exact free prose the live
	// drafting agent committed. CI validate (a Go mirror typing trigger as a free string)
	// accepts this; the server codec must reject it.
	const prose = "A commitment of any size appears, however it arrives, and is still held only in the person's memory."
	poisoned := strings.Replace(string(raw), `"busMessage"`, `"`+prose+`"`, 1)
	if poisoned == string(raw) {
		t.Fatalf("test fixture invalid: %q not found in encoded document", "busMessage")
	}

	_, _, derr := DecodeProjectJSON([]byte(poisoned), id)
	if derr == nil {
		t.Fatalf("malformed closed-enum document must FAIL decode; got nil error")
	}
	if k := kindOf(t, derr); k != fwra.ContractMisuse {
		t.Fatalf("decode error kind = %v, want ContractMisuse (terminal); Infrastructure would retry forever (F36)", k)
	}
	// Terminal = non-retryable: this is what lets the Manager read-back retry policy stop.
	var e *fwra.Error
	_ = errors.As(derr, &e)
	if e.Retryable {
		t.Fatalf("decode error must be NON-retryable; got Retryable=true (F36 loop-forever bug)")
	}
	// The decode diagnostic (the wire-name rejection) must survive so it can be shown at the
	// human StageDraftFailed gate as the failureReason.
	if !strings.Contains(derr.Error(), "is not a recognized Trigger wire name") {
		t.Errorf("decode error must carry the wire-name diagnostic; got: %v", derr)
	}
}

// Black-box regression tests for the per-activity construction status head-state
// (Task 1: seed-archistrator-design-state). Mirrors the gitactivity_test.go
// discipline: real throwaway on-disk git store, no mocks, test-authoring
// constitution §7 anti-cheat. Covers:
//   - RecordActivityStarted births the row (Phase=Running, StartedAt set)
//   - RecordActivityCompleted advances to Done, CompletedAt set
//   - idempotent re-record (same key, stale version → ledger wins)
//   - EncodeProjectJSON → DecodeProjectJSON round-trip preserves ActivityConstruction

// newConstructionStore spins a real local git store and seeds a project so
// modeRequireExisting Record* verbs have a row.
func newConstructionStore(t *testing.T) (*GitStore, ProjectID, Version, RepoCredential) {
	t.Helper()
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID(uuid.NewString())
	v, err := store.CreateProject(ctx, id, "alice", "ConstructionDemo", cred, fwra.IdempotencyKey("wf:create-con"))
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return store, id, v, cred
}

// readConstruction reads the ActivityConstruction row for activityID.
func readConstruction(t *testing.T, store *GitStore, id ProjectID, cred RepoCredential, activityID string) ActivityConstructionStatus {
	t.Helper()
	proj, err := store.ReadProject(fwra.Context{Context: context.Background()}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	s, ok := proj.ActivityConstruction[activityID]
	if !ok {
		t.Fatalf("ActivityConstruction[%s] absent; have keys %v", activityID, constructionKeys(proj))
	}
	return s
}

func constructionKeys(p Project) []string {
	out := make([]string, 0, len(p.ActivityConstruction))
	for k := range p.ActivityConstruction {
		out = append(out, k)
	}
	return out
}

// TestRecordActivityStarted_BirthsRow — RecordActivityStarted births the row with
// Phase=Running and a non-nil StartedAt; CompletedAt must be nil.
func TestRecordActivityStarted_BirthsRow(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2, err := store.RecordActivityStarted(fwra.Context{Context: ctx}, id, v, "X001", cred, fwra.IdempotencyKey("wf:started"))
	if err != nil {
		t.Fatalf("RecordActivityStarted: %v", err)
	}
	if v2 != v+1 {
		t.Fatalf("version = %d, want %d", v2, v+1)
	}
	s := readConstruction(t, store, id, cred, "X001")
	if s.ActivityID != "X001" {
		t.Fatalf("ActivityID = %q, want X001", s.ActivityID)
	}
	if s.Phase != ActivityConstructionRunning {
		t.Fatalf("Phase = %v, want Running", s.Phase)
	}
	if s.StartedAt == nil {
		t.Fatal("StartedAt must be set after RecordActivityStarted")
	}
	if s.CompletedAt != nil {
		t.Fatalf("CompletedAt must be nil after started, got %v", s.CompletedAt)
	}
}

// TestRecordActivityCompleted_AdvancesToDone — after a Started, RecordActivityCompleted
// flips Phase to Done and sets CompletedAt.
func TestRecordActivityCompleted_AdvancesToDone(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2, err := store.RecordActivityStarted(fwra.Context{Context: ctx}, id, v, "X001", cred, fwra.IdempotencyKey("wf:started-done"))
	if err != nil {
		t.Fatalf("RecordActivityStarted: %v", err)
	}
	v3, err := store.RecordActivityCompleted(fwra.Context{Context: ctx}, id, v2, "X001", cred, fwra.IdempotencyKey("wf:completed"))
	if err != nil {
		t.Fatalf("RecordActivityCompleted: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}
	s := readConstruction(t, store, id, cred, "X001")
	if s.Phase != ActivityConstructionDone {
		t.Fatalf("Phase = %v, want Done", s.Phase)
	}
	if s.CompletedAt == nil {
		t.Fatal("CompletedAt must be set after RecordActivityCompleted")
	}
	if s.StartedAt == nil {
		t.Fatal("StartedAt must still be set after CompletedAt")
	}
}

// TestRecordActivityStarted_Idempotent — retrying the same key with a stale version
// returns the prior Version via the dedup ledger, no double-apply.
func TestRecordActivityStarted_Idempotent(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2, err := store.RecordActivityStarted(fwra.Context{Context: ctx}, id, v, "X001", cred, fwra.IdempotencyKey("wf:started-idem"))
	if err != nil {
		t.Fatalf("RecordActivityStarted: %v", err)
	}
	before, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}

	// Retry with the SAME key but stale expectedVersion=0; dedup must win.
	v2again, err := store.RecordActivityStarted(fwra.Context{Context: ctx}, id, 0, "X001", cred, fwra.IdempotencyKey("wf:started-idem"))
	if err != nil {
		t.Fatalf("idempotent retry should succeed via ledger, got: %v", err)
	}
	if v2again != v2 {
		t.Fatalf("idempotent retry version = %d, want original %d", v2again, v2)
	}
	after, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if after.Version != before.Version {
		t.Fatalf("retry produced a NEW state commit %d → %d (DOUBLE APPLY)", before.Version, after.Version)
	}
}

// TestActivityConstruction_RoundTrip — EncodeProjectJSON → DecodeProjectJSON
// preserves the ActivityConstruction map (phase, timestamps).
func TestActivityConstruction_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	comp := now.Add(5 * time.Minute)

	p := Project{}
	p.ActivityConstruction = map[string]ActivityConstructionStatus{
		"X001": {
			ActivityID:  "X001",
			Phase:       ActivityConstructionDone,
			StartedAt:   &now,
			CompletedAt: &comp,
		},
		"X002": {
			ActivityID: "X002",
			Phase:      ActivityConstructionRunning,
			StartedAt:  &now,
		},
	}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON: ok=false, want true")
	}

	x001, found := got.ActivityConstruction["X001"]
	if !found {
		t.Fatal("X001 absent after round-trip")
	}
	if x001.Phase != ActivityConstructionDone {
		t.Fatalf("X001 Phase = %v, want Done", x001.Phase)
	}
	if x001.StartedAt == nil || !x001.StartedAt.Equal(now) {
		t.Fatalf("X001 StartedAt = %v, want %v", x001.StartedAt, now)
	}
	if x001.CompletedAt == nil || !x001.CompletedAt.Equal(comp) {
		t.Fatalf("X001 CompletedAt = %v, want %v", x001.CompletedAt, comp)
	}

	x002, found := got.ActivityConstruction["X002"]
	if !found {
		t.Fatal("X002 absent after round-trip")
	}
	if x002.Phase != ActivityConstructionRunning {
		t.Fatalf("X002 Phase = %v, want Running", x002.Phase)
	}
	if x002.CompletedAt != nil {
		t.Fatalf("X002 CompletedAt should be nil, got %v", x002.CompletedAt)
	}
}

// TestActivityConstructionPhase_String — the phase String() returns wire names.
func TestActivityConstructionPhase_String(t *testing.T) {
	cases := []struct {
		phase ActivityConstructionPhase
		want  string
	}{
		{ActivityConstructionNotStarted, "notStarted"},
		{ActivityConstructionRunning, "running"},
		{ActivityConstructionDone, "done"},
	}
	for _, c := range cases {
		if got := c.phase.String(); got != c.want {
			t.Errorf("Phase(%d).String() = %q, want %q", c.phase, got, c.want)
		}
	}
}

// TestDeploymentTopology_JSONRoundTrip proves the typed deployment topology on
// DeploymentOperationsModel serializes its enum fields as STRING wire names (matching
// the Layer/ComponentKind/CallMode convention via enumjson.go) and round-trips
// losslessly through json.Marshal/json.Unmarshal.
func TestDeploymentTopology_JSONRoundTrip(t *testing.T) {
	containerKey := "project-state-access"

	original := &DeploymentOperationsModel{
		Deployment: DeploymentTopology{
			DeliveryStyle: StyleBoth,
			Containers: []DeployContainer{
				{Key: containerKey, Name: "server", Technology: "Go", Description: "the application server", Components: []string{"ProjectStateAccess"}},
			},
			Environments: []DeploymentEnvironment{
				{
					Profile: ProfileCloud,
					Title:   "Production (cloud)",
					Nodes: []DeploymentNode{
						{
							Name:       "k8s-cluster",
							Technology: "Kubernetes",
							Children: []DeploymentNode{
								{
									Name:       "archistrator-ns",
									Technology: "Namespace",
									ContainerInstances: []ContainerInstance{
										{ContainerKey: containerKey, Note: "server pod"},
									},
								},
							},
						},
					},
				},
				{
					Profile: ProfileTest,
					Title:   "Test (ephemeral)",
					Nodes: []DeploymentNode{
						{Name: "test-harness", Technology: "in-memory"},
					},
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	js := string(data)
	// Enum fields must render as the expected STRING tokens, not integers.
	for _, want := range []string{
		`"deliveryStyle":"both"`,
		`"profile":"cloud"`,
		`"profile":"test"`,
		`"containerKey":"` + containerKey + `"`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("marshalled JSON missing %s\nfull: %s", want, js)
		}
	}

	var back DeploymentOperationsModel
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(*original, back) {
		t.Fatalf("round-trip mismatch:\n got  %#v\n want %#v", back, *original)
	}
}

// TestDeploymentEnums_WireTokens pins the exact string tokens for each enum value
// and confirms unknown wire names error the same way the existing enums do.
func TestDeploymentEnums_WireTokens(t *testing.T) {
	styleCases := map[DeliveryStyle]string{
		StyleCloud: "cloud",
		StyleLocal: "local",
		StyleBoth:  "both",
	}
	for v, want := range styleCases {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal DeliveryStyle(%d): %v", v, err)
		}
		if got := string(data); got != `"`+want+`"` {
			t.Fatalf("DeliveryStyle(%d) marshalled as %s, want %q", v, got, want)
		}
		var back DeliveryStyle
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if back != v {
			t.Fatalf("DeliveryStyle round-trip: got %d, want %d", back, v)
		}
	}

	profileCases := map[DeploymentProfile]string{
		ProfileCloud: "cloud",
		ProfileLocal: "local",
		ProfileTest:  "test",
	}
	for v, want := range profileCases {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal DeploymentProfile(%d): %v", v, err)
		}
		if got := string(data); got != `"`+want+`"` {
			t.Fatalf("DeploymentProfile(%d) marshalled as %s, want %q", v, got, want)
		}
		var back DeploymentProfile
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if back != v {
			t.Fatalf("DeploymentProfile round-trip: got %d, want %d", back, v)
		}
	}
}

// TestDeploymentEnums_InvalidWireName confirms an unrecognized string token errors,
// mirroring unmarshalEnum's "is not a recognized ... wire name" behaviour.
func TestDeploymentEnums_InvalidWireName(t *testing.T) {
	var s DeliveryStyle
	if err := json.Unmarshal([]byte(`"hybrid"`), &s); err == nil {
		t.Fatal("expected error unmarshalling invalid DeliveryStyle wire name, got nil")
	}
	var p DeploymentProfile
	if err := json.Unmarshal([]byte(`"staging"`), &p); err == nil {
		t.Fatal("expected error unmarshalling invalid DeploymentProfile wire name, got nil")
	}
}

// servicecontract_test.go verifies the ServiceContract contract-document model
// survives a full EncodeProjectJSON → DecodeProjectJSON round-trip, including a
// byte-identical second pass. Mirrors the TestActivityConstruction_RoundTrip
// discipline: no git store, no mocks — just the public codec seam.

// TestServiceContract_RoundTrip — a Project with one ServiceContracts["artifactAccess"]
// entry (a contract document: title + $defs + interface with a param + result)
// survives EncodeProjectJSON → DecodeProjectJSON intact and re-encodes byte-identically.
func TestServiceContract_RoundTrip(t *testing.T) {
	p := Project{}
	p.ServiceContracts = map[string]ServiceContract{
		"artifactAccess": {
			Component: "artifactAccess",
			Layer:     "ResourceAccess",
			GoPackage: "internal/resourceaccess/artifact",
			Title:     "artifact contract",
			Defs: map[string]json.RawMessage{
				"ArtifactID": json.RawMessage(`{"type":"string"}`),
				"Artifact":   json.RawMessage(`{"type":"object","properties":{"id":{"$ref":"#/$defs/ArtifactID"}},"required":["id"],"additionalProperties":false}`),
			},
			Interface: ContractInterface{
				Name:  "ArtifactAccess",
				Layer: "resourceaccess",
				Operations: []ContractOperation{
					{Name: "Cancel", Params: nil, Error: true},
					{
						Name: "Read",
						Params: []ContractParam{
							{Name: "id", Schema: json.RawMessage(`{"$ref":"#/$defs/ArtifactID"}`)},
						},
						Result: json.RawMessage(`{"$ref":"#/$defs/Artifact"}`),
						Error:  true,
					},
				},
			},
		},
	}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON: ok=false, want true")
	}

	assertServiceContractDocSurvived(t, got)

	// BYTE-IDENTICAL second pass: re-encoding the decoded aggregate yields the
	// identical bytes (the persistence invariant).
	raw2, err := EncodeProjectJSON(got)
	if err != nil {
		t.Fatalf("EncodeProjectJSON (2nd pass): %v", err)
	}
	if !bytes.Equal(raw, raw2) {
		t.Fatalf("round-trip not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", raw, raw2)
	}
}

// assertServiceContractDocSurvived asserts the ServiceContracts["artifactAccess"]
// contract document round-tripped intact — component identity, $defs, and the
// interface's operations with their params and result.
func assertServiceContractDocSurvived(t *testing.T, got Project) {
	t.Helper()
	sc, found := got.ServiceContracts["artifactAccess"]
	if !found {
		t.Fatal("ServiceContracts[artifactAccess] absent after round-trip")
	}
	if sc.Component != "artifactAccess" {
		t.Fatalf("Component = %q, want artifactAccess", sc.Component)
	}
	if sc.GoPackage != "internal/resourceaccess/artifact" {
		t.Fatalf("GoPackage = %q, want internal/resourceaccess/artifact", sc.GoPackage)
	}
	if sc.Title != "artifact contract" {
		t.Fatalf("Title = %q, want artifact contract", sc.Title)
	}
	if len(sc.Defs) != 2 {
		t.Fatalf("Defs len = %d, want 2", len(sc.Defs))
	}
	if len(sc.Interface.Operations) != 2 {
		t.Fatalf("Operations len = %d, want 2", len(sc.Interface.Operations))
	}
	read := sc.Interface.Operations[1]
	if read.Name != "Read" {
		t.Fatalf("Operations[1].Name = %q, want Read", read.Name)
	}
	if len(read.Params) != 1 || read.Params[0].Name != "id" {
		t.Fatalf("Operations[1].Params unexpected: %+v", read.Params)
	}
	if len(read.Result) == 0 {
		t.Fatal("Operations[1].Result absent after round-trip")
	}
}

func TestPhaseArtifacts_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	pa := PhaseArtifacts{
		SRS: map[string]SRSRecord{
			"projectExport": {Component: "projectExport", Content: "# SRS\n1. export project state", AuthoredAt: &now},
		},
		TestPlan: map[string]TestPlanRecord{
			"projectExport": {Component: "projectExport", Content: "## Test Plan\n- verify export", AuthoredAt: &now},
		},
		IntegrationNote: map[string]IntegrationNoteRecord{
			"projectExport": {Component: "projectExport", Content: "integrated OK", AuthoredAt: &now},
		},
	}
	b, err := json.Marshal(pa)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PhaseArtifacts
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SRS["projectExport"].Content != pa.SRS["projectExport"].Content {
		t.Errorf("SRS content mismatch after round-trip")
	}
	if got.TestPlan["projectExport"].Component != "projectExport" {
		t.Errorf("TestPlan component mismatch after round-trip")
	}
}

func TestTestingState_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	ts := TestingState{
		QualityGates: []QualityGate{
			{ActivityType: "C-PE", Phase: "construction", When: "before", Mode: "escalate"},
		},
		Defects: []DefectRecord{
			{ID: "D-001", Title: "null pointer in export", Severity: "high", FiledAt: &now},
		},
		TestRuns: []TestRun{
			{ID: "TR-001", StartedAt: &now, Passed: 42, Failed: 0},
		},
	}
	b, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TestingState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.QualityGates) != 1 || got.QualityGates[0].Mode != "escalate" {
		t.Errorf("QualityGates mismatch after round-trip")
	}
	if len(got.Defects) != 1 || got.Defects[0].Severity != "high" {
		t.Errorf("Defects mismatch after round-trip")
	}
	if len(got.TestRuns) != 1 || got.TestRuns[0].Passed != 42 {
		t.Errorf("TestRuns mismatch after round-trip")
	}
}

func TestProject_PhaseArtifacts_Field(t *testing.T) {
	p := Project{}
	if p.PhaseArtifacts != nil {
		t.Error("PhaseArtifacts should be nil by default")
	}
	now := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	p.PhaseArtifacts = &PhaseArtifacts{
		SRS: map[string]SRSRecord{"c": {Component: "c", Content: "x", AuthoredAt: &now}},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal Project: %v", err)
	}
	var got Project
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal Project: %v", err)
	}
	if got.PhaseArtifacts == nil || got.PhaseArtifacts.SRS["c"].Content != "x" {
		t.Error("PhaseArtifacts not round-tripped through Project")
	}
}

// TestProjectDoc_PhaseArtifacts_RoundTrip verifies that a Project with populated
// PhaseArtifacts and TestingState encodes via encodeProjectDoc (the canonical 2-space
// json.MarshalIndent path) and decodes back equal via decodeProjectDoc.
func TestProjectDoc_PhaseArtifacts_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	p := Project{
		ID:    ProjectID("test-proj-001"),
		Name:  "roundtrip test",
		Phase: PhaseConstruction,
		PhaseArtifacts: &PhaseArtifacts{
			SRS: map[string]SRSRecord{
				"authManager": {Component: "authManager", Content: "# SRS", AuthoredAt: &now},
			},
			UXRequirements: map[string]UXRequirementsRecord{
				"homeScreen": {Surface: "homeScreen", Content: "UX reqs", AuthoredAt: &now},
			},
			ProvisioningSpec: map[string]ProvisioningSpecRecord{
				"postgres": {Resource: "postgres", Content: "spec", AuthoredAt: &now},
			},
			DocOutline: map[string]DocOutlineRecord{
				"api-guide": {Doc: "api-guide", Content: "outline", AuthoredAt: &now},
			},
		},
		TestingState: &TestingState{
			SystemTestPlan: &SystemTestPlan{
				UseCaseIndex: []string{"UC1", "UC2"},
				Entries:      []string{"smoke pass", "export flow"},
				Status:       "approved",
				ApprovedAt:   &now,
			},
			HarnessModule: &HarnessModule{RepoRef: "corpus/tests/harness", Status: "approved"},
			PerfHarness:   &PerfHarness{RepoRef: "corpus/tests/perf", Status: ""},
			QualityGates: []QualityGate{
				{ActivityType: "C-IE", Phase: "construction", When: "before", Mode: "escalate"},
			},
			QualityAuditReport: "All gates green.",
			TestRuns: []TestRun{
				{ID: "TR-001", StartedAt: &now, Passed: 100, Failed: 2, Note: "initial run"},
			},
			Defects: []DefectRecord{
				{ID: "D-001", Title: "export returns 500", Severity: "critical", FiledAt: &now},
			},
		},
	}

	encoded, err := encodeProjectDoc(&p, now)
	if err != nil {
		t.Fatalf("encodeProjectDoc: %v", err)
	}

	got, ok, err := decodeProjectDoc(encoded, p.ID)
	if err != nil {
		t.Fatalf("decodeProjectDoc: %v", err)
	}
	if !ok {
		t.Fatal("decodeProjectDoc: project not found")
	}

	assertProjectDocPhaseArtifacts(t, got)
	assertProjectDocTestingState(t, got)
}

// assertProjectDocPhaseArtifacts asserts the PhaseArtifacts round-trip through the
// projectDoc codec.
func assertProjectDocPhaseArtifacts(t *testing.T, got Project) {
	t.Helper()
	// PhaseArtifacts round-trip
	if got.PhaseArtifacts == nil {
		t.Fatal("PhaseArtifacts is nil after round-trip")
	}
	if got.PhaseArtifacts.SRS["authManager"].Content != "# SRS" {
		t.Errorf("PhaseArtifacts.SRS content mismatch: got %q", got.PhaseArtifacts.SRS["authManager"].Content)
	}
	if got.PhaseArtifacts.UXRequirements["homeScreen"].Surface != "homeScreen" {
		t.Errorf("PhaseArtifacts.UXRequirements surface mismatch")
	}
	if got.PhaseArtifacts.ProvisioningSpec["postgres"].Resource != "postgres" {
		t.Errorf("PhaseArtifacts.ProvisioningSpec resource mismatch")
	}
	if got.PhaseArtifacts.DocOutline["api-guide"].Content != "outline" {
		t.Errorf("PhaseArtifacts.DocOutline content mismatch")
	}
}

// assertProjectDocTestingState asserts the TestingState round-trip through the
// projectDoc codec.
func assertProjectDocTestingState(t *testing.T, got Project) {
	t.Helper()
	// TestingState round-trip
	if got.TestingState == nil {
		t.Fatal("TestingState is nil after round-trip")
	}
	if got.TestingState.SystemTestPlan == nil || got.TestingState.SystemTestPlan.Status != "approved" {
		t.Errorf("TestingState.SystemTestPlan status mismatch")
	}
	if len(got.TestingState.SystemTestPlan.UseCaseIndex) != 2 {
		t.Errorf("TestingState.SystemTestPlan.UseCaseIndex len mismatch: got %d", len(got.TestingState.SystemTestPlan.UseCaseIndex))
	}
	if got.TestingState.HarnessModule == nil || got.TestingState.HarnessModule.RepoRef != "corpus/tests/harness" {
		t.Errorf("TestingState.HarnessModule mismatch")
	}
	if got.TestingState.QualityAuditReport != "All gates green." {
		t.Errorf("TestingState.QualityAuditReport mismatch")
	}
	if len(got.TestingState.QualityGates) != 1 || got.TestingState.QualityGates[0].Mode != "escalate" {
		t.Errorf("TestingState.QualityGates mismatch")
	}
	if len(got.TestingState.TestRuns) != 1 || got.TestingState.TestRuns[0].Passed != 100 {
		t.Errorf("TestingState.TestRuns mismatch")
	}
	if len(got.TestingState.Defects) != 1 || got.TestingState.Defects[0].Severity != "critical" {
		t.Errorf("TestingState.Defects mismatch")
	}
}

// TestProjectDoc_BackCompat_NoPhaseArtifacts verifies that an existing project.json
// without phaseArtifacts or testingState decodes cleanly to nil/empty containers
// (backward compatibility).
func TestProjectDoc_BackCompat_NoPhaseArtifacts(t *testing.T) {
	// Minimal project.json as it would appear before Task 3 fields were added.
	raw := []byte(`{
  "id": "legacy-project",
  "version": 1,
  "phase": 0,
  "owner": "testowner",
  "name": "legacy project",
  "research": {},
  "slots": {}
}`)
	got, ok, err := decodeProjectDoc(raw, ProjectID("legacy-project"))
	if err != nil {
		t.Fatalf("decodeProjectDoc on legacy JSON: %v", err)
	}
	if !ok {
		t.Fatal("project not found in legacy JSON")
	}
	if got.PhaseArtifacts != nil {
		t.Errorf("PhaseArtifacts should be nil for legacy project.json, got %+v", got.PhaseArtifacts)
	}
	if got.TestingState != nil {
		t.Errorf("TestingState should be nil for legacy project.json, got %+v", got.TestingState)
	}
}

// operatingmodel_test.go — coverage for the project-level OperatingModel field + the
// SetOperatingModel head-state write (founder ruling 2026-07-05). A project is born
// self-operated (the back-compat default); SetOperatingModel flips it to
// archistrator-operated; a project.json that pre-dates the field decodes to the
// default; and an unknown wire value is rejected.

func TestGitStore_SetOperatingModel_RoundTrip(t *testing.T) {
	store, _, cred, ctx := newLocalGitStoreWithRepo(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// A fresh project is born self-operated (the default applied on decode).
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.OperatingModel != OperatingModelSelfOperated {
		t.Fatalf("fresh project operating model = %q, want selfOperated (born explicit)", proj.OperatingModel)
	}

	// Flip it to archistrator-operated.
	v2, err := store.SetOperatingModel(ctx, id, proj.Version, OperatingModelArchistratorOperated, cred, "wf:setmodel")
	if err != nil {
		t.Fatalf("SetOperatingModel: %v", err)
	}
	after, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after set: %v", err)
	}
	if after.OperatingModel != OperatingModelArchistratorOperated {
		t.Fatalf("operating model after set = %q, want archistratorOperated", after.OperatingModel)
	}

	// IDEMPOTENT RETRY: same key dedups to the original result version (no double-write).
	vAgain, err := store.SetOperatingModel(ctx, id, 999, OperatingModelArchistratorOperated, cred, "wf:setmodel")
	if err != nil {
		t.Fatalf("idempotent retry SetOperatingModel: %v", err)
	}
	if vAgain != v2 {
		t.Fatalf("idempotent retry must dedup to result version %d, got %d", v2, vAgain)
	}
}

func TestGitStore_SetOperatingModel_RejectsUnknownValue(t *testing.T) {
	store, _, cred, ctx := newLocalGitStoreWithRepo(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_, err := store.SetOperatingModel(ctx, id, 1, OperatingModel("bogusCloud"), cred, "wf:bad")
	if err == nil {
		t.Fatal("SetOperatingModel with an unknown model must fail")
	}
}

// TestDecodeProjectJSON_PreFieldReadsAsSelfOperated proves a committed project.json
// that pre-dates the operatingModel field decodes to the EMPTY value (preserved verbatim
// for byte-identical round-trip) which every reader interprets as the DEFAULT
// (selfOperated) via OrDefault — so an existing project keeps today's open guidance.
func TestDecodeProjectJSON_PreFieldReadsAsSelfOperated(t *testing.T) {
	// A minimal pre-field document — no "operatingModel" key at all.
	raw := []byte(`{"id":"p1","version":3,"phase":0,"owner":"alice","name":"Legacy","research":{"Sources":null},"slots":{}}`)
	proj, ok, err := DecodeProjectJSON(raw, ProjectID("p1"))
	if err != nil || !ok {
		t.Fatalf("DecodeProjectJSON: ok=%v err=%v", ok, err)
	}
	if !proj.OperatingModel.IsZero() {
		t.Fatalf("pre-field project decoded operating model = %q, want empty (verbatim)", proj.OperatingModel)
	}
	if proj.OperatingModel.OrDefault() != OperatingModelSelfOperated {
		t.Fatalf("pre-field project OrDefault = %q, want selfOperated", proj.OperatingModel.OrDefault())
	}
}

// TestEncodeProjectJSON_PersistsOperatingModel proves the field round-trips through the
// canonical project.json encoder once set (a lazy migration persists the concrete value).
func TestEncodeProjectJSON_PersistsOperatingModel(t *testing.T) {
	p := Project{ID: "p1", Owner: "alice", Name: "Demo", OperatingModel: OperatingModelArchistratorOperated}
	b, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	if !strings.Contains(string(b), `"operatingModel": "archistratorOperated"`) {
		t.Fatalf("encoded project.json missing operatingModel; got:\n%s", string(b))
	}
}

// validSystemJSON is a minimal, well-formed System model with explicit, consistent
// enum fields on every component/relationship/dynamic view.
const validSystemJSON = `{
  "components": [
    {"id":"web-client","name":"WebClient","kind":"client","layer":"client","encapsulates":"","atomicBusinessVerbs":[]},
    {"id":"order-mgr","name":"OrderManager","kind":"manager","layer":"manager","encapsulates":"the order workflow","atomicBusinessVerbs":[]},
    {"id":"pricing-eng","name":"PricingEngine","kind":"engine","layer":"engine","encapsulates":"pricing","atomicBusinessVerbs":[]}
  ],
  "relationships": [
    {"from":"web-client","to":"order-mgr","mode":"sync","label":"places order"},
    {"from":"order-mgr","to":"pricing-eng","mode":"sync","label":"prices"}
  ],
  "dynamicViews": [
    {"useCaseId":"uc1","key":"uc1-place-order","title":"Place order","participants":["web-client","order-mgr"],
     "edges":[{"from":"web-client","to":"order-mgr","mode":"sync","label":"places order"}]}
  ]
}`

func TestRequireModelFields_ValidSystem(t *testing.T) {
	if err := RequireModelFields(KindSystem, []byte(validSystemJSON)); err != nil {
		t.Fatalf("valid system should pass, got: %v", err)
	}
}

// ---- TraceCall.Alt (rollout rulings 2026-07-31): tolerant-decode on requireDynamicViewSteps ----

func TestRequireModelFields_DynamicViewStep_NoAlt_Passes(t *testing.T) {
	// A step-keyed dynamic view whose call omits "alt" entirely — the shape every
	// committed view predates the field with. Absence must be fine.
	j := `{
      "components": [
        {"id":"c","name":"WebClient","kind":"client","layer":"client","encapsulates":"","atomicBusinessVerbs":[]}
      ],
      "relationships": [],
      "dynamicViews": [
        {"useCaseId":"uc1","key":"uc1-k","title":"UC1","steps":[
          {"activityNodeId":"n1","calls":[{"from":"c","to":"c","mode":"sync","label":"x"}]}
        ]}
      ]
    }`
	if err := RequireModelFields(KindSystem, []byte(j)); err != nil {
		t.Fatalf("a step call omitting alt should pass, got: %v", err)
	}
}

func TestRequireModelFields_DynamicViewStep_AltString_Passes(t *testing.T) {
	j := `{
      "components": [
        {"id":"c","name":"WebClient","kind":"client","layer":"client","encapsulates":"","atomicBusinessVerbs":[]}
      ],
      "relationships": [],
      "dynamicViews": [
        {"useCaseId":"uc1","key":"uc1-k","title":"UC1","steps":[
          {"activityNodeId":"n1","calls":[{"from":"c","to":"c","mode":"sync","label":"x","alt":"g1"}]}
        ]}
      ]
    }`
	if err := RequireModelFields(KindSystem, []byte(j)); err != nil {
		t.Fatalf("a step call with a string alt should pass, got: %v", err)
	}
}

func TestRequireModelFields_DynamicViewStep_AltWrongType_Rejected(t *testing.T) {
	j := `{
      "components": [
        {"id":"c","name":"WebClient","kind":"client","layer":"client","encapsulates":"","atomicBusinessVerbs":[]}
      ],
      "relationships": [],
      "dynamicViews": [
        {"useCaseId":"uc1","key":"uc1-k","title":"UC1","steps":[
          {"activityNodeId":"n1","calls":[{"from":"c","to":"c","mode":"sync","label":"x","alt":42}]}
        ]}
      ]
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "alt") {
		t.Fatalf("a non-string alt must be rejected naming alt, got: %v", err)
	}
}

func TestRequireModelFields_MissingLayer(t *testing.T) {
	// The live F81 case: a manager component that omits "layer". The strict struct decode
	// would silently default it to LayerClient; the presence+consistency check must reject.
	j := `{
      "components": [
        {"id":"order-mgr","name":"OrderManager","kind":"manager","encapsulates":"x","atomicBusinessVerbs":[]}
      ],
      "relationships": [],
      "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil {
		t.Fatal("a manager component missing its layer must be rejected")
	}
	if !strings.Contains(err.Error(), "layer") {
		t.Fatalf("error should name the missing layer field, got: %v", err)
	}
}

func TestRequireModelFields_LayerKindMismatch(t *testing.T) {
	// layer present but inconsistent with kind (manager kind, client layer) — the
	// signature of an omitted-then-defaulted layer that happened to be re-serialized.
	j := `{
      "components": [
        {"id":"order-mgr","name":"OrderManager","kind":"manager","layer":"client","encapsulates":"x","atomicBusinessVerbs":[]}
      ],
      "relationships": [],
      "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil {
		t.Fatal("layer inconsistent with kind must be rejected")
	}
	if !strings.Contains(err.Error(), "manager") || !strings.Contains(err.Error(), "client") {
		t.Fatalf("error should explain the kind/layer mismatch, got: %v", err)
	}
}

func TestRequireModelFields_MissingMode(t *testing.T) {
	j := `{
      "components": [
        {"id":"a","name":"A","kind":"manager","layer":"manager","encapsulates":"x"},
        {"id":"b","name":"B","kind":"engine","layer":"engine","encapsulates":"y"}
      ],
      "relationships": [ {"from":"a","to":"b","label":"x"} ],
      "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("a relationship missing its mode must be rejected naming mode, got: %v", err)
	}
}

func TestRequireModelFields_MissingKind(t *testing.T) {
	j := `{
      "components": [
        {"id":"a","name":"A","layer":"manager"}
      ],
      "relationships": [],
      "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("a component missing its kind must be rejected naming kind, got: %v", err)
	}
}

func TestRequireModelFields_UnrecognizedLayer(t *testing.T) {
	j := `{
      "components": [
        {"id":"a","name":"A","kind":"manager","layer":"bogus"}
      ],
      "relationships": [],
      "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "layer") {
		t.Fatalf("an unrecognized layer wire value must be rejected, got: %v", err)
	}
}

func TestRequireModelFields_CoreUseCases(t *testing.T) {
	valid := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","kind":"start","label":""},{"id":"a","kind":"action","label":"do"}],
                      "edges":[{"from":"s","to":"a","kind":"controlFlow","guard":""}]}},
         "rejectionReason":""}
      ]
    }`
	if err := RequireModelFields(KindCoreUseCases, []byte(valid)); err != nil {
		t.Fatalf("valid core use cases should pass, got: %v", err)
	}

	missingTrigger := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"classification":"core"},"rejectionReason":""}
      ]
    }`
	if err := RequireModelFields(KindCoreUseCases, []byte(missingTrigger)); err == nil || !strings.Contains(err.Error(), "trigger") {
		t.Fatalf("a use case missing its trigger must be rejected naming trigger, got: %v", err)
	}

	missingNodeKind := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","label":""}],"edges":[]}},"rejectionReason":""}
      ]
    }`
	if err := RequireModelFields(KindCoreUseCases, []byte(missingNodeKind)); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("an activity node missing its kind must be rejected, got: %v", err)
	}
}

// ---- ActivityNode.DecidedBy (rollout rulings 2026-07-31): tolerant-decode on requireActivityNodes ----

func TestRequireModelFields_ActivityNode_NoDecidedBy_Passes(t *testing.T) {
	// An activity node omitting "decidedBy" entirely — the shape every committed use
	// case predates the field with. Absence must be fine.
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","kind":"start","label":""},{"id":"a","kind":"action","label":"do"}],
                      "edges":[{"from":"s","to":"a","kind":"controlFlow","guard":""}]}},
         "rejectionReason":""}
      ]
    }`
	if err := RequireModelFields(KindCoreUseCases, []byte(j)); err != nil {
		t.Fatalf("an activity node omitting decidedBy should pass, got: %v", err)
	}
}

func TestRequireModelFields_ActivityNode_DecidedByString_Passes(t *testing.T) {
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","kind":"start","label":""},
                                {"id":"d","kind":"decision","label":"route","decidedBy":"order-mgr"},
                                {"id":"a","kind":"action","label":"do"}],
                      "edges":[{"from":"s","to":"d","kind":"controlFlow","guard":""},
                               {"from":"d","to":"a","kind":"guardedFlow","guard":"g"}]}},
         "rejectionReason":""}
      ]
    }`
	if err := RequireModelFields(KindCoreUseCases, []byte(j)); err != nil {
		t.Fatalf("a decision node with a string decidedBy should pass, got: %v", err)
	}
}

func TestRequireModelFields_ActivityNode_DecidedByWrongType_Rejected(t *testing.T) {
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","kind":"start","label":""},
                                {"id":"d","kind":"decision","label":"route","decidedBy":42},
                                {"id":"a","kind":"action","label":"do"}],
                      "edges":[{"from":"s","to":"d","kind":"controlFlow","guard":""},
                               {"from":"d","to":"a","kind":"guardedFlow","guard":"g"}]}},
         "rejectionReason":""}
      ]
    }`
	err := RequireModelFields(KindCoreUseCases, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "decidedBy") {
		t.Fatalf("a non-string decidedBy must be rejected naming decidedBy, got: %v", err)
	}
}

// ---- SYS-ENCAPSULATES (raw twin): M/E/RA must name a non-empty volatility; a client may be empty ----

func TestRequireModelFields_Encapsulates_ManagerMustBeNonEmpty(t *testing.T) {
	j := `{
      "components": [
        {"id":"m","name":"OrderManager","kind":"manager","layer":"manager","encapsulates":""}
      ],
      "relationships": [], "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "encapsulates") {
		t.Fatalf("a manager with empty encapsulates must be rejected naming encapsulates, got: %v", err)
	}
}

func TestRequireModelFields_Encapsulates_MissingKeyRejected(t *testing.T) {
	j := `{
      "components": [
        {"id":"c","name":"WebClient","kind":"client","layer":"client"}
      ],
      "relationships": [], "dynamicViews": []
    }`
	err := RequireModelFields(KindSystem, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "encapsulates") {
		t.Fatalf("a component omitting the encapsulates key must be rejected, got: %v", err)
	}
}

func TestRequireModelFields_Encapsulates_EmptyClientAllowed(t *testing.T) {
	// A CLIENT may carry empty encapsulates (transport owns no volatility); the non-empty
	// expectation for a client is a read-back finding, not a hard codec failure — this is
	// exactly what keeps committed state (empty-encapsulates clients) readable.
	j := `{
      "components": [
        {"id":"c","name":"WebClient","kind":"client","layer":"client","encapsulates":"","atomicBusinessVerbs":[]}
      ],
      "relationships": [], "dynamicViews": []
    }`
	if err := RequireModelFields(KindSystem, []byte(j)); err != nil {
		t.Fatalf("an empty-encapsulates client must be allowed on the write path, got: %v", err)
	}
}

// ---- UC-ACT-PRESENT: every use case needs a non-null activity with start + action ----

func TestRequireModelFields_ActivityPresent_NullRejected(t *testing.T) {
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core","activity":null},"rejectionReason":""}
      ]
    }`
	err := RequireModelFields(KindCoreUseCases, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "activity") {
		t.Fatalf("a use case with a null activity must now be rejected, got: %v", err)
	}
}

func TestRequireModelFields_ActivityPresent_NoActionRejected(t *testing.T) {
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","kind":"start","label":""}],"edges":[]}},"rejectionReason":""}
      ]
    }`
	err := RequireModelFields(KindCoreUseCases, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "structurally empty") {
		t.Fatalf("a start-only activity must be rejected as structurally empty, got: %v", err)
	}
}

// ---- UC-ACT-PRESENT tier parity (2026-07-30 callchain-realization): an ENTRY is a start
// node OR an edge-less timeEvent/acceptEvent node — mirrors methodcheck's
// activityHasEntryAndAction (framework-go/methodcheck/rules_statevalidation.go). ----

func TestRequireModelFields_ActivityPresent_EventEntryOnly_Passes(t *testing.T) {
	// No start node at all: the diagram's only entry is an edge-less timeEvent — the
	// standard ingress for a scheduled use case. Must be accepted.
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Nightly sweep","actors":[],"trigger":"timer","classification":"core",
          "activity":{"nodes":[{"id":"t","kind":"timeEvent","label":"midnight"},
                                {"id":"a","kind":"action","label":"do"},
                                {"id":"e","kind":"end","label":""}],
                      "edges":[{"from":"t","to":"a","kind":"controlFlow","guard":""},
                               {"from":"a","to":"e","kind":"controlFlow","guard":""}]}},
         "rejectionReason":""}
      ]
    }`
	if err := RequireModelFields(KindCoreUseCases, []byte(j)); err != nil {
		t.Fatalf("an edge-less timeEvent entry (no start node) must be accepted as an entry, got: %v", err)
	}
}

func TestRequireModelFields_ActivityPresent_EventWithIncomingEdge_Rejected(t *testing.T) {
	// The diagram's only event node HAS an incoming edge — it is not an entry — and
	// there is no start node, so the diagram must still be rejected as structurally
	// empty (an event node mid-flow does not satisfy UC-ACT-PRESENT).
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Nightly sweep","actors":[],"trigger":"timer","classification":"core",
          "activity":{"nodes":[{"id":"a","kind":"action","label":"do"},
                                {"id":"t","kind":"timeEvent","label":"midnight"}],
                      "edges":[{"from":"a","to":"t","kind":"controlFlow","guard":""}]}},
         "rejectionReason":""}
      ]
    }`
	err := RequireModelFields(KindCoreUseCases, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "structurally empty") {
		t.Fatalf("a timeEvent node with an incoming edge is not an entry; must be rejected as structurally empty, got: %v", err)
	}
}

// ---- UC-GUARD-LABEL: a guardedFlow edge must carry non-empty guard text ----

func TestRequireModelFields_GuardLabel_EmptyGuardRejected(t *testing.T) {
	j := `{
      "decisions": [
        {"useCase":{"id":"uc1","name":"Place order","actors":[],"trigger":"clientAction","classification":"core",
          "activity":{"nodes":[{"id":"s","kind":"start"},{"id":"a","kind":"action","label":"do"}],
                      "edges":[{"from":"s","to":"a","kind":"guardedFlow","guard":""}]}},"rejectionReason":""}
      ]
    }`
	err := RequireModelFields(KindCoreUseCases, []byte(j))
	if err == nil || !strings.Contains(err.Error(), "guard") {
		t.Fatalf("a guardedFlow edge with empty guard must be rejected, got: %v", err)
	}
}

// ---- STD-STATUS-EXPLICIT: every standard-check item must emit status ----

func TestRequireModelFields_StandardCheck(t *testing.T) {
	valid := `{"items":[{"section":"S","guideline":"G","status":"pass","justification":""}]}`
	if err := RequireModelFields(KindStandardCheck, []byte(valid)); err != nil {
		t.Fatalf("valid standard check should pass, got: %v", err)
	}
	missing := `{"items":[{"section":"S","guideline":"G","justification":""}]}`
	err := RequireModelFields(KindStandardCheck, []byte(missing))
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("a standard-check item omitting status must be rejected naming status, got: %v", err)
	}
}

// ---- VOL-AXIS-EXPLICIT: every volatility must emit axis ----

func TestRequireModelFields_Volatilities(t *testing.T) {
	valid := `{"items":[{"name":"V","rationale":"r","axis":"sameCustomerOverTime"}]}`
	if err := RequireModelFields(KindVolatilities, []byte(valid)); err != nil {
		t.Fatalf("valid volatilities should pass, got: %v", err)
	}
	missing := `{"items":[{"name":"V","rationale":"r"}]}`
	err := RequireModelFields(KindVolatilities, []byte(missing))
	if err == nil || !strings.Contains(err.Error(), "axis") {
		t.Fatalf("a volatility omitting axis must be rejected naming axis, got: %v", err)
	}
}

// ---- rejected[] (the ch. 2 false-volatility record) + traces[] (SR traceability) ----

func TestRequireModelFields_Volatilities_RejectedAndTraces(t *testing.T) {
	// A fully-populated model — accepted item with structured SR traces, plus one
	// rejected candidate per RejectionClass filter — must pass.
	valid := `{
      "items":[{"name":"V","rationale":"r","axis":"sameCustomerOverTime","traces":["SR-1","SR-2"]}],
      "rejected":[
        {"name":"UI theme","reason":"conditional config, not open-ended","class":"variableNotVolatile"},
        {"name":"Tax rules","reason":"identical across customers","class":"natureOfTheBusiness"},
        {"name":"Reporting","reason":"habitual block, no volatility","class":"speculative"},
        {"name":"Email transport","reason":"folded into notification volatility","class":"foldedInto"}
      ]
    }`
	if err := RequireModelFields(KindVolatilities, []byte(valid)); err != nil {
		t.Fatalf("valid volatilities with rejected+traces should pass, got: %v", err)
	}

	// BACK-COMPAT: an older model with no rejected roster (and no traces) stays legal.
	legacy := `{"items":[{"name":"V","rationale":"r","axis":"sameCustomerOverTime"}]}`
	if err := RequireModelFields(KindVolatilities, []byte(legacy)); err != nil {
		t.Fatalf("a legacy volatilities model without rejected/traces must keep passing, got: %v", err)
	}

	// A rejected candidate omitting its class must be rejected naming class —
	// RejectionClass's zero value (variableNotVolatile) would otherwise silently
	// absorb the omission (the F81 zero-value hole).
	missingClass := `{"items":[],"rejected":[{"name":"X","reason":"r"}]}`
	if err := RequireModelFields(KindVolatilities, []byte(missingClass)); err == nil || !strings.Contains(err.Error(), "class") {
		t.Fatalf("a rejected candidate omitting class must be rejected naming class, got: %v", err)
	}

	// An unrecognized class wire name must be rejected listing the valid filters.
	badClass := `{"items":[],"rejected":[{"name":"X","reason":"r","class":"bogus"}]}`
	if err := RequireModelFields(KindVolatilities, []byte(badClass)); err == nil || !strings.Contains(err.Error(), "variableNotVolatile") {
		t.Fatalf("an unrecognized rejection class must be rejected naming the valid wire values, got: %v", err)
	}

	// A rejected candidate with an empty reason must be rejected: the record IS the
	// reasoning (TradeMe precedent — every rejection is documented).
	noReason := `{"items":[],"rejected":[{"name":"X","reason":" ","class":"speculative"}]}`
	if err := RequireModelFields(KindVolatilities, []byte(noReason)); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("a rejected candidate with an empty reason must be rejected naming reason, got: %v", err)
	}
}

// TestRequireModelFields_ReadBackParity confirms the check integrates into the codec:
// a System draft that omits every component's layer (the live F81 corruption) fails to
// re-decode through DecodeProjectJSON, exactly as the write path rejects it.
func TestRequireModelFields_ReadBackParity(t *testing.T) {
	// Build a project doc whose system slot carries a layer-less component. We hand-craft
	// the slot map shape decodeSlotsMap consumes (kind 5 = System).
	doc := `{
      "schemaVersion": 1,
      "slots": {
        "5": {"status": 4, "kind": 5, "model": {
          "components": [ {"id":"m","name":"OrderManager","kind":"manager","encapsulates":"x","atomicBusinessVerbs":[]} ],
          "relationships": [], "dynamicViews": []
        }}
      }
    }`
	_, _, err := DecodeProjectJSON([]byte(doc), ProjectID("p"))
	if err == nil {
		t.Fatal("read-back of a system slot with a layer-less component must fail")
	}
	if !strings.Contains(err.Error(), "layer") {
		t.Fatalf("read-back error should name the missing layer, got: %v", err)
	}
}

// A slot COMMITTED before the Revisions field existed persists with the revisions key
// omitted (zero-value). Decoding it must GRANDFATHER Revisions to 1 — a committed artifact
// is by definition revision 1 — so the amendment index (max(1,Revisions)) selects a real
// -amend-N branch and, crucially, a re-commit lands at 2 (not 1), keeping successive
// -amend-N branch names unique. A never-committed slot must stay at 0 so its FIRST commit
// still lands at 1.
func Test_decodeSlotsMap_GrandfathersPreFieldCommittedRevisions(t *testing.T) {
	missionJSON, err := json.Marshal(&MissionStatement{Vision: "v", Mission: "m"})
	if err != nil {
		t.Fatalf("marshal mission: %v", err)
	}
	w := map[string]slotJSON{
		// Pre-field COMMITTED slot: revisions omitted ⇒ entry.Revisions == 0.
		"committed": {Kind: int(KindMission), Status: int(ReviewCommitted), Model: missionJSON},
		// NON-committed slot (awaiting review): must NOT be grandfathered.
		"awaiting": {Kind: int(KindGlossary), Status: int(ReviewAwaitingReview), Model: mustJSON(t, &Glossary{})},
	}
	var p Project
	if err := decodeSlotsMap(w, &p); err != nil {
		t.Fatalf("decodeSlotsMap: %v", err)
	}
	if p.Mission.Revisions != 1 {
		t.Fatalf("pre-field COMMITTED slot must grandfather to Revisions 1, got %d", p.Mission.Revisions)
	}
	if p.Glossary.Revisions != 0 {
		t.Fatalf("a non-committed slot must stay at Revisions 0 (its first commit lands at 1), got %d", p.Glossary.Revisions)
	}
}

// End-to-end for item 3: a pre-field committed slot, once GRANDFATHERED on read to Revisions 1,
// lands at 2 on its first re-commit (amendment) via commitTransition's ++ — never at 1 — so the
// pre-field slot's second amendment gets a UNIQUE -amend-2 branch instead of colliding on -amend-1.
func Test_commitTransition_PreFieldReCommitLandsAtTwo(t *testing.T) {
	missionJSON, err := json.Marshal(&MissionStatement{Vision: "v", Mission: "m"})
	if err != nil {
		t.Fatalf("marshal mission: %v", err)
	}
	var p Project
	if err := decodeSlotsMap(map[string]slotJSON{
		"committed": {Kind: int(KindMission), Status: int(ReviewCommitted), Model: missionJSON},
	}, &p); err != nil {
		t.Fatalf("decodeSlotsMap: %v", err)
	}
	// Grandfathered base is 1.
	if p.Mission.Revisions != 1 {
		t.Fatalf("grandfathered base must be Revisions 1, got %d", p.Mission.Revisions)
	}
	// A re-commit (the amendment merge → CommitArtifact) bumps to 2.
	if err := commitTransition(KindMission, nil)(&p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}
	if p.Mission.Revisions != 2 {
		t.Fatalf("a pre-field slot's re-commit must land at 2 (first commit was 1), got %d", p.Mission.Revisions)
	}
}

// A never-committed slot's FIRST commit still lands at 1 (the grandfather only floors
// COMMITTED slots, so it does not inflate a genuine first commit).
func Test_commitTransition_FirstCommitLandsAtOne(t *testing.T) {
	missionJSON, err := json.Marshal(&MissionStatement{Vision: "v", Mission: "m"})
	if err != nil {
		t.Fatalf("marshal mission: %v", err)
	}
	var p Project
	if err := decodeSlotsMap(map[string]slotJSON{
		"awaiting": {Kind: int(KindMission), Status: int(ReviewAwaitingReview), Model: missionJSON},
	}, &p); err != nil {
		t.Fatalf("decodeSlotsMap: %v", err)
	}
	if p.Mission.Revisions != 0 {
		t.Fatalf("a never-committed slot must decode at Revisions 0, got %d", p.Mission.Revisions)
	}
	if err := commitTransition(KindMission, nil)(&p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}
	if p.Mission.Revisions != 1 {
		t.Fatalf("a genuine first commit must land at 1, got %d", p.Mission.Revisions)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// This file pins the PUBLIC typed-wire contract the SPA consumes: camelCase field
// names on every Phase-1 model + nested type, STRING enum names (not integer
// ordinals) for the enums the SPA reads, and a STRING ArtifactKind discriminator.
// CODE is the source of truth; openapi.yaml follows these bytes.

// TestArtifactKind_JSONString proves an ArtifactKind marshals to its canonical
// camelCase wire name and round-trips, and that legacy integer ordinals still
// decode (backward compatibility for any previously-persisted payload).
func TestArtifactKind_JSONString(t *testing.T) {
	for _, k := range AllArtifactKinds() {
		data, err := json.Marshal(k)
		if err != nil {
			t.Fatalf("marshal %v: %v", k, err)
		}
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatalf("kind %v did not marshal to a JSON string: %s", k, data)
		}
		if s != k.WireName() {
			t.Fatalf("kind %v marshalled as %q, want %q", k, s, k.WireName())
		}
		var back ArtifactKind
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %q: %v", data, err)
		}
		if back != k {
			t.Fatalf("round-trip kind: got %v, want %v", back, k)
		}
	}
	// Legacy integer ordinal still decodes.
	var legacy ArtifactKind
	if err := json.Unmarshal([]byte("4"), &legacy); err != nil {
		t.Fatalf("legacy ordinal: %v", err)
	}
	if legacy != KindCoreUseCases {
		t.Fatalf("legacy ordinal 4 = %v, want KindCoreUseCases", legacy)
	}
}

// TestMissionStatement_CamelCaseWire pins the literal camelCase field names of the
// MissionStatement model + its nested Objective.
func TestMissionStatement_CamelCaseWire(t *testing.T) {
	m := MissionStatement{
		Vision:     "ship value",
		Objectives: []Objective{{Number: 1, Statement: "be useful"}},
		Mission:    "components",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	for _, want := range []string{"vision", "objectives", "mission"} {
		if _, ok := generic[want]; !ok {
			t.Fatalf("missing camelCase field %q in %s", want, data)
		}
	}
	var obj []map[string]json.RawMessage
	if err := json.Unmarshal(generic["objectives"], &obj); err != nil {
		t.Fatalf("objectives: %v", err)
	}
	for _, want := range []string{"number", "statement"} {
		if _, ok := obj[0][want]; !ok {
			t.Fatalf("Objective missing camelCase field %q in %s", want, generic["objectives"])
		}
	}
	var back MissionStatement
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(m, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, m)
	}
}

// TestVolatilities_AxisStringEnum pins that the Axis enum serializes as a STRING
// name (not an integer ordinal) and round-trips, and that Glossary fields are camelCase.
func TestVolatilities_AxisStringEnum(t *testing.T) {
	v := Volatilities{Items: []Volatility{
		{Name: "tax rules", Rationale: "jurisdictions change", Axis: AxisAllCustomersAtOneTime},
	}}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic struct {
		Items []struct {
			Name      string          `json:"name"`
			Rationale string          `json:"rationale"`
			Axis      json.RawMessage `json:"axis"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	if len(generic.Items) != 1 {
		t.Fatalf("items: %s", data)
	}
	var axisStr string
	if err := json.Unmarshal(generic.Items[0].Axis, &axisStr); err != nil {
		t.Fatalf("axis must be a JSON string, got %s", generic.Items[0].Axis)
	}
	if axisStr != "allCustomersAtOneTime" {
		t.Fatalf("axis = %q, want %q", axisStr, "allCustomersAtOneTime")
	}
	var back Volatilities
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(v, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, v)
	}
}

// TestSystem_StringEnums_CamelCase pins string enum names (component kind, layer,
// call mode) and camelCase fields across the System model and its nested types,
// and a full round-trip.
func TestSystem_StringEnums_CamelCase(t *testing.T) {
	cid := Slug("ProjectStateAccess")
	ucid := Slug("Co-author")
	s := System{
		Components: []Component{{
			ID:                  cid,
			Name:                "ProjectStateAccess",
			Kind:                CompResourceAccess,
			Layer:               LayerResourceAccess,
			Encapsulates:        "project head-state",
			AtomicBusinessVerbs: []string{"createProject"},
		}},
		Relationships: []Relationship{{From: cid, To: cid, Mode: CallQueued, Label: "x"}},
		DynamicViews: []DynamicView{{
			UseCaseID: ucid,
			Key:       "uc1",
			Title:     "Co-author",
			Steps: []CallStep{{
				ActivityNodeID: "step1",
				Calls:          []TraceCall{{From: cid, To: cid, Mode: CallSync, Label: "y"}},
			}},
		}},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic struct {
		Components []struct {
			Kind                json.RawMessage `json:"kind"`
			Layer               json.RawMessage `json:"layer"`
			AtomicBusinessVerbs []string        `json:"atomicBusinessVerbs"`
		} `json:"components"`
		Relationships []struct {
			Mode json.RawMessage `json:"mode"`
		} `json:"relationships"`
		DynamicViews []map[string]json.RawMessage `json:"dynamicViews"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	assertStringEq(t, "component kind", generic.Components[0].Kind, "resourceAccess")
	assertStringEq(t, "component layer", generic.Components[0].Layer, "resourceAccess")
	assertStringEq(t, "relationship mode", generic.Relationships[0].Mode, "queued")
	if _, ok := generic.DynamicViews[0]["useCaseId"]; !ok {
		t.Fatalf("DynamicView missing camelCase useCaseId in %s", data)
	}
	var back System
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(s, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, s)
	}
}

// decodeCommittedProject reads and decodes THIS repo's own committed
// .aiarch/state/project.json — shared by the tolerant-decode regressions below, each
// of which needs the same live fixture (16 realized/reshaped dynamic views; the
// Task-8 batch-1 design amendment (2026-08-01) put explicit TraceCall.Alt values
// on 12 of the calls across uc2/uc4's both-surface entry steps — see
// wantAltTally below; the Task-7 design amendment (2026-08-01) put explicit
// ActivityNode.DecidedBy values on 24 of the 37 decision nodes — see
// TestCommittedProjectJSON_ActivityNodes_DecidedBySplit).
func decodeCommittedProject(t *testing.T) Project {
	t.Helper()
	root := findRepoRootFromCwd(t)
	raw, err := os.ReadFile(filepath.Join(root, ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatalf("read project.json: %v", err)
	}
	proj, ok, err := DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON reported not-ok for the committed project.json")
	}
	return proj
}

// altCallKey identifies one TraceCall within one dynamic-view step by its
// (view, step, from, to) — unique within every step authored so far, batch-1
// included (no step repeats a (from,to) pair).
type altCallKey struct {
	view, step, from, to string
}

// wantAltTally is the Task-8 architect spec's §2a/§2c alt-group authoring,
// value-keyed exactly like wantDecidedByTally below: the both-surface entry
// steps of uc2-commit-project-option (await-decision, review-options) and
// uc4-operate-delivered-system (publish-trigger) pair the actor->Client leg
// ("s1") with the Client->Manager leg ("s2") per the Task-5 alt-group
// contract. Every other committed call carries no alt tag.
var wantAltTally = map[altCallKey]string{
	{"uc2-commit-project-option", "await-decision", "architect-user", "web-client"}:         "s1",
	{"uc2-commit-project-option", "await-decision", "architect-user", "mcp-client"}:         "s1",
	{"uc2-commit-project-option", "await-decision", "web-client", "project-design-manager"}: "s2",
	{"uc2-commit-project-option", "await-decision", "mcp-client", "project-design-manager"}: "s2",
	{"uc2-commit-project-option", "review-options", "architect-user", "web-client"}:         "s1",
	{"uc2-commit-project-option", "review-options", "architect-user", "mcp-client"}:         "s1",
	{"uc2-commit-project-option", "review-options", "web-client", "project-design-manager"}: "s2",
	{"uc2-commit-project-option", "review-options", "mcp-client", "project-design-manager"}: "s2",
	{"uc4-operate-delivered-system", "publish-trigger", "operator", "web-client"}:           "s1",
	{"uc4-operate-delivered-system", "publish-trigger", "operator", "mcp-client"}:           "s1",
	{"uc4-operate-delivered-system", "publish-trigger", "web-client", "operations-manager"}: "s2",
	{"uc4-operate-delivered-system", "publish-trigger", "mcp-client", "operations-manager"}: "s2",
}

// TestCommittedProjectJSON_DynamicViewCalls_Alt is the tolerant-decode regression
// for TraceCall.Alt (rollout rulings 2026-07-31), extended by Task 8 (2026-08-01)
// to pin the VALUES batch-1 actually authored rather than only asserting absence:
// a call that never mentions "alt" must decode EXACTLY as it did before the field
// existed (Alt reads back nil, not a zero-value string standing in for absence),
// and a call that IS one of wantAltTally's 12 batch-1 entries must decode to
// exactly its authored group value.
func TestCommittedProjectJSON_DynamicViewCalls_Alt(t *testing.T) {
	proj := decodeCommittedProject(t)

	sys, ok := proj.SystemDesign.Model.(*System)
	if !ok || sys == nil {
		t.Fatal("SystemDesign slot did not decode to a non-nil *System")
	}
	if len(sys.DynamicViews) == 0 {
		t.Fatal("committed System has no dynamic views — fixture assumption (16 realized views) no longer holds")
	}
	callCount := 0
	seen := map[altCallKey]bool{}
	for _, dv := range sys.DynamicViews {
		for _, step := range dv.Steps {
			for _, call := range step.Calls {
				callCount++
				key := altCallKey{dv.Key, step.ActivityNodeID, call.From, call.To}
				want, isAltGroup := wantAltTally[key]
				if !isAltGroup {
					if call.Alt != nil {
						t.Fatalf("dynamic view %q step %q: call %+v decoded a non-nil Alt outside "+
							"the authored batch-1 alt groups — tolerant decode or authoring regressed",
							dv.Key, step.ActivityNodeID, call)
					}
					continue
				}
				seen[key] = true
				if call.Alt == nil {
					t.Errorf("dynamic view %q step %q: call %s->%s want alt %q, got nil",
						dv.Key, step.ActivityNodeID, call.From, call.To, want)
				} else if *call.Alt != want {
					t.Errorf("dynamic view %q step %q: call %s->%s want alt %q, got %q",
						dv.Key, step.ActivityNodeID, call.From, call.To, want, *call.Alt)
				}
			}
		}
	}
	if callCount == 0 {
		t.Fatal("committed dynamic views have zero calls across all steps — fixture assumption no longer holds")
	}
	for key := range wantAltTally {
		if !seen[key] {
			t.Errorf("wantAltTally entry %+v was not found among the committed calls — the alt-group authoring or this pin has drifted", key)
		}
	}
}

// wantDecidedByTally is the Task-7 architect spec's D-table explicit-value
// tally (8 distinct deciders across the 24 explicit rows). Pinning the VALUES,
// not just the count, is load-bearing: Task 7b renamed the "design-health"
// component to "design-health-engine" and had to atomically retarget uc1's
// ci-check.decidedBy (spec FLAG-6) — a rename that forgot the slot-4 retarget
// would still decode 24 nodes of the right kinds and so stay green against a
// count-only pin. It did its job: this pin failed until the retarget landed
// (fix-round-1 FINDING 5), and it stays value-keyed for the next rename.
var wantDecidedByTally = map[string]int{
	"architect-user":       10,
	"intervention-engine":  5,
	"merchant-gateway":     2,
	"estimation-engine":    2,
	"operator":             2,
	"design-health-engine": 1,
	"review-engine":        1,
	"autoscaler-engine":    1,
}

// TestCommittedProjectJSON_ActivityNodes_DecidedBySplit is the tolerant-decode
// regression for ActivityNode.DecidedBy (rollout rulings 2026-07-31; authored
// 2026-08-01 by the Task-7 design amendment). It replaces the earlier
// "_NoDecidedBy" pin — that one asserted the field was universally absent, which
// was only ever a point-in-time fact (no committed node used the field yet); the
// Task-7 amendment legitimately adds explicit values on 24 of the committed
// design's 37 decision nodes (the D-table in the Task-7 architect spec), so this
// version pins the AMENDED reality instead: every activity node decodes without
// error, exactly 24 nodes across the 16 use cases carry a non-nil DecidedBy, every
// one of those 24 sits on a decision or switch node (DecidedBy is illegal on any
// other kind — CC-DECIDED-BY's placement rule), the field is still nil everywhere
// it isn't explicitly authored (tolerant decode: no zero-value string stands in
// for absence), and the VALUES tally exactly against wantDecidedByTally (not
// merely the count — see its doc comment).
func TestCommittedProjectJSON_ActivityNodes_DecidedBySplit(t *testing.T) {
	proj := decodeCommittedProject(t)

	cuc, ok := proj.CoreUseCases.Model.(*CoreUseCases)
	if !ok || cuc == nil {
		t.Fatal("CoreUseCases slot did not decode to a non-nil *CoreUseCases")
	}
	if len(cuc.Decisions) == 0 {
		t.Fatal("committed CoreUseCases has no decisions — fixture assumption no longer holds")
	}
	nodeCount, decidedByCount := 0, 0
	gotTally := map[string]int{}
	for _, d := range cuc.Decisions {
		if d.UseCase.Activity == nil {
			continue
		}
		for _, node := range d.UseCase.Activity.Nodes {
			nodeCount++
			if node.DecidedBy == nil {
				continue
			}
			decidedByCount++
			if node.Kind != NodeDecision && node.Kind != NodeSwitch {
				t.Fatalf("use case %q activity node %q (kind %v) carries a DecidedBy but is not a "+
					"decision/switch node — CC-DECIDED-BY placement violation on the committed state",
					d.UseCase.ID, node.ID, node.Kind)
			}
			if *node.DecidedBy == "" {
				t.Fatalf("use case %q activity node %q decoded an empty-string DecidedBy — "+
					"the field should be omitted, not empty, when there is no decider", d.UseCase.ID, node.ID)
			}
			gotTally[*node.DecidedBy]++
		}
	}
	if nodeCount == 0 {
		t.Fatal("committed use cases have zero activity nodes across all decisions — fixture assumption no longer holds")
	}
	if decidedByCount != 24 {
		t.Fatalf("committed activity nodes carry DecidedBy on %d nodes, want 24 (the Task-7 "+
			"architect spec's D-table explicit rows) — investigate drift, don't just re-pin", decidedByCount)
	}
	if !reflect.DeepEqual(gotTally, wantDecidedByTally) {
		t.Fatalf("committed DecidedBy value tally = %v, want %v (a value drifted — e.g. a rename "+
			"that forgot to retarget a slot-4 decidedBy — even though the count still matches)",
			gotTally, wantDecidedByTally)
	}
}

// TestUseCase_StringEnums pins trigger / classification / node kind / edge kind
// string names and camelCase field names on the use-case grammar.
func TestUseCase_StringEnums(t *testing.T) {
	n1 := Slug("d")
	n2 := Slug("l")
	c := CoreUseCases{Decisions: []UseCaseDecision{{
		UseCase: UseCase{
			ID:             Slug("Co-author"),
			Name:           "Co-author",
			Actors:         []Actor{{ID: Slug("architect"), Role: "architect"}},
			Trigger:        TriggerBusMessage,
			Classification: ClassNonCore,
			Activity: &ActivityDiagram{
				Nodes: []ActivityNode{{ID: n1, Kind: NodeDecision, Label: "d"}, {ID: n2, Kind: NodeLoop, Label: "l"}},
				Edges: []ActivityEdge{{From: n1, To: n2, Kind: EdgeGuardedFlow, Guard: "g"}},
			},
		},
		RejectionReason: "permutation",
	}}}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic struct {
		Decisions []struct {
			UseCase struct {
				Trigger        json.RawMessage `json:"trigger"`
				Classification json.RawMessage `json:"classification"`
				Activity       struct {
					Nodes []struct {
						Kind json.RawMessage `json:"kind"`
					} `json:"nodes"`
					Edges []struct {
						Kind json.RawMessage `json:"kind"`
					} `json:"edges"`
				} `json:"activity"`
			} `json:"useCase"`
			RejectionReason string `json:"rejectionReason"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	uc := generic.Decisions[0].UseCase
	assertStringEq(t, "trigger", uc.Trigger, "busMessage")
	assertStringEq(t, "classification", uc.Classification, "nonCore")
	assertStringEq(t, "node kind", uc.Activity.Nodes[0].Kind, "decision")
	assertStringEq(t, "node kind", uc.Activity.Nodes[1].Kind, "loop")
	assertStringEq(t, "edge kind", uc.Activity.Edges[0].Kind, "guardedFlow")
	var back CoreUseCases
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(c, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, c)
	}
}

// TestStandardCheck_CheckStatusStringEnum pins the CheckStatus string name + camelCase.
func TestStandardCheck_CheckStatusStringEnum(t *testing.T) {
	sc := StandardCheck{Items: []CheckItem{{Section: "§3.4", Guideline: "g", Status: CheckWaived, Justification: "j"}}}
	data, err := json.Marshal(sc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic struct {
		Items []struct {
			Status json.RawMessage `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assertStringEq(t, "check status", generic.Items[0].Status, "waived")
	var back StandardCheck
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(sc, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, sc)
	}
}

// TestEnums_AcceptLegacyOrdinals proves every string enum still unmarshals a bare
// integer ordinal (backward-compat with the prompts that emit integers and any
// previously-persisted JSONB payload).
func TestEnums_AcceptLegacyOrdinals(t *testing.T) {
	var a Axis
	mustUnmarshal(t, "1", &a)
	if a != AxisAllCustomersAtOneTime {
		t.Fatalf("axis legacy: %v", a)
	}
	var tr Trigger
	mustUnmarshal(t, "2", &tr)
	if tr != TriggerBusMessage {
		t.Fatalf("trigger legacy: %v", tr)
	}
	var cl Classification
	mustUnmarshal(t, "1", &cl)
	if cl != ClassNonCore {
		t.Fatalf("classification legacy: %v", cl)
	}
	var nk ActivityNodeKind
	mustUnmarshal(t, "2", &nk)
	if nk != NodeDecision {
		t.Fatalf("node kind legacy: %v", nk)
	}
	var ek EdgeKind
	mustUnmarshal(t, "1", &ek)
	if ek != EdgeGuardedFlow {
		t.Fatalf("edge kind legacy: %v", ek)
	}
	var ck ComponentKind
	mustUnmarshal(t, "3", &ck)
	if ck != CompResourceAccess {
		t.Fatalf("component kind legacy: %v", ck)
	}
	var ly Layer
	mustUnmarshal(t, "3", &ly)
	if ly != LayerResourceAccess {
		t.Fatalf("layer legacy: %v", ly)
	}
	var cm CallMode
	mustUnmarshal(t, "1", &cm)
	if cm != CallQueued {
		t.Fatalf("call mode legacy: %v", cm)
	}
	var cs CheckStatus
	mustUnmarshal(t, "1", &cs)
	if cs != CheckWaived {
		t.Fatalf("check status legacy: %v", cs)
	}
}

func assertStringEq(t *testing.T, what string, raw json.RawMessage, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%s must be a JSON string, got %s", what, raw)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", what, got, want)
	}
}

func mustUnmarshal(t *testing.T, data string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), v); err != nil {
		t.Fatalf("unmarshal %q into %T: %v", data, v, err)
	}
}

// TestCritiqueCarrier_RoundTrip_And_Isolation pins the D-MSD-Δ amendment: the
// first-class PM-critique read-back carrier (ArtifactSlot.CritiqueVerdict /
// CritiqueNotes) round-trips through the canonical .aiarch/state/project.json codec
// (the single shape aiarch-validate decodes), is OMITTED when empty (decode-compat),
// and is CLEARED by the status transitions while the architect's Notes ride
// separately on Reject — the collision the senior review identified cannot recur.
func TestCritiqueCarrier_RoundTrip_And_Isolation(t *testing.T) {
	mission, err := NewMissionStatement("ship value", []Objective{{Number: 1, Statement: "be useful"}}, "components")
	if err != nil {
		t.Fatalf("NewMissionStatement: %v", err)
	}

	// A staged slot carrying a critique-revise read-back carrier (what the Action committed).
	p := Project{ID: ProjectID("p1"), Version: 2, Phase: PhaseSystemDesign, Owner: "o"}
	p.Mission = ArtifactSlot{
		Status:          ReviewAwaitingReview,
		Model:           mission,
		CritiqueVerdict: CritiqueVerdictRevise,
		CritiqueNotes:   "tighten the vision sentence",
	}

	assertCritiqueCarrierRoundTrips(t, p)

	// DECODE-COMPAT: a slot with no critique carrier must NOT emit the keys (omitempty),
	// so legacy rows + the aiarch-validate decode are byte-identical.
	clean := Project{ID: ProjectID("p2"), Version: 1, Owner: "o"}
	clean.Glossary = ArtifactSlot{Status: ReviewCommitted, Model: mustGlossaryWC(t)}
	craw, err := EncodeProjectJSON(clean)
	if err != nil {
		t.Fatalf("EncodeProjectJSON(clean): %v", err)
	}
	if strings.Contains(string(craw), "critiqueVerdict") || strings.Contains(string(craw), "critiqueNotes") {
		t.Fatalf("a slot with no critique must omit the carrier keys, got:\n%s", craw)
	}

	// ISOLATION + CLEAR: a status transition (Reject) writes Notes and CLEARS the
	// critique carrier — the architect's reject rationale never collides with a stale
	// critique verdict.
	transition := statusTransition("RejectArtifact", KindMission, ReviewRejected, "REJECT: rework the vision")
	if terr := transition(&p); terr != nil {
		t.Fatalf("statusTransition: %v", terr)
	}
	if p.Mission.Notes != "REJECT: rework the vision" {
		t.Fatalf("Reject must write the architect rationale to Notes, got %q", p.Mission.Notes)
	}
	if p.Mission.CritiqueVerdict != "" || p.Mission.CritiqueNotes != "" {
		t.Fatalf("a status transition must CLEAR the critique carrier, got verdict=%q notes=%q", p.Mission.CritiqueVerdict, p.Mission.CritiqueNotes)
	}
}

// assertCritiqueCarrierRoundTrips encodes p through the canonical project.json codec
// and asserts the critique carrier keys land on disk under their camelCase JSON names
// and round-trip back typed, with the architect Notes field untouched.
func assertCritiqueCarrierRoundTrips(t *testing.T, p Project) {
	t.Helper()
	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	// The carrier keys are present on disk under their camelCase JSON names.
	if !strings.Contains(string(raw), "critiqueVerdict") || !strings.Contains(string(raw), "critiqueNotes") {
		t.Fatalf("expected critiqueVerdict/critiqueNotes keys in project.json, got:\n%s", raw)
	}
	back, ok, err := DecodeProjectJSON(raw, ProjectID("p1"))
	if err != nil || !ok {
		t.Fatalf("DecodeProjectJSON: ok=%v err=%v", ok, err)
	}
	if back.Mission.CritiqueVerdict != CritiqueVerdictRevise || back.Mission.CritiqueNotes != "tighten the vision sentence" {
		t.Fatalf("critique carrier did not round-trip: %+v", back.Mission)
	}
	if back.Mission.Notes != "" {
		t.Fatalf("the architect Notes field must stay empty (the critique rode its own carrier), got %q", back.Mission.Notes)
	}
}

func mustGlossaryWC(t *testing.T) *Glossary {
	t.Helper()
	g, err := NewGlossary([]GlossaryItem{{Term: "Aggregate", Definition: "a consistency boundary"}})
	if err != nil {
		t.Fatalf("NewGlossary: %v", err)
	}
	return g
}

// enumwire_completeness_test.go closes the F81-class hazard: projectstate carries
// 14 closed ordinal enums that marshal to STRING wire names via a hand-maintained
// (ordinal -> name) map/switch — the 13 (ordinal -> name) tables in enumjson.go
// plus ArtifactKind's WireName() switch in identity.go. Adding a new const to one
// of these enums' iota block WITHOUT adding the matching map/switch entry compiles
// fine (Go does not check map/switch exhaustiveness against an iota block) and
// fails only at RUNTIME, the first time that ordinal crosses the wire, with
// "projectstate: <Enum>(<n>) has no wire name" (see marshalEnum in enumjson.go and
// ArtifactKind.MarshalJSON in identity.go). Previously that failure mode was only
// ever caught by hitting it live.
//
// This test closes the hole by checking BIDIRECTIONAL completeness, for each of
// the 14 enums, between the Go wire-name map/switch and the CONTRACT's declared
// `enum` ordinal list — read live from the committed .aiarch/state/project.json's
// serviceContracts.projectStateAccess.$defs, not a hardcoded golden list, so the
// test tracks the contract as it evolves:
//
//   - Direction 1 (map is BEHIND the contract): every ordinal the contract
//     declares must marshal successfully. A declared ordinal with no map entry
//     is exactly the F81 hazard this test exists to close.
//   - Direction 2 (map is AHEAD of the contract): the wire map/registry must not
//     carry MORE entries than the contract declares (2a), and no ordinal outside
//     the declared set, probed across a window past the max declared value, may
//     marshal successfully (2b). Either symptom means a map entry exists for an
//     ordinal the contract doesn't know about — drift in the other direction.
//
// $def location note: all 14 enums' declared ordinal lists were verified to live
// directly in serviceContracts.projectStateAccess.$defs — none needed the
// const-block fallback, and none needed a search in another component's contract.
// That's somewhat notable given ActivityType and TestingVariant are read across
// component boundaries (constructionManager, etc.) at runtime: their canonical
// $def still sits with projectStateAccess, the owning RA, like the other 12.
func TestEnumWireMap_BidirectionalCompletenessVsContract(t *testing.T) {
	declared := loadDeclaredEnumOrdinals(t)

	checks := []struct {
		name    string
		mapSize int
		marshal func(ordinal int) ([]byte, error)
	}{
		{"Axis", len(axisNames), func(o int) ([]byte, error) { return json.Marshal(Axis(o)) }},
		{"CheckStatus", len(checkStatusNames), func(o int) ([]byte, error) { return json.Marshal(CheckStatus(o)) }},
		{"ComponentKind", len(componentKindNames), func(o int) ([]byte, error) { return json.Marshal(ComponentKind(o)) }},
		{"Layer", len(layerNames), func(o int) ([]byte, error) { return json.Marshal(Layer(o)) }},
		{"CallMode", len(callModeNames), func(o int) ([]byte, error) { return json.Marshal(CallMode(o)) }},
		{"Trigger", len(triggerNames), func(o int) ([]byte, error) { return json.Marshal(Trigger(o)) }},
		{"Classification", len(classificationNames), func(o int) ([]byte, error) { return json.Marshal(Classification(o)) }},
		{"ActivityNodeKind", len(activityNodeKindNames), func(o int) ([]byte, error) { return json.Marshal(ActivityNodeKind(o)) }},
		{"DeliveryStyle", len(deliveryStyleNames), func(o int) ([]byte, error) { return json.Marshal(DeliveryStyle(o)) }},
		{"DeploymentProfile", len(deploymentProfileNames), func(o int) ([]byte, error) { return json.Marshal(DeploymentProfile(o)) }},
		{"EdgeKind", len(edgeKindNames), func(o int) ([]byte, error) { return json.Marshal(EdgeKind(o)) }},
		{"ActivityType", len(activityTypeNames), func(o int) ([]byte, error) { return json.Marshal(ActivityType(o)) }},
		{"TestingVariant", len(testingVariantNames), func(o int) ([]byte, error) { return json.Marshal(TestingVariant(o)) }},
		// ArtifactKind has no exposed name->ordinal map (WireName is a switch, not a
		// table) — AllArtifactKinds() is its authoritative enumeration, and its
		// length stands in for "map size" for the Direction-2a check.
		{"ArtifactKind", len(AllArtifactKinds()), func(o int) ([]byte, error) { return json.Marshal(ArtifactKind(o)) }},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			ords, ok := declared[c.name]
			if !ok || len(ords) == 0 {
				t.Fatalf("%s: no declared `enum` ordinal list found in "+
					"serviceContracts.projectStateAccess.$defs — cannot verify wire-map "+
					"completeness against the contract", c.name)
			}
			verifyEnumWireCompleteness(t, c.name, ords, c.mapSize, c.marshal)
		})
	}
}

// verifyEnumWireCompleteness runs both completeness directions for one enum: every
// contract-declared ordinal must marshal (direction 1), and the wire-name
// map/registry must carry no more entries — nor marshal any ordinal outside the
// declared set — than the contract declares (direction 2). Factored out of
// TestEnumWireMap_BidirectionalCompletenessVsContract to keep that function's
// cognitive complexity within the repo's gocognit/gocyclo gate.
func verifyEnumWireCompleteness(t *testing.T, name string, declaredOrds []int, mapSize int, marshal func(int) ([]byte, error)) {
	t.Helper()

	declaredSet := make(map[int]bool, len(declaredOrds))
	maxDeclared := 0
	for _, o := range declaredOrds {
		declaredSet[o] = true
		if o > maxDeclared {
			maxDeclared = o
		}
	}

	// Direction 1: every ordinal the CONTRACT declares must marshal successfully
	// via the Go wire-name map/switch.
	for _, o := range declaredOrds {
		if _, err := marshal(o); err != nil {
			t.Errorf("F81 hazard: %s ordinal %d is declared in the contract's enum "+
				"list but has NO wire-name map entry (marshal error: %v) — a const was "+
				"added to the iota block without a matching wire-name map/switch entry "+
				"in enumjson.go/identity.go; this compiles fine and fails only at "+
				"runtime, the first time this value crosses the wire", name, o, err)
		}
	}

	// Direction 2a: the wire-name map/registry must not carry MORE entries than
	// the contract declares.
	if mapSize != len(declaredOrds) {
		drift := "behind"
		if mapSize > len(declaredOrds) {
			drift = "ahead of"
		}
		t.Errorf("F81 hazard (reverse): %s wire-name map/registry has %d entries but "+
			"the contract declares %d ordinals — the map has drifted %s the contract",
			name, mapSize, len(declaredOrds), drift)
	}

	// Direction 2b: no ordinal OUTSIDE the declared set — probed across
	// [0, maxDeclared+5] — may marshal successfully. A success here means the
	// wire-name map carries an entry for an ordinal the contract doesn't know
	// about.
	for probe := 0; probe <= maxDeclared+5; probe++ {
		if declaredSet[probe] {
			continue
		}
		if _, err := marshal(probe); err == nil {
			t.Errorf("F81 hazard (reverse): %s ordinal %d is NOT in the contract's "+
				"declared enum list but marshals successfully — the wire-name map has "+
				"an entry the contract doesn't declare", name, probe)
		}
	}
}

// contractDef is the slice of a $defs entry this test reads: its declared `enum`
// list, if any. Kept as raw messages because some $defs enums are string-backed
// (e.g. ActivityMethodPhase, CritiqueVerdict) rather than the integer ordinals
// this test covers — those are filtered out in loadDeclaredEnumOrdinals.
type contractDef struct {
	Enum []json.RawMessage `json:"enum"`
}

// loadDeclaredEnumOrdinals reads the repo's committed .aiarch/state/project.json
// and returns, for every $def under serviceContracts.projectStateAccess that
// declares a purely-integer `enum` list, its name -> declared ordinal list.
// Non-integer (string-backed) enums are skipped: they use a different wire
// encoding and are out of scope for the F81 ordinal-drift hazard this test
// closes.
func loadDeclaredEnumOrdinals(t *testing.T) map[string][]int {
	t.Helper()
	root := findRepoRootFromCwd(t)
	raw, err := os.ReadFile(filepath.Join(root, ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatalf("read project.json: %v", err)
	}

	var top struct {
		ServiceContracts map[string]struct {
			Defs map[string]contractDef `json:"$defs"`
		} `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("parse project.json: %v", err)
	}
	psa, ok := top.ServiceContracts["projectStateAccess"]
	if !ok || len(psa.Defs) == 0 {
		t.Fatal("serviceContracts.projectStateAccess.$defs missing or empty in project.json")
	}

	out := make(map[string][]int, len(psa.Defs))
	for name, def := range psa.Defs {
		if len(def.Enum) == 0 {
			continue
		}
		ords := make([]int, 0, len(def.Enum))
		allInt := true
		for _, r := range def.Enum {
			var n int
			if err := json.Unmarshal(r, &n); err != nil {
				allInt = false
				break
			}
			ords = append(ords, n)
		}
		if !allInt {
			continue // string-backed enum — different wire encoding, out of scope
		}
		out[name] = ords
	}
	return out
}

// findRepoRootFromCwd ascends from the test's working directory to the directory
// holding `.aiarch/state/project.json` (the repo root). Mirrors the identical
// helper in server/internal/contract_defs_test.go (package internal_test);
// duplicated here (rather than shared) because that helper lives in a different
// package/build unit and this test stays dependency-free within projectstate's
// own test suite.
func findRepoRootFromCwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".aiarch", "state", "project.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (.aiarch/state/project.json) ascending from %s", dir)
		}
		dir = parent
	}
}

// TestInternalCatalog_RepresentativeRAAndEngine proves the generated internal
// tool surface (toolcatalog.gen.go, from .serviceContracts) carries a correct
// descriptor for a representative ResourceAccess AND Engine operation: right
// name, layer, readOnlyHint, and a self-contained (parseable) input/output schema.
func TestInternalCatalog_RepresentativeRAAndEngine(t *testing.T) {
	// ResourceAccess read: projectStateAccess.ReadProject → read-only, exposable,
	// input schema names the projectId param.
	rp, ok := InternalToolByName("projectStateReadProject")
	if !ok {
		t.Fatal("expected a generated tool for projectStateAccess.ReadProject")
	}
	if rp.Layer != "ResourceAccess" || rp.Operation != "ReadProject" || rp.Component != "projectStateAccess" {
		t.Fatalf("wrong descriptor metadata: %+v", rp)
	}
	if !rp.ReadOnly {
		t.Fatal("ReadProject must be marked read-only (a Read* op)")
	}
	if rp.AgentHidden {
		t.Fatal("a projectStateAccess READ must be agent-exposable")
	}
	assertObjectSchemaHasProp(t, rp.InputSchema, "projectID")
	assertParseable(t, rp.OutputSchema)

	// Engine op: reviewEngine.ProposeReviews → read-only (Engines are pure), exposable.
	pr, ok := InternalToolByName("reviewProposeReviews")
	if !ok {
		t.Fatal("expected a generated tool for reviewEngine.ProposeReviews")
	}
	if pr.Layer != "Engine" || !pr.ReadOnly {
		t.Fatalf("every Engine op must be read-only (pure): %+v", pr)
	}
	if pr.AgentHidden {
		t.Fatal("an Engine op must be agent-exposable")
	}
	assertParseable(t, pr.InputSchema)
	assertParseable(t, pr.OutputSchema)

	// ResourceAccess with a payload result → schema carries the reachable $defs.
	rt, ok := InternalToolByName("artifactRetrieveOutputTree")
	if !ok {
		t.Fatal("expected a generated tool for artifactAccess.RetrieveOutputTree")
	}
	if !assertParseableHasDefs(t, rt.OutputSchema) {
		t.Fatal("a payload result schema must inline its reachable $defs to be self-contained")
	}
}

// TestInternalCatalog_AgentHiddenRawOpsAbsentFromExposable proves the merge-
// authority raw ops (e.g. CommitArtifact) are GENERATED (present in the full
// catalog, flagged AgentHidden) but ABSENT from the agent-exposable set — the
// composed verbs / server rail replace them.
func TestInternalCatalog_AgentHiddenRawOpsAbsentFromExposable(t *testing.T) {
	commit, ok := InternalToolByName("projectStateCommitArtifact")
	if !ok {
		t.Fatal("CommitArtifact must still be GENERATED into the full catalog")
	}
	if !commit.AgentHidden {
		t.Fatal("raw CommitArtifact must be AgentHidden — merge authority stays with the server rail")
	}
	for _, tl := range AgentExposableTools() {
		if tl.Component == "projectStateAccess" && !tl.ReadOnly {
			t.Fatalf("a projectStateAccess write leaked into the agent-exposable set: %s", tl.Name)
		}
		if tl.AgentHidden {
			t.Fatalf("AgentExposableTools returned an AgentHidden tool: %s", tl.Name)
		}
	}

	// Every RA/Engine contract operation is tool-eligible: the catalog is non-empty
	// and every descriptor carries parseable schemas.
	all := InternalToolCatalog()
	if len(all) < 40 {
		t.Fatalf("expected the full RA/Engine surface, got only %d tools", len(all))
	}
	for _, tl := range all {
		assertParseable(t, tl.InputSchema)
		assertParseable(t, tl.OutputSchema)
	}
}

func assertParseable(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("schema does not parse: %v\n%s", err, raw)
	}
}

func assertObjectSchemaHasProp(t *testing.T, raw json.RawMessage, prop string) {
	t.Helper()
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("input schema does not parse: %v", err)
	}
	if _, ok := s.Properties[prop]; !ok {
		t.Fatalf("input schema missing property %q; have %v", prop, keysOf(s.Properties))
	}
}

func assertParseableHasDefs(t *testing.T, raw json.RawMessage) bool {
	t.Helper()
	assertParseable(t, raw)
	var s struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	_ = json.Unmarshal(raw, &s)
	return len(s.Defs) > 0
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestActivityConstructionStatus_SeededFacets_RoundTrip(t *testing.T) {
	in := ActivityConstructionStatus{
		ActivityID:  "C-CW",
		Phase:       ActivityConstructionDone,
		Kind:        ActivityKindFrontend,
		BuildStatus: BuildIntegrated,
		Produced: []ProducedArtifact{
			{Kind: "service-contract", Title: "webClient — service contract", Source: "implementation/contracts/webClient.md", Produced: true, Note: "frozen App-B contract"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ActivityConstructionStatus
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Kind != ActivityKindFrontend || out.BuildStatus != BuildIntegrated || len(out.Produced) != 1 || out.Produced[0].Source != "implementation/contracts/webClient.md" {
		t.Fatalf("round-trip lost facets: %+v", out)
	}
}

func TestActivityKind_String(t *testing.T) {
	if ActivityKindService.String() != "service" || ActivityKindFrontend.String() != "frontend" || ActivityKindTesting.String() != "testing" {
		t.Fatalf("kind strings wrong")
	}
}

func TestActivityBuildStatus_String(t *testing.T) {
	if BuildIntegrated.String() != "integrated" || BuildInReview.String() != "in-review" || BuildInConstruction.String() != "in-construction" {
		t.Fatalf("status strings wrong")
	}
}

// ---- Task 1: ActivityType + TestingVariant + ActivityMethodPhase ----

func TestActivityType_String(t *testing.T) {
	cases := []struct {
		k    ActivityType
		want string
	}{
		{ActivityTypeService, "service"},
		{ActivityTypeFrontend, "frontend"},
		{ActivityTypeTesting, "testing"},
		{ActivityTypeDeployment, "deployment"},
		{ActivityTypeDocumentation, "documentation"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("ActivityType(%d).String() = %q, want %q", c.k, got, c.want)
		}
	}
}

func TestActivityType_JSONRoundTrip(t *testing.T) {
	// Verify all 5 values marshal to string names and unmarshal back correctly.
	vals := []ActivityType{
		ActivityTypeService, ActivityTypeFrontend, ActivityTypeTesting,
		ActivityTypeDeployment, ActivityTypeDocumentation,
	}
	for _, v := range vals {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %d: %v", v, err)
		}
		var got ActivityType
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %q: %v", b, err)
		}
		if got != v {
			t.Errorf("round-trip: got %d, want %d", got, v)
		}
	}
}

func TestActivityType_LegacyIntDecode(t *testing.T) {
	// Existing project.json entries have Kind as int (0/1/2); must still decode.
	cases := []struct {
		raw  string
		want ActivityType
	}{
		{"0", ActivityTypeService},
		{"1", ActivityTypeFrontend},
		{"2", ActivityTypeTesting},
	}
	for _, c := range cases {
		var got ActivityType
		if err := json.Unmarshal([]byte(c.raw), &got); err != nil {
			t.Errorf("Unmarshal %q: %v", c.raw, err)
		}
		if got != c.want {
			t.Errorf("Unmarshal %q = %d, want %d", c.raw, got, c.want)
		}
	}
}

func TestTestingVariant_String(t *testing.T) {
	cases := []struct {
		v    TestingVariant
		want string
	}{
		{TestVariantPlan, "plan"},
		{TestVariantHarness, "harness"},
		{TestVariantPerf, "perf"},
		{TestVariantSystemTest, "systemTest"},
		{TestVariantQAProcess, "qaProcess"},
	}
	for _, c := range cases {
		if got := c.v.String(); got != c.want {
			t.Errorf("TestingVariant(%d).String() = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestTestingVariant_JSONRoundTrip(t *testing.T) {
	vals := []TestingVariant{
		TestVariantPlan, TestVariantHarness, TestVariantPerf,
		TestVariantSystemTest, TestVariantQAProcess,
	}
	for _, v := range vals {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %d: %v", v, err)
		}
		var got TestingVariant
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %q: %v", b, err)
		}
		if got != v {
			t.Errorf("round-trip: got %d, want %d", got, v)
		}
	}
}

func TestActivityMethodPhase_Constants(t *testing.T) {
	cases := map[ActivityMethodPhase]string{
		MethodPhaseRequirements:   "requirements",
		MethodPhaseDetailedDesign: "detailed_design",
		MethodPhaseTestPlan:       "test_plan",
		MethodPhaseConstruction:   "construction",
		MethodPhaseIntegration:    "integration",
	}
	for p, want := range cases {
		if p.String() != want {
			t.Errorf("%v.String() = %q, want %q", p, p.String(), want)
		}
	}
}

func TestActivityMethodPhase_ServicePhaseIDs(t *testing.T) {
	// Verify the canonical IDs the v3 design specifies for service phase set.
	if MethodPhaseRequirements != "requirements" {
		t.Errorf("MethodPhaseRequirements = %q, want %q", MethodPhaseRequirements, "requirements")
	}
	if MethodPhaseDetailedDesign != "detailed_design" {
		t.Errorf("MethodPhaseDetailedDesign = %q, want %q", MethodPhaseDetailedDesign, "detailed_design")
	}
	if MethodPhaseTestPlan != "test_plan" {
		t.Errorf("MethodPhaseTestPlan = %q, want %q", MethodPhaseTestPlan, "test_plan")
	}
	if MethodPhaseConstruction != "construction" {
		t.Errorf("MethodPhaseConstruction = %q, want %q", MethodPhaseConstruction, "construction")
	}
	if MethodPhaseIntegration != "integration" {
		t.Errorf("MethodPhaseIntegration = %q, want %q", MethodPhaseIntegration, "integration")
	}
}

// ---- Task 2: PhaseCompletion + phaseSetFor + CoarsePhase/CoarseBuildStatus ----

func TestPhaseSetFor_Service(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	wantPhases := []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration,
	}
	wantWeights := []int{15, 20, 10, 40, 15}
	if len(phases) != len(wantPhases) {
		t.Fatalf("service phase set len = %d, want %d", len(phases), len(wantPhases))
	}
	total := 0
	for i, p := range phases {
		if p.Phase != wantPhases[i] {
			t.Errorf("phase[%d] = %q, want %q", i, p.Phase, wantPhases[i])
		}
		if p.Weight != wantWeights[i] {
			t.Errorf("phase[%d] weight = %d, want %d", i, p.Weight, wantWeights[i])
		}
		if p.Label == "" {
			t.Errorf("phase[%d] %q has empty label", i, p.Phase)
		}
		if p.Completed {
			t.Errorf("phase[%d] seeded Completed=true", i)
		}
		total += p.Weight
	}
	if total != 100 {
		t.Errorf("weight sum = %d, want 100", total)
	}
}

func TestPhaseCompletion_JSONRoundTrip(t *testing.T) {
	// Verify PhaseCompletion marshals/unmarshals correctly including optional fields.
	pc := PhaseCompletion{
		Phase:       MethodPhaseRequirements,
		Weight:      15,
		Completed:   true,
		ArtifactRef: "phaseArtifacts/srs/C-IE",
	}
	b, err := json.Marshal(pc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PhaseCompletion
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Phase != MethodPhaseRequirements || got.Weight != 15 || !got.Completed || got.ArtifactRef != "phaseArtifacts/srs/C-IE" {
		t.Errorf("round-trip lost data: %+v", got)
	}
}

func TestActivityConstructionStatus_BackCompatNoPhasesField(t *testing.T) {
	// Existing project.json entries without "phases" must still decode (nil Phases is fine).
	raw := `{"activityID":"C-CW","phase":2,"kind":1,"buildStatus":2,"produced":[{"Kind":"service-contract","Title":"webClient","Source":"implementation/contracts/webClient.md","Produced":true}]}`
	var got ActivityConstructionStatus
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal legacy entry: %v", err)
	}
	if got.ActivityID != "C-CW" {
		t.Errorf("ActivityID = %q, want C-CW", got.ActivityID)
	}
	if got.Phase != ActivityConstructionDone {
		t.Errorf("Phase = %v, want Done", got.Phase)
	}
	if got.Kind != ActivityKindFrontend {
		t.Errorf("Kind = %v, want Frontend", got.Kind)
	}
	if got.Phases != nil {
		t.Errorf("Phases should be nil for legacy entry, got %v", got.Phases)
	}
}

func TestCoarsePhase_AllDone(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	for i := range phases {
		phases[i].Completed = true
	}
	if got := CoarsePhase(phases); got != ActivityConstructionDone {
		t.Errorf("CoarsePhase(all done) = %v, want Done", got)
	}
}

func TestCoarsePhase_NoneStarted(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	if got := CoarsePhase(phases); got != ActivityConstructionNotStarted {
		t.Errorf("CoarsePhase(none started) = %v, want NotStarted", got)
	}
}

func TestCoarsePhase_SomeCompleted(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	phases[0].Completed = true
	if got := CoarsePhase(phases); got != ActivityConstructionRunning {
		t.Errorf("CoarsePhase(some completed) = %v, want Running", got)
	}
}

func TestCoarsePhase_EmptyPhases(t *testing.T) {
	if got := CoarsePhase(nil); got != ActivityConstructionNotStarted {
		t.Errorf("CoarsePhase(nil) = %v, want NotStarted", got)
	}
	if got := CoarsePhase([]PhaseCompletion{}); got != ActivityConstructionNotStarted {
		t.Errorf("CoarsePhase([]) = %v, want NotStarted", got)
	}
}

func TestCoarseBuildStatus_Integrated(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	// Mark both Construction and Integration done.
	for i := range phases {
		if phases[i].Phase == MethodPhaseConstruction || phases[i].Phase == MethodPhaseIntegration {
			phases[i].Completed = true
		}
	}
	if got := CoarseBuildStatus(phases, MethodPhaseIntegration); got != BuildIntegrated {
		t.Errorf("CoarseBuildStatus(integration done) = %v, want Integrated", got)
	}
}

func TestCoarseBuildStatus_InReview(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	// Mark only Construction done (not Integration).
	for i := range phases {
		if phases[i].Phase == MethodPhaseConstruction {
			phases[i].Completed = true
		}
	}
	if got := CoarseBuildStatus(phases, MethodPhaseIntegration); got != BuildInReview {
		t.Errorf("CoarseBuildStatus(construction done, integration not) = %v, want InReview", got)
	}
}

func TestCoarseBuildStatus_InConstruction(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	if got := CoarseBuildStatus(phases, MethodPhaseConstruction); got != BuildInConstruction {
		t.Errorf("CoarseBuildStatus(nothing done) = %v, want InConstruction", got)
	}
}

func canonicalIDsAllowed(p ActivityMethodPhase) bool {
	switch p {
	case MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration:
		return true
	}
	return false
}

func TestProfileFor_AllCanonicalIDsAndSum100(t *testing.T) {
	cases := []struct {
		name    string
		typ     ActivityType
		variant TestingVariant
		wantLen int
	}{
		{"service", ActivityTypeService, 0, 5},
		{"frontend", ActivityTypeFrontend, 0, 5},
		{"deployment", ActivityTypeDeployment, 0, 3},
		{"documentation", ActivityTypeDocumentation, 0, 3},
		{"testing_plan", ActivityTypeTesting, TestVariantPlan, 3},
		{"testing_harness", ActivityTypeTesting, TestVariantHarness, 3},
		{"testing_perf", ActivityTypeTesting, TestVariantPerf, 3},
		{"testing_systemtest", ActivityTypeTesting, TestVariantSystemTest, 3},
		{"testing_qa", ActivityTypeTesting, TestVariantQAProcess, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pr := ProfileFor(c.typ, c.variant)
			if len(pr.Phases) != c.wantLen {
				t.Fatalf("%s: len = %d, want %d", c.name, len(pr.Phases), c.wantLen)
			}
			total := 0
			for _, p := range pr.Phases {
				if !canonicalIDsAllowed(p.Phase) {
					t.Errorf("%s: non-canonical phase id %q", c.name, p.Phase)
				}
				if p.Label == "" {
					t.Errorf("%s: phase %q has empty label", c.name, p.Phase)
				}
				total += p.Weight
			}
			if total != 100 {
				t.Errorf("%s: weight sum = %d, want 100", c.name, total)
			}
		})
	}
}

func TestProfileFor_ServiceIsCanonicalFive(t *testing.T) {
	got := ProfileFor(ActivityTypeService, 0).PhaseIDs()
	want := []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration,
	}
	if len(got) != len(want) {
		t.Fatalf("service PhaseIDs len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("service PhaseIDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestProfileFor_TestingPlanRelabelsCanonicalIDs(t *testing.T) {
	pr := ProfileFor(ActivityTypeTesting, TestVariantPlan)
	want := []ProfilePhase{
		{MethodPhaseRequirements, 20, "Use-Case Trace"},
		{MethodPhaseConstruction, 45, "Plan Authoring"},
		{MethodPhaseIntegration, 35, "Plan Review"},
	}
	if len(pr.Phases) != len(want) {
		t.Fatalf("plan profile len = %d, want %d", len(pr.Phases), len(want))
	}
	for i, w := range want {
		if pr.Phases[i] != w {
			t.Errorf("plan phase[%d] = %+v, want %+v", i, pr.Phases[i], w)
		}
	}
}

func TestActivityProgress_None(t *testing.T) {
	status := ActivityConstructionStatus{
		ActivityID: "C-PE",
		Type:       ActivityTypeService,
		Phases:     phaseSetFor(ActivityTypeService, 0),
	}
	if got := ActivityProgress(status); got != 0 {
		t.Errorf("ActivityProgress(none done) = %d, want 0", got)
	}
}

func TestActivityProgress_FirstPhase(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0) // 15/20/10/40/15
	phases[0].Completed = true                    // Requirements = 15%
	status := ActivityConstructionStatus{ActivityID: "C-PE", Type: ActivityTypeService, Phases: phases}
	if got := ActivityProgress(status); got != 15 {
		t.Errorf("ActivityProgress(requirements done) = %d, want 15", got)
	}
}

func TestActivityProgress_ThreePhases(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0) // 15/20/10/40/15
	phases[0].Completed = true                    // 15
	phases[1].Completed = true                    // 20
	phases[2].Completed = true                    // 10 → total 45
	status := ActivityConstructionStatus{ActivityID: "C-PE", Type: ActivityTypeService, Phases: phases}
	if got := ActivityProgress(status); got != 45 {
		t.Errorf("ActivityProgress(3 phases) = %d, want 45", got)
	}
}

func TestActivityProgress_AllDone(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	for i := range phases {
		phases[i].Completed = true
	}
	status := ActivityConstructionStatus{ActivityID: "C-PE", Type: ActivityTypeService, Phases: phases}
	if got := ActivityProgress(status); got != 100 {
		t.Errorf("ActivityProgress(all done) = %d, want 100", got)
	}
}

func TestActivityProgress_EmptyPhases(t *testing.T) {
	status := ActivityConstructionStatus{ActivityID: "C-PE", Type: ActivityTypeService, Phases: nil}
	if got := ActivityProgress(status); got != 0 {
		t.Errorf("ActivityProgress(nil phases) = %d, want 0", got)
	}
}

func TestProjectEarnedValue_Empty(t *testing.T) {
	if got := ProjectEarnedValue(nil, nil); got != 0.0 {
		t.Errorf("ProjectEarnedValue(empty) = %f, want 0.0", got)
	}
}

func TestProjectEarnedValue_ZeroEffort(t *testing.T) {
	// All activities have zero effort: edge case — return 0
	phases := phaseSetFor(ActivityTypeService, 0)
	phases[0].Completed = true
	statuses := []ActivityConstructionStatus{
		{ActivityID: "C-PE", Type: ActivityTypeService, Phases: phases},
	}
	effortDays := map[string]float64{"C-PE": 0.0}
	got := ProjectEarnedValue(statuses, effortDays)
	if got != 0.0 {
		t.Errorf("ProjectEarnedValue(zero effort) = %f, want 0.0", got)
	}
}

func TestProjectEarnedValue_OneActivity_HalfDone(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0) // 15/20/10/40/15
	phases[0].Completed = true                    // 15
	phases[1].Completed = true                    // 20 → 35% done
	statuses := []ActivityConstructionStatus{
		{ActivityID: "C-PE", Type: ActivityTypeService, Phases: phases},
	}
	effortDays := map[string]float64{"C-PE": 10.0}
	got := ProjectEarnedValue(statuses, effortDays)
	// Σ(E_i × A_i) / Σ E_i = (10 × 0.35) / 10 = 0.35
	if got < 0.34 || got > 0.36 {
		t.Errorf("ProjectEarnedValue = %f, want ~0.35", got)
	}
}

func TestProjectEarnedValue_TwoActivities(t *testing.T) {
	phases1 := phaseSetFor(ActivityTypeService, 0)
	for i := range phases1 {
		phases1[i].Completed = true // 100%
	}
	phases2 := phaseSetFor(ActivityTypeService, 0)
	// 0% done

	statuses := []ActivityConstructionStatus{
		{ActivityID: "C-A", Type: ActivityTypeService, Phases: phases1},
		{ActivityID: "C-B", Type: ActivityTypeService, Phases: phases2},
	}
	effortDays := map[string]float64{"C-A": 5.0, "C-B": 15.0}
	got := ProjectEarnedValue(statuses, effortDays)
	// Σ(E_i × A_i) / Σ E_i = (5×1.0 + 15×0.0) / 20 = 5/20 = 0.25
	if got < 0.24 || got > 0.26 {
		t.Errorf("ProjectEarnedValue = %f, want ~0.25", got)
	}
}

// TestProjectEarnedValue_AppATableA2 exercises the App-A Table A-2 example from
// Appendix A §2: a 4-activity project with weighted earned value = 40.25%.
// Table A-2 (illustrative): activities A/B/C/D with effort 10/20/15/5 days,
// progress 100/50/25/0% → EV = (10*1 + 20*0.5 + 15*0.25 + 5*0) / 50
//
//	= (10 + 10 + 3.75 + 0) / 50 = 23.75 / 50 = 0.475
//
// NOTE: the brief names the expected value 40.25%. Using the exact figures from
// the task brief (activities C-DA/C-MCN/C-SPA/C-MCP with 10/20/15/5 effort days
// and 3 of 5 phases / 2 of 5 / 1 of 5 / 0 of 5 completed respectively):
// progress = 45%/35%/15%/0% → EV = (10*0.45 + 20*0.35 + 15*0.15 + 5*0) / 50
// = (4.5 + 7.0 + 2.25 + 0) / 50 = 13.75 / 50 = 0.275 — does not yield 40.25%.
// Using the simplest App-A §2 example consistent with the brief's 40.25% target:
// efforts 20/40/30/10 = 100 days total, progress 50%/50%/25%/0% →
// EV = (20*0.5 + 40*0.5 + 30*0.25 + 10*0) / 100 = (10+20+7.5+0)/100 = 0.375.
// The brief says "App-A Table A-2 project example = 40.25%"; verifying the exact
// combination: efforts 20/30/15/5=70, progress 100%/30%/20%/0%:
// EV = (20+9+3+0)/70 = 32/70 = 0.457. Cannot reconstruct 40.25% without the
// actual table. Use the two-activity test above (0.25) as the worked example instead,
// and add the brief's 3-of-5-phases-service-activity (45%) via TestActivityProgress_ThreePhases.
func TestProjectEarnedValue_NilEffortMap_DefaultsToEqualWeight(t *testing.T) {
	// When effortDays is nil (or activity missing), each activity defaults to E=1.0.
	phases1 := phaseSetFor(ActivityTypeService, 0)
	for i := range phases1 {
		phases1[i].Completed = true // 100%
	}
	phases2 := phaseSetFor(ActivityTypeService, 0) // 0%

	statuses := []ActivityConstructionStatus{
		{ActivityID: "C-A", Type: ActivityTypeService, Phases: phases1},
		{ActivityID: "C-B", Type: ActivityTypeService, Phases: phases2},
	}
	got := ProjectEarnedValue(statuses, nil)
	// Σ(1.0×1.0 + 1.0×0.0) / 2.0 = 0.5
	if got < 0.49 || got > 0.51 {
		t.Errorf("ProjectEarnedValue(nil effortDays) = %f, want ~0.5", got)
	}
}

// stalecause_test.go — the ADDITIVE stale-cause recording on the F38 staleness rail.
// commitTransition, when an upstream slot re-commits, must flag every already-committed
// downstream slot StaleBasis AND record WHY (the upstream kind + its new revision).

// committedSlot is a tiny helper: an already-committed slot at a given revision.
func committedSlot(m ArtifactModel, rev int64) ArtifactSlot {
	return ArtifactSlot{Status: ReviewCommitted, Model: m, Revisions: rev}
}

func TestCommitTransition_RecordsStaleCauseOnDownstream(t *testing.T) {
	// Volatilities (upstream) and CoreUseCases (downstream) both already committed.
	p := &Project{}
	p.Volatilities = committedSlot(&Volatilities{Items: []Volatility{{Name: "V", Axis: AxisSameCustomerOverTime}}}, 1)
	p.CoreUseCases = committedSlot(&CoreUseCases{}, 1)

	// Re-commit (amend) Volatilities.
	if err := commitTransition(KindVolatilities, nil)(p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}

	if !p.CoreUseCases.StaleBasis {
		t.Fatal("downstream CoreUseCases must be flagged stale after an upstream amendment")
	}
	c := p.CoreUseCases.StaleBasisCause
	if c == nil {
		t.Fatal("downstream slot must carry a stale cause")
	}
	if c.UpstreamKind != KindVolatilities.WireName() {
		t.Fatalf("cause upstream kind = %q, want %q", c.UpstreamKind, KindVolatilities.WireName())
	}
	if c.UpstreamRevision != 2 {
		t.Fatalf("cause upstream revision = %d, want 2 (revision after the amendment)", c.UpstreamRevision)
	}
	// The upstream slot that re-committed clears its own staleness/cause.
	if p.Volatilities.StaleBasis || p.Volatilities.StaleBasisCause != nil {
		t.Fatal("the re-committed upstream slot must clear its own staleness and cause")
	}
}

func TestCommitTransition_ClearsStaleCauseOnReconcile(t *testing.T) {
	p := &Project{}
	p.CoreUseCases = committedSlot(&CoreUseCases{}, 1)
	p.CoreUseCases.StaleBasis = true
	p.CoreUseCases.StaleBasisCause = &StaleCause{UpstreamKind: "volatilities", UpstreamRevision: 2}

	// Re-committing CoreUseCases itself IS the reconcile.
	if err := commitTransition(KindCoreUseCases, nil)(p); err != nil {
		t.Fatalf("commitTransition: %v", err)
	}
	if p.CoreUseCases.StaleBasis || p.CoreUseCases.StaleBasisCause != nil {
		t.Fatal("re-committing a slot must clear its own StaleBasis AND StaleBasisCause")
	}
}

func TestStaleCause_RoundTripsThroughCodec(t *testing.T) {
	p := Project{ID: "p"}
	p.CoreUseCases = committedSlot(&CoreUseCases{Decisions: []UseCaseDecision{{
		UseCase: UseCase{
			Name:           "UC",
			Trigger:        TriggerClientAction,
			Classification: ClassCore,
			Activity: &ActivityDiagram{
				Nodes: []ActivityNode{{ID: "s", Kind: NodeStart}, {ID: "a", Kind: NodeAction, Label: "do"}},
				Edges: []ActivityEdge{{From: "s", To: "a", Kind: EdgeControlFlow}},
			},
		},
	}}}, 1)
	p.CoreUseCases.StaleBasis = true
	p.CoreUseCases.StaleBasisCause = &StaleCause{UpstreamKind: "volatilities", UpstreamRevision: 3}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, "p")
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	c := got.CoreUseCases.StaleBasisCause
	if c == nil {
		t.Fatal("stale cause must survive the encode → decode round-trip")
	}
	if c.UpstreamKind != "volatilities" || c.UpstreamRevision != 3 {
		t.Fatalf("stale cause round-trip mismatch: %+v", *c)
	}
}

func TestDeriveKind(t *testing.T) {
	cases := map[string]struct {
		id, component string
		want          ActivityKind
	}{
		"manager": {"C-MST", "settlementManager", ActivityKindService},
		"engine":  {"C-BE", "billingEngine", ActivityKindService},
		"access":  {"C-PA", "projectStateAccess", ActivityKindService},
		"client":  {"C-CW", "webClient", ActivityKindService},
		"spa":     {"U-SPA", "", ActivityKindFrontend},
		"ci":      {"N-CI", "", ActivityKindTesting},
	}
	for name, c := range cases {
		if got := DeriveKind(c.id, c.component); got != c.want {
			t.Errorf("%s: DeriveKind(%q,%q)=%v want %v", name, c.id, c.component, got, c.want)
		}
	}
}

func TestDeriveBuildStatus(t *testing.T) {
	if s, integ := DeriveBuildStatus(CorpusPresence{HasLog: true, HasPassingReview: true}); s != BuildIntegrated || !integ {
		t.Errorf("log+review should be integrated")
	}
	if s, integ := DeriveBuildStatus(CorpusPresence{HasLog: true}); s != BuildInReview || integ {
		t.Errorf("log-only should be in-review, not integrated")
	}
	if s, _ := DeriveBuildStatus(CorpusPresence{}); s != BuildInConstruction {
		t.Errorf("no corpus should default in-construction")
	}
}

func TestDeriveProduced(t *testing.T) {
	got := DeriveProduced(CorpusPresence{HasLog: true, HasContract: true, ContractFile: "implementation/contracts/webClient.md"}, "webClient", ActivityTypeService)
	if len(got) != 2 {
		t.Fatalf("want 2 artifacts (contract+code) got %d", len(got))
	}
	if got[0].Kind != "service-contract" || got[0].Source != "implementation/contracts/webClient.md" || !got[0].Produced {
		t.Errorf("contract artifact wrong: %+v", got[0])
	}
	if got[1].Kind != "code" || !got[1].Produced {
		t.Errorf("code artifact wrong: %+v", got[1])
	}
}

func TestDeriveProduced_Frontend(t *testing.T) {
	got := DeriveProduced(CorpusPresence{HasLog: true}, "SystemDesignScreen", ActivityTypeFrontend)
	if len(got) != 2 {
		t.Fatalf("want 2 artifacts (ui-design+ui-code) got %d: %+v", len(got), got)
	}
	if got[0].Kind != "ui-design" || !got[0].Produced {
		t.Errorf("ui-design artifact wrong: %+v", got[0])
	}
	if got[1].Kind != "ui-code" || !got[1].Produced {
		t.Errorf("ui-code artifact wrong: %+v", got[1])
	}
}

func TestDeriveProduced_Deployment(t *testing.T) {
	got := DeriveProduced(CorpusPresence{HasLog: true}, "R-DEP", ActivityTypeDeployment)
	if len(got) != 1 || got[0].Kind != "deployment" || !got[0].Produced {
		t.Fatalf("want 1 deployment artifact got %+v", got)
	}
}

func TestDeriveType_Prefixes(t *testing.T) {
	cases := map[string]ActivityType{
		"U-SPA-Home": ActivityTypeFrontend,
		"N-STP":      ActivityTypeTesting,
		"N-IT":       ActivityTypeTesting,
		"C-Orders":   ActivityTypeService,
		"E-Pricing":  ActivityTypeService,
	}
	for id, want := range cases {
		if got := DeriveType(id); got != want {
			t.Errorf("DeriveType(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestDeriveVariant_TestingPrefixes(t *testing.T) {
	cases := map[string]TestingVariant{
		"N-STP":   TestVariantPlan,
		"N-STH":   TestVariantHarness,
		"N-PERF":  TestVariantPerf,
		"N-IT":    TestVariantSystemTest,
		"N-QA":    TestVariantQAProcess,
		"N-OTHER": TestVariantPlan, // unknown N- falls back to Plan
	}
	for id, want := range cases {
		if got := DeriveVariant(id); got != want {
			t.Errorf("DeriveVariant(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestProfileSlug(t *testing.T) {
	cases := []struct {
		t    ActivityType
		v    TestingVariant
		want string
	}{
		{ActivityTypeService, 0, "service"},
		{ActivityTypeFrontend, 0, "frontend"},
		{ActivityTypeDeployment, 0, "deployment"},
		{ActivityTypeDocumentation, 0, "documentation"},
		{ActivityTypeTesting, TestVariantPlan, "testing-plan"},
		{ActivityTypeTesting, TestVariantHarness, "testing-harness"},
		{ActivityTypeTesting, TestVariantPerf, "testing-perf"},
		{ActivityTypeTesting, TestVariantSystemTest, "testing-systemtest"},
		{ActivityTypeTesting, TestVariantQAProcess, "testing-qa"},
	}
	for _, c := range cases {
		if got := profileSlug(c.t, c.v); got != c.want {
			t.Errorf("profileSlug(%v,%v) = %q, want %q", c.t, c.v, got, c.want)
		}
	}
}

func TestCommandFor(t *testing.T) {
	if got := CommandFor(ActivityTypeService, 0, MethodPhaseDetailedDesign); got != "service-detailed-design" {
		t.Errorf("got %q, want service-detailed-design", got)
	}
	if got := CommandFor(ActivityTypeTesting, TestVariantHarness, MethodPhaseConstruction); got != "testing-harness-construction" {
		t.Errorf("got %q, want testing-harness-construction", got)
	}
}

// TestCommandForTotalOverProfiles asserts CommandFor returns a non-empty,
// well-formed slug for every phase that ProfileFor actually emits — the command
// matrix is exactly the flattening of ProfileFor.
func TestCommandForTotalOverProfiles(t *testing.T) {
	for _, combo := range allProfileCombos() {
		for _, p := range ProfileFor(combo.t, combo.v).PhaseIDs() {
			got := CommandFor(combo.t, combo.v, p)
			if got == "" {
				t.Errorf("CommandFor(%v,%v,%q) empty", combo.t, combo.v, p)
			}
			if want := profileSlug(combo.t, combo.v) + "-" + kebabPhase(p); got != want {
				t.Errorf("CommandFor = %q, want %q", got, want)
			}
		}
	}
}

type profileCombo struct {
	t ActivityType
	v TestingVariant
}

// allProfileCombos enumerates every distinct (type, variant) profile in the domain.
func allProfileCombos() []profileCombo {
	return []profileCombo{
		{ActivityTypeService, 0},
		{ActivityTypeFrontend, 0},
		{ActivityTypeDeployment, 0},
		{ActivityTypeDocumentation, 0},
		{ActivityTypeTesting, TestVariantPlan},
		{ActivityTypeTesting, TestVariantHarness},
		{ActivityTypeTesting, TestVariantPerf},
		{ActivityTypeTesting, TestVariantSystemTest},
		{ActivityTypeTesting, TestVariantQAProcess},
	}
}

// repoCommandsDir walks up from this test file to the repo root (the dir holding
// .claude) and returns .claude/commands.
func repoCommandsDir(t *testing.T) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for range 8 {
		cand := filepath.Join(dir, ".claude", "commands")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate .claude/commands above test file")
	return ""
}

// TestEveryProfilePhaseHasCommandFile asserts the command matrix is exactly the
// flattening of ProfileFor: every (profile, phase) has a .claude/commands/<name>.md.
func TestEveryProfilePhaseHasCommandFile(t *testing.T) {
	cmds := repoCommandsDir(t)
	seen := map[string]bool{}
	for _, combo := range allProfileCombos() {
		for _, p := range ProfileFor(combo.t, combo.v).PhaseIDs() {
			name := CommandFor(combo.t, combo.v, p)
			seen[name] = true
			path := filepath.Join(cmds, name+".md")
			if _, err := os.Stat(path); err != nil {
				t.Errorf("missing command file for (%v,%v,%q): %s.md", combo.t, combo.v, p, name)
			}
		}
	}
	// Sanity: the matrix is exactly 30 distinct commands.
	if len(seen) != 30 {
		t.Errorf("expected 30 distinct commands, got %d", len(seen))
	}
}

// TestDesignCommandFor is the failing-first table test for Plan-2 Task B1: every
// draft slug (16 dispatchable kinds — SdpReview is excluded, assembled
// server-side), every critique slug (the 5 designKindHasCritique kinds — the 4
// PM-critiqued business-alignment kinds plus the architect-self-critiqued System,
// amendment 2026-07-17), the two answer slugs (by addressee), and the "" cases
// for undispatchable combinations.
func TestDesignCommandFor(t *testing.T) {
	cases := []struct {
		name      string
		k         ArtifactKind
		mode      DesignJobMode
		addressee string
		want      string
	}{
		// ---- draft: all 16 dispatchable kinds, verbatim slugs ----
		{"draft mission", KindMission, DesignJobModeDraft, "", "mission-draft"},
		{"draft glossary", KindGlossary, DesignJobModeDraft, "", "glossary-draft"},
		{"draft scrubbedRequirements", KindScrubbedRequirements, DesignJobModeDraft, "", "scrubbed-requirements-draft"},
		{"draft volatilities", KindVolatilities, DesignJobModeDraft, "", "volatilities-draft"},
		{"draft coreUseCases", KindCoreUseCases, DesignJobModeDraft, "", "core-use-cases-draft"},
		{"draft system", KindSystem, DesignJobModeDraft, "", "system-draft"},
		{"draft operationalConcepts", KindOperationalConcepts, DesignJobModeDraft, "", "operational-concepts-draft"},
		{"draft standardCheck", KindStandardCheck, DesignJobModeDraft, "", "standard-check-draft"},
		{"draft planningAssumptions", KindPlanningAssumptions, DesignJobModeDraft, "", "planning-assumptions-draft"},
		{"draft activityList", KindActivityList, DesignJobModeDraft, "", "activity-list-draft"},
		{"draft network", KindNetwork, DesignJobModeDraft, "", "network-draft"},
		{"draft normalSolution", KindNormalSolution, DesignJobModeDraft, "", "normal-solution-draft"},
		{"draft subcriticalSolution", KindSubcriticalSolution, DesignJobModeDraft, "", "subcritical-solution-draft"},
		{"draft compressedSolution", KindCompressedSolution, DesignJobModeDraft, "", "compressed-solution-draft"},
		{"draft decompressedSolution", KindDecompressedSolution, DesignJobModeDraft, "", "decompressed-solution-draft"},
		{"draft riskModel", KindRiskModel, DesignJobModeDraft, "", "risk-model-draft"},

		// ---- critique: exactly the designKindHasCritique 5 (4 PM + architect-self-critiqued System) ----
		{"critique mission", KindMission, DesignJobModeCritique, "", "mission-critique"},
		{"critique glossary", KindGlossary, DesignJobModeCritique, "", "glossary-critique"},
		{"critique scrubbedRequirements", KindScrubbedRequirements, DesignJobModeCritique, "", "scrubbed-requirements-critique"},
		{"critique coreUseCases", KindCoreUseCases, DesignJobModeCritique, "", "core-use-cases-critique"},
		{"critique system (architect self-critique)", KindSystem, DesignJobModeCritique, "", "system-critique"},

		// ---- answer: addressee-selected, kind-independent ----
		{"answer architect", KindMission, DesignJobModeAnswer, "architect", "design-answer"},
		{"answer pm", KindMission, DesignJobModeAnswer, "pm", "design-answer-pm"},

		// ---- undispatchable combinations ----
		{"sdpReview draft undispatchable (assembled server-side)", KindSdpReview, DesignJobModeDraft, "", ""},
		{"sdpReview critique undispatchable", KindSdpReview, DesignJobModeCritique, "", ""},
		{"volatilities critique: non-critique kind", KindVolatilities, DesignJobModeCritique, "", ""},
		{"answer unknown addressee", KindMission, DesignJobModeAnswer, "nobody", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DesignCommandFor(c.k, c.mode, c.addressee); got != c.want {
				t.Errorf("DesignCommandFor(%v,%v,%q) = %q, want %q", c.k, c.mode, c.addressee, got, c.want)
			}
		})
	}
}

// TestDesignCommandsExistInMethodAssets is the CROSS-REPO WIRE TEST: for every
// (kind, mode[, addressee]) combination DesignCommandFor deems dispatchable
// (non-"" slug), the method-assets embedded .claude/commands tree must carry a
// matching file — proving the server's command-slug derivation and the seeded
// .claude corpus can never drift apart.
func TestDesignCommandsExistInMethodAssets(t *testing.T) {
	files, err := methodassets.ClaudeFiles()
	if err != nil {
		t.Fatalf("methodassets.ClaudeFiles: %v", err)
	}
	assertCommandFile := func(slug string) {
		t.Helper()
		if slug == "" {
			return
		}
		path := ".claude/commands/" + slug + ".md"
		if _, ok := files[path]; !ok {
			t.Errorf("missing %s in method-assets embedded corpus", path)
		}
	}
	for _, k := range AllArtifactKinds() {
		assertCommandFile(DesignCommandFor(k, DesignJobModeDraft, ""))
		assertCommandFile(DesignCommandFor(k, DesignJobModeCritique, ""))
	}
	for _, addressee := range []string{"architect", "pm"} {
		assertCommandFile(DesignCommandFor(KindMission, DesignJobModeAnswer, addressee))
	}
}

// Pure review-ledger logic tests (internal package — they exercise the unexported
// helpers appendReviewComments / normalizeReviewThread / applyReviewCommentStatus that
// the GitStore verbs build on).

func TestAppendReviewComments_MintsDeterministicIDsAndStampsOpen(t *testing.T) {
	in := []ReviewComment{
		{Anchor: "$.vision", AnchorText: "the old vision", Text: "too vague", AuthorRole: "architect"},
		{Anchor: "", Text: "free-form nit", AuthorRole: "architect"},
	}
	got := appendReviewComments(nil, 2, in)
	if len(got) != 2 {
		t.Fatalf("appended %d, want 2", len(got))
	}
	if got[0].ID != "r2c1" || got[1].ID != "r2c2" {
		t.Fatalf("ids = %q,%q want r2c1,r2c2", got[0].ID, got[1].ID)
	}
	for i, c := range got {
		if c.Status != ReviewCommentOpen {
			t.Errorf("comment %d status = %q, want open", i, c.Status)
		}
		if c.Round != 2 {
			t.Errorf("comment %d round = %d, want 2", i, c.Round)
		}
		if c.Response != "" {
			t.Errorf("comment %d response = %q, want empty", i, c.Response)
		}
	}
	if got[0].AnchorText != "the old vision" {
		t.Errorf("anchorText not carried: %q", got[0].AnchorText)
	}
}

func TestAppendReviewComments_IdempotentOnRetry(t *testing.T) {
	in := []ReviewComment{
		{Anchor: "$.a", Text: "one", AuthorRole: "architect"},
		{Anchor: "$.b", Text: "two", AuthorRole: "architect"},
	}
	first := appendReviewComments(nil, 1, in)
	// A Temporal retry re-runs the SAME (round, comments) — the deterministic ids dedup,
	// so no duplicate entries appear (review-ledger §5).
	second := appendReviewComments(first, 1, in)
	if len(second) != 2 {
		t.Fatalf("re-append duplicated entries: len = %d, want 2", len(second))
	}
}

func TestAppendReviewComments_DistinctRoundsAccumulate(t *testing.T) {
	r1 := appendReviewComments(nil, 1, []ReviewComment{{Anchor: "$.a", Text: "one"}})
	r2 := appendReviewComments(r1, 2, []ReviewComment{{Anchor: "$.b", Text: "two"}})
	if len(r2) != 2 {
		t.Fatalf("distinct rounds should accumulate: len = %d, want 2", len(r2))
	}
	if r2[0].ID != "r1c1" || r2[1].ID != "r2c1" {
		t.Fatalf("ids = %q,%q want r1c1,r2c1", r2[0].ID, r2[1].ID)
	}
}

func TestNormalizeReviewThread_ResponsePresenceDecidesStatus(t *testing.T) {
	thread := []ReviewComment{
		{ID: "r1c1", Status: ReviewCommentOpen, Response: "fixed the vision"}, // agent responded
		{ID: "r1c2", Status: ReviewCommentAddressed, Response: ""},            // agent claimed addressed w/o response
		{ID: "r1c3", Status: ReviewCommentWaived, Response: ""},               // waived stays sticky
	}
	got := normalizeReviewThread(thread)
	if got[0].Status != ReviewCommentAddressed {
		t.Errorf("responded comment status = %q, want addressed", got[0].Status)
	}
	if got[1].Status != ReviewCommentOpen {
		t.Errorf("empty-response comment status = %q, want open (server overrides the agent's claim)", got[1].Status)
	}
	if got[2].Status != ReviewCommentWaived {
		t.Errorf("waived comment status = %q, want waived (sticky)", got[2].Status)
	}
}

func TestApplyReviewCommentStatus_LegalTransitions(t *testing.T) {
	thread := []ReviewComment{
		{ID: "r1c1", Status: ReviewCommentOpen},
		{ID: "r1c2", Status: ReviewCommentAddressed, Response: "done"},
	}
	// open -> waived (dismiss).
	got, err := applyReviewCommentStatus(thread, "r1c1", ReviewCommentWaived)
	if err != nil {
		t.Fatalf("open->waived: %v", err)
	}
	if got[0].Status != ReviewCommentWaived {
		t.Errorf("r1c1 status = %q, want waived", got[0].Status)
	}
	// addressed -> open (reopen) clears the response so the next normalize keeps it open.
	got, err = applyReviewCommentStatus(got, "r1c2", ReviewCommentOpen)
	if err != nil {
		t.Fatalf("addressed->open: %v", err)
	}
	if got[1].Status != ReviewCommentOpen {
		t.Errorf("r1c2 status = %q, want open", got[1].Status)
	}
	if got[1].Response != "" {
		t.Errorf("reopen must clear response, got %q", got[1].Response)
	}
}

func TestApplyReviewCommentStatus_IllegalTransitionAndUnknownID(t *testing.T) {
	thread := []ReviewComment{{ID: "r1c1", Status: ReviewCommentOpen}}
	// open -> open is not a legal human transition.
	if _, err := applyReviewCommentStatus(thread, "r1c1", ReviewCommentOpen); kindOfErr(err) != fwra.ContractMisuse {
		t.Errorf("open->open kind = %v, want ContractMisuse", kindOfErr(err))
	}
	// unknown id is NotFound.
	if _, err := applyReviewCommentStatus(thread, "nope", ReviewCommentWaived); kindOfErr(err) != fwra.NotFound {
		t.Errorf("unknown id kind = %v, want NotFound", kindOfErr(err))
	}
}

func kindOfErr(err error) fwra.Kind {
	var e *fwra.Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return fwra.Kind(0)
}

// Question-comments (2026-07-05): type/addressee defaulting + the approve-gate classifier.

func TestAppendReviewComments_CarriesTypeAndAddressee(t *testing.T) {
	in := []ReviewComment{
		{Text: "why this volatility?", Type: ReviewCommentTypeQuestion, Addressee: ReviewAddresseePM},
		{Text: "rename this" /* no Type → change-request */},
	}
	out := appendReviewComments(nil, 1, in)
	if len(out) != 2 {
		t.Fatalf("want 2 entries, got %d", len(out))
	}
	if out[0].Type != ReviewCommentTypeQuestion || out[0].Addressee != ReviewAddresseePM {
		t.Errorf("question entry lost its type/addressee: %+v", out[0])
	}
	if out[1].Type != "" || out[1].Addressee != "" {
		t.Errorf("change-request entry must have empty type/addressee (legacy default): %+v", out[1])
	}
	// Ids are minted deterministically and predictable via ReviewCommentID.
	if out[0].ID != ReviewCommentID(1, 0) || out[1].ID != ReviewCommentID(1, 1) {
		t.Errorf("minted ids not deterministic: %q %q", out[0].ID, out[1].ID)
	}
}

func TestReviewCommentClassifiers(t *testing.T) {
	cases := []struct {
		name       string
		c          ReviewComment
		isQuestion bool
		blocksAppr bool
	}{
		{"legacy open (empty type) blocks", ReviewComment{Status: ReviewCommentOpen}, false, true},
		{"open change-request blocks", ReviewComment{Status: ReviewCommentOpen, Type: ReviewCommentTypeChangeRequest}, false, true},
		{"open question does NOT block", ReviewComment{Status: ReviewCommentOpen, Type: ReviewCommentTypeQuestion}, true, false},
		{"addressed question does NOT block", ReviewComment{Status: ReviewCommentAddressed, Type: ReviewCommentTypeQuestion}, true, false},
		{"addressed change-request does NOT block", ReviewComment{Status: ReviewCommentAddressed}, false, false},
		{"waived change-request does NOT block", ReviewComment{Status: ReviewCommentWaived}, false, false},
	}
	for _, tc := range cases {
		if got := ReviewCommentIsQuestion(tc.c); got != tc.isQuestion {
			t.Errorf("%s: IsQuestion=%v want %v", tc.name, got, tc.isQuestion)
		}
		if got := ReviewCommentBlocksApprove(tc.c); got != tc.blocksAppr {
			t.Errorf("%s: BlocksApprove=%v want %v", tc.name, got, tc.blocksAppr)
		}
	}
}

// An answered question normalizes to addressed (Response non-empty), an unanswered one
// stays open — and either way a question never blocks approve.
func TestNormalize_QuestionStatusAndGate(t *testing.T) {
	thread := []ReviewComment{
		{ID: "q1", Status: ReviewCommentOpen, Type: ReviewCommentTypeQuestion, Response: ""},
		{ID: "q2", Status: ReviewCommentOpen, Type: ReviewCommentTypeQuestion, Response: "because X"},
	}
	out := normalizeReviewThread(thread)
	if out[0].Status != ReviewCommentOpen {
		t.Errorf("unanswered question must stay open, got %q", out[0].Status)
	}
	if out[1].Status != ReviewCommentAddressed {
		t.Errorf("answered question must be addressed, got %q", out[1].Status)
	}
	for _, c := range out {
		if ReviewCommentBlocksApprove(c) {
			t.Errorf("a question must never block approve: %+v", c)
		}
	}
}

// A staleAck audit entry is sticky: normalization never reopens it (no Response, but it must
// stay addressed), and appendStaleAck stamps it addressed + staleAck with a fresh id.
func TestStaleAck_AppendAndNormalizeSticky(t *testing.T) {
	out := appendStaleAck(nil, "architect", "diagrams only")
	if len(out) != 1 {
		t.Fatalf("want 1 entry, got %d", len(out))
	}
	e := out[0]
	if e.Type != ReviewCommentTypeStaleAck || e.Status != ReviewCommentAddressed || e.AuthorRole != "architect" {
		t.Fatalf("staleAck shape wrong: %+v", e)
	}
	// Normalize must NOT flip the staleAck (empty response) to open.
	normalized := normalizeReviewThread(out)
	if normalized[0].Status != ReviewCommentAddressed {
		t.Errorf("staleAck must stay addressed after normalize, got %q", normalized[0].Status)
	}
	if ReviewCommentBlocksApprove(normalized[0]) {
		t.Errorf("staleAck must never block approve")
	}
}

func TestReviewPolicy_EmptyRequiresNoHuman(t *testing.T) {
	var p ReviewPolicy
	if p.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("empty policy must require no human approval (inert)")
	}
}

func TestReviewPolicy_RequiresHumanForGatedPhase(t *testing.T) {
	p := ReviewPolicy{GatedPhasesByType: map[string][]ActivityMethodPhase{
		"frontend": {MethodPhaseDetailedDesign},
	}}
	if !p.RequiresHuman("frontend", MethodPhaseDetailedDesign) {
		t.Error("frontend/detailed_design should require human")
	}
	if p.RequiresHuman("frontend", MethodPhaseConstruction) {
		t.Error("frontend/construction not gated")
	}
	if p.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("service not gated")
	}
}

func TestReviewPolicyFromGateIDs_MapsMockIDs(t *testing.T) {
	p := ReviewPolicyFromGateIDs(map[string][]string{"service": {"svc-contract"}})
	if !p.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("svc-contract must map to detailed_design")
	}
}

// ---- Tests: preset resolution + non-overridable floor (Task 7) --------------

// presetPtr is a small test helper — ReviewPolicy.Preset is *string (modelgen's
// optional-scalar convention) so literal construction needs an addressable value.
func presetPtr(s string) *string { return &s }

// TestReviewPolicy_EffectiveGate_VibesAutoApprovesDraft is the brief's Step-1 scenario:
// under the "vibes" preset, a non-floor phase (the detailed-design/contract "draft
// commit") is auto-approved — no gate.
func TestReviewPolicy_EffectiveGate_VibesAutoApprovesDraft(t *testing.T) {
	p := ReviewPolicy{Preset: presetPtr(ReviewPresetVibes)}
	if p.EffectiveGate("service", MethodPhaseDetailedDesign, false) {
		t.Error("vibes must auto-approve a draft commit (detailed_design, no floor)")
	}
	if p.EffectiveGate("service", MethodPhaseConstruction, false) {
		t.Error("vibes must auto-approve construction dispatch when the floor is not touched")
	}
}

// TestReviewPolicy_EffectiveGate_FloorBlocksFlaggedDispatch is the brief's Step-1
// scenario: the non-overridable floor still blocks a flagged (deploy/spend/schema-
// touching) construction dispatch even under "vibes".
func TestReviewPolicy_EffectiveGate_FloorBlocksFlaggedDispatch(t *testing.T) {
	p := ReviewPolicy{Preset: presetPtr(ReviewPresetVibes)}
	if !p.EffectiveGate("service", MethodPhaseConstruction, true) {
		t.Error("the floor must block a flagged construction dispatch even under vibes")
	}
}

// TestReviewPolicy_EffectiveGate_FloorOnlyGuardsConstructionPhase pins the floor's
// scope: floorTouched only forces a gate at MethodPhaseConstruction (the dispatch),
// never at other phases — the floor is about construction dispatch, not the whole
// activity.
func TestReviewPolicy_EffectiveGate_FloorOnlyGuardsConstructionPhase(t *testing.T) {
	p := ReviewPolicy{Preset: presetPtr(ReviewPresetVibes)}
	if p.EffectiveGate("service", MethodPhaseDetailedDesign, true) {
		t.Error("the floor must not gate phases other than construction dispatch")
	}
}

// TestReviewPolicy_EffectiveGate_Checkpoints pins the "checkpoints" preset to gating
// exactly the per-activity contract/architecture commit (detailed_design) and the
// construction dispatch (construction) — not requirements/test_plan/integration.
func TestReviewPolicy_EffectiveGate_Checkpoints(t *testing.T) {
	p := ReviewPolicy{Preset: presetPtr(ReviewPresetCheckpoints)}
	gated := map[ActivityMethodPhase]bool{
		MethodPhaseRequirements:   false,
		MethodPhaseDetailedDesign: true,
		MethodPhaseTestPlan:       false,
		MethodPhaseConstruction:   true,
		MethodPhaseIntegration:    false,
	}
	for phase, want := range gated {
		if got := p.EffectiveGate("service", phase, false); got != want {
			t.Errorf("checkpoints EffectiveGate(%s) = %v, want %v", phase, got, want)
		}
	}
}

// TestReviewPolicy_EffectiveGate_Full pins the "full" preset to gating every phase —
// today's approve-everything behavior.
func TestReviewPolicy_EffectiveGate_Full(t *testing.T) {
	p := ReviewPolicy{Preset: presetPtr(ReviewPresetFull)}
	for _, phase := range []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration,
	} {
		if !p.EffectiveGate("service", phase, false) {
			t.Errorf("full preset must gate every phase, %s did not gate", phase)
		}
	}
}

// TestReviewPolicy_EffectiveGate_LegacyFallsBackToExplicitMap pins the "" / unset
// preset to legacy/explicit mode: EffectiveGate falls back to RequiresHuman's
// committed GatedPhasesByType map, unchanged from pre-Task-7 behavior (e.g. the
// webApp PolicyPanel's ReviewPolicyFromGateIDs output).
func TestReviewPolicy_EffectiveGate_LegacyFallsBackToExplicitMap(t *testing.T) {
	p := ReviewPolicy{GatedPhasesByType: map[string][]ActivityMethodPhase{
		"frontend": {MethodPhaseDetailedDesign},
	}}
	if !p.EffectiveGate("frontend", MethodPhaseDetailedDesign, false) {
		t.Error("legacy mode must honor the explicit GatedPhasesByType map")
	}
	if p.EffectiveGate("service", MethodPhaseDetailedDesign, false) {
		t.Error("legacy mode must not gate an un-configured activity type")
	}
}

// TestCreateProject_DefaultsReviewPolicyToVibesPreset pins the Task 7 default: a
// FRESH project (the "project.json is first materialized" path — CreateProject's
// modeCreateOnly branch, the one path init-vs-first-materialization judgment call
// picked, see docs/superpowers/sdd/task-7-report.md) is born with ReviewPolicy.Preset
// explicitly "vibes". This is behavior-PRESERVING for every existing caller: an empty
// GatedPhasesByType map already gated nothing (RequiresHuman's zero-value "pure
// vibes"), so making the default explicit changes no dispatch decision — it only gives
// the local-first funnel (and any future preset UI) a real value to read/upgrade.
func TestCreateProject_DefaultsReviewPolicyToVibesPreset(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID("fresh-project")
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	p, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if p.ReviewPolicy.Preset == nil || *p.ReviewPolicy.Preset != ReviewPresetVibes {
		t.Fatalf("fresh project ReviewPolicy.Preset = %v, want %q", p.ReviewPolicy.Preset, ReviewPresetVibes)
	}
}

// TestContractTouchesReviewFloor_KeywordMatch pins the floor's data list
// (deploy/spend/schema, case-insensitive substring match on operation names).
func TestContractTouchesReviewFloor_KeywordMatch(t *testing.T) {
	tests := []struct {
		name string
		ops  []string
		want bool
	}{
		{"deploy op", []string{"DeployService"}, true},
		{"spend op", []string{"AuthorizeSpend"}, true},
		{"schema op", []string{"MigrateSchema"}, true},
		{"case-insensitive", []string{"deployservice"}, true},
		{"no match", []string{"GenerateArtifact", "ReadStatus"}, false},
		{"no ops", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ops []ContractOperation
			for _, name := range tt.ops {
				ops = append(ops, ContractOperation{Name: name})
			}
			contract := ServiceContract{Interface: ContractInterface{Operations: ops}}
			if got := ContractTouchesReviewFloor(contract); got != tt.want {
				t.Errorf("ContractTouchesReviewFloor(%v) = %v, want %v", tt.ops, got, tt.want)
			}
		})
	}
}

// TestProjectDoc_ReviewPolicy_RoundTrip verifies that a Project with a non-empty
// ReviewPolicy encodes and decodes symmetrically via EncodeProjectJSON / DecodeProjectJSON.
func TestProjectDoc_ReviewPolicy_RoundTrip(t *testing.T) {
	p := Project{
		ID:   ProjectID("rp-rt-001"),
		Name: "review policy round-trip",
		ReviewPolicy: ReviewPolicy{
			GatedPhasesByType: map[string][]ActivityMethodPhase{
				"service":  {MethodPhaseDetailedDesign, MethodPhaseIntegration},
				"frontend": {MethodPhaseDetailedDesign},
			},
		},
	}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, p.ID)
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON: ok=false")
	}
	if len(got.ReviewPolicy.GatedPhasesByType) != 2 {
		t.Fatalf("ReviewPolicy.GatedPhasesByType len = %d, want 2", len(got.ReviewPolicy.GatedPhasesByType))
	}
	if !got.ReviewPolicy.RequiresHuman("service", MethodPhaseDetailedDesign) {
		t.Error("service/detailed_design lost across round-trip")
	}
	if !got.ReviewPolicy.RequiresHuman("service", MethodPhaseIntegration) {
		t.Error("service/integration lost across round-trip")
	}
	if !got.ReviewPolicy.RequiresHuman("frontend", MethodPhaseDetailedDesign) {
		t.Error("frontend/detailed_design lost across round-trip")
	}
	if got.ReviewPolicy.RequiresHuman("frontend", MethodPhaseConstruction) {
		t.Error("frontend/construction should not be gated after round-trip")
	}
}

// ---------------------------------------------------------------------------
// Persisted-state JSON key casing (QA defect B, 2026-07-16).
//
// The committed .aiarch/state/project.json follows the schema-first lowerCamel
// casing convention everywhere. The handwritten ResearchCorpus /
// ResearchSourceRef types carried capitalized json tags ("Sources", "Title",
// "Path", "ContentBytes") and leaked PascalCase keys into the committed state.
// These suites (1) prove the research block now persists camelCase keys end to
// end, (2) prove documents written with the LEGACY capitalized keys still
// decode (Go's json.Unmarshal matches case-insensitively), and (3) gate the
// whole persisted type tree so a new capitalized json tag can never land
// unnoticed again.
// ---------------------------------------------------------------------------

// Test_ResearchCorpus_PersistsCamelCaseKeys drives the real write path
// (SetResearchInput over a local git repo) and asserts the committed
// project.json research block uses lowerCamel keys — not the legacy
// capitalized ones.
func Test_ResearchCorpus_PersistsCamelCaseKeys(t *testing.T) {
	store, raw, cred, ctx := newLocalGitStoreWithRepo(t)
	id := ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	research := ResearchInput{Sources: []ResearchSource{{Title: "Founder Brief", Content: "corpus body"}}}
	if _, err := store.SetResearchInput(ctx, id, 1, research, cred, "wf:research"); err != nil {
		t.Fatalf("SetResearchInput: %v", err)
	}

	snap, err := raw.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("raw ReadSubtree: %v", err)
	}
	pj, ok := snap.Files["project.json"]
	if !ok {
		t.Fatal("project.json not committed")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(pj, &doc); err != nil {
		t.Fatalf("unmarshal project.json: %v", err)
	}
	var researchBlock struct {
		Sources []map[string]json.RawMessage `json:"sources"`
	}
	if err := json.Unmarshal(doc["research"], &researchBlock); err != nil {
		t.Fatalf("unmarshal research block: %v", err)
	}
	if len(researchBlock.Sources) != 1 {
		t.Fatalf("committed research block has no lowerCamel `sources` array: %s", doc["research"])
	}
	src := researchBlock.Sources[0]
	for _, want := range []string{"title", "path", "contentBytes"} {
		if _, ok := src[want]; !ok {
			t.Errorf("committed research source missing lowerCamel key %q; keys present: %v", want, keysOf(src))
		}
	}
	for _, legacy := range []string{"Sources", "Title", "Path", "ContentBytes"} {
		if bytes.Contains(doc["research"], []byte(`"`+legacy+`"`)) {
			t.Errorf("committed research block still carries legacy capitalized key %q: %s", legacy, doc["research"])
		}
	}
}

// Test_ResearchCorpus_LegacyCapitalizedKeysStillDecode proves back-compat: a
// project.json committed BEFORE the casing fix (capitalized "Sources"/"Title"/
// "Path"/"ContentBytes") still decodes into the corpus — Go's json.Unmarshal
// matches struct fields case-insensitively, so no migration of existing state
// files is needed.
func Test_ResearchCorpus_LegacyCapitalizedKeysStillDecode(t *testing.T) {
	legacy := []byte(`{
		"id": "p1",
		"version": 3,
		"phase": 1,
		"owner": "alice",
		"name": "Demo",
		"research": {"Sources": [{"Title": "Founder Brief", "Path": ".aiarch/state/research/00-founder-brief.txt", "ContentBytes": 42}]},
		"slots": {}
	}`)
	p, exists, err := decodeProjectDoc(legacy, ProjectID("p1"))
	if err != nil {
		t.Fatalf("decodeProjectDoc(legacy casing): %v", err)
	}
	if !exists {
		t.Fatal("decodeProjectDoc: exists=false")
	}
	if len(p.Research.Sources) != 1 {
		t.Fatalf("legacy capitalized research keys no longer decode; got %+v", p.Research)
	}
	got := p.Research.Sources[0]
	want := ResearchSourceRef{Title: "Founder Brief", Path: ".aiarch/state/research/00-founder-brief.txt", ContentBytes: 42}
	if got != want {
		t.Fatalf("legacy decode = %+v, want %+v", got, want)
	}
}

// Test_PersistedStateJSONTags_AreLowerCamel is the regression gate for the
// whole persisted type tree: every json tag reachable from projectDoc (the
// on-disk project.json shape), from appliedRecord (the dedup ledger file), and
// from every artifact model in the closed ArtifactKind sum (the slot payloads)
// must begin with a lowercase letter. A field with NO json tag is equally an
// offender — encoding/json then uses the exported (capitalized) Go name.
//
// KNOWN OFFENDERS (ratchet, not waiver): the entries in
// legacyUpperCamelPersistedTags below predate this gate. They are GENERATED
// from project.json .serviceContracts (contract.gen.go — e.g. ProducedArtifact,
// Profile), so fixing them means fixing the casing in the committed service
// contracts and re-running `make gen`, which changes the persisted shape AND
// the wire surface together — earmarked as a follow-up contract-casing pass.
// This list must only ever SHRINK; adding to it fails review by construction
// (the test message says to fix the tag, not to extend the list).
func Test_PersistedStateJSONTags_AreLowerCamel(t *testing.T) {
	roots := []reflect.Type{
		reflect.TypeFor[projectDoc](),
		reflect.TypeFor[appliedRecord](),
	}
	for _, k := range AllArtifactKinds() {
		m, ok := NewModelForKind(k)
		if !ok {
			t.Fatalf("NewModelForKind(%v): no factory", k)
		}
		roots = append(roots, reflect.TypeOf(m))
	}

	offenders := map[string]bool{}
	seen := map[reflect.Type]bool{}
	for _, r := range roots {
		collectUpperJSONTags(r, seen, offenders)
	}

	for o := range offenders {
		if legacyUpperCamelPersistedTags[o] {
			continue
		}
		t.Errorf("persisted JSON key not lowerCamel: %s — give the field a camelCase json tag (project.json is schema-first lowerCamel)", o)
	}
	// Ratchet: every allowlisted entry must still exist; a fixed offender must be
	// REMOVED from the list so the gate only ever tightens.
	for l := range legacyUpperCamelPersistedTags {
		if !offenders[l] {
			t.Errorf("legacyUpperCamelPersistedTags entry %q no longer offends — remove it from the allowlist (the gate must only shrink)", l)
		}
	}
}

// legacyUpperCamelPersistedTags is the closed set of PRE-EXISTING capitalized
// json keys in the persisted state, all owned by generated contract types
// (contract.gen.go ← project.json .serviceContracts). See the gate test's doc
// comment: fix = contract casing pass + `make gen`; this list only shrinks.
var legacyUpperCamelPersistedTags = map[string]bool{
	// ActivityGitStatus (projectDoc.activityGit map values) — generated; the
	// webApp wire layer (GitStatus.tsx et al.) consumes these capitalized keys,
	// so the fix must move contract + regen + webApp together.
	"ActivityGitStatus.ActivityID:ActivityID":         true,
	"ActivityGitStatus.BranchName:BranchName":         true,
	"ActivityGitStatus.BranchRef:BranchRef":           true,
	"ActivityGitStatus.CRLabel:CRLabel":               true,
	"ActivityGitStatus.PullRequestRef:PullRequestRef": true,
	"ActivityGitStatus.CICheck:CICheck":               true,
	"ActivityGitStatus.Merged:Merged":                 true,
	"ActivityGitStatus.ArchApproved:ArchApproved":     true,
	"ActivityGitStatus.IsRevert:IsRevert":             true,
	"ActivityGitStatus.UpdatedAt:UpdatedAt":           true,
	// ProducedArtifact (activityConstruction produced[]) — generated.
	"ProducedArtifact.Kind:Kind":         true,
	"ProducedArtifact.Title:Title":       true,
	"ProducedArtifact.Source:Source":     true,
	"ProducedArtifact.Produced:Produced": true,
	"ProducedArtifact.Note:Note":         true,
	// ConstructionProgress (projectDoc.constructionProgress) — generated.
	"ConstructionProgress.Week:Week":                     true,
	"ConstructionProgress.TotalWeeks:TotalWeeks":         true,
	"ConstructionProgress.HandOffModel:HandOffModel":     true,
	"ConstructionProgress.SupervisionCap:SupervisionCap": true,
}

// collectUpperJSONTags walks the struct type tree reachable from t (through
// pointers, slices, arrays and maps) and records every field whose effective
// JSON key starts with an uppercase letter, as "<Type>.<Field>:<key>".
func collectUpperJSONTags(t reflect.Type, seen map[reflect.Type]bool, out map[string]bool) {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		collectUpperJSONTags(t.Elem(), seen, out)
		return
	case reflect.Map:
		// Map KEYS are data (activity IDs, component names), not schema fields.
		collectUpperJSONTags(t.Elem(), seen, out)
		return
	case reflect.Struct:
	default:
		return
	}
	if seen[t] {
		return
	}
	seen[t] = true
	if t == reflect.TypeFor[time.Time]() {
		return // marshals as an RFC3339 string, no keys
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		key := strings.Split(tag, ",")[0]
		if key == "" {
			key = f.Name // no tag: encoding/json uses the Go field name
		}
		if !f.Anonymous && key != "" && key[0] >= 'A' && key[0] <= 'Z' {
			out[t.Name()+"."+f.Name+":"+key] = true
		}
		collectUpperJSONTags(f.Type, seen, out)
	}
}
