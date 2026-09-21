/**
 * Route-intercept stubs for the System Design co-author experience.
 *
 * These let a spec render a SPECIFIC design-experience state (a committed
 * artifact, or a terminal draftFailed) WITHOUT a live drafting stack — the same
 * "stub the wire, drive the real SPA" tactic the F79 gate-error verification uses.
 * Everything is fulfilled in-browser via `page.route`, so the specs are hermetic:
 * they run green even with no Go server behind the proxy.
 *
 * The stubbed wire shapes mirror the generated client contract EXACTLY (the SPA's
 * `mapProjectState` / `mapSessionState` decode them):
 *   • GetProject  (SystemDesignProjectState) — PascalCase envelope, `Slots[]` with
 *     integer `stage` (2 = committed) and a `{kind, model}` model envelope.
 *   • GetSessionState (SystemDesignSessionStateView) — camelCase, integer
 *     `artifactKind` / `stage` (7 = draftFailed).
 *
 * This module links ZERO webApp source; the field names are copied as black-box
 * literals, exactly like tests/support/testids.ts.
 */
import { readFileSync } from 'node:fs';
import { type Page } from '@playwright/test';

/** Phase-1 ArtifactKind WIRE ordinals (openapi enum order). This is the wire enum,
 * NOT the drafting sequence: `scrubbedRequirements` (2), `operationalConcepts` (6)
 * and `standardCheck` (7) are retired in place — they left PHASE1_ORDER and lost
 * their step pages, but they keep their ordinals and stay valid wire values on every
 * already-committed project.json, so nothing here is renumbered. */
const KIND_ORDINAL: Record<string, number> = {
  mission: 0,
  glossary: 1,
  scrubbedRequirements: 2,
  volatilities: 3,
  coreUseCases: 4,
  system: 5,
  operationalConcepts: 6,
  standardCheck: 7,
};

/** ArtifactStage ordinal for a committed slot. */
const STAGE_COMMITTED = 2;
/** SessionStage ordinal for a session parked at the human review gate. */
const STAGE_AWAITING_REVIEW = 2;
/** SessionStage ordinal for the async design-job terminal failure. */
const STAGE_DRAFT_FAILED = 7;

interface Slot {
  kind: string;
  stage: number;
  revisions: number;
  model: { kind: string; model?: unknown };
}

function projectState(id: string, name: string, slots: Slot[]): unknown {
  return {
    ProjectID: id,
    Name: name,
    Owner: 'dev-architect',
    Phase: 0,
    Version: 1,
    Research: { sources: [] },
    Slots: slots,
  };
}

/** A committed slot carrying a real model envelope (renders through ArtifactRenderer). */
function committedSlot(kind: string, model?: unknown): Slot {
  return { kind, stage: STAGE_COMMITTED, revisions: 1, model: { kind, model } };
}

/** ArtifactStage ordinal for a slot nothing has been drafted into yet. */
const STAGE_EMPTY = 0;

/** The Phase-1 slots a freshly created project reads back with: every one listed,
 *  every one empty. get-project lists every slot kind, empty ones included, and the
 *  home base's table of contents is built from that list. */
const FRESH_PHASE1_KINDS = ['mission', 'glossary', 'volatilities', 'coreUseCases', 'system'];

function emptySlot(kind: string): Slot {
  return { kind, stage: STAGE_EMPTY, revisions: 0, model: { kind } };
}

/**
 * stubSessionGate fulfils `/api/userinfo` with a dev principal so the SPA's session
 * gate mounts the router without any real backend. Call before `page.goto`.
 */
export async function stubSessionGate(page: Page): Promise<void> {
  await page.route('**/api/userinfo', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        kind: 'user',
        sub: 'dev-architect',
        preferred_username: 'dev-architect',
        roles: ['drive-phase', 'approve-artifact'],
      }),
    }),
  );
}

/** Intercept GetProject for `projectId`, returning the given wire project state. */
async function stubGetProject(page: Page, projectId: string, state: unknown): Promise<void> {
  await page.route(`**/api/v1/system-design/get-project/${projectId}**`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(state),
    }),
  );
}

/** Every session probe 404s: committed slots only, no live co-author session (R6). */
async function stubNoSession(page: Page): Promise<void> {
  await page.route('**/api/v1/system-design/get-session-state/**', (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'no session', code: 'not_found' }),
    }),
  );
}

