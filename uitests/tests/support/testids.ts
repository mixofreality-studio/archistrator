/**
 * Stable `data-testid`s the SPA exposes, flattened here for the spec files.
 *
 * SHARING BEATS MIRRORING (step-9 cleanup, 2026-07-10): this used to hand-copy
 * every id as its own kebab-case string literal, justified as an "anti-cheat
 * boundary" — the founder's later ruling on the appgen codegen sweep overturned
 * that: a renamed testid should fail ONE import resolution, not silently drift
 * until the matching assertion happens to break. TESTID now IMPORTS its values
 * from the SPA's own published `UI_IDENTIFIERS` map
 * (`webApp/src/utilities/constants/UIIdentifiers.ts`) — a pure string-literal
 * data module with no React/component/hook/business-logic surface, so this
 * package still links no webApp BEHAVIOR, only its one published id table.
 * TESTID keeps its OWN flat, camelCase shape (distinct from UI_IDENTIFIERS'
 * nested namespaces) so every existing `TESTID.x` call site across the specs
 * is unchanged — only the right-hand-side values are now derived, not retyped.
 * A handful of view-switch VALUES with no UI_IDENTIFIERS counterpart
 * (deployProfileCloud/Local/Test) remain uitests-local literals — genuinely
 * this package's own concern, not a testid.
 *
 * un-cheatable-ness is unaffected: a spec still selects DOM elements only by
 * `page.getByTestId(TESTID.x)` / role / label (see eslint.config.js's
 * no-inline-testid rule) — this file is still the single allowed source of
 * testid strings, it just resolves them by import instead of by hand-copy.
 */
import { UI_IDENTIFIERS } from '../../../webApp/src/utilities/constants/UIIdentifiers.js';
import { PHASE1_ORDER } from '../../../webApp/src/contracts/methodMetadata.js';
import type { PhaseId } from '../../../webApp/src/contracts/adapters.js';

