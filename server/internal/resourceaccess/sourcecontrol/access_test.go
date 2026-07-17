package sourcecontrol

// access_test.go — the single test file for sourceControlAccess (FileLayout
// standard). It merges the former sourcecontrol_test.go (package
// sourcecontrol_test, the black-box STP suite) and agenticdesign_test.go
// (package sourcecontrol, white-box structural tests over the embedded DESIGN
// workflow asset). The former black-box tests deliberately exercise ONLY the
// exported surface — the external-view discipline is preserved by CONVENTION
// after the single-test-file standard removed the separate _test package; do
// not reach for unexported identifiers from those tests.

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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"gopkg.in/yaml.v3"
)

const (
	testAccount = "acme"
	stpAppSlug  = "aiarch-app"
)

// newAccess builds an Access wired to the real satellite client pointed at the
// fake GitHub.
func newAccess(t *testing.T, fake *gh.FakeGitHub) SourceControlAccess {
	t.Helper()
	keyPEM, err := gh.GenerateAppKeyPEM()
	if err != nil {
		t.Fatalf("generate app key: %v", err)
	}
	client, err := fwgithub.NewAppClient("12345", keyPEM, fake.BaseURL())
	if err != nil {
		t.Fatalf("NewAppClient: %v", err)
	}
	access, err := NewGitHubSourceControlAccess(client, testAccount, stpAppSlug, true)
	if err != nil {
		t.Fatalf("sc.New: %v", err)
	}
	return access
}

// rc wraps a plain context as the ResourceAccess call Context every contract
// method now takes as its first param (the idempotency key rides on it).
func rc(ctx context.Context) fwra.Context { return fwra.Context{Context: ctx} }

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
	if _, err := NewGitHubSourceControlAccess(nil, testAccount, stpAppSlug, true); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("New(nil) kind = %v, want ContractMisuse", kindOf(err))
	}
}

func TestU2_NewRejectsEmptyAccount(t *testing.T) {
	keyPEM, _ := gh.GenerateAppKeyPEM()
	client, _ := fwgithub.NewAppClient("1", keyPEM, "http://x")
	if _, err := NewGitHubSourceControlAccess(client, "   ", stpAppSlug, true); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("New(empty account) kind = %v, want ContractMisuse", kindOf(err))
	}
}

func TestU3_GetInstallationTokenRejectsZeroRepo(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	_, err := a.GetInstallationToken(rc(context.Background()), RepoRef(""))
	requireKind(t, err, fwra.ContractMisuse)
	if len(fake.Requests()) != 0 {
		t.Fatalf("guard should fire before any wire call; got %d requests", len(fake.Requests()))
	}
}

func TestU4_AdoptRejectsEmptyRepoName(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	_, err := a.AdoptProjectRepo(rc(context.Background()), RepoAdoptionSpec{RepoName: "", Account: testAccount})
	requireKind(t, err, fwra.ContractMisuse)
	if len(fake.Requests()) != 0 {
		t.Fatalf("guard should fire before any wire call; got %d requests", len(fake.Requests()))
	}
}

func TestU5_OpenBranchGuards(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	cred := RepoCredential{Bytes: []byte("t"), ExpiresAt: time.Now().Add(time.Hour)}
	repo := RepoRefFromString(testAccount + "|" + testAccount + "/proj")

	if _, err := a.OpenBranch(rc(context.Background()), RepoRef(""), "b", cred); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("zero repo: kind = %v", kindOf(err))
	}
	if _, err := a.OpenBranch(rc(context.Background()), repo, "b", RepoCredential{}); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("empty cred: kind = %v", kindOf(err))
	}
	if _, err := a.OpenBranch(rc(context.Background()), repo, "  ", cred); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("empty branch: kind = %v", kindOf(err))
	}
}

func TestU6_OpenPullRequestRejectsHeadEqBase(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	cred := RepoCredential{Bytes: []byte("t"), ExpiresAt: time.Now().Add(time.Hour)}
	repo := RepoRefFromString(testAccount + "|" + testAccount + "/proj")
	_, err := a.OpenPullRequest(rc(context.Background()), repo, PullRequestSpec{Head: "main", Base: "main"}, cred)
	requireKind(t, err, fwra.ContractMisuse)
}

func TestU7_PRRailRejectsZeroPR(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	cred := RepoCredential{Bytes: []byte("t"), ExpiresAt: time.Now().Add(time.Hour)}
	repo := RepoRefFromString(testAccount + "|" + testAccount + "/proj")
	if _, err := a.GetPullRequestStatus(rc(context.Background()), repo, PullRequestRef(""), cred); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("status zero PR: kind = %v", kindOf(err))
	}
	if err := a.PostReview(rc(context.Background()), repo, PullRequestRef(""), ReviewSubmission{}, cred); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("postReview zero PR: kind = %v", kindOf(err))
	}
	if _, err := a.MergePullRequest(rc(context.Background()), repo, PullRequestRef(""), cred); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("merge zero PR: kind = %v", kindOf(err))
	}
}

