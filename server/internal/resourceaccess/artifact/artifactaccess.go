// Package artifact is the artifactAccess component of the ResourceAccess layer —
// the git-backed store for produced design/construction artifact payloads,
// addressed by content hash and scoped per project.
package artifact

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	githubinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// gitstore.go is the concrete, git-backed implementation of the ArtifactAccess
// port for PHASE-3 CONSTRUCTION OUTPUTS (artifactAccess.md §6 infrastructure
// mapping), reworked for the 2026-06-09 git-only pivot: the backing store is the
// SAME per-project git repo projectStateAccess now fronts (NO Gitea). It targets
// BOTH deployment profiles behind the unchanged, infrastructure-opaque surface:
//
//   - Cloud/remote profile — the user's GitHub repo per project; git-HTTP auth via
//     a GitHub-App installation token minted INTERNALLY (App-JWT ->
//     MintInstallationToken) by the `auth` resolver the composition root supplies.
//     The RA NEVER threads a credential through its surface and NEVER calls a
//     sibling RA (NoSideways) — the layer-legal auth resolution C-CP-R's senior
//     review ratified and the contract §6/Non-goal #11 prose ("token
//     acquired/refreshed inside the package") prescribes.
//   - Local/embedded profile — the user's local on-disk git repo over a `file://`
//     remote; no HTTP credential (GitAuth.Local).
//
// INFRASTRUCTURE MAPPING (caller-opaque; for the senior reviewer and future
// maintainers, per artifactAccess.md §6):
//
//   - There is NO logical (project, kind, variant) addressing — content is the key
//     (decision 3). A stored output lands at a content file ("output{ext}", ext
//     derived from the advisory MIMEType) plus a sidecar "meta.json" recording the
//     MIMEType (git stores bytes only; the advisory hint must persist alongside so
//     retrieve can return it faithfully). The commit lives on a CONTENT-DERIVED
//     branch ("aiarch/output/{contentHash}") so distinct content never contends and
//     identical content collapses to one branch tip. The commit message is
//     "aiarch: {idempotencyKey}" (the key is opaque here).
//   - The returned content address is "{commitToken}:{contentPath}" — a plain
//     string (decision 2; NO ArtifactRef wrapper). commitToken is the satellite's
//     opaque commit-identity token; callers compare the address by value (==) and
//     never parse it.
//   - CONTENT-ADDRESSABILITY + DEDUP: identical content collapses to the same
//     commit token (the satellite fixes author/committer/time so the hash is a pure
//     function of the tree). Before committing, the Store probes the content-derived
//     branch tip; if its stored content+MIMEType are byte-identical it returns the
//     EXISTING address with NO new commit. Different content lands on a different
//     branch / address; the prior output is never overwritten (immutable history).
//   - RetrieveConstructionOutput resolves an address back to its bytes+MIMEType via
//     the satellite (read the content file and its sibling meta.json at the commit).
//   - RetrieveOutputTree resolves the address's commit, flattens its tree into a
//     path->address map (each entry address is "{commitToken}:{entryPath}", itself
//     retrievable via RetrieveConstructionOutput), pinned to the queried address.
//
// The receiver imports NO Temporal (layer rule): the idempotencyKey arrives as an
// ordinary parameter and is never read from ambient context. Git vocabulary lives
// only in the github satellite — this RA names no SHA/blob/tree/owner/repo on its
// surface or returned types.
//
// The receiver, the *GitArtifactAccess struct (holding the satellite blob handle
// `git` + the per-call auth resolver `auth`), and its public constructor
// NewGitArtifactAccess are GENERATED into contract.gen.go from the contract's
// `infra: ["Git"]` binding. The two profiles (LOCAL => GitAuth{Local:true}; CLOUD
// => an internally-minted installation token) are supplied as the `auth` resolver
// by the composition root; auth NEVER crosses the contract surface, and the RA
// NEVER calls a sibling RA to obtain it (NoSideways). The behaviour below is
// hand-written on the generated struct.

// StoreConstructionOutput commits the construction output and returns its
// content-address string. Storing byte-identical content (same bytes AND same
// MIMEType) returns the EXISTING address without producing a new commit
// (artifactAccess.md §2.1 content-addressable idempotency). Storing different
// content yields a NEW address; the prior output is retained (immutable history).
func (g *GitArtifactAccess) StoreConstructionOutput(rc fwra.Context, content ConstructionOutput) (string, error) {
	if len(content.Bytes) == 0 {
		return "", fwra.New(fwra.ContractMisuse, "artifact.StoreConstructionOutput: empty content bytes")
	}
	if strings.TrimSpace(string(rc.IdempotencyKey)) == "" {
		return "", fwra.New(fwra.ContractMisuse, "artifact.StoreConstructionOutput: empty idempotencyKey")
	}

	ctx := rc.Context
	auth, err := g.auth(ctx)
	if err != nil {
		return "", err
	}

	contentPath := contentPathFor(content.MIMEType)
	branch := branchFor(content)

	// Dedup probe: a content-derived branch already holding byte-identical
	// content+MIMEType collapses to the EXISTING address with no new commit
	// (artifactAccess.md §2.1). This is what gives "same content -> same address".
	if existing, tipToken, found, derr := g.git.ProbeFileAtBranchTip(ctx, branch, contentPath, auth); derr != nil {
		return "", derr
	} else if found && bytes.Equal(existing, content.Bytes) {
		// Confirm the sidecar MIMEType matches too (same bytes + same MIMEType is the
		// content-addressable identity; the branch is keyed on both, so a hit here is
		// near-certain, but verify the meta to be exact).
		if metaBytes, _, mfound, merr := g.git.ProbeFileAtBranchTip(ctx, branch, metaFile, auth); merr == nil && mfound && decodeMeta(metaBytes) == content.MIMEType {
			return makeAddress(tipToken, contentPath), nil
		}
	}

	files := []githubinfra.GitObjectFile{
		{Path: contentPath, Bytes: content.Bytes},
		{Path: metaFile, Bytes: encodeMeta(content.MIMEType)},
	}
	commitToken, err := g.git.StoreOutput(ctx, branch, files, commitMessage(rc.IdempotencyKey), auth)
	if err != nil {
		return "", err
	}
	return makeAddress(commitToken, contentPath), nil
}

