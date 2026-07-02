# Per-Activity-Type Artifact Renderers + Preview Seam

- **Date:** 2026-07-01
- **Status:** Design — pending review
- **Scope:** Give every activity type a first-class artifact/preview + review experience in the webApp, by generalizing the rendering seam into an `artifactKind → renderer` registry composed from a small palette of reusable primitives. Builds on [[2026-06-30-activity-lifecycle-consolidation-and-ui-profile-design]].
- **Goal (founder):** good previews + a lovely experience for *every* activity type, so construction can be driven from the app itself instead of a Claude Code session. This is one of the last things before that switchover.

---

## 1. Problem

The lifecycle consolidation shipped the *mechanism* (one canonical lifecycle + per-type `Profile` of phase/weight/label + per-phase human approval gate). But the **artifact/review experience** — the whole point for a human driving construction from the app — is built for exactly one type:

- **Service / contract-bearing** activities get the rich `ServiceContractView` (C4 + react-flow, 4 tabs). Fully wired.
- **UI** has a *designed-but-unbuilt* preview experience (`PreviewAccess`/`PreviewHandle` link — §4 of the prior spec). No code yet.
- **Everything else** — testing (plan/harness/perf/system-test/QA), deployment, documentation — falls through to the generic "honest pointer" `ArtifactCard` (`webApp/src/components/construction/ArtifactActivityDetail.tsx:149-194`): title + source + "open in corpus once the port lands." No preview, no review surface.

Two structural gaps behind this:

1. **No dispatch seam.** `ArtifactActivityDetail.tsx:233-240` hardcodes "contract resolved → `ServiceContractView`, else generic card." There is no `classification → renderer` registry.
2. **The profile carries no artifact/render hint.** Contrary to the prior spec's §3 tuple, the shipped `Profile` (`server/internal/resourceaccess/projectstate/activityprofile.go:18-20`) is *only* `[]ProfilePhase{Phase, Weight, Label}`. The `artifactKind`/`reviewExperience` half was never implemented. So there is no field to dispatch on today.

Additionally, `deployment` and `documentation` activities are **not reachable** — `DeriveType` (`corpusderive.go`) only ever emits `Service | Frontend | Testing` — so even a perfect renderer for them would never be selected.

## 2. Decision: dispatch on classification, not a new field

The artifact family is (near-)isomorphic to the activity's classification: one `ActivityType` (+ `TestingVariant`) → one artifact family living in a known head-state slot. So the dispatch key is the **classification the profile is already keyed on** (`ActivityType`, `TestingVariant`) — *not* a new `artifactKind` field on the profile. This avoids adding a redundant field and keeps `ProfileFor` the single keying function.

- The webApp already carries a coarse classification per row (`row.kind` / `KIND_META` in `KindBadge.tsx`). The plan surfaces the full `(ActivityType, TestingVariant)` classification + the relevant head-state slots to the renderer.
- Precedent for a registry already exists in the codebase: `webApp/src/components/project/ProjectArtifactRenderer.tsx` dispatches Phase-1/2 artifact slots to bespoke views (`SdpReviewView`, `VolatilityMap`, `ArchitectureFlow`, …). We mirror that shape for Phase-3 activity artifacts.

## 3. The seam: a renderer registry over a primitive palette

The insight that keeps this from becoming a "clock" (§2 of the prior spec): **you do not build 7 bespoke experiences — you build ~5 reusable primitives + a dispatch, and each type is a thin composition.** When every remaining artifact is mapped to its actual review surface, they collapse onto a small palette, most of which already exists:

| Primitive renderer | Reuses | Consumed by |
|---|---|---|
| `SequenceFlow` (react-flow) | `webApp/src/components/flow/DynamicViewFlow.tsx` | system-test journeys, richer testing, service dynamic views |
| `MetricsDashboard` (charts) | `dataviz` conventions | N-PERF load projections |
| `MarkdownDoc` | net-new (small) | docs, QA audit report, SRS/test-plan prose |
| `DiffView` (before/after) | net-new (small) | deployment provisioning diff |
| `LinkPanel` (handle → origin) | the `PreviewHandle` design (prior §4.4) | **UI preview, published doc site, deployed env** |
| `TraceTable` / `GateTable` | `PhaseGatePanel.tsx` / `PolicyPanel.tsx` gate rendering | N-STP use-case trace, N-QA gates |

**Registry shape (webApp):**

```
artifactRenderers: Record<Classification, TypeArtifactRenderer>
  service      → ServiceContractView            (exists)
  frontend     → UiPreviewPanel (LinkPanel)     (prior spec — built with UI profile)
  testing:plan → TestPlanView   (TraceTable + MarkdownDoc)
  testing:perf → PerfView       (MetricsDashboard)
  testing:systemTest → SystemTestView (TestRun table + DefectList + SequenceFlow)
  testing:harness → HarnessView (LinkPanel)
  testing:qaProcess → QaView    (GateTable + MarkdownDoc)
  deployment   → DeployView     (DiffView + LinkPanel)
  documentation→ DocView        (MarkdownDoc + LinkPanel)
```

Each `TypeArtifactRenderer` receives the activity, its full classification, and the relevant head-state slot(s), lays out that type's phase artifacts using the primitives, and renders **alongside the existing `PhaseGatePanel`** (the approve/send-back gate is already generic — no new gate mechanism). `ArtifactActivityDetail.tsx` becomes: header + lifecycle strip + `artifactRenderers[classification]` (fallback: the current honest-pointer card).

