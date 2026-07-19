package main

// toolproxy_test.go covers Task-5 review finding 2: the brief requires "tool
// responses that reference session/project state include the SPA URL so the
// driver surfaces it." mountProxiedTools is a pure verbatim relay (see
// toolproxy.go's package doc) EXCEPT for this one addition — a trailing
// `SPA: <url>` text content block appended to results of tools whose NAME
// falls in the get/start/session/project word families (see
// referencesSessionOrProjectState's doc for the exact, conservative,
// name-prefix rule and why it was chosen over a semantic/description-based
// one). This uses two in-memory-transport mcp.Server/Client pairs — one
// standing in for the archistrator-server child (the "upstream" being
// proxied), one standing in for the Claude Code driver calling the LOCAL
// bridged server — so the whole relay + footer behavior is exercised without
// spawning any real subprocess.
import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectFakeUpstream builds an in-memory upstream mcp.Server exposing two
// tools: one whose name matches the session/project-state word families
// (systemDesignGetProject, real name from the 35-tool catalog — see
// mcp_mount.go) and one that does not (constructionExecuteNextActivity, also
// a real name). Each handler returns one fixed TextContent block so the test
// can assert exactly what mountProxiedTools adds on top, byte for byte.
func connectFakeUpstream(ctx context.Context, t *testing.T) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake-upstream", Version: "0.0.0"}, nil)

	mcp.AddTool(srv, &mcp.Tool{Name: "systemDesignGetProject", Description: "matching tool"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "project-state-payload"}}}, nil, nil
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "constructionExecuteNextActivity", Description: "non-matching tool"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "activity-dispatched"}}}, nil, nil
		})

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("connect fake upstream server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "archistrator-cli-under-test", Version: mcpVersion}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect fake upstream client: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// mountedLocalClient mounts upstream's tools onto a fresh local mcp.Server via
// mountProxiedTools (the function under test), then connects a second
// in-memory client to LOCAL — standing in for the Claude Code driver — and
// returns that client session for the test to drive CallTool through the
// full relay path.
func mountedLocalClient(ctx context.Context, t *testing.T, upstream *mcp.ClientSession, spaURL string) *mcp.ClientSession {
	t.Helper()
	local := mcp.NewServer(&mcp.Implementation{Name: "archistrator", Version: mcpVersion}, nil)
	if _, err := mountProxiedTools(ctx, local, upstream, spaURL); err != nil {
		t.Fatalf("mountProxiedTools: %v", err)
	}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := local.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("connect local server: %v", err)
	}
	driver := mcp.NewClient(&mcp.Implementation{Name: "driver-under-test", Version: "0.0.0"}, nil)
	cs, err := driver.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect driver client: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func textBlocks(t *testing.T, res *mcp.CallToolResult) []string {
	t.Helper()
	var out []string
	for _, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			t.Fatalf("content block is not TextContent: %#v", c)
		}
		out = append(out, tc.Text)
	}
	return out
}

