/**
 * The two strips above the GRAPH lens's canvas — HTML, not canvas, so they
 * never pan away (Decision D8):
 *
 *  - GateRibbon: the network's milestones (gateRibbon.ts). A completion count
 *    appears ONLY when every feeder's evidence was observed; otherwise the
 *    chip reads an em dash and says why (spec §9.2 — never a badged number).
 *    A feeder carrying reconstructed evidence puts the hatch and the
 *    spelled-out `≈ RECONSTRUCTED` stamp on the chip. Hovering a chip
 *    hover-focuses its feeders on the canvas (or, for M0, what it gates).
 *  - GraphKey: how to read the canvas — the live App C layering check (R5),
 *    how many components no activity builds, and the spine's state key.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';

import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { ActivityNode } from '../list/activityTree';
import { ProvenanceGroupStamp, ProvenanceRailMark } from '../provenance';
import { provenanceBasesOf, provenanceTooltipFor } from '../provenanceAxis';
import type { ActivityGraphModel } from './activityGraphModel';
import type { RibbonMilestone } from './gateRibbon';
import {
  SEGMENT_STATES,
  SEGMENT_STATE_LABEL,
  layeringCheckText,
  ribbonCountLabel,
  segmentPaint,
} from './graphPresentation';

// ---------------------------------------------------------------------------
// The ribbon
// ---------------------------------------------------------------------------

function countTooltip(m: RibbonMilestone): string {
  if (m.feeders.length === 0) {
    return `Gates ${String(m.gates.length)} activities. It has no feeders of its own, so it asserts no completion state.`;
  }
  if (m.complete === undefined) {
    const n = m.unobserved > 0 ? m.unobserved : m.unreported;
    const feeders = n === 1 ? '1 feeder has' : `${String(n)} feeders have`;
    return m.unobserved > 0
      ? `No completion count: ${feeders} no observed record.`
      : `No completion count: ${feeders} a phase not yet reported.`;
  }
  return `${String(m.complete)} of ${String(m.feeders.length)} feeders have passed every lifecycle gate, all of it observed.`;
}

export function GateRibbon({
  ribbon,
  nodes,
  t,
  onHover,
}: {
  ribbon: readonly RibbonMilestone[];
  nodes: readonly ActivityNode[];
  t: Tokens;
  onHover: (milestoneId: string | null) => void;
}): ReactElement | null {
  if (ribbon.length === 0) return null;
  const byId = new Map(nodes.map((n) => [n.activityId, n]));

  return (
    <Box
      aria-label="Milestones"
      data-testid={UI_IDENTIFIERS.Construction.GRAPH_RIBBON}
      sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'stretch', gap: 0.75 }}
    >
      {ribbon.map((m) => {
        const feederNodes = m.feeders
          .map((id) => byId.get(id))
          .filter((n): n is ActivityNode => n !== undefined);
        const bases = provenanceBasesOf({ phases: feederNodes });
        const reading = {
          origin: m.provenance,
          bases,
          tooltip: provenanceTooltipFor(m.provenance, bases),
        };
        return (
          <Tooltip key={m.id} title={countTooltip(m)}>
            <Box
              data-provenance={m.provenance}
              data-testid={UI_IDENTIFIERS.Construction.graphMilestone(m.id)}
              sx={{
                display: 'flex',
                alignItems: 'stretch',
                gap: 0.6,
                pr: 1,
                bgcolor: t.paper,
                border: `1.5px solid ${t.line}`,
                borderRadius: `${String(Math.min(t.radius, 8))}px`,
                overflow: 'hidden',
                '&:focus-visible': { outline: `2px solid ${t.accent}` },
              }}
              tabIndex={0}
              onBlur={() => {
                onHover(null);
              }}
              onFocus={() => {
                onHover(m.id);
              }}
              onMouseEnter={() => {
                onHover(m.id);
              }}
              onMouseLeave={() => {
                onHover(null);
              }}
            >
              <ProvenanceRailMark reading={reading} t={t} />
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, py: 0.4 }}>
                <Typography
                  sx={{ fontFamily: t.mono, fontSize: 11, fontWeight: 800, color: t.ink }}
                >
                  {m.id}
                </Typography>
                <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink }}>
                  {m.name}
                </Typography>
                <Typography
                  sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, whiteSpace: 'nowrap' }}
                >
                  {m.feeders.length > 0 ? `${String(m.feeders.length)} feeders · ` : ''}
                  {ribbonCountLabel(m)}
                </Typography>
                <ProvenanceGroupStamp reading={reading} t={t} />
              </Box>
            </Box>
          </Tooltip>
        );
      })}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// The key
// ---------------------------------------------------------------------------

export function GraphKey({
  model,
  t,
}: {
  model: Pick<ActivityGraphModel, 'alarms' | 'sanctionedSideways' | 'hollowCount'>;
  t: Tokens;
}): ReactElement {
  const alarmed = model.alarms.up + model.alarms.sideways > 0;
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.GRAPH_KEY}
      sx={{
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        columnGap: 1.5,
        rowGap: 0.5,
        fontFamily: t.mono,
        fontSize: 10.5,
        color: t.muted,
      }}
    >
      <Box
        component="span"
        data-alarms={String(model.alarms.up + model.alarms.sideways)}
        data-testid={UI_IDENTIFIERS.Construction.GRAPH_LAYER_CHECK}
        sx={{ color: alarmed ? t.dangerFg : t.ink, fontWeight: 700 }}
        title="Every edge is an architecture call. In this layered drawing an upward call, or a sideways one other than the queued Manager→Manager call App C sanctions, is a layering violation and is drawn in red."
      >
        {layeringCheckText(model)}
      </Box>
      <Box component="span">
        {model.hollowCount} {model.hollowCount === 1 ? 'component' : 'components'} with no activity
        (dashed)
      </Box>
      <Box component="span" sx={{ display: 'inline-flex', flexWrap: 'wrap', gap: 1 }}>
        {SEGMENT_STATES.map((s) => {
          const p = segmentPaint(t, s);
          return (
            <Box
              component="span"
              key={s}
              sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.4 }}
            >
              <Box
                component="span"
                sx={{
                  width: 14,
                  height: 7,
                  boxSizing: 'border-box',
                  bgcolor: p.fill,
                  opacity: p.opacity,
                  border: p.borderStyle === 'none' ? 'none' : `1px ${p.borderStyle} ${p.border}`,
                  borderLeft: p.accentEdge !== undefined ? `3px solid ${p.accentEdge}` : undefined,
                  ...(s === 'absent'
                    ? {
                        backgroundImage: `linear-gradient(${t.line}, ${t.line})`,
                        backgroundSize: '100% 1px',
                        backgroundPosition: 'center',
                        backgroundRepeat: 'no-repeat',
                      }
                    : {}),
                }}
              />
              {SEGMENT_STATE_LABEL[s]}
            </Box>
          );
        })}
      </Box>
      <Box component="span">hatched rail = ≈ RECONSTRUCTED</Box>
    </Box>
  );
}
