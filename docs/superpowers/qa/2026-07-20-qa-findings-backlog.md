# QA findings backlog — 2026-07-20 manual pass

Accumulated per-screen findings. NO changes are made during the review sweep; after all
system-design screens are reviewed we reconcile overarching themes with the founder and
then execute. Type 1 = archistrator's own design content (via app comment→amend).
Type 2 = archistrator app code/UX (subagent edits).

## System Design · Mission (step 1) — reviewed, founder input received

### Type 1 — mission/objectives content (founder direction captured 2026-07-20)

- [P1] Mission statement rewrite (APPROVED DIRECTION, execute later): drop the (a)–(d)
  functional decomposition and all "components" vocabulary (violates the binding
  business-alignment skill ruling). Keep the genuine how ("orchestrate, not author code")
  in business/user language.
- [P1] Objective 2 rewrite (FOUNDER DIRECTION): "method conformance" is not itself an
  objective. The objective: provide a software design system that reliably produces
  maintainable applications — strong guardrails where open-ended agentic coding (e.g.
  bare Claude Code) has none; heavy codegen + architecture-validation tooling minimizes
  the room agents have to screw up; "the best and most efficient way to build complex
  apps with agentic coding." NEVER use the terms "The Method" or "rightingsoftware"
  anywhere in artifact text.
- [P1] Objective 3 rewrite (FOUNDER DIRECTION): current "revenue only from operation"
  pins the business in and founder wants ~the opposite: initial focus is operations
  revenue, but leave the door open to charging for tokens (design/construction), or
  consulting / building apps for people. Reframe as revenue-model flexibility with
  operations-first focus.
- [NEW objective] (FOUNDER DIRECTION): streamlined, opinionated SDLC with configurable
  review policies — some customers run "vibes" (fully automatic), serious customers
  require human approval at chosen stages; the SDLC workflow is designed by archistrator,
  review/approval requirements are configured per customer.
- [P3] Objectives 2/4 solution-vocabulary leaks — subsumed by the rewrites above.
- [P3] Security/trust objective missing (money movement, repo write access, operating
  customer systems) — founder to ratify whether to add or fold into audit objective.
- [EARMARK] Objective 3 vs platform-funded venue mode C: if mode C ratifies, the
  usage-fee-vs-pass-through distinction must be reflected (likely subsumed by the
  objective-3 rewrite).
- [DEFERRED→Architecture step] Committed systemDesign still lists HandOffEngine despite
  the ratified cut — stale roster.

### Type 2 — app code (queued, not yet approved for execution)

- [P0] SystemDesignView.tsx:592 — Amend affordance missing on freshly-committed Phase-1
  artifacts; backport F-GTD-11 guard `(sessionMissing || stage === 'committed')` from
  ProjectDesignExperience.tsx:717 (verified in code; live-repro'd).
- [P1] No PM-ratification signal on committed artifacts (skill makes ratification a
  required gate; nothing shows the gate closed).
- [P2] Role chips ("architect"/"pm") explanation is mouse-only Tooltip on non-interactive
  chips — invisible to keyboard/SR users (SystemDesignView.tsx:326-343).
- [P2] Vision→Objectives→Mission traceability chain not visualized.
- [P2] Hand-rolled `div role="button"` in SlimSpine.tsx:63-98 + ExperienceChrome.tsx:88-129;
  stepper is 8 separate tab stops, no arrow-key roving; consolidate onto ButtonBase.
- [P3] Query dedupe: get-project ×3, userinfo ×2 per page load.
- [P3] Provenance line shows storage path, not research provenance / link to inputs.
- [P3] CommentableList roving tab-stop defaults to Objective 2 (expected first item).

### Bonus (home screen, drive-by — park for later)

- Review-policy radio group renders with NO option selected on project home.
- Economics strip says "awaits Phase 2" while Phase 2 is DONE.

## System Design · Glossary (step 2) — reviewed, awaiting founder

### Type 1 — glossary content (likely ONE amendment covering all)

- [P1] "The Method" vocabulary in 5 terms: term names "Method Artifact" + "Method Step
  Authoring", plus defs of Architect User / Project State / Project Repository. Coin the
  platform's own SDLC vocabulary here (e.g. Design Artifact / Artifact Authoring,
  "the platform's guided delivery process"). "rightingsoftware" does not appear.
- [P1] Revenue model hard-coded: Customer "construction… at no charge from aiarch",
  Usage "metered as the sole basis of the customer's bill", End User "never pay aiarch".
  Strip/soften pricing clauses per the founder's revenue-flexibility ruling (same root
  tension as Mission objective 3 — reconcile in one pass).
- [P2] "aiarch" ×7 across 5 defs (Customer, End User, Operated System, Service Invoice,
  Payment Gateway); 0 mentions of "archistrator". Replace with "the platform" or define
  the short name — the glossary currently embodies the exact two-names ambiguity it
  exists to kill.
- [P2] Missing customer-facing terms: Review Policy (+ Review Gate, Amendment) — real
  typed concepts (ReviewPreset vibes/checkpoints/full) absent from the vocabulary; and
  Architect User's "approving every gated decision" is false under the vibes preset —
  reword to "decisions their review policy gates to a human".
