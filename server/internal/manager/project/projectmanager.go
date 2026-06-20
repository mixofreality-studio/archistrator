package project

import (
	"context"
	"fmt"
	"sort"

	"github.com/davidmarne/archistrator/server/internal/engine/estimation"
	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
	fwmanager "github.com/davidmarne/archistrator-platform/framework-go/manager"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// Manager is the projectManager façade — a THIN Manager over the project
// head-state aggregate (architecture.dsl `projectManager`). It holds NO Temporal
// client and owns NO durable workflow; it is the project CATALOG + cross-phase
// typed read the webClient talks to for the three non-co-authoring operations.
//
// At project BIRTH it drives the downward sourceControlAccess edges that ADOPT the
// user's repo and SEAT the agentic-design workflow file (caller-home ratified ==
// project birth), synchronously before projectState.CreateProject, so a project is
// never cataloged without an adopted, workflow-seated source-control repo.
// sourceControl is optional (nil when the GitHub App is unconfigured — a dev server
// with no creds): when nil, project creation proceeds repo-less so a credential-free
// dev stack still functions (the I-RA nil-guard, preserved).
//
// CORRECTION (2026-06-15, founder ruling): aiarch does NO secret management. The
// CLAUDE_CODE_OAUTH_TOKEN the design workflow reads is provisioned by the Claude Code
// GitHub App when the USER runs /install-github-app on their repo — a user onboarding
// prerequisite, never seated by aiarch. The old per-request agenticToken parameter +
// the WriteAgenticToken seating step are gone.
type Manager struct {
	projectState  ProjectStateAccess
	sourceControl SourceControlAccess         // optional; nil ⇒ repo-less create (dev, no creds)
	estimator     estimation.EstimationEngine // construction-estimation Engine; ComputeNetwork at read (compute-at-read)
}

// NewManager constructs a Manager over the (narrow) projectStateAccess port, the
// (optional) sourceControlAccess port, and the constructionEstimationEngine it calls
// at READ time to populate the network computed block (compute-at-read, founder gate
// 2026-06-19). Pass a nil SourceControlAccess to run repo-less (a dev server with no
// GitHub App credentials). The estimator is a pure, deterministic Engine — a downward
// Manager→Engine edge; nil disables the network compute (the read returns the authored
// network unenriched) so existing tests that pass nil keep compiling.
func NewManager(ps ProjectStateAccess, sc SourceControlAccess, estimator estimation.EstimationEngine) *Manager {
	return &Manager{projectState: ps, sourceControl: sc, estimator: estimator}
}

// CreateProject births a new project. NAME-AS-IDENTITY (C-PM-Δ, 2026-06-15): the
// USER supplies the repo name, which IS the project identity (project name == repo
// name); the server no longer mints a UUID. The supplied name is validated, then —
// IN ORDER, preserving the I-RA call-order guarantee + idempotent re-convergence —
// the Manager:
//
//  1. ADOPTS the user's existing repo (sourceControlAccess.AdoptProjectRepo).
//     Adopt is permissive (2026-06-16): it succeeds regardless of repo content; only
//     NotUnderInstallation surfaces as an error → creation does NOT proceed. (The
//     strict-empty RepoNotEmpty hard-fail is gone; a repo with committed .aiarch/state
//     is RESUMED by projectStateAccess.CreateProject in step 3.)
//  2. SEATS the agentic-design workflow file (caller-home ratified == project birth):
//     a. mints a short-lived credential (MintRepoCredential),
//     b. commits the claude-code-action DESIGN workflow file (SeatAgenticWorkflow).
//  3. creates the head-state row (projectStateAccess.CreateProject), STRICTLY AFTER
//     the above, keyed on the repo name as identity.
//
// Returns the project id (== the adopted repo name). Validation errors (empty
// owner/name) surface as ContractMisuse before any RA call.
//
// ORDER GUARANTEE: adopt → seat (workflow file) → create. Every write is idempotent —
// adopt re-converges on the repo name, the workflow file is overwrite-if-changed, and
// CreateProject dedups on the createProject idempotency key — so a retry after a
// partial failure RE-CONVERGES rather than duplicating. The repo handle is NOT
// threaded into projectState: the repo name IS the identity, so the handle is
// re-derivable, and the head-state stays free of a redundant (and provider-leaking)
// repo-ref column (sourceControlAccess.md §10.1 Q7).
//
// CORRECTION (2026-06-15, founder ruling): aiarch does NO secret management. The
// CLAUDE_CODE_OAUTH_TOKEN the design workflow reads is provisioned by the Claude Code
// GitHub App when the USER runs /install-github-app on their repo — a user onboarding
// prerequisite, never seated by aiarch. The old per-request agenticToken parameter +
// the WriteAgenticToken seating step are gone; seating is now just committing the
// workflow file (which still needs an installation token via MintRepoCredential).
func (m *Manager) CreateProject(ctx context.Context, owner OwnerScope, name string) (ProjectID, error) {
	if owner == "" {
		return "", newError(fwmanager.ContractMisuse, "empty owner")
	}
	if name == "" {
		return "", newError(fwmanager.ContractMisuse, "empty name")
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
		// Seat the agentic-design workflow file (committing it needs an installation
		// token). The OAuth secret it reads is user-provisioned via the Claude Code
		// GitHub App — not aiarch's concern.
		cred, err := m.sourceControl.MintRepoCredential(ctx, repo)
		if err != nil {
			return "", mapRAError(err)
		}
		if err := m.sourceControl.SeatAgenticWorkflow(ctx, repo, cred, key); err != nil {
			return "", mapRAError(err)
		}
	}

	if _, err := m.projectState.CreateProject(ctx, projectID, owner, name, key); err != nil {
		return "", mapRAError(err)
	}
	return projectID, nil
}

// createProjectIdempotencyKey derives the stable logical idempotency key for "create
// this project". The project id IS the user-supplied repo name and unique per
// project, so it is itself the natural dedup token: a retry carrying the same name
// collapses to a no-op across adopt, seating, and the RA dedup ledger.
func createProjectIdempotencyKey(projectID ProjectID) fwra.IdempotencyKey {
	return fwra.IdempotencyKey(fmt.Sprintf("%s:createProject", projectID))
}

// ListProjects returns the landing-grid catalog for owner, newest-first (the RA's
// ordering). A pass-through over projectStateAccess.ListProjects.
func (m *Manager) ListProjects(ctx context.Context, owner OwnerScope) ([]ProjectSummary, error) {
	if owner == "" {
		return nil, newError(fwmanager.ContractMisuse, "empty owner")
	}
	summaries, err := m.projectState.ListProjects(ctx, owner)
	if err != nil {
		return nil, mapRAError(err)
	}
	return summaries, nil
}

// GetProject returns the full typed head-state for one project, mapping the Project
// aggregate's named typed slots into ProjectState's stable-ordered ArtifactSlotView
// list. fwra.NotFound passes through as fwmanager.NotFound.
func (m *Manager) GetProject(ctx context.Context, projectID ProjectID) (ProjectState, error) {
	if projectID == "" {
		return ProjectState{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(ctx, projectID)
	if err != nil {
		return ProjectState{}, mapRAError(err)
	}
	m.computeNetworkAtRead(&proj)
	return projectStateFromAggregate(proj), nil
}

// computeNetworkAtRead populates the Network slot's COMPUTE-AT-READ block (per-node CPM
// figures, criticality bands, milestone event times, summary) by running the
// constructionEstimationEngine.ComputeNetwork over the AUTHORED network × activity list.
// This is the compute-at-read wiring (founder gate 2026-06-19): the math that formerly
// ran client-side (toNetworkView) now runs server-side on every read, so the SPA renders
// authoritative figures. It is a NO-OP when the estimator is nil (a test/dev Manager
// wired without one) or the Network slot has no authored model. The authored fields
// (dependencies, criticalPath, milestones) are preserved untouched; only the computed
// fields are filled. A compute error (only a degenerate-input guard) is swallowed — the
// authored network is still served unenriched rather than failing the whole read.
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

	solution, err := m.estimator.ComputeNetwork(activities, *net)
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
			Column:         n.Column,
		}
	}
	net.Computed = computed

	// Overwrite the served criticalPath[] with the engine's computed float-0 ACTIVITY set
	// (task #14, architect-ruled 2026-06-19). The authored criticalPath[] in project.json
	// is the stale PRE-agentic-pivot CP (27 names); the engine recomputes the authoritative
	// post-pivot float-0 set. Serving the computed set keeps the wire self-consistent — the
	// SPA's CP-highlight then matches the float-0 colouring from `computed[].onCriticalPath`.
	// Milestones are deliberately EXCLUDED (solution.Nodes holds only activities; milestones
	// carry their own onCriticalPath on the milestone objects), so criticalPath[] stays an
	// activity list per its existing semantics + the webApp NetworkView usage. Sorted for a
	// deterministic wire order.
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
		CriticalPathActivityCount: solution.Summary.CriticalPathActivityCount,
		CriticalPathDays:          solution.Summary.CriticalPathDays,
		MaxFloat:                  solution.Summary.MaxFloat,
		NearCriticalCount:         solution.Summary.NearCriticalCount,
	}

	// Merge the computed milestone facets back onto the authored milestone rows (matched
	// by id), preserving authored id/name/public/dependsOn order.
	computedByID := make(map[string]estimation.NetworkMilestoneSolution, len(solution.Milestones))
	for _, ms := range solution.Milestones {
		computedByID[ms.ID] = ms
	}
	for i := range net.Milestones {
		if ms, found := computedByID[net.Milestones[i].ID]; found {
			// Set the computed pointers non-nil so they emit on the wire even when the
			// computed value is false / 0 (the authored on-disk doc leaves them nil).
			onCP := ms.OnCriticalPath
			event := ms.EventTime
			net.Milestones[i].OnCriticalPath = &onCP
			net.Milestones[i].EventTime = &event
		}
	}
}

