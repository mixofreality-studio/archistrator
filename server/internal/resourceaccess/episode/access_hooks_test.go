// Hooks for episodeAccess scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.
//
// The subject is the REAL episodeAccess, built the way cmd/server/main.gen.go
// builds it on the local profile (episode.NewLocalFSEpisodeAccess over the
// shared repo URL), here over the host's own git repository (h.Repo.URL). Its
// resource is that repo's filesystem: the episode ledger and the per-episode raw
// trace files under <repo>/.aiarch/traces. Every scenario names its own project
// and episode ids, so one ledger per test process keeps them apart.
//
// Arrangement goes through public contracts wherever a contract can write the
// state: episodes through AppendEpisode, and the caller's other calls at a node
// through the real collaborators' public constructors (DCT §12 B2). The one
// exception is a raw trace file. In production agenticJobAccess writes
// <episodeId>.jsonl beside the ledger and episodeAccess only reads it, so a hook
// lays it on the resource directly, exactly as that writer leaves it.

package episode_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// newSubject builds the component under test against the host's real downstream
// doubles: the local-profile filesystem store over the host's git repository.
func newSubject(t *testing.T, h *scenariohost.Host) episode.EpisodeAccess {
	t.Helper()
	if h.Repo.URL == "" {
		t.Fatal("hooks: scenariohost started no git repository")
	}
	return episode.NewLocalFSEpisodeAccess(h.Repo.URL)
}

// tracesDir is where the local store keeps its ledger and the raw trace files
// agenticJobAccess writes: <repo>/.aiarch/traces.
func tracesDir(h *scenariohost.Host) string {
	return filepath.Join(h.Repo.Dir, ".aiarch", "traces")
}

// writeTrace arranges one episode's raw trace file as agenticJobAccess writes it:
// one JSON event per line.
func writeTrace(t *testing.T, h *scenariohost.Host, episodeID string, events ...string) {
	t.Helper()
	dir := tracesDir(h)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("hooks: mkdir traces: %v", err)
	}
	var body []byte
	for _, e := range events {
		body = append(body, e...)
		body = append(body, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, episodeID+".jsonl"), body, 0o600); err != nil {
		t.Fatalf("hooks: write trace %s: %v", episodeID, err)
	}
}

func ptr[T any](v T) *T { return &v }

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("hooks: time %q: %v", s, err)
	}
	return v
}

// projectRepos maps a scenario's project id to the project repo its amendment
// session was opened in. The local projectstate store holds exactly one project
// per repo (its identity guard refuses a second id), and the replan scenarios
// name two projects, so each gets its own real bare repo (repo-per-project, the
// cloud shape). The step hook that opens the session records it here; the probe
// that observes the session reads it back.
var projectRepos sync.Map

// openAmendmentSession plays deliveryManager's calls at the reenter node that
// precede its appendEpisode (replan-under-scope-change dynamic view, calls 4-5),
// on real collaborators: the project exists on main (projectStateAccess
// CreateProject, the amendment basis), then sourceControlAccess mints the
// credential and opens the Phase-2 amendment session branch off main. The
// branch is the one the scenario's probe reads back through designSessionAccess.
func openAmendmentSession(t *testing.T, h *scenariohost.Host, projectID episode.ProjectID, branch string) {
	t.Helper()
	repo := testinfra.StartLocalGitRepo(t, "main")
	projectRepos.Store(projectID, repo.URL)

	psa := projectstate.NewGitLocalProjectStateAccess(repo.URL)
	if _, err := psa.CreateProject(callContext(t, h, "arrange-create-project"), projectstate.ProjectID(projectID), "dct-ep-owner", string(projectID)); err != nil {
		t.Fatalf("hooks: create project %s: %v", projectID, err)
	}

	sca := sourcecontrol.NewGitLocalSourceControlAccess(repo.URL)
	ref := sourcecontrol.GitLocalRepoRefForProject(sourcecontrol.ProjectID(projectID))
	cred, err := sca.GetInstallationToken(callContext(t, h, "arrange-installation-token"), ref)
	if err != nil {
		t.Fatalf("hooks: installation token for %s: %v", projectID, err)
	}
	if _, err := sca.OpenBranch(callContext(t, h, "arrange-open-branch"), ref, sourcecontrol.BranchName(branch), cred); err != nil {
		t.Fatalf("hooks: open amendment branch %s: %v", branch, err)
	}
}

