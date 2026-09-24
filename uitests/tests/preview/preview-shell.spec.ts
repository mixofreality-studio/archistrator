/**
 * The PREVIEW build (design-renderer-data.md §2′, P1): the SAME archistrator app,
 * its real shell, router, hooks and components, booted in preview mode over the
 * fixture transport. This package's test-local fixtures (../../preview-fixtures)
 * are the data; `?screen=<id>&state=<id>` picks one.
 *
 * What this pins, against the built bundle served statically (the `preview`
 * project in playwright.config.ts; no Go server, no network):
 *   - each fixture state renders through the REAL components (the rows the
 *     console draws are exactly the fixture's activities; the landing's card is
 *     the fixture's project; a pending read holds the real loading state; an
 *     error fixture reaches the real error UI);
 *   - an unfixtured call fails LOUDLY (the alarm, the incident log);
 *   - every other request is blocked by the guard before it leaves the page;
 *   - a nested preview inside a preview is refused;
 *   - memory history: the page URL is never rewritten;
 *   - an unknown state is an honest error page, never a guess.
 */
import { readFileSync } from 'node:fs';
import type { Page, Request } from '@playwright/test';
import { test, expect } from '../support/dispatchGuard.js';
import { TESTID } from '../support/testids.js';

const FIXTURES = new URL('../../preview-fixtures/web-client/', import.meta.url);

interface FixtureFile {
  route: string;
  ops: Record<string, { result?: unknown; error?: { message?: string } }>;
}

function fixture(screen: string, state: string): FixtureFile {
  return JSON.parse(
    readFileSync(new URL(`${screen}/${state}.json`, FIXTURES), 'utf8')
  ) as FixtureFile;
}

interface Incident {
  kind: 'fixture-miss' | 'network-blocked' | 'navigation-blocked';
  detail: string;
}

function incidents(page: Page): Promise<Incident[] | null> {
  return page.evaluate(
    () =>
      (
        window as unknown as {
          __ARCHISTRATOR_PREVIEW__?: { incidents: Incident[] };
        }
      ).__ARCHISTRATOR_PREVIEW__?.incidents ?? null
  );
}

/**
 * Every request the page makes that is not the static bundle itself. A preview
 * answers from fixtures, so this must stay empty.
 */
function watchNetwork(page: Page): string[] {
  const offBundle: string[] = [];
  page.on('request', (req: Request) => {
    const url = new URL(req.url());
    const isBundle =
      url.pathname === '/index.html' ||
      url.pathname.startsWith('/assets/') ||
      url.protocol === 'data:';
    if (!isBundle) offBundle.push(`${req.method()} ${req.url()}`);
  });
  return offBundle;
}

async function openState(page: Page, screen: string, state: string): Promise<string[]> {
  const offBundle = watchNetwork(page);
  await page.goto(`/index.html?screen=${screen}&state=${state}`);
  return offBundle;
}

