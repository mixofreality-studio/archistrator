package harness

// construction_seed.go builds the PUBLISHED on-disk `.aiarch/state/project.json`
// shape for a project pre-staged directly into Phase-3 construction, paired with
// StartLocalGitRepoWithFiles (localgit.go). UC3's ExecuteNextActivity needs a
// project already carrying a committed ActivityList + Network + ServiceContracts —
// driving the full Phase-1/2 wire sequence just to reach that state would make
// every UC3 case pay for Phase-1/2's cost; the Method's own state is git-as-DB, so
// staging it directly in the seed commit of the fixture repo is the black-box
// equivalent of "this project already completed project design" (constructionMan-
// ager.md never requires reproving Phase 1/2 — that is STP-UC1/UC2's job).
//
// This is the harness's own hand-mirror of the PUBLISHED wire/on-disk shape
// (server/internal/resourceaccess/projectstate: gitstore.go's projectDoc,
// slotcodec.go's slotJSON, models_phase2.go's ActivityList/Network,
// servicecontract.go's ServiceContract) — built from plain maps and marshaled to
// JSON, never an imported server type (R1/R3: the harness module cannot import the
// system under test).

import (
	"encoding/json"
	"strconv"
)

// Ordinals mirrored from the published projectstate contract (identity.go /
// contract.gen.go) — see httptransport.go's wire enum ordinal tables for the
// sibling convention this file follows.
const (
	kindActivityListOrdinal  = 9  // projectstate.KindActivityList
	kindNetworkOrdinal       = 10 // projectstate.KindNetwork
	reviewCommittedOrdinal   = 2  // projectstate.ReviewCommitted
	phaseConstructionOrdinal = 2  // projectstate.PhaseConstruction
)

// SeedActivity is one construction-network activity for ConstructionProjectJSON:
// its id (the network/activity-list/service-contract key, name-as-identity), its
// predecessor activity ids (the eligibility dependency edges nextEligibleActivity
// walks — server/internal/manager/construction/next_eligible_activity_test.go), and
// its effort in 5-day quanta.
type SeedActivity struct {
	ID         string
	DependsOn  []string
	EffortDays float64
}

// ConstructionProjectJSON builds a `.aiarch/state/project.json` document staged
// directly at Phase 3 (construction): a committed ActivityList + Network slot pair
// (the two slots nextEligibleActivity requires) plus one ServiceContract per
// activity (the hardened component resolver requires a real contract entry to
// dispatch — its Title/Name matches the contract key, per
// next_eligible_activity_test.go's TestNextEligibleActivity_Chain). No
// ActivityConstruction rows are seeded — every activity starts NotStarted (the zero
// value / an absent map entry), exactly like a freshly-sealed Phase-2 project.
func ConstructionProjectJSON(projectID string, activities []SeedActivity) []byte {
	activityItems := make([]map[string]any, 0, len(activities))
	deps := make([]map[string]any, 0, len(activities))
	contracts := map[string]any{}
	for _, a := range activities {
		dependsOn := a.DependsOn
		if dependsOn == nil {
			dependsOn = []string{}
		}
		activityItems = append(activityItems, map[string]any{
			"name": a.ID, "title": a.ID, "effortDays": a.EffortDays,
			"workerClass": "AI", "coding": true, "riskBucket": 1,
		})
		deps = append(deps, map[string]any{"activity": a.ID, "dependsOn": dependsOn})
		contracts[a.ID] = map[string]any{"component": a.ID, "layer": "Manager"}
	}

	activityListModel := map[string]any{"activities": activityItems}
	networkModel := map[string]any{"dependencies": deps, "criticalPath": []string{}}

	doc := map[string]any{
		"id":      projectID,
		"version": 1,
		"phase":   phaseConstructionOrdinal,
		"owner":   testOwner,
		"name":    projectID,
		"slots": map[string]any{
			strconv.Itoa(kindActivityListOrdinal): map[string]any{
				"status": reviewCommittedOrdinal, "kind": kindActivityListOrdinal, "model": activityListModel,
			},
			strconv.Itoa(kindNetworkOrdinal): map[string]any{
				"status": reviewCommittedOrdinal, "kind": kindNetworkOrdinal, "model": networkModel,
			},
		},
		"serviceContracts": contracts,
		// The "vibes" review-policy preset, stated explicitly (it is also the
		// default GitStore.CreateProject seeds for a fresh project): under the
		// local construction executor, vibes auto-approves every phase AND
		// auto-merges activity/<id> into main on completion (local-merge-and-
		// policy Commit 1) — the shape the UC3 local-executor systemtest proves.
		"reviewPolicy": map[string]any{"preset": "vibes"},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		// Only plain maps/slices/strings/bools/numbers above — cannot fail.
		panic("harness: ConstructionProjectJSON: " + err.Error())
	}
	return out
}
