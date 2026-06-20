/**
 * Hand-rolled SVG EV tracking chart — planned (dashed) vs earned (solid) curves.
 * Ported from the UX mock's EvTrackingChart
 * (ux-mock/src/components/construction/ConstructionTracker.tsx ~lines 223–280),
 * adapted to real EvCurves data. The AC curve and current-week marker are omitted
 * (no fabricated spend, no fabricated "now" position).
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import type { EvCurves } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';

const W = 760;
const H = 280;
const PAD_L = 40;
const PAD_B = 34;
const PAD_T = 14;
const PAD_R = 16;
const PLOT_W = W - PAD_L - PAD_R;
const PLOT_H = H - PAD_T - PAD_B;

type Pt = readonly [number, number];

function toPath(pts: readonly Pt[]): string {
  return pts.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`).join(' ');
}

export function EvTrackingChart({ ev }: { ev: EvCurves }): ReactNode {
  const t = useTokens();

  const weeks = ev.weeks;
  const maxWeek = Math.max(weeks.length > 0 ? Math.max(...weeks) : 0, 1);
  const sx = (w: number): number => PAD_L + (w / maxWeek) * PLOT_W;
  const sy = (v: number): number => PAD_T + (1 - v / 100) * PLOT_H;

  const plannedPts: Pt[] = weeks.map((w, i) => [sx(w), sy(ev.planned[i] ?? 0)]);

  const earnedPts: Pt[] = [];
  for (let i = 0; i < weeks.length; i++) {
    const e = ev.earned[i];
    if (e !== undefined) {
      earnedPts.push([sx(weeks[i] ?? 0), sy(e)]);
    }
  }

  const trendLine = (): ReactNode => {
    if (earnedPts.length < 2) return null;
    const a = earnedPts[earnedPts.length - 2];
    const b = earnedPts[earnedPts.length - 1];
    if (a === undefined || b === undefined) return null;
    const dx = b[0] - a[0];
    if (dx === 0) return null;
    const slope = (b[1] - a[1]) / dx;
    if (slope === 0) return null;
    const y100 = sy(100);
    const x100 = b[0] + (y100 - b[1]) / slope;
    return (
      <line
        opacity={0.7}
        stroke={t.committedDot}
        strokeDasharray="3 3"
        strokeWidth={1.5}
        x1={b[0]}
        x2={x100}
        y1={b[1]}
        y2={y100}
      />
    );
  };

  const viewBox = `0 0 ${W.toString()} ${H.toString()}`;

  return (
    <Box
      component="svg"
      sx={{ display: 'block', height: H, width: '100%' }}
      viewBox={viewBox}
    >
      {/* Grid lines */}
      {([0, 25, 50, 75, 100] as const).map((g) => (
        <g key={g}>
          <line
            opacity={g === 0 ? 1 : 0.5}
            stroke={t.line}
            strokeWidth={g === 0 ? 1.5 : 1}
            x1={PAD_L}
            x2={W - PAD_R}
            y1={sy(g)}
            y2={sy(g)}
          />
          <text
            fill={t.muted}
            fontFamily={t.mono}
            fontSize={9}
            textAnchor="end"
            x={PAD_L - 6}
            y={sy(g) + 3}
          >
            {g}
          </text>
        </g>
      ))}

      {/* Y-axis */}
      <line
        stroke={t.line}
        strokeWidth={1.5}
        x1={PAD_L}
        x2={PAD_L}
        y1={PAD_T}
        y2={H - PAD_B}
      />

      {/* X-axis labels */}
      {weeks.map((w) => (
        <text
          fill={t.muted}
          fontFamily={t.mono}
          fontSize={8.5}
          key={w}
          textAnchor="middle"
          x={sx(w)}
          y={H - PAD_B + 14}
        >
          {w}
        </text>
      ))}
      <text
        fill={t.muted}
        fontFamily={t.mono}
        fontSize={9.5}
        textAnchor="middle"
        x={PAD_L + PLOT_W / 2}
        y={H - 4}
      >
        construction week →
      </text>

      {/* Planned PV — dashed accent2 */}
      <path d={toPath(plannedPts)} fill="none" stroke={t.accent2} strokeDasharray="5 4" strokeWidth={2} />
      {plannedPts.map(([x, y], i) => (
        <circle cx={x} cy={y} fill={t.accent2} key={i} r={2.2} />
      ))}

      {/* Earned EV — solid committedDot */}
      <path
        d={toPath(earnedPts)}
        fill="none"
        stroke={t.committedDot}
        strokeLinejoin="round"
        strokeWidth={2.5}
      />
      {earnedPts.map(([x, y], i) => (
        <circle cx={x} cy={y} fill={t.committedDot} key={i} r={3} stroke={t.bg} strokeWidth={1} />
      ))}

      {/* Earned trend projection */}
      {trendLine()}
    </Box>
  );
}
