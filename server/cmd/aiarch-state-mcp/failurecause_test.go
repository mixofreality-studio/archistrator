package main

// failurecause_test.go holds the venue half of typed failure causes (FPC plan 6, Task 6.4): the
// local executor runs a real task spec's gates over a fake `claude` on PATH, and the GitHub arm
// reads the two artifacts its workflow uploads. What the agentic-job RA reports of a failed run
// is its cause, whose fault it is, whether the model was reached at all, and which checks were red.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// The stream-json lines a fake claude prints: a turn the model never took (the CLI's own stand-in for
// a request it could not make) and a turn a real model took.
const (
	streamUnreached = `{"type":"assistant","message":{"id":"m1","model":"<synthetic>","content":[{"type":"text","text":"API Error: Connection error."}],"usage":{"input_tokens":0,"output_tokens":0}}}
{"type":"result","subtype":"success","is_error":true,"result":"API Error: Connection error."}`
	streamReached = `{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"text","text":"I changed the state."}],"usage":{"input_tokens":10,"output_tokens":12}}}
{"type":"result","subtype":"success","is_error":false,"result":"done","num_turns":1}`
)

// fakeClaude is one scripted agent run: whether it commits a change that turns the task's check red
// (it empties the project's service contracts, which `rule:project` refuses), what it prints on
// stdout and stderr, and how it exits.
type fakeClaude struct {
	breakState bool
	stdout     string
	stderr     string
	exit       int
	// refusal is the line the agent's aiarch-state session appends to the refusals sidecar the venue
	// names in AIARCH_REFUSALS_FILE when the write gate refuses a write ("" for no refusal).
	refusal string
}

// install puts the script on PATH as `claude`, and returns the repo's state it commits when it breaks.
func (f fakeClaude) install(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	var script strings.Builder
	script.WriteString("#!/bin/sh\ncat >/dev/null\n")
	if f.breakState {
		var top map[string]json.RawMessage
		if err := json.Unmarshal(freshState(t), &top); err != nil {
			t.Fatal(err)
		}
		delete(top, "serviceContracts")
		broken, err := json.Marshal(top)
		if err != nil {
			t.Fatal(err)
		}
		brokenPath := filepath.Join(dir, "broken.project.json")
		if err := os.WriteFile(brokenPath, broken, 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&script, "cp %q .aiarch/state/project.json\n", brokenPath)
		script.WriteString("git add .aiarch/state/project.json\n")
		script.WriteString("git -c user.name=agent -c user.email=agent@example.com commit -q -m 'agent: change the state'\n")
	}
	if f.refusal != "" {
		fmt.Fprintf(&script, "cat >>\"$AIARCH_REFUSALS_FILE\" <<'REFUSAL'\n%s\nREFUSAL\n", f.refusal)
	}
	fmt.Fprintf(&script, "cat <<'STREAM'\n%s\nSTREAM\n", f.stdout)
	if f.stderr != "" {
		fmt.Fprintf(&script, "echo %q >&2\n", f.stderr)
	}
	fmt.Fprintf(&script, "exit %d\n", f.exit)
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script.String()), 0o700); err != nil { //nolint:gosec // an executable test script
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(filepath.ListSeparator)+pathWithout(t, "claude"))
}

