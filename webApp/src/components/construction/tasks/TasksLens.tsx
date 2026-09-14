/**
 * The TASKS lens (Stage C) — one row per decision the construction pipeline is
 * stopped on, waiting for a human (spec §6, §7.7).
 *
 * WHAT A ROW IS
 * -------------
 * One DECISION, not one activity: `(activityId, gate, round)` for a gate, or the
 * activity itself where the machine stopped (a variance awaiting a steer, a
 * terminal failure). The owed set comes from the live workflow stage
 * (owedWork.ts), ranked risk-floor-first then by blast radius (owedRanking.ts);
 * every sentence it says is in tasksLensCopy.ts. This file is presentation only —
 * the pure `components` layer: props and useTokens, nothing else.
 *
 * THE COLUMNS (§7.7): WHAT (the float rail + critical-path border weight on the
 * left edge, `activity › phase › gate task`, the ask, what you are about to read,
 * the machine's verdict) · WHY (the rule that opened it) · WHO (the reviewer set)
 * · WAITING (and the round) · BLAST (`↓N` downstream, float, critical path) ·
 * ACTION ([Review] opens the shared pane in place; [GitHub ↗] only where a PR
 * exists — never a dead link).
 *
 * UNKNOWN IS SAID, NOT FILLED: the client is never told when a gate opened (plan
 * Q2), so WAITING reads `—` with a tooltip saying why; a round with no ledger
 * entry reads `round —`; a reviewer set nobody reported reads `—`.
 *
 * GEOMETRY: seven columns when the lens has the room; under ~860px of its own
 * width (the shared pane open at 1280/1366) each row folds into three lines —
 * WHAT across, then WHY/WHO, then WAITING/BLAST — with ACTION held on the right, so
 * no column is ever scrolled out of sight. Folding is a container query on the
 * lens itself, not a viewport media query: it is the pane, not the window, that
 * takes the room.
 */
