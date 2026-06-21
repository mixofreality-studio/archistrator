package projectstate_test

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

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// fixedClock is a deterministic, server-side clock so UpdatedAt is asserted exactly
// (proving the timestamp is server-resolved, not caller-minted).
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// newActivityStore spins a real local git store with a fixed clock and seeds a
// Phase-3 project so modeRequireExisting Record* verbs have a row to upsert.
func newActivityStore(t *testing.T, clk time.Time) (*ps.GitStore, ps.ProjectID, ps.Version, ps.RepoCredential, context.Context) {
	t.Helper()
	store, cred, ctx := newLocalGitStore(t)
	store = store.WithClock(fixedClock(clk))
	id := ps.ProjectID(uuid.NewString())
	v, err := store.CreateProject(ctx, id, "alice", "GitDemo", cred, "wf:create")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return store, id, v, cred, ctx
}

func readActivity(t *testing.T, store *ps.GitStore, ctx context.Context, id ps.ProjectID, cred ps.RepoCredential, activityID string) ps.ActivityGitStatus {
	t.Helper()
	proj, err := store.ReadProject(ctx, id, cred)
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

	v2, err := store.RecordActivityBranchOpened(ctx, id, v, "C-MST", "activity/C-MST", "ref-cmst", "pr-7", "cr-021", false, cred, "wf:branch")
	if err != nil {
		t.Fatalf("RecordActivityBranchOpened: %v", err)
	}
	if v2 != v+1 {
		t.Fatalf("version = %d, want %d", v2, v+1)
	}
	g := readActivity(t, store, ctx, id, cred, "C-MST")
	if g.ActivityID != "C-MST" || g.BranchName != "activity/C-MST" || g.BranchRef != "ref-cmst" {
		t.Fatalf("branch fields wrong: %+v", g)
	}
	if g.PullRequestRef != "pr-7" || g.CRLabel != "cr-021" {
		t.Fatalf("PR/CR fields wrong: %+v", g)
	}
	if g.CICheck != ps.CICheckPending {
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
	v2, err := store.RecordActivityBranchOpened(ctx, id, v, "C-MST", "activity/C-MST", "ref-cmst", "", "", false, cred, "wf:branch-only")
	if err != nil {
		t.Fatalf("branch-only touch: %v", err)
	}
	g := readActivity(t, store, ctx, id, cred, "C-MST")
	if g.BranchRef != "ref-cmst" || g.PullRequestRef != "" {
		t.Fatalf("after branch-only: %+v, want branch set, prRef empty", g)
	}
	if g.CICheck != ps.CICheckPending {
		t.Fatalf("CICheck after birth = %v, want Pending", g.CICheck)
	}

	// Second touch: the OpenPullRequest fills the PR fields; branch must survive.
	v3, err := store.RecordActivityBranchOpened(ctx, id, v2, "C-MST", "activity/C-MST", "ref-cmst", "pr-9", "cr-021", true, cred, "wf:pr-touch")
	if err != nil {
		t.Fatalf("pr touch: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}
	g = readActivity(t, store, ctx, id, cred, "C-MST")
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

	v, err := store.RecordActivityBranchOpened(ctx, id, v, "C-MST", "activity/C-MST", "ref", "pr-1", "", false, cred, "wf:branch")
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	v, err = store.RecordActivityCIObserved(ctx, id, v, "C-MST", ps.CICheckFailure, cred, "wf:ci-1")
	if err != nil {
		t.Fatalf("ci failure: %v", err)
	}
	if g := readActivity(t, store, ctx, id, cred, "C-MST"); g.CICheck != ps.CICheckFailure {
		t.Fatalf("CICheck = %v, want Failure", g.CICheck)
	}
	v, err = store.RecordActivityCIObserved(ctx, id, v, "C-MST", ps.CICheckSuccess, cred, "wf:ci-2")
	if err != nil {
		t.Fatalf("ci success: %v", err)
	}
	g := readActivity(t, store, ctx, id, cred, "C-MST")
	if g.CICheck != ps.CICheckSuccess {
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

	v, err := store.RecordActivityBranchOpened(ctx, id, v, "C-MST", "b", "ref", "pr-1", "", false, cred, "wf:branch")
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	v, err = store.RecordActivityArchApproved(ctx, id, v, "C-MST", cred, "wf:approve")
	if err != nil {
		t.Fatalf("arch approve: %v", err)
	}
	if g := readActivity(t, store, ctx, id, cred, "C-MST"); !g.ArchApproved || g.Merged {
		t.Fatalf("after approve: %+v, want ArchApproved=true Merged=false", g)
	}
	v, err = store.RecordActivityMerged(ctx, id, v, "C-MST", cred, "wf:merge")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	g := readActivity(t, store, ctx, id, cred, "C-MST")
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

	v2, err := store.RecordActivityBranchOpened(ctx, id, v, "C-MST", "b", "ref", "pr-1", "", false, cred, "wf:branch")
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	before, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}

	// Re-record with the SAME key but a deliberately stale expectedVersion (0). The
	// dedup probe must short-circuit and return the original v2, NOT a Conflict.
	v2again, err := store.RecordActivityBranchOpened(ctx, id, 0, "C-MST", "b", "ref", "pr-1", "", false, cred, "wf:branch")
	if err != nil {
		t.Fatalf("idempotent re-record should succeed via ledger, got: %v", err)
	}
	if v2again != v2 {
		t.Fatalf("idempotent re-record version = %d, want original %d", v2again, v2)
	}
	after, err := store.ReadProject(ctx, id, cred)
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
	_, err := store.RecordActivityCIObserved(ctx, id, v, "", ps.CICheckSuccess, cred, "wf:k")
	if k := kindOf(t, err); k != fwra.ContractMisuse {
		t.Fatalf("empty activityID kind = %v, want ContractMisuse", k)
	}
}

// TestRecordActivity_RequireExistingProject — a Record* against a project that does
// not exist is NotFound (modeRequireExisting; the project row exists by Phase 3).
func TestRecordActivity_RequireExistingProject(t *testing.T) {
	store, cred, ctx := newLocalGitStore(t)
	_, err := store.RecordActivityMerged(ctx, ps.ProjectID(uuid.NewString()), 0, "C-MST", cred, "wf:k")
	if k := kindOf(t, err); k != fwra.NotFound {
		t.Fatalf("Record* on absent project kind = %v, want NotFound", k)
	}
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

	type outcome struct {
		who string
		v   ps.Version
		err error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		v, e := store.RecordActivityBranchOpened(ctx, id, base, "C-MST", "b-mst", "ref-mst", "pr-1", "", false, cred, "wf:A")
		results <- outcome{"A-CMST", v, e}
	}()
	go func() {
		defer wg.Done()
		v, e := store.RecordActivityBranchOpened(ctx, id, base, "C-UC1", "b-uc1", "ref-uc1", "pr-2", "", false, cred, "wf:B")
		results <- outcome{"B-CUC1", v, e}
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
	if !gotWinner {
		t.Fatal("expected exactly one CAS winner, both lost")
	}
	if loser.who == "" {
		t.Fatal("expected one CAS loser (non-fast-forward), both won — LOST UPDATE")
	}
	if winner.v != base+1 {
		t.Fatalf("winner landed at version %d, want base+1 (%d)", winner.v, base+1)
	}
	if k := kindOf(t, loser.err); k != fwra.Conflict {
		t.Fatalf("loser %s kind = %v, want Conflict", loser.who, k)
	}

	// The loser reloads HEAD and re-applies against the winner's new tip.
	cur, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after race: %v", err)
	}
	switch loser.who {
	case "A-CMST":
		_, err = store.RecordActivityBranchOpened(ctx, id, cur.Version, "C-MST", "b-mst", "ref-mst", "pr-1", "", false, cred, "wf:A")
	case "B-CUC1":
		_, err = store.RecordActivityBranchOpened(ctx, id, cur.Version, "C-UC1", "b-uc1", "ref-uc1", "pr-2", "", false, cred, "wf:B")
	}
	if err != nil {
		t.Fatalf("loser %s retry: %v", loser.who, err)
	}

	// BOTH activity rows survive — the partial-map-key update did not clobber.
	final, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject final: %v", err)
	}
	mst, okMst := final.ActivityGit["C-MST"]
	uc1, okUc1 := final.ActivityGit["C-UC1"]
	if !okMst || !okUc1 {
		t.Fatalf("both activity rows must survive convergence, have keys: %v", keysOf(final.ActivityGit))
	}
	if mst.BranchRef != "ref-mst" || uc1.BranchRef != "ref-uc1" {
		t.Fatalf("convergence corrupted a row: C-MST=%+v C-UC1=%+v", mst, uc1)
	}
	// Sanity: the satellite's CAS loss is the documented sentinel.
	_ = fwgithub.ErrRefCASLost
}

func keysOf(m map[string]ps.ActivityGitStatus) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
