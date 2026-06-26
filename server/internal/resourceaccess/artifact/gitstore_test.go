package artifact

// Black-box-style regression suite for the git-backed artifactAccess Store
// (C-AA-R rework; Gitea removed per the 2026-06-09 git-only pivot). Per The
// Method's testing doctrine (NO BDD, regression-first): the tests drive the RA's
// PUBLIC ArtifactAccess verbs and fake ONLY the external git boundary —
// the LOCAL profile runs against a REAL throwaway on-disk git repo
// (testinfra.LocalGitRepo) over go-git's file:// transport, mirroring C-PA-R's
// gitstore_test.go; the CLOUD profile runs against a fake AppClient to prove the
// internal-token-mint auth path threads a non-local GitAuth.
//
// This file lives IN-PACKAGE (package artifact) so it can build a Store over the
// satellite *GitBlobStore via the internal newStore + a test auth source, and so
// the cloud-token path can be exercised with a fake appClient. It still drives the
// public surface (StoreConstructionOutput / RetrieveConstructionOutput /
// RetrieveOutputTree) exactly as an external caller would.
//
// Coverage (mirrors the retired gitea_test.go so nothing regresses):
//  1. store -> non-empty content-address string; retrieve round-trips bytes+MIME.
//  2. content-addressability / DETERMINISTIC-SHA DEDUP: identical content twice
//     (even under different idempotency keys) -> the SAME address, no duplicate;
//     different content -> a NEW address, the prior retained (immutable history).
//  3. retrieveOutputTree -> flat path->address Entries; each entry resolves;
//     unknown root -> fwra.NotFound.
//  4. error-kind mapping: unknown address -> fwra.NotFound; malformed/empty ->
//     fwra.ContractMisuse (before any git IO); empty content/key -> ContractMisuse.
//  5. BOTH profiles: local path against the real repo; cloud token path against a
//     fake AppClient (asserts a non-local GitAuth is threaded).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// rcWith builds the ResourceAccess call Context the port now takes from a plain
// ctx + idempotency key (the cross-cutting params the hand-written surface passed
// explicitly now ride fwra.Context). Tests that don't exercise the key pass "".
func rcWith(ctx context.Context, key fwra.IdempotencyKey) fwra.Context {
	return fwra.Context{Context: ctx, IdempotencyKey: key}
}

// newLocalTestStore builds a Store over a REAL throwaway on-disk git repo using
// the LOCAL profile (GitAuth.Local). This is the genuine git store the testing
// doctrine mandates (skips if git is not on PATH).
func newLocalTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	repo := gh.StartLocalGitRepo(t, "main")
	store, err := NewLocalStore(repo.URL)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return store, context.Background()
}

// newDummyStore builds a Store without any repo. Used only for the pre-condition
// guards, which short-circuit BEFORE any git IO / auth.
func newDummyStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	store, err := NewLocalStore("file:///nonexistent/repo.git")
	if err != nil {
		t.Fatalf("NewLocalStore (dummy): %v", err)
	}
	return store, context.Background()
}

func assertKind(t *testing.T, err error, want fwra.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %s, got nil", want)
	}
	var e *fwra.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fwra.Error, got %T: %v", err, err)
	}
	if e.Kind != want {
		t.Fatalf("expected kind %s, got %s (detail: %s)", want, e.Kind, e.Detail)
	}
}

// TestStoreContractMisuse — pre-condition violations are caught as
// fwra.ContractMisuse before any git IO / auth.
func TestStoreContractMisuse(t *testing.T) {
	store, ctx := newDummyStore(t)
	good := ConstructionOutput{Bytes: []byte("x"), MIMEType: "text/plain"}

	storeCases := []struct {
		name    string
		content ConstructionOutput
		key     fwra.IdempotencyKey
	}{
		{"nil content bytes", ConstructionOutput{Bytes: nil, MIMEType: "text/plain"}, "k"},
		{"empty content bytes", ConstructionOutput{Bytes: []byte{}, MIMEType: "text/plain"}, "k"},
		{"whitespace idempotency key", good, "  "},
		{"empty idempotency key", good, ""},
	}
	for _, tc := range storeCases {
		t.Run("Store/"+tc.name, func(t *testing.T) {
			_, err := store.StoreConstructionOutput(rcWith(ctx, tc.key), tc.content)
			assertKind(t, err, fwra.ContractMisuse)
		})
	}

	retrieveCases := []struct {
		name    string
		address string
	}{
		{"empty address", ""},
		{"no separator", "noseparator"},
		{"no leading sha", ":noleadingsha"},
	}
	for _, tc := range retrieveCases {
		t.Run("RetrieveConstructionOutput/"+tc.name, func(t *testing.T) {
			_, err := store.RetrieveConstructionOutput(rcWith(ctx, ""), tc.address)
			assertKind(t, err, fwra.ContractMisuse)
		})
		t.Run("RetrieveOutputTree/"+tc.name, func(t *testing.T) {
			_, err := store.RetrieveOutputTree(rcWith(ctx, ""), tc.address)
			assertKind(t, err, fwra.ContractMisuse)
		})
	}
}

