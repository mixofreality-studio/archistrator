/**
 * The SDP-review artifact (audience: management) — the assembled options table
 * (the four joined rows), the time-cost and time-risk curves, the architect's
 * recommendation, and the DECISION CAPTURE gate: choose an option to commit
 * (submitSDPDecision commit <optionId>) or reject all options with feedback
 * (submitSDPDecision rejectAll). Ported visual design from ux-mock SdpReview, bound
 * to the real typed SdpReview candidate model via api/projectAdapters.toSdpReviewView.
 */
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import TextField from '@mui/material/TextField';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import StarIcon from '@mui/icons-material/Star';
import CheckIcon from '@mui/icons-material/Check';
import ReplayIcon from '@mui/icons-material/Replay';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import type { ProjectArtifactKind, ProjectArtifactModelEnvelope } from '../../contracts/types';
import { SOLUTION_LABELS } from '../../contracts/types';
import {
  toSdpReviewView,
  toRiskModelView,
  narrowProject,
  formatMoney,
  formatDurationDays,
  type RiskRowView,
  type SdpOptionView,
} from '../../contracts/projectAdapters';
import { solutionAccentColor } from './solutionAccent';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { BandedScatter, type ScatterPoint } from './charts';
import { SectionHeading } from './SectionHeading';
import { ComputedBadge, AuthoredBadge } from './computed';
import { useComments } from '../comments/CommentContext';
import { sdpOptionAnchor } from '../comments/CommentContext';
import { useRegisterAnchor } from '../comments/AnchorRegistry';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

/** The per-row "Comment on this item" affordance for the ARIA-table option rows. */
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

/**
 * One option's `role="row"` in the options table, with its comment ANCHOR
 * enrolled. Extracted from the parent's `.map()` body for the same reason
 * `ContractSignatureList`'s `OpRow` is: it calls {@link useRegisterAnchor}, a
 * hook, which React forbids inside a loop callback.
 *
 * Before this, `armOption` armed `sdpOptionAnchor(solutionKind)` and NOTHING
 * enrolled it, so every comment on an option — the one surface whose whole job is
 * choosing between options — could only fall into the margin's UNPLACED group, on
 * the live gate and in history alike.
 *
 * The ref goes on the row's FIRST CELL, not on the row itself: the row is
 * `display: contents` (it has to be, so its cells are items of the outer grid),
 * and an element with no box has an empty `getBoundingClientRect()` — it would
 * have enrolled an anchor that measures as offset 0 and placed the card at the
 * top of the margin, which is a worse lie than leaving it unplaced. The first
 * cell is the OPTION name, the visual start of the row the reader armed.
 */
function OptionRow({
  option,
  t,
  children,
}: {
  option: SdpOptionView;
  t: Tokens;
  /** The remaining cells, rendered by the caller (they need its formatters). */
  children: ReactNode;
}): ReactNode {
  const registerAnchor = useRegisterAnchor(sdpOptionAnchor(option.solutionKind));
  return (
    <Box role="row" sx={ROW_REVEAL_SX}>
      <Box
        ref={registerAnchor}
        role="cell"
        sx={{
          px: 1.25,
          py: 0.9,
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
            bgcolor: solutionAccentColor(t, option.solutionKind),
            border: `1.5px solid ${t.line}`,
          }}
        />
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink }}>
          {optionLabel(option.solutionKind)}
        </Typography>
        {option.recommended ? (
          <StarIcon aria-hidden sx={{ fontSize: 13, color: t.accent }} />
        ) : null}
      </Box>
      {children}
    </Box>
  );
}

/** Shared sx that reveals a row's comment button on row hover / keyboard focus. */
const ROW_REVEAL_SX = {
  display: 'contents',
  '& .row-comment-action': { opacity: 0, transition: 'opacity 120ms' },
  '&:hover .row-comment-action, &:focus-within .row-comment-action': { opacity: 1 },
} as const;

function optionLabel(kind: ProjectArtifactKind): string {
  return SOLUTION_LABELS[kind] ?? kind;
}

/** Average days in a month — the calendar-days → months hint for management. */
const DAYS_PER_MONTH = 30.44;

function formatMonths(days: number): string {
  return `≈${(days / DAYS_PER_MONTH).toFixed(1)} mo`;
}

/**
 * The server's rationale names options by their slot kind
 * (`recommend decompressedSolution: …`) and cites the book by appendix. Both are
 * opaque to a first-time reader; the option's display label and the book's name
 * are not. Purely a wording pass — the numbers are left exactly as authored.
 */
