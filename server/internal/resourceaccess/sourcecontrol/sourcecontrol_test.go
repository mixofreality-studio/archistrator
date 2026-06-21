package sourcecontrol_test

// SERVICE TEST PLAN (STP) — sourceControlAccess (C-SC-AD + C-SC-AG).
//
// Per [[the-method-testing]], the STP enumerates every way to demonstrate the
// component does NOT work. Written before/with the code; black-box, wire-level,
// against a FAKE GitHub (an httptest.Server serving canned GitHub REST + App-auth
// responses — framework-go-infrastructure-github/testinfra). Every case drives the
// RA's PUBLIC contract surface (the two interfaces) through the REAL satellite
// AppClient (so the JWT mint, the REST calls, and the error-kind mapping are all
// exercised); only the external GitHub boundary is faked. No live GitHub, ever.
//
// 2026-06-15 agentic-pivot re-cut: Contract-1 (ISourceControlLifecycle) is now 4
// ops. provisionProjectRepo → adoptProjectRepo (strict-adopt); +commitManagedFiles
// (SC-B; 2026-06-16 generalized from the single-file commitAgenticWorkflowFile to the
// managed-file bundle seat). U4/U12/U13/U13b re-cut to the new surface; U29–U40 added
// for adopt's decision table + the seat verb.
//
// 2026-06-16 PERMISSIVE-RESUME re-cut (I-RA-Δ): the founder relaxed adopt from
// strict-empty to PERMISSIVE — adopt SUCCEEDS regardless of repo content (a repo
// with a README/claude.yml or a prior .aiarch/ is fine). The emptiness probe + the
// RepoNotEmpty/Conflict hard-fail are GONE; only NotUnderInstallation remains. The
// old U30/U31 RepoNotEmpty assertions are FLIPPED to assert adopt-succeeds-with-
// content (the topic is still applied). The RESUME-from-.aiarch behavior is proven
// at the projectStateAccess layer (CreateProject resume) + the I-RA-Δ integration
// proof, not here.
//
//   CONTRACT-MISUSE / PRE-CONDITION (the guard fires before any wire call):
//     U1  New rejects a nil github client                                → ContractMisuse
//     U2  New rejects an empty account                                   → ContractMisuse
//     U3  GetInstallationToken rejects a zero RepoRef                    → ContractMisuse
//     U4  AdoptProjectRepo rejects an empty RepoName                     → ContractMisuse
//     U5  OpenBranch rejects empty branch / zero repo / empty cred       → ContractMisuse
//     U6  OpenPullRequest rejects head==base                            → ContractMisuse
//     U7  PR-rail verbs reject a zero PullRequestRef                     → ContractMisuse
//     U8  RepoRef round-trips opaquely (String/FromString/Equal); a
//         malformed RepoRef → ContractMisuse on use
//
//   LIFECYCLE — ISourceControlLifecycle (contract #1):
//     U9  InstallAuthorizeApp happy: discovers the installation; App-JWT Bearer
//     U10 InstallAuthorizeApp NOT-INSTALLED: account absent → NotFound
//     U11 InstallAuthorizeApp Auth: 401 on the App call → fwra.Auth (terminal)
//     U14 GetInstallationToken happy: mints; RepoCredential{Bytes,ExpiresAt}
//     U15 GetInstallationToken CACHING: 2nd call within validity served from cache
//     U16 GetInstallationToken RE-MINT past safety margin
//     U17 GetInstallationToken NotFound: unknown installation → NotFound
//
//   ADOPT — adoptProjectRepo permissive-resume policy (2026-06-16 founder ruling):
//     U12 adopt SUCCESS (under-install + EMPTY): tags aiarch-project topic + title;
//         no repo-create POST; returns the user-named RepoRef
//     U29 adopt NotUnderInstallation: repo 404 under the installation → NotFound,
//         NO topic mutation
//     U30 adopt SUCCESS WITH PRE-EXISTING CONTENT (a README/branch we did not author):
//         permissive adopt — SUCCEEDS, topic applied (was strict RepoNotEmpty/Conflict)
//     U13 adopt idempotent re-adopt (already ours) → SUCCESS, re-applies topic
//     U31 adopt SUCCESS even with a pre-existing .aiarch/ tree (the resume case at the
//         RA seam) — permissive adopt SUCCEEDS, topic applied (was strict RepoNotEmpty)
//     U13b ListProjectRepos discovery: name-as-identity (ProjectID == whole repo
//          name); topic is the SOLE filter (no aiarch- prefix fallback)
//
//   (writeActionsSecret REMOVED 2026-06-15 — aiarch does no secret management; the
//   CLAUDE_CODE_OAUTH_TOKEN is user-provisioned via the Claude Code GitHub App. The
//   old U32–U35 seal/plaintext-leak/upsert/auth cases are deleted.)
//
//   COMMIT-MANAGED-FILES — commitManagedFiles (SC-B, generalized 2026-06-16):
//     U36 seat-bundle happy: workflow + go.mod + method test → sequential PUTs →
//         CommitRef; all three committed bytes match
//     U37 overwrite-if-changed: differing content → a new commit
//     U38 byte-identical → no-op success (NO PUT), returns a CommitRef
//     U39 allowlist guard: a path off the managed-file allowlist rejects the WHOLE
//         bundle → ContractMisuse (no wire call), even bundled with a valid file
//     U40 guards: zero repo / zero cred / empty fileset / empty content → ContractMisuse;
//         a scaffold-root path (go.mod) is accepted
//
//   VALUE SEMANTICS:
//     U28 CheckState String; ReviewVerdict→event mapping via PostReview;
//         RepoCredential/Installation/Refs IsZero; CommitRef IsZero.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sc "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

