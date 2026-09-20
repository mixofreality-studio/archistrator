/// <reference types="node" />
/**
 * The float ramp and the one "critical" colour, measured in every theme (designer
 * palette ruling, fix I). Weight is contrast against the ground; hue is LCh; distance
 * is CIEDE2000 (utilities/theme/colorScience.ts, pinned here to Sharma 2005).
 *
 * The token VALUES are read from themes.ts's source, as the ruling asks (its literal
 * parser): each theme's `key: '<theme>'` block, token by token. themes.ts also
 * imports under node:test now (its sibling import carries its extension), so the
 * parsed values are held to TOKENS — the parser cannot drift from the real theme —
 * and the themes measured are THEME_ORDER's, so a theme added tomorrow is measured
 * the day it lands (integration review minor, fix I). The band colours checked are
 * the ones bandTokens() renders from those values, so the ramp and its wiring are
 * pinned together.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { THEME_ORDER, TOKENS, type Tokens } from '../../utilities/theme/themes.ts';
import { contrast, deltaE2000, lab, lch, type Lab } from './colorScience.testkit.ts';
import { BAND_TOKEN, CRITICAL_PATH_TOKEN, FLOAT_BANDS_BY_FLOAT } from './bandRamp.ts';
import { bandTokens } from './bandTokens.ts';
import { SELECTION_TOKEN, STATUS_TOKEN } from '../construction/statusRamp.ts';
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
/** Every theme, from themes.ts itself: never a hand-kept list. */
const THEME_KEYS: readonly string[] = THEME_ORDER;

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

/** A token's literal hex in one theme; fails the test when the theme has none. */
function hexOf(t: Record<string, string>, key: string, token: string): string {
  const hex = t[token];
  assert.ok(hex !== undefined, `${key}: ${token} is a literal #RRGGBB in themes.ts`);
  return hex;
}

/** The hued status colours a band must stay clear of: every STATUS_TOKEN value but
 *  muted, plus the selection accent. */
const STATUS_HUED: readonly string[] = [
  ...new Set([...Object.values(STATUS_TOKEN).filter((v) => v !== 'muted'), SELECTION_TOKEN]),
];

/** Weight: the LOWER contrast against the two grounds the rail sits on. */
function weight(t: Record<string, string>, key: string, colour: string): number {
  return Math.min(
    contrast(colour, hexOf(t, key, 'paper')),
    contrast(colour, hexOf(t, key, 'paperAlt'))
  );
}

// 0 ---------------------------------------------------------------------------
void test('the themes measured are every theme, and the parsed literals are the TOKENS values', () => {
  assert.deepEqual(
    [...THEME_ORDER].sort(),
    Object.keys(TOKENS).sort(),
    'THEME_ORDER is every theme'
  );
  const measured = [
    'paper',
    'paperAlt',
    'criticalFg',
    'criticalText',
    ...Object.values(BAND_TOKEN),
    ...STATUS_HUED,
  ];
  for (const key of THEME_ORDER) {
    const t = themeTokens(key);
    const real = TOKENS[key] as unknown as Record<string, unknown>;
    for (const token of measured) {
      assert.equal(hexOf(t, key, token), real[token], `${key}.${token}: parsed vs TOKENS`);
    }
  }
});

// 1 ---------------------------------------------------------------------------
void test('CIEDE2000 matches the Sharma, Wu & Dalal (2005) reference pairs within 1e-4', () => {
  const pairs: readonly [Lab, Lab, number][] = [
    [[50, 2.6772, -79.7751], [50, 0, -82.7485], 2.0425],
    [[50, 0, 0], [50, -1, 2], 2.3669],
    [[50, 2.5, 0], [73, 25, -18], 27.1492],
    [[60.2574, -34.0099, 36.2677], [60.4626, -34.1751, 39.4387], 1.2644],
  ];
  for (const [one, two, want] of pairs) {
    const got = deltaE2000(one, two);
    assert.ok(
      Math.abs(got - want) < 1e-4,
      `ΔE2000(${one.join(', ')} / ${two.join(', ')}) = ${got.toFixed(6)}, want ${String(want)}`
    );
    // Symmetric, as the formula is.
    assert.ok(Math.abs(deltaE2000(two, one) - want) < 1e-4, 'ΔE2000 is symmetric');
  }
  // The conversions it is fed: white is L* 100, black 0; WCAG black-on-white is 21.
  assert.ok(Math.abs(lab('#FFFFFF')[0] - 100) < 1e-3, 'white is L* 100');
  assert.ok(Math.abs(lab('#000000')[0]) < 1e-9, 'black is L* 0');
  assert.ok(Math.abs(contrast('#000000', '#FFFFFF') - 21) < 1e-9, 'black on white is 21:1');
});