// TestMountProxiedTools_AppendsSPAFooter_ForSessionProjectStateTool covers a
// proxied call to a tool whose name references session/project state
// (systemDesignGetProject — Get + Project word families): the result must
// carry the upstream's original content block UNCHANGED, plus exactly one
// trailing "SPA: <url>" text block.
func TestMountProxiedTools_AppendsSPAFooter_ForSessionProjectStateTool(t *testing.T) {
	ctx := context.Background()
	upstream := connectFakeUpstream(ctx, t)
	const spaURL = "http://127.0.0.1:8877/"
	driver := mountedLocalClient(ctx, t, upstream, spaURL)

	res, err := driver.CallTool(ctx, &mcp.CallToolParams{Name: "systemDesignGetProject"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	blocks := textBlocks(t, res)
	if len(blocks) != 2 {
		t.Fatalf("content blocks = %v, want exactly 2 (original + SPA footer)", blocks)
	}
	if blocks[0] != "project-state-payload" {
		t.Errorf("original content block = %q, want it unchanged", blocks[0])
	}
	want := "SPA: " + spaURL
	if blocks[1] != want {
		t.Errorf("footer block = %q, want %q", blocks[1], want)
	}
}

// TestMountProxiedTools_LeavesNonMatchingToolUntouched covers the negative
// case: constructionExecuteNextActivity does not fall in the get/start/
// session/project word families, so its result must be relayed byte-for-byte
// verbatim — no footer appended.
func TestMountProxiedTools_LeavesNonMatchingToolUntouched(t *testing.T) {
	ctx := context.Background()
	upstream := connectFakeUpstream(ctx, t)
	driver := mountedLocalClient(ctx, t, upstream, "http://127.0.0.1:8877/")

	res, err := driver.CallTool(ctx, &mcp.CallToolParams{Name: "constructionExecuteNextActivity"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	blocks := textBlocks(t, res)
	if len(blocks) != 1 {
		t.Fatalf("content blocks = %v, want exactly 1 (untouched)", blocks)
	}
	if blocks[0] != "activity-dispatched" {
		t.Errorf("content block = %q, want unchanged", blocks[0])
	}
	for _, b := range blocks {
		if strings.HasPrefix(b, "SPA:") {
			t.Fatalf("non-matching tool result carries an SPA footer: %v", blocks)
		}
	}
}

// TestReferencesSessionOrProjectState documents and pins the conservative
// name-prefix rule against every real tool name in the 35-tool catalog
// (mcp_mount.go's four generated Handler.Register calls) so a future rename
// or catalog change gets a visible diff here instead of silently drifting.
func TestReferencesSessionOrProjectState(t *testing.T) {
	cases := map[string]bool{
		// systemDesign* — matches only the specific Get/List/Create/Start verbs.
		"systemDesignGetProject":             true,
		"systemDesignGetSessionState":        true,
		"systemDesignListProjects":           true,
		"systemDesignCreateProject":          true,
		"systemDesignStartSystemDesign":      true,
		"systemDesignAdvancePhase":           false,
		"systemDesignRequestArtifactDraft":   false,
		"systemDesignSetOperatingModel":      false,
		"systemDesignSetResearchInput":       false,
		"systemDesignAskQuestions":           false,
		"systemDesignAcknowledgeStaleBasis":  false,
		"systemDesignSetReviewCommentStatus": false,
		"systemDesignSubmitReviewDecision":   false,

		// projectDesign* — the whole family matches via its OWN namespace word
		// ("project"), since every Project-Design artifact IS project state.
		"projectDesignAdvanceToConstruction":  true,
		"projectDesignGetSessionState":        true,
		"projectDesignRequestArtifactDraft":   true,
		"projectDesignRequestSDPCommit":       true,
		"projectDesignAskQuestions":           true,
		"projectDesignAcknowledgeStaleBasis":  true,
		"projectDesignSetReviewCommentStatus": true,
		"projectDesignSubmitReviewDecision":   true,
		"projectDesignSubmitSDPDecision":      true,

		// construction* — matches only its own Get/Project verbs.
		"constructionGetSessionState":     true,
		"constructionPauseProject":        true,
		"constructionExecuteNextActivity": false,
		"constructionOverrideActivity":    false,
		"constructionRunReplanSweep":      false,
		"constructionSubmitPhaseDecision": false,
		"constructionUpdateReviewPolicy":  false,

		// operations* — none reference session/project state by name; they are
		// about the deployed/operated system, a DIFFERENT state family.
		"operationsApplyDelinquencyPolicy":  false,
		"operationsDeployAfterConstruction": false,
		"operationsQueryCostProjection":     false, // NOT a false-positive: "Projection" is a whole word, not "Project".
		"operationsQueryOperatedSystemView": false,
		"operationsReconcileOperatedState":  false,
		"operationsWithdrawSystem":          false,
	}
	for name, want := range cases {
		if got := referencesSessionOrProjectState(name); got != want {
			t.Errorf("referencesSessionOrProjectState(%q) = %v, want %v", name, got, want)
		}
	}
}
