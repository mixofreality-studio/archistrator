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
 *  - GraphKeyBar: the live up/sideways check (R5), always visible, and ONE
 *    "Key" popover button beside it — how many components no activity builds,
 *    the spine's state key, the provenance hatch and the schedule channels.
 */
import { useState, type ReactElement, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import ButtonBase from '@mui/material/ButtonBase';
import Link from '@mui/material/Link';
import Popover from '@mui/material/Popover';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import WarningAmberIcon from '@mui/icons-material/WarningAmber';
import { alpha } from '@mui/material/styles';

import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { ActivityNode } from '../list/activityTree';
import { ProvenanceGroupStamp, ProvenanceRailMark, ReconstructedBadge } from '../provenance';
import {
  GRADE_LABEL,
  HATCH_INK_ALPHA,
  provenanceBasesOf,
  provenanceGradeOf,
  provenanceHatchFill,
  provenanceTooltipFor,
  reconstructedHintFor,
  type ProvenanceOrigin,
} from '../provenanceAxis';
import { bandTokens } from '../../project/bandTokens';
import { CRITICAL_PATH_TOKEN } from '../../project/bandRamp';
import type { ActivityGraphModel } from './activityGraphModel';
import type { RibbonMilestone } from './gateRibbon';
import { SCHEDULE_CAPTION } from './laneSchedule';
import { M0_STALE_LABEL, m0PresentationFor, type M0Facts, type M0Presentation } from './m0Gate';
import {
  SEGMENT_STATES,
  SEGMENT_STATE_LABEL,
  hollowKeyText,
  layeringCheckText,
  layeringCheckTone,
  ribbonCountLabel,
  segmentPaint,
} from './graphPresentation';

// ---------------------------------------------------------------------------
// The ribbon
// ---------------------------------------------------------------------------

/**
 * Where a reconstructed chip's basis is read: a feeder. The line before it is the
 * one fixed hint every reconstructed mark gives (provenanceAxis.reconstructedHintFor,
 * shared with the provenance rail since fix I).
 *
 * Never a feeder's own basis prose: that ran 948-1752 characters across a
 * milestone's several feeders, truncated mid-word, and overran the viewport at
 * 1366×768 (round 3 — the tooltip used to fold provenanceTooltipFor's full,
 * per-basis text in here). A reader who wants a basis opens the feeder itself.
 */
const FEEDER_POINTER = 'Select a feeder for its basis.';

/** A milestone chip's one tooltip: its count sentence, then — only when its
 *  feeders' worst provenance is reconstructed — the fixed hint and the pointer. */
function milestoneTooltipText(count: string, origin: ProvenanceOrigin): string {
  return provenanceGradeOf(origin) === 'reconstructed'
    ? `${count}\n\n${reconstructedHintFor(origin)} ${FEEDER_POINTER}`
    : count;
}

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
 * M0's copy — title, body, and on a stale approval the way back to the SDP
 * review. The mouse's hover tooltip and the chip's popover (keyboard, click)
 * show the same words.
 */
function M0Details({
  p,
  t,
  testId,
  onOpenSdpReview,
}: {
  p: M0Presentation;
  t: Tokens;
  testId: string;
  onOpenSdpReview: (() => void) | undefined;
}): ReactElement {
  return (
    <Box data-testid={testId} sx={{ p: 0.5 }}>
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
  );
}

/**
 * M0 — the SDP review gate (PM Q4 ruling). Its state is the project's phase
 * and its amber flag the SDP review slot's staleness (m0Gate.ts); the chip and
 * hover copy are the ruling's, verbatim. No date, option, cost or duration.
 *
 * The chip is a BUTTON (graph re-review): a tooltip's link cannot be reached by
 * keyboard — Tab leaves the chip for the next control, never into a portal. So
 * Enter (or a click) opens the same copy as a popover with focus inside it,
 * where "Open the SDP review →" is the first Tab stop; Escape closes it and
 * focus returns to the chip. The mouse keeps its hover tooltip, held shut while
 * the popover is open so the copy is on screen once.
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
  const [hovering, setHovering] = useState(false);
  const [popoverAnchor, setPopoverAnchor] = useState<HTMLElement | null>(null);
  return (
    <>
      <Tooltip
        disableFocusListener
        open={popoverAnchor === null ? hovering : false}
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
          <M0Details
            p={p}
            t={t}
            testId={UI_IDENTIFIERS.Construction.GRAPH_M0_HOVER}
            onOpenSdpReview={onOpenSdpReview}
          />
        }
        onClose={() => {
          setHovering(false);
        }}
        onOpen={() => {
          setHovering(true);
        }}
      >
        <ButtonBase
          disableRipple
          aria-expanded={popoverAnchor !== null}
          aria-haspopup="dialog"
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
          onBlur={() => {
            onHover(null);
          }}
          onClick={(e) => {
            setPopoverAnchor(e.currentTarget);
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
              {/* Its own span, spaces preserved: bare text inside an inline-flex
                  item loses its edge whitespace, so "M0·SDP review" ran
                  together (designer re-check 5). */}
              {i > 0 ? (
                <Box component="span" data-m0-separator="" sx={{ whiteSpace: 'pre' }}>
                  {' · '}
                </Box>
              ) : null}
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
        </ButtonBase>
      </Tooltip>
      <Popover
        anchorEl={popoverAnchor}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        open={popoverAnchor !== null}
        slotProps={{
          paper: {
            sx: { maxWidth: 380, p: 1, bgcolor: t.paper, border: `1.5px solid ${t.line}` },
          },
        }}
        onClose={() => {
          setPopoverAnchor(null);
        }}
      >
        <M0Details
          p={p}
          t={t}
          testId={UI_IDENTIFIERS.Construction.GRAPH_M0_POPOVER}
          onOpenSdpReview={onOpenSdpReview}
        />
      </Popover>
    </>
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
      // ONE line that scrolls sideways (designer P1-2): a wrapping ribbon took
      // two lines at 1366 and pushed the canvas below the fold.
      sx={{
        display: 'flex',
        flexWrap: 'nowrap',
        alignItems: 'stretch',
        gap: 0.75,
        overflowX: 'auto',
        overflowY: 'hidden',
        scrollbarWidth: 'thin',
        pb: 0.25,
      }}
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
          // ONE tooltip per chip (designer re-check 10): the chip's carries the
          // count sentence AND the provenance prose, and the stamp inside it
          // takes no pointer events, so its own tooltip never opens as well.
          <Tooltip
            key={m.id}
            title={
              <span style={{ whiteSpace: 'pre-line' }}>
                {milestoneTooltipText(countTooltip(m), m.provenance)}
              </span>
            }
          >
            <Box
              data-provenance={m.provenance}
              data-testid={UI_IDENTIFIERS.Construction.graphMilestone(m.id)}
              sx={{
                display: 'flex',
                alignItems: 'stretch',
                flexShrink: 0,
                whiteSpace: 'nowrap',
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
                <Box component="span" sx={{ display: 'inline-flex', pointerEvents: 'none' }}>
                  <ProvenanceGroupStamp reading={reading} t={t} />
                </Box>
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

/**
 * The check row: the live up/sideways check, always visible, and ONE "Key"
 * button beside it that opens the legend as a popover (designer P1-2) — the
 * inline key took up to four lines with the pane open and pushed the canvas
 * below the fold.
 */
export function GraphKeyBar({
  model,
  t,
}: {
  model: Pick<ActivityGraphModel, 'alarms' | 'sanctionedSideways' | 'hollowCount'>;
  t: Tokens;
}): ReactElement {
  // Red only for a real alarm; muted at zero (designer P2).
  const tone = layeringCheckTone(model);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
      <Box
        component="span"
        data-alarms={String(model.alarms.up + model.alarms.sideways)}
        data-testid={UI_IDENTIFIERS.Construction.GRAPH_LAYER_CHECK}
        data-tone={tone}
        sx={{
          flex: '1 1 auto',
          minWidth: 0,
          overflowX: 'auto',
          whiteSpace: 'nowrap',
          scrollbarWidth: 'thin',
          fontFamily: t.mono,
          fontSize: 10.5,
          color: tone === 'alarm' ? t.dangerFg : t.muted,
          fontWeight: 700,
        }}
        title="Every edge is an architecture call. In this layered drawing an upward call, or a sideways one other than the queued Manager→Manager call App C sanctions, is a layering violation and is drawn in red."
      >
        {layeringCheckText(model)}
      </Box>
      <Button
        aria-expanded={anchor !== null}
        aria-haspopup="dialog"
        data-testid={UI_IDENTIFIERS.Construction.GRAPH_KEY_BUTTON}
        size="small"
        sx={{
          flexShrink: 0,
          minWidth: 0,
          py: 0,
          px: 1,
          fontFamily: t.mono,
          fontSize: 10.5,
          fontWeight: 700,
          textTransform: 'none',
          color: t.ink,
          borderColor: t.line,
        }}
        variant="outlined"
        onClick={(e) => {
          setAnchor(e.currentTarget);
        }}
      >
        Key
      </Button>
      <Popover
        anchorEl={anchor}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
        open={anchor !== null}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        onClose={() => {
          setAnchor(null);
        }}
      >
        <GraphKeyLegend model={model} t={t} />
      </Popover>
    </Box>
  );
}

/** One key row: a drawn swatch, then its words. */
function KeyRow({ swatch, children }: { swatch: ReactElement; children: ReactNode }): ReactElement {
  return (
    <Box component="span" sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
      {swatch}
      <Box component="span">{children}</Box>
    </Box>
  );
}

/** The legend the Key button opens. */
function GraphKeyLegend({
  model,
  t,
}: {
  model: Pick<ActivityGraphModel, 'hollowCount'>;
  t: Tokens;
}): ReactElement {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.GRAPH_KEY}
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 0.75,
        p: 1.5,
        maxWidth: 440,
        bgcolor: t.paper,
        fontFamily: t.mono,
        fontSize: 10.5,
        color: t.muted,
      }}
    >
      <Box component="span">{hollowKeyText(model.hollowCount)}</Box>
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
                  borderLeft:
                    p.awaitingEdge !== undefined ? `3px solid ${p.awaitingEdge}` : undefined,
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
      {/* Drawn swatches, one channel a row (designer re-check 8): the copy used
          to read "hatched rail = ≈ RECONSTRUCTED", which said "= =". */}
      <KeyRow
        swatch={
          <Box
            data-testid={UI_IDENTIFIERS.Construction.graphKeySwatch('hatch')}
            sx={{
              width: 4,
              height: 14,
              flexShrink: 0,
              // The key draws the very hatch a lane's rail does.
              ...provenanceHatchFill(alpha(t.ink, HATCH_INK_ALPHA)),
            }}
          />
        }
      >
        hatched rail — reconstructed evidence; its card carries{' '}
        <Box component="span" sx={{ display: 'inline-flex', pointerEvents: 'none' }}>
          <ReconstructedBadge label={GRADE_LABEL.reconstructed} t={t} title="" />
        </Box>
      </KeyRow>
      <KeyRow
        swatch={
          <Box
            data-testid={UI_IDENTIFIERS.Construction.graphKeySwatch('spine')}
            sx={{ width: 28, height: 6, flexShrink: 0, bgcolor: t.line, borderRadius: '2px' }}
          />
        }
      >
        spine length — effort
      </KeyRow>
      <KeyRow
        swatch={
          <Box
            data-testid={UI_IDENTIFIERS.Construction.graphKeySwatch('float')}
            sx={{ display: 'inline-flex', alignItems: 'center', gap: '2px', flexShrink: 0 }}
          >
            <Box
              component="span"
              sx={{ width: 3, height: 10, bgcolor: bandTokens(t, 'red').fg, borderRadius: '1px' }}
            />
            <Box
              component="span"
              sx={{ fontFamily: t.mono, fontSize: 9, fontWeight: 700, color: t.ink }}
            >
              5
            </Box>
          </Box>
        }
      >
        rail and numeral — total float, in days
      </KeyRow>
      <KeyRow
        swatch={
          <Box
            data-testid={UI_IDENTIFIERS.Construction.graphKeySwatch('critical')}
            sx={{
              width: 14,
              height: 12,
              flexShrink: 0,
              boxSizing: 'border-box',
              bgcolor: t.paper,
              border: `1px solid ${alpha(t.line, 0.6)}`,
              borderLeft: `3px solid ${t[CRITICAL_PATH_TOKEN]}`,
            }}
          />
        }
      >
        heavy left edge — on the critical path
      </KeyRow>
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
