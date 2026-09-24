/**
 * construction-pending-notes.spec — an operator note no agent dispatch has carried
 * (B1 fix round, item 12). A note is pending from the moment it is recorded until an
 * agent dispatch carries it; a failed scaffold sync leaves a send-back's note pending
 * at the gate. The console says so:
 *   - work left, nothing running: "Note pending — the last dispatch did not start";
 *   - Done or Skipped:            "Note never delivered — no agent ran after it".
 *
 * Neither the seeded corpus nor the live one carries a pending note, and seeding one
 * would perturb the rows other specs pin (the only unfinished recorded rows are the
 * integration-pending pair). So each case EDITS the get-project read on its way to
 * the page, adding `operatorNotes` exactly as the server sends them — the pattern
 * construction-resume.spec uses for the pause. It runs the same in both modes.
 *
 * SAFETY: the shared dispatch guard aborts every non-GET; nothing here writes.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** Integration-pending (waiting): work left, nothing runs it. */
const WAITING = 'C-billing-manager';
/** Every task passed: Done. */
const DONE = 'C-episode-access';
/** No note at all. */
const QUIET = 'C-billing-engine';

const AT = '2026-09-14T10:00:00Z';

/** The notes each edited row carries, as the wire sends them (kind 1 sendBack, 2
 *  retry, 5 skip). Only undelivered, non-skip notes are pending. */
const NOTES: Record<string, unknown[]> = {
  [WAITING]: [
    { noteId: `${WAITING}:note:r1:1`, kind: 1, gate: 'integration', text: 'tighten the retry budget', recordedAt: AT },
    { noteId: `${WAITING}:note:r1:2`, kind: 2, gate: 'takeover', text: 'the fixture is back', recordedAt: AT },
    {
      noteId: `${WAITING}:note:r0:1`,
      kind: 2,
      gate: 'takeover',
      text: 'already carried',
      recordedAt: AT,
      deliveredToAttemptId: `${WAITING}:construction:1`,
      deliveredAt: AT,
    },
  ],
  [DONE]: [
    { noteId: `${DONE}:note:r1:1`, kind: 2, gate: 'takeover', text: 'too late for the run', recordedAt: AT },
    { noteId: `${DONE}:note:r1:2`, kind: 5, gate: 'takeover', text: 'a skip note is never pending', recordedAt: AT },
  ],
};

/** Serve every get-project read with NOTES added to their rows. */
async function addNotesToTheRead(page: Page): Promise<void> {
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const fetchWire = async () => {
      const response = await route.fetch();
      return { response, wire: (await response.json()) as Record<string, unknown> };
    };
    try {
      let got: Awaited<ReturnType<typeof fetchWire>>;
      try {
        got = await fetchWire();
      } catch (err) {
        // A fetched body can be disposed before json() reads it; the read is an
        // idempotent GET on a still-unhandled route, so it is fetched once more.
        if (page.isClosed() || !/has been disposed/.test(String(err))) throw err;
        got = await fetchWire();
      }
      const rows = got.wire.activityExecution as Record<string, Record<string, unknown>> | null;
      for (const [id, notes] of Object.entries(NOTES)) {
        const row = rows?.[id];
        if (row !== undefined) row.operatorNotes = notes;
      }
      await route.fulfill({ response: got.response, json: got.wire });
    } catch (err) {
      if (page.isClosed() || /has been closed/.test(String(err))) return;
      throw err;
    }
  });
}

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

test('the list marks a row whose note did not reach an agent, and a Done row whose note never did', async ({
  page,
}) => {
  await addNotesToTheRead(page);
  await gotoApp(page, '/project/archistrator/construction?lens=list');

  const waiting = page.getByTestId(TESTID.constructionListPendingNote(WAITING));
  await expect(waiting).toBeVisible();
  // Two pending (the delivered one is not): the plural, in the awaiting register.
  await expect(waiting).toHaveText('✎2');
  await expect(waiting).toHaveAttribute('aria-label', '2 notes pending — the last dispatch did not start');
  await expect(waiting).toHaveAttribute('data-note-kind', 'didNotStart');

  const done = page.getByTestId(TESTID.constructionListPendingNote(DONE));
  await expect(done).toHaveText('✎1');
  await expect(done).toHaveAttribute('aria-label', 'Note never delivered — no agent ran after it');
  await expect(done).toHaveAttribute('data-note-kind', 'neverDelivered');

  await expect(page.getByTestId(TESTID.constructionListPendingNote(QUIET))).toHaveCount(0);

  await waiting.hover();
  await expect(page.getByRole('tooltip')).toHaveText(
    "2 notes pending — the last dispatch did not start. The operator's note has not reached an agent yet. It rides this activity's next dispatch."
  );
});

test('the pane says the last dispatch did not start, with why on hover', async ({ page }) => {
  await addNotesToTheRead(page);
  await gotoApp(page, `/project/archistrator/construction?lens=list&a=${WAITING}`);

  const line = page.getByTestId(TESTID.constructionDetailPendingNote);
  await expect(line).toHaveText('2 notes pending — the last dispatch did not start');
  await line.hover();
  await expect(page.getByRole('tooltip')).toHaveText(
    "The operator's note has not reached an agent yet. It rides this activity's next dispatch."
  );
});

test('the pane says a Done activity’s note was never delivered', async ({ page }) => {
  await addNotesToTheRead(page);
  await gotoApp(page, `/project/archistrator/construction?lens=list&a=${DONE}`);

  const line = page.getByTestId(TESTID.constructionDetailPendingNote);
  await expect(line).toHaveText('Note never delivered — no agent ran after it');
  await line.hover();
  await expect(page.getByRole('tooltip')).toHaveText(
    "Recorded after the last agent run. It rides this activity's next dispatch if the activity is re-queued."
  );
});

test('an activity with no pending note says nothing about notes', async ({ page }) => {
  await addNotesToTheRead(page);
  await gotoApp(page, `/project/archistrator/construction?lens=list&a=${QUIET}`);
  await expect(page.getByTestId(TESTID.constructionDetailBreadcrumb)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionDetailPendingNote)).toHaveCount(0);
});
