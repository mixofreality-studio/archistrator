package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestRig_FullDraftCycleOverStdio builds the binary and drives a full draft cycle over a
// real stdio MCP connection: initialize (the client handshake), tools/list, a rejected
// putDraftModel (bad enum), an accepted putDraftModel, and publishDraft — asserting the
// committed state on the session branch. It is the end-to-end proof the binary speaks MCP.
func TestRig_FullDraftCycleOverStdio(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	repo := initGitRepoWithProject(t, minimalProject(), "aiarch-design/rigproj/3")

	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		envArtifactKind+"=Volatilities",
		envJobMode+"=draft",
		envTargetBranch+"=aiarch-design/rigproj/3",
		envProjectID+"=rigproj",
		envStateRoot+"="+repo,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to MCP server: %v", err)
	}
	defer func() { _ = session.Close() }()

	// tools/list — the draft-mode set must include putDraftModel and NOT setCritiqueVerdict.
	names := mustListToolNames(ctx, t, session)
	for _, want := range []string{"listResearchSources", "getCommittedSlot", "getDraftSlot", "getReviewThread", "getCritique", "putDraftModel", "respondToReviewComment", "publishDraft"} {
		if !names[want] {
			t.Fatalf("draft-mode tool %q missing from tools/list: %v", want, names)
		}
	}
	if names["setCritiqueVerdict"] {
		t.Fatalf("draft mode must NOT expose setCritiqueVerdict")
	}

	// A bad enum is rejected as an IsError tool result (self-correctable).
	res := callTool(ctx, t, session, "putDraftModel", map[string]any{
		"model": map[string]any{"items": []any{map[string]any{"name": "P", "rationale": "r", "axis": "changes a lot"}}},
	})
	if !res.IsError {
		t.Fatalf("expected IsError for a bad enum draft")
	}

	// A valid model is accepted.
	res = callTool(ctx, t, session, "putDraftModel", map[string]any{
		"model": map[string]any{"items": []any{map[string]any{"name": "P", "rationale": "r", "axis": "sameCustomerOverTime"}}},
	})
	if res.IsError {
		t.Fatalf("valid draft rejected: %s", contentText(res))
	}

	// publishDraft commits + pushes the session branch.
	res = callTool(ctx, t, session, "publishDraft", map[string]any{"message": "draft volatilities"})
	if res.IsError {
		t.Fatalf("publishDraft failed: %s", contentText(res))
	}

	// The pushed branch tip carries a valid, methodcheck-clean project.json with the draft.
	raw := gitShow(t, repo, "aiarch-design/rigproj/3", ".aiarch/state/project.json")
	proj, ok, derr := projectstate.DecodeProjectJSON(raw, "rigproj")
	if derr != nil || !ok {
		t.Fatalf("committed project.json does not decode: %v", derr)
	}
	if proj.Volatilities.Status != projectstate.ReviewCommitted || proj.Volatilities.Model == nil {
		t.Fatalf("committed volatilities slot not populated: %+v", proj.Volatilities)
	}
	findings, _ := methodcheck.ValidateProjectJSON(raw)
	if len(filterErrorFindings(findings)) != 0 {
		t.Fatalf("committed state has methodcheck errors: %v", findings)
	}
}

// TestRig_CritiqueModeToolSet proves critique mode omits putDraftModel and includes
// setCritiqueVerdict.
func TestRig_CritiqueModeToolSet(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	p := minimalProject()
	p.Mission = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: &projectstate.MissionStatement{}}
	repo := initGitRepoWithProject(t, p, "aiarch-design/rigproj/0")

	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		envArtifactKind+"=Mission",
		envJobMode+"=critique",
		envProjectID+"=rigproj",
		envStateRoot+"="+repo,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	if names["putDraftModel"] {
		t.Fatalf("critique mode must NOT expose putDraftModel")
	}
	if !names["setCritiqueVerdict"] {
		t.Fatalf("critique mode must expose setCritiqueVerdict")
	}
}

