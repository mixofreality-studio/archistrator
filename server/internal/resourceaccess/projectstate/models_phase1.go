package projectstate

import (
	"errors"
	"fmt"
)

// ---- MissionStatement — ch. 5 business alignment ----

// Objective is a single numbered business objective in a MissionStatement.
// (projectStateAccess.md §3.5)
type Objective struct {
	Number    int    `json:"number"`
	Statement string `json:"statement"`
}

// MissionStatement is the typed artifact for ch. 5 business alignment.
// Vision is ONE terse sentence; Objectives are from the business perspective,
// numbered; Mission is expressed in components, not features.
// (projectStateAccess.md §3.5)
type MissionStatement struct {
	Vision     string      `json:"vision"`     // ONE terse sentence
	Objectives []Objective `json:"objectives"` // business perspective only; numbered
	Mission    string      `json:"mission"`    // expressed in components, not features
}

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (m *MissionStatement) Kind() ArtifactKind { return KindMission }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (m *MissionStatement) isArtifactModel() {}

// NewMissionStatement constructs a MissionStatement after validating SHAPE only.
// Shape check: Vision must be non-empty. Semantic rules (e.g. objective count,
// wording) are enforced by artifactValidationEngine, not here.
// (projectStateAccess.md §3.5 "smart constructors enforce SHAPE only")
func NewMissionStatement(vision string, objectives []Objective, mission string) (*MissionStatement, error) {
	if vision == "" {
		return nil, errors.New("projectstate.NewMissionStatement: Vision must not be empty")
	}
	return &MissionStatement{
		Vision:     vision,
		Objectives: objectives,
		Mission:    mission,
	}, nil
}

// ---- Glossary — ch. 3 "What's in a Name" ----

// GlossaryItem is one entry in the system Glossary.
// Category aligns with the Four Questions: Who / What / How-activity / Where.
// (projectStateAccess.md §3.5)
type GlossaryItem struct {
	Term       string `json:"term"`
	Definition string `json:"definition"`
	Category   string `json:"category"` // Who / What / How-activity / Where (the Four Questions), optional
}

// Glossary is the typed artifact for the system ubiquitous language per ch. 3.
// (projectStateAccess.md §3.5)
type Glossary struct {
	Items []GlossaryItem `json:"items"`
}

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (g *Glossary) Kind() ArtifactKind { return KindGlossary }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (g *Glossary) isArtifactModel() {}

// NewGlossary constructs a Glossary after validating SHAPE only.
// Shape checks: no item may have an empty Term or empty Definition.
// Semantic rules (completeness, Four-Question coverage) are enforced by
// artifactValidationEngine, not here.
// (projectStateAccess.md §3.5 "smart constructors enforce SHAPE only")
func NewGlossary(items []GlossaryItem) (*Glossary, error) {
	for i, item := range items {
		if item.Term == "" {
			return nil, fmt.Errorf("projectstate.NewGlossary: item at index %d has empty Term", i)
		}
		if item.Definition == "" {
			return nil, fmt.Errorf("projectstate.NewGlossary: item at index %d has empty Definition", i)
		}
	}
	return &Glossary{Items: items}, nil
}

// ---- Volatilities — ch. 2, the two axes ----

// Axis is the closed 2-set of volatility axes per ch. 2.
// (projectStateAccess.md §3.5)
type Axis int

const (
	AxisSameCustomerOverTime  Axis = iota // one customer over time
	AxisAllCustomersAtOneTime             // all customers at one time
)

// Volatility is a single identified volatility with its axis and rationale.
// (projectStateAccess.md §3.5)
type Volatility struct {
	Name      string `json:"name"` // bolded volatility name
	Rationale string `json:"rationale"`
	Axis      Axis   `json:"axis"`
}

// Volatilities is the typed artifact for the ch. 2 volatility analysis.
// Grouped by Axis on render via artifactRenderingAccess.
// (projectStateAccess.md §3.5)
type Volatilities struct {
	Items []Volatility `json:"items"`
}

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (v *Volatilities) Kind() ArtifactKind { return KindVolatilities }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (v *Volatilities) isArtifactModel() {}

// ---- CoreUseCases — ch. 4 ----

// UseCaseDecision pairs a UseCase with its inclusion/exclusion rationale.
// RejectionReason is empty when the use case is core; it carries the reason
// when the use case was evaluated and rejected as a permutation.
// (projectStateAccess.md §3.5)
type UseCaseDecision struct {
	UseCase         UseCase `json:"useCase"`
	RejectionReason string  `json:"rejectionReason"` // "" when core; reason when rejected as a permutation
}

// CoreUseCases is the slot-level typed artifact for the ch. 4 core use-case
// selection. It holds the raw list and the core selection.
//
// Constraint (enforced by artifactValidationEngine, not here): a CoreUseCases
// collection must hold 2–6 UseCase values with Classification==ClassCore.
// (projectStateAccess.md §3.5)
type CoreUseCases struct {
	Decisions []UseCaseDecision `json:"decisions"`
}

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (c *CoreUseCases) Kind() ArtifactKind { return KindCoreUseCases }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (c *CoreUseCases) isArtifactModel() {}

