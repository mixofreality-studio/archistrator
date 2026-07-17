/**
 * The volatilities artifact as a compact AXES OVERVIEW on top of two labeled
 * LANES — never a 2-axis scatter. Each volatility carries a single categorical
 * axis, not a bivariate coordinate, so plotting fabricated x/y points collapsed
 * them onto an unreadable diagonal. The overview draws the book's iconic axes
 * (Y = same customer over time, X = all customers at one moment, arrows from a
 * shared origin) with each volatility as a labeled dot ALONG ITS OWN AXIS LINE,
 * evenly spaced by per-axis order — an honest rendering of the categorical
 * model. Both views stay visible (no toggle): the overview is the at-a-glance
 * shape of the analysis, the lanes are the browsable/accessible lists.
 *
 * A11y decision for the overview SVG: it is aria-hidden and out of the tab
 * order. Its dots are pointer-clickable shortcuts to the SAME selection the
 * lane chips drive; keyboard and screen-reader users have the full equivalent
 * interaction in the lanes, so exposing a second N-stop dot sequence would only
 * duplicate the surface. Geometry (spacing, arrow tips, label truncation) is
 * pure and unit-tested in volatilityMapLogic.axesLayout.
 *
 * Each lane is a single-select WAI-ARIA listbox (role=option + aria-selected,
 * roving tabindex — the CommentableList precedent): ↑/↓/Home/End move focus
 * within a lane, click/Enter/Space select (focus alone never selects), Escape
 * anywhere in the map clears. Selecting opens the side-rail inspect card (a
 * visually-hidden polite live region announces the swap) and arms a comment
 * anchor (`$.items[n]`) for the chat rail. Pure keyboard/announcement logic
 * lives in volatilityMapLogic.ts. Recolored from tokens.
 *
 * Below the lanes, rejected candidates (model `rejected`, absent on older
 * artifacts → renders nothing) appear in a collapsed disclosure (GatePanel's
 * button + aria-expanded/aria-controls pattern): name, classification chip,
 * reason, and a per-item comment anchor at `$.rejected[n]` — a path disjoint
 * from the accepted `$.items[n]`, so anchors never collide.
 */
import { useId, useMemo, useRef, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import Collapse from '@mui/material/Collapse';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import CloseIcon from '@mui/icons-material/Close';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { useParams } from '@tanstack/react-router';
import {
  toC4View,
  toVolatilityView,
  AXIS1_LABEL,
  AXIS2_LABEL,
  type VolatilityPoint,
} from '../contracts/adapters';
import type { ArtifactModelEnvelope, Axis, RejectedVolatility } from '../contracts/types';
import { useProject } from '../hooks/useProject';
import {
  useComments,
  volatilityAnchor,
  rejectedVolatilityAnchor,
} from './comments/CommentContext';
import {
  axesLayout,
  axisLabel,
  laneKeyAction,
  rejectionClassLabel,
  selectionAnnouncement,
  truncateLabel,
} from './volatilityMapLogic';
import { useTokens } from '../utilities/theme/ThemeContext';
import type { Tokens } from '../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

/**
 * Normalize a name to bare alphanumerics for a tolerant join: a component's
 * `encapsulates` prose typically leads with the volatility's exact name (often
 * "<Name>: …"), so a normalized containment match reliably links the two without
 * depending on punctuation/casing.
 */
function normalizeName(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9]/g, '');
}

function axisColor(t: Tokens, a: Axis): string {
  return a === 'sameCustomerOverTime' ? t.accent2 : t.committedDot;
}

/** A point paired with its stable index in the flat points array (the anchor). */
interface IndexedPoint {
  v: VolatilityPoint;
  i: number;
}

