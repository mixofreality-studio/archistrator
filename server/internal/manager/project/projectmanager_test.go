package project

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// fakeProjectStateAccess is the contract-first test double over the narrow
// ProjectStateAccess port this package declares. Each verb records its inputs and
// returns canned outputs so the Manager's mapping/derivation logic is exercised in
// isolation (no Temporal, no Postgres).
type fakeProjectStateAccess struct {
	createCalls   int
	createOwner   projectstate.OwnerScope
	createName    string
	createID      projectstate.ProjectID
	createKey     fwra.IdempotencyKey
	createVersion projectstate.Version
	createErr     error

	listCalls   int
	listOwner   projectstate.OwnerScope
	listSummary []projectstate.ProjectSummary
	listErr     error

	readCalls   int
	readID      projectstate.ProjectID
	readProject projectstate.Project
	readErr     error
}

func (f *fakeProjectStateAccess) CreateProject(rc fwra.Context, projectID projectstate.ProjectID, owner projectstate.OwnerScope, name string) (projectstate.Version, error) {
	f.createCalls++
	f.createID = projectID
	f.createOwner = owner
	f.createName = name
	f.createKey = rc.IdempotencyKey
	if f.createErr != nil {
		return 0, f.createErr
	}
	if f.createVersion == 0 {
		f.createVersion = 1
	}
	return f.createVersion, nil
}

func (f *fakeProjectStateAccess) ListProjects(_ fwra.Context, owner projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	f.listCalls++
	f.listOwner = owner
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listSummary, nil
}

func (f *fakeProjectStateAccess) ReadProject(_ fwra.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
	f.readCalls++
	f.readID = projectID
	if f.readErr != nil {
		return projectstate.Project{}, f.readErr
	}
	return f.readProject, nil
}

// --- CreateProject ----------------------------------------------------------

// TestCreateProject_NameIsIdentityAndCallsRAOnce proves NAME-AS-IDENTITY (C-PM-Δ):
// the returned project id IS the user-supplied name (no server-minted UUID), and the
// RA is called exactly once carrying that identity.
func TestCreateProject_NameIsIdentityAndCallsRAOnce(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := NewManager(fake, nil, nil)

	id, err := m.CreateProject(context.Background(), OwnerScope("alice@example.com"), "my-cool-system")
	if err != nil {
		t.Fatalf("CreateProject: unexpected error: %v", err)
	}
	if id != ProjectID("my-cool-system") {
		t.Fatalf("CreateProject: id = %q, want the user-supplied name (name-as-identity)", id)
	}
	if fake.createCalls != 1 {
		t.Fatalf("CreateProject: RA called %d times, want 1", fake.createCalls)
	}
	if fake.createID != id {
		t.Fatalf("CreateProject: RA id %s, want %s", fake.createID, id)
	}
	if fake.createOwner != OwnerScope("alice@example.com") {
		t.Fatalf("CreateProject: RA owner %q, want alice@example.com", fake.createOwner)
	}
	if fake.createName != "my-cool-system" {
		t.Fatalf("CreateProject: RA name %q, want my-cool-system", fake.createName)
	}
	if fake.createKey.IsZero() {
		t.Fatal("CreateProject: derived idempotency key is empty")
	}
}

func TestCreateProject_EmptyOwner_ContractMisuse(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := NewManager(fake, nil, nil)

	_, err := m.CreateProject(context.Background(), OwnerScope(""), "My Project")
	if err == nil {
		t.Fatal("CreateProject: expected error for empty owner")
	}
	var me *fwmanager.Error
	if !errors.As(err, &me) || me.Kind != fwmanager.ContractMisuse {
		t.Fatalf("CreateProject: want ContractMisuse, got %v", err)
	}
	if fake.createCalls != 0 {
		t.Fatalf("CreateProject: RA should not be called on validation failure, got %d", fake.createCalls)
	}
}

func TestCreateProject_EmptyName_ContractMisuse(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := NewManager(fake, nil, nil)

	_, err := m.CreateProject(context.Background(), OwnerScope("alice"), "")
	if err == nil {
		t.Fatal("CreateProject: expected error for empty name")
	}
	var me *fwmanager.Error
	if !errors.As(err, &me) || me.Kind != fwmanager.ContractMisuse {
		t.Fatalf("CreateProject: want ContractMisuse, got %v", err)
	}
}

