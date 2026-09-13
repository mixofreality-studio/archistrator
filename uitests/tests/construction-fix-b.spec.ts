/**
 * construction-fix-b.spec — fix round B (designer P1-7, P1-8, fix-A concern 2).
 *
 *  - P1-7  a profile names its own tasks; the book's task KEY rides beside a task
 *          the profile renamed, and the pane's exit line is the profile's own.
 *  - P1-8  every tier-1 row's kind / provenance / progress / state slots line up
 *          under the column header, and a no-record row reads NOT STARTED: a dashed
 *          empty track, a muted 0%, the hollow circle, "not started" — no chip.
 *  - fix-A concern 2: at 1280 with the pane open, ids stay whole AND titles stay
 *          legible; the `≈ RECONSTRUCTED` header badge stays spelled out.
 *  - P1-11 "Observed only" keeps every activity and strips reconstructed evidence,
 *          so a backfilled activity reads NOT STARTED (spec R6) instead of vanishing.
 *  - P2    "Expand to current phase" is disabled, and says why, with nothing in
 *          flight; the pane collapses with a collapse-pane icon and a tooltip.
 *
 * Read-only: nothing here dispatches (no Begin/Run/Retry is pressed).
 * Gated like construction-tracker.spec.ts: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

/** The slot columns every tier-1 row shares with the header. */
const SLOTS = ['kind', 'provenance', 'progress', 'state'] as const;

interface RowGeometry {
  id: string;
  slots: Record<string, number>;
  titleWidth: number;
  idTruncated: boolean;
}

/** One DOM read: each tier-1 row's slot lefts, its title width, and whether its id
 *  is cut; plus the header's slot lefts. */
async function readGeometry(
  page: Page
): Promise<{ header: Record<string, number>; rows: RowGeometry[] }> {
  return page.evaluate(
    ({ headerId, slots, idPrefix, titlePrefix }) => {
      const header = document.querySelector(`[data-testid="${headerId}"]`);
      if (header === null) throw new Error('no list header');
      const lefts = (root: Element): Record<string, number> => {
        const out: Record<string, number> = {};
        for (const s of slots) {
          const el = root.querySelector(`:scope > [data-slot="${s}"]`);
          if (el !== null) out[s] = el.getBoundingClientRect().left;
        }
        return out;
      };
      const rows = Array.from(document.querySelectorAll(`[data-testid^="${idPrefix}"]`)).map(
        (idCell) => {
          const id = (idCell.getAttribute('data-testid') ?? '').slice(idPrefix.length);
          const grid = idCell.closest('[data-slot="idTitle"]')?.parentElement;
          const title = document.querySelector(`[data-testid="${titlePrefix}${id}"]`);
          const el = idCell as HTMLElement;
          return {
            id,
            slots: grid !== null && grid !== undefined ? lefts(grid) : {},
            // The ROOM the title is given (its flex box), not the text's own width:
            // a short title is short, not squeezed.
            titleWidth: title?.parentElement?.getBoundingClientRect().width ?? 0,
            idTruncated: el.scrollWidth > el.clientWidth + 0.5,
          };
        }
      );
      return { header: lefts(header), rows };
    },
    {
      headerId: TESTID.constructionListHeader,
      slots: [...SLOTS],
      idPrefix: TESTID.constructionListIdCell(''),
      titlePrefix: TESTID.constructionListTitleCell(''),
    }
  );
}

for (const width of [1280, 1366, 1600]) {
  test(`at ${String(width)}px with the pane open, every slot lines up under its header and titles stay legible`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await gotoApp(page, '/project/archistrator/construction?lens=list&a=U-SPA-web-client');
    await expect(page.getByTestId(TESTID.constructionDetailPane)).toBeVisible({ timeout: 15_000 });
    await expect(page.getByTestId(TESTID.constructionListHeader)).toBeVisible();
    await page.waitForTimeout(300);

    const g = await readGeometry(page);
    expect(g.rows.length).toBeGreaterThan(20);
    for (const s of SLOTS) expect(g.header[s], `header ${s}`).toBeDefined();
    for (const r of g.rows) {
      for (const s of SLOTS) {
        expect(Math.abs((r.slots[s] ?? -1e6) - (g.header[s] ?? 0)), `${r.id} ${s} vs header`).toBeLessThanOrEqual(1.5);
      }
      expect(r.idTruncated, `${r.id} id is truncated`).toBe(false);
      // Legible: the title never collapses beside a full-width id (fix-A concern 2).
      expect(r.titleWidth, `${r.id} title width`).toBeGreaterThanOrEqual(120);
    }

    // The header badge stays SPELLED OUT, however narrow the list (never an icon).
    const badge = page
      .getByTestId(TESTID.constructionListRow('C-construction-manager'))
      .getByTestId(TESTID.constructionProvenanceBadge);
    await expect(badge).toBeVisible();
    await expect(badge).toContainText(/reconstructed/i);
  });
}