// RetrieveConstructionOutput resolves a content address back to its
// ConstructionOutput. An unknown / unresolvable address surfaces as fwra.NotFound
// (artifactAccess.md §2.2).
func (g *GitArtifactAccess) RetrieveConstructionOutput(rc fwra.Context, contentAddress string) (ConstructionOutput, error) {
	commitToken, contentPath, err := parseAddress(contentAddress)
	if err != nil {
		return ConstructionOutput{}, err
	}
	ctx := rc.Context
	auth, err := g.auth(ctx)
	if err != nil {
		return ConstructionOutput{}, err
	}

	contentBytes, err := g.git.ReadFileAtCommit(ctx, commitToken, contentPath, auth)
	if err != nil {
		return ConstructionOutput{}, err
	}
	mime := ""
	if metaBytes, mErr := g.git.ReadFileAtCommit(ctx, commitToken, metaFile, auth); mErr == nil {
		mime = decodeMeta(metaBytes)
	}
	// else: an address without a sibling meta (externally-created) yields an empty
	// MIMEType — advisory-only per artifactAccess.md §3, so this is benign.

	return ConstructionOutput{Bytes: contentBytes, MIMEType: mime}, nil
}

// RetrieveOutputTree resolves the commit at contentAddress and returns its flat
// path->content-address snapshot (artifactAccess.md §2.3). Every entry address is
// itself a content address resolvable by RetrieveConstructionOutput. An unknown
// address surfaces as fwra.NotFound.
func (g *GitArtifactAccess) RetrieveOutputTree(rc fwra.Context, contentAddress string) (OutputTree, error) {
	commitToken, _, err := parseAddress(contentAddress)
	if err != nil {
		return OutputTree{}, err
	}
	ctx := rc.Context
	auth, err := g.auth(ctx)
	if err != nil {
		return OutputTree{}, err
	}

	paths, err := g.git.WalkTreeFiles(ctx, commitToken, auth)
	if err != nil {
		return OutputTree{}, err
	}

	// The generated OutputTree.Entries map is keyed by string (JSON Schema map keys
	// are always strings); the logical OutputPath key bridges to its string form at
	// this boundary (a within-package conversion — OutputPath is the contract's own
	// named scalar, but the map key carries the bare string per the wire shape).
	entries := map[string]string{}
	for _, name := range paths {
		// Each file entry is addressed by the SAME commit token + its path, so it
		// round-trips through RetrieveConstructionOutput.
		entries[string(OutputPath(name))] = makeAddress(commitToken, name)
	}
	return OutputTree{Root: contentAddress, Entries: entries}, nil
}

// Package artifact is the artifactAccess component of the aiarch server's
// ResourceAccess layer — the Temporal-free port over the content-addressable
// store for PHASE-3 CONSTRUCTION OUTPUTS only (artifactAccess.md, re-cut
// 2026-05-26). It mediates every write into and read out of the construction
// store: generated source trees, compiled build artifacts, helm charts, k8s
// manifests, deployable bundles. It does NOT store Phase-1/2 Method artifacts —
// those are typed domain models in projectStateAccess.
//
// Per The Method's layer model ([[the-method-layers]]): ResourceAccess
// components import NO Temporal. The calling Manager wraps each verb below in a
// Manager-owned Temporal Activity and passes the idempotencyKey in as an
// ordinary parameter; this package never reads Temporal context.
//
// The shared value types ConstructionOutput / OutputTree / OutputPath are owned
// HERE (construction.go) — this is the RA that stores them. workerAccess —
// which PRODUCES a ConstructionOutput — imports them as a downward edge.
//
// Derived faithfully from the frozen artifactAccess.md contract (Phase-3 CAS).

// The ArtifactAccess port interface and its I/O value types (ConstructionOutput,
// OutputTree, and the named scalar OutputPath) are now GENERATED from
// this component's `.serviceContracts` entry in .aiarch/state/project.json into
// contract.gen.go (schema-first; edit that entry and run
// `make gen`). Each method takes the ResourceAccess call Context `rc fwra.Context`
// first — it embeds context.Context and carries the Principal + IdempotencyKey, so
// the cross-cutting ctx/idempotencyKey that the hand-written surface passed
// explicitly now ride the context. The design rationale not captured by the
// generated signatures:
//
//   - StoreConstructionOutput establishes a content-addressable identity for one
//     output. Storing identical content returns the SAME address (no duplicate);
//     storing different content yields a NEW address (the prior is retained —
//     immutable history). The caller-supplied rc.IdempotencyKey goes into the infra
//     commit message; this method never reads Temporal.
//   - RetrieveConstructionOutput is a pure by-address read; an unknown address
//     surfaces as fwra.NotFound. Byte-identical across retries.
//   - RetrieveOutputTree returns the flat path->content-address snapshot at a
//     tree-root address; an unknown address surfaces as fwra.NotFound.
//
// The shared ResourceAccess error model is framework-go's fwra.Error, constructed
// with fwra.New / fwra.Wrap using the shared kinds (fwra.NotFound, fwra.Conflict,
// fwra.Auth, fwra.Transient, fwra.Infrastructure, fwra.ContractMisuse). It is used
// directly (no package-local alias) so this package exports ONLY its generated
// contract surface.

