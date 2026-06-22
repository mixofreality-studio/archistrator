package projectstate

// gitconstruction.go is the git-substrate realization of the additive Phase-3
// construction-transition verbs (constructionManager.md §5.3; see construction.go
// for the Postgres-era port + rationale). The git GitStore satisfies the SAME
// ConstructionTransitionAccess facet — re-cut with the Manager-threaded
// `cred RepoCredential` (REWORK.4) the substrate swap forces, exactly as the
// Phase-1/2 verbs are. v1 records the transition through the shared ref-CAS +
// in-repo-dedup applyMutation path so it is durable and replay-idempotent; the
// richer per-activity head-state status aggregate is populated from Task 4.

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// GitConstructionTransitionAccess is the cred-threaded construction-transition
// facet of the git store (the §REWORK.4 re-cut of ConstructionTransitionAccess).
// 7 ops total (≤12 per contract cap).
type GitConstructionTransitionAccess interface {
	RecordChangeReviewed(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordActivityExited(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, outcome ActivityOutcome, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordOperatorPaused(ctx context.Context, projectID ProjectID, expectedVersion Version, reason string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordPhaseStarted(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, phase ActivityMethodPhase, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordPhaseCompleted(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, phase ActivityMethodPhase, artifactRef string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordServiceContractProduced(ctx context.Context, projectID ProjectID, expectedVersion Version, component string, contract ServiceContract, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordPhaseArtifactProduced(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, mapKey string, payload PhaseArtifactPayload, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
}

var _ GitConstructionTransitionAccess = (*GitStore)(nil)

// PhaseArtifactPayload is a tagged union of all phase artifact types.
// Exactly one field should be set. RecordPhaseArtifactProduced inspects which
// field is non-zero and routes it to the right map in PhaseArtifacts or
// TestingState. mapKey is the component/surface/resource/doc name used as the
// map key for PhaseArtifacts fields; it is unused for pointer-scalar TestingState
// fields (SystemTestPlan, HarnessModule, PerfHarness, QualityAuditReport).
type PhaseArtifactPayload struct {
	// PhaseArtifacts fields (keyed by mapKey)
	SRS              *SRSRecord
	TestPlan         *TestPlanRecord
	IntegrationNote  *IntegrationNoteRecord
	UXRequirements   *UXRequirementsRecord
	UIDesign         *UIDesignRecord
	ProvisioningSpec *ProvisioningSpecRecord
	DeployNote       *DeployNoteRecord
	DocOutline       *DocOutlineRecord
	DocNote          *DocNoteRecord
	// TestingState fields (project-level singletons / slices)
	SystemTestPlan *SystemTestPlan
	HarnessModule  *HarnessModule
	PerfHarness    *PerfHarness
	QualityGate    *QualityGate
	TestRun        *TestRun
	Defect         *DefectRecord
	// QualityAuditReport replaces the string in TestingState when non-empty.
	QualityAuditReport string
}

// RecordChangeReviewed records the review transition for activityID by setting
// BuildStatus = BuildInReview. Uses modeRequireExisting (project row exists by
// Phase 3, same discipline as gitactivity.go verbs).
func (s *GitStore) RecordChangeReviewed(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordChangeReviewed: empty activityID")
	}
	return s.applyMutation(ctx, "RecordChangeReviewed", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			cs.BuildStatus = BuildInReview
		})
		return nil
	})
}

// RecordActivityExited records the binary activity exit for activityID. On
// ActivityOutcomeCompleted: Phase = ActivityConstructionDone, BuildStatus =
// BuildIntegrated. On other outcomes: Phase = ActivityConstructionDone,
// BuildStatus = BuildInReview (skipped/taken-over land done but not integrated).
// CompletedAt is server-resolved if not already set.
func (s *GitStore) RecordActivityExited(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, outcome ActivityOutcome, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityExited: empty activityID")
	}
	now := s.now()
	return s.applyMutation(ctx, "RecordActivityExited", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			cs.Phase = ActivityConstructionDone
			if cs.CompletedAt == nil {
				t := now
				cs.CompletedAt = &t
			}
			switch outcome {
			case ActivityOutcomeCompleted:
				cs.BuildStatus = BuildIntegrated
			default:
				// Skipped / TakenOver: activity is done but was not reviewed+integrated.
				cs.BuildStatus = BuildInReview
			}
		})
		return nil
	})
}

