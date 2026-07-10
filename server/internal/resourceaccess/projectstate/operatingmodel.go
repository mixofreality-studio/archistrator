package projectstate

// OperatingModel declares WHO OPERATES the built application — a PROJECT-LEVEL
// choice made at creation (founder ruling 2026-07-05, from live QA: the gtdapp
// deployment artifact drafted an arbitrary AWS EKS/RDS/CloudFront topology, which
// is only legitimate when the customer runs the app themselves).
//
// It is a Method INPUT, deliberately distinguished from the review-gated artifacts:
// it is NOT an ArtifactModel, is NOT held in an ArtifactSlot, and carries NO
// AwaitingReview/Committed lifecycle — a plain, settable field on Project (the same
// posture as ResearchInput / OperatorPaused), set once at creation and read by the
// OperationalConcepts (deployment topology) and PlanningAssumptions (launch
// infrastructure) draft prompts to CONSTRAIN what infrastructure the design may draw.
//
// String-encoded (not an ordinal enum) so the on-disk JSON is self-describing and a
// pre-field project.json decodes cleanly to the DEFAULT (selfOperated) — see
// OrDefault + the decodeProjectDoc default.
type OperatingModel string

const (
	// OperatingModelSelfOperated — the customer runs the built app in their OWN
	// infrastructure (today's behavior; any cloud/infra the design justifies). This
	// is the DEFAULT and the back-compat value for every project that pre-dates the
	// field.
	OperatingModelSelfOperated OperatingModel = "selfOperated"

	// OperatingModelArchistratorOperated — archistrator OPERATES the app on the
	// platform. The design is then CONSTRAINED to the archistrator-platform palette
	// ONLY: CloudNativePG Postgres (framework-go-infrastructure-postgres), Temporal
	// (framework-go-infrastructure-temporal), Keycloak auth
	// (framework-go-infrastructure-keycloak), the otel stack
	// (framework-go-infrastructure-otel), deployed to the platform Kubernetes cluster
	// via the ArgoCD stack at software/k8s. NO AWS RDS/EKS/CloudFront/bespoke cloud —
	// those are self-operated-only.
	OperatingModelArchistratorOperated OperatingModel = "archistratorOperated"
)

// DefaultOperatingModel is the back-compat default applied on read to any project.json
// that pre-dates the field (an empty value). Self-operated preserves today's open
// deployment guidance for every existing project.
const DefaultOperatingModel = OperatingModelSelfOperated

// IsZero reports whether the model is unset (an empty value — a pre-field project).
func (m OperatingModel) IsZero() bool { return m == "" }

// OrDefault returns the model, substituting the DEFAULT (selfOperated) for an empty
// value. Applied on decode so every in-memory Project carries a concrete model and
// every reader (prompts, wire) sees selfOperated for pre-field projects.
func (m OperatingModel) OrDefault() OperatingModel {
	if m == "" {
		return DefaultOperatingModel
	}
	return m
}

// Valid reports whether the model is one of the two known values. Used by the
// SetOperatingModel write pre-condition to reject an unknown wire value.
func (m OperatingModel) Valid() bool {
	switch m {
	case OperatingModelSelfOperated, OperatingModelArchistratorOperated:
		return true
	default:
		return false
	}
}
