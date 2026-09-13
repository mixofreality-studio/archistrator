/**
 * The float-criticality band → theme-token mapping (Löwy ch.8 §2), shared by the
 * network node, the dependency edges, the legend swatches, and the minimap. Lives
 * in its own module (no component export) so React Fast Refresh stays happy and so
 * the node-colouring and edge-colouring can never drift apart. All colours come
 * from theme tokens — never hardcoded.
 *
 * Which token carries each band is bandRamp.ts's BAND_TOKEN (designer palette
 * ruling): the lower the float, the heavier the mark. Critical (float 0) is
 * criticalFg, the ONE "critical" colour every critical-path mark reads too
 * (CRITICAL_PATH_TOKEN) — never the accent, and never dangerFg (failed/error only).
 */
import { alpha } from '@mui/material/styles';
import type { Tokens } from '../../utilities/theme/themes';
import type { FloatBand } from '../../contracts/projectAdapters';
import { BAND_TOKEN } from './bandRamp.ts';

export interface BandTokens {
  /** The strong band colour (left-border, chip text/border, minimap fill). */
  fg: string;
  /** A soft band tint for the card fill (~0.16 alpha, like awaitingBg). */
  soft: string;
}

export function bandTokens(t: Tokens, band: FloatBand): BandTokens {
  const fg = t[BAND_TOKEN[band]];
  return { fg, soft: alpha(fg, 0.16) };
}

export const BAND_LABEL: Record<FloatBand, string> = {
  critical: 'critical',
  red: 'red ≤5d float',
  yellow: 'yellow 6–25d float',
  green: 'green ≥26d float',
};
