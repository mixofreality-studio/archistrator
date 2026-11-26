package internal_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/designhealth"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	agenticjobfake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
	messagebusfake "github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus/fake"
)

// The design arm's upstream-defect route (FIX-044, and its second defect class, ISS-030-2),
// end to end. The delivery Manager package admits only generated scenario tests, every one of
// them skipped under ruling 8, so the route's judgement (judgeUpstreamDefect and what it
// calls) is pinned here the way variance_upstream_test.go pins the construction arm: the REAL
// delivery worker runs the REAL walk of archistrator's own `architecture` activity in
// Temporal's in-process test environment over the REAL git-backed facets in a throwaway repo,
// with the agent job the one double. The architecture draft's task has reported upstream (the
// report is on the project the walk reads, stamped with the attempt it answers), and every
// probe is a contract read: the attempt ledger's detail, the core use cases' review thread and
// the walks the route started.

const (
	upActivity = "architecture"
	upTask     = "architectureDraft"
	upRule     = "UC-OUTCOME-AT-ASKING-SURFACE"
	upPrior    = "architecture:architectureDraft:0"
)

// upArrangement is the hold arrangement (own committed project, core use cases committed) with the
// architecture activity not started, its draft slot empty, and the given reports on the document.
func upArrangement(t *testing.T, reports []projectstate.UpstreamDefectReport) ([]byte, string) {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(holdArrangementAt(t, projectstate.ReviewCommitted), &top); err != nil {
		t.Fatal(err)
	}
	var rows map[string]json.RawMessage
	if err := json.Unmarshal(top["activityExecution"], &rows); err != nil {
		t.Fatal(err)
	}
	delete(rows, upActivity)
	var slots map[string]json.RawMessage
	if err := json.Unmarshal(top["slots"], &slots); err != nil {
		t.Fatal(err)
	}
	var cuc struct {
		Model struct {
			Decisions []struct {
				UseCase struct {
					ID string `json:"id"`
				} `json:"useCase"`
			} `json:"decisions"`
		} `json:"model"`
	}
	if err := json.Unmarshal(slots["4"], &cuc); err != nil || len(cuc.Model.Decisions) == 0 {
		t.Fatalf("the arranged project holds no committed core use case: %v", err)
	}
	var sys map[string]json.RawMessage
	key := "5"
	if err := json.Unmarshal(slots[key], &sys); err != nil {
		t.Fatal(err)
	}
	sys["status"] = json.RawMessage("0")
	var err error
	if slots[key], err = json.Marshal(sys); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]any{"activityExecution": rows, "slots": slots, "upstreamDefects": reports} {
		if top[k], err = json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return out, cuc.Model.Decisions[0].UseCase.ID
}

func upReport(revision string, rules ...projectstate.UpstreamRuleRef) projectstate.UpstreamDefectReport {
	return projectstate.UpstreamDefectReport{
		ID: revision + ":requirements:coreUseCasesReview", Activity: upActivity, Task: upTask, Revision: revision,
		ActivityType: "requirements", Gate: "coreUseCasesReview", RuleFindings: rules,
		Rationale: "the use case's outcome is not at the asking surface",
	}
}

// upOutcome is what the route did with the report.
type upOutcome struct {
	detail   string
	comments []projectstate.ReviewComment
	reopened []map[string]any
}