// TestStoreRetrieveRoundTrip — store -> non-empty address; retrieve round-trips
// bytes + MIMEType (LOCAL profile, real git).
func TestStoreRetrieveRoundTrip(t *testing.T) {
	store, ctx := newLocalTestStore(t)
	want := ConstructionOutput{Bytes: []byte("package main\n\nfunc main() {}\n"), MIMEType: "text/x-go"}

	addr, err := store.StoreConstructionOutput(rcWith(ctx, "wf-1:act-1"), want)
	if err != nil {
		t.Fatalf("StoreConstructionOutput: %v", err)
	}
	if addr == "" {
		t.Fatal("expected non-empty content address")
	}

	got, err := store.RetrieveConstructionOutput(rcWith(ctx, ""), addr)
	if err != nil {
		t.Fatalf("RetrieveConstructionOutput: %v", err)
	}
	if string(got.Bytes) != string(want.Bytes) {
		t.Fatalf("bytes mismatch: got %q want %q", got.Bytes, want.Bytes)
	}
	if got.MIMEType != want.MIMEType {
		t.Fatalf("MIMEType mismatch: got %q want %q", got.MIMEType, want.MIMEType)
	}
}

// TestContentAddressable_SameContentSameAddress — DETERMINISTIC-SHA DEDUP: storing
// identical content twice (even under different idempotency keys) converges on the
// same address with no duplicate.
func TestContentAddressable_SameContentSameAddress(t *testing.T) {
	store, ctx := newLocalTestStore(t)
	content := ConstructionOutput{Bytes: []byte("helm chart bytes"), MIMEType: "application/yaml"}

	addr1, err := store.StoreConstructionOutput(rcWith(ctx, "wf-1:act-1"), content)
	if err != nil {
		t.Fatalf("StoreConstructionOutput #1: %v", err)
	}
	// Same content, DIFFERENT idempotency key — content addressing must still
	// converge on the same address (dedup on content, no duplicate).
	addr2, err := store.StoreConstructionOutput(rcWith(ctx, "wf-2:act-9"), content)
	if err != nil {
		t.Fatalf("StoreConstructionOutput #2: %v", err)
	}
	if addr1 != addr2 {
		t.Fatalf("expected identical addresses for identical content, got %q vs %q", addr1, addr2)
	}
	// Same key retried also dedups (the common Manager-retry path).
	addr3, err := store.StoreConstructionOutput(rcWith(ctx, "wf-1:act-1"), content)
	if err != nil {
		t.Fatalf("StoreConstructionOutput #3: %v", err)
	}
	if addr1 != addr3 {
		t.Fatalf("expected dedup on retry, got %q vs %q", addr1, addr3)
	}
}