/**
 * stubCommittedCoreUseCases stubs a project whose ONLY committed Phase-1 slot is
 * `coreUseCases`, carrying the REAL committed model extracted from this repo's
 * dogfood project.json (testdata/coreUseCasesProject.json). Its session probes 404
 * (no live co-author session), so the step renders the committed artifact through
 * the CommittedArtifactPanel → UseCaseCarousel → walkthrough. Returns the project id.
 *
 * The fixture's coreUseCases SLOT (stage/revisions/model) is mechanically kept in
 * sync with the repo's actual committed `.aiarch/state/project.json` via
 * `npm run regen:core-use-cases-fixture` (server/cmd/gen-uitests-fixtures) — see
 * `npm run check:core-use-cases-fixture` for the drift check. The fixture's outer
 * envelope (ProjectID/Name/Owner/Phase/Version/Research) is a deliberately
 * synthetic test identity, untouched by the regen.
 */
export async function stubCommittedCoreUseCases(page: Page): Promise<string> {
  const fixture = JSON.parse(
    readFileSync(new URL('../../testdata/coreUseCasesProject.json', import.meta.url), 'utf8'),
  ) as { ProjectID: string; Slots: Slot[] };
  const projectId = fixture.ProjectID;

  await stubSessionGate(page);
  await stubGetProject(page, projectId, fixture);
  await stubNoSession(page);
  return projectId;
}

/**
 * A wire ModelGlossaryItem literal (schema.ts): the Four-Questions `category`
 * may be a refined "How-activity"-style tag or '' (→ Uncategorized).
 */
export interface StubGlossaryItem {
  term: string;
  definition: string;
  category: string;
}

/**
 * stubCommittedGlossary stubs a project whose `glossary` slot is COMMITTED with
 * the given typed items (plus a committed empty `mission` upstream so the spine
 * shows glossary as a done step). Its session probes 404 (no live co-author
 * session), so selecting the glossary spine step renders the committed artifact
 * through CommittedArtifactPanel → GlossaryView. Returns the project id.
 */
export async function stubCommittedGlossary(
  page: Page,
  items: StubGlossaryItem[],
): Promise<string> {
  const projectId = 'glossary-fixture';
  const slots = [committedSlot('mission', {}), committedSlot('glossary', { items })];

  await stubSessionGate(page);
  await stubGetProject(page, projectId, projectState(projectId, 'Glossary Fixture', slots));
  await stubNoSession(page);
  return projectId;
}

/** A wire ModelVolatility literal (schema.ts) — one categorical Löwy axis, no 2D. */
export interface StubVolatility {
  name: string;
  rationale: string;
  axis: 'sameCustomerOverTime' | 'allCustomersAtOneTime';
  /** Scrubbed-requirement ids (SR-…) this volatility traces to. */
  traces?: string[];
}

/** A wire ModelRejectedVolatility literal (schema.ts) — classified rejection. */
export interface StubRejectedVolatility {
  name: string;
  reason: string;
  class: 'variableNotVolatile' | 'natureOfTheBusiness' | 'speculative' | 'foldedInto';
}

/**
 * stubCommittedVolatilities stubs a project whose `volatilities` slot is
 * COMMITTED with the given accepted items + rejected candidates (the model's
 * newer `rejected`/`traces` fields), upstream mission/glossary committed empty.
 * Session probes 404, so selecting the volatilities spine step renders the
 * committed VolatilityMap. Returns the project id.
 */
export async function stubCommittedVolatilities(
  page: Page,
  items: StubVolatility[],
  rejected: StubRejectedVolatility[],
): Promise<string> {
  const projectId = 'volatilities-fixture';
  const slots = [
    ...['mission', 'glossary'].map((k) => committedSlot(k, {})),
    committedSlot('volatilities', { items, rejected }),
  ];

  await stubSessionGate(page);
  await stubGetProject(page, projectId, projectState(projectId, 'Volatilities Fixture', slots));
  await stubNoSession(page);
  return projectId;
}

/**
 * stubAwaitingReviewGlossary stubs a project sitting on the Glossary step with a
 * live co-author session parked at the human review GATE (stage awaitingReview),
 * carrying the given typed items as the draft under review. Mission upstream is
 * committed so the spine's first-open step IS glossary. The spec then drives the
 * REAL GatePanel / ChatRail (stage a note, send back) against whatever
 * submit-review-decision route it installs — the F-QA2-47 fault-path tactic.
 * Returns the project id.
 *
 * Items are typed structurally (wire ModelGlossaryItem literals) so this stub
 * stands alone.
 */
