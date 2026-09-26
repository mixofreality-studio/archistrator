/**
 * The PREVIEW build (design-renderer-data.md §2′, P1): the SAME archistrator app,
 * its real shell, router, hooks and components, booted in preview mode over the
 * fixture transport. This package's test-local fixtures (../../preview-fixtures)
 * are the data; `?screen=<id>&state=<id>` picks one.
 *
 * What this pins, against the built bundle served statically (the `preview`
 * project in playwright.config.ts; no Go server, no network):
 *   - each fixture state renders through the REAL components (the rows the
 *     PLAN draws are exactly the fixture's activities; the landing's card is
 *     the fixture's project; a pending read holds the real loading state; an
 *     error fixture reaches the real error UI);
 *   - an unfixtured call fails LOUDLY (the alarm, the incident log);
 *   - every other request is blocked by the guard before it leaves the page;
 *   - a nested preview inside a preview is refused;
 *   - memory history: the page URL is never rewritten;
 *   - an unknown state is an honest error page, never a guess.
 *
 * STAGE 5 RETARGET (Task 12). The first two cases used to drive
 * `?screen=construction`, whose `ActivityTreeView` Task 13 deletes. All four
 * guarantees are unchanged — the real screen draws exactly the fixture's
 * activities, an unfixtured call is LOUD, nothing but the bundle is fetched, a
 * nested preview is refused — they are simply asserted over the PLAN's fixtures
 * now, and the unclassified-row guarantee moved with them.
 *
 * STAGE 5 TEARDOWN (Task 13). The construction console is deleted and its route
 * redirects, so the `construction/` fixture screen is gone. Three of its states
 * MOVED to `plan/` unchanged but for their route — `loading` (the pending read),
 * `begin-confirm` (Begin + its confirm, ported onto the plan in Task 11) and
 * `owed-gate` (the TASKS lens, which survives per spec §7.3) — and their cases
 * below moved with them. Two states and their cases are RETIRED with the
 * surface they drove: `service-pane` (the shared DetailPane) and
 * `built-surface-link` (the pane's FrontendArtifactView). The `built-surface-link`
 * case's SCRIPTED half — window.open is refused and opens no second tab — is kept
 * below on `plan/list`; its real-link half (an `<a target="_blank">` the
 * navigation guard refuses) has NO surviving preview vehicle, because no fixture
 * on the plan or the activity screen renders an external link today. That is an
 * EARMARK for stage 6: capture a frontend (`U-SPA-web-client`) activity fixture,
 * and the `navigation-blocked` incident kind gets its black-box guard back.
 */
import { test, expect } from '../support/dispatchGuard.js';
import { TESTID } from '../support/testids.js';
import { fixture, incidents, openState, viewAnswer, viewResult } from '../support/previewShell.js';

/**
 * "Every plan row, whatever its activity id." DERIVED from the id builder, the
 * same way `plan.spec.ts` derives it: a hand-typed `/^plan-row-/` would go on
 * matching nothing — silently, as a count of 0 against a count of 0 — the day
 * `UI_IDENTIFIERS.Plan.row` is renamed.
 */
const PLAN_ROW_RE = new RegExp(`^${TESTID.planRow('')}`);

