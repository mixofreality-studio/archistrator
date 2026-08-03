/**
 * Client-side CSV flattener for the episode Export action. There is no
 * `exportEpisodes` op (cut per the 2026-08-02 facet ruling) — the export button
 * assembles an `EpisodeExport` from the already-fetched episode list + the
 * per-episode timelines (fetched on demand, see hooks/useEpisodes.ts's
 * `useFetchEpisodeTimelines`), then this pure function flattens it to CSV
 * (RFC-4180 quoting, \n line endings). `traces` rides along for the JSON export
 * (`{records, traces}`, downloaded verbatim) but is not itself flattened — the
 * CSV is one row per episode record.
 */
import type { EpisodeRecordView, TimelineEvent } from '../contracts/types';

/** The client-assembled export payload — one episode list + its fetched
 *  per-episode timelines, keyed by episodeId. Both the JSON and CSV exports are
 *  built from the same value. */
export interface EpisodeExport {
  records: EpisodeRecordView[];
  traces: Record<string, TimelineEvent[]>;
}

const CSV_HEADER =
  'episodeId,kind,targetRef,outcome,model,workerClass,tokensIn,tokensOut,cacheRead,cacheCreate,costUsd,numTurns,startedAt,endedAt';

/** RFC-4180 field quoting: a field containing a comma, double quote, or newline
 *  is wrapped in double quotes, with embedded double quotes doubled. */
function csvField(value: string): string {
  if (/[",\n\r]/.test(value)) {
    return `"${value.replace(/"/g, '""')}"`;
  }
  return value;
}

function csvRow(r: EpisodeRecordView): string {
  const fields = [
    r.episodeId,
    r.kind,
    r.targetRef,
    r.outcome,
    r.model ?? '',
    r.workerClass ?? '',
    String(r.usage.in),
    String(r.usage.out),
    String(r.usage.cacheRead),
    String(r.usage.cacheCreate),
    r.costUsd !== undefined ? String(r.costUsd) : '',
    r.numTurns !== undefined ? String(r.numTurns) : '',
    r.startedAt,
    r.endedAt,
  ];
  return fields.map(csvField).join(',');
}

/** Flattens an EpisodeExport to CSV text: the fixed header row, then one row per
 *  record, \n line endings throughout (no trailing newline). */
export function flattenEpisodesToCsv(exp: EpisodeExport): string {
  return [CSV_HEADER, ...exp.records.map(csvRow)].join('\n');
}
