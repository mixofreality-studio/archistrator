package artifact

import (
	"bytes"
	"context"
	"strings"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
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
//     MintInstallationToken) inside this package (see gitstore_client.go). The RA
//     NEVER threads a credential through its surface and NEVER calls a sibling RA
//     (NoSideways) — the layer-legal auth resolution C-CP-R's senior review
//     ratified and the contract §6/Non-goal #11 prose ("token acquired/refreshed
//     inside the package") prescribes.
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
// The struct imports NO Temporal (layer rule): the idempotencyKey arrives as an
// ordinary parameter and is never read from ambient context. Git vocabulary lives
// only in the github satellite — this RA names no SHA/blob/tree/owner/repo on its
// surface or returned types.
type Store struct {
	// blob is the satellite's provider-neutral content-addressable git handle.
	blob gitBlobClient
	// auth resolves the provider-neutral GitAuth for each call. For the cloud
	// profile it mints/refreshes the installation token internally; for the local
	// profile it returns GitAuth{Local: true}. Auth is NEVER on the contract surface.
	auth gitAuthSource
}

// gitBlobClient is the satellite surface this RA depends on — declared as an
// interface so the impl is unit-testable and the dependency is explicit. The
// satellite *GitBlobStore satisfies it.
type gitBlobClient interface {
	StoreOutput(ctx context.Context, branch string, files []fwgithub.GitObjectFile, message string, auth fwgithub.GitAuth) (string, error)
	ReadFileAtCommit(ctx context.Context, commitHash, path string, auth fwgithub.GitAuth) ([]byte, error)
	ProbeFileAtBranchTip(ctx context.Context, branch, path string, auth fwgithub.GitAuth) ([]byte, string, bool, error)
	WalkTreeFiles(ctx context.Context, commitHash string, auth fwgithub.GitAuth) ([]string, error)
}

// gitAuthSource yields the provider-neutral GitAuth for a call. It is the package-
// internal auth seam: cloud => an internally-minted installation token; local =>
// GitAuth{Local: true}. It never appears on the contract surface.
type gitAuthSource interface {
	gitAuth(ctx context.Context) (fwgithub.GitAuth, error)
}

// Compile-time proof the concrete Store satisfies the port. If the port ever
// drifts, this line breaks the build — exactly the guard The Method wants between a
// contract and its construction.
var _ ArtifactAccess = (*Store)(nil)

// newStore wires a Store over a satellite blob client + an auth source. It is the
// shared inner constructor; the exported NewCloudStore / NewLocalStore wrap it with
// the profile-specific auth source (gitstore_client.go).
func newStore(blob gitBlobClient, auth gitAuthSource) *Store {
	return &Store{blob: blob, auth: auth}
}

// StoreConstructionOutput commits the construction output and returns its
// content-address string. Storing byte-identical content (same bytes AND same
// MIMEType) returns the EXISTING address without producing a new commit
// (artifactAccess.md §2.1 content-addressable idempotency). Storing different
// content yields a NEW address; the prior output is retained (immutable history).
func (s *Store) StoreConstructionOutput(ctx context.Context, content ConstructionOutput, idempotencyKey fwra.IdempotencyKey) (string, error) {
	if len(content.Bytes) == 0 {
		return "", fwra.New(fwra.ContractMisuse, "artifact.StoreConstructionOutput: empty content bytes")
	}
	if strings.TrimSpace(string(idempotencyKey)) == "" {
		return "", fwra.New(fwra.ContractMisuse, "artifact.StoreConstructionOutput: empty idempotencyKey")
	}

	auth, err := s.auth.gitAuth(ctx)
	if err != nil {
		return "", err
	}

	contentPath := contentPathFor(content.MIMEType)
	branch := branchFor(content)

	// Dedup probe: a content-derived branch already holding byte-identical
	// content+MIMEType collapses to the EXISTING address with no new commit
	// (artifactAccess.md §2.1). This is what gives "same content -> same address".
	if existing, tipToken, found, derr := s.blob.ProbeFileAtBranchTip(ctx, branch, contentPath, auth); derr != nil {
		return "", derr
	} else if found && bytes.Equal(existing, content.Bytes) {
		// Confirm the sidecar MIMEType matches too (same bytes + same MIMEType is the
		// content-addressable identity; the branch is keyed on both, so a hit here is
		// near-certain, but verify the meta to be exact).
		if metaBytes, _, mfound, merr := s.blob.ProbeFileAtBranchTip(ctx, branch, metaFile, auth); merr == nil && mfound && decodeMeta(metaBytes) == content.MIMEType {
			return makeAddress(tipToken, contentPath), nil
		}
	}

	files := []fwgithub.GitObjectFile{
		{Path: contentPath, Bytes: content.Bytes},
		{Path: metaFile, Bytes: encodeMeta(content.MIMEType)},
	}
	commitToken, err := s.blob.StoreOutput(ctx, branch, files, commitMessage(idempotencyKey), auth)
	if err != nil {
		return "", err
	}
	return makeAddress(commitToken, contentPath), nil
}

// RetrieveConstructionOutput resolves a content address back to its
// ConstructionOutput. An unknown / unresolvable address surfaces as fwra.NotFound
// (artifactAccess.md §2.2).
func (s *Store) RetrieveConstructionOutput(ctx context.Context, contentAddress string) (ConstructionOutput, error) {
	commitToken, contentPath, err := parseAddress(contentAddress)
	if err != nil {
		return ConstructionOutput{}, err
	}
	auth, err := s.auth.gitAuth(ctx)
	if err != nil {
		return ConstructionOutput{}, err
	}

	contentBytes, err := s.blob.ReadFileAtCommit(ctx, commitToken, contentPath, auth)
	if err != nil {
		return ConstructionOutput{}, err
	}
	mime := ""
	if metaBytes, mErr := s.blob.ReadFileAtCommit(ctx, commitToken, metaFile, auth); mErr == nil {
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
func (s *Store) RetrieveOutputTree(ctx context.Context, contentAddress string) (OutputTree, error) {
	commitToken, _, err := parseAddress(contentAddress)
	if err != nil {
		return OutputTree{}, err
	}
	auth, err := s.auth.gitAuth(ctx)
	if err != nil {
		return OutputTree{}, err
	}

	paths, err := s.blob.WalkTreeFiles(ctx, commitToken, auth)
	if err != nil {
		return OutputTree{}, err
	}

	entries := map[OutputPath]string{}
	for _, name := range paths {
		// Each file entry is addressed by the SAME commit token + its path, so it
		// round-trips through RetrieveConstructionOutput.
		entries[OutputPath(name)] = makeAddress(commitToken, name)
	}
	return OutputTree{Root: contentAddress, Entries: entries}, nil
}
