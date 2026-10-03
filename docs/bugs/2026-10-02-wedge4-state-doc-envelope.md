# Wedge #4 — the state document's envelope is not mergeable text

Found by the FIRST paid todomvc benchmark run
(`todomvc-run-20261003T025700Z-4fa797ea`, $1.63, 7 real agent episodes). The
`requirements` activity drafted, reviewed and staged all four Phase-1 artifacts,
then burned all ten merge attempts in 1.4 s and gave up with
`VarianceExhausted / "the local merge exceeded max attempts"`.

## Root cause (fixed)

`.aiarch/state/project.json` stores the projectstate store's own bookkeeping
INLINE in the document git merges: `version` (the optimistic-concurrency token)
and `updatedAt` (the write timestamp). Within ONE activity the delivery rail
writes its row/review ledger to `main` while the agent stages artifacts on
`activity/<id>`, so BOTH refs move those two lines off the merge base and a
3-way TEXT merge has nothing to pick:

```
CONFLICT (content): Merge conflict in .aiarch/state/project.json
```

Deterministic, for every activity whose walk dispatches work. **Not** contention
and **not** a retry-bound shortfall — the run recorded ZERO
`object not found`, ZERO `non-fast-forward` and ZERO `Conflict` events, and the
ten attempts were 123 ms apart.

Fixed in `agenticjob.resolveStateDocEnvelopeConflict`: when the ONLY unmerged
path is the state document and the ONLY difference git could not resolve is those
two members, the merge is re-run with the envelope neutralised, main's envelope is
restored, and the merge commits. Pinned by
`TestLocalExecMergeJob_PaidRunStateDocs_Merge` over the run's VERBATIM documents
(`testdata/run1statedoc/`).

## Open earmarks

1. **`sourcecontrol.gitLocalAccess.mergeHeadIntoMain` + `trialMerge` carry the
   identical defect.** Off the paid path (the local profile finishes an activity
   through agenticjob's merge job, not a local "PR"), and the resolver cannot be
   shared — an RA may not import another RA (NoSideways) and promoting it to a
   utility is a Method architecture change. Fix both together with a shared home;
   do not copy 200 lines. Comment left at the site.

2. **The merge carries the branch's `applied_mutations/` dedup records onto main,
   and `lookupAppliedInSnapshot` is not ref-scoped.** A branch-staging
   idempotency key re-sent as a MAIN-scoped write would then dedup-hit and return
   the branch's `resultVersion` (12 in this run) instead of writing. Inherent to
   "merge the branch into main", pre-dating this fix; not reachable with today's
   RunID-derived keys, but it is a fabricated-success shape.

3. **`encodeProjectDoc`'s doc comment lies.** It says passing the zero time
   "preserve[s] whatever updatedAt is already present in the doc"; the code
   stamps the zero value. That is why the paid run's branch document carried
   `"updatedAt": "0001-01-01T00:00:00Z"`.

4. **The pump died at 19:57:12, one second into the run**, with
   `child workflow execution already started` — the known pump double-start
   (memory: "Pump double-start", branch `pump-singular-per-project`). That is why
   `delivery.lease.unreachable` fired: the lease signal's target workflow had
   already completed. The lease fails open BY DESIGN
   (`requestMainWriteLease`'s own comment) and is **not** causal here — there was
   exactly one activity running, so there was nothing to serialise, and the merge
   conflicts identically with zero concurrency. The pump defect is real and still
   open; it is a separate wedge.

5. **`delivery.construction.jobFailed` published only the ordinal
   `failureReason`** while `dispatchJobFailedDetail`'s string — which already
   contained the whole answer — was computed and dropped. Now logged as `detail`.
