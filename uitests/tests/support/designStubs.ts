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

/** Phase-1 ArtifactKind ordinals (openapi enum order — mirrors PHASE1_ORDER in
 * webApp/src/contracts/methodMetadata.ts). */
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

/**
 * stubCommittedCoreUseCases stubs a project whose ONLY committed Phase-1 slot is
 * `coreUseCases`, carrying the REAL committed model extracted from this repo's
 * dogfood project.json (testdata/coreUseCasesProject.json). Its session probes 404
 * (no live co-author session), so the step renders the committed artifact through
 * the CommittedArtifactPanel → UseCaseCarousel → walkthrough. Returns the project id.
 */
export async function stubCommittedCoreUseCases(page: Page): Promise<string> {
  const fixture = JSON.parse(
    readFileSync(new URL('../../testdata/coreUseCasesProject.json', import.meta.url), 'utf8'),
  ) as { ProjectID: string; Slots: Slot[] };
  const projectId = fixture.ProjectID;

  await stubSessionGate(page);
  await stubGetProject(page, projectId, fixture);
  // Every session probe 404s: committed slot, no live co-author session (R6).
  await page.route('**/api/v1/system-design/get-session-state/**', (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'no session', code: 'not_found' }),
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
  const upstream = [
    'mission',
    'glossary',
    'scrubbedRequirements',
    'volatilities',
    'coreUseCases',
  ].map((k) => committedSlot(k, {}));

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

export const DESIGN_STUB_KIND_ORDINAL = KIND_ORDINAL;