const (
	testAccount = "acme"
	testAppSlug = "aiarch-app"
)

// newAccess builds an Access wired to the real satellite client pointed at the
// fake GitHub.
func newAccess(t *testing.T, fake *gh.FakeGitHub) *sc.Access {
	t.Helper()
	keyPEM, err := gh.GenerateAppKeyPEM()
	if err != nil {
		t.Fatalf("generate app key: %v", err)
	}
	client, err := fwgithub.NewAppClient("12345", keyPEM, fake.BaseURL())
	if err != nil {
		t.Fatalf("NewAppClient: %v", err)
	}
	access, err := sc.New(client, testAccount, testAppSlug, true)
	if err != nil {
		t.Fatalf("sc.New: %v", err)
	}
	return access
}

func kindOf(err error) fwra.Kind {
	var fe *fwra.Error
	if errors.As(err, &fe) {
		return fe.Kind
	}
	return fwra.Unknown
}

func requireKind(t *testing.T, err error, want fwra.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %v, got nil", want)
	}
	if got := kindOf(err); got != want {
		t.Fatalf("error kind = %v, want %v (err: %v)", got, want, err)
	}
}

// seedInstallation scripts the App-installations discovery so `account` resolves
// to installation id 99, plus the token mint.
func seedInstallation(fake *gh.FakeGitHub, account string) {
	fake.On("GET", "/app/installations", gh.JSON(200, []map[string]any{
		{"id": 99, "account": map[string]any{"login": account}},
	}))
	fake.On("POST", "/app/installations/99/access_tokens", gh.JSON(201, map[string]any{
		"token":      "ghs_faketoken",
		"expires_at": time.Now().Add(1 * time.Hour).UTC(),
	}))
}

// ---------------------------------------------------------------------------
// U1–U8  Contract-misuse / value semantics
// ---------------------------------------------------------------------------

func TestU1_NewRejectsNilClient(t *testing.T) {
	if _, err := sc.New(nil, testAccount, testAppSlug, true); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("New(nil) kind = %v, want ContractMisuse", kindOf(err))
	}
}

func TestU2_NewRejectsEmptyAccount(t *testing.T) {
	keyPEM, _ := gh.GenerateAppKeyPEM()
	client, _ := fwgithub.NewAppClient("1", keyPEM, "http://x")
	if _, err := sc.New(client, "   ", testAppSlug, true); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("New(empty account) kind = %v, want ContractMisuse", kindOf(err))
	}
}

func TestU3_GetInstallationTokenRejectsZeroRepo(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	_, err := a.GetInstallationToken(context.Background(), sc.RepoRef{})
	requireKind(t, err, fwra.ContractMisuse)
	if len(fake.Requests()) != 0 {
		t.Fatalf("guard should fire before any wire call; got %d requests", len(fake.Requests()))
	}
}

func TestU4_AdoptRejectsEmptyRepoName(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	_, err := a.AdoptProjectRepo(context.Background(), sc.RepoAdoptionSpec{RepoName: "", Account: testAccount}, "")
	requireKind(t, err, fwra.ContractMisuse)
	if len(fake.Requests()) != 0 {
		t.Fatalf("guard should fire before any wire call; got %d requests", len(fake.Requests()))
	}
}

func TestU5_OpenBranchGuards(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	cred := sc.RepoCredential{Bytes: []byte("t"), ExpiresAt: time.Now().Add(time.Hour)}
	repo := sc.RepoRefFromString(testAccount + "|" + testAccount + "/proj")

	if _, err := a.OpenBranch(context.Background(), sc.RepoRef{}, "b", cred, ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("zero repo: kind = %v", kindOf(err))
	}
	if _, err := a.OpenBranch(context.Background(), repo, "b", sc.RepoCredential{}, ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("empty cred: kind = %v", kindOf(err))
	}
	if _, err := a.OpenBranch(context.Background(), repo, "  ", cred, ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("empty branch: kind = %v", kindOf(err))
	}
}

func TestU6_OpenPullRequestRejectsHeadEqBase(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	cred := sc.RepoCredential{Bytes: []byte("t"), ExpiresAt: time.Now().Add(time.Hour)}
	repo := sc.RepoRefFromString(testAccount + "|" + testAccount + "/proj")
	_, err := a.OpenPullRequest(context.Background(), repo, sc.PullRequestSpec{Head: "main", Base: "main"}, cred, "")
	requireKind(t, err, fwra.ContractMisuse)
}

