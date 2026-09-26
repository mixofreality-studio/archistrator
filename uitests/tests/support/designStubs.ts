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
 * `stubCreatedProject`, which answers start-project in the browser so the
 * catalog → create → home flow can run without writing anything.
 *
 * The stubbed wire shapes mirror the generated client contract EXACTLY (the SPA's
 * `mapProjectState` decodes them): the project state (DeliveryProjectState) is a
 * PascalCase envelope with `Slots[]` carrying an integer `stage` and a
 * `{kind, model}` model envelope.
 *
 * STAGE 4a: the three reads this module stubbed — get-project, list-projects and
 * get-session-state — are ONE route now, `POST /api/v1/delivery/query-project-view`,
 * and what distinguishes them is the `ProjectViewQuery` in the request BODY. So
 * there is ONE page route, switching on `query.kind`, where there were three URL
 * patterns; each answer is a `DeliveryProjectView` — `{ kind, <member> }` — not the
 * bare body the old per-rail op returned. A stub that forgot the envelope would
 * have the SPA read `undefined` out of a 200 and render an empty screen.
 *
 * This module links ZERO webApp source; the field names are copied as black-box
 * literals, exactly like tests/support/testids.ts.
 */
import { type Page, type Route } from '@playwright/test';

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

/** The ONE merged read. The route is a POST and its selector is the body. */
const QUERY_PROJECT_VIEW = '**/api/v1/delivery/query-project-view';

/** The ONE write that creates (or continues) a project. */
const START_PROJECT = '**/api/v1/delivery/start-project';

/** The `ProjectViewQuery` a query-project-view call carries, as far as a stub reads it. */
function queryOf(route: Route): { kind?: string; projectId?: string } {
  const body: unknown = route.request().postDataJSON();
  if (typeof body !== 'object' || body === null || !('query' in body)) return {};
  const query: unknown = body.query;
  return typeof query === 'object' && query !== null ? query : {};
}

/**
 * stubCreatedProject stands in for a project start-project "just made", without
 * creating anything (fix-F review: landing.spec and design-experience.spec used to
 * POST real creates). In the browser:
 *   • start-project answers `projectId` (the server-minted id the SPA navigates to);
 *   • the project reads as a fresh one: Phase 0, no slots;
 *   • every design-session probe 404s (no session yet), so the first step offers
 *     "Request draft";
 *   • the REAL catalog read comes back with this project's row added.
 * Pair it with the shared dispatch guard, which aborts any other write. Returns the
 * number of creates it answered, so a spec can assert the create was faked.
 *
 * One handler answers all three reads, because one route serves all three: an
 * unhandled kind falls through to the real server rather than being faked, so a
 * screen that starts reading `pump` or `designHealth` fails visibly instead of
 * quietly getting a project summary.
 */
export async function stubCreatedProject(
  page: Page,
  projectId: string,
  name: string,
): Promise<{ creates: number }> {
  const answered = { creates: 0 };
  await page.route(START_PROJECT, (route) => {
    answered.creates += 1;
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(projectId),
    });
  });
  const fresh = projectState(projectId, name, FRESH_PHASE1_KINDS.map(emptySlot));
  await page.route(QUERY_PROJECT_VIEW, async (route) => {
    const { kind, projectId: asked } = queryOf(route);
    if (kind === 'summary' && asked === projectId) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ kind: 'summary', summary: fresh }),
      });
      return;
    }
    if (kind === 'session') {
      // No live co-author session: the probe reads a 404 as "nothing here".
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'no session', code: 'not_found' }),
      });
      return;
    }
    if (kind !== 'projects') {
      await route.fallback();
      return;
    }
    // A catalog read can still be in flight when the test ends. Only "the page has
    // closed" is ignored here; any other failure still fails the test.
    try {
      const response = await route.fetch();
      const view = (await response.json()) as { projects?: unknown[] };
      await route.fulfill({
        response,
        json: {
          kind: 'projects',
          projects: [
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
            ...(view.projects ?? []),
          ],
        },
      });
    } catch (err) {
      if (page.isClosed() || /has been closed/.test(String(err))) return;
      throw err;
    }
  });
  return answered;
}
