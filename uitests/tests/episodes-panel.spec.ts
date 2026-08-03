/**
 * episodes-panel.spec — the SP1 capture-seam episodes panel on a design
 * artifact page (route `/project/$projectId/design/system`, below the artifact
 * renderer — see webApp SystemDesignView's `episodesSlot`).
 *
 * AC flow:
 *   • the panel lists the artifact's captured episodes;
 *   • a row expands to its lineage tree + timeline, and the timeline shows the
 *     real tool events mined from that episode's captured trace;
 *   • a GAP episode renders its outcome chip AND, expanded, its gap REASON —
 *     never an empty-looking panel. A gap is a present, first-class outcome
 *     ("the badges are the point"), not an absence;
 *   • Export JSON downloads the records + traces;
 *   • Export CSV downloads the flat per-episode ledger under its exact header.
 *
 * ── State this spec needs ───────────────────────────────────────────────────
 * REAL captured episode state on the well-known "archistrator" dogfood project
 * — the same project artifact-systemtest.spec.ts and the use-case-coverage
 * meta-check already read. That means the server behind the SPA proxy must be
 * pointed at a project-state repo seeded from this checkout's
 * `.aiarch/state/project.json` AND holding an episode ledger at
 * `<repoRoot>/.aiarch/traces/`. See uitests/README (Running) and
 * `server/cmd/gen-uitests-episodes` for the exact provisioning.
 *
 * Like every other spec here it SELF-SKIPS (never fails) when that state is
 * absent — CI's deliberately-empty project-state repo has no episodes at all.
 * The two episodes it drives are resolved OFF THE WIRE (fetchDesignEpisodes),
 * not hardcoded, so this spec runs unchanged against episodes produced by a
 * REAL agentic dispatch rather than the seeding tool.
 *
 * Self-contained (workers=1, fullyParallel=false): it creates no project and
 * mutates no state, so it neither depends on nor disturbs any other spec.
 */
import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { TESTID } from './support/testids.js';
import {
  fetchDesignEpisodes,
  gotoApp,
  skipUnlessServer,
  type DesignEpisodes,
} from './support/gating.js';
import { tagUseCase } from './support/useCases.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** The dogfood project + the design step the episodes are recorded against. */
const PROJECT_ID = 'archistrator';
const ARTIFACT_KIND = 'mission';

/**
 * The exact Task-10 CSV header — asserted byte for byte, because downstream
 * ledger consumers (the audit-spine workstream, a customer's own SOC 2 evidence
 * pull) parse this by column position. A silent column rename/reorder is a
 * breaking change to a published export, so it gets an equality assertion, not
 * a `contains`.
 */
const CSV_HEADER =
  'episodeId,kind,targetRef,outcome,model,workerClass,tokensIn,tokensOut,' +
  'cacheRead,cacheCreate,costUsd,numTurns,startedAt,endedAt';

let discovered: DesignEpisodes | undefined;

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  discovered = await fetchDesignEpisodes(request, BASE);
  test.skip(
    discovered === undefined,
    'uitests: no captured episodes (a traced succeeded one AND a gap) on the "archistrator" ' +
      'project behind the SPA proxy — this spec asserts REAL episode-ledger content and cannot ' +
      "run against a stack whose ledger was never provisioned. Point the server's " +
      'ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL at a repo seeded from this checkout, then run ' +
      'server/cmd/gen-uitests-episodes against that same repo (see uitests/README).',
  );
});

/** The episodes resolved in beforeEach; unreachable when the skip above fired. */
function episodes(): DesignEpisodes {
  if (discovered === undefined) {
    throw new Error('episodes-panel.spec: beforeEach should have skipped — no episodes resolved');
  }
  return discovered;
}

/** Opens the design experience on the artifact the episodes are recorded against. */
async function openArtifactPage(page: import('@playwright/test').Page): Promise<void> {
  await gotoApp(page, `/project/${PROJECT_ID}/design/system`);
  await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();
  await page.getByTestId(TESTID.spineStep(ARTIFACT_KIND)).click();
  await expect(page.getByTestId(TESTID.episodesPanel)).toBeVisible();
}

