/**
 * The "Operating" presentation state (Task 14, finish-construction): a project
 * whose construction has fully finished — every network activity reached the
 * server's ActivityConstructionDone phase AND BuildIntegrated status, with the
 * project itself in the construction phase. Presentation-only: there is no new
 * ProjectPhase enum member. "Operating" is a derived overlay label the render
 * sites (catalog tile, AppShell chip, HomeBase construction card, the Tracker's
 * completion panel, the Console's begin/resume button) apply on top of the
 * existing 3-phase model.
 *
 * `deriveOperating` is the ONE TS authority for this derivation and mirrors the
 * server's unexported `isConstructionComplete`
 * (server/internal/resourceaccess/projectstate/projectstateaccess.go) bit for
 * bit: same fixture corpus, same result. It is deliberately typed over RAW
 * ordinals (not the app-mapped ConstructionRow shape used elsewhere in
 * constructionAdapters.ts) so its own test can iterate the shared fixture file
 * with zero reshaping — see operating.test.ts and
 * testdata/operating_fixtures.json (shared byte-identically with the Go side's
 * server/internal/resourceaccess/projectstate/testdata/operating_fixtures.json —
 * keep both in sync; see that file's own sync comment). The single real call
 * site is wire.ts's mapProjectState, which adapts the raw
 * SystemDesignActivityConstructionStatus map into this shape once, at the wire
 * boundary, and threads the resulting boolean down as ProjectStateWithGit.operating
 * — every render site consumes THAT boolean rather than re-deriving it.
 */
import type { ProjectPhase } from './types';

/**
 * Coarse per-activity construction lifecycle ordinal — mirrors the server's
 * ActivityConstructionPhase (0 notStarted, 1 running, 2 done, 3 failed). Distinct
 * from ConstructionRow.phase (the fine-grained ActivityMethodPhase STRING already
 * mapped for display elsewhere) and from ActivityBuildStatusRow (the coarser
 * build-status STRING lens) — this is the raw ordinal the fixture corpus carries.
 */
export type ActivityConstructionPhaseLike = 0 | 1 | 2 | 3;

/**
 * Per-activity build-status ordinal — mirrors the server's ActivityBuildStatus
 * (0 inConstruction, 1 inReview, 2 integrated, 3 failed).
 */
export type BuildStatusLike = 0 | 1 | 2 | 3;

/** ActivityConstructionPhase.ActivityConstructionDone ordinal. */
const PHASE_DONE: ActivityConstructionPhaseLike = 2;

/** ActivityBuildStatus.BuildIntegrated ordinal. */
const BUILD_INTEGRATED: BuildStatusLike = 2;

/** One row's shape, matching the fixture file exactly (phase + buildStatus ordinals). */
export interface OperatingRow {
  phase: ActivityConstructionPhaseLike;
  buildStatus: BuildStatusLike;
}

/**
 * True iff the project has entered construction, has at least one construction
 * row (an empty/absent set — construction started but nothing dispatched, or a
 * project that never reached construction — is never "operating"), and EVERY row
 * has BOTH reached the coarse Done phase AND the Integrated build status. A row
 * that is Done-but-not-integrated (the Skipped/TakenOver outcome shape) or still
 * Running/Failed keeps the project out of the operating state — phase alone
 * reaching Done is not sufficient. Mirrors the Go isConstructionComplete exactly;
 * see this file's doc comment for the shared fixture corpus both sides assert
 * against.
 */
export function deriveOperating(
  rows: readonly OperatingRow[],
  projectPhase: ProjectPhase
): boolean {
  if (projectPhase !== 'construction' || rows.length === 0) return false;
  return rows.every((row) => row.phase === PHASE_DONE && row.buildStatus === BUILD_INTEGRATED);
}
