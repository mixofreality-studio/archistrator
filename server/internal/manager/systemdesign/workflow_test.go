package systemdesign

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// =============================================================================
// C-MSD-Δ regression spine — the AGENTIC-PIVOT dispatch → observe → read-back
// child gate (systemDesignManager.md §0d). Method product → NO BDD; regression-
// first, black-box at the WIRE SEAM. The LLM is stubbed at the EXTERNAL agentic-job
// boundary — a FAKE constructionPipelineAccess (submit/observe) + a FAKE
// projectStateAccess serving the read-back model the Action "committed". The
// Manager under test is NOT faked; the workflow drives the REAL dispatch → observe
// → read-back → human-gate sequence over the Temporal in-memory test environment
// (testsuite.WorkflowTestSuite — no Docker, no dev server, runs under -short).
//
// Covers (the contract's required wire-level cases):
//   - happy DRAFT round (dispatch → observe(Succeeded) → read-back → AwaitingReview)
//   - a REDRAFT gets a DISTINCT idempotency key (distinct ActivityID per dispatch)
//   - the PM-critique SECOND round-trip (mission)
//   - PhaseFailed → StageDraftFailed (NOT perpetual Drafting) → human gate (anti-wedge)
//   - the suspend/resume reviewDecision gate unchanged (Approve commits / Withdraw)
//   - the parent sequence + seal are untouched
// =============================================================================

// ---- fakeProjectState: read-back + the human-gate thin-writes ----------------

// fakeProjectState serves a scripted head-state on ReadProject (the read-back of the
// model the Action committed) and records the human-gate thin-writes the Manager makes.
type fakeProjectState struct {
	mu sync.Mutex

	project  projectstate.Project // the head-state ReadProject returns (read-back)
	notFound bool                 // when true ReadProject returns fwra.NotFound

	staged    []projectstate.ArtifactModel
	committed []projectstate.ArtifactKind
	rejected  []rejectCall
	withdrawn []projectstate.ArtifactKind
	advanced  int

	version projectstate.Version
}

type rejectCall struct {
	kind  projectstate.ArtifactKind
	notes string
}

func (f *fakeProjectState) ReadProject(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return projectstate.Project{}, fwra.New(fwra.NotFound, "no row yet")
	}
	return f.project, nil
}

func (f *fakeProjectState) ReadProjectVersion(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return 0, fwra.New(fwra.NotFound, "no row yet")
	}
	return f.project.Version, nil
}

func (f *fakeProjectState) bump() projectstate.Version {
	f.version++
	return f.version
}

func (f *fakeProjectState) StageArtifactForReview(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, model projectstate.ArtifactModel) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.staged = append(f.staged, model)
	return f.bump(), nil
}

func (f *fakeProjectState) CommitArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, kind projectstate.ArtifactKind) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.committed = append(f.committed, kind)
	return f.bump(), nil
}

func (f *fakeProjectState) RejectArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, kind projectstate.ArtifactKind, notes string) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejected = append(f.rejected, rejectCall{kind: kind, notes: notes})
	// Mirror the real RA's Notes write: RejectArtifact stamps slot.Notes with the
	// architect's reject rationale. This is the COLLISION setup — a subsequent critique
	// read-back must read CritiqueVerdict, NOT these reject Notes. (The RA also clears
	// the critique carrier on transition; that invariant is covered by the projectstate
	// unit tests. Here the test scripts the carrier explicitly to model the next round's
	// critique commit, so we leave it to the script and only set Notes.)
	f.mutateSlotLocked(kind, func(s *projectstate.ArtifactSlot) {
		s.Notes = notes
	})
	return f.bump(), nil
}

// mutateSlotLocked applies fn to the served head-state's named slot. Caller holds mu.
// Used by the fakes to model the RA's slot mutations so the read-back reflects them.
func (f *fakeProjectState) mutateSlotLocked(kind projectstate.ArtifactKind, fn func(*projectstate.ArtifactSlot)) {
	switch kind {
	case projectstate.KindMission:
		fn(&f.project.Mission)
	case projectstate.KindGlossary:
		fn(&f.project.Glossary)
	case projectstate.KindScrubbedRequirements:
		fn(&f.project.ScrubbedRequirements)
	case projectstate.KindCoreUseCases:
		fn(&f.project.CoreUseCases)
	case projectstate.KindSystem:
		fn(&f.project.SystemDesign)
	}
}

