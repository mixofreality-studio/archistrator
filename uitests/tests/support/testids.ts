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

  // Architecture (System artifact) view switcher — static / dynamic / perspective.
  // The switch VALUES are the bare strings 'static' | 'dynamic' | 'perspective'.
  archViewSwitch: UI_IDENTIFIERS.Architecture.VIEW_SWITCH,
  archViewStatic: UI_IDENTIFIERS.Architecture.VIEW_STATIC,
  archViewDynamic: UI_IDENTIFIERS.Architecture.VIEW_DYNAMIC,
  archViewPerspective: UI_IDENTIFIERS.Architecture.VIEW_PERSPECTIVE,
  archDynamicPicker: UI_IDENTIFIERS.Architecture.DYNAMIC_PICKER,
  archPerspectivePicker: UI_IDENTIFIERS.Architecture.PERSPECTIVE_PICKER,

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

  // Deployment (operationalConcepts artifact) profile switcher — the switch
  // VALUES ('cloud'/'local'/'test') are this package's own concern (no
  // UI_IDENTIFIERS counterpart — they are DeploymentProfile prop values, not
  // published testids).
  deployProfileSwitch: UI_IDENTIFIERS.Deployment.PROFILE_SWITCH,
  deployProfileCloud: 'cloud',
  deployProfileLocal: 'local',
  deployProfileTest: 'test',

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

  // Construction console (route `/project/$projectId/construction`).
  constructionTabTracker: UI_IDENTIFIERS.Construction.TAB_TRACKER,
  constructionTabArtifacts: UI_IDENTIFIERS.Construction.TAB_ARTIFACTS,
  constructionTracker: UI_IDENTIFIERS.Construction.TRACKER,
  constructionArtifacts: UI_IDENTIFIERS.Construction.ARTIFACTS,
  constructionArtifactRow: UI_IDENTIFIERS.Construction.artifactRow,
  constructionSystemTestView: UI_IDENTIFIERS.Construction.SYSTEM_TEST_VIEW,
  constructionTestPlanView: UI_IDENTIFIERS.Construction.TEST_PLAN_VIEW,
  constructionScenarioPicker: UI_IDENTIFIERS.Construction.SCENARIO_PICKER,
  constructionCaseChip: UI_IDENTIFIERS.Construction.caseChip,
  activityLifecyclePanel: UI_IDENTIFIERS.Construction.ACTIVITY_LIFECYCLE_PANEL,

  // Operations console (route `/operations/$operatedAppId`).
  operationsRoot: UI_IDENTIFIERS.Operations.ROOT,
  operationsTabStatus: UI_IDENTIFIERS.Operations.TAB_STATUS,
  operationsAwaiting: UI_IDENTIFIERS.Operations.AWAITING,

  // Billing (route `/project/$projectId/billing`).
  billingRoot: UI_IDENTIFIERS.Billing.ROOT,
  billingPendingState: UI_IDENTIFIERS.Billing.PENDING_STATE,
  billingHomeLink: UI_IDENTIFIERS.Billing.HOME_LINK,
} as const;

/**
 * The ordered Phase-1 artifact kinds (openapi ArtifactKind enum order). The
 * first — `mission` — is the spine's first step and the only one reachable
 * from a fresh project. Imported straight from the SPA's own PHASE1_ORDER
 * (webApp/src/contracts/methodMetadata.ts) — this display order is product
 * data the SPA owns, not a wire enum uitests should re-derive by hand.
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
