/// <reference types="node" />
/**
 * The float bands' colours are monotonic in every theme: the lower the float, the
 * stronger the alarm (designer re-check #12). Critical used to be the accent, a
 * rust in Retro, weaker than the ≤5d band's danger red.
 *
 * themes.ts cannot be imported under node:test (it imports a sibling without an
 * extension), so its literal hex values are read from the source: each theme's
 * `key: '<theme>'` block, token by token. The colours checked are the ones
 * bandTokens() renders from those values, so the ramp and its wiring are pinned
 * together.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import type { Tokens } from '../../utilities/theme/themes';
import { BAND_TOKEN, FLOAT_BANDS_BY_FLOAT } from './bandRamp.ts';
import { bandTokens } from './bandTokens.ts';
import type { FloatBand } from '../../contracts/types';

/** The colour bandTokens() renders for `band`, from one theme's literal tokens. */
function bandColour(t: Record<string, string>, band: FloatBand): string {
  assert.ok(t[BAND_TOKEN[band]] !== undefined, `the theme defines ${BAND_TOKEN[band]}`);
  return bandTokens(t as unknown as Tokens, band).fg;
}

const THEMES_SRC = readFileSync(
  new URL('../../utilities/theme/themes.ts', import.meta.url),
  'utf8'
);
const THEME_KEYS = ['retro', 'mor', 'retroMor', 'blueprint', 'blueprintMor'] as const;

/** One theme's literal hex tokens, read from its block in themes.ts. */
function themeTokens(key: string): Record<string, string> {
  const start = THEMES_SRC.indexOf(`key: '${key}',`);
  assert.ok(start >= 0, `theme ${key} is in themes.ts`);
  const block = THEMES_SRC.slice(start, THEMES_SRC.indexOf('\n  },', start));
  const out: Record<string, string> = {};
  for (const m of block.matchAll(/^\s+(\w+): '(#[0-9A-Fa-f]{6})',$/gm)) {
    const [, name, hex] = m;
    if (name !== undefined && hex !== undefined) out[name] = hex;
  }
  return out;
}

function channels(hex: string): [number, number, number] {
  const n = Number.parseInt(hex.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/** Hue in degrees, signed around red: (-180, 180]. A pink just short of red is
 *  slightly negative, so it still reads as redder than an orange. */
function hue(hex: string): number {
  const [r, g, b] = channels(hex).map((c) => c / 255) as [number, number, number];
  const max = Math.max(r, g, b);
  const d = max - Math.min(r, g, b);
  if (d === 0) return 0;
  const h =
    max === r
      ? 60 * (((g - b) / d) % 6)
      : max === g
        ? 60 * ((b - r) / d + 2)
        : 60 * ((r - g) / d + 4);
  return h > 180 ? h - 360 : h;
}

function luminance(hex: string): number {
  const [r, g, b] = channels(hex).map((c) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

void test('the lower the float, the stronger the alarm: the hue climbs from red to green, in every theme', () => {
  for (const key of THEME_KEYS) {
    const t = themeTokens(key);
    const hues = FLOAT_BANDS_BY_FLOAT.map((band) => hue(bandColour(t, band)));
    for (let i = 1; i < hues.length; i++) {
      const [redder, next] = [hues[i - 1] ?? 0, hues[i] ?? 0];
      assert.ok(
        next - redder >= 8,
        `${key}: ${String(FLOAT_BANDS_BY_FLOAT[i - 1])} (${redder.toFixed(1)}°) must read redder than ${String(FLOAT_BANDS_BY_FLOAT[i])} (${next.toFixed(1)}°)`
      );
    }
  }
});

void test('critical (float 0) is the danger colour, never the accent', () => {
  assert.equal(BAND_TOKEN.critical, 'dangerFg');
  for (const key of THEME_KEYS) {
    const t = themeTokens(key);
    assert.notEqual(bandColour(t, 'critical'), t['accent'], `${key}: critical is not the accent`);
  }
});

void test('every band colour clears 3:1 against the surfaces the rail sits on (WCAG 1.4.11)', () => {
  for (const key of THEME_KEYS) {
    const t = themeTokens(key);
    for (const band of FLOAT_BANDS_BY_FLOAT) {
      const colour = bandColour(t, band);
      for (const surface of ['paper', 'paperAlt'] as const) {
        const ground = t[surface] ?? '';
        const ratio = contrast(colour, ground);
        assert.ok(
          ratio >= 3,
          `${key}: ${band} ${colour} on ${surface} ${ground} is ${ratio.toFixed(2)}:1`
        );
      }
    }
  }
});