// RecordOperatorPaused records the operator-paused head-state transition by
// setting Project.OperatorPaused = true and Project.PauseReason = reason.
func (s *GitStore) RecordOperatorPaused(ctx context.Context, projectID ProjectID, expectedVersion Version, reason string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "RecordOperatorPaused", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		p.OperatorPaused = true
		p.PauseReason = reason
		return nil
	})
}

// RecordPhaseStarted records that activityID's construction agent has entered the
// given phase. It seeds the Phases slice from phaseSetFor if not yet populated,
// sets CurrentPhase = phase, and advances the coarse Phase to Running (if not
// already Done).
func (s *GitStore) RecordPhaseStarted(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, phase ActivityMethodPhase, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordPhaseStarted: empty activityID")
	}
	if phase == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordPhaseStarted: empty phase")
	}
	return s.applyMutation(ctx, "RecordPhaseStarted", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			if len(cs.Phases) == 0 {
				cs.Phases = phaseSetFor(cs.Type, cs.Variant)
			}
			cs.CurrentPhase = phase
			if cs.Phase != ActivityConstructionDone {
				cs.Phase = ActivityConstructionRunning
			}
		})
		return nil
	})
}

// RecordPhaseCompleted marks the given phase Completed = true, records the
// server-resolved CompletedAt, and optionally sets ArtifactRef. It recomputes the
// coarse Phase via CoarsePhase over the updated Phases slice so the tracker
// advances atomically with the phase completion.
func (s *GitStore) RecordPhaseCompleted(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, phase ActivityMethodPhase, artifactRef string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordPhaseCompleted: empty activityID")
	}
	if phase == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordPhaseCompleted: empty phase")
	}
	now := s.now()
	return s.applyMutation(ctx, "RecordPhaseCompleted", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			if len(cs.Phases) == 0 {
				cs.Phases = phaseSetFor(cs.Type, cs.Variant)
			}
			for i := range cs.Phases {
				if cs.Phases[i].Phase == phase {
					t := now
					cs.Phases[i].Completed = true
					cs.Phases[i].CompletedAt = &t
					if artifactRef != "" {
						cs.Phases[i].ArtifactRef = artifactRef
					}
					break
				}
			}
			// Recompute coarse phase from the updated Phases slice.
			cs.Phase = CoarsePhase(cs.Phases)
		})
		return nil
	})
}

// RecordServiceContractProduced writes the typed ServiceContract for component
// into Project.ServiceContracts, lazy-allocating the map on first write.
func (s *GitStore) RecordServiceContractProduced(ctx context.Context, projectID ProjectID, expectedVersion Version, component string, contract ServiceContract, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if component == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordServiceContractProduced: empty component")
	}
	return s.applyMutation(ctx, "RecordServiceContractProduced", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		if p.ServiceContracts == nil {
			p.ServiceContracts = make(map[string]ServiceContract)
		}
		p.ServiceContracts[component] = contract
		return nil
	})
}

// RecordPhaseArtifactProduced writes the typed artifact carried in payload into
// the correct PhaseArtifacts / TestingState slot of the Project aggregate.
// mapKey is used as the per-component/surface/resource/doc map key for
// PhaseArtifacts fields; it is unused for singleton TestingState fields.
// Exactly one payload field should be set; if none is set the verb is a no-op
// (idempotent empty payload is tolerated — the ledger dedup will still fire).
func (s *GitStore) RecordPhaseArtifactProduced(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, mapKey string, payload PhaseArtifactPayload, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordPhaseArtifactProduced: empty activityID")
	}
	return s.applyMutation(ctx, "RecordPhaseArtifactProduced", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		applyPhaseArtifactPayload(p, mapKey, payload)
		return nil
	})
}

// applyPhaseArtifactPayload routes the payload to the correct Project field.
// It is a pure function (no I/O) extracted for testability.
func applyPhaseArtifactPayload(p *Project, mapKey string, payload PhaseArtifactPayload) {
	applyPhaseArtifactsPayload(p, mapKey, payload)
	applyTestingStatePayload(p, payload)
}

// applyPhaseArtifactsPayload routes the PhaseArtifacts-group fields (keyed by
// mapKey) from the payload into p.PhaseArtifacts, lazy-allocating as needed.
// Split into two halves to keep each helper under the funlen threshold.
func applyPhaseArtifactsPayload(p *Project, mapKey string, payload PhaseArtifactPayload) {
	applyPhaseArtifactsSpecDesign(p, mapKey, payload)
	applyPhaseArtifactsDeployDoc(p, mapKey, payload)
}