// setSlotCritique scripts the served head-state's critique carrier for kind — what a
// critique Action would have committed. Safe for concurrent use (e.g. from onObserve).
func (f *fakeProjectState) setSlotCritique(kind projectstate.ArtifactKind, verdict, notes string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mutateSlotLocked(kind, func(s *projectstate.ArtifactSlot) {
		s.CritiqueVerdict = verdict
		s.CritiqueNotes = notes
	})
}

func (f *fakeProjectState) WithdrawArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, kind projectstate.ArtifactKind, _ string) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.withdrawn = append(f.withdrawn, kind)
	return f.bump(), nil
}

func (f *fakeProjectState) AdvancePhase(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.advanced++
	return f.bump(), nil
}

func (f *fakeProjectState) SetResearchInput(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ResearchInput) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) CreateProject(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.OwnerScope, _ string) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) ListProjects(_ fwra.Context, _ projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	return nil, nil
}

// ---- fakePipeline: the EXTERNAL agentic-job seam (constructionPipelineAccess) ---

// fakePipeline stands in for the claude-code-action DESIGN job at the WIRE seam. It
// records every submitted spec (so tests assert the ProjectID / artifact_kind /
// branch in DispatchInputs and the DISTINCT idempotency key per dispatch) and serves
// a scripted terminal phase per observe. By default a submitted job is observed
// PipelineSucceeded immediately (the job "ran, committed the JSON, CI went green").
type fakePipeline struct {
	mu sync.Mutex

	// phases is the scripted terminal phase per dispatch in order; once exhausted the
	// last entry repeats. Empty == always PipelineSucceeded.
	phases []PipelinePhase
	// diagnostic is attached to a failed/cancelled observation.
	diagnostic string

	submits []submitRecord
	// handleByName tracks the phase to return for each issued handle.
	handlePhase map[string]PipelinePhase
	nextID      int
	// onObserve, when set, is invoked on each observe (used to mutate the read-back
	// state mid-flight, e.g. clearing a critique note so the PM-revise loop terminates).
	onObserve func()
}

type submitRecord struct {
	projectID      ProjectID
	idempotencyKey fwra.IdempotencyKey
	dispatchInputs map[string]string
	// targetRepo / workflowFile capture the per-project-design-dispatch override so the
	// proof can assert the dispatch hit the per-project repo + aiarch-design.yml (not the
	// central construction repo + aiarch-construct.yml). Empty == dormant-rail fallback.
	targetRepo   string
	workflowFile string
}

func newFakePipeline(phases ...PipelinePhase) *fakePipeline {
	return &fakePipeline{phases: phases, handlePhase: map[string]PipelinePhase{}}
}

func (p *fakePipeline) SubmitConstructionPipeline(_ context.Context, spec PipelineSpec, key fwra.IdempotencyKey) (PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := len(p.submits)
	p.submits = append(p.submits, submitRecord{
		projectID:      spec.ProjectID,
		idempotencyKey: key,
		dispatchInputs: spec.DispatchInputs,
		targetRepo:     spec.TargetRepo,
		workflowFile:   spec.WorkflowFile,
	})
	phase := PipelineSucceeded
	if len(p.phases) > 0 {
		if idx < len(p.phases) {
			phase = p.phases[idx]
		} else {
			phase = p.phases[len(p.phases)-1]
		}
	}
	p.nextID++
	name := "design-run/" + uuid.NewString()
	p.handlePhase[name] = phase
	return PipelineHandle{Name: name}, nil
}

