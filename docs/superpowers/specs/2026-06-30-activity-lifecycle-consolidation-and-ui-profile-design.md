# Activity Lifecycle Consolidation + UI Activity Profile

- **Date:** 2026-06-30
- **Status:** Design — pending review
- **Scope:** Consolidate the per-type lifecycle proliferation to one canonical App-A lifecycle; ship the UI activity profile first; define testing profiles behind it.

---

## 1. Problem

Construction today is hardwired to a single *service* lifecycle, while the model layer carries **nine distinct per-type "lifecycles"** (a "v3 design" effort): `service`, `frontend`, `deployment`, `documentation`, plus five testing variants (`plan`/`harness`/`perf`/`systemTest`/`qaProcess`), each with its own bespoke phase-id namespace and weights (`phaseSetFor` / `phaseSetForTestingVariant` in `server/internal/resourceaccess/projectstate/activityconstructionstatus.go`).

Two concrete symptoms:

1. **The pump only walks `service`.** `server/internal/manager/construction/workflow.go:445` iterates a hardwired `servicePhases`; UI and testing activities never dispatch regardless of their `ActivityType`.
2. **Three non-aligned "activity type" taxonomies** exist (planning `Coding bool`; persisted `ActivityType`+`TestingVariant`; Manager/review `ActivityKind`/`reviewKind`), and the bespoke phase-id namespaces broke the shared earned-value/progress formula that App-A's tracking depends on.

The founder's question — *"should we have a special lifecycle per type, or consolidate?"* — was answered against the book (see §2).

## 2. Decision driver: what Righting Software actually prescribes

Appendix A (Project Tracking) defines **one generic activity life cycle** that every activity follows, not a lifecycle per component type.

- *"You can break each activity in the project — be it a service or a noncoding activity — into its own little life cycle."* (appa.xhtml)
- *"Regardless of the specific life-cycle flow, most activities will have internal phases such as Requirements, Detailed Design, or Construction."*
- **Table A-1** — the one canonical phase set + weights: **Requirements 15 · Detailed Design 20 · Test Plan 10 · Construction 40 · Integration 15.**
- UI is used as a **weighting** example, never a separate lifecycle: *"you may decide that the Requirements phase will be weighted 40% for the UI activity and only 10% for the Logging activity."*

Activities differ along **three axes only** — none of which is a bespoke phase set:

1. **coding vs noncoding** (coding sub-split *structural* / *nonstructural*) — ch13 "three categories"
2. **one owning role** — a *column* in Tables 13-1/13-4 (Developer, Architect, Test Engineer, Quality Control/Tester, QA, UX/UI Expert, DevOps…)
3. **per-activity phase weights** (and trivial activities may omit phases)

Löwy names the proliferation anti-pattern directly: *"Do not design a clock… Think of project design as a sundial, rather than a clock."* (ch14). **Nine hand-tuned lifecycles is designing a clock.**

**Validation walkthroughs** (done during design) confirmed the single lifecycle absorbs the hard cases:
- **System Test Plan** — the v3 bespoke set `use_case_trace 20 → plan_authoring 45 → plan_review 35` is *identical* to the generic `Requirements → Construction → Integration` with two phases dropped and three renamed. The rename was the only thing it added — and the rename is what broke the shared progress formula.
- **UI** — maps to the generic lifecycle weighted toward design/construction; the UI-ness lives in the artifact + review experience, not the phases.

## 3. Target model

**One canonical lifecycle.** Phases: `Requirements → Detailed Design → Test Plan → Construction → Integration` (canonical ids retained for the shared progress formula).

**A profile** is a preset over that one lifecycle — *not* a new lifecycle:

```
profile = (
  rolePerPhase,     // which agent persona executes each phase
  phaseSubset,      // which of the 5 canonical phases are present
  weights,          // per-phase weights (sum 100)
  labels,           // per-phase DISPLAY labels (domain language; ids stay canonical)
  artifactKind,     // what the phase produces → drives rendering/preview
  reviewExperience  // reviewer set + lens + the surface the human reviews on
)
```

**Two layers vary independently** (this is the core insight):

| Layer | Varies per activity? | Faithful basis |
|---|---|---|
| **Lifecycle** (phase progression + weights) | No — one shared lifecycle | Table A-1 |
| **Deliverable + review experience** (artifact kind + how the human reviews it) | Yes — richly | owning role + the activity's deliverable |

