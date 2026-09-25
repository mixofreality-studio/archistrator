/**
 * WHICH MANAGER'S EPISODE LEDGER an activity's episodes were captured in.
 *
 * `QueryActivityView` is a CONSTRUCTION op, so it is tempting to read every
 * episode it points at through the construction manager. That is wrong for three
 * of the fourteen activity types. The unified activity experience (spec
 * 2026-09-20 §5.1) makes Requirements, Architecture and Project Design activities
 * 1–3, but their episodes were — and still are — captured by the SYSTEM DESIGN
 * and PROJECT DESIGN managers; `useEpisodes.ts` picks a different op per manager
 * (`TIMELINE_OP`), so a design activity's `episodeId` read through
 * `constructionGetEpisodeTimeline` finds nothing and the turn timeline silently
 * comes back empty.
 *
 * Measured against the fixtures: `activity-experience/architecture-round.json`
 * carries `episodeId: "ep-arch-draft-2"` on an `architecture` activity — an
 * episode of the systemDesign ledger, reached through
 * `systemDesignGetEpisodeTimeline`.
 *
 * NOTE on `EpisodesTarget.targetRef`: the TIMELINE op takes only `episodeID`, so
 * the target's `targetRef` selects nothing for a timeline read — it is the LIST
 * op that needs the artifact-kind slug for a design target. The Activity
 * Experience makes no list read (its revision select IS the list), so it passes
 * the activityId for every type and the design managers never see it.
 *
 * Pure and React-free: `node --test` loads it directly.
 */
import type { EpisodesManager } from './useEpisodes.ts';

/** The three design activity types, and the manager that captured each one's episodes. */
const DESIGN_MANAGER: Readonly<Record<string, EpisodesManager>> = {
  requirements: 'systemDesign',
  architecture: 'systemDesign',
  projectDesign: 'projectDesign',
};

/**
 * The manager for an activity's `ConstructionActivityView.type`. Every type that
 * is not one of the three design ones is construction — including a type this
 * build has never heard of, which is the honest default: the construction rail
 * is what `QueryActivityView` serves, and a new activity type arrives there.
 */
export function activityEpisodesManager(type: string): EpisodesManager {
  return DESIGN_MANAGER[type] ?? 'construction';
}
