package project

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// rc is the Manager-layer call Context the ops lead with (zero principal in tests).
func rc() fwm.Context { return fwm.Context{Context: context.Background()} }

// slotByKind finds the contract slot whose Kind is the canonical wire name of the
// given projectstate ArtifactKind.
func slotByKind(st ProjectState, kind projectstate.ArtifactKind) (ArtifactSlotView, bool) {
	for _, s := range st.Slots {
		if s.Kind == kind.WireName() {
			return s, true
		}
	}
	return ArtifactSlotView{}, false
}

// fakeProjectStateAccess is the contract-first test double over the narrow
// ProjectStateAccess port this package declares (projectstate-typed; the Manager
// converts the value shapes into its OWN contract types after the call).
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
// the returned project id IS the user-supplied name, and the RA is called exactly once.
func TestCreateProject_NameIsIdentityAndCallsRAOnce(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := NewManager(fake, nil, nil)

	id, err := m.CreateProject(rc(), OwnerScope("alice@example.com"), "my-cool-system")
	if err != nil {
		t.Fatalf("CreateProject: unexpected error: %v", err)
	}
	if id != ProjectID("my-cool-system") {
		t.Fatalf("CreateProject: id = %q, want the user-supplied name (name-as-identity)", id)
	}
	if fake.createCalls != 1 {
		t.Fatalf("CreateProject: RA called %d times, want 1", fake.createCalls)
	}
	if fake.createID != projectstate.ProjectID("my-cool-system") {
		t.Fatalf("CreateProject: RA id %s, want my-cool-system", fake.createID)
	}
	if fake.createOwner != projectstate.OwnerScope("alice@example.com") {
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

	_, err := m.CreateProject(rc(), OwnerScope(""), "My Project")
	if err == nil {
		t.Fatal("CreateProject: expected error for empty owner")
	}
	var me *fwm.Error
	if !errors.As(err, &me) || me.Kind != fwm.ContractMisuse {
		t.Fatalf("CreateProject: want ContractMisuse, got %v", err)
	}
	if fake.createCalls != 0 {
		t.Fatalf("CreateProject: RA should not be called on validation failure, got %d", fake.createCalls)
	}
}

func TestCreateProject_EmptyName_ContractMisuse(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := NewManager(fake, nil, nil)

	_, err := m.CreateProject(rc(), OwnerScope("alice"), "")
	if err == nil {
		t.Fatal("CreateProject: expected error for empty name")
	}
	var me *fwm.Error
	if !errors.As(err, &me) || me.Kind != fwm.ContractMisuse {
		t.Fatalf("CreateProject: want ContractMisuse, got %v", err)
	}
}

func TestCreateProject_RAConflict_MapsInfrastructure(t *testing.T) {
	fake := &fakeProjectStateAccess{createErr: fwra.New(fwra.Conflict, "row exists")}
	m := NewManager(fake, nil, nil)

	_, err := m.CreateProject(rc(), OwnerScope("alice"), "P")
	var me *fwm.Error
	if !errors.As(err, &me) {
		t.Fatalf("CreateProject: want fwm.Error, got %v", err)
	}
	if me.Kind != fwm.Infrastructure {
		t.Fatalf("CreateProject: Conflict should map to Infrastructure, got %v", me.Kind)
	}
}

// --- CreateProject: adopt + workflow seating + call order -------------------

type callOrder struct{ seq []string }

func (c *callOrder) record(name string) { c.seq = append(c.seq, name) }

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

type fakeRepoRef string

func (r fakeRepoRef) IsZero() bool   { return r == "" }
func (r fakeRepoRef) String() string { return string(r) }

type fakeCred struct{}

func (fakeCred) IsZero() bool { return false }

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

// TestCreateProject_AdoptThenSeatThenCreate is the load-bearing call-order guarantee:
// adopt → mint-credential → seat-workflow → create, all under the SAME idempotency key.
func TestCreateProject_AdoptThenSeatThenCreate(t *testing.T) {
	order := &callOrder{}
	ps := &orderingProjectState{fakeProjectStateAccess: &fakeProjectStateAccess{}, order: order}
	sc := &fakeSourceControl{order: order}
	m := NewManager(ps, sc, nil)

	id, err := m.CreateProject(rc(), OwnerScope("alice@example.com"), "my-cool-system")
	if err != nil {
		t.Fatalf("CreateProject: unexpected error: %v", err)
	}
	if id != ProjectID("my-cool-system") {
		t.Fatalf("CreateProject: id = %q, want name-as-identity my-cool-system", id)
	}
	if sc.adoptCalls != 1 || sc.mintCalls != 1 || sc.workflowCalls != 1 {
		t.Fatalf("source-control call counts: adopt=%d mint=%d workflow=%d, want 1 each",
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
	if sc.adoptSpec.RepoName != string(id) {
		t.Fatalf("AdoptProjectRepo RepoName = %q, want %q (name-as-identity)", sc.adoptSpec.RepoName, string(id))
	}
	if sc.adoptSpec.Title != "my-cool-system" {
		t.Fatalf("AdoptProjectRepo Title = %q, want my-cool-system", sc.adoptSpec.Title)
	}
	if sc.adoptKey != ps.createKey || sc.workflowKey != ps.createKey {
		t.Fatalf("idempotency keys diverged: adopt=%q workflow=%q create=%q",
			sc.adoptKey, sc.workflowKey, ps.createKey)
	}
}

// TestCreateProject_AdoptFailure_NoSeatingNoCreate proves the order is also a GATE.
func TestCreateProject_AdoptFailure_NoSeatingNoCreate(t *testing.T) {
	order := &callOrder{}
	ps := &orderingProjectState{fakeProjectStateAccess: &fakeProjectStateAccess{}, order: order}
	sc := &fakeSourceControl{order: order, adoptErr: fwra.New(fwra.Transient, "github 503")}
	m := NewManager(ps, sc, nil)

	_, err := m.CreateProject(rc(), OwnerScope("alice"), "taken-repo")
	if err == nil {
		t.Fatal("CreateProject: expected error when adopt fails")
	}
	var me *fwm.Error
	if !errors.As(err, &me) {
		t.Fatalf("CreateProject: want fwm.Error, got %v", err)
	}
	if me.Kind != fwm.Infrastructure {
		t.Fatalf("adopt Transient should map to Infrastructure, got %v", me.Kind)
	}
	if sc.adoptCalls != 1 {
		t.Fatalf("AdoptProjectRepo called %d times, want 1", sc.adoptCalls)
	}
	if sc.mintCalls != 0 || sc.workflowCalls != 0 {
		t.Fatalf("seating must NOT run after adopt failure: mint=%d workflow=%d", sc.mintCalls, sc.workflowCalls)
	}
	if ps.createCalls != 0 {
		t.Fatalf("projectState.CreateProject must NOT be called after an adopt failure, got %d", ps.createCalls)
	}
	if len(order.seq) != 1 || order.seq[0] != "adoptProjectRepo" {
		t.Fatalf("call order = %v, want [adoptProjectRepo] only", order.seq)
	}
}

// TestCreateProject_NilSourceControl_SkipsAdopt proves a credential-free dev server
// (nil sourceControl) still creates projects — repo-less, no adopt.
func TestCreateProject_NilSourceControl_SkipsAdopt(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := NewManager(fake, nil, nil)

	id, err := m.CreateProject(rc(), OwnerScope("alice"), "dev-project")
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
	src := []projectstate.ProjectSummary{
		{ProjectID: "alpha", Name: "A", Owner: "alice", Phase: projectstate.PhaseSystemDesign, CommittedCount: 2, TotalCount: 8, UpdatedAt: now},
		{ProjectID: "beta", Name: "B", Owner: "alice", Phase: projectstate.PhaseProjectDesign, CommittedCount: 9, TotalCount: 9, UpdatedAt: now},
	}
	fake := &fakeProjectStateAccess{listSummary: src}
	m := NewManager(fake, nil, nil)

	got, err := m.ListProjects(rc(), OwnerScope("alice"))
	if err != nil {
		t.Fatalf("ListProjects: unexpected error: %v", err)
	}
	if fake.listCalls != 1 || fake.listOwner != projectstate.OwnerScope("alice") {
		t.Fatalf("ListProjects: RA calls=%d owner=%q", fake.listCalls, fake.listOwner)
	}
	if len(got) != len(src) {
		t.Fatalf("ListProjects: got %d summaries, want %d", len(got), len(src))
	}
	if got[0].ProjectID != ProjectID("alpha") || got[0].Name != "A" || got[0].Owner != OwnerScope("alice") {
		t.Fatalf("ListProjects: summary[0] identity mismatch: %+v", got[0])
	}
	if got[0].Phase != PhaseSystemDesign || got[0].CommittedCount != 2 || got[0].TotalCount != 8 {
		t.Fatalf("ListProjects: summary[0] progress mismatch: %+v", got[0])
	}
	if got[1].ProjectID != ProjectID("beta") || got[1].Phase != PhaseProjectDesign {
		t.Fatalf("ListProjects: summary[1] mismatch: %+v", got[1])
	}
}

func TestListProjects_RAError_MapsInfrastructure(t *testing.T) {
	fake := &fakeProjectStateAccess{listErr: fwra.New(fwra.Infrastructure, "db down")}
	m := NewManager(fake, nil, nil)

	_, err := m.ListProjects(rc(), OwnerScope("alice"))
	var me *fwm.Error
	if !errors.As(err, &me) || me.Kind != fwm.Infrastructure {
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
		Mission: projectstate.ArtifactSlot{
			Status: projectstate.ReviewCommitted,
			Model:  &projectstate.MissionStatement{},
		},
		Glossary: projectstate.ArtifactSlot{
			Status: projectstate.ReviewAwaitingReview,
			Model:  &projectstate.Glossary{},
			Notes:  "needs trimming",
		},
		Volatilities: projectstate.ArtifactSlot{
			Status: projectstate.ReviewRejected,
			Model:  &projectstate.Volatilities{},
			Notes:  "redo",
		},
	}
}

func TestGetProject_MapsAggregateToTypedSlots(t *testing.T) {
	id := ProjectID("my-cool-system")
	fake := &fakeProjectStateAccess{readProject: sampleProject(projectstate.ProjectID(id))}
	m := NewManager(fake, nil, nil)

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: unexpected error: %v", err)
	}
	if fake.readCalls != 1 || fake.readID != projectstate.ProjectID(id) {
		t.Fatalf("GetProject: RA calls=%d id=%s", fake.readCalls, fake.readID)
	}
	if st.ProjectID != id || st.Name != "Sample" || st.Owner != OwnerScope("alice") {
		t.Fatalf("GetProject: identity mismatch: %+v", st)
	}
	if st.Phase != PhaseSystemDesign || st.Version != 7 {
		t.Fatalf("GetProject: phase/version mismatch: %+v", st)
	}
	if len(st.Research.Sources) != 1 || st.Research.Sources[0].Title != "Brief" {
		t.Fatalf("GetProject: research not mapped: %+v", st.Research)
	}

	if len(st.Slots) != len(projectstate.AllArtifactKinds()) {
		t.Fatalf("GetProject: got %d slots, want %d", len(st.Slots), len(projectstate.AllArtifactKinds()))
	}

	mission, _ := slotByKind(st, projectstate.KindMission)
	if mission.Stage != StageCommitted {
		t.Fatalf("Mission stage = %v, want StageCommitted", mission.Stage)
	}
	if mission.Model.Kind != "mission" || mission.Model.Model == nil {
		t.Fatalf("Mission model not mapped opaquely: %+v", mission.Model)
	}

	glossary, _ := slotByKind(st, projectstate.KindGlossary)
	if glossary.Stage != StageAwaitingReview {
		t.Fatalf("Glossary stage = %v, want StageAwaitingReview", glossary.Stage)
	}
	if glossary.Model.Model == nil {
		t.Fatal("Glossary model should be populated")
	}
	if glossary.Notes == nil || *glossary.Notes != "needs trimming" {
		t.Fatalf("Glossary notes = %v", glossary.Notes)
	}

	volatilities, _ := slotByKind(st, projectstate.KindVolatilities)
	if volatilities.Stage != StageRejected {
		t.Fatalf("Volatilities stage = %v, want StageRejected", volatilities.Stage)
	}

	scrubbed, _ := slotByKind(st, projectstate.KindScrubbedRequirements)
	if scrubbed.Stage != StageEmpty {
		t.Fatalf("ScrubbedRequirements stage = %v, want StageEmpty", scrubbed.Stage)
	}
	if scrubbed.Model.Model != nil {
		t.Fatal("ScrubbedRequirements model should be nil (empty slot)")
	}
	if scrubbed.Notes != nil {
		t.Fatal("ScrubbedRequirements notes should be nil (empty)")
	}
}

// decodeNetwork decodes the opaque Network slot model into the canonical projectstate
// type so the compute-at-read assertions can inspect the enriched figures.
func decodeNetwork(t *testing.T, st ProjectState) *projectstate.Network {
	t.Helper()
	slot, ok := slotByKind(st, projectstate.KindNetwork)
	if !ok || slot.Model.Model == nil {
		t.Fatalf("network slot model not present: %+v", slot.Model)
	}
	var n projectstate.Network
	if err := json.Unmarshal(*slot.Model.Model, &n); err != nil {
		t.Fatalf("decode network model: %v", err)
	}
	return &n
}

// TestGetProject_ComputeNetworkAtRead verifies the compute-at-read wiring over a small
// diamond network A→{B,C}→D with a milestone fanning in on D — read back through the
// opaque slot-model envelope.
func TestGetProject_ComputeNetworkAtRead(t *testing.T) {
	id := ProjectID("net-proj")
	p := sampleProject(projectstate.ProjectID(id))
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

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	netModel := decodeNetwork(t, st)
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
	if len(netModel.Dependencies) != 3 {
		t.Fatalf("authored dependencies perturbed: %v", netModel.Dependencies)
	}
	if got := netModel.CriticalPath; len(got) != 3 || got[0] != "A" || got[1] != "C" || got[2] != "D" {
		t.Fatalf("criticalPath not the computed float-0 set [A C D]: %v", got)
	}
	if len(netModel.Milestones) != 1 {
		t.Fatalf("milestones = %d, want 1", len(netModel.Milestones))
	}
	ms := netModel.Milestones[0]
	if ms.EventTime == nil || *ms.EventTime != 25 || ms.OnCriticalPath == nil || !*ms.OnCriticalPath {
		t.Fatalf("milestone compute wrong: %+v", ms)
	}
}

// TestGetProject_NilEstimator_NoCompute verifies the compute-at-read is a no-op when no
// estimator is injected: the authored network is served unenriched.
func TestGetProject_NilEstimator_NoCompute(t *testing.T) {
	id := ProjectID("net-proj")
	p := sampleProject(projectstate.ProjectID(id))
	p.Network = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: []projectstate.NetworkDependency{{Activity: "B", DependsOn: []string{"A"}}},
			CriticalPath: []string{"A", "B"},
		},
	}
	fake := &fakeProjectStateAccess{readProject: p}
	m := NewManager(fake, nil, nil)

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	net := decodeNetwork(t, st)
	if net.Summary != nil || len(net.Computed) != 0 {
		t.Fatalf("nil estimator should not compute: %+v", net)
	}
	if len(net.CriticalPath) != 2 || net.CriticalPath[0] != "A" {
		t.Fatalf("nil estimator should preserve authored criticalPath: %v", net.CriticalPath)
	}
}

