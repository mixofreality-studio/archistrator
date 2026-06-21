package projectstate

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// gitactivityconstruction.go holds the ADDITIVE per-activity construction status
// Record* verbs (Task 1: seed-archistrator-design-state). They mirror the gitactivity.go
// pattern exactly: modeRequireExisting, optimistic-version CAS via applyMutation,
// idempotency dedup via the in-repo ledger, and partial map-key upsert so two records
// for two DIFFERENT activities converge under ref-CAS (the GIT.4 convergence invariant
// applies here too).
//
// The concrete GitStore satisfies the additive GitActivityConstructionAccess facet in
// addition to GitActivityStatusAccess and GitConstructionTransitionAccess.

// GitActivityConstructionAccess is the additive per-activity construction head-state
// facet. Kept on a separate additive interface (matching GitActivityStatusAccess) so
// the core GitProjectStateAccess port remains source-stable.
type GitActivityConstructionAccess interface {
	// RecordActivityStarted marks activityID as Running and records the server-resolved
	// StartedAt timestamp. Optimistic on expectedVersion; idempotent on idempotencyKey.
	RecordActivityStarted(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)

	// RecordActivityCompleted marks activityID as Done and records the server-resolved
	// CompletedAt timestamp. Optimistic on expectedVersion; idempotent on idempotencyKey.
	RecordActivityCompleted(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
}

// Compile-time proof the concrete GitStore satisfies the additive construction facet.
var _ GitActivityConstructionAccess = (*GitStore)(nil)

// upsertActivityConstruction fetches (or initialises) the per-activity construction row,
// applies the supplied in-place mutation, and writes the SINGLE map key back. The map is
// lazily allocated. This is a PARTIAL map-key update (mirrors upsertActivity in
// gitactivity.go — GIT.4): only the named key is touched; every other
// ActivityConstruction entry is left byte-identical, so two records on DIFFERENT
// activityIds converge under ref-CAS instead of clobbering.
func upsertActivityConstruction(p *Project, activityID string, mutate func(s *ActivityConstructionStatus)) {
	if p.ActivityConstruction == nil {
		p.ActivityConstruction = map[string]ActivityConstructionStatus{}
	}
	s := p.ActivityConstruction[activityID] // zero value on first touch — births the row
	s.ActivityID = activityID
	mutate(&s)
	p.ActivityConstruction[activityID] = s
}

// RecordActivityStarted records that activityID's construction agent has been dispatched
// (Phase → Running, StartedAt server-resolved). Uses modeRequireExisting (project row
// exists by Phase 3, same as gitactivity.go verbs).
func (s *GitStore) RecordActivityStarted(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityStarted: empty activityID")
	}
	now := s.now()
	return s.applyMutation(ctx, "RecordActivityStarted", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			cs.Phase = ActivityConstructionRunning
			// Advance the finer BuildStatus lens in lock-step with the coarse Phase so the
			// SINGLE constructionRows projection (catalog.go) tells the whole cascade story:
			// a dispatched activity is being built now → in-construction (the SPA's tracker
			// keys node color off BuildStatus). The pump only ever touches a NotStarted/absent
			// row (eligibility gate), so this never clobbers a seeded corpus BuildStatus.
			cs.BuildStatus = BuildInConstruction
			t := now
			cs.StartedAt = &t
		})
		return nil
	})
}

// RecordActivityCompleted records that activityID's construction agent has finished
// (Phase → Done, CompletedAt server-resolved). Uses modeRequireExisting.
func (s *GitStore) RecordActivityCompleted(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityCompleted: empty activityID")
	}
	now := s.now()
	return s.applyMutation(ctx, "RecordActivityCompleted", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		upsertActivityConstruction(p, activityID, func(cs *ActivityConstructionStatus) {
			cs.Phase = ActivityConstructionDone
			// The per-activity construction spine completes only AFTER its review passed
			// and the change merged (workflow.go steps 5–8a), so a completed activity IS
			// integrated — advance BuildStatus to Integrated. This is what adds the activity
			// to the SPA's done-set (constructionAdapters: status==='integrated'), turning its
			// node green AND unblocking its dependents so the frontier cascades forward.
			cs.BuildStatus = BuildIntegrated
			t := now
			cs.CompletedAt = &t
		})
		return nil
	})
}