Consolidating the *lifecycle* does **not** flatten the *experience*. A service's Detailed Design emits a contract diagram; a UI activity's Construction emits a runnable preview; a load test emits load projections — different `artifactKind` + `reviewExperience`, same lifecycle underneath. This is the faithful place to be bespoke.

**Canonical phase ids are kept; labels are per-profile.** The engine keeps `requirements/detailed_design/test_plan/construction/integration` so the cross-activity progress formula in `appa.xhtml` still works ("progress across all activities… life cycles, and phases"); the human sees domain labels (e.g. a test plan activity shows "Use-Case Trace / Plan Authoring / Plan Review").

This consolidation also dissolves the three-taxonomy problem: `ActivityType`/`TestingVariant`/`ActivityKind`/`reviewKind` collapse into *(classification, owningRole, profile)*.

### 3.1 Precedent: design and construction are PHASES of one activity, not two activities

Verified in the pump: it dispatches **one child workflow per construction *activity*** (`constructActivityWorkflowID`, single `constructActivityInput` with one `ActivityID`+`ComponentID` — `workflow.go:264-273`), and that one activity walks all five phases (`workflow.go:445,518`). So **defining the service contract (`detailed_design`) and implementing it (`construction`) are two phases of one activity**, dispatched to *different agent roles* (senior-developer on `detailed_design`, junior-developer on `construction`). This is the precedent the UI profile follows (§4).

## 4. UI activity profile (ship first)

### 4.1 One activity, design + construction as phases

The UI activity mirrors the service activity exactly — one activity, per-phase role dispatch:

```
ux_requirements → design → [human approval] → test_plan → construction → integration
  UX/UI           UX/UI                         author     developer      run flows e2e
                  real MUI on mock data,        component  wire real      on real data +
                  GH Pages preview,             flows      contracts,     system integration
                  human approves                          harden
```

The book's "UX/UI designs, Developer builds" role split is honored the same way service honors "senior designs the contract, junior builds" — **per-phase agent dispatch inside one activity**, not two activities.

### 4.2 The UI spec IS the real app (code-as-design)

Instead of Figma-mockup-then-rebuild, the design phase produces **real MUI components running on mock data** — the app the human plays with in review is legit, not a throwaway. Consequences:

- **Design output is real code that construction continues.** Construction = swap mock data for real service-contract wiring + harden edge/error/loading states + integrate into the app shell. Components are *not* rebuilt.
- **No declarative UI-contract schema.** Services are declarative-first (schema → codegen); **UI is code-first** (code → derived handle manifest). The only declarative artifact is the **published handle manifest** — `webApp/src/constants/UIIdentifiers.ts` (already exists, already mirrored blackbox in `uitests/tests/support/testids.ts`) plus a views list.
- **Mock data = MSW** baked into a `VITE_MOCK=1` build. Fixtures conform to the consumed service contracts so design↔backend can't drift. This keeps the design phase **high-float** (deployable with no backend). **One shared fixtures module** must back both the app-level service worker and any story-level MSW handlers — otherwise fixtures get authored twice and drift re-enters through the provider seam (§4.5). `PreviewAccess`'s implicit invariant is "same mock fixtures, any transport."

### 4.3 Flows = the activity's own `Test Plan` phase

There are two testing *scopes*:

| Scope | Tests | Lives in |
|---|---|---|
| **Component flows** | does *this* UI component work — its own interactions, on mock data | the activity's **`test_plan` phase** (in-activity) |
| **System journeys** | end-to-end across components, integrated system | a **separate System Test activity** (N-STP/N-IT) |

