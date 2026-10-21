package internal_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/pushgate"
)

// TestLocalPushesHoldTheRepoLock is the FIX-002 arch gate: every git push the
// ResourceAccess layer performs — go-git Push/PushContext, `git push` through
// os/exec, or a push through a git runner — must run inside a
// github.PushLocal callback, so no two in-process writers can race into the
// LOCAL state repo. That repo is non-bare with main checked out and
// receive.denyCurrentBranch=updateInstead (`archistrator init`); a racing loser
// writes its tree into the target's index before losing the ref CAS and wedges
// every later push (docs/bugs/2026-10-18-fix-002-interim-push-lock.md).
//
// The check is by TYPE (pushgate), not text: a push in an exported function,
// a function used as a value, or one reached through an interface fails unless
// it sits lexically inside the PushLocal callback.
//
// The ONE exemption is operatedruntime's gitOpsCommit, which pushes to the
// operated system's GitHub origin — a remote host with no checked-out branch to
// wedge. pushgate fails a stale exemption, so this entry cannot outlive the push.
//
// INTERIM with the lock it polices: when the r2 topology ruling lands (local
// state no longer on a checked-out branch), this gate goes with PushLocal's lock.
func TestLocalPushesHoldTheRepoLock(t *testing.T) {
	pushgate.Check(t, pushgate.Spec{
		ModuleRoot: "..",
		Patterns:   []string{"./internal/resourceaccess/..."},
		Wrappers:   []pushgate.Func{pushgate.PushLocalFunc},
		Exempt: []pushgate.Func{
			"(github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime.realOperatedRuntime).gitOpsCommit",
		},
	})
}