func TestU7_PRRailRejectsZeroPR(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	cred := sc.RepoCredential{Bytes: []byte("t"), ExpiresAt: time.Now().Add(time.Hour)}
	repo := sc.RepoRefFromString(testAccount + "|" + testAccount + "/proj")
	if _, err := a.GetPullRequestStatus(context.Background(), repo, sc.PullRequestRef{}, cred); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("status zero PR: kind = %v", kindOf(err))
	}
	if err := a.PostReview(context.Background(), repo, sc.PullRequestRef{}, sc.ReviewSubmission{}, cred, ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("postReview zero PR: kind = %v", kindOf(err))
	}
	if _, err := a.MergePullRequest(context.Background(), repo, sc.PullRequestRef{}, cred, ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("merge zero PR: kind = %v", kindOf(err))
	}
}

func TestU8_RefValueSemantics(t *testing.T) {
	r := sc.RepoRefFromString("acme|acme/my-project")
	if r.String() != "acme|acme/my-project" {
		t.Fatalf("RepoRef String round-trip failed: %q", r.String())
	}
	if !r.Equal(sc.RepoRefFromString("acme|acme/my-project")) {
		t.Fatalf("RepoRef Equal failed")
	}
	if (sc.RepoRef{}).IsZero() != true {
		t.Fatalf("zero RepoRef should be zero")
	}
	// malformed ref (no separator) → ContractMisuse on use
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	cred := sc.RepoCredential{Bytes: []byte("t"), ExpiresAt: time.Now().Add(time.Hour)}
	_, err := a.OpenBranch(context.Background(), sc.RepoRefFromString("no-separator"), "b", cred, "")
	requireKind(t, err, fwra.ContractMisuse)
}

// ---------------------------------------------------------------------------
// U9–U17  Lifecycle (install + token)
// ---------------------------------------------------------------------------

func TestU9_InstallAuthorizeAppHappy(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	a := newAccess(t, fake)

	inst, err := a.InstallAuthorizeApp(context.Background(), testAccount, "")
	if err != nil {
		t.Fatalf("InstallAuthorizeApp: %v", err)
	}
	if inst.IsZero() {
		t.Fatalf("expected a non-zero Installation")
	}
	req := findRequest(t, fake, "GET", "/app/installations")
	if !strings.HasPrefix(req.Auth, "Bearer ") {
		t.Fatalf("discovery should use an App-JWT Bearer; got %q", req.Auth)
	}
}

func TestU10_InstallAuthorizeAppNotInstalled(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("GET", "/app/installations", gh.JSON(200, []map[string]any{
		{"id": 7, "account": map[string]any{"login": "someone-else"}},
	}))
	a := newAccess(t, fake)
	_, err := a.InstallAuthorizeApp(context.Background(), testAccount, "")
	requireKind(t, err, fwra.NotFound)
}

func TestU11_InstallAuthorizeAppAuth(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("GET", "/app/installations", gh.Response{Status: 401, Body: `{"message":"bad jwt"}`})
	a := newAccess(t, fake)
	_, err := a.InstallAuthorizeApp(context.Background(), testAccount, "")
	requireKind(t, err, fwra.Auth)
	if err.(*fwra.Error).Retryable {
		t.Fatalf("Auth must be terminal (non-retryable)")
	}
}

func TestU14_GetInstallationTokenHappy(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	a := newAccess(t, fake)
	repo := sc.RepoRefFromString("acme|acme/my-project")

	cred, err := a.GetInstallationToken(context.Background(), repo)
	if err != nil {
		t.Fatalf("GetInstallationToken: %v", err)
	}
	if cred.IsZero() {
		t.Fatalf("expected a non-empty credential")
	}
	if cred.ExpiresAt.IsZero() {
		t.Fatalf("expected an ExpiresAt")
	}
}

func TestU15_GetInstallationTokenCaches(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	a := newAccess(t, fake)
	repo := sc.RepoRefFromString("acme|acme/my-project")

	if _, err := a.GetInstallationToken(context.Background(), repo); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	mintsAfterFirst := countRequests(fake, "POST", "/app/installations/99/access_tokens")
	if _, err := a.GetInstallationToken(context.Background(), repo); err != nil {
		t.Fatalf("second mint: %v", err)
	}
	mintsAfterSecond := countRequests(fake, "POST", "/app/installations/99/access_tokens")
	if mintsAfterSecond != mintsAfterFirst {
		t.Fatalf("second call should be served from the in-seam cache; mint count went %d → %d", mintsAfterFirst, mintsAfterSecond)
	}
}