import type { ReactElement, ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Link from '@mui/material/Link';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';

import type { GitRow, ReviewPolicyView } from '../../../contracts/types';
import type { FloatBand } from '../../../contracts/projectAdapters';
import { useTokens } from '../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { bandTokens } from '../../project/bandTokens';
import { KindBadge } from '../KindBadge';
import type { LensSelection } from '../lens/useLensSelection';
import { criticalBorderPx, floatPresentation } from '../list/activityRowPresentation.ts';
import { taskDetailStateFill } from '../detail/detailPaneState.ts';
import type { RankedOwed } from './owedRanking.ts';
import { OWED_CHIP } from './owedChip.ts';
import {
  allClearHeadlineFor,
  askFor,
  beginLinkLabel,
  ciVerdictFor,
  emptyStateLine,
  filteredLineFor,
  headlineFor,
  policyBannerFor,
  policySummaryFor,
  retryLabel,
  roundLabel,
  slotsLineFor,
  CANT_TURN_OFF_LABEL,
  FLOOR_MAY_STILL_ASK_LABEL,
  FLOOR_MAY_STILL_ASK_TOOLTIP,
  HEADLINE_TOOLTIP,
  SET_POLICY_LABEL,
  STOP_ASKING_LABEL,
  whyAffordanceFor,
  uncheckedErroredLine,
  uncheckedPendingLine,
  WAITING_UNKNOWN_TOOLTIP,
  type EmptyStateCounts,
  type UncheckedCounts,
} from './tasksLensCopy.ts';

const FLOAT_BANDS: ReadonlySet<string> = new Set(['critical', 'red', 'yellow', 'green']);
const asBand = (band: string | undefined): FloatBand | undefined =>
  band !== undefined && FLOAT_BANDS.has(band) ? (band as FloatBand) : undefined;

/** The one line the row shows while a decision it sent is in flight (Task 5). */
export interface RowFlowNote {
  tone: 'progress' | 'ok' | 'danger';
  text: string;
}

export interface TasksLensProps {
  /** The owed decisions the toolbar lets through, already ranked. */
  items: readonly RankedOwed[];
  /** How many are owed before the toolbar's filters — tells "nothing is owed"
   *  apart from "your filters hide what is owed". */
  totalOwed: number;
  projectId: string;
  policy: ReviewPolicyView | undefined;
  supervisionCap: number | undefined;
  selection: LensSelection;
  /** "Contract · 12 ops" / "Test plan · 5 scenarios" / "—" for one item. */
  shapeOf: (item: RankedOwed) => string;
  gitOf: (activityId: string) => GitRow | undefined;
  /** The decision-in-flight note for a row, if one is showing (Task 5) — by item,
   *  since an earlier decision for the same ACTIVITY can hold it (round 2). */
  flowOf?: (item: RankedOwed) => RowFlowNote | undefined;
  /** Which decision a lingering row was decided with — a sent-back row reads
   *  SENT BACK, not RESUMED (designer P2). */
  decidedOf?: (key: string) => 'approve' | 'sendBack' | undefined;
  empty: {
    counts: EmptyStateCounts;
    /** The header's Begin/Resume, opening the same confirm step. Absent when the
     *  project is operating (nothing left to begin). */
    resume?: { label: string; disabled: boolean; onClick: () => void };
  };
  /** Probes with no answer yet (owedWork's `unchecked`) and a retry for the failed
   *  ones; `retrying` counts the failed ones being asked again right now. Above
   *  zero, the lens makes no all-clear claim. */
  unchecked: UncheckedCounts & { retrying: number; onRetry: () => void };
  onReview: (item: RankedOwed) => void;
  onClearFilters: () => void;
  /** Rows a decision was just made on, lingering with their evidence line (spec
   *  §6: "lingers ~30s, then leaves") — shown, but no longer owed. */
  lingeringKeys?: ReadonlySet<string> | undefined;
  /** The recorded operator pause (B1.7): the lens says so above everything else. */
  paused?: { label: string; reason: string | undefined } | undefined;
}

export function TasksLens(props: TasksLensProps): ReactElement {
  const t = useTokens();
  const { items, totalOwed, policy, projectId } = props;
  const banner = policyBannerFor(policy);
  // A lingering row is shown but no longer owed: the header counts what still waits.
  const lingering = props.lingeringKeys;
  const owedNow = lingering !== undefined ? items.filter((i) => !lingering.has(i.key)) : items;
  const slots = slotsLineFor(owedNow, props.supervisionCap);
  // Lingering rows are always shown; everything else owed may be filtered out.
  const filtered = filteredLineFor(owedNow.length, totalOwed - (lingering?.size ?? 0));

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.TASKS_LENS}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, containerType: 'inline-size' }}
    >
      {props.paused !== undefined ? (
        <Box
          data-testid={UI_IDENTIFIERS.Construction.TASKS_PAUSED_LABEL}
          role="status"
          sx={{
            px: 1.75,
            py: 1.1,
            border: `1.5px solid ${t.line}`,
            borderRadius: `${String(t.radius)}px`,
            bgcolor: t.paperAlt,
            fontFamily: t.mono,
            fontSize: 12,
            fontWeight: 700,
          }}
          title={props.paused.reason}
        >
          {props.paused.label}
          {props.paused.reason !== undefined ? (
            <Box component="span" sx={{ fontWeight: 400, opacity: 0.75, ml: 1 }}>
              ({props.paused.reason})
            </Box>
          ) : null}
        </Box>
      ) : null}
      {banner !== undefined ? (
        <Box
          data-testid={UI_IDENTIFIERS.Construction.TASKS_POLICY_BANNER}
          role="note"
          sx={{
            px: 1.75,
            py: 1.1,
            border: `1.5px dashed ${t.line}`,
            borderRadius: `${String(t.radius)}px`,
            bgcolor: t.paperAlt,
          }}
        >
          <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}>
            <Box component="strong" sx={{ fontWeight: 800 }}>
              {banner.title}
            </Box>{' '}
            {banner.body}{' '}
            <Link
              data-testid={UI_IDENTIFIERS.Construction.TASKS_POLICY_LINK}
              href={`/project/${projectId}/home`}
              sx={{ fontWeight: 700, color: t.accent2, whiteSpace: 'nowrap' }}
              underline="hover"
            >
              {SET_POLICY_LABEL}
            </Link>
          </Typography>
        </Box>
      ) : (
        // The summary would repeat the banner; it speaks only once a policy exists.
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.TASKS_POLICY_SUMMARY}
          sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}
        >
          {policySummaryFor(policy)}
        </Typography>
      )}

      {totalOwed === 0 ? (
        <NothingNeedsYou empty={props.empty} t={t} unchecked={props.unchecked} />
      ) : items.length === 0 ? (
        <>
          <UncheckedNotice t={t} unchecked={props.unchecked} />
          <FilteredOut t={t} totalOwed={totalOwed} onClearFilters={props.onClearFilters} />
        </>
      ) : (
        <>
          <Box>
            <Tooltip placement="bottom-start" title={HEADLINE_TOOLTIP}>
              <Typography
                component="h2"
                data-testid={UI_IDENTIFIERS.Construction.TASKS_HEADLINE}
                sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 17, color: t.ink }}
              >
                {owedNow.length > 0
                  ? headlineFor(owedNow)
                  : allClearHeadlineFor(props.unchecked, true)}
              </Typography>
            </Tooltip>
            {filtered !== undefined ? (
              <Typography
                data-testid={UI_IDENTIFIERS.Construction.TASKS_FILTERED}
                sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted, mt: 0.25 }}
              >
                {filtered}
              </Typography>
            ) : null}
            <UncheckedNotice t={t} unchecked={props.unchecked} />
            {slots !== undefined ? (
              <Typography
                data-testid={UI_IDENTIFIERS.Construction.TASKS_SLOTS}
                sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.awaitingFg, mt: 0.25 }}
              >
                {slots}
              </Typography>
            ) : null}
          </Box>
          <OwedTable {...props} t={t} />
        </>
      )}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// The table