export async function stubAwaitingReviewGlossary(
  page: Page,
  items: { term: string; definition: string; category: string }[],
): Promise<string> {
  const projectId = 'glossary-gate-fixture';
  const slots = [committedSlot('mission', {})];

  await stubSessionGate(page);
  await stubGetProject(page, projectId, projectState(projectId, 'Glossary Gate Fixture', slots));
  await page.route('**/api/v1/system-design/get-session-state/**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        projectId,
        artifactKind: KIND_ORDINAL.glossary,
        stage: STAGE_AWAITING_REVIEW,
        draft: { kind: 'glossary', model: { items } },
        findings: [],
      }),
    }),
  );
  return projectId;
}

/**
 * stubDraftFailedArchitecture stubs a project sitting on the Architecture (`system`)
 * step in the terminal `draftFailed` stage — the async design-job failure the
 * DraftFailedPanel renders. The upstream Phase-1 slots (mission…coreUseCases) are
 * committed so the spine lands its first-open step on `system`; the session probe
 * for the active step returns stage=draftFailed with a human reason + run URL.
 * Returns the project id.
 */
export async function stubDraftFailedArchitecture(page: Page): Promise<string> {
  const projectId = 'draft-failed-fixture';
  const upstream = ['mission', 'glossary', 'volatilities', 'coreUseCases'].map((k) =>
    committedSlot(k, {}),
  );

  await stubSessionGate(page);
  await stubGetProject(
    page,
    projectId,
    projectState(projectId, 'Draft Failed Fixture', upstream),
  );
  // Session probe: whatever step is active, report the async terminal failure. The
  // response echoes the requested `kind` so the panel titles the right artifact.
  await page.route('**/api/v1/system-design/get-session-state/**', (route) => {
    const url = new URL(route.request().url());
    const kindOrdinal = Number(url.searchParams.get('kind') ?? KIND_ORDINAL.system);
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        projectId,
        artifactKind: kindOrdinal,
        stage: STAGE_DRAFT_FAILED,
        draft: { kind: 'system' },
        failureReason:
          'The design job failed in your CI: the drafting Action exited non-zero before committing an artifact.',
        failureRunUrl: 'https://github.com/acme/archistrator/actions/runs/123456',
      }),
    });
  });
  return projectId;
}

/**
 * stubRetryableDraftFailedGlossary stubs a project whose Glossary step sits at the
 * async `draftFailed` stage, where the Retry POST (request-artifact-draft) flips
 * the SAME session to the review gate — the server-side "resume from read-back"
 * transition (failed → awaitingReview within seconds, NO new CI job). Drives the
 * F-QA2-50 regression: the flip must render WITHOUT a reload. Returns the
 * project id.
 */
export async function stubRetryableDraftFailedGlossary(
  page: Page,
  items: { term: string; definition: string; category: string }[],
): Promise<string> {
  const projectId = 'retryable-draft-failed-fixture';
  let stage: number = STAGE_DRAFT_FAILED;
  // Model the LIVE race that froze the SPA: the server resumes on its own clock,
  // so the read the retry mutation's invalidation triggers still observes the
  // stale failed stage — only a LATER poll can ever deliver the flip. (With the
  // failed stage treated as a poll stop, that later poll never came.)
  let staleReadsAfterRetry = 0;

  await stubSessionGate(page);
  await stubGetProject(
    page,
    projectId,
    projectState(projectId, 'Retryable Failed Fixture', [committedSlot('mission', {})]),
  );
  // The Retry mutation: acknowledge the draft request and move the session to the
  // gate — the SPA only ever learns the new stage from the polled session state.
  await page.route('**/api/v1/system-design/request-artifact-draft/**', (route) => {
    stage = STAGE_AWAITING_REVIEW;
    staleReadsAfterRetry = 1;
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify('sess-retry-1'),
    });
  });
  await page.route('**/api/v1/system-design/get-session-state/**', (route) => {
    const stillFailed = stage === STAGE_DRAFT_FAILED || staleReadsAfterRetry > 0;
    if (staleReadsAfterRetry > 0) staleReadsAfterRetry--;
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(
        stillFailed
          ? {
              projectId,
              artifactKind: KIND_ORDINAL.glossary,
              stage: STAGE_DRAFT_FAILED,
              draft: { kind: 'glossary' },
              failureReason:
                'The design job failed in your CI: the drafting Action exited non-zero before committing an artifact.',
              failureRunUrl: 'https://github.com/acme/archistrator/actions/runs/123456',
            }
          : {
              projectId,
              artifactKind: KIND_ORDINAL.glossary,
              stage,
              draft: { kind: 'glossary', model: { items } },
              findings: [],
            },
      ),
    });
  });
  return projectId;
}

