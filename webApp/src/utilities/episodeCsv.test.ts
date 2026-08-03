/// <reference types="node" />
/**
 * Unit tests for the episode CSV flattener (Task 10, SP1 capture-seam trace UI).
 * Covers the exact header row the black-box uitests assert on, one row per
 * record, RFC-4180 quoting of fields carrying commas/quotes, and \n line
 * endings (node --test's native runner — no vitest in this repo).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { flattenEpisodesToCsv, type EpisodeExport } from './episodeCsv.ts';
import type { EpisodeRecordView } from '../contracts/types';

function record(overrides: Partial<EpisodeRecordView> = {}): EpisodeRecordView {
  return {
    episodeId: 'ep-1',
    kind: 'construction',
    targetRef: 'act-1',
    outcome: 'succeeded',
    usage: { in: 100, out: 200, cacheRead: 10, cacheCreate: 5 },
    startedAt: '2026-08-01T00:00:00Z',
    endedAt: '2026-08-01T00:05:00Z',
    ...overrides,
  };
}

const EXPECTED_HEADER =
  'episodeId,kind,targetRef,outcome,model,workerClass,tokensIn,tokensOut,cacheRead,cacheCreate,costUsd,numTurns,startedAt,endedAt';

void test('emits the exact expected header row', () => {
  const exp: EpisodeExport = { records: [], traces: {} };
  const csv = flattenEpisodesToCsv(exp);
  assert.equal(csv, EXPECTED_HEADER);
});

void test('emits one row per record, in order, with \\n line endings', () => {
  const exp: EpisodeExport = {
    records: [record({ episodeId: 'ep-1' }), record({ episodeId: 'ep-2', outcome: 'gap' })],
    traces: {},
  };
  const csv = flattenEpisodesToCsv(exp);
  const lines = csv.split('\n');
  assert.equal(lines.length, 3);
  assert.equal(lines[0], EXPECTED_HEADER);
  assert.match(lines[1] ?? '', /^ep-1,/);
  assert.match(lines[2] ?? '', /^ep-2,/);
  assert.ok(!csv.includes('\r'));
});

void test('renders every column, including optional fields, in the declared order', () => {
  const exp: EpisodeExport = {
    records: [
      record({
        episodeId: 'ep-3',
        kind: 'review',
        targetRef: 'mission',
        outcome: 'failed',
        model: 'claude-sonnet-5',
        workerClass: 'architect',
        usage: { in: 1000, out: 2000, cacheRead: 300, cacheCreate: 40 },
        costUsd: 1.25,
        numTurns: 7,
        startedAt: '2026-08-01T00:00:00Z',
        endedAt: '2026-08-01T00:10:00Z',
      }),
    ],
    traces: {},
  };
  const [, row] = flattenEpisodesToCsv(exp).split('\n');
  assert.equal(
    row,
    'ep-3,review,mission,failed,claude-sonnet-5,architect,1000,2000,300,40,1.25,7,2026-08-01T00:00:00Z,2026-08-01T00:10:00Z'
  );
});

void test('leaves optional fields blank (not "undefined") when absent', () => {
  const exp: EpisodeExport = { records: [record()], traces: {} };
  const [, row] = flattenEpisodesToCsv(exp).split('\n');
  // model,workerClass,...,costUsd,numTurns are all absent on the bare fixture.
  assert.equal(
    row,
    'ep-1,construction,act-1,succeeded,,,100,200,10,5,,,2026-08-01T00:00:00Z,2026-08-01T00:05:00Z'
  );
});

void test('RFC-4180 quotes a field containing a comma', () => {
  const exp: EpisodeExport = {
    records: [record({ workerClass: 'frontend, backend' })],
    traces: {},
  };
  const [, row] = flattenEpisodesToCsv(exp).split('\n');
  assert.ok((row ?? '').includes('"frontend, backend"'));
});

void test('RFC-4180 quotes and doubles embedded double quotes', () => {
  const exp: EpisodeExport = {
    records: [record({ model: 'the "fast" model' })],
    traces: {},
  };
  const [, row] = flattenEpisodesToCsv(exp).split('\n');
  assert.ok((row ?? '').includes('"the ""fast"" model"'));
});

void test('RFC-4180 quotes a field containing an embedded newline', () => {
  const exp: EpisodeExport = {
    records: [record({ workerClass: 'line1\nline2' })],
    traces: {},
  };
  // The embedded newline lands INSIDE the quoted field, so assert against the
  // full string rather than a split('\n') line (which would cut the field).
  const csv = flattenEpisodesToCsv(exp);
  assert.ok(csv.includes('"line1\nline2"'));
});