test.describe('preview shell: the real app over fixtures', () => {
  test('plan · list: the real plan draws exactly the fixture activities', async ({ page }) => {
    const data = fixture('plan', 'list');
    const project = viewResult<{
      Name: string;
      activityExecution: Record<string, unknown>;
    }>(data, 'summary');
    const activityIds = Object.keys(project.activityExecution);
    expect(activityIds.length).toBeGreaterThan(0);

    const offBundle = await openState(page, 'plan', 'list');
    await expect(page.getByTestId(TESTID.planList)).toBeVisible();
    for (const id of activityIds) {
      await expect(page.getByTestId(TESTID.planRow(id))).toBeVisible();
    }
    await expect(page.getByTestId(PLAN_ROW_RE)).toHaveCount(activityIds.length);
    await expect(page.getByText(project.Name, { exact: true }).first()).toBeVisible();

    // Clean: nothing missed, nothing blocked, nothing sent.
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
    // Memory history: the router opened the fixture's route without touching the URL.
    expect(page.url()).toMatch(/\/index\.html\?screen=plan&state=list$/);
    await expect(page).toHaveTitle('Preview · plan · list · fixture data');
  });

  test('plan · unclassified: an activity the classifier refused to type is still LISTED, and draws no mini lifecycle', async ({
    page,
  }) => {
    interface Row {
      ActivityID: string;
      classified: boolean;
      Phases: unknown[];
    }
    const project = viewResult<{ activityExecution: Record<string, Row> }>(
      fixture('plan', 'unclassified'),
      'summary',
    );
    const rows = Object.values(project.activityExecution);
    // The fixture's whole point (spec §9 AC4): exactly one row the server could
    // not type — ClassifyType's ok=false — carrying no phases on the wire.
    const unknownIds = rows.filter((r) => !r.classified).map((r) => r.ActivityID);
    expect(unknownIds).toEqual(['C-usage-access']);
    const unknownId = unknownIds[0] ?? '';
    expect(project.activityExecution[unknownId]?.Phases).toEqual([]);
    // A typed neighbour, as the control below.
    const knownId = rows.find((r) => r.classified && r.Phases.length > 0)?.ActivityID ?? '';
    expect(knownId).not.toEqual('');

    const offBundle = await openState(page, 'plan', 'unclassified');
    // The COMMITTED activity list decides what exists, so the row is there even
    // though the server could not type it.
    const unknown = page.getByTestId(TESTID.planRow(unknownId));
    await expect(unknown).toBeVisible();
    await expect(unknown).toContainText('Unclassified');
    // And it draws NO mini lifecycle: miniLifecycleFromRow returns [] over a row
    // with no phases, which is what the "—" in its place says out loud.
    await expect(unknown.getByTestId(TESTID.lifecycleGraphMini)).toHaveCount(0);
    await expect(unknown).toContainText('—');

    // The control: a typed neighbour DOES draw one, so the count above measures
    // the classification rather than a mini lifecycle nothing renders anywhere.
    const known = page.getByTestId(TESTID.planRow(knownId));
    await expect(known.getByTestId(TESTID.lifecycleGraphMini)).toHaveCount(1);

    // Incidents first: a miss names the op it missed, which a bare alarm count does not.
    expect(await incidents(page)).toEqual([]);
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(offBundle).toEqual([]);
  });

  test('plan · loading: a pending read holds the real loading state', async ({ page }) => {
    const offBundle = await openState(page, 'plan', 'loading');
    await expect(page.getByRole('progressbar').first()).toBeVisible();
    // It stays loading: the fixture never answers.
    await page.waitForTimeout(2_000);
    await expect(page.getByRole('progressbar').first()).toBeVisible();
    await expect(page.getByTestId(TESTID.planList)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('landing · resting: the real catalog shows the fixture project', async ({ page }) => {
    const rows = viewResult<{ ProjectID: string; Name: string }[]>(
      fixture('landing', 'resting'),
      'projects',
    );
    const [summary] = rows ?? [];
    expect(summary).toBeDefined();
    const offBundle = await openState(page, 'landing', 'resting');
    await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
    const card = page.getByTestId(TESTID.projectCard(summary?.ProjectID ?? ''));
    await expect(card).toBeVisible();
    await expect(card).toContainText(summary?.Name ?? '');
    await expect(page.getByTestId(TESTID.errorAlert)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('landing · load-error: an error fixture reaches the real error UI', async ({ page }) => {
    const message = viewAnswer(fixture('landing', 'load-error'), 'projects')?.error?.message;
    expect(message).toBeTruthy();
    const offBundle = await openState(page, 'landing', 'load-error');
    await expect(page.getByTestId(TESTID.projectsLandingScreen)).toBeVisible();
    // The app's ordinary ErrorAlert: "<message> (<code>, HTTP <status>)".
    await expect(page.getByTestId(TESTID.errorAlert)).toContainText(
      `${message ?? ''} (unavailable, HTTP 503)`
    );
    // An error the fixture states is not an incident: nothing missed, nothing blocked.
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });
});

/**
 * Preview P1b: the construction hooks that used to call apiClient directly (the
 * session probe, Begin, the phase decision) now ride the OpsClient, so the
 * preview answers them from fixtures instead of the network guard refusing them.
 * Each case below is a state P1 could not show.
 *
 * Task 13: the two `service-pane` cases are RETIRED with the shared DetailPane.
 * What they held, and where it now lives (or does not):
 *   • "the real pane shows the service contract" → `activity-experience.spec.ts`
 *     §12 mounts `serviceContractRoot` on the activity screen over the same
 *     `C-billing-state-access` contract, so the renderer keeps its preview guard;
 *   • "a live session makes the gate read *awaiting you*" → the activity screen
 *     says this with `lifecycleNode(...)`'s `aria-current="step"` plus the gate's
 *     own submit bar (§2a/§2b), not with a pane state chip;
 *   • "Approve runs the REAL mutation over the fixture transport and a 4xx reads
 *     *Rejected*" → NOT covered anywhere. EARMARK: no preview fixture answers a
 *     submit with an error, so the app's 4xx→rejection mapping has no black-box
 *     guard left after this commit (`construction-tasks-lens.spec.ts` pinned the
 *     REST half and is deleted in this same commit).
 */
test.describe('preview shell: Begin and the owed gate (P1b)', () => {
  const GATE = 'C-billing-state-access';

  test('plan · begin-confirm: Begin opens the real confirm; Cancel closes it and dispatches nothing', async ({
    page,
  }) => {
    const data = fixture('plan', 'begin-confirm');
    // A dispatch from this state must be LOUD: execute-next-activity has no fixture.
    expect(data.ops['deliveryExecuteNextActivity']).toBeUndefined();
    const project = viewResult<{
      activityExecution: Record<string, { ActivityID: string; recorded: boolean }>;
    }>(data, 'summary');
    const unrecorded = Object.values(project.activityExecution)
      .filter((r) => !r.recorded)
      .map((r) => r.ActivityID)
      .sort();
    expect(unrecorded.length).toBeGreaterThan(0);

    const offBundle = await openState(page, 'plan', 'begin-confirm');
    const begin = page.getByTestId(TESTID.constructionBegin);
    await expect(begin).toHaveText(/Begin construction/);
    await expect(begin).toBeEnabled();
    await begin.click();
    const dialog = page.getByTestId(TESTID.constructionBeginConfirm);
    await expect(dialog).toBeVisible();
    for (const id of unrecorded) {
      await expect(page.getByTestId(TESTID.constructionBeginCandidate(id))).toBeVisible();
    }
    await expect(dialog.getByTestId(/^construction-begin-candidate-/)).toHaveCount(
      unrecorded.length
    );

    await page.getByTestId(TESTID.constructionBeginConfirmCancel).click();
    await expect(dialog).toBeHidden();
    await expect(begin).toHaveText(/Begin construction/);
    await page.waitForTimeout(300);
    // Nothing dispatched: a dispatch would have been a fixture miss.
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('plan · owed-gate: the real TASKS lens owes exactly the fixture gate', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'plan', 'owed-gate');
    await expect(page.getByTestId(TESTID.constructionTasksLens)).toBeVisible();
    const row = page.getByTestId(TESTID.constructionTasksRow(`${GATE}:gate`));
    await expect(row).toBeVisible();
    await expect(row).toContainText('Design Review');
    // The reviewers come from the session fixture, through the migrated probe.
    await expect(row).toContainText('system-architect');
    await expect(page.getByTestId(TESTID.constructionTasksHeadline)).toContainText('1 decision');
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });
});

test.describe('preview shell: loud failures and closed doors', () => {
  test('an unfixtured call fails LOUDLY: the alarm names the op', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') errors.push(msg.text());
    });
    const offBundle = await openState(page, 'plan', 'unfixtured-read');
    const alarm = page.getByTestId(TESTID.previewAlarm);
    await expect(alarm).toBeVisible();
    await expect(alarm).toContainText('fixture-miss: deliveryQueryProjectView(summary)');
    await expect
      .poll(() => incidents(page))
      .toContainEqual({
        kind: 'fixture-miss',
        detail: 'deliveryQueryProjectView(summary)',
      });
    expect(errors.some((e) => e.includes('fixture-miss: deliveryQueryProjectView(summary)'))).toBe(
      true,
    );
    // A miss is not a silent network fallback: nothing left the page.
    expect(offBundle).toEqual([]);
  });

  test('every other request is blocked by the guard before it leaves the page', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'plan', 'list');
    await expect(page.getByTestId(TESTID.planList)).toBeVisible();

    const outcome = await page.evaluate(async () => {
      const result: Record<string, string> = {};
      try {
        await fetch('/api/v1/delivery/query-project-view');
        result['fetch'] = 'sent';
      } catch (e) {
        result['fetch'] = e instanceof Error ? `${e.name}: ${e.message}` : String(e);
      }
      try {
        new XMLHttpRequest();
        result['xhr'] = 'constructed';
      } catch (e) {
        result['xhr'] = e instanceof Error ? e.name : String(e);
      }
      try {
        new WebSocket('ws://localhost:1/socket');
        result['ws'] = 'constructed';
      } catch (e) {
        result['ws'] = e instanceof Error ? e.name : String(e);
      }
      return result;
    });
    expect(outcome['fetch']).toMatch(
      /^PreviewNetworkBlockedError: archistrator-preview-network-guard: fetch /
    );
    expect(outcome['xhr']).toBe('PreviewNetworkBlockedError');
    expect(outcome['ws']).toBe('PreviewNetworkBlockedError');

    // Blocked in the page, so nothing reached the network, and the alarm says so.
    expect(offBundle).toEqual([]);
    await expect(page.getByTestId(TESTID.previewAlarm)).toContainText('network-blocked: fetch');
    const kinds = (await incidents(page))?.map((i) => i.kind);
    expect(kinds).toEqual(['network-blocked', 'network-blocked', 'network-blocked']);
  });

  test('the nested preview is off: a frame inside the preview is refused', async ({ page }) => {
    const offBundle = await openState(page, 'plan', 'list');
    await expect(page.getByTestId(TESTID.planList)).toBeVisible();
    const violated = await page.evaluate(
      () =>
        new Promise<string>((resolve) => {
          document.addEventListener('securitypolicyviolation', (e) => {
            resolve(e.effectiveDirective);
          });
          const frame = document.createElement('iframe');
          frame.src = '/index.html?screen=landing&state=resting';
          document.body.appendChild(frame);
          setTimeout(() => {
            resolve('no violation');
          }, 3_000);
        })
    );
    expect(violated).toBe('frame-src');
    expect(offBundle).toEqual([]);
  });

  /**
   * Task 13: what is left of "the nested preview is off in the pane". The pane
   * that carried the `<a target="_blank">` is deleted, and no surviving preview
   * fixture renders an external link (the plan's TASKS rows carry a GitHub link
   * only when the row has a `prUrl`, and neither `plan/tasks` nor `plan/owed-gate`
   * does). So the SCRIPTED refusal is kept — window.open throws and opens no
   * second tab — and the real-link half is EARMARKED for stage 6, together with
   * the `navigation-blocked` incident kind it was the only cover for.
   */
  test('a scripted window.open is refused and opens no second tab', async ({ page, context }) => {
    const opened: string[] = [];
    context.on('page', (p) => opened.push(p.url()));
    const offBundle = await openState(page, 'plan', 'list');
    await expect(page.getByTestId(TESTID.planList)).toBeVisible();

    const scripted = await page.evaluate(() => {
      try {
        window.open('/index.html?screen=landing&state=resting');
        return 'opened';
      } catch (e) {
        return e instanceof Error ? e.name : String(e);
      }
    });
    expect(scripted).toBe('PreviewNetworkBlockedError');

    // No second tab or window ever opened, and nothing left the page.
    await page.waitForTimeout(500);
    expect(opened).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('an unknown state is an honest error page listing what the build carries', async ({
    page,
  }) => {
    await openState(page, 'plan', 'no-such-state');
    const errorPage = page.getByTestId(TESTID.previewErrorPage);
    await expect(errorPage).toBeVisible();
    await expect(errorPage).toContainText('Screen "plan" has no state "no-such-state".');
    await expect(errorPage.getByRole('link', { name: 'plan · list' })).toBeVisible();
    await expect(errorPage.getByRole('link', { name: 'landing · load-error' })).toBeVisible();
    // It never guessed a state: the app did not boot.
    await expect(page.getByTestId(TESTID.planList)).toHaveCount(0);
  });
});