func (p *fakePipeline) ObserveConstructionPipeline(_ context.Context, handle PipelineHandle) (PipelineObservation, error) {
	p.mu.Lock()
	phase := p.handlePhase[handle.Name]
	hook := p.onObserve
	diag := p.diagnostic
	p.mu.Unlock()
	if hook != nil {
		hook()
	}
	obs := PipelineObservation{Phase: phase}
	if phase == PipelineFailed || phase == PipelineCancelled {
		obs.Diagnostic = diag
	}
	return obs, nil
}

var _ constructionPipelineAccess = (*fakePipeline)(nil)

// ---- helpers ----------------------------------------------------------------

func newWorkflows(ps *fakeProjectState, pipe *fakePipeline) *Workflows {
	return &Workflows{ProjectState: ps, Pipeline: pipe}
}

// registerCoAuthor registers the child gate workflow + its activities on the test
// env, exactly as RegisterWorker does in production (same stable names).
func registerCoAuthor(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.CoAuthorArtifactWorkflow, workflow.RegisterOptions{Name: ExecutionKindCoAuthor})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.DispatchDesignJobActivity)
	env.RegisterActivity(wf.ObserveDesignJobActivity)
	env.RegisterActivity(wf.StageArtifactForReviewActivity)
	env.RegisterActivity(wf.CommitArtifactActivity)
	env.RegisterActivity(wf.RejectArtifactActivity)
	env.RegisterActivity(wf.WithdrawArtifactActivity)
}

func registerPhaseAdvance(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.PhaseAdvanceWorkflow, workflow.RegisterOptions{Name: ExecutionKindPhaseAdvance})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.AdvancePhaseActivity)
}

func mustMission(t *testing.T) *projectstate.MissionStatement {
	t.Helper()
	m, err := projectstate.NewMissionStatement("ship value", []projectstate.Objective{{Number: 1, Statement: "be useful"}}, "components")
	if err != nil {
		t.Fatalf("NewMissionStatement: %v", err)
	}
	return m
}

func mustGlossary(t *testing.T) *projectstate.Glossary {
	t.Helper()
	g, err := projectstate.NewGlossary([]projectstate.GlossaryItem{{Term: "Aggregate", Definition: "a consistency boundary"}})
	if err != nil {
		t.Fatalf("NewGlossary: %v", err)
	}
	return g
}

func committedSlot(model projectstate.ArtifactModel) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: model}
}

// awaitingSlot mirrors what the Action commits + how the Manager reads it back: the
// Kind slot carries the typed Model the design job committed (the read-back source)
// plus the FIRST-CLASS PM-critique read-back carrier (D-MSD-Δ amendment) — distinct
// from the architect-reject Notes field. verdict is "" | "approve" | "revise";
// critiqueNotes rides a "revise" verdict.
func awaitingSlot(model projectstate.ArtifactModel, verdict, critiqueNotes string) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status:          projectstate.ReviewAwaitingReview,
		Model:           model,
		CritiqueVerdict: verdict,
		CritiqueNotes:   critiqueNotes,
	}
}

// systemReadBack builds a project whose System slot carries a committed-by-Action
// System model (the read-back source for a System draft) plus the committed priors.
func systemReadBack(t *testing.T, id ProjectID) projectstate.Project {
	t.Helper()
	return projectstate.Project{
		ID:                   projectstate.ProjectID(id),
		Version:              3,
		Mission:              committedSlot(mustMission(t)),
		Glossary:             committedSlot(mustGlossary(t)),
		ScrubbedRequirements: committedSlot(&projectstate.ScrubbedRequirements{}),
		Volatilities:         committedSlot(&projectstate.Volatilities{}),
		CoreUseCases:         committedSlot(&projectstate.CoreUseCases{}),
		SystemDesign:         awaitingSlot(&projectstate.System{}, "", ""),
	}
}

// ---- Tests: child gate dispatch → observe → read-back -----------------------