// This file documents the shared Phase-3 construction-output value types. They are
// now GENERATED from the artifactAccess `.serviceContracts` entry in
// .aiarch/state/project.json into contract.gen.go (schema-first; edit
// that entry and run `make gen`). They live HERE in artifactAccess (the RA that
// STORES them); workerAccess — which PRODUCES a ConstructionOutput — references them
// directly. The shared value vocabulary is owned by the RA that fronts the resource
// it describes (per The Method's layer model: ResourceAccess owns its value types
// and exposes them on its port).
//
// The generated types and their design rationale (not captured by the generated
// declarations):
//
//   - ConstructionOutput is a Phase-3 build product (opaque bytes + advisory MIME).
//     Canonical shared value type per artifactAccess.md §3; owned here, referenced
//     by workerAccess (which produces a ConstructionOutput as a downward import —
//     the worker RA can read the artifact RA's value types because they sit at the
//     same layer and the producer→store value flow is the natural direction).
//   - OutputTree is a frozen path->content-address snapshot (artifactAccess.md §3).
//     Its Entries map is generated as map[string]string (JSON Schema map keys are
//     always strings); the impl bridges the logical OutputPath key at the boundary.
//   - OutputPath is a logical, slash-separated path within an OutputTree
//     (artifactAccess.md §3). Infrastructure-opaque.

// helpers.go holds the INFRASTRUCTURE-PRIVATE address/branch/meta encoding for the
// content-addressable construction-output store. It imports NO git package — git
// vocabulary is confined to the github satellite (gitstore.go delegates to it).
// These helpers only shape the opaque content-address string and the on-store
// path/branch/meta encoding the RA controls.

// metaFile is the sidecar path inside each output's commit carrying the MIMEType
// hint (git stores bytes only — the advisory MIMEType must be persisted to be
// faithfully returned on retrieve).
const metaFile = "meta.json"

// addrSeparator joins the opaque commit-identity token and the content path inside
// the content-address string. A colon separates the hex commit token (never
// contains ':') from the content path: parseAddress splits on the FIRST ':'
// (SplitN, 2), so a path containing ':' still round-trips. A colon — not a NUL — is
// used deliberately: the address is persisted into projectStateAccess head-state
// (git JSON / Postgres JSONB), Temporal payloads, and logs, and must be safe text.
const addrSeparator = ":"

// branchFor derives the deterministic, INFRASTRUCTURE-PRIVATE content-derived
// branch name from a hash of the output's content+MIMEType:
// "aiarch/output/{contentHash}". There is no logical (project, kind, variant)
// addressing — content is the key (artifactAccess.md decision 3). Distinct content
// lands on distinct branches (never contends); identical content lands on one
// branch whose tip the dedup probe collapses to a single address. The branch name
// is never exposed on the contract (artifactAccess.md §6).
func branchFor(content ConstructionOutput) string {
	h := sha256.New()
	h.Write([]byte(content.MIMEType))
	h.Write([]byte{0})
	h.Write(content.Bytes)
	return "aiarch/output/" + hex.EncodeToString(h.Sum(nil))
}

// contentPathFor derives the content file path from the advisory MIMEType. The
// extension is best-effort (the MIMEType is advisory per artifactAccess.md §3); an
// unknown/empty MIMEType yields a bare "output" with no extension. Deterministic so
// the same MIMEType always maps to the same path.
func contentPathFor(mimeType string) string {
	return "output" + extFor(mimeType)
}

// extFor maps a MIMEType to a file extension. Conservative: only a small set of
// construction-relevant types is recognised; everything else falls back to ""
// (no extension). Correctness does not depend on this — retrieve reads the path
// recorded in the address, not a re-derived one.
func extFor(mimeType string) string {
	switch normalizeMIME(mimeType) {
	case "text/markdown":
		return ".md"
	case "text/plain":
		return ".txt"
	case "text/x-go", "text/x-gosrc":
		return ".go"
	case "application/json":
		return ".json"
	case "application/yaml", "text/yaml", "application/x-yaml":
		return ".yaml"
	case "application/octet-stream":
		return ".bin"
	default:
		return ""
	}
}

// normalizeMIME strips any parameters (e.g. "; charset=utf-8") and lowercases.
func normalizeMIME(mimeType string) string {
	m := mimeType
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = m[:i]
	}
	return strings.ToLower(strings.TrimSpace(m))
}

// metaDoc is the on-infrastructure JSON shape of the sidecar meta file. A dedicated
// wire struct keeps the infrastructure encoding decoupled from the port value
// object.
type metaDoc struct {
	MIMEType string `json:"mimeType"`
}

func encodeMeta(mimeType string) []byte {
	b, _ := json.Marshal(metaDoc{MIMEType: mimeType})
	return b
}

func decodeMeta(b []byte) string {
	var d metaDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return ""
	}
	return d.MIMEType
}

// makeAddress builds the opaque content-address string from the satellite's opaque
// commit-identity token and a content path (artifactAccess.md decision 2 — a plain
// string, no wrapper type). Callers never parse the result — only parseAddress does.
func makeAddress(commitToken, contentPath string) string {
	return commitToken + addrSeparator + contentPath
}

