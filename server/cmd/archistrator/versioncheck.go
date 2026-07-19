package main

// versioncheck.go implements Task-5 review finding 3: a version-skew guard
// between this `archistrator` binary and the `archistrator-server` binary it
// spawns (serverchild.go). cmd/server has no hand-written flag hook (its
// main.go is config.gen.go-driven env vars only — no flag.FlagSet anywhere;
// confirmed by inspection), so adding a `--build-info` flag there would mean
// hand-forking the composition root this package's whole design (see
// serverchild.go's package doc) exists to avoid. The cheapest HONEST channel
// available without that is comparing both binaries' VCS revisions: this
// process reads its OWN via runtime/debug.ReadBuildInfo, and the CHILD's by
// parsing the compiled archistrator-server binary FILE directly via
// debug/buildinfo.ReadFile — no execution required, so this can run before
// the child is even spawned.
//
// On a detected mismatch this is a WARNING, not a refusal (per the brief):
// the finding is folded into the same Instructions channel degrade() uses to
// carry Step 3-5 failures, so the driver sees it on the very first MCP
// `initialize` response, but the local stack still starts and tools still
// mount.
import (
	"debug/buildinfo"
	"fmt"
	"runtime/debug"
)

// buildIdentity is the minimal build identity used to detect version skew.
// Both fields are empty when unavailable — e.g. a `go run` build, a binary
// built with `-buildvcs=false`, or (for the child) a corrupt/non-Go file —
// and empty is treated as "cannot verify", never as a mismatch.
type buildIdentity struct {
	Version  string
	Revision string
}

// ownBuildIdentity reads THIS running archistrator binary's build identity.
func ownBuildIdentity() buildIdentity {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return buildIdentity{}
	}
	return buildIdentity{Version: info.Main.Version, Revision: vcsRevision(info.Settings)}
}

// childBuildIdentity reads the build identity embedded in the compiled
// binary at path, without executing it.
func childBuildIdentity(path string) buildIdentity {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return buildIdentity{}
	}
	return buildIdentity{Version: info.Main.Version, Revision: vcsRevision(info.Settings)}
}

func vcsRevision(settings []debug.BuildSetting) string {
	for _, s := range settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

// versionSkewWarning compares own against child (the archistrator-server
// binary found at childPath) and returns a non-empty, actionable warning
// line when both report a VCS revision AND those revisions differ. Either
// side missing a revision returns "" — a silent no-op, not a false alarm:
// the guard must never claim skew it cannot actually verify.
func versionSkewWarning(own, child buildIdentity, childPath string) string {
	if own.Revision == "" || child.Revision == "" || own.Revision == child.Revision {
		return ""
	}
	return fmt.Sprintf(
		"archistrator: VERSION SKEW WARNING — this archistrator binary (version %s, rev %s) and the "+
			"archistrator-server binary it spawned at %s (version %s, rev %s) were built from DIFFERENT "+
			"commits. Tool results may not match this archistrator's understanding of the wire contract. "+
			"Rebuild both from the same commit (scripts/build-local.sh) if anything looks inconsistent.",
		displayVersion(own.Version), shortRev(own.Revision), childPath, displayVersion(child.Version), shortRev(child.Revision))
}

// displayVersion renders an empty/unknown module version ("(devel)" or "")
// as "unknown" rather than a confusing blank in the warning text.
func displayVersion(v string) string {
	if v == "" || v == "(devel)" {
		return "unknown"
	}
	return v
}

// shortRev renders a short (12-char) prefix of a VCS revision, matching the
// convention `git rev-parse --short` uses, so the warning stays scannable.
func shortRev(rev string) string {
	const n = 12
	if len(rev) <= n {
		return rev
	}
	return rev[:n]
}