// ---------------------------------------------------------------------------

const FOLD = '@container (max-width: 860px)';

const COLUMNS = [
  { area: 'what', label: 'What' },
  { area: 'why', label: 'Why' },
  { area: 'who', label: 'Who' },
  { area: 'wait', label: 'Waiting' },
  { area: 'blast', label: 'Blast' },
  { area: 'action', label: 'Action' },
] as const;

const GRID_SX = {
  display: 'grid',
  columnGap: 1.5,
  rowGap: 0.5,
  gridTemplateColumns:
    'minmax(0, 2.6fr) minmax(0, 1.25fr) minmax(0, 1fr) minmax(0, 0.75fr) minmax(0, 0.85fr) auto',
  gridTemplateAreas: '"what why who wait blast action"',
  [FOLD]: {
    gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr) auto',
    gridTemplateAreas: '"what what action" "why who action" "wait blast action"',
  },
} as const;

function OwedTable({
  items,
  selection,
  shapeOf,
  gitOf,
  flowOf,
  decidedOf,
  lingeringKeys,
  projectId,
  onReview,
  t,
}: TasksLensProps & { t: Tokens }): ReactElement {
  return (
    <Box data-testid={UI_IDENTIFIERS.Construction.TASKS_TABLE} role="table">
      <Box
        role="row"
        sx={{
          ...GRID_SX,
          px: 1.5,
          pb: 0.75,
          borderBottom: `1.5px solid ${t.line}`,
          [FOLD]: { display: 'none' },
        }}
      >
        {COLUMNS.map((c) => (
          <Typography
            key={c.area}
            role="columnheader"
            sx={{
              gridArea: c.area,
              fontFamily: t.mono,
              fontSize: 9.5,
              fontWeight: 700,
              letterSpacing: '0.1em',
              textTransform: 'uppercase',
              color: t.muted,
            }}
          >
            {c.label}
          </Typography>
        ))}
      </Box>
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1, mt: 1 }}>
        {items.map((item) => (
          <OwedRow
            decided={decidedOf?.(item.key)}
            flow={flowOf?.(item)}
            git={gitOf(item.activityId)}
            item={item}
            key={item.key}
            lingering={lingeringKeys?.has(item.key) === true}
            projectId={projectId}
            selected={isSelected(item, selection)}
            shape={shapeOf(item)}
            t={t}
            onReview={() => {
              onReview(item);
            }}
          />
        ))}
      </Box>
    </Box>
  );
}

