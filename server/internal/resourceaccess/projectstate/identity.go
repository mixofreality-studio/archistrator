package projectstate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ProjectID is the project aggregate identifier — NAME-AS-IDENTITY (C-PM-Δ,
// 2026-06-15). It is a string DEFINED type whose value IS the user-supplied
// adopted repo name (project name == repo name, server-resolved). It is NO LONGER
// a uuid.UUID alias: the server stops minting UUIDs for project identity, the user
// supplies the repo name on CreateProject, and that name threads verbatim through
// adopt → seat → createProject and round-trips as the persisted `.aiarch/state/`
// `id`. The empty string is the zero value (the "no project" sentinel). The git
// catalog enumerates by the `aiarch-project` topic and returns repo name == this
// identity (the prior `uuid.Parse` skip is gone). (sourceControlAccess.md §10.1 Q7
// — re-derivation degenerates to identity, so no head-state repo-ref column.)
type ProjectID string

// String returns the project identity as a plain string. Defined so the many
// existing `projectID.String()` call sites (logging, idempotency-key derivation,
// catalog ordering, projectDoc serialization) keep compiling unchanged after the
// uuid.UUID→string flip.
func (p ProjectID) String() string { return string(p) }

// Version is the optimistic-concurrency token: per-aggregate mutation count.
// 0 == no row yet. Bumped by one on each successful write verb. NOT a row id or
// timestamp. (projectStateAccess.md §3.0)
type Version int64

// OwnerScope identifies the owning principal of a project (e.g. the subject or
// email of the authenticated user). It scopes the project catalog so ListProjects
// returns only the rows a principal owns. A plain string newtype — the RA stores
// it verbatim and never interprets it. (Task 2.3)
type OwnerScope string

// ComponentID is the stable identifier for a System component.
//
// NAME-AS-IDENTITY (founder decision 2026-06-04): a Component's identity is its
// human-readable Name, carried as a JSONPath/React-key-safe SLUG string. The
// server assigns ComponentID = Slug(Component.Name) in the LLM-draft finalize
// pass (systemdesign.finalize*); the LLM never emits an id. Cross-references
// (Relationship.From/To, DynamicView.Participants/UseCaseID,
// ContainerInstance.ComponentID) carry this same slug. It is a plain string
// alias — validators use it as an opaque map key and format it directly; the
// persisted/served `id` field and the webApp JSONPath anchors ($.components[id=…])
// are unchanged in SHAPE (still a string `id`), only the VALUE moved from a uuid
// to a name-slug.
type ComponentID = string

// UseCaseID is the stable identifier for a UseCase — see ComponentID. The server
// assigns UseCaseID = Slug(UseCase.Name); DynamicView.UseCaseID and
// UseCase.VariationOf carry this slug. A plain string alias.
type UseCaseID = string

// slugNonAlnum collapses every run of non-alphanumeric characters into a single
// hyphen for Slug.
var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Slug converts a human-readable entity name into a stable, JSONPath/React-key-safe
// identity token: lowercased, non-alphanumeric runs collapsed to single hyphens,
// leading/trailing hyphens trimmed. It is the deterministic name→id function used
// by the systemDesign finalize pass to assign Component/UseCase/Actor/ActivityNode
// identities from the names the LLM authored (founder decision 2026-06-04:
// name-as-identity, no UUIDs). A name that slugs to "" (e.g. all punctuation)
// yields "" — the finalize pass treats that as an actionable error.
func Slug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// ArtifactKind discriminates the named Project slots and is used by the generic
// write verbs (commit/reject/withdraw/advancePhase) and the ArtifactModel sealed sum.
// One constant per slot defined across §3.3 and §3.4.
// (projectStateAccess.md §3.1)
//
// NOTE — vocabulary coexistence: this projectstate.ArtifactKind (and
// projectstate.ProjectID) is the TARGET typed-model vocabulary. The older
// internal/resourceaccess ArtifactKind (string-blob names) and ProjectID string
// are being migrated out across tasks T4–T10 and will be deleted when done —
// until then the two coexist intentionally and consumers are migrated
// package-by-package.
type ArtifactKind int

const (
	// ---- Phase 1 ----
	KindMission              ArtifactKind = iota // Mission slot; Model is *MissionStatement
	KindGlossary                                 // Glossary slot; Model is *Glossary
	KindScrubbedRequirements                     // ScrubbedRequirements slot; Model is *ScrubbedRequirements (OQ-2)
	KindVolatilities                             // Volatilities slot; Model is *Volatilities
	KindCoreUseCases                             // CoreUseCases slot; Model is *CoreUseCases
	KindSystem                                   // System slot; Model is *System (Grammar A)
	KindOperationalConcepts                      // OperationalConcepts slot; Model is *OperationalConcepts
	KindStandardCheck                            // StandardCheck slot; Model is *StandardCheck
	// ---- Phase 2 (additive; design-only until projectDesignManager is built) ----
	KindPlanningAssumptions  // PlanningAssumptions slot; Model is *PlanningAssumptions
	KindActivityList         // ActivityList slot; Model is *ActivityList
	KindNetwork              // Network slot; Model is *Network
	KindNormalSolution       // NormalSolution slot; Model is *Solution
	KindSubcriticalSolution  // SubcriticalSolution slot; Model is *Solution
	KindCompressedSolution   // CompressedSolution slot; Model is *Solution
	KindDecompressedSolution // DecompressedSolution slot; Model is *Solution
	KindRiskModel            // RiskModel slot; Model is *RiskModel
	KindSdpReview            // SdpReview slot; Model is *SdpReview
)

// String returns a stable human-readable name for the ArtifactKind.
// Used in error messages and arch-test output.
func (k ArtifactKind) String() string {
	switch k {
	case KindMission:
		return "Mission"
	case KindGlossary:
		return "Glossary"
	case KindScrubbedRequirements:
		return "ScrubbedRequirements"
	case KindVolatilities:
		return "Volatilities"
	case KindCoreUseCases:
		return "CoreUseCases"
	case KindSystem:
		return "System"
	case KindOperationalConcepts:
		return "OperationalConcepts"
	case KindStandardCheck:
		return "StandardCheck"
	case KindPlanningAssumptions:
		return "PlanningAssumptions"
	case KindActivityList:
		return "ActivityList"
	case KindNetwork:
		return "Network"
	case KindNormalSolution:
		return "NormalSolution"
	case KindSubcriticalSolution:
		return "SubcriticalSolution"
	case KindCompressedSolution:
		return "CompressedSolution"
	case KindDecompressedSolution:
		return "DecompressedSolution"
	case KindRiskModel:
		return "RiskModel"
	case KindSdpReview:
		return "SdpReview"
	default:
		return fmt.Sprintf("ArtifactKind(%d)", int(k))
	}
}

// WireName returns the canonical camelCase wire name for the ArtifactKind — the
// STRING discriminator the public typed wire contract uses (the SPA reads
// {"kind":"mission","model":{…}}). This is the single source of truth for the
// kind↔name mapping; the REST DTO layer, the model envelopes, and the OpenAPI
// `ArtifactModelEnvelope.kind` enum all derive from it. Distinct from String(),
// which yields the PascalCase Go-identifier name used in error/diagnostic text.
func (k ArtifactKind) WireName() string {
	switch k {
	// ---- Phase 1 ----
	case KindMission:
		return "mission"
	case KindGlossary:
		return "glossary"
	case KindScrubbedRequirements:
		return "scrubbedRequirements"
	case KindVolatilities:
		return "volatilities"
	case KindCoreUseCases:
		return "coreUseCases"
	case KindSystem:
		return "system"
	case KindOperationalConcepts:
		return "operationalConcepts"
	case KindStandardCheck:
		return "standardCheck"
	// ---- Phase 2 ----
	case KindPlanningAssumptions:
		return "planningAssumptions"
	case KindActivityList:
		return "activityList"
	case KindNetwork:
		return "network"
	case KindNormalSolution:
		return "normalSolution"
	case KindSubcriticalSolution:
		return "subcriticalSolution"
	case KindCompressedSolution:
		return "compressedSolution"
	case KindDecompressedSolution:
		return "decompressedSolution"
	case KindRiskModel:
		return "riskModel"
	case KindSdpReview:
		return "sdpReview"
	default:
		return ""
	}
}

// artifactKindByWireName is the inverse of WireName, built once from
// AllArtifactKinds so the two directions can never drift.
var artifactKindByWireName = func() map[string]ArtifactKind {
	m := make(map[string]ArtifactKind, len(AllArtifactKinds()))
	for _, k := range AllArtifactKinds() {
		m[k.WireName()] = k
	}
	return m
}()

// ArtifactKindFromWireName maps a canonical camelCase wire name back to its
// ArtifactKind. Returns (0, false) for an unrecognized name.
func ArtifactKindFromWireName(name string) (ArtifactKind, bool) {
	k, ok := artifactKindByWireName[name]
	return k, ok
}

// MarshalJSON encodes the ArtifactKind as its canonical camelCase wire name
// (a STRING discriminator), so the public typed contract reads
// {"kind":"mission",…} rather than an opaque integer ordinal.
func (k ArtifactKind) MarshalJSON() ([]byte, error) {
	name := k.WireName()
	if name == "" {
		return nil, fmt.Errorf("projectstate: ArtifactKind(%d) has no wire name", int(k))
	}
	return json.Marshal(name)
}

// UnmarshalJSON decodes the string wire name back into the ArtifactKind. For
// backward compatibility with any persisted/legacy integer-ordinal payload it
// also accepts a bare integer.
func (k *ArtifactKind) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		kind, ok := ArtifactKindFromWireName(name)
		if !ok {
			return fmt.Errorf("projectstate: %q is not a recognized ArtifactKind wire name", name)
		}
		*k = kind
		return nil
	}
	var ordinal int
	if err := json.Unmarshal(data, &ordinal); err != nil {
		return fmt.Errorf("projectstate: ArtifactKind must be a string wire name or integer ordinal: %w", err)
	}
	*k = ArtifactKind(ordinal)
	return nil
}

