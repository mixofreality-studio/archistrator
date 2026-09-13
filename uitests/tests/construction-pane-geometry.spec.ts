/**
 * construction-pane-geometry.spec — the detail pane pins below the lens toolbar
 * as MEASURED, and no row shows above the stuck toolbar (fix round A, designer
 * P0-1 and P0-2).
 *
 * WHAT WENT WRONG BEFORE
 * ----------------------
 *  - P0-1: the pane pinned at a constant 76px under a 100vh height cap. The
 *    toolbar wraps to ~86px at 1280/1366, so the pane slid under it, and 100vh
 *    ignored the app chrome above the scroller, so the action bar ("Run this
 *    task") ran off the bottom of the screen — worst on N-STP with its Code
 *    Review task selected and Scope = "Unknown only".
 *  - P0-2: the scroller had 24px of top padding, so rows scrolled through a strip
 *    ABOVE the stuck toolbar.
 *
 * Every measurement is one DOM read (page.evaluate), relative to the real
 * scroller: the console scrolls an inner container, never the window.
 *
 * Gated like construction-tracker.spec.ts: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface Geometry {
  viewportH: number;
  scrollTop: number;
  scrollerTop: number;
  scrollerBottom: number;
  toolbarTop: number;
  toolbarBottom: number;
  stuck: string | null;
  paneTop: number | null;
  runTop: number | null;
  runBottom: number | null;
  /** Rows whose visible part lies between the scroller's top and the toolbar's. */
  rowsAboveToolbar: string[];
}

async function measure(page: Page, scrollTo?: number | 'bottom'): Promise<Geometry> {
  return page.evaluate(
    ({ toolbarId, paneId, runId, target }) => {
      const byId = (id: string): Element | null =>
        document.querySelector(`[data-testid="${id}"]`);
      const toolbar = byId(toolbarId);
      if (toolbar === null) throw new Error('no lens toolbar');
      // The real scroller: the toolbar's nearest overflow:auto/scroll ancestor.
      let scroller: HTMLElement | null = toolbar.parentElement;
      while (scroller !== null && !/(auto|scroll)/.test(getComputedStyle(scroller).overflowY)) {
        scroller = scroller.parentElement;
      }
      if (scroller === null) throw new Error('no scroller above the lens toolbar');
      if (target !== null) {
        scroller.scrollTop = target === 'bottom' ? scroller.scrollHeight : target;
        // A synchronous scroll event so the shell's stuck flag is current.
        scroller.dispatchEvent(new Event('scroll'));
      }
      const s = scroller.getBoundingClientRect();
      const tb = toolbar.getBoundingClientRect();
      const pane = byId(paneId)?.getBoundingClientRect() ?? null;
      const run = byId(runId)?.getBoundingClientRect() ?? null;
      const rowsAboveToolbar = Array.from(
        document.querySelectorAll('[data-testid^="construction-list-row-"]')
      )
        .filter((el) => {
          // Only the part of a row actually VISIBLE between the scroller's top
          // edge and the toolbar's top edge counts: a row half-scrolled past a
          // flush-stuck toolbar starts above it and ends below the scroller's
          // top, but that overlap is hidden BEHIND the toolbar, not above it.
          const r = el.getBoundingClientRect();
          const visible = Math.min(r.bottom, tb.top) - Math.max(r.top, s.top);
          return r.height > 0 && visible > 0.5;
        })
        .map((el) => el.getAttribute('data-testid') ?? '');
      return {
        viewportH: window.innerHeight,
        scrollTop: scroller.scrollTop,
        scrollerTop: s.top,
        scrollerBottom: s.bottom,
        toolbarTop: tb.top,
        toolbarBottom: tb.bottom,
        stuck: toolbar.getAttribute('data-stuck'),
        paneTop: pane?.top ?? null,
        runTop: run?.top ?? null,
        runBottom: run?.bottom ?? null,
        rowsAboveToolbar,
      };
    },
    {
      toolbarId: TESTID.constructionLensToolbar,
      paneId: TESTID.constructionDetailPane,
      runId: TESTID.constructionDetailActionRun,
      target: scrollTo ?? null,
    }
  );
}

function expectPaneClearsToolbarAndActionBarOnScreen(g: Geometry, where: string): void {
  expect(g.paneTop, `${where}: pane rendered`).not.toBeNull();
  expect(g.runTop, `${where}: action bar rendered`).not.toBeNull();
  // Never under the toolbar, whatever height it wrapped to.
  expect(g.paneTop!, `${where}: pane top vs toolbar bottom`).toBeGreaterThanOrEqual(
    g.toolbarBottom - 0.5
  );
  // The action bar is wholly on screen: below the toolbar, above the scroller's
  // bottom edge and the viewport's.
  expect(g.runTop!, `${where}: action bar top`).toBeGreaterThanOrEqual(g.toolbarBottom - 0.5);
  expect(g.runBottom!, `${where}: action bar bottom vs scroller`).toBeLessThanOrEqual(
    g.scrollerBottom + 0.5
  );
  expect(g.runBottom!, `${where}: action bar bottom vs viewport`).toBeLessThanOrEqual(
    g.viewportH + 0.5
  );
}

