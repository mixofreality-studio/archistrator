# Agent task spec wave: earmarks

These items were deferred during the agent-task-spec wave (plan `docs/superpowers/plans/2026-10-11-agent-task-spec.md`).
Each one names what triggers it and what closes it.

## E1: the local rail still runs a dispatch with no envelope ungated (ruling R41)

**What.** `agenticjob.taskSpecEnvelopeOf` lets one kind of dispatch through ungated: a lifecycle dispatch that carries neither `task_id` nor `spec_hash`. Such a run gets no spec, no `--model`/`--tools`, no edit scope and no checks.

**How it shows.** Each such dispatch is logged at WARN: `localexec: a lifecycle dispatch carries no task spec envelope; it runs UNGATED`.

**Why it is still there.** The 23 non-answer `SubmitAgenticJob` bindings in the agenticJobAccess component test plan (`.aiarch/state/project.json`) predate the envelope, and the generated agenticJobAccess scenarios dispatch them as written. Both Managers always send the envelope, and two tests pin this:
- `TestEveryDesignDispatchCarriesItsTaskSpecEnvelope`;
- `TestTestingGatePassCitesTheJudgedRun`, through `assertTaskSpecEnvelopes`.

So production dispatches are always gated.

**To close.** The data edit needs the founder's permission; it was refused in B6.
1. Add the envelope to those 23 bindings:
   - `task_id`, `spec_hash` and `revision` on every binding;
   - `activity_id` as well on the design ones.
2. Run `make gen-tests`.
3. Delete the ungated branch in `taskSpecEnvelopeOf`, so that a dispatch with no envelope is refused like a malformed one.

## E2: gen-tests runs at the repo root (ruling R42)

The templates run `make gen-tests` at the checkout root, and the local rail now does the same. A project whose Go module is not at the root has no root Makefile. archistrator is one: its `moduleDir` is `server`. For such a project, the integration task fails in both venues.

**To check:** at D1. **To fix:** in the platform template and the local rail together.

## E3: the check gate is not sandboxed (rulings R27, R39)

The gate's subprocesses run code the agent wrote, outside the agent's sandbox. They run with an allowlisted env, and their git calls are isolated. Repo-local `.git/config` filter drivers remain the one open residual.

**To close:** R27's separate gate job.

## E4: gate time stacks onto the Manager's observe ceiling (M6, parked)

The time before the agent (task-spec and gen-tests, 15 min each) and after it (edit-scope 15, checks 45, record 15) adds to the agent's 25 minutes. A long check gate can pass the Manager's 1h observe ceiling. The cloud job has the same shape.