// IsPhase1 reports whether the kind belongs to The Method's Phase 1 (System Design)
// — the phase driven by systemDesignManager. (projectStateAccess.md §3.1)
func (k ArtifactKind) IsPhase1() bool {
	switch k {
	case KindMission,
		KindGlossary,
		KindScrubbedRequirements,
		KindVolatilities,
		KindCoreUseCases,
		KindSystem,
		KindOperationalConcepts,
		KindStandardCheck:
		return true
	default:
		return false
	}
}

// Phase1RequiredKinds returns the ordered set of artifact kinds that must all be
// Committed before Phase 1 can be sealed via advancePhase. Ordering follows the
// Phase-1 design sequence (projectStateAccess.md §3.1, systemDesignManager.md §1.7).
func Phase1RequiredKinds() []ArtifactKind {
	return []ArtifactKind{
		KindMission,
		KindGlossary,
		KindScrubbedRequirements,
		KindVolatilities,
		KindCoreUseCases,
		KindSystem,
		KindOperationalConcepts,
		KindStandardCheck,
	}
}

// IsPhase2 reports whether the kind belongs to The Method's Phase 2 (Project Design)
// — the phase driven by projectDesignManager. (projectStateAccess.md §3.1,
// projectDesignManager.md §2.1)
func (k ArtifactKind) IsPhase2() bool {
	switch k {
	case KindPlanningAssumptions,
		KindActivityList,
		KindNetwork,
		KindNormalSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
		KindDecompressedSolution,
		KindRiskModel,
		KindSdpReview:
		return true
	default:
		return false
	}
}