// runLocalSRS dispatches the srs task of the fixture's component to the local executor over the born
// repo, with the fake claude on PATH, and returns the terminal observation. The srs task's one check
// is `rule:project`, so a run is green until the agent breaks the state.
func runLocalSRS(t *testing.T, f fakeClaude) (agenticjob.PipelineObservation, string) {
	t.Helper()
	repo := bornRepo(t, freshState(t))
	spec := activitySpec(t, repo, freshActivity, "srs")
	f.install(t)
	access, err := agenticjob.NewLocalExecAgenticJobAccess(repo, "todomvc", stateBinary(t), projectstate.StateToolVersion(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rc := fwra.Context{Context: context.Background(), IdempotencyKey: fwra.IdempotencyKey("failure-cause-" + t.Name())}
	handle, err := access.SubmitAgenticJob(rc, agenticjob.PipelineSpec{
		ProjectID: "todomvc", ActivityID: freshActivity, Steps: []agenticjob.PipelineStep{{Name: "construct"}},
		DispatchInputs: map[string]string{
			"command": spec.Identity.Command, "component_id": freshComponent, "activity_id": freshActivity,
			"task_id": "srs", "spec_hash": spec.Hash, "revision": freshActivity + ":srs:1",
		},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return observeToTerminal(t, access, rc, handle), repo
}

// causeOf is the cause an observation carries, or nil.
func causeOf(obs agenticjob.PipelineObservation) agenticjob.FailureCause {
	if obs.Cause == nil {
		return nil
	}
	return *obs.Cause
}

// A run that never reached the model proves nothing about the work, so even with the checks red it is a
// host fault, and it leaves no progress fingerprint for the next attempt to be compared against
// (FIX-065: a network outage was counted as no progress, and ended an activity).
func TestAgenticJob_NoFingerprintWhenModelUnreached(t *testing.T) {
	obs, _ := runLocalSRS(t, fakeClaude{breakState: true, stdout: streamUnreached, exit: 1})
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("phase = %d (%s), want failed", obs.Phase, obs.Diagnostic)
	}
	if obs.ReachedModel {
		t.Error("a run whose only assistant turn was the synthetic stand-in reached the model")
	}
	if _, ok := causeOf(obs).(*agenticjob.ModelUnreachable); !ok {
		t.Errorf("cause = %T, want *ModelUnreachable", causeOf(obs))
	}
	if obs.FaultClass != agenticjob.FaultHost {
		t.Errorf("fault class = %q, want host", obs.FaultClass)
	}
	if obs.ProgressFingerprint != nil {
		t.Errorf("a run that never reached the model left the progress fingerprint %q", *obs.ProgressFingerprint)
	}
}

func TestAgenticJob_RateLimitedIsHost(t *testing.T) {
	obs, _ := runLocalSRS(t, fakeClaude{stdout: streamUnreached, stderr: "429 rate_limit_error: this request would exceed your rate limit", exit: 1})
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("phase = %d (%s), want failed", obs.Phase, obs.Diagnostic)
	}
	if _, ok := causeOf(obs).(*agenticjob.RateLimited); !ok {
		t.Errorf("cause = %T, want *RateLimited", causeOf(obs))
	}
	if obs.FaultClass != agenticjob.FaultHost {
		t.Errorf("fault class = %q, want host", obs.FaultClass)
	}
}

// A run that reached the model and whose checks went red is the gate's: its cause names exactly the
// checks the runner recorded as not passing, and, having reached the model, it leaves a fingerprint.
func TestAgenticJob_RedChecksAreGateRed(t *testing.T) {
	obs, repo := runLocalSRS(t, fakeClaude{breakState: true, stdout: streamReached, exit: 0})
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("phase = %d (%s), want failed", obs.Phase, obs.Diagnostic)
	}
	if !obs.ReachedModel {
		t.Error("a run with a real assistant turn did not reach the model")
	}
	run, ok := checkRunOnBranch(t, repo, "activity/"+freshActivity, freshActivity, "srs")
	if !ok {
		t.Fatal("the branch holds no CheckRun for the srs task")
	}
	var want []string
	for _, r := range run.Results {
		if r.Status != "pass" {
			want = append(want, r.CheckId)
		}
	}
	if len(want) == 0 {
		t.Fatal("the recorded run has no red check: the fake agent did not break the state")
	}
	red, ok := causeOf(obs).(*agenticjob.GateRed)
	if !ok {
		t.Fatalf("cause = %T, want *GateRed", causeOf(obs))
	}
	if !slices.Equal(red.Checks, want) || !slices.Equal(obs.GateFailures, want) {
		t.Errorf("gateRed checks %v, gateFailures %v, want the recorded run's %v", red.Checks, obs.GateFailures, want)
	}
	if obs.FaultClass != agenticjob.FaultGate {
		t.Errorf("fault class = %q, want gate", obs.FaultClass)
	}
	if obs.ProgressFingerprint == nil || *obs.ProgressFingerprint == "" {
		t.Error("a red run that reached the model left no progress fingerprint")
	}
}

// A run whose state write the Method write gate refused, and that then committed nothing, is a
// refused write and no longer an agent that gave up: its cause names the refusal's findings, and the
// diagnostic names the rule ids and the first message (ISS-028-2: RUN-028's ten N-ATP attempts each ended
// agentGaveUp, the eight UC-OUTCOME-AT-ASKING-SURFACE refusals visible only in the agent's own prose).
func TestAgenticJob_RefusedWriteIsWriteRefused(t *testing.T) {
	refusal := `{"verb":"phase artifact","findings":[` +
		`{"ruleId":"UC-OUTCOME-AT-ASKING-SURFACE","severity":"error","message":"UC-3 has no outcome at its asking surface","ownerSlot":"coreUseCases"},` +
		`{"ruleId":"UC-ACTOR-PRESENT","severity":"error","message":"UC-4 names no actor","ownerSlot":"coreUseCases"}]}`
	obs, _ := runLocalSRS(t, fakeClaude{stdout: streamReached, exit: 0, refusal: refusal})
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("phase = %d (%s), want failed", obs.Phase, obs.Diagnostic)
	}
	wr, ok := causeOf(obs).(*agenticjob.WriteRefused)
	if !ok {
		t.Fatalf("cause = %T, want *WriteRefused (diagnostic %q)", causeOf(obs), obs.Diagnostic)
	}
	if wr.Verb != "phase artifact" || len(wr.Findings) != 2 {
		t.Fatalf("cause = %+v, want verb %q with the two findings", wr, "phase artifact")
	}
	if f := wr.Findings[0]; f.RuleID != "UC-OUTCOME-AT-ASKING-SURFACE" || f.OwnerSlot != "coreUseCases" ||
		f.Message != "UC-3 has no outcome at its asking surface" || f.Severity != "error" {
		t.Errorf("first finding = %+v", f)
	}
	for _, want := range []string{"UC-OUTCOME-AT-ASKING-SURFACE", "UC-ACTOR-PRESENT", "UC-3 has no outcome at its asking surface"} {
		if !strings.Contains(obs.Diagnostic, want) {
			t.Errorf("the diagnostic %q does not name %q", obs.Diagnostic, want)
		}
	}
	if obs.FaultClass != agenticjob.FaultAgent {
		t.Errorf("fault class = %q, want agent", obs.FaultClass)
	}
}