// parseAddress splits an opaque content address back into the commit-identity token
// and content path. A malformed address (not produced by makeAddress) is a caller
// pre-condition violation => fwra.ContractMisuse.
func parseAddress(address string) (commitToken, contentPath string, err error) {
	if address == "" {
		return "", "", fwra.New(fwra.ContractMisuse, "artifact: empty content address")
	}
	parts := strings.SplitN(address, addrSeparator, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fwra.New(fwra.ContractMisuse, "artifact: malformed content address")
	}
	return parts[0], parts[1], nil
}

// commitMessage embeds the caller-supplied idempotencyKey in the commit message
// (artifactAccess.md §6). The key is opaque to this layer.
func commitMessage(key fwra.IdempotencyKey) string {
	return "aiarch: " + string(key)
}

// variant.go holds the deployment-profile VARIANT CONSTRUCTORS for artifactAccess —
// the composition-root policy that used to live in cmd/server (buildArtifactAccess +
// artifact_auth.go) folded into the owning package. Each variant assembles the
// generated GitArtifactAccess (contract.gen.go) over the satellite *GitBlobStore plus
// the profile-specific auth resolver:
//
//	LOCAL  — the user's on-disk git repo over a file:// remote needs no HTTP
//	         credential (GitAuth{Local:true}).
//	CLOUD  — the user's GitHub repo; the short-lived INSTALLATION TOKEN is minted
//	         INTERNALLY (App-JWT -> MintInstallationToken, over the shared *AppClient
//	         the composition root builds once) and cached to expiry. The credential
//	         is NEVER threaded through the RA's contract surface and the RA NEVER
//	         calls a sibling RA to obtain it (NoSideways) — the discipline the
//	         artifactAccess contract §6 / Non-goal #11 prescribes. The same client
//	         backs the test-run store (Actions run artifacts).
//	DRY-RUN — an in-memory stub for the local dogfood/demo profile
//	         (ARCHISTRATOR_CONSTRUCTION_DRYRUN=true); nothing is committed.
//
// These live in the RA package (not internal/, but the RA that owns the contract):
// the git satellite is sanctioned infrastructure plumbing, not a sibling RA, so
// importing it here keeps the no-sideways discipline intact.

// NewGitLocalArtifactAccess builds the LOCAL-profile artifactAccess: a satellite
// *GitBlobStore over the on-disk construction repo plus the no-credential file://
// auth resolver. No network IO at construction.
//
// This is the step-8 A2 composegen VARIANT constructor (variant token GitLocal):
// infra-free, so the generated composition root calls it WITHOUT an error return.
// The former eager empty-repoURL guard is DEFERRED to first use — an empty URL
// yields a store that returns the ContractMisuse error on every operation rather
// than at boot (P1 gap #2 residual: the composition root's nil-guard is lost, but
// the local profile's artifactAccess is swapped for the dry-run stub in the
// dogfood via FinalizeArtifactAccess, and a repo-less non-dryrun local server
// simply never exercises artifact IO).
func NewGitLocalArtifactAccess(repoURL string) ArtifactAccess {
	blob, err := githubinfra.NewGitBlobStore(repoURL)
	if err != nil {
		return erroringArtifactAccess{err: err}
	}
	return NewGitArtifactAccess(blob, localGitAuth())
}

// NewGitHubCloudArtifactAccess builds the CLOUD-profile artifactAccess: a satellite
// *GitBlobStore over the user's GitHub construction repo plus the internal
// token-minting auth resolver (App-JWT -> MintInstallationToken, cached to expiry),
// and the GitHub-Actions-backed test-run store over the SAME shared *AppClient the
// sourcecontrol + agenticjob variants use (the composition root builds it once from
// the github-app credentials; no raw credential crosses into this package). Config
// is validated eagerly (a missing field surfaces as fwra.ContractMisuse) but no
// network IO happens; the installation token is minted lazily on first use.
// installationID 0 ⇒ discovered on first call. The test-run zips cache under the
// user cache dir (archistrator/testruns/<run>/<artifact>.zip).
//
// This is the step-8 A2 composegen VARIANT constructor (variant token GitHubCloud):
// the composition root threads its args via hooks.ArtifactAccessGitHubCloudArgs.
func NewGitHubCloudArtifactAccess(app *githubinfra.AppClient, repoURL, owner string, installationID int64) (ArtifactAccess, error) {
	blob, auth, err := newCloudArtifactStore(app, repoURL, owner, installationID)
	if err != nil {
		return nil, err
	}
	cacheDir, err := defaultTestRunCacheDir()
	if err != nil {
		return nil, err
	}
	token := func(ctx context.Context) (string, error) {
		a, aerr := auth.resolve(ctx)
		return a.Token, aerr
	}
	runs := newCloudTestRunStore(app, owner, repoNameOf(repoURL), token, cacheDir)
	return newCloudArtifactAccess(blob, auth.resolve, runs), nil
}

// repoNameOf extracts the repository name from a GitHub clone URL
// ("https://github.com/acme/proj.git" → "proj"); the Actions artifact routes
// address the repo by owner + name while the blob store takes the URL whole.
func repoNameOf(repoURL string) string {
	u := strings.TrimRight(strings.TrimSpace(repoURL), "/")
	u = strings.TrimSuffix(u, ".git")
	if i := strings.LastIndexAny(u, "/:"); i >= 0 {
		u = u[i+1:]
	}
	return u
}