export const TESTID = {
  // Session gate / common
  loading: UI_IDENTIFIERS.Common.LOADING,
  errorAlert: UI_IDENTIFIERS.Common.ERROR_ALERT,

  // The preview build's own chrome (webApp/src/previewShell/): the loud miss /
  // blocked-request banner and the unknown-state error page.
  previewAlarm: UI_IDENTIFIERS.Preview.ALARM,
  previewErrorPage: UI_IDENTIFIERS.Preview.ERROR_PAGE,

  // Projects landing (route `/`)
  projectsLandingScreen: UI_IDENTIFIERS.ProjectsLanding.SCREEN,
  projectsGrid: UI_IDENTIFIERS.ProjectsLanding.GRID,
  emptyState: UI_IDENTIFIERS.ProjectsLanding.EMPTY_STATE,
  newProjectCard: UI_IDENTIFIERS.ProjectsLanding.NEW_PROJECT_CARD,
  newProjectButton: UI_IDENTIFIERS.ProjectsLanding.NEW_PROJECT_BUTTON,
  createProjectDialog: UI_IDENTIFIERS.ProjectsLanding.CREATE_PROJECT_DIALOG,
  newProjectNameInput: UI_IDENTIFIERS.ProjectsLanding.NEW_PROJECT_NAME_INPUT,
  createProjectSubmit: UI_IDENTIFIERS.ProjectsLanding.CREATE_PROJECT_SUBMIT,
  createProjectCancel: UI_IDENTIFIERS.ProjectsLanding.CREATE_PROJECT_CANCEL,
  createProjectPrereqs: UI_IDENTIFIERS.ProjectsLanding.CREATE_PROJECT_PREREQS,
  projectCard: UI_IDENTIFIERS.ProjectsLanding.projectCard,

  // Shell
  projectMenu: UI_IDENTIFIERS.Shell.PROJECT_MENU,
  projectMenuNew: UI_IDENTIFIERS.Shell.PROJECT_MENU_NEW,
  projectMenuItem: UI_IDENTIFIERS.Shell.projectMenuItem,
  teamNav: UI_IDENTIFIERS.Shell.TEAM_NAV,

  // Team roster (route `/project/$projectId/team`) — static Method-roles roster.
  teamScreen: UI_IDENTIFIERS.Team.ROOT,
  teamRoleCard: UI_IDENTIFIERS.Team.roleCard,
  teamCharterDrawer: UI_IDENTIFIERS.Team.CHARTER_DRAWER,
  teamCharterClose: UI_IDENTIFIERS.Team.CHARTER_CLOSE,
  teamCharterTogglePrompt: UI_IDENTIFIERS.Team.TOGGLE_PROMPT,

  // Home base (route `/project/$projectId/home`)
  homeBaseScreen: UI_IDENTIFIERS.HomeBase.SCREEN,
  resumeDesign: UI_IDENTIFIERS.HomeBase.RESUME_DESIGN,
  artifactToc: UI_IDENTIFIERS.HomeBase.ARTIFACT_TOC,
  economicsStrip: UI_IDENTIFIERS.HomeBase.ECONOMICS_STRIP,
  // NOTE: phase ids are the typed PhaseId values — systemDesign / projectDesign /
  // construction — NOT the route slug. The `phase-card-system` shorthand in the
  // task brief resolves to `phase-card-systemDesign` on the wire.
  phaseCard: UI_IDENTIFIERS.HomeBase.phaseCard,
  tocRow: UI_IDENTIFIERS.HomeBase.tocRow,

  // Design experience (route `/project/$projectId/design/system`)
  designExperience: UI_IDENTIFIERS.DesignExperience.ROOT,
  designClose: UI_IDENTIFIERS.DesignExperience.CLOSE,
  slimSpine: UI_IDENTIFIERS.DesignExperience.SLIM_SPINE,
  spineStep: UI_IDENTIFIERS.DesignExperience.spineStep,
  requestDraft: UI_IDENTIFIERS.DesignExperience.REQUEST_DRAFT,
  researchInput: UI_IDENTIFIERS.DesignExperience.RESEARCH_INPUT,
  researchInputTitle: UI_IDENTIFIERS.DesignExperience.RESEARCH_INPUT_TITLE,
  researchInputText: UI_IDENTIFIERS.DesignExperience.RESEARCH_INPUT_TEXT,
  researchInputSubmit: UI_IDENTIFIERS.DesignExperience.RESEARCH_INPUT_SUBMIT,
  generatingScene: UI_IDENTIFIERS.DesignExperience.GENERATING_SCENE,
  ciJobNotice: UI_IDENTIFIERS.DesignExperience.CI_JOB_NOTICE,
  ciJobLink: UI_IDENTIFIERS.DesignExperience.CI_JOB_LINK,
  // The full-width framing banner shown ONLY while an artifact is drafted/
  // awaitingReview (never on a committed first paint — UX-P1-4/P2-10) and its
  // committed-state replacement: a compact (?) info button + popover.
  artifactIntro: UI_IDENTIFIERS.DesignExperience.ARTIFACT_INTRO,
  artifactInfo: UI_IDENTIFIERS.DesignExperience.ARTIFACT_INFO,
  artifactRender: UI_IDENTIFIERS.DesignExperience.ARTIFACT_RENDER,
  draftFailed: UI_IDENTIFIERS.DesignExperience.DRAFT_FAILED,
  draftFailureReason: UI_IDENTIFIERS.DesignExperience.DRAFT_FAILURE_REASON,
  retryDraft: UI_IDENTIFIERS.DesignExperience.RETRY_DRAFT,
  withdrawDraft: UI_IDENTIFIERS.DesignExperience.WITHDRAW_DRAFT,

  // Glossary artifact — the searchable, filterable reference widget (search +
  // Four-Questions category chips + grouped term list). Chips key by the BASE
  // label (Who/What/How/Where/Uncategorized — refined "How · Activity"-style
  // sub-labels roll up under one How chip); sections keep the refined label.
  glossaryRoot: UI_IDENTIFIERS.Glossary.ROOT,
  glossarySearch: UI_IDENTIFIERS.Glossary.SEARCH,
  glossaryChipAll: UI_IDENTIFIERS.Glossary.CHIP_ALL,
  glossaryChip: UI_IDENTIFIERS.Glossary.chip,
  glossarySection: UI_IDENTIFIERS.Glossary.section,
  glossaryEmpty: UI_IDENTIFIERS.Glossary.EMPTY,

  // CommentableList (the shared item-granular commenting primitive): a row and
  // its per-item "Comment on this item" button, keyed by the caller's item key
  // (the glossary keys rows `${term}-${originalIndex}`).
  commentListItem: UI_IDENTIFIERS.Comments.listItem,
  commentListItemButton: UI_IDENTIFIERS.Comments.listItemComment,

  // Volatility map (volatilities artifact) — compact axes overview + two
  // single-select lane listboxes + side-rail summary/detail + the collapsed
  // rejected-candidates disclosure. chip()/dot() key by the point's index in
  // the flat model items array; rejectedItem() by the `rejected` array index.
  volatilityMap: UI_IDENTIFIERS.VolatilityMap.ROOT,
  volatilityLane: UI_IDENTIFIERS.VolatilityMap.lane,
  volatilityChip: UI_IDENTIFIERS.VolatilityMap.chip,
  volatilityDetail: UI_IDENTIFIERS.VolatilityMap.DETAIL,
  volatilityDetailClose: UI_IDENTIFIERS.VolatilityMap.DETAIL_CLOSE,
  volatilitySummary: UI_IDENTIFIERS.VolatilityMap.SUMMARY,
  volatilityAxes: UI_IDENTIFIERS.VolatilityMap.AXES,
  volatilityDot: UI_IDENTIFIERS.VolatilityMap.dot,
  volatilityRejectedToggle: UI_IDENTIFIERS.VolatilityMap.REJECTED_TOGGLE,
  volatilityRejectedList: UI_IDENTIFIERS.VolatilityMap.REJECTED_LIST,
  volatilityRejectedItem: UI_IDENTIFIERS.VolatilityMap.rejectedItem,

  // Architecture (System artifact) view switcher — static / dynamic / perspective /
  // deployment. The switch VALUES are the bare strings 'static' | 'dynamic' |
  // 'perspective' | 'deployment'.
  archViewSwitch: UI_IDENTIFIERS.Architecture.VIEW_SWITCH,
  archViewStatic: UI_IDENTIFIERS.Architecture.VIEW_STATIC,
  archViewDynamic: UI_IDENTIFIERS.Architecture.VIEW_DYNAMIC,
  archViewPerspective: UI_IDENTIFIERS.Architecture.VIEW_PERSPECTIVE,
  archViewDeployment: UI_IDENTIFIERS.Architecture.VIEW_DEPLOYMENT,
  archDynamicPicker: UI_IDENTIFIERS.Architecture.DYNAMIC_PICKER,
  archPerspectivePicker: UI_IDENTIFIERS.Architecture.PERSPECTIVE_PICKER,
  archDynamicStepPrev: UI_IDENTIFIERS.Architecture.DYNAMIC_STEP_PREV,
  archDynamicStepNext: UI_IDENTIFIERS.Architecture.DYNAMIC_STEP_NEXT,
  // The dynamic lens' walkthrough-driven trace: the walkthrough pane that steers
  // it, and the call-chain caption listing the current step's fragment.
  archDynamicActivityTrace: UI_IDENTIFIERS.Architecture.DYNAMIC_ACTIVITY_TRACE,
  archDynamicFragment: UI_IDENTIFIERS.Architecture.DYNAMIC_FRAGMENT,
  // Per-view CC verdict roll-up beside the dynamic picker, and the FragmentBar's
  // named CC-checks chip (Task 6, call-chain rollout — a read-only count since the
  // Design Health step retired).
  archViewVerdict: UI_IDENTIFIERS.Architecture.VIEW_VERDICT,
  archCcChecksChip: UI_IDENTIFIERS.Architecture.CC_CHECKS_CHIP,

  // Core Use Cases artifact — the grouped (Core / Variations) use-case picker.
  useCasePicker: UI_IDENTIFIERS.UseCaseCarousel.PICKER,
  // Core Use Cases view-mode toggle + walkthrough controls. The current-node id
  // is a stable hook for asserting the per-step "you-are-here" camera focus.
  useCaseViewWalkthrough: UI_IDENTIFIERS.UseCaseCarousel.VIEW_WALKTHROUGH,
  useCaseViewDiagram: UI_IDENTIFIERS.UseCaseCarousel.VIEW_DIAGRAM,
  walkthroughNext: UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_NEXT,
  walkthroughBack: UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_BACK,
  walkthroughRestart: UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_RESTART,
  walkthroughBranch: UI_IDENTIFIERS.UseCaseCarousel.walkthroughBranch,
  walkthroughPathStep: UI_IDENTIFIERS.UseCaseCarousel.walkthroughPathStep,
  walkthroughCurrentNode: UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_CURRENT_NODE,
  // Per-use-case realization roll-up chip, per-step badge, and the current
  // step's compact call list + call-chain join (Task 11).
  useCaseRealizationChip: UI_IDENTIFIERS.UseCaseCarousel.REALIZATION_CHIP,
  useCaseStepBadge: UI_IDENTIFIERS.UseCaseCarousel.STEP_BADGE,
  useCaseStepCalls: UI_IDENTIFIERS.UseCaseCarousel.STEP_CALLS,

  // The Deployment & Operations page (and its `deploy-profile-switch` toggle) is
  // retired. The committed topology now renders as the Architecture step's
  // Deployment lens (archViewSwitch → 'deployment'), which is instance-less: it
  // shows the first committed environment and offers no profile picker, so there is
  // no switcher testid left to publish.

  // Gate panel
  gatePanel: UI_IDENTIFIERS.GatePanel.ROOT,
  gateApprove: UI_IDENTIFIERS.GatePanel.APPROVE,
  gateSendback: UI_IDENTIFIERS.GatePanel.SENDBACK,
  gateWithdraw: UI_IDENTIFIERS.GatePanel.WITHDRAW,
  gateFindings: UI_IDENTIFIERS.GatePanel.FINDINGS,
  // Inline error banner near the gate actions (failed decision — F79/F-QA2-47).
  gateError: UI_IDENTIFIERS.GatePanel.GATE_ERROR,

  // Chat rail (anchored comments)
  chatRail: UI_IDENTIFIERS.Chat.RAIL,
  chatToggle: UI_IDENTIFIERS.Chat.TOGGLE,
  chatInput: UI_IDENTIFIERS.Chat.INPUT,
  chatSend: UI_IDENTIFIERS.Chat.SEND,
  commentAnchor: UI_IDENTIFIERS.Chat.commentAnchor,
  // Invisible probe reflecting the currently-armed comment anchor (data-anchor-*
  // attrs) — how a diagram-node click OR keyboard ('c'/Enter on a focused,
  // labeled node) arming is observed black-box.
  commentArmedAnchor: UI_IDENTIFIERS.Comments.ARMED_ANCHOR,

  // GIT-FORWARD per-activity row cluster (U-SPA-GIT). The construction tracker's
  // active-activity detail renders this when the project read carries a gitRow
  // for the active activity (honest-empty: absent otherwise).
  gitRowMeta: UI_IDENTIFIERS.Git.ROW_META,
  gitPrLink: UI_IDENTIFIERS.Git.PR_LINK,
  gitBranch: UI_IDENTIFIERS.Git.BRANCH,
  gitMerged: UI_IDENTIFIERS.Git.MERGED,
  gitCrLabel: UI_IDENTIFIERS.Git.CR_LABEL,
  gitArchApproved: UI_IDENTIFIERS.Git.ARCH_APPROVED,
  gitCiStatus: UI_IDENTIFIERS.Git.ciStatus,

  // Construction console (route `/project/$projectId/construction`). The
  // Tracker/Interventions/Artifacts tab bar (and its per-tab root/row testids)
  // retired with Task 13 — the lens shell mounts directly, so there is no tab
  // bar left to select and no separate Artifacts-tab activity row; the same
  // activity is reached via the LIST lens's tree (constructionListRow) and its
  // artifact is read from the detail pane (constructionDetailBodyArtifact).
  constructionSystemTestView: UI_IDENTIFIERS.Construction.SYSTEM_TEST_VIEW,
  constructionTestPlanView: UI_IDENTIFIERS.Construction.TEST_PLAN_VIEW,
  // Begin/Resume and the confirm step in front of it, which names what a Begin
  // would dispatch before anything is (fix round A, designer P0-3).
  constructionBegin: UI_IDENTIFIERS.Construction.BEGIN_BUTTON,
  /** The loud alert a failed Begin dispatch raises (fix-B review M3). */
  constructionBeginError: UI_IDENTIFIERS.Construction.BEGIN_ERROR,
  constructionBeginNote: UI_IDENTIFIERS.Construction.BEGIN_NOTE,
  constructionBeginConfirm: UI_IDENTIFIERS.Construction.BEGIN_CONFIRM_DIALOG,
  constructionBeginConfirmCancel: UI_IDENTIFIERS.Construction.BEGIN_CONFIRM_CANCEL,
  constructionBeginConfirmDispatch: UI_IDENTIFIERS.Construction.BEGIN_CONFIRM_DISPATCH,
  constructionBeginCandidate: UI_IDENTIFIERS.Construction.beginConfirmCandidate,
  constructionCaseChip: UI_IDENTIFIERS.Construction.caseChip,
  constructionActiveCase: UI_IDENTIFIERS.Construction.ACTIVE_CASE,
  constructionCaseExpect: UI_IDENTIFIERS.Construction.CASE_EXPECT,
  // Stage D — the GRAPH lens (webApp/src/components/construction/graph): the
  // architecture layer by layer, each card carrying its activity's lifecycle.
  constructionGraphCanvas: UI_IDENTIFIERS.Construction.GRAPH_CANVAS,
  constructionGraphRibbon: UI_IDENTIFIERS.Construction.GRAPH_RIBBON,
  constructionGraphMilestone: UI_IDENTIFIERS.Construction.graphMilestone,
  constructionGraphKey: UI_IDENTIFIERS.Construction.GRAPH_KEY,
  constructionGraphKeyButton: UI_IDENTIFIERS.Construction.GRAPH_KEY_BUTTON,
  constructionGraphFilterStatus: UI_IDENTIFIERS.Construction.GRAPH_FILTER_STATUS,
  constructionGraphRowGutter: UI_IDENTIFIERS.Construction.GRAPH_ROW_GUTTER,
  constructionGraphRowLabel: UI_IDENTIFIERS.Construction.graphRowLabel,
  constructionGraphClearFilters: UI_IDENTIFIERS.Construction.GRAPH_CLEAR_FILTERS,
  constructionGraphLayerCheck: UI_IDENTIFIERS.Construction.GRAPH_LAYER_CHECK,
  constructionGraphCard: UI_IDENTIFIERS.Construction.graphCard,
  constructionGraphLane: UI_IDENTIFIERS.Construction.graphLane,
  constructionGraphSegment: UI_IDENTIFIERS.Construction.graphSegment,
  constructionGraphHoverCard: UI_IDENTIFIERS.Construction.GRAPH_HOVER_CARD,
  constructionGraphHoverLane: UI_IDENTIFIERS.Construction.graphHoverLane,
  constructionGraphLaneFloat: UI_IDENTIFIERS.Construction.graphLaneFloat,
  constructionGraphScheduleCaption: UI_IDENTIFIERS.Construction.GRAPH_SCHEDULE_CAPTION,
  constructionGraphM0Hover: UI_IDENTIFIERS.Construction.GRAPH_M0_HOVER,
  constructionGraphM0OpenSdp: UI_IDENTIFIERS.Construction.GRAPH_M0_OPEN_SDP,
  constructionGraphM0Popover: UI_IDENTIFIERS.Construction.GRAPH_M0_POPOVER,
  constructionGraphKeySwatch: UI_IDENTIFIERS.Construction.graphKeySwatch,
  // The Stage-B lens shell's toolbar (ConstructionShell.tsx) — its right end
  // is exactly what the old overlay Drawer's backdrop used to cover.
  constructionLensToolbar: UI_IDENTIFIERS.Construction.LENS_TOOLBAR,
  constructionLensButton: UI_IDENTIFIERS.Construction.lensButton,
  constructionListRunway: UI_IDENTIFIERS.Construction.LIST_RUNWAY,
  constructionLensKind: UI_IDENTIFIERS.Construction.LENS_KIND,
  constructionLensScope: UI_IDENTIFIERS.Construction.LENS_SCOPE,
  constructionLensSortRanked: UI_IDENTIFIERS.Construction.LENS_SORT_RANKED,
  constructionLensSort: UI_IDENTIFIERS.Construction.LENS_SORT,
  // Navigability (Stage B Task 11) — search box and the per-row provenance
  // mark a search reveal stamps on a reconstructed task match (see
  // needsInlineProvenanceMark in activityScope.ts).
  constructionLensSearch: UI_IDENTIFIERS.Construction.LENS_SEARCH,
  constructionSearchMatchProvenance: UI_IDENTIFIERS.Construction.searchMatchProvenance,
  // The spelled-out "≈ RECONSTRUCTED" badge ProvenanceGroupStamp puts on a
  // tier-1 activity header and a tier-2 phase header (never on a task row).
  constructionProvenanceBadge: UI_IDENTIFIERS.Construction.PROVENANCE_BADGE,
  // The LIST lens's three-tier tree (Stage B Task 6). It replaced the CPM graph
  // as the LIST lens's body — the graph returns under the GRAPH lens in Stage D.
  constructionListTree: UI_IDENTIFIERS.Construction.LIST_TREE,
  constructionListHeader: UI_IDENTIFIERS.Construction.LIST_HEADER,
  constructionLensObservedOnly: UI_IDENTIFIERS.Construction.LENS_OBSERVED_ONLY,
  constructionLensExpandToPhase: UI_IDENTIFIERS.Construction.LENS_EXPAND_TO_PHASE,
  constructionLensToolbarToggles: UI_IDENTIFIERS.Construction.LENS_TOOLBAR_TOGGLES,
  constructionDetailCollapseToggle: UI_IDENTIFIERS.Construction.DETAIL_COLLAPSE_TOGGLE,
  constructionListTitleCell: UI_IDENTIFIERS.Construction.listTitleCell,
  constructionListPendingLine: UI_IDENTIFIERS.Construction.listPendingLine,
  constructionGraphHoverPending: UI_IDENTIFIERS.Construction.graphHoverPending,
  constructionDetailPendingResume: UI_IDENTIFIERS.Construction.DETAIL_PENDING_RESUME,
  constructionListIdCell: UI_IDENTIFIERS.Construction.listIdCell,
  constructionListTaskBookKey: UI_IDENTIFIERS.Construction.listTaskBookKey,
  constructionListRow: UI_IDENTIFIERS.Construction.listRow,
  // The LIST's empty state and its "Clear filters" (fix round A, P1-9).
  constructionListEmpty: UI_IDENTIFIERS.Construction.LIST_EMPTY,
  constructionListClearFilters: UI_IDENTIFIERS.Construction.LIST_CLEAR_FILTERS,
  // The pane header's count line and exit line (fix round A, P1-6).
  constructionDetailSelectionSummary: UI_IDENTIFIERS.Construction.DETAIL_SELECTION_SUMMARY,
  constructionDetailExitCriterion: UI_IDENTIFIERS.Construction.DETAIL_EXIT_CRITERION,
  // The shared detail pane (Stage B Task 4) that replaced the overlay Drawer
  // above as the console's mounted detail surface — beside content at
  // >=1200px, DETAIL_DRAWER below that (same overlay mechanism, kept).
  constructionDetailPane: UI_IDENTIFIERS.Construction.DETAIL_PANE,
  /** The shell's wrapper AROUND the pane — a flex ITEM of the content row, and
   *  therefore the box the row's `align-items: stretch` actually acts on. */
  constructionLensDetail: UI_IDENTIFIERS.Construction.LENS_DETAIL,
  constructionDetailDrawer: UI_IDENTIFIERS.Construction.DETAIL_DRAWER,
  constructionDetailClose: UI_IDENTIFIERS.Construction.DETAIL_CLOSE,
  /** The provenance rail (drawn only for a reconstructed row, phase, task or lane). */
  constructionProvenanceRail: UI_IDENTIFIERS.Construction.PROVENANCE_RAIL,
  constructionDetailBreadcrumb: UI_IDENTIFIERS.Construction.DETAIL_BREADCRUMB,
  constructionDetailStateChip: UI_IDENTIFIERS.Construction.DETAIL_STATE_CHIP,
  /** The pane header's provenance chip — or, with "Observed only" hiding this
   *  selection's attempts, the "OBSERVED ONLY · N reconstructed hidden" chip (B1). */
  constructionDetailProvenanceChip: UI_IDENTIFIERS.Construction.DETAIL_PROVENANCE_CHIP,
  constructionDetailObservedOnlyChip: UI_IDENTIFIERS.Construction.DETAIL_OBSERVED_ONLY_CHIP,
  constructionDetailActionBar: UI_IDENTIFIERS.Construction.DETAIL_ACTION_BAR,
  constructionDetailActionRun: UI_IDENTIFIERS.Construction.detailAction('run'),
  // The four detail-pane bodies (Stage B Tasks 8-10) — which one fills the
  // pane's single body slot for the current selection (see bodyDispatch.ts).
  constructionDetailBodyArtifact: UI_IDENTIFIERS.Construction.DETAIL_BODY_ARTIFACT,
  constructionDetailBodyUnknown: UI_IDENTIFIERS.Construction.DETAIL_BODY_UNKNOWN,
  // The TASKS lens (Stage C): one row per owed decision, the header, the
  // degraded-policy banner, the empty state, and the pane's decision controls.
  constructionTasksLens: UI_IDENTIFIERS.Construction.TASKS_LENS,
  constructionTasksHeadline: UI_IDENTIFIERS.Construction.TASKS_HEADLINE,
  constructionTasksPolicyBanner: UI_IDENTIFIERS.Construction.TASKS_POLICY_BANNER,
  constructionTasksPolicySummary: UI_IDENTIFIERS.Construction.TASKS_POLICY_SUMMARY,
  constructionTasksPolicyLink: UI_IDENTIFIERS.Construction.TASKS_POLICY_LINK,
  constructionTasksEmpty: UI_IDENTIFIERS.Construction.TASKS_EMPTY,
  constructionTasksEmptyCounts: UI_IDENTIFIERS.Construction.TASKS_EMPTY_COUNTS,
  constructionTasksResume: UI_IDENTIFIERS.Construction.TASKS_RESUME,
  constructionTasksUnchecked: UI_IDENTIFIERS.Construction.TASKS_UNCHECKED,
  constructionTasksUncheckedRetry: UI_IDENTIFIERS.Construction.TASKS_UNCHECKED_RETRY,
  constructionTasksFiltered: UI_IDENTIFIERS.Construction.TASKS_FILTERED,
  constructionTasksRow: UI_IDENTIFIERS.Construction.tasksRow,
  constructionTasksCell: UI_IDENTIFIERS.Construction.tasksCell,
  constructionTasksReview: UI_IDENTIFIERS.Construction.tasksReview,
  constructionTasksGitHub: UI_IDENTIFIERS.Construction.tasksGitHub,
  constructionTasksFlow: UI_IDENTIFIERS.Construction.tasksFlow,
  constructionLensTasksCount: UI_IDENTIFIERS.Construction.LENS_TASKS_COUNT,
  constructionDetailDecisionNote: UI_IDENTIFIERS.Construction.DETAIL_DECISION_NOTE,
  constructionDetailDecisionSendBack: UI_IDENTIFIERS.Construction.DETAIL_DECISION_SEND_BACK,
  constructionDetailDecisionFlow: UI_IDENTIFIERS.Construction.DETAIL_DECISION_FLOW,
  constructionDetailDecisionCaption: UI_IDENTIFIERS.Construction.DETAIL_DECISION_CAPTION,
  constructionDetailDecisionLead: UI_IDENTIFIERS.Construction.DETAIL_DECISION_LEAD,
  constructionDetailNextDecision: UI_IDENTIFIERS.Construction.DETAIL_NEXT_DECISION,
  constructionDetailVerdict: UI_IDENTIFIERS.Construction.DETAIL_VERDICT,
  constructionDetailOwedReason: UI_IDENTIFIERS.Construction.DETAIL_OWED_REASON,
  constructionDetailReviewOnlyNote: UI_IDENTIFIERS.Construction.DETAIL_REVIEW_ONLY_NOTE,
  // The artifact frame and its placements (renderers slice 1): every committed
  // artifact in the pane mounts in one frame whose role label and source line
  // say what it is — and never carries the provenance hatch.
  constructionArtifactFrame: UI_IDENTIFIERS.Construction.ARTIFACT_FRAME,
  constructionArtifactRole: UI_IDENTIFIERS.Construction.ARTIFACT_ROLE,
  constructionArtifactSource: UI_IDENTIFIERS.Construction.ARTIFACT_SOURCE,
  constructionArtifactFocus: UI_IDENTIFIERS.Construction.ARTIFACT_FOCUS,
  constructionArtifactReconstructedNote: UI_IDENTIFIERS.Construction.ARTIFACT_RECONSTRUCTED_NOTE,
  constructionArtifactStateLine: UI_IDENTIFIERS.Construction.ARTIFACT_STATE_LINE,
  constructionArtifactAboutTask: UI_IDENTIFIERS.Construction.ARTIFACT_ABOUT_TASK,
  constructionContractSummary: UI_IDENTIFIERS.Construction.CONTRACT_SUMMARY,
  constructionContractSummaryOpen: UI_IDENTIFIERS.Construction.CONTRACT_SUMMARY_OPEN,
  constructionContractReference: UI_IDENTIFIERS.Construction.CONTRACT_REFERENCE,
  constructionContractReferenceOpen: UI_IDENTIFIERS.Construction.CONTRACT_REFERENCE_OPEN,
  constructionContractGap: UI_IDENTIFIERS.Construction.CONTRACT_GAP,
  constructionContractByDesign: UI_IDENTIFIERS.Construction.CONTRACT_BY_DESIGN,
  constructionWhoReachesIt: UI_IDENTIFIERS.Construction.WHO_REACHES_IT,
  constructionCodeReviewCommit: UI_IDENTIFIERS.Construction.CODE_REVIEW_COMMIT,
  constructionSrsUnreadable: UI_IDENTIFIERS.Construction.SRS_UNREADABLE,
  constructionComponentTestPlan: UI_IDENTIFIERS.Construction.COMPONENT_TEST_PLAN,
  constructionComponentTestPlanEmpty: UI_IDENTIFIERS.Construction.COMPONENT_TEST_PLAN_EMPTY,
  constructionTestCoverage: UI_IDENTIFIERS.Construction.TEST_COVERAGE,
  constructionTestCoverageDirect: UI_IDENTIFIERS.Construction.TEST_COVERAGE_DIRECT,
  constructionCoverageReachedRow: UI_IDENTIFIERS.Construction.coverageReachedRow,
  constructionUseCaseFlowsLink: UI_IDENTIFIERS.Construction.USE_CASE_FLOWS_LINK,
  constructionFocusView: UI_IDENTIFIERS.Construction.FOCUS_VIEW,
  constructionFocusClose: UI_IDENTIFIERS.Construction.FOCUS_CLOSE,
  constructionFocusHeading: UI_IDENTIFIERS.Construction.FOCUS_HEADING,
  constructionFocusRail: UI_IDENTIFIERS.Construction.FOCUS_RAIL,
  constructionFocusRailToggle: UI_IDENTIFIERS.Construction.FOCUS_RAIL_TOGGLE,
  constructionFocusVerdictChip: UI_IDENTIFIERS.Construction.FOCUS_VERDICT_CHIP,
  constructionFocusPlaceholder: UI_IDENTIFIERS.Construction.FOCUS_PLACEHOLDER,
  constructionCodeReviewNoView: UI_IDENTIFIERS.Construction.CODE_REVIEW_NO_VIEW,
  constructionCasePicker: UI_IDENTIFIERS.Construction.CASE_PICKER,
  serviceContractSignatureList: UI_IDENTIFIERS.ServiceContract.SIGNATURE_LIST,
  serviceContractOpRow: UI_IDENTIFIERS.ServiceContract.opRow,
  serviceContractOpSignature: UI_IDENTIFIERS.ServiceContract.OP_SIGNATURE,
  serviceContractOpStructs: UI_IDENTIFIERS.ServiceContract.OP_STRUCTS,
  serviceContractOpenFocus: UI_IDENTIFIERS.ServiceContract.OPEN_FOCUS,
  serviceContractCodeCanvas: UI_IDENTIFIERS.ServiceContract.CODE_CANVAS,
  serviceContractUtilitiesLine: UI_IDENTIFIERS.ServiceContract.UTILITIES_LINE,
  serviceContractStructCard: UI_IDENTIFIERS.ServiceContract.STRUCT_CARD,
  serviceContractCodeCanvasFrame: UI_IDENTIFIERS.ServiceContract.CODE_CANVAS_FRAME,
  serviceContractCanvasNeedsRoom: UI_IDENTIFIERS.ServiceContract.CANVAS_NEEDS_ROOM,
  serviceContractCanvasNeedsRoomCollapse: UI_IDENTIFIERS.ServiceContract.CANVAS_NEEDS_ROOM_COLLAPSE,
  serviceContractCodeInterfaceNode: UI_IDENTIFIERS.ServiceContract.CODE_INTERFACE_NODE,
  serviceContractCodeCanvasCaption: UI_IDENTIFIERS.ServiceContract.CODE_CANVAS_CAPTION,
  serviceContractFieldRow: UI_IDENTIFIERS.ServiceContract.FIELD_ROW,
  constructionDetailProvenanceDisclosure: UI_IDENTIFIERS.Construction.DETAIL_PROVENANCE_DISCLOSURE,
  serviceContractStructName: UI_IDENTIFIERS.ServiceContract.STRUCT_NAME,
  serviceContractParamRow: UI_IDENTIFIERS.ServiceContract.PARAM_ROW,
  serviceContractNeighbourRow: UI_IDENTIFIERS.ServiceContract.neighbourRow,
  constructionDetailBodyReview: UI_IDENTIFIERS.Construction.DETAIL_BODY_REVIEW,
  constructionDetailProvenanceNote: UI_IDENTIFIERS.Construction.DETAIL_PROVENANCE_NOTE,
  constructionDetailBody: UI_IDENTIFIERS.Construction.DETAIL_BODY,
  constructionScenarioPicker: UI_IDENTIFIERS.Construction.SCENARIO_PICKER,
  constructionDetailEvidence: UI_IDENTIFIERS.Construction.DETAIL_EVIDENCE,
  // The service contract view (ServiceContractView).
  serviceContractRoot: UI_IDENTIFIERS.ServiceContract.ROOT,
  serviceContractTabCode: UI_IDENTIFIERS.ServiceContract.TAB_CODE,
  serviceContractTabComponent: UI_IDENTIFIERS.ServiceContract.TAB_COMPONENT,
  serviceContractTabDynamic: UI_IDENTIFIERS.ServiceContract.TAB_DYNAMIC,
  serviceContractTabFacets: UI_IDENTIFIERS.ServiceContract.TAB_FACETS,
  serviceContractComponentFlow: UI_IDENTIFIERS.ServiceContract.COMPONENT_FLOW,
  serviceContractRevisionHistory: UI_IDENTIFIERS.ServiceContract.REVISION_HISTORY,
  serviceContractStatusChip: UI_IDENTIFIERS.ServiceContract.STATUS_CHIP,
  serviceContractFacetsEmpty: UI_IDENTIFIERS.ServiceContract.FACETS_EMPTY,
  archC4Node: UI_IDENTIFIERS.Architecture.c4Node,
  // The frontend renderer: no live iframe, a new-tab link or the no-surfaces state.
  constructionFrontendView: UI_IDENTIFIERS.Construction.FRONTEND_VIEW,
  constructionFrontendOpenLink: UI_IDENTIFIERS.Construction.FRONTEND_OPEN_LINK,
  constructionFrontendNoSurfaces: UI_IDENTIFIERS.Construction.FRONTEND_NO_SURFACES,
  constructionDetailAction: UI_IDENTIFIERS.Construction.detailAction,

  // Operations console (route `/operations/$operatedAppId`).
  operationsRoot: UI_IDENTIFIERS.Operations.ROOT,
  operationsTabStatus: UI_IDENTIFIERS.Operations.TAB_STATUS,
  operationsAwaiting: UI_IDENTIFIERS.Operations.AWAITING,

  // SP1 capture-seam episodes panel — mounted below the artifact renderer on
  // every design-artifact page (Phase 1 + Phase 2) and inside the construction
  // activity-lifecycle drawer. Per-row ids append the episode id.
  episodesPanel: UI_IDENTIFIERS.Episodes.PANEL,
  episodesRow: UI_IDENTIFIERS.Episodes.episodeRow,
  episodeTimeline: UI_IDENTIFIERS.Episodes.TIMELINE,
  episodeLineageTree: UI_IDENTIFIERS.Episodes.LINEAGE_TREE,
  episodesExportMenu: UI_IDENTIFIERS.Episodes.EXPORT_MENU_BUTTON,
  episodeExportJson: UI_IDENTIFIERS.Episodes.EXPORT_JSON,
  episodeExportCsv: UI_IDENTIFIERS.Episodes.EXPORT_CSV,
  episodeOutcomeChip: UI_IDENTIFIERS.Episodes.outcomeChip,
  episodeTimelineFilter: UI_IDENTIFIERS.Episodes.TIMELINE_FILTER,

  // Billing (route `/project/$projectId/billing`).
  billingRoot: UI_IDENTIFIERS.Billing.ROOT,
  billingPendingState: UI_IDENTIFIERS.Billing.PENDING_STATE,
  billingHomeLink: UI_IDENTIFIERS.Billing.HOME_LINK,
} as const;

/**
 * The ordered Phase-1 artifact kinds — the DRAFTING SEQUENCE, not the wire enum.
 * The first — `mission` — is the spine's first step and the only one reachable
 * from a fresh project. Imported straight from the SPA's own PHASE1_ORDER
 * (webApp/src/contracts/methodMetadata.ts) — this display order is product
 * data the SPA owns, not a wire enum uitests should re-derive by hand. That import
 * is why the specs iterating this list needed no edit when Phase 1 collapsed to
 * Requirements + Architecture (mission / glossary / volatilities / coreUseCases /
 * system): they follow PHASE1_ORDER automatically.
 */
export const PHASE1_ARTIFACTS = PHASE1_ORDER;

/**
 * The active phase id for a fresh project (its phase card + resume target).
 * Typed against the SPA's own `PhaseId` union (webApp/src/contracts/adapters.ts)
 * so a rename of the 'systemDesign' literal fails to compile here rather than
 * silently drifting — there is no standalone webApp export of just the first
 * phase id to import a VALUE from (PhaseId only appears as a Record key).
 */
export const ACTIVE_PHASE_ID: PhaseId = 'systemDesign';
