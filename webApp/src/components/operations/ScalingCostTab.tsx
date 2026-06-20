/**
 * Scaling & Cost — TWO readings of the same operated app:
 *   CUSTOMER cost transparency (the highest-value piece): run-rate now (from the
 *   live view) + projected monthly + the scale what-if curve (the read-only
 *   cost-projection endpoint). [operationEstimationEngine.queryCostProjection]
 *   OPERATOR autoscaler view: mode (Auto/Manual) + decision history (with why)
 *   from the live view, plus the Update-autoscaler-policy publish action. The
 *   autoscaler is a THIRD actor; resume from idle-pause is traffic-driven, not a
 *   button. [autoscalerEngine]
 */
import type { ReactNode } from 'react';
import { useState } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Tooltip from '@mui/material/Tooltip';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import TuneIcon from '@mui/icons-material/Tune';
import type { Tokens } from '../../theme/themes';
import { useTokens } from '../../theme/ThemeContext';
import type { AutoscalerDecision, OperationsView, WhatIfPoint } from '../../api/operations';
import { formatMoney, formatEventTime } from '../../api/operationsAdapters';
import { useCostProjection } from '../../hooks/useCostProjection';
import { useOperationAction } from '../../hooks/useOperationsMutations';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { AwaitingPanel } from './AwaitingPanel';

