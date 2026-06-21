/**
 * Stable `data-testid`s the SPA exposes, MIRRORED here as plain string literals.
 *
 * Anti-cheat boundary: this package links ZERO webApp source (the UI sibling of
 * ../systemtests, which links zero server code). So we cannot `import` the
 * webApp's UIIdentifiers.ts — instead we copy the wire-visible ids the SPA
 * renders. These are a black-box contract: if the SPA renames a testid, the
 * matching assertion here fails, exactly as a wire-format change would. Keep in
 * sync with products/archistrator/webApp/src/constants/UIIdentifiers.ts.
 */
export const TESTID = {
  // Session gate / common
  loading: 'loading-indicator',
  errorAlert: 'error-alert',

  // Projects landing (route `/`)
  projectsLandingScreen: 'projects-landing-screen',
  projectsGrid: 'projects-grid',
  emptyState: 'empty-state',
  newProjectCard: 'new-project-card',
  newProjectButton: 'new-project-button',
  createProjectDialog: 'create-project-dialog',
  newProjectNameInput: 'new-project-name-input',
  createProjectSubmit: 'create-project-submit',
  createProjectCancel: 'create-project-cancel',
  createProjectPrereqs: 'create-project-prereqs',
  projectCard: (projectId: string): string => `project-card-${projectId}`,

  // Shell
  projectMenu: 'project-menu',
  projectMenuNew: 'project-menu-new',
  projectMenuItem: (projectId: string): string => `project-menu-item-${projectId}`,
  teamNav: 'team-nav',

  // Team roster (route `/project/$projectId/team`) — static Method-roles roster.
  // Mirrors UIIdentifiers.Team in webApp/src/constants/UIIdentifiers.ts.
  teamScreen: 'team-screen',
  teamRoleCard: (roleId: string): string => `team-role-card-${roleId}`,
  teamCharterDrawer: 'team-charter-drawer',
  teamCharterClose: 'team-charter-close',
  teamCharterTogglePrompt: 'team-charter-toggle-prompt',

  // Home base (route `/project/$projectId/home`)
  homeBaseScreen: 'home-base-screen',
  resumeDesign: 'resume-design',
  artifactToc: 'artifact-toc',
  economicsStrip: 'economics-strip',
  // NOTE: phase ids are the typed PhaseId values — systemDesign / projectDesign /
  // construction — NOT the route slug. The `phase-card-system` shorthand in the
  // task brief resolves to `phase-card-systemDesign` on the wire.
  phaseCard: (phaseId: string): string => `phase-card-${phaseId}`,
  tocRow: (kind: string): string => `toc-row-${kind}`,

  // Design experience (route `/project/$projectId/design/system`)
  designExperience: 'design-experience',
  designClose: 'design-close',
  slimSpine: 'slim-spine',
  spineStep: (kind: string): string => `spine-step-${kind}`,
  requestDraft: 'request-draft',
  researchInput: 'research-input',
  researchInputTitle: 'research-input-title',
  researchInputText: 'research-input-text',
  researchInputSubmit: 'research-input-submit',
  generatingScene: 'generating-scene',
  ciJobNotice: 'ci-job-notice',
  ciJobLink: 'ci-job-link',
  artifactRender: 'artifact-render',
  draftFailed: 'draft-failed',
  draftFailureReason: 'draft-failure-reason',
  retryDraft: 'retry-draft',
  withdrawDraft: 'withdraw-draft',

  // Architecture (System artifact) view switcher — static / dynamic / perspective.
  // Mirrors UIIdentifiers.Architecture; the switch VALUES are the bare strings
  // 'static' | 'dynamic' | 'perspective'.
  archViewSwitch: 'arch-view-switch',
  archViewStatic: 'static',
  archViewDynamic: 'dynamic',
  archViewPerspective: 'perspective',
  archDynamicPicker: 'arch-dynamic-picker',
  archPerspectivePicker: 'arch-perspective-picker',

  // Deployment (operationalConcepts artifact) profile switcher — values are the
  // bare DeploymentProfile strings 'cloud' | 'local' | 'test'.
  deployProfileSwitch: 'deploy-profile-switch',
  deployProfileCloud: 'cloud',
  deployProfileLocal: 'local',
  deployProfileTest: 'test',

  // Gate panel
  gatePanel: 'gate-panel',
  gateApprove: 'gate-approve',
  gateSendback: 'gate-sendback',
  gateWithdraw: 'gate-withdraw',
  gateFindings: 'findings',

  // Chat rail (anchored comments)
  chatRail: 'chat-rail',
  chatToggle: 'chat-toggle',
  chatInput: 'chat-input',
  chatSend: 'chat-send',
  commentAnchor: (n: number): string => `comment-anchor-${String(n)}`,

  // GIT-FORWARD per-activity row cluster (U-SPA-GIT). The construction tracker's
  // active-activity detail renders this when the project read carries a gitRow
  // for the active activity (honest-empty: absent otherwise). Mirrors
  // UIIdentifiers.Git in webApp/src/constants/UIIdentifiers.ts.
  gitRowMeta: 'git-row-meta',
  gitPrLink: 'git-pr-link',
  gitBranch: 'git-branch',
  gitMerged: 'git-merged',
  gitCrLabel: 'git-cr-label',
  gitArchApproved: 'git-arch-approved',
  gitCiStatus: (status: string): string => `git-ci-${status}`,
} as const;

/**
 * The ordered Phase-1 artifact kinds (openapi ArtifactKind enum order). The first
 * — `mission` — is the spine's first step and the only one reachable from a fresh
 * project. Mirrors webApp/src/api/types.ts PHASE1_ARTIFACTS.
 */
export const PHASE1_ARTIFACTS = [
  'mission',
  'glossary',
  'scrubbedRequirements',
  'volatilities',
  'coreUseCases',
  'system',
  'operationalConcepts',
  'standardCheck',
] as const;

/** The active phase id for a fresh project (its phase card + resume target). */
export const ACTIVE_PHASE_ID = 'systemDesign';
