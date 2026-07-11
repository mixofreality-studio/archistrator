package projectstate

import (
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// gitactivityconstruction.go holds the per-activity construction status Record* verbs
// (Task 1: seed-archistrator-design-state). They mirror the gitactivity.go pattern
// exactly: modeRequireExisting, optimistic-version CAS via applyMutation, idempotency
// dedup via the in-repo ledger, and partial map-key upsert so two records for two
// DIFFERENT activities converge under ref-CAS (the GIT.4 convergence invariant applies
// here too).
//
// These two verbs (RecordActivityStarted/RecordActivityCompleted) were formerly their
// own additive GitActivityConstructionAccess facet; they are now folded onto the SAME
// generated GitActivityStatusAccess contract as the gitactivity.go verbs (one 6-op
// promoted component, contract.gitActivityStatusAccess.schema.json) — see gitactivity.go
// for the interface + its compile-time assertion. The concrete GitStore satisfies
// GitActivityStatusAccess in addition to GitConstructionTransitionAccess.

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
func (s *GitStore) RecordActivityStarted(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityStarted: empty activityID")
	}
	now := s.now()
	return s.applyMutation(rc.Context, "RecordActivityStarted", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
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
func (s *GitStore) RecordActivityCompleted(rc fwra.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityCompleted: empty activityID")
	}
	now := s.now()
	return s.applyMutation(rc.Context, "RecordActivityCompleted", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
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
