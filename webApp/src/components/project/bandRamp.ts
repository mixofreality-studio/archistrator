/**
 * The float bands' colour ramp: which theme token carries each band (designer
 * palette ruling, fix I). No runtime imports, so node:test pins it against the
 * real theme values (bandRamp.test.ts); bandTokens.ts builds the colours from it.
 *
 * The lower the float, the heavier the mark: critical (float 0) takes criticalFg —
 * the ONE "critical" colour, which every critical-path mark also reads
 * (CRITICAL_PATH_TOKEN) — then bandRed (≤5d), bandYellow, bandGreen. Weight is
 * contrast against the ground, falling at every step; the hue climbs at every
 * step; and no band is near a status colour or the accent. Critical used to take
 * the accent, and then dangerFg, which is failed/error only.
 */
import type { FloatBand } from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';

/** The bands in float order: 0 first, most slack last. */
export const FLOAT_BANDS_BY_FLOAT: readonly FloatBand[] = ['critical', 'red', 'yellow', 'green'];

/** The theme token each band's colour comes from. */
export const BAND_TOKEN = {
  critical: 'criticalFg',
  red: 'bandRed',
  yellow: 'bandYellow',
  green: 'bandGreen',
} as const satisfies Record<FloatBand, keyof Tokens>;

/** The token every critical-path mark reads: the float-0 band's own colour. */
export const CRITICAL_PATH_TOKEN = BAND_TOKEN.critical;