// TestRig_AnswerModeToolSet asserts the question-comments answer mode exposes
// respondToReviewComment (+ reads + publishDraft) but NEITHER putDraftModel NOR
// setCritiqueVerdict.
func TestRig_AnswerModeToolSet(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	p := minimalProject()
	p.Mission = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: &projectstate.MissionStatement{}}
	repo := initGitRepoWithProject(t, p, "aiarch-design/rigproj/0")

	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		envArtifactKind+"=Mission",
		envJobMode+"=answer",
		envProjectID+"=rigproj",
		envStateRoot+"="+repo,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	if names["putDraftModel"] {
		t.Fatalf("answer mode must NOT expose putDraftModel")
	}
	if names["setCritiqueVerdict"] {
		t.Fatalf("answer mode must NOT expose setCritiqueVerdict")
	}
	if !names["respondToReviewComment"] {
		t.Fatalf("answer mode must expose respondToReviewComment")
	}
	if !names["getReviewThread"] || !names["publishDraft"] {
		t.Fatalf("answer mode must expose the read verbs + publishDraft")
	}
}

// TestRig_RawReadToolsInEveryModeOverStdio proves the per-mode registration model:
// on TOP of the draft-mode composed verbs, the binary registers the non-hidden
// READ-ONLY + Engine raw generated tools from the catalog (each carrying its
// readOnlyHint), and NEVER an agent-hidden op nor a raw WRITE tool — the composed
// verbs stay the only write surface.
func TestRig_RawReadToolsInEveryModeOverStdio(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	repo := initGitRepoWithProject(t, minimalProject(), "aiarch-design/rigproj/3")

	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		envArtifactKind+"=Volatilities",
		envJobMode+"=draft",
		envProjectID+"=rigproj",
		envStateRoot+"="+repo,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	got := mustListToolsByName(ctx, t, session)

	// The draft-mode composed verbs are present.
	for _, w := range []string{"putDraftModel", "getCommittedSlot", "publishDraft"} {
		if got[w] == nil {
			t.Fatalf("draft composed verb %q missing: %v", w, keysOfTools(got))
		}
	}
	// A non-hidden read-only raw RA tool is registered and carries its readOnlyHint.
	if got["projectStateReadProject"] == nil {
		t.Fatalf("non-hidden read-only raw tool projectStateReadProject must register in every mode: %v", keysOfTools(got))
	}
	if a := got["projectStateReadProject"].Annotations; a == nil || !a.ReadOnlyHint {
		t.Fatalf("raw projectStateReadProject must carry readOnlyHint: %+v", got["projectStateReadProject"].Annotations)
	}
	// An agent-hidden raw op is NEVER registered.
	if got["projectStateCommitArtifact"] != nil {
		t.Fatal("agent-hidden projectStateCommitArtifact must NOT register")
	}
	// A non-hidden raw WRITE op is NOT registered — writes stay composed-only.
	if got["artifactStoreConstructionOutput"] != nil {
		t.Fatal("a raw write op must NOT register (the composed verbs are the write surface)")
	}
	// Every registered tool that IS a raw catalog tool must be non-hidden & read-only.
	for name := range got {
		if tl, ok := projectstate.InternalToolByName(name); ok {
			if tl.AgentHidden || !tl.ReadOnly {
				t.Fatalf("registered raw tool %q must be non-hidden & read-only (hidden=%v readOnly=%v)", name, tl.AgentHidden, tl.ReadOnly)
			}
		}
	}
}

// TestRig_RawReadToolExecutesOverStdio proves the EXECUTION rail end to end: the raw
// projectStateAccess READ tool (registered in every mode's set), driven over a real
// stdio MCP connection against a throwaway checkout, RUNS and returns the committed
// project as JSON text — no longer the bootstrap "not executable" error.
func TestRig_RawReadToolExecutesOverStdio(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	repo := initGitRepoWithProject(t, minimalProject(), "aiarch-design/rigproj/3")

	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		envArtifactKind+"=Volatilities",
		envJobMode+"=draft",
		envProjectID+"=rigproj",
		envStateRoot+"="+repo,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	res := callTool(ctx, t, session, "projectStateReadProject", map[string]any{"projectID": "rigproj"})
	if res.IsError {
		t.Fatalf("raw read tool returned an error result: %s", contentText(res))
	}
	body := contentText(res)
	if !strings.Contains(body, "rigproj") {
		t.Fatalf("expected the read project JSON to name the project id; got: %s", body)
	}
	if strings.Contains(body, "not executable") {
		t.Fatalf("raw read tool still returns the bootstrap stub: %s", body)
	}
}

