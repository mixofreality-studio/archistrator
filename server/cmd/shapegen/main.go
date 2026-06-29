// cmd/shapegen emits a canonical project.json with one minimal-but-valid example
// per artifact slot, using the real projectstate codec. The output is the ground-
// truth shape reference every slot-authoring task (Tasks 4–20) mirrors so there is
// no guesswork about enum encodings or field names.
//
// Usage:
//
//	cd products/archistrator/server
//	go run ./cmd/shapegen
//
// Output is written to .git/sdd/archistrator-shapes.json (durable across sessions)
// AND printed to stdout.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func main() {
	p := buildExampleProject()

	raw, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "EncodeProjectJSON: %v\n", err)
		os.Exit(1)
	}

	// Pretty-print for readability (EncodeProjectJSON already uses MarshalIndent,
	// but compact-then-indent gives us a canonical re-indent pass).
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		fmt.Fprintf(os.Stderr, "re-marshal unmarshal: %v\n", err)
		os.Exit(1)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "re-marshal indent: %v\n", err)
		os.Exit(1)
	}

	// Round-trip decode to prove the shape is valid.
	proj2, ok, err := projectstate.DecodeProjectJSON(raw, "archistrator")
	if err != nil || !ok {
		fmt.Fprintf(os.Stderr, "round-trip decode failed: ok=%v err=%v\n", ok, err)
		os.Exit(1)
	}
	_ = proj2

	// Write to durable output path.
	outPath := "/Users/davidmarne/mixofrealitystudio/software/.git/sdd/archistrator-shapes.json"
	if err := os.WriteFile(outPath, pretty, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", outPath, err)
		os.Exit(1)
	}

	// Also print to stdout.
	fmt.Println(string(pretty))
	fmt.Fprintf(os.Stderr, "\nwrote %s\n", outPath)
	fmt.Fprintln(os.Stderr, "round-trip: OK")
}

