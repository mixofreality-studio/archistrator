package projectdesign

// operatingmodel_prompt_test.go — coverage for the OPERATING-MODEL launch-infrastructure
// constraint the PlanningAssumptions draft prompt carries (founder ruling 2026-07-05).
// This is the Phase-2 sibling of the systemDesign OperationalConcepts constraint:
// archistrator-operated MUST fix the launch infrastructure to the platform palette and
// forbid bespoke cloud; self-operated (the default) keeps today's open guidance.

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

var platformPaletteMarkers = []string{
	"framework-go-infrastructure-postgres",
	"framework-go-infrastructure-temporal",
	"framework-go-infrastructure-keycloak",
	"framework-go-infrastructure-otel",
	"software/k8s",
}

var forbiddenCloudMarkers = []string{"AWS", "RDS", "EKS", "CloudFront"}

func Test_PlanningAssumptionsPrompt_ArchistratorOperated_FixesPlatformPalette(t *testing.T) {
	proj := projectstate.Project{OperatingModel: projectstate.OperatingModelArchistratorOperated}
	prompt := architectDraftPrompt(projectstate.KindPlanningAssumptions, proj, "", nil, 0)

	if !strings.Contains(prompt, "ARCHISTRATOR-OPERATED") {
		t.Fatalf("archistrator-operated planning-assumptions prompt missing the operating-model header")
	}
	for _, m := range platformPaletteMarkers {
		if !strings.Contains(prompt, m) {
			t.Errorf("archistrator-operated planning-assumptions prompt missing required platform marker %q", m)
		}
	}
	for _, m := range forbiddenCloudMarkers {
		if !strings.Contains(prompt, m) {
			t.Errorf("archistrator-operated planning-assumptions prompt should NAME (to forbid) bespoke-cloud marker %q", m)
		}
	}
	if !strings.Contains(prompt, "FORBIDDEN") {
		t.Errorf("archistrator-operated planning-assumptions prompt missing the FORBIDDEN clause")
	}
}

func Test_PlanningAssumptionsPrompt_SelfOperated_KeepsOpenGuidance(t *testing.T) {
	proj := projectstate.Project{OperatingModel: projectstate.OperatingModelSelfOperated}
	prompt := architectDraftPrompt(projectstate.KindPlanningAssumptions, proj, "", nil, 0)
	if strings.Contains(prompt, "ARCHISTRATOR-OPERATED") || strings.Contains(prompt, "framework-go-infrastructure-postgres") {
		t.Errorf("self-operated planning-assumptions prompt must NOT carry the platform-palette constraint")
	}
}

// Test_PlanningAssumptionsPrompt_UnsetDefaultsSelfOperated proves a pre-field project
// (empty OperatingModel) is treated as self-operated (the back-compat default).
func Test_PlanningAssumptionsPrompt_UnsetDefaultsSelfOperated(t *testing.T) {
	prompt := architectDraftPrompt(projectstate.KindPlanningAssumptions, projectstate.Project{}, "", nil, 0)
	if strings.Contains(prompt, "ARCHISTRATOR-OPERATED") {
		t.Errorf("unset operating model must default to self-operated (no platform constraint)")
	}
}

// Test_OperatingModelConstraint_OnlyOnPlanningAssumptions proves the constraint is
// scoped to the launch-infrastructure artifact — it must NOT leak into another Phase-2
// kind (e.g. ActivityList) even when the project is archistrator-operated.
func Test_OperatingModelConstraint_OnlyOnPlanningAssumptions(t *testing.T) {
	proj := projectstate.Project{OperatingModel: projectstate.OperatingModelArchistratorOperated}
	prompt := architectDraftPrompt(projectstate.KindActivityList, proj, "", nil, 0)
	if strings.Contains(prompt, "ARCHISTRATOR-OPERATED") {
		t.Errorf("operating-model infrastructure constraint leaked into the ActivityList prompt")
	}
}
