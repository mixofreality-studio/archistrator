/**
 * The float bands' colours (bandTokens.ts) are MONOTONIC in alarm: the lower
 * the float, the stronger the alarm, in every theme. "Stronger" is measured on
 * the red→green ramp every band scale uses — a hue nearer red is a stronger
 * alarm — so critical < red < yellow < green in hue, reds just below 360°
 * wrapped to below zero.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { THEME_ORDER, TOKENS } from '../../utilities/theme/themes.ts';
import { bandTokens } from './bandTokens.ts';
import type { FloatBand } from '../../contracts/projectAdapters.ts';

/** HSL hue of a #rrggbb colour, in degrees, reds above 300° wrapped negative. */
function alarmHue(hex: string): number {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex);
  assert.ok(m !== null, `not a #rrggbb colour: ${hex}`);
  const [r, g, b] = [m[1], m[2], m[3]].map((c) => Number.parseInt(c ?? '0', 16) / 255) as [
    number,
    number,
    number,
  ];
  const max = Math.max(r, g, b);
  const d = max - Math.min(r, g, b);
  assert.ok(d > 0, `a grey has no hue: ${hex}`);
  const h =
    max === r
      ? 60 * (((g - b) / d) % 6)
      : max === g
        ? 60 * ((b - r) / d + 2)
        : 60 * ((r - g) / d + 4);
  const deg = (h + 360) % 360;
  return deg > 300 ? deg - 360 : deg;
}

const BY_FLOAT: FloatBand[] = ['critical', 'red', 'yellow', 'green'];

for (const key of THEME_ORDER) {
  void test(`${key}: lower float is the stronger alarm — critical, red, yellow, green climb the ramp`, () => {
    const hues = BY_FLOAT.map((band) => alarmHue(bandTokens(TOKENS[key], band).fg));
    for (let i = 1; i < hues.length; i += 1) {
      assert.ok(
        (hues[i - 1] ?? 0) < (hues[i] ?? 0),
        `${key}: ${BY_FLOAT[i - 1] ?? ''} (${String(hues[i - 1])}°) must be redder than ${BY_FLOAT[i] ?? ''} (${String(hues[i])}°)`
      );
    }
  });
}

void test('zero float is drawn in the danger ink — never the theme accent', () => {
  for (const key of THEME_ORDER) {
    assert.equal(bandTokens(TOKENS[key], 'critical').fg, TOKENS[key].dangerFg, key);
  }
});