// TestGetProject_OverwritesStaleCriticalPath verifies the served criticalPath[] is the
// COMPUTED float-0 activity set, not the stale authored names.
func TestGetProject_OverwritesStaleCriticalPath(t *testing.T) {
	id := ProjectID("net-proj")
	p := sampleProject(projectstate.ProjectID(id))
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
			CriticalPath: []string{"STALE", "GONE"},
		},
	}
	fake := &fakeProjectStateAccess{readProject: p}
	m := NewManager(fake, nil, estimation.New())

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	net := decodeNetwork(t, st)
	if len(net.CriticalPath) != 2 || net.CriticalPath[0] != "A" || net.CriticalPath[1] != "B" {
		t.Fatalf("criticalPath not overwritten with computed float-0 set: %v", net.CriticalPath)
	}
}

func TestGetProject_NotFoundPassesThrough(t *testing.T) {
	fake := &fakeProjectStateAccess{readErr: fwra.New(fwra.NotFound, "no row")}
	m := NewManager(fake, nil, nil)

	_, err := m.GetProject(rc(), ProjectID("missing"))
	var me *fwm.Error
	if !errors.As(err, &me) {
		t.Fatalf("GetProject: want fwm.Error, got %v", err)
	}
	if me.Kind != fwm.NotFound {
		t.Fatalf("GetProject: want NotFound, got %v", me.Kind)
	}
}