func TestU16_GetInstallationTokenRemintNearExpiry(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("GET", "/app/installations", gh.JSON(200, []map[string]any{
		{"id": 99, "account": map[string]any{"login": testAccount}},
	}))
	fake.On("POST", "/app/installations/99/access_tokens", gh.JSON(201, map[string]any{
		"token": "ghs_short", "expires_at": time.Now().Add(5 * time.Second).UTC(),
	}))
	a := newAccess(t, fake)
	repo := sc.RepoRefFromString("acme|acme/my-project")

	if _, err := a.GetInstallationToken(context.Background(), repo); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	if _, err := a.GetInstallationToken(context.Background(), repo); err != nil {
		t.Fatalf("second mint: %v", err)
	}
	if got := countRequests(fake, "POST", "/app/installations/99/access_tokens"); got != 2 {
		t.Fatalf("near-expiry token must be re-minted, not cached; mint count = %d, want 2", got)
	}
}

func TestU17_GetInstallationTokenNotFound(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("GET", "/app/installations", gh.JSON(200, []map[string]any{})) // empty
	a := newAccess(t, fake)
	repo := sc.RepoRefFromString("acme|acme/my-project")
	_, err := a.GetInstallationToken(context.Background(), repo)
	requireKind(t, err, fwra.NotFound)
}

// ---------------------------------------------------------------------------
// U12, U29–U31, U13, U13b  Adopt — strict-adopt decision table
// ---------------------------------------------------------------------------

func TestU12_AdoptSuccessEmptyUnderInstall(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	fake.EnableRepoCatalog()
	// The user has supplied a fresh empty repo under the installation.
	fake.SeedEmptyRepo(testAccount, "my-project", true)
	a := newAccess(t, fake)

	ref, err := a.AdoptProjectRepo(context.Background(), sc.RepoAdoptionSpec{
		RepoName: "my-project", Account: testAccount, Title: "My Project",
	}, "wf:act")
	if err != nil {
		t.Fatalf("AdoptProjectRepo: %v", err)
	}
	if ref.IsZero() {
		t.Fatalf("expected a non-zero RepoRef")
	}
	// adopt must NOT create a repo (no POST /orgs/.../repos).
	if countRequests(fake, "POST", "/orgs/acme/repos") != 0 {
		t.Fatalf("adopt must not CREATE a repo")
	}
	// adopt tags the aiarch-project topic via PUT /repos/.../topics.
	topicsReq := findRequest(t, fake, "PUT", "/repos/acme/my-project/topics")
	if !strings.Contains(topicsReq.Body, "aiarch-project") {
		t.Fatalf("adopt should apply the aiarch-project topic; got %q", topicsReq.Body)
	}
}

func TestU29_AdoptNotUnderInstallation(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	fake.EnableRepoCatalog()
	// The repo is NOT seeded → GET /repos/acme/missing 404s under the installation.
	a := newAccess(t, fake)

	_, err := a.AdoptProjectRepo(context.Background(), sc.RepoAdoptionSpec{
		RepoName: "missing", Account: testAccount, Title: "Missing",
	}, "")
	requireKind(t, err, fwra.NotFound)
	if !strings.Contains(err.Error(), "NotUnderInstallation") {
		t.Fatalf("expected a NotUnderInstallation detail; got %v", err)
	}
	// NO mutation on the failure path.
	if countRequests(fake, "PUT", "/repos/acme/missing/topics") != 0 {
		t.Fatalf("a not-under-installation adopt must not mutate topics")
	}
}

// TestU30_AdoptSucceedsWithPreExistingContent proves the PERMISSIVE-RESUME adopt
// (founder ruling 2026-06-16, REPLACES strict-empty): a repo that already has content
// (a README/claude.yml — here modeled as a non-empty repo with branches + a foreign
// topic) under the installation ADOPTS SUCCESSFULLY and gets the aiarch-project topic.
// The old strict-empty RepoNotEmpty/Conflict hard-fail is GONE.
func TestU30_AdoptSucceedsWithPreExistingContent(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	fake.EnableRepoCatalog()
	// A repo with pre-existing content: it has a default branch + branches, no aiarch topic.
	fake.SeedRepo(testAccount, "has-stuff", "Pre-existing", []string{"misc"}, true)
	a := newAccess(t, fake)

	ref, err := a.AdoptProjectRepo(context.Background(), sc.RepoAdoptionSpec{
		RepoName: "has-stuff", Account: testAccount, Title: "Has Stuff",
	}, "")
	if err != nil {
		t.Fatalf("permissive adopt of a non-empty repo must SUCCEED, got: %v", err)
	}
	if ref.IsZero() {
		t.Fatalf("expected a non-zero RepoRef on permissive adopt")
	}
	// adopt still applies the aiarch-project topic — regardless of content.
	topicsReq := findRequest(t, fake, "PUT", "/repos/acme/has-stuff/topics")
	if !strings.Contains(topicsReq.Body, "aiarch-project") {
		t.Fatalf("permissive adopt should apply the aiarch-project topic; got %q", topicsReq.Body)
	}
}

