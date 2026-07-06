/**
 * The volatilities artifact as two labeled LANES — one column per Löwy axis —
 * NOT a 2-axis scatter. Each volatility carries a single categorical axis, not a
 * bivariate coordinate, so plotting x/y fabricated points collapsed them onto an
 * unreadable diagonal. Lanes render one honest, collision-free vertical list per
 * axis. Bound to adapters.toVolatilityView (the flat VolatilityPoint[]; the
 * point's index in that array is the stable comment anchor). Chips are clickable
 * AND keyboard-operable: selecting one opens an inspect card and arms a comment
 * anchor (`$.items[n]`) for the chat rail. Recolored from tokens.
 */
import { useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { useParams } from '@tanstack/react-router';
import {
  toC4View,
  toVolatilityView,
  AXIS1_LABEL,
  AXIS2_LABEL,
  type VolatilityPoint,
} from '../contracts/adapters';
import type { ArtifactModelEnvelope } from '../contracts/types';
import type { Axis } from '../contracts/models';
import { useProject } from '../hooks/useProject';
import { useComments, volatilityAnchor } from './comments/CommentContext';
import { useTokens } from '../utilities/theme/ThemeContext';
import type { Tokens } from '../utilities/theme/themes';

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
  const points = toVolatilityView(envelope).points;
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

  if (points.length === 0) {
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
    <Box sx={{ display: 'flex', gap: 2, flexDirection: { xs: 'column', md: 'row' } }}>
      <Box
        sx={{ flexGrow: 1, display: 'flex', gap: 2, flexDirection: { xs: 'column', sm: 'row' } }}
      >
        <Lane
          color={t.accent2}
          items={axis1}
          sel={sel}
          subtitle="Varies for one customer over time."
          t={t}
          title={AXIS1_LABEL}
          onSelect={setSel}
        />
        <Lane
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
            <Typography sx={{ color: t.muted, fontSize: 13.5, lineHeight: 1.6 }}>
              Two axes of change: the left lane evolves for one customer over time; the right lane
              differs across customers at one moment. Select a volatility to inspect
              {enabled ? ' or comment' : ''}.
            </Typography>
          )}
        </Paper>
      </Box>
    </Box>
  );
}

function Lane({
  t,
  color,
  title,
  subtitle,
  items,
  sel,
  onSelect,
}: {
  t: Tokens;
  color: string;
  title: string;
  subtitle: string;
  items: IndexedPoint[];
  sel: number | null;
  onSelect: (updater: (s: number | null) => number | null) => void;
}): ReactNode {
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
      <Box sx={{ p: 1, display: 'flex', flexDirection: 'column', gap: 0.75, overflowY: 'auto' }}>
        {items.map(({ v, i }) => (
          <VolChip
            active={sel === i}
            color={color}
            key={`${v.name}-${String(i)}`}
            t={t}
            v={v}
            onSelect={() => {
              onSelect(() => i);
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
  onSelect,
}: {
  t: Tokens;
  v: VolatilityPoint;
  color: string;
  active: boolean;
  /** Select this chip — the single source of truth for the inspector. Fired by
   *  click, Enter/Space, AND keyboard focus, so the side rail always reflects the
   *  focused/clicked chip. The redundant per-chip hover tooltip is intentionally
   *  gone: it stacked, overlapped siblings, and was decoupled from the inspector. */
  onSelect: () => void;
}): ReactNode {
  return (
    <Box
      aria-label={v.name}
      aria-pressed={active}
      role="button"
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
      tabIndex={0}
      onClick={onSelect}
      onFocus={onSelect}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onSelect();
        }
      }}
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
  onComment,
}: {
  v: VolatilityPoint;
  t: Tokens;
  /** The System component that encapsulates this volatility, when the join resolves. */
  encapsulatedBy?: string | undefined;
  /** Present only when commenting is active; omit on read-only surfaces. */
  onComment?: (() => void) | undefined;
}): ReactNode {
  const axisText =
    v.axis === 'sameCustomerOverTime' ? 'Axis 1 — over time' : 'Axis 2 — across customers';
  return (
    <Box>
      {/* Header row: name/axis on the left, the comment affordance PINNED top-right so
          it never sinks below a long rationale (P2-8). */}
      <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1 }}>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography
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
            {axisText}
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
    </Box>
  );
}