// readAmendmentSession observes the amendment session through the real
// designSessionAccess over the project's repo: the project as it stands on the
// session branch.
func readAmendmentSession(t *testing.T, h *scenariohost.Host, projectID episode.ProjectID, branch string) (any, error) {
	t.Helper()
	url, ok := projectRepos.Load(projectID)
	if !ok {
		t.Fatalf("hooks: no amendment session was opened for %s", projectID)
	}
	repoURL, ok := url.(string)
	if !ok {
		t.Fatalf("hooks: project repo for %s is %T, not a URL", projectID, url)
	}
	ds := projectstate.NewGitLocalDesignSessionAccess(repoURL)
	return ds.ReadProjectOnBranch(callContext(t, h, "probe-amendment-session"), projectstate.ProjectID(projectID), branch)
}

// stepReplanUnderScopeChangeP2S1 — Replan Under Scope Change — [no: scope change] · [accept], step 1: AppendEpisode
// Projected stimulus (JSON): {"seq":1,"node":"reenter","input":{"kind":"call","from":"delivery-manager","to":"episodeaccess","op":"AppendEpisode","call":"replan-under-scope-change/reenter/8"},"expectedOutputs":[{"node":"reenter","kind":"inferred","to":"","label":"Re-enter project design at the affected step (assumptions, activities, or network)"}],"expectedStates":[{"node":"amendment-session-open","object":"Phase-2 amendment session","inState":"[open at affected step]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"AppendEpisode","inputs":[{"name":"projectID","value":"dct-ep-replan-P2"},{"name":"record","value":"{\"EpisodeID\":\"ep-replan-under-scope-change-P2\",\"Kind\":0,\"TargetRef\":\"projectDesign/network\",\"Usage\":{\"In\":41200,\"Out\":3900,\"CacheRead\":512000,\"CacheCreate\":18000},\"StartedAt\":\"2026-10-02T09:00:00Z\",\"EndedAt\":\"2026-10-02T09:12:00Z\",\"Outcome\":0,\"Lineage\":{\"WorkflowID\":\"dct-ep-replan-P2:activity:projectDesign\",\"RunID\":\"run-replan-under-scope-change-P2\"},\"WorkerClass\":\"project-designer\"}"}],"expect":{"errorExpected":false},"probes":[{"for":"reenter","component":"episodeAccess","operation":"ListEpisodes","inputs":[{"name":"query","value":"{\"ProjectID\":\"dct-ep-replan-P2\",\"TargetRef\":\"projectDesign/network\"}"}],"expect":{"result":"[{\"EpisodeID\":\"ep-replan-under-scope-change-P2\",\"Kind\":0,\"TargetRef\":\"projectDesign/network\",\"Outcome\":0}]","errorExpected":false}},{"for":"amendment-session-open","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"dct-ep-replan-P2"},{"name":"branch","value":"aiarch/amend/dct-ep-replan-P2/network"}],"expect":{"result":"{\"id\":\"dct-ep-replan-P2\"}","errorExpected":false}}],"unobservable":[],"hook":true}
//
// The guards classify=[no: scope change] and mgmt=[accept] are deliveryManager's
// decisions; episodeAccess holds no state that steers them. The node's preceding
// calls open the amendment session at the affected step (network); the
// re-entered design episode is then appended as deliveryManager appends it.
func stepReplanUnderScopeChangeP2S1(t *testing.T, h *scenariohost.Host, subject episode.EpisodeAccess, in inputReplanUnderScopeChangeP2S1) (any, error) {
	t.Helper()
	openAmendmentSession(t, h, in.ProjectID, "aiarch/amend/dct-ep-replan-P2/network")
	return nil, subject.AppendEpisode(callContext(t, h, "1"), in.ProjectID, in.Record)
}