// ---- OperationalConcepts — ch. 5 ----

// OperationalDecision is one infrastructure/topology decision, justified
// against a numbered business objective. (projectStateAccess.md §3.5)
type OperationalDecision struct {
	Topic               string `json:"topic"` // e.g. "communication topology", "sync vs queued", "pub/sub edges"
	Decision            string `json:"decision"`
	JustifyingObjective int    `json:"justifyingObjective"` // Objective number from MissionStatement
}

// DeliveryStyle is the closed set of system delivery styles. The set of deployment
// environments is DERIVED from it (test is always present): cloud→{cloud,test},
// local→{local,test}, both→{cloud,local,test}. (spec-2026-06-03 Decision 4)
type DeliveryStyle int

const (
	StyleCloud DeliveryStyle = iota
	StyleLocal
	StyleBoth
)

// DeploymentProfile is the closed set of deployment environment profiles.
// (spec-2026-06-03 Decision 4)
type DeploymentProfile int

const (
	ProfileCloud DeploymentProfile = iota
	ProfileLocal
	ProfileTest
)

// ContainerInstance places a System Component into a deployment node.
// ComponentID must reference a real System Component (cross-referenced by the
// artifactValidationEngine's DEP-INSTANCE-EXIST predicate, not here).
type ContainerInstance struct {
	ComponentID ComponentID `json:"componentId"` // must reference a System Component
	Note        string      `json:"note"`        // per-profile instance note (optional)
}

// DeploymentNode is nestable: cluster → namespace → instance.
type DeploymentNode struct {
	Name       string              `json:"name"`
	Technology string              `json:"technology"`
	Children   []DeploymentNode    `json:"children"`
	Instances  []ContainerInstance `json:"instances"`
}

// DeploymentEnvironment is the set of nodes for one DeploymentProfile.
type DeploymentEnvironment struct {
	Profile DeploymentProfile `json:"profile"`
	Title   string            `json:"title"`
	Nodes   []DeploymentNode  `json:"nodes"`
}

// DeploymentTopology is the typed deployment model carried by OperationalConcepts.
// The deployed component graph is identical across profiles (instances swapped at
// the durable-execution / client-transport / git seams); enforced by the
// artifactValidationEngine's DEP-* predicates, not here.
type DeploymentTopology struct {
	DeliveryStyle DeliveryStyle           `json:"deliveryStyle"`
	Environments  []DeploymentEnvironment `json:"environments"`
}

// OperationalConcepts is the typed artifact for the ch. 5 operational-concepts
// section. Each decision is justified against a business objective. It also
// carries the typed deployment topology (spec-2026-06-03 Decision 2).
// (projectStateAccess.md §3.5)
type OperationalConcepts struct {
	Decisions  []OperationalDecision `json:"decisions"`
	Deployment DeploymentTopology    `json:"deployment"` // optional; zero value is an empty topology
}

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (o *OperationalConcepts) Kind() ArtifactKind { return KindOperationalConcepts }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (o *OperationalConcepts) isArtifactModel() {}

// ---- StandardCheck — App C design-standard walk ----

// CheckStatus is the outcome of a single App C design-standard item.
// (projectStateAccess.md §3.5)
type CheckStatus int

const (
	CheckPass   CheckStatus = iota // item passes the guideline
	CheckWaived                    // item waived with written justification
	CheckFail                      // item fails
)

// CheckItem is one row of the App C design-standard walk.
// Justification is required when Status == CheckWaived.
// (projectStateAccess.md §3.5)
type CheckItem struct {
	Section       string      `json:"section"` // App C section, e.g. "§3.4"
	Guideline     string      `json:"guideline"`
	Status        CheckStatus `json:"status"`
	Justification string      `json:"justification"` // required when CheckWaived
}

// StandardCheck is the typed artifact for the App C design-standard walk.
// (projectStateAccess.md §3.5)
type StandardCheck struct {
	Items []CheckItem `json:"items"`
}

// Kind implements ArtifactModel. (projectStateAccess.md §3.5)
func (s *StandardCheck) Kind() ArtifactKind { return KindStandardCheck }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (s *StandardCheck) isArtifactModel() {}

// ---- ScrubbedRequirements — OQ-2 (artifactValidationEngine.md) ----

// Requirement is a single scrubbed requirement item.
// (artifactValidationEngine.md OQ-2; projectStateAccess.md KindScrubbedRequirements)
type Requirement struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
}

// ScrubbedRequirements is the typed artifact holding the set of scrubbed
// requirements that the validation Engine cross-references.
// (artifactValidationEngine.md OQ-2; identity.go KindScrubbedRequirements)
type ScrubbedRequirements struct {
	Items []Requirement `json:"items"`
}

// Kind implements ArtifactModel. (identity.go KindScrubbedRequirements)
func (r *ScrubbedRequirements) Kind() ArtifactKind { return KindScrubbedRequirements }

// isArtifactModel seals the ArtifactModel sum to this package's models.
func (r *ScrubbedRequirements) isArtifactModel() {}
