/**
 * The stage-5 LIVE SMOKE: the plan and one activity, against a real running
 * server, over whatever the dogfood "archistrator" project happens to hold.
 *
 * TWO ASSERTIONS. NOT MORE. Everything this surface PROMISES is pinned
 * deterministically in `tests/preview/plan.spec.ts` and
 * `tests/preview/activity-experience.spec.ts`, over committed fixtures that no
 * one else can move. This file exists for the one thing a preview cannot show:
 * that the real screens mount over the real server's own shape. Shared state
 * that other sessions (and the pump itself) mutate is not a place to assert a
 * count, an order or a state — so it asserts only that the plan lists SOMETHING
 * and that opening one of those rows draws that activity's lifecycle.
 *
 * WHY THIS ONE SKIPS, when `requireServer` would FAIL. The fix-H ruling — a
 * probe that gets no answer FAILS, because a skip reads as green — is about the
 * specs that assert the product's behaviour: a skip there hides a regression.
 * This file asserts nothing those two preview suites do not already assert, so a
 * skip here hides nothing; and the smoke is meant to be runnable from a
 * developer's checkout with no Postgres up. So the probe below is local and
 * deliberate, it names why it skipped, and it is the ONLY probe in this package
 * that behaves this way. Anything that would be lost by skipping belongs in a
 * preview spec, not here.
 *
 * Stage 5 Task 13 added two `tagUseCase(...)` lines to the case below. They are
 * ANNOTATIONS, not assertions — `meta/use-case-coverage.spec.ts` greps this file's
 * SOURCE for them and never runs it — so the skip ruling above is unchanged: this
 * file still asserts nothing the two preview suites do not.
 *
 * SAFETY: runs under the shared dispatch guard, like every other spec. It reads;
 * it writes nothing and creates nothing.
 */
import type { APIRequestContext } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { gotoApp, PROBE_TIMEOUT_MS } from './support/gating.js';
import { tagUseCase } from './support/useCases.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** The dogfood project every non-stubbed spec in this package reads. */
const PROJECT = 'archistrator';

/**
 * Whether a dev-mode server answers `/api/userinfo` behind the SPA proxy. Unlike
 * `requireServer`, "no answer" is `false` rather than a throw — see the header.
 */
async function serverAnswers(request: APIRequestContext): Promise<boolean> {
  try {
    const res = await request.get(`${BASE}/api/userinfo`, {
      headers: { Accept: 'application/json' },
      timeout: PROBE_TIMEOUT_MS,
    });
    return res.status() === 200;
  } catch {
    return false;
  }
}

test.beforeEach(async ({ request }) => {
  test.skip(
    !(await serverAnswers(request)),
    `uitests: no dev-mode server answers ${BASE}/api/userinfo — the stage-5 live smoke needs one. ` +
      'Everything it would have checked is pinned deterministically by tests/preview/plan.spec.ts ' +
      'and tests/preview/activity-experience.spec.ts, which need no server at all.'
  );
});

test('the plan lists the real project activities, and a row opens that activity', async ({
  page,
  dispatchGuard,
}) => {
  // The core use cases this flow exercises (meta/use-case-coverage.spec.ts greps
  // for these literals). Stage 5 Task 13 moved both tags HERE, from specs it
  // deleted with the screens they drove: `drive-system-design` was on
  // architecture-views / artifact-affordances / design-experience /
  // episodes-panel, which all drove `/design/system`; and the construction
  // activity tag was on construction-tracker, which drove `/construction`. Both
  // use cases are now driven from the ONE plan: the design phases are activities
  // 1–3 on it, and every construction activity is a row on it, so this smoke
  // really does walk both.
  //
  // The id is `execute-a-project-activity`: the model renamed it when one
  // Activity Experience took over every rail (and absorbed
  // `commit-to-a-project-option` into it). The coverage meta-check greps these
  // literals against the COMMITTED core use case list, so the old
  // `execute-a-construction-activity` tagged nothing and left the real use case
  // reading as an uncovered gap.
  tagUseCase('drive-system-design');
  tagUseCase('execute-a-project-activity');

  await gotoApp(page, `/project/${PROJECT}/plan`);

  // 1 — the plan screen renders with at least one row.
  await expect(page.getByTestId(TESTID.planList)).toBeVisible();
  const rows = page.getByTestId(new RegExp(`^${TESTID.planRow('')}`));
  expect(await rows.count()).toBeGreaterThan(0);

  // 2 — opening one draws that activity's own lifecycle.
  await rows.first().click();
  await expect(page.getByTestId(TESTID.lifecycleGraph)).toBeVisible();

  // Not a third assertion about the product: the standing safety invariant every
  // spec in this package carries — this one dispatched nothing.
  expect(dispatchGuard.blocked).toEqual([]);
});