export function ScalingCostTab({
  operatedAppId,
  view,
}: {
  operatedAppId: string;
  view: OperationsView | undefined;
}): ReactNode {
  const t = useTokens();
  const costQuery = useCostProjection(operatedAppId, undefined, view !== undefined);
  const cost = costQuery.data;
  const action = useOperationAction(operatedAppId);
  const [policyResult, setPolicyResult] = useState<string | null>(null);

  if (view === undefined) {
    return (
      <AwaitingPanel
        detail="This operated app has no observed cost or autoscaler data. Once it is deployed, run-rate, projected monthly cost, and the autoscaler decision history populate from the live view + queryCostProjection."
        title="No cost or scaling data yet"
      />
    );
  }

  const updatePolicy = (): void => {
    action.mutate('autoscaler-policy', {
      onSuccess: (r) => { setPolicyResult(r.published ? 'policy republished' : 'accepted'); },
    });
  };

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Operations.SCALING_TAB}
      sx={{ display: 'flex', flexDirection: 'column', gap: 3, maxWidth: 1080 }}
    >
      {/* ===== CUSTOMER COST TRANSPARENCY ===== */}
      <Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1, flexWrap: 'wrap' }}>
          <Chip label="CUSTOMER VIEW" size="small" sx={{ height: 19, fontSize: 8.5, bgcolor: t.accent, color: t.accentText, fontWeight: 700 }} />
          <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 18, color: t.ink }}>What you’re billed for hosting</Typography>
          <Tooltip title="The customer always sees run-rate + projected monthly + what-if. Pure read; queryCostProjection mutates no state. The aggregated hosting line is charged on the Billing surface.">
            <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>queryCostProjection · no state mutation</Typography>
          </Tooltip>
        </Box>

        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, 1fr)' }, gap: 1.5, mb: 1.5 }}>
          <BigMetric emphasize hint="extrapolated at current observed load" label="Run-rate (now)" t={t} unit="/ day" value={formatMoney(view.currentRunRate)} />
          <BigMetric
            emphasize
            hint={costQuery.isLoading ? 'loading projection…' : 'at current trajectory'}
            label="Projected this month"
            t={t}
            value={cost !== undefined ? formatMoney(cost.projectedMonthlyCost) : '—'}
          />
        </Box>

        {/* the scale what-if curve */}
        <Paper sx={{ p: 2 }}>
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink, mb: 0.25 }}>SCALE WHAT-IF CURVE</Typography>
          <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, mb: 1.5 }}>
            Projected monthly cost as the autoscaler settles at each replica level — “if I scale up, what do I pay?”
          </Typography>
          {cost !== undefined && cost.scaleWhatIfCurve.length > 0 ? (
            <WhatIfCurve points={cost.scaleWhatIfCurve} t={t} />
          ) : (
            <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
              {costQuery.isLoading ? 'loading what-if curve…' : 'No what-if curve available yet.'}
            </Typography>
          )}
        </Paper>
      </Box>

      {/* ===== OPERATOR AUTOSCALER VIEW ===== */}
      <Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1, flexWrap: 'wrap' }}>
          <Chip label="OPERATOR VIEW" size="small" sx={{ height: 19, fontSize: 8.5, color: t.muted }} variant="outlined" />
          <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 18, color: t.ink }}>Autoscaler — the third actor</Typography>
          <Chip
            label={view.autoscaler.mode.toUpperCase()}
            size="small"
            sx={{ height: 19, fontSize: 9, bgcolor: view.autoscaler.mode === 'Manual' ? t.awaitingBg : t.committedBg, color: view.autoscaler.mode === 'Manual' ? t.awaitingFg : t.committedFg }}
          />
          <Box sx={{ flexGrow: 1 }} />
          <Button
            color="inherit"
            data-testid={UI_IDENTIFIERS.Operations.AUTOSCALER_POLICY_BUTTON}
            disabled={action.isPending}
            size="small"
            startIcon={<TuneIcon sx={{ fontSize: 14 }} />}
            sx={{ py: 0.2, fontSize: 10.5, color: t.ink, borderColor: t.line }}
            variant="outlined"
            onClick={updatePolicy}
          >
            Update autoscaler policy
          </Button>
          {policyResult !== null && (
            <Chip label={policyResult} size="small" sx={{ height: 18, fontSize: 9, fontFamily: t.mono, color: t.committedFg }} variant="outlined" />
          )}
        </Box>

        <Paper sx={{ p: 0, overflow: 'hidden' }}>
          <Box sx={{ px: 2, py: 1.1, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${t.line}`, display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>DECISION HISTORY</Typography>
            <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>· proposeDesiredState (with why)</Typography>
          </Box>
          {view.autoscaler.decisions.length === 0 ? (
            <Box sx={{ px: 2, py: 2, textAlign: 'center' }}>
              <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>No autoscaler decisions observed yet.</Typography>
            </Box>
          ) : (
            view.autoscaler.decisions.map((d, i) => (
              <DecisionRow d={d} key={`${d.at}-${String(i)}`} last={i === view.autoscaler.decisions.length - 1} t={t} />
            ))
          )}
          <Box sx={{ px: 2, py: 1, bgcolor: t.paperAlt, borderTop: `1.5px solid ${t.line}` }}>
            <Typography sx={{ fontFamily: t.body, fontSize: 10.5, color: t.muted, lineHeight: 1.45 }}>
              Resume from idle-pause is <b>traffic-driven</b> (Envoy + scale-from-zero), not an operator button. The engine DECIDES; the Manager
              EXECUTES by publishing desired state; the GitOps runtime converges.
            </Typography>
          </Box>
        </Paper>
      </Box>
    </Box>
  );
}

function BigMetric({ t, label, value, unit, hint, emphasize }: { t: Tokens; label: string; value: string; unit?: string; hint: string; emphasize?: boolean }): ReactNode {
  return (
    <Paper sx={{ p: 2, borderLeft: emphasize === true ? `5px solid ${t.accent}` : undefined, bgcolor: emphasize === true ? t.paperAlt : undefined }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em', color: t.muted, textTransform: 'uppercase' }}>{label}</Typography>
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.75 }}>
        <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 30, lineHeight: 1.05, color: t.ink }}>{value}</Typography>
        {unit !== undefined && <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>{unit}</Typography>}
      </Box>
      <Typography sx={{ fontFamily: t.body, fontSize: 11, color: t.muted, mt: 0.25, lineHeight: 1.4 }}>{hint}</Typography>
    </Paper>
  );
}

/** The scale what-if curve — SVG over the (replicas → projected monthly cost) points. */
function WhatIfCurve({ t, points }: { t: Tokens; points: WhatIfPoint[] }): ReactNode {
  const [hover, setHover] = useState<number | null>(null);
  const W = 360;
  const H = 180;
  const padL = 48;
  const padB = 30;
  const padT = 14;
  const padR = 14;
  const plotW = W - padL - padR;
  const plotH = H - padT - padB;
  const xs = points.map((p) => p.replicas);
  const ys = points.map((p) => p.projectedMonthlyCost.minorUnits);
  const xMax = Math.max(...xs, 1);
  const yMax = Math.max(...ys, 1) * 1.1;
  const sx = (x: number): number => padL + (x / xMax) * plotW;
  const sy = (y: number): number => padT + (1 - y / yMax) * plotH;
  const line = points
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${sx(p.replicas).toFixed(1)},${sy(p.projectedMonthlyCost.minorUnits).toFixed(1)}`)
    .join(' ');
  const active = hover !== null ? points[hover] : points[points.length - 1];

  return (
    <Box>
      <Box component="svg" sx={{ width: '100%', height: H, display: 'block' }} viewBox={`0 0 ${String(W)} ${String(H)}`}>
        <line stroke={t.line} strokeWidth={1.5} x1={padL} x2={padL} y1={padT} y2={H - padB} />
        <line stroke={t.line} strokeWidth={1.5} x1={padL} x2={W - padR} y1={H - padB} y2={H - padB} />
        <text fill={t.muted} fontFamily={t.mono} fontSize={9} textAnchor="middle" x={padL + plotW / 2} y={H - 4}>
          replicas →
        </text>
        <path d={line} fill="none" stroke={t.accent} strokeLinejoin="round" strokeWidth={2.25} />
        {points.map((p, i) => {
          const cx = sx(p.replicas);
          const cy = sy(p.projectedMonthlyCost.minorUnits);
          const isActive = hover === i || (hover === null && i === points.length - 1);
          return (
            <g key={i} style={{ cursor: 'pointer' }} onMouseEnter={() => { setHover(i); }} onMouseLeave={() => { setHover(null); }}>
              <circle cx={cx} cy={cy} fill={isActive ? t.accent : t.paper} r={isActive ? 6 : 4} stroke={t.accent} strokeWidth={isActive ? 2.5 : 1.5} />
              <text fill={t.muted} fontFamily={t.mono} fontSize={8.5} textAnchor="middle" x={cx} y={H - padB + 12}>
                {p.replicas}
              </text>
            </g>
          );
        })}
      </Box>
      {active !== undefined && (
        <Box sx={{ mt: 1, p: 1, bgcolor: t.committedBg, border: `1px solid ${t.line}`, borderRadius: t.radius / 8 + 0.5, display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.committedFg }}>
            at <b>{active.replicas}</b> replicas:
          </Typography>
          <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 16, color: t.ink }}>{formatMoney(active.projectedMonthlyCost)}/mo</Typography>
        </Box>
      )}
    </Box>
  );
}

function DecisionRow({ t, d, last }: { t: Tokens; d: AutoscalerDecision; last: boolean }): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.25, px: 2, py: 1.1, borderBottom: last ? 'none' : `1px solid ${t.line}`, borderLeft: `4px solid ${d.published ? t.accent : 'transparent'}` }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, minWidth: 96, pt: 0.15 }}>{formatEventTime(d.at)}</Typography>
      <Box sx={{ minWidth: 96 }}>
        <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, px: 0.75, py: 0.15, borderRadius: 99, border: `1px solid ${t.accent}`, bgcolor: t.paperAlt }}>
          <Box sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: t.accent }} />
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 9.5, color: t.ink }}>{d.action}</Typography>
        </Box>
      </Box>
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.4, flexGrow: 1 }}>
        {d.reason}
        {!d.published && <Box component="span" sx={{ fontFamily: t.mono, fontSize: 9, color: t.muted }}> · no republish</Box>}
      </Typography>
    </Box>
  );
}
