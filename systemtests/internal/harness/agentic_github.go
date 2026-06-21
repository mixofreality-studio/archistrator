package harness

// agentic_github.go is the systemtests-level FAKE of the external agentic-job +
// GitHub PR seam (FU-RA-ST-1 from I-RA; the design-rail slice of I-UC1-UC2-AG).
//
// THE SEAM IT FAKES. With the design rail wired (sourceControlAccess + the
// constructionPipelineAccess present), the REAL server boots into the AGENTIC
// design path (D-MSD-Δ / D-MPD-Δ): the systemDesignManager / projectDesignManager
// no longer draft synchronously through workerAccess — they DISPATCH a
// claude-code-action DESIGN job (workflow_dispatch), OBSERVE it to a terminal
// phase, READ BACK the typed draft the Action committed on the session branch,
// gate on the human, then guard→+1→merge the PR. Two transports back that flow:
//
//   - The PR-rail + Actions go through the GitHub REST API at
//     ARCHISTRATOR_GITHUB_API_BASE_URL (constructionPipelineAccess +
//     sourceControlAccess, both over the framework-go-infrastructure-github
//     satellite). This fake IS that REST API.
//   - The project-state read-back / stage / commit go through go-git over the
//     per-project repo URL. In the LOCAL project-state profile that is a file://
//     on-disk repo (StartLocalGitRepo) — the natural git-transport substrate. The
//     server is booted with ProjectStateGitLocal=true so the project-state writes
//     hit the on-disk repo, while the REST fake handles the rail + Actions.
//
// THE AGENTIC JOB. The load-bearing simulation is: on a workflow_dispatch the fake
// plays the Action — it COMMITS a DETERMINISTIC typed draft (a project.json with
// the requested artifact slot staged as a draft model) onto the session branch
// (inputs.target_branch) of the SAME on-disk file:// repo the server reads back
// from, reports a terminal PhaseSucceeded run, a green required-check, and an open
// PR. The server's branch-aware ReadProjectOnBranch (go-git clone of the file://
// repo on the session branch) then sees the committed draft — proving the
// dispatch→observe→read-back round-trip end to end with NO live GitHub and NO LLM.
//
// On a merge PUT the fake fast-forwards the session branch into main of the file://
// repo, so the server's post-merge ReadProject (on main) reflects the committed
// artifact — the approve→commit leg is HARD here (the draft is fixed, unlike the
// offline-cassette UC1 limitation).
//
// BLACK-BOX DISCIPLINE: this fake links ZERO server code. It speaks only the GitHub
// REST wire + drives the on-disk file:// repo with the plain `git` CLI (the same
// approach localgit.go uses). It knows the on-disk `.aiarch/state/project.json`
// shape only as PUBLISHED JSON (the kind-keyed slot envelope), never an imported
// type — exactly the boundary the test constitution mandates (real server, fake
// only the external agentic-job + GitHub seam).

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// AgenticGitHub is the in-process fake of the external agentic-job + GitHub PR
// seam. It fronts ONE on-disk file:// project repo (the server's LOCAL
// project-state substrate) and turns every workflow_dispatch into a committed
// draft on the dispatched session branch. Concurrency-safe: the embedded Temporal
// Worker may dispatch/observe from multiple goroutines.
type AgenticGitHub struct {
	t       *testing.T
	server  *httptest.Server
	repoDir string // the bare file:// repo directory on disk (the project repo)
	account string // the org login the App is "installed" on

	mu        sync.Mutex
	runs      []fakeAgenticRun
	nextRunID int64
	prs       map[string]int // session branch -> PR number
	nextPR    int
	merged    map[int]bool // PR number -> merged
	approvals map[int]int  // PR number -> approval count

	// checkGreen scripts the required-check rollup the merge guard reads. true =>
	// green (the happy path). Set false (ScriptRedCheck) to drive the
	// merge-blocked / StageDraftFailed path.
	checkGreen bool
	// failDispatches, when true, makes EVERY dispatched run terminate as a FAILED job
	// (observe → PhaseFailed → StageDraftFailed) WITHOUT committing a draft. Persistent
	// (not one-shot): requestArtifactDraft is a Temporal SignalWithStart that buffers a
	// redraft signal, so the recovery gate AUTO-REDRAFTS once after the first failure —
	// a one-shot fail would let the second (auto-redraft) draft succeed and the session
	// would escape StageDraftFailed. Persisting the failure keeps the session at the
	// human-visible draftFailed gate (the anti-wedge proof). Reset with FailDispatches(false).
	failDispatches bool

	// draftFor maps an artifact wire-kind to the deterministic draft model JSON the
	// agentic job commits for it. Seeded with sane defaults; a test may override.
	draftFor map[string]json.RawMessage

	// lastCommitErr / lastMergeErr record the last fake-side git fault so a test that
	// fails on an un-reached gate can report WHY (the fault is surfaced as a 500 to the
	// dispatch/merge, never a t.Fatalf from the handler goroutine).
	lastCommitErr string
	lastMergeErr  string

	// dispatchTargets records (owner, repo, workflowFile) for EVERY workflow_dispatch the
	// fake received — the load-bearing per-project-design-dispatch proof. The flagged
	// live-activation gap was that DESIGN dispatch hit the FIXED construction repo +
	// aiarch-construct.yml instead of the per-project repo + aiarch-design.yml; before
	// the fix the fake intercepted all GitHub REST regardless of repo, so it could not
	// catch this. Now the test ASSERTS each dispatch addressed the per-project repo +
	// aiarch-design.yml (DispatchTargets).
	dispatchTargets []DispatchTarget

	requests []string // METHOD PATH log, for diagnostics
}