// erroringArtifactAccess is the deferred-guard store NewGitLocalArtifactAccess
// returns when the repo URL is empty: it carries the construction error and
// returns it from every contract operation, so a misconfigured local artifact
// store fails at first use with a clear ContractMisuse rather than nil-panicking.
type erroringArtifactAccess struct{ err error }

var _ ArtifactAccess = erroringArtifactAccess{}

func (e erroringArtifactAccess) StoreConstructionOutput(fwra.Context, ConstructionOutput) (string, error) {
	return "", e.err
}

func (e erroringArtifactAccess) RetrieveConstructionOutput(fwra.Context, string) (ConstructionOutput, error) {
	return ConstructionOutput{}, e.err
}

func (e erroringArtifactAccess) RetrieveOutputTree(fwra.Context, string) (OutputTree, error) {
	return OutputTree{}, e.err
}

func (e erroringArtifactAccess) GetTestRunIndex(fwra.Context, TestRunRef) (TestRunIndex, error) {
	return TestRunIndex{}, e.err
}

func (e erroringArtifactAccess) ReadTestRunFile(fwra.Context, TestRunRef, string) (ConstructionOutput, error) {
	return ConstructionOutput{}, e.err
}

// ---------------------------------------------------------------------------
// TEST-RUN OPS (deterministic component testing, spec §6.3). The venue's
// test-results artifact — `test-results/**` with `<component>/results.json`, the
// `go test -json` stream, Playwright videos/traces — is a GitHub Actions artifact
// zip in the CLOUD profile and a directory under .aiarch/test-results in the LOCAL
// one. Both are read through the same two contract ops:
//
//   - GetTestRunIndex(run) → the file list (slash-separated, relative to the
//     artifact root, sorted), the bytes of results.json, or Expired=true when the
//     venue's retention window elapsed (a RENDERED state the gate holds on, never a
//     500 — Review Focus 3).
//   - ReadTestRunFile(run, path) → one file's bytes + an advisory MIME by extension
//     (the delivery file endpoint streams it); a traversal path is ContractMisuse,
//     an absent entry NotFound.
//
// The profile split is the testRunStore seam below: localTestRunStore walks the
// directory TestRunRef.Artifact names; cloudTestRunStore lists the run's artifacts,
// finds TestRunRef.Artifact by name, downloads the zip ONCE into an on-disk cache
// keyed by run id + artifact name, and indexes/reads it with archive/zip. The
// generated GitArtifactAccess (contract.gen.go: blob store + auth resolver) has no
// field for the Actions client, so the LOCAL profile — which needs nothing beyond
// the directory — is served by the generated receiver itself (dispatching on the
// resolver's GitAuth.Local), and the CLOUD profile wraps it with the Actions-backed
// store (cloudArtifactAccess). The two variants live in this file, not in
// testrun_cloud.go / testrun_local.go, because the layer file-layout gate
// (TestFileLayout, rule file-not-allowed) permits exactly one handwritten impl
// file per ResourceAccess package.
// ---------------------------------------------------------------------------

// testRunStore is the profile seam behind the two test-run contract ops.
type testRunStore interface {
	index(ctx context.Context, run TestRunRef) (TestRunIndex, error)
	read(ctx context.Context, run TestRunRef, path string) (ConstructionOutput, error)
}

// GetTestRunIndex serves the LOCAL profile from the generated receiver: a file://
// store's test runs are directories. A non-local resolver means this receiver was
// built without the Actions-backed store the cloud profile needs
// (NewGitHubCloudArtifactAccess wraps it in one), so the call is refused rather
// than answered with an empty run a gate could mistake for evidence.
func (g *GitArtifactAccess) GetTestRunIndex(rc fwra.Context, run TestRunRef) (TestRunIndex, error) {
	store, err := g.localTestRuns(rc.Context)
	if err != nil {
		return TestRunIndex{}, err
	}
	return store.index(rc.Context, run)
}

// ReadTestRunFile reads one file of a LOCAL test-run directory; see GetTestRunIndex
// for the profile rule.
func (g *GitArtifactAccess) ReadTestRunFile(rc fwra.Context, run TestRunRef, path string) (ConstructionOutput, error) {
	store, err := g.localTestRuns(rc.Context)
	if err != nil {
		return ConstructionOutput{}, err
	}
	return store.read(rc.Context, run, path)
}

// localTestRuns resolves the profile: a GitAuth.Local resolver ⇒ the directory
// store; anything else is the cloud profile reached without its wrapper.
func (g *GitArtifactAccess) localTestRuns(ctx context.Context) (testRunStore, error) {
	auth, err := g.auth(ctx)
	if err != nil {
		return nil, err
	}
	if !auth.Local {
		return nil, fwra.New(fwra.ContractMisuse, "artifact: test-run ops on a non-local store need the GitHub Actions client; build the cloud profile with NewGitHubCloudArtifactAccess")
	}
	return localTestRunStore{}, nil
}

// cloudArtifactAccess is the CLOUD-profile artifactAccess: the generated git-backed
// store for construction outputs plus the Actions-backed test-run store.
type cloudArtifactAccess struct {
	ArtifactAccess
	runs testRunStore
}

var _ ArtifactAccess = (*cloudArtifactAccess)(nil)

func newCloudArtifactAccess(blob *githubinfra.GitBlobStore, auth func(ctx context.Context) (githubinfra.GitAuth, error), runs testRunStore) ArtifactAccess {
	return &cloudArtifactAccess{ArtifactAccess: NewGitArtifactAccess(blob, auth), runs: runs}
}

func (c *cloudArtifactAccess) GetTestRunIndex(rc fwra.Context, run TestRunRef) (TestRunIndex, error) {
	return c.runs.index(rc.Context, run)
}