function isSelected(item: RankedOwed, selection: LensSelection): boolean {
  if (selection.activityId !== item.activityId) return false;
  const task = item.gate?.task;
  return task === undefined || selection.task === undefined || selection.task === task;
}

function OwedRow({
  item,
  git,
  shape,
  selected,
  lingering,
  flow,
  decided,
  projectId,
  t,
  onReview,
}: {
  item: RankedOwed;
  projectId: string;
  /** How a lingering row was decided. */
  decided: 'approve' | 'sendBack' | undefined;
  git: GitRow | undefined;
  shape: string;
  selected: boolean;
  /** Decided and resumed: shown in place, no longer owed. */
  lingering: boolean;
  flow: RowFlowNote | undefined;
  t: Tokens;
  onReview: () => void;
}): ReactElement {
  const fl = floatPresentation(item.blast.float, asBand(item.blast.band));
  const rail = fl.band !== undefined ? bandTokens(t, fl.band).fg : t.line;
  const fill = taskDetailStateFill(
    t,
    lingering ? 'passed' : item.reason === 'failed' ? 'failed' : 'awaitingHuman'
  );
  const ci = ciVerdictFor(git?.ciStatus);
  const affordance = whyAffordanceFor(item);
  const key = item.key;
  const cell = (column: string): string => UI_IDENTIFIERS.Construction.tasksCell(key, column);

  return (
    <Box
      aria-selected={selected}
      data-lingering={String(lingering)}
      data-reason={item.reason}
      data-risk-floor={String(item.why.riskFloor)}
      data-testid={UI_IDENTIFIERS.Construction.tasksRow(key)}
      role="row"
      sx={{
        ...GRID_SX,
        alignItems: 'start',
        px: 1.5,
        py: 1.25,
        bgcolor: lingering ? t.paper : t.awaitingBg,
        borderRadius: `${String(t.radius)}px`,
        // The float rail, at the critical-path border weight (§7.2) — dashed
        // hairline where no float is known, never a fabricated band.
        borderLeft: `${String(criticalBorderPx(item.blast.onCriticalPath))}px ${fl.known ? 'solid' : 'dashed'} ${rail}`,
        outline: selected ? `2px solid ${t.accent}` : 'none',
        outlineOffset: -2,
      }}
    >
      {/* WHAT */}
      <Box data-testid={cell('what')} sx={{ gridArea: 'what', minWidth: 0 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
          <Box
            sx={{
              px: 0.75,
              py: 0.15,
              borderRadius: 99,
              border: `1px solid ${fill.border}`,
              bgcolor: fill.bg,
              color: fill.fg,
              fontFamily: t.mono,
              fontSize: 9,
              fontWeight: 800,
              letterSpacing: '0.06em',
              textTransform: 'uppercase',
              whiteSpace: 'nowrap',
            }}
          >
            {lingering
              ? decided === 'sendBack'
                ? 'Sent back'
                : 'Resumed'
              : OWED_CHIP[item.reason].label}
          </Box>
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink }}>
            {item.activityId}
          </Typography>
          {item.title !== undefined ? (
            <Typography
              sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, minWidth: 0 }}
              title={item.title}
            >
              {item.title}
            </Typography>
          ) : null}
          {item.kind !== undefined ? <KindBadge kind={item.kind} size="xs" t={t} /> : null}
        </Box>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.awaitingFg, mt: 0.4 }}>
          {whereLabel(item)}
        </Typography>
        <Typography
          sx={{
            fontFamily: t.body,
            fontSize: 13,
            fontWeight: 600,
            color: item.reason === 'failed' ? t.dangerFg : t.ink,
            lineHeight: 1.4,
            mt: 0.4,
          }}
        >
          {askFor(item)}
        </Typography>
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1.25, mt: 0.5, fontFamily: t.mono }}>
          {/* "—" says nothing a reader needs before "no CI record" (designer P2). */}
          {shape !== '—' ? (
            <Typography
              data-testid={cell('shape')}
              sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}
            >
              {shape}
            </Typography>
          ) : null}
          <Typography
            data-testid={cell('ci')}
            sx={{
              fontFamily: t.mono,
              fontSize: 10.5,
              fontWeight: ci.tone === 'danger' ? 700 : 400,
              color: ci.tone === 'danger' ? t.dangerFg : ci.tone === 'ok' ? t.committedFg : t.muted,
            }}
          >
            {ci.label}
          </Typography>
        </Box>
      </Box>

      {/* WHY */}
      <Cell area="why" caption="Why" t={t} testid={cell('why')}>
        <Tooltip title={item.why.tooltip}>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 11,
              fontWeight: item.why.riskFloor ? 800 : 500,
              // The risk floor always ASKS a human: the awaiting tone. dangerFg is
              // failed/error only (palette ruling, rule 2; bandRamp.test.ts pin).
              color: item.why.riskFloor ? t.awaitingFg : t.ink,
            }}
          >
            {item.why.riskFloor ? '⚑ ' : ''}
            {item.why.rule}
          </Typography>
        </Tooltip>
        {affordance === 'stopAsking' || affordance === 'stopAskingFloorMayAsk' ? (
          <>
            <Link
              data-testid={cell('stop-asking')}
              href={`/project/${projectId}/home`}
              sx={{ fontFamily: t.mono, fontSize: 10.5, fontWeight: 700, color: t.accent2 }}
              underline="hover"
            >
              {STOP_ASKING_LABEL}
            </Link>
            {/* A construction gate the risk floor could still hold (EffectiveGate):
                turning the rule off may not stop it (tasks round 2, designer). */}
            {affordance === 'stopAskingFloorMayAsk' ? (
              <Tooltip title={FLOOR_MAY_STILL_ASK_TOOLTIP}>
                <Typography
                  data-testid={cell('stop-asking-hedge')}
                  sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}
                >
                  {FLOOR_MAY_STILL_ASK_LABEL}
                </Typography>
              </Tooltip>
            ) : null}
          </>
        ) : affordance === 'cantTurnOff' ? (
          <Typography
            data-testid={cell('cant-turn-off')}
            sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}
          >
            {CANT_TURN_OFF_LABEL}
          </Typography>
        ) : null}
      </Cell>

      {/* WHO */}
      <Cell area="who" caption="Who" t={t} testid={cell('who')}>
        {item.reviewers.length > 0 ? (
          <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink }}>
            {item.reviewers.map((r) => r.role).join(' · ')}
          </Typography>
        ) : (
          <Unknown t={t} tooltip="No reviewer set was reported for this decision." />
        )}
      </Cell>

      {/* WAITING */}
      <Cell area="wait" caption="Waiting" t={t} testid={cell('waiting')}>
        {/* Folded, WAITING reads on one line — "— · round —" (designer P2). */}
        <Box
          sx={{
            display: 'flex',
            flexDirection: 'column',
            [FOLD]: { flexDirection: 'row', alignItems: 'baseline', gap: 0.5 },
          }}
        >
          <Unknown t={t} tooltip={WAITING_UNKNOWN_TOOLTIP} />
          {item.reason === 'gate' ? (
            <>
              <Typography
                component="span"
                sx={{
                  display: 'none',
                  fontFamily: t.mono,
                  fontSize: 10.5,
                  color: t.muted,
                  [FOLD]: { display: 'inline' },
                }}
              >
                ·
              </Typography>
              <Typography
                data-testid={cell('round')}
                sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}
              >
                {roundLabel(item.round)}
              </Typography>
            </>
          ) : null}
        </Box>
      </Cell>

      {/* BLAST */}
      <Cell area="blast" caption="Blast" t={t} testid={cell('blast')}>
        <Tooltip
          title={
            item.blast.downstreamIds.length > 0
              ? `Waiting on this: ${item.blast.downstreamIds.join(', ')}`
              : 'Nothing downstream is waiting on this decision.'
          }
        >
          <Typography sx={{ fontFamily: t.mono, fontSize: 13, fontWeight: 800, color: t.ink }}>
            ↓{item.blast.downstream}
          </Typography>
        </Tooltip>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
          float {fl.numeral}
          {item.blast.onCriticalPath === true ? ' · critical path' : ''}
        </Typography>
      </Cell>

      {/* ACTION */}
      <Box
        sx={{
          gridArea: 'action',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'flex-end',
          gap: 0.5,
        }}
      >
        <Button
          data-testid={UI_IDENTIFIERS.Construction.tasksReview(key)}
          size="small"
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 11.5,
            textTransform: 'none',
            color: t.bg,
            bgcolor: t.accent,
            '&:hover': { bgcolor: t.accent2 },
          }}
          variant="contained"
          onClick={onReview}
        >
          Review
        </Button>
        {git?.prUrl !== undefined ? (
          <Link
            data-testid={UI_IDENTIFIERS.Construction.tasksGitHub(key)}
            href={git.prUrl}
            rel="noopener noreferrer"
            sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}
            target="_blank"
            underline="hover"
          >
            GitHub ↗
          </Link>
        ) : null}
        {flow !== undefined ? (
          <Typography
            data-testid={UI_IDENTIFIERS.Construction.tasksFlow(key)}
            role="status"
            sx={{
              fontFamily: t.mono,
              fontSize: 10.5,
              fontWeight: 700,
              textAlign: 'right',
              maxWidth: 200,
              color:
                flow.tone === 'danger'
                  ? t.dangerFg
                  : flow.tone === 'ok'
                    ? t.committedFg
                    : t.awaitingFg,
            }}
          >
            {flow.text}
          </Typography>
        ) : null}
      </Box>
    </Box>
  );
}