// probeReplanUnderScopeChangeP2S1N2 — Replan Under Scope Change — [no: scope change] · [accept], step 1, probe 2: designSessionAccess.ReadProjectOnBranch proves amendment-session-open.
// Probe (JSON): {"for":"amendment-session-open","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"dct-ep-replan-P2"},{"name":"branch","value":"aiarch/amend/dct-ep-replan-P2/network"}],"expect":{"result":"{\"id\":\"dct-ep-replan-P2\"}","errorExpected":false}}
func probeReplanUnderScopeChangeP2S1N2(t *testing.T, h *scenariohost.Host, _ episode.EpisodeAccess) (any, error) {
	t.Helper()
	return readAmendmentSession(t, h, "dct-ep-replan-P2", "aiarch/amend/dct-ep-replan-P2/network")
}

// stepReplanUnderScopeChangeP3S1 — Replan Under Scope Change — [no: scope change] · [reject], step 1: AppendEpisode
// Projected stimulus (JSON): {"seq":1,"node":"reenter","input":{"kind":"call","from":"delivery-manager","to":"episodeaccess","op":"AppendEpisode","call":"replan-under-scope-change/reenter/8"},"expectedOutputs":[{"node":"reenter","kind":"inferred","to":"","label":"Re-enter project design at the affected step (assumptions, activities, or network)"}],"expectedStates":[{"node":"amendment-session-open","object":"Phase-2 amendment session","inState":"[open at affected step]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"AppendEpisode","inputs":[{"name":"projectID","value":"dct-ep-replan-P3"},{"name":"record","value":"{\"EpisodeID\":\"ep-replan-under-scope-change-P3\",\"Kind\":0,\"TargetRef\":\"projectDesign/activityList\",\"Usage\":{\"In\":41200,\"Out\":3900,\"CacheRead\":512000,\"CacheCreate\":18000},\"StartedAt\":\"2026-10-02T09:00:00Z\",\"EndedAt\":\"2026-10-02T09:12:00Z\",\"Outcome\":0,\"Lineage\":{\"WorkflowID\":\"dct-ep-replan-P3:activity:projectDesign\",\"RunID\":\"run-replan-under-scope-change-P3\"},\"WorkerClass\":\"project-designer\"}"}],"expect":{"errorExpected":false},"probes":[{"for":"reenter","component":"episodeAccess","operation":"ListEpisodes","inputs":[{"name":"query","value":"{\"ProjectID\":\"dct-ep-replan-P3\",\"TargetRef\":\"projectDesign/activityList\"}"}],"expect":{"result":"[{\"EpisodeID\":\"ep-replan-under-scope-change-P3\",\"Kind\":0,\"TargetRef\":\"projectDesign/activityList\",\"Outcome\":0}]","errorExpected":false}},{"for":"amendment-session-open","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"dct-ep-replan-P3"},{"name":"branch","value":"aiarch/amend/dct-ep-replan-P3/activityList"}],"expect":{"result":"{\"id\":\"dct-ep-replan-P3\"}","errorExpected":false}}],"unobservable":[],"hook":true}
//
// As P2, with the session opened at the activity-list step: the guards
// classify=[no: scope change] and mgmt=[reject] are deliveryManager's.
func stepReplanUnderScopeChangeP3S1(t *testing.T, h *scenariohost.Host, subject episode.EpisodeAccess, in inputReplanUnderScopeChangeP3S1) (any, error) {
	t.Helper()
	openAmendmentSession(t, h, in.ProjectID, "aiarch/amend/dct-ep-replan-P3/activityList")
	return nil, subject.AppendEpisode(callContext(t, h, "1"), in.ProjectID, in.Record)
}

// probeReplanUnderScopeChangeP3S1N2 — Replan Under Scope Change — [no: scope change] · [reject], step 1, probe 2: designSessionAccess.ReadProjectOnBranch proves amendment-session-open.
// Probe (JSON): {"for":"amendment-session-open","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"dct-ep-replan-P3"},{"name":"branch","value":"aiarch/amend/dct-ep-replan-P3/activityList"}],"expect":{"result":"{\"id\":\"dct-ep-replan-P3\"}","errorExpected":false}}
func probeReplanUnderScopeChangeP3S1N2(t *testing.T, h *scenariohost.Host, _ episode.EpisodeAccess) (any, error) {
	t.Helper()
	return readAmendmentSession(t, h, "dct-ep-replan-P3", "aiarch/amend/dct-ep-replan-P3/activityList")
}