## 4. Per-type artifact/review experience

| Type | Head-state slot | Renderer composition | Hosted origin? |
|---|---|---|---|
| Testing · Plan (N-STP) | `TestingState.SystemTestPlan` | `TraceTable` (UC → plan entry) + `MarkdownDoc` (entries) + approved chip | no |
| Testing · Harness (N-STH) | `TestingState.HarnessModule` | `LinkPanel` (repoRef/PR) + approved chip | no |
| Testing · Perf (N-PERF) | `TestingState.PerfHarness` | `MetricsDashboard` (load projections) + `LinkPanel` (rig) | no |
| Testing · System Test (N-IT) | `TestingState.TestRuns` + `.Defects` | run table (pass/fail) + `DefectList` (severity) + `SequenceFlow` (journeys) | no |
| Testing · QA (N-QA) | `TestingState.QualityGates` + `.QualityAuditReport` | `GateTable` + `MarkdownDoc` (audit) | no |
| Deployment (R-*) | `PhaseArtifacts.ProvisioningSpec` + `.DeployNote` | `DiffView` (provisioning diff) + convergence report + `LinkPanel` (env) | maybe |
| Documentation (N-ADR…) | `PhaseArtifacts.DocOutline` + `.DocNote` | `MarkdownDoc` (rendered) + `LinkPanel` (doc site) | maybe |
| Service (C-*) | `ServiceContracts[c]` | `ServiceContractView` (unchanged) | no |
| Frontend (U-*) | `PhaseArtifacts.UIDesign` + `PreviewHandle` | `UiPreviewPanel` (`LinkPanel`) | yes |

**`LinkPanel` = the generalized `PreviewHandle`.** "A reviewable artifact at a URL backed by some substrate" is one volatility whether it is a GH-Pages UI build, a deployed staging env, or a published doc site. Decision: **generalize `PreviewAccess`** so the deployed-env and doc-site cases are additional providers/handles, not new plumbing. `LinkPanel` renders any `PreviewHandle` (origin + accessModel/TLS warnings), and UI/deploy/doc all feed it.

## 5. Prerequisites & data plumbing

1. **`DeriveType` reachability** — extend so deployment/documentation are selectable (e.g. `R-*` → Deployment; disambiguate `N-ADR`/doc ids from `N-` testing → Documentation). Without this their renderers can never be reached. **In scope** (small, unblocks two renderers).
2. **Head-state exposure** — `PhaseArtifacts` and `TestingState` must reach the webApp (API response + TS types in `webApp/src/api/types.ts`). `ServiceContracts` already flows; these follow the same path.
3. **Fixture state** — the seeded `project.json` has `phaseArtifacts`/`testingState` null. To *see* the renderers on real state we seed representative records for at least the proof type (below).

## 6. Sequencing: seam + one proof, then plug-ins

Mirroring how UI was the hardest proof for the mechanism, we prove the *renderer seam* with one type end-to-end, then the rest are incremental plug-ins:

1. **Seam** — the `artifactRenderers` registry + the fallback + the primitive palette scaffolding (`SequenceFlow`, `MetricsDashboard`, `MarkdownDoc`, `DiffView`, `LinkPanel`, `TraceTable`/`GateTable`).
2. **Proof: Testing · System Test (N-IT)** — highest primitive reuse (`SequenceFlow` from existing react-flow) and `TestingState` is already fully modeled. Build `SystemTestView` end-to-end; seed fixture runs/defects; drive it.
3. **Plug-ins, one per review checkpoint** — Perf, QA, Plan/Harness, Deployment, Documentation. Frontend/UI stays on its own prior-spec track (`PreviewAccess`), consuming the shared `LinkPanel`.

## 7. Working cadence (founder instruction)

Per [[archistrator-ui-work-review-loop]]: the app runs locally against **archistrator's own project state** ([[archistrator-run-app-locally]]). For **each new renderer / UI change**: build → **Playwright-drive the page** → open it → **STOP and ask the founder to review** → only then proceed to the next type. Each `TypeArtifactRenderer` is its own review checkpoint.

## 8. Non-goals / deferred

- Live interactive iframe embeds (the `LinkPanel` link covers review).
- Editing artifacts from the webApp — these are read/review surfaces; production stays in the construction pump.
- Perfecting every type's fixture data — only enough to exercise each renderer.
- Cloudflare/Storybook preview providers — additional `PreviewAccess` providers later, not this effort.

## 9. Open questions

1. **Classification transport** — pass the full `(ActivityType, TestingVariant)` to the webApp as an explicit field, or keep deriving from the activity id prefix on the frontend (mirror `DeriveType`)? Leaning explicit field to avoid a second source of truth.
2. **Per-phase vs per-activity rendering** — render one composite per activity (chosen here, matches current `ArtifactActivityDetail`), or a sub-view per phase? Composite first; revisit if a type needs phase-level drill-down.
3. **`PreviewHandle` scope for non-UI** — does a deployed env / doc site genuinely fit the `PreviewHandle` fields (`Scope`, `Liveness`, `AccessModel`), or does `LinkPanel` need a lighter handle type for the link-only cases?