export function VolatilityMap({
  envelope,
}: {
  envelope: ArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const { setAnchor, enabled } = useComments();
  const params = useParams({ strict: false });
  const projectId = typeof params.projectId === 'string' ? params.projectId : '';
  const { data: project } = useProject(projectId);
  const [sel, setSel] = useState<number | null>(null);
  const { points, rejected } = toVolatilityView(envelope);
  const selected = sel !== null ? points[sel] : undefined;

  // Join the committed System artifact: which component encapsulates each volatility.
  // The component that names this volatility in its `encapsulates` prose owns it
  // (Method: one component per area of volatility). Keyed by normalized volatility
  // name; absent when the System isn't committed or no component claims it.
  const encapsulatedBy = useMemo(() => {
    const systemEnvelope = project?.slots.find((s) => s.kind === 'system')?.model;
    const components = toC4View(systemEnvelope).components;
    const m = new Map<string, string>();
    for (const p of toVolatilityView(envelope).points) {
      const key = normalizeName(p.name);
      const owner = components.find((c) => normalizeName(c.encapsulates).includes(key));
      if (owner !== undefined) m.set(key, owner.name);
    }
    return m;
  }, [project, envelope]);

  if (points.length === 0 && rejected.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No volatilities drafted yet.
      </Box>
    );
  }

  const indexed: IndexedPoint[] = points.map((v, i) => ({ v, i }));
  const axis1 = indexed.filter((p) => p.v.axis === 'sameCustomerOverTime');
  const axis2 = indexed.filter((p) => p.v.axis === 'allCustomersAtOneTime');

  return (
    <Box
      data-testid={UI_IDENTIFIERS.VolatilityMap.ROOT}
      sx={{ display: 'flex', gap: 2, flexDirection: 'column' }}
      onKeyDown={(e) => {
        // Escape anywhere within the map clears the selection (back to summary).
        if (e.key === 'Escape' && sel !== null) {
          e.stopPropagation();
          setSel(null);
        }
      }}
    >
      {points.length > 0 ? (
        <AxesOverview axis1={axis1} axis2={axis2} sel={sel} t={t} onSelect={setSel} />
      ) : null}

      <Box sx={{ display: 'flex', gap: 2, flexDirection: { xs: 'column', md: 'row' } }}>
        <Box
          sx={{ flexGrow: 1, display: 'flex', gap: 2, flexDirection: { xs: 'column', sm: 'row' } }}
        >
          <Lane
            axis="sameCustomerOverTime"
            color={t.accent2}
            items={axis1}
            sel={sel}
            subtitle="Varies for one customer over time."
            t={t}
            title={AXIS1_LABEL}
            onSelect={setSel}
          />
          <Lane
            axis="allCustomersAtOneTime"
            color={t.committedDot}
            items={axis2}
            sel={sel}
            subtitle="Differs across customers at one moment."
            t={t}
            title={AXIS2_LABEL}
            onSelect={setSel}
          />
        </Box>

        {/* side rail: selection inspect + comment */}
        <Box sx={{ width: { xs: '100%', md: 280 }, flexShrink: 0 }}>
          <Paper sx={{ p: 2 }}>
            <Typography sx={{ color: t.muted, mb: 1 }} variant="subtitle2">
              {points.length} VOLATILITIES
            </Typography>
            {selected !== undefined && sel !== null ? (
              <SelectionCard
                encapsulatedBy={encapsulatedBy.get(normalizeName(selected.name))}
                t={t}
                v={selected}
                onClear={() => {
                  setSel(null);
                }}
                onComment={
                  enabled
                    ? (): void => {
                        setAnchor({
                          kind: 'node',
                          label: selected.name,
                          source: 'Volatilities · axis lanes',
                          jsonPath: volatilityAnchor(sel),
                        });
                      }
                    : undefined
                }
              />
            ) : (
              <Typography
                data-testid={UI_IDENTIFIERS.VolatilityMap.SUMMARY}
                sx={{ color: t.muted, fontSize: 13.5, lineHeight: 1.6 }}
              >
                Two axes of change: the left lane evolves for one customer over time; the right
                lane differs across customers at one moment. Select a volatility to inspect
                {enabled ? ' or comment' : ''}.
              </Typography>
            )}
          </Paper>
        </Box>
      </Box>

      {rejected.length > 0 ? (
        <RejectedCandidates
          rejected={rejected}
          t={t}
          onComment={
            enabled
              ? (r, i): void => {
                  setAnchor({
                    kind: 'node',
                    label: r.name,
                    source: 'Volatilities · rejected candidates',
                    jsonPath: rejectedVolatilityAnchor(i),
                  });
                }
              : undefined
          }
        />
      ) : null}

      {/* Visually-hidden polite announcer for the summary↔inspect swap (the
          StageChip pattern): the keyed child only remounts — and so only
          announces — when the SELECTION changes, not on every re-render. */}
      <Box
        aria-live="polite"
        component="span"
        role="status"
        sx={{
          position: 'absolute',
          width: '1px',
          height: '1px',
          margin: '-1px',
          padding: 0,
          overflow: 'hidden',
          clipPath: 'inset(50%)',
          whiteSpace: 'nowrap',
        }}
      >
        {selected !== undefined && sel !== null ? (
          <span key={String(sel)}>{selectionAnnouncement(selected.name, selected.axis)}</span>
        ) : null}
      </Box>
    </Box>
  );
}

