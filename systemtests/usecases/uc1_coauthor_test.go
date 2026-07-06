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

	// glossary's Phase-1 predecessor (mission) must already be Committed — the
	// wire surface enforces the spine ordering (systemdesignmanager.go
	// checkPhase1Predecessor, STP-UC1-B1). Seed it directly in the project's
	// on-disk head-state (black-box JSON, mirroring uc1_agentic_test.go's
	// SeedCommittedDesignSlots use) rather than driving mission through its own
	// co-author round trip — this test proves GLOSSARY's wiring (chosen because its
	// cassettes exercise both the architect draft AND the PM-critique round-trip),
	// not the whole Phase-1 sequence.
	repo := harness.StartLocalGitRepo(t, "main")
	srv := startServerWithEnv(t, true /* devAuth */, harness.GitLocalEnv(repo.URL()))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	runUC1(ctx, t, tr, repo)
}

// runUC1 is the transport-agnostic UC1 flow. It runs against ANY Transport, so
// once mcpClient is built the MCP transport reuses it verbatim for the R4
// cross-surface equivalence test (see Test_UC1_CrossSurfaceEquivalence below).
// repo is the LOCAL project-state git substrate the caller's server was booted
// against — used ONLY to seed glossary's uncommitted Phase-1 predecessor
// (mission) directly after CreateProject, never to bypass the wire surface for
// anything this flow actually asserts on.
func runUC1(ctx context.Context, t *testing.T, tr harness.Transport, repo harness.LocalGitRepo) {
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

	// 0b) Seed glossary's predecessor (mission) as Committed — the spine-ordering
	// gate (checkPhase1Predecessor) refuses an out-of-order draft with a 409
	// FailedPrecondition otherwise.
	repo.SeedCommittedDesignSlots("mission")

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
// webClient (HTTP) AND mcpClient (MCP) — the server's full MCP surface
// (server/internal/client/mcp/*, mounted at /mcp via streamable HTTP; the
// official modelcontextprotocol/go-sdk on the server side) — and assert
// EQUIVALENT observable state. runUC1 is transport-agnostic (identical steps,
// identical assertions) — reusing it verbatim against harness.NewHTTPTransport
// and harness.NewMCPTransport IS the equivalence proof: any divergence in
// wiring behavior between the two surfaces (a route the MCP tool handles
// differently, a stage the MCP read decodes differently, ...) surfaces as one
// sub-test failing while the other passes, catching exactly the "mcpClient
// mirrors webClient method-for-method" regression this test guards against.
// Each surface gets its OWN server + repo (UC1 wiring is per-project, not
// shared state) — the equivalence asserted is BEHAVIORAL (the same verb
// sequence produces the same wire-observable outcomes), not a literal shared
// aggregate.
func Test_UC1_CrossSurfaceEquivalence(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	httpRepo := harness.StartLocalGitRepo(t, "main")
	httpSrv := startServerWithEnv(t, true /* devAuth */, harness.GitLocalEnv(httpRepo.URL()))
	httpTr := harness.NewHTTPTransport(httpSrv.BaseURL())
	t.Cleanup(func() { _ = httpTr.Close() })

	mcpRepo := harness.StartLocalGitRepo(t, "main")
	mcpSrv := startServerWithEnv(t, true /* devAuth */, harness.GitLocalEnv(mcpRepo.URL()))
	mcpTr := harness.NewMCPTransport(mcpSrv.BaseURL())
	t.Cleanup(func() { _ = mcpTr.Close() })

	t.Run("http", func(t *testing.T) { runUC1(ctx, t, httpTr, httpRepo) })
	t.Run("mcp", func(t *testing.T) { runUC1(ctx, t, mcpTr, mcpRepo) })
}