func (c *cloudArtifactAccess) ReadTestRunFile(rc fwra.Context, run TestRunRef, path string) (ConstructionOutput, error) {
	return c.runs.read(rc.Context, run, path)
}

// resultsFile is the per-component scenario results the venue writes
// (`<component>/results.json`, shape scenariohost.ScenarioResult[]).
const resultsFile = "results.json"

// cleanTestRunPath validates a path inside a test-run artifact: relative,
// slash-separated, no empty / "." / ".." segments — so neither the directory
// variant nor a zip lookup can escape the artifact root.
func cleanTestRunPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fwra.New(fwra.ContractMisuse, "artifact: empty test-run file path")
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return "", fwra.New(fwra.ContractMisuse, "artifact: test-run file path must be relative and slash-separated: "+p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fwra.New(fwra.ContractMisuse, "artifact: test-run file path escapes the artifact: "+p)
		}
	}
	return p, nil
}

// pickResults chooses the results.json an index reports from the sorted file
// list: a root-level results.json first, else the shallowest (then first) one — a
// run is recorded for ONE component, so there is normally exactly one.
func pickResults(files []string) (string, bool) {
	best, depth := "", -1
	for _, f := range files {
		if f != resultsFile && !strings.HasSuffix(f, "/"+resultsFile) {
			continue
		}
		d := strings.Count(f, "/")
		if depth == -1 || d < depth {
			best, depth = f, d
		}
	}
	return best, depth != -1
}

// testRunMIME is the advisory MIME the file endpoint streams a test-run file
// under, by extension. Unknown extensions are opaque bytes.
func testRunMIME(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".webm":
		return "video/webm"
	case ".zip":
		return "application/zip"
	case ".json":
		return "application/json"
	case ".jsonl", ".txt", ".log":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

// localTestRunStore is the LOCAL variant: TestRunRef.Artifact is the results
// directory (`.aiarch/test-results/<run>`), absolute or relative to the server
// process's working directory — which `serve` pins to the state repo.
type localTestRunStore struct{}

func (localTestRunStore) root(run TestRunRef) (string, error) {
	if strings.TrimSpace(run.Artifact) == "" {
		return "", fwra.New(fwra.ContractMisuse, "artifact: local test run needs its results directory in TestRunRef.Artifact")
	}
	info, err := os.Stat(run.Artifact)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fwra.New(fwra.NotFound, "artifact: test-run directory not found: "+run.Artifact)
		}
		return "", fwra.Wrap(fwra.Infrastructure, err, "artifact: stat test-run directory")
	}
	if !info.IsDir() {
		return "", fwra.New(fwra.ContractMisuse, "artifact: test-run artifact is not a directory: "+run.Artifact)
	}
	return run.Artifact, nil
}

func (s localTestRunStore) index(_ context.Context, run TestRunRef) (TestRunIndex, error) {
	root, err := s.root(run)
	if err != nil {
		return TestRunIndex{}, err
	}
	var files []string
	walk := func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		return TestRunIndex{}, fwra.Wrap(fwra.Infrastructure, err, "artifact: walk test-run directory")
	}
	sort.Strings(files)
	idx := TestRunIndex{Files: files}
	if name, ok := pickResults(files); ok {
		b, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(name))) //nolint:gosec // name came out of the walk of root itself, never from a caller
		if rerr != nil {
			return TestRunIndex{}, fwra.Wrap(fwra.Infrastructure, rerr, "artifact: read "+name)
		}
		idx.Results = b
	}
	return idx, nil
}

func (s localTestRunStore) read(_ context.Context, run TestRunRef, p string) (ConstructionOutput, error) {
	rel, err := cleanTestRunPath(p)
	if err != nil {
		return ConstructionOutput{}, err
	}
	root, err := s.root(run)
	if err != nil {
		return ConstructionOutput{}, err
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // rel is cleanTestRunPath-validated (relative, no ".." segment) under the run's own root
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ConstructionOutput{}, fwra.New(fwra.NotFound, "artifact: test-run file not found: "+rel)
		}
		return ConstructionOutput{}, fwra.Wrap(fwra.Infrastructure, err, "artifact: read test-run file "+rel)
	}
	return ConstructionOutput{Bytes: b, MIMEType: testRunMIME(rel)}, nil
}

// actionsArtifactClient is the satellite surface the cloud store depends on
// (*githubinfra.AppClient satisfies it); an interface so the seam is explicit.
type actionsArtifactClient interface {
	ListArtifacts(ctx context.Context, owner, repo string, runID int64, instToken string) ([]githubinfra.Artifact, error)
	DownloadArtifact(ctx context.Context, owner, repo string, artifactID int64, instToken string) (io.ReadCloser, error)
}

// cloudTestRunStore is the CLOUD variant: TestRunRef.RunID is the GitHub Actions
// run id and TestRunRef.Artifact the artifact's upload name
// (`test-results-<activityId>-<revision>`). The zip is fetched once per
// (run, artifact) into cacheDir and served from there afterwards; a listing that
// says expired, or a zip route answering 404/410, is Expired, not an error.
type cloudTestRunStore struct {
	actions  actionsArtifactClient
	owner    string
	repo     string
	token    func(ctx context.Context) (string, error)
	cacheDir string

	// mu serialises the fetch-or-reuse of a cached zip so two concurrent readers
	// of the same run download it once.
	mu sync.Mutex
}

