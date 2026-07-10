package systemdesign

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

// askquestions_dispatch_test.go — F82 coverage for the System-Design answer-job dispatch
// twin (the manager kinds 2/5 go through THIS manager). Mirrors the projectdesign tests:
// a dispatch fires (incl. on a LIVE session branch), re-fires with a fresh key, and logs a
// submit fault loudly.

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

func sdManagerWith(pipe constructionpipeline.ConstructionPipelineAccess) *systemDesignManager {
	return &systemDesignManager{
		pipeline: pipe,
		repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRef("acme|acme/gtdapp"), true
		},
	}
}

func sdQuestions() []projectstate.ReviewComment {
	return questionsToLedger(projectstate.ReviewAddresseeArchitect, []AnchoredComment{
		{JSONPath: "$.components[0]", Text: "Which layer owns settlement?", AnchorText: "settlement"},
	})
}

// Dispatch fires for a LIVE session (non-empty branch) — the answer job rides the session
// branch tip where the ledger lives.
func TestSDDispatchAnswerJob_FiresOnLiveSessionBranch(t *testing.T) {
	pipe := &recordingPipeline{}
	m := sdManagerWith(pipe)
	const branch = "aiarch-design/gtdapp/5"
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindSystem, branch, projectstate.ReviewAddresseeArchitect, sdQuestions())

	if len(pipe.specs) != 1 {
		t.Fatalf("expected one submit, got %d", len(pipe.specs))
	}
	spec := pipe.specs[0]
	if spec.DispatchInputs[dispatchInputJobMode] != jobModeAnswer {
		t.Fatalf("job_mode must be answer, got %q", spec.DispatchInputs[dispatchInputJobMode])
	}
	if spec.DispatchInputs[dispatchInputTargetBranch] != branch {
		t.Fatalf("the answer job must target the live session branch, got %q", spec.DispatchInputs[dispatchInputTargetBranch])
	}
}

func TestSDDispatchAnswerJob_ReFiresWithUniqueKey(t *testing.T) {
	pipe := &recordingPipeline{}
	m := sdManagerWith(pipe)
	qs := sdQuestions()
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindSystem, "", projectstate.ReviewAddresseeArchitect, qs)
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindSystem, "", projectstate.ReviewAddresseeArchitect, qs)
	if len(pipe.keys) != 2 || pipe.keys[0] == pipe.keys[1] {
		t.Fatalf("re-ask must re-fire with a distinct key, got %v", pipe.keys)
	}
}

func TestSDDispatchAnswerJob_LogsSubmitFailure(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	pipe := &recordingPipeline{err: fwra.New(fwra.Infrastructure, "boom")}
	m := sdManagerWith(pipe)
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindSystem, "", projectstate.ReviewAddresseeArchitect, sdQuestions())
	if out := buf.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "dispatch FAILED") {
		t.Fatalf("a submit failure must be logged at ERROR; log was:\n%s", out)
	}
}