function humanizeRationale(rationale: string): string {
  let out = rationale;
  for (const [kind, label] of Object.entries(SOLUTION_LABELS)) {
    out = out.split(kind).join(label);
  }
  return out
    .replace(/^recommend\s+/i, 'Recommend ')
    .replace(/\bApp C\b/g, 'Righting Software Appendix C');
}

/**
 * What the per-option glossary says about each option. These are the four
 * Project Design options of The Method (Righting Software ch. 9–11); a reader
 * meeting "Subcritical" for the first time cannot be expected to know it is the
 * deliberately understaffed option.
 */
const OPTION_MEANING: Partial<Record<ProjectArtifactKind, string>> = {
  normalSolution: 'the minimum staffing that keeps the critical path moving unimpeded.',
  decompressedSolution:
    'the normal plan with deliberate schedule slack added, trading a little time for lower risk.',
  subcriticalSolution:
    'deliberately understaffed — shown to prove fewer people makes the project longer and costlier, not cheaper.',
  compressedSolution:
    'finishes sooner through more parallel work and top resources, at higher cost and risk.',
};

function bounds(values: number[], pad: number): { min: number; max: number } {
  if (values.length === 0) return { min: 0, max: 1 };
  const lo = Math.min(...values);
  const hi = Math.max(...values);
  const span = hi - lo || Math.abs(hi) || 1;
  return { min: lo - span * pad, max: hi + span * pad };
}