// TestRig_RawUnavailableToolOverStdio proves an external-RA raw READ tool (registered
// in every mode's set) returns the honest unavailable-in-substrate error when called.
func TestRig_RawUnavailableToolOverStdio(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	repo := initGitRepoWithProject(t, minimalProject(), "aiarch-design/rigproj/3")

	// operatedRuntimeGetApplicationHealth is an external-RA read op with a single simple
	// required arg (appID) — it needs an operated runtime the job does not provision.
	const raName = "operatedRuntimeGetApplicationHealth"
	if _, ok := projectstate.InternalToolByName(raName); !ok {
		t.Skipf("%s not in catalog", raName)
	}

	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		envArtifactKind+"=Volatilities",
		envJobMode+"=draft",
		envProjectID+"=rigproj",
		envStateRoot+"="+repo,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	res := callTool(ctx, t, session, raName, map[string]any{"appID": "x"})
	if !res.IsError {
		t.Fatalf("expected an unavailable-in-substrate error result for %s", raName)
	}
	if !strings.Contains(contentText(res), "unavailable in this substrate") {
		t.Fatalf("expected unavailable-in-substrate text, got: %s", contentText(res))
	}
}

// TestRig_ConstructModeFullCycleOverStdio drives a full CONSTRUCTION cycle over a real
// stdio MCP connection: the construct job mode (no artifact kind — ambient component +
// activity), tools/list (the record verbs present, the design verbs absent),
// recordServiceContract for the ambient component, then publishDraft — asserting the
// committed contract on the pushed activity branch.
func TestRig_ConstructModeFullCycleOverStdio(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	repo := initGitRepoWithProject(t, projectWithComponent(t, "billing-engine"), "activity/C-BE")

	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		envJobMode+"="+jobModeConstruct,
		envComponentID+"=billing-engine",
		envActivityID+"=C-BE",
		envTargetBranch+"=activity/C-BE",
		envProjectID+"=rigproj",
		envStateRoot+"="+repo,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"recordServiceContract", "recordPhaseArtifact", "recordTestingState", "getCommittedSlot", "publishDraft"} {
		if !names[want] {
			t.Fatalf("construct-mode tool %q missing: %v", want, names)
		}
	}
	if names["putDraftModel"] || names["setCritiqueVerdict"] {
		t.Fatal("construct mode must not expose the design verbs")
	}

	res := callTool(ctx, t, session, "recordServiceContract", map[string]any{
		"contract": map[string]any{"component": "billingEngine", "layer": "Engine", "title": "Billing Engine"},
	})
	if res.IsError {
		t.Fatalf("recordServiceContract failed: %s", contentText(res))
	}
	res = callTool(ctx, t, session, "publishDraft", map[string]any{"message": "record billingEngine contract"})
	if res.IsError {
		t.Fatalf("publishDraft failed: %s", contentText(res))
	}

	raw := gitShow(t, repo, "activity/C-BE", ".aiarch/state/project.json")
	proj, ok, derr := projectstate.DecodeProjectJSON(raw, "rigproj")
	if derr != nil || !ok {
		t.Fatalf("committed project.json does not decode: %v", derr)
	}
	if proj.ServiceContracts["billingEngine"].Component != "billingEngine" {
		t.Fatalf("committed contract not present for the ambient component: %+v", proj.ServiceContracts)
	}
}

