/**
 * THE CLASSIFICATION → RENDERER SEAM, asserted black-box.
 *
 * Stage 5 Task 13 retired 38 specs with the screens that drove them. Five of the
 * renderers those specs exercised are NOT retired — `MissionView`,
 * `GlossaryView`, `VolatilityMap`, `UseCaseCarousel` and `ArchitectureView` are
 * all still shipped, and since the design rails were deleted the Activity
 * Experience's REVIEW body is their only reachable caller (spec §7.4: "artifact
 * renderers and review aids are kept"). Every one of them still has its unit
 * tests; none of them had a Playwright vehicle any more.
 *
 * This is that vehicle, reduced to the one claim a deletion wave can break:
 * opening a review task mounts THE RIGHT RENDERER for the artifact that task
 * judges, and mounts it cleanly. It deliberately asserts nothing about what is
 * INSIDE a renderer — the per-renderer behaviour specs (chips, lanes, keyboard
 * roving, view switches) are earmarked in
 * `docs/bugs/2026-09-24-stage5-webapp-earmarks.md` with the `stubActivityView`
 * recipe that would bring them back. A smoke case that can be kept honest beats
 * four specs that cannot be run.
 *
 * The path under test, per case:
 *
 *   review task → `taskArtifactFor` resolves `{ kind: 'slot', artifactKind }`
 *   → `ArtifactPanel`'s `slot` branch → `ArtifactRenderer` dispatches on the
 *   committed slot envelope's `kind` → the renderer's own root id.
 *
 * `activityArtifactUnavailable` is asserted ABSENT beside each one: the
 * dispatcher's honest-refusal panel renders inside the same `ArtifactPanel`, so
 * without that assertion a case that resolved NO artifact kind would still find
 * a visible panel and read as a pass. `incidents(page)` is asserted empty for
 * the same reason the other two preview suites do it — a screen that rendered
 * half a state over an unanswered op is not a pass.
 *
 * Fixtures, both already shipped by Task 6:
 *   - `activity-experience/requirements-backfilled` — the Requirements activity,
 *     four phases, all passed, with the four committed Phase-1 slots its four
 *     review tasks judge. Its route names no task, so each case navigates by
 *     clicking that task's lifecycle node (click = latest revision).
 *   - `activity-experience/architecture-round` — the Architecture activity,
 *     whose route already names `?task=architectureReview`.
 */
import { test, expect } from '../support/dispatchGuard.js';
import { TESTID } from '../support/testids.js';
import { incidents, openState } from '../support/previewShell.js';

/**
 * The four Requirements review tasks, each with the renderer its committed slot
 * must reach. The task ids are the lifecycle's own (`lifecycles.gen.ts`), which
 * is what the node testid is keyed by.
 */
const REQUIREMENTS_CASES: readonly { task: string; renderer: string; label: string }[] = [
  { task: 'missionReview', renderer: TESTID.missionRoot, label: 'MissionView' },
  { task: 'glossaryReview', renderer: TESTID.glossaryRoot, label: 'GlossaryView' },
  { task: 'volatilitiesReview', renderer: TESTID.volatilityMap, label: 'VolatilityMap' },
  { task: 'coreUseCasesReview', renderer: TESTID.useCaseCarouselRoot, label: 'UseCaseCarousel' },
];

test.describe('activity experience: the kept artifact renderers still mount', () => {
  for (const { task, renderer, label } of REQUIREMENTS_CASES) {
    test(`requirements · ${task} renders ${label}`, async ({ page }) => {
      const offBundle = await openState(page, 'activity-experience', 'requirements-backfilled');
      await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();

      // Click = this task at its latest revision. The fixture's route names no
      // task, so the default-task rule opened some other one; this is the only
      // way a preview (memory history) can reach a chosen `?task=`.
      await page.getByTestId(TESTID.lifecycleNode(task)).click();

      await expect(page.getByTestId(TESTID.activityReviewBody)).toBeVisible();
      await expect(page.getByTestId(TESTID.activityArtifactPanel)).toBeVisible();
      await expect(page.getByTestId(renderer)).toBeVisible();
      // The dispatcher refused nothing: no "no renderer for this" panel.
      await expect(page.getByTestId(TESTID.activityArtifactUnavailable)).toHaveCount(0);

      await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
      expect(await incidents(page)).toEqual([]);
      expect(offBundle).toEqual([]);
    });
  }

  test('architecture · architectureReview renders ArchitectureView', async ({ page }) => {
    // This fixture's own route is `?task=architectureReview`, so the review body
    // is what the screen opens on — no navigation.
    const offBundle = await openState(page, 'activity-experience', 'architecture-round');
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();

    await expect(page.getByTestId(TESTID.activityReviewBody)).toBeVisible();
    await expect(page.getByTestId(TESTID.activityArtifactPanel)).toBeVisible();
    await expect(page.getByTestId(TESTID.architectureRoot)).toBeVisible();
    await expect(page.getByTestId(TESTID.activityArtifactUnavailable)).toHaveCount(0);

    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });
});