// projectStateFromAggregate maps the head-state Project aggregate to the typed
// ProjectState transport shape. It emits one ArtifactSlotView per defined
// ArtifactKind in the stable slot order, deriving each slot's Stage from its
// stored ArtifactReviewStatus and carrying its typed Model (nil when empty) and
// architect Notes. Findings/Critique are session-transient and absent from the
// head-state, so they are not mapped here.
func projectStateFromAggregate(p projectstate.Project) ProjectState {
	kinds := projectstate.AllArtifactKinds()
	slots := make([]ArtifactSlotView, 0, len(kinds))
	for _, kind := range kinds {
		slot := slotForKind(p, kind)
		slots = append(slots, ArtifactSlotView{
			Kind:  kind,
			Stage: stageForStatus(slot.Status),
			Model: slot.Model,
			Notes: slot.Notes,
		})
	}
	return ProjectState{
		ProjectID:            p.ID,
		Name:                 p.Name,
		Owner:                p.Owner,
		Phase:                p.Phase,
		Version:              p.Version,
		Research:             p.ResearchInput,
		Slots:                slots,
		GitRows:              p.ActivityGit,          // carried WHOLE (GIT.3); nil until the first git Record* in Phase 3
		ActivityConstruction: p.ActivityConstruction, // carried WHOLE; nil until Phase 3 RecordActivityStarted
		ConstructionProgress: p.ConstructionProgress, // carried WHOLE; nil until seeded by the bootstrap generator
		ServiceContracts:     p.ServiceContracts,     // carried WHOLE; nil until seeded by the construction bootstrapper
	}
}

// slotForKind reads the named slot for kind off the Project aggregate. Mirrors the
// closed slot set; an unknown kind (impossible for AllArtifactKinds) yields the
// zero slot.
func slotForKind(p projectstate.Project, kind ArtifactKind) projectstate.ArtifactSlot {
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