// Happy DRAFT round: the gate DISPATCHES (with the right ProjectID + artifact_kind +
// branch in DispatchInputs and an RA-controlled — Manager-supplied — idempotency
// key), OBSERVES to PipelineSucceeded, READS BACK the committed typed model, and
// suspends at AwaitingReview surfacing the typed Draft. System is architect-owned →
// a SINGLE dispatch (no PM critique).
func Test_CoAuthor_DraftRoundTrip_DispatchObserveReadBack_AwaitsReview(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline() // default: dispatch observed Succeeded
	wf := newWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(QuerySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageAwaitingReview {
			t.Fatalf("want StageAwaitingReview, got %d", view.Stage)
		}
		if view.Draft.Kind != "system" || view.Draft.Model == nil {
			t.Fatalf("expected a staged system read-back draft envelope, got %+v", view.Draft)
		}
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.staged) != 1 {
		t.Fatalf("want 1 staged read-back model, got %d", len(ps.staged))
	}
	if _, ok := ps.staged[0].(*projectstate.System); !ok {
		t.Fatalf("staged model is not *projectstate.System: %T", ps.staged[0])
	}
	// System is architect-owned: exactly ONE dispatch (no PM critique).
	if len(pipe.submits) != 1 {
		t.Fatalf("System draft must be a single dispatch, got %d submits", len(pipe.submits))
	}
	sub := pipe.submits[0]
	if sub.projectID != id {
		t.Fatalf("dispatch carried wrong ProjectID: %q", sub.projectID)
	}
	if sub.dispatchInputs[dispatchInputArtifactKind] != projectstate.KindSystem.String() {
		t.Fatalf("dispatch artifact_kind = %q, want %s", sub.dispatchInputs[dispatchInputArtifactKind], projectstate.KindSystem)
	}
	if sub.dispatchInputs[dispatchInputTargetBranch] == "" {
		t.Fatal("dispatch must carry a non-empty target_branch")
	}
	if sub.dispatchInputs[dispatchInputDesignPrompt] == "" {
		t.Fatal("dispatch must carry the composed design_prompt")
	}
	// The Manager MUST NOT set idempotency_token in DispatchInputs (RA-controlled).
	if _, present := sub.dispatchInputs["idempotency_token"]; present {
		t.Fatal("the Manager must NOT set idempotency_token in DispatchInputs (RA-controlled)")
	}
	if sub.idempotencyKey.IsZero() {
		t.Fatal("the dispatch Activity must supply a non-empty idempotency key")
	}
	// DORMANT-RAIL (non-git) preservation: with NO rail wired (newWorkflows), the
	// per-project-design-dispatch override is EMPTY, so the RA falls back to the
	// configured construction repo — the non-git / Postgres path is byte-unchanged.
	if sub.targetRepo != "" || sub.workflowFile != "" {
		t.Fatalf("dormant-rail dispatch must leave the per-project target empty, got repo=%q file=%q", sub.targetRepo, sub.workflowFile)
	}
}

// An Approve signal commits the read-back artifact via CommitArtifact(kind); the
// child gate returns CoAuthorApproved. Mission is PM-critiqued (critique observed
// Succeeded, read-back Notes empty == CritiqueApprove → straight to the human gate).
func Test_CoAuthor_Approve_Commits(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// Mission read-back: the slot carries the Action-committed Mission; the critique
	// carrier verdict is "approve" so the PM round-trip ratifies and the gate proceeds.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), projectstate.CritiqueVerdictApprove, ""),
	}}
	pipe := newFakePipeline() // draft Succeeded, critique Succeeded
	wf := newWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var outcome CoAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != CoAuthorApproved {
		t.Fatalf("want CoAuthorApproved, got %d", outcome)
	}
	if len(ps.committed) != 1 || ps.committed[0] != projectstate.KindMission {
		t.Fatalf("want CommitArtifact(KindMission), got %v", ps.committed)
	}
	// Mission is PM-critiqued: TWO dispatch round-trips (draft + critique).
	if len(pipe.submits) != 2 {
		t.Fatalf("Mission must dispatch draft + PM-critique (2 round-trips), got %d", len(pipe.submits))
	}
	if pipe.submits[1].dispatchInputs[dispatchInputArtifactKind] != projectstate.KindMission.String() {
		t.Fatal("the second dispatch must be the PM-critique over the same kind")
	}
}