/**
 * The compact two-axis overview: the book's iconic axes as arrows from a shared
 * origin, each volatility a labeled dot evenly spaced ALONG ITS OWN AXIS LINE
 * (positions come from axesLayout — per-axis order only, no fabricated 2D
 * coordinates). Dots are pointer-clickable shortcuts driving the SAME selection
 * as the lane chips; the SVG is aria-hidden and unfocusable because the lanes
 * below are the complete accessible surface for the identical items/actions
 * (see the component doc comment).
 */
function AxesOverview({
  t,
  axis1,
  axis2,
  sel,
  onSelect,
}: {
  t: Tokens;
  /** Axis-1 points in lane order (vertical axis dots), with flat indexes. */
  axis1: IndexedPoint[];
  /** Axis-2 points in lane order (horizontal axis dots), with flat indexes. */
  axis2: IndexedPoint[];
  sel: number | null;
  onSelect: (i: number) => void;
}): ReactNode {
  const layout = axesLayout(axis1.length, axis2.length);
  const { origin, yArrowTip, xArrowTip } = layout;

  const dot = (p: IndexedPoint, cx: number, cy: number, color: string): ReactNode => (
    <circle
      cx={cx}
      cy={cy}
      cursor="pointer"
      data-testid={UI_IDENTIFIERS.VolatilityMap.dot(p.i)}
      fill={color}
      key={`${p.v.name}-${String(p.i)}`}
      r={sel === p.i ? 7 : 5}
      stroke={sel === p.i ? t.accent : t.paper}
      strokeWidth={sel === p.i ? 2.5 : 1.5}
      onClick={() => {
        onSelect(p.i);
      }}
    />
  );

  return (
    <Paper sx={{ p: 1.5 }}>
      <Box sx={{ overflowX: 'auto' }}>
        <svg
          aria-hidden="true"
          data-testid={UI_IDENTIFIERS.VolatilityMap.AXES}
          focusable="false"
          height={layout.height}
          style={{ display: 'block' }}
          width={layout.width}
        >
          {/* axis lines + arrowheads */}
          <line
            stroke={t.line}
            strokeWidth={1.5}
            x1={origin.x}
            x2={yArrowTip.x}
            y1={origin.y}
            y2={yArrowTip.y}
          />
          <line
            stroke={t.line}
            strokeWidth={1.5}
            x1={origin.x}
            x2={xArrowTip.x}
            y1={origin.y}
            y2={xArrowTip.y}
          />
          <polygon
            fill={t.line}
            points={`${String(yArrowTip.x)},${String(yArrowTip.y - 7)} ${String(yArrowTip.x - 4.5)},${String(yArrowTip.y + 2)} ${String(yArrowTip.x + 4.5)},${String(yArrowTip.y + 2)}`}
          />
          <polygon
            fill={t.line}
            points={`${String(xArrowTip.x + 7)},${String(xArrowTip.y)} ${String(xArrowTip.x - 2)},${String(xArrowTip.y - 4.5)} ${String(xArrowTip.x - 2)},${String(xArrowTip.y + 4.5)}`}
          />

          {/* axis labels, colored like their lanes */}
          <text
            fill={t.accent2}
            fontFamily={t.mono}
            fontSize={10.5}
            fontWeight={700}
            x={yArrowTip.x + 10}
            y={yArrowTip.y + 4}
          >
            {AXIS1_LABEL}
          </text>
          <text
            fill={t.committedDot}
            fontFamily={t.mono}
            fontSize={10.5}
            fontWeight={700}
            textAnchor="end"
            x={xArrowTip.x}
            y={xArrowTip.y - 12}
          >
            {AXIS2_LABEL}
          </text>

          {/* Axis-1 dots up the vertical axis, labels rightward into the empty
              quadrant (no 2D positions exist, so nothing is there to collide). */}
          {axis1.map((p, k) => {
            const d = layout.yDots[k];
            if (d === undefined) return null;
            return (
              <g key={`${p.v.name}-${String(p.i)}`}>
                {dot(p, d.x, d.y, t.accent2)}
                <text
                  fill={t.ink}
                  fontFamily={t.mono}
                  fontSize={10.5}
                  fontWeight={sel === p.i ? 700 : 400}
                  x={d.x + 12}
                  y={d.y + 3.5}
                >
                  {truncateLabel(p.v.name)}
                </text>
              </g>
            );
          })}

          {/* Axis-2 dots along the horizontal axis, tick-style rotated labels below. */}
          {axis2.map((p, k) => {
            const d = layout.xDots[k];
            if (d === undefined) return null;
            return (
              <g key={`${p.v.name}-${String(p.i)}`}>
                {dot(p, d.x, d.y, t.committedDot)}
                <text
                  fill={t.ink}
                  fontFamily={t.mono}
                  fontSize={10.5}
                  fontWeight={sel === p.i ? 700 : 400}
                  textAnchor="end"
                  transform={`rotate(-35 ${String(d.x)} ${String(d.y + 16)})`}
                  x={d.x}
                  y={d.y + 16}
                >
                  {truncateLabel(p.v.name, 16)}
                </text>
              </g>
            );
          })}
        </svg>
      </Box>
    </Paper>
  );
}