func newCloudTestRunStore(actions actionsArtifactClient, owner, repo string, token func(ctx context.Context) (string, error), cacheDir string) *cloudTestRunStore {
	return &cloudTestRunStore{actions: actions, owner: owner, repo: repo, token: token, cacheDir: cacheDir}
}

// defaultTestRunCacheDir is where the cloud store keeps downloaded zips:
// $XDG_CACHE_HOME-equivalent/archistrator/testruns (the per-user cache the OS
// reserves for re-fetchable data).
func defaultTestRunCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fwra.Wrap(fwra.Infrastructure, err, "artifact: no user cache dir for test-run zips")
	}
	return filepath.Join(base, "archistrator", "testruns"), nil
}

func parseRunID(run TestRunRef) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(run.RunID), 10, 64)
	if err != nil || id <= 0 {
		return 0, fwra.New(fwra.ContractMisuse, "artifact: cloud test run needs a numeric GitHub run id in TestRunRef.RunID, got "+strconv.Quote(run.RunID))
	}
	if strings.TrimSpace(run.Artifact) == "" {
		return 0, fwra.New(fwra.ContractMisuse, "artifact: cloud test run needs the artifact name in TestRunRef.Artifact")
	}
	return id, nil
}

// cachePath keys the cached zip by run id AND artifact name (a run may upload more
// than one artifact); the name is sanitised to a single path segment.
func (c *cloudTestRunStore) cachePath(runID int64, artifact string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, artifact)
	return filepath.Join(c.cacheDir, strconv.FormatInt(runID, 10), safe+".zip")
}

// ensureZip returns the on-disk zip for the run's artifact, downloading it once.
// expired=true (with no path) reports a retention-expired artifact.
func (c *cloudTestRunStore) ensureZip(ctx context.Context, run TestRunRef) (zipPath string, expired bool, err error) {
	runID, err := parseRunID(run)
	if err != nil {
		return "", false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	zipPath = c.cachePath(runID, run.Artifact)
	if _, serr := os.Stat(zipPath); serr == nil {
		return zipPath, false, nil
	}
	tok, err := c.token(ctx)
	if err != nil {
		return "", false, err
	}
	arts, err := c.actions.ListArtifacts(ctx, c.owner, c.repo, runID, tok)
	if err != nil {
		return "", false, err
	}
	var found *githubinfra.Artifact
	for i := range arts {
		if arts[i].Name == run.Artifact {
			found = &arts[i]
			break
		}
	}
	if found == nil {
		return "", false, fwra.New(fwra.NotFound, "artifact: run "+run.RunID+" has no artifact named "+run.Artifact)
	}
	if found.Expired {
		return "", true, nil
	}
	body, err := c.actions.DownloadArtifact(ctx, c.owner, c.repo, found.ID, tok)
	if err != nil {
		if isArtifactGone(err) {
			return "", true, nil
		}
		return "", false, err
	}
	defer func() { _ = body.Close() }()
	if err := writeFileAtomically(zipPath, body); err != nil {
		return "", false, err
	}
	return zipPath, false, nil
}

// isArtifactGone recognises the satellite's mapping of a vanished artifact zip:
// 404 → fwra.NotFound, and GitHub's 410 Gone (ClassifyStatus has no Gone kind, so
// it arrives as Infrastructure "status 410").
func isArtifactGone(err error) bool {
	var e *fwra.Error
	if errors.As(err, &e) && e.Kind == fwra.NotFound {
		return true
	}
	return strings.Contains(err.Error(), "status 410")
}

// writeFileAtomically streams r into a temp file beside dst and renames it into
// place, so a crashed download never leaves a half-written zip the cache would
// then trust.
func writeFileAtomically(dst string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "artifact: create test-run cache dir")
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.part")
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "artifact: create test-run cache file")
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fwra.Wrap(fwra.Transient, err, "artifact: download test-run zip")
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fwra.Wrap(fwra.Infrastructure, err, "artifact: close test-run cache file")
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return fwra.Wrap(fwra.Infrastructure, err, "artifact: commit test-run cache file")
	}
	return nil
}

func (c *cloudTestRunStore) index(ctx context.Context, run TestRunRef) (TestRunIndex, error) {
	zipPath, expired, err := c.ensureZip(ctx, run)
	if err != nil {
		return TestRunIndex{}, err
	}
	if expired {
		return TestRunIndex{Expired: true}, nil
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return TestRunIndex{}, fwra.Wrap(fwra.Infrastructure, err, "artifact: open test-run zip")
	}
	defer func() { _ = zr.Close() }()
	var files []string
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		files = append(files, f.Name)
	}
	sort.Strings(files)
	idx := TestRunIndex{Files: files}
	if name, ok := pickResults(files); ok {
		b, rerr := readZipEntry(&zr.Reader, name)
		if rerr != nil {
			return TestRunIndex{}, rerr
		}
		idx.Results = b
	}
	return idx, nil
}

func (c *cloudTestRunStore) read(ctx context.Context, run TestRunRef, p string) (ConstructionOutput, error) {
	rel, err := cleanTestRunPath(p)
	if err != nil {
		return ConstructionOutput{}, err
	}
	zipPath, expired, err := c.ensureZip(ctx, run)
	if err != nil {
		return ConstructionOutput{}, err
	}
	if expired {
		return ConstructionOutput{}, fwra.New(fwra.NotFound, "artifact: test-run artifact "+run.Artifact+" of run "+run.RunID+" has expired")
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return ConstructionOutput{}, fwra.Wrap(fwra.Infrastructure, err, "artifact: open test-run zip")
	}
	defer func() { _ = zr.Close() }()
	b, err := readZipEntry(&zr.Reader, rel)
	if err != nil {
		return ConstructionOutput{}, err
	}
	return ConstructionOutput{Bytes: b, MIMEType: testRunMIME(rel)}, nil
}

