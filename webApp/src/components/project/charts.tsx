/**
 * Hand-rolled SVG charts, token-driven (the VolatilityMap approach) so they
 * recolor across all five themes. No chart lib — keeps the look on-brand and light.
 * Ported from the frozen UX mock (ux-mock/src/components/project/charts.tsx), bound
 * to real typed data by the callers.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import type { Tokens } from '../../theme/themes';

// ---------------------------------------------------------------------------
// Scatter curve with exclusion bands — the SDP time-risk / time-cost charts.
// ---------------------------------------------------------------------------
export interface ScatterPoint {
  x: number;
  y: number;
  label: string;
  color: string;
  emphasized?: boolean;
  out?: boolean;
}

export function BandedScatter({
  t,
  points,
  xLabel,
  yLabel,
  xMin,
  xMax,
  yMin,
  yMax,
  bands,
  height = 280,
}: {
  t: Tokens;
  points: ScatterPoint[];
  xLabel: string;
  yLabel: string;
  xMin: number;
  xMax: number;
  yMin: number;
  yMax: number;
  /** y-value bands in y-units. */
  bands?: { from: number; to: number; kind: 'in' | 'out' }[];
  height?: number;
}): ReactNode {
  const W = 560;
  const H = height;
  const padL = 52;
  const padB = 40;
  const padT = 18;
  const padR = 18;
  const plotW = W - padL - padR;
  const plotH = H - padT - padB;
  const spanX = xMax - xMin || 1;
  const spanY = yMax - yMin || 1;
  const px = (x: number): number => padL + ((x - xMin) / spanX) * plotW;
  const py = (y: number): number => padT + (1 - (y - yMin) / spanY) * plotH;

  const sorted = [...points].sort((a, b) => a.x - b.x);
  const line = sorted
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${px(p.x).toFixed(1)},${py(p.y).toFixed(1)}`)
    .join(' ');

  return (
    <Box component="svg" sx={{ width: '100%', height, display: 'block' }} viewBox={`0 0 ${String(W)} ${String(H)}`}>
      {bands?.map((b, i) => {
        const yTop = py(Math.max(b.from, b.to));
        const yBot = py(Math.min(b.from, b.to));
        const fill = b.kind === 'in' ? t.committedDot : t.awaitingFg;
        return (
          <g key={i}>
            <rect fill={fill} height={yBot - yTop} opacity={b.kind === 'in' ? 0.1 : 0.13} width={plotW} x={padL} y={yTop} />
            <text fill={fill} fontFamily={t.mono} fontSize={9} fontWeight={700} opacity={0.9} textAnchor="end" x={padL + plotW - 6} y={yTop + 12}>
              {b.kind === 'in' ? 'INCLUSION ZONE' : 'EXCLUSION'}
            </text>
          </g>
        );
      })}

      {/* axes */}
      <line stroke={t.line} strokeWidth={1.5} x1={padL} x2={padL} y1={padT} y2={H - padB} />
      <line stroke={t.line} strokeWidth={1.5} x1={padL} x2={W - padR} y1={H - padB} y2={H - padB} />

      {/* axis labels */}
      <text fill={t.muted} fontFamily={t.mono} fontSize={10} textAnchor="middle" x={padL + plotW / 2} y={H - 6}>
        {xLabel} →
      </text>
      <text fill={t.muted} fontFamily={t.mono} fontSize={10} textAnchor="middle" transform={`rotate(-90 14 ${String(padT + plotH / 2)})`} x={14} y={padT + plotH / 2}>
        {yLabel} →
      </text>

      {/* connecting curve */}
      <path d={line} fill="none" opacity={0.8} stroke={t.muted} strokeDasharray="5 4" strokeWidth={1.5} />

      {/* points */}
      {points.map((p, i) => {
        const cx = px(p.x);
        const cy = py(p.y);
        const r = p.emphasized === true ? 8 : 6;
        return (
          <g key={i}>
            <circle cx={cx} cy={cy} fill={p.color} r={r} stroke={p.emphasized === true ? t.accent : t.line} strokeWidth={p.emphasized === true ? 2.5 : 1.5} />
            {p.out === true && (
              <text fill={t.accentText} fontFamily={t.mono} fontSize={9} fontWeight={700} textAnchor="middle" x={cx} y={cy + 3.5}>
                ✕
              </text>
            )}
            <text fill={p.emphasized === true ? t.accent : t.ink} fontFamily={t.mono} fontSize={9.5} fontWeight={700} textAnchor="middle" x={cx} y={cy - r - 4}>
              {p.label}
            </text>
          </g>
        );
      })}
    </Box>
  );
}
