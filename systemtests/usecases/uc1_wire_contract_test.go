package usecases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// Coverage additions for today's server changes (blackbox, wire-level):
//
//   - Test_UC1_NoActiveSessionErrorContract: the clean no-session error contract
//     (065a9e7) — HTTP 404 body + MCP isError text both name the project, neither
//     leaks a Temporal/git internal.
//   - Test_UC1_ListProjects_StoredOwnerAndPhaseName: ListProjects returns the
//     project's STORED owner (PM-P2-6) and human-readable PhaseName (PM-P2-5).
//   - Test_UC1_WritePathValidation_SystemEncapsulatesRejection: a19a25b's
//     RequireModelFields write-path validation (SYS-ENCAPSULATES) rejects a
//     committed System model whose Manager component declares no encapsulated
//     volatility — proven at the wire via the agentic fake's scriptable draft
//     (the only wire-reachable way to land such a model, since the server's own
//     drafting prompts never omit the field; RequireModelFields is enforced on
//     every read of a populated slot, not just the write path that produced it).

// Test_UC1_NoActiveSessionErrorContract proves 065a9e7's clean error-message
// contract: GetSessionState for a kind that was NEVER drafted on an otherwise
// real project must refuse with a single, clean, project-scoped NotFound —
// 'no active design session for project "<id>"' — never a leaked Temporal
// internal ("workflow not found for ID: ..."). Checked over BOTH wire surfaces
// against the SAME project (HTTP body's "error" field; MCP's isError text).
func Test_UC1_NoActiveSessionErrorContract(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	srv := startServer(t, true /* devAuth */)
	httpTr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = httpTr.Close() })

	projectID, err := httpTr.CreateProject(ctx, "no-session-"+harness.ShortID())
	if err != nil {
		t.Fatalf("createProject: %v", err)
	}
	wantSubstr := fmt.Sprintf("no active design session for project %q", projectID)

	assertCleanNotFound := func(t *testing.T, tr harness.Transport) {
		t.Helper()
		_, _, err := tr.GetSessionState(ctx, projectID, "glossary")
		if !errors.Is(err, harness.ErrNotFound) {
			t.Fatalf("[%s] GetSessionState on a never-drafted kind: err = %v, want ErrNotFound", tr.Name(), err)
		}
		if !strings.Contains(err.Error(), wantSubstr) {
			t.Fatalf("[%s] error text = %q, want it to contain %q (clean no-session contract, 065a9e7)", tr.Name(), err.Error(), wantSubstr)
		}
		if strings.Contains(strings.ToLower(err.Error()), "workflow not found") {
			t.Fatalf("[%s] error text leaked a Temporal internal: %q", tr.Name(), err.Error())
		}
	}

	// [http] a real project, a Phase-1 kind that was NEVER drafted -> 404 NotFound,
	// body carrying the clean Detail (not a Temporal-internal string).
	t.Run("http", func(t *testing.T) { assertCleanNotFound(t, httpTr) })

	// [mcp] the SAME project, the SAME never-drafted kind, over the MCP surface —
	// isError text carries the identical Detail (mapManagerError's "<Kind>: <Detail>").
	mcpTr := harness.NewMCPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = mcpTr.Close() })
	t.Run("mcp", func(t *testing.T) { assertCleanNotFound(t, mcpTr) })

	t.Logf("no-session error contract verified over both surfaces: %q", wantSubstr)
}

// Test_UC1_ListProjects_StoredOwnerAndPhaseName proves PM-P2-6 (ListProjects
// returns each project's CANONICAL STORED owner, not the caller's enumeration
// scope echoed back) and PM-P2-5 (PhaseName is the human-readable label
// alongside the bare Phase ordinal) over both wire surfaces.
func Test_UC1_ListProjects_StoredOwnerAndPhaseName(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	srv := startServer(t, true /* devAuth */)
	httpTr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = httpTr.Close() })

	name := "list-projects-" + harness.ShortID()
	projectID, err := httpTr.CreateProject(ctx, name)
	if err != nil {
		t.Fatalf("createProject: %v", err)
	}

	assertListed := func(t *testing.T, tr harness.Transport) {
		t.Helper()
		summaries, err := tr.ListProjects(ctx, harness.TestOwner)
		if err != nil {
			t.Fatalf("[%s] listProjects: %v", tr.Name(), err)
		}
		var found *harness.ProjectSummary
		for i := range summaries {
			if summaries[i].ProjectID == projectID {
				found = &summaries[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("[%s] listProjects(%q) did not include the just-created project %q; got %d summaries", tr.Name(), harness.TestOwner, projectID, len(summaries))
		}
		if found.Owner != harness.TestOwner {
			t.Fatalf("[%s] summary.Owner = %q, want the STORED owner %q (PM-P2-6)", tr.Name(), found.Owner, harness.TestOwner)
		}
		// A freshly-created project is in Phase 1 (system-design) — the human
		// label the SPA renders instead of a bare ordinal (PM-P2-5).
		if found.PhaseName != "system-design" {
			t.Fatalf("[%s] summary.PhaseName = %q, want %q for a freshly-created project", tr.Name(), found.PhaseName, "system-design")
		}
		t.Logf("[%s] listProjects: %q owner=%q phaseName=%q", tr.Name(), found.Name, found.Owner, found.PhaseName)
	}

	t.Run("http", func(t *testing.T) { assertListed(t, httpTr) })

	mcpTr := harness.NewMCPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = mcpTr.Close() })
	t.Run("mcp", func(t *testing.T) { assertListed(t, mcpTr) })
}