for (const width of [1280, 1366, 1600]) {
  test(`at ${String(width)}px the pinned pane clears the toolbar and keeps "Run this task" on screen — N-STP Code Review, Scope = Unknown only`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    await gotoApp(
      page,
      '/project/archistrator/construction?lens=list&a=N-STP&p=construction&k=codeReview'
    );
    await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });
    await page.getByTestId(TESTID.constructionLensScope).getByRole('combobox').click();
    await page.getByRole('option', { name: 'Unknown only' }).click();
    await expect(page.getByTestId(TESTID.constructionDetailActionRun)).toBeVisible();
    await page.waitForTimeout(250);

    // Fix-A review I3: the RESIZE OBSERVER keeps the published toolbar height
    // current. "Unknown only" re-wrapped the toolbar just now and nothing has
    // scrolled since — measure() below fires a synthetic scroll, which would mask a
    // dead observer — so the value is read here, before any scroll.
    const published = await page.evaluate(
      ({ toolbarId, detailId }) => {
        const toolbar = document.querySelector(`[data-testid="${toolbarId}"]`);
        const detail = document.querySelector(`[data-testid="${detailId}"]`);
        if (toolbar === null || detail === null) throw new Error('no toolbar or detail slot');
        return {
          value: getComputedStyle(detail).getPropertyValue('--lens-toolbar-h').trim(),
          height: Math.ceil(toolbar.getBoundingClientRect().height),
        };
      },
      { toolbarId: TESTID.constructionLensToolbar, detailId: TESTID.constructionLensDetail }
    );
    expect(published.value, 'published toolbar height at rest').toBe(`${String(published.height)}px`);

    expectPaneClearsToolbarAndActionBarOnScreen(await measure(page, 0), 'at rest');
    const bottom = await measure(page, 'bottom');
    await page.waitForTimeout(150);
    expectPaneClearsToolbarAndActionBarOnScreen(await measure(page), 'scrolled to the bottom');
    // When the page could scroll at all, nothing may show above the stuck toolbar.
    if (bottom.scrollTop > 0) expect(bottom.rowsAboveToolbar).toEqual([]);
  });
}

test('at 1280px no row shows above the stuck toolbar, which carries its stuck state, and the pane still clears it', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await gotoApp(page, '/project/archistrator/construction?lens=list&a=C-construction-manager');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });
  // Only to lengthen the page: the reveal expands every matched activity.
  await page.getByTestId(TESTID.constructionLensSearch).getByRole('textbox').fill('srs review');
  await page.waitForTimeout(600);

  const atRest = await measure(page, 0);
  expect(atRest.stuck).toBe('false');

  const scrolled = await measure(page, 400);
  expect(scrolled.scrollTop, 'the page really scrolled').toBeGreaterThan(100);
  expect(scrolled.stuck).toBe('true');
  // P0-2: the stuck toolbar sits at the scroller's own top edge, so there is no
  // strip above it for rows to show through.
  expect(scrolled.toolbarTop - scrolled.scrollerTop).toBeLessThanOrEqual(1);
  expect(scrolled.rowsAboveToolbar).toEqual([]);
  expectPaneClearsToolbarAndActionBarOnScreen(scrolled, 'scrolled 400px');

  // Fix-A review I4: the geometry is written on the DETAIL SLOT's wrapper, never on
  // the shell root the whole list inherits from (a write there restyled ~4k
  // elements per scroll step) — and once the pane is pinned, scrolling further
  // changes no value, so it writes nothing at all.
  const writes = await page.evaluate(
    async ({ toolbarId, detailId }) => {
      const toolbar = document.querySelector(`[data-testid="${toolbarId}"]`);
      const detail = document.querySelector<HTMLElement>(`[data-testid="${detailId}"]`);
      if (toolbar === null || detail === null) throw new Error('no toolbar or detail slot');
      const root = toolbar.parentElement;
      let scroller: HTMLElement | null = toolbar.parentElement;
      while (scroller !== null && !/(auto|scroll)/.test(getComputedStyle(scroller).overflowY)) {
        scroller = scroller.parentElement;
      }
      if (root === null || scroller === null) throw new Error('no shell root or scroller');
      let mutations = 0;
      const observer = new MutationObserver((records) => {
        mutations += records.length;
      });
      observer.observe(detail, { attributes: true, attributeFilter: ['style'] });
      for (let i = 1; i <= 10; i++) {
        scroller.scrollTop = 400 + i * 15;
        scroller.dispatchEvent(new Event('scroll'));
        await new Promise((resolve) => requestAnimationFrame(() => resolve(undefined)));
      }
      await new Promise((resolve) => setTimeout(resolve, 100));
      observer.disconnect();
      return {
        rootInline: root.style.getPropertyValue('--lens-toolbar-h'),
        detailInline: detail.style.getPropertyValue('--lens-toolbar-h'),
        mutations,
      };
    },
    { toolbarId: TESTID.constructionLensToolbar, detailId: TESTID.constructionLensDetail }
  );
  expect(writes.rootInline, 'nothing is written on the shell root').toBe('');
  expect(writes.detailInline, 'the detail slot carries the geometry').not.toBe('');
  expect(writes.mutations, 'style writes while scrolling a pinned pane').toBe(0);
});