func TestCreateProject_RAConflict_MapsInfrastructure(t *testing.T) {
	fake := &fakeProjectStateAccess{createErr: fwra.New(fwra.Conflict, "row exists")}
	m := NewManager(fake, nil, nil)

	_, err := m.CreateProject(context.Background(), OwnerScope("alice"), "P")
	var me *fwmanager.Error
	if !errors.As(err, &me) {
		t.Fatalf("CreateProject: want fwmanager.Error, got %v", err)
	}
	if me.Kind != fwmanager.Infrastructure {
		t.Fatalf("CreateProject: Conflict should map to Infrastructure, got %v", me.Kind)
	}
}

// --- CreateProject: adopt + workflow seating + call order -------------------

// callOrder is a shared sequence recorder both fakes append to, so a test can
// assert adopt → seat (workflow file) → create.
type callOrder struct{ seq []string }

func (c *callOrder) record(name string) { c.seq = append(c.seq, name) }

// fakeSourceControl is the contract-first double over the narrow SourceControlAccess
// port this package declares. It records its inputs + call order and returns canned
// (or error) handles. EXTERNAL infra (GitHub) is the only thing faked — the component
// under test (the Manager) is real.
//
// CORRECTION (2026-06-15): the WriteAgenticToken seating verb was removed from the
// port (aiarch does no secret management; CLAUDE_CODE_OAUTH_TOKEN is user-provisioned
// via the Claude Code GitHub App). The fake's secret-write recorder is gone with it.
type fakeSourceControl struct {
	order *callOrder

	adoptCalls int
	adoptSpec  RepoSpec
	adoptKey   fwra.IdempotencyKey
	adoptErr   error

	mintCalls int
	mintErr   error

	workflowCalls int
	workflowKey   fwra.IdempotencyKey
	workflowErr   error
}

func (f *fakeSourceControl) AdoptProjectRepo(_ context.Context, spec RepoSpec, key fwra.IdempotencyKey) (RepoRef, error) {
	f.adoptCalls++
	f.adoptSpec = spec
	f.adoptKey = key
	if f.order != nil {
		f.order.record("adoptProjectRepo")
	}
	if f.adoptErr != nil {
		return nil, f.adoptErr
	}
	return fakeRepoRef(spec.RepoName), nil
}

func (f *fakeSourceControl) MintRepoCredential(_ context.Context, _ RepoRef) (RepoCredential, error) {
	f.mintCalls++
	if f.order != nil {
		f.order.record("mintRepoCredential")
	}
	if f.mintErr != nil {
		return nil, f.mintErr
	}
	return fakeCred{}, nil
}

func (f *fakeSourceControl) SeatAgenticWorkflow(_ context.Context, _ RepoRef, _ RepoCredential, key fwra.IdempotencyKey) error {
	f.workflowCalls++
	f.workflowKey = key
	if f.order != nil {
		f.order.record("seatAgenticWorkflow")
	}
	return f.workflowErr
}

// fakeRepoRef / fakeCred are minimal handles the fake returns.
type fakeRepoRef string

func (r fakeRepoRef) IsZero() bool   { return r == "" }
func (r fakeRepoRef) String() string { return string(r) }

type fakeCred struct{}

func (fakeCred) IsZero() bool { return false }

// orderingProjectState wraps the base fake to also record its create call into the
// shared order recorder.
type orderingProjectState struct {
	*fakeProjectStateAccess
	order *callOrder
}

func (o *orderingProjectState) CreateProject(rc fwra.Context, projectID projectstate.ProjectID, owner projectstate.OwnerScope, name string) (projectstate.Version, error) {
	if o.order != nil {
		o.order.record("createProject")
	}
	return o.fakeProjectStateAccess.CreateProject(rc, projectID, owner, name)
}