// THE ANTI-WEDGE TEST. A dispatched job that reaches a TERMINAL FAILURE phase
// (PipelineFailed — drafting failed or the required CI validation check went red)
// must NOT crash the workflow and must NOT leave a perpetual StageDrafting. The
// session lands in the human-visible StageDraftFailed carrying the neutral
// Diagnostic, surfaced by getSessionState, and suspends on the SAME reviewDecision
// gate awaiting a human Retry/Withdraw. Withdraw ends gracefully.
func Test_CoAuthor_PhaseFailed_LandsInStageDraftFailed_NotPerpetualDrafting(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline(PipelineFailed)
	pipe.diagnostic = "aiarch-validate found 2 violations"
	wf := newWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(QuerySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		// The load-bearing anti-wedge assertion: NOT a perpetual Drafting.
		if view.Stage == StageDrafting {
			t.Fatal("a failed design job must NOT leave the session in perpetual StageDrafting (the wedge)")
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("want StageDraftFailed after a terminal failure phase, got %d", view.Stage)
		}
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("StageDraftFailed must carry a human FailureReason (the neutral diagnostic)")
		}
		// Suspended on the SAME reviewDecision gate — Withdraw ends it.
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after withdraw from the draft-failed gate")
	}
	// A ran-but-failed job is terminal-at-the-Manager — it is escalated to the human
	// gate, NOT a workflow crash.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal job failure must NOT fail the workflow, got: %v", err)
	}
	if len(ps.staged) != 0 {
		t.Fatalf("a failed draft must stage nothing, got %d", len(ps.staged))
	}
	if len(ps.withdrawn) != 1 {
		t.Fatalf("withdraw from the draft-failed gate must call WithdrawArtifact once, got %d", len(ps.withdrawn))
	}
}

// PhaseCancelled is likewise a terminal failure that lands in StageDraftFailed
// (never a perpetual Drafting).
func Test_CoAuthor_PhaseCancelled_LandsInStageDraftFailed(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline(PipelineCancelled)
	wf := newWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		enc, _ := env.QueryWorkflow(QuerySessionState)
		var view SessionStateView
		_ = enc.Get(&view)
		if view.Stage != StageDraftFailed {
			t.Fatalf("PhaseCancelled must land in StageDraftFailed, got %d", view.Stage)
		}
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("PhaseCancelled must not crash the workflow: %v", err)
	}
}

// A REDRAFT (the human Retry-via-Reject after a draft-failed gate) issues a SECOND
// dispatch with a DISTINCT idempotency key — a fresh, idempotent job, not a dedup of
// the stale one (the key is derived inside the dispatch Activity from a fresh
// ActivityID per ExecuteActivity invocation; N1).
func Test_CoAuthor_DraftFailedThenRetry_DistinctIdempotencyKey(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	// First dispatch fails; the retry dispatch (2nd) succeeds → reaches AwaitingReview.
	pipe := newFakePipeline(PipelineFailed, PipelineSucceeded)
	pipe.diagnostic = "transient CI flake"
	wf := newWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

	// Reject at the draft-failed gate → Retry-via-Reject → re-dispatch.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "retry please"}})
	}, 20*time.Second)
	// After the successful redraft reaches AwaitingReview, withdraw to end.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("a retry must issue a SECOND dispatch, got %d submits", len(pipe.submits))
	}
	k1 := pipe.submits[0].idempotencyKey
	k2 := pipe.submits[1].idempotencyKey
	if k1 == k2 {
		t.Fatalf("a redraft must get a DISTINCT idempotency key (fresh job), got identical %q", k1)
	}
	// The successful redraft staged the read-back model once.
	if len(ps.staged) != 1 {
		t.Fatalf("the recovered redraft must stage exactly once, got %d", len(ps.staged))
	}
}

