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
    // Compact caveat chip on the Standard Check header when upstream slots drifted.
    STANDARD_CHECK_CAVEAT: 'standard-check-caveat',
    ARTIFACT_RENDER: 'artifact-render',
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
    // The "All · N" reset chip.
    CHIP_ALL: 'glossary-chip-all',
    // A category filter chip, keyed by its Four-Questions BASE label
    // (Who/What/How/Where/Uncategorized — refined How-* sub-labels roll up).
    chip: (base: string) => `glossary-chip-${base}`,
    // A category section header, keyed by its (possibly refined) display label.
    section: (label: string) => `glossary-section-${label}`,
    // The "no terms match" filtered-empty state.
    EMPTY: 'glossary-empty',
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
  },
  Architecture: {
    VIEW_SWITCH: 'arch-view-switch',
    VIEW_STATIC: 'static',
    VIEW_DYNAMIC: 'dynamic',
    VIEW_PERSPECTIVE: 'perspective',
    DYNAMIC_PICKER: 'arch-dynamic-picker',
    PERSPECTIVE_PICKER: 'arch-perspective-picker',
  },
  Deployment: {
    PROFILE_SWITCH: 'deploy-profile-switch',
  },
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
    // Toggle that reveals/collapses the carried-over PENDING · NOT SENT drafts.
    PENDING_DISCLOSURE: 'chat-pending-disclosure',
    commentAnchor: (n: number) => `comment-anchor-${String(n)}`,
    // A durable review-ledger thread entry (server), keyed by its ledger id.
    threadEntry: (id: string) => `thread-entry-${id}`,
    // Per-entry lifecycle actions: waive an open entry / reopen an addressed one.
    threadWaive: (id: string) => `thread-waive-${id}`,
    threadReopen: (id: string) => `thread-reopen-${id}`,
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
    // The shared CommentableList primitive (role=listbox) that makes any itemized
    // artifact keyboard-navigable and item-granular-commentable.
    LIST: 'comment-list',
    // A CommentableList row (role=option), keyed by the caller-supplied item key.
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
    TAB_TRACKER: 'construction-tab-tracker',
    TAB_INTERVENTIONS: 'construction-tab-interventions',
    TAB_ARTIFACTS: 'construction-tab-artifacts',
    SUMMARY_STRIP: 'construction-summary-strip',
    TRACKER: 'construction-tracker',
    AWAITING: 'construction-awaiting',
    ACTIVE_DETAIL: 'construction-active-detail',
    ROLE_LINE: 'construction-role-line',
    INTERVENTIONS: 'construction-interventions',
    ARTIFACTS: 'construction-artifacts',
    artifactRow: (id: string) => `construction-artifact-row-${id}`,
    SYSTEM_TEST_VIEW: 'construction-system-test-view',
    TEST_PLAN_VIEW: 'construction-test-plan-view',
    FRONTEND_VIEW: 'construction-frontend-view',
    FRONTEND_PREVIEW_FRAME: 'construction-frontend-preview-frame',
    SCENARIO_PICKER: 'construction-scenario-picker',
    caseChip: (caseId: string) => `construction-case-chip-${caseId}`,
    BEGIN_BUTTON: 'construction-begin',
    PAUSE_BUTTON: 'construction-pause',
    PAUSE_REASON: 'construction-pause-reason',
    PAUSE_CONFIRM: 'construction-pause-confirm',
    OVERRIDE_BUTTON: 'construction-override',
    overrideKind: (kind: string) => `construction-override-${kind}`,
    OVERRIDE_NOTES: 'construction-override-notes',
    OVERRIDE_CONFIRM: 'construction-override-confirm',
    trackerNode: (id: string) => `construction-track-node-${id}`,
    ACTIVITY_LIFECYCLE_PANEL: 'construction-activity-lifecycle-panel',
    POLICY_PANEL: 'construction-policy-panel',
    policyRowToggle: (kind: string) => `construction-policy-toggle-${kind}`,
    PHASE_GATE_PANEL: 'construction-phase-gate-panel',
    PHASE_GATE_APPROVE: 'construction-phase-gate-approve',
    PHASE_GATE_SENDBACK: 'construction-phase-gate-sendback',
    // Intervention queue + drawer test IDs (U-SPA-INTERVENTION)
    INTERVENTION_QUEUE_COUNT: 'construction-intervention-queue-count',
    interventionQueueCard: (activityId: string) => `construction-intervention-card-${activityId}`,
    interventionReviewButton: (activityId: string) =>
      `construction-intervention-review-${activityId}`,
    INTERVENTION_DRAWER: 'construction-intervention-drawer',
    INTERVENTION_DRAWER_CLOSE: 'construction-intervention-drawer-close',
    INTERVENTION_OPERATOR_BAR: 'construction-intervention-operator-bar',
    interventionSteerButton: (kind: string) => `construction-intervention-steer-${kind}`,
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
    COMPONENT_FLOW: 'service-contract-component-flow',
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
} as const;