// TestRig_FreshSystemReachesRecordServiceContract replays, at $0, the wall the
// 2026-10-25 paid run hit (FIX-007): a System AS THE ARCHITECTURE AGENT DRAFTED IT (the
// fresh-project corpus, testdata/fresh-systems — no contractKey anywhere) goes through
// the production draft path (putDraftModel → publishDraft over stdio), and then every
// non-Resource component's detailed design records its contract through the construct
// verb. Before FIX-007 the server stored the System key-less and every
// recordServiceContract was refused ("the System gives component … no contractKey").
//
// Asserted: the published System stamps every non-Resource component's key as
// lowerCamel(id) and gives a Resource none; each component's contract lands at
// .serviceContracts[lowerCamel(id)] (never under the kebab id); a Resource is refused,
// named.
func TestRig_FreshSystemReachesRecordServiceContract(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	for name, fx := range freshSystems(t) {
		t.Run(name, func(t *testing.T) {
			const draftBranch = "aiarch-design/rigproj/5"
			repo := initGitRepoWithProject(t, fx.base, draftBranch)
			published := rigDraftSystem(t, bin, repo, draftBranch, fx.system)

			sys, ok := published.SystemDesign.Model.(*projectstate.System)
			if !ok || sys == nil {
				t.Fatalf("published System slot holds no System: %+v", published.SystemDesign)
			}
			for _, c := range sys.Components {
				got := ""
				if c.ContractKey != nil {
					got = *c.ContractKey
				}
				want := lowerCamelOfID(c.ID)
				if c.Kind == projectstate.CompResource {
					want = ""
				}
				if got != want {
					t.Errorf("published component %q (%s) has contractKey %q, want %q", c.ID, c.Kind, got, want)
				}
			}
			for _, c := range sys.Components {
				branch := "activity/C-" + c.ID
				mustGit(t, repo, "checkout", "-q", "-b", branch, draftBranch)
				mustGit(t, repo, "push", "-q", "-u", "origin", branch)
				res, contracts := rigRecordContract(t, bin, repo, branch, c)
				if c.Kind == projectstate.CompResource {
					if !res.IsError || !strings.Contains(contentText(res), c.ID) {
						t.Errorf("recordServiceContract for the Resource %q: want a refusal naming it, got error=%v %q", c.ID, res.IsError, contentText(res))
					}
					continue
				}
				if res.IsError {
					t.Errorf("recordServiceContract for %q (%s) refused: %s", c.ID, c.Kind, contentText(res))
					continue
				}
				key := lowerCamelOfID(c.ID)
				if contracts[key].Component != key {
					t.Errorf("component %q: contract not filed at .serviceContracts[%q]: %v", c.ID, key, keysOfContracts(contracts))
				}
				if _, wrong := contracts[c.ID]; wrong && c.ID != key {
					t.Errorf("component %q: contract filed under the kebab id", c.ID)
				}
			}
		})
	}
}

// freshSystem is one fresh-project fixture: the System model exactly as the run's
// architecture agent drafted it, and the committed Phase-1 predecessors (slots 0-4) it
// was drafted against, which the draft gate's call-chain rules resolve it through.
type freshSystem struct {
	system json.RawMessage
	base   projectstate.Project
}

