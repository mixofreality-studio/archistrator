package project

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// Compile-time proof the concrete Manager satisfies the generated ProjectManager
// port. Each op leads with the Manager-layer call Context (fwm.Context, embedding
// context.Context + the Principal); the *Manager derives ctx := rc.Context inside.
var _ ProjectManager = (*Manager)(nil)

// Manager is the projectManager façade — a THIN Manager over the project head-state
// aggregate (architecture.dsl `projectManager`). It holds NO Temporal client and owns
// NO durable workflow; it is the project CATALOG + cross-phase typed read the
// webClient talks to for the three non-co-authoring operations.
//
// At project BIRTH it drives the downward sourceControlAccess edges that ADOPT the
// user's repo and SEAT the agentic-design workflow file, synchronously before
// projectState.CreateProject, so a project is never cataloged without an adopted,
// workflow-seated source-control repo. sourceControl is optional (nil when the GitHub
// App is unconfigured — a dev server with no creds): when nil, project creation
// proceeds repo-less.
type Manager struct {
	projectState  ProjectStateAccess
	sourceControl SourceControlAccess         // optional; nil ⇒ repo-less create (dev, no creds)
	estimator     estimation.EstimationEngine // construction-estimation Engine; ComputeNetwork at read (compute-at-read)
}

// NewManager constructs a Manager over the (narrow) projectStateAccess port, the
// (optional) sourceControlAccess port, and the constructionEstimationEngine it calls
// at READ time to populate the network computed block (compute-at-read). Pass a nil
// SourceControlAccess to run repo-less (a dev server with no GitHub App credentials).
// The estimator is a pure, deterministic Engine — a downward Manager→Engine edge; nil
// disables the network compute (the read returns the authored network unenriched).
func NewManager(ps ProjectStateAccess, sc SourceControlAccess, estimator estimation.EstimationEngine) *Manager {
	return &Manager{projectState: ps, sourceControl: sc, estimator: estimator}
}

// CreateProject births a new project. NAME-AS-IDENTITY (C-PM-Δ): the USER supplies
// the repo name, which IS the project identity (project name == repo name). The
// supplied name is validated, then — IN ORDER, preserving the I-RA call-order
// guarantee + idempotent re-convergence — the Manager:
//
//  1. ADOPTS the user's existing repo (sourceControlAccess.AdoptProjectRepo).
//  2. SEATS the agentic-design workflow file: mint a short-lived credential, then
//     commit the claude-code-action DESIGN workflow file.
//  3. creates the head-state row (projectStateAccess.CreateProject), STRICTLY AFTER
//     the above, keyed on the repo name as identity.
//
// Returns the project id (== the adopted repo name). Validation errors (empty
// owner/name) surface as ContractMisuse before any RA call. Every write is idempotent
// — a retry after a partial failure RE-CONVERGES rather than duplicating.
func (m *Manager) CreateProject(rc fwm.Context, owner OwnerScope, name string) (ProjectID, error) {
	ctx := rc.Context
	if owner == "" {
		return "", newError(fwm.ContractMisuse, "empty owner")
	}
	if name == "" {
		return "", newError(fwm.ContractMisuse, "empty name")
	}

	// NAME-AS-IDENTITY: the user-supplied name IS the project identity == repo name.
	projectID := ProjectID(name)
	key := createProjectIdempotencyKey(projectID)

	// Adopt the user's existing repo + seat the workflow file FIRST (project birth,
	// before the head-state row). Skipped only when source-control is unconfigured
	// (nil) — a repo-less dev server. Every step is idempotent; a retry re-converges.
	if m.sourceControl != nil {
		repo, err := m.sourceControl.AdoptProjectRepo(ctx, RepoSpec{RepoName: name, Title: name}, key)
		if err != nil {
			return "", mapRAError(err)
		}
		cred, err := m.sourceControl.MintRepoCredential(ctx, repo)
		if err != nil {
			return "", mapRAError(err)
		}
		if err := m.sourceControl.SeatAgenticWorkflow(ctx, repo, cred, key); err != nil {
			return "", mapRAError(err)
		}
	}

	if _, err := m.projectState.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: key},
		projectstate.ProjectID(projectID), projectstate.OwnerScope(owner), name); err != nil {
		return "", mapRAError(err)
	}
	return projectID, nil
}