// DispatchTarget is the (owner, repo, workflowFile) one workflow_dispatch addressed —
// recorded so a test can assert the per-project-design-dispatch retargeting.
type DispatchTarget struct {
	Owner        string
	Repo         string
	WorkflowFile string
}

// fakeAgenticRun is one simulated workflow run (the dispatched Action).
type fakeAgenticRun struct {
	ID         int64
	Name       string // "aiarch-cp-<idempotency_token>"
	Status     string // "completed"
	Conclusion string // "success" | "failure"
	HeadBranch string // the session branch the dispatch targeted
}

// kindOrdinal maps an artifact kind NAME to its projectstate ordinal (the slot map
// key in .aiarch/state/project.json). The ordinals are the published on-disk
// discriminator (projectstate identity.go), kept here as the fake's only piece of
// shared vocabulary — the same way the wire DTOs hand-mirror the published shapes.
//
// The DESIGN DISPATCH stamps the dispatch input `artifact_kind` as the kind's
// PascalCase String() form (ArtifactKind.String(), e.g. "Volatilities"), NOT the
// camelCase WireName the HTTP routes use. The fake therefore keys on BOTH forms so
// it resolves the dispatched kind regardless of which name reaches it (the wire-kind
// keys also let a test seed draftFor by the route's name).
var kindOrdinal = map[string]int{
	// camelCase wire names (HTTP routes / draftFor keys)
	"mission": 0, "glossary": 1, "scrubbedRequirements": 2, "volatilities": 3,
	"coreUseCases": 4, "system": 5, "operationalConcepts": 6, "standardCheck": 7,
	"planningAssumptions": 8, "activityList": 9, "network": 10, "normalSolution": 11,
	"subcriticalSolution": 12, "compressedSolution": 13, "decompressedSolution": 14,
	"riskModel": 15, "sdpReview": 16,
	// PascalCase String() names (the dispatch `artifact_kind` input)
	"Mission": 0, "Glossary": 1, "ScrubbedRequirements": 2, "Volatilities": 3,
	"CoreUseCases": 4, "System": 5, "OperationalConcepts": 6, "StandardCheck": 7,
	"PlanningAssumptions": 8, "ActivityList": 9, "Network": 10, "NormalSolution": 11,
	"SubcriticalSolution": 12, "CompressedSolution": 13, "DecompressedSolution": 14,
	"RiskModel": 15, "SdpReview": 16,
}

// wireKindByOrdinal recovers the camelCase wire name for an ordinal — used to look
// up the scripted draft model (draftFor is keyed by wire name) when the dispatch
// supplied the PascalCase String() form.
var wireKindByOrdinal = map[int]string{
	0: "mission", 1: "glossary", 2: "scrubbedRequirements", 3: "volatilities",
	4: "coreUseCases", 5: "system", 6: "operationalConcepts", 7: "standardCheck",
	8: "planningAssumptions", 9: "activityList", 10: "network", 11: "normalSolution",
	12: "subcriticalSolution", 13: "compressedSolution", 14: "decompressedSolution",
	15: "riskModel", 16: "sdpReview",
}

// reviewAwaitingReviewStatus is the published slot status ordinal for "staged,
// awaiting review" (projectstate ArtifactReviewStatus.ReviewAwaitingReview == 1).
// The agentic Action commits the draft as a populated slot; the server's
// StageArtifactForReview thin-write flips it again, but it must already be
// non-ReviewNone (== have a Model) so the read-back finds a model.
const reviewAwaitingReviewStatus = 1