// TestContentAddressable_DifferentContentDifferentAddress — different content
// yields a NEW address; the prior output is retained (immutable history).
func TestContentAddressable_DifferentContentDifferentAddress(t *testing.T) {
	store, ctx := newLocalTestStore(t)

	v1 := ConstructionOutput{Bytes: []byte("build output one"), MIMEType: "text/plain"}
	v2 := ConstructionOutput{Bytes: []byte("build output two"), MIMEType: "text/plain"}

	addr1, err := store.StoreConstructionOutput(rcWith(ctx, "wf-1:act-1"), v1)
	if err != nil {
		t.Fatalf("StoreConstructionOutput v1: %v", err)
	}
	addr2, err := store.StoreConstructionOutput(rcWith(ctx, "wf-1:act-2"), v2)
	if err != nil {
		t.Fatalf("StoreConstructionOutput v2: %v", err)
	}
	if addr1 == addr2 {
		t.Fatalf("expected distinct addresses for distinct content, both %q", addr1)
	}

	// The prior output is NOT overwritten — its address still resolves to v1.
	got1, err := store.RetrieveConstructionOutput(rcWith(ctx, ""), addr1)
	if err != nil {
		t.Fatalf("RetrieveConstructionOutput prior: %v", err)
	}
	if string(got1.Bytes) != string(v1.Bytes) {
		t.Fatalf("prior output mutated: got %q want %q", got1.Bytes, v1.Bytes)
	}
	got2, err := store.RetrieveConstructionOutput(rcWith(ctx, ""), addr2)
	if err != nil {
		t.Fatalf("RetrieveConstructionOutput new: %v", err)
	}
	if string(got2.Bytes) != string(v2.Bytes) {
		t.Fatalf("new output wrong: got %q want %q", got2.Bytes, v2.Bytes)
	}
}

// TestRetrieveUnknownAddress — an unknown (well-formed) content address resolves to
// fwra.NotFound.
func TestRetrieveUnknownAddress(t *testing.T) {
	store, ctx := newLocalTestStore(t)
	unknown := "0123456789abcdef0123456789abcdef01234567:output.bin"
	_, err := store.RetrieveConstructionOutput(rcWith(ctx, ""), unknown)
	assertKind(t, err, fwra.NotFound)
}

// TestRetrieveOutputTree — retrieveOutputTree returns the flat path->address
// Entries snapshot; every entry resolves; a second distinct store yields its own
// tree; an unknown root -> fwra.NotFound.
func TestRetrieveOutputTree(t *testing.T) {
	store, ctx := newLocalTestStore(t)

	a := ConstructionOutput{Bytes: []byte("file a contents"), MIMEType: "text/plain"}
	b := ConstructionOutput{Bytes: []byte("file b contents"), MIMEType: "text/plain"}

	addrA, err := store.StoreConstructionOutput(rcWith(ctx, "wf:a"), a)
	if err != nil {
		t.Fatalf("StoreConstructionOutput a: %v", err)
	}

	tree, err := store.RetrieveOutputTree(rcWith(ctx, ""), addrA)
	if err != nil {
		t.Fatalf("RetrieveOutputTree: %v", err)
	}
	if tree.Root != addrA {
		t.Fatalf("tree Root = %q, want %q", tree.Root, addrA)
	}
	if len(tree.Entries) == 0 {
		t.Fatal("expected at least one tree entry")
	}
	for path, entryAddr := range tree.Entries {
		if _, err := store.RetrieveConstructionOutput(rcWith(ctx, ""), entryAddr); err != nil {
			t.Fatalf("RetrieveConstructionOutput entry %q (%q): %v", path, entryAddr, err)
		}
	}
	var foundA bool
	for path, entryAddr := range tree.Entries {
		if !strings.HasPrefix(string(path), "output") {
			continue
		}
		got, err := store.RetrieveConstructionOutput(rcWith(ctx, ""), entryAddr)
		if err != nil {
			t.Fatalf("RetrieveConstructionOutput output entry %q: %v", path, err)
		}
		if string(got.Bytes) != string(a.Bytes) {
			t.Fatalf("output entry %q bytes mismatch: got %q want %q", path, got.Bytes, a.Bytes)
		}
		foundA = true
	}
	if !foundA {
		t.Fatalf("expected an 'output' entry in the tree, got %v", tree.Entries)
	}

	addrB, err := store.StoreConstructionOutput(rcWith(ctx, "wf:b"), b)
	if err != nil {
		t.Fatalf("StoreConstructionOutput b: %v", err)
	}
	if addrB == addrA {
		t.Fatalf("expected distinct roots, both %q", addrA)
	}
	treeB, err := store.RetrieveOutputTree(rcWith(ctx, ""), addrB)
	if err != nil {
		t.Fatalf("RetrieveOutputTree b: %v", err)
	}
	if treeB.Root != addrB {
		t.Fatalf("treeB Root = %q, want %q", treeB.Root, addrB)
	}
	var foundB bool
	for path, entryAddr := range treeB.Entries {
		if !strings.HasPrefix(string(path), "output") {
			continue
		}
		got, err := store.RetrieveConstructionOutput(rcWith(ctx, ""), entryAddr)
		if err != nil {
			t.Fatalf("RetrieveConstructionOutput treeB output entry %q: %v", path, err)
		}
		if string(got.Bytes) != string(b.Bytes) {
			t.Fatalf("treeB output entry %q bytes mismatch: got %q want %q", path, got.Bytes, b.Bytes)
		}
		foundB = true
	}
	if !foundB {
		t.Fatalf("expected an 'output' entry in treeB, got %v", treeB.Entries)
	}
}

