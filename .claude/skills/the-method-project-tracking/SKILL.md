---
name: the-method-project-tracking
description: Weekly project tracking during Phase 3 / construction. Capture binary activity exits, compute earned value, build projections, detect off-track and trigger corrective actions. Walks Appendix A and the Appendix C §5 standard check. Invoke weekly during construction.
---

# Project Tracking

Tracking is what turns a project design into a project. Per Löwy (App A §6 "The Essence of a Project"): *"the essence of a project is the ability to project. It is called a project because you are supposed to project. Conversely, if you do not project, you do not have a project."*

This skill runs on a weekly cadence during Phase 3. Each invocation produces one weekly log entry containing:
- Activity status (per the binary exit criteria from App A §1)
- Project progress and effort (per App A §2)
- Earned value (actual vs planned, per App A §3)
- Projections (per App A §4) — where the project is *heading*
- Corrective action triggers (per App A §5) — including the trigger for `[[the-method-scope-change]]` when variance is structural
- App C §5 standard check (six items)

Per `[[the-method-doctrine]]` Directive 9 (*be on time throughout*): the only way to meet the deadline at the end is to stay on time *throughout*. This skill is the mechanism by which that happens.

## Canonical source

**Primary:** Löwy, [Appendix A "Project Tracking"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml).