// TestCreateProject_AdoptThenSeatThenCreate is the load-bearing call-order guarantee
// (C-PM-Δ): the user's repo is ADOPTED, then SEATED with the agentic-design workflow
// file (mint credential → commit workflow), STRICTLY BEFORE the head-state row — so a
// project is never cataloged without an adopted, workflow-seated repo. Name-as-identity:
// the adopted RepoName == the returned id == the created id, and the SAME idempotency
// key threads through every verb (retry re-convergence).
//
// CORRECTION (2026-06-15): no writeActionsSecret step — aiarch does no secret
// management; the CLAUDE_CODE_OAUTH_TOKEN is user-provisioned via the Claude Code
// GitHub App. Seating is now just mint → commit-workflow.
func TestCreateProject_AdoptThenSeatThenCreate(t *testing.T) {
	order := &callOrder{}
	ps := &orderingProjectState{fakeProjectStateAccess: &fakeProjectStateAccess{}, order: order}
	sc := &fakeSourceControl{order: order}
	m := NewManager(ps, sc, nil)

	id, err := m.CreateProject(context.Background(), OwnerScope("alice@example.com"), "my-cool-system")
	if err != nil {
		t.Fatalf("CreateProject: unexpected error: %v", err)
	}
	if id != ProjectID("my-cool-system") {
		t.Fatalf("CreateProject: id = %q, want name-as-identity my-cool-system", id)
	}

	// Each collaborator called exactly once.
	if sc.adoptCalls != 1 || sc.mintCalls != 1 || sc.workflowCalls != 1 {
		t.Fatalf("source-control call counts: adopt=%d mint=%d workflow=%d, want 1 each",
			sc.adoptCalls, sc.mintCalls, sc.workflowCalls)
	}
	if ps.createCalls != 1 {
		t.Fatalf("projectState.CreateProject called %d times, want 1", ps.createCalls)
	}

	// THE ORDER (C-PM-Δ required sequence, post-correction): adopt → mint-credential →
	// seat-workflow → create.
	want := []string{"adoptProjectRepo", "mintRepoCredential", "seatAgenticWorkflow", "createProject"}
	if len(order.seq) != len(want) {
		t.Fatalf("call order = %v, want %v", order.seq, want)
	}
	for i := range want {
		if order.seq[i] != want[i] {
			t.Fatalf("call order = %v, want %v", order.seq, want)
		}
	}

	// Name-as-identity: the adopted RepoName IS the project identity == the created id.
	if sc.adoptSpec.RepoName != id.String() {
		t.Fatalf("AdoptProjectRepo RepoName = %q, want %q (name-as-identity)", sc.adoptSpec.RepoName, id.String())
	}
	if sc.adoptSpec.Title != "my-cool-system" {
		t.Fatalf("AdoptProjectRepo Title = %q, want my-cool-system", sc.adoptSpec.Title)
	}

	// The SAME idempotency key threads through adopt, workflow, AND create — so a retry
	// after a partial failure re-converges.
	if sc.adoptKey != ps.createKey || sc.workflowKey != ps.createKey {
		t.Fatalf("idempotency keys diverged: adopt=%q workflow=%q create=%q (retry would not re-converge)",
			sc.adoptKey, sc.workflowKey, ps.createKey)
	}
}