// A run that reached the model, committed nothing and met no refusal is still an agent that gave up.
func TestAgenticJob_NoRefusalStaysAgentGaveUp(t *testing.T) {
	obs, _ := runLocalSRS(t, fakeClaude{stdout: streamReached, exit: 0})
	if _, ok := causeOf(obs).(*agenticjob.AgentGaveUp); !ok {
		t.Fatalf("cause = %T, want *AgentGaveUp (diagnostic %q)", causeOf(obs), obs.Diagnostic)
	}
}

// The local executor holds its runs in memory only: a server that restarted no longer has the run, which
// cannot be recovered. That is the host's loss, not the work's.
func TestAgenticJob_RunnerLostAfterRestart(t *testing.T) {
	repo := bornRepo(t, freshState(t))
	t.Setenv("PATH", pathWithout(t, "claude"))
	access, err := agenticjob.NewLocalExecAgenticJobAccess(repo, "todomvc", stateBinary(t), projectstate.StateToolVersion(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	obs, err := access.ObserveAgenticJob(fwra.Context{Context: context.Background()}, agenticjob.PipelineHandle("local:a-run-another-process-started"))
	if err != nil {
		t.Fatal(err)
	}
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("phase = %d, want failed", obs.Phase)
	}
	if _, ok := causeOf(obs).(*agenticjob.RunnerLost); !ok {
		t.Errorf("cause = %T, want *RunnerLost", causeOf(obs))
	}
	if obs.FaultClass != agenticjob.FaultHost {
		t.Errorf("fault class = %q, want host", obs.FaultClass)
	}
}

// ghActionsServer is the slice of GitHub the cloud arm reads of a finished run: the installation token, the
// run itself (completed, failed) and the artifacts its workflow uploaded, each a zip.
func ghActionsServer(t *testing.T, artifacts map[string][]byte) *fwgithub.AppClient {
	t.Helper()
	ids := map[string]int64{}
	var order []string
	for name := range artifacts {
		order = append(order, name)
	}
	slices.Sort(order)
	for i, name := range order {
		ids[name] = int64(100 + i)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /app/installations/99/access_tokens", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token":"ghs_fake","expires_at":"2999-01-01T00:00:00Z"}`))
	})
	mux.HandleFunc("GET /repos/acme/app/actions/runs/7", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":7,"name":"aiarch-cp-x","status":"completed","conclusion":"failure"}`))
	})
	mux.HandleFunc("GET /repos/acme/app/actions/runs/7/artifacts", func(w http.ResponseWriter, _ *http.Request) {
		var list []map[string]any
		for _, name := range order {
			list = append(list, map[string]any{"id": ids[name], "name": name, "size_in_bytes": len(artifacts[name]), "expired": false, "created_at": "2026-11-16T00:00:00Z"})
		}
		out, _ := json.Marshal(map[string]any{"total_count": len(list), "artifacts": list})
		_, _ = w.Write(out)
	})
	for _, name := range order {
		body := artifacts[name]
		mux.HandleFunc(fmt.Sprintf("GET /repos/acme/app/actions/artifacts/%d/zip", ids[name]), func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	key, err := gh.GenerateAppKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	app, err := fwgithub.NewAppClient("12345", key, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// zipOf is a zip holding one file.
func zipOf(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func observeGitHubRun(t *testing.T, artifacts map[string][]byte) agenticjob.PipelineObservation {
	t.Helper()
	app := ghActionsServer(t, artifacts)
	access, err := agenticjob.NewGitHubActionsAgenticJobAccess(app, "acme", "app", "aiarch-construct.yml", "main", 99)
	if err != nil {
		t.Fatal(err)
	}
	obs, err := access.ObserveAgenticJob(fwra.Context{Context: context.Background()}, agenticjob.PipelineHandle("run/7"))
	if err != nil {
		t.Fatal(err)
	}
	return obs
}

// A cloud run that uploaded no execution log tells nobody whether the agent reached the model, and the
// honest answer is that the cause is unknown: not a host fault (the run would be retried for ever) and
// not the work's (the attempt would be spent).
func TestAgenticJob_GitHubMissingExecutionLogIsUnknown(t *testing.T) {
	obs := observeGitHubRun(t, nil)
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("phase = %d, want failed", obs.Phase)
	}
	if _, ok := causeOf(obs).(*agenticjob.UnknownCause); !ok {
		t.Errorf("cause = %T, want *UnknownCause", causeOf(obs))
	}
	if obs.ReachedModel || obs.FaultClass != agenticjob.FaultUnknown {
		t.Errorf("reachedModel %v, fault class %q, want false and unknown", obs.ReachedModel, obs.FaultClass)
	}
}

// With the execution log, the cloud arm reads what the local one does: the transcript's assistant turns
// say whether the model was reached (claude-code-action writes a JSON array of events), and the check
// results name what was red.
func TestAgenticJob_GitHubReadsTheTwoArtifacts(t *testing.T) {
	unreached := `[{"type":"assistant","message":{"id":"m1","model":"<synthetic>","content":[],"usage":{"output_tokens":0}}},{"type":"result","subtype":"success","is_error":true}]`
	reached := `[{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","content":[],"usage":{"output_tokens":9}}},{"type":"result","subtype":"success","is_error":false}]`
	results := `[{"checkId":"command:build","status":"pass"},{"checkId":"scenario:listManager:x","status":"fail"}]`

	obs := observeGitHubRun(t, map[string][]byte{"execution-log-C-x-1-1": zipOf(t, "execution.json", unreached)})
	if _, ok := causeOf(obs).(*agenticjob.ModelUnreachable); !ok || obs.ReachedModel || obs.FaultClass != agenticjob.FaultHost {
		t.Errorf("an unreached transcript: cause %T, reached %v, class %q, want ModelUnreachable, false, host", causeOf(obs), obs.ReachedModel, obs.FaultClass)
	}

	obs = observeGitHubRun(t, map[string][]byte{
		"execution-log-C-x-1-1": zipOf(t, "execution.json", reached),
		"check-results-C-x-1-1": zipOf(t, "check-results/results.json", results),
	})
	red, ok := causeOf(obs).(*agenticjob.GateRed)
	if !ok || !obs.ReachedModel || obs.FaultClass != agenticjob.FaultGate {
		t.Fatalf("a reached transcript with a red check: cause %T, reached %v, class %q, want GateRed, true, gate", causeOf(obs), obs.ReachedModel, obs.FaultClass)
	}
	if want := []string{"scenario:listManager:x"}; !slices.Equal(red.Checks, want) || !slices.Equal(obs.GateFailures, want) {
		t.Errorf("gateRed checks %v, gateFailures %v, want %v", red.Checks, obs.GateFailures, want)
	}
}

// The observation crosses Temporal as JSON: its cause must survive the trip as the same variant, and a
// kind nobody declared must be refused rather than decoded into nothing.
func TestAgenticJob_ObservationCauseSurvivesTheWire(t *testing.T) {
	var cause agenticjob.FailureCause = &agenticjob.GateRed{Checks: []string{"command:build"}}
	in := agenticjob.PipelineObservation{Handle: "local:x", Phase: agenticjob.PhaseFailed, ReachedModel: true, Cause: &cause, FaultClass: agenticjob.FaultGate, GateFailures: []string{"command:build"}}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out agenticjob.PipelineObservation
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if got, ok := causeOf(out).(*agenticjob.GateRed); !ok || !slices.Equal(got.Checks, []string{"command:build"}) || !out.ReachedModel || out.FaultClass != agenticjob.FaultGate {
		t.Errorf("round trip lost the cause: %+v (%s)", out, raw)
	}
	if err := json.Unmarshal([]byte(`{"Handle":"h","Cause":{"kind":"cosmicRay","model":{}}}`), &out); err == nil {
		t.Error("a cause of an undeclared kind was accepted")
	}
	var none agenticjob.PipelineObservation
	raw, err = json.Marshal(agenticjob.PipelineObservation{Handle: "h", Phase: agenticjob.PhaseSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &none); err != nil || none.Cause != nil || strings.Contains(string(raw), `"Cause"`) {
		t.Errorf("an observation with no cause wrote %s (decoded %+v, %v)", raw, none, err)
	}
}