// IsSolutionKind reports whether the kind is one of the four solution-option slots
// (the four Solution models distinguished by SlotKind). (projectStateAccess.md §3.6)
func (k ArtifactKind) IsSolutionKind() bool {
	switch k {
	case KindNormalSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
		KindDecompressedSolution:
		return true
	default:
		return false
	}
}

// Phase2RequiredKinds returns the ordered set of artifact kinds that must all be
// Committed before Phase 2 can be sealed via advanceToConstruction. Ordering follows
// the Phase-2 design sequence: planning assumptions → activity list → network → the
// four solution options → risk model → SDP review (projectDesignManager.md §2.4/§6.3,
// the-method-* Project-Design phase order).
func Phase2RequiredKinds() []ArtifactKind {
	return []ArtifactKind{
		KindPlanningAssumptions,
		KindActivityList,
		KindNetwork,
		KindNormalSolution,
		KindDecompressedSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
		KindRiskModel,
		KindSdpReview,
	}
}

// SolutionKinds returns the four solution-option slot kinds in the SDP-review row
// order (normal, decompressed-normal, subcritical, compressed).
func SolutionKinds() []ArtifactKind {
	return []ArtifactKind{
		KindNormalSolution,
		KindDecompressedSolution,
		KindSubcriticalSolution,
		KindCompressedSolution,
	}
}