// PM-CRITIQUE second round-trip with CritiqueRevise (read-back Notes non-empty):
// each round re-dispatches the architect draft BEFORE the human gate. When the PM
// critic never converges, the loop must NOT crash the workflow (the wedge) — after
// maxRedraftAttempts the committed draft is staged for the human gate with the
// unresolved critique surfaced as a WARNING finding (the architect makes the call).
// This proves BOTH the PM second round-trip AND the non-convergence anti-wedge.
func Test_CoAuthor_PMCritiqueRevise_SecondRoundTrip_StagesForHumanGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// The Mission slot's critique carrier is persistently "revise" with notes
	// (CritiqueRevise every round) — the critic never converges.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), projectstate.CritiqueVerdictRevise, "tighten the vision sentence"),
	}}
	pipe := newFakePipeline() // every draft + critique dispatch Succeeds
	wf := newWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(QuerySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageAwaitingReview {
			t.Fatalf("non-converging PM critique must stage for the human gate, got stage %d", view.Stage)
		}
		var sawWarning bool
		for _, f := range view.Findings {
			if string(f.RuleID) == "PM-CRITIQUE-UNRESOLVED" {
				sawWarning = true
			}
		}
		if !sawWarning {
			t.Fatalf("expected a PM-CRITIQUE-UNRESOLVED warning at the gate, got %+v", view.Findings)
		}
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewApprove})
	}, 90*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete (PM non-convergence must stage, not hang/crash)")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("PM non-convergence must NOT crash the workflow: %v", err)
	}
	// At least draft + critique per round, looped to maxRedraftAttempts: >2 dispatches,
	// proving the PM critique is a real SECOND round-trip that re-dispatches the draft.
	if len(pipe.submits) <= 2 {
		t.Fatalf("PM-revise must re-dispatch (draft+critique per round); got only %d dispatches", len(pipe.submits))
	}
	// Exactly one stage (the best-effort draft at the gate after the loop).
	if len(ps.staged) != 1 {
		t.Fatalf("want exactly one best-effort stage at the gate, got %d", len(ps.staged))
	}
}

// A Reject at the AwaitingReview gate calls RejectArtifact and loops back to a fresh
// DRAFT dispatch (the human-gate suspend/resume is unchanged). Approve after the
// redraft commits.
func Test_CoAuthor_Reject_LoopsToFreshDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline() // every dispatch Succeeds
	wf := newWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

	const rejectNotes = "rework the decomposition"
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: rejectNotes}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.rejected) != 1 || ps.rejected[0].kind != projectstate.KindSystem || ps.rejected[0].notes != rejectNotes {
		t.Fatalf("want one RejectArtifact(KindSystem, %q), got %v", rejectNotes, ps.rejected)
	}
	if len(ps.committed) != 1 {
		t.Fatalf("want one commit after redraft->approve, got %v", ps.committed)
	}
	// Reject loops to a FRESH dispatch: at least 2 draft dispatches.
	if len(pipe.submits) < 2 {
		t.Fatalf("a reject must re-dispatch a fresh draft, got %d submits", len(pipe.submits))
	}
}

// THE COLLISION REGRESSION (D-MSD-Δ amendment — the senior-identified bug). On a
// PM-critiqued kind, a Reject at the AwaitingReview gate writes slot.Notes (the
// architect's reject rationale). The loop then re-drafts and re-critiques with NO
// intervening Stage before the critique read-back. With the OLD Notes-as-carrier the
// critique read-back would misread the reject Notes as CritiqueRevise and re-loop on
// the architect's own words. With the first-class CritiqueVerdict carrier the reject
// Notes are IGNORED by the read-back: the scripted "approve" verdict drives it and the
// gate proceeds to Approve. This proves the carrier read-back does NOT collide with the
// frozen reject Notes field.
func Test_CoAuthor_RejectNotes_DoNotLeakIntoCritiqueReadBack(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// Mission starts with an "approve" critique carrier so the FIRST round ratifies.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), projectstate.CritiqueVerdictApprove, ""),
	}}
	pipe := newFakePipeline() // every draft + critique dispatch Succeeds
	// Each critique round "commits" an "approve" verdict into the slot carrier (the
	// awaitingSlot seed already holds it; the redraft round's critique re-asserts it on
	// observe). The architect's reject Notes (set by RejectArtifact) are left untouched,
	// so the test proves the read-back consults the FIRST-CLASS carrier, not Notes.
	pipe.onObserve = func() {
		ps.setSlotCritique(projectstate.KindMission, projectstate.CritiqueVerdictApprove, "")
	}
	wf := newWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

	const rejectNotes = "REJECT-RATIONALE: rework the vision — this MUST NOT be read as a PM verdict"

	// First gate: REJECT (writes slot.Notes = rejectNotes, clears the critique carrier).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: rejectNotes}})
	}, 30*time.Second)
	// Second gate (after redraft+approve-critique): APPROVE to commit and finish.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewApprove})
	}, 80*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.rejected) != 1 || ps.rejected[0].notes != rejectNotes {
		t.Fatalf("want one RejectArtifact carrying the reject rationale, got %v", ps.rejected)
	}
	// The reject Notes must NOT have driven an extra critique-revise loop. After the
	// reject round the critique APPROVES, so the gate reaches Approve and commits once.
	if len(ps.committed) != 1 || ps.committed[0] != projectstate.KindMission {
		t.Fatalf("the architect's reject Notes must NOT be read as a PM critique verdict; want a single commit, got committed=%v", ps.committed)
	}
}