export function SdpReviewView({
  envelope,
  riskEnvelope,
  pending,
  readOnly: readOnlyProp = false,
  onCommit,
  onRejectAll,
  decision = 'own',
  onChoose,
}: {
  envelope: ProjectArtifactModelEnvelope | undefined;
  /**
   * The committed `riskModel` slot, OPTIONAL. When present, the options its
   * exclusion zones rule out are marked EXCLUDED (with the model's own reason) in
   * the table, on the decision cards and on the time–risk curve, and the curve
   * shades the too-risky / over-safe zones. Without it the view still renders,
   * just without that judgement — it never invents one.
   */
  riskEnvelope?: ProjectArtifactModelEnvelope | undefined;
  /** A decision mutation is in flight — disable the gate. */
  pending: boolean;
  /**
   * The SDP is already committed, or the reader is on a past revision — the
   * decision gate is read-only.
   *
   * Read-only is INERT, not merely disabled: the option cards stop being radios
   * altogether (no `role`, no `aria-checked`, no tab stop, no cursor, no
   * handlers) and present the standing choice in words, so a read-only surface
   * carries no focusable control that does nothing and no orphaned ARIA. Which
   * option was chosen is said as text, never by colour alone.
   *
   * A model that already carries a `chosenOption` is read-only regardless: the
   * decision it records is the plan of record, and offering a second choice over
   * it would be a control that changes nothing.
   */
  readOnly?: boolean;
  onCommit: (optionId: string) => void;
  /**
   * Reject every option, with the required feedback. OMITTED where the surface offers
   * no reject-all (a read-only history; a chooser whose host has no op for it): the
   * Reject all button and its composer are then not rendered at all.
   */
  onRejectAll?: ((feedback: string) => void) | undefined;
  /**
   * WHERE the decision is made. `'own'` (the default) is today's terminal gate —
   * choose an option, Commit & unlock Phase 3, Reject all — exactly as the
   * Project Design experience has always rendered it, and every existing caller
   * is untouched.
   *
   * `'chooser'` keeps the option radiogroup and DROPS the Commit verb, for a
   * surface that owns the approval itself: the Activity Experience routes approve
   * through ONE SubmitBar (spec §7.2), so a second Commit button would be a
   * duplicate verb. Reject all is NOT a send-back (spec §12 B7): it declines the
   * plan with a reason and returns no artifact for a redraft, so it stays here
   * whenever the host hands {@link onRejectAll}. The standing choice is reported
   * through {@link onChoose}. The Phase-2 twin of `SubmitBar.allowSendBack`
   * (RULING R11).
   */
  decision?: 'own' | 'chooser';
  /**
   * The option the reader currently has selected, reported on every change AND
   * once on mount (the architect's recommendation) — a controlling surface must
   * know which plan its own Approve would commit before anything is touched.
   * Give it a STABLE identity (a `useState` setter, or `useCallback`).
   */
  onChoose?: ((optionId: string) => void) | undefined;
}): ReactNode {
  const t = useTokens();
  // `enabled` is the read-only surface's switch: this view hand-rolls its option
  // rows' comment button instead of going through `CommentableList`, which reads
  // the same flag (CommentableList.tsx:273). Without it the Activity
  // Experience's read-only history — which shadows the provider with a disabled
  // one — would still render a focusable button whose arm is a no-op.
  const { setAnchor, enabled: commentsEnabled } = useComments();
  const view = toSdpReviewView(envelope);
  const chosenOnRecord = narrowProject(envelope, 'sdpReview')?.chosenOption ?? '';
  const readOnly = readOnlyProp || chosenOnRecord.length > 0;
  const risk = toRiskModelView(riskEnvelope);
  const riskByKind = new Map<ProjectArtifactKind, RiskRowView>(
    risk.rows.map((r) => [r.solutionKind, r])
  );
  const exclusionOf = (o: SdpOptionView): string | undefined => {
    const r = riskByKind.get(o.solutionKind);
    if (r === undefined || r.included) return undefined;
    return r.exclusionReason.length > 0 ? r.exclusionReason : 'outside the acceptable risk range';
  };
  const hasRisk = risk.rows.length > 0;

  // The reader's OWN pick, '' until they touch the radiogroup. The default is
  // derived on every render, NOT seeded into state: the envelope routinely
  // arrives after the first render (the slot is fetched), and a `useState`
  // seeded from an empty view kept '' forever, so the gate silently defaulted to
  // the FIRST option instead of the architect's recommendation — and reported
  // that to the host's Approve.
  const [chosen, setChosen] = useState<string>('');
  const [feedback, setFeedback] = useState('');
  const [rejecting, setRejecting] = useState(false);

  // Roving-tabindex radiogroup over the option cards: the selected card is the
  // single tab-stop, arrow keys move (and select), Space/Enter selects.
  const optionRefs = useRef<(HTMLDivElement | null)[]>([]);
  const moveRadio = useCallback(
    (from: number, dir: number): void => {
      const n = view.options.length;
      if (n === 0) return;
      const next = (from + dir + n) % n;
      optionRefs.current[next]?.focus();
      const o = view.options[next];
      if (o !== undefined && !readOnly) setChosen(o.optionId);
    },
    [view.options, readOnly]
  );
  const onRadioKeyDown = useCallback(
    (e: React.KeyboardEvent, index: number): void => {
      switch (e.key) {
        case 'ArrowRight':
        case 'ArrowDown':
          e.preventDefault();
          moveRadio(index, 1);
          break;
        case 'ArrowLeft':
        case 'ArrowUp':
          e.preventDefault();
          moveRadio(index, -1);
          break;
        case ' ':
        case 'Enter': {
          e.preventDefault();
          const o = view.options[index];
          if (o !== undefined && !readOnly) setChosen(o.optionId);
          break;
        }
        default:
          break;
      }
    },
    [moveRadio, view.options, readOnly]
  );

  const armOption = (o: SdpOptionView): void => {
    const label = optionLabel(o.solutionKind);
    setAnchor({
      kind: 'node',
      label,
      source: `SDP Review · ${label}`,
      jsonPath: sdpOptionAnchor(o.solutionKind),
    });
  };

  const isOption = (id: string): boolean => view.options.some((o) => o.optionId === id);
  const selected = readOnly
    ? isOption(chosenOnRecord)
      ? chosenOnRecord
      : view.recommendation
    : isOption(chosen)
      ? chosen
      : isOption(view.recommendation)
        ? view.recommendation
        : (view.options[0]?.optionId ?? '');

  // Report the standing choice upward. Above the early return because a hook
  // cannot sit after one; a no-op when nobody is listening (`decision: 'own'`).
  useEffect(() => {
    onChoose?.(selected);
  }, [selected, onChoose]);

  if (view.options.length === 0) {
    return (
      <Paper sx={{ p: 3 }}>
        <SectionHeading
          hint="It appears here once the four plan options (normal, decompressed-normal, subcritical and compressed) and their risk model are computed. Management then chooses one to become the plan of record."
          t={t}
        >
          Software Development Plan review — not assembled yet
        </SectionHeading>
      </Paper>
    );
  }

  const recommendedOption = view.options.find((o) => o.recommended);
  const selectedOption = view.options.find((o) => o.optionId === selected);

  // time–cost: duration (days) × build cost (major units)
  const costPts: ScatterPoint[] = view.options.map((o) => ({
    x: o.durationDays,
    y: o.buildCost.minorUnits / 100,
    label: optionLabel(o.solutionKind),
    color: solutionAccentColor(t, o.solutionKind),
    emphasized: o.recommended,
    out: exclusionOf(o) !== undefined,
  }));
  // time–risk: duration (days) × composite risk
  const riskPts: ScatterPoint[] = view.options.map((o) => ({
    x: o.durationDays,
    y: o.compositeRisk,
    label: optionLabel(o.solutionKind),
    color: solutionAccentColor(t, o.solutionKind),
    emphasized: o.recommended,
    out: exclusionOf(o) !== undefined,
  }));

  const durBounds = bounds(
    view.options.map((o) => o.durationDays),
    0.12
  );
  const costBounds = bounds(
    view.options.map((o) => o.buildCost.minorUnits / 100),
    0.15
  );
  const riskValues = view.options.map((o) => o.compositeRisk);
  if (hasRisk) riskValues.push(risk.tooRiskyThreshold, risk.overSafeThreshold);
  const riskBounds = bounds(riskValues, 0.2);
  const riskYMin = Math.max(
    0,
    Math.min(riskBounds.min, hasRisk ? risk.overSafeThreshold - 0.05 : 0)
  );
  const riskYMax = Math.min(1, riskBounds.max);
  const riskBands = hasRisk
    ? [
        {
          from: risk.tooRiskyThreshold,
          to: riskYMax,
          kind: 'out' as const,
          label: `EXCLUDED · TOO RISKY (> ${risk.tooRiskyThreshold.toFixed(2)})`,
        },
        {
          from: risk.overSafeThreshold,
          to: risk.tooRiskyThreshold,
          kind: 'in' as const,
          label: 'ACCEPTABLE RISK',
        },
        {
          from: riskYMin,
          to: risk.overSafeThreshold,
          kind: 'out' as const,
          label: `EXCLUDED · OVER-SAFE (< ${risk.overSafeThreshold.toFixed(2)})`,
        },
      ].filter((b) => b.to > b.from)
    : undefined;

  const currency = view.options[0]?.buildCost.currency ?? 'USD';
  const moneyTick = (v: number): string =>
    formatMoney({ minorUnits: Math.round(v * 100), currency });
  const dayTick = (d: number): string => `${String(Math.round(d))} d`;

  const COLUMNS = [
    'Option',
    'Duration · calendar days',
    'Build cost',
    'Composite risk · 0–1',
    'Monthly run cost',
    'Charge per billing cycle',
    'Standing',
  ];

  return (
    <Box
      data-testid={UI_IDENTIFIERS.SdpReview.ROOT}
      sx={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 1040 }}
    >
      {/* audience banner */}
      <Paper sx={{ p: 2, bgcolor: t.committedBg, borderLeft: `5px solid ${t.committedDot}` }}>
        <SectionHeading color={t.committedFg} t={t}>
          Software Development Plan (SDP) review · for management
        </SectionHeading>
        <Typography sx={{ fontFamily: t.body, fontSize: 14, color: t.committedFg, mt: 0.5 }}>
          Project Design produced several ways to build the same system. Here you{' '}
          <b>choose one option</b> to make it the plan of record — that is milestone M0, and it
          unlocks Phase 3 (construction) — or reject them all and send Project Design back for
          rework. Options the risk model excludes stay listed so the trade-offs are visible.
        </Typography>
      </Paper>

      {/* options table */}
      <Paper sx={{ p: 0, overflow: 'hidden' }}>
        <Box
          sx={{
            px: 2,
            py: 1.25,
            bgcolor: t.paperAlt,
            borderBottom: `1.5px solid ${t.line}`,
          }}
        >
          <SectionHeading adornment={<ComputedBadge t={t} />} t={t}>
            The options
          </SectionHeading>
        </Box>
        <Box
          aria-label="SDP options table — scroll sideways for more columns"
          role="region"
          sx={{
            overflowX: 'auto',
            '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: -2 },
          }}
          tabIndex={0}
        >
          <Box
            aria-label="SDP options"
            role="table"
            sx={{
              display: 'grid',
              gridTemplateColumns: '170px repeat(5, 1fr) 170px',
              minWidth: 960,
            }}
          >
            <Box role="row" sx={{ display: 'contents' }}>
              {COLUMNS.map((h) => (
                <Box
                  key={h}
                  role="columnheader"
                  sx={{ px: 1.25, py: 0.75, borderBottom: `1.5px solid ${t.line}` }}
                >
                  <Typography
                    sx={{
                      fontFamily: t.mono,
                      fontSize: 10.5,
                      letterSpacing: '0.04em',
                      textTransform: 'uppercase',
                      color: t.muted,
                      lineHeight: 1.3,
                    }}
                  >
                    {h}
                  </Typography>
                </Box>
              ))}
            </Box>
            {view.options.map((o) => {
              const excluded = exclusionOf(o);
              return (
                <OptionRow key={o.optionId} option={o} t={t}>
                  <Cell sub={formatMonths(o.durationDays)} t={t}>
                    {formatDurationDays(o.durationDays)}
                  </Cell>
                  <Cell t={t}>{formatMoney(o.buildCost)}</Cell>
                  <Cell strong t={t}>
                    {o.compositeRisk.toFixed(2)}
                  </Cell>
                  <Cell t={t}>{formatMoney(o.projectedMonthlyCost)}</Cell>
                  <Cell t={t}>{formatMoney(o.expectedPerCycleNet)}</Cell>
                  <Box
                    role="cell"
                    sx={{
                      px: 1.25,
                      py: 0.9,
                      borderBottom: `1px solid ${t.line}`,
                      display: 'flex',
                      alignItems: 'center',
                      gap: 0.75,
                    }}
                  >
                    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.4, minWidth: 0 }}>
                      {o.recommended ? (
                        <StandingChip bg={t.committedBg} fg={t.committedFg} label="Recommended" />
                      ) : null}
                      {excluded !== undefined ? (
                        <>
                          <StandingChip bg={t.awaitingBg} fg={t.awaitingFg} label="Excluded" />
                          <Typography
                            sx={{
                              fontFamily: t.body,
                              fontSize: 11.5,
                              color: t.muted,
                              lineHeight: 1.3,
                            }}
                          >
                            {excluded}
                          </Typography>
                        </>
                      ) : null}
                      {!o.recommended && excluded === undefined ? (
                        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
                          {hasRisk ? 'Within range' : '—'}
                        </Typography>
                      ) : null}
                    </Box>
                    <Box sx={{ flexGrow: 1 }} />
                    {commentsEnabled ? (
                      <RowCommentButton
                        label={optionLabel(o.solutionKind)}
                        t={t}
                        testKey={o.optionId}
                        onArm={() => {
                          armOption(o);
                        }}
                      />
                    ) : null}
                  </Box>
                </OptionRow>
              );
            })}
          </Box>
        </Box>
        <OptionGlossary
          options={view.options}
          overSafe={hasRisk ? risk.overSafeThreshold : undefined}
          t={t}
          tooRisky={hasRisk ? risk.tooRiskyThreshold : undefined}
        />
      </Paper>

      {/* the two curves */}
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 2 }}>
        <Paper sx={{ p: 2 }}>
          <SectionHeading
            adornment={<ComputedBadge t={t} />}
            hint="Each dot is one option: how long it takes against what it costs to build. Further left is sooner; lower is cheaper."
            t={t}
          >
            Time–cost curve
          </SectionHeading>
          <Box sx={{ mt: 1 }}>
            <BandedScatter
              formatX={dayTick}
              formatY={moneyTick}
              height={260}
              points={costPts}
              t={t}
              xLabel="duration (calendar days)"
              xMax={durBounds.max}
              xMin={durBounds.min}
              yLabel="build cost"
              yMax={costBounds.max}
              yMin={costBounds.min}
            />
          </Box>
        </Paper>
        <Paper sx={{ p: 2 }}>
          <SectionHeading
            adornment={<ComputedBadge t={t} />}
            hint={
              hasRisk
                ? 'Each dot is one option: how long it takes against its composite risk. Shaded zones are the risk model’s limits; a ✕ marks an excluded option.'
                : 'Each dot is one option: how long it takes against its composite risk. Lower is safer.'
            }
            t={t}
          >
            Time–risk curve
          </SectionHeading>
          <Box sx={{ mt: 1 }}>
            <BandedScatter
              {...(riskBands !== undefined ? { bands: riskBands } : {})}
              formatX={dayTick}
              formatY={(y) => y.toFixed(2)}
              height={260}
              points={riskPts}
              t={t}
              xLabel="duration (calendar days)"
              xMax={durBounds.max}
              xMin={durBounds.min}
              yLabel="composite risk"
              yMax={riskYMax}
              yMin={riskYMin}
            />
          </Box>
        </Paper>
      </Box>

      {/* recommendation */}
      {view.rationale.length > 0 && (
        <Paper sx={{ p: 2.5, borderLeft: `5px solid ${t.accent}` }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
            <StarIcon aria-hidden sx={{ fontSize: 18, color: t.accent }} />
            <Typography
              component="h3"
              sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink, m: 0 }}
            >
              Architect&rsquo;s recommendation
              {recommendedOption !== undefined
                ? `: ${optionLabel(recommendedOption.solutionKind)}`
                : ''}
            </Typography>
            <AuthoredBadge t={t} />
          </Box>
          <Typography sx={{ fontFamily: t.body, fontSize: 13.5, lineHeight: 1.6, color: t.ink }}>
            {humanizeRationale(view.rationale)}
          </Typography>
        </Paper>
      )}

      {/* DECISION CAPTURE — the terminal gate (disabled + read-only once committed) */}
      <Paper
        data-testid={UI_IDENTIFIERS.SdpReview.GATE}
        sx={{ p: 0, overflow: 'hidden', border: `2px solid ${t.accent}` }}
      >
        <Box
          sx={{
            px: 2.5,
            py: 1.5,
            bgcolor: t.accent,
            display: 'flex',
            alignItems: 'center',
            gap: 1,
          }}
        >
          <CheckIcon aria-hidden sx={{ fontSize: 18, color: t.accentText }} />
          <Typography
            component="h3"
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 13,
              letterSpacing: '0.06em',
              color: t.accentText,
              m: 0,
            }}
          >
            {readOnly
              ? 'DECISION — COMMITTED'
              : decision === 'chooser'
                ? 'YOUR DECISION — CHOOSE THE OPTION'
                : 'YOUR DECISION — THE SDP GATE'}
          </Typography>
        </Box>

        <Box sx={{ p: 2.5 }}>
          {rejecting && !readOnly && onRejectAll !== undefined ? (
            <RejectAll
              feedback={feedback}
              pending={pending}
              t={t}
              onCancel={() => {
                setRejecting(false);
              }}
              onChange={setFeedback}
              onReject={() => {
                onRejectAll(feedback.trim());
              }}
            />
          ) : (
            <>
              <Typography
                id="sdp-choose-option-label"
                sx={{
                  fontFamily: t.mono,
                  fontSize: 11,
                  letterSpacing: '0.08em',
                  color: t.muted,
                  mb: 1,
                }}
              >
                {readOnly ? 'THE OPTION ON RECORD' : 'CHOOSE AN OPTION'}
              </Typography>
              <Box
                // `group` and not `radiogroup`: read-only, the cards are not
                // radios (OptionCard), and a radiogroup containing none is
                // orphaned ARIA. The heading above it stops instructing, too.
                aria-labelledby="sdp-choose-option-label"
                role={readOnly ? 'group' : 'radiogroup'}
                sx={{
                  display: 'grid',
                  gridTemplateColumns: {
                    xs: '1fr',
                    sm: '1fr 1fr',
                    md: `repeat(${String(Math.min(view.options.length, 4))}, 1fr)`,
                  },
                  gap: 1,
                  mb: 2.5,
                }}
              >
                {view.options.map((o, i) => (
                  <OptionCard
                    excluded={exclusionOf(o)}
                    key={o.optionId}
                    option={o}
                    readOnly={readOnly}
                    refCb={(el) => {
                      optionRefs.current[i] = el;
                    }}
                    selected={selected === o.optionId}
                    t={t}
                    onKeyDown={(e) => {
                      onRadioKeyDown(e, i);
                    }}
                    onSelect={() => {
                      if (!readOnly) setChosen(o.optionId);
                    }}
                  />
                ))}
              </Box>

              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
                <Box aria-live="polite" sx={{ minWidth: 0 }}>
                  <Typography
                    sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12.5, color: t.ink }}
                  >
                    {readOnly ? 'Committed' : 'You will commit'}:{' '}
                    {selectedOption !== undefined
                      ? optionLabel(selectedOption.solutionKind)
                      : selected}
                  </Typography>
                  <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
                    {readOnly
                      ? 'This decision is already bound as the plan of record.'
                      : decision === 'chooser'
                        ? 'Approving this review binds it as the plan of record and unlocks Phase 3 (Construction).'
                        : 'Commit binds the plan of record and unlocks Phase 3 (Construction).'}
                    {!readOnly &&
                    selectedOption !== undefined &&
                    exclusionOf(selectedOption) !== undefined
                      ? ' Note: the risk model excludes this option.'
                      : ''}
                  </Typography>
                </Box>
                <Box sx={{ flexGrow: 1 }} />
                {onRejectAll !== undefined && !readOnly ? (
                  <Button
                    color="inherit"
                    data-testid={UI_IDENTIFIERS.SdpReview.REJECT_ALL}
                    disabled={pending}
                    startIcon={<ReplayIcon />}
                    sx={{ color: t.muted }}
                    variant="text"
                    onClick={() => {
                      setRejecting(true);
                    }}
                  >
                    Reject all
                  </Button>
                ) : null}
                {decision === 'own' ? (
                  <Button
                    color="primary"
                    data-testid={UI_IDENTIFIERS.SdpReview.COMMIT}
                    disabled={pending || readOnly || selected.length === 0}
                    startIcon={<CheckIcon />}
                    variant="contained"
                    onClick={() => {
                      onCommit(selected);
                    }}
                  >
                    Commit &amp; unlock Phase 3
                  </Button>
                ) : null}
              </Box>
            </>
          )}
        </Box>
      </Paper>
    </Box>
  );
}