// TestCreateProject_AdoptFailure_NoSeatingNoCreate proves the order is also a GATE:
// if adopt fails, NEITHER the workflow seating NOR the head-state row happen (no orphan
// project pointing at an un-adopted repo).
func TestCreateProject_AdoptFailure_NoSeatingNoCreate(t *testing.T) {
	order := &callOrder{}
	ps := &orderingProjectState{fakeProjectStateAccess: &fakeProjectStateAccess{}, order: order}
	// An adopt failure (here a Transient infra fault — note the strict-empty RepoNotEmpty
	// hard-fail is GONE post-2026-06-16-permissive-resume) must gate seating + create. A
	// non-NotFound/non-ContractMisuse adopt error maps to Infrastructure at this thin
	// catalog Manager (no optimistic-concurrency loop to recover it).
	sc := &fakeSourceControl{order: order, adoptErr: fwra.New(fwra.Transient, "github 503")}
	m := NewManager(ps, sc, nil)

	_, err := m.CreateProject(context.Background(), OwnerScope("alice"), "taken-repo")
	if err == nil {
		t.Fatal("CreateProject: expected error when adopt fails")
	}
	var me *fwmanager.Error
	if !errors.As(err, &me) {
		t.Fatalf("CreateProject: want fwmanager.Error, got %v", err)
	}
	if me.Kind != fwmanager.Infrastructure {
		t.Fatalf("adopt Conflict should map to Infrastructure, got %v", me.Kind)
	}
	if sc.adoptCalls != 1 {
		t.Fatalf("AdoptProjectRepo called %d times, want 1", sc.adoptCalls)
	}
	// No seating, no create.
	if sc.mintCalls != 0 || sc.workflowCalls != 0 {
		t.Fatalf("seating must NOT run after adopt failure: mint=%d workflow=%d",
			sc.mintCalls, sc.workflowCalls)
	}
	if ps.createCalls != 0 {
		t.Fatalf("projectState.CreateProject must NOT be called after an adopt failure, got %d", ps.createCalls)
	}
	if len(order.seq) != 1 || order.seq[0] != "adoptProjectRepo" {
		t.Fatalf("call order = %v, want [adoptProjectRepo] only", order.seq)
	}
}

// TestCreateProject_WiredSourceControl_AdoptsAndSeats proves that with a wired
// source-control, the repo is adopted AND the workflow file is seated (mint → commit)
// before the head-state row — unconditionally, with no per-user token gate (the old
// no-token-no-seating branch is gone post-correction).
func TestCreateProject_WiredSourceControl_AdoptsAndSeats(t *testing.T) {
	order := &callOrder{}
	ps := &orderingProjectState{fakeProjectStateAccess: &fakeProjectStateAccess{}, order: order}
	sc := &fakeSourceControl{order: order}
	m := NewManager(ps, sc, nil)

	id, err := m.CreateProject(context.Background(), OwnerScope("alice"), "repo-x")
	if err != nil {
		t.Fatalf("CreateProject: unexpected error: %v", err)
	}
	if id != ProjectID("repo-x") {
		t.Fatalf("CreateProject: id = %q", id)
	}
	if sc.adoptCalls != 1 || sc.mintCalls != 1 || sc.workflowCalls != 1 {
		t.Fatalf("adopt+seat must run: adopt=%d mint=%d workflow=%d, want 1 each",
			sc.adoptCalls, sc.mintCalls, sc.workflowCalls)
	}
	if ps.createCalls != 1 {
		t.Fatalf("projectState.CreateProject called %d times, want 1", ps.createCalls)
	}
	want := []string{"adoptProjectRepo", "mintRepoCredential", "seatAgenticWorkflow", "createProject"}
	if len(order.seq) != len(want) {
		t.Fatalf("call order = %v, want %v", order.seq, want)
	}
	for i := range want {
		if order.seq[i] != want[i] {
			t.Fatalf("call order = %v, want %v", order.seq, want)
		}
	}
}

// TestCreateProject_NilSourceControl_SkipsAdopt proves a credential-free dev server
// (nil sourceControl) still creates projects — repo-less, no adopt, no crash.
func TestCreateProject_NilSourceControl_SkipsAdopt(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := NewManager(fake, nil, nil)

	id, err := m.CreateProject(context.Background(), OwnerScope("alice"), "dev-project")
	if err != nil {
		t.Fatalf("CreateProject (nil source control): unexpected error: %v", err)
	}
	if id != ProjectID("dev-project") {
		t.Fatalf("CreateProject: id = %q", id)
	}
	if fake.createCalls != 1 {
		t.Fatalf("projectState.CreateProject called %d times, want 1", fake.createCalls)
	}
}

// --- ListProjects -----------------------------------------------------------