// defaultDrafts seeds a minimal, decode-valid draft model per kind the E2E tests
// exercise. The read-back only requires slot.Model != nil and a model the
// published codec can decode; these are the smallest such documents. (A richer
// draft is unnecessary — the WIRING is what these tests prove; semantic validity
// is the live LLM's job, founder-gated.)
func defaultDrafts() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		// volatilities — architect-owned (no PM critique), the simplest UC1 kind.
		"volatilities": json.RawMessage(`{"items":[{"name":"Agentic draft volatility","rationale":"fake agentic job","axis":0}]}`),
		// glossary — a PM-critiqued kind, used to exercise the critique round-trip too.
		"glossary": json.RawMessage(`{"items":[{"term":"Agentic","definition":"the dispatched design job","category":"What"}]}`),
		// planningAssumptions — the first Phase-2 (UC2) kind, architect-owned.
		"planningAssumptions": json.RawMessage(`{"resources":["dev-a"],"calendarDaysPerWeek":5,"infrastructureKind":0,"declaredUsage":{},"terms":{},"notes":"fake agentic Phase-2 draft"}`),
	}
}

// StartAgenticGitHub spins up the fake fronting the given on-disk file:// repo
// (the server's LOCAL project-state repo) under the given account. The repo MUST
// be a bare repo with a seeded main (StartLocalGitRepo). git must be on PATH.
func StartAgenticGitHub(t *testing.T, repo LocalGitRepo, account string) *AgenticGitHub {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping agentic E2E proof")
	}
	f := &AgenticGitHub{
		t:          t,
		repoDir:    repo.bare,
		account:    account,
		nextRunID:  1,
		prs:        map[string]int{},
		nextPR:     1,
		merged:     map[int]bool{},
		approvals:  map[int]int{},
		checkGreen: true,
		draftFor:   defaultDrafts(),
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

// BaseURL is the fake's REST root — pass it as ARCHISTRATOR_GITHUB_API_BASE_URL.
func (f *AgenticGitHub) BaseURL() string { return f.server.URL }

// ScriptRedCheck makes the required-check rollup RED, so the merge guard blocks
// the merge (drives the StageDraftFailed-at-approve path). Reversible.
func (f *AgenticGitHub) ScriptRedCheck(green bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkGreen = green
}

// FailDispatches makes EVERY dispatched job terminate as a FAILED run (PhaseFailed)
// WITHOUT committing a draft — the anti-wedge path. Persistent (not one-shot)
// because requestArtifactDraft's SignalWithStart buffers a redraft signal that
// auto-redrafts once after the first failure; only failing every attempt keeps the
// session pinned at the human-visible draftFailed gate.
func (f *AgenticGitHub) FailDispatches(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failDispatches = fail
}

// DispatchCount returns how many workflow_dispatch calls the fake received — the
// assertion that the server took the AGENTIC path (>0) rather than a synchronous
// worker draft (0).
func (f *AgenticGitHub) DispatchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if strings.HasPrefix(r, "POST ") && strings.HasSuffix(r, "/dispatches") {
			n++
		}
	}
	return n
}

// DispatchTargets returns a copy of every (owner, repo, workflowFile) a
// workflow_dispatch addressed — the per-project-design-dispatch proof.
func (f *AgenticGitHub) DispatchTargets() []DispatchTarget {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]DispatchTarget, len(f.dispatchTargets))
	copy(out, f.dispatchTargets)
	return out
}

// AssertDispatchedToPerProjectRepo fails the test unless AT LEAST ONE workflow_dispatch
// was received AND every dispatch addressed the PER-PROJECT repo (owner=account,
// repo=projectID — name-as-identity) + the aiarch-design.yml workflow. This is the
// load-bearing assertion that catches the live-activation gap: before the
// per-project-design-dispatch fix the DESIGN dispatch hit the FIXED construction repo
// (repo="construction") + aiarch-construct.yml, so this assertion would fail.
func (f *AgenticGitHub) AssertDispatchedToPerProjectRepo(t *testing.T, projectID string) {
	t.Helper()
	targets := f.DispatchTargets()
	if len(targets) == 0 {
		t.Fatal("no workflow_dispatch recorded — the agentic design path did not dispatch")
	}
	for i, tgt := range targets {
		if tgt.Owner != f.account {
			t.Fatalf("dispatch %d owner = %q, want the account %q (per-project repo owner)", i, tgt.Owner, f.account)
		}
		if tgt.Repo != projectID {
			t.Fatalf("dispatch %d repo = %q, want the PER-PROJECT repo %q (name-as-identity) — NOT the central construction repo", i, tgt.Repo, projectID)
		}
		if tgt.WorkflowFile != "aiarch-design.yml" {
			t.Fatalf("dispatch %d workflow = %q, want aiarch-design.yml — NOT aiarch-construct.yml", i, tgt.WorkflowFile)
		}
	}
}