// readZipEntry reads one named entry; absent → NotFound.
func readZipEntry(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name != name || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fwra.Wrap(fwra.Infrastructure, err, "artifact: open test-run zip entry "+name)
		}
		defer func() { _ = rc.Close() }()
		b, err := io.ReadAll(rc)
		if err != nil {
			return nil, fwra.Wrap(fwra.Infrastructure, err, "artifact: read test-run zip entry "+name)
		}
		return b, nil
	}
	return nil, fwra.New(fwra.NotFound, "artifact: test-run file not found: "+name)
}

// localGitAuth is the LOCAL/embedded profile resolver: a file:// remote needs no
// HTTP credential.
func localGitAuth() func(ctx context.Context) (githubinfra.GitAuth, error) {
	return func(context.Context) (githubinfra.GitAuth, error) {
		return githubinfra.GitAuth{Local: true}, nil
	}
}

// tokenRefreshSkew re-mints the installation token a little before its hard expiry.
const tokenRefreshSkew = 60 * time.Second

// cloudGitAuth is the CLOUD profile resolver state. It mints/refreshes the
// installation token internally (App-JWT -> MintInstallationToken), caching it to
// expiry. Thread-safe.
type cloudGitAuth struct {
	app   *githubinfra.AppClient
	owner string

	mu             sync.Mutex
	installationID int64
	token          string
	tokenExpiry    time.Time
}

// resolve returns a non-local GitAuth carrying a valid installation token, minting
// or refreshing it internally. A cached token is reused until shortly before expiry.
func (c *cloudGitAuth) resolve(ctx context.Context) (githubinfra.GitAuth, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-tokenRefreshSkew)) {
		return githubinfra.GitAuth{Token: c.token}, nil
	}
	if c.installationID == 0 {
		id, err := c.app.FindInstallation(ctx, c.owner)
		if err != nil {
			return githubinfra.GitAuth{}, err
		}
		c.installationID = id
	}
	tok, exp, err := c.app.MintInstallationToken(ctx, c.installationID)
	if err != nil {
		return githubinfra.GitAuth{}, err
	}
	c.token = tok
	c.tokenExpiry = exp
	return githubinfra.GitAuth{Token: tok}, nil
}

// newCloudArtifactStore builds the cloud-profile satellite *GitBlobStore over the
// project's construction repo plus the internal token-minting auth resolver over
// the shared App client. It validates config eagerly (a missing field surfaces as
// fwra.ContractMisuse) but performs no network IO; the installation token is minted
// lazily on first use. installationID 0 ⇒ discovered on first call.
func newCloudArtifactStore(app *githubinfra.AppClient, repoURL, owner string, installationID int64) (*githubinfra.GitBlobStore, *cloudGitAuth, error) {
	if app == nil {
		return nil, nil, fwra.New(fwra.ContractMisuse, "artifact cloud: nil github app client")
	}
	if strings.TrimSpace(repoURL) == "" {
		return nil, nil, fwra.New(fwra.ContractMisuse, "artifact cloud: empty RepoURL")
	}
	if strings.TrimSpace(owner) == "" {
		return nil, nil, fwra.New(fwra.ContractMisuse, "artifact cloud: empty Owner")
	}
	blob, err := githubinfra.NewGitBlobStore(repoURL)
	if err != nil {
		return nil, nil, err
	}
	return blob, &cloudGitAuth{app: app, owner: owner, installationID: installationID}, nil
}

// ---------------------------------------------------------------------------
// DRY-RUN — artifact.ArtifactAccess stub. Store returns a deterministic fake content
// address; Retrieve returns a minimal valid output. Nothing is committed. Backs the
// UC3 construction Worker when ARCHISTRATOR_CONSTRUCTION_DRYRUN=true (local dogfood /
// demo profile only). Construction dispatches real work via the GH-Actions pipeline
// (agentic-everywhere); there is no server-side LLM worker seam.
// ---------------------------------------------------------------------------

// NewDryRunArtifactAccess returns the in-memory dry-run artifactAccess stub.
func NewDryRunArtifactAccess() ArtifactAccess {
	return dryRunArtifacts{}
}

type dryRunArtifacts struct{}

var _ ArtifactAccess = dryRunArtifacts{}

func (dryRunArtifacts) StoreConstructionOutput(rc fwra.Context, _ ConstructionOutput) (string, error) {
	return "dryrun-addr:" + string(rc.IdempotencyKey), nil
}

func (dryRunArtifacts) RetrieveConstructionOutput(_ fwra.Context, _ string) (ConstructionOutput, error) {
	return ConstructionOutput{Bytes: []byte("dry-run construction output"), MIMEType: "text/plain"}, nil
}

func (dryRunArtifacts) RetrieveOutputTree(_ fwra.Context, contentAddress string) (OutputTree, error) {
	return OutputTree{Root: contentAddress, Entries: map[string]string{}}, nil
}

func (dryRunArtifacts) GetTestRunIndex(_ fwra.Context, _ TestRunRef) (TestRunIndex, error) {
	return TestRunIndex{Results: []byte("[]"), Files: []string{}}, nil
}

func (dryRunArtifacts) ReadTestRunFile(_ fwra.Context, _ TestRunRef, _ string) (ConstructionOutput, error) {
	return ConstructionOutput{Bytes: []byte("dry-run test-run file"), MIMEType: "text/plain"}, nil
}
