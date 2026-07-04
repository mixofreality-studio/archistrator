/**
 * The risk-model artifact — the per-option criticality + activity risk
 * decomposition into a composite risk, as a comparison table, PLUS the
 * time-cost / time-risk curves (each row doubles as a curve point) with the
 * exclusion-zone bands shaded on the time-risk chart (composite risk above
 * tooRiskyThreshold, or below overSafeThreshold, excludes the option). Bound
 * to the typed RiskModel candidate model via api/projectAdapters.toRiskModelView.
 * Reuses the BandedScatter chart already used by SdpReviewView's time-cost/
 * time-risk curves.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import type { ProjectArtifactKind, ProjectArtifactModelEnvelope } from '../../api/types';
import { SOLUTION_LABELS } from '../../api/types';
import {
  toRiskModelView,
  formatMoney,
  formatDurationDays,
  solutionAccentColor,
} from '../../api/projectAdapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { ComputedBadge } from './computed';
import { BandedScatter, type ScatterPoint } from './charts';
import { useComments } from '../comments/CommentContext';
import { riskModelRowAnchor } from '../comments/CommentContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

/** The per-row "Comment on this item" affordance for the ARIA-table rows (the
 * CommentableList primitive is a listbox, so table rows carry their own button). */
function RowCommentButton({
  t,
  label,
  testKey,
  onArm,
}: {
  t: Tokens;
  label: string;
  testKey: string;
  onArm: () => void;
}): ReactNode {
  return (
    <Tooltip title="Comment on this item">
      <IconButton
        aria-label={`Comment on ${label}`}
        className="row-comment-action"
        data-testid={UI_IDENTIFIERS.Comments.listItemComment(testKey)}
        size="small"
        sx={{
          color: t.accentText,
          bgcolor: t.accent,
          border: `1.5px solid ${t.line}`,
          borderRadius: 1,
          '&:hover': { bgcolor: t.accent2 },
        }}
        onClick={onArm}
      >
        <ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />
      </IconButton>
    </Tooltip>
  );
}

/** Shared sx that reveals a row's comment button on row hover / keyboard focus. */
const ROW_REVEAL_SX = {
  display: 'contents',
  '& .row-comment-action': { opacity: 0, transition: 'opacity 120ms' },
  '&:hover .row-comment-action, &:focus-within .row-comment-action': { opacity: 1 },
} as const;

function bounds(values: number[], pad: number): { min: number; max: number } {
  if (values.length === 0) return { min: 0, max: 1 };
  const lo = Math.min(...values);
  const hi = Math.max(...values);
  const span = hi - lo || Math.abs(hi) || 1;
  return { min: lo - span * pad, max: hi + span * pad };
}