func TestU13_AdoptIdempotentReadopt(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	fake.EnableRepoCatalog()
	// A repo WE already adopted (aiarch-project topic present) carrying only our init
	// (single default branch, no foreign .aiarch tree).
	fake.SeedAdoptedRepo(testAccount, "my-project", "My Project", true)
	a := newAccess(t, fake)

	ref, err := a.AdoptProjectRepo(context.Background(), sc.RepoAdoptionSpec{
		RepoName: "my-project", Account: testAccount, Title: "My Project",
	}, "")
	if err != nil {
		t.Fatalf("idempotent re-adopt must succeed, got: %v", err)
	}
	if ref.IsZero() {
		t.Fatalf("expected the existing RepoRef on idempotent re-adopt")
	}
	// The idempotent path still (re-)applies the topic (converged → effective no-op).
	topicsReq := findRequest(t, fake, "PUT", "/repos/acme/my-project/topics")
	if !strings.Contains(topicsReq.Body, "aiarch-project") {
		t.Fatalf("idempotent re-adopt should re-apply the aiarch-project topic; got %q", topicsReq.Body)
	}
}

// TestU31_AdoptSucceedsWithPreExistingAiarchTree proves permissive adopt over the
// RESUME shape AT THE RA SEAM: a repo that already carries a committed `.aiarch/` tree
// (a prior run's design state) ADOPTS SUCCESSFULLY — it is NOT a RepoNotEmpty hard-fail
// anymore. "If the repo already has .aiarch/, just re-initialize the project from its
// current progress" — adopt succeeds here; the actual state re-load (resume) is the
// projectStateAccess.CreateProject layer's job, proven separately.
func TestU31_AdoptSucceedsWithPreExistingAiarchTree(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	fake.EnableRepoCatalog()
	// A repo with a pre-existing .aiarch/ tree (a prior run's committed design state).
	fake.SeedRepo(testAccount, "my-project", "My Project", []string{"aiarch-project"}, true)
	fake.SeedRepoFile(testAccount, "my-project", ".aiarch", []byte("prior-state"))
	a := newAccess(t, fake)

	ref, err := a.AdoptProjectRepo(context.Background(), sc.RepoAdoptionSpec{
		RepoName: "my-project", Account: testAccount, Title: "My Project",
	}, "")
	if err != nil {
		t.Fatalf("permissive adopt of a repo with a pre-existing .aiarch/ must SUCCEED (resume), got: %v", err)
	}
	if ref.IsZero() {
		t.Fatalf("expected a non-zero RepoRef on permissive resume-adopt")
	}
	// adopt re-applies the aiarch-project topic (converged → effective no-op).
	topicsReq := findRequest(t, fake, "PUT", "/repos/acme/my-project/topics")
	if !strings.Contains(topicsReq.Body, "aiarch-project") {
		t.Fatalf("permissive resume-adopt should (re-)apply the aiarch-project topic; got %q", topicsReq.Body)
	}
}

