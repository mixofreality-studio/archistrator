package sourcecontrol

// behavior.go carries the FREE-FUNCTION behaviour of the named-scalar / enum /
// struct value types in this component's contract — the established "behavioral
// value type → generated scalar + free functions" pattern (same as
// durableexecution's ExecutionHandle/ExecutionStatus and constructionpipeline's
// PipelineHandle/PipelinePhase/RepoTarget). The opaque-handle value types
// (Installation, RepoRef, CommitRef, BranchRef, PullRequestRef) are generated as
// $def named scalars (contract.gen.go); the RepoCredential struct + the CheckState
// enum are generated as a struct / enum. Their methods would not survive codegen,
// so they live here as free functions the impl + callers call. The opaque token
// the impl packs IS the string value, so the handle behaviour is a thin, parse-free
// pass over that value.

import (
	"strings"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ---------------------------------------------------------------------------
// Installation behaviour (free functions over the generated named scalar).
// ---------------------------------------------------------------------------

// InstallationString returns the canonical printable form (logs, audit). Replaces
// the former Installation.String() method.
func InstallationString(i Installation) string { return string(i) }

// InstallationIsZero reports whether the handle addresses no installation. Replaces
// the former Installation.IsZero() method.
func InstallationIsZero(i Installation) bool { return i == "" }

// ---------------------------------------------------------------------------
// RepoRef behaviour (free functions over the generated named scalar).
// ---------------------------------------------------------------------------

// RepoRefString returns the canonical printable form. Replaces the former
// RepoRef.String() method.
func RepoRefString(r RepoRef) string { return string(r) }

// RepoRefEqual reports value equality of two repo refs. Replaces the former
// RepoRef.Equal() method.
func RepoRefEqual(a, b RepoRef) bool { return a == b }

// RepoRefIsZero reports whether the ref addresses no repo. Replaces the former
// RepoRef.IsZero() method.
func RepoRefIsZero(r RepoRef) bool { return r == "" }

// RepoRefFromString reconstructs a RepoRef from the exact RepoRefString form a
// prior AdoptProjectRepo returned (a Manager re-materialising a persisted handle).
// Pure value reconstruction; a malformed ref is rejected by the verb that consumes
// it. (Replaces the former RepoRefFromString constructor — same name, now a thin
// cast over the named scalar.)
func RepoRefFromString(s string) RepoRef { return RepoRef(s) }

// RepoRefOwnerRepo decodes the RepoRef into its provider owner + repo coordinates —
// the ONLY public accessor of the otherwise-opaque owner/repo encoding. It exists
// so a caller that must address the repo on a DIFFERENT infrastructure port than
// this RA (the per-project-design-dispatch: the constructionPipelineAccess seam
// dispatches the agentic DESIGN job to the per-project repo) can resolve the
// owner/repo WITHOUT re-implementing this RA's private RepoRef encoding. A malformed
// ref is a ContractMisuse the caller surfaces. This is the single seam where
// owner/repo leaves the RA, deliberately scoped to the cross-port dispatch target.
// (Replaces the former RepoRef.OwnerRepo() method.)
func RepoRefOwnerRepo(r RepoRef) (owner, repo string, err error) {
	_, fullName, serr := splitRepoRef(r)
	if serr != nil {
		return "", "", serr
	}
	o, n, ok := strings.Cut(fullName, "/")
	if !ok || o == "" || n == "" {
		return "", "", fwra.New(fwra.ContractMisuse, "sourcecontrol: RepoRef full name is not owner/repo")
	}
	return o, n, nil
}

// ---------------------------------------------------------------------------
// RepoCredential behaviour (free function over the generated struct).
// ---------------------------------------------------------------------------

// (RepoCredentialIsZero lives in sourcecontrol.go next to the type.)

// ---------------------------------------------------------------------------
// CommitRef behaviour (free functions over the generated named scalar).
// ---------------------------------------------------------------------------

// CommitRefString returns the canonical printable form. Replaces the former
// CommitRef.String() method.
func CommitRefString(c CommitRef) string { return string(c) }

// CommitRefIsZero reports whether the ref addresses no commit. Replaces the former
// CommitRef.IsZero() method.
func CommitRefIsZero(c CommitRef) bool { return c == "" }

// ---------------------------------------------------------------------------
// BranchRef behaviour (free functions over the generated named scalar).
// ---------------------------------------------------------------------------

// BranchRefString returns the canonical printable form. Replaces the former
// BranchRef.String() method.
func BranchRefString(b BranchRef) string { return string(b) }

// BranchRefIsZero reports whether the ref addresses no branch. Replaces the former
// BranchRef.IsZero() method.
func BranchRefIsZero(b BranchRef) bool { return b == "" }

// ---------------------------------------------------------------------------
// PullRequestRef behaviour (free functions over the generated named scalar).
// ---------------------------------------------------------------------------

// PullRequestRefString returns the canonical printable form. Replaces the former
// PullRequestRef.String() method.
func PullRequestRefString(p PullRequestRef) string { return string(p) }

// PullRequestRefEqual reports value equality of two PR refs. Replaces the former
// PullRequestRef.Equal() method.
func PullRequestRefEqual(a, b PullRequestRef) bool { return a == b }

// PullRequestRefIsZero reports whether the ref addresses no PR. Replaces the former
// PullRequestRef.IsZero() method.
func PullRequestRefIsZero(p PullRequestRef) bool { return p == "" }

// PullRequestRefFromString reconstructs a PullRequestRef from a persisted
// PullRequestRefString form (a Manager re-materialising a handle across an Activity
// boundary). (Replaces the former constructor — same name, now a thin cast.)
func PullRequestRefFromString(s string) PullRequestRef { return PullRequestRef(s) }

// ---------------------------------------------------------------------------
// CheckState behaviour (free function over the generated enum).
// ---------------------------------------------------------------------------

var checkStateNames = map[CheckState]string{
	CheckPending: "Pending", CheckSuccess: "Success", CheckFailure: "Failure",
}

// CheckStateString returns the stable name (logs, audit). Replaces the former
// CheckState.String() method (the generated contract type carries no methods).
func CheckStateString(s CheckState) string {
	if n, ok := checkStateNames[s]; ok {
		return n
	}
	return "Pending"
}
