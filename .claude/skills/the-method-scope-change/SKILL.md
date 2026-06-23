---
name: the-method-scope-change
description: Handle scope changes during Phase 3 / construction. Architect + project-manager re-run project design to produce fresh options for management; never silently absorb scope. Event-triggered by scope addition/removal, deadline shift, resource change, or variance detected by tracking.
---

# Scope Change

Scope changes during construction are the test of whether the team really has a project, or just an aspiration. Per Löwy (App A §6.2): when scope changes, *"You now need to redesign the project to assess the consequences of the change."* This skill is the redesign.

It is **event-triggered**, not scheduled. The triggers come from outside the team (management adds or removes scope, moves the deadline, changes resources) or from inside (`[[the-method-project-tracking]]` detects structural variance — see its Step 8). Either way, the response is the same: snapshot the current state, re-enter Phase 2, produce a new SDP review revision for management, and let management decide.

Per `[[the-method-doctrine]]` Directive 7 (*educated decisions with options*): management does not get told "we're slipping by 3 weeks." Management gets a menu of redesigned options — each with duration, cost, risk — and chooses. The team's job is the design, not the decision.

## Canonical source

**Primary:** Löwy, [Appendix A "Project Tracking"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml). Specifically:

- [§5 "Projections and Corrective Actions"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml#appalev1sec5) — the variance patterns that often trigger scope change
- [§6 "More on Projections"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml#appalev1sec6)
- [§6.2 "Handling Scope Creep"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml#appalev2sec12) — the canonical scope-change procedure
- [§6.3 "Building Trust"](../../../../rightingsoftware/OEBPS/xhtml/appa.xhtml#appalev2sec13)

Per App A §6.2: *"When anyone tries to increase (or decrease) the scope of the project and asks for your approval or consent, you should politely ask to get back to them with your answer. You now need to redesign the project to assess the consequences of the change."*

## Input

- `methodpoc/designs/<product>/project/network.yaml` — current state (what construction has actually consumed of float, what remains)
- `methodpoc/designs/<product>/implementation/log/week-<NN>.md` — most recent tracking log (gives team throughput, EAC projections)
- `methodpoc/designs/<product>/project/sdp-review.md` (the original) and any prior revisions `sdp-review-<NN>.md`
- The **scope-change description** — what changed, who's asking, when
- `methodpoc/designs/<product>/project/planning-assumptions.md` — to detect whether resource or calendar assumptions are also being changed

## Output

`methodpoc/designs/<product>/project/sdp-review-<NN>.md` — a new SDP review document, numbered as the next revision (sdp-review-02, -03, ...). The original `sdp-review.md` is not edited.

Side effects:
- `network.yaml` is updated to `network.yaml` (v.NN) only if and when management accepts a new option. Until then, the original network stands.
- A short rejection log entry is written if management rejects all redesigned options.

## Procedure

### Step 1 — Capture the trigger

Write down, in `sdp-review-<NN>.md`'s opening section:

| Field | Example |
|---|---|
| Trigger type | Scope addition / scope removal / deadline shift / resource change / variance-driven (tracking detected structural slip) |
| Source | Management / customer / detected internally by `[[the-method-project-tracking]]` week-<NN>.md |
| Description | "Add export-to-PDF feature to OrderManager", "Move release date in by 4 weeks", "Senior-dev-1 reassigned", "EAC projects 25% cost overrun" |
| Date | YYYY-MM-DD |
| Current week | Construction week NN |

Per App A §6.2: do **not** answer in the room. *"Politely ask to get back to them."* The redesign takes the time it takes.

### Step 2 — Snapshot the current state

Before re-running anything, capture:

- Which activities are done, which are in-progress, which are pending (from latest tracking log)
- Total / free float consumed so far (from `network.yaml` + tracking)
- Team throughput as measured: actual EV slope from the tracking logs vs the planned slope
- Indirect cost burn vs plan
- CPI and SPI

This snapshot becomes the **baseline for the redesign**. Per App A §4, the projected EAC is calibrated to the team's measured throughput, not the original plan. You re-plan from where you are, not from week zero.

### Step 3 — Determine which Phase 2 skills to re-run

The scope-change description determines the re-entry point:

| Trigger | First skill to re-run | Why |
|---|---|---|
| Scope addition (new feature, new component, new use case) | `[[the-method-activity-list]]` | New activities are needed; the activity list itself must change |
| Scope removal (a feature is cut) | `[[the-method-activity-list]]` | Activities are dropped; check downstream impact on dependencies |
| Pure deadline shift (no scope, no resources) | `[[the-method-network-draft]]` | Activity list is unchanged; the network rebalancing happens by re-running compression or decompression |
| Resource change (more / fewer / different developers) | `[[the-method-planning-assumptions]]` then `[[the-method-network-draft]]` | Planning assumptions changed; activity list typically unchanged; network re-balances |
| Variance-driven (tracking shows the original estimates were wrong) | `[[the-method-activity-list]]` (revise durations) | Re-estimate the activities using the measured throughput |
| Architecture change requested | Back to **Phase 1** (`[[the-method-architecture]]`) | This is not a scope change; it is a system redesign |

If the trigger crosses categories (e.g., scope addition **and** deadline shift), re-run from the earliest applicable entry point.

### Step 4 — Re-enter Phase 2

Walk Phase 2 from the entry point determined in Step 3 through the SDP review:

1. **`[[the-method-planning-assumptions]]`** — only if resources or calendar changed; produce an updated `planning-assumptions.md` (or note "unchanged")
2. **`[[the-method-activity-list]]`** — add / remove / re-estimate activities; produce `activities.md` v.<NN>
3. **`[[the-method-network-draft]]`** — rebuild the network including completed and in-progress activities as "fixed"; produce `network.yaml` v.<NN>
4. **`[[the-method-normal-solution]]`** — produce a new normal option from the current state forward; the new "minimum staffing for unimpeded critical path" given what's left
5. **`[[the-method-decompressed-solution]]`** — only if normal's risk justifies decompression (per its own gate); usually yes for variance-driven changes
6. **`[[the-method-subcritical-solution]]`** — produce, to keep management honest about the cost of under-staffing
7. **`[[the-method-compressed-solution]]`** — produce, to give management a "go faster" option for the redesigned scope (and to test whether the original commitment is recoverable)
8. **`[[the-method-risk-modeling]]`** — full curves across the new option set
9. **`[[the-method-project-design-standard-check]]`** — App C §4 walked against the new artifacts
10. **`[[the-method-sdp-review]]`** — assemble `sdp-review-<NN>.md`

For a **small** scope change (e.g., one feature removed and durations re-estimated within existing activities), some of these steps will be trivial — the activity list change is a one-line edit, normal/compressed deltas are small, risk barely moves. Do not skip steps; mark them "unchanged from sdp-review-<NN-1>" if so. The audit trail matters.

For a **large** scope change (e.g., a whole subsystem added, a senior developer leaves), this is a meaningful re-design and should take days, not hours.

### Step 5 — Compare new options against the original commitment

The new SDP review's options table should include a **delta column** vs the management-committed option from the previous SDP review:

| Option | Duration | Direct cost | Total cost | Risk | Δ vs original commitment |
|---|---|---|---|---|---|
| New normal | 140 days | 30 mm | 44 mm | 0.52 | +20 days, +6 mm total cost |
| New decompressed | 150 days | 30 mm | 45 mm | 0.42 | +30 days, +7 mm total cost |
| New subcritical | 180 days | 31 mm | 49 mm | 0.65 | +60 days, +11 mm total cost |
| New compressed | 130 days | 36 mm | 47 mm | 0.61 | +10 days, +9 mm total cost |
| **Original commitment recoverable?** | 120 days | — | — | — | (no option matches; reject scope change to honour) |

Per App A §6.2: *"If they cannot afford the new schedule and cost implications, then nothing really changed. If they accept them, then you have new schedule and cost commitments for the project. Either way, you will always meet your commitments."*

The "original commitment recoverable?" row tells management whether they can have both the scope change *and* the original commitment. Usually no — that's why this is a real decision.

### Step 6 — Present to management; record the decision

Management has three possible responses:

| Response | What happens |
|---|---|
| **Accept a new option** | Update `network.yaml` to the new option's state; `sdp-review-<NN>.md` becomes the new commitment; reset `[[the-method-project-tracking]]`'s planned EV curve from this date forward |
| **Reject the scope change** | Original commitment stands; `network.yaml` is **not** updated; `sdp-review-<NN>.md` ends with a rejection note; the scope change is dropped from the project |
| **Defer** | Acceptable for a short period — the project is at risk while deferred. Document the deferral, give it an explicit decision date. |

Per App A §6.3: surface the projections and the new options across decision-makers. Trust is built by surfacing variance early and offering choices, not by absorbing variance silently.

### Step 7 — Update downstream artifacts (only on accept)

If management accepts a new option:

- `network.yaml` becomes the new option's network
- `[[the-method-project-tracking]]`'s next weekly log uses the new planned EV curve as its blue line
- If staffing changed, `handoff.md` may need an update via `[[the-method-handoff]]`
- If components were added or removed, `[[the-method-service-contract]]` must be invoked for new components (and contract files removed for cut components)

If management rejects:

- Nothing changes in artifacts other than the rejection note appended to `sdp-review-<NN>.md`
- Tracking continues against the original plan

### Step 8 — Write `sdp-review-<NN>.md`

Format (extends the structure of `[[the-method-sdp-review]]`):

```markdown
# SDP Review — Revision <NN> — <Product>

Date: <YYYY-MM-DD>
Audience: <management decision-maker(s) by name>
Original commitment: sdp-review.md (or sdp-review-<NN-1>.md) — <chosen option name>, <duration>, <total cost>, <risk>

## Trigger

| Field | Value |
|---|---|
| Type | <scope addition / removal / deadline shift / resource change / variance-driven> |
| Source | <who is asking, or which tracking log detected the variance> |
| Description | <what changed> |
| Date raised | <YYYY-MM-DD> |
| Construction week | <NN> |

## Current state snapshot (from week-<NN>.md)

| Metric | Value |
|---|---|
| Progress (EV %) | 38% |
| Effort (AC % of BAC) | 41% |
| Indirect burn | 8 mm of 12 mm planned |
| CPI | 0.93 |
| SPI | 0.95 |
| Float consumed on critical path | 5 days of 0 (already lost 5 days) |
| Near-critical chains threatened | A013–A018 (3 days remaining) |

## Re-planned options

| Option | Duration (from today) | Direct cost (remaining) | Total cost (remaining) | Risk | Δ vs original commitment |
|---|---|---|---|---|---|
| New normal | 65 days | 18 mm | 22 mm | 0.52 | +20 days |
| New decompressed | 75 days | 18 mm | 23 mm | 0.42 | +30 days |
| New subcritical | 95 days | 19 mm | 26 mm | 0.65 | +60 days |
| New compressed | 55 days | 22 mm | 25 mm | 0.61 | +10 days |

(Original commitment unrecoverable: no option lands on the original 120-day total.)

## Time-cost curve
<embedded chart>

## Time-risk curve
<embedded chart>

## Architect's recommendation

<recommended option with rationale>

## Decision

Date decision required: <YYYY-MM-DD>
Decision made: <pending | accepted: <option name> | rejected (scope change dropped) | deferred until <date>>
Decision-maker: <name>
Rationale: <management's rationale, recorded for the audit trail>

## If accepted: downstream actions

- [ ] `network.yaml` updated to v.<NN>
- [ ] `handoff.md` reviewed; updated if staffing changed
- [ ] `[[the-method-service-contract]]` invoked for new components: <list>
- [ ] `[[the-method-service-contract]]` files removed for cut components: <list>
- [ ] week-<NN+1>.md uses the new planned EV curve

## If rejected: rejection note

Scope change is dropped. Original `sdp-review.md` (or `sdp-review-<NN-1>.md`) commitment stands. Tracking continues against original plan.
```

## Exit criteria (for router)

- `sdp-review-<NN>.md` exists, numbered correctly (next sequential revision)
- Trigger and current-state snapshot are captured
- Phase 2 was re-entered from the correct skill (per Step 3)
- All four solution options were considered for the redesign (even if some are marked "unchanged from prior")
- A delta-vs-original-commitment row exists in the options table
- Management decision is recorded as one of: pending / accepted (option named) / rejected / deferred (with date)
- On accept: `network.yaml` has been updated; downstream artifact updates have been flagged or completed
- On reject: original commitment artifacts are unchanged; rejection note is appended

## When to invoke

- **Management asks for scope to be added** — even one feature
- **Management asks for scope to be removed** — even one feature; the float released should be quantified, not absorbed
- **Deadline moves** (in or out)
- **Resource roster changes** — gain or loss, especially a senior leaving
- **`[[the-method-project-tracking]]` Step 8 fires** — variance is structural and the corrective action requires re-planning
- **A near-critical chain has lost all its float** and the resulting commitment slip exceeds management's tolerance

Do **not** invoke for routine in-week corrective actions that fit inside the existing plan's float. Those are absorbed by `[[the-method-project-tracking]]`'s Step 7 corrective actions, not re-plans.

## Anti-patterns to reject

- **Absorbing scope silently** — the team takes on the scope change without telling management, on the assumption that they'll make it up later. Per App A §6.2: *"You now need to redesign the project to assess the consequences of the change."* Silent absorption is the canonical betrayal of project tracking.
- **Re-planning without management review** — producing a new plan and starting to execute against it without the SDP review step. The SDP review is the management interface; skipping it strips management of their choice and replaces Directive 7 with the team's preferences.
- **Bartering one feature for another inside the team without management** — the team decides to swap feature X (planned, on the critical path) for feature Y (newly requested, easier) without telling anyone. This is a scope change. Run the skill.
- **Treating variance-driven scope change as separate from management-driven** — both flow through the same skill. The team detecting a slip is just as valid a trigger as management asking for a feature.
- **Skipping the delta-vs-original-commitment row** — management needs to see the comparison to make a decision. Without it the options are abstract.
- **Editing `sdp-review.md` in place** — destroys the audit trail. Always produce a new numbered revision.
- **Updating `network.yaml` before management accepts** — premature commitment. Snapshot the proposed state in the new SDP review; only mutate `network.yaml` after the decision.
- **Skipping subcritical or decompressed in the re-plan** — Directive 7 requires options. A re-plan that presents only one new path is not a choice.

## Related skills

- `[[the-method-project-tracking]]` — detects the variance triggers
- `[[the-method-sdp-review]]` — the underlying SDP review structure this skill re-uses
- `[[the-method-normal-solution]]`, `[[the-method-decompressed-solution]]`, `[[the-method-subcritical-solution]]`, `[[the-method-compressed-solution]]` — the four options re-run for each scope change
- `[[the-method-risk-modeling]]` — re-run over the new option set
- `[[the-method-project-design-standard-check]]` — App C §4 walked against the redesign artifacts
- `[[the-method-handoff]]` — revisited only if staffing changes
- `[[the-method-service-contract]]` — invoked for newly-added components
