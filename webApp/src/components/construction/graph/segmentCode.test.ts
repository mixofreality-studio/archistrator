/**
 * The segment-code fit rule (segmentCode.ts): a code renders only when its
 * measured text fits its segment — otherwise nothing, never "R…".
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  SEGMENT_CODE_LETTER_SPACING,
  segmentCodeFits,
  subscribeToFontMetricChanges,
  type FontFaceSetLike,
} from './segmentCode.ts';

void test('a code fits when its measured text is no wider than its segment', () => {
  assert.equal(segmentCodeFits(14.2, 30), true);
  assert.equal(segmentCodeFits(30, 30), true);
});

void test('a code wider than its segment does not fit — it renders nothing, never a clipped fragment', () => {
  assert.equal(segmentCodeFits(14.2, 14.1), false);
  assert.equal(segmentCodeFits(22, 9), false);
});

void test('an unmeasured or empty width never fits', () => {
  assert.equal(segmentCodeFits(0, 30), false);
  assert.equal(segmentCodeFits(Number.NaN, 30), false);
  assert.equal(segmentCodeFits(10, Number.NaN), false);
  assert.equal(segmentCodeFits(10, 0), false);
});

void test('codes are set with no letter-spacing, so the measure is the text alone', () => {
  assert.equal(SEGMENT_CODE_LETTER_SPACING, 0);
});

// ---------------------------------------------------------------------------
// subscribeToFontMetricChanges (round 3: a ResizeObserver never fires for a
// web font finishing load, so this is the other half of "measured, not
// counted" — a fake FontFaceSet, no DOM at all.
// ---------------------------------------------------------------------------

/** A minimal, fully controllable `FontFaceSetLike` double. */
function fakeFonts(): {
  fonts: FontFaceSetLike;
  resolveReady: () => void;
  rejectReady: (err: unknown) => void;
  fireLoadingDone: () => void;
  listenerCount: () => number;
} {
  let resolve: () => void = () => undefined;
  let reject: (err: unknown) => void = () => undefined;
  const ready = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  const listeners = new Set<() => void>();
  return {
    fonts: {
      ready,
      addEventListener: (_type, listener): void => {
        listeners.add(listener);
      },
      removeEventListener: (_type, listener): void => {
        listeners.delete(listener);
      },
    },
    resolveReady: resolve,
    rejectReady: reject,
    fireLoadingDone: (): void => {
      for (const l of listeners) l();
    },
    listenerCount: () => listeners.size,
  };
}

/** Flushes the microtask queue enough times for a chained `.then().catch()`
 *  to run — two hops. */
async function flushMicrotasks(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
}

void test('subscribeToFontMetricChanges re-measures once fonts.ready resolves', async () => {
  const { fonts, resolveReady } = fakeFonts();
  let calls = 0;
  subscribeToFontMetricChanges(fonts, () => {
    calls += 1;
  });
  assert.equal(calls, 0, 'not called before ready resolves');
  resolveReady();
  await flushMicrotasks();
  assert.equal(calls, 1);
});

void test('subscribeToFontMetricChanges re-measures on every loadingdone event', () => {
  const { fonts, fireLoadingDone } = fakeFonts();
  let calls = 0;
  subscribeToFontMetricChanges(fonts, () => {
    calls += 1;
  });
  fireLoadingDone();
  fireLoadingDone();
  assert.equal(calls, 2, 'a re-measure per event, not just the first');
});

void test('subscribeToFontMetricChanges registers exactly one loadingdone listener', () => {
  const { fonts, listenerCount } = fakeFonts();
  subscribeToFontMetricChanges(fonts, () => undefined);
  assert.equal(listenerCount(), 1);
});

void test('the returned unsubscribe removes the loadingdone listener — a later event never fires', () => {
  const { fonts, fireLoadingDone, listenerCount } = fakeFonts();
  let calls = 0;
  const unsubscribe = subscribeToFontMetricChanges(fonts, () => {
    calls += 1;
  });
  unsubscribe();
  assert.equal(listenerCount(), 0);
  fireLoadingDone();
  assert.equal(calls, 0);
});

void test('unsubscribing before fonts.ready resolves suppresses the deferred re-measure', async () => {
  const { fonts, resolveReady } = fakeFonts();
  let calls = 0;
  const unsubscribe = subscribeToFontMetricChanges(fonts, () => {
    calls += 1;
  });
  unsubscribe();
  resolveReady();
  await flushMicrotasks();
  assert.equal(calls, 0);
});

void test('a rejecting fonts.ready never throws and never calls onChange', async () => {
  const { fonts, rejectReady } = fakeFonts();
  let calls = 0;
  subscribeToFontMetricChanges(fonts, () => {
    calls += 1;
  });
  rejectReady(new Error('should never happen, per the FontFaceSet spec'));
  await flushMicrotasks();
  assert.equal(calls, 0);
});