// 2 ---------------------------------------------------------------------------
void test('hue: the LCh hue rises by at least 12° at each step from critical to green', () => {
  for (const key of THEME_KEYS) {
    const t = themeTokens(key);
    const hues = FLOAT_BANDS_BY_FLOAT.map((band) => lch(bandColour(t, band))[2]);
    for (let i = 1; i < hues.length; i++) {
      const [before, after] = [hues[i - 1] ?? 0, hues[i] ?? 0];
      assert.ok(
        after - before >= 12,
        `${key}: ${String(FLOAT_BANDS_BY_FLOAT[i - 1])} (${before.toFixed(1)}°) → ${String(FLOAT_BANDS_BY_FLOAT[i])} (${after.toFixed(1)}°) rises less than 12°`
      );
    }
  }
});

// 3 ---------------------------------------------------------------------------
void test('weight: contrast against the ground falls ≥1.08× a step, green ≥3.2:1, and |ΔL*| from paper strictly falls', () => {
  for (const key of THEME_KEYS) {
    const t = themeTokens(key);
    const colours = FLOAT_BANDS_BY_FLOAT.map((band) => bandColour(t, band));
    const weights = colours.map((c) => weight(t, key, c));
    const paperL = lab(hexOf(t, key, 'paper'))[0];
    const deltaL = colours.map((c) => Math.abs(lab(c)[0] - paperL));
    for (let i = 1; i < colours.length; i++) {
      const [heavier, lighter] = [weights[i - 1] ?? 0, weights[i] ?? 0];
      const from = String(FLOAT_BANDS_BY_FLOAT[i - 1]);
      const to = String(FLOAT_BANDS_BY_FLOAT[i]);
      assert.ok(
        heavier >= 1.08 * lighter,
        `${key}: ${from} ${String(colours[i - 1])} (${heavier.toFixed(2)}:1) → ${to} ${String(colours[i])} (${lighter.toFixed(2)}:1) falls by only ${(heavier / lighter).toFixed(3)}×`
      );
      assert.ok(
        (deltaL[i - 1] ?? 0) > (deltaL[i] ?? 0),
        `${key}: |ΔL*| from paper must strictly fall ${from} (${(deltaL[i - 1] ?? 0).toFixed(1)}) → ${to} (${(deltaL[i] ?? 0).toFixed(1)})`
      );
    }
    const green = weights[weights.length - 1] ?? 0;
    assert.ok(green >= 3.2, `${key}: green is ${green.toFixed(2)}:1, below 3.2:1`);
  }
});

// 4 ---------------------------------------------------------------------------
void test('separation: every band is ≥15 CIEDE2000 from every hued status colour and the accent', () => {
  for (const key of THEME_KEYS) {
    const t = themeTokens(key);
    for (const band of FLOAT_BANDS_BY_FLOAT) {
      const bandToken = BAND_TOKEN[band];
      const colour = bandColour(t, band);
      for (const status of STATUS_HUED) {
        const hex = hexOf(t, key, status);
        const d = deltaE2000(lab(colour), lab(hex));
        assert.ok(
          d >= 15,
          `${key}: ${bandToken} ${colour} vs ${status} ${hex} is ΔE2000 ${d.toFixed(2)}, under 15`
        );
      }
    }
  }
});