/** `Detailed Design › Design Review (book: Design Review)`, or what stopped. */
function whereLabel(item: RankedOwed): string {
  if (item.reason === 'takeover') return 'Variance · awaiting an operator steer';
  if (item.reason === 'failed') return 'Terminal failure · the pump will not restart it';
  const g = item.gate;
  const phase = g?.phaseName ?? g?.lifecyclePhase ?? 'phase unreported';
  if (g?.label === undefined) return `${phase} › gate`;
  const book =
    g.bookLabel !== undefined && g.bookLabel !== g.label ? ` (book: ${g.bookLabel})` : '';
  return `${phase} › ${g.label}${book}`;
}

function Cell({
  area,
  caption,
  testid,
  t,
  children,
}: {
  area: string;
  caption: string;
  testid: string;
  t: Tokens;
  children: ReactNode;
}): ReactElement {
  return (
    <Box data-testid={testid} sx={{ gridArea: area, minWidth: 0 }}>
      {/* The column's name inside the cell, shown only once the table has folded
          and its header row is gone. */}
      <Typography
        sx={{
          display: 'none',
          fontFamily: t.mono,
          fontSize: 9,
          fontWeight: 700,
          letterSpacing: '0.1em',
          textTransform: 'uppercase',
          color: t.muted,
          [FOLD]: { display: 'block' },
        }}
      >
        {caption}
      </Typography>
      {children}
    </Box>
  );
}

