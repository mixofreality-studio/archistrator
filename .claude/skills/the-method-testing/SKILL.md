---
name: the-method-testing
description: Reference for Juval Löwy's testing & quality doctrine in The Method (Righting Software ch. 2/9/11/12/13/14, App A). The authoritative source for the testing strategy — test types, when tests are written, the three quality roles, and where testing sits in the activity graph. Use when another skill or activity needs the by-the-book testing classification. No BDD/Gherkin.
model: inherit
skills: the-method
---

# The Method — Testing & Quality Doctrine

The authoritative statement of how Löwy treats testing and quality assurance.
Other skills ([[the-method-activity-list]], [[the-method-review-routing]],
[[the-method-handoff]]) cite this rather than re-deriving it.

Canonical sources (book in repo at `../../../research/rightingsoftware/`): ch02
(unit vs regression), ch09 (roles, cost, daily build), ch11 (test activities in
the network), ch12 (quality multiplication), ch13 (TradeMe staffing), ch14
(the hand-off, smoke tests, QA), App A (per-service test plan).

## 1. Test types (and Löwy's stance)

- **Unit testing** — *necessary but "borderline useless" on its own* (ch02):
  defects live in the *interactions between units*, not the units. Never treat
  passing unit tests as system verification ("the streetlight effect").
- **Regression testing — the load-bearing verification** (ch02). *"The only
  way to verify change is full regression testing of the system, its
  subsystems, its components and interactions, and finally its units."*
  Volatility-based decomposition exists partly to make end-to-end +
  per-subsystem + per-component regression feasible.
- **Integration testing** — folded into regression, per use case.
- **System testing** — a distinct, **terminal** network activity.
- **Smoke tests** — daily clean build + power-up to "exercise the plumbing."
- **White-box test client** + **black-box unit testing** against the test plan
  (per service, App A).