Sections walked:
- [§1 "Activity Life Cycle and Status"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml#appalev1sec1) — phase exit criteria, phase weights, activity status
- [§2 "Project Status"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml#appalev1sec2) — progress, earned value, effort, indirect cost
- [§3 "Tracking Progress and Effort"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml#appalev1sec3) — weekly cadence, plot vs planned EV
- [§4 "Projections"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml#appalev1sec4) — extrapolate progress and effort lines
- [§5 "Projections and Corrective Actions"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml#appalev1sec5) — all-is-well / underestimating / resource leak / overestimating
- [§6 "More on Projections"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml#appalev1sec6) — drive by trend, handle scope creep, build trust

**Standard reference:** [App C §5 "Project Tracking Guidelines"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec5) — six items, walked at the bottom of every weekly log.

## Input

State is git-as-DB: everything below is read from / written to `.aiarch/state/project.json`.

- The committed **network** (`.network`) — current state, with planned EV curve
- The committed **sdp-review** (`.sdpReview`) — chosen option (gives BAC = budget at completion, planned duration, planned staffing)
- The per-activity construction state (`.activityConstruction`) — each activity's phase completions, build status, and (for prior weeks) the recorded tracking points
- This week's activity completions (from `RecordPhaseCompleted` / `RecordActivityExited` events) — what passed its binary exit criterion since the last tracking point
- This week's actual direct cost (person-days × rate, per resource)
- This week's actual indirect cost burn (core team, DevOps, testers, etc.)

## Output

A weekly tracking record appended to the project state — the binary phase exits land in `.activityConstruction[activityId]` (via `RecordPhaseStarted`/`RecordPhaseCompleted`/`RecordActivityExited`), and the week's earned-value point + projection are recorded alongside the network's tracking series in `project.json`. There is **no** `implementation/log/week-<NN>.md` file; the markdown in Step 10 is a render-on-read of that typed state.

`<NN>` is the construction week number, zero-padded. Week 01 is the first week after the hand-off completes; week 02 is the next, etc.

## Procedure

### Step 1 — Walk every activity in `.network` and update its `.activityConstruction` status

Per App A §1, every activity is in one of three life-cycle states. Status changes only when a **binary exit criterion** for the activity's current internal phase has passed:

| State | Meaning | Binary exit criterion |
|---|---|---|
| **pending** | Not started | (none — activity has not yet started) |
| **in-progress** | Started, current phase not complete | A phase's exit criterion has been met (e.g., test plan reviewed, code review passed). Update the phase-completion list on the activity. |
| **done** | All phases complete with their exit criteria met | The activity's final phase exit criterion has been met |

For each activity, record the list of completed phases and which phase is currently in flight in `.activityConstruction[activityId]` (the phase completions and `currentPhase` fields). Per App A §1.1, the exit criterion is binary — a phase is either done or not done. Partial credit is not allowed. *"The `Construction` phase is complete once you have had the code review, not simply when the code is checked in."*

### Step 2 — Compute progress per activity (App A §1.3)

For each activity:

```
A_i(t) = Σ over completed phases j of W_j
```

Where `W_j` is the weight of phase `j` (from the `.activityList` / `.network` phase weights; each `.activityConstruction` phase completion carries its `weight`, summing to 100% across the activity's phases).

Example (App A Table A-1):

| Phase | Weight |
|---|---|
| Requirements | 15% |
| Detailed Design | 20% |
| Test Plan | 10% |
| Construction | 40% |
| Integration | 15% |

If Requirements + Detailed Design + Test Plan are done, the activity is 45% complete (15 + 20 + 10).

Use the **same weighting technique** across all activities (App A §1.2: *"For accurate tracking, it does not matter much which technique you use to allocate the weight of the phases as long as you apply the technique consistently."*). If the `.activityList` did not pre-assign weights, default to equal weighting (e.g., 5 phases → 20% each) and record that choice on the week-01 tracking record so it's stable for the project lifetime.

### Step 3 — Compute project progress (App A §2)

```
Progress(t) = Σ ( E_i × A_i(t) ) / Σ E_i
```

Where `E_i` is the estimated duration of activity `i` and `A_i(t)` is the activity's progress percentage at time `t`.

This is **earned value (EV) as a percentage**. It is the **actual EV to date** — what the team has actually earned, regardless of when it earned it.

### Step 4 — Compute project effort (App A §2.5)

```
Effort(t) = Σ S_i(t) / Σ R_i
```

Where `S_i(t)` is cumulative direct cost spent on activity `i` and `R_i` is the planned direct cost for activity `i`.

This is the **actual cost (AC) as a percentage of BAC**. Effort and progress are *unitless* and on the same percentage scale (App A §1.3) — that is why they can be plotted on the same chart against the planned EV curve.

### Step 5 — Plot the three lines (App A §3)

Three curves on the same axes:

- **Blue: planned EV** — the shallow S curve from the SDP-chosen option (or its truncated linear regression per App A §4)
- **Green: actual progress (EV)** — computed in Step 3, one data point per weekly tracking
- **Red: actual effort (AC)** — computed in Step 4, one data point per weekly tracking

Each tracking week appends one new point to green and red in the tracking series. Blue is fixed at project start (and only redrawn if `[[the-method-scope-change]]` formally accepts a re-plan).

Track the indirect cost separately — App A §2.6 explains why it usually isn't worth charting against the plan (linear, doesn't suggest corrective action), but you must add it to direct cost when reporting *total* spend.

### Step 6 — Project the trend lines (App A §4)

Per App A §4: *"a month into a year-long project, you already have a good idea where the project is heading via a projection that is highly calibrated to the actual throughput of the team."*

Once you have **≥ 4 data points** (typically a month into weekly tracking), fit a linear regression to the green and red lines and extrapolate forward:

| Projection | Compute | Meaning |
|---|---|---|
| **EAC (estimate at completion, time)** | Time at which the projected green line hits 100% | Projected completion date |
| **EAC (estimate at completion, direct cost)** | Effort percentage at the time projected green hits 100% | Projected direct cost as % of BAC |
| **Schedule overrun** | EAC time − planned completion date | Slippage in weeks |
| **Cost overrun** | EAC cost − 100% of BAC | Direct cost overrun percentage |
| **Indirect cost overrun** | Same as schedule overrun (App A §4 note: indirect cost is linear with time) | |

CPI / SPI (industry abbreviations Löwy does not use but produces the same numbers):

- CPI (cost performance index) = EV / AC
- SPI (schedule performance index) = EV / PV (where PV is planned EV at this date)
- EAC = AC + (BAC − EV) / CPI

CPI / SPI = 1.0 means on plan. < 1 means behind. > 1 means ahead. Use these to summarise the chart in a single number if the reader is not chart-literate.

### Step 7 — Classify the projection against App A §5 patterns

The chart shape determines the corrective action. App A §5 enumerates four patterns:

| Pattern | Green vs blue | Red vs blue | Likely cause | Corrective action |
|---|---|---|---|---|
| **All is well** (§5.1) | green ≈ blue | red ≈ blue | On plan | Do nothing. Knowing when not to act is as important as knowing when to. |
| **Underestimating** (§5.2) | green below blue | red above blue | Activities are larger than estimated | (a) Push deadline to projected green-hits-100% point, **or** (b) reduce scope so EV percentage catches up. Never throw more people at the project (Brooks's law) — except possibly at the project's very origin. |
| **Resource leak** (§5.3) | green below blue, also below red | red below blue | People assigned to your project are working on someone else's | Convene project manager + leaking project's manager + lowest common manager. Present two options: their project takes priority (push your deadline), or yours does (revoke their access, possibly extract resources from them). |
| **Overestimating** (§5.4) | green above blue | red above blue (excess parallelism) | Activities are smaller than estimated, or too many people on the project | (a) Pull deadline in (downside: customer/server/team not ready), (b) increase scope (downside: pressure), or (c) **release resources** (preferred). If detected early, downshift to a less compressed option. |

The classification dictates the corrective action. Do not skip to "throw more people on it"; App A §5.2 calls this Brooks's gasoline.

### Step 8 — Trigger downstream skills when variance is structural

Some variance patterns require formally re-entering project design:

| Trigger | Skill to invoke |
|---|---|
| Underestimating with scope-reduction chosen | `[[the-method-scope-change]]` — produces a new SDP review revision |
| Underestimating with deadline-push chosen | `[[the-method-scope-change]]` — same |
| Overestimating with downshift to less-compressed option | `[[the-method-scope-change]]` — same |
| Resource leak with deadline push | `[[the-method-scope-change]]` — same |
| Hard scope addition / removal from management | `[[the-method-scope-change]]` — same |
| All is well | None — write the log and move on |
| Resource leak resolved by management decision (project gets priority) | None — log decision, continue |
| Near-critical chain has lost all its float (App C §5.6) | Surface the float loss; if it implies a longer duration, trigger `[[the-method-scope-change]]` |

Per App C §5.6: *"Track the float of near-critical chains."* The activity-status update in Step 1 must include float consumption for each near-critical chain. When a chain's float hits zero, it becomes a second critical path; surface it.

### Step 9 — Walk the App C §5 standard check

Six items. Each PASS / WAIVED (with justification) / FAIL (return to fix before publishing the log).

| # | Guideline | How to verify against this week's log | Status |
|---|---|---|---|
| 1 | Adopt binary exit criteria for internal phases | Every activity's status update in Step 1 cites a binary criterion (review passed, test passed, etc.). No "70% done" entries. | |
| 2 | Assign consistent phase weights across all activities | Step 2 used the same weighting technique that was set in the week-01 record. | |
| 3 | Track progress and effort on a weekly basis | This tracking record exists and is dated within one week of the prior record. | |
| 4 | Never base your progress reports on features | The progress in Step 3 is computed from activities (services and noncoding) in `.network`, not "features delivered" or stories. | |
| 5 | Always base your progress reports on integration points | Activity completion is gated on integration (the integration phase or its exit criterion), not on standalone coding. | |
| 6 | Track the float of near-critical chains | The float of every near-critical chain (any chain with total float ≤ a small threshold, e.g., ≤ 5 days) appears in the status section. | |

Items 4 and 5 are non-waivable.

### Step 10 — Record the week's tracking point

The canonical form is the typed tracking record in `project.json` (the appended earned-value point + projection, with the binary phase exits already landed in `.activityConstruction`). The markdown below is the equivalent **human rendering** of that record — not a `designs/.../log/week-<NN>.md` file:

```markdown
# Tracking — Week <NN> — <Product>

Date: <YYYY-MM-DD>
Tracked by: <project manager or architect>
Construction week: <NN> of <total>

## Activity status

| Activity | State | Phases done | Phase in flight | % complete | Float remaining |
|---|---|---|---|---|---|
| A001 — Architecture | done | Req/DD/TP/C/I | — | 100% | — |
| A012 — OrderManager construction | in-progress | Req/DD/TP | C | 45% | critical (0) |
| A013 — PricingEngine construction | in-progress | Req/DD | TP | 35% | 3 days |
| A014 — InventoryAccess | pending | — | — | 0% | 8 days |
| ... | | | | | |

### Near-critical chain float (App C §5.6)
| Chain (entry → exit) | Total float | Δ vs last week |
|---|---|---|
| A013 → A018 → A024 | 3 days | -2 (lost 2 days this week) |
| A015 → A022 | 8 days | 0 |

## Project status

| Metric | This week | Last week | Δ | Planned at this date |
|---|---|---|---|---|
| Progress (EV %) | 38% | 32% | +6 pts | 40% |
| Effort (AC % of BAC) | 41% | 35% | +6 pts | 40% |
| Indirect cost burn | 8 mm | 6.8 mm | +1.2 | 8 mm |
| CPI (EV/AC) | 0.93 | 0.91 | +0.02 | 1.00 |
| SPI (EV/PV) | 0.95 | 0.93 | +0.02 | 1.00 |

## Projections (App A §4)

Trend basis: weeks 01–<NN> (≥ 4 points)

| Projection | Value | Vs plan |
|---|---|---|
| Projected completion date | <YYYY-MM-DD> | + 2 weeks |
| Projected EAC (direct cost) | 108% of BAC | + 8% |
| Projected indirect overrun | +2 weeks of overhead | |

### Chart
<embedded plot of blue planned EV / green progress / red effort with extrapolated dashed lines>

## App A §5 classification

**Pattern: Underestimating (mild)** — green slightly below blue, red slightly above blue.

Likely cause: A012 OrderManager construction is taking longer than estimated; A013 PricingEngine TestPlan also dragged.

## Corrective action

<chosen action> per App A §5.2.

Triggers `[[the-method-scope-change]]` to produce a new `.sdpReview` revision? **<Yes | No, monitoring for one more week>**

## App C §5 standard check

| # | Guideline | Status | Justification (if waived) |
|---|---|---|---|
| 1 | Binary exit criteria | PASS | |
| 2 | Consistent phase weights | PASS (technique: per-phase duration ÷ total, set week-01) | |
| 3 | Weekly tracking | PASS (dated <date>, prior week dated <date>) | |
| 4 | Not feature-based | PASS | |
| 5 | Integration-based | PASS | |
| 6 | Near-critical float tracked | PASS (table above) | |

## Notes / actions
- <e.g., dev-2 is back from leave; A013 should accelerate next week>
- <e.g., requested clarification on A019's scope; pending response from PM>
```

## Exit criteria (for router)

- A week-`<NN>` tracking point is recorded in `project.json`, dated within 7 days of the week-`<NN-1>` point (App C §5.3)
- Every activity in `.network` has a status, % complete (in `.activityConstruction`), and float-remaining (for near-critical chains)
- Progress, effort, indirect cost, CPI, and SPI are computed
- Once ≥ 4 weekly data points exist, EAC projections are present
- A pattern from App A §5 is named
- If a corrective action triggers `[[the-method-scope-change]]`, that skill has been invoked or scheduled
- All six App C §5 items are PASS or WAIVED — items 4 and 5 are PASS only

## When to invoke this skill

- **Weekly**, ideally on the same day each week (App A §3 / App C §5.3)
- **Ad-hoc** when an integration phase exit happens off-cadence and management asks for status
- **Never less than weekly** — App C §5.3 is explicit: weekly is the cadence; more frequent is fine, less frequent is a FAIL

## Relationship to scope change

This skill *detects* variance. `[[the-method-scope-change]]` *responds* to variance that exceeds the team's ability to absorb. Most weeks, no scope change is needed — the corrective action is internal (reassign, eliminate a bottleneck, fix a code-review backlog). Scope change is triggered only when the variance is structural:

- Deadline must move
- Resources must change in volume
- Project's scope (functional or non-functional) must change
- The chosen SDP option (normal / decompressed / compressed / subcritical) is no longer viable

When in doubt, run this skill's projection through one more week before triggering scope change. App A §6 ("drive looking forward, not at the pavement") — but App A §5.1 ("knowing when not to do something is as important") — both apply. Detect early, act once.

## Anti-patterns to reject

- **Reporting on coding only** — App A §3 / App C §5.5 require progress reports based on **integration points**, not lines of code, commits, or stories. Until an activity's integration exit criterion has passed, the activity is not done.
- **Silent variance absorption** — if CPI < 1.0 or SPI < 1.0 and you log "everything is fine," you are lying to the project. Variance gets harder to absorb the longer it goes unsurfaced.
- **Per-developer status instead of activity status** — "Alice 80%, Bob 60%" is meaningless. Activity status is what tracks against `.network`. Per-developer status is a side channel.
- **Feature-based progress reports** — App C §5.4 non-waivable. Features are not units of progress; integration points are.
- **Throwing people at variance** (App A §5.2 / Brooks) — almost always makes the project worse. The exception is at the project's origin, where you can re-pivot to a more-compressed SDP option.
- **Reactive corrections** — App A §6: drive by trend, not by current state. Reacting after green has fallen far below blue means the corrective action must be much larger and more disruptive.
- **Skipping a weekly log because "nothing changed"** — there is always something to log: the activity-status table, the float of near-critical chains, the projection. Skipping a log breaks the trend and weakens the projection.
- **Inconsistent phase weights** — App C §5.2 violated. Week-01 sets the weighting technique for the whole project; subsequent weeks must use the same technique.
- **No near-critical float tracking** — App C §5.6 violated. Critical-path-only tracking misses the chain that *becomes* critical next week.
