package projectstate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
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

	v3 := stageAndCommitMission(ctx, t, state, id, v1)
	assertMissionReadsBackFromGit(ctx, t, state, id, v3)

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

// stageAndCommitMission runs UC1's stage → commit pair for the mission slot and
// returns the post-commit version.
//
// The main-branch StageArtifactForReview contract op was retired (Wave-1 fossil
// prune); staging now rides the surviving designSession branch verb with an empty
// branch (== main).
func stageAndCommitMission(ctx context.Context, t *testing.T, state ProjectStateAccess, id ProjectID, v1 Version) Version {
	t.Helper()
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
	return v3
}

// assertMissionReadsBackFromGit re-reads through a FRESH clone — which is what
// proves the JSON is committed to the git repo rather than held in memory.
func assertMissionReadsBackFromGit(ctx context.Context, t *testing.T, state ProjectStateAccess, id ProjectID, want Version) {
	t.Helper()
	proj, err := state.ReadProject(fwra.Context{Context: ctx}, id)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != want {
		t.Fatalf("ReadProject version = %d, want %d", proj.Version, want)
	}
	if proj.Mission.Status != ReviewCommitted {
		t.Fatalf("mission status = %v, want Committed", proj.Mission.Status)
	}
	got, ok := proj.Mission.Model.(*MissionStatement)
	if !ok || got.Vision != "vision-text" || got.Mission != "mission-text" {
		t.Fatalf("mission model round-trip through git failed: %+v", proj.Mission.Model)
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

// --------------------------------------------------------------------------
// Project-identity guard — the local git-backed store is single-project (a
// repo is whatever project.json happens to be committed under statePathPrefix)
// and, pre-fix, nothing checked that the caller's projectID matched the `id`
// already committed there: decodeProjectDoc unconditionally STAMPED the
// requested projectID onto the decoded aggregate, discarding the real on-disk
// id. Two archistrator instances sharing a Temporal namespace hit this for
// real: one instance's worker executed the OTHER project's workflow against
// its OWN repo, silently rewriting that repo's project.json `id` and grafting
// a foreign activityConstruction entry into an otherwise-intact document.
// guardProjectIdentity (run from applyMutationOnBranchFiles' STEP 0,
// projectstateaccess.go) refuses any mutation whose target projectID doesn't
// match a non-empty on-disk id, BEFORE anything is decoded or written.
// --------------------------------------------------------------------------

// readRawProjectDoc reads project.json straight off the repo through a fresh
// ReadSubtree — the same "prove it's really on disk, not merely held in memory"
// read path assertIdentityPersistedVerbatimOnDisk above uses. Returns a copy:
// snap.Files' backing bytes must not be mutated/reused across calls.
func readRawProjectDoc(ctx context.Context, t *testing.T, repo *fwgithub.GitStore) []byte {
	t.Helper()
	snap, err := repo.ReadSubtree(ctx, ".aiarch/state", fwgithub.GitAuth{Local: true})
	if err != nil {
		t.Fatalf("ReadSubtree: %v", err)
	}
	raw, ok := snap.Files["project.json"]
	if !ok {
		t.Fatal("project.json not committed to the repo")
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}

// TestProjectIdentityGuard_RefusesCrossProjectWrite reproduces the observed
// corruption at the store layer: a repo already carries a committed document for
// ownerID; a write then arrives addressed to a DIFFERENT project (intruderID) —
// exactly what happens when two archistrator instances share a Temporal namespace
// and one instance's worker executes the other's workflow against its own repo.
// The write must be refused outright, as fwra.ContractMisuse (permanent — no retry
// fixes a repo-identity mismatch), naming both projects, and the on-disk document
// must be byte-for-byte UNCHANGED afterward.
func TestProjectIdentityGuard_RefusesCrossProjectWrite(t *testing.T) {
	store, proj, cred, ctx := newLocalGitStoreWithRepo(t)

	ownerID := ProjectID("todomvc-run-20260808T000116Z-1744cf81")
	intruderID := ProjectID("archistrator")

	v1, err := store.CreateProject(fwra.Context{Context: ctx}, ownerID, "alice", "TodoMVC", cred, fwra.IdempotencyKey("wf:create-owner"))
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	before := readRawProjectDoc(ctx, t, proj)

	_, err = store.RecordOperatorPaused(fwra.Context{Context: ctx}, intruderID, v1, "cross-project contamination", cred, fwra.IdempotencyKey("wf:pause-intruder"))
	if err == nil {
		t.Fatal("RecordOperatorPaused for a MISMATCHED project must be refused, got nil error")
	}
	if got := kindOf(t, err); got != fwra.ContractMisuse {
		t.Fatalf("error kind = %v, want fwra.ContractMisuse (permanent — a retry can never fix an identity mismatch)", got)
	}
	var fe *fwra.Error
	if !errors.As(err, &fe) {
		t.Fatalf("expected *fwra.Error, got %T", err)
	}
	if fe.Retryable {
		t.Fatalf("identity-mismatch ContractMisuse error must NOT be retryable, got Retryable=true: %v", fe)
	}
	if !strings.Contains(err.Error(), string(ownerID)) || !strings.Contains(err.Error(), string(intruderID)) {
		t.Fatalf("error must name BOTH the on-disk owner project and the refused requester, got: %v", err)
	}

	after := readRawProjectDoc(ctx, t, proj)
	if !bytes.Equal(before, after) {
		t.Fatalf("refused write MUST NOT touch the document:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestProjectIdentityGuard_FreshRepoFirstWriteSucceeds proves the guard's
// empty/absent-id carve-out: a repo with NO committed project.json yet (exactly
// what `archistrator init` scaffolds — cmd/archistrator/init.go deliberately
// writes no project.json) must still accept its first write (CreateProject),
// and a subsequent SAME-project write must keep working normally afterward.
func TestProjectIdentityGuard_FreshRepoFirstWriteSucceeds(t *testing.T) {
	store, _, cred, ctx := newLocalGitStoreWithRepo(t)
	id := ProjectID("todomvc-run-20260808T000116Z-1744cf81")

	v1, err := store.CreateProject(fwra.Context{Context: ctx}, id, "alice", "TodoMVC", cred, fwra.IdempotencyKey("wf:create-fresh"))
	if err != nil {
		t.Fatalf("CreateProject on a fresh repo (no project.json yet) must succeed, got: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("CreateProject version = %d, want 1", v1)
	}

	v2, err := store.RecordOperatorPaused(fwra.Context{Context: ctx}, id, v1, "matching-project", cred, fwra.IdempotencyKey("wf:pause-same"))
	if err != nil {
		t.Fatalf("RecordOperatorPaused for the SAME project must keep working, got: %v", err)
	}
	if v2 != v1+1 {
		t.Fatalf("version = %d, want %d", v2, v1+1)
	}
}

// TestProjectIdentityGuard_CreateProjectRefusesForeignIDAgainstOccupiedRepo is the
// committed regression for the gap an independent reviewer found and reproduced:
// CreateProject's RESUME probe used to delegate to ReadProject, which (via
// decodeProjectFromSnapshot) STAMPS the requested projectID onto whatever it
// decodes and never compares it to the on-disk `id`. Called for a FOREIGN id
// against a repo that already holds a DIFFERENT project's committed document —
// exactly the LOCAL-profile single-repo shape of the real incident — that used to
// return a FABRICATED SUCCESS (a version number, nil error) for a project that was
// never created in that repo, without ever reaching applyMutation /
// loadAggregateForMutation / the write-path guard. This must now fail loudly, the
// same way every sibling verb does, and the on-disk document must be untouched.
func TestProjectIdentityGuard_CreateProjectRefusesForeignIDAgainstOccupiedRepo(t *testing.T) {
	store, proj, cred, ctx := newLocalGitStoreWithRepo(t)

	ownerID := ProjectID("todomvc-run-20260808T000116Z-1744cf81")
	intruderID := ProjectID("archistrator")

	v1, err := store.CreateProject(fwra.Context{Context: ctx}, ownerID, "alice", "TodoMVC", cred, fwra.IdempotencyKey("wf:create-owner"))
	if err != nil {
		t.Fatalf("CreateProject(owner): %v", err)
	}
	if v1 != 1 {
		t.Fatalf("CreateProject(owner) version = %d, want 1", v1)
	}

	before := readRawProjectDoc(ctx, t, proj)

	gotVersion, err := store.CreateProject(fwra.Context{Context: ctx}, intruderID, "bob", "Archistrator", cred, fwra.IdempotencyKey("wf:create-intruder"))
	if err == nil {
		t.Fatalf("CreateProject for a FOREIGN project against an occupied repo must be refused, got a fabricated success: version=%d", gotVersion)
	}
	if got := kindOf(t, err); got != fwra.ContractMisuse {
		t.Fatalf("error kind = %v, want fwra.ContractMisuse (permanent — a retry can never fix an identity mismatch)", got)
	}
	var fe *fwra.Error
	if !errors.As(err, &fe) {
		t.Fatalf("expected *fwra.Error, got %T", err)
	}
	if fe.Retryable {
		t.Fatalf("identity-mismatch ContractMisuse error must NOT be retryable, got Retryable=true: %v", fe)
	}
	if !strings.Contains(err.Error(), string(ownerID)) || !strings.Contains(err.Error(), string(intruderID)) {
		t.Fatalf("error must name BOTH the on-disk owner project and the refused requester, got: %v", err)
	}

	after := readRawProjectDoc(ctx, t, proj)
	if !bytes.Equal(before, after) {
		t.Fatalf("refused CreateProject MUST NOT touch the document:\nbefore: %s\nafter:  %s", before, after)
	}

	// The owner project itself must be entirely unaffected — a fresh read still
	// resolves to the SAME id/version, not the intruder's.
	owner, err := store.ReadProject(fwra.Context{Context: ctx}, ownerID, cred)
	if err != nil {
		t.Fatalf("ReadProject(owner) after refused intruder CreateProject: %v", err)
	}
	if owner.ID != ownerID || owner.Version != v1 {
		t.Fatalf("owner project corrupted by the refused intruder CreateProject: got id=%s version=%d, want id=%s version=%d",
			owner.ID, owner.Version, ownerID, v1)
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

func readConstructionStatus(t *testing.T, store *GitStore, id ProjectID, cred RepoCredential, activityID string) ActivityExecution {
	t.Helper()
	p := readProject(t, store, id, cred)
	s, ok := p.ActivityExecution[activityID]
	if !ok {
		t.Fatalf("ActivityConstruction[%s] absent", activityID)
	}
	return s
}

// seedActivity creates a project and calls RecordActivityStarted so
// modeRequireExisting verbs have a row to upsert.
func seedActivity(t *testing.T, store *GitStore, id ProjectID, v Version, cred RepoCredential, activityID string) Version {
	t.Helper()
	v2, err := store.RecordActivityStarted(fwra.Context{Context: context.Background()}, id, v, activityID, ActivityTypeService, TestVariantPlan, cred, fwra.IdempotencyKey("wf:seed-"+activityID))
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

	// The review is a ReviewRound on the execution ledger now (stage-3 task 3), and the
	// coarse build status this verb used to stamp is derived from it. What survives the
	// deprecation is the row it births and the version it advances.
	s := readConstructionStatus(t, store, id, cred, "C001")
	if s.ActivityID != "C001" {
		t.Fatalf("the row must be born under its own id, got %+v", s)
	}
	if len(s.Reviews) != 0 || len(s.Attempts) != 0 {
		t.Fatalf("the deprecated verb must record no ledger entry of its own, got %+v", s)
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

	// The exit stamp is the whole record: Done follows from it, and INTEGRATED follows
	// from the ledger instead — an activity that exited with no gate passed is done but
	// not integrated, which is the Skipped/TakenOver shape and now also the honest read
	// of a "completed" exit nothing was recorded for.
	s := readConstructionStatus(t, store, id, cred, "C002")
	if coarsePhaseOf(s) != ActivityConstructionDone {
		t.Fatalf("Phase = %v, want Done", coarsePhaseOf(s))
	}
	if buildStatusOf(s) != BuildInReview {
		t.Fatalf("BuildStatus = %v, want BuildInReview over an empty ledger", buildStatusOf(s))
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
	if coarsePhaseOf(s) != ActivityConstructionDone {
		t.Fatalf("Phase = %v, want Done", coarsePhaseOf(s))
	}
	if buildStatusOf(s) != BuildInReview {
		t.Fatalf("BuildStatus = %v, want BuildInReview (skipped)", buildStatusOf(s))
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
// B1.7: RecordOperatorResumed clears the recorded pause
// --------------------------------------------------------------------------

// TestRecordOperatorResumed_ClearsThePause: both fields are cleared, and the resumed
// project encodes exactly as one that was never paused (both fields omitempty).
func TestRecordOperatorResumed_ClearsThePause(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()
	never, err := EncodeProjectJSON(readProject(t, store, id, cred))
	if err != nil {
		t.Fatalf("encode the never-paused project: %v", err)
	}
	v2, err := store.RecordOperatorPaused(fwra.Context{Context: ctx}, id, v, "operator halt", cred, fwra.IdempotencyKey("wf:paused"))
	if err != nil {
		t.Fatalf("RecordOperatorPaused: %v", err)
	}
	v3, err := store.RecordOperatorResumed(fwra.Context{Context: ctx}, id, v2, cred, fwra.IdempotencyKey("wf:resumed"))
	if err != nil {
		t.Fatalf("RecordOperatorResumed: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}
	p := readProject(t, store, id, cred)
	if p.OperatorPaused || p.PauseReason != "" {
		t.Fatalf("after a resume OperatorPaused=%v PauseReason=%q, want false and empty", p.OperatorPaused, p.PauseReason)
	}
	resumed, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode the resumed project: %v", err)
	}
	// Version and the audit trail move with every write; everything else must match.
	if a, b := stripVolatile(t, never), stripVolatile(t, resumed); a != b {
		t.Fatalf("a resumed project must encode as a never-paused one:\nnever  %s\nresumed %s", a, b)
	}
}

// stripVolatile drops the members every write moves (version, updatedAt), so two
// encodings compare on content.
func stripVolatile(t *testing.T, raw []byte) string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	delete(m, "version")
	delete(m, "updatedAt")
	if _, ok := m["operatorPaused"]; ok {
		t.Fatalf("a cleared pause must omit operatorPaused, got %s", m["operatorPaused"])
	}
	if _, ok := m["pauseReason"]; ok {
		t.Fatalf("a cleared pause must omit pauseReason, got %s", m["pauseReason"])
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	return string(out)
}

// TestRecordOperatorResumed_IdempotentUnderTheSameKey: a retried resume (same key) is
// deduplicated, like every head-state record.
func TestRecordOperatorResumed_IdempotentUnderTheSameKey(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()
	v2, err := store.RecordOperatorPaused(fwra.Context{Context: ctx}, id, v, "halt", cred, fwra.IdempotencyKey("wf:p"))
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	v3, err := store.RecordOperatorResumed(fwra.Context{Context: ctx}, id, v2, cred, fwra.IdempotencyKey("wf:r"))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	again, err := store.RecordOperatorResumed(fwra.Context{Context: ctx}, id, v2, cred, fwra.IdempotencyKey("wf:r"))
	if err != nil {
		t.Fatalf("a replayed resume under the same key must dedupe, got %v", err)
	}
	if again != v3 {
		t.Fatalf("the replayed resume returned version %d, want the original %d", again, v3)
	}
}

// TestRecordOperatorResumed_NotPausedChangesNoField: the RA is an idempotent writer of
// the cleared state; the façade owns the "not paused" precondition.
func TestRecordOperatorResumed_NotPausedChangesNoField(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	if _, err := store.RecordOperatorResumed(fwra.Context{Context: context.Background()}, id, v, cred, fwra.IdempotencyKey("wf:r0")); err != nil {
		t.Fatalf("resume of an unpaused project: %v", err)
	}
	if p := readProject(t, store, id, cred); p.OperatorPaused || p.PauseReason != "" {
		t.Fatalf("an unpaused project must stay unpaused, got %v %q", p.OperatorPaused, p.PauseReason)
	}
}

// TestRecordOperatorResumed_NoProjectIsNotFound: the resume requires an existing project.
func TestRecordOperatorResumed_NoProjectIsNotFound(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	_, err := store.RecordOperatorResumed(fwra.Context{Context: ctx}, ProjectID(uuid.NewString()), 0, cred, fwra.IdempotencyKey("wf:r1"))
	if k := kindOf(t, err); k != fwra.NotFound {
		t.Fatalf("resume of an absent project kind = %v, want NotFound", k)
	}
}

// --------------------------------------------------------------------------
// STP 5: RecordPhaseStarted records no lifecycle claim of its own
// --------------------------------------------------------------------------

// Entering a lifecycle phase is not a fact the ledgers are missing: the attempt is. The
// verb used to seed a stored phase set, stamp CurrentPhase and advance a stored coarse
// roll-up — all three derived now (spec §5.3) — so what it leaves behind is the row it
// births and nothing else. The row is still NOT started: a seeded row with no start stamp,
// no ledger and no exit asserts nothing about having run.
func TestRecordPhaseStarted_RecordsNoDerivedClaim(t *testing.T) {
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
	if len(s.Attempts) != 0 {
		t.Fatalf("attempts = %d, want none: entering a phase records no attempt", len(s.Attempts))
	}
	if got := completedPhasesOf(s); len(got) != 0 {
		t.Fatalf("completed phases = %v, want none", got)
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

	// The completion is recorded the ONE way the read path recognises: a passed attempt
	// at the phase's GATE task. The resolved set then reports the phase complete, with
	// the clock and the artifact taken from that attempt — so the record and the
	// derivation are the same fact, not two.
	s := readConstructionStatus(t, store, id, cred, "C005")
	gate, ok := latestAttempt(s.Attempts, GateTaskFor(MethodPhaseRequirements))
	if !ok {
		t.Fatal("RecordPhaseCompleted must record the requirements gate attempt")
	}
	if gate.Outcome != OutcomePassed || gate.Evidence.Ref != "srs/myservice.md" {
		t.Fatalf("gate attempt = %+v, want passed citing srs/myservice.md", gate)
	}
	var reqPhase *PhaseCompletion
	resolved := resolvedOf(s)
	for i := range resolved {
		if resolved[i].Phase == MethodPhaseRequirements {
			reqPhase = &resolved[i]
			break
		}
	}
	if reqPhase == nil {
		t.Fatal("MethodPhaseRequirements not in the resolved set after RecordPhaseCompleted")
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

	// Every gate attempt is on the ledger, so the row's own profile resolves every phase
	// complete — which is what makes the coarse roll-up Done for any reader that knows
	// how to classify the row.
	s := readConstructionStatus(t, store, id, cred, "C006")
	if got, want := len(completedPhasesOf(s)), len(resolvedOf(s)); got != want || want == 0 {
		t.Fatalf("completed phases = %d of %d, want every phase complete", got, want)
	}
	if got := CoarsePhaseFor(s, resolvedOf(s)); got != ActivityConstructionDone {
		t.Fatalf("Phase = %v after all phases completed, want Done", got)
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
	// Nothing was recorded: a phase outside the lifecycle has no gate task to attempt.
	s := readConstructionStatus(t, store, id, cred, "C010")
	if len(s.Attempts) != 0 {
		t.Fatalf("attempts = %d, want none for a phase the lifecycle does not carry", len(s.Attempts))
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

func (s *stubProjectState) RejectArtifactOnBranchWithComments(_ fwra.Context, _ ProjectID, _ Version, _ string, _ ArtifactKind, _ string, _ int64, _ []ReviewComment, _ []ReviewReply, _ fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "RejectArtifactOnBranchWithComments")
	return 30, nil
}

func (s *stubProjectState) SetReviewCommentStatusOnBranch(_ fwra.Context, _ ProjectID, _ Version, _ string, _ ArtifactKind, _ string, _ string, _ fwra.IdempotencyKey) (Version, error) {
	s.calls = append(s.calls, "SetReviewCommentStatusOnBranch")
	return 31, nil
}

func (s *stubProjectState) SeedReviewCommentsOnBranch(_ fwra.Context, _ ProjectID, _ Version, _ string, _ ArtifactKind, _ int64, _ []ReviewComment, _ []ReviewReply, _ fwra.IdempotencyKey) (Version, error) {
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
	v, err := s.RejectArtifactOnBranchWithComments(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, "notes", 0, nil, nil, "idem-1")
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
	v, err := s.SeedReviewCommentsOnBranch(fwra.Context{Context: context.Background()}, "proj-1", 1, "session-branch", KindMission, 0, nil, nil, "idem-1")
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

// TestProjectEnvelope_RoundTrip_PreservesOperatorPaused pins that the RECORDED operator
// pause crosses the Temporal boundary. The construction pump's recorded-pause gate reads
// it from the decoded envelope (I2 ruling, 2026-09-12); dropping it in EncodeProject or
// Decode would silently let a sweep-started pump dispatch through a pause. An unpaused
// project's wire payload must stay byte-identical (no "operatorPaused" key).
func TestProjectEnvelope_RoundTrip_PreservesOperatorPaused(t *testing.T) {
	p := Project{ID: ProjectID(uuid.NewString()), Version: 7, OperatorPaused: true, PauseReason: "operator halt"}
	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	if !env.OperatorPaused || env.PauseReason != "operator halt" {
		t.Fatalf("EncodeProject dropped the recorded pause: %+v", env)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var wire ProjectEnvelope
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	got, err := wire.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.OperatorPaused || got.PauseReason != "operator halt" {
		t.Fatalf("Decode dropped the recorded pause: OperatorPaused=%v PauseReason=%q", got.OperatorPaused, got.PauseReason)
	}

	unpaused, err := EncodeProject(Project{ID: ProjectID(uuid.NewString()), Version: 1})
	if err != nil {
		t.Fatalf("EncodeProject(unpaused): %v", err)
	}
	rawUnpaused, err := json.Marshal(unpaused)
	if err != nil {
		t.Fatalf("marshal unpaused envelope: %v", err)
	}
	if bytes.Contains(rawUnpaused, []byte("operatorPaused")) || bytes.Contains(rawUnpaused, []byte("pauseReason")) {
		t.Fatalf("an unpaused envelope must omit the pause keys, got %s", rawUnpaused)
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
		ActivityExecution: map[string]ActivityExecution{
			"C-A": {
				ActivityID: "C-A",
				StartedAt:  &envelopeStartedAt,
				Attempts: []TaskAttempt{{
					AttemptID: "C-A:srsReview:1", Task: TaskSRSReview, Phase: MethodPhaseRequirements,
					Attempt: 1, Outcome: OutcomePassed,
				}},
				Version: 3,
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

// envelopeStartedAt is the start stamp the envelope round-trip carries. A package-level
// value because the row holds a POINTER to it and a composite literal cannot take the
// address of a call.
var envelopeStartedAt = time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)

// assertConstructionActivityStatusSurvived asserts the activityExecution section
// round-tripped field-for-field.
func assertConstructionActivityStatusSurvived(t *testing.T, back Project) {
	t.Helper()
	acs, ok := back.ActivityExecution["C-A"]
	if !ok {
		t.Fatalf("activityExecution[C-A] must survive the round trip, got %+v", back.ActivityExecution)
	}
	if acs.StartedAt == nil || !acs.StartedAt.Equal(envelopeStartedAt) {
		t.Fatalf("the head facts must survive, got %+v", acs)
	}
	if acs.Version != 3 {
		t.Fatalf("the per-activity version must survive, got %d", acs.Version)
	}
	if len(acs.Attempts) != 1 || acs.Attempts[0].Outcome != OutcomePassed {
		t.Fatalf("the attempt ledger must survive verbatim, got %+v", acs.Attempts)
	}
	// And the derivation the row no longer stores reads off it: the requirements gate
	// passed, so that phase is complete and the activity is running.
	if coarsePhaseOf(acs) != ActivityConstructionRunning {
		t.Fatalf("coarse roll-up = %v, want Running", coarsePhaseOf(acs))
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

	// ReviewPolicy — the phase gate's snapshot source (the DOCUMENT the reviewEngine reads).
	if !slices.Contains(back.ReviewPolicy.GatedPhasesByType["service"], MethodPhaseDetailedDesign) {
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
	if _, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, "please revise", 1, comments, nil, cred, "wf:reject"); err != nil {
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
	if _, err := store.SeedReviewCommentsOnBranch(ctx, id, v2, "", KindMission, 0, comments, nil, cred, "wf:seed"); err != nil {
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
	v3, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, "n", 1, comments, nil, cred, "wf:reject")
	if err != nil {
		t.Fatalf("first reject: %v", err)
	}
	if _, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, "n", 1, comments, nil, cred, "wf:reject"); err != nil {
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

func TestGitStore_SetReviewCommentStatus_ResolveAndReopen(t *testing.T) {
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
	v3, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, "n", 1, comments, nil, cred, "wf:reject")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	// resolve the open comment.
	v4, err := store.SetReviewCommentStatusOnBranch(ctx, id, v3, "", KindMission, "r1c1", ReviewCommentResolved, cred, "wf:resolve")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Mission.ReviewThread[0].Status != ReviewCommentResolved {
		t.Fatalf("status after resolve = %q, want resolved", proj.Mission.ReviewThread[0].Status)
	}
	// resolved->open is the legal reopen; it must set the sticky Reopened bit.
	if _, err := store.SetReviewCommentStatusOnBranch(ctx, id, v4, "", KindMission, "r1c1", ReviewCommentOpen, cred, "wf:reopen"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	proj, err = store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Mission.ReviewThread[0].Status != ReviewCommentOpen {
		t.Fatalf("status after reopen = %q, want open", proj.Mission.ReviewThread[0].Status)
	}
	if !proj.Mission.ReviewThread[0].Reopened {
		t.Fatal("reopen must set the sticky Reopened bit")
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
	if _, err := store.SetReviewCommentStatusOnBranch(ctx, id, v2, "", KindMission, "nope", ReviewCommentResolved, cred, "wf:resolve"); kindOf(t, err) != fwra.NotFound {
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
	v3, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, "n", 1, []ReviewComment{{Anchor: "$.a", Text: "one"}}, nil, cred, "wf:reject")
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
	if ack.Type != ReviewCommentTypeStaleAck || ack.Status != ReviewCommentAnswered || ack.AuthorRole != "architect" {
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

// operatingFixtureRow / operatingFixtureCase decode the SHARED fixture corpus at
// testdata/operating_fixtures.json — shared byte-identically with the webApp TS
// side (see the sync comment atop that file) so both languages assert the exact
// same construction-complete cases. Kept minimal (the committed activity list's
// names, stored phase + buildStatus ints keyed by activity id, project-level phase,
// expected bool): the fixture rows carry stored state only, so the ledger read is
// never reached here — see TestIsConstructionComplete_ReadsTheLedgerWhereThePumpNeverWrote.
type operatingFixtureRow struct {
	Phase       int `json:"phase"`
	BuildStatus int `json:"buildStatus"`
}

// rowFromFixtureOrdinals materializes the fixture's (phase, buildStatus) ordinals as the
// row shape that PRODUCES them. The corpus is shared byte-identically with the webApp
// (contracts/operating.ts deriveOperating), so its cases stay exactly as authored; what
// changed is that a row no longer STORES those two ordinals, so the Go side builds the
// evidence each pair is derived from:
//   - failed: a recorded FailureReason, the sticky terminal.
//   - integrated: an exit stamp AND a full passed ledger — integration is the claim that
//     every lifecycle gate passed, which is exactly what the corpus's skipped-shaped-row
//     case exists to deny (Done alone is not enough).
//   - done, not integrated: an exit stamp and no ledger.
//   - running: a start stamp.
func rowFromFixtureOrdinals(id string, row operatingFixtureRow) ActivityExecution {
	out := ActivityExecution{ActivityID: id}
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	switch {
	case ActivityConstructionPhase(row.Phase) == ActivityConstructionFailed ||
		ActivityBuildStatus(row.BuildStatus) == BuildFailed:
		out.FailureReason = PipelineFailed
		out.CompletedAt = &at
	case ActivityBuildStatus(row.BuildStatus) == BuildIntegrated:
		out.CompletedAt = &at
		out.Attempts = constructionLedger(id, ProfileFor(ActivityTypeService, TestVariantPlan).PhaseIDs()...)
	case ActivityConstructionPhase(row.Phase) == ActivityConstructionDone:
		out.CompletedAt = &at
	case ActivityConstructionPhase(row.Phase) == ActivityConstructionRunning:
		out.StartedAt = &at
	}
	return out
}

type operatingFixtureCase struct {
	Name         string                         `json:"name"`
	Activities   []string                       `json:"activities"`
	Rows         map[string]operatingFixtureRow `json:"rows"`
	ProjectPhase int                            `json:"projectPhase"`
	Expect       bool                           `json:"expect"`
}

// TestIsConstructionComplete_Fixtures drives isConstructionComplete against the
// shared fixture corpus (architect condition: same cases on both the Go and TS
// sides). skipped-shaped-row pins the Done+InReview shape RecordActivityExited
// leaves for a Skipped/TakenOver outcome: Phase alone reaching Done is not enough.
// listed-activity-without-row, rows-without-a-list and
// row-outside-the-list-is-ignored pin Task 7a's rule that the COMMITTED activity
// list, not the set of existing rows, is what must be complete.
func TestIsConstructionComplete_Fixtures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "operating_fixtures.json"))
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var cases []operatingFixtureCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture corpus is empty; this test would pass vacuously")
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			p := Project{Phase: Phase(tc.ProjectPhase)}
			if len(tc.Activities) > 0 {
				items := make([]ActivityItem, 0, len(tc.Activities))
				for _, name := range tc.Activities {
					// WorkerClass + Coding are what type the row at READ time, and the type
					// selects the lifecycle profile every completion is resolved against. The
					// fixture JSON is shared byte-identically with the webApp and stays exactly
					// as authored; only the harness that materializes it knows about types.
					items = append(items, ActivityItem{Name: name, WorkerClass: "junior-developer", Coding: true})
				}
				p.ActivityList = ArtifactSlot{Status: ReviewCommitted, Model: &ActivityList{Activities: items}}
			}
			if len(tc.Rows) > 0 {
				p.ActivityExecution = make(map[string]ActivityExecution, len(tc.Rows))
				for key, row := range tc.Rows {
					p.ActivityExecution[key] = rowFromFixtureOrdinals(key, row)
				}
			}
			if got := isConstructionComplete(p); got != tc.Expect {
				t.Errorf("isConstructionComplete(%s) = %v, want %v", tc.Name, got, tc.Expect)
			}
		})
	}
}

// TestIsConstructionComplete_ReadsTheLedgerWhereThePumpNeverWrote covers what the
// ordinal-only shared fixture cannot: the catalog signal reads each row through
// EffectiveConstructionPhase, so a backfilled row (ledger only, no stored phase fields)
// counts as Done+Integrated beside a pump-written one. A listed activity with no row, or
// an activity list that is not committed, still keeps the project incomplete.
func TestIsConstructionComplete_ReadsTheLedgerWhereThePumpNeverWrote(t *testing.T) {
	items := []ActivityItem{
		{Name: "C-a", WorkerClass: "junior-developer", Coding: true},
		{Name: "C-b", WorkerClass: "junior-developer", Coding: true},
	}
	build := func() Project {
		return Project{
			Phase:        PhaseConstruction,
			ActivityList: ArtifactSlot{Status: ReviewCommitted, Model: &ActivityList{Activities: items}},
			ActivityExecution: map[string]ActivityExecution{
				"C-a": {ActivityID: "C-a", Attempts: constructionLedger("C-a", ProfileFor(ActivityTypeService, TestVariantPlan).PhaseIDs()...)},
				"C-b": {ActivityID: "C-b", CompletedAt: &envelopeStartedAt,
					Attempts: constructionLedger("C-b", ProfileFor(ActivityTypeService, TestVariantPlan).PhaseIDs()...)},
			},
		}
	}
	if !isConstructionComplete(build()) {
		t.Fatal("a ledger-Done row beside a stored Done+Integrated row must read complete")
	}
	noRow := build()
	delete(noRow.ActivityExecution, "C-a")
	if isConstructionComplete(noRow) {
		t.Error("a listed activity with no row has not started; the project must not read complete")
	}
	uncommitted := build()
	uncommitted.ActivityList.Status = ReviewAwaitingReview
	if isConstructionComplete(uncommitted) {
		t.Error("an activity list awaiting review is not the committed plan; the project must not read complete")
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
	if _, err := store.RejectArtifactOnBranchWithComments(ctx, id, v2, "", KindMission, notes, 0, nil, nil, cred, "wf:reject"); err != nil {
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
	_, err := store.RejectArtifactOnBranchWithComments(ctx, id, 1, "", KindMission, "notes", 0, nil, nil, cred, "wf:reject")
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
func readConstruction(t *testing.T, store *GitStore, id ProjectID, cred RepoCredential, activityID string) ActivityExecution {
	t.Helper()
	proj, err := store.ReadProject(fwra.Context{Context: context.Background()}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	s, ok := proj.ActivityExecution[activityID]
	if !ok {
		t.Fatalf("ActivityConstruction[%s] absent; have keys %v", activityID, constructionKeys(proj))
	}
	return s
}

func constructionKeys(p Project) []string {
	out := make([]string, 0, len(p.ActivityExecution))
	for k := range p.ActivityExecution {
		out = append(out, k)
	}
	return out
}

// TestRecordActivityStarted_BirthsRow — RecordActivityStarted births the row with
// Phase=Running and a non-nil StartedAt; CompletedAt must be nil.
func TestRecordActivityStarted_BirthsRow(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2, err := store.RecordActivityStarted(fwra.Context{Context: ctx}, id, v, "X001", ActivityTypeService, TestVariantPlan, cred, fwra.IdempotencyKey("wf:started"))
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
	if coarsePhaseOf(s) != ActivityConstructionRunning {
		t.Fatalf("Phase = %v, want Running", coarsePhaseOf(s))
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

	v2, err := store.RecordActivityStarted(fwra.Context{Context: ctx}, id, v, "X001", ActivityTypeService, TestVariantPlan, cred, fwra.IdempotencyKey("wf:started-done"))
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
	if coarsePhaseOf(s) != ActivityConstructionDone {
		t.Fatalf("Phase = %v, want Done", coarsePhaseOf(s))
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

	v2, err := store.RecordActivityStarted(fwra.Context{Context: ctx}, id, v, "X001", ActivityTypeService, TestVariantPlan, cred, fwra.IdempotencyKey("wf:started-idem"))
	if err != nil {
		t.Fatalf("RecordActivityStarted: %v", err)
	}
	before, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}

	// Retry with the SAME key but stale expectedVersion=0; dedup must win.
	v2again, err := store.RecordActivityStarted(fwra.Context{Context: ctx}, id, 0, "X001", ActivityTypeService, TestVariantPlan, cred, fwra.IdempotencyKey("wf:started-idem"))
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
	p.ActivityExecution = map[string]ActivityExecution{
		"X001": {
			ActivityID:  "X001",
			StartedAt:   &now,
			CompletedAt: &comp,
		},
		"X002": {
			ActivityID: "X002",
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

	x001, found := got.ActivityExecution["X001"]
	if !found {
		t.Fatal("X001 absent after round-trip")
	}
	if coarsePhaseOf(x001) != ActivityConstructionDone {
		t.Fatalf("X001 Phase = %v, want Done", coarsePhaseOf(x001))
	}
	if x001.StartedAt == nil || !x001.StartedAt.Equal(now) {
		t.Fatalf("X001 StartedAt = %v, want %v", x001.StartedAt, now)
	}
	if x001.CompletedAt == nil || !x001.CompletedAt.Equal(comp) {
		t.Fatalf("X001 CompletedAt = %v, want %v", x001.CompletedAt, comp)
	}

	x002, found := got.ActivityExecution["X002"]
	if !found {
		t.Fatal("X002 absent after round-trip")
	}
	if coarsePhaseOf(x002) != ActivityConstructionRunning {
		t.Fatalf("X002 Phase = %v, want Running", coarsePhaseOf(x002))
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

// deploymentRoundTripFixture is a minimal project.json document whose slot-6
// (KindOperationalConcepts) model carries one entry each of
// deployment.infrastructure/bindings/settings, shaped exactly like the restored
// state (2026-08-09 finish-construction plan, Task 2). The generated
// DeploymentTopology Go type (contract.gen.go) currently models only
// deliveryStyle/containers/environments, so decodeProjectDoc silently drops
// these three sections on unmarshal into the typed model — this fixture is the
// input that proves it.
const deploymentRoundTripFixture = `{
  "id": "deployment-roundtrip-project",
  "version": 1,
  "phase": 0,
  "owner": "testowner",
  "name": "deployment roundtrip project",
  "research": {},
  "slots": {
    "6": {
      "status": 2,
      "kind": 6,
      "model": {
        "deploymentScenario": "cloud",
        "constructionVenue": {"kind": "github"},
        "reviewPolicyRef": "",
        "trustSummaries": {"billing": "", "usageMetering": "", "dataOwnership": ""},
        "deployment": {
          "deliveryStyle": 0,
          "containers": [],
          "environments": [],
          "infrastructure": [{"key":"postgres","substrate":"postgres","profiles":["cloud"],"presence":"required","env":{"URL":"ARCHISTRATOR_POSTGRES_URL"}}],
          "bindings": [{"component":"projectStateAccess","presence":"required","provides":[],"settings":[{"name":"projectStateGitRepoURL","type":"string","default":"","env":"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL","description":"projectStateAccess GitLocal on-disk head-state repo URL (file://)."}],"perProfile":{"local":{"variant":"GitLocal","infra":[]},"cloud":{"variant":"GitHub","infra":["github-app"]}}}],
          "settings": [{"name":"listenAddr","type":"string","default":":8080","env":"ARCHISTRATOR_LISTEN_ADDR","description":"HTTP listen address."}]
        }
      }
    }
  }
}`

// TestDeploymentSectionsSurviveRoundTrip is the RED test for the codec drop
// (2026-08-09 finish-construction plan, Task 2 — fixed in Task 3). It decodes
// deploymentRoundTripFixture through decodeProjectDoc (the same decode path
// applyMutationOnBranchFiles uses to load the aggregate for a mutation) and
// re-encodes it through encodeProjectDoc (the same encode path buildStateFiles
// uses to assemble the next commit), then inspects the raw re-encoded JSON's
// slot-6 model.deployment object for the three sections. It must stay red,
// unmodified, until Task 3 adds Infrastructure/Bindings/Settings fields to the
// generated DeploymentTopology type.
// deploymentSectionFromReencoded walks the re-encoded project.json down to
// slot 6's model.deployment object, failing the test if any hop is missing.
func deploymentSectionFromReencoded(t *testing.T, reencoded []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(reencoded, &doc); err != nil {
		t.Fatalf("re-decode re-encoded project.json: %v", err)
	}
	slots, ok := doc["slots"].(map[string]any)
	if !ok {
		t.Fatal("re-encoded document has no slots map")
	}
	slot6, ok := slots["6"].(map[string]any)
	if !ok {
		t.Fatal("re-encoded document has no slot 6 (operationalConcepts)")
	}
	model, ok := slot6["model"].(map[string]any)
	if !ok {
		t.Fatal("re-encoded slot 6 has no model")
	}
	deployment, ok := model["deployment"].(map[string]any)
	if !ok {
		t.Fatal("re-encoded slot 6 model has no deployment object")
	}
	return deployment
}

// requireSingletonArray asserts deployment[field] round-tripped as a
// one-element JSON array, returning it (or nil, after recording a failure).
func requireSingletonArray(t *testing.T, deployment map[string]any, field string) []any {
	t.Helper()
	arr, ok := deployment[field].([]any)
	if !ok || len(arr) != 1 {
		t.Errorf("deployment.%s did not survive the round-trip: got %v", field, deployment[field])
		return nil
	}
	return arr
}

func TestDeploymentSectionsSurviveRoundTrip(t *testing.T) {
	p, ok, err := decodeProjectDoc([]byte(deploymentRoundTripFixture), ProjectID("deployment-roundtrip-project"))
	if err != nil {
		t.Fatalf("decodeProjectDoc: %v", err)
	}
	if !ok {
		t.Fatal("decodeProjectDoc: project not found")
	}

	reencoded, err := encodeProjectDoc(&p, time.Time{})
	if err != nil {
		t.Fatalf("encodeProjectDoc: %v", err)
	}

	deployment := deploymentSectionFromReencoded(t, reencoded)

	if infra := requireSingletonArray(t, deployment, "infrastructure"); infra != nil {
		if entry, ok := infra[0].(map[string]any); !ok || entry["key"] != "postgres" {
			t.Errorf("deployment.infrastructure[0].key did not survive the round-trip: got %v", infra[0])
		}
	}

	requireSingletonArray(t, deployment, "bindings")
	requireSingletonArray(t, deployment, "settings")
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
// of which needs the same live fixture (18 dynamic views, 18 realized — 16 as of
// the Task-10 batch-3 landing (2026-08-01), plus the two nonCore variations the
// D0 design amendment added on 2026-09-19; the Task-8
// batch-1 design amendment (2026-08-01) put explicit TraceCall.Alt values on 12
// of the calls across uc2/uc4's both-surface entry steps, the Task-9 batch-2
// design amendment (2026-08-01) grew that to 52, and the Task-10 batch-3
// design amendment (2026-08-01) grew that to 100 — see wantAltTally below; the
// Task-7 design amendment (2026-08-01) put explicit ActivityNode.DecidedBy
// values on 24 of the 37 decision nodes — see
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
// (view, step, from, to). The true invariant is uniqueness among
// ALT-CARRYING calls only: every (view, step, from, to) that appears as a key
// in wantAltTally below is unique, which is all wantAltTally's lookup needs.
// It is NOT true that no step repeats a (from,to) pair outside an alt group —
// nine committed steps legitimately do (uc1-drive-system-design's new
// `decision` step reads then rejects on the same sdm->psa edge;
// uc2-commit-project-option's `commit-option`; uc3-execute-construction-
// activity's `activity-eligible`, `dispatch-job`, and `record-review-merge`;
// var-manage-projects' new `adopt-repo`, which adopts then seats the repo on
// the same sdm->sca edge; batch-3's var-ask-review-question
// `dispatch-answer-job`, which dispatches then observes the same sdm->aja
// edge; and the Task-11 align-up's two rail-before-dispatch steps —
// uc1-drive-system-design's `dispatch-draft-job` (getInstallationToken then
// openBranch, same sdm->sca edge) and var-replan-scope-change's `reenter`
// (the same pair on pdm->sca)) — none of those repeated pairs carries an alt
// tag, so they never collide in wantAltTally.
type altCallKey struct {
	view, step, from, to string
}

// wantAltTally is the Task-8 architect spec's §2a/§2c alt-group authoring
// (batch 1: uc2-commit-project-option await-decision/review-options,
// uc4-operate-delivered-system publish-trigger — 12 entries), extended by the
// Task-9 architect spec's §3a/§3b/§3c/§4 batch-2 authoring (uc1's
// read-prior-models + human-gate retrofit, uc2's revoke patch (Ruling A1),
// uc3's escalate-operator patch (Ruling A3), and the four newly realized
// views' both-surface entry steps — +40 entries, 52 total), further extended
// by the Task-10 architect spec's §4 batch-3 authoring (the final seven
// views' both-surface entry steps, R-1/R-2 — +48 entries, 100 total),
// value-keyed exactly like wantDecidedByTally below: every both-surface
// entry step pairs the actor->Client leg ("s1") with the Client->Manager leg ("s2") per the
// Task-5 alt-group contract. Every other committed call carries no alt tag.
var wantAltTally = map[altCallKey]string{
	{"uc2-commit-project-option", "await-decision", "architect-user", "web-client"}:                  "s1",
	{"uc2-commit-project-option", "await-decision", "architect-user", "mcp-client"}:                  "s1",
	{"uc2-commit-project-option", "await-decision", "web-client", "project-design-manager"}:          "s2",
	{"uc2-commit-project-option", "await-decision", "mcp-client", "project-design-manager"}:          "s2",
	{"uc2-commit-project-option", "review-options", "architect-user", "web-client"}:                  "s1",
	{"uc2-commit-project-option", "review-options", "architect-user", "mcp-client"}:                  "s1",
	{"uc2-commit-project-option", "review-options", "web-client", "project-design-manager"}:          "s2",
	{"uc2-commit-project-option", "review-options", "mcp-client", "project-design-manager"}:          "s2",
	{"uc4-operate-delivered-system", "publish-trigger", "operator", "web-client"}:                    "s1",
	{"uc4-operate-delivered-system", "publish-trigger", "operator", "mcp-client"}:                    "s1",
	{"uc4-operate-delivered-system", "publish-trigger", "web-client", "operations-manager"}:          "s2",
	{"uc4-operate-delivered-system", "publish-trigger", "mcp-client", "operations-manager"}:          "s2",
	{"uc1-drive-system-design", "read-prior-models", "architect-user", "web-client"}:                 "s1",
	{"uc1-drive-system-design", "read-prior-models", "architect-user", "mcp-client"}:                 "s1",
	{"uc1-drive-system-design", "read-prior-models", "web-client", "system-design-manager"}:          "s2",
	{"uc1-drive-system-design", "read-prior-models", "mcp-client", "system-design-manager"}:          "s2",
	{"uc1-drive-system-design", "human-gate", "architect-user", "web-client"}:                        "s1",
	{"uc1-drive-system-design", "human-gate", "architect-user", "mcp-client"}:                        "s1",
	{"uc1-drive-system-design", "human-gate", "web-client", "system-design-manager"}:                 "s2",
	{"uc1-drive-system-design", "human-gate", "mcp-client", "system-design-manager"}:                 "s2",
	{"uc2-commit-project-option", "revoke", "architect-user", "web-client"}:                          "s1",
	{"uc2-commit-project-option", "revoke", "architect-user", "mcp-client"}:                          "s1",
	{"uc2-commit-project-option", "revoke", "web-client", "project-design-manager"}:                  "s2",
	{"uc2-commit-project-option", "revoke", "mcp-client", "project-design-manager"}:                  "s2",
	{"uc3-execute-construction-activity", "escalate-operator", "operator", "web-client"}:             "s1",
	{"uc3-execute-construction-activity", "escalate-operator", "operator", "mcp-client"}:             "s1",
	{"uc3-execute-construction-activity", "escalate-operator", "web-client", "construction-manager"}: "s2",
	{"uc3-execute-construction-activity", "escalate-operator", "mcp-client", "construction-manager"}: "s2",
	{"var-manage-projects", "prepare-repo", "architect-user", "web-client"}:                          "s1",
	{"var-manage-projects", "prepare-repo", "architect-user", "mcp-client"}:                          "s1",
	{"var-manage-projects", "submit-create", "web-client", "system-design-manager"}:                  "s2",
	{"var-manage-projects", "submit-create", "mcp-client", "system-design-manager"}:                  "s2",
	{"var-manage-projects", "catalog-open", "architect-user", "web-client"}:                          "s1",
	{"var-manage-projects", "catalog-open", "architect-user", "mcp-client"}:                          "s1",
	{"var-manage-projects", "catalog-open", "web-client", "system-design-manager"}:                   "s2",
	{"var-manage-projects", "catalog-open", "mcp-client", "system-design-manager"}:                   "s2",
	{"var-manage-projects", "capture-research", "architect-user", "web-client"}:                      "s1",
	{"var-manage-projects", "capture-research", "architect-user", "mcp-client"}:                      "s1",
	{"var-manage-projects", "capture-research", "web-client", "system-design-manager"}:               "s2",
	{"var-manage-projects", "capture-research", "mcp-client", "system-design-manager"}:               "s2",
	{"var-track-weekly-progress", "week-elapses", "architect-user", "web-client"}:                    "s1",
	{"var-track-weekly-progress", "week-elapses", "architect-user", "mcp-client"}:                    "s1",
	{"var-track-weekly-progress", "week-elapses", "web-client", "system-design-manager"}:             "s2",
	{"var-track-weekly-progress", "week-elapses", "mcp-client", "system-design-manager"}:             "s2",
	{"var-replan-scope-change", "present", "architect-user", "web-client"}:                           "s1",
	{"var-replan-scope-change", "present", "architect-user", "mcp-client"}:                           "s1",
	{"var-replan-scope-change", "present", "web-client", "project-design-manager"}:                   "s2",
	{"var-replan-scope-change", "present", "mcp-client", "project-design-manager"}:                   "s2",
	{"var-replan-scope-change", "mgmt", "architect-user", "web-client"}:                              "s1",
	{"var-replan-scope-change", "mgmt", "architect-user", "mcp-client"}:                              "s1",
	{"var-replan-scope-change", "mgmt", "web-client", "project-design-manager"}:                      "s2",
	{"var-replan-scope-change", "mgmt", "mcp-client", "project-design-manager"}:                      "s2",
	// Task-10 batch-3 additions (2026-08-01, §4 of the batch-3 architect spec):
	// the final seven views' both-surface entry steps (48 entries, all s1/s2
	// pairs). onboard 8 (resolve-app 4, validate-instrument 4), add-use-case 10
	// (capture-uc 2, revalidate 2, reopen-slot 2, redraft-review 4), view-log
	// 4, download 4, cost-projection 8 (open-console 4, request-projection 4),
	// ask 4, send-back 10 (anchor-comments 2, send-back 2, re-review 4,
	// close-comments 2) — 52 + 48 = 100 total.
	{"var-onboard-new-customer", "resolve-app", "architect-user", "web-client"}:            "s1",
	{"var-onboard-new-customer", "resolve-app", "architect-user", "mcp-client"}:            "s1",
	{"var-onboard-new-customer", "resolve-app", "web-client", "billing-manager"}:           "s2",
	{"var-onboard-new-customer", "resolve-app", "mcp-client", "billing-manager"}:           "s2",
	{"var-onboard-new-customer", "validate-instrument", "architect-user", "web-client"}:    "s1",
	{"var-onboard-new-customer", "validate-instrument", "architect-user", "mcp-client"}:    "s1",
	{"var-onboard-new-customer", "validate-instrument", "web-client", "billing-manager"}:   "s2",
	{"var-onboard-new-customer", "validate-instrument", "mcp-client", "billing-manager"}:   "s2",
	{"var-add-use-case", "capture-uc", "architect-user", "web-client"}:                     "s1",
	{"var-add-use-case", "capture-uc", "architect-user", "mcp-client"}:                     "s1",
	{"var-add-use-case", "revalidate", "web-client", "system-design-manager"}:              "s2",
	{"var-add-use-case", "revalidate", "mcp-client", "system-design-manager"}:              "s2",
	{"var-add-use-case", "reopen-slot", "web-client", "system-design-manager"}:             "s2",
	{"var-add-use-case", "reopen-slot", "mcp-client", "system-design-manager"}:             "s2",
	{"var-add-use-case", "redraft-review", "architect-user", "web-client"}:                 "s1",
	{"var-add-use-case", "redraft-review", "architect-user", "mcp-client"}:                 "s1",
	{"var-add-use-case", "redraft-review", "web-client", "system-design-manager"}:          "s2",
	{"var-add-use-case", "redraft-review", "mcp-client", "system-design-manager"}:          "s2",
	{"var-view-state-log", "open-history", "operator", "web-client"}:                       "s1",
	{"var-view-state-log", "open-history", "operator", "mcp-client"}:                       "s1",
	{"var-view-state-log", "open-history", "web-client", "system-design-manager"}:          "s2",
	{"var-view-state-log", "open-history", "mcp-client", "system-design-manager"}:          "s2",
	{"var-download-source", "open-repo", "architect-user", "web-client"}:                   "s1",
	{"var-download-source", "open-repo", "architect-user", "mcp-client"}:                   "s1",
	{"var-download-source", "open-repo", "web-client", "construction-manager"}:             "s2",
	{"var-download-source", "open-repo", "mcp-client", "construction-manager"}:             "s2",
	{"var-view-cost-projection", "open-console", "operator", "web-client"}:                 "s1",
	{"var-view-cost-projection", "open-console", "operator", "mcp-client"}:                 "s1",
	{"var-view-cost-projection", "open-console", "web-client", "operations-manager"}:       "s2",
	{"var-view-cost-projection", "open-console", "mcp-client", "operations-manager"}:       "s2",
	{"var-view-cost-projection", "request-projection", "operator", "web-client"}:           "s1",
	{"var-view-cost-projection", "request-projection", "operator", "mcp-client"}:           "s1",
	{"var-view-cost-projection", "request-projection", "web-client", "operations-manager"}: "s2",
	{"var-view-cost-projection", "request-projection", "mcp-client", "operations-manager"}: "s2",
	{"var-ask-review-question", "write-questions", "architect-user", "web-client"}:         "s1",
	{"var-ask-review-question", "write-questions", "architect-user", "mcp-client"}:         "s1",
	{"var-ask-review-question", "write-questions", "web-client", "system-design-manager"}:  "s2",
	{"var-ask-review-question", "write-questions", "mcp-client", "system-design-manager"}:  "s2",
	{"var-send-back-redraft", "anchor-comments", "architect-user", "web-client"}:           "s1",
	{"var-send-back-redraft", "anchor-comments", "architect-user", "mcp-client"}:           "s1",
	{"var-send-back-redraft", "send-back", "web-client", "system-design-manager"}:          "s2",
	{"var-send-back-redraft", "send-back", "mcp-client", "system-design-manager"}:          "s2",
	{"var-send-back-redraft", "re-review", "architect-user", "web-client"}:                 "s1",
	{"var-send-back-redraft", "re-review", "architect-user", "mcp-client"}:                 "s1",
	{"var-send-back-redraft", "re-review", "web-client", "system-design-manager"}:          "s2",
	{"var-send-back-redraft", "re-review", "mcp-client", "system-design-manager"}:          "s2",
	{"var-send-back-redraft", "close-comments", "web-client", "system-design-manager"}:     "s2",
	{"var-send-back-redraft", "close-comments", "mcp-client", "system-design-manager"}:     "s2",
	// D0 additions (2026-09-19, founder-approved design amendment): the two new
	// nonCore variations of execute-a-construction-activity and their views —
	// var-resume-paused-construction (see-paused 2, choose-resume 4, retry-later 2)
	// and var-requeue-failed-activity (review-failure 2, write-change 4). Both
	// enter construction-manager and construction-manager ONLY (Don't 6a /
	// DV-SINGLE-MGR), which is why the paused read is drawn by the actor legs here
	// and the getProject read stays on the views that already draw it. The
	// operator-laned choose-resume / write-change steps carry BOTH legs (CC-ACTOR-
	// LANE: a laned node's step must touch its actor). +14 entries, 114 total.
	{"var-resume-paused-construction", "see-paused", "operator", "web-client"}:                "s1",
	{"var-resume-paused-construction", "see-paused", "operator", "mcp-client"}:                "s1",
	{"var-resume-paused-construction", "choose-resume", "operator", "web-client"}:             "s1",
	{"var-resume-paused-construction", "choose-resume", "operator", "mcp-client"}:             "s1",
	{"var-resume-paused-construction", "choose-resume", "web-client", "construction-manager"}: "s2",
	{"var-resume-paused-construction", "choose-resume", "mcp-client", "construction-manager"}: "s2",
	{"var-resume-paused-construction", "retry-later", "operator", "web-client"}:               "s1",
	{"var-resume-paused-construction", "retry-later", "operator", "mcp-client"}:               "s1",
	{"var-requeue-failed-activity", "review-failure", "operator", "web-client"}:               "s1",
	{"var-requeue-failed-activity", "review-failure", "operator", "mcp-client"}:               "s1",
	{"var-requeue-failed-activity", "write-change", "operator", "web-client"}:                 "s1",
	{"var-requeue-failed-activity", "write-change", "operator", "mcp-client"}:                 "s1",
	{"var-requeue-failed-activity", "write-change", "web-client", "construction-manager"}:     "s2",
	{"var-requeue-failed-activity", "write-change", "mcp-client", "construction-manager"}:     "s2",
}

// TestCommittedProjectJSON_DynamicViewCalls_Alt is the tolerant-decode regression
// for TraceCall.Alt (rollout rulings 2026-07-31), extended by Task 8 (2026-08-01)
// to pin the VALUES batch-1 actually authored, by Task 9 (2026-08-01) to
// extend the pin over batch-2's additions, and by Task 10 (2026-08-01) to
// extend the pin over batch-3's additions (the final seven views — all 16
// dynamic views now realized), rather than only asserting absence: a call
// that never mentions "alt" must decode EXACTLY as it did before the field
// existed (Alt reads back nil, not a zero-value string standing in for
// absence), and a call that IS one of wantAltTally's 114 entries must decode
// to exactly its authored group value.
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
							"the authored alt groups — tolerant decode or authoring regressed",
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

// The three design activity types (spec 2026-09-20 §5.1). Their wire names ARE the
// method-assets lifecycle keys, so a plan activity of this type resolves its lifecycle
// with no special case. Ordinals 7/8/9 are appended, never inserted.
func TestActivityType_DesignTypesRoundTripAndKeyTheirLifecycles(t *testing.T) {
	cases := []struct {
		typ  ActivityType
		name string
	}{
		{ActivityTypeRequirements, "requirements"},
		{ActivityTypeArchitecture, "architecture"},
		{ActivityTypeProjectDesign, "projectDesign"},
	}
	for _, c := range cases {
		if got := c.typ.String(); got != c.name {
			t.Errorf("%d.String() = %q, want %q", int(c.typ), got, c.name)
		}
		raw, err := json.Marshal(c.typ)
		if err != nil {
			t.Fatalf("marshal %s: %v", c.name, err)
		}
		if string(raw) != `"`+c.name+`"` {
			t.Errorf("marshal %s = %s, want %q", c.name, raw, c.name)
		}
		var back ActivityType
		if err := json.Unmarshal(raw, &back); err != nil || back != c.typ {
			t.Errorf("round trip %s: got %v, %v", c.name, back, err)
		}
		if got := LifecycleKeyFor(c.typ, TestVariantPlan); got != c.name {
			t.Errorf("LifecycleKeyFor(%s) = %q, want %q", c.name, got, c.name)
		}
		if len(ProfileFor(c.typ, TestVariantPlan).Phases) == 0 {
			t.Errorf("%s has no lifecycle phases — the method-assets pin does not carry it", c.name)
		}
	}
	// 0-6 are untouched and the three are APPENDED: the count is the cheapest proof
	// that nothing was inserted in the middle of the wire vocabulary.
	if len(activityTypeNames) != 10 {
		t.Fatalf("activityTypeNames holds %d types, want 10", len(activityTypeNames))
	}
	for ordinal, want := range map[ActivityType]string{7: "requirements", 8: "architecture", 9: "projectDesign"} {
		if got := activityTypeNames[ordinal]; got != want {
			t.Errorf("ordinal %d = %q, want %q", int(ordinal), got, want)
		}
	}
}

// The phase shapes the three design lifecycles publish, pinned so a method-assets
// release that reshapes them fails here rather than silently reshaping the console.
func TestProfileFor_DesignLifecycleShapes(t *testing.T) {
	want := map[ActivityType][]string{
		ActivityTypeRequirements:  {"mission", "glossary", "volatilities", "coreUseCases"},
		ActivityTypeArchitecture:  {"architecture"},
		ActivityTypeProjectDesign: {"sdp"},
	}
	for typ, ids := range want {
		var got []string
		for _, p := range ProfileFor(typ, TestVariantPlan).PhaseIDs() {
			got = append(got, string(p))
		}
		if !slices.Equal(got, ids) {
			t.Errorf("%s phases = %v, want %v", typ, got, ids)
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
	in := ActivityExecution{
		ActivityID: "C-CW",
		Type:       ActivityTypeFrontend,
		Produced: []ProducedArtifact{
			{Kind: "service-contract", Title: "webClient — service contract", Source: "implementation/contracts/webClient.md", Produced: true, Note: "frozen App-B contract"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ActivityExecution
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Type != ActivityTypeFrontend || len(out.Produced) != 1 || out.Produced[0].Source != "implementation/contracts/webClient.md" {
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

// ComponentUnresolved is the terminal FailureReason recorded when the construction
// pump cannot dispatch an eligible activity because its authored componentId names no
// component in the committed systemDesign. Its wire name is what the console renders,
// so it is pinned here alongside the load-bearing ordinals of its predecessors.
//
// DependencyUnresolved (ordinal 7) and DependencyCycle (ordinal 8) are the sibling
// dependency-graph FailureReason variants minted in c2c33ab: a dangling dependency id
// (names neither an authored activity nor an authored milestone) vs. a dependency
// cycle (every id resolves; the topology is broken). Pinned here too, alongside every
// pre-existing ordinal, so a reordering of the whole enum fails loudly — the persisted
// ordinals are load-bearing: every failure record already on disk decodes through them.
func TestFailureReason_ComponentUnresolvedWireName(t *testing.T) {
	if got := ComponentUnresolved.String(); got != "componentUnresolved" {
		t.Fatalf("want componentUnresolved, got %q", got)
	}
	if ComponentUnresolved != 6 {
		t.Fatalf("ComponentUnresolved must be ordinal 6, got %d", ComponentUnresolved)
	}
	if got := DependencyUnresolved.String(); got != "dependencyUnresolved" {
		t.Fatalf("want dependencyUnresolved, got %q", got)
	}
	if DependencyUnresolved != 7 {
		t.Fatalf("DependencyUnresolved must be ordinal 7, got %d", DependencyUnresolved)
	}
	if got := DependencyCycle.String(); got != "dependencyCycle" {
		t.Fatalf("want dependencyCycle, got %q", got)
	}
	if DependencyCycle != 8 {
		t.Fatalf("DependencyCycle must be ordinal 8, got %d", DependencyCycle)
	}
	// The persisted ordinals of the pre-existing variants are load-bearing: every
	// failure record already on disk decodes through them.
	if FailureReasonUnknown != 0 || PipelineFailed != 1 || PipelineCancelled != 2 ||
		PipelineTimedOut != 3 || VarianceExhausted != 4 || EscalationTimedOut != 5 {
		t.Fatal("existing FailureReason ordinals must not move")
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
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
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

func TestLegacyActivityConstructionRow_CarriesAPreRenameEntryForward(t *testing.T) {
	// A row written before stage 3's wire break: the five derived members and no head
	// facts but the coarse roll-up. LegacyActivityConstructionRow is the one typed reader
	// they still have, and it carries forward what the new shape can hold — the type, the
	// produced artifacts, and, crucially, the TERMINALITY the roll-up asserted.
	raw := `{"activityID":"C-CW","phase":2,"kind":1,"type":1,"buildStatus":2,"phases":[{"phase":"requirements","weight":40,"completed":true}],"currentPhase":"integration","produced":[{"Kind":"service-contract","Title":"webClient","Source":"implementation/contracts/webClient.md","Produced":true}]}`
	var legacy LegacyActivityConstructionRow
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		t.Fatalf("unmarshal legacy entry: %v", err)
	}
	if legacy.Phase != LegacyPhaseDone || legacy.Kind != ActivityKindFrontend ||
		legacy.CurrentPhase != MethodPhaseIntegration || len(legacy.Phases) != 1 {
		t.Fatalf("the migration's reader must still see every derived member: %+v", legacy)
	}
	got := legacy.toActivityExecution()
	if got.ActivityID != "C-CW" {
		t.Errorf("ActivityID = %q, want C-CW", got.ActivityID)
	}
	if got.Type != ActivityTypeFrontend {
		t.Errorf("Type = %v, want Frontend", got.Type)
	}
	if len(got.Produced) != 1 {
		t.Errorf("Produced must carry forward, got %+v", got.Produced)
	}
	// A stored Done with no completion stamp of its own still reads as exited, or the
	// pump would re-dispatch every finished activity the instant this decoder ran.
	if got.CompletedAt == nil {
		t.Fatal("a legacy Done row must carry an exit stamp forward")
	}
	if coarsePhaseOf(got) != ActivityConstructionDone {
		t.Errorf("coarse roll-up = %v, want Done", coarsePhaseOf(got))
	}
}

func TestCoarsePhase_AllDone(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
	for i := range phases {
		phases[i].Completed = true
	}
	if got := CoarsePhase(phases); got != ActivityConstructionDone {
		t.Errorf("CoarsePhase(all done) = %v, want Done", got)
	}
}

func TestCoarsePhase_NoneStarted(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
	if got := CoarsePhase(phases); got != ActivityConstructionNotStarted {
		t.Errorf("CoarsePhase(none started) = %v, want NotStarted", got)
	}
}

func TestCoarsePhase_SomeCompleted(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
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

func TestCoarseBuildStatus_IntegratedWhenAllPhasesDone(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
	// Mark every phase done.
	for i := range phases {
		phases[i].Completed = true
	}
	if got := CoarseBuildStatus(phases); got != BuildIntegrated {
		t.Errorf("CoarseBuildStatus(all phases done) = %v, want Integrated", got)
	}
}

func TestCoarseBuildStatus_InReview(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
	// Mark only Construction done (not Integration).
	for i := range phases {
		if phases[i].Phase == MethodPhaseConstruction {
			phases[i].Completed = true
		}
	}
	if got := CoarseBuildStatus(phases); got != BuildInReview {
		t.Errorf("CoarseBuildStatus(construction done, integration not) = %v, want InReview", got)
	}
}

func TestCoarseBuildStatus_InConstruction(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
	if got := CoarseBuildStatus(phases); got != BuildInConstruction {
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

// ---- Earned value over the RESOLVED set -------------------------------------------
//
// ActivityProgress takes the resolved phase set, not the row: the stored slice it used to
// sum is gone (stage-3 task 4), and a row-shaped signature would have returned 0 for
// every activity in the project — earned value collapsing to zero with no test failing.
// ProjectEarnedValue resolves each row itself, from its ledger and its committed plan
// item, so the curve and the status chip beside it read the same evidence.

// ledgerRow builds a service row whose ledger records the named lifecycle phases complete
// — the evidence a resolved completion is derived FROM.
func ledgerRow(id string, complete ...ActivityMethodPhase) ActivityExecution {
	return ActivityExecution{ActivityID: id, Type: ActivityTypeService, Attempts: constructionLedger(id, complete...)}
}

// codingMeta is the committed activity list item that types a row as a coding service
// activity, which is what selects the 15/20/10/40/15 profile at read time.
func codingMeta(ids ...string) map[string]ActivityItem {
	out := make(map[string]ActivityItem, len(ids))
	for _, id := range ids {
		out[id] = ActivityItem{Name: id, WorkerClass: "junior-developer", Coding: true}
	}
	return out
}

func TestActivityProgress_None(t *testing.T) {
	if got := ActivityProgress(ProfileFor(ActivityTypeService, 0).toPhaseCompletions()); got != 0 {
		t.Errorf("ActivityProgress(none done) = %d, want 0", got)
	}
}

func TestActivityProgress_FirstPhase(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions() // 15/20/10/40/15
	phases[0].Completed = true                                        // Requirements = 15%
	if got := ActivityProgress(phases); got != 15 {
		t.Errorf("ActivityProgress(requirements done) = %d, want 15", got)
	}
}

func TestActivityProgress_ThreePhases(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions() // 15/20/10/40/15
	phases[0].Completed = true                                        // 15
	phases[1].Completed = true                                        // 20
	phases[2].Completed = true                                        // 10 → total 45
	if got := ActivityProgress(phases); got != 45 {
		t.Errorf("ActivityProgress(3 phases) = %d, want 45", got)
	}
}

func TestActivityProgress_AllDone(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
	for i := range phases {
		phases[i].Completed = true
	}
	if got := ActivityProgress(phases); got != 100 {
		t.Errorf("ActivityProgress(all done) = %d, want 100", got)
	}
}

func TestActivityProgress_EmptyPhases(t *testing.T) {
	if got := ActivityProgress(nil); got != 0 {
		t.Errorf("ActivityProgress(nil phases) = %d, want 0", got)
	}
}

// TestActivityProgress_OverAResolvedLedger is the CARRY-OVER guard: the progress a row
// earns comes from its ledger through the resolver, with the PROFILE's weights, so a row
// that stores no phase set at all still earns what its gates prove.
func TestActivityProgress_OverAResolvedLedger(t *testing.T) {
	row := ledgerRow("C-PE", MethodPhaseRequirements, MethodPhaseDetailedDesign)
	_, _, resolved, classified := ResolveConstructionRow(row, codingMeta("C-PE")["C-PE"])
	if !classified {
		t.Fatal("a coding service activity must classify")
	}
	if got := ActivityProgress(resolved); got != 35 {
		t.Errorf("ActivityProgress(requirements+detailedDesign) = %d, want 35", got)
	}
}

func TestProjectEarnedValue_Empty(t *testing.T) {
	if got := ProjectEarnedValue(nil, nil, nil); got != 0.0 {
		t.Errorf("ProjectEarnedValue(empty) = %f, want 0.0", got)
	}
}

func TestProjectEarnedValue_ZeroEffort(t *testing.T) {
	// All activities have zero effort: edge case — return 0
	rows := []ActivityExecution{ledgerRow("C-PE", MethodPhaseRequirements)}
	got := ProjectEarnedValue(rows, codingMeta("C-PE"), map[string]float64{"C-PE": 0.0})
	if got != 0.0 {
		t.Errorf("ProjectEarnedValue(zero effort) = %f, want 0.0", got)
	}
}

func TestProjectEarnedValue_OneActivity_HalfDone(t *testing.T) {
	rows := []ActivityExecution{ledgerRow("C-PE", MethodPhaseRequirements, MethodPhaseDetailedDesign)} // 15 + 20
	got := ProjectEarnedValue(rows, codingMeta("C-PE"), map[string]float64{"C-PE": 10.0})
	// Σ(E_i × A_i) / Σ E_i = (10 × 0.35) / 10 = 0.35
	if got < 0.34 || got > 0.36 {
		t.Errorf("ProjectEarnedValue = %f, want ~0.35", got)
	}
}

func TestProjectEarnedValue_TwoActivities(t *testing.T) {
	all := ProfileFor(ActivityTypeService, TestVariantPlan).PhaseIDs()
	rows := []ActivityExecution{
		ledgerRow("C-A", all...), // 100%
		ledgerRow("C-B"),         // 0% — an opened row whose gates have decided nothing
	}
	rows[1].Attempts = []TaskAttempt{constructionAttempt("C-B", TaskSRS, 1, OutcomePending)}
	got := ProjectEarnedValue(rows, codingMeta("C-A", "C-B"), map[string]float64{"C-A": 5.0, "C-B": 15.0})
	// Σ(E_i × A_i) / Σ E_i = (5×1.0 + 15×0.0) / 20 = 5/20 = 0.25
	if got < 0.24 || got > 0.26 {
		t.Errorf("ProjectEarnedValue = %f, want ~0.25", got)
	}
}

func TestProjectEarnedValue_NilEffortMap_DefaultsToEqualWeight(t *testing.T) {
	// When effortDays is nil (or activity missing), each activity defaults to E=1.0.
	all := ProfileFor(ActivityTypeService, TestVariantPlan).PhaseIDs()
	rows := []ActivityExecution{ledgerRow("C-A", all...), ledgerRow("C-B")}
	rows[1].Attempts = []TaskAttempt{constructionAttempt("C-B", TaskSRS, 1, OutcomePending)}
	got := ProjectEarnedValue(rows, codingMeta("C-A", "C-B"), nil)
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
		if got := deriveVariant(id); got != want {
			t.Errorf("deriveVariant(%q) = %v, want %v", id, got, want)
		}
	}
}

// The command family each profile dispatches into, one cell per family: the words the
// lifecycle data states, not a slug this package composes. profileSlug's own table test
// retired with it — a UI-design activity still walks the FRONTEND commands and an I-*
// activity still walks /service-integration, and that is what these rows pin.
func TestCommandFor_NamesTheProfilesCommandFamily(t *testing.T) {
	cases := []struct {
		t    ActivityType
		v    TestingVariant
		p    ActivityMethodPhase
		want string
	}{
		{ActivityTypeService, 0, MethodPhaseRequirements, "service-requirements"},
		{ActivityTypeFrontend, 0, MethodPhaseRequirements, "frontend-requirements"},
		{ActivityTypeUIDesign, 0, MethodPhaseDetailedDesign, "frontend-detailed-design"},
		{ActivityTypeIntegration, 0, MethodPhaseIntegration, "service-integration"},
		{ActivityTypeDeployment, 0, MethodPhaseConstruction, "deployment-construction"},
		{ActivityTypeDocumentation, 0, MethodPhaseConstruction, "documentation-construction"},
		{ActivityTypeTesting, TestVariantPlan, MethodPhaseConstruction, "testing-plan-construction"},
		{ActivityTypeTesting, TestVariantHarness, MethodPhaseConstruction, "testing-harness-construction"},
		{ActivityTypeTesting, TestVariantPerf, MethodPhaseConstruction, "testing-perf-construction"},
		{ActivityTypeTesting, TestVariantSystemTest, MethodPhaseConstruction, "testing-systemtest-construction"},
		{ActivityTypeTesting, TestVariantQAProcess, MethodPhaseConstruction, "testing-qa-construction"},
	}
	for _, c := range cases {
		if got := CommandFor(c.t, c.v, c.p); got != c.want {
			t.Errorf("CommandFor(%v,%v,%q) = %q, want %q", c.t, c.v, c.p, got, c.want)
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

// CommandFor is total over exactly the phases ProfileFor emits: every one names a
// well-formed command slug. That it names a command file that EXISTS is
// TestEveryProfilePhaseHasCommandFile, below; the composition rule itself is no longer
// this package's to state — the lifecycle data carries the name.
func TestCommandForTotalOverProfiles(t *testing.T) {
	slug := regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	for _, combo := range allProfileCombos() {
		for _, p := range ProfileFor(combo.t, combo.v).PhaseIDs() {
			got := CommandFor(combo.t, combo.v, p)
			if got == "" {
				t.Errorf("CommandFor(%v,%v,%q) empty", combo.t, combo.v, p)
			}
			if !slug.MatchString(got) {
				t.Errorf("CommandFor(%v,%v,%q) = %q, not a command slug", combo.t, combo.v, p, got)
			}
		}
	}
}

// CARRY-OVER from Task 3's review (binding): with cmd/gen-uiprofiles gone, the claim "a
// profile's command matches its own phase" was covered only by a distinct-count sanity
// check (TestEveryActivityTypeResolvesToALifecycle asserts CommandFor is non-empty, not
// that it is the RIGHT command). This is the real oracle: for every (type, variant,
// carried) phase, CommandFor must equal the lifecycle's own dispatch task's Command for
// that phase — recomputed HERE straight off methodassets.LifecycleFor's raw Tasks, never
// through CommandFor's own dispatchTaskIn helper, so a bug in that helper cannot hide
// behind a test that calls it too.
func TestCommandFor_MatchesTheLifecyclesDispatchTaskCommand(t *testing.T) {
	for _, combo := range allActivityTypeCombos() {
		key := LifecycleKeyFor(combo.t, combo.v)
		lc, ok := methodassets.LifecycleFor(key)
		if !ok {
			t.Fatalf("no lifecycle %q", key)
		}
		for _, ph := range lc.Phases {
			p := ActivityMethodPhase(ph.ID)
			want := rawDispatchCommand(lc, ph.ID)
			if got := CommandFor(combo.t, combo.v, p); got != want {
				t.Errorf("%s/%s: CommandFor = %q, want the lifecycle's own dispatch task command %q", key, p, got, want)
			}
		}
	}
}

// rawDispatchCommand is a phase's dispatch-task command read straight off the
// lifecycle's own Tasks, never through the production dispatchTaskIn helper, so a bug
// in that helper cannot hide behind a test that calls it too. "" when the phase has no
// dispatch task at all — projectDesign's `sdp` is one review gate and nothing else.
func rawDispatchCommand(lc methodassets.Lifecycle, phaseID string) string {
	for _, task := range lc.Tasks {
		if task.Phase == phaseID && task.Kind == methodassets.LifecycleTaskDispatch {
			return task.Command
		}
	}
	return ""
}

type profileCombo struct {
	t ActivityType
	v TestingVariant
}

// allProfileCombos enumerates the CONSTRUCTION (type, variant) profiles: the ones
// whose phases walk the .claude command matrix. It is deliberately not every type —
// allActivityTypeCombos is.
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

// allActivityTypeCombos enumerates every distinct (type, variant) an activity can
// carry: the construction profiles, the two types whose phases are not a command
// family (UIDesign, Integration), and the three design types the plan's fixed prefix
// uses. Totality over THIS list is what makes
// TestEveryLifecycleIsReachableFromAnActivityType total — it replaces the seeded
// designLifecycleKeys set that stood in for the design types while they had no
// ActivityType.
func allActivityTypeCombos() []profileCombo {
	return append(allProfileCombos(),
		profileCombo{ActivityTypeUIDesign, 0},
		profileCombo{ActivityTypeIntegration, 0},
		profileCombo{ActivityTypeRequirements, 0},
		profileCombo{ActivityTypeArchitecture, 0},
		profileCombo{ActivityTypeProjectDesign, 0},
	)
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
		if c.Response != nil {
			t.Errorf("comment %d response = %v, want nil (never written)", i, c.Response)
		}
		if len(c.Replies) != 0 {
			t.Errorf("comment %d replies = %+v, want none", i, c.Replies)
		}
		if c.Reopened {
			t.Errorf("comment %d reopened = true, want false", i)
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

// TestNormalizeReviewThread_ReplyHistoryDecidesStatus supersedes the retired
// Response-presence rule: status is now derived from the reply thread (see
// TestNormalizeReviewThreadDerivesFromLastReply for the full derive-rule matrix). This
// keeps the original 3-row shape (agent-answered / agent-claimed-without-a-reply /
// resolved-stays-sticky) under the new vocabulary.
func TestNormalizeReviewThread_ReplyHistoryDecidesStatus(t *testing.T) {
	agentReply := ReviewCommentReply{ID: "r1c1-u1", AuthorRole: "architect", Text: "fixed the vision", At: "2026-09-19T00:00:00Z"}
	thread := []ReviewComment{
		{ID: "r1c1", Status: ReviewCommentOpen, Replies: []ReviewCommentReply{agentReply}}, // agent replied last
		{ID: "r1c2", Status: ReviewCommentAnswered},                                        // agent claimed answered w/o a reply
		{ID: "r1c3", Status: ReviewCommentResolved},                                        // resolved stays sticky
	}
	got := normalizeReviewThread(thread)
	if got[0].Status != ReviewCommentAnswered {
		t.Errorf("agent-replied comment status = %q, want answered", got[0].Status)
	}
	if got[1].Status != ReviewCommentOpen {
		t.Errorf("no-reply comment status = %q, want open (server overrides the agent's claim)", got[1].Status)
	}
	if got[2].Status != ReviewCommentResolved {
		t.Errorf("resolved comment status = %q, want resolved (sticky)", got[2].Status)
	}
}

// TestApplyReviewCommentStatus_LegalTransitions supersedes the retired open->waived /
// addressed->open pair with the new open|answered->resolved (close) and resolved->open
// (reopen) transitions (see TestApplyReviewCommentStatusTransitions for the full matrix
// including the reopen's Reopened bit + preserved reply history).
func TestApplyReviewCommentStatus_LegalTransitions(t *testing.T) {
	thread := []ReviewComment{
		{ID: "r1c1", Status: ReviewCommentOpen},
		{ID: "r1c2", Status: ReviewCommentAnswered},
	}
	// open -> resolved (close).
	got, err := applyReviewCommentStatus(thread, "r1c1", ReviewCommentResolved)
	if err != nil {
		t.Fatalf("open->resolved: %v", err)
	}
	if got[0].Status != ReviewCommentResolved {
		t.Errorf("r1c1 status = %q, want resolved", got[0].Status)
	}
	// answered -> resolved (close).
	got, err = applyReviewCommentStatus(got, "r1c2", ReviewCommentResolved)
	if err != nil {
		t.Fatalf("answered->resolved: %v", err)
	}
	if got[1].Status != ReviewCommentResolved {
		t.Errorf("r1c2 status = %q, want resolved", got[1].Status)
	}
}

func TestApplyReviewCommentStatus_IllegalTransitionAndUnknownID(t *testing.T) {
	thread := []ReviewComment{{ID: "r1c1", Status: ReviewCommentOpen}}
	// open -> open is not a legal human transition.
	if _, err := applyReviewCommentStatus(thread, "r1c1", ReviewCommentOpen); kindOfErr(err) != fwra.ContractMisuse {
		t.Errorf("open->open kind = %v, want ContractMisuse", kindOfErr(err))
	}
	// unknown id is NotFound.
	if _, err := applyReviewCommentStatus(thread, "nope", ReviewCommentResolved); kindOfErr(err) != fwra.NotFound {
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

// legacyResponsePtr is a small test helper — ReviewComment.Response is the deprecated
// *string kept only for the read-compat shim (RULING P8: KEPT, not deleted, as a
// never-written pointer so a pre-thread ledger committed to git still decodes); literal
// construction in a legacy-ledger test needs an addressable value.
func legacyResponsePtr(s string) *string { return &s }

func TestNormalizeReviewThreadDerivesFromLastReply(t *testing.T) {
	agent := ReviewCommentReply{ID: "r1", AuthorRole: "architect", Text: "Split it.", At: "2026-09-19T00:00:00Z"}
	human := ReviewCommentReply{ID: "r2", AuthorRole: "architect-user", Text: "Still wrong.", At: "2026-09-19T01:00:00Z"}

	cases := []struct {
		name string
		in   ReviewComment
		want string
	}{
		{"no replies is open", ReviewComment{ID: "c1", Status: ReviewCommentOpen}, ReviewCommentOpen},
		{"agent reply last is answered", ReviewComment{ID: "c2", Replies: []ReviewCommentReply{agent}}, ReviewCommentAnswered},
		{"reviewer reply last reopens", ReviewComment{ID: "c3", Replies: []ReviewCommentReply{agent, human}}, ReviewCommentOpen},
		{"reopened bit beats an agent reply", ReviewComment{ID: "c4", Replies: []ReviewCommentReply{agent}, Reopened: true}, ReviewCommentOpen},
		{"resolved is sticky", ReviewComment{ID: "c5", Status: ReviewCommentResolved}, ReviewCommentResolved},
		{"staleAck is sticky", ReviewComment{ID: "c6", Type: ReviewCommentTypeStaleAck, Status: ReviewCommentAnswered}, ReviewCommentAnswered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeReviewThread([]ReviewComment{tc.in})
			if got[0].Status != tc.want {
				t.Fatalf("status = %q, want %q", got[0].Status, tc.want)
			}
		})
	}
}

func TestApplyReviewCommentStatusTransitions(t *testing.T) {
	agent := ReviewCommentReply{ID: "r1", AuthorRole: "architect", Text: "done", At: "2026-09-19T00:00:00Z"}
	legal := []struct{ from, to string }{
		{ReviewCommentOpen, ReviewCommentResolved},
		{ReviewCommentAnswered, ReviewCommentResolved},
		{ReviewCommentResolved, ReviewCommentOpen},
	}
	for _, tc := range legal {
		thread := []ReviewComment{{ID: "c1", Status: tc.from, Replies: []ReviewCommentReply{agent}}}
		got, err := applyReviewCommentStatus(thread, "c1", tc.to)
		if err != nil {
			t.Fatalf("%s->%s: unexpected error %v", tc.from, tc.to, err)
		}
		if got[0].Status != tc.to {
			t.Fatalf("%s->%s: status = %q", tc.from, tc.to, got[0].Status)
		}
	}

	// A reopen sets the sticky bit and KEEPS the reply history.
	thread := []ReviewComment{{ID: "c1", Status: ReviewCommentResolved, Replies: []ReviewCommentReply{agent}}}
	got, err := applyReviewCommentStatus(thread, "c1", ReviewCommentOpen)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !got[0].Reopened {
		t.Fatal("reopen must set the sticky Reopened bit")
	}
	if len(got[0].Replies) != 1 {
		t.Fatalf("reopen must preserve replies, got %d", len(got[0].Replies))
	}

	// Illegal: answered -> open is the manager's derived business, not a human verb.
	if _, err := applyReviewCommentStatus(
		[]ReviewComment{{ID: "c1", Status: ReviewCommentAnswered}}, "c1", ReviewCommentAnswered,
	); err == nil {
		t.Fatal("expected an error for a no-op transition")
	}
}

func TestNormalizeReviewThreadReadCompat(t *testing.T) {
	legacy := []ReviewComment{
		{ID: "c1", Status: "addressed", Response: legacyResponsePtr("Reworded it.")},
		{ID: "c2", Status: "waived"},
	}
	got := normalizeReviewThread(migrateLegacyReviewThread(legacy, "architect", "2026-01-01T00:00:00Z"))
	if got[0].Status != ReviewCommentAnswered {
		t.Fatalf("legacy addressed should read as answered, got %q", got[0].Status)
	}
	if len(got[0].Replies) != 1 || got[0].Replies[0].Text != "Reworded it." {
		t.Fatalf("legacy response should synthesize one reply, got %+v", got[0].Replies)
	}
	if got[1].Status != ReviewCommentResolved {
		t.Fatalf("legacy waived should read as resolved, got %q", got[1].Status)
	}
}

// TestDecodeProjectJSON_MigratesLegacyReviewThreadOnRead is the fix-round-1 covering
// test (Important finding): migrateLegacyReviewThread was defined but never called
// from any read path, so a committed pre-thread ledger never actually migrated —
// decodeSlotsMap read the raw "addressed"/"waived" strings straight through forever,
// a value validReviewCommentStatus no longer accepts. The raw JSON below is the EXACT
// shape of all 27 legacy entries in the real committed .aiarch/state/project.json as
// of this fix: a staleAck audit entry, status "addressed", response present but
// EMPTY (not absent — decodeSlotsMap must handle a non-nil-but-empty *string, not
// just a nil one). It goes through DecodeProjectJSON, the single exported decode seam
// both the live git store (decodeProjectFromSnapshot) and the CI validator use, so
// this proves the fix at the actual boundary every reader shares — not just a direct
// call to the migrate helper.
func TestDecodeProjectJSON_MigratesLegacyReviewThreadOnRead(t *testing.T) {
	raw := []byte(`{
		"id": "proj-1",
		"version": 1,
		"phase": 1,
		"slots": {
			"1": {
				"kind": 1,
				"status": 2,
				"reviewThread": [
					{
						"id": "r1c1",
						"anchor": "",
						"anchorText": "",
						"text": "Reviewed — unaffected: diagrams only, no term changes",
						"authorRole": "architect",
						"round": 1,
						"status": "addressed",
						"response": "",
						"type": "staleAck",
						"addressee": ""
					}
				]
			}
		}
	}`)
	p, ok, err := DecodeProjectJSON(raw, ProjectID("proj-1"))
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON: want ok=true for a populated document")
	}
	thread := p.Glossary.ReviewThread
	if len(thread) != 1 {
		t.Fatalf("want 1 review comment, got %d", len(thread))
	}
	c := thread[0]
	if c.Status != ReviewCommentAnswered {
		t.Fatalf("legacy staleAck \"addressed\" must decode as %q, got %q (validReviewCommentStatus no longer accepts %q)",
			ReviewCommentAnswered, c.Status, "addressed")
	}
	if !validReviewCommentStatus(c.Status) {
		t.Errorf("decoded status %q must be a currently-valid wire value", c.Status)
	}
	// The real data's response is present but EMPTY, not a real reply — migration must
	// NOT synthesize a reply from it.
	if len(c.Replies) != 0 {
		t.Errorf("an empty legacy response must not synthesize a reply, got %+v", c.Replies)
	}
	// The migration is transient (render-on-read): it must not touch the decoded
	// struct's Response field itself, only Status/Replies.
	if c.Response == nil || *c.Response != "" {
		t.Errorf("Response must decode verbatim (present, empty), got %v", c.Response)
	}
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
		{"answered question does NOT block", ReviewComment{Status: ReviewCommentAnswered, Type: ReviewCommentTypeQuestion}, true, false},
		{"answered change-request does NOT block", ReviewComment{Status: ReviewCommentAnswered}, false, false},
		{"resolved change-request does NOT block", ReviewComment{Status: ReviewCommentResolved}, false, false},
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

// An answered question normalizes to answered (agent reply last), an unanswered one
// stays open — and either way a question never blocks approve.
func TestNormalize_QuestionStatusAndGate(t *testing.T) {
	thread := []ReviewComment{
		{ID: "q1", Status: ReviewCommentOpen, Type: ReviewCommentTypeQuestion},
		{ID: "q2", Status: ReviewCommentOpen, Type: ReviewCommentTypeQuestion, Replies: []ReviewCommentReply{
			{ID: "q2-u1", AuthorRole: "architect", Text: "because X", At: "2026-09-19T00:00:00Z"},
		}},
	}
	out := normalizeReviewThread(thread)
	if out[0].Status != ReviewCommentOpen {
		t.Errorf("unanswered question must stay open, got %q", out[0].Status)
	}
	if out[1].Status != ReviewCommentAnswered {
		t.Errorf("answered question must be answered, got %q", out[1].Status)
	}
	for _, c := range out {
		if ReviewCommentBlocksApprove(c) {
			t.Errorf("a question must never block approve: %+v", c)
		}
	}
}

// A staleAck audit entry is sticky: normalization never reopens it (no replies, but it must
// stay answered), and appendStaleAck stamps it answered + staleAck with a fresh id.
func TestStaleAck_AppendAndNormalizeSticky(t *testing.T) {
	out := appendStaleAck(nil, "architect", "diagrams only")
	if len(out) != 1 {
		t.Fatalf("want 1 entry, got %d", len(out))
	}
	e := out[0]
	if e.Type != ReviewCommentTypeStaleAck || e.Status != ReviewCommentAnswered || e.AuthorRole != "architect" {
		t.Fatalf("staleAck shape wrong: %+v", e)
	}
	// Normalize must NOT flip the staleAck (no replies) to open.
	normalized := normalizeReviewThread(out)
	if normalized[0].Status != ReviewCommentAnswered {
		t.Errorf("staleAck must stay answered after normalize, got %q", normalized[0].Status)
	}
	if ReviewCommentBlocksApprove(normalized[0]) {
		t.Errorf("staleAck must never block approve")
	}
}

// The nine TestReviewPolicy_* gate cases MOVED to
// internal/engine/review/engine_test.go's
// Test_ProposeReviews_ConstructionGate_MatchesTheRetiredEffectiveGate when
// EffectiveGate/RequiresHuman moved into the reviewEngine (spec 2026-09-20 §5.4,
// stage 2). They are transcribed row for row there — this package no longer decides
// anything about the policy, so there is nothing left here to pin. What stays is the
// SHAPING of the client's gate-id vocabulary into the stored document, below, and the
// floor's data list further down.

// TestReviewPolicyFromGateIDs_MapsMockIDs pins the shaping, not a decision: the webApp
// PolicyPanel's ad-hoc gate id "svc-contract" must land in the committed document as
// the canonical detailed_design phase, so the mock vocabulary never reaches head-state.
func TestReviewPolicyFromGateIDs_MapsMockIDs(t *testing.T) {
	p := ReviewPolicyFromGateIDs(map[string][]string{"service": {"svc-contract"}})
	if !slices.Contains(p.GatedPhasesByType["service"], MethodPhaseDetailedDesign) {
		t.Errorf("svc-contract must map to detailed_design, got %v", p.GatedPhasesByType["service"])
	}
}

// TestCreateProject_LeavesReviewPolicyPresetUnset pins the corrected birth default: a
// FRESH project (the "project.json is first materialized" path — CreateProject's
// modeCreateOnly branch) is born with ReviewPolicy.Preset UNSET (nil, the
// legacy/explicit mode), NOT "vibes".
//
// This REPLACES the Task 7 pin (2026-07-19), whose stated rationale — "behavior-
// preserving, an empty GatedPhasesByType already gated nothing, so the explicit
// default changes no dispatch decision" — was falsified one day later by the design
// vibes autogate (2026-07-20): systemdesign/projectdesign set policyAutoApprove from
// ReviewPolicy.Preset DIRECTLY, so a birth-seeded "vibes" auto-approved every design
// draft and committed it without the architect (the rails ask the reviewEngine for that
// verdict now, but the preset is still its only input). The human design gate — the Method's
// commit authority — was gone for every project ever created, which is what the UC1/UC2
// agentic E2E system tests exist to prove.
//
// Nil keeps CONSTRUCTION behavior byte-identical (the reviewEngine's legacy-preset arm
// looks the phase up in an empty map — ungated, floor unchanged; see
// Test_ProposeReviews_ConstructionGate_MatchesTheRetiredEffectiveGate's "legacy" row),
// so this only restores the design gate.
func TestCreateProject_LeavesReviewPolicyPresetUnset(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	id := ProjectID("fresh-project")
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	p, err := store.ReadProject(fwra.Context{Context: ctx}, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	// Preset is the design autogate's ONLY input (policyAutoApprove), so any non-nil
	// value born here — "vibes" above all — silently removes the human design gate.
	if p.ReviewPolicy.Preset != nil {
		t.Fatalf("fresh project ReviewPolicy.Preset = %q, want unset (nil) — a birth-seeded preset auto-approves every design draft", *p.ReviewPolicy.Preset)
	}
}

// ---- Tests: CodecCarriesEveryMember (the derived-plan writer's loss guard) ---------

// legacyStaleAckSlotJSON is the EXACT shape slots 9 and 10 of the repo's own committed
// project.json carry today: a staleAck review comment written before the Reopened/Replies
// members existed and before Status was derived from the reply history. Decoding it runs
// normalizeReviewThread, which rewrites "addressed" into the derived "answered" and fills
// the two missing members — so the slot's stored bytes and its codec encoding differ
// although the codec carries every last field of it.
const legacyStaleAckSlotJSON = `{"status":"committed","reviewThread":[` +
	`{"id":"r1c1","anchor":"","anchorText":"","text":"Reviewed — unaffected: Slot-5 system amendment (rev 2)",` +
	`"authorRole":"architect","round":1,"status":"addressed","response":"","type":"staleAck","addressee":""}]}`

// normalizedStaleAckSlotJSON is what the codec writes for the value above.
const normalizedStaleAckSlotJSON = `{"status":"committed","reviewThread":[` +
	`{"id":"r1c1","anchor":"","anchorText":"","text":"Reviewed — unaffected: Slot-5 system amendment (rev 2)",` +
	`"authorRole":"architect","round":1,"status":"answered","reopened":false,"replies":null,` +
	`"response":"","type":"staleAck","addressee":""}]}`

// The defect this guard was written for: the derived-plan writer refused to rewrite slots
// 9 and 10 because their bytes are not their codec encoding — but the whole difference is
// the codec's OWN normalizer rewriting a legacy Status and filling two members the
// document predates. Nothing is dropped, so nothing may be refused.
func TestCodecCarriesEveryMember_ANormalizedLegacyReviewThreadLosesNothing(t *testing.T) {
	lost, err := CodecCarriesEveryMember(json.RawMessage(legacyStaleAckSlotJSON), json.RawMessage(normalizedStaleAckSlotJSON))
	if err != nil {
		t.Fatalf("CodecCarriesEveryMember: %v", err)
	}
	if len(lost) != 0 {
		t.Fatalf("the codec's own normalization drops nothing, got lost=%v", lost)
	}
	// And the two really are different bytes — otherwise the test proves nothing.
	if legacyStaleAckSlotJSON == normalizedStaleAckSlotJSON {
		t.Fatal("the fixture must reproduce the byte difference the writer refused on")
	}
}

// A genuinely foreign change — a member the codec does not carry, which is the change
// that makes a rewrite lossy — must still be refused, and the guard must NAME it.
func TestCodecCarriesEveryMember_RefusesWhatTheCodecDrops(t *testing.T) {
	tests := []struct {
		name     string
		stored   string
		encoded  string
		wantLost []string
	}{
		{
			name:     "an unknown field the codec never decoded",
			stored:   `{"status":"committed","legacyOnly":{"note":"hand-authored"}}`,
			encoded:  `{"status":"committed"}`,
			wantLost: []string{"legacyOnly"},
		},
		{
			name:     "an unknown field nested inside a carried one",
			stored:   `{"reviewThread":[{"id":"r1c1","provenance":"hand"}]}`,
			encoded:  `{"reviewThread":[{"id":"r1c1"}]}`,
			wantLost: []string{"reviewThread[0].provenance"},
		},
		{
			name:     "a whole comment the encoding no longer holds",
			stored:   `{"reviewThread":[{"id":"r1c1"},{"id":"r1c2"}]}`,
			encoded:  `{"reviewThread":[{"id":"r1c1"}]}`,
			wantLost: []string{"reviewThread[1]"},
		},
		{
			name:     "a structure flattened to a scalar",
			stored:   `{"model":{"activities":[]}}`,
			encoded:  `{"model":""}`,
			wantLost: []string{"model"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lost, err := CodecCarriesEveryMember(json.RawMessage(tt.stored), json.RawMessage(tt.encoded))
			if err != nil {
				t.Fatalf("CodecCarriesEveryMember: %v", err)
			}
			if !slices.Equal(lost, tt.wantLost) {
				t.Fatalf("lost = %v, want %v", lost, tt.wantLost)
			}
		})
	}
}

// Members the ENCODING adds are the normalizer filling in what the document omitted, and
// an identical member pair loses nothing — the two ends of the range.
func TestCodecCarriesEveryMember_AddedAndIdenticalMembersAreNotLosses(t *testing.T) {
	for _, tt := range []struct{ name, stored, encoded string }{
		{"the normalizer adds a member", `{"id":"r1c1"}`, `{"id":"r1c1","reopened":false,"replies":null}`},
		{"identical", legacyStaleAckSlotJSON, legacyStaleAckSlotJSON},
		{"key order alone", `{"a":1,"b":2}`, `{"b":2,"a":1}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lost, err := CodecCarriesEveryMember(json.RawMessage(tt.stored), json.RawMessage(tt.encoded))
			if err != nil || len(lost) != 0 {
				t.Fatalf("lost = %v, err = %v", lost, err)
			}
		})
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
	if !slices.Contains(got.ReviewPolicy.GatedPhasesByType["service"], MethodPhaseDetailedDesign) {
		t.Error("service/detailed_design lost across round-trip")
	}
	if !slices.Contains(got.ReviewPolicy.GatedPhasesByType["service"], MethodPhaseIntegration) {
		t.Error("service/integration lost across round-trip")
	}
	if !slices.Contains(got.ReviewPolicy.GatedPhasesByType["frontend"], MethodPhaseDetailedDesign) {
		t.Error("frontend/detailed_design lost across round-trip")
	}
	if slices.Contains(got.ReviewPolicy.GatedPhasesByType["frontend"], MethodPhaseConstruction) {
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

// TestArtifactSlot_JSONRoundTrip covers ArtifactSlot's custom MarshalJSON/
// UnmarshalJSON (projectstateaccess.go, added for operationsManager's
// projectStateAccess.readProject Temporal Activity boundary — the first consumer
// to pass a full Project through plain encoding/json instead of the git/postgres
// substrate's own slotJSON codec). Covers both documented states: a populated
// model (the ArtifactModel envelope round-trips byte-for-byte) and a nil model
// (the documented "no model yet" zero value).
func TestArtifactSlot_JSONRoundTrip(t *testing.T) {
	t.Run("populated model", func(t *testing.T) {
		want := ArtifactSlot{
			Status: ReviewCommitted,
			Model: &DeploymentOperationsModel{
				DeploymentScenario: ScenarioKind("cloud"),
				ReviewPolicyRef:    "policy-1",
				Deployment: DeploymentTopology{
					Containers: []DeployContainer{{Key: "archistrator-server", Name: "archistrator-server"}},
				},
			},
			Notes:     "some review notes",
			Revisions: 3,
		}

		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}

		var got ArtifactSlot
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		if got.Status != want.Status || got.Notes != want.Notes || got.Revisions != want.Revisions {
			t.Fatalf("scalar fields did not round-trip: got %+v, want %+v", got, want)
		}
		gotModel, ok := got.Model.(*DeploymentOperationsModel)
		if !ok {
			t.Fatalf("Model did not decode back to *DeploymentOperationsModel, got %T", got.Model)
		}
		if !reflect.DeepEqual(gotModel, want.Model) {
			t.Errorf("Model did not round-trip:\ngot  %+v\nwant %+v", gotModel, want.Model)
		}
	})

	t.Run("nil model", func(t *testing.T) {
		want := ArtifactSlot{Status: ReviewNone, Notes: "no model drafted yet"}

		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}

		var got ArtifactSlot
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		if got.Model != nil {
			t.Errorf("Model = %#v, want nil", got.Model)
		}
		if got.Status != want.Status || got.Notes != want.Notes {
			t.Errorf("scalar fields did not round-trip: got %+v, want %+v", got, want)
		}
	})
}

// TestArtifactSlot_UnmarshalJSON_RejectsNonEnvelopeModel covers the review
// finding this task's ArtifactSlot codec originally missed: a "Model" JSON value
// that is present but NOT a {"kind":...,"model":...} envelope (e.g. a producer
// that wrote json.Marshal(concreteModel) directly, bypassing EncodeModel) must
// fail loudly, not silently decode to a nil model. No such producer exists
// today; this guards against one ever landing unnoticed.
func TestArtifactSlot_UnmarshalJSON_RejectsNonEnvelopeModel(t *testing.T) {
	raw := []byte(`{"Status":1,"Model":{"reviewPolicyRef":"policy-1"},"Notes":""}`)

	var got ArtifactSlot
	err := json.Unmarshal(raw, &got)
	if err == nil {
		t.Fatalf("want an error decoding a non-envelope Model, got nil (Model=%#v)", got.Model)
	}
	if !strings.Contains(err.Error(), "kind") {
		t.Errorf("error should name the missing envelope discriminator, got: %v", err)
	}
}

func TestActivityItem_ComponentIDRoundTrips(t *testing.T) {
	in := ActivityList{Activities: []ActivityItem{
		{Name: "C-TLM", Title: "TodoListManager", Coding: true, ComponentID: "todo-list-manager"},
		{Name: "N-STP", Title: "System Test Plan", Coding: false},
	}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// omitempty: the noncoding entry must not carry a misleading empty string.
	if strings.Contains(string(b), `"componentId":""`) {
		t.Fatalf("empty componentId must be omitted, got %s", b)
	}
	if !strings.Contains(string(b), `"componentId":"todo-list-manager"`) {
		t.Fatalf("authored componentId missing from wire form, got %s", b)
	}
	var out ActivityList
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Activities[0].ComponentID != "todo-list-manager" {
		t.Fatalf("want todo-list-manager, got %q", out.Activities[0].ComponentID)
	}
	if out.Activities[1].ComponentID != "" {
		t.Fatalf("want empty, got %q", out.Activities[1].ComponentID)
	}
}

func TestLayerString(t *testing.T) {
	for layer, want := range map[Layer]string{
		LayerClient:         "client",
		LayerManager:        "manager",
		LayerEngine:         "engine",
		LayerResourceAccess: "resourceAccess",
		LayerResource:       "resource",
		LayerUtility:        "utility",
	} {
		if got := layer.String(); got != want {
			t.Fatalf("Layer(%d).String() = %q, want %q", layer, got, want)
		}
	}
}

// TestFinalizeDraftModel_CoreUseCases pins the server-side NAME→ID finalize pass
// (name-as-identity): agents author names only; the server assigns UseCase.ID =
// Slug(Name) and Actor.ID = Slug(Role), re-points VariationOf at the slug, and
// re-anchors LinkedActorID/DecidedBy actor references onto the normalized ids.
// Its absence let CoreUseCases commit with raw-name use-case ids and EMPTY actor
// ids, which no System realization could root an actor→Client chain on.
func TestFinalizeDraftModel_CoreUseCases(t *testing.T) {
	variation := "Work the list"
	lane := "Team Member"
	decider := "Team member"
	componentRef := "todoListManager"
	c := &CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{
			Name: "Work the list",
			Actors: []Actor{
				{Role: "Team member"},
				{Role: "Automated tooling"},
			},
			Activity: &ActivityDiagram{Nodes: []ActivityNode{
				{ID: "l1", Kind: NodeSwimLane, RoleName: "Team member", LinkedActorID: &lane},
				{ID: "d1", Kind: NodeDecision, Label: "route", DecidedBy: &decider},
				{ID: "d2", Kind: NodeDecision, Label: "route2", DecidedBy: &componentRef},
			}},
		}},
		{UseCase: UseCase{Name: "Review the list", VariationOf: &variation}},
	}}
	if err := FinalizeDraftModel(c); err != nil {
		t.Fatalf("FinalizeDraftModel: %v", err)
	}
	uc := c.Decisions[0].UseCase
	if uc.ID != "work-the-list" {
		t.Fatalf("UseCase.ID = %q, want slug of the name", uc.ID)
	}
	if uc.Actors[0].ID != "team-member" || uc.Actors[1].ID != "automated-tooling" {
		t.Fatalf("actor ids = %q,%q, want role slugs", uc.Actors[0].ID, uc.Actors[1].ID)
	}
	if got := *uc.Activity.Nodes[0].LinkedActorID; got != "team-member" {
		t.Fatalf("LinkedActorID = %q, want the normalized actor id", got)
	}
	if got := *uc.Activity.Nodes[1].DecidedBy; got != "team-member" {
		t.Fatalf("actor DecidedBy = %q, want the normalized actor id", got)
	}
	if got := *uc.Activity.Nodes[2].DecidedBy; got != "todoListManager" {
		t.Fatalf("component DecidedBy = %q, must be left untouched", got)
	}
	if got := *c.Decisions[1].UseCase.VariationOf; got != "work-the-list" {
		t.Fatalf("VariationOf = %q, want the core use case's slug", got)
	}
	// Idempotence: a second pass is a no-op.
	if err := FinalizeDraftModel(c); err != nil {
		t.Fatalf("second FinalizeDraftModel: %v", err)
	}
	if c.Decisions[0].UseCase.Actors[0].ID != "team-member" {
		t.Fatalf("finalize must be idempotent")
	}
}

// TestFinalizeDraftModel_EmptySlugFails: a name that slugs to nothing is an
// actionable error, not a silently-empty identity.
func TestFinalizeDraftModel_EmptySlugFails(t *testing.T) {
	c := &CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{Name: "Work the list", Actors: []Actor{{Role: "!!!"}}}},
	}}
	if err := FinalizeDraftModel(c); err == nil {
		t.Fatalf("an actor role slugging to \"\" must fail the finalize pass")
	}
}

// TestValidateModelIdentities_CoreUseCases pins the identity-integrity gate on
// the exact shape that escaped every existing check: Actor.required was already
// ["id","role"], so an actor with id "" is schema-VALID. Non-emptiness is a Go
// invariant, and it must name the offending entity so the agent can fix it.
func TestValidateModelIdentities_CoreUseCases(t *testing.T) {
	blank := ""
	c := &CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{
			ID:     "work-the-list",
			Name:   "Work the list",
			Actors: []Actor{{ID: "", Role: "Team member"}, {ID: "automated-tooling", Role: "Automated tooling"}},
			Activity: &ActivityDiagram{
				Nodes: []ActivityNode{{ID: "", Kind: NodeAction, Label: "add a todo"}},
				Edges: []ActivityEdge{{From: "n1", To: ""}},
			},
		}},
		{UseCase: UseCase{ID: "", Name: "Review the list", VariationOf: &blank}},
	}}
	err := ValidateModelIdentities(c)
	if err == nil {
		t.Fatalf("empty identities must be rejected")
	}
	msg := err.Error()
	for _, want := range []string{
		`use case "Work the list" actor 1 (role "Team member"): id is empty`,
		`use case "Work the list" node 1 (label "add a todo"): id is empty`,
		`use case "Work the list" edge 1: to is empty`,
		`use case "Review the list": id is empty`,
		`use case "Review the list": variationOf is empty`,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message must name the offender %q; got:\n%s", want, msg)
		}
	}
}

// TestValidateModelIdentities_System covers the identity graph the architecture
// carries: component ids, relationship endpoints, and dynamic-view references.
func TestValidateModelIdentities_System(t *testing.T) {
	s := &System{
		Components:    []Component{{ID: "", Name: "TodoListManager"}, {ID: "listAccess", Name: "ListAccess"}},
		Relationships: []Relationship{{From: "todoListManager", To: "", Label: "reads"}},
		DynamicViews: []DynamicView{{
			UseCaseID: "", Key: "work-the-list", Title: "Work the list",
			Steps: []CallStep{{ActivityNodeID: "n1", Calls: []TraceCall{{From: "team-member", To: ""}}}},
		}},
	}
	err := ValidateModelIdentities(s)
	if err == nil {
		t.Fatalf("empty identities must be rejected")
	}
	for _, want := range []string{
		`component "TodoListManager": id is empty`,
		`relationship 1 (label "reads"): to is empty`,
		`dynamic view "Work the list": useCaseId is empty`,
		`dynamic view "Work the list" step 1 call 1: to is empty`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message must name the offender %q; got:\n%s", want, err.Error())
		}
	}
}

// TestValidateModelIdentities_CleanModelsPass: a fully-identified model, and a
// model kind this gate does not cover, both pass silently.
func TestValidateModelIdentities_CleanModelsPass(t *testing.T) {
	c := &CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{ID: "work-the-list", Name: "Work the list", Actors: []Actor{{ID: "team-member", Role: "Team member"}}}},
	}}
	if err := ValidateModelIdentities(c); err != nil {
		t.Fatalf("a fully-identified CoreUseCases must pass: %v", err)
	}
	if err := ValidateModelIdentities(&Glossary{}); err != nil {
		t.Fatalf("an uncovered kind must pass: %v", err)
	}
}

func TestLatestAttempt_ReturnsHighestAttemptNumber(t *testing.T) {
	attempts := []TaskAttempt{
		{AttemptID: AttemptID("C-x", TaskDetailedDesign, 1), Task: TaskDetailedDesign, Attempt: 1, Outcome: OutcomePassed},
		{AttemptID: AttemptID("C-x", TaskDesignReview, 1), Task: TaskDesignReview, Attempt: 1, Outcome: OutcomeRejected},
		{AttemptID: AttemptID("C-x", TaskDetailedDesign, 2), Task: TaskDetailedDesign, Attempt: 2, Outcome: OutcomePassed},
	}
	got, ok := latestAttempt(attempts, TaskDetailedDesign)
	if !ok {
		t.Fatal("latestAttempt(detailedDesign) not found")
	}
	if got.Attempt != 2 {
		t.Errorf("latestAttempt(detailedDesign).Attempt = %d, want 2", got.Attempt)
	}
}

// LatestAttempt must select on the attempt NUMBER, not on slice position. Every other
// fixture happens to store attempts in ascending order, so a naive "return the last
// matching element" implementation would pass them all. This one stores attempt 2
// BEFORE attempt 1 for the same task; the ledger is append-only but nothing in the
// type guarantees a caller hands it a sorted slice.
func TestLatestAttempt_IgnoresSliceOrder(t *testing.T) {
	attempts := []TaskAttempt{
		{AttemptID: AttemptID("C-x", TaskDetailedDesign, 2), Task: TaskDetailedDesign, Attempt: 2, Outcome: OutcomePassed},
		{AttemptID: AttemptID("C-x", TaskDetailedDesign, 1), Task: TaskDetailedDesign, Attempt: 1, Outcome: OutcomeRejected},
	}
	got, ok := latestAttempt(attempts, TaskDetailedDesign)
	if !ok {
		t.Fatal("latestAttempt(detailedDesign) not found")
	}
	if got.Attempt != 2 {
		t.Errorf("LatestAttempt returned attempt %d for an out-of-order slice, want 2", got.Attempt)
	}
	if got.Outcome != OutcomePassed {
		t.Errorf("LatestAttempt returned outcome %q, want %q — it picked the wrong record", got.Outcome, OutcomePassed)
	}
}

func TestLatestAttempt_MissingTaskReportsNotFound(t *testing.T) {
	if _, ok := latestAttempt(nil, TaskCodeReview); ok {
		t.Error("latestAttempt(nil) reported found, want not found")
	}
}

// App A's binary exit, verbatim: a phase is complete iff its GATE task's latest
// attempt passed. A passed non-gate task earns nothing.
func TestPhaseCompleteFromAttempts_RequiresTheGateTask(t *testing.T) {
	// Construction task passed but code review has not happened.
	attempts := []TaskAttempt{
		{AttemptID: AttemptID("C-x", TaskConstruction, 1), Task: TaskConstruction, Attempt: 1, Outcome: OutcomePassed},
	}
	// No gate attempt at all: the ledger has NO OPINION. Not "incomplete" — undecided.
	if complete, decided := phaseCompleteFromAttempts(attempts, MethodPhaseConstruction); complete || decided {
		t.Errorf("no codeReview attempt = (%v, %v), want (false, false) — silence is not a denial", complete, decided)
	}
	attempts = append(attempts, TaskAttempt{
		AttemptID: AttemptID("C-x", TaskCodeReview, 1), Task: TaskCodeReview, Attempt: 1, Outcome: OutcomePassed,
	})
	if complete, decided := phaseCompleteFromAttempts(attempts, MethodPhaseConstruction); !complete || !decided {
		t.Errorf("passed codeReview = (%v, %v), want (true, true)", complete, decided)
	}
}

// The distinction a one-bool version cannot express, and the reason this helper was
// dead while the read path reimplemented it inline: a REJECTED gate and a MISSING gate
// attempt both used to return plain false. They mean opposite things to a caller
// deciding whether to let a stored completion stand.
func TestPhaseCompleteFromAttempts_RejectedGateIsDecidedIncomplete(t *testing.T) {
	attempts := []TaskAttempt{
		{AttemptID: AttemptID("C-x", TaskCodeReview, 1), Task: TaskCodeReview, Attempt: 1, Outcome: OutcomePassed},
		{AttemptID: AttemptID("C-x", TaskCodeReview, 2), Task: TaskCodeReview, Attempt: 2, Outcome: OutcomeRejected},
	}
	complete, decided := phaseCompleteFromAttempts(attempts, MethodPhaseConstruction)
	if complete {
		t.Error("phase reported complete when the LATEST gate attempt was rejected")
	}
	if !decided {
		t.Error("a REJECTED gate is a decision, not silence — decided must be true, or the read path will let a stale stored completion stand")
	}
	// A phase outside the vocabulary has no gate, so nothing is decided.
	if complete, decided := phaseCompleteFromAttempts(attempts, ActivityMethodPhase("not-a-phase")); complete || decided {
		t.Errorf("unknown phase = (%v, %v), want (false, false)", complete, decided)
	}
}

// The rule under test is the struct TAG, not the round-trip. encoding/json never
// treats a non-pointer struct-typed field as empty, so an omitempty on Provenance
// would be a silent no-op TODAY and a live bug the moment the field becomes a
// pointer or the encoder's emptiness rule changes. A marshal-and-grep assertion
// passes either way and therefore proves nothing; assert the tag itself.
func TestTaskAttempt_ProvenanceIsNotOmitempty(t *testing.T) {
	f, ok := reflect.TypeFor[TaskAttempt]().FieldByName("Provenance")
	if !ok {
		t.Fatal("TaskAttempt has no Provenance field")
	}
	if got := f.Tag.Get("json"); got != "provenance" {
		t.Errorf(`TaskAttempt.Provenance json tag = %q, want exactly "provenance" — a missing or dropped provenance stamp must never be omitted from the wire`, got)
	}
}

// Evidence is encoded on every attempt, even when it is empty (Task 8 review, item
// 1). The committed corpus carries 132 `"evidence":{}` entries. An omitempty or
// omitzero tag would silently drop every one of them on the next encode, and the
// rest of the suite would stay green: the reviewer proved that by mutation. So the
// tag must be exactly "evidence".
func TestTaskAttempt_EvidenceIsNotOmitempty(t *testing.T) {
	f, ok := reflect.TypeFor[TaskAttempt]().FieldByName("Evidence")
	if !ok {
		t.Fatal("TaskAttempt has no Evidence field")
	}
	if got := f.Tag.Get("json"); got != "evidence" {
		t.Errorf(`TaskAttempt.Evidence json tag = %q, want exactly "evidence" (no omitempty, no omitzero): an empty evidence ref must still be encoded`, got)
	}
}

// The activityConstruction map key IS the row's identity (Task 8 review, item 2).
// A row stored under another activity's key must fail decode as TERMINAL, naming
// both ids. A matching key, and a legacy row with no activityID, decode cleanly.
func TestDecodeProjectJSON_ActivityConstructionKeyMismatch_IsTerminal(t *testing.T) {
	id := ProjectID("11111111-1111-1111-1111-111111111111")
	encode := func(rows map[string]ActivityExecution) []byte {
		t.Helper()
		raw, err := EncodeProjectJSON(Project{ID: id, ActivityExecution: rows})
		if err != nil {
			t.Fatalf("EncodeProjectJSON: %v", err)
		}
		return raw
	}
	for name, rows := range map[string]map[string]ActivityExecution{
		"matching key":          {"C-a": {ActivityID: "C-a"}},
		"legacy row with no id": {"C-a": {}},
		"several matching rows": {"C-a": {ActivityID: "C-a"}, "N-STP": {ActivityID: "N-STP"}},
	} {
		if _, _, err := DecodeProjectJSON(encode(rows), id); err != nil {
			t.Errorf("%s: want a clean decode, got %v", name, err)
		}
	}

	_, _, err := DecodeProjectJSON(encode(map[string]ActivityExecution{
		"C-a": {ActivityID: "C-a"},
		"C-b": {ActivityID: "C-c"},
	}), id)
	if err == nil {
		t.Fatal("a row whose activityID differs from its map key must FAIL decode")
	}
	if k := kindOf(t, err); k != fwra.ContractMisuse {
		t.Fatalf("decode error kind = %v, want ContractMisuse (terminal: malformed committed state)", k)
	}
	if !strings.Contains(err.Error(), `activityExecution["C-b"]`) || !strings.Contains(err.Error(), `"C-c"`) {
		t.Errorf("the error must name the key and the stored id; got: %v", err)
	}
}

// Phase is denormalized from PhaseForTask(Task). Assert the rule itself over all
// twelve tasks so the two cannot drift once real writers start populating the field.
func TestTaskAttempt_PhaseIsDenormalizedFromTask(t *testing.T) {
	all := []MethodTask{
		TaskSRS, TaskSRSReview, TaskSTP, TaskSTPReview,
		TaskSomeConstruction, TaskDetailedDesign, TaskDesignReview,
		TaskConstruction, TaskTestClient, TaskCodeReview,
		TaskIntegration, TaskTesting,
	}
	if len(all) != 12 {
		t.Fatalf("fixture lists %d tasks, want the 12 of Figure A-1", len(all))
	}
	for _, task := range all {
		// The stamp any writer must apply.
		a := TaskAttempt{
			AttemptID: AttemptID("C-x", task, 1),
			Task:      task,
			Phase:     PhaseForTask(task),
			Attempt:   1,
		}
		if a.Phase != PhaseForTask(a.Task) {
			t.Errorf("TaskAttempt{Task: %q}.Phase = %v, want PhaseForTask(%q) = %v", task, a.Phase, task, PhaseForTask(task))
			continue
		}
		if a.Phase == "" {
			t.Errorf("PhaseForTask(%q) = \"\" — every Figure A-1 task belongs to a phase", task)
			continue
		}
		// Cross-check the denormalized value against the independent lookup: the phase
		// stamped on the attempt must be the one that actually owns the task.
		if PhaseForTask(a.Task) != a.Phase {
			t.Errorf("attempt %q stamped Phase %v, but the task belongs to %v", a.AttemptID, a.Phase, PhaseForTask(a.Task))
		}
	}
}

func TestClassifyType_UnclassifiableReportsNotOK(t *testing.T) {
	// An unknown workerClass with coding=false matches no rule in ClassifyActivity.
	_, ok := ClassifyType("C-AA", "", false, false)
	if ok {
		t.Error("ClassifyType on an unclassifiable row reported ok=true; it must refuse to guess")
	}
}

func TestClassifyType_NoLenientDeploymentFallback(t *testing.T) {
	typ, ok := ClassifyType("C-AA", "", false, false)
	if ok && typ == ActivityTypeDeployment {
		t.Error("ClassifyType still falls back to Deployment for an unclassifiable row")
	}
}

func TestClassifyType_ServiceContractStillWins(t *testing.T) {
	typ, ok := ClassifyType("C-AA", "", false, true)
	if !ok || typ != ActivityTypeService {
		t.Errorf("ClassifyType(hasServiceContract=true) = (%v, %v), want (Service, true)", typ, ok)
	}
}

func TestClassifyType_ClassifiableRowsStillResolve(t *testing.T) {
	cases := []struct {
		id, workerClass string
		coding          bool
		want            ActivityType
	}{
		{"C-billing-manager", "junior-developer", true, ActivityTypeService},
		{"U-SPA-web-client", "junior-developer", true, ActivityTypeFrontend},
		{"N-UI-CONCEPT", "ui-designer", false, ActivityTypeUIDesign},
		// ui-designer + coding is Frontend by the workerClass rule itself (rule 2), not by
		// an id prefix. The neutral id matters: under a U-SPA id, a mutant that dropped
		// rule 2's coding arm would still reach Frontend through the prefix rule.
		{"N-UI-BUILD", "ui-designer", true, ActivityTypeFrontend},
		{"N-IT", "software-tester", false, ActivityTypeTesting},
		{"I-billing", "senior-developer", false, ActivityTypeIntegration},
	}
	for _, c := range cases {
		typ, ok := ClassifyType(c.id, c.workerClass, c.coding, false)
		if !ok {
			t.Errorf("%s: ClassifyType reported not-ok for a classifiable row", c.id)
			continue
		}
		if typ != c.want {
			t.Errorf("%s: ClassifyType = %v, want %v", c.id, typ, c.want)
		}
	}
}

// The three reserved design ids classify to their own types and carry the
// not-dispatchable sentinel with them. The workerClass/coding pair they are authored
// with (system-architect, coding=false) would otherwise type them as Documentation.
func TestClassifyActivity_DesignPrefixIsTypedButNotDispatchable(t *testing.T) {
	cases := []struct {
		id   string
		want ActivityType
	}{
		{"requirements", ActivityTypeRequirements},
		{"architecture", ActivityTypeArchitecture},
		{"projectDesign", ActivityTypeProjectDesign},
	}
	for _, c := range cases {
		typ, variant, err := ClassifyActivity(c.id, "system-architect", false)
		if typ != c.want || variant != TestVariantPlan {
			t.Errorf("%s -> (%s, %s), want (%s, plan)", c.id, typ, variant, c.want)
		}
		if !errors.Is(err, ErrDesignActivityNotDispatchable) {
			t.Errorf("%s: err = %v, want ErrDesignActivityNotDispatchable", c.id, err)
		}
		// The VIEW lens must still type it: a design row renders with its lifecycle.
		if got, ok := ClassifyType(c.id, "system-architect", false, false); !ok || got != c.want {
			t.Errorf("ClassifyType(%s) = (%s, %v), want (%s, true)", c.id, got, ok, c.want)
		}
	}
	// An ordinary activity is untouched and an unclassifiable one still fails the old way.
	if _, _, err := ClassifyActivity("C-billing-engine", "junior-developer", true); err != nil {
		t.Errorf("a coding activity must still classify cleanly: %v", err)
	}
	if _, ok := ClassifyType("N-WAT", "", false, false); ok {
		t.Error("an unclassifiable activity must still be refused, not swept in with design")
	}
}

// B3: the command a design lifecycle's work task runs is today's design command, and
// the projectDesign gate has none because it has no dispatch task at all.
func TestCommandFor_DesignLifecycles(t *testing.T) {
	cases := []struct {
		typ   ActivityType
		phase ActivityMethodPhase
		want  string
	}{
		{ActivityTypeRequirements, "mission", "mission-draft"},
		{ActivityTypeRequirements, "glossary", "glossary-draft"},
		{ActivityTypeRequirements, "volatilities", "volatilities-draft"},
		{ActivityTypeRequirements, "coreUseCases", "core-use-cases-draft"},
		{ActivityTypeArchitecture, "architecture", "system-draft"},
		{ActivityTypeProjectDesign, "sdp", ""},
	}
	for _, c := range cases {
		if got := CommandFor(c.typ, TestVariantPlan, c.phase); got != c.want {
			t.Errorf("CommandFor(%s, %s) = %q, want %q", c.typ, c.phase, got, c.want)
		}
	}
}

// constructionLedger is the attempt ledger cmd/backfill-attempts writes: one passed
// attempt per lifecycle node task (work, then gate), origin backfilled, with a basis.
func constructionLedger(activityID string, phases ...ActivityMethodPhase) []TaskAttempt {
	var out []TaskAttempt
	for _, ph := range phases {
		for _, task := range []MethodTask{AgentTaskFor(ph), GateTaskFor(ph)} {
			out = append(out, constructionAttempt(activityID, task, 1, OutcomePassed))
		}
	}
	return out
}

func constructionAttempt(activityID string, task MethodTask, n int, outcome TaskOutcome) TaskAttempt {
	return TaskAttempt{
		AttemptID:  AttemptID(activityID, task, n),
		Task:       task,
		Phase:      PhaseForTask(task),
		Attempt:    n,
		Outcome:    outcome,
		Provenance: AttemptProvenance{Origin: OriginBackfilled, Basis: "test fixture"},
	}
}

// TestEffectiveConstructionPhase_HeadFactsThenTheLedger pins the ONE derivation, case by
// case, now that nothing is stored for it to arbitrate against (stage-3 task 4; architect
// ruling Q2 is discharged with the stored half it protected). A recorded failure is the
// sticky terminal; an exit stamp is Done however far the ledger got; otherwise the ledger
// decides; a row the classifier refuses to type claims nothing from its ledger.
func TestEffectiveConstructionPhase_HeadFactsThenTheLedger(t *testing.T) {
	service := ActivityItem{Name: "C-x", WorkerClass: "junior-developer", Coding: true}
	all := ProfileFor(ActivityTypeService, TestVariantPlan).PhaseIDs()
	full := constructionLedger("C-x", all...)
	partial := constructionLedger("C-x", all[0])
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	rejected := append(
		constructionLedger("C-x", MethodPhaseRequirements, MethodPhaseTestPlan, MethodPhaseDetailedDesign),
		constructionAttempt("C-x", TaskConstruction, 1, OutcomePassed),
		constructionAttempt("C-x", TaskCodeReview, 1, OutcomeRejected),
	)

	cases := []struct {
		name      string
		row       ActivityExecution
		meta      ActivityItem
		wantState ActivityConstructionPhase
		wantBuild ActivityBuildStatus
	}{
		{"an opened row with no decided phase is Running",
			ActivityExecution{ActivityID: "C-x", StartedAt: &at},
			service, ActivityConstructionRunning, BuildInConstruction},
		{"an exit over an incomplete ledger is the Skipped/TakenOver shape",
			ActivityExecution{ActivityID: "C-x", StartedAt: &at, CompletedAt: &at, Attempts: partial},
			service, ActivityConstructionDone, BuildInReview},
		{"a recorded failure stays Failed under a passing ledger",
			ActivityExecution{ActivityID: "C-x", FailureReason: PipelineFailed, CompletedAt: &at, Attempts: full},
			service, ActivityConstructionFailed, BuildFailed},
		{"a ledger-only row is decided by its ledger",
			ActivityExecution{ActivityID: "C-x", Attempts: full},
			service, ActivityConstructionDone, BuildIntegrated},
		{"a rejected latest gate reads Running",
			ActivityExecution{ActivityID: "C-x", Attempts: rejected},
			service, ActivityConstructionRunning, BuildInConstruction},
		{"an unclassified row claims nothing from its ledger",
			ActivityExecution{ActivityID: "C-x", Attempts: full},
			ActivityItem{Name: "C-x"}, ActivityConstructionNotStarted, BuildInConstruction},
		{"an empty row has not started",
			ActivityExecution{ActivityID: "C-x"},
			service, ActivityConstructionNotStarted, BuildInConstruction},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotState, gotBuild := EffectiveConstructionPhase(c.row, c.meta)
			if gotState != c.wantState || gotBuild != c.wantBuild {
				t.Errorf("EffectiveConstructionPhase = (%v, %v), want (%v, %v)", gotState, gotBuild, c.wantState, c.wantBuild)
			}
		})
	}
}

// TestPumpWroteRow_IsTheStartStamp pins architect (D), D.1.1 in its post-rename form: an
// opened row is one carrying a start stamp, and the row OpenActivity /
// RecordActivityStarted write satisfies it, so a row the pump has begun can never read as
// ledger-only.
func TestPumpWroteRow_IsTheStartStamp(t *testing.T) {
	ledger := constructionLedger("C-x", MethodPhaseRequirements)
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		row  ActivityExecution
		want bool
	}{
		{"an empty row", ActivityExecution{ActivityID: "C-x"}, false},
		{"a ledger-only row", ActivityExecution{ActivityID: "C-x", Attempts: ledger}, false},
		{"a failure detail alone is not the clause", ActivityExecution{ActivityID: "C-x", FailureDetail: "x"}, false},
		{"an opened row", ActivityExecution{ActivityID: "C-x", StartedAt: &at}, true},
		{"an opened row that exited", ActivityExecution{ActivityID: "C-x", StartedAt: &at, CompletedAt: &at}, true},
		{"an opened row with a ledger", ActivityExecution{ActivityID: "C-x", StartedAt: &at, Attempts: ledger}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PumpWroteRow(c.row); got != c.want {
				t.Errorf("PumpWroteRow = %v, want %v", got, c.want)
			}
		})
	}

	store, id, v, cred := newConstructionStore(t)
	if _, err := store.RecordActivityStarted(fwra.Context{Context: context.Background()}, id, v, "X001", ActivityTypeService, TestVariantPlan, cred, fwra.IdempotencyKey("wf:pump-wrote")); err != nil {
		t.Fatalf("RecordActivityStarted: %v", err)
	}
	if s := readConstruction(t, store, id, cred, "X001"); !PumpWroteRow(s) {
		t.Errorf("the row RecordActivityStarted writes (%+v) must satisfy PumpWroteRow", s)
	}
}

// TestResolveDependencySatisfied_TheMovedPumpRule pins the dependency rule the pump and
// the construction view now share (architect (D), D.3) directly, beside the pump tests
// that exercise it end to end: an activity is satisfied iff its effective phase is Done;
// a milestone iff its own dependencies are, recursively; a dangling id and a milestone
// cycle are plan defects with their own FailureReason; AllDepsSatisfied stops at the
// first unsatisfied or defective id in authored order.
func TestResolveDependencySatisfied_TheMovedPumpRule(t *testing.T) {
	svc := func(name string) ActivityItem {
		return ActivityItem{Name: name, WorkerClass: "junior-developer", Coding: true}
	}
	all := ProfileFor(ActivityTypeService, TestVariantPlan).PhaseIDs()
	items := map[string]ActivityItem{"A-done": svc("A-done"), "A-partial": svc("A-partial"), "A-norow": svc("A-norow"), "A-stored": svc("A-stored")}
	status := map[string]ActivityExecution{
		"A-done":    {ActivityID: "A-done", Attempts: constructionLedger("A-done", all...)},
		"A-partial": {ActivityID: "A-partial", Attempts: constructionLedger("A-partial", all[:len(all)-1]...)},
		"A-stored":  {ActivityID: "A-stored", CompletedAt: &envelopeStartedAt},
	}
	milestones := MilestonesByID(&Network{Milestones: []NetworkMilestone{
		{ID: "M-start"},
		{ID: "M-met", DependsOn: []string{"A-done", "M-start"}},
		{ID: "M-unmet", DependsOn: []string{"A-done", "A-partial"}},
		{ID: "M-loop-a", DependsOn: []string{"M-loop-b"}},
		{ID: "M-loop-b", DependsOn: []string{"M-loop-a"}},
	}})
	cases := []struct {
		dep  string
		want DependencyResolution
	}{
		{"A-done", DependencyResolution{Satisfied: true}},
		{"A-stored", DependencyResolution{Satisfied: true}},
		{"A-partial", DependencyResolution{}},
		{"A-norow", DependencyResolution{}},
		{"M-start", DependencyResolution{Satisfied: true}},
		{"M-met", DependencyResolution{Satisfied: true}},
		{"M-unmet", DependencyResolution{}},
		{"M-loop-a", DependencyResolution{ProblemKind: DependencyCycle, ProblemReason: `milestone dependency cycle detected: "M-loop-a" depends (directly or transitively) on itself`}},
		{"ghost", DependencyResolution{ProblemKind: DependencyUnresolved, ProblemReason: `dependency id "ghost" is neither an authored activity (activityList) nor an authored milestone (network.milestones)`}},
	}
	for _, c := range cases {
		t.Run(c.dep, func(t *testing.T) {
			if got := ResolveDependencySatisfied(c.dep, items, status, milestones, map[string]bool{}); got != c.want {
				t.Errorf("ResolveDependencySatisfied(%q) = %+v, want %+v", c.dep, got, c.want)
			}
		})
	}
	if got := AllDepsSatisfied([]string{"A-done", "M-met"}, items, status, milestones); got != (DependencyResolution{Satisfied: true}) {
		t.Errorf("AllDepsSatisfied(all met) = %+v, want satisfied", got)
	}
	if got := AllDepsSatisfied([]string{"A-done", "A-partial", "ghost"}, items, status, milestones); got != (DependencyResolution{}) {
		t.Errorf("AllDepsSatisfied stops at the first unsatisfied id, before a later defect: got %+v", got)
	}
	if got := AllDepsSatisfied([]string{"ghost", "A-partial"}, items, status, milestones); got.ProblemKind != DependencyUnresolved {
		t.Errorf("AllDepsSatisfied reports a defect met first: got %+v", got)
	}
	if got := AllDepsSatisfied(nil, items, status, milestones); !got.Satisfied {
		t.Errorf("no dependencies is satisfied: got %+v", got)
	}
}

func TestGateTaskFor_IsTheBinaryExitCriterion(t *testing.T) {
	cases := map[ActivityMethodPhase]MethodTask{
		MethodPhaseRequirements:   TaskSRSReview,
		MethodPhaseTestPlan:       TaskSTPReview,
		MethodPhaseDetailedDesign: TaskDesignReview,
		MethodPhaseConstruction:   TaskCodeReview,
		MethodPhaseIntegration:    TaskTesting,
	}
	for phase, want := range cases {
		if got := GateTaskFor(phase); got != want {
			t.Errorf("GateTaskFor(%v) = %q, want %q", phase, got, want)
		}
		if !isGateTask(want) {
			t.Errorf("isGateTask(%q) = false, want true", want)
		}
	}
}

// AgentTaskFor is the OTHER half of the Figure A-1 alternation: the AI-work task an
// agent dispatch's episode is burned on, as opposed to the gate task that REVIEWS that
// work. Each canonical phase has exactly one — pinned here both by value and by
// uniqueness, so a future task added to a phase cannot silently make the selection
// ambiguous.
func TestAgentTaskFor_IsTheSingleAIWorkTaskPerPhase(t *testing.T) {
	cases := map[ActivityMethodPhase]MethodTask{
		MethodPhaseRequirements:   TaskSRS,
		MethodPhaseTestPlan:       TaskSTP,
		MethodPhaseDetailedDesign: TaskDetailedDesign,
		MethodPhaseConstruction:   TaskConstruction,
		MethodPhaseIntegration:    TaskIntegration,
	}
	for phase, want := range cases {
		if got := AgentTaskFor(phase); got != want {
			t.Errorf("AgentTaskFor(%v) = %q, want %q", phase, got, want)
		}
		if isGateTask(want) {
			t.Errorf("AgentTaskFor(%v) = %q, which is a GATE task — the review, not the work", phase, want)
		}
		if got := GateTaskFor(phase); got == want {
			t.Errorf("phase %v names %q as both its work and its gate", phase, want)
		}
	}
	if got := AgentTaskFor(ActivityMethodPhase("not-a-phase")); got != "" {
		t.Errorf("AgentTaskFor(unknown) = %q, want \"\" — no attribution is possible", got)
	}
}

func TestPhaseForTask_RoundTrips(t *testing.T) {
	for _, p := range []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseTestPlan, MethodPhaseDetailedDesign,
		MethodPhaseConstruction, MethodPhaseIntegration,
	} {
		for _, task := range []MethodTask{AgentTaskFor(p), GateTaskFor(p)} {
			if got := PhaseForTask(task); got != p {
				t.Errorf("PhaseForTask(%q) = %v, want %v", task, got, p)
			}
		}
	}
	if got := PhaseForTask(TaskSomeConstruction); got != MethodPhaseDetailedDesign {
		t.Errorf("PhaseForTask(someConstruction) = %v, want detailed_design", got)
	}
	if got := PhaseForTask(TaskTestClient); got != MethodPhaseConstruction {
		t.Errorf("PhaseForTask(testClient) = %v, want construction", got)
	}
}

// The per-type task SET, not merely its size — TasksForProfile's node-only counts
// (service 10 · frontend 10 · deployment 6 · documentation 6 · uiDesign 4 · integration
// 2, each two short of Spec R1's full Figure A-1 count because the two sub-attempt
// tasks are not lifecycle nodes) turn on exactly these rows, and a length assertion
// passes for any set of the right size at all — including one drawn from the wrong
// phases.
func TestTasksForProfile_PerTypeTaskSets(t *testing.T) {
	cases := []struct {
		name string
		typ  ActivityType
		want []MethodTask
	}{
		{"service", ActivityTypeService, []MethodTask{
			TaskSRS, TaskSRSReview,
			TaskDetailedDesign, TaskDesignReview,
			TaskSTP, TaskSTPReview,
			TaskConstruction, TaskCodeReview,
			TaskIntegration, TaskTesting,
		}},
		{"frontend", ActivityTypeFrontend, []MethodTask{
			TaskSRS, TaskSRSReview,
			TaskDetailedDesign, TaskDesignReview,
			TaskSTP, TaskSTPReview,
			TaskConstruction, TaskCodeReview,
			TaskIntegration, TaskTesting,
		}},
		// No requirements and no test-plan phase: 2 + 2 + 2 = 6, NOT 9. TasksForProfile
		// emits lifecycle NODES only — the sub-attempt tasks someConstruction/testClient
		// are not among them (see conditionalTasks).
		{"deployment", ActivityTypeDeployment, []MethodTask{
			TaskDetailedDesign, TaskDesignReview,
			TaskConstruction, TaskCodeReview,
			TaskIntegration, TaskTesting,
		}},
		{"documentation", ActivityTypeDocumentation, []MethodTask{
			TaskDetailedDesign, TaskDesignReview,
			TaskConstruction, TaskCodeReview,
			TaskIntegration, TaskTesting,
		}},
		{"uiDesign", ActivityTypeUIDesign, []MethodTask{
			TaskSRS, TaskSRSReview,
			TaskDetailedDesign, TaskDesignReview,
		}},
		{"integration", ActivityTypeIntegration, []MethodTask{
			TaskIntegration, TaskTesting,
		}},
	}
	for _, c := range cases {
		got := TasksForProfile(ProfileFor(c.typ, TestVariantPlan))
		if len(got) != len(c.want) {
			t.Errorf("%s: TasksForProfile len = %d, want %d (got %v)", c.name, len(got), len(c.want), got)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%s: TasksForProfile[%d] = %q, want %q (full set %v)", c.name, i, got[i], c.want[i], got)
			}
		}
	}
}

// allProfiles is every (type, variant) cell that resolves to a DISTINCT profile.
var allProfiles = []struct {
	name    string
	typ     ActivityType
	variant TestingVariant
}{
	{"service", ActivityTypeService, TestVariantPlan},
	{"frontend", ActivityTypeFrontend, TestVariantPlan},
	{"uiDesign", ActivityTypeUIDesign, TestVariantPlan},
	{"deployment", ActivityTypeDeployment, TestVariantPlan},
	{"documentation", ActivityTypeDocumentation, TestVariantPlan},
	{"integration", ActivityTypeIntegration, TestVariantPlan},
	{"testing-plan", ActivityTypeTesting, TestVariantPlan},
	{"testing-harness", ActivityTypeTesting, TestVariantHarness},
	{"testing-perf", ActivityTypeTesting, TestVariantPerf},
	{"testing-systemtest", ActivityTypeTesting, TestVariantSystemTest},
	{"testing-qa", ActivityTypeTesting, TestVariantQAProcess},
}

// Every phase a profile carries is whole: a label, a weight and a work/gate pair that
// are two different tasks. The per-profile WORDS (a test plan's construction gate is
// "Scenario Review", not "Code Review") left this package with TaskLabelFor; they are
// the platform data's business now, held by method-assets' own lifecycles_test.go and
// by the SPA's lifecycleProfiles.test.ts.
func TestProfileWords_TotalOverExactlyTheProfilesPhases(t *testing.T) {
	for _, pr := range allProfiles {
		sum := 0
		for _, ph := range ProfileFor(pr.typ, pr.variant).Phases {
			sum += ph.Weight
			work, gate := AgentTaskFor(ph.Phase), GateTaskFor(ph.Phase)
			if ph.Label == "" || work == "" || gate == "" {
				t.Errorf("%s: phase %q is incomplete: label=%q work=%q gate=%q", pr.name, ph.Phase, ph.Label, work, gate)
			}
			if work == gate {
				t.Errorf("%s: phase %q names its work and its gate the same (%q)", pr.name, ph.Phase, work)
			}
		}
		if sum != 100 {
			t.Errorf("%s: weights sum to %d, want 100", pr.name, sum)
		}
	}
}

func TestAttemptID_Format(t *testing.T) {
	got := AttemptID("C-billing-manager", TaskDesignReview, 2)
	want := "C-billing-manager:designReview:2"
	if got != want {
		t.Errorf("AttemptID = %q, want %q", got, want)
	}
}

// The whole design: a dropped or absent stamp must fail SUSPICIOUS, never blessed.
func TestRecordOrigin_ZeroValueIsSynthesized(t *testing.T) {
	var zero RecordOrigin
	if zero != OriginSynthesized {
		t.Fatalf("zero RecordOrigin = %q, want %q — a missing stamp must never read as observed", zero, OriginSynthesized)
	}
}

func TestAttemptProvenance_ZeroStructIsSynthesized(t *testing.T) {
	var p AttemptProvenance
	if p.Origin != OriginSynthesized {
		t.Errorf("zero AttemptProvenance.Origin = %q, want %q", p.Origin, OriginSynthesized)
	}
}

func TestAttemptProvenance_DecodingAbsentOriginIsSynthesized(t *testing.T) {
	var p AttemptProvenance
	if err := json.Unmarshal([]byte(`{"generator":"x"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Origin != OriginSynthesized {
		t.Errorf("absent origin decoded to %q, want %q", p.Origin, OriginSynthesized)
	}
}

func TestAttemptProvenance_EncodingEmitsOriginKey(t *testing.T) {
	// Finding 1: No omitempty on Origin means zero value MUST be emitted on the wire.
	// If someone added omitempty, synthesized origins would be silently dropped.
	p := AttemptProvenance{} // zero value: Origin = OriginSynthesized
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The "origin" key must be present in the JSON, even though its value is empty string.
	if !bytes.Contains(data, []byte(`"origin":`)) {
		t.Errorf("marshaled JSON = %s, want to contain \"origin\" key", data)
	}
}

func TestAttemptProvenance_ValidateRejectsUnknownOrigin(t *testing.T) {
	p := AttemptProvenance{Origin: RecordOrigin("observed-ish")}
	if err := p.Validate(); err == nil {
		t.Error("Validate() accepted an unknown origin, want error")
	}
}

func TestAttemptProvenance_ValidateAcceptsKnownOrigins(t *testing.T) {
	// Finding 3: Positive cases for Validate().
	cases := []struct {
		name string
		p    AttemptProvenance
	}{
		{"zero value (synthesized)", AttemptProvenance{}},
		{"observed", AttemptProvenance{Origin: OriginObserved}},
		{"backfilled with basis", AttemptProvenance{Origin: OriginBackfilled, Basis: "serviceContracts[artifactAccess]"}},
	}
	for _, c := range cases {
		if err := c.p.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v, want nil", c.name, err)
		}
	}
}

func TestAttemptProvenance_ValidateRequiresBasisForBackfilled(t *testing.T) {
	p := AttemptProvenance{Origin: OriginBackfilled}
	if err := p.Validate(); err == nil {
		t.Error("Validate() accepted backfilled with no basis, want error")
	}
	p.Basis = "serviceContracts[artifactAccess]"
	if err := p.Validate(); err != nil {
		t.Errorf("Validate() rejected backfilled with a basis: %v", err)
	}
}

// Contagion: a value derived from any synthesized input is itself synthesized.
func TestWorstOrigin_Contagion(t *testing.T) {
	cases := []struct {
		name string
		in   []RecordOrigin
		want RecordOrigin
	}{
		{"all observed", []RecordOrigin{OriginObserved, OriginObserved}, OriginObserved},
		{"one backfilled", []RecordOrigin{OriginObserved, OriginBackfilled}, OriginBackfilled},
		{"one synthesized wins", []RecordOrigin{OriginObserved, OriginBackfilled, OriginSynthesized}, OriginSynthesized},
		{"empty is observed", nil, OriginObserved},
	}
	for _, c := range cases {
		if got := worstOrigin(c.in...); got != c.want {
			t.Errorf("%s: worstOrigin(%v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestWorstOrigin_UnknownOriginRanksAsSynthesized(t *testing.T) {
	// Finding 2: Unknown origins (those not in the closed enum) must rank as badly as synthesized.
	// If someone changed originRank's default branch to rank unknown as trustworthy,
	// this test would catch it.
	got := worstOrigin(OriginObserved, RecordOrigin("who-knows"))
	// The unknown origin becomes the worst (has rank 0, same as synthesized), so it's returned.
	want := RecordOrigin("who-knows")
	if got != want {
		t.Errorf("worstOrigin(OriginObserved, \"who-knows\") = %q, want %q", got, want)
	}
	// Verify that the unknown origin is suspicious (fails validation) — the critical property.
	p := AttemptProvenance{Origin: got}
	if err := p.Validate(); err == nil {
		t.Error("unknown origin should fail Validate(), proving it's treated as dangerous")
	}
}

// The Integrated-at-85% bug: Integration done while Requirements never completed must
// NOT read as Integrated.
func TestCoarseBuildStatus_RequiresAllPhasesForIntegrated(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
	for i := range phases {
		// Everything except Requirements.
		if phases[i].Phase != MethodPhaseRequirements {
			phases[i].Completed = true
		}
	}
	if got := CoarseBuildStatus(phases); got == BuildIntegrated {
		t.Error("CoarseBuildStatus reported Integrated with Requirements incomplete")
	}
}

func TestCoarseBuildStatus_AllPhasesCompleteIsIntegrated(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
	for i := range phases {
		phases[i].Completed = true
	}
	if got := CoarseBuildStatus(phases); got != BuildIntegrated {
		t.Errorf("CoarseBuildStatus(all done) = %v, want Integrated", got)
	}
}

// A uiDesign profile has no Integration phase at all; completing its two phases must
// still read as Integrated rather than being stuck in construction forever.
// The len(phases)==0 branch. BuildInConstruction is a NAMED, plausible value derived
// from no evidence at all, which is why callers must never reach here with an empty
// slice for a row they intend to render a chip for — the read path materializes the
// profile skeleton first (ResolvePhaseCompletions) or suppresses the whole claim
// (Classified=false, or HasBuildEvidence=false for a row with no evidence). Pinned so
// the branch's meaning is stated, not stumbled on.
func TestCoarseBuildStatus_EmptyPhaseSetIsAnEvidenceLessDefault(t *testing.T) {
	for _, phases := range [][]PhaseCompletion{nil, {}} {
		if got := CoarseBuildStatus(phases); got != BuildInConstruction {
			t.Errorf("CoarseBuildStatus(%v) = %v, want BuildInConstruction", phases, got)
		}
	}
	// Same input, same evidence: the coarse PHASE deriver answers NotStarted. The two
	// together are the "Not started / In construction" pair a row with no phase set
	// used to render — true of the input, false of the activity.
	if got := CoarsePhase(nil); got != ActivityConstructionNotStarted {
		t.Errorf("CoarsePhase(nil) = %v, want NotStarted", got)
	}
}

func TestCoarseBuildStatus_ProfileWithoutIntegrationCanIntegrate(t *testing.T) {
	phases := ProfileFor(ActivityTypeUIDesign, 0).toPhaseCompletions()
	for i := range phases {
		phases[i].Completed = true
	}
	if got := CoarseBuildStatus(phases); got != BuildIntegrated {
		t.Errorf("uiDesign all-phases-done = %v, want Integrated", got)
	}
}

func TestCoarseBuildStatusFor_ARecordedFailureIsStillSticky(t *testing.T) {
	phases := ProfileFor(ActivityTypeService, 0).toPhaseCompletions()
	for i := range phases {
		phases[i].Completed = true
	}
	row := ActivityExecution{ActivityID: "C-x", FailureReason: PipelineFailed}
	if got := CoarseBuildStatusFor(row, phases); got != BuildFailed {
		t.Errorf("CoarseBuildStatusFor(failed row) = %v, want Failed (sticky)", got)
	}
}

// An activity is drawn in its OWN component's layer. The derivation gives every
// component-bearing activity the component it builds (C-<id>, R-<id>, U-SPA-<clientId>),
// so the client app lands on the Client row and a vendor resource on the Resource row by
// the same ordinary lookup as a coding activity — no id prefix is consulted.
func TestLayerForActivity_TakesItsComponentsLayer(t *testing.T) {
	for _, want := range []string{"client", "manager", "engine", "resourceAccess", "resource"} {
		layer, band := LayerForActivity(want)
		if layer != want || band != "layered" {
			t.Errorf("LayerForActivity(%q) = (%q, %q), want (%q, layered)", want, layer, band, want)
		}
	}
}

func TestLayerForActivity_ComponentlessActivitiesAreProjectWide(t *testing.T) {
	layer, band := LayerForActivity("")
	if band != "projectWide" {
		t.Errorf("band = %q, want projectWide", band)
	}
	if layer != "" {
		t.Errorf("layer = %q, want empty — no fake layer for a cross-cutting activity", layer)
	}
}

// A committed activity list with ZERO activities is not a completed construction: without
// the guard, the loop over an empty list falls straight through to true. The fixture
// harness above commits no list at all when a case has `activities: []`, so only its !ok
// branch runs there; this pins the empty-list guard itself (Task 7a review minor 1).
func TestIsConstructionComplete_EmptyCommittedListIsNotComplete(t *testing.T) {
	p := Project{
		Phase:        PhaseConstruction,
		ActivityList: ArtifactSlot{Status: ReviewCommitted, Model: &ActivityList{Activities: []ActivityItem{}}},
		ActivityExecution: map[string]ActivityExecution{
			"C-a": {ActivityID: "C-a", CompletedAt: &envelopeStartedAt,
				Attempts: constructionLedger("C-a", ProfileFor(ActivityTypeService, TestVariantPlan).PhaseIDs()...)},
		},
	}
	if isConstructionComplete(p) {
		t.Fatal("isConstructionComplete = true over an empty committed activity list, want false")
	}
}

// ResolveConstructionRow classifies by the committed item's Name first and falls back to
// the row's own ActivityID only when the activity is not in the committed list.
func TestResolveConstructionRow_ClassifiesByTheCommittedNameFirst(t *testing.T) {
	cases := []struct {
		name        string
		row         ActivityExecution
		meta        ActivityItem
		wantType    ActivityType
		wantVariant TestingVariant
	}{
		{"the name decides the type", ActivityExecution{ActivityID: "C-other"}, ActivityItem{Name: "U-SPA-web-client", Coding: true}, ActivityTypeFrontend, TestVariantPlan},
		{"the name decides the variant", ActivityExecution{ActivityID: "N-STP"}, ActivityItem{Name: "N-IT", WorkerClass: "software-tester"}, ActivityTypeTesting, TestVariantSystemTest},
		{"the row id when the list does not hold it", ActivityExecution{ActivityID: "U-SPA-web-client"}, ActivityItem{}, ActivityTypeFrontend, TestVariantPlan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typ, variant, _, classified := ResolveConstructionRow(tc.row, tc.meta)
			if !classified || typ != tc.wantType || variant != tc.wantVariant {
				t.Errorf("ResolveConstructionRow = (%v, %v, classified=%v), want (%v, %v, true)", typ, variant, classified, tc.wantType, tc.wantVariant)
			}
		})
	}
}

// With several mismatched rows, the error names the FIRST key in sorted order, so
// the same bad document always reports the same row (fix-E review minor). Map
// iteration order is random per range, so an unsorted walk fails this within a
// few of the 64 decodes.
func TestDecodeProjectJSON_ActivityConstructionKeyMismatch_NamesTheFirstSortedKey(t *testing.T) {
	id := ProjectID("11111111-1111-1111-1111-111111111111")
	raw, err := EncodeProjectJSON(Project{ID: id, ActivityExecution: map[string]ActivityExecution{
		"C-z": {ActivityID: "X-z"},
		"C-m": {ActivityID: "C-m"},
		"C-a": {ActivityID: "X-a"},
	}})
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	for i := range 64 {
		_, _, err := DecodeProjectJSON(raw, id)
		if err == nil {
			t.Fatal("two mismatched rows must FAIL decode")
		}
		if !strings.Contains(err.Error(), `activityExecution["C-a"]`) || strings.Contains(err.Error(), "C-z") {
			t.Fatalf("decode %d: the error must name the first sorted key (C-a) and only it; got: %v", i, err)
		}
	}
}

// fixedCatalog yields exactly the refs it is given, so a test can list a repo with
// no parseable id, a repo with no project.json, and a repo with malformed state.
type fixedCatalog struct{ refs []ProjectCatalogRef }

func (c fixedCatalog) ListProjectRepos(context.Context, OwnerScope, RepoCredential) ([]ProjectCatalogRef, error) {
	return c.refs, nil
}

// captureSlog routes the default logger into a buffer for the test's duration.
// The package's tests do not run in parallel, so swapping the default is safe.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// commitRawProjectJSON commits raw bytes as the repo's project.json, bypassing the
// store's writers: the only way malformed committed state gets there is by hand.
func commitRawProjectJSON(t *testing.T, repo gh.LocalGitRepo, raw []byte) {
	t.Helper()
	work := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("clone", repo.Dir, ".")
	git("config", "user.email", "test@aiarch.local")
	git("config", "user.name", "test")
	path := filepath.Join(work, statePathPrefix, projectFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "hand-written project.json")
	git("push", "origin", "HEAD")
}

// TestGitStore_ListProjects_SaysWhatItSkips (fix-E review): list-projects must never
// drop or thin a project silently. A repo with no parseable id is skipped and the
// skip is logged; a repo whose project.json is not there yet is listed without its
// head-state, and that is logged with its id and the reason.
func TestGitStore_ListProjects_SaysWhatItSkips(t *testing.T) {
	readable, ghost := ProjectID(uuid.NewString()), ProjectID("ghost-"+uuid.NewString())
	repos := map[ProjectID]*fwgithub.GitStore{}
	for _, id := range []ProjectID{readable, ghost} {
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
	cred, ctx := LocalRepoCredential(), context.Background()
	if _, err := store.CreateProject(ctx, readable, "alice", "Real", cred, "wf:real"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	store = store.WithCatalog(fixedCatalog{refs: []ProjectCatalogRef{
		{ProjectID: readable, Title: "Real"},
		{ProjectID: ghost, Title: "Ghost"},
		{ProjectID: "", Title: "Nameless"},
	}})
	logs := captureSlog(t)

	summaries, err := store.ListProjects(ctx, "alice", cred)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("ListProjects = %d rows, want 2 (the real one and the ghost): %+v", len(summaries), summaries)
	}
	out := logs.String()
	for _, want := range []string{
		"skipping a catalog repo with no project id", "title=Nameless",
		"listing a project without its head-state", "projectID=" + string(ghost), "no state for project",
		// An expected state, re-listed on every landing visit: Debug, not Info (fix-F review).
		`level=DEBUG msg="projectstate.ListProjects: listing a project without its head-state"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log must say %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "projectID="+string(readable)) {
		t.Errorf("a project that read cleanly must not be logged; got:\n%s", out)
	}
}

// TestGitStore_ListProjects_SkipsAnUnreadableProject (fix-F review ruling): one
// project whose committed state will not decode is SKIPPED with a warning, and the
// owner's other projects are still listed. It used to fail the whole list, which
// blanked the landing grid for that owner. The log names WHICH project and why: the
// decode error alone does not carry the project id.
func TestGitStore_ListProjects_SkipsAnUnreadableProject(t *testing.T) {
	bad, good := ProjectID("bad-"+uuid.NewString()), ProjectID(uuid.NewString())
	badRepo, goodRepo := gh.StartLocalGitRepo(t, "main"), gh.StartLocalGitRepo(t, "main")
	raw, err := EncodeProjectJSON(Project{ID: bad, Name: "Bad", ActivityExecution: map[string]ActivityExecution{
		"C-a": {ActivityID: "C-b"}, // stored under another activity's key
	}})
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	commitRawProjectJSON(t, badRepo, raw)
	repos := map[ProjectID]*fwgithub.GitStore{}
	for id, r := range map[ProjectID]gh.LocalGitRepo{bad: badRepo, good: goodRepo} {
		gs, err := fwgithub.NewGitStore(r.URL, "main")
		if err != nil {
			t.Fatalf("NewGitStore(%s): %v", id, err)
		}
		repos[id] = gs
	}
	store, err := NewGitStore(multiRepoLocator{repos: repos}, true)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	cred, ctx := LocalRepoCredential(), context.Background()
	if _, err := store.CreateProject(ctx, good, "alice", "Good", cred, "wf:good"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	store = store.WithCatalog(fixedCatalog{refs: []ProjectCatalogRef{
		{ProjectID: bad, Title: "Bad"},
		{ProjectID: good, Title: "Good"},
	}})
	logs := captureSlog(t)

	summaries, err := store.ListProjects(ctx, "alice", cred)
	if err != nil {
		t.Fatalf("one unreadable project must not fail the list: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ProjectID != good {
		t.Fatalf("ListProjects = %+v, want exactly the readable project %s", summaries, good)
	}
	out := logs.String()
	for _, want := range []string{
		`level=WARN msg="projectstate.ListProjects: skipping a project that could not be read"`,
		"projectID=" + string(bad),
		`activityExecution[\"C-a\"]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log must say %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "projectID="+string(good)) {
		t.Errorf("a project that read cleanly must not be logged; got:\n%s", out)
	}
}

// refusingRemote serves a git-HTTP remote that refuses every credential with a 401,
// the way GitHub answers an expired or revoked installation token.
func refusingRemote(t *testing.T) *fwgithub.GitStore {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="GitHub"`)
		http.Error(w, "Bad credentials", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	gs, err := fwgithub.NewGitStore(srv.URL+"/owner/repo.git", "main")
	if err != nil {
		t.Fatalf("NewGitStore(refusing): %v", err)
	}
	return gs
}

// TestGitStore_ListProjects_FailsWhenEveryProjectRefusesTheCredential (fix-G review
// ruling): the listing reads every project with ONE credential. When every project
// refuses it, the credential is at fault, not a project, and the call fails with an
// Auth error. Skipping each with a warning would blank the landing grid with no
// error at all.
func TestGitStore_ListProjects_FailsWhenEveryProjectRefusesTheCredential(t *testing.T) {
	a, b := ProjectID(uuid.NewString()), ProjectID(uuid.NewString())
	repos := map[ProjectID]*fwgithub.GitStore{a: refusingRemote(t), b: refusingRemote(t)}
	store, err := NewGitStore(multiRepoLocator{repos: repos}, false)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(fixedCatalog{refs: []ProjectCatalogRef{
		{ProjectID: a, Title: "A"},
		{ProjectID: b, Title: "B"},
	}})

	summaries, err := store.ListProjects(context.Background(), "alice", RepoCredential{Bytes: []byte("expired-token")})
	if err == nil {
		t.Fatalf("a credential every project refuses must fail the list; got %+v", summaries)
	}
	var e *fwra.Error
	if !errors.As(err, &e) || e.Kind != fwra.Auth {
		t.Fatalf("want a fwra Auth error, got %v", err)
	}
	if !strings.Contains(err.Error(), "refused for all 2") {
		t.Errorf("the error must say the credential was refused for every project; got %v", err)
	}
	if summaries != nil {
		t.Errorf("a failed list returns no rows; got %+v", summaries)
	}
}

// TestGitStore_ListProjects_FailsWhenTheOnlyProjectRefusesTheCredential (fix I, the
// fix-H review's mutant A): the all-refused rule holds for a ONE-project catalog too.
// That is current behaviour, and in local mode one project IS the whole list: a
// refusal there must fail the call with Auth, never come back as an empty landing
// grid with no error. The two-project case above could not tell "every project
// refused" from "more than one project refused".
func TestGitStore_ListProjects_FailsWhenTheOnlyProjectRefusesTheCredential(t *testing.T) {
	only := ProjectID(uuid.NewString())
	repos := map[ProjectID]*fwgithub.GitStore{only: refusingRemote(t)}
	store, err := NewGitStore(multiRepoLocator{repos: repos}, false)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(fixedCatalog{refs: []ProjectCatalogRef{{ProjectID: only, Title: "Only"}}})

	summaries, err := store.ListProjects(context.Background(), "alice", RepoCredential{Bytes: []byte("expired-token")})
	if err == nil {
		t.Fatalf("the only project refusing the credential must fail the list; got %+v", summaries)
	}
	var e *fwra.Error
	if !errors.As(err, &e) || e.Kind != fwra.Auth {
		t.Fatalf("want a fwra Auth error, got %v", err)
	}
	if !strings.Contains(err.Error(), "refused for all 1") {
		t.Errorf("the error must say the credential was refused for its one project; got %v", err)
	}
	if summaries != nil {
		t.Errorf("a failed list returns no rows; got %+v", summaries)
	}
}

// TestGitStore_ListProjects_SkipsAProjectThatRefusesTheCredential_BesideOnesItReads:
// an auth refusal on SOME projects, while the same credential reads the others, is
// those projects' fault (a repo outside the grant, say). Each is skipped with a
// warning and the rest are listed, exactly like any other per-project read fault.
func TestGitStore_ListProjects_SkipsAProjectThatRefusesTheCredential_BesideOnesItReads(t *testing.T) {
	refusing, good := ProjectID("refusing-"+uuid.NewString()), ProjectID(uuid.NewString())
	goodRepo := gh.StartLocalGitRepo(t, "main")
	goodStore, err := fwgithub.NewGitStore(goodRepo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore(good): %v", err)
	}
	repos := map[ProjectID]*fwgithub.GitStore{refusing: refusingRemote(t), good: goodStore}
	ctx := context.Background()
	// The readable project is created through a LOCAL store over the same repo.
	local, err := NewGitStore(multiRepoLocator{repos: repos}, true)
	if err != nil {
		t.Fatalf("NewGitStore(local): %v", err)
	}
	if _, err := local.CreateProject(ctx, good, "alice", "Good", LocalRepoCredential(), "wf:good"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	store, err := NewGitStore(multiRepoLocator{repos: repos}, false)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(fixedCatalog{refs: []ProjectCatalogRef{
		{ProjectID: refusing, Title: "Refusing"},
		{ProjectID: good, Title: "Good"},
	}})
	logs := captureSlog(t)

	summaries, err := store.ListProjects(ctx, "alice", RepoCredential{Bytes: []byte("installation-token")})
	if err != nil {
		t.Fatalf("one project refusing the credential must not fail the list: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ProjectID != good {
		t.Fatalf("ListProjects = %+v, want exactly the readable project %s", summaries, good)
	}
	out := logs.String()
	for _, want := range []string{
		`level=WARN msg="projectstate.ListProjects: skipping a project that could not be read"`,
		"projectID=" + string(refusing),
		"auth failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log must say %q; got:\n%s", want, out)
		}
	}
}

// TestGitStore_ListProjects_FailsOnACredentialThatCannotAuthenticate: a cloud listing
// handed an empty credential cannot read any project, so it fails the call before
// reading one. It used to be refused per project, and every project was skipped: an
// empty grid and no error.
func TestGitStore_ListProjects_FailsOnACredentialThatCannotAuthenticate(t *testing.T) {
	id := ProjectID(uuid.NewString())
	repo := gh.StartLocalGitRepo(t, "main")
	gs, err := fwgithub.NewGitStore(repo.URL, "main")
	if err != nil {
		t.Fatalf("NewGitStore: %v", err)
	}
	store, err := NewGitStore(multiRepoLocator{repos: map[ProjectID]*fwgithub.GitStore{id: gs}}, false)
	if err != nil {
		t.Fatalf("NewGitStore(RA): %v", err)
	}
	store = store.WithCatalog(fixedCatalog{refs: []ProjectCatalogRef{{ProjectID: id, Title: "One"}}})

	summaries, err := store.ListProjects(context.Background(), "alice", RepoCredential{})
	var e *fwra.Error
	if !errors.As(err, &e) || e.Kind != fwra.ContractMisuse {
		t.Fatalf("an empty cloud credential must fail the list with ContractMisuse; got %+v, %v", summaries, err)
	}
}

// ---- Operator notes (plan B1.1) ------------------------------------------------------

func operatorNoteInput(id string, kind OperatorNoteKind, text string) OperatorNoteInput {
	return OperatorNoteInput{NoteID: id, Kind: kind, Gate: "detailed_design", Text: text}
}

func raKindOf(err error) fwra.Kind {
	var e *fwra.Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return fwra.Unknown
}

var noteClock = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

func TestRecordOperatorNote_AppendsInRecordedOrder(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	store = store.WithClock(func() time.Time { return noteClock })
	rc := fwra.Context{Context: context.Background()}
	first := operatorNoteInput("C-A:note:r1:1", NoteSendBack, "tighten the error model")
	first.Comments = []NoteComment{{JSONPath: "$.ops[0]", Text: "name the failure"}}
	v, err := store.RecordOperatorNote(rc, id, v, "C-A", first, cred, fwra.IdempotencyKey("wf:note-1"))
	if err != nil {
		t.Fatalf("RecordOperatorNote(first): %v", err)
	}
	second := operatorNoteInput("C-A:note:r1:2", NoteRetry, "the fixture server was down")
	second.Gate = "takeover"
	if _, err := store.RecordOperatorNote(rc, id, v, "C-A", second, cred, fwra.IdempotencyKey("wf:note-2")); err != nil {
		t.Fatalf("RecordOperatorNote(second): %v", err)
	}
	want := []OperatorNote{
		{NoteID: first.NoteID, Kind: NoteSendBack, Gate: "detailed_design", Text: first.Text, Comments: first.Comments, RecordedAt: noteClock},
		{NoteID: second.NoteID, Kind: NoteRetry, Gate: "takeover", Text: second.Text, RecordedAt: noteClock},
	}
	if got := readConstruction(t, store, id, cred, "C-A").OperatorNotes; !reflect.DeepEqual(got, want) {
		t.Fatalf("OperatorNotes = %+v, want %+v", got, want)
	}
}

func TestRecordOperatorNote_OneIDNamesOneNote(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	rc := fwra.Context{Context: context.Background()}
	n := operatorNoteInput("C-A:note:r1:1", NoteSendBack, "tighten the error model")
	v, err := store.RecordOperatorNote(rc, id, v, "C-A", n, cred, fwra.IdempotencyKey("wf:a"))
	if err != nil {
		t.Fatalf("RecordOperatorNote: %v", err)
	}
	// The same note again under a NEW idempotency key (not a ledger replay): nothing appended.
	v, err = store.RecordOperatorNote(rc, id, v, "C-A", n, cred, fwra.IdempotencyKey("wf:b"))
	if err != nil {
		t.Fatalf("re-recording identical content must succeed, got %v", err)
	}
	if got := readConstruction(t, store, id, cred, "C-A").OperatorNotes; len(got) != 1 {
		t.Fatalf("identical content appended a second note: %+v", got)
	}
	for name, changed := range map[string]OperatorNoteInput{
		"text":     {NoteID: n.NoteID, Kind: n.Kind, Gate: n.Gate, Text: "something else"},
		"kind":     {NoteID: n.NoteID, Kind: NoteRetry, Gate: n.Gate, Text: n.Text},
		"gate":     {NoteID: n.NoteID, Kind: n.Kind, Gate: "integration", Text: n.Text},
		"comments": {NoteID: n.NoteID, Kind: n.Kind, Gate: n.Gate, Text: n.Text, Comments: []NoteComment{{JSONPath: "$", Text: "x"}}},
	} {
		if _, err := store.RecordOperatorNote(rc, id, v, "C-A", changed, cred, fwra.IdempotencyKey("wf:c-"+name)); raKindOf(err) != fwra.ContractMisuse {
			t.Errorf("same id, different %s: want ContractMisuse, got %v", name, err)
		}
	}
	if got := readConstruction(t, store, id, cred, "C-A").OperatorNotes; len(got) != 1 || got[0].Text != n.Text {
		t.Fatalf("a refused duplicate changed the note: %+v", got)
	}
}

func TestRecordOperatorNote_RefusesMisuse(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	rc := fwra.Context{Context: context.Background()}
	cases := map[string]struct {
		activityID string
		note       OperatorNoteInput
	}{
		"empty activityID":  {"", operatorNoteInput("n", NoteSendBack, "t")},
		"empty noteId":      {"C-A", operatorNoteInput("", NoteSendBack, "t")},
		"blank text":        {"C-A", operatorNoteInput("n", NoteSendBack, " \n\t")},
		"unknown kind 0":    {"C-A", operatorNoteInput("n", OperatorNoteKindUnknown, "t")},
		"kind out of range": {"C-A", operatorNoteInput("n", OperatorNoteKind(7), "t")},
	}
	for name, c := range cases {
		if _, err := store.RecordOperatorNote(rc, id, v, c.activityID, c.note, cred, fwra.IdempotencyKey("wf:"+name)); raKindOf(err) != fwra.ContractMisuse {
			t.Errorf("%s: want ContractMisuse, got %v", name, err)
		}
	}
}

func TestRecordOperatorNoteDelivered_StampsOnce(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	store = store.WithClock(func() time.Time { return noteClock })
	rc := fwra.Context{Context: context.Background()}
	v, err := store.RecordOperatorNote(rc, id, v, "C-A", operatorNoteInput("n1", NoteSendBack, "tighten it"), cred, fwra.IdempotencyKey("wf:rec"))
	if err != nil {
		t.Fatalf("RecordOperatorNote: %v", err)
	}
	const attempt = "C-A:detailedDesign:2"
	v, err = store.RecordOperatorNoteDelivered(rc, id, v, "C-A", "n1", attempt, cred, fwra.IdempotencyKey("wf:d1"))
	if err != nil {
		t.Fatalf("RecordOperatorNoteDelivered: %v", err)
	}
	got := readConstruction(t, store, id, cred, "C-A").OperatorNotes[0]
	if got.DeliveredToAttemptID != attempt || got.DeliveredAt == nil || !got.DeliveredAt.Equal(noteClock) {
		t.Fatalf("delivery stamp = %q at %v, want %q at %v", got.DeliveredToAttemptID, got.DeliveredAt, attempt, noteClock)
	}
	// Re-stamping the same attempt (a retried write under a new key) changes nothing.
	later := store.WithClock(func() time.Time { return noteClock.Add(time.Hour) })
	v, err = later.RecordOperatorNoteDelivered(rc, id, v, "C-A", "n1", attempt, cred, fwra.IdempotencyKey("wf:d2"))
	if err != nil {
		t.Fatalf("re-stamping the same attempt must succeed, got %v", err)
	}
	if again := readConstruction(t, store, id, cred, "C-A").OperatorNotes[0]; !again.DeliveredAt.Equal(noteClock) {
		t.Fatalf("re-stamping moved DeliveredAt to %v", again.DeliveredAt)
	}
	// A note is delivered once.
	if _, err := store.RecordOperatorNoteDelivered(rc, id, v, "C-A", "n1", "C-A:detailedDesign:3", cred, fwra.IdempotencyKey("wf:d3")); raKindOf(err) != fwra.ContractMisuse {
		t.Fatalf("stamping a different attempt: want ContractMisuse, got %v", err)
	}
	if kept := readConstruction(t, store, id, cred, "C-A").OperatorNotes[0]; kept.DeliveredToAttemptID != attempt {
		t.Fatalf("a refused re-stamp moved the delivery to %q", kept.DeliveredToAttemptID)
	}
	if _, err := store.RecordOperatorNoteDelivered(rc, id, v, "C-A", "no-such-note", attempt, cred, fwra.IdempotencyKey("wf:d4")); raKindOf(err) != fwra.NotFound {
		t.Errorf("unknown note: want NotFound, got %v", err)
	}
	if _, err := store.RecordOperatorNoteDelivered(rc, id, v, "C-ZZ", "n1", attempt, cred, fwra.IdempotencyKey("wf:d5")); raKindOf(err) != fwra.NotFound {
		t.Errorf("no such row: want NotFound, got %v", err)
	}
	for name, args := range map[string][3]string{
		"empty activityID": {"", "n1", attempt}, "empty noteID": {"C-A", "", attempt}, "empty attemptID": {"C-A", "n1", ""},
	} {
		if _, err := store.RecordOperatorNoteDelivered(rc, id, v, args[0], args[1], args[2], cred, fwra.IdempotencyKey("wf:m-"+name)); raKindOf(err) != fwra.ContractMisuse {
			t.Errorf("%s: want ContractMisuse, got %v", name, err)
		}
	}
}

// Notes survive the project.json codec and the Temporal envelope, and a row without
// notes serializes with no operatorNotes key at all (byte-identical to before B1.1).
func TestOperatorNotes_RoundTripAndOmittedWhenNone(t *testing.T) {
	delivered := noteClock.Add(time.Minute)
	notes := []OperatorNote{
		{NoteID: "n1", Kind: NoteSendBack, Gate: "detailed_design", Text: "tighten it",
			Comments: []NoteComment{{JSONPath: "$.ops[0]", Text: "name the failure"}}, RecordedAt: noteClock,
			DeliveredToAttemptID: "C-A:detailedDesign:2", DeliveredAt: &delivered},
		{NoteID: "n2", Kind: NoteSkip, Text: "built by hand", RecordedAt: noteClock},
	}
	p := Project{ID: ProjectID(uuid.NewString()), Version: 3, ActivityExecution: map[string]ActivityExecution{
		"C-A": {ActivityID: "C-A", OperatorNotes: notes},
		"C-B": {ActivityID: "C-B", StartedAt: &envelopeStartedAt},
	}}
	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	decoded, _, err := DecodeProjectJSON(raw, p.ID)
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !reflect.DeepEqual(decoded.ActivityExecution["C-A"].OperatorNotes, notes) {
		t.Fatalf("codec round trip: %+v, want %+v", decoded.ActivityExecution["C-A"].OperatorNotes, notes)
	}
	env, err := EncodeProject(p)
	if err != nil {
		t.Fatalf("EncodeProject: %v", err)
	}
	wireRaw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var wire ProjectEnvelope
	if err := json.Unmarshal(wireRaw, &wire); err != nil {
		t.Fatal(err)
	}
	fromWire, err := wire.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(fromWire.ActivityExecution["C-A"].OperatorNotes, notes) {
		t.Fatalf("envelope round trip: %+v, want %+v", fromWire.ActivityExecution["C-A"].OperatorNotes, notes)
	}
	rowB, err := json.Marshal(p.ActivityExecution["C-B"])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rowB, []byte("operatorNotes")) {
		t.Fatalf("a row without notes must omit the key, got %s", rowB)
	}
}

func TestPendingOperatorNotes_UndeliveredDeliverableKindsInOrder(t *testing.T) {
	r := ActivityExecution{OperatorNotes: []OperatorNote{
		{NoteID: "a", Kind: NoteSendBack},
		{NoteID: "b", Kind: NoteSkip},
		{NoteID: "c", Kind: NoteRetry, DeliveredToAttemptID: "X:srs:1"},
		{NoteID: "d", Kind: NoteTakeover},
		{NoteID: "e", Kind: NoteReassign},
		{NoteID: "f", Kind: NoteRequeue},
		{NoteID: "g", Kind: NoteRetry},
	}}
	var got []string
	for _, n := range PendingOperatorNotes(r) {
		got = append(got, n.NoteID)
	}
	if want := []string{"a", "d", "e", "f", "g"}; !slices.Equal(got, want) {
		t.Fatalf("pending = %v, want %v (skip and delivered notes are never pending)", got, want)
	}
}

// ---- lifecycles.json, held against this package's remaining hand tables ----
//
// The per-type lifecycle no longer exists twice: profileRows is gone and ProfileFor is
// an adapter over the data, so a profile-vs-lifecycle comparison compares the data with
// itself. What these two still hold is the DESIGN rail, whose commands and required
// kinds are hand tables here (DesignCommandFor, Phase1RequiredKinds) and whose
// lifecycles ship in the platform: a command renamed on one side alone fails here.

// The requirements and architecture lifecycles are today's design rail, in order:
// one draft per Phase1RequiredKinds() kind, dispatched by DesignCommandFor's draft
// command, and critiqued by exactly the command DesignCommandFor dispatches today —
// "" for a kind designKindHasCritique excludes (volatilities).
func TestLifecyclesParity_DesignActivitiesFollowTheDesignRail(t *testing.T) {
	var drafts []methodassets.LifecycleTask
	critiqueOf := map[string]string{}
	for _, key := range []string{"requirements", "architecture"} {
		lc, ok := methodassets.LifecycleFor(key)
		if !ok {
			t.Fatalf("method-assets carries no lifecycle %q", key)
		}
		for _, task := range lc.Tasks {
			if task.Kind == methodassets.LifecycleTaskDispatch {
				drafts = append(drafts, task)
				continue
			}
			critiqueOf[task.Reviews] = task.Command
		}
	}
	kinds := Phase1RequiredKinds()
	if len(drafts) != len(kinds) {
		t.Fatalf("%d design dispatch tasks, Phase1RequiredKinds has %d", len(drafts), len(kinds))
	}
	for i, k := range kinds {
		draft := drafts[i]
		if draft.ArtifactKind != k.String() {
			t.Errorf("design dispatch %d produces %q, want %q", i, draft.ArtifactKind, k.String())
		}
		if want := DesignCommandFor(k, DesignJobModeDraft, ""); draft.Command != want {
			t.Errorf("%s command = %q, want %q", draft.ID, draft.Command, want)
		}
		if want := DesignCommandFor(k, DesignJobModeCritique, ""); critiqueOf[draft.ID] != want {
			t.Errorf("%s is critiqued by %q, want %q", draft.ID, critiqueOf[draft.ID], want)
		}
	}
}

// Project Design is deterministic (R7): one human gate over the computed SDP, and
// nothing to dispatch — the same "" DesignCommandFor returns for KindSdpReview.
func TestLifecyclesParity_ProjectDesignIsOneUndispatchedGate(t *testing.T) {
	lc, ok := methodassets.LifecycleFor("projectDesign")
	if !ok || len(lc.Tasks) != 1 {
		t.Fatalf("projectDesign must be one task, got %+v", lc)
	}
	gate := lc.Tasks[0]
	if gate.Kind != methodassets.LifecycleTaskReview || gate.ArtifactKind != KindSdpReview.String() {
		t.Errorf("projectDesign gate = %+v, want a review of %s", gate, KindSdpReview)
	}
	if want := DesignCommandFor(KindSdpReview, DesignJobModeDraft, ""); gate.Command != want {
		t.Errorf("projectDesign gate command = %q, want %q", gate.Command, want)
	}
}

// The lifecycle-key rule, over EVERY activity type and testing variant, at its ONE
// production home. A stray variant on a non-testing type is ignored (the zero variant
// is what every non-testing activity carries), and every key it produces must resolve
// in the pinned method-assets — a key rule nothing can look up is a silent 404.
func TestLifecycleKeyFor_CoversEveryTypeAndVariant(t *testing.T) {
	cases := []struct {
		typ     ActivityType
		variant TestingVariant
		want    string
	}{
		{ActivityTypeService, TestVariantPlan, "service"},
		{ActivityTypeFrontend, TestVariantPlan, "frontend"},
		{ActivityTypeDeployment, TestVariantPlan, "deployment"},
		{ActivityTypeDocumentation, TestVariantPlan, "documentation"},
		{ActivityTypeUIDesign, TestVariantPlan, "uiDesign"},
		{ActivityTypeIntegration, TestVariantPlan, "integration"},
		{ActivityTypeTesting, TestVariantPlan, "testing:plan"},
		{ActivityTypeTesting, TestVariantHarness, "testing:harness"},
		{ActivityTypeTesting, TestVariantPerf, "testing:perf"},
		{ActivityTypeTesting, TestVariantSystemTest, "testing:systemTest"},
		{ActivityTypeTesting, TestVariantQAProcess, "testing:qaProcess"},
		{ActivityTypeService, TestVariantHarness, "service"},
	}
	for _, c := range cases {
		got := LifecycleKeyFor(c.typ, c.variant)
		if got != c.want {
			t.Errorf("LifecycleKeyFor(%s, %s) = %q, want %q", c.typ, c.variant, got, c.want)
		}
		if _, ok := methodassets.LifecycleFor(got); !ok {
			t.Errorf("method-assets has no lifecycle for key %q", got)
		}
	}
}

// ---- lifecycle totality (stage 2: the data is the only source) ----
//
// profileRows is gone, so profile↔lifecycle parity is tautological and its test with
// it. What survives is the pair of claims a table cannot make for itself: every
// (ActivityType, TestingVariant) an activity can carry resolves to a lifecycle, and
// every lifecycle the platform ships is reachable from one.

func TestEveryActivityTypeResolvesToALifecycle(t *testing.T) {
	for _, combo := range allActivityTypeCombos() {
		key := LifecycleKeyFor(combo.t, combo.v)
		lc, ok := methodassets.LifecycleFor(key)
		if !ok {
			t.Errorf("no lifecycle %q — ProfileFor would silently fall back to service", key)
			continue
		}
		if got := ProfileFor(combo.t, combo.v); len(got.Phases) != len(lc.Phases) {
			t.Errorf("%s: ProfileFor has %d phases, the lifecycle has %d", key, len(got.Phases), len(lc.Phases))
		}
		for _, ph := range lc.Phases {
			p := ActivityMethodPhase(ph.ID)
			// A GATE-ONLY phase carries no dispatch task on purpose — projectDesign's
			// `sdp` is the M0 review and nothing is dispatched into it — so "" is the
			// honest command there. The claim is only about the phases the data says
			// something IS dispatched into.
			if rawDispatchCommand(lc, ph.ID) != "" && CommandFor(combo.t, combo.v, p) == "" {
				t.Errorf("%s/%s: no dispatch command — the phase walk would dispatch nothing", key, p)
			}
			if ph.ExitCriterion == "" {
				t.Errorf("%s/%s: no exit criterion", key, p)
			}
		}
	}
}

func TestEveryLifecycleIsReachableFromAnActivityType(t *testing.T) {
	reachable := map[string]bool{}
	for _, combo := range allActivityTypeCombos() {
		reachable[LifecycleKeyFor(combo.t, combo.v)] = true
	}
	for _, lc := range methodassets.Lifecycles() {
		if !reachable[lc.Type] {
			t.Errorf("lifecycle %q is shipped but no activity type reaches it", lc.Type)
		}
		delete(reachable, lc.Type)
	}
	for key := range reachable {
		t.Errorf("activity types reach key %q but the platform ships no such lifecycle", key)
	}
}

// A phase a profile does not carry has no command. profileSlug used to fabricate one
// ("deployment-requirements") for a .claude/commands file that does not exist; the data
// simply has no dispatch task there, and "" is the honest answer.
func TestCommandFor_IsEmptyForAPhaseTheProfileDoesNotCarry(t *testing.T) {
	if got := CommandFor(ActivityTypeDeployment, 0, MethodPhaseRequirements); got != "" {
		t.Errorf("CommandFor(deployment, requirements) = %q, want \"\"", got)
	}
	if got := CommandFor(ActivityTypeIntegration, 0, MethodPhaseConstruction); got != "" {
		t.Errorf("CommandFor(integration, construction) = %q, want \"\"", got)
	}
}

// A phase id names ONE work task and ONE gate task across every lifecycle the platform
// ships. That is what lets AgentTaskFor and GateTaskFor take a phase and no activity
// type — the signature the workflow's phase walk and App A's completion rule both need.
// A release that made two lifecycles disagree about a phase id would otherwise be
// resolved silently, by map-insertion order.
func TestLifecyclePhaseTasksAreUnambiguous(t *testing.T) {
	type pair struct{ work, gate, source string }
	seen := map[string]pair{}
	for _, lc := range methodassets.Lifecycles() {
		for _, ph := range lc.Phases {
			got := pair{dispatchTaskIn(lc, ActivityMethodPhase(ph.ID)).ID, ph.Gate, lc.Type}
			prev, held := seen[ph.ID]
			if held && (prev.work != got.work || prev.gate != got.gate) {
				t.Errorf("phase %q: %s says work=%q gate=%q, %s says work=%q gate=%q",
					ph.ID, prev.source, prev.work, prev.gate, got.source, got.work, got.gate)
			}
			if !held {
				seen[ph.ID] = got
			}
		}
	}
}

// The same, one level down: a task id belongs to ONE phase across every lifecycle, which
// is what makes PhaseForTask's denormalized stamp on a TaskAttempt well defined.
func TestLifecycleTasksBelongToOnePhase(t *testing.T) {
	seen := map[string]string{}
	for _, lc := range methodassets.Lifecycles() {
		for _, task := range lc.Tasks {
			if prev, held := seen[task.ID]; held && prev != task.Phase {
				t.Errorf("task %q is in phase %q and in phase %q", task.ID, prev, task.Phase)
			}
			seen[task.ID] = task.Phase
		}
	}
	for task, p := range conditionalTasks {
		if p == "" {
			continue
		}
		if _, isNode := seen[string(task)]; isNode {
			t.Errorf("%q is recorded as a sub-attempt of %q but the data carries a node for it", task, p)
		}
	}
}

// ===========================================================================
// activityExecutionAccess — the fifth contract facet (stage 3 task 3).
//
// The facet is ADDITIVE: it is exercised here against the same git-local
// substrate the other four facets' tests use, through the same
// applyMutationOnBranchFiles funnel, so its idempotency comes from the same
// dedup-first probe and its conflict semantics from the same ref-CAS.
// ===========================================================================

// newExecutionStore is newConstructionStore plus the facet under test and an opened
// activity — every verb below but OpenActivity needs a row to write onto.
func newExecutionStore(t *testing.T) (ActivityExecutionAccess, *GitStore, ProjectID, Version, RepoCredential) {
	t.Helper()
	store, id, v, cred := newConstructionStore(t)
	return &activityExecutionAccess{store: store, minter: localCredentialMinter{}}, store, id, v, cred
}

func execRC() fwra.Context { return fwra.Context{Context: context.Background()} }

// openTestActivity opens C-X so the verbs under test have a row, and returns the
// version after the open.
func openTestActivity(t *testing.T, a ActivityExecutionAccess, id ProjectID, v Version, cred RepoCredential) Version {
	t.Helper()
	v2, err := a.OpenActivity(execRC(), id, v, NoActivityVersionExpectation, "C-X", ActivityTypeService, TestVariantPlan,
		LifecyclePin{TypeKey: "service", AssetsVersion: "v0.9.0"}, cred, fwra.IdempotencyKey("wf:open"))
	if err != nil {
		t.Fatalf("OpenActivity: %v", err)
	}
	return v2
}

// openRoundFixture opens one review round on C-X and returns the version after it.
func openRoundFixture(t *testing.T, a ActivityExecutionAccess, id ProjectID, v Version, cred RepoCredential) Version {
	t.Helper()
	v2, err := a.OpenReviewRound(execRC(), id, v, NoActivityVersionExpectation, "C-X", ReviewRoundInput{
		RoundID: "C-X:designReview:1", TaskID: TaskDesignReview, Reviews: TaskDetailedDesign, Round: 1,
		SubjectRef: SubjectRef{Kind: SubjectCommit, Ref: "deadbeef"},
		Reviewers:  []RoundReviewer{{Role: "architect", Actor: "system-architect", Required: true}},
	}, cred, fwra.IdempotencyKey("wf:round-1"))
	if err != nil {
		t.Fatalf("OpenReviewRound: %v", err)
	}
	return v2
}

// TestOpenActivity_BirthsTheRowAndPinsItsLifecycle — OpenActivity is the fold of
// RecordActivityStarted + RecordPhaseStarted: it births the row, stamps the type pair
// the dispatcher classified, seeds the phase set from THAT pair, and pins the lifecycle
// the task DAG was resolved against so a mid-flight method-assets release cannot
// re-shape an activity that is already running.
func TestOpenActivity_BirthsTheRowAndPinsItsLifecycle(t *testing.T) {
	a, store, id, v, cred := newExecutionStore(t)

	v2 := openTestActivity(t, a, id, v, cred)
	if v2 != v+1 {
		t.Fatalf("version = %d, want %d", v2, v+1)
	}
	row := readConstruction(t, store, id, cred, "C-X")
	if coarsePhaseOf(row) != ActivityConstructionRunning {
		t.Fatalf("Phase = %v, want Running", coarsePhaseOf(row))
	}
	if row.Type != ActivityTypeService {
		t.Fatalf("Type = %v, want service", row.Type)
	}
	if row.StartedAt == nil {
		t.Fatal("StartedAt must be stamped by the store")
	}
	if row.Version == 0 {
		t.Fatal("OpenActivity must stamp the per-activity version")
	}
	if row.Pin == nil || row.Pin.TypeKey != "service" || row.Pin.AssetsVersion != "v0.9.0" {
		t.Fatalf("lifecycle pin = %+v, want the pin the caller opened against", row.Pin)
	}
}

// TestOpenReviewRound_IsIdempotentUnderRetry — two appends of the SAME deterministic
// RoundID under retry produce ONE round. This is the property the whole ledger rests
// on: Temporal retries an activity, and an append that is not idempotent doubles the
// history it is supposed to record.
func TestOpenReviewRound_IsIdempotentUnderRetry(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)

	in := ReviewRoundInput{
		RoundID: "C-X:designReview:1", TaskID: TaskDesignReview, Reviews: TaskDetailedDesign, Round: 1,
		SubjectRef: SubjectRef{Kind: SubjectCommit, Ref: "deadbeef"},
		Reviewers:  []RoundReviewer{{Role: "architect", Actor: "system-architect", Required: true}},
	}
	v1, err := a.OpenReviewRound(execRC(), id, v, NoActivityVersionExpectation, "C-X", in, cred, fwra.IdempotencyKey("k1"))
	if err != nil {
		t.Fatalf("OpenReviewRound: %v", err)
	}
	v2, err := a.OpenReviewRound(execRC(), id, v, NoActivityVersionExpectation, "C-X", in, cred, fwra.IdempotencyKey("k1"))
	if err != nil || v2 != v1 {
		t.Fatalf("a retry of the same idempotencyKey must replay, not re-apply: v1=%d v2=%d err=%v", v1, v2, err)
	}
	exec, err := a.ReadActivityExecution(execRC(), id, "C-X")
	if err != nil {
		t.Fatalf("ReadActivityExecution: %v", err)
	}
	if len(exec.Reviews) != 1 {
		t.Fatalf("one round, appended once; got %d: %+v", len(exec.Reviews), exec.Reviews)
	}
	if exec.Reviews[0].Outcome != RoundPending {
		t.Fatalf("a freshly opened round is pending; got %q", exec.Reviews[0].Outcome)
	}
	if exec.Reviews[0].OpenedAt == "" {
		t.Fatal("OpenedAt must be stamped by the store")
	}
}

// TestOpenReviewRound_ADifferentKeyForTheSameRoundStillAppendsOnce — the dedup ledger
// covers the RETRY; the round id covers the RE-ISSUE. A caller that mints a fresh
// idempotencyKey for a round it already opened must still not double the history.
func TestOpenReviewRound_ADifferentKeyForTheSameRoundStillAppendsOnce(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)
	v = openRoundFixture(t, a, id, v, cred)

	if _, err := a.OpenReviewRound(execRC(), id, v, NoActivityVersionExpectation, "C-X", ReviewRoundInput{
		RoundID: "C-X:designReview:1", TaskID: TaskDesignReview, Reviews: TaskDetailedDesign, Round: 1,
		SubjectRef: SubjectRef{Kind: SubjectCommit, Ref: "deadbeef"},
	}, cred, fwra.IdempotencyKey("a-different-key")); err != nil {
		t.Fatalf("re-opening the same round must be a no-op success: %v", err)
	}
	exec, _ := a.ReadActivityExecution(execRC(), id, "C-X")
	if len(exec.Reviews) != 1 {
		t.Fatalf("one round id, one round; got %d", len(exec.Reviews))
	}
}

// TestAppendReviewVerdict_CarriesItsCommentsInTheSameCommit — a verdict and its comments
// land in ONE commit (spec §5.3, "AppendReviewVerdict (verdict + its comments in one
// commit)"). A crash between them would leave a round whose verdict cites comments
// nobody can read.
func TestAppendReviewVerdict_CarriesItsCommentsInTheSameCommit(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)
	v = openRoundFixture(t, a, id, v, cred)

	if _, err := a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1",
		ReviewVerdict{ReviewerRole: "architect", Actor: "system-architect", Verdict: VerdictSendBack,
			Summary: "contract too wide", AttemptID: "C-X:detailedDesign:1"},
		[]ReviewComment{{Anchor: "ops[3]", Text: "split this op", AuthorRole: "architect", Type: "changeRequest"}},
		nil, cred, fwra.IdempotencyKey("k2")); err != nil {
		t.Fatalf("AppendReviewVerdict: %v", err)
	}
	exec, _ := a.ReadActivityExecution(execRC(), id, "C-X")
	r := exec.Reviews[0]
	if len(r.Verdicts) != 1 || len(r.Thread) != 1 {
		t.Fatalf("verdict and comment must land together; verdicts=%d thread=%d", len(r.Verdicts), len(r.Thread))
	}
	if r.Verdicts[0].At == "" {
		t.Fatal("the store stamps the verdict clock, not the caller")
	}
	if r.Thread[0].ID != ReviewCommentID(1, 0) || r.Thread[0].Status != ReviewCommentOpen {
		t.Fatalf("the store mints the comment id and opens it, exactly as the artifact ledger does: %+v", r.Thread[0])
	}
	if r.Outcome != RoundPending {
		t.Fatalf("a verdict does not decide the round; outcome=%q", r.Outcome)
	}
}

// TestAppendReviewVerdict_IsIdempotentOnItsOwnContent — the same verdict re-appended
// under a FRESH idempotencyKey appends once. A verdict is identified by who cast it, on
// what, and how — not by the key the caller happened to mint.
func TestAppendReviewVerdict_IsIdempotentOnItsOwnContent(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)
	v = openRoundFixture(t, a, id, v, cred)

	verdict := ReviewVerdict{ReviewerRole: "architect", Actor: "system-architect",
		Verdict: VerdictApprove, Summary: "good", AttemptID: "C-X:detailedDesign:1"}
	var err error
	if v, err = a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1", verdict, nil, nil, cred, fwra.IdempotencyKey("k3")); err != nil {
		t.Fatalf("AppendReviewVerdict: %v", err)
	}
	if _, err = a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1", verdict, nil, nil, cred, fwra.IdempotencyKey("k4")); err != nil {
		t.Fatalf("AppendReviewVerdict (re-issue): %v", err)
	}
	exec, _ := a.ReadActivityExecution(execRC(), id, "C-X")
	if len(exec.Reviews[0].Verdicts) != 1 {
		t.Fatalf("one verdict, appended once; got %d", len(exec.Reviews[0].Verdicts))
	}
}

// TestDecideReviewRound_IsTerminalAndAppendOnly — DecideReviewRound stamps the outcome,
// it never rewrites a verdict or a comment, and a second decision on a decided round is
// refused.
func TestDecideReviewRound_IsTerminalAndAppendOnly(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)
	v = openRoundFixture(t, a, id, v, cred)

	var err error
	if v, err = a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1",
		ReviewVerdict{ReviewerRole: "architect", Actor: "system-architect", Verdict: VerdictSendBack,
			Summary: "no", AttemptID: "C-X:detailedDesign:1"},
		[]ReviewComment{{Anchor: "ops[0]", Text: "name the failure", AuthorRole: "architect", Type: "changeRequest"}},
		nil, cred, fwra.IdempotencyKey("k5")); err != nil {
		t.Fatalf("AppendReviewVerdict: %v", err)
	}
	if v, err = a.DecideReviewRound(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1", RoundSentBack, "system-architect", cred, fwra.IdempotencyKey("k6")); err != nil {
		t.Fatalf("DecideReviewRound: %v", err)
	}
	exec, _ := a.ReadActivityExecution(execRC(), id, "C-X")
	r := exec.Reviews[0]
	if r.Outcome != RoundSentBack || r.DecidedBy != "system-architect" || r.DecidedAt == "" {
		t.Fatalf("decision not stamped: %+v", r)
	}
	if len(r.Verdicts) != 1 || len(r.Thread) != 1 {
		t.Fatalf("deciding must not rewrite the ledger; verdicts=%d thread=%d", len(r.Verdicts), len(r.Thread))
	}

	_, err = a.DecideReviewRound(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1", RoundPassed, "someone-else", cred, fwra.IdempotencyKey("k7"))
	if err == nil {
		t.Fatal("a decided round is terminal; a second, different decision must be refused")
	}
	if got := kindOfErr(err); got != fwra.Conflict {
		t.Fatalf("second decision error class = %v, want Conflict", got)
	}
	if _, err := a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1",
		ReviewVerdict{ReviewerRole: "pm", Actor: "product-manager", Verdict: VerdictApprove, Summary: "late", AttemptID: "C-X:detailedDesign:1"},
		nil, nil, cred, fwra.IdempotencyKey("k8")); err == nil {
		t.Fatal("a decided round takes no further verdicts")
	}
}

// TestDecideReviewRound_RefusesPendingAsADecision — "pending" is the state a round is in
// before it is decided, never a decision. Allowing it would silently un-decide a round.
func TestDecideReviewRound_RefusesPendingAsADecision(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)
	v = openRoundFixture(t, a, id, v, cred)

	_, err := a.DecideReviewRound(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1", RoundPending, "system-architect", cred, fwra.IdempotencyKey("k9"))
	if err == nil || kindOfErr(err) != fwra.ContractMisuse {
		t.Fatalf("deciding a round 'pending' must be ContractMisuse; got %v", err)
	}
}

// TestSetReviewCommentStatus_WalksTheRoundsThread — the comment vocabulary and its legal
// transitions are the artifact ledger's, reused verbatim; only the ledger it walks moved.
func TestSetReviewCommentStatus_WalksTheRoundsThread(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)
	v = openRoundFixture(t, a, id, v, cred)

	var err error
	if v, err = a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1",
		ReviewVerdict{ReviewerRole: "architect", Actor: "system-architect", Verdict: VerdictSendBack, Summary: "no", AttemptID: "C-X:detailedDesign:1"},
		[]ReviewComment{{Anchor: "ops[0]", Text: "split", AuthorRole: "architect", Type: "changeRequest"}},
		nil, cred, fwra.IdempotencyKey("k10")); err != nil {
		t.Fatalf("AppendReviewVerdict: %v", err)
	}
	if v, err = a.SetReviewCommentStatus(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1", ReviewCommentID(1, 0), ReviewCommentResolved, cred, fwra.IdempotencyKey("k11")); err != nil {
		t.Fatalf("SetReviewCommentStatus: %v", err)
	}
	exec, _ := a.ReadActivityExecution(execRC(), id, "C-X")
	if exec.Reviews[0].Thread[0].Status != ReviewCommentResolved {
		t.Fatalf("status = %q, want resolved", exec.Reviews[0].Thread[0].Status)
	}
	if _, err := a.SetReviewCommentStatus(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1", "nope", ReviewCommentResolved, cred, fwra.IdempotencyKey("k12")); err == nil ||
		kindOfErr(err) != fwra.NotFound {
		t.Fatalf("an unknown comment id must be NotFound; got %v", err)
	}
}

// TestRecordAttemptOutcome_AppendsOnceAndResolvesInPlace — an attempt is opened pending
// and resolved later under a DIFFERENT idempotencyKey; that is one attempt, not two, and
// the AttemptID format the episode ledger joins on is preserved verbatim.
func TestRecordAttemptOutcome_AppendsOnceAndResolvesInPlace(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)

	in := TaskAttemptInput{AttemptID: AttemptID("C-X", TaskDetailedDesign, 1), TaskID: TaskDetailedDesign,
		Attempt: 1, Actor: ActorAgent, Outcome: OutcomePending, EvidenceKind: EvidenceEpisode, EvidenceRef: "ep-1"}
	var err error
	if v, err = a.RecordAttemptOutcome(execRC(), id, v, NoActivityVersionExpectation, "C-X", in, cred, fwra.IdempotencyKey("k13")); err != nil {
		t.Fatalf("RecordAttemptOutcome (open): %v", err)
	}
	in.Outcome = OutcomePassed
	if v, err = a.RecordAttemptOutcome(execRC(), id, v, NoActivityVersionExpectation, "C-X", in, cred, fwra.IdempotencyKey("k14")); err != nil {
		t.Fatalf("RecordAttemptOutcome (resolve): %v", err)
	}
	exec, _ := a.ReadActivityExecution(execRC(), id, "C-X")
	if len(exec.Attempts) != 1 {
		t.Fatalf("one attempt id, one attempt; got %d", len(exec.Attempts))
	}
	at := exec.Attempts[0]
	if at.AttemptID != "C-X:detailedDesign:1" {
		t.Fatalf("AttemptID = %q — the episode ledger's TargetRef join depends on this format", at.AttemptID)
	}
	if at.Outcome != OutcomePassed || at.EndedAt == nil {
		t.Fatalf("a resolved attempt carries its outcome and an end stamp: %+v", at)
	}
	if at.Phase != PhaseForTask(TaskDetailedDesign) {
		t.Fatalf("Phase = %q, want the task's own phase %q", at.Phase, PhaseForTask(TaskDetailedDesign))
	}
	if at.Provenance.Origin != OriginObserved {
		t.Fatalf("a live write is observed, not synthesized; got %q", at.Provenance.Origin)
	}

	in.Outcome = OutcomeFailed
	if _, err := a.RecordAttemptOutcome(execRC(), id, v, NoActivityVersionExpectation, "C-X", in, cred, fwra.IdempotencyKey("k15")); err == nil {
		t.Fatal("a resolved attempt must not be re-resolved differently; one id names one attempt")
	}
}

// TestRecordActivityOutcome_FoldsExitedAndFailed — the one verb carries both terminals
// the two retired ones did, and the failure arm keeps the closed reason vocabulary.
func TestRecordActivityOutcome_FoldsExitedAndFailed(t *testing.T) {
	for _, tt := range []struct {
		name      string
		outcome   ActivityOutcome
		reason    FailureReason
		wantPhase ActivityConstructionPhase
		wantBuild ActivityBuildStatus
	}{
		// "completed" reads done-but-not-integrated here because nothing was recorded on the
		// ledger: integration is the claim that every gate passed, and this store holds no
		// gate attempt at all. That is the disposition, not a regression — it is the same
		// rule that makes the skipped case below read the way it does.
		{"completed", ActivityOutcomeCompleted, FailureReasonUnknown, ActivityConstructionDone, BuildInReview},
		{"skipped", ActivityOutcomeSkipped, FailureReasonUnknown, ActivityConstructionDone, BuildInReview},
		{"failed", ActivityOutcomeUnknown, PipelineFailed, ActivityConstructionFailed, BuildFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, store, id, v, cred := newExecutionStore(t)
			v = openTestActivity(t, a, id, v, cred)
			if _, err := a.RecordActivityOutcome(execRC(), id, v, NoActivityVersionExpectation, "C-X", tt.outcome, tt.reason, "detail", cred, fwra.IdempotencyKey("k16")); err != nil {
				t.Fatalf("RecordActivityOutcome: %v", err)
			}
			row := readConstruction(t, store, id, cred, "C-X")
			if coarsePhaseOf(row) != tt.wantPhase || buildStatusOf(row) != tt.wantBuild {
				t.Fatalf("phase/build = %v/%v, want %v/%v", coarsePhaseOf(row), buildStatusOf(row), tt.wantPhase, tt.wantBuild)
			}
			if row.CompletedAt == nil {
				t.Fatal("a terminal outcome stamps CompletedAt")
			}
		})
	}
}

// TestRecordOperatorNote_FoldsTheDeliveryStamp — the retired pair (RecordOperatorNote +
// RecordOperatorNoteDelivered) is one verb: recording a note already addressed to an
// attempt costs one commit, not two.
func TestRecordOperatorNote_FoldsTheDeliveryStamp(t *testing.T) {
	a, store, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)

	note := OperatorNoteInput{NoteID: "n1", Kind: NoteSendBack, Gate: "detailed_design", Text: "tighten it"}
	var err error
	if v, err = a.RecordOperatorNote(execRC(), id, v, NoActivityVersionExpectation, "C-X", note, "C-X:detailedDesign:2", cred, fwra.IdempotencyKey("k17")); err != nil {
		t.Fatalf("RecordOperatorNote: %v", err)
	}
	row := readConstruction(t, store, id, cred, "C-X")
	if len(row.OperatorNotes) != 1 {
		t.Fatalf("one note; got %d", len(row.OperatorNotes))
	}
	n := row.OperatorNotes[0]
	if n.DeliveredToAttemptID != "C-X:detailedDesign:2" || n.DeliveredAt == nil {
		t.Fatalf("the delivery stamp must ride the same commit: %+v", n)
	}
	if len(PendingOperatorNotes(row)) != 0 {
		t.Fatal("a note delivered at record time is not pending")
	}
	// An undelivered note is still the ordinary case, and still pending.
	if _, err = a.RecordOperatorNote(execRC(), id, v, NoActivityVersionExpectation, "C-X",
		OperatorNoteInput{NoteID: "n2", Kind: NoteRetry, Gate: "", Text: "retry it"}, "", cred, fwra.IdempotencyKey("k18")); err != nil {
		t.Fatalf("RecordOperatorNote (undelivered): %v", err)
	}
	if got := len(PendingOperatorNotes(readConstruction(t, store, id, cred, "C-X"))); got != 1 {
		t.Fatalf("pending notes = %d, want 1", got)
	}
}

// TestStageTaskOutput_StagesTheModelAndNamesWhereItLanded — staging writes the task's
// output and hands back the ref the caller needs to cite, carrying the resulting version
// INSIDE the ref (the contract dialect admits one result per operation).
func TestStageTaskOutput_StagesTheModelAndNamesWhereItLanded(t *testing.T) {
	a, store, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)

	env, err := EncodeModel(&MissionStatement{Vision: "v", Mission: "m"})
	if err != nil {
		t.Fatalf("EncodeModel: %v", err)
	}
	ref, err := a.StageTaskOutput(execRC(), id, v, NoActivityVersionExpectation, "C-X", "detailedDesign", "", env, cred, fwra.IdempotencyKey("k19"))
	if err != nil {
		t.Fatalf("StageTaskOutput: %v", err)
	}
	if ref.ActivityID != "C-X" || ref.TaskID != "detailedDesign" || ref.Version != v+1 {
		t.Fatalf("StagedRef = %+v, want C-X/detailedDesign at version %d", ref, v+1)
	}
	proj, err := store.ReadProject(execRC(), id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Mission.Status != ReviewAwaitingReview {
		t.Fatalf("the staged model must be awaiting review; got %v", proj.Mission.Status)
	}
	// An empty envelope stages nothing, and saying so is better than a version bump that
	// records no fact: construction's own output is staged on the branch by the agent and
	// recorded through RecordAttemptOutcome's evidence, not here.
	if _, err := a.StageTaskOutput(execRC(), id, ref.Version, NoActivityVersionExpectation, "C-X", "construction", "", ModelEnvelope{}, cred, fwra.IdempotencyKey("k20")); err == nil ||
		kindOfErr(err) != fwra.ContractMisuse {
		t.Fatalf("an empty envelope must be ContractMisuse; got %v", err)
	}
}

// TestCommitActivityArtifacts_AppendsProducedOnce — the commit half of the family is
// append-only and idempotent on what it names, like every other verb here.
func TestCommitActivityArtifacts_AppendsProducedOnce(t *testing.T) {
	a, store, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)

	in := CommitArtifactsInput{TaskID: TaskConstruction, Commit: "abc123", ApprovedBy: "system-architect", DraftedBy: "junior-developer",
		Artifacts: []ProducedArtifact{{Kind: "code", Title: "orders", Source: "server/internal/x", Produced: true}}}
	var err error
	if v, err = a.CommitActivityArtifacts(execRC(), id, v, NoActivityVersionExpectation, "C-X", in, cred, fwra.IdempotencyKey("k21")); err != nil {
		t.Fatalf("CommitActivityArtifacts: %v", err)
	}
	if _, err = a.CommitActivityArtifacts(execRC(), id, v, NoActivityVersionExpectation, "C-X", in, cred, fwra.IdempotencyKey("k22")); err != nil {
		t.Fatalf("CommitActivityArtifacts (re-issue): %v", err)
	}
	row := readConstruction(t, store, id, cred, "C-X")
	if len(row.Produced) != 1 {
		t.Fatalf("one artifact, appended once; got %d: %+v", len(row.Produced), row.Produced)
	}
	if row.Produced[0].Note == "" {
		t.Fatal("the commit's provenance must be recorded on what it committed")
	}
}

// TestAcknowledgeStaleBasis_IsScopedToAnActivity — the activity-scoped form is not a
// rename of the project-scoped one: it refuses an activity it has no row for, which is
// exactly what "scoped to an activity" has to mean if it means anything. The slot
// transition itself is the project-scoped verb's, reused verbatim and already covered by
// its own tests, so what is asserted here is the scoping and the guards.
func TestAcknowledgeStaleBasis_IsScopedToAnActivity(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)

	if _, err := a.AcknowledgeStaleBasis(execRC(), id, v, NoActivityVersionExpectation, "C-NOPE", KindMission, "unaffected", cred, fwra.IdempotencyKey("k25")); err == nil ||
		kindOfErr(err) != fwra.NotFound {
		t.Fatalf("an activity with no row must be NotFound; got %v", err)
	}
	// The slot guard still fires underneath: mission is not committed here, so the
	// acknowledgement has nothing to clear and says so rather than writing.
	if _, err := a.AcknowledgeStaleBasis(execRC(), id, v, NoActivityVersionExpectation, "C-X", KindMission, "unaffected", cred, fwra.IdempotencyKey("k26")); err == nil ||
		kindOfErr(err) != fwra.ContractMisuse {
		t.Fatalf("acknowledging a slot that is not committed must be ContractMisuse; got %v", err)
	}
}

// TestActivityExecutionVerbs_RefuseEmptyIdentifiers — `required` in the contract schema
// dialect is PRESENCE-only, so non-emptiness lives here, in the implementation
// (2026-08-13 contract-strictness ruling). Every verb guards its own ids.
func TestActivityExecutionVerbs_RefuseEmptyIdentifiers(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)
	k := fwra.IdempotencyKey("kx")

	for _, tt := range []struct {
		name string
		call func() error
	}{
		{"OpenActivity/activityID", func() error {
			_, err := a.OpenActivity(execRC(), id, v, NoActivityVersionExpectation, "", ActivityTypeService, TestVariantPlan, LifecyclePin{TypeKey: "service", AssetsVersion: "x"}, cred, k)
			return err
		}},
		{"OpenActivity/pin", func() error {
			_, err := a.OpenActivity(execRC(), id, v, NoActivityVersionExpectation, "C-Y", ActivityTypeService, TestVariantPlan, LifecyclePin{}, cred, k)
			return err
		}},
		{"StageTaskOutput/taskID", func() error {
			_, err := a.StageTaskOutput(execRC(), id, v, NoActivityVersionExpectation, "C-X", "", "", ModelEnvelope{Kind: KindMission}, cred, k)
			return err
		}},
		{"RecordAttemptOutcome/attemptID", func() error {
			_, err := a.RecordAttemptOutcome(execRC(), id, v, NoActivityVersionExpectation, "C-X", TaskAttemptInput{TaskID: TaskDetailedDesign, Attempt: 1}, cred, k)
			return err
		}},
		{"RecordAttemptOutcome/taskID", func() error {
			_, err := a.RecordAttemptOutcome(execRC(), id, v, NoActivityVersionExpectation, "C-X", TaskAttemptInput{AttemptID: "a", Attempt: 1}, cred, k)
			return err
		}},
		{"OpenReviewRound/roundID", func() error {
			_, err := a.OpenReviewRound(execRC(), id, v, NoActivityVersionExpectation, "C-X", ReviewRoundInput{TaskID: TaskDesignReview, Reviews: TaskDetailedDesign, Round: 1}, cred, k)
			return err
		}},
		{"OpenReviewRound/subjectRef", func() error {
			_, err := a.OpenReviewRound(execRC(), id, v, NoActivityVersionExpectation, "C-X", ReviewRoundInput{RoundID: "r", TaskID: TaskDesignReview, Reviews: TaskDetailedDesign, Round: 1}, cred, k)
			return err
		}},
		{"AppendReviewVerdict/roundID", func() error {
			_, err := a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "", ReviewVerdict{ReviewerRole: "architect", Actor: "a", Verdict: VerdictApprove, AttemptID: "x"}, nil, nil, cred, k)
			return err
		}},
		{"AppendReviewVerdict/reviewerRole", func() error {
			_, err := a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "r", ReviewVerdict{Actor: "a", Verdict: VerdictApprove, AttemptID: "x"}, nil, nil, cred, k)
			return err
		}},
		{"SetReviewCommentStatus/commentID", func() error {
			_, err := a.SetReviewCommentStatus(execRC(), id, v, NoActivityVersionExpectation, "C-X", "r", "", ReviewCommentResolved, cred, k)
			return err
		}},
		{"SetReviewCommentStatus/status", func() error {
			_, err := a.SetReviewCommentStatus(execRC(), id, v, NoActivityVersionExpectation, "C-X", "r", "c", "banana", cred, k)
			return err
		}},
		{"DecideReviewRound/decidedBy", func() error {
			_, err := a.DecideReviewRound(execRC(), id, v, NoActivityVersionExpectation, "C-X", "r", RoundPassed, "", cred, k)
			return err
		}},
		{"CommitActivityArtifacts/taskID", func() error {
			_, err := a.CommitActivityArtifacts(execRC(), id, v, NoActivityVersionExpectation, "C-X", CommitArtifactsInput{ApprovedBy: "a", DraftedBy: "d"}, cred, k)
			return err
		}},
		{"RecordActivityOutcome/activityID", func() error {
			_, err := a.RecordActivityOutcome(execRC(), id, v, NoActivityVersionExpectation, "", ActivityOutcomeCompleted, FailureReasonUnknown, "", cred, k)
			return err
		}},
		{"RecordOperatorNote/noteID", func() error {
			_, err := a.RecordOperatorNote(execRC(), id, v, NoActivityVersionExpectation, "C-X", OperatorNoteInput{Kind: NoteRetry, Text: "t"}, "", cred, k)
			return err
		}},
		{"AcknowledgeStaleBasis/note", func() error {
			_, err := a.AcknowledgeStaleBasis(execRC(), id, v, NoActivityVersionExpectation, "C-X", KindMission, "  ", cred, k)
			return err
		}},
		{"ReadActivityExecution/activityID", func() error {
			_, err := a.ReadActivityExecution(execRC(), id, "")
			return err
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil {
				t.Fatal("an empty or unknown required identifier must be refused, not written")
			}
			if got := kindOfErr(err); got != fwra.ContractMisuse {
				t.Fatalf("error class = %v, want ContractMisuse (err=%v)", got, err)
			}
		})
	}
}

// TestReadActivityExecution_IsNotFoundForAnUnopenedActivity — the narrow read says
// "there is no such activity" rather than handing back an empty pair of ledgers that
// reads as "this activity did nothing".
func TestReadActivityExecution_IsNotFoundForAnUnopenedActivity(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	_ = openTestActivity(t, a, id, v, cred)

	if _, err := a.ReadActivityExecution(execRC(), id, "C-NOPE"); err == nil ||
		kindOfErr(err) != fwra.NotFound {
		t.Fatalf("an unopened activity must be NotFound; got %v", err)
	}
}

// ---- Fix round 1: the findings the review reproduced -----------------------

// TestAppendReviewVerdict_KeepsEveryReviewersComments — a round has a ROSTER, and the
// artifact ledger's batch-indexed id minting drops the second reviewer's first comment
// onto the first reviewer's r1c1 and silently skips it. A review ledger that loses a
// required reviewer's only comment is worse than no ledger.
func TestAppendReviewVerdict_KeepsEveryReviewersComments(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)
	v = openRoundFixture(t, a, id, v, cred)

	var err error
	if v, err = a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1",
		ReviewVerdict{ReviewerRole: "architect", Actor: "system-architect", Verdict: VerdictSendBack,
			Summary: "too wide", AttemptID: "C-X:detailedDesign:1"},
		[]ReviewComment{{Anchor: "ops[3]", Text: "split this op", AuthorRole: "architect", Type: "changeRequest"}},
		nil, cred, fwra.IdempotencyKey("m1")); err != nil {
		t.Fatalf("AppendReviewVerdict (architect): %v", err)
	}
	if _, err = a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1",
		ReviewVerdict{ReviewerRole: "qaEngineer", Actor: "qa-engineer", Verdict: VerdictSendBack,
			Summary: "no test plan", AttemptID: "C-X:detailedDesign:1"},
		[]ReviewComment{{Anchor: "ops[7]", Text: "where is the failure path tested", AuthorRole: "qaEngineer", Type: "changeRequest"}},
		nil, cred, fwra.IdempotencyKey("m2")); err != nil {
		t.Fatalf("AppendReviewVerdict (qa): %v", err)
	}

	exec, _ := a.ReadActivityExecution(execRC(), id, "C-X")
	r := exec.Reviews[0]
	if len(r.Verdicts) != 2 {
		t.Fatalf("both reviewers' verdicts must be recorded; got %d", len(r.Verdicts))
	}
	if len(r.Thread) != 2 {
		t.Fatalf("both reviewers' comments must survive; got %d: %+v", len(r.Thread), r.Thread)
	}
	if r.Thread[0].ID == r.Thread[1].ID {
		t.Fatalf("two comments, two ids; both are %q", r.Thread[0].ID)
	}
	roles := map[string]string{r.Thread[0].AuthorRole: r.Thread[0].Text, r.Thread[1].AuthorRole: r.Thread[1].Text}
	if roles["architect"] != "split this op" || roles["qaEngineer"] != "where is the failure path tested" {
		t.Fatalf("each comment must keep its own author and text: %+v", roles)
	}
}

// TestAppendReviewVerdict_AReissuedBatchStillAppendsOnce — the round mints ids from the
// thread it lands on, so it cannot dedup on the minted id the way the artifact ledger
// does. It dedups on CONTENT instead, which has to hold under a fresh idempotency key —
// that is what makes a Temporal retry of the same batch a no-op.
func TestAppendReviewVerdict_AReissuedBatchStillAppendsOnce(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)
	v = openRoundFixture(t, a, id, v, cred)

	batch := []ReviewComment{
		{Anchor: "ops[3]", Text: "split this op", AuthorRole: "architect", Type: "changeRequest"},
		{Anchor: "ops[4]", Text: "and name the failure", AuthorRole: "architect", Type: "changeRequest"},
	}
	verdict := ReviewVerdict{ReviewerRole: "architect", Actor: "system-architect", Verdict: VerdictSendBack,
		Summary: "two things", AttemptID: "C-X:detailedDesign:1"}
	var err error
	if v, err = a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1", verdict, batch, nil, cred, fwra.IdempotencyKey("m3")); err != nil {
		t.Fatalf("AppendReviewVerdict: %v", err)
	}
	if _, err = a.AppendReviewVerdict(execRC(), id, v, NoActivityVersionExpectation, "C-X", "C-X:designReview:1", verdict, batch, nil, cred, fwra.IdempotencyKey("m4")); err != nil {
		t.Fatalf("AppendReviewVerdict (re-issue under a fresh key): %v", err)
	}
	exec, _ := a.ReadActivityExecution(execRC(), id, "C-X")
	if got := len(exec.Reviews[0].Thread); got != 2 {
		t.Fatalf("a re-issued batch appends once; thread holds %d: %+v", got, exec.Reviews[0].Thread)
	}
	if len(exec.Reviews[0].Verdicts) != 1 {
		t.Fatalf("and so does its verdict; got %d", len(exec.Reviews[0].Verdicts))
	}
}

// TestOpenActivity_PinsTheLifecycleOnce — the pin names the lifecycle the
// ledger was written under. A re-open that quietly re-pinned would retro-date every
// attempt and round already recorded to a DAG they were never written against.
func TestOpenActivity_PinsTheLifecycleOnce(t *testing.T) {
	a, store, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)

	// Re-opening with the SAME pin is the ordinary retry: a no-op success.
	v2, err := a.OpenActivity(execRC(), id, v, NoActivityVersionExpectation, "C-X", ActivityTypeService, TestVariantPlan,
		LifecyclePin{TypeKey: "service", AssetsVersion: "v0.9.0"}, cred, fwra.IdempotencyKey("m5"))
	if err != nil {
		t.Fatalf("re-opening with the same pin must succeed: %v", err)
	}
	if got := readConstruction(t, store, id, cred, "C-X").Pin; got == nil || got.AssetsVersion != "v0.9.0" {
		t.Fatalf("pin = %+v, want the original", got)
	}
	// A DIFFERENT pin is refused, and the message names both so the caller can see which
	// release moved under it.
	_, err = a.OpenActivity(execRC(), id, v2, NoActivityVersionExpectation, "C-X", ActivityTypeService, TestVariantPlan,
		LifecyclePin{TypeKey: "service", AssetsVersion: "v0.10.0"}, cred, fwra.IdempotencyKey("m6"))
	if err == nil || kindOfErr(err) != fwra.ContractMisuse {
		t.Fatalf("re-pinning must be ContractMisuse; got %v", err)
	}
	if !strings.Contains(err.Error(), "v0.9.0") || !strings.Contains(err.Error(), "v0.10.0") {
		t.Fatalf("the refusal must name both pins; got %v", err)
	}
	if got := readConstruction(t, store, id, cred, "C-X").Pin; got.AssetsVersion != "v0.9.0" {
		t.Fatalf("a refused re-pin must not have written; pin = %+v", got)
	}
}

// TestOpenActivity_RefusesToResurrectAFinishedActivity — a terminal row's ledgers are the
// record of a finished activity. Re-opening it in place would leave Running and Done the
// same row with nothing saying which came first, and the completion stamp would describe
// a run that is notionally still going.
func TestOpenActivity_RefusesToResurrectAFinishedActivity(t *testing.T) {
	for _, tt := range []struct {
		name    string
		outcome ActivityOutcome
		reason  FailureReason
	}{
		{"done", ActivityOutcomeCompleted, FailureReasonUnknown},
		{"failed", ActivityOutcomeUnknown, PipelineFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, store, id, v, cred := newExecutionStore(t)
			v = openTestActivity(t, a, id, v, cred)
			v, err := a.RecordActivityOutcome(execRC(), id, v, NoActivityVersionExpectation, "C-X", tt.outcome, tt.reason, "d", cred, fwra.IdempotencyKey("m7"))
			if err != nil {
				t.Fatalf("RecordActivityOutcome: %v", err)
			}
			before := readConstruction(t, store, id, cred, "C-X")

			_, err = a.OpenActivity(execRC(), id, v, NoActivityVersionExpectation, "C-X", ActivityTypeService, TestVariantPlan,
				LifecyclePin{TypeKey: "service", AssetsVersion: "v0.9.0"}, cred, fwra.IdempotencyKey("m8"))
			if err == nil || kindOfErr(err) != fwra.Conflict {
				t.Fatalf("re-opening an exited activity must be a Conflict; got %v", err)
			}
			after := readConstruction(t, store, id, cred, "C-X")
			if coarsePhaseOf(after) != coarsePhaseOf(before) || buildStatusOf(after) != buildStatusOf(before) || after.CompletedAt == nil {
				t.Fatalf("the terminal row must be untouched: before=%v/%v after=%v/%v",
					coarsePhaseOf(before), buildStatusOf(before), coarsePhaseOf(after), buildStatusOf(after))
			}
		})
	}
}

// TestOpenReviewRound_StampsObservedProvenance — a round carries where its record came
// from, in the same closed vocabulary an attempt carries. Without it a round the
// migration reconstructed reads exactly like one a reviewer actually cast.
func TestOpenReviewRound_StampsObservedProvenance(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)
	v = openTestActivity(t, a, id, v, cred)
	_ = openRoundFixture(t, a, id, v, cred)

	exec, _ := a.ReadActivityExecution(execRC(), id, "C-X")
	p := exec.Reviews[0].Provenance
	if p.Origin != OriginObserved {
		t.Fatalf("a live write is observed; got %q", p.Origin)
	}
	if p.GeneratedAt == nil {
		t.Fatal("provenance must carry when the record was produced")
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("the stamped provenance must satisfy the shared rule: %v", err)
	}
}

// TestReviewRound_RoundTripsThroughCodecCarryingEveryMember — the round is a new member
// of the stored aggregate, so the codec has to carry ALL of it: a field the encoder drops
// vanishes without a trace, and comparing decoded values cannot see the loss.
func TestReviewRound_RoundTripsThroughCodecCarryingEveryMember(t *testing.T) {
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	p := Project{ID: "p", Version: 1}
	p.ActivityExecution = map[string]ActivityExecution{"C-X": {
		ActivityID: "C-X",
		Type:       ActivityTypeService,
		Pin:        &LifecyclePin{TypeKey: "service", AssetsVersion: "v0.9.0"},
		Reviews: []ReviewRound{{
			RoundID: "C-X:designReview:1", TaskID: TaskDesignReview, Reviews: TaskDetailedDesign, Round: 1,
			SubjectRef: SubjectRef{Kind: SubjectCommit, Ref: "deadbeef"},
			Reviewers:  []RoundReviewer{{Role: "architect", Actor: "system-architect", Required: true}},
			Verdicts: []ReviewVerdict{{ReviewerRole: "architect", Actor: "system-architect",
				Verdict: VerdictAbstain, Summary: "not mine to judge", AttemptID: "C-X:detailedDesign:1", At: at.Format(time.RFC3339)}},
			Thread: []ReviewComment{{ID: "r1c1", Anchor: "ops[0]", AnchorText: "Op", Text: "split",
				AuthorRole: "architect", Round: 1, Status: ReviewCommentOpen, Replies: []ReviewCommentReply{}, Type: "changeRequest"}},
			Outcome: RoundSentBack, DecidedBy: "system-architect",
			OpenedAt: at.Format(time.RFC3339), DecidedAt: at.Format(time.RFC3339),
			Provenance: AttemptProvenance{Origin: OriginBackfilled, Generator: "cmd/migrate-activity-execution@abc1234",
				GeneratedAt: &at, Basis: "operatorNotes[0]"},
		}},
	}}

	raw, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, ok, err := DecodeProjectJSON(raw, "p")
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	reEncoded, err := EncodeProjectJSON(got)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	lost, err := CodecCarriesEveryMember(raw, reEncoded)
	if err != nil {
		t.Fatalf("CodecCarriesEveryMember: %v", err)
	}
	if len(lost) != 0 {
		t.Fatalf("the codec drops %v", lost)
	}
	r := got.ActivityExecution["C-X"].Reviews[0]
	if r.Provenance.Origin != OriginBackfilled || r.Provenance.Basis != "operatorNotes[0]" {
		t.Fatalf("provenance must survive the round-trip: %+v", r.Provenance)
	}
	if r.Verdicts[0].Verdict != VerdictAbstain || r.Outcome != RoundSentBack || r.DecidedAt == "" {
		t.Fatalf("the round's own members must survive: %+v", r)
	}
	if pin := got.ActivityExecution["C-X"].Pin; pin == nil || pin.TypeKey != "service" {
		t.Fatalf("the lifecycle pin must survive: %+v", pin)
	}
}

// legacyActivityConstructionDocJSON is a project.json document carrying ONE construction
// row in the shape every committed row is stored in today: the two append-only ledgers
// and the head facts, PLUS the five members spec §5.3 rules derived — phase, phases,
// currentPhase, kind, buildStatus. It is the input to the wave's one wire break.
const legacyActivityConstructionDocJSON = `{
  "id": "p-legacy",
  "version": 7,
  "phase": 2,
  "owner": "o",
  "name": "Legacy",
  "research": {},
  "slots": {},
  "activityConstruction": {
    "C-X": {
      "activityID": "C-X",
      "type": 2,
      "kind": 2,
      "variant": 1,
      "phase": 2,
      "phases": [
        {"phase": "requirements", "weight": 15, "completed": true, "label": "Requirements"},
        {"phase": "construction", "weight": 40, "completed": true, "label": "Construction"}
      ],
      "currentPhase": "construction",
      "buildStatus": 1,
      "startedAt": "2026-09-01T10:00:00Z",
      "completedAt": "2026-09-02T10:00:00Z",
      "failureDetail": "the pipeline gave up",
      "failureReason": 2,
      "attempts": [
        {"attemptId": "C-X:srs:1", "task": "srs", "phase": "requirements", "attempt": 1,
         "outcome": "passed", "evidence": {}, "provenance": {"origin": "observed"}}
      ],
      "produced": [{"Kind": "code", "Title": "T", "Source": "s", "Produced": true, "Note": "n"}],
      "operatorNotes": [{"noteId": "n1", "kind": 1, "gate": "construction", "text": "redo",
                         "recordedAt": "2026-09-01T11:00:00Z"}],
      "lifecyclePin": {"typeKey": "testing", "assetsVersion": "v0.9.0"}
    }
  },
  "reviewPolicy": {},
  "updatedAt": "2026-09-02T10:00:00Z"
}`

// TestActivityExecution_LegacyRowDecodesAndTheCodecLosesNothing is the wave's ONE wire
// break, pinned. A legacy document's activityConstruction member must decode into
// ActivityExecution (task 9 rewrites it; until then, production reads it), and the codec
// must not DROP anything on the way back out except the five members spec §5.3 rules
// DERIVED — the half no equality check can supply (CodecCarriesEveryMember's own doc).
//
// The comparison is rooted at the ROW MAP rather than at the document, because the member
// itself moved: compared whole-document, collectDroppedMembers would report the single
// path "activityConstruction" and say nothing about what the rows inside it lost, which
// is the question this test exists to answer.
func TestActivityExecution_LegacyRowDecodesAndTheCodecLosesNothing(t *testing.T) {
	p, ok, err := DecodeProjectJSON([]byte(legacyActivityConstructionDocJSON), "p-legacy")
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if _, held := p.ActivityExecution["C-X"]; !held {
		t.Fatalf("the legacy member must decode into the new one, got %+v", p.ActivityExecution)
	}
	reEncoded, err := EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	lost, err := CodecCarriesEveryMember(
		rowMapMember(t, []byte(legacyActivityConstructionDocJSON), "activityConstruction"),
		rowMapMember(t, reEncoded, "activityExecution"))
	if err != nil {
		t.Fatalf("CodecCarriesEveryMember: %v", err)
	}
	// The DERIVED members are the only permitted losses, and they are named, not a count.
	want := []string{"C-X.buildStatus", "C-X.currentPhase", "C-X.kind", "C-X.phase", "C-X.phases"}
	if !slices.Equal(lost, want) {
		t.Fatalf("codec losses:\n got %v\nwant %v", lost, want)
	}
}

// rowMapMember lifts one top-level member out of an encoded project document.
func rowMapMember(t *testing.T, doc []byte, member string) json.RawMessage {
	t.Helper()
	var held map[string]json.RawMessage
	if err := json.Unmarshal(doc, &held); err != nil {
		t.Fatalf("read the document: %v", err)
	}
	raw, ok := held[member]
	if !ok {
		names := make([]string, 0, len(held))
		for k := range held {
			names = append(names, k)
		}
		slices.Sort(names)
		t.Fatalf("the document carries no %q member; it holds %v", member, names)
	}
	return raw
}

// ---- Test-only derivations of the members the row stopped storing ------------------
//
// Stage-3 task 4 removed phase / phases / currentPhase / kind / buildStatus from the
// stored row: they are DERIVED from the two ledgers and the head facts now (spec §5.3).
// A test that used to read one off the row reads it through the SAME derivation
// production uses, so these helpers are the read path, not a shim around it. They pass
// the zero ActivityItem because the assertions below are about the row, not about a
// committed plan item — which is exactly the case the derivation answers from the head
// facts alone.

func coarsePhaseOf(s ActivityExecution) ActivityConstructionPhase {
	phase, _ := EffectiveConstructionPhase(s, ActivityItem{})
	return phase
}

func buildStatusOf(s ActivityExecution) ActivityBuildStatus {
	_, build := EffectiveConstructionPhase(s, ActivityItem{})
	return build
}

// resolvedOf is the row's profile-ordered phase set, resolved from its ledger against the
// type it carries — what the stored Phases slice used to approximate.
func resolvedOf(s ActivityExecution) []PhaseCompletion {
	return ResolvePhaseCompletions(ProfileFor(s.Type, s.Variant), s.Attempts)
}

// completedPhasesOf names the lifecycle phases the row's ledger resolves complete.
func completedPhasesOf(s ActivityExecution) []ActivityMethodPhase {
	var out []ActivityMethodPhase
	for _, pc := range resolvedOf(s) {
		if pc.Completed {
			out = append(out, pc.Phase)
		}
	}
	return out
}

// ---- The per-activity Version (task 4, step 3) -------------------------------------

// TestWithActivityVersion_RefusesAStaleExpectationAndStampsTheCounter pins the
// per-activity optimistic check at the UNIT level: the wrapper itself, over an in-memory
// Project, with no store or credential in the way. The two tests below it drive the same
// rule through a real verb on a real store, which is the arming stage 4a did; this one
// keeps the rule readable in one screen.
//
// Three properties, one test, because they are one rule: a stale expectation is a
// Conflict naming BOTH versions; a refused transition leaves the row — and its counter —
// exactly as it found it; an applied transition advances the counter by one.
func TestWithActivityVersion_RefusesAStaleExpectationAndStampsTheCounter(t *testing.T) {
	newProject := func() *Project {
		return &Project{ActivityExecution: map[string]ActivityExecution{
			"C-X": {ActivityID: "C-X", Version: 7},
		}}
	}
	noop := func(*ActivityExecution) error { return nil }

	p := newProject()
	err := withActivityVersion("RecordAttemptOutcome", "C-X", 3, noop)(p)
	if err == nil {
		t.Fatal("a stale per-activity version must be refused")
	}
	if got := kindOf(t, err); got != fwra.Conflict {
		t.Fatalf("kind = %v, want Conflict — the caller resolves it by re-reading", got)
	}
	if !strings.Contains(err.Error(), "7") || !strings.Contains(err.Error(), "3") {
		t.Fatalf("the refusal must name BOTH versions, got: %v", err)
	}
	if p.ActivityExecution["C-X"].Version != 7 {
		t.Fatalf("a refused transition must not advance the counter, got %d", p.ActivityExecution["C-X"].Version)
	}

	p = newProject()
	if err := withActivityVersion("RecordAttemptOutcome", "C-X", 7, noop)(p); err != nil {
		t.Fatalf("the held version must be accepted: %v", err)
	}
	if got := p.ActivityExecution["C-X"].Version; got != 8 {
		t.Fatalf("version = %d, want 8 — an applied transition stamps the counter", got)
	}

	// The honest no-op guard: a caller with no version to assert passes 0 and the
	// transition still applies and still stamps.
	p = newProject()
	if err := withActivityVersion("RecordAttemptOutcome", "C-X", NoActivityVersionExpectation, noop)(p); err != nil {
		t.Fatalf("no expectation must not refuse: %v", err)
	}
	if got := p.ActivityExecution["C-X"].Version; got != 8 {
		t.Fatalf("version = %d, want 8", got)
	}

	// And a transition that fails leaves the counter alone, exactly as a refused one does.
	p = newProject()
	boom := errors.New("the transition refused")
	if err := withActivityVersion("RecordAttemptOutcome", "C-X", 7, func(*ActivityExecution) error { return boom })(p); !errors.Is(err, boom) {
		t.Fatalf("the transition's own error must surface, got %v", err)
	}
	if got := p.ActivityExecution["C-X"].Version; got != 7 {
		t.Fatalf("version = %d, want 7 — a failed transition stamps nothing", got)
	}
}

// TestActivityExecutionRefusesAStaleActivityVersion drives the armed guard through a REAL
// verb on a REAL store, which is what stage 4a changed: the callers can fill the parameter
// now, so a stale one is refused instead of compared against a fabricated zero.
//
// A stale expectation is Conflict — the same class the git ref-CAS loss carries, because
// it is the same "someone already moved this" the caller resolves by re-reading — and it
// names both versions and says what to do. This is the guard that makes parallel children
// safe: two writers on the SAME activity cannot interleave, while two children on
// DIFFERENT activities never contend at all (the project-level CAS alone would have made
// them).
//
// BOTH shapes of guarded verb are driven, because the facet has two and only one of them
// would be caught by testing the other. A STAMPING verb runs inside withActivityVersion
// and advances the row's counter; AcknowledgeStaleBasis calls the same check explicitly
// and then writes the SLOT, so it honours the expectation while advancing nothing — and a
// guard that is only checked and never stamped is exactly the guard a refactor deletes
// without a single test noticing.
func TestActivityExecutionRefusesAStaleActivityVersion(t *testing.T) {
	cases := []struct {
		name string
		// seed prepares whatever the verb needs beyond an opened activity and returns the
		// project version to write from.
		seed func(t *testing.T, store *GitStore, id ProjectID, v Version, cred RepoCredential) Version
		// apply runs the verb ONCE with the per-activity version its caller is holding.
		apply func(a ActivityExecutionAccess, id ProjectID, v Version, held int64, cred RepoCredential, key string) (Version, error)
		// stamps is whether an APPLIED transition advances the row's own counter.
		stamps bool
	}{
		{
			name: "RecordAttemptOutcome",
			seed: func(_ *testing.T, _ *GitStore, _ ProjectID, v Version, _ RepoCredential) Version { return v },
			apply: func(a ActivityExecutionAccess, id ProjectID, v Version, held int64, cred RepoCredential, key string) (Version, error) {
				return a.RecordAttemptOutcome(execRC(), id, v, held, "C-X", TaskAttemptInput{
					AttemptID: key, TaskID: TaskSRS, Attempt: 1, Outcome: OutcomePassed,
				}, cred, fwra.IdempotencyKey(key))
			},
			stamps: true,
		},
		{
			// The slot the acknowledgement clears has to be committed AND stale for the
			// transition to write anything, or "a fresh expectation applies" would be
			// indistinguishable from the slot guard refusing underneath it.
			name: "AcknowledgeStaleBasis",
			seed: seedStaleGlossary,
			apply: func(a ActivityExecutionAccess, id ProjectID, v Version, held int64, cred RepoCredential, key string) (Version, error) {
				return a.AcknowledgeStaleBasis(execRC(), id, v, held, "C-X", KindGlossary, "no term changes", cred, fwra.IdempotencyKey(key))
			},
			stamps: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, store, id, v, cred := newExecutionStore(t)
			v = openTestActivity(t, a, id, v, cred)
			v = c.seed(t, store, id, v, cred)
			held := verbRowVersion(t, a, id, "C-X")

			v, err := c.apply(a, id, v, held, cred, "k-fresh")
			if err != nil {
				t.Fatalf("a fresh expectation must apply: %v", err)
			}
			after := verbRowVersion(t, a, id, "C-X")
			switch {
			case c.stamps && after != held+1:
				t.Fatalf("an applied transition advances the counter: version = %d, want %d", after, held+1)
			case !c.stamps && after != held:
				t.Fatalf("this verb writes the slot, not the row: version = %d, want %d", after, held)
			}

			// A STAMPING verb has left `held` one behind — the value a second child would
			// still be holding. A non-stamping one has not moved the row at all, so the
			// stale number has to be fabricated to say the same thing: "I read this
			// somewhere this write is not going."
			stale := held
			if !c.stamps {
				stale = held + 1
			}
			if _, err = c.apply(a, id, v, stale, cred, "k-stale"); err == nil {
				t.Fatal("a stale per-activity version must be refused")
			}
			if got := kindOf(t, err); got != fwra.Conflict {
				t.Fatalf("kind = %v, want Conflict — the caller resolves it by re-reading", got)
			}
			if !strings.Contains(err.Error(), "re-read the activity and re-apply") {
				t.Errorf("the Conflict must tell the caller what to do, got %q", err.Error())
			}
			if got := verbRowVersion(t, a, id, "C-X"); got != after {
				t.Fatalf("a refused write must leave the row where it found it: version = %d, want %d", got, after)
			}
		})
	}
}

// seedStaleGlossary commits Mission then Glossary and AMENDS Mission, which is what makes
// the committed Glossary stale — the only state in which an acknowledgement has anything
// to clear. Returns the project version after the amend.
func seedStaleGlossary(t *testing.T, store *GitStore, id ProjectID, v Version, cred RepoCredential) Version {
	t.Helper()
	ctx := context.Background()
	stageCommit := func(v Version, kind ArtifactKind, model ArtifactModel, tag string) Version {
		staged, err := store.StageArtifactForReviewOnBranch(ctx, id, v, "", model, cred, fwra.IdempotencyKey("wf:stage:"+tag))
		if err != nil {
			t.Fatalf("stage %s: %v", tag, err)
		}
		committed, err := store.CommitArtifact(ctx, id, staged, kind, cred, fwra.IdempotencyKey("wf:commit:"+tag))
		if err != nil {
			t.Fatalf("commit %s: %v", tag, err)
		}
		return committed
	}
	v = stageCommit(v, KindMission, &MissionStatement{Vision: "v1", Mission: "m1"}, "mission1")
	v = stageCommit(v, KindGlossary, &Glossary{}, "glossary1")
	v = stageCommit(v, KindMission, &MissionStatement{Vision: "v2", Mission: "m2"}, "mission2")
	if !readProject(t, store, id, cred).Glossary.StaleBasis {
		t.Fatal("precondition: the Glossary must be stale after the Mission amend")
	}
	return v
}

// TestActivityExecutionAcceptsTheUnreadPosture is the other half of the rule.
// NoActivityVersionExpectation is still honoured, and it is not a loophole: it is the
// posture of a writer that has not read the row — OpenActivity on a BIRTH, and a tool
// writing history it never read. Every workflow caller reads the row from the project read
// it already makes, so every workflow caller passes a real number.
//
// A BIRTH is the one case where the unread posture is the ONLY admissible one, so that is
// asserted here too: a caller claiming to hold version 3 of a row this store does not have
// read it somewhere this write is not going.
func TestActivityExecutionAcceptsTheUnreadPosture(t *testing.T) {
	a, _, id, v, cred := newExecutionStore(t)

	if _, err := a.OpenActivity(execRC(), id, v, 3, "C-X", ActivityTypeService, TestVariantPlan,
		LifecyclePin{TypeKey: "service", AssetsVersion: "v0.9.0"}, cred, "k-phantom"); err == nil {
		t.Fatal("a held version for a row that does not exist yet must be refused")
	} else if got := kindOf(t, err); got != fwra.Conflict {
		t.Fatalf("kind = %v, want Conflict", got)
	}

	v = openTestActivity(t, a, id, v, cred) // the birth itself passes the unread posture
	if _, err := a.RecordAttemptOutcome(execRC(), id, v, NoActivityVersionExpectation, "C-X", TaskAttemptInput{
		AttemptID: AttemptID("C-X", TaskSRS, 1), TaskID: TaskSRS, Attempt: 1, Outcome: OutcomePassed,
	}, cred, "k-unread"); err != nil {
		t.Fatalf("the unread posture must still apply: %v", err)
	}
}

// TestEveryMutatingVerbOnARowStampsItsVersion pins ActivityExecution.Version's own claim:
// EVERY transition on a row advances it. It loops BOTH rails — the retired facet's verbs
// (which write through upsertActivityExecution) and activityExecutionAccess's (which write
// through withActivityVersion) — because both write these rows for the length of this
// wave, and a counter only one rail stamped would stand still across a real write and
// tell a reader nothing happened.
//
// A test that claims "every" and checks one verb is worse than no test: it reports the
// property as held while nine writers quietly do not hold it. Each case runs against its
// OWN freshly opened store, so a verb's stamp is read in isolation.
func TestEveryMutatingVerbOnARowStampsItsVersion(t *testing.T) {
	const activity = "C-X"
	note := OperatorNoteInput{NoteID: "n1", Kind: NoteSendBack, Gate: "construction", Text: "redo"}

	cases := []struct {
		name string
		// apply runs ONE mutating verb against an already-opened row.
		apply func(t *testing.T, a ActivityExecutionAccess, store *GitStore, id ProjectID, v Version, cred RepoCredential)
	}{
		// ---- the retired facet's verbs (upsertActivityExecution) ----
		{"RecordChangeReviewed", func(t *testing.T, _ ActivityExecutionAccess, store *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(store.RecordChangeReviewed(execRC(), id, v, activity, cred, "k-cr")).must(t)
		}},
		{"RecordActivityExited", func(t *testing.T, _ ActivityExecutionAccess, store *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(store.RecordActivityExited(execRC(), id, v, activity, ActivityOutcomeCompleted, cred, "k-ex")).must(t)
		}},
		{"RecordActivityFailed", func(t *testing.T, _ ActivityExecutionAccess, store *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(store.RecordActivityFailed(execRC(), id, v, activity, PipelineFailed, "detail", cred, "k-fail")).must(t)
		}},
		{"RecordOperatorNote", func(t *testing.T, _ ActivityExecutionAccess, store *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(store.RecordOperatorNote(execRC(), id, v, activity, note, cred, "k-note")).must(t)
		}},
		{"RecordOperatorNoteDelivered", func(t *testing.T, _ ActivityExecutionAccess, store *GitStore, id ProjectID, v Version, cred RepoCredential) {
			v2 := verbDone(store.RecordOperatorNote(execRC(), id, v, activity, note, cred, "k-note")).must(t)
			// Read the version the delivery starts from, so the assertion below is about
			// the DELIVERY's stamp and not about the note's.
			before := readConstruction(t, store, id, cred, activity).Version
			verbDone(store.RecordOperatorNoteDelivered(execRC(), id, v2, activity, note.NoteID, "C-X:srs:1", cred, "k-deliver")).must(t)
			if got := readConstruction(t, store, id, cred, activity).Version; got != before+1 {
				t.Fatalf("RecordOperatorNoteDelivered: version = %d, want %d", got, before+1)
			}
		}},
		{"RecordPhaseStarted", func(t *testing.T, _ ActivityExecutionAccess, store *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(store.RecordPhaseStarted(execRC(), id, v, activity, MethodPhaseRequirements, cred, "k-ps")).must(t)
		}},
		{"RecordPhaseCompleted", func(t *testing.T, _ ActivityExecutionAccess, store *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(store.RecordPhaseCompleted(execRC(), id, v, activity, MethodPhaseRequirements, "srs.md", cred, "k-pc")).must(t)
		}},
		{"RecordActivityStarted", func(t *testing.T, _ ActivityExecutionAccess, store *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(store.RecordActivityStarted(execRC(), id, v, activity, ActivityTypeService, TestVariantPlan, cred, "k-as")).must(t)
		}},
		{"RecordActivityCompleted", func(t *testing.T, _ ActivityExecutionAccess, store *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(store.RecordActivityCompleted(execRC(), id, v, activity, cred, "k-ac")).must(t)
		}},

		// ---- activityExecutionAccess's own (withActivityVersion) ----
		{"RecordAttemptOutcome", func(t *testing.T, a ActivityExecutionAccess, _ *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(a.RecordAttemptOutcome(execRC(), id, v, NoActivityVersionExpectation, activity, TaskAttemptInput{
				AttemptID: AttemptID(activity, TaskSRS, 1), TaskID: TaskSRS, Attempt: 1, Outcome: OutcomePassed,
			}, cred, "k-attempt")).must(t)
		}},
		{"OpenReviewRound", func(t *testing.T, a ActivityExecutionAccess, _ *GitStore, id ProjectID, v Version, cred RepoCredential) {
			openRoundFixture(t, a, id, v, cred)
		}},
		{"AppendReviewVerdict", func(t *testing.T, a ActivityExecutionAccess, _ *GitStore, id ProjectID, v Version, cred RepoCredential) {
			v2 := openRoundFixture(t, a, id, v, cred)
			before := verbRowVersion(t, a, id, activity)
			verbDone(a.AppendReviewVerdict(execRC(), id, v2, NoActivityVersionExpectation, activity, "C-X:designReview:1", ReviewVerdict{
				ReviewerRole: "architect", Actor: "system-architect", Verdict: VerdictApprove, AttemptID: "C-X:detailedDesign:1",
			}, nil, nil, cred, "k-verdict")).must(t)
			if got := verbRowVersion(t, a, id, activity); got != before+1 {
				t.Fatalf("AppendReviewVerdict: version = %d, want %d", got, before+1)
			}
		}},
		{"SetReviewCommentStatus", func(t *testing.T, a ActivityExecutionAccess, _ *GitStore, id ProjectID, v Version, cred RepoCredential) {
			v2 := openRoundFixture(t, a, id, v, cred)
			v3 := verbDone(a.AppendReviewVerdict(execRC(), id, v2, NoActivityVersionExpectation, activity, "C-X:designReview:1", ReviewVerdict{
				ReviewerRole: "architect", Actor: "system-architect", Verdict: VerdictSendBack, AttemptID: "C-X:detailedDesign:1",
			}, []ReviewComment{{Anchor: "ops[0]", Text: "split", AuthorRole: "architect"}}, nil, cred, "k-verdict")).must(t)
			before := verbRowVersion(t, a, id, activity)
			verbDone(a.SetReviewCommentStatus(execRC(), id, v3, NoActivityVersionExpectation, activity, "C-X:designReview:1", "r1c1", ReviewCommentResolved, cred, "k-status")).must(t)
			if got := verbRowVersion(t, a, id, activity); got != before+1 {
				t.Fatalf("SetReviewCommentStatus: version = %d, want %d", got, before+1)
			}
		}},
		{"DecideReviewRound", func(t *testing.T, a ActivityExecutionAccess, _ *GitStore, id ProjectID, v Version, cred RepoCredential) {
			v2 := openRoundFixture(t, a, id, v, cred)
			before := verbRowVersion(t, a, id, activity)
			verbDone(a.DecideReviewRound(execRC(), id, v2, NoActivityVersionExpectation, activity, "C-X:designReview:1", RoundPassed, "system-architect", cred, "k-decide")).must(t)
			if got := verbRowVersion(t, a, id, activity); got != before+1 {
				t.Fatalf("DecideReviewRound: version = %d, want %d", got, before+1)
			}
		}},
		{"CommitActivityArtifacts", func(t *testing.T, a ActivityExecutionAccess, _ *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(a.CommitActivityArtifacts(execRC(), id, v, NoActivityVersionExpectation, activity, CommitArtifactsInput{
				TaskID: TaskCodeReview, ApprovedBy: "system-architect", DraftedBy: "junior-developer",
				Artifacts: []ProducedArtifact{{Kind: "code", Title: "T", Source: "s"}},
			}, cred, "k-commit")).must(t)
		}},
		{"RecordActivityOutcome", func(t *testing.T, a ActivityExecutionAccess, _ *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(a.RecordActivityOutcome(execRC(), id, v, NoActivityVersionExpectation, activity, ActivityOutcomeCompleted, FailureReasonUnknown, "", cred, "k-outcome")).must(t)
		}},
		{"RecordOperatorNote (facet)", func(t *testing.T, a ActivityExecutionAccess, _ *GitStore, id ProjectID, v Version, cred RepoCredential) {
			verbDone(a.RecordOperatorNote(execRC(), id, v, NoActivityVersionExpectation, activity, note, "", cred, "k-facet-note")).must(t)
		}},
		{"AcknowledgeStaleBasis", func(_ *testing.T, a ActivityExecutionAccess, _ *GitStore, id ProjectID, v Version, cred RepoCredential) {
			// The activity-scoped slot transition: it guards on the row existing but writes
			// the SLOT, so it stamps no row version. It is one of TWO such verbs on the
			// facet — StageTaskOutput is the other, staging a task's output on the branch
			// through stageArtifactForReviewOnBranch and returning a StagedRef rather than
			// a Version, so it has no row transition to stamp and no place in this loop.
			// Named here rather than omitted, so both exceptions are recorded decisions,
			// and this one's error is deliberately unread — the slot it targets may not be
			// committed.
			_, _ = a.AcknowledgeStaleBasis(execRC(), id, v, NoActivityVersionExpectation, activity, KindSystem, "seen", cred, "k-ack")
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, store, id, v, cred := newExecutionStore(t)
			v = openTestActivity(t, a, id, v, cred)
			opened := readConstruction(t, store, id, cred, activity)
			if opened.Version != 1 {
				t.Fatalf("an opened row is at version 1, got %d", opened.Version)
			}
			c.apply(t, a, store, id, v, cred)
			after := readConstruction(t, store, id, cred, activity).Version
			if c.name == "AcknowledgeStaleBasis" {
				if after != opened.Version {
					t.Fatalf("AcknowledgeStaleBasis writes the slot, not the row: version = %d, want %d", after, opened.Version)
				}
				return
			}
			if after <= opened.Version {
				t.Fatalf("%s left the per-activity version at %d: every transition on the row advances it", c.name, after)
			}
		})
	}
}

// verbDone wraps a verb's (Version, error) pair so a test can assert on it in ONE
// expression: Go admits a multi-value call only as a function's SOLE argument, so
// `must(t, verb(...))` does not compile and `verbDone(verb(...)).must(t)` does.
type verbDoneResult struct {
	version Version
	err     error
}

func verbDone(v Version, err error) verbDoneResult { return verbDoneResult{v, err} }

func (r verbDoneResult) must(t *testing.T) Version {
	t.Helper()
	if r.err != nil {
		t.Fatalf("verb: %v", r.err)
	}
	return r.version
}

// verbRowVersion reads one row's per-activity version through the facet's own narrow read.
func verbRowVersion(t *testing.T, a ActivityExecutionAccess, id ProjectID, activityID string) int64 {
	t.Helper()
	row, err := a.ReadActivityExecution(execRC(), id, activityID)
	if err != nil {
		t.Fatalf("ReadActivityExecution: %v", err)
	}
	return row.Version
}

// TestLegacyIntegratedRow_StaysIntegratedThroughTheLedger is the other half of the
// tolerance's promise. An exit stamp alone reads as done-but-in-review, so a legacy row
// that stored BuildStatus == Integrated would have been quietly un-completed by the rename
// — and two consumers act on that: isConstructionComplete (the catalog's Operating signal)
// and the EV curve's integrated set. The claim is carried as the EVIDENCE the derivation
// reads, stamped backfilled so nothing can mistake it for a recorded gate.
func TestLegacyIntegratedRow_StaysIntegratedThroughTheLedger(t *testing.T) {
	item := ActivityItem{Name: "C-a", WorkerClass: "junior-developer", Coding: true}
	legacy := LegacyActivityConstructionRow{
		ActivityID:  "C-a",
		Phase:       LegacyPhaseDone,
		BuildStatus: BuildIntegrated,
		Phases:      ProfileFor(ActivityTypeService, TestVariantPlan).toPhaseCompletions(),
	}
	for i := range legacy.Phases {
		legacy.Phases[i].Completed = true
	}
	row := legacy.toActivityExecution()

	phase, build := EffectiveConstructionPhase(row, item)
	if phase != ActivityConstructionDone || build != BuildIntegrated {
		t.Fatalf("legacy Done+Integrated = (%v, %v), want (Done, Integrated)", phase, build)
	}
	for _, a := range row.Attempts {
		if a.Provenance.Origin != OriginBackfilled || a.Provenance.Basis != "legacy activityConstruction.phases" {
			t.Fatalf("a carried-forward gate must be stamped backfilled with its basis: %+v", a.Provenance)
		}
	}

	// And the catalog's Operating signal, which is the consumer that would have flipped.
	p := Project{
		Phase:             PhaseConstruction,
		ActivityList:      ArtifactSlot{Status: ReviewCommitted, Model: &ActivityList{Activities: []ActivityItem{item}}},
		ActivityExecution: map[string]ActivityExecution{"C-a": row},
	}
	if !isConstructionComplete(p) {
		t.Fatal("a legacy Integrated row must keep the project construction-complete")
	}
}

// A RECORDED rejection outranks a stored roll-up: the tolerance fills the gates the ledger
// has not decided, never the ones it has. Otherwise a legacy BuildStatus nobody can trace
// to a review would overwrite a review that actually happened.
func TestLegacyIntegratedRow_NeverOverridesARecordedGate(t *testing.T) {
	legacy := LegacyActivityConstructionRow{
		ActivityID:  "C-a",
		Type:        ActivityTypeService,
		Phase:       LegacyPhaseDone,
		BuildStatus: BuildIntegrated,
		Attempts:    []TaskAttempt{constructionAttempt("C-a", TaskCodeReview, 1, OutcomeRejected)},
	}
	row := legacy.toActivityExecution()
	latest, ok := latestAttempt(row.Attempts, TaskCodeReview)
	if !ok || latest.Outcome != OutcomeRejected {
		t.Fatalf("the recorded codeReview rejection must stand, got %+v", row.Attempts)
	}
	_, build := EffectiveConstructionPhase(row, ActivityItem{Name: "C-a", WorkerClass: "junior-developer", Coding: true})
	if build == BuildIntegrated {
		t.Fatal("a rejected construction gate must keep the row out of Integrated")
	}
}