/**
 * The collapsed-by-default rejected-candidates disclosure (GatePanel's real
 * <button> + aria-expanded/aria-controls + Collapse pattern): name, human
 * classification chip, reason, and — when commenting is active — a per-item
 * anchor at `$.rejected[n]`.
 */
function RejectedCandidates({
  t,
  rejected,
  onComment,
}: {
  t: Tokens;
  rejected: RejectedVolatility[];
  /** Present only when commenting is active; omit on read-only surfaces. */
  onComment?: ((r: RejectedVolatility, index: number) => void) | undefined;
}): ReactNode {
  const [open, setOpen] = useState(false);
  const regionId = useId();

  return (
    <Paper sx={{ p: 0, overflow: 'hidden' }}>
      {/* A real <button> (not a click-only Box) so the disclosure is keyboard
          operable and announces its expanded state to assistive tech. */}
      <Box
        aria-controls={regionId}
        aria-expanded={open}
        component="button"
        data-testid={UI_IDENTIFIERS.VolatilityMap.REJECTED_TOGGLE}
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          px: 2,
          py: 1.25,
          cursor: 'pointer',
          width: '100%',
          textAlign: 'left',
          font: 'inherit',
          color: 'inherit',
          border: 'none',
          bgcolor: t.paperAlt,
          borderBottom: open ? `1.5px solid ${t.line}` : 'none',
        }}
        type="button"
        onClick={() => {
          setOpen((v) => !v);
        }}
      >
        <Typography
          sx={{ fontFamily: t.mono, fontWeight: 700, letterSpacing: '0.1em', fontSize: 12 }}
        >
          REJECTED CANDIDATES · {rejected.length}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        <ExpandMoreIcon sx={{ transform: open ? 'rotate(180deg)' : 'none', transition: '120ms' }} />
      </Box>

      <Collapse id={regionId} in={open}>
        <Box
          data-testid={UI_IDENTIFIERS.VolatilityMap.REJECTED_LIST}
          sx={{ p: 2, display: 'flex', flexDirection: 'column', gap: 1.75 }}
        >
          {rejected.map((r, i) => (
            <Box
              data-testid={UI_IDENTIFIERS.VolatilityMap.rejectedItem(i)}
              key={`${r.name}-${String(i)}`}
              sx={{
                borderLeft: `3px dashed ${t.line}`,
                pl: 1.5,
                display: 'flex',
                flexDirection: 'column',
                gap: 0.5,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
                <Typography
                  sx={{
                    fontFamily: t.mono,
                    fontWeight: 700,
                    fontSize: 12.5,
                    color: t.ink,
                    wordBreak: 'break-word',
                  }}
                >
                  {r.name}
                </Typography>
                <Typography
                  sx={{
                    fontFamily: t.mono,
                    fontSize: 10.5,
                    color: t.muted,
                    border: `1.5px solid ${t.line}`,
                    borderRadius: 1,
                    px: 0.75,
                    whiteSpace: 'nowrap',
                  }}
                >
                  {rejectionClassLabel(r.class)}
                </Typography>
                <Box sx={{ flexGrow: 1 }} />
                {onComment !== undefined ? (
                  <Tooltip title="Comment on this rejected candidate">
                    <IconButton
                      aria-label={`Comment on ${r.name} (rejected candidate)`}
                      size="small"
                      sx={{ flexShrink: 0, color: t.muted }}
                      onClick={() => {
                        onComment(r, i);
                      }}
                    >
                      <ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />
                    </IconButton>
                  </Tooltip>
                ) : null}
              </Box>
              <Typography sx={{ color: t.muted, fontSize: 13, lineHeight: 1.55 }}>
                {r.reason}
              </Typography>
            </Box>
          ))}
        </Box>
      </Collapse>
    </Paper>
  );
}

