/**
 * construction-begin-confirm.spec — Begin/Resume is decided once, from the
 * session endpoint, and Begin asks before it dispatches (fix round A, designer
 * P0-3 + review I3).
 *
 * WHAT WENT WRONG BEFORE
 * ----------------------
 * The label was guessed from the rows: 23 activities carry backfilled
 * (reconstructed) attempts no pump ever ran, so the console read "Resume
 * construction" on a project whose pump had never started — and because the rows
 * arrive a beat after the page, it read "Begin" first and flipped to "Resume"
 * about 1.1s after load. The session endpoint is the single source now: no
 * activity of the seeded project has a construction session, so the one label is
 * "Begin construction", and it is never shown until every probe has answered.
 *
 * SAFETY: this spec opens the confirm dialog and CANCELS it. It never presses the
 * dispatch button, and it fails if any execute-next-activity request is sent.
 *
 * Gated like construction-tracker.spec.ts: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy.
 */
import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

const COMMITTED_LABEL = /Begin construction|Resume construction/;

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

/** The activities the server reports with no stored record — read from the same
 *  get-project wire the console reads, so the expectation is never hardcoded. */
async function unrecordedActivityIds(
  request: import('@playwright/test').APIRequestContext
): Promise<string[]> {
  const res = await request.get(`${BASE}/api/v1/system-design/get-project/archistrator`, {
    headers: { Accept: 'application/json' },
  });
  expect(res.status()).toBe(200);
  const data = (await res.json()) as {
    ActivityConstruction?: Record<string, { ActivityID: string; recorded: boolean }>;
  };
  return Object.values(data.ActivityConstruction ?? {})
    .filter((r) => !r.recorded)
    .map((r) => r.ActivityID)
    .sort();
}

test('Begin/Resume commits to one label from the session endpoint, and Begin names what it would dispatch before dispatching nothing', async ({
  page,
  request,
}) => {
  const dispatches: string[] = [];
  page.on('request', (r) => {
    if (r.url().includes('/construction/execute-next-activity/')) dispatches.push(r.url());
  });

  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toBeVisible({ timeout: 15_000 });

  // Sample the button through the load. It may say it is checking (disabled), but
  // it may never offer Begin or Resume while disabled-for-loading, and once it is
  // enabled it must hold ONE label — the old Begin→Resume flip fails here.
  // ONE atomic DOM read per sample: reading the label and the enabled state as two
  // separate calls can straddle the Checking→Begin transition and pair the old
  // label with the new state — a false flip the button never showed.
  const samples: { label: string; enabled: boolean }[] = [];
  for (let i = 0; i < 25; i++) {
    samples.push(
      await begin.evaluate((el) => ({
        label: (el as HTMLElement).innerText.trim(),
        enabled: !(el as HTMLButtonElement).disabled,
      }))
    );
    await page.waitForTimeout(100);
  }
  const enabledLabels = new Set(samples.filter((s) => s.enabled).map((s) => s.label));
  expect(enabledLabels.size, `labels once enabled: ${[...enabledLabels].join(' | ')}`).toBe(1);
  for (const s of samples.filter((x) => !x.enabled)) {
    expect(s.label).not.toMatch(COMMITTED_LABEL);
  }

  // No construction session exists for any activity of the seeded project (every
  // per-activity probe answers 404), so the session's answer is Begin — even though
  // 23 activities carry reconstructed attempts.
  await expect(begin).toHaveText(/Begin construction/);
  await expect(begin).toBeEnabled();

  await begin.click();
  const dialog = page.getByTestId(TESTID.constructionBeginConfirm);
  await expect(dialog).toBeVisible();

  const expected = await unrecordedActivityIds(request);
  expect(expected.length).toBeGreaterThan(0);
  for (const id of expected) {
    await expect(page.getByTestId(TESTID.constructionBeginCandidate(id))).toBeVisible();
  }
  const named = await page.evaluate(() =>
    Array.from(document.querySelectorAll('[data-testid^="construction-begin-candidate-"]'))
      .map((el) => (el.getAttribute('data-testid') ?? '').slice('construction-begin-candidate-'.length))
      .sort()
  );
  expect(named).toEqual(expected);

  await page.getByTestId(TESTID.constructionBeginConfirmCancel).click();
  await expect(dialog).toBeHidden();
  expect(dispatches).toEqual([]);
});