test.describe('preview shell: the real app over fixtures', () => {
  test('construction · resting: the real console draws exactly the fixture activities', async ({
    page,
  }) => {
    const data = fixture('construction', 'resting');
    const project = data.ops['systemDesignGetProject']?.result as {
      Name: string;
      activityExecution: Record<string, unknown>;
    };
    const activityIds = Object.keys(project.activityExecution);
    expect(activityIds.length).toBeGreaterThan(0);

    const offBundle = await openState(page, 'construction', 'resting');
    await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible();
    for (const id of activityIds) {
      await expect(page.getByTestId(TESTID.constructionListRow(id))).toBeVisible();
    }
    await expect(page.getByTestId(/^construction-list-row-/)).toHaveCount(activityIds.length);
    await expect(page.getByText(project.Name, { exact: true }).first()).toBeVisible();

    // Clean: nothing missed, nothing blocked, nothing sent.
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
    // Memory history: the router opened the fixture's route without touching the URL.
    expect(page.url()).toMatch(/\/index\.html\?screen=construction&state=resting$/);
    await expect(page).toHaveTitle('Preview · construction · resting · fixture data');
  });

  test('construction · unclassified-row: an activity the classifier refused to type renders UNCLASSIFIED with zero lifecycle sub-rows', async ({
    page,
  }) => {
    interface Row {
      ActivityID: string;
      classified: boolean;
      Phases: unknown[];
    }
    const project = fixture('construction', 'unclassified-row').ops['systemDesignGetProject']
      ?.result as { activityExecution: Record<string, Row> };
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

    const offBundle = await openState(page, 'construction', 'unclassified-row');
    const unknown = page.getByTestId(TESTID.constructionListRow(unknownId));
    await expect(unknown).toBeVisible();
    // It says what it is, and never guesses a kind.
    await expect(unknown).toContainText('UNCLASSIFIED');

    // ArrowRight is the tree's expand gesture. Nothing opens: an unclassified
    // activity has no phase and no task rows to open. The sub-row ids are the
    // tree's own (`<activityId>::<phase>[::<task>]`).
    await unknown.click();
    await page.keyboard.press('ArrowRight');
    await expect(
      page.getByTestId(new RegExp(`^construction-list-row-${unknownId}::`))
    ).toHaveCount(0);

    // The control: the SAME gesture on a typed activity does open its lifecycle,
    // so the count above measures the classification, not a dead keystroke.
    const known = page.getByTestId(TESTID.constructionListRow(knownId));
    await known.click();
    await page.keyboard.press('ArrowRight');
    await expect(
      page.getByTestId(new RegExp(`^construction-list-row-${knownId}::`)).first()
    ).toBeVisible();

    // Incidents first: a miss names the op it missed, which a bare alarm count does not.
    expect(await incidents(page)).toEqual([]);
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(offBundle).toEqual([]);
  });

  test('construction · loading: a pending read holds the real loading state', async ({ page }) => {
    const offBundle = await openState(page, 'construction', 'loading');
    await expect(page.getByRole('progressbar').first()).toBeVisible();
    // It stays loading: the fixture never answers.
    await page.waitForTimeout(2_000);
    await expect(page.getByRole('progressbar').first()).toBeVisible();
    await expect(page.getByTestId(TESTID.constructionListTree)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('landing · resting: the real catalog shows the fixture project', async ({ page }) => {
    const [summary] = fixture('landing', 'resting').ops['systemDesignListProjects']?.result as {
      ProjectID: string;
      Name: string;
    }[];
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
    const message = fixture('landing', 'load-error').ops['systemDesignListProjects']?.error
      ?.message;
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
 */
test.describe('preview shell: the construction detail, Begin and the owed gate (P1b)', () => {
  const GATE = 'C-billing-state-access';

  test('construction · service-pane: the real pane shows the service contract and the live session', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'construction', 'service-pane');
    const pane = page.getByTestId(TESTID.constructionDetailPane);
    await expect(pane).toBeVisible();
    // The session fixture (awaiting approval) reached the pane through the
    // migrated session probe: that is what makes the gate "awaiting you".
    await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(/awaiting you/i);
    const contract = pane.getByTestId(TESTID.serviceContractRoot);
    await expect(contract).toBeVisible();
    await expect(contract).toContainText('billingStateAccess');
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('construction · service-pane: Approve runs the real mutation over the fixture transport; its 409 reads "Rejected"', async ({
    page,
  }) => {
    const data = fixture('construction', 'service-pane');
    expect(data.ops['constructionSubmitPhaseDecision']?.error?.message).toBeTruthy();
    const offBundle = await openState(page, 'construction', 'service-pane');
    await page.getByTestId(TESTID.constructionDetailAction('approve')).click();
    // A 4xx is a rejection (the same mapping construction-tasks-lens.spec pins on REST).
    await expect(page.getByTestId(TESTID.constructionDetailDecisionFlow)).toContainText('Rejected');
    // The fixture answered it: not a miss, not a blocked request, nothing sent.
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('construction · begin-confirm: Begin opens the real confirm; Cancel closes it and dispatches nothing', async ({
    page,
  }) => {
    const data = fixture('construction', 'begin-confirm');
    // A dispatch from this state must be LOUD: execute-next-activity has no fixture.
    expect(data.ops['constructionExecuteNextActivity']).toBeUndefined();
    const project = data.ops['systemDesignGetProject']?.result as {
      activityExecution: Record<string, { ActivityID: string; recorded: boolean }>;
    };
    const unrecorded = Object.values(project.activityExecution)
      .filter((r) => !r.recorded)
      .map((r) => r.ActivityID)
      .sort();
    expect(unrecorded.length).toBeGreaterThan(0);

    const offBundle = await openState(page, 'construction', 'begin-confirm');
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

  test('construction · owed-gate: the real TASKS lens owes exactly the fixture gate', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'construction', 'owed-gate');
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
    const offBundle = await openState(page, 'construction', 'unfixtured-read');
    const alarm = page.getByTestId(TESTID.previewAlarm);
    await expect(alarm).toBeVisible();
    await expect(alarm).toContainText('fixture-miss: systemDesignGetProject');
    await expect
      .poll(() => incidents(page))
      .toContainEqual({
        kind: 'fixture-miss',
        detail: 'systemDesignGetProject',
      });
    expect(errors.some((e) => e.includes('fixture-miss: systemDesignGetProject'))).toBe(true);
    // A miss is not a silent network fallback: nothing left the page.
    expect(offBundle).toEqual([]);
  });

  test('every other request is blocked by the guard before it leaves the page', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'construction', 'resting');
    await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible();

    const outcome = await page.evaluate(async () => {
      const result: Record<string, string> = {};
      try {
        await fetch('/api/v1/system-design/get-project/archistrator');
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
    const offBundle = await openState(page, 'construction', 'resting');
    await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible();
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

  test('the nested preview is off in the pane: its "Open in a new tab" link is refused', async ({
    page,
    context,
  }) => {
    const opened: string[] = [];
    context.on('page', (p) => opened.push(p.url()));
    const offBundle = await openState(page, 'construction', 'built-surface-link');

    // The REAL detail pane (FrontendArtifactView), rendering the fixture's ui-code route.
    await expect(page.getByTestId(TESTID.constructionFrontendView)).toBeVisible();
    const link = page.getByTestId(TESTID.constructionFrontendOpenLink);
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute('target', '_blank');

    await link.click();
    await expect(page.getByTestId(TESTID.previewAlarm)).toContainText('navigation-blocked:');
    await expect
      .poll(() => incidents(page))
      .toEqual([
        {
          kind: 'navigation-blocked',
          detail: expect.stringContaining(
            '/project/archistrator/construction'
          ) as unknown as string,
        },
      ]);

    // The scripted form is refused too.
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
    await openState(page, 'construction', 'no-such-state');
    const errorPage = page.getByTestId(TESTID.previewErrorPage);
    await expect(errorPage).toBeVisible();
    await expect(errorPage).toContainText('Screen "construction" has no state "no-such-state".');
    await expect(errorPage.getByRole('link', { name: 'construction · resting' })).toBeVisible();
    await expect(errorPage.getByRole('link', { name: 'landing · load-error' })).toBeVisible();
    // It never guessed a state: the app did not boot.
    await expect(page.getByTestId(TESTID.constructionListTree)).toHaveCount(0);
  });
});
