/**
 * Route-intercept stubs for a FRESH project, without creating one.
 *
 * This module used to carry the System Design co-author experience's whole stub
 * set — a committed glossary, committed volatilities, a committed architecture
 * with a deployment topology, an awaiting-review gate, two draft-failure states
 * and a mission with a review thread — so a spec could render one specific state
 * of the design rail hermetically, with no Go server behind the proxy. Stage 5
 * §7.4 deleted that rail (`/project/$id/design/system|project` now redirect to
 * the plan), and Task 13 deleted the ten specs that drove it, so every one of
 * those stubs lost its last caller and went with them.
 *
 * What is left is the ONE stub that has nothing to do with the design rail:
 * `stubCreatedProject`, which answers create-project in the browser so the
 * catalog → create → home flow can run without writing anything.
 *
 * The stubbed wire shapes mirror the generated client contract EXACTLY (the SPA's
 * `mapProjectState` decodes them): GetProject (SystemDesignProjectState) is a
 * PascalCase envelope with `Slots[]` carrying an integer `stage` and a
 * `{kind, model}` model envelope.
 *
 * This module links ZERO webApp source; the field names are copied as black-box
 * literals, exactly like tests/support/testids.ts.
 */
import { type Page } from '@playwright/test';

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

/** ArtifactStage ordinal for a slot nothing has been drafted into yet. */
const STAGE_EMPTY = 0;

/** The Phase-1 slots a freshly created project reads back with: every one listed,
 *  every one empty. get-project lists every slot kind, empty ones included, and the
 *  home base's table of contents is built from that list. */
const FRESH_PHASE1_KINDS = ['mission', 'glossary', 'volatilities', 'coreUseCases', 'system'];

function emptySlot(kind: string): Slot {
  return { kind, stage: STAGE_EMPTY, revisions: 0, model: { kind } };
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

/** Every session probe 404s: no live co-author session. */
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