// MergeCount returns how many PRs the fake has merged (the approve→merge proof).
func (f *AgenticGitHub) MergeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.merged {
		if m {
			n++
		}
	}
	return n
}

// LastFault returns the last fake-side git fault (commit or merge), or "" — so a
// test that fails on an un-reached gate can report WHY (the fault was surfaced as a
// 500 to the dispatch/merge, never a t.Fatalf from the handler goroutine).
func (f *AgenticGitHub) LastFault() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastCommitErr != "" {
		return "commit: " + f.lastCommitErr
	}
	if f.lastMergeErr != "" {
		return "merge: " + f.lastMergeErr
	}
	return ""
}

// SetDraft overrides the deterministic draft model JSON committed for a wire-kind.
func (f *AgenticGitHub) SetDraft(wireKind string, modelJSON json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.draftFor[wireKind] = modelJSON
}

// Env returns the server env that wires the REAL server into the agentic design
// path against this fake: the LOCAL project-state git substrate (file:// repo) +
// the GitHub App identity/account/construction-repo pointed at the fake's REST
// root. With both present, buildDesignProjectState selects LOCAL (checked first)
// for the project-state writes while sourceControlAccess + constructionPipeline
// (the rail + dispatch/observe) hit the fake. appKeyPEM is a throwaway RSA key the
// satellite signs its App-JWT with (the fake does not verify the signature).
func (f *AgenticGitHub) Env(repo LocalGitRepo, appKeyPEM string) []string {
	env := GitLocalEnv(repo.URL())
	return append(env,
		"ARCHISTRATOR_GITHUB_API_BASE_URL="+f.BaseURL(),
		"ARCHISTRATOR_GITHUB_APP_ID=123456",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM="+appKeyPEM,
		"ARCHISTRATOR_GITHUB_ACCOUNT="+f.account,
		"ARCHISTRATOR_GITHUB_APP_SLUG=aiarch-app",
		// constructionPipelineAccess gate (owner+name) → the design dispatch RA is
		// constructed and the design Managers dispatch agentic jobs.
		"ARCHISTRATOR_CONSTRUCTION_REPO_OWNER="+f.account,
		"ARCHISTRATOR_CONSTRUCTION_REPO_NAME=construction",
		"ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE=aiarch-design.yml",
	)
}

// --- the REST handler -------------------------------------------------------

var (
	reInstallations = regexp.MustCompile(`^/app/installations$`)
	reAccessToken   = regexp.MustCompile(`^/app/installations/(\d+)/access_tokens$`)
	reRepoMeta      = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)$`)
	reTopics        = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/topics$`)
	reContents      = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/contents/(.+)$`)
	reGitRefs       = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/git/refs$`)
	reGitRef        = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/git/ref/(.+)$`)
	reProtection    = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/branches/([^/]+)/protection$`)
	reAgDispatch    = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/actions/workflows/([^/]+)/dispatches$`)
	reAgListRuns    = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/actions/workflows/([^/]+)/runs$`)
	reAgGetRun      = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/actions/runs/(\d+)$`)
	rePulls         = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/pulls$`)
	rePull          = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/pulls/(\d+)$`)
	rePullReviews   = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/pulls/(\d+)/reviews$`)
	rePullMerge     = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/pulls/(\d+)/merge$`)
	reCheckRuns     = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/commits/([^/]+)/check-runs$`)
)

