package systemdesign

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// catalog.go holds the three CATALOG / cross-phase typed-read ops folded onto the
// systemDesignManager from the former projectManager (dissolved 2026-06-28): a
// project's permanent identity IS its living system design, so the project CATALOG
// + the cross-phase typed head-state read belong on this Manager. These ops own NO
// Temporal workflow; they are thin synchronous reads/writes over the published
// projectStateAccess (head state), sourceControlAccess (project-birth adopt + seat),
// and the constructionEstimationEngine (compute-at-read CPM + EV/SPI).
//
// SCHEMA-FIRST: the public surface (the 3 ops + the ProjectState projection types)
// is GENERATED into contract.gen.go from project.json .serviceContracts; this file
// is the hand-written impl on the unexported *systemDesignManager. The generated
// contract imports neither projectstate nor Temporal — the aggregate value shapes
// are field-mapped to the Manager's OWN contract types at the boundary, and the
// per-slot artifact MODEL is carried OPAQUELY as an {kind, raw-json} envelope.

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
// — a retry after a partial failure RE-CONVERGES rather than duplicating. The rail
// (sourceControlAccess) is optional: nil ⇒ repo-less create (a dev server with no
// GitHub App credentials).
func (m *systemDesignManager) CreateProject(rc fwm.Context, owner OwnerScope, name string) (ProjectID, error) {
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
	if m.rail != nil {
		repo, err := m.rail.AdoptProjectRepo(fwra.Context{Context: ctx, IdempotencyKey: key}, sourcecontrol.RepoAdoptionSpec{
			RepoName: name, // name-as-identity: the project id IS the repo name
			Title:    name,
		})
		if err != nil {
			return "", mapRAError(err)
		}
		cred, err := m.rail.GetInstallationToken(fwra.Context{Context: ctx}, repo)
		if err != nil {
			return "", mapRAError(err)
		}
		files, err := sourcecontrol.ManagedScaffoldFiles(repo)
		if err != nil {
			return "", mapRAError(err)
		}
		if _, err := m.rail.CommitManagedFiles(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, files, cred); err != nil {
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
func (m *systemDesignManager) ListProjects(rc fwm.Context, owner OwnerScope) ([]ProjectSummary, error) {
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
func (m *systemDesignManager) GetProject(rc fwm.Context, projectID ProjectID) (ProjectState, error) {
	ctx := rc.Context
	if projectID == "" {
		return ProjectState{}, newError(fwm.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return ProjectState{}, mapRAError(err)
	}
	m.computeNetworkAtRead(&proj)
	return m.projectStateToContract(proj), nil
}

// mapRAError translates a projectStateAccess / sourceControlAccess error into the
// Manager façade error model. fwra.NotFound → NotFound; fwra.ContractMisuse →
// ContractMisuse; everything else (incl. Conflict — a thin read/catalog op has no
// optimistic-concurrency loop to recover it) → Infrastructure with the original
// retryability preserved.
func mapRAError(err error) error {
	if err == nil {
		return nil
	}
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwm.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwm.ContractMisuse, err.Error())
		default:
			mapped := fwm.Wrap(fwm.Infrastructure, err, "projectStateAccess")
			mapped.Retryable = raErr.Retryable
			return mapped
		}
	}
	return newError(fwm.Infrastructure, err.Error())
}

// ---------------------------------------------------------------------------
// Compute-at-read enrichment (INTERNAL impl). Operates on the projectstate.Project
// aggregate BEFORE mapping to the contract.
// ---------------------------------------------------------------------------

// computeNetworkAtRead populates the Network slot's COMPUTE-AT-READ block (per-node CPM
// figures, criticality bands, milestone event times, summary) by running the
// constructionEstimationEngine.ComputeNetwork over the AUTHORED network × activity list.
// NO-OP when the estimator is nil or the Network slot has no authored model.
func (m *systemDesignManager) computeNetworkAtRead(p *projectstate.Project) {
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
// constructionEstimationEngine's OWN SLIM ActivityList at the call boundary.
func toEstimationActivityList(al projectstate.ActivityList) estimation.ActivityList {
	out := estimation.ActivityList{Activities: make([]estimation.ActivityItem, 0, len(al.Activities))}
	for _, a := range al.Activities {
		out.Activities = append(out.Activities, estimation.ActivityItem{Name: a.Name, EffortDays: a.EffortDays})
	}
	return out
}

// toEstimationNetwork converts the canonical projectstate.Network to the
// constructionEstimationEngine's OWN SLIM Network at the call boundary.
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
// projectstate → contract conversions (the Manager boundary).
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
// ProjectState transport shape. Read-time projections (each git row's prUrl/prNumber
// composed from m.repoBase + the opaque ref, and the EV/SPI earned-value curve from
// m.estimator) are sourced server-side here rather than re-derived by the webClient.
func (m *systemDesignManager) projectStateToContract(p projectstate.Project) ProjectState {
	return ProjectState{
		ProjectID:            ProjectID(p.ID),
		Name:                 p.Name,
		Owner:                OwnerScope(p.Owner),
		Phase:                Phase(int(p.Phase)),
		Version:              int64(p.Version),
		Research:             researchToContract(p.ResearchInput),
		Slots:                slotsToContract(p),
		GitRows:              m.gitRowsToContract(p.ActivityGit),
		ActivityConstruction: constructionRowsToContract(p.ActivityConstruction),
		ConstructionProgress: m.constructionProgressToContract(p),
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
// kind wire name + the concrete model's own JSON (nil when the slot is empty).
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

// notesPtr maps an architect-notes string to the optional contract field.
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
		return ArtifactStageAwaitingReview
	case projectstate.ReviewCommitted:
		return ArtifactStageCommitted
	case projectstate.ReviewRejected:
		return ArtifactStageRejected
	case projectstate.ReviewWithdrawn:
		return ArtifactStageWithdrawn
	default:
		return ArtifactStageEmpty
	}
}

// gitRowsToContract maps the per-activity git head-state map (honest-empty: nil in ⇒
// nil out). It composes each row's READ-TIME prUrl/prNumber projections from
// m.repoBase + the opaque pullRequestRef — the durable aggregate stays
// provider-opaque; prUrl/prNumber are pure read-time projections, never stored.
func (m *systemDesignManager) gitRowsToContract(rows map[string]projectstate.ActivityGitStatus) map[string]ActivityGitStatus {
	if len(rows) == 0 {
		return nil
	}
	out := make(map[string]ActivityGitStatus, len(rows))
	for id, g := range rows {
		prNumber, prURL := projectPRRef(g.PullRequestRef, m.repoBase)
		out[id] = ActivityGitStatus{
			ActivityID:     g.ActivityID,
			BranchName:     g.BranchName,
			BranchRef:      g.BranchRef,
			PullRequestRef: g.PullRequestRef,
			PrNumber:       int64(prNumber),
			PrURL:          prURL,
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

// projectPRRef is the SINGLE server-side site that turns the OPAQUE pullRequestRef into
// the SPA's two read-time render fields (D-PA-GIT-PRURL-ruling R1/R2). It isolates BOTH
// the "the opaque ref is a decimal PR number" assumption AND the GitHub "/pull/<n>" URL
// grammar to one place — the durable aggregate stays provider-opaque.
//
//   - prNumber: strconv.Atoi(ref). Zero (→ omitted by the web wire's omitempty) when ref
//     is "" (branch-only first touch) or unparseable — never panics, never fabricates.
//   - prURL: <repoBase>/pull/<ref>, ONLY when ref != "" AND repoBase != "". Otherwise "".
func projectPRRef(ref, repoBase string) (prNumber int, prURL string) {
	if ref == "" {
		return 0, ""
	}
	if n, err := strconv.Atoi(ref); err == nil {
		prNumber = n
	}
	if repoBase != "" {
		prURL = repoBase + "/pull/" + ref
	}
	return prNumber, prURL
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
// (nil in ⇒ nil out) AND computes the EV/SPI earned-value curve server-side via the
// constructionEstimationEngine (compute-at-read).
func (m *systemDesignManager) constructionProgressToContract(p projectstate.Project) *ConstructionProgress {
	cp := p.ConstructionProgress
	if cp == nil {
		return nil
	}
	return &ConstructionProgress{
		Week:           int64(cp.Week),
		TotalWeeks:     int64(cp.TotalWeeks),
		HandOffModel:   cp.HandOffModel,
		SupervisionCap: int64(cp.SupervisionCap),
		EV:             m.computeEVAtRead(p, int64(cp.TotalWeeks)),
	}
}

// computeEVAtRead computes the EV/SPI earned-value curve via the
// constructionEstimationEngine.ComputeEarnedValue over the AUTHORED activity list ×
// network, the integrated activity set, the calendar days/week, and the total-week
// framing. Zero EVCurve when the estimator is nil or inputs are degenerate.
func (m *systemDesignManager) computeEVAtRead(p projectstate.Project, totalWeeks int64) EVCurve {
	if m.estimator == nil {
		return EVCurve{}
	}
	var activities projectstate.ActivityList
	if al, ok := p.ActivityList.Model.(*projectstate.ActivityList); ok && al != nil {
		activities = *al
	}
	var network projectstate.Network
	if net, ok := p.Network.Model.(*projectstate.Network); ok && net != nil {
		network = *net
	}

	integrated := make([]string, 0, len(p.ActivityConstruction))
	for id, r := range p.ActivityConstruction {
		if r.BuildStatus == projectstate.BuildIntegrated {
			integrated = append(integrated, id)
		}
	}

	curve, err := m.estimator.ComputeEarnedValue(
		fweng.Context{Context: context.Background()},
		toEstimationActivityList(activities),
		toEstimationNetwork(network),
		integrated,
		totalWeeks,
		int64(calendarDaysPerWeek(p)),
	)
	if err != nil {
		return EVCurve{}
	}
	return EVCurve{Weeks: curve.Weeks, Earned: curve.Earned, Planned: curve.Planned, SPI: curve.SPI}
}

// calendarDaysPerWeek reads the working days/week from the PlanningAssumptions slot,
// defaulting to the standard 5-day workweek when the slot is absent or non-positive.
func calendarDaysPerWeek(p projectstate.Project) int {
	if pa, ok := p.PlanningAssumptions.Model.(*projectstate.PlanningAssumptions); ok && pa != nil && pa.CalendarDaysPerWeek > 0 {
		return int(pa.CalendarDaysPerWeek)
	}
	return 5
}

// serviceContractsToContract maps the typed service-contract corpus (honest-empty:
// nil in ⇒ nil out) onto the web-transport ServiceContract DTO. The contract
// DOCUMENT (its `interface` operations resolved against the document's `$defs`) is
// the source of truth: each op's parameters become input ContractStructs, its result
// becomes an output ContractStruct, and — when the op can fail — the layer's typed
// error becomes a final output box. Every struct's fields are resolved from the
// referenced `$def`'s properties (order-preserved). This is what feeds the SPA's
// «interface» diagram boxes; nothing is fabricated and nothing is served empty.
func serviceContractsToContract(scs map[string]projectstate.ServiceContract) map[string]ServiceContract {
	if len(scs) == 0 {
		return nil
	}
	out := make(map[string]ServiceContract, len(scs))
	for name, sc := range scs {
		layerErr := layerErrorName(sc.Layer)
		anyError := false
		for _, op := range sc.Interface.Operations {
			if op.Error {
				anyError = true
				break
			}
		}
		errorModel := ""
		if anyError {
			errorModel = "Operations fail with " + layerErr + " — the typed " + sc.Layer + " fault."
		}
		out[name] = ServiceContract{
			Component:     sc.Component,
			Layer:         sc.Layer,
			Stereotype:    sc.Title,
			Ops:           opsFromInterface(sc.Interface, sc.Defs, layerErr),
			DataContracts: dataContractNames(sc.Defs),
			ErrorModel:    errorModel,
		}
	}
	return out
}

// opsFromInterface derives the transport op list from the contract document's
// interface, resolving each op's params/result/error against the document's `$defs`
// into the input/output ContractStructs + a `name(params) → (result, error)`
// signature the SPA diagram renders. Returns nil for an empty interface.
func opsFromInterface(iface projectstate.ContractInterface, defs map[string]json.RawMessage, layerErr string) []ContractOp {
	if len(iface.Operations) == 0 {
		return nil
	}
	out := make([]ContractOp, 0, len(iface.Operations))
	for _, op := range iface.Operations {
		inputs := make([]ContractStruct, 0, len(op.Params))
		for _, p := range op.Params {
			inputs = append(inputs, structFromSchema(p.Name, p.Schema, defs))
		}
		var outputs []ContractStruct
		if len(op.Result) > 0 {
			outputs = append(outputs, structFromSchema("result", op.Result, defs))
		}
		if op.Error {
			outputs = append(outputs, ContractStruct{
				Name:   layerErr,
				Fields: []GoField{{Name: "fault", Type: layerErr}},
			})
		}
		out = append(out, ContractOp{
			Signature: opSignature(op, layerErr),
			Inputs:    inputs,
			Outputs:   outputs,
		})
	}
	return out
}

// opSignature renders one operation as `name(p: T, …) → (Result, error)`, using the
// same `→` separator the SPA signature parser recognises. Pointer params are starred.
func opSignature(op projectstate.ContractOperation, layerErr string) string {
	params := make([]string, 0, len(op.Params))
	for _, p := range op.Params {
		t := schemaTypeName(p.Schema, defaultTypeName)
		if p.Pointer {
			t = "*" + t
		}
		params = append(params, p.Name+": "+t)
	}
	sig := op.Name + "(" + strings.Join(params, ", ") + ")"
	var rets []string
	if len(op.Result) > 0 {
		rets = append(rets, schemaTypeName(op.Result, defaultTypeName))
	}
	if op.Error {
		rets = append(rets, layerErr)
	}
	switch len(rets) {
	case 0:
		// no declared return
	case 1:
		sig += " → " + rets[0]
	default:
		sig += " → (" + strings.Join(rets, ", ") + ")"
	}
	return sig
}

// structFromSchema resolves one JSON Schema node into a ContractStruct: the box is
// titled with the node's resolved Go-ish type name, and its fields are the referenced
// `$def`'s (or inline object's) properties. A scalar / array / external type has no
// sub-fields, so it carries a single self-field named selfName so no box is empty.
func structFromSchema(selfName string, raw json.RawMessage, defs map[string]json.RawMessage) ContractStruct {
	typeName := schemaTypeName(raw, selfName)
	fields := objectFields(raw, defs)
	if len(fields) == 0 {
		fields = []GoField{{Name: selfName, Type: typeName}}
	}
	return ContractStruct{Name: typeName, Fields: fields}
}

const defaultTypeName = "value"

// layerErrorName maps a Method layer to its framework error type, the typed fault
// every op on that layer returns on failure.
func layerErrorName(layer string) string {
	switch strings.ToLower(layer) {
	case "resourceaccess":
		return "fwra.Error"
	case "engine":
		return "fweng.Error"
	case "manager":
		return "fwm.Error"
	default:
		return "error"
	}
}

// dataContractNames returns the document's `$defs` names (the data contracts),
// sorted for a deterministic wire order. nil when there are none.
func dataContractNames(defs map[string]json.RawMessage) []string {
	if len(defs) == 0 {
		return nil
	}
	names := make([]string, 0, len(defs))
	for k := range defs {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// schemaTypeName resolves a JSON Schema node to a Go-ish type name: an array → []T,
// an explicit x-go-type → that, a `$ref` → its base name, otherwise the mapped
// primitive. fallback is returned when the node is empty / unrecognised.
func schemaTypeName(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 {
		return fallback
	}
	var n struct {
		Ref     string          `json:"$ref"`
		Type    json.RawMessage `json:"type"`
		Items   json.RawMessage `json:"items"`
		XGoType string          `json:"x-go-type"`
	}
	if err := json.Unmarshal(raw, &n); err != nil {
		return fallback
	}
	if len(n.Items) > 0 {
		return "[]" + schemaTypeName(n.Items, fallback)
	}
	if n.XGoType != "" {
		return n.XGoType
	}
	if n.Ref != "" {
		return refBase(n.Ref)
	}
	return primitiveTypeName(n.Type, fallback)
}

// primitiveTypeName maps a JSON Schema `type` (a string OR a ["null", T] union) to a
// Go-ish primitive name.
func primitiveTypeName(rawType json.RawMessage, fallback string) string {
	if len(rawType) == 0 {
		return fallback
	}
	kind := ""
	var single string
	if err := json.Unmarshal(rawType, &single); err == nil {
		kind = single
	} else {
		var union []string
		if err := json.Unmarshal(rawType, &union); err == nil {
			for _, k := range union {
				if k != "null" {
					kind = k
					break
				}
			}
		}
	}
	switch kind {
	case "string":
		return "string"
	case "integer":
		return "int"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "object":
		return "object"
	case "array":
		return "[]any"
	case "":
		return fallback
	default:
		return kind
	}
}

// refBase returns the trailing name of a JSON Schema `$ref` (e.g. "#/$defs/Foo" → "Foo").
func refBase(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

// objectFields resolves a schema node's properties into ordered GoFields. It follows
// a single `$ref` into defs, then reads the resolved object's `properties` in
// declaration order (json.Decoder token stream preserves key order). Non-object
// nodes (scalars, arrays, enums) have no properties → nil.
func objectFields(raw json.RawMessage, defs map[string]json.RawMessage) []GoField {
	if len(raw) == 0 {
		return nil
	}
	var head struct {
		Ref        string          `json:"$ref"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil
	}
	if head.Ref != "" {
		target, ok := defs[refBase(head.Ref)]
		if !ok {
			return nil
		}
		return objectFields(target, defs)
	}
	if len(head.Properties) == 0 {
		return nil
	}
	return orderedProperties(head.Properties)
}

// orderedProperties decodes a JSON Schema `properties` object into ordered GoFields,
// preserving the on-disk key order. Each field's type is resolved from its schema and
// its name honours an `x-go-name` override when present.
func orderedProperties(props json.RawMessage) []GoField {
	dec := json.NewDecoder(bytes.NewReader(props))
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil
	}
	var fields []GoField
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fields
		}
		key, _ := keyTok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return fields
		}
		name := key
		var override struct {
			XGoName string `json:"x-go-name"`
		}
		if json.Unmarshal(val, &override) == nil && override.XGoName != "" {
			name = override.XGoName
		}
		fields = append(fields, GoField{Name: name, Type: schemaTypeName(val, key)})
	}
	return fields
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