- [P2] "Source Control System" def excludes the ratified GitLocal venue ("code-hosting…
  runs on the customer's account and credentials"). Make venue-neutral; consider a
  "Construction Venue" term.
- [P3] Payment Gateway vs MerchantGateway(Access) naming drift with the architecture.
- [P3] Worker's "or a human contractor" is speculative (no such path exists).
- [P3] "Method Artifact" def near-circular + "typed" implementation leak.
- [P3] Trim candidates: Idle-Pause ⊂ Autoscaling; Usage vs Usage Assumption.

### FOUNDER RULINGS on Glossary content (2026-07-20, fold into the amendment)

- Work Item Tracker (and its WHAT twin "Work Item"): the customer's EXTERNAL project-
  management system should NOT be a concern of our app — drop the tracker-sync concern.
  Instead the OBJECTIVES gain extensibility: other agents (via MCP) or apps (via API)
  can sync with the system. NOTE: ripples into architecture (any WorkItem component/
  use case) — reconcile when we reach the Architecture step.
- Idle-Pause → rename to "Scale to Zero" (industry-standard term).
- Usage Assumption → rename to "Expected Usage" (or similar natural phrasing).
- Worker → "Agent" is the more standard term (not "coding agent" — agents also review/
  design). Rename term + audit definitions using "Worker(s)".

### FOUNDER additions to Business Objectives (Mission amendment)

- Accessibility-to-non-coders objective: make it easy for ANYONE to run and host an
  application — no coding skill needed; "if you can think in systems you should be good."
- We-handle-the-hard-parts objective: the platform absorbs the genuinely hard
  operational work — operations, scaling, load projections, etc.
- Extensibility objective (reframe of current obj 8): agents (via MCP) and apps (via
  API) can sync/drive the system programmatically with the same capabilities as humans.

### Type 2 — app code

- [P2] Chip counts stale under active text filter — root cause verified:
  glossaryLogic.ts:74-83 computed from unfiltered entries, GlossaryView.tsx:67 memo
  deps `[entries]` only. Derive chip counts from a text-only filter pass.
- [P2] Filter box: no clear affordance — no endAdornment "×", no Escape handler
  (GlossaryView.tsx:98-120). Add both (standard MUI search pattern).
- [CONFIRMED CROSS-STEP] The Mission P0 (missing Amend on committed artifacts,
  SystemDesignView.tsx:592) affects ALL 8 system-design steps — fix once upstream.
  Note: CommittedArtifactPanel's fill logic is currently dead code because of it;
  re-verify fill after the fix.
- [P3] No term provenance / synonym / "not to be confused with" surface (disambiguation
  tooling nice-to-have).
- [CORRECTION] Category chips are real MUI Chips via ButtonBase (NOT hand-rolled) —
  only SlimSpine stepper + ExperienceChrome close button are hand-rolled DIVs.

### Passes worth keeping (consistency bar for later screens)

- Per-section CommentableList composite-widget keyboard model (Tab between sections,
  arrows within) — ARIA-APG-correct and consistent with Mission.
- aria-live match announcements keyed by message text (no per-keystroke spam); clear
  0-match empty state; O(n) memoized filter (fine at 200 terms); zero model→view drift;
  clean layering with unit-tested logic extraction.
- Four-Questions categorization judged book-faithful by PM (chips ARE the four
  questions); definitions overwhelmingly crisp business language.

## System Design · Scrubbed Requirements (step 3) — reviewed, awaiting founder

### MAJOR THEME (founder-raised): should this be a standalone phase at all?

The book has NO scrubbed-requirements artifact. Scrubbing is a ch02 analysis technique
inside volatility discovery ("scrubbing away the solutions" → Volatilities List); ch05
TradeMe goes use-cases → business alignment → volatility identification with no R-list.
DECIDED 2026-07-20 (delegated by founder to product-manager agent): **Option 2 —
reframe as "Required Behaviors"**. Rationale: the book has no such artifact because a
trained human architect scrubs in their head; we automated the architect, so the
implicit must become an explicit, reviewable artifact — the non-engineer customer's
"did the machine understand what my system must do?" checkpoint, and the last fully
readable screen before doctrine turns technical. Prescribed shape:
- Rename step/slot label "Scrubbed Requirements" → "Required Behaviors" (internal slot
  kind may keep identity to limit ripple).
- Typed item: `id` (stable), `behavior` (imperative business language), `statedAs`
  (raw stated ask(s) from research this was scrubbed/consolidated from; nullable),
  `volatilityHint` (candidate volatility — the step-4 hand-off the skill demands).
- NO numeric ID gaps: renumber survivors contiguously; absorbed asks recorded in the
  survivor's `statedAs` (provenance lives on the survivor).
- Step 4 consumes the volatilityHint column as its candidate seed list; statedAs
  enables trace-back to the customer's original words.
Execute in reconciliation phase (schema + rename + renumber + view + skills/prompts).

### Type 1 — content

- [P1] Vocabulary sweep (same class as Mission/Glossary): R-002 "Method artifact",
  R-006 "Method role"; R-005/R-006 workers→Agent; R-023 idle-pause→scale to zero;
  R-025 usage assumptions→expected usage. Decide whether R-004's option-name enumeration
  (normal/decompressed/compressed/subcritical) counts as book vocabulary.
- [P1] R-013 revenue hard-coding — "no charge for construction or automated-assistant
  work… never invoices" is the strongest hard-coding anywhere; R-015 "compute the
  customer owns" forecloses platform-funded venue. ONE reconciling amendment across
  mission obj 3 + glossary Customer/Usage + R-013/R-015.
- [P2] Missing requirement: configurable review policy — reverse-orphan (Review Policy
  volatility + shipped ReviewPreset exist; no requirement traces to them).
- [P2] R-014 "ALL state… one customer-owned store" contradicted by built system
  (operated runtime/billing/usage/Temporal state live in platform stores). Scope to
  project artifacts + constructed outputs; "one store" is solution-flavored.
- [P3] R-007 tighten to programmatic parity (agents/apps same capabilities as humans).
- [P3] R-002 "automated assistant" mild solution residue; R-001 untraced (vision
  restated — 17/18 R-IDs appear in architecture rationale, R-001 nowhere).
- [RIPPLE] WIT-removal ruling: requirements are already clean (nothing mandates the
  tracker), but slot 3 "Work Tracking" volatility + slot 5 WorkItemAccess/WorkItemTracker
  + glossary Work Item terms need one coordinated amendment.

### ID-gap mystery SOLVED (both reviewers, independently, via git)

Commit bfb4da0 "re-scrub requirements to behavior-only (22→18)" deliberately retired
R-010 (billing duplicate + named tech), R-012 (solution), R-017 (MCP decomposition =
design decision), R-018 (monorepo/tech-stack constraint); R-019–R-022 were scrubbed
before the seed commit (unrecoverable). Stable-ID retention is correct for traceability;
the failing is display-side only.

### Type 2 — app code

- [P1] Typed model {id, statement} drops the volatility-hint column the requirements-
  analysis skill calls "critical — the input to volatility-identification", and the
  original-text column. Artifact-shape change + hand-off defect to step 4.
  (Interacts with the MAJOR THEME above — resolve together.)
- [P2] Double "COMMITTED" indicator when the Amend strip renders (header StageChip at
  SystemDesignView.tsx:285 + panel strip CommittedArtifactPanel.tsx:155-192) — resolve
  BEFORE the Amend-guard fix propagates the strip to all 8 steps.
- [P2] ID gaps unexplained on screen — extend the ArtifactInfoButton popover pattern
  (ArtifactIntro.tsx:29-48) + consider a one-line provenance note; full per-ID rationale
  needs a model field (artifact-vs-UI decision).
- Pass/pattern: comment anchors keyed by stable R-0xx id (ScrubbedRequirementsView.tsx:
  36-41) — reorder-safe; back-propagate to Mission/Glossary anchoring. MUI Dialog a11y
  verified fine (auto aria-labelledby). CommentableList consistency holds.

## System Design · Volatilities (step 4) — reviewed, awaiting founder

Architect's verdict: weakest artifact pair so far — slots 3 (volatilities) and 5
(architecture) disagree about who encapsulates what.

### Type 1 — content

- [P1] One-volatility-one-component doctrine broken ×3, slots 3↔5 contradict:
  (a) Operational State Store — slot 3 names 3 fronting RAs, slot 5 has 4 claimants
  (RevenueLedgerAccess unacknowledged; = the 2026-07-17 "4 components claim 1
  volatility" defect); (b) Source Control Target — slot 3 says 3 RAs, SIX slot-5
  components claim it; (c) Customer App Infrastructure — rationale admits "no single
  owning component". Fix via overfold re-pass amendment: sub-volatility naming or
  explicit facet doctrine in operationalConcepts + a real owner for (c).
- [P1] Construction Hand-Off Policy maps to the CUT HandOffEngine and duplicates the
  review-policy direction; Review Policy sits on axis 1 with a "Phase-3 only" rationale
  contradicted by the built per-project ReviewPreset. ONE amendment: fold Hand-Off
  Policy into Review Policy, move to axis 2, rewrite to the vibes/checkpoints/full
  preset model, retire HandOffEngine from slot 5.
- [P1] "Work Tracking" volatility contradicts WIT-out ruling — full ripple confirmed:
  slot 3 item + WorkItemAccess (planned, never built) + WorkItemTracker resource +
  UC2/UC3 call chains traverse it + glossary terms. Coordinated removal amendment
  across 4 artifacts; re-route call chains. (Axis 2 → 10, total → 21.)
- [P2] Axis misassignments: Durable Execution Runtime + Operational State Store justify
  axis 2 by TEST-PROFILE variance (a test profile is not a customer → honest axis is 1);
  Payment Provider's axis-2 case is speculative (one provider today; book's
  resist-speculation heuristic).
- [P2] Empty traces + rejected: the typed contract ALREADY supports ModelVolatility.
  traces (schema.ts:1032) and ModelRejectedVolatility with book-faithful rejection
  classes (schema.ts:848-853) and the VIEW renders both — the committed artifact just
  doesn't populate them. Populating traces = the PM's volatilityHint back-edge; an
  identification pass with zero rejections recorded is doctrinally incomplete.
- [P2] Five "Phase Workflow" volatilities read as functional decomposition to a
  non-engineer (one-per-Manager, named after phases). Rename to foreground the CHANGE
  ("step choreography evolves as practice matures"), per PM.
- [P2] Vocabulary purge: "the Method" ×3, "worker" ×3, "idle-pause" ×1, "aiarch" ×5,
  named tech + stale as-of date in Service Pricing ("Claude Code token… (2026-06-14)").
- [P3] Review-policy coverage exists but is invisible: split across Review Policy +
  Construction Hand-Off Policy, neither referencing the shipped preset vocabulary.
  (Subsumed by the fold amendment above.)
- [P3] 22 items exceeds the skill's ~6–15 band; WIT removal + workflow consolidation
  moves toward it. Service Pricing itself is GOOD — already the open-ended pricing
  policy the founder wants (upstream artifacts hard-coded what it correctly absorbs).
- [P2] PM ratification gate: architect-only chip is book-correct for authoring, but
  hasPmCritic=false means NO PM pass at all on volatilities — PM recommends a
  ratifier-only signal (customer-gap check without co-authoring).

### Type 2 — app code

- [P1] "Encapsulated by" is a fragile prose-substring join (VolatilityMap.tsx:108-118,
  first component whose `encapsulates` prose contains the name) — displays a single
  FALSE owner for multi-claimant volatilities, silently omits orphans, breaks on rename.
  Fix: typed encapsulatedBy (component id) on ModelVolatility; removes the view's
  useProject fetch → also clears the legacy-allowlist layering debt
  (eslint.platform.config.js:77) for free.
- [P2] Retro-theme WCAG failure (MEASURED 3.63:1 < 4.5:1): axis-2 caption + detail-panel
  axis line use t.committedDot (#6E8A3F) as TEXT on paper (#FBF6EA)
  (VolatilityMap.tsx:382-391, 80-82/722-724). Other themes pass. Fix: darker text-safe
  variant (same remediation as bandYellow, themes.ts:84-88).
- [P2] Focus drop on detail-panel close: Clear-selection/Escape unmounts the focused
  button → focus falls to body (VolatilityMap.tsx:745-755). Fix: refocus the opening
  VolChip via the existing chipRefs machinery.
- [FOUNDER-FLAGGED] Axis diagram adds no information over the lanes (dots evenly spaced
  on the axes; zero-information chart; aria-hidden duplicate; its captions host the
  contrast bug; summary card ⅔ empty beside it). Options: (1) remove, promote lanes;
  (2) RECOMMENDED: demote diagram to the pre-selection empty state of the right card;
  (3) encode a real dimension (e.g. #behaviors insulated once traces lands).
  Founder decision pending.
- [P3] Select-then-comment divergence from steps 1-3 judged INTENTIONAL and justified
  (drill-down data shape; genuine ARIA listbox) — keep; add a low-priority keyboard
  shortcut parity pass (one-key comment from a selected chip).
- Passes: listbox keyboard model unit-tested (volatilityMapLogic.ts:23-44); aria-hidden
  SVG is the right call while the diagram remains a duplicate; view renders
  traces/rejected already (ahead of data); selected-state ring + lane mirror solid.

### FOUNDER QUESTIONS — ANSWERED by architect (2026-07-20)

- Channel vs Transport: MERGE (22→21). Channel = which KIND of caller (browser/agent-
  over-MCP/scheduler → one Client component each) — genuine volatility, keep. Transport
  = wire mechanics for a fixed caller kind — can never ripple past a single Client
  (each Client owns its wire stack), earns no system-level entry. Slot 5 mis-tags
  SchedulerClient as the Transport binding (it's a channel, and not "customer"
  interaction at all). Amendment: fold Transport rationale into Channel; re-tag
  SchedulerClient. Analogy for the artifact: doors into a building vs door hinges.
- Durable Execution Runtime: KEEP BOTH, FIX AXIS. The book does BOTH things (ch05:
  workflow-Manager pattern codified as an operational concept AND workflow storage
  still listed as Resource+RA). Built system already implements the split: slot 6
  committed decision "durable primitives are execution substrate, not architecture
  edges"; DurableExecutionAccess exposes only 4 cross-execution control-plane verbs,
  zero Temporal lexemes, Temporal confined to one file. The RA encapsulates "which
  substrate, when addressed from OUTSIDE an execution" — legitimate. Real defect =
  axis (test-profile variance is not customer variance): move to axis 1. Optional
  rename "Workflow Execution Substrate".

## System Design · Core Use Cases (step 5) — reviewed, awaiting founder

Overall: strongest artifact so far — 5 core within the 2–6 band, one per essence
activity; all 12 variations carry recorded rejection rationale (the ledger discipline
volatilities lacks); flows validated against the real built rails.

### Type 1 — content

- [P1] Bill-the-User flow still describes the PRE-charge-only settlement model
  ("compute net settlement… charge or PAY OUT THE NET") — the platform never pays out;
  slot 5's dynamic view for the SAME use case is already reconciled (no payout).
  Slots 4↔5 disagree on a core use case. Amend the slot-4 flow.
- [P1] WIT ripple fully mapped in flows: "Open a work item per sealed activity" (UC
  Commit), "Mark the work item in progress"/"close the work item" (UC Execute) + THREE
  slot-5 dynamic views traverse work-item-access. Same coordinated amendment.
- [P2] Review gates unconditional in flows (23 review steps; vibes/checkpoints/full
  invisible) — add a policy decision node where gates appear (UC1 + UC3).
- [P2] Step texts leak implementation vocabulary ("billing aggregate", "bind the
  gateway account", "job", "hand-off policy") — rewrite in business language.
- [P3] "assign a worker per hand-off policy" → Agent + fold hand-off into review policy.
- [P3] Ghost variation: "End-User Dispute or Chargeback" says "Removed 2026-06-09" in
  its own rejectionReason yet still renders with a flow — drop or reclassify.
- [P3] Verify intervention (mission obj 7) actually appears inside core flows.
- GOOD: no "Method" vocabulary in any of the ~170 step texts; core set verdict RIGHT;
  Drive System Design flow matches the real rail (draft→validity→critique→review→seal).

### Type 2 — app code

- [P2] Full-diagram readability: React Flow zoom Controls DO exist (UX corrected my
  observation — likely below screenshot crop); real fixes = collapse the 300px meta
  sidebar in Full-diagram mode + default fit/fit-to-width for dense flows.
- [P2/P3] slot-4 nodes' linkedCompId empty on all 226 nodes (PM: cases can't
  self-validate architecture; architect: slot-5 dynamic views carry component chains
  for all 17 — the linkage gap is real but validation lives in slot 5; the agreement
  defects above are the sharper issue). Populate linkedCompId (or wire steps↔slot-5
  views) so step↔component linkage is inspectable.
- [P3] Model fields never rendered: trigger (clientAction/busMessage/timer — determines
  entry Client + sync-vs-queued) and actors.
- [P3] Walkthrough step card: step-level comment exists only via drag-select (no
  visible button, undiscoverable) — add per-step comment button (parity w/ ProseSection).
- PASSES to reuse: keyboard parity BETTER than mouse (node aria-label + Enter/c);
  walkthrough refocuses stepTitleRef after Next/Back/Restart (the exact fix Volatilities
  detail-close needs); only 1 of 17 diagrams mounted at a time (no perf issue); real
  MUI Select with grouped options; PATH breadcrumb click-to-rewind keyboard-operable;
  layering clean.

### FOUNDER OBSERVATIONS on Core Use Cases (2026-07-20, for reconciliation)

- Actor-emphasis question: are we really "DRIVING system design" or reviewing it? The
  platform should perform the ch1–4 techniques itself; the human's job is review.
  Re-examine "Drive System Design" naming/framing (and swimlane emphasis) so the
  machine is the doer and the architect-user is the reviewer/ratifier.
- Granularity question: is "Execute a Construction Activity" the core use case, or is
  CONSTRUCTION itself the core use case (machine builds; user reviews)? Per-activity
  execution may be machine detail below the essence level. Architect+PM to weigh at
  reconciliation (interacts with UC3 pump + variations set).
- "Track Weekly Project Progress": confirmed book doctrine (Appendix A weekly earned-
  value tracking) — keep the capability, but reconsider whether it reads as a
  customer-facing variation or as machine-internal cadence surfaced in a console.
- Otherwise: high-level descriptions ratified as "pretty good" by founder (did not
  click through every step).

## System Design · Architecture (step 6) — reviewed, awaiting founder

Architect headline: layer rules CLEAN (zero violations across ~80 edges — no Client→
Engine/RA, Engines pure, single Manager→Manager correctly queued, closed layering);
call chains largely accurate; executing the THREE ALREADY-RATIFIED rulings makes
slot 5 converge exactly onto the built server code AND slot 3's rationale.

### Type 1 — content (the reconciliation amendment)

- [P1] Six components ruled dead / never built still committed:
  (a) HandOffEngine — cut ratified 2026-07-10 but NEVER EXECUTED anywhere: in roster,
  has serviceContract, package still built (server/internal/engine/handoff), still
  called (UC3 "PickWorkerClass"). Fold policy into ReviewEngine, delete package.
  (b) WorkItemAccess + WorkItemTracker — WIT-out; re-route 3 dynamic views.
  (c) 4 project-state facet RAs → collapse to ONE RA with 4 contract facets — the code
  is ALREADY one package (server/internal/resourceaccess/projectstate/) matching the
  2026-07-17 R-014 ruling. Post-reconciliation: 11 RAs = exactly the 11 built packages;
  Source Control Target claimants 6→3 = slot 3's own rationale. Contradiction dies free.
- [P2] Reconciled 1:1 volatility→owner mapping proposed (recorded in architect report):
  5 workflows→5 Managers; estimation models→2 Engines; pricing→BillingEngine;
  intervention→InterventionEngine; Review Policy (absorbing Hand-Off, axis 2)→
  ReviewEngine; autoscaling→AutoscalerEngine; Channel (absorbing Transport +
  SchedulerClient re-tag)→Client tier; Source Control Target→3 RAs under EXPLICIT
  facet doctrine in operationalConcepts; Operational State Store→3 RAs (facet doctrine);
  Durable Execution (axis 1)→DurableExecutionAccess; Pipeline Target→
  ConstructionPipelineAccess; Payment Provider→MerchantGatewayAccess; Customer App
  Infrastructure→NEEDS A REAL OWNER; Work Tracking→deleted.
  RevenueLedgerAccess: fold into BillingStateAccess (standalone no-op RA fails the
  encapsulation test; keep verbs as billing-state ops for revenue-flexibility door).
- [P2] Dead-op overfold (D1-D4) visible in committed UC3 chain: recordChangeReviewed
  appears twice (project-state-access edge 17 + construction-transition-access edge 25).
  Facet collapse should prune the 12 dead ops.
- [P2] Blurb vocabulary unreadable by customers: ref-CAS ×4, idempotency ×5, dedup ×5,
  content-addressed ×2; bare R-nnn references unresolvable in situ. Rewrite the
  customer-visible tier in business language; keep mechanics as engineer detail.
- [P3] ConstructionPipelineAccess misnamed (design managers dispatch through it too) —
  volatility is "agentic job execution target"; rename (e.g. AgenticJobAccess), serves
  Worker→Agent too. [P3] "worker" ×2 (dies with cut / judge Temporal jargon).
- Cardinality (honest): Managers 5 ✓ (at bound); Engines 7→6 post-cut, still above
  App-C ratio — architect recommends explicit standardCheck WAIVER, not forced merge;
  core ≈22 services vs TradeMe ~10 — defensible for a 5-family platform, no
  anti-patterns. Slot 6's "10 Resources / 11 RAs" decision text needs same touch-up.
- Slot 5 is the CLEANEST artifact for vocabulary (0 aiarch, 0 Method).

### Type 2 — app code

- [P1] (PM) No customer-facing narrative / volatility→component crosswalk: the data is
  all present (blurbs name volatility + R-nnn; 5 Managers ↔ 5 core use cases) but the
  screen is a 43-node engineer wall. Build the crosswalk + plain-language overview
  (also resolves bare-R-nnn links). PM ratifier-signal same as Volatilities.
- [P2] (UX) Component-focus click instantly opens comment composer
  (PerspectiveFlow.tsx:119-128) vs deliberate select-then-comment elsewhere
  (ArchitectureFlow.tsx:264-271 comment documents the intent). Align; prefer
  neighbor-click = refocus perspective.
- [P2] (PM) Dynamic-view captions are raw call signatures — pair with plain-language
  line for the customer.
- [P3] atomicBusinessVerbs populated on all 15 RAs but rendered NOWHERE — natural home
  is Component-focus node cards (RA verbs ARE the contract surface).
- [P3] Step-through control idioms differ (labeled buttons vs icon-only) between
  UseCase walkthrough and Dynamic view; ToggleButtonGroup-vs-Tabs is an app-wide
  convention question.
- Passes: node commenting identical in all 3 modes (shared C4Node factory, Enter/c);
  focus management mirrored from walkthrough; Autocomplete find pans/zooms via shared
  FocusNodes; viewMemory persists mode+dynamicKey+componentId; useMemo throughout.

### FOUNDER FINDINGS on Architecture (2026-07-20)

- QUESTION to architect (pending): are we actually doing ch04-style architecture
  VALIDATION — are the step-5 use cases validated against step-6 dynamic views/call
  chains (composition validation), or do the dynamic views merely exist? Where is the
  validation recorded?
- WorkItemTracker: founder reconfirms removal (extensible app can support external
  trackers, but not first-class). Already scoped in the coordinated WIT amendment.
- Component names "kinda weird, but fine" — low priority; note the
  ConstructionPipelineAccess→AgenticJobAccess rename covers the worst offender.
- [P2][2] Static-diagram dead end: seeing a component in Static view, there's no way
  to jump to Component-focus for it or see which dynamic views it participates in.
  Add node → "Focus this component" + "Appears in N use cases" cross-navigation.

## System Design · Operational Concepts (step 7) — reviewed, awaiting founder

### FOUNDER QUESTION ANSWERED — ch04 validation

YES in mechanism, PARTIAL in recorded evidence. Dynamic views ARE the validation
artifact per the architecture skill; machine-enforced (methodcheck USECASE-DYNAMIC-
MISSING at putDraftModel + in CI; ARCH-CHAINCOV core gate); coverage = all 17 (beyond
book). BUT: standardCheck pass is dated 2026-07-03 citing 39 components vs today's 43
(amendments never forced re-run); several pass items have EMPTY justifications; no
iteration ledger (chains that failed → decomposition changes). ADD: (1) slot-5
amendment marks standardCheck stale + forces re-run; (2) per-dynamic-view
validatedAtRevision stamp + finding set; (3) non-empty justification requirement.

### Type 1 — content

- [P1] Deployment view contradicts its own decisions ×3: (i) "Anthropic Messages API —
  LLM workers" external service vs "aiarch holds no LLM key" decision (WorkerAccess
  was removed by ratification); (ii) MerchantGateway "charge / payout / connected
  accounts" = pre-charge-only marketplace semantics, contradicts 2026-07-03
  ratification AND the billing decision's own text; (iii) OperatedSystemState "also
  holds the fallback project head-state" vs "project-state Postgres cluster is
  removed" two cards up. One deployment amendment.
- [P1] billing-model decision hard-codes "no flat subscription… construction is not
  charged… no token unit price" — strip the foreclosing clauses, keep the ServicePricing
  Strategy framing (the decision already contains the right half).
- [P1] Objective linkage is by NUMBER (justifyingObjective:int) — the mission rewrite
  will silently re-point all 11 decisions if objectives renumber. MOVE TO STABLE
  OBJECTIVE IDS BEFORE amending the mission; re-validate obj-2/obj-3-linked decisions
  after.
- [P2] Justification quality: only ~2 of 11 genuine (git-as-DB→audit STRONG;
  billing→obj3 consistent). Rubber stamps: Go→predictable-delivery, durable-primitives→
  obj1, catalog-read→obj4, topology→obj1 (better: obj7). Mis-pointed: agentic-dispatch→
  obj8 (really obj3/obj4). PM+architect converge: internal-only decisions should NOT
  claim a business objective; consider moving language/tooling policy out of the design
  artifact entirely (it re-smuggles scrubbed R-012/R-018).
- [P2] agentic-job dispatch GitHub-only vs ratified GitLocal reversal — and internally
  contradicts its own Local profile ("worker=claude -p", "No Anthropic key"). Restate
  venue-neutral.
- [P2] Missing decisions ×3 (architect proposed texts in report): review-policy gating
  (presets + non-overridable risk floor @506411e); construction-venue-selection-is-
  config; FACET DOCTRINE (home for the step-6 reconciliation — full proposed text in
  architect report).
- [P2] (PM) An artifact named "Operational Concepts" contains NONE of the customer's
  operational concerns: zero on uptime/SLA, data isolation, backup/DR; security only
  as a Keycloak infra box. Add customer-facing operational decisions. GOOD: data
  ownership/exit substantively covered (git-as-DB + .aiarch migration artifact) but
  buried in engineer framing — surface as a "you own and can export everything"
  guarantee.
- [P2] Vocabulary: "aiarch" ×9 (worst yet), "Method" ×2, "worker" ×5, "Claude Code
  account" load-bearing in billing decision — genericize.
- [P3] Changelog framing in committed decisions ("…are removed" ×2 — state current
  reality); gtd namespace = instance data in a design artifact (label "example" or
  drop).
- [P2] (PM) TIER the artifact: customer summary per decision (ratifiable) + engineer
  detail; engineer-only decisions drop objective claims.

### Type 2 — app code

- [P1] Deployment diagram nodes 100% keyboard-unreachable: all four node types are
  plain Box (no tabIndex/role/aria-label/keyboard handler) while FlowCanvas sets
  nodesFocusable=false expecting nodes to supply their own focusable inner element
  (as C4Node/ActivityNode do). No keyboard path to comment any deployment node. Apply
  the established focusable-wrapper pattern (DeploymentNodes.tsx).
- [P2] Cloud/Test/Local profile selection in plain useState — not remount-persistent
  (OperationalConceptsView.tsx:107); apply the module viewMemory pattern used by
  ArchitectureView/UseCaseCarousel.
- [P3] Truncated "justifies objective" line: SR gets full text via Tooltip aria-label,
  but sighted keyboard-only users can't trigger the tooltip (label lacks tabIndex).
- [P3] OperationalConceptsView on legacy layering allowlist (same debt class as
  VolatilityMap).
- CLEARED: the remembered "OpConcepts iframe bug" is NOT this screen — only iframe
  in webApp is construction/renderers/FrontendArtifactView.tsx (MCP-Apps surface);
  update the memory pointer.
- Passes: 11 decisions use the standard CommentableList idiom (fully consistent);
  deployment layout pure+memoized; no console errors.

### FOUNDER RULING on Operational Concepts (2026-07-20) — MAJOR THEME

RE-SCOPE the artifact into "Deployment & Operations Model". The current slot conflates:
(1) PLATFORM DOCTRINE (Temporal, clients→Managers, Envoy, Go, codegen, layering) —
archistrator DECIDES these as the opinionated SDLC; not per-project ratifiable; move
to platform method assets, render read-only ("how systems built here run");
(2) genuinely per-project operational choices (deployment scenario, venue, review
policy, scaling policy, infra building blocks) — the small ratifiable core;
(3) the deployment-models view (founder likes; = the deployment volatility) — KEEP but
fix the model: OperatedRuntime is NOT an "external service" (category error — it's the
product), Anthropic API + WIT contradictions already filed, and the LOCAL profile must
tell the real local story (where .aiarch git state lives, embedded Temporal, local SPA,
local agentic runner) instead of one empty laptop box.
Usage Log: surface as the customer trust story ("we meter what your running app
consumes; that's the only thing you're billed on"), not a Postgres table description.
EXACT SHAPE delegated to PM + architect at reconciliation (like Required-Behaviors).
For archistrator's own project, engineer-tier detail stays (tiered per PM proposal).

## System Design · Standard Check (step 8) — reviewed, awaiting founder

### Type 1 — content

- [P1] §mission check-logic defect (wrong when written, not rot): pass criterion
  silently degraded from "has an encapsulating component" to "rationale names
  something" — accepted the ownerless Customer App Infrastructure. Fix criterion to
  "exactly one component (or ratified facet group)" + re-disposition honestly.
- [P1] Staleness confirmed as ROT for everything else: checks were TRUE on 2026-07-03;
  rot event aa4bfa0/eb03e55 (2026-07-11, +4 RAs) never triggered a slot-7 re-run —
  even though 0809438 proves the staleness mechanism works for other slots.
- [P2] §billing-model = business-policy rule inside the design standard (revenue model
  triple-locked: R-013 + opconcepts + here). Remove or reduce to structural pointer.
  Sole intruder among 59 (PM swept). Custom items worth PROMOTING to the skill:
  §mission (fixed), §dsl, §DEP-GRAPH-IDENTITY.
- [P2] N/A has no status — encoded as pass (inflates "56 pass"); add n/a/deferred to
  CheckStatus. [P3] D5 half-wrong: "design iteratively" IS satisfied at Phase 1
  (amendment loop is the evidence).
- Waivers: all 3 genuinely GOOD (exemplary App C discipline — ironic vs 20 bare
  passes). Refresh at reconciliation: §2d 7→6 Engines (keep, update), §2h 22→~19
  (keep, update), §naming keep + opportunistic renames only on touched contracts.
- App C coverage COMPLETE (37/37 book items + Prime + 9 Directives, verified vs
  appc.xhtml.txt).
- [P2] Vocabulary: aiarch ×2, "Claude Code account" ×2 (load-bearing in
  §DEP-GRAPH-IDENTITY), Method ×1, worker ×1, WorkItem ×2.

### Type 2 — app code

- [P1] (PM) Stale self-attestation posture: 20/56 passes EMPTY evidence; no FAIL ever;
  nothing machine-verified; drift caveat chip exists in app but doesn't fire here.
- [P2] Machine-derived tier (architect's structural fix for the rot class): ~40 of 59
  rules derivable LIVE from the typed model (all counts, edge modes/directions,
  pubsub don'ts, chain coverage, encapsulator join, op counts, dsl round-trip);
  slot stores only attestations + waivers w/ verifiedAtRevision; screen joins both +
  drift warning. Genuinely attestational: Prime, D1-D4, §3a-d, waivers.
- [P2] (UX) 59-item flat list: no section grouping/jump-nav/filter (Glossary pattern
  exists for less); single roving tab stop = arrow through everything.
- [P2] (UX) show-more expanders lack aria-expanded/aria-controls (RejectedCandidates
  in VolatilityMap does it right — copy).
- [P2] (UX) Per-item staleness UI blocked on schema: add structured verifiedAt (+
  basisRevision) per item; StaleBasisChip pattern reusable but is slot-granular today.
- [P2] (PM) Tiering: customer summary ("checked against 59 rules; 3 waivers, here's
  what they mean") + engineer detail; waivers are the customer-facing weight.
- Passes: PASS/WAIVED chips text+color (WCAG 1.4.1 ok, contrast ok); commentable
  59/59; expander state row-local (no cross-render); view faithful; layering clean.

# ═══════════ PHASE 1 SYNTHESIS — RECONCILIATION AGENDA ═══════════

All 8 system-design screens reviewed (Mission, Glossary, Scrubbed Requirements,
Volatilities, Core Use Cases, Architecture, Operational Concepts, Standard Check).

## Amendment families (type 1) — execution order matters

0. PRECONDITION: stable objective IDs in the mission model (opconcepts links by
   number; renumbering silently re-points 11 decisions).
1. MISSION REWRITE: statement (drop components/(a)-(d)), obj 2 (design-system
   framing), obj 3 (revenue flexibility), new objectives (non-coder accessibility,
   hard-parts-handled, MCP/API extensibility, configurable review policy); re-point
   opconcepts justifications after.
2. VOCABULARY PURGE (all 8 artifacts): Method/rightingsoftware terms; aiarch→
   platform; worker→Agent; idle-pause→scale-to-zero; usage-assumption→expected-usage;
   "Claude Code" genericized where load-bearing.
3. REVENUE FLEXIBILITY: R-013/R-015, glossary Customer/Usage/End User, opconcepts
   billing decision + MerchantGateway payout blurb, standardcheck §billing-model.
4. WIT-OUT: Work Tracking volatility, WorkItemAccess+WorkItemTracker, 3 dynamic
   views, 3 flow steps, 2 glossary terms, deployment diagram, standardcheck refs.
5. REVIEW-POLICY FIRST-CLASS: glossary terms (Review Policy/Gate/Amendment), new
   requirement, volatility fold (Hand-Off→Review Policy, axis 2), gate branches in
   UC1/UC3 flows, opconcepts decision, Architect User definition fix.
6. ARCHITECTURE RECONCILIATION: execute HandOffEngine cut (roster+contract+package+
   chains); facet collapse 4→1 project-state RAs + facet doctrine decision in
   opconcepts; RevenueLedgerAccess→BillingStateAccess; Customer App Infrastructure
   owner; Channel/Transport merge; Durable Execution axis→1 (+rename option);
   SchedulerClient re-tag; ConstructionPipelineAccess→AgenticJobAccess; test-profile
   axis fixes; populate volatility traces + rejected[]; prune 12 dead ops; renumber/
   refresh standardcheck (§2d/§2h waivers, counts) + re-run.
7. ARTIFACT RESHAPES (decided): Scrubbed→"Required Behaviors" (PM's shape: id/
   behavior/statedAs/volatilityHint, contiguous renumber); OpConcepts→"Deployment &
   Operations Model" re-scope (platform doctrine → method assets read-only; per-
   project knobs + fixed deployment models; local story; PM+architect to shape);
   Standard Check: DECIDED by PM (delegated 2026-07-20) — teardown + conditional
   Design Health: (a) DELETE the standardCheck slot and step-8 committed artifact;
   (b) ~40 machine rules become always-on LIVE validations (methodcheck tier),
   surfaced as passive health strips ON the screens they concern (never committed);
   (c) the 3 waivers move onto their host artifacts as committed records (§2d+§naming
   → Architecture, §2h → Volatilities), keeping justification + re-eval triggers;
   (d) semantic attestations (Prime, D1-D4, §3a-d) attach to the Architecture
   artifact, re-attested via amendment→mark-stale; (e) "Design Health" VIEW
   (render-on-read, never committed): live check status + waiver ledger in plain
   language + attestations = the customer-confidence surface; (f) step 8 becomes
   REVIEW-POLICY-CONDITIONAL: vibes = no ceremony (phase seals when live checks
   green); checkpoints/full = explicit ratification moment over the Design Health
   view (red check or unacknowledged waiver blocks the seal).

## App changes (type 2) — by priority

P0/P1: Amend-guard backport (SystemDesignView.tsx:592, all 8 steps); deployment
nodes keyboard access (DeploymentNodes.tsx); typed encapsulatedBy (kills false-owner
prose join + clears legacy allowlist debt); PM-ratification/verdict signal on
committed artifacts; architecture narrative + volatility→component crosswalk view.
P2: double-COMMITTED strip; retro-theme axis text contrast; Volatilities focus-drop
fix; glossary chip counts + filter clear (Escape + ×); Component-focus click
behavior; full-diagram sidebar collapse; walkthrough step comment button; ID-gap/
provenance surfacing (schema-dependent); OpConcepts profile viewMemory; customer/
engineer tiering pattern (Architecture, OpConcepts, StandardCheck); standardcheck
grouping + aria-expanded + verifiedAt schema; stable-objective-id schema.
P3: axis-diagram demotion (founder option 2); dynamic-view plain-language captions;
atomicBusinessVerbs render; trigger/actors render; step-idiom consistency;
query dedupe (get-project ×3); Static→ComponentFocus cross-nav (founder);
Home screen: review-policy radios unselected, economics "awaits Phase 2" while done.

## Process/gate additions (rule 2 — validations we're missing)

- Amendment→staleness: slot-5 (or any basis) amendment must mark standardCheck stale
  + force re-run (mechanism exists, not wired for slot 7).
- methodcheck live-derivation tier for the ~40 mechanical standard-check rules.
- Non-empty justification gate on standard-check passes.
- Drafting-skill defects: volatilities draft never populates traces/rejected (view
  renders them); use-case draft never populates linkedCompId. Fix prompts/skills.
- Promote §mission/§dsl/§DEP-GRAPH-IDENTITY custom checks into the skill.
- validatedAtRevision stamps on dynamic views (ch04 evidence).

## Remaining review scope (before or after executing changes — founder's call)

- Phase 2: Project Design screens (9 steps). Phase 3: Construction console.
- Home/landing screens (drive-by findings parked).
