---
name: the-method-activity-list
description: Project Design — produce the activity list (coding + noncoding) with 5-day quantum estimates. ONE coding activity per component (detailed-design and construction are internal lifecycle phases, not separate activities), plus integration and noncoding. Reads the committed systemDesign and planningAssumptions artifacts in project.json. Produces the typed ActivityList committed to project.json → .activityList. Invoke after [[the-method-planning-assumptions]], before [[the-method-network-draft]].
---

# Activity List

The architecture defines what to build. The activity list says how the work decomposes into estimable units. Every activity is in 5-day quanta, ≤35 days, with role assignment and behavioral dependencies.

## Canonical source

**Primary:**
- Löwy, Ch. 7 §5 "Effort Estimations" — estimation rules
- Ch. 7 §5.3 "Activity Estimations"
- Ch. 11 §1.2a "List of Activities" — first worked example
- Ch. 13 §1.1 "Individual Activity Estimations" — second worked example

**Noncoding activities reference:** Ch. 13 — Table 13-3 shows the full noncoding activity inventory from TradeMe.

**Standard reference:** Appendix C §4.4 "Estimations" — quantum of 5 days, no god activities, accuracy over precision.

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` files. Markdown/DSL is a render-on-read of the typed state, never the source of truth.

- The committed **systemDesign** artifact in `project.json` → `.systemDesign` — the architecture decomposition: each component → coding activities; relationships → integration activities. (When rendered as Structurizr DSL, each component is a `container`.)
- The committed **planningAssumptions** artifact in `project.json` → `.planningAssumptions`

## Output

The activity list is a **typed model committed into `.aiarch/state/project.json` → `.activityList`** — git is the database. It is NOT a `designs/<product>/project/activities.md` file; any markdown (including the tables below) is a render-on-read of that JSON slot.

Two usage patterns produce this slot:

1. **Agentic/CI dispatch:** the agent produces the typed `ActivityList` model as JSON and commits it into `.activityList` on its session branch; the server reads it back and stages it (`StageArtifactForReview`) for the human review gate (`CommitArtifact` / `RejectArtifact`).
2. **Local interactive:** same — produce the typed model and write it into the `.activityList` slot. Never a `designs/*.md` file.

## Worker classes and the activity inventory — canonical doctrine

Two normative rules govern this activity list: every activity's `workerClass` is drawn from the fixed Method team roster (and must have a `rateCard` entry in the committed PlanningAssumptions), and each activity is named with its short network id under the fixed prefix conventions — with the generated client transport tier getting NO coding activity and the standard UI/testing inventory ALWAYS emitted. The canonical statements of both rules live under **Draft-job doctrine → Worker classes are a fixed roster** and **→ Activity inventory** below; they apply identically here. Where the book says UX-designer or DevOps, use `ui-designer` and `senior-developer`.

## Procedure

### Step 1 — Coding activities per component

**One activity per component.** For each component declared in the committed `systemDesign` artifact (every component is a `container` in the rendered DSL), emit **exactly one** coding activity. Detailed design and construction are **internal phases of that activity's lifecycle** (App A — every activity "is its own little life cycle" with Requirements → Detailed Design → Test Plan → Construction → Integration phases), dispatched to different roles per phase (senior-developer designs the contract in the detailed-design phase; junior-developer builds in the construction phase per [[the-method-handoff]]). They are **not** two separate activities.

> This is the deliberate correction of the "clock": do NOT emit a `D###` design activity *and* a `C###` construction activity per component. The base activity list is one activity per component; the per-phase role hand-off lives inside the lifecycle. Pulling contract design out into a *separate* activity is a **compression technique** — see [[the-method-compressed-solution]] — applied selectively (to components others build against, to break dependencies and parallelize), never universally in the base list.

> ID-prefix convention (load-bearing, not just recommended): `C-<abbrev>` the single per-component coding activity, `R-*` resource provisioning, `U-SPA*` SPA/webApp construction (the ONLY prefix classified as frontend downstream), `G-*` UI-design concepts, `I-*` integration, `N-*` noncoding (testing variants key on `N-STP`/`N-STH`/`N-PERF`/`N-IT`/`N-QA`). (`D###` design-first activities appear ONLY in the compressed solution, never the base.) In the typed model the activity `name` IS this short network id; the human-readable label goes in `title`. Downstream classifiers (DeriveType / DeriveVariant / ClassifyType) key on these prefixes — a prose name like `webapp-client-coding` classifies as a generic service.

Format each entry:

```markdown
| ID | Name | Type | Component | Role | Duration (days) | Depends on |
|---|---|---|---|---|---|---|
| C001 | OrderManager | coding | OrderManager | senior→junior (per-phase) | 20 | (its dependencies' contracts) |
```

The single duration covers the whole lifecycle (design + build + test-plan + integration phases); the profile weights (App A Table A-1) apportion it across phases. Dependencies are on other components' *activities* (their frozen contracts become available as their detailed-design phase completes).

**Sizing rules:**
- A component's single activity spans its full lifecycle; size it to the whole thing (design phase ≈ 20% + construction ≈ 40% + the rest per the App A weights).
- Construction durations vary by component size and layer. Typical:
  - Manager: 15–30 days
  - Engine: 10–20 days
  - ResourceAccess: 5–15 days
  - Resource (when we build it): 10–20 days
  - Client: 15–35 days (often the largest)
  - Utility: 5–15 days

### Step 2 — Integration activities

For each cluster of components that integrate together, add an integration activity. Per App C: *"avoid integration at the end of the project"* — integrations happen incrementally.

Identify integration points from the relationships in the committed `systemDesign` artifact. Typical patterns:

```markdown
| A040 | Integrate OrderManager ↔ PricingEngine | integration | (composite) | senior-developer + test-engineer | 5 | A012, A024 |
| A041 | Integrate OrderManager ↔ Message Bus | integration | (composite) | senior-developer | 5 | A012, A060 |
```

Integration depends on the construction of both sides.

### Step 2b — Standard UI-Design and Test-Plan activities

Two activities are **always emitted** (not left ad-hoc), because every plan needs them and their reviewers are fixed by role:

**UI-Design activity (only for products with a UI surface — a Client + SPA/app container).** Emit one UI-design activity, prefix `G###`, role `ui-designer`, sequenced *before* the UI construction activities (the UI construction depends on it). The designer produces UI concepts; review is computed at construction time by `[[the-method-review-routing]]` (founder/architect-user + ux-reviewer + product-manager + architect) — do **not** stamp reviewers here.

| G001 | UI design concepts for the SPA | ui-design | reactSPA | ui-designer | 15 | (manager detailed-designs) |

**Testing activities (always).** Per Löwy's testing doctrine ([[the-method-testing]]) — unit testing alone is "borderline useless"; the load-bearing verification is full regression of the integrated system — emit, **not** BDD/Gherkin specs:

- a **System Test Plan** (`N-STP`, role `test-engineer`) — the ways to prove the integrated system fails, traced to the core use cases; early and high-float;
- a **System Test Harness** (`N-STH`, role `test-engineer`) — code that drives the system to break it (best-fit tech: Playwright for UI/SPA E2E, Go for API/integration; no Gherkin layer);
- a **Regression Test Harness** (`N-RTH`, role **`senior-developer`** — Löwy: regression harness is *developer-owned*, distinct from the test-engineer's system harness);
- **daily build + smoke** (`N-SMOKE`, role `senior-developer` — the roster has no devops class);
- a process **QA** activity (`N-QA`, role `qa-engineer`) — *"what will it take to assure quality?"*, distinct from test execution;
- a terminal **System Testing** gate (end-of-project, role **`software-tester`** — Löwy: testers run system testing; aim for a 1:1–2:1 tester:developer ratio).

Per-service test plans (STP) are written *before* each component's construction and live inside the construction activity — do not emit one activity per STP. Their review (`system-architect` + `product-manager` + `qa-engineer`) is computed at construction time by `[[the-method-review-routing]]` (`artifactKind: test-plan`).

| N-STP | System Test Plan (all core UCs) | noncoding | test-engineer | 15 | — |
| N-STH | System Test Harness (Playwright + Go) | noncoding | test-engineer | 20 | N-STP |
| N-RTH | Regression Test Harness | noncoding | senior-developer | 15 | N-STP |

Routing note: reviewer sets are **never** columns in this table — they are dynamic (see `[[the-method-review-routing]]`). This step only guarantees the *work* exists; who reviews it is computed when it is performed.

### Step 3 — Noncoding activities

Per ch. 13 (TradeMe second example), noncoding activities cluster at the beginning and end of the project. Walk through this checklist and add what applies.

**Beginning of project:**
- Requirements analysis (formal pass beyond `/system-design`)
- Architecture review with management
- Project planning (this very phase + downstream phases)
- System test plan + system test harness (test-engineer; early, high-float)
- Regression test harness (developer-owned)
- Quality-assurance process + gates (qa-engineer)
- Development environment setup
- Build / CI infrastructure + daily build & smoke
- Source control setup
- Database/schema design (the model, not RA code)
- Security review
- UX design (often a phase-long activity per ch. 11)

**Middle of project:**
- Code review activities (folded into construction in some teams; explicit otherwise)
- Documentation
- Architecture refinement / ADRs

**End of project:**
- System testing (terminal gate; run by software-tester)
- Performance testing
- Hardening / bug fix
- User acceptance testing
- Production deployment
- Training
- Documentation finalization
- Handover

Format:

```markdown
### Noncoding activities

| ID | Name | Type | Role | Duration (days) | Depends on |
|---|---|---|---|---|---|
| N001 | Requirements analysis | noncoding | product-manager | 10 | — |
| N002 | UX design | noncoding | ui-designer | 25 (spans entire UI phase) | N001 |
| N003 | Build/CI setup | noncoding | senior-developer | 10 | — |
| N004 | Production environment provisioning | noncoding | senior-developer | 15 | N003 |
| N005 | Integration testing | quality | test-engineer | 15 | (all construction done) |
| N006 | Hardening | quality | senior-developer + junior-developer | 10 | N005 |
| N007 | Deployment | noncoding | senior-developer | 5 | N006 |
| N008 | Training | noncoding | product-manager | 5 | N007 |
```

### Step 4 — Apply estimation rules (App C §4.4)

For each activity, verify:

| Rule | Check |
|---|---|
| Quantum of 5 days | duration is a multiple of 5 |
| No god activities | duration ≤ 35 |
| Resource assigned | role column not empty |
| Strive for accuracy, not precision | Don't estimate to 11.5 days; use 10 or 15 |
| Reduce estimation uncertainty | If you're guessing wildly, break the activity down |

If any duration > 35 days, split. Per ch. 12 §1: *"god activities" hide complexity and corrupt the network.*

### Step 5 — Overall project estimation cross-check

Per App C §4.4e: *"Estimate the project as a whole to validate or even initiate your project design."*

Use a broadband technique:
- Sum activity durations (total effort, person-days)
- Apply optimism reduction (typically multiply by 1.2–1.5 based on team's historical accuracy)
- Compare to your prior project estimation

If the sum is wildly different from a broadband estimate, something is off — either the activity list is missing things, or the estimates are biased.

Document the overall estimate in the `.activityList` model (rendered at the bottom of the activity list):

```markdown
## Overall project estimate (cross-check)

- Sum of activity durations: <N> person-days
- Broadband estimate (architect's gut): <N> person-days
- Reconciliation: <comment>
```

### Step 6 — Roles and phases table

Per ch. 11 Table 11-2 / ch. 13 Table 13-4, build the roles-and-phases mapping:

```markdown
## Roles and Phases

| Role | Phase 1 (design) | Phase 2 (build) | Phase 3 (integrate) | Phase 4 (harden) | Phase 5 (deploy) |
|---|---|---|---|---|---|
| Architect | X | X | X | X | X |
| Project Manager | X | X | X | X | X |
| Product Manager | X | X | X | X | X |
| Senior dev | X | X (incl. regression harness) | X | X | |
| Junior dev | | X (unit + STP tests) | X | X | |
| Test engineer | X (test plan + harness) | X (harness build) | X | X (perf) | X |
| Software tester | | | X (system test) | X (system testing) | |
| QA engineer | X (gates) | X (process audit) | X | X | X |
| UX designer | X | X | | | |
| DevOps | X | X | X | X | X |
```

Per Löwy ch. 9: the **test engineer** (builds harnesses, writes code to break the system), the **software tester** (runs system testing; 1:1–2:1 tester:developer ratio), and the **QA engineer** (senior, process — "what will it take to assure quality?") are three *distinct* roles. Do not collapse them.

The table keeps the book's row names; in the typed model the "UX designer" row is `ui-designer` and the "DevOps" row is `senior-developer` (the fixed roster has no devops class).

This is "a crude staffing distribution" (ch. 11) — it confirms which roles span the whole project and which are activity-specific.

## Draft-job doctrine (CI dispatch)

This is the normative task the CI draft job (and a local `/project-design` run) executes to produce the `ActivityList`. It is self-contained: everything a draft agent needs to author a sound activity list is stated here.

Convert the architecture into the activity list. Emit exactly ONE coding activity per component of the committed System, named after that component — detailed design and construction are internal lifecycle phases of that single activity (a per-phase role hand-off), NOT separate network nodes; do NOT split a component into a D### design activity and a C### construction activity in the base list. Integration (I-*) and noncoding (N-*) activities — test plan, test harness, environment setup, etc. — are separate activities. Give each activity its effort in 5-day quanta, its worker class, and a Fibonacci risk bucket.

### Activity inventory

NAME each activity with its SHORT NETWORK ID using the fixed prefix conventions — downstream classifiers key on them: C-<abbrev> per-component coding, U-SPA* SPA/webApp construction (the frontend), G-* UI-design concepts, I-* integration, R-* resource provisioning, N-* noncoding — and put the human-readable label in title. GENERATED CLIENT TIER: the platform GENERATES the entire transport scaffolding from the committed service contracts — REST handlers, typed API clients, MCP tool surfaces, and the OpenAPI document — so a client-tier component whose substance is that generated transport (an api / mcp / agent client) gets NO coding activity; do not plan work the generator does. The handwritten UI IS real work: for a product with a UI surface emit one G-SPA ui-design activity (ui-designer) and U-SPA* construction activities (junior-developer, one per core-use-case screen cluster), with UI construction depending on G-SPA. Emit one I-UC* integration activity per core use case (senior-developer). ALWAYS emit the standard testing set: N-STP system test plan (test-engineer), N-STH system test harness — the Playwright UI-test and Go system-test rigs (test-engineer), N-RTH regression test harness (senior-developer), N-SMOKE daily build and smoke (senior-developer), N-QA process QA (qa-engineer), N-PERF performance testing (test-engineer), and the terminal N-IT system-testing gate (software-tester).

### Worker classes are a fixed roster

WORKER CLASSES ARE A FIXED ROSTER, not open vocabulary: every worker class MUST be spelled exactly as one of system-architect, product-manager, project-manager, senior-developer, junior-developer, ui-designer, ux-reviewer, qa-engineer, test-engineer, software-tester — the Method team the platform actually dispatches. NEVER invent a domain-, component-, or platform-flavored class (no Capture-Engineer, no Platform-DevOps-Engineer): an unknown class silently rides default token rates in the cost engines and misclassifies in every downstream view. Typical assignment: junior-developer builds components and the SPA; senior-developer integrates and owns regression/CI/smoke/provisioning; system-architect owns schema and ADR work; ui-designer the UI-design concepts; test-engineer the system test plan, harness, and perf rig; qa-engineer the QA process; software-tester the terminal system-testing gate.

## Exit criteria (for router)

`.aiarch/state/project.json` → `.activityList` holds a committed typed model with:
- One coding activity per component (detailed-design + construction are internal lifecycle phases, NOT separate activities; no `D###`/`C###` pair per component in the base)
- Integration activities for each major relationship cluster
- Noncoding activities from the checklist
- All durations in 5-day quanta, ≤35 days, with role assignments
- Every `workerClass` is from the fixed roster (see "Worker classes are a fixed roster") and has a PlanningAssumptions `rateCard` entry; activity `name`s follow the ID-prefix convention (`U-SPA*` for SPA work, `N-ST*` variants for testing)
- NO coding activity for generated client-tier transport (api/mcp/agent clients); `U-SPA*` activities exist for the handwritten UI
- Overall estimate cross-check
- Roles-and-phases table
- A `G###` UI-design activity exists for any product with a UI surface, sequenced before UI construction
- Testing activities are present (always): system test plan (`N-STP`), system test harness (`N-STH`), regression harness (`N-RTH`), daily build/smoke (`N-SMOKE`), QA process (`N-QA`), terminal system testing — with no reviewer columns (routing is dynamic per `[[the-method-review-routing]]`). No BDD/Gherkin.

Move to `the-method-network-draft`.

## Anti-patterns to reject

- **Single "implement everything" activity** — god activity; split per component.
- **A `D###` + `C###` pair per component in the base list** — this is the "clock." Detailed design is a *phase* of the one per-component activity, dispatched to the senior via the per-phase hand-off ([[the-method-handoff]]), not a separate activity. Separate design-first activities belong ONLY in the compressed solution ([[the-method-compressed-solution]]), applied selectively.
- **No noncoding activities** — projects don't ship without UX, infra, deployment, training. Force the inventory.
- **No integration activities** — integration-at-end is App C anti-pattern. Schedule incremental.
- **Durations like 7, 11, 22 days** — break the quantum rule. Round to 5/10/15/20/25/30/35.
- **A single role for everything** — flatten the team's skill diversity; misses the senior-hand-off opportunity.