// buildExampleProject constructs a Project with one minimal-but-valid example per
// slot. Every slot is set to ReviewCommitted so the shape reflects the committed state
// slot-authoring tasks produce.
func buildExampleProject() projectstate.Project {
	p := projectstate.Project{
		ID:      "archistrator",
		Version: 1,
		Phase:   projectstate.PhaseSystemDesign,
		Owner:   "davemarne@gmail.com",
		Name:    "archistrator",
	}

	// ---- Slot 0: Mission (KindMission = 0) ----
	mission, err := projectstate.NewMissionStatement(
		"Deliver a method-driven design tool that guides software architects through The Method.",
		[]projectstate.Objective{
			{Number: 1, Statement: "Enable architects to produce validated, method-compliant system designs."},
		},
		"Expose archistrator as a server-side API consumed by an agentic design workflow.",
	)
	if err != nil {
		panic("mission: " + err.Error())
	}
	p.Mission = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: mission}

	// ---- Slot 1: Glossary (KindGlossary = 1) ----
	glossary, err := projectstate.NewGlossary([]projectstate.GlossaryItem{
		{Term: "Project", Definition: "A software system being designed through The Method.", Category: "What"},
		{Term: "Architect", Definition: "The human who reviews and commits artifact drafts.", Category: "Who"},
	})
	if err != nil {
		panic("glossary: " + err.Error())
	}
	p.Glossary = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: glossary}

	// ---- Slot 2: ScrubbedRequirements (KindScrubbedRequirements = 2) ----
	scrubbed := &projectstate.ScrubbedRequirements{
		Items: []projectstate.Requirement{
			{ID: "REQ-1", Statement: "The system shall expose a REST API for artifact management."},
			{ID: "REQ-2", Statement: "The system shall validate artifacts against The Method's rules."},
		},
	}
	p.ScrubbedRequirements = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: scrubbed}

	// ---- Slot 3: Volatilities (KindVolatilities = 3) ----
	volatilities := &projectstate.Volatilities{
		Items: []projectstate.Volatility{
			{
				Name:      "Rendering format",
				Rationale: "Diagram rendering may evolve from Structurizr DSL to other formats.",
				Axis:      projectstate.AxisSameCustomerOverTime,
			},
			{
				Name:      "Validation rule set",
				Rationale: "The Method's rules may expand as the book is refined.",
				Axis:      projectstate.AxisAllCustomersAtOneTime,
			},
		},
	}
	p.Volatilities = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: volatilities}

	// ---- Slot 4: CoreUseCases (KindCoreUseCases = 4) ----
	// UseCase with an ActivityDiagram (nodes + edges) to show the full shape.
	activityDiagram := &projectstate.ActivityDiagram{
		Nodes: []projectstate.ActivityNode{
			{ID: "start", Kind: projectstate.NodeStart, Label: ""},
			{ID: "submit-draft", Kind: projectstate.NodeAction, Label: "Submit draft"},
			{ID: "validate", Kind: projectstate.NodeDecision, Label: "Valid?"},
			{ID: "store-commit", Kind: projectstate.NodeAction, Label: "Store committed slot"},
			{ID: "return-error", Kind: projectstate.NodeAction, Label: "Return validation error"},
			{ID: "merge", Kind: projectstate.NodeMerge, Label: ""},
			{ID: "end", Kind: projectstate.NodeEnd, Label: ""},
		},
		Edges: []projectstate.ActivityEdge{
			{From: "start", To: "submit-draft", Kind: projectstate.EdgeControlFlow},
			{From: "submit-draft", To: "validate", Kind: projectstate.EdgeControlFlow},
			{From: "validate", To: "store-commit", Kind: projectstate.EdgeGuardedFlow, Guard: "[yes]"},
			{From: "validate", To: "return-error", Kind: projectstate.EdgeGuardedFlow, Guard: "[no]"},
			{From: "store-commit", To: "merge", Kind: projectstate.EdgeControlFlow},
			{From: "return-error", To: "merge", Kind: projectstate.EdgeControlFlow},
			{From: "merge", To: "end", Kind: projectstate.EdgeControlFlow},
		},
	}
	uc1, err := projectstate.NewUseCase(projectstate.UseCase{
		ID:             "co-author-method-artifact",
		Name:           "Co-author method artifact",
		Actors:         []projectstate.Actor{{ID: "architect", Role: "Architect"}},
		Trigger:        projectstate.TriggerClientAction,
		Classification: projectstate.ClassCore,
		Activity:       activityDiagram,
	})
	if err != nil {
		panic("uc1: " + err.Error())
	}
	coreUseCases := &projectstate.CoreUseCases{
		Decisions: []projectstate.UseCaseDecision{
			{UseCase: *uc1, RejectionReason: ""},
		},
	}
	p.CoreUseCases = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: coreUseCases}

	// ---- Slot 5: SystemDesign / System (KindSystem = 5) ----
	compManager := projectstate.Component{
		ID:           "system-design-manager",
		Name:         "SystemDesignManager",
		Kind:         projectstate.CompManager,
		Layer:        projectstate.LayerManager,
		Encapsulates: "system design workflow",
	}
	compRA := projectstate.Component{
		ID:                  "project-state-access",
		Name:                "ProjectStateAccess",
		Kind:                projectstate.CompResourceAccess,
		Layer:               projectstate.LayerResourceAccess,
		Encapsulates:        "project head-state aggregate",
		AtomicBusinessVerbs: []string{"StageArtifactForReview", "CommitArtifact", "ReadProject"},
	}
	compResource := projectstate.Component{
		ID:    "git-store",
		Name:  "GitStore",
		Kind:  projectstate.CompResource,
		Layer: projectstate.LayerResource,
	}
	compClient := projectstate.Component{
		ID:    "design-client",
		Name:  "DesignClient",
		Kind:  projectstate.CompClient,
		Layer: projectstate.LayerClient,
	}
	compEngine := projectstate.Component{
		ID:           "artifact-validation-engine",
		Name:         "ArtifactValidationEngine",
		Kind:         projectstate.CompEngine,
		Layer:        projectstate.LayerEngine,
		Encapsulates: "artifact semantic validation rules",
	}
	system, err := projectstate.NewSystem(
		[]projectstate.Component{compClient, compManager, compEngine, compRA, compResource},
		[]projectstate.Relationship{
			{From: "design-client", To: "system-design-manager", Mode: projectstate.CallSync, Label: "stageArtifactForReview"},
			{From: "system-design-manager", To: "artifact-validation-engine", Mode: projectstate.CallSync, Label: "validate"},
			{From: "system-design-manager", To: "project-state-access", Mode: projectstate.CallSync, Label: "stageArtifactForReview"},
			{From: "project-state-access", To: "git-store", Mode: projectstate.CallSync, Label: "commit"},
		},
		[]projectstate.DynamicView{
			{
				UseCaseID:    "co-author-method-artifact",
				Key:          "uc1-co-author-method-artifact",
				Title:        "Co-author method artifact",
				Participants: []projectstate.ComponentID{"design-client", "system-design-manager", "artifact-validation-engine", "project-state-access"},
				Edges: []projectstate.Relationship{
					{From: "design-client", To: "system-design-manager", Mode: projectstate.CallSync, Label: "stageArtifactForReview"},
					{From: "system-design-manager", To: "artifact-validation-engine", Mode: projectstate.CallSync, Label: "validate"},
					{From: "system-design-manager", To: "project-state-access", Mode: projectstate.CallSync, Label: "stageArtifactForReview"},
				},
			},
		},
	)
	if err != nil {
		panic("system: " + err.Error())
	}
	p.SystemDesign = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: system}

	// ---- Slot 6: OperationalConcepts (KindOperationalConcepts = 6) ----
	opConcepts := &projectstate.OperationalConcepts{
		Decisions: []projectstate.OperationalDecision{
			{
				Topic:               "communication topology",
				Decision:            "Synchronous REST API between client and manager; no pub/sub.",
				JustifyingObjective: 1,
			},
		},
		Deployment: projectstate.DeploymentTopology{
			DeliveryStyle: projectstate.StyleCloud,
			Environments: []projectstate.DeploymentEnvironment{
				{
					Profile: projectstate.ProfileCloud,
					Title:   "Cloud",
					Nodes: []projectstate.DeploymentNode{
						{
							Name:       "Kubernetes cluster",
							Technology: "k8s",
							Children: []projectstate.DeploymentNode{
								{
									Name:       "archistrator namespace",
									Technology: "k8s-namespace",
									Instances: []projectstate.ContainerInstance{
										{ComponentID: "system-design-manager", Note: "Ktor server pod"},
									},
								},
							},
						},
					},
				},
				{
					Profile: projectstate.ProfileTest,
					Title:   "Test",
					Nodes: []projectstate.DeploymentNode{
						{
							Name:       "test process",
							Technology: "in-process",
							Instances: []projectstate.ContainerInstance{
								{ComponentID: "system-design-manager", Note: "embedded test server"},
							},
						},
					},
				},
			},
		},
	}
	p.OperationalConcepts = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: opConcepts}

	// ---- Slot 7: StandardCheck (KindStandardCheck = 7) ----
	stdCheck := &projectstate.StandardCheck{
		Items: []projectstate.CheckItem{
			{
				Section:   "§3.4",
				Guideline: "Each Manager encapsulates exactly one workflow volatility.",
				Status:    projectstate.CheckPass,
			},
			{
				Section:       "§4.2",
				Guideline:     "Use cases must have 2–6 core entries.",
				Status:        projectstate.CheckWaived,
				Justification: "Single core use case sufficient for MVP scope; will expand in Phase 2.",
			},
		},
	}
	p.StandardCheck = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: stdCheck}

	// ---- Slot 8: PlanningAssumptions (KindPlanningAssumptions = 8) ----
	planningAssumptions := &projectstate.PlanningAssumptions{
		Resources:           []string{"senior-engineer", "junior-engineer"},
		CalendarDaysPerWeek: 5.0,
		InfrastructureKind:  projectstate.InfrastructureKindGoTemporalPostgres,
		DeclaredUsage: projectstate.UsageAssumption{
			ExpectedDailyActiveUsers: 100,
			RequestsPerMinute:        10.0,
			AvgPayloadBytes:          4096,
		},
		Terms: projectstate.SettlementTerms{
			RevenueShare:         projectstate.RevenueShareLaunchFlat10,
			RevenueSharePercent:  10.0,
			ComputeCost:          projectstate.ComputeCostFlatMarkup,
			ComputeMarkupPercent: 15.0,
			Schedule:             projectstate.ScheduleMonthly,
		},
		Notes: "Initial planning assumptions for archistrator MVP.",
	}
	p.PlanningAssumptions = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: planningAssumptions}

	// ---- Slot 9: ActivityList (KindActivityList = 9) ----
	activityList := &projectstate.ActivityList{
		Activities: []projectstate.ActivityItem{
			{Name: "Domain model implementation", EffortDays: 5, WorkerClass: "senior-engineer", Coding: true, RiskBucket: 3},
			{Name: "Server API implementation", EffortDays: 10, WorkerClass: "senior-engineer", Coding: true, RiskBucket: 5},
			{Name: "Validation engine", EffortDays: 5, WorkerClass: "senior-engineer", Coding: true, RiskBucket: 2},
			{Name: "Integration testing", EffortDays: 5, WorkerClass: "junior-engineer", Coding: false, RiskBucket: 2},
		},
	}
	p.ActivityList = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: activityList}

	// ---- Slot 10: Network (KindNetwork = 10) ----
	network := &projectstate.Network{
		Dependencies: []projectstate.NetworkDependency{
			{Activity: "Server API implementation", DependsOn: []string{"Domain model implementation"}},
			{Activity: "Validation engine", DependsOn: []string{"Domain model implementation"}},
			{Activity: "Integration testing", DependsOn: []string{"Server API implementation", "Validation engine"}},
		},
		CriticalPath: []string{"Domain model implementation", "Server API implementation", "Integration testing"},
	}
	p.Network = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: network}

	// ---- Slot 11: NormalSolution (KindNormalSolution = 11) ----
	normalSolution := projectstate.NewSolution(projectstate.KindNormalSolution)
	normalSolution.StaffingCap = 2
	normalSolution.CalendarDaysPerWeek = 5.0
	normalSolution.ClassRates = map[string]projectstate.Money{
		"senior-engineer": {MinorUnits: 80000, Currency: "USD"},
		"junior-engineer": {MinorUnits: 50000, Currency: "USD"},
	}
	normalSolution.BufferDays = 0
	p.NormalSolution = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: normalSolution}

	// ---- Slot 12: SubcriticalSolution (KindSubcriticalSolution = 12) ----
	subcriticalSolution := projectstate.NewSolution(projectstate.KindSubcriticalSolution)
	subcriticalSolution.StaffingCap = 1
	subcriticalSolution.CalendarDaysPerWeek = 5.0
	subcriticalSolution.ClassRates = map[string]projectstate.Money{
		"senior-engineer": {MinorUnits: 80000, Currency: "USD"},
	}
	subcriticalSolution.BufferDays = 0
	p.SubcriticalSolution = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: subcriticalSolution}

	// ---- Slot 13: CompressedSolution (KindCompressedSolution = 13) ----
	compressedSolution := projectstate.NewSolution(projectstate.KindCompressedSolution)
	compressedSolution.StaffingCap = 3
	compressedSolution.CalendarDaysPerWeek = 5.0
	compressedSolution.ClassRates = map[string]projectstate.Money{
		"senior-engineer": {MinorUnits: 80000, Currency: "USD"},
		"junior-engineer": {MinorUnits: 50000, Currency: "USD"},
	}
	compressedSolution.BufferDays = 0
	p.CompressedSolution = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: compressedSolution}

	// ---- Slot 14: DecompressedSolution (KindDecompressedSolution = 14) ----
	decompressedSolution := projectstate.NewSolution(projectstate.KindDecompressedSolution)
	decompressedSolution.StaffingCap = 2
	decompressedSolution.CalendarDaysPerWeek = 4.0
	decompressedSolution.ClassRates = map[string]projectstate.Money{
		"senior-engineer": {MinorUnits: 80000, Currency: "USD"},
		"junior-engineer": {MinorUnits: 50000, Currency: "USD"},
	}
	decompressedSolution.BufferDays = 5.0
	p.DecompressedSolution = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: decompressedSolution}

	// ---- Slot 15: RiskModel (KindRiskModel = 15) ----
	riskModel := &projectstate.RiskModel{
		Rows: []projectstate.RiskRow{
			{SolutionKind: projectstate.KindNormalSolution, CriticalityRisk: 0.3, ActivityRisk: 0.2, Composite: 0.44},
			{SolutionKind: projectstate.KindSubcriticalSolution, CriticalityRisk: 0.5, ActivityRisk: 0.3, Composite: 0.65},
			{SolutionKind: projectstate.KindCompressedSolution, CriticalityRisk: 0.2, ActivityRisk: 0.2, Composite: 0.36},
			{SolutionKind: projectstate.KindDecompressedSolution, CriticalityRisk: 0.2, ActivityRisk: 0.1, Composite: 0.28},
		},
	}
	p.RiskModel = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: riskModel}

	// ---- Slot 16: SdpReview (KindSdpReview = 16) ----
	sdpReview := &projectstate.SdpReview{
		Options: []projectstate.SdpOptionRow{
			{
				OptionID:             "opt-normal",
				SolutionKind:         projectstate.KindNormalSolution,
				DurationDays:         25,
				BuildCost:            projectstate.Money{MinorUnits: 2000000, Currency: "USD"},
				CompositeRisk:        0.44,
				ProjectedMonthlyCost: projectstate.Money{MinorUnits: 50000, Currency: "USD"},
				ExpectedPerCycleNet:  projectstate.Money{MinorUnits: 100000, Currency: "USD"},
				RevenueSharePercent:  10.0,
			},
		},
		Recommendation: "opt-normal",
		Rationale:      "Normal schedule balances cost and risk within acceptable bounds.",
	}
	p.SdpReview = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: sdpReview}

	return p
}