function Unknown({ t, tooltip }: { t: Tokens; tooltip: string }): ReactElement {
  return (
    <Tooltip title={tooltip}>
      <Typography component="span" sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
        —
      </Typography>
    </Tooltip>
  );
}

// ---------------------------------------------------------------------------
// The two non-table states
// ---------------------------------------------------------------------------

/**
 * Probes that have not answered, said plainly: still checking, or could not check
 * (with a Retry that refetches exactly those). Nothing when every probe answered.
 */
function UncheckedNotice({
  unchecked,
  t,
  prominent = false,
}: {
  unchecked: TasksLensProps['unchecked'];
  t: Tokens;
  /** In place of the empty state's heading, at its weight. */
  prominent?: boolean;
}): ReactElement | null {
  const pending = uncheckedPendingLine(unchecked.pending);
  const errored = uncheckedErroredLine(unchecked.errored);
  if (pending === undefined && errored === undefined) return null;
  const text = {
    fontFamily: prominent ? t.display : t.mono,
    fontWeight: 700,
    fontSize: prominent ? 17 : 11.5,
  };
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.TASKS_UNCHECKED}
      role="status"
      sx={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: prominent ? 'center' : 'flex-start',
        gap: 0.5,
        mt: prominent ? 0 : 0.25,
      }}
    >
      {pending !== undefined ? (
        <Typography sx={{ ...text, color: t.muted }}>{pending}</Typography>
      ) : null}
      {errored !== undefined ? (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography sx={{ ...text, color: t.dangerFg }}>{errored}</Typography>
          {/* Stays in place while the failed probes are asked again, saying so —
              never a Retry that vanishes mid-click (designer re-check B1). */}
          <Button
            data-testid={UI_IDENTIFIERS.Construction.TASKS_UNCHECKED_RETRY}
            disabled={unchecked.retrying > 0}
            size="small"
            sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, textTransform: 'none' }}
            variant="outlined"
            onClick={unchecked.onRetry}
          >
            {retryLabel(unchecked.retrying > 0)}
          </Button>
        </Box>
      ) : null}
    </Box>
  );
}

