// Hooks for sourceControlAccess scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.
//
// The subject is the REAL local-profile sourceControlAccess, built the way
// cmd/server/main.gen.go builds it (sourcecontrol.NewGitLocalSourceControlAccess over
// the shared repo URL). Every binding names a "local|local/<project>" RepoRef, the
// dummy "local" credential and "local#<head>" PR refs — the local realisation's own
// encodings — so the GitHub realisation is not what these scenarios describe.
//
// One real bare repo PER SCENARIO (testinfra.StartLocalGitRepo, the same double the
// host boots for the process): the local rail is repo-scoped, not ref-scoped, and the
// scenarios reuse one activity branch name (activity/C-SCA) that some merge and delete
// and others expect to find open, while the local projectstate store holds exactly one
// project per repo. A scenario-private repo keeps every verdict independent of test
// order. newSubject records the scenario's repo so a probe on another component's
// contract (designSessionAccess) reads the same repository.
//
// Arrangement goes through public contracts only (DCT §12 B2): the subject's own
// CommitManagedFiles / OpenBranch, and projectStateAccess / designSessionAccess
// through their public local-profile constructors.
package sourcecontrol_test

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// scenarioRepos maps a scenario (its test name) to the URL of the bare repo its
// subject was built over.
var scenarioRepos sync.Map

// newSubject builds the component under test against a real bare git repo private to
// the scenario: the local-profile variant the composition root wires.
func newSubject(t *testing.T, h *scenariohost.Host) sourcecontrol.SourceControlAccess {
	t.Helper()
	if h.Repo.URL == "" {
		t.Fatal("hooks: scenariohost started no git repository")
	}
	repo := testinfra.StartLocalGitRepo(t, "main")
	scenarioRepos.Store(t.Name(), repo.URL)
	return sourcecontrol.NewGitLocalSourceControlAccess(repo.URL)
}

// scenarioRepo returns the URL of the repo newSubject built this scenario's subject over.
func scenarioRepo(t *testing.T) string {
	t.Helper()
	v, ok := scenarioRepos.Load(t.Name())
	if !ok {
		t.Fatalf("hooks: no subject repo recorded for %s", t.Name())
	}
	url, ok := v.(string)
	if !ok {
		t.Fatalf("hooks: subject repo for %s is %T, not a URL", t.Name(), v)
	}
	return url
}

// seatedManifest is the method-assets seat manifest a seated project carries on main
// ({"version":"v0.10.1"}, the bytes the ask-a-clarifying-question bindings expect).
const seatedManifest = `{"version":"v0.10.1"}`

// manifestPath is the managed seat manifest the answer job's dispatch reads.
const manifestPath = ".claude/.method-assets-manifest.json"

// readSeatedManifest arranges a project whose managed scaffold is seated — the seat
// manifest committed on main through the subject's own CommitManagedFiles, as project
// birth seats it — then performs the dispatch's ReadManagedFile.
func readSeatedManifest(t *testing.T, h *scenariohost.Host, subject sourcecontrol.SourceControlAccess, repo sourcecontrol.RepoRef, path string, cred sourcecontrol.RepoCredential) (any, error) {
	t.Helper()
	if path != manifestPath {
		t.Fatalf("hooks: binding reads %q, the arrangement seats %q", path, manifestPath)
	}
	files := []sourcecontrol.ManagedFile{{Path: manifestPath, Content: []byte(seatedManifest)}}
	if _, err := subject.CommitManagedFiles(callContext(t, h, "arrange-seat"), repo, files, cred); err != nil {
		t.Fatalf("hooks: seat %s: %v", manifestPath, err)
	}
	return subject.ReadManagedFile(callContext(t, h, "1"), repo, path, cred)
}

// stepAskAClarifyingQuestionDuringReviewP1S1 — Ask a Clarifying Question During Review — [yes: session branch] · [yes], step 1: ReadManagedFile
//
// answer-ran=[yes] is the dispatch reaching a seated project: the manifest is on main.
// artifact-state=[yes: session branch] is designSessionAccess's, not this component's
// (ReadManagedFile always reads main).
func stepAskAClarifyingQuestionDuringReviewP1S1(t *testing.T, h *scenariohost.Host, subject sourcecontrol.SourceControlAccess, in inputAskAClarifyingQuestionDuringReviewP1S1) (any, error) {
	t.Helper()
	return readSeatedManifest(t, h, subject, in.Repo, in.Path, in.Cred)
}

// stepAskAClarifyingQuestionDuringReviewP3S1 — Ask a Clarifying Question During Review — [no: committed] · [yes], step 1: ReadManagedFile
//
// As P1: the artifact-state guard ([no: committed]) is designSessionAccess's; the
// answer job's dispatch reads the seated manifest off main.
func stepAskAClarifyingQuestionDuringReviewP3S1(t *testing.T, h *scenariohost.Host, subject sourcecontrol.SourceControlAccess, in inputAskAClarifyingQuestionDuringReviewP3S1) (any, error) {
	t.Helper()
	return readSeatedManifest(t, h, subject, in.Repo, in.Path, in.Cred)
}

