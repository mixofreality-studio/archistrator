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
      ActivityConstruction: Record<string, unknown>;
    };
    const activityIds = Object.keys(project.ActivityConstruction);
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
