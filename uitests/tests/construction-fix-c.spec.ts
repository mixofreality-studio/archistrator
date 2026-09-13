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
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ page, request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
  await page.route('**/execute-next-activity/**', (r) => r.abort());
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
  const pane = page.getByTestId(TESTID.constructionDetailPane);
  // Toggle off: the record reads as what it is.
  await expect(chip).toHaveText(/≈\s*RECONSTRUCTED/);

  await page.getByRole('switch', { name: 'Observed only' }).check();
  await expect(chip).toHaveText(`OBSERVED ONLY · ${String(hidden)} reconstructed hidden`);
  await expect(pane).not.toContainText('UNRECORDED');
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
  await expect(chip).toHaveText(
    `OBSERVED ONLY · ${String(hiddenInRequirements)} reconstructed hidden`
  );

  // A row with NO stored record is the one place UNRECORDED belongs.
  await page.getByTestId(TESTID.constructionListRow('C-billing-state-access')).click();
  await expect(chip).toHaveText('UNRECORDED');
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
    await expect(run, suffix).toBeEnabled();
  }
});