func TestU8_RefValueSemantics(t *testing.T) {
	r := RepoRefFromString("acme|acme/my-project")
	if RepoRefString(r) != "acme|acme/my-project" {
		t.Fatalf("RepoRef String round-trip failed: %q", RepoRefString(r))
	}
	if !RepoRefEqual(r, RepoRefFromString("acme|acme/my-project")) {
		t.Fatalf("RepoRef Equal failed")
	}
	if RepoRefIsZero(RepoRef("")) != true {
		t.Fatalf("zero RepoRef should be zero")
	}
	// malformed ref (no separator) → ContractMisuse on use
	fake := gh.Start()
	defer fake.Close()
	a := newAccess(t, fake)
	cred := RepoCredential{Bytes: []byte("t"), ExpiresAt: time.Now().Add(time.Hour)}
	_, err := a.OpenBranch(rc(context.Background()), RepoRefFromString("no-separator"), "b", cred)
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

	inst, err := a.InstallAuthorizeApp(rc(context.Background()), testAccount)
	if err != nil {
		t.Fatalf("InstallAuthorizeApp: %v", err)
	}
	if InstallationIsZero(inst) {
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
	_, err := a.InstallAuthorizeApp(rc(context.Background()), testAccount)
	requireKind(t, err, fwra.NotFound)
}

func TestU11_InstallAuthorizeAppAuth(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("GET", "/app/installations", gh.Response{Status: 401, Body: `{"message":"bad jwt"}`})
	a := newAccess(t, fake)
	_, err := a.InstallAuthorizeApp(rc(context.Background()), testAccount)
	requireKind(t, err, fwra.Auth)
	var raErr *fwra.Error
	if errors.As(err, &raErr) && raErr.Retryable {
		t.Fatalf("Auth must be terminal (non-retryable)")
	}
}

func TestU14_GetInstallationTokenHappy(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	a := newAccess(t, fake)
	repo := RepoRefFromString("acme|acme/my-project")

	cred, err := a.GetInstallationToken(rc(context.Background()), repo)
	if err != nil {
		t.Fatalf("GetInstallationToken: %v", err)
	}
	if RepoCredentialIsZero(cred) {
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
	repo := RepoRefFromString("acme|acme/my-project")

	if _, err := a.GetInstallationToken(rc(context.Background()), repo); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	mintsAfterFirst := countRequests(fake, "POST", "/app/installations/99/access_tokens")
	if _, err := a.GetInstallationToken(rc(context.Background()), repo); err != nil {
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
	repo := RepoRefFromString("acme|acme/my-project")

	if _, err := a.GetInstallationToken(rc(context.Background()), repo); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	if _, err := a.GetInstallationToken(rc(context.Background()), repo); err != nil {
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
	repo := RepoRefFromString("acme|acme/my-project")
	_, err := a.GetInstallationToken(rc(context.Background()), repo)
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

	ref, err := a.AdoptProjectRepo(rc(context.Background()), RepoAdoptionSpec{
		RepoName: "my-project", Account: testAccount, Title: "My Project",
	})
	if err != nil {
		t.Fatalf("AdoptProjectRepo: %v", err)
	}
	if RepoRefIsZero(ref) {
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

	_, err := a.AdoptProjectRepo(rc(context.Background()), RepoAdoptionSpec{
		RepoName: "missing", Account: testAccount, Title: "Missing",
	})
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

	ref, err := a.AdoptProjectRepo(rc(context.Background()), RepoAdoptionSpec{
		RepoName: "has-stuff", Account: testAccount, Title: "Has Stuff",
	})
	if err != nil {
		t.Fatalf("permissive adopt of a non-empty repo must SUCCEED, got: %v", err)
	}
	if RepoRefIsZero(ref) {
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

	ref, err := a.AdoptProjectRepo(rc(context.Background()), RepoAdoptionSpec{
		RepoName: "my-project", Account: testAccount, Title: "My Project",
	})
	if err != nil {
		t.Fatalf("idempotent re-adopt must succeed, got: %v", err)
	}
	if RepoRefIsZero(ref) {
		t.Fatalf("expected the existing RepoRef on idempotent re-adopt")
	}
	// The idempotent path still (re-)applies the topic (converged → effective no-op).
	topicsReq := findRequest(t, fake, "PUT", "/repos/acme/my-project/topics")
	if !strings.Contains(topicsReq.Body, "aiarch-project") {
		t.Fatalf("idempotent re-adopt should re-apply the aiarch-project topic; got %q", topicsReq.Body)
	}
}

// TestU41_AdoptBestEffortTopicOnPermissionDenied proves the 2026-07-04 founder ruling:
// the App is NOT required to hold `administration`, so the topic PUT can 403
// (→ fwra.Auth) on a contents+metadata-only installation. That permission-denied MUST
// NOT sink the adoption — the repo is reachable, so adopt SUCCEEDS (WARN-and-proceed)
// and returns a live RepoRef. (The live failure this fixes: a valid installation +
// reachable repo, but SetRepoTopics 403 → the whole CreateProject 503'd.)
func TestU41_AdoptBestEffortTopicOnPermissionDenied(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	fake.EnableRepoCatalog()
	// Reachable repo (GET /repos/... served from the catalog) ...
	fake.SeedRepo(testAccount, "no-admin", "No admin perm", nil, true)
	// ... but the topic PUT is forbidden (App lacks administration:write). A scripted
	// route takes precedence over the stateful catalog, so this forces the 403 → Auth.
	fake.On("PUT", "/repos/acme/no-admin/topics", gh.Response{Status: 403, Body: `{"message":"Resource not accessible by integration"}`})
	a := newAccess(t, fake)

	ref, err := a.AdoptProjectRepo(rc(context.Background()), RepoAdoptionSpec{
		RepoName: "no-admin", Account: testAccount, Title: "No Admin",
	})
	if err != nil {
		t.Fatalf("permission-denied topic tagging must NOT fail adoption (best-effort); got: %v", err)
	}
	if RepoRefIsZero(ref) {
		t.Fatalf("expected a non-zero RepoRef even when topic tagging was skipped")
	}
	// The attempt was made (and degraded), not silently skipped: the PUT was issued.
	if countRequests(fake, "PUT", "/repos/acme/no-admin/topics") != 1 {
		t.Fatalf("adopt should still ATTEMPT the topic PUT once before degrading")
	}
}

// TestU42_AdoptStillFailsOnTransientTopicError proves the degrade is NARROW: a
// transient/infra failure of the topic PUT (here a 500 → fwra.Transient) is a REAL
// outage and stays a HARD error — it is NOT masked as a best-effort skip. Only the
// Auth (permission) kind degrades.
func TestU42_AdoptStillFailsOnTransientTopicError(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	seedInstallation(fake, testAccount)
	fake.EnableRepoCatalog()
	fake.SeedRepo(testAccount, "flaky", "Flaky", nil, true)
	// A GitHub 5xx → fwra.Transient. This must propagate, not degrade.
	fake.On("PUT", "/repos/acme/flaky/topics", gh.Response{Status: 500, Body: `{"message":"server error"}`})
	a := newAccess(t, fake)

	_, err := a.AdoptProjectRepo(rc(context.Background()), RepoAdoptionSpec{
		RepoName: "flaky", Account: testAccount, Title: "Flaky",
	})
	requireKind(t, err, fwra.Transient)
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

	ref, err := a.AdoptProjectRepo(rc(context.Background()), RepoAdoptionSpec{
		RepoName: "my-project", Account: testAccount, Title: "My Project",
	})
	if err != nil {
		t.Fatalf("permissive adopt of a repo with a pre-existing .aiarch/ must SUCCEED (resume), got: %v", err)
	}
	if RepoRefIsZero(ref) {
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
	a := newAccess(t, fake).(SourceControlCatalogAccess)

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

func adoptedFixture(t *testing.T, repoName string) (*gh.FakeGitHub, SourceControlAccess, RepoRef, RepoCredential) {
	t.Helper()
	fake := gh.Start()
	fake.EnableRepoCatalog()
	fake.SeedAdoptedRepo(testAccount, repoName, "Title", true)
	a := newAccess(t, fake)
	repo := RepoRefFromString(testAccount + "|" + testAccount + "/" + repoName)
	cred := RepoCredential{Bytes: []byte("ghs_inst"), ExpiresAt: time.Now().Add(time.Hour)}
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

	ref, err := a.CommitManagedFiles(rc(context.Background()), repo, []ManagedFile{
		{Path: ".github/workflows/aiarch-design.yml", Content: wf},
		{Path: "go.mod", Content: gomod},
		{Path: "aiarch_method_test.go", Content: mtest},
	}, cred)
	if err != nil {
		t.Fatalf("CommitManagedFiles: %v", err)
	}
	if CommitRefIsZero(ref) {
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

	ref, err := a.CommitManagedFiles(rc(context.Background()), repo, []ManagedFile{
		{Path: path, Content: []byte("new content")},
	}, cred)
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if CommitRefIsZero(ref) {
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

	ref, err := a.CommitManagedFiles(rc(context.Background()), repo, []ManagedFile{
		{Path: path, Content: content},
	}, cred)
	if err != nil {
		t.Fatalf("byte-identical commit: %v", err)
	}
	if CommitRefIsZero(ref) {
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
	_, err := a.CommitManagedFiles(rc(context.Background()), repo, []ManagedFile{
		{Path: ".github/workflows/aiarch-design.yml", Content: []byte("ok")},
		{Path: "src/main.go", Content: []byte("package main")},
	}, cred)
	requireKind(t, err, fwra.ContractMisuse)
	if len(fake.Requests()) != preCount {
		t.Fatalf("the allowlist guard must fire before any wire call; requests went %d → %d", preCount, len(fake.Requests()))
	}
}

func TestU40_CommitManagedFilesGuards(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	good := []ManagedFile{{Path: ".github/workflows/x.yml", Content: []byte("c")}}

	if _, err := a.CommitManagedFiles(rc(context.Background()), RepoRef(""), good, cred); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("zero repo: kind = %v", kindOf(err))
	}
	if _, err := a.CommitManagedFiles(rc(context.Background()), repo, good, RepoCredential{}); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("zero cred: kind = %v", kindOf(err))
	}
	if _, err := a.CommitManagedFiles(rc(context.Background()), repo, nil, cred); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("empty fileset: kind = %v", kindOf(err))
	}
	if _, err := a.CommitManagedFiles(rc(context.Background()), repo, []ManagedFile{{Path: ".github/workflows/x.yml", Content: nil}}, cred); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("empty content: kind = %v", kindOf(err))
	}
	// A scaffold-root path (go.mod) is on the allowlist.
	if _, err := a.CommitManagedFiles(rc(context.Background()), repo, []ManagedFile{{Path: "go.mod", Content: []byte("module x\n")}}, cred); err != nil {
		t.Fatalf("go.mod is a scaffold-root managed file; should be accepted: %v", err)
	}
}

// ---------------------------------------------------------------------------
// U18–U27  PR rail
// ---------------------------------------------------------------------------

func railFixture(t *testing.T) (*gh.FakeGitHub, SourceControlAccess, RepoRef, RepoCredential) {
	t.Helper()
	fake := gh.Start()
	a := newAccess(t, fake)
	repo := RepoRefFromString("acme|acme/my-project")
	cred := RepoCredential{Bytes: []byte("ghs_x"), ExpiresAt: time.Now().Add(time.Hour)}
	return fake, a, repo, cred
}

func TestU18_OpenBranchHappy(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("GET", "/repos/acme/my-project/git/ref/heads/main", gh.JSON(200, map[string]any{
		"ref": "refs/heads/main", "object": map[string]any{"sha": "deadbeef"},
	}))
	fake.On("POST", "/repos/acme/my-project/git/refs", gh.JSON(201, map[string]any{"ref": "refs/heads/act-1"}))

	br, err := a.OpenBranch(rc(context.Background()), repo, "act-1", cred)
	if err != nil {
		t.Fatalf("OpenBranch: %v", err)
	}
	if BranchRefIsZero(br) {
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

	if _, err := a.OpenBranch(rc(context.Background()), repo, "act-1", cred); err != nil {
		t.Fatalf("branch-exists must map to success, got: %v", err)
	}
}

func TestU20_OpenPullRequestHappy(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("POST", "/repos/acme/my-project/pulls", gh.JSON(201, map[string]any{"number": 42, "state": "open"}))

	pr, err := a.OpenPullRequest(rc(context.Background()), repo, PullRequestSpec{Head: "act-1", Base: "main", Title: "T"}, cred)
	if err != nil {
		t.Fatalf("OpenPullRequest: %v", err)
	}
	if PullRequestRefString(pr) != "42" {
		t.Fatalf("PullRequestRef = %q, want 42", PullRequestRefString(pr))
	}
}

func TestU21_OpenPullRequestIdempotent(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("POST", "/repos/acme/my-project/pulls", gh.Response{Status: 422, Body: `{"message":"A pull request already exists"}`})
	fake.OnPrefix("GET", "/repos/acme/my-project/pulls", gh.JSON(200, []map[string]any{{"number": 42, "state": "open"}}))

	pr, err := a.OpenPullRequest(rc(context.Background()), repo, PullRequestSpec{Head: "act-1", Base: "main"}, cred)
	if err != nil {
		t.Fatalf("existing-PR must map to success, got: %v", err)
	}
	if PullRequestRefString(pr) != "42" {
		t.Fatalf("expected the existing PR #42, got %q", PullRequestRefString(pr))
	}
}

func TestU22_GetPullRequestStatusFolds(t *testing.T) {
	tests := []struct {
		name       string
		checkRuns  []map[string]any
		wantRollup CheckState
	}{
		{"success", []map[string]any{{"status": "completed", "conclusion": "success"}}, CheckSuccess},
		{"failure", []map[string]any{{"status": "completed", "conclusion": "failure"}}, CheckFailure},
		{"pending", []map[string]any{{"status": "in_progress", "conclusion": ""}}, CheckPending},
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
			st, err := a.GetPullRequestStatus(rc(context.Background()), repo, PullRequestRefFromString("42"), cred)
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

	if err := a.PostReview(rc(context.Background()), repo, PullRequestRefFromString("42"), ReviewSubmission{Verdict: ReviewApprove, Body: "+1"}, cred); err != nil {
		t.Fatalf("PostReview: %v", err)
	}
	req := findRequest(t, fake, "POST", "/repos/acme/my-project/pulls/42/reviews")
	if !strings.Contains(req.Body, `"APPROVE"`) {
		t.Fatalf("approve review should carry event=APPROVE; got %q", req.Body)
	}
}

// TestU23b_PostReviewSelfApprovalDegrades proves the self-+1 skip: an App-authored
// session PR (every amendment PR) makes GitHub reject the App's own approval with a
// 422; that is ceremonial, not fatal, so PostReview returns a no-op success — the
// amendment approve path no longer dead-loops. The request IS attempted (the skip is
// a degrade on the wire rejection, not a pre-emptive suppression).
func TestU23b_PostReviewSelfApprovalDegrades(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("POST", "/repos/acme/my-project/pulls/42/reviews",
		gh.Response{Status: 422, Body: `{"message":"Unprocessable Entity","errors":["Can not approve your own pull request"]}`})

	if err := a.PostReview(rc(context.Background()), repo, PullRequestRefFromString("42"), ReviewSubmission{Verdict: ReviewApprove, Body: "+1"}, cred); err != nil {
		t.Fatalf("self-approval 422 must degrade to a no-op success, got: %v", err)
	}
	if countRequests(fake, "POST", "/repos/acme/my-project/pulls/42/reviews") != 1 {
		t.Fatalf("expected exactly one review POST attempt before the degrade")
	}
}

// TestU23c_PostReviewNonSelfRejectionStillErrors proves the skip is narrow: a
// non-ContractMisuse rejection from the reviews endpoint (e.g. a 403 permission
// fault → fwra.Auth) is NOT the self-approval case and must still surface as an error.
func TestU23c_PostReviewNonSelfRejectionStillErrors(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("POST", "/repos/acme/my-project/pulls/42/reviews",
		gh.Response{Status: 403, Body: `{"message":"Resource not accessible by integration"}`})

	err := a.PostReview(rc(context.Background()), repo, PullRequestRefFromString("42"), ReviewSubmission{Verdict: ReviewApprove, Body: "+1"}, cred)
	requireKind(t, err, fwra.Auth)
}

func TestU24_MergePullRequestHappy(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("GET", "/repos/acme/my-project/pulls/42", gh.JSON(200, map[string]any{"number": 42, "merged": false}))
	fake.On("PUT", "/repos/acme/my-project/pulls/42/merge", gh.JSON(200, map[string]any{"sha": "mergedsha", "merged": true}))

	res, err := a.MergePullRequest(rc(context.Background()), repo, PullRequestRefFromString("42"), cred)
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

	res, err := a.MergePullRequest(rc(context.Background()), repo, PullRequestRefFromString("42"), cred)
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

	_, err := a.MergePullRequest(rc(context.Background()), repo, PullRequestRefFromString("42"), cred)
	requireKind(t, err, fwra.Conflict)
}

func TestU27_ConfigureBranchProtection(t *testing.T) {
	fake, a, repo, cred := railFixture(t)
	defer fake.Close()
	fake.On("PUT", "/repos/acme/my-project/branches/main/protection", gh.JSON(200, map[string]any{"url": "x"}))

	if err := a.ConfigureBranchProtection(rc(context.Background()), repo, cred); err != nil {
		t.Fatalf("ConfigureBranchProtection: %v", err)
	}
	req := findRequest(t, fake, "PUT", "/repos/acme/my-project/branches/main/protection")
	if !strings.Contains(req.Body, stpAppSlug) {
		t.Fatalf("branch protection should restrict/bypass the App slug; got %q", req.Body)
	}
}

// ---------------------------------------------------------------------------
// U28  Value semantics
// ---------------------------------------------------------------------------

func TestU28_ValueSemantics(t *testing.T) {
	if CheckStateString(CheckSuccess) != "Success" || CheckStateString(CheckFailure) != "Failure" || CheckStateString(CheckPending) != "Pending" {
		t.Fatalf("CheckState String mapping wrong")
	}
	if !RepoCredentialIsZero(RepoCredential{}) {
		t.Fatalf("empty credential should be zero")
	}
	if !InstallationIsZero(Installation("")) {
		t.Fatalf("empty installation should be zero")
	}
	if !PullRequestRefIsZero(PullRequestRef("")) || !BranchRefIsZero(BranchRef("")) {
		t.Fatalf("empty refs should be zero")
	}
	if !CommitRefIsZero(CommitRef("")) {
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

// ---------------------------------------------------------------------------
// U43–U47  managed-scaffold sync (sync-on-dispatch, 2026-07-06; torn-state
// hardening F-QA2-36, 2026-07-16). The design Managers converge the seated
// prompt surface onto the CURRENT rendering before every design-job dispatch:
// drift → commit(s) with the sync message naming the refreshed pins;
// byte-identical → NO commit. The seat manifest commits LAST, and the
// manifest-version fast path is only trusted after this process has once
// converged the full set (managedSyncMemo), so an interrupted or torn seat is
// always healed by the next dispatch.
// ---------------------------------------------------------------------------

// putMessage extracts the commit "message" from the recorded contents-PUT body at path.
func putMessage(t *testing.T, fake *gh.FakeGitHub, path string) string {
	t.Helper()
	for _, r := range fake.Requests() {
		if r.Method == "PUT" && r.Path == path {
			var body struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
				t.Fatalf("decode contents PUT body: %v", err)
			}
			return body.Message
		}
	}
	t.Fatalf("no contents PUT recorded for %s", path)
	return ""
}

// seedSyncedScaffold seats the ENTIRE sync-scoped file set (both workflows + the
// .claude tree + the seat manifest) into the fake repo at the CURRENT rendering, so a
// subsequent sync finds everything byte-identical and the manifest version current.
func seedSyncedScaffold(t *testing.T, fake *gh.FakeGitHub, repoName, appSlug string) []ManagedFile {
	t.Helper()
	files, err := managedSyncFiles(RepoRefFromString(testAccount+"|"+testAccount+"/"+repoName), appSlug)
	if err != nil {
		t.Fatalf("managedSyncFiles: %v", err)
	}
	for _, f := range files {
		fake.SeedRepoFile(testAccount, repoName, f.Path, f.Content)
	}
	return files
}

// countClaudePuts counts contents PUTs under the repo's .claude/ tree.
func countClaudePuts(fake *gh.FakeGitHub, repoName string) int {
	n := 0
	for _, r := range fake.Requests() {
		if r.Method == "PUT" && strings.HasPrefix(r.Path, "/repos/"+testAccount+"/"+repoName+"/contents/.claude/") {
			n++
		}
	}
	return n
}

// U43: DRIFT (no manifest) → FULL CONVERGE + COMMIT. A pre-B4 repo (stale seated
// design workflow, no seat manifest) takes the full-seat path: the whole prompt
// surface converges — the design workflow is refreshed to the CURRENT rendering
// under the SYNC message, the .claude tree (incl. the manifest) is seated — and
// changed=true is reported. The sync NEVER touches the birth-only scaffold roots
// (go.mod / method test / internal/.gitkeep — user-territory after birth).
func TestU43_SyncManagedScaffoldDriftCommitsRefresh(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	fake.SeedRepoFile(testAccount, "alpha", DesignWorkflowPath, []byte("stale seated workflow (old pin)"))

	changed, err := SyncManagedScaffold(context.Background(), a, repo, cred)
	if err != nil {
		t.Fatalf("SyncManagedScaffold: %v", err)
	}
	if !changed {
		t.Fatal("a drifted seated scaffold must report changed=true")
	}
	if got := countRequests(fake, "PUT", "/repos/acme/alpha/contents/"+DesignWorkflowPath); got != 1 {
		t.Fatalf("a drifted workflow must issue exactly one contents PUT, got %d", got)
	}
	want, err := DesignWorkflowFile(stpAppSlug)
	if err != nil {
		t.Fatalf("DesignWorkflowFile: %v", err)
	}
	stored, ok := fake.RepoFile(testAccount, "alpha", DesignWorkflowPath)
	if !ok || string(stored) != string(want.Content) {
		t.Fatal("the refreshed seated workflow must equal the CURRENT template rendering")
	}
	msg := putMessage(t, fake, "/repos/acme/alpha/contents/"+DesignWorkflowPath)
	if msg != syncManagedScaffoldMessage() {
		t.Fatalf("sync commit message = %q, want %q", msg, syncManagedScaffoldMessage())
	}
	// The full converge seats the prompt surface: the manifest (and tree) land.
	if _, ok := fake.RepoFile(testAccount, "alpha", scaffoldManifestPath); !ok {
		t.Fatal("the full-seat sync must seat the .claude seat manifest")
	}
	if countClaudePuts(fake, "alpha") == 0 {
		t.Fatal("the full-seat sync must seat the .claude tree")
	}
	// The sync scope is the PROMPT SURFACE only — go.mod, the method test, and the
	// gitkeep are birth-only and must never be re-seated by the sync.
	if countRequests(fake, "PUT", "/repos/acme/alpha/contents/go.mod") != 0 ||
		countRequests(fake, "PUT", "/repos/acme/alpha/contents/aiarch_method_test.go") != 0 ||
		countRequests(fake, "PUT", "/repos/acme/alpha/contents/internal/.gitkeep") != 0 {
		t.Fatal("the sync must never touch the birth-only scaffold roots")
	}
}

// U44: EVERYTHING CURRENT → NO COMMIT; FAST PATH ONLY ONCE VERIFIED (F-QA2-36).
// With the whole prompt surface seated at the current rendering, the FIRST sync of
// a process may NOT trust the manifest alone (a torn tree can hide under a current
// manifest): it walks the full sync set fetch-compare-put — reads, but zero contents
// PUTs (no empty commit), changed=false. Only the SECOND sync — full set verified
// this process AND manifest version current — takes the fast path: the sole .claude
// round-trip is the ONE manifest GET.
func TestU44_SyncManagedScaffoldByteIdenticalNoCommit(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	seedSyncedScaffold(t, fake, "alpha", stpAppSlug)
	preCount := len(fake.Requests())

	changed, err := SyncManagedScaffold(context.Background(), a, repo, cred)
	if err != nil {
		t.Fatalf("SyncManagedScaffold: %v", err)
	}
	if changed {
		t.Fatal("a byte-identical seated scaffold must report changed=false")
	}
	sawClaudeRead := false
	for _, r := range fake.Requests()[preCount:] {
		if r.Method == "PUT" {
			t.Fatalf("a current seated scaffold must issue NO contents PUT, got PUT %s", r.Path)
		}
		if strings.Contains(r.Path, "/contents/.claude/") && !strings.HasSuffix(r.Path, scaffoldManifestPath) {
			sawClaudeRead = true
		}
	}
	// VERIFICATION PROOF: the first sync of a process must NOT skip the tree on the
	// manifest's say-so — it reads the .claude files to prove them byte-exact.
	if !sawClaudeRead {
		t.Fatal("the first sync of a process must verify the .claude tree, not trust the manifest alone")
	}

	// Second sync: verified this process + manifest current → fast path.
	preCount = len(fake.Requests())
	changed, err = SyncManagedScaffold(context.Background(), a, repo, cred)
	if err != nil {
		t.Fatalf("SyncManagedScaffold (2nd): %v", err)
	}
	if changed {
		t.Fatal("a byte-identical seated scaffold must report changed=false on the fast path")
	}
	for _, r := range fake.Requests()[preCount:] {
		if r.Method == "PUT" {
			t.Fatalf("the fast path must issue NO contents PUT, got PUT %s", r.Path)
		}
		// FAST-PATH PROOF: the only .claude read is the ONE manifest GET — the tree
		// is fingerprinted by version once this process has verified it.
		if strings.Contains(r.Path, "/contents/.claude/") && !strings.HasSuffix(r.Path, scaffoldManifestPath) {
			t.Fatalf("the fast path must not touch .claude files beyond the manifest, got %s %s", r.Method, r.Path)
		}
	}
}

// U44b: TORN REPO WITH A LYING MANIFEST IS HEALED (F-QA2-36). The gtdapp incident:
// a pre-fix interrupted sync left a manifest at the CURRENT version (with a garbage
// files list, at that) over a tree with a STALE .claude file. The manifest version
// matches — but a fresh process (no memo) must NOT trust it: the first sync runs the
// full converge, rewrites the stale asset AND the off-rendering manifest
// (changed=true). Only after that byte-exact verification does the fast path arm:
// the second sync skips the tree (one manifest GET, no writes).
func TestU44b_SyncManagedScaffoldHealsTornTreeUnderCurrentManifest(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	files := seedSyncedScaffold(t, fake, "alpha", stpAppSlug)
	// The lying manifest: matching version, nonsense files list (the list is never
	// compared — the HEAL comes from the full byte converge, not list reconciliation).
	manifest, err := json.Marshal(map[string]any{
		"version": methodassets.Version(),
		"files":   []string{".claude/retained-orphan.md", ".claude/not-a-real-file.md"},
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	fake.SeedRepoFile(testAccount, "alpha", scaffoldManifestPath, manifest)
	// The tear: one .claude asset stale under the current-version manifest.
	fake.SeedRepoFile(testAccount, "alpha", ".claude/commands/mission-draft.md", []byte("stale prompt body"))

	changed, err := SyncManagedScaffold(context.Background(), a, repo, cred)
	if err != nil {
		t.Fatalf("SyncManagedScaffold: %v", err)
	}
	if !changed {
		t.Fatal("healing a torn tree must report changed=true")
	}
	var wantCmd, wantManifest []byte
	for _, f := range files {
		switch f.Path {
		case ".claude/commands/mission-draft.md":
			wantCmd = f.Content
		case scaffoldManifestPath:
			wantManifest = f.Content
		}
	}
	stored, ok := fake.RepoFile(testAccount, "alpha", ".claude/commands/mission-draft.md")
	if !ok || !bytes.Equal(stored, wantCmd) {
		t.Fatal("the sync must heal the stale .claude asset despite the current-version manifest")
	}
	storedMan, ok := fake.RepoFile(testAccount, "alpha", scaffoldManifestPath)
	if !ok || !bytes.Equal(storedMan, wantManifest) {
		t.Fatal("the sync must rewrite the lying manifest to the exact rendering")
	}

	// Healed and verified → the second sync takes the fast path: no .claude reads
	// beyond the manifest, no writes.
	preCount := len(fake.Requests())
	changed, err = SyncManagedScaffold(context.Background(), a, repo, cred)
	if err != nil {
		t.Fatalf("SyncManagedScaffold (2nd): %v", err)
	}
	if changed {
		t.Fatal("a healed scaffold must report changed=false on the fast path")
	}
	for _, r := range fake.Requests()[preCount:] {
		if r.Method == "PUT" {
			t.Fatalf("the fast path must issue NO contents PUT, got PUT %s", r.Path)
		}
		if strings.Contains(r.Path, "/contents/.claude/") && !strings.HasSuffix(r.Path, scaffoldManifestPath) {
			t.Fatalf("the fast path must not touch .claude files beyond the manifest, got %s %s", r.Method, r.Path)
		}
	}
}

// U44c: FAST PATH STILL CONVERGES THE WORKFLOWS. The workflows carry the
// SERVER-owned (ldflags-stampable) state-MCP pin, which the module version cannot
// fingerprint — so even on a fast-path hit (full set verified this process +
// manifest version current), a drifted seated workflow is refreshed (changed=true)
// while the .claude tree stays untouched (its only round-trip: the manifest GET).
func TestU44c_SyncManagedScaffoldFastPathConvergesWorkflows(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	seedSyncedScaffold(t, fake, "alpha", stpAppSlug)
	// Prime the fast path: a first sync verifies the full set for this process.
	if _, err := SyncManagedScaffold(context.Background(), a, repo, cred); err != nil {
		t.Fatalf("SyncManagedScaffold (prime): %v", err)
	}
	fake.SeedRepoFile(testAccount, "alpha", DesignWorkflowPath, []byte("stale seated workflow (old stamped pin)"))
	preCount := len(fake.Requests())

	changed, err := SyncManagedScaffold(context.Background(), a, repo, cred)
	if err != nil {
		t.Fatalf("SyncManagedScaffold: %v", err)
	}
	if !changed {
		t.Fatal("a drifted workflow must report changed=true even on a manifest fast-path hit")
	}
	want, err := DesignWorkflowFile(stpAppSlug)
	if err != nil {
		t.Fatalf("DesignWorkflowFile: %v", err)
	}
	stored, ok := fake.RepoFile(testAccount, "alpha", DesignWorkflowPath)
	if !ok || string(stored) != string(want.Content) {
		t.Fatal("the fast path must still converge the seated workflow onto the current rendering")
	}
	for _, r := range fake.Requests()[preCount:] {
		if r.Method == "PUT" && strings.Contains(r.Path, "/contents/.claude/") {
			t.Fatalf("the fast path must not write any .claude file, got PUT %s", r.Path)
		}
		if r.Method == "GET" && strings.Contains(r.Path, "/contents/.claude/") && !strings.HasSuffix(r.Path, scaffoldManifestPath) {
			t.Fatalf("the fast path must not read .claude files beyond the manifest, got GET %s", r.Path)
		}
	}
}

// U44d: VERSION MISMATCH → FULL CONVERGE. A seated manifest carrying a DIFFERENT
// methodassets version misses the fast path: the whole prompt surface converges,
// refreshing the stale .claude asset and the manifest itself (changed=true).
func TestU44d_SyncManagedScaffoldVersionMismatchFullSeat(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	files := seedSyncedScaffold(t, fake, "alpha", stpAppSlug)
	manifest, err := json.Marshal(map[string]any{"version": "v0.0.0-not-current", "files": []string{}})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	fake.SeedRepoFile(testAccount, "alpha", scaffoldManifestPath, manifest)
	fake.SeedRepoFile(testAccount, "alpha", ".claude/commands/mission-draft.md", []byte("stale prompt body"))

	changed, err := SyncManagedScaffold(context.Background(), a, repo, cred)
	if err != nil {
		t.Fatalf("SyncManagedScaffold: %v", err)
	}
	if !changed {
		t.Fatal("a version-mismatched scaffold must report changed=true")
	}
	var wantCmd []byte
	for _, f := range files {
		if f.Path == ".claude/commands/mission-draft.md" {
			wantCmd = f.Content
		}
	}
	stored, ok := fake.RepoFile(testAccount, "alpha", ".claude/commands/mission-draft.md")
	if !ok || !bytes.Equal(stored, wantCmd) {
		t.Fatal("the full converge must refresh the stale .claude asset to the current rendering")
	}
	storedMan, ok := fake.RepoFile(testAccount, "alpha", scaffoldManifestPath)
	var m struct {
		Version string `json:"version"`
	}
	if !ok || json.Unmarshal(storedMan, &m) != nil || m.Version != methodassets.Version() {
		t.Fatal("the full converge must refresh the seat manifest to the current version")
	}
}

// U45: FALLBACK. A rail that lacks the auxiliary sync + read surfaces (here: the real
// access hidden behind an interface-embedding wrapper, so the type assertions miss)
// still CONVERGES the full sync set through the frozen CommitManagedFiles verb — the
// refreshed bytes land — but reports changed=false (the frozen verb does not report
// drift) and never takes the fast path (no read surface).
func TestU45_SyncManagedScaffoldFallsBackToFrozenVerb(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()
	fake.SeedRepoFile(testAccount, "alpha", DesignWorkflowPath, []byte("stale seated workflow"))
	wrapped := struct{ SourceControlAccess }{a} // hides SyncManagedFiles + ReadManagedFile + AppSlug

	changed, err := SyncManagedScaffold(context.Background(), wrapped, repo, cred)
	if err != nil {
		t.Fatalf("SyncManagedScaffold (fallback): %v", err)
	}
	if changed {
		t.Fatal("the frozen-verb fallback cannot report drift; changed must be false")
	}
	want, err := DesignWorkflowFile("") // wrapper hides AppSlug too → empty slug rendering
	if err != nil {
		t.Fatalf("DesignWorkflowFile: %v", err)
	}
	stored, ok := fake.RepoFile(testAccount, "alpha", DesignWorkflowPath)
	if !ok || string(stored) != string(want.Content) {
		t.Fatal("the fallback must still converge the seated workflow onto the current rendering")
	}
}

// contentsPuts returns the repo's recorded contents-PUT paths (repo-relative), in
// wire order.
func contentsPuts(fake *gh.FakeGitHub, repoName string) []string {
	prefix := "/repos/" + testAccount + "/" + repoName + "/contents/"
	var out []string
	for _, r := range fake.Requests() {
		if r.Method == "PUT" && strings.HasPrefix(r.Path, prefix) {
			out = append(out, strings.TrimPrefix(r.Path, prefix))
		}
	}
	return out
}

// U46: THE MANIFEST COMMITS LAST (F-QA2-36). A full seat writes every content file
// BEFORE the seat manifest — under a plain path sort the manifest (".claude/.method-…")
// landed FIRST, so a sync that died mid-loop left a current-version manifest over a
// half-written tree and the fast path skipped the tear forever. The write order is the
// resumability guarantee: the manifest may only ever assert a version whose content
// files all landed before it.
func TestU46_SyncManagedScaffoldCommitsManifestLast(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()

	changed, err := SyncManagedScaffold(context.Background(), a, repo, cred)
	if err != nil {
		t.Fatalf("SyncManagedScaffold: %v", err)
	}
	if !changed {
		t.Fatal("a full seat into an empty repo must report changed=true")
	}
	puts := contentsPuts(fake, "alpha")
	if len(puts) == 0 {
		t.Fatal("a full seat must issue contents PUTs")
	}
	if got := puts[len(puts)-1]; got != scaffoldManifestPath {
		t.Fatalf("the seat manifest must be the LAST contents PUT, got %s (manifest position matters: it is the sync fast-path's currency fingerprint)", got)
	}
	n := 0
	for _, p := range puts {
		if p == scaffoldManifestPath {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the seat manifest must be written exactly once, got %d PUTs", n)
	}
}

// U47: AN INTERRUPTED SYNC IS RESUMED BY THE NEXT ONE (F-QA2-36). Simulate the live
// gtdapp failure — the per-file loop dies partway through the .claude tree — and
// prove the fix's invariant: because the manifest commits last, the interrupted sync
// leaves NO current manifest behind, so the next dispatch's sync misses the fast
// path, re-converges the full set, and completes the seat (manifest landing last).
func TestU47_SyncManagedScaffoldInterruptedSyncResumes(t *testing.T) {
	fake, a, repo, cred := adoptedFixture(t, "alpha")
	defer fake.Close()

	// Pick a victim mid-way through the commit order (path-sorted .claude tree).
	files, err := managedSyncFiles(repo, stpAppSlug)
	if err != nil {
		t.Fatalf("managedSyncFiles: %v", err)
	}
	var claude []string
	for _, f := range files {
		if strings.HasPrefix(f.Path, claudePathPrefix) && f.Path != scaffoldManifestPath {
			claude = append(claude, f.Path)
		}
	}
	if len(claude) < 3 {
		t.Fatalf("need a multi-file .claude tree to interrupt, got %d files", len(claude))
	}
	victim := claude[len(claude)/2]
	victimRoute := "/repos/" + testAccount + "/alpha/contents/" + victim
	fake.On("PUT", victimRoute, gh.Response{Status: 500, Body: `{"message":"mid-loop death"}`})

	// First sync dies mid-loop at the victim.
	if _, err := SyncManagedScaffold(context.Background(), a, repo, cred); err == nil {
		t.Fatal("the interrupted sync must surface the mid-loop error")
	}
	// Files ahead of the victim landed; the MANIFEST did not (it commits last), so
	// the torn state cannot masquerade as current.
	if _, ok := fake.RepoFile(testAccount, "alpha", claude[0]); !ok {
		t.Fatalf("files committed before the interruption must have landed (%s)", claude[0])
	}
	if _, ok := fake.RepoFile(testAccount, "alpha", scaffoldManifestPath); ok {
		t.Fatal("an interrupted sync must NOT have written the seat manifest (manifest-last is the resumability guarantee)")
	}
	if countRequests(fake, "PUT", "/repos/"+testAccount+"/alpha/contents/"+scaffoldManifestPath) != 0 {
		t.Fatal("no manifest PUT may precede the content files")
	}

	// The failure clears (re-script the victim route to succeed) and the next
	// dispatch's sync completes the converge.
	fake.On("PUT", victimRoute, gh.JSON(201, map[string]any{
		"content": map[string]any{},
		"commit":  map[string]any{"sha": "resume123"},
	}))
	preCount := len(fake.Requests())
	changed, err := SyncManagedScaffold(context.Background(), a, repo, cred)
	if err != nil {
		t.Fatalf("SyncManagedScaffold (resume): %v", err)
	}
	if !changed {
		t.Fatal("the resuming sync must report changed=true (it completes the seat)")
	}
	storedMan, ok := fake.RepoFile(testAccount, "alpha", scaffoldManifestPath)
	var m struct {
		Version string `json:"version"`
	}
	if !ok || json.Unmarshal(storedMan, &m) != nil || m.Version != methodassets.Version() {
		t.Fatal("the resuming sync must complete the seat: manifest present at the current version")
	}
	// And the manifest is still the LAST write of the resuming pass.
	var resumePuts []string
	prefix := "/repos/" + testAccount + "/alpha/contents/"
	for _, r := range fake.Requests()[preCount:] {
		if r.Method == "PUT" && strings.HasPrefix(r.Path, prefix) {
			resumePuts = append(resumePuts, strings.TrimPrefix(r.Path, prefix))
		}
	}
	if len(resumePuts) == 0 || resumePuts[len(resumePuts)-1] != scaffoldManifestPath {
		t.Fatalf("the resuming pass must write the manifest last, got PUTs %v", resumePuts)
	}
}

// ---- from agenticdesign_test.go ----

// agenticdesign_test.go — structural tests over the RENDERED design workflow. The
// asset itself lives in the platform method-assets module (B4 delegation); these
// tests pin the WIRING CONTRACTS the app depends on — the dispatch-input names the
// design Managers fill, the satellite run-name/idempotency anchors, the state-MCP
// pin handshake — against the exact bytes the seat/sync commits (DesignWorkflowFile),
// so a module bump that breaks a server-side contract fails HERE, not live.
//
// These assert the asset WIRING (the contract anchors), not a live Actions run.
// The yaml.v3 + framework-go-infrastructure-github imports here are TEST-ONLY, so
// the Method layering checker (loaded with Tests:false) never scans them.

// testAppSlug is a representative configured App slug the structural tests render the
// workflow with.
const testAppSlug = "archistrator-bot"

// renderedDesignWorkflow renders the design workflow with the given slug or fails the
// test. Most structural assertions are slug-independent, so they use testAppSlug.
func renderedDesignWorkflow(t *testing.T, appSlug string) []byte {
	t.Helper()
	f, err := DesignWorkflowFile(appSlug)
	if err != nil {
		t.Fatalf("DesignWorkflowFile(%q): %v", appSlug, err)
	}
	return f.Content
}

// expectedDispatchInputs is the CONTRACT between the workflow template and the design
// Managers (C-MSD-Δ / C-MPD-Δ DispatchInputs on PipelineSpec). idempotency_token
// is the load-bearing dispatch anchor shared with the construction workflow; the
// other five are the additive DESIGN-job parameters (thin dispatch: `command` routes
// to the slash command under .claude/commands/ — design_prompt no longer exists).
var expectedDispatchInputs = []string{
	"idempotency_token",
	"command",
	"artifact_kind",
	"target_branch",
	"prior_state_ref",
	"job_mode",
}

// requiredDispatchInputs are the inputs that MUST be required:true. prior_state_ref
// is intentionally optional (empty on the first artifact of a fresh project);
// job_mode is optional (defaulted to "draft").
var requiredDispatchInputs = []string{
	"idempotency_token",
	"command",
	"artifact_kind",
	"target_branch",
}

// workflowDoc is a minimal structural view of the workflow_dispatch surface we
// assert on — we are testing the asset wiring, not running Actions.
type workflowDoc struct {
	Name    string `yaml:"name"`
	RunName string `yaml:"run-name"`
	On      struct {
		WorkflowDispatch struct {
			Inputs map[string]struct {
				Description string `yaml:"description"`
				Required    bool   `yaml:"required"`
				Type        string `yaml:"type"`
			} `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
}

func TestRenderedDesignWorkflowParsesAsYAML(t *testing.T) {
	var doc workflowDoc
	if err := yaml.Unmarshal(renderedDesignWorkflow(t, testAppSlug), &doc); err != nil {
		t.Fatalf("rendered template does not parse as YAML: %v", err)
	}
	if doc.Name == "" {
		t.Error("workflow has no top-level name")
	}
}

func TestDeclaresExpectedDispatchInputs(t *testing.T) {
	var doc workflowDoc
	if err := yaml.Unmarshal(renderedDesignWorkflow(t, testAppSlug), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	inputs := doc.On.WorkflowDispatch.Inputs
	if inputs == nil {
		t.Fatal("workflow declares no workflow_dispatch inputs")
	}
	for _, name := range expectedDispatchInputs {
		if _, ok := inputs[name]; !ok {
			t.Errorf("missing expected workflow_dispatch input %q", name)
		}
	}
	for _, name := range requiredDispatchInputs {
		in, ok := inputs[name]
		if !ok {
			continue // already reported above
		}
		if !in.Required {
			t.Errorf("input %q must be required:true", name)
		}
	}
	// prior_state_ref + job_mode are the intentionally-optional inputs.
	if in, ok := inputs["prior_state_ref"]; ok && in.Required {
		t.Error("prior_state_ref must be optional (required:false) — empty on a fresh project")
	}
	if in, ok := inputs["job_mode"]; ok && in.Required {
		t.Error("job_mode must be optional (required:false) — defaulted to draft")
	}
	// Thin dispatch: the composed-prompt input is GONE (the command routes to the
	// slash command under .claude/commands/, which carries the drafting logic).
	if _, ok := inputs["design_prompt"]; ok {
		t.Error("design_prompt must no longer exist as a dispatch input (thin dispatch)")
	}
}

func TestIdempotencyAnchorMatchesDispatchConstants(t *testing.T) {
	var doc workflowDoc
	if err := yaml.Unmarshal(renderedDesignWorkflow(t, testAppSlug), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The load-bearing input name MUST equal the satellite constant the
	// constructionPipelineAccess RA fills, or dispatch/observe/cancel break.
	if _, ok := doc.On.WorkflowDispatch.Inputs[fwgithub.DispatchInputKeyIdempotency]; !ok {
		t.Errorf("workflow must declare the %q input (DispatchInputKeyIdempotency)",
			fwgithub.DispatchInputKeyIdempotency)
	}
	// run-name MUST carry the RunNamePrefix so ListRunsByName can resolve runs.
	if !strings.HasPrefix(doc.RunName, fwgithub.RunNamePrefix) {
		t.Errorf("run-name %q must start with RunNamePrefix %q", doc.RunName, fwgithub.RunNamePrefix)
	}
	if !strings.Contains(doc.RunName, "${{ inputs."+fwgithub.DispatchInputKeyIdempotency+" }}") {
		t.Errorf("run-name %q must stamp the idempotency_token input", doc.RunName)
	}
}

func TestReferencesGoTestGateAndStatePath(t *testing.T) {
	body := string(renderedDesignWorkflow(t, testAppSlug))

	// The required check is `aiarch-state-mcp validate` — the policy-aware Method gate
	// shipped in the pinned state-MCP binary (2026-07-06; the amendment deadlock
	// fixes). The old seated `go mod tidy` + `go test ./...` steps must be gone from
	// the DESIGN workflow (the seated go.mod/aiarch_method_test.go scaffold remains
	// for the product repo's own CI), and the long-removed aiarch-validate container
	// must stay gone.
	if !strings.Contains(body, "${{ steps.statemcp.outputs.bin }} validate") {
		t.Error("workflow's required check must run the pinned binary's `validate` subcommand")
	}
	// SLOT-SCOPED severity: the validate step threads the job's ambient artifact —
	// the SAME dispatch input that fixes the drafting agent's slot — as --slot, via an
	// env var (never shell-interpolated), so the CI verdict scopes exactly as the
	// in-loop putDraftModel verdict does.
	if !strings.Contains(body, `validate --slot "${ARTIFACT_KIND}"`) {
		t.Error("the validate step must pass the ambient artifact as --slot (via the ARTIFACT_KIND env)")
	}
	if !strings.Contains(body, "ARTIFACT_KIND: ${{ inputs.artifact_kind }}") {
		t.Error("the validate step must source ARTIFACT_KIND from the artifact_kind dispatch input")
	}
	if strings.Contains(body, "go test ./...") {
		t.Error("the design workflow must no longer run the seated `go test ./...` gate (replaced by `aiarch-state-mcp validate`)")
	}
	if strings.Contains(body, "go mod tidy") {
		t.Error("the design workflow must no longer materialize go.sum (the seated go test is not run here)")
	}
	if !strings.Contains(body, "actions/setup-go") {
		t.Error("workflow must set up Go before installing the pinned validator binary")
	}
	// The validate JOB installs the pinned binary itself (jobs share no filesystem):
	// the `go install <path>@pin` line must appear in BOTH the draft and validate jobs.
	if strings.Count(body, "go install "+StateMcpModulePath) < 2 {
		t.Error("both the draft and validate jobs must install the pinned aiarch-state-mcp binary")
	}
	if strings.Contains(body, "aiarch-validate") {
		t.Error("workflow must no longer reference the removed aiarch-validate CLI/container")
	}

	// Commits / validates under the .aiarch/state/ tree that methodcheck.Check and
	// projectStateAccess read.
	if !strings.Contains(body, ".aiarch/state/") {
		t.Error("workflow must commit/validate under the .aiarch/state/ tree")
	}

	// References claude-code-action authenticated by the named secret only (never
	// an inlined token value).
	if !strings.Contains(body, "claude-code-action") {
		t.Error("workflow must run claude-code-action")
	}
	if !strings.Contains(body, "secrets.CLAUDE_CODE_OAUTH_TOKEN") {
		t.Error("workflow must reference CLAUDE_CODE_OAUTH_TOKEN by secret name")
	}
}

// TestDesignWorkflowThinDispatchNoPRDependency asserts the thin-dispatch prompt
// contract (Plan-2): the Claude step's prompt is EXACTLY the slash-command
// invocation — no drafting logic, no composed design_prompt — with the intent
// living in the per-kind command under .claude/commands/. It also pins the F39
// invariant that nothing in the template depends on a critique PR existing: the
// run-name and the validate job both key off the branch, never a PR.
func TestDesignWorkflowThinDispatchNoPRDependency(t *testing.T) {
	body := string(renderedDesignWorkflow(t, testAppSlug))

	// THIN PROMPT: the prompt is the slash command, verbatim.
	if !strings.Contains(body, "/${{ inputs.command }}") {
		t.Error("the Claude step's prompt must be the thin slash-command invocation /${{ inputs.command }}")
	}
	// The composed-prompt input and the old agent-facing file-edit / open-a-PR
	// instructions must be gone.
	if strings.Contains(body, "design_prompt") {
		t.Error("the workflow must no longer reference design_prompt (thin dispatch)")
	}
	if strings.Contains(body, "ALSO open a pull request") ||
		strings.Contains(body, "In both modes, commit onto the branch") ||
		strings.Contains(body, "set \"critiqueVerdict\" to") {
		t.Error("the old agent-facing file-edit / open-a-PR instructions must be removed")
	}
	// The aiarch-state MCP server is wired into the Claude step.
	if !strings.Contains(body, "--mcp-config") || !strings.Contains(body, "aiarch-state") {
		t.Error("the Claude step must wire the aiarch-state MCP server via --mcp-config")
	}

	// No PR dependency anywhere structural: the run-name keys off the idempotency
	// token, and the validate job checks out the target branch — never a PR ref.
	var doc workflowDoc
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("rendered template must still parse as YAML: %v", err)
	}
	if !strings.Contains(doc.RunName, "idempotency_token") {
		t.Errorf("run-name must key off idempotency_token, not a PR: %q", doc.RunName)
	}
	// The validate job checks out inputs.target_branch (the branch), not a PR merge ref.
	if !strings.Contains(body, "ref: ${{ inputs.target_branch }}") {
		t.Error("validate must check out the target branch (no critique-PR dependency)")
	}
}

// TestDesignWorkflowWiresStateMcp asserts the rendered workflow obtains the local
// aiarch-state MCP server (go install <path>@<pin>), writes its MCP config with the
// ambient session context baked in from the dispatch inputs, and wires --mcp-config into
// the Claude step. This is the delivery mechanism — the binary is fetched the SAME way the
// seated go test fetches framework-go (go install @ a GOPROXY-resolvable pin).
func TestDesignWorkflowWiresStateMcp(t *testing.T) {
	body := string(renderedDesignWorkflow(t, testAppSlug))

	// VERSION HANDSHAKE (managed-scaffold sync): the pin is stamped as a step env the job
	// echoes (so any run's log states which binary generation the seated scaffold carries)
	// and the install resolves through that same env — one rendered value, two uses.
	if !strings.Contains(body, `AIARCH_STATE_MCP_PIN: "`+StateMcpModulePin+`"`) {
		t.Errorf("workflow must stamp the state-MCP pin as the AIARCH_STATE_MCP_PIN env (%s); got:\n%s", StateMcpModulePin, body)
	}
	if !strings.Contains(body, "go install "+StateMcpModulePath+`@"${AIARCH_STATE_MCP_PIN}"`) {
		t.Errorf("workflow must `go install %s@\"${AIARCH_STATE_MCP_PIN}\"`; got:\n%s", StateMcpModulePath, body)
	}
	if !strings.Contains(body, "echo \"aiarch-state-mcp pin: "+StateMcpModulePath+"@${AIARCH_STATE_MCP_PIN}\"") {
		t.Error("workflow must echo the stamped state-MCP pin (the version-handshake log line)")
	}
	// The MCP config bakes in the ambient env keys the binary reads (never agent-supplied).
	for _, key := range []string{"AIARCH_PROJECT_ID", "AIARCH_ARTIFACT_KIND", "AIARCH_JOB_MODE", "AIARCH_TARGET_BRANCH", "AIARCH_STATE_ROOT"} {
		if !strings.Contains(body, key) {
			t.Errorf("MCP config must set ambient env %q", key)
		}
	}
	// The ambient kind + job mode come from the dispatch inputs, not the agent.
	if !strings.Contains(body, "${{ inputs.artifact_kind }}") || !strings.Contains(body, "${{ inputs.job_mode }}") {
		t.Error("MCP config must source artifact_kind + job_mode from the dispatch inputs")
	}
	// The rendered workflow must still be valid YAML after the MCP steps.
	var doc workflowDoc
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("rendered workflow must parse as YAML after the MCP wiring: %v", err)
	}
}

// TestDesignWorkflowReconcilesStateConflict asserts the F80 refresh-step wiring: answer
// jobs skip the merge-from-main, and a draft/critique conflict on the state document is
// resolved DETERMINISTICALLY via the aiarch-state-mcp `reconcile` subcommand rather than
// dead-ending RED. It also asserts the F82 self-heal: a conflict reconcile cannot resolve
// (a withdrawn/dead branch, or a conflict beyond the owned slot) hard-resets the scratch
// session branch to origin/main instead of dead-ending every future amendment of the slot.
func TestDesignWorkflowReconcilesStateConflict(t *testing.T) {
	body := string(renderedDesignWorkflow(t, testAppSlug))

	// F80(a): answer jobs short-circuit before the merge (still on the branch tip).
	if !strings.Contains(body, `[ "${JOB_MODE}" = "answer" ]`) {
		t.Error("refresh step must skip the merge-from-main for answer jobs (F80a)")
	}
	// F80(b): a state-document conflict is reconciled, not failed.
	if !strings.Contains(body, "reconcile") {
		t.Error("refresh step must invoke the aiarch-state-mcp reconcile subcommand (F80b)")
	}
	if !strings.Contains(body, "--diff-filter=U") {
		t.Error("refresh step must detect the conflicted file set before auto-resolving")
	}
	// It reads BOTH merge stages of the state file (ours = :2, theirs/main = :3).
	if !strings.Contains(body, `:2:${STATE_FILE}`) || !strings.Contains(body, `:3:${STATE_FILE}`) {
		t.Error("reconcile must read both merge stages of the state document")
	}
	// F82: a conflict the reconcile CANNOT resolve (or a conflict on any file beyond the
	// owned state slot) self-heals to main rather than dead-ending — the branch is scratch,
	// the durable state lives on main. It must hard-reset to origin/main and force-push (with
	// lease), never `exit 1` on the conflict.
	if !strings.Contains(body, "reset --hard origin/main") {
		t.Error("refresh step must self-heal a non-reconcilable conflict by hard-resetting to origin/main (F82)")
	}
	if !strings.Contains(body, "push --force-with-lease") {
		t.Error("refresh step must force-push (with lease) the self-heal reset (F82)")
	}
	// The reconcile is GUARDED (its failure routes to self-heal, not a RED step) — the
	// self-heal function must be invoked from both the non-state-conflict and the
	// reconcile-failure paths.
	if strings.Count(body, "self_heal_reset ") < 2 {
		t.Error("refresh step must route BOTH the non-state conflict and the reconcile-failure paths to the self-heal reset (F82)")
	}
	if strings.Contains(body, "refusing to auto-resolve") {
		t.Error("refresh step must no longer dead-end a conflicting refresh with an honest RED failure (F82 replaces it with self-heal)")
	}
	// The MCP binary is installed BEFORE the refresh step needs it (reconcile).
	iInstall := strings.Index(body, "go install "+StateMcpModulePath)
	iRefresh := strings.Index(body, "Refresh the session branch from main")
	if iInstall < 0 || iRefresh < 0 || iInstall > iRefresh {
		t.Error("the aiarch-state MCP binary must be installed before the refresh step (which invokes reconcile)")
	}
	var doc workflowDoc
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("rendered workflow must parse as YAML after the F80 reconcile wiring: %v", err)
	}
}

// TestDesignWorkflowAllowedBots asserts the allowed_bots actor is templated from the
// configured App slug (never hardcoded) and, crucially, is OMITTED entirely when the
// slug is empty — an unconfigured deployment then still supports human-dispatched runs
// rather than emitting an empty/invalid allowed_bots value.
func TestDesignWorkflowAllowedBots(t *testing.T) {
	// With a configured slug, allowed_bots renders with exactly that slug, and it must
	// parse as valid YAML.
	withSlug := string(renderedDesignWorkflow(t, "acme-aiarch-bot"))
	if !strings.Contains(withSlug, "allowed_bots: acme-aiarch-bot") {
		t.Errorf("rendered workflow must set allowed_bots to the configured slug; got:\n%s", withSlug)
	}
	var doc workflowDoc
	if err := yaml.Unmarshal([]byte(withSlug), &doc); err != nil {
		t.Fatalf("rendered workflow (with slug) must parse as YAML: %v", err)
	}

	// With an empty slug, the allowed_bots KEY must be ABSENT (guard: omit, don't emit
	// empty). The result must still be a valid workflow (parses as YAML).
	empty := string(renderedDesignWorkflow(t, ""))
	if strings.Contains(empty, "allowed_bots:") {
		t.Errorf("empty slug must omit the allowed_bots key entirely; got:\n%s", empty)
	}
	if err := yaml.Unmarshal([]byte(empty), &doc); err != nil {
		t.Fatalf("rendered workflow (empty slug) must parse as YAML: %v", err)
	}
}

// TestManagedScaffoldFiles asserts the FULL birth scaffold bundle (methodassets
// delegation, B4): both agentic workflows + the templated go-test gate (go.mod +
// aiarch_method_test.go) + the complete .claude tree with its seat manifest + the
// internal/.gitkeep placeholder — all on the managed-file allowlist, all non-empty,
// with the repo's module path templated in and NO server-owned path double-written.
func TestManagedScaffoldFiles(t *testing.T) {
	// owner|owner/repo encoding the RA produces (makeRepoRef): account=acme,
	// fullName=acme/widgets.
	repo := makeRepoRef("acme", "acme/widgets")
	files, err := ManagedScaffoldFiles(repo, testAppSlug)
	if err != nil {
		t.Fatalf("ManagedScaffoldFiles: %v", err)
	}

	byPath := map[string]ManagedFile{}
	for i, f := range files {
		byPath[f.Path] = f
		// Every seated file MUST be on the managed-file allowlist (the verb rejects
		// anything else) and non-empty (the verb rejects empty content).
		if !isManagedFilePath(f.Path) {
			t.Errorf("scaffold file %q is not on the managed-file allowlist", f.Path)
		}
		if len(f.Content) == 0 {
			t.Errorf("scaffold file %q has empty content", f.Path)
		}
		// Deterministic seat order (stable commit history).
		if i > 0 && files[i-1].Path >= f.Path {
			t.Errorf("scaffold files must be path-sorted: %q before %q", files[i-1].Path, f.Path)
		}
	}

	// (1) BOTH workflows are present; the design workflow equals the single-file
	// rendering (seat and sync share one rendering) and carries allowed_bots.
	wf, ok := byPath[DesignWorkflowPath]
	if !ok {
		t.Fatalf("missing %s in the scaffold bundle", DesignWorkflowPath)
	}
	if !bytes.Equal(wf.Content, renderedDesignWorkflow(t, testAppSlug)) {
		t.Error("workflow content must be the template rendered with the App slug")
	}
	if !strings.Contains(string(wf.Content), "allowed_bots: "+testAppSlug) {
		t.Errorf("seated workflow must allow-list the configured App slug; got:\n%s", wf.Content)
	}
	cwf, ok := byPath[".github/workflows/aiarch-construct.yml"]
	if !ok {
		t.Fatal("missing .github/workflows/aiarch-construct.yml in the scaffold bundle")
	}
	// The construct workflow templates the slug UNGUARDED (methodassets contract:
	// AppSlug is REQUIRED for construction dispatch).
	if !strings.Contains(string(cwf.Content), testAppSlug) {
		t.Error("seated construct workflow must carry the configured App slug")
	}

	// (2) go.mod templated with the derived module path + the framework-go require pin
	// (the pin is module-owned now: methodassets.FrameworkGoVersion).
	goMod, ok := byPath[GoModPath]
	if !ok {
		t.Fatalf("missing %s in the scaffold bundle", GoModPath)
	}
	gm := string(goMod.Content)
	if !strings.Contains(gm, "module github.com/acme/widgets") {
		t.Errorf("go.mod must declare the derived module path; got:\n%s", gm)
	}
	if !strings.Contains(gm, "require github.com/mixofreality-studio/archistrator-platform/framework-go "+methodassets.FrameworkGoVersion) {
		t.Errorf("go.mod must require framework-go at the module-pinned version %q; got:\n%s", methodassets.FrameworkGoVersion, gm)
	}

	// (3) the method test templates the module path into arch.MethodSpec + calls
	// methodcheck.Check.
	mt, ok := byPath[MethodTestPath]
	if !ok {
		t.Fatalf("missing %s in the scaffold bundle", MethodTestPath)
	}
	mts := string(mt.Content)
	if !strings.Contains(mts, "methodcheck.Check") {
		t.Error("method test must call methodcheck.Check")
	}
	if !strings.Contains(mts, "github.com/acme/widgets") {
		t.Errorf("method test must template the module path into arch.MethodSpec; got:\n%s", mts)
	}

	// (4) internal/.gitkeep keeps the internal/ directory present so the method gate's
	// arch.MethodSpec ./internal/... load pattern resolves instead of hard-erroring on
	// a fresh repo. The module emits it EMPTY; the seat overrides it with the
	// explanatory one-liner because CommitManagedFiles rejects empty content.
	gk, ok := byPath[internalGitkeepPath]
	if !ok {
		t.Fatalf("missing %s in the scaffold bundle", internalGitkeepPath)
	}
	if string(gk.Content) != internalGitkeepContent {
		t.Errorf("internal/.gitkeep content = %q, want %q", gk.Content, internalGitkeepContent)
	}

	// (5) the .claude prompt surface rides the seat: a known command asset + the seat
	// manifest, whose version fingerprint IS this server's methodassets version.
	if _, ok := byPath[".claude/commands/mission-draft.md"]; !ok {
		t.Fatal("missing .claude/commands/mission-draft.md in the scaffold bundle")
	}
	man, ok := byPath[scaffoldManifestPath]
	if !ok {
		t.Fatalf("missing %s in the scaffold bundle", scaffoldManifestPath)
	}
	var m struct {
		Version string   `json:"version"`
		Files   []string `json:"files"`
	}
	if err := json.Unmarshal(man.Content, &m); err != nil {
		t.Fatalf("seat manifest is not valid JSON: %v", err)
	}
	// The manifest version must equal what the sync fast-path will compare against —
	// never a specific literal (build-info derived; "devel" outside a versioned build).
	if m.Version != methodassets.Version() {
		t.Errorf("seat manifest version = %q, want methodassets.Version() = %q", m.Version, methodassets.Version())
	}
	if len(m.Files) == 0 {
		t.Error("seat manifest must list the .claude files it owns")
	}

	// (6) the server-owned state path is NEVER double-written by the scaffold.
	for p := range byPath {
		if strings.HasPrefix(p, ".aiarch/") {
			t.Errorf("scaffold must not seat server-owned path %q (CreateProject seeds .aiarch/state)", p)
		}
	}
}

// TestInternalGitkeepAcceptedByAllowlist proves the seeded placeholder path is on the
// managed-file allowlist (so CommitManagedFiles accepts it), while an arbitrary file
// under internal/ is NOT (the allowlist lists the literal internal/.gitkeep, not an
// internal/ prefix — keeping it tight).
func TestInternalGitkeepAcceptedByAllowlist(t *testing.T) {
	if !isManagedFilePath(internalGitkeepPath) {
		t.Errorf("%q must be on the managed-file allowlist", internalGitkeepPath)
	}
	if isManagedFilePath("internal/main.go") {
		t.Error("an arbitrary file under internal/ must NOT be on the allowlist — only the literal internal/.gitkeep is")
	}
}

// TestClaudeTreeAllowlist proves the .claude/ prefix allowance (B4): clean paths under
// the methodassets prompt surface are managed files, while path traversal, absolute
// paths, and non-clean forms can never ride the prefix onto the allowlist.
func TestClaudeTreeAllowlist(t *testing.T) {
	for _, p := range []string{
		".claude/agents/x.md",
		".claude/commands/mission-draft.md",
		".claude/skills/the-method/SKILL.md",
		scaffoldManifestPath,
	} {
		if !isManagedFilePath(p) {
			t.Errorf("%q must be on the managed-file allowlist (.claude prefix)", p)
		}
	}
	for _, p := range []string{
		".claude/../x",               // escapes the tree (cleans to "x")
		".claude/a/../../etc/pw",     // escapes via embedded ..
		".claude/a/../b.md",          // non-clean even though it stays inside
		".claude//double-slash.md",   // non-clean
		".claude/./self.md",          // non-clean
		"/.claude/abs.md",            // absolute
		".claudefile",                // prefix must be the DIRECTORY .claude/
		".claude",                    // the bare directory is not a file path
		"x/.claude/nested.md",        // prefix must anchor at the repo root
		".aiarch/state/project.json", // server-owned, never scaffold-managed
	} {
		if isManagedFilePath(p) {
			t.Errorf("%q must NOT be on the managed-file allowlist", p)
		}
	}
}

// TestManagedScaffoldFilesRejectsZeroRepo proves a malformed RepoRef (no owner/repo)
// is a ContractMisuse the accessor surfaces, not a silent empty module path.
func TestManagedScaffoldFilesRejectsZeroRepo(t *testing.T) {
	if _, err := ManagedScaffoldFiles(RepoRef(""), testAppSlug); err == nil {
		t.Fatal("expected an error for a zero RepoRef (unresolvable module path)")
	}
}

// TestRailAppSlug proves the birth-scaffold caller can read the App slug off the
// concrete GitHub access (which knows its own slug), and that a rail NOT exposing it
// yields "" (so allowed_bots is omitted rather than emitted empty).
func TestRailAppSlug(t *testing.T) {
	// The concrete access exposes its configured slug via AppSlug(); RailAppSlug reads it.
	a := &access{appSlug: "cfg-app-slug"}
	if got := a.AppSlug(); got != "cfg-app-slug" {
		t.Errorf("access.AppSlug() = %q, want cfg-app-slug", got)
	}
	if got := RailAppSlug(a); got != "cfg-app-slug" {
		t.Errorf("RailAppSlug(access) = %q, want cfg-app-slug", got)
	}

	// A rail that does not expose AppSlug (any SourceControlAccess without the method)
	// yields "" — the omit-allowed_bots guard.
	if got := RailAppSlug(railWithoutSlug{}); got != "" {
		t.Errorf("RailAppSlug(rail-without-AppSlug) = %q, want empty", got)
	}
}

// railWithoutSlug is a SourceControlAccess that does NOT implement AppSlug() (like a
// test fake), used to prove RailAppSlug degrades to "". All methods panic — RailAppSlug
// only type-asserts, it never calls them.
type railWithoutSlug struct{}

func (railWithoutSlug) AdoptProjectRepo(fwra.Context, RepoAdoptionSpec) (RepoRef, error) {
	panic("unused")
}
func (railWithoutSlug) CommitManagedFiles(fwra.Context, RepoRef, []ManagedFile, RepoCredential) (CommitRef, error) {
	panic("unused")
}
func (railWithoutSlug) ConfigureBranchProtection(fwra.Context, RepoRef, RepoCredential) error {
	panic("unused")
}
func (railWithoutSlug) GetInstallationToken(fwra.Context, RepoRef) (RepoCredential, error) {
	panic("unused")
}
func (railWithoutSlug) GetPullRequestStatus(fwra.Context, RepoRef, PullRequestRef, RepoCredential) (PullRequestStatus, error) {
	panic("unused")
}
func (railWithoutSlug) InstallAuthorizeApp(fwra.Context, AccountRef) (Installation, error) {
	panic("unused")
}
func (railWithoutSlug) MergePullRequest(fwra.Context, RepoRef, PullRequestRef, RepoCredential) (MergeResult, error) {
	panic("unused")
}
func (railWithoutSlug) OpenBranch(fwra.Context, RepoRef, BranchName, RepoCredential) (BranchRef, error) {
	panic("unused")
}
func (railWithoutSlug) OpenPullRequest(fwra.Context, RepoRef, PullRequestSpec, RepoCredential) (PullRequestRef, error) {
	panic("unused")
}
func (railWithoutSlug) PostReview(fwra.Context, RepoRef, PullRequestRef, ReviewSubmission, RepoCredential) error {
	panic("unused")
}
func (railWithoutSlug) SyncManagedScaffold(fwra.Context, RepoRef, RepoCredential) (bool, error) {
	panic("unused")
}

var _ SourceControlAccess = railWithoutSlug{}

// TestStateMcpPinIsNotABranch guards the managed-scaffold sync's premise: the pin the
// template renders (and the sync converges seated copies onto) must be a FIXED ref —
// a commit SHA or a tag the release process moves deliberately — never a branch name.
// GOPROXY caches branch→pseudo-version resolutions, so a branch pin can silently serve
// a stale binary (the drift class the sync exists to eliminate); with a branch pin the
// seated bytes also never change, so the sync could not even detect the drift.
func TestStateMcpPinIsNotABranch(t *testing.T) {
	if strings.TrimSpace(StateMcpModulePin) == "" {
		t.Fatal("StateMcpModulePin must not be empty")
	}
	for _, branch := range []string{"main", "master", "HEAD"} {
		if StateMcpModulePin == branch {
			t.Fatalf("StateMcpModulePin must be a fixed ref (commit SHA or tag), not the branch %q", branch)
		}
	}
}

// TestStateMcpPinIsFullCommitSHA pins the pin's SHAPE: a full 40-hex-char commit SHA.
// This is the runtime half of the F-QA2-23 tool-surface coupling (see the doc comment
// on StateMcpModulePin): the prompt-surface gate
// (cmd/aiarch-state-mcp/promptsurface_test.go TestPromptSurfaceToolReferencesExistInRegistry)
// proves every prompt-referenced tool exists at HEAD source, and a full unambiguous SHA
// is what makes "bump the pin to a pushed commit that has the tool" a deterministic,
// auditable act (an abbreviated SHA or a tag-typo would be resolved — or not — by
// GOPROXY heuristics). If the release process ever moves to server/vX.Y.Z tags, loosen
// this DELIBERATELY in the same change that updates the pin doctrine above.
func TestStateMcpPinIsFullCommitSHA(t *testing.T) {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(StateMcpModulePin) {
		t.Fatalf("StateMcpModulePin = %q; want a full 40-char lowercase hex commit SHA "+
			"(pushed, and carrying every tool the method-assets prompts reference — see the pin's doc comment)", StateMcpModulePin)
	}
}

// TestDesignWorkflowFileIsTheSeatRendering proves the sync and the birth seat share
// ONE rendering: DesignWorkflowFile equals the DesignWorkflowPath entry of the
// ManagedScaffoldFiles birth bundle byte-for-byte, so sync-on-dispatch converges the
// seated copy onto exactly what a fresh seat would commit today.
func TestDesignWorkflowFileIsTheSeatRendering(t *testing.T) {
	single, err := DesignWorkflowFile(testAppSlug)
	if err != nil {
		t.Fatalf("DesignWorkflowFile: %v", err)
	}
	if single.Path != DesignWorkflowPath {
		t.Fatalf("DesignWorkflowFile path = %q, want %q", single.Path, DesignWorkflowPath)
	}
	bundle, err := ManagedScaffoldFiles(RepoRef("acct|acme/proj"), testAppSlug)
	if err != nil {
		t.Fatalf("ManagedScaffoldFiles: %v", err)
	}
	var seat []byte
	for _, f := range bundle {
		if f.Path == DesignWorkflowPath {
			seat = f.Content
		}
	}
	if seat == nil {
		t.Fatalf("seat bundle is missing %s", DesignWorkflowPath)
	}
	if !bytes.Equal(single.Content, seat) {
		t.Fatal("DesignWorkflowFile and the ManagedScaffoldFiles seat entry must be the SAME rendering (seat and sync can never disagree)")
	}
}

// TestSyncManagedScaffoldMessageNamesGenerations pins the sync commit-message
// contract: it names BOTH generations the refreshed rendering carries — the
// method-assets module version (the prompt surface) and the state-MCP pin (the
// workflows' validator binary) — so the repo history records exactly what the
// scaffold was synced to. The methodassets version is asserted DYNAMICALLY
// (build-info derived; never a literal).
func TestSyncManagedScaffoldMessageNamesGenerations(t *testing.T) {
	msg := syncManagedScaffoldMessage()
	want := "aiarch: sync managed scaffold to method-assets@" + methodassets.Version() +
		" aiarch-state-mcp@" + StateMcpModulePin
	if msg != want {
		t.Fatalf("syncManagedScaffoldMessage() = %q, want %q", msg, want)
	}
}