test('a no-record row reads NOT STARTED: dashed empty track, muted 0%, hollow circle, no chip', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1366, height: 900 });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const row = page.getByTestId(TESTID.constructionListRow('C-billing-state-access'));
  await expect(row).toBeVisible({ timeout: 15_000 });

  const read = await row.evaluate((el) => {
    const progress = el.querySelector('[data-slot="progress"]');
    const state = el.querySelector('[data-slot="state"]');
    const track = progress?.firstElementChild as HTMLElement | null | undefined;
    return {
      progressKind: progress?.getAttribute('data-progress-kind'),
      progressText: (progress as HTMLElement | null)?.innerText.trim(),
      trackBorder: track !== null && track !== undefined ? getComputedStyle(track).borderTopStyle : '',
      stateKind: state?.getAttribute('data-state-slot'),
      stateText: (state as HTMLElement | null)?.innerText.trim(),
    };
  });
  expect(read.progressKind).toBe('notStarted');
  expect(read.progressText).toBe('0%');
  expect(read.trackBorder).toBe('dashed');
  expect(read.stateKind).toBe('notStarted');
  expect(read.stateText).toBe('not started');
});

test('a renamed task carries the book’s key, and the pane states the profile’s own exit', async ({
  page,
}) => {
  await gotoApp(page, '/project/archistrator/construction?lens=list&a=N-STP&p=construction&k=codeReview');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });
  // The deep link selects the task; a search on its PROFILE label reveals its row.
  await page.getByTestId(TESTID.constructionLensSearch).getByRole('textbox').fill('scenario review');
  const node = 'N-STP::construction::codeReview';
  const taskRow = page.getByTestId(TESTID.constructionListRow(node));
  await expect(taskRow).toBeVisible({ timeout: 15_000 });
  // N-STP's construction gate is the profile's "Scenario Review", not the book's
  // "Code Review" — whose key rides beside it.
  await expect(taskRow).toContainText('Scenario Review');
  await expect(taskRow).not.toContainText('Code Review');
  await expect(page.getByTestId(TESTID.constructionListTaskBookKey(node))).toHaveText('codeReview');
  // A Service row reads the book and so carries no second key.
  await expect(page.getByTestId(TESTID.constructionDetailExitCriterion)).toContainText(
    'black-box scenarios'
  );
  await expect(page.getByTestId(TESTID.constructionDetailExitCriterion)).not.toContainText(
    'code-complete'
  );
});

test('"Observed only" keeps every activity and reads a backfilled one as not started', async ({
  page,
}) => {
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });
  const ids = page.getByTestId(/^construction-list-id-/);
  const badges = page
    .getByTestId(TESTID.constructionListTree)
    .getByTestId(TESTID.constructionProvenanceBadge);
  const before = await ids.count();
  expect(before).toBeGreaterThan(20);
  expect(await badges.count(), 'badges with the toggle off').toBeGreaterThan(0);

  const cm = page.getByTestId(TESTID.constructionListRow('C-construction-manager'));
  const stateOf = async (): Promise<string | null> =>
    cm.evaluate((el) => el.querySelector('[data-slot="state"]')?.getAttribute('data-state-slot') ?? null);
  expect(await stateOf()).toBe('chip');

  // MUI's Switch renders its input with role=switch.
  const toggle = page.getByRole('switch', { name: 'Observed only' });
  await toggle.check();
  await expect(page.getByText('Observed only')).toBeVisible();
  // Every activity stays (the committed list decides what exists) …
  await expect(ids).toHaveCount(before);
  // … and one known only from backfilled evidence reads not started, with no hatch
  // or badge left to qualify a claim it no longer makes.
  await expect.poll(stateOf).toBe('notStarted');
  await expect(badges).toHaveCount(0);

  await toggle.uncheck();
  await expect.poll(stateOf).toBe('chip');
  expect(await badges.count()).toBeGreaterThan(0);
});

test('"Expand to current phase" is disabled, and says why, when nothing is in flight', async ({
  page,
}) => {
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });
  const expand = page.getByTestId(TESTID.constructionLensExpandToPhase);
  // The seeded project has nothing in construction or in review.
  await expect(expand).toBeDisabled();
  await expand.hover({ force: true });
  await expect(page.getByRole('tooltip')).toContainText(
    'Nothing is in construction or awaiting your review'
  );
});

test('the pane collapses with a collapse-pane icon and a tooltip, and expands back', async ({
  page,
}) => {
  await gotoApp(page, '/project/archistrator/construction?lens=list&a=C-billing-manager');
  await expect(page.getByTestId(TESTID.constructionDetailPane)).toBeVisible({ timeout: 15_000 });
  const toggle = page.getByTestId(TESTID.constructionDetailCollapseToggle);
  // ->| : a collapse-pane mark, not the chevron that read as "next".
  await expect(toggle.getByTestId('LastPageRoundedIcon')).toBeVisible();
  await expect(toggle.getByTestId('ChevronRightRoundedIcon')).toHaveCount(0);
  await toggle.hover();
  await expect(page.getByRole('tooltip')).toContainText('Collapse the detail pane');
  await toggle.click();
  await expect(
    page.getByTestId(TESTID.constructionDetailCollapseToggle).getByTestId('FirstPageRoundedIcon')
  ).toBeVisible();
});