func TestListProjects_PassesThrough(t *testing.T) {
	now := time.Now().UTC()
	want := []projectstate.ProjectSummary{
		{ProjectID: "alpha", Name: "A", Owner: "alice", Phase: projectstate.PhaseSystemDesign, CommittedCount: 2, TotalCount: 8, UpdatedAt: now},
		{ProjectID: "beta", Name: "B", Owner: "alice", Phase: projectstate.PhaseProjectDesign, CommittedCount: 9, TotalCount: 9, UpdatedAt: now},
	}
	fake := &fakeProjectStateAccess{listSummary: want}
	m := NewManager(fake, nil, nil)

	got, err := m.ListProjects(context.Background(), OwnerScope("alice"))
	if err != nil {
		t.Fatalf("ListProjects: unexpected error: %v", err)
	}
	if fake.listCalls != 1 || fake.listOwner != OwnerScope("alice") {
		t.Fatalf("ListProjects: RA calls=%d owner=%q", fake.listCalls, fake.listOwner)
	}
	if len(got) != len(want) {
		t.Fatalf("ListProjects: got %d summaries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListProjects: summary[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListProjects_RAError_MapsInfrastructure(t *testing.T) {
	fake := &fakeProjectStateAccess{listErr: fwra.New(fwra.Infrastructure, "db down")}
	m := NewManager(fake, nil, nil)

	_, err := m.ListProjects(context.Background(), OwnerScope("alice"))
	var me *fwmanager.Error
	if !errors.As(err, &me) || me.Kind != fwmanager.Infrastructure {
		t.Fatalf("ListProjects: want Infrastructure, got %v", err)
	}
}

// --- GetProject -------------------------------------------------------------

func sampleProject(id projectstate.ProjectID) projectstate.Project {
	return projectstate.Project{
		ID:      id,
		Version: 7,
		Phase:   projectstate.PhaseSystemDesign,
		Owner:   "alice",
		Name:    "Sample",
		ResearchInput: projectstate.ResearchInput{
			Sources: []projectstate.ResearchSource{{Title: "Brief", Content: "founder brief"}},
		},
		// Mission: committed, populated.
		Mission: projectstate.ArtifactSlot{
			Status: projectstate.ReviewCommitted,
			Model:  &projectstate.MissionStatement{},
		},
		// Glossary: awaiting review, populated, with notes.
		Glossary: projectstate.ArtifactSlot{
			Status: projectstate.ReviewAwaitingReview,
			Model:  &projectstate.Glossary{},
			Notes:  "needs trimming",
		},
		// Volatilities: rejected, model retained.
		Volatilities: projectstate.ArtifactSlot{
			Status: projectstate.ReviewRejected,
			Model:  &projectstate.Volatilities{},
			Notes:  "redo",
		},
		// ScrubbedRequirements: empty (ReviewNone, nil model).
	}
}

func TestGetProject_MapsAggregateToTypedSlots(t *testing.T) {
	id := ProjectID("my-cool-system")
	fake := &fakeProjectStateAccess{readProject: sampleProject(id)}
	m := NewManager(fake, nil, nil)

	st, err := m.GetProject(context.Background(), id)
	if err != nil {
		t.Fatalf("GetProject: unexpected error: %v", err)
	}
	if fake.readCalls != 1 || fake.readID != id {
		t.Fatalf("GetProject: RA calls=%d id=%s", fake.readCalls, fake.readID)
	}
	if st.ProjectID != id || st.Name != "Sample" || st.Owner != "alice" {
		t.Fatalf("GetProject: identity mismatch: %+v", st)
	}
	if st.Phase != projectstate.PhaseSystemDesign || st.Version != 7 {
		t.Fatalf("GetProject: phase/version mismatch: %+v", st)
	}
	if len(st.Research.Sources) != 1 || st.Research.Sources[0].Title != "Brief" {
		t.Fatalf("GetProject: research not mapped: %+v", st.Research)
	}

	// Slots: one per defined ArtifactKind, in stable order.
	byKind := map[projectstate.ArtifactKind]ArtifactSlotView{}
	for _, s := range st.Slots {
		byKind[s.Kind] = s
	}
	if len(st.Slots) != len(projectstate.AllArtifactKinds()) {
		t.Fatalf("GetProject: got %d slots, want %d", len(st.Slots), len(projectstate.AllArtifactKinds()))
	}

	mission := byKind[projectstate.KindMission]
	if mission.Stage != StageCommitted {
		t.Fatalf("Mission stage = %v, want StageCommitted", mission.Stage)
	}
	if mission.Model == nil || mission.Model.Kind() != projectstate.KindMission {
		t.Fatalf("Mission model not mapped: %+v", mission.Model)
	}

	glossary := byKind[projectstate.KindGlossary]
	if glossary.Stage != StageAwaitingReview {
		t.Fatalf("Glossary stage = %v, want StageAwaitingReview", glossary.Stage)
	}
	if glossary.Model == nil {
		t.Fatal("Glossary model should be populated")
	}
	if glossary.Notes != "needs trimming" {
		t.Fatalf("Glossary notes = %q", glossary.Notes)
	}

	volatilities := byKind[projectstate.KindVolatilities]
	if volatilities.Stage != StageRejected {
		t.Fatalf("Volatilities stage = %v, want StageRejected", volatilities.Stage)
	}

	scrubbed := byKind[projectstate.KindScrubbedRequirements]
	if scrubbed.Stage != StageEmpty {
		t.Fatalf("ScrubbedRequirements stage = %v, want StageEmpty", scrubbed.Stage)
	}
	if scrubbed.Model != nil {
		t.Fatal("ScrubbedRequirements model should be nil (empty slot)")
	}
}

// TestGetProject_ComputeNetworkAtRead verifies the compute-at-read wiring: when a real
// constructionEstimationEngine is injected, GetProject populates the Network slot's
// computed block (per-node CPM + bands), the summary, and the milestone event times —
// over a small diamond network A→{B,C}→D with a milestone fanning in on D.
func TestGetProject_ComputeNetworkAtRead(t *testing.T) {
	id := ProjectID("net-proj")
	p := sampleProject(id)
	// Diamond: A(5) → B(5),C(15) → D(5). Longest path A→C→D = 25 days. B has 10d float.
	p.ActivityList = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.ActivityList{Activities: []projectstate.ActivityItem{
			{Name: "A", EffortDays: 5, WorkerClass: "dev"},
			{Name: "B", EffortDays: 5, WorkerClass: "dev"},
			{Name: "C", EffortDays: 15, WorkerClass: "dev"},
			{Name: "D", EffortDays: 5, WorkerClass: "dev"},
		}},
	}
	p.Network = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: []projectstate.NetworkDependency{
				{Activity: "B", DependsOn: []string{"A"}},
				{Activity: "C", DependsOn: []string{"A"}},
				{Activity: "D", DependsOn: []string{"B", "C"}},
			},
			CriticalPath: []string{"A", "C", "D"},
			Milestones: []projectstate.NetworkMilestone{
				{ID: "M-DONE", Name: "Done", Public: true, DependsOn: []string{"D"}},
			},
		},
	}

	fake := &fakeProjectStateAccess{readProject: p}
	m := NewManager(fake, nil, estimation.New())

	st, err := m.GetProject(context.Background(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}

	byKind := map[projectstate.ArtifactKind]ArtifactSlotView{}
	for _, s := range st.Slots {
		byKind[s.Kind] = s
	}
	netModel, ok := byKind[projectstate.KindNetwork].Model.(*projectstate.Network)
	if !ok || netModel == nil {
		t.Fatalf("network model not present: %+v", byKind[projectstate.KindNetwork].Model)
	}
	if netModel.Summary == nil {
		t.Fatal("compute-at-read: summary not populated")
	}
	if netModel.Summary.TotalDurationDays != 25 {
		t.Fatalf("project duration = %v, want 25", netModel.Summary.TotalDurationDays)
	}
	if len(netModel.Computed) != 4 {
		t.Fatalf("computed nodes = %d, want 4", len(netModel.Computed))
	}
	if cNode := netModel.Computed["C"]; !cNode.OnCriticalPath || cNode.Band != estimation.BandCritical {
		t.Fatalf("C should be critical: %+v", cNode)
	}
	if bNode := netModel.Computed["B"]; bNode.OnCriticalPath || bNode.TotalFloat != 10 {
		t.Fatalf("B should be off-CP with float 10: %+v", bNode)
	}
	// Authored dependencies preserved untouched.
	if len(netModel.Dependencies) != 3 {
		t.Fatalf("authored dependencies perturbed: %v", netModel.Dependencies)
	}
	// criticalPath[] is OVERWRITTEN with the computed float-0 ACTIVITY set (task #14),
	// sorted. The diamond's float-0 activities are A, C, D (B has float 10). Milestones
	// are excluded from criticalPath[].
	if got := netModel.CriticalPath; len(got) != 3 || got[0] != "A" || got[1] != "C" || got[2] != "D" {
		t.Fatalf("criticalPath not the computed float-0 set [A C D]: %v", got)
	}
	// Milestone event time = max(predecessor EF) = D.earliestFinish = 25.
	if len(netModel.Milestones) != 1 {
		t.Fatalf("milestones = %d, want 1", len(netModel.Milestones))
	}
	ms := netModel.Milestones[0]
	if ms.EventTime == nil || *ms.EventTime != 25 || ms.OnCriticalPath == nil || !*ms.OnCriticalPath {
		t.Fatalf("milestone compute wrong: %+v", ms)
	}
}