// TestU13b_ListProjectReposDiscovery proves name-as-identity: two adopted project
// repos are returned by ListProjectRepos, filtered to the aiarch-project topic
// (the SOLE signal — no aiarch- prefix fallback), each carrying its title and with
// ProjectID() == the WHOLE (user-supplied) repo name.
func TestU13b_ListProjectReposDiscovery(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	fake.EnableRepoCatalog()
	// A non-aiarch repo (no topic) must NOT appear; an aiarch-named-but-untopiced repo
	// must ALSO NOT appear (proving the prefix fallback is gone).
	fake.SeedRepo(testAccount, "some-other-repo", "Not ours", []string{"misc"}, true)
	fake.SeedRepo(testAccount, "aiarch-legacy", "No topic", nil, true)
	// Two genuinely-adopted repos (user-named, aiarch-project topic).
	fake.SeedAdoptedRepo(testAccount, "alpha", "Project Alpha", true)
	fake.SeedAdoptedRepo(testAccount, "beta-svc", "Project Beta", true)
	a := newAccess(t, fake)

	refs, err := a.ListProjectRepos(context.Background(), testAccount)
	if err != nil {
		t.Fatalf("ListProjectRepos: %v", err)
	}
	got := map[string]string{}
	for _, r := range refs {
		got[r.ProjectID()] = r.Description
	}
	if len(got) != 2 {
		t.Fatalf("ListProjectRepos returned %d aiarch repos, want 2 (topic-only filter): %+v", len(got), refs)
	}
	if got["alpha"] != "Project Alpha" || got["beta-svc"] != "Project Beta" {
		t.Fatalf("name-as-identity ProjectID()/title wrong: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// adoptedFixture — shared by the commitManagedFiles cases (U36–U40).
// (The writeActionsSecret cases U32–U35 were removed 2026-06-15: aiarch does no
// secret management; the CLAUDE_CODE_OAUTH_TOKEN is user-provisioned via the Claude
// Code GitHub App.)
// ---------------------------------------------------------------------------

func adoptedFixture(t *testing.T, repoName string) (*gh.FakeGitHub, *sc.Access, sc.RepoRef, sc.RepoCredential) {
	t.Helper()
	fake := gh.Start()
	fake.EnableRepoCatalog()
	fake.SeedAdoptedRepo(testAccount, repoName, "Title", true)
	a := newAccess(t, fake)
	repo := sc.RepoRefFromString(testAccount + "|" + testAccount + "/" + repoName)
	cred := sc.RepoCredential{Bytes: []byte("ghs_inst"), ExpiresAt: time.Now().Add(time.Hour)}
	return fake, a, repo, cred
}

// ---------------------------------------------------------------------------
// U36–U40  commitManagedFiles (SC-B, generalized 2026-06-16 from the single-file
// workflow seat to the managed-file bundle: design workflow + go-test gate)
// ---------------------------------------------------------------------------

func TestU36_CommitManagedFilesSeatsBundle(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	wf := []byte("name: aiarch-design\non: workflow_dispatch\n")
	gomod := []byte("module github.com/acme/alpha\n\ngo 1.25.0\n")
	mtest := []byte("package method_test\n")

	ref, err := a.CommitManagedFiles(context.Background(), repo, []sc.ManagedFile{
		{Path: ".github/workflows/aiarch-design.yml", Content: wf},
		{Path: "go.mod", Content: gomod},
		{Path: "aiarch_method_test.go", Content: mtest},
	}, cred, "wf:wf")
	if err != nil {
		t.Fatalf("CommitManagedFiles: %v", err)
	}
	if ref.IsZero() {
		t.Fatalf("expected a non-zero CommitRef")
	}
	// All three files landed.
	for path, want := range map[string][]byte{
		".github/workflows/aiarch-design.yml": wf,
		"go.mod":                              gomod,
		"aiarch_method_test.go":               mtest,
	} {
		stored, ok := fake.RepoFile(testAccount, "alpha", path)
		if !ok || string(stored) != string(want) {
			t.Fatalf("file %q mismatch: stored=%q want=%q", path, stored, want)
		}
	}
}

func TestU37_CommitManagedFilesOverwriteIfChanged(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	path := ".github/workflows/aiarch-design.yml"
	fake.SeedRepoFile(testAccount, "alpha", path, []byte("old content"))

	ref, err := a.CommitManagedFiles(context.Background(), repo, []sc.ManagedFile{
		{Path: path, Content: []byte("new content")},
	}, cred, "")
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if ref.IsZero() {
		t.Fatalf("expected a CommitRef on a changed write")
	}
	// A PUT was issued (the content changed).
	if countRequests(fake, "PUT", "/repos/acme/alpha/contents/"+path) != 1 {
		t.Fatalf("a changed file must issue exactly one contents PUT")
	}
	stored, _ := fake.RepoFile(testAccount, "alpha", path)
	if string(stored) != "new content" {
		t.Fatalf("stored content not overwritten: %q", stored)
	}
}

func TestU38_CommitManagedFilesByteIdenticalNoOp(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	path := ".github/workflows/aiarch-design.yml"
	content := []byte("identical bytes")
	fake.SeedRepoFile(testAccount, "alpha", path, content)

	ref, err := a.CommitManagedFiles(context.Background(), repo, []sc.ManagedFile{
		{Path: path, Content: content},
	}, cred, "")
	if err != nil {
		t.Fatalf("byte-identical commit: %v", err)
	}
	if ref.IsZero() {
		t.Fatalf("a no-op commit should still return the existing tip CommitRef")
	}
	// No PUT — byte-identical short-circuits.
	if countRequests(fake, "PUT", "/repos/acme/alpha/contents/"+path) != 0 {
		t.Fatalf("a byte-identical file must NOT issue a contents PUT (no empty commit)")
	}
}

func TestU39_CommitManagedFilesRejectsPathOffAllowlist(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	preCount := len(fake.Requests())

	// A non-allowlisted path (not under .github/workflows/ nor a scaffold root) must
	// reject the WHOLE bundle before any wire call — even when bundled with a valid file.
	_, err := a.CommitManagedFiles(context.Background(), repo, []sc.ManagedFile{
		{Path: ".github/workflows/aiarch-design.yml", Content: []byte("ok")},
		{Path: "src/main.go", Content: []byte("package main")},
	}, cred, "")
	requireKind(t, err, fwra.ContractMisuse)
	if len(fake.Requests()) != preCount {
		t.Fatalf("the allowlist guard must fire before any wire call; requests went %d → %d", preCount, len(fake.Requests()))
	}
}

func TestU40_CommitManagedFilesGuards(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	good := []sc.ManagedFile{{Path: ".github/workflows/x.yml", Content: []byte("c")}}

	if _, err := a.CommitManagedFiles(context.Background(), sc.RepoRef{}, good, cred, ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("zero repo: kind = %v", kindOf(err))
	}
	if _, err := a.CommitManagedFiles(context.Background(), repo, good, sc.RepoCredential{}, ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("zero cred: kind = %v", kindOf(err))
	}
	if _, err := a.CommitManagedFiles(context.Background(), repo, nil, cred, ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("empty fileset: kind = %v", kindOf(err))
	}
	if _, err := a.CommitManagedFiles(context.Background(), repo, []sc.ManagedFile{{Path: ".github/workflows/x.yml", Content: nil}}, cred, ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("empty content: kind = %v", kindOf(err))
	}
	// A scaffold-root path (go.mod) is on the allowlist.
	if _, err := a.CommitManagedFiles(context.Background(), repo, []sc.ManagedFile{{Path: "go.mod", Content: []byte("module x\n")}}, cred, ""); err != nil {
		t.Fatalf("go.mod is a scaffold-root managed file; should be accepted: %v", err)
	}
}

// ---------------------------------------------------------------------------
// U18–U27  PR rail
// ---------------------------------------------------------------------------

func railFixture(t *testing.T) (*gh.FakeGitHub, *sc.Access, sc.RepoRef, sc.RepoCredential) {
	t.Helper()
	fake := gh.Start()
	a := newAccess(t, fake)
	repo := sc.RepoRefFromString("acme|acme/my-project")
	cred := sc.RepoCredential{Bytes: []byte("ghs_x"), ExpiresAt: time.Now().Add(time.Hour)}
	return fake, a, repo, cred
}

func TestU18_OpenBranchHappy(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("GET", "/repos/acme/my-project/git/ref/heads/main", gh.JSON(200, map[string]any{
		"ref": "refs/heads/main", "object": map[string]any{"sha": "deadbeef"},
	}))
	fake.On("POST", "/repos/acme/my-project/git/refs", gh.JSON(201, map[string]any{"ref": "refs/heads/act-1"}))

	br, err := a.OpenBranch(context.Background(), repo, "act-1", cred, "wf:act-1")
	if err != nil {
		t.Fatalf("OpenBranch: %v", err)
	}
	if br.IsZero() {
		t.Fatalf("expected a BranchRef")
	}
	req := findRequest(t, fake, "POST", "/repos/acme/my-project/git/refs")
	if !strings.HasPrefix(req.Auth, "token ") {
		t.Fatalf("branch create should use the threaded installation token; got %q", req.Auth)
	}
}

func TestU19_OpenBranchIdempotent(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("GET", "/repos/acme/my-project/git/ref/heads/main", gh.JSON(200, map[string]any{
		"object": map[string]any{"sha": "deadbeef"},
	}))
	fake.On("POST", "/repos/acme/my-project/git/refs", gh.Response{Status: 422, Body: `{"message":"Reference already exists"}`})

	if _, err := a.OpenBranch(context.Background(), repo, "act-1", cred, ""); err != nil {
		t.Fatalf("branch-exists must map to success, got: %v", err)
	}
}

func TestU20_OpenPullRequestHappy(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("POST", "/repos/acme/my-project/pulls", gh.JSON(201, map[string]any{"number": 42, "state": "open"}))

	pr, err := a.OpenPullRequest(context.Background(), repo, sc.PullRequestSpec{Head: "act-1", Base: "main", Title: "T"}, cred, "")
	if err != nil {
		t.Fatalf("OpenPullRequest: %v", err)
	}
	if pr.String() != "42" {
		t.Fatalf("PullRequestRef = %q, want 42", pr.String())
	}
}

func TestU21_OpenPullRequestIdempotent(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("POST", "/repos/acme/my-project/pulls", gh.Response{Status: 422, Body: `{"message":"A pull request already exists"}`})
	fake.OnPrefix("GET", "/repos/acme/my-project/pulls", gh.JSON(200, []map[string]any{{"number": 42, "state": "open"}}))

	pr, err := a.OpenPullRequest(context.Background(), repo, sc.PullRequestSpec{Head: "act-1", Base: "main"}, cred, "")
	if err != nil {
		t.Fatalf("existing-PR must map to success, got: %v", err)
	}
	if pr.String() != "42" {
		t.Fatalf("expected the existing PR #42, got %q", pr.String())
	}
}

func TestU22_GetPullRequestStatusFolds(t *testing.T) {
	tests := []struct {
		name       string
		checkRuns  []map[string]any
		wantRollup sc.CheckState
	}{
		{"success", []map[string]any{{"status": "completed", "conclusion": "success"}}, sc.CheckSuccess},
		{"failure", []map[string]any{{"status": "completed", "conclusion": "failure"}}, sc.CheckFailure},
		{"pending", []map[string]any{{"status": "in_progress", "conclusion": ""}}, sc.CheckPending},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake, a, repo, cred := railFixture(t)
			defer fake.Close()
			fake.On("GET", "/repos/acme/my-project/pulls/42", gh.JSON(200, map[string]any{
				"number": 42, "mergeable": true, "head": map[string]any{"sha": "c0ffee"},
			}))
			fake.On("GET", "/repos/acme/my-project/commits/c0ffee/check-runs", gh.JSON(200, map[string]any{
				"total_count": len(tc.checkRuns), "check_runs": tc.checkRuns,
			}))
			fake.On("GET", "/repos/acme/my-project/pulls/42/reviews", gh.JSON(200, []map[string]any{
				{"state": "APPROVED"}, {"state": "COMMENTED"},
			}))
			st, err := a.GetPullRequestStatus(context.Background(), repo, sc.PullRequestRefFromString("42"), cred)
			if err != nil {
				t.Fatalf("GetPullRequestStatus: %v", err)
			}
			if st.CheckRollup != tc.wantRollup {
				t.Fatalf("rollup = %v, want %v", st.CheckRollup, tc.wantRollup)
			}
			if st.ApprovalCount != 1 {
				t.Fatalf("approval count = %d, want 1", st.ApprovalCount)
			}
			if !st.Mergeable {
				t.Fatalf("expected mergeable")
			}
		})
	}
}

func TestU23_PostReviewApprove(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("POST", "/repos/acme/my-project/pulls/42/reviews", gh.JSON(200, map[string]any{"id": 1, "state": "APPROVED"}))

	if err := a.PostReview(context.Background(), repo, sc.PullRequestRefFromString("42"), sc.ReviewSubmission{Verdict: sc.ReviewApprove, Body: "+1"}, cred, ""); err != nil {
		t.Fatalf("PostReview: %v", err)
	}
	req := findRequest(t, fake, "POST", "/repos/acme/my-project/pulls/42/reviews")
	if !strings.Contains(req.Body, `"APPROVE"`) {
		t.Fatalf("approve review should carry event=APPROVE; got %q", req.Body)
	}
}

func TestU24_MergePullRequestHappy(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("GET", "/repos/acme/my-project/pulls/42", gh.JSON(200, map[string]any{"number": 42, "merged": false}))
	fake.On("PUT", "/repos/acme/my-project/pulls/42/merge", gh.JSON(200, map[string]any{"sha": "mergedsha", "merged": true}))

	res, err := a.MergePullRequest(context.Background(), repo, sc.PullRequestRefFromString("42"), cred, "")
	if err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	if !res.Merged || res.Commit != "mergedsha" {
		t.Fatalf("MergeResult = %+v, want merged with commit mergedsha", res)
	}
}

func TestU25_MergePullRequestAlreadyMerged(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("GET", "/repos/acme/my-project/pulls/42", gh.JSON(200, map[string]any{"number": 42, "merged": true}))

	res, err := a.MergePullRequest(context.Background(), repo, sc.PullRequestRefFromString("42"), cred, "")
	if err != nil {
		t.Fatalf("already-merged must map to success, got: %v", err)
	}
	if !res.Merged {
		t.Fatalf("expected Merged=true on already-merged")
	}
	if countRequests(fake, "PUT", "/repos/acme/my-project/pulls/42/merge") != 0 {
		t.Fatalf("already-merged path must not issue the merge PUT")
	}
}

func TestU26_MergePullRequestNotMergeable(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("GET", "/repos/acme/my-project/pulls/42", gh.JSON(200, map[string]any{"number": 42, "merged": false}))
	fake.On("PUT", "/repos/acme/my-project/pulls/42/merge", gh.Response{Status: 405, Body: `{"message":"Pull Request is not mergeable"}`})

	_, err := a.MergePullRequest(context.Background(), repo, sc.PullRequestRefFromString("42"), cred, "")
	requireKind(t, err, fwra.Conflict)
}

func TestU27_ConfigureBranchProtection(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("PUT", "/repos/acme/my-project/branches/main/protection", gh.JSON(200, map[string]any{"url": "x"}))

	if err := a.ConfigureBranchProtection(context.Background(), repo, cred, ""); err != nil {
		t.Fatalf("ConfigureBranchProtection: %v", err)
	}
	req := findRequest(t, fake, "PUT", "/repos/acme/my-project/branches/main/protection")
	if !strings.Contains(req.Body, testAppSlug) {
		t.Fatalf("branch protection should restrict/bypass the App slug; got %q", req.Body)
	}
}

// ---------------------------------------------------------------------------
// U28  Value semantics
// ---------------------------------------------------------------------------

func TestU28_ValueSemantics(t *testing.T) {
	if sc.CheckSuccess.String() != "Success" || sc.CheckFailure.String() != "Failure" || sc.CheckPending.String() != "Pending" {
		t.Fatalf("CheckState String mapping wrong")
	}
	if !(sc.RepoCredential{}).IsZero() {
		t.Fatalf("empty credential should be zero")
	}
	if !(sc.Installation{}).IsZero() {
		t.Fatalf("empty installation should be zero")
	}
	if !(sc.PullRequestRef{}).IsZero() || !(sc.BranchRef{}).IsZero() {
		t.Fatalf("empty refs should be zero")
	}
	if !(sc.CommitRef{}).IsZero() {
		t.Fatalf("empty CommitRef should be zero")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func findRequest(t *testing.T, fake *gh.FakeGitHub, method, path string) gh.RecordedRequest {
	t.Helper()
	for _, r := range fake.Requests() {
		if r.Method == method && r.Path == path {
			return r
		}
	}
	t.Fatalf("no %s %s request recorded; got %+v", method, path, fake.Requests())
	return gh.RecordedRequest{}
}

func countRequests(fake *gh.FakeGitHub, method, path string) int {
	n := 0
	for _, r := range fake.Requests() {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}
