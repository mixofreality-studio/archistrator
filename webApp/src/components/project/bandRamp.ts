/**
 * The float bands' colour ramp: which theme token carries each band (designer
 * re-check #12). No runtime imports, so node:test pins it against the real theme
 * values (bandRamp.test.ts); bandTokens.ts builds the colours from it.
 *
 * The lower the float, the stronger the alarm: critical (float 0) takes the
 * danger colour, red (≤5d) an orange-red, then yellow, then green. Critical used
 * to take the ACCENT, which in the Retro theme is a rust, weaker than the danger
 * red the ≤5d band took, so float 0 read as less alarming than float 5; in the
 * other themes the accent is a lavender or a cyan, no alarm colour at all.
 */
import type { FloatBand } from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';

/** The bands in float order: 0 first, most slack last. */
export const FLOAT_BANDS_BY_FLOAT: readonly FloatBand[] = ['critical', 'red', 'yellow', 'green'];

/** The theme token each band's colour comes from. */
export const BAND_TOKEN = {
  critical: 'dangerFg',
  red: 'bandRed',
  yellow: 'bandYellow',
  green: 'bandGreen',
} as const satisfies Record<FloatBand, keyof Tokens>;