// createProjectIdempotencyKey derives the stable logical idempotency key for "create
// this project". The project id IS the user-supplied repo name and unique per
// project, so it is itself the natural dedup token.
func createProjectIdempotencyKey(projectID ProjectID) fwra.IdempotencyKey {
	return fwra.IdempotencyKey(fmt.Sprintf("%s:createProject", projectID))
}

// ListProjects returns the landing-grid catalog for owner, newest-first (the RA's
// ordering). A pass-through over projectStateAccess.ListProjects, mapped to the
// contract ProjectSummary.
func (m *Manager) ListProjects(rc fwm.Context, owner OwnerScope) ([]ProjectSummary, error) {
	ctx := rc.Context
	if owner == "" {
		return nil, newError(fwm.ContractMisuse, "empty owner")
	}
	summaries, err := m.projectState.ListProjects(fwra.Context{Context: ctx}, projectstate.OwnerScope(owner))
	if err != nil {
		return nil, mapRAError(err)
	}
	out := make([]ProjectSummary, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, summaryToContract(s))
	}
	return out, nil
}

// GetProject returns the full typed head-state for one project, mapping the
// projectstate.Project aggregate's named typed slots into the contract ProjectState.
// fwra.NotFound passes through as fwm.NotFound.
func (m *Manager) GetProject(rc fwm.Context, projectID ProjectID) (ProjectState, error) {
	ctx := rc.Context
	if projectID == "" {
		return ProjectState{}, newError(fwm.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return ProjectState{}, mapRAError(err)
	}
	m.computeNetworkAtRead(&proj)
	return projectStateToContract(proj), nil
}

// ---------------------------------------------------------------------------
// Compute-at-read enrichment (INTERNAL impl). Operates on the projectstate.Project
// aggregate BEFORE mapping to the contract — it casts the Network + ActivityList
// slots to typed and runs the estimation Engine, filling the Network slot's computed
// block. This is NOT contract surface (it field-maps into the Engine's slim Option-B
// types), so it does not force generating those types into project's contract.
// ---------------------------------------------------------------------------

// computeNetworkAtRead populates the Network slot's COMPUTE-AT-READ block (per-node CPM
// figures, criticality bands, milestone event times, summary) by running the
// constructionEstimationEngine.ComputeNetwork over the AUTHORED network × activity list.
// NO-OP when the estimator is nil or the Network slot has no authored model. The
// authored fields (dependencies, milestones) are preserved untouched; only the computed
// fields are filled. A compute error (a degenerate-input guard) is swallowed.
func (m *Manager) computeNetworkAtRead(p *projectstate.Project) {
	if m.estimator == nil {
		return
	}
	net, ok := p.Network.Model.(*projectstate.Network)
	if !ok || net == nil {
		return
	}
	var activities projectstate.ActivityList
	if al, alok := p.ActivityList.Model.(*projectstate.ActivityList); alok && al != nil {
		activities = *al
	}

	solution, err := m.estimator.ComputeNetwork(fweng.Context{Context: context.Background()}, toEstimationActivityList(activities), toEstimationNetwork(*net))
	if err != nil {
		return // degenerate input guard — serve the authored network unenriched
	}

	computed := make(map[string]projectstate.NetworkNodeCompute, len(solution.Nodes))
	for id, n := range solution.Nodes {
		computed[id] = projectstate.NetworkNodeCompute{
			EarliestStart:  n.EarliestStart,
			EarliestFinish: n.EarliestFinish,
			LatestStart:    n.LatestStart,
			LatestFinish:   n.LatestFinish,
			TotalFloat:     n.TotalFloat,
			FreeFloat:      n.FreeFloat,
			OnCriticalPath: n.OnCriticalPath,
			NearCritical:   n.NearCritical,
			Band:           n.Band,
			Column:         int(n.Column),
		}
	}
	net.Computed = computed

	// Overwrite the served criticalPath[] with the engine's computed float-0 ACTIVITY
	// set (the authored criticalPath[] may be stale). Sorted for a deterministic wire order.
	computedCP := make([]string, 0, len(solution.Nodes))
	for id, n := range solution.Nodes {
		if n.OnCriticalPath {
			computedCP = append(computedCP, id)
		}
	}
	sort.Strings(computedCP)
	net.CriticalPath = computedCP

	net.Summary = &projectstate.NetworkSummary{
		TotalDurationDays:         solution.Summary.TotalDurationDays,
		CriticalPathActivityCount: int(solution.Summary.CriticalPathActivityCount),
		CriticalPathDays:          solution.Summary.CriticalPathDays,
		MaxFloat:                  solution.Summary.MaxFloat,
		NearCriticalCount:         int(solution.Summary.NearCriticalCount),
	}

	// Merge the computed milestone facets back onto the authored milestone rows (matched
	// by id), preserving authored id/name/public/dependsOn order.
	computedByID := make(map[string]estimation.NetworkMilestoneSolution, len(solution.Milestones))
	for _, ms := range solution.Milestones {
		computedByID[ms.ID] = ms
	}
	for i := range net.Milestones {
		if ms, found := computedByID[net.Milestones[i].ID]; found {
			onCP := ms.OnCriticalPath
			event := ms.EventTime
			net.Milestones[i].OnCriticalPath = &onCP
			net.Milestones[i].EventTime = &event
		}
	}
}

// toEstimationActivityList converts the canonical projectstate.ActivityList to the
// constructionEstimationEngine's OWN SLIM ActivityList at the call boundary (Option B
// full encapsulation). ComputeNetwork reads only Name + EffortDays.
func toEstimationActivityList(al projectstate.ActivityList) estimation.ActivityList {
	out := estimation.ActivityList{Activities: make([]estimation.ActivityItem, 0, len(al.Activities))}
	for _, a := range al.Activities {
		out.Activities = append(out.Activities, estimation.ActivityItem{Name: a.Name, EffortDays: a.EffortDays})
	}
	return out
}

// toEstimationNetwork converts the canonical projectstate.Network to the
// constructionEstimationEngine's OWN SLIM Network at the call boundary. ComputeNetwork
// reads only the AUTHORED Dependencies + Milestones (it COMPUTES the rest).
func toEstimationNetwork(net projectstate.Network) estimation.Network {
	deps := make([]estimation.NetworkDependency, 0, len(net.Dependencies))
	for _, d := range net.Dependencies {
		deps = append(deps, estimation.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	var milestones []estimation.NetworkMilestone
	if len(net.Milestones) > 0 {
		milestones = make([]estimation.NetworkMilestone, 0, len(net.Milestones))
		for _, mlst := range net.Milestones {
			milestones = append(milestones, estimation.NetworkMilestone{Id: mlst.ID, DependsOn: mlst.DependsOn})
		}
	}
	return estimation.Network{Dependencies: deps, Milestones: milestones}
}

// ---------------------------------------------------------------------------
// projectstate → contract conversions (the Manager boundary). The aggregate's value
// shapes are field-mapped into project's OWN contract types so the generated contract
// imports no projectstate. The per-slot artifact model is carried OPAQUELY as a
// {kind, raw-json} envelope.
// ---------------------------------------------------------------------------

// summaryToContract maps a projectstate.ProjectSummary onto the contract ProjectSummary.
func summaryToContract(s projectstate.ProjectSummary) ProjectSummary {
	return ProjectSummary{
		ProjectID:      ProjectID(s.ProjectID),
		Name:           s.Name,
		Owner:          OwnerScope(s.Owner),
		Phase:          Phase(int(s.Phase)),
		CommittedCount: int64(s.CommittedCount),
		TotalCount:     int64(s.TotalCount),
		UpdatedAt:      s.UpdatedAt,
	}
}

// projectStateToContract maps the head-state Project aggregate to the contract
// ProjectState transport shape.
func projectStateToContract(p projectstate.Project) ProjectState {
	return ProjectState{
		ProjectID:            ProjectID(p.ID),
		Name:                 p.Name,
		Owner:                OwnerScope(p.Owner),
		Phase:                Phase(int(p.Phase)),
		Version:              int64(p.Version),
		Research:             researchToContract(p.ResearchInput),
		Slots:                slotsToContract(p),
		GitRows:              gitRowsToContract(p.ActivityGit),
		ActivityConstruction: constructionRowsToContract(p.ActivityConstruction),
		ConstructionProgress: constructionProgressToContract(p.ConstructionProgress),
		ServiceContracts:     serviceContractsToContract(p.ServiceContracts),
	}
}

// researchToContract maps the Phase-1 research corpus.
func researchToContract(r projectstate.ResearchInput) ResearchInput {
	sources := make([]ResearchSource, 0, len(r.Sources))
	for _, s := range r.Sources {
		sources = append(sources, ResearchSource{Title: s.Title, Content: s.Content})
	}
	return ResearchInput{Sources: sources}
}

// slotsToContract emits one ArtifactSlotView per defined ArtifactKind in the stable
// slot order, deriving each slot's Stage from its stored ArtifactReviewStatus and
// carrying its typed Model OPAQUELY (the {kind, raw-json} envelope).
func slotsToContract(p projectstate.Project) []ArtifactSlotView {
	kinds := projectstate.AllArtifactKinds()
	slots := make([]ArtifactSlotView, 0, len(kinds))
	for _, kind := range kinds {
		slot := slotForKind(p, kind)
		slots = append(slots, ArtifactSlotView{
			Kind:  kind.WireName(),
			Stage: stageForStatus(slot.Status),
			Model: encodeSlotModel(kind, slot.Model),
			Notes: notesPtr(slot.Notes),
		})
	}
	return slots
}

// encodeSlotModel carries the slot's typed model OPAQUELY: the canonical camelCase
// kind wire name + the concrete model's own JSON (nil when the slot is empty). project
// never names the concrete projectstate model types or the sealed ArtifactModel sum
// here — the model is marshaled to raw JSON and passed through to the SPA verbatim.
func encodeSlotModel(kind projectstate.ArtifactKind, m projectstate.ArtifactModel) ArtifactSlotModel {
	env := ArtifactSlotModel{Kind: kind.WireName()}
	if m != nil {
		if raw, err := json.Marshal(m); err == nil {
			rm := json.RawMessage(raw)
			env.Model = &rm
		}
	}
	return env
}

// notesPtr maps an architect-notes string to the optional contract field: nil for the
// empty string (omitted on the wire), &notes otherwise.
func notesPtr(notes string) *string {
	if notes == "" {
		return nil
	}
	n := notes
	return &n
}

// stageForStatus maps the stored per-slot ArtifactReviewStatus to the contract stage.
func stageForStatus(s projectstate.ArtifactReviewStatus) ArtifactStage {
	switch s {
	case projectstate.ReviewAwaitingReview:
		return StageAwaitingReview
	case projectstate.ReviewCommitted:
		return StageCommitted
	case projectstate.ReviewRejected:
		return StageRejected
	case projectstate.ReviewWithdrawn:
		return StageWithdrawn
	default:
		return StageEmpty
	}
}

// gitRowsToContract maps the per-activity git head-state map (honest-empty: nil in ⇒
// nil out, so the slot is omitted on the wire).
func gitRowsToContract(rows map[string]projectstate.ActivityGitStatus) map[string]ActivityGitStatus {
	if len(rows) == 0 {
		return nil
	}
	out := make(map[string]ActivityGitStatus, len(rows))
	for id, g := range rows {
		out[id] = ActivityGitStatus{
			ActivityID:     g.ActivityID,
			BranchName:     g.BranchName,
			BranchRef:      g.BranchRef,
			PullRequestRef: g.PullRequestRef,
			CICheck:        CICheckState(int(g.CICheck)),
			ArchApproved:   g.ArchApproved,
			Merged:         g.Merged,
			CRLabel:        g.CRLabel,
			IsRevert:       g.IsRevert,
			UpdatedAt:      g.UpdatedAt,
		}
	}
	return out
}

// constructionRowsToContract maps the per-activity construction head-state map
// (honest-empty: nil in ⇒ nil out).
func constructionRowsToContract(rows map[string]projectstate.ActivityConstructionStatus) map[string]ActivityConstructionStatus {
	if len(rows) == 0 {
		return nil
	}
	out := make(map[string]ActivityConstructionStatus, len(rows))
	for id, r := range rows {
		out[id] = ActivityConstructionStatus{
			ActivityID:    r.ActivityID,
			Type:          ActivityType(int(r.Type)),
			Kind:          ActivityType(int(r.Kind)),
			Variant:       TestingVariant(int(r.Variant)),
			Phase:         ActivityConstructionPhase(int(r.Phase)),
			Phases:        phasesToContract(r.Phases),
			CurrentPhase:  ActivityMethodPhase(string(r.CurrentPhase)),
			StartedAt:     r.StartedAt,
			CompletedAt:   r.CompletedAt,
			BuildStatus:   ActivityBuildStatus(int(r.BuildStatus)),
			Produced:      producedToContract(r.Produced),
			FailureReason: FailureReason(int(r.FailureReason)),
			FailureDetail: r.FailureDetail,
		}
	}
	return out
}

// phasesToContract maps the App-A internal phase-completion records.
func phasesToContract(phases []projectstate.PhaseCompletion) []PhaseCompletion {
	if len(phases) == 0 {
		return nil
	}
	out := make([]PhaseCompletion, 0, len(phases))
	for _, ph := range phases {
		out = append(out, PhaseCompletion{
			Phase:       ActivityMethodPhase(string(ph.Phase)),
			Weight:      int64(ph.Weight),
			Completed:   ph.Completed,
			CompletedAt: ph.CompletedAt,
			ArtifactRef: ph.ArtifactRef,
		})
	}
	return out
}

// producedToContract maps the produced-artifact cards.
func producedToContract(produced []projectstate.ProducedArtifact) []ProducedArtifact {
	if len(produced) == 0 {
		return nil
	}
	out := make([]ProducedArtifact, 0, len(produced))
	for _, p := range produced {
		out = append(out, ProducedArtifact{Kind: p.Kind, Title: p.Title, Source: p.Source, Produced: p.Produced, Note: p.Note})
	}
	return out
}

// constructionProgressToContract maps the project-level Phase-3 framing scalars
// (nil in ⇒ nil out).
func constructionProgressToContract(p *projectstate.ConstructionProgress) *ConstructionProgress {
	if p == nil {
		return nil
	}
	return &ConstructionProgress{
		Week:           int64(p.Week),
		TotalWeeks:     int64(p.TotalWeeks),
		HandOffModel:   p.HandOffModel,
		SupervisionCap: int64(p.SupervisionCap),
	}
}

// serviceContractsToContract maps the typed service-contract corpus (honest-empty:
// nil in ⇒ nil out).
func serviceContractsToContract(scs map[string]projectstate.ServiceContract) map[string]ServiceContract {
	if len(scs) == 0 {
		return nil
	}
	out := make(map[string]ServiceContract, len(scs))
	for name, sc := range scs {
		out[name] = ServiceContract{
			Component:     sc.Component,
			Layer:         sc.Layer,
			Stereotype:    sc.Stereotype,
			Volatility:    sc.Volatility,
			Status:        sc.Status,
			Inbound:       partiesToContract(sc.Inbound),
			Outbound:      partiesToContract(sc.Outbound),
			Ops:           opsToContract(sc.Ops),
			DataContracts: sc.DataContracts,
			ErrorModel:    sc.ErrorModel,
			Idempotency:   sc.Idempotency,
			Revisions:     revisionsToContract(sc.Revisions),
		}
	}
	return out
}

func partiesToContract(parties []projectstate.ContractParty) []ContractParty {
	if len(parties) == 0 {
		return nil
	}
	out := make([]ContractParty, 0, len(parties))
	for _, p := range parties {
		out = append(out, ContractParty{Name: p.Name, Layer: p.Layer, How: p.How})
	}
	return out
}

func opsToContract(ops []projectstate.ContractOp) []ContractOp {
	if len(ops) == 0 {
		return nil
	}
	out := make([]ContractOp, 0, len(ops))
	for _, op := range ops {
		out = append(out, ContractOp{
			Signature:  op.Signature,
			Stereotype: op.Stereotype,
			Note:       op.Note,
			Inputs:     structsToContract(op.Inputs),
			Outputs:    structsToContract(op.Outputs),
		})
	}
	return out
}

func structsToContract(structs []projectstate.ContractStruct) []ContractStruct {
	if len(structs) == 0 {
		return nil
	}
	out := make([]ContractStruct, 0, len(structs))
	for _, cs := range structs {
		fields := make([]GoField, 0, len(cs.Fields))
		for _, f := range cs.Fields {
			fields = append(fields, GoField{Name: f.Name, Type: f.Type, Note: f.Note})
		}
		out = append(out, ContractStruct{Name: cs.Name, Fields: fields})
	}
	return out
}

func revisionsToContract(revs []projectstate.ContractRevision) []ContractRevision {
	if len(revs) == 0 {
		return nil
	}
	out := make([]ContractRevision, 0, len(revs))
	for _, r := range revs {
		out = append(out, ContractRevision{Rev: r.Rev, At: r.At, By: r.By, ByActivity: r.ByActivity, Summary: r.Summary})
	}
	return out
}

// slotForKind reads the named slot for kind off the Project aggregate.
func slotForKind(p projectstate.Project, kind projectstate.ArtifactKind) projectstate.ArtifactSlot {
	switch kind {
	case projectstate.KindMission:
		return p.Mission
	case projectstate.KindGlossary:
		return p.Glossary
	case projectstate.KindScrubbedRequirements:
		return p.ScrubbedRequirements
	case projectstate.KindVolatilities:
		return p.Volatilities
	case projectstate.KindCoreUseCases:
		return p.CoreUseCases
	case projectstate.KindSystem:
		return p.SystemDesign
	case projectstate.KindOperationalConcepts:
		return p.OperationalConcepts
	case projectstate.KindStandardCheck:
		return p.StandardCheck
	case projectstate.KindPlanningAssumptions:
		return p.PlanningAssumptions
	case projectstate.KindActivityList:
		return p.ActivityList
	case projectstate.KindNetwork:
		return p.Network
	case projectstate.KindNormalSolution:
		return p.NormalSolution
	case projectstate.KindSubcriticalSolution:
		return p.SubcriticalSolution
	case projectstate.KindCompressedSolution:
		return p.CompressedSolution
	case projectstate.KindDecompressedSolution:
		return p.DecompressedSolution
	case projectstate.KindRiskModel:
		return p.RiskModel
	case projectstate.KindSdpReview:
		return p.SdpReview
	default:
		return projectstate.ArtifactSlot{}
	}
}