/** A small text pill for the table's Standing column — a word, never colour alone. */
function StandingChip({ label, fg, bg }: { label: string; fg: string; bg: string }): ReactNode {
  return (
    <Chip
      label={label}
      size="small"
      sx={{
        alignSelf: 'flex-start',
        height: 22,
        fontSize: 11,
        fontWeight: 700,
        color: fg,
        bgcolor: bg,
        border: `1px solid ${fg}`,
      }}
    />
  );
}

/**
 * "How to read this table" — said ONCE, in words, under the options. Every
 * column header names its unit; this names what each option IS and what the
 * risk and money columns mean, which a header cannot.
 */
function OptionGlossary({
  t,
  options,
  tooRisky,
  overSafe,
}: {
  t: Tokens;
  options: SdpOptionView[];
  tooRisky: number | undefined;
  overSafe: number | undefined;
}): ReactNode {
  const kinds = options.map((o) => o.solutionKind).filter((k, i, a) => a.indexOf(k) === i);
  const term = (label: string): ReactNode => (
    <Box component="dt" sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink }}>
      {label}
    </Box>
  );
  const def = (text: ReactNode): ReactNode => (
    <Box
      component="dd"
      sx={{ m: 0, fontFamily: t.body, fontSize: 12.5, lineHeight: 1.45, color: t.muted }}
    >
      {text}
    </Box>
  );
  return (
    <Box sx={{ px: 2, py: 1.5, borderTop: `1.5px solid ${t.line}`, bgcolor: t.paperAlt }}>
      <Typography
        component="h4"
        sx={{
          fontFamily: t.mono,
          fontSize: 11,
          letterSpacing: '0.08em',
          color: t.muted,
          m: 0,
          mb: 1,
        }}
      >
        HOW TO READ THE OPTIONS
      </Typography>
      <Box
        component="dl"
        sx={{
          m: 0,
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', sm: '180px 1fr' },
          columnGap: 2,
          rowGap: 0.75,
        }}
      >
        {kinds.map((k) => {
          const meaning = OPTION_MEANING[k];
          return meaning === undefined ? null : (
            <Box key={k} sx={{ display: 'contents' }}>
              {term(optionLabel(k))}
              {def(meaning.charAt(0).toUpperCase() + meaning.slice(1))}
            </Box>
          );
        })}
        {term('Composite risk')}
        {def(
          <>
            From 0 (every activity has plenty of slack) to 1 (everything is critical): the average
            of criticality risk (how much of the work sits on or near the critical path) and
            activity risk (how unevenly slack is spread).
            {tooRisky !== undefined && overSafe !== undefined
              ? ` The risk model excludes options above ${tooRisky.toFixed(2)} (too risky) or below ${overSafe.toFixed(2)} (over-safe: paying for slack you do not need).`
              : ''}
          </>
        )}
        {term('Monthly run cost')}
        {def('The projected cost of operating the built system per month at your declared load.')}
        {term('Charge per billing cycle')}
        {def('The expected charge for each billing cycle of operating the system.')}
      </Box>
    </Box>
  );
}