function Lane({
  t,
  color,
  title,
  subtitle,
  axis,
  items,
  sel,
  onSelect,
}: {
  t: Tokens;
  color: string;
  title: string;
  subtitle: string;
  /** The lane's Löwy axis — the listbox identity (test id + adapters key). */
  axis: Axis;
  items: IndexedPoint[];
  sel: number | null;
  onSelect: (i: number) => void;
}): ReactNode {
  // Roving tab stop WITHIN this lane (each lane is one listbox, one tab stop).
  const [focused, setFocused] = useState(0);
  const chipRefs = useRef<(HTMLDivElement | null)[]>([]);
  const focusedPos = Math.min(focused, items.length - 1);

  return (
    <Paper sx={{ flex: 1, p: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <Box sx={{ p: 1.5, borderBottom: `1px solid ${t.line}`, borderTop: `4px solid ${color}` }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12.5, color: t.ink }}>
          {title} · {items.length}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
          {subtitle}
        </Typography>
      </Box>
      <Box
        aria-label={`${title} volatilities`}
        data-testid={UI_IDENTIFIERS.VolatilityMap.lane(axis)}
        role="listbox"
        sx={{
          p: 1,
          display: 'flex',
          flexDirection: 'column',
          gap: 0.75,
          overflowY: 'auto',
          // Bound the lane so internal scroll actually engages for 20+ items
          // instead of growing the page unboundedly.
          maxHeight: 480,
        }}
      >
        {items.map(({ v, i }, pos) => (
          <VolChip
            active={sel === i}
            chipRef={(el) => {
              chipRefs.current[pos] = el;
            }}
            color={color}
            index={i}
            key={`${v.name}-${String(i)}`}
            t={t}
            tabIndex={pos === focusedPos ? 0 : -1}
            v={v}
            onFocus={() => {
              setFocused(pos);
            }}
            onKeyDown={(e) => {
              const action = laneKeyAction(e.key, pos, items.length);
              if (action.kind === 'move') {
                e.preventDefault();
                setFocused(action.index);
                chipRefs.current[action.index]?.focus();
              } else if (action.kind === 'select') {
                e.preventDefault();
                onSelect(i);
              }
            }}
            onSelect={() => {
              onSelect(i);
            }}
          />
        ))}
      </Box>
    </Paper>
  );
}

function VolChip({
  t,
  v,
  color,
  active,
  index,
  tabIndex,
  chipRef,
  onSelect,
  onFocus,
  onKeyDown,
}: {
  t: Tokens;
  v: VolatilityPoint;
  color: string;
  /** This chip is THE selected one (single-select across both lanes). */
  active: boolean;
  /** The point's index in the flat points array — the comment-anchor identity. */
  index: number;
  /** The lane's roving tab stop: 0 on the focused option, -1 elsewhere. */
  tabIndex: number;
  chipRef: (el: HTMLDivElement | null) => void;
  /** Select this chip — the single source of truth for the inspector. Fired by
   *  click and Enter/Space ONLY; keyboard focus merely moves the roving tab stop
   *  so screen-reader/keyboard users can browse without committing a selection. */
  onSelect: () => void;
  /** Lane focus tracking (roving tabindex) — never selects. */
  onFocus: () => void;
  /** Lane keyboard handler (arrows/Home/End move, Enter/Space select). */
  onKeyDown: (e: React.KeyboardEvent) => void;
}): ReactNode {
  return (
    <Box
      aria-selected={active}
      data-testid={UI_IDENTIFIERS.VolatilityMap.chip(index)}
      ref={chipRef}
      role="option"
      sx={{
        cursor: 'pointer',
        px: 1,
        py: 0.75,
        display: 'flex',
        alignItems: 'center',
        gap: 0.75,
        bgcolor: active ? color : t.paperAlt,
        color: active ? t.accentText : t.ink,
        border: `1.5px solid ${active ? t.accent : t.line}`,
        borderLeft: `4px solid ${color}`,
        borderRadius: t.radius / 8 + 0.5,
        boxShadow: active ? `0 0 0 2px ${t.accent}` : 'none',
        '&:hover': { borderColor: t.accent },
        '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 1 },
      }}
      tabIndex={tabIndex}
      onClick={onSelect}
      onFocus={onFocus}
      onKeyDown={onKeyDown}
    >
      <Box sx={{ width: 7, height: 7, borderRadius: '50%', bgcolor: color, flexShrink: 0 }} />
      <Typography
        sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11.5, wordBreak: 'break-word' }}
      >
        {v.name}
      </Typography>
    </Box>
  );
}