// TestGetProject_NilEstimator_NoCompute verifies the compute-at-read is a no-op when no
// estimator is injected (a dev/test Manager): the authored network is served unenriched.
func TestGetProject_NilEstimator_NoCompute(t *testing.T) {
	id := ProjectID("net-proj")
	p := sampleProject(id)
	p.Network = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: []projectstate.NetworkDependency{{Activity: "B", DependsOn: []string{"A"}}},
			CriticalPath: []string{"A", "B"},
		},
	}
	fake := &fakeProjectStateAccess{readProject: p}
	m := NewManager(fake, nil, nil)

	st, err := m.GetProject(context.Background(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	for _, s := range st.Slots {
		if s.Kind == projectstate.KindNetwork {
			net := s.Model.(*projectstate.Network)
			if net.Summary != nil || len(net.Computed) != 0 {
				t.Fatalf("nil estimator should not compute: %+v", net)
			}
			// With no estimator the authored criticalPath[] is left untouched.
			if len(net.CriticalPath) != 2 || net.CriticalPath[0] != "A" {
				t.Fatalf("nil estimator should preserve authored criticalPath: %v", net.CriticalPath)
			}
		}
	}
}

// TestGetProject_OverwritesStaleCriticalPath verifies task #14: when the authored
// criticalPath[] disagrees with the computed float-0 set (the agentic-pivot staleness),
// the served criticalPath[] is the COMPUTED float-0 activity set, not the stale authored
// names. Here the authored CP names a stale node ("STALE") and omits the real float-0
// node B-equivalent; the computed set wins.
func TestGetProject_OverwritesStaleCriticalPath(t *testing.T) {
	id := ProjectID("net-proj")
	p := sampleProject(id)
	// Linear chain A(5) → B(5): both float-0 (the whole chain is the critical path).
	p.ActivityList = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.ActivityList{Activities: []projectstate.ActivityItem{
			{Name: "A", EffortDays: 5, WorkerClass: "dev"},
			{Name: "B", EffortDays: 5, WorkerClass: "dev"},
		}},
	}
	p.Network = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: []projectstate.NetworkDependency{{Activity: "B", DependsOn: []string{"A"}}},
			CriticalPath: []string{"STALE", "GONE"}, // stale authored names — must be overwritten
		},
	}
	fake := &fakeProjectStateAccess{readProject: p}
	m := NewManager(fake, nil, estimation.New())

	st, err := m.GetProject(context.Background(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	for _, s := range st.Slots {
		if s.Kind == projectstate.KindNetwork {
			net := s.Model.(*projectstate.Network)
			// Overwritten with the computed float-0 set [A, B] (sorted), NOT the stale authored names.
			if len(net.CriticalPath) != 2 || net.CriticalPath[0] != "A" || net.CriticalPath[1] != "B" {
				t.Fatalf("criticalPath not overwritten with computed float-0 set: %v", net.CriticalPath)
			}
		}
	}
}

func TestGetProject_NotFoundPassesThrough(t *testing.T) {
	fake := &fakeProjectStateAccess{readErr: fwra.New(fwra.NotFound, "no row")}
	m := NewManager(fake, nil, nil)

	_, err := m.GetProject(context.Background(), ProjectID("missing"))
	var me *fwmanager.Error
	if !errors.As(err, &me) {
		t.Fatalf("GetProject: want fwmanager.Error, got %v", err)
	}
	if me.Kind != fwmanager.NotFound {
		t.Fatalf("GetProject: want NotFound, got %v", me.Kind)
	}
}

func TestGetProject_EmptyProjectID_ContractMisuse(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := NewManager(fake, nil, nil)

	_, err := m.GetProject(context.Background(), ProjectID(""))
	var me *fwmanager.Error
	if !errors.As(err, &me) || me.Kind != fwmanager.ContractMisuse {
		t.Fatalf("GetProject: want ContractMisuse, got %v", err)
	}
	if fake.readCalls != 0 {
		t.Fatal("GetProject: RA should not be called on nil id")
	}
}

// --- envelope round-trip ----------------------------------------------------

// TestProjectState_JSONRoundTrip proves ProjectState marshals the typed
// ArtifactSlotView.Model via the SAME {kind:int, raw:json} envelope systemdesign
// uses, so the SPA's generated client decodes both identically, and that a full
// round-trip reconstructs the concrete typed models and the empty/nil slot.
func TestProjectState_JSONRoundTrip(t *testing.T) {
	id := ProjectID("my-cool-system")
	in := ProjectState{
		ProjectID: id,
		Name:      "Sample",
		Owner:     "alice",
		Phase:     projectstate.PhaseSystemDesign,
		Version:   7,
		Research: projectstate.ResearchInput{
			Sources: []projectstate.ResearchSource{{Title: "Brief", Content: "x"}},
		},
		Slots: []ArtifactSlotView{
			{Kind: projectstate.KindMission, Stage: StageCommitted, Model: &projectstate.MissionStatement{}},
			{Kind: projectstate.KindGlossary, Stage: StageAwaitingReview, Model: &projectstate.Glossary{}, Notes: "n"},
			{Kind: projectstate.KindScrubbedRequirements, Stage: StageEmpty, Model: nil},
		},
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Assert the wire envelope shape matches systemdesign's: each slot's kind is a
	// STRING wire name; each slot's model envelope is {"kind":"<name>","model":...};
	// an empty slot's model envelope has no "model".
	var generic struct {
		Slots []struct {
			Kind  string `json:"kind"`
			Model struct {
				Kind  string          `json:"kind"`
				Model json.RawMessage `json:"model"`
			} `json:"model"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	if len(generic.Slots) != 3 {
		t.Fatalf("wire: got %d slots", len(generic.Slots))
	}
	if generic.Slots[0].Kind != "mission" {
		t.Fatalf("wire: slot kind = %q, want \"mission\"", generic.Slots[0].Kind)
	}
	if generic.Slots[0].Model.Kind != "mission" {
		t.Fatalf("wire: mission envelope kind = %q, want \"mission\"", generic.Slots[0].Model.Kind)
	}
	if len(generic.Slots[0].Model.Model) == 0 {
		t.Fatal("wire: mission envelope should carry model payload")
	}
	if len(generic.Slots[2].Model.Model) != 0 {
		t.Fatal("wire: empty slot should have no model payload")
	}

	// Full round-trip back into ProjectState reconstructs concrete typed models.
	var out ProjectState
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if out.ProjectID != id || out.Name != "Sample" || out.Version != 7 {
		t.Fatalf("round-trip identity mismatch: %+v", out)
	}
	if len(out.Slots) != 3 {
		t.Fatalf("round-trip: got %d slots", len(out.Slots))
	}
	if out.Slots[0].Model == nil || out.Slots[0].Model.Kind() != projectstate.KindMission {
		t.Fatalf("round-trip: mission model = %+v", out.Slots[0].Model)
	}
	if out.Slots[0].Stage != StageCommitted {
		t.Fatalf("round-trip: mission stage = %v", out.Slots[0].Stage)
	}
	if out.Slots[1].Notes != "n" {
		t.Fatalf("round-trip: glossary notes = %q", out.Slots[1].Notes)
	}
	if out.Slots[2].Model != nil {
		t.Fatalf("round-trip: empty slot model should be nil, got %+v", out.Slots[2].Model)
	}
}