/**
 * A stubbed live deployment-health reading: the wire rows
 * GET /api/v1/operations/query-deployment-health/{operatedAppID} answers.
 * `Health` is the OperationsHealthState ORDINAL (0 Neutral, 1 Healthy, 2 Unhealthy);
 * `ModelKey` is the topology element key the overlay colours.
 */
export interface StubNodeHealth {
  ModelKey: string;
  Health: number;
}

/** The operated-app id the D13 derivation is stubbed to answer (a uuid, never the
 *  project id — the two are not interchangeable, see useOperatedAppId). */
const STUB_OPERATED_APP_ID = '6f1d2c34-9ab5-4e77-8c10-2f5b7d9e4a13';

/**
 * stubCommittedArchitecture stubs a project whose `system` slot is COMMITTED with a
 * small layered decomposition AND whose retired-in-place `operationalConcepts` slot
 * is COMMITTED with a deployment topology — the exact head-state shape the
 * Architecture step's DEPLOYMENT LENS reads (ArchitectureView joins the topology off
 * the project head-state through CommittedSlotsContext, not off its own slot).
 *
 * This is the hermetic replacement for the deployment coverage architecture-views.spec
 * used to carry on the retired Deployment & Operations step: `operationalConcepts` is
 * no longer in the drafting sequence, so no live run can reach a committed topology
 * any more, but the lens must keep rendering for the projects that already have one.
 * Stubbing the wire is now the only way to exercise it.
 *
 * The first environment is `cloud`, mirroring the real committed order (cloud, test,
 * local) — the lens shows environments[0], and `cloud` is the ONE environment D10 ever
 * tints, so this is also the only profile on which the health overlay is observable.
 *
 * `health` drives the live-overlay arm:
 *   • omitted → capabilities answers `operations:false` (the LOCAL profile, D9). The
 *     health query stays dormant, nothing is tinted, and the diagram must render
 *     exactly as it did before the overlay existed — an unobserved node is neutral,
 *     never red.
 *   • supplied → capabilities answers `operations:true` and the health read returns
 *     these rows, so the named element keys colour.
 *
 * Returns the project id.
 */
