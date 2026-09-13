/**
 * The float-criticality band → theme-token mapping (Löwy ch.8 §2), shared by the
 * network node, the dependency edges, the legend swatches, and the minimap. Lives
 * in its own module (no component export) so React Fast Refresh stays happy and so
 * the node-colouring and edge-colouring can never drift apart. All colours come
 * from theme tokens — never hardcoded.
 */
import { alpha } from '@mui/material/styles';
import type { Tokens } from '../../utilities/theme/themes';
import type { FloatBand } from '../../contracts/projectAdapters';

export interface BandTokens {
  /** The strong band colour (left-border, chip text/border, minimap fill). */
  fg: string;
  /** A soft band tint for the card fill (~0.16 alpha, like awaitingBg). */
  soft: string;
}

/**
 * The scale is MONOTONIC in alarm (orchestrator ruling, designer re-check): the
 * lower the float, the stronger the alarm — critical (zero float) in the danger
 * ink, then amber, yellow, green, a red→green ramp in every theme (pinned by
 * bandTokens.test.ts). Critical used to take the theme's ACCENT — rust, purple
 * or cyan by theme — so a 0-day activity read milder than a 5-day one.
 */
export function bandTokens(t: Tokens, band: FloatBand): BandTokens {
  switch (band) {
    case 'critical':
      return { fg: t.dangerFg, soft: alpha(t.dangerFg, 0.16) };
    case 'red':
      return { fg: t.awaitingFg, soft: alpha(t.awaitingFg, 0.16) };
    case 'yellow':
      return { fg: t.bandYellow, soft: alpha(t.bandYellow, 0.16) };
    case 'green':
      return { fg: t.bandGreen, soft: alpha(t.bandGreen, 0.16) };
  }
}

export const BAND_LABEL: Record<FloatBand, string> = {
  critical: 'critical',
  red: 'red ≤5d float',
  yellow: 'yellow 6–25d float',
  green: 'green ≥26d float',
};