// THE MISSING-VERDICT SAFE DEFAULT. A critique dispatch reaches PipelineSucceeded but
// the slot's CritiqueVerdict read-back carrier is EMPTY (the job claimed success yet
// committed no verdict). The safe rule is NOT a silent approve: the session lands in
// the human-visible StageDraftFailed (the same anti-wedge gate as a failed job),
// surfacing a FailureReason, and suspends awaiting Retry/Withdraw. Withdraw ends clean
// and NOTHING is committed (an unreviewed draft must never sail through as approved).
func Test_CoAuthor_CritiqueMissingVerdict_LandsInStageDraftFailed_NotSilentApprove(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// Mission draft read-back is fine, but the critique carrier verdict is EMPTY.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), "", ""),
	}}
	pipe := newFakePipeline() // both draft + critique dispatch observed Succeeded
	wf := newWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(QuerySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a missing critique verdict must land in StageDraftFailed (NOT silent approve), got stage %d", view.Stage)
		}
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("StageDraftFailed from a missing verdict must carry a human FailureReason")
		}
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after withdraw from the missing-verdict gate")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a missing critique verdict must NOT crash the workflow: %v", err)
	}
	if len(ps.committed) != 0 {
		t.Fatalf("a missing critique verdict must NEVER commit (no silent approve), got committed=%v", ps.committed)
	}
	if len(ps.staged) != 0 {
		t.Fatalf("a missing critique verdict must stage nothing, got %d", len(ps.staged))
	}
}

// ---- Tests: parent sequence (SystemDesignPhaseWorkflow) ---------------------

func registerPhase(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.SystemDesignPhaseWorkflow, workflow.RegisterOptions{Name: ExecutionKindPhase})
	env.RegisterWorkflowWithOptions(wf.CoAuthorArtifactWorkflow, workflow.RegisterOptions{Name: ExecutionKindCoAuthor})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.AdvancePhaseActivity)
}