export async function stubCommittedArchitecture(
  page: Page,
  health?: StubNodeHealth[],
): Promise<string> {
  const projectId = 'architecture-fixture';
  const system = {
    components: [
      {
        id: 'web-client',
        name: 'WebClient',
        layer: 'client',
        kind: 'client',
        encapsulates: 'the design experience',
      },
      {
        id: 'design-manager',
        name: 'DesignManager',
        layer: 'manager',
        kind: 'manager',
        encapsulates: 'the design use-case sequence',
      },
      {
        id: 'artifact-access',
        name: 'ArtifactAccess',
        layer: 'resourceAccess',
        kind: 'resourceAccess',
        encapsulates: 'artifact storage',
      },
    ],
    relationships: [
      { from: 'web-client', to: 'design-manager', label: 'drives the design', mode: 'sync' },
      {
        from: 'design-manager',
        to: 'artifact-access',
        label: 'reads/writes artifacts',
        mode: 'sync',
      },
    ],
  };
  // The topology shape DeploymentFlow consumes: shared container DEFINITIONS (which
  // carry the packaged System component names the instances are coloured by) plus one
  // environment per profile, whose nodes nest container instances + infrastructure.
  const operationalConcepts = {
    deployment: {
      containers: [
        {
          key: 'app-server',
          name: 'design-server',
          technology: 'Go',
          description: 'The design server.',
          components: ['DesignManager', 'ArtifactAccess'],
          surface: 'service',
        },
        {
          key: 'app-spa',
          name: 'design SPA',
          technology: 'TypeScript · React',
          description: 'The browser client.',
          components: ['WebClient'],
          surface: 'spa',
        },
      ],
      environments: [
        {
          profile: 'cloud',
          title: 'Cloud (K8s cluster)',
          nodes: [
            {
              key: 'cluster',
              name: 'Design cluster',
              technology: 'Kubernetes',
              description: '',
              instances: 1,
              tags: [],
              children: [
                {
                  key: 'namespace',
                  name: 'design namespace',
                  technology: 'K8s namespace',
                  description: '',
                  instances: 1,
                  tags: [],
                  children: [],
                  infrastructureNodes: [],
                  containerInstances: [
                    { key: 'ci-server', containerKey: 'app-server', note: '', tags: [] },
                  ],
                  softwareSystemInstances: [],
                },
              ],
              infrastructureNodes: [
                {
                  key: 'infra-db',
                  name: 'ProjectStateDB',
                  technology: 'Postgres',
                  description: 'The project store.',
                  tags: [],
                  role: 'other',
                },
              ],
              containerInstances: [],
              softwareSystemInstances: [],
            },
            {
              key: 'browser',
              name: 'Web browser',
              technology: 'Chrome',
              description: '',
              instances: 1,
              tags: [],
              children: [],
              infrastructureNodes: [],
              containerInstances: [
                { key: 'ci-spa', containerKey: 'app-spa', note: '', tags: [] },
              ],
              softwareSystemInstances: [],
            },
          ],
          persons: [{ key: 'architect', name: 'Architect', description: '' }],
          relationships: [
            { from: 'architect', to: 'ci-spa', label: 'designs in', technology: 'HTTPS' },
            { from: 'ci-spa', to: 'ci-server', label: 'calls', technology: 'REST' },
          ],
        },
        {
          profile: 'local',
          title: 'Local (developer laptop)',
          nodes: [
            {
              key: 'laptop',
              name: 'Developer laptop',
              technology: 'single binary',
              description: '',
              instances: 1,
              tags: [],
              children: [],
              infrastructureNodes: [],
              containerInstances: [
                { key: 'local-ci-server', containerKey: 'app-server', note: '', tags: [] },
              ],
              softwareSystemInstances: [],
            },
          ],
          persons: [],
          relationships: [],
        },
      ],
    },
  };

  const slots = [
    ...['mission', 'glossary', 'volatilities', 'coreUseCases'].map((k) => committedSlot(k, {})),
    committedSlot('system', system),
    committedSlot('operationalConcepts', operationalConcepts),
  ];

  await stubSessionGate(page);
  await stubGetProject(page, projectId, projectState(projectId, 'Architecture Fixture', slots));
  await stubNoSession(page);

  // D9 capability gate. Stubbed in BOTH arms so the spec never reaches a real
  // server: `false` is the local profile, where the overlay must stay dormant.
  await page.route('**/api/v1/capabilities', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ operations: health !== undefined }),
    }),
  );
  // D13 derivation — read in both arms (its own query is gated only on a non-empty
  // projectId), so it is stubbed in both to keep the run hermetic.
  await page.route('**/api/v1/projects/*/operated-app-id', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ operatedAppId: STUB_OPERATED_APP_ID }),
    }),
  );
  if (health !== undefined) {
    await page.route('**/api/v1/operations/query-deployment-health/**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ Nodes: health }),
      }),
    );
  }
  return projectId;
}

/**
 * stubCreatedProject stands in for a project create-project "just made", without
 * creating anything (fix-F review: landing.spec and design-experience.spec used to
 * POST real creates). In the browser:
 *   • create-project answers `projectId` (the server-minted id the SPA navigates to);
 *   • the project reads as a fresh one: Phase 0, no slots;
 *   • every design-session probe 404s (no session yet), so the first step offers
 *     "Request draft";
 *   • the REAL catalog read comes back with this project's row added.
 * Pair it with the shared dispatch guard, which aborts any other write. Returns the
 * number of creates it answered, so a spec can assert the create was faked.
 */
export async function stubCreatedProject(
  page: Page,
  projectId: string,
  name: string,
): Promise<{ creates: number }> {
  const answered = { creates: 0 };
  await page.route('**/api/v1/system-design/create-project', (route) => {
    answered.creates += 1;
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(projectId),
    });
  });
  await stubGetProject(
    page,
    projectId,
    projectState(projectId, name, FRESH_PHASE1_KINDS.map(emptySlot)),
  );
  await stubNoSession(page);
  await page.route('**/api/v1/system-design/list-projects**', async (route) => {
    // A catalog read can still be in flight when the test ends. Only "the page has
    // closed" is ignored here; any other failure still fails the test.
    try {
      const response = await route.fetch();
      const rows = (await response.json()) as unknown[];
      await route.fulfill({
        response,
        json: [
          {
            ProjectID: projectId,
            Name: name,
            Owner: 'dev-architect',
            Phase: 0,
            PhaseName: 'systemDesign',
            CommittedCount: 0,
            TotalCount: 5,
            UpdatedAt: new Date().toISOString(),
          },
          ...rows,
        ],
      });
    } catch (err) {
      if (page.isClosed() || /has been closed/.test(String(err))) return;
      throw err;
    }
  });
  return answered;
}

