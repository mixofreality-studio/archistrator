package projectstate_test

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

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

func readProject(t *testing.T, store *ps.GitStore, id ps.ProjectID, cred ps.RepoCredential) ps.Project {
	t.Helper()
	p, err := store.ReadProject(context.Background(), id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	return p
}

func readConstructionStatus(t *testing.T, store *ps.GitStore, id ps.ProjectID, cred ps.RepoCredential, activityID string) ps.ActivityConstructionStatus {
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
func seedActivity(t *testing.T, store *ps.GitStore, id ps.ProjectID, v ps.Version, cred ps.RepoCredential, activityID string) ps.Version {
	t.Helper()
	v2, err := store.RecordActivityStarted(context.Background(), id, v, activityID, cred, fwra.IdempotencyKey("wf:seed-"+activityID))
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

	v3, err := store.RecordChangeReviewed(ctx, id, v2, "C001", cred, fwra.IdempotencyKey("wf:cr-reviewed"))
	if err != nil {
		t.Fatalf("RecordChangeReviewed: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}

	s := readConstructionStatus(t, store, id, cred, "C001")
	if s.BuildStatus != ps.BuildInReview {
		t.Fatalf("BuildStatus = %v, want BuildInReview", s.BuildStatus)
	}
}

// STP 1b: empty activityID is rejected at the guard
func TestRecordChangeReviewed_EmptyActivityID_Error(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()
	_, err := store.RecordChangeReviewed(ctx, id, v, "", cred, fwra.IdempotencyKey("wf:cr-empty"))
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

	v3, err := store.RecordActivityExited(ctx, id, v2, "C002", ps.ActivityOutcomeCompleted, cred, fwra.IdempotencyKey("wf:exited-completed"))
	if err != nil {
		t.Fatalf("RecordActivityExited: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}

	s := readConstructionStatus(t, store, id, cred, "C002")
	if s.Phase != ps.ActivityConstructionDone {
		t.Fatalf("Phase = %v, want Done", s.Phase)
	}
	if s.BuildStatus != ps.BuildIntegrated {
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

	_, err := store.RecordActivityExited(ctx, id, v2, "C003", ps.ActivityOutcomeSkipped, cred, fwra.IdempotencyKey("wf:exited-skipped"))
	if err != nil {
		t.Fatalf("RecordActivityExited(Skipped): %v", err)
	}

	s := readConstructionStatus(t, store, id, cred, "C003")
	if s.Phase != ps.ActivityConstructionDone {
		t.Fatalf("Phase = %v, want Done", s.Phase)
	}
	if s.BuildStatus != ps.BuildInReview {
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

	v2, err := store.RecordOperatorPaused(ctx, id, v, "awaiting contractor availability", cred, fwra.IdempotencyKey("wf:paused"))
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
	p := ps.Project{
		OperatorPaused: true,
		PauseReason:    "manual hold",
	}
	raw, err := ps.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	got, ok, err := ps.DecodeProjectJSON(raw, "")
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

	v3, err := store.RecordPhaseStarted(ctx, id, v2, "C004", ps.MethodPhaseRequirements, cred, fwra.IdempotencyKey("wf:phase-started"))
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
	if s.CurrentPhase != ps.MethodPhaseRequirements {
		t.Fatalf("CurrentPhase = %q, want %q", s.CurrentPhase, ps.MethodPhaseRequirements)
	}
	if s.Phase != ps.ActivityConstructionRunning {
		t.Fatalf("Phase = %v, want Running", s.Phase)
	}
}

// STP 5b: empty phase is rejected
func TestRecordPhaseStarted_EmptyPhase_Error(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()
	v2 := seedActivity(t, store, id, v, cred, "C004b")
	_, err := store.RecordPhaseStarted(ctx, id, v2, "C004b", "", cred, fwra.IdempotencyKey("wf:phase-started-empty"))
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
	v3, err := store.RecordPhaseStarted(ctx, id, v2, "C005", ps.MethodPhaseRequirements, cred, fwra.IdempotencyKey("wf:ps-c005"))
	if err != nil {
		t.Fatalf("RecordPhaseStarted: %v", err)
	}

	v4, err := store.RecordPhaseCompleted(ctx, id, v3, "C005", ps.MethodPhaseRequirements, "srs/myservice.md", cred, fwra.IdempotencyKey("wf:pc-c005"))
	if err != nil {
		t.Fatalf("RecordPhaseCompleted: %v", err)
	}
	if v4 != v3+1 {
		t.Fatalf("version = %d, want %d", v4, v3+1)
	}

	s := readConstructionStatus(t, store, id, cred, "C005")
	var reqPhase *ps.PhaseCompletion
	for i := range s.Phases {
		if s.Phases[i].Phase == ps.MethodPhaseRequirements {
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
	phases := []ps.ActivityMethodPhase{
		ps.MethodPhaseRequirements,
		ps.MethodPhaseDetailedDesign,
		ps.MethodPhaseTestPlan,
		ps.MethodPhaseConstruction,
		ps.MethodPhaseIntegration,
	}

	cur := v2
	for i, ph := range phases {
		startKey := fwra.IdempotencyKey("wf:ps-c006-" + ph.String())
		cur2, err := store.RecordPhaseStarted(ctx, id, cur, "C006", ph, cred, startKey)
		if err != nil {
			t.Fatalf("RecordPhaseStarted(%s): %v", ph, err)
		}
		completedKey := fwra.IdempotencyKey("wf:pc-c006-" + ph.String())
		cur3, err := store.RecordPhaseCompleted(ctx, id, cur2, "C006", ph, "", cred, completedKey)
		if err != nil {
			t.Fatalf("RecordPhaseCompleted(%s): %v", ph, err)
		}
		_ = i
		cur = cur3
	}

	s := readConstructionStatus(t, store, id, cred, "C006")
	if s.Phase != ps.ActivityConstructionDone {
		t.Fatalf("Phase = %v after all phases completed, want Done", s.Phase)
	}
}

// --------------------------------------------------------------------------
// STP 8: RecordServiceContractProduced writes contract under component key
// --------------------------------------------------------------------------

func TestRecordServiceContractProduced_WritesContract(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	contract := ps.ServiceContract{
		Component: "myEngine",
		Layer:     "Engine",
		GoPackage: "internal/engine/myengine",
		Title:     "myengine contract",
		Interface: ps.ContractInterface{
			Name:  "MyEngine",
			Layer: "engine",
			Operations: []ps.ContractOperation{
				{Name: "Compute", Error: true},
			},
		},
	}

	v2, err := store.RecordServiceContractProduced(ctx, id, v, "myEngine", contract, cred, fwra.IdempotencyKey("wf:contract-myengine"))
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
	_, err := store.RecordServiceContractProduced(ctx, id, v, "", ps.ServiceContract{}, cred, fwra.IdempotencyKey("wf:contract-empty"))
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
	payload := ps.PhaseArtifactPayload{
		SRS: &ps.SRSRecord{
			Component:  "myService",
			Content:    "the service does X when Y",
			AuthoredAt: &now,
		},
	}

	v3, err := store.RecordPhaseArtifactProduced(ctx, id, v2, "C007", "myService", payload, cred, fwra.IdempotencyKey("wf:artifact-srs"))
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
	payload := ps.PhaseArtifactPayload{
		SystemTestPlan: &ps.SystemTestPlan{
			UseCaseIndex: []string{"UC1", "UC2"},
			Entries:      []string{"verify create project", "verify read project"},
			Status:       "approved",
			ApprovedAt:   &approved,
		},
	}

	v3, err := store.RecordPhaseArtifactProduced(ctx, id, v2, "N001", "", payload, cred, fwra.IdempotencyKey("wf:artifact-stp"))
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

	v3, err := store.RecordPhaseStarted(ctx, id, v2, "C008", ps.MethodPhaseRequirements, cred, fwra.IdempotencyKey("wf:ps-idem"))
	if err != nil {
		t.Fatalf("RecordPhaseStarted: %v", err)
	}
	before := readProject(t, store, id, cred)

	// Retry with SAME key but stale expectedVersion; dedup ledger must win.
	v3again, err := store.RecordPhaseStarted(ctx, id, 0, "C008", ps.MethodPhaseRequirements, cred, fwra.IdempotencyKey("wf:ps-idem"))
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
	p := ps.Project{}
	p.PhaseArtifacts = &ps.PhaseArtifacts{
		SRS: map[string]ps.SRSRecord{
			"svcA": {Component: "svcA", Content: "requirements text", AuthoredAt: &now},
		},
		TestPlan: map[string]ps.TestPlanRecord{
			"svcA": {Component: "svcA", Content: "test plan text"},
		},
	}
	p.TestingState = &ps.TestingState{
		SystemTestPlan: &ps.SystemTestPlan{
			UseCaseIndex: []string{"UC1"},
			Status:       "approved",
		},
		QualityAuditReport: "all gates green",
	}

	raw, err := ps.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	got, ok, err := ps.DecodeProjectJSON(raw, "")
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
	_, err := store.RecordPhaseArtifactProduced(ctx, id, v, "", "key", ps.PhaseArtifactPayload{}, cred, fwra.IdempotencyKey("wf:artifact-empty"))
	if err == nil {
		t.Fatal("want error for empty activityID, got nil")
	}
}

// TestRecordServiceContractProduced_TwoComponents verifies second write doesn't
// clobber the first (map-key upsert, not full-map replace).
func TestRecordServiceContractProduced_TwoComponents(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2, err := store.RecordServiceContractProduced(ctx, id, v, "engineA", ps.ServiceContract{Component: "engineA", Title: "engineA contract"}, cred, fwra.IdempotencyKey("wf:contract-a"))
	if err != nil {
		t.Fatalf("RecordServiceContractProduced(engineA): %v", err)
	}
	_, err = store.RecordServiceContractProduced(ctx, id, v2, "engineB", ps.ServiceContract{Component: "engineB", Title: "engineB contract"}, cred, fwra.IdempotencyKey("wf:contract-b"))
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
	v3, err := store.RecordPhaseStarted(ctx, id, v2, "C010", ps.MethodPhaseRequirements, cred, fwra.IdempotencyKey("wf:ps-c010"))
	if err != nil {
		t.Fatalf("RecordPhaseStarted: %v", err)
	}
	// complete a phase that is NOT in the service phase set (e.g. "ui_design" — a non-existent id post-refactor)
	v4, err := store.RecordPhaseCompleted(ctx, id, v3, "C010", ps.ActivityMethodPhase("ui_design"), "", cred, fwra.IdempotencyKey("wf:pc-c010-nophase"))
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
