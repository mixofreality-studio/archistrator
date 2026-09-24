/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { episodeDuration, episodeFactsFor, episodeTokens } from './episodeFacts.ts';
import type { EpisodeRecordView } from '../../contracts/types.ts';

function record(over: Partial<EpisodeRecordView> = {}): EpisodeRecordView {
  return {
    episodeId: 'ep-1',
    kind: 'construction',
    targetRef: 'C-x',
    outcome: 'succeeded',
    usage: { in: 100, out: 200, cacheRead: 3000, cacheCreate: 40 },
    startedAt: '2026-09-22T08:45:00Z',
    endedAt: '2026-09-22T08:47:30Z',
    ...over,
  };
}

void test('a duration reads in minutes and seconds, or in seconds under a minute', () => {
  assert.equal(episodeDuration('2026-09-22T08:45:00Z', '2026-09-22T08:47:30Z'), '2m 30s');
  assert.equal(episodeDuration('2026-09-22T08:45:00Z', '2026-09-22T08:45:42Z'), '42s');
});

void test('an unparseable or inverted span states nothing rather than an em dash', () => {
  assert.equal(episodeDuration('not a date', '2026-09-22T08:47:30Z'), undefined);
  assert.equal(episodeDuration('2026-09-22T08:47:30Z', '2026-09-22T08:45:00Z'), undefined);
});

void test('tokens are the four main-loop counters added, cache included', () => {
  assert.equal(episodeTokens({ in: 100, out: 200, cacheRead: 3000, cacheCreate: 40 }), 3340);
});

void test('the strip omits a fact the record does not carry, never zeroes it', () => {
  const facts = episodeFactsFor(record());
  assert.deepEqual(
    facts.map((f) => f.label),
    ['DURATION', 'TOKENS (MAIN LOOP)']
  );
  // No MODEL, no TURNS, no SUBAGENTS — the record says nothing about them, and
  // "0 subagents" would be an answer this record never gave.
});

void test('every fact the record carries is stated, in reading order', () => {
  const facts = episodeFactsFor(
    record({
      model: 'claude-opus-5',
      numTurns: 34,
      subagentSpans: [{ toolUseId: 'toolu_sub_0' }],
    })
  );
  assert.deepEqual(facts, [
    { label: 'DURATION', value: '2m 30s' },
    { label: 'MODEL', value: 'claude-opus-5' },
    { label: 'TOKENS (MAIN LOOP)', value: '3,340' },
    { label: 'TURNS', value: '34' },
    { label: 'SUBAGENTS', value: '1' },
  ]);
});