// stepRequeueAFailedConstructionActivityP1S1 — Re-queue a Failed Construction Activity, step 1: ListEpisodes
// Projected stimulus (JSON): {"seq":1,"node":"read-attempts","input":{"kind":"call","from":"delivery-manager","to":"episodeaccess","op":"ListEpisodes","call":"requeue-a-failed-construction-activity/read-attempts/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ListEpisodes","inputs":[{"name":"query","value":"{\"ProjectID\":\"dct-ep-requeue-P1\",\"TargetRef\":\"C-dct-ep-foxtrot\"}"}],"expect":{"result":"[{\"EpisodeID\":\"ep-rq-a1\",\"Kind\":1,\"TargetRef\":\"C-dct-ep-foxtrot\",\"Outcome\":1,\"NumTurns\":18,\"CostUSD\":1.2,\"TracePath\":\"episodes/traces/ep-rq-a1.jsonl\"},{\"EpisodeID\":\"ep-rq-a2\",\"Kind\":1,\"TargetRef\":\"C-dct-ep-foxtrot\",\"Outcome\":3,\"GapReason\":\"trace stream lost: runner terminated before result event\"}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
//
// Arrangement: the failed activity's two prior construction attempts (a1 ran out
// of turns, Failed with its trace captured; a2 lost its trace stream, Gap),
// appended through the contract in the order they ran, plus a1's raw trace file
// as agenticJobAccess left it, which step 2 (ReadTraceEvents) reads back.
func stepRequeueAFailedConstructionActivityP1S1(t *testing.T, h *scenariohost.Host, subject episode.EpisodeAccess, in inputRequeueAFailedConstructionActivityP1S1) (any, error) {
	t.Helper()
	const project episode.ProjectID = "dct-ep-requeue-P1"
	const activity = "C-dct-ep-foxtrot"
	usage := episode.EpisodeUsage{In: 18400, Out: 2100, CacheRead: 240000, CacheCreate: 9000}
	attempts := []episode.EpisodeRecord{
		{
			EpisodeID:   "ep-rq-a1",
			Kind:        episode.EpisodeKindConstruction,
			TargetRef:   activity,
			Lineage:     &episode.EpisodeLineage{WorkflowID: string(project) + ":activity:" + activity, RunID: "run-rq-0001", ActivityID: ptr(activity)},
			WorkerClass: ptr("junior-developer"),
			Usage:       usage,
			CostUSD:     ptr(1.2),
			NumTurns:    ptr(int64(18)),
			StartedAt:   at(t, "2026-10-01T14:00:00Z"),
			EndedAt:     at(t, "2026-10-01T14:25:00Z"),
			Outcome:     episode.EpisodeFailed,
			TracePath:   ptr("episodes/traces/ep-rq-a1.jsonl"),
		},
		{
			EpisodeID:   "ep-rq-a2",
			Kind:        episode.EpisodeKindConstruction,
			TargetRef:   activity,
			Lineage:     &episode.EpisodeLineage{WorkflowID: string(project) + ":activity:" + activity, RunID: "run-rq-0002", ActivityID: ptr(activity)},
			WorkerClass: ptr("junior-developer"),
			StartedAt:   at(t, "2026-10-01T14:30:00Z"),
			EndedAt:     at(t, "2026-10-01T14:31:00Z"),
			Outcome:     episode.EpisodeGap,
			GapReason:   ptr("trace stream lost: runner terminated before result event"),
		},
	}
	for i, a := range attempts {
		if err := subject.AppendEpisode(callContext(t, h, "arrange-"+a.EpisodeID), project, a); err != nil {
			t.Fatalf("hooks: arrange attempt %d (%s): %v", i+1, a.EpisodeID, err)
		}
	}
	writeTrace(t, h, "ep-rq-a1",
		`{"type":"system","subtype":"init","session_id":"ep-rq-a1"}`,
		`{"type":"assistant","message":{"stop_reason":"tool_use"}}`,
		`{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":18}`,
	)
	return subject.ListEpisodes(callContext(t, h, "1"), in.Query)
}