// TestRetrieveOutputTreeUnknown — an unknown tree-root address -> fwra.NotFound.
func TestRetrieveOutputTreeUnknown(t *testing.T) {
	store, ctx := newLocalTestStore(t)
	_, err := store.RetrieveOutputTree(rcWith(ctx, ""), "0123456789abcdef0123456789abcdef01234567:output.bin")
	assertKind(t, err, fwra.NotFound)
}

// --- CLOUD profile: internal-token-mint auth path ---------------------------

// fakeAppClient is a stub satellite AppClient that records the minted token and
// hands back a fixed one, so the cloud auth source can be exercised without any
// network IO.
type fakeAppClient struct {
	installID int64
	token     string
	mintCalls int
	findCalls int
}

func (f *fakeAppClient) FindInstallation(_ context.Context, _ string) (int64, error) {
	f.findCalls++
	return f.installID, nil
}

func (f *fakeAppClient) MintInstallationToken(_ context.Context, _ int64) (string, time.Time, error) {
	f.mintCalls++
	return f.token, time.Now().Add(time.Hour), nil
}

// capturingBlob records the GitAuth threaded into each call, so the cloud test can
// assert a non-local token credential reaches the satellite.
type capturingBlob struct {
	lastAuth fwgithub.GitAuth
}

func (c *capturingBlob) StoreOutput(_ context.Context, _ string, _ []fwgithub.GitObjectFile, _ string, auth fwgithub.GitAuth) (string, error) {
	c.lastAuth = auth
	return "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", nil
}

func (c *capturingBlob) ReadFileAtCommit(_ context.Context, _, _ string, auth fwgithub.GitAuth) ([]byte, error) {
	c.lastAuth = auth
	return nil, fwra.New(fwra.NotFound, "not found")
}

func (c *capturingBlob) ProbeFileAtBranchTip(_ context.Context, _, _ string, auth fwgithub.GitAuth) ([]byte, string, bool, error) {
	c.lastAuth = auth
	return nil, "", false, nil
}

func (c *capturingBlob) WalkTreeFiles(_ context.Context, _ string, auth fwgithub.GitAuth) ([]string, error) {
	c.lastAuth = auth
	return nil, nil
}

// TestCloudProfile_InternalTokenMint — the CLOUD profile mints the installation
// token INTERNALLY (no credential on the surface, no sibling-RA call) and threads a
// NON-local GitAuth.Token into the satellite. Same surface, different auth source.
func TestCloudProfile_InternalTokenMint(t *testing.T) {
	app := &fakeAppClient{installID: 42, token: "ghs-fake-installation-token"}
	blob := &capturingBlob{}
	store := newStore(blob, &cloudAuth{app: app, owner: "acme"})

	addr, err := store.StoreConstructionOutput(rcWith(context.Background(), "wf:c"),
		ConstructionOutput{Bytes: []byte("cloud bytes"), MIMEType: "text/plain"})
	if err != nil {
		t.Fatalf("StoreConstructionOutput (cloud): %v", err)
	}
	if addr == "" {
		t.Fatal("expected non-empty cloud address")
	}
	if blob.lastAuth.Local {
		t.Fatal("cloud profile threaded a LOCAL GitAuth; expected a token credential")
	}
	if blob.lastAuth.Token != app.token {
		t.Fatalf("cloud GitAuth.Token = %q, want minted token %q", blob.lastAuth.Token, app.token)
	}
	if app.mintCalls == 0 {
		t.Fatal("expected the installation token to be minted internally")
	}
}
