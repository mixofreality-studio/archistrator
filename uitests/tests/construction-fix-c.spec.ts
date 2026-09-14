/**
 * construction-fix-c.spec — fix round C (designer re-check B1/B2).
 *
 *  - B1  With "Observed only" on, a RECORDED row whose evidence the toggle set
 *        aside is not an unrecorded one. The pane's chip reads
 *        "OBSERVED ONLY · N reconstructed hidden" and its body says the N attempts
 *        are hidden — never UNRECORDED, never "No record… has not run". UNRECORDED
 *        stays where the row really is `recorded: false`.
 *  - B2  The run action names what is selected ("Run this activity / phase /
 *        task") and carries ↻ only where the selection holds an attempt, ▶ else.
 *
 * SAFETY: every execute-next-activity request is TRAPPED and ABORTED by
 * page.route before the page opens. Nothing here presses Run, Begin or Retry.
 * Gated like construction-tracker.spec.ts: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy.
 */
import type { APIRequestContext, Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

// Every non-GET is aborted by the shared dispatch guard (support/dispatchGuard).
test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface WireAttempt {
  task: string;
  phase: string;
  provenance: { origin: string };
}
interface WireRow {
  ActivityID: string;
  recorded: boolean;
  attempts?: WireAttempt[];
}

/** The rows as the server sends them — so every count below is read, not hardcoded. */
async function wireRows(request: APIRequestContext): Promise<Record<string, WireRow>> {
  const res = await request.get(`${BASE}/api/v1/system-design/get-project/archistrator`, {
    headers: { Accept: 'application/json' },
  });
  expect(res.status()).toBe(200);
  const data = (await res.json()) as { ActivityConstruction?: Record<string, WireRow> };
  return data.ActivityConstruction ?? {};
}

function reconstructed(row: WireRow | undefined, phase?: string): number {
  return (row?.attempts ?? []).filter(
    (a) => a.provenance.origin !== 'observed' && (phase === undefined || a.phase === phase)
  ).length;
}

async function openList(page: Page, suffix = ''): Promise<void> {
  await page.setViewportSize({ width: 1280, height: 900 });
  await gotoApp(page, `/project/archistrator/construction?lens=list${suffix}`);
  await expect(page.getByTestId(TESTID.constructionListRow('C-billing-engine'))).toBeVisible({
    timeout: 15_000,
  });
}

test('B1: Observed only says how many reconstructed attempts it hid, and never calls a recorded row unrecorded', async ({
  page,
  request,
}) => {
  const rows = await wireRows(request);
  const recorded = rows['C-billing-engine'];
  expect(recorded?.recorded).toBe(true);
  const hidden = reconstructed(recorded);
  const hiddenInRequirements = reconstructed(recorded, 'requirements');
  expect(hidden).toBeGreaterThan(0);
  expect(hiddenInRequirements).toBeGreaterThan(0);
  expect(rows['C-billing-state-access']?.recorded).toBe(false);

  await openList(page, '&a=C-billing-engine');
  const chip = page.getByTestId(TESTID.constructionDetailProvenanceChip);
  // The hidden count has its own chip, BESIDE the grade chip (fix-C review).
  const hiddenChip = page.getByTestId(TESTID.constructionDetailObservedOnlyChip);
  const pane = page.getByTestId(TESTID.constructionDetailPane);
  // Toggle off: the record reads as what it is.
  await expect(chip).toHaveText(/≈\s*RECONSTRUCTED/);
  await expect(hiddenChip).toHaveCount(0);

  await page.getByRole('switch', { name: 'Observed only' }).check();
  await expect(hiddenChip).toHaveText(`OBSERVED ONLY · ${String(hidden)} reconstructed hidden`);
  // Nothing observed remains, so there is no grade to state — never UNRECORDED.
  await expect(chip).toHaveCount(0);
  await expect(pane).not.toContainText('UNRECORDED');
  // B2 under Observed only (designer ruling): the stripped row reads not started,
  // so the run is a first run, ▶ — the hidden-count chip beside it says why.
  await expect(page.getByTestId(TESTID.constructionDetailActionRun)).toHaveText(
    '▶ Run this activity'
  );
  const body = page.getByTestId(TESTID.constructionDetailBodyUnknown);
  await expect(body).toContainText(
    `Nothing observed. ${String(hidden)} reconstructed attempts are hidden by Observed only — turn it off to see them.`
  );
  await expect(body).not.toContainText('No record');
  await expect(body).not.toContainText('Nothing is recorded');

  // Scoped to the selection: a phase counts only its own hidden attempts. A full
  // load resets the toolbar store, so the toggle is set again.
  await openList(page, '&a=C-billing-engine&p=requirements');
  await page.getByRole('switch', { name: 'Observed only' }).check();
  await expect(hiddenChip).toHaveText(
    `OBSERVED ONLY · ${String(hiddenInRequirements)} reconstructed hidden`
  );

  // A row with NO stored record is the one place UNRECORDED belongs.
  await page.getByTestId(TESTID.constructionListRow('C-billing-state-access')).click();
  await expect(chip).toHaveText('UNRECORDED');
  await expect(hiddenChip).toHaveCount(0);
  await expect(pane).not.toContainText('OBSERVED ONLY');
});

test('B2: the run action names its selection, and marks a re-run only where an attempt exists', async ({
  page,
  request,
}) => {
  const rows = await wireRows(request);
  expect(rows['U-SPA-web-client']?.attempts ?? []).toHaveLength(0);
  expect((rows['C-billing-engine']?.attempts ?? []).length).toBeGreaterThan(0);

  const run = page.getByTestId(TESTID.constructionDetailActionRun);
  const cases: [string, string][] = [
    ['&a=C-billing-engine', '↻ Run this activity'],
    ['&a=C-billing-engine&p=requirements', '↻ Run this phase'],
    ['&a=C-billing-engine&p=requirements&k=srs', '↻ Run this task'],
    ['&a=U-SPA-web-client', '▶ Run this activity'],
    ['&a=U-SPA-web-client&p=requirements', '▶ Run this phase'],
    ['&a=U-SPA-web-client&p=requirements&k=srs', '▶ Run this task'],
  ];
  for (const [suffix, label] of cases) {
    await openList(page, suffix);
    await expect(run, suffix).toHaveText(label);
    // Present everywhere, but OFF with its reason: the console cannot start work
    // yet, and an enabled no-op is the lie (designer P1-2, ruled 2026-09-12).
    await expect(run, suffix).toBeDisabled();
    await expect(run, suffix).toHaveAttribute('data-reason', /not wired/i);
  }
});

// ---------------------------------------------------------------------------
// N1–N5 (designer re-check, non-blocking)
// ---------------------------------------------------------------------------

test('N1: a deep link to a task opens its ancestors and brings the task row into view', async ({
  page,
}) => {
  await openList(page, '&a=N-STP&p=construction&k=codeReview');
  const taskRow = page.getByTestId(TESTID.constructionListRow('N-STP::construction::codeReview'));
  // No search, no click: the link alone reveals the row.
  await expect(taskRow).toBeVisible();
  await expect(taskRow).toBeInViewport();
  await page.waitForTimeout(700);
  const read = await page.evaluate(
    ({ rowId, toolbarId }) => {
      const row = document.querySelector(`[data-testid="${rowId}"]`)?.getBoundingClientRect();
      const bar = document.querySelector(`[data-testid="${toolbarId}"]`)?.getBoundingClientRect();
      return { rowTop: row?.top ?? -1, rowBottom: row?.bottom ?? -1, barBottom: bar?.bottom ?? 0, vh: window.innerHeight };
    },
    {
      rowId: TESTID.constructionListRow('N-STP::construction::codeReview'),
      toolbarId: TESTID.constructionLensToolbar,
    }
  );
  // Not merely "in the viewport": clear of the sticky toolbar that covers the top.
  expect(read.rowTop).toBeGreaterThanOrEqual(read.barBottom);
  expect(read.rowBottom).toBeLessThanOrEqual(read.vh);
});

test('N2: the book key trails the gate tag behind a separator, and never repeats the label', async ({
  page,
}) => {
  await openList(page, '&a=U-SPA-web-client&p=test_plan&k=stpReview');
  const flowReview = 'U-SPA-web-client::test_plan::stpReview';
  const row = page.getByTestId(TESTID.constructionListRow(flowReview));
  await expect(row).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionListTaskBookKey(flowReview))).toHaveText('stpReview');
  // Label, then the gate tag, then "· key" — not "Flow Review stpReview gate".
  expect((await row.innerText()).replace(/\s+/g, ' ')).toMatch(/Flow Review gate · stpReview/);

  // "Flow Testing" would only repeat `testing`: no key at all.
  await openList(page, '&a=U-SPA-web-client&p=integration&k=testing');
  const flowTesting = 'U-SPA-web-client::integration::testing';
  await expect(page.getByTestId(TESTID.constructionListRow(flowTesting))).toContainText('Flow Testing');
  await expect(page.getByTestId(TESTID.constructionListTaskBookKey(flowTesting))).toHaveCount(0);
});

