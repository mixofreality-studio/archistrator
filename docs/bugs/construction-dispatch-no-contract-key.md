# Bug: construction never dispatches — "no service-contract key resolves for activity"

**Status:** fixed on branch `construction-dispatch-componentid` · **Severity:** blocked all real construction · **Found:** 2026-08-07 (AC iteration 0, first real end-to-end benchmark build of todomvc)

Fixed by the design in
`docs/superpowers/specs/2026-08-08-construction-dispatch-componentid-design.md`: dispatch now
resolves an activity's component through an authored `ComponentID` field instead of the
contract-only `resolveComponentID` resolver described below (deleted), and a milestone-dependency
defect found by the same drain (§4.3a of that spec) is fixed alongside it. Evidence: a dry-run
drain of the todomvc corpus reached 26/26 activities `Done` under
`ARCHISTRATOR_CONSTRUCTION_DRYRUN=true`, with zero activities stalled quiescent.

## Symptom

A real build sails through all of Phase 1 (system design) and all of Phase 2
(project design) and reaches Phase 3 construction with a fully computed network
(e.g. 33 activities, 7 eligible, 21 blocked, 0 integrated). Then **construction
dispatches nothing** and the pump spins idle until the drive's construction poll
budget times out. The serve log repeats, indefinitely:

```
WARN  construction pump: no service-contract key resolves for activity — skipping dispatch
INFO  no eligible activity — cascade quiescent
INFO  pump sweep tick complete
```

In the failing run this warning fired 44 times and **zero** detailed-design or
construction agents ever spawned (`grep -c "spawning claude"` over the construction
window = 0). So it is not slow and not rate-limited — it is a hard logic stall.

## Root cause (chicken-and-egg)

`server/internal/manager/construction/constructionmanager.go`, the eligible-activity
selection (~line 755–800):

1. It picks the first eligible activity (not-started + all deps done).
2. It calls `resolveComponentID(item.Title, produced, proj.ServiceContracts)`
   (constructionmanager.go:822) to map that activity to the component it builds.
3. `resolveComponentID` resolves **only** via existing service contracts:
   - it looks through the activity's `produced` artifacts for one of
     `Kind == "service-contract"` and matches its title against
     `proj.ServiceContracts`, **or**
   - it fuzzy-matches the normalized activity title against the keys of
     `proj.ServiceContracts`.
4. On a fresh project `proj.ServiceContracts` is **empty** — no detailed-design
   has run yet — so both paths return `ok == false`, the pump logs the warning
   and **skips dispatch** (returns `constructionActivity{}, false`).

The contradiction: construction dispatch is **gated on a service contract that
only detailed-design produces**, but detailed-design is an internal phase of the
coding activity that the pump refuses to dispatch until the contract already
exists. Nothing ever populates `ServiceContracts`, so every activity is skipped
forever and the cascade goes quiescent.

Per The Method (and this codebase's own activity model — see
`the-method-activity-list`), detailed-design and construction are *internal
lifecycle phases of one coding activity*, not separate activities. So dispatching
a coding activity should itself run detailed-design first (producing the service
contract), then construct against it. The pump instead treats a resolved contract
as a **precondition** for dispatch.

## Where to look

- `server/internal/manager/construction/constructionmanager.go`
  - eligible-activity selection + the skip site (~755–800)
  - `resolveComponentID` (822) — the contract-only resolution
  - `hydrateConstructionActivity` (914)
- `server/internal/manager/construction/constructactivity.go` — how a construction
  activity is actually executed once dispatched (does it run detailed-design and
  record the contract, or expect one already present?)
- `server/internal/manager/construction/pumpnextactivity.go` /
  `pumpsweep.go` — the pump loop that logs "no eligible activity — cascade quiescent"
- `projectstate.ServiceContract` / `recordServiceContract` — how contracts get
  written, and which activity phase writes them.

## Likely fix directions (for the fixing agent to evaluate — do not assume)

- Resolve the component identity for an activity **without** requiring a
  pre-existing contract (e.g. from the activity's declared component / the
  committed system design), so the pump can dispatch. The activity's own
  detailed-design phase then records the contract before construction.
- Or split dispatch so the pump first dispatches the **detailed-design** phase for
  an activity whose component has no contract yet, then construction once the
  contract exists.
- Whatever the shape, the invariant to restore: **a coding activity with no
  service contract yet must be dispatchable** (into detailed-design), not skipped.

## How to reproduce

Run the benchmark against a rebuilt binary (see the bench repo's
`docs/running-benchmarks.md`):

```bash
cd ../archistrator && bash scripts/build-local.sh
cd ../archistrator-bench
npm run bench -- run todomvc --archistrator ../archistrator
# watch: tail -f $TMPDIR/archistrator-bench-scratch/<runId>/serve.log
# → design completes, then the "no service-contract key resolves" warning loops.
```

The failing run's captured evidence (24 design-phase episodes, serve log, and the
computed 33-activity network) is archived under
`../archistrator-bench/runs/todomvc/run-20260808T000116Z-1744cf81/`.

## Not the bug

- Not rate limiting (that was a *separate*, earlier finding — a saturated 5-hour
  subscription window; unrelated to this stall).
- Not the deterministic bench driver — the pump is server-side and fails on its
  own; the driver only polls for activities to reach terminal, which they never do.
- Not project design — the network, activity list, and all four solution options
  computed correctly.