function NothingNeedsYou({
  empty,
  unchecked,
  t,
}: {
  empty: TasksLensProps['empty'];
  unchecked: TasksLensProps['unchecked'];
  t: Tokens;
}): ReactElement {
  // The all-clear is a claim about every in-flight activity: made only once every
  // probe has answered. Until then the unchecked lines take the heading's place.
  const headline = allClearHeadlineFor(unchecked);
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.TASKS_EMPTY}
      sx={{
        border: `1.5px solid ${t.line}`,
        borderRadius: `${String(t.radius)}px`,
        bgcolor: t.paper,
        px: 3,
        py: 5,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        gap: 1.25,
        textAlign: 'center',
      }}
    >
      {headline !== undefined ? (
        <Tooltip title={HEADLINE_TOOLTIP}>
          <Typography
            component="h2"
            sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 22, color: t.ink }}
          >
            {headline}
          </Typography>
        </Tooltip>
      ) : (
        <UncheckedNotice prominent t={t} unchecked={unchecked} />
      )}
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.TASKS_EMPTY_COUNTS}
        sx={{ fontFamily: t.mono, fontSize: 12.5, color: t.muted }}
      >
        {emptyStateLine(empty.counts)}
      </Typography>
      {empty.resume !== undefined ? (
        // A link, not a second primary button beside the header's own (designer
        // P2) — and it says what it would start with. Same confirm step.
        <Link
          component="button"
          data-testid={UI_IDENTIFIERS.Construction.TASKS_RESUME}
          disabled={empty.resume.disabled}
          sx={{
            mt: 0.5,
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 12,
            color: t.accent2,
            '&:disabled': { color: t.muted, cursor: 'default', textDecoration: 'none' },
          }}
          underline="hover"
          onClick={empty.resume.onClick}
        >
          {beginLinkLabel(empty.resume.label, empty.counts.eligible)}
        </Link>
      ) : null}
    </Box>
  );
}

function FilteredOut({
  totalOwed,
  t,
  onClearFilters,
}: {
  totalOwed: number;
  t: Tokens;
  onClearFilters: () => void;
}): ReactElement {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.TASKS_EMPTY}
      sx={{
        border: `1.5px dashed ${t.line}`,
        borderRadius: `${String(t.radius)}px`,
        px: 3,
        py: 3,
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        flexWrap: 'wrap',
      }}
    >
      <Typography sx={{ fontFamily: t.body, fontSize: 13, color: t.ink }}>
        {totalOwed === 1
          ? '1 decision is owed, but the toolbar’s filters hide it.'
          : `${String(totalOwed)} decisions are owed, but the toolbar’s filters hide them.`}
      </Typography>
      <Button size="small" variant="outlined" onClick={onClearFilters}>
        Clear filters
      </Button>
    </Box>
  );
}