// The parent drives the seven steps in fixed order; each child Approve auto-advances;
// after step 7 the parent seals Phase 1. The child is MOCKED to Approve so this test
// isolates the parent's sequencing + seal (unchanged by the agentic pivot).
func Test_Phase_AllStepsApproved_SealsPhase1(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	proj := allPhase1Committed(t)
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows(ps, newFakePipeline())
	registerPhase(env, wf)

	env.OnWorkflow(ExecutionKindCoAuthor, mock.Anything, mock.Anything).
		Return(CoAuthorApproved, nil).Times(len(projectstate.Phase1RequiredKinds()))

	env.ExecuteWorkflow(ExecutionKindPhase, PhaseInput{ProjectID: ProjectID(proj.ID)})

	if !env.IsWorkflowCompleted() {
		t.Fatal("parent workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("parent workflow error: %v", err)
	}
	if ps.advanced != 1 {
		t.Fatalf("parent must seal Phase 1 exactly once after all steps approved, advanced=%d", ps.advanced)
	}
}

// If a child gate reports Withdraw, the parent HALTS and does not seal.
func Test_Phase_StepWithdrawn_HaltsSequence_NoSeal(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	proj := allPhase1Committed(t)
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows(ps, newFakePipeline())
	registerPhase(env, wf)

	env.OnWorkflow(ExecutionKindCoAuthor, mock.Anything, mock.Anything).
		Return(CoAuthorWithdrawn, nil).Once()

	env.ExecuteWorkflow(ExecutionKindPhase, PhaseInput{ProjectID: ProjectID(proj.ID)})

	if !env.IsWorkflowCompleted() {
		t.Fatal("parent workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("parent workflow error: %v", err)
	}
	if ps.advanced != 0 {
		t.Fatalf("a withdrawn step must NOT seal the phase, advanced=%d", ps.advanced)
	}
}

// ---- Tests: phase seal (PhaseAdvanceWorkflow) -------------------------------

// advancePhase returns MissingArtifacts when a required slot is uncommitted.
func Test_PhaseAdvance_Blocked_MissingArtifacts(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	proj := projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Mission: committedSlot(mustMission(t))}
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows(ps, newFakePipeline())
	registerPhaseAdvance(env, wf)

	env.ExecuteWorkflow(ExecutionKindPhaseAdvance, PhaseAdvanceInput{ProjectID: ProjectID(proj.ID)})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res PhaseAdvanceResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if res.Advanced {
		t.Fatal("want Advanced:false with a required slot uncommitted")
	}
	if len(res.MissingArtifacts) == 0 {
		t.Fatal("want a non-empty MissingArtifacts set")
	}
	missing := map[ArtifactKind]bool{}
	for _, k := range res.MissingArtifacts {
		missing[k] = true
	}
	if missing[KindMission] {
		t.Fatalf("Mission is committed and must NOT be missing: %v", res.MissingArtifacts)
	}
	if !missing[KindStandardCheck] {
		t.Fatalf("StandardCheck is uncommitted and must be missing: %v", res.MissingArtifacts)
	}
	if ps.advanced != 0 {
		t.Fatalf("blocked advance must NOT seal the phase, advanced=%d", ps.advanced)
	}
}

// advancePhase returns Advanced:true when all required slots are committed (the seal
// gates on all-committed; standard-check VALIDITY is the Action's CI check now).
func Test_PhaseAdvance_AllCommitted_Advances(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	proj := allPhase1Committed(t)
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows(ps, newFakePipeline())
	registerPhaseAdvance(env, wf)

	env.ExecuteWorkflow(ExecutionKindPhaseAdvance, PhaseAdvanceInput{ProjectID: ProjectID(proj.ID)})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res PhaseAdvanceResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("want Advanced:true, got %+v", res)
	}
	if ps.advanced != 1 {
		t.Fatalf("want AdvancePhase sealed once, advanced=%d", ps.advanced)
	}
}

// allPhase1Committed builds a Project with every Phase-1 required slot committed.
func allPhase1Committed(t *testing.T) projectstate.Project {
	t.Helper()
	g, err := projectstate.NewGlossary([]projectstate.GlossaryItem{{Term: "Aggregate", Definition: "a consistency boundary"}})
	if err != nil {
		t.Fatalf("NewGlossary: %v", err)
	}
	return projectstate.Project{
		ID:                   projectstate.ProjectID(uuid.NewString()),
		Version:              8,
		Mission:              committedSlot(mustMission(t)),
		Glossary:             committedSlot(g),
		ScrubbedRequirements: committedSlot(&projectstate.ScrubbedRequirements{}),
		Volatilities:         committedSlot(&projectstate.Volatilities{}),
		CoreUseCases:         committedSlot(&projectstate.CoreUseCases{}),
		SystemDesign:         committedSlot(&projectstate.System{}),
		OperationalConcepts:  committedSlot(&projectstate.OperationalConcepts{}),
		StandardCheck:        committedSlot(&projectstate.StandardCheck{}),
	}
}
