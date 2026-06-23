---
name: the-method-planning-assumptions
description: Project Design — capture explicit planning assumptions (resources, calendar, infra, dependencies). Without these, the project network is meaningless. Architect drives drafting; project-manager contributes constraint data. Reads the committed systemDesign artifact in project.json. Produces the typed PlanningAssumptions committed to project.json → .planningAssumptions. Invoke as the first phase of project design, before [[the-method-activity-list]].
---

# Planning Assumptions

The project network calculates duration and cost as functions of resources and dependencies. Without explicit planning assumptions, any solution is fiction. App C calls this out as a hard guideline: *"Capture and verify planning assumptions."*

## Canonical source

**Primary:** Löwy, [Ch. 11 §1.4 "Planning Assumptions"](../../../../rightingsoftware/OEBPS/xhtml/ch11.xhtml#ch11lev2sec4) — the worked example shows what to capture.

**Supporting:**
- [Ch. 7 §3 "Educated Decisions"](../../../../rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev1sec3) — why planning assumptions matter
- [Ch. 7 §3.1 "Plans, Not Plan"](../../../../rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07lev2sec4) — assumptions drive multiple plans
- [Ch. 13 §1 "Estimations"](../../../../rightingsoftware/OEBPS/xhtml/ch13.xhtml#ch13lev1sec1) — second worked example, planning assumptions section

**Standard reference:** [Appendix C §4.1c "Capture and verify planning assumptions"](../../../../rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec4).

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` files. Markdown/DSL is a render-on-read of the typed state, never the source of truth.

- The committed **systemDesign** artifact in `project.json` → `.systemDesign` (the architecture decomposition; component count drives team-size estimates)
- User input via interactive dialog (collected during this phase)

## Output

The planning assumptions are a **typed model committed into `.aiarch/state/project.json` → `.planningAssumptions`** — git is the database. It is NOT a `designs/<product>/project/planning-assumptions.md` file; any markdown (including the Step 2 template) is a render-on-read of that JSON slot.

Two usage patterns produce this slot:

1. **Agentic/CI dispatch:** the agent produces the typed `PlanningAssumptions` model as JSON and commits it into `.planningAssumptions` on its session branch; the server reads it back and stages it (`StageArtifactForReview`) for the human review gate, where it is reviewed via `CommitArtifact` / `RejectArtifact`.
2. **Local interactive:** same — produce the typed model and write it into the `.planningAssumptions` slot. Never a `designs/*.md` file.

## Procedure

The architect drives this; the project-manager contributes resource/calendar data and political constraints.

### Step 1 — Interactive gather

Ask the user **one question at a time**. Prefer multiple choice where applicable.

**Team composition:**
1. Who's on the core team? Architect, project manager, product manager — same person, different people?
2. What's the available developer pool? How many senior, how many junior? (Use Löwy's definition of senior: capable of designing detailed contracts.)
3. Test engineers — dedicated or shared?
4. DevOps support — full-time, part-time, on-call?
5. External experts available? (UX, security, DBA, performance — per ch. 13 noncoding activities)

**Calendar:**
6. Holidays / vacation windows in the planning window?
7. Other projects competing for the same people? What percentage of capacity goes to those?
8. Hard deadlines (regulatory, contractual, public-event-driven)?

**Infrastructure:**
9. Build / CI infrastructure available, or must we build it?
10. Production environment exists, or must we provision it?
11. Required integrations with existing systems (legacy, third-party)?

**Quality:**
12. Defect leakage tolerance? (Ch. 14 §8: quality is a planned activity, not free.)
13. Required performance SLAs?
14. Required compliance / audit standards?

**Political / organizational:**
15. Stakeholders who must approve milestones?
16. Customer or executive deadlines that are immovable?
17. Anything the team is *not allowed* to change (legacy systems, vendor contracts)?

Record raw answers as you go.

### Step 2 — Normalize into the typed planning-assumptions model

After the dialog, produce the typed `PlanningAssumptions` model and commit it to `.aiarch/state/project.json` → `.planningAssumptions`. The markdown below is the equivalent **human rendering** of that JSON — use it to review the content, but the source of truth is the slot, not a `*.md` file:

```markdown
# Planning Assumptions — <Product>

Captured: <YYYY-MM-DD>
By: <architect-name>
Verified by: <project-manager-name>

## Team

### Core team
- **Architect:** <name or role>
- **Project Manager:** <name or role>
- **Product Manager:** <name or role>

### Developer pool
- Senior developers: N available, M% capacity (rest leaks to other projects)
- Junior developers: N available, M% capacity
- Note: "senior" per Löwy = capable of detailed contract design

### Specialists
- Test engineer: dedicated / shared / external
- DevOps: dedicated / shared
- UX/UI: external expert, available for ~N days per quarter
- Security expert: external, on-demand
- DBA: external, on-demand

## Calendar

- Planning window: <start> to <target>
- Working days/week: 5
- Holiday days within window: N (list dates)
- Known vacation: <list>
- Effective working days: N

## Infrastructure assumptions

- Build/CI: available / must-build (specify)
- Production environment: exists / must-provision
- Integration targets: <list>

## Quality assumptions

- Defect leakage target: <number>
- Performance SLAs: <list>
- Compliance: <list>

## Political/organizational constraints

- Hard deadlines: <list>
- Immovable scope items: <list>
- Approval gates: <list>
- Stakeholder dependencies: <list>

## Risk flags from assumptions

- <e.g., "Only one senior developer for detailed design — bus-factor 1 on critical path">
- <e.g., "UX expert availability uncertain in Q3">
```

### Step 3 — Verify

Hand to the project-manager (or the user) to verify each line. Assumptions that can't be verified become **explicit risks** that get tracked into the project design.

Per ch. 11: *"Rarely will someone hand you the planning assumptions on a silver platter... Some form of discovery, back-and-forth, and negotiation always takes place at the front end of the project as you try to distill your specific planning assumptions. You can even reverse this flow: Start with your take on the planning assumptions and staffing distributions, captured as suggested here, and then ask for feedback and comments."*

### Step 4 — Flag risk-laden assumptions

For each assumption that has a non-trivial probability of being wrong (e.g., "we can hire 2 more seniors in 60 days"), flag it in the **Risk flags** section. These will feed into:
- Activity dependencies in [[the-method-activity-list]]
- Risk model in [[the-method-risk-modeling]]
- SDP review options in [[the-method-sdp-review]]

## Exit criteria (for router)

`.aiarch/state/project.json` → `.planningAssumptions` holds a committed typed model with all sections populated. PjM has verified. Risk flags identified.

Move to `the-method-activity-list`.

## Anti-patterns to reject

- **Implicit assumptions** ("we'll have a test engineer when we need one") — make it explicit or drop.
- **Optimistic team availability** without leak factor — every shared resource has leak; quantify it.
- **No holiday/vacation accounting** — December projects always slip.
- **"We can hire to fix this"** — Brooks's Law. Treat the current team as the binding constraint.
- **Hidden political deadlines** — surface them now or they'll surprise the SDP review.