export function RiskModelView({
  envelope,
}: {
  envelope: ProjectArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const { setAnchor } = useComments();
  const view = toRiskModelView(envelope);
  const rows = view.rows;

  const armRow = (kind: ProjectArtifactKind): void => {
    const label = SOLUTION_LABELS[kind] ?? kind;
    setAnchor({
      kind: 'node',
      label,
      source: `Risk Model · ${label}`,
      jsonPath: riskModelRowAnchor(kind),
    });
  };

  if (rows.length === 0) {
    return (
      <Typography sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No risk model drafted yet.
      </Typography>
    );
  }

  const shortLabel = (kind: (typeof rows)[number]['solutionKind']): string =>
    (SOLUTION_LABELS[kind] ?? kind).split('-')[0] ?? kind;

  const costPts: ScatterPoint[] = rows.map((r) => ({
    x: r.durationDays,
    y: r.totalCost.minorUnits / 100,
    label: shortLabel(r.solutionKind),
    color: solutionAccentColor(t, r.solutionKind),
    emphasized: r.included,
    out: !r.included,
  }));
  const riskPts: ScatterPoint[] = rows.map((r) => ({
    x: r.durationDays,
    y: r.composite,
    label: shortLabel(r.solutionKind),
    color: solutionAccentColor(t, r.solutionKind),
    emphasized: r.included,
    out: !r.included,
  }));

  const durBounds = bounds(
    rows.map((r) => r.durationDays),
    0.12
  );
  const costBounds = bounds(
    rows.map((r) => r.totalCost.minorUnits / 100),
    0.15
  );
  const riskLo = Math.min(0, view.overSafeThreshold, ...rows.map((r) => r.composite));
  const riskHi = Math.max(1, view.tooRiskyThreshold, ...rows.map((r) => r.composite));
  const riskBounds = bounds([riskLo, riskHi], 0.08);

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 1040 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          Criticality risk + activity risk → composite, per option. All
        </Typography>
        <ComputedBadge t={t} />
      </Box>

      <Paper sx={{ p: 0, overflow: 'hidden' }}>
        <Box
          aria-label="Risk model options"
          role="table"
          sx={{ display: 'grid', gridTemplateColumns: '1.3fr 0.9fr 0.9fr 1fr 1fr 1fr 1.6fr' }}
        >
          <Box role="row" sx={{ display: 'contents' }}>
            {['OPTION', 'DURATION', 'COST', 'CRITICALITY', 'ACTIVITY', 'COMPOSITE', 'STATUS'].map(
              (h) => (
                <Box
                  key={h}
                  role="columnheader"
                  sx={{
                    px: 1.5,
                    py: 0.9,
                    borderBottom: `1.5px solid ${t.line}`,
                    bgcolor: t.paperAlt,
                  }}
                >
                  <Typography
                    sx={{
                      fontFamily: t.mono,
                      fontSize: 9.5,
                      letterSpacing: '0.06em',
                      color: t.muted,
                    }}
                  >
                    {h}
                  </Typography>
                </Box>
              )
            )}
          </Box>
          {rows.map((r) => (
            <Box key={r.solutionKind} role="row" sx={ROW_REVEAL_SX}>
              <Box
                role="cell"
                sx={{
                  px: 1.5,
                  py: 1,
                  borderBottom: `1px solid ${t.line}`,
                  display: 'flex',
                  alignItems: 'center',
                  gap: 0.6,
                }}
              >
                <Box
                  sx={{
                    width: 9,
                    height: 9,
                    bgcolor: solutionAccentColor(t, r.solutionKind),
                    border: `1.5px solid ${t.line}`,
                  }}
                />
                <Typography
                  sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11.5, color: t.ink }}
                >
                  {SOLUTION_LABELS[r.solutionKind] ?? r.solutionKind}
                </Typography>
              </Box>
              <Cell t={t}>{formatDurationDays(r.durationDays)}</Cell>
              <Cell t={t}>{formatMoney(r.totalCost)}</Cell>
              <Cell t={t}>{r.criticalityRisk.toFixed(2)}</Cell>
              <Cell t={t}>{r.activityRisk.toFixed(2)}</Cell>
              <Cell strong t={t}>
                {r.composite.toFixed(2)}
              </Cell>
              <Box
                role="cell"
                sx={{
                  px: 1.5,
                  py: 1,
                  borderBottom: `1px solid ${t.line}`,
                  display: 'flex',
                  alignItems: 'center',
                  gap: 1,
                }}
              >
                <Typography
                  sx={{
                    fontFamily: t.mono,
                    fontSize: 10.5,
                    fontWeight: r.included ? 500 : 700,
                    color: r.included ? t.muted : t.accent,
                    flexGrow: 1,
                    minWidth: 0,
                  }}
                >
                  {r.included
                    ? 'included'
                    : r.exclusionReason.length > 0
                      ? r.exclusionReason
                      : 'excluded'}
                </Typography>
                <RowCommentButton
                  label={SOLUTION_LABELS[r.solutionKind] ?? r.solutionKind}
                  t={t}
                  testKey={r.solutionKind}
                  onArm={() => {
                    armRow(r.solutionKind);
                  }}
                />
              </Box>
            </Box>
          ))}
        </Box>
      </Paper>

      {/* the two curves — same chart component as the SDP review, plus the
          exclusion-zone shading on the time-risk chart */}
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 2 }}>
        <Paper sx={{ p: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
            <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink }}>
              TIME–COST CURVE
            </Typography>
            <ComputedBadge t={t} />
          </Box>
          <BandedScatter
            height={260}
            points={costPts}
            t={t}
            xLabel="duration (days)"
            xMax={durBounds.max}
            xMin={durBounds.min}
            yLabel="total cost"
            yMax={costBounds.max}
            yMin={costBounds.min}
          />
        </Paper>
        <Paper sx={{ p: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
            <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink }}>
              TIME–RISK CURVE
            </Typography>
            <ComputedBadge t={t} />
          </Box>
          <BandedScatter
            bands={[
              { from: view.tooRiskyThreshold, to: riskBounds.max, kind: 'out' },
              { from: riskBounds.min, to: view.overSafeThreshold, kind: 'out' },
            ]}
            height={260}
            points={riskPts}
            t={t}
            xLabel="duration (days)"
            xMax={durBounds.max}
            xMin={durBounds.min}
            yLabel="composite risk"
            yMax={riskBounds.max}
            yMin={riskBounds.min}
          />
        </Paper>
      </Box>
    </Box>
  );
}

function Cell({
  t,
  children,
  strong,
}: {
  t: Tokens;
  children: ReactNode;
  strong?: boolean;
}): ReactNode {
  return (
    <Box
      role="cell"
      sx={{
        px: 1.5,
        py: 1,
        borderBottom: `1px solid ${t.line}`,
        display: 'flex',
        alignItems: 'center',
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: strong === true ? 13 : 12,
          fontWeight: strong === true ? 700 : 500,
          color: t.ink,
        }}
      >
        {children}
      </Typography>
    </Box>
  );
}