// freshSystems is the fresh-project corpus (testdata/fresh-systems/README.md).
func freshSystems(t *testing.T) map[string]freshSystem {
	t.Helper()
	const predecessors = ".predecessors.json"
	paths, err := filepath.Glob(filepath.Join("testdata", "fresh-systems", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]freshSystem{}
	for _, p := range paths {
		if strings.HasSuffix(p, predecessors) {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(p), ".json")
		sys, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(strings.TrimSuffix(p, ".json") + predecessors)
		if err != nil {
			t.Fatalf("fixture %s has no predecessors file: %v", name, err)
		}
		base, ok, err := projectstate.DecodeProjectJSON(raw, "rigproj")
		if err != nil || !ok {
			t.Fatalf("fixture %s predecessors do not decode: ok=%v err=%v", name, ok, err)
		}
		out[name] = freshSystem{system: sys, base: base}
	}
	if len(out) == 0 {
		t.Fatal("no fresh-system fixtures")
	}
	return out
}

// lowerCamelOfID is the test's own statement of the key a kebab id files its contract
// under — written out here, not borrowed from the code under test.
func lowerCamelOfID(id string) string {
	parts := strings.Split(id, "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func keysOfContracts(m map[string]projectstate.ServiceContract) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// rigConnect starts the binary in repo under env and returns a connected MCP session.
func rigConnect(t *testing.T, bin, repo string, env ...string) (context.Context, *mcp.ClientSession) {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), env...)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return ctx, session
}

// rigDraftSystem drafts system into the System slot in draft mode and publishes it,
// returning the project as the pushed branch carries it.
func rigDraftSystem(t *testing.T, bin, repo, branch string, system json.RawMessage) projectstate.Project {
	t.Helper()
	ctx, session := rigConnect(t, bin, repo,
		envArtifactKind+"=System", envJobMode+"=draft", envTargetBranch+"="+branch,
		envProjectID+"=rigproj", envStateRoot+"="+repo)
	var model map[string]any
	if err := json.Unmarshal(system, &model); err != nil {
		t.Fatal(err)
	}
	if res := callTool(ctx, t, session, "putDraftModel", map[string]any{"model": model}); res.IsError {
		t.Fatalf("putDraftModel(System) refused the drafted System: %s", contentText(res))
	}
	if res := callTool(ctx, t, session, "publishDraft", map[string]any{"message": "draft system"}); res.IsError {
		t.Fatalf("publishDraft failed: %s", contentText(res))
	}
	_ = session.Close()
	mustGit(t, repo, "fetch", "-q", "origin")
	mustGit(t, repo, "checkout", "-q", branch)
	mustGit(t, repo, "reset", "-q", "--hard", "origin/"+branch)
	proj, ok, err := projectstate.DecodeProjectJSON(gitShow(t, repo, branch, ".aiarch/state/project.json"), "rigproj")
	if err != nil || !ok {
		t.Fatalf("published project.json does not decode: ok=%v err=%v", ok, err)
	}
	return proj
}

// rigRecordContract runs construct mode on branch for component c, records a minimal
// contract and (on success) publishes it, returning the tool result and the contracts
// the pushed branch carries.
func rigRecordContract(t *testing.T, bin, repo, branch string, c projectstate.Component) (*mcp.CallToolResult, map[string]projectstate.ServiceContract) {
	t.Helper()
	ctx, session := rigConnect(t, bin, repo,
		envJobMode+"="+jobModeConstruct, envComponentID+"="+c.ID, envActivityID+"=C-"+c.ID,
		envTargetBranch+"="+branch, envProjectID+"=rigproj", envStateRoot+"="+repo)
	key := lowerCamelOfID(c.ID)
	res := callTool(ctx, t, session, "recordServiceContract", map[string]any{
		"contract": map[string]any{"component": key, "layer": contractLayerOf(c.Kind), "title": c.Name},
	})
	if res.IsError {
		return res, nil
	}
	if pub := callTool(ctx, t, session, "publishDraft", map[string]any{"message": "record " + key + " contract"}); pub.IsError {
		t.Fatalf("publishDraft for %q failed: %s", c.ID, contentText(pub))
	}
	proj, ok, err := projectstate.DecodeProjectJSON(gitShow(t, repo, "origin/"+branch, ".aiarch/state/project.json"), "rigproj")
	if err != nil || !ok {
		t.Fatalf("published project.json does not decode: ok=%v err=%v", ok, err)
	}
	return res, proj.ServiceContracts
}

// contractLayerOf is a ServiceContract's Layer word for a component kind.
func contractLayerOf(k projectstate.ComponentKind) string {
	return map[projectstate.ComponentKind]string{
		projectstate.CompClient: "Client", projectstate.CompManager: "Manager", projectstate.CompEngine: "Engine",
		projectstate.CompResourceAccess: "ResourceAccess", projectstate.CompResource: "Resource", projectstate.CompUtility: "Utility",
	}[k]
}

func keysOfTools(m map[string]*mcp.Tool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---- rig helpers ----

// mustListToolNames lists the session's tools and returns the set of tool names.
func mustListToolNames(ctx context.Context, t *testing.T, session *mcp.ClientSession) map[string]bool {
	t.Helper()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	return names
}

// mustListToolsByName lists the session's tools and returns them keyed by name.
func mustListToolsByName(ctx context.Context, t *testing.T, session *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]*mcp.Tool{}
	for _, tl := range tools.Tools {
		got[tl.Name] = tl
	}
	return got
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "aiarch-state-mcp")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v: %s", err, out)
	}
	return bin
}

func initGitRepoWithProject(t *testing.T, p projectstate.Project, branch string) string {
	t.Helper()
	repo := t.TempDir()
	bare := t.TempDir()
	mustGit(t, bare, "init", "--bare", "-b", "main")
	mustGit(t, repo, "init", "-b", "main")
	mustGit(t, repo, "config", "user.name", "rig")
	mustGit(t, repo, "config", "user.email", "rig@example.com")
	mustGit(t, repo, "remote", "add", "origin", bare)

	if err := os.MkdirAll(filepath.Join(repo, statePathPrefix), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, statePathPrefix, projectFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-m", "seed")
	mustGit(t, repo, "checkout", "-b", branch)
	mustGit(t, repo, "push", "-u", "origin", branch)
	return repo
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func gitShow(t *testing.T, repo, branch, path string) []byte {
	t.Helper()
	cmd := exec.Command("git", "show", branch+":"+path)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git show %s:%s: %v: %s", branch, path, err, out)
	}
	return out
}

func callTool(ctx context.Context, t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return res
}

func contentText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
