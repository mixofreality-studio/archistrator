/**
 * The episode body's two pure rules: WHOSE episodes the caption may claim
 * (episodeAttribution.ts) and where a subagent span sits on its episode's clock
 * (spanGeometry.ts).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { ACTIVITY_LEVEL_CAPTION, attributionOf, episodeScopeFor } from './episodeAttribution.ts';
import { formatSpanDuration, ganttBarsFor } from './spanGeometry.ts';

// ---------------------------------------------------------------------------
// Attribution — the honesty rule
// ---------------------------------------------------------------------------

void test('an episode carrying the BARE activity id is unattributed', () => {
  assert.equal(attributionOf('C-artifact-access', 'C-artifact-access:srs:1'), 'unattributed');
});

void test('an episode carrying the attempt key verbatim is attributed', () => {
  assert.equal(attributionOf('C-artifact-access:srs:1', 'C-artifact-access:srs:1'), 'attributed');
});

void test('a PREFIX match is never attribution — that is the guess this rule exists to stop', () => {
  // `C-artifact-access` is a prefix of `C-artifact-access:srs:1`, and `C-artifact-access:srs:1` is a prefix of
  // `C-artifact-access:srs:10`. Either as a match would claim a legacy episode for whichever
  // task happened to be selected.
  assert.equal(
    attributionOf('C-artifact-access:srs:10', 'C-artifact-access:srs:1'),
    'unattributed'
  );
  assert.equal(
    attributionOf('C-artifact-access:srsReview:1', 'C-artifact-access:srs:1'),
    'unattributed'
  );
});

void test('with no attempt selected nothing can be attributed', () => {
  assert.equal(attributionOf('C-artifact-access:srs:1', undefined), 'unattributed');
  assert.equal(attributionOf('C-artifact-access:srs:1', ''), 'unattributed');
});

void test('the caption states activity-level scope whenever nothing matches the attempt key', () => {
  const scope = episodeScopeFor(
    ['C-artifact-access', 'C-artifact-access', 'C-artifact-access'],
    'C-artifact-access:srs:1'
  );
  assert.equal(scope.showing, 'all');
  assert.equal(scope.total, 3);
  assert.equal(scope.attributed, 0);
  assert.equal(scope.caption, ACTIVITY_LEVEL_CAPTION);
  // The exact wording the brief requires, so a later edit cannot quietly soften it.
  assert.match(
    scope.caption,
    /activity-level; episodes written before this release are not attributable to a specific task/
  );
});

void test('a real attempt-key match narrows the list AND says so', () => {
  const scope = episodeScopeFor(
    ['C-artifact-access', 'C-artifact-access:srs:1'],
    'C-artifact-access:srs:1'
  );
  assert.equal(scope.showing, 'attributed');
  assert.equal(scope.total, 2);
  assert.equal(scope.attributed, 1);
  assert.match(scope.caption, /attributable to this task/);
  assert.match(scope.caption, /C-artifact-access:srs:1/);
});

void test('an empty list is still scoped honestly rather than silently', () => {
  const scope = episodeScopeFor([], 'C-artifact-access:srs:1');
  assert.equal(scope.showing, 'all');
  assert.equal(scope.total, 0);
  assert.equal(scope.caption, ACTIVITY_LEVEL_CAPTION);
});

// ---------------------------------------------------------------------------
// The gantt — untimed is a first-class answer
// ---------------------------------------------------------------------------

const EPISODE_START = '2026-09-09T00:00:00.000Z';
const EPISODE_END = '2026-09-09T00:01:40.000Z'; // 100s — 1s == 1%

void test('places a span as a percentage of its episode window', () => {
  const [bar] = ganttBarsFor({
    startedAt: EPISODE_START,
    endedAt: EPISODE_END,
    subagentSpans: [
      {
        toolUseId: 'tu_1',
        startedAt: '2026-09-09T00:00:20.000Z',
        endedAt: '2026-09-09T00:00:50.000Z',
      },
    ],
  });
  assert.ok(bar !== undefined);
  assert.equal(bar.untimed, false);
  assert.equal(bar.leftPct, 20);
  assert.equal(bar.widthPct, 30);
  assert.equal(bar.durationMs, 30000);
});

void test('a span with no clock is UNTIMED, never dropped and never placed', () => {
  const [bar] = ganttBarsFor({
    startedAt: EPISODE_START,
    endedAt: EPISODE_END,
    subagentSpans: [{ toolUseId: 'tu_1' }],
  });
  assert.ok(bar !== undefined);
  assert.equal(bar.untimed, true);
  assert.equal(bar.leftPct, 0);
  assert.equal(bar.widthPct, 100);
  assert.equal(bar.durationMs, undefined);
});

void test('an unusable episode window makes every span untimed, not all of them zero-width', () => {
  const bars = ganttBarsFor({
    startedAt: EPISODE_END,
    endedAt: EPISODE_START, // inverted
    subagentSpans: [
      { toolUseId: 'tu_1', startedAt: EPISODE_START, endedAt: EPISODE_END },
      { toolUseId: 'tu_2' },
    ],
  });
  assert.deepEqual(
    bars.map((b) => b.untimed),
    [true, true]
  );
});

void test('a span that started but never ended keeps its true start and claims no duration', () => {
  const [bar] = ganttBarsFor({
    startedAt: EPISODE_START,
    endedAt: EPISODE_END,
    subagentSpans: [{ toolUseId: 'tu_1', startedAt: '2026-09-09T00:00:50.000Z' }],
  });
  assert.ok(bar !== undefined);
  assert.equal(bar.leftPct, 50);
  assert.equal(bar.untimed, true);
  assert.equal(bar.durationMs, undefined);
});

void test('a very short span is floored to something visible, never to nothing', () => {
  const [bar] = ganttBarsFor({
    startedAt: EPISODE_START,
    endedAt: EPISODE_END,
    subagentSpans: [
      {
        toolUseId: 'tu_1',
        startedAt: '2026-09-09T00:00:10.000Z',
        endedAt: '2026-09-09T00:00:10.200Z',
      },
    ],
  });
  assert.ok(bar !== undefined);
  assert.ok(bar.widthPct >= 1.5, 'a 200ms span must still be drawable');
  // The numeral carries the quantitative claim, so the floor never has to.
  assert.equal(formatSpanDuration(bar.durationMs), '200ms');
});

void test('a bar can never overflow its own track', () => {
  const [bar] = ganttBarsFor({
    startedAt: EPISODE_START,
    endedAt: EPISODE_END,
    subagentSpans: [
      {
        toolUseId: 'tu_1',
        startedAt: '2026-09-09T00:00:90.000Z',
        endedAt: '2026-09-09T00:05:00.000Z',
      },
    ],
  });
  assert.ok(bar !== undefined);
  assert.ok(bar.leftPct + bar.widthPct <= 100.001);
});

void test('formats span durations across the three magnitudes', () => {
  assert.equal(formatSpanDuration(undefined), '—');
  assert.equal(formatSpanDuration(940), '940ms');
  assert.equal(formatSpanDuration(4200), '4.2s');
  assert.equal(formatSpanDuration(45000), '45s');
  assert.equal(formatSpanDuration(185000), '3m 05s');
});
