package projectdesign

import (
	"context"
	"fmt"
	"math"
	"strings"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	billing "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// maxSDPReassembleAttempts bounds the SDP RejectAll re-assemble loop (contract
// §6.3 step 7 — bound the loop like systemdesign's maxRedraftAttempts).
const maxSDPReassembleAttempts = 5

// ===========================================================================
// (B) AssembleSDPReviewWorkflow — the UC2 spine (contract §6.3 B). Loop until
// Commit/RejectAll-exhausted:
//
//  1. readProject -> require committed PlanningAssumptions + ActivityList + Network
//     + the four Solution slots; missing -> non-retryable "sdp inputs incomplete".
//  2. ASSEMBLE the four ProjectOptions deterministically from the committed slots.
//  3. For EACH option call the three Engines DIRECTLY; JOIN into an SdpOptionRow.
//  4. Build the SdpReview (four rows + recommendation + rationale).
//  5. stageArtifactForReview(SdpReview) -> AwaitingReview.
//  6. awaitSignal("sdpDecision").
//  7. Commit(optionId) -> re-run the three Engines on the chosen option, re-stage
//     with Recommendation=chosen, commitArtifact(KindSdpReview), exit;
//     RejectAll(feedback) -> rejectArtifact(KindSdpReview), loop to step 2 (bounded).
// ===========================================================================

// sdpReviewInput is the start payload for AssembleSDPReviewWorkflow.
type sdpReviewInput struct {
	ProjectID ProjectID
}

// sdpDecisionSignal is the sdpDecision signal payload (contract §6.5).
type sdpDecisionSignal struct {
	Decision SDPDecision
	OptionID *OptionID
	Feedback *ReviewFeedback
	// Approver is the acting identity that made the SDP decision (PM-P2-4), recorded as the
	// SdpReview commit's approvedBy provenance on an Approve. Additive to the signal payload.
	Approver string
}

func (wf *workflows) AssembleSDPReviewWorkflow(ctx workflow.Context, in sdpReviewInput) error {
	state := &coAuthorState{
		projectID:    in.ProjectID,
		artifactKind: KindSdpReview,
		stage:        StageAssemblingSDP,
	}
	// SUB-STEP (Plan-3 C2): this workflow NEVER calls markActive — the SDP assembly is a
	// server-side deterministic join (assembleSdpReview), not an agentic dispatch, so there is
	// no role/step to stamp. state.activeRole/activeStep/activeRound stay at their zero value
	// (None/None/0) for the workflow's whole life, and view() reports that honestly.
	if err := workflow.SetQueryHandler(ctx, querySessionState, state.view); err != nil {
		return err
	}

	// feedback woven into the next assembly on a RejectAll loop.
	feedback := ""

	for attempt := 0; ; attempt++ {
		state.stage = StageAssemblingSDP

		// Step 1: read the committed Phase-2 inputs.
		proj, err := wf.readProject(ctx, in.ProjectID)
		if err != nil {
			if isReadNotFound(err) {
				return temporal.NewNonRetryableApplicationError(
					"sdp inputs incomplete (project has no state)", "SDPInputsIncomplete", nil)
			}
			return err
		}

		// Steps 2-4: assemble the four options, run the three Engines per option,
		// join into the SdpReview. Factored into a pure helper so it is unit-testable
		// without Temporal and so it stays deterministic (no clock, no RNG).
		review, asmErr := wf.assembleSdpReview(proj, feedback)
		if asmErr != nil {
			// A missing prerequisite is a non-retryable precondition failure; an Engine
			// error is escalated as a non-retryable terminal (the Manager mis-assembled
			// the option, or an engine invariant broke — neither is retryable).
			return asmErr
		}
		state.draft = review

		// Step 5: stageArtifactForReview(SdpReview) -> AwaitingReview.
		if err := wf.stageReview(ctx, in.ProjectID, review, &state.headVersion); err != nil {
			return err
		}
		state.stage = StageAwaitingReview

		// Step 6: awaitSignal("sdpDecision") — suspend durably.
		var sig sdpDecisionSignal
		workflow.GetSignalChannel(ctx, signalSDPDecision).Receive(ctx, &sig)

		// Step 7: branch on the architect's decision.
		switch sig.Decision {
		case SDPCommit:
			// Commit-time confirmation: bind the architect's chosen option, RE-RUN the
			// three engines on it, re-stage with Recommendation=chosen, then commit.
			if err := wf.sdpCommit(ctx, in, sig, proj, feedback, review, state); err != nil {
				return err
			}
			return nil

		case SDPRejectAll:
			notes := signalNotes(sig.Feedback)
			if err := wf.rejectReview(ctx, in.ProjectID, notes, &state.headVersion); err != nil {
				return err
			}
			if attempt+1 >= maxSDPReassembleAttempts {
				return temporal.NewNonRetryableApplicationError(
					"SDP review rejected past max re-assemble attempts", "SDPReassembleExhausted", nil)
			}
			feedback = notes
			state.stage = StageRedrafting
			continue

		case SDPDecisionUnknown:
			// The zero value: no legitimate signal carries it. Same terminal
			// rejection as the default case below.
			return temporal.NewNonRetryableApplicationError("unknown SDP decision", "UnknownSDPDecision", nil)

		default:
			return temporal.NewNonRetryableApplicationError("unknown SDP decision", "UnknownSDPDecision", nil)
		}
	}
}

// sdpCommit handles the SDPCommit arm of AssembleSDPReviewWorkflow: it binds the
// architect's chosen option, RE-RUNS the three engines on the chosen option (so the
// committed review records Recommendation=chosen), re-stages, and commits KindSdpReview.
// `confirmed` is byte-identical to the staged `review` the architect saw (assembleSdpReview
// is deterministic over the same `proj`, no clock/RNG — §6.8), so the option set cannot skew
// between the gate and the commit; we re-derive only to bind the choice. The workflow-command
// order is identical to the pre-extraction inline arm.
func (wf *workflows) sdpCommit(
	ctx workflow.Context,
	in sdpReviewInput,
	sig sdpDecisionSignal,
	proj projectstate.Project,
	feedback string,
	review *projectstate.SdpReview,
	state *coAuthorState,
) error {
	logger := workflow.GetLogger(ctx)

	if sig.OptionID == nil || !optionInReview(review, projectstate.OptionID(*sig.OptionID)) {
		return temporal.NewNonRetryableApplicationError(
			"SDP commit named an option not in the assembled review", "UnknownOption", nil)
	}
	chosen := projectstate.OptionID(*sig.OptionID)

	confirmed, cErr := wf.assembleSdpReview(proj, feedback)
	if cErr != nil {
		return cErr
	}
	confirmed.Recommendation = chosen
	confirmed.Rationale = fmt.Sprintf("architect committed option %s", chosen)
	state.draft = confirmed

	if err := wf.stageReview(ctx, in.ProjectID, confirmed, &state.headVersion); err != nil {
		return err
	}
	if err := wf.commitReview(ctx, in.ProjectID, &state.headVersion, sig.Approver); err != nil {
		return err
	}
	state.stage = StageCommitted
	logger.Info("SDP review committed", "option", string(chosen))
	return nil
}

// stageReview stages the SdpReview into its slot (status AwaitingReview), updating
// headVersion via the workflow-level Conflict loop.
func (wf *workflows) stageReview(ctx workflow.Context, projectID ProjectID, review *projectstate.SdpReview, headVersion *projectstate.Version) error {
	env, encErr := encodeModel(review)
	if encErr != nil {
		return fwmanager.MapError(encErr)
	}
	// The SDP review is assembled and staged directly on MAIN (no agentic draft rail /
	// session branch), so the Conflict re-read targets main (branch=="") — and the
	// generated designSessionAccess op's own empty-branch fallback stages on main.
	v, err := wf.applyRecovering(ctx, projectID, "", *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionStageArtifactForReviewOnBranch(ctx, projectstate.ProjectID(projectID), expected, "", env)
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// commitReview commits the SdpReview slot. approver is the acting SDP-decision identity,
// recorded as the commit's approvedBy provenance (PM-P2-4); the SDP review is assembled by
// the design manager, so draftedBy is the rail identity.
func (wf *workflows) commitReview(ctx workflow.Context, projectID ProjectID, headVersion *projectstate.Version, approver string) error {
	v, err := wf.applyRecovering(ctx, projectID, "", *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionCommitArtifactWithProvenance(ctx, projectstate.ProjectID(projectID), expected, projectstate.KindSdpReview, approver, railDraftedBy(0))
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// rejectReview records a rejected SdpReview outcome. Round=0/Comments=nil (the SDP review
// has no review-ledger thread) is BYTE-IDENTICAL to the pre-B9 behavior: the old
// RejectArtifactActivity already routed every call — including this zero-round,
// comment-less one — through the SAME LedgerProjectStateAccess.RejectArtifactOnBranchWithComments
// verb the generated invoker now reaches directly (the production ProjectStateAccess
// satisfies the ledger extension unconditionally), so this is not a new code path.
func (wf *workflows) rejectReview(ctx workflow.Context, projectID ProjectID, notes string, headVersion *projectstate.Version) error {
	v, err := wf.applyRecovering(ctx, projectID, "", *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionRejectArtifactOnBranchWithComments(ctx, projectstate.ProjectID(projectID), expected, "", projectstate.KindSdpReview, notes, 0, nil)
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// ===========================================================================
// Deterministic SDP-review assembly (contract §6.3 B steps 2-4). Pure helper —
// no clock, no RNG, no I/O — unit-testable without Temporal and replay-safe.
// ===========================================================================

// assembleSdpReview builds the four ProjectOptions from the committed Phase-2 head
// state, runs the three Engines per option, joins into the SdpReview, and picks the
// recommendation. Returns a non-retryable terminal on a missing prerequisite or an
// Engine error. feedback is woven into the Rationale on a re-assembly.
func (wf *workflows) assembleSdpReview(proj projectstate.Project, feedback string) (*projectstate.SdpReview, error) {
	pa, alErr := committedPlanningAssumptions(proj)
	if alErr != nil {
		return nil, sdpIncomplete(alErr)
	}
	al, alErr2 := committedActivityList(proj)
	if alErr2 != nil {
		return nil, sdpIncomplete(alErr2)
	}
	nw, nwErr := committedNetwork(proj)
	if nwErr != nil {
		return nil, sdpIncomplete(nwErr)
	}

	rows := make([]projectstate.SdpOptionRow, 0, len(projectstate.SolutionKinds()))
	for _, kind := range projectstate.SolutionKinds() {
		sol, sErr := committedSolution(proj, kind)
		if sErr != nil {
			return nil, sdpIncomplete(sErr)
		}
		opt := assembleOption(kind, pa, al, nw, sol)

		ce, eErr := wf.Estimation.EstimateForOption(fweng.Context{Context: context.Background()}, toEstimationOption(opt))
		if eErr != nil {
			return nil, escalateEngine("estimationEngine", kind, eErr)
		}
		of, oErr := wf.OperationEst.EstimateForOption(
			fweng.Context{Context: context.Background()},
			toOperationOption(opt),
			toOperationUsage(opt.DeclaredUsage),
			operationestimation.InfrastructureKind(opt.InfrastructureKind),
		)
		if oErr != nil {
			return nil, escalateEngine("operationEstimationEngine", kind, oErr)
		}
		proj2, pErr := wf.Settlement.ProjectCommitTimeRevenueShareAndComputeCost(fweng.Context{Context: context.Background()}, toSettlementOption(opt))
		if pErr != nil {
			return nil, escalateEngine("settlementEngine", kind, pErr)
		}

		rows = append(rows, projectstate.SdpOptionRow{
			OptionID:             opt.OptionID,
			SolutionKind:         kind,
			DurationDays:         ce.DurationDays,
			BuildCost:            toProjectStateMoneyFromEstimation(ce.BuildCost),
			CompositeRisk:        ce.Risk.Composite,
			ProjectedMonthlyCost: monthlyCostAtDeclaredLoad(of.UsageCostCurve),
			ExpectedPerCycleNet:  toProjectStateMoney(of.PayoutVsShortfallForecast.ExpectedPerCycleNet),
			RevenueSharePercent:  proj2.RevenueSharePercent,
		})
	}

	rec, rationale := recommendOption(rows)
	if feedback != "" {
		rationale = rationale + " (re-assembled with architect feedback: " + feedback + ")"
	}
	return &projectstate.SdpReview{Options: rows, Recommendation: rec, Rationale: rationale}, nil
}

// toSettlementOption converts the canonical projectstate option to the
// settlementEngine's OWN ProjectOption snapshot at the call boundary (Option B full
// encapsulation: the Engine redefines every domain type it uses as its own generated
// def and imports no projectstate, so the Manager maps field-by-field here). The
// Engine reads only the option's settlement Terms, so only OptionID + Terms cross.
func toSettlementOption(opt projectstate.ProjectOption) billing.ProjectOption {
	t := opt.Terms
	return billing.ProjectOption{
		OptionID: billing.OptionID(opt.OptionID),
		Terms: billing.BillingTerms{
			RevenueShare:         billing.RevenueShareKind(t.RevenueShare),
			RevenueSharePercent:  t.RevenueSharePercent,
			ComputeCost:          billing.ComputeCostKind(t.ComputeCost),
			ComputeMarkupPercent: t.ComputeMarkupPercent,
			Schedule:             billing.ScheduleKind(t.Schedule),
		},
	}
}

// toOperationOption converts the canonical projectstate option to the
// operationEstimationEngine's OWN slim ProjectOption snapshot at the call boundary
// (Option B full encapsulation: the Engine redefines every domain type it uses as its
// own generated def and imports no projectstate, so the Manager maps field-by-field
// here). The Engine reads only the option's settlement Terms, so only OptionID + Terms
// cross.
func toOperationOption(opt projectstate.ProjectOption) operationestimation.ProjectOption {
	t := opt.Terms
	return operationestimation.ProjectOption{
		OptionID: operationestimation.OptionID(opt.OptionID),
		Terms: operationestimation.SettlementTerms{
			RevenueShare:         operationestimation.RevenueShareKind(t.RevenueShare),
			RevenueSharePercent:  t.RevenueSharePercent,
			ComputeCost:          operationestimation.ComputeCostKind(t.ComputeCost),
			ComputeMarkupPercent: t.ComputeMarkupPercent,
			Schedule:             operationestimation.ScheduleKind(t.Schedule),
		},
	}
}

// toOperationUsage converts the canonical declared-usage snapshot to the
// operationEstimationEngine's OWN UsageAssumption at the call boundary. The integer
// fields widen to int64 in the generated contract def.
func toOperationUsage(u projectstate.UsageAssumption) operationestimation.UsageAssumption {
	return operationestimation.UsageAssumption{
		ExpectedDailyActiveUsers: int64(u.ExpectedDailyActiveUsers),
		RequestsPerMinute:        u.RequestsPerMinute,
		AvgPayloadBytes:          int64(u.AvgPayloadBytes),
	}
}

// assembleOption builds one ProjectOption by value from the committed Phase-2 slots
// (contract §6.3 B step 2). DETERMINISTIC — no clock, no RNG; the activity ordering
// is preserved from the ActivityList so the Engine join replays identically.
func assembleOption(
	kind projectstate.ArtifactKind,
	pa projectstate.PlanningAssumptions,
	al projectstate.ActivityList,
	nw projectstate.Network,
	sol projectstate.Solution,
) projectstate.ProjectOption {
	onCritical := make(map[string]bool, len(nw.CriticalPath))
	for _, name := range nw.CriticalPath {
		onCritical[name] = true
	}

	classSet := map[string]struct{}{}
	activities := make([]projectstate.OptionActivity, 0, len(al.Activities))
	for _, a := range al.Activities {
		classSet[a.WorkerClass] = struct{}{}
		activities = append(activities, projectstate.OptionActivity{
			ActivityID:  a.Name,
			EffortDays:  a.EffortDays,
			WorkerClass: a.WorkerClass,
			// OnCriticalPath/RiskBucket are authored METADATA only — the estimationEngine
			// computes its OWN per-option critical path + float-based risk from the network
			// (Phase-2 rework F2/F3); it no longer trusts these fields for the math.
			OnCriticalPath: onCritical[a.Name],
			RiskBucket:     a.RiskBucket,
		})
	}
	classes := make([]string, 0, len(classSet))
	for c := range classSet {
		classes = append(classes, c)
	}

	// Calendar is a SHARED planning assumption for EVERY option (Phase-2 rework F5): the
	// per-option Solution.CalendarDaysPerWeek "cheat" (compressed silently switching 2→5
	// d/wk) is retired. Compression now comes from a higher StaffingCap (parallelism where
	// the network allows) + the 30% exclusion guard in recommendOption.
	//
	// EARMARK (F5e — deferred): the book's SECOND compression lever, "top resources" (a
	// model-tier upgrade, e.g. junior sonnet→opus, that shortens critical-activity effort
	// at higher $/day), is NOT modeled here — cap-based parallelism cannot shorten below
	// the unconstrained critical path. When implemented, the compressed option would carry
	// a per-class effort/throughput multiplier fed to the estimationEngine.
	return projectstate.ProjectOption{
		OptionID:     projectstate.OptionID(kind.String()),
		SolutionKind: kind,
		Network:      projectstate.ActivityNetwork{Activities: activities},
		// AI-derived per-class $/day rates (Phase-2 rework F11) — not the old flat human rates.
		WorkerMix:           projectstate.WorkerMix{ClassRates: deriveClassRates(pa, classes), StaffingCap: sol.StaffingCap},
		CalendarDaysPerWeek: pa.CalendarDaysPerWeek,
		Terms:               pa.Terms,
		DeclaredUsage:       pa.DeclaredUsage,
		InfrastructureKind:  pa.InfrastructureKind,
		Dependencies:        nw.Dependencies,
		Milestones:          nw.Milestones,
		IndirectDailyRate:   indirectDailyRateOf(pa),
		BufferDays:          sol.BufferDays,
		// Top-resource compression lever (F5e): >1 for the compressed option, which speeds
		// up its critical path (shorter + riskier) at a convex cost premium in the engine.
		CriticalSpeedup: sol.CriticalSpeedup,
	}
}

// monthlyCostAtDeclaredLoad picks the UsageCostCurve point nearest LoadMultiplier==1.0
// (the declared-usage point). Deterministic.
func monthlyCostAtDeclaredLoad(curve operationestimation.UsageCostCurve) projectstate.Money {
	best := operationestimation.Money{}
	bestDist := math.MaxFloat64
	for _, p := range curve.Points {
		d := math.Abs(p.LoadMultiplier - 1.0)
		if d < bestDist {
			bestDist = d
			best = p.ProjectedMonthlyCost
		}
	}
	// Convert the Engine's OWN Money back to the canonical projectstate.Money at the
	// boundary (Option B full encapsulation).
	return toProjectStateMoney(best)
}

// toProjectStateMoney converts the operationEstimationEngine's OWN Money back to the
// canonical projectstate.Money at the call boundary (Option B full encapsulation).
func toProjectStateMoney(m operationestimation.Money) projectstate.Money {
	return projectstate.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

// toEstimationOption converts the canonical projectstate option to the
// estimationEngine's OWN SLIM ProjectOption snapshot at the call boundary
// (Option B full encapsulation: the Engine redefines every domain type it uses as its
// own generated def and imports no projectstate, so the Manager maps field-by-field
// here). The Engine reads only the construction-side network + worker mix + calendar,
// so only those (plus OptionID for audit) cross — the settlement Terms / declared usage
// / infra / solution kind do NOT. The generated WorkerMix.StaffingCap + OptionActivity.
// RiskBucket widen int → int64.
func toEstimationOption(opt projectstate.ProjectOption) estimation.ProjectOption {
	activities := make([]estimation.OptionActivity, 0, len(opt.Network.Activities))
	for _, a := range opt.Network.Activities {
		activities = append(activities, estimation.OptionActivity{
			ActivityId:     a.ActivityID,
			EffortDays:     a.EffortDays,
			WorkerClass:    a.WorkerClass,
			OnCriticalPath: a.OnCriticalPath,
			RiskBucket:     int64(a.RiskBucket),
		})
	}
	rates := make(map[string]estimation.Money, len(opt.WorkerMix.ClassRates))
	for cls, m := range opt.WorkerMix.ClassRates {
		rates[cls] = estimation.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
	}
	deps := make([]estimation.NetworkDependency, 0, len(opt.Dependencies))
	for _, d := range opt.Dependencies {
		deps = append(deps, estimation.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	milestones := make([]estimation.NetworkMilestone, 0, len(opt.Milestones))
	for _, m := range opt.Milestones {
		milestones = append(milestones, estimation.NetworkMilestone{Id: m.ID, DependsOn: m.DependsOn})
	}
	return estimation.ProjectOption{
		OptionId:            estimation.OptionID(opt.OptionID),
		Network:             estimation.ActivityNetwork{Activities: activities, Dependencies: deps, Milestones: milestones},
		WorkerMix:           estimation.WorkerMix{ClassRates: rates, StaffingCap: int64(opt.WorkerMix.StaffingCap)},
		CalendarDaysPerWeek: opt.CalendarDaysPerWeek,
		IndirectDailyRate:   estimation.Money{MinorUnits: opt.IndirectDailyRate.MinorUnits, Currency: opt.IndirectDailyRate.Currency},
		BufferDays:          opt.BufferDays,
		CriticalSpeedup:     opt.CriticalSpeedup,
	}
}

// toProjectStateMoneyFromEstimation converts the estimationEngine's OWN
// Money back to the canonical projectstate.Money at the call boundary (Option B full
// encapsulation).
func toProjectStateMoneyFromEstimation(m estimation.Money) projectstate.Money {
	return projectstate.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

// Exclusion-zone bounds (App C §4.7g–i; the-method-risk-modeling Step 5). Options with
// composite risk above tooRisky or below overSafe are OUT; a compressed option more than
// maxCompression shorter than normal is OUT (death-zone proximity / >30% rule, F8).
const (
	riskTooRisky   = 0.75
	riskOverSafe   = 0.30
	maxCompression = 0.30
)

// recommendOption applies the App C exclusion zones across the option rows, then picks the
// best IN-band option (lowest CompositeRisk, tie-break lowest DurationDays) — this is the
// cost-risk sweet spot the book expects to land on the decompressed-normal (F8). If every
// option is out of band it falls back to the lowest-risk row so the recommendation is never
// empty (management still needs a pointer, with the caveat in the rationale).
func recommendOption(rows []projectstate.SdpOptionRow) (projectstate.OptionID, string) {
	if len(rows) == 0 {
		return "", "no options assembled"
	}
	normalDur := 0.0
	for _, r := range rows {
		if r.SolutionKind == projectstate.KindNormalSolution {
			normalDur = r.DurationDays
		}
	}
	included := func(r projectstate.SdpOptionRow) bool {
		if r.CompositeRisk > riskTooRisky || r.CompositeRisk < riskOverSafe {
			return false
		}
		if normalDur > 0 && r.DurationDays < normalDur {
			if (normalDur-r.DurationDays)/normalDur > maxCompression {
				return false
			}
		}
		return true
	}
	pickBest := func(pred func(projectstate.SdpOptionRow) bool) (projectstate.SdpOptionRow, bool) {
		var best projectstate.SdpOptionRow
		found := false
		for _, r := range rows {
			if !pred(r) {
				continue
			}
			if !found || r.CompositeRisk < best.CompositeRisk ||
				(r.CompositeRisk == best.CompositeRisk && r.DurationDays < best.DurationDays) {
				best = r
				found = true
			}
		}
		return best, found
	}
	if best, ok := pickBest(included); ok {
		return best.OptionID, fmt.Sprintf(
			"recommend %s: lowest in-band composite risk (%.3f) at %.1f days (App C risk-crossover exclusions applied)",
			best.OptionID, best.CompositeRisk, best.DurationDays)
	}
	best, _ := pickBest(func(projectstate.SdpOptionRow) bool { return true })
	return best.OptionID, fmt.Sprintf(
		"recommend %s: ALL options fell outside the App C risk band [%.2f,%.2f]; picked lowest composite risk (%.3f) — review before committing",
		best.OptionID, riskOverSafe, riskTooRisky, best.CompositeRisk)
}

// optionInReview reports whether id names one of the assembled option rows.
func optionInReview(review *projectstate.SdpReview, id projectstate.OptionID) bool {
	if review == nil {
		return false
	}
	for _, r := range review.Options {
		if r.OptionID == id {
			return true
		}
	}
	return false
}

// sdpIncomplete wraps a missing-prerequisite error as a non-retryable terminal.
func sdpIncomplete(cause error) error {
	return temporal.NewNonRetryableApplicationError(
		"sdp inputs incomplete: "+cause.Error(), "SDPInputsIncomplete", cause)
}

// escalateEngine wraps an Engine error as a non-retryable terminal (the option was
// mis-assembled or an engine invariant broke — neither is retryable).
func escalateEngine(engineName string, kind projectstate.ArtifactKind, cause error) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("%s failed for option %s: %s", engineName, kind, cause.Error()),
		"SDPEngineError", cause)
}

// committedModel returns the committed typed model in the slot named by kind, or a
// FailedPrecondition error if the slot is not committed / not populated.
func committedModel(proj projectstate.Project, kind projectstate.ArtifactKind) (projectstate.ArtifactModel, error) {
	slot := slotFor(proj, kind)
	if slot.Status != projectstate.ReviewCommitted || slot.Model == nil {
		return nil, fwmanager.New(fwmanager.FailedPrecondition,
			fmt.Sprintf("SDP prerequisite %s is not committed", kind))
	}
	return slot.Model, nil
}

func committedPlanningAssumptions(proj projectstate.Project) (projectstate.PlanningAssumptions, error) {
	m, err := committedModel(proj, projectstate.KindPlanningAssumptions)
	if err != nil {
		return projectstate.PlanningAssumptions{}, err
	}
	pa, ok := m.(*projectstate.PlanningAssumptions)
	if !ok {
		return projectstate.PlanningAssumptions{}, wrongModelType(projectstate.KindPlanningAssumptions, m)
	}
	return *pa, nil
}

func committedActivityList(proj projectstate.Project) (projectstate.ActivityList, error) {
	m, err := committedModel(proj, projectstate.KindActivityList)
	if err != nil {
		return projectstate.ActivityList{}, err
	}
	al, ok := m.(*projectstate.ActivityList)
	if !ok {
		return projectstate.ActivityList{}, wrongModelType(projectstate.KindActivityList, m)
	}
	return *al, nil
}

func committedNetwork(proj projectstate.Project) (projectstate.Network, error) {
	m, err := committedModel(proj, projectstate.KindNetwork)
	if err != nil {
		return projectstate.Network{}, err
	}
	nw, ok := m.(*projectstate.Network)
	if !ok {
		return projectstate.Network{}, wrongModelType(projectstate.KindNetwork, m)
	}
	return *nw, nil
}

func committedSolution(proj projectstate.Project, kind projectstate.ArtifactKind) (projectstate.Solution, error) {
	m, err := committedModel(proj, kind)
	if err != nil {
		return projectstate.Solution{}, err
	}
	sol, ok := m.(*projectstate.Solution)
	if !ok {
		return projectstate.Solution{}, wrongModelType(kind, m)
	}
	return *sol, nil
}

func wrongModelType(want projectstate.ArtifactKind, got projectstate.ArtifactModel) error {
	gotKind := "nil"
	if got != nil {
		gotKind = got.Kind().String()
	}
	return fwmanager.New(fwmanager.ContractMisuse,
		fmt.Sprintf("expected a %s model, got %s", want, gotKind))
}

// airates.go derives each project option's per-worker-class build-cost rate from the AI
// rate card (Phase-2 estimation rework F11). Team members are AI AGENTS, not humans, so
// the old flat $800/$500 human day-rates are gone: an agent's cost is the LLM inference
// it burns per agent-day = expected tokens × the Claude API price for the model that
// agent runs.
//
//	rate($/day) = MegatokensInPerDay × price_in + MegatokensOutPerDay × price_out
//
// The role→model mapping is the source-of-truth agent roster in .claude/agents/*.md
// frontmatter (F11c). Phantom worker classes that map to no agent (architect,
// devops-agent, web-engineer-agent) are intentionally absent (F11d).
//
// Pure + deterministic (no clock, no RNG, no I/O) so the SDP assembly stays replay-safe.

// modelPrice is the Claude API price for one model, in USD MINOR UNITS (cents) per
// megatoken (MTok). Source: Anthropic price list (F11b) — fable $10/$50, opus $5/$25,
// sonnet $3/$15, haiku $1/$5 per MTok in/out.
type modelPrice struct {
	inCentsPerMTok  float64
	outCentsPerMTok float64
}

// apiPricing is the per-model Claude API price list, keyed by the frontmatter model id.
var apiPricing = map[string]modelPrice{
	"fable":  {inCentsPerMTok: 1000, outCentsPerMTok: 5000}, // $10 in / $50 out
	"opus":   {inCentsPerMTok: 500, outCentsPerMTok: 2500},  // $5 in / $25 out
	"sonnet": {inCentsPerMTok: 300, outCentsPerMTok: 1500},  // $3 in / $15 out
	"haiku":  {inCentsPerMTok: 100, outCentsPerMTok: 500},   // $1 in / $5 out
}

// priceFamily normalizes a model id to its apiPricing family key. The rate card's
// modelId is authored as a FULL API id ("claude-opus-4-8", "claude-haiku-4-5-20251001")
// while apiPricing is keyed by short family names — the exact-key lookup silently
// priced EVERY full id as sonnet (found live on gtdapp 2026-07-11: the opus architect
// class costed at sonnet rates). Substring match on the lowercased id; unknown ids
// keep the documented sonnet fallback via the caller's miss branch.
func priceFamily(modelID string) string {
	id := strings.ToLower(modelID)
	for _, fam := range [...]string{"fable", "opus", "sonnet", "haiku"} {
		if strings.Contains(id, fam) {
			return fam
		}
	}
	return id
}

// roleModel maps each worker CLASS (agent role) to the model it runs (F11c), taken
// verbatim from .claude/agents/*.md frontmatter. The phantom classes (architect,
// devops-agent, web-engineer-agent) are deliberately NOT here.
var roleModel = map[string]string{
	"system-architect": "fable",
	"project-manager":  "fable",
	"senior-developer": "opus",
	"product-manager":  "opus",
	"ui-designer":      "opus",
	"junior-developer": "sonnet",
	"qa-engineer":      "sonnet",
	"test-engineer":    "sonnet",
	"software-tester":  "sonnet",
	"ux-reviewer":      "sonnet",
}

// Default token throughput per agent-day (F11a). Kept uniform across classes so the cost
// SPREAD between classes comes purely from the model tier (fable roles are the most
// expensive per day, sonnet roles the cheapest). Tunable per-class via
// PlanningAssumptions.RateCard once the state pass authors it.
const (
	defaultMTokInPerDay  = 2.0 // ~2M input tokens / agent-day (context + tool results)
	defaultMTokOutPerDay = 0.5 // ~0.5M output tokens / agent-day (generated code + notes)
)

// defaultModelForClass returns the model a class runs, defaulting an UNKNOWN class (e.g.
// a stale "architect" fixture) to sonnet so rate derivation never fails a valid option.
func defaultModelForClass(class string) string {
	if m, ok := roleModel[class]; ok {
		return m
	}
	return "sonnet"
}

// defaultRateSpec returns the default AI rate spec for a class (uniform throughput on the
// class's mapped model).
func defaultRateSpec(class string) projectstate.WorkerRateSpec {
	return projectstate.WorkerRateSpec{
		ModelID:             defaultModelForClass(class),
		MegatokensInPerDay:  defaultMTokInPerDay,
		MegatokensOutPerDay: defaultMTokOutPerDay,
	}
}

// deriveClassRates computes the per-day build-cost rate for every worker class used by
// the option (F11b). It prefers the authored PlanningAssumptions.RateCard entry, falling
// back to the documented default spec for any class the card omits, so an option always
// assembles even before the state pass authors the card. Deterministic: the output map
// is keyed by class; iteration order is irrelevant.
func deriveClassRates(pa projectstate.PlanningAssumptions, classes []string) map[string]projectstate.Money {
	rates := make(map[string]projectstate.Money, len(classes))
	for _, class := range classes {
		spec, ok := pa.RateCard[class]
		if !ok || spec.ModelID == "" {
			spec = defaultRateSpec(class)
		}
		rates[class] = rateForSpec(spec)
	}
	return rates
}

// rateForSpec turns a rate spec into a USD/day Money via the Claude API price list. An
// unknown model id falls back to sonnet pricing (never panics). Deterministic integer
// truncation (no rounding-mode ambiguity) matches the estimationEngine's cost math.
func rateForSpec(spec projectstate.WorkerRateSpec) projectstate.Money {
	price, ok := apiPricing[priceFamily(spec.ModelID)]
	if !ok {
		price = apiPricing["sonnet"]
	}
	cents := spec.MegatokensInPerDay*price.inCentsPerMTok + spec.MegatokensOutPerDay*price.outCentsPerMTok
	return projectstate.Money{MinorUnits: int64(cents), Currency: "USD"}
}

// defaultIndirectDailyRate is the overhead burn per calendar day used when
// PlanningAssumptions.IndirectDailyRate is unset (F6). $50/day (5000 cents USD) — the
// platform/orchestration overhead that accrues over the schedule regardless of which
// agents are active. Makes a longer (subcritical) option demonstrably costlier.
var defaultIndirectDailyRate = projectstate.Money{MinorUnits: 5000, Currency: "USD"}

// indirectDailyRateOf returns the authored indirect rate, or the documented default when
// unset.
func indirectDailyRateOf(pa projectstate.PlanningAssumptions) projectstate.Money {
	if pa.IndirectDailyRate.MinorUnits != 0 || pa.IndirectDailyRate.Currency != "" {
		return pa.IndirectDailyRate
	}
	return defaultIndirectDailyRate
}