func (f *AgenticGitHub) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.mu.Unlock()

	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && reInstallations.MatchString(p):
		writeJSONResp(w, 200, []map[string]any{{"id": 999, "account": map[string]any{"login": f.account}}})
	case r.Method == http.MethodPost && reAccessToken.MatchString(p):
		writeJSONResp(w, 201, map[string]any{
			"token":      "ghs-fake-installation-token",
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})

	// --- adopt + workflow-file seat (project birth) ---
	case r.Method == http.MethodGet && reRepoMeta.MatchString(p):
		m := reRepoMeta.FindStringSubmatch(p)
		writeJSONResp(w, 200, map[string]any{
			"full_name": m[1] + "/" + m[2], "default_branch": "main", "size": 1,
			"topics": []string{"aiarch-project"},
		})
	case r.Method == http.MethodPut && reTopics.MatchString(p):
		writeJSONResp(w, 200, map[string]any{"names": []string{"aiarch-project"}})
	case reContents.MatchString(p):
		f.handleContents(w, r.Method, body)
	case r.Method == http.MethodPut && reProtection.MatchString(p):
		writeJSONResp(w, 200, map[string]any{})

	// --- PR rail: branch refs ---
	case r.Method == http.MethodPost && reGitRefs.MatchString(p):
		// CreateBranch. The agentic-job commit (on dispatch) creates the real branch;
		// here just report idempotent success.
		writeJSONResp(w, 201, map[string]any{"ref": "refs/heads/created"})
	case r.Method == http.MethodGet && reGitRef.MatchString(p):
		m := reGitRef.FindStringSubmatch(p)
		writeJSONResp(w, 200, map[string]any{"ref": "refs/" + m[3], "object": map[string]any{"sha": "mainsha"}})

	// --- Actions: dispatch + observe ---
	case r.Method == http.MethodPost && reAgDispatch.MatchString(p):
		m := reAgDispatch.FindStringSubmatch(p)
		f.handleDispatch(w, body, DispatchTarget{Owner: m[1], Repo: m[2], WorkflowFile: m[3]})
	case r.Method == http.MethodGet && reAgListRuns.MatchString(p):
		f.handleListRuns(w)
	case r.Method == http.MethodGet && reAgGetRun.MatchString(p):
		f.handleGetRun(w, reAgGetRun.FindStringSubmatch(p)[3])

	// --- PR rail: pulls / status / reviews / merge ---
	case r.Method == http.MethodPost && rePulls.MatchString(p):
		f.handleOpenPR(w, body)
	case r.Method == http.MethodGet && rePulls.MatchString(p):
		f.handleFindPR(w, r.URL.RawQuery)
	case r.Method == http.MethodGet && rePull.MatchString(p):
		f.handleGetPR(w, rePull.FindStringSubmatch(p)[3])
	case r.Method == http.MethodGet && reCheckRuns.MatchString(p):
		f.handleCheckRuns(w)
	case r.Method == http.MethodGet && rePullReviews.MatchString(p):
		f.handleListReviews(w, rePullReviews.FindStringSubmatch(p)[3])
	case r.Method == http.MethodPost && rePullReviews.MatchString(p):
		f.handlePostReview(w, rePullReviews.FindStringSubmatch(p)[3])
	case r.Method == http.MethodPut && rePullMerge.MatchString(p):
		f.handleMerge(w, rePullMerge.FindStringSubmatch(p)[3])

	default:
		writeJSONResp(w, 404, map[string]any{"message": "agentic-fake: no route for " + r.Method + " " + p})
	}
}

func (f *AgenticGitHub) handleContents(w http.ResponseWriter, method string, body []byte) {
	// The workflow-file seat (PUT .github/workflows/aiarch-design.yml) + the adopt
	// emptiness probe (GET). Seating is over the Contents API onto the file:// repo's
	// MAIN; we accept the PUT as a no-op success (the workflow file's presence is not
	// load-bearing for the fake — the fake IS the Action). A GET 404s (no prior file).
	switch method {
	case http.MethodGet:
		writeJSONResp(w, 404, map[string]any{"message": "Not Found"})
	case http.MethodPut:
		writeJSONResp(w, 201, map[string]any{"commit": map[string]any{"sha": "seatsha"}})
	default:
		writeJSONResp(w, 405, map[string]any{"message": "method not allowed"})
	}
}

