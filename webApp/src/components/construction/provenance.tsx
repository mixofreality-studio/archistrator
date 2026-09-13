/* eslint-disable react-refresh/only-export-components -- the pure axis is re-exported
   alongside its two components, the same colocation ActivityTreeView.tsx / status.tsx /
   computed.tsx already use. */
/**
 * The provenance axis, rendered.
 *
 * The RULES live in provenanceAxis.ts (a plain `.ts` sibling, because Node's
 * type-stripping test runner cannot load a `.tsx` module at all) and this file
 * is the geometry over them — the same split as activityTree.ts /
 * ActivityTreeView.tsx and detailPaneState.ts / DetailPane.tsx. The pure module
 * is re-exported from here so a caller reaches the whole axis — the rail, the
 * badge, and the functions behind both — through one import.
 *
 * WHAT EACH GRADE LOOKS LIKE
 * --------------------------
 *   recorded       an empty 3px column. No ink. The absence of a mark IS the
 *                  statement, and it is the only grade allowed to be silent.
 *   reconstructed  the column filled with the house scanline hatch, in the
 *                  row's own ink. Plus, on GROUP headers only, the
 *                  `≈ RECONSTRUCTED` badge.
 *   unknown        a dashed hairline at the column's edge. Every activity nothing
 *                  has been attempted on has an empty ledger, so this one is drawn at
 *                  the very bottom of the ink budget: FloatRail already learnt
 *                  on this surface that a hairline per row turns into
 *                  ruled-paper texture and drowns the rows that carry data.
 *
 * The ink is ONE token for every grade (`currentColor`, set here). That is not
 * an implementation detail — it is the guarantee that no colour ever encodes
 * provenance, which would collide with the status and float channels that own
 * colour on this surface.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Tooltip from '@mui/material/Tooltip';
import { alpha } from '@mui/material/styles';

import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { ReconstructedBadge } from '../project/computed';
import {
  provenanceBasesOf,
  provenanceRailFor,
  provenanceRailTooltipFor,
  provenanceTooltipFor,
  worstOriginOf,
  type ProvenanceBearing,
  type ProvenanceOrigin,
} from './provenanceAxis.ts';

export {
  GRADE_LABEL,
  provenanceBasesOf,
  provenanceGradeOf,
  provenanceRailFor,
  provenanceTooltipFor,
  worstOriginOf,
  type ProvenanceBearing,
  type ProvenanceGrade,
  type ProvenanceOrigin,
  type ProvenanceRail,
} from './provenanceAxis.ts';
export { ReconstructedBadge } from '../project/computed';

/**
 * One node's provenance, read once. Both marks are driven from this so a row's
 * rail and its group's badge can never disagree about the same node.
 */
export interface ProvenanceReading {
  origin: ProvenanceOrigin;
  /** The distinct basis strings behind the reconstructed attempts beneath it. */
  bases: string[];
  /** The tooltip prose — sub-grade plus basis. */
  tooltip: string;
}

/** The rail column's own width — `provenanceRailFor` owns the drawn width; the
 *  column is one px wider so the hatch never abuts the row's content. */
const RAIL_COLUMN_PX = 4;

export function readProvenance(node: ProvenanceBearing): ProvenanceReading {
  const origin = worstOriginOf(node);
  const bases = provenanceBasesOf(node);
  return { origin, bases, tooltip: provenanceTooltipFor(origin, bases) };
}

/**
 * The rail — the mark EVERY tier carries, including the task rows the badge
 * deliberately stays off.
 *
 * Rendered as a fixed-width column rather than a border so the three tiers line
 * up into one continuous vertical band when a group is expanded: that
 * continuity is what makes the hatch read as a material the whole subtree is
 * made of, rather than as a per-row annotation.
 */
export function ProvenanceRailMark({
  reading,
  t,
}: {
  reading: ProvenanceReading;
  t: Tokens;
}): ReactElement {
  const rail = provenanceRailFor(reading.origin);

  // A rail is drawn exactly when the grade asks for a width. `recorded` earns no
  // decoration (the absence of a mark IS "we watched this happen") and `unknown`
  // is already spoken for by the surface's existing dashed marks — see
  // provenanceRailFor. The column still reserves its space in both cases so the
  // three tiers stay aligned into one band.
  if (rail.widthPx === 0) {
    return <Box sx={{ width: RAIL_COLUMN_PX, alignSelf: 'stretch', flexShrink: 0 }} />;
  }

  // The fixed hint, never the basis (fix I): the rail sits nested in a row or a
  // lane, and a basis-long tooltip here was a wall of text over it.
  return (
    <Tooltip title={provenanceRailTooltipFor(reading.origin)}>
      <Box
        data-provenance={reading.origin}
        data-testid={UI_IDENTIFIERS.Construction.PROVENANCE_RAIL}
        sx={{
          width: RAIL_COLUMN_PX,
          alignSelf: 'stretch',
          flexShrink: 0,
          // ONE ink, set from the theme and never from the grade. The texture is
          // the whole channel.
          color: alpha(t.ink, 0.75),
          backgroundImage: rail.texture,
          backgroundSize: `${String(rail.widthPx)}px 100%`,
          backgroundRepeat: 'repeat-y',
          backgroundPosition: 'left top',
        }}
      />
    </Tooltip>
  );
}

/**
 * The GROUP stamp — tier 1 and tier 2 only.
 *
 * Nothing renders for `recorded` or `unknown`. That is density rule 1: the
 * badge exists to name the one grade a reader would otherwise mistake for fact,
 * and putting a chip on the other two would put a mark back on every row and
 * reproduce the density failure that got two prototype rounds rejected.
 */
export function ProvenanceGroupStamp({
  reading,
  t,
}: {
  reading: ProvenanceReading;
  t: Tokens;
}): ReactElement | null {
  if (provenanceRailFor(reading.origin).grade !== 'reconstructed') return null;
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.PROVENANCE_BADGE}
      sx={{ display: 'inline-flex', flexShrink: 0 }}
    >
      <ReconstructedBadge t={t} title={reading.tooltip} />
    </Box>
  );
}
