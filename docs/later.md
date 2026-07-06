# later.md — deferred work (written off 2026-07-05, qa-gtd-pass wrap)

> Wrap addendum: the agentic-sub-workflow representation (schema marker, diagram
> rendering, dynamics-as-manifest palettes) was built, founder-reviewed, REJECTED, and
> fully reverted (ae8646a/b3080ce/91dd5de) — do not resurrect without fresh direction.
> What shipped and STAYS: generated RA/Engine tool catalog (71 tools; state writes
> agent-hidden), in-substrate raw tool execution, composed verbs for design AND
> construction, construct session mode, prompt cutover to tools-only state writes,
> per-mode tool sets (composed verbs + all non-hidden read-only/Engine raw tools).

Founder ruling: after the four agentic-workflow priorities (generated tools in GH
Actions → agentic sub-workflows in use cases/dynamics → both project.jsons document
all use cases + agentic sub-workflows → glossary/method skills), STOP. Everything
below is deferred, in rough value order. Findings reference the qa-gtd-pass findings
log (session scratchpads; summarized in the final QA report).

## gtdapp Phase-2 completion (resume point)
- Re-fire the PM economics answer (F58/F82: question r2c1 seeded on planningAssumptions;
  answer job never dispatched pre-fix) and the architect integrations answer (failed on
  F80 pre-fix); founder inputs: DAU / revenue-share targets → planningAssumptions
  amendment (economics currently all zeros).
- Follow-up Architecture amendment: founder's two change-requests — add http + MCP
  clients (webapp + MCP entry points, agents capture/clarify/engage under an agent
  policy) and rename Persistence Access → Item Access. Comment texts preserved in the
  QA log / session transcript.
- Re-acknowledge Phase-2 slots re-flagged by architecture commits (dynamics-only
  changes; F76 idea: basis-diff-aware propagation or bulk acknowledge).
- Solutions chain: decompressed (retry — branch cleaned) → subcritical → compressed →
  risk model → SDP review interactively in the UI (options, time-cost/time-risk curves,
  commit decision) → project-design standard check → founder gate before construction.
- OpConcepts staleness reconcile after the architecture settles.
- F67 question to architect: normal-solution staffing cap 14 vs planning's 8 staff.

## UI polish batch (run ux-reviewer pass over the batch)
- F79 approve advances optimistically before server confirmation (approve failure
  invisible — operator misled; highest of this batch).
- F78 design experience resets to step 1 on background refresh; pending-comment
  dismissals don't persist.
- F62 amendment at AwaitingReview still framed "COMMITTED — sealed" (GENERATING case
  fixed; review case remains).
- F56 HomeBase lacks a Project-Design section (committed Phase-2 artifacts unreachable
  from home).
- F71 stepper nodes ignore synthetic/element clicks — keyboard a11y suspect.
- F74 stale banner offers "Reconcile via amendment" while an amendment is already in
  flight.
- F70 Team nav button inert. F61 stepper tooltip lingers/overlaps. F57 co-author rail
  hint wrong for not-drafted artifacts. Degenerate-layer warning banner (F81c) if not
  landed with the F81 fix.
- F72 stage enums differ across managers (systemdesign review=2, projectdesign=3) —
  expose string stage names in session views.

## Rail / server
- F69 unhandled 'redraft' signals (suspect signal-with-start leaves a buffered signal
  on every fresh session; benign but noisy — verify + drain).
- F19 approve of a never-drafted artifact silently no-ops (should FailedPrecondition).
- F20 pre-phase session reads leak Temporal internals to users.
- F22 get-project ships full research corpus on every read (mostly mitigated by F42
  pointers; verify the read model is slim now).
- F33 design-rail systemtest against a real local-git substrate (every rail bug cost a
  live Actions run to find; harness rig exists in aiarch-state-mcp rig test — extend to
  the server rail). Swap the harness's applyDraftToProjectJSON mirror for the real
  binary over stdio (recommended by the MCP build agent).
- F5 InterventionDrawer operator-steer controls inert (verify during construction).
- F6 UI preview iframes assume dogfood same-origin (preview strategy for external apps).
- F82 follow-through: answer-job status surfaced on question entries in the UI.
- archistrator STP scenarios missing required acknowledgeStale arg (5 STP-ARG-NAME
  ERRORs in the methoddesign gate — amend the committed systemTestPlan slot).
- dogfood slot-5 rev-2 earmark: work-item-tracker + work-item-access are planned-only
  (no server/internal/resourceaccess/workitem package) yet the UC2/UC3 call chains
  traverse them — decide at the NEXT slot-5 redraft: build the RA or re-route the
  work-item steps (earmark also carried on the work-item-tracker encapsulates).
- dogfood DV-REL-COVERAGE residue (warnings, accepted): ProjectDesignManager→
  {ConstructionPipelineAccess, SourceControlAccess} appear in no call chain (Phase-2
  draft dispatch has no dedicated use case — fold into a uc2 preamble edge or accept),
  plus the 13 utility edges (convention: utilities carry no dynamic-view lines).