// Test_UC1_WritePathValidation_SystemEncapsulatesRejection proves a19a25b's
// SYS-ENCAPSULATES write-path validation rule: a System model committing a
// Manager component with an EMPTY encapsulates field is rejected (the
// component "claims to encapsulate nothing", which the strict codec — write +
// read-back, RequireModelFields — refuses to accept as committed state).
//
// The only WIRE-REACHABLE way to land such a model is the agentic dispatch path
// (the real drafting prompts never omit the field, and there is no raw
// "PUT a draft model" route on webClient/mcpClient) — mirrors
// uc1_agentic_test.go's fake.SetDraft use. The fake commits the SCRIPTED
// (invalid) draft directly onto the session branch, bypassing the agent-facing
// putDraftModel gate a real claude-code-action job would hit first — which is
// exactly why this is the RIGHT place to prove the SERVER's own read-back gate
// independently catches it too (the F36/F66 read-back-parity invariant:
// decodeSlotsMap enforces the SAME RequireModelFields check on every read of a
// populated slot, not only on the write that first produced it).
func Test_UC1_WritePathValidation_SystemEncapsulatesRejection(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	const account = "aiarch-test-org"
	const kind = "system"

	projRepo := harness.StartLocalGitRepo(t, "main")
	artRepo := harness.StartLocalGitRepo(t, "main")
	fake := harness.StartAgenticGitHub(t, projRepo, account)
	appKey := harness.GenerateAppKeyPEM(t)

	srv := startServerWithEnv(t, true /* devAuth */, fake.Env(projRepo, artRepo, appKey))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	// Script an INVALID System draft: one Manager component with kind/layer
	// correctly matched (so requireComponentFields' layer-consistency check
	// passes and does NOT mask the assertion this test is actually after) but an
	// EMPTY "encapsulates" — the SYS-ENCAPSULATES violation for a volatility-
	// owning kind (modelfields.go requireComponentFields).
	invalidSystem := json.RawMessage(`{
		"components": [
			{"id": "mgr-1", "name": "orderManager", "kind": 1, "layer": 1, "encapsulates": ""}
		],
		"relationships": [],
		"dynamicViews": []
	}`)
	fake.SetDraft(kind, invalidSystem)

	projectID, err := tr.CreateProject(ctx, "uc1-validation-"+harness.ShortID())
	if err != nil {
		t.Fatalf("createProject: %v", err)
	}

	// system's Phase-1 predecessors must already be Committed (checkPhase1Predecessor)
	// — seed them directly onto the project.json CreateProject just wrote (must run
	// AFTER CreateProject: SeedCommittedDesignSlots reads the existing state to edit it).
	projRepo.SeedCommittedDesignSlots("mission", "glossary", "scrubbedRequirements", "volatilities", "coreUseCases")

	if _, err := tr.RequestArtifactDraft(ctx, projectID, kind); err != nil {
		t.Fatalf("requestArtifactDraft: %v", err)
	}
	_ = harness.WaitForStartedSession(ctx, t, tr, projectID, kind, 90*time.Second)

	// HARD: the agentic job committed the (invalid) draft and the server's OWN
	// read-back gate (decodeSlotsMap -> RequireModelFields) rejected it — this is
	// deterministic (a scripted fake draft, no LLM), so a bounded miss is a real
	// defect, not a model-quality flake. The anti-wedge rule routes a read-back
	// decode failure to the SAME human-visible StageDraftFailed gate a terminal
	// job failure uses (workflow.go readBackDecodeFailedReason).
	if !harness.TryReachStage(ctx, tr, projectID, kind, "draftFailed", 90*time.Second) {
		st, _, _ := tr.GetSessionState(ctx, projectID, kind)
		t.Fatalf("invalid System draft was NOT rejected: session at stage %q (want draftFailed) — SYS-ENCAPSULATES write-path validation did not fire (fake fault: %q)", st.Stage, fake.LastFault())
	}

	st, _, err := tr.GetSessionState(ctx, projectID, kind)
	if err != nil {
		t.Fatalf("getSessionState after draftFailed: %v", err)
	}
	const wantSubstr = "must name the volatility it encapsulates"
	if !strings.Contains(st.FailureReason, wantSubstr) {
		t.Fatalf("draftFailed FailureReason = %q, want it to contain %q (SYS-ENCAPSULATES, a19a25b) — fake fault: %q, dispatches=%d",
			st.FailureReason, wantSubstr, fake.LastFault(), fake.DispatchCount())
	}
	t.Logf("SYS-ENCAPSULATES write-path validation verified: draftFailed with reason %q", st.FailureReason)
}
