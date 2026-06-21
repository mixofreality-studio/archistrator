package usecases

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// UC1 — co-author a Method artifact — driven BLACK-BOX through the running
// server's published Client surface. This is the wire-level replacement for the
// retired in-package internal/client/web/integration_test.go (which was
// `package web` and wired the Manager/Engine/RA by importing internals). Here the
// harness boots the REAL server binary as a SUBPROCESS and drives it over HTTP,
// asserting the route/auth/Manager wiring round-trips end to end.
//
// The green gate is the WIRING (a started session observable through the read
// route + commit through the review route), NOT the small local model converging
// to a constructable glossary — gating on model quality would make a wiring test
// flaky (see designs/aiarch/implementation/log/C-CW-2026-05-30.md). The
// approve→commit leg is therefore best-effort.

func Test_UC1_CoauthorGlossary_WiringHappyPath(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	srv := startServer(t, true /* devAuth */)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	runUC1(ctx, t, tr)
}

// runUC1 is the transport-agnostic UC1 flow. It runs against ANY Transport, so
// once mcpClient is built the MCP transport reuses it verbatim for the R4
// cross-surface equivalence test (see Test_UC1_CrossSurfaceEquivalence below).
func runUC1(ctx context.Context, t *testing.T, tr harness.Transport) {
	t.Helper()
	const kind = "glossary"

	// 0) Mint the project through the catalog — projects are no longer born
	// implicitly on first phase touch; the catalog assigns the projectId every
	// project-scoped route is then nested under.
	projectID, err := tr.CreateProject(ctx, "UC1 wiring")
	if err != nil {
		t.Fatalf("[%s] createProject: %v", tr.Name(), err)
	}
	if projectID == "" {
		t.Fatalf("[%s] createProject: empty projectId", tr.Name())
	}

	// 1) Start a co-authoring draft for the glossary kind.
	sessionRef, err := tr.RequestArtifactDraft(ctx, projectID, kind)
	if err != nil {
		t.Fatalf("[%s] draft: %v", tr.Name(), err)
	}
	if sessionRef == "" {
		t.Fatalf("[%s] draft: empty sessionRef", tr.Name())
	}

	// 2) The started session is observable through the published read surface.
	st := harness.WaitForStartedSession(ctx, t, tr, projectID, kind, 90*time.Second)
	if st.ProjectID != projectID {
		t.Fatalf("[%s] session projectId = %q, want %q", tr.Name(), st.ProjectID, projectID)
	}
	if st.ArtifactKind != kind {
		t.Fatalf("[%s] session artifactKind = %q, want %q", tr.Name(), st.ArtifactKind, kind)
	}
	t.Logf("[%s] UC1 wiring verified: started session observable at stage %q", tr.Name(), st.Stage)

	// 3) Best-effort approve→commit leg IF the local model reached the gate.
	if !harness.TryReachStage(ctx, tr, projectID, kind, "awaitingReview", 2*time.Minute) {
		t.Logf("[%s] model did not reach the human gate in window; wiring already verified — skipping approve leg", tr.Name())
		return
	}
	if err := tr.SubmitReview(ctx, projectID, kind, "approve", ""); err != nil {
		t.Fatalf("[%s] approve: %v", tr.Name(), err)
	}
	if !harness.TryReachStage(ctx, tr, projectID, kind, "committed", 30*time.Second) {
		t.Fatalf("[%s] approved at the gate but glossary never reached committed", tr.Name())
	}
	t.Logf("[%s] UC1 approve→commit leg verified through the review route", tr.Name())
}

// Test_UC1_ResearchInputContract is a black-box check of the research-input
// Client contract — the wire-level replacement for the retired in-package
// handlers_setresearchinput_test.go. A valid corpus is accepted (204); an empty
// corpus is rejected at the transport edge (400 → ErrBadRequest).
func Test_UC1_ResearchInputContract(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	srv := startServer(t, true /* devAuth */)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	// Mint the project first — research-input is now a project-scoped route, and a
	// project must exist before its head-state can record the corpus.
	pid, err := tr.CreateProject(ctx, "UC1 research-input contract")
	if err != nil {
		t.Fatalf("createProject: %v", err)
	}

	// A well-formed corpus passes the Client's shape validation and is FORWARDED
	// to the Manager — it must NOT be rejected as a bad request. The Client
	// contract under test is "accept + forward a well-formed request", not the
	// Manager's project precondition. Tolerate nil OR NotFound (best-effort on the
	// Manager precondition) and only fail on a wrong 400.
	if err := tr.SetResearchInput(ctx, pid, []harness.ResearchSource{
		{Title: "Founder brief", Content: "We are building X."},
		{Title: "Competitor analysis", Content: "Z does W."},
	}); errors.Is(err, harness.ErrBadRequest) {
		t.Fatalf("well-formed research input wrongly rejected as bad request: %v", err)
	}

	// Shape violations are rejected at the transport edge (400 → ErrBadRequest),
	// before any Manager call — ported black-box from the retired handler test.
	for name, sources := range map[string][]harness.ResearchSource{
		"empty corpus":    nil,
		"missing title":   {{Content: "has content, no title"}},
		"missing content": {{Title: "has title, no content"}},
	} {
		if err := tr.SetResearchInput(ctx, pid, sources); !errors.Is(err, harness.ErrBadRequest) {
			t.Errorf("%s: err = %v, want ErrBadRequest", name, err)
		}
	}
}

// Test_UC1_AuthRejectsWithoutClaims is the black-box auth-boundary check — the
// wire-level replacement for the retired in-package auth test. With dev-mode OFF
// and no Envoy-forwarded claims, an intent is rejected as unauthenticated.
func Test_UC1_AuthRejectsWithoutClaims(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	srv := startServer(t, false /* devAuth off */)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	if _, err := tr.StartDesign(ctx, harness.NewProjectID()); !errors.Is(err, harness.ErrUnauthenticated) {
		t.Fatalf("dev-off start: err = %v, want ErrUnauthenticated", err)
	}
}

// Test_UC1_CrossSurfaceEquivalence is the R4 headline test: drive UC1 through
// webClient (HTTP) AND mcpClient (MCP) and assert identical committed state. It
// is SKIPPED until mcpClient is built (no MCP SDK in the server module yet — see
// architecture.dsl mcpClient + the C-MC construction activity). When that lands,
// add harness.NewMCPTransport and run runUC1 against both, asserting equality —
// the guard that keeps "mcpClient mirrors webClient method-for-method" honest.
func Test_UC1_CrossSurfaceEquivalence(t *testing.T) {
	t.Skip("R4 cross-surface equivalence: pending mcpClient construction (C-MC) — see architecture.dsl mcpClient")
}