// activityBranch is the activity branch the review-path bindings' PR ("local#activity/C-SCA") proposes.
const activityBranch = "activity/C-SCA"

// arrangeReviewPR arranges what the review task finds when it posts the verdict: the
// project committed on main, its activity branch open off main, and the activity's
// artifact (the core use cases) staged for review on that branch — the draft the
// activity's dispatch task left. The PR is the branch proposed into main.
func arrangeReviewPR(t *testing.T, h *scenariohost.Host, subject sourcecontrol.SourceControlAccess, repo sourcecontrol.RepoRef, cred sourcecontrol.RepoCredential, projectID projectstate.ProjectID) {
	t.Helper()
	url := scenarioRepo(t)
	psa := projectstate.NewGitLocalProjectStateAccess(url)
	if _, err := psa.CreateProject(callContext(t, h, "arrange-create-project"), projectID, "stp-sca-owner", string(projectID)); err != nil {
		t.Fatalf("hooks: create project %s: %v", projectID, err)
	}
	if _, err := subject.OpenBranch(callContext(t, h, "arrange-open-branch"), repo, activityBranch, cred); err != nil {
		t.Fatalf("hooks: open %s: %v", activityBranch, err)
	}
	ds := projectstate.NewGitLocalDesignSessionAccess(url)
	env, err := ds.ReadProjectOnBranch(callContext(t, h, "arrange-read-branch"), projectID, activityBranch)
	if err != nil {
		t.Fatalf("hooks: read %s on %s: %v", projectID, activityBranch, err)
	}
	model := projectstate.ModelEnvelope{Kind: projectstate.KindCoreUseCases, Model: json.RawMessage(`{"decisions":[]}`)}
	rc := callContext(t, h, "arrange-stage")
	if _, err := ds.StageArtifactForReviewOnBranch(rc, projectID, env.Version, activityBranch, model, rc.IdempotencyKey); err != nil {
		t.Fatalf("hooks: stage core use cases on %s: %v", activityBranch, err)
	}
}

// readCommittedProject observes the commit through designSessionAccess over the
// scenario's repo: the project as it stands on main.
func readCommittedProject(t *testing.T, h *scenariohost.Host, projectID projectstate.ProjectID) (any, error) {
	t.Helper()
	ds := projectstate.NewGitLocalDesignSessionAccess(scenarioRepo(t))
	return ds.ReadProjectOnBranch(callContext(t, h, "probe-main"), projectID, "main")
}

// stepExecuteAProjectActivityP3S1 — Execute a Project Activity — review · agents only · approve, step 1: PostReview
//
// task-kind=review: the review PR is open over the staged artifact (arrangeReviewPR).
// requires-human=agents only and verdict=approve are deliveryManager's decisions; the
// verdict arrives here as the binding's ReviewSubmission.
func stepExecuteAProjectActivityP3S1(t *testing.T, h *scenariohost.Host, subject sourcecontrol.SourceControlAccess, in inputExecuteAProjectActivityP3S1) (any, error) {
	t.Helper()
	arrangeReviewPR(t, h, subject, in.Repo, in.Cred, "stp-sca-eapa-p3")
	return nil, subject.PostReview(callContext(t, h, "1"), in.Repo, in.Pr, in.Review, in.Cred)
}

// probeExecuteAProjectActivityP3S3N1 — Execute a Project Activity — review · agents only · approve, step 3, probe 1: designSessionAccess.ReadProjectOnBranch proves artifact-committed.
func probeExecuteAProjectActivityP3S3N1(t *testing.T, h *scenariohost.Host, _ sourcecontrol.SourceControlAccess) (any, error) {
	t.Helper()
	return readCommittedProject(t, h, "stp-sca-eapa-p3")
}

// stepExecuteAProjectActivityP4S1 — Execute a Project Activity — review · human required · approve, step 1: PostReview
//
// As P3; requires-human=human required means the approving verdict is the human's,
// relayed through the same PostReview.
func stepExecuteAProjectActivityP4S1(t *testing.T, h *scenariohost.Host, subject sourcecontrol.SourceControlAccess, in inputExecuteAProjectActivityP4S1) (any, error) {
	t.Helper()
	arrangeReviewPR(t, h, subject, in.Repo, in.Cred, "stp-sca-eapa-p4")
	return nil, subject.PostReview(callContext(t, h, "1"), in.Repo, in.Pr, in.Review, in.Cred)
}

// probeExecuteAProjectActivityP4S3N1 — Execute a Project Activity — review · human required · approve, step 3, probe 1: designSessionAccess.ReadProjectOnBranch proves artifact-committed.
func probeExecuteAProjectActivityP4S3N1(t *testing.T, h *scenariohost.Host, _ sourcecontrol.SourceControlAccess) (any, error) {
	t.Helper()
	return readCommittedProject(t, h, "stp-sca-eapa-p4")
}
