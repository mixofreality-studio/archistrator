# Manual QA pass — lessons learned (2026-07-20)

Full manual QA of system design → project design → construction screens, local mode,
reviewed per-screen by product-manager (method fidelity), system-architect (design
correctness), and ux-reviewer (a11y/perf/usability/M3) subagents.

Purpose of this file: capture per-screen lessons as we go so later screens (and later
QA passes) reapply them instead of rediscovering. Add a lesson the moment it is learned.

## Rig

- Server: `GOWORK=off go build` from `server/`, binary nohup'd from scratchpad
  (`boot-server.sh` sources `server/.env`, overrides `ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true`,
  `ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL=file://<scratch>/state-repo`,
  `ARCHISTRATOR_CONSTRUCTION_DRYRUN=true`, `ARCHISTRATOR_AUTH_DEV_MODE=true`). Listens :8888.
- State: **non-bare working checkout** of `main` at `<scratch>/state-repo`, with
  `git config receive.denyCurrentBranch updateInstead` set on it (2026-08-02: the local rail
  now REFUSES a bare state repo — the episode-trace sidecar lives under `.aiarch/traces/` in
  the checkout's working tree, and the capture-seam trust rule requires that path to physically
  exist outside the agent sandbox's write allowlist, which a bare repo cannot provide). Type-1
  changes (app comment system) land as commits there → fetch back into the working repo when
  done.
- SPA: pre-existing vite on :5199 (this checkout's webApp, `--strictPort`), proxies /api → :8888.
- Type-1 change = comment through the app (design/state changes). Type-2 change = direct
  repo change by subagents (app bugs, UX, method-fidelity fixes). Founder approves every
  change and every screen-exit.

## Cross-screen lessons

- **Phase-1/Phase-2 twin drift**: `SystemDesignView.tsx` and `ProjectDesignExperience.tsx`
  are near-twins; fixes land in one and not the other (F-GTD-11 Amend-guard fix existed
  only in phase 2). When fixing either experience, always check the twin — and prefer
  extracting shared logic over parallel edits.
- **Hover-hidden controls are a tooling artifact, not an a11y gap**: comment buttons are
  `opacity: 0` until hover/focus-within but remain real tab stops in the DOM. A live
  interactive a11y-tree snapshot under-reports them; verify in code before filing.
- **Judge mission/design content against the SKILL doctrine, not the raw book**: the
  skills carry binding founder rulings that are stricter than Löwy's text (e.g. no
  "components" vocabulary in the committed mission). Reviewer disagreements resolve in
  favor of the skill.

## Execution-phase lessons (reconciliation waves)

- Manager `deps[].component` references a serviceContracts CONTRACT KEY (codegen
  resolution), NOT a slot-5 component — the facet→owning-component join rides only on
  the contract's own `component` field. Re-pointing facet deps at owning components
  broke temporalgen invoker resolution and masqueraded as a platform codegen gap.
  When a "generator bug" appears right after a state edit, suspect the state edit's
  reference semantics first.
- Fail-closed migration scripts (assert preconditions, abort without writing) let
  state migrations be written in parallel with a moving merge safely.
- In a shared working tree with parallel agents, one whole-tree regen captures other
  families' in-flight state — classify build reds by family before reacting, and
  plan one final deterministic regen at integration.
- FALSE-GREEN GATES: webApp `npm run typecheck`/`check` run `tsc --noEmit`, which is
  VACUOUS here (root tsconfig is `files:[]` + project references with no `-b`, so it
  checks nothing). The real type gate is `tsc -b` (what `npm run build` runs). Every
  "webApp typecheck green" claim during this effort was meaningless until caught. Fixed
  the script to `tsc -b`. Lesson: verify a gate actually exercises what you think before
  trusting its green — run the command and confirm it reports a KNOWN error.
- SCHEMA-FIRST vs PUBLISHED-METHODCHECK divergence: renaming a wire field
  (statement→behavior) that archistrator's schema-first codegen owns still breaks the
  dogfood methodcheck gate (`go test -tags methoddesign`, runs the PUBLISHED methodcheck
  over own project.json), because the published projectmodel/methodcheck read the old
  field name and re-derive VOL-TRACE from statement text (not the explicit traces[]
  array). Tag-gated tests are NOT in the default `go test ./...` bar — always run the
  dogfood gate explicitly after any slot-shape change.

## Per-screen lessons

### System design

- Mission (step 1): screen renders the typed model faithfully, contrast/keyboard largely
  strong (CommentableList roving tabindex + `aria-keyshortcuts`), but committed-state
  chrome (Amend, ratification signal) is where the gaps were. Duplicate `get-project`
  fetches (×3) on load — check query dedupe on later screens too.
- Glossary (step 2): content findings cluster into vocabulary hygiene (book-term leakage,
  stale codename "aiarch", missing terms for real typed concepts like Review Policy) —
  check EVERY later artifact for the same three classes. Derived UI counts must share
  the same filter inputs as the list they describe (stale chip counts came from a memo
  whose deps omitted the query). Filters need an explicit clear path (Escape + ×).
  Amend-guard P0 confirmed step-agnostic: fix upstream once, then re-verify the
  committed-panel fill logic it currently dead-codes.
- Scrubbed Requirements (step 3): before reviewing an artifact's content, check whether
  the ARTIFACT ITSELF is book-doctrine or our extension (this one isn't in the book —
  founder caught it; reviewers didn't). Git archaeology answers content mysteries fast
  (ID gaps = deliberate re-scrub commit) — but if we needed git to answer it, the screen
  is hiding provenance the user needs. Typed-model shape review matters as much as text
  review: the slot dropped the volatility-hint hand-off column that step 4 consumes.
  Reverse-orphan check is productive: built features (ReviewPreset) with no requirement
  tracing to them expose missing requirements.
- Operational Concepts (step 7): an artifact can contradict ITSELF (deployment view vs
  its own decisions ×3) — intra-slot consistency checks matter, not just cross-slot.
  Numeric foreign keys (justifyingObjective:int) are amendment hazards — stabilize ids
  BEFORE amending the referenced artifact. Rubber-stamp traceability (every card forced
  to claim an objective) is worse than honest "internal decision, no business claim".
  New diagram node types must inherit the focusable-wrapper pattern — the deployment
  nodes shipped with zero keyboard access because they were built fresh instead of
  reusing C4Node's shell.
- Architecture (step 6): "ratified but never executed" is a distinct defect class —
  the HandOffEngine cut existed only as a ruling; roster, contract, package, and call
  chains all survived. Reconciliation should always end by diffing architecture roster
  vs built packages (here they converge exactly once rulings execute). When a model
  field is populated everywhere but rendered nowhere (atomicBusinessVerbs), that's
  UI debt, not data debt — inverse of the volatilities traces/rejected case.
- Core Use Cases (step 5): pairwise slot AGREEMENT checks beat single-slot review —
  slot 4's billing flow contradicted slot 5's already-reconciled dynamic view of the
  same use case; always diff a use case's flow against its dynamic view. The rejection-
  ledger discipline present here is the model for fixing the volatilities slot. UX
  verify-in-code beats screenshots (my "no zoom controls" was a crop artifact). The
  walkthrough's refocus-after-unmount pattern is the house standard for focus
  management — propagate wherever controls unmount under focus.
- Volatilities (step 4): cross-artifact JOINS are where the bodies are buried — the
  prose-substring "Encapsulated by" join silently displayed a false single owner and
  hid the 4-claimants defect; prefer typed id references over prose joins everywhere.
  When a typed contract has optional fields (traces/rejected) that the view renders but
  the committed data never populates, the DRAFTING PROMPT/skill is the real defect —
  check the other slots for same. A decorative diagram that duplicates an adjacent list
  is clutter unless it encodes a real dimension (founder instinct confirmed by
  zero-information analysis). Theme-specific contrast must be measured per theme —
  retro failed where 4 other themes passed on the same token.

### Project design

- (pending)

### Construction

- (pending)