func TestGetProject_EmptyProjectID_ContractMisuse(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := NewManager(fake, nil, nil)

	_, err := m.GetProject(rc(), ProjectID(""))
	var me *fwm.Error
	if !errors.As(err, &me) || me.Kind != fwm.ContractMisuse {
		t.Fatalf("GetProject: want ContractMisuse, got %v", err)
	}
	if fake.readCalls != 0 {
		t.Fatal("GetProject: RA should not be called on nil id")
	}
}

// --- opaque envelope wire shape ---------------------------------------------

// TestProjectState_SlotWireShape proves the directly-serialized ArtifactSlotView marshals
// each slot with a STRING kind discriminator + the opaque {kind, model} envelope (the
// SAME wire shape the systemdesign session read emits), and that an empty slot omits the
// inner model payload (and notes).
func TestProjectState_SlotWireShape(t *testing.T) {
	id := ProjectID("my-cool-system")
	fake := &fakeProjectStateAccess{readProject: sampleProject(projectstate.ProjectID(id))}
	m := NewManager(fake, nil, nil)

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}

	data, err := json.Marshal(st.Slots)
	if err != nil {
		t.Fatalf("marshal slots: %v", err)
	}
	var wire []struct {
		Kind  string `json:"kind"`
		Stage int    `json:"stage"`
		Model struct {
			Kind  string          `json:"kind"`
			Model json.RawMessage `json:"model"`
		} `json:"model"`
		Notes *string `json:"notes"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	if len(wire) != len(projectstate.AllArtifactKinds()) {
		t.Fatalf("wire: got %d slots", len(wire))
	}
	byKind := map[string]int{}
	for i, w := range wire {
		byKind[w.Kind] = i
	}
	mission := wire[byKind["mission"]]
	if mission.Model.Kind != "mission" || len(mission.Model.Model) == 0 {
		t.Fatalf("wire: mission envelope wrong: %+v", mission.Model)
	}
	scrubbed := wire[byKind["scrubbedRequirements"]]
	if len(scrubbed.Model.Model) != 0 {
		t.Fatal("wire: empty slot should omit the inner model payload")
	}
	if scrubbed.Notes != nil {
		t.Fatal("wire: empty slot should omit notes")
	}
}