## Platform (release-coordination gated)
- ClassifyStatus: split rate-limit 403 from permission 403 (bit approve twice today);
  keep response bodies (F14).
- Rail verbs ClosePullRequest / DeleteBranch (branch debris cleanup; spec preserved in
  session transcript; onboarding should also recommend delete_branch_on_merge).
- methodcheck release + pin bump: USECASE-DYNAMIC-MISSING + SYSTEM-LAYER-DEGENERATE
  twins live on archistrator-platform branch methodcheck-usecase-dynamic-missing;
  until released, only the app-side mirrors enforce. NOTE: that branch also carries
  an unpushed leftover commit a364acd9 (agentic sub-workflow methodcheck rules) for a
  feature the founder REVERTED — drop it when cutting the release.
- methodcheck PLATFORM TWINS PENDING for the state-validation rules shipped app-side
  2026-07-05 (server: statevalidationfindings.go read-back findings + RequireModelFields
  presence twins). The app enforces them only as read-back findings (and, for the
  presence/consistency subset, in projectstate.RequireModelFields on the write+read-back
  codec); the authoritative cross-artifact WRITE gate belongs in methodcheck:
    - SYS-RA-ORPHAN, SYS-ENCAPSULATES (client non-empty), SYS-REL-DUP,
      DV-CHAIN-CONNECTED   → System rules (app: findings only)
    - UC-VARIATION-REF, UC-ACT-PRESENT, UC-GUARD-LABEL → CoreUseCases (app:
      UC-ACT-PRESENT + UC-GUARD-LABEL already hard-blocked in RequireModelFields;
      UC-VARIATION-REF is a finding)
    - GLOSS-FOURQ (Glossary), SR-ID-UNIQUE (ScrubbedRequirements),
      OPC-TOPIC-COVERAGE (OperationalConcepts) → findings only
    - STD-STATUS-EXPLICIT, STD-FAIL-OPEN (StandardCheck), VOL-AXIS-EXPLICIT
      (Volatilities) → app hard-blocks status/axis PRESENCE in RequireModelFields;
      STD-FAIL-OPEN is gated at AdvancePhase (systemdesign manager).
  APP-SIDE DEVIATION recorded for the architect: SYS-ENCAPSULATES non-empty is enforced
  in RequireModelFields ONLY for the volatility-owning kinds (manager/engine/
  resourceAccess); CLIENT non-empty is a read-back FINDING, not a hard codec block,
  because committed state (gtdapp) carries empty-encapsulates clients and reads must
  never hard-fail (the critical read-safety invariant). Resource/utility empties are
  warnings. The methodcheck twin should decide whether client non-empty becomes a hard
  write gate.
- mcpgen upstream of the in-repo emitter; methodcheck enum-strictness asymmetry (F36).
- Platform release plan generally (v0.4.x line; scaffold pin 2 releases stale).

## Construction / Phase 3 (after the founder design sign-off gate)
- F4 generic construction scaffold seated at advance-to-construction (aiarch-construct
  + .claude tree; archistrator-operated infra constraint) — note P1b covers the MCP
  wiring inside the template; the seating flow itself is this item.
- Scaffold re-seat verb (CreateProject's constant idempotency key means seated workflow
  files never refresh; today's re-seat was a manual commit — needs a real
  SyncManagedScaffold verb).
- UC4 archistrator-operated actual deploy (k8s manifests, CNPG, Keycloak realm,
  Temporal namespace); operations RA still stub-only (503s).
- Construction dispatch QA: preview experience per activity type, InterventionDrawer,
  weekly tracking (the-method-project-tracking).

## Housekeeping
- .claude/agents/project-manager.md pre-existing uncommitted edit (untouched all pass —
  founder to keep or drop).
- gtdapp branch debris from pre-F40 sessions (0-draft/0-critique/1-draft…): deletable
  once rail cleanup verbs exist, or manually.
  - F82 (2026-07-06): the design-workflow TEMPLATE now self-heals a conflicting session-
    branch refresh — a conflict reconcile cannot resolve (a withdrawn/dead branch that
    survived, or a conflict beyond the owned state slot) hard-resets the scratch branch to
    origin/main + force-push (with lease) instead of dead-ending every future amendment of
    the slot. The active-draft reconcile path (F80b) is unchanged. This does NOT retire the
    debris earmark or the ClosePullRequest/DeleteBranch rail verbs above — the stale
    BRANCHES still linger until real cleanup exists; self-heal only stops them BRICKING new
    amendments. Also: existing product repos (gtdapp) keep the OLD refresh behavior until
    re-seated — the seated copy is a committed snapshot and only refreshes via the
    SyncManagedScaffold re-seat verb (see Construction earmark), so their brick persists
    until re-seat.
- webApp prettier drift (~71 files, pre-existing on main, not enforced).
- Onboarding copy: recommend org-level CLAUDE_CODE_OAUTH_TOKEN + `gh secret list`
  verification (F18); F8 zero UpdatedAt renders "12/31/1"; F9 ghost card for
  adopted-but-uncreated projects needs a "finish creating" CTA.
