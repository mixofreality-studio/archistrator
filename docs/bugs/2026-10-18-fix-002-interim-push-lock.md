# FIX-002: the INTERIM per-repo push lock, its removal condition, and wedge recovery

Status: **INTERIM.** Shipped under founder ruling option (A), 2026-10-18: the classifier
fix plus a per-repo push lock. The r2 bare-repo / unchecked-out-ref topology was **out
of scope** and is still open.

## What wedged

`archistrator init` makes the user's working repo the local state store. The repo is
non-bare, `main` is checked out, and `receive.denyCurrentBranch=updateInstead`
(server/cmd/archistrator/init.go). With updateInstead, git runs push-to-checkout
(`read-tree -u -m HEAD <new>`) before the ref transaction checks the old value. Suppose
two writers push from the same base. The loser has already written its tree into the
target's index and worktree when it loses the ref CAS. The repo is then left with staged
changes, and git refuses every later push to `main` with
`! [remote rejected] main -> main (Working directory has staged changes)`. No re-read can
clear that refusal.

Run run-20261017T211949Z-30a6612c hit this when two OpenActivity births raced. It logged
1159 `concurrent ref advance` errors before the operator stopped it. Each of those errors
was really this refusal, misreported as a CAS loss.

## What shipped

1. **Classifier (root cause, permanent).** This is platform `framework-go-infrastructure-github`,
   `pushclassify.go`. A checkout refusal is now `fwra.Infrastructure` with
   `Retryable=false`, and its message names the dirty checkout. It is no longer reported
   as `fwra.Conflict` or `ErrRefCASLost`. One classifier serves both
   `GitStore.CommitSubtree` and `GitBlobStore.StoreOutput`.
2. **Per-repo push lock (INTERIM, compensation for the topology).** This is
   `github.PushLocal` and `LockLocalRepo` in `localrepolock.go`. It is a process-wide lock
   keyed by the repo's canonical path, so the `file://` form, the plain form and a symlink
   all share one lock. Every in-process receive-pack into a local repo runs inside the lock:
   - platform `GitStore.CommitSubtree` and `GitBlobStore.StoreOutput` (local remotes only);
   - app agenticjob `localExecAccess.mergeActivityBranch`. Each clone → merge → push →
     branch-delete pass runs under the lock, and the caller's ctx is threaded through so a
     parked waiter can be cancelled. `deleteMergedBranch` is reached only inside that
     pass. The instance's private `gitMu` no longer guards any push. It still serialises
     the instance's own `git worktree add/remove`, which writes no pushed ref.
   - app sourcecontrol `gitLocalAccess.CommitManagedFiles`, `OpenBranch` (which calls
     `gitLocalPush`), and `MergePullRequest` (which calls `mergeHeadIntoMain` →
     `gitLocalDeleteBranch` → `gitLocalPush`). The private `gitMu` was deleted. It only
     ever serialised the instance against itself, and the reads it also held run in
     throwaway clones that need no lock.
3. **Gate.** `server/internal/arch_pushgate_test.go` (`TestLocalPushesHoldTheRepoLock`)
   runs the platform's type-based `pushgate` over `./internal/resourceaccess/...`. A go-git
   `Push`/`PushContext`, an `exec` `git push`, or a push through a git runner must sit
   inside a `PushLocal` callback. The only exemption is operatedruntime's `gitOpsCommit`,
   which pushes to a GitHub origin, and the gate fails if that exemption goes stale. I
   mutation-tested it: unwrapping `MergePullRequest`'s `PushLocal` turns it red with two
   violations, `mergeHeadIntoMain` and `gitLocalPush`.
4. **Regression.** agenticjob `TestLocalStateRepoSurvivesConcurrentBirthsAndAMerge` runs on
   the init topology (`testinfra.StartLocalCheckedOutRepo`). Each of 40 rounds races 4
   OpenActivity births against one local merge job, and the test asserts that every write
   lands and the checkout is clean after every round. Before the app half it went red 3
   out of 4 times, in round 12, 13, 13 and 15 (`round N left the checked-out main dirty`).

## Scope limit (why it is interim)

The lock only serialises writers **inside the one server process**. Serve enforces one
server per project. The following writers can still dirty the checkout and get a write
refused:

- an agent's own `git push origin main`;
- the user's own push;
- a dirty user worktree on `main`.

The classifier now reports that refusal once, non-retryably, with its real cause. Nothing
heals it automatically.

## REMOVAL CONDITION

**When the r2 topology ruling lands, delete the lock and keep the classifier.** That
ruling puts local state somewhere no one checks out, either a bare repo or refs no
worktree has checked out (docs/bugs/2026-10-04-dct-plan5-rulings.md r2 rows 109–111,
suspended at r3 row 54, deferred 2026-10-10). Against such a target, concurrent pushes are
an ordinary CAS. Platform `TestGitStore_RefCASLoserConflict` shows this on a bare repo.

1. Make `PushLocal` a pass-through, or inline it, and delete `LockLocalRepo` and
   `localrepolock.go`'s semaphore map.
2. Delete `server/internal/arch_pushgate_test.go` and the `pushgate` package. They police
   the lock and nothing else.
3. Keep `pushclassify.go` and its tests. They are the root-cause half.
4. Re-point `TestLocalStateRepoSurvivesConcurrentBirthsAndAMerge` at the new topology. Keep
   it as the proof that concurrent writers converge.

The founder ruling on that topology goes to the founder with this fix. It is not waiting
for the next run, because it has already been deferred once.

## Recovering an already-wedged repo (one time, by hand)

The code never heals a wedged repo. Healing it automatically would hide the race, race the
next writer, and touch the user's worktree. A repo wedged before this fix stays wedged
until someone does the following:

1. Stop the server.
2. Look at what is staged. It is a losing writer's tree, so it is not the user's work:

   ```sh
   git -C <repo> status
   git -C <repo> diff --cached --stat
   ```

3. If any of **your own** uncommitted edits to tracked files are in the worktree, copy them
   aside first. The next step discards every staged and unstaged change to tracked files.
4. Reset the index and worktree to `main`'s committed tip:

   ```sh
   git -C <repo> reset --mixed
   git -C <repo> checkout -- .
   ```

5. Confirm that `git -C <repo> status --porcelain --untracked-files=no` prints nothing, then
   restart the server.

Do not force-push and do not raise `maxMutateConflictAttempts`. The refusal is permanent,
so a larger retry budget would only take longer to burn out.

## Still owed

- **infra-github release** (v0.3.0, a new minor after v0.2.0; go.mod also gains
  `golang.org/x/tools`). After it is released, bump the app pin in `server/go.mod` and run
  `GOWORK=off`. Until then the app half builds and tests only in workspace mode, because
  `PushLocal`, `pushgate` and `StartLocalCheckedOutRepo` are unreleased.
- **Lock-wait tracing.** Lock wait time is not traced yet, because infra-github has no
  tracing dependency.
- **$0 bench smoke** on the init topology with 2 or more concurrent births. The pass
  condition is a clean state repo and no `concurrent ref advance` lines in serve.log.