The component's Playwright flows (over its published handles) are its **`Test Plan` phase** — exactly like a service writes its own unit/integration test plan in its Test Plan phase. They run twice, App-A style: against the **mock-data build** during construction (component blackbox) and against the **real-data build** at integration. Because it's one activity, the handles are internal — no cross-activity contract boundary for component flows. Only the System Test activity (consuming multiple components' handles) has that boundary.

**Scope gating:** full-journey flows require a `Scope=fullApp` preview (the `VITE_MOCK` dev server or the GH Pages build), not a `Scope=componentStory` provider. The flows runner reads `handle.Scope` (§4.5) and skips/marks full-journey flows when the served preview is story-scoped — so a Storybook-only preview never silently under-reports coverage.

### 4.4 Review surface: a preview handle + human approval

- **Preview = a handle, not a provider.** The pump publishes the `VITE_MOCK=1` build through the `PreviewAccess` abstraction (§4.5) and stores the returned typed **`PreviewHandle`** in head-state. Both review consumers dereference the handle identically — neither branches on which provider served it.
- **Review = a link.** The archistrator review panel **links** to `handle.Origin`; the human plays with the legit app and **approves/comments**. No iframe embed, no walkthrough video required for the design review. (The panel reads `handle.AccessModel`/`handle.TLS` to warn appropriately — e.g. a public-obscure GH Pages URL vs. a localhost-only origin.)
- **Two-stage gate:** `uiDesigner` (Claude vision) pre-screens against HIG/Material + the published handles (`kindUIDesign`, `MayAmend: true`), then the **human holds the blocking approval**. A bounce re-runs the preceding phase via the pump's existing variance loop.
- Playwright does not disappear — it is the **runner** for the `test_plan` flows; it consumes the *same* handle (`UITESTS_BASE_URL = handle.Origin`) and gates its full-journey flows on `handle.Scope` (§4.3). Video/trace return to being failure artifacts (already captured by `uitests/`).

### 4.5 `PreviewAccess` — the preview-host volatility abstraction

*(System-architect review, 2026-06-30.)* "How/where the mock-data UI is served for review" is a **volatility**, not a fixed choice of GitHub Pages. The stable interface is *a reviewable preview identified by a URL/handle, backed by mock data, bound to a branch/activity*; the hosting substrate varies both over time (GH Pages → Cloudflare → …) and across concurrent consumers (a CI run wants a persistent remote origin; a developer/agent iterating locally wants a localhost origin). So it is encapsulated behind a ResourceAccess.

- **`PreviewAccess` (ResourceAccess).** Ops: `PublishPreview(branchSlug, activityId, mockBuildArtifact) → PreviewHandle` · `AwaitReady(handle) → PreviewHandle` · `ResolvePreview(handle) → PreviewHandle` · `TeardownPreview(handle)`.
- **`PreviewHandle`** (the whole payload both consumers need to self-configure): `Origin` (base URL) · `BasePath` (`/` localhost, `/archistrator/<branch>/` GH Pages) · `ProviderKind` · `Scope` (`fullApp | componentStory`) · `Liveness` (`persistentRemote | processScoped` + TTL) · `AccessModel` (`publicObscure | localhostOnly | authenticated`) · `TLS` · `Binding` (branchSlug + activityId).
- **Providers = Resources behind the contract:** `GhPagesPerBranchProvider` (persistent/public/TLS/fullApp; owns the `github.io` URL shape, the SPA 404→index fallback, and merge/delete teardown), and `LocalDevServerProvider` (the `VITE_MOCK` dev server over localhost — ephemeral/localhost-only/no-TLS/fullApp; `AwaitReady` = socket bind; teardown = kill process; History-API routing native so no 404 shim). **Cloudflare Pages is simply a third provider**, not a redesign.
- **Layer placement:** `PreviewAccess` is the volatility-encapsulating **ResourceAccess**; the hosting substrates (GH Pages, local dev server, Cloudflare) are its swappable **Resources**. The construction **Manager** (the pump) calls `PublishPreview` during the design/construction phase and stores the handle; the review gate and flows runner read it *down* through `PreviewAccess`. Closed layering preserved.
- **Scope is first-class because Storybook ≠ full-app.** A component-`Story` provider cannot serve the router-dependent, multi-step journeys the `test_plan` flows need. So the **primary local provider is `LocalDevServerProvider`** (capability-equivalent to GH Pages — same built app, different transport); **Storybook is a narrower third provider** whose handle advertises `Scope=componentStory`, and the flows runner **gates on `handle.Scope`** so it never over-claims full-journey coverage.

## 5. Reuse vs net-new

**Reuse (already built):**
- Playwright blackbox harness `uitests/` (chromium, video/screenshot/trace, `UITESTS_BASE_URL` hook to drive a deployed origin).
- `@xyflow/react` (React Flow) — renders flows/sequence diagrams (same pattern as `ContractCodeFlow.tsx`).
- `review.go` already routes `kindUIDesign → uiDesigner (mayAmend)` and `kindUICode → seniorReviewer`.
- `lifecycleTemplates.ts` FRONTEND/TESTING phase labels already name these hooks (fold into profile labels).
- `eslint-plugin-jsx-a11y` strict — roles/accessible-names already enforced.
- `UIIdentifiers.ts` published-handle registry + blackbox mirror.
- Replay-worker + cassette backend (`uitests.yml`) for real-ish-backend blackbox at integration.

**Net-new:**
1. **Lifecycle consolidation** — collapse the 9 phase-sets + `lifecycleTemplates.ts` per-kind tables → one canonical phase set + per-profile `(rolePerPhase, subset, weights, labels, artifactKind, reviewExperience)`. Pump reads the profile from the activity instead of hardwired `servicePhases`. Migrate persisted phase ids.
2. **MSW mock layer** in webApp + a `VITE_MOCK=1` build (one shared fixtures module — §4.2).
3. **`PreviewAccess` abstraction** (§4.5) + a `GhPagesPerBranchProvider` and a `LocalDevServerProvider`; the pump publishes and captures a **typed `PreviewHandle`** into head-state (not a bare URL string).
4. **Design-review panel** in webApp — links to `handle.Origin` + human approve/bounce gate (review.go routing already exists).
5. **UI profile** wired end-to-end (`ux_requirements → design → test_plan → construction → integration`, per-phase agent dispatch; flows generated/authored against published handles).
6. **Testing profiles** defined behind the UI work (presets over the one lifecycle; component flows in-activity, System Test as separate activities).

## 6. Delivery order

1. **Consolidation foundation** — one canonical phase set + profile mechanism; pump reads profile; migrate the v3 phase ids; keep the existing service path green (it already walks the canonical 5).
2. **UI profile end-to-end** — MSW build → `PreviewAccess.PublishPreview` (ship the `GhPagesPerBranchProvider` first as *one provider*, not the architecture) → design-review panel (link + approve) → flows as Test Plan phase → construction wiring.
3. **Testing profiles** — System Test Plan / System Test as profiles + separate activities (the model already exists; this is dispatch + review wiring).

## 7. Out of scope / deferred

**The deliverable of this effort is the profile *mechanism* + the UI profile as its proof, NOT every activity type's artifact/review experience.** Defining all per-type experiences up front would re-introduce the "clock" (§2). Instead, each remaining activity type is a **small follow-up spec** that plugs a new `(artifactKind, reviewExperience)` into the mechanism this effort builds — no re-architecture. UI is sequenced first precisely because it is the *hardest* review experience (novel runnable preview + human-in-the-loop gate); once the extension point carries UI, the rest are incremental.

Follow-up profiles (each its own spec, reusing the mechanism):

| Profile | Likely `artifactKind` | Likely `reviewExperience` |
|---|---|---|
| `deployment` | provisioning diff / convergence report | DevOps + architect sign-off |
| `documentation` | doc | doc review (tech-writer + architect) |
| **N-PERF** | load projections / load-sequencing rig | perf review of projections |
| **N-QA** | gate definitions / process audit | QA process review |
| richer testing | API-call / UI-interaction **sequence diagram** | test-engineer review |

Also deferred:
- Live interactive **iframe** preview (needs hosted ephemeral origin) — the preview link covers review.
- **Cloudflare Pages** and **Storybook** previews — each is simply an *additional `PreviewAccess` provider* (§4.5), not a redesign. That "new provider, not re-plumbing" property is the payoff that justifies building the abstraction now rather than hardwiring GH Pages.

## 8. Open questions

- Migration of any **already-seeded `project.json`** activities carrying v3 phase ids — verify the canonical-id remap is lossless (per §2.1 the legacy integer `ActivityType` encoding already decodes; phase-id strings need a remap pass).
- Exact **profile storage** — whether profiles live as data in `project.json` alongside the activity or as code-side presets keyed by `(classification, role)`.
- **`PreviewHandle` head-state persistence** — head-state stores the *typed* `PreviewHandle` (§4.5), never a bare URL string; a string discards `ProviderKind/Scope/Liveness/AccessModel/TLS` and the preview-host volatility leaks back through the persistence layer. Confirm the head-state schema carries the typed record.