// handleDispatch plays the agentic Action: it reads the target_branch +
// artifact_kind inputs, COMMITS a deterministic draft onto that session branch of
// the on-disk file:// repo (unless FailNextDispatch was scripted), and records a
// terminal run named aiarch-cp-<idempotency_token>.
func (f *AgenticGitHub) handleDispatch(w http.ResponseWriter, body []byte, tgt DispatchTarget) {
	var payload struct {
		Ref    string            `json:"ref"`
		Inputs map[string]string `json:"inputs"`
	}
	_ = json.Unmarshal(body, &payload)
	token := payload.Inputs["idempotency_token"]
	branch := payload.Inputs["target_branch"]
	kind := payload.Inputs["artifact_kind"]
	jobMode := payload.Inputs["job_mode"]

	f.mu.Lock()
	// Record the (owner, repo, workflowFile) this dispatch addressed — the
	// per-project-design-dispatch proof (which repo + workflow the DESIGN job hit).
	f.dispatchTargets = append(f.dispatchTargets, tgt)
	fail := f.failDispatches
	id := f.nextRunID
	f.nextRunID++
	f.mu.Unlock()

	conclusion := "success"
	if fail {
		conclusion = "failure"
	} else {
		// Commit the deterministic draft (or critique verdict) onto the session
		// branch. On a commit fault, record it + 500 the dispatch (NOT t.Fatalf —
		// failing from this HTTP-handler goroutine would close the server mid-request
		// and surface as an EOF retry storm); the test then fails on the un-reached gate
		// with the recorded reason.
		if err := f.commitAgenticDraft(branch, kind, jobMode); err != nil {
			f.mu.Lock()
			f.lastCommitErr = err.Error()
			f.mu.Unlock()
			writeJSONResp(w, 500, map[string]any{"message": "agentic-fake commit failed: " + err.Error()})
			return
		}
	}

	f.mu.Lock()
	f.runs = append(f.runs, fakeAgenticRun{
		ID: id, Name: "aiarch-cp-" + token, Status: "completed",
		Conclusion: conclusion, HeadBranch: branch,
	})
	f.mu.Unlock()

	// GitHub returns 204 (no body) for workflow_dispatch.
	w.WriteHeader(http.StatusNoContent)
}

// commitAgenticDraft is the heart of the fake: it materializes the committed draft
// the server reads back. It clones the file:// repo, branches the session branch
// off main, reads the current .aiarch/state/project.json (the head-state
// CreateProject committed), sets the requested slot's draft model (or the critique
// verdict for a critique job), bumps the aggregate version, and pushes the session
// branch. Done with the plain git CLI over the on-disk repo (black-box: no server
// import).
func (f *AgenticGitHub) commitAgenticDraft(branch, wireKind, jobMode string) error {
	if branch == "" {
		return fmt.Errorf("empty target_branch")
	}
	work := f.t.TempDir()
	clone := filepath.Join(work, "clone")
	if err := git(work, "clone", "file://"+f.repoDir, clone); err != nil {
		return err
	}
	cfg(clone)
	// Branch off main (idempotent: if the session branch already exists upstream,
	// check it out; a within-attempt redraft reuses the branch).
	if err := git(clone, "fetch", "origin", branch); err == nil {
		_ = git(clone, "checkout", "-B", branch, "origin/"+branch)
	} else {
		_ = git(clone, "checkout", "-B", branch, "origin/main")
	}

	statePath := filepath.Join(clone, ".aiarch", "state", "project.json")
	raw, rerr := readFile(statePath)
	if rerr != nil {
		return fmt.Errorf("read project.json: %w", rerr)
	}
	updated, uerr := f.applyDraftToProjectJSON(raw, wireKind, jobMode)
	if uerr != nil {
		return uerr
	}
	if werr := writeFile(statePath, updated); werr != nil {
		return werr
	}
	if err := git(clone, "add", "-A"); err != nil {
		return err
	}
	if err := git(clone, "commit", "-m", "aiarch: agentic draft "+wireKind+" ("+jobMode+")"); err != nil {
		return err
	}
	return git(clone, "push", "origin", branch)
}

