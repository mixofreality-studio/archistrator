/**
 * The hover card's placement (hoverCardPlacement.ts) — right, with the left as
 * its one fallback, bounded to the canvas by flip and preventOverflow.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  HOVER_CARD_FALLBACKS,
  HOVER_CARD_PLACEMENT,
  hoverCardModifiers,
  type PopperModifierSpec,
} from './hoverCardPlacement.ts';

function modifier(list: PopperModifierSpec[], name: string): PopperModifierSpec {
  const m = list.find((x) => x.name === name);
  assert.ok(m !== undefined, `no ${name} modifier`);
  return m;
}

const CANVAS = { tag: 'the canvas element' } as unknown as Element;

void test('placed right, with the left as the one fallback', () => {
  assert.equal(HOVER_CARD_PLACEMENT, 'right');
  assert.deepEqual(HOVER_CARD_FALLBACKS, ['left']);
  assert.deepEqual(modifier(hoverCardModifiers(CANVAS), 'flip').options['fallbackPlacements'], [
    'left',
  ]);
});

void test('flip and preventOverflow are both bounded to the CANVAS', () => {
  const mods = hoverCardModifiers(CANVAS);
  assert.equal(modifier(mods, 'flip').options['boundary'], CANVAS);
  assert.equal(modifier(mods, 'preventOverflow').options['boundary'], CANVAS);
});

void test('preventOverflow may slide the card along both axes, untethered, so it never hangs off', () => {
  const po = modifier(hoverCardModifiers(CANVAS), 'preventOverflow').options;
  assert.equal(po['altAxis'], true);
  assert.equal(po['tether'], false);
});

void test('with no canvas yet, no boundary is invented — popper uses its clipping parents', () => {
  for (const b of [null, undefined]) {
    const mods = hoverCardModifiers(b);
    assert.equal('boundary' in modifier(mods, 'flip').options, false);
    assert.equal('boundary' in modifier(mods, 'preventOverflow').options, false);
  }
});
