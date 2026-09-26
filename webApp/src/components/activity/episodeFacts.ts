/**
 * The header facts of ONE episode — what the dispatch body puts above the turn
 * timeline, so a reader sees what the run COST before they read what it did.
 *
 * This is the prototype's `DispatchBody` facts strip, kept and made real: there
 * it was assembled from hand-authored `minutes`/`tokensK`/`turns`/`subagents`
 * fields, and every one of them is on the wire's `EpisodeRecordView` already.
 * `EpisodeTimeline` (components/episodes) renders EVENTS and nothing else, so
 * without this strip the episode's duration, model, token spend and turn count
 * would be on screen nowhere — the facts an operator actually reads first.
 *
 * A fact the record does not carry is OMITTED, never rendered as `0` or `—`:
 * "this episode used no tokens" and "this record does not say" are different
 * claims and only one of them is ever true.
 *
 * Pure and React-free: `node --test` loads it directly.
 */
import type { EpisodeRecordView } from '../../contracts/types.ts';

export interface EpisodeFact {
  label: string;
  value: string;
}

/**
 * `startedAt`→`endedAt` as a reader says it. Ported from `EpisodesPanel`'s
 * private `formatDuration` (:113) so the two surfaces cannot drift; an
 * unparseable or inverted span is `undefined` here rather than the panel's `—`,
 * because this strip omits a fact it cannot state.
 */
export function episodeDuration(startedAt: string, endedAt: string): string | undefined {
  const start = Date.parse(startedAt);
  const end = Date.parse(endedAt);
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return undefined;
  const seconds = Math.round((end - start) / 1000);
  if (seconds < 60) return `${String(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  return `${String(minutes)}m ${String(seconds % 60)}s`;
}

/**
 * Total MAIN-LOOP tokens. Subagent tokens are in neither `usage` nor
 * `streamedUsage` (fixture-proven — see `useDeliveryQueries.ts`), which is why the
 * label says so: an unqualified "TOKENS" over a main-loop-only number is the
 * kind of quiet undercount a cost review is built on.
 */
export function episodeTokens(usage: EpisodeRecordView['usage']): number {
  return usage.in + usage.out + usage.cacheRead + usage.cacheCreate;
}

export function episodeFactsFor(record: EpisodeRecordView): readonly EpisodeFact[] {
  const duration = episodeDuration(record.startedAt, record.endedAt);
  const facts: EpisodeFact[] = [];
  if (duration !== undefined) facts.push({ label: 'DURATION', value: duration });
  if (record.model !== undefined && record.model.length > 0) {
    facts.push({ label: 'MODEL', value: record.model });
  }
  facts.push({
    label: 'TOKENS (MAIN LOOP)',
    value: episodeTokens(record.usage).toLocaleString('en-US'),
  });
  if (record.numTurns !== undefined) facts.push({ label: 'TURNS', value: String(record.numTurns) });
  if (record.subagentSpans !== undefined) {
    facts.push({ label: 'SUBAGENTS', value: String(record.subagentSpans.length) });
  }
  return facts;
}
