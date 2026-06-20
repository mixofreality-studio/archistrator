/**
 * The SDP-review artifact (audience: management) — the assembled options table
 * (the four joined rows), the time-cost and time-risk curves, the architect's
 * recommendation, and the DECISION CAPTURE gate: choose an option to commit
 * (submitSDPDecision commit <optionId>) or reject all options with feedback
 * (submitSDPDecision rejectAll). Ported visual design from ux-mock SdpReview, bound
 * to the real typed SdpReview candidate model via api/projectAdapters.toSdpReviewView.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import TextField from '@mui/material/TextField';
import StarIcon from '@mui/icons-material/Star';
import CheckIcon from '@mui/icons-material/Check';
import ReplayIcon from '@mui/icons-material/Replay';
import type { ProjectArtifactKind, ProjectArtifactModelEnvelope } from '../../api/types';
import { SOLUTION_LABELS } from '../../api/types';
import { toSdpReviewView, formatMoney, solutionAccentColor, type SdpOptionView } from '../../api/projectAdapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { BandedScatter, type ScatterPoint } from './charts';
import { ComputedBadge, AuthoredBadge } from './computed';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

function shortLabel(kind: ProjectArtifactKind): string {
  return (SOLUTION_LABELS[kind] ?? kind).split('-')[0] ?? kind;
}

function bounds(values: number[], pad: number): { min: number; max: number } {
  if (values.length === 0) return { min: 0, max: 1 };
  const lo = Math.min(...values);
  const hi = Math.max(...values);
  const span = hi - lo || Math.abs(hi) || 1;
  return { min: lo - span * pad, max: hi + span * pad };
}

export function SdpReviewView({
  envelope,
  pending,
  onCommit,
  onRejectAll,
}: {
  envelope: ProjectArtifactModelEnvelope | undefined;
  /** A decision mutation is in flight — disable the gate. */
  pending: boolean;
  onCommit: (optionId: string) => void;
  onRejectAll: (feedback: string) => void;
}): ReactNode {
  const t = useTokens();
  const view = toSdpReviewView(envelope);
  const [chosen, setChosen] = useState<string>(view.recommendation);
  const [feedback, setFeedback] = useState('');
  const [rejecting, setRejecting] = useState(false);

  if (view.options.length === 0) {
    return (
      <Typography sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        The SDP review has not been assembled yet.
      </Typography>
    );
  }

  const selected = chosen.length > 0 ? chosen : view.options[0]?.optionId ?? '';

  // time–cost: duration (days) × build cost (major units)
  const costPts: ScatterPoint[] = view.options.map((o) => ({
    x: o.durationDays,
    y: o.buildCost.minorUnits / 100,
    label: shortLabel(o.solutionKind),
    color: solutionAccentColor(t, o.solutionKind),
    emphasized: o.recommended,
  }));
  // time–risk: duration (days) × composite risk
  const riskPts: ScatterPoint[] = view.options.map((o) => ({
    x: o.durationDays,
    y: o.compositeRisk,
    label: shortLabel(o.solutionKind),
    color: solutionAccentColor(t, o.solutionKind),
    emphasized: o.recommended,
  }));

  const durBounds = bounds(view.options.map((o) => o.durationDays), 0.12);
  const costBounds = bounds(view.options.map((o) => o.buildCost.minorUnits / 100), 0.15);
  const riskBounds = bounds(view.options.map((o) => o.compositeRisk), 0.2);

  return (
    <Box data-testid={UI_IDENTIFIERS.SdpReview.ROOT} sx={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 1040 }}>
      {/* audience banner */}
      <Paper sx={{ p: 2, bgcolor: t.committedBg, borderLeft: `5px solid ${t.committedDot}` }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, letterSpacing: '0.1em', color: t.committedFg, mb: 0.5 }}>AUDIENCE · MANAGEMENT</Typography>
        <Typography sx={{ fontFamily: t.body, fontSize: 14, color: t.committedFg }}>
          The SDP gate is not &ldquo;approve prose&rdquo; — it is <b>CHOOSE an option</b> to bind the plan of record and unlock Phase 3,
          or reject all options to re-enter Project Design. Excluded options stay visible so management sees the trade-offs.
        </Typography>
      </Paper>

      {/* options table */}
      <Paper sx={{ p: 0, overflow: 'hidden' }}>
        <Box sx={{ px: 2, py: 1, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${t.line}`, display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, letterSpacing: '0.08em', color: t.ink }}>OPTIONS</Typography>
          <ComputedBadge t={t} />
        </Box>
        <Box sx={{ overflowX: 'auto' }}>
          <Box sx={{ display: 'grid', gridTemplateColumns: '160px repeat(5, 1fr) 90px', minWidth: 880 }}>
            {['OPTION', 'DURATION', 'BUILD COST', 'RISK', 'MONTHLY OPS', 'PER-CYCLE NET', 'REC'].map((h) => (
              <Box key={h} sx={{ px: 1.25, py: 0.75, borderBottom: `1.5px solid ${t.line}` }}>
                <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.06em', color: t.muted }}>{h}</Typography>
              </Box>
            ))}
            {view.options.map((o) => (
              <Box key={o.optionId} sx={{ display: 'contents' }}>
                <Box sx={{ px: 1.25, py: 0.9, borderBottom: `1px solid ${t.line}`, display: 'flex', alignItems: 'center', gap: 0.6 }}>
                  <Box sx={{ width: 9, height: 9, bgcolor: solutionAccentColor(t, o.solutionKind), border: `1.5px solid ${t.line}` }} />
                  <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11.5, color: t.ink }}>{SOLUTION_LABELS[o.solutionKind] ?? o.solutionKind}</Typography>
                  {o.recommended ? <StarIcon sx={{ fontSize: 13, color: t.accent }} /> : null}
                </Box>
                <Cell t={t}>{`${String(o.durationDays)} d`}</Cell>
                <Cell t={t}>{formatMoney(o.buildCost)}</Cell>
                <Cell strong t={t}>{o.compositeRisk.toFixed(2)}</Cell>
                <Cell t={t}>{formatMoney(o.projectedMonthlyCost)}</Cell>
                <Cell t={t}>{formatMoney(o.expectedPerCycleNet)}</Cell>
                <Box sx={{ px: 1.25, py: 0.9, borderBottom: `1px solid ${t.line}`, display: 'flex', alignItems: 'center' }}>
                  {o.recommended ? (
                    <Chip label="REC" size="small" sx={{ height: 20, fontSize: 9.5, color: t.committedFg, bgcolor: t.committedBg }} />
                  ) : null}
                </Box>
              </Box>
            ))}
          </Box>
        </Box>
      </Paper>

      {/* the two curves */}
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 2 }}>
        <Paper sx={{ p: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
            <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink }}>TIME–COST CURVE</Typography>
            <ComputedBadge t={t} />
          </Box>
          <BandedScatter height={260} points={costPts} t={t} xLabel="duration (days)" xMax={durBounds.max} xMin={durBounds.min} yLabel="build cost" yMax={costBounds.max} yMin={costBounds.min} />
        </Paper>
        <Paper sx={{ p: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
            <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink }}>TIME–RISK CURVE</Typography>
            <ComputedBadge t={t} />
          </Box>
          <BandedScatter height={260} points={riskPts} t={t} xLabel="duration (days)" xMax={durBounds.max} xMin={durBounds.min} yLabel="composite risk" yMax={riskBounds.max} yMin={Math.min(0, riskBounds.min)} />
        </Paper>
      </Box>

      {/* recommendation */}
      {view.rationale.length > 0 && (
        <Paper sx={{ p: 2.5, borderLeft: `5px solid ${t.accent}` }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
            <StarIcon sx={{ fontSize: 18, color: t.accent }} />
            <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink }}>Architect&rsquo;s recommendation</Typography>
            <AuthoredBadge t={t} />
          </Box>
          <Typography sx={{ fontFamily: t.body, fontSize: 13.5, lineHeight: 1.6, color: t.ink }}>{view.rationale}</Typography>
        </Paper>
      )}

      {/* DECISION CAPTURE — the terminal gate */}
      <Paper data-testid={UI_IDENTIFIERS.SdpReview.GATE} sx={{ p: 0, overflow: 'hidden', border: `2px solid ${t.accent}` }}>
        <Box sx={{ px: 2.5, py: 1.5, bgcolor: t.accent, display: 'flex', alignItems: 'center', gap: 1 }}>
          <CheckIcon sx={{ fontSize: 18, color: t.accentText }} />
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, letterSpacing: '0.06em', color: t.accentText }}>DECISION CAPTURE — THE SDP GATE</Typography>
          <Box sx={{ flexGrow: 1 }} />
          <AuthoredBadge label="you decide" t={t} />
        </Box>

        <Box sx={{ p: 2.5 }}>
          {rejecting ? (
            <RejectAll feedback={feedback} pending={pending} t={t} onCancel={() => { setRejecting(false); }} onChange={setFeedback} onReject={() => { onRejectAll(feedback); }} />
          ) : (
            <>
              <Typography sx={{ fontFamily: t.mono, fontSize: 11, letterSpacing: '0.08em', color: t.muted, mb: 1 }}>1 · CHOOSE AN OPTION</Typography>
              <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr 1fr', md: `repeat(${String(Math.min(view.options.length, 4))}, 1fr)` }, gap: 1, mb: 2.5 }}>
                {view.options.map((o) => (
                  <OptionCard key={o.optionId} option={o} selected={selected === o.optionId} t={t} onSelect={() => { setChosen(o.optionId); }} />
                ))}
              </Box>

              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
                <Box sx={{ minWidth: 0 }}>
                  <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12.5, color: t.ink }}>
                    You will commit: {SOLUTION_LABELS[view.options.find((o) => o.optionId === selected)?.solutionKind ?? 'sdpReview'] ?? selected}
                  </Typography>
                  <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted }}>Commit binds the plan of record and unlocks Phase 3 (Construction).</Typography>
                </Box>
                <Box sx={{ flexGrow: 1 }} />
                <Button color="inherit" data-testid={UI_IDENTIFIERS.SdpReview.REJECT_ALL} disabled={pending} startIcon={<ReplayIcon />} sx={{ color: t.muted }} variant="text" onClick={() => { setRejecting(true); }}>
                  Reject all
                </Button>
                <Button color="primary" data-testid={UI_IDENTIFIERS.SdpReview.COMMIT} disabled={pending || selected.length === 0} startIcon={<CheckIcon />} variant="contained" onClick={() => { onCommit(selected); }}>
                  Commit &amp; unlock Phase 3
                </Button>
              </Box>
            </>
          )}
        </Box>
      </Paper>
    </Box>
  );
}

function OptionCard({ t, option, selected, onSelect }: { t: Tokens; option: SdpOptionView; selected: boolean; onSelect: () => void }): ReactNode {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.SdpReview.optionCard(option.optionId)}
      sx={{
        p: 1.5,
        cursor: 'pointer',
        border: `2px solid ${selected ? t.accent : t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        bgcolor: selected ? t.awaitingBg : 'transparent',
        boxShadow: selected && t.hardShadow ? `3px 3px 0 ${t.shadowColor}` : 'none',
        transition: 'all 90ms ease',
      }}
      onClick={onSelect}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.6 }}>
        <Box sx={{ width: 14, height: 14, borderRadius: '50%', border: `2px solid ${selected ? t.accent : t.line}`, bgcolor: selected ? t.accent : 'transparent', flexShrink: 0 }} />
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink }}>{SOLUTION_LABELS[option.solutionKind] ?? option.solutionKind}</Typography>
      </Box>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, mt: 0.5 }}>
        {formatMoney(option.buildCost)} · risk {option.compositeRisk.toFixed(2)}
      </Typography>
    </Box>
  );
}

