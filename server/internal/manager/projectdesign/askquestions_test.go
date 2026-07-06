package projectdesign

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// askquestions_test.go — F82 coverage for the Project-Design answer-job dispatch path
// (the manager kind=8/PlanningAssumptions goes through THIS manager). Focus: a
// pm-addressed dispatch actually fires, a re-ask re-fires with a fresh key, and a submit
// fault is LOGGED LOUDLY rather than vanishing.

// recordingPipeline is a fake ConstructionPipelineAccess that records every submit and can
// be told to fail — the seam the swallowed error hid.
type recordingPipeline struct {
	specs []constructionpipeline.PipelineSpec
	keys  []fwra.IdempotencyKey
	err   error
}

func (p *recordingPipeline) SubmitConstructionPipeline(rc fwra.Context, spec constructionpipeline.PipelineSpec) (constructionpipeline.PipelineHandle, error) {
	p.specs = append(p.specs, spec)
	p.keys = append(p.keys, rc.IdempotencyKey)
	if p.err != nil {
		return constructionpipeline.PipelineHandle(""), p.err
	}
	return constructionpipeline.PipelineHandle("run-1"), nil
}

func (p *recordingPipeline) ObserveConstructionPipeline(fwra.Context, constructionpipeline.PipelineHandle) (constructionpipeline.PipelineObservation, error) {
	return constructionpipeline.PipelineObservation{}, nil
}

func (p *recordingPipeline) CancelConstructionPipeline(fwra.Context, constructionpipeline.PipelineHandle) error {
	return nil
}

func managerWith(pipe constructionpipeline.ConstructionPipelineAccess, repoOK bool) *projectDesignManager {
	return &projectDesignManager{
		pipeline: pipe,
		repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRef("acme|acme/gtdapp"), repoOK
		},
	}
}

func sampleQuestions() []projectstate.ReviewComment {
	return questionsToLedger(projectstate.ReviewAddresseePM, []AnchoredComment{
		{JSONPath: "$.assumptions[0]", Text: "Is the calendar 5 days/week?", AnchorText: "calendar"},
	})
}

// A pm-addressed dispatch actually submits a job_mode=answer run to the project repo.
func TestDispatchAnswerJob_FiresForPM(t *testing.T) {
	pipe := &recordingPipeline{}
	m := managerWith(pipe, true)
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, sampleQuestions())

	if len(pipe.specs) != 1 {
		t.Fatalf("expected exactly one answer-job submit, got %d", len(pipe.specs))
	}
	spec := pipe.specs[0]
	if spec.DispatchInputs[dispatchInputJobMode] != jobModeAnswer {
		t.Fatalf("answer job must dispatch with job_mode=answer, got %q", spec.DispatchInputs[dispatchInputJobMode])
	}
	if spec.TargetRepo.Owner != "acme" || spec.TargetRepo.Name != "gtdapp" {
		t.Fatalf("answer job must target the project repo, got %+v", spec.TargetRepo)
	}
	if !strings.Contains(spec.DispatchInputs[dispatchInputDesignPrompt], "Product Manager") {
		t.Fatalf("a pm-addressed answer prompt must put the agent in the Product Manager role")
	}
}

// Re-asking re-fires the answer job with a DIFFERENT idempotency key (F82 recovery), so the
// RA does not dedup the re-dispatch away.
func TestDispatchAnswerJob_ReFiresWithUniqueKey(t *testing.T) {
	pipe := &recordingPipeline{}
	m := managerWith(pipe, true)
	qs := sampleQuestions()
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, qs)
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, qs)

	if len(pipe.keys) != 2 {
		t.Fatalf("expected two submits, got %d", len(pipe.keys))
	}
	if pipe.keys[0] == pipe.keys[1] {
		t.Fatalf("re-ask must re-fire with a DIFFERENT key (else the RA dedups it away); both were %q", pipe.keys[0])
	}
}

// A submit FAULT is logged loudly (ERROR) instead of vanishing — the F82 root-cause fix.
func TestDispatchAnswerJob_LogsSubmitFailure(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	pipe := &recordingPipeline{err: fwra.New(fwra.Infrastructure, "boom")}
	m := managerWith(pipe, true)
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, sampleQuestions())

	out := buf.String()
	if !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "dispatch FAILED") {
		t.Fatalf("a submit failure must be logged at ERROR; log was:\n%s", out)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("the failure log must carry the underlying error; log was:\n%s", out)
	}
}

// A rail-less (nil pipeline/repo) server logs a WARN and does not attempt a submit.
func TestDispatchAnswerJob_RailDormantWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	m := &projectDesignManager{} // no pipeline, no repo
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, sampleQuestions())
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("a dormant rail must WARN that the question will not be auto-answered; log was:\n%s", buf.String())
	}
}

// An unresolved repo logs an ERROR and does not submit.
func TestDispatchAnswerJob_RepoUnresolvedErrors(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	pipe := &recordingPipeline{}
	m := managerWith(pipe, false) // repo resolver returns ok=false
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, sampleQuestions())
	if len(pipe.specs) != 0 {
		t.Fatalf("no submit must be attempted when the repo does not resolve")
	}
	if !strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("an unresolved repo must be logged at ERROR; log was:\n%s", buf.String())
	}
}

func TestExistingQuestionRound(t *testing.T) {
	qs := sampleQuestions()
	// Seeded at round 3 in the thread.
	thread := []projectstate.ReviewComment{{
		Type: projectstate.ReviewCommentTypeQuestion, Round: 3,
		Addressee: qs[0].Addressee, Anchor: qs[0].Anchor, Text: qs[0].Text,
	}}
	if r, ok := existingQuestionRound(thread, qs); !ok || r != 3 {
		t.Fatalf("existingQuestionRound must find the prior seeding at round 3, got r=%d ok=%v", r, ok)
	}
	// A never-seeded question is not found.
	if _, ok := existingQuestionRound(nil, qs); ok {
		t.Fatal("existingQuestionRound must report not-found for an empty thread")
	}
}

func TestAnswerJobDispatchKey_Unique(t *testing.T) {
	qs := sampleQuestions()
	k1 := answerJobDispatchKey("gtdapp", KindPlanningAssumptions, "", qs)
	k2 := answerJobDispatchKey("gtdapp", KindPlanningAssumptions, "", qs)
	if k1 == k2 {
		t.Fatalf("answerJobDispatchKey must be unique per call, both were %q", k1)
	}
}