// applyDraftToProjectJSON mutates the published .aiarch/state/project.json: it sets
// the requested kind's slot to a populated draft (the typed model) for a draft job,
// or the critiqueVerdict carrier for a critique job. It works over the published
// JSON shape directly (the kind-keyed slot envelope) — the fake's only contact with
// the on-disk shape, mirroring how the harness DTOs hand-mirror published wire
// shapes. The version bump keeps the server's optimistic-concurrency read coherent.
func (f *AgenticGitHub) applyDraftToProjectJSON(raw []byte, wireKind, jobMode string) ([]byte, error) {
	ordinal, ok := kindOrdinal[wireKind]
	if !ok {
		return nil, fmt.Errorf("agentic fake: unknown artifact kind %q", wireKind)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("agentic fake: decode project.json: %w", err)
	}
	// IMPORTANT: do NOT bump the aggregate `version`. The agentic Action commits the
	// DRAFT MODEL into the slot out-of-band on the session branch; it is NOT a
	// projectStateAccess mutation. The server's read-back reads the draft, then the
	// FIRST versioned write is the server's own StageArtifactForReview, whose
	// optimistic-concurrency CAS expects the SAME version the server read from main
	// (the headVersion it carries into the stage). Bumping the version here would make
	// the session branch's version one ahead of the server's headVersion and the stage
	// CAS would fail with "stale version: have 2, expected 1". The Action mutates only
	// the slot; the version is the server's to advance.

	// slots map (kind-ordinal-keyed slotJSON envelope).
	slots := map[string]json.RawMessage{}
	if s, ok := doc["slots"]; ok {
		_ = json.Unmarshal(s, &slots)
	}
	key := strconv.Itoa(ordinal)

	if jobMode == "critique" {
		// PM-critique job: write the critiqueVerdict carrier onto the slot (approve —
		// ratify the draft unchanged, so the loop proceeds to the human gate).
		var slot map[string]json.RawMessage
		if existing, ok := slots[key]; ok {
			_ = json.Unmarshal(existing, &slot)
		}
		if slot == nil {
			slot = map[string]json.RawMessage{}
		}
		slot["critiqueVerdict"], _ = json.Marshal("approve")
		slots[key], _ = json.Marshal(slot)
	} else {
		// Draft job: stage the typed model in the slot. draftFor is keyed by the
		// camelCase wire name; the dispatch may supply the PascalCase String() form, so
		// resolve the wire name from the ordinal.
		lookup := wireKindByOrdinal[ordinal]
		model, ok := f.draftFor[lookup]
		if !ok {
			return nil, fmt.Errorf("agentic fake: no scripted draft for kind %q (ordinal %d)", wireKind, ordinal)
		}
		slot := map[string]json.RawMessage{
			"status": mustJSON(reviewAwaitingReviewStatus),
			"kind":   mustJSON(ordinal),
			"model":  model,
		}
		slots[key], _ = json.Marshal(slot)
	}
	doc["slots"], _ = json.Marshal(slots)
	return json.MarshalIndent(doc, "", "  ")
}

func (f *AgenticGitHub) handleListRuns(w http.ResponseWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	runs := make([]map[string]any, 0, len(f.runs))
	for _, r := range f.runs {
		runs = append(runs, map[string]any{
			"id": r.ID, "name": r.Name, "status": r.Status, "conclusion": r.Conclusion,
		})
	}
	writeJSONResp(w, 200, map[string]any{"total_count": len(runs), "workflow_runs": runs})
}

func (f *AgenticGitHub) handleGetRun(w http.ResponseWriter, idStr string) {
	id, _ := strconv.ParseInt(idStr, 10, 64)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.runs {
		if r.ID == id {
			writeJSONResp(w, 200, map[string]any{
				"id": r.ID, "name": r.Name, "status": r.Status, "conclusion": r.Conclusion,
			})
			return
		}
	}
	writeJSONResp(w, 404, map[string]any{"message": "no run"})
}

func (f *AgenticGitHub) handleOpenPR(w http.ResponseWriter, body []byte) {
	var payload struct {
		Head string `json:"head"`
	}
	_ = json.Unmarshal(body, &payload)
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.prs[payload.Head]
	if !ok {
		n = f.nextPR
		f.nextPR++
		f.prs[payload.Head] = n
	}
	// The head sha must be a SINGLE path segment (no slashes) — it is later addressed
	// as /commits/{sha}/check-runs by the merge guard. Session branch names DO contain
	// slashes (aiarch-design/<proj>/<kind>-draft), so derive a slash-free sha from the
	// PR number, not the branch.
	writeJSONResp(w, 201, map[string]any{"number": n, "state": "open", "head": map[string]any{"sha": prHeadSHA(n)}})
}

func (f *AgenticGitHub) handleFindPR(w http.ResponseWriter, rawQuery string) {
	// findOpenPR: head filter is head=owner:branch. Return the existing PR if known.
	head := ""
	for _, kv := range strings.Split(rawQuery, "&") {
		if strings.HasPrefix(kv, "head=") {
			head = strings.TrimPrefix(kv, "head=")
		}
	}
	if i := strings.Index(head, ":"); i >= 0 {
		head = head[i+1:]
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if n, ok := f.prs[head]; ok {
		writeJSONResp(w, 200, []map[string]any{{"number": n, "state": "open", "head": map[string]any{"sha": prHeadSHA(n)}}})
		return
	}
	writeJSONResp(w, 200, []map[string]any{})
}

func (f *AgenticGitHub) handleGetPR(w http.ResponseWriter, numStr string) {
	n, _ := strconv.Atoi(numStr)
	f.mu.Lock()
	defer f.mu.Unlock()
	mergeable := true
	writeJSONResp(w, 200, map[string]any{
		"number": n, "state": "open", "merged": f.merged[n], "mergeable": &mergeable,
		"head": map[string]any{"sha": prHeadSHA(n)},
	})
}

func (f *AgenticGitHub) handleCheckRuns(w http.ResponseWriter) {
	f.mu.Lock()
	green := f.checkGreen
	f.mu.Unlock()
	conclusion := "success"
	if !green {
		conclusion = "failure"
	}
	writeJSONResp(w, 200, map[string]any{
		"total_count": 1,
		"check_runs":  []map[string]any{{"status": "completed", "conclusion": conclusion}},
	})
}

func (f *AgenticGitHub) handleListReviews(w http.ResponseWriter, numStr string) {
	n, _ := strconv.Atoi(numStr)
	f.mu.Lock()
	count := f.approvals[n]
	f.mu.Unlock()
	out := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, map[string]any{"state": "APPROVED"})
	}
	writeJSONResp(w, 200, out)
}

