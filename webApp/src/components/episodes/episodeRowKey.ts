/**
 * The key an episode row is addressed by: the task and attempt the episode served.
 *
 * A construction episode's `targetRef` is the attempt key the delivery Manager stamps
 * on it, `<activityId>:<task>:<n>` (n 1-based per task), so the row of the first
 * construction attempt is `construction-1` on every run that made one — what the run
 * ARRANGED, where the episode id is whatever the venue minted. A ref without an
 * attempt key (a design episode's artifact kind) names no attempt; that row keeps its
 * episode id.
 */
import type { EpisodeRecordView } from '../../contracts/types';

export function episodeRowKey(episode: Pick<EpisodeRecordView, 'episodeId' | 'targetRef'>): string {
  const ref = episode.targetRef;
  const nAt = ref.lastIndexOf(':');
  const taskAt = nAt > 0 ? ref.lastIndexOf(':', nAt - 1) : -1;
  if (taskAt <= 0) return episode.episodeId;
  const task = ref.slice(taskAt + 1, nAt);
  const n = ref.slice(nAt + 1);
  if (task === '' || !/^[1-9][0-9]*$/.test(n)) return episode.episodeId;
  return `${task}-${n}`;
}