test('N3: a task under a classified, not-started activity reads NOT STARTED, not UNKNOWN', async ({
  page,
}) => {
  await openList(page, '&a=U-SPA-web-client&p=requirements&k=srs');
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText('NOT STARTED');
});

test('N4: the Begin confirm names "the N activities with nothing recorded yet"', async ({
  page,
  request,
}) => {
  const rows = await wireRows(request);
  const n = Object.values(rows).filter((r) => !r.recorded).length;
  expect(n).toBeGreaterThan(1);
  await openList(page);
  await page.getByTestId(TESTID.constructionBegin).click();
  const dialog = page.getByTestId(TESTID.constructionBeginConfirm);
  await expect(dialog).toContainText(`the ${String(n)} activities with nothing recorded yet:`);
  await page.getByTestId(TESTID.constructionBeginConfirmCancel).click();
  await expect(dialog).toBeHidden();
});

test('N5: Begin is disabled in EVERY committed frame until constructionStarted is known', async ({
  page,
}) => {
  // Not sampled: a MutationObserver records the button's (label, disabled) after
  // every DOM change, so no frame can fall between two reads. The designer's
  // "enabled Checking…" frame came from reading the label and the disabled state
  // as two separate calls across the Checking→Begin commit.
  await page.addInitScript((beginId: string) => {
    const log: string[] = [];
    (window as unknown as { __beginLog: string[] }).__beginLog = log;
    const record = (): void => {
      const el = document.querySelector<HTMLButtonElement>(`[data-testid="${beginId}"]`);
      if (el === null) return;
      const entry = `${el.innerText.trim()}|${el.disabled ? 'disabled' : 'enabled'}`;
      if (log[log.length - 1] !== entry) log.push(entry);
    };
    new MutationObserver(record).observe(document, {
      subtree: true,
      childList: true,
      attributes: true,
      characterData: true,
    });
  }, TESTID.constructionBegin);
  await openList(page);
  await expect(page.getByTestId(TESTID.constructionBegin)).toHaveText(/Begin construction/);
  await expect(page.getByTestId(TESTID.constructionBegin)).toBeEnabled();
  const log = await page.evaluate(() => (window as unknown as { __beginLog: string[] }).__beginLog);
  expect(log.length, 'the observer saw the button').toBeGreaterThan(0);
  for (const entry of log) {
    const [label = '', flag] = entry.split('|');
    if (/Begin construction|Continue construction/.test(label)) continue;
    // Anything that is not the committed label is a disabled state.
    expect(flag, `"${label}" was ${flag ?? '?'}`).toBe('disabled');
  }
  const enabledLabels = new Set(log.filter((e) => e.endsWith('|enabled')).map((e) => e.split('|')[0]));
  expect([...enabledLabels]).toEqual(['Begin construction']);
});