func (f *AgenticGitHub) handlePostReview(w http.ResponseWriter, numStr string) {
	n, _ := strconv.Atoi(numStr)
	f.mu.Lock()
	f.approvals[n]++
	f.mu.Unlock()
	writeJSONResp(w, 200, map[string]any{"state": "APPROVED"})
}

// handleMerge fast-forwards the PR's session branch into main of the on-disk repo,
// so the server's post-merge ReadProject (on main) reflects the committed artifact.
func (f *AgenticGitHub) handleMerge(w http.ResponseWriter, numStr string) {
	n, _ := strconv.Atoi(numStr)
	f.mu.Lock()
	branch := f.branchForPR(n)
	already := f.merged[n]
	f.mu.Unlock()
	if already {
		writeJSONResp(w, 200, map[string]any{"sha": "mergesha", "merged": true})
		return
	}
	if branch == "" {
		writeJSONResp(w, 404, map[string]any{"message": "no branch for PR"})
		return
	}
	if err := f.mergeBranchIntoMain(branch); err != nil {
		f.mu.Lock()
		f.lastMergeErr = err.Error()
		f.mu.Unlock()
		writeJSONResp(w, 500, map[string]any{"message": "agentic-fake merge failed: " + err.Error()})
		return
	}
	f.mu.Lock()
	f.merged[n] = true
	f.mu.Unlock()
	writeJSONResp(w, 200, map[string]any{"sha": "mergesha-" + branch, "merged": true})
}

// branchForPR resolves the session branch for a PR number. Caller holds f.mu.
func (f *AgenticGitHub) branchForPR(n int) string {
	for branch, num := range f.prs {
		if num == n {
			return branch
		}
	}
	return ""
}

// mergeBranchIntoMain merges the session branch into main of the bare file:// repo
// (the App-mediated merge), via a throwaway working clone.
func (f *AgenticGitHub) mergeBranchIntoMain(branch string) error {
	work := f.t.TempDir()
	clone := filepath.Join(work, "merge")
	if err := git(work, "clone", "file://"+f.repoDir, clone); err != nil {
		return err
	}
	cfg(clone)
	if err := git(clone, "fetch", "origin", branch); err != nil {
		return err
	}
	if err := git(clone, "checkout", "main"); err != nil {
		return err
	}
	// A non-ff merge keeps a deterministic merge commit; -X theirs is unnecessary
	// (the session branch is strictly ahead of main for the slot it touched).
	if err := git(clone, "merge", "--no-ff", "-m", "aiarch: merge "+branch, "origin/"+branch); err != nil {
		return err
	}
	return git(clone, "push", "origin", "main")
}

// --- small git/file/json helpers (CLI-driven; no go-git, no server import) ---

func git(dir string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %v\n%s", args, err, out)
	}
	return nil
}

func cfg(clone string) {
	_ = git(clone, "config", "user.email", "agentic@aiarch.local")
	_ = git(clone, "config", "user.name", "agentic-fake")
}

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// prHeadSHA is the slash-free head sha for a PR number. It MUST be a single path
// segment: the merge guard addresses it as /commits/{sha}/check-runs, and session
// branch names contain slashes (aiarch-design/<proj>/<kind>-draft), so the sha is
// derived from the PR number, never the branch name.
func prHeadSHA(n int) string { return "headsha-pr" + strconv.Itoa(n) }

// GenerateAppKeyPEM returns a throwaway 2048-bit RSA private key in PKCS#1 PEM
// form — the GitHub App private key the server's satellite signs its App-JWT with.
// The fake never verifies the signature (it only needs a syntactically-valid key
// so the satellite's mintAppJWT succeeds), so any fresh key works.
func GenerateAppKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate app key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
}

func writeJSONResp(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"message":"marshal error"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