/** SessionStage ordinal for a session that has reached its terminal committed stage. */
const STAGE_SESSION_COMMITTED = 4;

/**
 * The three business objectives the margin fixture renders. The margin anchors to
 * the objective's INDEX (`$.objectives[n]`, see MissionView → missionObjectiveAnchor),
 * so index 2 — "Objective 3" — is what the open change request below sits beside.
 */
const MISSION_OBJECTIVES = [
  { number: 1, statement: 'Cut the time from research corpus to a committed mission below one day.' },
  { number: 2, statement: 'Keep every design decision traceable to the artifact slot that carries it.' },
  { number: 3, statement: 'Let a reviewer answer "what will this button do?" without leaving the page.' },
];

/** The open change request's id — the spec keys its margin card off this. */
export const MARGIN_CHANGE_REQUEST_ID = 'r1c0';
/** The answered, PM-addressed question's id (unanchored → the UNPLACED group). */
export const MARGIN_QUESTION_ID = 'r1c1';
/** The rendered anchor reference on the change request's card — also its jump button's name. */
export const MARGIN_ANCHOR_TEXT = 'Objective 3';

/**
 * stubCommittedMissionWithThread stubs the acceptance fixture for the comment
 * margin: a COMMITTED `mission` slot (revisions 2, so the header chip reads
 * 'committed · r2') whose review thread carries the two entry shapes the margin
 * has to place differently —
 *
 *   • an OPEN change request anchored to `$.objectives[2]`, which must sit LEVEL
 *     with that objective's row; and
 *   • an ANSWERED question addressed to `pm` with one agent reply and NO anchor,
 *     which has no row to sit beside and therefore leads the margin in its
 *     UNPLACED group.
 *
 * The thread rides the SESSION view (that is where `reviewThread` lives on the
 * wire — see wire.ts's mapSessionState), so the session probe answers 200 at the
 * terminal `committed` stage rather than the 404 the other committed stubs use.
 * That combination — committed slot + committed session — is exactly the
 * `sessionMissing || stage === 'committed'` arm SystemDesignView renders the
 * committed panel from, so the submit bar's verb reads Amend, not Send back.
 *
 * Everything is fulfilled in-browser: it creates nothing. Returns the project id.
 */
export async function stubCommittedMissionWithThread(page: Page): Promise<string> {
  const projectId = 'comment-margin-fixture';
  const mission = {
    vision: 'Software architecture that a small team can actually hold in its head.',
    objectives: MISSION_OBJECTIVES,
    mission: 'Drive a project from research corpus to constructed system through one reviewed ladder of artifacts.',
  };
  const slots: Slot[] = [
    { kind: 'mission', stage: STAGE_COMMITTED, revisions: 2, model: { kind: 'mission', model: mission } },
  ];

  await stubSessionGate(page);
  await stubGetProject(page, projectId, projectState(projectId, 'Comment Margin Fixture', slots));
  await page.route('**/api/v1/system-design/get-session-state/**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        projectId,
        artifactKind: KIND_ORDINAL.mission,
        stage: STAGE_SESSION_COMMITTED,
        activeRole: 0,
        activeStep: 0,
        round: 1,
        draft: { kind: 'mission', model: mission },
        findings: [],
        reviewThread: [
          {
            id: MARGIN_CHANGE_REQUEST_ID,
            anchor: '$.objectives[2]',
            anchorText: MARGIN_ANCHOR_TEXT,
            text: 'This objective names a feeling, not a measurable outcome. Say what the reviewer can check.',
            authorRole: 'architect-user',
            round: 1,
            status: 'open',
            replies: [],
            reopened: false,
            type: 'changeRequest',
            addressee: '',
          },
          {
            id: MARGIN_QUESTION_ID,
            anchor: '',
            anchorText: '',
            text: 'Is "one day" a business commitment or an aspiration?',
            authorRole: 'architect-user',
            round: 1,
            status: 'answered',
            replies: [
              {
                id: `${MARGIN_QUESTION_ID}-a0`,
                authorRole: 'pm',
                text: 'An aspiration for now — the commitment lands with the first paying customer.',
                at: '2026-09-19T10:00:00Z',
              },
            ],
            reopened: false,
            type: 'question',
            addressee: 'pm',
          },
        ],
      }),
    }),
  );
  return projectId;
}

export const DESIGN_STUB_KIND_ORDINAL = KIND_ORDINAL;
