/**
 * Hand-rolled SVG charts, token-driven (the VolatilityMap approach) so they
 * recolor across all five themes. No chart lib — keeps the look on-brand and light.
 * Ported from the frozen UX mock (ux-mock/src/components/project/charts.tsx), bound
 * to real typed data by the callers.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import type { Tokens } from '../../utilities/theme/themes';

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

/** A concise numeric format for the aria-label series summary. */
function fmtNum(n: number): string {
  if (Number.isInteger(n)) return n.toLocaleString();
  return Math.abs(n) < 10 ? n.toFixed(2) : Math.round(n).toLocaleString();
}

/**
 * A computed accessible summary of the scatter series so screen-reader users get
 * the same trade-off signal the sighted chart conveys, e.g.
 * "composite risk versus duration (days): Normal 737.5 / 0.71; …".
 */
function describeSeries(
  points: ScatterPoint[],
  xLabel: string,
  yLabel: string,
  bands: readonly { from: number; to: number; kind: 'in' | 'out'; label?: string }[]
): string {
  if (points.length === 0) return `${yLabel} versus ${xLabel}: no data.`;
  const series = points
    .map(
      (p) =>
        `${p.label} ${fmtNum(p.x)} ${xLabel} / ${fmtNum(p.y)} ${yLabel}${p.out === true ? ' (excluded)' : ''}`
    )
    .join('; ');
  const zones =
    bands.length > 0
      ? ` Shaded zones: ${bands
          .map(
            (b) =>
              `${b.label ?? (b.kind === 'in' ? 'inclusion zone' : 'exclusion zone')} ${fmtNum(Math.min(b.from, b.to))}–${fmtNum(Math.max(b.from, b.to))}`
          )
          .join('; ')}.`
      : '';
  return `Scatter chart, ${yLabel} versus ${xLabel}. ${series}.${zones}`;
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
  formatX,
  formatY,
}: {
  t: Tokens;
  points: ScatterPoint[];
  xLabel: string;
  yLabel: string;
  xMin: number;
  xMax: number;
  yMin: number;
  yMax: number;
  /**
   * y-value bands in y-units. `label` names the zone on the chart (it defaults to
   * a generic in/out word); the zone is ALSO named in the accessible summary, so
   * the shading is never the only carrier.
   */
  bands?: { from: number; to: number; kind: 'in' | 'out'; label?: string }[];
  height?: number;
  /** Tick-label formatters. Without them the axes carry no values at all. */
  formatX?: (x: number) => string;
  formatY?: (y: number) => string;
}): ReactNode {
  const W = 560;
  const H = height;
  const padL = formatY !== undefined ? 76 : 52;
  const padB = 40;
  const padT = 18;
  const padR = 18;
  const plotW = W - padL - padR;
  const plotH = H - padT - padB;
  const spanX = xMax - xMin || 1;
  const spanY = yMax - yMin || 1;
  const px = (x: number): number => padL + ((x - xMin) / spanX) * plotW;
  const py = (y: number): number => padT + (1 - (y - yMin) / spanY) * plotH;

  // Three ticks per axis (both ends and the middle of the plotted domain) — enough
  // to read magnitudes off the chart without a gridline forest.
  const ticks = (lo: number, hi: number): number[] => [lo, (lo + hi) / 2, hi];

  const sorted = [...points].sort((a, b) => a.x - b.x);
  const line = sorted
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${px(p.x).toFixed(1)},${py(p.y).toFixed(1)}`)
    .join(' ');

  return (
    <Box
      aria-label={describeSeries(points, xLabel, yLabel, bands ?? [])}
      component="svg"
      role="img"
      sx={{ width: '100%', height, display: 'block' }}
      viewBox={`0 0 ${String(W)} ${String(H)}`}
    >
      {bands?.map((b, i) => {
        const yTop = py(Math.max(b.from, b.to));
        const yBot = py(Math.min(b.from, b.to));
        const fill = b.kind === 'in' ? t.committedDot : t.awaitingFg;
        return (
          <g key={i}>
            <rect
              fill={fill}
              height={yBot - yTop}
              opacity={b.kind === 'in' ? 0.1 : 0.13}
              width={plotW}
              x={padL}
              y={yTop}
            />
            <text
              fill={fill}
              fontFamily={t.mono}
              fontSize={9}
              fontWeight={700}
              textAnchor="end"
              x={padL + plotW - 6}
              y={yTop + 12}
            >
              {b.label ?? (b.kind === 'in' ? 'INCLUSION ZONE' : 'EXCLUSION')}
            </text>
          </g>
        );
      })}

      {/* axes */}
      <line stroke={t.line} strokeWidth={1.5} x1={padL} x2={padL} y1={padT} y2={H - padB} />
      <line stroke={t.line} strokeWidth={1.5} x1={padL} x2={W - padR} y1={H - padB} y2={H - padB} />

      {/* tick values */}
      {formatX !== undefined
        ? ticks(xMin, xMax).map((x) => (
            <g key={`x${String(x)}`}>
              <line
                stroke={t.line}
                strokeWidth={1}
                x1={px(x)}
                x2={px(x)}
                y1={H - padB}
                y2={H - padB + 4}
              />
              <text
                fill={t.muted}
                fontFamily={t.mono}
                fontSize={9.5}
                textAnchor="middle"
                x={px(x)}
                y={H - padB + 15}
              >
                {formatX(x)}
              </text>
            </g>
          ))
        : null}
      {formatY !== undefined
        ? ticks(yMin, yMax).map((y) => (
            <g key={`y${String(y)}`}>
              <line stroke={t.line} strokeWidth={1} x1={padL - 4} x2={padL} y1={py(y)} y2={py(y)} />
              <text
                dominantBaseline="middle"
                fill={t.muted}
                fontFamily={t.mono}
                fontSize={9.5}
                textAnchor="end"
                x={padL - 7}
                y={py(y)}
              >
                {formatY(y)}
              </text>
            </g>
          ))
        : null}

      {/* axis labels */}
      <text
        fill={t.muted}
        fontFamily={t.mono}
        fontSize={10}
        textAnchor="middle"
        x={padL + plotW / 2}
        y={H - 6}
      >
        {xLabel} →
      </text>
      <text
        fill={t.muted}
        fontFamily={t.mono}
        fontSize={10}
        textAnchor="middle"
        transform={`rotate(-90 14 ${String(padT + plotH / 2)})`}
        x={14}
        y={padT + plotH / 2}
      >
        {yLabel} →
      </text>

      {/* connecting curve */}
      <path
        d={line}
        fill="none"
        opacity={0.8}
        stroke={t.muted}
        strokeDasharray="5 4"
        strokeWidth={1.5}
      />

      {/* points */}
      {points.map((p, i) => {
        const cx = px(p.x);
        const cy = py(p.y);
        const r = p.emphasized === true ? 8 : 6;
        return (
          <g key={i}>
            <circle
              cx={cx}
              cy={cy}
              fill={p.color}
              r={r}
              stroke={p.emphasized === true ? t.accent : t.line}
              strokeWidth={p.emphasized === true ? 2.5 : 1.5}
            />
            {p.out === true && (
              <text
                fill={t.accentText}
                fontFamily={t.mono}
                fontSize={9}
                fontWeight={700}
                textAnchor="middle"
                x={cx}
                y={cy + 3.5}
              >
                ✕
              </text>
            )}
            <text
              fill={p.emphasized === true ? t.accentInk : t.ink}
              fontFamily={t.mono}
              fontSize={9.5}
              fontWeight={700}
              textAnchor="middle"
              x={cx}
              y={cy - r - 4}
            >
              {p.label}
            </text>
          </g>
        );
      })}
    </Box>
  );
}
