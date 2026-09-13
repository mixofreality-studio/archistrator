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
import Link from '@mui/material/Link';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import WarningAmberIcon from '@mui/icons-material/WarningAmber';

import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { ActivityNode } from '../list/activityTree';
import { ProvenanceGroupStamp, ProvenanceRailMark } from '../provenance';
import { provenanceBasesOf, provenanceTooltipFor } from '../provenanceAxis';
import type { ActivityGraphModel } from './activityGraphModel';
import type { RibbonMilestone } from './gateRibbon';
import { SCHEDULE_CAPTION } from './laneSchedule';
import { M0_STALE_LABEL, m0PresentationFor, type M0Facts } from './m0Gate';
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

/**
 * M0 — the SDP review gate (PM Q4 ruling). Its state is the project's phase
 * and its amber flag the SDP review slot's staleness (m0Gate.ts); the chip and
 * hover copy are the ruling's, verbatim. No date, option, cost or duration.
 */
function M0Chip({
  m,
  m0,
  t,
  onHover,
  onOpenSdpReview,
}: {
  m: RibbonMilestone;
  m0: M0Facts;
  t: Tokens;
  onHover: (milestoneId: string | null) => void;
  onOpenSdpReview: (() => void) | undefined;
}): ReactElement {
  const p = m0PresentationFor(m0, m.gates.length);
  return (
    <Tooltip
      slotProps={{
        tooltip: {
          sx: {
            bgcolor: t.paper,
            color: t.ink,
            border: `1.5px solid ${t.line}`,
            boxShadow: 3,
            maxWidth: 380,
          },
        },
      }}
      title={
        <Box data-testid={UI_IDENTIFIERS.Construction.GRAPH_M0_HOVER} sx={{ p: 0.5 }}>
          <Typography sx={{ fontWeight: 700, fontSize: 12.5, color: t.ink }}>{p.title}</Typography>
          <Typography sx={{ fontSize: 12, lineHeight: 1.45, color: t.ink, mt: 0.5 }}>
            {p.body}
          </Typography>
          {p.link !== undefined && onOpenSdpReview !== undefined ? (
            <Link
              component="button"
              data-testid={UI_IDENTIFIERS.Construction.GRAPH_M0_OPEN_SDP}
              sx={{ mt: 0.75, fontSize: 12, color: t.accent }}
              onClick={onOpenSdpReview}
            >
              {p.link}
            </Link>
          ) : null}
        </Box>
      }
    >
      <Box
        data-m0-state={p.state}
        data-stale={String(p.stale)}
        data-testid={UI_IDENTIFIERS.Construction.graphMilestone(m.id)}
        sx={{
          display: 'flex',
          alignItems: 'center',
          flexShrink: 0,
          px: 1,
          py: 0.4,
          bgcolor: t.paper,
          border: `1.5px solid ${t.line}`,
          borderRadius: `${String(Math.min(t.radius, 8))}px`,
          fontFamily: t.mono,
          fontSize: 11,
          color: t.ink,
          whiteSpace: 'nowrap',
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
        {p.chipParts.map((part, i) => (
          <Box component="span" key={part} sx={{ display: 'inline-flex', alignItems: 'center' }}>
            {i > 0 ? ' · ' : ''}
            {part === M0_STALE_LABEL ? (
              <Box
                component="span"
                sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.3, fontWeight: 700 }}
              >
                <WarningAmberIcon aria-hidden sx={{ fontSize: 13, color: t.bandYellow }} />
                {part}
              </Box>
            ) : (
              <Box component="span" sx={{ fontWeight: i === 0 ? 800 : 400 }}>
                {part}
              </Box>
            )}
          </Box>
        ))}
      </Box>
    </Tooltip>
  );
}

export function GateRibbon({
  ribbon,
  nodes,
  m0,
  t,
  onHover,
  onOpenSdpReview,
}: {
  ribbon: readonly RibbonMilestone[];
  nodes: readonly ActivityNode[];
  /** M0's facts from the project read (m0Gate.m0FactsFor); absent reads "—". */
  m0: M0Facts;
  t: Tokens;
  onHover: (milestoneId: string | null) => void;
  /** Navigation only — the stale approval's way back to the SDP review. */
  onOpenSdpReview?: () => void;
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
        if (m.id === 'M0') {
          return (
            <M0Chip
              key={m.id}
              m={m}
              m0={m0}
              t={t}
              onHover={onHover}
              onOpenSdpReview={onOpenSdpReview}
            />
          );
        }
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
      <Box component="span">
        spine length = effort · rail + numeral = total float (days) · heavy left edge = critical
        path
      </Box>
      <Box
        component="span"
        data-testid={UI_IDENTIFIERS.Construction.GRAPH_SCHEDULE_CAPTION}
        sx={{ fontStyle: 'italic' }}
      >
        {SCHEDULE_CAPTION}
      </Box>
    </Box>
  );
}