function SelectionCard({
  v,
  t,
  encapsulatedBy,
  onClear,
  onComment,
}: {
  v: VolatilityPoint;
  t: Tokens;
  /** The System component that encapsulates this volatility, when the join resolves. */
  encapsulatedBy?: string | undefined;
  /** Back affordance: clear the selection and return to the summary card. */
  onClear: () => void;
  /** Present only when commenting is active; omit on read-only surfaces. */
  onComment?: (() => void) | undefined;
}): ReactNode {
  return (
    <Box data-testid={UI_IDENTIFIERS.VolatilityMap.DETAIL}>
      {/* Header row: name/axis on the left, the comment + clear affordances PINNED
          top-right so they never sink below a long rationale (P2-8). */}
      <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1 }}>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography
            component="h3"
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 15,
              color: t.ink,
              wordBreak: 'break-word',
            }}
          >
            {v.name}
          </Typography>
          <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: axisColor(t, v.axis) }}>
            {axisLabel(v.axis)}
          </Typography>
        </Box>
        {onComment !== undefined ? (
          <Tooltip title="Comment on this volatility">
            <IconButton
              aria-label={`Comment on ${v.name} (volatility)`}
              size="small"
              sx={{
                flexShrink: 0,
                color: t.accentText,
                bgcolor: t.accent,
                border: `1.5px solid ${t.line}`,
                borderRadius: 1,
                '&:hover': { bgcolor: t.accent2 },
              }}
              onClick={onComment}
            >
              <ChatBubbleOutlineIcon sx={{ fontSize: 15 }} />
            </IconButton>
          </Tooltip>
        ) : null}
        <Tooltip title="Clear selection">
          <IconButton
            aria-label="Clear selection"
            data-testid={UI_IDENTIFIERS.VolatilityMap.DETAIL_CLOSE}
            size="small"
            sx={{ flexShrink: 0, color: t.muted }}
            onClick={onClear}
          >
            <CloseIcon sx={{ fontSize: 15 }} />
          </IconButton>
        </Tooltip>
      </Box>

      {encapsulatedBy !== undefined ? (
        <Typography
          sx={{
            mt: 1,
            fontFamily: t.mono,
            fontSize: 11.5,
            color: t.ink,
            display: 'flex',
            flexWrap: 'wrap',
            gap: 0.5,
          }}
        >
          <Box component="span" sx={{ color: t.muted }}>
            Encapsulated by
          </Box>
          <Box component="span" sx={{ fontWeight: 700 }}>
            {encapsulatedBy}
          </Box>
        </Typography>
      ) : null}

      <Typography sx={{ color: t.muted, fontSize: 13.5, lineHeight: 1.6, mt: 1.5 }}>
        {v.rationale}
      </Typography>

      {/* Requirement traceability, when the artifact recorded it (SR-… ids). */}
      {v.traces !== undefined ? (
        <Typography
          component="p"
          sx={{ mt: 1.5, fontFamily: t.mono, fontSize: 11, color: t.muted }}
          variant="caption"
        >
          Traces: {v.traces.join(', ')}
        </Typography>
      ) : null}
    </Box>
  );
}