// runUpstream runs the architecture walk over the arrangement until the route has settled. When
// priorBounce is set the requirements row exists and carries the note an earlier bounce left.
func runUpstream(t *testing.T, reports func(useCase string) []projectstate.UpstreamDefectReport, priorBounce bool) upOutcome {
	t.Helper()
	_, useCase := upArrangement(t, nil)
	state, _ := upArrangement(t, reports(useCase))
	url := holdRepo(t, state)
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.SetTestTimeout(10 * time.Minute)
	ae := projectstate.NewGitLocalActivityExecutionAccess(url)
	ps := projectstate.NewGitLocalProjectStateAccess(url)
	jobs := &agenticjobfake.FakeAgenticJobAccess{
		SubmitAgenticJobFn: func(fwra.Context, agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
			return agenticjob.PipelineHandle("job-1"), nil
		},
		ObserveAgenticJobFn: func(_ fwra.Context, h agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
			return agenticjob.PipelineObservation{Handle: h, Phase: agenticjob.PhaseSucceeded}, nil
		},
	}
	bus := &messagebusfake.FakeMessageBus{
		DeliverSignalFn: func(fwra.Context, messagebus.ExecutionID, messagebus.SignalName, messagebus.ExecutionPayload, *messagebus.ExecutionStart) error {
			return nil
		},
	}
	mgr := delivery.NewDeliveryManager(envClient{env: env}, ps, nil,
		intervention.NewInterventionEngine(), review.NewReviewEngine(), estimation.NewEstimationEngine(),
		operationestimation.NewOperationEstimationEngine(), billing.NewBillingEngine(), designhealth.NewDesignHealthEngine(),
		jobs, nil, projectstate.NewGitLocalGitActivityStatusAccess(url), projectstate.NewGitLocalDesignSessionAccess(url),
		ae, bus, episode.NewLocalFSEpisodeAccess(url), 0, "tiered")
	delivery.RegisterManagerWorker(envWorker{env: env}, mgr)

	rc := fwra.Context{Context: context.Background()}
	if priorBounce {
		if _, err := ae.OpenActivity(rc, holdProject, 0, "requirements", projectstate.ActivityTypeRequirements,
			projectstate.TestVariantNone, projectstate.LifecyclePin{
				TypeKey:       projectstate.LifecycleKeyFor(projectstate.ActivityTypeRequirements, projectstate.TestVariantNone),
				AssetsVersion: "test",
			}, projectstate.RepoCredential{}, "seed-open"); err != nil {
			t.Fatal(err)
		}
		if _, err := ae.RecordOperatorNote(rc, holdProject, 1, "requirements",
			projectstate.OperatorNoteInput{NoteID: "requirements:note:upstream:" + upPrior, Kind: projectstate.NoteSendBack, Text: "an earlier bounce"}, "",
			projectstate.RepoCredential{}, "seed-note"); err != nil {
			t.Fatal(err)
		}
	}
	var reopened []map[string]any
	env.SetOnChildWorkflowStartedListener(func(info *workflow.Info, _ workflow.Context, args converter.EncodedValues) {
		var in map[string]any
		if err := args.Get(&in); err != nil {
			t.Errorf("the child's input could not be read: %v", err)
			return
		}
		if info.WorkflowExecution.ID == holdProject+":activity:requirements" {
			reopened = append(reopened, in)
		}
	})
	typ, err := json.Marshal(projectstate.ActivityTypeArchitecture)
	if err != nil {
		t.Fatal(err)
	}
	started := env.Now()
	var poll func()
	poll = func() {
		if env.Now().Sub(started) > 10*time.Minute {
			env.CancelWorkflow()
			return
		}
		env.RegisterDelayedCallback(poll, time.Minute)
	}
	env.RegisterDelayedCallback(poll, time.Minute)
	env.ExecuteWorkflow("deliveryActivity", map[string]any{
		"ProjectID": holdProject, "ActivityID": upActivity,
		"Activity": map[string]any{"ActivityID": upActivity, "Type": json.RawMessage(typ)},
	})

	out := upOutcome{reopened: reopened}
	row, err := ae.ReadActivityExecution(rc, holdProject, upActivity)
	if err != nil {
		t.Fatalf("read the architecture ledger: %v", err)
	}
	for _, a := range row.Attempts {
		if string(a.Task) == upTask && a.Attempt == 1 {
			out.detail = a.Detail
		}
	}
	main, err := ps.ReadProject(rc, holdProject)
	if err != nil {
		t.Fatal(err)
	}
	out.comments = main.CoreUseCases.ReviewThread
	return out
}

func (o upOutcome) noted(marker string) []projectstate.ReviewComment {
	var out []projectstate.ReviewComment
	for _, c := range o.comments {
		if strings.Contains(c.Text, marker) {
			out = append(out, c)
		}
	}
	return out
}

func upMarker() string {
	return "[upstream defect report " + projectstate.AttemptID(upActivity, projectstate.MethodTask(upTask), 1) + "]"
}

