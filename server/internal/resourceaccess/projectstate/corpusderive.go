package projectstate

import "strings"

// corpusderive.go holds the PURE corpus→typed-state derivation rules (Task 2). No
// filesystem access — Task 3 (cmd/seed-construction) does the IO and feeds these the
// observed CorpusPresence. Reproducible, deterministic, unit-testable.

// CorpusPresence is what the corpus scanner observed for one activity id.

// a log/<id>.md exists
// a matching *-review.md / -R log exists (passing)
// a contracts/<component>.md exists
// corpus-relative path to the contract, when HasContract

// DeriveKind maps an activity to its kind from the activity-id family.
// Only U-SPA* activities are "frontend" — SPA UI-design activities are the sole
// frontend kind. N-* activities are testing. Everything else (including all
// *Client / *Manager / *Engine / *Access components and infra/integration) is
// service, because a Client component exposes a service contract just like any
// other service-layer component.
func DeriveKind(activityID, componentName string) ActivityKind {
	_ = componentName // caller passes it; classification is id-based only
	id := strings.ToUpper(activityID)
	switch {
	case strings.HasPrefix(id, "U-SPA"):
		return ActivityKindFrontend
	case strings.HasPrefix(id, "N-"):
		return ActivityKindTesting
	default:
		return ActivityKindService
	}
}

// DeriveType maps an activity id prefix to its canonical ActivityType. Mirrors
// DeriveKind's prefix logic (U-SPA → Frontend, N- → Testing, else Service) but is
// the forward-looking name (DeriveKind is retained for the legacy Kind field).
func DeriveType(activityID string) ActivityType {
	id := strings.ToUpper(activityID)
	switch {
	case strings.HasPrefix(id, "U-SPA"):
		return ActivityTypeFrontend
	case strings.HasPrefix(id, "N-"):
		return ActivityTypeTesting
	default:
		return ActivityTypeService
	}
}

// DeriveVariant maps a testing activity id prefix to its TestingVariant. Meaningful
// only when DeriveType == ActivityTypeTesting; unknown N- ids fall back to Plan.
// Order matters: N-STH / N-STP share the "N-ST" stem, so match the longer first.
func DeriveVariant(activityID string) TestingVariant {
	id := strings.ToUpper(activityID)
	switch {
	case strings.HasPrefix(id, "N-STH"):
		return TestVariantHarness
	case strings.HasPrefix(id, "N-STP"):
		return TestVariantPlan
	case strings.HasPrefix(id, "N-PERF"):
		return TestVariantPerf
	case strings.HasPrefix(id, "N-IT"):
		return TestVariantSystemTest
	case strings.HasPrefix(id, "N-QA"):
		return TestVariantQAProcess
	default:
		return TestVariantPlan
	}
}

// ClassifyType determines an activity's canonical ActivityType for the view-model
// classification. It supersedes the id-prefix-only DeriveType because the N-*
// id namespace conflates genuine testing (N-IT/N-STP/N-PERF/N-QA/N-STH) with
// infra (N-SC/N-CI), deployment (N-DEP), and documentation (N-ADR). The
// authoritative signals are: whether the activity produced a service contract,
// the U-SPA* frontend prefix, and — for noncoding activities — the owning
// workerClass from the Phase-2 activity list.
//
//   - produced a service contract → Service (it built a component), regardless of id
//   - U-SPA* id                    → Frontend
//   - coding == true               → Service
//   - noncoding, by workerClass:
//     software-tester / test-engineer / qa-engineer → Testing
//     system-architect                              → Documentation
//     everything else (dev/devops noncoding)        → Deployment
func ClassifyType(id, workerClass string, coding, hasServiceContract bool) ActivityType {
	if hasServiceContract {
		return ActivityTypeService
	}
	if strings.HasPrefix(strings.ToUpper(id), "U-SPA") {
		return ActivityTypeFrontend
	}
	if coding {
		return ActivityTypeService
	}
	switch workerClass {
	case "software-tester", "test-engineer", "qa-engineer":
		return ActivityTypeTesting
	case "system-architect":
		return ActivityTypeDocumentation
	default:
		return ActivityTypeDeployment
	}
}

// DeriveBuildStatus maps corpus presence to the finer build-status lens. integrated is
// true only when a log AND a passing review both exist.
func DeriveBuildStatus(p CorpusPresence) (ActivityBuildStatus, bool) {
	switch {
	case p.HasLog && p.HasPassingReview:
		return BuildIntegrated, true
	case p.HasLog:
		return BuildInReview, false
	default:
		return BuildInConstruction, false
	}
}

// DeriveProduced builds the produced-artifact list from corpus evidence, KIND-SPECIFIC
// by the activity type. A frozen contract is emitted for any component that produced
// one (a Client exposes one too). The built work-product then differs by type — this is
// the fix for the "generic code stub" that made frontend/deployment activities render a
// thin, wrong Artifacts-tab experience:
//
//   - Frontend    → a ui-design CONCEPT artifact + a ui-code artifact whose Source
//     carries the SPA preview ROUTE (a "/project/..." path). The
//     Artifacts-tab frontend renderer frames that route as a live
//     same-origin iframe. Source is backfilled with the real route per
//     surface (seed leaves it empty; the renderer degrades gracefully).
//   - Deployment  → a single deployment artifact (the applied provisioning change).
//   - everything  → the generic built-component "code" artifact (unchanged).
func DeriveProduced(p CorpusPresence, componentName string, typ ActivityType) []ProducedArtifact {
	var out []ProducedArtifact
	if p.HasContract {
		out = append(out, ProducedArtifact{
			Kind:     "service-contract",
			Title:    componentName + " — service contract",
			Source:   p.ContractFile,
			Produced: true,
			Note:     "Frozen App-B service contract.",
		})
	}
	if !p.HasLog {
		return out
	}
	switch typ {
	case ActivityTypeFrontend:
		out = append(out,
			ProducedArtifact{
				Kind:     "ui-design",
				Title:    componentName + " — UI design concept",
				Source:   "implementation/log",
				Produced: true,
				Note:     "UI-design concept: personas, screens, layout, and flows for this surface.",
			},
			ProducedArtifact{
				Kind:     "ui-code",
				Title:    componentName + " — built UI",
				Source:   "", // backfilled with the SPA preview route, e.g. /project/archistrator/design/system
				Produced: true,
				Note:     "SPA surface built against the approved design; preview at the route in Source.",
			},
		)
	case ActivityTypeDeployment:
		out = append(out, ProducedArtifact{
			Kind:     "deployment",
			Title:    componentName + " — deployment change",
			Source:   "implementation/log",
			Produced: true,
			Note:     "Provisioning/deployment change applied and verified against the target environment.",
		})
	case ActivityTypeService, ActivityTypeTesting, ActivityTypeDocumentation:
		out = append(out, ProducedArtifact{
			Kind:     "code",
			Title:    componentName + " — built component",
			Source:   "implementation/log",
			Produced: true,
			Note:     "Construction output recorded in the implementation log.",
		})
	default:
		out = append(out, ProducedArtifact{
			Kind:     "code",
			Title:    componentName + " — built component",
			Source:   "implementation/log",
			Produced: true,
			Note:     "Construction output recorded in the implementation log.",
		})
	}
	return out
}