// 5 ---------------------------------------------------------------------------
void test('adjacent bands are ≥10 CIEDE2000 apart', () => {
  for (const key of THEME_KEYS) {
    const t = themeTokens(key);
    for (let i = 1; i < FLOAT_BANDS_BY_FLOAT.length; i++) {
      const a = FLOAT_BANDS_BY_FLOAT[i - 1] ?? 'critical';
      const b = FLOAT_BANDS_BY_FLOAT[i] ?? 'green';
      const d = deltaE2000(lab(bandColour(t, a)), lab(bandColour(t, b)));
      assert.ok(d >= 10, `${key}: ${a} vs ${b} is ΔE2000 ${d.toFixed(2)}, under 10`);
    }
  }
});

// 6 ---------------------------------------------------------------------------
void test('token wiring: one critical token, failed is danger, blocked is awaiting, no status is a band or the accent', () => {
  assert.equal(BAND_TOKEN.critical, 'criticalFg');
  assert.equal(CRITICAL_PATH_TOKEN, BAND_TOKEN.critical);
  assert.equal(STATUS_TOKEN.failed, 'dangerFg');
  assert.equal(STATUS_TOKEN.blocked, 'awaitingFg');
  const bandTokenSet = new Set<string>(Object.values(BAND_TOKEN));
  for (const [status, token] of Object.entries(STATUS_TOKEN)) {
    assert.ok(!bandTokenSet.has(token), `${status} → ${token} is a band token`);
    assert.notEqual(token, 'accent', `${status} → the accent`);
  }
});

// 7 ---------------------------------------------------------------------------
void test('critical text on a critical fill clears 4.5:1 in every theme', () => {
  for (const key of THEME_KEYS) {
    const t = themeTokens(key);
    const fill = hexOf(t, key, 'criticalFg');
    const text = hexOf(t, key, 'criticalText');
    const ratio = contrast(text, fill);
    assert.ok(
      ratio >= 4.5,
      `${key}: criticalText ${text} on criticalFg ${fill} is ${ratio.toFixed(2)}:1`
    );
  }
});

// 8 ---------------------------------------------------------------------------
/** A line's `t.accent` in the selection branch (`isSelected ? … t.accent …` or
 *  `selected ? …`) is selection, the accent's own meaning; strip it first. */
function withoutSelectionBranch(line: string): string {
  return line.replace(/\b(?:isSelected|selected)\s*\?\s*(?:`[^`]*`|t\.accent\b)/g, '');
}

void test('source pin: no NetworkNode/NetworkView/NetworkSummaryStrip line paints a critical mark with the accent', () => {
  const ACCENT = /\bt\.accent\b/;
  // `critical` names the summary strip's CRITICAL PATH figure (designer final pass,
  // item 4: it was drawn in the accent).
  const CRITICAL = /\b(?:crit|critical|onCp|onCriticalPath)\b/;
  for (const file of ['NetworkNode.tsx', 'NetworkView.tsx', 'NetworkSummaryStrip.tsx']) {
    const src = readFileSync(new URL(`./${file}`, import.meta.url), 'utf8');
    src.split('\n').forEach((line, i) => {
      const rest = withoutSelectionBranch(line);
      assert.ok(
        !(ACCENT.test(rest) && CRITICAL.test(rest)),
        `${file}:${String(i + 1)} paints a critical mark with the accent: ${line.trim()}`
      );
    });
  }
});

// 9 ---------------------------------------------------------------------------
/** dangerFg means failed/error only (palette ruling, rule 2). The TASKS lens
 *  painted the risk-floor rule in it (final review minor); a line naming the risk
 *  floor must never use it. */
void test('source pin: no TasksLens line paints the risk floor with dangerFg', () => {
  const src = readFileSync(new URL('../construction/tasks/TasksLens.tsx', import.meta.url), 'utf8');
  src.split('\n').forEach((line, i) => {
    assert.ok(
      !(/\bt\.dangerFg\b/.test(line) && /\briskFloor\b/.test(line)),
      `TasksLens.tsx:${String(i + 1)} paints the risk floor with dangerFg: ${line.trim()}`
    );
  });
  // …and it does paint it, in the awaiting tone.
  assert.match(src, /riskFloor \? t\.awaitingFg : t\.ink/);
});