> **No BDD/Gherkin.** Löwy never mentions it. In aiarch we removed the BDD
> layer entirely. The load-bearing harness is **wire-level and black-box**
> against the system's Client surfaces (see §7): **Go** — a *separate module*,
> own `go.mod`, sibling to the server, importing zero server code — driving
> HTTP via `net/http` and MCP via the Go MCP SDK; **Playwright** for any SPA/UI
> E2E (browser-driven, hence inherently out-of-process even though TS).
> Per-component white-box unit tests are minimized to near-zero (§1's "borderline
> useless"; §7 R2).

## 2. When tests are written — test-PLAN-first, NOT TDD

Löwy is *test-plan-first*, not test-first. There is no red-green-refactor.

Per-service life cycle (App A, Table A-1):
`Requirements → Detailed Design → Test Plan → Construction → Integration`.

- The developer writes a **Service Test Plan (STP)** *before* coding — *"a list
  of all the ways the developer will later demonstrate the service does not
  work."*
- The white-box test client is built **in tandem with** construction.
- Black-box unit tests run against the STP; then integration + regression.

The discipline: **plan the tests, then build code and tests together, then
integrate and regression-test.**

## 3. The three quality roles (do NOT collapse them)

Löwy (ch09) prescribes three *distinct* roles. QA ≠ testing.

| Role | What | Owns | Agent |
|---|---|---|---|
| **Test engineer** | A full engineer who writes code to *break* the system. Not a tester. *"Every software project should have a test engineer."* | System Test Plan + System Test Harness (early, high-float); perf rig | `test-engineer` |
| **Software tester** | *Runs* system testing + regression; files defects. Wants **1:1–2:1** tester:developer ratio. | System Testing (terminal) | `software-tester` |
| **QA engineer** | A *single senior* expert on the *process*: "what will it take to assure quality?" *Not* test execution. *"A sign of organizational maturity."* | Quality gates, process audit, defect taxonomy | `qa-engineer` |

Plus: **developers** write their own STP + unit tests, and **own the
Regression Test Harness** (ch13: regression harness is developer-owned, system
harness is test-engineer-owned). If quality is high enough, developers are *not
needed during system testing* (ch11).

## 4. Where testing sits in the activity graph

State is git-as-DB: these testing outputs are typed records in
`.aiarch/state/project.json` → `.testingState` (system test plan, harness, perf
rig, quality gates, defects, test runs), NOT `designs/*.md` files. The
per-service Service Test Plan a developer writes is the `.phaseArtifacts.testPlan`
entry for the component. Per-activity status lives in `.activityConstruction`.

| Activity | Position | Float | Owner | project.json target |
|---|---|---|---|---|
| System Test Plan (`N-STP`) | early | high | test-engineer | `.testingState.systemTestPlan` |
| System Test Harness (`N-STH`) | early | high | test-engineer | `.testingState.harnessModule` |
| Regression Test Harness (`N-RTH`) | early, run continuously | — | senior-developer | harness module (sibling to server) + `.activityConstruction` |
| Daily build + smoke (`N-SMOKE`) | ongoing | indirect | devops | CI (`GOWORK=off go build/test` under `server/`) |
| QA process + gates (`N-QA`) | spans project | — | qa-engineer | `.testingState.qualityGates` / `.testingState.qualityAuditReport` |
| System Testing (`N-IT`) | **terminal, on critical path** | 0 | software-tester | `.testingState.testRuns` + `.testingState.defects` |
| Performance test (`N-PERF`) | terminal | — | test-engineer | `.testingState.perfHarness` |

Test Plan / Test Harness are **early high-float enablers** — deferring them
"consumes 77% of their float… very risky" (ch11). System Testing is the
terminal gate (~15% of project value, ch07) and largely resists compression.

## 5. Layers and testing (structural, not type-per-layer)

Löwy does **not** map a test *type* to each layer. The mapping is structural:
- Each **service** (any layer) gets STP + unit + integration.
- The **system** gets the System Test Plan, System Test Harness, Regression
  Harness, and System Testing.
- Features — and thus end-to-end testability — emerge only once the
  **Managers** integrate the lower layers (ch07), so System Testing depends on
  Clients + Managers being integrated.
- aiarch specifics (governed by §7): invariants that *look* per-layer —
  **Engine** determinism, **ResourceAccess** idempotency (same-content →
  same-SHA) — are **promoted to an observable at the Client surface and asserted
  black-box**, never via a white-box test reaching into the package. Manager
  Temporal workflows, Client↔Manager wiring, and the SPA are all exercised
  through the **wire-level Client-surface harness**; the whole system through
  regression + system testing. Only a genuinely un-surfaceable invariant keeps
  one contract-level check, and it never gates alone.

## 6. Cost & economics

- System Testing ≈ **15%** of project value (ch07). Within a service: Test Plan
  10% + Integration 15% (App A).
- **Quality multiplies** (ch12): system quality = *product* of component
  qualities (10 components at 90% → 35% system quality). You cannot skimp.
- *"Quality is not free, but it does tend to pay for itself"* (ch14); quality →
  productivity → shorter schedule (ch09).
- Test engineer + testers building/running harnesses = **direct cost**; ongoing
  regression + daily build/smoke = **indirect cost**.

## 7. Test-authoring constitution (binding for archistrator-built systems)

Löwy's three roles are *distinct humans he trusts as professionals*. In aiarch
the "developer" and the "test engineer" are **AI agents**, and a single agent
writing both a component and its own test is a **cheating surface Löwy never
faced**: the test can be shaped to the implementation's shortcuts, or assert on
internals instead of contract behaviour. This constitution makes the role
separation and the black-box surface **structurally binding, not cultural**. It
applies to archistrator itself and to **every system archistrator builds**.

- **R1 — Every test is black-box against a published contract.** No test imports
  a system's internal packages. The load-bearing verification is wire-level and
  out-of-process. White-box, in-package, internals-reaching tests are an
  anti-pattern *in Löwy's own terms* (App A's per-service testing is "black-box
  against the test plan"), not a tier we keep.
- **R2 — The load-bearing tier is the only routine tier.** The System Test
  Harness + Regression Harness drive the system's **Client-layer surfaces** over
  the wire, organized **by core use case**. Unit tests are "borderline useless"
  (ch02) → write essentially none. A sub-surface invariant (Engine determinism,
  ResourceAccess idempotency) is verified by **promoting it to an observable at
  the Client surface** and asserting it black-box — the promotion doubles as
  real observability. Only a genuinely un-surfaceable invariant gets one
  contract-level check, and **it never gates alone**.
- **R3 — The harness is a separate module, sibling to the server, wire-only.**
  For Go systems: its own `go.mod`, placed **outside the server package tree** so
  Go's `internal/` rule **compiler-seals** the server's internals against import
  (path-based, enforced across module boundaries — a `go build` error, not a
  lint). It talks to the system over the wire (boots the built binary as a
  subprocess, or hits the running stack) and **links zero server code**. A
  depguard allowlist caps harness imports to **stdlib + protocol SDKs (HTTP, the
  MCP SDK) + the test lib + generated public contract types**. Placement and the
  allowlist are CI-asserted (R6).
- **R4 — Multi-surface systems get a cross-surface equivalence test.** When two
  Client surfaces expose the same operations over different transports (e.g. an
  HTTP API and an MCP server mirroring it method-for-method), a
  **transport-agnostic step layer** drives each core use case through *both* and
  asserts identical committed state. This is what keeps "mirrors method-for-
  method" honest as both surfaces evolve.
- **R5 — Author separation is binding.** The agent that authors the load-bearing
  harness (`test-engineer`) is **blind to the implementation** — it sees only the
  core use cases + the Client contracts (generated OpenAPI/MCP specs), never the
  component source. Implementing developer agents do **not** write the
  load-bearing tests; `software-tester` runs them. **This separation IS the
  anti-cheat guarantee** — black-box surface alone is not enough without it.
- **R6 — Enforcement is mechanical.** The arch checker / CI asserts: (a) no
  harness module path under the server tree; (b) the harness depguard allowlist
  holds; (c) `…/internal/…` is never imported cross-tree (compiler-true, asserted
  for clarity). **Architecture-fitness tests (the layering arch checker) are not
  "tests" in this constitution's sense and stay** — they verify structure, not
  behaviour, and cannot be gamed by importing internals (that is the thing they
  forbid).

## Anti-patterns

- **Unit tests as system verification** — they verify units, not interactions.
- **TDD / test-first ceremony** — Löwy is test-*plan*-first, not test-first.
- **BDD/Gherkin** — not in The Method; removed from aiarch.
- **Collapsing the three roles** — test-engineer ≠ tester ≠ QA.
- **Owning regression in the test-engineer** — regression harness is
  developer-owned.
- **Deferring the test plan/harness** — they are early high-float enablers.
- **Skipping daily build + smoke** — a constant, defect-free codebase
  "accelerates the schedule like nothing else."
- **White-box / in-package internals-reaching tests** (§7 R1) — not Löwy (App A
  is black-box-against-the-test-plan), and the primary agent cheating surface.
- **One agent authoring a component *and* its load-bearing tests** (§7 R5) — the
  harness author must be blind to the implementation.
- **Harness module inside the server tree** (§7 R3) — breaks Go's `internal/`
  seal and re-legalizes importing internals.
