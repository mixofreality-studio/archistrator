/**
 * The volatilities artifact as a two-axis scatter — the visual decomposition.
 * Bound to adapters.toVolatilityView (VolatilityPoint[]; x,y in 0..1, axis is the
 * typed Axis name). Points are clickable: selecting one opens an inspect card and
 * arms a comment anchor (`$.items[n]`) for the chat rail. Recolored from tokens.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Tooltip from '@mui/material/Tooltip';
import Button from '@mui/material/Button';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import {
  toVolatilityView,
  AXIS1_LABEL,
  AXIS2_LABEL,
  type VolatilityPoint,
} from '../api/adapters';
import type { ArtifactModelEnvelope } from '../api/types';
import type { Axis } from '../api/models';
import { useComments, volatilityAnchor } from './comments/CommentContext';
import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';


function axisColor(t: Tokens, a: Axis): string {
  return a === 'sameCustomerOverTime' ? t.accent2 : t.committedDot;
}

export function VolatilityMap({ envelope }: { envelope: ArtifactModelEnvelope | undefined }): ReactNode {
  const t = useTokens();
  const { setAnchor } = useComments();
  const [sel, setSel] = useState<number | null>(null);
  const points = toVolatilityView(envelope).points;
  const selected = sel !== null ? points[sel] : undefined;

  if (points.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No volatilities drafted yet.
      </Box>
    );
  }

  return (
    <Box sx={{ display: 'flex', gap: 2, flexDirection: { xs: 'column', md: 'row' } }}>
      <Paper sx={{ flexGrow: 1, p: 0, position: 'relative', overflow: 'hidden' }}>
        <Box sx={{ position: 'relative', height: 560, pl: 7, pb: 6, pt: 3, pr: 3 }}>
          {/* axes */}
          <Box sx={{ position: 'absolute', left: 56, bottom: 48, top: 24, width: 2, bgcolor: t.line }} />
          <Box sx={{ position: 'absolute', left: 56, right: 24, bottom: 48, height: 2, bgcolor: t.line }} />
          {/* axis labels */}
          <Typography sx={{ position: 'absolute', left: 56, right: 24, bottom: 16, textAlign: 'center', fontFamily: t.mono, fontSize: 11, color: t.muted }}>
            {AXIS2_LABEL} →
          </Typography>
          <Typography sx={{ position: 'absolute', left: 6, top: '50%', transformOrigin: 'left', transform: 'rotate(-90deg) translateX(-50%)', fontFamily: t.mono, fontSize: 11, color: t.muted, whiteSpace: 'nowrap' }}>
            {AXIS1_LABEL} →
          </Typography>

          {/* plotted volatilities */}
          <Box sx={{ position: 'absolute', left: 56, right: 24, top: 24, bottom: 48 }}>
            {points.map((v, i) => {
              const c = axisColor(t, v.axis);
              const active = sel === i;
              return (
                <Tooltip key={`${v.name}-${String(i)}`} title={v.rationale}>
                  <Box
                    sx={{
                      position: 'absolute',
                      left: `${String(v.x * 100)}%`,
                      bottom: `${String(v.y * 100)}%`,
                      transform: 'translate(-50%, 50%)',
                      cursor: 'pointer',
                      px: 1,
                      py: 0.5,
                      display: 'flex',
                      alignItems: 'center',
                      gap: 0.6,
                      bgcolor: active ? c : t.paperAlt,
                      color: active ? t.accentText : t.ink,
                      border: `1.5px solid ${active ? t.accent : t.line}`,
                      borderLeft: `4px solid ${c}`,
                      borderRadius: t.radius / 8 + 0.5,
                      boxShadow: active ? `0 0 0 2px ${t.accent}` : t.hardShadow ? `2px 2px 0 ${t.shadowColor}` : 'none',
                      whiteSpace: 'nowrap',
                      zIndex: active ? 2 : 1,
                      '&:hover': { zIndex: 3 },
                    }}
                    onClick={() => {
                      setSel((s) => (s === i ? null : i));
                    }}
                  >
                    <Box sx={{ width: 7, height: 7, borderRadius: '50%', bgcolor: c, flexShrink: 0 }} />
                    <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11 }}>{v.name}</Typography>
                  </Box>
                </Tooltip>
              );
            })}
          </Box>
        </Box>
      </Paper>

      {/* side rail: legend + selection */}
      <Box sx={{ width: { xs: '100%', md: 280 }, flexShrink: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
        <Paper sx={{ p: 2 }}>
          <Typography sx={{ color: t.muted, mb: 1 }} variant="subtitle2">
            AXES · {points.length} VOLATILITIES
          </Typography>
          <Legend color={t.accent2} label="Axis 1 — over time" t={t} />
          <Legend color={t.committedDot} label="Axis 2 — across customers" t={t} />
        </Paper>
        <Paper sx={{ p: 2, flexGrow: 1 }}>
          {selected !== undefined && sel !== null ? (
            <SelectionCard
              t={t}
              v={selected}
              onComment={() => {
                setAnchor({
                  kind: 'node',
                  label: selected.name,
                  source: 'Volatilities · axis map',
                  jsonPath: volatilityAnchor(sel),
                });
              }}
            />
          ) : (
            <Typography sx={{ color: t.muted, fontSize: 13.5, lineHeight: 1.6 }}>
              Each volatility is placed by how it changes: <b>up</b> = evolves for one customer over time;{' '}
              <b>right</b> = differs across customers at one moment. Click a chip to inspect or comment.
            </Typography>
          )}
        </Paper>
      </Box>
    </Box>
  );
}

function Legend({ t, color, label }: { t: Tokens; color: string; label: string }): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.75 }}>
      <Box sx={{ width: 12, height: 12, bgcolor: color, border: `1.5px solid ${t.line}` }} />
      <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.ink }}>{label}</Typography>
    </Box>
  );
}

function SelectionCard({
  v,
  t,
  onComment,
}: {
  v: VolatilityPoint;
  t: Tokens;
  onComment: () => void;
}): ReactNode {
  const axisText =
    v.axis === 'sameCustomerOverTime' ? 'Axis 1 — over time' : 'Axis 2 — across customers';
  return (
    <Box>
      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 15, color: t.ink, wordBreak: 'break-word' }}>
        {v.name}
      </Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: axisColor(t, v.axis), mb: 1 }}>
        {axisText}
      </Typography>
      <Typography sx={{ color: t.muted, fontSize: 13.5, lineHeight: 1.6, mb: 2 }}>{v.rationale}</Typography>
      <Button
        size="small"
        startIcon={<ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />}
        sx={{ color: t.ink, borderColor: t.line }}
        variant="outlined"
        onClick={onComment}
      >
        Comment on this
      </Button>
    </Box>
  );
}