// ensurePhaseArtifacts lazy-inits p.PhaseArtifacts and returns it.
func ensurePhaseArtifacts(p *Project) *PhaseArtifacts {
	if p.PhaseArtifacts == nil {
		p.PhaseArtifacts = &PhaseArtifacts{}
	}
	return p.PhaseArtifacts
}

// applyPhaseArtifactsSpecDesign handles the spec/design half of PhaseArtifacts
// fields: SRS, TestPlan, IntegrationNote, UXRequirements, UIDesign.
func applyPhaseArtifactsSpecDesign(p *Project, mapKey string, payload PhaseArtifactPayload) {
	if payload.SRS != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.SRS == nil {
			pa.SRS = make(map[string]SRSRecord)
		}
		pa.SRS[mapKey] = *payload.SRS
	}
	if payload.TestPlan != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.TestPlan == nil {
			pa.TestPlan = make(map[string]TestPlanRecord)
		}
		pa.TestPlan[mapKey] = *payload.TestPlan
	}
	if payload.IntegrationNote != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.IntegrationNote == nil {
			pa.IntegrationNote = make(map[string]IntegrationNoteRecord)
		}
		pa.IntegrationNote[mapKey] = *payload.IntegrationNote
	}
	if payload.UXRequirements != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.UXRequirements == nil {
			pa.UXRequirements = make(map[string]UXRequirementsRecord)
		}
		pa.UXRequirements[mapKey] = *payload.UXRequirements
	}
	if payload.UIDesign != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.UIDesign == nil {
			pa.UIDesign = make(map[string]UIDesignRecord)
		}
		pa.UIDesign[mapKey] = *payload.UIDesign
	}
}

// applyPhaseArtifactsDeployDoc handles the infra/doc half of PhaseArtifacts
// fields: ProvisioningSpec, DeployNote, DocOutline, DocNote.
func applyPhaseArtifactsDeployDoc(p *Project, mapKey string, payload PhaseArtifactPayload) {
	if payload.ProvisioningSpec != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.ProvisioningSpec == nil {
			pa.ProvisioningSpec = make(map[string]ProvisioningSpecRecord)
		}
		pa.ProvisioningSpec[mapKey] = *payload.ProvisioningSpec
	}
	if payload.DeployNote != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.DeployNote == nil {
			pa.DeployNote = make(map[string]DeployNoteRecord)
		}
		pa.DeployNote[mapKey] = *payload.DeployNote
	}
	if payload.DocOutline != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.DocOutline == nil {
			pa.DocOutline = make(map[string]DocOutlineRecord)
		}
		pa.DocOutline[mapKey] = *payload.DocOutline
	}
	if payload.DocNote != nil {
		pa := ensurePhaseArtifacts(p)
		if pa.DocNote == nil {
			pa.DocNote = make(map[string]DocNoteRecord)
		}
		pa.DocNote[mapKey] = *payload.DocNote
	}
}

// applyTestingStatePayload routes the TestingState-group fields (project-level
// singletons and append-slices) from the payload into p.TestingState,
// lazy-allocating as needed.
func applyTestingStatePayload(p *Project, payload PhaseArtifactPayload) {
	ensureTestingState := func() *TestingState {
		if p.TestingState == nil {
			p.TestingState = &TestingState{}
		}
		return p.TestingState
	}
	if payload.SystemTestPlan != nil {
		ensureTestingState().SystemTestPlan = payload.SystemTestPlan
	}
	if payload.HarnessModule != nil {
		ensureTestingState().HarnessModule = payload.HarnessModule
	}
	if payload.PerfHarness != nil {
		ensureTestingState().PerfHarness = payload.PerfHarness
	}
	if payload.QualityGate != nil {
		ts := ensureTestingState()
		ts.QualityGates = append(ts.QualityGates, *payload.QualityGate)
	}
	if payload.TestRun != nil {
		ts := ensureTestingState()
		ts.TestRuns = append(ts.TestRuns, *payload.TestRun)
	}
	if payload.Defect != nil {
		ts := ensureTestingState()
		ts.Defects = append(ts.Defects, *payload.Defect)
	}
	if payload.QualityAuditReport != "" {
		ensureTestingState().QualityAuditReport = payload.QualityAuditReport
	}
}
