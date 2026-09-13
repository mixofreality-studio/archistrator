/**
 * construction-graph-m0.spec — the GRAPH lens's M0 gate (PM Q4 ruling),
 * black-box. Every expectation is computed from the project read:
 *
 *   - STATE is the project's phase (Construction → passed, earlier → not
 *     passed, unreadable → "—"), never the SDP review's contents;
 *   - the amber "basis changed" flag is the SDP review slot's staleBasis, with
 *     its cause and the count of stale Project Design artifacts in the hover;
 *   - "Observed only" does not touch M0;
 *   - no date, option, cost or duration;
 *   - "Open the SDP review →" is navigation only.
 *
 * DISPATCH SAFETY: every non-GET request is aborted before any navigation, by the
 * context-level guard (support/dispatchGuard), never a per-page route.
 */
import type { APIRequestContext, Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';
const PROJECT_DESIGN_KINDS = new Set([
  'planningAssumptions',
  'activityList',
  'network',
  'normalSolution',
  'decompressedSolution',
  'subcriticalSolution',
  'compressedSolution',
  'riskModel',
  'sdpReview',
]);

test.beforeEach(async ({ request }) => {
  // DISPATCH SAFETY: support/dispatchGuard's context route aborts every
  // non-GET before any navigation (and any request a test still holds, at teardown).
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface Slot {
  kind: string;
  staleBasis?: boolean;
  staleBasisCause?: { upstreamKind?: string; upstreamRevision?: number };
  model: { model: Record<string, unknown> };
}
interface M0Truth {
  state: 'passed' | 'not passed' | '—';
  stale: boolean;
  causeRevision: number | undefined;
  staleCount: number;
  gates: number;
}

async function m0Truth(request: APIRequestContext): Promise<M0Truth> {
  const res = await request.get(`${BASE}/api/v1/system-design/get-project/archistrator`);
  expect(res.ok()).toBe(true);
  const body = (await res.json()) as { PhaseName?: string; Slots: Slot[] };
  const sdp = body.Slots.find((s) => s.kind === 'sdpReview');
  const network = body.Slots.find((s) => s.kind === 'network')?.model.model as
    | { dependencies?: { dependsOn?: string[] | null }[] }
    | undefined;
  const phase = body.PhaseName;
  return {
    state:
      phase === 'construction'
        ? 'passed'
        : phase === 'systemDesign' || phase === 'projectDesign'
          ? 'not passed'
          : '—',
    stale: sdp?.staleBasis === true,
    causeRevision: sdp?.staleBasisCause?.upstreamRevision,
    staleCount: body.Slots.filter((s) => PROJECT_DESIGN_KINDS.has(s.kind) && s.staleBasis === true)
      .length,
    gates: (network?.dependencies ?? []).filter((d) => (d.dependsOn ?? []).includes('M0')).length,
  };
}

function expectedChip(t: M0Truth): string {
  const parts = ['M0', 'SDP review', t.state];
  if (t.state === 'passed' && t.stale) parts.push('basis changed');
  parts.push(`gates ${String(t.gates)}`);
  return parts.join(' · ');
}

async function openGraph(page: Page): Promise<void> {
  await page.setViewportSize({ width: 1366, height: 768 });
  await gotoApp(page, GRAPH);
  await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
}

function chip(page: Page): ReturnType<Page['getByTestId']> {
  return page.getByTestId(TESTID.constructionGraphMilestone('M0'));
}

test('M0 states the project phase, flags a stale SDP approval in amber, and counts what it gates', async ({
  page,
  request,
}) => {
  const t = await m0Truth(request);
  await openGraph(page);
  await expect(chip(page)).toHaveText(expectedChip(t));
  await expect(chip(page)).toHaveAttribute(
    'data-m0-state',
    t.state === 'passed' ? 'passed' : t.state === 'not passed' ? 'notPassed' : 'unknown'
  );
  await expect(chip(page)).toHaveAttribute('data-stale', String(t.state === 'passed' && t.stale));
});

test('the M0 hover is the PM copy — cause, k, and no date, option or cost', async ({
  page,
  request,
}) => {
  const t = await m0Truth(request);
  await openGraph(page);
  await chip(page).hover();
  const hover = page.getByTestId(TESTID.constructionGraphM0Hover);
  await expect(hover).toBeVisible();
  if (t.state === 'passed' && t.stale) {
    await expect(hover).toContainText('M0 — SDP review passed; the plan has changed since');
    if (t.causeRevision !== undefined) {
      await expect(hover).toContainText(`(revision ${String(t.causeRevision)}) was amended`);
    }
    await expect(hover).toContainText(
      `${String(t.staleCount)} Project Design artifacts, including the SDP review, are marked stale`
    );
    await expect(hover).toContainText('This does not block construction.');
    await expect(page.getByTestId(TESTID.constructionGraphM0OpenSdp)).toHaveText(
      'Open the SDP review →'
    );
  } else if (t.state === 'passed') {
    await expect(hover).toContainText(`${String(t.gates)} activities start here.`);
  }
  const text = (await hover.textContent()) ?? '';
  expect(text).not.toMatch(/\d{4}-\d{2}-\d{2}|\$|weeks?\b/i);
});

test('"Observed only" does not touch M0', async ({ page, request }) => {
  const t = await m0Truth(request);
  await openGraph(page);
  await page.getByRole('switch', { name: 'Observed only' }).click();
  await expect(chip(page)).toHaveText(expectedChip(t));
});

test('"Open the SDP review →" navigates to the SDP review, and does nothing else', async ({
  page,
  request,
}) => {
  const t = await m0Truth(request);
  test.skip(!(t.state === 'passed' && t.stale), 'the link appears only on a stale approval');
  await openGraph(page);
  await chip(page).hover();
  await page.getByTestId(TESTID.constructionGraphM0OpenSdp).click();
  await expect.poll(() => new URL(page.url()).pathname).toMatch(/\/design\/project\/sdp-review$/);
});

test('keyboard: the M0 chip is a button — Enter opens its copy, Tab reaches "Open the SDP review →", Escape returns focus', async ({
  page,
  request,
}) => {
  const t = await m0Truth(request);
  await openGraph(page);
  const c = chip(page);
  await expect(c).toHaveRole('button');
  await page.keyboard.press('Shift');
  await c.focus();
  await page.keyboard.press('Enter');
  const pop = page.getByTestId(TESTID.constructionGraphM0Popover);
  await expect(pop).toBeVisible();
  await expect(pop).toContainText('M0 — SDP review');
  // One surface at a time: the hover tooltip is not also on screen.
  await expect(page.getByTestId(TESTID.constructionGraphM0Hover)).toHaveCount(0);
  test.skip(!(t.state === 'passed' && t.stale), 'the link appears only on a stale approval');

  const link = page.getByTestId(TESTID.constructionGraphM0OpenSdp);
  await page.keyboard.press('Tab');
  await expect(link).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(pop).toHaveCount(0);
  await expect(c).toBeFocused();

  await page.keyboard.press('Enter');
  await expect(pop).toBeVisible();
  await page.keyboard.press('Tab');
  await expect(link).toBeFocused();
  await page.keyboard.press('Enter');
  await expect.poll(() => new URL(page.url()).pathname).toMatch(/\/design\/project\/sdp-review$/);
});

test('the M0 chip keeps the spaces around its " · " separators (designer re-check 5)', async ({
  page,
}) => {
  await openGraph(page);
  const widths = await chip(page).evaluate((el) =>
    Array.from(el.querySelectorAll('[data-m0-separator]')).map((sep) => {
      const probe = document.createElement('span');
      probe.style.font = getComputedStyle(sep).font;
      probe.style.whiteSpace = 'pre';
      probe.textContent = '·';
      document.body.append(probe);
      const dot = probe.getBoundingClientRect().width;
      probe.remove();
      return { sep: sep.getBoundingClientRect().width, dot };
    })
  );
  expect(widths.length, 'the chip has separators').toBeGreaterThanOrEqual(3);
  // Each separator draws its two spaces: clearly wider than the bare dot.
  for (const w of widths) expect(w.sep).toBeGreaterThan(w.dot * 2);
});