function RejectAll({ t, feedback, pending, onChange, onCancel, onReject }: { t: Tokens; feedback: string; pending: boolean; onChange: (v: string) => void; onCancel: () => void; onReject: () => void }): ReactNode {
  return (
    <>
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, letterSpacing: '0.08em', color: t.muted, mb: 1 }}>REJECT ALL OPTIONS — RE-ENTER PROJECT DESIGN</Typography>
      <TextField
        fullWidth
        multiline
        data-testid={UI_IDENTIFIERS.SdpReview.REJECT_FEEDBACK}
        minRows={3}
        placeholder="Required: why none of the options work, and what the re-assembly should change."
        sx={{ mb: 2 }}
        value={feedback}
        onChange={(e) => { onChange(e.target.value); }}
      />
      <Box sx={{ display: 'flex', gap: 1.5, justifyContent: 'flex-end' }}>
        <Button color="inherit" disabled={pending} sx={{ color: t.muted }} variant="text" onClick={onCancel}>Cancel</Button>
        <Button color="primary" data-testid={UI_IDENTIFIERS.SdpReview.REJECT_SUBMIT} disabled={pending || feedback.trim().length === 0} startIcon={<ReplayIcon />} variant="contained" onClick={onReject}>
          Reject all &amp; re-assemble
        </Button>
      </Box>
    </>
  );
}

function Cell({ t, children, strong }: { t: Tokens; children: ReactNode; strong?: boolean }): ReactNode {
  return (
    <Box sx={{ px: 1.25, py: 0.9, borderBottom: `1px solid ${t.line}`, display: 'flex', alignItems: 'center' }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: strong === true ? 13 : 12, fontWeight: strong === true ? 700 : 500, color: t.ink }}>{children}</Typography>
    </Box>
  );
}
