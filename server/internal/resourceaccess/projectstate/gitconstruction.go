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
	// --- PhaseArtifacts routes (keyed by mapKey) ---
	if payload.SRS != nil {
		if p.PhaseArtifacts == nil {
			p.PhaseArtifacts = &PhaseArtifacts{}
		}
		if p.PhaseArtifacts.SRS == nil {
			p.PhaseArtifacts.SRS = make(map[string]SRSRecord)
		}
		p.PhaseArtifacts.SRS[mapKey] = *payload.SRS
	}
	if payload.TestPlan != nil {
		if p.PhaseArtifacts == nil {
			p.PhaseArtifacts = &PhaseArtifacts{}
		}
		if p.PhaseArtifacts.TestPlan == nil {
			p.PhaseArtifacts.TestPlan = make(map[string]TestPlanRecord)
		}
		p.PhaseArtifacts.TestPlan[mapKey] = *payload.TestPlan
	}
	if payload.IntegrationNote != nil {
		if p.PhaseArtifacts == nil {
			p.PhaseArtifacts = &PhaseArtifacts{}
		}
		if p.PhaseArtifacts.IntegrationNote == nil {
			p.PhaseArtifacts.IntegrationNote = make(map[string]IntegrationNoteRecord)
		}
		p.PhaseArtifacts.IntegrationNote[mapKey] = *payload.IntegrationNote
	}
	if payload.UXRequirements != nil {
		if p.PhaseArtifacts == nil {
			p.PhaseArtifacts = &PhaseArtifacts{}
		}
		if p.PhaseArtifacts.UXRequirements == nil {
			p.PhaseArtifacts.UXRequirements = make(map[string]UXRequirementsRecord)
		}
		p.PhaseArtifacts.UXRequirements[mapKey] = *payload.UXRequirements
	}
	if payload.UIDesign != nil {
		if p.PhaseArtifacts == nil {
			p.PhaseArtifacts = &PhaseArtifacts{}
		}
		if p.PhaseArtifacts.UIDesign == nil {
			p.PhaseArtifacts.UIDesign = make(map[string]UIDesignRecord)
		}
		p.PhaseArtifacts.UIDesign[mapKey] = *payload.UIDesign
	}
	if payload.ProvisioningSpec != nil {
		if p.PhaseArtifacts == nil {
			p.PhaseArtifacts = &PhaseArtifacts{}
		}
		if p.PhaseArtifacts.ProvisioningSpec == nil {
			p.PhaseArtifacts.ProvisioningSpec = make(map[string]ProvisioningSpecRecord)
		}
		p.PhaseArtifacts.ProvisioningSpec[mapKey] = *payload.ProvisioningSpec
	}
	if payload.DeployNote != nil {
		if p.PhaseArtifacts == nil {
			p.PhaseArtifacts = &PhaseArtifacts{}
		}
		if p.PhaseArtifacts.DeployNote == nil {
			p.PhaseArtifacts.DeployNote = make(map[string]DeployNoteRecord)
		}
		p.PhaseArtifacts.DeployNote[mapKey] = *payload.DeployNote
	}
	if payload.DocOutline != nil {
		if p.PhaseArtifacts == nil {
			p.PhaseArtifacts = &PhaseArtifacts{}
		}
		if p.PhaseArtifacts.DocOutline == nil {
			p.PhaseArtifacts.DocOutline = make(map[string]DocOutlineRecord)
		}
		p.PhaseArtifacts.DocOutline[mapKey] = *payload.DocOutline
	}
	if payload.DocNote != nil {
		if p.PhaseArtifacts == nil {
			p.PhaseArtifacts = &PhaseArtifacts{}
		}
		if p.PhaseArtifacts.DocNote == nil {
			p.PhaseArtifacts.DocNote = make(map[string]DocNoteRecord)
		}
		p.PhaseArtifacts.DocNote[mapKey] = *payload.DocNote
	}
	// --- TestingState routes (project-level singletons / slices) ---
	if payload.SystemTestPlan != nil {
		if p.TestingState == nil {
			p.TestingState = &TestingState{}
		}
		p.TestingState.SystemTestPlan = payload.SystemTestPlan
	}
	if payload.HarnessModule != nil {
		if p.TestingState == nil {
			p.TestingState = &TestingState{}
		}
		p.TestingState.HarnessModule = payload.HarnessModule
	}
	if payload.PerfHarness != nil {
		if p.TestingState == nil {
			p.TestingState = &TestingState{}
		}
		p.TestingState.PerfHarness = payload.PerfHarness
	}
	if payload.QualityGate != nil {
		if p.TestingState == nil {
			p.TestingState = &TestingState{}
		}
		p.TestingState.QualityGates = append(p.TestingState.QualityGates, *payload.QualityGate)
	}
	if payload.TestRun != nil {
		if p.TestingState == nil {
			p.TestingState = &TestingState{}
		}
		p.TestingState.TestRuns = append(p.TestingState.TestRuns, *payload.TestRun)
	}
	if payload.Defect != nil {
		if p.TestingState == nil {
			p.TestingState = &TestingState{}
		}
		p.TestingState.Defects = append(p.TestingState.Defects, *payload.Defect)
	}
	if payload.QualityAuditReport != "" {
		if p.TestingState == nil {
			p.TestingState = &TestingState{}
		}
		p.TestingState.QualityAuditReport = payload.QualityAuditReport
	}
}
