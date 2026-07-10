package systemdesign

// operatingmodel_prompt_test.go — coverage for the OPERATING-MODEL deployment
// constraint the OperationalConcepts draft prompt carries (founder ruling 2026-07-05,
// from live QA: the gtdapp deployment artifact drafted an arbitrary AWS EKS/RDS/
// CloudFront topology). Archistrator-operated MUST require the platform palette and
// forbid bespoke cloud; self-operated (the default) MUST keep today's open guidance
// (emit nothing extra).

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// platformPaletteMarkers are the exact module names the archistrator-operated
// constraint MUST name so the drafting agent draws only the platform palette.
var platformPaletteMarkers = []string{
	"framework-go-infrastructure-postgres",
	"framework-go-infrastructure-temporal",
	"framework-go-infrastructure-keycloak",
	"framework-go-infrastructure-otel",
	"software/k8s",
	"CloudNativePG",
	"Keycloak",
	"Temporal",
}

// forbiddenCloudMarkers are bespoke-cloud technologies the constraint MUST forbid.
var forbiddenCloudMarkers = []string{"AWS", "RDS", "EKS", "CloudFront"}

func Test_OperationalConceptsPrompt_ArchistratorOperated_RequiresPlatformPalette(t *testing.T) {
	proj := projectstate.Project{OperatingModel: projectstate.OperatingModelArchistratorOperated}
	prompt := architectDraftPrompt(projectstate.KindOperationalConcepts, proj, ReviewFeedback{}, nil, 0)

	if !strings.Contains(prompt, "ARCHISTRATOR-OPERATED") {
		t.Fatalf("archistrator-operated opconcepts prompt missing the operating-model header")
	}
	for _, m := range platformPaletteMarkers {
		if !strings.Contains(prompt, m) {
			t.Errorf("archistrator-operated opconcepts prompt missing required platform marker %q", m)
		}
	}
	for _, m := range forbiddenCloudMarkers {
		if !strings.Contains(prompt, m) {
			t.Errorf("archistrator-operated opconcepts prompt should NAME (to forbid) bespoke-cloud marker %q", m)
		}
	}
	if !strings.Contains(prompt, "FORBIDDEN") {
		t.Errorf("archistrator-operated opconcepts prompt missing the FORBIDDEN clause")
	}
}

func Test_OperationalConceptsPrompt_SelfOperated_KeepsOpenGuidance(t *testing.T) {
	proj := projectstate.Project{OperatingModel: projectstate.OperatingModelSelfOperated}
	prompt := architectDraftPrompt(projectstate.KindOperationalConcepts, proj, ReviewFeedback{}, nil, 0)

	if strings.Contains(prompt, "ARCHISTRATOR-OPERATED") || strings.Contains(prompt, "framework-go-infrastructure-postgres") {
		t.Errorf("self-operated opconcepts prompt must NOT carry the platform-palette constraint")
	}
}

// Test_OperationalConceptsPrompt_UnsetOperatingModel_DefaultsSelfOperated proves a
// pre-field project (empty OperatingModel) is treated as self-operated — the
// back-compat default — so the open guidance is preserved for existing projects.
func Test_OperationalConceptsPrompt_UnsetOperatingModel_DefaultsSelfOperated(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindOperationalConcepts, projectstate.Project{}, ReviewFeedback{}, nil, 0)
	if strings.Contains(prompt, "ARCHISTRATOR-OPERATED") {
		t.Errorf("unset operating model must default to self-operated (no platform constraint)")
	}
}

// Test_OperatingModelConstraint_OnlyOnOperationalConcepts proves the constraint is
// scoped to the deployment-carrying artifact — it must NOT leak into an unrelated
// Phase-1 kind (e.g. Mission) even when the project is archistrator-operated.
func Test_OperatingModelConstraint_OnlyOnOperationalConcepts(t *testing.T) {
	proj := projectstate.Project{OperatingModel: projectstate.OperatingModelArchistratorOperated}
	prompt := architectDraftPrompt(projectstate.KindSystem, proj, ReviewFeedback{}, nil, 0)
	if strings.Contains(prompt, "ARCHISTRATOR-OPERATED") {
		t.Errorf("operating-model deployment constraint leaked into the System (architecture) prompt")
	}
}