test('the panel lists the artifact captured episodes', async ({ page }) => {
  tagUseCase('drive-system-design');
  const { succeededId, gapId } = episodes();

  await openArtifactPage(page);

  // >=1 row on a design artifact page — and specifically the two the ledger
  // reports for this artifact, each with its outcome chip.
  await expect(page.getByTestId(TESTID.episodesRow(succeededId))).toBeVisible();
  await expect(page.getByTestId(TESTID.episodeOutcomeChip(succeededId))).toHaveText('SUCCEEDED');
  await expect(page.getByTestId(TESTID.episodesRow(gapId))).toBeVisible();
});

test('a row expands to its lineage tree and a timeline of real tool events', async ({ page }) => {
  tagUseCase('drive-system-design');
  const { succeededId } = episodes();

  await openArtifactPage(page);
  await page.getByTestId(TESTID.episodesRow(succeededId)).click();

  // workflow → activity → episode → subagent spans.
  await expect(page.getByTestId(TESTID.episodeLineageTree)).toContainText(succeededId);

  const timeline = page.getByTestId(TESTID.episodeTimeline);
  await expect(timeline).toBeVisible();
  // The timeline is mined from the episode's REAL captured trace: its main loop
  // wrote a file, so the Write tool_use must surface as a tool event row.
  await expect(timeline).toContainText('tool_use · Write');
  // ...and the args summary stays metadata-only — the file's CONTENT is never
  // rendered, only a length hint (Task-10 review minor (a)).
  await expect(timeline).toContainText('file_path=');
  await expect(timeline).not.toContainText('content=hi');
});

test('a gap episode renders its outcome chip and its gap reason', async ({ page }) => {
  tagUseCase('drive-system-design');
  const { gapId, gapReason } = episodes();

  await openArtifactPage(page);

  // The chip is the point: a gap is a PRESENT outcome, badged like any other.
  await expect(page.getByTestId(TESTID.episodeOutcomeChip(gapId))).toHaveText('GAP');

  await page.getByTestId(TESTID.episodesRow(gapId)).click();

  // Expanded, it must explain itself — not read as an empty/broken panel.
  const timeline = page.getByTestId(TESTID.episodeTimeline);
  await expect(timeline).toBeVisible();
  await expect(timeline).toContainText('GAP — no episode ran');
  expect(gapReason.length, 'the ledger should carry a real gap reason').toBeGreaterThan(0);
  await expect(timeline).toContainText(gapReason);
});

test('Export JSON downloads the episode records and their traces', async ({ page }) => {
  tagUseCase('drive-system-design');
  await openArtifactPage(page);

  await page.getByTestId(TESTID.episodesExportMenu).click();
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByTestId(TESTID.episodeExportJson).click(),
  ]);

  expect(download.suggestedFilename()).toBe(`episodes-systemDesign-${ARTIFACT_KIND}.json`);
  const path = await download.path();
  const parsed = JSON.parse(readFileSync(path, 'utf8')) as { records?: unknown[] };
  expect(Array.isArray(parsed.records)).toBe(true);
  expect(parsed.records?.length ?? 0).toBeGreaterThanOrEqual(1);
});

test('Export CSV downloads the flat ledger under its exact header', async ({ page }) => {
  tagUseCase('drive-system-design');
  await openArtifactPage(page);

  await page.getByTestId(TESTID.episodesExportMenu).click();
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByTestId(TESTID.episodeExportCsv).click(),
  ]);

  expect(download.suggestedFilename()).toBe(`episodes-systemDesign-${ARTIFACT_KIND}.csv`);
  const path = await download.path();
  const lines = readFileSync(path, 'utf8').split('\n');
  expect(lines[0]).toBe(CSV_HEADER);
  // One data row per listed episode — the header alone would pass a `contains`.
  expect(lines.length).toBeGreaterThanOrEqual(2);
});