/** The read-only card's word for the standing choice — never colour alone. */
const CHOSEN_MARK = 'CHOSEN';

function OptionCard({
  t,
  option,
  excluded,
  selected,
  readOnly,
  refCb,
  onSelect,
  onKeyDown,
}: {
  t: Tokens;
  option: SdpOptionView;
  /** The risk model's exclusion reason, when it rules this option out. */
  excluded: string | undefined;
  selected: boolean;
  /** Inert presentation: no role, no tab stop, no handlers. See {@link SdpReviewView}. */
  readOnly: boolean;
  refCb: (el: HTMLDivElement | null) => void;
  onSelect: () => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
}): ReactNode {
  const label = optionLabel(option.solutionKind);
  return (
    <Box
      // A read-only card is not a radio that refuses to change — it is not a
      // radio. Keeping `role="radio"` with a tab stop and a pointer cursor left a
      // control a keyboard reader could reach and press to no effect, inside a
      // radiogroup that no longer exists.
      aria-checked={readOnly ? undefined : selected}
      aria-label={
        readOnly
          ? undefined
          : `${label}, ${formatDurationDays(option.durationDays)}, ${formatMoney(option.buildCost)}, risk ${option.compositeRisk.toFixed(2)}${option.recommended ? ', recommended' : ''}${excluded !== undefined ? `, excluded: ${excluded}` : ''}`
      }
      data-testid={UI_IDENTIFIERS.SdpReview.optionCard(option.optionId)}
      ref={refCb}
      role={readOnly ? undefined : 'radio'}
      sx={{
        p: 1.5,
        cursor: readOnly ? 'default' : 'pointer',
        border: `2px solid ${selected ? t.accent : t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        bgcolor: selected ? t.awaitingBg : 'transparent',
        boxShadow: selected && t.hardShadow ? `3px 3px 0 ${t.shadowColor}` : 'none',
        transition: 'all 90ms ease',
        ...(readOnly
          ? {}
          : { '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 2 } }),
      }}
      tabIndex={readOnly ? undefined : selected ? 0 : -1}
      onClick={readOnly ? undefined : onSelect}
      onKeyDown={readOnly ? undefined : onKeyDown}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.6 }}>
        <Box
          sx={{
            width: 14,
            height: 14,
            borderRadius: '50%',
            border: `2px solid ${selected ? t.accent : t.line}`,
            bgcolor: selected ? t.accent : 'transparent',
            flexShrink: 0,
          }}
        />
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink }}>
          {label}
        </Typography>
        {/* With the radio role gone, the dot is colour alone — which is not a
            signal. The word is. */}
        {readOnly && selected ? (
          <Typography
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 9.5,
              letterSpacing: '0.12em',
              color: t.accentInk,
            }}
          >
            {CHOSEN_MARK}
          </Typography>
        ) : null}
      </Box>
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted, mt: 0.5 }}>
        {formatDurationDays(option.durationDays)} · {formatMoney(option.buildCost)} · risk{' '}
        {option.compositeRisk.toFixed(2)}
      </Typography>
      {option.recommended || excluded !== undefined ? (
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 10.5,
            letterSpacing: '0.06em',
            mt: 0.5,
            color: excluded !== undefined ? t.awaitingFg : t.committedText,
          }}
        >
          {option.recommended ? 'RECOMMENDED' : ''}
          {option.recommended && excluded !== undefined ? ' · ' : ''}
          {excluded !== undefined ? 'EXCLUDED BY RISK MODEL' : ''}
        </Typography>
      ) : null}
    </Box>
  );
}

function RejectAll({
  t,
  feedback,
  pending,
  onChange,
  onCancel,
  onReject,
}: {
  t: Tokens;
  feedback: string;
  pending: boolean;
  onChange: (v: string) => void;
  onCancel: () => void;
  onReject: () => void;
}): ReactNode {
  return (
    <>
      <Typography
        sx={{ fontFamily: t.mono, fontSize: 11, letterSpacing: '0.08em', color: t.muted, mb: 1 }}
      >
        REJECT ALL OPTIONS — RE-ENTER PROJECT DESIGN
      </Typography>
      <TextField
        fullWidth
        multiline
        minRows={3}
        placeholder="Required: why none of the options work, and what the re-assembly should change."
        // On the textarea itself, so a black-box `fill` lands on the field it names.
        slotProps={{ htmlInput: { 'data-testid': UI_IDENTIFIERS.SdpReview.REJECT_FEEDBACK } }}
        sx={{ mb: 2 }}
        value={feedback}
        onChange={(e) => {
          onChange(e.target.value);
        }}
      />
      <Box sx={{ display: 'flex', gap: 1.5, justifyContent: 'flex-end' }}>
        <Button
          color="inherit"
          disabled={pending}
          sx={{ color: t.muted }}
          variant="text"
          onClick={onCancel}
        >
          Cancel
        </Button>
        <Button
          color="primary"
          data-testid={UI_IDENTIFIERS.SdpReview.REJECT_SUBMIT}
          disabled={pending || feedback.trim().length === 0}
          startIcon={<ReplayIcon />}
          variant="contained"
          onClick={onReject}
        >
          Reject all &amp; re-assemble
        </Button>
      </Box>
    </>
  );
}

function Cell({
  t,
  children,
  strong,
  sub,
}: {
  t: Tokens;
  children: ReactNode;
  strong?: boolean;
  /** A secondary line under the value (a unit conversion). */
  sub?: string;
}): ReactNode {
  return (
    <Box
      role="cell"
      sx={{
        px: 1.25,
        py: 0.9,
        borderBottom: `1px solid ${t.line}`,
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'center',
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
      {sub !== undefined ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>{sub}</Typography>
      ) : null}
    </Box>
  );
}
