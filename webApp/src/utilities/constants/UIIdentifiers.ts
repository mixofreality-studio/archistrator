/**
 * Centralized UI test identifiers (data-testid). No magic strings in components
 * or tests — reference these. The future black-box uitests Playwright package
 * selects interactive elements by these stable ids.
 */
export const UI_IDENTIFIERS = {
  Shell: {
    APP_BAR: 'app-bar',
    APP_SHELL: 'app-shell',
    BRAND: 'app-shell-brand',
    PROJECT_MENU: 'project-menu',
    projectMenuItem: (projectId: string) => `project-menu-item-${projectId}`,
    PROJECT_MENU_NEW: 'project-menu-new',
    ACCOUNT_MENU_BUTTON: 'account-menu-button',
    ACCOUNT_MENU_HOME: 'account-menu-home',
    ACCOUNT_MENU_ALL_PROJECTS: 'account-menu-all-projects',
    ACCOUNT_MENU_CHANGES: 'account-menu-changes',
    ACCOUNT_MENU_BILLING: 'account-menu-billing',
    ACCOUNT_MENU_TEAM: 'account-menu-team',
    TEAM_NAV: 'team-nav',
    USER_LABEL: 'user-label',
    LOGOUT_BUTTON: 'logout-button',
    DEV_MODE_BADGE: 'dev-mode-badge',
    THEME_SWITCHER: 'theme-switcher',
    themeOption: (key: string) => `theme-option-${key}`,
  },
  ProjectsLanding: {
    SCREEN: 'projects-landing-screen',
    GRID: 'projects-grid',
    EMPTY_STATE: 'empty-state',
    NEW_PROJECT_CARD: 'new-project-card',
    NEW_PROJECT_BUTTON: 'new-project-button',
    CREATE_PROJECT_DIALOG: 'create-project-dialog',
    NEW_PROJECT_NAME_INPUT: 'new-project-name-input',
    CREATE_PROJECT_SUBMIT: 'create-project-submit',
    CREATE_PROJECT_CANCEL: 'create-project-cancel',
    CREATE_PROJECT_PREREQS: 'create-project-prereqs',
    projectCard: (projectId: string) => `project-card-${projectId}`,
  },
  HomeBase: {
    SCREEN: 'home-base-screen',
    RESUME_DESIGN: 'resume-design',
    ECONOMICS_STRIP: 'economics-strip',
    ARTIFACT_TOC: 'artifact-toc',
    ARTIFACT_PROSE: 'artifact-prose',
    phaseCard: (phase: string) => `phase-card-${phase}`,
    tocRow: (kind: string) => `toc-row-${kind}`,
    OPEN_SYSTEM_DESIGN: 'open-system-design',
    OPEN_PROJECT_DESIGN: 'open-project-design',
    // Ghost-project recovery affordance (repo adopted but head-state init failed).
    GHOST_PANEL: 'home-base-ghost-panel',
    GHOST_FINISH_SETUP: 'home-base-ghost-finish-setup',
    GHOST_BACK: 'home-base-ghost-back',
    // Review-policy preset control (vibes / checkpoints / full) + its permanent
    // deploy/spend/schema risk-floor note.
    REVIEW_POLICY: 'review-policy-control',
    reviewPolicyOption: (preset: string) => `review-policy-option-${preset}`,
    REVIEW_POLICY_FLOOR_NOTE: 'review-policy-floor-note',
  },
  DesignWizard: {
    SCREEN: 'design-wizard-screen',
    artifactStep: (kind: string) => `artifact-step-${kind}`,
  },
  UseCaseCarousel: {
    // The Core Use Cases artifact's grouped use-case picker (Core / Variations
    // ListSubheader sections — A6). Selectable black-box via testid rather than
    // its "Use case" label text.
    PICKER: 'usecase-picker',
    // View-mode toggle (walkthrough choose-your-path vs. full activity diagram).
    VIEW_WALKTHROUGH: 'usecase-view-walkthrough',
    VIEW_DIAGRAM: 'usecase-view-diagram',
    // Ch. 4 band-context marker on the corpus summary line ("target 2–6") —
    // carries the warning accent when the core count falls outside the band.
    CORE_BAND: 'usecase-core-band',
    // The WHY-core essence rationale prose on a CORE use case's sidebar card
    // (symmetric to the nonCore rejectionReason).
    ESSENCE_RATIONALE: 'usecase-essence-rationale',
    // Per-use-case navigable join → the Architecture step's Dynamic lens,
    // preselected to this use case's call chain (?view=<dynamic-view-key>).
    CALL_CHAIN_LINK: 'usecase-call-chain-link',
    // The walkthrough's "Next" advance control (single-successor step). Black-box
    // hook for asserting the per-step camera move on the you-are-here map.
    WALKTHROUGH_NEXT: 'walkthrough-next',
    // Walkthrough nav: rewind one step / restart from the start node.
    WALKTHROUGH_BACK: 'walkthrough-back',
    WALKTHROUGH_RESTART: 'walkthrough-restart',
    // A branch-choice button at a decision/fork, keyed by its outgoing edge
    // (`${from}-${to}` — the same edge key the activity diagram uses).
    walkthroughBranch: (edgeId: string) => `walkthrough-branch-${edgeId}`,
    // A breadcrumb Path chip, keyed by its position in the walked path.
    walkthroughPathStep: (index: number) => `walkthrough-path-${String(index)}`,
    // The you-are-here map's CURRENT step node (the ringed node). Exactly one is
    // present in walkthrough mode; its identity changes as the reader advances.
    WALKTHROUGH_CURRENT_NODE: 'walkthrough-current-node',
    // Per-use-case realization roll-up chip ("N/M steps realized"), rendered next
    // to the CORE/NON-CORE chip — the carousel-level summary of the walkthrough's
    // per-step badges (Task 11).
    REALIZATION_CHIP: 'usecase-realization-chip',
    // The current walkthrough step's realization badge on the focus card — one of
    // "✓ realized" / "✗ <ruleId>" / "— no realization".
    STEP_BADGE: 'usecase-step-badge',
    // The current step's compact mono call list + "View call chain" join, shown
    // below the focus card's title whenever the step is realized.
    STEP_CALLS: 'usecase-step-calls',
  },
  DesignExperience: {
    ROOT: 'design-experience',
    CLOSE: 'design-close',
    SLIM_SPINE: 'slim-spine',
    LOADING_SKELETON: 'design-loading-skeleton',
    spineStep: (kind: string) => `spine-step-${kind}`,
    REQUEST_DRAFT: 'request-draft',
    RESEARCH_INPUT: 'research-input',
    RESEARCH_INPUT_TITLE: 'research-input-title',
    RESEARCH_INPUT_TEXT: 'research-input-text',
    RESEARCH_INPUT_SUBMIT: 'research-input-submit',
    GENERATING_SCENE: 'generating-scene',
    GENERATING_ROLE_LINE: 'generating-role-line',
    AMENDING_NOTICE: 'generating-amending-notice',
    CI_JOB_NOTICE: 'ci-job-notice',
    CI_JOB_LINK: 'ci-job-link',
    ARTIFACT_INTRO: 'artifact-intro',
    // Header (?) info button that carries the committed artifact's framing copy
    // (replaces the full-width committed intro banner).
    ARTIFACT_INFO: 'artifact-info',
    ARTIFACT_RENDER: 'artifact-render',
    // The artifact's scroll container. The comment margin measures every anchor
    // offset against THIS element, so an acceptance test that drives the page's
    // scroll must drive this one and not the window.
    DESIGN_SCROLL: 'design-scroll',
    DRAFT_FAILED: 'draft-failed',
    DRAFT_FAILURE_REASON: 'draft-failure-reason',
    DRAFT_FAILURE_RUN_LINK: 'draft-failure-run-link',
    DRAFT_FAILED_GATE_ERROR: 'draft-failed-gate-error',
    RETRY_DRAFT: 'retry-draft',
    WITHDRAW_DRAFT: 'withdraw-draft',
    // Committed-panel amendment affordances: the header Amend button, its small
    // rationale composer, and the composer's controls.
    AMEND: 'committed-amend',
    RECONCILE: 'committed-reconcile',
    AMEND_COMPOSER: 'amend-composer',
    AMEND_RATIONALE: 'amend-rationale',
    AMEND_INCLUDE_PENDING: 'amend-include-pending',
    AMEND_SUBMIT: 'amend-submit',
    AMEND_CANCEL: 'amend-cancel',
    // 'COMMITTED · revision N' meta on the committed-panel header.
    COMMITTED_REVISION: 'committed-revision',
    // Read-only 'COMMITTED … — current' label shown above the generating scene while
    // a committed artifact's amendment drafts.
    AMEND_CURRENT_LABEL: 'amend-current-label',
    // 'basis changed — reconcile' warning chip (committed panel + HomeBase rows).
    STALE_CHIP: 'stale-basis-chip',
    // F45 stale banner (committed pane) + its two actions and the "mark reviewed —
    // unaffected" confirm-strip (note field + confirm/cancel).
    STALE_BANNER: 'stale-basis-banner',
    STALE_RECONCILE: 'stale-reconcile',
    STALE_MARK_REVIEWED: 'stale-mark-reviewed',
    STALE_ACK_NOTE: 'stale-ack-note',
    STALE_ACK_CONFIRM: 'stale-ack-confirm',
    STALE_ACK_CANCEL: 'stale-ack-cancel',
    // F-GTD-12: caption explaining why "mark reviewed" is disabled (amendment in flight).
    STALE_ACK_DISABLED: 'stale-ack-disabled',
    // F-GTD-18: inline error line when the acknowledge mutation failed.
    STALE_ACK_ERROR: 'stale-ack-error',
    // F-GTD-18: warning above the review gate after a contained approve/merge-window
    // fault (the session returned to awaitingReview carrying a failureReason).
    APPROVE_FAULT: 'approve-fault',
    // Compact stale marker on a spine step, keyed by slot kind.
    spineStale: (kind: string) => `spine-stale-${kind}`,
  },
  Glossary: {
    // The glossary reference widget (GlossaryView): search + Four-Questions
    // filter chips + the grouped, alphabetized term list.
    ROOT: 'glossary-view',
    SEARCH: 'glossary-search',
    // The search field's clear (×) button, shown only while the query is non-empty.
    CLEAR: 'glossary-clear',
    // The "All · N" reset chip.
    CHIP_ALL: 'glossary-chip-all',
    // A category filter chip, keyed by its Four-Questions BASE label
    // (Who/What/How/Where/Uncategorized — refined How-* sub-labels roll up).
    chip: (base: string) => `glossary-chip-${base}`,
    // A category section header, keyed by its (possibly refined) display label.
    section: (label: string) => `glossary-section-${label}`,
    // The "no terms match" filtered-empty state.
    EMPTY: 'glossary-empty',
    // The per-term usage chip row (cross-artifact whole-word joins), keyed by the
    // item's original-array index (the same identity the comment anchors use).
    usage: (index: number) => `glossary-usage-${String(index)}`,
    // One usage chip-link on that row (→ the used-in step), keyed by index + step kind.
    usageLink: (index: number, kind: string) => `glossary-usage-link-${String(index)}-${kind}`,
  },
  VolatilityMap: {
    // The volatilities artifact's two-lane single-select map (VolatilityMap).
    ROOT: 'volatility-map',
    // One lane listbox, keyed by its Löwy axis value (the adapters Axis union).
    lane: (axis: string) => `volatility-lane-${axis}`,
    // One chip (role=option), keyed by the point's index in the flat points
    // array — the comment-anchor identity ($.items[n]).
    chip: (index: number) => `volatility-chip-${String(index)}`,
    // The side-rail inspect card for the selected volatility + its clear (×).
    DETAIL: 'volatility-detail',
    DETAIL_CLOSE: 'volatility-detail-close',
    // The side-rail summary shown when nothing is selected.
    SUMMARY: 'volatility-summary',
    // The compact axes-overview SVG above the lanes (decorative for AT — the
    // lanes are the accessible surface; dots are pointer-clickable only).
    AXES: 'volatility-axes',
    // One clickable dot in the axes overview, keyed by the SAME flat points
    // index as chip() — clicking selects the same item as the lane chip.
    dot: (index: number) => `volatility-dot-${String(index)}`,
    // The rejected-candidates disclosure (GatePanel button pattern) + its rows,
    // keyed by the candidate's index in the model's `rejected` array.
    REJECTED_TOGGLE: 'volatility-rejected-toggle',
    REJECTED_LIST: 'volatility-rejected-list',
    rejectedItem: (index: number) => `volatility-rejected-${String(index)}`,
    // Navigable join on the detail card: "Encapsulated by" component links
    // (→ Architecture step, keyed by owner position). The trace-id links went with
    // the Required Behaviors step — the ids still render, as plain provenance text.
    ownerLink: (index: number) => `volatility-owner-link-${String(index)}`,
  },
  Architecture: {
    VIEW_SWITCH: 'arch-view-switch',
    VIEW_STATIC: 'static',
    VIEW_DYNAMIC: 'dynamic',
    VIEW_PERSPECTIVE: 'perspective',
    // The 4th lens on the same System artifact: the committed deployment
    // topology (DeploymentFlow, the same one the Deployment & Operations step
    // renders). Instance-less like Static — no companion picker.
    VIEW_DEPLOYMENT: 'deployment',
    DYNAMIC_PICKER: 'arch-dynamic-picker',
    PERSPECTIVE_PICKER: 'arch-perspective-picker',
    // Dynamic step-through Prev/Next controls (F-QA2-51 testability).
    DYNAMIC_STEP_PREV: 'arch-dynamic-step-prev',
    DYNAMIC_STEP_NEXT: 'arch-dynamic-step-next',
    // The owning use case's WALKTHROUGH, rendered beside the call chain in the
    // dynamic lens: the stepper that drives it (founder QA round 2). Absent when
    // the view links no use case with a diagram (the chain then runs full-width
    // and pages itself with DYNAMIC_STEP_PREV/NEXT).
    DYNAMIC_ACTIVITY_TRACE: 'arch-dynamic-activity-trace',
    // The call-chain caption in fragment mode: the calls the walkthrough's
    // current step realizes, or "No realization for this step".
    DYNAMIC_FRAGMENT: 'arch-dynamic-fragment',
    // The anti-functional-decomposition badge on a C4 node (a Manager/Engine/
    // ResourceAccess encapsulating no identified volatility), keyed by component id.
    noVolatility: (componentId: string) => `arch-no-volatility-${componentId}`,
    // Design-Health structure findings joined onto the diagram (findingOverlays):
    // the badge on an offending relationship edge (keyed by its from/to pair), the
    // badge on an offending component node, and the legend count chip (a read-only
    // count since the Design Health step retired).
    findingEdge: (from: string, to: string) => `arch-finding-edge-${from}-${to}`,
    findingNode: (componentId: string) => `arch-finding-node-${componentId}`,
    /** One C4 component card in any PerspectiveFlow/architecture canvas. */
    c4Node: (componentId: string) => `arch-c4-node-${componentId}`,
    FINDING_COUNT: 'arch-finding-count',
    // Task 6 (call-chain rollout): the per-view CC verdict roll-up (viewVerdict),
    // rendered beside DYNAMIC_PICKER — "15/15 realized · CC clean" / "0/7
    // realized · pending" / "N CC findings" / "N/M realized".
    VIEW_VERDICT: 'arch-view-verdict',
    // The FragmentBar CC-checks chip ("CC checks · passing" / "CC checks · N
    // findings here" — "here" qualifies the step-scoped count against
    // VIEW_VERDICT's view-scoped one, fix round 1 FINDING 2). A read-only count
    // since the Design Health step retired, present only when findings context is
    // loaded (statusBySeq defined).
    CC_CHECKS_CHIP: 'arch-cc-checks-chip',
  },
  // The Deployment & Operations Model page is retired, and every testid in its old
  // `Deployment` namespace (profile switch, knobs, trust, infra, doctrine, objective
  // links) went with it. The committed TOPOLOGY still renders — as the Architecture
  // step's Deployment lens — under Architecture.VIEW_DEPLOYMENT above; DeploymentFlow's
  // own nodes carry no testids of their own (uitests select them structurally by
  // xyflow's generated `.react-flow__node` class).
  GatePanel: {
    ROOT: 'gate-panel',
    APPROVE: 'gate-approve',
    APPROVE_CONFIRM: 'gate-approve-confirm',
    APPROVE_CANCEL: 'gate-approve-cancel',
    SENDBACK: 'gate-sendback',
    WITHDRAW: 'gate-withdraw',
    FINDINGS: 'findings',
    // The surfaced PM-critique conclusion (F-QA2-7): disclosure header + body.
    PM_REVIEW: 'gate-pm-review',
    PM_REVIEW_BADGE: 'gate-pm-review-badge',
    // Banner naming the open-comment count that blocks approve.
    OPEN_BLOCK: 'gate-open-block',
    // Graceful FailedPrecondition surface after an approve race.
    GATE_ERROR: 'gate-error',
  },
  // The comment COMPOSER, which moved out of the deleted ChatRail into the foot
  // of the margin. The ids keep their `chat-*` spelling so the existing uitests
  // selectors keep resolving; Task 11 renames them along with its own specs.
  Chat: {
    RAIL: 'chat-rail',
    TOGGLE: 'chat-toggle',
    SEND: 'chat-send',
    INPUT: 'chat-input',
    // Composer type/addressee pickers + the separate Ask send (question-comments).
    TYPE_CHANGE_REQUEST: 'chat-type-change-request',
    TYPE_QUESTION: 'chat-type-question',
    ADDRESSEE_PM: 'chat-addressee-pm',
    ADDRESSEE_ARCHITECT: 'chat-addressee-architect',
    ASK: 'chat-ask',
    commentAnchor: (n: number) => `comment-anchor-${String(n)}`,
  },
  Margin: {
    ROOT: 'comment-margin',
    UNPLACED: 'comment-margin-unplaced',
    RESOLVED_DISCLOSURE: 'comment-margin-resolved',
    // The composer card at the foot of the margin (armed-anchor chip + input).
    COMPOSER: 'comment-margin-composer',
    card: (id: string) => `margin-card-${id}`,
    reply: (id: string) => `margin-reply-${id}`,
    resolve: (id: string) => `margin-resolve-${id}`,
    reopen: (id: string) => `margin-reopen-${id}`,
    // A STAGED (posted locally, not yet sent) note, keyed by its accumulator index.
    staged: (n: number) => `margin-staged-${String(n)}`,
    stagedDiscard: (n: number) => `margin-staged-discard-${String(n)}`,
  },
  // Comment-anchoring affordances that arm a CommentContext anchor from a
  // diagram surface or a text selection. Diagram edges/nodes arm on CLICK (React
  // Flow's `selected` state is inert in these controlled graphs), so the two
  // click-armed surfaces expose no button of their own — assert via ARMED_ANCHOR.
  Comments: {
    // Floating "Comment" button over a non-collapsed text selection (mouse OR keyboard).
    SELECTION_POPOVER: 'comment-selection-popover',
    // Dynamic sequence-view per-step comment button (in the step caption bar).
    STEP_COMMENT: 'comment-step',
    // Use-case-as-a-whole comment button (next to the use-case picker).
    USECASE_COMMENT: 'comment-usecase',
    // Invisible probe reflecting the currently-armed anchor (data-anchor-* attrs).
    // Static-edge + deployment-node arming (click-only) is observed through this.
    ARMED_ANCHOR: 'comment-armed-anchor',
    // The shared CommentableList primitive (role=list) that makes any itemized
    // artifact keyboard-navigable and item-granular-commentable.
    LIST: 'comment-list',
    // A CommentableList row (role=listitem), keyed by the caller-supplied item key.
    listItem: (key: string) => `comment-list-item-${key}`,
    // The per-row "Comment on this item" button inside a CommentableList row.
    listItemComment: (key: string) => `comment-list-item-button-${key}`,
  },
  ProjectDesign: {
    SDP_ASSEMBLE: 'sdp-assemble',
    ADVANCE_CONSTRUCTION: 'advance-construction',
    ADVANCE_RESULT: 'advance-result',
    ADVANCE_STALE_ERROR: 'advance-stale-error',
    ADVANCE_ANYWAY: 'advance-anyway',
  },
  Construction: {
    ROOT: 'construction-console',
    // TAB_TRACKER/TAB_INTERVENTIONS/TAB_ARTIFACTS and the tab bodies' own root
    // testids (TRACKER/INTERVENTIONS/ARTIFACTS/artifactRow) retired with the tab
    // shell (Task 13) — the lens shell (LENS_TOOLBAR etc. below) is the console
    // now. The retired components' ids (the summary strip, the tracker nodes, the
    // lifecycle panel, the policy and phase-gate panels, the intervention queue,
    // drawer and override controls) went with them (cleanup round); B1 mints its
    // own ids when it rebuilds pause, resume and override, the same way Task 3
    // minted LENS_* rather than reviving stale ones.
    AWAITING: 'construction-awaiting',
    SYSTEM_TEST_VIEW: 'construction-system-test-view',
    TEST_PLAN_VIEW: 'construction-test-plan-view',
    FRONTEND_VIEW: 'construction-frontend-view',
    // No live preview iframe: the app refuses to be framed (a4130340). A built
    // surface's route opens in a new tab; with none recorded, the §5.5 empty state.
    FRONTEND_OPEN_LINK: 'construction-frontend-open-link',
    FRONTEND_NO_SURFACES: 'construction-frontend-no-surfaces',
    SCENARIO_PICKER: 'construction-scenario-picker',
    caseChip: (caseId: string) => `construction-case-chip-${caseId}`,
    /** The selected case's "what this proves / expected outcome" box (its left
     *  border carries the case's ink) and the EXPECT label inside it. */
    ACTIVE_CASE: 'construction-active-case',
    CASE_EXPECT: 'construction-case-expect',
    BEGIN_BUTTON: 'construction-begin',
    /** The alert a failed Begin dispatch raises — failures are loud (spec §6). */
    BEGIN_ERROR: 'construction-begin-error',
    /** The note after a 200 that dispatched nothing ("Nothing to dispatch…"). */
    BEGIN_NOTE: 'construction-begin-note',
    // The confirm step in front of Begin/Resume: it names what would be
    // dispatched before anything is (BeginConfirmDialog).
    BEGIN_CONFIRM_DIALOG: 'construction-begin-confirm',
    BEGIN_CONFIRM_CANCEL: 'construction-begin-confirm-cancel',
    BEGIN_CONFIRM_DISPATCH: 'construction-begin-confirm-dispatch',
    // B1.7: a paused project offers Resume in Begin's place, with its status label.
    RESUME_BUTTON: 'construction-resume',
    PAUSED_LABEL: 'construction-paused-label',
    RESUME_OUTCOME: 'construction-resume-outcome',
    TASKS_PAUSED_LABEL: 'construction-tasks-paused-label',
    beginConfirmCandidate: (activityId: string) => `construction-begin-candidate-${activityId}`,
    // P1-9: the LIST's way back when the toolbar filtered every row away.
    LIST_CLEAR_FILTERS: 'construction-list-clear-filters',
    // P1-6: the pane header's count line when no single task is selected
    // ("N attempts · M phases"), in place of the attempt selector.
    DETAIL_SELECTION_SUMMARY: 'construction-detail-selection-summary',
    // P0-4: a tier-1 row's id cell (sized in ch, titled with the full id).
    listIdCell: (activityId: string) => `construction-list-id-${activityId}`,
    listTitleCell: (activityId: string) => `construction-list-title-${activityId}`,
    /** An integration-pending activity's fromPhase row: "waits on …" / "next in line". */
    listPendingLine: (activityId: string) => `construction-list-pending-${activityId}`,
    /** The same line on the hover card's fromPhase line (graph lens). */
    graphHoverPending: (activityId: string) => `construction-graph-hover-pending-${activityId}`,
    /** The pane's sentence for an integration-pending activity. */
    DETAIL_PENDING_RESUME: 'construction-detail-pending-resume',
    /** An activity's pending operator notes: the list row's mark (pendingNotes.ts). */
    listPendingNote: (activityId: string) => `construction-list-pending-note-${activityId}`,
    /** The pane's pending-note line (pendingNotes.ts). */
    DETAIL_PENDING_NOTE: 'construction-detail-pending-note',
    /** A TASKS row's pending-note line (pendingNotes.ts). */
    tasksPendingNote: (key: string) => `construction-tasks-pending-note-${key}`,
    /** The book's Figure A-1 task key, shown beside a task the profile renamed (P1-7). */
    listTaskBookKey: (nodeId: string) => `construction-list-task-book-key-${nodeId}`,
    // Stage D — the GRAPH lens: the architecture layer by layer, each component
    // card carrying its activity's lifecycle spine (components/construction/graph).
    GRAPH_CANVAS: 'construction-graph-canvas',
    GRAPH_RIBBON: 'construction-graph-ribbon',
    graphMilestone: (id: string) => `construction-graph-milestone-${id}`,
    /** The key's popover content; GRAPH_KEY_BUTTON opens it (designer P1-2). */
    GRAPH_KEY: 'construction-graph-key',
    GRAPH_KEY_BUTTON: 'construction-graph-key-button',
    /** "N of 29 match · Clear filters" — shown only while a filter is active (P1-4). */
    GRAPH_FILTER_STATUS: 'construction-graph-filter-status',
    /** The pinned HTML row-label gutter and one row's label in it (P1-6). */
    GRAPH_ROW_GUTTER: 'construction-graph-row-gutter',
    graphRowLabel: (row: string) => `construction-graph-row-label-${row}`,
    GRAPH_CLEAR_FILTERS: 'construction-graph-clear-filters',
    GRAPH_LAYER_CHECK: 'construction-graph-layer-check',
    graphCard: (cardId: string) => `construction-graph-card-${cardId}`,
    graphLane: (activityId: string) => `construction-graph-lane-${activityId}`,
    graphSegment: (activityId: string, phase: string) =>
      `construction-graph-segment-${activityId}-${phase}`,
    GRAPH_HOVER_CARD: 'construction-graph-hover-card',
    /** One lane's line inside the hover card (carries data-provenance). */
    graphHoverLane: (activityId: string) => `construction-graph-hover-lane-${activityId}`,
    /** A lane's float rail + numeral — rendered ONLY when the network has a computed entry. */
    // Not under the `construction-graph-lane-` prefix: specs select the lane
    // family by that prefix, and a float mark must never count as a lane.
    graphLaneFloat: (activityId: string) => `construction-graph-float-${activityId}`,
    GRAPH_SCHEDULE_CAPTION: 'construction-graph-schedule-caption',
    /** M0's hover (PM Q4 copy) and its navigation-only link to the SDP review. */
    GRAPH_M0_HOVER: 'construction-graph-m0-hover',
    GRAPH_M0_OPEN_SDP: 'construction-graph-m0-open-sdp',
    /** M0's copy as a popover — the chip is a button, so the link is keyboard-reachable. */
    GRAPH_M0_POPOVER: 'construction-graph-m0-popover',
    /** The key's drawn swatches: hatch, spine, float, critical (designer re-check 8). */
    graphKeySwatch: (kind: string) => `construction-graph-key-swatch-${kind}`,
    // The TASKS lens (Stage C) — one row per decision the pipeline is stopped on
    // (tasks/owedWork.ts), keyed by the item's own key
    // (`<activityId>:<gateTask>:<round>` or `<activityId>:<reason>`).
    TASKS_LENS: 'construction-tasks-lens',
    TASKS_HEADLINE: 'construction-tasks-headline',
    TASKS_SLOTS: 'construction-tasks-slots',
    TASKS_POLICY_BANNER: 'construction-tasks-policy-banner',
    TASKS_POLICY_SUMMARY: 'construction-tasks-policy-summary',
    TASKS_POLICY_LINK: 'construction-tasks-policy-link',
    TASKS_TABLE: 'construction-tasks-table',
    TASKS_EMPTY: 'construction-tasks-empty',
    TASKS_EMPTY_COUNTS: 'construction-tasks-empty-counts',
    TASKS_RESUME: 'construction-tasks-resume',
    // Probes with no answer (architect Q1): "Checking N…" / "Couldn't check N…".
    TASKS_UNCHECKED: 'construction-tasks-unchecked',
    TASKS_UNCHECKED_RETRY: 'construction-tasks-unchecked-retry',
    // "1 shown · 3 owed" when the toolbar hides owed decisions (review I4).
    TASKS_FILTERED: 'construction-tasks-filtered',
    tasksRow: (key: string) => `construction-tasks-row-${key}`,
    tasksCell: (key: string, column: string) => `construction-tasks-${column}-${key}`,
    tasksReview: (key: string) => `construction-tasks-review-${key}`,
    tasksSteer: (key: string, action: string) => `construction-tasks-steer-${action}-${key}`,
    tasksGitHub: (key: string) => `construction-tasks-github-${key}`,
    tasksFlow: (key: string) => `construction-tasks-flow-${key}`,
    // The decision the shared pane carries for an owed gate (Stage C Task 5).
    DETAIL_DECISION_NOTE: 'construction-detail-decision-note',
    DETAIL_DECISION_SEND_BACK: 'construction-detail-decision-send-back',
    DETAIL_DECISION_FLOW: 'construction-detail-decision-flow',
    // After a decision: the composer's caption (the note is not delivered yet) and
    // the body's lead line (designer P0-1, P1-4).
    DETAIL_DECISION_CAPTION: 'construction-detail-decision-caption',
    DETAIL_DECISION_LEAD: 'construction-detail-decision-lead',
    // The drawer's footer link to the next owed decision, below 1200px (designer P2).
    DETAIL_NEXT_DECISION: 'construction-detail-next-decision',
    // A steer-needed or failed activity in the pane: why it is owed, and why it
    // is review-only (designer P0-2, the PM's must-hold).
    DETAIL_OWED_REASON: 'construction-detail-owed-reason',
    DETAIL_REVIEW_ONLY_NOTE: 'construction-detail-review-only-note',
    // The lens shell (Stage B): ONE route, three lenses over one dataset, a
    // shared toolbar whose state survives a lens switch, and a persistent
    // detail slot. Replaces the Tracker/Interventions/Artifacts tab bar.
    LENS_TOOLBAR: 'construction-lens-toolbar',
    lensButton: (lens: string) => `construction-lens-${lens}`,
    LENS_TASKS_COUNT: 'construction-lens-tasks-count',
    LENS_SEARCH: 'construction-lens-search',
    LENS_SCOPE: 'construction-lens-scope',
    // The TASKS lens's static order label, in place of the Sort menu (designer P1-1).
    LENS_SORT_RANKED: 'construction-lens-sort-ranked',
    LENS_KIND: 'construction-lens-kind',
    LENS_LAYER: 'construction-lens-layer',
    LENS_SORT: 'construction-lens-sort',
    LENS_CONTENT: 'construction-lens-content',
    LENS_DETAIL: 'construction-lens-detail',
    // Navigability (Stage B Task 11): the 528-row tree gets no "expand all" —
    // only a targeted expand to whatever is in flight right now — and an
    // explicit audit toggle that treats reconstructed/synthesized evidence as
    // absent, so a reviewer can see what the surface would show if the founder's
    // 2026-09-09 ruling had never widened the backfill.
    LENS_EXPAND_TO_PHASE: 'construction-lens-expand-to-phase',
    /** The "Observed only" evidence toggle (was "Hide synthesized", designer P1-11). */
    LENS_OBSERVED_ONLY: 'construction-lens-observed-only',
    /** Expand + Observed only: ONE no-wrap group, so they wrap together (designer). */
    LENS_TOOLBAR_TOGGLES: 'construction-lens-toolbar-toggles',
    // The provenance mark carried on a SEARCH-MATCHED task row itself (in
    // addition to the ancestor reveal + the group headers' own badge). A task
    // row read in isolation still asserts a state; a match that scrolls one
    // into view must not be the one case where that assertion has no visible
    // provenance context beside it.
    searchMatchProvenance: (nodeId: string) => `construction-search-match-provenance-${nodeId}`,
    // The LIST lens's three-tier tree (Stage B Task 6): activity › lifecycle
    // phase › Figure A-1 task. One id per rendered row, keyed by the tree's own
    // node id (`<activityId>`, `<activityId>::<phase>`, `<activityId>::<phase>::<task>`).
    LIST_TREE: 'construction-list-tree',
    /** The column header above the tier-1 rows (float … state). */
    LIST_HEADER: 'construction-list-header',
    LIST_EMPTY: 'construction-list-empty',
    /** The blank room a deep link adds below the list to centre its row (designer N1). */
    LIST_RUNWAY: 'construction-list-runway',
    listRow: (nodeId: string) => `construction-list-row-${nodeId}`,
    listAttempts: (nodeId: string) => `construction-list-attempts-${nodeId}`,
    // The provenance axis (Stage B Task 7) — orthogonal to state. The rail rides
    // every tier; the `≈ RECONSTRUCTED` badge rides GROUP headers only (tier 1
    // and tier 2), so a screen of task rows never fills with chips.
    PROVENANCE_RAIL: 'construction-provenance-rail',
    PROVENANCE_BADGE: 'construction-provenance-badge',
    // The shared detail pane (Stage B Task 4) — one header/body/action-bar
    // surface behind all three lenses. Laid out BESIDE the content at
    // >=1200px; below that it degrades to DETAIL_DRAWER, a non-modal (persistent)
    // Drawer over the right edge, rendered by DetailPane.tsx itself.
    DETAIL_PANE: 'construction-detail-pane',
    DETAIL_DRAWER: 'construction-detail-drawer',
    DETAIL_COLLAPSE_TOGGLE: 'construction-detail-collapse-toggle',
    DETAIL_RESIZE_HANDLE: 'construction-detail-resize-handle',
    DETAIL_CLOSE: 'construction-detail-close',
    DETAIL_BREADCRUMB: 'construction-detail-breadcrumb',
    DETAIL_STATE_CHIP: 'construction-detail-state-chip',
    DETAIL_PROVENANCE_CHIP: 'construction-detail-provenance-chip',
    /** "Observed only"'s hidden-count chip, BESIDE the grade chip (fix-C review). */
    DETAIL_OBSERVED_ONLY_CHIP: 'construction-detail-observed-only-chip',
    DETAIL_ATTEMPT_SELECT: 'construction-detail-attempt-select',
    DETAIL_EXIT_CRITERION: 'construction-detail-exit-criterion',
    DETAIL_BODY: 'construction-detail-body',
    DETAIL_ACTION_BAR: 'construction-detail-action-bar',
    detailAction: (id: string) => `construction-detail-action-${id}`,
    // The four bodies that fill the pane's one body slot (Stage B Tasks 8-10),
    // plus the by-design-ABSENT sibling of the unknown one — a phase the
    // activity's profile does not carry is not a gap in the data, and the two
    // never share a body or an id.
    DETAIL_BODY_UNKNOWN: 'construction-detail-body-unknown',
    DETAIL_BODY_ABSENT: 'construction-detail-body-absent',
    DETAIL_BODY_EPISODES: 'construction-detail-body-episodes',
    DETAIL_BODY_REVIEW: 'construction-detail-body-review',
    DETAIL_BODY_ARTIFACT: 'construction-detail-body-artifact',
    // Provenance IN THE PANE (Stage B Task 8, founder ruling 2026-09-09). The
    // list stamps a reconstructed group with `≈ RECONSTRUCTED`; the pane is
    // where a reader goes to CHECK one, so it quotes the basis in the open and
    // states the evidence pointer — including its absence, which is the state of
    // six of the ten task rows on every widened activity.
    DETAIL_PROVENANCE_NOTE: 'construction-detail-provenance-note',
    DETAIL_PROVENANCE_BASIS: 'construction-detail-provenance-basis',
    /** The condensed note's "Basis and evidence" disclosure (its <summary>). */
    DETAIL_PROVENANCE_DISCLOSURE: 'construction-detail-provenance-disclosure',
    DETAIL_EVIDENCE: 'construction-detail-evidence',
    // The episode body's honesty caption (Task 9): episodes are activity-level
    // unless an episode's TargetRef is literally the selected attempt key.
    DETAIL_EPISODE_CAPTION: 'construction-detail-episode-caption',
    DETAIL_SUBAGENT_GANTT: 'construction-detail-subagent-gantt',
    // The review body's verdict block (Task 10). A verdict RECONSTRUCTED from a
    // produced-record note carries its own stamp and is never presented as a
    // structured verdict — awaitPhaseDecision drops sig.Feedback, so no
    // structured verdict exists to present.
    DETAIL_VERDICT: 'construction-detail-verdict',
    DETAIL_VERDICT_STAMP: 'construction-detail-verdict-stamp',
    // The ARTIFACT FRAME (designer renderers-placement §1): every committed
    // artifact in the pane mounts in one frame whose ROLE label (UNDER REVIEW /
    // COMMITTED NOW / REFERENCE) and SOURCE line say what it is. It never carries
    // the provenance hatch — that belongs to the attempt, above it.
    ARTIFACT_FRAME: 'construction-artifact-frame',
    ARTIFACT_ROLE: 'construction-artifact-role',
    ARTIFACT_SOURCE: 'construction-artifact-source',
    ARTIFACT_FOCUS: 'construction-artifact-focus',
    /** The one sentence between a reconstructed attempt's note and the frame. */
    ARTIFACT_RECONSTRUCTED_NOTE: 'construction-artifact-reconstructed-note',
    /** A not-started / unknown selection showing an artifact: its state, in one line. */
    ARTIFACT_STATE_LINE: 'construction-artifact-state-line',
    /** The unknown body's briefing, collapsed under the artifact. */
    ARTIFACT_ABOUT_TASK: 'construction-artifact-about-task',
    CONTRACT_SUMMARY: 'construction-contract-summary',
    CONTRACT_SUMMARY_OPEN: 'construction-contract-summary-open',
    CONTRACT_REFERENCE: 'construction-contract-reference',
    CONTRACT_REFERENCE_OPEN: 'construction-contract-reference-open',
    /** A missing contract (a real gap) versus none by design — two ids, two sentences. */
    CONTRACT_GAP: 'construction-contract-gap',
    CONTRACT_BY_DESIGN: 'construction-contract-by-design',
    CONTRACT_UNRESOLVED: 'construction-contract-unresolved',
    WHO_REACHES_IT: 'construction-who-reaches-it',
    CODE_REVIEW_COMMIT: 'construction-code-review-commit',
    SRS_UNREADABLE: 'construction-srs-unreadable',
    COMPONENT_TEST_PLAN: 'construction-component-test-plan',
    COMPONENT_TEST_PLAN_EMPTY: 'construction-component-test-plan-empty',
    TEST_COVERAGE: 'construction-test-coverage',
    TEST_COVERAGE_DIRECT: 'construction-test-coverage-direct',
    coverageReachedRow: (scenarioId: string) => `construction-test-coverage-reached-${scenarioId}`,
    USE_CASE_FLOWS_LINK: 'construction-use-case-flows-link',
    // The FOCUS view (designer §3): the artifact full-viewport over the console,
    // the invariant header and action bar in a rail beside it, the lens mounted
    // underneath. Driven by `&focus=1`.
    FOCUS_VIEW: 'construction-focus-view',
    FOCUS_CLOSE: 'construction-focus-close',
    /** The focus region's heading — where focus lands on entry (polish 4). */
    FOCUS_HEADING: 'construction-focus-heading',
    /** The rail's own content under the header: the note, the sentence, the verdict. */
    FOCUS_RAIL: 'construction-focus-rail',
    /** Collapse / show the focus view's side panel (per viewer, remembered). */
    FOCUS_RAIL_TOGGLE: 'construction-focus-rail-toggle',
    /** The verdict as one chip in the focus header while the side panel is collapsed. */
    FOCUS_VERDICT_CHIP: 'construction-focus-verdict-chip',
    /** The pane's body while the focus view is open: unmounted, one line instead. */
    FOCUS_PLACEHOLDER: 'construction-focus-placeholder',
    /** Code Review on a reconstructed (or unreviewed) attempt: one line, no CODE frame. */
    CODE_REVIEW_NO_VIEW: 'construction-code-review-no-view',
    /** The narrow scenario browser's case dropdown (more than 3 cases). */
    CASE_PICKER: 'construction-case-picker',
  },
  // The GIT-FORWARD per-activity row cluster (U-SPA-GIT). The shared chrome the
  // construction tracker (and future CR/operations surfaces) render per
  // git-backed activity, keyed by ActivityID via gitFor(...).
  Git: {
    ROW_META: 'git-row-meta',
    PR_LINK: 'git-pr-link',
    BRANCH: 'git-branch',
    MERGED: 'git-merged',
    CR_LABEL: 'git-cr-label',
    ARCH_APPROVED: 'git-arch-approved',
    ciStatus: (status: string) => `git-ci-${status}`,
  },
  ServiceContract: {
    ROOT: 'service-contract-view',
    TAB_CODE: 'service-contract-tab-code',
    TAB_COMPONENT: 'service-contract-tab-component',
    TAB_DYNAMIC: 'service-contract-tab-dynamic',
    TAB_FACETS: 'service-contract-tab-facets',
    REVISION_HISTORY: 'service-contract-revision-history',
    revisionRow: (rev: string) => `service-contract-revision-${rev}`,
    /** The Component tab's relationships view (PerspectiveFlow over system.relationships). */
    COMPONENT_FLOW: 'service-contract-component-flow',
    /** The status chip — rendered only when a contract records a status. */
    STATUS_CHIP: 'service-contract-status-chip',
    FACETS_EMPTY: 'service-contract-facets-empty',
    // The Code tab below 900px (designer check B1): every op as an HTML row, 12px
    // mono and wrapping, each expanding inline into its request / response / error
    // tables. The canvas draws in the focus view only.
    SIGNATURE_LIST: 'service-contract-signature-list',
    opRow: (index: number) => `service-contract-op-${String(index)}`,
    OP_SIGNATURE: 'service-contract-op-signature',
    OP_STRUCTS: 'service-contract-op-structs',
    /** "Open diagram in focus view" at the top of the list. */
    OPEN_FOCUS: 'service-contract-open-focus',
    /** The focus view's list form, when the window leaves no room for the canvas. */
    CANVAS_NEEDS_ROOM: 'service-contract-canvas-needs-room',
    /** The needs-room note's "collapse the side panel" — the canvas then has room. */
    CANVAS_NEEDS_ROOM_COLLAPSE: 'service-contract-canvas-needs-room-collapse',
    /** The code canvas's one caption, above it. */
    CODE_CANVAS_CAPTION: 'service-contract-code-canvas-caption',
    /** The code canvas's «interface» node. */
    CODE_INTERFACE_NODE: 'service-contract-code-interface',
    /** One field row of a real struct's table in the list. */
    FIELD_ROW: 'service-contract-field-row',
    /** The canvas itself (ContractCodeFlow), drawn in the focus view only. */
    CODE_CANVAS: 'service-contract-code-canvas',
    /** The utilities a component may reach, as one line in place of their nodes. */
    UTILITIES_LINE: 'service-contract-utilities-line',
    /** One struct card of an expanded op on the canvas (data-struct, data-role). */
    STRUCT_CARD: 'service-contract-struct-card',
    /** The code canvas's own frame (the drawing area, under its legend). */
    CODE_CANVAS_FRAME: 'service-contract-code-canvas-frame',
    /** A real struct's name — a header over its fields (list and canvas). */
    STRUCT_NAME: 'service-contract-struct-name',
    /** A primitive or alias param: one row, `tickID  string` (list and canvas). */
    PARAM_ROW: 'service-contract-param-row',
    /** The pane's Component tab as text: one row per caller or callee. */
    neighbourRow: (componentId: string) => `service-contract-neighbour-${componentId}`,
  },
  Operations: {
    ROOT: 'operations-console',
    TAB_STATUS: 'operations-tab-status',
    TAB_DEPLOYMENTS: 'operations-tab-deployments',
    TAB_SCALING: 'operations-tab-scaling',
    TAB_INTERVENTIONS: 'operations-tab-interventions',
    APP_SELECTOR: 'operations-app-selector',
    appOption: (id: string) => `operations-app-option-${id}`,
    STATUS_TAB: 'operations-status-tab',
    DEPLOYMENTS_TAB: 'operations-deployments-tab',
    SCALING_TAB: 'operations-scaling-tab',
    INTERVENTIONS_TAB: 'operations-interventions-tab',
    AWAITING: 'operations-awaiting',
    DEPLOY_BUTTON: 'operations-deploy',
    SCALE_BUTTON: 'operations-scale',
    AUTOSCALER_POLICY_BUTTON: 'operations-autoscaler-policy',
    WITHDRAW_BUTTON: 'operations-withdraw',
    BILLING_LINK: 'operations-billing-link',
    CAPABILITIES_UNREACHABLE: 'operations-capabilities-unreachable',
    CAPABILITIES_RETRY: 'operations-capabilities-retry',
  },
  ChangeRequests: {
    ROOT: 'change-requests-screen',
    CLOSE: 'change-requests-close',
    INTAKE_OPEN: 'change-requests-intake-open',
    INTAKE_TITLE: 'change-requests-intake-title',
    INTAKE_BODY: 'change-requests-intake-body',
    INTAKE_SUBMIT: 'change-requests-intake-submit',
    INTAKE_CANCEL: 'change-requests-intake-cancel',
    EMPTY_STATE: 'change-requests-empty',
    subprojectCard: (id: string) => `change-requests-subproject-${id}`,
  },
  Subproject: {
    ROOT: 'subproject-flow-screen',
    CLOSE: 'subproject-flow-close',
    NOT_READY: 'subproject-flow-not-ready',
    BACK: 'subproject-flow-back',
  },
  Billing: {
    ROOT: 'billing-screen',
    PENDING_STATE: 'billing-pending',
    HOME_LINK: 'billing-home-link',
  },
  Team: {
    ROOT: 'team-screen',
    roleCard: (id: string) => `team-role-card-${id}`,
    CHARTER_DRAWER: 'team-charter-drawer',
    CHARTER_CLOSE: 'team-charter-close',
    TOGGLE_PROMPT: 'team-charter-toggle-prompt',
  },
  SdpReview: {
    ROOT: 'sdp-review',
    GATE: 'sdp-gate',
    COMMIT: 'sdp-commit',
    REJECT_ALL: 'sdp-reject-all',
    REJECT_FEEDBACK: 'sdp-reject-feedback',
    REJECT_SUBMIT: 'sdp-reject-submit',
    optionCard: (optionId: string) => `sdp-option-${optionId}`,
  },
  Gate: {
    STAGE_CHIP: 'gate-stage-chip',
    REQUEST_DRAFT_BUTTON: 'gate-request-draft-button',
    DRAFT_DISPLAY: 'gate-draft-display',
    FINDINGS_LIST: 'gate-findings-list',
    APPROVE_BUTTON: 'gate-approve-button',
    REJECT_BUTTON: 'gate-reject-button',
    WITHDRAW_BUTTON: 'gate-withdraw-button',
    FEEDBACK_INPUT: 'gate-feedback-input',
  },
  Common: {
    ERROR_ALERT: 'error-alert',
    LOADING: 'loading-indicator',
  },
  // The SP1 capture-seam episodes panel (Task 10) — mounted per design-artifact
  // page (Phase 1 + Phase 2) and per construction activity. Base ids match the
  // task brief exactly; per-row/per-episode ids append the id, following the
  // repo's row-testid convention (listRow / graphLane / roleCard, ...).
  Episodes: {
    PANEL: 'episodes-panel',
    episodeRow: (episodeId: string) => `episodes-row-${episodeId}`,
    TIMELINE: 'episode-timeline',
    LINEAGE_TREE: 'episode-lineage-tree',
    EXPORT_MENU_BUTTON: 'episodes-export-menu',
    EXPORT_JSON: 'episode-export-json',
    EXPORT_CSV: 'episode-export-csv',
    outcomeChip: (episodeId: string) => `episode-outcome-chip-${episodeId}`,
    TIMELINE_FILTER: 'episode-timeline-filter',
    /** The panel header's count, plus the optional scope caption after it. */
    HEADER_COUNT: 'episodes-header-count',
  },
  // The preview build's own chrome (src/previewShell/, design-renderer-data.md
  // §2′.1): the loud banner a fixture miss or a blocked request raises, and the
  // honest error page for a ?screen=&state= the build does not carry.
  Preview: {
    ALARM: 'preview-alarm',
    ERROR_PAGE: 'preview-error-page',
  },
} as const;
