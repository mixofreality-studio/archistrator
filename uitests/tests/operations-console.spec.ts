/**
 * operations-console.spec — the Operations console
 * (route `/operations/$operatedAppId`), the UC4 "Operate a Delivered System"
 * console (see .coreUseCases in project.json).
 *
 * This repo's own "archistrator" project is the aiarch PLATFORM itself, still
 * in construction — it has no operated end-user app, and no project in this
 * checkout has one (OPERATE is Phase 4, post-delivery). So there is no real
 * `operatedAppId` to seed a "happy path" against. What IS real and exercisable
 * today: the console is wired into the router and its runtime-status read
 * (readRuntimeStatus / GET .../query-operated-system-view) is built to 404
 * gracefully and render an HONEST awaiting state — by design, per
 * OperationsConsole.tsx's own doc comment ("Every tab degrades to an honest
 * awaiting state rather than an error when the read is quiet"). Navigating to
 * ANY operatedAppId and observing that honest awaiting state IS the accurate
 * exercise of this use case against this repo's real data: no operated
 * system exists, and the UI says so rather than erroring or faking one.
 *
 * Pure-UI: needs only the SPA + a Postgres-backed dev server (the runtime-
 * status read 404s cleanly with no operated-system infra provisioned).
 */
import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, gotoApp } from './support/gating.js';
import { tagUseCase } from './support/useCases.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

test('the Operations console renders the honest awaiting state for an unoperated app', async ({
  page,
}) => {
  tagUseCase('operate-a-delivered-system');

  await gotoApp(page, '/operations/uitest-unoperated-app');
  await expect(page.getByTestId(TESTID.operationsRoot)).toBeVisible();
  await expect(page.getByTestId(TESTID.operationsTabStatus)).toBeVisible();

  // No such operated app exists (real 404) — the Status tab degrades to the
  // honest awaiting panel, not an error and not fabricated data.
  await expect(page.getByTestId(TESTID.operationsAwaiting)).toBeVisible({ timeout: 15_000 });
});
