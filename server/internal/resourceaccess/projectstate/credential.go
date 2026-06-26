package projectstate

import "time"

// RepoCredential is the provider-neutral, SHORT-LIVED bearer credential the
// Manager threads into every provider-touching projectStateAccess verb
// (projectStateAccess.md §REWORK.4). It is the credential the merged
// sourceControlAccess.GetInstallationToken MINTS (founder merge 2026-06-25 folded
// the former ISourceControlLifecycle + IPullRequestRail into one
// SourceControlAccess port) and the Manager carries down as a caller-supplied
// parameter — projectStateAccess NEVER calls sourceControlAccess itself
// (RA-never-calls-RA / NoSideways).
//
// SHAPE-MATCHED, NOT IMPORTED. The contract says RepoCredential is "the same
// opaque value type sourceControlAccess.md §3.2 defines — referenced, not
// redefined." The Method's layer rule (NoSideways: a ResourceAccess imports no
// sibling ResourceAccess, arch-checker-enforced) forbids projectstate importing
// the sourcecontrol package, so the layer-legal realization of "referenced, not
// redefined" is a LOCAL value type of the IDENTICAL shape ({Bytes, ExpiresAt}).
// The two are structurally identical opaque carriers; the Manager constructs this
// one from the credential getInstallationToken returned. (See C-PA-R log §"contract
// gaps flagged" — the architect may prefer promoting RepoCredential to framework-go
// so both RAs reference one definition; until then the shape-match is the
// layer-legal equivalent.)
//
// PROVIDER-NEUTRAL: carries NO ghs_ prefix, NO installation id, NO App JWT. Bytes
// is write-only at this consumer — presented to the git transport, never logged,
// persisted, parsed, or compared.
type RepoCredential struct {
	// Bytes is the opaque bearer secret (the installation token's bytes). Presented
	// to the git remote; never inspected here.
	Bytes []byte
	// ExpiresAt is when the Manager re-mints (calls getInstallationToken again).
	// Carried for parity with the source type; this RA does not act on it (the
	// Manager owns re-mint timing).
	ExpiresAt time.Time
}

// IsZero reports whether the credential is empty. A zero credential is only valid
// for the LOCAL on-disk-git profile (a trivially-valid local credential,
// projectStateAccess.md §REWORK.4 LOCAL note); against a remote GitHub it is a
// caller pre-condition violation surfaced as fwra.ContractMisuse.
func (c RepoCredential) IsZero() bool { return len(c.Bytes) == 0 }

// LocalRepoCredential is the trivially-valid credential the LOCAL deployment
// profile (on-disk git) threads — the same parameter, a no-op value. It signals
// the git-data layer to attach no HTTP auth (a file:// remote needs none).
func LocalRepoCredential() RepoCredential { return RepoCredential{Bytes: []byte("local")} }
