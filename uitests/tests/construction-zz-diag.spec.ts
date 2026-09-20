/**
 * TEMPORARY — a one-run probe, to be deleted once its numbers are read.
 *
 * N5 and S4 failed on CI's Linux runner and passed on macOS. This prints what
 * each of them measures, on whatever platform it runs, so the divergence is read
 * rather than guessed: the header labels' rendered widths against their slots,
 * which face actually drew them, and the code canvas's frame against its node.
 * It asserts nothing — it only reports.
 */
import { test } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface LabelReading {
  text: string | null;
  width: number;
  slot: number;
  font: string;
  size: string;
  tracking: string;
}

interface HeaderReading {
  float: LabelReading;
  effort: LabelReading;
  floatToEffort: number;
  columnGap: string;
  listWidth: number;
  spaceMonoLoaded: boolean;
  jetbrainsLoaded: boolean;
  fontsStatus: string;
}

test('DIAG: the header labels, as this platform draws them', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await gotoApp(page, '/project/archistrator/construction?lens=list&a=C-billing-engine');
  await page.getByTestId(TESTID.constructionListRow('C-billing-engine')).waitFor({ timeout: 20_000 });
  await page.setViewportSize({ width: 1600, height: 900 });
  await page.evaluate(() => document.fonts.ready.then(() => true));
  await page.waitForTimeout(400);
  const reading = await page.evaluate(
    ([headerId, treeId]): HeaderReading => {
      const header = document.querySelector(`[data-testid="${headerId}"]`);
      if (header === null) throw new Error('no list header');
      const read = (el: Element | null): LabelReading & { left: number; right: number } => {
        if (el === null) throw new Error('missing header cell');
        const range = document.createRange();
        range.selectNodeContents(el);
        const rect = range.getBoundingClientRect();
        const cs = getComputedStyle(el);
        return {
          text: el.textContent,
          width: rect.width,
          slot: el.getBoundingClientRect().width,
          font: cs.fontFamily,
          size: cs.fontSize,
          tracking: cs.letterSpacing,
          left: rect.left,
          right: rect.right,
        };
      };
      const float = read(header.querySelector('[data-slot="float"]'));
      const effort = read(header.querySelector('[data-slot="effort"]'));
      const tree = document.querySelector(`[data-testid="${treeId}"]`);
      return {
        float,
        effort,
        floatToEffort: effort.left - float.right,
        columnGap: getComputedStyle(header).columnGap,
        listWidth: tree instanceof HTMLElement ? tree.getBoundingClientRect().width : -1,
        spaceMonoLoaded: document.fonts.check('700 9px "Space Mono"'),
        jetbrainsLoaded: document.fonts.check('700 9px "JetBrains Mono"'),
        fontsStatus: document.fonts.status,
      };
    },
    [TESTID.constructionListHeader, TESTID.constructionListTree] as const
  );
  console.log('DIAG-N5', JSON.stringify(reading));
});

test('DIAG: the code canvas against its drawing', async ({ page }) => {
  await page.setViewportSize({ width: 1760, height: 950 });
  await gotoApp(
    page,
    '/project/archistrator/construction?lens=list&a=C-construction-manager&p=detailed_design&k=detailedDesign&focus=1'
  );
  const focus = page.getByTestId(TESTID.constructionFocusView);
  const canvas = focus.getByTestId(TESTID.serviceContractCodeCanvas);
  const iface = canvas.getByTestId(TESTID.serviceContractCodeInterfaceNode);
  const frame = canvas.getByTestId(TESTID.serviceContractCodeCanvasFrame);
  await iface.waitFor({ timeout: 20_000 });
  const report = async (label: string): Promise<void> => {
    const [i, f] = [await iface.boundingBox(), await frame.boundingBox()];
    const declared = await frame.getAttribute('data-canvas-height');
    const fonts = await page.evaluate(() => ({
      status: document.fonts.status,
      spaceMono: document.fonts.check('700 10.5px "Space Mono"'),
    }));
    console.log(
      'DIAG-S4',
      label,
      JSON.stringify({
        node: i?.height,
        frame: f?.height,
        declared,
        band: i !== null && f !== null ? f.height - i.height : null,
        top: i !== null && f !== null ? i.y - f.y : null,
        fonts,
      })
    );
  };
  await report('first-look');
  await page.evaluate(() => document.fonts.ready.then(() => true));
  await page.waitForTimeout(2000);
  await report('after-fonts');
});