test('N5: at 1600 with the pane open, header labels keep a gutter and fit their slots', async ({
  page,
}) => {
  await openList(page, '&a=C-billing-engine');
  await page.setViewportSize({ width: 1600, height: 900 });
  await page.waitForTimeout(400);
  const cells = await page.evaluate((headerId) => {
    const header = document.querySelector(`[data-testid="${headerId}"]`);
    if (header === null) throw new Error('no list header');
    const text = (el: Element | null): { left: number; right: number; box: number } => {
      if (el === null) throw new Error('missing header cell');
      const r = document.createRange();
      r.selectNodeContents(el);
      const t = r.getBoundingClientRect();
      return { left: t.left, right: t.right, box: el.getBoundingClientRect().width };
    };
    const idTitle = header.children[4];
    return {
      float: text(header.querySelector('[data-slot="float"]')),
      effort: text(header.querySelector('[data-slot="effort"]')),
      id: text(idTitle?.children[0] ?? null),
    };
  }, TESTID.constructionListHeader);
  for (const [name, c] of Object.entries(cells)) {
    expect(c.right - c.left, `${name} fits its slot`).toBeLessThanOrEqual(c.box + 0.5);
  }
  expect(cells.effort.left - cells.float.right, 'float→effort gutter').toBeGreaterThanOrEqual(8);
  expect(cells.id.left - cells.effort.right, 'effort→id gutter').toBeGreaterThanOrEqual(8);
});

for (const width of [1280, 1366, 1600]) {
  test(`N5: at ${String(width)} with the pane open, the search placeholder is never clipped`, async ({
    page,
  }) => {
    await openList(page, '&a=C-billing-engine');
    await page.setViewportSize({ width, height: 900 });
    await page.waitForTimeout(400);
    const read = await page.evaluate((searchId) => {
      const input = document.querySelector<HTMLInputElement>(`[data-testid="${searchId}"] input`);
      if (input === null) throw new Error('no search input');
      const cs = getComputedStyle(input);
      const ctx = document.createElement('canvas').getContext('2d');
      if (ctx === null) throw new Error('no canvas');
      ctx.font = cs.font;
      return {
        text: ctx.measureText(input.placeholder).width,
        room: input.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight),
      };
    }, TESTID.constructionLensSearch);
    expect(read.text, `placeholder ${String(read.text)}px in ${String(read.room)}px`).toBeLessThanOrEqual(
      read.room + 0.5
    );
  });
}

test('N5: a disabled "Expand to current phase" looks disabled', async ({ page }) => {
  await openList(page);
  const expand = page.getByTestId(TESTID.constructionLensExpandToPhase);
  await expect(expand).toBeDisabled();
  const style = await expand.evaluate((el) => {
    const cs = getComputedStyle(el);
    return { opacity: parseFloat(cs.opacity), border: cs.borderTopStyle };
  });
  expect(style.opacity).toBeLessThanOrEqual(0.5);
  expect(style.border).toBe('dashed');
});