// A report naming only a core-use-case rule finding is admitted and routed exactly as an
// unrealizable-node report is: the change request is seeded once on the core use cases' thread, it
// names the finding (rule, use case, the refusal's message) and says to amend the shape, and the
// requirements walk is started to re-open the core use cases' gate.
func TestUpstreamRoute_RuleFindingIsSentBackToTheCoreUseCases(t *testing.T) {
	attempt := projectstate.AttemptID(upActivity, projectstate.MethodTask(upTask), 1)
	o := runUpstream(t, func(uc string) []projectstate.UpstreamDefectReport {
		return []projectstate.UpstreamDefectReport{upReport(attempt,
			projectstate.UpstreamRuleRef{RuleID: upRule, UseCase: uc, Finding: "the reply lands at a different Client than the one that asked"})}
	}, false)
	if !strings.Contains(o.detail, "sent back to requirements.coreUseCasesReview") {
		t.Errorf("the attempt's detail %q does not say the report was sent back", o.detail)
	}
	seeded := o.noted(upMarker())
	if len(seeded) != 1 {
		t.Fatalf("%d change requests carry %s on the core use cases' thread, want exactly 1 (detail %q)", len(seeded), upMarker(), o.detail)
	}
	c := seeded[0]
	if c.Type != projectstate.ReviewCommentTypeChangeRequest {
		t.Errorf("the seeded comment is a %v, want a change request", c.Type)
	}
	for _, want := range []string{upRule, "the reply lands at a different Client than the one that asked", "Amend the shape of each named use case"} {
		if !strings.Contains(c.Text, want) {
			t.Errorf("the change request %q does not carry %q", c.Text, want)
		}
	}
	if strings.Contains(c.Text, "Amend each named node") {
		t.Errorf("a rule-only report was told to reclassify nodes: %q", c.Text)
	}
	if len(o.reopened) != 1 {
		t.Fatalf("%d walks of requirements were started, want the one re-open", len(o.reopened))
	}
	reopen, _ := o.reopened[0]["Reopen"].(map[string]any)
	if reopen == nil || reopen["TaskID"] != "coreUseCasesReview" {
		t.Errorf("the re-open = %v, want the core use cases' gate coreUseCasesReview re-armed", o.reopened[0]["Reopen"])
	}
}

// A rule outside the core-use-case family and a finding no refusal stamped are refused, not routed:
// the redraft may correct the report, nothing is seeded and nothing is re-opened.
func TestUpstreamRoute_RefusesAnUnownedOrUnstampedRuleFinding(t *testing.T) {
	attempt := projectstate.AttemptID(upActivity, projectstate.MethodTask(upTask), 1)
	for name, tc := range map[string]struct {
		ref  func(uc string) projectstate.UpstreamRuleRef
		want string
	}{
		"a rule outside the family": {func(uc string) projectstate.UpstreamRuleRef {
			return projectstate.UpstreamRuleRef{RuleID: "CC-COVERAGE", UseCase: uc, Finding: "a node has no call"}
		}, "is not a core-use-case rule"},
		"a finding the verb never stamped": {func(uc string) projectstate.UpstreamRuleRef {
			return projectstate.UpstreamRuleRef{RuleID: upRule, UseCase: uc}
		}, "carries no finding"},
		"a use case that is not committed": {func(string) projectstate.UpstreamRuleRef {
			return projectstate.UpstreamRuleRef{RuleID: upRule, UseCase: "ghost", Finding: "x"}
		}, "no committed use case"},
	} {
		o := runUpstream(t, func(uc string) []projectstate.UpstreamDefectReport {
			return []projectstate.UpstreamDefectReport{upReport(attempt, tc.ref(uc))}
		}, false)
		if !strings.Contains(o.detail, "the report was refused: ") || !strings.Contains(o.detail, tc.want) {
			t.Errorf("%s: the attempt's detail %q does not say the report was refused for %q", name, o.detail, tc.want)
		}
		if n := len(o.noted(upMarker())); n != 0 {
			t.Errorf("%s: a refused report seeded %d change requests", name, n)
		}
		if len(o.reopened) != 0 {
			t.Errorf("%s: a refused report started %d re-opens", name, len(o.reopened))
		}
	}
}

// The PM's cap, one bounce per defect, holds for a rule finding too: the same rule at the same use
// case coming back after the amendment it was sent back for is held for the operator with both
// sides' reasoning, and re-opens nothing a second time.
func TestUpstreamRoute_ARepeatedRuleFindingEscalates(t *testing.T) {
	attempt := projectstate.AttemptID(upActivity, projectstate.MethodTask(upTask), 1)
	o := runUpstream(t, func(uc string) []projectstate.UpstreamDefectReport {
		ref := projectstate.UpstreamRuleRef{RuleID: upRule, UseCase: uc, Finding: "the reply lands at a different Client than the one that asked"}
		earlier := upReport(upPrior, ref)
		earlier.Rationale = "the earlier reasoning"
		return []projectstate.UpstreamDefectReport{earlier, upReport(attempt, ref)}
	}, true)
	for _, want := range []string{"held for the operator", "rule finding " + upRule, "came back after the amendment it was sent back for", "the earlier reasoning"} {
		if !strings.Contains(o.detail, want) {
			t.Errorf("the attempt's detail %q does not say %q", o.detail, want)
		}
	}
	if n := len(o.noted(upMarker())); n != 0 {
		t.Errorf("a repeated finding was sent back again: %d change requests", n)
	}
	if len(o.reopened) != 0 {
		t.Errorf("a repeated finding started %d re-opens", len(o.reopened))
	}
}
